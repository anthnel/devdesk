package command

import (
	"sort"
	"strings"
)

// Suggestion représente une suggestion de commande
type Suggestion struct {
	Text     string // Texte de la commande: "status", "s", "ctx list"
	Display  string // Format d'affichage: "s (status)", "ctx list"
	IsAlias  bool   // true pour les alias comme "s", "gla"
	FullName string // Nom complet si alias: "status" pour "s"
	Priority int    // Priorité de tri (1=exact, 2=alias, 3=full)
}

// CompletionEngine gère les suggestions de commandes
type CompletionEngine struct {
	commands []Suggestion // Liste pré-construite de suggestions
}

// NewCompletionEngine crée un nouveau moteur de complétion avec toutes les commandes disponibles
func NewCompletionEngine() *CompletionEngine {
	engine := &CompletionEngine{}
	engine.buildCommands()
	return engine
}

// buildCommands initialise la liste des commandes depuis le parser
func (ce *CompletionEngine) buildCommands() {
	var suggestions []Suggestion

	// Récupérer les alias
	aliases := GetAliases()

	// Ajouter les alias avec leur nom complet
	for alias, fullName := range aliases {
		suggestions = append(suggestions, Suggestion{
			Text:     alias,
			Display:  alias + " (" + fullName + ")",
			IsAlias:  true,
			FullName: fullName,
			Priority: 2, // Alias ont priorité moyenne
		})
	}

	// Ajouter les commandes complètes
	commands := map[string]string{
		"dashboard":     "dashboard",
		"status":        "status",
		"gitlab-auth":   "gitlab-auth",
		"containers":    "containers",
		"oci-resources": "oci-resources",
		"context":       "context",
		"theme":         "theme",
		"quit":          "quit",
	}

	for cmd, display := range commands {
		suggestions = append(suggestions, Suggestion{
			Text:     cmd,
			Display:  display,
			IsAlias:  false,
			FullName: "",
			Priority: 3, // Commandes complètes ont priorité basse
		})
	}

	// Ajouter alias pour context
	suggestions = append(suggestions, Suggestion{
		Text:     "ctx",
		Display:  "ctx (context)",
		IsAlias:  true,
		FullName: "context",
		Priority: 2,
	})

	// Ajouter alias pour quit
	suggestions = append(suggestions, Suggestion{
		Text:     "q",
		Display:  "q (quit)",
		IsAlias:  true,
		FullName: "quit",
		Priority: 2,
	})

	ce.commands = suggestions
}

// GetSuggestions retourne les suggestions correspondant à l'input
func (ce *CompletionEngine) GetSuggestions(input string) []Suggestion {
	input = strings.TrimSpace(strings.ToLower(input))

	// Input vide → retourner toutes les commandes (limitées)
	if input == "" {
		return ce.limitSuggestions(ce.commands, 10)
	}

	var matches []Suggestion

	// Matching par préfixe
	for _, cmd := range ce.commands {
		cmdLower := strings.ToLower(cmd.Text)

		// Match exact → priorité maximale
		if cmdLower == input {
			cmd.Priority = 1
			matches = append(matches, cmd)
		} else if strings.HasPrefix(cmdLower, input) {
			// Match préfixe → garder priorité existante
			matches = append(matches, cmd)
		}
	}

	// Trier par priorité (1=high, 3=low) puis alphabétiquement
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Priority != matches[j].Priority {
			return matches[i].Priority < matches[j].Priority
		}
		return matches[i].Text < matches[j].Text
	})

	return ce.limitSuggestions(matches, 10)
}

// limitSuggestions limite le nombre de suggestions retournées
func (ce *CompletionEngine) limitSuggestions(suggestions []Suggestion, max int) []Suggestion {
	if len(suggestions) > max {
		return suggestions[:max]
	}
	return suggestions
}
