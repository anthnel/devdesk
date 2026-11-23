package security

import (
	"cmp"
	"sort"
	"strconv"

	"charm.land/lipgloss/v2"

	"github.com/anthnel/devdesk/internal/remediation"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-754 (SC2): "Group by fix" on the Misconfigurations tab. One row per
// file and per fix: the key is the file, the rule (remediation.RuleKey, which
// folds Trivy's two spellings) and the state of the built-in fix. The count is a
// number of findings and nothing more: ctrl+o fixes one finding at a time, so a
// group never promises to close anything.

// noFileSubject is what the File column says for a finding that names no file,
// which is what an image's misconfigurations do.
const noFileSubject = "(image)"

// misconfigGroups groups the misconfigurations of findings by file and by fix: one
// group per (file, rule, state of the built-in fix). Worst severity first, then
// the most findings, then the file, the rule and the state of the fix.
func misconfigGroups(findings []scan.Finding, onDisk bool) []fixGroup {
	return groupMisconfigs(findings, func(f scan.Finding) fixStatus { return fixStatusOf(f, onDisk) })
}

// groupMisconfigs is misconfigGroups with the status of a finding given, so that
// a test can give two findings of one rule reasons the catalog never produces.
func groupMisconfigs(findings []scan.Finding, statusOf func(scan.Finding) fixStatus) []fixGroup {
	byKey := map[groupKey]*fixGroup{}
	var order []groupKey
	for _, f := range findings {
		if scan.Categorize(f) != scan.CategoryMisconfiguration {
			continue
		}
		status := statusOf(f)
		key := groupKey{File: f.File, Rule: remediation.RuleKey(f.ID), Fix: status.State}
		if status.State == fixBuiltInDeclined {
			key.Reason = status.Reason
		}
		g, ok := byKey[key]
		if !ok {
			g = &fixGroup{
				Key:       key,
				Misconfig: true,
				Severity:  f.Severity,
				Subject:   fileSubject(f.File),
				Rule:      inlineText(f.ID),
				Status:    status,
			}
			byKey[key] = g
			order = append(order, key)
		}
		if severityRank(f.Severity) > severityRank(g.Severity) {
			g.Severity = f.Severity
		}
		g.Members = append(g.Members, f)
	}
	groups := make([]fixGroup, 0, len(order))
	for _, key := range order {
		g := *byKey[key]
		g.Command = sharedCommand(g.Members)
		groups = append(groups, g)
	}
	sort.SliceStable(groups, func(i, j int) bool { return misconfigGroupLess(groups[i], groups[j]) })
	return groups
}

// fileSubject is the file as the row shows it: relative to the scanned directory,
// on one line, with no control or bidi character from the scan.
func fileSubject(file string) string {
	if file == "" {
		return noFileSubject
	}
	return inlineText(shortPaths(file, ""))
}

// sharedCommand is the command every member agrees on, "" when one has none, when
// they differ, or when it is not plain words (runCommand decides all of that).
func sharedCommand(members []scan.Finding) string {
	command := ""
	for i, f := range members {
		c, ok := runCommand(f)
		if !ok || i > 0 && c != command {
			return ""
		}
		command = c
	}
	return command
}

// misconfigGroupLess orders groups by worst severity, then the most findings, then
// the file, the rule and the state of the fix. It reads the key, not the order of
// the scan, so a reversed scan lists the same rows.
func misconfigGroupLess(a, b fixGroup) bool {
	return cmp.Or(
		cmp.Compare(severityRank(b.Severity), severityRank(a.Severity)),
		cmp.Compare(len(b.Members), len(a.Members)),
		cmp.Compare(a.Subject, b.Subject),
		cmp.Compare(a.Key.File, b.Key.File),
		cmp.Compare(a.Key.Rule, b.Key.Rule),
		cmp.Compare(a.Key.Fix, b.Key.Fix),
		cmp.Compare(a.Key.Reason, b.Key.Reason),
	) < 0
}

// misconfigGroupColumns describes the grouped list of the Misconfigurations tab:
// Severity, File, Rule, Findings, Fix. Cells are plain text (Rule 122); only
// Severity is coloured, and it is also its word. The file keeps its tail, which is
// what tells two files apart.
func misconfigGroupColumns() []datatable.Column[fixGroup] {
	return []datatable.Column[fixGroup]{
		{
			Title: "Severity", Sizing: datatable.SizingFixed, MinWidth: 10,
			Cell:  func(g fixGroup) string { return string(g.Severity) },
			Style: func(g fixGroup) lipgloss.Style { return theme.SeverityTextStyle(string(g.Severity)) },
			Less:  func(a, b fixGroup) bool { return severityRank(a.Severity) < severityRank(b.Severity) },
		},
		{
			Title: "File", Sizing: datatable.SizingContent, MinWidth: 14, MaxWidth: 40,
			TruncateHead: true,
			Cell:         func(g fixGroup) string { return g.Subject },
			Less:         func(a, b fixGroup) bool { return a.Subject < b.Subject },
		},
		{
			Title: "Rule", Sizing: datatable.SizingContent, MinWidth: 10, MaxWidth: 24,
			Cell: func(g fixGroup) string { return g.Rule },
			Less: func(a, b fixGroup) bool { return a.Rule < b.Rule },
		},
		{
			Title: "Findings", Sizing: datatable.SizingFixed, MinWidth: 8, DescFirst: true,
			Cell: func(g fixGroup) string { return strconv.Itoa(len(g.Members)) },
			Less: func(a, b fixGroup) bool { return len(a.Members) < len(b.Members) },
		},
		{
			Title: "Fix", Sizing: datatable.SizingContent, MinWidth: 16, Optional: true, Flex: 1,
			Cell: func(g fixGroup) string { return inlineText(g.Status.Label()) },
			Less: func(a, b fixGroup) bool { return a.Status.Label() < b.Status.Label() },
		},
	}
}
