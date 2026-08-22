// Package session opens a forge session and remembers its token.
//
// It is the one place that knows both backends exist, and the only reason it is
// a package of its own: internal/forge must not import a backend (a domain
// package cannot depend on its implementations), and a backend must not import
// its sibling. Something above both has to choose, and this is it.
//
// The credential store is **not** forge-shaped — Storage is keyed on a URL and
// knows nothing else — so the loading and forgetting of a token are the same
// code whichever platform the context targets. Only opening a session differs,
// and only in which constructor it calls.
package session

import (
	"context"
	"fmt"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/credentials"
	"github.com/anthnel/devdesk/internal/forge"
	githubforge "github.com/anthnel/devdesk/internal/forge/github"
	gitlabforge "github.com/anthnel/devdesk/internal/forge/gitlab"
)

// Auth opens sessions against a host and remembers their tokens.
type Auth struct {
	storage credentials.Storage
}

// NewAuth builds an Auth over a secret store. A nil store is legitimate: the
// session still opens, and nothing is remembered.
func NewAuth(storage credentials.Storage) *Auth {
	return &Auth{storage: storage}
}

// Result is a live session, plus whatever could not be saved about it.
type Result struct {
	Forge forge.Forge
	User  forge.User
	// SaveWarning is set when the session opened but the token could not be
	// stored. It is not an error: the session is valid, only its persistence
	// failed, and telling the user they are not logged in would be false.
	SaveWarning string
}

// Backend builds a client for a platform, without opening a session.
//
// The default is GitLab, matching config.applyDefaults: an unknown type has
// already been normalised by the time a context is loaded, and falling through
// to the one backend that has always existed is better than returning an error
// nothing can act on.
func Backend(forgeType, url, token string) (forge.Forge, error) {
	switch forgeType {
	case config.ForgeGitHub:
		return githubforge.New(url, token)
	default:
		return gitlabforge.New(url, token)
	}
}

// Authenticate opens a session and saves the token.
func (a *Auth) Authenticate(ctx context.Context, forgeType, url, token string) (*Result, error) {
	result, err := a.AuthenticateOnly(ctx, forgeType, url, token)
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
func (a *Auth) AuthenticateOnly(ctx context.Context, forgeType, url, token string) (*Result, error) {
	backend, err := Backend(forgeType, url, token)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	// CurrentUser is the connection test: authentication succeeded exactly when
	// the host answers with who the token belongs to.
	user, err := backend.CurrentUser(ctx)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	return &Result{Forge: backend, User: user}, nil
}

// LoadCredentials reads a stored token.
//
// It takes no forge type, and that is not an oversight: the store is keyed on
// the URL, so a context that switched platform without changing host finds the
// token it already had — which is right, because it is the same host asking.
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
