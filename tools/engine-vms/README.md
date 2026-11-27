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
| `dd-podman` | Podman, rootful and rootless (user `dd`) | kind (podman provider, rootless), kubectl |

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
- Downloads (kubectl, kind, Podman static build) are checked against the sha256
  in `versions.env`, taken from the release pages on 2026-11-26 (trust on first use).
  The Podman build is the third-party `mgoltzsche/podman-static`, not an
  official packaging. Docker comes from the signed Docker apt repository.
- Cleanup: `destroy.sh` deletes the two VMs, the profile, the images and the project,
  and refuses to go on if the project holds anything else. No script touches another
  project, instance, volume, network or profile.
- Docker's rate limits on anonymous pulls apply to the shared host address.
- Other `incus` users on the host can see and delete these VMs; they have no
  further protection than the project.
