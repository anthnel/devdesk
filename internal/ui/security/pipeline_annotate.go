package security

import (
	"fmt"
	"sort"
	"strings"

	"github.com/anthnel/devdesk/internal/scan"
)

// annotatePipeline writes each CI finding into the resolved document, as a YAML
// comment on the line that defines the job it is about.
//
// **Only the job's key line, and only in column 0.** Two measurements decide
// that, both taken on a real resolved pipeline:
//
//   - The offending script line is not a usable anchor. The same `before_script`
//     is inlined into every job that references it — twelve occurrences of one
//     line in one document — so a text match names no single job. And those
//     lines sit inside block scalars, where a `#` is not a comment at all but
//     script text: annotating there would rewrite the pipeline rather than
//     describe it.
//   - A job key is unambiguous and safe. It is a mapping key at the top level,
//     so a trailing comment is a comment, and there is exactly one of each.
//
// **A job the document does not contain is not annotated.** GitLab resolves
// hidden jobs — the `.`-prefixed templates — away, so a finding about one names
// something the resolved document genuinely does not have. Inventing a line for
// it would be worse than leaving it to the CI tab, which lists it either way.
//
// The comment names plumber, so a reader can tell what the forge emitted from
// what DevDesk added.
func annotatePipeline(document string, findings []scan.Finding) string {
	byJob := findingsByJob(findings)
	if len(byJob) == 0 {
		return document
	}

	lines := strings.Split(document, "\n")
	for i, line := range lines {
		job, ok := jobKeyOf(line)
		if !ok {
			continue
		}
		if labels, found := byJob[job]; found {
			lines[i] = line + "  # plumber: " + strings.Join(labels, ", ")
		}
	}
	return strings.Join(lines, "\n")
}

// findingsByJob groups the CI findings that name a job, in the order a reader
// would rank them: worst severity first, then by code so one document always
// annotates the same way.
func findingsByJob(findings []scan.Finding) map[string][]string {
	type entry struct {
		label string
		rank  int
		code  string
	}
	grouped := make(map[string][]entry)
	for _, f := range findings {
		job := jobOf(f)
		if f.Source != scan.SourcePlumber || job == "" {
			continue
		}
		grouped[job] = append(grouped[job], entry{
			label: fmt.Sprintf("%s (%s)", f.ID, f.Severity),
			rank:  severityRank(f.Severity),
			code:  f.ID,
		})
	}

	out := make(map[string][]string, len(grouped))
	for job, entries := range grouped {
		sort.SliceStable(entries, func(i, j int) bool {
			if entries[i].rank != entries[j].rank {
				return entries[i].rank > entries[j].rank
			}
			return entries[i].code < entries[j].code
		})
		labels := make([]string, 0, len(entries))
		seen := make(map[string]bool, len(entries))
		for _, e := range entries {
			// One control can raise the same code twice on one job — plumber
			// reports one issue per offending script line. The job carries the
			// code once: repeating it says nothing the count does not.
			if seen[e.label] {
				continue
			}
			seen[e.label] = true
			labels = append(labels, e.label)
		}
		out[job] = labels
	}
	return out
}

// descriptionJobPrefix is how plumberFinding wrote a job before Finding.Job
// existed. It is DevDesk's own format, not plumber's — the tool emits a field.
const descriptionJobPrefix = "job: "

// jobOf is the job a finding is about, read from a stored result of any age.
//
// Every result written before Finding.Job existed carries the job in its
// Description and nowhere else, so a reader that only looked at the field would
// find nothing on every scan already on disk — and the document would come back
// with no comment in it and nothing saying why. That is an absence read as an
// emptiness, and rescanning every target is not a fix a user can be expected to
// discover.
//
// Reading Description back is safe because the string is DevDesk's: it was
// written as "job: " + issue.Job by this repository, so this parses its own
// output rather than guessing at a tool's. A description that is not a job —
// "branch: main", a bare type — has no prefix and yields nothing.
func jobOf(f scan.Finding) string {
	if f.Job != "" {
		return f.Job
	}
	if rest, ok := strings.CutPrefix(f.Description, descriptionJobPrefix); ok {
		return strings.TrimSpace(rest)
	}
	return ""
}

// jobKeyOf returns the job a line defines, and whether it defines one.
//
// Column 0 and nothing else: an indented `key:` belongs to a job rather than
// naming one, and a line inside a block scalar can look like anything at all.
func jobKeyOf(line string) (string, bool) {
	if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '#' || line[0] == '-' {
		return "", false
	}
	idx := strings.Index(line, ":")
	if idx <= 0 {
		return "", false
	}
	// A key line ends at the colon, or continues with a value. Either way what
	// follows the colon must not start a new token on the same column.
	rest := line[idx+1:]
	if rest != "" && !strings.HasPrefix(rest, " ") {
		return "", false
	}
	return line[:idx], true
}
