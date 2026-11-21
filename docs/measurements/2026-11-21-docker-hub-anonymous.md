# What DevDesk does on an anonymous Docker Hub entry: search, tags, pull, unreachable Hub (DEVDESK-669, S-04a)

Measurement note of 2026-11-21 (run between 18:59 and 19:10 UTC). No production file was changed by the
measure. The code under test is `main` at `1a20aee9` (release 2.1.0). A dated measure is never corrected
afterwards: if Docker Hub or the code moves these results, add a new note. Docker Hub's search API is not a
public contract (`internal/imagesearch/hub.go`), so this is a snapshot.

| | |
|---|---|
| Subject | a context whose only registry entry is `url: docker.io`, `slug: docker-io`, `kind: registry`, `auth_mode: anonymous`, no username, no alias (decision D-E of `docs/product/default-registries/stories.md`) |
| Method | the real `RegistryBrowser` of `internal/ui/oci_resources` (the model behind `:oci`, `b`), driven by a throwaway `go test` file placed in that package for the run and deleted afterwards (not committed; the real keys `b`, typed name, `enter`, `down`, `right` were fed to `Update`, the returned `Cmd`s were run for real, the answers fed back, `Render()` and `RenderFooter()` read). Production searcher `defaultImageSearcher` (`ByRegistry(HubSearcher, CatalogSearcher)`), production tag lister `oci.ListRegistryTagsContext`, production `fetchDockerHubTagsMeta`, production `pullOneImageCmd` with `imagepull.Default(cfg, nil)` |
| Isolation | temporary `HOME` (the test binary's `TestMain`), empty `DOCKER_CONFIG`, no proxy variable except in the unreachable run, no `docker login` used, no TUI started on `~/.devdesk`. An anonymous entry sends no credentials (`credsFor` returns none for it) |
| Machine | Linux 6.12 (Debian 13), go1.27.4, `docker` CLI present, Docker daemon NOT reachable (see criterion 2), `podman` absent |
| Requests | every HTTP request of the Go code went through a counting `http.DefaultTransport` wrapper with a hard cap of 24; 18 reached Docker Hub, none answered 429; cosign was not run (see criterion 2) |
| Rate-limit headers | see the request table. The source IP in `docker-ratelimit-source` is left out of this note |

## The 18 requests that reached Docker Hub

| # | Scenario | Request | Status | `ratelimit-*` seen |
|---|---|---|---|---|
| 1 | `docker.io`, search `nginx` | `GET https://index.docker.io/v1/search?n=50&q=nginx` | 200 | none |
| 2 | `docker.io`, tags of `library/nginx` (from the row, `right`) | `GET https://registry-1.docker.io/v2/library/nginx/tags/list` | 401 (`Bearer realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:library/nginx:pull"`) | `limit 1500;w=60`, `remaining 1499;w=60` |
| 3 | idem | `GET https://auth.docker.io/token?scope=repository:library/nginx:pull&service=registry.docker.io` | 200 | none |
| 4 | idem | `GET https://registry-1.docker.io/v2/library/nginx/tags/list` | 200 (1409 tags) | `limit 1500;w=60`, `remaining 1499;w=60` |
| 5 | idem | `GET https://hub.docker.com/v2/repositories/library/nginx/tags?page_size=100&ordering=last_updated` | 200 (100 dates) | none |
| 6 | `registry-1.docker.io`, search `nginx` | same as 1 | 200 | none |
| 7 to 10 | `registry-1.docker.io`, typed `library/nginx` (tag flow) | same four as 2 to 5 | 401, 200, 200, 200 | `remaining 1498;w=60` |
| 11 | `index.docker.io`, search `nginx` | `GET https://index.docker.io/v2/_catalog?n=1000` | 401 (`scope="registry:catalog:*"`) | `limit 1500;w=60`, `remaining 1497;w=60` |
| 12 | idem | `GET https://auth.docker.io/token?scope=registry:catalog:*&service=registry.docker.io` | 400 | none |
| 13 | `index.docker.io`, typed `library/nginx` | `GET https://index.docker.io/v2/library/nginx/tags/list` | 401 | `limit 1500;w=60`, `remaining 1497;w=60` |
| 14 | idem | `GET https://auth.docker.io/token?scope=repository:library/nginx:pull&service=registry.docker.io` | 200 | none |
| 15 | idem | `GET https://index.docker.io/v2/library/nginx/tags/list` | 200 (1409 tags) | `limit 1500;w=60`, `remaining 1496;w=60` |
| 16 | pull of `hello-world`, digest | `HEAD https://registry-1.docker.io/v2/library/hello-world/manifests/latest` | 401 | none |
| 17 | idem | `GET https://auth.docker.io/token?scope=repository:library/hello-world:pull&service=registry.docker.io` | 200 | none |
| 18 | idem | `HEAD https://registry-1.docker.io/v2/library/hello-world/manifests/latest` | 200 | `limit 100;w=3600`, `remaining 100;w=3600` |

Tag meta requests (5, 9) answered 200; for `index.docker.io` no `hub.docker.com` request is made (see criterion 5).
The `limit 1500;w=60` headers are a per-minute request budget on the registry API; the pull quota appears only
on the manifest answer (18): `100` with the window `w=3600` (the research note, `research.md`, says 100 pulls
per 6 hours, which would read `w=21600`; this is what the header said on this day). A `HEAD` did not decrement
`remaining` (still 100), as Docker documents.

## Criterion 1: image search on the anonymous entry (`nginx`)

Verdict: OK.

Code path actually taken (`defaultImageSearcher`, `internal/ui/oci_resources/browser_flow.go:72`):
`imagesearch.ByRegistry` tests `oci.IsDockerHub(t.RegistryURL)`; `docker.io` is Docker Hub, so the
`HubSearcher` (`internal/imagesearch/hub.go`) runs, one anonymous `GET https://index.docker.io/v1/search?n=50&q=nginx`
(request 1). `_catalog` is NOT used for this spelling and `internal/oci/hub.go` only provides the spelling
test (`IsDockerHub`). The target carried no username or password (anonymous entry).

Observed: HTTP 200, 50 results (`HubLimit`, one page, no pagination), `partial=false`, outcome
`ok`. The first Hub row is `nginx` (`Repo library/nginx`, official, 21425 stars, "Official build of Nginx."), then
`nginx/nginx-ingress` (124), `nginx/nginx-prometheus-exporter` (53), and so on. The level 1 table shows Name,
Registry (`docker.io`), Stars, Official, Description; the table lists `nginx` first, then the others alphabetically by name. No credentials,
no prior configuration. Footer on the selected row: `Image docker.io/library/nginx on docker.io`.

Side observation: Hub returned exactly 50 results and the search reports `partial=false`; the user is not told
there may be more than 50 (not a contradiction of the criterion).

## Criterion 2: tags of `library/nginx`, then pull of `hello-world`

Verdict: tags OK; pull NOT verified end to end (environment limit, not a DevDesk finding).

Tags (requests 2 to 5). Opened with `right` on the `nginx` row: `startTagRequests` calls
`oci.ListRegistryTagsContext` on `registryAPIURL("docker.io")` = `https://registry-1.docker.io`: the first
`GET .../v2/library/nginx/tags/list` answers 401 with a Bearer challenge, the anonymous token comes from
`auth.docker.io/token` (no credentials, scope `repository:library/nginx:pull`, answers 200), the retry answers 200 with
1409 tags in one page. Then `loadMultiRegistryTagsMetaCmd` asks `hub.docker.com/v2/repositories/library/nginx/tags?page_size=100&ordering=last_updated`
(200) for dates. Observed in the tag table: 1409 rows, sorted by tag, `Updated` filled for 100 of them (1, 1-alpine,
1-alpine-otel, ... "1 day ago", "2 days ago") and `-` for the others (1-alpine3.17, 1-bookworm, ...). The dates cover only the 100 most recently
updated tags, which is how `fetchDockerHubTagsMeta` is written (`page_size=100`, one request); it is a limit, not a failure.

Pull (requests 16 to 18). `pullOneImageCmd("library/hello-world:latest", imagepull.Default(cfg, nil))`, the
production wiring. The reference handed to the engine is `library/hello-world:latest` (`multiImageName`, engine
docker). Signature verification is on by default (`scan.image_verification`), and `imagepull.Pull` first resolves
the tag to a digest: `HEAD https://registry-1.docker.io/v2/library/hello-world/manifests/latest`, 401, anonymous
token, 200 (quota header `100;w=3600`, remaining 100). No policy matched and nothing local to continue from: verdict
`no-policy`, decision allow, cosign never ran. The next step is `docker pull` through the engine, and it
failed: `RegistryPullCompleteMsg.Err` = `docker pull failed: permission denied while trying to connect to the docker API at unix:///var/run/docker.sock`.
This account is not in the `docker` group (`/var/run/docker.sock` is `root:docker 660`), `podman` is absent, and no
`sudo` or other workaround was used. So the anonymous `docker pull` itself was not run here, the local image
store was not touched and no `docker rmi` was needed. What this run proves: DevDesk reaches the pull step on an
anonymous entry with no login, no credential and a working digest/verification step; what it does not prove: that the
engine pulls `hello-world` through that reference. See "Blocking questions".

## Criterion 3: Hub unreachable

Verdict: partial gap (the entry stays, an error shows on search, nothing shows on a typed repository).

Run: the same flow with `HTTPS_PROXY=HTTP_PROXY=http://127.0.0.1:1` (closed port), first use of the entry, real
code. Two requests were attempted and failed at `proxyconnect tcp: dial tcp 127.0.0.1:1: connect: connection refused`; none reached Docker Hub.

| First use | Observed |
|---|---|
| Search `nginx` | `HubSearcher` error `hub search: Get "https://index.docker.io/v1/search?n=50&q=nginx": proxyconnect tcp: ... connection refused`; outcome `failed`; the empty table stays (Rule 139), the header gets `Failed = docker.io`; the footer stays EMPTY (no message), because `registryFailureReason` returns "" for anything but a refused token realm, an upstream-blocked proxy or an `oci.NoAnswerError`. The cause goes to the journal only (`ERROR [oci_resources] image search on docker.io`) |
| Typed `library/nginx` (tag flow from the form) | tags error `GET https://registry-1.docker.io/v2/library/nginx/tags/list: ... connection refused`; empty tag table, header only; the footer stays EMPTY (journal only: `handleMultiRegistryTagsFailed` shows a footer only when `tagsFromSearch` or when the reason is non-empty) |
| Hub reachable but silent (timeout) | measured locally against a server that never answers, no Hub traffic: `hub search: Get "...": context deadline exceeded`, `registryFailureReason` = "". The catalog path wraps a deadline as `oci.NoAnswerError` ("no answer within 15 s", footer text), the Hub path does not |
| `right` on a search row, tags fail | not run; by reading `handleMultiRegistryTagsFailed`: footer `Could not load tags`, with a reason only when `registryFailureReason` has one |

The entry is never removed or modified in any of these cases (nothing in the paths touches the configuration).

## Criterion 4: gaps against criteria 1 to 3, and what to decide

| # | Criterion | Step | Observed | Proposed follow-up |
|---|---|---|---|---|
| G1 | 3 | search with the Hub unreachable | no footer message; only `Failed  docker.io` in the header; a timeout has no reason either (`no answer within N s` exists for the catalog path only) | story "Explain why an anonymous Docker Hub search failed": make `HubSearcher` (and the typed-repository tag flow) return errors that `registryFailureReason` can word (network error, timeout), footer at level Error (Rule 128) |
| G2 | 3 | typed `library/nginx` with the Hub unreachable | nothing shown at all (empty table, empty footer) | same story as G1, or a bug: a failed tag listing opened from the form must say something |
| G3 | 2 | pull on this machine | engine pull not run (`docker.sock` permission denied) | not a code defect; the check is to be repeated on a machine where the account can reach the engine (decision: who runs it) |
| G4 | 2 (side) | dates of the tags | only the 100 most recent tags carry `Updated`; 1309 of 1409 show `-` | optional story on the Hub tags metadata (pagination of `hub.docker.com/v2`), no decision needed for S-04a |

The decision belongs to the user: for G1 and G2, either (a) fix the code (the Hub search and tag flow word
their errors; S-04b already covers the 429 case, the same function `registryFailureReason` would be extended), or
(b) revisit `auth_mode` (this has no influence on these gaps: an anonymous entry and a credentialed one fail the same way when the network is
down), or (c) revisit variant A (writing Docker Hub into every new context): the entry works for search and tags, so these
measurements give no reason to give it up. The criterion 3 text accepts "impact faible"; the gaps are about the quality of the message, not about
the entry's presence.

Not a gap but worth S-05's attention: anonymous quota 100 pulls with `w=3600` on this day, a `HEAD` is not counted,
and the search (`index.docker.io/v1/search`) and `hub.docker.com` calls carried no `ratelimit-*` header.

## Criterion 5: URL spellings

Each spelling was run as the only entry of a context, with the same real flow.

| Entry `url` | `IsDockerHub` | Search `nginx` | Tags `library/nginx` | Dates | Result |
|---|---|---|---|---|---|
| `docker.io` | yes | `HubSearcher`, 200, 50 rows, outcome ok, label `docker.io` | `registry-1.docker.io`, 1409 tags | 100 dates (`hub.docker.com`) | works; confirms D-E |
| `registry-1.docker.io` | yes | identical request and rows; Registry column shows `registry-1.docker.io` (the URL is the label when the alias is empty) | identical (typed `library/nginx`), 1409 tags | 100 dates | works, same behavior, a longer label |
| `index.docker.io` | NO | `CatalogSearcher`: `GET https://index.docker.io/v2/_catalog?n=1000` 401, then token for `registry:catalog:*` answers 400 (`*oci.StatusError`, `token endpoint returned 400`), outcome `failed`, 0 rows, no footer text (the header line was not printed in that run; it is the same `Failed` class as in the unreachable run) | `https://index.docker.io/v2/library/nginx/tags/list` works (401, token 200, 200), 1409 tags | none: `loadMultiRegistryTagsMetaCmd` skips non-Hub URLs, the `Updated` column is all `-` | search fails, tags work without dates; not suitable |

D-E (`docker.io`) is confirmed: it is the only spelling that gives the search, the tags and their dates under the
standard Hub routing (`oci.IsDockerHub`), with the plain label `docker.io`. `registry-1.docker.io` behaves the same
with a different label; `index.docker.io` is the one that does not work for search, as the decision already said.
Not measured: the pull reference built for `index.docker.io` (`oci.RegistryHost(url)/repo:tag`), only read in
`multiImageName`.

## Blocking questions

1. Pull end to end (criterion 2): the account has no access to the Docker daemon. Should the user run `docker pull hello-world` (and `docker rmi` if the
   image was absent) or a DevDesk pull on a machine with the engine, and add the result to a new note? Podman is not installed.
