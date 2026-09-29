package fsbrowse

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestValidName(t *testing.T) {
	cases := []struct {
		name string
		want error
	}{
		{"notes.md", nil},
		{".env", nil},
		{"with space", nil},
		{"", ErrEmptyName},
		{"   ", ErrEmptyName},
		{".", ErrReservedName},
		{"..", ErrReservedName},
		{"a/b", ErrSeparatorName},
		{"../x", ErrSeparatorName},
		{"a\x00b", ErrInvalidChar},
	}
	for _, c := range cases {
		if got := ValidName(c.name); !errors.Is(got, c.want) {
			t.Errorf("ValidName(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestCreateMakesAFileOrADirectoryAndNeverOverwrites(t *testing.T) {
	dir := t.TempDir()

	file, err := Create(dir, "a.txt", false)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	if info, err := os.Stat(file); err != nil || info.IsDir() {
		t.Fatalf("want a regular file at %s, got %v %v", file, info, err)
	}
	sub, err := Create(dir, "sub", true)
	if err != nil {
		t.Fatalf("create dir: %v", err)
	}
	if info, err := os.Stat(sub); err != nil || !info.IsDir() {
		t.Fatalf("want a directory at %s", sub)
	}

	if _, err := Create(dir, "a.txt", false); !errors.Is(err, os.ErrExist) {
		t.Errorf("an existing file must be refused, got %v", err)
	}
	if _, err := Create(dir, "sub", true); !errors.Is(err, os.ErrExist) {
		t.Errorf("an existing directory must be refused, got %v", err)
	}
	if _, err := Create(dir, "../escape", true); !errors.Is(err, ErrSeparatorName) {
		t.Errorf("a separator must be refused, got %v", err)
	}
}

func TestListPutsDirectoriesFirstHidesDotFilesAndFollowsLinks(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"b.txt", "A.txt", ".hidden"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "zdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(dir, "link")
	if err := os.Symlink(filepath.Join(dir, "zdir"), linked); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	entries, err := List(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name)
	}
	want := []string{"link", "zdir", "A.txt", "b.txt"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if !entries[0].IsDir {
		t.Error("a link to a directory must list as a directory")
	}

	all, _ := List(dir, true)
	if len(all) != 5 {
		t.Errorf("showHidden must list the dot file, got %d entries", len(all))
	}
}

func TestRemoveRefusesARootAndHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	for _, p := range []string{string(filepath.Separator), home} {
		if err := Remove(p); !errors.Is(err, ErrProtectedPath) {
			t.Errorf("Remove(%q) = %v, want ErrProtectedPath", p, err)
		}
	}

	victim := filepath.Join(home, "gone")
	if err := os.MkdirAll(filepath.Join(victim, "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Remove(victim); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(victim); !os.IsNotExist(err) {
		t.Error("the directory must be gone")
	}
}

func TestNearestExistingWalksUpToAnAncestor(t *testing.T) {
	dir := t.TempDir()
	if got := NearestExisting(filepath.Join(dir, "missing", "deeper")); got != dir {
		t.Errorf("got %q, want %q", got, dir)
	}
	if got := NearestExisting(""); got != "" {
		t.Errorf("empty path must give empty, got %q", got)
	}
}

func TestExpandHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	cases := map[string]string{
		"~":          home,
		"~/x/y":      filepath.Join(home, "x", "y"),
		"/abs":       "/abs",
		"~other/x":   "~other/x",
		"relative/x": "relative/x",
	}
	for in, want := range cases {
		if got := ExpandHome(in); got != want {
			t.Errorf("ExpandHome(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHumanSize(t *testing.T) {
	cases := map[int64]string{0: "0 B", 1023: "1023 B", 1024: "1.0 KiB", 1536: "1.5 KiB", 1 << 20: "1.0 MiB"}
	for in, want := range cases {
		if got := HumanSize(in); got != want {
			t.Errorf("HumanSize(%d) = %q, want %q", in, got, want)
		}
	}
}
