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
	"github.com/anthnel/devdesk/internal/credentials"
	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/ui/configuration"
	"github.com/anthnel/devdesk/internal/ui/gitlab/auth"
	"github.com/anthnel/devdesk/internal/ui/gitlab/explorer"
	ociresources "github.com/anthnel/devdesk/internal/ui/oci_resources"
	"github.com/anthnel/devdesk/internal/ui/security"
	"github.com/anthnel/devdesk/internal/ui/theme"
	uiviewer "github.com/anthnel/devdesk/internal/ui/viewer"
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

// framelessViewportStyle is the viewport style for a view that draws its own
// frames (FramelessView): same background, no border.
func framelessViewportStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Background(theme.ColorBackground).
		Foreground(theme.ColorText)
}

// New crée une nouvelle App, dimensionnée sur le terminal courant.
func New(cfg *config.Config) *App {
	width, height, err := term.GetSize(os.Stdout.Fd())
	if err != nil {
		panic(err)
	}
	app := newWithSize(cfg, width, height)
	app.useSecrets(credentials.Select(app.currentContext, cfg.App.SecretBackend))
	return app
}

// useSecrets points the router at a resolved secret store and sweeps whatever
// plaintext an earlier version left in this context's configuration file.
//
// It is separate from newWithSize because resolving a backend is I/O — a probe
// of the host store and a read of git's configuration — and a constructor that
// reaches for the machine's keyring is one tests cannot run twice the same way.
func (a *App) useSecrets(sel credentials.Selection) {
	a.sharedState.Secrets = sel
	a.sharedState.SecretNotices = credentials.MigrateLegacySecrets(sel.Storage, a.currentContext)
	log.Printf("Secret backend for context %q: %s", a.currentContext, sel.Backend)

	// The auth view holds the storage it was built with, so rebuild it if it
	// already exists. Every other view reaches secrets through the router.
	if _, ok := a.views[command.ViewGitlabAuth]; ok {
		a.views[command.ViewGitlabAuth] = a.newAuthView()
	}
}

// newWithSize builds the router at a given size. New() reads that size from the
// terminal; splitting it out keeps the constructor itself free of I/O.
func newWithSize(cfg *config.Config, width, height int) *App {
	currentContext, _ := config.GetCurrentContext()

	// Un store est toujours présent : les vues n'ont jamais à tester le nil.
	// New() remplace celui-ci par le backend réel via useSecrets.
	sharedState := &shared.State{
		Secrets: credentials.SessionOnly("no secret store has been resolved yet"),
	}

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

// resize lays out the window. Il **itère**, et c'est nécessaire : la hauteur du
// footer est demandée à la vue (Rule 124), mais une vue dont le footer dépend
// de sa taille — le dashboard cache sa ligne d'onglets quand il n'en reste
// qu'un — répond d'après la taille qu'elle avait *avant*. Une seule passe la
// laisserait donc décalée d'une ligne jusqu'au redimensionnement suivant.
// Deux passes suffisent : la seconde interroge une vue qui connaît sa taille.
func (a *App) resize(width, height int) {
	a.width = width
	a.height = height

	const passes = 2
	for range passes {
		if !a.layoutOnce(width, height) {
			return
		}
	}
}

// layoutOnce sizes the viewport against the active view's footer and hands the
// view its content height. Il retourne true quand la hauteur du footer a changé
// en cours de route, c'est-à-dire quand une seconde passe dit autre chose.
func (a *App) layoutOnce(width, height int) (changed bool) {
	// Rule 124: footer height is deducted from the viewport (tabs + optional info line)
	footerHeight := a.getFooterHeight()
	a.lastFooterHeight = footerHeight

	headerHeight := lipgloss.Height(a.renderHeader())
	availableHeight := height - headerHeight - footerHeight
	a.viewport.Width = width
	a.viewport.Height = availableHeight - 1 // -1 for the custom title border line

	// Propager à la vue active. Le viewport a 1 bordure (bottom only), la vue
	// doit connaître la hauteur intérieure. Une vue sans cadre n'en a aucune et
	// récupère la ligne.
	viewportBorderHeight := 1
	a.viewport.Style = viewportStyle()
	if a.frameless() {
		viewportBorderHeight = 0
		a.viewport.Style = framelessViewportStyle()
	}
	contentHeight := max((availableHeight-1)-viewportBorderHeight, 1)
	if view, ok := a.views[a.currentView]; ok {
		updatedView, _ := view.Update(tea.WindowSizeMsg{Width: width, Height: contentHeight})
		a.views[a.currentView] = updatedView
	}

	return a.getFooterHeight() != footerHeight
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
	case configuration.ConfigSavedMsg:
		return a.handleConfigSaved(msg)

	case ContextSwitchCompleteMsg:
		return a.handleContextSwitchComplete(msg)

	case ContextSwitchErrorMsg:
		log.Printf("ERROR: Context operation failed: %v", msg.Error)
		return a, nil

	case ContextListMsg:
		return a.handleContextList(msg)

	// ── GitLab authentication ────────────────────────────────────────────
	case auth.AuthResultMsg:
		return a.handleAuthResult(msg)

	case auth.LogoutCompleteMsg:
		return a.handleLogoutComplete(msg)

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

	case security.InventoryScanFinishedMsg:
		// A rescan started from the inventory belongs to it wherever the user
		// has gone. Forwarded rather than dropped: the row is marked as
		// scanning, and a reload deliberately keeps that marker, so a lost
		// completion leaves it spinning for the life of the view.
		return a.routeToSecurityView(msg)

	// ── Selection mode ───────────────────────────────────────────────────
	// Only the explorer borrows a view now, and only the workspaces one: the
	// security form was the other borrower, and picking a scan target went with
	// it (phase 3).
	case security.BackToOriginMsg:
		return a, a.switchView(msg.Origin)

	case explorer.CloneSelectionRequestMsg:
		return a.handleExplorerCloneRequest()

	case workspaces.DirectorySelectedMsg:
		return a.handleDirectorySelected(msg)

	case workspaces.SelectionCancelledMsg:
		return a.handleSelectionCancelled()

	// ── The document viewer ──────────────────────────────────────────────
	// One message, three producers: a file in workspaces, an inspect and a log
	// in containers. The viewer is a destination like the security view, and is
	// wired the same way.
	case uiviewer.OpenRequestMsg:
		return a.handleViewerOpenRequest(msg)

	case uiviewer.BackToOriginMsg:
		return a, a.switchView(msg.Origin)

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
