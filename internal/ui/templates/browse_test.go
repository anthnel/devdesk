package templates

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/template"
	"github.com/anthnel/devdesk/internal/ui/filebrowser"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// localForm opens N and cycles the source to local.
func localForm(t *testing.T) Model {
	t.Helper()
	m := send(t, opened(t), testutil.Key("N"))
	m.form.focus = slices.Index(m.form.fields(), fieldKind)
	m.form.updateFocus()
	m.form.Update(testutil.Key("right"))
	if m.form.currentKind() != template.KindLocal {
		t.Fatalf("kind = %s, want local", m.form.currentKind())
	}
	return m
}

// Only a local template's directory is a path on this machine (§3.95): its
// label carries the browse marker, and enter there opens the picker.
func TestOnlyALocalDirectoryIsBrowsable(t *testing.T) {
	m := send(t, opened(t), testutil.Key("N"))
	m.form.focus = slices.Index(m.form.fields(), fieldPath)
	m.form.updateFocus()
	if strings.Contains(ansi.Strip(m.form.View()), theme.IconBrowse) {
		t.Error("a git subdirectory must not carry the browse marker")
	}
	cmd := m.form.Update(testutil.Key("enter"))
	if _, ok := testutil.MsgOf[filebrowser.PickRequestMsg](cmd); ok {
		t.Error("enter on a git subdirectory must move on, not browse")
	}

	m = localForm(t)
	if !strings.Contains(ansi.Strip(m.form.View()), "Directory "+theme.IconBrowse) {
		t.Error("a local directory must carry the browse marker")
	}
}

func TestEnterOnTheLocalDirectoryAsksForADirectoryPicker(t *testing.T) {
	m := localForm(t)
	m.form.path.SetValue("/srv/templates")
	m.form.focus = slices.Index(m.form.fields(), fieldPath)
	m.form.updateFocus()

	cmd := m.form.Update(testutil.Key("enter"))
	req, ok := testutil.MsgOf[filebrowser.PickRequestMsg](cmd)
	if !ok {
		t.Fatal("enter on the directory must ask for a picker")
	}
	if req.Kind != filebrowser.PickDir || req.Start != "/srv/templates" || req.Tag != pathTag {
		t.Errorf("unexpected request %+v", req)
	}
	if got := m.form.EnterDescription(); got != "Browse" {
		t.Errorf("enter reads %q on the directory", got)
	}
}

func TestAPickedDirectoryFillsTheField(t *testing.T) {
	m := localForm(t)
	m = send(t, m, filebrowser.PathPickedMsg{Tag: pathTag, Path: "/srv/templates/go-api"})
	if got := m.form.path.Value(); got != "/srv/templates/go-api" {
		t.Errorf("directory = %q", got)
	}
}

func TestALocalDirectoryIsShortenedAtRest(t *testing.T) {
	m := localForm(t)
	long := "/srv/" + strings.Repeat("nested/", 15) + "go-api"
	m.form.path.SetValue(long)
	view := ansi.Strip(m.form.View())
	if strings.Contains(view, long) || !strings.Contains(view, "go-api") {
		t.Errorf("the directory must be shortened at rest and keep its name:\n%s", view)
	}
}
