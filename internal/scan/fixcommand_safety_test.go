package scan

import "testing"

// DEVDESK-751: the command a user copies is built from names and versions a
// scan reported, which a hostile package index or image can choose. FixCommand
// builds none from a value that is not a plain name or version, and the entries
// it did build before stay byte for byte as they were (the scan cache holds them).

func TestFixCommandRefusesANameOrVersionThatIsNotPlain(t *testing.T) {
	bad := []string{
		"x; rm -rf /home", "x && id", "x|id", "$(id)", "`id`", "a b", "a\tb", "a\nb", "a\rb",
		"a\x1b[31mb", "a>b", "a<b", "a'b", `a"b`, `a\b`, "a(b)", "a{b}", "a*b", "a?b",
		"a!b", "a#b", "a\x00b", "a b", "é", "-rf", "--registry=evil",
	}
	for _, eco := range fixCommandEcosystems {
		for _, v := range bad {
			if cmd, ok := FixCommand(eco, v, "1.2.3"); ok || cmd != "" {
				t.Errorf("FixCommand(%q, pkg %q) = %q, %v; want none", eco, v, cmd, ok)
			}
			if cmd, ok := FixCommand(eco, "p", v); ok || cmd != "" {
				t.Errorf("FixCommand(%q, version %q) = %q, %v; want none", eco, v, cmd, ok)
			}
		}
	}
}

// Every form the table's own ecosystems produce, with the characters real
// names and versions carry.
func TestFixCommandKeepsThePlainNamesAndVersionsByteForByte(t *testing.T) {
	tests := []struct{ ecosystem, pkg, version, want string }{
		{"gomod", "github.com/foo/bar/v2", "2.0.1", "go get github.com/foo/bar/v2@v2.0.1"},
		{"gomod", "golang.org/x/net", "v0.0.0-20230101-abcdef123456", "go get golang.org/x/net@v0.0.0-20230101-abcdef123456"},
		{"npm", "@scope/name.js", "1.2.3-beta.1+build.5", "npm install @scope/name.js@1.2.3-beta.1+build.5"},
		{"pip", "zope.interface", "5.0.0rc1", "pip install zope.interface==5.0.0rc1"},
		{"cargo", "tokio-util", "0.7.10", "cargo update -p tokio-util --precise 0.7.10"},
		{"debian", "libstdc++6", "12.2.0-14+deb12u1", "apt-get install --only-upgrade libstdc++6"},
		{"debian", "g++-12", "1:12.2.0~rc1-2", "apt-get install --only-upgrade g++-12"},
		{"alpine", "libcrypto3", "3.1.4-r0", "apk upgrade libcrypto3"},
		{"npm", "semver", "^6.3.1", "npm install semver@^6.3.1"},
	}
	for _, tc := range tests {
		got, ok := FixCommand(tc.ecosystem, tc.pkg, tc.version)
		if !ok || got != tc.want {
			t.Errorf("FixCommand(%q, %q, %q) = %q, %v; want %q", tc.ecosystem, tc.pkg, tc.version, got, ok, tc.want)
		}
	}
}

// The finding keeps a way to be fixed by hand: when no command is built, the
// sentence stands in, and the view shows it as text, never under Run.
func TestAHostileNameGivesASentenceNotACommand(t *testing.T) {
	const pkg = "x; rm -rf /home"
	got := trivyFixCommand("npm", vulnOf(pkg, "1.0.0", "1.2.3"))
	if got != "Update "+pkg+" to 1.2.3" {
		t.Fatalf("trivyFixCommand = %q, want the instruction", got)
	}
	if !IsFixInstruction(Finding{PkgName: pkg, FixCommand: got}) {
		t.Error("the sentence is not recognised as an instruction: it would be offered under Run")
	}
}
