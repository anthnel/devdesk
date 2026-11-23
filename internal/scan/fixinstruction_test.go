package scan

import "testing"

// DEVDESK-750 (step 2). A finding's FixCommand is either a command the user can
// run or, for an ecosystem with no known command, the sentence "Update p to v".
// The details view must tell them apart (it shows "Run: ..." only for the
// first), and the sentence must stay byte-identical: it is stored in the scan
// cache, and changing it would make every cached result read differently.

// fixCommandEcosystems is every ecosystem FixCommand knows (a case added to
// FixCommand belongs here too).
var fixCommandEcosystems = []string{
	"gomod", "gobinary", "npm", "node-pkg", "yarn", "pnpm", "pip", "pipenv",
	"python-pkg", "poetry", "cargo", "rust-binary", "alpine", "debian", "ubuntu",
}

func vulnOf(pkg, installed, fixed string) TrivyVulnerability {
	return TrivyVulnerability{PkgName: pkg, InstalledVersion: installed, FixedVersion: fixed}
}

func TestAKnownEcosystemFixIsACommandNotAnInstruction(t *testing.T) {
	for _, eco := range fixCommandEcosystems {
		t.Run(eco, func(t *testing.T) {
			cmd := trivyFixCommand(eco, vulnOf("p", "1.0.0", "1.2.3"))
			if cmd == "" {
				t.Fatalf("trivyFixCommand(%q) is empty", eco)
			}
			if _, ok := FixCommand(eco, "p", "1.2.3"); !ok {
				t.Fatalf("the fixture claims %q is a known ecosystem and FixCommand disagrees", eco)
			}
			if IsFixInstruction(Finding{PkgName: "p", FixCommand: cmd}) {
				t.Errorf("IsFixInstruction(%q) = true for a real command", cmd)
			}
		})
	}
}

func TestAnUnknownEcosystemFixIsAnInstructionByteForByte(t *testing.T) {
	tests := []struct {
		name, ecosystem string
		vuln            TrivyVulnerability
		want            string
	}{
		{"conda", "conda", vulnOf("p", "1.0", "1.2"), "Update p to 1.2"},
		{"empty ecosystem (cached result)", "", vulnOf("p", "1.0", "1.2"), "Update p to 1.2"},
		{"scoped name", "conda", vulnOf("@scope/p", "1.0", "1.2"), "Update @scope/p to 1.2"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := trivyFixCommand(tc.ecosystem, tc.vuln)
			if got != tc.want {
				t.Fatalf("trivyFixCommand = %q, want %q (the cache holds this exact string)", got, tc.want)
			}
			if !IsFixInstruction(Finding{PkgName: tc.vuln.PkgName, FixCommand: got}) {
				t.Errorf("IsFixInstruction(%q) = false for an instruction", got)
			}
		})
	}
}

func TestFixInstructionIsTheSentenceTrivyFixCommandUses(t *testing.T) {
	if got := fixInstruction("p", "1.2"); got != "Update p to 1.2" {
		t.Errorf("fixInstruction(p, 1.2) = %q, want %q", got, "Update p to 1.2")
	}
	if got, want := trivyFixCommand("conda", vulnOf("q", "0.1", "0.2")), fixInstruction("q", "0.2"); got != want {
		t.Errorf("trivyFixCommand = %q, fixInstruction = %q: two spellings of one sentence", got, want)
	}
}

func TestIsFixInstructionEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		f    Finding
		want bool
	}{
		{"empty FixCommand", Finding{PkgName: "p"}, false},
		{"empty everything", Finding{}, false},
		{"another package", Finding{PkgName: "p", FixCommand: "Update q to 1.2"}, false},
		{"no version after to", Finding{PkgName: "p", FixCommand: "Update p to"}, false},
		{"no package name on the finding", Finding{FixCommand: "Update p to 1.2"}, false},
		{"a package that merely starts alike", Finding{PkgName: "p", FixCommand: "Update pp to 1.2"}, false},
		{"a real command", Finding{PkgName: "p", FixCommand: "go get p@v1.2"}, false},
		{"the sentence", Finding{PkgName: "p", FixCommand: "Update p to 1.2"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsFixInstruction(tc.f); got != tc.want {
				t.Errorf("IsFixInstruction(%+v) = %v, want %v", tc.f, got, tc.want)
			}
		})
	}
}
