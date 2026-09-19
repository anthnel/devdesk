package template

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// committedRepo is a local repository holding one committed file.
func committedRepo(t *testing.T, content string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	run(t, dir, "init", "--quiet", "--initial-branch=main")
	commitFile(t, dir, content)
	return dir
}

func commitFile(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "add", "README.md")
	run(t, dir, "commit", "--quiet", "-m", content)
}

func readme(t *testing.T, files []File) string {
	t.Helper()
	f, ok := byPath(files)["README.md"]
	if !ok {
		t.Fatalf("no README.md in %v", files)
	}
	return string(f.Content)
}

// TestCacheServesTheFirstReadUntilSync is the contract of `F`: a source that
// moved on is not noticed by a read, and is by a sync.
func TestCacheServesTheFirstReadUntilSync(t *testing.T) {
	repo := committedRepo(t, "one")
	src := Source{Kind: KindLocal, Path: repo}
	c := NewCacheAt(t.TempDir())
	ctx := context.Background()

	files, err := c.Fetch(ctx, "demo", src, Credentials{})
	if err != nil {
		t.Fatal(err)
	}
	if got := readme(t, files); got != "one" {
		t.Fatalf("first read = %q", got)
	}

	commitFile(t, repo, "two")

	files, err = c.Fetch(ctx, "demo", src, Credentials{})
	if err != nil {
		t.Fatal(err)
	}
	if got := readme(t, files); got != "one" {
		t.Errorf("read after a new commit = %q, want the cached %q", got, "one")
	}

	files, err = c.Sync(ctx, "demo", src, Credentials{})
	if err != nil {
		t.Fatal(err)
	}
	if got := readme(t, files); got != "two" {
		t.Errorf("sync = %q, want %q", got, "two")
	}
	files, _ = c.Fetch(ctx, "demo", src, Credentials{})
	if got := readme(t, files); got != "two" {
		t.Errorf("read after sync = %q, want %q", got, "two")
	}
}

// TestCacheDoesNotAnswerForAnotherSource: editing a template to point elsewhere
// must not keep serving what it used to point at.
func TestCacheDoesNotAnswerForAnotherSource(t *testing.T) {
	a := committedRepo(t, "from a")
	b := committedRepo(t, "from b")
	c := NewCacheAt(t.TempDir())
	ctx := context.Background()

	if _, err := c.Fetch(ctx, "demo", Source{Kind: KindLocal, Path: a}, Credentials{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.FetchedAt("demo", Source{Kind: KindLocal, Path: b}); ok {
		t.Error("FetchedAt answered for a source that was never read")
	}
	files, err := c.Fetch(ctx, "demo", Source{Kind: KindLocal, Path: b}, Credentials{})
	if err != nil {
		t.Fatal(err)
	}
	if got := readme(t, files); got != "from b" {
		t.Errorf("read = %q, want %q", got, "from b")
	}
}

// TestFailedSyncKeepsThePreviousCopy: an outage must not cost the template it
// last managed to read.
func TestFailedSyncKeepsThePreviousCopy(t *testing.T) {
	repo := committedRepo(t, "one")
	src := Source{Kind: KindLocal, Path: repo}
	c := NewCacheAt(t.TempDir())
	ctx := context.Background()

	if _, err := c.Sync(ctx, "demo", src, Credentials{}); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Sync(ctx, "demo", src, Credentials{}); err == nil {
		t.Fatal("Sync() of a vanished source succeeded")
	}
	files, err := c.Fetch(ctx, "demo", src, Credentials{})
	if err != nil {
		t.Fatalf("Fetch() after a failed sync: %v", err)
	}
	if got := readme(t, files); got != "one" {
		t.Errorf("read = %q, want the previous copy", got)
	}
}

func TestCacheTreatsACorruptFileAsAMiss(t *testing.T) {
	repo := committedRepo(t, "one")
	src := Source{Kind: KindLocal, Path: repo}
	dir := t.TempDir()
	c := NewCacheAt(dir)

	if err := os.WriteFile(filepath.Join(dir, "demo.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	files, err := c.Fetch(context.Background(), "demo", src, Credentials{})
	if err != nil {
		t.Fatal(err)
	}
	if got := readme(t, files); got != "one" {
		t.Errorf("read = %q", got)
	}
}

func TestCacheRefusesASlugThatIsNotOne(t *testing.T) {
	c := NewCacheAt(t.TempDir())
	if err := c.Forget("../escape"); err == nil {
		t.Error("Forget() accepted a path as a slug")
	}
	if _, ok := c.FetchedAt("../escape", Source{}); ok {
		t.Error("FetchedAt() answered for a path as a slug")
	}
}

func TestForgetDropsTheCopy(t *testing.T) {
	repo := committedRepo(t, "one")
	src := Source{Kind: KindLocal, Path: repo}
	c := NewCacheAt(t.TempDir())

	if _, err := c.Sync(context.Background(), "demo", src, Credentials{}); err != nil {
		t.Fatal(err)
	}
	if err := c.Forget("demo"); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.FetchedAt("demo", src); ok {
		t.Error("the copy survived Forget()")
	}
	if err := c.Forget("demo"); err != nil {
		t.Errorf("Forget() of a template never cached: %v", err)
	}
}

func TestAnEmptyCacheAlwaysFetches(t *testing.T) {
	repo := committedRepo(t, "one")
	files, err := Cache{}.Fetch(context.Background(), "demo", Source{Kind: KindLocal, Path: repo}, Credentials{})
	if err != nil {
		t.Fatal(err)
	}
	if got := readme(t, files); got != "one" {
		t.Errorf("read = %q", got)
	}
}
