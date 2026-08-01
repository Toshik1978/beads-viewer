# Contributing to bv

Thanks for looking. This guide covers the essentials; [`CLAUDE.md`](CLAUDE.md)
is the full development and verification workflow, and it is written for
whoever is doing the work — human or agent.

## Before you start

`bv` is a personal project. It is maintained in whatever time is left over
after everything else, and that shapes what you can expect:

- **Pull requests may not be reviewed.** Not because they are unwelcome —
  there simply may not be time. Please do not read silence as a judgement on
  your work.
- **Issues may not get a response,** and there is no target response time.
- **No support is offered.** [`README.md`](README.md) and `bv --help` are
  thorough; they are the support.

If you need a change on a schedule you control, **fork the project**. That is
a first-class outcome here, not a fallback — the licence permits it and it
will serve you better than waiting. Bug reports with a clear reproduction are
still genuinely useful even when they go unanswered for a while.

## Prerequisites

- **Go 1.26+**, as pinned in `go.mod`. Everything builds with
  `CGO_ENABLED=0`; there is no C toolchain to install.
- **[go-task](https://taskfile.dev)** — all automation is driven through
  `Taskfile.yml`.
- **[pre-commit](https://pre-commit.com)** — the hooks in
  `.pre-commit-config.yaml`.
- **[golangci-lint](https://golangci-lint.run) v2.x.** `.mise.toml` pins the
  major, so `mise install` (or `task setup`) gets you a current 2.x; CI pins
  `v2.12.2` exactly, because a v3 would read a different config schema. The
  two are not identical, so a new check in a later 2.x can fire locally and
  not in CI, or the reverse. If a lint result surprises you, compare
  `golangci-lint --version` against the pin in `.github/workflows/ci.yml`.
- **[git-cliff](https://git-cliff.org)** and
  **[GoReleaser](https://goreleaser.com)** — only to cut a release. Both are
  pinned in `.mise.toml` too.
- **[`br`](https://github.com/steveyegge/beads)** is optional. Two tests in
  `internal/beads` compare this project's readiness derivation against `br`'s
  own answer; without it on `PATH` they skip, and the rest of the suite is
  unaffected.

## Getting started

```sh
task setup          # mise install, go mod download
pre-commit install  # commit-msg, pre-commit and pre-push hooks
task build          # the bv binary
```

One `pre-commit install` wires all three stages — `.pre-commit-config.yaml`
declares `default_install_hook_types`, so no `--hook-type` flags are needed.

## Before you open a pull request

Run the gate. It must exit 0:

```sh
task check
```

That is `format:check`, `lint` and `test`. It takes seconds, not minutes.

### Stage first, then run the gate

`internal/licensing` sweeps **git-tracked** files. A file that is written but
not yet `git add`ed is invisible to it, so the gate passes, the commit lands,
and the very next run goes red on the file just committed. Stage, then gate,
then commit. This is also why the full gate is a *pre-push* hook rather than a
pre-commit one: pre-push is the first point at which the sweep sees what a
reviewer will.

The pre-commit hook runs the formatters, so a push will catch formatting even
if you forget. Do not skip hooks and do not reach for `--no-verify`.

## Conventions

- **Commits follow [Conventional Commits](https://www.conventionalcommits.org/)**,
  enforced by a `commit-msg` hook. This is not a style preference:
  `.cliff.toml` builds `CHANGELOG.md` by parsing those prefixes and that
  section becomes the GitHub release body, so a commit that does not parse is
  a commit missing from the release notes. `feat`, `fix`, `docs`, `chore`,
  `build`, `ci`, `test`, `perf`, `style`, `refactor` and `revert` are
  accepted, as is a `!` breaking-change marker. Write the subject for someone
  who was not there. No AI-attribution trailers of any kind.
- **Tests use testify suites, one entry point per package.** Exactly one
  top-level `func Test<Package>(t *testing.T)`, which does nothing but
  `suite.Run` each suite; every assertion is a suite method. A bare
  `func TestX(t *testing.T)` will not be accepted. See the Testing section of
  [`CLAUDE.md`](CLAUDE.md).
- **A test that cannot fail for its stated reason is worse than no test.**
  Before submitting one, break the thing it covers and watch it go red. Much
  of this codebase's review history is that mistake in various disguises.
- **Do not name a filesystem path from your own machine in a tracked file,**
  and name other projects only in `README.md`'s Acknowledgements.
  `internal/licensing` enforces both and will fail the build.
- **New dependencies** need a reason: what it solves, and why the standard
  library or an existing dependency is insufficient. There are nine direct
  dependencies and that number is a deliberate ceiling, not a coincidence.

## Core constraints

`bv` is **read-only with respect to `.beads/`**: it never writes anywhere
inside that directory and never spawns a subprocess. Both are asserted by
tests in `cmd/bv/main_test.go`, and the second is a source check — importing
`os/exec` anywhere in the module fails the build. A change that would break
either is out of scope rather than a bug to work around.

It also **renders rather than validates**: unknown `status` and `issue_type`
values are kept as raw strings, unknown JSON fields are ignored, and labels
are never checked. A workspace `br` itself would reject must still open here.

There are size tripwires — a line budget, a per-file cap, a field cap on the
root model — and they exist to catch one specific regression: a single model
object absorbing every view's state until nothing is reviewable in isolation.
[`CLAUDE.md`](CLAUDE.md) lists them with the reasoning, and
[`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) explains the shape they
protect.

## Releases

Releases are cut by hand, by the maintainer, by pushing a `v*` tag. The full
sequence — generate the changelog section, verify, tag, push — is in
[`docs/RELEASING.md`](docs/RELEASING.md). Read it before touching
`.goreleaser.yaml`, `.cliff.toml`, or anything under `.github/scripts/`.
