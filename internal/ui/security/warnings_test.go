package security

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// Trivy and Gitleaks write a wall of structured log lines to stderr. The point
// of this layer is that the panel shows the one line that explains the failure
// rather than the wall, so these tests are all about what gets discarded.

func TestParseScanErrorSplitsSourceFromDetail(t *testing.T) {
	source, detail := parseScanError("trivy vuln: trivy failed: 2026-08-01T10:00:00Z\tFATAL\trun error: image not found")

	if source != "trivy vuln" {
		t.Errorf("source = %q, want the scanner that failed", source)
	}
	if detail != "Error : image not found" {
		t.Errorf("detail = %q, want the message alone", detail)
	}
}

// Each scanner wraps its own error differently; the intermediate "<tool>
// failed:" prefix carries nothing the source label does not already say.
func TestParseScanErrorStripsEveryToolPrefix(t *testing.T) {
	prefixes := []string{
		"trivy failed: ",
		"trivy misconfig failed: ",
		"sbom generation failed: ",
		"gitleaks failed: ",
	}

	for _, prefix := range prefixes {
		_, detail := parseScanError("some source: " + prefix + "the real problem")

		if detail != "the real problem" {
			t.Errorf("with prefix %q, detail = %q", prefix, detail)
		}
	}
}

// A message with no source label is still shown rather than dropped.
func TestParseScanErrorWithoutASource(t *testing.T) {
	source, detail := parseScanError("docker daemon unreachable")

	if source != "" {
		t.Errorf("source = %q, want none", source)
	}
	if detail != "docker daemon unreachable" {
		t.Errorf("detail = %q", detail)
	}
}

func TestExtractMeaningfulLines(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "INFO lines are discarded",
			in: "2026-08-01T10:00:00Z\tINFO\tVulnerability scanning is enabled\n" +
				"2026-08-01T10:00:01Z\tERROR\tdb download failed",
			want: "Error : db download failed",
		},
		{
			name: "WARN and WARNING are both kept",
			in: "2026-08-01T10:00:00Z\tWARN\tfirst\n" +
				"2026-08-01T10:00:01Z\tWARNING\tsecond",
			want: "Error : first\nError : second",
		},
		{
			name: "multi-space separators work like tabs",
			in:   "2026-08-01T10:00:00Z   ERROR   logrus style",
			want: "Error : logrus style",
		},
		{
			name: "progress bars are discarded",
			in: "12.34 MiB / 45.67 MiB [-----] 27%\n" +
				"[--- downloading ---]\n" +
				"1.2 p/s eta 3s\n" +
				"2026-08-01T10:00:00Z\tERROR\tthe actual failure",
			want: "Error : the actual failure",
		},
		{
			name: "unstructured lines are kept verbatim",
			in:   "exit status 1\nsomething went wrong",
			want: "exit status 1\nsomething went wrong",
		},
		{
			name: "blank lines are dropped",
			in:   "first\n\n   \nsecond",
			want: "first\nsecond",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractMeaningfulLines(tt.in); got != tt.want {
				t.Errorf("extractMeaningfulLines() =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

// Discarding everything would leave the user with an empty panel and no idea
// why the scan failed, so the raw text comes back instead.
func TestExtractMeaningfulLinesFallsBackToTheOriginal(t *testing.T) {
	onlyInfo := "2026-08-01T10:00:00Z\tINFO\tnothing interesting"

	if got := extractMeaningfulLines(onlyInfo); got != onlyInfo {
		t.Errorf("extractMeaningfulLines() = %q, want the original text", got)
	}
}

// Trivy wraps its fatal errors twice; the panel shows the innermost message.
func TestFatalLinesAreUnwrapped(t *testing.T) {
	tests := []struct {
		name    string
		level   string
		message string
		want    string
	}{
		{"run error", "FATAL", "Fatal error\trun error: image not found", "Error : image not found"},
		{"no run error", "FATAL", "Fatal error\tsomething else", "Error : something else"},
		{"not fatal", "ERROR", "left alone", "Error : left alone"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatScanErrorLine(tt.level, tt.message); got != tt.want {
				t.Errorf("formatScanErrorLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSplitLogLine(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		wantLevel   string
		wantMessage string
	}{
		{"tab separated", "2026-08-01T10:00:00Z\tERROR\tthe message", "ERROR", "the message"},
		{"space separated", "2026-08-01T10:00:00Z   ERROR   the message", "ERROR", "the message"},
		{"level with no message", "2026-08-01T10:00:00Z\tERROR", "ERROR", ""},
		{"no separator at all", "2026-08-01T10:00:00Z", "", "2026-08-01T10:00:00Z"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level, message := splitLogLine(tt.line)

			if level != tt.wantLevel || message != tt.wantMessage {
				t.Errorf("splitLogLine() = (%q, %q), want (%q, %q)", level, message, tt.wantLevel, tt.wantMessage)
			}
		})
	}
}

// ── The panel ────────────────────────────────────────────────────────────────

// Warnings replace the table rather than sitting above it: a scan that failed
// has no partial findings worth browsing, and the tab bar would suggest it did.
func TestWarningsReplaceTheTable(t *testing.T) {
	result := resultFixture()
	result.Errors = []string{"trivy vuln: trivy failed: 2026-08-01T10:00:00Z\tFATAL\trun error: image not found"}

	m := feed(t, NewWithPreloadedResult(testConfig(), result), testutil.Resize(160, 30))

	view := m.View()
	if !strings.Contains(view, "Scan Warnings") {
		t.Errorf("the warnings panel is not shown:\n%s", view)
	}
	if !strings.Contains(view, "image not found") {
		t.Error("the panel does not show the parsed failure")
	}
	if !strings.Contains(view, "trivy vuln") {
		t.Error("the panel does not name the scanner that failed")
	}
	if strings.Contains(view, "CVE-2026-0001") {
		t.Error("the findings table is rendered behind the warnings")
	}
}

// Rule 124: no tab bar when warnings replace the table, so the footer is two
// lines rather than three.
func TestWarningsShrinkTheFooter(t *testing.T) {
	result := resultFixture()
	result.Errors = []string{"trivy: failed"}
	m := feed(t, NewWithPreloadedResult(testConfig(), result), testutil.Resize(160, 30))

	if got := m.GetFooterHeight(); got != 2 {
		t.Errorf("GetFooterHeight() = %d with warnings, want 2", got)
	}
	if lines := strings.Count(m.RenderFooter(160), "\n") + 1; lines != 2 {
		t.Errorf("RenderFooter() emitted %d lines", lines)
	}
}
