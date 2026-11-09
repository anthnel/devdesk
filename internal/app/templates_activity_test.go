package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/template"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/templates"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-149 step 4: what only the router can show about the templates
// activity column. The failure lives in jobs.Registry, so it outlives the view.

// templatesRouter hosts the real templates view on a catalog holding one git
// template, "api", under the isolated HOME.
func templatesRouter(t *testing.T) *App {
	t.Helper()
	testutil.FastTimers(t, &animationFastInterval)
	a := routerAt(t, &bareView{}, 180, 40)
	path, err := template.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	store, err := template.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	err = store.Put(template.Entry{
		Slug: "api", Name: "API",
		Source: template.Source{Kind: template.KindGit, URL: "https://git.example.com/acme/api.git", Ref: "main"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// openTemplates switches to the view and feeds the router what its Init loaded.
func openTemplates(t *testing.T, a *App) {
	t.Helper()
	_ = a.switchView(command.ViewTemplates) // its batch carries tick timers
	for _, msg := range testutil.Msgs(a.views[command.ViewTemplates].Init()) {
		a.Update(msg)
	}
	a.resize(a.width, a.height)
}

func templatesRow(t *testing.T, a *App) string {
	t.Helper()
	return rowOf(t, ansi.Strip(a.views[command.ViewTemplates].View()), "API")
}

func failSyncOnRow(a *App) {
	a.Update(jobs.StartMsg{Run: jobs.NewRun(jobs.KindSync, command.ViewTemplates, "", "API", "api").WithDisplay("api", "API")})
	a.Update(templates.SyncCompleteMsg{Slug: "api", Name: "API", Err: errors.New("boom")})
}

func wantTemplatesFailed(t *testing.T, a *App, when string) {
	t.Helper()
	if row := templatesRow(t, a); !strings.Contains(row, failedGlyph()) {
		t.Errorf("%s: the row has no %s: %q", when, failedGlyph(), row)
	}
}

func TestTemplatesFailureSurvivesRefreshAndLeavingThenRetryClearsIt(t *testing.T) {
	a := templatesRouter(t)
	openTemplates(t, a)
	if row := templatesRow(t, a); strings.Contains(row, failedGlyph()) || !strings.Contains(row, theme.IconRepository) {
		t.Fatalf("a fresh row should show its rest icon only: %q", row)
	}

	failSyncOnRow(a)
	wantTemplatesFailed(t, a, "after the failed sync")

	_, cmd := a.Update(testutil.Key("ctrl+r"))
	for _, msg := range testutil.Msgs(cmd) {
		a.Update(msg)
	}
	wantTemplatesFailed(t, a, "after ctrl+r")

	_ = a.switchView(command.ViewJobs)
	openTemplates(t, a)
	wantTemplatesFailed(t, a, "after a round trip through :jobs")

	// A config save or a context switch rebuilds every view.
	delete(a.views, command.ViewTemplates)
	openTemplates(t, a)
	wantTemplatesFailed(t, a, "after the view was rebuilt")

	// A new attempt (F) clears the mark. Only the start message is fed: running
	// its work would fetch the source.
	_, cmd = a.Update(testutil.Key("F"))
	var start *jobs.StartMsg
	for _, msg := range testutil.Msgs(cmd) {
		if s, ok := msg.(jobs.StartMsg); ok {
			start = &s
		}
	}
	if start == nil {
		t.Fatal("F on the failed row started nothing")
	}
	a.Update(*start)
	row := templatesRow(t, a)
	if strings.Contains(row, failedGlyph()) {
		t.Errorf("a retry in flight still shows %s: %q", failedGlyph(), row)
	}
	if !strings.Contains(row, indicator.Glyph(indicator.Queued, true)) {
		t.Errorf("a retry waiting to start has no queued glyph: %q", row)
	}

	a.Update(templates.SyncStartingMsg{Slug: "api"})
	if row := templatesRow(t, a); !strings.Contains(row, indicator.Glyph(indicator.Running, true)) &&
		!strings.Contains(row, indicator.Glyph(indicator.Running, false)) {
		t.Errorf("a running retry shows neither phase of the running dot: %q", row)
	}

	a.Update(templates.SyncCompleteMsg{Slug: "api", Name: "API", Files: 3})
	row = templatesRow(t, a)
	if strings.Contains(row, failedGlyph()) || !strings.Contains(row, theme.IconRepository) {
		t.Errorf("after a successful retry the row should be at rest: %q", row)
	}
}
