package status

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/status"
)

// tickCmd returns a command that ticks every second
func tickCmd() tea.Cmd {
	return tea.Tick(1*time.Second, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

// checkComponents launches the check of all components (async)
func checkComponents(cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		// Create the checker
		checker := status.NewChecker(time.Duration(cfg.Status.Timeout))

		// Launch the checks
		ctx := context.Background()
		results := checker.CheckAll(ctx, cfg.Status.Components)

		// Return the results
		return CheckCompleteMsg{
			Components: results,
			Timestamp:  time.Now(),
			Err:        nil,
		}
	}
}

// saveComponent saves the config (Rule 110: does NOT modify the config, I/O only)
func saveComponent(cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		// Save the config (I/O only, no model modification)
		err := config.Save(cfg)
		return ComponentSavedMsg{
			Success: err == nil,
			Error:   err,
		}
	}
}

// deleteComponent saves the config (Rule 110: does NOT modify the config, I/O only)
func deleteComponent(cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		// Save the config (I/O only, no model modification)
		err := config.Save(cfg)
		return ComponentDeletedMsg{
			Success: err == nil,
			Error:   err,
		}
	}
}
