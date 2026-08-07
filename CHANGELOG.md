# Changelog

What changed in each `bv` release and why. Highlights are written by hand; the
commit lists under them are generated with [git-cliff](https://git-cliff.org)
via `task changelog TAG=vX.Y.Z`.

Versions follow [semver](https://semver.org). Commits follow
[Conventional Commits](https://www.conventionalcommits.org).

---

## v1.5.0 — 2026-08-07

Hiding closed issues — on by default since v1.3.0 — quietly changed the answer
to "is this blocked?". A filter removes issues from the set the views draw
from, and blockedness was being worked out from that narrowed set, where an
issue a filter removed is indistinguishable from one that was never there. An
unresolvable blocker is not a satisfied one, so an issue whose prerequisites
were all finished — and therefore hidden — read as blocked by them. This
release separates the two questions: what you can see is the filter's
business, what is holding up what is the workspace's.

**This moves blocked and ready markers on any workspace where finished work
blocks unfinished work**, which is to say most of them. The status bar's
counts do not move — they were always computed from every issue in the
workspace, which is part of how this went unnoticed. What changes is which
issues carry a blocked or a ready marker, and which board column they sit in.
No key was added, removed or reassigned.

### Blocked and ready describe the workspace, not the filtered view

- An issue whose blockers are all closed is ready, whether or not those
  blockers are on screen. Before, hide-closed on its own was enough to mark it
  blocked; so was a text query that happened not to match the blocker, or a
  label filter, or an explicit status filter.
- The kanban board showed this most plainly, because a derived-blocked issue
  is filed under Blocked whatever its own status field says: a workspace with
  one issue in progress could show an empty In Progress column and no
  indication of where that issue had gone. On the 562-issue workspace this was
  reported from, eleven issues counted as blocked that were not, and nine had
  lost their ready marker.
- The dependency view no longer lists a filtered-out blocker as a missing id —
  the one kind of blocker it flags as unresolvable, and the most alarming
  thing it can say about an issue whose prerequisites are simply done.
- The detail pane and the status bar never had this problem: both already read
  the whole workspace rather than the filtered copy. That is the shape of the
  fix — the rule those two followed at their own call sites is now a property
  of a filtered snapshot itself, so every view gets it without having to
  remember to.

### The board's status lane keeps showing closed work

- A Closed column emptied by hide-closed is a labelled column with nothing in
  it and no explanation on screen. The status lane is exempt from that
  preference now: it shows closed issues in the column that already sets them
  apart from live work, which is the whole of what hiding them would have
  achieved.
- The other three lanes still honour it. Priority, assignee and type have no
  Closed column, so a closed P1 would sit among the open P1s — exactly the
  mixing the preference exists to prevent. `s` cycles the lanes, and closed
  cards come and go with them.
- Every other filter still reaches the board unchanged: a query, a label or a
  status filter narrows it the same way it narrows the list and the tree. Only
  hide-closed depends on the lane.
- The status bar's `(N hidden)` goes quiet while that lane is on screen,
  rather than counting issues as hidden beside a column that is showing them.

### Features

- feat(board): exempt the status lane from hide-closed ([3d69c5f](https://github.com/Toshik1978/beads-viewer/commit/3d69c5f35941340072bbb18852a4f5f06cbccfd9))

### Bug Fixes

- fix: derive blockedness from the workspace, not the filtered view ([0708bbb](https://github.com/Toshik1978/beads-viewer/commit/0708bbbcf41fb1575f027a80867d09b8f11e40cf))

### Others

- docs(readme): record the board's hide-closed exemption ([bd362ec](https://github.com/Toshik1978/beads-viewer/commit/bd362ec2854b447611cf02cba6acc194d5882d58))

---

## v1.4.0 — 2026-08-07

`br` 1.4.0 changed both what it writes and what it prints, and `bv` had
drifted from it. Nothing ever failed — `bv` ignores fields it does not
recognise, which is precisely why the drift went unnoticed: the edges it had
stopped understanding simply did not appear. This release re-syncs the two.
Five kinds of dependency edge that were being dropped now show up, an issue
`br` has renamed can still be found under the id you remember, and three
fields `br` no longer writes are gone from the rules that consulted them.

**This changes what counts as ready, but only on older workspaces.** `br` no
longer emits `pinned`, `ephemeral` or `is_template`, so `bv` no longer reads
them. On a workspace `br` 1.4.0 wrote, nothing moves — those fields are never
set. On an `issues.jsonl` from an earlier `br`, or one hand-edited to carry
them, issues that used to be held out of the ready count are now included. No
key was added, removed or reassigned.

### Five dependency types that were invisible

- `br` defines eleven kinds of dependency edge; `bv` knew six. Edges of type
  `relates-to`, `duplicates`, `supersedes`, `caused-by` and `replies-to` were
  not merely unlabelled — they were dropped outright, so an issue carrying one
  showed no trace of it anywhere in the dependency view. They now appear in
  the related column beside `related` and `discovered-from`.
- What blocks an issue is unchanged. `blocks`, `conditional-blocks` and
  `waits-for` are still the only edges that hold work up — checked against
  `br` 1.4.0 rather than assumed — so no issue's blocked or ready state moves
  on account of the five new types.

### Relation labels now read from the end you are on

- Most of these edges are asymmetric, and the card now says which side of one
  you are looking at: *supersedes* on the issue that replaces, *superseded by*
  on the issue replaced. Likewise *duplicates* / *duplicated by*, *caused by* /
  *caused*, and *replies to* / *reply*. `relates-to` is symmetric and reads
  *related* from both ends.
- This also corrects `discovered-from`, wrong since the dependency view
  shipped: both ends read *discovered from*, including the issue that was
  discovered from rather than the one that did the discovering. The far end
  now reads *led to*.
- When two issues each declare an edge about the other, each card names the
  edge that issue itself declared — unless it declared only the generic
  `related`, in which case the more specific claim from the other side wins.

### Renamed issues stay findable

- `br` renames an issue when it gains a parent: `bv-k7g` becomes `bv-vz2.5`,
  and a deletion marker stays behind at the old id. The old id therefore
  outlives the thing it named, in commit messages, in notes, and in other
  issues' dependency rows.
- Typing an old id into the filter finds the issue under its current one, and
  looking one up lands on the live issue rather than the marker `br` parked
  at that id.
- A dependency edge still naming an old id is followed too. The renamed issue
  is treated as a real blocker rather than a satisfied one, so the "blocks"
  and "blocked by" columns no longer disagree about the same edge depending on
  which issue you were looking at when you asked.

### Older workspaces still open

- `bv` reads what `br` wrote, whichever version wrote it: fields 1.4.0 added
  are ignored, fields it dropped are simply not consulted, and a status or
  issue type `bv` has never heard of is still rendered as written. A workspace
  from an earlier `br` opens exactly as it did, apart from the readiness
  change noted above.

### Features

- feat(beads): index the five dependency types br 1.4.0 added ([d6bb635](https://github.com/Toshik1978/beads-viewer/commit/d6bb635cd6812985ebe7319ad785a2ac286025e1))
- feat(depsview): label relation edges by direction ([d228e28](https://github.com/Toshik1978/beads-viewer/commit/d228e28563038807eb6e8c44e27ce5ac50f26667))
- feat(beads): resolve ids an issue used to carry ([105d964](https://github.com/Toshik1978/beads-viewer/commit/105d964551bc2f12f9cebce4c1c246b9cc9400df))

### Bug Fixes

- fix(beads): make RelationTo symmetric when both ends claim a specific type ([afe2fe3](https://github.com/Toshik1978/beads-viewer/commit/afe2fe3ff41ab43a75b726503f6202244ac1308e))
- fix: resolve former ids through the relation and blocking derivations ([541fce9](https://github.com/Toshik1978/beads-viewer/commit/541fce917e5e9622e24595fb875a777d9f3fcde1))

### Others

- docs(beads): spec and plan the br 1.4.0 contract adaptation ([10a3f71](https://github.com/Toshik1978/beads-viewer/commit/10a3f719c4efdfc8249e729bd98212ba28002e8a))
- refactor(depsview): split rendering out of depsview.go ([d6598e1](https://github.com/Toshik1978/beads-viewer/commit/d6598e1a89e5f2583ba7a921cc379a3944a9e9fa))
- docs(depsview): fix depsview.go's stale file header ([1a1c8a4](https://github.com/Toshik1978/beads-viewer/commit/1a1c8a4ba6fd5d7b64c75b3694190f5a62be380b))
- refactor(beads): drop the fields br 1.4.0 no longer emits ([5e84130](https://github.com/Toshik1978/beads-viewer/commit/5e84130f2381f3984ec9b9e3482fa7bf319c7688))

---

## v1.3.0 — 2026-08-03

A fourth view: a dependency board that answers, for one issue, what is
holding it up and what is waiting on it. Alongside it, a selection that
follows you from pane to pane instead of resetting.

**This release changes a default.** Closed issues are now hidden unless you
ask for them. `c` still toggles it, and `--hide-closed=false`, `hide_closed:
false` or `BV_HIDE_CLOSED=false` start a session with them shown. No key was
reassigned; two were added and one gained a second meaning, all listed at the
end.

### Closed issues are hidden by default

- A `bv` started with no flags, no environment and no config file opens with
  terminal-status issues hidden, in every view. One filter feeds all four
  panes, so this is one narrowing rather than four agreeing ones.
- The status bar says so on every frame, without a keypress: `(N hidden)`
  beside the view name and `hide closed` among the filter criteria. That is
  the only signal that issues are missing from what you are looking at, and
  it is what keeps `c` discoverable for someone who never read these notes.
- Deletion markers are unaffected — they were already hidden and still are,
  independently of this toggle. The hidden count subtracts them, so it
  reports what hide-closed actually hides rather than inflating itself with
  records the toggle never governed.

### The dependency view

- `4` from any view opens it on the issue you were reading. Four columns:
  what blocks it, the issue itself, what it blocks, and what it is merely
  related to. It runs full screen, as the board does — four columns squeezed
  into a split terminal would each be narrower than the board's own minimum
  column width.
- The left column answers more than one question, because "what is blocking
  this" has more than one answer. A card there can be an ordinary unfinished
  dependency; an id this workspace has no issue for, marked *not in
  workspace*; an ancestor marked *via parent*, when the thing holding you up
  is something a parent is waiting on rather than anything about this issue;
  or, for an epic, a still-open child. Each is labelled, because each needs a
  different response — and because an issue's own dependency rows are only
  one of the four, a view that listed just those would say "nothing is
  blocking this" about an issue the status bar counts as blocked.
- `enter` makes the highlighted card the new subject and rebuilds the columns
  around it, so a chain of blockers can be walked one hop at a time.
  `backspace` walks back. Leaving with `1`, `2` or `3` takes the issue you
  ended on with you.
- The last column carries `related` and `discovered-from` edges in both
  directions, each card tagged with the kind that produced it. Direction is a
  fact about which record holds the row, not about which issue is the useful
  context.
- Parent and child edges are deliberately absent. That hierarchy is the tree
  view's subject, and repeating it here would make the outer columns mean two
  things at once.

### Views open where you left off

- Switching views puts the cursor on the issue you were reading, in every
  direction. Each view used to keep its own cursor, so a switch landed
  wherever that pane was last left — often the top, or a row the tree
  restored from a previous session.
- The tree expands whatever collapsed ancestors are hiding the row, since a
  row that does not exist cannot be selected. It only ever expands: tree
  expansion is persisted between runs, so collapsing something here to
  satisfy a view switch would discard a choice you made in an earlier
  session.
- The dependency view re-roots rather than moving a cursor — that is what
  makes `4` land on the right subject in the first place.

### Smaller things

- An issue that declares itself its own parent now appears as a root instead
  of disappearing. It was previously excluded from the roots and reachable
  only as its own child, which meant it never appeared in the tree at all
  despite being in the file. `bv` renders rather than validates, so malformed
  data should be visible, not hidden.
- The detail pane's "Blocks" section reads an index instead of scanning every
  issue in the workspace on every frame. Same issues, same order; it also no
  longer lists an issue twice when that issue declares two blocking edges on
  the same target.

### Keys that changed

| Key | Before | Now |
|---|---|---|
| `4` | unbound | dependency view |
| `backspace` | unbound | back to the previous subject (deps) |
| `enter` | open in list (board) | open in list (board), re-root (deps) |

`?` now lists the filtering keys under **Global** rather than in a section of
their own. `/`, `c` and `esc` are unchanged — only where they are printed
moved, so the overlay still fits an 80×24 terminal now that the views section
carries two more bindings.

### Features

- feat(config): hide closed issues by default ([52a601e](https://github.com/Toshik1978/beads-viewer/commit/52a601e281664bef7e42d784d4f3d404248e16d2))
- feat(treeview): reveal an id by expanding the ancestors that hide it ([94edadb](https://github.com/Toshik1978/beads-viewer/commit/94edadb5a53224a6aace44cac308a5339da67a23))
- feat(beads): index dependents and relatives on the snapshot ([b29a99e](https://github.com/Toshik1978/beads-viewer/commit/b29a99e382300f11d7dc974e7d7b286de27ef67c))
- feat(tui): carry the selected issue across a view switch ([7e75baa](https://github.com/Toshik1978/beads-viewer/commit/7e75baa75ca0fdb08e4cd042ac0dac3549c59aba))
- feat(depsview): build the four dependency columns ([5ef26c5](https://github.com/Toshik1978/beads-viewer/commit/5ef26c5d2d0d0c387f8a848c0273c05461c99a1d))
- feat(depsview): render the dependency columns ([d074547](https://github.com/Toshik1978/beads-viewer/commit/d0745478371324bfdc0675003792c4cd87772b9c))
- feat(depsview): navigate, re-root and walk back ([1313911](https://github.com/Toshik1978/beads-viewer/commit/131391102203764963c71753929b06ed4dd27845))
- feat(tui): add the dependency view as the fourth pane ([fd35dc1](https://github.com/Toshik1978/beads-viewer/commit/fd35dc1d48151653dae3fed95acde23ca9dab4b5))

### Bug Fixes

- fix(config): make HideClosed override test discriminate the regression ([bf6c4fb](https://github.com/Toshik1978/beads-viewer/commit/bf6c4fbf9902b13a67bbad365a783ad51e58f40f))
- fix(beads): dedupe Dependents and pin the self-parenting root case ([90c45d3](https://github.com/Toshik1978/beads-viewer/commit/90c45d370e57dc2787de124ad4e220b0faf398c5))
- fix(depsview): resolve duplicate ids like Snapshot's byID, pin discovered-from precedence ([9b81737](https://github.com/Toshik1978/beads-viewer/commit/9b817376c570326c7c2e0c2ba4c21ceebce82ef6))
- fix(depsview): resolve duplicate ids via Snapshot.ByID, not a sorted-slice scan ([83f34a0](https://github.com/Toshik1978/beads-viewer/commit/83f34a032a79ff67a76a3ba7e3f8e45643915d3f))
- fix(depsview): charge each entry's real row cost, not a fixed one ([bb05204](https://github.com/Toshik1978/beads-viewer/commit/bb05204a110e353cf2bd6388fe059907bfb30658))
- fix(depsview): make the window's start agree with its own cost model ([bf2b8c6](https://github.com/Toshik1978/beads-viewer/commit/bf2b8c617cbee5a43134b32019d52f4dde0cc8aa))
- fix(depsview): seed a focus so the view never opens on nothing ([5c3d01b](https://github.com/Toshik1978/beads-viewer/commit/5c3d01b42db6c4e2e3b7db1f91ecd00e1f112c49))
- fix(depsview): name a focus that has left the snapshot, and dedupe blocked-by ([9f8876f](https://github.com/Toshik1978/beads-viewer/commit/9f8876f00e591ccc1c01ec84ba2a522a68c8e1b1))
- fix(depsview): clear history only on a re-root from outside the view ([672fb3f](https://github.com/Toshik1978/beads-viewer/commit/672fb3fbeec45230578101e393645568bbb9acc0))
- fix(depsview): count only entries hidden below the window ([97f9941](https://github.com/Toshik1978/beads-viewer/commit/97f9941e1d16e14766479ccdb2e35bf977a1072a))

### Others

- docs(changelog): separate versions with a rule, and add a preamble ([0e7ae5b](https://github.com/Toshik1978/beads-viewer/commit/0e7ae5b7ea2a2726fa5854a74f89f4efd7490ccb))
- docs(beads): spec and plan for the dependency board epic ([6fa139c](https://github.com/Toshik1978/beads-viewer/commit/6fa139cab77ede8ddebb1e7c7c62c04d0a5e426f))
- docs(beads): resolve two pre-flight conflicts in the epic plan ([c1b0f32](https://github.com/Toshik1978/beads-viewer/commit/c1b0f3246a8636c070265f0925522aa638fe20c3))
- refactor(tui): extract card rendering into cardfmt ([ef96135](https://github.com/Toshik1978/beads-viewer/commit/ef961359f2f7a9e084ac2747a020671f1939f710))
- docs(cardfmt): fix stale renderCard reference left by the card move ([7b968cd](https://github.com/Toshik1978/beads-viewer/commit/7b968cdbab08022ea64f7d3f69f19d3f8127c847))
- refactor(detail): read the blocks section from the reverse index ([30fa552](https://github.com/Toshik1978/beads-viewer/commit/30fa552a79c4d3e876fdfcdeba57b72ba1416cae))
- docs(beads): correct the epic's line-cap target from 7,600 to 8,200 ([fb5365d](https://github.com/Toshik1978/beads-viewer/commit/fb5365dc56ee41eae6c037b1306c7e87de66f0c2))
- docs(beads): give task 6.1 the keys.go split it now needs ([c174e9c](https://github.com/Toshik1978/beads-viewer/commit/c174e9c7cfb8f31602c27aa5182eaa85ad32ea4a))
- docs(beads): set the epic's final line cap at 8,600 ([14e34b2](https://github.com/Toshik1978/beads-viewer/commit/14e34b2d889c48ad4b621b13be4c53ce5726a10e))
- docs: document the dependency view and the hide-closed default ([dd254c3](https://github.com/Toshik1978/beads-viewer/commit/dd254c3e427593c6937bc4cf2ea75187de75ac4b))
- docs: catch up the View interface docs and deps-view nuances ([97ade2b](https://github.com/Toshik1978/beads-viewer/commit/97ade2bad2741d5a70315b0ec7b945f2166ffedf))

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
