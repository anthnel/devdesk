package ociresources

import (
	"fmt"
	"math"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/imagesearch"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// noWidthLimit tells fitNames the header width is not known yet (before the first
// WindowSizeMsg): nothing is abbreviated.
const noWidthLimit = math.MaxInt

// skippedHint explains a Skipped line; it is the first thing fitNames drops.
var skippedHint = fmt.Sprintf(" (type %d+ characters)", imagesearch.CatalogMinQueryRunes)

// fitNames joins names with ", " and appends suffix, within width cells
// (ansi.StringWidth). It tries, in order, and returns the first that fits:
//  1. every name + suffix;
//  2. the first k names + ", +N" + suffix, k from len-1 down to 1;
//  3. "+N" + suffix (N = len(names));
//  4. steps 1 to 3 again without the suffix (only when suffix != "").
//
// When nothing fits it returns "+N" (wider than width: the header clips it).
// Empty names return "".
func fitNames(names []string, suffix string, width int) string {
	if len(names) == 0 {
		return ""
	}
	fits := func(s string) bool { return ansi.StringWidth(s) <= width }
	try := func(suffix string) (string, bool) {
		if all := strings.Join(names, ", ") + suffix; fits(all) {
			return all, true
		}
		for k := len(names) - 1; k >= 1; k-- {
			s := fmt.Sprintf("%s, +%d%s", strings.Join(names[:k], ", "), len(names)-k, suffix)
			if fits(s) {
				return s, true
			}
		}
		if s := fmt.Sprintf("+%d%s", len(names), suffix); fits(s) {
			return s, true
		}
		return "", false
	}
	if s, ok := try(suffix); ok {
		return s
	}
	if suffix != "" {
		if s, ok := try(""); ok {
			return s
		}
	}
	return fmt.Sprintf("+%d", len(names))
}

// searchSummary is what the level 1 header says about the lot, names in form order.
type searchSummary struct {
	Pending     int      // in the live lot, no answer yet
	Interrupted []string // the lot was abandoned (→) before they answered
	Failed      []string
	Refused     []string
	Partial     []string // ok or no result, cut at a cap
	NoResult    []string // answered, nothing matches (Skipped excluded)
	Skipped     []string // catalog not walked: query too short
}

// summarizeSearch sorts the lot's keys into classes. Pure; names are label(key),
// in the order of keys.
func summarizeSearch(keys []string, outcomes map[string]imagesearch.Outcome, live bool,
	label func(key string) string) searchSummary {
	var s searchSummary
	for _, k := range keys {
		oc, ok := outcomes[k]
		if !ok {
			if live {
				s.Pending++
			} else {
				s.Interrupted = append(s.Interrupted, label(k))
			}
			continue
		}
		switch oc.Verdict {
		case imagesearch.VerdictFailed:
			s.Failed = append(s.Failed, label(k))
		case imagesearch.VerdictRefused:
			s.Refused = append(s.Refused, label(k))
		case imagesearch.VerdictNoResult:
			if oc.Skipped {
				s.Skipped = append(s.Skipped, label(k))
				continue
			}
			s.NoResult = append(s.NoResult, label(k))
			if oc.Partial {
				s.Partial = append(s.Partial, label(k))
			}
		case imagesearch.VerdictOK:
			if oc.Partial {
				s.Partial = append(s.Partial, label(k))
			}
		}
	}
	return s
}

// searchSummary summarizes the current level 1 lot (live: b.gen == b.searchGen).
func (b *RegistryBrowser) searchSummary() searchSummary {
	return summarizeSearch(b.searchKeys, b.outcomes, b.gen == b.searchGen, b.entryLabel)
}

// entryLabel is the Registry column's label for key: entryAlias, else the URL,
// else the key itself.
func (b *RegistryBrowser) entryLabel(key string) string {
	e := b.entryFor(key)
	if e == nil {
		return key
	}
	if a := entryAlias(*e); a != "" {
		return a
	}
	if e.URL != "" {
		return e.URL
	}
	return key
}

// searchHeaderInfo is the level 1 header: the image count, then one line per class
// that has something to say (nothing when every registry answered: only the count).
// Order: Pending or Interrupted, Failed (red), Refused, Partial (orange), No result,
// Skipped (grey). Each name list is abbreviated by fitNames to the room the header
// gives a value, so a +N is never lost; Skipped drops its hint first. termWidth 0
// or less means the width is not known yet. At most shortcut.HeaderRows lines.
func searchHeaderInfo(images int, s searchSummary, termWidth int) []shortcut.HeaderInfo {
	type line struct {
		key    string
		names  []string
		count  string // a ready value, instead of names
		suffix string
		style  lipgloss.Style
	}
	warn := theme.HeaderWarnValueStyle()
	var lines []line
	if s.Pending > 0 {
		lines = append(lines, line{key: "Pending", count: fmt.Sprintf("%d", s.Pending), style: theme.HeaderValueStyle})
	}
	lines = append(lines,
		line{key: "Interrupted", names: s.Interrupted, style: warn},
		line{key: "Failed", names: s.Failed, style: theme.SeverityTextStyle("CRITICAL")},
		line{key: "Refused", names: s.Refused, style: warn},
		line{key: "Partial", names: s.Partial, style: warn},
		line{key: "No result", names: s.NoResult, style: theme.DimStyle},
		line{key: "Skipped", names: s.Skipped, suffix: skippedHint, style: theme.DimStyle},
	)
	var kept []line
	keys := []string{"Images"}
	for _, l := range lines {
		if l.count == "" && len(l.names) == 0 {
			continue
		}
		// Unreachable today: Pending and Interrupted exclude each other, so at most
		// six classes plus Images fit the budget. A new line (IS-14) must revisit
		// this bound rather than be cut silently here.
		if len(kept)+1 >= shortcut.HeaderRows {
			break
		}
		kept = append(kept, l)
		keys = append(keys, l.key)
	}
	room := noWidthLimit
	if termWidth > 0 {
		room = shortcut.HeaderValueWidth(termWidth, keys)
	}
	infos := []shortcut.HeaderInfo{{Key: "Images", Value: fmt.Sprintf("%d", images), Style: theme.HeaderValueStyle}}
	for _, l := range kept {
		v := l.count
		if v == "" {
			v = fitNames(l.names, l.suffix, room)
		}
		infos = append(infos, shortcut.HeaderInfo{Key: l.key, Value: v, Style: l.style})
	}
	return infos
}
