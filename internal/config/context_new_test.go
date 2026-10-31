package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// ── ValidateNewContextName ───────────────────────────────────────────────────

func TestValidateNewContextName_ReservedNames(t *testing.T) {
	for _, name := range []string{"-", "new", "list"} {
		t.Run(name, func(t *testing.T) {
			err := ValidateNewContextName(name)
			if !errors.Is(err, ErrReservedContextName) {
				t.Errorf("ValidateNewContextName(%q) = %v, want ErrReservedContextName", name, err)
			}
		})
	}
}

func TestValidateNewContextName_AppliesExistingFormat(t *testing.T) {
	for _, name := range []string{"Invalid!", "UPPER", "with space", ""} {
		t.Run(name, func(t *testing.T) {
			err := ValidateNewContextName(name)
			if err == nil {
				t.Fatalf("ValidateNewContextName(%q) = nil, want error", name)
			}
			if errors.Is(err, ErrReservedContextName) {
				t.Errorf("ValidateNewContextName(%q) reported reserved, want a format error", name)
			}
		})
	}
}

func TestValidateNewContextName_AcceptsOrdinaryNames(t *testing.T) {
	for _, name := range []string{"default", "dev", "client-a", "ctx123", "newer", "listing"} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateNewContextName(name); err != nil {
				t.Errorf("ValidateNewContextName(%q) = %v, want nil", name, err)
			}
		})
	}
}

// ── CreateContext ────────────────────────────────────────────────────────────

func TestCreateContext_ReservedNameCreatesNothing(t *testing.T) {
	home := setupTmpHome(t)
	for _, name := range []string{"-", "new", "list"} {
		err := CreateContext(name)
		if !errors.Is(err, ErrReservedContextName) {
			t.Errorf("CreateContext(%q) = %v, want ErrReservedContextName", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".devdesk")); !os.IsNotExist(err) {
		t.Errorf("a reserved name must not create ~/.devdesk, stat err = %v", err)
	}
}

func TestCreateContext_ExistingWrapsErrContextExistsAndKeepsFile(t *testing.T) {
	setupTmpHome(t)
	if err := CreateContext("dup"); err != nil {
		t.Fatalf("first CreateContext error: %v", err)
	}
	path, err := GetContextPath("dup")
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := CreateContext("dup"); !errors.Is(err, ErrContextExists) {
		t.Fatalf("second CreateContext = %v, want ErrContextExists", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("an existing context file was modified by a refused CreateContext")
	}
}

// ── LoadContext ──────────────────────────────────────────────────────────────

func TestLoadContext_MissingWrapsErrContextNotFound(t *testing.T) {
	setupTmpHome(t)
	if _, err := LoadContext("ghost"); !errors.Is(err, ErrContextNotFound) {
		t.Errorf("LoadContext(ghost) = %v, want ErrContextNotFound", err)
	}
}

// ── Non-regression: reserved names stay loadable when a file exists ──────────

func TestReservedNamesStillLoadFromExistingFiles(t *testing.T) {
	setupTmpHome(t)
	for _, name := range []string{"list", "new"} {
		if err := SaveContext(Default(), name); err != nil {
			t.Fatalf("SaveContext(%q): %v", name, err)
		}
		if err := ValidateContextName(name); err != nil {
			t.Errorf("ValidateContextName(%q) = %v, must stay valid", name, err)
		}
		if _, err := LoadContext(name); err != nil {
			t.Errorf("LoadContext(%q) = %v, must keep working", name, err)
		}
		if err := SetCurrentContext(name); err != nil {
			t.Errorf("SetCurrentContext(%q) = %v, must keep working", name, err)
		}
		if got, err := GetCurrentContext(); err != nil || got != name {
			t.Errorf("GetCurrentContext() = %q, %v; want %q", got, err, name)
		}
	}
}
