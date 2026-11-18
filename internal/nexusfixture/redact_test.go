package nexusfixture_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/nexusfixture"
)

// The raw exchanges below are SYNTHETIC (invented credentials and bodies, never
// a Nexus answer): they exercise the redaction rules only.

const (
	rawBasic = "Basic dXNlcjpwYXNz" // user:pass
	rawHTML  = "<!doctype html>\n<html><head><title>404 - Synthetic Page</title></head>\n<body>internal host datomir</body></html>\n"
)

func rawHeader(kv ...string) http.Header {
	h := http.Header{}
	for i := 0; i+1 < len(kv); i += 2 {
		h.Add(kv[i], kv[i+1])
	}
	return h
}

func rawJSONExchange(body string) nexusfixture.RawExchange {
	return nexusfixture.RawExchange{
		Method: "GET", Target: "/service/rest/v1/repositories/docker-proxy", Host: "127.0.0.1:30481",
		RequestHeader: rawHeader("Authorization", rawBasic, "Accept", "application/json"),
		Status:        200,
		Header:        rawHeader("Server", "Nexus/3.98.1-01 (COMMUNITY)", "Content-Type", "application/json"),
		Body:          []byte(body),
	}
}

func TestResponseHeaderAllowlistIsTheClosedList(t *testing.T) {
	want := []string{"Server", "Content-Type", "Content-Length", "Docker-Distribution-Api-Version",
		"Docker-Content-Digest", "Link", "WWW-Authenticate", "Location"}
	got := slices.Clone(nexusfixture.ResponseHeaderAllowlist)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("ResponseHeaderAllowlist = %v, want exactly %v", got, want)
	}
}

func TestRedactKeepsTheRequestLine(t *testing.T) {
	ex := nexusfixture.Redact(rawJSONExchange(`{"name" : "docker-proxy"}`))
	if ex.Method != "GET" || ex.Target != "/service/rest/v1/repositories/docker-proxy" || ex.Host != "127.0.0.1:30481" {
		t.Errorf("request line = %s %s (Host %s), want it passed through", ex.Method, ex.Target, ex.Host)
	}
	if ex.Status != 200 {
		t.Errorf("Status = %d, want 200", ex.Status)
	}
	if ex.Header.Get("Host") != "" || ex.RequestHeader.Get("Host") != "" {
		t.Errorf("Host is Exchange.Host only, not a header (%v / %v)", ex.RequestHeader, ex.Header)
	}
}

func TestRedactAuthorization(t *testing.T) {
	cases := []struct {
		name, value string
		want        string // "" = the header must be gone or carry no part of the value
	}{
		{"basic", rawBasic, "Basic <redacted>"},
		{"basic lowercase scheme", "basic dXNlcjpwYXNz", "Basic <redacted>"},
		{"bearer", "Bearer s3cr3t-opaque-token", "Bearer <redacted>"},
		{"already placeholder", "Basic <redacted>", "Basic <redacted>"},
		{"unknown scheme", "Digest username=\"bob\", response=\"abcdef\"", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := rawJSONExchange(`{}`)
			raw.RequestHeader = rawHeader("Authorization", tc.value)
			ex := nexusfixture.Redact(raw)
			got := ex.RequestHeader.Get("Authorization")
			if tc.want != "" && got != tc.want {
				t.Errorf("Authorization = %q, want %q", got, tc.want)
			}
			if tc.want == "" && got != "" && got != "Basic <redacted>" && got != "Bearer <redacted>" {
				t.Errorf("Authorization = %q, want it absent or a placeholder", got)
			}
			for _, secret := range []string{"dXNlcjpwYXNz", "s3cr3t-opaque-token", "abcdef", "bob"} {
				if strings.Contains(strings.Join(ex.RequestHeader.Values("Authorization"), "|"), secret) {
					t.Errorf("Authorization %v still carries %q", ex.RequestHeader.Values("Authorization"), secret)
				}
			}
		})
	}
}

func TestRedactRequestHeaders(t *testing.T) {
	raw := rawJSONExchange(`{}`)
	raw.RequestHeader = rawHeader(
		"Authorization", rawBasic,
		"Accept", "application/json",
		"Cookie", "NXSESSIONID=abc123",
		"X-Nexus-UI", "true",
	)
	ex := nexusfixture.Redact(raw)
	if got := ex.RequestHeader.Get("Accept"); got != "application/json" {
		t.Errorf("Accept = %q, want it kept as sent", got)
	}
	for _, name := range []string{"Cookie", "X-Nexus-UI"} {
		if v := ex.RequestHeader.Values(name); len(v) != 0 {
			t.Errorf("request header %s = %v, want it dropped", name, v)
		}
	}
}

func TestRedactResponseHeadersKeepOnlyTheAllowlist(t *testing.T) {
	raw := rawJSONExchange(`{"repositories":[]}`)
	raw.Header = rawHeader(
		"Server", "Nexus/3.98.1-01 (COMMUNITY)",
		"Content-Type", "application/json",
		"Content-Length", "19",
		"Docker-Distribution-Api-Version", "registry/2.0",
		"Docker-Content-Digest", "sha256:0000",
		"Link", `</v2/_catalog?last=a&n=1>; rel="next"`,
		"WWW-Authenticate", `Bearer realm="http://127.0.0.1:30481/v2/token"`,
		"Location", "http://127.0.0.1:30481/elsewhere",
		"Set-Cookie", "NXSESSIONID=abc123; Path=/",
		"X-Foo", "bar",
		"Date", "Tue, 17 Nov 2026 10:00:00 GMT",
		"X-Content-Type-Options", "nosniff",
	)
	ex := nexusfixture.Redact(raw)
	for _, name := range nexusfixture.ResponseHeaderAllowlist {
		want := raw.Header.Values(name)
		if got := ex.Header.Values(name); !slices.Equal(got, want) {
			t.Errorf("response header %s = %v, want %v (kept as sent)", name, got, want)
		}
	}
	for name := range ex.Header {
		if !slices.ContainsFunc(nexusfixture.ResponseHeaderAllowlist, func(a string) bool { return http.CanonicalHeaderKey(a) == name }) {
			t.Errorf("response header %s survived, it is not in the allowlist", name)
		}
	}
}

func TestRedactDoesNotModifyItsInput(t *testing.T) {
	raw := rawJSONExchange(`{"token" : "abc"}`)
	raw.RequestHeader.Set("Cookie", "NXSESSIONID=abc")
	raw.Header.Set("Set-Cookie", "NXSESSIONID=abc")
	_ = nexusfixture.Redact(raw)
	if raw.RequestHeader.Get("Authorization") != rawBasic || raw.RequestHeader.Get("Cookie") == "" {
		t.Errorf("request headers of the input changed: %v", raw.RequestHeader)
	}
	if raw.Header.Get("Set-Cookie") == "" {
		t.Errorf("response headers of the input changed: %v", raw.Header)
	}
	if string(raw.Body) != `{"token" : "abc"}` {
		t.Errorf("body of the input changed: %q", raw.Body)
	}
}

func TestRedactBodies(t *testing.T) {
	htmlRaw := rawJSONExchange("")
	htmlRaw.Status = 404
	htmlRaw.Header = rawHeader("Server", "Nexus/3.98.1-01 (COMMUNITY)", "Content-Type", "text/html;charset=utf-8", "Content-Length", "109")
	htmlRaw.Body = []byte(rawHTML)

	emptyRaw := rawJSONExchange("")
	emptyRaw.Status = 403
	emptyRaw.Body = nil

	jsonRaw := rawJSONExchange(`{"repositories":["api","team/api"]}`)

	t.Run("HTML is summarised with its size and title", func(t *testing.T) {
		ex := nexusfixture.Redact(htmlRaw)
		want := "<HTML body omitted: " + strconv.Itoa(len(rawHTML)) + " bytes, <title>404 - Synthetic Page</title>>"
		if ex.Stub != want {
			t.Errorf("Stub = %q, want %q", ex.Stub, want)
		}
		if ex.Body != nil {
			t.Errorf("Body = %q, want nil when Stub is set", ex.Body)
		}
	})
	t.Run("an empty body reads <empty body>", func(t *testing.T) {
		ex := nexusfixture.Redact(emptyRaw)
		if ex.Stub != "<empty body>" || ex.Body != nil {
			t.Errorf("Stub = %q, Body = %q, want <empty body> and nil", ex.Stub, ex.Body)
		}
	})
	t.Run("a JSON body is kept verbatim", func(t *testing.T) {
		ex := nexusfixture.Redact(jsonRaw)
		if ex.Stub != "" || string(ex.Body) != `{"repositories":["api","team/api"]}` {
			t.Errorf("Stub = %q, Body = %q, want the body untouched", ex.Stub, ex.Body)
		}
	})
}

func TestRedactSecretJSONValues(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{
			"uploaderIp spaced like Nexus writes it",
			`{ "name" : "x", "uploaderIp" : "10.1.2.3" }`,
			`{ "name" : "x", "uploaderIp" : "<redacted>" }`,
		},
		{
			"compact",
			`{"token":"abc","access_token":"def","refresh_token":"ghi"}`,
			`{"token":"<redacted>","access_token":"<redacted>","refresh_token":"<redacted>"}`,
		},
		{
			"password and secret, nested",
			`{"a":{"password": "p","b":[{"secret":"s"}]}}`,
			`{"a":{"password": "<redacted>","b":[{"secret":"<redacted>"}]}}`,
		},
		{
			"a value with an escaped quote is replaced whole",
			`{"password":"p\"q\\","x":"y"}`,
			`{"password":"<redacted>","x":"y"}`,
		},
		{
			"every other byte stays: escapes, unicode, layout",
			"{\n  \"name\" : \"caf\\u00e9 \\/ \u00e9\",\n  \"token\" : \"abc\",\n  \"n\" : 1.50\n}",
			"{\n  \"name\" : \"caf\\u00e9 \\/ \u00e9\",\n  \"token\" : \"<redacted>\",\n  \"n\" : 1.50\n}",
		},
		{
			"other keys and words in values are left alone",
			`{"username" : "admin", "message" : "no token here", "tokenType" : "keep"}`,
			`{"username" : "admin", "message" : "no token here", "tokenType" : "keep"}`,
		},
		{
			"an already redacted value is stable",
			`{"token" : "<redacted>"}`,
			`{"token" : "<redacted>"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ex := nexusfixture.Redact(rawJSONExchange(tc.in))
			if string(ex.Body) != tc.want {
				t.Errorf("Body =\n%s\nwant\n%s", ex.Body, tc.want)
			}
		})
	}
}

// TestRedactThenCheckIsClean is the contract between the recorder's two steps:
// whatever Redact writes, CheckLeaks accepts.
func TestRedactThenCheckIsClean(t *testing.T) {
	html := rawJSONExchange("")
	html.Target = "/v2/_catalog"
	html.Status = 404
	html.RequestHeader = rawHeader("Authorization", rawBasic, "Cookie", "NXSESSIONID=abc123")
	html.Header = rawHeader(
		"Server", "Nexus/3.98.1-01 (COMMUNITY)",
		"Content-Type", "text/html;charset=utf-8",
		"Set-Cookie", "NXSESSIONID=abc123; Path=/",
		"X-Foo", "bar",
	)
	html.Body = []byte(rawHTML)

	js := rawJSONExchange("{\n  \"name\" : \"docker-proxy\",\n  \"uploaderIp\" : \"10.1.2.3\",\n  \"url\" : \"http://127.0.0.1:30481/repository/docker-proxy\"\n}")
	js.Header = rawHeader("Server", "Nexus/3.98.1-01 (COMMUNITY)", "Content-Type", "application/json", "Set-Cookie", "NXSESSIONID=zzz")

	for name, raw := range map[string]nexusfixture.RawExchange{"html 404": html, "json with uploaderIp": js} {
		t.Run(name, func(t *testing.T) {
			base, err := nexusfixture.Parse("c", "synthetic--admin.http", []byte(synthValid))
			if err != nil {
				t.Fatal(err)
			}
			ex := nexusfixture.Redact(raw)
			if got := ex.RequestHeader.Get("Authorization"); got != "Basic <redacted>" {
				t.Errorf("Authorization = %q, want %q", got, "Basic <redacted>")
			}
			base.Exchanges = []nexusfixture.Exchange{ex}
			data := nexusfixture.Format(base)
			for _, secret := range []string{"dXNlcjpwYXNz", "NXSESSIONID", "10.1.2.3", "datomir", "X-Foo"} {
				if bytes.Contains(data, []byte(secret)) {
					t.Errorf("the formatted fixture still holds %q:\n%s", secret, data)
				}
			}
			if leaks := nexusfixture.CheckLeaks("synthetic--admin", data, nil); len(leaks) != 0 {
				t.Errorf("CheckLeaks on a redacted exchange = %v, want none\n%s", leaks, data)
			}
			if _, err := nexusfixture.Parse("c", "synthetic--admin.http", data); err != nil {
				t.Errorf("the redacted fixture does not parse back: %v\n%s", err, data)
			}
		})
	}
}

// TestRedactLeavesARealAnswerAsItIs: the recorder reads a bench answer, and an
// answer that was already clean must come out the same, byte for byte, in the
// file (that is what lets it write nothing for an unchanged answer). Stub
// fixtures cannot be turned back into a raw body and are left out.
func TestRedactLeavesARealAnswerAsItIs(t *testing.T) {
	all, err := nexusfixture.All()
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, f := range all {
		for i, ex := range f.Exchanges {
			if ex.Stub != "" {
				continue
			}
			checked++
			raw := nexusfixture.RawExchange{
				Method: ex.Method, Target: ex.Target, Host: ex.Host,
				RequestHeader: ex.RequestHeader.Clone(), Status: ex.Status,
				Header: ex.Header.Clone(), Body: slices.Clone(ex.Body),
			}
			got := nexusfixture.Redact(raw)
			got.Reason, ex.Reason = "", "" // the raw answer has none: Format falls back to the standard phrase

			want, have := f, f
			want.Exchanges, have.Exchanges = []nexusfixture.Exchange{ex}, []nexusfixture.Exchange{got}
			if w, h := nexusfixture.Format(want), nexusfixture.Format(have); !bytes.Equal(w, h) {
				t.Errorf("%s exchange %d: Redact changed a clean answer\nwant:\n%s\ngot:\n%s", f.Name, i+1, w, h)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no verbatim exchange was checked: the test is vacuous")
	}
}

// DEVDESK-569 #1: the value of a secret key may be an object or an array. Redact
// replaces the whole value, balanced, by "<redacted>": whatever the value held,
// strings with brackets or escaped quotes included, leaves no byte behind.
func TestRedactSecretKeyWithAnObjectOrArrayValue(t *testing.T) {
	cases := []struct{ name, in, want, planted string }{
		{"object", `{"token" : {"v":"zz-obj-1"}}`, `{"token" : "<redacted>"}`, "zz-obj-1"},
		{"array", `{"password":["zz-arr-1"],"x":1}`, `{"password":"<redacted>","x":1}`, "zz-arr-1"},
		{"nested", `{"secret":{"a":{"b":["zz-deep-2",{"c":2}]}},"after":true}`, `{"secret":"<redacted>","after":true}`, "zz-deep-2"},
		{"brackets inside strings do not unbalance it", `{"token":{"a":"}","b":["]",{"c":"zz-br-3"}]},"x":"y"}`, `{"token":"<redacted>","x":"y"}`, "zz-br-3"},
		{"an escaped quote inside a string", `{"secret":{"a":"q\"}zz-esc-4"},"x":1}`, `{"secret":"<redacted>","x":1}`, "zz-esc-4"},
		{"several lines", "{\n  \"token\" : {\n    \"v\" : \"zz-ml-5\"\n  },\n  \"n\" : 1\n}", "{\n  \"token\" : \"<redacted>\",\n  \"n\" : 1\n}", "zz-ml-5"},
		{"an array of objects", `{"password":[{"v":"zz-ao-6"},{"v":"zz-ao-7"}]}`, `{"password":"<redacted>"}`, "zz-ao-6"},
		{"two secret keys, one of each kind", `{"token":{"v":"zz-two-8"},"secret":"zz-two-9"}`, `{"token":"<redacted>","secret":"<redacted>"}`, "zz-two-8"},
		{"a secret key nested in the value of another", `{"credentials":{"password":"zz-in-10"}}`, `{"credentials":"<redacted>"}`, "zz-in-10"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ex := nexusfixture.Redact(rawJSONExchange(tc.in))
			if string(ex.Body) != tc.want {
				t.Errorf("Body =\n%s\nwant\n%s", ex.Body, tc.want)
			}
			if bytes.Contains(ex.Body, []byte(tc.planted)) {
				t.Errorf("Body still holds the secret %q:\n%s", tc.planted, ex.Body)
			}
			if !json.Valid(ex.Body) {
				t.Errorf("Body is not valid JSON any more (the replacement is not balanced):\n%s", ex.Body)
			}
			if leaks := nexusfixture.CheckLeaks("x--admin", redactedFixture(t, ex), nil); len(leaks) != 0 {
				t.Errorf("CheckLeaks on the redacted exchange = %v, want none", leaks)
			}
		})
	}

	// A value cut short (a truncated recording) cannot be balanced: Redact must
	// still not keep anything of it.
	t.Run("an unterminated value keeps nothing", func(t *testing.T) {
		for _, in := range []string{`{"token":{"v":"zz-cut-11"`, `{"password":["zz-cut-12",`, `{"secret":{"a":"zz-cut-13`} {
			ex := nexusfixture.Redact(rawJSONExchange(in))
			if bytes.Contains(ex.Body, []byte("zz-cut-")) {
				t.Errorf("Redact(%q).Body = %q, still holds the secret", in, ex.Body)
			}
		}
	})
}

// redactedFixture formats an exchange as a fixture file.
func redactedFixture(t *testing.T, ex nexusfixture.Exchange) []byte {
	t.Helper()
	base, err := nexusfixture.Parse("c", "synthetic--admin.http", []byte(synthValid))
	if err != nil {
		t.Fatal(err)
	}
	base.Exchanges = []nexusfixture.Exchange{ex}
	return nexusfixture.Format(base)
}

// DEVDESK-569 #2: a key names a secret whatever its case, and by its ending
// (accessToken, proxyPassword, apiKey...), not only by the six exact names.
func TestRedactSecretKeyForms(t *testing.T) {
	secret := []string{
		"Password", "PASSWORD", "Token", "TOKEN", "Secret", "accessToken", "bearerToken", "refreshToken",
		"proxyPassword", "apiKey", "client_secret", "privateKey", "credentials", "Credentials",
		"access_token", "uploaderIp", "UploaderIP",
	}
	for _, key := range secret {
		t.Run(key, func(t *testing.T) {
			in := `{"name" : "x", "` + key + `" : "zz-form-1", "after" : 1}`
			want := `{"name" : "x", "` + key + `" : "<redacted>", "after" : 1}`
			ex := nexusfixture.Redact(rawJSONExchange(in))
			if string(ex.Body) != want {
				t.Errorf("Body =\n%s\nwant\n%s", ex.Body, want)
			}
			if leaks := nexusfixture.CheckLeaks("x--admin", redactedFixture(t, ex), nil); len(leaks) != 0 {
				t.Errorf("CheckLeaks on the redacted exchange = %v, want none", leaks)
			}
		})
	}
	t.Run("credentials holding an object", func(t *testing.T) {
		ex := nexusfixture.Redact(rawJSONExchange(`{"credentials" : {"user":"zz-form-2"}}`))
		if string(ex.Body) != `{"credentials" : "<redacted>"}` {
			t.Errorf("Body = %s", ex.Body)
		}
	})

	harmless := `{"name" : "docker-proxy", "url" : "http://127.0.0.1:30481/x", "remoteUrl" : "http://bench-upstream.nexus-bench.svc.cluster.local:5000", ` +
		`"memberNames" : [ "a", "b" ], "tokenType" : "keep", "tokenCount" : 3, "username" : "admin", "message" : "no password here", "online" : true}`
	t.Run("harmless keys are left alone", func(t *testing.T) {
		if ex := nexusfixture.Redact(rawJSONExchange(harmless)); string(ex.Body) != harmless {
			t.Errorf("Body =\n%s\nwant it untouched:\n%s", ex.Body, harmless)
		}
	})
}

// DEVDESK-569 #3: a body that is neither JSON nor HTML is not kept: it is
// summarised like the HTML stub (its size and its type), since nothing tells
// where a secret sits in it. The summary must be one line, hold no byte of the
// body, round-trip through the file format as a stub, and pass CheckLeaks.
func TestRedactSummarisesABodyThatIsNeitherJSONNorHTML(t *testing.T) {
	filler := strings.Repeat("&pad=1", 20)
	cases := []struct {
		name, contentType, body, mediaType, planted string
	}{
		{"plain text", "text/plain", "token=zz-plain-1&x=1" + filler, "text/plain", "zz-plain-1"},
		{"plain text with a charset", "text/plain; charset=utf-8", "the password is zz-plain-2" + filler, "text/plain", "zz-plain-2"},
		{"a form", "application/x-www-form-urlencoded", "token=zz-form-3&x=1" + filler, "application/x-www-form-urlencoded", "zz-form-3"},
		{"octet stream", "application/octet-stream", "zz-bin-4\x00\x01" + filler, "application/octet-stream", "zz-bin-4"},
		{"XML", "application/xml", "<a><token>zz-xml-5</token></a>" + filler, "application/xml", "zz-xml-5"},
		{"text that looks like JSON", "text/plain", `{"token":"zz-json-6"}` + filler, "text/plain", "zz-json-6"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := rawJSONExchange("")
			raw.Header = rawHeader("Server", "Nexus/3.98.1-01 (COMMUNITY)", "Content-Type", tc.contentType)
			raw.Body = []byte(tc.body)
			ex := nexusfixture.Redact(raw)
			if ex.Body != nil {
				t.Fatalf("Body = %q, want nil: the body must not be kept", ex.Body)
			}
			if ex.Stub == "" {
				t.Fatal("Stub is empty: want a summary of the body")
			}
			if strings.ContainsAny(ex.Stub, "\r\n") {
				t.Errorf("Stub = %q, want one line", ex.Stub)
			}
			if strings.Contains(ex.Stub, tc.planted) {
				t.Errorf("Stub = %q repeats the secret", ex.Stub)
			}
			if !strings.Contains(ex.Stub, strconv.Itoa(len(tc.body))) {
				t.Errorf("Stub = %q, want the body size (%d)", ex.Stub, len(tc.body))
			}
			if !strings.Contains(ex.Stub, tc.mediaType) {
				t.Errorf("Stub = %q, want the media type %q", ex.Stub, tc.mediaType)
			}

			data := redactedFixture(t, ex)
			if bytes.Contains(data, []byte(tc.planted)) {
				t.Errorf("the formatted fixture holds the secret:\n%s", data)
			}
			if leaks := nexusfixture.CheckLeaks("x--admin", data, nil); len(leaks) != 0 {
				t.Errorf("CheckLeaks = %v, want none:\n%s", leaks, data)
			}
			// The file must read it back as a stub (not as a body to replay).
			f, err := nexusfixture.Parse("c", "synthetic--admin.http", data)
			if err != nil {
				t.Fatalf("the fixture does not parse back: %v\n%s", err, data)
			}
			if got := f.Exchanges[0]; got.Stub != ex.Stub || got.Body != nil {
				t.Errorf("parsed back Stub = %q, Body = %q, want the stub %q and no body", got.Stub, got.Body, ex.Stub)
			}
		})
	}

	// What stays a body: JSON, in any of its media types.
	for _, ct := range []string{
		"application/json", "application/json;charset=utf-8", "Application/JSON",
		"application/vnd.docker.distribution.manifest.v2+json", "application/vnd.oci.image.index.v1+json",
	} {
		t.Run("JSON is kept: "+ct, func(t *testing.T) {
			raw := rawJSONExchange(`{"repositories":["api"]}`)
			raw.Header = rawHeader("Content-Type", ct)
			if ex := nexusfixture.Redact(raw); ex.Stub != "" || string(ex.Body) != `{"repositories":["api"]}` {
				t.Errorf("Stub = %q, Body = %q, want the JSON body kept", ex.Stub, ex.Body)
			}
		})
	}
	t.Run("HTML is still the HTML stub", func(t *testing.T) {
		raw := rawJSONExchange("")
		raw.Header = rawHeader("Content-Type", "text/html;charset=utf-8")
		raw.Body = []byte(rawHTML)
		if ex := nexusfixture.Redact(raw); !strings.HasPrefix(ex.Stub, "<HTML body omitted:") || ex.Body != nil {
			t.Errorf("Stub = %q, Body = %q, want the HTML stub", ex.Stub, ex.Body)
		}
	})
}
