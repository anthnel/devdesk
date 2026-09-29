package configuration

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/filebrowser"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// A path field says so on its label, the way a closed list does (§3.95).
func TestAPathFieldCarriesTheBrowseMarker(t *testing.T) {
	m := newModel(t)
	count := 0
	for _, s := range m.sections {
		for _, f := range s.Fields {
			if !f.path {
				continue
			}
			count++
			if !strings.Contains(ansi.Strip(m.renderField(f, false, 200)), f.Label+" "+theme.IconBrowse) {
				t.Errorf("tab %q: %q renders no browse marker", s.Title, f.Label)
			}
		}
	}
	if count < 4 {
		t.Fatalf("found %d path fields, want at least the workspaces root, the log file and a scanner's binary and config", count)
	}
}

func TestEnterOnAPathFieldAsksForAPickerWhereItPoints(t *testing.T) {
	m := focusOn(t, newModel(t), "Workspaces dir")
	m.input.SetValue("/srv/work")

	if !testutil.ShortcutEnabled(m.GetShortcuts(), "enter") {
		t.Error("enter must be offered on a path field")
	}
	_, cmd := m.Update(testutil.Key("enter"))
	req, ok := testutil.MsgOf[filebrowser.PickRequestMsg](cmd)
	if !ok {
		t.Fatal("enter on a path field must ask for a picker")
	}
	if req.Kind != filebrowser.PickDir || req.Start != "/srv/work" || req.Tag == "" {
		t.Errorf("unexpected request %+v", req)
	}
}

func TestEnterIsGreyedOnAFieldThatIsNotAPath(t *testing.T) {
	m := focusOn(t, newModel(t), "IDE command")
	if !testutil.ShortcutDisabled(m.GetShortcuts(), "enter") {
		t.Error("enter does nothing on a plain text field, so it is greyed there")
	}
}

func TestAPickedPathIsWrittenToItsFieldShownAndSaved(t *testing.T) {
	m := focusOn(t, newModel(t), "Log file")
	_, cmd := m.Update(testutil.Key("enter"))
	req, _ := testutil.MsgOf[filebrowser.PickRequestMsg](cmd)

	picked := filepath.Join(t.TempDir(), "devdesk.log")
	next, cmd := m.Update(filebrowser.PathPickedMsg{Tag: req.Tag, Path: picked})
	m = next.(Model)

	if m.config.App.LogFile != picked {
		t.Errorf("log file = %q, want %q", m.config.App.LogFile, picked)
	}
	if m.input.Value() != picked {
		t.Errorf("the focused input shows %q, want the picked path", m.input.Value())
	}
	if _, ok := testutil.MsgOf[ConfigSavedMsg](cmd); !ok {
		t.Error("a picked path must be saved like a typed one")
	}
}

// Two scanners both have a Binary: the answer must reach the one that asked.
func TestAPickedBinaryReachesTheToolThatAsked(t *testing.T) {
	m := newModel(t)
	var target field
	for _, s := range m.sections {
		for _, f := range s.Fields {
			if f.path && f.Label == "Binary" && f.Tool == "gitleaks" {
				target = f
			}
		}
	}
	if target.Tool == "" {
		t.Fatal("no gitleaks Binary field")
	}
	next, _ := m.Update(filebrowser.PathPickedMsg{Tag: pickTag(target), Path: "/opt/gitleaks"})
	m = next.(Model)
	if got := *target.str(m.config); got != "/opt/gitleaks" {
		t.Errorf("gitleaks binary = %q", got)
	}
	if got := m.config.Scan.Tools.Tool("trivy").Binary; got == "/opt/gitleaks" {
		t.Error("the answer leaked into another tool's Binary")
	}
}

func TestALongPathIsShortenedAtRest(t *testing.T) {
	m := newModel(t)
	long := "/srv/" + strings.Repeat("deeply/", 20) + "workspaces"
	m.config.App.WorkspacesDir = long
	f := focusOn(t, m, "Workspaces dir").current()

	rest := ansi.Strip(m.renderField(f, false, 60))
	if strings.Contains(rest, long) || !strings.HasSuffix(strings.TrimRight(rest, " "), "workspaces") {
		t.Errorf("at rest the path must be shortened and keep its name: %q", rest)
	}
	if w := ansi.StringWidth(rest); w > 60 {
		t.Errorf("the shortened row is %d wide, over 60", w)
	}
}
