# The Nexus bench — `internal/nexusbench`

> Looking for how to run the bench? See [`../nexus-bench-usage.md`](../nexus-bench-usage.md).

> DevDesk architecture notes. Referenced from `.claude/CLAUDE.md`;
> read this file when working on the code it describes.

## What it is

DEVDESK-504 (NX-01): a disposable Nexus Community instance on the k0s cluster
of `datomir`, deployed, provisioned over REST and destroyed with one command, so
the registry measurements of the NX stories run against a real Nexus. It is a
development tool: the `dk` binary does not import it, nothing in `mise run
check` or `mise run ci` touches a cluster, and integration tests skip unless
`DEVDESK_NEXUS_BENCH=1` is set (and always when `CI` is set).

Status: the parameters (step 1), the manifest rendering (step 2), the `kubectl`
runner, errors and preflight (step 3), the deployment and its wait (step 4), the
secrets store and the descriptor (step 5), the REST client and provisioning
(step 6), the destruction (step 7), the command with its mise tasks (step 8) and
the guarded integration tests with the source guards (step 9) exist. The
preflight only reads the cluster; the writes are the two applies of `Deploy`,
the REST calls of `Provision` and the namespace delete of `Destroy`.

Validated on `datomir` on 2026-11-16 (step 10, see "First real run" below): `up`,
a second `up`, the three read-only integration tests and the destructive
lifecycle all ran against a real Nexus 3.98.1-01 Community on k0s. One
hypothesis was wrong (a proxy whose upstream does not resolve is refused), and
`Provision` now copes with it.

## Decisions (plan `.claude/plans/DEVDESK-504-plan.md`)

- **D-A** A Go package, `internal/nexusbench`, with a `package main` under
  `cmd/nexus-bench`, driven by `mise run nexus-bench:*` tasks outside `ci`.
  `kubectl` through `os/exec`, no `client-go`, no bash.
- **D-B** Namespace `nexus-bench`, context `datomir`, NodePorts 30481 (REST,
  UI) and 30482 (Docker connector); 30483-30489 reserved for the next NX
  stories. Refused: namespaces `default`, `kube-*`, `app-*`, `flux-system`,
  `local-path-storage`, `traefik`, `kyverno`, `observability`, `plane`; NodePorts
  outside 30000-32767 or equal; an image from `tvp-zot.incus:5000` (Kyverno
  `verify-tvp-images` admits signed images only).
- **D-C** Resources 500m/2 CPU, 3Gi/4Gi memory, 5Gi PVC, JVM 1 GiB heap and 1 GiB
  direct memory, image `docker.io/sonatype/nexus3:3.98.1`. To be confirmed on
  the first real run (H-RES).
- **D-D** A deterministic, versioned `descriptor.json` (no timestamp, pod name or
  secret); secrets in a `0600` file outside the repository, shared by all
  worktrees.
- **D-E** Idempotent REST provisioning: read, then create or replace; admin
  password changed with the new one written first; the EULA is accepted only
  with `NEXUS_BENCH_ACCEPT_EULA=yes`.
- **D-F** One typed error with a cause and a check to run; the preflight runs
  before any write; the namespace carries a `managed-by` label and `down`
  refuses to delete one without it.

## Parameters (`params.go`)

Precedence: flag, then `NEXUS_BENCH_*` variable, then default. `FromEnv(base,
getenv)` applies the variables onto `base` and returns a new value (the base is
not changed); an empty variable counts as unset; a value that does not parse is
an error naming its variable. `Validate()` runs before any call to the cluster
and reports every problem at once. Every value that reaches a manifest is
checked against a strict pattern, newlines included. The patterns are meant to
keep a value from injecting YAML on their own, but the templates still
writes every value quoted: the validation is the first guard, not the only one.

| Field | Variable | Default |
|---|---|---|
| `Context` | `NEXUS_BENCH_CONTEXT` | `datomir` |
| `Namespace` | `NEXUS_BENCH_NAMESPACE` | `nexus-bench` |
| `HTTPNodePort` | `NEXUS_BENCH_HTTP_NODEPORT` | `30481` |
| `DockerNodePort` | `NEXUS_BENCH_DOCKER_NODEPORT` | `30482` |
| `CPURequest` / `CPULimit` | `NEXUS_BENCH_CPU_REQUEST` / `_CPU_LIMIT` | `500m` / `2` |
| `MemoryRequest` / `MemoryLimit` | `NEXUS_BENCH_MEMORY_REQUEST` / `_MEMORY_LIMIT` | `3Gi` / `4Gi` |
| `StorageSize` | `NEXUS_BENCH_STORAGE_SIZE` | `5Gi` |
| `JVMOpts` | `NEXUS_BENCH_JVM_OPTS` | `-Xms1g -Xmx1g -XX:MaxDirectMemorySize=1g -Djava.util.prefs.userRoot=/nexus-data/javaprefs` |
| `Image`, `ImageTag` | `NEXUS_BENCH_IMAGE`, `_IMAGE_TAG` | `docker.io/sonatype/nexus3`, `3.98.1` |
| `UpstreamImage`, `UpstreamImageTag` | `NEXUS_BENCH_UPSTREAM_IMAGE`, `_UPSTREAM_IMAGE_TAG` | `docker.io/library/registry`, `3.0.0` |
| `UpstreamURL` | `NEXUS_BENCH_UPSTREAM_URL` | `http://bench-upstream.<namespace>.svc.cluster.local:5000` |
| `Anonymous` | `NEXUS_BENCH_ANONYMOUS` | `true` |
| `AcceptEULA` | `NEXUS_BENCH_ACCEPT_EULA` | `false`; true only for the exact value `yes` |
| `Hosts` | `NEXUS_BENCH_HOSTS` (comma list) | none: derived (`127.0.0.1`, node InternalIP) |
| `Timeout`, `DeleteTimeout` | `NEXUS_BENCH_TIMEOUT`, `_DELETE_TIMEOUT` | `10m`, `3m` |
| `SecretsPath`, `DescriptorPath` | `NEXUS_BENCH_SECRETS`, `NEXUS_BENCH_DESCRIPTOR` | none: derived |

Rules beyond the shape of each value:

- CPU, memory and storage quantities are greater than zero (`0`, `0m`, `0Gi` are
  refused), and a request never exceeds its limit (CPU compared in milli-cores,
  memory in bytes, units mixed freely: `2000m` equals `2`). The error names both
  variables, so a lone `NEXUS_BENCH_MEMORY_LIMIT=2Gi` against the default `3Gi`
  request is refused.
- `JVMOpts` is 1-512 characters among letters, digits, space and `_ . , : = / + @ %
  -`; it starts with a letter, a digit, or `-` followed by a non-space (so not
  `- x`, `@x`, `%x`, `,x`, `:x`), holds no `": "` and does not end with `:`, so it is
  a plain YAML scalar by itself.
- `Hosts` entries are IP addresses or host names (letters, digits, `.`, `-`,
  253 characters at most): no port, no whitespace.
- `UpstreamURL` is `http://` or `https://` plus a host, without credentials, made
  of URL characters only (no `@`, no whitespace) and with a port, when present,
  between 1 and 65535.
- `Image`/`ImageTag` and `UpstreamImage`/`UpstreamImageTag` go through one
  `validateImage(imageEnv, tagEnv, image, tag)`: each error names the variable of
  its own pair. Both refuse the `tvp-zot.incus:5000` prefix (Kyverno
  `verify-tvp-images`, unsigned bench images).
- `UpstreamImageRef()` is `UpstreamImage:UpstreamImageTag`. `UpstreamIsBench()` is
  true when `UpstreamURL` is the in-cluster URL of `Namespace`, i.e. the bench
  deploys its own upstream rather than pointing the proxy at a custom one.
- With a custom `NEXUS_BENCH_UPSTREAM_URL` (`UpstreamIsBench()` false), `up` still
  applies the manifests of the bench upstream: `Render` ignores the URL, so the
  `bench-upstream` Deployment, claim and Service are created and run although the
  proxy does not read them (decision of the user, 2026-11-17: keep it). What a custom
  URL does change: `up` does not scale, seed or wait for that upstream, and
  `Provision` does not allow its host through the SSRF protection.
- The CLI flags `--upstream-image` and `--upstream-image-tag` are common flags; a
  non-default value makes `describesDefaultBench` false (descriptor not to be
  committed).
- Paths hold no control character and are 4096 bytes at most.

`NEXUS_BENCH_NAMESPACE` alone moves `UpstreamURL` to the new namespace, unless
the base already carried a custom upstream or `NEXUS_BENCH_UPSTREAM_URL` is set.
The context name is limited to letters, digits, `.`, `_`, `@` and `-`, because
it names a directory of the secrets path.

## Manifests (`render.go`, `manifests/`)

`Render(p Params) ([]byte, error)` calls `p.Validate()` first (an invalid value
returns Validate's error, naming its variable, and no bytes) and writes a
multi-document YAML stream separated by `---`, in this order: Namespace, PVC,
Deployment, Service (Nexus), then PVC, Deployment, Service (the upstream
registry): seven documents. `RenderNamespace(p)` returns the first document alone, so
that `up` can apply the Namespace before the rest. The output is deterministic
(no map iteration, no timestamp); the golden of the defaults is
`testdata/render-default.golden.yaml`, rewritten with
`go test ./internal/nexusbench -run TestRenderGolden -update` and reviewed as a
diff.

| File (`go:embed`, `text/template`) | Object |
|---|---|
| `00-namespace.yaml.tmpl` | Namespace, labels `app.kubernetes.io/managed-by` and `part-of` = `devdesk-nexus-bench`; no `pod-security.kubernetes.io/*` label |
| `10-pvc.yaml.tmpl` | PVC `nexus-data`, `ReadWriteOnce`, `StorageSize`, no `storageClassName` (the cluster default class, `local-path`) |
| `20-deployment.yaml.tmpl` | Deployment `nexus`: 1 replica, `Recreate`, one container (`ImageRef`), ports `http` 8081 and `docker-group` 8082, requests and limits, env `INSTALL4J_ADD_VM_PARAMS`, PVC on `/nexus-data`, `startupProbe` on `/service/rest/v1/status` (10 s x 60 = 10 min), `readinessProbe` (same path), no `livenessProbe`, UID and GID 200, `runAsNonRoot`, no privilege escalation, `drop: [ALL]`, `RuntimeDefault` seccomp, no service account token, no service links |
| `30-service.yaml.tmpl` | Service `nexus`, `NodePort`: `http` 8081 to `HTTPNodePort`, `docker-group` 8082 to `DockerNodePort` |
| `40-upstream-pvc.yaml.tmpl` | PVC `bench-upstream-data`, `ReadWriteOnce`, a fixed `1Gi` (`StorageSize` sizes Nexus only), no `storageClassName`; label `app.kubernetes.io/name: bench-upstream` on the three upstream objects |
| `50-upstream-deployment.yaml.tmpl` | Deployment `bench-upstream`: **no `replicas` field** (`kubectl scale` owns it, so a server-side apply of the whole stream neither conflicts with it nor puts the upstream back after it was cut), `Recreate`, one container (`UpstreamImageRef`), port `registry` 5000, env `OTEL_TRACES_EXPORTER=none` (distribution v3 traces by default), fixed requests 50m / 64Mi and limits 500m / 256Mi, PVC on `/var/lib/registry`, `readinessProbe` `GET /v2/`, UID and GID 1000, `runAsNonRoot`, no privilege escalation, `drop: [ALL]`, `RuntimeDefault` seccomp, no service account token, no service links |
| `60-upstream-service.yaml.tmpl` | Service `bench-upstream`, `ClusterIP` only: `registry` 5000 to the container port `registry`; reached by `UpstreamURL` inside the cluster and by a port-forward from the host, never by a node port |

Quoting: the templates see only a `manifestData` built from validated values,
and every string goes through the `q` template function (`strconv.Quote`, a
valid YAML double-quoted scalar), so `true`, `null`, `123`, `0x1F`, `12:30:45` or
`2001-01-23` in `NEXUS_BENCH_JVM_OPTS` decode as strings (`TestRenderRoundTripsTrickyJVMOpts`,
`TestRenderQuotesEveryValue`). Quoted: namespace, image references (Nexus and upstream), JVM options,
CPU and memory quantities, storage size, and the fixed values of the upstream
documents. Not quoted: the two NodePorts (integers),
and the literals of the template (names, labels, container ports).

What the render does not carry: the context, the upstream URL, `Anonymous`,
`AcceptEULA`, `Hosts`, timeouts and paths (a change of any of them leaves the
bytes unchanged, `TestRenderIgnoresParamsTheManifestsDoNotCarry`); no Secret, no
ConfigMap, no `envFrom` or `valueFrom`, no hostPath, no object of cluster scope
but the Namespace; the word `password` or `secret` never appears. The admin
password is read from the pod and written to the secrets file by the
provisioning step, not by a manifest.

## Errors, runner and preflight (`errors.go`, `kubectl.go`, `preflight.go`)

**Errors.** Every failure of the bench is a `*BenchError{Kind, Cause, Check, Err}`.
`Error()` is `"<Cause>. Check: <Check>"`: a probable cause and what to run or look
at (a `kubectl` command, or a `mise` task), in English. `Err` is kept for
`errors.Is`/`errors.As` and is never printed: the text of a failed sub-process can
carry a credential, and neither the stderr of kubectl nor its error reaches a
message. `ErrKind` holds the kinds of D-F in the order the bench meets them:
`KubectlMissing`, `ContextNotFound`, `ClusterUnreachable`, `Forbidden`,
`NamespaceForeign`, `NamespaceMissing`, `NodePortTaken`, `AdmissionDenied`,
`PodPending`, `ImagePull`, `Crash`, `Deadline`, `RESTUnreachable`,
`ProvisionFailed`. The zero value is `Unclassified`, so an error nobody classified
never passes for a named kind. `SkopeoMissing`, `RegistryUnreachable`,
`RegistryRefused` and `SeedFailed` (DEVDESK-505) were appended after
`ProvisionFailed`, so no earlier value moved (`TestSkopeoErrKindsAreAppendedAtTheEnd`).

`classifyKubectl(stderr)` is the fallback for what the exit status and the JSON
cannot tell apart; kubectl's wording changes between versions, so it matches
lower-cased fragments and returns `Unclassified` for an unknown text. Order of the
rules: an admission webhook that denied (even reported as `Forbidden`) is
`AdmissionDenied`; `provided port is already allocated` or `... not in the valid
range` is `NodePortTaken`; `forbidden` is `Forbidden`; `Unable to connect to the
server`, `connection refused`, `i/o timeout`, `TLS handshake timeout` are
`ClusterUnreachable`.

**Runner.** `Runner.Run(ctx, stdin, args...)` returns both streams even on
failure. `NewExecRunner(context)` finds `kubectl` with `exec.LookPath` (a
`KubectlMissing` error otherwise, which names `kubectl` and `mise install`) and
puts `--context <context>` before every call, so callers never pass it.
`kubectl.go` is the only non-test file of the package that starts `kubectl` (the commands of `Runner` and the port-forward, see "The two process doors" below); the other two processes are the `git check-ignore` of the secrets store (see below) and `skopeo` (`skopeo.go`).

**Preflight.** `RunPreflight(ctx, r, p, op) (PreflightResult, error)` with `op` one
of `OpUp`, `OpProvision`, `OpDown`, `OpStatus`, `OpSeed`, `OpCut`, `OpRestore`, `OpReset` (appended, no earlier value moves). Checks, in this order, stopping at
the first failure:

1. `p.Validate()`: invalid parameters are refused before any call.
2. `kubectl config get-contexts -o name`: the exact context name is listed, else
   `ContextNotFound`. Nothing else is asked.
3. `kubectl get --raw /readyz`: the API server answers, else `ClusterUnreachable`;
   a refusal (stderr `forbidden`: expired token, 403) is `Forbidden` instead, through
   the same `classifyKubectl` as every other call.
4. `kubectl auth can-i <verb> <resource> [--subresource=<sub>]` (`-n <ns>`, `-A` or
   nothing). A subresource is a flag, never `pods/exec` in the resource: kubectl
   reads that as the object `exec` of the pods and answers about the wrong thing
   (the NX-01 question `create pods/exec` had this defect and is corrected here:
   it is now `create pods --subresource=exec`). A refusal still names
   `pods/exec` in its cause, and its `Check` carries the flag:
   `up` needs create and patch `namespaces` (cluster; a server-side apply is a
   PATCH, also on a second run), create `persistentvolumeclaims`, `services`,
   `deployments` and `pods` `exec` (namespace), list `services` (`-A`),
   patch `deployments`, `services` and `persistentvolumeclaims`, and get and list
   `pods` and `persistentvolumeclaims` (the wait of `Deploy`), all in the
   namespace (no manifest creates a Secret, so none is asked), then, asked last
   so the fourteen before stay where they were, patch `deployments`
   `--subresource=scale` (the upstream is scaled), create `pods`
   `--subresource=portforward` and get `services` (the seed
   that follows reaches the upstream through a port-forward, which reads the
   Service first);
   `provision` needs create `pods` `exec`, get `deployments` and list `pods`
   (namespace); `down` needs delete `namespaces`; `seed` needs, in this order, get
   `deployments`, list `pods`, get `pods`, get `services`, get and list
   `persistentvolumeclaims` (EnsureUpstream reads the claim on every turn of its
   wait, so a refusal comes before the scale write), create `pods`
   `--subresource=portforward` and patch `deployments` `--subresource=scale`
   (namespace); `cut` needs, in this order, get `deployments`, list `pods` and patch
   `deployments` `--subresource=scale` (no tunnel, no Service, no claim); `restore`
   needs exactly the list of `seed` (the same slice, `seedPermissions`); `reset` needs
   that list, then delete, create and patch `persistentvolumeclaims` (the
   server-side apply of an absent claim needs create) and never `pods` `exec` (a
   reset does not change the admin password), all in the namespace;
   `status` asks nothing. A `no` arrives on stdout with a non-zero exit, so stdout is read
   first: `Forbidden`, naming the verb and the resource.
5. `kubectl get namespace <ns> -o json`: absent is fine for `up` and `down` (nothing
   to delete) and `NamespaceMissing` for every other operation; present
   without the label `app.kubernetes.io/managed-by=devdesk-nexus-bench` is
   `NamespaceForeign` for every operation (the plan lists `up` and `down`;
   `provision` and `status` refuse too, so the bench never runs `exec` in a
   namespace it did not create).
6. `up` only (`seed` does not read the Services of the cluster), `kubectl get services -A -o json`: a Service other than `nexus` of
   our namespace that holds `HTTPNodePort` or `DockerNodePort` is `NodePortTaken`,
   naming `<namespace>/<name>` and the port. Our own Service is accepted, which is
   what makes a second `up` possible.
7. `up`, `provision` and `status` only (`Op.readsNode`), `kubectl get nodes -o json`
   (`seed`, `cut`, `restore` and `reset` go through a loopback port-forward or
   need no address, and no can-i covers `get nodes`, so their `NodeIP` is empty): `NodeIP` is the first
   `InternalIP`; none is not an error (`NodeIP` stays empty).

A call that fails in a way the checks above did not expect goes through
`classifyKubectl` (a `forbidden` stderr becomes `Forbidden`, anything unknown
`Unclassified`). Every call that reaches the API server carries
`--request-timeout=10s`.

**No write before the end of the preflight.** The preflight issues `config`,
`get` and `auth can-i` only, never sends stdin, and a failure at any point leaves
the cluster untouched. The tests run it against a scripted fake runner that fails
the test on any call outside the six above and records every write verb
(`apply`, `create`, `delete`, `patch`, `exec`, ...) it would have seen. The
`Runner` of the later steps (apply, delete) is only called after `RunPreflight`
returned without an error.

## Secrets and descriptor (`secrets.go`, `descriptor.go`)

**Secrets.** `Secrets{AdminPassword, PendingAdminPassword, PullPassword}` (JSON
`adminPassword`, `pendingAdminPassword`, `pullPassword`; the last two are omitted
when empty) is kept by a `SecretStore` (`Load`, `Save`, `Remove`), implemented by
`NewFileStore(path)`. `PendingAdminPassword` is the new admin password, written
before it is sent to Nexus, so an interrupted change loses nothing.

- Path: `DefaultSecretsPath(context, namespace, getenv)` is
  `$XDG_STATE_HOME/devdesk/nexus-bench/<context>/<namespace>/secrets.json`, else
  `$HOME/.local/state/...` (`USERPROFILE` when `HOME` is unset); an empty variable
  counts as unset, none of them set is an error, and `NEXUS_BENCH_SECRETS`
  overrides the whole path. It is outside every repository and shared by all the
  worktrees, so a worktree never keeps a stale admin password of its own.
- Modes: the file is `0600`, the folders `Save` creates are `0700`. The write is
  atomic (`writeFileAtomic`): a temporary file in the same folder, `chmod`, sync,
  rename, then a best-effort sync of the folder so the new name survives a power
  cut (errors ignored: Windows and some file systems cannot sync a folder); the
  temporary file is removed when anything fails, so a failed `Save`
  leaves no partial file and a file left by someone else with a wider mode is
  replaced, not kept. POSIX modes are not meaningful on Windows (the test skips).
- File name: `NewFileStore` refuses a path whose file name is not `secrets.json`
  (`Remove` deletes that file, and a mistyped `NEXUS_BENCH_SECRETS` must not point
  it at `~/.ssh/id_ed25519`).
- Mode on read: `Load` refuses a file with a group or other bit (`mode & 0o077`),
  outside Windows, with a `BenchError` that never shows the content and says
  `chmod 600 <path>`.
- Display: `Secrets` has `String`, `GoString` and `LogValue` methods that all give
  `Secrets{redacted}`, so no fmt verb (`%v`, `%+v`, `%#v`, `%s`, `%q`) and no slog
  handler prints a password (`TestSecretsAreRedactedWhenFormatted`). The JSON of
  the file is unaffected.
- `Load` returns `(zero, false, nil)` when the file is absent. A file that is not a
  valid JSON object is a `BenchError` that names the path and never shows the
  content (the underlying error is kept in `Err`, which is not printed).
- `Remove` deletes the file, then the namespace folder when it is empty; it
  succeeds when there is nothing to remove, and a folder holding anything else
  stays.
- Git guard: `NewFileStore` refuses an empty path and touches nothing on disk.
  It walks up from the file's folder looking for a `.git` entry (directory or
  file, so a worktree counts); with none, the path is accepted without asking
  git. With one, it runs `git -C <root> check-ignore -q -- <path>`: ignored is
  accepted; not ignored, or git unavailable, is a `BenchError` that says to set
  `NEXUS_BENCH_SECRETS` outside the repository. The process runs with the caller's
  environment minus `GIT_DIR`, `GIT_WORK_TREE`, `GIT_INDEX_FILE`, `GIT_COMMON_DIR`,
  `GIT_OBJECT_DIRECTORY`, `GIT_NAMESPACE`, `GIT_CEILING_DIRECTORIES`,
  `GIT_CONFIG_PARAMETERS`, `GIT_CONFIG_COUNT`, `GIT_CONFIG_KEY_*`,
  `GIT_CONFIG_VALUE_*`, `GIT_CONFIG_GLOBAL`, `GIT_CONFIG_SYSTEM` and
  `GIT_CONFIG_NOSYSTEM` (`gitEnvironment`), then `GIT_CONFIG_GLOBAL=<devnull>` and
  `GIT_CONFIG_NOSYSTEM=1`, so the verdict is about the repository of the path and
  its own ignore rules, not about a repository or a `core.excludesFile` the
  caller's variables or configuration point at (a path ignored only by a global
  excludes file is therefore refused). That call is the only process
  outside `kubectl.go` and `skopeo.go`; it lives in the package variable `gitIgnores`, so it can
  be replaced and found by a source guard.

**Descriptor.** `BuildDescriptor(p, DeployResult, ProvisionResult, SeedResult)`
returns the `Descriptor` of schema 2 (D-D, D-M; schema 1 was NX-01's):
`schemaVersion`, `kubeContext`, `namespace`,
`image`, `nexusVersion`, `edition`, `eulaAccepted`, `hosts`, `rest`,
`dockerConnectors`, `repositories`, `realms`, `accounts`, `registryAccess`,
`upstream`, `dataset`, in that order, which is the
order of the struct fields. The three sections of schema 2 (DEVDESK-543):

- `registryAccess`: `scheme` `http`, `tlsVerify` `false`, `routing` `path` (a
  client reaches the Docker repositories by path on the HTTP NodePort, plain
  HTTP; true on the real bench since `docker.pathEnabled` is set, H-PATH).
- `upstream`: `service` `bench-upstream`, `image` `Params.UpstreamImageRef()`,
  `url` `Params.UpstreamURL` (the one `docker-proxy` mirrors), `persistent`
  `true`, `exposure` `port-forward` (the upstream has no NodePort; a client
  reaches it through `kubectl port-forward`).
- `dataset`: one entry per image of the `SeedResult`, in its order and copied
  (never shared): `name`, `tag`, `digest`, `location` (`docker-hosted` or
  `upstream`), `cachedInProxy` (= `DatasetImage.PullThrough`: Seed pulled it
  through `docker-proxy`). It comes from the `SeedResult`, not from `Dataset()`:
  an empty result writes `"dataset": []`, never `null`.

`ProxyPending` false, the nominal case of NX-02, lists `docker-proxy` and makes
it a member of `docker-group`. `hosts` is `Params.Hosts` when set, else `127.0.0.1`
then `DeployResult.NodeIP` when it is not empty; the URLs are `http://<host>:<nodePort>`
for each host (`RESTBaseURL(host, p)` is the exported REST origin: brackets for
IPv6, `%25` for a zone; the deployment, the descriptor and the command of step 8
all build it there). `DeployResult{ReachableHosts, NodeIP}` (step 4) and
`ProvisionResult{NexusVersion, Edition, EULAAccepted}` (step 6) are defined in
`descriptor.go` and reused by those steps. The account `anonymous` always
appears; its `enabled` is a `*bool`, so `false` is written when
`NEXUS_BENCH_ANONYMOUS=false`, and the other accounts write no `enabled`.

- `WriteDescriptor(path, d)`: JSON indented by two spaces, one final newline, `0644`
  file, atomic through the same helper; the bytes depend on `d` alone.
- `LoadDescriptor(path)`: a missing file is `ErrNoDescriptor` (wrapped, use
  `errors.Is`); invalid JSON or a `schemaVersion` other than 2 (missing counts as 0)
  is an error that names the file and never shows its content. A schema 1 file
  is a `*BenchError` (kind `Unclassified`) whose `Check` says to run `mise run
  nexus-bench:up` to write it again; `status` reports it as `descriptor:
  unreadable (...)` and still prints the pod and REST lines.
- `DescriptorPath()`: `NEXUS_BENCH_DESCRIPTOR` when set and not empty, else
  `internal/nexusbench/descriptor.json`, found with `runtime.Caller`, so it does
  not depend on the working directory (and does not survive `-trimpath`).
- Never in the descriptor: a password or any secret, the path of the secrets file,
  a timestamp, a pod name, or the word `password` (`TestDescriptorHoldsNoSecret`,
  `TestDescriptorFormat`).

The golden `testdata/descriptor.example.json` is the descriptor of `Defaults()`
with node IP `192.168.1.31`, Nexus `3.98.1-01`, `COMMUNITY`, EULA accepted. It is
checked both ways: `WriteDescriptor` must write exactly these bytes
(`TestDescriptorGolden`) and `LoadDescriptor` must read back `BuildDescriptor`'s
value (`TestLoadDescriptorGoldenMatchesBuild`). After a deliberate change of the
schema, regenerate it with
`go test ./internal/nexusbench -run TestDescriptorGolden -update` and review the
diff by hand (bump `schemaVersion` when a field changes meaning). The golden holds
the whole `Dataset()` (`SeedResult{Images: Dataset()}`), with `mirror/cached` as the
only image cached in the proxy.

## REST client and provisioning (`nexusapi.go`, `provision.go`, `initialpassword.go`)

`Provision(ctx, baseURL, ip, store, p, out) (ProvisionResult, error)` brings a
running Nexus to the state the bench needs (D-E). `baseURL` is the REST origin
(no path); `ip` is an `InitialPassword` (`Read(ctx) (pw, ok, err)`); `out` gets
progress lines (`provision: ...`) and never a password.

**Idempotence.** Every object is read, then created when absent or replaced when
present; there is no "create and ignore the error". A second run sends no `POST`
(except the one `POST` of a proxy whose upstream does not resolve yet, see
"Unresolvable upstream" under call 9 below) and changes neither the state nor
any password. Order of the calls:

1. `GET /v1/status` (no account): version and edition from the `Server` header
   (`Nexus/3.98.1-01 (COMMUNITY)`), else `info.version` of
   `/service/rest/swagger.json`, else empty (a note on `out`). The version is
   optional: a swagger answer that is a refusal or not JSON (a proxy page) leaves
   it empty; only a transport failure stops the run.
2. Admin credentials (below).
3. EULA: `GET /v1/system/eula`; 404 means an edition without one (noted,
   `EULAAccepted` false); `accepted: true` changes nothing; otherwise it is
   accepted only when `Params.AcceptEULA` (`NEXUS_BENCH_ACCEPT_EULA=yes`), with
   `POST {"accepted":true,"disclaimer":<as received>}`; the disclaimer is kept as
   a `json.RawMessage`, never rebuilt. Without consent `Provision` stops with a
   `ProvisionFailed` that names `NEXUS_BENCH_ACCEPT_EULA` and says that no
   repository, role or account was created, before any creation (the
   only write before it is the admin password change, which the EULA read needs).
4. Blob store: `GET /v1/blobstores` must list `default`, else `ProvisionFailed`
   before any role, user or repository is created.
5. Realms: `GET /v1/security/realms/active`; the missing ones of
   `NexusAuthenticatingRealm`, `DockerToken` are appended and the list is
   `PUT` back (a list that is already complete is not written). The plan said
   "set these two"; appending keeps a realm the instance already needs.
6. Anonymous access: `GET`, then always `PUT {"enabled": Params.Anonymous,
   "userId": "anonymous", "realmName": <current, default NexusAuthorizingRealm>}`.
7. Role `bench-docker-pull` (`nx-repository-view-docker-*-browse` and `-read`):
   `GET` 404 then `POST`, found then `PUT`.
8. User `bench-pull` (role above, status `active`): `GET /users?userId=bench-pull`,
   then `POST` with the password, or `PUT` plus `PUT .../change-password` with the
   stored password. The list filter matches by substring, so the element whose
   `userId` is exactly `bench-pull` is the one used.
8a. SSRF allowance (`ensureUpstreamAllowed`, DEVDESK-505, H-SSRF): Nexus 3.98 turns
   its SSRF protection on for a new installation and refuses a proxy whose remote
   URL host resolves to a private address, a ClusterIP included (see "Observed on
   the real bench (NX-02a)"). Only when `Params.UpstreamIsBench()`: `GET
   /v1/security/ssrf-protection` (`{"enabled","allowedIPs","allowedDomains"}`); a
   404 (a version without it) or `enabled: false` changes nothing, and the
   protection is never switched on or off; a host already in `allowedDomains`
   (case-insensitive) changes nothing; otherwise the lists are `PUT` back with the
   host of the bench upstream (`bench-upstream.<namespace>.svc.cluster.local`)
   appended to `allowedDomains`, the entries the instance already had kept and
   `allowedIPs` sent as an array, never `null`. The settings travel as a raw JSON
   object (`map[string]json.RawMessage`): only `allowedDomains` (and a null
   `allowedIPs`) changes, any other field a version of Nexus adds goes back as it
   was. `allowableUpstreamHost` is the one place that names the host, and it
   checks it again whatever `UpstreamIsBench` said: `validateNamespace` of the
   namespace first, then the URL must have no credentials and exactly that host
   (an error that repeats no part of the URL). It runs before the proxy is created,
   so the proxy is never refused for it. An upstream URL the bench did not deploy
   is not allowed (the operator chose that address): if Nexus refuses it for a
   private address the failure is `APIError.RemoteURLPrivate`, a `ProvisionFailed`
   whose cause says the SSRF protection is on and whose check names
   `NEXUS_BENCH_UPSTREAM_URL`, the advice to leave it unset and the endpoint to
   edit. The same refusal for the bench's own upstream (the allowance did not take,
   or the instance has no such setting) has its own wording: it says the upstream
   is meant to be allowed and sends to `/service/rest/v1/security/ssrf-protection`
   and `allowedDomains`, with no advice to unset a variable the operator never set.
9. `docker-hosted`, `docker-proxy` (`remoteUrl` = `Params.UpstreamURL`,
   `indexType: REGISTRY`) and `docker-group` (members hosted then proxy,
   `docker.httpPort: 8082`), all on blob store `default`, `forceBasicAuth: false`
   (anonymous pulls), the hosted and the proxy with `docker.pathEnabled: true`
   (H-PATH: a docker client reaches them by path on the HTTP port only then; the
   default is `null` and `/v2/` answers 404 on that port): `GET .../{name}` 404
   then `POST`, found then `PUT`.
   **Unresolvable upstream.** Nexus resolves the host of the proxy's remote URL
   when it saves the repository and answers `400` (validation error on
   `proxy.remoteUrl`) when it does not. The client keeps one verdict from that
   answer, `APIError.RemoteURLBlocked` (a boolean, true only for a `400` with an
   entry whose id ends in `proxy.remoteUrl` and whose message contains `resolve
   host`, case-insensitive, in a body of at most 4 KiB; the body is parsed and
   dropped, no text of it is kept or printed; a malformed URL or any other
   refusal of that field is an ordinary failure). On that verdict `Provision`
   writes `provision: warning: Nexus refused the remote URL of docker-proxy: it
   cannot resolve the upstream host yet, so it was left out; run up again once
   the upstream exists`, goes on, creates the group with `docker-hosted` as its only
   member, and returns `ProvisionResult.ProxyPending` (true only when the proxy
   does not exist; an existing proxy stays, stays a member, and the warning says
   `left as it was`). Any other `400`, or a `400` on another repository, still
   fails. `BuildDescriptor` then lists neither `docker-proxy` nor it as a group
   member. A run after NX-02 deployed the upstream creates the proxy and updates
   the group, and the descriptor changes (commit it). So a second `up` is not
   POST-free while the proxy is pending: it sends the one refused `POST` of the
   proxy.

**Admin credentials, a state machine with the write first.** `Load` the store;
the candidates are `adminPassword`, then `pendingAdminPassword`; an empty value
is never tried (a `{}` file means "not provided", and no request carries an empty
password). A candidate is accepted when `GET /v1/security/users?userId=admin`
answers 2xx; a 401 is a refusal and goes to the next candidate, any other status
is an error (a redirect or a 500 is not "wrong password"). A pending password
that the instance accepts is promoted to `adminPassword` with no new change.
When no candidate works, `InitialPassword.Read` supplies the password of the pod:
absent or refused gives the error `the bench admin password is unknown ...`, with
the check `mise run nexus-bench:down`, then `up`. Otherwise a password is
generated (or the stored pending one is reused), **`Save` stores it as
`pendingAdminPassword` first**, then `PUT /users/admin/change-password`
(`text/plain`) runs with the initial password, then `Save` promotes it to
`adminPassword`. A failed `Save` stops the run before the instance is touched
(`TestProvisionStopsWhenSaveFails`); a crash between the steps is resumed from the
pending password on the next run. The pull password follows the same rule: stored
before the user is created or its password set.

**Generated passwords** are 32 characters of `[A-Za-z0-9]` from `crypto/rand`
(rejection sampling, no modulo bias), never a fixed or predictable value.

**`kubectlInitialPassword`** (`NewKubectlInitialPassword(runner, namespace)`) runs
`-n <ns> exec deploy/nexus -- cat /nexus-data/admin.password` through the
`Runner`, so `kubectl.go` stays the only file that starts a process. A "No such
file or directory" on stderr is (empty, false, nil); another failure is a
`BenchError` (kind from `classifyKubectl`, else `ProvisionFailed`) whose message
holds neither stdout nor stderr. Unit-tested on a fake `Runner`; no integration
test.

**Client** (`client`, `newClient(baseURL, user, password)`): a dedicated
`http.Client` with `Timeout` 15 s, no proxy (the environment's proxy would receive
the credentials), and a `CheckRedirect` that returns `http.ErrUseLastResponse`:
no redirection is followed, so the 3xx is the answer and the target is never
contacted or named. A 2xx answer is read through `io.LimitReader` at 1 MiB + 1;
more is a `tooLargeError` and the content is never decoded (exactly 1 MiB is
read). Basic auth is set per request, only when there is a user; no header is
logged.

**Errors.** `*APIError{Op, Status}` prints `<Op>: HTTP <status> <text>`: no URL,
no request or answer body. `Provision` returns `*BenchError` only:
`RESTUnreachable` when a call got no answer, `ProvisionFailed` otherwise, with
the `*APIError` (or the store or pod error) in `Err` so `errors.As` and
`errors.Is` reach it. A 401 or 403 adds the hint that the stored admin password
may not match. No error and no line on `out` holds a password
(`TestProvisionNeverPrintsAPassword`, and `requireQuiet` after every scenario,
which knows every password the fake saw).

**H-REST: unverified hypotheses.** The in-memory Nexus of `fakenexus_test.go`
(`httptest`, loopback) is the one place that knows the JSON shapes, and it was
written from the Sonatype documentation, not from an instance. To check against
`/service/rest/swagger.json` at step 10, and to fix in the fake and the client
together:

- the `Server` header on `/v1/status`, and the swagger fallback;
- the EULA payload and the `404` of an edition without one;
- `change-password` as `text/plain`, and whether `admin.password` is deleted once
  the password is changed through the API;
- the realm ids (`DockerToken`) and that the active list can be read and `PUT`;
- the anonymous payload (`enabled`, `userId`, `realmName`);
- the role and user payloads (`source` only on `PUT`, `externalRoles`), and a
  `userId` filter that matches by substring;
- the repository payloads (`storage`, `docker`, `proxy`, `dockerProxy`,
  `negativeCache`, `httpClient`, `group.memberNames`, `docker.httpPort`), the
  statuses of `POST` (201) and `PUT` (204), the path-routing field `docker.pathEnabled` (null by default, which makes `/v2/`
  answer 404 on the HTTP port: `Provision` now sets it, H-PATH), and a proxy created
  before its upstream exists.

Verdict of the first real run (details in "First real run"): every item above
held except the last one. The `Server` header, the EULA payload, `text/plain`
change-password, the realm ids, the anonymous, role, user and repository
payloads and all the statuses matched the swagger and the fake. A proxy created
before its upstream resolves is refused with `400`; the fake does the same since
(`BlockUpstream`, `ResolveUpstream`).

**Reading a stored bench: `adminFromStore`, `listComponents` (`components.go`,
DEVDESK-541, NX-02a).** For the commands that run after `up` (seed, restore,
reset).

- `adminFromStore(ctx, baseURL, store)` returns a `*client` that authenticates as
  `admin` with the stored `AdminPassword`, and the stored `Secrets` (so the caller
  has the `bench-pull` password too). It sends no request: a stored password the
  instance refuses shows on the first call as an `*APIError` 401. It never reads
  the initial password of the pod and never falls back to it. No stored admin
  password (no file, `{}`, only a pending or a pull password: a pending password
  is not a current one) is a `*BenchError` of kind `ProvisionFailed` whose `Check`
  says to run `mise run nexus-bench:up`; so is a store that cannot be read, with
  the cause in `Err` and not in the message.
- `(*client).listComponents(ctx, repo)` calls `GET /v1/components?repository=<repo>`
  and follows `continuationToken` (sent through `url.Values`, so escaped) until it
  is `null`. A `404` (a repository that does not exist, was just deleted, or is a
  proxy still pending) is an empty list, not an error; a `404` on a later page is
  an `*APIError`. Failures are those of the rest of the client: `*APIError`
  (`list the components of <repo>`), `*transportError`, `*tooLargeError`,
  `*decodeError`; an error on any page returns no component at all, never the
  first pages. A token equal to the previous one fails at once with a
  `*decodeError` (no `APIError`, no `BenchError`); a listing whose tokens keep
  changing stops after 1000 calls with the same error. A `component` is `{Name, Version, Digest}`; `Digest` is
  `sha256:` + the checksum of the asset whose path holds `/manifests/` (the one
  ending in the tag first), and is empty, without an error, when the instance
  lists no such asset. The delete and the unblock are in `repoapi.go` (see "Cut, restore and reset").
- **H-COMPONENTS: held on the real bench (2026-11-17).** A Docker component is one
  image tag (`name` the image name, nested names such as `team/api` included,
  `version` the tag), the manifest asset is `v2/<name>/manifests/<tag>` and its
  `sha256` checksum is the digest of the manifest (the seed compares it with the
  data set and `status` reports `dataset: matches`); the component is listed as
  soon as the push or the pull-through ends (the seed's 30 s wait was not needed).

**Security guarantees.** No redirect followed; answers bounded; no proxy; no
`Authorization` header, URL with credentials, body or password in any error or
output; passwords written to the store before use; an empty password never sent;
no process started outside `kubectl.go`.

## Destruction and the clock (`destroy.go`, `clock.go`)

`Destroy(ctx, runner, clock, store, params, out)` is the one irreversible
operation of the bench (risk R1), so its order is fixed:

1. `RunPreflight(OpDown)`: `Validate` first (no call at all), then context,
   `/readyz`, `auth can-i delete namespaces`, and the namespace. A namespace
   without the label `app.kubernetes.io/managed-by=devdesk-nexus-bench` is
   refused here with `NamespaceForeign`, before any write.
2. A namespace that is absent prints `nothing to delete` and succeeds. The
   stored secrets are removed anyway, so a stale `secrets.json` does not
   outlive its bench and a second `down` is idempotent.
3. **One write**: `delete namespace <ns> --wait=false`, issued once, never
   retried (a `Forbidden` is returned as is). Nothing else is ever written: no
   finalizer patch, no force delete, no delete of another object. The tests
   journal every call and fail on a second write.
4. The namespace is read (`get namespace <ns> -o json`) until `NotFound` or
   `NEXUS_BENCH_DELETE_TIMEOUT` (default 3 min). The first read is immediate; the
   interval between reads doubles from 1 s to 10 s, always through `Clock.Sleep`
   and clipped to what remains of the timeout. A namespace stuck in `Terminating`
   ends in a `Deadline` error that cites the namespace, `spec.finalizers` and
   each `status.conditions` entry (type, status, message); the bench does not
   remove them.
5. One read of `get pv -o json`. A PersistentVolume whose `claimRef.namespace` is
   the bench namespace prints a line starting with `warning:` that names it and
   its phase and says it still exists: the run still succeeds, because the
   reclaim policy decides (`Delete` may remove it seconds later, `Retain` keeps
   the data on the node) and the bench does not delete a volume itself. No such volume, no
   warning. A PV list that cannot be read is also a warning, not a failure.
6. `store.Remove()`, last, and only when the namespace is gone. Its failure is
   returned as a `BenchError` (the namespace is already deleted; running `down`
   again retries the removal). A failed or cancelled run never touches the
   secrets.

A cancelled context ends the wait with an error that wraps `context.Canceled`
(`errors.Is` holds); the namespace keeps terminating on the cluster. Lines on
`out` start with `down:` and, like every error, hold no password.

`Clock` (`Now`, `Sleep(ctx, d) error`) is how the waits spend time. `NewClock()`
is the wall clock: `Sleep` returns `ctx.Err()` when the context is done (before
sleeping, even for a zero duration), returns at once for `d <= 0`, and otherwise
waits on a timer or the context. `fakeClock` of `clock_test.go` moves its time
forward instead of waiting, so no test sleeps for real.

## Deployment and the wait (`deploy.go`, `probe.go`)

`Deploy(ctx, runner, prober, clock, params, out) (DeployResult, error)` applies
the manifests and waits until Nexus answers. Order, fixed:

1. `RunPreflight(OpUp)`, whole; no write before it succeeds.
2. `ctx` is looked at (a runner may ignore it), then
   `kubectl apply --server-side --field-manager=devdesk-nexus-bench -f -` with
   `RenderNamespace(p)` on stdin: the Namespace alone, so a failure later leaves
   at worst our labeled namespace and never anything outside it.
3. The same apply with `Render(p)`: the whole stream, the Namespace again
   included. A second `up` sends the same two applies and gets the same result;
   `--prune` is never used.
4. The wait, one round per `pollInterval` (5 s) through `Clock.Sleep`: read
   `get pods -n <ns> -l app.kubernetes.io/name=nexus -o json` and
   `get pvc -n <ns> -o json` (the list; the claim of Nexus, `nexus-data`, is
   picked by name), diagnose them, then probe every host. The deadline
   is `NEXUS_BENCH_TIMEOUT` (10 min); it is checked after a round, so the cluster
   is read once more at the very end.

**Failure of an apply** is classified from its stderr by `classifyKubectl`:
`NodePortTaken` (names `NEXUS_BENCH_HTTP_NODEPORT` and `..._DOCKER_NODEPORT`),
`AdmissionDenied` (a webhook refusal wins over the `Forbidden` the API server
also reports), `Forbidden`, `ClusterUnreachable`, else `Unclassified`. The
runner's error is the `Err` of the `BenchError` (for `errors.Is`); neither the
stderr nor the error of the prober is ever in a message. After a failed apply
nothing else is called: no read, no probe, no sleep.

**Diagnosis**, each round, on the pods that are not being deleted:

| Reading | Kind | When |
|---|---|---|
| container waiting on `ErrImagePull` or `ImagePullBackOff` | `ImagePull` (names the image and `NEXUS_BENCH_IMAGE`, `_IMAGE_TAG`) | at once |
| `CrashLoopBackOff`, or `OOMKilled` as last or current termination | `Crash` (names the memory limit and `NEXUS_BENCH_MEMORY_LIMIT`) | at once |
| `PodScheduled=False`, reason `Unschedulable`, message with `Insufficient memory` or `Insufficient cpu` | `PodPending` (the request and `NEXUS_BENCH_MEMORY_REQUEST` or `_CPU_REQUEST`) | after `pendingGrace` (30 s) |
| claim `Pending` while a pod is scheduled | `PodPending` (the size and `NEXUS_BENCH_STORAGE_SIZE`) | after `pendingGrace` |
| no host answers by the deadline | `Deadline` (last state of the pod and the claim, the hosts tried, `NEXUS_BENCH_TIMEOUT`) | at `Params.Timeout` |

**One diagnosis, two workloads (DEVDESK-540).** The wait and the table above take a
`workload` (`deploy.go`): the pod selector, the variables that set the image and
the memory, the name of the claim and of the Deployment, and how to name them in a
message. `nexusWorkload()` is Nexus; `upstreamWorkload()` is the pod of the bench
upstream, whose memory is fixed by its manifest (no memory variable, so `Crash`
says `the bench-upstream container keeps crashing` without a limit nor a variable
to raise, `PodPending` for a lack of memory or cpu names no request, the pending
claim names no `NEXUS_BENCH_STORAGE_SIZE`, and `ImagePull` names
`NEXUS_BENCH_UPSTREAM_IMAGE` and `_IMAGE_TAG` and the upstream reference). The claim
is selected by name in the list `get pvc -n <ns> -o json`: the namespace holds the
claims of Nexus and of the upstream, kubectl lists them by name, and reading
`Items[0]` read the claim of the upstream (`bench-upstream-data`) instead of
`nexus-data` once the upstream manifests were applied, so a Pending claim of the
upstream was blamed on Nexus (`TestDeployReadsTheClaimOfNexusOnly`).

The unschedulable pod is read before the claim: while the pod is not scheduled, a
claim of a `WaitForFirstConsumer` class is `Pending` for that reason alone and is
not blamed. A reading that changes or disappears restarts the grace count (the
volume binds when the pod is scheduled; the Deployment may have made no pod yet).
When the deadline arrives with such a reading still active, it is returned as
`PodPending` rather than `Deadline`. A cancelled context is returned as is
(`errors.Is(err, context.Canceled)`), never as a `Deadline`.

**Reached hosts.** The hosts are `Params.Hosts` when set, else `127.0.0.1` and
the node `InternalIP` (when there is one). Each round probes them in parallel at
`http://<host>:<HTTPNodePort>` (`net.JoinHostPort`, so IPv6 gets brackets) and
returns after the first round in which at least one answered, with
`DeployResult.ReachableHosts` = the hosts that answered in that round, in host
order. A host that did not answer then gets a line on `out` that starts with
`deploy: warning:` and names it; no warning when all answered. `BuildDescriptor`
lists in `hosts` (and in every URL) `Params.Hosts` when set, else
`ReachableHosts` when not empty, else the earlier fallback (`127.0.0.1` and
`NodeIP`), so the descriptor never promises a host that did not answer
(`descriptor_hosts_test.go`).

**Prober** (`probe.go`): `NewProber()` is one unauthenticated `GET
<baseURL>/service/rest/v1/status` on a dedicated client (5 s timeout, no proxy,
no keep-alive, `CheckRedirect` returning `http.ErrUseLastResponse`): nil on 200
and only then, otherwise a `*APIError` (status only) or a `*transportError`
(wrapping the cause for `errors.Is`). The body is neither read for its content
nor reported, and a redirect target is never contacted.

**Rights the deployment needs** (all asked by the preflight, namespace-scoped
except the first two): create and patch `namespaces` (cluster); create and patch
`persistentvolumeclaims`, `services`, `deployments`; get and list `pods` and
`persistentvolumeclaims`; create `pods/exec` (used by `Provision`); list `services` in all namespaces (NodePort check).

**Not covered by the fakes.** `Deploy` has only run against a scripted `Runner`,
a fake `Prober` and a fake `Clock`; the real kubectl output shape of
`get pods -o json` (container status fields), the real duration of the pull and
the start, and whether `kubectl apply --server-side` of a Namespace that carries
no field of its own conflicts on a second run were left for step 10: the real
output was read, a second `up` applied the same two documents with no conflict,
and the start took 48 s (see "First real run").

## Residual risks (security audit of the steps 4, 6, 7)

Known and accepted for a throw-away bench; none is fixed by this change.

- **Basic credentials in clear HTTP.** Nexus is reached on `http://`, so the admin
  and `bench-pull` credentials of `Provision` travel unencrypted. The command
  honours the commitment made for it: it sends them to
  `RESTBaseURL("127.0.0.1", p)` and to another host only on `--rest-host`, with a
  warning (see "The command"). The package itself does not enforce it.
- **`local-path` volume in `0777`.** The data folder of the local-path
  provisioner is world-writable, so `admin.password` is readable by every local
  account of the node between the start of Nexus and the provisioning (which
  changes it).
- **NodePorts on the LAN, anonymous access on purpose.** The two NodePorts listen
  on every interface of the node, and anonymous access is enabled by default so
  that pulls need no login (`NEXUS_BENCH_ANONYMOUS`). Anyone on the LAN can read
  the bench; do not put anything private in it.
- **Mutable image tag.** The image is `docker.io/sonatype/nexus3:<tag>` with no
  digest: the same tag can resolve to another image later.
- **NX-02a exposure (DEVDESK-505).** `docker.pathEnabled` makes `docker-hosted`
  and `docker-proxy` reachable by path on the HTTP NodePort, anonymous pulls
  included, so anyone on the LAN can ask the proxy to fetch from `bench-upstream`
  (a pull of a name the proxy does not hold makes Nexus read the upstream). The
  host of the bench upstream stays in `allowedDomains` of the SSRF protection
  until the instance is reset; the protection itself stays on and no other host is
  allowed. Both are consequences of the design, not changes to it (D158 (3), (4),
  (13) in `docs/backlog.md`).
- **Partial result on error.** `Provision` returns the `ProvisionResult` it had
  gathered together with the error. The command does not write the descriptor
  when `err != nil` (`TestDescriptorIsNotWrittenUnlessProvisionSucceeded`), or it
  would describe a bench that is half provisioned.

## The command and the mise tasks (`cmd/nexus-bench`, `mise.toml`)

`internal/nexusbench/cmd/nexus-bench` is a `package main` of six files:
`main.go` (`deps`, `defaultDeps`, `run`), `flags.go` (flags and parameter
resolution), `commands.go` (`up`, `provision`, `down`, `seed`, `cut-upstream`, `restore-upstream`, `reset`, `render`), `record.go` (`record`, DEVDESK-511), `status.go` and
`usage.go` (the help). `main()` only builds a context cancelled by SIGINT and
SIGTERM, calls `run(ctx, args, os.Getenv, os.Stdout, os.Stderr, defaultDeps())`
and exits with its code. `deps` holds every constructor and library call that
reaches a cluster (`newRunner`, `newProber`, `clock`, `newStore`, `newInitial`,
`preflight`, `deploy`, `provision`, `destroy`) and, since NX-02a (DEVDESK-544),
the doors of the seed and the reads of `status` (`newForwarder`, `newCopier`,
`ensureUpstream`, `seed`, `readUpstream`, `readImages`); `defaultDeps` wires
`NewExecRunner`, `NewProber`, `NewClock`, `NewFileStore`,
`NewKubectlInitialPassword`, `RunPreflight`, `Deploy`, `Provision`, `Destroy`,
`NewExecPortForwarder`, `NewSkopeoCopier`, `EnsureUpstream`, `Seed`,
`ReadUpstream` and `readImagesAnonymously` (NX-02b adds `cutUpstream`, `restoreUpstream`, `reset` and `unblockProxy`, wired to `CutUpstream`, `RestoreUpstream`, `Reset` and `UnblockProxy`) (which calls `ListImagesAnonymously`),
and the tests wire fakes, so no test needs kubectl, skopeo or a network.

**Tasks.** Eleven, in `mise.toml`, none reachable from `ci`, `check`, `test` or
`test-race` (`mise_tasks_test.go` reads the file and checks it, and checks that
`kubectl` is declared on each of them, not in the global `[tools]`, so the CI does
not install it; kubectl `1.35`, because `datomir` runs k0s v1.36.x and kubectl is
supported within one minor version of the API server, so 1.35 is the oldest
accepted; `render` and `record` declare no kubectl (`record` reaches no cluster unless `--state` is given, which then needs a kubectl already on the `PATH`), and the test
requires its absence there). `nexus-bench:seed` needs `skopeo` too, which is a
tool of the host: mise does not install it, the task does not declare it, and its
description says so (`TestMiseSeedTaskNamesSkopeoAsAPrerequisiteOfTheHost`).
Arguments follow `--`: mise appends them to the command line.

| Task | Runs | Acts on the cluster |
|---|---|---|
| `nexus-bench:up` | `go run ./internal/nexusbench/cmd/nexus-bench up` | yes |
| `nexus-bench:provision` | `... provision` | yes (REST and `exec`) |
| `nexus-bench:down` | `... down` | yes (deletes the namespace) |
| `nexus-bench:seed` | `... seed` | yes (port-forward, REST reads, skopeo pushes); needs skopeo on the host |
| `nexus-bench:cut-upstream` | `... cut-upstream` | yes (scales the upstream to zero) |
| `nexus-bench:restore-upstream` | `... restore-upstream` | yes (scale, REST writes, port-forward, skopeo reads); needs skopeo on the host |
| `nexus-bench:reset` | `... reset` | yes (deletes repositories and the upstream claim, then provisions and seeds); needs skopeo on the host |
| `nexus-bench:status` | `... status` | reads only |
| `nexus-bench:render` | `... render` | no, contacts nothing |
| `nexus-bench:record` | `... record` | no cluster, no kubectl declared: reads the bench through its loopback ports and rewrites fixture files (see "Recorded fixtures"); only `--state` reads the upstream, and needs `kubectl` on the `PATH` (the task does not install it) |
| `nexus-bench:test` | `go test -timeout 60m -count=1 -run Integration ./internal/nexusbench/...` with `DEVDESK_NEXUS_BENCH=1` set by the task's `env` table (portable, no `env` command) | yes; the tests skip when `CI` is set |

**Subcommands and flags.** Every subcommand takes the same parameter flags, one
per `NEXUS_BENCH_*` variable: `--context`, `--namespace`, `--http-nodeport`,
`--docker-nodeport`, `--cpu-request`, `--cpu-limit`, `--memory-request`,
`--memory-limit`, `--storage-size`, `--jvm-opts`, `--image`, `--image-tag`,
`--upstream-image`, `--upstream-image-tag`, `--upstream-url`, `--anonymous`,
`--hosts`, `--timeout`, `--delete-timeout`, `--secrets`, `--descriptor`. `up`
and `provision` add `--accept-eula` and `--rest-host`; `seed`, `restore-upstream` and `reset` add `--rest-host`
only, and accept a loopback address only (`cut-upstream` has no REST host: it only scales). `seed` provisions nothing, so there is no EULA to accept). Each subcommand has its own `flag.FlagSet` (`ContinueOnError`,
output discarded: the command prints parse errors itself on stderr and `-h` on
stdout). A flag is laid over its variable and the result goes through
`FromEnv(Defaults(), ...)` once, so "flag, then `NEXUS_BENCH_*`, then default",
the parsing and the rule that `--namespace` moves the upstream URL (unless an
upstream is given, by flag or variable) are the library's own. `--accept-eula`
becomes `yes` (and `--accept-eula=false` `no`, which beats a variable set to
`yes`). `Validate` runs before anything else, `render` included.

**Exit codes.** `0` success; `1` failure (invalid parameter, kubectl missing, a
step failed, a run cancelled); `2` mistake in the command line (no or unknown
subcommand, unknown flag, a value of the wrong type, a missing value, an extra
argument), with the help on stderr when the subcommand is missing or unknown.
`help`, `-h` and `--help` print on stdout and exit `0`. `go run` (so every
`mise run nexus-bench:*`) does not propagate the code: it prints `exit status 2`
and exits `1`. Code `2` is only visible when the compiled binary is run directly;
the general help says so (`TestHelpSaysGoRunDoesNotKeepExitCodeTwo`).

**Interruption.** `main()` cancels the run's context on SIGINT or SIGTERM and
then hands the signals back to the default behavior (`context.AfterFunc(ctx,
stop)`), so a second Ctrl-C kills a run that is slow to wind down. This is not
unit-tested: it lives in `main()`, which the tests do not call.

**Output.** The progress of the library steps goes to stdout, then one line
`nexus-bench: descriptor written to <path>`. Every failure is one line on stderr:
a `BenchError` (also when wrapped by `%w` or joined) prints exactly its
`Error()`, and nothing around it or inside it (no wrapper text, no `Err`, no
trace); a cancelled run says `cancelled` and that `status` shows where things
stand; another error prints its message. No output holds a password
(`requireClean` of the tests plants markers in the fakes).

**`up`.** Resolve the parameters, then the secrets path
(`--secrets`/`NEXUS_BENCH_SECRETS`, else `DefaultSecretsPath`; with no place for
it the run stops before any cluster call and says to set
`NEXUS_BENCH_SECRETS`), `newRunner`, `newStore` (the git guard), then, when the
bench deploys the upstream (`Params.UpstreamIsBench`), `newForwarder` and
`newCopier`: everything that can refuse comes before the first write to the
cluster, so a host without skopeo stops `up` before it deploys. Then, as D-J says,
`Deploy` (which runs `RunPreflight(OpUp)` itself, so the command does not run a
second one), `EnsureUpstream`, `Provision` (the proxy is created and joins the
group), `Seed`, and the descriptor. **The descriptor is written only after all of
them succeeded**, never on a failure at any step, and a descriptor from an earlier
run is left as it was. It is built with `BuildDescriptor(p, DeployResult,
ProvisionResult, SeedResult)`, the `SeedResult` being what `Seed` returned: the
whole data set. With an upstream URL the bench did not deploy (a flag or
`NEXUS_BENCH_UPSTREAM_URL`) there is nothing to scale and nothing to push to:
`up` builds no door, calls neither `EnsureUpstream` nor `Seed`, prints `seed:
skipped, NEXUS_BENCH_UPSTREAM_URL points at ...` on stdout, needs no skopeo, and
describes an empty data set (the behavior of NX-01).

**`seed`.** `newRunner`, `newStore`, `newForwarder`, `newCopier` (all before the
cluster is touched), then the descriptor of the previous run is read, then
`RunPreflight(OpSeed)`, `Seed`, and the descriptor again. `Seed` has no data of its
own about Nexus: the version, the edition and the EULA consent are carried over
from the descriptor of the previous run, and so are its **hosts**
(`DeployResult{ReachableHosts: prev.Hosts}`), hence its `rest` and
`dockerConnectors` URLs: the preflight of a seed does not read the node, so it
has no address to give, and rebuilding the hosts from it would drop the node
address `up` found. The file is not trusted for them: they go through the rule of
`NEXUS_BENCH_HOSTS` (`ValidateDescriptorHosts`: an IP address or a host name of at
most 253 characters) and a bound of 16 hosts, and a bad host stops the seed before
any copy with a message that says to run `up` and does not repeat the host. Known
limit: a syntactically valid host name passes, whether it is the node or not is not
checked. `provision` never reads the hosts of the previous descriptor. Without a readable descriptor of schema 2 (missing, schema 1,
not JSON), or with the descriptor of another bench (another namespace or another
context), `seed` stops before any copy with a message that says to run `mise run
nexus-bench:up`; the file is not touched. As for `up`, the descriptor is written
only after a `Seed` that succeeded. `--rest-host` is accepted for a loopback
address only (`127.0.0.1`, `::1`, `localhost`): any other host is refused before
any dependency is built, with a message that says seed always goes through
127.0.0.1 (the images and the credentials never leave the machine from `seed`).
There is no `--accept-eula`. `seed` against an upstream URL
the bench did not deploy is refused by `Seed` itself (`SeedFailed`); the command
does not second-guess it.

**Bounds on what Nexus returns (F3, F6).** The names of the components a message
prints are quoted (`%q`, an escape sequence shows as text) and at most
`maxSeedNamesShown` (5) are shown, the rest summed up as `and K more`
(`shownNames`). `listComponents` reads at most `maxListedComponents` (5000)
components in all; past it the listing is an error naming the bound and the
number of components (`tooManyError`, a `ProvisionFailed` asking for a reset),
with no partial result.

**Accepted risks (single-user machines).** F4: the port-forward listens on a
loopback port, which another local user could take between the choice of the port
and the bind; F5: the skopeo authfile of the seed stays on disk if the process is
killed with SIGKILL (it is removed on every other exit). Both assume a development
machine with one user.

**The seed deadline.** `Seed` gets a context that ends after `Params.Timeout`,
counted from the start of the seed (not of `up`), so a copy over a port-forward
that hangs cannot hang the command and the library does not have to remember it.
A seed that outlasts it fails with exit `1`, a `Deadline` message naming the
timeout (`--timeout`/`NEXUS_BENCH_TIMEOUT`) and no descriptor; the copies already
made are kept and a second `seed` copies only what differs. A cancelled run keeps
the plain `cancelled` message.

**`provision`.** `RunPreflight(OpProvision)`, `Provision`, then the descriptor,
built with `DeployResult{NodeIP: <node address of the preflight>}`. It seeds
nothing, but the data set is fixed in the code and `provision` touches neither the
images nor the upstream, so it keeps the data set, **re-emitted from the code**
(`Dataset()`), never copied from the file: the file's data set is compared to the
code's, and counts only when it is the same bench (namespace, context and upstream
URL unchanged) and it lists exactly that data set. For another bench, or a file
that disagrees (another digest, an image added, removed or moved), the data set is empty and stdout says `provision: the descriptor now
lists no data set; run mise run nexus-bench:seed` (without the hint when the
upstream is not the bench's, since `seed` refuses it). A descriptor that is
missing or of schema 1 has no data set to keep: the file is written with an empty
one, with no line.

**`down`.** `Destroy` (its own `RunPreflight(OpDown)` first). The descriptor is
not touched or created.

**`status`.** Read-only: `RunPreflight(OpStatus)` (a missing namespace is its
`NamespaceMissing` error, exit `1`), one `kubectl get pods -l
app.kubernetes.io/name=nexus -o json`, one probe of the REST origin, the state of
the upstream, one read of the descriptor and, when it can, two anonymous REST
listings. Six lines, each a finding, exit `0`:

    bench: namespace nexus-bench, context datomir
    pod: running (nexus-xxx, 1/1 ready, 0 restarts)     | not found | pending ...
    rest: ready (http://127.0.0.1:30481)                 | not ready (the cause is not shown)
    upstream: ready | cut (0 replicas) | starting (...) | missing (...) | not managed by the bench (...) | unknown (...)
    descriptor: matches (<path>) | not found (<path>) | unreadable (<cause>) | differs from the current parameters (<path>) + one line per field
    dataset: matches (<n> images) | differs (<n> image(s)) | unknown (<why>)

`upstream:` is `ReadUpstream` (deployment missing, cut, starting, ready); an
upstream URL the bench did not deploy is `not managed by the bench` and nothing is
read; a deployment that cannot be read is `unknown`, without the text of the
failure. `dataset:` compares the data set the descriptor lists with the listings
of `docker-hosted` (the hosted images) and `docker-proxy` (the images cached in the
proxy): one difference per image that is missing, has another digest, or is in
the listing and not in the data set (so `mirror/uncached` in the proxy counts). `matches (<n> images compared)` counts the images
actually compared (the hosted ones and the ones cached in the proxy, not the whole
data set).
It is `unknown` when it cannot compare: no readable descriptor, a descriptor with
no data set (the line sends to `seed` only when the upstream is the bench's),
Nexus not answering, anonymous access off (`--anonymous=false`), a
listing that fails or gives no digest. The listings come from
`ListImagesAnonymously` (`GET /service/rest/v1/components?repository=<repo>`, the
`continuationToken` followed, no `Authorization` header at all, on the loopback
REST origin). `docker-group` is never listed (a read through it can cache an
image, F9), skopeo is never started, no port is forwarded, nothing is written.

The descriptor comparison covers context, namespace, image, both NodePorts, the
upstream URL, the upstream image (`upstream image: descriptor ..., current ...`)
and the anonymous flag; hosts and versions come from the cluster and are left out,
and so is the dataset (the parameters know nothing of it; the `dataset:` line
covers it). A schema 1 descriptor is `unreadable` with the message of
`LoadDescriptor` (run `mise run nexus-bench:up`).
It creates no store, builds no `InitialPassword` and sends no credential.

**`render`.** Prints `Render(p)` and nothing else; it builds no dependency (the
test runs it with an empty `deps`).

**Where the credentials go.** `Provision` and `Seed`
are given `RESTBaseURL("127.0.0.1", p)`, whatever hosts `Deploy` reached, so the
admin and `bench-pull` passwords stay on the loopback by default. `--rest-host <host>`
(a host name, an IPv4 or IPv6 address, brackets and a zone allowed; no port,
scheme, path, credentials or space, refused before any cluster call with exit
`1`) sends them to that host instead, and unless it is a loopback address or
`localhost` the command prints on stderr a warning that the credentials travel in
clear text. The flag moves the REST origin only: the images of the seed always go
through `127.0.0.1` (the port-forward and the NodePort path), and the help of
`--rest-host` says so (`up`, `provision`, `seed`). `status` and the deployment
wait probe without credentials.

**Non-default bench.** After writing the descriptor, if the parameters differ
from `Defaults()` (any field but the two timeouts, the two paths, the EULA
consent and an empty host list), stderr gets `warning: descriptor.json now
describes a non-default bench; do not commit it`. The file is written all the
same, at `--descriptor`/`NEXUS_BENCH_DESCRIPTOR`, else the versioned one. A
default bench prints nothing on stderr.

## Integration tests and source guards (`bench_integration_test.go`, `guard_test.go`, step 9)

Read-only state of the run on 2026-11-16: the three read tests pass (8 subtests)
and the lifecycle passes (151 s for two downs and three deployments).

**Gates.** Every test of `bench_integration_test.go` starts with
`requireBench(t)`: it skips unless `DEVDESK_NEXUS_BENCH=1` and `CI` is empty, so
`mise run test`, `check`, `ci` and the CI never reach the cluster. The mise task
`nexus-bench:test` sets the variable (`env` table). The destructive test needs
two more things: `DEVDESK_NEXUS_BENCH_LIFECYCLE=1` and `NEXUS_BENCH_ACCEPT_EULA=yes`
(the lifecycle provisions Nexus, so the EULA has to be accepted by the operator).
None of the tests writes the versioned `descriptor.json`.

**The four tests.** The first three need a bench that is up and provisioned
(`mise run nexus-bench:up`): they read the versioned descriptor and the secrets
file (`openLiveBench`; the REST URL is the descriptor's one on the loopback) and
fail with the command to run when either is missing.

| Test | Reads | Writes |
|---|---|---|
| `TestIntegrationBenchDescribed` | `GET /service/rest/v1/status` without credentials (200), then as admin the repository list and, per repository of the descriptor, its type, format, group members or proxy remote URL | nothing |
| `TestIntegrationAnonymousAsDescribed` | the anonymous-access setting as admin, and `GET /repositories` without credentials: 200 when the descriptor says anonymous is enabled, 401 when not | nothing |
| `TestIntegrationPullAccountIsReadOnly` | `GET /repositories` as `bench-pull` (200) | one control `POST` of a hosted repository `bench-readonly-probe` as `bench-pull`, which has to be refused with **403**. If Nexus accepts it (any 2xx), the test deletes that repository as admin and fails |
| `TestIntegrationLifecycle` | admin listings of repositories, roles and users | destructive, see below |

`TestIntegrationLifecycle` runs `Deploy` and `Provision` (first up), compares a
snapshot of repositories, roles and users, runs up and provision a second time
and requires the same snapshot (idempotence), then `Destroy` and checks that
`get namespace` answers NotFound, `Deploy` on a fresh instance (the pod must hold
an `admin.password` again), and a final `Destroy`. It uses the parameters of
`FromEnv` and the secrets path of the bench (`NEXUS_BENCH_SECRETS`, else the
default), refuses to start without the EULA, and refuses when `go test` has less
than 45 minutes left (`lifecycleMinTimeout`: three deployments of up to ten
minutes plus the deletions; the default of `go test` is 10 minutes). The
namespace is whatever the parameters say; the preflight already refuses a
namespace this tool did not create.

**Running it.** The task passes `-timeout 60m` for that reason
(`TestMiseBenchTestTaskRunsTheIntegrationTestsGuarded` requires a `-timeout` of
at least 45 minutes):

    mise run nexus-bench:test                         # the three read tests, lifecycle skipped
    DEVDESK_NEXUS_BENCH_LIFECYCLE=1 NEXUS_BENCH_ACCEPT_EULA=yes mise run nexus-bench:test

**A failure leaves the bench deployed.** The lifecycle uses `t.Fatalf` and no
cleanup: when a step fails halfway, the namespace stays as it is so that the
cause can be diagnosed (`nexus-bench status`, `kubectl logs`). Clean up with
`mise run nexus-bench:down`.

**Source guards (`guard_test.go`).** Three tests read the package with
`go/parser`, name file and line, and each detector has a self-test fed with a
faulty and a correct source held in memory. A failure points at code that breaks
the rule, not at a test to relax.

1. *Every test that reaches the bench calls `requireBench(t)` first.* A call of
   `NewExecRunner`, `NewExecPortForwarder`, `NewSkopeoCopier`, `DescriptorPath`,
   `DefaultSecretsPath` or `newClient` (unless
   `newClient` is given the address of an `httptest` server) before the guard, or
   in a function without it, fails. Exceptions are named in
   `requireBenchExceptions`, with a `check` that verifies the reason in the
   source (the PATH is isolated by `installFakeKubectl` or `installHelperTool`, the
   environment is a map, or, for the one test that runs the real skopeo, it starts
   its own loopback `newFakeRegistry` and moves `HOME`); an exception that
   matches nothing fails too (`TestGuardExceptionsStillMatch`). Limit: calls are
   read by name in the test files of this folder; a call hidden behind a function
   value, or made by the tests of `cmd/nexus-bench` (which run on fakes), is not
   seen.
2. *Only `kubectl.go`, `secrets.go` and `skopeo.go` start a process*
   (`execAllowed`; `exec.Command` or `CommandContext`, any import name, dot
   import refused): one door to the cluster (`kubectl.go`: the commands and the
   port-forward), one to `git check-ignore`, one to the image registries
   (`skopeo.go`). A listed file that no longer starts one
   must be dropped from the list. Limit: `syscall.Exec`, `os.StartProcess` and a
   process started by another package are not seen.
3. *No Authorization header outside the client.* The word `authorization` (in an
   identifier or a string) may appear only in `nexusapi.go` and `probe.go`,
   `SetBasicAuth` only in `nexusapi.go`, and `net/http/httputil` is imported
   nowhere. Comments are not read. Limit: a header name built by concatenation,
   or a whole `http.Header` printed with `%v`, is not seen.

**The mise guard** (`mise_tasks_test.go`) reads `mise.toml` with a small
line-based reader (no TOML library in `go.mod`) and checks: the eleven tasks exist
(`TestMiseDeclaresTheElevenBenchTasks`)
and call the command; none of `ci`, `check`, `test`, `test-race` reaches a
`nexus-bench` task, directly or through `depends` and `run` references; `kubectl`
is declared on every task that reaches the cluster, absent on `render` and `record`
(`TestMiseRecordTaskNeedsNoKubectlAndSaysWhatItNeeds`) and absent from the global `[tools]`; `nexus-bench:test` sets
`DEVDESK_NEXUS_BENCH=1`, runs `-run Integration` and carries a `-timeout` of at
least 45 minutes. Limit: it reads the file, it does not ask mise to resolve it.

## First real run (datomir, 2026-11-16)

Step 10, on k0s v1.36 (`datomir`), image `docker.io/sonatype/nexus3:3.98.1`
(pulled in 10.6 s), `Server: Nexus/3.98.1-01 (COMMUNITY)`. The operator accepted
the Community EULA before the run; the REST client never accepts it on its own.

**Deployment (H-RES).** First `up` on an empty cluster namespace: 48 s from the
apply to a Ready pod and a changed admin password (pull included); the pod went
Pending, ContainerCreating, running not ready, ready, and the claim bound with
the pod (`local-path`). Idle after provisioning: 55m CPU and 1257Mi memory
(`kubectl top pod`), against the 3Gi request and 4Gi limit of D-C: the request
could be lowered, but it was kept (the JVM grows with the first pulls, to be
measured with NX-02). `/nexus-data` held 3.7 MB. `admin.password` is gone once
the password is changed through the API (H-REST confirmed). An `up` on an
already provisioned namespace takes 1 to 2 s.

**REST statuses seen** (Nexus `request.log`): `PUT .../users/admin/change-password`
204, `POST /system/eula` 204, `PUT /security/realms/active` 204, `PUT
/security/anonymous` 200, `POST /security/roles` 200, `POST /security/users`
200, `POST /repositories/docker/hosted` 201, `POST .../group` 201, `PUT
/security/roles/{id}` 204, `PUT /security/users/{id}` 204, `PUT
.../users/{id}/change-password` 204, `PUT .../hosted/{name}` and `PUT
.../group/{name}` 204. The swagger lists the same codes (`GET /v1/system/eula`
answers 200 whatever the state, `POST` 204/500). The client accepts any 2xx; the
fake answers 200 for `POST` roles and users and 201 for repositories, as the
swagger and the instance do.

**The proxy.** `POST /repositories/docker/proxy` with
`http://bench-upstream.nexus-bench.svc.cluster.local:5000`, a host that does not
exist yet (NX-02): `400`, content type `application/vnd.siesta-validation-errors-v1+json`,
body `[{"id":"HelperBean.proxy.remoteUrl","message":"Proxy URL blocked: Failed to
resolve host: ..."}]`. The proxy is not created, the instance keeps working. The
answer to this is in "REST client and provisioning", step 9: the proxy is
pending, the group holds `docker-hosted` alone, the descriptor says so. The
committed `descriptor.json` therefore describes the bench as it is before NX-02:
`docker-hosted` and `docker-group` (with `docker-hosted` as its only member), no
proxy. It is not the descriptor of the bench after NX-02, which has the proxy and
lists it in the group, so the versioned file differs before and after NX-02. It
also embeds the node IP `192.168.1.31`, which is specific to this machine.

**CA2 (REST provisioning) is met for `docker-hosted` and `docker-group`; the
proxy is deferred to NX-02.** Nexus 3.98.1-01 refuses a proxy whose upstream host
does not resolve, and the upstream only exists once NX-02 deploys it. The
criterion to carry into NX-02: run `mise run nexus-bench:up` again, check that
`docker-proxy` exists and is a member of `docker-group`, and commit the new
`descriptor.json`.

**Idempotence (CA3).** A second and third `up` sent no `POST` but the refused
proxy one (the pending proxy of "The proxy" above), `PUT` everything else (including a `change-password` of `bench-pull`
with the stored password, the same value), left every password unchanged and
wrote byte for byte the same `descriptor.json`. The lifecycle test compares the
snapshots of repositories, roles and users after a second up and provision and
finds them equal.

**Read-only tests.** `TestIntegrationBenchDescribed`,
`TestIntegrationAnonymousAsDescribed` and `TestIntegrationPullAccountIsReadOnly`
pass: status 200 without credentials, the anonymous settings and the anonymous
`GET /repositories` as described, and the control `POST` of `bench-pull` is
refused with 403 (nothing was created).

**Lifecycle (CA3, CA4).** `TestIntegrationLifecycle` passes in 151 s: up,
provision, up and provision again with no change, `down`, `up` on a fresh
instance (a new pod with a new `admin.password`), `down`. Afterwards the namespace
is `NotFound`, no PersistentVolume of the bench remains (`local-path` reclaims
with `Delete`), `secrets.json` is removed (the empty `datomir` folder above it
stays) and `descriptor.json` is unchanged.

## Data set and OCI layout (`dataset.go`, `ocilayout.go`, DEVDESK-538)

The images the bench holds are fixed in code (decision D-I of DEVDESK-505), in
this order. `Dataset()` returns a copy of the table with `Digest` filled by the
generator; the digests are never written by hand.

| Image | `Location` | Role |
|---|---|---|
| `api:1.0.0`, `api:1.1.0` | `docker-hosted` | one-component name, two tags |
| `team/api:1.0.0` | `docker-hosted` | two-component name |
| `tools/cli:0.1.0` | `docker-hosted` | third hosted image |
| `shared/base:1.0` | `docker-hosted` and `upstream` | same name and tag in both places, different contents and digests, so a read through the group says which member answered |
| `mirror/cached:1.0` | `upstream`, `PullThrough` | pulled once through `docker-proxy`, so only in the proxy |
| `mirror/uncached:1.0` | `upstream` | never pulled |

`WriteLayout(dir, images)` writes an OCI image layout into an existing, empty
directory (anything else is an error and nothing is written: the images are built in
memory first) and returns the reference of each image, `img-01`, `img-02`, ...
(a `:` would break skopeo's `oci:<dir>:<ref>` syntax). `imageDigest(img)` gives the
manifest digest without writing. The bytes depend on `Name`, `Tag` and `Location`
only (not on `Digest` or `PullThrough`), with the standard library alone:

- one layer per image, an uncompressed tar (`application/vnd.oci.image.layer.v1.tar`),
  entries in sorted order (`bench/`, `bench/<source>/`, `bench/<source>/<name>-<tag>.txt`,
  `/` of the name replaced by `-`), USTAR, `ModTime` at the Unix epoch, uid/gid 0,
  no owner names, modes 0755 (folders) and 0644 (file);
- config `linux/amd64`, no `created`, label `org.devdesk.nexus-bench.source`
  (`hosted` or `upstream`) under the key `config.Labels`, spelled as the image spec
  does (`TestWriteLayoutConfigKeyIsLabelsAsTheImageSpecSpellsIt`), `rootfs.diff_ids` holding the layer digest;
- OCI v1 manifest, `index.json` with `org.opencontainers.image.ref.name`, and the
  `oci-layout` file (`1.0.0`); JSON written by `encoding/json` from structs, so the
  key order is fixed.

`testdata/dataset-digests.golden` holds one line per image
(`name:tag<TAB>location<TAB>digest`), rewritten with
`go test ./internal/nexusbench -run TestDatasetDigestsGolden -update`; a change of
the generator moves every digest and shows in its diff.

**H-LAYER fallback.** The constant `layerGzip` (`ocilayout.go`) is `false`. Setting
it to `true` writes the layer with `compress/gzip` (no name, no date,
`application/vnd.oci.image.layer.v1.tar+gzip`); the diff id stays the digest of the
tar. The tests that pin the uncompressed layer (`TestWriteLayoutLayerIsAn...`,
`TestWriteLayoutConfig`) and the golden then have to follow, and the digests depend on
the `compress/gzip` output of the Go version.

**Confirmed on the bench.** Nexus 3.98.1 accepts the uncompressed layer under
`strictContentTypeValidation` (H-LAYER), skopeo with `--preserve-digests` pushes the
layout to the upstream and to `docker-hosted` without changing a digest (H-PUSH), and
`docker-proxy` keeps the upstream digest of `mirror/cached:1.0` on pull-through
("Observed on the real bench (NX-02a)").

## The two process doors: port-forward and skopeo (`kubectl.go`, `skopeo.go`, DEVDESK-539)

NX-02 needs two more processes: a `kubectl port-forward` to reach the upstream
registry Service from this machine, and `skopeo` to push the data set into it and
into the bench Nexus. Both are started from one file each, and both are written so
that a failure leaves no process and no secret behind.

**Port-forward.** `PortForwarder.Forward(ctx, namespace, service, port)` runs
`kubectl --context <c> -n <ns> port-forward svc/<service> :<port> --address
127.0.0.1` (`NewExecPortForwarder`, `KubectlMissing` like `NewExecRunner`; the
namespace and the Service name are checked against a DNS-like pattern first) and
returns `127.0.0.1:<port>`, the port kubectl chose, with a `stop` function.

- The address comes from `parseForwardingLine`, which accepts only
  `Forwarding from 127.0.0.1:<port> -> ...` with a port from 1 to 65535; the IPv6
  line kubectl prints as well, another address, and `127.0.0.1.evil.com` are
  ignored. The wait is `portForwardTimeout` (15 s, a variable so a test can shorten
  it); a `Deadline` `BenchError` after it.
- One goroutine calls `cmd.Wait`, so kubectl is always reaped, whoever ended it.
  `stop` kills the process and waits for that goroutine, any number of times
  (`sync.Once`), and always returns nil. The cancellation of `ctx` kills kubectl
  too (`exec.CommandContext`), whether `Forward` is still waiting (it kills, waits
  and returns an error) or has returned. After every error path the process is
  gone.
- kubectl exiting before the line is a `BenchError` whose kind comes from
  `classifyKubectl(stderr)`; the stderr (capped at 16 KiB) picks the kind and is
  never put in a message.

**skopeo.** `ImageCopier` has two methods, `Copy(ctx, src, dst, CopyOptions)` and
`ManifestDigest(ctx, ref, creds)`; `ImageRef{Transport, Location}` is `oci`, `dir` or
`docker` (the location of a docker reference has no `//`). `NewSkopeoCopier` finds
skopeo on PATH (`SkopeoMissing`).

- *Loopback only.* TLS verification is switched off (`--src-tls-verify=false`,
  `--dest-tls-verify=false`, `--tls-verify=false`) for every docker reference and
  for no other, so the host is the only protection: `checkSide` refuses, **before
  anything is created or started**, a docker reference whose host is not one of
  the two literals `127.0.0.1` and `[::1]` (an optional port), or that has no `/`
  followed by a repository name: without one skopeo reads `localhost:5000` as an
  image of docker.io, with TLS off. `localhost` is not accepted, it resolves
  through the host's configuration. The host is the text before the first `/`,
  parsed with `net.SplitHostPort` (never a prefix test), so `127.0.0.1@evil.com`,
  `127.0.0.1.evil.com`, `127.0.0.1:5000@evil.com`, `localhost`, `localhost.evil.com`
  and `evil.com/127.0.0.1:5000/x` are refused, as are a LAN address (the node
  itself), an empty host and `127.0.0.1:5000/`. An unknown transport, and
  credentials given for an `oci` or `dir` side, are refused the same way. The
  `oci` and `dir` locations are paths and are not read as hosts.
- *Credentials.* A side with no credentials gets `--src-no-creds` /
  `--dest-no-creds` / `--no-creds`; a side with credentials gets `--*authfile
  <file>`, a container auth file `{"auths":{"<host[:port]>":{"auth":"<base64
  user:password>"}}}`. There is one file per side, so the credentials of one are
  never offered to the other. No password is ever an argument (visible through
  `/proc`) or an environment variable. `Credentials` implements `fmt.Formatter`
  (every verb, `%d` and `%x` included, prints `Credentials{User: <redacted>,
  Password: [REDACTED]}`), `String`, `GoString` and `slog.LogValuer`, and the
  password has `json:"-"`.
- *Working folder.* Each call makes `os.MkdirTemp` (`0700`), writes in it
  `policy.json` (`{"default":[{"type":"insecureAcceptAnything"}]}`, given with
  `--policy` before the subcommand; `--insecure-policy` is never used), an empty
  `registries.conf` and the authentication files (`0600`), and `defer`s
  `os.RemoveAll`. The folder is gone after a success, a skopeo failure and a
  cancellation (the tests point `TMPDIR` at a folder and require it empty).
- *Environment* (`skopeoEnvironment(registriesConf)`, modelled on
  `gitEnvironment`): the caller's, without `HTTP_PROXY`, `HTTPS_PROXY`,
  `ALL_PROXY`, `NO_PROXY` (any case), `REGISTRY_AUTH_FILE` and every `CONTAINERS_*`
  variable, then `CONTAINERS_REGISTRIES_CONF=<the empty file>`, so no proxy,
  mirror, login or policy of the host applies. Names are compared in upper case
  (Windows). `HOME` and `PATH` are kept.
- *Digest.* `ManifestDigest` runs `skopeo inspect --raw` (stdout capped at 4 MiB)
  and computes the sha256 in Go, so the answer does not depend on anything skopeo
  says about the manifest. `Copy` takes `--preserve-digests` from the options.
- *Errors.* Always a `*BenchError`. The stderr of skopeo (capped at 64 KiB) only
  picks the kind through `classifySkopeo` and is never in a message, nor is its
  stdout; `Err` is the exit status of the process. A cancelled or expired context
  is `Unclassified` or `Deadline`. `classifySkopeo` matches lower-cased fragments:
  `connection refused`, `no such host`, `i/o timeout` give `RegistryUnreachable`;
  `unauthorized`, `denied:` and `requested access to the resource is denied`
  (a bare `permission denied` of a local folder is `Unclassified`),
  `authentication required` (what skopeo 1.18.0 says to
  a push without a login, found by the real-skopeo test) and a whole-word `403`
  (a port like 40399 does not count) give `RegistryRefused`; `manifest unknown`
  gives `SeedFailed`; anything else `Unclassified`.

**Accepted risks of the two doors** (audit of DEVDESK-539, single-user development
machine):

- The loopback guard checks the host of the registry reference only. It does not
  check the `realm` of a Bearer challenge a registry sends back, so a registry
  could point skopeo at another host to fetch a token; the registries the bench
  talks to are the bench's own (the upstream through the port-forward, the bench
  Nexus), which do not do it.
- A SIGKILL of the Go process skips the `defer`s and the context kill: a
  `kubectl port-forward` can outlive it, and the temporary folder
  (`/tmp/devdesk-skopeo-*`, `0700`, with an authentication file `0600` holding the
  admin password while a call ran) can stay on disk. Nothing sweeps them; remove
  them by hand.
- While a port-forward lives, its `127.0.0.1:<port>` is reachable by every local
  account, without credentials of the upstream (which is anonymous by design).
- `policy.json` is `insecureAcceptAnything`: no signature is checked. It is
  bounded by the loopback guard (only the bench's own registries are reached),
  not by the policy.

**Tests.** The two doors are tested with the "helper process" pattern
(`helper_process_test.go`): a one-line shell shim named `kubectl` or `skopeo` is the
only thing on PATH and re-runs the test binary, which records its arguments,
environment and the files it was pointed at (authentication file, policy,
`registries.conf`, read while they exist), answers as the test asked and exits or
hangs. `TestSkopeoAgainstFakeRegistry` runs the real skopeo against
`fakeregistry_test.go`, an in-process OCI registry on the loopback with an
optional Basic challenge (skipped without skopeo): HTTP fallback, credentials from
the authentication file, `--preserve-digests` byte for byte, and the error kinds.
None of it needs the cluster or `requireBench`; the source guards (below) list why
each such test is exempt.

## The upstream lifecycle (`upstream.go`, DEVDESK-540)

`UpstreamState` is `UpstreamMissing` < `UpstreamCut` < `UpstreamStarting` <
`UpstreamReady`. Neither function runs a preflight: the callers do (`Deploy`,
then the seed, `RunPreflight(OpSeed)`).

`ReadUpstream(ctx, runner, p)` reads only: `get deployment bench-upstream -n <ns>
-o json` (the object by name, never the list), then, when one replica is wanted,
`get pods -n <ns> -l app.kubernetes.io/name=bench-upstream -o json`. A Deployment
that is not there (`NotFound`) is Missing; zero replicas is Cut, even while an old
pod is still terminating; otherwise a pod that is not terminating and whose `Ready`
condition is `True` is Ready, else Starting (a pod that fails to pull is Starting,
not an error: diagnosing is the wait's job). A failed read is a `*BenchError`
classified like every other call.

`EnsureUpstream(ctx, runner, clock, p, out)` reads first and writes at most once:

| State read | Does |
|---|---|
| Missing | an error naming `bench-upstream` and `mise run nexus-bench:up` (`Deploy` applies the Deployment); no write |
| Ready | nothing: no write, no wait |
| Starting | no write; the wait |
| Cut | `kubectl scale deployment/bench-upstream --replicas=1 -n <ns>` once (a failed scale is classified and not retried), then the wait |

A custom upstream (`!p.UpstreamIsBench()`) is refused before any call, with an
error that names `NEXUS_BENCH_UPSTREAM_URL`: the bench scales the upstream it
deploys and nothing else. The wait is the one of `Deploy` without the hosts: a
round (`round()`, shared) reads the pods of the upstream and the claim
`bench-upstream-data`, diagnoses them with `upstreamWorkload()`, then ends on a
ready pod, on the deadline (`Params.Timeout`, within one interval; a `Deadline`
that cites the pod and names `NEXUS_BENCH_TIMEOUT`, or the pending reading that
was still there) or sleeps `upstreamPollInterval` (2 s: a registry starts in
seconds, and nothing is probed). A cancelled context is returned as is.
`CutUpstream` and the recycling of the storage: see "Cut, restore and reset".

**Rights.** `up` gained, after its fourteen, patch `deployments/scale`, create
`pods/portforward` and get `services` (namespace); `seed` asks get `deployments`,
list `pods`, get `pods`, get `services`, create `pods/portforward` and patch
`deployments/scale`. `kubectl port-forward svc/bench-upstream` fetches the Service,
lists the pods of its selector, gets the pod it picks, then posts to
`pods/portforward`: that is why `get services` and `get pods` are asked, on top of
`list pods`.

## The seed (`seed.go`, DEVDESK-542, NX-02a)

`Seed(ctx, SeedDeps{Runner, Forwarder, Copier, Clock, Store}, baseURL, p, out)`
puts the data set (D-I, `Dataset()`) in place and returns it as a `SeedResult`
(digests filled, `PullThrough` true exactly for the images cached by
`docker-proxy`). It never scales, applies or writes to Nexus (every request it
sends is a `GET` as `admin`), and it runs no preflight: the caller does.

**Order.**

1. The stored credentials (`adminFromStore`, then the `bench-pull` password): a
   missing one is `ProvisionFailed` that says to run `mise run nexus-bench:up`,
   before any kubectl call or request.
2. The upstream: a custom `NEXUS_BENCH_UPSTREAM_URL` is `SeedFailed` (the proxy
   would not read what the seed pushes); then `ReadUpstream`: cut (`the upstream
   is cut`, run `nexus-bench:restore-upstream`), missing (run `up`) and starting are
   `SeedFailed` with no write and no copy. `restore-upstream` brings it back.
3. The OCI layout of the whole data set in a folder made under `TMPDIR`
   (`os.MkdirTemp`), removed when Seed returns; the layout reference of an image is
   its index in `Dataset()`, so the two `shared/base` images do not mix.
4. One port-forward to `svc/bench-upstream:5000`, stopped by a `defer` on every
   path. Seed checks itself that its address is `127.0.0.1:<port>` before any call
   to the copier. The upstream images are pushed, in the order of `Dataset()`, with
   no credentials; the forward is stopped before the next phase.
5. The `docker-hosted` images, in order, with `admin` (`nexusImageRef`).
6. The pull-through: the `PullThrough` images are copied from `docker-proxy` to a
   throwaway `dir:` folder with `bench-pull`. `docker-proxy` is listed first by
   REST; an image it lists is not pulled again unless the listed digest differs
   from the data set's.
7. The listings, by REST as `admin`: `docker-proxy` must hold exactly the
   `PullThrough` images and `docker-hosted` exactly the hosted ones (name and tag
   are enough: a component listed without a digest counts as present; a digest it
   does list must be the one of the data set, else it is a failure). A missing one is
   waited for, by `Clock`, 30 s at most (2 s polls, the same bound for both
   repositories, H-COMPONENTS), then `SeedFailed`. A component outside the data set
   fails at once, without waiting, with `<repo> holds images outside the dataset`
   and `run mise run nexus-bench:reset`.

**Idempotence.** An image is copied only when `ManifestDigest` at its destination
differs from the digest of the data set. Absent means `errors.Is(err,
errImageAbsent)`: `ManifestDigest` (through `inspectFailure`) puts that sentinel in
the chain of its error only when `isImageAbsent(stderr)` proves the absence
(`manifest unknown`, `name unknown`, `NAME_UNKNOWN`, `not found` or a whole-word
`404`, any case) and the output is not one `classifySkopeo` reads as an unreachable
or refusing registry (a refusal that quotes a 404 proves nothing). No manifest
printed, a manifest above 4 MiB, an unreachable or refusing registry, an
interrupted call and any unclassified output carry no sentinel: every other read
error stops the seed. Every `Copy` carries
`--preserve-digests`. A second run copies nothing (CA6).

**F9.** `mirror/uncached` is never read through `docker-proxy` or `docker-group`:
a read caches it, and only `reset` removes it. Seed pulls through the proxy only
the `PullThrough` images and never reads the group.

**Credentials.** None for the upstream, `admin` for `docker-hosted`, `bench-pull`
for `docker-proxy`. No password reaches `out`, an error or an argv. Every failure
is a `*BenchError`; a `*BenchError` of a door keeps its kind and gains the step
(`copying api:1.0.0 to docker-hosted: ...`), any other error becomes an
`Unclassified` one that keeps the cause for `errors.Is` and prints none of it. A
cancelled context is returned as is (`errors.Is(err, context.Canceled)`). Output
lines start with `seed: `; a nil writer is accepted.

**References.** `nexusImageRef(p, repository, img)` is the one place that builds
`127.0.0.1:<HTTPNodePort>/<repository>/<name>:<tag>`; `upstreamImageRef(local,
img)` builds `<local>/<name>:<tag>`.

**Settled on the bench (step 12).** H-PATH, H-PUSH and H-COMPONENTS were checked on
2026-11-17: see "Observed on the real bench (NX-02a)".

## Observed on the real bench (NX-02a, datomir, 2026-11-17)

Nexus `3.98.1-01` COMMUNITY (EULA accepted by the user beforehand), `registry:3.0.0`
upstream, skopeo 1.18.0, `datomir` (k0s). The first two runs of `up` stopped, on the SSRF protection and on the path
routing (below); once both were fixed `up` ran to the end, and a further run copied
nothing and left `descriptor.json` unchanged byte for byte (CA6); `nexus-bench:status` is clean (`descriptor: matches`, `dataset:
matches (6 images compared)`); the three read-only integration tests and
`TestIntegrationDatasetAsDescribed` pass against the bench.

| Hypothesis | Verdict | Evidence |
|---|---|---|
| H-PATH docker-hosted and docker-proxy answer by path on 8081 | **false by default, true with `docker.pathEnabled: true`** | `GET /repositories/docker/hosted/docker-hosted` shows `docker.pathEnabled: null`; `GET /v2/`, `/v2/docker-hosted/` answer 404 on 30481 and skopeo stops on `pinging container registry ... StatusCode: 404`. After a `PUT` with `pathEnabled: true` on the hosted and the proxy: `/v2/` 401, `/v2/docker-hosted/` 401, `/v2/docker-hosted/api/tags/list` 404 (unknown image) and the push works. The swagger documents the field (`DockerAttributes.pathEnabled`: "Allows to use repository name in Docker image paths"). Not a case for port connectors: `Provision` now sends the field (the fallback the plan foresaw) |
| H-PUSH skopeo pushes to the hosted over HTTP with Basic | verified | `skopeo copy --preserve-digests --dest-tls-verify=false --dest-authfile` from the layout to `127.0.0.1:30481/docker-hosted/api:1.0.0` exits 0; `skopeo inspect --raw` back hashes to the digest of the data set |
| H-LAYER uncompressed layer, deterministic digests | verified | the uncompressed `application/vnd.oci.image.layer.v1.tar` is accepted under `strictContentTypeValidation: true`; the eight manifest digests the registries serve are the ones of the golden (`status`: `dataset: matches`); `layerGzip` stays `false`, so the digests do not depend on the `compress/gzip` output (one Go version was used, the tar writer is the standard library's) |
| H-PROXYHTTP the proxy accepts an `http://` upstream | verified | `docker-proxy` created with `http://bench-upstream.nexus-bench.svc.cluster.local:5000`; the pull-through of `mirror/cached:1.0` through it succeeds |
| H-DNS the Nexus pod resolves the Service name | verified | same pull-through; the SSRF refusal itself said `Host resolves to ... 10.110.133.156`, so Nexus resolved the name |
| H-UPRUN `registry:3.0.0` runs as UID 1000 on `local-path` | verified | pod Ready with `uid=1000 gid=1000`, pushes persist; idle 1m CPU, 19 Mi memory (requests 50m / 64Mi, limit 256Mi) |
| H-SCALE `kubectl scale` needs `patch deployments/scale` | verified | `kubectl scale ... --dry-run=server -v=6` does `GET deployments/bench-upstream` then `PATCH deployments/bench-upstream/scale` |
| H-COMPONENTS the listing shows the image once pushed or pulled | verified | see the bullet under "REST client and provisioning": nested names, manifest checksum = digest, no wait needed |
| H-UNBLOCK, H-STALE, H-CUTDELAY, H-REPODEL | **settled in NX-02b** | see "Cut, restore and reset" below: unblock held (the `PUT` lifts the block at once), stale held, cut delay 20 s, repository deletion 204 and empty at once |
| H-SSRF (new) a proxy to a ClusterIP upstream is accepted | **false**, fixed | see below |

**H-SSRF (not anticipated).** The first `up` stopped at the proxy: `400` on
`proxy.remoteUrl`, and the Nexus log said `Proxy URL blocked: Host resolves to
private/local IP address(es): 10.110.133.156 (private network address). To allow
connections, update the SSRF protection settings via the API at
/v1/security/ssrf-protection.` Nexus 3.98 logs `SsrfProtectionDefaultMigrationStep_2_120
- New installation detected, enabling SSRF protection by default`. The endpoint is
`GET`/`PUT /service/rest/v1/security/ssrf-protection` with
`SsrfProtectionConfigurationXO {enabled (required), allowedIPs[], allowedDomains[]}`,
both lists "allowed to bypass SSRF protection"; the instance starts with
`enabled: true` and both lists empty. The narrowest setting is the domain of the
bench upstream in `allowedDomains` (a Service name is stable, a ClusterIP is not):
the protection stays on, no other host is allowed, and it was enough: the proxy was
created at once. A domain match does not depend on the resolved address, so the
Service can be recreated without touching the setting. Not tried: `allowedIPs`, the
protection off. The decision is in D158 (13).

**Measures.**

- Time: a run of `up` on the seeded bench takes 1.7 s (`go run` included); the first
  seed (four images to the hosted, one pull-through, three to the upstream) fits in
  the 2.6 s of that run.
- Nexus after the seed: 113m CPU and 1328 Mi memory (`kubectl top`), against 55m and
  1257 Mi idle before NX-02.
- What `maxNexusResponse` (`1 << 20`, `internal/registrymgr/nexus.go`) bounds is
  `GET /service/rest/v1/repositories/<name>` (generic metadata) and the group
  endpoint, not the catalog or the tags: on the bench the metadata of `docker-group`
  is 173 bytes and of `docker-proxy` 276 bytes (the group endpoint answers 403
  without an administrator account); the catalog (`maxCatalogPage`, `1 << 20`, in
  `internal/oci`) through the group connector is 95 bytes for six names, a tag list
  36 to 39 bytes; the REST component listings (not read by DevDesk) are 7516 bytes for
  the five hosted images and 1452 bytes for the one cached image, that is about 1.5
  KB per image, so `1 << 20` would hold roughly 700 images in a listing. The constant
  is unchanged.
- A catalog read through the group (or the proxy) lists `mirror/uncached` although
  the proxy holds no component for it: Nexus forwards `_catalog` of a proxy to its
  upstream. The components of `docker-proxy` stayed at `mirror/cached` alone after
  that read, so it does not cache. What a name in the catalog means for NX-03 to
  NX-05 is not "cached"; only the REST components say what the proxy holds.
- `registry:3.0.0` logs at debug level by default (the readiness probe and the
  traces fill the log); `REGISTRY_LOG_LEVEL` is not set by the manifest.

## Cut, restore and reset (`upstream.go`, `restore.go`, `reset.go`, `repoapi.go`, DEVDESK-534, NX-02b)

Three commands, three mise tasks (`nexus-bench:cut-upstream`, `nexus-bench:restore-upstream`,
`nexus-bench:reset`). Each runs a preflight (`OpCut`, `OpRestore`, `OpReset`, see "Errors,
runner and preflight"); `reset` runs its own, as `down` does, the two others get theirs from
the command. None of them can be run on a custom upstream (`NEXUS_BENCH_UPSTREAM_URL`): the
bench scales and empties only the upstream it deploys.

**`cut-upstream`** (`CutUpstream`). Reads the upstream, scales it to zero replicas when it is
not already cut (one write, never retried), then waits, until `Params.Timeout`, for no pod of
it to be left, a terminating one included (its volume is still mounted). Idempotent. It
touches neither Nexus nor the descriptor and needs no skopeo, no store and no REST host.
Measured: about 2.5 s.

**`restore-upstream`** (`RestoreUpstream`). Order: the stored credentials are read first (before
any write); `EnsureUpstream` (one replica, wait for ready); `UnblockProxy`; a check, through a
port-forward and with no credential, that each upstream image has the digest of the data set;
a read of each `PullThrough` image through `docker-proxy` as `bench-pull`, digest equal.
It writes no descriptor and copies no image. `UnblockProxy` reads `docker-proxy` (absent is an
error: `up` creates it), sends again the configuration `Provision` writes (`repositorySpecs`,
never one read back from the instance), which lifts the automatic block, then `POST
/v1/repositories/docker-proxy/invalidate-cache`. Measured: 3 to 7 s with the wait for ready.

**`reset`** (`Reset`). Order: preflight `OpReset`; custom upstream refused; loopback origin
checked; stored credentials, checked by one admin `GET` (a refused password stops everything
before the first deletion); label of the upstream claim checked; `DELETE` of `docker-group`,
`docker-proxy` and `docker-hosted` (a 404 is already done); replacement of the upstream
storage (below); `Provision`; a wait of 30 s at most (`seedListWait`) for `docker-hosted` and
`docker-proxy` to list nothing; `Seed`. The command reads the previous descriptor before
anything and writes it again after a success, with the previous hosts (a reset reads no node);
the file is the same byte for byte. Measured: about 9 to 11 s. `reset` never changes the admin
password (`Provision` gets an `InitialPassword` that never reads the pod) and never rolls or
restarts the Nexus pod; `nexus-data`, the namespace and the passwords are kept.

**Storage of the upstream** (`recycleUpstreamStorage`). `CutUpstream`; the claim
`bench-upstream-data` is read and its label `app.kubernetes.io/managed-by=devdesk-nexus-bench`
checked (D-P: a claim the bench did not create is never deleted); it is deleted with
`kubectl delete --raw /api/v1/namespaces/<ns>/persistentvolumeclaims/bench-upstream-data -f -`,
whose body is a `DeleteOptions` with `preconditions.uid` set to the UID just read. `kubectl
delete pvc --wait=false` was not chosen because it has no precondition: a claim replaced since
the read would be deleted. A claim read without a UID is refused. The delete is sent once; the
wait for `NotFound` lasts until `Params.DeleteTimeout`; then the claim alone (document 5 of
`Render`, `renderUpstreamClaim`, never the whole stream, which would roll the Nexus pod) is
applied again and `EnsureUpstream` runs. A claim already gone is not deleted again, and one
still `Terminating` is waited for.

> **Warning.** Never put `dryRun` in the query of the raw path (`...?dryRun=All`):
> `kubectl delete --raw` does not honor it and the claim is really deleted (seen on the
> bench, DEVDESK-534 step 9). There is no dry run for this call; check with `get pvc`.

**Half-way.** An error after the first deletion keeps its kind and cause and ends its check
with "the reset stopped half-way: run `mise run nexus-bench:reset` again, it starts over".
What was deleted stays deleted; the reset is idempotent. The one exception is the
repositories that never empty (`errRepositoriesNeverEmpty`): running again would wait for the
same thing, so the check points at the log of the Nexus pod. A cancelled command prints that
the data set may be half deleted.

**Rights.** `cut`: get `deployments`, list `pods`, patch `deployments/scale`. `restore`: the
list of `seed`. `reset`: that list, plus delete, create and patch `persistentvolumeclaims`;
never `pods/exec`.

**Loopback guard.** `restore-upstream` and `reset` send the admin credentials in clear text
and go through the port-forwards of this machine, so `--rest-host` must be `127.0.0.1` or
`::1` (`IsLoopbackRESTHost`, the one predicate of the command and the library). `localhost`,
another loopback address (`127.0.0.2`, `::ffff:127.0.0.1`) and any name are refused;
`requireLoopbackBase` also refuses an origin with user information and parses the host, so
`127.0.0.1@evil.example` fails. The port-forward address is checked too
(`checkLoopbackForward`, shared with the seed).

**Closed lists.** `benchRepositories` (`docker-group`, `docker-proxy`, `docker-hosted`) are the
only repositories `deleteRepository` accepts; `invalidatableRepositories` (`docker-proxy`,
`docker-group`; a hosted repository has no cache) the only ones `invalidateCache` accepts. A
name outside a list is refused before any request, so a name never decides the path the admin
credentials go to.

**`up` with a cut upstream (D-Q).** `up` reads the upstream before bringing it back; if it was
cut, it calls `UnblockProxy` after `Provision`, so a second `up` leaves the bench nominal. With a
cut upstream and a `--rest-host` that is not loopback, `up` stops before scaling anything (a
refusal later would leave the proxy blocked, since a second `up` finds the upstream ready). If
the unblock itself fails, the error says to run `restore-upstream`.

**What to know when using it.**

- After `reset` the Nexus blobs of the deleted repositories are not compacted: `nexus-data`
  grows with each reset. How to get that space back is not measured.
- The admin and `bench-pull` credentials go to the NodePort on the loopback and the images
  through it: this assumes this machine is the node (as before NX-02b; not new).
- Reading `mirror/cached` through `docker-proxy` after a restore proves the proxy still has it
  in its cache, not that the proxy is unblocked. `mirror/uncached` is what proves the unblock:
  it is served only if the proxy reaches the upstream again.
- While the upstream is cut, the first read of an image that is not cached fails after about
  20 s (the connect timeout of Nexus) and puts the proxy in `AUTO_BLOCKED_UNAVAILABLE`; the
  next reads answer in 0.03 s with `manifest unknown`, which does not mean the image does not
  exist.

**Measured on the real bench (2026-11-17, datomir).** Source: `.claude/plans/DEVDESK-534-measures.md`
(steps 0 and 9).

| Hypothesis | Verdict | Evidence |
|---|---|---|
| H-CUTDELAY | **held** | first read of an uncached image with the upstream at zero replicas: 20 s (`502`, connect timeout, proxy `AUTO_BLOCKED_UNAVAILABLE`), then 0.02 to 0.03 s (`manifest unknown`); pods gone 1 to 2 s after the scale |
| H-UNBLOCK | **held** | the `PUT` of the proxy configuration lifts the block (0.8 s); without it Nexus unblocks itself after 21 s (100 s cut) to 79 s (4.5 min cut), growing with the cut. `restore-upstream` serves `mirror/uncached` at once (0.04 s, digest of the data set). `invalidate-cache`: 204 |
| H-STALE | **held** | cached images are served while cut (0.03 s) with the digest of the data set, also after `invalidate-cache` and the refreshing read, and after the upstream storage was replaced (the upstream was empty) |
| H-REPODEL | **held** | `DELETE` of the three repositories: 204 x3; recreated empty at once (no wait seen); the group connector 8082 answers 401 again; the Nexus pod is not restarted |
| H-PVCDEL | **held** | the claim is `NotFound` 0.09 s after the delete; its UID changes on each reset; `kubectl delete --raw` with a wrong UID: `409 Conflict` |

Timings: `cut-upstream` 2.5 s, `restore-upstream` 3 to 7 s, `reset` 9 to 11 s. The UIDs of the
namespace, the Nexus pod (0 restart) and `nexus-data` did not change across the resets.
`DEVDESK_NEXUS_BENCH_SCENARIOS=1 mise run nexus-bench:test` (`bench_scenarios_integration_test.go`:
`TestIntegrationUpstreamCutRestore`, `TestIntegrationResetKeepsInstance`) passes in 52 s.

## Recorded fixtures (`internal/nexusfixture`, `record`, DEVDESK-511, NX-08a)

Plan `.claude/plans/DEVDESK-511-plan.md`. Tests of the Nexus-facing code (image
search, group discovery) replay answers measured on the bench instead of
hand-written ones, with no bench, no cluster and no secret: `mise run test` needs
none of them. Scope is partial: the NX-03 measures
(`docs/measurements/2026-11-17-nexus-catalog.md`), the NX-04 ones (a proxy whose
upstream is cut, `docs/measurements/2026-11-20-nexus-cached-only.md`) and the NX-05 ones
(routing, authentication, Bearer realm, redirects, `Link`,
`docs/measurements/2026-11-20-nexus-access.md`), all on Nexus 3.98.1-01 Community, exist as
fixtures. See "What is not recorded yet" below.

### Where things are

| Path | Role |
|---|---|
| `internal/nexusfixture/` | format, parser and writer (`format.go`), embedded loader (`load.go`), `Redact` (`redact.go`), `CheckLeaks` (`leaks.go`). No `testing` import: the recorder imports it. |
| `internal/nexusfixture/fixtures/<campaign>/*.http` | the fixtures, embedded with `go:embed`. Three campaigns today: `catalog` (75 files), `cachedonly` (17 files, NX-04) and `access` (43 files, NX-05). |
| `internal/nexusfixture/fakenexus/` | the replay server. Imports `testing`: import it from `_test.go` files only. |
| `internal/nexusbench/record.go`, `cmd/nexus-bench/record.go` | the recorder and its command. |
| `internal/nexusfixture/stale_test.go` | the stale-fixture test. |
| `internal/imagesearch/catalog_fixture_test.go`, `internal/registrymgr/nexus_fixture_test.go` | the replay tests of the `catalog` campaign. |
| `internal/imagesearch/cachedonly_fixture_test.go`, `internal/ui/oci_resources/browser_cached_only_fixture_test.go`, `internal/oci/manifest_fixture_test.go` | the replay tests of the `cachedonly` campaign (see "The `cachedonly` campaign" below). |
| `internal/oci/registry_fixture_test.go`, `internal/imagesearch/access_fixture_test.go`, `internal/ui/oci_resources/browser_nexus_routing_fixture_test.go` | the replay tests of the `access` campaign (see "The `access` campaign" below). |

### Format of a fixture

One file is one fixture, LF line endings only (a carriage return is refused).
The file name is `<scenario>--<identity>.http`; the identity is `admin`, `pull`,
`anon` or `badcred` (`nexusfixture.BadCredentials`: the `admin` account with the
public wrong password `fakenexus.BadPassword`, the way to record a refused login)
and comes from the suffix alone. A file is a commented header, then one
or more exchanges:

```
# scenario: <free text>                      required
# nexus: <version> <EDITION> (<free text>)   required
# measured: <YYYY-MM-DD>[, <free text>]      required
# account: <free text>                       required
# redacted: <free text>                      required
# state: <free text>                         optional, "" means nominal
# flow: <bearer | redirects>                 optional, how the recorder follows an answer
# effect: caches                             optional, the request can cache an image in the proxy
# record: <replay | pending | skip: reason>  optional, default replay
# recorded-by: nexus-bench record            written by the recorder only

GET /v2/_catalog HTTP/1.1
Host: 127.0.0.1:30482
Authorization: Basic <redacted>

HTTP/1.1 200 OK
Server: Nexus/3.98.1-01 (COMMUNITY)
Content-Type: application/json

{"repositories":[...]}
```

- The request carries `Host`, `Authorization` (`Basic <redacted>` or `Bearer
  <redacted>`, or none for `anon`) and optionally `Accept`.
- The body is the text after the blank line. Two stand-ins exist: `<empty body>`,
  and a summary for a body that is not replayed: `<HTML body omitted: N bytes,
  <title>T</title>>`, or `<body omitted: N bytes, media/type>` for another media
  type. The fake replays `<empty body>` as empty and the HTML stub as a minimal
  page with that title; a test that needs the real body cannot use a stub.
- Several exchanges in one file are separated by a `###` line (a body line made of
  `###` is written with one backslash more and read back with one less). The
  optional `flow` header says why a file holds several: `bearer` (the 401 with its
  `WWW-Authenticate: Bearer` challenge, the token request, the retry with the token)
  or `redirects` (each hop of a same-origin redirect chain); `parse` refuses any
  other value, naming the header. `effect: caches` marks a request that can leave
  an image in the proxy (a HEAD of a manifest through it); it is the only value.
  Both are written after `state` and before `record`, and `nexus-bench record`
  honours them (see below). A flow that stops short says why in the parentheses of
  the `nexus:` header, which the recorder rewrites on every run:
  `realm off-origin, not followed`, `realm not followed` (a realm on an origin of
  the run that is no token path, or has a scheme or user information the recorder
  refuses), `no challenge`, `off-origin redirect, not followed` (also `no Bearer
  challenge`, `token refused, not retried`, `redirect to a request outside the
  closed list, not followed`).
- `record: pending` marks a request-only fixture (no response section) that
  `record` is to fill. `Load` refuses a pending fixture.
- `Format(Parse(b)) == b` for every embedded file (`TestEveryFixtureRoundTrips`).

The `nexus:` header gives the version (first token) and the edition (second); the
stale test reads them. Identity is by file name, not by header: the fake and the
recorder choose the credentials from it.

### Where fixtures come from

Of the 75 files of `catalog`, 63 are copies, byte for byte, of the dated evidence in
`docs/measurements/nexus-catalog/` (the NX-03 answers) and 12 were recorded by
`nexus-bench record` (DEVDESK-580).

The copies: `catalog-*` (group connector, hosted root, and the three repository
paths, each as admin, pull and anon), `catalog-{hosted,proxy,group}-path--{admin,pull,anon}`
and `catalog-{hosted,proxy,group}-path-on-connector-30482--admin` (the 12 answers of
the catalog by member path, DEVDESK-579), `page-group-n1000--admin`,
`rest-repositories-*` (the three formats and the group detail endpoint, three
identities), the two `v2-ping-*`, and the 21 imported by DEVDESK-580 (PR1): the
pagination measured by NX-03, `page-group-n1-p0..p5`, `page-group-n2-p0..p4`,
`page-hosted-n2-p0..p1`, `page-proxy-n2-p0..p1` and `page-group-n6` (16, admin), and
five `tags-*` files for the tag lists (`tags-hosted-api-path`,
`tags-hosted-api-v2-repository`, `tags-{proxy,hosted,group}-shared-base`, admin). They are
copied, not moved: the measurement note keeps its paths and the evidence stays as
measured. Not imported: the `*-bearer--*` files (two exchanges), `manifest-head-*`
(a read through the proxy caches), `*-after-measures`, `bench-status-*`, `ctx-*`,
the other `*-path*` files, `tags-group-mirror-uncached` (no proof it had no cache
effect) and `tags-hosted-api-repository` (outside the recorder's closed list,
postponed to the NX-05 PR).

The 12 recorded ones carry `# recorded-by: nexus-bench record` and the date
2026-11-20: the answers to the `n=1000` DevDesk sends, for the three identities,
`catalog-hosted-root-30481-n1000--*` (404, the HTTP-port root),
`catalog-group-path-n1000--*` (404), `catalog-hosted-path-n1000--*` (200, 4 names) and
`catalog-proxy-path-n1000--*` (200, 3 names). They were written from `pending`
request files, as for any new fixture.

The 17 files of `cachedonly` were all recorded by `nexus-bench record` on 2026-11-20 (NX-04,
written from `pending` files too, then frozen byte for byte in
`docs/measurements/nexus-cachedonly/`, whose note is
`docs/measurements/2026-11-20-nexus-cached-only.md`). None is imported from older evidence.

The 43 files of `access` were all recorded by `nexus-bench record` on 2026-11-20 (NX-05, written from
`pending` files too, then frozen byte for byte in `docs/measurements/nexus-access/`, whose note is
`docs/measurements/2026-11-20-nexus-access.md`). The `w5-` and `w6-` ones were recorded by the guarded
scenario `TestIntegrationDevDeskAccessVariants`, which writes to the bench and puts it back. None is
imported from older evidence.

A fixture is either byte-identical to its dated evidence, or it says
`# recorded-by: nexus-bench record` (a regenerated file). The check catches a
hand-edited copy of measured evidence and an unknown `recorded-by` value, but it
cannot prove a fixture was recorded. The `recorded-by` header is declarative:
`CheckProvenance` accepts any fixture that declares `nexus-bench record` with no
further check, so a hand-written file carrying that line passes. The guard
against that is PR review, not code.
`TestEveryFixtureIsMeasuredOrRecorded` runs `CheckProvenance(fixture, repoRoot)`
over every embedded campaign (not only `catalog`, so a campaign added later
cannot escape it); a fixture with no header is compared byte for byte with
`EvidenceDir(campaign)`, and the test names the offender. A regeneration rewrites
the fixtures only, never the evidence.

### The `cachedonly` campaign (DEVDESK-580, PR3, NX-04)

17 files, all recorded by `nexus-bench record` on 2026-11-20, frozen as evidence in
`docs/measurements/nexus-cachedonly/` (`evidenceDirs["cachedonly"]`; the copies are
byte-identical to the fixtures). The file name prefix says in which state of the bench
the request was sent, and the order matters (the files of a campaign are sent in lexical
order):

| Prefix | State | What it holds |
|---|---|---|
| `nominal-` | upstream reachable | the proxy's `_catalog` (admin, pull, anon), `tags/list` of `mirror/cached` and `mirror/uncached`, and `nominal-zz-head-...` (a HEAD of the uncached manifest through the proxy, `effect: caches`, sent last) |
| `cut1-` | upstream cut, proxy not yet blocked | the first read, `_catalog` of the proxy: 502 after about 20 s |
| `cut2-` | upstream cut, proxy auto-blocked | the same read (3 identities), the hosted catalog (control), the group connector's catalog, `tags/list` through the proxy and the group, HEAD of both manifests |

Two requests recorded in different states are the SAME request with different answers
(`GET /v2/docker-proxy/_catalog?n=1000` as `nominal-`, `cut1-` and `cut2-`), and fakenexus
refuses two recordings of one request: a test builds one fake per state from a chosen
subset of fixtures, never from the whole campaign (`fakenexus.New(t, picked...)`).

The replay tests pin what the measure says, including what contradicts the criteria of
NX-08b; each starts by checking the recorded status of the fixtures it uses, so a
regenerated fixture with another answer fails there and names the file. The three that
cover NX-04: `TestProxyCatalogOnMeasuredCutUpstream`, `TestHostedAndGroupCatalogOnMeasuredCutUpstream`
and `TestFailedNexusAnswerKeepsItsCause` (`internal/imagesearch`),
`TestCachedOnlyLineOnMeasuredCutUpstream` and the group-connector one
(`internal/ui/oci_resources`), `TestManifestDigestOnMeasuredCutUpstream` and
`TestTagsOnMeasuredCutUpstream` (`internal/oci`). What they cannot replay: the delay. The
first read after the cut takes Nexus about 20 s to answer its 502, the registry client gives
up after 15 s, so on the live bench DevDesk shows `failed` with a deadline and no
`*oci.StatusError`. That verdict, that 20 s delay and the dataset status the bench printed
after the HEAD (`dataset: differs`) are **reported by the session of 2026-11-20, not
replayable by a fixture**: the guarded integration test `TestIntegrationDevDeskOnACutUpstream`
(`internal/nexusbench`, `DEVDESK_NEXUS_BENCH_SCENARIOS=1`, asserts nothing, logs verdict,
cause and duration) was not replayed for this PR. H-CUTDELAY (20 s) was measured in NX-02b on
a read of an uncached **manifest**, not on `_catalog`; that `_catalog` shows the same delay is
the report above, not a measurement of this repository.

The hosted catalog is a fixture of this campaign in the cut state only
(`cut2-catalog-hosted-path`, a control); its nominal answer is `catalog-hosted-path-n1000` of
`catalog` (NX-03). Likewise the HEAD of the shared homonym through the group
(`manifest-head-group-shared-base--admin`) is a proof of NX-03
(`docs/measurements/nexus-catalog`), not a fixture embedded here.

A campaign that still holds a `record: pending` file cannot be loaded, so the provenance,
version and redaction checks skip it. `TestNoPendingFixtureIsVersioned` (`internal/nexusfixture`)
fails the suite as soon as such a file exists in an embedded campaign, unless
`DEVDESK_FIXTURE_RECORDING=1` is set: that variable is for a local recording session only
(the request files are written, the recorder fills them) and is never set in a run that is
committed or merged.

Regeneration and the date. Five bodies hold `Remote Auto Blocked until <date>`, the end of
the block, which changes at every recording. The stale test reads only the version and the
edition, `TestRedactLeavesARealAnswerAsItIs` and the leak check do not care about a date, so
nothing goes red; but `sameAnswer` compares whole exchanges, so a regeneration (after
`cut-upstream`, in the `cut1-` then `cut2-` order) reports those five files `written` with a
new date, never `unchanged`. That is churn in the diff, not a divergence. Neither
`record: skip` nor a redaction rule was used: `skip` would stop the recorder from regenerating
those files and the stale test does not exempt a skipped fixture (it would stay red after a
version bump until a hand edit), and a redaction rule changes the bytes of files the recorder
wrote, which means recording them again on the bench. The replay tests match the message by its
prefix (`Remote Auto Blocked until `) and check the rest parses as a date, never the date
itself: keep it that way.

### The `access` campaign (DEVDESK-580, PR4, NX-05)

43 files, all recorded by `nexus-bench record` on 2026-11-20, frozen in `docs/measurements/nexus-access/`
(`evidenceDirs["access"]`, byte-identical copies). By family and state:

| Prefix | State | What it holds |
|---|---|---|
| `routing-tags-*` (9), `tags-page-hosted-n1` | `nominal` | `tags/list` of `api` by path, by `/repository/<r>` and on the group connector, for admin, pull and anon; one page of `?n=1` with its `Link` |
| `bearer-*` (6) | `nominal`, `flow: bearer` | `GET /v2/` anonymous on both ports (401 Bearer, token, retry: three exchanges); catalogs for anon (200) and `badcred` (401 BASIC) |
| `realm-probe-*` (2) | `nominal`, `flow: bearer` | `GET /v2/` with `X-Forwarded-Host: realm-probe.invalid` and `X-Forwarded-Proto: https`: the realm follows the announced host; one exchange (the recorder does not follow an off-origin realm) |
| `redirects-*` (7) | `nominal`, `flow: redirects` | `GET /v2` without its slash, `/v2/<repo>`, `/repository/<r>/v2`, the HEAD of a hosted manifest, the GET of a hosted blob: no redirect anywhere |
| `w5-*` (10) | `realm-off` | the `DockerToken` realm removed from the active realms |
| `w6-*` (8) | `anon-off` | anonymous access cut |

Like `cachedonly`, a state can hold the SAME request as another with another answer (`GET /v2/` anonymous as
`bearer-ping-`, `realm-probe-ping-`, `w5-ping-`, `w6-ping-`, `w5-bearer-ping-`; the same catalog request in `bearer-catalog-`, `w5-catalog-`
and `w6-catalog-`), and fakenexus refuses two recordings of one request: a test builds one fake from a chosen subset,
never from the whole campaign. `fakenexus` keys a request on host, method, target, identity and Bearer-or-not, never on a
request header, so the probe's `X-Forwarded-*` are recorded but not matched.

What the replay tests pin (they check the recorded statuses and challenges first, and fail naming the file if the
measurement changed):

- `TestValidCredentialsNeverEnterTheBearerFlowOnNexus` (`internal/oci`): admin and pull, nominal, W5 and W6: one request,
  Basic at once, none to `/v2/token`.
- `TestBearerFlowOnMeasuredNexusAnswers`: a wrong password is a typed `*oci.StatusError{401, SourceRegistry}` carrying the
  `BASIC` challenge, no token request; an anonymous `GET /v2/` follows 401, token, retry and ends on a plain 401 (nominal and W5); an
  anonymous catalog read with anonymous access cut goes to the realm (what Nexus answers there was NOT recorded and is not asserted).
- `TestMeasuredRealmStaysOnTheRegistryOrigin`: every measured realm is on the origin of its request (and `checkRealmOrigin` accepts it
  with credentials); the probe's off-origin `https` realm gets the anonymous token request when no credentials are configured, and is
  REFUSED when they are (`ErrTokenRealmOffOrigin`, nothing sent to that host), since DEVDESK-629 / D168. The frozen note
  (`docs/measurements/2026-11-20-nexus-access.md`) still describes the earlier behavior (the realm was followed, with Basic); it is not edited.
- `TestRedirectsOnMeasuredNexusAnswers`: no exchange of any campaign carries a 3xx or a `Location` (a future redirect turns it
  red); the direct answers are replayed. The refusal of an off-origin redirect or `Link` stays covered by hand-written tests only,
  named in the test and in the note. The blob GET is a stub (3072 bytes not kept) that fakenexus cannot serve: pinned from the file.
- `TestTagsLinkOnMeasuredNexusAnswers`: the measured `Link` resolves to a same-origin URL the client would follow. The walk of the
  catalog pages (criterion 6) is `TestCatalogWalk*` of PR1.
- `TestCatalogVerdictsOnMeasuredNexusAccess` (`internal/imagesearch`): `refused` for a wrong password, `ok` for admin and for anon
  unless anonymous access is cut.
- `TestURLsDevDeskCallsAreTheMeasuredOnes` (`internal/ui/oci_resources`): the real browser, search and tag list, for the three
  addressing modes (by path with a member `repo_prefix`, by connector, by `/repository/<r>` in the URL of an entry) and the three
  identities; the fake answers only what the bench answered and `Hits` are exact. `TestAccessVariantsThroughTheBrowser`: the same for the
  verdicts, nominal, W5 and W6.

Not measured, so not replayed: subdomain routing, a dedicated connector for `docker-hosted` or `docker-proxy` (W7), `docker.subdomain`
(W8), an off-origin realm or redirect on a real topology, HEAD or GET of a manifest or blob through the proxy or the group, the token
endpoint under W6, the `pull` account under W5 and W6, the second page of the tags.

### Redaction and leak check

`Redact` turns what the recorder read into what may be written:

- `Authorization` becomes `Basic <redacted>` or `Bearer <redacted>`;
- response headers are filtered to an allowlist (`ResponseHeaderAllowlist`:
  `Server`, `Docker-Distribution-Api-Version`, `WWW-Authenticate`, `Location`,
  `Link`, `Docker-Content-Digest`, `Content-Type`, `Content-Length`); request
  headers keep `Accept` and the two headers of the realm probe, `X-Forwarded-Host`
  (a value of `AllowedHosts`) and `X-Forwarded-Proto` (exactly `http` or `https`, never
  a value of `AllowedHosts`); any other value drops the header, and
  `X-Forwarded-For` is never kept;
- an empty body becomes `<empty body>`, an HTML body or any non-JSON body becomes
  a `<HTML body omitted: ...>` or `<body omitted: ...>` stub;
- in a JSON body, the value of a key whose name ends in `token`, `password`,
  `passwd`, `passphrase`, `secret`, `apikey`, `privatekey`, `credential` or
  `uploaderip` (case-folded, plurals and `_`/`-` forms included; escapes are
  decoded first) is replaced in
  place by `"<redacted>"`; every other byte stays as Nexus sent it (a token answer
  keeps its `expires_in`);
- in a JSON body, a private, link-local or CGNAT IPv4 address, with its port, inside
  the value of a `message`, `detail` or `error` key (case-folded) becomes
  `<redacted-address>`: the 502 of a cut upstream names the upstream's cluster
  address. An address in any other value is left for `CheckLeaks` to refuse (all or
  nothing).

`CheckLeaks(name, data, exact)` scans a fixture as text, line by line, with 13
rules: `Authorization` not redacted; cookie or session header (`NXSESSIONID`,
`X-Nexus-UI`); response header outside the allowlist; request header outside
`Host`, `Authorization`, `Accept` and the two probe headers (`X-Forwarded-Host`,
`X-Forwarded-Proto`: `X-Forwarded-Host` goes through the host rule, `X-Forwarded-Proto`
admits exactly `http` or `https` and never a host); a header line that is not `Name: value`;
JWT-like value; other known secret formats (GitHub, GitLab, AWS key, PEM key);
a JSON secret key whose value is not `<redacted>` (`Bearer <redacted>` is the only
form of a Bearer `Authorization`); a host outside `AllowedHosts` (`127.0.0.1`, the
bench's own upstream service, and `realm-probe.invalid`, the reserved name the realm
probe sends in `X-Forwarded-Host`); a private, link-local, CGNAT
or unique-local address; an internal name or local path (`datomir`,
`secrets.json`, `/home/`, `/Users/`, a Windows profile, internal zones such as
`.lan` or `.home.arpa`, looked for in the line decoded and in the line with each `%XX`
turned into a separator, because decoding can make a match disappear: `x.lan%41`,
`nexus.home.arpa%2Dmirror`); a query parameter other than `n` and `last`, except `service`
and `scope` on a token path only (`/v2/token`, `/repository/<name>/v2/token`), where
`service` is URL-decoded and goes through the host rules and `account` stays refused;
and each
`exact` term (also lower-cased, JSON-escaped and URL-escaped). A `Leak` names the
fixture, the line and the rule, never the matched text.

`TestEmbeddedFixturesLeakNothing` runs it over every embedded fixture in `mise run
test`. Names the repository must not carry (an employer's instance, hosts) go in
an **external deny list**: a file outside the repository, one term per line,
blank lines and `#` lines ignored, named by `DEVDESK_FIXTURE_DENYLIST`. When the
variable is unset the test logs that the list was not used; the recorder adds the
same terms to its own check.

Known limits: the rules are fixed patterns, so a name that matches none of them
is only caught by the external deny list, which nobody runs by default; an exotic
encoding of a term (other than JSON and URL escaping, case folding, and the
Basic encoding of the recorder's passwords) is not looked for.

### The fake Nexus (`fakenexus`)

```go
s := fakenexus.New(t, fixtures...)   // fixtures from nexusfixture.Load("catalog")
base := s.URL("127.0.0.1:30482")     // the fake that stands for that recorded Host
```

- **One `httptest` server per recorded `Host`**: `127.0.0.1:30481` (HTTP port,
  REST and path routes) and `127.0.0.1:30482` (the group's Docker connector) get
  two different loopback URLs. `Link`, `Location` and `WWW-Authenticate` values
  naming `http://<recorded Host>` are rewritten to the fake's URL;
  `Content-Length` is recomputed, never copied.
- **Identity comes from `Authorization`**: none is `anon`; Basic with
  `fakenexus.User(id)` (`admin`, `bench-pull`) and `fakenexus.Password`
  (`fixture-password`) is that identity; Basic `admin` with `fakenexus.BadPassword`
  (`fixture-wrong-password`) is `badcred`; `Bearer fixture-token-<identity>` (`admin`,
  `pull`, `anon`; there is no `badcred` token) is that identity, presented as a
  Bearer. Any other credentials match nothing.
- **Token route**: a recorded request on a token path is matched on its path, then on
  its query decoded, after the recorded `http://<Host>` of each value is pointed at
  the fake (so `service` must name the fake the client was sent to, not the recorded
  address). The answer is served with `<redacted>` replaced by
  `fixture-token-<identity of the fixture>`. A request carrying that token matches
  the exchanges recorded with `Bearer <redacted>` for that identity, never another's,
  and an unauthenticated one never matches a Bearer exchange.
- **HEAD**: the recorded headers are replayed, `Content-Length` included (the size a
  GET would have), and no body is written.
- **An unrecorded request is a test error**: `t.Errorf("no recorded answer for
  ...")` and a 501. The fake never invents an answer. Two fixtures recording the
  same host, method, target, identity and Bearer-or-not (checked exchange by
  exchange, also inside one multi-exchange file), or one with no status, also fail
  the test. The error reads `no recorded answer for <method> <target> as
  <identity>`, or `as unrecognized credentials`. `Hits(name)` counts the requests a fixture answered.
- **Catalog tolerance, `n=1000`**: DevDesk always asks `/v2/_catalog?n=1000`
  (`internal/oci/catalog.go`), but NX-03 recorded most catalogs without `n`. An
  exact fixture always wins. Otherwise a `GET P?n=1000` (no `last`) is answered
  by a fixture recorded as `GET P` with no query when that answer is a 200 with no
  `Link` and at most 1000 names. Any other `n` is unmatched. **Measured** (`n=1000`
  and no `n` answer identically): for the group connector (`page-group-n1000--admin`
  against `catalog-group-connector-30482--admin`) and, since DEVDESK-580, for the
  hosted and proxy catalogs by path, the 404 of the group's path and the 404 of the
  HTTP-port root, for the three identities (`catalog-*-n1000--*` against their no-`n`
  twin; `TestTheCatalogToleranceHasMeasuredGround`). Those exact fixtures are what the
  replay tests hit; the tolerance stays for the group connector's pull and anonymous
  identities and the `/repository/<name>/v2/` paths, where it is still an
  **inference** (only no-`n` and `n=2` were measured). A recorded 404 is never
  tolerated: `?n=1000` on a target with no exact fixture is unmatched
  (`TestCatalogToleranceDoesNotServeA404`). A test of the tolerance itself leaves the
  exact fixtures out of the set (`without` in `server_test.go`).
- **Bearer realm**: `v2-ping-*` replays the `WWW-Authenticate: Bearer realm=...`
  challenge (rewritten to the fake). A token endpoint is replayed only when a fixture
  recorded one (a `flow: bearer` file); with none, a client that follows the realm
  gets an unrecorded-request error. No embedded fixture records a flow yet.

### What the replay tests pin

- `TestCatalogOnMeasuredNexusAnswers` (`imagesearch`): the real `CatalogSearcher`
  and `Classify` on the measured catalogs, for hosted, proxy and group, as admin,
  pull and anon; the prefix filter on a real unprefixed catalog gives the
  `refused` verdict (`ErrPrefixNotInCatalog`). `TestCatalogOnTheHTTPPortRootIsRefused`
  (the HTTP-port root answers 404 to `?n=1000`, `catalog-hosted-root-30481-n1000--*`)
  and `TestCatalogOfAGroupUnderItsPrefixIsRefused` (the group's path, 404 as well,
  `catalog-group-path-n1000--*`) give `refused` with a registry `StatusError{404}`
  (DEVDESK-580, no `Skip` left).
- `TestDetectGroupOnMeasuredNexusAnswers` and
  `TestMeasuredRESTAnswersHoldThePremises` (`registrymgr`): `DetectGroup` on the
  measured REST answers. As the pull account, the detailed group endpoint answers
  **404, not 403** (contradiction C1 of the plan; the anonymous call gets 403).
  Since DEVDESK-578 (NX-07b) this is a members refusal (`Status: 404`, shown as
  `denied`), not a failed discovery, and the test pins that on the recorded
  answers. Nexus sends the same body for a group that does not exist, see
  `registries.md`.
- The data of the prefix tests of `imagesearch` (`catalog_http_test.go`,
  `catalog_test.go`) is now neutral (`myproject`): Nexus 3.98.1 does not prefix
  catalog names, and a comment says so. The synthetic Nexus of
  `internal/ui/oci_resources/browser_catalog_share_test.go` (one `_catalog` with
  prefixed names for three members) was marked as contradicted by the measures;
  DEVDESK-579 (NX-12) settled how a member's catalog is reached (its path, see
  `network.md`), and the catalog tests of `imagesearch` now also replay the 12
  path-catalog fixtures (`catalog_fixture_test.go`).

### The stale-fixture test

`TestFixturesMatchTheBenchVersion` (`stale_test.go`) compares the `nexus:` header
of every fixture with `nexusVersion` and `edition` of
`internal/nexusbench/descriptor.json` (the versioned descriptor `up` writes from
the instance's answer; it is not `testdata/descriptor.example.json`, a golden whose
version is a constant). It decodes three fields and imports nothing from
`nexusbench`. A mismatch fails `mise run test` with the count per campaign, the two
versions and the command `mise run nexus-bench:record`: bumping the Nexus image
forces the fixtures to be regenerated in the same PR. An unreadable descriptor or
a schema other than 2 fails with the path.

A fixture marked `record: skip`, or one with several exchanges and no `flow`
header, is compared like the others: the recorder skips it but the stale test does
not exempt it, so after a bump it stays red until someone edits its header by hand.
That is why it stays the exception, each one justified in its header. The
multi-exchange fixtures of a flow (`flow: bearer`, `flow: redirects`) are not in that
case: `nexus-bench record` regenerates them (DEVDESK-580, PR2).

### `nexus-bench record` / `mise run nexus-bench:record`

Regenerates the fixtures from the running bench. The request to replay is read
from the fixture itself (method, target, `Host`, request headers, identity from the
file name), so a new fixture is a request-only file with `# record: pending`.

Order: parameters, then the descriptor of this bench (schema 2, same context and
namespace; missing means `mise run nexus-bench:up`), then the deny list, then the
secrets store (admin password required; passwords come from the store only, no
flag carries one), then, with `--state` only, the state of the bench, then
`RecordFixtures`. Without `--state` there is no kubectl step and no state filter:
the run behaves as it did before the states existed (`RecordOptions.State` is
`StateAny`). It therefore records every fixture it selects, `state: upstream-cut`
ones included, and can overwrite such a fixture with a nominal answer: always pass
`--state` for a campaign that mixes states. The descriptor is not written.

Flags: the common parameter flags, plus `--campaign <name>` (repeatable, default
every folder), `--only <glob>` (on fixture names without `.http`, e.g.
`catalog-*`), `--dir <folder>` (default the fixtures folder of the source tree),
`--rest-host`, which accepts a loopback address only, and four that only `record`
takes (the other subcommands refuse them with exit 2):

- `--state nominal|upstream-cut`: the state the bench is in. The command builds the
  kubectl runner, runs the preflight `OpRecord` (get the Deployment, list the pods,
  nothing written, no node, no tunnel), reads the upstream (`ReadUpstream`, read
  only) and compares: `nominal` wants the upstream ready, `upstream-cut` wants it
  cut, anything else is exit 1 with a message that names the declared state and
  what to run (`restore-upstream`, `cut-upstream`, `up`), before any request. The
  library then records only the fixtures whose `state` header starts with that
  state (`nominal` also takes a file with no `state` header); the others are
  reported `skipped` and never requested, the file untouched. The fixtures that are
  sensitive to the order of the requests carry a `cut1-`, `cut2-` prefix: the files
  of a campaign are sent in the lexical order of their names, and `--only` splits a
  state in separate runs. An unknown value is a usage error (exit 2).
- `--allow-cache-effects`: lets a fixture that says `effect: caches` be sent (see
  the guards). It reaches for no cluster by itself.
- `--dataset-as-admin` (boolean, off by default, DEVDESK-580 PR4): compares the data
  set of the bench with the listing the admin account reads instead of the anonymous
  one (`RecordOptions.DatasetAsAdmin`). It exists for a bench whose anonymous access is
  cut (scenario W6), where the anonymous listing is a 401 and the run would be refused.
  With it the two listings (`docker-hosted` and `docker-proxy`, never `docker-group`)
  are sent with the stored admin credentials to the REST origin and never anonymously,
  even when anonymous access is still on (it is not a fallback); the comparison, the
  refusal on a drift and its advice (`reset`) are the same. The admin password is
  checked before the first request (a run that has jobs and none stored is refused
  with `mise run nexus-bench:up`), a refused listing is a refusal whose advice does
  not blame anonymous access, and no password is in an error. A run with nothing to
  record reads no listing. Without the flag nothing changes. It reaches for no cluster.
- `--request-timeout <duration>` (default `15s`): what one request may take,
  connection included, per request and not for the run. The upstream's first read
  takes 20 s when it is cut (NX-02b), so a run in that state asks for `45s`. A
  negative value is refused before anything is read.

Guards. All are checked before the first request, except where stated:

- origins are loopback URLs and loopback `host:port` pairs of the descriptor; the
  REST origin is required;
- GET and the HEAD of a manifest only, and a closed list of paths:
  `/v2/_catalog`, `/v2/<name>/tags/list`,
  `/v2/<repo>[/<repo>...]/_catalog` (the catalog of a member by path, DEVDESK-579),
  `/repository/<repo>/v2/_catalog`, `/service/rest/v1/repositories[/...]`, and four
  shapes of the `access` campaign (DEVDESK-580, PR4), GET only, `<repo>` being one of the
  three repositories of the bench (`docker-hosted`, `docker-proxy`, `docker-group`,
  the set the HEAD of a manifest knows): `/v2` (no trailing slash),
  `/repository/<repo>/v2` (no trailing slash), `/v2/<repo>` (exactly the repository,
  nothing after it) and `/repository/<repo>/v2/<name>/tags/list` (the tags of an image
  by the repository path); with a
  query limited to `n` and `last`; never a manifest or a blob (a read through the
  proxy caches and would change the bench). The only percent escape admitted is
  `%2F` (either case) in the value of `last`, the form a Nexus group page `Link`
  gives (`last=shared%2Fbase`, DEVDESK-580); any other escape, one in the path or in
  `n`, and a literal `..` stay refused (`recordQueryParam`, `refusedRequest`). The
  extensions of DEVDESK-580 are `record_request.go`'s (`refusedFixtureRequest`):
  - the HEAD of a manifest of `docker-hosted` on the HTTP port
    (`/repository/docker-hosted/v2/<name>/manifests/<ref>`, or the path routing
    `/v2/docker-hosted/...`) is sent as any GET is. Through `docker-proxy`,
    `docker-group` or a Docker connector it can cache an image in the proxy: it is
    sent only when the fixture says `effect: caches` **and** the run says
    `--allow-cache-effects` (each refusal names what is missing), and the report
    lists the fixture under `CacheEffects`. A HEAD with a query, an escape, a
    dot-dot, an upper-case name or a repository that is none of the bench's is
    refused whatever the flags. A HEAD answer has no body, it is written
    `<empty body>`, with its `Docker-Content-Digest` and `Content-Length`;
  - the GET of a blob of `docker-hosted` (`.../blobs/sha256:<64 hex>`, HTTP port),
    only for a `flow: redirects` fixture; its body is summarized, never kept. A GET
    of a manifest, and a blob anywhere else, stay refused;
  - the token paths (`/v2/token`, `/repository/<name>/v2/token`) are in no list: a
    token request is built by a `bearer` flow from the challenge it follows, and a
    fixture that asks for one itself is refused;
- a fixture file or campaign folder that is a symbolic link is refused; the file
  as it stands goes through `CheckLeaks` before it is sent;
- a redirection is followed only by a `flow: redirects` fixture, a Bearer
  challenge only by a `flow: bearer` fixture; elsewhere a 302 or a 401 is recorded as
  such. A body is read up to 1 MiB, and only the fixture's request headers are sent.
  The flows (`record_flow.go`):
  - `bearer`: the first exchange is sent as DevDesk sends it (Basic for `admin`,
    `pull` and `badcred`, nothing for `anon`). On a 401 with a `WWW-Authenticate:
    Bearer` challenge, the realm is followed only when its `host:port` is a key of
    the run's origins (`RecordOptions.Origins`) **and** its path is a token path;
    the token is then asked for with the identity's credentials and the `service`
    and `scope` of the challenge, and the first request is sent again with `Bearer
    <token>`: three exchanges (`Authorization` written `Basic <redacted>`, `Basic
    <redacted>`, `Bearer <redacted>`; the token body `{"token":"<redacted>",...}`).
    Any other realm ends the flow with the 401 alone and the note `realm off-origin,
    not followed` (its `host:port` is no origin of the run); a realm on an origin of
    the run whose path is no token path, or whose scheme or user information is
    refused, gets the note `realm not followed`. Nothing is sent to it, which is
    what the measurement of a refusal is. A 401 of the token request ends it too
    (two exchanges); no 401 gives one exchange and the note `no challenge`. The token
    read is added to the forbidden terms of the run before any check, so that no
    file, error or report can hold it: an answer that carries it anywhere but in
    the token answer itself is refused;
  - `redirects`: a 3xx whose `Location` (a relative one is resolved against the
    request that was redirected) is on the same origin as the request of the hop (its
    `host:port` equals that request's `Host`) and a request the closed list allows is
    followed with the same credentials, up to 5 hops; every hop is an exchange. Any
    other `Location`, another origin of the descriptor included, is recorded and not
    followed (note `off-origin redirect, not followed`, or `redirect to a request
    outside the closed list, not followed` for the same origin); a chain past 5 hops is an error and nothing is
    written;
  - the `badcred` identity sends `admin` with `recordBadPassword`
    (`fixture-wrong-password`, the recorder's own constant, equal to
    `fakenexus.BadPassword`, which imports `testing`); it needs no stored password and
    the file says `Basic <redacted>`;
- the data set is compared **anonymously**, inside the library
  (`ListImagesAnonymously`, run by `RecordFixtures` itself, not by the command), and
  anything but a match is refused with `mise run nexus-bench:reset`. Consequence: a
  bench whose anonymous access is turned off cannot be recorded;
- the `Server:` header of each answer must name the descriptor's version and
  edition, else it fails (`up`);
- all or nothing: every answer is redacted and passed through `CheckLeaks` (with
  both passwords, their Basic encodings, and the deny list) first; any leak or
  error writes nothing. Then files are written atomically (`0644`);
- answers equal to the fixture, apart from `measured:`, leave the file alone
  (`unchanged`, bytes and modification time). `sameAnswer` compares every exchange of
  the file, so a change in the token step or in the answer to the retry is a new
  answer.

A fixture that says `record: skip`, that is of another state than the run, or that
holds more than one exchange and names no flow, is reported as skipped (with its
reason) and never requested; a run whose fixtures are all skipped asks nothing of the
bench, not even the data set. The report lists each fixture (`written`, `unchanged`,
`skipped`) and one line per campaign, `record: catalog: N written, M unchanged, K
skipped`. When fixtures were sent with a cache effect, the output names each
(`cache effect  <campaign>/<name>`) and asks for `mise run nexus-bench:reset`; the
report keeps `CacheEffects` even when the run fails after sending one, and the
command then asks for the reset after the error. Errors are one line, free of secrets
(`BenchError`). No output holds a password or a token (`requireClean` in the tests).

Written files carry `# measured:` with today's date and `# recorded-by: nexus-bench
record`; they then stop being byte-identical to the evidence, which the provenance
test accepts.

### The access bricks and the guarded scenario (`access.go`, DEVDESK-580, PR4, W5 and W6)

Two settings of the bench have to change for the fixtures of the `access` campaign that
need them, and the bench must then be put back. Three functions of `access.go` do it,
for the scenario test only (bench tools, not DevDesk features):

- `ActiveRealms(ctx, baseURL, store, p) ([]string, error)` reads the active realms in the
  order the instance lists them (one GET).
- `SetActiveRealms(ctx, baseURL, store, p, realms) error` replaces them with the list as
  given, in its order, in one PUT. A list that would lock the bench out is refused before
  any request: none, one without `NexusAuthenticatingRealm`, or one with a blank name
  (Provision, which puts the bench back, signs in as admin).
- `SetAnonymousAccess(ctx, baseURL, store, p, enabled) error` enables or disables anonymous
  access with one read (to keep the realm) and one PUT of the same object `ensureAnonymous`
  sends (`enabled`, `userId`, `realmName`, shared in `client.setAnonymous`).

Guards, as for `RestoreUpstream`: a REST origin that is not the loopback is refused before
the credentials are read (`requireLoopbackBase`); the stored admin credentials are the only
ones sent (none stored: a refusal that names `mise run nexus-bench:up`, nothing sent); a
cancelled context is returned as is; every other error is a `*BenchError` that carries no
password; no write but the one PUT. Provision undoes both (it adds the required realms back
and sets anonymous access to what the parameters say), so `mise run nexus-bench:provision`
is the way back.

`TestIntegrationDevDeskAccessVariants` (`bench_scenarios_integration_test.go`) uses them. It
needs `DEVDESK_NEXUS_BENCH=1` and `DEVDESK_NEXUS_BENCH_SCENARIOS=1` (skipped otherwise) and
the environment of the other scenarios. It checks that the bench is nominal (data set,
required realms, anonymous access), then: W5 removes `DockerToken` from the active realms,
calls `RecordFixtures` in-process on the `access` fixtures named `w5-*` (`StateAny`), and
runs Provision; W6 cuts anonymous access, records the `w6-*` fixtures with `DatasetAsAdmin`,
and runs Provision; it ends by checking that the data set, the realms (as they were, in
their order) and anonymous access are nominal again. The Provision of the way back is
registered with `t.Cleanup` before the first write and runs again whatever the outcome. It
makes no other write (no W7, no W8). It writes the fixtures it records under the source
tree, like `nexus-bench record`; a campaign without a `w5-` or `w6-` fixture fails the run
rather than passing for a recording.

### What is not recorded yet

- The group line (now `May omit images`, DEVDESK-629 F2) and the verdict `failed` are measured since DEVDESK-580 (PR3): the 502 of a
  cut upstream and the 404 of an auto-blocked proxy are fixtures of the `cachedonly`
  campaign, replayed by the tests above. What stays on hand-written answers is every
  other failure (a 5xx or a 429 on a hosted member, an unreadable body, a network error)
  and the deadline the user really waits for (15 s in the client, 20 s in Nexus), which
  no fixture can replay (the abandon is typed since DEVDESK-633, `*oci.NoAnswerError`, text
  `no answer within 15 s`; the measures are unchanged). A proxy's catalog lists `mirror/uncached`, which it does not
  hold (it relays the upstream); the search shows it.
- NX-05 is recorded and replayed since DEVDESK-580 (PR4): the `access` campaign above. What stays unmeasured
  is listed at the end of that section (subdomain, a connector for `docker-hosted`, an off-origin realm or
  redirect, the token endpoint under W6). `tags-hosted-api-repository` is no longer postponed: the
  `/repository/<name>/v2/<image>/tags/list` shape is in the recorder's closed list and recorded as
  `routing-tags-hosted-repository`.
- The `?n=1000` request on the HTTP-port root is recorded (DEVDESK-580, PR1).
- Both bench members of the group get the alias `docker` (`docker-hosted`,
  `docker-proxy`): same label, display only. Flagged, not changed.

NX-04 and NX-05 are meant to add files (pending fixtures, then `record`), not code,
unless they need a new request shape.

## Still to come

The path that uses this lifecycle (NX-03 onward). This file grows with each step.
