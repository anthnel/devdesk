package nexusfixture_test

import (
	"testing"

	"github.com/anthnel/devdesk/internal/nexusfixture"
)

// DEVDESK-580 step 6: the token request parameters and the request headers the
// recorder sends to measure a Bearer realm. Each case plants one line into the
// real NX-03 base of leaks_test.go; the values are invented.

const (
	tokenService = "http%3A%2F%2F127.0.0.1%3A30482%2Fv2%2Ftoken"
	tokenScope   = "registry%3Acatalog%3A%2A"
)

// requestLineCase replaces the request line of the base.
func requestLineCase(name, target, planted, rule string) leakCase {
	return leakCase{name: name, old: baseRequestLine, repl: "GET " + target + " HTTP/1.1\n", planted: planted, rule: rule}
}

// TestCheckLeaksAcceptsTheTokenRequestAndTheProbeHeaders: what the recorder's
// Bearer flow and realm probe write must not be a leak.
func TestCheckLeaksAcceptsTheTokenRequestAndTheProbeHeaders(t *testing.T) {
	base := leakBase(t)
	request := func(target string) string { return plant(t, base, baseRequestLine, "GET "+target+" HTTP/1.1\n") }
	reqHeader := func(line string) string { return plant(t, base, hdrAuth, hdrAuth+line+"\n") }
	respHeader := func(line string) string { return plant(t, base, hdrServer, hdrServer+line+"\n") }

	cases := []struct{ name, data string }{
		{"token request, encoded service and scope", request("/v2/token?service=" + tokenService + "&scope=" + tokenScope)},
		{"token request, plain service and scope", request("/v2/token?service=http://127.0.0.1:30482/v2/token&scope=registry:catalog:*")},
		{"token request, scope first as Go encodes it", request("/v2/token?scope=" + tokenScope + "&service=" + tokenService)},
		{"token request under a repository path", request("/repository/docker-hosted/v2/token?service=http%3A%2F%2F127.0.0.1%3A30481%2Frepository%2Fdocker-hosted%2Fv2%2Ftoken&scope=repository%3Ateam%2Fapi%3Apull")},
		{"token request, service is a bare loopback host and port", request("/v2/token?service=127.0.0.1:30482")},
		{"token request, service on the bench upstream", request("/v2/token?service=http%3A%2F%2Fbench-upstream.nexus-bench.svc.cluster.local%3A5000%2Fv2%2Ftoken")},
		{"token request with no query", request("/v2/token")},
		{"catalog paging query is unchanged", request("/v2/_catalog?n=1000&last=a")},

		{"X-Forwarded-Host with the reserved probe name", reqHeader("X-Forwarded-Host: realm-probe.invalid")},
		{"X-Forwarded-Host with an allowed host and port", reqHeader("X-Forwarded-Host: 127.0.0.1:30482")},
		// A real probe sends the scheme it measures with: exactly http or https.
		{"X-Forwarded-Proto https", reqHeader("X-Forwarded-Proto: https")},
		{"X-Forwarded-Proto http", reqHeader("X-Forwarded-Proto: http")},
		{"a realm that followed the probe", respHeader(`WWW-Authenticate: Bearer realm="http://realm-probe.invalid/v2/token",service="http://realm-probe.invalid/v2/token"`)},

		{"Bearer placeholder, the only form", plant(t, base, hdrAuth, "Authorization: Bearer <redacted>\n")},
		{"token answer with redacted token fields", plant(t, base, bodyOnl, "  \"token\" : \"<redacted>\",\n  \"access_token\" : \"<redacted>\",\n  \"expires_in\" : 300,\n")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if leaks := nexusfixture.CheckLeaks("clean--admin", []byte(tc.data), nil); len(leaks) != 0 {
				t.Errorf("CheckLeaks = %v, want none for:\n%s", leaks, tc.data)
			}
		})
	}
}

// TestCheckLeaksTokenAndProbeRules is the table input -> rule. A case passes when
// a leak with the rule sits on the planted line, no leak sits elsewhere, and none
// repeats the planted value (assertPlantedLeak).
func TestCheckLeaksTokenAndProbeRules(t *testing.T) {
	base := leakBase(t)
	reqHdr := func(name, line, planted, rule string) leakCase {
		return leakCase{name: name, old: hdrAuth, repl: hdrAuth + line + "\n", planted: planted, rule: rule}
	}
	cases := []leakCase{
		// service and scope are admitted on a token path only.
		requestLineCase("scope on the catalog", "/v2/_catalog?scope=x", "scope=x", rQuery),
		requestLineCase("service on the catalog", "/v2/_catalog?service=http%3A%2F%2F127.0.0.1%3A30482%2Fv2%2Ftoken", "service=", rQuery),
		requestLineCase("scope on a token-like path", "/v2/tokens?scope=x", "scope=x", rQuery),
		requestLineCase("scope on a path that only starts like the token path", "/v2/token/extra?scope=x", "scope=x", rQuery),
		requestLineCase("scope on tags/list", "/v2/team/api/tags/list?scope=x", "scope=x", rQuery),
		// account is never admitted, on a token path either.
		requestLineCase("account on the token path", "/v2/token?account=nx-admin&service="+tokenService, "account=nx-admin", rQuery),
		requestLineCase("an unknown parameter next to the token ones", "/v2/token?service="+tokenService+"&client_id=zz-client-7", "client_id=zz-client-7", rQuery),
		// service values go through the host rule, also once URL-decoded.
		requestLineCase("service on another host, URL-encoded", "/v2/token?service=http%3A%2F%2Fevil.example%2Fv2%2Ftoken", "evil.example", rHost),
		requestLineCase("service on another host, plain", "/v2/token?service=http://evil.example/v2/token", "evil.example", rHost),
		requestLineCase("service that is a bare foreign host", "/v2/token?service=registry.zz-foreign.example", "registry.zz-foreign.example", rHost),
		requestLineCase("service with user info", "/v2/token?service=http%3A%2F%2Fzz-user%40127.0.0.1%2Fv2%2Ftoken", "zz-user", rHost),
		requestLineCase("service on a private address, plain", "/v2/token?service=http://10.0.0.1/", "10.0.0.1", rPrivateAddress),
		requestLineCase("service on a private address, URL-encoded", "/v2/token?service=http%3A%2F%2F192.168.7.7%2Fv2%2Ftoken", "192.168.7.7", rPrivateAddress),
		requestLineCase("service on an internal name", "/v2/token?service=http%3A%2F%2Fregistry.zz.home.arpa%2Fv2%2Ftoken", "registry.zz.home.arpa", rInternalName),

		// A URL-decoding that makes a match disappear ("%2D" joins "-mirror" to the
		// name, "%41" an "A") must not hide an internal name: raw and decoded are both scanned.
		{name: "internal name followed by an encoded hyphen", old: bodyOnl, repl: bodyOnl + "  \"note\" : \"nexus.home.arpa%2Dmirror\",\n", planted: "nexus.home.arpa%2Dmirror", rule: rInternalName},
		{name: "internal name followed by an encoded letter", old: bodyOnl, repl: bodyOnl + "  \"note\" : \"x.lan%41\",\n", planted: "x.lan%41", rule: rInternalName},
		requestLineCase("internal name followed by an encoded hyphen in a service", "/v2/token?service=nexus.home.arpa%2Dmirror", "nexus.home.arpa%2Dmirror", rInternalName),

		// The probe headers: the name is admitted, the value goes through the host rule.
		reqHdr("X-Forwarded-Host on another host", "X-Forwarded-Host: evil.example", "evil.example", rHost),
		reqHdr("X-Forwarded-Host, lower-case name", "x-forwarded-host: evil.example", "evil.example", rHost),
		reqHdr("X-Forwarded-Host on a private address", "X-Forwarded-Host: 10.0.0.1", "10.0.0.1", rPrivateAddress),
		reqHdr("X-Forwarded-Host on a name that only ends like the reserved one", "X-Forwarded-Host: realm-probe.invalid.evil.example", "realm-probe.invalid.evil.example", rHost),
		reqHdr("X-Forwarded-Proto on another host", "X-Forwarded-Proto: evil.example", "evil.example", rHost),
		reqHdr("X-Forwarded-Proto with the reserved probe host, a host is no scheme", "X-Forwarded-Proto: realm-probe.invalid", "realm-probe.invalid", rHost),
		reqHdr("X-Forwarded-Proto, an unknown scheme", "X-Forwarded-Proto: evil", "evil", rHost),
		reqHdr("X-Forwarded-Proto, exact match only", "X-Forwarded-Proto: HTTPS", "HTTPS", rHost),
		reqHdr("X-Forwarded-Proto, scheme with text", "X-Forwarded-Proto: https evil", "https evil", rHost),
		reqHdr("X-Forwarded-For stays refused", "X-Forwarded-For: 127.0.0.1", "X-Forwarded-For", rReqHeader),

		// Bearer <redacted> is the only form of a Bearer Authorization (these held
		// before DEVDESK-580 and must keep holding with the token flow).
		{name: "Bearer with the fake token form", old: hdrAuth, repl: "Authorization: Bearer fixture-token-admin\n", planted: "fixture-token-admin", rule: rAuthorization},
		{name: "Bearer placeholder followed by text", old: hdrAuth, repl: "Authorization: Bearer <redacted> zz-extra-1\n", planted: "zz-extra-1", rule: rAuthorization},
		{name: "bearer in lower case", old: hdrAuth, repl: "Authorization: bearer <redacted>\n", planted: "bearer <redacted>", rule: rAuthorization},
		{name: "token answer with a raw token", old: bodyOnl, repl: bodyOnl + "  \"token\" : \"zz-raw-token-1\",\n", planted: "zz-raw-token-1", rule: rJSONSecret},
		{name: "token answer with a raw access_token", old: bodyOnl, repl: bodyOnl + "  \"access_token\" : \"zz-raw-token-2\",\n", planted: "zz-raw-token-2", rule: rJSONSecret},
	}
	if leaks := nexusfixture.CheckLeaks(leakBaseName, []byte(base), nil); len(leaks) != 0 {
		t.Fatalf("the unmodified base is not clean (%v): every case below would be meaningless", leaks)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertPlantedLeak(t, plant(t, base, tc.old, tc.repl), tc)
		})
	}
}
