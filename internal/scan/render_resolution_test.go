package scan

import (
	"strings"
	"testing"
)

// mountHint is the start of the sentence a kustomize image's missing path
// adds to the resolution.
const mountHint = "If this reference points outside the directory DevDesk mounts into the container"

// loadRestrictorStderr is what kustomize v5 printed for an overlay that names a
// file outside its own directory (DEVDESK-742, a binary run; paths anonymised).
// kubectl apply -k refuses it the same way.
const loadRestrictorStderr = "Error: accumulating resources: accumulation err='accumulating resources from " +
	"'../../notes-infra/flux/dev/notes-infra-dev.yaml': security; file " +
	"'/home/u/src/notes/deployment/notes-infra/flux/dev/notes-infra-dev.yaml' is not in or below " +
	"'/home/u/src/notes/deployment/kustomizations/dev'': must build at directory: " +
	"'/home/u/src/notes/deployment/notes-infra/flux/dev/notes-infra-dev.yaml': file is not directory"

// A render failure says how to fix it in the renderer's own terms: helm's
// advice is about dependencies, kustomize's about building the overlay, and a
// file refused by kustomize's load restrictor gets the two ways out. A path
// kustomize could not find, in a container, may be one the mount hides, and
// the resolution says so; a binary sees the whole file system, so it does not.
// The finding stays HIGH and keeps kustomize's whole message.
func TestARenderFailureIsResolvedInItsRenderersTerms(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		dialect   string
		from      ToolSource
		stderr    string
		want      []string
		forbidden []string
	}{
		{name: "kustomize, file outside the overlay", source: SourceKustomize, dialect: "kustomize",
			from: ToolSourceBinary, stderr: loadRestrictorStderr,
			want: []string{"refuses", "outside the overlay", "kubectl apply -k",
				"directory with its own kustomization",
				"scan.tools.kustomize.args: [--load-restrictor=LoadRestrictionsNone]"},
			forbidden: []string{"helm", "chart"}},
		{name: "kustomize, anything else", source: SourceKustomize, dialect: "kustomize",
			from:      ToolSourceContainer,
			stderr:    "Error: no matches for Id Deployment.v1.apps/web.[noNs]; failed to find unique target for patch",
			want:      []string{"kustomize build"},
			forbidden: []string{"helm", "chart", "load-restrictor", strings.ToLower(mountHint)}},
		{name: "kustomize image, evalsymlink failure", source: SourceKustomize, dialect: "kustomize",
			from:   ToolSourceContainer,
			stderr: "Error: accumulating resources: accumulation err='accumulating resources from '../../base': evalsymlink failure on '/base' : lstat /base: no such file or directory'",
			want:   []string{"kustomize build", mountHint}},
		{name: "kustomize image, no such file", source: SourceKustomize, dialect: "kustomize",
			from:   ToolSourceContainer,
			stderr: "Error: open /scan/x/patch.yaml: no such file or directory",
			want:   []string{"kustomize build", mountHint}},
		{name: "kustomize binary, evalsymlink failure", source: SourceKustomize, dialect: "kustomize",
			from:      ToolSourceBinary,
			stderr:    "Error: accumulating resources: accumulation err='accumulating resources from '../../base': evalsymlink failure on '/x/base' : lstat /x/base: no such file or directory'",
			want:      []string{"kustomize build"},
			forbidden: []string{strings.ToLower(mountHint)}},
		{name: "helm", source: SourceHelm, dialect: "helm",
			from:      ToolSourceContainer,
			stderr:    "Error: open /scan/lib/Chart.yaml: no such file or directory",
			want:      []string{"helm dependency build"},
			forbidden: []string{strings.ToLower(mountHint)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := renderFailure(tt.source, tt.dialect, "x/kustomization.yaml", "Overlay does not build", tt.from,
				nil, "", &exitError{Code: 1, Stderr: tt.stderr})

			if f.Severity != SeverityHigh || f.ID != K8sRenderID {
				t.Errorf("finding = %s %s, want %s HIGH", f.ID, f.Severity, K8sRenderID)
			}
			if f.Message != tt.stderr || !strings.HasSuffix(f.Title, tt.stderr) {
				t.Errorf("the tool's message was not kept whole:\ntitle   %q\nmessage %q", f.Title, f.Message)
			}
			for _, w := range tt.want {
				if !strings.Contains(f.Resolution, w) {
					t.Errorf("resolution lacks %q:\n%s", w, f.Resolution)
				}
			}
			for _, w := range tt.forbidden {
				if strings.Contains(strings.ToLower(f.Resolution), w) {
					t.Errorf("resolution mentions %q:\n%s", w, f.Resolution)
				}
			}
		})
	}
}
