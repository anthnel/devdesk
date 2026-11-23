package security

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/scan"
)

// whereLabels are the labels the facts of the Where block can carry.
var whereLabels = []string{"Where:", "Package:", "Job:", "Script:", "Match:"}

// whereRow is the label a line of the Where block starts with, "" for a line
// that continues the previous value.
func whereRow(line string) string {
	trimmed := strings.TrimLeft(line, " ")
	for _, label := range whereLabels {
		if strings.HasPrefix(trimmed, label) {
			return label
		}
	}
	return ""
}

// whereBlock is the lines of the Where block: from its first fact up to the
// first line that is blank or opens another block.
func whereBlock(lines []string) []string {
	var block []string
	for _, line := range lines {
		if len(block) == 0 {
			if whereRow(line) != "" {
				block = append(block, line)
			}
			continue
		}
		if strings.TrimSpace(line) == "" || (whereRow(line) == "" && startsBlockLabel(line)) {
			break
		}
		block = append(block, line)
	}
	return block
}

func licenseFinding() scan.Finding {
	return scan.Finding{
		ID: "MIT", Title: "MIT License", Severity: scan.SeverityLow, Source: scan.SourceTrivyLicense,
		File: "go.mod", PkgName: "github.com/mattn/go-isatty",
	}
}

// The labels of the Where block start in the same column, the values start in
// one column, and a value that wraps hangs under the values.
func TestTheFactsUnderWhereAreAlignedOnTheirValues(t *testing.T) {
	cases := map[string]scan.Finding{
		"license, file and package": licenseFinding(),
		"secret, file only": func() scan.Finding {
			f := gitleaksSecret("config/prod.env", 12)
			f.Match = ""
			return f
		}(),
		"misconfiguration, file only": misconfigOf("DS002"),
		"cve, package only":           cveWith("gomod", "go get golang.org/x/net@v0.7.0"),
		"cve, file and package":       fullFinding(),
		"ci, file job and script":     plumberControl(),
		"secret with a long match":    longSecret(),
		"long package":                longCVE(),
	}
	for name, f := range cases {
		for _, size := range detailsSizes {
			t.Run(fmt.Sprintf("%s/%d", name, size.width), func(t *testing.T) {
				lines := plainLines(openDetail(t, f, size.width, size.height))
				block := whereBlock(lines)
				if len(block) == 0 {
					t.Fatalf("no Where block:\n%s", strings.Join(lines, "\n"))
				}
				pad := leadingSpaces(block[0])
				valueCol := -1
				for _, line := range block {
					if label := whereRow(line); label != "" {
						if got := leadingSpaces(line); got != pad {
							t.Errorf("label %s starts at column %d, want %d:\n%s", label, got, pad, strings.Join(block, "\n"))
						}
						rest := strings.TrimPrefix(strings.TrimLeft(line, " "), label)
						col := len(line) - len(strings.TrimLeft(rest, " "))
						if strings.TrimSpace(rest) == "" {
							continue // the value is on the lines below: too narrow a column
						}
						if valueCol < 0 {
							valueCol = col
						} else if col != valueCol {
							t.Errorf("value of %s starts at column %d, want %d:\n%s", label, col, valueCol, strings.Join(block, "\n"))
						}
						continue
					}
					if valueCol >= 0 && leadingSpaces(line) != valueCol {
						t.Errorf("continuation at column %d, want %d:\n%s", leadingSpaces(line), valueCol, strings.Join(block, "\n"))
					}
				}
				for _, line := range block {
					if w := ansi.StringWidth(line); w > size.width-2 {
						t.Errorf("a line spans %d cells, limit %d: %q", w, size.width-2, line)
					}
				}
			})
		}
	}
}

// The license finding of the report: Package is a label of its own, under Where.
func TestALicenseFindingAlignsPackageUnderWhere(t *testing.T) {
	lines := plainLines(openDetail(t, licenseFinding(), 120, 28))
	got := whereBlock(lines)
	want := []string{"Where:   go.mod", "Package: github.com/mattn/go-isatty"}
	if len(got) != len(want) {
		t.Fatalf("Where block = %q, want %q", got, want)
	}
	for i := range want {
		if strings.TrimSpace(got[i]) != want[i] || leadingSpaces(got[i]) != leadingSpaces(got[0]) {
			t.Errorf("Where block line %d = %q, want %q at the same column", i, got[i], want[i])
		}
	}
}
