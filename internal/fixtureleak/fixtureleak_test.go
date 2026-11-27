package fixtureleak_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/fixtureleak"
)

// OC-08b (DEVDESK-942), step 1 of the OC-08 plan: the text detectors that
// internal/nexusfixture used to keep to itself move to internal/fixtureleak, so
// that internal/enginefixture shares them instead of copying them.
//
// The tests below pin down the behaviour nexusfixture has today (its own tests,
// which this step does not touch, are the safety net). The API they expect, in a
// package without any import of "testing" and none of the two fixture packages:
//
//	const DenylistEnv = "DEVDESK_FIXTURE_DENYLIST"
//	func SecretFormat(line string) bool                      // GitHub, GitLab, AWS ids, PEM private key
//	func JWTLike(line string) bool                           // "eyJ" followed by 10 or more base64url characters
//	func URLAuthorities(line string) []string                // http(s) URL authorities, also with JSON-escaped slashes
//	func AllowedAuthority(authority string, hosts []string) bool
//	func LocalAddresses(line string, allowed []netip.Prefix) bool
//	func InternalHostNames(line string) []string             // names in .home.arpa, .lan, .internal, .local
//	func ExactVariants(terms []string) []string              // lower-cased forms of each term
//	func ParseDenylist(data []byte) []string
//	func DenylistSummary(n int) string
//	func ReadDenylist(getenv func(string) string) (terms []string, note string, err error)
//
// None of them returns or logs more than a name of a rule's input class: the
// callers report file, line and rule, never the matched text.

// The values below look like credentials to the secret-format rules under test.
// They are assembled at run time so that no literal of this file does: a
// pre-commit or hosting scanner would otherwise take them for real ones.
var (
	ghToken36  = "ghp" + "_" + strings.Repeat("Zz0", 12) // 36 characters after the prefix
	ghToken35  = "ghp" + "_" + strings.Repeat("Zz0", 11) + "Zz"
	glToken20  = "glpat" + "-" + strings.Repeat("zA9", 6) + "zA"
	glToken19  = "glpat" + "-" + strings.Repeat("zA9", 6) + "z"
	awsKeyID   = "AKIA" + "ZZ12345678901234"
	awsTemp    = "ASIA" + "ZZ12345678901234"
	awsShort   = "AKIA" + "1234"
	awsLower   = "AKIA" + "shortletters"
	jwtPayload = "eyJ" + "hbGciOiJIUzI1NiJ9"
)

func pem(kind string) string {
	if kind != "" {
		kind += " "
	}
	return "-----BEGIN " + kind + "PRIVATE" + " KEY-----"
}

func TestSecretFormat(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		{"GitHub personal token", ghToken36, true},
		{"GitHub token in a JSON value", `"note" : "` + ghToken36 + `"`, true},
		{"GitHub OAuth token", "gho" + "_" + strings.Repeat("Zz0", 12), true},
		{"GitHub server token", "ghs" + "_" + strings.Repeat("Zz0", 12), true},
		{"GitHub token, one character short", ghToken35, false},
		{"GitLab token", glToken20, true},
		{"GitLab token, one character short", glToken19, false},
		{"AWS access key id", awsKeyID, true},
		{"AWS temporary key id", awsTemp, true},
		{"AWS prefix, too short", awsShort, false},
		{"AWS prefix, lower-case tail", awsLower, false},
		{"PEM RSA", pem("RSA"), true},
		{"PEM PKCS8", pem(""), true},
		{"PEM OpenSSH", pem("OPENSSH"), true},
		{"PEM EC", pem("EC"), true},
		{"PEM certificate is not a private key", "-----BEGIN CERTIFICATE-----", false},
		{"prefixes alone", "ghp_short glpat-x", false},
		{"plain text", "Error response from daemon: no such container", false},
		{"empty line", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fixtureleak.SecretFormat(tt.line); got != tt.want {
				t.Errorf("SecretFormat = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestJWTLike(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		{"a JWT header", jwtPayload + ".zz-payload-5", true},
		{"in a quoted value", `"note" : "` + jwtPayload + `"`, true},
		{"in a link header", "<" + jwtPayload + `zz6>; rel="next"`, true},
		{"the prefix with 10 characters after it", "eyJ" + "abcdefghij", true},
		{"the prefix with 9 characters after it", "eyJ" + "abcdefghi", false},
		{"the prefix alone", "eyJ", false},
		{"ordinary text", "no token here", false},
		{"empty line", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fixtureleak.JWTLike(tt.line); got != tt.want {
				t.Errorf("JWTLike = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestURLAuthorities(t *testing.T) {
	tests := []struct {
		name string
		line string
		want []string
	}{
		{"a URL with a port", `"url" : "http://nexus.example:8081/repository/x"`, []string{"nexus.example:8081"}},
		{"https", "see https://reg.example/v2/", []string{"reg.example"}},
		{"upper-case scheme", "HTTPS://Reg.Example/v2/", []string{"Reg.Example"}},
		{"slashes escaped for JSON", `"u" : "https:\/\/a.example\/v2"`, []string{"a.example"}},
		{"user info stays in the authority", "https://u:p@reg.example/", []string{"u:p@reg.example"}},
		{"two URLs on a line", "http://127.0.0.1:5000/a and https://quay.io/b", []string{"127.0.0.1:5000", "quay.io"}},
		{"a query ends the authority", "http://h.example?x=1", []string{"h.example"}},
		{"a comma ends the authority", "http://h.example,http://i.example", []string{"h.example", "i.example"}},
		{"a bare image reference is not a URL", "docker.io/library/alpine:3.20", nil},
		{"a scheme other than http", "ftp://h.example/", nil},
		{"no URL", "nothing", nil},
		{"empty line", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fixtureleak.URLAuthorities(tt.line)
			if !slices.Equal(got, tt.want) {
				t.Errorf("URLAuthorities = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAllowedAuthority(t *testing.T) {
	hosts := []string{"127.0.0.1", "localhost", "index.docker.io", "::1"}
	tests := []struct {
		name      string
		authority string
		want      bool
	}{
		{"listed host", "index.docker.io", true},
		{"listed host with a port", "127.0.0.1:30481", true},
		{"listed host in another case", "LOCALHOST:5000", true},
		{"listed IPv6 host in brackets, with a port", "[::1]:80", true},
		{"unlisted host", "reg.example", false},
		{"unlisted host with a port", "reg.example:5000", false},
		{"a namesake that only starts like a listed host", "index.docker.io.evil.example", false},
		{"user info on a listed host", "u@127.0.0.1", false},
		{"user and password on a listed host", "u:p@127.0.0.1:5000", false},
		{"empty authority", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fixtureleak.AllowedAuthority(tt.authority, hosts); got != tt.want {
				t.Errorf("AllowedAuthority(%q) = %v, want %v", tt.authority, got, tt.want)
			}
		})
	}
	if fixtureleak.AllowedAuthority("127.0.0.1", nil) {
		t.Error("AllowedAuthority with no allowed host accepted an authority")
	}
}

func TestLocalAddresses(t *testing.T) {
	engineNets := []netip.Prefix{netip.MustParsePrefix("10.88.0.0/16")}
	tests := []struct {
		name    string
		line    string
		allowed []netip.Prefix
		want    bool
	}{
		{"10/8", "see 10.20.30.40", nil, true},
		{"172.16/12", "see 172.20.4.2", nil, true},
		{"192.168/16", "192.168.1.10", nil, true},
		{"link-local", "see 169.254.169.254", nil, true},
		{"glued to a letter", "ip10.1.2.3", nil, true},
		{"glued to a letter, 192.168", "host192.168.7.7", nil, true},
		{"CGNAT low end", "100.64.0.1", nil, true},
		{"CGNAT high end", "100.127.255.254", nil, true},
		{"just below CGNAT", "100.63.255.255", nil, false},
		{"just above CGNAT", "100.128.0.1", nil, false},
		{"just above 172.16/12", "172.32.0.1", nil, false},
		{"public address", "8.8.8.8", nil, false},
		{"loopback", "127.0.0.1", nil, false},
		{"IPv6 unique-local", "see fd00::1f", nil, true},
		{"IPv6 unique-local, upper case", "see FD00::2A", nil, true},
		{"IPv6 unique-local, fc", "fc00::1", nil, true},
		{"IPv6 link-local", "see fe80::a1b2:c3d4", nil, true},
		{"a digest that starts like an IPv6 prefix", "sha256:fd00000000000000000000000000000000000000000000000000000000000001", nil, false},
		{"a digest that starts like a link-local prefix", "sha256:fe80000000000000000000000000000000000000000000000000000000000002", nil, false},
		{"a version number", "version 29.10.1", nil, false},
		{"no address", "nothing", nil, false},
		{"empty line", "", nil, false},

		{"an allowed network is not reported", "ip 10.88.0.5", engineNets, false},
		{"an address outside the allowed network is", "ip 10.89.0.5", engineNets, true},
		{"an allowed and a forbidden address on one line", "10.88.0.5 and 192.168.1.1", engineNets, true},
		{"the allowed network does not allow another private range", "192.168.1.10", engineNets, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fixtureleak.LocalAddresses(tt.line, tt.allowed); got != tt.want {
				t.Errorf("LocalAddresses = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInternalHostNames(t *testing.T) {
	tests := []struct {
		name string
		line string
		want []string // lower-cased; each must be among the names found
	}{
		{"home.arpa", "nas01.home.arpa", []string{"nas01.home.arpa"}},
		{"in a JSON value", `"note" : "NAS02.HOME.ARPA"`, []string{"nas02.home.arpa"}},
		{".lan", "gitlab.corp.lan", []string{"gitlab.corp.lan"}},
		{".internal", "vault.zz9.internal", []string{"vault.zz9.internal"}},
		{".local", "printer.local", []string{"printer.local"}},
		{"cluster-local name", "bench-upstream.nexus-bench.svc.cluster.local:5000", []string{"bench-upstream.nexus-bench.svc.cluster.local"}},
		// A URL escape is a separator and the line is also decoded: a match of
		// either reading counts.
		{"a %2D after the zone", "nexus.home.arpa%2Dmirror", []string{"nexus.home.arpa"}},
		{"a %41 after the zone", "x.lan%41", []string{"x.lan"}},
		{"a %2F before the name", "%2Fbench-upstream.nexus-bench.svc.cluster.local", []string{"bench-upstream.nexus-bench.svc.cluster.local"}},
		{"two names", "a.lan and b.internal", []string{"a.lan", "b.internal"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lowerAll(fixtureleak.InternalHostNames(tt.line))
			for _, want := range tt.want {
				if !slices.Contains(got, want) {
					t.Errorf("InternalHostNames = %q, want it to contain %q", got, want)
				}
			}
			for _, name := range got {
				if strings.HasPrefix(name, "2f") || strings.Contains(name, "%") {
					t.Errorf("InternalHostNames returned %q: an escape was glued to the name", name)
				}
			}
		})
	}

	none := []string{
		"",
		"no dot here",
		"docker.io/library/alpine and local-name and lan.example.com",
		"localhost",
		"index.docker.io",
		"lanes.example.org",
	}
	for _, line := range none {
		t.Run("none/"+line, func(t *testing.T) {
			if got := fixtureleak.InternalHostNames(line); len(got) != 0 {
				t.Errorf("InternalHostNames(%q) = %q, want none", line, got)
			}
		})
	}
}

func lowerAll(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = strings.ToLower(n)
	}
	return out
}

func TestExactVariants(t *testing.T) {
	tests := []struct {
		name  string
		terms []string
		want  []string // each must be among the variants
	}{
		{"a slash", []string{"a/b"}, []string{"a/b", `a\/b`, "a%2fb"}},
		{"a space and a slash", []string{"zz sec/ret"}, []string{"zz sec/ret", `zz sec\/ret`, "zz+sec%2fret", "zz%20sec%2fret"}},
		{"a quote", []string{`zz"q`}, []string{`zz"q`, `zz\"q`}},
		{"a backslash", []string{`zz\b`}, []string{`zz\b`, `zz\\b`}},
		{"the case is folded", []string{"ZZ-Name"}, []string{"zz-name"}},
		{"several terms", []string{"one", "two"}, []string{"one", "two"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fixtureleak.ExactVariants(tt.terms)
			for _, want := range tt.want {
				if !slices.Contains(got, want) {
					t.Errorf("ExactVariants(%q) = %q, want it to contain %q", tt.terms, got, want)
				}
			}
			for _, v := range got {
				if v != strings.ToLower(v) {
					t.Errorf("variant %q is not lower-cased: callers compare it with lower-cased text", v)
				}
			}
		})
	}
	for _, terms := range [][]string{nil, {}, {""}, {"", ""}} {
		if got := fixtureleak.ExactVariants(terms); len(got) != 0 {
			t.Errorf("ExactVariants(%q) = %q, want none: an empty term matches nothing", terms, got)
		}
	}
}

func TestParseDenylist(t *testing.T) {
	tests := []struct {
		name  string
		file  string
		terms []string
	}{
		{"empty file", "", nil},
		{"only blank lines", "\n  \n\t\n", nil},
		{"only comments", "# names of the instance\n  # another\n", nil},
		{"comments, blanks and two terms", "# c\n\nzz-name-1\n  zz-name-2  \n", []string{"zz-name-1", "zz-name-2"}},
		{"no final newline", "zz-name-3", []string{"zz-name-3"}},
		{"a # inside a term is kept", "zz#name-4\n", []string{"zz#name-4"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fixtureleak.ParseDenylist([]byte(tt.file)); !slices.Equal(got, tt.terms) {
				t.Errorf("ParseDenylist = %q, want %q", got, tt.terms)
			}
		})
	}
}

func TestDenylistSummaryNeverPassesForAnAppliedList(t *testing.T) {
	if got := fixtureleak.DenylistSummary(0); !strings.Contains(got, "0 terms") || !strings.Contains(got, "NOT applied") {
		t.Errorf("DenylistSummary(0) = %q, want it to say \"0 terms\" and that the list was NOT applied", got)
	}
	for _, n := range []int{1, 2, 17} {
		want := strconv.Itoa(n) + " terms"
		if got := fixtureleak.DenylistSummary(n); !strings.Contains(got, want) {
			t.Errorf("DenylistSummary(%d) = %q, want it to give the count (%q)", n, got, want)
		}
	}
}

func TestDenylistEnvIsTheOneNexusAlreadyUses(t *testing.T) {
	if fixtureleak.DenylistEnv != "DEVDESK_FIXTURE_DENYLIST" {
		t.Errorf("DenylistEnv = %q, want DEVDESK_FIXTURE_DENYLIST", fixtureleak.DenylistEnv)
	}
}

func TestReadDenylist(t *testing.T) {
	write := func(t *testing.T, content string) string {
		t.Helper()
		p := filepath.Join(t.TempDir(), "denylist.txt")
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	env := func(path string) func(string) string {
		return func(key string) string {
			if key == fixtureleak.DenylistEnv {
				return path
			}
			return ""
		}
	}

	t.Run("variable not set", func(t *testing.T) {
		terms, note, err := fixtureleak.ReadDenylist(env(""))
		if err != nil || len(terms) != 0 {
			t.Fatalf("ReadDenylist = %q, %q, %v, want no term and no error", terms, note, err)
		}
		if !strings.Contains(note, "not set") || !strings.Contains(note, fixtureleak.DenylistEnv) {
			t.Errorf("note %q does not say that %s is not set", note, fixtureleak.DenylistEnv)
		}
	})
	t.Run("a file with two terms", func(t *testing.T) {
		terms, note, err := fixtureleak.ReadDenylist(env(write(t, "# c\nzz-one\nzz-two\n")))
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(terms, []string{"zz-one", "zz-two"}) {
			t.Errorf("terms = %q", terms)
		}
		if !strings.Contains(note, "2 terms") {
			t.Errorf("note %q does not give the count", note)
		}
	})
	t.Run("a file without an active term is not silent", func(t *testing.T) {
		terms, note, err := fixtureleak.ReadDenylist(env(write(t, "# only a comment\n\n")))
		if err != nil || len(terms) != 0 {
			t.Fatalf("ReadDenylist = %q, %q, %v", terms, note, err)
		}
		if !strings.Contains(note, "0 terms") {
			t.Errorf("note %q does not say \"0 terms\"", note)
		}
	})
	t.Run("an unreadable file is an error that names the variable", func(t *testing.T) {
		_, _, err := fixtureleak.ReadDenylist(env(filepath.Join(t.TempDir(), "absent.txt")))
		if err == nil || !strings.Contains(err.Error(), fixtureleak.DenylistEnv) {
			t.Errorf("err = %v, want one that names %s", err, fixtureleak.DenylistEnv)
		}
	})
}

// The package is production code shared by two fixture packages: it must not
// import "testing", and it must not import either of them (no cycle, and no
// detector that knows one format).
func TestPackageStaysPlainProductionCode(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var production []string
	for _, f := range files {
		if !strings.HasSuffix(f, "_test.go") {
			production = append(production, f)
		}
	}
	if len(production) == 0 {
		t.Fatal("internal/fixtureleak has no non-test Go file")
	}
	forbidden := []string{
		"testing",
		"github.com/anthnel/devdesk/internal/nexusfixture",
		"github.com/anthnel/devdesk/internal/enginefixture",
	}
	for _, f := range production {
		parsed, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range parsed.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if slices.Contains(forbidden, path) {
				t.Errorf("%s imports %q", f, path)
			}
		}
	}
}

// The detectors live here once. Neither fixture package declares them again:
// a copy would drift (Rule 201). The check reads string literals only, so a
// comment may still name a format.
func TestDetectorsAreDeclaredOnce(t *testing.T) {
	const modulePath = "github.com/anthnel/devdesk/internal/fixtureleak"
	// Fragments of the regular expressions that moved here.
	fragments := []string{"eyJ", "gh[pousr]", "glpat-", `home\.arpa`, "-----BEGIN"}
	for _, file := range []string{
		filepath.Join("..", "nexusfixture", "leaks.go"),
		filepath.Join("..", "enginefixture", "leaks.go"),
	} {
		t.Run(filepath.ToSlash(file), func(t *testing.T) {
			parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
			if err != nil {
				t.Fatalf("the file must exist and parse: %v", err)
			}
			imports := false
			for _, imp := range parsed.Imports {
				if path, _ := strconv.Unquote(imp.Path.Value); path == modulePath {
					imports = true
				}
			}
			if !imports {
				t.Errorf("%s does not import %s", file, modulePath)
			}
			ast.Inspect(parsed, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				for _, frag := range fragments {
					if strings.Contains(lit.Value, frag) {
						t.Errorf("%s declares a detector that fixtureleak owns (a literal holding %q)", file, frag)
					}
				}
				return true
			})
		})
	}
}
