package credentials

import (
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

func TestExtractHost(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "HTTPS URL",
			url:      "https://gitlab.example.com",
			expected: "gitlab.example.com",
		},
		{
			name:     "HTTP URL",
			url:      "http://gitlab.example.com",
			expected: "gitlab.example.com",
		},
		{
			name:     "URL with path",
			url:      "https://gitlab.example.com/api/v4",
			expected: "gitlab.example.com",
		},
		{
			name:     "URL with port",
			url:      "https://gitlab.example.com:8080",
			expected: "gitlab.example.com:8080",
		},
		{
			name:     "Bare hostname",
			url:      "gitlab.example.com",
			expected: "gitlab.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractHost(tt.url)
			if result != tt.expected {
				t.Errorf("extractHost(%s) = %s, expected %s", tt.url, result, tt.expected)
			}
		})
	}
}

func TestDetectHelper(t *testing.T) {
	// Test that detectHelper returns a non-empty string
	helper := detectHelper()
	if helper == "" {
		t.Error("detectHelper() returned empty string")
	}

	// Should return a valid helper name
	// Common helpers: store, osxkeychain, wincred, libsecret
	// We accept any non-empty string since it depends on system config
	t.Logf("Detected helper: %s", helper)
}

func TestNewHelperStorage(t *testing.T) {
	// Test with explicit helper
	storage := NewHelperStorage("store")
	if storage == nil {
		t.Fatal("NewHelperStorage() returned nil")
	}
	if storage.helper != "store" {
		t.Errorf("Expected helper 'store', got '%s'", storage.helper)
	}

	// Test with auto-detect
	storage = NewHelperStorage("")
	if storage == nil {
		t.Fatal("NewHelperStorage(\"\") returned nil")
	}
	if storage.helper == "" {
		t.Error("NewHelperStorage(\"\") did not auto-detect helper")
	}
}
