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
	}
	for _, tc := range tests {
		if got := PickFixed(tc.installed, tc.fixedIn); got != tc.want {
			t.Errorf("%s: PickFixed(%q, %q) = %q, want %q", tc.name, tc.installed, tc.fixedIn, got, tc.want)
		}
	}
}
