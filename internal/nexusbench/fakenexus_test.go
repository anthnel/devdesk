package nexusbench

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// This file holds the in-memory Nexus the Provision tests run against: an
// httptest server on the loopback (never a port reserved by hand) that keeps the
// state a provisioning touches. It is the single place that knows the shape of
// the REST payloads; every one of them is a hypothesis (H-REST) taken from the
// Sonatype Nexus Repository REST API documentation, to be checked against the
// instance's /service/rest/swagger.json at step 10 and fixed here and in the
// client together:
//
//	GET  /service/rest/v1/status                      Server: Nexus/<version> (<EDITION>), no auth
//	GET  /service/rest/swagger.json                   {"info":{"version":...}}, no auth, optional
//	GET  /service/rest/v1/system/eula                 {"accepted":bool,"disclaimer":string}
//	POST /service/rest/v1/system/eula                 {"accepted":true,"disclaimer":<as received>}  -> 204
//	GET  /service/rest/v1/blobstores                  [{"name":"default","type":"File"}]
//	GET  /service/rest/v1/security/realms/active      ["NexusAuthenticatingRealm",...]
//	PUT  /service/rest/v1/security/realms/active      the same array                                -> 204
//	GET  /service/rest/v1/security/anonymous          {"enabled","userId","realmName"}
//	PUT  /service/rest/v1/security/anonymous          the same object                               -> 200
//	GET  /service/rest/v1/security/roles/{id}         {"id","name","description","privileges","roles"} or 404
//	POST /service/rest/v1/security/roles              the same object                               -> 200
//	PUT  /service/rest/v1/security/roles/{id}         the same object                               -> 204
//	GET  /service/rest/v1/security/users[?userId=x]   [{"userId","firstName","lastName","emailAddress","status","roles",...}]
//	POST /service/rest/v1/security/users              the same plus "password"                      -> 200
//	PUT  /service/rest/v1/security/users/{id}         the same without a password effect            -> 204
//	PUT  /service/rest/v1/security/users/{id}/change-password   text/plain, the new password        -> 204
//	GET  /service/rest/v1/repositories/docker/{hosted,proxy,group}/{name}   the configuration or 404
//	POST /service/rest/v1/repositories/docker/{kind}  the configuration                            -> 201
//	                                                  a proxy whose host does not resolve           -> 400, see BlockUpstream
//	PUT  /service/rest/v1/repositories/docker/{kind}/{name}  the configuration                      -> 204
//	     a successful PUT of docker-proxy lifts the "blocked" state, see AutoBlockProxy        (DEVDESK-534, NX-02b)
//	DELETE /service/rest/v1/repositories/{name}        any kind, no format segment                  -> 204, 404 when unknown
//	     the repository and its components go together, unless KeepComponentsAfterDelete simulates a
//	     storage that empties late. H-REPODEL, to settle at step 0 of NX-02b: the real answer codes, and
//	     whether a repository recreated under the same name lists no component at once.
//	POST /service/rest/v1/repositories/{name}/invalidate-cache   proxy and group                     -> 204
//	     404 for an unknown name, 400 for a hosted repository (it has no cache). Empties the negative
//	     cache (MarkNegativeCached). H-INVAL, to settle at step 0.
//	GET  /service/rest/v1/components?repository=<name>[&continuationToken=<t>]                    (DEVDESK-541, NX-02a)
//	     {"items":[{"id","repository","format":"docker","name","version","assets":[{"path","checksum":{"sha1","sha256"}}]}],
//	     "continuationToken":<opaque string>|null}; 404 for a repository that does not exist, 400 without
//	     the repository parameter or with a token it did not issue. H-COMPONENTS, to settle at step 12:
//	     a Docker component is one image tag (name = the image name, version = the tag), and the sha256
//	     checksum of its asset under v2/<name>/manifests/<tag> is the digest of the manifest. The
//	     component lists the blob assets first, so a client that takes the first asset gets a wrong digest.
//	GET  /service/rest/v1/security/ssrf-protection    {"enabled","allowedIPs","allowedDomains"}            (DEVDESK-505, observed on 3.98.1-01)
//	PUT  /service/rest/v1/security/ssrf-protection    the same object                                      -> 200 with the object
//	     The fake keeps every field of the body it does not know and gives it back (SetSSRFField), as an
//	     instance of another version could have more fields than the three above.
//	     While enabled, a proxy whose remote URL host is a private one (see isPrivateUpstreamHost: a cluster
//	     Service name or a private IP address) and is in neither allowed list is refused with a 400 on
//	     proxy.remoteUrl ("Proxy URL blocked: Host resolves to private/local IP address(es)").
//
// Repository configuration, as the fake demands it (a missing key is a 400):
//
//	all:    name, online, storage{blobStoreName, strictContentTypeValidation},
//	        docker{v1Enabled, forceBasicAuth}
//	hosted: + storage.writePolicy
//	proxy:  + proxy{remoteUrl, contentMaxAge, metadataMaxAge}, dockerProxy{indexType}
//	group:  + group{memberNames}, docker.httpPort
//
// The userId filter of the users list matches by substring, like the real
// service (hypothesis), and the instance holds a decoy user whose id contains
// "bench-pull": a client that takes the first element of the answer instead of
// the exact id is caught.

// Paths of the fake, shared with the tests.
const (
	pathAPIRoot       = "/service/rest/v1"
	pathSwagger       = "/service/rest/swagger.json"
	pathStatus        = pathAPIRoot + "/status"
	pathEULA          = pathAPIRoot + "/system/eula"
	pathBlobStores    = pathAPIRoot + "/blobstores"
	pathRealms        = pathAPIRoot + "/security/realms/active"
	pathAnonymous     = pathAPIRoot + "/security/anonymous"
	pathRoles         = pathAPIRoot + "/security/roles"
	pathUsers         = pathAPIRoot + "/security/users"
	pathRepositories  = pathAPIRoot + "/repositories"
	pathComponents    = pathAPIRoot + "/components"
	pathSSRF          = pathAPIRoot + "/security/ssrf-protection"
	pathAdminPassword = pathUsers + "/admin/change-password"

	fakeServerHeader = "Nexus/3.98.1-01 (COMMUNITY)"
	// fakeInitialAdmin is the password of a fresh instance (the one in
	// /nexus-data/admin.password); it carries the marker so that any leak of it
	// is caught by the same check as every other secret.
	fakeInitialAdmin = secretMarker + "-initial-admin"
	// fakeDisclaimer has characters a careless re-encoding would alter; the
	// client has to send it back as received.
	fakeDisclaimer = "Use of Nexus Repository Community Edition is governed by the \"EULA\" <https://example.invalid/eula> & its terms.\nSecond line, with é and ü."
)

// repoPath is the REST path of a Docker repository kind, with the name when given.
func repoPath(kind, name string) string {
	p := pathRepositories + "/docker/" + kind
	if name != "" {
		p += "/" + name
	}
	return p
}

// orderLog is the ordered journal of what happened, shared by the fake Nexus
// ("HTTP <method> <path>") and the fake secret store ("STORE Save pending ..."),
// so that a test can prove which came first.
type orderLog struct {
	mu      sync.Mutex
	entries []string
}

func (l *orderLog) add(entry string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, entry)
}

func (l *orderLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.entries)
}

// indexFuncFrom returns the index of the first entry at or after start that
// match accepts, or -1.
func (l *orderLog) indexFuncFrom(start int, match func(string) bool) int {
	for i, e := range l.snapshot() {
		if i >= start && match(e) {
			return i
		}
	}
	return -1
}

// index returns the index of the first entry equal to entry, or -1.
func (l *orderLog) index(entry string) int {
	return l.indexFuncFrom(0, func(e string) bool { return e == entry })
}

// fakeReq is one request the fake Nexus received.
type fakeReq struct {
	Method, Path, Query string
	// User is the Basic auth user ("" when none); EmptyPassword is true when
	// the request carried Basic auth with an empty password.
	User          string
	EmptyPassword bool
	Body          string
}

// fakeFault is an injected failure, matched on the method ("" = any) and the
// path prefix. Exactly one of status, size and drop is set.
type fakeFault struct {
	method, prefix string
	status         int    // answer this status (with Location when location is set)
	location       string // redirect target
	body           string // answer this raw body, as the validation errors of Nexus do
	size           int    // pad the body of a 200 GET to this many bytes with trailing spaces
}

func (ft fakeFault) matches(r *http.Request) bool {
	return (ft.method == "" || ft.method == r.Method) && strings.HasPrefix(r.URL.Path, ft.prefix)
}

// fakeComponent is one image tag held by a repository. Digest is "sha256:<hex>"
// of the manifest, or "" for a component whose manifest asset is not listed.
type fakeComponent struct {
	Name, Version, Digest string
}

// defaultComponentPageSize is how many components the fake lists per page until
// SetComponentPageSize changes it.
const defaultComponentPageSize = 3

// componentTokenPrefix starts every continuation token the fake issues. The rest
// of a token holds characters a URL has to escape (+ / = & ?), so a client that
// pastes the token into the query unescaped is caught.
const componentTokenPrefix = "page+"

type fakeUser struct {
	UserID, FirstName, LastName, EmailAddress, Status string
	Roles                                             []string
	Password                                          string
}

// fakeNexus is the in-memory instance. All its state is behind mu.
type fakeNexus struct {
	t   *testing.T
	srv *httptest.Server
	log *orderLog

	mu              sync.Mutex
	serverHeader    string
	swaggerVersion  string
	swaggerRaw      string // a 200 answer that is not JSON; wins over swaggerVersion
	eulaPresent     bool
	eulaAccepted    bool
	blobStores      []string
	realms          []string
	anonymous       map[string]any
	users           map[string]*fakeUser
	roles           map[string]map[string]any
	repos           map[string]map[string]map[string]any // kind -> name -> configuration
	faults          []fakeFault
	upstreamBlocked bool
	ssrfPresent     bool
	ssrf            ssrfSettings
	ssrfExtra       map[string]json.RawMessage // fields of the settings beyond ssrfSettings
	components      map[string][]fakeComponent // repository name -> components, in listing order
	componentPage   int
	loopTokens      map[string]bool // repositories whose listing never ends
	proxyBlocked    bool            // AutoBlockProxy: docker-proxy is auto-blocked until a PUT of it
	negativeCached  map[string]bool // repositories whose negative cache holds an entry
	keepAfterDelete int             // components listings that still show the old ones after a DELETE
	staleReads      map[string]int  // repository name -> listings left that show the components of a deleted one
	laterPageFails  int             // status answered to any page that carries a continuation token; 0 = none
	dropAll         bool
	reqs            []fakeReq
	secretsSeen     map[string]bool
}

// newFakeNexus starts a fresh instance: the admin account with the initial
// password, the EULA not accepted, the default blob store, no Docker repository.
func newFakeNexus(t *testing.T, log *orderLog) *fakeNexus {
	t.Helper()
	f := &fakeNexus{
		t:            t,
		log:          log,
		serverHeader: fakeServerHeader,
		eulaPresent:  true,
		blobStores:   []string{"default"},
		realms:       []string{"NexusAuthenticatingRealm"},
		anonymous:    map[string]any{"enabled": false, "userId": "anonymous", "realmName": "NexusAuthorizingRealm"},
		users: map[string]*fakeUser{
			"admin":        {UserID: "admin", FirstName: "Administrator", LastName: "User", EmailAddress: "admin@example.org", Status: "active", Roles: []string{"nx-admin"}, Password: fakeInitialAdmin},
			"anonymous":    {UserID: "anonymous", FirstName: "Anonymous", LastName: "User", EmailAddress: "anonymous@example.org", Status: "active", Roles: []string{"nx-anonymous"}},
			"a-bench-pull": {UserID: "a-bench-pull", FirstName: "Decoy", LastName: "User", EmailAddress: "decoy@example.org", Status: "active", Roles: []string{}},
		},
		roles: map[string]map[string]any{
			"nx-admin":     {"id": "nx-admin", "name": "nx-admin", "privileges": []any{"nx-all"}, "roles": []any{}},
			"nx-anonymous": {"id": "nx-anonymous", "name": "nx-anonymous", "privileges": []any{"nx-healthcheck-read"}, "roles": []any{}},
		},
		// A new 3.98 instance has SSRF protection on, with both lists empty.
		ssrfPresent:    true,
		ssrf:           ssrfSettings{Enabled: true, AllowedIPs: []string{}, AllowedDomains: []string{}},
		repos:          map[string]map[string]map[string]any{"hosted": {}, "proxy": {}, "group": {}},
		secretsSeen:    map[string]bool{},
		components:     map[string][]fakeComponent{},
		componentPage:  defaultComponentPageSize,
		loopTokens:     map[string]bool{},
		negativeCached: map[string]bool{},
		staleReads:     map[string]int{},
	}
	f.srv = httptest.NewServer(f)
	t.Cleanup(f.srv.Close)
	return f
}

// URL is the base URL of the instance, without a path.
func (f *fakeNexus) URL() string { return f.srv.URL }

// ---- fault and fixture setters -------------------------------------------

// Forbid answers status to every request that matches method ("" = any) and prefix.
func (f *fakeNexus) Forbid(method, prefix string, status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.faults = append(f.faults, fakeFault{method: method, prefix: prefix, status: status})
}

// ForbidWithBody is Forbid with a raw answer body, in the content type of the
// validation errors of Nexus.
func (f *fakeNexus) ForbidWithBody(method, prefix string, status int, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.faults = append(f.faults, fakeFault{method: method, prefix: prefix, status: status, body: body})
}

// RedirectTo answers status (a 3xx) with a Location header to the matching requests.
func (f *fakeNexus) RedirectTo(method, prefix string, status int, location string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.faults = append(f.faults, fakeFault{method: method, prefix: prefix, status: status, location: location})
}

// Oversize pads the body of every 200 GET under prefix with trailing spaces to
// exactly size bytes: the content stays valid JSON, so a client that decoded
// it before checking the size would succeed.
func (f *fakeNexus) Oversize(prefix string, size int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.faults = append(f.faults, fakeFault{method: http.MethodGet, prefix: prefix, size: size})
}

// BlockUpstream makes the instance refuse a proxy repository the way the real
// one does when it cannot resolve the host of the remote URL: a 400 whose body
// is a validation error list on proxy.remoteUrl (observed on 3.98.1-01, see
// "First real run" in docs/architecture/nexus-bench.md). ResolveUpstream undoes it.
func (f *fakeNexus) BlockUpstream() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.upstreamBlocked = true
}

// ResolveUpstream makes the host of the remote URL resolvable again.
func (f *fakeNexus) ResolveUpstream() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.upstreamBlocked = false
}

// refuseUnresolvedUpstream answers the 400 of a proxy remote URL Nexus will not
// accept and reports whether it did: a host that does not resolve (BlockUpstream),
// or, while SSRF protection is on, a private host that is not allowed. The content
// type is the one Nexus sends.
func (f *fakeNexus) refuseUnresolvedUpstream(w http.ResponseWriter, kind string, cfg map[string]any) bool {
	if kind != "proxy" {
		return false
	}
	remote, _ := jsonAt(cfg, "proxy", "remoteUrl").(string)
	message := ""
	switch {
	case f.upstreamBlocked:
		message = "Proxy URL blocked: Failed to resolve host: " + remote
	case f.ssrfRefuses(remote):
		message = "Proxy URL blocked: Host resolves to private/local IP address(es): 10.110.133.156 (private network address). " +
			"To allow connections, update the SSRF protection settings via the API at /v1/security/ssrf-protection."
	default:
		return false
	}
	body := []map[string]any{{"id": "HelperBean.proxy.remoteUrl", "message": message}}
	data, _ := json.Marshal(body)
	w.Header().Set("Content-Type", "application/vnd.siesta-validation-errors-v1+json")
	w.WriteHeader(http.StatusBadRequest)
	_, _ = w.Write(data)
	return true
}

// ssrfRefuses reports whether the SSRF protection refuses the remote URL: it is
// on, the host is a private one, and neither allowed list names it.
func (f *fakeNexus) ssrfRefuses(remoteURL string) bool {
	if !f.ssrfPresent || !f.ssrf.Enabled {
		return false
	}
	u, err := url.Parse(remoteURL)
	if err != nil || !isPrivateUpstreamHost(u.Hostname()) {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return !slices.ContainsFunc(f.ssrf.AllowedDomains, func(d string) bool { return strings.EqualFold(d, host) }) &&
		!slices.Contains(f.ssrf.AllowedIPs, host)
}

// isPrivateUpstreamHost stands for "the host resolves to a private address": the
// name of a cluster Service (what the bench upstream is) or a private IP literal.
// Any other name (the .invalid hosts of the other tests) counts as public.
func isPrivateUpstreamHost(host string) bool {
	host = strings.ToLower(host)
	if strings.HasSuffix(host, ".svc.cluster.local") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && (ip.IsPrivate() || ip.IsLoopback())
}

// ssrfSettings is the part of /v1/security/ssrf-protection the tests read and set
// (H-SSRF): the fake keeps any other field apart, see SetSSRFField.
type ssrfSettings struct {
	Enabled        bool     `json:"enabled"`
	AllowedIPs     []string `json:"allowedIPs"`
	AllowedDomains []string `json:"allowedDomains"`
}

// SetSSRFField sets a field of the settings the client does not know about.
func (f *fakeNexus) SetSSRFField(name string, value any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, err := json.Marshal(value)
	if err != nil {
		f.t.Fatalf("fake Nexus: SetSSRFField: %v", err)
	}
	if f.ssrfExtra == nil {
		f.ssrfExtra = map[string]json.RawMessage{}
	}
	f.ssrfExtra[name] = data
}

// SSRFField returns a field of the settings beyond the three the client knows,
// decoded, and whether it is there.
func (f *fakeNexus) SSRFField(name string) (any, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, ok := f.ssrfExtra[name]
	if !ok {
		return nil, false
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		f.t.Fatalf("fake Nexus: SSRFField: %v", err)
	}
	return v, true
}

// SetSSRF replaces the SSRF protection settings.
func (f *fakeNexus) SetSSRF(s ssrfSettings) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ssrf = s
}

// SSRF returns the SSRF protection settings.
func (f *fakeNexus) SSRF() ssrfSettings {
	f.mu.Lock()
	defer f.mu.Unlock()
	return ssrfSettings{
		Enabled:        f.ssrf.Enabled,
		AllowedIPs:     slices.Clone(f.ssrf.AllowedIPs),
		AllowedDomains: slices.Clone(f.ssrf.AllowedDomains),
	}
}

// RemoveSSRFEndpoint makes /v1/security/ssrf-protection answer 404, as a version
// that has no such protection.
func (f *fakeNexus) RemoveSSRFEndpoint() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ssrfPresent = false
}

// AutoBlockProxy puts docker-proxy in the state Nexus gives a proxy whose upstream
// failed: auto-blocked. Only a successful PUT of the repository lifts it (H-UNBLOCK,
// to settle at step 0 of NX-02b: whether the PUT is enough, or the cache also has to be
// invalidated). ProxyBlocked reads it back.
func (f *fakeNexus) AutoBlockProxy() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.proxyBlocked = true
}

// ProxyBlocked reports whether docker-proxy is still auto-blocked.
func (f *fakeNexus) ProxyBlocked() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.proxyBlocked
}

// MarkNegativeCached makes the negative cache of a repository hold an entry, as a
// proxy does once its upstream answered "not found" or did not answer. Only
// invalidate-cache empties it.
func (f *fakeNexus) MarkNegativeCached(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.negativeCached[name] = true
}

// NegativeCached reports whether the negative cache of a repository still holds an entry.
func (f *fakeNexus) NegativeCached(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.negativeCached[name]
}

// KeepComponentsAfterDelete makes every repository deleted from now on keep listing
// its old components for n more listings (one per request of the components
// endpoint, a page counts), as a storage that empties late would. The repository
// itself is gone at once; a repository recreated under the same name shows the old
// components until the n listings have passed. This is H-REPODEL being false for n
// reads.
func (f *fakeNexus) KeepComponentsAfterDelete(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keepAfterDelete = n
}

// ComponentCount is how many components the instance holds for a repository name,
// whether or not the repository exists.
func (f *fakeNexus) ComponentCount(repo string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.components[repo])
}

// DropConnections makes the instance close every connection without an answer.
func (f *fakeNexus) DropConnections() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dropAll = true
}

// SetAdminPassword replaces the current admin password.
func (f *fakeNexus) SetAdminPassword(pw string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.users["admin"].Password = pw
}

// RemoveBlobStore makes the named blob store absent.
func (f *fakeNexus) RemoveBlobStore(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.blobStores = slices.DeleteFunc(f.blobStores, func(n string) bool { return n == name })
}

// AcceptEULAAlready marks the EULA as accepted before the run.
func (f *fakeNexus) AcceptEULAAlready() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.eulaAccepted = true
}

// RemoveEULAEndpoint makes /v1/system/eula answer 404, as an edition without one.
func (f *fakeNexus) RemoveEULAEndpoint() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.eulaPresent = false
}

// SetServerHeader sets the Server header of every answer ("" = no header) and
// the version the swagger document reports ("" = no document).
func (f *fakeNexus) SetServerHeader(header, swaggerVersion string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.serverHeader, f.swaggerVersion = header, swaggerVersion
}

// SetSwaggerBody makes the swagger document a 200 answer with this body, which
// is not JSON (a proxy's HTML page, say).
func (f *fakeNexus) SetSwaggerBody(raw string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.swaggerRaw = raw
}

// SetAnonymous sets whether anonymous access is enabled.
func (f *fakeNexus) SetAnonymous(enabled bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.anonymous["enabled"] = enabled
}

// MutateAnonymous changes the anonymous access setting in place.
func (f *fakeNexus) MutateAnonymous(fn func(m map[string]any)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f.anonymous)
}

// MutateRepo changes a stored repository configuration in place.
func (f *fakeNexus) MutateRepo(kind, name string, fn func(cfg map[string]any)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cfg, ok := f.repos[kind][name]
	if !ok {
		f.t.Fatalf("fake Nexus: no %s repository %q to mutate", kind, name)
	}
	fn(cfg)
}

// DeleteRepo removes a repository and the components it held.
func (f *fakeNexus) DeleteRepo(kind, name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.repos[kind], name)
	delete(f.components, name)
}

// AddComponents appends components to the listing of an existing repository.
func (f *fakeNexus) AddComponents(repo string, comps ...fakeComponent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.repoExists(repo) {
		f.t.Fatalf("fake Nexus: no repository %q to add components to", repo)
	}
	f.components[repo] = append(f.components[repo], comps...)
}

// SetComponentPageSize sets how many components one page of the listing holds.
func (f *fakeNexus) SetComponentPageSize(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.componentPage = n
}

// FailLaterComponentPages makes every page after the first one (a request that
// carries a continuation token) answer status.
func (f *fakeNexus) FailLaterComponentPages(status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.laterPageFails = status
}

// LoopComponentToken makes the listing of repo hand out a continuation token on
// every page, forever.
func (f *fakeNexus) LoopComponentToken(repo string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loopTokens[repo] = true
}

// MutateRole changes a stored role in place.
func (f *fakeNexus) MutateRole(id string, fn func(role map[string]any)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	role, ok := f.roles[id]
	if !ok {
		f.t.Fatalf("fake Nexus: no role %q to mutate", id)
	}
	fn(role)
}

// DeleteRole removes a role.
func (f *fakeNexus) DeleteRole(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.roles, id)
}

// MutateUser changes a stored user in place.
func (f *fakeNexus) MutateUser(id string, fn func(u *fakeUser)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[id]
	if !ok {
		f.t.Fatalf("fake Nexus: no user %q to mutate", id)
	}
	fn(u)
}

// DeleteUser removes a user.
func (f *fakeNexus) DeleteUser(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.users, id)
}

// SetRealms replaces the active realms.
func (f *fakeNexus) SetRealms(realms ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.realms = slices.Clone(realms)
}

// ---- observation ------------------------------------------------------------

// Requests returns every request received so far, in order.
func (f *fakeNexus) Requests() []fakeReq {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.reqs)
}

// Count is the number of requests for method and path (without the query).
func (f *fakeNexus) Count(method, path string) int {
	n := 0
	for _, r := range f.Requests() {
		if r.Method == method && r.Path == path {
			n++
		}
	}
	return n
}

// SecretsSeen returns every password the instance ever received: Basic auth
// passwords, new passwords of change-password and passwords of created users.
func (f *fakeNexus) SecretsSeen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.secretsSeen))
	for s := range f.secretsSeen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// AdminPassword is the current admin password of the instance.
func (f *fakeNexus) AdminPassword() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.users["admin"].Password
}

// User returns a copy of a user.
func (f *fakeNexus) User(id string) (fakeUser, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[id]
	if !ok {
		return fakeUser{}, false
	}
	c := *u
	c.Roles = slices.Clone(u.Roles)
	return c, true
}

// Role returns a deep copy of a role.
func (f *fakeNexus) Role(id string) (map[string]any, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.roles[id]
	if !ok {
		return nil, false
	}
	return deepCopy(r), true
}

// Repo returns a deep copy of a repository configuration.
func (f *fakeNexus) Repo(kind, name string) (map[string]any, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.repos[kind][name]
	if !ok {
		return nil, false
	}
	return deepCopy(r), true
}

// RepoCount is the number of Docker repositories of all kinds.
func (f *fakeNexus) RepoCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, byName := range f.repos {
		n += len(byName)
	}
	return n
}

// Realms returns the active realms.
func (f *fakeNexus) Realms() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.realms)
}

// Anonymous returns a deep copy of the anonymous access settings.
func (f *fakeNexus) Anonymous() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return deepCopy(f.anonymous)
}

// EULAAccepted reports whether the EULA is accepted.
func (f *fakeNexus) EULAAccepted() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.eulaAccepted
}

// Snapshot is the whole observable state as JSON, passwords left out (they are
// compared apart, and a failure must not print them).
func (f *fakeNexus) Snapshot() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	users := map[string]any{}
	for id, u := range f.users {
		users[id] = map[string]any{
			"firstName": u.FirstName, "lastName": u.LastName, "emailAddress": u.EmailAddress,
			"status": u.Status, "roles": u.Roles,
		}
	}
	state := map[string]any{
		"users": users, "roles": f.roles, "realms": f.realms, "anonymous": f.anonymous,
		"eulaAccepted": f.eulaAccepted, "blobStores": f.blobStores, "repos": f.repos, "ssrf": f.ssrfBody(),
	}
	data, err := json.Marshal(state)
	if err != nil {
		f.t.Fatalf("fake Nexus: snapshot: %v", err)
	}
	return string(data)
}

// ---- the HTTP handler -------------------------------------------------------

func (f *fakeNexus) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	f.mu.Lock()
	defer f.mu.Unlock()

	user, pass, hasAuth := r.BasicAuth()
	req := fakeReq{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Body: string(body)}
	if hasAuth {
		req.User, req.EmptyPassword = user, pass == ""
	}
	f.reqs = append(f.reqs, req)
	f.log.add("HTTP " + r.Method + " " + r.URL.Path)
	f.noteSecrets(r, pass, body)

	if f.serverHeader != "" {
		w.Header().Set("Server", f.serverHeader)
	}
	if f.dropAll {
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				_ = conn.Close()
			}
		}
		return
	}
	for _, ft := range f.faults {
		if ft.size == 0 && ft.matches(r) {
			if ft.location != "" {
				w.Header().Set("Location", ft.location)
			}
			if ft.body != "" {
				w.Header().Set("Content-Type", "application/vnd.siesta-validation-errors-v1+json")
				w.WriteHeader(ft.status)
				_, _ = w.Write([]byte(ft.body))
				return
			}
			f.reply(w, r, ft.status, map[string]any{"message": "injected fault"})
			return
		}
	}

	switch r.URL.Path {
	case pathSwagger:
		if f.swaggerRaw != "" {
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, f.swaggerRaw)
			return
		}
		if f.swaggerVersion == "" {
			f.reply(w, r, http.StatusNotFound, nil)
			return
		}
		f.reply(w, r, http.StatusOK, map[string]any{"info": map[string]any{"version": f.swaggerVersion}})
		return
	case pathStatus:
		f.reply(w, r, http.StatusOK, nil)
		return
	}
	if !f.authorize(w, r, user, pass, hasAuth) {
		return
	}
	switch p := r.URL.Path; {
	case p == pathEULA:
		f.serveEULA(w, r, body)
	case p == pathBlobStores:
		f.serveBlobStores(w, r)
	case p == pathComponents:
		f.serveComponents(w, r)
	case p == pathRealms:
		f.serveRealms(w, r, body)
	case p == pathAnonymous:
		f.serveAnonymous(w, r, body)
	case p == pathSSRF:
		f.serveSSRF(w, r, body)
	case p == pathRoles || strings.HasPrefix(p, pathRoles+"/"):
		f.serveRoles(w, r, body)
	case p == pathUsers || strings.HasPrefix(p, pathUsers+"/"):
		f.serveUsers(w, r, body)
	case strings.HasPrefix(p, pathRepositories+"/docker/"):
		f.serveRepos(w, r, body)
	case strings.HasPrefix(p, pathRepositories+"/"):
		f.serveRepoByName(w, r)
	default:
		f.reply(w, r, http.StatusNotFound, nil)
	}
}

// noteSecrets remembers every password this request carries.
func (f *fakeNexus) noteSecrets(r *http.Request, basicPass string, body []byte) {
	if basicPass != "" {
		f.secretsSeen[basicPass] = true
	}
	switch {
	case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/change-password"):
		if pw := strings.TrimSpace(string(body)); pw != "" {
			f.secretsSeen[pw] = true
		}
	case r.Method == http.MethodPost && r.URL.Path == pathUsers:
		if m, ok := decodeObject(body); ok {
			if pw, _ := m["password"].(string); pw != "" {
				f.secretsSeen[pw] = true
			}
		}
	}
}

// authorize answers 401 to an unknown credential and 403 to a known user that
// is not the administrator; it reports whether the request may go on.
func (f *fakeNexus) authorize(w http.ResponseWriter, r *http.Request, user, pass string, hasAuth bool) bool {
	u := f.users[user]
	if !hasAuth || u == nil || pass == "" || u.Password != pass {
		w.Header().Set("WWW-Authenticate", `Basic realm="Sonatype Nexus Repository Manager"`)
		f.reply(w, r, http.StatusUnauthorized, nil)
		return false
	}
	if user != "admin" {
		f.reply(w, r, http.StatusForbidden, nil)
		return false
	}
	return true
}

// reply writes status and, when v is not nil, v as JSON. A matching Oversize
// fault pads the body of a 200 GET.
func (f *fakeNexus) reply(w http.ResponseWriter, r *http.Request, status int, v any) {
	var data []byte
	if v != nil {
		data, _ = json.Marshal(v)
		w.Header().Set("Content-Type", "application/json")
	}
	if status == http.StatusOK && r.Method == http.MethodGet {
		for _, ft := range f.faults {
			if ft.size > 0 && ft.matches(r) && len(data) < ft.size {
				data = append(data, strings.Repeat(" ", ft.size-len(data))...)
			}
		}
	}
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

func (f *fakeNexus) badRequest(w http.ResponseWriter, r *http.Request, why string) {
	f.reply(w, r, http.StatusBadRequest, map[string]any{"message": why})
}

func (f *fakeNexus) serveEULA(w http.ResponseWriter, r *http.Request, body []byte) {
	if !f.eulaPresent {
		f.reply(w, r, http.StatusNotFound, nil)
		return
	}
	switch r.Method {
	case http.MethodGet:
		f.reply(w, r, http.StatusOK, map[string]any{"accepted": f.eulaAccepted, "disclaimer": fakeDisclaimer})
	case http.MethodPost:
		m, ok := decodeObject(body)
		if !ok {
			f.badRequest(w, r, "not a JSON object")
			return
		}
		if accepted, _ := m["accepted"].(bool); !accepted {
			f.badRequest(w, r, "accepted must be true")
			return
		}
		if d, _ := m["disclaimer"].(string); d != fakeDisclaimer {
			f.badRequest(w, r, "the disclaimer is not the one the instance serves")
			return
		}
		f.eulaAccepted = true
		f.reply(w, r, http.StatusNoContent, nil)
	default:
		f.reply(w, r, http.StatusMethodNotAllowed, nil)
	}
}

func (f *fakeNexus) serveBlobStores(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		f.reply(w, r, http.StatusMethodNotAllowed, nil)
		return
	}
	list := make([]any, 0, len(f.blobStores))
	for _, n := range f.blobStores {
		list = append(list, map[string]any{"name": n, "type": "File"})
	}
	f.reply(w, r, http.StatusOK, list)
}

// serveComponents lists the components of one repository, a page at a time. The
// token is the offset of the next page behind an opaque prefix; the last page
// carries "continuationToken": null, as Nexus does.
func (f *fakeNexus) serveComponents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		f.reply(w, r, http.StatusMethodNotAllowed, nil)
		return
	}
	q := r.URL.Query()
	repo := q.Get("repository")
	if repo == "" {
		f.badRequest(w, r, "the repository parameter is required")
		return
	}
	if !f.repoExists(repo) {
		f.reply(w, r, http.StatusNotFound, nil)
		return
	}
	if left, stale := f.staleReads[repo]; stale {
		if left <= 0 {
			delete(f.staleReads, repo)
			delete(f.components, repo)
		} else {
			f.staleReads[repo] = left - 1
		}
	}
	offset := 0
	if token := q.Get("continuationToken"); token != "" {
		if f.laterPageFails != 0 {
			f.reply(w, r, f.laterPageFails, map[string]any{"message": "injected fault"})
			return
		}
		n, ok := strings.CutPrefix(token, componentTokenPrefix)
		if !ok {
			f.badRequest(w, r, "unknown continuation token")
			return
		}
		digits, _, _ := strings.Cut(n, "/")
		parsed, err := strconv.Atoi(digits)
		if err != nil || parsed < 0 {
			f.badRequest(w, r, "unknown continuation token")
			return
		}
		offset = parsed
	}
	all := f.components[repo]
	end := min(offset+f.componentPage, len(all))
	offset = min(offset, end)
	items := make([]any, 0, end-offset)
	for _, c := range all[offset:end] {
		items = append(items, c.payload(repo))
	}
	var token any // null on the last page
	if end < len(all) {
		token = componentTokenPrefix + strconv.Itoa(end) + "/=&?"
	} else if f.loopTokens[repo] {
		token = componentTokenPrefix + "0/=&?"
	}
	f.reply(w, r, http.StatusOK, map[string]any{"items": items, "continuationToken": token})
}

// payload is the JSON of a component: the blob assets first (with a checksum
// that is not the manifest's), then the manifest asset when Digest is set.
func (c fakeComponent) payload(repo string) map[string]any {
	blob := func(layer string) map[string]any {
		return map[string]any{
			"path": "v2/" + c.Name + "/blobs/sha256:" + layer, "repository": repo, "format": "docker",
			"checksum": map[string]any{"sha1": strings.Repeat("1", 40), "sha256": layer},
		}
	}
	assets := []any{blob(strings.Repeat("b", 64)), blob(strings.Repeat("c", 64))}
	if c.Digest != "" {
		assets = append(assets, map[string]any{
			"path": "v2/" + c.Name + "/manifests/" + c.Version, "repository": repo, "format": "docker",
			"checksum": map[string]any{"sha1": strings.Repeat("2", 40), "sha256": strings.TrimPrefix(c.Digest, "sha256:")},
		})
	}
	return map[string]any{
		"id": repo + ":" + c.Name + ":" + c.Version, "repository": repo, "format": "docker",
		"group": nil, "name": c.Name, "version": c.Version, "assets": assets,
	}
}

func (f *fakeNexus) serveRealms(w http.ResponseWriter, r *http.Request, body []byte) {
	switch r.Method {
	case http.MethodGet:
		f.reply(w, r, http.StatusOK, f.realms)
	case http.MethodPut:
		var realms []string
		if err := json.Unmarshal(body, &realms); err != nil || len(realms) == 0 {
			f.badRequest(w, r, "the body must be a non-empty array of realm ids")
			return
		}
		f.realms = realms
		f.reply(w, r, http.StatusNoContent, nil)
	default:
		f.reply(w, r, http.StatusMethodNotAllowed, nil)
	}
}

func (f *fakeNexus) serveAnonymous(w http.ResponseWriter, r *http.Request, body []byte) {
	switch r.Method {
	case http.MethodGet:
		f.reply(w, r, http.StatusOK, f.anonymous)
	case http.MethodPut:
		m, ok := decodeObject(body)
		if !ok {
			f.badRequest(w, r, "not a JSON object")
			return
		}
		if _, isBool := m["enabled"].(bool); !isBool {
			f.badRequest(w, r, "enabled must be a boolean")
			return
		}
		if !nonEmptyString(m, "userId") || !nonEmptyString(m, "realmName") {
			f.badRequest(w, r, "userId and realmName are required")
			return
		}
		f.anonymous = m
		f.reply(w, r, http.StatusOK, m)
	default:
		f.reply(w, r, http.StatusMethodNotAllowed, nil)
	}
}

// ssrfBody is the settings as the instance answers them: the three known fields
// and the ones the client does not know.
func (f *fakeNexus) ssrfBody() map[string]any {
	body := map[string]any{"enabled": f.ssrf.Enabled, "allowedIPs": f.ssrf.AllowedIPs, "allowedDomains": f.ssrf.AllowedDomains}
	for k, v := range f.ssrfExtra {
		body[k] = v
	}
	return body
}

func (f *fakeNexus) serveSSRF(w http.ResponseWriter, r *http.Request, body []byte) {
	if !f.ssrfPresent {
		f.reply(w, r, http.StatusNotFound, nil)
		return
	}
	switch r.Method {
	case http.MethodGet:
		f.reply(w, r, http.StatusOK, f.ssrfBody())
	case http.MethodPut:
		var s ssrfSettings
		var all map[string]json.RawMessage
		if err := json.Unmarshal(body, &s); err != nil || s.AllowedIPs == nil || s.AllowedDomains == nil ||
			json.Unmarshal(body, &all) != nil {
			f.badRequest(w, r, "enabled, allowedIPs and allowedDomains are required")
			return
		}
		f.ssrf = s
		f.ssrfExtra = map[string]json.RawMessage{}
		for k, v := range all {
			if k != "enabled" && k != "allowedIPs" && k != "allowedDomains" {
				f.ssrfExtra[k] = v
			}
		}
		f.reply(w, r, http.StatusOK, f.ssrfBody())
	default:
		f.reply(w, r, http.StatusMethodNotAllowed, nil)
	}
}

func (f *fakeNexus) serveRoles(w http.ResponseWriter, r *http.Request, body []byte) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, pathRoles), "/")
	switch {
	case id == "" && r.Method == http.MethodGet:
		list := make([]any, 0, len(f.roles))
		for _, role := range f.roles {
			list = append(list, role)
		}
		f.reply(w, r, http.StatusOK, list)
	case id == "" && r.Method == http.MethodPost:
		m, why := f.decodeRole(body)
		if why != "" {
			f.badRequest(w, r, why)
			return
		}
		rid, _ := m["id"].(string)
		if _, exists := f.roles[rid]; exists {
			f.badRequest(w, r, "the role already exists")
			return
		}
		f.roles[rid] = m
		f.reply(w, r, http.StatusOK, m)
	case id != "" && r.Method == http.MethodGet:
		role, ok := f.roles[id]
		if !ok {
			f.reply(w, r, http.StatusNotFound, nil)
			return
		}
		f.reply(w, r, http.StatusOK, role)
	case id != "" && r.Method == http.MethodPut:
		if _, ok := f.roles[id]; !ok {
			f.reply(w, r, http.StatusNotFound, nil)
			return
		}
		m, why := f.decodeRole(body)
		if why != "" {
			f.badRequest(w, r, why)
			return
		}
		if rid, _ := m["id"].(string); rid != id {
			f.badRequest(w, r, "the id of the body is not the id of the path")
			return
		}
		f.roles[id] = m
		f.reply(w, r, http.StatusNoContent, nil)
	default:
		f.reply(w, r, http.StatusMethodNotAllowed, nil)
	}
}

func (f *fakeNexus) decodeRole(body []byte) (map[string]any, string) {
	m, ok := decodeObject(body)
	if !ok {
		return nil, "not a JSON object"
	}
	if !nonEmptyString(m, "id") || !nonEmptyString(m, "name") {
		return nil, "id and name are required"
	}
	if _, isList := m["privileges"].([]any); !isList {
		return nil, "privileges must be an array"
	}
	return m, ""
}

func (f *fakeNexus) serveUsers(w http.ResponseWriter, r *http.Request, body []byte) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, pathUsers), "/")
	parts := []string{}
	if rest != "" {
		parts = strings.Split(rest, "/")
	}
	switch {
	case len(parts) == 0 && r.Method == http.MethodGet:
		f.listUsers(w, r)
	case len(parts) == 0 && r.Method == http.MethodPost:
		f.createUser(w, r, body)
	case len(parts) == 1 && r.Method == http.MethodPut:
		f.updateUser(w, r, parts[0], body)
	case len(parts) == 2 && parts[1] == "change-password" && r.Method == http.MethodPut:
		f.changePassword(w, r, parts[0], body)
	default:
		f.reply(w, r, http.StatusNotFound, nil)
	}
}

func (u *fakeUser) payload() map[string]any {
	return map[string]any{
		"userId": u.UserID, "firstName": u.FirstName, "lastName": u.LastName,
		"emailAddress": u.EmailAddress, "source": "default", "status": u.Status,
		"readOnly": false, "roles": u.Roles, "externalRoles": []any{},
	}
}

func (f *fakeNexus) listUsers(w http.ResponseWriter, r *http.Request) {
	filter := strings.ToLower(r.URL.Query().Get("userId"))
	ids := make([]string, 0, len(f.users))
	for id := range f.users {
		if filter == "" || strings.Contains(strings.ToLower(id), filter) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	list := make([]any, 0, len(ids))
	for _, id := range ids {
		list = append(list, f.users[id].payload())
	}
	f.reply(w, r, http.StatusOK, list)
}

// decodeUser reads a user payload; password is demanded when needPassword.
func (f *fakeNexus) decodeUser(body []byte, needPassword bool) (*fakeUser, string) {
	m, ok := decodeObject(body)
	if !ok {
		return nil, "not a JSON object"
	}
	for _, key := range []string{"userId", "firstName", "lastName", "emailAddress", "status"} {
		if !nonEmptyString(m, key) {
			return nil, key + " is required"
		}
	}
	roles, isList := m["roles"].([]any)
	if !isList {
		return nil, "roles must be an array"
	}
	u := &fakeUser{
		UserID: m["userId"].(string), FirstName: m["firstName"].(string), LastName: m["lastName"].(string),
		EmailAddress: m["emailAddress"].(string), Status: m["status"].(string), Roles: []string{},
	}
	for _, role := range roles {
		s, _ := role.(string)
		u.Roles = append(u.Roles, s)
	}
	if needPassword {
		if !nonEmptyString(m, "password") {
			return nil, "password is required"
		}
		u.Password = m["password"].(string)
	}
	return u, ""
}

func (f *fakeNexus) createUser(w http.ResponseWriter, r *http.Request, body []byte) {
	u, why := f.decodeUser(body, true)
	if why != "" {
		f.badRequest(w, r, why)
		return
	}
	if _, exists := f.users[u.UserID]; exists {
		f.badRequest(w, r, "the user already exists")
		return
	}
	f.users[u.UserID] = u
	f.reply(w, r, http.StatusOK, u.payload())
}

func (f *fakeNexus) updateUser(w http.ResponseWriter, r *http.Request, id string, body []byte) {
	cur, ok := f.users[id]
	if !ok {
		f.reply(w, r, http.StatusNotFound, nil)
		return
	}
	u, why := f.decodeUser(body, false)
	if why != "" {
		f.badRequest(w, r, why)
		return
	}
	if u.UserID != id {
		f.badRequest(w, r, "the userId of the body is not the id of the path")
		return
	}
	// A PUT never changes the password: change-password does.
	u.Password = cur.Password
	f.users[id] = u
	f.reply(w, r, http.StatusNoContent, nil)
}

func (f *fakeNexus) changePassword(w http.ResponseWriter, r *http.Request, id string, body []byte) {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "text/plain") {
		f.reply(w, r, http.StatusUnsupportedMediaType, nil)
		return
	}
	u, ok := f.users[id]
	if !ok {
		f.reply(w, r, http.StatusNotFound, nil)
		return
	}
	pw := strings.TrimSpace(string(body))
	if pw == "" {
		f.badRequest(w, r, "the new password is empty")
		return
	}
	u.Password = pw
	f.reply(w, r, http.StatusNoContent, nil)
}

func (f *fakeNexus) serveRepos(w http.ResponseWriter, r *http.Request, body []byte) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, pathRepositories+"/docker/"), "/")
	parts := strings.Split(rest, "/")
	kind := parts[0]
	byName, known := f.repos[kind]
	if !known || len(parts) > 2 {
		f.reply(w, r, http.StatusNotFound, nil)
		return
	}
	switch {
	case len(parts) == 1 && r.Method == http.MethodPost:
		cfg, why := f.decodeRepo(kind, body)
		if why != "" {
			f.badRequest(w, r, why)
			return
		}
		name := cfg["name"].(string)
		if f.repoExists(name) {
			f.badRequest(w, r, "a repository of that name already exists")
			return
		}
		if f.refuseUnresolvedUpstream(w, kind, cfg) {
			return
		}
		byName[name] = cfg
		f.reply(w, r, http.StatusCreated, nil)
	case len(parts) == 2 && r.Method == http.MethodGet:
		cfg, ok := byName[parts[1]]
		if !ok {
			f.reply(w, r, http.StatusNotFound, nil)
			return
		}
		f.reply(w, r, http.StatusOK, cfg)
	case len(parts) == 2 && r.Method == http.MethodPut:
		if _, ok := byName[parts[1]]; !ok {
			f.reply(w, r, http.StatusNotFound, nil)
			return
		}
		cfg, why := f.decodeRepo(kind, body)
		if why != "" {
			f.badRequest(w, r, why)
			return
		}
		if cfg["name"] != parts[1] {
			f.badRequest(w, r, "the name of the body is not the name of the path")
			return
		}
		if f.refuseUnresolvedUpstream(w, kind, cfg) {
			return
		}
		byName[parts[1]] = cfg
		if kind == "proxy" && parts[1] == dockerProxyRepository {
			f.proxyBlocked = false
		}
		f.reply(w, r, http.StatusNoContent, nil)
	default:
		f.reply(w, r, http.StatusMethodNotAllowed, nil)
	}
}

// serveRepoByName serves the calls that name a repository without its format and
// kind: DELETE {name} and POST {name}/invalidate-cache.
func (f *fakeNexus) serveRepoByName(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, pathRepositories+"/"), "/"), "/")
	name := parts[0]
	kind, found := f.repoKind(name)
	switch {
	case len(parts) == 1 && r.Method == http.MethodDelete:
		if !found {
			f.reply(w, r, http.StatusNotFound, nil)
			return
		}
		delete(f.repos[kind], name)
		delete(f.negativeCached, name)
		if name == dockerProxyRepository {
			f.proxyBlocked = false
		}
		if f.keepAfterDelete > 0 {
			f.staleReads[name] = f.keepAfterDelete
		} else {
			delete(f.components, name)
		}
		f.reply(w, r, http.StatusNoContent, nil)
	case len(parts) == 2 && parts[1] == "invalidate-cache" && r.Method == http.MethodPost:
		switch {
		case !found:
			f.reply(w, r, http.StatusNotFound, nil)
		case kind == "hosted":
			f.badRequest(w, r, "a hosted repository has no cache to invalidate")
		default:
			delete(f.negativeCached, name)
			f.reply(w, r, http.StatusNoContent, nil)
		}
	case len(parts) == 1 || (len(parts) == 2 && parts[1] == "invalidate-cache"):
		f.reply(w, r, http.StatusMethodNotAllowed, nil)
	default:
		f.reply(w, r, http.StatusNotFound, nil)
	}
}

// repoKind returns the kind ("hosted", "proxy" or "group") of the repository.
func (f *fakeNexus) repoKind(name string) (string, bool) {
	for kind, byName := range f.repos {
		if _, ok := byName[name]; ok {
			return kind, true
		}
	}
	return "", false
}

func (f *fakeNexus) repoExists(name string) bool {
	for _, byName := range f.repos {
		if _, ok := byName[name]; ok {
			return true
		}
	}
	return false
}

// decodeRepo checks the keys of the Repository configuration documented above.
func (f *fakeNexus) decodeRepo(kind string, body []byte) (map[string]any, string) {
	m, ok := decodeObject(body)
	if !ok {
		return nil, "not a JSON object"
	}
	need := func(path ...string) bool { return jsonAt(m, path...) != nil }
	if !nonEmptyString(m, "name") {
		return nil, "name is required"
	}
	if _, isBool := m["online"].(bool); !isBool {
		return nil, "online must be a boolean"
	}
	blob, _ := jsonAt(m, "storage", "blobStoreName").(string)
	if !slices.Contains(f.blobStores, blob) {
		return nil, "the blob store " + blob + " does not exist"
	}
	for _, path := range [][]string{
		{"storage", "strictContentTypeValidation"}, {"docker", "v1Enabled"}, {"docker", "forceBasicAuth"},
	} {
		if !need(path...) {
			return nil, strings.Join(path, ".") + " is required"
		}
	}
	switch kind {
	case "hosted":
		if !need("storage", "writePolicy") {
			return nil, "storage.writePolicy is required"
		}
	case "proxy":
		if s, _ := jsonAt(m, "proxy", "remoteUrl").(string); s == "" {
			return nil, "proxy.remoteUrl is required"
		}
		for _, path := range [][]string{{"proxy", "contentMaxAge"}, {"proxy", "metadataMaxAge"}, {"dockerProxy", "indexType"}} {
			if !need(path...) {
				return nil, strings.Join(path, ".") + " is required"
			}
		}
	case "group":
		if !need("docker", "httpPort") {
			return nil, "docker.httpPort is required"
		}
		members, isList := jsonAt(m, "group", "memberNames").([]any)
		if !isList {
			return nil, "group.memberNames must be an array"
		}
		for _, member := range members {
			name, _ := member.(string)
			if _, hosted := f.repos["hosted"][name]; !hosted {
				if _, proxy := f.repos["proxy"][name]; !proxy {
					return nil, "the group member " + name + " does not exist"
				}
			}
		}
	}
	return m, ""
}

// ---- JSON helpers, shared with the tests -----------------------------------

func decodeObject(body []byte) (map[string]any, bool) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil || m == nil {
		return nil, false
	}
	return m, true
}

func nonEmptyString(m map[string]any, key string) bool {
	s, ok := m[key].(string)
	return ok && s != ""
}

func deepCopy(m map[string]any) map[string]any {
	data, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		panic(err)
	}
	return out
}

// jsonAt follows keys through nested objects; nil when any step is absent.
func jsonAt(m map[string]any, keys ...string) any {
	var cur any = m
	for _, k := range keys {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = obj[k]
	}
	return cur
}

// stringsAt reads the array of strings at keys; nil when it is not one.
func stringsAt(m map[string]any, keys ...string) []string {
	list, ok := jsonAt(m, keys...).([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, v := range list {
		s, _ := v.(string)
		out = append(out, s)
	}
	return out
}
