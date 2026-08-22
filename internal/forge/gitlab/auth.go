package gitlab

import (
	"context"
	"fmt"

	"github.com/anthnel/devdesk/internal/credentials"
	"github.com/anthnel/devdesk/internal/forge"
)

// Auth opens a session against a GitLab host and remembers its token.
//
// It moved here from internal/gitlab with the rest of that package (§3.6 step
// 3). Nothing about the credential store is GitLab-shaped — Storage is keyed on
// a URL and knows nothing else — but building a session means building a
// backend, and the backend is what this package is. When a second one exists,
// choosing between them is a switch on the context's declared forge, and this
// is one of the two places it will live.
type Auth struct {
	storage credentials.Storage
}

// NewAuth builds an Auth over a secret store. A nil store is legitimate: the
// session still opens, and nothing is remembered.
func NewAuth(storage credentials.Storage) *Auth {
	return &Auth{storage: storage}
}

// AuthResult is a live session, plus whatever could not be saved about it.
type AuthResult struct {
	Forge forge.Forge
	User  forge.User
	// SaveWarning is set when the session opened but the token could not be
	// stored. It is not an error: the session is valid, only its persistence
	// failed, and telling the user they are not logged in would be false.
	SaveWarning string
}

// Authenticate opens a session and saves the token.
//
// The saveCredentials parameter is long gone: it existed to offer a choice
// between the credential helper and the configuration file, and there is one
// destination now (§3.9). A caller re-authenticating with a token that already
// came from the store uses AuthenticateOnly.
func (a *Auth) Authenticate(ctx context.Context, url, token string) (*AuthResult, error) {
	result, err := a.AuthenticateOnly(ctx, url, token)
	if err != nil {
		return nil, err
	}

	if a.storage != nil {
		if err := a.storage.Save(url, token); err != nil {
			result.SaveWarning = fmt.Sprintf("The token was not saved: %v. You will have to enter it again next launch.", err)
		}
	}

	return result, nil
}

// AuthenticateOnly opens a session and writes nothing. It is the auto-login
// path, whose token already comes from the store.
func (a *Auth) AuthenticateOnly(ctx context.Context, url, token string) (*AuthResult, error) {
	backend, err := New(url, token)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	// CurrentUser is the connection test: authentication succeeded exactly when
	// the host answers with who the token belongs to.
	user, err := backend.CurrentUser(ctx)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	return &AuthResult{Forge: backend, User: user}, nil
}

// LoadCredentials reads a stored token.
func (a *Auth) LoadCredentials(url string) (string, error) {
	if a.storage == nil {
		return "", fmt.Errorf("no storage configured")
	}
	return a.storage.Load(url)
}

// Logout forgets a stored token.
func (a *Auth) Logout(url string) error {
	if a.storage == nil {
		return nil
	}
	return a.storage.Delete(url)
}
