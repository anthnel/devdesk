# Sandbox / host relay

### Landing a sandbox-made commit (host-relay)

A Docker Sandbox (`sbx`) has no `entire://` auth of its own — `ENTIRE_TOKEN`
via `sbx secret set-custom` doesn't work (`git-remote-entire` parses the
JWT's claims locally, before any network call, but the secret proxy only
substitutes the real token on the wire — the sandbox process itself only
ever sees an unparseable placeholder), and an interactive `entire login`
inside the sandbox trades that for a persistent, harder-to-reason-about
credential sitting in a less-trusted place. So the sandbox edits and commits
locally only; the host does every `origin` operation — fetch, push, PR,
merge. `git pull`/`git fetch` run *inside* the sandbox will fail with `no
auth context for cluster ...` — that's expected, not a setup bug; don't
`entire login` there to fix it (see `~/projects/github/anthnel/sbx-kits/entire/README.md`
for the full rationale).

A direct-mode sandbox mounts this exact repo directory, so both the commits and
the working files land where the host sees them — provided the worktree is under
`.worktrees/`, which is why that is the rule. There is nothing to transfer; the
host only has to push, because the sandbox has no `entire://` auth:

```bash
# From the host, once the sandbox reports "done and committed":
git push origin <branch>
gh pr create -R anthnel/devdesk --base main --head <branch> --fill
gh pr merge -R anthnel/devdesk <n> --squash --delete-branch
git fetch origin main && git merge --ff-only origin/main   # may need a retry, see above (in the `git-worktree-pr` skill, SKILL.md)
git worktree remove .worktrees/<branch>
```

**If a worktree was made at the old sibling path** (`../devdesk-<branch>`), the
host cannot see its files and `git worktree list` shows it `prunable`. The
commits are still in the shared `.git/objects`, so nothing is lost — but the
host's copy of those files is stale, and `git status` there reports that stale
content as uncommitted modifications, which reads as if work were pending when
the opposite is true. Recover by taking the branch tip:

```bash
git stash push -m "stale files from a sandbox sibling worktree"   # reversible
git worktree prune -v                  # drop the unreachable admin entry
git push origin <sha>:refs/heads/<branch>
```

**A worktree created from inside the sandbox registers a Linux path the
host's git can't resolve — even under `.worktrees/`.** `.worktrees/` shares
*files* between host and sandbox, but each side's git still writes its own
`.git/worktrees/<name>/gitdir` pointer, and the sandbox's git records a Linux
path (`/c/Users/...`). The host's `git worktree remove` on that same
directory then fails with `fatal: '<path>' is not a working tree` — Windows
git cannot resolve the pointer left behind. The costlier part: a plain `git
worktree prune` run on the host right after that failure deregisters the
worktree **silently** — git treats an unresolvable pointer as a worktree
that's gone — while leaving alone any sibling worktree whose path it *can*
read. If the branch was already merged the content is safe either way, but a
host-run `prune` while an agent is still working inside a sandbox-created
worktree would deregister that worktree out from under it, mid-task, with
nothing on either side raising a warning. The rule this implies: **whoever
creates a worktree removes it** — do not `git worktree remove` or `prune`
from the other side of the host/sandbox split.

#### Developing in the sandbox, testing from the host

This is the normal mode, and the split works out well: **git is what the
host does not need in the worktree, the build is what the sandbox does not
need.** Measured in a worktree whose pointer carries a Linux path, from the
host:

| From the host, in `.worktrees/<branch>/` | Result |
|---|---|
| any `git` command | `fatal: not a git repository: (NULL)` |
| `go build ./...` | exit 0 |
| `go test ./internal/command/` | `ok … 0.239s` |
| `go build .` (with buildvcs stamping) | exit 0 |
| `mise run build` | OK |

The breakage is therefore **entirely** on the git side, not just on
`worktree remove`: Go does not need the VCS, and `-buildvcs=auto` degrades
silently rather than failing when it gets no answer. The main checkout is
never affected.

The resulting split of responsibilities:

1. **The sandbox creates the worktree** — it is the one that will commit
   into it, so it is the one that must keep a working git.
2. **The sandbox edits and commits.** All the git activity happens there.
3. **The host builds, tests, and runs the application** in that same
   directory, since the files are shared through the mount. No git command
   here — the `fatal` above is expected, it does not signal a broken repo.
4. **To read the diff from the host, go through the main checkout**: the
   refs and `.git/objects` are shared, so it sees everything the sandbox
   committed — `git log --oneline main..<branch>`, `git diff main...<branch>`.
5. **The host pushes and merges from the main checkout**, never from the
   worktree — that is the host-relay above.
6. **The sandbox removes its worktree.** If it no longer exists, the host can
   delete the *files* (they are inside the mount, unlike the sibling case
   above) then `git worktree prune` — but only once no sandbox still has a
   live worktree.

**Testing from the host is not a workaround.** DevDesk needs a real
terminal, the host's Docker socket, the host's secret manager
(`internal/credentials`), `~/.devdesk/`, and binding ports: none of that is
representative inside the sandbox. What the sandbox does better is write
code.

**The reverse direction is forbidden**: the host does not create the
worktree for the sandbox to work in. By symmetry, the `C:/Users/...` path
Windows git writes is not resolvable under Linux — the sandbox would lose
the git it needs to commit. (This direction has not been tested; it is the
same mechanism read backwards.)
