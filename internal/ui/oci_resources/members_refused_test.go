package ociresources

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-510 (NX-07), step 4: what a refused group says, and where. Red phase.
//
// Formats decided with the UX answer (US English, plain text in cells, Rule 122):
//
//	Members cell, refused, nothing listed:  "denied · <TimeAgo(refusal.At)>"
//	Members cell, refused, N members known: "denied · N known · <TimeAgo(refusal.At)>"
//	  (the state comes first, the count last known stays visible, D23; the age is
//	  the refusal's, the last attempt. The column has to be wide enough to hold it
//	  whole: the tests read the rendered row, not only the cell.)
//	Members cell, settled empty:            "0 · <TimeAgo>"  (unchanged)
//	Members cell, never probed:             "never"          (unchanged)
//	Members cell, listing newer than the refusal: the refusal is stale and the
//	  cell is the plain "N · <TimeAgo(listing)>".
//
// Status line (derived on every render, no timer, no new shortcut):
//
//	refused:                   Warn  "Members could not be read with this account — declare them by hand with repo_prefix"
//	refused, N declared:       Warn  "Members could not be read with this account — N declared by hand are used"
//	settled empty (0 members): Info  "No member found — this group is empty"
//
// The footer is never the only trace: the cell carries the state as a word.

const (
	statusRefusedLine  = "Members could not be read with this account — declare them by hand with repo_prefix"
	statusDeclaredFmt  = "Members could not be read with this account — %d declared by hand are used"
	statusEmptyGroup   = "No member found — this group is empty"
	drillInNoMembers   = "No members to open — declare them by hand with repo_prefix"
	registryHostInTest = "registry.example.com"
)

// refusalCases are the three refusals the wording must handle (CA4).
var refusalCases = []struct {
	name    string
	refusal cache.RegistryGroupRefusal
}{
	{"403 account", cache.RegistryGroupRefusal{At: at(7), Status: 403}},
	{"401 account", cache.RegistryGroupRefusal{At: at(7), Status: 401}},
	{"anonymous", cache.RegistryGroupRefusal{At: at(7), Status: 403, Anonymous: true}},
}

// wantRefusalText checks what every refusal text shares: plain text, English
// punctuation only, no raw error, no URL, and nothing that reads as an empty group.
func wantRefusalText(t *testing.T, what, text string) {
	t.Helper()
	if strings.Contains(text, "\x1b") {
		t.Errorf("%s = %q carries an escape sequence (Rule 122)", what, text)
	}
	for _, r := range text {
		if r > 0x7e && r != '—' && r != '·' {
			t.Errorf("%s = %q holds %q: US English only (Rule 129)", what, text, r)
			break
		}
	}
	for _, bad := range []string{"management API returned", registryHostInTest, "docker-group", "http", "0 members", "empty", "No member found"} {
		if strings.Contains(text, bad) {
			t.Errorf("%s = %q contains %q: a refusal must not leak the error or read as an empty group", what, text, bad)
		}
	}
}

func wantAll(t *testing.T, what, text string, words ...string) {
	t.Helper()
	for _, w := range words {
		if !strings.Contains(text, w) {
			t.Errorf("%s = %q, want it to contain %q", what, text, w)
		}
	}
}

// ── The pure wording (H2, H3, H4) ───────────────────────────────────────────

func TestRefusedMembersCellSaysDeniedAndWhen(t *testing.T) {
	for _, tc := range refusalCases {
		t.Run(tc.name, func(t *testing.T) {
			ago := theme.TimeAgo(tc.refusal.At)

			got := refusedMembersCell(groupRefusalView{Refusal: tc.refusal})
			if want := "denied · " + ago; got != want {
				t.Errorf("nothing listed: cell = %q, want %q", got, want)
			}
			wantRefusalText(t, "cell", got)

			got = refusedMembersCell(groupRefusalView{Refusal: tc.refusal, Known: 3})
			if !strings.HasPrefix(got, "denied") {
				t.Errorf("3 known: cell = %q, want the state first", got)
			}
			wantAll(t, "3 known: cell", got, "3 known", ago)
			wantRefusalText(t, "cell", got)
		})
	}
}

func TestRefusedStatusLineWording(t *testing.T) {
	for _, tc := range refusalCases {
		t.Run(tc.name, func(t *testing.T) {
			none := refusedStatusLine(groupRefusalView{Refusal: tc.refusal})
			declared := refusedStatusLine(groupRefusalView{Refusal: tc.refusal, Declared: 2})
			wantAll(t, "status line", none, "repo_prefix")
			wantRefusalText(t, "status line", none)
			wantRefusalText(t, "status line (declared)", declared)
			if !tc.refusal.Anonymous {
				if none != statusRefusedLine {
					t.Errorf("status line = %q, want %q", none, statusRefusedLine)
				}
				if want := fmt.Sprintf(statusDeclaredFmt, 2); declared != want {
					t.Errorf("status line with 2 declared = %q, want %q", declared, want)
				}
			}
			if declared == none {
				t.Error("declared members must change the line: it says they are used")
			}
		})
	}
}

func TestRefusedDrillInTextWording(t *testing.T) {
	for _, tc := range refusalCases {
		t.Run(tc.name, func(t *testing.T) {
			got := refusedDrillInText(groupRefusalView{Refusal: tc.refusal})
			wantAll(t, "drill-in text", got, "repo_prefix")
			wantRefusalText(t, "drill-in text", got)
			if strings.Contains(got, "ctrl+r") {
				t.Errorf("drill-in text = %q: ctrl+r cannot help after a refusal", got)
			}
			if !tc.refusal.Anonymous && got != drillInNoMembers {
				t.Errorf("drill-in text = %q, want %q", got, drillInNoMembers)
			}
		})
	}
}

// ── H1: the footer after a refused ctrl+r ───────────────────────────────────

func TestTheFooterAfterARefusedRefreshExplainsAndStaysAWarning(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		words []string
	}{
		{"account 403", refusalOf(403, false), []string{"could not be read", "this account", "repo_prefix"}},
		{"account 401", refusalOf(401, false), []string{"could not be read", "this account", "repo_prefix"}},
		{"anonymous", refusalOf(403, true), []string{"repo_prefix"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := groupModel(t)
			m = feed(t, m, RegistryGroupDetectedMsg{RegistryURL: registryHostInTest, Slug: "prod", Err: tc.err, At: at(7)})

			if m.footer.Level() != sharedcomponents.LevelWarning {
				t.Errorf("footer level = %v, want Warn (%q)", m.footer.Level(), m.footer.Text())
			}
			wantAll(t, "footer", m.footer.Text(), tc.words...)
			wantRefusalText(t, "footer", m.footer.Text())
			if strings.Contains(m.footer.Text(), tc.err.Error()) {
				t.Errorf("footer = %q carries the error text", m.footer.Text())
			}
		})
	}
}

// ── H2/H3 through the model ─────────────────────────────────────────────────

func listing(members int, day int) *cache.RegistryGroupEntry {
	e := cache.RegistryGroupEntry{DiscoveredAt: at(day)}
	for i := range members {
		e.Members = append(e.Members, cache.RegistryGroupMember{
			Alias: fmt.Sprintf("m%d", i), URL: registryHostInTest, RepoPrefix: fmt.Sprintf("m%d", i),
		})
	}
	return &e
}

// tabWith is the Registries tab, group row selected, with the cache the test hands in.
func tabWith(t *testing.T, cfg *config.Config, entry *cache.RegistryGroupEntry, refusal *cache.RegistryGroupRefusal) Model {
	t.Helper()
	m := feed(t, New(cfg), tea.WindowSizeMsg{Width: 180, Height: 30},
		ImagesListMsg{Images: imageFixtures()}, NetworksListMsg{Networks: networkFixtures()}, VolumesListMsg{Volumes: volumeFixtures()})
	loaded := RegistryGroupCacheLoadedMsg{Entries: map[string]cache.RegistryGroupEntry{}}
	if entry != nil {
		loaded.Entries["prod"] = *entry
	}
	if refusal != nil {
		loaded.Refusals = map[string]cache.RegistryGroupRefusal{"prod": *refusal}
	}
	m = feed(t, m, loaded)
	for m.activeTab != tabRegistries {
		m = feed(t, m, testutil.Key("tab"))
	}
	return m
}

func refusalAt(day int) *cache.RegistryGroupRefusal {
	return &cache.RegistryGroupRefusal{At: at(day), Status: 403}
}

func TestTheMembersCellOfEachState(t *testing.T) {
	cases := []struct {
		name    string
		entry   *cache.RegistryGroupEntry
		refusal *cache.RegistryGroupRefusal
		want    func(cell string) string // empty string = pass, else the complaint
	}{
		{"never probed", nil, nil, exactly("never")},
		{"listed", listing(2, 3), nil, exactly("2 · " + theme.TimeAgo(at(3)))},
		{"settled empty", listing(0, 3), nil, exactly("0 · " + theme.TimeAgo(at(3)))},
		{"refused, never listed", nil, refusalAt(7), exactly("denied · " + theme.TimeAgo(at(7)))},
		{"refused after an empty listing", listing(0, 3), refusalAt(7), exactly("denied · " + theme.TimeAgo(at(7)))},
		{"refused, 2 members known", listing(2, 3), refusalAt(7), func(c string) string {
			if !strings.HasPrefix(c, "denied") || !strings.Contains(c, "2 known") || !strings.Contains(c, theme.TimeAgo(at(7))) {
				return "want denied first, the 2 members known and the age of the refusal"
			}
			return ""
		}},
		{"stale refusal, listing is newer", listing(2, 9), refusalAt(7), exactly("2 · " + theme.TimeAgo(at(9)))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := tabWith(t, testConfig(), tc.entry, tc.refusal)
			cell := m.registryTable.Table().Rows()[0][membersCol]

			if complaint := tc.want(cell); complaint != "" {
				t.Errorf("Members cell = %q: %s", cell, complaint)
			}
			if strings.Contains(cell, "\x1b") {
				t.Errorf("Members cell = %q carries an escape sequence (Rule 122)", cell)
			}
			// The column must be wide enough to hold the whole word and age.
			if line := ansi.Strip(renderedRow(t, m.registryTable.View(), "prod")); !strings.Contains(line, cell) {
				t.Errorf("the rendered row does not hold the whole cell %q:\n%s", cell, line)
			}
		})
	}
}

func exactly(want string) func(string) string {
	return func(got string) string {
		if got != want {
			return fmt.Sprintf("want %q", want)
		}
		return ""
	}
}

func TestARefusedRowIsFlaggedForItsStyleOnly(t *testing.T) {
	m := tabWith(t, testConfig(), listing(2, 3), refusalAt(7))
	items := m.registryTable.Items()
	if !items[0].membersRefused {
		t.Error("the refused group's row is not flagged membersRefused")
	}
	if items[1].membersRefused {
		t.Error("another registry's row is flagged membersRefused")
	}

	var members datatable.Column[registryRow]
	for _, col := range registryColumns() {
		if col.Title == "Members" {
			members = col
		}
	}
	refused := members.Style(registryRow{membersRefused: true}).Render("denied")
	plain := members.Style(registryRow{}).Render("denied")
	if refused == plain {
		t.Error("the Members column renders a refused row like any other: the colour must not be the only carrier, but it must differ")
	}

	stale := tabWith(t, testConfig(), listing(2, 9), refusalAt(7))
	if stale.registryTable.Items()[0].membersRefused {
		t.Error("a refusal older than the listing still flags the row")
	}
}

func TestTheStatusLineFollowsTheSelectedGroupRow(t *testing.T) {
	cases := []struct {
		name      string
		entry     *cache.RegistryGroupEntry
		refusal   *cache.RegistryGroupRefusal
		wantText  string
		wantLevel sharedcomponents.Level
	}{
		{"refused, never listed", nil, refusalAt(7), statusRefusedLine, sharedcomponents.LevelWarning},
		{"refused, 2 known", listing(2, 3), refusalAt(7), statusRefusedLine, sharedcomponents.LevelWarning},
		{"settled empty", listing(0, 3), nil, statusEmptyGroup, sharedcomponents.LevelInfo},
		{"refused after an empty listing", listing(0, 3), refusalAt(7), statusRefusedLine, sharedcomponents.LevelWarning},
		{"listed", listing(2, 3), nil, "", sharedcomponents.LevelInfo},
		{"never probed", nil, nil, "", sharedcomponents.LevelInfo},
		{"stale refusal", listing(2, 9), refusalAt(7), "", sharedcomponents.LevelInfo},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := tabWith(t, testConfig(), tc.entry, tc.refusal)
			st := m.status()
			if st.Text != tc.wantText || (tc.wantText != "" && st.Level != tc.wantLevel) {
				t.Errorf("status = %q at level %v, want %q at level %v", st.Text, st.Level, tc.wantText, tc.wantLevel)
			}
			if tc.wantText != "" {
				m = feed(t, m, testutil.Key("down"))
				if got := m.status().Text; got != "" {
					t.Errorf("another row selected: status = %q, want nothing: the line belongs to the group row", got)
				}
			}
		})
	}
}

func TestTheStatusLineCountsTheMembersDeclaredByHand(t *testing.T) {
	m := tabWith(t, declaredConfig(), nil, refusalAt(7))
	st := m.status()
	if want := fmt.Sprintf(statusDeclaredFmt, 1); st.Text != want || st.Level != sharedcomponents.LevelWarning {
		t.Errorf("status = %q at level %v, want Warn %q", st.Text, st.Level, want)
	}
}

func TestDeclaredMembersCountsTheEntriesOfOneGroup(t *testing.T) {
	regs := declaredConfig().Registry.Registries
	regs = append(regs,
		config.RegistryItem{Slug: "other", Kind: config.KindRegistry, Parent: "elsewhere", URL: registryHostInTest, RepoPrefix: "x", AuthMode: config.AuthInherit},
		config.RegistryItem{Slug: "dhi2", Kind: config.KindRegistry, Parent: "prod", URL: registryHostInTest, RepoPrefix: "dhi2", AuthMode: config.AuthInherit})
	if got := declaredMembers(regs, "prod"); got != 2 {
		t.Errorf("declaredMembers(prod) = %d, want 2", got)
	}
	if got := declaredMembers(regs, "nobody"); got != 0 {
		t.Errorf("declaredMembers(nobody) = %d, want 0", got)
	}
}

// The state is held by the cell and the status line, not by the 3-second footer:
// once the message expires, the refusal is still on screen.
func TestTheRefusalOutlivesItsFooterMessage(t *testing.T) {
	m := groupModel(t)
	m = feed(t, m, RegistryGroupDetectedMsg{RegistryURL: registryHostInTest, Slug: "prod", Err: refusalOf(403, false), At: at(7)})
	m = feed(t, m, sharedcomponents.ClearFooterMsg{ID: m.footer.ID()})

	if m.footer.IsSet() {
		t.Fatalf("footer still says %q after its expiry", m.footer.Text())
	}
	if cell := m.registryTable.Table().Rows()[0][membersCol]; !strings.HasPrefix(cell, "denied") {
		t.Errorf("Members cell = %q after the footer expired, want it to say denied", cell)
	}
	if got := m.status().Text; got != statusRefusedLine {
		t.Errorf("status = %q after the footer expired, want %q", got, statusRefusedLine)
	}
}

func TestASettledListingTakesTheRefusalOffTheScreen(t *testing.T) {
	m := groupModel(t)
	m = feed(t, m, RegistryGroupDetectedMsg{RegistryURL: registryHostInTest, Slug: "prod", Err: refusalOf(403, false), At: at(7)})
	m = feed(t, m, testutil.Key("ctrl+r"))
	m = feed(t, m, RegistryGroupDetectedMsg{RegistryURL: registryHostInTest, Slug: "prod", At: at(8)}) // settled, empty

	if cell := m.registryTable.Table().Rows()[0][membersCol]; strings.Contains(cell, "denied") {
		t.Errorf("Members cell = %q after a settled listing", cell)
	}
	if got := m.status().Text; got != statusEmptyGroup {
		t.Errorf("status = %q, want %q after a settled empty listing", got, statusEmptyGroup)
	}
}

// Rule 130: the set of shortcut keys is the same from one state to another.
func TestARefusalDoesNotChangeTheShortcutKeys(t *testing.T) {
	neutral := testutil.ShortcutKeys(tabWith(t, testConfig(), nil, nil).GetShortcuts())
	for name, m := range map[string]Model{
		"refused":       tabWith(t, testConfig(), nil, refusalAt(7)),
		"refused known": tabWith(t, testConfig(), listing(2, 3), refusalAt(7)),
	} {
		if got := testutil.ShortcutKeys(m.GetShortcuts()); !slices.Equal(got, neutral) {
			t.Errorf("%s: shortcut keys = %v, want %v as in every other state", name, got, neutral)
		}
	}
}

// ── H4: → on a refused group ────────────────────────────────────────────────

func TestEnteringARefusedGroupWithNothingToOpenSaysSoWithoutCtrlR(t *testing.T) {
	m := tabWith(t, testConfig(), nil, refusalAt(7))

	m = feed(t, m, testutil.Key("right"))

	if m.registryGroupSlug != "" {
		t.Errorf("registryGroupSlug = %q, want the tab to stay at the top level", m.registryGroupSlug)
	}
	if m.footer.Level() != sharedcomponents.LevelWarning || m.footer.Text() != drillInNoMembers {
		t.Errorf("footer = %q at level %v, want Warn %q", m.footer.Text(), m.footer.Level(), drillInNoMembers)
	}
}

func TestEnteringARefusedGroupOpensItsDeclaredMembers(t *testing.T) {
	m := tabWith(t, declaredConfig(), nil, refusalAt(7))

	m = feed(t, m, testutil.Key("right"))

	if m.registryGroupSlug != "prod" {
		t.Fatalf("registryGroupSlug = %q (footer %q), want the group entered: declared members are members", m.registryGroupSlug, m.footer.Text())
	}
	if _, ok := findRow(m, "dhi"); !ok {
		t.Errorf("the group level lacks the declared member dhi: %+v", m.registryTable.Items())
	}
}

func TestEnteringARefusedGroupWithKnownMembersStillOpensThem(t *testing.T) {
	m := tabWith(t, testConfig(), listing(2, 3), refusalAt(7))
	m = feed(t, m, testutil.Key("right"))
	if m.registryGroupSlug != "prod" || len(m.registryTable.Items()) != 2 {
		t.Errorf("registryGroupSlug = %q with %d rows, want the 2 members last known", m.registryGroupSlug, len(m.registryTable.Items()))
	}
}

// Unchanged: a group nobody has probed still points at ctrl+r.
func TestEnteringAGroupNeverProbedStillPointsAtCtrlR(t *testing.T) {
	m := feed(t, tabWith(t, testConfig(), nil, nil), testutil.Key("right"))
	if m.registryGroupSlug != "" || !strings.Contains(m.footer.Text(), "ctrl+r") {
		t.Errorf("registryGroupSlug = %q, footer = %q, want it to stay and say ctrl+r", m.registryGroupSlug, m.footer.Text())
	}
}

// ── CA5: what is declared by hand is not hidden by the refusal ──────────────

// declaredConfig adds, to the prod group, one entry declared by hand: a member
// the account cannot list but a user can name (parent + repo_prefix).
func declaredConfig() *config.Config {
	cfg := testConfig()
	cfg.Registry.Registries = append(cfg.Registry.Registries, config.RegistryItem{
		Slug: "dhi", Alias: "dhi", Kind: config.KindRegistry, Parent: "prod",
		URL: registryHostInTest, RepoPrefix: "dhi-proxy", AuthMode: config.AuthInherit,
	})
	return cfg
}

func findRow(m Model, alias string) (registryRow, bool) {
	for _, r := range m.registryTable.Items() {
		if r.alias == alias {
			return r, true
		}
	}
	return registryRow{}, false
}

func TestADeclaredMemberStaysListedOnTheRegistriesTabWhenItsGroupIsRefused(t *testing.T) {
	m := tabWith(t, declaredConfig(), nil, nil)
	m = feed(t, m, RegistryGroupDetectedMsg{RegistryURL: registryHostInTest, Slug: "prod", Err: refusalOf(403, false), At: at(7)})

	row, ok := findRow(m, "dhi")
	if !ok {
		t.Fatalf("the declared member vanished from the Registries tab: %+v", m.registryTable.Items())
	}
	if row.url != registryHostInTest+"/dhi-proxy" {
		t.Errorf("declared member address = %q, want %q", row.url, registryHostInTest+"/dhi-proxy")
	}
}

func TestADeclaredMemberStaysCheckedInTheBrowserWhenItsGroupIsRefused(t *testing.T) {
	m := tabWith(t, declaredConfig(), nil, nil)
	m = feed(t, m, RegistryGroupDetectedMsg{RegistryURL: registryHostInTest, Slug: "prod", Err: refusalOf(403, false), At: at(7)})

	next, _ := m.openMultiRegistryBrowser()
	b := next.(Model).registryBrowser
	if b == nil {
		t.Fatal("the browser did not open")
	}
	var dhi, prod *browserRegistryEntry
	for i := range b.entries {
		switch b.entries[i].key {
		case "dhi":
			dhi = &b.entries[i]
		case "prod":
			prod = &b.entries[i]
		}
	}
	if dhi == nil {
		t.Fatalf("the declared member is not an entry of the browser: %+v", b.entries)
	}
	if !b.selected(*dhi) || !dhi.cachedOnly {
		t.Errorf("declared member: checked = %v, cachedOnly = %v, want both true", b.selected(*dhi), dhi.cachedOnly)
	}
	if dhi.URL != registryHostInTest || dhi.repoPrefix != "dhi-proxy" {
		t.Errorf("declared member = %q + %q, want its address kept", dhi.URL, dhi.repoPrefix)
	}
	if prod == nil || !b.selected(*prod) {
		t.Errorf("the refused group is not offered as itself, checked: %+v", prod)
	}
}
