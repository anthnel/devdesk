package scan

import "testing"

func TestFixCommandPerEcosystem(t *testing.T) {
	tests := []struct {
		ecosystem, pkg, version string
		want                    string
		ok                      bool
	}{
		{"gomod", "golang.org/x/net", "0.17.0", "go get golang.org/x/net@v0.17.0", true},
		{"gomod", "golang.org/x/net", "v0.17.0", "go get golang.org/x/net@v0.17.0", true},
		{"gobinary", "golang.org/x/net", "0.17.0", "go get golang.org/x/net@v0.17.0", true},
		{"npm", "lodash", "4.17.21", "npm install lodash@4.17.21", true},
		{"yarn", "lodash", "4.17.21", "yarn add lodash@4.17.21", true},
		{"pnpm", "lodash", "4.17.21", "pnpm add lodash@4.17.21", true},
		{"pip", "requests", "2.31.0", "pip install requests==2.31.0", true},
		{"poetry", "requests", "2.31.0", "poetry add requests==2.31.0", true},
		{"cargo", "tokio", "1.32.0", "cargo update -p tokio --precise 1.32.0", true},
		{"alpine", "openssl", "3.1.4-r0", "apk upgrade openssl", true},
		{"debian", "openssl", "3.0.11-1", "apt-get install --only-upgrade openssl", true},
		{"ubuntu", "openssl", "3.0.2-0ubuntu1.12", "apt-get install --only-upgrade openssl", true},
		{"jar", "log4j-core", "2.17.1", "", false},
		{"", "libfoo", "1.2.3", "", false},
		{"npm", "lodash", "", "", false},
		{"npm", "", "1.0.0", "", false},
	}
	for _, tc := range tests {
		got, ok := FixCommand(tc.ecosystem, tc.pkg, tc.version)
		if got != tc.want || ok != tc.ok {
			t.Errorf("FixCommand(%q, %q, %q) = %q, %v; want %q, %v",
				tc.ecosystem, tc.pkg, tc.version, got, ok, tc.want, tc.ok)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
		ok   bool
	}{
		{"1.2.3", "1.2.3", 0, true},
		{"1.2.3", "1.2.4", -1, true},
		{"1.10.0", "1.9.0", 1, true},
		{"1.2", "1.2.0", 0, true},
		{"v1.2.3", "1.2.3", 0, true},
		{"1.2.3-rc1", "1.2.3", -1, true},
		{"3.0.11-r0", "3.0.9-r0", 1, true},
		{"1:1.1.1n", "1.1.1", 0, false},
		// A letter right after the digits is an order (OpenSSL style), not a
		// pre-release: 1.1.1l < 1.1.1w, and the bare 1.1.1 is below both.
		{"1.1.1l", "1.1.1w", -1, true},
		{"1.1.1w", "1.1.1l", 1, true},
		{"1.1.1w", "1.1.1w", 0, true},
		{"1.1.1", "1.1.1a", -1, true},
		{"1.1.1w-r0", "1.1.1l-r0", 1, true},
		{"1.1.1l-r0", "1.1.1w", -1, true},
		{"1.1.2", "1.1.1w", 1, true},
		{"1.1.1w", "1.1.2", -1, true},
		// A word after the digits is not a letter suffix: neither moves.
		{"1.2.3rc1", "1.2.3rc2", 0, true},
		{"1.2.3a1", "1.2.3b1", 0, true},
		{"abc", "1.0.0", 0, false},
		{"", "1.0.0", 0, false},
	}
	for _, tc := range tests {
		got, ok := CompareVersions(tc.a, tc.b)
		if got != tc.want || ok != tc.ok {
			t.Errorf("CompareVersions(%q, %q) = %d, %v; want %d, %v", tc.a, tc.b, got, ok, tc.want, tc.ok)
		}
	}
}

func TestPickFixed(t *testing.T) {
	tests := []struct {
		name, installed, fixedIn, want string
	}{
		{"empty", "1.0.0", "", ""},
		{"single", "1.0.0", "1.0.1", "1.0.1"},
		{"single not orderable", "1.0.0", "1:1.1.1n-0", "1:1.1.1n-0"},
		{"same major wins over lower other major", "2.1.0", "1.9.9, 2.1.4, 3.0.1", "2.1.4"},
		{"lowest on the same major", "2.1.0", "2.3.0, 2.1.4", "2.1.4"},
		{"no branch on the same major: lowest overall", "1.0.0", "2.5.0, 3.0.0", "2.5.0"},
		{"unorderable options", "1.0.0", "abc, 1.0.1", ""},
		{"unknown installed version", "", "1.0.1, 2.0.0", "1.0.1"},
		{"spaces and empty entries", "1.0.0", " 1.0.2 , ,1.1.0", "1.0.2"},
		{"letter suffixes are ordered", "1.1.1k", "1.1.1w, 1.1.1l", "1.1.1l"},
	}
	for _, tc := range tests {
		if got := PickFixed(tc.installed, tc.fixedIn); got != tc.want {
			t.Errorf("%s: PickFixed(%q, %q) = %q, want %q", tc.name, tc.installed, tc.fixedIn, got, tc.want)
		}
	}
}

// DEVDESK-754, review: a lone letter after the digits is a release letter for
// OpenSSL and an alpha/beta pre-release for PEP 440 ("2.0b" < "2.0"), so the
// Python ecosystems do not read it as an increasing order.
func TestCompareVersionsInReadsALoneLetterByEcosystem(t *testing.T) {
	tests := []struct {
		eco, a, b string
		want      int
		ok        bool
	}{
		{"alpine", "1.1.1l", "1.1.1w", -1, true},
		{"debian", "1.1.1l", "1.1.1w", -1, true},
		{"", "1.1.1l", "1.1.1w", -1, true},
		{"npm", "1.1.1", "1.1.1a", -1, true},
		{"pip", "2.0", "2.0b", 0, false},
		{"pip", "2.0b", "2.0", 0, false},
		{"pipenv", "2.0a", "2.0b", 0, false},
		{"poetry", "2.0b", "2.0", 0, false},
		{"python-pkg", "2.0b", "2.0", 0, false},
		// What does not use a lone letter is unchanged.
		{"pip", "2.0.1", "2.0", 1, true},
		{"pip", "2.0rc1", "2.0rc2", 0, true},
		{"pip", "2.0-1", "2.0", -1, true},
	}
	for _, tc := range tests {
		got, ok := CompareVersionsIn(tc.eco, tc.a, tc.b)
		if got != tc.want || ok != tc.ok {
			t.Errorf("CompareVersionsIn(%q, %q, %q) = %d, %v; want %d, %v", tc.eco, tc.a, tc.b, got, ok, tc.want, tc.ok)
		}
	}
}

func TestPickFixedInDoesNotChooseBetweenPythonPreReleaseLetters(t *testing.T) {
	if got := PickFixedIn("alpine", "1.1.1k", "1.1.1w, 1.1.1l"); got != "1.1.1l" {
		t.Errorf("alpine: PickFixedIn = %q, want 1.1.1l", got)
	}
	if got := PickFixedIn("pip", "1.0", "2.0b, 2.0"); got != "" {
		t.Errorf("pip: PickFixedIn = %q, want no choice between 2.0b and 2.0", got)
	}
}
