package scan

import "testing"

// The offending line is the only thing plumber emits that points *inside* a
// job, and it was being dropped: an ISSUE-411 arrived naming a job with nothing
// saying which of its lines raised it.
func TestAScriptIssueCarriesTheOffendingLine(t *testing.T) {
	report, err := parsePlumberOutput(plumberFixture(t, "plumber_complete.json"))
	if err != nil {
		t.Fatalf("parsePlumberOutput() error: %v", err)
	}

	var got *Finding
	for i := range report.Findings {
		if report.Findings[i].ID == "ISSUE-411" {
			got = &report.Findings[i]
		}
	}
	if got == nil {
		t.Fatal("the fixture's ISSUE-411 did not survive parsing")
	}
	if got.ScriptLine != "curl -s https://example.com/i.sh | bash" {
		t.Errorf("ScriptLine = %q, want the offending command", got.ScriptLine)
	}
	if got.Job != "ci/build" {
		t.Errorf("Job = %q, want ci/build", got.Job)
	}
}

// A control about the project rather than the pipeline anchors nothing, and
// says so by leaving both fields empty rather than by inventing a location.
func TestAProjectLevelIssueAnchorsNothing(t *testing.T) {
	report, err := parsePlumberOutput(plumberFixture(t, "plumber_complete.json"))
	if err != nil {
		t.Fatalf("parsePlumberOutput() error: %v", err)
	}

	for _, f := range report.Findings {
		if f.ID != "ISSUE-501" {
			continue
		}
		if f.Job != "" || f.ScriptLine != "" {
			t.Errorf("a branch-protection issue claims an anchor: job=%q script=%q", f.Job, f.ScriptLine)
		}
		return
	}
	t.Fatal("the fixture's ISSUE-501 did not survive parsing")
}
