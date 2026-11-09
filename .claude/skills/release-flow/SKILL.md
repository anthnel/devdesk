---
name: release-flow
description: How DevDesk publishes a release (release-please PR, then goreleaser) and when to propose one. Load after every merge on main to check the triggers, before merging the release PR, or before editing release-please-config.json, .release-please-manifest.json, .goreleaser.yaml, .github/workflows/release.yml.
---

### Publishing a release — release-please, then goreleaser

**A release takes two merges, and that is the point.** `release-please`
watches `main` and keeps a **release pull request** open: it reads the
conventional commits since the last tag, decides the version, and writes
`CHANGELOG.md`. Merging that PR creates the tag and the GitHub Release;
`goreleaser` then builds the six binaries (linux, darwin, windows × amd64,
arm64) and attaches them to that Release.

#### The flow, plainly — accumulate, then release whenever you want

The two actors: `release-please` **proposes** (it never writes directly to
`main` except through its own PR), `goreleaser` **builds**, and it only
triggers once that PR is merged. Concrete timeline:

1. A `fix:` or a `feat:` is merged onto `main` (a normal work PR, see
   above, in the `git-worktree-pr` skill, SKILL.md). The code ships right away. release-please sees that commit and
   **creates or updates** — never two PRs in parallel — the
   "chore(main): release x.y.z" PR with the corresponding changelog; that PR
   only touches `CHANGELOG.md` and `.release-please-manifest.json`.
2. Step 1 repeats as many times as wanted — several fixes, one or several
   features, over days or weeks. Each merge grows the same release PR (the
   version number can climb, a `feat:` bumps the minor) without any tag
   being set and without any binary being published.
3. When the moment is judged right — and only then — the release PR itself
   gets merged. That specific merge, and only that one, sets the tag,
   creates the GitHub Release, and wakes up goreleaser.

| Action | Effect |
|---|---|
| Merge a `fix:`/`feat:` onto `main` | Code shipped immediately; the release PR updates in the background; nothing is published |
| Merge the release PR | **Sole trigger for publishing** — tag, Release, binaries |

Publishing is therefore never a per-commit act: it is a deliberate act,
decided by merging a PR that already exists and that can be left to grow
indefinitely.

```bash
# nothing to run: the release PR opens and updates itself on its own
gh pr list -R anthnel/devdesk --label "autorelease: pending"
gh pr merge -R anthnel/devdesk <n> --squash   # ← this merge is what publishes
```

#### When to propose a release

Publishing is never automatic, but it should not be forgotten either. The `pm`
**proposes** merging the release PR at key moments; **the user alone decides**.
No agent merges the release PR without the user's explicit agreement.

Any one of these is enough to trigger a proposal:

1. **Story delivered**: a Plane story moves to Done (its `feat:`/`fix:` PR is merged on `main`).
2. **Module finished**: every story of a Plane module (epic) is Done.
3. **Security fix**: a fix touching security (auth, secrets, external input,
   vulnerable dependencies, image signatures) lands on `main`. Propose immediately.
4. **Accumulation**: the open release PR
   (`gh pr list -R anthnel/devdesk --label "autorelease: pending"`) holds at
   least 5 changelog entries, or 2 weeks have passed since the last release
   (`gh release list -R anthnel/devdesk --limit 1`).

A subagent never speaks on its own, so the check belongs to the session that
orchestrates (orchestrator, `/feature`, `/bugfix`): **after every merge on
`main`**, it evaluates the triggers above. If one is met, it delegates to the
`pm` to draft the proposal: proposed version, content (taken from the release
PR body), the trigger met, and anything still in progress that would justify
waiting. It then asks the user. On yes, `devops` merges the release PR; on no,
nothing happens and the PR keeps accumulating.

**Why not semantic-release**, which would achieve the same thing in a single
merge: it pushes the tag directly to `main`, and this repository forbids
that — the Entire mirror rejects that push, and this refusal is the *only*
protection `main` has, since branch protection is unavailable on this plan
(the 403 above, in the `git-worktree-pr` skill, SKILL.md). One extra merge per release is the price of that rule.

**The mirror replicates tags, verified on 2026-09-06.** The tag is set by
GitHub Actions, so it is born on GitHub and not via the mirror — but
`git fetch origin --tags` brings it back, just as it brings back a
squash-merge done by `gh`. That was the only unknown in the chain, and it is
now resolved: nothing forces setting a tag by hand.

| File | Role |
|---|---|
| `release-please-config.json` | the type (`go`), the CHANGELOG sections |
| `.release-please-manifest.json` | the current version — **written by the bot**, never by hand |
| `.goreleaser.yaml` | the six targets, the `-ldflags`, `mode: append` |
| `.github/workflows/release.yml` | the two jobs, chained by `release_created` |

**A repository setting no workflow can grant itself**: *Settings →
Actions → Allow GitHub Actions to create and approve pull requests*. Without
it, the first run fails on the PR it cannot open, and the message does not
say that is why. **Already active here** —
`gh api repos/anthnel/devdesk/actions/permissions/workflow` answers
`can_approve_pull_request_reviews: true`, verified on 2026-09-06. The same
call shows `default_workflow_permissions: read`, which is not a problem:
that is the default when a workflow states nothing, and `release.yml`
declares its own.

**The binary's version comes from `-ldflags`**, not from a committed file
(§3.62). `release-please` knows how to write into a Go file (`versionFile`)
and it is not used for that: it would be a second source of truth for an
answer the build already gives, and a dev binary would then show the number
of the last release instead of `dev`.

**Before pushing a change to the chain**, `mise run release-check` validates
the configuration and `mise run release-snapshot` builds the six targets
without publishing anything — it is the only way to discover a broken target
before a tag is set.

**The first release will list the whole history** (66 `feat:` and 50 `fix:`
as of 2026-09-06). This is deliberate — an empty first CHANGELOG would be
worse — and the PR is editable before the merge. To start at `1.0.0` rather
than `0.1.0`, a one-off `"release-as": "1.0.0"` in the config, removed
afterward.
