package cache

import (
	"os"
	"path/filepath"
	"testing"
)

// The "secrets" verdict moved from a boolean to a pointer, and this file
// holds both ends of what that changes for files already written: what must
// survive does survive, and what was never written reads as "unknown"
// rather than "clean".

// writeRaw writes a cache file verbatim, to describe a file the way a
// previous version left it — something a round-trip through the current
// struct could not produce.
func writeRaw(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

// The old field was always written — `json:"sensitive"`, without omitempty —
// so both verdicts a previous version could have recorded read back
// unchanged. That is what makes the migration free: there isn't one.
func TestALegacyWorkspaceVerdictSurvivesTheChangeOfType(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{"a repository that carried a secret", `true`, true},
		{"one that was looked at and was clean", `false`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "workspace-scans.json")
			writeRaw(t, path, `{"version":1,"contexts":{"work":{"/home/u/repo":{
				"repo_path":"/home/u/repo","critical":1,"sensitive":`+tt.raw+`,
				"scanned_at":"2026-08-01T12:00:00Z"}}}}`)

			got := openWorkspaceCache(t, path, "work").Get("/home/u/repo")

			if got == nil {
				t.Fatal("the entry was dropped")
			}
			if got.Sensitive == nil {
				t.Fatalf("Sensitive = nil, want the verdict %v the file records", tt.want)
			}
			if *got.Sensitive != tt.want {
				t.Errorf("Sensitive = %v, want %v", *got.Sensitive, tt.want)
			}
		})
	}
}

// An image entry written before the image scan had a secrets stage has no
// key at all. It must read as "nobody looked": a false would be a green
// icon slapped on a scan that never looked, which is the one lie this field
// could tell.
func TestAnImageEntryWrittenBeforeTheSecretStageHasNoVerdict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image-scans.json")
	writeRaw(t, path, `{"version":1,"contexts":{"work":{"api:v1":{
		"image_id":"sha256:abc","critical":2,"scanned_at":"2026-08-01T12:00:00Z"}}}}`)

	got := openImageCache(t, path).Get("api:v1")

	if got == nil {
		t.Fatal("the entry was dropped")
	}
	if got.Critical != 2 {
		t.Errorf("Critical = %d, want the counts to come through untouched", got.Critical)
	}
	if got.Sensitive != nil {
		t.Errorf("Sensitive = %v, want nil — that scan had no secret stage at all", *got.Sensitive)
	}
}

// The verdict round-trips through the file, in all three of its values.
func TestTheThreeVerdictsSurviveARoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		write *bool
	}{
		{"found", secretsFound()},
		{"clean", secretsClean()},
		{"nobody looked", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "image-scans.json")
			c := openImageCache(t, path)
			if err := c.Set("api:v1", ImageScanEntry{Sensitive: tt.write}); err != nil {
				t.Fatalf("Set: %v", err)
			}

			// Re-read from the file, not from the memory of the instance that
			// just wrote it: it is the serialization that is under test.
			got := openImageCache(t, path).Get("api:v1")

			if got == nil {
				t.Fatal("the entry did not come back")
			}
			switch {
			case tt.write == nil && got.Sensitive != nil:
				t.Errorf("Sensitive = %v, want nil", *got.Sensitive)
			case tt.write != nil && got.Sensitive == nil:
				t.Errorf("Sensitive = nil, want %v", *tt.write)
			case tt.write != nil && *got.Sensitive != *tt.write:
				t.Errorf("Sensitive = %v, want %v", *got.Sensitive, *tt.write)
			}
		})
	}
}
