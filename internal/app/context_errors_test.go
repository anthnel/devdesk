package app

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/ui/components"
)

// DEVDESK-88 step 7: a failure to list contexts, or an unclassified disk error
// on a creation, must name the right operation in the footer and in the log.

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &buf
}

func TestListContextsFailureReportsListOperation(t *testing.T) {
	a := routerAt(t, &fakeView{}, 180, 40)
	// os.UserHomeDir fails when HOME is unset, so config.ListContexts errors.
	t.Setenv("HOME", "")
	buf := captureLog(t)

	msg := a.listContexts()()
	em, ok := msg.(ContextSwitchErrorMsg)
	if !ok {
		t.Fatalf("listContexts() = %T, want ContextSwitchErrorMsg", msg)
	}
	if em.Op != opList {
		t.Errorf("Op = %q, want %q", em.Op, opList)
	}

	posts := drive(t, a, func() tea.Msg { return msg })
	wantPost(t, posts, components.LevelError, "Failed to list contexts — check logs")
	if !strings.Contains(buf.String(), "ERROR [app/context] list") {
		t.Errorf("log = %q, want it to contain %q", buf.String(), "ERROR [app/context] list")
	}
	if strings.Contains(buf.String(), `switch ""`) {
		t.Errorf("log still names a nameless switch: %q", buf.String())
	}
}

func TestCreateDiskErrorSaysCreateNotSwitch(t *testing.T) {
	a := routerAt(t, &fakeView{}, 180, 40)
	// A file where the ~/.devdesk directory should be: creating fails with an
	// error that is neither "exists" nor "reserved" nor "not found".
	home, _ := os.UserHomeDir()
	if err := os.WriteFile(filepath.Join(home, ".devdesk"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	posts := drive(t, a, a.createContext("x"))
	wantPost(t, posts, components.LevelError, `Failed to create context "x" — check logs`)
}

func TestErrorFooterPerOperation(t *testing.T) {
	a := routerAt(t, &fakeView{}, 180, 40)
	_, cmd := a.Update(ContextSwitchErrorMsg{Name: "x", Op: opCreate, Error: os.ErrPermission})
	wantPost(t, drive(t, a, cmd), components.LevelError, `Failed to create context "x" — check logs`)
	_, cmd = a.Update(ContextSwitchErrorMsg{Name: "x", Op: opSwitch, Error: os.ErrPermission})
	wantPost(t, drive(t, a, cmd), components.LevelError, `Failed to switch to "x" — check logs`)
}
