package scan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// AddToGitleaksIgnore writes into the repository being scanned, so these tests
// run against a real temporary directory rather than a stub — the file format
// is the contract gitleaks itself reads.

func readIgnore(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, ".gitleaksignore"))
	if err != nil {
		t.Fatalf("reading .gitleaksignore: %v", err)
	}
	return string(data)
}

// The fingerprint is gitleaks' own key, so dismissing a finding writes exactly
// that and nothing else.
func TestDismissingAFindingWritesItsFingerprint(t *testing.T) {
	dir := t.TempDir()

	err := AddToGitleaksIgnore(dir, Finding{
		Fingerprint: "config/prod.env:aws-access-token:42",
		File:        "config/prod.env",
		ID:          "aws-access-token",
		Line:        42,
	})
	if err != nil {
		t.Fatalf("dismissing failed: %v", err)
	}

	if got := readIgnore(t, dir); got != "config/prod.env:aws-access-token:42\n" {
		t.Errorf("the ignore file holds %q, want the fingerprint on its own line", got)
	}
}

// Findings cached before gitleaks reported fingerprints have none; the same
// key is rebuilt from the parts rather than writing a blank line, which would
// match nothing.
func TestAFindingWithoutAFingerprintGetsOneBuilt(t *testing.T) {
	dir := t.TempDir()

	if err := AddToGitleaksIgnore(dir, Finding{
		File: "config/prod.env",
		ID:   "aws-access-token",
		Line: 42,
	}); err != nil {
		t.Fatalf("dismissing failed: %v", err)
	}

	if got := strings.TrimSpace(readIgnore(t, dir)); got != "config/prod.env:aws-access-token:42" {
		t.Errorf("the rebuilt entry is %q, want file:rule:line", got)
	}
}

// The file accumulates: dismissing a second finding must not lose the first.
func TestDismissingAppendsToWhatIsThere(t *testing.T) {
	dir := t.TempDir()

	for _, fp := range []string{"a.env:rule-a:1", "b.env:rule-b:2"} {
		if err := AddToGitleaksIgnore(dir, Finding{Fingerprint: fp}); err != nil {
			t.Fatalf("dismissing %s failed: %v", fp, err)
		}
	}

	content := readIgnore(t, dir)
	for _, want := range []string{"a.env:rule-a:1", "b.env:rule-b:2"} {
		if !strings.Contains(content, want) {
			t.Errorf("%q is missing from:\n%s", want, content)
		}
	}
}

// Dismissing the same finding twice is a normal thing to do from a stale
// results table; it must not double the line or report an error.
func TestDismissingTwiceIsASilentNoOp(t *testing.T) {
	dir := t.TempDir()
	finding := Finding{Fingerprint: "config/prod.env:aws-access-token:42"}

	if err := AddToGitleaksIgnore(dir, finding); err != nil {
		t.Fatalf("first dismissal failed: %v", err)
	}
	if err := AddToGitleaksIgnore(dir, finding); err != nil {
		t.Fatalf("second dismissal reported an error: %v", err)
	}

	if got := strings.Count(readIgnore(t, dir), "aws-access-token"); got != 1 {
		t.Errorf("the entry appears %d times, want once", got)
	}
}

// An existing file written by hand keeps its contents and its trailing newline
// discipline.
func TestAnExistingIgnoreFileIsPreserved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitleaksignore")
	if err := os.WriteFile(path, []byte("# dismissed by hand\nold.env:rule:1\n"), 0o644); err != nil {
		t.Fatalf("seeding the ignore file: %v", err)
	}

	if err := AddToGitleaksIgnore(dir, Finding{Fingerprint: "new.env:rule:2"}); err != nil {
		t.Fatalf("dismissing failed: %v", err)
	}

	content := readIgnore(t, dir)
	for _, want := range []string{"# dismissed by hand", "old.env:rule:1", "new.env:rule:2"} {
		if !strings.Contains(content, want) {
			t.Errorf("%q is missing from:\n%s", want, content)
		}
	}
}

// A directory that cannot be written to has to report it, or the user is told a
// finding was dismissed when nothing was recorded.
func TestAnUnwritableTargetIsReported(t *testing.T) {
	// The path exists as a file, so creating .gitleaksignore beneath it fails
	// on every platform — unlike a read-only directory, which root ignores.
	notADir := filepath.Join(t.TempDir(), "repo")
	if err := os.WriteFile(notADir, nil, 0o644); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	if err := AddToGitleaksIgnore(notADir, Finding{Fingerprint: "x:y:1"}); err == nil {
		t.Error("dismissing into an unwritable target reported success")
	}
}
