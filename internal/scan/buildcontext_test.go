package scan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
)

// tree writes files under a fresh directory; a name ending in "/" is a
// directory.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func checkIDs(t *testing.T, dir string) []string {
	t.Helper()
	findings, err := CheckBuildContext(dir)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, f := range findings {
		ids = append(ids, f.ID)
	}
	return ids
}

const copyAll = "FROM node:22\nWORKDIR /app\nCOPY . .\n"

func TestTheGitDirectoryInTheContextIsReported(t *testing.T) {
	dir := tree(t, map[string]string{"Dockerfile": copyAll, ".git/": ""})
	findings, err := CheckBuildContext(dir)
	if err != nil || len(findings) != 1 {
		t.Fatalf("findings = %+v, %v", findings, err)
	}
	f := findings[0]
	if f.ID != BuildContextGitID || f.File != "Dockerfile" || f.Line != 3 || f.EndLine != 3 {
		t.Errorf("finding = %+v, want DEVDESK-CTX-001 on the COPY line", f)
	}
	if Categorize(f) != CategoryMisconfiguration || f.IaCType != "dockerfile" || f.Severity != SeverityHigh {
		t.Errorf("finding = %+v, want a HIGH dockerfile misconfiguration", f)
	}
	if !strings.Contains(f.Message, "there is no .dockerignore") {
		t.Errorf("message = %q, want it to say why .git is sent", f.Message)
	}
}

// Every condition is one that was checked: remove any one and nothing is said.
func TestNothingIsReportedWithoutAllThreeFacts(t *testing.T) {
	for _, tt := range []struct {
		name  string
		files map[string]string
	}{
		{"excluded by .dockerignore", map[string]string{"Dockerfile": copyAll, ".git/": "", ".dockerignore": ".git\n"}},
		{"excluded by the Dockerfile's own ignore file", map[string]string{"Dockerfile": copyAll, ".git/": "", "Dockerfile.dockerignore": "**/.git\n"}},
		{"an ignore file that cannot be read with certainty", map[string]string{"Dockerfile": copyAll, ".git/": "", ".dockerignore": "[\n"}},
		{"a worktree's .git is a file", map[string]string{"Dockerfile": copyAll, ".git": "gitdir: /elsewhere\n"}},
		{"no .git at all", map[string]string{"Dockerfile": copyAll}},
		{"a narrow COPY", map[string]string{"Dockerfile": "FROM node:22\nCOPY src /app\n", ".git/": ""}},
		{"the whole context copied in a builder stage only", map[string]string{
			"Dockerfile": "FROM golang:1.23 AS build\nCOPY . /src\nFROM scratch\nCOPY --from=build /src/app /app\n", ".git/": ""}},
		{"a Dockerfile outside the root", map[string]string{"docker/Dockerfile": copyAll, ".git/": ""}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if ids := checkIDs(t, tree(t, tt.files)); len(ids) != 0 {
				t.Errorf("reported %v", ids)
			}
		})
	}
}

// A final stage built FROM an earlier one carries that stage's layers.
func TestAFinalStageInheritsItsParentsCopy(t *testing.T) {
	dir := tree(t, map[string]string{
		"Dockerfile": "FROM node:22 AS base\nCOPY . /app\nFROM base AS final\nCMD [\"node\"]\n",
		".git/":      "",
	})
	if ids := checkIDs(t, dir); len(ids) != 1 || ids[0] != BuildContextGitID {
		t.Errorf("reported %v, want the .git finding", ids)
	}
}

func TestSensitiveFilesInTheContextAreListed(t *testing.T) {
	dir := tree(t, map[string]string{
		"Dockerfile":          copyAll,
		".dockerignore":       "*.pem\n",
		".env":                "SECRET=1\n",
		".env.example":        "SECRET=\n",
		"tls.pem":             "root-level, excluded",
		"certs/client.pem":    "not matched by a root-anchored *.pem",
		"node_modules/.npmrc": "a package's own, not walked",
	})
	findings, err := CheckBuildContext(dir)
	if err != nil || len(findings) != 1 {
		t.Fatalf("findings = %+v, %v", findings, err)
	}
	f := findings[0]
	if f.ID != BuildContextSensitiveID {
		t.Fatalf("id = %s", f.ID)
	}
	if !strings.Contains(f.Message, ".env, certs/client.pem") || strings.Contains(f.Message, "example") ||
		strings.Contains(f.Message, "tls.pem,") || strings.Contains(f.Message, "npmrc") {
		t.Errorf("message = %q, want .env and certs/client.pem only", f.Message)
	}
}

func TestALongListIsCounted(t *testing.T) {
	files := map[string]string{"Dockerfile": copyAll}
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		files[n+".key"] = ""
	}
	findings, _ := CheckBuildContext(tree(t, files))
	if len(findings) != 1 || !strings.HasSuffix(findings[0].Message, "and 2 more") {
		t.Errorf("findings = %+v", findings)
	}
}

// The check needs no tool, so the category alone decides — and only for a
// directory, since an image has no context left to read.
func TestTheCheckRunsWithTheMisconfigurationCategory(t *testing.T) {
	on := config.DefaultScanCategories()
	on.Misconfig.Enabled = true
	off := on
	off.Misconfig.Enabled = false
	for _, tt := range []struct {
		cats   config.ScanCategories
		target TargetType
		want   bool
	}{
		{on, TargetDirectory, true},
		{on, TargetImage, false},
		{off, TargetDirectory, false},
	} {
		s := newScannerWithDeps(ScanOptions{Categories: tt.cats}, Report{})
		if got := s.checksBuildContext(tt.target); got != tt.want {
			t.Errorf("misconfig %v on a %s: %v, want %v", tt.cats.Misconfig.Enabled, tt.target, got, tt.want)
		}
	}
}
