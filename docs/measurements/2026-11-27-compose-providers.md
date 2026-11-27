# Compose providers under Podman: `podman-compose` and `docker-compose` (OC-01, DEVDESK-796, H8)

Measurement note of 2026-11-27. It completes section 6 and hypothesis H8 of `2026-11-27-engines.md`, which stays
frozen: that file said only `podman-compose` 1.0.6 had been measured. Nothing in product code was touched.

## Machine and versions

| | |
|---|---|
| Machine | Incus test VM (label: "engine test host, Podman VM"), Ubuntu 24.04, kernel 6.8, cgroup v2, 2 vCPU, 3 GiB, built by `tools/engine-vms` |
| Podman | **6.2.1 (N)** and **5.8.11 (N-1)**, third-party static build, **rootless** (`podman info -f '{{.Host.Security.Rootless}}'` = `true`, uid 2000) |
| `podman-compose` | 1.0.6 (Ubuntu package, installed without its `podman` dependency) |
| `docker-compose` | 5.8.0, standalone binary, sha256 `b8840f5008c447f41fce0a57ae3adeab42c89b1eb6e6012524bcabbfb4ffbaf6` taken from the release's own `docker-compose-linux-x86_64.sha256`, pinned in `versions.env` and checked at install |
| Image | `alpine:3.20` |

## Commands

Compose file `~/m/compose.yaml`: services `db` and `web` (both `alpine:3.20`, `sleep 600`), `web` has `depends_on: [db]`.
For `docker-compose`, the Podman API socket was started first: `podman system service --time 900 unix:///run/user/2000/podman/podman.sock`.
Per provider and per Podman line:

```
export PODMAN_COMPOSE_PROVIDER=<podman-compose | /usr/local/bin/docker-compose> PODMAN_COMPOSE_WARNING_LOGS=false
podman compose -p ddmproj up -d
podman ps --filter label=com.docker.compose.project --format '{{.Names}}'
podman inspect --format '{{json .Config.Labels}}' <name>
podman ps --filter label=io.podman.compose.project=ddmproj --format '{{.Names}}'
podman network inspect --format '{{json .Labels}}' ddmproj_default
podman compose -p ddmproj down -t 1
```

## Results (identical on Podman 6.2.1 and 5.8.11)

| | `podman-compose` 1.0.6 | `docker-compose` 5.8.0 |
|---|---|---|
| container names | `ddmproj_db_1`, `ddmproj_web_1` | `ddmproj-db-1`, `ddmproj-web-1` |
| `com.docker.compose.project` | `ddmproj` | `ddmproj` |
| `io.podman.compose.project` | `ddmproj` | **absent** |
| `com.docker.compose.service`, `.container-number`, `.project.working_dir` | present (`web`/`db`, `1`, absolute path) | present |
| `com.docker.compose.project.config_files` | `compose.yaml` (relative) | `/home/dd/m/compose.yaml` (absolute) |
| `com.docker.compose.config-hash` | **absent** (own `io.podman.compose.config-hash`) | present |
| `com.docker.compose.oneoff` | **absent** | `False` |
| `com.docker.compose.depends_on` | **absent** | `""` on `db`, `db:service_started:false` on `web` |
| `com.docker.compose.image`, `.version` | **absent** (`io.podman.compose.version` = 1.0.6) | present (`sha256:...`, `5.8.0`) |
| other own labels | `PODMAN_SYSTEMD_UNIT=podman-compose@ddmproj.service` | none |
| filter `label=com.docker.compose.project=ddmproj` | both containers | both containers |
| filter `label=io.podman.compose.project=ddmproj` | both containers | **no container** |
| network `ddmproj_default` labels | `com.docker.compose.project`, `io.podman.compose.project` | `com.docker.compose.{project,network,config-hash,version}`, no `io.podman.compose.*` |
| network after `down` | **left in place** | removed |

Rootless only; rootful was not re-measured here (section 6 of the existing file found no rootful/rootless difference for `podman-compose`).
No difference between Podman 6.2.1 and 5.8.11 for either provider.

## Conclusion on H8

**H8 is confirmed.** The set of labels, the container name separator, the form of `config_files`, and the presence of
`io.podman.compose.project` all depend on the Compose provider.

What OC-51 (Project column) can read reliably on both providers: **`com.docker.compose.project`**, present on every
container (and on the network) with both providers, on Podman 6.2.1 and 5.8.11. Do not read `io.podman.compose.project`
alone (absent with `docker-compose`). Do not depend, for the project name, on the container name (`_` or `-`
separator). Also do not rely on `config-hash`, `oneoff`, `depends_on`, `image`, `version` or an absolute `config_files`
under Podman: they exist only with `docker-compose`.

## Limits

- One run per configuration, one image, one two-service project, no profile, no `run --rm` (a `oneoff=True` container was not made).
- `docker-compose` needs the Podman API socket; a provider started another way (system socket, `DOCKER_HOST`) was not measured.
- Other providers or versions (a newer `podman-compose`, `docker-compose` plugin form) are not measured.
- Podman is the third-party static build, not a distribution package.

## Open point

The H8 row of `2026-11-27-engines.md` (frozen) still reads "not measurable here"; this note supersedes it. No link was added there.
