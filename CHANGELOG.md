# Changelog

## v1.0.0 — 2026-08-01

The first release: a read-only terminal browser for a `.beads` workspace.

`bv` opens the `issues.jsonl` that `br` maintains and gives you three ways to
read it — a list with a detail pane, a parent/child tree, and a kanban board —
with live reload as `br` writes. It never writes anywhere inside `.beads/` and
never spawns a subprocess; `br` remains the only thing that touches your data.
Both invariants are asserted by tests rather than merely intended.

This is 1.0 because the surface is settled, not because the project is large.
Keybindings, flags and the config file are what a user depends on, and they
follow semantic versioning from here: no key is reassigned and no flag removed
in a minor release.

### Views

- **List** with a detail pane, showing description, design, acceptance
  criteria and notes rendered as markdown.
- **Tree** of parent/child relationships, with guides drawn for every
  ancestor, expansion and selection persisted between runs, and cycle and
  orphan handling that keeps a malformed graph readable instead of hanging.
- **Board** grouped into columns by status or assignee, with per-column counts
  and the age of the oldest issue.

All three share one filter, so a query means the same thing in every view.

### Reading a workspace

- Live reload that survives `br`'s atomic writes: the watcher follows the
  directory rather than the file, so a replaced inode does not go unnoticed.
  A decode that fails leaves the previous snapshot on screen rather than
  emptying the view.
- Ready and blocked are derived from the dependency graph to match `br`'s own
  rules, including the epic close-ordering marker, and are checked against
  `br ready` by a test. Two divergences are documented in the README.
- Unknown `status` and `issue_type` values render as written. `bv` renders
  rather than validates, so a workspace `br` itself would reject still opens.
- Yank (`y`) copies the selected issue id over OSC 52 — which is why no
  subprocess is needed, and why it works over SSH.
- Themes follow the terminal's reported background, falling back to a
  background-agnostic palette when nothing is reported, as is usual over SSH
  and inside tmux. `BV_THEME` overrides the choice.

### Scale

Comfortable to roughly 50,000 issues. A keystroke redraws in 1.2 ms at 10,000
issues and 4.7 ms at 50,000; what degrades first is the full re-decode on
every `br` write, not anything you interact with. The README's Scale section
gives the measurements and the reasoning.

### Installing

Prebuilt archives for Linux and macOS on x86-64 and arm64. The Linux builds
are static — `CGO_ENABLED=0` and no cgo dependencies — so one binary runs on
any distribution, Alpine included. There is no Windows build.
