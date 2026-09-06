package command

import (
	"testing"
)

func TestNewCompletionEngine(t *testing.T) {
	engine := NewCompletionEngine()
	if len(engine.commands) == 0 {
		t.Error("Expected commands to be initialized")
	}

	// Vérifier qu'on a au moins les commandes de base
	hasStatus := false
	hasQuit := false
	for _, cmd := range engine.commands {
		if cmd.Text == "status" {
			hasStatus = true
		}
		if cmd.Text == "quit" {
			hasQuit = true
		}
	}

	if !hasStatus {
		t.Error("Expected 'status' command to be initialized")
	}
	if !hasQuit {
		t.Error("Expected 'quit' command to be initialized")
	}
}

// The catalogue and the parser used to be two hand-kept lists, and had drifted:
// `git-explorer`, `workspaces`, `security` and `net` all worked when typed in
// full but could not be completed (§1.3 D17). They are one list now, and these
// two tests are what keeps them one.

func TestEverythingThatParsesCanBeCompleted(t *testing.T) {
	engine := NewCompletionEngine()

	for _, name := range everyCommandName() {
		suggested := false
		for _, s := range engine.GetSuggestions(name) {
			if s.Text == name {
				suggested = true
				break
			}
		}
		if !suggested {
			t.Errorf("%q is accepted by the parser but never suggested", name)
		}
	}
}

func TestEverythingSuggestedCanBeRun(t *testing.T) {
	for _, s := range NewCompletionEngine().commands {
		if ParseCommand(s.Text).Type == CommandUnknown {
			t.Errorf("%q is suggested but the parser rejects it", s.Text)
		}
	}
}

// everyCommandName is every spelling the parser accepts, views and actions
// alike.

// TestTheNewNamesAreTheSuggestedOnes is the other half: a rename nobody is told
// about is a removal with extra steps.
func TestTheNewNamesAreTheSuggestedOnes(t *testing.T) {
	engine := NewCompletionEngine()

	for _, name := range []string{"git-auth", "ga", "git-explorer", "ge"} {
		suggested := false
		for _, s := range engine.GetSuggestions(name) {
			if s.Text == name {
				suggested = true
			}
		}
		if !suggested {
			t.Errorf("%q is not suggested", name)
		}
	}
}

// everyCommandName is every spelling completion is expected to offer.
func everyCommandName() []string {
	names := make([]string, 0, len(viewNames)+len(actionNames)+len(actionAliases))
	for name := range viewNames {
		names = append(names, name)
	}
	for name := range actionNames {
		names = append(names, name)
	}
	for name := range actionAliases {
		names = append(names, name)
	}
	return names
}

func TestGetSuggestions_EmptyInput(t *testing.T) {
	engine := NewCompletionEngine()
	suggestions := engine.GetSuggestions("")

	// Should return commands (limited to 10)
	if len(suggestions) == 0 {
		t.Error("Expected suggestions for empty input")
	}
	if len(suggestions) > 10 {
		t.Errorf("Expected max 10 suggestions, got %d", len(suggestions))
	}
}

func TestGetSuggestions_PrefixMatch(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"s", []string{"s", "status"}},
		{"st", []string{"status"}},
		{"ga", []string{"ga"}},         // Exact match on alias
		{"git-", []string{"git-auth"}}, // Prefix match on full command
		{"gla", []string{}},            // Retired spelling: no longer parses, no suggestions
		{"gitlab", []string{}},         // Same, in full
		{"q", []string{"q", "quit"}},
		{"xyz", []string{}}, // no match
	}

	engine := NewCompletionEngine()
	for _, tt := range tests {
		t.Run("input:"+tt.input, func(t *testing.T) {
			suggestions := engine.GetSuggestions(tt.input)

			// Verify suggestions contain expected texts
			for _, exp := range tt.expected {
				found := false
				for _, sug := range suggestions {
					if sug.Text == exp {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Input %q: expected suggestion %q not found", tt.input, exp)
				}
			}

			// If no matches expected, verify empty
			if len(tt.expected) == 0 && len(suggestions) != 0 {
				t.Errorf("Input %q: expected no suggestions, got %d", tt.input, len(suggestions))
			}
		})
	}
}

func TestGetSuggestions_CaseInsensitive(t *testing.T) {
	engine := NewCompletionEngine()

	// Test lowercase, uppercase, mixed case
	inputs := []string{"s", "S", "St", "ST", "status", "STATUS"}

	for _, input := range inputs {
		suggestions := engine.GetSuggestions(input)
		if len(suggestions) == 0 {
			t.Errorf("Input %q: expected suggestions for case-insensitive match", input)
		}

		// All should have "status" somewhere in suggestions
		hasStatus := false
		for _, sug := range suggestions {
			if sug.Text == "status" {
				hasStatus = true
				break
			}
		}
		if !hasStatus {
			t.Errorf("Input %q: expected 'status' in suggestions", input)
		}
	}
}

func TestGetSuggestions_Priority(t *testing.T) {
	engine := NewCompletionEngine()

	// Match exact should have priority 1
	suggestions := engine.GetSuggestions("status")
	if len(suggestions) == 0 {
		t.Fatal("Expected suggestions for 'status'")
	}

	// First suggestion should be exact match with priority 1
	if suggestions[0].Text != "status" || suggestions[0].Priority != 1 {
		t.Errorf("Expected first suggestion to be exact match 'status' with priority 1, got %q priority %d",
			suggestions[0].Text, suggestions[0].Priority)
	}
}

func TestGetSuggestions_ContextCommand(t *testing.T) {
	engine := NewCompletionEngine()

	// "context" should match as a simple command
	suggestions := engine.GetSuggestions("context")
	hasContext := false
	for _, sug := range suggestions {
		if sug.Text == "context" {
			hasContext = true
			break
		}
	}
	if !hasContext {
		t.Error("Expected 'context' suggestion")
	}

	// "ctx" alias should also match
	suggestions = engine.GetSuggestions("ctx")
	hasCtx := false
	for _, sug := range suggestions {
		if sug.Text == "ctx" {
			hasCtx = true
			break
		}
	}
	if !hasCtx {
		t.Error("Expected 'ctx' alias suggestion")
	}
}

func TestLimitSuggestions(t *testing.T) {
	engine := NewCompletionEngine()

	// Create more than 10 suggestions
	suggestions := []Suggestion{}
	for i := 0; i < 15; i++ {
		suggestions = append(suggestions, Suggestion{Text: "test"})
	}

	limited := engine.limitSuggestions(suggestions, 10)

	if len(limited) != 10 {
		t.Errorf("Expected 10 suggestions, got %d", len(limited))
	}

	// Test with fewer than limit
	short := []Suggestion{{Text: "test1"}, {Text: "test2"}}
	limitedShort := engine.limitSuggestions(short, 10)

	if len(limitedShort) != 2 {
		t.Errorf("Expected 2 suggestions to remain unchanged, got %d", len(limitedShort))
	}
}

func TestSuggestion_DisplayFormat(t *testing.T) {
	engine := NewCompletionEngine()

	// Test alias display format
	suggestions := engine.GetSuggestions("s")

	foundAlias := false
	for _, sug := range suggestions {
		if sug.Text == "s" && sug.IsAlias {
			// Alias should have format "s (status)"
			if sug.Display != "s (status)" {
				t.Errorf("Expected alias display 's (status)', got %q", sug.Display)
			}
			if sug.FullName != "status" {
				t.Errorf("Expected FullName 'status', got %q", sug.FullName)
			}
			foundAlias = true
			break
		}
	}

	if !foundAlias {
		t.Error("Expected to find alias 's' with proper display format")
	}
}
