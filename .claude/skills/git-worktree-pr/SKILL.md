---
name: git-worktree-pr
description: "DevDesk git workflow: worktree under .worktrees/, branch, push through the Entire mirror, PR and merge with `gh -R anthnel/devdesk`, sandbox/host relay, follow-up commit after a squash-merge, worktree cleanup. Load before creating a worktree or branch, pushing, opening or merging a PR, or removing a worktree."
---

## Work starts in a new worktree

**Any task that will produce a commit begins with its own worktree**, cut from
the current `main`, **under `.worktrees/` inside the repository**:

```bash
git fetch origin main
git worktree add -b <branch> .worktrees/<branch> origin/main
cd .worktrees/<branch>
```

Answering a question, reading code, running the app — none of that needs one.
Editing does.

**Under the repository, not beside it.** This reverses what this file said until
now, and the reason it gave was wrong. It claimed a worktree at `.worktrees/x`
would be compiled by `go build ./...` and `go test ./...`. It would not: the go
tool skips every directory whose name begins with `.` or `_`, so `.worktrees/`
is invisible to `./...`. Verified rather than assumed — a package of deliberately
invalid Go placed under `.worktrees/` builds clean and `go list ./...` does not
name it. The old argument holds for `worktrees/` without the dot, which is
presumably where it came from.

What decides it is the **sandbox mount**, and it cost a full debugging session to
find. A direct-mode sandbox mounts *this directory only*. A sibling worktree at
`../devdesk-<branch>` is therefore outside the mount, so its **files** are written
into the container's private layer and never reach the host — while `.git` is
inside the mount and stays shared. Refs agree, files diverge, and nothing warns:
the branch tip moves, the host's copy of those files does not, and `git status`
on the host reports the *stale* content as uncommitted modifications. An agent
and the person testing its work end up compiling different code while both
believe they are on the same branch. Under `.worktrees/`, host and sandbox see
one filesystem.

Two effects worth knowing, neither blocking:

- `.worktrees/` is in `.gitignore`; a worktree there is not a change to the tree.
- Claude Code discovers the nested `.claude/` of each worktree, so its skills and
  rules appear a second time, prefixed by the worktree path. Harmless, and the
  price of the directory being inside the repo.

**What isolates, and what does not.** git refuses to check out one branch in two
worktrees, so two agents cannot land on the same branch by accident — that is the
whole protection, and it is worth knowing what it does *not* cover:

| Shared by every worktree | Consequence |
|---|---|
| `~/.devdesk/` — contexts, `config.yaml`, scan caches | two agents editing settings or scanning at once overwrite each other's; nothing warns |
| the host keyring, Docker, the ports the app binds | one at a time, whoever gets there first |
| `.git/hooks` | verified: `git rev-parse --git-path hooks` resolves to the **main** checkout's, so the Entire checkpoint and pre-push hooks fire normally from a worktree |
| `origin` | inherited — `git push -u origin <branch>` goes through the mirror as usual |

`mise` tasks and `go build` work unchanged; both read the tree they are run in.

**`entire graph` indexes per directory.** A fresh worktree has no index, so the
first `entire graph search` there pays a full build (~5 s on this repo, measured)
and the symbol ids it returns are namespaced by the directory name
(`local/<branch>:Go:…`). Nothing breaks; do not be surprised by the first search
being slow or by ids that do not match another worktree's.

**Clean up when the PR is merged**, from the main checkout:

```bash
git worktree remove .worktrees/<branch>
git worktree list          # what is still out there
git worktree prune         # after a directory was deleted by hand
```

`git worktree remove` refuses a tree with uncommitted changes, which is the
point — look before forcing.

**The main checkout stays on `main` and stays clean.** It is what `git fetch
origin main && git merge --ff-only origin/main` needs after each merge, and what
every new worktree is cut from.

## Git workflow — pull requests only

**Never push directly to `main`.** The Entire mirror rejects it:

```
! [remote rejected] main -> main (protected branch)
```

This is enforced by the mirror, not by GitHub, and **Entire does not document it**
— neither the CLI help nor `docs.entire.io` mentions a protected default branch.
The only branch restriction Entire states is that `entire/unmirrored/*` is never
forwarded. Treat the rejection as observed behaviour, not as a specified one.

**The repository is public since 2026-09-06**, which is what makes GitHub-side
enforcement possible at all: a private repo on a free personal plan answers
`403 Upgrade to GitHub Pro or make this repository public` on both
`/repos/anthnel/devdesk/branches/main/protection` and
`/repos/anthnel/devdesk/rulesets` — that was the prior state, and it is not a
description to trust going forward if the repo ever goes private again. Do not
cite `gh api repos/anthnel/devdesk/branches/main` reporting `"protected":
false` as proof of anything either way: that field only ever reflects legacy
branch protection, so it reads `false` under a ruleset too, and it read `false`
on the free-private plan whatever the configuration. Query
`/repos/anthnel/devdesk/rulesets` itself (or `gh ruleset check main`) for the
real answer.

A ruleset named `main-protection` (id `22399372`) now targets `refs/heads/main`
only — deliberately not `entire/checkpoints/v1`, which receives automatic
pushes from the Entire hook and would break if rule-gated. It requires a pull
request (0 approving reviews — solo maintainer, the PR is a record not a review
gate) with the `Lint and build` and `Test` checks from `ci.yml` passing,
blocks force-pushes and branch deletion, and grants no bypass. This closes the
gap the mirror alone left open: the Entire mirror's rejection of a direct push
to `main` was, until now, the *only* protection — a second remote pointing
straight at GitHub would have sailed through (see the warning against adding
one, below). Now GitHub itself refuses that push too.

**The `Entire Gates` check is expected to stay red on this repository.** It is
a PR check from the `entire` app, and its **Approvals** gate fails until another
reviewer approves the trail. That approval cannot arrive: the repository has one
developer, so the gate cannot be green, and no second account or dummy reviewer
should be added to satisfy it. The check is not required by `main-protection`
(GitHub reports the PR as `UNSTABLE` but `MERGEABLE`), so a red `Entire Gates`
alone does not block a merge. Merge once `Lint and build` and `Test` pass. The
Findings, Checks and Up to date gates must stay green: if any of them fails, it
is a real signal, so stop and raise it with the user. Agents (orchestrator,
devops) must not stop to ask for agreement over the Approvals failure alone.
To read the reason:
`gh api repos/anthnel/devdesk/commits/<sha>/check-runs --jq '.check_runs[] | select(.name=="Entire Gates") | .output.summary'`.

Every other branch, including `entire/checkpoints/v1`, pushes through the mirror
and is forwarded to GitHub.

**Pull requests are GitHub's, not Entire's.** Entire has no merge-request
concept — it layers checkpoints and sessions over git and forwards pushes. So
the PR steps below are `gh`, and the branch they review is one `origin` carried
to GitHub. `entire review` (labs) runs a multi-agent review against the current
branch and is a pre-merge step, not a substitute for the PR.

```bash
git worktree add -b <branch> .worktrees/<branch> origin/main   # work happens here
git push -u origin <branch>                     # via the mirror — forwarded to GitHub
gh pr create -R anthnel/devdesk --base main --head <branch>
gh pr merge -R anthnel/devdesk <n> --squash --delete-branch
# then, from the main checkout:
git fetch origin main && git merge --ff-only origin/main   # may need a retry, see below
git worktree remove .worktrees/<branch>
```

**`-R anthnel/devdesk` is not optional.** `gh` infers the repository from a
remote pointing at a GitHub host, and `origin` is an `entire://` URL, so it
finds none and fails with *"none of the git remotes configured for this
repository point to a known GitHub host"*. The flag names the repo directly.
Do not solve this by adding a GitHub remote — see below.

**After a merge, `origin` can read stale for a minute or two.** The mirror lags
GitHub, so `git fetch origin` right after `gh pr merge` may return the *previous*
`main` — with no error, which is the part that misleads. `gh pr view <n> --json state`
says `MERGED` while `git rev-parse origin/main` still points at the commit before
it. Re-run the fetch a moment later; `gh` is what to trust in the meantime,
because it talks to GitHub directly and needs no remote.

**Do not add a second remote pointing at GitHub to work around that.** The
`main-protection` ruleset (above) would now refuse a direct `git push github
main` too, so it is no longer the *only* defeat of the only protection — but a
direct push still bypasses the mirror, and with it the Entire hook that
records checkpoints and sessions, which is not visible until much later.
`origin` is the only remote, deliberately.

**Merging several branches cut from the same commit conflicts in
`docs/backlog.md`.** Every fix inserts its entry at the top of §1.1, so the
second and third merges land on the same anchor. The resolution is always to
keep both sides — they are independent entries, not competing edits.


**A follow-up commit on the same branch, after the first one already got
squash-merged, will conflict on a plain `git merge`** — squashing rewrites
history, so `git merge origin/main` computes the wrong common ancestor
(the branch's *own* pre-squash parent) and re-diffs files that already
landed, even when nothing actually conflicts in substance. Don't hand-edit
through that; cherry-pick the new commit straight onto current `origin/main`
instead — its real parent already matches what's on `main`, so it applies
clean:

```bash
git worktree add -b <tmp-branch> .worktrees/<tmp-branch> <new-sha>
cd .worktrees/<tmp-branch>
git reset --hard origin/main
git cherry-pick <new-sha>              # clean apply — verify before trusting this
go build ./... && go test ./<touched-packages>/...
git push origin HEAD:refs/heads/<new-branch-name>
# gh pr create / gh pr merge as above, then from the main checkout:
git worktree remove .worktrees/<tmp-branch>
```

### Remotes

| Remote | URL | Use |
|--------|-----|-----|
| `origin` | `entire://aws-eu-central-1.entire.io/gh/anthnel/devdesk` | Entire mirror — everything: clone, fetch, push |

**There is one remote, and there should stay one.** The repository is hosted on
GitHub and mirrored to EntireDB in `aws-eu-central-1` (a second placement exists
in `aws-us-east-2`), but every git operation goes through the mirror: it is the
regional path, it is what keeps agent reads fast, and it is where the checkpoint
hook runs. Anything needing GitHub's own answer — PR state, review threads —
goes through `gh`, which authenticates directly and does not need a remote.

Semantic search (`entire search`, and the `entire:*` skills that depend on it) is
**not available in the EU region** — the server answers `semantic search is not
yet available in the region(s) hosting this search`. Nothing in the repo config
fixes this.

The sandbox/host relay (landing a sandbox-made commit, developing in the sandbox and testing from the host) is in [sandbox.md](sandbox.md); read it only when a sandbox is involved.

After the merge, load the `release-flow` skill.
