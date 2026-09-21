package cache

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestARemediationResultRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.json")
	want := RemediationEntry{Critical: 1, High: 2, Medium: 3, Low: 4, ScannedAt: time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)}

	if err := setRemediationResultAt(path, "alpine:3.21", want); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err := readRemediationFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got["alpine:3.21"] != want {
		t.Errorf("entry = %+v, want %+v", got["alpine:3.21"], want)
	}
}

func TestAMissingRemediationFileIsAnEmptyCache(t *testing.T) {
	got, err := readRemediationFile(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil || len(got) != 0 {
		t.Errorf("got %v, %v; want an empty cache", got, err)
	}
}

func TestACorruptRemediationFileIsReported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readRemediationFile(path); err == nil {
		t.Error("a corrupt file read as an empty cache")
	}
}

// Scans of several candidates finish together, each on its own Cmd. None of
// their entries may be lost to another's read-modify-write.
func TestConcurrentRemediationWritesLoseNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.json")
	const n = 20
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ref := "img:" + string(rune('a'+i))
			if err := setRemediationResultAt(path, ref, RemediationEntry{Critical: i}); err != nil {
				t.Errorf("set %s: %v", ref, err)
			}
		}()
	}
	wg.Wait()

	got, err := readRemediationFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != n {
		t.Errorf("%d entries survived, want %d", len(got), n)
	}
}

func TestTheRemediationFileIsPrivateAndLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r.json")
	if err := setRemediationResultAt(path, "a:1", RemediationEntry{}); err != nil {
		t.Fatalf("set: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("directory holds %d files, want only the cache", len(entries))
	}
	if info, err := os.Stat(path); err == nil && info.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Errorf("mode = %v, want owner-only", info.Mode().Perm())
	}
}
