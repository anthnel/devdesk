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
9. `docker-hosted`, `docker-proxy` (`remoteUrl` = `Params.UpstreamURL`,
   `indexType: REGISTRY`) and `docker-group` (members hosted then proxy,
   `docker.httpPort: 8082`), all on blob store `default`, `forceBasicAuth: false`
   (anonymous pulls): `GET .../{name}` 404 then `POST`, found then `PUT`.
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
  statuses of `POST` (201) and `PUT` (204), the path-routing field added in 3.83
  (left to the default of Nexus), and a proxy created before its upstream exists.

Verdict of the first real run (details in "First real run"): every item above
held except the last one. The `Server` header, the EULA payload, `text/plain`
change-password, the realm ids, the anonymous, role, user and repository
payloads and all the statuses matched the swagger and the fake. A proxy created
before its upstream resolves is refused with `400`; the fake does the same since
(`BlockUpstream`, `ResolveUpstream`).

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
- **Partial result on error.** `Provision` returns the `ProvisionResult` it had
  gathered together with the error. The command does not write the descriptor
  when `err != nil` (`TestDescriptorIsNotWrittenUnlessProvisionSucceeded`), or it
  would describe a bench that is half provisioned.

## The command and the mise tasks (`cmd/nexus-bench`, `mise.toml`)

`internal/nexusbench/cmd/nexus-bench` is a `package main` of five files:
`main.go` (`deps`, `defaultDeps`, `run`), `flags.go` (flags and parameter
resolution), `commands.go` (`up`, `provision`, `down`, `render`), `status.go` and
`usage.go` (the help). `main()` only builds a context cancelled by SIGINT and
SIGTERM, calls `run(ctx, args, os.Getenv, os.Stdout, os.Stderr, defaultDeps())`
and exits with its code. `deps` holds every constructor and library call that
reaches a cluster (`newRunner`, `newProber`, `clock`, `newStore`, `newInitial`,
`preflight`, `deploy`, `provision`, `destroy`); `defaultDeps` wires
`NewExecRunner`, `NewProber`, `NewClock`, `NewFileStore`,
`NewKubectlInitialPassword`, `RunPreflight`, `Deploy`, `Provision` and `Destroy`,
and the tests wire fakes, so no test needs kubectl or a network.

**Tasks.** Six, in `mise.toml`, none reachable from `ci`, `check`, `test` or
`test-race` (`mise_tasks_test.go` reads the file and checks it, and checks that
`kubectl` is declared on each of them, not in the global `[tools]`, so the CI does
not install it; kubectl `1.35`, because `datomir` runs k0s v1.36.x and kubectl is
supported within one minor version of the API server, so 1.35 is the oldest
accepted; `render` contacts no cluster and declares no kubectl, and the test
requires its absence there). Arguments follow `--`: mise appends them to the
command line.

| Task | Runs | Acts on the cluster |
|---|---|---|
| `nexus-bench:up` | `go run ./internal/nexusbench/cmd/nexus-bench up` | yes |
| `nexus-bench:provision` | `... provision` | yes (REST and `exec`) |
| `nexus-bench:down` | `... down` | yes (deletes the namespace) |
| `nexus-bench:status` | `... status` | reads only |
| `nexus-bench:render` | `... render` | no, contacts nothing |
| `nexus-bench:test` | `go test -timeout 60m -count=1 -run Integration ./internal/nexusbench/...` with `DEVDESK_NEXUS_BENCH=1` set by the task's `env` table (portable, no `env` command) | yes; the tests skip when `CI` is set |

**Subcommands and flags.** Every subcommand takes the same parameter flags, one
per `NEXUS_BENCH_*` variable: `--context`, `--namespace`, `--http-nodeport`,
`--docker-nodeport`, `--cpu-request`, `--cpu-limit`, `--memory-request`,
`--memory-limit`, `--storage-size`, `--jvm-opts`, `--image`, `--image-tag`,
`--upstream-url`, `--anonymous`, `--hosts`, `--timeout`, `--delete-timeout`,
`--secrets`, `--descriptor`. `up` and `provision` add `--accept-eula` and
`--rest-host`. Each subcommand has its own `flag.FlagSet` (`ContinueOnError`,
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
`NEXUS_BENCH_SECRETS`), `newRunner`, `newStore` (the git guard), `Deploy` (which
runs `RunPreflight(OpUp)` itself, so the command does not run a second one), then
`Provision`, then the descriptor. **The descriptor is written only after a
successful `Provision`**, never on a failure at any step, and a descriptor from
an earlier run is left as it was. It is built with `BuildDescriptor(p,
DeployResult, ProvisionResult)`.

**`provision`.** `RunPreflight(OpProvision)`, `Provision`, then the descriptor,
built with `DeployResult{NodeIP: <node address of the preflight>}`.

**`down`.** `Destroy` (its own `RunPreflight(OpDown)` first). The descriptor is
not touched or created.

**`status`.** Read-only: `RunPreflight(OpStatus)` (a missing namespace is its
`NamespaceMissing` error, exit `1`), one `kubectl get pods -l
app.kubernetes.io/name=nexus -o json`, one probe of the REST origin, one read of
the descriptor. Four lines, each a finding, exit `0`:

    bench: namespace nexus-bench, context datomir
    pod: running (nexus-xxx, 1/1 ready, 0 restarts)     | not found | pending ...
    rest: ready (http://127.0.0.1:30481)                 | not ready (the cause is not shown)
    descriptor: matches (<path>) | not found (<path>) | differs from the current parameters (<path>) + one line per field

The comparison covers context, namespace, image, both NodePorts, the upstream URL
and the anonymous flag; hosts and versions come from the cluster and are left out.
It creates no store, builds no `InitialPassword` and sends no credential.

**`render`.** Prints `Render(p)` and nothing else; it builds no dependency (the
test runs it with an empty `deps`).

**Where the credentials go.** `Provision` is given
`RESTBaseURL("127.0.0.1", p)`, whatever hosts `Deploy` reached, so the admin and
`bench-pull` passwords stay on the loopback by default. `--rest-host <host>`
(a host name, an IPv4 or IPv6 address, brackets and a zone allowed; no port,
scheme, path, credentials or space, refused before any cluster call with exit
`1`) sends them to that host instead, and unless it is a loopback address or
`localhost` the command prints on stderr a warning that the credentials travel in
clear text. `status` and the deployment wait probe without credentials.

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
   `NewExecRunner`, `DescriptorPath`, `DefaultSecretsPath` or `newClient` (unless
   `newClient` is given the address of an `httptest` server) before the guard, or
   in a function without it, fails. Exceptions are named in
   `requireBenchExceptions`, with a `check` that verifies the reason in the
   source (the PATH is isolated, the environment is a map); an exception that
   matches nothing fails too (`TestGuardExceptionsStillMatch`). Limit: calls are
   read by name in the test files of this folder; a call hidden behind a function
   value, or made by the tests of `cmd/nexus-bench` (which run on fakes), is not
   seen.
2. *Only `kubectl.go` and `secrets.go` start a process* (`exec.Command` or
   `CommandContext`, any import name, dot import refused): one door to the
   cluster, one to `git check-ignore`. A listed file that no longer starts one
   must be dropped from the list. Limit: `syscall.Exec`, `os.StartProcess` and a
   process started by another package are not seen.
3. *No Authorization header outside the client.* The word `authorization` (in an
   identifier or a string) may appear only in `nexusapi.go` and `probe.go`,
   `SetBasicAuth` only in `nexusapi.go`, and `net/http/httputil` is imported
   nowhere. Comments are not read. Limit: a header name built by concatenation,
   or a whole `http.Header` printed with `%v`, is not seen.

**The mise guard** (`mise_tasks_test.go`) reads `mise.toml` with a small
line-based reader (no TOML library in `go.mod`) and checks: the six tasks exist
and call the command; none of `ci`, `check`, `test`, `test-race` reaches a
`nexus-bench` task, directly or through `depends` and `run` references; `kubectl`
is declared on `up`, `provision`, `down`, `status` and `test`, absent on `render`
and absent from the global `[tools]`; `nexus-bench:test` sets
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

## Still to come

NX-02 deploys the upstream (`bench-upstream` in the bench namespace); after it,
`mise run nexus-bench:up` creates the proxy, puts it into the group, and the
descriptor gets the proxy back (CA2 for the proxy; commit the new `descriptor.json`). This file grows with each step.
