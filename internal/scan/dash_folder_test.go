package scan

import (
	"reflect"
	"strings"
	"testing"
)

// DEVDESK-763: the folder of a chart or an overlay, and the name of a manifest,
// come from the repository being scanned. One that starts with "-" must reach
// helm, kustomize and kubeconform as a path, never as an option.

const (
	dashOverlay = "--load-restrictor=LoadRestrictionsNone"
	dashChart   = "-x"
)

func TestADashFolderIsPassedToTheRenderersAsAPath(t *testing.T) {
	binary := ToolSpec{Source: ToolSourceBinary}
	withArgs := ToolSpec{Source: ToolSourceBinary, Args: []string{"--set", "a=b"}}
	scope := targetOnly("/repo")

	tests := []struct {
		name string
		got  toolCmd
		want []string
	}{
		{"helm lint", helmArgs(scope, dashChart, binary, "lint"), []string{"lint", "./-x"}},
		{"helm template", helmArgs(scope, dashChart, binary, "template"), []string{"template", "./-x"}},
		{"helm template with the user's flags", helmArgs(scope, dashChart, withArgs, "template"),
			[]string{"template", "--set", "a=b", "./-x"}},
		{"kustomize build", kustomizeArgs(scope, dashOverlay, binary),
			[]string{"build", "./--load-restrictor=LoadRestrictionsNone"}},
		// Only the first segment can be read as an option.
		{"a dash below the first segment", helmArgs(scope, "charts/-x", binary, "lint"), []string{"lint", "charts/-x"}},
		// The names that need nothing keep their argv, byte for byte.
		{"helm ordinary", helmArgs(scope, "charts/app", binary, "template"), []string{"template", "charts/app"}},
		{"kustomize ordinary", kustomizeArgs(scope, "kustomizations/dev", binary), []string{"build", "kustomizations/dev"}},
	}
	for _, tt := range tests {
		if !reflect.DeepEqual(tt.got.Args, tt.want) {
			t.Errorf("%s: args = %q, want %q", tt.name, tt.got.Args, tt.want)
		}
		if tt.got.Dir != "/repo" {
			t.Errorf("%s: dir = %q", tt.name, tt.got.Dir)
		}
	}
}

func TestADashFolderReachesAContainerRenderingAsAPath(t *testing.T) {
	tool := ToolSpec{Source: ToolSourceContainer, Image: "img:1"}
	for name, tc := range map[string]toolCmd{
		"helm":      helmArgs(targetOnly("/repo"), dashChart, tool, "lint"),
		"kustomize": kustomizeArgs(targetOnly("/repo"), dashOverlay, tool),
	} {
		last := tc.Args[len(tc.Args)-1]
		if !strings.HasPrefix(last, "./-") {
			t.Errorf("%s: last argument %q of %q is not a path", name, last, tc.Args)
		}
	}
}

func TestAKubeconformFileStartingWithADashFollowsTheEndOfOptions(t *testing.T) {
	files := []string{"-x.yaml", "deploy/app.yaml"}
	opts := KubeconformOptions{KubernetesVersion: "1.36.0"}

	native := kubeconformArgs("/repo", files, ToolSpec{Source: ToolSourceBinary}, opts).Args
	want := []string{"-output", "json", "-strict", "-kubernetes-version", "1.36.0", "--", "-x.yaml", "deploy/app.yaml"}
	if !reflect.DeepEqual(native, want) {
		t.Errorf("native args = %q, want %q", native, want)
	}

	box := kubeconformArgs("/repo", files, ToolSpec{Source: ToolSourceContainer}, opts).Args
	if i := indexOf(box, "--"); i < 0 || !reflect.DeepEqual(box[i:], []string{"--", "-x.yaml", "deploy/app.yaml"}) {
		t.Errorf("container args = %q, want the files after --", box)
	}

	plain := kubeconformArgs("/repo", []string{"deploy/app.yaml"}, ToolSpec{Source: ToolSourceBinary}, opts).Args
	if indexOf(plain, "--") >= 0 {
		t.Errorf("ordinary names gained a --: %q", plain)
	}
}

func indexOf(args []string, s string) int {
	for i, a := range args {
		if a == s {
			return i
		}
	}
	return -1
}

// End to end on a synthetic repository, with a scripted runner: what the stage
// runs, and what the findings say.
func TestADashFolderIsRenderedAndReportedUnderItsOwnName(t *testing.T) {
	isolateHome(t)
	repo := writeRepo(t, map[string]string{
		dashOverlay + "/kustomization.yaml": "resources: []\n",
		dashChart + "/Chart.yaml":           "apiVersion: v2\nname: x\nversion: 0.1.0\n",
	})
	r := &scriptedRunner{reply: func(tc toolCmd) ([]byte, error) {
		switch subcommandOf(tc) {
		case "lint":
			return []byte("==> Linting ./-x\n[ERROR] templates/: directory does not exist\n"), &exitError{Code: 1}
		case "template":
			return nil, &exitError{Code: 1, Stderr: "Error: boom"}
		case "build":
			return nil, &exitError{Code: 1, Stderr: "Error: kaboom"}
		}
		t.Errorf("unexpected invocation: %s", tc)
		return nil, nil
	}}
	useRunner(t, r)
	deps := binaries(ToolTrivy, ToolGitleaks, ToolKubeconform, ToolHelm, ToolKustomize)

	result, _ := newScannerWithDeps(k8sSchemaFor("1.36.0"), deps).Scan(t.Context(), repo, TargetDirectory)

	var got [][]string
	for _, c := range r.calls {
		if c.Name == "helm" || c.Name == "kustomize" {
			got = append(got, c.Args)
		}
	}
	want := [][]string{{"lint", "./-x"}, {"template", "./-x"}, {"build", "./--load-restrictor=LoadRestrictionsNone"}}
	if !sameArgvSet(got, want) {
		t.Errorf("renderer argv = %q, want %q", got, want)
	}

	for _, f := range result.Findings {
		if f.ID != K8sRenderID && f.ID != HelmLintID {
			continue
		}
		if strings.HasPrefix(f.File, "./") || strings.Contains(f.File, "/./") {
			t.Errorf("%s: file %q carries the ./ of the argument", f.ID, f.File)
		}
		if top, _, _ := strings.Cut(f.File, "/"); top != dashChart && top != dashOverlay {
			t.Errorf("%s: file %q is not under the folder's own name", f.ID, f.File)
		}
		if strings.Contains(f.Title+" "+strings.Join(f.Steps, " "), "./") {
			t.Errorf("%s: the readable text carries ./: %q %q", f.ID, f.Title, f.Steps)
		}
		if f.Repro != "" {
			t.Errorf("%s: repro %q for a folder that cannot be typed as a plain word", f.ID, f.Repro)
		}
	}
	// The lint error already says what is wrong with the chart, so only the
	// overlay gets a render failure of its own.
	if n := countID(result.Findings, K8sRenderID); n != 1 {
		t.Errorf("%d render findings, want the overlay's: %+v", n, result.Findings)
	}
	if n := countID(result.Findings, HelmLintID); n != 1 {
		t.Errorf("%d lint findings, want the chart's: %+v", n, result.Findings)
	}
}

func sameArgvSet(got, want [][]string) bool {
	if len(got) != len(want) {
		return false
	}
	for _, w := range want {
		found := false
		for _, g := range got {
			if reflect.DeepEqual(g, w) {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func countID(findings []Finding, id string) int {
	n := 0
	for _, f := range findings {
		if f.ID == id {
			n++
		}
	}
	return n
}
