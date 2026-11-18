package netproxy

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
)

const (
	// forwardHelperEnv makes the test binary run TestContainerForwardHelper for real.
	forwardHelperEnv = "NETPROXY_TEST_FORWARD_HELPER"
	// forwardOtherEnv is the value the helper puts in HTTPS_PROXY after the first read.
	forwardOtherEnv = "NETPROXY_TEST_FORWARD_OTHER"
	// forwardSep separates the helper's three readings on its RESULT line.
	forwardSep = " ;; "
)

// lookupOf is an injectable environment: a map, with "" counting as set.
func lookupOf(env map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := env[name]
		return v, ok
	}
}

func forwardOf(env map[string]string) Forward {
	return ForwardFromLookup(lookupOf(env))
}

func TestForwardedVariablesAreTheSixNamesInOrder(t *testing.T) {
	want := []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "NO_PROXY", "no_proxy"}
	if !slices.Equal(ForwardedVariables, want) {
		t.Fatalf("ForwardedVariables = %v, want %v", ForwardedVariables, want)
	}
}

func TestForwardZeroValueIsEmptyAndSafe(t *testing.T) {
	var f Forward
	if !f.Empty() {
		t.Error("the zero Forward is not Empty()")
	}
	if got := f.Names(); len(got) != 0 {
		t.Errorf("Names() = %v, want none", got)
	}
	if got := f.Redact("nothing to hide"); got != "nothing to hide" {
		t.Errorf("Redact changed a text with no secret: %q", got)
	}
	if _, _, ok := f.Loopback(); ok {
		t.Error("the zero Forward reports a loopback proxy")
	}
	_ = f.String() // must not panic
}

func TestForwardNamesAndEnviron(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		names   []string
		environ []string // what Environ(nil) returns, in order
	}{
		{
			name:  "nothing defined",
			env:   map[string]string{},
			names: nil, environ: nil,
		},
		{
			name:    "HTTPS_PROXY alone gives both casings",
			env:     map[string]string{"HTTPS_PROXY": "http://p:3128"},
			names:   []string{"HTTPS_PROXY", "https_proxy"},
			environ: []string{"HTTPS_PROXY=http://p:3128", "https_proxy=http://p:3128"},
		},
		{
			name:    "https_proxy alone gives both casings, with its value",
			env:     map[string]string{"https_proxy": "http://low:3128"},
			names:   []string{"HTTPS_PROXY", "https_proxy"},
			environ: []string{"HTTPS_PROXY=http://low:3128", "https_proxy=http://low:3128"},
		},
		{
			name:    "upper case wins over lower case for both names",
			env:     map[string]string{"HTTPS_PROXY": "http://up:1", "https_proxy": "http://low:2"},
			names:   []string{"HTTPS_PROXY", "https_proxy"},
			environ: []string{"HTTPS_PROXY=http://up:1", "https_proxy=http://up:1"},
		},
		{
			name:    "an empty upper case value is not defined",
			env:     map[string]string{"HTTPS_PROXY": "", "https_proxy": "x"},
			names:   []string{"HTTPS_PROXY", "https_proxy"},
			environ: []string{"HTTPS_PROXY=x", "https_proxy=x"},
		},
		{
			name:  "both empty is nothing",
			env:   map[string]string{"HTTPS_PROXY": "", "https_proxy": ""},
			names: nil, environ: nil,
		},
		{
			name:    "NO_PROXY alone",
			env:     map[string]string{"NO_PROXY": "corp.test"},
			names:   []string{"NO_PROXY", "no_proxy"},
			environ: []string{"NO_PROXY=corp.test", "no_proxy=corp.test"},
		},
		{
			name:    "no_proxy alone",
			env:     map[string]string{"no_proxy": ".corp.test"},
			names:   []string{"NO_PROXY", "no_proxy"},
			environ: []string{"NO_PROXY=.corp.test", "no_proxy=.corp.test"},
		},
		{
			name: "all three pairs, in ForwardedVariables order whatever the map order",
			env: map[string]string{
				"no_proxy": "n", "https_proxy": "https://s:2", "http_proxy": "http://h:1",
			},
			names: []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "NO_PROXY", "no_proxy"},
			environ: []string{
				"HTTP_PROXY=http://h:1", "http_proxy=http://h:1",
				"HTTPS_PROXY=https://s:2", "https_proxy=https://s:2",
				"NO_PROXY=n", "no_proxy=n",
			},
		},
		{
			name: "ALL_PROXY and FTP_PROXY are ignored",
			env: map[string]string{
				"ALL_PROXY": "http://a:1", "all_proxy": "http://a:1",
				"FTP_PROXY": "http://f:1", "ftp_proxy": "http://f:1",
			},
			names: nil, environ: nil,
		},
		{
			name: "ALL_PROXY next to a real variable changes nothing",
			env: map[string]string{
				"ALL_PROXY": "http://a:1", "HTTP_PROXY": "http://h:1",
			},
			names:   []string{"HTTP_PROXY", "http_proxy"},
			environ: []string{"HTTP_PROXY=http://h:1", "http_proxy=http://h:1"},
		},
		{
			name:    "the value is passed as is, not validated",
			env:     map[string]string{"HTTPS_PROXY": "not a url at all"},
			names:   []string{"HTTPS_PROXY", "https_proxy"},
			environ: []string{"HTTPS_PROXY=not a url at all", "https_proxy=not a url at all"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := forwardOf(tt.env)
			if got, want := f.Empty(), len(tt.names) == 0; got != want {
				t.Errorf("Empty() = %v, want %v", got, want)
			}
			if got := f.Names(); !slices.Equal(got, tt.names) {
				t.Errorf("Names() = %v, want %v", got, tt.names)
			}
			if got := f.Environ(nil); !slices.Equal(got, tt.environ) {
				t.Errorf("Environ(nil) = %v, want %v", got, tt.environ)
			}
		})
	}
}

func TestForwardEnvironWithNothingToForwardReturnsBase(t *testing.T) {
	base := []string{"PATH=/bin", "HOME=/h"}
	got := forwardOf(nil).Environ(base)
	if !slices.Equal(got, base) {
		t.Errorf("Environ(base) = %v, want %v", got, base)
	}
}

func TestForwardEnvironReplacesTheSixNamesAndKeepsTheRest(t *testing.T) {
	f := forwardOf(map[string]string{
		"HTTPS_PROXY": "http://new:1",
		"NO_PROXY":    "corp.test",
	})
	base := []string{
		"PATH=/bin",
		"HTTP_PROXY=http://stale:9",
		"HTTPS_PROXY=http://old:2",
		"HOME=/h",
		"https_proxy=http://old-low:3",
		"no_proxy=stale",
		"NO_PROXY=stale",
		"http_proxy=http://stale-low:4",
		"ALL_PROXY=http://kept:5",
		"LANG=C",
	}
	snapshot := slices.Clone(base)

	got := f.Environ(base)

	want := []string{
		"PATH=/bin", "HOME=/h", "ALL_PROXY=http://kept:5", "LANG=C",
		"HTTPS_PROXY=http://new:1", "https_proxy=http://new:1",
		"NO_PROXY=corp.test", "no_proxy=corp.test",
	}
	if !slices.Equal(got, want) {
		t.Errorf("Environ(base) =\n  %v\nwant\n  %v", got, want)
	}
	// A stale variable of the parent must not survive: HTTP_PROXY is not
	// forwarded, so the child does not get one either (no duplicates, no leak).
	for _, name := range ForwardedVariables {
		n := 0
		for _, kv := range got {
			if strings.HasPrefix(kv, name+"=") {
				n++
			}
		}
		if n > 1 {
			t.Errorf("%s appears %d times in %v", name, n, got)
		}
	}
	if !slices.Equal(base, snapshot) {
		t.Errorf("Environ modified its argument: %v, was %v", base, snapshot)
	}
}

func TestForwardLoopback(t *testing.T) {
	tests := []struct {
		name         string
		env          map[string]string
		wantOK       bool
		wantVariable string
		wantDisplay  string
	}{
		{"IPv4 with scheme", map[string]string{"HTTPS_PROXY": "http://127.0.0.1:3128"}, true, "HTTPS_PROXY", "http://127.0.0.1:3128"},
		{"localhost without scheme", map[string]string{"HTTPS_PROXY": "localhost:3128"}, true, "HTTPS_PROXY", "http://localhost:3128"},
		{"IPv6 with scheme", map[string]string{"HTTPS_PROXY": "http://[::1]:3128"}, true, "HTTPS_PROXY", "http://[::1]:3128"},
		{"IPv6 without scheme", map[string]string{"HTTPS_PROXY": "[::1]:3128"}, true, "HTTPS_PROXY", "http://[::1]:3128"},
		{"any 127/8 address", map[string]string{"HTTPS_PROXY": "http://127.1.2.3:8080"}, true, "HTTPS_PROXY", "http://127.1.2.3:8080"},
		{"host name in capitals", map[string]string{"HTTPS_PROXY": "http://LOCALHOST:1"}, true, "HTTPS_PROXY", "http://LOCALHOST:1"},
		{"no port shows the scheme default", map[string]string{"HTTPS_PROXY": "http://127.0.0.1"}, true, "HTTPS_PROXY", "http://127.0.0.1:80"},
		{"socks5 scheme", map[string]string{"HTTPS_PROXY": "socks5://localhost:1080"}, true, "HTTPS_PROXY", "socks5://localhost:1080"},
		{
			"credentials are not in the display",
			map[string]string{"HTTPS_PROXY": "http://u:secret@127.0.0.1:3128"},
			true, "HTTPS_PROXY", "http://127.0.0.1:3128",
		},
		{
			"the variable named is the one read: lower case",
			map[string]string{"https_proxy": "http://127.0.0.1:3128"},
			true, "https_proxy", "http://127.0.0.1:3128",
		},
		{
			"an empty upper case falls back to the lower case it read",
			map[string]string{"HTTPS_PROXY": "", "https_proxy": "http://127.0.0.1:3128"},
			true, "https_proxy", "http://127.0.0.1:3128",
		},
		{
			"upper case wins, so a remote upper case hides a loopback lower case",
			map[string]string{"HTTPS_PROXY": "http://proxy.corp:3128", "https_proxy": "http://127.0.0.1:3128"},
			false, "", "",
		},
		{"HTTP_PROXY alone", map[string]string{"HTTP_PROXY": "http://127.0.0.1:3128"}, true, "HTTP_PROXY", "http://127.0.0.1:3128"},
		{"http_proxy alone", map[string]string{"http_proxy": "localhost:8080"}, true, "http_proxy", "http://localhost:8080"},
		{
			"HTTPS is tested before HTTP",
			map[string]string{"HTTP_PROXY": "http://localhost:1", "HTTPS_PROXY": "http://127.0.0.1:2"},
			true, "HTTPS_PROXY", "http://127.0.0.1:2",
		},
		{
			// Simplest reading of the plan: the HTTPS pair first, then the HTTP
			// pair, and the first loopback one wins.
			"a remote HTTPS proxy does not hide a loopback HTTP proxy",
			map[string]string{"HTTPS_PROXY": "http://proxy.corp:3128", "HTTP_PROXY": "http://127.0.0.1:3128"},
			true, "HTTP_PROXY", "http://127.0.0.1:3128",
		},
		{"proxy.localhost is not loopback", map[string]string{"HTTPS_PROXY": "http://proxy.localhost:3128"}, false, "", ""},
		{"a private address is not loopback", map[string]string{"HTTPS_PROXY": "http://10.0.0.1:3128"}, false, "", ""},
		{"a remote host is not loopback", map[string]string{"HTTPS_PROXY": "http://proxy.corp:3128"}, false, "", ""},
		{"an invalid value is not loopback", map[string]string{"HTTPS_PROXY": "http://"}, false, "", ""},
		{"a mangled credential is not loopback", map[string]string{"HTTPS_PROXY": "127.0.0.1:3128/x@y"}, false, "", ""},
		{"NO_PROXY alone is never loopback", map[string]string{"NO_PROXY": "127.0.0.1,localhost"}, false, "", ""},
		{"nothing defined", map[string]string{}, false, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			variable, display, ok := forwardOf(tt.env).Loopback()
			if ok != tt.wantOK {
				t.Fatalf("Loopback() ok = %v, want %v (variable %q, display %q)", ok, tt.wantOK, variable, display)
			}
			if variable != tt.wantVariable || display != tt.wantDisplay {
				t.Errorf("Loopback() = (%q, %q), want (%q, %q)", variable, display, tt.wantVariable, tt.wantDisplay)
			}
			if strings.Contains(display, "secret") || strings.Contains(display, "@") {
				t.Errorf("Loopback() display %q carries credentials", display)
			}
		})
	}
}

func TestForwardRedact(t *testing.T) {
	const (
		raw     = "http://user:s3cr3t%40x@proxy:3128"
		display = "http://proxy:3128"
	)
	f := forwardOf(map[string]string{"HTTPS_PROXY": raw})

	t.Run("the raw value and the password, in every form, are gone", func(t *testing.T) {
		text := "dial " + raw + ": refused; password s3cr3t@x leaked; also s3cr3t%40x; and user:s3cr3t%40x@proxy"
		got := f.Redact(text)
		for _, leak := range []string{raw, "s3cr3t%40x", "s3cr3t@x", "s3cr3t"} {
			if strings.Contains(got, leak) {
				t.Errorf("Redact left %q in %q", leak, got)
			}
		}
		if !strings.Contains(got, display) {
			t.Errorf("Redact did not replace the raw value by its display %q: %q", display, got)
		}
	})

	t.Run("a text with no secret is unchanged", func(t *testing.T) {
		const text = "level=info msg=\"pulling image\" proxy configured"
		if got := f.Redact(text); got != text {
			t.Errorf("Redact(%q) = %q", text, got)
		}
	})

	t.Run("an empty text", func(t *testing.T) {
		if got := f.Redact(""); got != "" {
			t.Errorf("Redact(\"\") = %q", got)
		}
	})

	t.Run("the lower case twin carries the same secret", func(t *testing.T) {
		low := forwardOf(map[string]string{"https_proxy": raw})
		if got := low.Redact("error: " + raw + " s3cr3t%40x"); strings.Contains(got, "s3cr3t") {
			t.Errorf("Redact left the secret in %q", got)
		}
	})

	t.Run("a short password is replaced only in the value and in user:pass@", func(t *testing.T) {
		const shortRaw = "http://u:ab@proxy:3128"
		short := forwardOf(map[string]string{"HTTPS_PROXY": shortRaw})
		text := "cab " + shortRaw + " abc u:ab@host"
		got := short.Redact(text)
		if strings.Contains(got, shortRaw) {
			t.Errorf("Redact left the raw value in %q", got)
		}
		if strings.Contains(got, "u:ab@") {
			t.Errorf("Redact left user:pass@ in %q", got)
		}
		if !strings.Contains(got, "cab") || !strings.Contains(got, "abc") {
			t.Errorf("Redact damaged the free text around a short password: %q", got)
		}
		if !strings.Contains(got, "http://proxy:3128") {
			t.Errorf("Redact did not replace the raw value by its display: %q", got)
		}
	})

	t.Run("a value with no credentials needs no redaction", func(t *testing.T) {
		plain := forwardOf(map[string]string{"HTTPS_PROXY": "http://proxy:3128"})
		const text = "connecting through proxy:3128"
		if got := plain.Redact(text); got != text {
			t.Errorf("Redact(%q) = %q", text, got)
		}
	})
}

// A value with no credentials holds nothing to hide: Redact leaves the way the
// tool wrote it alone, so a diagnostic still names what was dialed.
func TestForwardRedactLeavesACredentialFreeValueAlone(t *testing.T) {
	for _, tc := range []struct{ name, raw, text string }{
		{"no scheme", "proxy.corp:3128", "dial tcp proxy.corp:3128: i/o timeout"},
		{"no port", "http://proxy", "dial tcp proxy:80: connection refused via http://proxy"},
		{"explicit scheme and port", "http://proxy:3128", "proxy http://proxy:3128 refused"},
		{"unparseable, no at sign", "http://[::1", "bad proxy http://[::1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := forwardOf(map[string]string{"HTTPS_PROXY": tc.raw}).Redact(tc.text); got != tc.text {
				t.Errorf("Redact(%q) = %q", tc.text, got)
			}
		})
	}
	t.Run("an unparseable value holding an at sign is still replaced", func(t *testing.T) {
		got := forwardOf(map[string]string{"HTTPS_PROXY": "http://u:s3cr3t@[::1"}).Redact("bad http://u:s3cr3t@[::1")
		if strings.Contains(got, "s3cr3t") || strings.Contains(got, "http://u:") {
			t.Errorf("Redact left the credential in %q", got)
		}
	})
}

// TestForwardNeverPrintsItsValues: the raw proxy, credentials included, must
// not leave through fmt, whatever the verb.
func TestForwardNeverPrintsItsValues(t *testing.T) {
	const (
		password = "s3cr3tpass"
		raw      = "http://user:" + password + "@proxy.corp:3128"
	)
	f := forwardOf(map[string]string{
		"HTTPS_PROXY": raw,
		"HTTP_PROXY":  raw,
		"NO_PROXY":    "corp.test",
	})
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		for label, arg := range map[string]any{"value": f, "pointer": &f} {
			got := fmt.Sprintf(verb, arg)
			if strings.Contains(got, password) || strings.Contains(got, "user:") || strings.Contains(got, raw) {
				t.Errorf("Sprintf(%q, %s) leaks the credentials: %s", verb, label, got)
			}
		}
	}
	if got := f.String(); !strings.Contains(got, "HTTPS_PROXY=http://proxy.corp:3128") {
		t.Errorf("String() = %q, want it to show HTTPS_PROXY=http://proxy.corp:3128", got)
	}
	if got, want := f.GoString(), f.String(); got != want {
		t.Errorf("GoString() = %q, want the same as String() = %q", got, want)
	}
	// One Sprintf with all four verbs, as the plan phrases it.
	if got := fmt.Sprintf("%v %+v %#v %s", f, f, f, f); strings.Contains(got, password) || strings.Contains(got, "user:") {
		t.Errorf("combined Sprintf leaks the credentials: %s", got)
	}
}

// TestContainerForwardHelper is the child of TestContainerForwardReadsOnce. It
// reads ContainerForward, changes HTTPS_PROXY in its own environment, reads
// again, and answers with both readings plus a fresh read of the environment.
func TestContainerForwardHelper(t *testing.T) {
	if os.Getenv(forwardHelperEnv) != "1" {
		t.Skip("child of TestContainerForwardReadsOnce")
	}
	render := func(f Forward) string { return strings.Join(f.Environ(nil), "|") }
	first := render(ContainerForward())
	if err := os.Setenv("HTTPS_PROXY", os.Getenv(forwardOtherEnv)); err != nil {
		t.Fatal(err)
	}
	second := render(ContainerForward())
	fresh := render(ForwardFromLookup(os.LookupEnv))
	fmt.Println(resultPrefix + first + forwardSep + second + forwardSep + fresh)
}

func TestContainerForwardReadsOnce(t *testing.T) {
	const (
		first = "http://first.corp:3128"
		other = "http://other.corp:3128"
	)
	got := runChild(t, "TestContainerForwardHelper",
		map[string]string{forwardHelperEnv: "1", forwardOtherEnv: other},
		map[string]string{"HTTPS_PROXY": first})

	parts := strings.Split(got, forwardSep)
	if len(parts) != 3 {
		t.Fatalf("the helper answered %q, want three readings", got)
	}
	wantFirst := "HTTPS_PROXY=" + first + "|https_proxy=" + first
	if parts[0] != wantFirst {
		t.Errorf("first ContainerForward() = %q, want %q", parts[0], wantFirst)
	}
	if parts[1] != parts[0] {
		t.Errorf("ContainerForward() read the environment again: %q, then %q", parts[0], parts[1])
	}
	// Control: the environment did change, so only the cache explains parts[1].
	wantFresh := "HTTPS_PROXY=" + other + "|https_proxy=" + other
	if parts[2] != wantFresh {
		t.Errorf("control reading = %q, want %q (the helper did not change its environment)", parts[2], wantFresh)
	}
}

// TestForwardNeverPrintsThroughAnUnexportedField: fmt cannot call String on a
// field it may not export, so it walks the value. A Forward kept in a struct
// must not show its raw values that way.
func TestForwardNeverPrintsThroughAnUnexportedField(t *testing.T) {
	const (
		password = "s3cr3tpass"
		raw      = "http://user:" + password + "@proxy.corp:3128"
	)
	f := forwardOf(map[string]string{"HTTPS_PROXY": raw, "HTTP_PROXY": raw})
	type holder struct {
		f Forward
		p *Forward
		s []Forward
		m map[string]Forward
	}
	h := holder{f: f, p: &f, s: []Forward{f}, m: map[string]Forward{"k": f}}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		for label, arg := range map[string]any{"value": h, "pointer": &h} {
			got := fmt.Sprintf(verb, arg)
			if strings.Contains(got, password) || strings.Contains(got, "user:") || strings.Contains(got, raw) {
				t.Errorf("Sprintf(%q, %s) leaks the credentials: %s", verb, label, got)
			}
		}
	}
}

func TestForwardRedactEdgeCases(t *testing.T) {
	redact := func(env map[string]string, text string) string {
		return forwardOf(env).Redact(text)
	}
	mustNotContain := func(t *testing.T, got string, leaks ...string) {
		t.Helper()
		for _, leak := range leaks {
			if strings.Contains(got, leak) {
				t.Errorf("Redact left %q in %q", leak, got)
			}
		}
	}

	t.Run("an empty userinfo removes no at sign", func(t *testing.T) {
		got := redact(map[string]string{"HTTPS_PROXY": "http://@proxy:3128"},
			"mail me@example.com about http://@proxy:3128")
		if !strings.Contains(got, "me@example.com") {
			t.Errorf("Redact damaged the text: %q", got)
		}
		if !strings.Contains(got, "http://proxy:3128") || strings.Contains(got, "http://@proxy") {
			t.Errorf("Redact did not replace the raw value by its display: %q", got)
		}
	})

	t.Run("an empty userinfo before a password-less colon removes no at sign either", func(t *testing.T) {
		got := redact(map[string]string{"HTTPS_PROXY": "http://:@proxy:3128"}, "mail me@example.com")
		if got != "mail me@example.com" {
			t.Errorf("Redact damaged the text: %q", got)
		}
	})

	t.Run("a short user name without password does not eat an address", func(t *testing.T) {
		got := redact(map[string]string{"HTTPS_PROXY": "http://me@proxy:3128"}, "mail me@example.com")
		if got != "mail me@example.com" {
			t.Errorf("Redact damaged the text: %q", got)
		}
	})

	t.Run("escapes other than Go's own are removed", func(t *testing.T) {
		tests := []struct {
			name, raw, text string
			leaks           []string
		}{
			{
				"lower case hex in the password",
				"http://user:pa%2fss1@proxy:3128",
				"a pa%2fss1 b pa%2Fss1 c pa/ss1 d user:pa%2fss1@proxy",
				[]string{"pa%2fss1", "pa%2Fss1", "pa/ss1"},
			},
			{
				"an escaped letter in the user name",
				"http://us%65r:secretpw@proxy:3128",
				"dial us%65r:secretpw@proxy and secretpw and user:secretpw@",
				[]string{"secretpw", "us%65r"},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := redact(map[string]string{"HTTPS_PROXY": tt.raw}, tt.text)
				mustNotContain(t, got, tt.leaks...)
			})
		}
	})

	t.Run("an empty password makes the user name the secret", func(t *testing.T) {
		got := redact(map[string]string{"HTTPS_PROXY": "http://tokenvalue:@proxy:3128"},
			"token tokenvalue; tokenvalue:@proxy; http://tokenvalue:@proxy:3128")
		mustNotContain(t, got, "tokenvalue")
		if !strings.Contains(got, "token ") {
			t.Errorf("Redact damaged the text: %q", got)
		}
	})

	t.Run("an unusable value holding an at sign", func(t *testing.T) {
		tests := []struct {
			name, raw, text string
			leaks           []string
			kept            string
		}{
			{
				"no host",
				"http://user:secretpw@",
				"bad value http://user:secretpw@ then user:secretpw@x and secretpw",
				[]string{"secretpw", "user:"},
				"bad value",
			},
			{
				"an unescaped slash in the password",
				"user:pa/ss@proxy:3128",
				"bad value user:pa/ss@proxy:3128 then pa/ss and user:pa/ss@",
				[]string{"pa/ss", "user:"},
				"bad value",
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := redact(map[string]string{"HTTPS_PROXY": tt.raw}, tt.text)
				mustNotContain(t, got, tt.leaks...)
				if !strings.Contains(got, tt.kept) {
					t.Errorf("Redact damaged the text: %q", got)
				}
			})
		}
	})

	t.Run("two pairs with different credentials", func(t *testing.T) {
		got := redact(map[string]string{
			"HTTP_PROXY":  "http://alice:pwalice1@h1:81",
			"HTTPS_PROXY": "http://bob:pwbob22@h2:82",
		}, "a http://alice:pwalice1@h1:81 b http://bob:pwbob22@h2:82 c pwalice1 d pwbob22 e alice:pwalice1@ f bob:pwbob22@")
		mustNotContain(t, got, "pwalice1", "pwbob22", "alice:", "bob:")
		for _, display := range []string{"http://h1:81", "http://h2:82"} {
			if !strings.Contains(got, display) {
				t.Errorf("Redact did not show %q: %q", display, got)
			}
		}
	})
}

func TestForwardStringHidesAnUnprintableNoProxy(t *testing.T) {
	bad := forwardOf(map[string]string{"NO_PROXY": "corp.test\x1b]0;x\x07"})
	got := bad.String()
	if strings.ContainsAny(got, "\x1b\x07") {
		t.Errorf("String() carries a control character: %q", got)
	}
	if !strings.Contains(got, "NO_PROXY=[redacted]") {
		t.Errorf("String() = %q, want NO_PROXY=[redacted]", got)
	}
	ok := forwardOf(map[string]string{"NO_PROXY": "corp.test,.lan"})
	if got := ok.String(); got != "NO_PROXY=corp.test,.lan" {
		t.Errorf("String() = %q, want the NO_PROXY value as is", got)
	}
}
