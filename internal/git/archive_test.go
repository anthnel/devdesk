package git

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureRepo builds a repository with a committed file, a subdirectory, an
// ignored file and an uncommitted edit — everything ArchiveLocal must tell apart.
func fixtureRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	gitIn(t, dir, "init", "--quiet", "--initial-branch=main")
	write := func(name, body string, mode os.FileMode) {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	// `* -text` turns line-ending conversion off for this repository. Without
	// it a host with core.autocrlf=true (Windows) commits and archives
	// "committed\r\n", and the content assertions fail on the machine only.
	// The fixture lives in a fresh temp directory, so the repository's own
	// .gitattributes cannot reach it and it has to carry its own.
	write(".gitattributes", "* -text\n", 0o644)
	write("README.md", "committed\n", 0o644)
	write("app/main.go", "package main\n", 0o644)
	write(".gitignore", "secret.env\n", 0o644)
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "--quiet", "-m", "init")

	write("README.md", "edited but not committed\n", 0o644)
	write("secret.env", "TOKEN=1\n", 0o644)
	return dir
}

func names(t *testing.T, raw []byte) map[string]string {
	t.Helper()
	out := map[string]string{}
	tr := tar.NewReader(bytes.NewReader(raw))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		body, _ := io.ReadAll(tr)
		out[h.Name] = string(body)
	}
}

func TestArchiveLocalIsWhatWasCommitted(t *testing.T) {
	dir := fixtureRepo(t)

	raw, err := ArchiveLocal(context.Background(), dir, "", "", 0)
	if err != nil {
		t.Fatalf("ArchiveLocal() error = %v", err)
	}
	got := names(t, raw)

	if got["README.md"] != "committed\n" {
		t.Errorf("README.md = %q, want the committed content, not the working-tree edit", got["README.md"])
	}
	if _, ok := got["secret.env"]; ok {
		t.Error("an ignored, untracked file is in the template")
	}
	for name := range got {
		if name == ".git" || len(name) > 4 && name[:5] == ".git/" {
			t.Errorf("%q is in the template", name)
		}
	}
}

func TestArchiveLocalRootsAtASubdirectory(t *testing.T) {
	dir := fixtureRepo(t)

	raw, err := ArchiveLocal(context.Background(), dir, "", "app", 0)
	if err != nil {
		t.Fatalf("ArchiveLocal() error = %v", err)
	}
	got := names(t, raw)
	if len(got) != 1 || got["main.go"] == "" {
		t.Errorf("files = %v, want only main.go at the root", got)
	}
}

func TestArchiveLocalHonoursARef(t *testing.T) {
	dir := fixtureRepo(t)
	gitIn(t, dir, "tag", "v1")
	if err := os.WriteFile(filepath.Join(dir, "later.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "later.txt")
	gitIn(t, dir, "commit", "--quiet", "-m", "later")

	raw, err := ArchiveLocal(context.Background(), dir, "v1", "", 0)
	if err != nil {
		t.Fatalf("ArchiveLocal() error = %v", err)
	}
	if _, ok := names(t, raw)["later.txt"]; ok {
		t.Error("v1 contains a file committed after it")
	}
}

func TestArchiveLocalRefusesADirectoryThatIsNotARepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	if _, err := ArchiveLocal(context.Background(), t.TempDir(), "", "", 0); err == nil {
		t.Fatal("ArchiveLocal() accepted a plain directory")
	}
}

func TestArchiveRefusesAnOptionInPlaceOfARef(t *testing.T) {
	if _, err := ArchiveLocal(context.Background(), t.TempDir(), "--output=x", "", 0); err == nil {
		t.Error("ArchiveLocal() accepted a ref that git would read as an option")
	}
	if _, err := ArchiveRemote(context.Background(), "--upload-pack=id", "", "", "", 0); err == nil {
		t.Error("ArchiveRemote() accepted a URL that git would read as an option")
	}
}

// ArchiveRemote is exercised against a local path: git treats it as a remote
// for fetch, and it needs no network.
func TestArchiveRemoteFetchesABranchAndASHA(t *testing.T) {
	dir := fixtureRepo(t)
	sha := gitIn(t, dir, "rev-parse", "HEAD")
	sha = sha[:len(sha)-1]
	gitIn(t, dir, "config", "uploadpack.allowAnySHA1InWant", "true")

	for name, ref := range map[string]string{"default": "", "branch": "main", "sha": sha} {
		t.Run(name, func(t *testing.T) {
			raw, err := ArchiveRemote(context.Background(), dir, ref, "", "", 0)
			if err != nil {
				t.Fatalf("ArchiveRemote(%q) error = %v", ref, err)
			}
			if names(t, raw)["README.md"] != "committed\n" {
				t.Errorf("README.md is not the committed content")
			}
		})
	}
}

func TestArchiveLocalStopsAtTheByteLimit(t *testing.T) {
	dir := fixtureRepo(t)

	_, err := ArchiveLocal(context.Background(), dir, "", "", 512)
	if !errors.Is(err, ErrOutputTooLarge) {
		t.Fatalf("ArchiveLocal() error = %v, want ErrOutputTooLarge", err)
	}
}

func TestArchiveLocalRefusesASubmodule(t *testing.T) {
	dir := fixtureRepo(t)
	// A gitlink is what a submodule is in the tree; no second repository needed.
	gitIn(t, dir, "update-index", "--add", "--cacheinfo",
		"160000,1234567890123456789012345678901234567890,vendor/lib")
	gitIn(t, dir, "commit", "--quiet", "-m", "add a submodule")

	_, err := ArchiveLocal(context.Background(), dir, "", "", 0)
	if err == nil || !strings.Contains(err.Error(), "vendor/lib") {
		t.Fatalf("ArchiveLocal() error = %v, want a refusal naming the submodule", err)
	}
}
