package credentials

import (
	"fmt"
	"sync"
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

// MemoryStorage is the session fallback, which means one instance is shared by
// the Cmd goroutines of every view. Without the lock this is a data race, and
// `mise run test-race` is where it would surface.
func TestMemoryStorageSurvivesConcurrentUse(t *testing.T) {
	storage := NewMemoryStorage()

	const workers = 8
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := range workers {
		go func() {
			defer wg.Done()
			url := fmt.Sprintf("https://gitlab-%d.example.com", i)
			for range 50 {
				_ = storage.Save(url, "tok")
				_, _ = storage.Load(url)
				_ = storage.Delete(url)
			}
		}()
	}
	wg.Wait()
}
