package app

import (
	"log"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"
	gitlabclient "gitlab.com/gitlab-org/api/client-go"

	"gitlab.com/anthnell/devsecops/devdesk/internal/cache"
	"gitlab.com/anthnell/devsecops/devdesk/internal/command"
	"gitlab.com/anthnell/devsecops/devdesk/internal/config"
	"gitlab.com/anthnell/devsecops/devdesk/internal/credentials"
	"gitlab.com/anthnell/devsecops/devdesk/internal/gitlab"
	"gitlab.com/anthnell/devsecops/devdesk/internal/shared"
	"gitlab.com/anthnell/devsecops/devdesk/internal/ui/containers"
	"gitlab.com/anthnell/devsecops/devdesk/internal/ui/dashboard"
	"gitlab.com/anthnell/devsecops/devdesk/internal/ui/gitlab/auth"
	"gitlab.com/anthnell/devsecops/devdesk/internal/ui/gitlab/explorer"
	"gitlab.com/anthnell/devsecops/devdesk/internal/ui/help"
	"gitlab.com/anthnell/devsecops/devdesk/internal/ui/netdiag"
	ociresources "gitlab.com/anthnell/devsecops/devdesk/internal/ui/oci_resources"
	"gitlab.com/anthnell/devsecops/devdesk/internal/ui/security"
	"gitlab.com/anthnell/devsecops/devdesk/internal/ui/status"
	"gitlab.com/anthnell/devsecops/devdesk/internal/ui/theme"
	"gitlab.com/anthnell/devsecops/devdesk/internal/ui/workspaces"
)

// FormView interface for views that can have active forms
// Views implementing this interface can prevent command mode activation
type FormView interface {
	InEditMode() bool
}

// CommandModeView is an optional interface for views that want to allow
// entering command mode even while InEditMode() returns true.
// When implemented and AllowCommandMode() returns true, pressing ":" will
// enter command mode instead of being forwarded to the active text input.
type CommandModeView interface {
	AllowCommandMode() bool
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
	vp.Style = lipgloss.NewStyle().
		Background(theme.ColorBackground).
		Border(lipgloss.NormalBorder(), false, true, true, true).
		BorderForeground(theme.ColorViewportBorder).
		BorderBackground(theme.ColorBackground)
	return vp
}

// New crée une nouvelle App
func New(cfg *config.Config) *App {
	// Récupérer le contexte actuel
	currentContext, _ := config.GetCurrentContext()

	// Créer le shared state
	sharedState := &shared.State{}

	// Créer les vues
	views := make(map[command.ViewType]tea.Model)

	// Les autres vues seront créées au besoin (lazy loading)
	// ou créées ici si elles doivent être initialisées au démarrage

	// Vue par défaut
	defaultView := command.ViewDashboard
	if cfg.App.DefaultView != "" {
		// Parser la vue par défaut depuis la config
		if parsed, err := command.Parse(cfg.App.DefaultView); err == nil && parsed != "" {
			defaultView = parsed
		}
	}

	// Créer les vues initiales
	views[command.ViewDashboard] = dashboard.New(cfg, sharedState)
	views[command.ViewStatus] = status.New(cfg)

	// Récupérer les dimensions du terminal
	width, height, err := term.GetSize(os.Stdout.Fd())
	if err != nil {
		panic(err)
	}

	app := &App{
		config:           cfg,
		currentContext:   currentContext,
		viewport:         newViewport(width, height),
		currentView:      defaultView,
		views:            views,
		sharedState:      sharedState,
		commandMode:      false,
		commandInput:     newCommandInput(),
		completionEngine: command.NewCompletionEngine(),
		completionIndex:  0,
		width:            width,
		height:           height,
	}

	// Appeler resize pour initialiser correctement les dimensions
	// Ceci calcule la hauteur disponible et propage aux vues
	app.resize(width, height)

	return app
}

// Context switching messages

// ContextSwitchCompleteMsg signale le succès du switch de contexte
type ContextSwitchCompleteMsg struct {
	ContextName  string
	Config       *config.Config
	Created      bool // true si le contexte a été créé automatiquement
	GitLabClient *gitlabclient.Client
	GitLabUser   *gitlabclient.User
}

// ContextSwitchErrorMsg signale une erreur lors du switch
type ContextSwitchErrorMsg struct {
	Error error
}

// ContextListMsg contient la liste des contextes disponibles
type ContextListMsg struct {
	Contexts []string
	Current  string
}

// ThemeListMsg contient la liste des thèmes disponibles
type ThemeListMsg struct {
	Themes  []string
	Current string
}

// ThemeAppliedMsg signale qu'un thème a été appliqué avec succès
type ThemeAppliedMsg struct {
	ThemeName string
}

// ThemeErrorMsg signale une erreur liée aux thèmes
type ThemeErrorMsg struct {
	Error error
}

// GitLabAutoLoginMsg signale le résultat de l'auto-login
type GitLabAutoLoginMsg struct {
	Client *gitlabclient.Client
	User   *gitlabclient.User
	Error  error
}

// Init initialise l'application
func (a *App) Init() tea.Cmd {
	// Note: resize() est déjà appelé dans New() pour initialiser les dimensions

	// Commandes à exécuter au démarrage
	var cmds []tea.Cmd

	// Tenter l'auto-login GitLab
	cmds = append(cmds, a.tryAutoLogin())

	// Initialiser la vue courante
	if view, ok := a.views[a.currentView]; ok {
		cmds = append(cmds, view.Init())
	}

	return tea.Batch(cmds...)
}

// tryAutoLogin tente de se connecter automatiquement à GitLab avec les credentials sauvegardés
func (a *App) tryAutoLogin() tea.Cmd {
	url := a.config.GitLab.URL
	currentContext := a.currentContext

	// Si pas d'URL configurée, pas d'auto-login
	if url == "" {
		return nil
	}

	return func() tea.Msg {
		storage := credentials.NewChainStorage(
			credentials.NewFileStorageForContext(currentContext),
			credentials.NewGitCredentialStorageWithContext(currentContext),
		)
		auth := gitlab.NewAuth(storage)

		// Essayer de charger le token depuis le storage
		token, err := auth.LoadCredentials(url)
		if err != nil || token == "" {
			// Essayer de charger depuis la config en fallback
			token = a.config.GitLab.Token
			if token == "" {
				// Pas de credentials disponibles, pas d'auto-login
				return GitLabAutoLoginMsg{Client: nil, User: nil, Error: nil}
			}
		}

		// Authentifier avec le token trouvé
		result, err := auth.Authenticate(url, token, false) // false = ne pas re-sauvegarder
		if err != nil {
			log.Printf("Auto-login failed: %v", err)
			return GitLabAutoLoginMsg{Client: nil, User: nil, Error: err}
		}

		log.Printf("Auto-login successful: %s", result.User.Username)
		return GitLabAutoLoginMsg{
			Client: result.Client,
			User:   result.User,
			Error:  nil,
		}
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
	a.viewport.Style = lipgloss.NewStyle().
		Background(theme.ColorBackground).
		Foreground(theme.ColorText).
		Border(lipgloss.NormalBorder(), false, true, true, true).
		BorderForeground(theme.ColorViewportBorder).
		BorderBackground(theme.ColorBackground)

	// Propager à la vue active.
	// Le viewport a 1 bordure (bottom only), la vue doit connaître la hauteur intérieure.
	viewportBorderHeight := 1
	contentHeight := (availableHeight - 1) - viewportBorderHeight
	if contentHeight < 1 {
		contentHeight = 1
	}
	if view, ok := a.views[a.currentView]; ok {
		adjustedMsg := tea.WindowSizeMsg{Width: width, Height: contentHeight}
		updatedView, _ := view.Update(adjustedMsg)
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

// replaceViewportBottomCorners replaces the bottom-left └ and bottom-right ┘ of the
// viewport's bottom border with T-junction chars ├ and ┤, so the filter bar (rendered
// in the footer) visually connects with the viewport border to form a closed rectangle.
func replaceViewportBottomCorners(body string) string {
	lastNewline := strings.LastIndex(body, "\n")
	if lastNewline == -1 {
		return body
	}
	lastLine := body[lastNewline+1:]
	// Replace first └ with ├ and last ┘ with ┤ in the bottom border line.
	lastLine = strings.Replace(lastLine, "└", "├", 1)
	lastLine = strings.Replace(lastLine, "┘", "┤", 1)
	return body[:lastNewline+1] + lastLine
}

// switchContext handles context switching with auto-creation
func (a *App) switchContext(contextName string) tea.Cmd {
	return func() tea.Msg {
		log.Printf("Context switch requested: %s", contextName)

		// Valider le nom du contexte
		if err := config.ValidateContextName(contextName); err != nil {
			log.Printf("ERROR: Context validation failed: %v", err)
			return ContextSwitchErrorMsg{Error: err}
		}

		// Vérifier si le contexte existe
		exists, err := config.ContextExists(contextName)
		if err != nil {
			log.Printf("ERROR: Failed to check context existence: %v", err)
			return ContextSwitchErrorMsg{Error: err}
		}

		// Si le contexte n'existe pas, le créer automatiquement
		created := false
		if !exists {
			log.Printf("Context '%s' does not exist, creating automatically", contextName)
			if err := config.CreateContext(contextName); err != nil {
				log.Printf("ERROR: Failed to create context '%s': %v", contextName, err)
				return ContextSwitchErrorMsg{Error: err}
			}
			created = true
			log.Printf("Context '%s' created successfully", contextName)
		}

		// Charger la configuration du contexte
		cfg, err := config.LoadContext(contextName)
		if err != nil {
			log.Printf("ERROR: Failed to load context '%s': %v", contextName, err)
			return ContextSwitchErrorMsg{Error: err}
		}

		// Persister le changement de contexte
		if err := config.SetCurrentContext(contextName); err != nil {
			log.Printf("ERROR: Failed to persist context switch: %v", err)
			return ContextSwitchErrorMsg{Error: err}
		}

		// Attempt auto-login with the new context's credentials
		var glClient *gitlabclient.Client
		var glUser *gitlabclient.User
		if cfg.GitLab.URL != "" {
			storage := credentials.NewChainStorage(
				credentials.NewFileStorageForContext(contextName),
				credentials.NewGitCredentialStorageWithContext(contextName),
			)
			gitlabAuth := gitlab.NewAuth(storage)
			token, err := gitlabAuth.LoadCredentials(cfg.GitLab.URL)
			if err != nil || token == "" {
				token = cfg.GitLab.Token
			}
			if token != "" {
				result, err := gitlabAuth.Authenticate(cfg.GitLab.URL, token, false)
				if err == nil {
					glClient = result.Client
					glUser = result.User
					log.Printf("Auto-login successful for context '%s': %s", contextName, glUser.Username)
				} else {
					log.Printf("Auto-login failed for context '%s': %v", contextName, err)
				}
			}
		}

		log.Printf("Context switch successful: %s (created: %v)", contextName, created)
		return ContextSwitchCompleteMsg{
			ContextName:  contextName,
			Config:       cfg,
			Created:      created,
			GitLabClient: glClient,
			GitLabUser:   glUser,
		}
	}
}

// listContexts retrieves available contexts
func (a *App) listContexts() tea.Cmd {
	return func() tea.Msg {
		log.Printf("Listing available contexts")
		contexts, err := config.ListContexts()
		if err != nil {
			log.Printf("ERROR: Failed to list contexts: %v", err)
			return ContextSwitchErrorMsg{Error: err}
		}

		current, _ := config.GetCurrentContext()
		if current == "" {
			current = "default"
		}

		log.Printf("Found %d contexts, current: %s", len(contexts), current)
		return ContextListMsg{
			Contexts: contexts,
			Current:  current,
		}
	}
}

// handleKeyMsg processes keyboard input
func (a *App) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Si un overlay est affiché, gérer la navigation
	if a.showHelp {
		return a.handleHelpKeyMsg(msg)
	}
	if a.showContextList {
		return a.handleContextListKeyMsg(msg)
	}
	if a.showThemeList {
		return a.handleThemeListKeyMsg(msg)
	}

	if a.commandMode {
		return a.handleCommandMode(msg)
	}

	switch msg.String() {
	case "ctrl+c":
		return a, tea.Quit
	case "q":
		cmd := a.maybeQuitApplication(msg)
		return a, cmd
	case "?":
		return a.maybeOpenHelp(msg)
	case ":":
		cmd := a.maybeEnterInCommandMode(msg)
		return a, cmd
	case "esc":
		cmd := a.maybeQuitCommandMode(msg)
		// Rule 124: re-resize if a form was closed and footer height changed
		if newFooterHeight := a.getFooterHeight(); newFooterHeight != a.lastFooterHeight {
			a.resize(a.width, a.height)
		}
		return a, cmd
	default:
		if view, ok := a.views[a.currentView]; ok {
			updatedView, cmd := view.Update(msg)
			a.views[a.currentView] = updatedView
			// Re-resize if the view's footer height changed (e.g., filter bar toggled).
			if newFooterHeight := a.getFooterHeight(); newFooterHeight != a.lastFooterHeight {
				a.resize(a.width, a.height)
			}
			return a, cmd
		}
		return a, nil
	}
}

// handleContextSwitchComplete processes successful context switches
func (a *App) handleContextSwitchComplete(msg ContextSwitchCompleteMsg) (tea.Model, tea.Cmd) {
	a.config = msg.Config
	a.currentContext = msg.ContextName

	// Reset GitLab auth state — the new context has its own credentials
	a.sharedState.GitLabClient = nil
	a.sharedState.IsAuthenticated = false
	a.sharedState.CurrentUser = nil
	a.sharedState.CachedGroups = nil
	a.sharedState.CachedProjects = nil
	a.sharedState.GitLabStats = nil

	// Apply auto-login result from the context switch if successful
	if msg.GitLabClient != nil && msg.GitLabUser != nil {
		a.sharedState.GitLabClient = msg.GitLabClient
		a.sharedState.CurrentUser = msg.GitLabUser
		a.sharedState.IsAuthenticated = true
	} else {
		// No credentials available — navigate to auth view for manual login
		a.currentView = command.ViewGitlabAuth
	}

	// Reinitialize views with new config; auth state is already populated above
	initCmd := a.reinitializeViews(msg.Config)

	windowSizeCmd := func() tea.Msg {
		return tea.WindowSizeMsg{Width: a.width, Height: a.height}
	}
	return a, tea.Batch(windowSizeCmd, initCmd)
}

// handleAuthResult handles GitLab authentication results
func (a *App) handleAutoLoginResult(msg GitLabAutoLoginMsg) (tea.Model, tea.Cmd) {
	// Si l'auto-login a échoué ou aucun client, ignorer silencieusement
	if msg.Error != nil || msg.Client == nil {
		return a, nil
	}

	// Auto-login réussi
	log.Printf("GitLab auto-login successful for user: %s", msg.User.Username)

	// Mettre à jour le shared state
	a.sharedState.GitLabClient = msg.Client
	a.sharedState.CurrentUser = msg.User
	a.sharedState.IsAuthenticated = true

	// Pas de sauvegarde de config (déjà sauvegardée lors de l'authentification manuelle)

	return a, nil
}

func (a *App) handleAuthResult(msg auth.AuthResultMsg) (tea.Model, tea.Cmd) {
	if msg.Error != nil {
		// L'erreur est déjà gérée par la vue, juste transmettre le message
		if view, ok := a.views[a.currentView]; ok {
			updatedView, cmd := view.Update(msg)
			a.views[a.currentView] = updatedView
			return a, cmd
		}
		return a, nil
	}

	// Authentification réussie
	log.Printf("GitLab authentication successful for user: %s", msg.User.Username)

	// Mettre à jour le shared state
	a.sharedState.GitLabClient = msg.Client
	a.sharedState.CurrentUser = msg.User
	a.sharedState.IsAuthenticated = true

	// Sauvegarder la configuration sur le disque
	if msg.ConfigToSave != nil {
		if err := config.Save(msg.ConfigToSave); err != nil {
			log.Printf("ERROR: Failed to save config after authentication: %v", err)
		} else {
			log.Printf("Config saved successfully (URL: %s, TokenSaved: %v)",
				msg.ConfigToSave.GitLab.URL, msg.SaveToConfig)
		}
	}

	// Transmettre le message à la vue pour mise à jour de l'UI
	if view, ok := a.views[a.currentView]; ok {
		updatedView, cmd := view.Update(msg)
		a.views[a.currentView] = updatedView
		return a, cmd
	}

	return a, nil
}

// handleWorkspaceScanDetails loads a cached scan result from disk and opens the security details view
func (a *App) handleWorkspaceScanDetails(msg workspaces.ScanDetailsRequestMsg) (tea.Model, tea.Cmd) {
	repoPath := msg.RepoPath
	return a, func() tea.Msg {
		result, err := cache.LoadWorkspaceScanResult(repoPath)
		return WorkspaceScanResultLoadedMsg{Result: result, RepoPath: repoPath, Err: err}
	}
}

// handleWorkspaceScanResultLoaded opens the security view with the loaded result.
// If the full result file is missing (cache inconsistency), opens in StateInput with the
// target pre-filled so the user can trigger a fresh scan instead of failing silently.
func (a *App) handleWorkspaceScanResultLoaded(msg WorkspaceScanResultLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil || msg.Result == nil {
		log.Printf("Workspace scan result not on disk for %s (fallback to input): %v", msg.RepoPath, msg.Err)
		secView := security.NewWithTargetReturnToWorkspaces(a.config, msg.RepoPath)
		secView.OriginView = command.ViewWorkspaces
		a.views[command.ViewSecurity] = secView
		a.currentView = command.ViewSecurity
		return a, tea.Batch(
			a.views[command.ViewSecurity].Init(),
			func() tea.Msg { return tea.WindowSizeMsg{Width: a.width, Height: a.height} },
		)
	}
	secView := security.NewWithPreloadedResult(a.config, msg.Result)
	secView.OriginView = command.ViewWorkspaces
	a.views[command.ViewSecurity] = secView
	a.currentView = command.ViewSecurity
	return a, tea.Batch(
		a.views[command.ViewSecurity].Init(),
		func() tea.Msg { return tea.WindowSizeMsg{Width: a.width, Height: a.height} },
	)
}

// routeToOCIImagesView forwards a message to the OCI images view even when it is not the active view.
// This keeps the scan state up to date while the user navigates elsewhere.
func (a *App) routeToOCIImagesView(msg tea.Msg) (tea.Model, tea.Cmd) {
	if view, ok := a.views[command.ViewOCIResources]; ok {
		updatedView, cmd := view.Update(msg)
		a.views[command.ViewOCIResources] = updatedView
		return a, cmd
	}
	return a, nil
}

// handleImageScanRequest is no longer needed as oci_resources view performs direct scans

// handleLaunchBatchScan switches back to OCI images view and forwards the batch scan request.
func (a *App) handleLaunchBatchScan(msg ociresources.LaunchBatchScanMsg) (tea.Model, tea.Cmd) {
	log.Printf("Launching batch scan, switching back to OCI images view")

	if _, exists := a.views[command.ViewOCIResources]; !exists {
		a.createView(command.ViewOCIResources)
	}
	a.currentView = command.ViewOCIResources

	if view, ok := a.views[command.ViewOCIResources]; ok {
		updatedView, cmd := view.Update(msg)
		a.views[command.ViewOCIResources] = updatedView
		return a, tea.Batch(cmd, func() tea.Msg {
			return tea.WindowSizeMsg{Width: a.width, Height: a.height}
		})
	}
	return a, nil
}

// handleLaunchSingleImageScan switches back to OCI images view and forwards the single scan request.
func (a *App) handleLaunchSingleImageScan(msg ociresources.LaunchSingleImageScanMsg) (tea.Model, tea.Cmd) {
	log.Printf("Launching single image scan for %s, switching back to OCI images view", msg.ImageName)

	if _, exists := a.views[command.ViewOCIResources]; !exists {
		a.createView(command.ViewOCIResources)
	}
	a.currentView = command.ViewOCIResources

	if view, ok := a.views[command.ViewOCIResources]; ok {
		updatedView, cmd := view.Update(msg)
		a.views[command.ViewOCIResources] = updatedView
		return a, tea.Batch(cmd, func() tea.Msg {
			return tea.WindowSizeMsg{Width: a.width, Height: a.height}
		})
	}
	return a, nil
}

// handleScanDetailsRequest loads a cached image scan result from disk and opens the security details view.
// Rule 126: Enter on a scanned image loads from cache — no new scan is triggered.
func (a *App) handleScanDetailsRequest(msg ociresources.ScanDetailsRequestMsg) (tea.Model, tea.Cmd) {
	log.Printf("Scan details requested for image: %s", msg.ImageName)
	imageName := msg.ImageName
	return a, func() tea.Msg {
		result, err := cache.LoadImageScanResult(imageName)
		return ImageScanResultLoadedMsg{Result: result, ImageName: imageName, Err: err}
	}
}

// handleImageScanResultLoaded opens the security view in StateDetails with the loaded result.
// Falls back to re-scanning when the full result file is missing (e.g. scanned before result
// persistence was introduced).
func (a *App) handleImageScanResultLoaded(msg ImageScanResultLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil || msg.Result == nil {
		// Full result file missing: fall back to scan mode so the user is not left with nothing
		log.Printf("Image scan result not on disk for %s (fallback to scan): %v", msg.ImageName, msg.Err)
		secView := security.NewWithImageTarget(a.config, msg.ImageName, false)
		secView.OriginView = command.ViewOCIResources
		a.views[command.ViewSecurity] = secView
		a.currentView = command.ViewSecurity
		return a, tea.Batch(
			a.views[command.ViewSecurity].Init(),
			func() tea.Msg { return security.StartScanMsg{} },
			func() tea.Msg { return tea.WindowSizeMsg{Width: a.width, Height: a.height} },
		)
	}
	secView := security.NewWithPreloadedResult(a.config, msg.Result)
	secView.OriginView = command.ViewOCIResources
	a.views[command.ViewSecurity] = secView
	a.currentView = command.ViewSecurity
	return a, tea.Batch(
		a.views[command.ViewSecurity].Init(),
		func() tea.Msg { return tea.WindowSizeMsg{Width: a.width, Height: a.height} },
	)
}

// handleSelectionRequest switches to workspaces or oci_resources view in selection mode
func (a *App) handleSelectionRequest(msg security.SelectionRequestMsg) (tea.Model, tea.Cmd) {
	a.selectionReturnView = a.currentView

	switch msg.Type {
	case "directory":
		log.Printf("Selection request: switching to workspaces for directory selection")
		a.views[command.ViewWorkspaces] = workspaces.NewForSelection(a.config, msg.Message)
		a.currentView = command.ViewWorkspaces
		return a, tea.Batch(
			a.views[command.ViewWorkspaces].Init(),
			func() tea.Msg {
				return tea.WindowSizeMsg{Width: a.width, Height: a.height}
			},
		)
	case "image":
		log.Printf("Selection request: switching to OCI images for image selection")
		a.views[command.ViewOCIResources] = ociresources.NewForSelection(a.config, msg.Message)
		a.currentView = command.ViewOCIResources
		return a, tea.Batch(
			a.views[command.ViewOCIResources].Init(),
			func() tea.Msg {
				return tea.WindowSizeMsg{Width: a.width, Height: a.height}
			},
		)
	}
	return a, nil
}

// handleExplorerPullRequest switches to workspaces view in selection mode for pull destination
func (a *App) handleExplorerPullRequest() (tea.Model, tea.Cmd) {
	a.selectionReturnView = a.currentView
	log.Printf("Explorer pull request: switching to workspaces for directory selection")
	a.views[command.ViewWorkspaces] = workspaces.NewForSelection(a.config, "Enter into a parent directory — the repo will be cloned inside it")
	a.currentView = command.ViewWorkspaces
	return a, tea.Batch(
		a.views[command.ViewWorkspaces].Init(),
		func() tea.Msg {
			return tea.WindowSizeMsg{Width: a.width, Height: a.height}
		},
	)
}

// handleDirectorySelected returns to the originating view with the selected directory path
func (a *App) handleDirectorySelected(msg workspaces.DirectorySelectedMsg) (tea.Model, tea.Cmd) {
	log.Printf("Directory selected: %s, returning to %s", msg.Path, a.selectionReturnView)
	a.currentView = a.selectionReturnView

	// Force recreation of workspaces view next time it's accessed to reset selection mode
	delete(a.views, command.ViewWorkspaces)

	if view, ok := a.views[a.currentView]; ok {
		var resultMsg tea.Msg
		if a.selectionReturnView == command.ViewGitlabExplorer {
			resultMsg = explorer.PullDestinationSelectedMsg{Path: msg.Path}
		} else {
			resultMsg = security.SelectionResultMsg{Path: msg.Path}
		}
		updatedView, cmd := view.Update(resultMsg)
		a.views[a.currentView] = updatedView
		a.viewport.GotoTop()
		return a, tea.Batch(cmd, func() tea.Msg {
			return tea.WindowSizeMsg{Width: a.width, Height: a.height}
		})
	}
	return a, nil
}

// handleImageSelected returns to security view with the selected image name
func (a *App) handleImageSelected(msg ociresources.ImageSelectedMsg) (tea.Model, tea.Cmd) {
	log.Printf("Image selected: %s, returning to %s", msg.ImageName, a.selectionReturnView)
	a.currentView = a.selectionReturnView

	// Reset selection mode without losing scan state
	if view, ok := a.views[command.ViewOCIResources]; ok {
		updatedView, _ := view.Update(ociresources.ResetSelectionMsg{})
		a.views[command.ViewOCIResources] = updatedView
	}

	if view, ok := a.views[a.currentView]; ok {
		updatedView, cmd := view.Update(security.SelectionResultMsg{Path: msg.ImageName})
		a.views[a.currentView] = updatedView
		return a, tea.Batch(cmd, func() tea.Msg {
			return tea.WindowSizeMsg{Width: a.width, Height: a.height}
		})
	}
	return a, nil
}

// handleSelectionCancelled returns to the originating view without changes
func (a *App) handleSelectionCancelled() (tea.Model, tea.Cmd) {
	log.Printf("Selection cancelled, returning to %s", a.selectionReturnView)
	a.currentView = a.selectionReturnView

	// Reset selection mode without losing scan state; workspaces has no ongoing state to preserve
	delete(a.views, command.ViewWorkspaces)
	if view, ok := a.views[command.ViewOCIResources]; ok {
		updatedView, _ := view.Update(ociresources.ResetSelectionMsg{})
		a.views[command.ViewOCIResources] = updatedView
	}

	if view, ok := a.views[a.currentView]; ok {
		var cancelMsg tea.Msg
		if a.selectionReturnView == command.ViewGitlabExplorer {
			cancelMsg = explorer.PullSelectionCancelledMsg{}
		} else {
			cancelMsg = security.SelectionCancelledMsg{}
		}
		updatedView, cmd := view.Update(cancelMsg)
		a.views[a.currentView] = updatedView
		a.viewport.GotoTop()
		return a, tea.Batch(cmd, func() tea.Msg {
			return tea.WindowSizeMsg{Width: a.width, Height: a.height}
		})
	}
	return a, nil
}

// listThemes retourne la liste des thèmes disponibles
func (a *App) listThemes() tea.Cmd {
	return func() tea.Msg {
		log.Printf("Listing available themes")
		themes, err := theme.ListThemes()
		if err != nil {
			log.Printf("ERROR: Failed to list themes: %v", err)
			return ThemeErrorMsg{Error: err}
		}

		return ThemeListMsg{
			Themes:  themes,
			Current: theme.CurrentThemeName,
		}
	}
}

// applyTheme charge et applique un thème par nom
func (a *App) applyTheme(name string) tea.Cmd {
	cfgRef := a.config
	return func() tea.Msg {
		log.Printf("Applying theme: %s", name)
		t, err := theme.LoadTheme(name)
		if err != nil {
			log.Printf("ERROR: Failed to load theme '%s': %v", name, err)
			return ThemeErrorMsg{Error: err}
		}

		theme.ApplyTheme(t)
		theme.CurrentThemeName = name

		// Sauvegarder le choix de thème dans la config
		cfgRef.App.Theme = name
		if err := config.Save(cfgRef); err != nil {
			log.Printf("ERROR: Failed to save theme preference: %v", err)
		}

		log.Printf("Theme '%s' applied successfully", name)
		return ThemeAppliedMsg{ThemeName: name}
	}
}

// handleThemeApplied gère le message de thème appliqué en rafraîchissant l'UI
func (a *App) handleThemeApplied(msg ThemeAppliedMsg) (tea.Model, tea.Cmd) {
	log.Printf("Theme applied: %s, refreshing UI", msg.ThemeName)
	// Forcer un redimensionnement pour rafraîchir le rendu
	return a, func() tea.Msg {
		return tea.WindowSizeMsg{Width: a.width, Height: a.height}
	}
}

// handleThemeListKeyMsg gère les touches clavier dans l'overlay de sélection de thème
func (a *App) handleThemeListKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		a.showThemeList = false
		return a, nil
	case "up", "k":
		if a.themeSelectedIdx > 0 {
			a.themeSelectedIdx--
		}
		return a, nil
	case "down", "j":
		if a.themeSelectedIdx < len(a.themeList)-1 {
			a.themeSelectedIdx++
		}
		return a, nil
	case "enter":
		if len(a.themeList) > 0 {
			a.showThemeList = false
			return a, a.applyTheme(a.themeList[a.themeSelectedIdx])
		}
		return a, nil
	}
	return a, nil
}

// maybeOpenHelp ouvre l'overlay d'aide si la vue n'est pas en mode édition
func (a *App) maybeOpenHelp(msg tea.Msg) (tea.Model, tea.Cmd) {
	if view, ok := a.views[a.currentView]; ok {
		// Vérifier si la vue est en mode édition
		if formView, implements := view.(FormView); implements && formView.InEditMode() {
			// Passer la touche à la vue
			updatedView, cmd := view.Update(msg)
			a.views[a.currentView] = updatedView
			return a, cmd
		}

		// Vérifier si la vue implémente HelpProvider
		if provider, implements := view.(help.Provider); implements {
			content := provider.GetHelpContent()
			rendered := help.Render(content, a.width)

			// Créer un viewport pour le scroll
			helpHeight := a.height - 6 // marge pour bordure + help text
			if helpHeight < 10 {
				helpHeight = 10
			}
			a.helpViewport = viewport.New(a.width-6, helpHeight)
			a.helpViewport.SetContent(rendered)
			a.showHelp = true
			return a, nil
		}
	}
	return a, nil
}

// handleHelpKeyMsg gère les touches clavier dans l'overlay d'aide
func (a *App) handleHelpKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "?":
		a.showHelp = false
		return a, nil
	default:
		// Déléguer au viewport pour le scroll (↑↓/jk/PgUp/PgDn/g/G)
		var cmd tea.Cmd
		a.helpViewport, cmd = a.helpViewport.Update(msg)
		return a, cmd
	}
}

// handleContextListKeyMsg gère les touches clavier dans l'overlay de sélection de contexte
func (a *App) handleContextListKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		a.showContextList = false
		return a, nil
	case "up", "k":
		if a.contextSelectedIdx > 0 {
			a.contextSelectedIdx--
		}
		return a, nil
	case "down", "j":
		if a.contextSelectedIdx < len(a.contextList)-1 {
			a.contextSelectedIdx++
		}
		return a, nil
	case "enter":
		if len(a.contextList) > 0 {
			a.showContextList = false
			return a, a.switchContext(a.contextList[a.contextSelectedIdx])
		}
		return a, nil
	}
	return a, nil
}

// reinitializeViews recreates all views with new config and returns the init cmd for the current view.
func (a *App) reinitializeViews(_ *config.Config) tea.Cmd {
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

func (a *App) maybeEnterInCommandMode(msg tea.Msg) tea.Cmd {
	// Si la vue est en mode édition, ne pas entrer en mode commande et passer le message à la vue

	if view, ok := a.views[a.currentView]; ok {
		var isInEditMode bool
		if formView, implementsFormView := view.(FormView); implementsFormView {
			isInEditMode = formView.InEditMode()
		}

		// If in edit mode, check if the view explicitly allows command mode entry
		if isInEditMode {
			if cmView, ok := view.(CommandModeView); ok && cmView.AllowCommandMode() {
				isInEditMode = false
			}
		}

		if !isInEditMode {
			// No active form - enter command mode
			a.commandMode = true
			a.commandInput.Reset()
			return func() tea.Msg {
				return tea.WindowSizeMsg{Width: a.width, Height: a.height}
			}
		}

		// Active form detected - pass key to view
		updatedView, cmd := view.Update(msg)
		a.views[a.currentView] = updatedView
		return cmd
	}

	return nil
}

func (a *App) maybeQuitCommandMode(msg tea.Msg) tea.Cmd {
	if view, ok := a.views[a.currentView]; ok {
		var isInEditMode bool
		if formView, implementsFormView := view.(FormView); implementsFormView {
			isInEditMode = formView.InEditMode()
		}

		if !isInEditMode {
			// No active form - quit command mode
			a.commandMode = false
			a.commandInput.Reset()
			return func() tea.Msg {
				return tea.WindowSizeMsg{Width: a.width, Height: a.height}
			}
		}

		// Active form detected - pass key to view
		updatedView, cmd := view.Update(msg)
		a.views[a.currentView] = updatedView
		return cmd
	}

	return nil
}

func (a *App) maybeQuitApplication(msg tea.Msg) tea.Cmd {
	if view, ok := a.views[a.currentView]; ok {
		var isInEditMode bool
		if formView, implementsFormView := view.(FormView); implementsFormView {
			isInEditMode = formView.InEditMode()
		}

		if !isInEditMode {
			return tea.Quit
		}

		// Active form detected - pass key to view
		updatedView, cmd := view.Update(msg)
		a.views[a.currentView] = updatedView
		return cmd
	}

	return nil
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.resize(msg.Width, msg.Height)
		return a, nil

	case tea.KeyMsg:
		return a.handleKeyMsg(msg)

	case ContextSwitchCompleteMsg:
		return a.handleContextSwitchComplete(msg)

	case ContextSwitchErrorMsg:
		log.Printf("ERROR: Context operation failed: %v", msg.Error)
		// Future: afficher l'erreur dans status bar
		return a, nil

	case ContextListMsg:
		// Afficher l'overlay avec la liste des contextes
		a.showContextList = true
		a.contextList = msg.Contexts
		// Positionner la sélection sur le contexte courant
		a.contextSelectedIdx = 0
		for i, ctx := range msg.Contexts {
			if ctx == msg.Current {
				a.contextSelectedIdx = i
				break
			}
		}
		return a, nil

	case ThemeListMsg:
		// Afficher l'overlay avec la liste des thèmes
		a.showThemeList = true
		a.themeList = msg.Themes
		a.currentTheme = msg.Current
		// Positionner la sélection sur le thème courant
		a.themeSelectedIdx = 0
		for i, t := range msg.Themes {
			if t == msg.Current {
				a.themeSelectedIdx = i
				break
			}
		}
		return a, nil

	case ThemeAppliedMsg:
		return a.handleThemeApplied(msg)

	case ThemeErrorMsg:
		log.Printf("ERROR: Theme operation failed: %v", msg.Error)
		return a, nil

	case auth.AuthResultMsg:
		// Intercepter l'authentification GitLab pour sauvegarder la config et mettre à jour le shared.State
		return a.handleAuthResult(msg)

	case GitLabAutoLoginMsg:
		return a.handleAutoLoginResult(msg)

	case workspaces.WorkspaceScanCompleteMsg:
		// Update workspace view with scan results.
		// Don't auto-switch if the user is in the security view: switching would cause the
		// security scan's ScanCompleteMsg to be routed to the wrong view, leaving the spinner
		// stuck forever (race condition between background batch scan and manual security scan).
		if view, ok := a.views[command.ViewWorkspaces]; ok {
			updatedView, cmd := view.Update(msg)
			a.views[command.ViewWorkspaces] = updatedView
			if a.currentView != command.ViewSecurity {
				a.currentView = command.ViewWorkspaces
			}
			return a, cmd
		}
		return a, nil

	case workspaces.ScanDetailsRequestMsg:
		// Workspaces view requests full scan details for a repo
		return a.handleWorkspaceScanDetails(msg)

	case WorkspaceScanResultLoadedMsg:
		// Cached scan result loaded: open security view in details mode
		return a.handleWorkspaceScanResultLoaded(msg)

	case ociresources.ImageScanStartingMsg:
		// Route scan progress to OCI images view regardless of current view
		return a.routeToOCIImagesView(msg)

	case ociresources.ImageScanFinishedMsg:
		// Route scan result to OCI images view regardless of current view
		return a.routeToOCIImagesView(msg)

	case ociresources.LaunchBatchScanMsg:
		// Security view delegated batch scan back to OCI images view
		return a.handleLaunchBatchScan(msg)

	case ociresources.LaunchSingleImageScanMsg:
		// Security view delegated single image scan back to OCI images view
		return a.handleLaunchSingleImageScan(msg)

	case ociresources.ScanDetailsRequestMsg:
		// OCI images view requests scan details for a specific image (loads from cache, no re-scan)
		return a.handleScanDetailsRequest(msg)

	case ImageScanResultLoadedMsg:
		// Cached image scan result loaded: open security view in details mode
		return a.handleImageScanResultLoaded(msg)

	case security.SelectionRequestMsg:
		// Security view requests to browse directories or images
		return a.handleSelectionRequest(msg)

	case security.BackToOriginMsg:
		// User pressed Esc in security results view: return to the originating view
		return a, a.switchView(msg.Origin)

	case explorer.PullSelectionRequestMsg:
		// Explorer requests workspace selection for pull destination
		return a.handleExplorerPullRequest()

	case workspaces.DirectorySelectedMsg:
		// Workspaces view returned a selected directory
		return a.handleDirectorySelected(msg)

	case workspaces.SelectionCancelledMsg:
		// Workspaces view cancelled selection
		return a.handleSelectionCancelled()

	case ociresources.ImageSelectedMsg:
		// OCI images view returned a selected image
		return a.handleImageSelected(msg)

	case ociresources.SelectionCancelledMsg:
		// OCI images view cancelled selection
		return a.handleSelectionCancelled()

	default:
		// Transmettre aux vues
		if view, ok := a.views[a.currentView]; ok {
			updatedView, cmd := view.Update(msg)
			a.views[a.currentView] = updatedView
			// Rule 124: re-resize if the view's footer height changed (e.g., security state transitions)
			if newFooterHeight := a.getFooterHeight(); newFooterHeight != a.lastFooterHeight {
				a.resize(a.width, a.height)
			}
			return a, cmd
		}
	}
	return a, nil
}

// View rend l'interface
func (a *App) View() string {
	// Import lipgloss si nécessaire
	var content string

	// Afficher la vue courante
	if view, ok := a.views[a.currentView]; ok {
		content = view.View()
	} else {
		content = "View not found: " + string(a.currentView)
	}

	// Remplir le contenu sur toute la largeur intérieure du viewport
	innerWidth := a.viewport.Width - 2 // -2 pour les bordures
	content = lipgloss.NewStyle().
		Background(theme.ColorBackground).
		Foreground(theme.ColorText).
		Width(innerWidth).
		Render(content)
	a.viewport.SetContent(content)

	header := a.renderHeader()

	// Construire la ligne de bordure supérieure avec le titre de la vue
	titleLine := theme.RenderBorderTitle("", a.width)
	if view, ok := a.views[a.currentView]; ok {
		if hv, implements := view.(HeaderView); implements {
			titleLine = theme.RenderBorderTitle(hv.GetTitle(), a.width)
		}
	}

	body := a.viewport.View()

	// Rule 136: replace viewport bottom corners with T-junctions when filter bar is visible,
	// so the viewport bottom border and the filter bar form a fully closed rectangle.
	if view, ok := a.views[a.currentView]; ok {
		if fbv, ok := view.(FilterBarView); ok && fbv.FilterBarVisible() {
			body = replaceViewportBottomCorners(body)
		}
	}

	// Rule 124: render footer below the viewport bottom border
	footer := ""
	if view, ok := a.views[a.currentView]; ok {
		if fv, ok := view.(FooterView); ok {
			footer = fv.RenderFooter(a.width)
		}
	}

	inner := header + "\n" + titleLine + "\n" + body
	if footer != "" {
		inner += "\n" + footer
	}
	mainView := lipgloss.Place(a.width, a.height, lipgloss.Left, lipgloss.Top, inner,
		lipgloss.WithWhitespaceBackground(theme.ColorBackground))

	// Si l'overlay d'aide est affiché, le superposer
	if a.showHelp {
		overlay := a.renderHelpOverlay()
		return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center, overlay,
			lipgloss.WithWhitespaceBackground(theme.ColorBackground))
	}

	// Si l'overlay de contextes est affiché, le superposer
	if a.showContextList {
		overlay := a.renderContextListOverlay()
		return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center, overlay,
			lipgloss.WithWhitespaceBackground(theme.ColorBackground))
	}

	// Si l'overlay de thèmes est affiché, le superposer
	if a.showThemeList {
		overlay := a.renderThemeListOverlay()
		return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center, overlay,
			lipgloss.WithWhitespaceBackground(theme.ColorBackground))
	}

	return mainView
}

// renderContextListOverlay affiche un overlay avec la liste des contextes
func (a *App) renderContextListOverlay() string {
	var b strings.Builder

	title := theme.TitleStyle.Render("Select Context")
	b.WriteString(title + "\n\n")

	for i, ctx := range a.contextList {
		label := ctx
		if ctx == a.currentContext {
			label += " (current)"
		}
		if i == a.contextSelectedIdx {
			b.WriteString(theme.KeyStyle.Render(theme.IconCircleSmall + " " + label))
		} else {
			b.WriteString(theme.Bg("  " + label))
		}
		b.WriteString("\n")
	}

	content := b.String()
	return theme.OverlayBoxStyle().Render(content)
}

// renderHelpOverlay affiche un overlay avec l'aide de la vue courante
func (a *App) renderHelpOverlay() string {
	var b strings.Builder

	b.WriteString(a.helpViewport.View())
	b.WriteString("\n")

	content := b.String()
	return theme.OverlayBoxStyle().Render(content)
}

// renderThemeListOverlay affiche un overlay avec la liste des thèmes
func (a *App) renderThemeListOverlay() string {
	var b strings.Builder

	title := theme.TitleStyle.Render("Select Theme")
	b.WriteString(title + "\n\n")

	for i, t := range a.themeList {
		label := t
		if t == a.currentTheme {
			label += " (current)"
		}
		if i == a.themeSelectedIdx {
			b.WriteString(theme.KeyStyle.Render(theme.IconCircleSmall + " " + label))
		} else {
			b.WriteString(theme.Bg("  " + label))
		}
		b.WriteString("\n")
	}

	content := b.String()
	return theme.OverlayBoxStyle().Render(content)
}

// handleCommandMode gère le mode commande
func (a *App) handleCommandMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		// Annuler le mode commande
		a.commandMode = false
		a.commandInput.Blur()
		a.resetCompletion()
		return a, func() tea.Msg {
			return tea.WindowSizeMsg{Width: a.width, Height: a.height}
		}

	case tea.KeyTab:
		// Cycle à travers les suggestions
		return a.handleCompletionCycle()

	case tea.KeyEnter:
		// Exécuter la commande
		input := a.commandInput.Value()

		// Si suggestions actives, accepter la suggestion courante
		if len(a.completionSuggestions) > 0 {
			suggestion := a.completionSuggestions[a.completionIndex]
			input = suggestion.Text
		}

		// Parser la commande avec le nouveau parser
		cmd := command.ParseCommand(input)

		switch cmd.Type {
		case command.CommandQuit:
			a.resetCompletion()
			return a, tea.Quit

		case command.CommandView:
			// Switch vers une nouvelle vue
			a.commandMode = false
			a.commandInput.Blur()
			a.resetCompletion()

			// Reset selection mode for views that support it, without losing other state
			if cmd.View == command.ViewWorkspaces {
				delete(a.views, cmd.View)
			}
			if cmd.View == command.ViewOCIResources {
				if view, ok := a.views[command.ViewOCIResources]; ok {
					updatedView, _ := view.Update(ociresources.ResetSelectionMsg{})
					a.views[command.ViewOCIResources] = updatedView
				}
			}

			return a, a.switchView(cmd.View)

		case command.CommandContext:
			a.commandMode = false
			a.commandInput.Blur()
			a.resetCompletion()

			if len(cmd.Args) > 0 {
				// Créer/switch vers un contexte nommé
				return a, a.switchContext(cmd.Args[0])
			}
			// Pas d'args → ouvrir la modale interactive
			return a, a.listContexts()

		case command.CommandTheme:
			// Ouvrir la modale interactive de sélection de thème
			a.commandMode = false
			a.commandInput.Blur()
			a.resetCompletion()
			return a, a.listThemes()

		case command.CommandUnknown:
			// Commande invalide - rester en mode commande
			return a, nil
		}

		return a, nil

	default:
		// Mettre à jour l'input
		var cmd tea.Cmd
		a.commandInput, cmd = a.commandInput.Update(msg)
		// Mettre à jour les suggestions après chaque frappe
		a.updateCompletions()
		return a, cmd
	}
}

// switchView change la vue courante
func (a *App) switchView(view command.ViewType) tea.Cmd {
	// Lazy loading des vues
	if _, exists := a.views[view]; !exists {
		// Créer la vue si elle n'existe pas encore
		a.createView(view)
	}

	a.currentView = view

	// Appeler Init() et envoyer WindowSizeMsg pour initialiser les dimensions
	if newView, ok := a.views[view]; ok {
		log.Printf("Switching to view: %s, calling Init()", view)

		// Retourner Init() + un WindowSizeMsg pour forcer le redimensionnement
		return tea.Batch(
			newView.Init(),
			func() tea.Msg {
				return tea.WindowSizeMsg{Width: a.width, Height: a.height}
			},
		)
	}

	return nil
}

// createView crée une vue (lazy loading)
func (a *App) createView(view command.ViewType) {
	switch view {
	case command.ViewDashboard:
		if _, exists := a.views[command.ViewDashboard]; !exists {
			a.views[command.ViewDashboard] = dashboard.New(a.config, a.sharedState)
		}
	case command.ViewStatus:
		if _, exists := a.views[command.ViewStatus]; !exists {
			a.views[command.ViewStatus] = status.New(a.config)
		}
	case command.ViewGitlabAuth:
		// Utiliser Git Credential Manager context-aware pour une sécurité renforcée
		// Chaque contexte aura ses propres credentials
		storage := credentials.NewChainStorage(
			credentials.NewFileStorageForContext(a.currentContext),
			credentials.NewGitCredentialStorageWithContext(a.currentContext),
		)
		log.Printf("Creating GitLab auth view with context: %s", a.currentContext)

		authView := auth.New(a.config, storage)

		// Si déjà authentifié, configurer l'auth
		if a.sharedState.GitLabClient != nil && a.sharedState.CurrentUser != nil {
			authView.SetAuth(a.sharedState.GitLabClient, a.sharedState.CurrentUser)
		}
		a.views[command.ViewGitlabAuth] = authView
	case command.ViewGitlabExplorer:
		if _, exists := a.views[command.ViewGitlabExplorer]; !exists {
			a.views[command.ViewGitlabExplorer] = explorer.New(a.config, a.sharedState)
		}
	case command.ViewWorkspaces:
		if _, exists := a.views[command.ViewWorkspaces]; !exists {
			a.views[command.ViewWorkspaces] = workspaces.New(a.config)
		}
	case command.ViewSecurity:
		if _, exists := a.views[command.ViewSecurity]; !exists {
			a.views[command.ViewSecurity] = security.New(a.config)
		}
	case command.ViewContainers:
		if _, exists := a.views[command.ViewContainers]; !exists {
			a.views[command.ViewContainers] = containers.New(a.config)
		}
	case command.ViewOCIResources:
		if _, exists := a.views[command.ViewOCIResources]; !exists {
			a.views[command.ViewOCIResources] = ociresources.New(a.config)
		}
	case command.ViewNet:
		if _, exists := a.views[command.ViewNet]; !exists {
			a.views[command.ViewNet] = netdiag.New(a.config)
		}
	}
}

// updateCompletions met à jour les suggestions basées sur l'input actuel
func (a *App) updateCompletions() {
	input := a.commandInput.Value()

	// Reset index si input change
	if input != a.completionInput {
		a.completionIndex = 0
		a.completionInput = input
	}

	a.completionSuggestions = a.completionEngine.GetSuggestions(input)
}

// handleCompletionCycle cycle à travers les suggestions (Tab répété)
func (a *App) handleCompletionCycle() (tea.Model, tea.Cmd) {
	if len(a.completionSuggestions) == 0 {
		return a, nil
	}

	// Increment avec wrap-around
	a.completionIndex = (a.completionIndex + 1) % len(a.completionSuggestions)
	return a, nil
}

// resetCompletion nettoie l'état de complétion
func (a *App) resetCompletion() {
	a.completionSuggestions = nil
	a.completionIndex = 0
	a.completionInput = ""
}
