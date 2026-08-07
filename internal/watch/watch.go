// Package watch reports changes to a single file by watching its parent
// directory.
package watch

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// DefaultDebounce collapses a burst of writes into one reload. br can touch
// issues.jsonl several times during a sync, and re-decoding a multi-megabyte
// file per event is visible in the UI.
const DefaultDebounce = 150 * time.Millisecond

// maxDebounce bounds how long a sustained burst can defer a reload. Without a
// ceiling, a steady stream of writes spaced closer together than debounce
// would reset the debounce timer forever and the UI would never update again,
// silently. The ceiling is measured from the first event of a burst, not
// reset by subsequent events in the same burst.
const maxDebounce = 2 * time.Second

// Watcher reports debounced changes to one file.
//
// It watches the file's parent directory rather than the file itself. br
// writes atomically — temp file, then rename — and a file-level watch is bound
// to an inode the rename replaces. Such a watch keeps reporting success while
// silently never firing again, which presents as a viewer that stops updating
// for no visible reason. A directory inode is stable, so filtering directory
// events by name is immune.
type Watcher struct {
	fsw    *fsnotify.Watcher
	events chan struct{}
	done   chan struct{}
	closer sync.Once
	log    *slog.Logger
	target string
}

// New watches path's parent directory and reports changes to path.
func New(log *slog.Logger, path string, debounce time.Duration) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create watcher: %w", err)
	}

	dir := filepath.Dir(path)
	if err := fsw.Add(dir); err != nil {
		_ = fsw.Close()

		return nil, fmt.Errorf("watch %s: %w", dir, err)
	}

	w := &Watcher{
		fsw:    fsw,
		events: make(chan struct{}, 1),
		done:   make(chan struct{}),
		log:    log,
		target: filepath.Clean(path),
	}

	go w.run(debounce)

	return w, nil
}

// Events delivers one value per debounced burst of changes.
func (w *Watcher) Events() <-chan struct{} { return w.events }

// Close stops watching and waits for the events channel to close. It is safe
// to call more than once.
//
// Once it returns, a consumer ranging over Events() has been released rather
// than left parked on a channel nothing will ever send to again. That costs
// nothing today — Close runs from main's deferred cleanup immediately before
// the process exits — but it is what makes a second watcher safe to start in
// the same process, for a workspace switch or a daemon mode, without leaking
// the first one's consumer goroutine.
//
// The drain below is both the wait and the release: run closes w.events on
// its way out and is its only sender, so ranging here returns exactly when
// that goroutine has finished. It cannot deadlock, because run's only send is
// non-blocking (see notify) and its every branch returns on w.done.
func (w *Watcher) Close() error {
	var err error
	w.closer.Do(func() {
		close(w.done)
		err = w.fsw.Close()
		for range w.events { //nolint:revive // draining until run closes it is the wait
		}
	})
	if err != nil {
		return fmt.Errorf("close watcher: %w", err)
	}

	return nil
}

// run translates filesystem events into debounced notifications.
//
// Two timers cooperate: debounceTimer resets on every matching event so a
// burst collapses to one reload once writes settle, and ceilingTimer starts
// once at the first event of a burst and is never reset, so a sustained
// stream cannot defer the reload past maxDebounce.
func (w *Watcher) run(debounce time.Duration) {
	// Closed here rather than in Close because this goroutine is the only
	// sender: closing a channel out from under a send panics, and no amount
	// of care at the call site fixes that once the two live in different
	// goroutines. Closing on the sender's way out is the one placement where
	// the race cannot arise.
	defer close(w.events)

	debounceTimer := time.NewTimer(debounce)
	stopTimer(debounceTimer)
	defer debounceTimer.Stop()

	ceilingTimer := time.NewTimer(maxDebounce)
	stopTimer(ceilingTimer)
	defer ceilingTimer.Stop()

	pending := false

	for {
		select {
		case <-w.done:
			return

		case event, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			pending = w.handleEvent(event, debounceTimer, ceilingTimer, debounce, pending)

		case <-debounceTimer.C:
			pending = false
			stopTimer(ceilingTimer)
			w.notify()

		case <-ceilingTimer.C:
			pending = false
			stopTimer(debounceTimer)
			w.notify()

		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			w.log.Warn("watcher error", slog.String("target", w.target), slog.Any("error", err))
		}
	}
}

// handleEvent resets the debounce timer when event names the watched file,
// starts the ceiling timer on the first event of a new burst, and reports
// whether a reload is now pending.
func (w *Watcher) handleEvent(
	event fsnotify.Event, debounceTimer, ceilingTimer *time.Timer, debounce time.Duration, pending bool,
) bool {
	if filepath.Clean(event.Name) != w.target {
		return pending
	}

	if !pending {
		// Burst clock starts once and is not reset by later events in the
		// same burst; that is what makes it an absolute ceiling.
		ceilingTimer.Reset(maxDebounce)
	}

	// Reset on every write so a burst fires once, after it settles.
	stopTimer(debounceTimer)
	debounceTimer.Reset(debounce)

	return true
}

// stopTimer stops t and drains a pending value if Stop reports the timer had
// already fired. Safe to call on a timer that was never started.
func stopTimer(t *time.Timer) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
}

// notify delivers an event without blocking.
//
// The channel has capacity one and a full buffer is dropped deliberately: the
// pending event already says "something changed", which is the entire payload,
// so a slow consumer coalesces rather than falling behind.
func (w *Watcher) notify() {
	select {
	case w.events <- struct{}{}:
	default:
	}
}
