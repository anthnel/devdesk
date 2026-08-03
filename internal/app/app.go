package app

import (
	"log"
	"os"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/ui/gitlab/auth"
	"github.com/anthnel/devdesk/internal/ui/gitlab/explorer"
	ociresources "github.com/anthnel/devdesk/internal/ui/oci_resources"
	"github.com/anthnel/devdesk/internal/ui/security"
	"github.com/anthnel/devdesk/internal/ui/theme"
	"github.com/anthnel/devdesk/internal/ui/workspaces"
)

// FormView interface for views that can have active forms
// Views implementing this interface can prevent command mode activation
type FormView interface {
	InEditMode() bool
}

// FilterBarView is implemented by views that have a visible filter bar.
// When the filter bar is visible, the viewport bottom border corners are
// replaced with T-junction chars (├/┤) to form a closed rectangle.
type FilterBarView interface {
	FilterBarVisible() bool
}

// App est le modèle principal avec routeur multi-vues
type App struct {
	config         *config.Config
	currentContext string
	viewport       viewport.Model

	// Navigation
	currentView command.ViewType
	views       map[command.ViewType]tea.Model
	sharedState *shared.State

	commandMode  bool
	commandInput textinput.Model

	// Command completion state
	completionEngine      *command.CompletionEngine
	completionSuggestions []command.Suggestion
	completionIndex       int    // Position Tab cycling
	completionInput       string // Détection changement

	// Context list overlay
	showContextList    bool
	contextList        []string
	contextSelectedIdx int

	// Theme list overlay
	showThemeList    bool
	themeList        []string
	currentTheme     string
	themeSelectedIdx int

	// Help overlay
	showHelp     bool
	helpViewport viewport.Model

	// Selection mode (cross-view browsing)
	selectionReturnView command.ViewType // View to return to after selection

	// Dimensions
	width            int
	height           int
	lastFooterHeight int // tracks current view's footer height for re-resize detection
}

func newCommandInput() textinput.Model {
	// Créer l'input pour le mode commande
	cmdInput := textinput.New()
	cmdInput.Prompt = "❯"
	cmdInput.SetSuggestions([]string{"status", "workspaces"})
	cmdInput.CharLimit = 50

	return cmdInput
}

func newViewport(width, height int) viewport.Model {
	vp := viewport.New(width, height)
	vp.Style = viewportStyle()
	return vp
}

func viewportStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Background(theme.ColorBackground).
		Foreground(theme.ColorText).
		Border(lipgloss.NormalBorder(), false, true, true, true).
		BorderForeground(theme.ColorViewportBorder).
		BorderBackground(theme.ColorBackground)
}

// New crée une nouvelle App, dimensionnée sur le terminal courant.
func New(cfg *config.Config) *App {
	width, height, err := term.GetSize(os.Stdout.Fd())
	if err != nil {
		panic(err)
	}
	return newWithSize(cfg, width, height)
}

// newWithSize builds the router at a given size. New() reads that size from the
// terminal; splitting it out keeps the constructor itself free of I/O.
func newWithSize(cfg *config.Config, width, height int) *App {
	currentContext, _ := config.GetCurrentContext()
	sharedState := &shared.State{}

	app := &App{
		config:           cfg,
		currentContext:   currentContext,
		viewport:         newViewport(width, height),
		currentView:      defaultView(cfg),
		views:            make(map[command.ViewType]tea.Model),
		sharedState:      sharedState,
		commandInput:     newCommandInput(),
		completionEngine: command.NewCompletionEngine(),
		width:            width,
		height:           height,
	}

	// Les autres vues sont créées à la demande (lazy loading).
	app.createView(command.ViewDashboard)
	app.createView(command.ViewStatus)
	app.createView(app.currentView)

	// Calcule la hauteur disponible et propage aux vues.
	app.resize(width, height)

	return app
}

// defaultView resolves the configured landing view, falling back to the
// dashboard when the setting is missing or unparseable.
func defaultView(cfg *config.Config) command.ViewType {
	if cfg.App.DefaultView == "" {
		return command.ViewDashboard
	}
	parsed, err := command.Parse(cfg.App.DefaultView)
	if err != nil || parsed == "" {
		return command.ViewDashboard
	}
	return parsed
}

// Init initialise l'application
func (a *App) Init() tea.Cmd {
	// Note: resize() est déjà appelé dans newWithSize() pour initialiser les dimensions
	cmds := []tea.Cmd{a.tryAutoLogin()}

	if view, ok := a.views[a.currentView]; ok {
		cmds = append(cmds, view.Init())
	}

	return tea.Batch(cmds...)
}

// requestResize asks for a fresh layout at the current size. Every handler that
// changes what the window holds returns one, because the viewport height is
// budgeted against the header and the active view's footer.
func (a *App) requestResize() tea.Cmd {
	return func() tea.Msg {
		return tea.WindowSizeMsg{Width: a.width, Height: a.height}
	}
}

func (a *App) resize(width, height int) {
	a.width = width
	a.height = height

	// Rule 124: footer height is deducted from the viewport (tabs + optional info line)
	footerHeight := a.getFooterHeight()
	a.lastFooterHeight = footerHeight

	headerHeight := lipgloss.Height(a.renderHeader())
	availableHeight := height - headerHeight - footerHeight
	a.viewport.Width = width
	a.viewport.Height = availableHeight - 1 // -1 for the custom title border line
	a.viewport.Style = viewportStyle()

	// Propager à la vue active. Le viewport a 1 bordure (bottom only), la vue
	// doit connaître la hauteur intérieure.
	const viewportBorderHeight = 1
	contentHeight := max((availableHeight-1)-viewportBorderHeight, 1)
	if view, ok := a.views[a.currentView]; ok {
		updatedView, _ := view.Update(tea.WindowSizeMsg{Width: width, Height: contentHeight})
		a.views[a.currentView] = updatedView
	}
}

// getFooterHeight returns the current footer height for the active view.
func (a *App) getFooterHeight() int {
	if view, ok := a.views[a.currentView]; ok {
		if fv, ok := view.(FooterView); ok {
			return fv.GetFooterHeight()
		}
	}
	return 0
}

// reinitializeViews drops every view so they are rebuilt against the current
// config, and returns the init cmd for the one on screen.
func (a *App) reinitializeViews() tea.Cmd {
	log.Printf("Reinitializing views with new config")
	a.views = make(map[command.ViewType]tea.Model)
	a.createView(a.currentView)

	var initCmd tea.Cmd
	if view, ok := a.views[a.currentView]; ok {
		initCmd = view.Init()
	}
	log.Printf("Views reinitialized successfully")
	return initCmd
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.resize(msg.Width, msg.Height)
		return a, nil

	case tea.KeyMsg:
		return a.handleKeyMsg(msg)

	// ── Contexts and themes ──────────────────────────────────────────────
	case ContextSwitchCompleteMsg:
		return a.handleContextSwitchComplete(msg)

	case ContextSwitchErrorMsg:
		log.Printf("ERROR: Context operation failed: %v", msg.Error)
		return a, nil

	case ContextListMsg:
		return a.handleContextList(msg)

	case ThemeListMsg:
		return a.handleThemeList(msg)

	case ThemeAppliedMsg:
		return a.handleThemeApplied(msg)

	case ThemeErrorMsg:
		log.Printf("ERROR: Theme operation failed: %v", msg.Error)
		return a, nil

	// ── GitLab authentication ────────────────────────────────────────────
	case auth.AuthResultMsg:
		return a.handleAuthResult(msg)

	case GitLabAutoLoginMsg:
		return a.handleAutoLoginResult(msg)

	// ── Scans ────────────────────────────────────────────────────────────
	case workspaces.WorkspaceScanCompleteMsg:
		return a.handleWorkspaceScanComplete(msg)

	case workspaces.ScanDetailsRequestMsg:
		return a.handleWorkspaceScanDetails(msg)

	case WorkspaceScanResultLoadedMsg:
		return a.handleWorkspaceScanResultLoaded(msg)

	case ociresources.ScanDetailsRequestMsg:
		return a.handleScanDetailsRequest(msg)

	case ImageScanResultLoadedMsg:
		return a.handleImageScanResultLoaded(msg)

	case ociresources.ImageScanStartingMsg:
		// Scan progress belongs to the OCI view wherever the user has gone.
		return a.routeToOCIImagesView(msg)

	case ociresources.ImageScanFinishedMsg:
		return a.routeToOCIImagesView(msg)

	case ociresources.LaunchBatchScanMsg:
		// The security view delegated the scan back to the OCI view.
		return a.handleLaunchScan(msg)

	case ociresources.LaunchSingleImageScanMsg:
		return a.handleLaunchScan(msg)

	// ── Selection mode ───────────────────────────────────────────────────
	case security.SelectionRequestMsg:
		return a.handleSelectionRequest(msg)

	case security.BackToOriginMsg:
		return a, a.switchView(msg.Origin)

	case explorer.PullSelectionRequestMsg:
		return a.handleExplorerPullRequest()

	case workspaces.DirectorySelectedMsg:
		return a.handleDirectorySelected(msg)

	case ociresources.ImageSelectedMsg:
		return a.handleImageSelected(msg)

	case workspaces.SelectionCancelledMsg:
		return a.handleSelectionCancelled()

	case ociresources.SelectionCancelledMsg:
		return a.handleSelectionCancelled()

	default:
		return a, a.forwardToActiveView(msg)
	}
}

// handleWorkspaceScanComplete updates the workspaces view with its results and
// brings them up — unless the user is in the security view. Switching there
// would route the security scan's own completion to the wrong view and leave
// its spinner running forever.
func (a *App) handleWorkspaceScanComplete(msg workspaces.WorkspaceScanCompleteMsg) (tea.Model, tea.Cmd) {
	view, ok := a.views[command.ViewWorkspaces]
	if !ok {
		return a, nil
	}
	updatedView, cmd := view.Update(msg)
	a.views[command.ViewWorkspaces] = updatedView
	if a.currentView != command.ViewSecurity {
		a.currentView = command.ViewWorkspaces
	}
	return a, cmd
}
