# CLAUDE.md

Guidance for an agent working *on* this repository — not for a user of `bv`.
See [`README.md`](./README.md) for that.

## The gate

`task check` runs, in order: `format:check` → `lint` → `test`. It must exit 0
before any commit. CI runs the same checks on Linux and macOS, though not via
this target — `golangci-lint-action` covers lint and formatting together, and
`release.yml` is what invokes `task check` verbatim.

`format:check` is redundant with `lint`: golangci-lint v2 runs the configured
formatters as part of `run`, verified against `gofumpt`, `gci` and `golines`
violations, each of which fails `run` on its own. It stays first in `check`
because it is far faster and prints a diff instead of a one-line complaint.

**Run it again after `git add`, not only before.** The `internal/licensing`
sweep (see Licensing below) enumerates *git-tracked* files. A new file that is
not yet staged is invisible to it: the gate goes green, the commit lands, and
the very next run goes red on the file just committed. Stage first, then run
the gate, then commit.

## CI and local hooks

`pre-commit install` wires three stages — the config declares the hook types,
so that one command is enough:

| Stage | Runs | Cost |
|---|---|---|
| `commit-msg` | Conventional Commits | instant |
| `pre-commit` | `golangci-lint fmt --diff` | seconds |
| `pre-push` | `task check` | seconds |

The full gate sits at pre-push because the licensing sweep enumerates
*git-tracked* files: pre-push is the first stage where its verdict matches
what a reviewer will see.

Four workflows:

- **`ci.yml`** — the gate on Linux and macOS, plus `release-config` (so a
  broken release config fails a PR, not a public tag), an install smoke test
  that actually *runs* the binary, `govulncheck`, and coverage. Coverage uses
  `-coverpkg=./...` and is gated at **80%** against a current 90.8% — a floor
  that far below fires only on a collapse, rather than policing ordinary
  movement. The gate runs *after* the badge is published; failing first would
  leave a broken `main` showing the last green run's badge.
- **`docs.yml`** — the licensing sweep for prose-only changes.
- **`commit-lint.yml`** — Conventional Commits. **Must never gain a path
  filter**: a docs-only change still has a subject, and `.cliff.toml` builds
  the CHANGELOG from it.
- **`release.yml`** — tag-triggered build, ending in a job that reads the
  published release back and fails on an empty body.

**`ci.yml`'s `paths-ignore` and `docs.yml`'s `paths` are complements and must
be edited together.** A file listed in one and missing from the other is a file
nothing checks. `CHANGELOG.md` is deliberately in neither — `release-config`
reads it. The lists are enumerated file by file rather than globbed with
`**.md`, because most markdown here *is* a CI input: the licensing sweep reads
every tracked file.

Every action is pinned to a moving major tag (`@v7`), never an exact `x.y.z`.
`.github/dependabot.yml` ignores minor and patch updates for actions because
Dependabot cannot tell an alias from a version and would otherwise propose
freezing — or silently reversing — a self-updating pin.

## Dependencies

At most **9** direct dependencies in `go.mod`, exactly:

```
charm.land/bubbletea/v2, charm.land/bubbles/v2, charm.land/lipgloss/v2,
charm.land/glamour/v2, github.com/charmbracelet/x/ansi,
github.com/fsnotify/fsnotify, github.com/spf13/cobra,
github.com/goccy/go-yaml, github.com/stretchr/testify (tests only)
```

Adding any other direct dependency requires explicit approval: state the
package, what it solves, and why the standard library is insufficient. Do not
add it until approved. No dependency may have a release older than
2025-01-01, and no pseudo-versions.

Two API facts worth remembering: only bubbletea, bubbles, lipgloss and
glamour use the `charm.land` vanity path — the `x/` repositories stay at
`github.com/charmbracelet/x/...`. And glamour v2 removed `WithAutoStyle`; use
`glamour.WithStandardStyle("dark")` or `("light")`.

## Size tripwires

These exist because the predecessor this project replaced had a `Model`
struct with roughly 100 fields and 90 methods — a single object that had
absorbed the state of every view, every overlay and every piece of UI
bookkeeping until nothing about it was reviewable in isolation. Every limit
below is a proxy for catching that regression again, not a target to
optimize for its own sake:

- Non-test Go: at most **8,600 lines** total (raised six times: from 6,000
  to 6,300 during Task 7.1, for a correctness fix; from 6,300 to 6,800 for the
  six interface features under the border/focus/board epic; from 6,800 to
  6,900 for `bv-wkx`, the unplanned blockedness fix, at 162 lines; from 6,900
  to 7,000 to restore headroom; from 7,000 to 7,100 for the row/tree/board
  epic's first task, which extracts the shared row formatter into its own
  package — a new package costs roughly 25 lines of declaration, doc comment
  and imports beyond the 50 it receives, and the epic's tree and board work
  adds about 50 more; from 7,100 to 8,600 for the dependency-view epic, which
  added a fourth view package (`depsview`), a second shared card-formatting
  package (`cardfmt`, alongside `rowfmt`) and the snapshot's reverse
  dependency index — see the commit history for all six decisions recorded
  in full. The first raise is the precedent for the third: a correctness fix
  nobody budgeted for is exactly the case this cap should yield to, and the
  epic's own six features landed at 6,720, inside their budget. The fourth is
  a different lesson: the third raise was landed on exactly, and a cap with
  zero headroom stops measuring the codebase and starts editing its prose —
  comments were compressed to fit the number rather than to read better.
  Leave headroom, and when this cap next binds, raise it rather than
  shortening an explanation. The sixth raise is the fourth's lesson applied
  rather than merely repeated, and it is worth recording exactly how it was
  arrived at, because the estimate was wrong twice before it was right: the
  epic's plan targeted 7,600; that was corrected to 8,200 mid-epic, once
  `depsview/deps.go` alone came in at 194 lines against a roughly 300-line
  estimate for the *whole* view; the epic then actually finished at
  **8,177** non-test lines, which against 8,200 would have left only 23 lines
  of headroom — the zero-slack case the fourth raise exists to warn about.
  8,600 is the third figure, and the first with real slack. Both of the
  earlier projections were low by roughly 290 lines, each time because a new
  view package cost substantially more than a line-count estimate
  anticipated — a lesson worth carrying forward rather than treating as
  settled: the next person estimating a view package's size should assume it
  will run over by more than they think, not budget to the estimate. The
  structural limits below are the ones that actually detect the regression
  this cap is a proxy for, and they were satisfied with margin at each
  raise).
- `cmd/bv/main.go`: at most **150 lines**.
- No non-test source file exceeds **500 lines**. Test files are governed
  instead by one file per suite — a test file that maps to exactly one
  `suite.Suite` has not accumulated unrelated responsibilities however long
  it grows, so the line cap does not apply to it.
- No behavioural type has more than **20 fields**. `beads.Issue` is exempt:
  it is the JSONL decode target, and its field count mirrors `br`'s own wire
  format rather than anything this project chose — currently 28.
- At most **5 exported type declarations per file** (`revive`'s
  `max-public-structs`, enforced with `enable-all-rules: true`). This counts
  every exported type declaration, not only structs. When a file would
  declare a sixth, split it by responsibility; never raise the cap or add a
  `nolint`.
- Do not add a `//nolint` directive that turns out to be unnecessary:
  `nolintlint` fails the build on an unused directive, exactly as surely as
  the issue it was meant to silence would have.

The root `Model` (`internal/tui/app.go`) is the tripwire that matters most: it
is capped at 12 fields by a test, and every view's own state lives behind the
`View` interface in its own package instead of on that struct. New per-view
state belongs in a view package, not on `Model`.

## Code style

This repository's `.golangci.yml` is the shared lint gate, unmodified except
for the `gci` and `gofumpt` module prefix. Consequences worth knowing before
writing code against it:

- `gochecknoglobals` / `gochecknoinits`: no package-level `var`, no `init()`.
  Construct and inject instead.
- `funcorder`: exported methods before unexported; constructors first.
- `revive` function-length: at most 40 statements / 60 lines per function
  (test files excluded).
- `gocyclo` / `cyclop`: cyclomatic complexity at most 10.
- `lll` / `golines`: 120 columns (test files excluded).
- `wrapcheck`: every error crossing a package boundary is wrapped, e.g.
  `fmt.Errorf("decode issues.jsonl: %w", err)`.
- `ireturn`: return concrete types, not interfaces. Accepting interfaces is
  fine.
- `sloglint`: `no-global: all`, lowercased messages, snake_case keys.
- `godot`: comments end with a period.
- Formatters: `gofumpt`, `gci` (sections: standard, default,
  `prefix(github.com/Toshik1978/beads-viewer)`), `golines` at 120 columns.

Write comments that explain *why*, not what the code already says.

## Testing

Three rules, non-negotiable, taken from this repository's own test suites —
look at any `*_test.go` file for a worked example:

1. **One entry point per package** — exactly one top-level
   `func Test<Package>(t *testing.T)`. No other top-level `Test*` function may
   exist in the package.
2. **The entry point only wires suites** — solely `suite.Run(t, new(...))`
   calls, one per `suite.Suite`, with no test logic in the entry point itself.
3. **All real tests are suite methods** — every assertion lives on a
   `suite.Suite` using suite assertion methods (`s.Equal`, `s.NoError`,
   `s.Require().NoError`, `s.Contains`), never a bare `func TestX(t
   *testing.T)` with `require.X(t, …)`.

Table-driven subtests use `s.Run(tc.name, func() { … })`.

Golden view tests store the ANSI-stripped frame (`ansi.Strip`) — an
escape-laden golden is unreviewable, and one nobody can read gets regenerated
on sight rather than checked, which is worse than no golden at all. Styling is
asserted separately from content, at minimum that a selected row's rendering
differs from an unselected one.

## Behavioural invariants

`bv` is **read-only with respect to `.beads/`**: it never writes anywhere
inside that directory and never spawns a subprocess. Both are asserted by
tests in `cmd/bv/main_test.go` (`TestNeverWritesInsideBeads`,
`TestNoSubprocessInTheBinary`). The latter is a source check, not a runtime
one: it runs `go list -deps ./...` and fails if any package under this module
other than `internal/licensing` (test-support code that shells out to `git`
and never reaches a `bv` run) imports `os/exec`. Any change that would make
either test fail is out of scope for this project, not a bug to work around.

That sentence describes what is *enforced*, which is narrower than what it
sounds like. `go list -deps` reports a package's non-test imports only, so the
check governs the shipped binary and nothing else: `cmd/bv/main_test.go` and
`internal/beads/derive_test.go` both import `os/exec` to shell out to `br`,
and both skip when `br` is absent. Neither is a violation, and neither would
be caught if it became one — a test binary is free to spawn processes; `bv`
is not.

This is narrower than "writes nothing": `internal/tui/treeview/state.go`
persists tree expansion and selection to
`$XDG_STATE_HOME/bv/trees/` (falling back to `~/.local/state`) on exit. That
path is outside `.beads/` and is not a violation of the invariant above — do
not "fix" it by removing the write. The test suite redirects `XDG_STATE_HOME`
in a temp directory precisely because this write is expected.

`bv` **renders rather than validates**: unknown `status` and `issue_type`
values are kept as raw strings, unknown JSON fields are ignored, and labels
are never validated. A workspace `br` itself would reject must still open
here.

## Licensing

- `LICENSE` is plain MIT, copyright the author alone. It previously carried an
  OpenAI/Anthropic rider and a second copyright line, both inherited from the
  viewer that inspired this one on the premise that this code was a derivative
  work. It is not: no upstream file ever entered this repository's history,
  and of 1,097 distinctive non-comment lines only 46 also appear upstream —
  every one of them a Go idiom, a library call form, or a field tag dictated
  by `br`'s wire format. Do not reintroduce either; a test asserts their
  absence, not merely MIT's presence.
- Never name an upstream project in a git-tracked file other than `README.md`
  (its Acknowledgements section) and `internal/licensing`'s own test, which
  enumerates the disallowed strings and is exempted from itself by name.
- Never name a filesystem path on someone else's machine in **any** git-tracked
  file, `README.md` included. That sweep is stricter than the name sweep on
  purpose: naming a project is a courtesy, naming a directory on a laptop is a
  leak.
- Run `go test ./internal/licensing/` after staging any new tracked file, not
  only after editing an existing one; the sweep only sees what git tracks, so
  a file that is written but not yet `git add`ed will pass today and fail on
  the very next run.

## Commit conventions

- Conventional commits: `feat:`, `fix:`, `test:`, `chore:`, `docs:`,
  `refactor:`.
- **No `Co-Authored-By`, no `Claude-Session`, no AI-attribution trailer of any
  kind.** Subject and body only.
- Feature branches use the `feature/` prefix — never `feat/` or `feat-`.
- Do not skip hooks; never pass `--no-verify`.

## Release process

See [`docs/RELEASING.md`](./docs/RELEASING.md) for the full sequence. In
short: `task changelog` generates the commit list, the release prose is
hand-written above it, `task release:verify` checks the tag against the
changelog before it is created, and the tag-triggered workflow builds and
publishes the archives. Do not run `git push` or `gh` in this repository. An
`origin` remote does exist — this sentence previously said it did not, which
went stale — but `docs/RELEASING.md` Step 1 is explicit that the first tag is
created only after this repository's history is re-initialised for
publication, not against the current working history. Pushing the working
history would pre-empt that re-initialisation, so the prohibition stands on
its own terms and is not merely a consequence of there being nowhere to push.

## Architecture

See [`docs/ARCHITECTURE.md`](./docs/ARCHITECTURE.md) for the shape of the
codebase and the reasoning behind it.
