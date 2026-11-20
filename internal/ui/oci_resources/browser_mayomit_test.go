package ociresources

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/imagesearch"
)

// DEVDESK-629 step 8 (F2, F4): the level 1 line `Cached only` is now `May omit
// images`. What it says no longer rests on "the list is limited to the cache" (a
// claim nobody measured on a group) but on what was measured: a Nexus group may
// leave out the names of a member it cannot reach right now, without a word (200,
// fewer names), and a proxy that failed or was refused has told nothing.
//
// The mention follows the ROLE of the entry in its Nexus group (group, proxy,
// hosted, unknown), read from the repository type discovery stored for the member.

// typedMember is a discovered member of a Nexus group with the repository type
// discovery read for it ("" = unknown, as in a cache written before the type).
type typedMember struct{ prefix, typ string }

// typedMembers are members of "nx" the way discovery stores them for a path-routed
// group (the host of the HTTP port plus one prefix each), with their type.
func typedMembers(base string, ms ...typedMember) map[string]cache.RegistryGroupEntry {
	var members []cache.RegistryGroupMember
	for _, m := range ms {
		members = append(members, cache.RegistryGroupMember{Alias: m.prefix, URL: base, RepoPrefix: m.prefix, Type: m.typ})
	}
	return map[string]cache.RegistryGroupEntry{"nx": {Members: members}}
}

// typedConnectorMembers are members of "nx" on a connector of its own: the group's
// URL and no prefix, whatever the member, with their type.
func typedConnectorMembers(base string, ms ...typedMember) map[string]cache.RegistryGroupEntry {
	var members []cache.RegistryGroupMember
	for _, m := range ms {
		members = append(members, cache.RegistryGroupMember{Alias: m.prefix, URL: base, Type: m.typ})
	}
	return map[string]cache.RegistryGroupEntry{"nx": {Members: members}}
}

// typedNexusMembersModel opens the browser on the anonymous group "nx" whose
// discovered members are the given typed prefixes at base.
func typedNexusMembersModel(t *testing.T, base string, ms ...typedMember) Model {
	t.Helper()
	return modelWithGroups(t, []config.RegistryItem{anonymousNexusGroup(base)}, typedMembers(base, ms...))
}

func TestMemberRole(t *testing.T) {
	tests := []struct {
		typ  string
		want nexusRole
	}{
		{"hosted", nexusRoleHosted},
		{"proxy", nexusRoleProxy},
		// A group nested in a group may omit what its own members cannot reach.
		{"group", nexusRoleGroup},
		{"", nexusRoleUnknown},
		{"weird", nexusRoleUnknown},
		{"Proxy", nexusRoleUnknown},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.typ), func(t *testing.T) {
			if got := memberRole(tt.typ); got != tt.want {
				t.Errorf("memberRole(%q) = %v, want %v", tt.typ, got, tt.want)
			}
		})
	}
}

// H4: the rule, role by verdict. A group is mentioned whatever it answered (an
// answer of a group says nothing about what it left out) except when the catalog
// was not walked at all; a proxy only when it failed or was refused; a hosted
// repository never (it holds what it holds); an unknown role never (a cache with
// no type, or a type that could not be read: no mention is better than a false one).
func TestMayOmit(t *testing.T) {
	outcomes := []struct {
		name string
		oc   imagesearch.Outcome
	}{
		{"ok", imagesearch.Outcome{Verdict: imagesearch.VerdictOK}},
		{"ok partial", imagesearch.Outcome{Verdict: imagesearch.VerdictOK, Partial: true}},
		{"no result", imagesearch.Outcome{Verdict: imagesearch.VerdictNoResult}},
		{"failed", imagesearch.Outcome{Verdict: imagesearch.VerdictFailed}},
		{"refused", imagesearch.Outcome{Verdict: imagesearch.VerdictRefused}},
		{"skipped", imagesearch.Outcome{Verdict: imagesearch.VerdictNoResult, Skipped: true}},
	}
	roles := []struct {
		name string
		role nexusRole
		want map[string]bool
	}{
		{"group", nexusRoleGroup, map[string]bool{"ok": true, "ok partial": true, "no result": true, "failed": true, "refused": true}},
		{"proxy", nexusRoleProxy, map[string]bool{"failed": true, "refused": true}},
		{"hosted", nexusRoleHosted, nil},
		{"unknown", nexusRoleUnknown, nil},
	}
	for _, r := range roles {
		for _, o := range outcomes {
			t.Run(r.name+"/"+o.name, func(t *testing.T) {
				if got, want := mayOmit(r.role, o.oc), r.want[o.name]; got != want {
					t.Errorf("mayOmit(%s, %s) = %v, want %v", r.name, o.name, got, want)
				}
			})
		}
	}
}

// The words of the line are gone from the production code: the key, the help text,
// the field, the predicate. A comment that still says "cached only" would keep the
// old reading alive, so the scan covers comments too. It names file and line.
func TestNoCachedOnlyWordingLeft(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	banned := []string{"Cached only", "cachedOnly", "CachedOnly", "cachedNoResultMark", "listsCacheOnly", "nothing in the cache"}
	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		scanned++
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			for _, w := range banned {
				if strings.Contains(line, w) {
					t.Errorf("%s:%d still says %q: %s", name, i+1, w, strings.TrimSpace(line))
				}
			}
		}
	}
	if scanned < 10 {
		t.Fatalf("scanned %d production files, the scan looks at the wrong directory", scanned)
	}
}

// The help says what the line means, in the words the user settled on, and no
// longer reads "(no result)" as "nothing in the cache".
func TestHelpExplainsMayOmitImages(t *testing.T) {
	const sentence = `May omit images (a Nexus proxy or group may not list images it cannot reach right now; "(no result)" does not mean the image is absent)`
	c := lvl1ImagesModel(t).GetHelpContent()

	var all strings.Builder
	header := ""
	for _, kb := range c.KeyBindings {
		all.WriteString(kb.Key + " " + kb.Description + "\n")
		if kb.Key == "Header (Browser images)" {
			header = kb.Description
		}
	}
	for _, s := range c.Sections {
		all.WriteString(s.Title + " " + s.Body + "\n")
	}
	if !strings.Contains(header, sentence) {
		t.Errorf("the Header (Browser images) help lacks %q:\n%s", sentence, header)
	}
	for _, gone := range []string{"Cached only", "nothing in the cache", "cached content"} {
		if strings.Contains(all.String(), gone) {
			t.Errorf("the help still says %q", gone)
		}
	}
}
