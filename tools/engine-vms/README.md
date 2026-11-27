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
