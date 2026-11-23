package scan

import (
	"bytes"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

// An overlay that failed in a container is reported LOW, not blamed, only when
// kustomize failed to accumulate the very reference that reaches a directory
// outside the mount, for want of the path. Any other failure, a binary run, or a reference
// that does not exist keeps the HIGH finding and kustomize's whole message;
// the LOW one still logs what kustomize said (review of DEVDESK-742).
func TestOnlyAnAccumulationFailureOutsideTheMountIsNotBlamed(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(base, "repo")
	writeFile(t, filepath.Join(repo, "base", "kustomization.yaml"), "resources: []\n")
	writeFile(t, filepath.Join(repo, "overlays", "prod", "kustomization.yaml"), "resources:\n- ../../base\n")
	writeFile(t, filepath.Join(repo, "overlays", "broken", "kustomization.yaml"), "resources:\n- ../../missing\n")
	target := filepath.Join(repo, "overlays")

	const accumulation = "Error: accumulating resources: accumulation err='accumulating resources from '../../base': " +
		"evalsymlink failure on '/base' : lstat /base: no such file or directory'"
	// Measured on kustomize v5.8.3: a malformed resource file in the mount
	// also fails "accumulating resources", without naming the reference that
	// leaves the mount nor a path that does not exist.
	const brokenFile = "Error: accumulating resources: accumulating resources from 'broken.yaml': " +
		"MalformedYAMLError: yaml: line 1: did not find expected node content in File: broken.yaml"
	const patch = "Error: no matches for Id Deployment.v1.apps/web.[noNs]; failed to find unique target for patch"
	image := ToolSpec{Source: ToolSourceContainer}
	binary := ToolSpec{Source: ToolSourceBinary}
	tests := []struct {
		name   string
		tool   ToolSpec
		dir    string
		stderr string
		want   SeverityLevel
	}{
		{"image, accumulation, reaches outside", image, "prod", accumulation, SeverityLow},
		{"image, another failure, reaches outside", image, "prod", patch, SeverityHigh},
		{"image, a broken file in the mount, reaches outside", image, "prod", brokenFile, SeverityHigh},
		{"binary, accumulation, reaches outside", binary, "prod", accumulation, SeverityHigh},
		{"image, accumulation, reference missing", image, "broken", accumulation, SeverityHigh},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logged bytes.Buffer
			previous := log.Writer()
			log.SetOutput(&logged)
			t.Cleanup(func() { log.SetOutput(previous) })
			v := &k8sValidation{target: target, scope: targetOnly(target), report: &KubeconformReport{}}

			f := v.overlayFailure(t.Context(), tt.tool, tt.dir, tt.dir+"/kustomization.yaml",
				&exitError{Code: 1, Stderr: tt.stderr})

			if f.Severity != tt.want {
				t.Fatalf("severity %s (%q), want %s", f.Severity, f.Title, tt.want)
			}
			if tt.want == SeverityHigh && f.Message != tt.stderr {
				t.Errorf("message = %q, want kustomize's whole message", f.Message)
			}
			if tt.want == SeverityLow && !strings.Contains(logged.String(), tt.stderr) {
				t.Errorf("kustomize's error is not in the log:\n%s", logged.String())
			}
		})
	}
}

// A reference is written by the repository's author: its control characters
// (escape sequences, newlines) never reach the finding.
func TestAnOutsideReferenceIsShownWithoutControlCharacters(t *testing.T) {
	f := outsideMountFinding("prod/kustomization.yaml", "prod", "prod", "../../x\x1b[2J\n\u0085y")
	for _, text := range []string{f.Title, f.Message} {
		if strings.IndexFunc(text, unicode.IsControl) >= 0 {
			t.Errorf("control character in %q", text)
		}
		if !strings.Contains(text, "../../x[2Jy") {
			t.Errorf("the printable part of the reference is gone: %q", text)
		}
	}
}
