package auth

import (
	"log"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	gitlabclient "gitlab.com/gitlab-org/api/client-go"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/credentials"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// SaveOption représente les options de sauvegarde du token
const (
	SaveToHelper = iota // Sauvegarder dans Git Credential Manager (sécurisé)
	SaveToConfig        // Sauvegarder dans le fichier de config (moins sécurisé)
)

// Model représente la vue d'authentification GitLab
type Model struct {
	config     *config.Config
	urlInput   textinput.Model
	tokenInput textinput.Model
	storage    credentials.Storage

	currentField int
	saveOption   int // SaveToHelper ou SaveToConfig

	authenticated bool               // True si l'utilisateur est authentifié
	user          *gitlabclient.User // Utilisateur actuellement authentifié

	authenticating bool
	spinner        spinner.Model
	error          string
	success        string
	warning        string // Warning non-bloquant (ex: échec sauvegarde credentials)

	width  int
	height int
}

// New crée une nouvelle vue d'authentification
func New(cfg *config.Config, storage credentials.Storage) *Model {
	// Créer les inputs
	urlInput := textinput.New()
	urlInput.Placeholder = "https://gitlab.com"
	urlInput.CharLimit = 200
	urlInput.Width = 60
	theme.StyleTextInput(&urlInput)

	// Pré-remplir avec la config si disponible
	if cfg.GitLab.URL != "" {
		urlInput.SetValue(cfg.GitLab.URL)
	}

	tokenInput := textinput.New()
	tokenInput.Placeholder = "glpat-xxxxxxxxxxxxxxxxxxxx"
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
	urlInput.Focus()

	return &Model{
		config:         cfg,
		urlInput:       urlInput,
		tokenInput:     tokenInput,
		storage:        storage,
		currentField:   0,
		saveOption:     SaveToHelper, // Par défaut, sauvegarder dans le helper
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

// loadSavedCredentials tente de charger les credentials depuis config ou storage
func (m *Model) loadSavedCredentials() tea.Cmd {
	return func() tea.Msg {
		log.Printf("AUTH: loadSavedCredentials() called")

		// 1. Essayer depuis la config
		if m.config.GitLab.URL != "" && m.config.GitLab.Token != "" {
			log.Printf("AUTH: Credentials loaded from config (URL: %s)", m.config.GitLab.URL)
			return CredentialsLoadedMsg{
				URL:    m.config.GitLab.URL,
				Token:  m.config.GitLab.Token,
				Source: "config",
			}
		}

		// 2. Sinon essayer depuis le storage (credential helper)
		if m.storage != nil && m.config.GitLab.URL != "" {
			log.Printf("AUTH: Trying to load token from storage for URL: %s", m.config.GitLab.URL)
			token, err := m.storage.Load(m.config.GitLab.URL)
			if err == nil && token != "" {
				log.Printf("AUTH: Token loaded from storage successfully")
				return CredentialsLoadedMsg{
					URL:    m.config.GitLab.URL,
					Token:  token,
					Source: "storage",
				}
			}
			if err != nil {
				log.Printf("AUTH: Failed to load from storage: %v", err)
			} else {
				log.Printf("AUTH: No token found in storage")
			}
		} else {
			if m.storage == nil {
				log.Printf("AUTH: Storage is nil")
			}
			if m.config.GitLab.URL == "" {
				log.Printf("AUTH: No URL in config")
			}
		}

		// Rien trouvé
		log.Printf("AUTH: No saved credentials found")
		return CredentialsLoadedMsg{}
	}
}

// CredentialsLoadedMsg contient les credentials chargés
type CredentialsLoadedMsg struct {
	URL    string
	Token  string
	Source string // "config" ou "storage" ou vide
}

// SetAuth configure l'authentification depuis l'extérieur (auto-login global)
func (m *Model) SetAuth(client *gitlabclient.Client, user *gitlabclient.User) {
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
	Client       *gitlabclient.Client
	User         *gitlabclient.User
	Error        error
	SaveWarning  string // Warning si la sauvegarde des credentials a échoué
	SaveToConfig bool
	ConfigToSave *config.Config
}

// LogoutCompleteMsg signale que le logout est terminé
type LogoutCompleteMsg struct{}
