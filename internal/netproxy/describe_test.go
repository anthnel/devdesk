package netproxy

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
)

// Distinctive credential tokens put in every proxy URL these tests build. A
// Decision, or anything Display returns, must never carry either of them
// (DEVDESK-582: the Proxy row shows which proxy applies, never who logs in).
const (
	secretUser = "s3cretUser"
	secretPass = "s3cretPass"
)

const (
	// describeHelperEnv makes the test binary run TestDescribeHelper for real.
	describeHelperEnv = "NETPROXY_TEST_DESCRIBE_HELPER"
)

// kindName names a DecisionKind so a failure reads without a lookup.
func kindName(k DecisionKind) string {
	switch k {
	case DecisionNone:
		return "None"
	case DecisionProxied:
		return "Proxied"
	case DecisionExcluded:
		return "Excluded"
	case DecisionLoopback:
		return "Loopback"
	case DecisionInvalid:
		return "Invalid"
	}
	return fmt.Sprintf("DecisionKind(%d)", int(k))
}

// env is a fake environment: getenv reads from it, an absent name is "".
type env map[string]string

func (e env) get(name string) string { return e[name] }

// neverSelect is a Selector that fails the test when it is consulted. Cases
// where the variable alone settles the answer use it, which pins "decided
// before sel is asked".
func neverSelect(t *testing.T) Selector {
	t.Helper()
	return func(u *url.URL) (*url.URL, error) {
		t.Errorf("the selector was consulted for %v, it must not be", u)
		return nil, nil
	}
}

// selectProxy is a Selector that always picks the proxy parsed from raw.
func selectProxy(t *testing.T, raw string) Selector {
	t.Helper()
	return fixed(mustURL(t, raw))
}

func TestDescribe(t *testing.T) {
	const proxyWithSecrets = "http://" + secretUser + ":" + secretPass + "@proxy:3128"

	tests := []struct {
		name     string
		target   string
		env      env
		sel      func(t *testing.T) Selector
		wantKind DecisionKind
		wantVar  string
		wantDisp string
	}{
		{
			name:   "HTTPS_PROXY carries an https target",
			target: "https://example.com:443",
			env:    env{"HTTPS_PROXY": proxyWithSecrets},
			sel: func(t *testing.T) Selector {
				return selectProxy(t, proxyWithSecrets)
			},
			wantKind: DecisionProxied, wantVar: "HTTPS_PROXY", wantDisp: "http://proxy:3128",
		},
		{
			name:   "HTTP_PROXY carries an http target",
			target: "http://example.com:80",
			env:    env{"HTTP_PROXY": proxyWithSecrets},
			sel: func(t *testing.T) Selector {
				return selectProxy(t, proxyWithSecrets)
			},
			wantKind: DecisionProxied, wantVar: "HTTP_PROXY", wantDisp: "http://proxy:3128",
		},
		{
			name:     "HTTP_PROXY alone says nothing about an https target",
			target:   "https://example.com:443",
			env:      env{"HTTP_PROXY": proxyWithSecrets},
			sel:      neverSelect,
			wantKind: DecisionNone,
		},
		{
			name:     "HTTPS_PROXY alone says nothing about an http target",
			target:   "http://example.com:80",
			env:      env{"HTTPS_PROXY": proxyWithSecrets},
			sel:      neverSelect,
			wantKind: DecisionNone,
		},
		{
			name:     "no variable at all",
			target:   "https://example.com:443",
			env:      env{},
			sel:      neverSelect,
			wantKind: DecisionNone,
		},
		{
			name:     "NO_PROXY alone is not a proxy",
			target:   "https://example.com:443",
			env:      env{"NO_PROXY": "example.com", "no_proxy": "example.com"},
			sel:      neverSelect,
			wantKind: DecisionNone,
		},
		{
			name:     "a scheme that has no variable",
			target:   "ftp://example.com:21",
			env:      env{"HTTPS_PROXY": proxyWithSecrets, "HTTP_PROXY": proxyWithSecrets},
			sel:      neverSelect,
			wantKind: DecisionNone,
		},
		{
			name:     "an empty variable counts as unset",
			target:   "https://example.com:443",
			env:      env{"HTTPS_PROXY": "", "https_proxy": ""},
			sel:      neverSelect,
			wantKind: DecisionNone,
		},
		{
			name:   "lower case variable only reports its own name",
			target: "https://example.com:443",
			env:    env{"https_proxy": proxyWithSecrets},
			sel: func(t *testing.T) Selector {
				return selectProxy(t, proxyWithSecrets)
			},
			wantKind: DecisionProxied, wantVar: "https_proxy", wantDisp: "http://proxy:3128",
		},
		{
			name:   "lower case http variable",
			target: "http://example.com:80",
			env:    env{"http_proxy": proxyWithSecrets},
			sel: func(t *testing.T) Selector {
				return selectProxy(t, proxyWithSecrets)
			},
			wantKind: DecisionProxied, wantVar: "http_proxy", wantDisp: "http://proxy:3128",
		},
		{
			name:   "upper case wins over lower case, like getEnvAny",
			target: "https://example.com:443",
			env:    env{"HTTPS_PROXY": proxyWithSecrets, "https_proxy": "http://other:1"},
			sel: func(t *testing.T) Selector {
				return selectProxy(t, proxyWithSecrets)
			},
			wantKind: DecisionProxied, wantVar: "HTTPS_PROXY", wantDisp: "http://proxy:3128",
		},
		{
			name:   "an empty upper case variable falls through to the lower case one",
			target: "https://example.com:443",
			env:    env{"HTTPS_PROXY": "", "https_proxy": proxyWithSecrets},
			sel: func(t *testing.T) Selector {
				return selectProxy(t, proxyWithSecrets)
			},
			wantKind: DecisionProxied, wantVar: "https_proxy", wantDisp: "http://proxy:3128",
		},
		{
			name:   "a bare host:port is a valid proxy value, as in Go",
			target: "https://example.com:443",
			env:    env{"HTTPS_PROXY": "proxy.corp:3128"},
			sel: func(t *testing.T) Selector {
				return selectProxy(t, "http://proxy.corp:3128")
			},
			wantKind: DecisionProxied, wantVar: "HTTPS_PROXY", wantDisp: "http://proxy.corp:3128",
		},
		{
			name:   "a socks5 proxy",
			target: "https://example.com:443",
			env:    env{"HTTPS_PROXY": "socks5://" + secretUser + ":" + secretPass + "@gw:1080"},
			sel: func(t *testing.T) Selector {
				return selectProxy(t, "socks5://"+secretUser+":"+secretPass+"@gw:1080")
			},
			wantKind: DecisionProxied, wantVar: "HTTPS_PROXY", wantDisp: "socks5://gw:1080",
		},
		{
			name:     "valid variable, selector says direct, ordinary host: excluded",
			target:   "https://intranet.example:443",
			env:      env{"HTTPS_PROXY": proxyWithSecrets, "NO_PROXY": "intranet.example"},
			sel:      func(*testing.T) Selector { return direct },
			wantKind: DecisionExcluded, wantVar: "HTTPS_PROXY",
		},
		{
			name:     "excluded keeps the lower case name",
			target:   "https://intranet.example:443",
			env:      env{"https_proxy": proxyWithSecrets},
			sel:      func(*testing.T) Selector { return direct },
			wantKind: DecisionExcluded, wantVar: "https_proxy",
		},
		{
			name:     "localhost is loopback",
			target:   "https://localhost:443",
			env:      env{"HTTPS_PROXY": proxyWithSecrets},
			sel:      func(*testing.T) Selector { return direct },
			wantKind: DecisionLoopback, wantVar: "HTTPS_PROXY",
		},
		{
			name:     "127.0.0.1 is loopback",
			target:   "https://127.0.0.1:443",
			env:      env{"HTTPS_PROXY": proxyWithSecrets},
			sel:      func(*testing.T) Selector { return direct },
			wantKind: DecisionLoopback, wantVar: "HTTPS_PROXY",
		},
		{
			name:     "the whole 127/8 block is loopback",
			target:   "https://127.0.0.2:443",
			env:      env{"HTTPS_PROXY": proxyWithSecrets},
			sel:      func(*testing.T) Selector { return direct },
			wantKind: DecisionLoopback, wantVar: "HTTPS_PROXY",
		},
		{
			name:     "[::1] is loopback",
			target:   "https://[::1]:443",
			env:      env{"HTTPS_PROXY": proxyWithSecrets},
			sel:      func(*testing.T) Selector { return direct },
			wantKind: DecisionLoopback, wantVar: "HTTPS_PROXY",
		},
		{
			name:     "loopback over http",
			target:   "http://localhost:8080",
			env:      env{"HTTP_PROXY": proxyWithSecrets},
			sel:      func(*testing.T) Selector { return direct },
			wantKind: DecisionLoopback, wantVar: "HTTP_PROXY",
		},
		{
			name:     "a non-loopback private address is excluded, not loopback",
			target:   "https://10.0.0.5:443",
			env:      env{"HTTPS_PROXY": proxyWithSecrets},
			sel:      func(*testing.T) Selector { return direct },
			wantKind: DecisionExcluded, wantVar: "HTTPS_PROXY",
		},
		{
			name:     "a non numeric port next to credentials",
			target:   "https://example.com:443",
			env:      env{"HTTPS_PROXY": secretUser + ":" + secretPass + "@proxy:abc"},
			sel:      neverSelect,
			wantKind: DecisionInvalid, wantVar: "HTTPS_PROXY",
		},
		{
			name:     "an explicit scheme with a broken URL: unterminated IPv6 literal",
			target:   "https://example.com:443",
			env:      env{"HTTPS_PROXY": "http://u:p@[::1"},
			sel:      neverSelect,
			wantKind: DecisionInvalid, wantVar: "HTTPS_PROXY",
		},
		{
			name:     "the same broken URL carrying credentials",
			target:   "https://example.com:443",
			env:      env{"HTTPS_PROXY": "http://" + secretUser + ":" + secretPass + "@[::1"},
			sel:      neverSelect,
			wantKind: DecisionInvalid, wantVar: "HTTPS_PROXY",
		},
		{
			name:     "a scheme with an empty host",
			target:   "https://example.com:443",
			env:      env{"HTTPS_PROXY": "http://"},
			sel:      neverSelect,
			wantKind: DecisionInvalid, wantVar: "HTTPS_PROXY",
		},
		{
			name:     "an invalid upper case variable is read before a valid lower case one",
			target:   "https://example.com:443",
			env:      env{"HTTPS_PROXY": "proxy.corp:abc", "https_proxy": proxyWithSecrets},
			sel:      neverSelect,
			wantKind: DecisionInvalid, wantVar: "HTTPS_PROXY",
		},
		{
			name:     "non numeric port in the variable",
			target:   "https://example.com:443",
			env:      env{"HTTPS_PROXY": "proxy.corp:abc"},
			sel:      neverSelect,
			wantKind: DecisionInvalid, wantVar: "HTTPS_PROXY",
		},
		{
			name:     "invalid lower case variable keeps its own name",
			target:   "http://example.com:80",
			env:      env{"http_proxy": secretUser + ":" + secretPass + "@proxy:abc"},
			sel:      neverSelect,
			wantKind: DecisionInvalid, wantVar: "http_proxy",
		},
		{
			name:     "invalid beats a loopback target",
			target:   "https://localhost:443",
			env:      env{"HTTPS_PROXY": "proxy.corp:abc"},
			sel:      neverSelect,
			wantKind: DecisionInvalid, wantVar: "HTTPS_PROXY",
		},
		{
			name:   "a selector error is invalid, and its text is dropped",
			target: "https://example.com:443",
			env:    env{"HTTPS_PROXY": proxyWithSecrets},
			sel: func(*testing.T) Selector {
				return func(*url.URL) (*url.URL, error) {
					return nil, errors.New("proxyconnect tcp: dial " + secretUser + ":" + secretPass + "@proxy:3128 failed")
				}
			},
			wantKind: DecisionInvalid, wantVar: "HTTPS_PROXY",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := describe(mustURL(t, tt.target), tt.env.get, tt.sel(t))
			if got.Kind != tt.wantKind {
				t.Fatalf("Kind = %s, want %s (%+v)", kindName(got.Kind), kindName(tt.wantKind), got)
			}
			if got.Variable != tt.wantVar {
				t.Errorf("Variable = %q, want %q", got.Variable, tt.wantVar)
			}
			if got.Proxy != tt.wantDisp {
				t.Errorf("Proxy = %q, want %q", got.Proxy, tt.wantDisp)
			}
			// The leak guard: nothing a Decision prints may carry a credential.
			for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
				out := fmt.Sprintf(format, got)
				for _, token := range []string{secretUser, secretPass} {
					if strings.Contains(out, token) {
						t.Errorf("Sprintf(%q, decision) = %q leaks %q", format, out, token)
					}
				}
			}
			if strings.ContainsAny(got.Proxy, "@?#") {
				t.Errorf("Proxy = %q carries userinfo, query or fragment", got.Proxy)
			}
		})
	}
}

// TestDescribeConsultsTheSelectorWithTheTarget pins that the answer comes from
// the selector given the target URL as is: Describe must not recompute the
// NO_PROXY decision on its own.
func TestDescribeConsultsTheSelectorWithTheTarget(t *testing.T) {
	var seen []*url.URL
	sel := Selector(func(u *url.URL) (*url.URL, error) {
		seen = append(seen, u)
		return nil, nil
	})
	target := mustURL(t, "https://intranet.example:8443")
	got := describe(target, env{"HTTPS_PROXY": "http://proxy:3128"}.get, sel)
	if got.Kind != DecisionExcluded {
		t.Fatalf("Kind = %s, want Excluded", kindName(got.Kind))
	}
	if len(seen) != 1 || seen[0].String() != target.String() {
		t.Fatalf("selector saw %v, want exactly [%v]", seen, target)
	}
}

// TestDescribeRefusesAMangledCredential pins that a value holding an "@" whose
// parsed form has no userinfo is Invalid: the credential was written in a way
// url.Parse does not read as userinfo (a "/", "?", "#" or an unescaped
// character before the "@"), so it landed in the host, the path, the query or
// the fragment, where it could reach the screen. The decision is made before
// the selector is asked, and nothing of the value may be in the Decision.
func TestDescribeRefusesAMangledCredential(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		fragments []string // pieces of the value that must never show
	}{
		{"a slash before the at sign", "http://user:1234/secret@proxy:3128", []string{"user", "1234", "secret"}},
		{"a plus in the token", "http://ghp_AbC+def/ghi@proxy:3128", []string{"ghp_AbC", "def", "ghi"}},
		{"a question mark, no scheme", "alice:9999?x@proxy", []string{"alice", "9999"}},
		{"a hash before the at sign", "http://tok#en@proxy", []string{"tok"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := describe(mustURL(t, "https://example.com:443"), env{"HTTPS_PROXY": tt.value}.get, neverSelect(t))
			if got.Kind != DecisionInvalid || got.Variable != "HTTPS_PROXY" || got.Proxy != "" {
				t.Fatalf("decision = %+v, want Invalid, HTTPS_PROXY and no proxy", got)
			}
			for _, format := range []string{"%v", "%+v", "%#v"} {
				out := fmt.Sprintf(format, got)
				for _, frag := range tt.fragments {
					if strings.Contains(out, frag) {
						t.Errorf("Sprintf(%q, decision) = %q leaks %q", format, out, frag)
					}
				}
			}
		})
	}
}

// TestDescribeKeepsAnEncodedCredential is the other side of the rule above: a
// credential url.Parse does read as userinfo stays a usable proxy.
func TestDescribeKeepsAnEncodedCredential(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"an escaped at sign in the password", "http://u:p%40x@proxy:3128"},
		{"credentials without a scheme", "u:p@proxy:3128"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := describe(mustURL(t, "https://example.com:443"), env{"HTTPS_PROXY": tt.value}.get,
				selectProxy(t, "http://proxy:3128"))
			if got.Kind != DecisionProxied || got.Proxy != "http://proxy:3128" {
				t.Fatalf("decision = %+v, want Proxied http://proxy:3128", got)
			}
		})
	}
}

// TestDescribeRefusesAHostThatIsNotPrintable pins that a proxy host made of
// anything but printable runes (a C1 control, a bidi override, a byte that is
// not UTF-8) is Invalid, so it never reaches the screen.
func TestDescribeRefusesAHostThatIsNotPrintable(t *testing.T) {
	const creds = secretUser + ":" + secretPass + "@"
	tests := []struct {
		name  string
		value string
	}{
		{"a lone C1 byte", "http://" + creds + "prox%9by:3128"},
		{"a C1 control rune", "http://" + creds + "prox%c2%9by:3128"},
		{"a right-to-left override", "http://" + creds + "prox%e2%80%aey:3128"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := describe(mustURL(t, "https://example.com:443"), env{"HTTPS_PROXY": tt.value}.get, neverSelect(t))
			if got.Kind != DecisionInvalid || got.Variable != "HTTPS_PROXY" || got.Proxy != "" {
				t.Fatalf("decision = %+v, want Invalid, HTTPS_PROXY and no proxy", got)
			}
			out := fmt.Sprintf("%+v", got)
			for _, token := range []string{secretUser, secretPass} {
				if strings.Contains(out, token) {
					t.Errorf("%q leaks %q", out, token)
				}
			}
		})
	}
}

// TestDescribeAcceptsAPrintableNonASCIIHost is the other side: a letter outside
// ASCII is printable.
func TestDescribeAcceptsAPrintableNonASCIIHost(t *testing.T) {
	got := describe(mustURL(t, "https://example.com:443"),
		env{"HTTPS_PROXY": "http://" + secretUser + ":" + secretPass + "@bü.example:3128"}.get,
		selectProxy(t, "http://"+secretUser+":"+secretPass+"@bü.example:3128"))
	if got.Kind != DecisionProxied || got.Proxy != "http://bü.example:3128" {
		t.Fatalf("decision = %+v, want Proxied http://bü.example:3128", got)
	}
}

// TestDescribeDoesNotTakeAnURLInThePathForAnExplicitScheme pins that "://"
// somewhere after the start does not make a value explicit: only a leading
// scheme does. This value is a host:port with a path and a query, a usable
// proxy for Go.
func TestDescribeDoesNotTakeAnURLInThePathForAnExplicitScheme(t *testing.T) {
	got := describe(mustURL(t, "https://example.com:443"),
		env{"HTTPS_PROXY": "proxy.corp:3128/p?u=http://x"}.get, selectProxy(t, "http://proxy.corp:3128"))
	if got.Kind != DecisionProxied || got.Proxy != "http://proxy.corp:3128" {
		t.Fatalf("decision = %+v, want Proxied http://proxy.corp:3128", got)
	}
}

// TestDescribeNeverShowsAnIPv6Zone pins that the zone of a link-local proxy
// address, which can be any text, is not part of what is displayed.
func TestDescribeNeverShowsAnIPv6Zone(t *testing.T) {
	const raw = "http://" + secretUser + ":" + secretPass + "@[fe80::1%25secret]:3128"
	got := describe(mustURL(t, "https://example.com:443"), env{"HTTPS_PROXY": raw}.get, selectProxy(t, raw))
	if got.Kind != DecisionProxied || got.Proxy != "http://[fe80::1]:3128" {
		t.Fatalf("decision = %+v, want Proxied http://[fe80::1]:3128", got)
	}
}

// TestDescribeDoesNotNeedASelectorToDecideNoneOrInvalid pins that a nil
// Selector is never called (so never dereferenced) when the variable alone
// settles the answer.
func TestDescribeDoesNotNeedASelectorToDecideNoneOrInvalid(t *testing.T) {
	tests := []struct {
		name   string
		target string
		env    env
		want   DecisionKind
	}{
		{"no variable", "https://example.com:443", env{}, DecisionNone},
		{"another scheme", "ftp://example.com:21", env{"HTTPS_PROXY": "http://proxy:3128"}, DecisionNone},
		{"an invalid value", "https://example.com:443", env{"HTTPS_PROXY": "proxy.corp:abc"}, DecisionInvalid},
		{"a broken URL", "https://example.com:443", env{"HTTPS_PROXY": "http://u:p@[::1"}, DecisionInvalid},
		{"an empty host", "https://example.com:443", env{"HTTPS_PROXY": "http://"}, DecisionInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := describe(mustURL(t, tt.target), tt.env.get, nil)
			if got.Kind != tt.want {
				t.Fatalf("Kind = %s, want %s", kindName(got.Kind), kindName(tt.want))
			}
		})
	}
}

// TestDecisionZeroValueIsNone pins that an unfilled Decision reads as "no
// proxy", not as a proxy or an error.
func TestDecisionZeroValueIsNone(t *testing.T) {
	var d Decision
	if d.Kind != DecisionNone || d.Variable != "" || d.Proxy != "" {
		t.Fatalf("zero Decision = %+v, want None with no variable and no proxy", d)
	}
}

func TestDisplay(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"userinfo, path, query and fragment are dropped", "http://" + secretUser + ":" + secretPass + "@p:3128/x?token=" + secretPass + "#f", "http://p:3128"},
		{"user without password", "http://" + secretUser + "@p:3128", "http://p:3128"},
		{"https default port", "https://p", "https://p:443"},
		{"http default port", "http://p", "http://p:80"},
		{"socks5 default port", "socks5://p", "socks5://p:1080"},
		{"socks5h default port", "socks5h://p", "socks5h://p:1080"},
		{"an explicit port is kept", "http://p:8080", "http://p:8080"},
		{"an explicit port equal to the default is kept", "https://p:443", "https://p:443"},
		{"IPv6 with a port", "http://[2001:db8::1]:8080", "http://[2001:db8::1]:8080"},
		{"IPv6 gets the default port, brackets kept", "https://[2001:db8::1]", "https://[2001:db8::1]:443"},
		{"an IPv6 zone is dropped", "http://" + secretUser + ":" + secretPass + "@[fe80::1%25secret]:3128", "http://[fe80::1]:3128"},
		{"userinfo on a socks5 proxy", "socks5://" + secretUser + ":" + secretPass + "@gw", "socks5://gw:1080"},
		// No default port is known for another scheme: scheme and host only.
		{"an unknown scheme has no default port", "foo://h", "foo://h"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Display(mustURL(t, tt.in))
			if got != tt.want {
				t.Fatalf("Display(%q) = %q, want %q", tt.in, got, tt.want)
			}
			for _, token := range []string{secretUser, secretPass} {
				if strings.Contains(got, token) {
					t.Fatalf("Display(%q) = %q leaks %q", tt.in, got, token)
				}
			}
		})
	}
}

// TestDisplayNil pins that no proxy displays as nothing, rather than panicking.
func TestDisplayNil(t *testing.T) {
	if got := Display(nil); got != "" {
		t.Fatalf("Display(nil) = %q, want empty", got)
	}
}

// TestDescribeHelper is the child half of TestDescribeAgainstProxyFromEnvironment.
// It does nothing in a normal run. http.ProxyFromEnvironment reads the proxy
// variables once per process, so the parent runs the test binary again with
// the variables it wants and the child asks the real Describe.
func TestDescribeHelper(t *testing.T) {
	if os.Getenv(describeHelperEnv) != "1" {
		t.Skip("child of TestDescribeAgainstProxyFromEnvironment")
	}
	d := Describe(mustURL(t, os.Getenv(helperTarget)))
	fmt.Printf("%skind=%s variable=%s proxy=%s\n", resultPrefix, kindName(d.Kind), d.Variable, d.Proxy)
}

// TestDescribeAgainstProxyFromEnvironment fixes Describe's deduction (nil from
// the selector means loopback or excluded) against the real
// http.ProxyFromEnvironment of the toolchain, so a future Go release that
// changes what it answers breaks here and not on a user's screen.
func TestDescribeAgainstProxyFromEnvironment(t *testing.T) {
	secretProxy := "http://" + secretUser + ":" + secretPass + "@proxy.test:3128"
	tests := []struct {
		name   string
		target string
		vars   map[string]string
		want   string
	}{
		{"a proxied host", "https://other.test:443",
			map[string]string{"HTTPS_PROXY": secretProxy, "NO_PROXY": "intranet.test"},
			"kind=Proxied variable=HTTPS_PROXY proxy=http://proxy.test:3128"},
		{"an excluded host", "https://intranet.test:443",
			map[string]string{"HTTPS_PROXY": secretProxy, "NO_PROXY": "intranet.test"},
			"kind=Excluded variable=HTTPS_PROXY proxy="},
		{"a subdomain of an excluded host", "https://api.intranet.test:443",
			map[string]string{"HTTPS_PROXY": secretProxy, "NO_PROXY": "intranet.test"},
			"kind=Excluded variable=HTTPS_PROXY proxy="},
		{"localhost", "https://localhost:443",
			map[string]string{"HTTPS_PROXY": secretProxy, "NO_PROXY": "intranet.test"},
			"kind=Loopback variable=HTTPS_PROXY proxy="},
		{"127.0.0.1", "https://127.0.0.1:443",
			map[string]string{"HTTPS_PROXY": secretProxy},
			"kind=Loopback variable=HTTPS_PROXY proxy="},
		{"[::1]", "https://[::1]:443",
			map[string]string{"HTTPS_PROXY": secretProxy},
			"kind=Loopback variable=HTTPS_PROXY proxy="},
		{"lower case variable", "https://other.test:443",
			map[string]string{"https_proxy": secretProxy},
			"kind=Proxied variable=https_proxy proxy=http://proxy.test:3128"},
		{"http target with HTTP_PROXY", "http://other.test:80",
			map[string]string{"HTTP_PROXY": secretProxy},
			"kind=Proxied variable=HTTP_PROXY proxy=http://proxy.test:3128"},
		{"http target with only HTTPS_PROXY", "http://other.test:80",
			map[string]string{"HTTPS_PROXY": secretProxy},
			"kind=None variable= proxy="},
		{"no variable", "https://other.test:443", nil,
			"kind=None variable= proxy="},
		// Only a value Go rejects too is compared here. "http://u:p@[::1" is
		// Invalid for Describe by design but is not compared with Go, which
		// wrongly accepts it as the proxy http://http:80 (its retry with an
		// "http://" prefix parses).
		{"an invalid value", "https://other.test:443",
			map[string]string{"HTTPS_PROXY": "proxy.corp:abc"},
			"kind=Invalid variable=HTTPS_PROXY proxy="},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runChild(t, "TestDescribeHelper",
				map[string]string{describeHelperEnv: "1", helperTarget: tt.target}, tt.vars)
			if got != tt.want {
				t.Fatalf("Describe(%s) = %q, want %q", tt.target, got, tt.want)
			}
			for _, token := range []string{secretUser, secretPass} {
				if strings.Contains(got, token) {
					t.Fatalf("Describe(%s) = %q leaks %q", tt.target, got, token)
				}
			}
		})
	}
}
