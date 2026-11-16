# The Nexus bench — `internal/nexusbench`

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
runner, errors and preflight (step 3), and the secrets store and the descriptor
(step 5) exist. The preflight only reads the cluster; nothing writes to it yet.

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
- Paths hold no control character and are 4096 bytes at most.

`NEXUS_BENCH_NAMESPACE` alone moves `UpstreamURL` to the new namespace, unless
the base already carried a custom upstream or `NEXUS_BENCH_UPSTREAM_URL` is set.
The context name is limited to letters, digits, `.`, `_`, `@` and `-`, because
it names a directory of the secrets path.

## Manifests (`render.go`, `manifests/`)

`Render(p Params) ([]byte, error)` calls `p.Validate()` first (an invalid value
returns Validate's error, naming its variable, and no bytes) and writes a
multi-document YAML stream separated by `---`, in this order: Namespace, PVC,
Deployment, Service. `RenderNamespace(p)` returns the first document alone, so
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

Quoting: the templates see only a `manifestData` built from validated values,
and every string goes through the `q` template function (`strconv.Quote`, a
valid YAML double-quoted scalar), so `true`, `null`, `123`, `0x1F`, `12:30:45` or
`2001-01-23` in `NEXUS_BENCH_JVM_OPTS` decode as strings (`TestRenderRoundTripsTrickyJVMOpts`,
`TestRenderQuotesEveryValue`). Quoted: namespace, image reference, JVM options,
CPU and memory quantities, storage size. Not quoted: the two NodePorts (integers),
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
never passes for a named kind. The last six are for the steps to come.

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
`kubectl.go` is the only non-test file of the package that starts `kubectl`; the one other process is the `git check-ignore` of the secrets store (see below).

**Preflight.** `RunPreflight(ctx, r, p, op) (PreflightResult, error)` with `op` one
of `OpUp`, `OpProvision`, `OpDown`, `OpStatus`. Checks, in this order, stopping at
the first failure:

1. `p.Validate()`: invalid parameters are refused before any call.
2. `kubectl config get-contexts -o name`: the exact context name is listed, else
   `ContextNotFound`. Nothing else is asked.
3. `kubectl get --raw /readyz`: the API server answers, else `ClusterUnreachable`;
   a refusal (stderr `forbidden`: expired token, 403) is `Forbidden` instead, through
   the same `classifyKubectl` as every other call.
4. `kubectl auth can-i <verb> <resource>` (`-n <ns>`, `-A` or nothing):
   `up` needs create `namespaces` (cluster), `persistentvolumeclaims`, `services`,
   `deployments`, `secrets` and `pods/exec` (namespace) and list `services` (`-A`);
   `provision` needs create `pods/exec`; `down` needs delete `namespaces`; `status`
   asks nothing. A `no` arrives on stdout with a non-zero exit, so stdout is read
   first: `Forbidden`, naming the verb and the resource.
5. `kubectl get namespace <ns> -o json`: absent is fine for `up` and `down` (nothing
   to delete) and `NamespaceMissing` for `provision` and `status`; present
   without the label `app.kubernetes.io/managed-by=devdesk-nexus-bench` is
   `NamespaceForeign` for every operation (the plan lists `up` and `down`;
   `provision` and `status` refuse too, so the bench never runs `exec` in a
   namespace it did not create).
6. `up` only, `kubectl get services -A -o json`: a Service other than `nexus` of
   our namespace that holds `HTTPNodePort` or `DockerNodePort` is `NodePortTaken`,
   naming `<namespace>/<name>` and the port. Our own Service is accepted, which is
   what makes a second `up` possible.
7. Everything but `down`, `kubectl get nodes -o json`: `NodeIP` is the first
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
  `GIT_OBJECT_DIRECTORY` and `GIT_NAMESPACE` (`gitEnvironment`), so the verdict is
  about the repository of the path, not one the caller's variables point at. That call is the only process
  outside `kubectl.go`; it lives in the package variable `gitIgnores`, so it can
  be replaced and found by a source guard.

**Descriptor.** `BuildDescriptor(p, DeployResult, ProvisionResult)` returns the
`Descriptor` of schema 1 (D-D): `schemaVersion`, `kubeContext`, `namespace`,
`image`, `nexusVersion`, `edition`, `eulaAccepted`, `hosts`, `rest`,
`dockerConnectors`, `repositories`, `realms`, `accounts`, in that order, which is the
order of the struct fields. `hosts` is `Params.Hosts` when set, else `127.0.0.1`
then `DeployResult.NodeIP` when it is not empty; the URLs are `http://<host>:<nodePort>`
for each host. `DeployResult{ReachableHosts, NodeIP}` (step 4) and
`ProvisionResult{NexusVersion, Edition, EULAAccepted}` (step 6) are defined in
`descriptor.go` and reused by those steps. The account `anonymous` always
appears; its `enabled` is a `*bool`, so `false` is written when
`NEXUS_BENCH_ANONYMOUS=false`, and the other accounts write no `enabled`.

- `WriteDescriptor(path, d)`: JSON indented by two spaces, one final newline, `0644`
  file, atomic through the same helper; the bytes depend on `d` alone.
- `LoadDescriptor(path)`: a missing file is `ErrNoDescriptor` (wrapped, use
  `errors.Is`); invalid JSON or a `schemaVersion` other than 1 (missing counts as 0)
  is an error that names the file and never shows its content.
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
diff by hand (bump `schemaVersion` when a field changes meaning).

## Still to come

Deploy and wait (4), the REST client and provisioning (6),
destroy (7), the `nexus-bench` command and mise tasks (8), guarded integration
tests and source guards (9), and the first real run on `datomir` (10, blocked on
the EULA and cluster-access decisions Q1 and Q2 of the plan). This file grows
with each step.
