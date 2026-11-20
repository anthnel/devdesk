package ociresources

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/imagesearch"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-637: TestTheSummaryNeverOverflowsTheHeader sweeps widths 50 to 220 with
// registries that are not Nexus entries, so the May omit images line (the longest
// key, 15 cells) never appeared in it. This sibling sweeps the same widths with
// Nexus entries: a group searched as one unit (its label ends with groupUnitSuffix)
// and a group of path-routed members, some proxies. Each scenario puts names on
// May omit images AND on another line, the case where the header holds the most.

const (
	widthGroupAlias = "nexus-production-group"
	widthPathSlug   = "ny"
	widthPathBase   = "nexus-two.example.com"
)

// mayOmitWidthModel opens the browser on the unit group "nx" (members on the
// group's own address: one verdict, under groupUnitLabel) and on the path-routed
// group "ny" with two proxies and a hosted repository.
func mayOmitWidthModel(t *testing.T) Model {
	t.Helper()
	unit := anonymousNexusGroup("nexus-one.example.com")
	unit.Alias = widthGroupAlias
	paths := nexusGroup()
	paths.Slug, paths.Alias, paths.URL, paths.AuthMode = widthPathSlug, widthPathSlug, widthPathBase, config.AuthAnonymous

	member := func(prefix, typ string) cache.RegistryGroupMember {
		return cache.RegistryGroupMember{Alias: prefix, URL: widthPathBase, RepoPrefix: prefix, Type: typ}
	}
	groups := map[string]cache.RegistryGroupEntry{
		"nx": {Members: []cache.RegistryGroupMember{
			{Alias: "docker-hosted", URL: unit.URL}, {Alias: "docker-proxy", URL: unit.URL},
		}},
		widthPathSlug: {Members: []cache.RegistryGroupMember{
			member("proxy-alpha-long-name", "proxy"), member("proxy-beta-long-name", "proxy"), member("hosted-gamma", "hosted"),
		}},
	}
	return modelWithGroups(t, []config.RegistryItem{unit, paths}, groups)
}

func TestTheMayOmitLineNeverOverflowsTheHeaderNorCutsAGroupSuffix(t *testing.T) {
	withImageSearcher(t, &fakeSearcher{})
	pathKey := func(prefix string) string { return memberKey(widthPathSlug, widthPathBase, prefix) }
	scenarios := []struct {
		name   string
		answer func(t *testing.T, m Model) Model
		// other is the line that shares the header with May omit images.
		other string
	}{
		{"group answered nothing, proxies failed", func(t *testing.T, m Model) Model {
			m = answerFor(t, m, "nx", nil, false, nil)
			m = answerFor(t, m, pathKey("proxy-alpha-long-name"), nil, false, errors.New("boom"))
			m = answerFor(t, m, pathKey("proxy-beta-long-name"), nil, false, errors.New("boom"))
			return answerFor(t, m, pathKey("hosted-gamma"), oneImage("nginx"), false, nil)
		}, "Failed"},
		{"group failed, proxies refused", func(t *testing.T, m Model) Model {
			m = answerFor(t, m, "nx", nil, false, errors.New("boom"))
			m = answerFor(t, m, pathKey("proxy-alpha-long-name"), nil, false, imagesearch.ErrUnsupported)
			m = answerFor(t, m, pathKey("proxy-beta-long-name"), nil, false, imagesearch.ErrUnsupported)
			return answerFor(t, m, pathKey("hosted-gamma"), nil, false, nil)
		}, "Refused"},
	}
	for _, sc := range scenarios {
		for w := 50; w <= 220; w += 10 {
			t.Run(fmt.Sprintf("%s/width %d", sc.name, w), func(t *testing.T) {
				// The browser is a pointer: each width builds its own model.
				m := searchLot(t, feed(t, mayOmitWidthModel(t), testutil.Resize(w, 40)))
				m = sc.answer(t, m)

				keys, vals := headerLines(m)
				if len(keys) > shortcut.HeaderRows {
					t.Errorf("%d lines, want at most %d", len(keys), shortcut.HeaderRows)
				}
				for _, want := range []string{mayOmitKey, sc.other} {
					if _, ok := vals[want]; !ok {
						t.Fatalf("keys = %v, want a %q line", keys, want)
					}
				}
				room := shortcut.HeaderValueWidth(w, keys)
				if room < 2 {
					return
				}
				for _, k := range keys {
					if got := ansi.StringWidth(vals[k]); got > room {
						t.Errorf("%s = %q is %d cells, room is %d", k, vals[k], got, room)
					}
				}
				// A name is kept whole or folded into +N: the group's suffix is never
				// cut, and neither is a no-result mark.
				for k, v := range vals {
					if strings.Contains(v, widthGroupAlias) && !strings.Contains(v, groupUnitLabel(widthGroupAlias)) {
						t.Errorf("%s = %q cuts the group's suffix %q", k, v, groupUnitSuffix)
					}
					if strings.Contains(v, "(no result") && !strings.Contains(v, noResultMark) {
						t.Errorf("%s = %q cuts the no-result mark", k, v)
					}
				}
			})
		}
	}
}
