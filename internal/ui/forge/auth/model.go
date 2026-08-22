package auth

import (
	"log"

	"github.com/anthnel/devdesk/internal/forge"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/credentials"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// Champs du formulaire, dans l'ordre de navigation.
//
// Il n'y a plus de champ de choix entre deux destinations : le token va dans le
// gestionnaire de secrets de l'hôte, et nulle part ailleurs (§3.9).
const (
	fieldToken = iota
	fieldSubmit

	lastField = fieldSubmit
)

// Model représente la vue d'authentification GitLab
type Model struct {
	config *config.Config
	// There is no URL input. forge.url is configuration and the configuration
	// view owns it; this view owns the token, which is a secret, and the act of
	// logging in. Both used to write the URL, so neither was authoritative.
	tokenInput textinput.Model

	// secrets is where the token goes, and what to tell the user about it.
	secrets credentials.Selection

	// notices report what the migration off plaintext configuration did, if
	// anything. Shown once, on the view that owns the token.
	notices []string

	currentField int

	authenticated bool // True si l'utilisateur est authentifié
	// user is the signed-in user, zero when there is none — m.authenticated is
	// the flag, and a second way to ask is how the two came to disagree.
	user forge.User

	authenticating bool
	spinner        spinner.Model
	error          string
	success        string
	warning        string // Warning non-bloquant (ex: échec sauvegarde credentials)

	width  int
	height int
}

// forgeTypeOf reads the context's platform, empty-safe. It exists because New
// runs before the model does, so m.vocab() is not available yet.
func forgeTypeOf(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	return cfg.Forge.Type
}

// New crée une nouvelle vue d'authentification
func New(cfg *config.Config, secrets credentials.Selection, notices []string) *Model {
	// Créer les inputs
	tokenInput := textinput.New()
	// The example token is the forge's. It was a GitLab literal here, and it
	// escaped vocabtest because `glpat-` names no forge — the guard looks for
	// the platforms' names, and a token prefix is forge-specific without
	// carrying one.
	tokenInput.Placeholder = forge.VocabularyFor(forgeTypeOf(cfg)).TokenPlaceholder
	tokenInput.CharLimit = 100
	tokenInput.Width = 60
	tokenInput.EchoMode = textinput.EchoPassword
	tokenInput.EchoCharacter = '•'
	theme.StyleTextInput(&tokenInput)

	// Spinner
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = theme.SpinnerStyle()

	// Focus sur le premier champ
	tokenInput.Focus()

	return &Model{
		config:         cfg,
		tokenInput:     tokenInput,
		secrets:        secrets,
		notices:        notices,
		currentField:   fieldToken,
		authenticating: false,
		spinner:        sp,
		error:          "",
		success:        "",
	}
}

// Init initialise le modèle
func (m *Model) Init() tea.Cmd {
	// Essayer de charger les credentials sauvegardés
	return tea.Batch(
		textinput.Blink,
		m.loadSavedCredentials(),
	)
}

// loadSavedCredentials récupère le token depuis le store de secrets.
//
// Il n'y a qu'une source : la configuration ne contient plus de token, et une
// version antérieure qui en aurait laissé un s'est fait migrer au démarrage
// (credentials.MigrateLegacySecrets).
func (m *Model) loadSavedCredentials() tea.Cmd {
	url := m.config.Forge.URL
	storage := m.secrets.Storage

	return func() tea.Msg {
		if storage == nil || url == "" {
			log.Printf("AUTH: No saved credentials to load (url set: %v)", url != "")
			return CredentialsLoadedMsg{}
		}

		token, err := storage.Load(url)
		if err != nil {
			log.Printf("AUTH: No token in %s for %s: %v", m.secrets.Backend, url, err)
			return CredentialsLoadedMsg{}
		}
		if token == "" {
			log.Printf("AUTH: Empty token in %s for %s", m.secrets.Backend, url)
			return CredentialsLoadedMsg{}
		}

		log.Printf("AUTH: Token loaded from %s", m.secrets.Backend)
		return CredentialsLoadedMsg{URL: url, Token: token}
	}
}

// CredentialsLoadedMsg contient les credentials chargés
type CredentialsLoadedMsg struct {
	URL   string
	Token string
}

// SetAuth configure l'authentification depuis l'extérieur (auto-login global)
func (m *Model) SetAuth(_ forge.Forge, user forge.User) {
	m.authenticating = false
	m.authenticated = true
	m.user = user
	m.error = ""
	m.success = "✓ Already authenticated as " + user.Username
}

// Messages pour l'authentification

// AuthStartMsg indique le début de l'authentification
type AuthStartMsg struct{}

// AuthResultMsg contient le résultat de l'authentification
type AuthResultMsg struct {
	Forge       forge.Forge
	User        forge.User
	Error       error
	SaveWarning string // Warning si la sauvegarde du secret a échoué

	// ConfigToSave carries the URL — and only the URL. The token goes to the
	// secret store; nothing about it is written to the configuration file.
	ConfigToSave *config.Config
}

// LogoutCompleteMsg signale que le logout est terminé
type LogoutCompleteMsg struct{}
