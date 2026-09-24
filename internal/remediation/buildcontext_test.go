package remediation

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anthnel/devdesk/internal/patch"
	"github.com/anthnel/devdesk/internal/scan"
)

var gitFinding = scan.Finding{
	ID: scan.BuildContextGitID, Source: scan.SourceBuildContext, IaCType: "dockerfile",
	File: "Dockerfile", Line: 3, EndLine: 3,
}

func applyIgnoreFix(t *testing.T, content string) (string, string) {
	t.Helper()
	rule, ok := FixFor(gitFinding)
	if !ok {
		t.Fatal("no fix for the .git finding")
	}
	edits, reason := rule.Fix([]byte(content), gitFinding)
	if reason != "" {
		return "", reason
	}
	out, err := patch.Rewrite([]byte(content), edits)
	if err != nil {
		t.Fatal(err)
	}
	return string(out), ""
}

func TestGitIsAppendedToTheDockerignore(t *testing.T) {
	for _, tt := range []struct{ name, in, want string }{
		{"after the last line", "node_modules\n", "node_modules\n.git\n"},
		{"to a file with no final newline", "node_modules", "node_modules\n.git\n"},
		{"keeping CRLF", "node_modules\r\n", "node_modules\r\n.git\r\n"},
		{"to an empty file", "", ".git\n"},
		// Last match wins, so an append also overrides a re-inclusion.
		{"after a negation that brought part of it back", ".git\n!.git/config\n", ".git\n!.git/config\n.git\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := applyIgnoreFix(t, tt.in)
			if reason != "" || got != tt.want {
				t.Errorf("got %q (%s), want %q", got, reason, tt.want)
			}
		})
	}
}

func TestAnIgnoreFileThatAlreadyExcludesGitIsLeftAlone(t *testing.T) {
	if _, reason := applyIgnoreFix(t, "**/.git\n"); reason != ReasonGitAlreadyIgnored {
		t.Errorf("reason = %q, want %q", reason, ReasonGitAlreadyIgnored)
	}
}

// The edit goes to the one ignore file that applies, and to none when that is
// not a single one.
func TestTheFixTargetsTheIgnoreFile(t *testing.T) {
	rule, _ := FixFor(gitFinding)
	for _, tt := range []struct {
		name   string
		files  []string
		rel    string
		reason string
	}{
		{"none", nil, "", ReasonNoDockerignore},
		{"the context's", []string{".dockerignore"}, ".dockerignore", ""},
		{"the Dockerfile's own", []string{"Dockerfile.dockerignore"}, "Dockerfile.dockerignore", ""},
		{"both", []string{".dockerignore", "Dockerfile.dockerignore"}, "", ReasonTwoIgnoreFiles},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range tt.files {
				if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			rel, reason := rule.FileFor(dir, gitFinding)
			if rel != tt.rel || reason != tt.reason {
				t.Errorf("FileFor = %q, %q; want %q, %q", rel, reason, tt.rel, tt.reason)
			}
		})
	}
}

// Every other rule edits the file its finding points at.
func TestARuleWithoutATargetEditsTheFindingsFile(t *testing.T) {
	rule, _ := FixFor(scan.Finding{ID: "AVD-DS-0002", Source: scan.SourceTrivyMisconfig, File: "api/Dockerfile"})
	if rel, reason := rule.FileFor("/anywhere", scan.Finding{File: "api/Dockerfile"}); rel != "api/Dockerfile" || reason != "" {
		t.Errorf("FileFor = %q, %q", rel, reason)
	}
}

// Sensitive files have no built-in fix: which of them the image needs is not
// something the files say.
func TestSensitiveFilesHaveNoBuiltInFix(t *testing.T) {
	f := gitFinding
	f.ID = scan.BuildContextSensitiveID
	if _, ok := FixFor(f); ok {
		t.Error("a fix is offered for the sensitive-files finding")
	}
}
