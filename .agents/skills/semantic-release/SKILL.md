---
name: semantic-release
description: "Cuts a new semantic-version release by analyzing commits since the last release, deciding the next version, and pushing a git tag (CI handles the rest). Use when asked to: release, cut a release, bump version, tag a release, ship a new version, or 'what version is next'."
---

# Releasing software

Determines the next [semantic version](https://semver.org/) from the commits
since the last release, then tags and pushes it. A GitHub Actions workflow
already triggers the actual release on tag push, so this skill's job ends at
pushing the tag.

## Rules

- **Never** create the release directly (no `gh release create`, no building
  artifacts). Only push a git tag; CI does the rest.
- **Major bumps always require explicit user confirmation** before tagging.
- Minor and patch bumps: proceed to tag and push without asking.
- If there are **no releasable changes** since the last tag, do not tag — report
  that and stop.
- Work on the default branch unless the user says otherwise. Warn if the local
  branch is behind/ahead of its remote, or the working tree is dirty.

## Workflow

### 1. Sync and find the last release

```bash
git fetch --tags --force
```

Find the latest release tag by semver order (not commit date):

```bash
git tag --list --sort=-v:refname | head -20
```

Pick the highest **stable** semver tag (ignore pre-release tags like
`-beta`, `-rc` unless the user is on a pre-release line). Note whether tags use a
`v` prefix (e.g. `v1.4.2`) and **match the existing convention** for the new tag.

If there are no tags yet, the first release is `v0.1.0` (or `v1.0.0` if the user
says the project is stable) — confirm with the user.

### 2. Collect changes since the last release

```bash
git log <last-tag>..HEAD --no-merges --pretty=format:'%h %s%n%b%n---'
```

Also skim the diff stat when commit messages are vague:

```bash
git diff <last-tag>..HEAD --stat
```

### 3. Decide the bump (use judgment)

Follow [Conventional Commits](https://www.conventionalcommits.org/) as the
primary signal, but use judgment when messages don't follow the convention —
read the actual diff to classify the change.

| Bump      | Trigger                                                                                  |
| --------- | ---------------------------------------------------------------------------------------- |
| **major** | `BREAKING CHANGE:` in body/footer, `!` after type (`feat!:`), or any backward-incompatible API/behavior change |
| **minor** | `feat:` — new, backward-compatible functionality                                         |
| **patch** | `fix:`, `perf:`, and other backward-compatible fixes                                     |
| none      | only `docs:`, `chore:`, `style:`, `test:`, `refactor:` with no user-facing effect        |

Take the **highest** bump implied by any commit. While `0.x.y`, prefer keeping
breaking changes as a minor bump unless the user wants `1.0.0` — flag this.

Briefly summarize to the user: the last version, the next version, and the key
commits that justify the bump.

### 4a. Major release → confirm first

State clearly that this is a **major / breaking** release, list the breaking
changes, and ask for explicit confirmation before tagging. Do not proceed
without a clear "yes".

### 4b. Minor / patch → tag and push

Create an annotated tag and push just that tag:

```bash
git tag -a <new-tag> -m "<new-tag>"
git push origin <new-tag>
```

(Always set `GIT_EDITOR=true` so the annotated-tag command can't open an editor.)

### 5. Confirm

Report the pushed tag and that CI will now run the release. Optionally link the
Actions run:

```bash
gh run list --limit 3
```
