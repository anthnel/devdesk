package gitlab

import (
	"fmt"

	gitlabclient "gitlab.com/gitlab-org/api/client-go"

	"gitlab.com/anthnell/devsecops/devdesk/internal/credentials"
)

// Auth gère l'authentification GitLab
type Auth struct {
	storage credentials.Storage
}

// NewAuth crée un nouveau gestionnaire d'auth
func NewAuth(storage credentials.Storage) *Auth {
	return &Auth{storage: storage}
}

// AuthResult contient le résultat de l'authentification avec warning éventuel
type AuthResult struct {
	Client      *gitlabclient.Client
	User        *gitlabclient.User
	SaveWarning string // Warning si la sauvegarde des credentials a échoué
}

// Authenticate authentifie avec GitLab et sauvegarde les credentials
func (a *Auth) Authenticate(url, token string, saveCredentials bool) (*AuthResult, error) {
	// Créer le client
	c, err := NewClient(url, token)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	// Tester la connexion
	user, err := TestConnection(c)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	result := &AuthResult{
		Client: c,
		User:   user,
	}

	// Sauvegarder les credentials si demandé
	if saveCredentials && a.storage != nil {
		if err := a.storage.Save(url, token); err != nil {
			// Ne pas échouer l'authentification, mais avertir l'utilisateur
			result.SaveWarning = fmt.Sprintf("Credentials not saved to Git Credential Manager: %v. Token will only be saved to config file if selected.", err)
		}
	}

	return result, nil
}

// LoadCredentials charge les credentials sauvegardés
func (a *Auth) LoadCredentials(url string) (string, error) {
	if a.storage == nil {
		return "", fmt.Errorf("no storage configured")
	}

	return a.storage.Load(url)
}

// Logout supprime les credentials sauvegardés
func (a *Auth) Logout(url string) error {
	if a.storage == nil {
		return nil
	}

	return a.storage.Delete(url)
}
