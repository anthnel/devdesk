# engine-vms: disposable Docker and Podman test VMs on Incus

OC-85 (DEVDESK-795). Two throwaway Incus virtual machines, run outside CI, for the
measurements and fixture recordings of DevDesk (OC-01, OC-09b, OC-58, OC-68) against
the engine matrix. Nothing here is part of `mise run ci`.

## What exists

On the Incus host (default `jakku.home.arpa`, reached over SSH), in the dedicated
project `devdesk-engine-tests` (label `user.devdesk=engine-tests`):

| VM | Engine | Also installed |
|---|---|---|
| `dd-docker` | Docker Engine, rootful | kind (docker provider), kubectl |
| `dd-podman` | Podman, rootful and rootless (user `dd`) | kind (podman provider, rootless), kubectl, `docker-compose` (pinned) |

Each VM holds both lines of the matrix, "N" (latest stable) and "N-1" (previous
major for Docker and Podman, previous minor for kind). `versions.env` pins them
(chosen 2026-11-26): Docker 29.10.1 / 28.5.2, Podman 6.2.1 / 5.8.11, kind 0.33.0
(node v1.37.0) / 0.32.0 (node v1.36.1), kubectl 1.37.3. Node images are pinned by digest.
Switch inside a VM with `dd-use n` or `dd-use n1`; a snapshot `clean` holds the
line N state.

Why full VMs and not system containers: kind and rootless Podman need a real
systemd, cgroup v2 with delegated controllers, a kernel of their own and the
ability to load modules; an Incus container shares the host kernel and its cgroup
tree, which would measure the host and not the engine. Ubuntu 24.04 (kernel 6.8,
cgroup v2). User `dd` (uid 2000) has linger enabled and the controllers `cpu
cpuset io memory pids` delegated (`user@.service.d/delegate.conf`). Ports stay
above 1024. One VM per engine rather than per version: the lines are interchangeable
and this fits the resource ceiling.

Size: 2 vCPU, 3 GiB RAM and a 30 GiB thin disk per VM, set in the `dd-engine`
profile. Stay within half of the host's free resources; stop the VMs when idle
(`reset.sh --stop`).

## Use

```bash
mise run engine-vms:create                 # idempotent: project, profile, VMs, clean snapshot
mise run engine-vms:verify -- --smoke      # versions vs versions.env, run a container, make and delete a kind cluster
mise run engine-vms:verify -- --matrix     # check both lines (n and n1)
mise run engine-vms:reset -- podman n1     # restore the clean snapshot, then select line n1
mise run engine-vms:destroy                # asks first; removes only this project
```

The same scripts run directly (`tools/engine-vms/create.sh`). Variables:
`DD_ENGINE_VMS_HOST` (SSH host, or `local`), `DD_ENGINE_VMS_PROJECT` (must start with
`devdesk-engine-tests`), `DD_ENGINE_VMS_POOL`, `DD_ENGINE_VMS_NETWORK`, `DD_ENGINE_VMS_IMAGE`.

Access from the development machine:

```bash
ssh jakku.home.arpa
incus --project devdesk-engine-tests exec dd-docker -- bash
incus --project devdesk-engine-tests exec --cwd /home/dd dd-podman -- runuser -u dd -- \
  env XDG_RUNTIME_DIR=/run/user/2000 bash       # rootless shell
```

Compose providers in `dd-podman`: the standalone `docker-compose` is pinned in `versions.env`
and installed by `provision.sh` in `/usr/local/bin` (idempotent, sum checked, hard failure on a
mismatch). `podman-compose` is not provisioned: install the Ubuntu package without its
dependencies (`apt-get download podman-compose`, then `dpkg -i --force-depends`, after
`apt-get install python3-yaml python3-dotenv`); a plain `apt-get install podman-compose` pulls the
Ubuntu `podman` and `crun`, which then shadow the pinned static runtime. `docker-compose` talks
to the Podman API socket, which the static build does not start: run
`podman system service --time 900 unix://$XDG_RUNTIME_DIR/podman/podman.sock` first.
`create.sh` skips a VM that already has its `clean` snapshot, so a VM created before the pin
gets `docker-compose` only by running `vm/provision.sh` in it again (the snapshot does not hold it).
The two providers coexist. `podman compose` runs
the first provider it finds, `docker-compose` before `podman-compose`, so always choose one
explicitly per command: `PODMAN_COMPOSE_PROVIDER=/usr/local/bin/docker-compose podman compose ...`
(or `PODMAN_COMPOSE_PROVIDER=podman-compose`). `compose_providers` in `containers.conf` would
do the same but is not set, so nothing depends on a hidden default.

Run the campaigns inside the VM, restore with `reset.sh` afterwards.

## Contract runs

OC-09a1 (DEVDESK-806), labels and cleanup OC-09a2 (DEVDESK-943). `contract.sh` runs the engine contract tests (the `*_contract_test.go` files, see
`docs/architecture/network.md`, "Engine contracts") against the engine of a VM:

```bash
DEVDESK_ENGINE_CONTRACT=1 mise run engine-contract -- docker n        # Docker line N, rootful
DEVDESK_ENGINE_CONTRACT=1 mise run engine-contract -- podman n1 --mode rootless
DEVDESK_ENGINE_CONTRACT=1 mise run engine-contract -- podman n --mode rootful --no-reset
```

**Do not export `DEVDESK_ENGINE_CONTRACT`.** Set it for the one command, as above: an exported variable stays in
your shell and opens the contract gate for every later `go test`, which would then reach your own engine. No task
sets it for you.

Usage: `contract.sh verify <docker|podman> <n|n1> [--mode rootful|rootless] [--no-reset]`.

- **The gate comes first and applies on your machine.** Without `DEVDESK_ENGINE_CONTRACT=1` it prints `skipped` and
  exits 0; with `CI`, `GITHUB_ACTIONS`, `GITLAB_CI`, `BUILDKITE` or `TF_BUILD` set it prints `refused` and exits 1, even
  with the variable; invalid arguments exit 2. None of
  these reaches the VM host. The Go tests apply the same gate again inside the VM.
- **Steps.** It builds one `linux/amd64` test binary per package that has a contract (`-trimpath`; only files known to
  git count), restores the VM's clean snapshot on the requested line (skipped with `--no-reset`, the line installed
  in the VM must still match), copies the binaries and `versions.env` to `/tmp/ddc-<run>`, runs
  `-test.run '^TestContract'` with an explicit environment (`env -i`), and removes the directory, also on failure.
  Nothing is copied back; the exit code is the contracts'. It prints how many contracts passed, were skipped and
  failed; when none passed (an engine outside N and N-1 is skipped in verify mode) the exit code stays 0 and it says
  `nothing was verified`.
- **The image.** `CONTRACT_IMAGE` in `versions.env` (`docker.io/library/alpine@sha256:...`, pinned by the digest of
  the multi-arch index; the date and the source are in the comment, trust on first use like the other sums) is the
  only image the contracts start. After the reset `contract.sh` pulls it into the VM, once, as the user of the mode
  (`vm_as_mode`); this script is the only place anything is pulled, the contracts run with `--pull=never` and the Go
  code never pulls. The contracts get it as `DEVDESK_ENGINE_CONTRACT_IMAGE`. Bumping it means reading the digest of
  the new index again from the registry and updating the comment.
- **The lab mark.** A contract that creates or removes a resource refuses to run unless
  `DEVDESK_ENGINE_CONTRACT_RUN` is set (the script sets it) and `/etc/dd/current` exists and holds `n` or `n1`:
  the file `dd-use.sh` writes in each VM. It keeps a contract that creates something from running on your own
  engine by accident. It is not a defence against someone who creates that file as root on their own machine.
- **The sentinel.** After the pull the script starts a container named `ddc-sentinel-<run>` with **no label**, in the
  store the contracts use (the user's for rootless podman, root's otherwise). The contracts label what they create
  with `devdesk.test=<run>` and remove only that. When they are done, pass or fail, the script inspects the sentinel:
  if it is gone the cleanup touched something without the label, and the run fails with that message. It then removes
  the sentinel by its id. It removes nothing else in the engine, and never prunes.
- **Mode.** Docker is rootful only (`--mode rootless` with docker is refused). Podman defaults to rootless, the
  mode the user `dd` runs; `--mode rootful` runs it as root. Rootless runs go through the user session of `dd`.
- **Acts on the Incus host** (`DD_ENGINE_VMS_HOST`, `DD_ENGINE_VMS_PROJECT`, as above): never run it against a host
  you do not own. It touches only the VM of the requested engine.
- The probe templates the contracts use (`version` and `info` formats) were written from the plan and have **not been
  confirmed on the VMs yet**: the first run on Docker 29.10.1 / 28.5.2 and Podman 6.2.1 / 5.8.11 is that
  confirmation, and a template an engine rejects fails the contract with the engine's own message.
- The label filters and the `inspect` templates of the cleanup are, like the probe, **not confirmed on the VMs yet**.
- Not there yet: recording fixtures and `engine-contract:record` (OC-09a3).

## Threat model and limits

- The VMs are disposable and hold no secret: no registry credential, no token. Do
  not log in to a registry in them; use public images only.
- A kubeconfig made by kind stays in the VM (`~dd/.kube`); never copy it out with
  `incus file pull` or `kind export`. The API server listens on loopback only.
  Recorded fixtures must be redacted like any other (docs/architecture/nexus-bench.md).
- Membership of the `docker` group, and rootful Podman, equal root inside the VM,
  not on the host. The VM boundary is the isolation; the VMs share the host's
  network bridge (`incusbr0`) and outbound access to docker.io, quay.io, ghcr.io and
  registry.k8s.io (no proxy is configured). Do not expose the engine sockets.
- Downloads (kubectl, kind, Podman static build, docker-compose) are checked against the sha256
  in `versions.env`, taken from the release pages on 2026-11-26 (trust on first use).
  The Podman build is the third-party `mgoltzsche/podman-static`, not an
  official packaging. Docker comes from the signed Docker apt repository.
- Cleanup: `destroy.sh` deletes the two VMs, the profile, the images and the project,
  and refuses to go on if the project holds anything else. No script touches another
  project, instance, volume, network or profile.
- Docker's rate limits on anonymous pulls apply to the shared host address.
- Other `incus` users on the host can see and delete these VMs; they have no
  further protection than the project.

## Known behavior

- The first `kind create cluster` after a snapshot restore often fails once (cold
  disk cache, kubeadm times out) and works on the second try; `verify.sh --smoke`
  retries once. Run one campaign at a time per VM: the VMs have 2 vCPU and 3 GiB.
- Verified on 2026-11-27 with `verify.sh --smoke --matrix`: both engines, lines N
  and N-1, a kind cluster created and deleted in each (rootless for Podman).
