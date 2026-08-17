package viewer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectionPrefersTheExtensionThenTheContent(t *testing.T) {
	cases := []struct {
		name    string
		file    string
		content string
		want    Kind
	}{
		{"json extension", "config.json", `{"a":1}`, KindJSON},
		{"xml extension", "pod.xml", `<a/>`, KindXML},
		{"log extension", "app.log", `whatever`, KindLog},
		{"yaml extension", "compose.yaml", "app:\n  name: dk\n", KindYAML},
		{"yml extension", "compose.yml", "app:\n  name: dk\n", KindYAML},
		{"toml extension", "Cargo.toml", "[package]\nname = \"dk\"\n", KindTOML},

		// A named extension is taken at its word. Sniffing a .md would open a
		// Markdown file as a tree the first time someone started one with a tag.
		{"markdown starting with a tag", "README.md", `<div>hi</div>`, KindPlain},
		{"go source", "main.go", "package main", KindPlain},
		{"text holding json", "notes.txt", `{"a":1}`, KindPlain},

		// A Markdown front matter block opens on `---`, which is also how a YAML
		// stream separates documents. The extension decides, so the question never
		// comes up.
		{"markdown with front matter", "post.md", "---\ntitle: hi\n---\n", KindPlain},

		// Only a file with no extension at all is guessed at.
		{"extensionless object", "dump", `  {"a":1}`, KindJSON},
		{"extensionless array", "dump", `[1,2]`, KindJSON},
		{"extensionless document", "dump", `<?xml version="1.0"?><a/>`, KindXML},
		{"Dockerfile", "Dockerfile", "FROM alpine", KindPlain},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DetectKind(tc.file, []byte(tc.content)); got != tc.want {
				t.Errorf("DetectKind(%q) = %q, want %q", tc.file, got, tc.want)
			}
		})
	}
}

// "This looks like a log" is not a decidable question, so it is never asked.
// The kind comes from the extension or from the producer, and from nowhere else.
func TestALogKindIsNeverInferredFromContent(t *testing.T) {
	logLike := "2026-01-01 [ERROR] boom\n2026-01-01 [INFO] fine\n"

	if got := DetectKind("output", []byte(logLike)); got == KindLog {
		t.Error("an extensionless file was sniffed as a log; the kind must be declared")
	}
	if got := DetectKind("notes.txt", []byte(logLike)); got == KindLog {
		t.Error("a .txt was sniffed as a log")
	}
	if got := DetectKind("app.log", []byte(logLike)); got != KindLog {
		t.Errorf("DetectKind(app.log) = %q, want log from the extension", got)
	}
}

// Same rule, same reason: `---` at the top of a file does not make it YAML, and
// a `[section]` line is prose in half the files that hold one.
func TestYAMLAndTOMLAreNeverInferredFromContent(t *testing.T) {
	cases := []struct {
		name    string
		file    string
		content string
	}{
		{"a yaml document separator", "dump", "---\napp:\n  name: dk\n"},
		{"a yaml mapping", "dump", "app:\n  name: dk\n"},
		{"a toml table", "dump", "[package]\nname = \"dk\"\n"},
		{"a toml assignment", "notes.txt", "name = \"dk\"\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectKind(tc.file, []byte(tc.content))
			if got == KindYAML || got == KindTOML {
				t.Errorf("DetectKind(%q) = %q; the kind must be declared, never sniffed", tc.file, got)
			}
		})
	}
}

// Neither has a tree, and the absence is a decision (see Kind.Structured): the
// view offers `f` off the back of this answer, so a wrong one would advertise a
// display that renders an empty pane.
func TestNeitherYAMLNorTOMLClaimsATree(t *testing.T) {
	for _, kind := range []Kind{KindYAML, KindTOML} {
		if kind.Structured() {
			t.Errorf("%q reports a tree it has no parser for", kind)
		}
	}
}

func TestABOMDoesNotHideTheFirstCharacter(t *testing.T) {
	withBOM := append([]byte{0xEF, 0xBB, 0xBF}, []byte(`{"a":1}`)...)
	if got := DetectKind("dump", withBOM); got != KindJSON {
		t.Errorf("DetectKind = %q, want json — the mark hid the brace", got)
	}
}

func TestABinaryFileIsRefusedRatherThanRendered(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "image.png")
	png := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D}
	if err := os.WriteFile(path, png, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := ReadFile(path); err != ErrBinary {
		t.Errorf("ReadFile = %v, want ErrBinary", err)
	}
}

func TestAFileOverTheSizeCapIsRefused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.log")
	if err := os.WriteFile(path, []byte(strings.Repeat("a", MaxSize+1)), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := ReadFile(path); err != ErrTooLarge {
		t.Errorf("ReadFile = %v, want ErrTooLarge", err)
	}
}

func TestAReadableTextFileComesBackWhole(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	want := "hello\nworld\n"
	if err := os.WriteFile(path, []byte(want), 0o600); err != nil {
		t.Fatal(err)
	}

	data, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != want {
		t.Errorf("ReadFile = %q, want %q", data, want)
	}
}

func TestADirectoryIsNotADocument(t *testing.T) {
	if _, err := ReadFile(t.TempDir()); err == nil {
		t.Error("ReadFile accepted a directory")
	}
}

func TestAFileSourceNamesItselfByItsBase(t *testing.T) {
	src := NewFileSource(filepath.Join("a", "b", "config.json"))
	if got := src.Name(); got != "config.json" {
		t.Errorf("Name = %q, want config.json", got)
	}
	if got := src.Kind(); got != KindAuto {
		t.Errorf("Kind = %q, want auto — a file browser does not know what it is opening", got)
	}
}
