package nexusfixture_test

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/nexusfixture"
)

// DEVDESK-580 step 6: what Redact keeps of the probe headers, what it writes for
// a token answer, and how it hides an address that a bench error message holds
// (plan section 6, risk 1: the 502 of a cut upstream names the upstream's
// cluster address). The raw exchanges are synthetic.

func TestRedactKeepsTheProbeRequestHeadersWithAnAllowedValue(t *testing.T) {
	tests := []struct {
		name    string
		header  [2]string
		want    string // "" = the header must be dropped
		wantKey string
	}{
		{"X-Forwarded-Host, reserved name", [2]string{"X-Forwarded-Host", "realm-probe.invalid"}, "realm-probe.invalid", "X-Forwarded-Host"},
		{"X-Forwarded-Host, loopback with a port", [2]string{"X-Forwarded-Host", "127.0.0.1:30482"}, "127.0.0.1:30482", "X-Forwarded-Host"},
		{"X-Forwarded-Proto, a host is no scheme", [2]string{"X-Forwarded-Proto", "realm-probe.invalid"}, "", "X-Forwarded-Proto"},
		{"X-Forwarded-Proto, https", [2]string{"X-Forwarded-Proto", "https"}, "https", "X-Forwarded-Proto"},
		{"X-Forwarded-Proto, http", [2]string{"X-Forwarded-Proto", "http"}, "http", "X-Forwarded-Proto"},
		{"X-Forwarded-Proto, an unknown scheme", [2]string{"X-Forwarded-Proto", "evil"}, "", "X-Forwarded-Proto"},
		{"X-Forwarded-Proto, upper case is not a scheme the probe sends", [2]string{"X-Forwarded-Proto", "HTTPS"}, "", "X-Forwarded-Proto"},
		{"X-Forwarded-Host, a host no fixture may name", [2]string{"X-Forwarded-Host", "evil.example"}, "", "X-Forwarded-Host"},
		{"X-Forwarded-Host, a private address", [2]string{"X-Forwarded-Host", "10.0.0.1"}, "", "X-Forwarded-Host"},
		{"X-Forwarded-For is not a probe header", [2]string{"X-Forwarded-For", "127.0.0.1"}, "", "X-Forwarded-For"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := rawJSONExchange(`{}`)
			raw.RequestHeader = rawHeader("Authorization", rawBasic, tt.header[0], tt.header[1])
			ex := nexusfixture.Redact(raw)
			got := ex.RequestHeader.Get(tt.wantKey)
			if got != tt.want {
				t.Errorf("%s = %q, want %q", tt.wantKey, got, tt.want)
			}
			if got := ex.RequestHeader.Get("Authorization"); got != "Basic <redacted>" {
				t.Errorf("Authorization = %q, want the placeholder whatever the other headers are", got)
			}
		})
	}
}

// A realm probe fixture is written by Format and must pass CheckLeaks.
func TestRedactedProbeRequestIsClean(t *testing.T) {
	raw := rawJSONExchange(`{}`)
	raw.Target = "/v2/"
	raw.Status = 401
	raw.RequestHeader = rawHeader("X-Forwarded-Host", "realm-probe.invalid", "X-Forwarded-Proto", "http")
	raw.Header = rawHeader(
		"Server", "Nexus/3.98.1-01 (COMMUNITY)",
		"Www-Authenticate", `Bearer realm="http://realm-probe.invalid/v2/token",service="http://realm-probe.invalid/v2/token"`,
		"Content-Type", "application/json",
	)
	data := redactedFixture(t, nexusfixture.Redact(raw))
	if !strings.Contains(string(data), "X-Forwarded-Host: realm-probe.invalid") {
		t.Errorf("the probe header is not in the fixture:\n%s", data)
	}
	if leaks := nexusfixture.CheckLeaks("probe--anon", data, nil); len(leaks) != 0 {
		t.Errorf("CheckLeaks on a redacted probe = %v, want none\n%s", leaks, data)
	}
}

// A token answer: the token and access_token values never reach the file.
func TestRedactTokenAnswer(t *testing.T) {
	raw := rawJSONExchange(`{"token":"zz-token-9f3","access_token":"zz-access-4c1","expires_in":300,"issued_at":"2026-11-20T10:00:00Z"}`)
	raw.Target = "/v2/token?service=http%3A%2F%2F127.0.0.1%3A30482%2Fv2%2Ftoken"
	raw.Host = "127.0.0.1:30482"
	ex := nexusfixture.Redact(raw)
	want := `{"token":"<redacted>","access_token":"<redacted>","expires_in":300,"issued_at":"2026-11-20T10:00:00Z"}`
	if string(ex.Body) != want {
		t.Errorf("Body = %s, want %s", ex.Body, want)
	}
	data := redactedFixture(t, ex)
	if leaks := nexusfixture.CheckLeaks("token--anon", data, nil); len(leaks) != 0 {
		t.Errorf("CheckLeaks on a redacted token answer = %v, want none\n%s", leaks, data)
	}
}

// The rule of risk 1: in the message of a JSON error, a private, link-local or
// CGNAT IPv4 address (with its port) becomes "<redacted-address>". Every other
// byte stays as sent, and an address anywhere else is left for CheckLeaks to
// refuse (all or nothing).
func TestRedactHidesAPrivateAddressInAJSONErrorMessage(t *testing.T) {
	const placeholder = "<redacted-address>"
	tests := []struct {
		name, body, want string
	}{
		{"address and port in a registry error message",
			`{"errors":[{"code":"UNKNOWN","message":"dial tcp 10.43.12.7:5000: i/o timeout"}]}`,
			`{"errors":[{"code":"UNKNOWN","message":"dial tcp ` + placeholder + `: i/o timeout"}]}`},
		{"address without a port",
			`{"message":"Bad Gateway: 192.168.1.20"}`,
			`{"message":"Bad Gateway: ` + placeholder + `"}`},
		{"172.16 range, error key",
			`{"error":"connect 172.16.0.9:8080 refused"}`,
			`{"error":"connect ` + placeholder + ` refused"}`},
		{"link-local, detail key",
			`{"detail":"169.254.1.1"}`,
			`{"detail":"` + placeholder + `"}`},
		{"CGNAT",
			`{"message":"via 100.64.1.1:443"}`,
			`{"message":"via ` + placeholder + `"}`},
		{"two addresses in one message",
			`{"message":"10.0.0.1:1 then 10.0.0.2:2"}`,
			`{"message":"` + placeholder + ` then ` + placeholder + `"}`},
		{"a public address is not private",
			`{"message":"dial tcp 8.8.8.8:53: refused"}`,
			`{"message":"dial tcp 8.8.8.8:53: refused"}`},
		{"a number that is not an address",
			`{"message":"timeout after 10.5 seconds on 3.98.1.01"}`,
			`{"message":"timeout after 10.5 seconds on 3.98.1.01"}`},
		{"an address outside a message key stays for CheckLeaks",
			`{"url":"http://10.0.0.1:5000","message":"ok"}`,
			`{"url":"http://10.0.0.1:5000","message":"ok"}`},
		{"a message with no address is untouched",
			`{"errors":[{"code":"UNAUTHORIZED","message":"authentication required","detail":null}]}`,
			`{"errors":[{"code":"UNAUTHORIZED","message":"authentication required","detail":null}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := rawJSONExchange(tt.body)
			raw.Status = 502
			ex := nexusfixture.Redact(raw)
			if string(ex.Body) != tt.want {
				t.Errorf("Body = %s\nwant   %s", ex.Body, tt.want)
			}
		})
	}
}

// Redact then CheckLeaks: the 502 of a cut upstream, as the bench can send it,
// comes out clean and without the address.
func TestRedactedErrorMessageIsCleanAndAddressFree(t *testing.T) {
	raw := rawJSONExchange(`{"errors":[{"code":"UNKNOWN","message":"dial tcp 10.43.12.7:5000: connect: connection refused"}]}`)
	raw.Status = 502
	data := redactedFixture(t, nexusfixture.Redact(raw))
	if strings.Contains(string(data), "10.43.12.7") {
		t.Errorf("the address is still in the fixture:\n%s", data)
	}
	if !strings.Contains(string(data), "dial tcp <redacted-address>: connect: connection refused") {
		t.Errorf("the message is not what the rule writes:\n%s", data)
	}
	if leaks := nexusfixture.CheckLeaks("cut--anon", data, nil); len(leaks) != 0 {
		t.Errorf("CheckLeaks = %v, want none\n%s", leaks, data)
	}
}
