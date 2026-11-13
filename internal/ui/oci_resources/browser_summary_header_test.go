package ociresources

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/imagesearch"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-305 (IS-13), step 4: the level 1 header keeps each registry's verdict
// visible (criteria C1 to C7), read through GetHeaderInfo on a real browser.

// headerLines returns the header's keys in order and its values by key.
func headerLines(m Model) ([]string, map[string]string) {
	var keys []string
	vals := map[string]string{}
	for _, info := range m.GetHeaderInfo("") {
		keys = append(keys, info.Key)
		vals[info.Key] = info.Value
	}
	return keys, vals
}

// fourRegistryModel opens the browser on hub, dhi.io, nexus-prod and dev, in that
// form order, at a terminal width wide.
func fourRegistryModel(t *testing.T, width int) Model {
	t.Helper()
	m := registryModel(t,
		anonymousRegistry("docker.io", "hub"),
		anonymousRegistry("dhi.io", "dhi.io"),
		anonymousRegistry("nexus.example.com", "nexus-prod"),
		anonymousRegistry("dev.example.com", "dev"),
	)
	return feed(t, m, testutil.Resize(width, 40))
}

func wantHeaderKeys(t *testing.T, m Model, want ...string) map[string]string {
	t.Helper()
	keys, vals := headerLines(m)
	if !slices.Equal(keys, want) {
		t.Fatalf("header keys = %v, want %v (values %v)", keys, want, vals)
	}
	return vals
}

// C1: a finished search names, per class, the registries that failed or refused.
func TestAFinishedSearchListsEachProblemByClass(t *testing.T) {
	withImageSearcher(t, &fakeSearcher{})
	m := searchLot(t, fourRegistryModel(t, 200))
	m = answerFor(t, m, "hub", oneImage("nginx"), false, nil)
	m = answerFor(t, m, "dhi.io", nil, false, imagesearch.ErrUnsupported)
	m = answerFor(t, m, "nexus-prod", nil, false, imagesearch.ErrUnsupported)
	m = answerFor(t, m, "dev", nil, false, errors.New("boom"))

	vals := wantHeaderKeys(t, m, "Images", "Failed", "Refused")
	if vals["Images"] != "1" {
		t.Errorf("Images = %q, want 1", vals["Images"])
	}
	if vals["Failed"] != "dev" {
		t.Errorf("Failed = %q, want dev", vals["Failed"])
	}
	if vals["Refused"] != "dhi.io, nexus-prod" {
		t.Errorf("Refused = %q, want %q (form order)", vals["Refused"], "dhi.io, nexus-prod")
	}

	// A filter changes the image count, never the summary.
	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("zzz")...)
	m = feed(t, m, testutil.Key("enter"))
	vals = wantHeaderKeys(t, m, "Images", "Failed", "Refused")
	if vals["Images"] != "0" || vals["Failed"] != "dev" || vals["Refused"] != "dhi.io, nexus-prod" {
		t.Errorf("header after a filter = %v, want Images 0 and the same summary", vals)
	}
}

// C2: while answers are missing, the header counts them.
func TestASearchInFlightCountsThePendingRegistries(t *testing.T) {
	withImageSearcher(t, &fakeSearcher{})
	m := searchLot(t, fourRegistryModel(t, 200))
	if got := wantHeaderKeys(t, m, "Images", "Pending")["Pending"]; got != "4" {
		t.Errorf("Pending after submit = %q, want 4", got)
	}
	m = answerFor(t, m, "hub", oneImage("nginx"), false, nil)
	if got := wantHeaderKeys(t, m, "Images", "Pending")["Pending"]; got != "3" {
		t.Errorf("Pending after one answer = %q, want 3", got)
	}
	m = answerFor(t, m, "dhi.io", oneImage("nginx"), false, nil)
	m = answerFor(t, m, "nexus-prod", oneImage("nginx"), false, nil)
	m = answerFor(t, m, "dev", oneImage("nginx"), false, nil)
	wantHeaderKeys(t, m, "Images")
}

// C3: a registry cut at a cap is named, whether it answered or found nothing.
func TestAPartialRegistryIsNamed(t *testing.T) {
	tests := []struct {
		name   string
		images []imagesearch.Image
		want   []string
		vals   map[string]string
	}{
		{"ok and partial", oneImage("nginx"), []string{"Images", "Partial"}, map[string]string{"Partial": "hub"}},
		{"no result and partial", nil, []string{"Images", "Partial", "No result"}, // V4: Partial before No result
			map[string]string{"Partial": "hub", "No result": "hub"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withImageSearcher(t, &fakeSearcher{})
			m := registryModel(t, anonymousRegistry("docker.io", "hub"))
			m = feed(t, m, testutil.Resize(200, 40))
			m = answerFor(t, searchLot(t, m), "hub", tt.images, true, nil)
			keys, vals := headerLines(m)
			if !slices.Equal(keys, tt.want) {
				t.Fatalf("header keys = %v, want %v", keys, tt.want)
			}
			for k, v := range tt.vals {
				if vals[k] != v {
					t.Errorf("%s = %q, want %q", k, vals[k], v)
				}
			}
		})
	}
}

// C4: on a narrow header a list ends with +N, never silently cut.
func TestANarrowHeaderAbbreviatesWithACount(t *testing.T) {
	withImageSearcher(t, &fakeSearcher{})
	m := searchLot(t, fourRegistryModel(t, 80))
	m = answerFor(t, m, "hub", oneImage("nginx"), false, nil)
	m = answerFor(t, m, "dhi.io", nil, false, imagesearch.ErrUnsupported)
	m = answerFor(t, m, "nexus-prod", nil, false, imagesearch.ErrUnsupported)
	m = answerFor(t, m, "dev", oneImage("nginx"), false, nil)
	vals := wantHeaderKeys(t, m, "Images", "Refused")
	if vals["Refused"] != "dhi.io, +1" {
		t.Errorf("Refused at 80 columns = %q, want %q", vals["Refused"], "dhi.io, +1")
	}
}

// Every class at once, long names: never more than HeaderRows lines, never a
// value wider than the room the header gives it.
func TestTheSummaryNeverOverflowsTheHeader(t *testing.T) {
	withImageSearcher(t, &fakeSearcher{})
	names := []string{"registry-one", "registry-two", "registry-three", "registry-four",
		"registry-five", "registry-six", "registry-seven"}
	for w := 50; w <= 220; w += 10 {
		t.Run(fmt.Sprintf("width %d", w), func(t *testing.T) {
			// The browser is a pointer: each width builds its own model.
			m0 := registryModel(t,
				anonymousRegistry("one.example.com", names[0]),
				anonymousRegistry("two.example.com", names[1]),
				anonymousRegistry("three.example.com", names[2]),
				anonymousRegistry("four.example.com", names[3]),
				anonymousRegistry("five.example.com", names[4]),
				anonymousRegistry("six.example.com", names[5]),
				anonymousRegistry("seven.example.com", names[6]),
			)
			m := searchLot(t, feed(t, m0, testutil.Resize(w, 40)))
			m = answerFor(t, m, names[0], oneImage("nginx"), false, nil)
			m = answerFor(t, m, names[1], nil, false, errors.New("boom"))
			m = answerFor(t, m, names[2], nil, false, imagesearch.ErrUnsupported)
			m = answerFor(t, m, names[3], oneImage("nginx"), true, nil)
			m = answerFor(t, m, names[4], nil, false, nil)
			m.registryBrowser.outcomes[names[5]] = imagesearch.Outcome{Verdict: imagesearch.VerdictNoResult, Skipped: true}
			// names[6] stays pending.

			keys, vals := headerLines(m)
			wantKeys := []string{"Images", "Pending", "Failed", "Refused", "Partial", "No result", "Skipped"}
			if !slices.Equal(keys, wantKeys) {
				t.Fatalf("keys = %v, want %v", keys, wantKeys)
			}
			if len(keys) > shortcut.HeaderRows {
				t.Errorf("%d lines, want at most %d", len(keys), shortcut.HeaderRows)
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
		})
	}
}

// C5: nothing to report, only the count.
func TestAllOKShowsOnlyTheImageCount(t *testing.T) {
	withImageSearcher(t, &fakeSearcher{})
	m := searchLot(t, fourRegistryModel(t, 200))
	for _, k := range []string{"hub", "dhi.io", "nexus-prod", "dev"} {
		m = answerFor(t, m, k, oneImage("nginx"), false, nil)
	}
	wantHeaderKeys(t, m, "Images")
}

// C6: level 2 keeps its usual tag count; back at level 1 the summary returns.
func TestLevelTwoShowsTagsAndTheSummaryComesBack(t *testing.T) {
	withImageSearcher(t, &fakeSearcher{})
	withTagRecorder(t)
	m := registryModel(t,
		anonymousRegistry("docker.io", "hub"),
		anonymousRegistry("prod.example.com", "prod"),
	)
	m = searchLot(t, feed(t, m, testutil.Resize(200, 40)))
	m = answerFor(t, m, "hub", oneImage("nginx"), false, nil)
	m = answerFor(t, m, "prod", nil, false, imagesearch.ErrUnsupported)
	before := wantHeaderKeys(t, m, "Images", "Refused")

	m = selectLevelOneRow(t, m, "hub", "library/nginx")
	m, _ = step(t, m, testutil.Key("right"))
	if m.registryBrowser.state != browserStateTags {
		t.Fatalf("state = %d after right, want the tags level", m.registryBrowser.state)
	}
	wantHeaderKeys(t, m, "Tags")

	m, _ = step(t, m, testutil.Key("left"))
	after := wantHeaderKeys(t, m, "Images", "Refused")
	if after["Refused"] != before["Refused"] || after["Images"] != before["Images"] {
		t.Errorf("header after coming back = %v, want %v", after, before)
	}
}

// C7: a one-character text skips the catalog; it is Skipped, not a failure.
func TestAOneCharacterQueryShowsSkippedNotRefused(t *testing.T) {
	hub, hubRequests := fakeHub(t)
	priv, _ := catalogServer(t, "api", "alpha")
	withImageSearcher(t, wiredLikeProduction(hub))

	m := registryModel(t,
		anonymousRegistry("docker.io", "hub"),
		anonymousRegistry(priv.URL, "private"))
	m = feed(t, m, testutil.Resize(200, 40))
	m, cmd := submitRaw(t, m, "a")
	if cmd == nil || m.registryBrowser.state != browserStateImages {
		t.Fatalf("the search did not start (state %d)", m.registryBrowser.state)
	}
	results := runCmds(t, cmd)
	for range 2 {
		m = feed(t, m, recvResult(t, results))
	}

	keys, vals := headerLines(m)
	for _, k := range []string{"Failed", "Refused", "No result"} {
		if slices.Contains(keys, k) {
			t.Errorf("header has a %q line (%v): Skipped is none of those", k, vals)
		}
	}
	if want := "private (type 2+ characters)"; vals["Skipped"] != want {
		t.Errorf("Skipped = %q, want %q (keys %v)", vals["Skipped"], want, keys)
	}
	if n := hubRequests.Load(); n != 1 {
		t.Errorf("the fake Hub got %d requests, want 1", n)
	}
}

// Opening the tags mid search and coming back leaves the header naming the
// registries that had not answered.
func TestTheHeaderNamesTheInterruptedRegistries(t *testing.T) {
	withImageSearcher(t, &fakeSearcher{})
	m := searchLot(t, fourRegistryModel(t, 200))
	m = answerFor(t, m, "hub", oneImage("nginx"), false, nil)
	m = selectLevelOneRow(t, m, "hub", "library/nginx")
	m, _ = step(t, m, testutil.Key("right"))
	m, _ = step(t, m, testutil.Key("left"))
	vals := wantHeaderKeys(t, m, "Images", "Interrupted")
	if want := "dhi.io, nexus-prod, dev"; vals["Interrupted"] != want {
		t.Errorf("Interrupted = %q, want %q", vals["Interrupted"], want)
	}
}
