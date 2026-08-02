package gitlab

import (
	"net/http"
	"testing"

	"github.com/anthnel/devdesk/internal/credentials"
)

func TestNewAuth(t *testing.T) {
	storage := credentials.NewMemoryStorage()
	auth := NewAuth(storage)

	if auth == nil {
		t.Fatal("NewAuth() returned nil")
	}
	if auth.storage == nil {
		t.Error("Expected storage to be set")
	}
}

func TestNewAuthNilStorage(t *testing.T) {
	auth := NewAuth(nil)

	if auth == nil {
		t.Fatal("NewAuth() returned nil")
	}
	if auth.storage != nil {
		t.Error("Expected storage to be nil")
	}
}

func TestLoadCredentials(t *testing.T) {
	storage := credentials.NewMemoryStorage()
	auth := NewAuth(storage)

	testURL := "https://gitlab.example.com"
	testToken := "test-token-12345"

	// Save token first
	err := storage.Save(testURL, testToken)
	if err != nil {
		t.Fatalf("Failed to save token: %v", err)
	}

	// Load credentials
	token, err := auth.LoadCredentials(testURL)
	if err != nil {
		t.Fatalf("LoadCredentials() error: %v", err)
	}
	if token != testToken {
		t.Errorf("Expected token '%s', got '%s'", testToken, token)
	}
}

func TestLoadCredentialsNonExistent(t *testing.T) {
	storage := credentials.NewMemoryStorage()
	auth := NewAuth(storage)

	_, err := auth.LoadCredentials("https://nonexistent.com")
	if err == nil {
		t.Error("Expected error when loading non-existent credentials, got nil")
	}
}

func TestLoadCredentialsNoStorage(t *testing.T) {
	auth := NewAuth(nil)

	_, err := auth.LoadCredentials("https://gitlab.com")
	if err == nil {
		t.Error("Expected error when loading with no storage, got nil")
	}
}

func TestLogout(t *testing.T) {
	storage := credentials.NewMemoryStorage()
	auth := NewAuth(storage)

	testURL := "https://gitlab.example.com"
	testToken := "test-token-12345"

	// Save token
	err := storage.Save(testURL, testToken)
	if err != nil {
		t.Fatalf("Failed to save token: %v", err)
	}

	// Verify it exists
	_, err = auth.LoadCredentials(testURL)
	if err != nil {
		t.Fatalf("Token should exist before logout: %v", err)
	}

	// Logout
	err = auth.Logout(testURL)
	if err != nil {
		t.Fatalf("Logout() error: %v", err)
	}

	// Verify it's gone
	_, err = auth.LoadCredentials(testURL)
	if err == nil {
		t.Error("Expected error after logout, token still exists")
	}
}

func TestLogoutNoStorage(t *testing.T) {
	auth := NewAuth(nil)

	// Should not error even with no storage
	err := auth.Logout("https://gitlab.com")
	if err != nil {
		t.Errorf("Logout() with no storage returned error: %v", err)
	}
}

func TestLogoutNonExistent(t *testing.T) {
	storage := credentials.NewMemoryStorage()
	auth := NewAuth(storage)

	// Logout of URL that doesn't exist should succeed
	err := auth.Logout("https://nonexistent.com")
	if err != nil {
		t.Errorf("Logout() of non-existent URL returned error: %v", err)
	}
}

// Mock storage that always fails
type failingStorage struct{}

func (f *failingStorage) Save(url, token string) error {
	return &testError{"save failed"}
}

func (f *failingStorage) Load(url string) (string, error) {
	return "", &testError{"load failed"}
}

func (f *failingStorage) Delete(url string) error {
	return &testError{"delete failed"}
}

type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}

func TestLoadCredentialsStorageError(t *testing.T) {
	storage := &failingStorage{}
	auth := NewAuth(storage)

	_, err := auth.LoadCredentials("https://gitlab.com")
	if err == nil {
		t.Error("Expected error from failing storage, got nil")
	}
}

func TestLogoutStorageError(t *testing.T) {
	storage := &failingStorage{}
	auth := NewAuth(storage)

	err := auth.Logout("https://gitlab.com")
	if err == nil {
		t.Error("Expected error from failing storage, got nil")
	}
}

func TestAuthenticateReturnsUserAndSavesToken(t *testing.T) {
	f := newFakeGitLab(t, jsonHandler(http.StatusOK, `{"id":7,"username":"alice"}`))
	storage := credentials.NewMemoryStorage()
	auth := NewAuth(storage)

	result, err := auth.Authenticate(f.server.URL, "test-token", true)

	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if result.User == nil || result.User.Username != "alice" {
		t.Errorf("result.User = %+v, want alice", result.User)
	}
	if result.Client == nil {
		t.Error("result.Client is nil")
	}
	if result.SaveWarning != "" {
		t.Errorf("SaveWarning = %q, want empty on a successful save", result.SaveWarning)
	}

	saved, err := storage.Load(f.server.URL)
	if err != nil {
		t.Fatalf("token was not saved: %v", err)
	}
	if saved != "test-token" {
		t.Errorf("saved token = %q, want test-token", saved)
	}
}

func TestAuthenticateSkipsSaveWhenNotRequested(t *testing.T) {
	f := newFakeGitLab(t, jsonHandler(http.StatusOK, `{"id":7,"username":"alice"}`))
	storage := credentials.NewMemoryStorage()
	auth := NewAuth(storage)

	if _, err := auth.Authenticate(f.server.URL, "test-token", false); err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	if _, err := storage.Load(f.server.URL); err == nil {
		t.Error("token was saved even though saveCredentials was false")
	}
}

func TestAuthenticateWarnsButSucceedsWhenSaveFails(t *testing.T) {
	f := newFakeGitLab(t, jsonHandler(http.StatusOK, `{"id":7,"username":"alice"}`))
	auth := NewAuth(&failingStorage{})

	result, err := auth.Authenticate(f.server.URL, "test-token", true)

	// A credential store that refuses the write must not cost the user their session.
	if err != nil {
		t.Fatalf("Authenticate() error = %v, want success with a warning", err)
	}
	if result.SaveWarning == "" {
		t.Error("SaveWarning is empty, want an explanation of the failed save")
	}
	if result.User == nil {
		t.Error("result.User is nil despite a successful authentication")
	}
}

func TestAuthenticateWithoutStorageDoesNotWarn(t *testing.T) {
	f := newFakeGitLab(t, jsonHandler(http.StatusOK, `{"id":7,"username":"alice"}`))
	auth := NewAuth(nil)

	result, err := auth.Authenticate(f.server.URL, "test-token", true)

	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if result.SaveWarning != "" {
		t.Errorf("SaveWarning = %q, want empty when no storage is configured", result.SaveWarning)
	}
}

func TestAuthenticateRejectsBadToken(t *testing.T) {
	f := newFakeGitLab(t, jsonHandler(http.StatusUnauthorized, `{"message":"401 Unauthorized"}`))
	storage := credentials.NewMemoryStorage()
	auth := NewAuth(storage)

	result, err := auth.Authenticate(f.server.URL, "wrong-token", true)

	if err == nil {
		t.Fatal("Authenticate() with a rejected token returned no error")
	}
	if result != nil {
		t.Errorf("result = %+v, want nil on error", result)
	}
	if _, err := storage.Load(f.server.URL); err == nil {
		t.Error("a rejected token was saved to storage")
	}
}

func TestAuthenticateRejectsMalformedURL(t *testing.T) {
	auth := NewAuth(credentials.NewMemoryStorage())

	if _, err := auth.Authenticate("://not-a-url", "test-token", true); err == nil {
		t.Error("Authenticate() with a malformed URL returned no error")
	}
}
