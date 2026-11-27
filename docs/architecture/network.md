# Container engine, OCI registries, network diagnostics, sockets and interfaces

> DevDesk architecture notes. Referenced from `.claude/CLAUDE.md`;
> read this file when working on the code it describes.

## The container engine — `internal/engine` and `internal/docker`

**`internal/engine`** names the engine in use — docker or podman — and carries
everything that is not the same between the two: the binary, the credential
helper prefix, the auth file, the host socket, and the `--format` templates. It
is its own package rather than a field of `internal/docker` because
`internal/scan` needs the same answer and does not import `internal/docker` —
`internal/forge`'s arrangement read one level down (§3.67).

`engine.Resolve(preference)` turns `app.container_engine` into a `Shape`:
`auto` takes docker if it is on PATH and podman otherwise; a pinned engine that
is absent is an **error**, never the other one, because the two do not hold the
same containers. Anything else is taken as a path to a binary. The router
resolves once at startup and once per context switch, into `engine.SetCurrent`.

A `Shape` carries **no resolved filesystem path**: `AuthPaths()`, `AuthPath()`
and `HostSocket()` are methods. The shape is built at package initialisation, so
a `$HOME` read there would be frozen before anything else runs — and whether the
socket exists is the whole question, since `podman system service` can start or
stop while the application is up.

**`internal/docker`** drives that engine. Every invocation goes through one
seam — `dockerRunner`, implemented by `cliRunner` (`exec.go`) — which is what
made podman cheap: the binary name lived in exactly one place. The package is
split by resource: `containers.go`, `images.go`, `networks.go`, `volumes.go`,
`registry.go`, `system.go`, `launch.go`, plus `exec.go` (the seam), `parse.go`,
`container_ports.go`, `identifier.go` and `names.go`. It is **not** called
`internal/container`: renaming it would touch 40 files for a vocabulary gain.

Two things about the engine are worth knowing before changing this code:

- **The `--format` templates are per engine and identical today.** Podman
  implements `--format`, but equality of the *fields* is guaranteed nowhere, and
  a divergence raises no error — it produces wrong rows. No measurement has been
  taken against a real podman; `TestTheTwoTemplateSetsAreStillUnmeasured` fails
  the day the two sets stop matching, so a divergence is recorded deliberately.
- **`system df -v` is scraped, not templated**, and is therefore disabled under
  podman (`Shape.ParsesSystemDFVerbose`). It is a fixed-width table read at
  offsets taken from its header line, and there is no `--format` that exposes
  per-image unique size.
- **Tests run once per engine, with a strict stub (OC-07, DEVDESK-804).** A test
  of `internal/docker` whose result depends on the engine goes through
  `forEachEngine(t, fn)` (`exec_test.go`): one subtest per `engine.Names()`,
  `useEngine` applied, the original engine restored by a `Cleanup` registered
  before `fn` runs. It pairs with `stubStrict(t, reply{argv, out, err}...)`: a
  `stubRunner` with a `report` is strict, matches the whole argv (so a wrong
  template fails, where the old stub only read the subcommand) and fails the test
  with the argv quoted through `%q` for `Run` and `Build` alike; a reply the
  code never asked for fails it too, at cleanup. Strictness is
  opt-in: a `stubRunner` without `report` keeps its per-subcommand behaviour, so
  tests migrate one by one. `TestForEachEngineCoversEveryConfiguredEngine`
  (`foreach_test.go`) fails when `config.ContainerEngines()` and the engines the
  helper runs diverge; it stands in for the guard of `TestEveryConfiguredEngineHasAShape`
  (`internal/engine`), which cannot see the helper.

- **Four typed, read-only reads for the path trace** (`container_network.go`,
  DEVDESK-330, DEVDESK-331), each with a `context.Context` that kills the
  invocation: `InspectContainerNetworking(ctx, id)`, `ListContainersContext(ctx,
  all)` (`ListContainers(all)` is unchanged and does not use it),
  `EngineOperatingSystem(ctx)` and `InspectNetworkBridge(ctx, networkID)`. They
  run `container inspect`, `ps`, `info` and `network inspect` and nothing else. `InspectContainerNetworking` runs
  `container inspect --format <containerNetworkingFormat> <id>`: the format
  selects `Name`, `HostConfig.NetworkMode`, `State.Running` and, per network,
  `NetworkID` (`EndpointNetwork.NetworkID`, kept only when it passes
  `IsContainerID`, else empty), `IPAddress`, `Gateway` and `GwPriority`, so `Config.Env`, labels and mounts are
  never read into memory. It reads stdout only, and its error carries the
  command label, never the output. The ID must pass `IsContainerID` and is one
  argv element, never a name. The inspect and `info` reads refuse a non-Docker
  engine with `ErrDockerOnly` before any invocation; output over 256 KiB
  (`maxInspectBytes`) is refused. `InspectContainer`, which returns the whole
  `docker inspect` JSON (secrets included) with no context, was not reused for
  that reason. It returns **stdout only** (OC-04, DEVDESK-800): a warning the
  engine writes on stderr never enters the viewer's document; on failure the
  message carries the stderr that `cliRunner` leaves on the `*exec.ExitError`. The `info` format
  (`{{.OperatingSystem}}`) is a docker-only literal and is not in
  `engine.Templates`.
- **Shell in a container (`docker/shell.go`, OC-04, DEVDESK-800).** `DetectShell(id)` runs `<engine> exec <id> which bash` through the runner and answers `/bin/bash`, else `/bin/sh`. Any failure answers `/bin/sh`, and so does no answer within `shellDetectTimeout` (8 s, the call's `dockerCmd.Ctx`): the `exec -it` that follows then reports the real problem in its own terminal. It waits on the daemon, so the containers view calls it only from a `tea.Cmd` (`detectShellCmd`), never from `Update`: `T` sets `shellDetecting` (footer `Opening shell…` with the activity dot, `WantsBlink` true) and returns the Cmd; its `ShellDetectedMsg` clears the flag and `handleShellDetected` starts the shell, in place (`teaexec.Process`) or in a new window (`uiterminal.ForCmd`). A second `T` while one is pending is refused with a footer warning. The answer is routed to the containers view wherever the user is (`App.routeShellDetected`, `docs/architecture/app-shell.md`): if the view is off screen the message arrives with `Abandoned` set, the wait ends and **no shell is started**. `ShellCmd(id, shell)` builds `<engine> exec -it <id> <shell>` through `runner.Build`; its `Args` are the argv given to the terminal emulator.
- `internal/docker/netdiag.go` — **gone**. §3.33 took the five probes that did not need a container, §3.43 the ports table, §3.44 the topology (and with it `docker/topology.go` and the last `--privileged`), §3.47 the route trace and the file itself (the trace came back in the process as `internal/nettrace`, DEVDESK-327). §3.61 removed the OCI connectivity test, and with it the last container this application runs for a diagnostic: `networks.go` now holds only `ListNetworks`, `CreateNetwork`, `RemoveNetwork`, `PruneNetworks` and `InspectNetwork`.

## OCI registries — `internal/oci`

`internal/oci/oci.go` — an OCI **registry** HTTP client: list tags,
download and extract a tar.gz (a repository template is one — `templates.md`). It speaks the distribution spec over `net/http`
and never shells out, so it is engine-agnostic and podman changed nothing in it.

**Typed status errors and context variants (DEVDESK-298, IS-18).** A registry,
token endpoint or registry manager's management API answering with a
non-success status returns `*oci.StatusError{Code, Source, Challenge}`; read it
with `errors.As`. `Source` is `SourceRegistry` (the v2 API), `SourceToken` (the
Bearer token endpoint) or `SourceManagementAPI` (a manager's own REST API, today
Nexus `/service/rest`, DEVDESK-509). `Challenge` is the `Www-Authenticate`
header of a 401 whose challenge is not Bearer. `Error()` keeps the three
historical messages to the character (`registry returned 404`,
`token endpoint returned 403`, `unexpected auth challenge: ...`), so nothing on
screen or in a log changed for them. The management API message is
`management API returned N`: the code only, never the status line, the body, a
header or the URL. It replaces the Nexus client's former `HTTP N`.
Failures that are not a status (unreadable body, missing realm, no token
field) stay plain errors and do not match `errors.As`.

`oci.ManifestDigest` (the `HEAD` on a manifest) is typed too (DEVDESK-631): a
non-200 answer returns `fmt.Errorf("%w for %s:%s", &StatusError{Code, Source:
SourceRegistry}, repo, tag)`, so `errors.As` finds the `*StatusError` and the
text stays `registry returned 404 for repo:tag` to the byte. A 200 without a
`Docker-Content-Digest` header stays a plain error (`registry gave no digest for
repo:tag`). Its callers (`imageupdate`, the signature stage of `scan`) only log
the error and mark the reference failed; none reads the text.

A 404 whose body says the proxy's upstream is blocked is marked (DEVDESK-632):
`registryStatusError(resp)` (`internal/oci/status_body.go`) builds the
`*StatusError` of every non-200 registry answer (tags, catalog, `ManifestDigest`)
and, for a 404 only, reads at most 4 KiB of the body (`maxStatusBody`, a
hypothesis like `maxNexusErrorBody`). When the body is JSON and the `message` of
the first `errors[]` entry starts with `Remote Auto Blocked` (exact, case
sensitive, no leading space), `UpstreamBlocked()` is true. A missing, empty,
plain-text, longer or cut body, another message, another status (502,
403) leaves it false. A body read that ends on a deadline (the client's 15 s
delay, the caller's context) is no answer: `registryStatusError` returns the
`*NoAnswerError` (verdict `failed`, `no answer within N s`) instead of a plain
404; any other read failure (a truncated body, an unexpected EOF) keeps the plain,
unmarked 404. `ManifestDigest` is a `HEAD`, which has no body: a "Remote Auto
Blocked" 404 on a manifest can never be marked `UpstreamBlocked()` and stays a
plain 404 (`refused`). The body never reaches the error: `Error()` is `registry
returned 404: the proxy's upstream is temporarily blocked, try again later`, with
no date and no duration. `imagesearch.Classify` ranks it `failed` (temporary),
not `refused`; the tags footer says `Could not load tags: the proxy's upstream
is temporarily blocked` (`registryFailureReason`) and the journal line carries
`temporarily blocked`. Every other 404 stays `refused` with `registry returned
404`.

The unexported `unexpectedChallenge` field is a deviation from the plan, which
had only `Code`, `Source` and `Challenge`: with those three, a 401 with a
non-Bearer challenge and a 401 returned by the registry after a token exchange
both read `Source == SourceRegistry`, `Code == 401`, and `Error()` could not
give each its own message. The flag tells them apart without widening the
exported surface.

`ListRegistryTagsContext` and the `...Context` variants of `registryGET`,
`registryDo`, `doRequest` and `exchangeBearerToken` take a `context.Context`
(the token endpoint request included) and check `ctx.Err()` at the head of
every page. The old names delegate with `context.Background()`, signatures
unchanged. A deadline or a cancel reaches the caller as an error that
satisfies `errors.Is(err, context.DeadlineExceeded)` or `context.Canceled`,
and never as a `StatusError`.

**The abandon after 15 s is typed (DEVDESK-633).** `oci.RequestTimeout` (15 s,
unchanged) is the delay of `registryHTTPClient` and the value of
`imageSearchDeadline`. A registry that does not answer in time ends the request
with a `*oci.NoAnswerError`, whose text is exactly `no answer within N s`, N read
from the constant (no `15` literal outside the tests). It wraps only
`context.DeadlineExceeded` (`Unwrap`), so `errors.Is` still finds a deadline,
while the URL, userinfo, host, `/v2/` path and query never come back in the
message. It is applied to the errors of `doRequestContext`, of the token
endpoint's `Do` and `readToken`, and of the body reads of `registryGETContext` and
`registryGETBoundedContext` (so tags, catalog, catalog under a prefix, the
manifest digest and the token exchange are covered). `context.Canceled` is not
touched, and a 502 received in time stays a `*StatusError{502}`, `failed`, with no
"no answer". `registryFailureReason` returns the error's own text for it (a constant
by construction), so the footer (Error) says `Image search on <alias>: no answer within 15 s` and `Could not load
tags: no answer within 15 s`, and the journal reads `ERROR [oci_resources] image
search on <alias>: no answer within 15 s`. **Limit:** the client's own delay and the
context's deadline are still one case: both give the same error and the same text,
and `imagesearch.Classify` (DEVDESK-303, IS-12) puts it in the `failed` verdict.
Only a delay that satisfies `errors.Is(err, context.DeadlineExceeded)` becomes a
`NoAnswerError`: a transport-level "TLS handshake timeout" keeps its own text. The
Docker Hub client of `internal/imagesearch/hub.go` is typed too since DEVDESK-667:
see "Why a failure says it at the footer" below.

**Why a failure says it at the footer (DEVDESK-667, S-04b).** The Docker Hub search
(`HubSearcher`) uses `oci.RequestTimeout` as its client delay and wraps a deadline with
`oci.NoAnswer` (the exported form of the internal `noAnswer` conversion, one rule for
both), so its failure is a `*oci.NoAnswerError` like the catalog's, with no URL or
`q=` in the text; any other transport error keeps its `*url.Error`, so `errors.As`
still finds the `*net.OpError` inside. `registryFailureReason(err, access)`
(`internal/ui/oci_resources/registry_failure.go`) turns an error into one constant
sentence, tested in this order, and returns `""` for anything else (so a 500, a
503, a 401/403/404/405 or an unknown error changes nothing):

| Error | Reason |
|---|---|
| `context.Canceled` | none (the user left) |
| `oci.ErrTokenRealmOffOrigin` | `token realm on another host` |
| `*oci.StatusError`, upstream blocked | `the proxy's upstream is temporarily blocked` |
| `*oci.StatusError` 429, registry or token endpoint | by access, below |
| `*oci.NoAnswerError` | `no answer within N s` |
| `*net.OpError` with `Op == "proxyconnect"` | `could not reach the proxy` |
| `*net.DNSError` | `could not resolve the registry host` |
| any other `*net.OpError` | `could not reach the registry` |

The no-answer reading is tested before the net errors: a connection that timed out is
both. The 429 wording follows `registryAccess`, which says how the request identified
itself: `Docker Hub anonymous rate limit reached, try again later`,
`Docker Hub rate limit reached, try again later` (entry with credentials),
`rate limit reached (HTTP 429), try again later` (any other registry). `accessFor`
derives it from the entry's resolved `authMode` (no credential store read in
`Update`); the Hub search is always `accessHubAnonymous`, since it sends no
credentials whatever the entry holds, and a tag listing is `accessHubLogin` only for a
Hub entry whose mode uses credentials. The reason never carries `err.Error()`, a host
or a typed text. The footer is `m.footer.Error` only (Rule 128): `Image search on
<alias>: <reason>` at level 1, `Could not load tags[: <reason>]` for the tags; the
tables stay tables (Rule 139) and nothing is retried.

**Credentials stay on their origin (DEVDESK-410).** Four guards keep the Basic
credentials and the Bearer token from leaving the registry they were meant for:

- **The tag list never follows a `Link: rel="next"` to another origin.**
  `ListRegistryTagsContext` checks each `next` with `sameOrigin` (the catalog
  walker's guard) before the next request. A foreign `next` is an error that
  wraps `ErrTagsNextOffOrigin` (test it with `errors.Is`) and names the
  destination host only. No partial list is returned: a short list could pass
  for the whole one, and `imageupdate` and the security remediation would read
  a false "up to date". The catalog walker behaves otherwise: it returns
  `truncated=true` with the names already read, and no error.
- **`registryHTTPClient` has a `CheckRedirect`, `RefuseOffOriginRedirect`**
  (`internal/oci/guard.go`, exported since DEVDESK-412). A
  redirect is refused when its scheme, host or port differ from the first
  request of the chain (`via[0]`, not only the previous hop), so a chain that
  leaves the origin and comes back is refused too. The error reads
  `refusing redirect to another origin (<host>)` and wraps
  `ErrOffOriginRedirect` (`errors.Is`). It holds neither the target URL, nor
  its path or query: `StripRedirectTarget` strips the `*url.Error` that carries
  them, so a signed URL reaches neither the footer nor the logs. The limit of
  10 redirects (`MaxRedirects`, the `net/http` default) is kept; past it the
  error wraps `ErrTooManyRedirects` (`stopped after 10 redirects`) and no
  longer carries the last target URL either. It applies to everything that uses
  the client: tags, catalog and the token endpoint.
- **The token endpoint's answer is capped at 1 MiB** (`maxTokenResponse`). A
  larger body is an error (`token response over 1048576 bytes`), not a silent
  cut.
- **A Bearer realm on another origin gets no credentials (DEVDESK-629,
  D168).** `exchangeBearerTokenContext` calls `checkRealmOrigin`
  (`internal/oci/realm.go`) right after `checkTokenRealm`: when credentials
  are configured (username and password both set, the condition under which
  Basic is sent) and the realm's scheme, host or port differ from the request
  that received the challenge, the exchange stops before any request to the
  realm with an error wrapping `ErrTokenRealmOffOrigin`
  (`token realm on another host (<host>)`, `errors.Is`), naming the realm's host
  only and not a `StatusError`. Without credentials the anonymous token request
  goes out as before. The comparison is `sameOrigin`'s, exact: `host` and
  `host:443` are two origins (H2). Two exceptions, frozen in `realmExceptions`
  (a test pins the list), one way, https on both sides, host exact with no
  port: Docker Hub (`registry-1.docker.io` to `auth.docker.io`) and GitLab.com
  (`registry.gitlab.com` to `gitlab.com`). A self-hosted GitLab registry whose
  realm is on the GitLab host is refused. The refusal shows at the footer, level
  Error, as `token realm on another host`, in the three interactive flows of the
  registry browser: tags opened from the search (`Could not load tags: token
  realm on another host`), tags opened from the form (`Could not load tags: token realm on
  another host` since DEVDESK-667; it was the bare reason before) and image search (`Image search on <alias>: token realm on
  another host`), through `registryFailureReason`
  (`internal/ui/oci_resources/registry_failure.go`); the search verdict is
  `failed`. The realm's host stays in the journal. The other callers
  (`imageupdate`, signature, remediation) have no footer.

**Behavior change.** Some redirects that were followed are now refused: `http`
to `https` on the same host, `host` to `host:443`, and a host written with a
different case. The comparison is on the exact scheme and `host:port`, and the
guard refuses when in doubt.

**Behavior change (DEVDESK-629).** A registry configured with credentials whose
Bearer realm is on another origin (other than the two exceptions above) used to
receive them as Basic at its token endpoint; it now fails with
`token realm on another host`. Without credentials nothing changes.

**The other registry clients (DEVDESK-412).** The same two guards, plus a few
more, cover the clients DEVDESK-410 left out. The shared code is
`internal/oci/guard.go`, which `internal/registrymgr` imports (`internal/oci`
imports nothing from `internal/`).

- **`guard.go`** exports `MaxRedirects`, `ErrOffOriginRedirect`,
  `ErrTooManyRedirects`, `RefuseOffOriginRedirect` and `StripRedirectTarget`
  (which also unwraps the unexported `errInsecureRedirect` and the blob's local
  refusal), and two helpers. `ReadAllLimited(r, limit)` accepts `limit` bytes
  and fails with an error wrapping `ErrBodyTooLarge` (`response body over
  <limit> bytes`) on the next one: never a silent cut. `RedactURL(raw)` masks
  the password of a URL's userinfo (`xxxxx`) and returns `<invalid URL>` for
  anything that does not parse or has no host, so it never echoes it.
- **`oci.Client` (templates, `oci.go`) has two `http.Client`, both with a 1
  minute timeout for the whole request, redirects and body included**
  (`templateClientTimeout`). `httpClient` (tag list, manifest) uses
  `RefuseOffOriginRedirect`. `blobClient` (the layer blob) follows a redirect to
  another origin on purpose (decision of the user, D156): a registry hands blobs
  to a CDN or an object store. The credentials never go with it
  (`oci_blob.go`, `followBlobRedirect`):
  - at most 10 hops, to `http` or `https` only;
  - never from `https` to `http`, compared with the previous hop
    (`refusing redirect from https to http (<host>)`);
  - once a hop left the origin of the first request, `Authorization` and
    `Referer` are deleted from every following request, one that comes back to
    the origin included. `net/http` alone would copy `Authorization` to the same
    host on another port, across a scheme change and to a subdomain, and it does
    so before `CheckRedirect` runs, hence the explicit deletion;
  - when the registry is not local, a redirect to `localhost`, `*.localhost` or
    a literal loopback, link-local or unspecified (`0.0.0.0`) address is refused
    (`isLocalHost`). A name that resolves to such an address (DNS rebinding) is
    caught at the dial by `Dialer.Control` (`blobDialControl`), which sees the
    address about to be dialed. The `ip:port` of a configured proxy
    (`HTTP_PROXY`, resolved when the proxy is chosen for a request) is allowed
    at the dial, so a local proxy keeps working. A name in `/etc/hosts` or in
    the DNS that points to `127.0.0.1` is not "local" for the literal check:
    only the dial control looks at what it resolves to. A registry is local by
    the same rule (`localhost`, `*.localhost`, a literal address), which turns
    the two checks off.
  - **The digest is always checked, redirected or not.** `parseLayerDigest`
    accepts `sha256:` and 64 lowercase hex digits only, before the URL is
    built (the digest goes into it); anything else, `sha512` included, fails
    with `unsupported layer digest: only sha256 is supported`. The compressed
    stream is hashed while read, the bytes after the gzip stream included
    (`checkBlobDigest`); a mismatch is `ErrBlobDigestMismatch` and no entry is
    returned.
  - **The compressed bytes are capped** at `blobCap(Limits)`:
    `Limits.Bytes + Limits.Files*2048 + 1 MiB`, no cap when `Limits.Bytes` is
    zero. Over it is `ErrArchiveTooLarge`. The decompressed `Limits` do not see
    what the network carries (a directory entry with a large size, PAX headers).
  - Errors name the host of the failing hop, never its path or query
    (`download layer from <host>: <cause>`, `blobFetchError`). The status line
    a server wrote is not repeated, only the code and its standard text.
- **A Bearer `realm` is checked before any request** (`checkTokenRealm`, applied
  by `exchangeBearerTokenContext` itself). A realm that is neither `http` nor
  `https` (a relative one included) is refused. An `http` realm behind an
  `https` registry is refused too, **with or without credentials**: otherwise
  the Basic credentials, or a token request, would go in clear. The error is
  `refusing a token realm that is not a safe http(s) URL for this registry
  (<host>)`: the realm's host only, never its userinfo, path or query
  (`errInsecureTokenRealm`). A missing or unparsable realm passes this check
  and keeps its own errors, which no longer repeat the realm.
- **URLs are redacted in errors and logs.** The error prefix of
  `doRequestContext` and of the `(retry)` request use `RedactURL`; a request
  that cannot be built reports the cause and the redacted URL
  (`buildRequestError`). A registry address in a log goes through
  `oci.RedactRegistryURL` (`guard.go`), which takes an address with or without a
  scheme: without `://` it prefixes `https://`, redacts, then removes the prefix
  so the host name stays readable; an unreadable address gives `<invalid URL>`.
  `browser_bridge.go` (tags and tag-meta logs) and the login and logout logs of
  `registries.go` and `commands.go` use it. In `internal/template/fetch.go`,
  `redactGitURL` masks the source URL in `fetching ...` for `http(s)` URLs (the
  scp form `git@host:path` is kept as written); it is being extended to mask a
  token written as the user name of an `http(s)` URL as well. A password written
  in a registry URL (`https://alice:...@host`) reaches neither the footer nor
  `log.Printf`.
- **The Nexus detector** (`internal/registrymgr/nexus.go`) has the same origin
  guard (`nexusHTTPClient.CheckRedirect = oci.RefuseOffOriginRedirect`, 8 s
  timeout unchanged) and reads each reply through
  `oci.ReadAllLimited(..., maxNexusResponse)`, 1 MiB. The cap is a hypothesis,
  not measured. The anonymous retry of the group members still happens on any
  error but a refused off-origin redirect (see `registries.md`); the first
  attempt decides the outcome, the retry only changes it by succeeding.
  `fetchGroupMembers` no longer swallows its errors (DEVDESK-510): a
  `StatusError` from the management API with 401 or 403, or the 404 Nexus
  answers a pull account (DEVDESK-578), becomes
  `*registrymgr.MembersRefusedError` (`errors.Is(err, ErrMembersRefused)`,
  `Group`, `Status`, `Anonymous`; its text holds the group and the status,
  never a URL or a body; for the 404, `Cause` is the plain
  `*oci.StatusError`), any other error is returned as it is. `nexusGET` reads
  the body of a 404 only, up to 4 KiB (`maxNexusErrorBody`, a hypothesis: the
  measured body is about 50 bytes), and only to check that its JSON `message`
  is `Repository not found`; the body never reaches an error or a log. A 404
  with any other body, or a longer one, stays a plain status error. A reply
  without a `group.memberNames` key (empty body, `null`, `{}`) is an error;
  `memberNames: []` is a group with no member, a non-nil empty list. The format
  segment of the path is escaped (`url.PathEscape`). A non-200
  status is `*oci.StatusError{Source: oci.SourceManagementAPI}`
  (`management API returned 401`). A context error (cancel, deadline) is never
  a `StatusError`: when the context is done, a body cut mid-read reports the
  context's own error. A 200 reply that is not a repository document (empty or
  whitespace-only body, JSON without a `type` such as `{}` or `null`, invalid
  JSON) is an error wrapping `errEmptyReply`, `errNoRepositoryType` or the
  `encoding/json` error, never "not a group": the caller does not cache it
  (D23), so the members last known stay.
- **Each member is typed by one more GET (DEVDESK-636).** After the member names
  are known (either endpoint), `memberTypes` asks
  `/service/rest/v1/repositories/<member>` for each one (the name through
  `url.PathEscape`, `nexusHTTPClient`, the group's credentials or none, at most
  `maxTypedMembers` = 32 members and `typeLookupWorkers` = 8 at once) and keeps
  `type` when it is `hosted`, `proxy` or `group`. A failure (status, deadline,
  redirect refused, invalid JSON, `{}`, `null`, any other value) leaves the
  member's `GroupMember.Type` empty (unknown), is logged at INFO with the server
  values quoted (`%q`, an unrecognized `type` cut to 64 characters first), and
  never fails the discovery or changes the list. No lookup is started once the
  context is done (the loop checks `ctx.Err()` and its wait for a free slot
  listens to `ctx`); the members not reached stay unknown. A
  group refused or not a group asks for no type. Cost: up to 32 more requests per
  `ctrl+r` on a Nexus group, none at start-up or at search time. The measured
  generic answers (`rest-repositories-docker-{hosted,proxy}--*`) carry `type`
  for all three identities.
- **Docker Hub tag dates** (`fetchDockerHubTagsMeta`, `commands.go`) read the
  page through `oci.ReadAllLimited(..., maxHubTagsMetaResponse)`, 4 MiB.
  Measured on 2026-11-16 with `page_size=100`: 530,629 bytes for
  `library/postgres` (the largest), 443,387 for `library/python`, 259,560 for
  `library/node`; the cap is about 7.9 times the largest. A page over it is an
  error wrapping `ErrBodyTooLarge` and no dates are returned (no partial map).
  `ociHTTPClient` is unchanged (15 s timeout, anonymous call, no credentials).

**Behavior change (DEVDESK-412).** A Bearer realm in `http://` behind an
`https` registry is refused, and so is a realm with another scheme. A template
blob redirected from `https` to `http`, or to a loopback or link-local address
from a registry that is not local, is refused. A registry that serves its
layers with a digest other than `sha256` can no longer be used as an OCI
template source. A Nexus URL that redirects to another origin gives no members
(the `http` to `https` redirect of the same host included: enter the URL in
`https`).

**Residual risks.** The accepted ones (private networks not filtered, local
registry named by an alias, proxy behavior, CPU before the digest check, sha256
only, Nexus limits, and the rest) are listed in D156 in `docs/backlog.md`.

**Catalog walk (DEVDESK-306, IS-03).** `ListCatalogContext(ctx, apiURL,
username, password, maxPages, pageSize)` returns `(names, truncated, err)`. It
asks `<apiURL>/v2/_catalog?n=<pageSize>` and follows `Link: rel="next"` for at
most `maxPages` pages (non-positive limits are an error). `truncated` is true
when names were left: the page cap was reached with a `next` link, more than
`maxPages*pageSize` names came back (the list is cut to that), or the `next`
link points to another origin. A name already returned is skipped silently
(first appearance order kept; a Nexus group repeats names across pages, the
measured `n=2` walk, DEVDESK-634), and the `maxPages*pageSize` cap counts
distinct names. Each page body is capped at 1 MiB; a larger
page is an error, not a silent cut. A non-200 answer is a `*StatusError`.
Authentication is the tags' flow: Basic, then Bearer on a challenge, the token
kept across pages.

- **`next` is followed only to the same origin.** `sameOrigin` compares scheme
  and host strictly (`host` and `host:443` differ), so a registry cannot make
  the walk send the Basic credentials or the token elsewhere. A foreign or
  differently written `next` ends the walk as `truncated`, with no error.
- **HTTP 3xx redirects are hardened too** (DEVDESK-410): the walk goes through
  `registryHTTPClient`, whose `CheckRedirect` refuses a redirect to another
  origin (see "Credentials stay on their origin" above).
- **Under a prefix, `next` must also stay on the member's catalog path.** A
  non-empty prefix follows a `next` only when its escaped path is exactly
  `/v2/<prefix>/_catalog` (same origin already checked); a link to
  `/v2/_catalog` or to another repository would attach the names of another
  catalog to the prefix, so it ends the walk as `truncated`, unfollowed. With
  an empty prefix any same-origin path is followed, as before.

**Catalog under a prefix (DEVDESK-579, NX-12).** `ListCatalogUnderContext(ctx,
apiURL, prefix, username, password, maxPages, pageSize)` is the same walk on
`/v2/<prefix>/_catalog`, the catalog of one Nexus group member; `ListCatalogContext`
is it with an empty prefix, and the two rules above apply to it unchanged. Names
come back as the server sent them, without the prefix (the caller rebuilds
`<prefix>/<name>`). Each prefix segment goes through `url.PathEscape`, so a
space, `%`, `?`, `#` or an already-encoded `%2F` stays one segment on the wire.
A prefix is refused before any request, with an error that gives the rule and
never carries the prefix or a credential, when it has an empty segment (leading,
trailing or doubled `/`), a `.` segment, any `..` (read literally, `a..b` too),
a backslash, a segment of spaces only, a control character (NUL included), a
segment over 255 bytes or a prefix over 1024 bytes. The blank segment, the
control characters and the length bounds are a choice: no repository name holds
them. Under a prefix, `next` is also held to the member's catalog path (last
bullet above).

## Image search — `internal/imagesearch` (DEVDESK-298, IS-18)

Pure Go, no UI. Types and one interface; two adapters, `HubSearcher` (IS-02)
and `CatalogSearcher` (IS-03); dhi.io is IS-17:

- `Target`: one registry to search (`EntryKey`, `Alias`, `RegistryURL`,
  `APIURL`, `RepoPrefix`, `Username`, `Password`; the credentials are empty for
  an anonymous entry, D12).
- `Query{Text, Limit}`: `Limit` 0 is the adapter's default.
- `Image`: one result (`EntryKey`, `Name`, `Repo`, `Description`, `Stars`,
  `HasStars`, `Official`). `Repo` is the final path, with the prefix and
  `library/` applied once (D39).
- `Searcher.Search(ctx, Target, Query) (images, partial, err)`, and
  `SearcherFunc`, the function adapter.
- `HubSearcher` (DEVDESK-301, IS-02, `hub.go`): anonymous, one GET on
  `https://index.docker.io/v1/search` (the endpoint `docker search` uses, not a
  public Hub contract), one page of at most `HubLimit` (50) results, no
  credentials (Target's are ignored), no redirects, 15 s client timeout, body
  capped at 1 MiB. A non-200 answer is an `*oci.StatusError`. A result without
  `star_count` keeps `HasStars` false (the Stars cell stays empty, never 0).
  Names are validated against a regex (`hubComponent`: lowercase alphanumerics
  joined by `.`, `_`, `__` or runs of `-`, as in the OCI distribution spec); a
  name that fails is dropped (`-rm`, `--help`, uppercase, `a//b`, a leading or
  trailing separator). A description is sanitised: escape sequences, control
  characters and Unicode format characters (category Cf: bidi overrides,
  zero-width, BOM) are removed. Privacy: the text the user typed is sent to
  Docker Hub as soon as a Hub entry is checked.
- `ByRegistry(hub, other)` (`route.go`): a target whose host is exactly `dhi.io`
  (DEVDESK-303, `isDHI`: case, scheme, port and path ignored; `registry.dhi.io`
  and `notdhi.io` are not dhi.io) is refused first, with `ErrUnsupported` and no
  request to either searcher, since dhi.io has no documented catalogue API
  (IS-17). Otherwise a target for which `oci.IsDockerHub(RegistryURL)` is true
  goes to `hub`, any other to `other`. A nil searcher answers `ErrUnsupported`
  with no request at all. Production uses
  `ByRegistry(HubSearcher{}, CatalogSearcher{})`: Hub goes to the Hub search,
  every other registry to its catalog. `isDHI` parses the URL (`url.Parse`, with
  `https://` added when there is no scheme) and compares `Hostname()`, so
  userinfo (`dhi.io@evil.example`), a trailing dot and a port cannot fool it.
- `CatalogSearcher` (DEVDESK-306, IS-03, `catalog.go`): searches a private
  registry by walking `/v2/_catalog` through `oci.ListCatalogContext` (or, for a
  Nexus member, `/v2/<prefix>/_catalog`, see "Catalog under a prefix" below) and
  filtering **client-side**: the text the user typed is never sent to the
  registry. Steps, in order:
  1. The query is trimmed; fewer than `CatalogMinQueryRunes` (2) runes returns
     `ErrQueryTooShort` with **no request**.
  2. The walk: `CatalogPageSize` (1000) names per page, at most
     `CatalogMaxPages` (10) pages, so at most 10 000 names. A cut walk sets
     `truncated`. An error from the walk (status, deadline, unreadable page)
     is returned as is, with no row.
  3. Guard R1 (before the filter), **root catalog only**: when the target has a `RepoPrefix`, only the
     names that start with `<prefix>/` are candidates. If the walk is complete and
     no name carries the prefix, the answer is `ErrPrefixNotInCatalog` (the
     cause names the prefix); this protects against a catalog that does not
     list names the way the entry reaches them and would otherwise read as a
     false "no result". The assumption that a Nexus catalog lists names prefixed
     by the repository was **contradicted** on the bench (Nexus 3.98.1-01
     Community, 2026-11-17, NX-03, hypothesis H1 of IS-03; D167): Nexus lists
     names without the prefix, so a Nexus member no longer goes through R1 (see
     `CatalogUnderPrefix` below). R1 still applies to every other registry that
     has a `repo_prefix`; none of them was measured. If the walk is truncated, the
     prefix may be on a page not read: the answer is no result with
     `partial` set, not an error. A target with `CatalogUnderPrefix` skips this
     step (see below).
  4. Filter: case-insensitive substring match on the part **after** the prefix.
     `Image.Name` is that part, `Image.Repo` the full catalog name.
  5. Only valid names are kept (`validRepoPath`, `names.go`: every
     `/`-separated component follows the distribution-spec grammar, as for Hub);
     others are dropped.
  6. Order by `MatchTier`, then name, then `Repo`; at most `CatalogMaxRows`
     (100) rows (`Query.Limit` can only lower that). Rows cut by the limit set
     `partial`, as does a truncated walk.

  `MatchTier` (ranks 0 to 4, see the order below) now lives in `imagesearch`
  (`names.go`) so the searcher and the table share one ranking;
  `oci_resources.matchTier` delegates to it. The 15 s per-registry deadline
  (`imageSearchDeadline`, below) covers the whole walk; hitting it is a
  `failed` verdict with no row. `CatalogSearcher.List` replaces
  `oci.ListCatalogContext` in tests. Walks per search
  (DEVDESK-311, IS-07, `walks.go`): `WithSharedWalks(ctx)` puts a memo on the
  batch context and `CatalogSearcher` goes through `walkCatalog`, so a search
  queries `/v2/_catalog` once per API URL and identity. Without the memo
  (any other caller) every call walks on its own.
  - Key: the exact API URL (trailing slash ignored), the catalog prefix
    (`catalogPrefix`, empty for the root catalog; DEVDESK-579) and the identity, an
    HMAC-SHA256 of username and password under a random per-batch key: no
    secret is held in clear and the key prints no userinfo. Two identities
    never share a walk, because Harbor, Artifactory and GitLab filter the
    catalog per account. A `credentials` entry with no effective secret shares
    the walk of an anonymous entry. A member on another port or subdomain has
    another API URL and is not grouped. Two Nexus members of one host read two
    different catalogs, so they have two keys and two walks: D140 no longer
    merges them (D167 amends it). Entries on the root catalog still share one
    walk per API URL and identity.
  - Leader and followers: the first call for a key walks, the others wait (or
    return on their own context). It is a memo, not a singleflight: the key is
    kept after the walk, so a member arriving later reuses the result, and it
    dies with the batch (no cache between two searches).
  - A shared failure (a 503, say) is the same error for every member, so the
    same verdict (`failed` or `refused`), one message per entry. A result
    caused by the leader's own context ending is not memoized: followers
    already waiting on that flight receive the leader's context error (verdict
    `failed`), and only members arriving afterwards walk again. A
    panic in the leader gives followers a sentinel error (`errWalkAborted`),
    never an empty catalog.
  - `repo_prefix` filtering, `partial`, `ErrPrefixNotInCatalog` and the
    `May omit images` mention stay computed per member.
  - Members of one Nexus host do **not** answer on the same `/v2/_catalog`
    (measured, Nexus 3.98.1-01): each answers on its own path catalog, which is why
    the key carries the prefix. This contradicts the shared-walk premise of
    IS-07 / D140 for Nexus members (measured on the bench, Nexus 3.98.1-01
    Community, 2026-11-17, NX-03; D167). For other registries on one host
    (Harbor, Artifactory, GitLab) the premise is neither confirmed nor
    contradicted: not measured.
  A catalog may be disabled or filtered to the account's rights (Harbor,
  Artifactory, GitLab): the answer is then `refused` or a shortened list.
- **`Target.CatalogUnderPrefix` (DEVDESK-579, NX-12).** A Nexus member has no
  usable root catalog: `/v2/_catalog` answers 404 on the HTTP port, and on a
  group connector it lists the members' names together with no member attribution.
  With the flag set, `CatalogSearcher` hands the lister `catalogPrefix = RepoPrefix`
  and reads `/v2/<prefix>/_catalog`. Differences from the root catalog:
  - Nexus returns names **without** the prefix; `Image.Repo` is rebuilt as
    `<prefix>/<name>`.
  - No guard R1: every name belongs to the member. An empty catalog is
    `no result`, not `refused`.
  - A prefix that fails `validRepoPath` is `refused` before any request. That
    check refuses uppercase, so a catalog name outside the OCI grammar is
    ignored and a member whose repository name has uppercase is refused. Not
    measured: whether Nexus serves `/v2/<UpperCaseName>/_catalog`.
  - Verdicts: 404, 401, 403 and 405 are `refused`; 5xx, 429 and network errors
    are `failed`.
  - The walk key holds the prefix (D140 amended, above). No request goes to
    `/v2/_catalog` for these targets.
  Who sets the flag is the UI's decision: see "Image search and Nexus members"
  below. Every other target keeps the root catalog and R1.
- **Verdicts (DEVDESK-303, IS-12, `verdict.go`).** `Classify(err, n, partial)`
  is a pure function that turns one registry's answer into an `Outcome{Verdict,
  Partial, Skipped}`. `Verdict` is one of `VerdictOK` (`ok`), `VerdictNoResult`
  (`no result`), `VerdictRefused` (`unsupported or refused`) and
  `VerdictFailed` (`failed`); the zero value is not a verdict (`pending`) and
  `Classify` never returns it. `Partial` and `Skipped` are marks, not verdicts:
  `Partial` with `ok` or `no result` (invariant Q1: a walk cut short that found
  nothing is still `no result`, marked `Partial`), never with `refused` or
  `failed`, never with `Skipped`; `Skipped` only with `no result`. Rules, first
  match wins:
  1. no error: `n > 0` is `ok`, `n == 0` is `no result`; `Partial` is kept as
     given in both cases;
  2. `ErrQueryTooShort` is `no result` with `Skipped`;
  3. `ErrUnsupported` or `ErrPrefixNotInCatalog` is `refused`;
  4. a `*oci.StatusError` is `refused` when `refusedStatus` holds: 401 and 403
     from either source, 404 and 405 from `SourceRegistry` only (a 404 from the
     token endpoint is a failure), except a 404 marked `UpstreamBlocked()` (a
     Nexus proxy whose upstream is blocked, DEVDESK-632), which is `failed`;
     any other status, 429 and 5xx included, is `failed`;
  5. anything else is `failed`: deadline, cancel, network, unreadable or
     oversized body, invalid request.

  `ErrQueryTooShort` and `ErrPrefixNotInCatalog` are returned by
  `CatalogSearcher` (DEVDESK-306).

## Image updates — `internal/imageupdate` (§3.88)

Three tables carry an **Update** column (`internal/ui/updatecol`): oci's Images
tab, the containers list, and the Remediation tab of a scan result. It shows an
arrow when the image's registry holds something newer, and says what:

| Kind | When | Cell |
|---|---|---|
| `NewPatch` | the tag has ≥ 3 version components and a tag differing only by the last, same variant, is higher (`20.11.1-alpine` → `20.11.4-alpine`) | the arrow and the tag |
| `NewBuild` | the digest the tag points to now is none of the local image's digests | the arrow alone (`IconArrowDown`) |

A newer patch wins over a new build. Every other state says why there is no
arrow, in grey — a blank used to stand for all of them, which in the
Remediation tab (bases rarely pinned, often not held locally) meant a column
that was silent without saying why:

| Kind | Cell | When |
|---|---|---|
| `UpToDate` | the check mark | the local digest is the tag's |
| `Pending` | `IconHourglass` | no answer yet |
| `Failed` | `IconHelpCircle` | the registry did not answer (logged) |
| `LocalBuild` | `IconHammer` | images, containers: no registry digest |
| `NotLocal` | `IconCloud` | Remediation: unpinned, and not held by the engine |
| `Pinned` | `IconPin` | a digest with no tag: nothing can move |
| `None` | blank | the question does not apply (untagged image, unresolved `FROM`) |

Every state but a newer patch is one glyph (`updatecol.Cell`): the words cost
eleven cells in the widest table there is, and the reason is still spelled out
by `G`'s greyed shortcut in the footer and by `?`. `Status.Label` keeps the
word, which is what the filter matches. `Evaluate` takes what "no local digest"
means to its caller (`LocalBuild` or `NotLocal`). The column's filter only matches an update's label, or `/ca`
would find every `local build`. A tag with fewer components (`3.20`,
`20`) has no patches of its own — it floats over them, so a new patch moves the
tag and the digest says it. That is also the only answer a floating tag
(§3.79) ever gets.

**The registry's side is cached, the comparison is not.** `imageupdate.Check`
keeps, per reference, the tag's digest and the newer patch tag in
`~/.devdesk/cache/image-updates.json` — 6 hours for an answer, 30 minutes for a
failure (unknown repository, no credentials) so it is not asked on every
refresh. `Evaluate` compares with the local digests each time a row is built,
so a pull clears the arrow without asking the registry again.

**Two requests at most per reference.** The digest is a `HEAD` on the manifest
(`oci.ManifestDigest`) accepting an OCI index and a Docker manifest list — a
multi-platform tag's digest is the index's, the one `docker pull` records in
`RepoDigests`. Docker Hub does not count a `HEAD` against its pull rate limit.
The tag list (`oci.ListRegistryTags`) is only read for a tag with a patch
component. At most four references are asked at once; credentials are the
engine's (`docker.GetStoredCreds`), as for the Remediation tab. For a URL with
a path the lookup is exact key first, then `host[:port]` as written (never the
host alone when the port differs, no default port made explicit), shared with
`RegistryLoginState` through `credentialPhases` (DEVDESK-635); logout stays on
the exact key. These callers pass the image's own registry host, so the login
they find goes to that same host; only the group discovery can name a second one
(`management_url`), see `registries.md`.

**What each view compares with:**

- *Images* — the image's own `RepoDigests`, now read by the same `image
  inspect` that reads its size (`templates().ImageInspect`). Only a tagged image
  **with** a digest is asked about: one built or loaded here has none, and its
  name would send the question to a registry that never heard of it.
- *Containers* — the digests of the image the container was **created from**
  (`docker.ContainerImageDigests`: `container inspect` for the image ID, then
  `image inspect`). After a pull the container still runs the old image, so the
  arrow stays until it is recreated — which is what it is there to say. The
  digests are read again only when the set of containers changes, not on every
  two-second refresh.
- *Remediation* — the digest the Dockerfile pins, or else the image the engine
  holds under that name (`docker.ImageRepoDigests`). A base the engine does not
  hold only gets the patch side.

`imageupdate.Tracker` is what each view keeps between checks: the facts it has
and the references a check is out for, so a reload asks only what is new or
stale. Every view reaches the network through a package variable its tests
replace.

**Updating — `G` on the Images tab.** It pulls what the column points at (the
newer patch tag, or the same tag again for a new build) and removes the image
it replaces, by ID and never forced. It refuses while any container, running
or stopped, was created from that image (`docker.ContainersUsingImage`, `ps -a
--filter ancestor=`): removing it would break the container, and keeping it
would leave the update half done. The check is made twice — from the engine's
container count to grey `G` (Rule 130; docker only, podman reports none), and
against the engine just before the pull, since a container can appear in
between. A pull that brings the image already held removes nothing
(`docker.SameImageID`). It runs as a pull job, so the row spins and `K` stops
it; every refusal and failure is a footer line naming why (the containers in
use, a failed pull, an old image that could not be removed).

**On a narrow terminal**, the containers table drops the two gauges before
Update (they are `DropFirst`, §3.71), then the I/O counters, then Update. Both
gauges now need 180 columns instead of 160.

**Not done**: a digest for a tag the engine holds under another name, and the
`podman` side of `RepoDigests` has not been measured against a real `podman
system service` — the field exists there under the same name.

**From a container to its image — `J`** (§3.89). The containers view sends
`ociresources.FocusImageRequestMsg` with the image reference exactly as the
engine printed it; the router switches to the OCI view and hands it the
request, which waits for the first listing if the view was just built
(`deferUntilListed`, the same queue agents use). Matching is the OCI view's
(`imageMatches`, `focus.go`): an implicit `latest`, docker's `nginx` against
podman's `docker.io/library/nginx`, a registry port that is not a tag, a
`repo@sha256:` digest against `RepoDigests`, and a bare ID — what `ps` shows
once the tag has moved to a newer image. A search hiding the target is
dropped; an image that is no longer listed is a footer `Warn`. There is no
`esc` back: `:ct` returns, as from any other view.

## The OCI tables and the activity column — `internal/ui/oci_resources` (DEVDESK-146, A-09a; DEVDESK-153, A-09b)

The four tables of `:oci` (Images, Networks, Volumes, Registries) and the
registry browser's tag table are on the activity column (Rule 125; the router
side is in `app-shell.md`).

**Periodic refresh and terminal focus (DEVDESK-210).** `refreshInterval` is 10 s
(a var, so a test can shorten it). `RefreshTickMsg` used to refresh once and
never again; it now re-arms `tickCmd()` on every tick. While `Model.blurred` is
set (from `shared.TerminalFocusMsg`) the tick only re-arms and skips
`refreshReads()` (images, scan cache, networks, volumes; it never scans, Rule
126). On the focus edge (`Resume`) it runs `refreshReads()` once, with no new tick.

**Registry browser (DEVDESK-218, A-09a2).** The tag table
(`registry_browser.go`, built by `newTagTable`, which `ActivityTables` also uses
so the guard sees the real table open or not, as `oci/registry-browser`) takes
`datatable.IndicatorColumn` as column 0, an 8th column; Registry, Tag and
Updated are 1 to 3 (`tagColumnTag` = 2, `tagColumnUpdated` = 3). `tagRow` holds
`Activity components.RowActivity` and `ScanRunning`, stamped by
`rebuildTagTable` from `components.DescribeRow` (scan and pull, `ociKinds`,
target `jobs.TargetKey(imageName)`) over the snapshot the router pushes through
`SetJobs(runs, failures, context)`; the cell at rest is empty. While a scan
really runs (not queued) the C/H/M/L cells hold `…` in `DimStyle`; a queued scan
or a pull keeps the counts. The blink phase arrives with `SetBlinkPhase` and
`WantsBlink` is true for a visible running tag row. Rule 116: Updated drops
under 102 columns, L under 84, M under 78, H under 72, C under 66; at 60 only
the dot, Registry and Tag remain. The browser has no spinner of its own
(DEVDESK-374): a search shows the shared activity dot in the footer
(`Searching registries...`), and `WantsBlink` is true while it runs. After the search the
footer describes the selected tag (`Status{Detail}`). `tagScanSpinnerIdx` and
the local frame are deleted. `DescribeRow` filters by context: a scan of another
context no longer shows on a tag (D108).

**Cancelling a lot and ignoring late answers (DEVDESK-298, IS-18).** The
browser launches requests in lots (one per validation that starts something).
`browserGen` is a package-wide `atomic.Uint64`, incremented only from `Update`
(`nextBrowserGen`). `beginRequests()` cancels the previous lot (one
`CancelFunc`, `cancelRequests`, per lot, not per registry), takes a new
generation and resets `pendingSearches`; the `context` is born in `Update`
(Rule 110). `MultiRegistryTagsLoadedMsg`, `MultiRegistryTagsMetaMsg` and
`ImageSearchResultMsg` carry the `Gen` they were started with, and their
handlers return before logging anything when it differs from the open
browser's. The counter is global rather than per browser so that a browser
closed and reopened, or a new `Model` after a context switch, cannot accept the
answer of an older instance. `closeMultiRegistryBrowser` calls
`cancelInFlight()`, which also covers closing through `G` (a pull). A validation
that starts nothing (empty name, no registry checked) cancels nothing and
leaves `gen` alone. The tag lookups go through the `listRegistryTags` variable
(`oci.ListRegistryTagsContext`), the seam tests swap to capture the context;
there is no deadline on a tag lookup, only `maxTagPages` and the per-request
client timeout. `Model.RegistryBrowserGen()` exposes the generation to tests
only.

**Level 1: image search (DEVDESK-299, IS-01; Open Tags DEVDESK-300, IS-11).**
The browser has three states: `browserStateInput` (form), `browserStateImages`
(level 1, appended last in the `iota` list) and `browserStateTags` (level 2).

*The form's buttons.* `formButtons()` (`browser_form.go`) is the single source
of the buttons: the keyboard, the rendering (`renderFormButtons`, one line) and
the header (`FormEnter`) all read it. Without a `Searcher` there is one button,
`Browse Tags` (action `actionSearch`). With one there are two, `Search`
(`actionSearch`) then `Open Tags` (`actionOpenTags`).

*Field indices.* Repository is `brFieldRepo` (0); the picker checkboxes are
1..n (n = `len(b.rows)`); the buttons follow: `brFieldButton(i)` = `1 + n + i`,
and `brFieldLast()` is the last button. `focusedButton()` maps the focus back to
a button (false on Repository or a checkbox). `down` stops at `brFieldLast()`.

*Validation.* `submit(action)` is common to both actions: an empty name starts
nothing; no registry checked sends `RegistryBrowserWarnMsg{"No registry
selected"}`, which the `Model` shows as a footer warning (the browser has no
footer; this also applies to a name that contains `/`, where the tag flow used
to refuse silently). Then `actionOpenTags` always goes to `startTagLookup`
(the former code extracted as is, name as typed). `actionSearch` follows the
IS-01 rule: a name with no `/` goes to `startImageSearch` when `b.searcher !=
nil`, anything else to `startTagLookup`. `enter` on Repository runs
`actionSearch`; `enter` on a button runs that button's action; `enter` on a
checkbox only advances the focus. The selection applies to both actions.

*Header.* `FormEnter()` returns the label and the lit state of the header's
single `enter` entry. The label follows the focus: `Search` on Repository and
on the Search button (`Browse tags` without a `Searcher`), `Open tags` on the
Open Tags button. `enter` is lit on Repository (Rule 130: it submits there) and
on a button, and greyed on a checkbox.

*Help.* `registryBrowserHelpBody(searchEnabled)` (`view.go`) depends on the
mode (Rule 114): without a `Searcher` it names Browse Tags only; with one it
describes Search, Open Tags and the shared selection, and the level 1 sentence.

**`defaultImageSearcher` is `ByRegistry(HubSearcher{}, CatalogSearcher{})` in
production (DEVDESK-301, IS-02; catalog since DEVDESK-306, IS-03), so level 1
and Open Tags are active.** `newRegistryBrowser`
copies it into `b.searcher`. Tests do not use it: `TestMain`
(`ociresources_test.go`) saves it in `productionImageSearcher` and sets
`defaultImageSearcher` to `nil` for the whole suite (no test may reach the real
Hub), and tests inject a fake with `withImageSearcher`.
`TestProductionWiresTheHubSearcher` reads the saved value. `internal/app`
injects nothing: its tests avoid level 1 by typing a name with a `/`.

`startImageSearch` runs one `searchImagesCmd` per checked registry, with
`targetFor(entry)` (same credentials as the tag flow, `credsFor`). Each command
wraps the lot's context in `context.WithTimeout(ctx, imageSearchDeadline)`:
`imageSearchDeadline` is 15 s **per registry**, the whole walk included, on top
of the 15 s client timeout per request (`imageSearchDeadline = oci.RequestTimeout`,
see "The abandon after 15 s is typed" above). A registry's
answer arrives as `ImageSearchResultMsg{Gen, EntryKey, Images, Partial, Err}`
and `AddImageResults` takes it (false when stale; `pendingSearches` never goes
under 0).

**Verdicts per registry (DEVDESK-303, IS-12).** `AddImageResults` calls
`imagesearch.Classify` in `Update`, never in the `Cmd` (Rule 110), and stores the
result in `RegistryBrowser.outcomes`, keyed by `EntryKey`. The map is reset by
`startImageSearch`, so it holds one lot only; a registry with no entry is still
pending. Only an `ok` verdict appends rows. `Outcome(entryKey)` reads one
verdict (false while pending). `Model.RegistryBrowserOutcomes()` returns a copy
of the map, or nil when the browser is closed; it is for the tests and for IS-13.

The Nexus verdicts below rest on measures made on the local bench, not on a
company instance: read "Measured Nexus behavior" after this section before
trusting a verdict for another Nexus.

**Image search and Nexus members (DEVDESK-579, NX-12).** `targetFor` sets
`CatalogUnderPrefix` on two kinds of entry: a plain entry that lists cached
content only and has a `repo_prefix` (a `parent:` entry of a `provider: nexus`
group), and a discovered member of a Nexus group that has a `repo_prefix`. The two
tests are equivalent, because `checkRepoPrefixes` refuses a `repo_prefix` on a
group. Any other entry keeps the root catalog and guard R1.

`searchUnits` decides what a lot searches. Discovered members **without an
address of their own** (same URL and same `repo_prefix` as their group, with the
group's slug as `ParentSlug`) are merged into **one** unit, the group, labelled
`<alias> (group)` (`groupUnitSuffix`). It is walked once, and its rows name no
member: Nexus does not say which member holds a name. By design this bypasses the
picker's selection: ticking one bare member searches the whole group, and a row can
carry the name of a member that was unticked. A group that was not discovered (a
pull account) is searched as itself, with its bare label. The Registry column
keeps the whole suffix when it cuts a cell (`datatable.Column.KeepSuffix`, see
`ui-components.md`). `lotUnits` holds the units of the current lot until the next
image search; `entryFor` falls back on it, so labels, the `→` tags drill of a group
row and the `r` filter work on a unit. The `?` help says: `'nx (group)': images
found through the group; Nexus does not say which member holds each one.`

**Measured Nexus behavior (NX-03, NX-04, NX-05).** Where the code rests on how
Nexus answers, this is what was observed and what was not. Every row was
**measured on the local bench**: Nexus Repository **3.98.1-01 Community**
(`docker.io/sonatype/nexus3:3.98.1`), k0s cluster, HTTP only, no reverse proxy,
no TLS, a file blob store (reported, not independently verified), three repositories holding six image names, one pull account with rights on
all Docker repositories. It was **not measured on the company's Nexus instance**:
the user decided on 2026-11-17 not to cross-check it, and that has not been done
since. The only datum from a company instance is the 2026-08-10 note (a pull
account refused on the detailed endpoint, Nexus version not recorded); it was
not re-measured there. The raw answers and the dates are in the frozen notes
`docs/measurements/2026-11-17-nexus-catalog.md` (NX-03),
`2026-11-20-nexus-cached-only.md` (NX-04) and `2026-11-20-nexus-access.md`
(NX-05); those notes describe the code of their day, this section the code now.

| Behavior | Measured (3.98.1-01 Community, bench) | Verdict on the old assumption | Code now |
|---|---|---|---|
| `_catalog` prefix (2026-11-17) | Names carry **no** repository prefix, whatever the URL form. `/v2/_catalog` on the HTTP port is a 404 (HTML): no root catalog. A hosted or proxy repository answers on `/v2/<repo>/_catalog` (200); a group answers on its own connector (root of its port: the union of its members, no attribution) and 404s under `/v2/<group>/_catalog` | H1 of IS-03 (prefixed names) and IS-07 (members of one host share a root catalog): **contradicted** (D167) | `ListCatalogUnderContext` reads `/v2/<prefix>/_catalog` and rebuilds `<prefix>/<name>`; one walk per member; a group is one unit |
| `_catalog` rights (2026-11-17) | The pull account and the anonymous account get 200 and the full list, same as admin | H2 (a pull account may call `_catalog`): **confirmed** for the bench accounts | `refused` only on 401, 403, 404, 405 |
| Detailed group endpoint (2026-11-17) | `GET /service/rest/v1/repositories/<format>/group/<name>`: the pull account gets **404** `Repository not found`, anonymous gets 403, admin gets `memberNames`. The generic endpoint answers all three, without members | The D161 expectation of a 403 for the pull account: **contradicted on the bench**; the 403 seen on 2026-08-10 on a company instance (version unknown) is **not settled** (DEVDESK-578) | the 404 with that exact message is a members refusal (`registries.md`) |
| Proxy content (2026-11-20, NX-04) | Upstream reachable: `_catalog` and `tags/list` list what the **upstream** holds (`mirror/uncached` is listed and pullable); a HEAD on a manifest pulls it **and writes it to the proxy cache**, `tags/list` and `_catalog` do not (cache writes read from `status` output, reported, not independently verified; NX-03 saw the same on an unrequested HEAD, attribution inferred). Upstream cut: the first `_catalog` read gets a 502 after about 20 s (reported, not independently verified), then the proxy auto-blocks and answers 404 `Remote Auto Blocked until <date>` to `_catalog` and `tags/list`, even for an image in its cache; the block is temporary (Nexus lifted it 21 s to 79 s after the cut, and `restore-upstream` lifts it at once); a HEAD still serves the cached image (200) and 404s the uncached one. The group's `_catalog` then omits the proxy's names with a 200 and its `tags/list` of a proxy image is an empty 200 | H3 / IS-14 / D137 (a proxy lists its cache, "limited to cached content"): **contradicted** | D168 (DEVDESK-629): `May omit images` replaces `Cached only`; the blocked 404 is `failed`, marked `UpstreamBlocked()` (DEVDESK-632); a delay is `no answer within 15 s` (DEVDESK-633) |
| Routing (2026-11-20, NX-05) | By path on the HTTP port (`/v2/<repo>/...`, also `/repository/<repo>/v2/...`) and by the group's connector: all answer 200 for admin, pull and anonymous | path and group connector: **confirmed** | DevDesk calls `/v2/<repo>/...` and the connector; it never uses the `/repository/` form (D39) |
| Bearer realm (2026-11-20, NX-05) | An anonymous `GET /v2/` is a 401 `Bearer` whose realm is on the same origin as the request, with the connector or repository path; the token request answers 200 even anonymously, but replaying `GET /v2/` with that token was a 401 again (nominal and with `DockerToken` removed). A wrong password gets a 401 `BASIC`, no `Bearer` challenge. With valid credentials DevDesk sends Basic first and never enters the Bearer flow. The realm is built from the announced host: `X-Forwarded-Host` and `X-Forwarded-Proto` change it | same-origin realm: **confirmed**. Token replay: on `GET /v2/` the token did not make the request accepted (the NX-05 note says so for this bench); NX-03 had a token accepted on a group-connector catalog. No other authenticated Bearer flow measured, and the answer to the token request with anonymous access cut is not | D168: a realm on another origin gets no credentials (`ErrTokenRealmOffOrigin`, two frozen exceptions, see "Credentials stay on their origin") |
| Redirects (2026-11-20, NX-05) | No 3xx and no `Location` in any answer (`tags`, `_catalog`, manifest HEAD, blob GET on `docker-hosted`, `/v2` without a slash) | none observed on this bench (same origin); off-origin: inconclusive (file blob store, no S3, no reverse proxy: reported, not verified; nothing read through the proxy or group) | the off-origin refusal stays (`RefuseOffOriginRedirect`), covered by hand-written tests only |
| `Link` and `n` (2026-11-17, 2026-11-20) | `n` is respected, `Link` is absolute and same-origin; `?n=1000` on `/v2/<repo>/_catalog` is a 200 for hosted and proxy, a 404 for the group by path and for the HTTP root. A group on its connector with `n=2` repeats names across pages (5 pages for 6 names) | confirmed (repeat: a new fact) | the catalog walk drops repeated names (DEVDESK-634) |

**Still open: not measured, or not conclusive.** Each one says what is missing.

- **An account restricted to some repositories** (a silent filtering that would
  read as a false "no result"; also the 403 or 404 of the detailed endpoint for
  such an account): the bench has no such account. Needs a role and a user limited
  to one Docker repository, which is a write on the bench.
- **A different Nexus version, a Nexus Pro, a Nexus behind a reverse proxy, HTTPS,
  LDAP, an S3 blob store, `forceBasicAuth`**: one version on one HTTP bench. The
  403-versus-404 difference with the 2026-08-10 note may come from the version;
  needs the version of that instance or a second bench.
- **A realm or a redirect on another origin**: the bench produces none (only the
  `X-Forwarded-*` probe shows the realm follows the announced host). Needs a
  reverse proxy in front of the bench, or an S3 blob store.
- **Subdomain routing and a dedicated connector for a hosted or proxy
  repository** (also `docker.subdomain`): not tried (W7 and W8 not done); the
  subdomain connector is documented as Pro, unverified on Community.
- **Whether the group's 404 under `/v2/<group>/_catalog` comes from
  `pathEnabled`** (on the three repositories), and the tolerance of `?n=1000` on
  `/repository/<repo>/v2/` paths and for the pull and anonymous identities of the
  group connector.
- **An uppercase repository name** (`/v2/<Name>/_catalog`).
- **An anonymous call with anonymous access cut**: the 401 `Bearer` is known, the
  answer of its token request is not.
- **The duration of the auto-block on `_catalog`**, a proxy with an empty cache,
  and the group's `tags/list` of a homonym with the upstream cut.
- **Logout of an entry with a path** (`U`): probably clears every repository of
  the host (D168, R6); not measured.
- **Why a group repeats names across pages**: the group probably merges the pages
  of its members; not settled.

**Level 1 header summary (DEVDESK-305, IS-13).** `searchHeaderInfo`
(`browser_summary.go`, called from `GetHeaderInfo`) says what the lot did, one
header line per class, in this order: `Images` (the row count, always), then
`Pending` (a count, live lot) or `Interrupted` (names, the lot was abandoned
with the left arrow before they answered), `Failed` (red), `Refused`, `Partial`
(orange), `May omit images` (orange, DEVDESK-308 IS-14, reworded by DEVDESK-629 F2), `No result` (dim) and `Skipped` (dim, catalog not walked because the
query is shorter than `imagesearch.CatalogMinQueryRunes`). A class with nothing
to say has no line, so a lot where every registry answered shows only `Images`.
`summarizeSearch` (pure) sorts the lot's keys into the classes. A name is the
Registry column's label (`entryLabel`: alias, else URL, else key), in form order.
`Partial` lists registries cut at a cap, `No result` those that answered with
nothing (Skipped excluded). A registry that is both partial and empty appears
on both lines, on purpose.

`May omit images` lists the registries that may not have listed every image
they hold, by their role in a Nexus group (DEVDESK-629, F2; it replaces the
`Cached only` line of DEVDESK-308, whose "limited to cached content" reading the
measures of NX-04 contradicted). `browserRegistryEntry.role` is a `nexusRole`
(group, proxy, hosted, unknown), set once by `buildEntries`: a
`provider: nexus` + `kind: group` entry is a group; a discovered member of one
takes `memberRole(RegistryGroupMember.Type)` (`hosted`, `proxy`, `group`; any
other value, case included, is unknown); a declared entry whose `parent` is the
slug of a Nexus group takes the type of the discovered member at its exact
`(URL, RepoPrefix)`, and is unknown when none matches or the cache predates the
type (H5); a group unit (`groupUnit`) is a group. `isNexusGroupOrMember` (the
former `listsCacheOnly`) only drives `catalogUnderPrefix`. The pure rule is
`mayOmit(role, outcome)`: a group whatever it answered (it says nothing about
what a member it could not reach left out), a proxy only when it `failed` or was
`refused`, a hosted repository never, an unknown role never (no mention is better
than a false one), and never a `Skipped` outcome. `summarizeSearch` takes a
`func(string) nexusRole` and reads it by entry key (`entryRole`, not by slug:
members share their group's slug). A group that answered with no result is
marked ` (no result)` on that line and is not on `No result`; the mark does not
mean the image is absent, only that this registry listed nothing and may have
left out what it could not reach, while `No result: quay` means the registry
answered and holds nothing that matches. A `failed` or `refused` mention stays on
its own line too and carries no mark; a partial registry appears on `Partial` and
on `May omit images`. Docker Hub, generic registries and a Nexus outside a group
never get the line. `Skipped` and `May omit images` exclude each other (`Skipped`
means no catalog entry answered), which keeps the budget below. The search sends
no request to the members of a group (F4): the group answers on its own connector,
and a member the group could not reach is invisible to DevDesk; the line says it
may happen, it cannot say it did. The MCP server is unchanged.
The Nexus catalog tests of `internal/imagesearch` (`TestCatalogOnMeasuredNexusAnswers`)
replay answers measured on the bench (Nexus 3.98.1-01, NX-03), not hand-written
ones: the verdicts `ok`, `No result` and `Refused` (prefix absent from a real
catalog) are checked there. `Failed` and the group line are measured since NX-04
(DEVDESK-580, PR3): a proxy's catalog lists names it does not hold
(`mirror/uncached`) while its upstream answers, and a group on its connector answers
`no result` for `mirror` once a member's upstream is cut, which is why `(no result)`
does not mean absent.
See "Recorded fixtures" in [`nexus-bench.md`](nexus-bench.md).

Each name list is fitted by `fitNames` to the room a value has
(`shortcut.HeaderValueWidth`), so the `+N` is never lost: all the names, else
the first k names then `+N`, else `+N` alone. `Skipped` carries the hint
` (type N+ characters)`, which is the first thing dropped when the header is
narrow. The summary has a budget of `shortcut.HeaderRows` (7) lines, `Images`
included; classes after the sixth are left out (with `May omit images` the seven
classes fit only because `Pending`/`Interrupted` and `Skipped`/`May omit images`
exclude each other). Before the first
`WindowSizeMsg` nothing is abbreviated. At level 2 (tags) the header is the
usual tag count.

Limit, accepted (R2): below about 50 columns `HeaderValueWidth` is 0 or 1,
nothing fits, `fitNames` returns `+N` anyway and the router clips it.

Logging, by verdict, through `logSearchOutcome` (the alias falls back to the
entry key):
- `failed`: `ERROR [oci_resources] image search on <alias>: <cause>`;
- `refused`: `INFO [oci_resources] image search on <alias>: unsupported or
  refused: <cause>` (a refusal is expected for dhi.io and for a registry whose catalog
  is disabled, forbidden or not under the entry's prefix, so it does not fill
  the error log);
- `ok` and `no result`: nothing.

The cause is `searchErrorCause(err)`: a `*url.Error` keeps only its Op and Err
(the URL carries the typed text and can carry credentials), then
`jobs.SafeCause`. Nothing else reaches the log. Nothing reaches the screen
either, and no error text reaches `:jobs` (Rule 128).

Level 1 shows `searchTable` (`datatable.Model[searchRow]`, built by
`newSearchTable`, also used by `ActivityTables` as `oci/registry-search`).
Columns: the activity column (`IndicatorColumn`, always `Idle`, `IconDocker` in
the `IconRoleImage` role), Name (flex, searchable), Registry, Stars (empty when
`HasStars` is false), Official (`official` or empty) and Description
(searchable). All cells are plain text (Rule 122). When the terminal narrows,
Description, then Stars, then Official leave whole (`Optional`, with
`DropFirst` on Description and Stars). Stars reserves 7 cells (title plus sort arrow). On Docker Hub rows this makes Description and Stars each leave one cell later than before DEVDESK-309 (Description at 92 columns instead of 91, Stars at 70 instead of 69). Official does not move (61), because Stars is already gone when Official leaves (`TestSearchColumnsLeaveAtTheMeasuredWidths`, `hubRows` fixture). At 60 columns only the activity column,
Name and Registry remain, and the line spans width-2 at every width (Rule 116). Results are
ordered by `sortSearchRows` (`browser_images_order.go`), a pure function of the
rows and the typed name (`RegistryBrowser.searchQuery`, set by
`startImageSearch`), so the order never depends on which registry answered
first. Ranks (`imagesearch.MatchTier`, case-insensitive): 0 name equals the query, 1 last
path segment equals it, 2 last segment starts with it, 3 name contains it, 4 no
match; then name, registry alias, `EntryKey` and `Repo`. Stars play no part in this default order; they serve the
`.` sort below. The
cursor follows the selected row by `EntryKey` + `Repo`, never by index:
`rebuildSearchTable` remembers the selection before `SetItems` and puts the
cursor back with `SetCursor` (all in `Update()`, `datatable` untouched). With
nothing selected (empty table, so also a new search, which starts from
`SetItems(nil)`) the cursor goes to the first row received; a row that
disappears (the cursor's row is no longer listed) sends the cursor to the first row (`GotoTop`). **Sort (DEVDESK-309, IS-05).** `.` calls `cycleSearchSort`, which advances
`datatable.CycleSort`: Name ▲, Name ▼, Registry ▲, Registry ▼, Stars ▼ (Stars
opens descending, `DescFirst`), Stars ▲, then back to the default order above.
Name compares lower-cased, Registry on its lower-cased label (`Less` on each
column); ties keep the default order. `starsKey` gives rows without stars
(`HasStars` false, private registries) one contiguous block, last in ▼ and first
in ▲, apart from an image with zero stars. The cursor follows the selected row
by `EntryKey` + `Repo` (`reselect`, shared with `rebuildSearchTable`). A new
search (`startImageSearch`) resets the sort and clears a confirmed `/` filter.
`.` typed in the open filter bar stays a character of the field.
**Registry filter (DEVDESK-310, IS-06).** `r` calls `cycleSearchRegistryFilter`,
which advances `searchFilter` (a `resultFilter`, apart from the tags' `registryFilter`)
with `nextFilter`: groups, then individual registries, then off. The stops come
from `searchFilterStops` -> `filterStopsFor` (shared with the tags table through
`filterStops`): they follow the form order (`b.searchKeys`) and keep only the
registries that have at least one row. With one registry that produced results
there is no stop, `r` does nothing and its shortcut is greyed. `filterLabel`
(shared with `registryFilterLabel`) names the active stop: the group label; for a registry outside a group, `parent/alias` when it has a parent, else its alias, else `registryRef`. `syncSearchFilterToken` shows it as one active
`components.FilterToken` in the bar, which appears while a filter exists and while `/` is being typed
(the footer grows by 2 lines). The token is display only: `rebuildSearchTable`
prefilters the rows (`searchFilter.matches`) before `SetItems`, and `datatable`
is unchanged. Order of computation: `r` filter, then the `/` query (name or
description, case-insensitive, no network request), then the `.` sort. `r`, `.`
and `:` typed in the open bar are characters of the field. `esc` closes the bar,
then clears the query, then goes back to the form; it does not clear the `r`
filter (cycle to off with `r`). The `r` filter survives `→`/`←` (tags). A new
search clears it, with the sort and the `/` filter. When the row under the
cursor is hidden by `r`, the cursor goes to the top. A filter that leaves no row
leaves a header-only table (Rule 139); `Images` counts the visible rows.
Rule 139: an empty table keeps its
header. The header reads `Images: <visible rows>`, the title carries the typed
name (`... > Registry Browser > <name>`) at level 1 (once `→` opens a row, the
title carries that row's name, see below), and the footer shows the shared
activity dot with `Searching registries...` while `pendingSearches > 0`. Once
the search is done, the footer describes the selected row (`Status.Detail`): its
full reference (`searchRow.ref`, built by `searchRowReference`: host, then
`Repo`; Docker Hub always `docker.io`) and the registry label (`Image <ref> on
<label>`, no ` on` when the label is empty); `Searching registries...` keeps
priority. The keys are in
`ui-components.md`.

**Fix at level 2:** `filterStopsFor` dropped the group stop wrongly when a
foreign registry had as many keys as the group had members; it now counts only
the results inside the group.

**`→` at level 1: open the tags of a row (DEVDESK-304).** `→` opens the usual
tag table for the row under the cursor (`openSelectedImageTags`). It sends one
request, to the row's own registry (`selectedImage` finds the entry by
`EntryKey`), for the row's final repository: `searchRow.img.Repo` is passed as
is, with no prefix and no `library/` added again (`normalizeRepoForRegistry` and
`joinRepoPrefix` run only in `startTagLookup`, the typed-name flow). Both flows
build the request through `newTagRequest` (alias, API address, `credsFor`) and
open the lot through `startTagRequests`, so the tag table, the pull and the scan
references are the same as in the full-name flow. `startTagRequests` also
records where the table came from: `tagsFromSearch` (true for `→`) and
`tagsImage` (the row's `Name`, empty for the full-name flow). `GetTitle` shows
`tagsImage` in the tags state instead of the typed name.

- **No row.** `imageDrillIn` (one computation for the header and the handler,
  Rule 130) reports `reasonNoImage`. `→` changes nothing and warns through
  `RegistryBrowserWarnMsg`: footer Warn `No image selected`. The `→ Open tags`
  shortcut is greyed.
- **Failed listing.** `handleMultiRegistryTagsLoaded`, when `tagsFromSearch` is
  set, leaves the table with its header only (Rule 139), shows `tagsLoadFailedText`
  (`Could not load tags`) as a 3 s footer Error, and logs
  `ERROR [oci_resources] registry tags on <alias>: <cause>` where the cause comes
  from `searchErrorCause`: no credentials, no URL. The full-name flow (opened from
  the form) says the same since DEVDESK-667: `handleMultiRegistryTagsFailed` shows
  `Could not load tags`, plus `: <reason>` when `registryFailureReason` has one,
  whichever way the listing was opened; it keeps its own journal line
  (`registry tags <repo> from <url>`).
- **Search in flight.** `→` while the level 1 search still runs cancels it
  (the browser has one lot at a time, `beginRequests`): the rows already shown
  stay, the late ones are dropped. Unchanged by IS-03, although catalog walks
  make searches slower and `→` will cut them more often: the user reopens the
  search. Keeping the lot alive behind the tags would be a separate story.

**Back to level 1 (DEVDESK-307).** `←` and `esc` on the tag table call
`leaveTags`, which returns no Cmd (so no new request). Typing in the tag filter
comes first: `esc` closes the bar and `←` stays in the text field. `leaveTags`
reads the origin from `tagsFromSearch`:

- **Opened with `→`:** back to level 1 (`browserStateImages`), untouched. The
  rows, the sort order, the cursor and the filter are the ones the user left;
  `searchTable` is neither emptied nor rebuilt (`rebuildSearchTable` would move
  the cursor).
- **Opened by full name:** back to the form (`browserStateInput`), focus on the
  Open Tags button as before.

On both origins `leaveTags` calls `abandonLot`: it cancels the tag lot, takes a
new generation and zeroes `pendingSearches`, so a late tag answer or failure is
dropped by the generation test and cannot show `Could not load tags` over the
form or level 1 (this closes open point 5 of D132). It also resets the tag
filters (`registryFilter`, `filterInput`, `filterActive`) and `tagsImage`, so
the title does not keep a stale `[alias]` or image name. Level 1 itself:
`←` goes back to the form (`backToForm`, which also calls `abandonLot`) and
keeps the typed name and the selected registries, even with a confirmed filter
query. `esc` still clears a confirmed query first. While the filter bar is being
typed in, `esc` closes the bar and `←` stays in the text field. There is no `←`
entry in the shortcuts header: `esc Go back` already shows in both states
(Rule 130, D133).

**Networks, Volumes and Registries (DEVDESK-153, A-09b).** Each takes
`datatable.IndicatorColumn` as column 0. Rows carry no `components.RowActivity`:
the datatable reads its own busy marker (`MarkBusy`), and the column's
`Indicator` function returns `Idle` (or `Running` when `registryRow.refreshing`
is set). A remove, login or logout in flight is that marker, reported as
`Running` with its label (`BusyLabel`). On these three tables a failure never
leaves a `⊗` dot, only a footer message. On Registries, a group whose members are being discovered
(`refreshingGroups`) is `Running` too, and the Members cell is static dim text:
the row's dot says it, and `WantsBlink` is true while a group refreshes or a
row action runs, so the periodic refresh alone never blinks. The row spinner is
gone from the whole view: `advanceBusySpinners`, `busyTick`, `spinnerFrameIdx`
and every `AdvanceSpinner` call are deleted. The view's `spinner.Model` and `loadingRegs` are
deleted (DEVDESK-374): a tab's load, the tag search and the network inspection
show the shared activity dot in the footer (Rule 128), and `footerLoading()`
feeds `WantsBlink` with the same priority order as `formStatus()`. A load shows
only while the active tab's table is empty: `ctrl+r` on a filled table is
silent.

**Images (DEVDESK-146, A-09a).**

- **Column 0 is `datatable.IndicatorColumn`.** At rest it shows `IconDocker` in
  the `theme.IconRoleImage` role; while something runs it shows the state glyph.
  `imageRow.Activity` is stamped from `m.rowActivity` (`activity.go`), which
  calls `components.DescribeRow` on the router snapshot (`m.jobs`), the
  remembered failures (`m.failures`) and the snapshot's context.
- **The key is the raw reference, `jobs.TargetKey(img.Name())`.** There is no
  canonicalization: `nginx` and `docker.io/library/nginx` are two keys. Two
  images named `<none>` share one key, as they already share the scan cache
  entry. Kinds read are scan and pull, whoever started them.
- **Static words, no spinner frame in a cell.** The Scanned cell reads
  `scanning` and the ID cell reads `pulling` while the operation runs
  (`scanRunning`, `pullRunning`). The blink phase is read at render, so a frame
  rebuilds no row.
- **Removal is this view's own observation.** The busy `●`/`○` the table used to
  draw is gone; a removal in flight is reported as `Running` with its label
  (`BusyLabel`) and goes through the same column.
- **The footer describes the selected row** (`status()` in `view.go`), after the
  active tab's load state and beside the action line or the batch progress. A
  failed row's detail is fixed, so the spinner stays; a running or queued
  row's detail replaces it.
- **`failedScans` is removed.** A failure is read from `Failures` in the router
  snapshot and stays on the row until the next attempt.
- **After a successful removal the view calls `jobs.ForgetTarget(name)`**
  (`results.go`), so a deleted image does not keep a stale failure.

**Widths (Rule 116).** The activity column costs 4 cells (2 of width and 2 of
padding). Each optional column now appears 4 columns later (the before and after
thresholds, in terminal columns):

| Column | Before | After |
|---|---|---|
| Update | 48 | 52 |
| Secrets | 57 | 61 |
| C | 63 | 67 |
| H | 69 | 73 |
| M | 75 | 79 |
| L | 81 | 85 |
| Sig | 86 | 90 |
| Disk Usage | 100 | 104 |
| Content Size | 116 | 120 |
| Scanned | 132 | 136 |

At 60 columns the visible columns are `●`, ID, Name and Update. At 70 they are
`●`, ID, Name, Update, Secrets and C. At these widths the columns that give way
are required ones, which Rule 116 allows: a whole column goes, never a
truncated value.

## The Containers table and the activity column — `internal/ui/containers` (DEVDESK-152, A-14)

The `:containers` table is on the activity column (Rule 125; the router side is
in `app-shell.md`).

**Terminal blurred (DEVDESK-210).** The two-second tick keeps re-arming itself
but skips `fetchContainers` and `fetchMetrics` while `Model.unfocused` is set
(from `shared.TerminalFocusMsg`). On the focus edge (`Resume`) `setTerminalFocus`
reads both once, with no new tick, since the chain never stopped.

- **The state comes from docker's `Status` text.** `docker.ParseStatus`
  (`internal/docker/status.go`) reads the `Status` column of `ps` once, into
  `Container.Facts` (`ExitCode`/`HasExitCode`, `Age`/`HasAge`). The exit code is
  exact. The age is a **lower bound**: docker rounds ("2 weeks" is 14 days, "About
  an hour" is 1 hr). The restart count is **not available**. Health suffixes
  and podman's trailing "ago" are tolerated. Text it does not recognise yields
  the zero value: no code, no age. `docker inspect` was rejected (D106).
- **Column 0 is `datatable.IndicatorColumn`** (`columnIndicator`), same shape
  and widths as before. `observedState` (`activity.go`) maps the engine state:
  `running` rests on `IconContainer` (role `IconRoleContainer`, theme key
  `icon_container`, default `ColorText`); `restarting` `◕`; `paused` `◐`;
  `created` and any unknown state `◌`; `exited`/`stopped` with code 0 `⊖`;
  `exited`/`stopped` with code ≠ 0 or **unknown**, and `dead`, `⊗`. Podman's
  `stopped` reads like `exited`; other podman states fall to `◌`.
- **An action in flight shows `●`/`○`** (stop, restart, pause, resume, delete,
  through `startAction`), and it wins over the observed state. The row shows
  its observed state again at the next list. `Model.WantsBlink()` is true
  while a row is busy or while the footer shows the load dot: a refresh or a
  `restarting` row never blinks. The blink phase comes from `jobs.FrameMsg`.
  The first load (no rows yet) shows the shared dot in the footer; `ctrl+r`
  with rows on screen stays silent, so the footer does not flicker on every
  refresh. The view holds no `spinner.Model` (DEVDESK-371).
- **The selected row is red only for `Failed`**, so a clean exit is not red under
  the cursor.
- **Footer.** `status()` shows the load first; otherwise the selected row's
  detail (`selectedDetail`, `observedText`): "Running since 3 hr ago" (no
  glyph; "Running for less than a minute" under a minute), "Restarting — last
  exit code 1", "Paused", "Created, never started", "Exited with code 137 — 5
  min ago" ("— just now" under a minute), "Exited cleanly (code 0)", "Exited
  (exit code unknown)", "Dead — the engine could not stop it cleanly". Any other
  state is named as reported. `progressLine()` (ex `actionLine`) follows:
  "Pruning containers…", "N actions running…", or the single label unless the
  selected row already says it. A failed action stays a footer `Error`.
- **Help (`?`)** shows `indicator.Legend(indicator.ContainersLegend...)`; the busy
  entry reads "an action (stop, restart, pause, resume, delete) is running".
- The state filters `r p s t z` are unchanged.

## Verified pulls — `internal/imagepull` (§3.82)

**Every pull goes through `imagepull.Pull`** — the registry browser's `G`, an
update's `G`, an agent's `image_pull_start`. `TestNoPullBypassesTheSignatureCheck`
(`internal/ui/oci_resources`) walks `internal/` and fails on any other file
naming `docker.PullImageContext`: a second path would be a pull nobody
verified. The engine's pull survives in the OCI view only as the seam
`pullImageContext`, handed to `imagepull.Deps` by `pullDeps` and called nowhere.

The sequence, in `pull.go`:

1. `scan.image_verification: off` pulls the tag, as before §3.82.
2. `~/.devdesk/trust.yaml` is read on every pull. A file that does not load
   **refuses** the pull, naming the error: an unreadable policy is not an empty
   one, and falling back to the built-in rules would drop the user's in silence.
3. The tag is resolved to a digest (`oci.ManifestDigest`, the index digest
   cosign signs). A registry that will not give it is a Failed verdict decided
   by the rule that would have applied — a user rule refuses, anything else
   pulls the tag and warns.
4. `trust.Evaluate` on that digest, with the local image of the same repository
   (`RepoDigests`) for continuity. `Block` is an `imagepull.BlockedError`,
   whose message is the reason, naming the rule.
5. `pull repo@digest`, then `docker.TagImage` back to the name asked for.
   Pulling the tag instead would let it move between the check and the pull —
   the attack. Measured on Docker and Podman; under Docker's containerd store
   `repo@sha256:…` also shows in `RepoTags`, `docker images` lists one row.

What the views do with it: a refusal is a footer **Error** naming the rule, and
the `:jobs` detail an agent reads is the same sentence; a Warn goes ahead and
says why after the pull. The Images tab header shows `Signatures: off`, or
`trust.yaml invalid` — read once when the view starts (`checkTrustPolicyCmd`).

**The `Sig` column** of the Images tab (`signatures.go`, the shared
`internal/ui/sigcol`) is each local image's verdict on the content on disk:
`imagepull.LocalDigest` picks the `RepoDigests` entry of the image's own
repository, and `imagepull.Check` asks about that digest against the rules
alone — continuity would compare the image with itself. An image with no such
digest was built or loaded here (a hammer, as in Update). Checked in the
background on `ImagesListMsg`, four at a time; `sigAsked` keeps the digest each
image was asked about, so an unchanged list asks nothing, a pull that changes
the digest asks again, a verdict about a former digest is dropped, and a Failed
one is retried after 30 minutes. An image no rule covers is decided without
cosign, and one DevDesk pulled comes from the verdict cache. Nothing acts on
the column.

**`run` never pulls**: `buildLaunchArgs` and `VerifyEntrypoint` pass
`--pull=never` (a missing image exits 125, both engines). An image reaches the
machine through a verified pull or not at all. DevDesk's own tool images —
Trivy, cosign, the netdiag image — are pulled by their `run` and are **not**
verified: a declared exception.

## Network Diagnostics View

`internal/ui/netdiag/` — four tabs (Diagnostics, Ports, Interfaces, Forward; the Path tab went in DEVDESK-471, the trace lives under Diagnostics):
- **Diagnostics tab** (`model.go`): target, port and resolver, then
  `internal/netcheck`'s pipeline — resolve, route, reach, proxy, connect, TLS, HTTP —
  chained one stage per message so the footer can name the question being asked.
  The seven tool checkboxes are gone (§3.33): the checks follow from the target
  and from what has already failed.
  **The route stage** (`netcheck/stage_route.go`, DEVDESK-474, D151) sorts each
  lookup error with `nettrace.IsNoRoute`: a proven absence of route is a finding,
  any other failure proves nothing (D20). Only the first 8 resolved addresses
  (`maxRoutedAddresses`) are looked up. The verdict:
  - every address proven without a route: **Fail** (`<host> has no local route`);
  - none routed and at least one undetermined: **Unknown**;
  - some routed and at least one proven without a route: **Warn**,
    `partial-route`; only the proven addresses are counted, and undetermined ones
    are added as `route undetermined for N`;
  - some routed, none proven, at least one undetermined: **OK**,
    `route-undetermined`.

  The system's error text (in the machine's language) appears only in the facts
  `No route` and `Route undetermined`, never in the summary (Rule 129). In the
  Fail case the results header and the MCP verdict read FAIL, where they read
  UNKNOWN before.

  The stage also keeps its readings on the check (DEVDESK-473): `Check.Routes`
  holds one `RouteLookup{IP, Hop, Err}` per address looked up, in the order
  asked, with the lookup's own error untouched (so `nettrace.IsNoRoute` still
  applies to it), and `Results.RouteLookups()` returns a copy of them, nil when
  the stage did not run (skipped or cancelled placeholders carry none). It is
  data for a later reader, so the path trace can reuse the reading instead of
  asking the routing table again; it is never shown and never serialized (the
  MCP tool maps a `Check` to its own DTO). Verdict, summary, reason and facts
  are unchanged. The path trace reuses these readings; see "The route is read
  once" under the trace sub-level.

  Real coverage is Linux and WSL2 only. `go-netroute` returns a
  `windows.Errno` that `IsNoRoute` does not recognize on Windows, and an error
  with no errno on macOS: both end as `undetermined`, which is the safe side.
  Two limits to know: if `go-netroute` stopped wrapping the netlink errno with
  `%w`, Linux would fall back to `undetermined` silently, with no failing
  build; and `prohibit` routes (`EACCES`) stay `undetermined`. Windows and macOS
  support is a follow-up story, to be created (see D151 in `docs/backlog.md`).
- **Ports tab** (`ports_model.go`): the machine's own socket table, re-read
  every `ports_refresh_interval`, filtered by protocol, state and text. `K`
  terminates a process, after a confirmation. It refuses DevDesk itself, its
  direct parent, PID 1 and, on Windows, PIDs 0 and 4, and it acts on the
  process the confirmation named, captured when it opened (DEVDESK-708,
  DEVDESK-714), and it checks at the signal that the PID still belongs to the
  process that was listed; `K` is greyed on a row whose identity could not be
  read (DEVDESK-709). It runs **no container** — see `internal/ports` below. Column 0 is the activity column (DEVDESK-150, below).
- **Interfaces tab** (`interfaces_model.go`): this machine's network interfaces
  — name, state, MTU, MAC, RX/TX error counters, addresses — in a `datatable`.
  It runs **no container**; see `internal/netiface` below. It was the Topology
  tab, and §3.44 is why three of its four sections are gone rather than
  translated.
- **Forward tab** (`forward_model.go`, `forward_form.go`): the port
  redirections, `N` to open one, `space` to pause or resume it and `K` to delete
  it. It holds **no listener** — see `internal/forward` below.
- **Trace sub-level of Diagnostics** (`path_model.go`, `path_view.go`,
  `path_run.go`, `path_target.go`, `path_from_route.go`, `path_steps.go`,
  `trace_form.go`, `trace_level.go`; DEVDESK-328, DEVDESK-329, DEVDESK-470,
  DEVDESK-471): the hop-by-hop path to a target, from `internal/nettrace`
  (below). It was the fifth tab; it is now `StateTrace` of the Diagnostics tab
  (`tabCount = 4`), described under "The Trace Path button, `H` and the trace
  sub-level". It runs **no container**; since DEVDESK-330 it can name a running
  Docker Engine container as the source of the column (below), but the probes
  still leave from this machine. Three states: `pathReady` (target known,
  nothing sent), `pathRunning` (hops arriving) and `pathDone` (hops kept on
  screen). The **body is always the hop table** (`PathModel.view()` returns
  `table.View()`, Rule 139), in every state: columns Hop, Step, Address, RTT,
  with a silent hop dim and never red. **The column starts on this machine**
  (DEVDESK-329): three local rows, then the hops. `source` reads `this machine`;
  `interface` reads `<name> (<source address>)`; `gateway` reads the gateway
  address. The local rows have an empty Hop and RTT (they asked nobody; `-`
  means "no answer") and the default text colour. `pathSteps` (`path_steps.go`)
  is the only place that orders the rows; the table item is a `pathStep` (kind
  source, interface, gateway or hop; DEVDESK-330 adds container, network and
  trace, below).
  Nothing is shown before the route is known, so a resolution error leaves 0
  rows. There is **no target field**: the target is the Diagnostics form's (or
  the results' host for `H`), typed once. The sub-level's footer is 3 lines in
  every state (tab row, blank line, info line) and keeps its own
  `FooterMessage`, returned by `activeFooter`, so `WantsBlink()` follows a
  running trace. `Model.Running()` is true while a trace runs, so the router
  does not rebuild the view mid-trace.

  Keys: see "The Trace Path button, `H` and the trace sub-level" below (`enter`
  starts or traces again, `esc` cancels then goes back, `ctrl+o` opens *Trace
  from*, `tab` switches tab, the navigation keys move in the table). `ctrl+o`
  while a trace runs is ignored; where the trace is unsupported it is greyed and
  warns with the platform reason (`sourceChoosable`, the one answer the header
  and the handler both read). While the selector is open it holds every key:
  `up`, `down`, `pgup`, `pgdown`, `home` and `end` move, `enter` chooses, `esc`
  closes, and every other key is swallowed, `tab` and `shift+tab` included
  (`netdiag.Model.Update` hands the key to the Path model before the tab
  switch). A paste is dropped. `Model.InEditMode()` is true while it is open,
  so `q` and `:` reach the view and do not quit or open the palette.

**Ports tab, terminal blurred (DEVDESK-210).** `PortsModel.unfocused` is set by
`shared.TerminalFocusMsg`, which `netdiag.Model.Update` forwards to the ports
poll whichever tab shows (the tick runs on every tab). The tick chain is kept:
`handleTick` re-arms `portsTickCmd` and skips `fetchPortsCmd` while `unfocused`.
On the focus edge (`Resume`) it reads once, with no new tick, so the chain is
not doubled. `unfocused` is separate from `paused`, the user's own pause (shown
in the bar): a user pause is never lifted by a focus, and the resume read is
skipped while `paused`.

**Ports tab, activity column (DEVDESK-150, Rule 125).** Column 0 of the socket
table is `datatable.IndicatorColumn` (untitled, two cells wide), the same
mechanism as `oci/images` (DEVDESK-146). The table has no registry run: `K` is
held in `datatable`'s `busy` map, keyed by PID, and `datatable` writes the
precedence "busy means `indicator.Running`" once, so the column's own state is
always `Idle` and an idle row shows an empty cell (a socket has no rest icon).

- **`K` running = lit dot.** Every socket of the PID shows `●`/`○`, as the
  spinner did before. The blink phase comes from `jobs.FrameMsg`, handled in
  `Model.Update` (`SetBlinkPhase`, nothing rebuilt); `Model.WantsBlink()` is true
  while any row is busy, whatever the active tab. The Diagnostics and Interfaces
  footers show the same shared dot while a run or an interfaces read is in
  flight (DEVDESK-373), driven by the same `FrameMsg`: `WantsBlink()` is also
  true while the active tab's footer is on `Activity`. The `netdiag` package
  holds no `spinner.Model` and no `spinner.TickMsg` handler.
- **The line spinner is gone**: `StatusColumn`, `portsSpinnerTickMsg`,
  `portsSpinnerCmd` and `handleSpinnerTick` are deleted. **State keeps its
  text** (`LISTEN`), dimmed like every busy row. This reverses the letter of
  §3.22 (the State cell used to be replaced by the spinner) and keeps its intent:
  the dim says the value is about to change.
- **The footer names the step**: for the selected busy row, `● Stopping sshd (812)`,
  `● Waiting for sshd (812) to exit` or `● Killing sshd (812)` (the last one is
  also the Windows text) (`PortsModel.rowDetail`, from `components.DescribeRow`),
  before the paused or stale text. At 60 columns, where PID and Process are dropped, this is
  where the process name is readable.
- **Failure stays in the footer only.** A refused or failed kill is an `Error`
  (`killFailureMessage`) and the row returns to rest: no `⊗`, nothing is
  remembered, because no registry kind exists for it. On Windows `K` stays lit
  on a busy row (Rule 130); pressing it again warns `Already killing PID N`. On
  Linux and macOS `K` is greyed on every row while a two-step stop is in
  progress: an exception, see « Two-step stop » under « The socket table ».
- **A periodic refresh never blinks**: `SetItems` leaves `busy` alone, so a
  refresh with nothing busy leaves column 0 empty and `WantsBlink()` false; one
  during a kill keeps the dots and the cursor.
- **A failed network test is not a row operation.** The Diagnostics table keeps
  its result in the Verdict column; no `⊗` was added (open question 4 of the
  activity-indicator stories, option A). The Diagnostics and Interfaces tables
  have no `Indicator`; neither does the trace's hop table (DEVDESK-328): a
  trace is one run, not a per-row operation, and its running state is the
  footer's shared dot. The Path files `path_model.go`, `path_target.go` and
  `path_view.go` (and, since DEVDESK-329, `path_steps.go`; since DEVDESK-330,
  `path_source.go`, `path_container.go`, `path_source_picker.go` and
  `path_source_flow.go`) are in the activity guard's scanned sources for
  `command.ViewNetdiag`; `path_run.go`, which holds the I/O, is not.
- **Files.** `ports_activity.go` (`ActivityTables()` reports `netdiag/ports`,
  `WantsBlink()`); `ports_commands.go` holds the I/O commands (`fetchPortsCmd`,
  `portsTickCmd`, `terminateCmd`, `stopProbeCmd`, `killProcessCmd(kill, target,
  gen)`, `requestPortsRefresh`, whose
  `portsRefreshMsg` makes `Update` read the table once, outside the tick chain),
  `processOps` (`protection`, `confirm`, `terminate`, `probe`, `kill`,
  `windows`) and
  `realProcessOps` (see « Seams and the signal guard » below), and `NewPortsDataMsg`, which the activity
  guard's footer seed uses to hand rows to the view without reading the socket
  table. `ports_commands.go` is not scanned by the guard (its refresh tick is
  data, not a row animation).
- **Widths (Rule 116).** Required columns give way rightmost first. Thresholds,
  as terminal width, with the column added (they were 99, 87, 78 and 50 before):

  | Columns shown | From width |
  |---|---|
  | all | 103 |
  | without Process | 91 |
  | without PID and Process | 82 |
  | activity, Proto, State, Local Address | 54 |

  At 60 columns the last set shows and `RenderedWidth()` is 58.

**Timing (§3.66).** `netcheck.Check` carries a `Duration time.Duration`
alongside `Summary`/`Facts` — zero means untimed. `stage_connect.go` and
`stage_tls.go` fill it from the dial and the handshake respectively;
`stage_http.go` fills it from an `httptrace.ClientTrace` attached in
`env.go`'s `Head`, which also gives `HTTPResult` a DNS/connect/TLS/TTFB
breakdown surfaced as `Facts` on the `HTTP` row. Nothing keeps a history
across runs — that stays an open question.

That breakdown also drives a waterfall: `Check.Phases` is a set of named,
non-overlapping spans that sum exactly to `Duration` (nil on checks with
nothing to break down — TCP, TLS). `stage_http.go`'s `httpPhases` derives
**Wait** and **Content** rather than charting TTFB directly, because TTFB is
measured from the start of the request and already contains DNS+connect+TLS —
charting it as a fifth independent share would double-count that overlap.
`internal/ui/netdiag/view.go`'s `phaseLines` renders one proportional bar per
phase, reusing the load-gauge machinery (`theme.GaugeFill`, `GaugeTrack`,
`GaugeFillWidth`, `GaugeTrackStyle`) with a new neutral fill colour, `theme.TimingFillStyle` —
a phase taking most of the time is a fact to read, not a severity to spot.

**Opened prefilled, from elsewhere (§3.66).** `netdiag.NewWithTarget` builds a
view with the form filled in but not yet run; `netdiag.OpenRequestMsg` is
what a producer elsewhere sends to ask the router for one (mirroring
`uiviewer.OpenRequestMsg`), and `netdiag.BackToOriginMsg` is how `esc` gets
back out, via `Model.OriginView` — empty when netdiag was opened directly
from the command line, in which case `esc` behaves exactly as it always has.
`status`'s `H` (`keymap.Diagnose`, `internal/ui/status/diagnostics.go`) is
the first producer: it resolves the selected monitor's target and
auto-runs the pipeline when the port is certain (http/https/ssl), or lands on
the prefilled form when it is a guess (icmp/dns).

`H` has a **second producer, inside netdiag** (DEVDESK-328, DEVDESK-471,
`path_from_route.go`): on the Diagnostics results, with the **Local route** row
(`netcheck.CheckRoute`) selected, it opens the trace sub-level (`StateTrace`) on
the results' host, in its **ready** state (`pathReady`: target known, nothing
sent). It **never launches**: nothing is resolved or sent until `enter` in the
sub-level. Only the host is carried over, not the port (the route check does not
depend on one). `pathFromRoute()` is the one availability read by the header
(the entry `H Trace the path` is greyed, Rule 130) and by the handler, which
refuses with the same reason at the footer. The reasons come in this order: the
platform cannot trace (`traceable()`), the selected row is not the route row
(`Select the Local route row to trace its path`), the target is one the trace
refuses (`parsePathTarget`, for example an IPv6 literal). If a trace is already
running, `H` changes nothing and warns `reasonPathBusy`.

**The Trace Path button, `H` and the trace sub-level (DEVDESK-470,
DEVDESK-471; `trace_form.go`, `trace_level.go`).** Both entry points call
`Model.openTrace(origin, target)`: `traceOrigin` records where `esc` leads back
to (`StateInput` for the button, `StateResults` for `H`), the state becomes
`StateTrace` on the Diagnostics tab, and `PathModel.ready(target)` puts the
shared `m.pathModel` in `pathReady` (source reset to this machine, previous run
forgotten). The button then launches at once (`PathModel.startFromForm`: `ready`,
then `launch`; the Cmd does the I/O, `Update` mutates, Rule 110); `H` does not. A
trace already running is refused with `reasonPathBusy` at the footer, and the
button stays lit (Rule 130). The body is `pathModel.view()`, the table alone
(Rule 139); the title reads `Network › Path › <host>` (host cut with an
ellipsis), the header is `pathModel.headerInfo()`, and the footer is 3 lines in
every state: the Diagnostics filter bar is not drawn in the sub-level
(`GetFooterHeight`, `RenderFooter`, Rule 124). The footer message is the Path
model's (`activeFooter`). Hops keep landing while another tab is on screen, and
coming back finds the sub-level.

**The route is read once (DEVDESK-473, D155; `path_known_route.go`).**
Diagnostics keeps its route lookups on the `Local route` check (`Check.Routes`);
the trace's interface and gateway rows answer from them instead of asking the
routing table a second time. `Model.openTrace` is the only writer of
`PathModel.known`: it assigns `knownRoutesOf(m.resultsHost, m.results)`, a copy
of the lookups (never shared with `m.results`), on every opening, empty when
the form opens the trace (no results). `Model.resultsHost` is the host of the
run behind the results: `StartRun` sets it (so `ctrl+r` too), `resetToForm`
clears it with the results. `PathModel.launch` builds the run from
`pm.runEngine(tg)`, the engine whose `route` function is the
`pathKnownRoutes.routeFirst` wrapper; the `Cmd` receives only values and never
reads `pm` (Rule 110). A reading is reused only when the snapshot's host equals
the target's host, the traced address (compared after `Unmap`) is one the route
stage looked up, the lookup did not end on `context.Canceled` or
`context.DeadlineExceeded` (it says nothing about the table), and the source is
this machine (a container source reads its own route on every launch). The
lookup's own error is returned untouched, so `no route` and `undetermined`
come from the same `nettrace.IsNoRoute` on both sides. *Trace again* and a
return to the results then `H` reuse the same reading; `ctrl+r`, a new run,
and a trip back to the form (`esc`, `N`) read again, because the results the
reading belonged to are gone. The `Local route` row itself is unchanged.

*Which address (D155 point 5, refining D144).* The trace still resolves the name
itself (`LookupNetIP(ctx, "ip4", host)`) and traces an address that resolution
returned now, never the Diagnostics one and never one from the DNS Server field.
When the source is this machine, `runEngine` also wraps `resolve`:
`pathKnownRoutes.prefer` moves the first IPv4 address of that answer which the
snapshot holds a *successful* reading of (same host test, `Unmap`, and a lookup
with `Err == nil`) to the front, the other addresses keeping their order, and
`resolvePathTarget` takes `addrs[0]` as before. A lookup that ended in an error
(`no route`, `undetermined`, a cancellation, a deadline, any other) is never
preferred (user decision, confirmed 2026-11-16). So on a round-robin name the
trace takes an address Diagnostics already read with success, whatever order the
resolver gave this time. With no results, for another host, from a container, or
with no preferable address in common, the answer is untouched and the first IPv4
address is traced (that one costs one more reading, unless it is the only
address: its failed reading is then reused as it is). IPv6 addresses are never
preferred. The reordering runs inside the `Cmd` on a copy of the snapshot taken
at launch (Rule 110).

`PathModel` has **no target field** (DEVDESK-471): `pathReady` is its zero value,
a fresh model is ready and inert, and `ready(target)` is the only way a target
gets in. Letters, a paste and `esc` do nothing in it; `enter` with no target
launches nothing.

Keys (`handleKeyTrace`: `esc` is the sub-level's, the rest goes to
`PathModel.handleKey`, which ignores letters; while a trace runs only the
table's navigation passes): `enter` launches from the ready state and launches
again after a finished or canceled trace (*Trace again*, replacing D152 point
2); `ctrl+o` opens *Trace from* (declared exception, surface `netdiag/trace`);
choosing a source goes back to `pathReady` with the target kept and sends
nothing; `esc` cancels a running trace, then goes back to `traceOrigin` with
fields, focus, filter and results untouched (no `resetToForm`); navigation moves
the table. An open selector holds every key, `tab` included. `tab` and
`shift+tab` otherwise still switch tabs, so the sub-level survives a visit to
another tab. `traceShortcuts()` is the one constructor of the sub-level's
shortcuts: ready and done share one key set (Rule 130), the selector and a
running trace replace the list. `InEditMode()` is true in the sub-level only
while the selector is open.

The Path tab is gone (DEVDESK-471, P2-02): `tabPath` no longer exists and the
tab bar has four labels.

**Files.** `trace_form.go` holds what the form does: `traceable()` (the one
availability, read by the button, the header and the handler, Rule 130),
`traceFromForm()` (the `enter` handler), the target validation and
`traceUnusedNotice`. `trace_level.go` holds the sub-level: `startFromForm`,
`ready`, `openTrace`, `inTrace`, `handleKeyTrace`, `escFromTrace` (with
`restoreFormFocus`), `traceTitle` and `traceShortcuts`.

**Order of refusals in `traceFromForm`.** An unsupported platform
(`launchable()`, macOS, Windows, WSL1) comes first: the button is greyed and
the reason is in the footer. Then a trace already running (`reasonPathBusy`).
Then the target.

**Target validation (Rule 140).** The target goes through `parsePathTarget`,
the trace's own parser, so an empty, invalid, `host:port` or IPv6 value gets
the trace's sentence (first letter capitalized). The
failure is carried twice: the Target field is underlined
(`traceMarks`, `InvalidMarks.Sync`, from `Update()` only) and the error sits
in the footer at level Error, derived from `traceValidation.Status()` in
`statusLine()`. Nothing is invalid before the first refused `enter` on the
button; after that, every edit of the Target revalidates (`revalidateTrace`).
`enter` on a text field still runs the diagnostics and does not validate
the trace target.

**Port and DNS Server.** The trace probes the path to a host, so both fields
are ignored. When either holds a non-blank value, `traceUnusedNotice` posts an
Info notice on the Path model's footer (`Port is not used by the path trace`,
`DNS Server is not used by the path trace`, or both). It is posted after
`startFromForm`, because launching clears that footer. The values stay in the
form.

**One model, one place.** The sub-level has no model of its own: it drives
`m.pathModel`, the only trace model now that the Path tab is gone. Starting from
the form or from `H` overwrites its target (`ready`) and forgets its previous
run; going back to the form or the results does not clear it.

**What is configurable, and what is not.** `internal/netcheck` held its timeouts
as constants with a comment saying they would become settings when somebody
asked; `network:` is what they became.

| Setting | Replaces |
|---|---|
| `check_timeout` | five per-stage constants — 5 s for DNS and the dial, 8 s for TLS and HTTP, 4 s for the ping |
| `ping_count` | `pingCount` |
| `cert_expiry_warn_days` | `expiryWarnWindow` |
| `ports_refresh_interval` | the 2 s tick |

**One timeout replaces five**, and the default is the old *maximum* so nothing
that answers today starts failing. The 5 / 5 / 8 / 8 / 4 split was never argued
anywhere — five rows for one idea, and each of the five a separate guess. The
cost is stated rather than discovered: an unreachable host now spends 8 s on DNS
instead of 5, which the staged progress line makes legible rather than a hang.

`traceroute_max_hops` was a sixth setting and went with the trace it bounded
(§3.47). It stays removed: the trace is back in the process (DEVDESK-327, see
[The path trace](#the-path-trace--internalnettrace)), but `nettrace.Options.MaxHops`
is not wired to any setting, and the trace (DEVDESK-328) still runs with
the engine's defaults (`nettrace.Options{}`).

Two things stay hardcoded, each for a reason:

- **`MinVersion: VersionTLS10`** — the handshake is *probing*, not securing.
  Reporting an old version is the point; a setting could only make the tool
  blind to what it exists to find.
- **`InsecureSkipVerify`** — the TLS stage verifies the chain itself so it can
  say *which* part failed. A setting here would collapse four checks into one
  error string.

`netcheck.Settings` travels **beside** `Env`, not on it: `Env` is the seam to
the network, while `PingCount` and `ExpiryWarnWindow` are read by stages and
never by a network call — putting a preference behind the seam would make every
fake answer for one. `Settings.Normalized()` fills in anything non-positive at
every entry point, so a hand-edited `0` becomes the default rather than a dial
with no deadline. The view calls `checkSettings()` per run rather than capturing
at construction, so a run already in flight and the config cannot disagree
halfway down the pipeline.

Every probe here answers from the DevDesk process. The route trace was the one
exception and §3.47 removed it: `--network host` on Docker Desktop is the VM's
namespace, so it traced a path from somewhere else. It is back, in the process,
since DEVDESK-327 (`internal/nettrace`, below). Nothing shells out.

**The `route` stage answers the question no other check can** (§3.44): when a
VPN captures the default route, a host that is plainly reachable elsewhere is
unreachable here, and every other row reports the *symptom* — no ICMP reply, no
TCP connect — while none reports the cause. `Env.Route(ctx, ip)` returns a
`RouteHop{Interface, Source, Gateway}`, and the summary names the **interface**
because that is what the user recognises: "Traffic leaves through ProtonVPN"
answers the question, `10.2.0.1` needs a second lookup to mean anything.

`github.com/libp2p/go-netroute` is what makes it one implementation instead of
three — `GetBestRoute2` on Windows, an `RTM_GETROUTE` netlink query on Linux, the
routing socket on the BSDs — with no privilege anywhere and ~2 ms per lookup,
measured. It depends only on `x/net` and `x/sys`, both already in the graph.

Four decisions, each with a test:

- **It gates nothing**, for `reach`'s reason: a machine whose routing table
  cannot be read still has a perfectly answerable question about the port.
- **A lookup that fails is `Unknown`, never `Fail`.** "Could not determine the
  route" is not "there is no route", and rendering the first as the second is
  D20. The platform error is also **localised** — Windows returns
  `ERROR_NETWORK_UNREACHABLE` in the machine's own language — so it goes in a
  fact and never in the summary (Rule 129).
- **One family without a route is a `Warn` with its count.** That is the IPv6
  case, and it is worth reporting: a client that prefers IPv6 hangs before
  falling back.
- **It takes its addresses from the `resolve` check's facts**, capped at
  `maxRoutedAddresses`, rather than resolving again — on a round-robin name the
  two could diverge and the route would describe an address no other check in the
  run ever touched. `TestTheRouteStageReadsWhatTheResolveStageWrote` pins the
  coupling, which is otherwise the kind that breaks in silence.

**The direct path and the Proxy row (DEVDESK-582).** The Diagnostics measure the
direct path from this machine, never the one through a proxy (decision Q1). The
HEAD (`systemEnv.Head`) builds its own `http.Transport` with no `Proxy` field, so
it goes direct. `Head` and `Handshake` are the two entries listed as permanent
exceptions of the proxy guard (below). Nothing is ever sent to a proxy.

What the checks do not measure, the Proxy row reports. `StageProxy` sits between
ICMP echo and TCP connect (check `proxy`, titled `Proxy`). It has no dependency, so
the row is present even when DNS fails, which is the usual case behind a mandatory
proxy. It reads the environment through `Env.ProxyFor`, which is
`netproxy.Describe`, and asks about both schemes, because the HEAD's scheme depends
on a handshake that runs later: `https://host:port` (`HTTPS_PROXY`) and
`http://host:port` (`HTTP_PROXY`). The facts `HTTPS` and `HTTP` give each answer. The
two answers merge by priority: Invalid, then Proxied, then Excluded, then Loopback,
then None.

| Merged answer | Verdict | Reason | Summary |
|---|---|---|---|
| no variable for either scheme | N/A | `proxy-none` | `None` |
| a proxy for at least one scheme | INFO | `proxy-configured` | `Configured for this target: http://proxy.corp:3128`, with `(https only)` or `(http only)`, or both proxies when they differ |
| `NO_PROXY` names the target | N/A | `proxy-excluded` | `Excluded by NO_PROXY` |
| a loopback target | N/A | `proxy-loopback` | `Not used for a loopback target` |
| a value that is not a usable proxy URL | WARN | `proxy-invalid` | `Invalid HTTPS_PROXY value` (the variable's name, in the case it was read in) |
| both values invalid | WARN | `proxy-invalid` | `Invalid HTTPS_PROXY and HTTP_PROXY values` (`invalidSummary`) |

The `None` row is always there, by choice: a stage must produce the same checks
whatever it finds (`TestEveryStageProducesWhatItDeclares`), the package tells "not
looked" from "found nothing", and the MCP output keeps a stable list.

`INFO` is a new verdict: an observation with no objective to meet. `Summarize`
and the overall verdict ignore it, as they ignore N/A. The `p` filter (problems
only) hides it. N/A rows are greyed.

When the direct dial of the TCP connect fails and at least one scheme is Proxied,
the conclusion changes (Reason `direct-failed-proxy-configured`): `Port 443 does
not accept direct connections; a proxy is configured for this target`, with the
fact `Proxy`. A closed direct path is expected behind a mandatory proxy, and "does
not accept connections" would mislead. `runConnect` reads `Check.Proxy` of the
Proxy row, never its Summary.

**DNS behind a mandatory proxy (DEVDESK-596).** When the direct resolution of a
named target fails (the DNS row is Fail) and the Proxy row says a proxy applies to
at least one scheme, the DNS row reads `<host> does not resolve directly; a proxy is
configured for this target`. The verdict stays Fail and the Reason is
`direct-resolve-failed-proxy-configured`. The facts are `Resolver`, `Proxy` (the
masked `scheme://host:port` of `netproxy.Display`) and `Error`, in that order. "Does
not resolve" and "resolves to no address" share the Summary; the `Error` fact tells
them apart. Without an applicable proxy (no variable, `NO_PROXY`, loopback, invalid
value) the row is unchanged. When the resolver itself misbehaves (`server
misbehaving`), the Reason, the Summary and the guidance stay as they were, because
the resolver is at fault; only a `Proxy` fact is added after `Resolver`.

The Proxy stage keeps its position (resolve, route, icmp, proxy, tcp, tls, http), so
`Steps()`, the row order and the MCP output do not change. Instead, after the Proxy
stage, the `stage.conclude`/`amends` hook calls `concludeResolveBehindProxy`, which
rewrites the DNS row in place. It reads `Check.Proxy`, the field the TCP conclusion
reads, never the environment and never a Summary. The `prior` slice of `RunStep` is
never modified: the rewrite builds a new slice of facts. While a run is in progress,
the DNS row briefly shows the generic conclusion between the end of the DNS phase
and the end of the Proxy stage; the gap is instantaneous. Over MCP, the `resolve`
check carries the new Reason in the existing `reason` field, with no new field.

The Explain guidance says what it means and what to do. Means: every check below
needs an address, so none of them ran; clients that go through the configured proxy
usually hand it the name and let it resolve it, so behind a mandatory proxy a name
this machine cannot resolve is expected and says nothing about whether the target
exists or is up. Do: check from a client that uses the proxy (for example an HTTPS
monitor in the Status view), or add the target to `NO_PROXY` if it must be reached
directly, then check the spelling or try another resolver.

A proxy leaves `netproxy` only as `netproxy.Display`: `scheme://host:port`, never
userinfo, path, query or IPv6 zone. An invalid value is named by its variable and
never quoted. `Check.Proxy` carries that string to the connect stage and to the
DNS conclusion (above), is never shown on its own and is never serialized, so
`net_check` over MCP does not carry that field. The masked proxy
(`scheme://host:port`) still goes out through the facts `HTTPS`, `HTTP` and
`Proxy`, in the UI and over MCP.

Limits:

- **Validity is stricter than Go's.** Go ignores a value it cannot parse, so
  `FromEnvironment` cannot tell it from "no proxy". `netproxy` also refuses a value
  with an explicit scheme that fails to parse, an empty or non-printable host, and
  a mis-encoded credential (an unescaped `/`, `?` or `#` before the `@`). Go
  accepts some of these: `http://[::1` is read as `http://http:80`. The row says
  Invalid where Go would silently go on.
- **Excluded and Loopback are deduced.** `Describe` does not recompute
  `NO_PROXY`. A nil answer with a valid variable means Loopback if the host is
  `localhost` or a loopback address, otherwise Excluded. The subprocess test of
  `netproxy` pins this against `http.ProxyFromEnvironment`.
- **Cache against variable.** `http.ProxyFromEnvironment` reads the environment
  once per process and caches it. `Describe` reads the variable's name and
  validity directly. A variable changed after the first lookup can therefore show
  a name the cached answer does not use.
- **CGI.** When `REQUEST_METHOD` is set, Go refuses `HTTP_PROXY` (`httpproxy`: `refusing to use HTTP_PROXY value in CGI environment`), so the row says
  `Invalid HTTP_PROXY value`.

## The proxy entry point — `internal/netproxy` (DEVDESK-581)

### Why it exists

The HTTPS monitor followed `HTTPS_PROXY` and `NO_PROXY` while the SSL monitor
dialed the target directly with `tls.DialWithDialer`: behind a corporate proxy
the HTTP check was green and the SSL check red for the same host. The fix is not
a patch on the SSL check but one place that decides the path of a connection, and
a test that keeps the rest of the module from deciding it elsewhere.

### The entry point

- `netproxy.Selector` (`func(*url.URL) (*url.URL, error)`) says which proxy
  carries a connection to a URL; `nil, nil` is direct. A nil `Selector` behaves
  like `FromEnvironment`. `Selector.ForRequest` adapts it to
  `http.Transport.Proxy`.
- `netproxy.FromEnvironment` is `http.ProxyFromEnvironment` on the URL: the same
  function, and so the same cache, as the HTTPS monitor.
- `netproxy.NewTransport(sel)` is `http.DefaultTransport.Clone()` with `Proxy`
  set from `sel`. It is the only way to build a transport that needs a custom
  selection (`oci.newBlobTransport` wraps its selector to learn which proxy
  addresses it may dial).
- `netproxy.Dialer.DialTLS(ctx, hostport, cfg)` opens a TLS connection the way
  an https request to `hostport` would travel: direct, or by `CONNECT` through
  an `http://` or `https://` proxy (Basic credentials from the proxy URL), then
  the handshake with `cfg`. One deadline, the context's, covers proxy dial,
  `CONNECT` and handshake. The zero `Dialer` follows the environment; its
  fields (`Proxy`, `DialContext`, `ProxyTLSConfig`) are what tests swap.
- `netproxy.Describe(u)` says what the proxy variables mean for one URL, as a
  `Decision`: a `Kind` (`DecisionNone`, the zero value, `DecisionProxied`,
  `DecisionExcluded`, `DecisionLoopback`, `DecisionInvalid`), the `Variable` read
  (`HTTPS_PROXY` or `https_proxy`) and, when proxied, `Proxy`. The answer comes from
  `FromEnvironment`; only the variable's name and validity are read apart. A
  `Decision` carries no credential. The Proxy row of the Diagnostics uses it (see
  above).
- `netproxy.Display(proxy)` renders a proxy URL as `scheme://host:port` and nothing
  else: no userinfo, path, query, fragment or IPv6 zone. It is the only way a proxy
  leaves the package for the screen.
- A failure of the proxy step is a `*netproxy.Error` with a `Step`:
  `StepSelect` (invalid proxy setting), `StepScheme` (socks5), `StepDial`,
  `StepProxyTLS`, `StepConnect` (refused, unreadable or unanswered), plus the
  proxy's `host:port` (never the userinfo) and the `CONNECT` status code. Its
  text is built from the step and the code, never from the proxy's own words. A
  failure of the target itself (handshake, certificate) comes back as it would
  without a proxy.

### The SSL monitor

`status.FetchCertificateChainContext` goes through `Dialer.DialTLS`
(`certDialer`, swapped by tests), so the SSL check, the certificate viewer and
the HTTPS monitor take the same path. When the failure is a `*netproxy.Error`,
`SSLChecker.Check` sets `ComponentStatus.ErrorStage = status.StageProxy` next to
`Error`; the field is internal (the MCP output only changes by the text of
`error`). On the Certificates tab the selected DOWN row's `Error` is shown in the
footer at level Error (`Model.certificateFailure`, derived on every render, no
timer, Rule 128), so a refused `CONNECT` is readable instead of cut off in a
column. The Monitors tab is unchanged. A timed message and the form's validation
status keep priority over it.

### The guard — `internal/netproxy/guard_test.go`

`TestNoHTTPClientBypassesTheProxyEntryPoint` parses every production `.go` file
of the module (go/ast, no type information; test files, `.worktrees`, `.git`,
`vendor`, `testdata`, `docs-site`, `node_modules` and `internal/netproxy` itself
are skipped) and fails on a place that leaves the entry point. The local name of
`net/http` and `crypto/tls` is resolved, aliases included.

| Rule | Reported |
|---|---|
| R1 | an `http.Transport` built by hand: literal, `&literal`, `new(http.Transport)` or `var x http.Transport` |
| R2 | any reference to `http.DefaultTransport` (cloning it keeps its `Proxy` and invites the next line to replace it) |
| R3 | an assignment to a `.Proxy` field, or a `Proxy:` key in a composite literal, in a file that imports `net/http` |
| R4 | a raw TLS dial: `tls.Dial`, `tls.DialWithDialer`, `tls.Client`, `tls.Dialer{}`, `&tls.Dialer{}`, `new(tls.Dialer)` or `var d tls.Dialer` |
| dot-import | a `. "net/http"` or `. "crypto/tls"` import, which hides every selector above; it cannot be excepted |

A client with no `Transport` is compliant: it uses `http.DefaultTransport` and so
the proxy environment. Reports read `file:line: rule (enclosing func)`.

Known exceptions live in `proxyExceptions`, matched by file, enclosing function
and rule, never by line.
`TestEveryProxyExceptionStillMatches` fails on a stale entry, and
`TestNetworkDocListsEveryProxyException` fails when this section does not name
the file and the function of an entry.

| File | Func | Rules | Reason | Until |
|---|---|---|---|---|
| `internal/nexusbench/nexusapi.go` | `newClient` | R2, R3 | the bench is reached directly (`Proxy = nil`): a proxy would receive the bench credentials | permanent |
| `internal/nexusbench/probe.go` | `NewProber` | R2, R3 | the probe goes without a proxy, like the bench client | permanent |
| `internal/netcheck/env.go` | `systemEnv.Head` | R1 | the Diagnostics measure the direct path (decision Q1, DEVDESK-582); the Proxy row says when a proxy applies | permanent |
| `internal/netcheck/env.go` | `systemEnv.Handshake` | R4 | the Diagnostics measure the direct path (decision Q1, DEVDESK-582); the TLS step runs on the same direct path as the TCP step | permanent |
| `internal/forward/proxy.go` | `routeTransport` | R1 | a named route stays direct, like the TCP forwards to the same target (decision Q1) | permanent |

Adding an exception means adding it to `proxyExceptions` and to this table in
the same commit, with a reason and an end.

### What the guard does not see

- Clients built inside third-party dependencies: the GitLab client-go, go-github
  and the Docker SDK build their own transports. They are not covered.
- A composite literal whose type is elided (`[]http.Transport{{...}}`), a local
  variable that shadows `http` or `tls`, reflection, and a wrapper type that
  hides the transport.
- An `http.Transport` held by value and not built from a literal: a struct
  field, or a parameter or result typed `http.Transport`. The guard inspects
  literals, `new`, `var` and `.Proxy` assignments only.
- Raw `net.Dial*` calls are out of scope: TCP forwards, DNS, the UDP of
  `nettrace` and netcheck's `DialTCP` are not HTTP paths.

### Go behaviors to keep in mind

Which variables are read, and how:

- `HTTP_PROXY`, `HTTPS_PROXY` and `NO_PROXY`, each in two casings. The upper
  case name wins over the lower case one, and an empty value counts as not
  defined (`firstNonEmpty`, like Go's `getEnvAny`).
- `ALL_PROXY` and `FTP_PROXY` are not read, by Go or by DevDesk.
- The environment is read once per process (`http.ProxyFromEnvironment` caches
  it; `ContainerForward` does the same with `sync.OnceValue`). A context switch
  reads nothing again: the user restarts DevDesk after changing a variable. So
  `t.Setenv` has no effect after the first call. A test that needs a given
  environment runs the code in a subprocess, or injects a `Selector` (or, for the
  containers, swaps the `proxyForward` seam of `internal/scan`).
- `NO_PROXY` takes a host name (it also covers its subdomains), `.suffix`
  (subdomains only), an IP address, a CIDR, `host:port`, and `*` (everything).
- `localhost` (exactly) and loopback addresses are never proxied by
  `http.ProxyFromEnvironment`, whatever the variables say; `*.localhost` names
  are proxied.
- CGI: when `REQUEST_METHOD` is set Go refuses `HTTP_PROXY` (see the Proxy row
  above). The container forward does not look at it: a value is forwarded as is.
- A proxy that limits `CONNECT` to port 443 answers 403 for another port (LDAPS,
  SMTPS); the footer now says so.
- socks5 proxies: the HTTPS monitor accepts them (`net/http` does), the SSL
  check refuses them with `StepScheme` and no new dependency. The gap is
  deliberate and known.

Who follows these variables after DEVDESK-581 and DEVDESK-582:

- The HTTP/HTTPS monitor, the SSL monitor and the certificate viewer: the entry
  point above (DEVDESK-581).
- The Diagnostics: direct on purpose (decision Q1). The Proxy row says which
  proxy would apply; no check is sent to a proxy (DEVDESK-582).
- The scanner containers: see the next section (DEVDESK-583).
- Image pulls done by the engine: the Docker or Podman daemon pulls, with its own
  proxy configuration (the service environment, Docker Desktop settings, the
  Podman machine). DevDesk does not configure it and does not pass its variables
  to it. DevDesk's own registry calls (`internal/oci`) go through the entry point.
- `git` (clone, sync) is a child process: it inherits the environment and reads
  it with libcurl's rules, not Go's. libcurl ignores an upper case `HTTP_PROXY`
  for `http://` URLs (only `http_proxy` counts), reads `all_proxy`, and its
  support for CIDR in `NO_PROXY` depends on the curl version. `http.proxy` in the
  git configuration wins. DevDesk does nothing to reconcile the two.

## Container scanners and the proxy (DEVDESK-583)

A scanner that runs from an image (trivy, gitleaks, cosign, plumber,
kubeconform, its manifest rendering, and the version probe of a tool image) has
its own network stack and does not see this process's environment. DevDesk
forwards the proxy itself.

- **One constructor.** `scan.containerRun` (`internal/scan/container_run.go`) is
  the only place that builds `<engine> run --rm`. Its `cmd()` writes
  `run --rm <Flags> -e NAME ... <Image> <Argv>`. Every launcher (`trivy_args.go`,
  `gitleaks.go`, `cosign.go`, `plumber.go`, `kubeconform.go`,
  `kubeconform_render.go`, `tool_source.go`) goes through it.
- **Names in argv, values in the environment.** `netproxy.ContainerForward()`
  returns a `Forward`, a snapshot of `HTTP_PROXY`, `HTTPS_PROXY` and `NO_PROXY`
  taken once. For each of the three, the first non-empty value of the upper then
  lower case name is given to both casings. `cmd()` adds `-e NAME` (no value) for
  each name, in the order of `ForwardedVariables`, and sets the values on the
  child process through `Forward.Environ(os.Environ())` (the six names are
  removed from the base first). The engine copies the client's value, so a
  credential in a proxy URL is in no argv: not in the process list, not in the
  command `:jobs` shows, not in a log. The value is not validated.
- **No rewriting.** A proxy on `127.0.0.1`, `localhost` or `::1` is forwarded as
  is. DevDesk does not rewrite it to `host.docker.internal`, does not add
  `--network host` and does not fall back to a direct connection.
- **A `Forward` never prints a value.** `String` and `GoString` show the display
  form (`netproxy.Display`); the values sit behind a function so `fmt` finds
  nothing to print, with any verb.
- **The renderers get no proxy and no network** (DEVDESK-767, D180). A
  `containerRun{Offline: true}` writes `run --rm <Flags> --network none <Image>
  <Argv>`: no `-e NAME`, an empty `Forward`, and `Env` nil unless the caller has
  variables of its own. helm always runs this way and kustomize too, unless
  `scan.tools.kustomize.allow_remote_bases` is on; kubeconform does not (it
  downloads schemas). The renderers run natively with `offlineEnviron`
  instead (a dead proxy and a `GIT_ALLOW_PROTOCOL` that names no transport), and a
  native kustomize is started only after the gate of DEVDESK-771 has read the
  overlay, because Go never proxies the loopback. No new `"run"` literal:
  `Offline` is a branch of the same `cmd()`, so `TestContainerGuard*` is unchanged.
  The loopback proxy warning (`scan.ContainerProxyWarning`) is unchanged and still
  names kubeconform and the other containers. See `scanning.md`.
- **The trust model.** The proxy and its credentials go to every scanner image,
  including one the user set through `tool.Image` (see `scanning.md`).
- **Stderr is redacted.** `cliRunner.Run` redacts each stderr line before it goes
  to `progressFn`, and `waitErr` redacts the diagnostic kept in the `exitError`,
  with `Forward.Redactor` / `Forward.Redact`. A raw proxy value that carries
  credentials becomes its display form (or `[redacted]` when it cannot be parsed),
  and the userinfo and the password disappear, as written, as Go escapes them and
  decoded. A value with no credentials is left alone. The limits are in
  `scanning.md`.
- **The loopback warning.** When the HTTPS or HTTP value names this machine's
  loopback (`Forward.Loopback`, first the HTTPS pair; `localhost` exactly and
  loopback IPs, not `*.localhost`) and a containerised tool is about to run,
  the footer says `<VARIABLE> is <scheme://host:port>, on this machine's loopback:
  scanner containers cannot reach it` (`scan.ContainerProxyWarning`; the variable
  is named as read, the value in display form). The router posts it when a scan
  or a verified pull starts, and the Remediation tab when it scans candidates
  (see `app-shell.md`). It is a warning: the scan still starts.
  - False positive: if the engine's default network is `host` (Podman
    `containers.conf` with `netns = "host"`), the proxy is reachable and the
    warning is wrong. DevDesk does not read the engine configuration.
  - Over-reporting: a scan (`KindScan`) takes every tool the enabled categories
    require, while a scan of images runs only trivy. The warning can name a
    container that the run will not use.
- **Podman on a remote machine** (a Podman machine on macOS or Windows): to be
  verified. Documentation says `-e NAME` without a value reads the client's
  environment, which is what the design relies on; it has not been checked by hand
  here.

### The container guard

`internal/netproxy/container_guard_test.go` keeps a launch from skipping the
forward. Like the HTTP guard it parses the production sources of the module
(go/ast, no types; test files, `.worktrees`, `.git`, `vendor`, `testdata`,
`docs-site` and `node_modules` skipped, `internal/netproxy` read) and reports:

| Rule | Reported |
|---|---|
| C1 | a `[]string` literal whose first element is the literal `"run"` |
| C2 | a call with the literal `"run"` as argument 0 or 1 and at least one argument after it |

The one allowed site is `containerRun.cmd` in `internal/scan/container_run.go`,
hard-coded in the test. Everything else needs an entry in `containerExceptions`,
matched by file, function and rule, and named in this section
(`TestNetworkDocListsEveryContainerException`);
`TestContainerGuardEveryExceptionStillMatches` fails on a stale one.

| File | Func | Rule | Reason | Until |
|---|---|---|---|---|
| `internal/docker/launch.go` | `buildLaunchArgs` | C1 | the user's own container, configured in the launch form: not a scanner | permanent |
| `internal/docker/launch.go` | `VerifyEntrypoint` | C2 | runs only `command -v`, nothing to forward | permanent |

What the guard does not see:

- a `"run"` that is not a literal: built at run time, or a named constant;
- `append(args, "run")`: the literal is the last argument, with nothing after it;
- the `container run` spelling: in `[]string{"container", "run", ...}` the first
  element is not `"run"` (C1), and in `exec.Command(bin, "container", "run", img)`
  the `"run"` is argument 2 (C2 looks at 0 and 1 only);
- a `"run"` at argument index 2 or later;
- C1 looks at the first element only: `[]string{"--rm", "run"}` is not reported;
- a launch that is not a `run` (`docker create`, `exec`), and clients built in
  third-party code.

## The path trace — `internal/nettrace`

The hop-by-hop path to an IPv4 target (DEVDESK-327, S-03a). It came back after
§3.47 deleted the container trace, and it fixes what failed there: it runs **in
the DevDesk process**, so the hops are this machine's. No container, no external
binary (the package never starts a child process; a source guard test enforces
it), no privilege. The spikes measured it unprivileged (uid 1000, no capability)
on native Linux and on WSL2 in NAT and mirrored modes
(`docs/product/network-map/{feasibility,spike-linux,spike-wsl}.md`).

**Linux and native Windows.** `Supported()` returns `nil` on Linux and, on
Windows, when the `iphlpapi.dll` echo entry points resolve (`trace_windows.go`);
it returns `ErrUnsupported` elsewhere (`trace_other.go`, build tag
`!linux && !windows`), and `Trace` then returns `ErrUnsupported` and sends
nothing. `golang.org/x/sys` is a direct dependency: `x/sys/unix` for the Linux
socket calls, `x/sys/windows` for `iphlpapi.dll` (Windows engine).

**One connected UDP socket per TTL (Linux).** Each socket is connected to the target
(port 33435), gets `IP_TTL` set to its hop number and `IP_RECVERR` enabled, and
sends one datagram. The kernel queues the ICMP answer on the socket's error
queue; `await` reads it with `recvmsg(MSG_ERRQUEUE)` through `RawConn.Read`
(the netpoller, no sleep loop) and `decodeExtendedErr` decodes it. A connected
socket with an ephemeral source port means an ICMP message forged off the path
can skew what is shown, not make the trace read something else.

**Two phases** (`engine.go`). `run` first opens all N sockets, in the caller's
goroutine; only then does one goroutine per TTL send and wait, in parallel. So
"not supported" or "no route" is found before any packet leaves: a refused
socket option (`ENOPROTOOPT`, `EOPNOTSUPP`, and `EPERM`/`EACCES` from the system
prober) maps to `ErrUnsupported`, a `connect` failure with `ENETUNREACH` or
`EHOSTUNREACH` to `ErrNoRoute`, and nothing is emitted. All probes then leave at
once, not spread out (D143).

**The Windows engine** (DEVDESK-486, IPv4 only; IPv6 is a separate story). It
plugs into the same two-phase engine through `echoProber` (`echo_conn.go`) and
needs no administrator right.

- **`IcmpSendEcho2`, synchronous, through `iphlpapi.dll`.** `trace_windows.go`
  loads `iphlpapi.dll` and calls `IcmpCreateFile`, `IcmpSendEcho2` and
  `IcmpCloseHandle`. Each TTL gets its own echo handle (`echoConn`, single-use:
  one send, one await). The call is synchronous and cannot be interrupted, so
  `await` runs it in a goroutine, one per TTL, and abandons it when the context
  ends; the goroutine closes the handle itself when it returns, never under the
  running call.
- **Preflight.** Before any handle is created, `udpPreflight` connects (without
  sending) a UDP socket to the target. A missing route fails there with the raw
  Winsock error and becomes `ErrNoRoute`, instead of looking like silence.
- **Classification is by code, never by text** (`echo_status.go`). The reply
  carries an `IP_STATUS` (`GetLastError` when there is no reply): `IP_SUCCESS`
  is `HopTarget`, `IP_TTL_EXPIRED_TRANSIT` is `HopRouter`, the destination
  net/host/protocol/port unreachable statuses are `HopUnreachable`,
  `IP_REQ_TIMED_OUT` is a silent hop, and everything else is `HopError` or an
  `*EchoStatusError` (`ICMP status <n>`, ASCII only, never a localized system
  message). A zero code with no reply is a failure, never a reached target.
- **Source guard extended to `x/sys/windows`** (`source_guard_test.go`). A
  file may import it only to load the one DLL: `NewLazySystemDLL` takes the
  literal `"iphlpapi.dll"`, and `NewProc` takes a literal from the allowlist
  `{IcmpCreateFile, Icmp6CreateFile, IcmpSendEcho2, Icmp6SendEcho2,
  IcmpCloseHandle}` (case-sensitive). Both must be called directly, never used
  as a value; writing the `Name`, `System` or `Dll` field of a DLL handle, naming
  `LazyDLL`/`DLL`/`LazyProc`/`Proc`, and the process, service and
  `LoadLibrary`/`GetProcAddress` symbols are refused.
- **`IsNoRoute` knows the Windows errnos.** Besides `ErrNoRoute`,
  `syscall.ENETUNREACH` and `syscall.EHOSTUNREACH`, it recognizes
  `WSAENETUNREACH`, `WSAEHOSTUNREACH`, `ERROR_NETWORK_UNREACHABLE` and
  `ERROR_HOST_UNREACHABLE` (`noroute_windows.go`, the Winsock codes and the IP
  Helper ones go-netroute can return). The error text never counts.
- **Known limit: the probes are ICMP echoes.** A target (or a firewall in front
  of it) that filters echo requests stays silent: the trace ends on the hop
  limit or the deadline with no `HopTarget`, where the Linux UDP probe would
  have been answered with Port Unreachable. A UDP or TCP fallback is tracked in
  D154 (`docs/backlog.md`).

**Streaming: `TraceEach`** (DEVDESK-328).
`TraceEach(ctx, target, opts, onHop func(Hop)) (Result, error)` is `Trace` that
also hands each hop to `onHop` as it resolves; `Trace` is `TraceEach` with a
nil callback, and the `Result` is the same. The callback is an argument rather
than an `Options` field. Contract:

- It is called from the goroutine that called `TraceEach`, **one call at a
  time**, so a plain slice needs no lock.
- Hops arrive **in answer order, not TTL order**.
- **Never for an unresolved hop** (`HopUnresolved`), **never after `TraceEach`
  has returned**, and never for a target refused before any probe (a
  validation or open error returns with no call).
- It must not block.
- **A hop above the terminal one may be reported** while the terminal is still
  unknown. `Result.Hops` is cut at the terminal hop as before, so the caller
  reconciles: the trace shows `visiblePathHops` while streaming (sorted by
  TTL, unresolved dropped, everything after the first target or unreachable
  hop cut) and replaces its hops with `Result.Hops` at the end.

**The result.** `Trace(ctx, target, Options) (Result, error)`. The target is
unmapped first (`::ffff:a.b.c.d` becomes IPv4); any other IPv6 address gets
`ErrNotIPv4`, and `0.0.0.0`, multicast and `255.255.255.255` get
`ErrNotUnicast`. Loopback and private targets are allowed: the caller decides at
its own boundary. Read `Result` only when the error is nil, because the zero
`Result` and the zero `Outcome` (`OutcomeReached`) mean "no trace".

| `Outcome` | Meaning |
|---|---|
| `OutcomeReached` | The target answered (Port Unreachable from the target itself, `HopTarget`). |
| `OutcomeUnreachable` | A router reported the target unreachable (`HopUnreachable`). It ends the trace, like a reached target. |
| `OutcomeMaxHops` | `MaxHops` (at most 30) exhausted without an answer from the target. |
| `OutcomeDeadline` | The whole-trace deadline (at most 30 s) expired. |
| `OutcomeCancelled` | The caller's context was canceled. |

`Hops` has a **stable shape**: entries in TTL order, `Hops[i].TTL == i+1`. It is
cut at the terminal hop (target reached or refused), or holds one entry per TTL
up to `MaxHops`. When the deadline or a cancellation ends the trace first it
holds exactly `MaxHops` entries, and every probe that did not resolve is a
`HopUnresolved`, even above a terminal hop already known. `HopKind` is
`silent`, `router`, `target`, `unreachable`, `unresolved` or `error`.

**A silent hop is not a failure.** Many routers do not answer expired probes; a
`HopSilent` in the middle of the path says nothing about the hops after it, and
a later TTL can still answer or reach the target. The probes are independent, so
the trace never stops at a silent hop.

**Errors.** Match the sentinels `ErrUnsupported`, `ErrNotIPv4`, `ErrNotUnicast`
and `ErrNoRoute` with `errors.Is`. The errors `Trace` returns and `Hop.Err` carry
the raw platform text (errno messages, in the machine's language): show a fixed
label, never that text, in a summary.

**Known limits**, measured and not fixed:

- **The number of hops that answer varies from one run to the next**, and so
  does the TTL at which the target answers (6 to 10 hops with an address to
  `1.1.1.1` in the native Linux spike; also observed on WSL2 NAT and on native
  Linux, sometimes with silent TTLs before the target). The cause is an
  **unverified hypothesis**: ICMP rate limiting by routers. A timing defect of
  the prober is not ruled out. The lowest TTL that reached the target is not the
  real hop count.
- **A multi-homed target** that answers Port Unreachable from another of its
  addresses is classified `HopUnreachable`, not reached (`classify`).
- **The duration is about `ProbeTimeout` (3 s by default)** as soon as one hop
  before the target is silent, because the trace completes only when every TTL
  up to the terminal one is resolved.
- **`Options.MaxHops` is not tied to the configuration** (`traceroute_max_hops`
  stays removed).
- **The probes are UDP to the fixed port 33435** (`defaultProbePort`, not
  exposed). The trace refuses a target that carries a port. See "The trace
  sub-level" below.

**`Supported()` and the trace.** The trace calls `Supported()` as a launch
guard (`PathModel.launchable()`): where it returns an error, the Trace Path button, `H`, `enter` and `ctrl+o`
are greyed out in the header and refused with a footer warning; nothing is
resolved or sent (Rule 130).

The guard is `pathSupport(goos, kind, traceErr)` in
`internal/ui/netdiag/path_platform.go` (DEVDESK-334). The trace is available
on native Linux, native Windows and WSL2. macOS, WSL1 and any other OS are
greyed out. It names the reason, as a `pathPlatformError` that unwraps to
`nettrace.ErrUnsupported`:

| Platform | Reason |
|---|---|
| Native Windows | none, the trace is available (since DEVDESK-486) |
| macOS | `The path trace needs Linux — not available on macOS` |
| Other OS (not linux, windows or darwin) | `The path trace is not available on <goos>` |
| WSL1 | `The path trace needs a Linux kernel — WSL1 has none (use WSL2)` |
| Anything else unsupported (`nettrace.Supported()` fails) | `The path trace is not supported on this platform` |

The footer shows the reason as a permanent Warning on the form (the Trace Path
button), not only after `enter`. The decision is made once per process
(`systemPathSupport`, a `sync.OnceValue`), so `launchable()` does no I/O per
frame, and nothing is sent on the network. A WSL2 whose networking mode is
unknown never greys the tab out. No WSL2 mode is switched off: the spike
verdict (`docs/product/network-map/spike-wsl.md`) is that WSL2 is supported, so
no switch is coded (YAGNI).

## The trace sub-level — streaming, cancellation, resolution

How `internal/ui/netdiag` drives `nettrace` (DEVDESK-328).

**Streaming.** `launch` (called from `Update`) creates the run's context, a
buffered `chan tea.Msg` and the launch Cmd: a batch of the producer
(`producePath`, which does the I/O and touches no model, Rule 110) and
`waitForPathMsg(ch)`. The producer sends three kinds of message, each tagged
with the run's generation: `pathResolvedMsg` (the address), `pathHopMsg` (one
per `onHop` call) and `pathDoneMsg` (the `Result` or an error), then closes the
channel. `Update` re-arms `waitForPathMsg` after every resolved or hop message;
the done message is the last and is not re-armed. The three messages reach the
Path sub-model whichever tab is on screen. The resolved message also carries the
route (below).

- **Capacity 32** (`pathChanCap = nettrace.MaxHopsLimit + 2`): the resolved
  message, at most one hop per TTL (30) and the done message. The producer
  therefore never blocks on a reader. Sends of the resolved and hop messages
  are non-blocking. **The done message is never lost**: if the channel is
  full, `sendDone` drops the oldest queued message to make room.
- **Generation.** `PathModel.gen` is bumped on each launch and on a cancel. A
  message whose `gen` differs is dropped and not re-armed, so the late messages
  of a canceled run change nothing.
- **Cancellation.** The model holds the run's `context.CancelFunc`; it is called
  **from `Update` only** (`releaseRun`, on `esc` while running, on the next
  launch, and when the done message arrives, which releases the context). `esc`
  answers at once: the state becomes `pathDone`, the outcome `Canceled`, the
  hops already seen stay on screen. The engine stops on its context; the view
  stops reading and nothing is drained, so no goroutine is left blocked on the
  channel.

**Resolution is the view's job, not `nettrace`'s**, which stays address-only. A
literal IPv4 is used as is. A name is resolved with
`net.DefaultResolver.LookupNetIP(ctx, "ip4", host)` under its own timeout, the
context's `network.check_timeout` (`netcheck.Settings.Normalized().CheckTimeout`,
read when the view is built; the tab falls back to 5 s
(`defaultPathResolveTimeout`) otherwise). The **first IPv4 address** is traced (unless Diagnostics already read one of the
addresses returned: "Which address", below), and the header's `Target` field shows it after
the name. A literal IPv6 target
(brackets or several colons) is refused before launch with the footer error
`Only IPv4 targets are supported by the path trace`.

**The route rows (DEVDESK-329).** After resolution and before the trace,
`producePath` asks `pathEngine.route` for the traced address. The system
engine uses `netcheck.SystemEnv(netcheck.DefaultSettings()).Route`, which sends
nothing and needs no privilege. The answer (`RouteHop`: interface, source,
gateway) rides on `pathResolvedMsg` (`route`, `routeErr`), so there is no extra
message and the capacity stays 32. `newPathRoute` turns it into a `pathRoute`
(addresses unmapped to IPv4). The lookup asks the kernel the same question as
`ip route get <addr>` (go-netroute, `RTM_GETROUTE`, policy rules applied), and
the probe socket is unmarked and not bound to a device, so a VPN that captures
the default route shows as the interface row, and the probes take that route.

- **Interface row**: `eth0 (192.168.1.21)`; without a source address `eth0`;
  without a name `unnamed (192.168.1.21)`; `no route` when the lookup proved
  there is none; `undetermined` when the lookup failed for another reason.
- **Gateway row**: the address; `directly connected` when the route has none
  (or an unspecified one); `-` after `no route`; `undetermined` when the lookup
  failed. The texts `undetermined`, `directly connected` and `-` are dim. The
  row stays even when the gateway is also hop 1.
- **`no route` is not `undetermined`.** `newPathRoute` sets `pathRoute.NoRoute`
  only when `nettrace.IsNoRoute(err)` is true: `ErrNoRoute`, or `ENETUNREACH` /
  `EHOSTUNREACH` anywhere in the error chain (`errors.Is`; the error text never
  counts). `IsNoRoute` (`internal/nettrace/noroute.go`) is the single place that
  classifies a no-route failure. Any other failure, including a routing table
  that could not be read, leaves `NoRoute` false and the rows read
  `undetermined`: that says nothing either way.
- **A failed lookup is not fatal.** The trace still runs; the rows read
  `no route` or `undetermined`. The raw error is logged (`ERROR [netdiag/path]
  route <host>: …`) and never shown.
- **The source is never a host name.** It is the fixed text `this machine`, not
  `os.Hostname()`: under WSL2 the distro's host name defaults to the Windows
  computer name, which would label the source as another machine. Under WSL2
  the row says so instead (see "The WSL2 labels" below), still without a host
  name.
- **Counter.** The footer's `Tracing <host> — N hops answered` counts hop rows
  only (`hopCount`), not the local rows. A run that ends on a no-route error
  keeps the local rows; any other error done clears every row (see "Errors
  become fixed labels" below).

**The container source (DEVDESK-330).** The column can start in a running
container of a Docker Engine instead of on this machine. Files: `path_source.go`
(the source and its list, pure), `path_container.go` (the rows' content, pure),
`path_source_picker.go` (the selector, a value type with no I/O),
`path_source_flow.go` (opening, choosing, drawing), `path_run.go` (the reads).

- **The source is a value of the tab**, `pathSource` (`ID`, `Name`, `Image`;
  the zero value is `this machine`). It is read by the launch, not by the trace
  engine: **the probes always leave from this machine**. Nothing runs inside the
  container.
- **The selector** is a framed list drawn over the table with `overlay.Compose`
  (palette variant of Rule 112; the body stays the table, Rule 139), titled
  `Trace from` in the top border, box width `clamp(width-4, 20, 60)`, centred,
  starting on the table's second line. It opens at once with `this machine` and
  the footer's loading dot (`Reading running containers...`, Rule 128), then
  fills from a `pathSourcesMsg` tagged with `sourcesGen`; an answer for an older
  request, or one that arrives after the selector closed, is dropped. The list
  is read again at every opening (no cache). The cursor starts on the source in
  use, marked `(current)`. Entries are `this machine`, then the containers whose
  state is `running` (paused and restarting are left out), sorted by name, shown
  as `<name> · <image>`. A row whose ID fails `IsContainerID` or whose name is
  empty after `engineText` is skipped.
- **Listing** (`listPathSources`, in a Cmd under the resolve timeout) goes in
  this order: engine name (`engine.Current().Name`, no invocation), then
  `info` for the OS name, then `ps`. The result is `this machine` alone, with a
  neutral footer note, when: the engine is podman (`podman is not supported as a
  container source`, nothing invoked); `info` or `ps` fails, or no engine is
  present (`No Docker Engine reachable — only this machine is offered`; the raw
  error goes to the log as `ERROR [netdiag/path] sources: …`); the OS name
  starts with `Docker Desktop` (`Docker Desktop is not supported as a container
  source yet`, `ps` is not called); or no container runs (`No running container
  — only this machine is offered`). Docker Desktop is refused because its
  containers live in a VM: drawing their network before `this machine` would
  be false.
- **Choosing.** `enter` on the source already in use only closes the selector.
  Any other choice clears the column (rows, route, container, address, outcome,
  error), returns the model to `pathReady` with the target kept and nothing
  sent, and shows a `Source` header field with the container name (absent for
  this machine). The column never mixes two sources. `H` from Diagnostics
  (`PathModel.ready`) resets the source to this machine: the route row it comes from is
  this machine's.
- **The container is read in the producer**, after the route lookup and before
  the trace (`inspectPathSource`, under the resolve timeout, killed by the run's
  context), and rides on `pathResolvedMsg` (`source`, `ctr`, `ctrErr`): no new
  message, capacity still 32. A trace from this machine never calls it. A
  failure is not fatal: the rows read `undetermined`, the trace runs, the raw
  error is logged (`ERROR [netdiag/path] inspect <id>: …`) and never shown.
  Reading at launch, not at selection, means "trace again" reads it again and a
  container that stopped in between is shown as not running.
- **Three more row kinds** (`bridge`, since DEVDESK-331, and `engine`, since DEVDESK-332, below), ordered by `pathSteps` only (it takes the container
  as a first argument; `nil` gives the DEVDESK-329 column unchanged): `container`
  and `network` before `source`, `trace` after `gateway` and before the hops.
  Hop and RTT are empty for all three; `trace`, and `network` when it reads
  `undetermined…` or `none…`, are dim.

  | Row | Address text |
  |---|---|
  | `container` | `web-1 (172.18.0.5)`; no address known: `web-1`; stopped at launch: `web-1 (not running)` |
  | `network` | `app_net via 172.18.0.1`; host mode: `host (shares this machine's stack)`; `none`: `none (no network)`; `container:<x>`: `shares another container's network`; undecidable: `undetermined` or `undetermined (N networks)`; inspection failed or container not running: `undetermined` |
  | `trace` | `from this machine, not from the container`; host mode: `from this machine (shared with the container)`; with an `engine` row: `from this machine (host or WSL VM)` |
  | `engine` | `internal path not visible (location unknown)` or `undetermined (bridge not verified)`; see below |

- **The default-route network is never guessed** (`defaultRouteNetwork`). The
  candidates are the endpoints with an IPv4 gateway (an internal network has
  none, so it cannot hold the default route). One candidate: it. Several: the
  one whose `GwPriority` is strictly the highest, provided the engine reported a
  priority on every candidate. A tie, or a missing priority (API before 1.48,
  Docker 28), gives `undetermined (N networks)`, with N the number of endpoints.
  moby documents a tie-break by network name, but its direction was not
  measured, and docs.docker.com says the default gateway "may change whenever a
  container's network connections change". The address on the row is the
  container's IPv4 on that network, and `via` is that network's gateway.
- **Host mode** (`HostConfig.NetworkMode == host`) draws no bridge: the network
  row says the container shares this machine's stack, and the trace row says
  the probes leave from this machine, shared with the container. `none` and
  `container:<id>` get their own texts and are never resolved further.
- **`engineText`.** Every string an engine returned (names, images, network
  names) passes once through `engineText` at conversion: invalid UTF-8 dropped,
  escape sequences stripped (`ansi.Strip`), control (Cc) and format (Cf, so
  bidi overrides and zero-width) runes dropped, whitespace collapsed, at most
  128 runes. The cells are therefore display-safe, and Rule 122 holds (plain
  text in `Cell`, colour in `Style`). It duplicates `imagesearch`'s
  sanitizer; a shared helper is a later refactor.
- **Criterion 7: the engine reads start nothing.** Three guards pin it.
  `TestPathSourceReadsStartNothing` (`internal/docker`) records the arguments of
  the four reads on a stub runner: the first is `ps`, `info`, `container` or
  `network` (then `inspect`), and no argument is a state-changing verb.
  `TestPathEngineReadsStartNothing` (`internal/ui/netdiag`) records the real
  wiring (`systemPathEngine`) through `listPathSources` and the producer and
  asserts the same. `TestPathTabCallsOnlyReadOnlyEngineFunctions` parses every
  non-test file of the package and fails on any `docker.<X>` outside
  `Container`, `ContainerNetworking`, `EndpointNetwork`,
  `ListContainersContext`, `EngineOperatingSystem`, `InspectContainerNetworking`,
  `InspectNetworkBridge`, `NetworkBridge` and `IsContainerID`, on any `engine.<X>` other than `Current`, and on a string
  literal equal to a verb such as `start` or `exec`.
- **Limits.** A remote engine (`DOCKER_HOST=tcp://…`, an ssh context) or an
  engine in a VM other than Docker Desktop is **not detected** as such: its
  containers are listed and drawn before `this machine`, which they never pass
  through. Since DEVDESK-332 the missing bridge is signalled by the `engine`
  row (below); the false positive on `docker0` of a remote engine remains. The
  `trace` row limits the damage. Network drivers (macvlan, ipvlan) are not
  distinguished, because the container inspection does not carry them. The
  container ID is the short one `docker ps` prints, validated by
  `IsContainerID`. Kubernetes, overlay and named VMs are out of scope.

**The bridge row (DEVDESK-331).** Between `network` and `source`, the column
shows the host bridge the container's network is attached to, **only when that
bridge exists on this machine**. Files: `path_bridge.go` (`readPathBridge`,
`hostBridge`), `path_run.go` (the reads), `path_steps.go` (`stepBridge`).

- **`InspectNetworkBridge(ctx, networkID)`** runs `network inspect --format
  <networkBridgeFormat> <id>`: only the driver, the
  `com.docker.network.bridge.name` option and the ID are read. The ID must be 64
  hex digits, one argv element, never a name. The result is a `NetworkBridge`
  (`Driver`, `Name`). `Name` is empty unless the driver is `bridge`; it is the
  option when set, else `br-` + the first 12 digits of the ID. The error carries
  no command output.
- **`pathEngine`** gets two more fields, `network` (`docker.InspectNetworkBridge`)
  and `interfaces` (`netiface.List`), so tests inject both.
- **`readPathBridge`** runs in the producer, after `inspectPathSource`, under the
  resolve timeout, and its result rides on `pathResolvedMsg.bridge`: no new
  message, capacity still 32. It makes **no call** for a trace from this machine,
  host, `none`, `container:<x>`, a container stopped at launch, an ambiguous
  default-route network (`routeEndpoint`, shared with `newPathContainer`), a
  failed inspection or an endpoint with no network ID.
- **`hostBridge`** accepts the interface only if its name equals the expected one
  exactly (case sensitive) **and** it carries the network's gateway among its
  IPv4 addresses. The OS is never consulted. Malformed CIDR entries are ignored.
  The name goes through `engineText`.
- **Failures are logged, never shown** (`ERROR [netdiag/path] bridge network|
  interfaces: …`, not logged when the run was canceled): the row is simply
  absent. `pathSteps` adds `stepBridge` only when `pathContainer.Bridge` is set
  (`withBridge`, for a container attached to a network). Address text is the
  interface name; Hop and RTT are empty.
- **Limits.** Absence of the row no longer means "not found": since
  DEVDESK-332 a missing or unverified bridge is told by the `engine` row below,
  and no row at all is left for host, `none`, `container:<x>`, a stopped
  container, an ambiguous network, a failed inspection or a network with no
  IPv4 gateway (for example `inhibit_ipv4`: `defaultRouteNetwork` keeps no
  candidate, so nothing is read). Rootless Docker gives an `engine` row only
  when the endpoint has an IPv4 gateway and no host interface has the
  bridge's name (`bridgeAbsent`, "not visible"). User-defined networks (`br-<id>`) are protected by their unique
  name. A **false positive** remains for the default `docker0` on a remote
  engine: if the remote engine and a local Docker both use `docker0` with
  `172.17.0.1/16`, the row appears though the container is not behind this
  machine's bridge. This extends the remote-engine limit above.

**The engine row (DEVDESK-332).** When the bridge of the container's network is
not verified on this machine, the column shows an `engine` row between `network`
and `source`, instead of a `bridge` row. It says the internal path is not seen
from here; it never says where the engine runs. Files: `path_bridge.go`
(`classifyBridge`, `readPathBridge`), `path_container.go` (`withBridgeRead`,
`engineView`), `path_steps.go` (`stepEngine`), `path_view.go`.

- **`pathBridge`** replaces the bridge name on `pathResolvedMsg.bridge`: a
  `State` and, when found, a display-safe `Name`. Four states:

  | State | Produced when |
  |---|---|
  | `bridgeNotRead` (zero value) | no read was made: this machine, host, `none`, `container:<x>`, stopped at launch, ambiguous default-route network, failed container inspection |
  | `bridgeFound` | an interface named exactly as the network's bridge exists **and** carries its gateway (`hostBridge`); the name passes `engineText` |
  | `bridgeAbsent` | the expected name is known, the gateway is valid and **no** interface has that name |
  | `bridgeUnverified` | anything else: no network ID on the endpoint, `network inspect` failed, the driver is not `bridge` (empty name), `netiface.List` failed, empty name or invalid gateway, an interface of that name exists but does not carry the gateway, or the name is empty after `engineText` |

- **`classifyBridge(want, gw, ifaces)`** gives the last three states from the
  engine's answer and this machine's interfaces. `readPathBridge` calls it and
  maps every failure to `bridgeUnverified` (logged as before, never shown).
- **`withBridgeRead`** (`pathContainer`, replaces `withBridge` in
  `handleResolved`) copies the outcome into the container: `found` sets
  `Bridge`, `absent` sets `Engine = engineHidden`, `unverified` sets
  `Engine = engineUnverified`. Only a container attached to a network
  (`attachNetwork`) is touched.
- **`pathSteps`** adds `stepBridge` when `Bridge` is set, else `stepEngine` when
  `Engine` is set: **never both**. Step cell `engine`; Hop and RTT are empty.
  The Address (`engineText()`) is dim, like `trace`, and carries **no bridge name
  and no IP**:

  | `Engine` | Address text |
  |---|---|
  | `engineHidden` | `internal path not visible (location unknown)` |
  | `engineUnverified` | `undetermined (bridge not verified)` |

  With an engine row the `trace` row reads `from this machine (host or WSL VM)`.
- **Decision: the engine's nature is never inferred.** Whether the engine runs
  in a VM, on another host or rootless is not read from the OS name, the
  `DOCKER_HOST` or an address range: the column says what this machine can and
  cannot see, nothing more. `engine VM` is to be written only when the engine
  itself declares it is a VM. That is not done: Docker Desktop stays excluded
  from `ctrl+o` (DEVDESK-330) and S-07 (WSL2) is not a prerequisite.

**The WSL2 labels (DEVDESK-333).** Under WSL2 the machine DevDesk runs on is a
VM, and its first hop is the Windows host's virtual switch, not a router. The
column says so only on proof. Files: `internal/wslenv` (detection),
`internal/ui/netdiag/path_wsl.go` (`withWSL`, `windowsHostGateway`,
`wslSourceText`), `path_run.go` (the read), `path_steps.go`.

- **Detection (`wslenv.Detect`).** `Gather` collects `Facts`, `Classify` turns
  them into `Info` (`WSL2`, `Mode`, `NATInterface`, `NATGateway`); a zero `Info`
  means not WSL2. Off Linux nothing is read. WSL2 is proven when
  `/proc/sys/kernel/osrelease` contains `WSL2` (case-insensitive) and the
  facts look like WSL (`WSL_DISTRO_NAME` set, or `microsoft` in osrelease).
  Native Linux gets no mention at all.
- **Generation (`wslenv.DetectKind`, DEVDESK-334).** `ClassifyKind(Facts)`
  returns `KindNone`, `KindWSL1` or `KindWSL2`, reading only `WSL_DISTRO_NAME`
  and the osrelease (`GatherKernel`: no process, no interface listing, nothing
  off Linux). WSL1 is an osrelease containing `Microsoft`, case-sensitive; it is
  checked before the case-insensitive WSL test. The trace uses it to refuse
  WSL1 (see "`Supported()` and the trace"). `ClassifyKind` can say WSL2 for an
  old 4.19 kernel where `Classify().WSL2` is false; known, minor.
- **Mode.** `wslinfo --networking-mode`, run only under WSL, from
  `/usr/bin/wslinfo` then `/usr/sbin/wslinfo` (absolute paths, never the search
  path), with the constant argument, an empty environment, a 2 s deadline and
  the output bounded to 64 bytes. The answer is cross-checked with the
  `loopback0` interface, which exists under mirrored networking only: `nat`
  with `loopback0`, `mirrored` without it, any other answer, a failed run or a
  missing `wslinfo` (WSL before 2.0.4) gives `unknown`.
- **Source row.** `WSL2 (VM), not the Windows host · NAT mode`, `· mirrored
  mode` or `· mode unknown`. The `trace` rows still say `from this machine`.
- **Windows host label.** The gateway row and the TTL 1 hop get ` — Windows
  host (virtual switch)` only when all hold: WSL2 proven, mode NAT, the default
  route of `/proc/net/route` (`defaultGateway`) has the same gateway and the same
  interface as the traced route, and the hop is TTL 1 at that gateway. Never in
  mirrored or unknown mode.
- **Read in the producer** (`detectPathPlatform`, once per launch, under the
  resolve timeout; `pathEngine.platform`, injectable). The result rides on
  `pathResolvedMsg`: no new message, capacity still 32. `withWSL` labels the
  steps without modifying them. With no answer the column reads as on native
  Linux.
- **Import guard.** `wslenv` starts a process, so it is the declared exception
  on `allowedInternalImports` in `path_engine_source_guard_test.go`. What it may
  run is pinned by its own `internal/wslenv/source_guard_test.go`, which is the
  contract.
- **Limits.** A VPN inside the distro that replaces the default route gets no
  Windows host label (the routes differ). Measured on WSL 2.7.13.0 only. The
  label says what the virtual switch is, not what the Windows host reaches.

**Errors become fixed labels.** `pathErrorLabel` maps the sentinels (matched
with `errors.Is`, wrapped errors included) to US English sentences:
a resolution failure to `Could not resolve <host> — no probe was sent`, no
IPv4 answer to `<host> has no IPv4 address — the path trace is IPv4 only`,
`ErrNotIPv4`, `ErrNotUnicast`, `ErrNoRoute` (`No route to <host> from this
machine — no probe was sent`; matched with `nettrace.IsNoRoute`, which also
accepts a raw `ENETUNREACH` / `EHOSTUNREACH` in the error chain) and `ErrUnsupported` to their own sentence, and
anything else to `The path trace failed — check logs`. The raw error text
(errno, resolver message, in the machine's language) goes to the log only
(`ERROR [netdiag/path] resolve|trace <host>: …`). An error leaves the sub-level in
`pathDone` with the hops cleared and the label shown as a persistent error
(level Error) until the next launch (`enter` traces again).
`handleDoneError` decides what stays on screen: when `nettrace.IsNoRoute(err)`
is true, the column **keeps the local rows** (container rows, source, interface
and gateway, platform labels), with the route set to `pathRoute{NoRoute: true}`
(`no route`, gateway `-`); the hops are cleared. Any other error clears the
route, the container, the platform info and every row, as before. How
a successful trace ended is the header's `Path` field (`pathOutcomeLabel`:
`Reached at hop N`, `Reported unreachable at hop N`,
`No answer from the target within N hops`, `Stopped after 30 s`, `Canceled`).

**When the call fails (DEVDESK-335).** `path_noresponse.go`. When a trace ends
on `OutcomeMaxHops` or `OutcomeDeadline`, `refreshSteps` passes the rows through
`withNoResponse(steps, res)`; it returns a copy and modifies neither input.

- **The tail.** `silentTail` takes the longest final run of hops whose kind is
  `HopSilent` or `HopUnresolved`. `withNoResponse` replaces it with one
  `stepNoResponse` row: Hop `N+` (the first TTL of the run; the TTL alone when
  one hop is silent), Step `silent`, RTT `-`. Address text: `no response beyond
  hop N (hops A to B)` or `(hop B)`, with N the TTL of the last router that
  answered (`Anchor`); `no response from any hop (hops 1 to B)` when none did.
- **The anchor.** The last hop that answered (kind `HopRouter` with a valid
  address) gets `· last to answer` after its address, after the Windows host
  label when there is one.
- **No grouping** for `OutcomeReached`, `OutcomeUnreachable` or
  `OutcomeCancelled`, nor when a `HopTarget` or `HopUnreachable` hop precedes the
  tail: that hop contradicts "no response beyond", so the rows stay as they are.
  A nil result changes nothing.
- **Nothing is concluded.** The rows say what was observed, never why: no cause
  and no "down". A silent hop most often means a router or firewall that does not
  answer these probes (ICMP filtering), which is a hypothesis; the rows do not
  state it.

**The target takes no port (DEVDESK-335).** `parsePathTarget` accepts a
bare host name or IPv4 address. `host:80`, `[::1]:443` and `host:` are refused
with `Path ignores ports: enter a host or IP address only` (`errPathPort`,
footer level Error, nothing resolved, no trace). A literal IPv6 target stays
refused with the IPv6 error. `pathTarget` has no `Port` field. The probes are
UDP to port 33435 (`defaultProbePort`); the trace reaches a host, not a service.
This partly replaces point 3 of D144 (the port accepted and shown, not probed);
see D150 in `docs/backlog.md`. `traceroute_max_hops` is still not a
setting: the trace uses `nettrace.Options{}`.

**Testing.** The engine is tested against a fake `prober`; the Linux prober has
loopback integration tests. A real trace to `1.1.1.1` is opt-in and never runs in
CI: `DEVDESK_NETTRACE_LIVE=1 go test -run TestLiveTraceToCloudflare -v ./internal/nettrace`
(Linux, needs the internet).

## The socket table — `internal/ports`

The machine's TCP and UDP sockets, and the process holding each, read **in this
process**. `List(ctx, resolve)`, `Kill(pid, Identity)`, `Terminate(pid,
Identity)`, `Probe(pid, Identity)`, `Liveness`, `ErrNoCleanStop`,
`ConfirmIdentity(pid, Identity)`, `Identity`, `ProtectionOf(pid)`, `Protection`
(with `Reason()`), `ProtectedError`, `UnverifiableError` and
`IdentityChangedError` are the whole interface (the two `...ForTest` constructors of `Identity` aside).

**It exists because the old one answered for the wrong machine** (D55). The
Ports tab ran `ss -tupan` inside
`docker run --rm --net=host --pid=host --privileged`, and on Docker Desktop
`--net=host` is the namespace of the Linux VM: the tab listed the VM's NFS
daemons with two-digit PIDs while not one of the host's 38 listening sockets
appeared, and `K` sent SIGKILL to a process of the VM under the impression it was
freeing a port on the machine. The sentence explaining this has sat at the top of
`docker.runDiagHost` since §3.33 rapatriated DNS, ICMP, TCP, TLS and HTTP for
exactly the same reason; `RunSS` and `KillProcess` were the two nobody pulled it
for. **Under Linux the defect did not exist**, which is why it went unnoticed:
the tab was right on the platform it was written on.

No new dependency: `gopsutil/v4/net` was already imported by
`internal/metrics/host.go`. Windows reads iphlpapi, Linux `/proc`, macOS and the
BSDs `lsof` — the last is a subprocess, but one the system supplies, and it
replaces a privileged container.

Four decisions, each with a test:

- **The process names come from one enumeration, not one call per socket.** The
  obvious `process.NewProcess(pid).Name()` goes through `OpenProcess` on
  Windows and needs rights over the target: measured here, **98 of 189** sockets
  came back *Access denied*, so the Process column would have been empty for
  every service on the box. `Processes()` reads a Toolhelp32 snapshot instead
  and named **294 of 294** in 11 ms, elevated or not. The bulk call is not an
  optimisation over the per-PID one — it is the difference between a column that
  is filled in and one that is not.
- **A PID of zero carries no PID.** It is the system declining to attribute the
  socket, and `K` keys on that field: left as `"0"` the row would look killable,
  and process 0 is a whole process group on Unix. `Kill` refuses it a second
  time, on the model of §3.23's double guard. The name is a *separate* question:
  a row may know which process holds the socket and not what it is called, and
  it must stay killable — that is the row the user is acting on.
- **Two states are normalised, and neither is cosmetic.** An unconnected
  datagram socket is `NONE` on Linux and nothing at all on Windows, so the same
  socket read differently depending on where DevDesk ran — and the `l`/`e`
  filters with it. `ESTABLISHED` is shortened to `ESTAB` because the State
  column is ten cells wide, so the long spelling renders as `ESTABLISHE`.
- **`n` resolves the host half only.** `ss` without `-n` also turned 22 into
  `ssh`; Go resolves a name to a port and not the other way round, so honouring
  that would mean shipping a copy of `/etc/services` and calling the result the
  system's answer. Saying less beats saying something the system did not.

The reverse-DNS cache is **package-level**, because the lookups run inside a
`Cmd` and a `Cmd` may not touch the model (Rule 110). It caches the failures
too: a machine talking to hosts with no PTR record would otherwise re-ask for
every one of them on every two-second tick — the storm the cache exists to
prevent, arriving through the failures instead of the successes. Wildcard,
loopback and unspecified addresses are never asked at all.

**`Kill` signals with the rights DevDesk has**, which is the visible change:
another user's process, or a service, now comes back refused by the operating
system instead of succeeding against the wrong machine.

**So the refusal has to be legible, and `K` has to say when it cannot even
try** (§3.49). What DevDesk itself decides is answerable before the keypress;
what the operating system decides is not:

- **No PID, no key.** A socket the system declines to attribute carries an empty
  `PID`, and `K` is greyed on that row (Rule 130) — it used to be advertised
  everywhere and warn only once pressed.
- **Protected, no key.** DevDesk itself, the process that started it, PID 1 and
  the Windows system PIDs are greyed before the keypress, with the reason
  (`Protection.Reason()`) in the footer when `K` is pressed. This is not a guess
  about what the OS would accept: DevDesk refuses by its own rule.
- **Unverifiable, no key (DEVDESK-709).** A row whose process identity could not
  be read at listing is greyed with `Cannot verify which process this is`. This
  is not a guess about what the OS would accept either: without an identity,
  DevDesk could not tell whether the PID still belongs to the process the user
  saw, so it has nothing safe to signal. The protected rule is read first, so
  DevDesk's own row says `This is DevDesk itself`, not `Cannot verify`.
- **Whether the OS will accept the signal is the attempt's answer**, never the
  header's, so a row with a PID stays lit even when the kill is certain to be
  refused. `killFailureMessage` classifies the failure with `errors.Is` through
  the `%w` wrapping `Kill` applies: `os.ErrPermission` reads *Refused by the
  system*, `os.ErrProcessDone` reads *no longer running*, anything else keeps
  the generic line. `Failed to kill PID N` made those the same sentence.

**Protected processes (DEVDESK-708).** `ports.ProtectionOf(pid)` says whether
DevDesk refuses to signal a PID, and why (`Protection`: `ProtectedSelf`,
`ProtectedInit`, `ProtectedWindowsSystem`, `ProtectedParent`; `Unprotected` is
the zero value). The rule, `protectionFor`, is pure and evaluated in this
order, so a PID that qualifies twice gets the first reason:

1. DevDesk's own PID: `This is DevDesk itself` (also when DevDesk is PID 1 in a
   container);
2. PID 1: `PID 1 is the system's init process`;
3. PID 0 or 4, when `goos` is `windows`: `Windows system process`;
4. DevDesk's **direct** parent, only if it was created no later than DevDesk:
   `This process started DevDesk`.

Rules of the parent check, each a decision (D174):

- **Direct parent only.** Ancestors above it (the terminal, the multiplexer,
  the login session) are not protected. A warning for them may come later; for
  now the user is trusted past the first level.
- **Born first.** `parentStartedFirst` compares the creation times of the parent
  and of DevDesk through `procOS.createTime`. A parent younger than DevDesk is a
  process that reused the PID of the real one, so it is not protected.
- **Unreadable or tied means protected.** If either time cannot be read, or the
  two are equal, the parent is protected: when in doubt, do not signal.
- **Lazy read.** The creation time costs a system call (`OpenProcess` on
  Windows) and `ProtectionOf` runs on every render of the header, so it is read
  only when the PID asked about is the parent's.
- **Parent 0 is ignored.** `os.Getppid()` returns 0 when the parent lives
  outside DevDesk's PID namespace (a container, a sandbox); there is nothing to
  compare, so the parent rule never matches.
- **A dead parent can be replaced by a sub-reaper** (`systemd --user`, an init
  shim) born before DevDesk. It is then shown as `This process started
  DevDesk`: too much protection, never too little.

`Kill` and `ProtectionOf` share one parser, `parsePID`: a PID outside int32 or
not in canonical decimal form (`+5`, `007`, spaces) is refused as `not a
process id` before any protection is looked at, and is `Unprotected` for
`ProtectionOf`. The kernel keeps 32 bits (`pid_t`, `DWORD`), so `"4294967295"`
would otherwise become `kill(-1)` and `"4294967297"` PID 1.

`Kill` checks `protectionOf` before the `n <= 0` check, so that `Kill("0")` on
Windows names `Windows system process`; on Unix, 0 and negative numbers stay
`not a process id`. A refusal is a `*ProtectedError` (`PID N not stopped:
<reason>`) and no signal is sent. It does not wrap `os.ErrPermission`: it is
DevDesk's own refusal, not the system's. The header and `Kill` read the same
function, so the greyed key and the refusal cannot disagree (Rule 130).

**Process identity (DEVDESK-709).** A PID names a process only while that
process lives, and the system may give it to another one. `ports.Identity`
(`identity.go`) lets DevDesk tell the two apart: the creation stamp of the
process, in the operating system's own unit. It is **opaque** to the interface,
which only asks `Verifiable()`; `internal/ports` compares it (`Conflicts`: both
known and different). The zero value is "unknown", so a caller that forgets the
identity gets a refusal, not a signal. Three kinds exist: unknown, known, and
*deferred* (Windows only, below).

| OS | Where it is read | Raw value |
|---|---|---|
| Linux | `/proc/<pid>/stat`, field 22 (`starttime`), counted from the last `)` because the command name may contain spaces and parentheses (`parseStat`, which also reads the state, field 3; a `Z` state is then confirmed by the `Threads:` line of `/proc/<pid>/status`) | ticks since boot |
| Windows | `GetProcessTimes`, `CreationTime` (`identity_windows.go`) | FILETIME as one 64-bit value (100 ns since 1601) |
| macOS | `sysctl kern.proc.pid`, `P_starttime` (`identity_darwin.go`) | seconds and microseconds as one value (`timevalStamp`) |
| other | nothing: the read returns `errors.ErrUnsupported` | every row unknown, `K` greyed |

- **Read once per distinct PID, at listing.** `List` calls `attachIdentities`
  after the socket snapshot. It reads each distinct PID once, in ascending
  order, and gives the value to every row of that PID. A PID that cannot be
  read leaves its rows unknown. `Socket.Identity` is not displayed.
  `ProtectionOf` and its `createTime` (milliseconds, from gopsutil) are
  untouched: the parent rule and the identity are two readings of two sources.
- **Windows: a budget, then a deferred read.** A creation time on Windows is a
  wall-clock time, so it can be compared with an instant. `List` takes `listedAt`
  *before* the snapshot, and a row counts as listed only if the process was
  created at least `windowsBirthSlack` (100 ms) before it (`judgeBirth`:
  `bornBefore`, `bornAround`, `bornAfter`); a process born later, or too close to
  tell, leaves the row unknown. Reading is capped at `windowsIdentityBudget`
  (100 ms) per refresh: when it runs out, the remaining PIDs get a *deferred*
  identity that carries `listedAt`, and `K` stays lit on them. The read happens
  when the confirmation opens (`ConfirmIdentity`, called by `confirmKill` in
  `Update`, so it is a short read at the press of `K`, never at render), with the
  same `judgeBirth` test: born before the listing gives a known identity, born
  after gives `IdentityChangedError`, too close to tell gives
  `UnverifiableError`. A deferred identity never reaches `Kill`: it is refused
  there as unverifiable. Linux and macOS have no budget; everything is read at
  listing.
- **Verified on the process held, then signalled.** `realOS.kill(pid, want, sig)`
  (`sig` is `signalTerm` or `signalKill`, DEVDESK-711; anything else is refused
  with `no signal named`) opens the process with `os.FindProcess` (a pidfd on Linux 5.4 and later, a
  handle on Windows), reads the identity through that held process
  (`heldIdentity`, per OS), compares it with `want` (`verdictAtSignal`, a pure
  function) and only then calls `p.Signal(syscall.SIGTERM)` or `p.Kill()`, which
  signal through the same pidfd or handle. SIGTERM and SIGKILL go through the
  same checks. The PID cannot change hands between the comparison and the signal.
  A failed read never sends. The per-OS files contain no signalling call, so
  `TestOnlyRealOSSendsASignal` still holds.
- **Errors.** `UnverifiableError` (`Cannot verify which process PID N is — not
  stopped`) and `IdentityChangedError` (`PID N is no longer the process that was
  listed — not stopped`) are DevDesk's own refusals, like `ProtectedError`: they
  wrap no OS sentinel, so the screen never reads them as `Refused by the
  system`. `UnverifiableError.Cause` is for the log; `Unwrap` does not return it.
  An OS refusal (`os.ErrPermission`, for instance a Windows handle that cannot be
  opened) or a gone process (`os.ErrProcessDone`) stays readable through
  `errors.Is`, and `killFailureMessage` words it as before.
- **Order in `Kill` and `Terminate`.** Both call `signalTarget`: parse,
  protection, `n <= 0`, then the identity: a non-known identity is an
  `UnverifiableError` and nothing is sent.

**Reaching the screen.** `PortsModel.refusal(pid, process, err)` is the one
classifier of every refused or failed stop: the press of `K` (the confirmation's
read), `Yes` (the table compared with the target) and the kill result all call
it. `errors.As` runs first for the three typed refusals; anything else goes
through `killFailureMessage` (`errors.Is`).

| Error | Footer | Level |
|---|---|---|
| `ProtectedError` | its own text | Warn |
| `IdentityChangedError` | `<name> (PID N) is no longer the process that was listed — table refreshed` (`PID N is …` when the name is empty), plus `requestPortsRefresh()` | Warn |
| `UnverifiableError` | its own text; the cause goes to the log | Warn |
| anything else | `killFailureMessage`: `Refused by the system — DevDesk cannot signal PID N`, `PID N is no longer running`, or the generic line | Error |

After an identity change the table is read once more, even when the tab is
paused (as `n` does), because the footer says it was refreshed; the pause stays
displayed.

**What is guaranteed, and what is not.**

- *Guaranteed*: on Linux with a pidfd, and on Windows within the assumptions
  below, no signal reaches a process other than the one whose identity was
  captured (at listing, or when the confirmation opened for a deferred row).
- *Residual windows, accepted and documented*:
  - **macOS, and Linux without a pidfd.** No handle on the process exists, so
    the comparison and the signal are two steps on a PID. `os.FindProcess` falls
    back to the bare PID, silently, on a kernel without the pidfd calls or where
    seccomp refuses them. The window is short, but it is there.
  - **Listing race (Linux, macOS).** The identity is read after the socket
    snapshot. If the holder exits and its PID is reused between the two, the row
    carries the newcomer's identity. That takes a full turn of `pid_max` within a
    few milliseconds. These stamps are not wall-clock times, so nothing compares
    them with an instant.
  - **Windows clock.** Anchoring to the listing instant assumes that
    `CreationTime` and `time.Now` read the same clock, with a granularity under
    100 ms (not verified). A wall clock set backwards
    between the listing and the read can make a younger process look older. A
    coarser granularity only produces cautious refusals (`bornAround`).
  - **Windows rows over the budget** are not greyed ahead of time when the system
    would refuse them: the refusal shows at the press of `K`. Reading PIDs in a
    stable order keeps the cut on the same PIDs from one refresh to the next.
  - **macOS `EIO`.** For a missing PID the kernel answers `sysctl kern.proc.pid`
    with no data, which `x/sys` reports as `EIO`. `readIdentity` reads `EIO`,
    `ESRCH`, `ENOENT` and a record with another `P_pid` as "process done"
    (not run on macOS).
- *Not run*: `identity_windows.go` and `identity_darwin.go` are compiled for
  their OS and read, never executed; no Windows or macOS machine was available.
  That `OpenProcess` with `PROCESS_QUERY_LIMITED_INFORMATION` and the handle of
  `os.FindProcess` both allow `GetProcessTimes` is the documented behaviour of
  the API, not checked here.
- *Cost on Windows: not measured* (as of 2026-11-23). `windowsIdentityBudget` is
  the story's starting value, not a measured threshold. To measure it, on a
  Windows machine **without elevation**, with a busy socket table (the story's
  case is 189 sockets):
  `DEVDESK_MEASURE_IDENTITY=1 go test -run TestMeasureIdentityCost -v ./internal/ports`
  (PowerShell: set `$env:DEVDESK_MEASURE_IDENTITY=1` first). It reads the table
  and every distinct PID five times with no budget, and logs the sockets and PIDs
  counted, the failures by kind (permission, gone, other), the min, median and
  max per round, and the cost of `List`. It never signals. Write the result in a
  dated note under `docs/measurements/` and adjust the constant if it
  contradicts the budget (D175). The task is DEVDESK-727.

**The target is captured (DEVDESK-714).** `confirmKill` copies the selected
row's PID and process name into `pendingKill` (a `killTarget`) in `Update`,
and builds the text of the confirmation from it. `handleConfirmYes` reads
`pendingKill`, never the table: the refresh keeps running during the
confirmation and the table's cursor stays on an index, so the selected row can
be another process by the time the user answers. Before the bug, `Yes` stopped
that other process. `handleConfirmNo` and `handleConfirmYes` both clear
`pendingKill` and the modal; a `Yes` with no pending target does nothing.
The target also carries its `Identity`: the one the row was listed with, or the
one `ops.confirm` returned when the confirmation opened (a refusal there shows
at once and opens no modal). `handleConfirmYes` then, in this order:

1. `targetPresence`, which compares the target with the last table received
   without a system call and has three outcomes. `present`: a row of the same
   PID has the same name and no conflicting identity. `replaced`: a row of the
   same PID has a known identity that differs from the target's; nothing is
   sent, and `refusal` says the PID is no longer the process that was listed and
   reads the table again. `absent`: no such row; nothing is sent and the footer
   warns `<name> (PID N) is no longer listed — not stopped`. The process may be
   gone, or its PID may belong to another process now; a refusal costs the user
   one keypress, a wrong signal costs a process. A deferred or unknown identity
   never conflicts here (`Conflicts` needs both known); the signal verifies it.
2. `ops.protection` on the captured PID: a target that became protected since
   the confirmation opened is refused with a warning naming the reason.
3. On Windows, the busy mark and `killProcessCmd(ops.kill, target, 0)`, with
   `kill` and the target copied as values (Rule 110). On Linux and macOS,
   `startStop(target)`, the first step of the two-step stop below.

A `ProtectedError`, an `IdentityChangedError` or an `UnverifiableError` that
comes back from `Kill` (the in-depth refusal) shows as a footer **warning**, not
an error: a precondition was not met, nothing failed (Rule 128).

**Two-step stop (DEVDESK-711, S-02b).** On Linux and macOS `K` asks for a clean
stop first and forces only after a second confirmation. `internal/ports` holds
stateless primitives; the state machine lives in `internal/ui/netdiag`
(`ports_stop.go`), because the wait must not block and `esc` must be able to end
it (Rule 110).

- **Primitives (`internal/ports`).** `Terminate(pid, Identity)` sends SIGTERM and
  returns at once; it waits for nothing. `Kill` is unchanged (SIGKILL,
  `TerminateProcess` on Windows). Both call `signalTarget` and then the single
  `procOS.kill(pid, want, sig)`, so protection, identity and the held process
  are the same for both signals. `Terminate` wraps its error `stopping process
  N: %w`; `refusal` reads it with `errors.As` and `errors.Is`, so the prefix
  never reaches the screen. On Windows `Terminate` answers `ErrNoCleanStop`,
  after the protection check; the UI never calls it there.
- **The probe sends nothing.** `Probe(pid, Identity)` reads the state and the
  identity stamp in one read (`readStatus`: `/proc/<pid>/stat` on Linux,
  `sysctl kern.proc.pid` on macOS) and decides in `livenessOf`:
  `ProcessExited` when no process holds the PID **or when another process does**
  (the stamp differs from the listed one: the listed process is gone),
  `ProcessZombie`, else `ProcessRunning`. On Linux `isLinuxZombie` decides: state
  `X` (dead) is a zombie; state `Z` is one **only if `Threads:` in
  `/proc/<pid>/status` is 1**. A process whose main thread has exited (state `Z`
  on the leader) while other threads still run is alive and stays `running`.
  `readStatus` reads `/proc/<pid>/status` (`readThreads`, `parseThreads`) only
  when the state is `Z`; a status file that cannot be read or parsed leaves the
  thread count unknown, and an unknown count reads as alive. A status file that
  is gone reads as exited. On macOS it is `P_stat == 5` (local constant
  `darwinSZOMB`, since `x/sys` exports none), unchanged.
  `Liveness.Stopped()` is true for the first two. A read that fails for another
  reason returns `ProcessRunning` with the error: an unreadable probe is never
  taken for a stop. On Windows and other platforms `Probe` returns
  `errors.ErrUnsupported`. The probe is not a signal, so
  `TestOnlyRealOSSendsASignal` does not apply to it.
- **The machine (`PortsModel.stop`, a `*pendingStop`; nil means no stop).**

  ```
  K, first Yes        SIGTERM result, no error          still running at the deadline
  signalling  ---------------------------> waiting ------------------------------> askingForce
     | error: no wait                         | exited or zombie: "stopped"             | Yes
     v                                        v                                          v
   (end)                                    (end)                                    forcing --> (end)
  ```

  `signalling`: `terminateCmd` is in flight, the row shows `Stopping nginx (812)`.
  `waiting`: the row shows `Waiting for nginx (812) to exit` and `stopProbeCmd`
  (a `tea.Tick` of `stopProbeEvery`, 250 ms) probes once per tick. `askingForce`:
  the row's busy mark is lifted and the `Force Kill` modal is open. `forcing`:
  `killProcessCmd` is in flight, the row shows `Killing nginx (812)`.
- **The clock.** `stopGrace` is 5 s, fixed (D176). The deadline is the instant
  `terminateCmd` returned plus `stopGrace` (`portsTermResultMsg.at`); each probe
  carries its tick instant (`portsStopProbeMsg.at`), and the model compares the
  two. No clock lives in the model, so tests choose the instants. A probe at or
  after the deadline that still reads running opens the second confirmation: at
  most about 21 probes per stop. An unreadable probe is logged
  (`ERROR [netdiag/ports] probe pid=N`) and counts as running. A zombie is
  counted as stopped (logged at `INFO`).
- **Generation.** Each stop takes `nextStopGen()`, a package-level atomic counter,
  so two netdiag views built one after the other never share one. Every message
  of a stop carries it. A message whose generation differs from `pm.stop.gen`,
  that arrives with no stop, or that arrives in a phase that does not expect it,
  is ignored: a tick that outlives its stop (after `esc`) touches neither the
  footer nor a stop started since.
- **Refused SIGTERM.** `portsTermResultMsg.err` ends the stop on the spot, with
  no wait, through `refusal` (the same classifier as before: `Refused by the
  system — DevDesk cannot signal PID N`, `PID N is no longer running`, the
  identity and protection warnings).
- **The second confirmation.** Opened by the deadline, never by a key, so it can
  open while the user is typing in the filter (`/`). `askForce` therefore does
  three things: it calls `FilterBar.EndSearch()` (the filter leaves edit mode and
  its text stays applied, as after `enter`), it opens the modal with
  `components.NewConfirmModal(...).WithoutYesShortcut()`, and it sets the Warn
  footer `nginx (812) did not exit within 5 s`. On this modal `y` and `Y` do
  nothing: a letter of a search the user was typing must not answer Yes to a
  SIGKILL. The focus starts on No; Yes takes `←` or `→` to move to the button,
  then `enter`. `n`, `N` and `esc` answer No. Every other `ConfirmModal` keeps
  its `y`/`Y` shortcut (opt-in, the zero value is unchanged). No answer
  forces anything. `No` and `esc` end the stop with the Info `nginx (812) left
  running — no SIGKILL sent`. `Yes` (`handleForceYes`) acts on the target
  captured at the first confirmation: a table that lists the PID under another
  identity (`replaced`) is refused with the identity-changed warning and no
  system call; a row **absent** from the table does not stop it (a process that
  is stopping often closes its sockets before it exits, which is the case this
  step is for), the identity is verified at the signal; the protection is
  recomputed. Then `kill` runs with the captured identity, the same check as
  SIGTERM.
- **The SIGKILL result is always announced.** `portsKillResultMsg.gen` is the
  generation of the stop it closes (0 for a Windows kill). A result of the
  current stop clears the busy mark and the stop; a stale one only gets its
  sentence. `os.ErrProcessDone` on a two-step result reads as Info
  `nginx (812) stopped` (the process left between the deadline and the `Yes`) and
  reads the table again; anything else goes through `refusal`; success is Info
  `Process 812 terminated`.
- **`esc` while waiting.** `cancelWait` ends the stop and says Info `Stopped
  waiting for nginx (812) — the stop request was already sent`. It signals
  nothing and does not take the SIGTERM back. Outside `waiting` it does
  nothing: during `signalling` and `forcing` there is nothing to cancel, and the
  modal of `askingForce` takes `esc` itself (as `No`).
- **One stop at a time.** `killable()` greys `K` on every row while `pm.stop` is
  set, after the reasons that belong to the row (protection, identity):
  `Another stop is already in progress`, or `This process is already being
  stopped` on the row of the stop. **This is an exception to Rule 130** (an
  operation in progress is not a cause of grey), imposed by the criterion that
  only one stop waits; it lasts at most the 5 s plus a modal, and pressing `K`
  puts the reason in the footer (D176). On Windows `K` stays lit on a busy row
  as before. `esc` is a control, greyed outside the wait (`Stop waiting`), and
  absent on Windows, so the set of keys on a platform does not change from one
  state to the next.
- **During the wait** the table keeps refreshing (its own tick chain; `space`
  stays the user's choice), every key answers, and the chain of probes is not
  suspended when the terminal loses focus (it is bounded by 5 s). A refresh does
  not touch `stop`. `Model.Running()` is true while a stop is pending, so the
  router does not rebuild a netdiag view opened from another view (`OriginView`)
  in the middle of one.
- **Windows is unchanged.** `pm.stop` is never set there: `handleConfirmYes`
  follows the S-02a path (`MarkBusy("Killing …")`, `killProcessCmd(kill, t, 0)`),
  selected by `processOps.windows`. No wait, no second confirmation, no new text.
- **Off screen (`netdiag.BackgroundMsg`).** `portsTermResultMsg`,
  `portsStopProbeMsg` and `portsKillResultMsg` implement `BackgroundMsg`
  (`netdiagBackground()`); no other type does
  (`TestOnlyNetdiagWorkIsBackgroundMessage`). `internal/app` routes them with
  `routeNetdiagWork`: to the active view if `:net` is on screen, otherwise to
  the netdiag view the router holds, through `routeToView`, without changing the
  current view and without creating the view if there is none. Without it the
  chain would stop when the user leaves `:net`, `K` would stay greyed and the
  row busy. This also fixes a kill result lost the same way before DEVDESK-711.
  See `app-shell.md`, « Cross-View Communication ».

**Wording per platform (DEVDESK-710, DEVDESK-711).** Windows has no signal to
send: the process is ended at once through `TerminateProcess`, and the
confirmation promises nothing clean. `processOps.windows` selects the texts. The
key is `K` everywhere.

| | Linux, macOS | Windows |
|---|---|---|
| `K` label in the header | `Stop process` | `Kill process` |
| Title | `Stop Process` | `Kill Process` |
| Body | `Ask <name> (PID <pid>) to stop?` + `Sends SIGTERM and waits up to 5 s. You can force it afterwards.` | `Terminate <name> (PID <pid>)?` + `Windows has no clean stop for this process: it is ended at once and cannot save anything.` |
| Row and footer | `Stopping <name> (<pid>)`, then `Waiting for <name> (<pid>) to exit` | `Killing <name> (<pid>)` |
| End | `<name> (<pid>) stopped` (it left within 5 s), `Process <pid> terminated` after a forced kill | `Process <pid> terminated` |
| Second confirmation | title `Force Kill`; `<name> (PID <pid>) did not exit within 5 s.` + `Force kill (SIGKILL)? It gets no chance to shut down cleanly.` | none |
| Help for `K` | `Ask the selected process to stop (SIGTERM), after confirmation; if it still runs 5 s later, offer SIGKILL (Ports tab)` | `End the selected process at once, after confirmation (Ports tab)` |
| `esc` | header `Stop waiting`, help `esc (Ports)` | none |

In the two Linux/macOS modals the process name is cut to 32 cells
(`stopNameWidth`), because `ConfirmModal` does not wrap and the lines must fit 80
columns. A name with no value reads `PID <pid>`. The Windows label stays free of
the word « Stop » (`TestWindowsLabelsPromiseNoCleanStop`): it must not suggest a
clean stop. A graceful close (`WM_CLOSE`) is a later story (S-04), not this one.

**Seams and the signal guard (DEVDESK-708, DEVDESK-709, DEVDESK-711).**
`internal/ports/procos.go` holds the seams the package reads about the host:
`selfPID`, `parentPID`, `goos`, `now` (the listing instant and the Windows
budget) and `procOS` (an `osProcs` with `createTime`, `identity`, `probe(pid)`
and `kill(pid, want, sig)`; production value `realOS{}`). Per OS,
`identity_linux.go`, `identity_windows.go`, `identity_darwin.go` and
`identity_other.go` hold `readStatus` (`readIdentity` is its start stamp),
`heldIdentity` and `isNoSuchProcess`; `readStatus` returns
`errors.ErrUnsupported` on Windows and other platforms. In `netdiag`,
`processOps` (`protection`, `confirm`, `terminate`, `probe`, `kill`, `windows`)
gathers what the tab asks of the system, wired by `realProcessOps()` to
`ports.ProtectionOf`, `ports.ConfirmIdentity`, `ports.Terminate`, `ports.Probe`,
`ports.Kill` and `runtime.GOOS == "windows"`; tests replace `PortsModel.ops`, so
no netdiag test can reach `ports.Kill`, `ports.Terminate` or `ports.Probe`. A test
builds an identity with `ports.IdentityForTest(raw)` or
`ports.DeferredIdentityForTest(listedAt)`; both panic outside a test binary.

`realOS.kill` is the **only** place in the package that sends a signal (SIGTERM
and SIGKILL both, chosen by its `sig` argument), and its
first statement is `refuseRealSignalUnderTest`: in a test binary
(`testing.Testing()`), a real signal **panics** unless the test called
`allowRealSignals(t)`. It panics rather than returning an error because an
error looks like an ordinary failed kill and the test could still pass. Two
tests hold the guard:

- `TestARealSignalInATestBinaryPanics` and
  `TestKillWithoutASubstitutedProcOSHitsTheGuard` run the panic.
- `TestOnlyRealOSSendsASignal` parses the non-test files of `internal/ports`
  and fails on a signalling call (`os.FindProcess`, `syscall.Kill`,
  `unix.Kill`, `windows.TerminateProcess`, a `.Kill(` or `.Signal(` method)
  outside `(realOS) kill`, or if the guard is not its first statement. It does
  not see an aliased import, another package or an external binary.

These tests send a real signal, each to a child the test started itself:
`TestKillEndsARealChildProcess` and `TestKillSparesARealChildWhoseIdentityDiffers`
(the child must survive: the identity differs, so nothing is sent), and, since
DEVDESK-711 (`internal/ports/stop_real_test.go`, Linux), `TestTerminateEndsARealChildProcess`,
`TestTerminateSparesARealChildWhoseIdentityDiffers`,
`TestTheTwoStepStopOnARealChildThatIgnoresSIGTERM` (a child that ignores SIGTERM
survives it and dies of the SIGKILL) and `TestKillAfterTerminateSparesAReusedPID`.
`TestProbeSeesARealZombie` signals nothing: it reads the `/proc` state of a child
that exited and was not yet waited for.
`TestKillOfAnExitedChildSendsNothing` calls `Kill` on a child that already
exited. They re-run the test binary as a child (`TestHelperProcessSleeps`), call
`allowRealSignals(t)` and wait for it. Never call
`allowRealSignals` in a test aimed at anything else. The seams are globals:
no `t.Parallel` in the package.

**Known limits.** S-01b (DEVDESK-709) closed the PID-reuse window on Linux with
a pidfd and on Windows with a handle. What stays, said plainly (the guarantees
and the cost measurement are under « Process identity »):

1. **macOS, and Linux without a pidfd** (kernel older than 5.4, or seccomp
   filtering `pidfd_open`). `os.FindProcess` silently falls back to the bare
   PID, so the comparison and the signal are two steps on a PID. The window is a
   few microseconds, and using it would need a whole turn of `pid_max` at an
   identical tick: impractical, not impossible.
2. **Identity read after the snapshot (Linux, macOS).** `attachIdentities` runs
   after the socket snapshot; a PID that changes hands in those few
   milliseconds carries the newcomer's identity.
3. **Windows clock set backwards.** `judgeBirth` compares a creation time with
   the listing instant (`windowsBirthSlack`, 100 ms). If the clock goes back
   between the listing and the birth of a process that took the PID, the
   newcomer can look older than the listing. It applies to both read paths; it
   matters mostly for a deferred row, where the gap lasts until the
   confirmation opens.
4. **`HOST_PROC`.** gopsutil pairs sockets and PIDs under that prefix, but
   `readIdentity` reads `/proc` as a literal path and the pidfd acts in
   DevDesk's own PID namespace. If `HOST_PROC` points at another namespace's
   `/proc`, the check agrees with itself and cannot see the mismatch. This
   predates S-01b.
5. **Windows inherited sockets.** A socket inherited by a child can keep the
   `OwningPid` of a parent that exited, with that PID reused before the listing.
   The identity is read from the PID as listed, so it matches the newcomer and
   the row is not caught. This predates S-01b.
6. **Windows and macOS code never ran.** `identity_windows.go` and
   `identity_darwin.go` are compiled and vetted, not executed. Unconfirmed
   assumptions: an unknown PID fails `OpenProcess` (and `os.FindProcess`) with
   `ERROR_INVALID_PARAMETER` on Windows; a missing PID answers `EIO`, or a record
   whose `P_pid` differs, on macOS.
7. **The two-step stop never ran on macOS** (DEVDESK-711). `readStatus` in
   `identity_darwin.go` is compiled and vetted, not executed. Not verified:
   that `sysctl kern.proc.pid` returns a zombie with `P_stat == SZOMB` (5, a
   constant copied from `<sys/proc.h>`, not from `x/sys`) and its `P_starttime`.
   If the kernel does not return it, the read means « gone »: the zombie reads as
   exited, and the stop counts as done either way. SIP may also refuse a SIGTERM
   (`EPERM`), which shows as `Refused by the system`.
8. **The second confirmation can wait out of sight.** The deadline can open the
   `Force Kill` modal while the user is on another tab or another view, since the
   messages reach netdiag off screen. Nothing is forced without the answer, but
   nothing says it is waiting; it shows when the user returns to the Ports tab,
   and `K` stays greyed until then (`Another stop is already in progress`).
9. **Rebuilding the view loses the wait.** `Running()` protects only a netdiag
   view opened from another view (`OriginView`). If the router rebuilds the view
   during a stop (a context switch, for instance), the wait and the second
   confirmation are lost without a word. The SIGTERM already sent is not taken
   back.
10. **PID reuse during the 5 s.** Between two probes the PID can change hands.
    `Probe` compares the identity stamp, so the listed process reads as exited
    and the footer says it stopped, which is true of the listed process. The
    SIGKILL re-verifies the identity at the signal, with the same residual window
    as any signal (limit 1). The identity is the one captured at the first
    confirmation, not re-read at the second.
11. **A row absent from the table can still be forced.** The second `Yes` does not
    require the row to be listed (a stopping process often closes its sockets
    first), so it relies on the identity check at the signal alone.

Also: the process name comes from the system and is not sanitised (DEVDESK-720).
S-02b adds it to four places: the first confirmation, the second confirmation,
the row's busy label and the footer lines (including the `did not exit` Warn);
the cut to 32 cells in the modals bounds the width, not the content.
The Linux zombie rule depends on `Threads:` being readable: if it is not, a
zombie reads as running and the second confirmation opens at the deadline
(the safe side). `targetPresence` trusts the last table received: a
paused or stale table is trusted as it stands, and the signal is what verifies.
`createTime` (milliseconds) still feeds the parent rule and `Identity` is a
second source; unifying them would change S-01a and is not done. When
`parentPID()` is 0 (PID namespace), the parent is not protected.

The platform error never reaches the screen — measured, PID 4 on this machine
answers `OpenProcess: Accès refusé.`, in the machine's own language, which is
the route stage's rule (§3.44). Also measured, and written down rather than
discovered: **the "already gone" branch does not fire on Windows**, where a
nonexistent PID fails `OpenProcess` with `ERROR_INVALID_PARAMETER` and maps to
neither sentinel. Mapping that code would be a guess, and the row disappears on
the next two-second refresh anyway.

**Nothing in the application runs `--network host` any more** (§3.47). §3.33
brought DNS, ICMP, TCP, TLS and HTTP into the process, §3.43 the socket table,
§3.44 the interfaces, and §3.47 removed the route trace — the last one. That is
the checkable form of "DevDesk never claims to answer for a machine that is not
yours", and it closes **D57**.

**The route trace was deleted rather than rewritten** (it came back in the
process with DEVDESK-327, see [The path trace](#the-path-trace--internalnettrace);
what follows is the history of §3.47), and the reasons are worth
keeping because they are the shape of the decision, not of this feature:

- it answered for the **wrong machine** — `--network host` traced from the Docker
  Desktop VM, so the hops were the VM's;
- what people came for has a **better answer**: `netcheck`'s Local route check
  names the interface and source address from *this* machine, in 2 ms, which is
  the split-tunnel question;
- rewriting it was **three unmeasured unknowns** — reading ICMP `TIME_EXCEEDED`
  without a raw socket on three platforms, a per-packet TTL `pro-bing` does not
  expose, and a TCP mode needing the ICMP error of an outgoing connection.

`H` went back into `keymap.Free()` beside `J`, `Q` and `Z`: a letter an action
has just released is redeclared free, or it stays reserved for something that no
longer exists.

The OCI connectivity test — the last container DevDesk started from a
network-inspect overlay, and with it `network.connectivity_image` and its
three-link migration chain (`docker.network_tool_image` →
`network.tool_image` → `network.connectivity_image`) — has been removed. The
network-inspect overlay itself (`:oci` → Networks → `enter`) stays; only the
`c` key it used to offer, and everything behind it, is gone.

## The port forwarder — `internal/forward`

A local TCP port redirected to a `host:port`, **in this process** and with no
privilege: a `net.Listen` and two `io.Copy`. `Registry.Open(port, target)`,
`Toggle(id)`, `Close(id)`, `CloseAll()`, `List()`, `Restore(entries)` and
`Entries()` are the whole interface; the messages a view exchanges with the router live in the same package so a view
never imports `internal/app`.

**The router owns the registry** (`shared.State.Forwards`, built once in
`newWithSize`). `reinitializeViews` drops every view on a config save and on a
context switch, so a listener held by the Forward tab would stay bound with
nothing left to close it. The tab asks (`forward.Open` / `Close` / `Refresh`) and
is told (`forward.ChangedMsg`, broadcast to every held view, on screen or not).
Forwards **survive a context switch** — one belongs to no context.

### Persistence (§3.75)

The **listener** cannot outlive the process, but the **intent** can: every
forward is written to `~/.devdesk/forwards.yaml` and reopened at the next launch.
It is a file of its own rather than a key of `config.yaml`, because the config
is per context and is reloaded on every switch, and a forward belongs to no
context.

| Piece | Where | Does |
|---|---|---|
| `Store` (`store.go`) | `internal/forward` | `Load` / `Save` the `[]Entry` (`local_port`, `target`, `paused`), `version: 1` |
| `Registry.Entries()` | `internal/forward` | describes what is *wanted*, in creation order, live or not |
| `Registry.Restore()` | `internal/forward` | reopens the entries at startup, binds in parallel |
| `restoreForwardsCmd` / `saveForwardsCmd` | `internal/app/forward.go` | the router's two Cmds; the list is copied in `Update` (Rule 110) |

A forward has a `State`:

| State | Meaning | In the file |
|---|---|---|
| `StateLive` | listener bound | an entry |
| `StatePaused` | the user stopped it; port released, row kept | `paused: true` |
| `StateUnbound` | wanted, not bound — port taken or target silent at the last attempt; `LastErr` says which | an entry (retried at launch) |

- **A refusal at creation deletes nothing; a failure at launch keeps the entry.**
  `Open` still refuses a typo, opens no row and writes nothing — the form's
  business. `Restore` answers a service that is not up yet, or a port some other
  process holds today, and forgetting the route for that would make the file
  lose things by being started at the wrong moment. The row reads unbound and
  `space` tries again.
- **`space` is Pause/Resume** (Rule 111's *Control* key), not a new capital. A
  resume is a bind like any other: probe, then listen. A failure is returned and
  recorded, so the row and the footer say the same thing.
- **`K` deletes**: the row and its entry, after a confirmation that says it will
  not come back.
- **Order is the creation order** (`entry.seq`), not the last bind and not the ID
  as text — `"10"` sorts before `"2"`, and a restored file creates its rows in
  the same instant. A row does not move when it is paused and resumed.
- **`serve` is handed its listener.** A pause clears `entry.ln`; an accept loop
  that read the field would be the goroutine dereferencing it.
- **The write is atomic** (temporary file, `Sync`, `Rename`) and serialised by a
  mutex: two `Cmd`s can be in flight, and the older list must not land last.
- **An unreadable file is refused, not treated as empty.** `Load` returns
  `ErrUnreadable` naming the file and touches nothing; the first save afterwards
  moves it to `forwards.yaml.unreadable` instead of overwriting it.
- **A test never writes the developer's home.** `newWithSize` sets no store;
  `New` calls `useForwardStore`, the way it calls `useSecrets`.

**Two instances share the file.** `~/.devdesk/` is shared, so two DevDesks write
`forwards.yaml` and the last write wins; the second one's reopening then finds
the first one's ports taken and reads unbound rather than failing. There is no
file lock — the atomic write keeps the file whole, not merged.

### Named routes (§3.74)

A forward with a **name** is an HTTP route instead of a TCP redirection. One
`http.Server` on `127.0.0.1:network.proxy_port` serves every route, and the
request's `Host` header picks the target: `http://api.localhost:8080` and
`http://app.localhost:8080` differ by name, not by port. No DNS entry, no
privilege — `*.localhost` is reserved for the loopback (RFC 6761), and the
platforms measured resolve it unaided.

| Piece | Where | Does |
|---|---|---|
| `proxy.go` | `internal/forward` | `OpenRoute`, `SetProxyPort`, the proxy's lifecycle, `serveHTTP` |
| `Entry.Name` | `store.go` | a route is an entry with a name and no `local_port`; the type is *deduced from the name*, so no state has a route without one |
| `network.proxy_port` | `internal/config` | the port, 8080 by default, per context (see below) |
| the form's `Type` field | `forward_form.go` | `TCP` / `HTTP`, a cycle field (Rule 132); only the fields that apply are shown |

- **The proxy binds with the first served route and closes with the last.** Its
  lifecycle is serialised by `proxyMu`; lock order is `proxyMu` then `mu`, and the
  request handler takes `mu` only, so serving never waits on a bind.
- **`Host` is matched without its port, trailing dot or case.** An unknown host,
  or bare `localhost`, gets a 404 that lists what *is* served. A target that does
  not answer gets a 502 naming it, and the route's `LastErr` records why.
- **The inbound `Host` is forwarded to the backend**, not replaced by the
  target's: an application behind a named route often keys on it. `WebSocket`
  upgrades pass (`httputil.ReverseProxy` handles them), which is what hot reload
  rides on.
- **A name must be a valid host name ending in `.localhost`**, unique whatever
  the state of the route that owns it — a paused route keeps its name, or
  resuming it would find it gone. Stored in lower case. No suffix completion in
  the form: what is typed is what is stored, and a name that does not end in
  `.localhost` is refused there before it reaches the registry.
- **A route has no port of its own.** `List` fills `LocalPort` with the proxy's,
  which is the number that goes in the URL. A TCP forward asking for that port is
  refused (`ErrProxyPortTaken`), and the proxy's own bind failing is
  `ErrProxyPortInUse` — not `ErrPortInUse`, whose sentence says to pick another
  port in a field a route does not have.
- **The proxy's `Server` and its listener are closed separately.** `Server.Close`
  reaches the listener only once `Serve` has registered it, on a goroutine; the
  registry closes the listener itself so that the port is free when the stop
  returns and a pause-then-resume can rebind at once.
- **The port follows the configuration.** It is per context, and a forward
  belongs to none, so a switch that changes it moves the proxy: `syncProxyPortCmd`
  runs on a context switch and on a saved configuration. It records the wanted
  port in `App.wantedProxyPort` in `Update` and the `Cmd` applies the value as it
  is *when it runs* — two moves in flight leave the proxy on the latest port
  whatever order they finish in. If the new port cannot be bound, every live
  route becomes unbound with the reason and nothing leaves the file.
- **Restore probes the targets, then binds the proxy once**, and only if a route
  passed: a file whose routes are all silent binds nothing.
- **Restore holds the file to the rules of `OpenRoute`.** A name is normalized
  (lower case) and validated, and a second entry for a name is refused — the
  file can be edited by hand, and a name that was never normalized would read
  live and never match a request. A refused entry is kept, unbound, with the
  reason, and is not rewritten. Resuming a paused route also refuses a name a
  live route already answers to.
- **A new proxy port retries the routes a bind failure left unbound.** Without
  it, correcting a taken `network.proxy_port` would report success and leave
  every route unbound with an error naming the port just replaced.
- **A resume marks the route live under `proxyMu`**, the lock a pause takes to
  decide the proxy is idle. Released in between, a pause of the last live route
  could close the proxy under a route that then read live with no listener. A
  test holds a resume in that window through an unexported hook and fails when
  the lock is released early.
- **A client that goes away is not the route failing.** A cancelled request does
  not set `LastErr`.

**A known race, left as it is.** `bindTCP` checks the proxy's port under `mu`,
while the proxy binds under `proxyMu`, and neither holds a lock across the other.
A TCP forward asked for the proxy's port at the very moment the first route binds
it can therefore lose the race and be refused with a generic port-in-use
sentence instead of the one that names `network.proxy_port`. Nothing is lost and
the window is a single `net.Listen`; closing it would mean holding `proxyMu`
across every TCP bind, which is worse than the message.

**Limits, stated rather than discovered.** HTTP only — `https://app.localhost`
needs a certificate the browser trusts, which is one more elevation. The port
stays in the URL (80 is privileged). A name outside `.localhost` does not
resolve. A client with its own resolver is not covered. The proxy adds no CORS
header: it makes nothing reachable that was not already on the loopback.

**Not yet measured:** browsers on Windows and macOS, and the `::1`-then-IPv4
fallback (the proxy binds IPv4 only). See §3.74.

Four decisions, each with a test:

- **Loopback only.** A forward binds `127.0.0.1`. `0.0.0.0` would put on the LAN a
  service its owner kept off it. The test asserts the listener's own address: a
  second bind cannot prove it, since `0.0.0.0` and `127.0.0.1` collide on a port
  in both directions.
- **Below 1024 is refused before the syscall**, and before the target is probed.
  There is no unprivileged way around it on Unix, and `permission denied` reads
  like something a retry would fix.
- **The target is dialled once before the port opens.** Otherwise the bind
  succeeds, the row reads healthy, and the failure only surfaces at the first
  client. Refusals are sentinels (`ErrPrivilegedPort`, `ErrPortInUse`,
  `ErrTargetUnreachable`), and `app.forwardRefusal` maps each to a sentence —
  the wording never comes from the operating system.
- **The registry has a mutex; `jobs.Registry` deliberately does not.** `jobs` is
  only written from `Update`. Here the accept and connection goroutines write the
  counters `Update` reads. The lock is never held across I/O, and a test closes a
  forward while a connection is live.

**A container's own address is not a target on Docker Desktop.** Measured on
Windows: `172.17.0.3:80` for an unpublished `nginx` gives `i/o timeout`. The
address is real inside the Linux VM and routes nowhere outside it — the same
mechanism as D55 for the Ports tab. It works on native Linux. The probe is what
turns that into a named refusal instead of a healthy-looking dead row, and it is
why the container pre-fill was not built (§3.1).

A forward records its local port and its target, and nothing about where the
target came from. It briefly carried a `Label` for a container name, with a
`Source` column to show it; both were removed because the pre-fill that would
have filled them was not built, so the column only ever read `-`.

## The interfaces — `internal/netiface`

This machine's network interfaces, read **in this process**. `List(ctx)` is the
whole interface.

**It exists because the old one answered for the wrong machine** (D57), and it
is `internal/ports`' story exactly one screen over. The Topology tab ran
`ip addr`, `ip -s link`, `ip route`, `ip neigh` and `iptables` in containers
started with `--network host`, which on Docker Desktop is the Linux VM's
namespace: the tab showed `eth0 10.254.254.3`, `docker0` and the `br-*` while
the machine had `Ethernet 2`, ProtonVPN, Tailscale and **two competing default
routes**. Not one interface and not one route in common. Under Linux the defect
did not exist, which is why it went unnoticed.

No new dependency: `net.Interfaces()` is the standard library and
`gopsutil/v4/net` was already imported. Measured here: 10 interfaces in 4,6 ms,
10 counter rows in 2,6 ms, and **0 of 10 interfaces without a matching counter
row** — the names agree character for character, so there is no correspondence
table to keep.

**The addresses are split by family, one column each** (§3.49). The split
happens in `List`, where each address is still a `net.IP` and the family is a
fact — `To4()` answers for an IPv4-mapped address as well as for a plain one,
which is right, it *is* an IPv4 address. A view splitting `AddressList()` again
would be parsing text this package produced, and would have to decide what an
unparseable entry means: a question that only exists once the type has been
thrown away. Hence `IPv4 []string` and `IPv6 []string` rather than one
`Addresses`.

Two consequences in the view:

- **Both columns follow their content and both carry a flex weight.** The flex
  is what levels them against each other when there is a shortfall — a table
  reclaims from the widest content column first — and with the flex on IPv6
  alone it absorbed the whole shortfall and rendered at **zero width, header
  included, from about 100 columns down**.
- **The cliff below 88 columns is gone** (§3.45). It was written down here
  rather than fixed, because `MinWidth` was an ask and giving the solver a floor
  would have changed every table in the application; §3.45 changed every table
  in the application. What gives way now is MTU, then MAC, then the two
  counters — the columns that declare `Optional` — and each is removed whole, so
  it hands back its padding as well as its width. At 80 columns the addresses
  get 18 and 29 cells instead of nothing.

Three decisions, each with a test:

- **`RxErrors` and `TxErrors` are `*uint64`.** The counters come from a second
  source that fails on its own, and a zero written because nobody looked is
  indistinguishable from an interface that has dropped nothing. `nil` renders as
  `-`, never `0` — D58 at the scale of a column, and `SecretVerdict`'s `*bool`
  under another name.
- **A counter failure does not fail the listing.** `List` returns an error only
  when the interfaces themselves could not be read: a list without its error
  counts is still the answer to "which adapters does this machine have".
- **An MTU the platform does not report is withheld.** Windows returns `-1` for
  its loopback pseudo-interface where `ip` returns 65536; `HasMTU` is what keeps
  a cell from printing a number the machine never meant.

**Three sections were removed rather than translated**, and §3.44 measures why
for each: the ARP cache loses IPv6 and four of its six states outside netlink,
the firewall's `{Name, Policy, Rules}` is an iptables shape that Windows profiles
and `pf` do not fit, and the routing *table* became a routing *question* — see
the `route` stage below.

**The tab is named after what it shows.** At one section "Topology" described
nothing, and deleting it would have meant rehousing the interfaces in Diagnostics
or Ports, where neither has room. One consequence has a test: the table has a
search box where the old tab had no input at all, so `InEditMode()` had to stop
returning a hardcoded `false` — otherwise a `:` typed into the query opens the
command line (Rule 111).

