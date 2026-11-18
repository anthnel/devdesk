package ociresources

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/registrymgr"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-510 (NX-07), step 3: a group whose members are refused is not an empty
// group. The Cmd writes the refusal beside the listing and never over it (D23);
// only the handler, in Update, changes the model (Rule 110); an unreachable
// manager writes nothing (CA3); the refusal survives a restart and an old cache
// file gives "never", not "0" (CA6). Wording (footer, cell, status line) is
// step 4 and is not tested here.

const groupSlug = "grp"

// isolateGroupHome points HOME at a directory of the test's own: the group cache
// lives under it, and the package-wide HOME is shared by every test (DEVDESK-36).
func isolateGroupHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func groupCacheFile(home string) string {
	return filepath.Join(home, ".devdesk", "cache", "registry-groups.json")
}

// nexusGroupServer starts a Nexus that says "group" on the generic endpoint with
// no member listed there, so the format endpoint (path holding "/group/") decides.
func nexusGroupServer(t *testing.T, format http.HandlerFunc) config.RegistryItem {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/group/") {
			_, _ = w.Write([]byte(`{"type":"group","format":"docker","attributes":{"group":{"memberNames":[]}}}`))
			return
		}
		format(w, r)
	}))
	t.Cleanup(srv.Close)
	return config.RegistryItem{
		Slug:     groupSlug,
		Kind:     config.KindGroup,
		Provider: config.ProviderNexus,
		URL:      srv.URL + "/repository/docker-group",
		AuthMode: config.AuthAnonymous,
	}
}

func replyStatus(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }
}

func replyMembers(names string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"group":{"memberNames":%s}}`, names)
	}
}

// knownMembers is what the cache held before the discovery under test.
func knownMembers() cache.RegistryGroupEntry {
	return cache.RegistryGroupEntry{
		Members: []cache.RegistryGroupMember{
			{Alias: "hosted", URL: "https://n/repository/hosted", RepoPrefix: "hosted"},
			{Alias: "dhi", URL: "https://n/repository/dhi-proxy", RepoPrefix: "dhi-proxy"},
		},
		DiscoveredAt: at(3),
	}
}

func openGroupCache(t *testing.T) *cache.RegistryGroupCache {
	t.Helper()
	c, err := cache.NewRegistryGroupCache()
	if err != nil {
		t.Fatalf("opening the group cache: %v", err)
	}
	return c
}

// seedGroup writes what was last known for the slug: a listing, and when
// refusal is non-nil a refusal beside it.
func seedGroup(t *testing.T, listing *cache.RegistryGroupEntry, refusal *cache.RegistryGroupRefusal) {
	t.Helper()
	c := openGroupCache(t)
	if listing != nil {
		if err := c.Set(groupSlug, *listing); err != nil {
			t.Fatalf("seeding the listing: %v", err)
		}
	}
	if refusal != nil {
		if err := c.SetRefusal(groupSlug, *refusal); err != nil {
			t.Fatalf("seeding the refusal: %v", err)
		}
	}
}

func runDetect(t *testing.T, reg config.RegistryItem) RegistryGroupDetectedMsg {
	t.Helper()
	msg, ok := run(t, detectRegistryGroupCmd(reg, "")).(RegistryGroupDetectedMsg)
	if !ok {
		t.Fatal("the Cmd did not return a RegistryGroupDetectedMsg")
	}
	return msg
}

// ── The Cmd: what it writes ─────────────────────────────────────────────────

// CA1 and D23: a refused listing is recorded, and what was last known stays.
func TestARefusedListingIsRecordedAndKeepsTheMembersLastKnown(t *testing.T) {
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			isolateGroupHome(t)
			known := knownMembers()
			seedGroup(t, &known, nil)
			reg := nexusGroupServer(t, replyStatus(code))

			msg := runDetect(t, reg)

			if !errors.Is(msg.Err, registrymgr.ErrMembersRefused) {
				t.Fatalf("msg.Err = %v, want a members refusal", msg.Err)
			}
			var refused *registrymgr.MembersRefusedError
			if !errors.As(msg.Err, &refused) || refused.Status != code {
				t.Fatalf("msg.Err = %v, want *MembersRefusedError with status %d", msg.Err, code)
			}
			if msg.At.IsZero() {
				t.Error("msg.At is zero: the handler cannot hold the instant the disk holds")
			}

			c := openGroupCache(t)
			entry := c.Get(groupSlug)
			if entry == nil || len(entry.Members) != 2 {
				t.Fatalf("listing on disk = %+v, want the 2 members last known (D23)", entry)
			}
			if !entry.DiscoveredAt.Equal(known.DiscoveredAt) {
				t.Errorf("DiscoveredAt = %v, want %v untouched: a refusal is not a listing",
					entry.DiscoveredAt, known.DiscoveredAt)
			}
			r := c.Refusal(groupSlug)
			if r == nil {
				t.Fatal("the refusal was not written to the cache")
			}
			if r.Status != code || !r.Anonymous {
				t.Errorf("refusal on disk = %+v, want status %d, anonymous", *r, code)
			}
			if !r.At.Equal(msg.At) {
				t.Errorf("refusal At = %v, msg.At = %v: disk and memory must hold the same instant", r.At, msg.At)
			}
		})
	}
}

// A group never listed and then refused has a refusal and still no listing:
// "an entry is present" keeps meaning "a settled answer".
func TestARefusedGroupNeverListedGetsNoListing(t *testing.T) {
	isolateGroupHome(t)
	reg := nexusGroupServer(t, replyStatus(http.StatusForbidden))

	msg := runDetect(t, reg)

	if !errors.Is(msg.Err, registrymgr.ErrMembersRefused) {
		t.Fatalf("msg.Err = %v, want a members refusal", msg.Err)
	}
	c := openGroupCache(t)
	if entry := c.Get(groupSlug); entry != nil {
		t.Errorf("listing on disk = %+v, want none: a refusal must not read as \"0 members\"", entry)
	}
	if c.Refusal(groupSlug) == nil {
		t.Error("the refusal was not written to the cache")
	}
}

// CA2: a settled empty answer is written, and it supersedes a refusal.
func TestAnEmptyListingIsWrittenAndClearsTheRefusal(t *testing.T) {
	isolateGroupHome(t)
	known := knownMembers()
	seedGroup(t, &known, &cache.RegistryGroupRefusal{At: at(4), Status: http.StatusForbidden})
	reg := nexusGroupServer(t, replyMembers("[]"))

	msg := runDetect(t, reg)

	if msg.Err != nil {
		t.Fatalf("msg.Err = %v, want nil: an empty group is a settled answer", msg.Err)
	}
	if msg.Members == nil || len(msg.Members) != 0 {
		t.Errorf("msg.Members = %#v, want a non-nil empty list", msg.Members)
	}
	c := openGroupCache(t)
	entry := c.Get(groupSlug)
	if entry == nil {
		t.Fatal("the empty listing was not written")
	}
	if len(entry.Members) != 0 {
		t.Errorf("Members on disk = %+v, want none", entry.Members)
	}
	if entry.DiscoveredAt.IsZero() || !entry.DiscoveredAt.Equal(msg.At) {
		t.Errorf("DiscoveredAt = %v, want msg.At = %v", entry.DiscoveredAt, msg.At)
	}
	if r := c.Refusal(groupSlug); r != nil {
		t.Errorf("refusal on disk = %+v, want it cleared by a settled listing", *r)
	}
}

func TestANonEmptyListingClearsTheRefusal(t *testing.T) {
	isolateGroupHome(t)
	seedGroup(t, nil, &cache.RegistryGroupRefusal{At: at(4), Status: http.StatusForbidden})
	reg := nexusGroupServer(t, replyMembers(`["docker-hosted","dhi-proxy"]`))

	msg := runDetect(t, reg)

	if msg.Err != nil || len(msg.Members) != 2 {
		t.Fatalf("msg = %+v, want 2 members and no error", msg)
	}
	c := openGroupCache(t)
	if entry := c.Get(groupSlug); entry == nil || len(entry.Members) != 2 {
		t.Errorf("listing on disk = %+v, want 2 members", entry)
	}
	if r := c.Refusal(groupSlug); r != nil {
		t.Errorf("refusal on disk = %+v, want it cleared", *r)
	}
}

// CA3: not being able to ask is neither a listing nor a refusal. Nothing is
// written: not the listing, not a refusal, and an earlier refusal stays as is.
func TestAnUnreachableManagerWritesNothing(t *testing.T) {
	shorten := func(t *testing.T) {
		old := groupDetectTimeout
		groupDetectTimeout = 300 * time.Millisecond
		t.Cleanup(func() { groupDetectTimeout = old })
	}
	cases := []struct {
		name   string
		format func(t *testing.T) http.HandlerFunc
	}{
		{"server error", func(*testing.T) http.HandlerFunc { return replyStatus(http.StatusInternalServerError) }},
		{"bad gateway", func(*testing.T) http.HandlerFunc { return replyStatus(http.StatusBadGateway) }},
		{"not found", func(*testing.T) http.HandlerFunc { return replyStatus(http.StatusNotFound) }},
		{"unreadable reply", func(*testing.T) http.HandlerFunc {
			return func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>")) }
		}},
		{"connection closed", func(*testing.T) http.HandlerFunc {
			return func(w http.ResponseWriter, _ *http.Request) {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err == nil {
					_ = conn.Close()
				}
			}
		}},
		{"deadline", func(t *testing.T) http.HandlerFunc {
			shorten(t)
			return func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/with what was last known", func(t *testing.T) {
			home := isolateGroupHome(t)
			known := knownMembers()
			seedGroup(t, &known, &cache.RegistryGroupRefusal{At: at(4), Status: http.StatusForbidden})
			before, err := os.ReadFile(groupCacheFile(home))
			if err != nil {
				t.Fatalf("reading the seeded cache: %v", err)
			}
			reg := nexusGroupServer(t, tc.format(t))

			msg := runDetect(t, reg)

			if msg.Err == nil {
				t.Fatal("msg.Err = nil: an unreachable manager is a failed discovery")
			}
			if errors.Is(msg.Err, registrymgr.ErrMembersRefused) {
				t.Errorf("msg.Err = %v: only a 401 or 403 is a refusal, this is a transient failure", msg.Err)
			}
			after, err := os.ReadFile(groupCacheFile(home))
			if err != nil {
				t.Fatalf("reading the cache after: %v", err)
			}
			if !bytes.Equal(before, after) {
				t.Errorf("the cache file changed on a failed discovery:\nbefore: %s\nafter:  %s", before, after)
			}
		})
		t.Run(tc.name+"/never asked before", func(t *testing.T) {
			home := isolateGroupHome(t)
			reg := nexusGroupServer(t, tc.format(t))

			msg := runDetect(t, reg)

			if msg.Err == nil || errors.Is(msg.Err, registrymgr.ErrMembersRefused) {
				t.Fatalf("msg.Err = %v, want a transient failure that is not a refusal", msg.Err)
			}
			if data, err := os.ReadFile(groupCacheFile(home)); err == nil {
				// Opening the cache may create it; it must hold neither a listing nor a refusal.
				c := openGroupCache(t)
				if c.Get(groupSlug) != nil || c.Refusal(groupSlug) != nil {
					t.Errorf("the cache holds an entry after a failed discovery: %s", data)
				}
			}
		})
	}
}

// The one conversion the Cmd (disk) and the handler (memory) share.
func TestRefusalRecordCarriesStatusAnonymityAndInstant(t *testing.T) {
	when := at(9)
	for _, anonymous := range []bool{false, true} {
		got := refusalRecord(&registrymgr.MembersRefusedError{Group: "g", Status: 403, Anonymous: anonymous}, when)
		want := cache.RegistryGroupRefusal{At: when, Status: 403, Anonymous: anonymous}
		if got != want {
			t.Errorf("anonymous=%v: refusalRecord = %+v, want %+v", anonymous, got, want)
		}
	}
}

// ── The handler: the only place the model changes (Rule 110) ────────────────

func refusalOf(status int, anonymous bool) *registrymgr.MembersRefusedError {
	return &registrymgr.MembersRefusedError{Group: "docker-group", Status: status, Anonymous: anonymous}
}

// groupModel is the Registries tab holding the fixture discovery for "prod", and
// a discovery under way for it.
func groupModel(t *testing.T) Model {
	t.Helper()
	m := feed(t, registriesTab(t), RegistryGroupCacheLoadedMsg{Entries: groupCacheFixture()})
	m = feed(t, m, testutil.Key("ctrl+r"))
	if !m.refreshingGroups["prod"] {
		t.Fatal("ctrl+r did not start a discovery for prod")
	}
	return m
}

func TestTheHandlerRecordsARefusalAndLeavesTheKnownMembersAlone(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want cache.RegistryGroupRefusal
	}{
		{"account", refusalOf(403, false), cache.RegistryGroupRefusal{At: at(7), Status: 403}},
		{"anonymous", refusalOf(401, true), cache.RegistryGroupRefusal{At: at(7), Status: 401, Anonymous: true}},
		{"wrapped", fmt.Errorf("discovery: %w", refusalOf(403, false)), cache.RegistryGroupRefusal{At: at(7), Status: 403}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := groupModel(t)

			m = feed(t, m, RegistryGroupDetectedMsg{
				RegistryURL: "registry.example.com", Slug: "prod", Err: tc.err, At: at(7),
			})

			if got, ok := m.groupRefusals["prod"]; !ok || got != tc.want {
				t.Errorf("groupRefusals[prod] = %+v (present %v), want %+v with the instant of msg.At", got, ok, tc.want)
			}
			entry := m.groupCache["prod"]
			if len(entry.Members) != 2 || !entry.DiscoveredAt.Equal(at(3)) {
				t.Errorf("groupCache[prod] = %+v, want the 2 members and the date last known", entry)
			}
			if m.refreshingGroups["prod"] {
				t.Error("the discovery is still shown as running after the refusal")
			}
			if m.footer.Level() == sharedcomponents.LevelError {
				t.Errorf("footer is at level Error (%q): a refusal is a warning, the failure case is the red one", m.footer.Text())
			}
		})
	}
}

func TestTheHandlerRecordsNothingForAGroupWithoutASlug(t *testing.T) {
	m := feed(t, registriesTab(t), RegistryGroupDetectedMsg{Err: refusalOf(403, false), At: at(7)})
	if len(m.groupRefusals) != 0 {
		t.Errorf("groupRefusals = %+v, want none: the cache and the table key on the slug", m.groupRefusals)
	}
}

func TestASettledListingClearsTheRefusalInMemory(t *testing.T) {
	for _, tc := range []struct {
		name    string
		members []registrymgr.GroupMember
		want    int
	}{
		{"with members", []registrymgr.GroupMember{{Alias: "dhi", URL: "registry.example.com", RepoPrefix: "dhi-proxy"}}, 1},
		{"empty", []registrymgr.GroupMember{}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := groupModel(t)
			m = feed(t, m, RegistryGroupDetectedMsg{
				RegistryURL: "registry.example.com", Slug: "prod", Err: refusalOf(403, false), At: at(7),
			})
			m = feed(t, m, testutil.Key("ctrl+r"))

			m = feed(t, m, RegistryGroupDetectedMsg{
				RegistryURL: "registry.example.com", Slug: "prod", Members: tc.members, At: at(8),
			})

			if r, ok := m.groupRefusals["prod"]; ok {
				t.Errorf("groupRefusals[prod] = %+v, want it cleared by a settled listing", r)
			}
			entry, ok := m.groupCache["prod"]
			if !ok || len(entry.Members) != tc.want {
				t.Fatalf("groupCache[prod] = %+v (present %v), want %d members", entry, ok, tc.want)
			}
			if !entry.DiscoveredAt.Equal(at(8)) {
				t.Errorf("DiscoveredAt = %v, want msg.At = %v: disk and memory hold the same instant", entry.DiscoveredAt, at(8))
			}
		})
	}
}

// CA3 on the model side: an unreachable manager changes nothing, and says so in red.
func TestAnUnreachableManagerChangesNeitherTheListingNorTheRefusal(t *testing.T) {
	m := groupModel(t)
	m = feed(t, m, RegistryGroupDetectedMsg{
		RegistryURL: "registry.example.com", Slug: "prod", Err: refusalOf(403, false), At: at(7),
	})
	m = feed(t, m, testutil.Key("ctrl+r"))
	refusal := m.groupRefusals["prod"]

	m = feed(t, m, RegistryGroupDetectedMsg{
		RegistryURL: "registry.example.com", Slug: "prod", Err: errUnreachable{}, At: at(8),
	})

	if got := m.groupRefusals["prod"]; got != refusal || got.Status != 403 {
		t.Errorf("groupRefusals[prod] = %+v, want the earlier refusal %+v kept (the refusal is still the last known fact)", got, refusal)
	}
	if entry := m.groupCache["prod"]; len(entry.Members) != 2 || !entry.DiscoveredAt.Equal(at(3)) {
		t.Errorf("groupCache[prod] = %+v, want what was last known", entry)
	}
	if m.refreshingGroups["prod"] {
		t.Error("the discovery is still shown as running after the failure")
	}
	if m.footer.Level() != sharedcomponents.LevelError {
		t.Errorf("footer level = %v, want Error for an unreachable manager (%q)", m.footer.Level(), m.footer.Text())
	}
}

// ── Loading the cache ───────────────────────────────────────────────────────

func TestLoadedRefusalsAreCopiedIntoTheModel(t *testing.T) {
	want := cache.RegistryGroupRefusal{At: at(5), Status: 403, Anonymous: true}
	m := feed(t, registriesTab(t), RegistryGroupCacheLoadedMsg{
		Entries:  groupCacheFixture(),
		Refusals: map[string]cache.RegistryGroupRefusal{"prod": want},
	})
	if got := m.groupRefusals["prod"]; got != want {
		t.Errorf("groupRefusals[prod] = %+v, want %+v", got, want)
	}
	if len(m.groupCache["prod"].Members) != 2 {
		t.Errorf("groupCache[prod] = %+v, want the loaded listing alongside", m.groupCache["prod"])
	}
}

// A load that carries no refusals (nil) leaves the model's alone, as it does for Entries.
func TestALoadWithoutRefusalsKeepsTheOnesInMemory(t *testing.T) {
	m := feed(t, registriesTab(t), RegistryGroupCacheLoadedMsg{
		Refusals: map[string]cache.RegistryGroupRefusal{"prod": {At: at(5), Status: 403}},
	})
	m = feed(t, m, RegistryGroupCacheLoadedMsg{Entries: groupCacheFixture()})
	if _, ok := m.groupRefusals["prod"]; !ok {
		t.Error("a load without refusals erased the one held in memory")
	}
}

// CA1, persistence: after a restart the refusal is read back from disk.
func TestARefusalOnDiskSurvivesARestart(t *testing.T) {
	isolateGroupHome(t)
	known := knownMembers()
	want := cache.RegistryGroupRefusal{At: at(5), Status: 403, Anonymous: true}
	seedGroup(t, &known, &want)

	msg, ok := run(t, loadRegistryGroupCache()).(RegistryGroupCacheLoadedMsg)
	if !ok {
		t.Fatal("loadRegistryGroupCache did not return a RegistryGroupCacheLoadedMsg")
	}
	if got := msg.Refusals[groupSlug]; got != want {
		t.Fatalf("msg.Refusals[%s] = %+v, want %+v", groupSlug, got, want)
	}
	if len(msg.Entries[groupSlug].Members) != 2 {
		t.Errorf("msg.Entries[%s] = %+v, want the listing last known as well", groupSlug, msg.Entries[groupSlug])
	}

	m := feed(t, registriesTab(t), msg)
	if got := m.groupRefusals[groupSlug]; got != want {
		t.Errorf("groupRefusals[%s] = %+v, want %+v after the load", groupSlug, got, want)
	}
}

// CA6: an old cache file holds empty entries a refusal may have written. Read
// through the UI's load, they come out unknown ("never"), not as "0 members"; an
// entry with members is kept.
func TestALegacyCacheFileGivesNeverForItsEmptyEntries(t *testing.T) {
	home := isolateGroupHome(t)
	legacy := `{
  "prod":  {"members": [], "discovered_at": "2026-08-01T10:00:00Z"},
  "other": {"members": [{"alias":"a","url":"u","repo_prefix":"a"}], "discovered_at": "2026-08-01T10:00:00Z"}
}`
	if err := os.MkdirAll(filepath.Dir(groupCacheFile(home)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(groupCacheFile(home), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	msg, ok := run(t, loadRegistryGroupCache()).(RegistryGroupCacheLoadedMsg)
	if !ok {
		t.Fatal("loadRegistryGroupCache did not return a RegistryGroupCacheLoadedMsg")
	}
	if _, present := msg.Entries["prod"]; present {
		t.Errorf("msg.Entries[prod] = %+v, want the empty legacy entry set aside", msg.Entries["prod"])
	}
	if len(msg.Entries["other"].Members) != 1 {
		t.Errorf("msg.Entries[other] = %+v, want the non-empty legacy entry kept", msg.Entries["other"])
	}
	if len(msg.Refusals) != 0 {
		t.Errorf("msg.Refusals = %+v, want none from a legacy file", msg.Refusals)
	}

	m := feed(t, registriesTab(t), msg)
	if got := m.registryTable.Table().Rows()[0][membersCol]; got != "never" {
		t.Errorf("Members cell for prod = %q, want %q (unknown, not 0 members)", got, "never")
	}
}
