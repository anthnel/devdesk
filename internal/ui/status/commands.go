package status

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/status"
)

// tickCmd retourne une commande qui tick toutes les secondes
func tickCmd() tea.Cmd {
	return tea.Tick(1*time.Second, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

// checkComponents lance la vérification de tous les composants (async)
func checkComponents(cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		// Créer le checker
		checker := status.NewChecker(time.Duration(cfg.Status.Timeout))

		// Lancer les vérifications
		ctx := context.Background()
		results := checker.CheckAll(ctx, cfg.Status.Components)

		// Retourner les résultats
		return CheckCompleteMsg{
			Components: results,
			Timestamp:  time.Now(),
			Err:        nil,
		}
	}
}

// saveComponent sauvegarde la config (Rule 110: ne modifie PAS la config, juste I/O)
func saveComponent(cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		// Sauvegarder la config (I/O uniquement, pas de modification du modèle)
		err := config.Save(cfg)
		return ComponentSavedMsg{
			Success: err == nil,
			Error:   err,
		}
	}
}

// deleteComponent sauvegarde la config (Rule 110: ne modifie PAS la config, juste I/O)
func deleteComponent(cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		// Sauvegarder la config (I/O uniquement, pas de modification du modèle)
		err := config.Save(cfg)
		return ComponentDeletedMsg{
			Success: err == nil,
			Error:   err,
		}
	}
}
