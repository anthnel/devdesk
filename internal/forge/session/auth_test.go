package session

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
)

// memStore is a credentials.Storage that lives in the test. It records what was
// asked of it, which is what the session tests are actually about — the token
// is not this package's to keep, only to hand over.
type memStore struct {
	tokens   map[string]string
	saveErr  error
	loadErr  error
	deleted  []string
	saveArgs []string
}

func newMemStore() *memStore { return &memStore{tokens: map[string]string{}} }

func (s *memStore) Save(url, token string) error {
	s.saveArgs = append(s.saveArgs, url)
	if s.saveErr != nil {
		return s.saveErr
	}
	s.tokens[url] = token
	return nil
}

func (s *memStore) Load(url string) (string, error) {
	if s.loadErr != nil {
		return "", s.loadErr
	}
	return s.tokens[url], nil
}

func (s *memStore) Delete(url string) error {
	s.deleted = append(s.deleted, url)
	delete(s.tokens, url)
	return nil
}

// userHandler answers the connection test and nothing else.
func userHandler(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte(`{"id":9,"username":"ada","name":"Ada"}`))
}

func TestAuthenticateOpensASessionAndKeepsTheToken(t *testing.T) {
	fake := newFakeForge(t, userHandler)
	store := newMemStore()

	result, err := NewAuth(store).Authenticate(context.Background(), config.ForgeGitLab, fake.server.URL, "glpat-x")
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if result.Forge == nil {
		t.Fatal("Authenticate() returned no forge")
	}
	if result.User.Username != "ada" {
		t.Errorf("user = %+v, want ada", result.User)
	}
	if result.SaveWarning != "" {
		t.Errorf("SaveWarning = %q on a store that accepted the token", result.SaveWarning)
	}
	if store.tokens[fake.server.URL] != "glpat-x" {
		t.Errorf("the store holds %q, want the token", store.tokens[fake.server.URL])
	}
}

// TestAStoreThatRefusesTheTokenStillOpensTheSession — the session is valid;
// only its persistence failed. Reporting an error would tell the user they are
// not logged in when they are.
func TestAStoreThatRefusesTheTokenStillOpensTheSession(t *testing.T) {
	fake := newFakeForge(t, userHandler)
	store := newMemStore()
	store.saveErr = errors.New("keyring locked")

	result, err := NewAuth(store).Authenticate(context.Background(), config.ForgeGitLab, fake.server.URL, "glpat-x")
	if err != nil {
		t.Fatalf("Authenticate() error = %v, want the session to open anyway", err)
	}
	if result.Forge == nil {
		t.Fatal("Authenticate() returned no forge")
	}
	if result.SaveWarning == "" {
		t.Error("SaveWarning is empty, so nothing tells the user the token was not kept")
	}
}

// TestAuthenticateOnlyWritesNothing is the auto-login path: its token already
// came from the store, and writing it back would be a write nobody asked for.
func TestAuthenticateOnlyWritesNothing(t *testing.T) {
	fake := newFakeForge(t, userHandler)
	store := newMemStore()

	if _, err := NewAuth(store).AuthenticateOnly(context.Background(), config.ForgeGitLab, fake.server.URL, "glpat-x"); err != nil {
		t.Fatalf("AuthenticateOnly() error = %v", err)
	}
	if len(store.saveArgs) != 0 {
		t.Errorf("AuthenticateOnly() saved to %v, want nothing", store.saveArgs)
	}
}

// TestARejectedTokenIsAnErrorAndNoSession — the connection test is the
// authentication, so a host that refuses the token must not yield a forge the
// caller would then use.
func TestARejectedTokenIsAnErrorAndNoSession(t *testing.T) {
	fake := newFakeForge(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"401 Unauthorized"}`))
	})
	store := newMemStore()

	result, err := NewAuth(store).Authenticate(context.Background(), config.ForgeGitLab, fake.server.URL, "bad")
	if err == nil {
		t.Fatal("Authenticate() succeeded against a host that refused the token")
	}
	if result != nil {
		t.Errorf("a refused token produced %+v, want no session", result)
	}
	if len(store.saveArgs) != 0 {
		t.Errorf("a refused token was saved to %v", store.saveArgs)
	}
}

func TestLoadAndLogoutGoThroughTheStore(t *testing.T) {
	store := newMemStore()
	store.tokens["https://gl"] = "glpat-x"
	auth := NewAuth(store)

	token, err := auth.LoadCredentials("https://gl")
	if err != nil || token != "glpat-x" {
		t.Errorf("LoadCredentials() = %q, %v", token, err)
	}

	if err := auth.Logout("https://gl"); err != nil {
		t.Errorf("Logout() error = %v", err)
	}
	if len(store.deleted) != 1 || store.deleted[0] != "https://gl" {
		t.Errorf("Logout() deleted %v, want the one URL", store.deleted)
	}
}

// TestNoStoreIsNotAFailure — MemoryStorage is the last resort and a nil store
// is what a context with none looks like. Logging out of nothing succeeds;
// loading from nothing says so.
func TestNoStoreIsNotAFailure(t *testing.T) {
	auth := NewAuth(nil)

	if _, err := auth.LoadCredentials("https://gl"); err == nil {
		t.Error("LoadCredentials() succeeded with no store, want an error naming it")
	}
	if err := auth.Logout("https://gl"); err != nil {
		t.Errorf("Logout() with no store = %v, want nil", err)
	}
}

// TestAnUnreachableHostIsAnErrorNotAnEmptySession pins the other failure: a
// URL the client cannot even be built for.
func TestAnUnreachableHostIsAnErrorNotAnEmptySession(t *testing.T) {
	if _, err := NewAuth(nil).AuthenticateOnly(context.Background(), config.ForgeGitLab, "://not a url", "x"); err == nil {
		t.Error("AuthenticateOnly() accepted a malformed host")
	}
}
