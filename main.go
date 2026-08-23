package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/anthnel/devdesk/internal/app"
	"github.com/anthnel/devdesk/internal/config"
	mcpserver "github.com/anthnel/devdesk/internal/mcp"
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

// main dispatches on the first argument before doing anything else, and the
// ordering is load-bearing: everything below writes to stdout on the TUI path —
// a config warning, a log-file warning, a fatal error — and in stdio MCP stdout
// *is* the protocol channel. A single warning printed before the server starts
// makes the client report a JSON parse error that names nothing.
//
// TestTheMCPBranchIsTakenBeforeAnythingPrints is what keeps this order.
//
// No CLI framework: one subcommand and one flag do not need one, and the TUI is
// still what `dk` with no argument means.
func main() {
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		os.Exit(runMCP(os.Args[2:]))
	}
	runTUI()
}

// runMCP serves the read-only MCP server on stdio for exactly one context.
//
// It returns an exit code rather than calling os.Exit itself so the refusal and
// the failure paths read as one function.
func runMCP(args []string) int {
	fs := flag.NewFlagSet("dk mcp", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	contextName := fs.String("context", "", "the configuration context to serve (default: the current one)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	// Resolved once, here. A server that re-read ~/.devdesk/.current-context
	// would change what it answers underneath an agent mid-conversation,
	// because the TUI writes that file.
	name := *contextName
	if name == "" {
		name = config.CurrentContextName()
	}

	cfg, err := config.LoadContext(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dk mcp: cannot load context %q: %v\n", name, err)
		return 1
	}

	if !cfg.MCP.Enabled {
		fmt.Fprintf(os.Stderr, "dk mcp: %v\n", mcpserver.Refused(name))
		return 1
	}

	// The client kills the process to stop the server, so a cancellable context
	// is what turns that into a clean shutdown rather than a severed pipe.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// io.EOF is how a stdio server ends: the client closed the pipe, which is
	// the ordinary shutdown and not a failure. Reporting it as one gives an exit
	// code that some clients surface as a crash for a server that worked. The
	// sentinel is checked rather than the message because the SDK's own
	// ErrServerClosing lives in an internal package and cannot be compared to.
	err = mcpserver.Serve(ctx, &mcpserver.Env{Config: cfg, Context: name})
	if err != nil && !errors.Is(err, io.EOF) && ctx.Err() == nil {
		fmt.Fprintf(os.Stderr, "dk mcp: %v\n", err)
		return 1
	}
	return 0
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

	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running application: %v\n", err)
		os.Exit(1)
	}
}
