package credentials

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMemoryStorage(t *testing.T) {
	storage := NewMemoryStorage()

	testURL := "https://gitlab.example.com"
	testToken := "test-token-12345"

	// Test Save
	err := storage.Save(testURL, testToken)
	if err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Test Load
	token, err := storage.Load(testURL)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if token != testToken {
		t.Errorf("Expected token '%s', got '%s'", testToken, token)
	}

	// Test Load non-existent
	_, err = storage.Load("https://nonexistent.com")
	if err == nil {
		t.Error("Expected error when loading non-existent URL, got nil")
	}

	// Test Delete
	err = storage.Delete(testURL)
	if err != nil {
		t.Fatalf("Delete() error: %v", err)
	}

	// Verify deletion
	_, err = storage.Load(testURL)
	if err == nil {
		t.Error("Expected error after delete, got nil")
	}
}

func TestMemoryStorageMultipleURLs(t *testing.T) {
	storage := NewMemoryStorage()

	urls := map[string]string{
		"https://gitlab.com":         "token1",
		"https://gitlab.example.com": "token2",
		"https://custom.gitlab.org":  "token3",
	}

	// Save multiple
	for url, token := range urls {
		if err := storage.Save(url, token); err != nil {
			t.Fatalf("Save() error for %s: %v", url, err)
		}
	}

	// Load and verify
	for url, expectedToken := range urls {
		token, err := storage.Load(url)
		if err != nil {
			t.Fatalf("Load() error for %s: %v", url, err)
		}
		if token != expectedToken {
			t.Errorf("Expected token '%s' for %s, got '%s'", expectedToken, url, token)
		}
	}

	// Delete one
	if err := storage.Delete("https://gitlab.com"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}

	// Verify others still exist
	token, err := storage.Load("https://gitlab.example.com")
	if err != nil {
		t.Errorf("Load() error after deleting different URL: %v", err)
	}
	if token != "token2" {
		t.Errorf("Expected token 'token2', got '%s'", token)
	}
}

func TestFileStorage(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "credentials.json")

	storage := NewFileStorage(filePath)

	testURL := "https://gitlab.example.com"
	testToken := "test-token-12345"

	// Test Save
	err := storage.Save(testURL, testToken)
	if err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatal("Credentials file was not created")
	}

	// Verify file permissions (Unix only — Windows does not support POSIX mode bits)
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filePath)
		if err != nil {
			t.Fatalf("Stat() error: %v", err)
		}
		if mode := info.Mode(); mode.Perm() != 0600 {
			t.Errorf("Expected file permissions 0600, got %o", mode.Perm())
		}
	}

	// Test Load
	token, err := storage.Load(testURL)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if token != testToken {
		t.Errorf("Expected token '%s', got '%s'", testToken, token)
	}

	// Test Load non-existent
	_, err = storage.Load("https://nonexistent.com")
	if err == nil {
		t.Error("Expected error when loading non-existent URL, got nil")
	}

	// Test Delete
	err = storage.Delete(testURL)
	if err != nil {
		t.Fatalf("Delete() error: %v", err)
	}

	// Verify deletion
	_, err = storage.Load(testURL)
	if err == nil {
		t.Error("Expected error after delete, got nil")
	}
}

func TestFileStorageMultipleURLs(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "credentials.json")

	storage := NewFileStorage(filePath)

	urls := map[string]string{
		"https://gitlab.com":         "token1",
		"https://gitlab.example.com": "token2",
		"https://custom.gitlab.org":  "token3",
	}

	// Save multiple
	for url, token := range urls {
		if err := storage.Save(url, token); err != nil {
			t.Fatalf("Save() error for %s: %v", url, err)
		}
	}

	// Load and verify
	for url, expectedToken := range urls {
		token, err := storage.Load(url)
		if err != nil {
			t.Fatalf("Load() error for %s: %v", url, err)
		}
		if token != expectedToken {
			t.Errorf("Expected token '%s' for %s, got '%s'", expectedToken, url, token)
		}
	}
}

func TestFileStorageCreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "nested", "dir", "credentials.json")

	storage := NewFileStorage(filePath)

	// Save should create parent directories
	err := storage.Save("https://gitlab.com", "token123")
	if err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Verify directory was created
	dir := filepath.Dir(filePath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Fatal("Parent directory was not created")
	}

	// Verify file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatal("Credentials file was not created")
	}
}

func TestFileStorageLoadNonExistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "nonexistent.json")

	storage := NewFileStorage(filePath)

	// Load from non-existent file should return error
	_, err := storage.Load("https://gitlab.com")
	if err == nil {
		t.Error("Expected error when loading from non-existent file, got nil")
	}
}

func TestFileStorageUpdateExisting(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "credentials.json")

	storage := NewFileStorage(filePath)

	testURL := "https://gitlab.example.com"

	// Save initial token
	err := storage.Save(testURL, "old-token")
	if err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Update with new token
	err = storage.Save(testURL, "new-token")
	if err != nil {
		t.Fatalf("Save() error on update: %v", err)
	}

	// Verify new token
	token, err := storage.Load(testURL)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if token != "new-token" {
		t.Errorf("Expected token 'new-token', got '%s'", token)
	}
}

// stubStorage is a Storage whose every operation can be made to fail, so the
// ChainStorage tests can drive fallback behaviour.
type stubStorage struct {
	token     string
	saveErr   error
	loadErr   error
	deleteErr error
	saved     bool
	deleted   bool
}

func (s *stubStorage) Save(_, token string) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.token = token
	s.saved = true
	return nil
}

func (s *stubStorage) Load(_ string) (string, error) {
	if s.loadErr != nil {
		return "", s.loadErr
	}
	return s.token, nil
}

func (s *stubStorage) Delete(_ string) error {
	s.deleted = true
	return s.deleteErr
}

func TestChainStorageSaveWritesToEveryBackend(t *testing.T) {
	a, b := &stubStorage{}, &stubStorage{}
	chain := NewChainStorage(a, b)

	if err := chain.Save("https://gitlab.example.com", "tok"); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if !a.saved || !b.saved {
		t.Errorf("saved to a=%v b=%v, want both", a.saved, b.saved)
	}
}

func TestChainStorageSaveSucceedsWhenOneBackendWorks(t *testing.T) {
	// The fallback exists because the system keychain can refuse the write;
	// losing it must not lose the token.
	broken := &stubStorage{saveErr: errors.New("keychain unavailable")}
	working := &stubStorage{}
	chain := NewChainStorage(broken, working)

	if err := chain.Save("https://gitlab.example.com", "tok"); err != nil {
		t.Fatalf("Save() error = %v, want success via the working backend", err)
	}
	if working.token != "tok" {
		t.Errorf("working backend holds %q, want tok", working.token)
	}
}

func TestChainStorageSaveFailsWhenEveryBackendFails(t *testing.T) {
	chain := NewChainStorage(
		&stubStorage{saveErr: errors.New("first failed")},
		&stubStorage{saveErr: errors.New("second failed")},
	)

	if err := chain.Save("https://gitlab.example.com", "tok"); err == nil {
		t.Error("Save() with every backend failing returned no error")
	}
}

func TestChainStorageLoadReturnsFirstHit(t *testing.T) {
	first := &stubStorage{token: "from-first"}
	second := &stubStorage{token: "from-second"}
	chain := NewChainStorage(first, second)

	got, err := chain.Load("https://gitlab.example.com")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got != "from-first" {
		t.Errorf("Load() = %q, want from-first — order decides", got)
	}
}

func TestChainStorageLoadSkipsEmptyAndFailingBackends(t *testing.T) {
	tests := []struct {
		name  string
		first *stubStorage
	}{
		{"backend returns an error", &stubStorage{loadErr: errors.New("no entry")}},
		{"backend returns an empty token", &stubStorage{token: ""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chain := NewChainStorage(tt.first, &stubStorage{token: "fallback"})

			got, err := chain.Load("https://gitlab.example.com")
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got != "fallback" {
				t.Errorf("Load() = %q, want fallback", got)
			}
		})
	}
}

func TestChainStorageLoadFailsWhenNoBackendHasIt(t *testing.T) {
	chain := NewChainStorage(&stubStorage{loadErr: errors.New("nope")})

	if _, err := chain.Load("https://gitlab.example.com"); err == nil {
		t.Error("Load() with no backend holding the token returned no error")
	}
}

func TestChainStorageDeleteReachesEveryBackend(t *testing.T) {
	// A backend that fails to delete must not stop the others: a token left
	// behind in one store would silently resurrect on the next load.
	failing := &stubStorage{deleteErr: errors.New("locked")}
	working := &stubStorage{}
	chain := NewChainStorage(failing, working)

	if err := chain.Delete("https://gitlab.example.com"); err != nil {
		t.Errorf("Delete() error = %v, want best-effort success", err)
	}
	if !failing.deleted || !working.deleted {
		t.Errorf("deleted from failing=%v working=%v, want both attempted", failing.deleted, working.deleted)
	}
}

func TestNewFileStorageForContextNamesFilePerContext(t *testing.T) {
	tests := []struct {
		name     string
		context  string
		wantFile string
	}{
		{"empty context falls back to default", "", "credentials-default.json"},
		{"named context", "prod", "credentials-prod.json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filepath.Base(NewFileStorageForContext(tt.context).filePath)
			if got != tt.wantFile {
				t.Errorf("file = %q, want %q", got, tt.wantFile)
			}
		})
	}
}

func TestFileStorageReportsCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "creds.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("writing corrupt file: %v", err)
	}
	s := NewFileStorage(path)

	if _, err := s.Load("https://gitlab.example.com"); err == nil {
		t.Error("Load() from a corrupt file returned no error")
	}
	// Save must refuse too rather than silently discarding whatever the file held.
	if err := s.Save("https://gitlab.example.com", "tok"); err == nil {
		t.Error("Save() over a corrupt file returned no error")
	}
	if err := s.Delete("https://gitlab.example.com"); err == nil {
		t.Error("Delete() on a corrupt file returned no error")
	}
}

func TestFileStorageDeleteOnMissingFile(t *testing.T) {
	s := NewFileStorage(filepath.Join(t.TempDir(), "absent.json"))

	if err := s.Delete("https://gitlab.example.com"); err == nil {
		t.Error("Delete() on a missing file returned no error")
	}
}

func TestFileStorageSaveFailsWhenParentIsAFile(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("writing blocker: %v", err)
	}
	// The parent directory cannot be created because a file already occupies it.
	s := NewFileStorage(filepath.Join(blocker, "creds.json"))

	if err := s.Save("https://gitlab.example.com", "tok"); err == nil {
		t.Error("Save() under an unusable parent returned no error")
	}
}
