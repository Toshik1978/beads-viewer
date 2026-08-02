# Changelog

What changed in each `bv` release and why. Highlights are written by hand; the
commit lists under them are generated with [git-cliff](https://git-cliff.org)
via `task changelog TAG=vX.Y.Z`.

Versions follow [semver](https://semver.org). Commits follow
[Conventional Commits](https://www.conventionalcommits.org).

---

## v1.2.0 — 2026-08-01

Tree rows now read the way list rows have since v1.1.0, and the board's `left`
and `right` land only where there is something to land on. No key changed in
this release.

### Tree rows

- Rows carry what the list's carry: the type glyph coloured by issue type, the
  priority by level, a status column coloured by status, and a right-aligned
  relative age. Same colours and same widths as the list, so one issue reads
  the same in either pane — the two panes now share a single implementation
  rather than agreeing by convention.
- The tree's indent grows with depth, so its columns give way in a different
  order than the list's: the age first, then the status, then the id, and only
  then does the title truncate. The title is the one column that identifies a
  row, so it is the one that survives.
- A selected row is still a single highlight, and an ancestor kept only so a
  matching descendant stays reachable is still muted as a whole. Colouring
  those per column would contradict what the highlight and the muting each say
  about the row.

### Board

- `left` and `right` move to the nearest column holding at least one issue,
  and stay put when there is none in that direction. They used to step one
  column at a time and park on empty ones, which emptied the selection and
  made the key look like it had done nothing.
- Empty columns still render, with their header, count and stats. Skipping is
  a navigation rule, not a visibility one: a status with nothing in it is
  worth seeing on a board used for triage.
- A reload or a regrouping that empties the column your cursor is already on
  still resolves to something valid. That is a separate rule from the
  skipping, and deliberately so — otherwise a write by `br` could strand the
  cursor with nothing selected.

### Features

- feat(tree): colour rows, and give them a status and age column ([b03b0d4](https://github.com/Toshik1978/beads-viewer/commit/b03b0d4bcdbb5d4fa43cabd2f1136b3cffe3320e))
- feat(board): skip empty columns when moving left or right ([8ebac57](https://github.com/Toshik1978/beads-viewer/commit/8ebac574aac325e9893be3229ddb47d6b13183a0))

### Others

- refactor(tui): extract the shared row formatter into rowfmt ([a0b053b](https://github.com/Toshik1978/beads-viewer/commit/a0b053b2c2eceedd7c04f837d98d79937893dd7c))
- refactor(tui): give rowfmt the status column width, and name it in the architecture ([766b0ad](https://github.com/Toshik1978/beads-viewer/commit/766b0ad5e646a4d42b67c4e2716f8f0125dddc24))

---

## v1.1.0 — 2026-08-01

Panes now carry a frame and a focus model, list rows carry colour and a date,
the board runs full screen, and the filter applies as you type. One bug fix
lands alongside them: `bv` was under-reporting blocked issues relative to `br`.

**This release reassigns `tab`.** On the board it used to cycle the swimlane
grouping; it now moves focus between the active view and the detail pane, and
swimlanes move to `s`. Every changed key is listed at the end. It stays a
minor release — see v1.0.0 on why the version number does not move for a
rebound key.

### Panes and focus

- Each pane draws a frame, and the frame's colour is what tells you which pane
  has focus. `tab` moves focus between the active view and the detail pane.
  With the detail pane focused the navigation keys scroll it and the view's
  cursor stays where it was; `pgup` and `pgdown` reach it under either focus,
  as before.
- Below the width or height a frame fits in, panes render unframed rather than
  spending a small terminal on decoration.

### List rows

- The type glyph, the priority and the status each render in their own colour.
  A selected row keeps its single-style highlight: a per-column colour inside
  the selection would emit a reset partway along the line and drop the
  highlight for the rest of the row.
- Rows carry a relative updated column — `2h`, `3d`, `11mo` — right-aligned in
  at most four cells. It is the first column dropped when the pane is tight,
  before labels and before the title truncates. A record with no `updated_at`
  shows nothing rather than an age measured from year one.

### Board

- The board takes the whole body. Its columns are the content, and a detail
  pane describing one card was not worth the rest of the terminal.
- `enter` on a card opens it in the list, which is how a card reaches a detail
  pane now that the board has none. An empty column, or `enter` pressed
  anywhere else, does nothing.
- `s` cycles the swimlane grouping, `tab` having become the focus key.

### Filtering

- Typing narrows every view about 150 ms after the last keystroke instead of
  waiting for `enter`. `enter` still commits and closes the box.
- `esc` inside the box restores the filter as it was when the box opened,
  rather than discarding an edit that has already been applied — including
  when you opened the box on a filter that was already active.
- `esc` with the box closed clears the query — text, status and labels — and
  leaves hide-closed alone, since `c` is that toggle's own key. It used to
  zero the whole filter, which silently undid `--hide-closed`.
- `c` hides closed issues in all three views rather than only the tree, so a
  query means the same thing in every pane. It is no longer persisted between
  runs: `--hide-closed` and `hide_closed` in the config file are what start a
  session with it on.

### Blocked issues

- Blockedness now inherits down parent-child edges, matching `br`: an issue
  whose parent is blocked is itself blocked. `bv` previously reported fewer
  blocked issues than `br blocked` did on the same workspace. Only
  dependency-derived blockedness inherits — an epic waiting on an open child
  does not make its siblings blocked.
- Where an issue is blocked with no blocker to name, the detail pane now says
  which of three reasons applies: an ancestor further up the chain, an epic
  waiting on an open child, or a blocker id absent from the workspace. It used
  to assert the third in all three cases.

### Keys that changed

| Key | Before | Now |
|---|---|---|
| `tab` | cycle swimlane (board) | focus list / detail |
| `s` | unbound | cycle swimlane (board) |
| `enter` | unbound | open in list (board) |
| `c` | hide closed (tree only) | hide closed (every view) |
| `esc` | clear the whole filter | clear the query; hide-closed survives |

A tree state file written by v1.0.0 still loads. Expansion and selection are
still restored; the hide-closed value in it is ignored rather than migrated,
since restoring a tree-local narrowing as a workspace-wide one would hide
issues in two panes you never set it on.

### Features

- feat(tui): frame each pane ([6e95247](https://github.com/Toshik1978/beads-viewer/commit/6e9524795a1b9e5372a012504b2c8c84c4961654))
- feat(tui): focus the list or the detail pane with tab ([41fad4e](https://github.com/Toshik1978/beads-viewer/commit/41fad4ed574b7548e7ac1f2f03a3ec750b64ec5c))
- feat(tui): route navigation keys to the focused pane ([23b00aa](https://github.com/Toshik1978/beads-viewer/commit/23b00aa6e0572d5ab8e6bce119689f44d08e82eb))
- feat(board): cycle swimlanes with s, freeing tab for focus ([daa4462](https://github.com/Toshik1978/beads-viewer/commit/daa4462d2114ae91c4a5ae9baf9c76d8da6d348f))
- feat(theme): style issue types and statuses ([c50149a](https://github.com/Toshik1978/beads-viewer/commit/c50149a6a57a02fdffa10f58aa60c3a3b7b1bcae))
- feat(uitext): add a compact relative-age helper ([621ecc9](https://github.com/Toshik1978/beads-viewer/commit/621ecc96b0ed0da38315c3d936848bd01de2696d))
- feat(list): colour rows column by column and show a relative age ([58add2e](https://github.com/Toshik1978/beads-viewer/commit/58add2ececf73b8368906456d9f57e3b1be1fde8))
- feat(board): give the board the whole body ([d85b6ba](https://github.com/Toshik1978/beads-viewer/commit/d85b6ba264d6d13cb9444d4a5a91f41acd7d90ac))
- feat(board): open the selected card in the list with enter ([9b4c376](https://github.com/Toshik1978/beads-viewer/commit/9b4c37693c37a8b699d693fb6ff1a1c72cd5c8e8))
- feat(tui): make hide-closed a global filter ([2782652](https://github.com/Toshik1978/beads-viewer/commit/2782652f5176bf04ee1bf14e65131983587c995a))
- feat(tui): apply the filter as you type, and restore on escape ([ed9c191](https://github.com/Toshik1978/beads-viewer/commit/ed9c1918ef44824e31c850f59728ca2d9fdaf214))

### Bug Fixes

- fix(beads): inherit blockedness from a dep-blocked ancestor ([7a087c1](https://github.com/Toshik1978/beads-viewer/commit/7a087c1b8aad00bd585a47cdbf1139b2e79fe65f))
- fix: address the whole-branch review ([a3331bc](https://github.com/Toshik1978/beads-viewer/commit/a3331bce44939de094f6a8396591c2c88b03897b))

### Others

- docs: correct the claim that this repository has no remote ([e57c5bc](https://github.com/Toshik1978/beads-viewer/commit/e57c5bce35bca020eac213c22befc61001e31a6f))

---

## v1.0.0 — 2026-08-01

The first release: a read-only terminal browser for a `.beads` workspace.

`bv` opens the `issues.jsonl` that `br` maintains and gives you three ways to
read it — a list with a detail pane, a parent/child tree, and a kanban board —
with live reload as `br` writes. It never writes anywhere inside `.beads/` and
never spawns a subprocess; `br` remains the only thing that touches your data.
Both invariants are asserted by tests rather than merely intended.

This is 1.0 because the surface is settled, not because the project is large —
and it stays on 1.x by choice. The honest alternative was 0.x, which would say
"not finished yet" about something that is finished.

Keybindings, flags and the config file are what you depend on, and they are
what these notes track. But `bv` is a reader: it stores nothing, exposes no
API, and nothing is built against it. A reassigned key costs you a moment's
surprise and a glance at `?` — not a broken build, not a lockfile to unpin,
not a migration to run. Spending a major version on that would burn the one
signal a major version carries on something a keystroke recovers from.

So a minor release here may reassign a key or retire a flag. When one does, it
says so in its first paragraph and lists every change in a table. Being told
before you upgrade is the guarantee that is worth anything at this size.

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

---
