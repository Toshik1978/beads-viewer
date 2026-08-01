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
  ├── listview, treeview, boardview   the three views, as peer packages
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
`SetSize`, `SetSnapshot`, `SetTheme`, `Update`, `View`, `Selected`. The list,
tree and board views each implement it in their own package, and the root
`Model` holds them as `views [3]View` — an array of the interface, not three
named struct fields, let alone three sets of inlined state.

The reason is concrete, not stylistic: the terminal application this project
replaced kept every view's state as fields directly on one root `Model` —
roughly 100 fields and 90 methods by the time this rewrite started, because
each new feature for the list, the tree or the board had nowhere to go except
onto that same struct. `Model` in this codebase is capped at 12 fields by a
test (`TestModelStaysSmall`), and the cap is enforceable precisely because
nothing about the list, tree or board's own internal state is allowed to live
there — it lives inside `listview.Model`, `treeview.Model` and
`boardview.Model`, each in its own package, each free to grow its own fields
without threatening the cap. Adding a fourth view means adding a package and
one array slot, not renegotiating what the root model is allowed to hold.

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
that are easy to get backwards: a parent-child edge never blocks, and a
dependency on an id absent from the snapshot *does* block, because an
unresolvable blocker is not the same thing as a satisfied one. Any future
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
