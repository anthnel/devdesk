package command

import (
	"sort"
	"strings"
)

// Suggestion represents a command suggestion
type Suggestion struct {
	Text     string // Command text: "status", "s", "ctx list"
	Display  string // Display format: "s (status)", "ctx list"
	IsAlias  bool   // true for aliases like "s", "ga"
	FullName string // Full name if alias: "status" for "s"
	Priority int    // Sort priority (1=exact, 2=alias, 3=full)
}

// CompletionEngine manages command suggestions
type CompletionEngine struct {
	commands []Suggestion // Pre-built list of suggestions
}

// NewCompletionEngine creates a new completion engine with all available commands
func NewCompletionEngine() *CompletionEngine {
	engine := &CompletionEngine{}
	engine.buildCommands()
	return engine
}

// buildCommands derives the suggestion list from the parser, which is the
// only source: everything that can be typed can be completed, and nothing else.
func (ce *CompletionEngine) buildCommands() {
	fullNames := FullNames()
	aliases := GetAliases()
	suggestions := make([]Suggestion, 0, len(fullNames)+len(aliases))

	for _, cmd := range fullNames {
		suggestions = append(suggestions, Suggestion{
			Text:     cmd,
			Display:  cmd,
			IsAlias:  false,
			FullName: "",
			Priority: 3, // Full commands have low priority
		})
	}

	for alias, fullName := range aliases {
		suggestions = append(suggestions, Suggestion{
			Text:     alias,
			Display:  alias + " (" + fullName + ")",
			IsAlias:  true,
			FullName: fullName,
			Priority: 2, // Aliases have medium priority
		})
	}

	ce.commands = suggestions
}

// GetSuggestions returns the suggestions matching the input
func (ce *CompletionEngine) GetSuggestions(input string) []Suggestion {
	input = strings.TrimSpace(strings.ToLower(input))

	// Empty input -> return all commands (limited)
	if input == "" {
		return ce.limitSuggestions(ce.commands, 10)
	}

	var matches []Suggestion

	// Prefix matching
	for _, cmd := range ce.commands {
		cmdLower := strings.ToLower(cmd.Text)

		// Exact match -> maximum priority
		if cmdLower == input {
			cmd.Priority = 1
			matches = append(matches, cmd)
		} else if strings.HasPrefix(cmdLower, input) {
			// Prefix match -> keep existing priority
			matches = append(matches, cmd)
		}
	}

	// Sort by priority (1=high, 3=low) then alphabetically
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Priority != matches[j].Priority {
			return matches[i].Priority < matches[j].Priority
		}
		return matches[i].Text < matches[j].Text
	})

	return ce.limitSuggestions(matches, 10)
}

// limitSuggestions limits the number of suggestions returned
func (ce *CompletionEngine) limitSuggestions(suggestions []Suggestion, max int) []Suggestion {
	if len(suggestions) > max {
		return suggestions[:max]
	}
	return suggestions
}
