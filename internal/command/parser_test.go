package command

import (
	"testing"
)

func TestParse_AllValidCommands(t *testing.T) {
	// Vérifier que toutes les commandes valides fonctionnent
	tests := []struct {
		name     string
		input    string
		expected ViewType
	}{
		{"status", "status", ViewStatus},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Parse(tt.input)
			if err != nil {
				t.Errorf("Parse() error = %v", err)
				return
			}
			if result != tt.expected {
				t.Errorf("Parse() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestParseCommand_Theme(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		expectedType CommandType
		expectedArgs []string
	}{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseCommand(tt.input)
			if result.Type != tt.expectedType {
				t.Errorf("ParseCommand(%q).Type = %v, want %v", tt.input, result.Type, tt.expectedType)
			}
			if tt.expectedArgs != nil {
				if len(result.Args) != len(tt.expectedArgs) {
					t.Errorf("ParseCommand(%q).Args len = %d, want %d", tt.input, len(result.Args), len(tt.expectedArgs))
					return
				}
				for i, arg := range tt.expectedArgs {
					if result.Args[i] != arg {
						t.Errorf("ParseCommand(%q).Args[%d] = %q, want %q", tt.input, i, result.Args[i], arg)
					}
				}
			}
		})
	}
}

// TestRetiredSpellingsNoLongerParse — the forge-prefixed names §3.6 replaced
// (gitlab-auth/gla, gitlab-explorer/gle, github-auth/gha, github-explorer/ghe,
// explorer/exp) used to keep resolving as a legacy fallback. That fallback is
// gone: one spelling per view, no exceptions.
func TestRetiredSpellingsNoLongerParse(t *testing.T) {
	for _, name := range []string{
		"gitlab-auth", "gla", "github-auth", "gha",
		"gitlab-explorer", "gle", "github-explorer", "ghe",
		"explorer", "exp",
	} {
		if got := ParseCommand(name).Type; got != CommandUnknown {
			t.Errorf("ParseCommand(%q).Type = %v, want CommandUnknown", name, got)
		}
	}
}

func TestGetAliases(t *testing.T) {
	aliases := GetAliases()

	// Vérifier que s est dans les alias
	if aliases["s"] != "status" {
		t.Errorf("GetAliases() should include s -> status, got: %s", aliases["s"])
	}
}

func TestParse_Netdiag(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected ViewType
	}{
		// The view is called netdiag everywhere — the directory, the docs, the
		// command list — but only "net" parsed, and ":netdiag" fell through to
		// CommandUnknown with no diagnostic (§1.3 D16).
		{"full name", "netdiag", ViewNetdiag},
		{"net alias", "net", ViewNetdiag},
		{"net with colon prefix", ":net", ViewNetdiag},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Parse(tt.input)
			if err != nil {
				t.Errorf("Parse() error = %v", err)
				return
			}
			if result != tt.expected {
				t.Errorf("Parse() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestParseCommand_Netdiag(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		expectedType CommandType
		expectedView ViewType
	}{
		{"net command", "net", CommandView, ViewNetdiag},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseCommand(tt.input)
			if result.Type != tt.expectedType {
				t.Errorf("ParseCommand(%q).Type = %v, want %v", tt.input, result.Type, tt.expectedType)
			}
			if result.View != tt.expectedView {
				t.Errorf("ParseCommand(%q).View = %v, want %v", tt.input, result.View, tt.expectedView)
			}
		})
	}
}
