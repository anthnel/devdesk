package security

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/scan"
)

// A resolved pipeline, shaped like the real ones: a hidden job is absent
// because GitLab resolves those away, and the same script line is inlined into
// several jobs.
const resolvedFixture = `stages:
- build
- test
build:
  image: alpine
  script:
  - |
    set -e
    curl -s https://example.test/i.sh | bash
test:
  image: alpine
  script:
  - |
    set -e
    curl -s https://example.test/i.sh | bash
`

func ciFinding(id, job string, sev scan.SeverityLevel) scan.Finding {
	return scan.Finding{ID: id, Job: job, Severity: sev, Source: scan.SourcePlumber}
}

// The comment goes on the job's key line, which is the one anchor that is both
// unambiguous and a real YAML comment.
func TestAFindingIsWrittenOnItsJobLine(t *testing.T) {
	got := annotatePipeline(resolvedFixture, []scan.Finding{
		ciFinding("ISSUE-411", "build", scan.SeverityHigh),
	})

	if !strings.Contains(got, "build:  # plumber: ISSUE-411 (HIGH)") {
		t.Errorf("the job line carries no comment:\n%s", got)
	}
	if strings.Contains(got, "test:  #") {
		t.Error("a job with no finding was annotated")
	}
}

// GitLab resolves hidden jobs away, so a finding about one names something the
// document does not contain. Inventing a line for it would be worse than
// leaving it to the CI tab.
func TestAJobTheDocumentDoesNotContainIsNotAnnotated(t *testing.T) {
	got := annotatePipeline(resolvedFixture, []scan.Finding{
		ciFinding("ISSUE-411", ".hidden-base", scan.SeverityHigh),
	})

	if got != resolvedFixture {
		t.Errorf("the document changed for a job it does not contain:\n%s", got)
	}
}

// The script line is inlined into every job that references it, so it is never
// the anchor — and it lives inside a block scalar, where a `#` is script text
// rather than a comment.
func TestNoCommentIsWrittenInsideABlockScalar(t *testing.T) {
	got := annotatePipeline(resolvedFixture, []scan.Finding{
		ciFinding("ISSUE-411", "build", scan.SeverityHigh),
	})

	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, " ") && strings.Contains(line, "plumber:") {
			t.Errorf("an indented line was annotated: %q", line)
		}
	}
}

// Worst first, then by code, so one document always annotates the same way.
func TestSeveralFindingsOnOneJobShareOneComment(t *testing.T) {
	got := annotatePipeline(resolvedFixture, []scan.Finding{
		ciFinding("ISSUE-403", "build", scan.SeverityLow),
		ciFinding("ISSUE-411", "build", scan.SeverityHigh),
	})

	if !strings.Contains(got, "build:  # plumber: ISSUE-411 (HIGH), ISSUE-403 (LOW)") {
		t.Errorf("the two findings are not on one line, worst first:\n%s", got)
	}
}

// One control raises one issue per offending script line, so a job can carry
// the same code twice. Repeating it says nothing the count does not.
func TestOneCodeAppearsOncePerJob(t *testing.T) {
	got := annotatePipeline(resolvedFixture, []scan.Finding{
		ciFinding("ISSUE-411", "build", scan.SeverityHigh),
		ciFinding("ISSUE-411", "build", scan.SeverityHigh),
	})

	if strings.Count(got, "ISSUE-411") != 1 {
		t.Errorf("the code is repeated on one job:\n%s", got)
	}
}

// Nothing but the annotated lines moves: same line count, and every other line
// byte for byte. The document is the forge's answer, not DevDesk's.
func TestOnlyTheAnnotatedLinesChange(t *testing.T) {
	got := annotatePipeline(resolvedFixture, []scan.Finding{
		ciFinding("ISSUE-411", "build", scan.SeverityHigh),
	})

	before, after := strings.Split(resolvedFixture, "\n"), strings.Split(got, "\n")
	if len(before) != len(after) {
		t.Fatalf("line count changed: %d -> %d", len(before), len(after))
	}
	for i := range before {
		if before[i] == after[i] {
			continue
		}
		if !strings.HasPrefix(after[i], before[i]+"  # plumber: ") {
			t.Errorf("line %d was rewritten rather than appended to:\n  %q\n  %q", i+1, before[i], after[i])
		}
	}
}

// A finding with no job — a project-level control, or an include — anchors
// nothing and changes nothing.
func TestAFindingWithNoJobChangesNothing(t *testing.T) {
	got := annotatePipeline(resolvedFixture, []scan.Finding{
		ciFinding("ISSUE-501", "", scan.SeverityCritical),
	})

	if got != resolvedFixture {
		t.Error("a finding with no job altered the document")
	}
}

// Only plumber's findings are written in: a CVE has nothing to do with a job.
func TestOnlyCIFindingsAreWrittenIn(t *testing.T) {
	got := annotatePipeline(resolvedFixture, []scan.Finding{
		{ID: "CVE-2024-1", Job: "build", Severity: scan.SeverityHigh, Source: scan.SourceTrivy},
	})

	if got != resolvedFixture {
		t.Error("a Trivy finding was written into the pipeline")
	}
}
