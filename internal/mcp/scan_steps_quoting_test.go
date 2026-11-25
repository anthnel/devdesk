package mcp

import (
	"context"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/testutil/stepshell"
)

// DEVDESK-772: the steps cross the pipe to an agent, which may copy one into a
// shell. The folder names in them come from the scanned repository, so the steps
// scan_result returns must be protected the same way the finding detail shows
// them: the folder is one literal argument, and a name no quote protects gives a
// text, not a command.
//
// The findings are not hand-written: a real scan stage (scan.RunKubeconform, with
// stub helm and kustomize programs that fail) builds them from a repository whose
// folders have hostile names, they go through the cache the tool reads, and the
// steps are taken from the tool's answer.

// posixQuote is the form the steps must show for a name that is not plain: the
// name in single quotes, an apostrophe inside it closing the quote, escaped, and
// opening it again.
func posixQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatalf("script %s: %v", name, err)
	}
	return p
}

func TestScanResultStepsAreSafeToPasteForAHostileFolderName(t *testing.T) {
	stepshell.Available(t)
	fakeCacheHome(t)
	repo := existingDir(t)
	// The scan stage logs each command it runs; that is noise here.
	previous := log.Writer()
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(previous) })

	pasteable := []string{"$(touch pwned)", "a;touch pwned", "`id`", "a b", "a'b", "a&b", "a|b", "a*", "a>pwned", "a b'c"}
	const lineBreak = "x\ntouch pwned"
	for _, name := range append(append([]string{}, pasteable...), lineBreak) {
		for _, d := range []struct{ dir, marker, body string }{
			{"charts", "Chart.yaml", "name: c\nversion: 0.1.0\n"},
			{"overlays", "kustomization.yaml", "resources: []\n"},
		} {
			dir := filepath.Join(repo, d.dir, name)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Skipf("this file system refuses the folder name %q: %v", name, err)
			}
			if err := os.WriteFile(filepath.Join(dir, d.marker), []byte(d.body), 0o644); err != nil {
				t.Fatalf("marker: %v", err)
			}
		}
	}

	bin := t.TempDir()
	// The first helm fails its lint with a message the parser reads (HELM-LINT);
	// the second prints nothing the parser knows, so the template that follows is
	// what fails (K8S-RENDER, with the dependency step). kustomize always fails.
	lintFails := writeScript(t, bin, "helm-lint", "[ \"$1\" = lint ] && { echo 'Error boom'; exit 1; }\nexit 1\n")
	templateFails := writeScript(t, bin, "helm-template", "echo unreadable\nexit 1\n")
	kustomizeFails := writeScript(t, bin, "kustomize-fails", "echo boom >&2\nexit 1\n")
	kubeconform := scan.ToolSpec{Source: scan.ToolSourceBinary, Binary: "kubeconform"}

	var findings []scan.Finding
	for _, helm := range []string{lintFails, templateFails} {
		opts := scan.KubeconformOptions{
			KubernetesVersion: "1.36.0",
			Helm:              scan.ToolSpec{Source: scan.ToolSourceBinary, Binary: helm},
		}
		if helm == lintFails {
			opts.Kustomize = scan.ToolSpec{Source: scan.ToolSourceBinary, Binary: kustomizeFails}
		}
		report, err := scan.RunKubeconform(context.Background(), repo, kubeconform, opts, nil)
		if err != nil {
			t.Fatalf("RunKubeconform: %v", err)
		}
		findings = append(findings, report.Findings...)
	}
	storeRepoResult(t, repo, findings...)

	var out scanResultOut
	callTool(t, connect(t, testEnv(nil)), "scan_result", map[string]any{"target": repo, "limit": 100}, &out)

	programs := []struct {
		text, stub string
		argv       []string
	}{
		{"helm template", "helm", []string{"template"}},
		{"helm dependency build", "helm", []string{"dependency", "build"}},
		{"helm lint", "helm", []string{"lint"}},
		{"kustomize build", "kustomize", []string{"build"}},
	}
	pasted := map[string]int{}
	var controlSteps int
	for _, f := range out.Findings {
		if f.ID != scan.HelmLintID && f.ID != scan.K8sRenderID {
			continue
		}
		dir := f.File
		if f.ID == scan.K8sRenderID {
			dir = path.Dir(f.File)
		}
		if strings.ContainsFunc(dir, unicode.IsControl) {
			for _, s := range f.Steps {
				controlSteps++
				if strings.ContainsFunc(s, unicode.IsControl) {
					t.Errorf("%s %q: step %q holds a line break or a control character", f.ID, dir, s)
				}
			}
			continue
		}
		for _, step := range f.Steps {
			for _, p := range programs {
				cmd, ok := stepshell.Command(step, p.text)
				if !ok {
					continue
				}
				pasted[p.text]++
				res := stepshell.Run(t, cmd, p.stub)
				want := append(append([]string{}, p.argv...), dir)
				if res.Pwned {
					t.Errorf("%s %q: pasting %q created the file pwned", f.ID, dir, cmd)
				}
				if !res.Called || strings.Join(res.Args, "\x00") != strings.Join(want, "\x00") {
					t.Errorf("%s %q: pasting %q gave the arguments %q (called %v), want %q", f.ID, dir, cmd, res.Args, res.Called, want)
				}
				if strings.ContainsAny(dir, " $'`;&|*>#") && !strings.Contains(step, posixQuote(dir)) {
					t.Errorf("%s %q: step %q does not show the folder as %s", f.ID, dir, step, posixQuote(dir))
				}
			}
		}
	}

	// The test must not pass on nothing: every command of every hostile folder
	// was found in the tool's answer, and the folder with a line break gave steps.
	for _, p := range programs {
		if pasted[p.text] < len(pasteable) {
			t.Errorf("%s: %d commands found in the answer, want one per folder (%d)", p.text, pasted[p.text], len(pasteable))
		}
	}
	if controlSteps == 0 {
		t.Error("the folder with a line break gave no step in the answer")
	}
}
