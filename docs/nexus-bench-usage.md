# Using the Nexus bench

A short, practical guide. For how the bench is built, read
[`architecture/nexus-bench.md`](architecture/nexus-bench.md) (design notes, not a manual).

## What it is for

The bench is a disposable Nexus Community instance on the k0s cluster `datomir`. You
deploy it, provision it, seed it and destroy it with `mise run nexus-bench:*` tasks.

Use it to:

- measure what a real Nexus answers (catalog, access, proxy with its upstream cut);
- record those answers as fixtures that the unit tests replay without any bench.

It is a development tool. The `dk` binary does not import it, and `mise run check` and
`mise run ci` never touch a cluster. `mise run test` needs no bench either: it replays the
recorded fixtures.

## Prerequisites

- A reachable k0s cluster, `kubectl` context `datomir` (`--context` or `NEXUS_BENCH_CONTEXT`
  to change it). mise installs `kubectl` 1.35 for the tasks that need it.
- `skopeo` on this machine for `up`, `seed`, `restore-upstream` and `reset`. mise does not
  install it.
- The bench reachable from this machine: REST, UI and path-based repositories on NodePort
  30481, the Docker connector of `docker-group` on NodePort 30482. They are tried on `127.0.0.1` and
  the node address (`--hosts` / `NEXUS_BENCH_HOSTS` overrides the list).
- You accept the Nexus Community EULA yourself: `--accept-eula` or `NEXUS_BENCH_ACCEPT_EULA=yes`.
  Read it first.

## Run it

Pass flags after `--`:

```bash
mise run nexus-bench:up -- --accept-eula
mise run nexus-bench:status
```

| Task | What it does |
|---|---|
| `nexus-bench:up` | Deploys Nexus and its upstream registry, waits, provisions, seeds, writes the descriptor. |
| `nexus-bench:provision` | Provisions a running Nexus again (idempotent). |
| `nexus-bench:seed` | Puts the data set in a bench that `up` provisioned. |
| `nexus-bench:status` | Reads the pod, REST, upstream, descriptor and data set. Changes nothing. |
| `nexus-bench:cut-upstream` | Scales the upstream to zero: the proxy serves only what it cached. |
| `nexus-bench:restore-upstream` | Brings the upstream back and puts the proxy back in service. |
| `nexus-bench:reset` | Deletes the Docker repositories and the upstream storage, then provisions and seeds again. Keeps the pod, namespace and passwords. |
| `nexus-bench:record` | Regenerates the recorded fixtures from the bench. |
| `nexus-bench:render` | Prints the manifests. Contacts nothing. |
| `nexus-bench:test` | Runs the integration tests (`DEVDESK_NEXUS_BENCH=1`, skipped when `CI` is set). |
| `nexus-bench:down` | Deletes the namespace and the stored passwords. There is no `destroy` task. |

`nexus-bench <subcommand> -h` lists every flag (`go run ./internal/nexusbench/cmd/nexus-bench -h`).
A flag beats its `NEXUS_BENCH_*` variable, which beats the default.

Main parameters (defaults from `up -h`):

| Variable | Default | Meaning |
|---|---|---|
| `NEXUS_BENCH_CONTEXT` | `datomir` | kubectl context |
| `NEXUS_BENCH_NAMESPACE` | `nexus-bench` | namespace, deleted by `down` |
| `NEXUS_BENCH_ACCEPT_EULA` | unset | the exact value `yes` accepts the EULA |
| `NEXUS_BENCH_ANONYMOUS` | `true` | anonymous access on or off |
| `NEXUS_BENCH_HTTP_NODEPORT` / `NEXUS_BENCH_DOCKER_NODEPORT` | `30481` / `30482` | NodePorts |
| `NEXUS_BENCH_IMAGE_TAG` | `3.98.1` | Nexus image tag |
| `NEXUS_BENCH_UPSTREAM_URL` | in-cluster `bench-upstream` on port 5000 | upstream of `docker-proxy` |
| `NEXUS_BENCH_SECRETS` | under `$XDG_STATE_HOME/devdesk/nexus-bench` | passwords, outside the repository |
| `NEXUS_BENCH_DESCRIPTOR` | `internal/nexusbench/descriptor.json` | descriptor file |

## Record fixtures

1. Bring the bench to the state the fixtures expect: `nominal`, or `upstream-cut` after
   `cut-upstream`.
2. Run `mise run nexus-bench:record -- --campaign access`. `--only 'catalog-*'` filters by name.
3. Check the diff of `internal/nexusfixture/fixtures/` and commit it.

`record` replays the requests through the loopback ports, writes the files only if every
request passed, redacts credentials and checks for leaks. Add `--state nominal|upstream-cut` to
make it read the upstream and refuse a bench in another state. With a cut upstream, use
`--request-timeout 45s`: the first read takes about 20 s.

## What is protected

- Without `--state`, `record` applies no state filter and can overwrite an `upstream-cut`
  fixture. Pass `--state` when the bench is not nominal.
- A HEAD that can cache an image in the proxy is sent only with `--allow-cache-effects`. The bench
  then needs a `reset`.
- A `record: pending` fixture is never versioned (`TestNoPendingFixtureIsVersioned`).
- The scenario tests that cut the upstream and reset the bench run only with
  `DEVDESK_NEXUS_BENCH=1` and `DEVDESK_NEXUS_BENCH_SCENARIOS=1`.
- Credentials go to `127.0.0.1` by default. `up` and `provision` take `--rest-host` with another host for the REST API
  only; `seed`, `reset` and `record` accept a loopback address only. A non-loopback host sends the admin password in clear text.
- NodePorts listen on every node interface and anonymous access is on by default: anyone on the
  LAN can read the data set.

## What the bench contains

- Repositories: `docker-hosted`, `docker-proxy` (mirrors the bench upstream) and `docker-group`
  (hosted then proxy). Hosted and proxy are also reachable by path on the HTTP port.
- Accounts: `admin`, `bench-pull` (read-only pull) and `anonymous`.
- Data set (8 images, `Dataset()` in `internal/nexusbench/dataset.go`): `api` 1.0.0 and 1.1.0,
  `team/api`, `tools/cli`, `shared/base` (hosted and upstream), `mirror/cached` (pulled through
  the proxy), `mirror/uncached`.
- Fixture campaigns in `internal/nexusfixture/fixtures/`: `catalog`, `cachedonly`, `access`.
- Dated measures in [`measurements/`](measurements/):
  [catalog](measurements/2026-11-17-nexus-catalog.md),
  [cached only](measurements/2026-11-20-nexus-cached-only.md),
  [access](measurements/2026-11-20-nexus-access.md). They are written in French.

## Limits

- One instance: Nexus `3.98.1-01` Community. Other versions are not measured.
- HTTP only, no TLS.
- No account restricted to one repository: filtering by privilege is not tested.
- No reverse proxy in front. A company Nexus behind one can differ in ways the bench does not
  reproduce: URL rewriting, the `realm`, `Location` and `Link` headers, other headers, TLS
  ended upstream.
- A measure that contradicts a criterion or the code is frozen and noted, not fixed in the same PR.

## Troubleshooting

- `up` fails on seeding: check `skopeo` is on `PATH`, or point `NEXUS_BENCH_UPSTREAM_URL` at an
  upstream of your own.
- `status` reports descriptor drift or a wrong data set: run `nexus-bench:reset`.
- A fixture refuses to record: the bench is not in the state the fixture declares. Fix the
  state, then re-run with `--state`.

## See also

- [`architecture/nexus-bench.md`](architecture/nexus-bench.md): design, parameters, recorded fixtures.
- [`architecture/network.md`](architecture/network.md) and
  [`architecture/registries.md`](architecture/registries.md): the DevDesk code the fixtures test.
