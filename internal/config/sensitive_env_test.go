package config

import (
	"os"
	"strings"
	"testing"
)

// OC-12: the `containers.sensitive_env` list adds names or patterns to the
// built-in ones that mask environment values (S9). No default, no migration:
// an empty list is never written.

func writeSensitiveContext(t *testing.T, name, list string) string {
	t.Helper()
	return seedContextFile(t, name, append(plainContext(t), []byte("\ncontainers:\n  sensitive_env:\n"+list)...))
}

// Criterion 3: the list is read from the YAML, written back by Save, and the
// matcher built from it classifies the configured name.
func TestSensitiveEnv_RoundTrip(t *testing.T) {
	setupTmpHome(t)
	path := writeSensitiveContext(t, "default", "    - INTERNAL_URL\n    - ^CORP_\n")

	cfg, err := LoadContext("default")
	if err != nil {
		t.Fatalf("LoadContext: %v", err)
	}
	want := []string{"INTERNAL_URL", "^CORP_"}
	if got := cfg.Containers.SensitiveEnv; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("SensitiveEnv = %q, want %q", got, want)
	}
	m, err := cfg.Containers.SensitiveEnvMatcher()
	if err != nil {
		t.Fatalf("SensitiveEnvMatcher: %v", err)
	}
	if !m.Sensitive("INTERNAL_URL") || !m.Sensitive("CORP_REGION") {
		t.Error("configured names are not sensitive")
	}
	if !m.Sensitive("API_TOKEN") || m.Sensitive("LOG_LEVEL") {
		t.Error("defaults changed by the list")
	}

	// Add an entry, save through the plain Save (current context), reload.
	cfg.Containers.SensitiveEnv = append(cfg.Containers.SensitiveEnv, "_dsn$")
	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := readFile(t, path); !strings.Contains(got, "sensitive_env:") || !strings.Contains(got, "_dsn$") {
		t.Errorf("list not written by Save:\n%s", got)
	}
	again, err := LoadContext("default")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(again.Containers.SensitiveEnv, "|"); got != "INTERNAL_URL|^CORP_|_dsn$" {
		t.Errorf("after reload SensitiveEnv = %q", got)
	}
}

// Criterion 3: the list follows the active context, in both directions.
func TestSensitiveEnv_FollowsTheActiveContext(t *testing.T) {
	setupTmpHome(t)
	defPath := seedContextFile(t, "default", plainContext(t))
	writeSensitiveContext(t, "dev", "    - DEV_ONLY\n")
	prodPath := writeSensitiveContext(t, "prod", "    - PROD_ONLY\n")

	classify := func(cfg *Config, name string) bool {
		t.Helper()
		m, err := cfg.Containers.SensitiveEnvMatcher()
		if err != nil {
			t.Fatalf("SensitiveEnvMatcher: %v", err)
		}
		return m.Sensitive(name)
	}

	if err := SetCurrentContext("dev"); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !classify(cfg, "DEV_ONLY") || classify(cfg, "PROD_ONLY") {
		t.Error("with dev active, only DEV_ONLY should be sensitive")
	}

	if err := SetCurrentContext("prod"); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if !classify(cfg, "PROD_ONLY") || classify(cfg, "DEV_ONLY") {
		t.Error("with prod active, only PROD_ONLY should be sensitive")
	}

	// Save writes to the active context's file and to no other.
	cfg.Containers.SensitiveEnv = []string{"PROD_ONLY", "ANOTHER"}
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, prodPath); !strings.Contains(got, "ANOTHER") {
		t.Errorf("prod file lacks the saved entry:\n%s", got)
	}
	if got := readFile(t, defPath); strings.Contains(got, "sensitive_env") {
		t.Errorf("default file received the list:\n%s", got)
	}
}

// Criterion 4: an invalid pattern does not fail the load (a failed load would
// leave no config, hence less masking than before); the matcher builder
// returns an error that names the field, and a matcher that still carries the
// defaults and the valid entries.
func TestSensitiveEnv_InvalidPattern(t *testing.T) {
	setupTmpHome(t)
	writeSensitiveContext(t, "default", "    - INTERNAL_URL\n    - \"[bad\"\n")

	cfg, err := LoadContext("default")
	if err != nil {
		t.Fatalf("LoadContext must keep the context usable, got: %v", err)
	}
	m, err := cfg.Containers.SensitiveEnvMatcher()
	if err == nil {
		t.Fatal("no error for an invalid pattern")
	}
	msg := err.Error()
	if !strings.Contains(msg, "containers.sensitive_env") {
		t.Errorf("error does not name the field: %q", msg)
	}
	if !strings.Contains(msg, "[bad") {
		t.Errorf("error does not quote the pattern: %q", msg)
	}
	if m == nil {
		t.Fatal("nil matcher next to the error")
	}
	for _, name := range []string{"API_TOKEN", "db_password", "KEYBOARD_LAYOUT", "INTERNAL_URL"} {
		if !m.Sensitive(name) {
			t.Errorf("%s no longer masked after an invalid pattern", name)
		}
	}
	if m.Sensitive("LOG_LEVEL") {
		t.Error("LOG_LEVEL became sensitive")
	}
}

// Criterion 5: a context without the section loads without touching its file,
// and a later save does not invent the section.
func TestSensitiveEnv_AbsentSectionChangesNothing(t *testing.T) {
	setupTmpHome(t)
	path := seedContextFile(t, "default", plainContext(t))
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(before), "sensitive_env") || strings.Contains(string(before), "containers") {
		t.Fatalf("Default() already writes the section:\n%s", before)
	}

	cfg, err := LoadContext("default")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Containers.SensitiveEnv) != 0 {
		t.Errorf("SensitiveEnv = %q, want empty", cfg.Containers.SensitiveEnv)
	}
	if after := readFile(t, path); after != string(before) {
		t.Errorf("loading changed the file:\n%s", after)
	}

	m, err := cfg.Containers.SensitiveEnvMatcher()
	if err != nil {
		t.Fatalf("SensitiveEnvMatcher with no list: %v", err)
	}
	if !m.Sensitive("API_TOKEN") || m.Sensitive("INTERNAL_URL") {
		t.Error("an absent list must mean exactly the defaults")
	}

	// A save rewrites the file with the defaults a load fills in (source,
	// image_verification), which has nothing to do with this section: what
	// matters is that it adds none, and that a second save changes nothing.
	if err := SaveContext(cfg, "default"); err != nil {
		t.Fatal(err)
	}
	first := readFile(t, path)
	if strings.Contains(first, "sensitive_env") || strings.Contains(first, "containers") {
		t.Errorf("saving a context without the list invented the section:\n%s", first)
	}
	if err := SaveContext(cfg, "default"); err != nil {
		t.Fatal(err)
	}
	if again := readFile(t, path); again != first {
		t.Errorf("a second save changed the file:\n%s", again)
	}
}

// An emptied list is not written either (omitempty), so removing the last
// entry leaves a file as before the feature.
func TestSensitiveEnv_EmptiedListIsNotWritten(t *testing.T) {
	setupTmpHome(t)
	path := writeSensitiveContext(t, "default", "    - INTERNAL_URL\n")
	cfg, err := LoadContext("default")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Containers.SensitiveEnv = nil
	if err := SaveContext(cfg, "default"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); strings.Contains(got, "sensitive_env") || strings.Contains(got, "containers") {
		t.Errorf("empty list written:\n%s", got)
	}
}
