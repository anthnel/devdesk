package gitlab

import (
	"fmt"

	gitlabclient "gitlab.com/gitlab-org/api/client-go"

	"github.com/anthnel/devdesk/internal/credentials"
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
	SaveWarning string // Warning si la sauvegarde du secret a échoué
}

// Authenticate authentifie avec GitLab et sauvegarde le token dans le store.
//
// Le paramètre saveCredentials a disparu : il n'existait que pour offrir le
// choix entre le credential helper et le fichier de config, et ce choix n'a plus
// de sens maintenant qu'il n'y a qu'une destination (§3.9). Les appelants qui
// ré-authentifient avec un token déjà stocké passent par AuthenticateOnly.
func (a *Auth) Authenticate(url, token string) (*AuthResult, error) {
	result, err := a.AuthenticateOnly(url, token)
	if err != nil {
		return nil, err
	}

	if a.storage != nil {
		if err := a.storage.Save(url, token); err != nil {
			// Ne pas échouer l'authentification, mais avertir l'utilisateur :
			// la session est valide, seule sa persistance a échoué.
			result.SaveWarning = fmt.Sprintf("The token was not saved: %v. You will have to enter it again next launch.", err)
		}
	}

	return result, nil
}

// AuthenticateOnly ouvre une session sans rien écrire dans le store. C'est le
// chemin de l'auto-login, dont le token vient déjà du store.
func (a *Auth) AuthenticateOnly(url, token string) (*AuthResult, error) {
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

	return &AuthResult{Client: c, User: user}, nil
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
