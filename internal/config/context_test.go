package config

import (
	"os"
	"path/filepath"
	"testing"
)

// setupTmpHome redirects os.UserHomeDir() to a fresh temp dir and restores on cleanup.
// Sets both HOME (Unix/macOS) and USERPROFILE (Windows) so the redirect works cross-platform.
func setupTmpHome(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("USERPROFILE", tmpDir)
	return tmpDir
}

// ── ValidateContextName ──────────────────────────────────────────────────────

func TestValidateContextName_Valid(t *testing.T) {
	cases := []string{"default", "dev", "prod", "client-a", "ctx123", "a-b-c"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateContextName(name); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateContextName_Invalid(t *testing.T) {
	cases := []string{"Dev", "PROD", "ctx_1", "ctx.x", "Ctx", "", "a b", "abc!"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateContextName(name); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

// ── GetContextPath ───────────────────────────────────────────────────────────

func TestGetContextPath_Default(t *testing.T) {
	tmpDir := setupTmpHome(t)
	path, err := GetContextPath("default")
	if err != nil {
		t.Fatalf("GetContextPath('default') error: %v", err)
	}
	expected := filepath.Join(tmpDir, ".devdesk", "config.yaml")
	if slash(path) != slash(expected) {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

func TestGetContextPath_Named(t *testing.T) {
	tmpDir := setupTmpHome(t)
	path, err := GetContextPath("dev")
	if err != nil {
		t.Fatalf("GetContextPath('dev') error: %v", err)
	}
	expected := filepath.Join(tmpDir, ".devdesk", "config-dev.yaml")
	if slash(path) != slash(expected) {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

// ── GetCurrentContext / SetCurrentContext ────────────────────────────────────

func TestGetCurrentContext_DefaultWhenMissing(t *testing.T) {
	setupTmpHome(t)
	ctx, err := GetCurrentContext()
	if err != nil {
		t.Fatalf("GetCurrentContext() error: %v", err)
	}
	if ctx != "default" {
		t.Errorf("expected 'default', got %q", ctx)
	}
}

func TestSetAndGetCurrentContext(t *testing.T) {
	setupTmpHome(t)

	if err := SetCurrentContext("dev"); err != nil {
		t.Fatalf("SetCurrentContext('dev') error: %v", err)
	}
	ctx, err := GetCurrentContext()
	if err != nil {
		t.Fatalf("GetCurrentContext() error: %v", err)
	}
	if ctx != "dev" {
		t.Errorf("expected 'dev', got %q", ctx)
	}
}

func TestSetCurrentContext_InvalidName(t *testing.T) {
	setupTmpHome(t)
	if err := SetCurrentContext("Invalid_Name"); err == nil {
		t.Error("expected error for invalid context name, got nil")
	}
}

func TestGetCurrentContext_WhitespaceFile(t *testing.T) {
	tmpDir := setupTmpHome(t)
	configDir := filepath.Join(tmpDir, ".devdesk")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	// File contains only whitespace — should be treated as "default" after TrimSpace
	if err := os.WriteFile(filepath.Join(configDir, ".current-context"), []byte("   "), 0600); err != nil {
		t.Fatal(err)
	}

	ctx, err := GetCurrentContext()
	if err != nil {
		t.Fatalf("GetCurrentContext() error: %v", err)
	}
	if ctx != "default" {
		t.Errorf("expected 'default' for whitespace-only file, got %q", ctx)
	}
}

func TestGetCurrentContext_TrulyEmptyFile(t *testing.T) {
	tmpDir := setupTmpHome(t)
	configDir := filepath.Join(tmpDir, ".devdesk")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, ".current-context"), []byte(""), 0600); err != nil {
		t.Fatal(err)
	}

	ctx, err := GetCurrentContext()
	if err != nil {
		t.Fatalf("GetCurrentContext() error: %v", err)
	}
	if ctx != "default" {
		t.Errorf("expected 'default' for empty file, got %q", ctx)
	}
}

// ── ListContexts ─────────────────────────────────────────────────────────────

func TestListContexts_EmptyDir(t *testing.T) {
	setupTmpHome(t)
	contexts, err := ListContexts()
	if err != nil {
		t.Fatalf("ListContexts() error: %v", err)
	}
	if len(contexts) != 0 {
		t.Errorf("expected empty context list, got %v", contexts)
	}
}

func TestListContexts_WithDefault(t *testing.T) {
	tmpDir := setupTmpHome(t)
	configDir := filepath.Join(tmpDir, ".devdesk")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Create default config
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(""), 0600); err != nil {
		t.Fatal(err)
	}

	contexts, err := ListContexts()
	if err != nil {
		t.Fatalf("ListContexts() error: %v", err)
	}
	if len(contexts) != 1 || contexts[0] != "default" {
		t.Errorf("expected [default], got %v", contexts)
	}
}

func TestListContexts_MultipleContexts(t *testing.T) {
	tmpDir := setupTmpHome(t)
	configDir := filepath.Join(tmpDir, ".devdesk")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"config.yaml", "config-dev.yaml", "config-prod.yaml"} {
		if err := os.WriteFile(filepath.Join(configDir, f), []byte(""), 0600); err != nil {
			t.Fatal(err)
		}
	}

	contexts, err := ListContexts()
	if err != nil {
		t.Fatalf("ListContexts() error: %v", err)
	}
	if len(contexts) != 3 {
		t.Errorf("expected 3 contexts, got %v", contexts)
	}
	want := map[string]bool{"default": true, "dev": true, "prod": true}
	for _, ctx := range contexts {
		if !want[ctx] {
			t.Errorf("unexpected context name %q in list", ctx)
		}
		delete(want, ctx)
	}
	for missing := range want {
		t.Errorf("context %q not found in list", missing)
	}
}

// ── ContextExists ─────────────────────────────────────────────────────────────

func TestContextExists_NotFound(t *testing.T) {
	setupTmpHome(t)
	exists, err := ContextExists("dev")
	if err != nil {
		t.Fatalf("ContextExists() error: %v", err)
	}
	if exists {
		t.Error("expected false for non-existent context")
	}
}

func TestContextExists_Found(t *testing.T) {
	tmpDir := setupTmpHome(t)
	configDir := filepath.Join(tmpDir, ".devdesk")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config-staging.yaml"), []byte(""), 0600); err != nil {
		t.Fatal(err)
	}

	exists, err := ContextExists("staging")
	if err != nil {
		t.Fatalf("ContextExists() error: %v", err)
	}
	if !exists {
		t.Error("expected true for existing context")
	}
}

func TestContextExists_InvalidName(t *testing.T) {
	setupTmpHome(t)
	_, err := ContextExists("Invalid!")
	if err == nil {
		t.Error("expected error for invalid context name, got nil")
	}
}

// ── SetCurrentContext / LoadContext ──────────────────────────────────────────

func TestLoadContext_Default(t *testing.T) {
	setupTmpHome(t)
	// Save a minimal config first
	cfg := Default()
	cfg.App.Theme = "light"
	if err := Save(cfg); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	loaded, err := LoadContext("default")
	if err != nil {
		t.Fatalf("LoadContext('default') error: %v", err)
	}
	if loaded.App.Theme != "light" {
		t.Errorf("expected theme 'light', got %q", loaded.App.Theme)
	}
}

func TestLoadContext_NotExist(t *testing.T) {
	setupTmpHome(t)
	_, err := LoadContext("ghost")
	if err == nil {
		t.Error("expected error for non-existent context, got nil")
	}
}

func TestLoadContext_InvalidName(t *testing.T) {
	setupTmpHome(t)
	_, err := LoadContext("Bad_Name")
	if err == nil {
		t.Error("expected error for invalid context name, got nil")
	}
}

// ── CreateContext ─────────────────────────────────────────────────────────────

func TestCreateContext_New(t *testing.T) {
	setupTmpHome(t)
	if err := CreateContext("myctx"); err != nil {
		t.Fatalf("CreateContext('myctx') error: %v", err)
	}
	exists, err := ContextExists("myctx")
	if err != nil {
		t.Fatalf("ContextExists() error: %v", err)
	}
	if !exists {
		t.Error("context should exist after CreateContext()")
	}
}

func TestCreateContext_AlreadyExists(t *testing.T) {
	setupTmpHome(t)
	if err := CreateContext("dup"); err != nil {
		t.Fatalf("first CreateContext('dup') error: %v", err)
	}
	if err := CreateContext("dup"); err == nil {
		t.Error("expected error when creating duplicate context, got nil")
	}
}

func TestCreateContext_InvalidName(t *testing.T) {
	setupTmpHome(t)
	if err := CreateContext("Invalid!"); err == nil {
		t.Error("expected error for invalid context name, got nil")
	}
}

// ── SaveContext ───────────────────────────────────────────────────────────────

func TestSaveContext_NamedContext(t *testing.T) {
	tmpDir := setupTmpHome(t)
	cfg := Default()
	cfg.App.Theme = "custom-light"

	if err := SaveContext(cfg, "staging"); err != nil {
		t.Fatalf("SaveContext() error: %v", err)
	}

	configPath := filepath.Join(tmpDir, ".devdesk", "config-staging.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("expected config-staging.yaml to be created")
	}
}

func TestSaveContext_InvalidName(t *testing.T) {
	setupTmpHome(t)
	if err := SaveContext(Default(), "BAD"); err == nil {
		t.Error("expected error for invalid context name, got nil")
	}
}

// ── ExpandPaths ───────────────────────────────────────────────────────────────

func TestExpandPaths_Tilde(t *testing.T) {
	cfg := &Config{
		App: AppConfig{
			WorkspacesDir: "~/projects",
			LogFile:       "~/logs/app.log",
		},
	}
	cfg.ExpandPaths("/home/user")
	if slash(cfg.App.WorkspacesDir) != "/home/user/projects" {
		t.Errorf("expected '/home/user/projects', got %q", cfg.App.WorkspacesDir)
	}
	if slash(cfg.App.LogFile) != "/home/user/logs/app.log" {
		t.Errorf("expected '/home/user/logs/app.log', got %q", cfg.App.LogFile)
	}
}

func TestExpandPaths_AbsoluteUnchanged(t *testing.T) {
	cfg := &Config{
		App: AppConfig{WorkspacesDir: "/absolute/path"},
	}
	cfg.ExpandPaths("/home/user")
	if slash(cfg.App.WorkspacesDir) != "/absolute/path" {
		t.Errorf("absolute path should be unchanged, got %q", cfg.App.WorkspacesDir)
	}
}
