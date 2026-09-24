package security

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/remediation"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

const contextDockerfile = "FROM node:22\nCOPY . /app\nCMD [\"node\", \"/app\"]\n"

// buildContextModel opens the Misconfigurations tab on a directory whose
// Dockerfile copies the whole context, with the finding the check would emit —
// read from the check itself, so the test does not restate its wording.
func buildContextModel(t *testing.T, ignore *string) (Model, string) {
	t.Helper()
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Dockerfile", contextDockerfile)
	if ignore != nil {
		write(".dockerignore", *ignore)
	}
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	findings, err := scan.CheckBuildContext(dir)
	if err != nil || len(findings) != 1 {
		t.Fatalf("CheckBuildContext = %+v, %v; want the .git finding alone", findings, err)
	}
	result := &scan.Result{Target: dir, TargetType: scan.TargetDirectory, Findings: findings}
	m := feed(t, NewWithPreloadedResult(testConfig(), nil, result), tea.WindowSizeMsg{Width: 160, Height: 30})
	for range TabMisconfig {
		m = feed(t, m, testutil.Key("tab"))
	}
	return m, dir
}

func readIn(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// §3.81: the finding points at the Dockerfile, the fix lands in the
// .dockerignore beside it — and the verification looks for the rule where the
// finding was, not where the edit went.
func TestTheBuildContextFixWritesTheDockerignoreNotTheDockerfile(t *testing.T) {
	ignore := "node_modules"
	m, dir := buildContextModel(t, &ignore)

	m = writeFix(t, m, dir)

	if got := readIn(t, dir, ".dockerignore"); got != "node_modules\n.git\n" {
		t.Errorf(".dockerignore = %q, want .git appended on a line of its own", got)
	}
	if got := readIn(t, dir, "Dockerfile"); got != contextDockerfile {
		t.Errorf("the Dockerfile was changed:\n%s", got)
	}
	if m.misconfigVerifying == nil || m.misconfigVerifying.File != "Dockerfile" {
		t.Fatalf("verification = %+v, want it looking in the Dockerfile", m.misconfigVerifying)
	}
	// And the re-scan it waits on would find nothing left to report.
	if findings, _ := scan.CheckBuildContext(dir); len(findings) != 0 {
		t.Errorf("still reported after the fix: %+v", findings)
	}
}

// DevDesk adds to a .dockerignore, it does not write one: with none, the key
// declines in the footer and the directory is left as it was.
func TestWithNoDockerignoreTheFixDeclines(t *testing.T) {
	m, dir := buildContextModel(t, nil)

	m, cmd := step(t, m, testutil.Key(writeRemediationKey))
	m = feed(t, m, run(t, cmd))

	if m.confirmModal != nil {
		t.Fatal("a confirmation opened for a fix that has no file to edit")
	}
	if got := m.footer.Text(); got != remediation.ReasonNoDockerignore {
		t.Errorf("footer = %q, want %q", got, remediation.ReasonNoDockerignore)
	}
	if _, err := os.Stat(filepath.Join(dir, ".dockerignore")); !os.IsNotExist(err) {
		t.Errorf("a .dockerignore was created: %v", err)
	}
}
