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
runner, errors and preflight (step 3), the deployment and its wait (step 4), the
secrets store and the descriptor (step 5), the REST client and provisioning
(step 6) and the destruction (step 7) exist. The preflight only reads the
cluster; the writes that exist are the two applies of `Deploy` and the namespace
delete of `Destroy`, which have only ever run against a fake `Runner`, and
`Provision` has only ever run against an in-memory fake (see H-REST below).

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
   `up` needs create and patch `namespaces` (cluster; a server-side apply is a
   PATCH, also on a second run), create `persistentvolumeclaims`, `services`,
   `deployments` and `pods/exec` (namespace), list `services` (`-A`),
   patch `deployments`, `services` and `persistentvolumeclaims`, and get and list
   `pods` and `persistentvolumeclaims` (the wait of `Deploy`), all in the
   namespace (no manifest creates a Secret, so none is asked);
   `provision` needs create `pods/exec`, get `deployments` and list `pods`
   (namespace); `down` needs delete `namespaces`; `status`
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
  outside `kubectl.go`; it lives in the package variable `gitIgnores`, so it can
  be replaced and found by a source guard.

**Descriptor.** `BuildDescriptor(p, DeployResult, ProvisionResult)` returns the
`Descriptor` of schema 1 (D-D): `schemaVersion`, `kubeContext`, `namespace`,
`image`, `nexusVersion`, `edition`, `eulaAccepted`, `hosts`, `rest`,
`dockerConnectors`, `repositories`, `realms`, `accounts`, in that order, which is the
order of the struct fields. `hosts` is `Params.Hosts` when set, else `127.0.0.1`
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

## REST client and provisioning (`nexusapi.go`, `provision.go`, `initialpassword.go`)

`Provision(ctx, baseURL, ip, store, p, out) (ProvisionResult, error)` brings a
running Nexus to the state the bench needs (D-E). `baseURL` is the REST origin
(no path); `ip` is an `InitialPassword` (`Read(ctx) (pw, ok, err)`); `out` gets
progress lines (`provision: ...`) and never a password.

**Idempotence.** Every object is read, then created when absent or replaced when
present; there is no "create and ignore the error". A second run sends no `POST`
and changes neither the state nor any password. Order of the calls:

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
9. `docker-hosted`, `docker-proxy` (`remoteUrl` = `Params.UpstreamURL`,
   `indexType: REGISTRY`) and `docker-group` (members hosted then proxy,
   `docker.httpPort: 8082`), all on blob store `default`, `forceBasicAuth: false`
   (anonymous pulls): `GET .../{name}` 404 then `POST`, found then `PUT`.

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
  statuses of `POST` (201) and `PUT` (204), the path-routing field added in 3.83
  (left to the default of Nexus), and a proxy created before its upstream exists.

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
   `get pvc -n <ns> -o json`, diagnose them, then probe every host. The deadline
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
no field of its own conflicts on a second run are for step 10.

## Residual risks (security audit of the steps 4, 6, 7)

Known and accepted for a throw-away bench; none is fixed by this change.

- **Basic credentials in clear HTTP.** Nexus is reached on `http://`, so the admin
  and `bench-pull` credentials of `Provision` travel unencrypted. The command of
  step 8 must prefer `127.0.0.1` (`RESTBaseURL("127.0.0.1", p)`) and send
  credentials to the node IP only on an explicit option of the operator. This is
  a commitment for step 8, not something the package enforces.
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
- **Partial result on error.** `Provision` returns the `ProvisionResult` it had
  gathered together with the error. The command must not write the descriptor
  when `err != nil`, or it would describe a bench that is half provisioned.

## Still to come

The `nexus-bench` command and mise tasks (8), guarded integration
tests and source guards (9), and the first real run on `datomir` (10, blocked on
the EULA and cluster-access decisions Q1 and Q2 of the plan). This file grows
with each step.
