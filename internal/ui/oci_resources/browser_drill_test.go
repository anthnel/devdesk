package ociresources

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/imagesearch"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// → at level 1 (DEVDESK-304, IS-08): it opens the tags of the row under the
// cursor, on that row's registry only, with the row's final repository as is.
// No test reaches a registry: the listRegistryTags seam answers, and the Cmd
// the handler returns for Hub metadata is never run.

// withEngine installs name as the current engine for the test.
func withEngine(t *testing.T, name string) {
	t.Helper()
	previous := engine.Current()
	t.Cleanup(func() { engine.SetCurrent(previous) })
	engine.SetCurrent(engine.ShapeFor(name))
}

// drillRows is level 1 settled with an `nginx` row on each registry of
// testConfig: "prod" (registry.example.com) and "hub" (docker.io, whose final
// repository carries library/).
func drillRows(t *testing.T) Model {
	t.Helper()
	m := lvl1SearchingModel(t)
	return feed(t, m,
		ImageSearchResultMsg{Gen: m.registryBrowser.gen, EntryKey: "prod",
			Images: []imagesearch.Image{{Name: "nginx", Repo: "nginx"}}},
		ImageSearchResultMsg{Gen: m.registryBrowser.gen, EntryKey: "hub",
			Images: []imagesearch.Image{{Name: "nginx", Repo: "library/nginx"}}},
	)
}

// selectImage moves the level 1 cursor to the row of (entryKey, repo).
func selectLevelOneRow(t *testing.T, m Model, entryKey, repo string) Model {
	t.Helper()
	m = feed(t, m, testutil.Key("home"))
	for range len(m.registryBrowser.searchTable.Visible()) + 1 {
		if r, ok := m.registryBrowser.searchTable.Selected(); ok && r.img.EntryKey == entryKey && r.img.Repo == repo {
			return m
		}
		m = feed(t, m, testutil.Key("down"))
	}
	t.Fatalf("no level 1 row for %s %s", entryKey, repo)
	return m
}

// pressRight presses → and plays the tag answers back into the model.
func pressRight(t *testing.T, m Model) (Model, tea.Cmd, []MultiRegistryTagsLoadedMsg) {
	t.Helper()
	m, cmd := step(t, m, testutil.Key("right"))
	var loaded []MultiRegistryTagsLoadedMsg
	for _, msg := range testutil.Msgs(cmd) {
		if l, ok := msg.(MultiRegistryTagsLoadedMsg); ok {
			loaded = append(loaded, l)
		}
	}
	for _, l := range loaded {
		m = feed(t, m, l)
	}
	return m, cmd, loaded
}

func tagRefs(m Model) []string {
	var out []string
	for _, tag := range m.registryBrowser.tags {
		out = append(out, multiImageName(tag.RegistryURL, tag.Repo, tag.Tag))
	}
	sort.Strings(out)
	return out
}

func (r *fullTagCalls) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = nil
}

// c.1: one request, to the row's registry, with the row's repository.
func TestRightOpensTheTagsOfTheSelectedRowOnly(t *testing.T) {
	withEngine(t, engine.Docker)
	rec := withFullTagRecorder(t)
	m := selectLevelOneRow(t, drillRows(t), "hub", "library/nginx")

	m, _, loaded := pressRight(t, m)

	b := m.registryBrowser
	if b.state != browserStateTags {
		t.Fatalf("state = %d, want browserStateTags", b.state)
	}
	if !b.tagsFromSearch {
		t.Error("tagsFromSearch is false after →")
	}
	want := []string{fmt.Sprintf("%q", []string{"https://registry-1.docker.io", "library/nginx", "", ""})}
	if got := rec.sorted(); !slices.Equal(got, want) {
		t.Errorf("requests = %v, want %v", got, want)
	}
	if len(loaded) != 1 || loaded[0].Repo != "library/nginx" || loaded[0].EntryKey != "hub" {
		t.Errorf("answers = %+v, want one for hub library/nginx", loaded)
	}
}

// c.1: the row's repository is final, nothing adds library/ or a prefix.
func TestRightDoesNotNormalizeTheRepository(t *testing.T) {
	for _, tc := range []struct{ entryKey, repo string }{
		{"prod", "nginx"},
		{"hub", "library/nginx"},
	} {
		t.Run(tc.entryKey, func(t *testing.T) {
			rec := withFullTagRecorder(t)
			m := selectLevelOneRow(t, drillRows(t), tc.entryKey, tc.repo)
			pressRight(t, m)
			if got := rec.repos(); !slices.Equal(got, []string{tc.repo}) {
				t.Errorf("repos asked = %v, want [%s] as is", got, tc.repo)
			}
		})
	}
}

// c.4: two registries return the same name; → asks only the one under the cursor.
func TestRightOnTheSecondRegistryAsksOnlyThatRegistry(t *testing.T) {
	for _, tc := range []struct{ entryKey, repo, api string }{
		{"prod", "nginx", "https://registry.example.com"},
		{"hub", "library/nginx", "https://registry-1.docker.io"},
	} {
		t.Run(tc.entryKey, func(t *testing.T) {
			rec := withFullTagRecorder(t)
			m := selectLevelOneRow(t, drillRows(t), tc.entryKey, tc.repo)
			m, _, _ = pressRight(t, m)
			rec.mu.Lock()
			calls := slices.Clone(rec.calls)
			rec.mu.Unlock()
			if len(calls) != 1 || calls[0].apiURL != tc.api {
				t.Fatalf("requests = %+v, want exactly one to %s", calls, tc.api)
			}
			if len(m.registryBrowser.tags) == 0 {
				t.Fatal("no tag rows")
			}
			for _, tag := range m.registryBrowser.tags {
				if tag.EntryKey != tc.entryKey {
					t.Errorf("tag row of %q, want only %q", tag.EntryKey, tc.entryKey)
				}
			}
		})
	}
}

// drillCase is one registry declaration, the level 1 row it returns, and the
// name that reaches the same image through the full name flow.
type drillCase struct {
	name string
	reg  config.RegistryItem
	row  imagesearch.Image
	full string
	// ref is the pull reference of tag v1, per engine.
	ref map[string]string
}

func drillCases() []drillCase {
	return []drillCase{
		{
			name: "hub", reg: config.RegistryItem{URL: "docker.io", Slug: "reg", Alias: "reg", AuthMode: config.AuthAnonymous},
			row: imagesearch.Image{Name: "nginx", Repo: "library/nginx"}, full: "library/nginx",
			ref: map[string]string{engine.Docker: "library/nginx:v1", engine.Podman: "docker.io/library/nginx:v1"},
		},
		{
			name: "hub bare name", reg: config.RegistryItem{URL: "docker.io", Slug: "reg", Alias: "reg", AuthMode: config.AuthAnonymous},
			row: imagesearch.Image{Name: "nginx", Repo: "library/nginx"}, full: "nginx",
			ref: map[string]string{engine.Docker: "library/nginx:v1", engine.Podman: "docker.io/library/nginx:v1"},
		},
		{
			name: "private", reg: config.RegistryItem{URL: "registry.example.com", Slug: "reg", Alias: "reg", AuthMode: config.AuthAnonymous},
			row: imagesearch.Image{Name: "api", Repo: "api"}, full: "api",
			ref: map[string]string{engine.Docker: "registry.example.com/api:v1", engine.Podman: "registry.example.com/api:v1"},
		},
		{
			name: "prefixed", reg: config.RegistryItem{URL: "registry.example.com", Slug: "reg", Alias: "reg", AuthMode: config.AuthAnonymous, RepoPrefix: "team"},
			row: imagesearch.Image{Name: "api", Repo: "team/api"}, full: "api",
			ref: map[string]string{engine.Docker: "registry.example.com/team/api:v1", engine.Podman: "registry.example.com/team/api:v1"},
		},
	}
}

// drillBrowser opens the browser on the one registry reg, a Searcher injected.
func drillBrowser(t *testing.T, reg config.RegistryItem) Model {
	t.Helper()
	withImageSearcher(t, &fakeSearcher{})
	cfg := config.Default()
	cfg.Registry.Registries = []config.RegistryItem{reg}
	m := feed(t, New(cfg), tea.WindowSizeMsg{Width: 180, Height: 30}, ImagesListMsg{Images: imageFixtures()})
	m = feed(t, m, testutil.Key(keymap.Browser))
	if m.registryBrowser == nil || len(m.registryBrowser.entries) != 1 {
		t.Fatal("the browser did not open on one entry")
	}
	return m
}

// c.3 and c.5: → and the full name give the same requests and the same
// references, for both engines and for Hub, private and prefixed registries.
func TestRightGivesTheSameReferencesAsTheFullName(t *testing.T) {
	for _, eng := range []string{engine.Docker, engine.Podman} {
		for _, tc := range drillCases() {
			t.Run(eng+"/"+tc.name, func(t *testing.T) {
				withEngine(t, eng)
				rec := withFullTagRecorder(t)

				// Level 1, then →.
				m := drillBrowser(t, tc.reg)
				key := m.registryBrowser.entries[0].key
				m = typeInto(t, m, "x")
				m, _ = step(t, m, testutil.Key("enter"))
				row := tc.row
				m = feed(t, m, ImageSearchResultMsg{Gen: m.registryBrowser.gen, EntryKey: key, Images: []imagesearch.Image{row}})
				rec.reset()
				m, _, _ = pressRight(t, m)
				drillRequests, drillRefs := rec.sorted(), tagRefs(m)

				// The full name flow, on a new browser.
				rec.reset()
				full := runTagSubmit(t, drillBrowser(t, tc.reg), tc.full, actionOpenTags, rec)

				if !slices.Equal(drillRequests, full.requests) {
					t.Errorf("→ asked %v, the full name asked %v", drillRequests, full.requests)
				}
				if !slices.Equal(drillRefs, full.refs) {
					t.Errorf("→ references %v, the full name %v", drillRefs, full.refs)
				}
				if want := []string{tc.ref[eng]}; !slices.Contains(drillRefs, want[0]) {
					t.Errorf("references = %v, want %s among them", drillRefs, want[0])
				}
				for _, ref := range drillRefs {
					if strings.Contains(ref, "team/team") || strings.Contains(ref, "library/library") {
						t.Errorf("reference %q applies the prefix or library/ twice", ref)
					}
				}
			})
		}
	}
}

// c.3: an image scanned through the full name shows its counts after →.
func TestRightShowsCountsOfAnImageScannedByTheFullName(t *testing.T) {
	withEngine(t, engine.Docker)
	withFullTagRecorder(t)
	m := selectLevelOneRow(t, drillRows(t), "hub", "library/nginx")
	m.scanCache = map[string]cache.ImageScanEntry{
		"library/nginx:latest": {Critical: 2, High: 3, Medium: 4, Low: 5, ScannedAt: at(1)},
	}

	m, _, _ = pressRight(t, m)

	var found bool
	for _, row := range m.registryBrowser.tagTable.Items() {
		if row.tag.Tag != "latest" {
			continue
		}
		found = true
		if !row.scanned || row.entry.Critical != 2 || row.entry.Low != 5 {
			t.Errorf("latest row = scanned %v, entry %+v, want the cached counts", row.scanned, row.entry)
		}
	}
	if !found {
		t.Fatal("no latest row")
	}
	for range 4 {
		if r, ok := m.registryBrowser.tagTable.Selected(); ok && r.tag.Tag == "latest" {
			break
		}
		m = feed(t, m, testutil.Key("down"))
	}
	_, cmd := step(t, m, testutil.Key("enter"))
	msg, ok := testutil.MsgOf[ScanDetailsRequestMsg](cmd)
	if !ok || msg.ImageName != "library/nginx:latest" {
		t.Errorf("enter gave %+v (ok=%v), want details of library/nginx:latest", msg, ok)
	}
}

// The request carries the entry's own credentials; an anonymous entry stays anonymous (D12).
func TestRightUsesTheEntryCredentials(t *testing.T) {
	rec := withFullTagRecorder(t)
	for _, tc := range []struct{ entryKey, repo, user string }{
		{"prod", "nginx", "anthnel"},
		{"hub", "library/nginx", ""},
	} {
		rec.reset()
		// Fresh model per entry: registryBrowser is a pointer, so pressing right
		// would leave a reused model in the tags level.
		mm := selectLevelOneRow(t, drillRows(t), tc.entryKey, tc.repo)
		entry := mm.registryBrowser.entryFor(tc.entryKey)
		wantUser, wantPass := mm.registryBrowser.credsFor(*entry)
		pressRight(t, mm)
		rec.mu.Lock()
		calls := slices.Clone(rec.calls)
		rec.mu.Unlock()
		if len(calls) != 1 || calls[0].user != wantUser || calls[0].pass != wantPass || calls[0].user != tc.user {
			t.Errorf("%s: request %+v, want user %q (credsFor: %q)", tc.entryKey, calls, tc.user, wantUser)
		}
	}
}

// H1: the level 1 table is not emptied, so a later return finds what it left.
func TestRightKeepsLevelOneIntact(t *testing.T) {
	withFullTagRecorder(t)
	m := selectLevelOneRow(t, drillRows(t), "hub", "library/nginx")
	b := m.registryBrowser
	images, names, cursor := len(b.images), lvl1Names(m), b.searchTable.Cursor()

	m, _, _ = pressRight(t, m)

	b = m.registryBrowser
	if len(b.images) != images {
		t.Errorf("images = %d, want %d", len(b.images), images)
	}
	if got := lvl1Names(m); !slices.Equal(got, names) {
		t.Errorf("level 1 rows = %v, want %v", got, names)
	}
	if got := b.searchTable.Cursor(); got != cursor {
		t.Errorf("level 1 cursor = %d, want %d", got, cursor)
	}
}

// QB2-A: → cancels a level 1 search still running; its late answers are dropped.
func TestRightCancelsALevelOneSearchStillRunning(t *testing.T) {
	withFullTagRecorder(t)
	m := lvl1SearchingModel(t)
	oldGen := m.registryBrowser.gen
	m = feed(t, m, ImageSearchResultMsg{Gen: oldGen, EntryKey: "prod",
		Images: []imagesearch.Image{{Name: "nginx", Repo: "nginx"}}})
	if m.registryBrowser.pendingSearches != 1 {
		t.Fatalf("pendingSearches = %d, want 1 (hub still searching)", m.registryBrowser.pendingSearches)
	}

	m, _ = step(t, m, testutil.Key("right"))

	b := m.registryBrowser
	if b.gen == oldGen {
		t.Error("gen did not change: the level 1 lot is still the open one")
	}
	if b.pendingSearches != 1 {
		t.Errorf("pendingSearches = %d, want 1 (the tag request)", b.pendingSearches)
	}
	if b.AddImageResults(ImageSearchResultMsg{Gen: oldGen, EntryKey: "hub",
		Images: []imagesearch.Image{{Name: "late", Repo: "late"}}}) {
		t.Error("a late level 1 answer was taken")
	}
	if len(b.images) != 1 {
		t.Errorf("images = %d, want the 1 that had arrived", len(b.images))
	}
}

// c.6, QB1-B: no row, nothing happens but a warning.
func TestRightWithoutARowWarnsAndStays(t *testing.T) {
	for _, tc := range []struct {
		name  string
		model func(*testing.T) Model
	}{
		{"no answer yet", lvl1SearchingModel},
		{"filter without result", func(t *testing.T) Model {
			m := lvl1ImagesModel(t)
			m = feed(t, m, testutil.Key("/"))
			m = feed(t, m, testutil.Type("zzzzzz")...)
			m = feed(t, m, testutil.Key("enter"))
			if got := len(m.registryBrowser.searchTable.Visible()); got != 0 {
				t.Fatalf("fixture shows %d rows, want 0", got)
			}
			return m
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := withFullTagRecorder(t)
			m := tc.model(t)
			gen := m.registryBrowser.gen

			next, cmd := step(t, m, testutil.Key("right"))

			if next.registryBrowser.state != browserStateImages {
				t.Errorf("state = %d, want to stay at level 1", next.registryBrowser.state)
			}
			if next.registryBrowser.gen != gen {
				t.Error("gen changed: a lot was started")
			}
			warn, ok := testutil.MsgOf[RegistryBrowserWarnMsg](cmd)
			if !ok || warn.Text != reasonNoImage {
				t.Fatalf("Cmd gave %+v (ok=%v), want RegistryBrowserWarnMsg{%q}", warn, ok, reasonNoImage)
			}
			next = feed(t, next, warn)
			if next.footer.Level() != components.LevelWarning || next.footer.Text() != reasonNoImage {
				t.Errorf("footer = %q at level %v, want a warning %q", next.footer.Text(), next.footer.Level(), reasonNoImage)
			}
			if got := rec.sorted(); len(got) != 0 {
				t.Errorf("requests = %v, want none", got)
			}
		})
	}
}

// → in the filter bar stays with the text field.
func TestRightWhileTypingAFilterStaysInTheFilter(t *testing.T) {
	withFullTagRecorder(t)
	m := lvl1ImagesModel(t)
	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("ngi")...)
	gen := m.registryBrowser.gen

	m = feed(t, m, testutil.Key("right"))

	b := m.registryBrowser
	if b.state != browserStateImages || b.gen != gen || b.tagsFromSearch {
		t.Errorf("state %d, gen changed %v, tagsFromSearch %v: → left the filter", b.state, b.gen != gen, b.tagsFromSearch)
	}
}

// QB3: the title names the opened row, not the typed text; the full name flow keeps the typed text.
func TestTheTitleNamesTheOpenedRow(t *testing.T) {
	withFullTagRecorder(t)
	m := lvl1SearchingModel(t)
	m = feed(t, m, ImageSearchResultMsg{Gen: m.registryBrowser.gen, EntryKey: "prod",
		Images: []imagesearch.Image{{Name: "bitnami/nginx", Repo: "bitnami/nginx"}}})

	m, _, _ = pressRight(t, m)

	if title := ansi.Strip(m.GetTitle()); !strings.HasSuffix(title, "bitnami/nginx") {
		t.Errorf("title = %q, want it to end with the opened row bitnami/nginx", title)
	}

	withImageSearcher(t, &fakeSearcher{})
	f := typeInto(t, browsingModel(t), "library/nginx")
	f.registryBrowser.submit(actionOpenTags)
	if title := ansi.Strip(f.GetTitle()); !strings.HasSuffix(title, "library/nginx") {
		t.Errorf("full name title = %q, want the typed text", title)
	}
}

// ── header and help ─────────────────────────────────────────────────────────

func TestLevelOneRightIsGreyedWithoutARow(t *testing.T) {
	sc := lvl1SearchingModel(t).GetShortcuts()
	if !testutil.HasShortcut(sc, "→") || !testutil.ShortcutDisabled(sc, "→") {
		t.Errorf("→ without a row: present %v, want present and greyed (%v)",
			testutil.HasShortcut(sc, "→"), testutil.ShortcutKeys(sc))
	}
	sc = lvl1ImagesModel(t).GetShortcuts()
	for _, s := range sc {
		if s.Key == "→" && s.Description != "Open tags" {
			t.Errorf("→ label = %q, want Open tags", s.Description)
		}
	}
}

// One computation for the header and the handler (Rule 130).
func TestImageDrillInAgreesWithTheShortcut(t *testing.T) {
	for _, m := range []Model{lvl1SearchingModel(t), lvl1ImagesModel(t)} {
		enabled := m.registryBrowser.imageDrillIn().Enabled()
		if got := testutil.ShortcutEnabled(m.GetShortcuts(), "→"); got != enabled {
			t.Errorf("imageDrillIn enabled = %v, shortcut enabled = %v", enabled, got)
		}
	}
	if r := lvl1SearchingModel(t).registryBrowser.imageDrillIn().Reason; r != reasonNoImage {
		t.Errorf("reason = %q, want %q", r, reasonNoImage)
	}
}

func TestRegistryBrowserHelpMentionsOpeningTagsWithRight(t *testing.T) {
	with := registryBrowserHelp(t, lvl1ImagesModel(t))
	if !strings.Contains(with, "→") {
		t.Error("help with a Searcher does not mention →")
	}
	m := lvl1ImagesModel(t)
	m.registryBrowser.searcher = nil
	if strings.Contains(registryBrowserHelp(t, m), "→") {
		t.Error("help without a Searcher mentions →")
	}
}

// ── failure after → ─────────────────────────────────────────────────────────

// afterRight presses → on the hub row (nothing is run) and returns the model.
func afterRight(t *testing.T) Model {
	t.Helper()
	withFullTagRecorder(t)
	m, _ := step(t, selectLevelOneRow(t, drillRows(t), "hub", "library/nginx"), testutil.Key("right"))
	return m
}

func failedTags(m Model, err error) MultiRegistryTagsLoadedMsg {
	return MultiRegistryTagsLoadedMsg{
		Gen: m.registryBrowser.gen, EntryKey: "hub", RegistryURL: "docker.io", Alias: "hub",
		Repo: "library/nginx", Err: err,
	}
}

func TestTagsFailingAfterRightShowAnErrorAndKeepTheTable(t *testing.T) {
	m := afterRight(t)

	m, cmd := step(t, m, failedTags(m, errors.New("boom")))

	if m.registryBrowser.state != browserStateTags {
		t.Fatalf("state = %d, want browserStateTags", m.registryBrowser.state)
	}
	if got := len(m.registryBrowser.tagTable.Items()); got != 0 {
		t.Errorf("tag rows = %d, want 0", got)
	}
	view := ansi.Strip(m.registryBrowser.View())
	for _, col := range []string{"Registry", "Tag"} {
		if !strings.Contains(view, col) {
			t.Errorf("the table lost its %q header (Rule 139):\n%s", col, view)
		}
	}
	if m.footer.Level() != components.LevelError || m.footer.Text() != tagsLoadFailedText {
		t.Errorf("footer = %q at level %v, want the error %q", m.footer.Text(), m.footer.Level(), tagsLoadFailedText)
	}
	if cmd == nil {
		t.Error("no Cmd: the footer message has no timer")
	}
	if m.registryBrowser.pendingSearches != 0 {
		t.Errorf("pendingSearches = %d, want 0", m.registryBrowser.pendingSearches)
	}
}

func TestTagsFailingAfterRightAreLoggedWithoutSecrets(t *testing.T) {
	for name, mkErr := range map[string]func() error{
		"plain": func() error {
			return errors.New("GET https://bob:s3cretpw@registry.example.com/v2/x/tags/list?password=hunter2: refused")
		},
		"url error": func() error {
			return &url.Error{Op: "Get", URL: "https://bob:s3cretpw@registry.example.com/v2/x/tags/list?password=hunter2",
				Err: errors.New("connection refused")}
		},
	} {
		t.Run(name, func(t *testing.T) {
			logs := captureLog(t)
			m := afterRight(t)
			step(t, m, failedTags(m, mkErr()))
			out := logs.String()
			if !strings.Contains(out, "registry tags on") || !strings.Contains(out, "hub") {
				t.Errorf("log = %q, want a line `registry tags on hub`", out)
			}
			for _, secret := range []string{"s3cretpw", "hunter2", "bob"} {
				if strings.Contains(out, secret) {
					t.Errorf("log leaks %q: %s", secret, out)
				}
			}
		})
	}
}

// c.7: the full name flow is unchanged: silent, and its own log line.
func TestTheFullNameFlowStaysSilentOnFailure(t *testing.T) {
	logs := captureLog(t)
	withFullTagRecorder(t)
	withImageSearcher(t, &fakeSearcher{})
	m := typeInto(t, browsingModel(t), "library/nginx")
	m.registryBrowser.submit(actionOpenTags)
	if m.registryBrowser.tagsFromSearch {
		t.Fatal("the full name flow is marked as coming from level 1")
	}

	m, cmd := step(t, m, failedTags(m, errors.New("boom")))

	if m.footer.IsSet() {
		t.Errorf("footer = %q, want none", m.footer.Text())
	}
	if cmd != nil {
		t.Errorf("Cmd = %T, want nil", testutil.Msg(cmd))
	}
	if !strings.Contains(logs.String(), "registry tags library/nginx from") {
		t.Errorf("log = %q, want the existing `registry tags library/nginx from` line", logs.String())
	}
}

func TestAStaleFailureAfterRightShowsNothing(t *testing.T) {
	m := afterRight(t)
	stale := failedTags(m, errors.New("boom"))
	m = feed(t, m, testutil.Key("esc"))
	if m.registryBrowser.state != browserStateInput {
		t.Fatalf("state = %d, want the form after esc", m.registryBrowser.state)
	}
	m.registryBrowser.submit(actionOpenTags)

	m, _ = step(t, m, stale)

	if m.footer.IsSet() {
		t.Errorf("a stale failure set the footer: %q", m.footer.Text())
	}
}

func TestTheTagsErrorExpires(t *testing.T) {
	m := afterRight(t)
	m, _ = step(t, m, failedTags(m, errors.New("boom")))
	if !m.footer.IsSet() {
		t.Fatal("no footer message to expire")
	}
	m = feed(t, m, components.ClearFooterMsg{ID: m.footer.ID()})
	if m.footer.IsSet() {
		t.Errorf("footer still shows %q", m.footer.Text())
	}
}

// The step 1 refactor keeps the full name flow out of level 1.
func TestTheFullNameFlowIsNotFromSearch(t *testing.T) {
	rec := withFullTagRecorder(t)
	withImageSearcher(t, &fakeSearcher{})
	m := typeInto(t, browsingModel(t), "library/nginx")
	_, cmd := m.registryBrowser.submit(actionOpenTags)
	testutil.Msgs(cmd)
	if m.registryBrowser.state != browserStateTags || m.registryBrowser.tagsFromSearch {
		t.Errorf("state %d, tagsFromSearch %v", m.registryBrowser.state, m.registryBrowser.tagsFromSearch)
	}
	if got := len(rec.sorted()); got != 2 {
		t.Errorf("requests = %d, want one per checked registry (2)", got)
	}
}
