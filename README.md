# bv

[![CI](https://github.com/Toshik1978/beads-viewer/actions/workflows/ci.yml/badge.svg)](https://github.com/Toshik1978/beads-viewer/actions/workflows/ci.yml)
[![Coverage](https://img.shields.io/endpoint?url=https%3A%2F%2Fgist.githubusercontent.com%2FToshik1978%2Fb96c4f7fe1e8b439c3dea83be6d4eaaf%2Fraw%2Fcoverage.json)](https://github.com/Toshik1978/beads-viewer/actions/workflows/ci.yml)
[![Tests](https://img.shields.io/endpoint?url=https%3A%2F%2Fgist.githubusercontent.com%2FToshik1978%2Fb96c4f7fe1e8b439c3dea83be6d4eaaf%2Fraw%2Ftests.json)](https://github.com/Toshik1978/beads-viewer/actions/workflows/ci.yml)

`br` is a command-line issue tracker that stores a project's issues as
`.beads/issues.jsonl`. `bv` is its read-only terminal companion: it reads
that file and gives you four ways to look at it — a list with a detail
pane, a parent/child tree, a kanban board, and a dependency board showing
what blocks the issue you are on and what is waiting on it — with live
reload as `br` writes.

`bv` never writes anywhere inside `.beads/` and never spawns a subprocess.
`br` is the only thing that ever changes your data; `bv` only reads it.

## Install

### Release archive

Each release publishes four archives, one per platform:

| Platform | Archive |
|---|---|
| Linux, x86_64 | `bv_<version>_linux_amd64.tar.gz` |
| Linux, arm64 | `bv_<version>_linux_arm64.tar.gz` |
| macOS, Intel | `bv_<version>_darwin_amd64.tar.gz` |
| macOS, Apple silicon | `bv_<version>_darwin_arm64.tar.gz` |

There is no Windows build. Download the archive for your platform and its
checksum, verify, then extract:

```bash
sha256sum -c bv_<version>_checksums.txt --ignore-missing   # Linux
shasum -a 256 -c bv_<version>_checksums.txt --ignore-missing   # macOS: sha256sum isn't stock
tar xzf bv_<version>_<os>_<arch>.tar.gz
```

Each archive contains the `bv` binary alongside `LICENSE` and this
`README.md`. MIT requires the copyright notice to travel with every copy, so
`LICENSE` ships inside the archive rather than only in the repository.

### From source

```bash
go install github.com/Toshik1978/beads-viewer/cmd/bv@latest
```

This requires a Go toolchain (`go.mod` currently targets Go 1.26) and builds
with `CGO_ENABLED=0`.

## Usage

Run `bv` from inside a project that has a `.beads` directory, or point it at
one directly:

```bash
bv                    # discovers .beads/ by walking up from the working directory
bv --db path/to/.beads
```

Workspace discovery, in order: `--db`, then the `BEADS_DIR` environment
variable, then the nearest `.beads` directory at or above the current
directory.

`bv` needs a terminal on stdin, and says so rather than starting when it does
not have one — `echo | bv`, `bv < /dev/null`, or a cron job — so a redirected
run ends in one line on stderr instead of a process that never returns.
`--version` and `--help` are unaffected and pipe normally.

### Flags

```
bv [flags]

      --db string      path to a .beads directory (overrides BEADS_DIR)
      --theme string   colour scheme: auto, light or dark
      --view string    initial view: list, tree, board or deps
      --hide-closed    hide closed issues (default true)
  -v, --version        version for bv
  -h, --help           help for bv
```

## Keybindings

These are grouped exactly as they appear in `bv`'s own in-app help overlay
(press `?`). A test compares the two sets of keys in both directions, so a
binding added, removed or moved without updating these tables fails the build
rather than shipping.

**Global**

| Key | Action |
|---|---|
| `q` / `ctrl+c` | quit |
| `?` | toggle help |
| `y` | yank the selected issue id to the clipboard |
| `tab` | move focus between the list and the detail pane |
| `/` | edit the free-text filter |
| `c` | toggle hide-closed |
| `esc` | clear the filter query |

**Views**

| Key | Action |
|---|---|
| `1` | list view |
| `2` | tree view |
| `3` | board view |
| `4` | dependency view |
| `enter` | open the selected card in the list view (board), or re-root the dependency view on the highlighted card |
| `backspace` | walk back to the previously focused card (dependency view) |

**Navigation**

| Key | Action |
|---|---|
| `up` / `k` | up |
| `down` / `j` | down |
| `left` / `h` | collapse the current node, or move to the previous column (board, deps) |
| `right` / `l` | expand the current node, or move to the next column (board, deps) |
| `home` / `g` | jump to top |
| `end` / `G` | jump to bottom |
| `space` | toggle expand (tree node, or a board card's detail) |
| `p` | jump to parent (tree) |
| `o` | expand all (tree) |
| `O` | collapse all (tree) |
| `s` | cycle swimlane grouping (board: status, priority, assignee, type) |
| `pgup` / `ctrl+u` | scroll the detail pane up |
| `pgdown` / `ctrl+d` | scroll the detail pane down |
| `ctrl+b` | page up (tree) |
| `ctrl+f` | page down (tree) |

While the filter box is open, every key edits the filter text and the view
narrows as you type, roughly a sixth of a second after you stop. `Enter`
closes the box and keeps what you typed; `Escape` closes it and restores the
filter to what it was when the box opened — including a filter that was
already active before you pressed `/`. With the box closed, `esc` clears the
typed query outright, leaving hide-closed alone.

## The dependency view

Press `4` from any view and the issue you were looking at becomes the subject of
four columns: what is blocking it, the issue itself, what it blocks, and what it
is merely related to.

The left column answers more than one question, because "what is blocking this"
has more than one answer. A card there can be an ordinary unfinished dependency;
an id this workspace has no issue for, labelled *not in workspace*; an ancestor
labelled *via parent*, when the thing holding you up is something a parent is
waiting on rather than anything about this issue; or, for an epic, a still-open
child. Each is labelled, because they need different responses.

This is more than a labelling nicety: an issue's own dependency rows are only
one of the ways it can be blocked, so a column that listed only those rows
would say "nothing is blocking this" about an issue the status bar still
counts as blocked. The four kinds exist, and are labelled distinctly, because
an empty blockers list here does not mean unblocked.

`enter` makes the highlighted card the new subject and rebuilds the columns
around it, so a chain of blockers can be walked one hop at a time. `backspace`
walks back. Leaving with `1`, `2` or `3` takes the issue you ended on with you.

Parent and child edges are deliberately absent: that hierarchy is the tree
view's subject, and repeating it here would make the outer columns mean two
things at once.

## Configuration

`bv` resolves its settings from four sources, in decreasing precedence:
command-line flags, environment variables, `~/.config/bv/config.yaml` (or
`$XDG_CONFIG_HOME/bv/config.yaml` when that variable is set), then built-in
defaults.

A full worked example of the config file:

```yaml
# ~/.config/bv/config.yaml
theme: dark           # auto (default), light or dark
view: tree            # list (default), tree, board or deps
hide_closed: false    # true by default
```

The equivalent environment variables are `BV_THEME`, `BV_VIEW`,
`BV_HIDE_CLOSED`, `BEADS_DIR` (the workspace path — the same variable `--db`
overrides) and `BV_LOG` (a file path enabling debug logging; unset, logs are
discarded rather than written to stderr, which would corrupt the terminal
display).

## State

`bv` writes nothing inside `.beads/` — but it is not stateless everywhere.
The tree view persists its expansion and selection to
`$XDG_STATE_HOME/bv/trees/` (falling back to `~/.local/state/bv/trees/` when
that variable is unset), keyed to the workspace, on exit — so the tree
reopens the way you left it. If the tree view opens already expanded and you
didn't expect that, this is why. The file lives outside `.beads/`, so it
never conflicts with anything `br` writes, and deleting it simply resets the
tree to its default: the root expanded, everything below it collapsed — not
to fully collapsed.

## The status bar

The status bar's counts — issues, open, ready, blocked, closed — are always
computed from the **whole workspace**, never from whatever the current search
or view has narrowed down to. Search for something that matches three issues
and the bar still reads the workspace's full total. That is deliberate:

- The counts describe the workspace, not the query — they are the
  denominator you are narrowing against, and the only place `bv` reports the
  workspace as a whole.
- More to the point, filtering them would be actively wrong, not merely
  inconsistent: readiness and blocking are resolved by looking up each
  dependency's target issue, and a dependency on an id that lookup can't find
  is deliberately treated as still blocking (see Known divergences below for
  why). Compute those against a filtered snapshot and a blocking issue sitting
  just outside the filter becomes unresolvable — silently changing *which*
  issues count as blocked based on what was typed into the search box. A
  cosmetic mismatch is preferable to that.

Two things the numbers do not mean, worth being explicit about:

- **"Open" folds in `in_progress`.** `bv`'s open count sums every non-terminal
  status; `br stats` reports `Open` and `In Progress` as separate lines on the
  same workspace. Three open issues and one in-progress show as `4 open` in
  `bv`. This is the same class of divergence as the ready count below, just
  visible on every frame instead of only in a project that has reconfigured
  its ready group.
- **Hide-closed narrows what you see, not what gets counted**, and it is on
  by default: a session started with no flag, no environment variable and no
  config entry already hides closed issues, so the status bar reads
  `(N hidden)` and `hide closed` before you have pressed anything. Turning it
  off takes one of four things — `--hide-closed=false`, `hide_closed: false`
  in `config.yaml`, `BV_HIDE_CLOSED=false`, or pressing `c` during the
  session. It is one filter shared by the views — `c` toggles it everywhere
  at once, and the choice is not persisted between runs — with a single
  exception: the board's status lane goes on showing closed issues, because
  it already has a Closed column that sets them apart. Cycle the board to any
  other lane with `s` and it hides them like everything else. The status
  bar's counts stay computed from every issue in the workspace either way,
  alongside an `(N hidden)` indicator while it is on, which goes quiet on the
  one pane that is not hiding anything.

## Known divergences from `br`

These are documented deliberately rather than hidden.

- **Ready is hardcoded to `status == open`.** `br` lets a project widen the
  ready status group via `workflow.status_groups.ready` in
  `.beads/policy.yaml`. `bv` ignores that file — its ready count always means
  "open, unblocked, not deferred, and not a throwaway issue (an id containing
  `-wisp-`)" — so a project that has reconfigured its ready group will see
  `bv`'s ready count disagree with `br ready`.
- **`bv`'s "open" folds in `in_progress`; `br stats` does not.** See The
  status bar above.
- **Yank needs OSC 52, and needs something selected.** The `y` key copies the
  selected issue id through the OSC 52 terminal escape sequence rather than a
  local clipboard library, so it works over SSH without `bv` ever spawning a
  subprocess. Not every terminal implements OSC 52, and inside tmux it
  additionally requires `set -g set-clipboard on` in your tmux configuration.
  Yank is also a silent no-op when nothing is selected — an empty list, for
  instance. If yank silently does nothing, one of these two is why.
- **Filtering by status or label, and showing tombstones, are not exposed.**
  The underlying filter can express all three, but no key, flag or config
  option reaches them in this MVP — they were cut from the UI, not forgotten.
  The one filter you can reach is free-text search plus `--hide-closed`.

## Scale

`bv` re-reads and re-decodes the whole of `issues.jsonl` on every reload,
because that is the only file it reads and `br` rewrites it atomically. That
one decision sets the ceiling, so it is worth stating where it lies rather
than leaving you to find out.

Measured on synthetic workspaces with a realistic dependency graph — a third
of issues blocked by one or two others, epics with children, parents, labels
and comments — at 160x50:

| | 10,000 issues (12 MB) | 50,000 issues (62 MB) |
|---|---|---|
| Decode `issues.jsonl` | 58 ms | 277 ms |
| Startup, decode to first frame | 67 ms | 335 ms |
| Keystroke to redrawn frame | 1.2 ms | 4.7 ms |
| Filter commit (Enter) | 3.9 ms | 26 ms |
| Peak memory | 47 MB | 210 MB |

A typical issue is around 1.2 KB, so 10,000 of them is roughly 12 MB. An
issue carrying a long `design` field can be ten times that; at 11 KB/issue,
10,000 issues make a 112 MB file, which still opens and scrolls — decode
rises to ~300 ms and peak memory to ~250 MB, while a keystroke still redraws
in under 3 ms.

Interaction stays comfortable well past the point where the file stops being
comfortable, for three reasons: the status bar's counts are served from
indexes built once per snapshot rather than by scanning; filtering commits on
Enter, not on every keystroke; and the reload runs off the event loop, so
even a 300 ms decode never blocks a keypress.

**What degrades first is repeated reloading, not size itself.** Every `br`
write triggers a full re-decode — collapsed by a 150 ms debounce, but still
proportional to the whole file. Beside a very large workspace under a busy
`br` session, that is a background thread decoding and a garbage collector
reclaiming, continuously. `bv` stays responsive; a laptop battery will
notice.

Roughly 50,000 issues is where the decode starts to be something you feel on
reload. Above that, the fix would be an incremental reader rather than
anything in the UI.

## Contributing

Read [`CONTRIBUTING.md`](./CONTRIBUTING.md) first — the short version is that
this is a personal project, pull requests may not be reviewed, and forking is
a first-class outcome rather than a fallback.

## Acknowledgements

`bv` was inspired by
[beads_viewer](https://github.com/Dicklesworthstone/beads_viewer), a Go TUI
for beads by Jeffrey Emanuel, and reads the workspace format of
[beads](https://github.com/steveyegge/beads) by Steve Yegge.

It is an independent implementation rather than a fork: no upstream code was
copied, and this repository has no upstream remote and tracks no changes from
either project. The acknowledgement is here because the idea came from
somewhere, not because anything is owed.

## Licensing

MIT. See [`LICENSE`](./LICENSE) for the full terms.
