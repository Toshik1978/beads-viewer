# Architecture

This document explains the shape of `bv` and, where it matters, why it is
shaped that way. See [`CLAUDE.md`](../CLAUDE.md) for the rules an agent
working on this code must follow, and [`README.md`](../README.md) for what
the finished program does.

## Layering

```
internal/beads      domain: JSONL decoding, snapshot indexing, filtering, derived state
internal/config     configuration resolution (flags, env, file, defaults)
internal/watch      debounced filesystem notification
internal/tui        the bubbletea application: root model, key map, layout
  ├── listview, treeview, boardview, depsview   the four views, as peer packages
  ├── rowfmt                         row composition shared by list and tree
  ├── cardfmt                        card composition shared by board and deps
  ├── detail                          the detail pane (markdown rendering)
  ├── theme                           colour scheme resolution and styles
  └── uitext                         display-width-aware text helpers
internal/licensing  repository-fact helpers for the licensing gate (its own test only)
cmd/bv              the composition root
```

`internal/licensing` sits outside the runtime import graph the rest of this
diagram describes — nothing in `cmd/bv` or `internal/tui` imports it, and it
imports none of them back. It exists for one consumer: its own test, which
enumerates git-tracked files and enforces the licensing invariants in
[`CLAUDE.md`](../CLAUDE.md). It is listed here anyway because it is a real
package with a build-breaking constraint of its own — `task check` fails if
its sweep fires — and a layering diagram that omitted it would understate
what actually gates a commit.

`internal/tui/rowfmt` holds the composition-and-styling rule shared by
`listview` and `treeview`, the two views that render issues as fixed columns:
a row is laid out once and rendered two ways, selected or not, because
`theme.Selected`'s background and a per-column foreground cannot coexist (see
`rowfmt.Columns.Styled`'s own doc comment). It deliberately does not own the
width ladder — which column each view sacrifices first as the pane narrows —
because that ladder differs per view; only the shared parts, such as the
status column's width, live here. It exists to keep "views are peers, they
don't import each other" true even where two views want the identical row
logic: without it, `treeview` would either duplicate `listview`'s styling or
import `listview` directly, and the rule in the next section would already be
broken by the package that follows it in this diagram.

`internal/tui/cardfmt` exists for the same reason, one level up: `boardview`
and `depsview` both render an issue as the same card — title, status,
priority — and without a shared package the second view built would have had
to either duplicate that rendering or import the first, breaking the same
peer rule `rowfmt` was created to keep intact. Two views wanting the
identical presentation is not a coincidence that happened twice; it is what a
fixed-width card representation of an issue looks like regardless of which
view is asking for one, which is exactly the argument for factoring it out
before a third view repeats the choice a third time.

`internal/beads` imports nothing from `charm.land` or any other UI package.
This is not incidental: it is what makes the domain — JSONL decoding,
snapshot indexing, ready/blocked derivation, filtering — testable without a
terminal, a pty, or any bubbletea machinery at all. A change to how ready or
blocked is computed can be verified with a plain table-driven test against a
constructed snapshot, with no rendering involved. If a domain type ever needs
to import something from `internal/tui`, that is a sign the boundary has been
crossed the wrong way.

## Views are peers behind an interface, not fields on the root model

The `View` interface (`internal/tui/view.go`) is deliberately narrow:
`SetSize`, `SetSnapshot`, `SetTheme`, `Update`, `View`, `Selected`, `Reveal`.
The list, tree, board and dependency views each implement it in their own
package, and the root `Model` holds them as `views [viewCount]View` — an
array of the interface, not four named struct fields, let alone four sets of
inlined state.

`Reveal` is the one method this epic added to that list, and it earns its
place there: it is what makes a view switch land on the issue the user was
already looking at, rather than resetting the selection every time. It is
deliberately not named `SelectByID` — the method `listview` and `boardview`
already exported for moving a cursor among rows that already exist — because
satisfying it can require changing *what is visible* before anything can be
selected: the tree view expands whichever collapsed ancestors are hiding the
row, and the dependency view re-roots its four columns on the new subject
outright.

The reason is concrete, not stylistic: the terminal application this project
replaced kept every view's state as fields directly on one root `Model` —
roughly 100 fields and 90 methods by the time this rewrite started, because
each new feature for the list, the tree or the board had nowhere to go except
onto that same struct. `Model` in this codebase is capped at 12 fields by a
test (`TestModelStaysSmall`), and the cap is enforceable precisely because
nothing about any view's own internal state is allowed to live there — it
lives inside `listview.Model`, `treeview.Model`, `boardview.Model` and
`depsview.Model`, each in its own package, each free to grow its own fields
without threatening the cap.

This section used to predict that adding a fourth view would mean adding a
package and one array slot, not renegotiating what the root model is allowed
to hold. That prediction has now been tested rather than merely stated: the
dependency view epic added one view package (`depsview`), one array slot
(`viewCount` from 3 to 4), one `config.ViewKind` value (`ViewDeps`), and two
key bindings (`ViewDeps`, `Back`) — plus a 28-line move out of `app.go` to
stay clear of this repository's 500-line-per-file cap, since the fourth
slot's wiring would otherwise have pushed it over. `Model`'s own field count
did not move. That is the claim the prediction was actually making, and it is
the one that held.

## Filtering happens once, in the app

`beads.Filter` is applied exactly once, in `Model.applyFilter`
(`internal/tui/app.go`), and the same filtered snapshot is handed to every
view. No view is permitted to filter for itself. The alternative — each view
running its own copy of the filter logic — is how the predecessor ended up
with three views that answered the same query differently: a text search that
matched a label in the list but not in the tree, because the two had quietly
diverged over time. Filtering once and handing the result down is what
guarantees a query means the same thing everywhere it is shown.

## The snapshot is immutable

`beads.Snapshot` (`internal/beads/snapshot.go`) is built once from a decoded
issue set and never mutated afterward. Every accessor that returns a slice
clones it on the way out, and construction itself clones every field of every
`Issue` that could alias the caller's memory. This is what lets the watcher
goroutine build a brand-new snapshot off the UI thread, on every reload,
without a lock: the old snapshot the UI is currently rendering from is never
touched, so there is nothing for a lock to protect. The app simply swaps the
pointer (`Model.applySnapshot`) once the new snapshot is ready, and the next
frame renders from it.

Rebuilding everything on every write is the whole of the design, so it is
worth stating what it costs rather than only why it is safe. Measured at
10,000 issues (a 12 MB `issues.jsonl`): decoding the file takes 58 ms, and a
keystroke still redraws in 1.2 ms, because the decode runs off the event loop
and the frame is served from indexes built once per snapshot rather than by
scanning. Around 50,000 issues — 277 ms to decode — is where a reload starts
to be something a reader feels, and where the answer would be an incremental
reader rather than anything in the UI. Those three numbers are what make
"just rebuild it" a design rather than a shortcut: the full table they are
quoted from, including memory and the very-large-file case, is maintained in
[`README.md`](../README.md#scale) and not duplicated here.

The same construction pass also builds a reverse index. Each `Issue`'s own
`Dependencies` only says what blocks *it* — the forward direction. `Snapshot`
additionally indexes `dependents` (every issue that declares a dependency on a
given id, i.e. what that id blocks) and `relatives` (every issue joined to it
by a relation edge — any of the seven types `DepType.IsRelation` reports true
for), both keyed by id, in the same single pass. "What does this block" and
"what is related to this" are therefore
answered by a map lookup rather than a scan of every issue in the workspace —
a cost the dependency view pays on every re-root and every reload, so a
linear scan there is not a one-off. `detail.renderBlocks` used to do exactly
that scan, on every frame, before the reverse index existed to make it
unnecessary.

## The directory is watched, not the file

`internal/watch` watches `issues.jsonl`'s *parent directory*, filtering events
by name, rather than watching the file itself. `br` writes atomically — a
temporary file, then a rename over the target — and a watch registered
against the file directly is a watch on that file's inode. The rename
replaces the inode the watch is bound to, and the watch keeps reporting
success while silently never firing again. This fails with no visible error:
`bv` would simply stop noticing changes after the very first atomic write,
and nothing about that failure mode would look different from `br` just not
having run. A directory's inode is stable across a rename inside it, so
filtering directory-level events by filename is immune to the problem
entirely.

## Derived state: `br`'s storage layer is the authority

<!--
  Deliberately no file:line citation into br's own source here. The natural
  form of that citation names the relative path of a sibling checkout on this
  machine, and internal/licensing's sweep disallows that exact string in
  every tracked file without exception — README.md included — so adding it
  back would turn the licensing gate red. Point a reader at the SQL by
  describing the behaviour instead, as
  the paragraph below does, not by citing where it lives.
-->

`beads.Snapshot.IsBlocked` and `IsReady` (`internal/beads/derive.go`) are
deliberately written to mirror `br`'s own blocker and readiness queries in its
SQLite storage layer line for line where it matters — including two details
that are easy to get backwards: a parent-child edge never blocks *by itself*,
and a dependency on an id absent from the snapshot *does* block, because an
unresolvable blocker is not the same thing as a satisfied one. The first of
those has a second half worth stating alongside it: an unfinished parent is
not a blocker — that is the ordinary state of every subtask — but a parent
held up by a dependency of its own is waiting on something the child cannot
finish, so that wait is inherited all the way down the chain. Any future
change to how `bv` computes ready or blocked should be checked against that
storage layer's own logic first, not re-derived from first principles — `br`
is the system of record, and `bv` exists to read what `br` already decided.

One divergence is deliberate and documented rather than accidental: `bv`
hardcodes the ready status group to `{open}`, while `br` allows a project to
widen that group through `.beads/policy.yaml`. `bv` does not read that file.
See [`README.md`](../README.md#known-divergences-from-br) for what this means
for a user.

## Why History was scoped out

Three measurements shaped the decision to drop history and git-correlation
features from this rewrite entirely rather than port them. They are inherited
from the spec that scoped this project — taken on real repositories rather
than estimated, but not reproduced in this repository: there is no benchmark
in this tree (`grep -rl "func Benchmark"` under this module returns nothing).

- Reading every revision of a multi-hundred-revision, several-hundred-megabyte
  JSONL history with `git log --raw` piped through `git cat-file --batch`
  took **130 ms**; doing the equivalent walk through a popular pure-Go git
  library took **3.05 s** — 23.5× slower, most of it lost to a path-filtered
  log walk that library could not use the repository's own commit-graph
  Bloom filters to skip.
- A full, uncached correlation run — matching commits to issues — took
  **410 ms** on a repository with 1,368 commits and 682 beads. An entire
  caching subsystem existed upstream purely to avoid re-paying that 410 ms.
  This repository's own history is far smaller (54 commits, 35 issues at the
  time of writing), so the 410 ms figure describes the spec's measurement,
  not a number reproduced here.
- Even with all of that machinery, the correlation heuristic that actually
  found anything was co-commit matching, 100% of the time, in every repository
  measured. The explicit-ID and temporal/author heuristics sitting beside it
  contributed nothing.

None of those numbers describe a feature worth its own dependency (a git
library), its own cache invalidation surface, and its own slice of the
maintenance burden — for a personal-scale, read-only viewer whose entire job
is showing what is in `.beads/issues.jsonl` right now. History was dropped by
decision, not by omission, and the git dependency it would have required
never entered this module's dependency graph at all.
