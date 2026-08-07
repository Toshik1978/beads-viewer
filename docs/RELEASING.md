# Releasing

The sequence for cutting a release of `bv`. This document and the release
machinery it describes — `task changelog`, `task release:check`,
`task release:verify`, `task release:snapshot`, `.cliff.toml`,
`.goreleaser.yaml`, `CHANGELOG.md` and the release workflow — were written to
match each other, and the machinery now exists.

Do not run `git push` or `gh` against this repository outside the steps
below, and never with `--force` against a shared branch.

## Step 1 — first release only: re-initialise history, then tag

Done, at `v1.0.0`: the development history was replaced with a fresh one
before publication, `origin` was created against it, and the first tag was cut
on the resulting history. Nothing in this step runs a second time — every
release starts at Step 2.

The heading stays rather than being deleted so the numbering the rest of this
document uses does not shift under the commits and prose that already cite it.

## Step 2 — generate the changelog entry

```bash
task changelog                # or: task changelog TAG=vX.Y.Z
```

This prepends the generated `### Features` / `### Bug Fixes` groups for the
commits since the last tag to `CHANGELOG.md`. Hand-write the prose above the
generated groups the same way the `v1.0.0` entry was written: a short
paragraph of what the release actually means for someone using `bv`, not a
restatement of the commit list below it.

## Step 3 — preview the release body

```bash
.github/scripts/extract-changelog.sh vX.Y.Z
```

This prints exactly the section the release workflow will use as the GitHub
release body — the text between `## vX.Y.Z — ` and the next `## ` heading.
Read it before committing; it is the last chance to catch a heading typo that
would otherwise make the extraction come up empty.

The script exits non-zero when the section is missing. That failure is a
guard, not a bug: it is what stops a tag being pushed for a version the
changelog never documented, which would otherwise publish a release with an
empty body.

## Step 4 — commit the changelog

```bash
git add CHANGELOG.md
task check
git commit -m "docs(changelog): add vX.Y.Z"
```

Run `task check` after staging, per this repository's usual rule — the
licensing sweep only sees tracked files, so a change staged after the gate
already ran is invisible to it.

## Step 5 — verify, tag and push

```bash
task release:check      # validate .goreleaser.yaml
task release:snapshot   # build the four archives locally, unpublished
task release:verify TAG=vX.Y.Z
git tag vX.Y.Z
git push origin vX.Y.Z
```

`task release:check` and `task release:snapshot` are a dry run: both exist
specifically so the first real archive build does not happen after the tag is
already public. Run them here, inspect `dist/*.tar.gz`, and only then move on.

`task release:verify` checks that the tag matches the version the changelog
documents, before anything is tagged — it creates nothing itself, so a failure
here costs nothing to walk back. `origin` already exists by this point: Step 1
created it on the first release, and it persists for every release after.

Pushing the tag triggers the release workflow, which runs `task check` again,
extracts the changelog section from Step 3, and publishes the four archives
described in [`README.md`](../README.md#install).

A second job then reads the published release back and fails if its body is
empty. That is not belt-and-braces: `--release-notes` is honoured by
GoReleaser's *changelog* pipe, so disabling that pipe makes the flag a silent
no-op — every step reports success and the release ships with working archives
and no notes at all. Nothing before publication can observe that, which is why
the check runs after it.

Most of what could go wrong here is caught earlier, on every pull request: the
`release-config` job in CI runs `goreleaser check` and proves the changelog's
current version extracts cleanly, so a broken release config fails a PR rather
than a public tag.

## Retrying a failed release

If the workflow fails after the tag was already pushed, fix the underlying
problem, then **delete the tag before pushing it again**:

```bash
git tag -d vX.Y.Z
git push origin :refs/tags/vX.Y.Z
git tag vX.Y.Z
git push origin vX.Y.Z
```

Delete-then-recreate, not force-push in place. GitHub does deliver a push
event for a tag ref that is force-updated to a new commit, so if "fix the
underlying problem" produced a new commit, force-pushing the moved tag would
also retrigger the workflow. The genuine no-op is narrower: force-pushing a
tag that still points at the exact commit it already pointed at — nothing
about the ref actually changed, so there is nothing for GitHub to report.
Deleting and recreating the tag sidesteps having to reason about which case
applies.
