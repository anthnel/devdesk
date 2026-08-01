package gitlab

import (
	"testing"

	"gitlab.com/anthnell/devsecops/devdesk/internal/credentials"
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

func TestAuthResultStruct(t *testing.T) {
	result := &AuthResult{
		Client:      nil,
		User:        nil,
		SaveWarning: "test warning",
	}

	if result.SaveWarning != "test warning" {
		t.Errorf("Expected warning 'test warning', got '%s'", result.SaveWarning)
	}
}
