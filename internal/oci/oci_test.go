package oci

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// tarEntry describes one member of a fixture archive.
type tarEntry struct {
	name     string
	body     string
	typeflag byte
}

// buildTarGz assembles a gzipped tar in memory, so the extraction tests need no
// fixture files on disk.
func buildTarGz(t *testing.T, entries []tarEntry) *bytes.Reader {
	t.Helper()

	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	for _, e := range entries {
		flag := e.typeflag
		if flag == 0 {
			flag = tar.TypeReg
		}
		hdr := &tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.body)), Typeflag: flag}
		if flag == tar.TypeDir {
			hdr.Size = 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("writing header for %q: %v", e.name, err)
		}
		if flag != tar.TypeDir {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatalf("writing body for %q: %v", e.name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("closing tar: %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("closing gzip: %v", err)
	}
	return bytes.NewReader(buf.Bytes())
}

func TestExtractTarGzReadsRegularFiles(t *testing.T) {
	archive := buildTarGz(t, []tarEntry{
		{name: "Dockerfile", body: "FROM scratch\n"},
		{name: "src/main.go", body: "package main\n"},
	})

	files, err := extractTarGz(archive)
	if err != nil {
		t.Fatalf("extractTarGz() returned an error: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("extracted %d files, want 2", len(files))
	}
	if got := files["Dockerfile"]; got != "FROM scratch\n" {
		t.Errorf("Dockerfile = %q, want %q", got, "FROM scratch\n")
	}
	if got := files["src/main.go"]; got != "package main\n" {
		t.Errorf("src/main.go = %q, want %q", got, "package main\n")
	}
}

func TestExtractTarGzSkipsDirectories(t *testing.T) {
	archive := buildTarGz(t, []tarEntry{
		{name: "templates/", typeflag: tar.TypeDir},
		{name: "templates/app.yaml", body: "kind: Deployment\n"},
	})

	files, err := extractTarGz(archive)
	if err != nil {
		t.Fatalf("extractTarGz() returned an error: %v", err)
	}
	if _, ok := files["templates/"]; ok {
		t.Error("directory entry was returned as a file")
	}
	if len(files) != 1 {
		t.Errorf("extracted %d entries, want only the single regular file", len(files))
	}
}

// Archive members are commonly prefixed with "./" by tar; the leading component
// is stripped so callers see stable, relative keys.
func TestExtractTarGzNormalisesLeadingPathComponents(t *testing.T) {
	archive := buildTarGz(t, []tarEntry{
		{name: "./relative.txt", body: "a"},
		{name: "/absolute.txt", body: "b"},
		{name: "plain.txt", body: "c"},
	})

	files, err := extractTarGz(archive)
	if err != nil {
		t.Fatalf("extractTarGz() returned an error: %v", err)
	}
	for _, want := range []string{"relative.txt", "absolute.txt", "plain.txt"} {
		if _, ok := files[want]; !ok {
			t.Errorf("missing normalised key %q; got keys %v", want, keysOf(files))
		}
	}
}

func TestExtractTarGzEmptyArchive(t *testing.T) {
	files, err := extractTarGz(buildTarGz(t, nil))
	if err != nil {
		t.Fatalf("extractTarGz() returned an error: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("extracted %d files from an empty archive, want 0", len(files))
	}
}

func TestExtractTarGzEmptyFileIsKept(t *testing.T) {
	files, err := extractTarGz(buildTarGz(t, []tarEntry{{name: "empty", body: ""}}))
	if err != nil {
		t.Fatalf("extractTarGz() returned an error: %v", err)
	}
	if got, ok := files["empty"]; !ok || got != "" {
		t.Errorf("empty file missing or altered: value=%q present=%v", got, ok)
	}
}

func TestExtractTarGzRejectsNonGzipInput(t *testing.T) {
	if _, err := extractTarGz(strings.NewReader("this is not gzip")); err == nil {
		t.Error("extractTarGz() accepted non-gzip input")
	}
}

func TestExtractTarGzRejectsTruncatedArchive(t *testing.T) {
	full := buildTarGz(t, []tarEntry{{name: "a.txt", body: strings.Repeat("x", 4096)}})
	raw := make([]byte, full.Len())
	if _, err := full.Read(raw); err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	if _, err := extractTarGz(bytes.NewReader(raw[:len(raw)/2])); err == nil {
		t.Error("extractTarGz() accepted a truncated archive")
	}
}

// A key containing parent references would hand a Zip Slip to any caller that
// writes the extracted map under a target directory, so escaping entries are
// rejected outright rather than silently rewritten.
func TestExtractTarGzRejectsParentTraversal(t *testing.T) {
	tests := []struct {
		name  string
		entry string
	}{
		{"leading parent references", "../../etc/passwd"},
		{"parent reference after a segment", "templates/../../etc/passwd"},
		{"absolute path with parent references", "/../../etc/passwd"},
		{"backslash separators", `..\..\etc\passwd`},
		{"bare parent reference", ".."},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			archive := buildTarGz(t, []tarEntry{{name: tc.entry, body: "root:x:0:0\n"}})

			files, err := extractTarGz(archive)
			if err == nil {
				t.Fatalf("extractTarGz() accepted %q, returning keys %v", tc.entry, keysOf(files))
			}
			if !strings.Contains(err.Error(), fmt.Sprintf("%q", tc.entry)) {
				t.Errorf("error %q does not name the offending entry %q", err, tc.entry)
			}
		})
	}
}

// Traversal that resolves back inside the root is legitimate and must survive,
// normalised.
func TestExtractTarGzNormalisesContainedTraversal(t *testing.T) {
	archive := buildTarGz(t, []tarEntry{{name: "templates/../README.md", body: "hello"}})

	files, err := extractTarGz(archive)
	if err != nil {
		t.Fatalf("extractTarGz() returned an error: %v", err)
	}
	if got, ok := files["README.md"]; !ok || got != "hello" {
		t.Errorf("want files[\"README.md\"] == \"hello\"; got %q (present=%v), keys %v", got, ok, keysOf(files))
	}
}

func TestNewClientTrimsTrailingSlash(t *testing.T) {
	tests := []struct {
		given string
		want  string
	}{
		{"https://registry.example.com/", "https://registry.example.com"},
		{"https://registry.example.com", "https://registry.example.com"},
		{"https://registry.example.com:5000/", "https://registry.example.com:5000"},
	}

	for _, tt := range tests {
		t.Run(tt.given, func(t *testing.T) {
			if got := NewClient(tt.given, "", "").registryURL; got != tt.want {
				t.Errorf("registryURL = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestListTagsHappyPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"group/tpl","tags":["v1","v2"]}`))
	}))
	defer srv.Close()

	tags, err := NewClient(srv.URL, "", "").ListTags("group/tpl")
	if err != nil {
		t.Fatalf("ListTags() returned an error: %v", err)
	}
	if gotPath != "/v2/group/tpl/tags/list" {
		t.Errorf("requested %q, want %q", gotPath, "/v2/group/tpl/tags/list")
	}
	if len(tags) != 2 || tags[0] != "v1" || tags[1] != "v2" {
		t.Errorf("tags = %v, want [v1 v2]", tags)
	}
}

func TestListTagsSendsBasicAuthOnlyWhenCredentialsAreSet(t *testing.T) {
	tests := []struct {
		name     string
		user     string
		pass     string
		wantAuth bool
	}{
		{"both set", "u", "p", true},
		{"no credentials", "", "", false},
		{"username only", "u", "", false},
		{"password only", "", "p", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sawAuth bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _, sawAuth = r.BasicAuth()
				_, _ = w.Write([]byte(`{"tags":[]}`))
			}))
			defer srv.Close()

			if _, err := NewClient(srv.URL, tt.user, tt.pass).ListTags("repo"); err != nil {
				t.Fatalf("ListTags() returned an error: %v", err)
			}
			if sawAuth != tt.wantAuth {
				t.Errorf("basic auth sent = %v, want %v", sawAuth, tt.wantAuth)
			}
		})
	}
}

func TestListTagsSurfacesHTTPErrors(t *testing.T) {
	for _, code := range []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusInternalServerError} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(code)
			}))
			defer srv.Close()

			_, err := NewClient(srv.URL, "", "").ListTags("repo")
			if err == nil {
				t.Fatalf("ListTags() accepted status %d", code)
			}
			// The message carries the URL so a failing registry call is diagnosable
			// from the footer message alone.
			if !strings.Contains(err.Error(), "/v2/repo/tags/list") {
				t.Errorf("error %q does not name the requested URL", err)
			}
		})
	}
}

func TestListTagsRejectsMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tags": [`))
	}))
	defer srv.Close()

	if _, err := NewClient(srv.URL, "", "").ListTags("repo"); err == nil {
		t.Error("ListTags() accepted a truncated JSON body")
	}
}

func TestListTagsEmptyTagList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":"repo"}`))
	}))
	defer srv.Close()

	tags, err := NewClient(srv.URL, "", "").ListTags("repo")
	if err != nil {
		t.Fatalf("ListTags() returned an error: %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("tags = %v, want empty for a response with no tags field", tags)
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
