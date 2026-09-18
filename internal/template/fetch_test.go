package template

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func byPath(files []File) map[string]File {
	m := make(map[string]File, len(files))
	for _, f := range files {
		m[f.Path] = f
	}
	return m
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestFetchLocalKeepsBinariesAndTheExecuteBit is the whole point of carrying
// bytes and a mode: a wrapper jar and mvnw must reach the new repository as
// they were committed.
func TestFetchLocalKeepsBinariesAndTheExecuteBit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	run(t, dir, "init", "--quiet", "--initial-branch=main")
	binary := []byte{0x00, 0xff, 0xfe, 0x80, 'P', 'K'}
	if err := os.WriteFile(filepath.Join(dir, "wrapper.jar"), binary, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mvnw"), []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "add", "wrapper.jar")
	// --chmod rather than os.Chmod: the mode git records must not depend on the
	// filesystem the test happens to run on (Windows has no execute bit).
	run(t, dir, "add", "--chmod=+x", "mvnw")
	run(t, dir, "commit", "--quiet", "-m", "init")

	files, err := Fetch(context.Background(), Source{Kind: KindLocal, Path: dir}, Credentials{})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	got := byPath(files)

	if !bytes.Equal(got["wrapper.jar"].Content, binary) || got["wrapper.jar"].Executable {
		t.Errorf("wrapper.jar = %+v, want the bytes intact and no execute bit", got["wrapper.jar"])
	}
	if !got["mvnw"].Executable {
		t.Errorf("mvnw = %+v, want the execute bit", got["mvnw"])
	}
}

func TestFetchRefusesAnInvalidSource(t *testing.T) {
	_, err := Fetch(context.Background(), Source{Kind: KindGit, URL: "ext::sh -c id"}, Credentials{})
	if err == nil {
		t.Fatal("Fetch() ran an ext:: transport")
	}
}

// fakeRegistry serves one template: a manifest with one layer and the layer.
func fakeRegistry(t *testing.T, wantUser string) *httptest.Server {
	t.Helper()

	var layer bytes.Buffer
	gz := gzip.NewWriter(&layer)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		name, body string
		mode       int64
	}{{"README.md", "hello\n", 0o644}, {"gradlew", "#!/bin/sh\n", 0o755}} {
		_ = tw.WriteHeader(&tar.Header{Name: f.name, Mode: f.mode, Size: int64(len(f.body)), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte(f.body))
	}
	_ = tw.Close()
	_ = gz.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, _, _ := r.BasicAuth(); user != wantUser {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case strings.Contains(r.URL.Path, "/manifests/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"layers": []map[string]string{{"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip", "digest": "sha256:abc"}},
			})
		case strings.Contains(r.URL.Path, "/blobs/"):
			_, _ = w.Write(layer.Bytes())
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchOCI(t *testing.T) {
	srv := fakeRegistry(t, "ada")

	files, err := Fetch(context.Background(),
		Source{Kind: KindOCI, URL: srv.URL, Path: "group/templates/lib", Ref: "v1"},
		Credentials{Username: "ada", Password: "secret"})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	got := byPath(files)
	if string(got["README.md"].Content) != "hello\n" || got["README.md"].Executable {
		t.Errorf("README.md = %+v", got["README.md"])
	}
	if !got["gradlew"].Executable {
		t.Errorf("gradlew lost its execute bit: %+v", got["gradlew"])
	}
}

func TestFetchOCIReportsARefusedLogin(t *testing.T) {
	srv := fakeRegistry(t, "ada")

	_, err := Fetch(context.Background(),
		Source{Kind: KindOCI, URL: srv.URL, Path: "group/templates/lib", Ref: "v1"}, Credentials{})
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("Fetch() error = %v, want the registry's 401", err)
	}
}

func TestFetchStopsOnACancelledContext(t *testing.T) {
	srv := fakeRegistry(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Fetch(ctx, Source{Kind: KindOCI, URL: srv.URL, Path: "a/b", Ref: "v1"}, Credentials{})
	if err == nil {
		t.Fatal("Fetch() ignored a cancelled context")
	}
}

func TestALimitIsRefusedNamingWhatIsTooBig(t *testing.T) {
	many := make([]File, MaxFiles+1)
	if err := checkLimits(many); err == nil || !strings.Contains(err.Error(), "501 files") {
		t.Errorf("checkLimits(%d files) = %v, want the count named", len(many), err)
	}
	if err := checkLimits(many[:MaxFiles]); err != nil {
		t.Errorf("checkLimits(exactly %d files) = %v, want it accepted", MaxFiles, err)
	}

	big := []File{
		{Path: "small.txt", Content: make([]byte, 10)},
		{Path: "dist/bundle.js", Content: make([]byte, MaxBytes)},
	}
	err := checkLimits(big)
	if err == nil || !strings.Contains(err.Error(), "dist/bundle.js") {
		t.Errorf("checkLimits(over the size) = %v, want the largest file named", err)
	}
	if err := checkLimits([]File{{Path: "ok", Content: make([]byte, MaxBytes)}}); err != nil {
		t.Errorf("checkLimits(exactly %d bytes) = %v, want it accepted", MaxBytes, err)
	}
}

// A link is refused, and the message says what to do rather than only what was
// found.
func TestReadArchiveExplainsALink(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	_ = tw.WriteHeader(&tar.Header{Name: "docs", Linkname: "../x", Typeflag: tar.TypeSymlink})
	_ = tw.Close()

	_, err := readArchive(buf.Bytes())
	if err == nil || !strings.Contains(err.Error(), "docs") || !strings.Contains(err.Error(), "replace it with a regular file") {
		t.Fatalf("readArchive() error = %v, want the entry and what to do", err)
	}
}

// The limits hold while the archive is being read, not after it is in memory.
func TestReadArchiveStopsPastTheLimits(t *testing.T) {
	big := archiveOf(t, 1, MaxBytes+1)
	if _, err := readArchive(big); !errors.Is(err, errTooLarge) {
		t.Errorf("readArchive(too many bytes) error = %v, want errTooLarge", err)
	}
	many := archiveOf(t, MaxFiles+1, 1)
	if _, err := readArchive(many); !errors.Is(err, errTooLarge) {
		t.Errorf("readArchive(too many files) error = %v, want errTooLarge", err)
	}
	ok := archiveOf(t, MaxFiles, 1)
	if _, err := readArchive(ok); err != nil {
		t.Errorf("readArchive(at the limits) error = %v", err)
	}
}

func archiveOf(t *testing.T, files, size int) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	body := bytes.Repeat([]byte("x"), size)
	for i := 0; i < files; i++ {
		if err := tw.WriteHeader(&tar.Header{Name: fmt.Sprintf("f%d", i), Mode: 0o644, Size: int64(size), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	_ = tw.Close()
	return buf.Bytes()
}
