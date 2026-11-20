package nexusfixture_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/nexusfixture"
)

// denylistEnv names a file, kept OUTSIDE the repository, with one term per
// line (names of an instance that must never appear in a fixture).
const denylistEnv = "DEVDESK_FIXTURE_DENYLIST"

const leakBaseName = "rest-repositories-docker-proxy--admin"

// The values below look like credentials to the secret-token rules under test.
// They are assembled at run time so that no literal of this file does: a
// pre-commit or hosting scanner would otherwise take them for real ones.
var (
	fakeGitHubToken = "ghp" + "_" + "Zz0123456789abcdefghijklmnopqrstuvwx"
	fakeGitLabToken = "glpat" + "-" + "zzAbCdEf0123456789xy"
	fakeSecretKey   = "sec" + "ret"
	fakeSecretVal   = "zz-planted-" + fakeSecretKey + "-6"
)

// pemBegin is the first line of a PEM private key block, e.g. pemBegin("RSA")
// or pemBegin("") for PKCS8, built from pieces for the reason given above.
func pemBegin(kind string) string {
	if kind != "" {
		kind += " "
	}
	return "-----BEGIN " + kind + "PRIVATE" + " KEY-----"
}

// leakBase is the real NX-03 answer the self-tests plant their leaks into: it has
// an Authorization line, a Host, a Server header, a JSON body with a URL and the
// bench's upstream service. The planted text is invented.
func leakBase(t *testing.T) string { t.Helper(); return leakFixture(t, leakBaseName) }

// leakFixture formats one real catalog fixture by name.
func leakFixture(t *testing.T, name string) string {
	t.Helper()
	fixtures, err := nexusfixture.Load("catalog")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		if f.Name == name {
			return string(nexusfixture.Format(f))
		}
	}
	t.Fatalf("the catalog campaign has no %s", name)
	return ""
}

// plant replaces the one occurrence of old in base, failing when the base has
// moved and old is no longer there exactly once.
func plant(t *testing.T, base, old, repl string) string {
	t.Helper()
	if n := strings.Count(base, old); n != 1 {
		t.Fatalf("the base fixture holds %q %d times, want exactly once", old, n)
	}
	return strings.Replace(base, old, repl, 1)
}

func lineOf(data, needle string) int {
	i := strings.Index(data, needle)
	if i < 0 {
		return 0
	}
	return strings.Count(data[:i], "\n") + 1
}

const (
	hdrAuth   = "Authorization: Basic <redacted>\n"
	hdrHost   = "Host: 127.0.0.1:30481\n"
	hdrServer = "Server: Nexus/3.98.1-01 (COMMUNITY)\n"
	bodyOnl   = "  \"online\" : true,\n"
	bodyURL   = "\"url\" : \"http://127.0.0.1:30481/repository/docker-proxy\""
	bodyUp    = "\"remoteUrl\" : \"http://bench-upstream.nexus-bench.svc.cluster.local:5000\""

	// baseRequestLine is the request line of leakBase.
	baseRequestLine = "GET /service/rest/v1/repositories/docker-proxy HTTP/1.1\n"
	metaAcct        = "# account: admin (nx-admin, Basic, sent preemptively)"
)

// bodyNote adds a JSON member after "online".
func bodyNote(member string) (old, repl string) { return bodyOnl, bodyOnl + "  " + member + ",\n" }

// The rule names CheckLeaks reports. The first nine are the ones of the first
// version of the check; the others are the corrections of DEVDESK-569. A case
// asserts the rule it expects, so a leak reported under the wrong name fails.
const (
	rAuthorization  = "authorization-not-redacted"
	rSession        = "cookie-or-session"
	rRespHeader     = "response-header-outside-allowlist"
	rJWT            = "jwt-like-value"
	rJSONSecret     = "json-secret-not-redacted"
	rHost           = "host-not-allowed"
	rPrivateAddress = "private-or-link-local-address"
	rInternalName   = "internal-name-or-path"
	rExactTerm      = "exact-term"

	rReqHeader    = "request-header-outside-allowlist"
	rMalformed    = "malformed-header-line"
	rSecretFormat = "secret-token-format"
	rQuery        = "request-query-outside-allowlist"
)

type leakCase struct {
	name    string
	old     string
	repl    string
	planted string   // the invented value: it must be found, and never echoed back
	rule    string   // the rule that must be reported
	at      string   // text on the line the leak is reported at; planted when empty
	exact   []string // extra terms handed to CheckLeaks
}

func leakCases() []leakCase {
	note := func(name, member, planted, rule string) leakCase {
		old, repl := bodyNote(member)
		return leakCase{name: name, old: old, repl: repl, planted: planted, rule: rule}
	}
	// bodyKey plants a JSON member with a key that names a secret.
	bodyKey := note
	// reqHdr and respHdr add one header line after Authorization / Server.
	reqHdr := func(name, line, planted, rule string) leakCase {
		return leakCase{name: name, old: hdrAuth, repl: hdrAuth + line + "\n", planted: planted, rule: rule}
	}
	respHdr := func(name, line, planted, rule string) leakCase {
		return leakCase{name: name, old: hdrServer, repl: hdrServer + line + "\n", planted: planted, rule: rule}
	}
	reqLine := func(name, target, planted, rule string) leakCase {
		return leakCase{name: name, old: baseRequestLine, repl: "GET " + target + " HTTP/1.1\n", planted: planted, rule: rule}
	}
	exactOld, exactRepl := bodyNote(`"note" : "zz-exact-secret-91"`)
	b64Old, b64Repl := bodyNote(`"note" : "dXNlcjpwYXNzMTIz"`)
	exact := func(name, member, planted string, terms ...string) leakCase {
		old, repl := bodyNote(member)
		return leakCase{name: name, old: old, repl: repl, planted: planted, rule: rExactTerm, exact: terms}
	}
	return []leakCase{
		// Rule: Authorization other than Basic/Bearer <redacted>.
		{name: "authorization/basic value", old: hdrAuth, repl: "Authorization: Basic dXNlcjpwYXNzMTIz\n", planted: "dXNlcjpwYXNzMTIz", rule: rAuthorization},
		{name: "authorization/lowercase name", old: hdrAuth, repl: "authorization: Basic dXNlcjpwYXNzMTIz\n", planted: "dXNlcjpwYXNzMTIz", rule: rAuthorization},
		{name: "authorization/bearer value", old: hdrAuth, repl: "Authorization: Bearer opaque-token-4242\n", planted: "opaque-token-4242", rule: rAuthorization},
		{name: "authorization/unknown scheme", old: hdrAuth, repl: "Authorization: Digest response=\"d1g3st-77\"\n", planted: "d1g3st-77", rule: rAuthorization},

		// Rule: Cookie, Set-Cookie, NXSESSIONID, X-Nexus-UI.
		{name: "cookie/request Cookie", old: hdrHost, repl: hdrHost + "Cookie: theme=dark; sid=c00kie-val-1\n", planted: "c00kie-val-1", rule: rSession},
		{name: "cookie/response Set-Cookie", old: hdrServer, repl: hdrServer + "Set-Cookie: sid=sessval-2; Path=/\n", planted: "sessval-2", rule: rSession},
		{name: "cookie/NXSESSIONID anywhere", old: hdrHost, repl: hdrHost + "X-Trace: NXSESSIONID=zz-nxsess-3\n", planted: "zz-nxsess-3", rule: rSession},
		{name: "cookie/X-Nexus-UI", old: hdrHost, repl: hdrHost + "X-Nexus-UI: ui-session-9\n", planted: "ui-session-9", rule: rSession},

		// Rule: a response header outside the allowlist.
		respHdr("allowlist/unknown response header", "X-Powered-By: some-proxy-77", "some-proxy-77", rRespHeader),
		respHdr("allowlist/Date", "Date: Tue, 17 Nov 2026 10:00:00 GMT", "Tue, 17 Nov 2026 10:00:00 GMT", rRespHeader),

		// Rule (DEVDESK-569 #5): a request header outside Host, Authorization, Accept.
		reqHdr("request header/Proxy-Authorization", "Proxy-Authorization: Basic zz-proxyauth-1", "zz-proxyauth-1", rReqHeader),
		reqHdr("request header/X-Api-Key", "X-Api-Key: zz-apikey-2", "zz-apikey-2", rReqHeader),
		reqHdr("request header/NX-ANTI-CSRF-TOKEN", "NX-ANTI-CSRF-TOKEN: zz-csrf-3", "zz-csrf-3", rReqHeader),
		reqHdr("request header/X-Forwarded-For", "X-Forwarded-For: 8.8.8.8", "8.8.8.8", rReqHeader),
		reqHdr("request header/User-Agent", "User-Agent: zz-agent-4", "zz-agent-4", rReqHeader),
		reqHdr("request header/lowercase name", "x-api-key: zz-apikey-5", "zz-apikey-5", rReqHeader),

		// Rule (DEVDESK-569 #5): a line of a header section that is not "Name: value".
		{name: "malformed/response line without a colon", old: hdrServer, repl: hdrServer + "zz-no-colon-resp\n", planted: "zz-no-colon-resp", rule: rMalformed},
		{name: "malformed/request line without a colon", old: hdrHost, repl: hdrHost + "zz-no-colon-req\n", planted: "zz-no-colon-req", rule: rMalformed},

		// Rule: a JWT-like value.
		note("jwt/in a value", `"note" : "eyJhbGciOiJIUzI1NiJ9.zz-payload-5"`, "eyJhbGciOiJIUzI1NiJ9.zz-payload-5", rJWT),
		respHdr("jwt/in a header", `Link: <eyJhbGciOiJIUzI1NiJ9zz6>; rel="next"`, "eyJhbGciOiJIUzI1NiJ9zz6", rJWT),

		// Rule: a JSON secret key whose value is not <redacted>.
		bodyKey("json/token", `"token" : "zz-planted-token-1"`, "zz-planted-token-1", rJSONSecret),
		bodyKey("json/token compact", `"token":"zz-planted-token-2"`, "zz-planted-token-2", rJSONSecret),
		bodyKey("json/access_token", `"access_token" : "zz-planted-access-3"`, "zz-planted-access-3", rJSONSecret),
		bodyKey("json/refresh_token", `"refresh_token" : "zz-planted-refresh-4"`, "zz-planted-refresh-4", rJSONSecret),
		bodyKey("json/password", `"password" : "zz-planted-password-5"`, "zz-planted-password-5", rJSONSecret),
		bodyKey("json/secret", `"`+fakeSecretKey+`" : "`+fakeSecretVal+`"`, fakeSecretVal, rJSONSecret),
		bodyKey("json/uploaderIp", `"uploaderIp" : "zz-planted-uploader-7"`, "zz-planted-uploader-7", rJSONSecret),

		// DEVDESK-569 #1: the value is an object or an array, not a string.
		bodyKey("json value/object", `"token" : {"v":"zz-obj-1"}`, "zz-obj-1", rJSONSecret),
		bodyKey("json value/array", `"password":["zz-arr-1"]`, "zz-arr-1", rJSONSecret),
		bodyKey("json value/nested", `"secret" : {"a":{"b":["zz-deep-2"]}}`, "zz-deep-2", rJSONSecret),
		bodyKey("json value/redacted inside an object is not a redacted value", `"token" : {"v":"<redacted>", "w":"zz-obj-3"}`, "zz-obj-3", rJSONSecret),

		// DEVDESK-569 #2: the key is matched whatever its case, and by its ending.
		bodyKey("json key/Password", `"Password" : "zz-key-1"`, "zz-key-1", rJSONSecret),
		bodyKey("json key/PASSWORD", `"PASSWORD" : "zz-key-2"`, "zz-key-2", rJSONSecret),
		bodyKey("json key/accessToken", `"accessToken" : "zz-key-3"`, "zz-key-3", rJSONSecret),
		bodyKey("json key/bearerToken", `"bearerToken" : "zz-key-4"`, "zz-key-4", rJSONSecret),
		bodyKey("json key/proxyPassword", `"proxyPassword" : "zz-key-5"`, "zz-key-5", rJSONSecret),
		bodyKey("json key/apiKey", `"apiKey" : "zz-key-6"`, "zz-key-6", rJSONSecret),
		bodyKey("json key/client_secret", `"client_secret" : "zz-key-7"`, "zz-key-7", rJSONSecret),
		bodyKey("json key/privateKey", `"privateKey" : "zz-key-8"`, "zz-key-8", rJSONSecret),
		bodyKey("json key/credentials", `"credentials" : "zz-key-9"`, "zz-key-9", rJSONSecret),
		bodyKey("json key/credentials object", `"credentials" : {"user":"zz-key-10"}`, "zz-key-10", rJSONSecret),
		bodyKey("json key/escaped letter", `"pass\u0077ord" : "zz-key-11"`, "zz-key-11", rJSONSecret),
		bodyKey("json key/escaped letter in a suffix key", `"proxyPass\u0077ord" : "zz-key-12"`, "zz-key-12", rJSONSecret),

		// Rule: a host in a URL or Host: outside AllowedHosts.
		{name: "host/URL in a body", old: bodyURL, repl: "\"url\" : \"http://nexus.zz-corp.example:8081/repository/docker-proxy\"", planted: "nexus.zz-corp.example", rule: rHost},
		{name: "host/Host header", old: hdrHost, repl: "Host: nexus.zz-corp.example:8081\n", planted: "nexus.zz-corp.example", rule: rHost},
		{name: "host/public IP", old: bodyURL, repl: "\"url\" : \"http://203.0.113.7:30481/repository/docker-proxy\"", planted: "203.0.113.7", rule: rHost},
		{name: "host/a namesake of the upstream is not the upstream", old: bodyUp, repl: "\"remoteUrl\" : \"http://bench-upstream.zz-other.svc.cluster.local:5000\"", planted: "zz-other", rule: rHost},
		{name: "host/https scheme", old: bodyURL, repl: "\"url\" : \"https://nexus.zz-tls.example/repository/docker-proxy\"", planted: "nexus.zz-tls.example", rule: rHost},

		// Rule: an RFC 1918, link-local, CGNAT or unique-local address.
		note("private/10.0.0.0/8", `"note" : "see 10.20.30.40"`, "10.20.30.40", rPrivateAddress),
		note("private/172.16.0.0/12", `"note" : "see 172.20.4.2"`, "172.20.4.2", rPrivateAddress),
		note("private/192.168.0.0/16", `"note" : "see 192.168.77.31"`, "192.168.77.31", rPrivateAddress),
		note("private/link-local", `"note" : "see 169.254.169.254"`, "169.254.169.254", rPrivateAddress),
		note("private/glued to a letter", `"note" : "ip10.1.2.3"`, "ip10.1.2.3", rPrivateAddress),
		note("private/glued to a letter, 192.168", `"note" : "host192.168.7.7"`, "host192.168.7.7", rPrivateAddress),
		note("private/CGNAT low end", `"note" : "see 100.64.0.1"`, "100.64.0.1", rPrivateAddress),
		note("private/CGNAT high end", `"note" : "see 100.127.255.254"`, "100.127.255.254", rPrivateAddress),
		note("private/IPv6 unique-local fd00", `"note" : "see fd00::1f"`, "fd00::1f", rPrivateAddress),
		note("private/IPv6 unique-local, full", `"note" : "see fd12:3456:789a::1"`, "fd12:3456:789a::1", rPrivateAddress),
		note("private/IPv6 unique-local, upper case", `"note" : "see FD00::2A"`, "FD00::2A", rPrivateAddress),
		note("private/IPv6 link-local", `"note" : "see fe80::a1b2:c3d4"`, "fe80::a1b2:c3d4", rPrivateAddress),
		note("private/IPv6 link-local with a zone", `"note" : "see fe80::1%25eth9"`, "eth9", rPrivateAddress),

		// Rule: internal names and local paths.
		note("internal/cluster context", `"note" : "prod-datomir-7"`, "prod-datomir-7", rInternalName),
		note("internal/Linux home", `"note" : "/home/zed9/.cache/x"`, "/home/zed9/.cache/x", rInternalName),
		note("internal/macOS home", `"note" : "/Users/zed9/Library"`, "/Users/zed9/Library", rInternalName),
		note("internal/secrets file", `"note" : "/opt/zz9/secrets.json"`, "/opt/zz9/secrets.json", rInternalName),
		note("internal/XDG variable", `"note" : "XDG_STATE_HOME=/zz9"`, "XDG_STATE_HOME=/zz9", rInternalName),
		{name: "internal/in the commented header", old: metaAcct, repl: "# account: admin (cluster zz-datomir-8)", planted: "zz-datomir-8", rule: rInternalName},
		note("internal/root home", `"note" : "/root/.docker/config.json"`, "/root/.docker", rInternalName),
		note("internal/root home after a space", `"note" : "see /root/zz9"`, "/root/zz9", rInternalName),
		note("internal/Windows profile, JSON-escaped", `"note" : "C:\\Users\\zed9\\AppData"`, "zed9", rInternalName),
		note("internal/Windows profile, raw", `"note" : "see C:\Users\zed9"`, "zed9", rInternalName),
		note("internal/Windows profile, lower case", `"note" : "c:\\users\\zed9"`, "zed9", rInternalName),
		note("internal/bare .home.arpa name", `"note" : "nas01.home.arpa"`, "nas01.home.arpa", rInternalName),
		note("internal/bare .home.arpa name, upper case", `"note" : "NAS02.HOME.ARPA"`, "NAS02.HOME.ARPA", rInternalName),
		note("internal/bare .lan name", `"note" : "gitlab.corp.lan"`, "gitlab.corp.lan", rInternalName),
		note("internal/bare .internal name", `"note" : "vault.zz9.internal"`, "vault.zz9.internal", rInternalName),
		note("internal/bare .local name", `"note" : "printer.local"`, "printer.local", rInternalName),
		note("internal/a namesake of the upstream, bare", `"note" : "bench-upstream.zz-other.svc.cluster.local"`, "zz-other", rInternalName),

		// Rule: a well-known secret format (DEVDESK-569 #7).
		note("format/GitHub token", `"note" : "`+fakeGitHubToken+`"`, fakeGitHubToken, rSecretFormat),
		note("format/GitLab token", `"note" : "`+fakeGitLabToken+`"`, fakeGitLabToken, rSecretFormat),
		note("format/AWS access key id", `"note" : "AKIAZZ12345678901234"`, "AKIAZZ12345678901234", rSecretFormat),
		note("format/PEM RSA", `"note" : "`+pemBegin("RSA")+`\nMIIzzpem1"`, "MIIzzpem1", rSecretFormat),
		note("format/PEM PKCS8", `"note" : "`+pemBegin("")+`\nMIIzzpem2"`, "MIIzzpem2", rSecretFormat),
		note("format/PEM OpenSSH", `"note" : "`+pemBegin("OPENSSH")+`\nzzpem3"`, "zzpem3", rSecretFormat),
		note("format/PEM EC", `"note" : "`+pemBegin("EC")+`\nzzpem4"`, "zzpem4", rSecretFormat),

		// Rule: a query of the request URL other than n= and last= (DEVDESK-569 #8).
		reqLine("query/access_token", "/service/rest/v1/repositories/docker-proxy?access_token=zz-query-1", "zz-query-1", rQuery),
		reqLine("query/a key that only starts like n", "/v2/_catalog?account=zz-query-2", "zz-query-2", rQuery),
		reqLine("query/n followed by a foreign key", "/v2/_catalog?n=10&token=zz-query-3", "zz-query-3", rQuery),
		reqLine("query/foreign key first", "/v2/_catalog?zz-query-4=1&n=10", "zz-query-4", rQuery),
		reqLine("query/a key without a value", "/v2/_catalog?zz-query-5", "zz-query-5", rQuery),

		// Rule: an exact term (the recorder's passwords, the external deny list).
		{name: "exact/password", old: exactOld, repl: exactRepl, planted: "zz-exact-secret-91", rule: rExactTerm, exact: []string{"zz-exact-secret-91"}},
		{name: "exact/Basic encoding", old: b64Old, repl: b64Repl, planted: "dXNlcjpwYXNzMTIz", rule: rExactTerm, exact: []string{"unrelated-term", "dXNlcjpwYXNzMTIz"}},

		// DEVDESK-569 #4: the term in another case, JSON-escaped or URL-escaped.
		exact("exact/other case", `"note" : "ZZ-Exact-Secret-92"`, "ZZ-Exact-Secret-92", "zz-exact-secret-92"),
		exact("exact/term in upper case, text in lower case", `"note" : "zz-exact-secret-93"`, "zz-exact-secret-93", "ZZ-EXACT-SECRET-93"),
		exact("exact/JSON-escaped quote", `"note" : "zz\"quote94"`, `zz\"quote94`, `zz"quote94`),
		exact("exact/JSON-escaped slash", `"note" : "zz\/slash95"`, `zz\/slash95`, "zz/slash95"),
		exact("exact/JSON-escaped backslash", `"note" : "zz\\back96"`, `zz\\back96`, `zz\back96`),
		exact("exact/JSON-escaped, other case", `"note" : "ZZ\/Slash97"`, `ZZ\/Slash97`, "zz/slash97"),
		exact("exact/URL-escaped with %20", `"note" : "zz%20sec%2Fret98"`, "zz%20sec%2Fret98", "zz sec/ret98"),
		exact("exact/URL-escaped with +", `"note" : "zz+sec%2Fret99"`, "zz+sec%2Fret99", "zz sec/ret99"),
		exact("exact/URL-escaped, lower-case hex", `"note" : "zz%2fsec%3dret100"`, "zz%2fsec%3dret100", "zz/sec=ret100"),
	}
}

func TestLeakIsNeverTheMatchedText(t *testing.T) {
	typ := reflect.TypeFor[nexusfixture.Leak]()
	var fields []string
	for f := range typ.Fields() {
		fields = append(fields, f.Name)
	}
	slices.Sort(fields)
	if want := []string{"Fixture", "Line", "Rule"}; !slices.Equal(fields, want) {
		t.Errorf("Leak fields = %v, want exactly %v: a field for the matched text would put the secret in a test log", fields, want)
	}
}

func TestAllowedHostsAreLoopbackAndTheBenchUpstream(t *testing.T) {
	// realm-probe.invalid (DEVDESK-580) is the reserved name the recorder sends in
	// X-Forwarded-Host to see whether the Bearer realm follows it; .invalid never
	// resolves, so it names no host.
	want := []string{"127.0.0.1", "bench-upstream.nexus-bench.svc.cluster.local", "realm-probe.invalid"}
	if got := nexusfixture.AllowedHosts; !slices.Equal(got, want) {
		t.Errorf("AllowedHosts = %v, want %v", got, want)
	}

	// The upstream is the one the versioned descriptor names, not a second copy
	// that could drift from it; the descriptor's other hosts are not allowed.
	data, err := os.ReadFile(filepath.Join("..", "nexusbench", "testdata", "descriptor.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		Hosts        []string `json:"hosts"`
		Repositories []struct {
			RemoteURL string `json:"remoteUrl"`
		} `json:"repositories"`
	}
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatal(err)
	}
	var upstream string
	for _, r := range d.Repositories {
		if r.RemoteURL == "" {
			continue
		}
		u, err := url.Parse(r.RemoteURL)
		if err != nil {
			t.Fatal(err)
		}
		upstream = u.Hostname()
	}
	if upstream == "" || !slices.Contains(nexusfixture.AllowedHosts, upstream) {
		t.Errorf("the descriptor's proxy upstream %q is not in AllowedHosts %v", upstream, nexusfixture.AllowedHosts)
	}
	for _, h := range d.Hosts {
		if h != "127.0.0.1" && slices.Contains(nexusfixture.AllowedHosts, h) {
			t.Errorf("AllowedHosts holds %q, a LAN address of the descriptor: only loopback may be allowed", h)
		}
	}
}

func TestCheckLeaksEachRuleCatchesItsLeak(t *testing.T) {
	base := leakBase(t)
	if leaks := nexusfixture.CheckLeaks(leakBaseName, []byte(base), nil); len(leaks) != 0 {
		t.Fatalf("the unmodified base is not clean (%v): every case below would be meaningless", leaks)
	}

	expected := map[string]bool{}
	for _, tc := range leakCases() {
		expected[tc.rule] = true
		t.Run(tc.name, func(t *testing.T) {
			assertPlantedLeak(t, plant(t, base, tc.old, tc.repl), tc)
		})
	}
	// Thirteen rules exist (the nine of the first version and the four of
	// DEVDESK-569): each one has at least one case that expects it by name.
	if len(expected) < 13 {
		t.Errorf("the cases expect %d distinct rules (%v), want at least 13", len(expected), expected)
	}
}

// assertPlantedLeak checks one planted fixture: every leak sits on the line of
// the plant, one of them carries the expected rule, and none repeats the value.
func assertPlantedLeak(t *testing.T, data string, tc leakCase) {
	t.Helper()
	at := tc.at
	if at == "" {
		at = tc.planted
	}
	line := lineOf(data, at)
	if line == 0 {
		t.Fatalf("the planted value %q is not in the fixture: the case is broken", at)
	}
	leaks := nexusfixture.CheckLeaks("planted--admin", []byte(data), tc.exact)
	if len(leaks) == 0 {
		t.Fatalf("no leak reported for a planted %s:\n%s", tc.name, data)
	}
	found := false
	for _, l := range leaks {
		if l.Fixture != "planted--admin" {
			t.Errorf("Leak.Fixture = %q, want the name given to CheckLeaks", l.Fixture)
		}
		if l.Rule == "" {
			t.Errorf("a leak at line %d has no Rule", l.Line)
		}
		if l.Line != line {
			t.Errorf("leak %q reported at line %d, want line %d (the only line that changed)", l.Rule, l.Line, line)
		}
		found = found || (l.Rule == tc.rule && l.Line == line)
	}
	if !found {
		t.Errorf("no leak with rule %q at line %d, got %v", tc.rule, line, leaks)
	}
	if echoed := fmt.Sprintf("%+v %#v", leaks, leaks); strings.Contains(echoed, tc.planted) {
		t.Errorf("the leaks repeat the planted value: %s", echoed)
	}
}

func TestCheckLeaksAcceptsCleanContent(t *testing.T) {
	base := leakBase(t)
	respHeaders := "Docker-Distribution-Api-Version: registry/2.0\n" +
		"Docker-Content-Digest: sha256:00000000000000000000000000000000000000000000000000000000000000aa\n" +
		"Link: </v2/_catalog?last=a&n=1>; rel=\"next\"\n" +
		"WWW-Authenticate: Bearer realm=\"http://127.0.0.1:30481/v2/token\",service=\"http://127.0.0.1:30481/v2/token\"\n" +
		"Location: http://127.0.0.1:30482/v2/_catalog\n" +
		"Content-Length: 99999\n"
	inBody := func(member string) string {
		o, r := bodyNote(member)
		return plant(t, base, o, r)
	}
	cases := []struct {
		name  string
		data  string
		exact []string
	}{
		{"the base itself", base, nil},
		{"an empty file", "", nil},
		{"Bearer placeholder", plant(t, base, hdrAuth, "Authorization: Bearer <redacted>\n"), nil},
		{"a request Accept header", plant(t, base, hdrAuth, hdrAuth+"Accept: application/json\n"), nil},
		{"every allowlisted response header", plant(t, base, hdrServer, hdrServer+respHeaders), nil},
		{"loopback with another port", plant(t, base, bodyURL, "\"url\" : \"http://127.0.0.1:30482/repository/docker-group\""), nil},
		{"redacted token", func() string { o, r := bodyNote(`"token" : "<redacted>"`); return plant(t, base, o, r) }(), nil},
		{"redacted uploaderIp", func() string { o, r := bodyNote(`"uploaderIp" : "<redacted>"`); return plant(t, base, o, r) }(), nil},
		{"keys and words that only look like secrets", func() string {
			o, r := bodyNote(`"tokenType" : "keep", "message" : "no token here", "username" : "admin"`)
			return plant(t, base, o, r)
		}(), nil},
		{"an exact term that is absent", base, []string{"zz-not-in-the-file"}},
		{"an empty exact term matches nothing", base, []string{""}},

		// DEVDESK-569: what the stricter rules must not take for a leak.
		{"harmless keys, whatever their case", inBody(`"tokenType" : "keep", "name" : "x", "url" : "u", "remoteUrl" : "r", "memberNames" : [ "docker-hosted", "docker-proxy" ], "Online" : true`), nil},
		{"a key that merely starts with a secret word", inBody(`"tokenCount" : 3`), nil},
		{"an image path that holds /home/", inBody(`"note" : "team/home/api"`), nil},
		{"image paths that hold /root/, /Users/ and /home/", inBody(`"repositories" : [ "team/home/api", "team/root/api", "team/Users/api", "home/api" ]`), nil},
		{"a request target that holds /home/", plant(t, base, baseRequestLine, "GET /v2/team/home/api/tags/list HTTP/1.1\n"), nil},
		{"a request target that starts with /home/ after the version", plant(t, base, baseRequestLine, "GET /v2/home/api/tags/list HTTP/1.1\n"), nil},
		{"a Link header with an image path", plant(t, base, hdrServer, hdrServer+"Link: </v2/team/home/api/tags/list?last=a&n=1>; rel=\"next\"\n"), nil},
		{"query n=1000", plant(t, base, baseRequestLine, "GET /v2/_catalog?n=1000 HTTP/1.1\n"), nil},
		{"query last=5", plant(t, base, baseRequestLine, "GET /v2/_catalog?last=5 HTTP/1.1\n"), nil},
		{"query last then n", plant(t, base, baseRequestLine, "GET /v2/_catalog?last=a&n=1 HTTP/1.1\n"), nil},
		{"query last holding an image path", plant(t, base, baseRequestLine, "GET /v2/_catalog?n=10&last=team%2Fhome%2Fapi HTTP/1.1\n"), nil},
		{"the bench upstream named without a URL", inBody(`"note" : "bench-upstream.nexus-bench.svc.cluster.local"`), nil},
		{"addresses just outside the private ranges", inBody(`"note" : "100.63.255.255 100.128.0.1 172.32.0.1 11.0.0.1"`), nil},
		{"digests that start like an IPv6 prefix", plant(t, base, hdrServer, hdrServer+"Docker-Content-Digest: sha256:fd00000000000000000000000000000000000000000000000000000000000001\nDocker-Content-Digest: sha256:fe80000000000000000000000000000000000000000000000000000000000002\n"), nil},
		{"secret-looking prefixes that are too short", inBody(`"note" : "ghp_short glpat-x AKIA1234 AKIAshortletters"`), nil},
		{"host names that only end like an internal one", inBody(`"note" : "docker.io/library/alpine and local-name and lan.example.com"`), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if leaks := nexusfixture.CheckLeaks("clean--admin", []byte(tc.data), tc.exact); len(leaks) != 0 {
				t.Errorf("CheckLeaks = %v, want none for:\n%s", leaks, tc.data)
			}
		})
	}
}

func TestCheckLeaksReportsEveryLeakWithItsLine(t *testing.T) {
	base := leakBase(t)
	data := plant(t, base, hdrAuth, "Authorization: Basic dXNlcjpwYXNzMTIz\n")
	o, r := bodyNote(`"password" : "zz-planted-password-5"`)
	data = plant(t, data, o, r)

	leaks := nexusfixture.CheckLeaks("two--admin", []byte(data), nil)
	var lines []int
	for _, l := range leaks {
		lines = append(lines, l.Line)
	}
	for _, want := range []int{lineOf(data, "dXNlcjpwYXNzMTIz"), lineOf(data, "zz-planted-password-5")} {
		if !slices.Contains(lines, want) {
			t.Errorf("no leak reported at line %d; reported lines: %v", want, lines)
		}
	}
}

// TestEmbeddedFixturesLeakNothing is the guard of criterion 1: every committed
// fixture, byte for byte as it sits in the tree, passes the leak check.
func TestEmbeddedFixturesLeakNothing(t *testing.T) {
	all, err := nexusfixture.All()
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join("fixtures", "*", "*.http"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 || len(files) != len(all) {
		t.Fatalf("%d fixture files on disk, %d embedded: the scan would not cover what the tests replay", len(files), len(all))
	}

	exact := externalDenylist(t)
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		name := strings.TrimSuffix(filepath.Base(file), ".http")
		for _, l := range nexusfixture.CheckLeaks(name, data, exact) {
			t.Errorf("%s line %d: leak, rule %q", file, l.Line, l.Rule)
		}
	}

	// The same files, as parsed and written back, say the same: Format is what
	// the recorder writes.
	for _, f := range all {
		for _, l := range nexusfixture.CheckLeaks(f.Name, nexusfixture.Format(f), exact) {
			t.Errorf("%s (formatted) line %d: leak, rule %q", f.Name, l.Line, l.Rule)
		}
	}
}

// externalDenylist reads the optional deny list. It is never in the repository:
// its terms are the names of an instance that must not appear anywhere in it.
// It always says how many terms it found, so that an empty or all-comment file
// is not mistaken for a deny list that was applied.
func externalDenylist(t *testing.T) []string {
	t.Helper()
	path := os.Getenv(denylistEnv)
	if path == "" {
		t.Logf("%s is not set: the external deny list was not used", denylistEnv)
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s names a file that cannot be read: %v", denylistEnv, err)
	}
	terms := parseDenylist(data)
	t.Logf("%s: %s", denylistEnv, denylistSummary(len(terms)))
	return terms
}

// parseDenylist keeps the active terms: one per line, blank lines and lines
// starting with # are skipped.
func parseDenylist(data []byte) []string {
	var terms []string
	for line := range bytes.Lines(data) {
		term := strings.TrimSpace(string(line))
		if term != "" && !strings.HasPrefix(term, "#") {
			terms = append(terms, term)
		}
	}
	return terms
}

// denylistSummary is the log line that says what the deny list amounted to.
func denylistSummary(n int) string {
	if n == 0 {
		return "0 terms: the file has no active term, so the external deny list was NOT applied"
	}
	return strconv.Itoa(n) + " terms applied"
}

func TestDenylistFileWithoutActiveTermIsNotSilent(t *testing.T) {
	cases := []struct {
		name, file string
		terms      int
	}{
		{"empty file", "", 0},
		{"only blank lines", "\n  \n\t\n", 0},
		{"only comments", "# names of the instance\n  # another\n", 0},
		{"comments, blanks and two terms", "# c\n\nzz-name-1\n  zz-name-2  \n", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			terms := parseDenylist([]byte(tc.file))
			if len(terms) != tc.terms {
				t.Fatalf("parseDenylist = %q, want %d terms", terms, tc.terms)
			}
			got := denylistSummary(len(terms))
			if tc.terms == 0 && !strings.Contains(got, "0 terms") {
				t.Errorf("summary %q does not say \"0 terms\"", got)
			}
			if tc.terms > 0 && !strings.Contains(got, strconv.Itoa(tc.terms)+" terms") {
				t.Errorf("summary %q does not give the count", got)
			}
		})
	}
}

// A second exchange is scanned like the first: the "###" line starts a request
// again, so its headers are checked against the header rules (a scanner that
// stayed in the body state would let all of these through).
func TestCheckLeaksScansEveryExchange(t *testing.T) {
	base := leakBase(t)
	const second = "###\n" +
		"GET /v2/ HTTP/1.1\n" +
		"Host: 127.0.0.1:30481\n" +
		"Authorization: Basic <redacted>\n" +
		"\n" +
		"HTTP/1.1 200 OK\n" +
		"Server: Nexus/3.98.1-01 (COMMUNITY)\n" +
		"Content-Type: application/json\n" +
		"\n" +
		"{\n" +
		"  \"online\" : true\n" +
		"}\n"
	if leaks := nexusfixture.CheckLeaks("two--admin", []byte(base+second), nil); len(leaks) != 0 {
		t.Fatalf("two clean exchanges are not clean (%v): the cases below would be meaningless", leaks)
	}
	cases := []leakCase{
		{name: "Set-Cookie", old: "Server: Nexus/3.98.1-01 (COMMUNITY)\n", repl: "Server: Nexus/3.98.1-01 (COMMUNITY)\nSet-Cookie: sid=zz-second-1\n", planted: "zz-second-1", rule: rSession},
		{name: "response header outside the allowlist", old: "Server: Nexus/3.98.1-01 (COMMUNITY)\n", repl: "Server: Nexus/3.98.1-01 (COMMUNITY)\nX-Powered-By: zz-second-2\n", planted: "zz-second-2", rule: rRespHeader},
		{name: "Authorization", old: "Authorization: Basic <redacted>\n", repl: "Authorization: Basic zz-second-3\n", planted: "zz-second-3", rule: rAuthorization},
		{name: "request header outside the allowlist", old: "Authorization: Basic <redacted>\n", repl: "Authorization: Basic <redacted>\nX-Api-Key: zz-second-4\n", planted: "zz-second-4", rule: rReqHeader},
		{name: "Host", old: "Host: 127.0.0.1:30481\n", repl: "Host: nexus.zz-second.example\n", planted: "nexus.zz-second.example", rule: rHost},
		{name: "query", old: "GET /v2/ HTTP/1.1\n", repl: "GET /v2/?access_token=zz-second-5 HTTP/1.1\n", planted: "zz-second-5", rule: rQuery},
		{name: "JSON secret", old: "  \"online\" : true\n", repl: "  \"online\" : true,\n  \"token\" : \"zz-second-6\"\n", planted: "zz-second-6", rule: rJSONSecret},
		{name: "line without a colon", old: "Content-Type: application/json\n", repl: "Content-Type: application/json\nzz-second-7\n", planted: "zz-second-7", rule: rMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertPlantedLeak(t, base+plant(t, second, tc.old, tc.repl), tc)
		})
	}
}

// A stub is one line of text in the body position, written by the recorder out
// of what the server sent: its title is not trusted any more than a body is.
func TestCheckLeaksScansTheTitleOfAnHTMLStub(t *testing.T) {
	const name = "catalog-hosted-root-30481--anon"
	base := leakFixture(t, name)
	const title = "<title>404 - Sonatype Nexus Repository</title>"
	if leaks := nexusfixture.CheckLeaks(name, []byte(base), nil); len(leaks) != 0 {
		t.Fatalf("the stub fixture is not clean (%v)", leaks)
	}
	cases := []leakCase{
		{name: "internal name", repl: "<title>404 - zz-datomir-9</title>", planted: "zz-datomir-9", rule: rInternalName},
		{name: "host", repl: "<title>404 - http://nexus.zz-title.example/</title>", planted: "nexus.zz-title.example", rule: rHost},
		{name: "private address", repl: "<title>404 - 10.77.66.55</title>", planted: "10.77.66.55", rule: rPrivateAddress},
		{name: "JWT", repl: "<title>404 - eyJhbGciOiJIUzI1NiJ9zz7</title>", planted: "eyJhbGciOiJIUzI1NiJ9zz7", rule: rJWT},
		{name: "session cookie name", repl: "<title>404 - NXSESSIONID=zz-title-8</title>", planted: "zz-title-8", rule: rSession},
		{name: "exact term", repl: "<title>404 - zz-title-term-9</title>", planted: "zz-title-term-9", rule: rExactTerm, exact: []string{"zz-title-term-9"}},
		{name: "well-known secret format", repl: "<title>404 - " + fakeGitHubToken + "</title>", planted: fakeGitHubToken, rule: rSecretFormat},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertPlantedLeak(t, plant(t, base, title, tc.repl), tc)
		})
	}
}

// secretKeysBody plants n secret keys (one per line, 11 bytes each) in the body
// of the base fixture.
func secretKeysBody(t *testing.T, n int) []byte {
	t.Helper()
	o, r := bodyNote(strings.TrimSuffix(strings.Repeat("\"token\":1,\n", n), ",\n"))
	return []byte(plant(t, leakBase(t), o, r))
}

// A body of about 1 MB made of 100,000 secret keys is reported whole within a
// few seconds. The bound is wide on purpose: it only catches a scan gone
// wrong by orders of magnitude. TestCheckLeaksTimeGrowsLinearly is the one that
// tells linear from quadratic.
func TestCheckLeaksOnALargeBody(t *testing.T) {
	if testing.Short() {
		t.Skip("large input")
	}
	const keys = 100_000
	data := secretKeysBody(t, keys)
	if len(data) < 900_000 {
		t.Fatalf("the input is %d bytes, want about 1 MB", len(data))
	}
	start := time.Now()
	leaks := nexusfixture.CheckLeaks("big--admin", data, nil)
	elapsed := time.Since(start)
	if len(leaks) != keys {
		t.Errorf("%d leaks reported, want one per secret key (%d)", len(leaks), keys)
	}
	if elapsed > 3*time.Second {
		t.Errorf("CheckLeaks took %s on %d bytes with %d secret keys, want well under 3s", elapsed, len(data), keys)
	}
}

// Counting the newlines before each match from the start of the text makes the
// scan quadratic in the number of secret keys: four times the keys then take
// about sixteen times as long (measured on the first version: about 0.2 s for
// 50,000 keys, 2.5 s for 200,000). A linear scan takes about four times as long.
// The bound is nine, plus a floor against timer noise.
func TestCheckLeaksTimeGrowsLinearly(t *testing.T) {
	if testing.Short() {
		t.Skip("large input")
	}
	small, large := secretKeysBody(t, 50_000), secretKeysBody(t, 200_000)
	best := func(data []byte, runs int) time.Duration {
		d := time.Duration(1<<63 - 1)
		for range runs {
			start := time.Now()
			nexusfixture.CheckLeaks("big--admin", data, nil)
			d = min(d, time.Since(start))
		}
		return d
	}
	tSmall, tLarge := best(small, 3), best(large, 1)
	if limit := 9*tSmall + 150*time.Millisecond; tLarge > limit {
		t.Errorf("4x the secret keys took %s against %s for the smaller body (limit %s): the scan is not linear", tLarge, tSmall, limit)
	}
}

func BenchmarkCheckLeaksLargeBody(b *testing.B) {
	fixtures, err := nexusfixture.Load("catalog")
	if err != nil {
		b.Fatal(err)
	}
	var base string
	for _, f := range fixtures {
		if f.Name == leakBaseName {
			base = string(nexusfixture.Format(f))
		}
	}
	o, r := bodyNote(strings.TrimSuffix(strings.Repeat("\"token\":1,\n", 100_000), ",\n"))
	data := []byte(strings.Replace(base, o, r, 1))
	b.ResetTimer()
	for range b.N {
		nexusfixture.CheckLeaks("big--admin", data, nil)
	}
}
