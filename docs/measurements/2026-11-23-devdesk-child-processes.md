# What DevDesk's own child processes look like: native scan, container scan, clone (DEVDESK-712, S-03 spike)

Measurement note of 2026-11-23. Spike S-03 of the `safe-process-stop` epic (`docs/product/safe-process-stop/`).
It produces no production code and no test: only this note. The harnesses that took the numbers are throwaway
programs kept outside the repository (a separate Go module in the session scratchpad); their behavior is
described below so the numbers can be redone. A dated measure is never corrected afterwards: if a later change
moves these numbers, add a new note.

Every statement below carries one of three labels:

- **Measured**: a number or an observation from a run of the harness on this machine.
- **Read in code**: from reading the repository (path and line at commit `ec457184`) or the gopsutil v4.26.7 source.
- **Unverified**: recalled, deduced or expected, and not checked here. Never to be quoted as a fact.

| | |
|---|---|
| Question | what does a "processes linked to DevDesk" screen see of what DevDesk starts, and is it worth building (H6, H7 of the brief)? |
| Machine | Linux 6.12.107 (Debian 13), 22 logical cores, 657 processes, load average about 0.7 at the start, go1.27.4, git 2.47.3, gitleaks 8.30.1, trivy 0.76.2, gopsutil v4.26.7 (the version of `go.mod`). Not a dedicated machine |
| Harness | a Go program that plays DevDesk: it starts a command through `exec.CommandContext` with `Start` then `Wait` (the shape of `cliRunner.Run`), and while the command runs it polls its own process with gopsutil `Process.Children()`, recursively, recording pid, ppid, name, command line, first and last sighting |
| Docker | the `docker` CLI is installed, but the user of this session cannot reach the daemon (`permission denied ... /var/run/docker.sock`, not in the `docker` group). Nothing was run against it and the permission was not worked around. **Scenario (b) is therefore not measured.** Podman is not installed |
| Windows, macOS | no machine available: **not measured** (see their section) |
| Not touched | `~/.devdesk`, any existing container or image, any process that the harness did not start itself |

## What `Children()` is, by reading the code

Read in code, gopsutil v4.26.7:

- **Linux** (`process_linux.go:341`): `Children()` globs `/proc/[0-9]*/stat` and reads every file, keeps those whose
  ppid equals the pid, then builds a `Process` for each. It does not read `/proc/<pid>/task/*/children`. One call
  therefore costs one read per process on the machine.
- **Windows** (`process_windows.go:675`): one `CreateToolhelp32Snapshot` of all processes, filtered on
  `ParentProcessID`, then `NewProcess` per match.
- **macOS** (`process_darwin.go:249`): lists every process, reads each one's ppid, filters. On a listing error it
  returns `nil, nil`, an empty list and no error.
- It returns **direct children only**. Descendants need a recursion, which repeats the whole scan at each level.
- A process whose parent died is reparented (to PID 1 or to a subreaper), so it is no longer a child of anything of
  DevDesk's. A descendants list cannot show an orphan. Only a PID noted at launch (track A of the brief) could.

## H6: does DevDesk keep a PID or a handle?

**Read in code: no.** Outside `internal/ports` (which reads other processes' identity, DEVDESK-709) and the
`nexus-bench` development tool (`internal/nexusbench/kubectl.go:217`, which keeps its own `cmd.Process` to stop its
own `kubectl port-forward`), no file under `internal/` reads `.Process` or `.Pid` of a started command
(`grep -rn '\.Process\b\|\.Pid\b' internal` , excluding tests). Every launch site hands the command to `Run`,
`Wait` or `Start` and drops the `*exec.Cmd`. What exists is the context's `CancelFunc`: the registry
(`internal/jobs/registry.go:290`, `Cancel`) stores it per run and per item and calls it. The brief's H6 stands:
the registry holds a state and a `CancelFunc`, never a process.

## Scenario (a): native scan (trivy, gitleaks, plumber run directly)

**Launch path (read in code).** The scanner (`internal/scan/scanner.go:647`, `:670`, `:697`, `:791`) calls
`RunTrivy` (`trivy.go:121`), `RunGitleaks` (`gitleaks.go:189`) or `RunPlumber` (`plumber.go:200`), which all call
`runner.Run`. The production runner is `cliRunner.Run` (`internal/scan/run.go:100`): `exec.CommandContext(ctx, tc.Name, tc.Args...)`
at `run.go:101`, `cmd.Start()` at `run.go:139` (stderr streamed to the progress callback) or `run.go:148`, `cmd.Wait()`
in `finish` at `run.go:163`. The binary is `trivyBinary`/`gitleaksBinary` (`trivy_args.go`, the resolved path or the plain
name). Trivy runs are serialized by `trivySem` (`trivy.go`), so at most one native Trivy process at a time; Gitleaks
and plumber are not.

**Cancellation context (read in code).** The context is created in the `Cmd` that starts the scan:
`context.WithCancel(context.Background())` at `internal/ui/workspaces/commands.go:160` and
`internal/ui/security/inventory_commands.go:244`, sent on the "starting" message, stored by the registry, called by
`K` in `:jobs`. `exec.CommandContext` has no `Cmd.Cancel` override here (no `Cancel`, `WaitDelay`, `Setpgid` or
`SysProcAttr` in `internal/scan`), so cancelling sends the default `Kill` to the direct child only.

**PID or handle kept (H6): no** (`cliRunner.Run` holds `cmd` for the length of the call and returns).

**What `Children()` saw (measured).**

| Command (argv as `gitleaksArgs` and `trivySecretArgs` build it, native) | Direct child | Grandchildren | Wait returned after |
|---|---|---|---|
| `gitleaks detect --source <worktree> --gitleaks-ignore-path <worktree> --report-format json --report-path /dev/stdout --no-git` | `gitleaks`, one | none | 784 ms (exit 1: findings in test fixtures; 22 polls) |
| `trivy fs --format json --scanners secret <worktree>` (scratch cache dir, `--skip-version-check`) | `trivy`, one | none | 610 ms (15 polls) |

- One process, no sub-process, for both tools. Typical lifetime on this tree: under a second for a secrets-only scan
  of this worktree (2,150 tracked files). A full Trivy vulnerability scan needs the vulnerability database and the network:
  **not measured**, expected to be much longer (Unverified).
- The recursive read cost 50 ms (median) per descent here (two `Children()` reads plus name and command line).

## Scenario (b): container scan (`docker run` / `podman run`)

**Launch path (read in code).** `wrapTrivy` ends in `containerRun{Flags, Image, Argv}.cmd()`
(`internal/scan/trivy_args.go:243`), as do Gitleaks (`gitleaks.go:96`), plumber (`plumber.go:111`), kubeconform
(`kubeconform.go:87`, `kubeconform_render.go:67`, `:100`) and cosign (`cosign.go:239`). `containerRun.cmd`
(`internal/scan/container_run.go:34`) builds `toolCmd{Name: engine.Current().Binary, Args: run --rm <flags> [-e NAME]... <image> <argv>}`
(`container_run.go:42`), with the proxy values in `Env`, never in argv. From there it is the same `cliRunner.Run`
(`run.go:101`, `:139`/`:148`, `:163`) and the same `CancelFunc` as scenario (a).

**PID or handle kept (H6): no**, same code.

**What a process view would see: the engine client, not the container.**

- **Read in code.** The child of DevDesk is the `docker` (or `podman`) client process. The scan command carries no
  `--name`, no `--label` and no `--cidfile` (`grep -rn '"--name"\|--cidfile' internal/scan`, no hit outside tests):
  nothing in DevDesk's argv lets anyone tie the running container to the scan that started it.
- **Unverified.** The container's processes belong to the daemon (Docker Engine) or, with local Podman, to its own
  monitor process, and are not descendants of DevDesk, so `Children()` cannot show them. Expected from the
  architecture of the engines, not observed here (no daemon access, no Podman).
- **Unverified.** On cancel the context sends `SIGKILL` to the client. The Docker CLI documentation says the client
  proxies most signals to the container but not `SIGKILL`; if that holds, the client dies and the container
  keeps running until it ends by itself. Recalled, not re-read today, not observed.
- **Not measured.** The lifetime of the client equals the lifetime of the container run, plus an image pull when the
  image is missing. Whether the client spawns a credential helper child during a pull is not known.

What I could not measure is exactly the point the story asked ("only the client is seen"): it rests on the
deduction above. Someone with daemon access can settle it in a few minutes by running the harness with
`docker run --rm <small image> sleep 3` and reading the container's main PID with `docker inspect` next to
the harness's `Children()` output.

## Scenario (c): git clone and git sync

**Launch path (read in code).**

- Clone: the explorer's pipeline (`internal/ui/forge/explorer/pipeline.go:104`) starts a context and a scheduler;
  each repository runs in a goroutine bounded by `spec.jobs` (`pipeline.go:136`), through `cloneOne` (`pipeline.go:194`)
  to `git.Clone` (`pipeline.go:203`), which is `exec.Command("git", "clone", repoURL, targetPath)` at
  `internal/git/ops.go:44` and `cmd.Run()` at `ops.go:55`. **No context reaches the process**: the pipeline's context
  stops the walk and the scheduler, and the clones already running are awaited (comment at `pipeline.go:95-100`; decision 12:
  a cut clone leaves half a repository). `jobs.KindClone.Cancellable()` is false (`internal/jobs/jobs.go:69`).
- Sync (`F` in `ws`): `git.Sync` (`internal/git/sync.go:43`) runs a tag `git fetch` (`sync.go:185`, `--no-tags --no-prune
  ... +refs/tags/*:refs/tags/*`), then `git fetch --quiet --prune --no-prune-tags`, then the reconcile commands, all through `runContextMax` (`sync.go:306-307`:
  `exec.CommandContext(ctx, "git", ...)`, `cmd.Run()` at `:317`) with `context.Background()` (`sync.go:277`). Not
  cancellable either (`KindSync` is not cancellable).
- Neither keeps a PID or a handle (H6: no).

**What `Children()` saw (measured).** The remote was a local repository (58 MB of incompressible files) reached
through `file://`, which makes git spawn its upload-pack side like a network transport would. A plain path would
take a local shortcut with no transport.

| Command | Processes seen (depth) | Lifetime | Notes |
|---|---|---|---|
| `git clone file://<src> <dst>` | `git clone` (1); under it `sh -c git-upload-pack` (2), `git-upload-pack` (3), `git pack-objects --revs --thin --stdout` (4); and `git index-pack --stdin --fix-thin` (2). Five processes, depth 4 | all five from the first poll (1 ms) to the end; the whole clone 4.8 to 4.9 s | the same clone with no harness at all: 4.92 s and 5.02 s, so polling every 1 ms or 250 ms did not change it |
| `git clone <plain path> <dst>` | `git clone` only | 43 ms | local hard-link shortcut, no transport process |
| `git fetch --quiet --prune --no-prune-tags` (new 8 MB commit upstream) | `git fetch` (1); `sh` (2), `git-upload-pack` (3), `git pack-objects` (4) seen only at the first poll; `git unpack-objects` (2) seen from 132 to 239 ms | 364 ms in all | |
| `git ls-remote --tags origin` (stand-in for the short git commands of a sync: DevDesk itself runs `symbolic-ref`, `rev-parse`, `config`, `for-each-ref`, `remote get-url`, `merge --ff-only` besides the two fetches, `sync.go:53-225`) | nothing: the command ended (4 ms) before the first poll could see it | 4 ms | |

- With a real HTTPS or SSH remote the transport processes are different (`git-remote-https`, `ssh`, a credential
  helper), and the depth differs. **Not measured** (no network target used): Unverified.
- The processes that matter for a clone (the transport, `index-pack`) are at depth 2 to 4. A list of direct children
  alone shows one `git` row.
- Short commands (the 4 ms `ls-remote` stand-in) are invisible to polling. A sync runs several git commands per
  repository; which of them are that short on a real remote was not measured.

**What happens to descendants when the parent is killed (measured, with a context DevDesk does not give its clones).**
The same `git clone file://` run was cancelled through the context after 1.5 s, the way `CommandContext` would:

- `Wait` returned `signal: killed` only at 3.685 s, not at 1.5 s: the surviving descendants kept the captured stderr
  pipe open, and `Wait` waits for it (`Cmd.WaitDelay` is not set in `internal/scan` or `internal/git`).
- Sampled from outside, two of the git processes had ppid 1197 (`systemd`, the user manager) at the 1.8 s sample, that
  is they had been reparented and were no longer children of the harness; none were left at the 4.1 s sample.
- So a process-level stop of the direct child alone does not stop the tree, and a descendants list drawn from
  `Children()` loses these processes the moment the parent dies.

## Side finding: a started and never waited-for process is a zombie child

**Measured** with a stand-in (`sh -c 'exit 0'` started with `cmd.Start()` and never waited for): 300 ms later
`Children()` returns it, status `[zombie]`.

**Read in code** which launch sites do that: `launchDetachedCmd` (`internal/ui/workspaces/actions.go:233-238`) and the two
IDE launchers (`actions.go:315-316`, `:328-329`) call `cmd.Start()` and drop the command; `internal/ui/viewer/update.go:445-446` is the same shape. `internal/browser/browser.go:15-19` does reap (`go func() { _ = cmd.Wait() }()`).
A launcher that ends at once therefore stays a zombie child of DevDesk until DevDesk exits. This is not the
subject of the spike and nothing was changed. It does mean a "children of DevDesk" screen would show a defunct
row for each terminal or IDE opened from `ws`, which a user cannot stop.

## Cost of reading the children (measured, Linux)

657 processes, 22 cores, load about 0.7:

| Read | Median | p95 | Max |
|---|---|---|---|
| one `Children()` call (30 reads, idle process) | 22.3 ms | 28.0 ms | 31.1 ms |
| one-pass table (`Processes()` plus `Ppid()` per process, 30 reads) | 47.4 ms | 51.9 ms | 54.1 ms |
| recursive descent with name and command line, a quiet child (gitleaks, trivy) | about 50 ms | 63 ms | 63 ms |
| the same descent while the five-process clone ran (19 reads at 250 ms) | 153 ms | 160 ms | 160 ms |

- The cost is linear in the number of processes on the machine and repeats at every level of the descent. On this
  machine a read every 2 s (the Ports tab cadence) is affordable; on a busy workstation or on a platform where the
  per-process reads are slower, it was not measured.
- Building the whole table once is slower here than one `Children()` call; there is no cheaper way to read it through
  gopsutil.

## Windows: not measured

No Windows machine was available; nothing was run.

- **Read in code**: gopsutil's `Children()` is one Toolhelp32 snapshot filtered on the parent id, then one `NewProcess`
  per match (`process_windows.go:675`). The repository's own comment in `internal/ports/ports.go:143` reports a
  Toolhelp32 snapshot naming 294 processes in 11 ms; that is a figure from an earlier measure, not re-taken here.
- To measure: the same harness with `git clone`, `trivy` and `docker run` argv, on native Windows, elevated and not;
  the cost of one `Children()` call and of the recursive descent; whether `NewProcess` of a child needs a handle that is
  refused for some processes; the process names (`git.exe`, `git-remote-https.exe`, `docker.exe`); and what is seen
  after the parent exits.
- **Unverified**: that Windows keeps the original parent id of a process after its parent has exited (so that a
  stale parent id could match a reused PID), and that `Process.Children()` is slow or limited there. Neither is
  asserted by this note.

## macOS: not measured

No macOS machine was available. **Read in code**: `Children()` lists all processes and reads each one's ppid
(`process_darwin.go:249`), with the quirk above (an error becomes an empty list). To measure: the same harness, the
cost per call, and whether the listing is complete for processes the user cannot inspect.

## What a "processes linked to DevDesk" screen would add to what exists

| Existing surface | What it already says about DevDesk's work | What it cannot say |
|---|---|---|
| `:jobs` (`internal/jobs`) | every scan, sync, clone, pull, index run, per target, with state, detail, failure memory, and `K` to cancel (scan, pull, index; not sync, clone) | the PID, the process or container behind a target; what is left running after a cancel |
| Ports tab (`internal/ports`, `internal/ui/netdiag`) | every socket on the machine with PID and process name, `LISTEN` and `ESTABLISHED` included (`ports.go:37`), so a clone's transport process and its connection appear while it runs | whether a process was started by DevDesk |
| Forward (`internal/forward`) | the local port forwards, which are listeners inside the DevDesk process (PID of DevDesk, protected by S-01a) | nothing is a separate process, so no PID to show |
| Containers view (`docker ps -a`, `internal/docker/containers.go:335`) | the containers of the engine, including a running scanner container, with stop and remove | that a given container is a DevDesk scan: the command has no name and no label (see scenario b) |

What is really missing, by this table and the measures:

1. **A link from a running target to its process or container**, mostly for the container scan: the client's PID is
   in nobody's hands, and the container is unattributable. Neither a descendants list (it would show the client
   only) nor an arbitrary process screen fixes the container; what would is something DevDesk puts into the command
   (a label or a name), which is a different change from a new screen.
2. **Stopping what `:jobs` does not stop** (clone, sync). That is a product decision (decision 12 and D7: a cut
   clone leaves half a repository, a sync in flight is waited out), not a visibility gap. A stop button on a `git`
   row would reopen it, and a stop of the direct child alone leaves the tree running (measured above).
3. **Orphans after a cancel or a crash.** A descendants list is blind to them by construction (reparenting). Only
   a PID noted at launch (track A) could see them, and that means a registry that holds PIDs, which does not exist
   (H6).
4. Everything else the screen could list (scanners for one second, git helpers for milliseconds, zombie launchers)
   is either already in `:jobs` with more context, invisible to polling, or not stoppable by the user.

H7 (value over `:jobs`, Forward, containers, Ports) is **not demonstrated** by this spike: on Linux, for the three
scenarios, the screen would repeat `:jobs` with PIDs and process names added.

## Proposal: go or no-go on S-03a to S-03d

This is a proposal. The decision is the user's. The evidence is Linux only; Windows and macOS could change the
costs, not the argument about value.

| Story | Proposal | Reason |
|---|---|---|
| S-03a, list the descendants read-only | **No-go** | The three scenarios show one process (native scan), one client process (container scan, by deduction) and a depth-4 tree that lasts seconds (clone). `:jobs` already lists the same work with its target and state. Short commands are invisible, orphans are invisible, the cost is 20 to 150 ms per read and grows with the machine. Nothing measured shows a question the user could not answer today |
| S-03b, add the safe stop | **No-go** | There is nothing worth stopping that is not already stoppable or deliberately not stoppable: scans stop through `:jobs`; a clone is kept uncancellable on purpose; stopping the direct child leaves its descendants running (measured), and stopping a container client may leave the container (unverified, documented by Docker) |
| S-03c, link to a workspace | **No-go** | The workspace is already the `:jobs` target of every DevDesk-started run. Linking arbitrary processes to a workspace would read the working directory of every process, which is the task-manager scope the brief ruled out |
| S-03d, link to a port | **No-go** | None of the three scenarios holds a listening port, and the forwards are in-process. The Ports tab already shows the sockets of a clone's transport with their PID |

Cheaper observations that came out of the spike, offered as raw material and not as stories: scanner containers
carry no label and no name, which is the actual obstacle to attributing and stopping them; and the launchers
that `Start()` without `Wait()` leave zombie children until DevDesk exits. If a concrete need appears later
(for example a leftover container or process that the user had to hunt by hand after a cancel), the first
thing to do is record it, then revisit S-03 with that case.

## Reproducing

The harness is not versioned. Shape: `go run` a program that starts `argv` with `exec.CommandContext`,
`Start` then `Wait`, with a goroutine that every 1 to 250 ms calls `process.NewProcess(os.Getpid())` and
walks `Children()` recursively, recording pid, ppid, `Name()`, `Cmdline()`, first and last sighting, and the cost of
each walk; a second mode cancels the context after a delay. Arguments used: the argv in the tables above;
the clone source was `git init` plus forty 1.5 MB random files (58 MB `.git`), cloned through `file://`.
