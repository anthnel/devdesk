package templates

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/template"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	uiviewer "github.com/anthnel/devdesk/internal/ui/viewer"
)

func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	// A footer message carries a three-second expiry, and reading a Cmd runs it.
	sharedcomponents.FooterMsgDuration = time.Millisecond

	code := m.Run()

	log.SetOutput(os.Stderr)
	os.Exit(code)
}

// ── Fixtures ─────────────────────────────────────────────────────────────────

func entry(slug, name string, tags ...string) template.Entry {
	return template.Entry{
		Slug: slug, Name: name, Tags: tags,
		Source: template.Source{Kind: template.KindGit, URL: "https://git.example.com/acme/" + slug + ".git", Ref: "main"},
	}
}

// catalogWith writes a catalog file holding the given entries and returns its path.
func catalogWith(t *testing.T, entries ...template.Entry) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "templates.yaml")
	store, err := template.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if err := store.Put(e); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// drive sends a message and follows every message the resulting commands
// produce, the way the runtime would, until nothing more comes back.
//
// Two things are deliberately not followed. The footer's expiry would clear the
// very message a test is about to read. And a typed character's cursor-blink
// Cmd blocks for the length of a blink, which across a form is a minute of
// nothing — typing produces none of this view's messages, so it is not run.
func drive(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	updated, cmd := m.Update(msg)
	next, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want templates.Model", updated)
	}
	if key, isKey := msg.(tea.KeyMsg); isKey && key.Type == tea.KeyRunes && m.confirmModal == nil {
		return next
	}
	for _, produced := range testutil.Msgs(cmd) {
		if _, expiry := produced.(sharedcomponents.ClearFooterMsg); expiry {
			continue
		}
		next = drive(t, next, produced)
	}
	return next
}

func send(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	for _, msg := range msgs {
		m = drive(t, m, msg)
	}
	return m
}

// opened returns a view on a catalog holding entries, laid out and loaded.
func opened(t *testing.T, entries ...template.Entry) Model {
	t.Helper()
	return openedAt(t, catalogWith(t, entries...))
}

func openedAt(t *testing.T, path string) Model {
	t.Helper()
	m := NewWithPath(config.Default(), nil, path)
	m = send(t, m, testutil.Resize(140, 24))
	return drive(t, m, testutil.Msgs(loadCatalogCmd(path))[0])
}

func slugs(m Model) []string {
	var out []string
	for _, r := range m.table.Visible() {
		out = append(out, r.Entry.Slug)
	}
	return out
}

// ── The list ─────────────────────────────────────────────────────────────────

func TestTheCatalogOpensAsRows(t *testing.T) {
	m := opened(t, entry("spring-api", "Spring API", "java"), entry("go-lib", "Go Library"))

	if got, want := slugs(m), []string{"go-lib", "spring-api"}; !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %v, want %v (sorted by name)", got, want)
	}
	if info := m.GetHeaderInfo(""); len(info) != 1 || info[0].Value != "2" {
		t.Errorf("header = %+v, want a count of 2", info)
	}
}

// Rule 139: the body is the table whatever it holds, so an empty catalog still
// has its header and its columns.
func TestAnEmptyCatalogIsStillATable(t *testing.T) {
	m := opened(t)
	if view := m.View(); !strings.Contains(view, "Name") || !strings.Contains(view, "Source") {
		t.Errorf("an empty catalog rendered %q, want the table's header", view)
	}
}

// Rule 116, stated over the rendered extent.
func TestTheTableFillsTheViewport(t *testing.T) {
	m := opened(t, entry("a", "A"))
	if got, want := m.table.RenderedWidth(), 140-2; got != want {
		t.Errorf("rendered width = %d, want %d", got, want)
	}
}

func TestTheFilterMatchesTagsAndDescription(t *testing.T) {
	spring := entry("spring-api", "Spring API", "java", "spring-boot")
	lib := entry("go-lib", "Go Library", "go")
	lib.Description = "shared helpers"
	m := opened(t, spring, lib)

	m = send(t, m, testutil.Key("/"))
	m = send(t, m, testutil.Type("spring-boot")...)
	if got := slugs(m); !reflect.DeepEqual(got, []string{"spring-api"}) {
		t.Errorf("filter on a tag = %v, want spring-api", got)
	}
}

// ── Creating ─────────────────────────────────────────────────────────────────

// fill types into the focused field and moves on with enter, which is how a
// form is filled by hand.
func fill(m Model, t *testing.T, texts ...string) Model {
	t.Helper()
	for _, text := range texts {
		if text != "" {
			m = send(t, m, testutil.Type(text)...)
		}
		m = send(t, m, testutil.Key("enter"))
	}
	return m
}

func TestCreatingATemplateSavesItAndSelectsIt(t *testing.T) {
	path := catalogWith(t, entry("aaa", "Aaa"))
	m := openedAt(t, path)

	m = send(t, m, testutil.Key("N"))
	if m.form == nil {
		t.Fatal("N did not open the form")
	}
	// name, description, tags, kind (left as git), URL, subdirectory, ref
	m = fill(m, t, "Spring API", "", "Java, Spring-Boot", "", "https://github.com/acme/spring.git", "", "main")
	m = send(t, m, testutil.Key("enter")) // the submit button

	if m.form != nil {
		t.Fatalf("the form is still open: %q", m.form.err)
	}
	saved, err := m.store.Get("spring-api")
	if err != nil {
		t.Fatalf("nothing was saved: %v", err)
	}
	if !reflect.DeepEqual(saved.Tags, []string{"java", "spring-boot"}) || saved.Source.Ref != "main" {
		t.Errorf("saved = %+v", saved)
	}
	if sel, _ := m.selectedEntry(); sel.Slug != "spring-api" {
		t.Errorf("selected = %q, want the new template", sel.Slug)
	}

	reopened, _ := template.Open(path)
	if _, err := reopened.Get("spring-api"); err != nil {
		t.Errorf("the catalog file does not hold it: %v", err)
	}
}

func TestAnInvalidURLKeepsTheFormOpenWithTheReason(t *testing.T) {
	path := catalogWith(t)
	m := openedAt(t, path)

	m = send(t, m, testutil.Key("N"))
	m = fill(m, t, "Evil", "", "", "", "ext::sh -c id", "", "")
	m = send(t, m, testutil.Key("enter"))

	if m.form == nil || !strings.Contains(m.form.err, "not allowed") {
		t.Fatalf("form = %+v, want it open with the reason", m.form)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("a refused entry created the catalog file")
	}
}

func TestATakenNameIsRefused(t *testing.T) {
	m := opened(t, entry("spring-api", "Spring API"))

	m = send(t, m, testutil.Key("N"))
	m = fill(m, t, "Spring  API!", "", "", "", "https://github.com/acme/x.git", "", "")
	m = send(t, m, testutil.Key("enter"))

	if m.form == nil || !strings.Contains(m.form.err, "already exists") {
		t.Errorf("form err = %v, want the collision named", m.form)
	}
}

func TestALocalTemplateHasNoURLField(t *testing.T) {
	m := opened(t)
	m = send(t, m, testutil.Key("N"))
	form := m.form

	hasURL := func() bool {
		for _, f := range form.fields() {
			if f == fieldURL {
				return true
			}
		}
		return false
	}
	if !hasURL() {
		t.Fatal("a git template has no URL field")
	}

	form.focus = 3 // the source field
	form.updateFocus()
	form.Update(testutil.Key("right"))

	if form.currentKind() != template.KindLocal || hasURL() {
		t.Errorf("kind = %s, url field present = %v; want a local template with none", form.currentKind(), hasURL())
	}
}

func TestEscLeavesTheFormWithoutSaving(t *testing.T) {
	path := catalogWith(t)
	m := openedAt(t, path)
	m = send(t, m, testutil.Key("N"))
	m = send(t, m, testutil.Type("Draft")...)

	m = send(t, m, testutil.Key("esc"))

	if m.form != nil {
		t.Error("esc left the form open")
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("esc saved something")
	}
}

// ── Editing ──────────────────────────────────────────────────────────────────

func TestEditingKeepsTheSlugWhateverTheNameBecomes(t *testing.T) {
	m := opened(t, entry("spring-api", "Spring API", "java"))

	m = send(t, m, testutil.Key("E"))
	if m.form == nil || m.form.name.Value() != "Spring API" {
		t.Fatalf("form = %+v, want it prefilled", m.form)
	}
	m.form.name.SetValue("Spring Boot API")
	m = send(t, m, testutil.Keys("enter", "enter", "enter", "enter", "enter", "enter", "enter", "enter")...)

	if got := slugs(m); !reflect.DeepEqual(got, []string{"spring-api"}) {
		t.Fatalf("rows = %v, want the same single entry", got)
	}
	if sel, _ := m.selectedEntry(); sel.Name != "Spring Boot API" {
		t.Errorf("name = %q, want it renamed", sel.Name)
	}
}

// ── Deleting ─────────────────────────────────────────────────────────────────

func TestDeletingAsksFirstAndDefaultsToNo(t *testing.T) {
	m := opened(t, entry("api", "API"))

	m = send(t, m, testutil.Key("D"))
	if m.confirmModal == nil {
		t.Fatal("D did not ask")
	}
	m = send(t, m, testutil.Key("enter")) // the focused button is No
	if len(slugs(m)) != 1 || m.confirmModal != nil {
		t.Errorf("rows = %v, modal open = %v; want the entry kept and the modal closed", slugs(m), m.confirmModal != nil)
	}
}

func TestConfirmingRemovesTheEntryFromTheFile(t *testing.T) {
	path := catalogWith(t, entry("api", "API"), entry("lib", "Lib"))
	m := openedAt(t, path)

	m = send(t, m, testutil.Key("D"), testutil.Key("y"))

	if got := slugs(m); !reflect.DeepEqual(got, []string{"lib"}) {
		t.Errorf("rows = %v, want lib", got)
	}
	reopened, _ := template.Open(path)
	if len(reopened.List()) != 1 {
		t.Errorf("the file still holds %d entries", len(reopened.List()))
	}
}

// ── Shortcuts (Rule 130) ─────────────────────────────────────────────────────

func TestTheShortcutKeysDoNotChangeWithTheSelection(t *testing.T) {
	empty := opened(t)
	full := opened(t, entry("api", "API"))

	base := testutil.ShortcutKeys(empty.GetShortcuts())
	if got := testutil.ShortcutKeys(full.GetShortcuts()); !reflect.DeepEqual(got, base) {
		t.Errorf("with a row: keys = %v, want %v", got, base)
	}
}

func TestNothingSelectedGreysTheKeysThatNeedARow(t *testing.T) {
	m := opened(t)
	sc := m.GetShortcuts()

	for _, key := range []string{"E", "D", "V"} {
		if !testutil.ShortcutDisabled(sc, key) {
			t.Errorf("%s is not greyed with nothing selected", key)
		}
	}
	if !testutil.ShortcutEnabled(sc, "N") {
		t.Error("N is greyed, but it does not need a row")
	}
	m = send(t, m, testutil.Key("E"))
	if m.footer.Text() != reasonNoTemplate {
		t.Errorf("footer = %q, want the reason", m.footer.Text())
	}
}

// An unreadable catalog refuses every write: saving over a file that could not
// be parsed would replace whatever the user had in it.
func TestAnUnreadableCatalogRefusesEveryWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "templates.yaml")
	if err := os.WriteFile(path, []byte("templates: [unclosed"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := openedAt(t, path)

	if m.storeErr == nil || m.footer.Level() != sharedcomponents.LevelError {
		t.Fatalf("storeErr = %v, footer level = %v; want the failure reported", m.storeErr, m.footer.Level())
	}
	for _, key := range []string{"N", "E", "D"} {
		if !testutil.ShortcutDisabled(m.GetShortcuts(), key) {
			t.Errorf("%s is not greyed on an unreadable catalog", key)
		}
	}
	m = send(t, m, testutil.Key("N"))
	if m.form != nil {
		t.Error("N opened a form over a catalog that could not be read")
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != "templates: [unclosed" {
		t.Errorf("the file was rewritten: %q", raw)
	}
}

func TestAFormTakesOverTheShortcutList(t *testing.T) {
	m := opened(t)
	m = send(t, m, testutil.Key("N"))

	sc := m.GetShortcuts()
	if testutil.HasShortcut(sc, "N") || !testutil.HasShortcut(sc, "esc") {
		t.Errorf("shortcuts = %v, want the form's own", testutil.ShortcutKeys(sc))
	}
	if !testutil.ShortcutDisabled(sc, "←→") {
		t.Error("←→ is not greyed off the source field")
	}
	if !m.InEditMode() {
		t.Error("a form does not report itself as editing, so `:` would leave it")
	}
}

// ── Preview ──────────────────────────────────────────────────────────────────

func TestPreviewAsksTheRouterToOpenAListing(t *testing.T) {
	m := opened(t, entry("api", "API"))

	_, cmd := m.Update(testutil.Key("V"))
	req, ok := testutil.MsgOf[uiviewer.OpenRequestMsg](cmd)
	if !ok {
		t.Fatal("V did not ask the router to open the viewer")
	}
	src, ok := req.Source.(previewSource)
	if !ok || src.entry.Slug != "api" {
		t.Errorf("source = %#v, want a preview of api", req.Source)
	}
}

func TestListingSummarisesTheFiles(t *testing.T) {
	got := listing(entry("api", "API"), []template.File{
		{Path: "src/main.go", Content: []byte("package main\n")},
		{Path: "mvnw", Content: []byte("#!/bin/sh\n"), Executable: true},
	})
	for _, want := range []string{"API — 2 files", "x  ", "mvnw", "src/main.go", "@ main"} {
		if !strings.Contains(got, want) {
			t.Errorf("listing lacks %q:\n%s", want, got)
		}
	}
	if strings.Index(got, "mvnw") > strings.Index(got, "src/main.go") {
		t.Error("files are not sorted by path")
	}
	if !strings.Contains(listing(entry("e", "E"), nil), "empty") {
		t.Error("a template with no files does not say a repository made from it would be empty")
	}
}

func TestSlugify(t *testing.T) {
	tests := map[string]string{
		"Spring Boot API":  "spring-boot-api",
		"  Spring  API!  ": "spring-api",
		"Java 21 / Maven":  "java-21-maven",
		"!!!":              "",
		"already-a-slug":   "already-a-slug",
	}
	for in, want := range tests {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}
