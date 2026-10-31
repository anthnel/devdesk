package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/ui/components"
)

// DEVDESK-88 step 6, round 1: enter on a highlighted reserved word ("new",
// "-") and path-like names. Executing a creation is step 7, so "nothing was
// created" is checked on disk rather than through a Cmd.

// downTo presses down until the highlighted row contains sub (at most 10 times).
func downTo(t *testing.T, a *App, sub string) {
	t.Helper()
	for i := 0; i < 10; i++ {
		if h := highlighted(listRows(t, a)); len(h) == 1 && strings.Contains(h[0], sub) {
			return
		}
		press(t, a, "down")
	}
	t.Fatalf("could not highlight %q; rows %q", sub, listRows(t, a))
}

// requireOpenOnNew checks enter behaved like tab on "new": line open, text
// "ctx new ", no footer post, no switch.
func requireOpenOnNew(t *testing.T, a *App) {
	t.Helper()
	post, posted := footerOf(t, a, "enter")
	if posted {
		t.Errorf("a footer message was posted: level %v %q", post.Level, post.Text)
	}
	if !a.commandMode {
		t.Error("the command line closed")
	}
	if got := a.commandInput.Value(); got != "ctx new " {
		t.Errorf("line = %q, want %q", got, "ctx new ")
	}
	if a.switchingTo != "" {
		t.Errorf("switched to %q", a.switchingTo)
	}
}

func TestReservedNewHighlightedByDefaultEnterActsLikeTab(t *testing.T) {
	a := argRouter(t) // default, web, work: none starts with n
	ctxOpen(t, a, "ctx n")
	if h := highlighted(listRows(t, a)); len(h) != 1 || !strings.Contains(h[0], "new") {
		t.Fatalf("highlighted %q, want only new", h)
	}
	requireOpenOnNew(t, a)
}

func TestReservedNewChosenBesideAMatchingContext(t *testing.T) {
	a := argRouter(t, "default", "nas", "web")
	ctxOpen(t, a, "ctx n")
	downTo(t, a, "new")
	requireOpenOnNew(t, a)
}

func TestReservedNewChosenFromTheFullList(t *testing.T) {
	a := argRouter(t)
	ctxOpen(t, a, "ctx ")
	downTo(t, a, "new")
	requireOpenOnNew(t, a)
}

func TestReservedDashChosenWithNoPreviousContext(t *testing.T) {
	for _, line := range []string{"ctx ", "ctx -"} {
		t.Run(line, func(t *testing.T) {
			a := argRouter(t)
			ctxOpen(t, a, line)
			downTo(t, a, "-")

			post, ok := footerOf(t, a, "enter")

			if !ok {
				t.Fatal("no footer message posted")
			}
			if post.Level != components.LevelWarning || post.Text != "No previous context in this session" {
				t.Errorf("footer = level %v %q", post.Level, post.Text)
			}
			if a.commandMode {
				t.Error("the command line stayed open")
			}
			if a.switchingTo != "" {
				t.Errorf("switched to %q", a.switchingTo)
			}
		})
	}
}

func TestReservedDashChosenSwitchesToThePreviousContext(t *testing.T) {
	for _, line := range []string{"ctx ", "ctx -"} {
		t.Run(line, func(t *testing.T) {
			a := argRouter(t)
			a.previousContext = "web"
			ctxOpen(t, a, line)
			downTo(t, a, "-")

			press(t, a, "enter")

			requireSwitchTo(t, a, "web")
		})
	}
}

// ── path-like names ──────────────────────────────────────────────────────────

func snapshotTree(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	_ = filepath.Walk(root, func(p string, _ os.FileInfo, err error) error {
		if err == nil {
			out = append(out, p)
		}
		return nil
	})
	return out
}

func TestEscapePathNamesAreRefusedAndCreateNothing(t *testing.T) {
	cases := []struct{ line, name string }{
		{"ctx ../escape", "../escape"},
		{"ctx new ../escape", "../escape"},
		{"ctx new a/b", "a/b"},
		{"ctx new ..", ".."},
		{"ctx ..", ".."},
	}
	for _, c := range cases {
		t.Run(c.line, func(t *testing.T) {
			a := argRouter(t)
			home, _ := os.UserHomeDir()
			parent := filepath.Dir(home)
			beforeHome := snapshotTree(t, home)
			beforeParent, _ := os.ReadDir(parent)
			ctxOpen(t, a, c.line)

			post, ok := footerOf(t, a, "enter")

			want := `Invalid context name "` + c.name + `" — use lowercase letters, digits and hyphens`
			if !ok {
				t.Fatal("no footer message posted")
			}
			if post.Level != components.LevelError || post.Text != want {
				t.Errorf("footer = level %v %q, want error %q", post.Level, post.Text, want)
			}
			if a.commandMode || a.switchingTo != "" {
				t.Errorf("commandMode=%v switchingTo=%q", a.commandMode, a.switchingTo)
			}
			if after := snapshotTree(t, home); strings.Join(after, "\n") != strings.Join(beforeHome, "\n") {
				t.Errorf("HOME tree changed:\nbefore %q\nafter  %q", beforeHome, after)
			}
			if afterParent, _ := os.ReadDir(parent); len(afterParent) != len(beforeParent) {
				t.Errorf("an entry appeared next to HOME: %d -> %d", len(beforeParent), len(afterParent))
			}
		})
	}
}
