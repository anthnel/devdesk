package dockerfile

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

func write(t *testing.T, root, rel string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("FROM alpine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIsDockerfileName(t *testing.T) {
	yes := []string{"Dockerfile", "dockerfile", "Dockerfile.dev", "api.Dockerfile", "Containerfile", "Containerfile.prod"}
	no := []string{"README.md", "docker-compose.yml", "Makefile", "mydockerfile", "dockerfiles"}
	for _, name := range yes {
		if !IsDockerfileName(name) {
			t.Errorf("%q is a Dockerfile name", name)
		}
	}
	for _, name := range no {
		if IsDockerfileName(name) {
			t.Errorf("%q is not a Dockerfile name", name)
		}
	}
}

func TestFindListsDockerfilesInPathOrder(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"Dockerfile", "svc/api/Dockerfile.dev", "svc/web/web.Dockerfile", "docs/README.md", "svc/Containerfile"} {
		write(t, root, rel)
	}
	got, truncated, err := Find(root)
	if err != nil || truncated {
		t.Fatalf("Find: %v, truncated %v", err, truncated)
	}
	want := []string{"Dockerfile", "svc/Containerfile", "svc/api/Dockerfile.dev", "svc/web/web.Dockerfile"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Find = %v, want %v", got, want)
	}
}

func TestFindSkipsWhatAPackageManagerPut(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"Dockerfile", "node_modules/x/Dockerfile", "vendor/y/Dockerfile", ".git/hooks/Dockerfile", ".worktrees/b/Dockerfile"} {
		write(t, root, rel)
	}
	got, _, err := Find(root)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if want := []string{"Dockerfile"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Find = %v, want %v", got, want)
	}
}

func TestFindStopsAtADepthBound(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a/b/c/d/Dockerfile") // four directories down: read
	write(t, root, "a/b/c/d/e/Dockerfile")
	got, _, err := Find(root)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if want := []string{"a/b/c/d/Dockerfile"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Find = %v, want %v", got, want)
	}
}

func TestFindBoundsTheAnswer(t *testing.T) {
	root := t.TempDir()
	for i := range maxFound + 5 {
		write(t, root, "svc"+strconv.Itoa(1000+i)+"/Dockerfile")
	}
	got, truncated, err := Find(root)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(got) != maxFound || !truncated {
		t.Errorf("got %d paths, truncated %v; want %d, true", len(got), truncated, maxFound)
	}
}

func TestFindOfAMissingRootIsAnError(t *testing.T) {
	if _, _, err := Find(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Error("a missing root was not reported")
	}
}
