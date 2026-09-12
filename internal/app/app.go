package app

import (
	"log"
	"net/http"
	"os"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/credentials"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/ui/configuration"
	"github.com/anthnel/devdesk/internal/ui/forge/auth"
	"github.com/anthnel/devdesk/internal/ui/forge/explorer"
	"github.com/anthnel/devdesk/internal/ui/netdiag"
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

// LeavingView is implemented by a view that holds an edit the user has not
// settled yet, and that would otherwise be lost when the router switches away.
//
// The router had no such point at all: switchView told nobody, so the
// configuration view's focused text field was written only by ↑↓ or tab. A path
// typed and abandoned with ctrl+p went nowhere, and the cached view kept showing
// it (§1.3 D62).
//
// Leave returns the settled view, whatever the settling has to say, and whether
// it may happen. A refusal cancels the switch — the alternative, letting the
// user go and reporting the abandoned value in the footer, cannot report
// anything: the footer belongs to the view, and the view is what leaves.
type LeavingView interface {
	Leave() (tea.Model, tea.Cmd, bool)
}

// FilterBarView is implemented by views that have a visible filter bar.
// When the filter bar is visible, the viewport bottom border corners are
// replaced with T-junction chars (├/┤) to form a closed rectangle.
type FilterBarView interface {
	FilterBarVisible() bool
}

// App is the main model, with a multi-view router
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
	completionInput       string // Change detection

	// Context list overlay
	showContextList    bool
	contextList        []string
	contextSelectedIdx int

	// Help overlay
	showHelp     bool
	helpViewport viewport.Model

	// Selection mode (cross-view browsing)
	selectionReturnView command.ViewType // View to return to after selection

	// Long-running work. The registry is the one bookkeeping of what is
	// running (internal/jobs); the router owns it, and owns the single spinner
	// chain that animates it (D5). Views read a snapshot carried in
	// JobsChangedMsg — see jobs.go.
	jobs        *jobs.Registry
	jobFrameIdx int
	jobTickSeq  int
	jobTicking  bool

	// The MCP server, which lives in this process (§3.61). program is the
	// handle a request needs to reach Update() — set once by AttachProgram,
	// before the loop starts — and the three fields below are what became of
	// the attempt to serve: a running server, the address it actually bound, or
	// the reason there is none. `mcp.enabled: false` is one of those reasons
	// rather than the absence of one.
	program     *tea.Program
	mcpDispatch mcpDispatcher
	mcpServer   *http.Server
	mcpToken    string
	// mcpEpoch numbers the starts, so a report from one the session has moved
	// past is recognised and its listener closed rather than stored.
	mcpEpoch uint64
	// pendingInvocations holds the reply channel of every action call waiting
	// for the identifier of the run it asked for. Mutated from Update alone,
	// like everything else here.
	pendingInvocations map[invocationID]chan mcpStartReply
	mcpAddr            string
	mcpErr             error

	// Dimensions
	width            int
	height           int
	lastFooterHeight int // tracks current view's footer height for re-resize detection
}

func newCommandInput() textinput.Model {
	// Create the input for command mode
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

// New creates a new App, sized to the current terminal.
func New(cfg *config.Config) *App {
	width, height, err := term.GetSize(os.Stdout.Fd())
	if err != nil {
		panic(err)
	}
	app := newWithSize(cfg, width, height)
	app.useSecrets(credentials.Select(app.currentContext, cfg.App.SecretBackend))
	// Resolved here rather than in newWithSize for the same reason as the
	// secret store: it runs exec.LookPath, and a constructor that reaches for
	// the machine is one tests cannot run twice the same way.
	app.resolveContainerEngine()
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
	if _, ok := a.views[command.ViewGitAuth]; ok {
		a.views[command.ViewGitAuth] = a.newAuthView()
	}
}

// newWithSize builds the router at a given size. New() reads that size from the
// terminal; splitting it out keeps the constructor itself free of I/O.
func newWithSize(cfg *config.Config, width, height int) *App {
	currentContext, _ := config.GetCurrentContext()

	// A store is always present: views never have to test for nil.
	// New() replaces this one with the real backend via useSecrets.
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
		jobs:             jobs.New(),
		commandInput:     newCommandInput(),
		completionEngine: command.NewCompletionEngine(),
		width:            width,
		height:           height,
	}

	// Other views are created on demand (lazy loading).
	app.createView(command.ViewDashboard)
	app.createView(command.ViewStatus)
	app.createView(app.currentView)

	// Computes the available height and propagates it to the views.
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

// AttachProgram gives the router the handle an MCP request needs to reach
// Update().
//
// It is called by main() between tea.NewProgram and p.Run(), which is the only
// window where writing a field of the model is safe outside Update: the loop
// has not started, so there is nothing to race with. Anything that needs the
// program later reads it; nothing writes it again.
func (a *App) AttachProgram(p *tea.Program) {
	a.program = p
	a.mcpDispatch = mcpDispatcher{program: p}
}

// Init initializes the application
func (a *App) Init() tea.Cmd {
	// Note: resize() is already called in newWithSize() to initialize the dimensions
	cmds := []tea.Cmd{a.tryAutoLogin(), a.startMCPCmd()}

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

// resize lays out the window. It **iterates**, and that is necessary: the
// footer height is asked of the view (Rule 124), but a view whose footer
// depends on its own size — the dashboard hides its tab line once only one
// remains — answers based on the size it had *before*. A single pass would
// therefore leave it off by one line until the next resize.
// Two passes are enough: the second queries a view that now knows its size.
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
// view its content height. It returns true when the footer height changed
// along the way, i.e. when a second pass says something different.
func (a *App) layoutOnce(width, height int) (changed bool) {
	// Rule 124: footer height is deducted from the viewport (tabs + optional info line)
	footerHeight := a.getFooterHeight()
	a.lastFooterHeight = footerHeight

	headerHeight := lipgloss.Height(a.renderHeader())
	availableHeight := height - headerHeight - footerHeight
	a.viewport.Width = width
	a.viewport.Height = availableHeight - 1 // -1 for the custom title border line

	// Propagate to the active view. The viewport has 1 border (bottom only),
	// the view must know the inner height. A frameless view has none and
	// reclaims the line.
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

	case MCPServerStartedMsg:
		return a.handleMCPServerStarted(msg)

	case mcpJobsRequestMsg:
		return a.handleMCPJobsRequest(msg)

	case mcpStartRequestMsg:
		return a.handleMCPStartRequest(msg)

	case mcpCancelRequestMsg:
		return a.handleMCPCancelRequest(msg)

	case jobs.RefusedMsg:
		return a.handleMCPRefused(msg)

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

	case ForgeAutoLoginMsg:
		return a.handleAutoLoginResult(msg)

	// ── Long-running work ────────────────────────────────────────────────
	// The shared spinner frame. One chain for the whole application, held by
	// the router (D5) — see jobs.go.
	case jobTickMsg:
		return a.handleJobTick(msg)

	// Every message below reports on work already under way, and each is routed
	// to the view that started it rather than to the one on screen. None of
	// them changes the current view: a scan can run for minutes, and dragging
	// the user back to watch it was the whole of D67.
	case jobs.StartMsg:
		return a.handleStartJobs(msg)

	case jobs.CancelOpenMsg:
		return a.handleCancelOpen(msg)

	case jobs.CancelMsg:
		return a.handleCancel(msg)

	case jobs.CancelItemMsg:
		return a.handleCancelItem(msg)

	// The clone reports through the registry like everything else now (D3).
	// Its run is *open*: the walk that discovers repositories is the slow part,
	// so targets arrive as they are found and CloneRunFinishedMsg seals the run
	// rather than merely closing a screen.
	case explorer.CloneEventMsg:
		return a.routeWork(command.ViewGitExplorer, msg)

	case explorer.CloneRunFinishedMsg:
		return a.routeWork(command.ViewGitExplorer, msg)

	// Creating and deleting on a forge are network calls, and the tree used to
	// blank itself for the whole of one. They report like the clone now, so the
	// row spins where the user asked for it and `:jobs` can see the work.
	case explorer.GroupCreatedMsg:
		return a.routeWork(command.ViewGitExplorer, msg)

	case explorer.ProjectCreatedMsg:
		return a.routeWork(command.ViewGitExplorer, msg)

	case explorer.DeleteCompleteMsg:
		return a.routeWork(command.ViewGitExplorer, msg)

	case workspaces.WorkspaceScanStartingMsg:
		return a.routeWork(command.ViewWorkspaces, msg)

	case workspaces.WorkspaceScanCompleteMsg:
		return a.routeWork(command.ViewWorkspaces, msg)

	case workspaces.WorkspaceSyncStartingMsg:
		return a.routeWork(command.ViewWorkspaces, msg)

	case workspaces.WorkspaceSyncCompleteMsg:
		return a.routeWork(command.ViewWorkspaces, msg)

	case workspaces.EntryDeletedMsg:
		// A delete is confirmed in a modal and then runs on its own; the marker
		// it sets is cleared here or not at all.
		return a.routeWork(command.ViewWorkspaces, msg)

	case ociresources.ImageScanStartingMsg:
		return a.routeWork(command.ViewOCIResources, msg)

	case ociresources.ImageScanFinishedMsg:
		return a.routeWork(command.ViewOCIResources, msg)

	// A pull is work like the rest (§3.60). It reached the OCI view through
	// the default forward before, which delivered it only while that view was
	// the active one — so walking away mid-pull dropped the completion and left
	// the row spinning, on top of the registry never hearing about the run.
	case ociresources.RegistryPullStartingMsg:
		return a.routeWork(command.ViewOCIResources, msg)

	case ociresources.RegistryPullCompleteMsg:
		return a.routeWork(command.ViewOCIResources, msg)

	case security.InventoryScanStartingMsg:
		return a.routeWork(command.ViewSecurity, msg)

	case security.InventoryScanFinishedMsg:
		return a.routeWork(command.ViewSecurity, msg)

	// ── Scan results ─────────────────────────────────────────────────────
	case workspaces.ScanDetailsRequestMsg:
		return a.handleWorkspaceScanDetails(msg)

	case WorkspaceScanResultLoadedMsg:
		return a.handleWorkspaceScanResultLoaded(msg)

	case ociresources.ScanDetailsRequestMsg:
		return a.handleScanDetailsRequest(msg)

	case ImageScanResultLoadedMsg:
		return a.handleImageScanResultLoaded(msg)

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

	// ── Network diagnostics, opened prefilled from elsewhere ─────────────
	// status's H (§3.66) is the first producer; wired the same way as the
	// viewer and the security view above.
	case netdiag.OpenRequestMsg:
		return a.handleNetdiagOpenRequest(msg)

	case netdiag.BackToOriginMsg:
		return a, a.switchView(msg.Origin)

	default:
		return a, a.forwardToActiveView(msg)
	}
}

// routeToView hands a message to a named view whether or not it is on screen,
// without changing which view is. Work started in a view has to finish there:
// its progress markers, its spinner and its cache write all live in its model,
// so a message dropped because the user walked away leaves a row marked busy
// for the life of the view.
//
// It does not re-measure the layout the way forwardToActiveView does — a view
// the user is not looking at cannot change the footer's shape on screen, and
// it is measured on its way back in (switchView asks for a resize).
func (a *App) routeToView(target command.ViewType, msg tea.Msg) (tea.Model, tea.Cmd) {
	view, ok := a.views[target]
	if !ok {
		return a, nil
	}
	updatedView, cmd := view.Update(msg)
	a.views[target] = updatedView
	return a, cmd
}
