package scan

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// The shape of a plumber report, read off a real run rather than off the
// documentation (§3.42 was twice wrong about this tool from reading it).
//
// The document is a flat object holding one `<control>Result` block per control
// — 23 on the run this was written against, and the set grows with the tool —
// plus a `plumberScore` block and a handful of flags. The control blocks cannot
// be named in a struct without going stale, so they are read by suffix.

// plumberDoc is the part of the report whose keys are fixed.
type plumberDoc struct {
	Score plumberScore `json:"plumberScore"`
	// DataCollectionDegraded is written only when it is true (omitempty), so
	// its absence is the ordinary case rather than a missing field.
	DataCollectionDegraded bool     `json:"dataCollectionDegraded"`
	DegradedReasons        []string `json:"degradedReasons"`
	CIMissing              bool     `json:"ciMissing"`
}

type plumberScore struct {
	Score string `json:"score"`
	// FinalPoints is a **float**, and the fixtures hid it: both happened to
	// score a whole number, so an int decoded them and the first repository
	// with a fractional score failed the whole stage with
	// `cannot unmarshal number 25.698320532936123 into Go struct field
	// plumberScore.finalPoints of type int`. plumber's own banner prints one
	// decimal, which is what gave it away in hindsight.
	FinalPoints float64           `json:"finalPoints"`
	CodeLosses  []plumberCodeLoss `json:"codeLosses"`
	Counts      map[string]int    `json:"counts"`
	Losses      []json.RawMessage `json:"losses"`
}

// plumberCodeLoss is where an issue's severity lives. An issue does not carry
// its own: the join is on `code`, and it is the whole reason --score is not
// optional for DevDesk — without it there is no severity at all, so no column
// to sort and no filter token to tick.
type plumberCodeLoss struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
}

// plumberControl is one `<control>Result` block.
type plumberControl struct {
	ControlName string         `json:"controlName"`
	Status      string         `json:"status"`
	Issues      []plumberIssue `json:"issues"`
}

// plumberIssue is one finding. The fields differ by control — a branch issue
// carries branchName and no url, a workflow issue carries url and job — so
// everything but code is optional.
type plumberIssue struct {
	Code        string `json:"code"`
	DocURL      string `json:"docUrl"`
	Fingerprint string `json:"fingerprint"`
	// URL is an absolute **host** path suffixed with :<line>, and in Docker
	// mode it is the container's. Split rather than shown as it stands.
	URL        string `json:"url"`
	Job        string `json:"job"`
	BranchName string `json:"branchName"`
	Type       string `json:"type"`
}

// parsePlumberOutput turns one report into a PlumberReport.
func parsePlumberOutput(data []byte) (*PlumberReport, error) {
	var doc plumberDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse plumber output: %w", err)
	}

	report := &PlumberReport{
		Points:    doc.Score.FinalPoints,
		Withheld:  doc.DataCollectionDegraded,
		Reasons:   doc.DegradedReasons,
		CIMissing: doc.CIMissing,
	}
	// The letter of a withheld run is not reported, and this is the one place
	// that decides it. A degraded run of the reference fixture reads B/79 where
	// the complete run reads E/30 — a control that did not run found nothing,
	// so the score comes out *better*. Showing it would flatter a repository
	// precisely when least was known about it.
	if !doc.DataCollectionDegraded {
		report.Score = doc.Score.Score
	}

	severity := severityByCode(doc.Score.CodeLosses)

	// The control blocks are found by suffix because their names are the
	// tool's, not ours: naming them here would go stale the first time plumber
	// adds a control, and silently — the block would simply not be read.
	var blocks map[string]json.RawMessage
	if err := json.Unmarshal(data, &blocks); err != nil {
		return nil, fmt.Errorf("failed to parse plumber output: %w", err)
	}
	names := make([]string, 0, len(blocks))
	for name := range blocks {
		if strings.HasSuffix(name, "Result") {
			names = append(names, name)
		}
	}
	// Sorted so one report always produces the same order: a map's iteration
	// order is random, and the findings table would reshuffle on every reload.
	sort.Strings(names)

	for _, name := range names {
		var control plumberControl
		if err := json.Unmarshal(blocks[name], &control); err != nil {
			// A block that does not have this shape is not a control; skipping
			// it is right, and failing the whole report over it would not be.
			continue
		}
		for _, issue := range control.Issues {
			report.Findings = append(report.Findings, plumberFinding(issue, control.ControlName, severity))
		}
	}

	return report, nil
}

// severityByCode indexes the score block for the join.
func severityByCode(losses []plumberCodeLoss) map[string]SeverityLevel {
	out := make(map[string]SeverityLevel, len(losses))
	for _, loss := range losses {
		out[loss.Code] = plumberSeverity(loss.Severity)
	}
	return out
}

// plumberSeverity maps plumber's vocabulary onto DevDesk's.
//
// It tallies exactly — critical, high, medium, low, in lowercase and with no
// "unknown" — so this is an uppercasing and not a translation table. An
// unrecognised value becomes UNKNOWN rather than being dropped: a finding with
// no severity still belongs in the tab, and UNKNOWN sorts below LOW because it
// is the absence of a score rather than a claim of something worse.
func plumberSeverity(s string) SeverityLevel {
	switch strings.ToLower(s) {
	case "critical":
		return SeverityCritical
	case "high":
		return SeverityHigh
	case "medium":
		return SeverityMedium
	case "low":
		return SeverityLow
	default:
		return SeverityUnknown
	}
}

// plumberFinding fills one Finding from one issue.
//
// An issue carries neither title nor description: docUrl points at the page for
// its code, and `plumber explain <code>` is the command made for it. So the
// title is built from the code and the control that raised it, and the
// resolution carries the command — nothing invented, and nothing free either.
func plumberFinding(issue plumberIssue, controlName string, severity map[string]SeverityLevel) Finding {
	file, line := splitPlumberLocation(issue.URL)

	title := issue.Code
	if controlName != "" {
		title = issue.Code + ": " + controlName
	}

	f := Finding{
		ID:          issue.Code,
		Title:       title,
		Severity:    severity[issue.Code],
		Source:      SourcePlumber,
		File:        file,
		Line:        line,
		Fingerprint: issue.Fingerprint,
		Resolution:  "plumber explain " + issue.Code,
	}
	if issue.DocURL != "" {
		f.References = []string{issue.DocURL}
	}
	// What the issue is *about*, when the control says so: a job, a branch, or
	// the kind of problem. It is the only per-issue detail there is.
	switch {
	case issue.Job != "":
		f.Description = "job: " + issue.Job
	case issue.BranchName != "":
		f.Description = "branch: " + issue.BranchName
	case issue.Type != "":
		f.Description = issue.Type
	}
	if severity[issue.Code] == SeverityUnknown {
		// Worth knowing rather than silently ranked last: a code with no entry
		// in codeLosses is a report produced without --score, which this
		// package never does, or a tool that has changed its shape.
		f.Description = strings.TrimSpace(f.Description + " (no severity in the score block)")
	}
	return f
}

// splitPlumberLocation separates the `<path>:<line>` a workflow issue carries.
//
// The path is absolute and host-side — the container's in Docker mode — and
// Windows paths hold a colon of their own, so the split is on the **last** one
// and only when what follows is a number. A path with no line survives whole
// rather than losing its drive letter.
func splitPlumberLocation(url string) (string, int) {
	if url == "" {
		return "", 0
	}
	idx := strings.LastIndex(url, ":")
	if idx <= 0 {
		return url, 0
	}
	line, err := strconv.Atoi(url[idx+1:])
	if err != nil {
		return url, 0
	}
	return url[:idx], line
}
