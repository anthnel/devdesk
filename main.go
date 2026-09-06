package main

import (
	"fmt"
	"log"
	"os"

	"github.com/anthnel/devdesk/internal/app"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
)

func getCurrentContextName() string {
	ctx, err := config.GetCurrentContext()
	if err != nil || ctx == "" {
		return "default"
	}
	return ctx
}

// main runs the TUI. There is no subcommand any more.
//
// `dk mcp` served the MCP server on stdio (§3.38) and is gone with it (§3.61):
// the server now lives in this process, over HTTP, started by the router. What
// that deletes is the flag set, the argument dispatch, and the ordering
// constraint that used to sit here — everything below writes to stdout, and in
// stdio stdout *was* the protocol channel, so a single warning printed before
// the server started made the client report a JSON parse error naming nothing.
// Over HTTP the two do not share a channel, so the constraint is gone rather
// than relaxed.
func main() {
	runTUI()
}

func runTUI() {
	// Forcer TrueColor si Windows Terminal est détecté (WSL ne propage pas COLORTERM)
	if os.Getenv("WT_SESSION") != "" && os.Getenv("COLORTERM") == "" {
		_ = os.Setenv("COLORTERM", "truecolor")
	}

	// Charger la configuration
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		fmt.Println("Using default configuration.")
		cfg = config.Default()
	}

	// Créer le répertoire de config s'il n'existe pas
	if err := config.EnsureConfigDir(); err != nil {
		fmt.Printf("Warning: Could not create config directory: %v\n", err)
	}

	// Toujours activer le logging dans un fichier pour éviter de polluer l'interface TUI
	logFile, err := tea.LogToFile(cfg.App.LogFile, "debug")
	if err != nil {
		fmt.Printf("Warning: Could not create log file: %v\n", err)
	} else {
		defer func() { _ = logFile.Close() }()
		log.Printf("=== DevDesk Started ===")
		log.Printf("Config loaded from context: %s", getCurrentContextName())
		log.Printf("Log file: %s", cfg.App.LogFile)
		log.Printf("DEBUG mode: %v", len(os.Getenv("DEBUG")) > 0)
	}

	// Charger et appliquer le thème sauvegardé
	if cfg.App.Theme != "" && cfg.App.Theme != "dark" {
		t, err := theme.LoadTheme(cfg.App.Theme)
		if err != nil {
			log.Printf("Warning: Could not load theme '%s': %v, using default", cfg.App.Theme, err)
		} else {
			theme.ApplyTheme(t)
			theme.CurrentThemeName = cfg.App.Theme
			log.Printf("Theme loaded: %s", cfg.App.Theme)
		}
	}

	// Créer l'application avec le router
	m := app.New(cfg)

	// Options du programme Bubble Tea
	opts := []tea.ProgramOption{
		tea.WithAltScreen(), // Mode plein écran
	}

	// Lancer le programme Bubble Tea
	p := tea.NewProgram(m, opts...)

	// The router needs the handle before the loop starts: an MCP request
	// reaches Update() through p.Send, and there is no other legal door
	// (Rule 110). Written here, once, while nothing else is running.
	m.AttachProgram(p)

	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running application: %v\n", err)
		os.Exit(1)
	}
}
