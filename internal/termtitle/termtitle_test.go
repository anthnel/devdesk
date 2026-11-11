package termtitle

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	wantPush = "\x1b[22;2t"
	wantPop  = "\x1b[23;2t"
	// What Bubble Tea writes when it clears the title on exit.
	clearTitle = "\x1b]2;\x07"
)

func TestPreserveWritesPushThenRunThenPop(t *testing.T) {
	var buf bytes.Buffer
	ran := false
	err := Preserve(&buf, func() error {
		ran = true
		// The push must already be out before run starts.
		if got := buf.String(); got != wantPush {
			t.Errorf("before run: wrote %q, want %q", got, wantPush)
		}
		buf.WriteString(clearTitle) // Bubble Tea clears the title inside run
		return nil
	})
	if err != nil {
		t.Fatalf("Preserve returned %v, want nil", err)
	}
	if !ran {
		t.Fatal("run was not called")
	}
	// Order: push, the title cleared by Bubble Tea, then pop last.
	if got, want := buf.String(), wantPush+clearTitle+wantPop; got != want {
		t.Errorf("wrote %q, want %q", got, want)
	}
}

func TestPreserveWritesOnlyConstantSequences(t *testing.T) {
	var buf bytes.Buffer
	_ = Preserve(&buf, func() error { return nil })
	if got, want := buf.String(), wantPush+wantPop; got != want {
		t.Errorf("wrote %q, want exactly %q (no variable data)", got, want)
	}
}

func TestPreservePropagatesRunError(t *testing.T) {
	var buf bytes.Buffer
	boom := errors.New("boom")
	err := Preserve(&buf, func() error { return boom })
	if !errors.Is(err, boom) {
		t.Fatalf("Preserve returned %v, want %v", err, boom)
	}
	if got, want := buf.String(), wantPush+wantPop; got != want {
		t.Errorf("wrote %q, want %q (pop must run after an error)", got, want)
	}
}

func TestPreserveRestoresThenRepanics(t *testing.T) {
	var buf bytes.Buffer
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_ = Preserve(&buf, func() error {
			buf.WriteString(clearTitle)
			panic("kaboom")
		})
	}()
	if recovered != "kaboom" {
		t.Fatalf("recovered %v, want the original panic value %q", recovered, "kaboom")
	}
	if got, want := buf.String(), wantPush+clearTitle+wantPop; got != want {
		t.Errorf("wrote %q, want %q (pop must run before the panic propagates)", got, want)
	}
}

// Only the termtitle package owns the XTWINOPS title-stack sequences, and only
// main.go calls Preserve.
func TestSequencesAndCallerAreConfined(t *testing.T) {
	root := filepath.Join("..", "..")
	needles := []string{"\\x1b[" + "22;", "\\x1b[" + "23;", "\\033[" + "22;", "\\033[" + "23;", "\\u001b[" + "22;", "\\u001b[" + "23;"}
	call := "termtitle." + "Preserve("

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".worktrees", "node_modules", "vendor":
				return fs.SkipDir
			}
			if filepath.Clean(path) == filepath.Join(root, "internal", "termtitle") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(b)
		for i, line := range strings.Split(src, "\n") {
			for _, n := range needles {
				if strings.Contains(line, n) {
					t.Errorf("%s:%d: title-stack sequence %q outside internal/termtitle", path, i+1, n)
				}
			}
			if strings.Contains(line, call) && filepath.Clean(path) != filepath.Join(root, "main.go") {
				t.Errorf("%s:%d: only main.go may call termtitle.Preserve", path, i+1)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	main, err := os.ReadFile(filepath.Join(root, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(main), call) {
		t.Error("main.go does not call termtitle.Preserve")
	}
	if !strings.Contains(string(main), "IsTerminal(") {
		t.Error("main.go must only call Preserve when stdout is a terminal (term.IsTerminal)")
	}
}
