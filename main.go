package main

import (
	"fmt"
	"log"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/anthnel/devdesk/internal/app"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/termtitle"
	"github.com/anthnel/devdesk/internal/ui/setup"
	"github.com/anthnel/devdesk/internal/ui/theme"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/term"
)

func getCurrentContextName() string {
	ctx, err := config.GetCurrentContext()
	if err != nil || ctx == "" {
		return "default"
	}
	return ctx
}

// main runs the TUI, with one exception: `dk setup`.
//
// `dk mcp` served the MCP server on stdio (§3.38) and is gone with it (§3.61):
// the server now lives in this process, over HTTP, started by the router. What
// that deleted was the flag set, the argument dispatch, and the ordering
// constraint that used to sit here — everything below writes to stdout, and in
// stdio stdout *was* the protocol channel, so a single warning printed before
// the server started made the client report a JSON parse error naming nothing.
// Over HTTP the two do not share a channel, so that constraint is gone rather
// than relaxed.
//
// `setup` is not a reversal of "no subcommand any more": it is one literal
// case, not a flag-parsing framework, and it runs before any context is
// loaded — the one thing the TUI itself cannot do for a user who has none yet.
func main() {
	if len(os.Args) > 1 && os.Args[1] == "setup" {
		if err := setup.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "setup failed:", err)
			os.Exit(1)
		}
		return
	}
	runTUI()
}

// forcedColorProfile returns TrueColor when the program runs in Windows
// Terminal, which WSL does not advertise through COLORTERM. Anywhere else, and
// whenever NO_COLOR is set, it reports false so Bubble Tea detects the profile
// itself: a forced profile bypasses the detection that honours NO_COLOR.
func forcedColorProfile(getenv func(string) string) (colorprofile.Profile, bool) {
	if getenv("WT_SESSION") != "" && getenv("NO_COLOR") == "" {
		return colorprofile.TrueColor, true
	}
	return colorprofile.NoTTY, false
}

// ensureDefaultContext writes config.yaml, with the commented price example, on
// a first launch. A failure is logged and does not stop the launch: the
// application runs on the in-memory defaults, as it did before the file existed.
func ensureDefaultContext() {
	if _, err := config.CreateDefaultContextIfMissing(); err != nil {
		log.Printf("WARN [config] could not create the default context file: %v", err)
	}
}

func runTUI() {
	// Load the configuration
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		fmt.Println("Using default configuration.")
		cfg = config.Default()
	}

	// Create the config directory if it does not exist
	if err := config.EnsureConfigDir(); err != nil {
		fmt.Printf("Warning: Could not create config directory: %v\n", err)
	}

	// Always enable logging to a file to avoid polluting the TUI interface
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

	// The log is open (or unavailable): a failure below is reported, never fatal.
	ensureDefaultContext()

	// app.theme is applied by the router (app.New).
	theme.SetGaugeGlyph(cfg.App.GaugeGlyph)

	// Create the application with the router
	m := app.New(cfg)

	// Start the Bubble Tea program. The router's View asks for the alternate
	// screen; main.go sets no terminal state.
	var opts []tea.ProgramOption
	if profile, forced := forcedColorProfile(os.Getenv); forced {
		opts = append(opts, tea.WithColorProfile(profile))
	}
	p := tea.NewProgram(m, opts...)

	// The router needs the handle before the loop starts: an MCP request
	// reaches Update() through p.Send, and there is no other legal door
	// (Rule 110). Written here, once, while nothing else is running.
	m.AttachProgram(p)

	// Bubble Tea clears the window title on exit; keep the one the terminal had
	// and put it back afterwards. Only on a terminal: a pipe would receive the
	// escape sequences as text.
	run := func() error {
		_, err := p.Run()
		return err
	}
	if term.IsTerminal(os.Stdout.Fd()) {
		err = termtitle.Preserve(os.Stdout, run)
	} else {
		err = run()
	}
	if err != nil {
		fmt.Printf("Error running application: %v\n", err)
		os.Exit(1)
	}
}
