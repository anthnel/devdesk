package template

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// inTempHome points the home directory at a temporary one, so nothing here
// touches the real ~/.devdesk.
func inTempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func TestMaterializeWritesTheFilesAndTheExecuteBit(t *testing.T) {
	home := inTempHome(t)

	dir, err := Materialize("spring-api", []File{
		{Path: "README.md", Content: []byte("hello")},
		{Path: "src/main/App.java", Content: []byte("class App {}")},
		{Path: "mvnw", Content: []byte("#!/bin/sh"), Executable: true},
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}

	if want := filepath.Join(home, ".devdesk", "cache", "template-scan", "spring-api"); dir != want {
		t.Errorf("dir = %q, want %q", dir, want)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "src", "main", "App.java")); string(got) != "class App {}" {
		t.Errorf("nested file = %q", got)
	}
	if runtime.GOOS != "windows" { // Windows has no execute bit to read back
		info, _ := os.Stat(filepath.Join(dir, "mvnw"))
		if info.Mode()&0o100 == 0 {
			t.Errorf("mvnw mode = %v, want the execute bit", info.Mode())
		}
	}
}

// A second scan replaces the first's directory: a file the template no longer
// has must not stay behind to be scanned.
func TestMaterializeReplacesWhatWasThere(t *testing.T) {
	inTempHome(t)
	dir, _ := Materialize("api", []File{{Path: "old.txt", Content: []byte("x")}})

	if _, err := Materialize("api", []File{{Path: "new.txt", Content: []byte("y")}}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dir, "old.txt")); err == nil {
		t.Error("a file from the previous scan is still there")
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); err != nil {
		t.Errorf("the new file is missing: %v", err)
	}
}

func TestMaterializeRefusesAPathOutsideItsDirectory(t *testing.T) {
	home := inTempHome(t)

	for _, path := range []string{"../escape.txt", "a/../../escape.txt", ".."} {
		if _, err := Materialize("api", []File{{Path: path, Content: []byte("x")}}); err == nil {
			t.Errorf("Materialize() accepted %q", path)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".devdesk", "cache", "template-scan", "escape.txt")); err == nil {
		t.Error("a file escaped its directory")
	}
}

// The directory removed is computed from the slug, so a slug that could climb
// out of the cache is refused before anything is removed.
func TestMaterializeRefusesASlugThatIsNotASlug(t *testing.T) {
	home := inTempHome(t)
	victim := filepath.Join(home, "precious")
	_ = os.MkdirAll(victim, 0o700)

	for _, slug := range []string{"", "..", "../../precious", "a/b", "A"} {
		if _, err := Materialize(slug, nil); err == nil {
			t.Errorf("Materialize(%q) was accepted", slug)
		}
	}
	if _, err := os.Stat(victim); err != nil {
		t.Errorf("a directory outside the cache was touched: %v", err)
	}
}
