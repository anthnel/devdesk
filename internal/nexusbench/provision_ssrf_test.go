package nexusbench

import (
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Nexus 3.98 has an SSRF protection on a new installation: a proxy whose remote
// URL host resolves to a private address (a ClusterIP is one) is refused with a
// 400 on proxy.remoteUrl unless the host is in allowedDomains or allowedIPs of
// /service/rest/v1/security/ssrf-protection. Observed on the bench on 2026-11-17
// (DEVDESK-505, H-SSRF). Provision allows the host of the bench upstream, and
// nothing else, before it creates the proxy.

// benchUpstreamBench is a bench whose proxy mirrors the upstream the bench
// deploys, the only upstream Provision allows through the SSRF protection.
func benchUpstreamBench(t *testing.T) *bench {
	t.Helper()
	b := newBench(t)
	b.p.UpstreamURL = upstreamFor(b.p.Namespace)
	return b
}

// benchUpstreamHost is the host Provision has to allow on a bench like that.
func benchUpstreamHost(b *bench) string {
	return "bench-upstream." + b.p.Namespace + ".svc.cluster.local"
}

func TestProvisionAllowsTheBenchUpstreamThroughSSRFProtection(t *testing.T) {
	b := benchUpstreamBench(t)
	res := b.mustRun()
	if res.ProxyPending {
		t.Error("ProxyPending = true, the proxy has to be created")
	}
	proxy := requireRepo(t, b.nexus, "proxy", dockerProxyRepository)
	if got := jsonAt(proxy, "proxy", "remoteUrl"); got != b.p.UpstreamURL {
		t.Errorf("proxy.remoteUrl = %v, want %s", got, b.p.UpstreamURL)
	}
	want := ssrfSettings{Enabled: true, AllowedIPs: []string{}, AllowedDomains: []string{benchUpstreamHost(b)}}
	if got := b.nexus.SSRF(); !reflect.DeepEqual(got, want) {
		t.Errorf("SSRF settings = %+v, want %+v", got, want)
	}
	group := requireRepo(t, b.nexus, "group", dockerGroupRepository)
	members := []string{dockerHostedRepository, dockerProxyRepository}
	if got := stringsAt(group, "group", "memberNames"); !reflect.DeepEqual(got, members) {
		t.Errorf("group members = %v, want %v", got, members)
	}
	if !strings.Contains(b.out.String(), "provision: ") || !strings.Contains(b.out.String(), benchUpstreamHost(b)) {
		t.Errorf("the output does not say which host was allowed:\n%s", b.out.String())
	}
}

func TestProvisionAllowsTheUpstreamBeforeItCreatesTheProxy(t *testing.T) {
	b := benchUpstreamBench(t)
	b.mustRun()
	put := b.log.index("HTTP PUT " + pathSSRF)
	post := b.log.index("HTTP POST " + repoPath("proxy", ""))
	if put < 0 || post < 0 || put > post {
		t.Errorf("PUT ssrf-protection at %d, POST proxy at %d: the allowance has to come first", put, post)
	}
	if n := b.nexus.Count(http.MethodPost, repoPath("proxy", "")); n != 1 {
		t.Errorf("%d POST of the proxy, want 1 (no refused attempt)", n)
	}
}

func TestProvisionSSRFAllowanceIsIdempotent(t *testing.T) {
	b := benchUpstreamBench(t)
	b.mustRun()
	before := b.nexus.Snapshot()
	b.ip.ok = false
	seen := len(b.nexus.Requests())

	b.mustRun()
	if got := b.nexus.Snapshot(); got != before {
		t.Errorf("a second run changed the instance:\nbefore %s\nafter  %s", before, got)
	}
	if n := b.nexus.Count(http.MethodPut, pathSSRF); n != 1 {
		t.Errorf("%d PUT of the SSRF settings over two runs, want 1", n)
	}
	if n := b.nexus.Count(http.MethodGet, pathSSRF); n != 2 {
		t.Errorf("%d GET of the SSRF settings over two runs, want 2 (one each)", n)
	}
	for _, w := range writes(b.requestsFrom(seen)) {
		if strings.Contains(w, "ssrf") || strings.Contains(w, "POST") {
			t.Errorf("second run wrote %q", w)
		}
	}
}

func TestProvisionSSRFAllowanceKeepsWhatIsThere(t *testing.T) {
	b := benchUpstreamBench(t)
	b.nexus.SetSSRF(ssrfSettings{Enabled: true, AllowedIPs: []string{"10.0.0.5"}, AllowedDomains: []string{"registry.corp.example"}})
	b.mustRun()
	want := ssrfSettings{
		Enabled: true, AllowedIPs: []string{"10.0.0.5"},
		AllowedDomains: []string{"registry.corp.example", benchUpstreamHost(b)},
	}
	if got := b.nexus.SSRF(); !reflect.DeepEqual(got, want) {
		t.Errorf("SSRF settings = %+v, want %+v", got, want)
	}
}

func TestProvisionSSRFAllowanceMatchesTheHostWhateverItsCase(t *testing.T) {
	b := benchUpstreamBench(t)
	b.nexus.SetSSRF(ssrfSettings{Enabled: true, AllowedIPs: []string{}, AllowedDomains: []string{strings.ToUpper(benchUpstreamHost(b))}})
	b.mustRun()
	if n := b.nexus.Count(http.MethodPut, pathSSRF); n != 0 {
		t.Errorf("%d PUT of the SSRF settings, the host was already allowed", n)
	}
}

func TestProvisionLeavesAnSSRFProtectionThatIsOffAlone(t *testing.T) {
	b := benchUpstreamBench(t)
	b.nexus.SetSSRF(ssrfSettings{Enabled: false, AllowedIPs: []string{}, AllowedDomains: []string{}})
	b.mustRun()
	if n := b.nexus.Count(http.MethodPut, pathSSRF); n != 0 {
		t.Errorf("%d PUT of the SSRF settings: the bench does not switch the protection on", n)
	}
	if got := b.nexus.SSRF(); got.Enabled || len(got.AllowedDomains) != 0 {
		t.Errorf("SSRF settings = %+v, want them untouched", got)
	}
	requireRepo(t, b.nexus, "proxy", dockerProxyRepository)
}

func TestProvisionNeverSwitchesTheSSRFProtectionOff(t *testing.T) {
	b := benchUpstreamBench(t)
	b.mustRun()
	for _, r := range b.nexus.Requests() {
		if r.Path == pathSSRF && r.Method == http.MethodPut && !strings.Contains(r.Body, `"enabled":true`) {
			t.Errorf("PUT of the SSRF settings without enabled=true: %s", r.Body)
		}
	}
}

func TestProvisionWithoutAnSSRFEndpointGoesOn(t *testing.T) {
	b := benchUpstreamBench(t)
	b.nexus.RemoveSSRFEndpoint()
	b.mustRun()
	if n := b.nexus.Count(http.MethodPut, pathSSRF); n != 0 {
		t.Errorf("%d PUT to an endpoint that answered 404", n)
	}
	requireRepo(t, b.nexus, "proxy", dockerProxyRepository)
}

func TestProvisionDoesNotAllowACustomIPUpstream(t *testing.T) {
	b := benchUpstreamBench(t)
	b.p.UpstreamURL = "http://10.110.133.156:5000"
	// Not the bench's own upstream URL: the allowance is for the bench upstream only.
	if b.p.UpstreamIsBench() {
		t.Fatal("the case is the bench upstream")
	}
	err := b.failure()
	requireMentions(t, err, []string{"ssrf", "ssrf-protection", "NEXUS_BENCH_UPSTREAM_URL", "unset"}, nil)
	if n := b.nexus.Count(http.MethodPut, pathSSRF); n != 0 {
		t.Errorf("%d PUT of the SSRF settings for a custom IP upstream", n)
	}
	if got := b.nexus.SSRF(); len(got.AllowedIPs) != 0 || len(got.AllowedDomains) != 0 {
		t.Errorf("the bench widened the SSRF protection for an upstream that is not its own: %+v", got)
	}
}

func TestProvisionDoesNotAllowACustomUpstream(t *testing.T) {
	b := newBench(t)
	b.p.UpstreamURL = upstreamFor("another-bench")
	err := b.failure()
	var be *BenchError
	if !errors.As(err, &be) || be.Kind != ProvisionFailed {
		t.Fatalf("error = %#v, want a ProvisionFailed BenchError", err)
	}
	requireMentions(t, err, []string{"ssrf", "ssrf-protection", "NEXUS_BENCH_UPSTREAM_URL", "unset"}, nil)
	if got := b.nexus.SSRF(); len(got.AllowedDomains) != 0 || len(got.AllowedIPs) != 0 {
		t.Errorf("the bench widened the SSRF protection for an upstream that is not its own: %+v", got)
	}
	if n := b.nexus.Count(http.MethodPut, pathSSRF); n != 0 {
		t.Errorf("%d PUT of the SSRF settings for a custom upstream", n)
	}
}

func TestProvisionSSRFRefusalIsNotTakenForAnUnresolvedHost(t *testing.T) {
	b := newBench(t)
	b.p.UpstreamURL = upstreamFor("another-bench")
	err := b.failure()
	var api *APIError
	if !errors.As(err, &api) {
		t.Fatalf("error = %v, want an *APIError in the chain", err)
	}
	if api.RemoteURLBlocked {
		t.Error("a private-address refusal was taken for an unresolved host: the proxy would be left pending without a word on SSRF")
	}
	if !api.RemoteURLPrivate {
		t.Error("RemoteURLPrivate = false for the private-address refusal")
	}
}

func TestProvisionSSRFSettingsRefusedFailsWithTheAccountHint(t *testing.T) {
	b := benchUpstreamBench(t)
	b.nexus.Forbid(http.MethodPut, pathSSRF, http.StatusForbidden)
	err := b.failure()
	var be *BenchError
	if !errors.As(err, &be) || be.Kind != ProvisionFailed {
		t.Fatalf("error = %#v, want a ProvisionFailed BenchError", err)
	}
	// The hint of every 401 and 403: the stored admin password may not match.
	requireMentions(t, err, []string{"bench account", "admin password", "nexus-bench:down"}, nil)
	var api *APIError
	if !errors.As(err, &api) || api.Status != http.StatusForbidden {
		t.Fatalf("error = %v, want the 403 in the chain", err)
	}
	if _, ok := b.nexus.Repo("proxy", dockerProxyRepository); ok {
		t.Error("the proxy was created although the allowance failed")
	}
}

func TestRemoteURLPrivateOnlyForAPrivateAddressRefusal(t *testing.T) {
	real := `[{"id":"HelperBean.proxy.remoteUrl","message":"Proxy URL blocked: Host resolves to private/local IP address(es): 10.110.133.156 (private network address). To allow connections, update the SSRF protection settings via the API at /v1/security/ssrf-protection."}]`
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"the real answer", real, true},
		{"the message in another case", strings.Replace(real, "Host resolves to private", "HOST RESOLVES TO PRIVATE", 1), true},
		{"among other errors", `[{"id":"HelperBean.name","message":"Invalid"},` + real[1:], true},
		{"an unresolved host", `[{"id":"HelperBean.proxy.remoteUrl","message":"Proxy URL blocked: Failed to resolve host: x"}]`, false},
		{"the message on another id", `[{"id":"HelperBean.name","message":"Host resolves to private/local IP address(es)"}]`, false},
		{"another message on the field", `[{"id":"HelperBean.proxy.remoteUrl","message":"Malformed URL"}]`, false},
		{"not JSON", `Host resolves to private/local IP address(es)`, false},
		{"an empty list", `[]`, false},
		{"over 4 KiB", `[{"id":"HelperBean.proxy.remoteUrl","message":"Host resolves to private/local IP address(es) ` + strings.Repeat("x", drainBytes) + `"}]`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := remoteURLPrivate([]byte(tc.body)); got != tc.want {
				t.Errorf("remoteURLPrivate(%.80q) = %v, want %v", tc.body, got, tc.want)
			}
			if tc.want && remoteURLRefused([]byte(tc.body)) {
				t.Error("a private-address refusal is also read as an unresolved host")
			}
		})
	}
}

func TestSSRFSettingsAreSentWithNonNullLists(t *testing.T) {
	b := benchUpstreamBench(t)
	// An instance may answer null for a list it never had.
	b.nexus.SetSSRF(ssrfSettings{Enabled: true})
	b.mustRun()
	var body string
	for _, r := range b.nexus.Requests() {
		if r.Method == http.MethodPut && r.Path == pathSSRF {
			body = r.Body
		}
	}
	if body == "" {
		t.Fatal("no PUT of the SSRF settings")
	}
	if strings.Contains(body, "null") {
		t.Errorf("the SSRF settings carry a null list: %s", body)
	}
	if got := b.nexus.SSRF(); !slices.Equal(got.AllowedDomains, []string{benchUpstreamHost(b)}) {
		t.Errorf("allowed domains = %v", got.AllowedDomains)
	}
}

// H-PATH, observed on 3.98.1-01 (DEVDESK-505): a docker client reaches a
// repository by path on the HTTP port (`127.0.0.1:<port>/<repository>/<name>`)
// only when docker.pathEnabled is true on it. The default is null, and /v2/ then
// answers 404 on that port.
func TestProvisionEnablesPathRoutingOnTheHostedAndTheProxy(t *testing.T) {
	b := benchUpstreamBench(t)
	b.mustRun()
	for _, repo := range []struct{ kind, name string }{
		{"hosted", dockerHostedRepository}, {"proxy", dockerProxyRepository},
	} {
		cfg := requireRepo(t, b.nexus, repo.kind, repo.name)
		if got := jsonAt(cfg, "docker", "pathEnabled"); got != true {
			t.Errorf("%s docker.pathEnabled = %v, want true", repo.name, got)
		}
	}
}

func TestProvisionPutsPathRoutingBackOnAHostedThatLostIt(t *testing.T) {
	b := benchUpstreamBench(t)
	b.mustRun()
	b.nexus.MutateRepo("hosted", dockerHostedRepository, func(cfg map[string]any) {
		cfg["docker"].(map[string]any)["pathEnabled"] = nil
	})
	b.ip.ok = false
	b.mustRun()
	cfg := requireRepo(t, b.nexus, "hosted", dockerHostedRepository)
	if got := jsonAt(cfg, "docker", "pathEnabled"); got != true {
		t.Errorf("docker.pathEnabled = %v after a second run, want true", got)
	}
}

// privateRefusalBody is what Nexus 3.98.1-01 answered for a ClusterIP upstream.
const privateRefusalBody = `[{"id":"HelperBean.proxy.remoteUrl","message":"Proxy URL blocked: Host resolves to private/local IP address(es): 10.110.133.156 (private network address). To allow connections, update the SSRF protection settings via the API at /v1/security/ssrf-protection."}]`

// A bench upstream that Nexus refuses all the same (the allowance did not take,
// or the instance has no such setting) is not an upstream the operator chose:
// the message must not tell them to unset a variable they never set.
func TestProvisionRefusedBenchUpstreamPointsAtTheSSRFSettingNotAtTheVariable(t *testing.T) {
	b := benchUpstreamBench(t)
	b.nexus.ForbidWithBody(http.MethodPost, repoPath("proxy", ""), http.StatusBadRequest, privateRefusalBody)
	err := b.failure()
	var be *BenchError
	if !errors.As(err, &be) || be.Kind != ProvisionFailed {
		t.Fatalf("error = %#v, want a ProvisionFailed BenchError", err)
	}
	requireMentions(t, err, []string{"ssrf", pathSSRF, "allowedDomains", benchUpstreamHost(b)}, []string{"unset", "NEXUS_BENCH_UPSTREAM_URL"})
	var api *APIError
	if !errors.As(err, &api) || !api.RemoteURLPrivate {
		t.Errorf("the *APIError with RemoteURLPrivate is not in the chain: %v", err)
	}
}

func TestProvisionRefusedCustomUpstreamKeepsTheAdviceToUnsetTheVariable(t *testing.T) {
	b := newBench(t)
	b.p.UpstreamURL = upstreamFor("another-bench")
	b.nexus.ForbidWithBody(http.MethodPost, repoPath("proxy", ""), http.StatusBadRequest, privateRefusalBody)
	requireMentions(t, b.failure(), []string{"ssrf", "NEXUS_BENCH_UPSTREAM_URL", "unset", pathSSRF}, nil)
}

// Defense in depth: even for the bench upstream, Provision only ever allows
// exactly bench-upstream.<namespace>.svc.cluster.local, for a namespace the bench
// accepts. A namespace of the cluster is refused before the instance is asked.
func TestProvisionDoesNotAllowAnUpstreamInAReservedNamespace(t *testing.T) {
	b := newBench(t)
	b.p.Namespace = "kube-system"
	b.p.UpstreamURL = upstreamFor(b.p.Namespace)
	if !b.p.UpstreamIsBench() {
		t.Fatal("the case is not the bench upstream")
	}
	err := b.failure()
	var be *BenchError
	if !errors.As(err, &be) || be.Kind != ProvisionFailed {
		t.Fatalf("error = %#v, want a ProvisionFailed BenchError", err)
	}
	requireMentions(t, err, []string{"kube-system"}, nil)
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		if n := b.nexus.Count(method, pathSSRF); n != 0 {
			t.Errorf("%d %s of the SSRF settings for a reserved namespace", n, method)
		}
	}
	if _, ok := b.nexus.Repo("proxy", dockerProxyRepository); ok {
		t.Error("the proxy was created")
	}
}

func TestAllowableUpstreamHost(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(p *Params)
		want    string
		wantErr bool
	}{
		{"the bench upstream", func(*Params) {}, "bench-upstream.nexus-bench.svc.cluster.local", false},
		{"another valid namespace", func(p *Params) { p.Namespace = "nexus-bench-2"; p.UpstreamURL = upstreamFor("nexus-bench-2") }, "bench-upstream.nexus-bench-2.svc.cluster.local", false},
		{"a reserved namespace", func(p *Params) { p.Namespace = "default"; p.UpstreamURL = upstreamFor("default") }, "", true},
		{"a namespace that is not a label", func(p *Params) { p.Namespace = "a.b"; p.UpstreamURL = upstreamFor("a.b") }, "", true},
		{"a host that is not the Service of the namespace", func(p *Params) { p.UpstreamURL = "http://registry.example.com:5000" }, "", true},
		{"the Service of another namespace", func(p *Params) { p.UpstreamURL = upstreamFor("other") }, "", true},
		{"credentials in the URL", func(p *Params) {
			p.UpstreamURL = "http://user:pw@bench-upstream.nexus-bench.svc.cluster.local:5000"
		}, "", true},
		{"an unparsable URL", func(p *Params) { p.UpstreamURL = "http://[::1" }, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := Defaults()
			tc.mutate(&p)
			got, err := allowableUpstreamHost(p)
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Errorf("allowableUpstreamHost = %q, %v; want %q, error %v", got, err, tc.want, tc.wantErr)
			}
			if err != nil && strings.Contains(err.Error(), "pw") {
				t.Errorf("the error repeats the credentials of the URL: %v", err)
			}
		})
	}
}

// The instance of another version may have more fields than the three the bench
// knows; a PUT built from a struct would drop them. Only allowedDomains changes.
func TestProvisionSSRFAllowanceKeepsFieldsItDoesNotKnow(t *testing.T) {
	b := benchUpstreamBench(t)
	b.nexus.SetSSRF(ssrfSettings{Enabled: true, AllowedIPs: []string{"10.0.0.5"}, AllowedDomains: []string{"registry.corp.example"}})
	b.nexus.SetSSRFField("blockedDomains", []string{"evil.example"})
	b.nexus.SetSSRFField("futureOption", map[string]any{"mode": "strict", "limit": 3})
	b.mustRun()
	if got, ok := b.nexus.SSRFField("blockedDomains"); !ok || !reflect.DeepEqual(got, []any{"evil.example"}) {
		t.Errorf("blockedDomains = %v (present %v), want it kept", got, ok)
	}
	if got, ok := b.nexus.SSRFField("futureOption"); !ok || !reflect.DeepEqual(got, map[string]any{"mode": "strict", "limit": float64(3)}) {
		t.Errorf("futureOption = %v (present %v), want it kept as it was", got, ok)
	}
	want := ssrfSettings{Enabled: true, AllowedIPs: []string{"10.0.0.5"}, AllowedDomains: []string{"registry.corp.example", benchUpstreamHost(b)}}
	if got := b.nexus.SSRF(); !reflect.DeepEqual(got, want) {
		t.Errorf("SSRF settings = %+v, want %+v", got, want)
	}
}
