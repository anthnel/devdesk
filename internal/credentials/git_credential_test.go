package credentials

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// hermeticGit points git at a throwaway HOME, config and credential store for
// the duration of the test. Without it these tests would write to the
// developer's real keychain or Git Credential Manager.
//
// The `store` helper is used with its default location rather than
// `--file=<path>`: git shell-parses the helper string, so a temporary path
// containing a space would break the invocation. Redirecting HOME keeps the
// path out of the command line entirely.
//
// It returns the path of the store file, which the tests read to assert on what
// git actually persisted.
func hermeticGit(t *testing.T) string {
	t.Helper()
	return hermeticGitWithHelper(t, "store")
}

// hermeticGitWithHelper is hermeticGit with the credential helper spelled out,
// so a test can install one that misbehaves.
func hermeticGitWithHelper(t *testing.T, helper string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}

	dir := t.TempDir()
	cfg := filepath.Join(dir, "gitconfig")
	if err := os.WriteFile(cfg, []byte("[credential]\n\thelper = "+helper+"\n"), 0o600); err != nil {
		t.Fatalf("writing git config: %v", err)
	}

	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir) // git falls back to this on Windows
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	return filepath.Join(dir, ".git-credentials")
}

// readStore returns the lines of the credential store, or nil when nothing has
// been written yet.
func readStore(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("reading credential store: %v", err)
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func TestNewGitCredentialStorageWithContextDefaultsContext(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty context falls back to default", "", "default"},
		{"named context is kept", "prod", "prod"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewGitCredentialStorageWithContext(tt.in).context; got != tt.want {
				t.Errorf("context = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGitCredentialRoundTrip(t *testing.T) {
	hermeticGit(t)
	s := NewGitCredentialStorageWithContext("default")

	if err := s.Save("https://gitlab.example.com", "tok-abc"); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := s.Load("https://gitlab.example.com")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got != "tok-abc" {
		t.Errorf("Load() = %q, want tok-abc", got)
	}
}

// TestGitCredentialIsolatesContexts is the regression test for the defect this
// storage was silently carrying: git drops the path field unless
// credential.useHttpPath is set, so both contexts were stored under
// https://gitlab.example.com and the last one saved won for every context.
func TestGitCredentialIsolatesContexts(t *testing.T) {
	store := hermeticGit(t)
	const host = "https://gitlab.example.com"

	dev := NewGitCredentialStorageWithContext("default")
	prod := NewGitCredentialStorageWithContext("prod")

	if err := dev.Save(host, "tok-default"); err != nil {
		t.Fatalf("Save(default) error = %v", err)
	}
	if err := prod.Save(host, "tok-prod"); err != nil {
		t.Fatalf("Save(prod) error = %v", err)
	}

	if lines := readStore(t, store); len(lines) != 2 {
		t.Errorf("credential store holds %d entries, want 2 — the contexts collapsed:\n%s",
			len(lines), strings.Join(lines, "\n"))
	}

	gotDev, err := dev.Load(host)
	if err != nil {
		t.Fatalf("Load(default) error = %v", err)
	}
	if gotDev != "tok-default" {
		t.Errorf("Load(default) = %q, want tok-default", gotDev)
	}

	gotProd, err := prod.Load(host)
	if err != nil {
		t.Fatalf("Load(prod) error = %v", err)
	}
	if gotProd != "tok-prod" {
		t.Errorf("Load(prod) = %q, want tok-prod", gotProd)
	}
}

func TestGitCredentialDeleteLeavesOtherContexts(t *testing.T) {
	hermeticGit(t)
	const host = "https://gitlab.example.com"

	dev := NewGitCredentialStorageWithContext("default")
	prod := NewGitCredentialStorageWithContext("prod")
	if err := dev.Save(host, "tok-default"); err != nil {
		t.Fatalf("Save(default) error = %v", err)
	}
	if err := prod.Save(host, "tok-prod"); err != nil {
		t.Fatalf("Save(prod) error = %v", err)
	}

	if err := dev.Delete(host); err != nil {
		t.Fatalf("Delete(default) error = %v", err)
	}

	if _, err := dev.Load(host); err == nil {
		t.Error("Load(default) succeeded after its credential was deleted")
	}
	got, err := prod.Load(host)
	if err != nil {
		t.Fatalf("Load(prod) after deleting another context error = %v", err)
	}
	if got != "tok-prod" {
		t.Errorf("Load(prod) = %q, want tok-prod", got)
	}
}

func TestGitCredentialSaveOverwritesSameContext(t *testing.T) {
	store := hermeticGit(t)
	s := NewGitCredentialStorageWithContext("default")

	if err := s.Save("https://gitlab.example.com", "old"); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := s.Save("https://gitlab.example.com", "new"); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := s.Load("https://gitlab.example.com")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got != "new" {
		t.Errorf("Load() = %q, want new", got)
	}
	if lines := readStore(t, store); len(lines) != 1 {
		t.Errorf("credential store holds %d entries, want 1 after an overwrite:\n%s",
			len(lines), strings.Join(lines, "\n"))
	}
}

func TestGitCredentialLoadReportsMissingCredential(t *testing.T) {
	hermeticGit(t)
	s := NewGitCredentialStorageWithContext("default")

	if _, err := s.Load("https://gitlab.example.com"); err == nil {
		t.Error("Load() with an empty store returned no error")
	}
}

func TestGitCredentialPreservesURLScheme(t *testing.T) {
	store := hermeticGit(t)
	s := NewGitCredentialStorageWithContext("default")

	// A self-hosted instance may be plain HTTP; storing it as HTTPS would make
	// the credential unreachable on load.
	if err := s.Save("http://gitlab.internal:8080", "tok"); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	lines := readStore(t, store)
	if len(lines) != 1 {
		t.Fatalf("credential store holds %d entries, want 1", len(lines))
	}
	if !strings.HasPrefix(lines[0], "http://") {
		t.Errorf("stored entry = %q, want it to keep the http:// scheme", lines[0])
	}

	got, err := s.Load("http://gitlab.internal:8080")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got != "tok" {
		t.Errorf("Load() = %q, want tok", got)
	}
}

func TestGitCredentialSeparatesPortsOnTheSameHost(t *testing.T) {
	hermeticGit(t)
	s := NewGitCredentialStorageWithContext("default")

	// The port is part of the host field, so two instances on one machine must
	// not share a credential.
	if err := s.Save("http://gitlab.internal:8080", "tok-8080"); err != nil {
		t.Fatalf("Save(:8080) error = %v", err)
	}
	if err := s.Save("http://gitlab.internal:9090", "tok-9090"); err != nil {
		t.Fatalf("Save(:9090) error = %v", err)
	}

	got, err := s.Load("http://gitlab.internal:8080")
	if err != nil {
		t.Fatalf("Load(:8080) error = %v", err)
	}
	if got != "tok-8080" {
		t.Errorf("Load(:8080) = %q, want tok-8080", got)
	}
}

func TestGitCredentialRejectsMalformedURL(t *testing.T) {
	s := NewGitCredentialStorageWithContext("default")
	const bad = "://not-a-url"

	if err := s.Save(bad, "tok"); err == nil {
		t.Error("Save() with a malformed URL returned no error")
	}
	if _, err := s.Load(bad); err == nil {
		t.Error("Load() with a malformed URL returned no error")
	}
	if err := s.Delete(bad); err == nil {
		t.Error("Delete() with a malformed URL returned no error")
	}
}

// TestGitCredentialTimesOutOnAHangingHelper covers the reason every call is
// bounded: a credential helper that waits forever — because it is misconfigured
// or wants input on a terminal DevDesk does not own — must not freeze the TUI.
func TestGitCredentialTimesOutOnAHangingHelper(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "hang.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 30\n"), 0o700); err != nil {
		t.Fatalf("writing helper script: %v", err)
	}
	hermeticGitWithHelper(t, "!"+filepath.ToSlash(script))

	s := NewGitCredentialStorageWithContext("default")
	start := time.Now()
	_, err := s.Load("https://gitlab.example.com")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Load() against a hanging helper returned no error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error = %v, want it to report a timeout", err)
	}
	// The helper sleeps for 30s; anything near that means the bound did not hold.
	if limit := credentialTimeout + 3*time.Second; elapsed > limit {
		t.Errorf("Load() took %v, want it bounded under %v", elapsed, limit)
	}
}

func TestParsePassword(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
		wantOK bool
	}{
		{
			name:   "typical fill reply",
			output: "protocol=https\nhost=gitlab.example.com\nusername=oauth2\npassword=tok-abc\n",
			want:   "tok-abc",
			wantOK: true,
		},
		{
			name:   "carriage returns are trimmed",
			output: "username=oauth2\r\npassword=tok-abc\r\n",
			want:   "tok-abc",
			wantOK: true,
		},
		{
			name:   "token containing an equals sign survives",
			output: "password=a=b=c\n",
			want:   "a=b=c",
			wantOK: true,
		},
		{
			name:   "empty password is still a reply",
			output: "password=\n",
			want:   "",
			wantOK: true,
		},
		{
			name:   "no password field",
			output: "protocol=https\nhost=gitlab.example.com\n",
			wantOK: false,
		},
		{
			name:   "empty output",
			output: "",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parsePassword(tt.output)
			if ok != tt.wantOK {
				t.Fatalf("parsePassword() ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("parsePassword() = %q, want %q", got, tt.want)
			}
		})
	}
}
