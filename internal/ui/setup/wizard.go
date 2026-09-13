package setup

import tea "github.com/charmbracelet/bubbletea"

// Run launches the `dk setup` wizard as a standalone full-screen program. It
// is the only entry point main.go needs from this package.
func Run() error {
	p := tea.NewProgram(New(), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
