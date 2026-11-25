package scan

import (
	"reflect"
	"slices"
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
		// Every relative folder gets the ./, a dash below the first segment too.
		{"a dash below the first segment", helmArgs(scope, "charts/-x", binary, "lint"), []string{"lint", "./charts/-x"}},
		{"helm ordinary", helmArgs(scope, "charts/app", binary, "template"), []string{"template", "./charts/app"}},
		{"kustomize ordinary", kustomizeArgs(scope, "kustomizations/dev", binary), []string{"build", "./kustomizations/dev"}},
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

// DEVDESK-763: kustomize reads some ordinary folder names as a remote git base
// and runs git fetch even when the folder exists. A "./" in front of every
// relative folder makes it a local path.
func TestEveryRelativeFolderIsPassedWithADotSlash(t *testing.T) {
	cases := []struct{ dir, want string }{
		{"github.com/org/repo", "./github.com/org/repo"},
		{"git@evil.example:o/r", "./git@evil.example:o/r"},
		{"https:/host/r", "./https:/host/r"},
		{"oci:/host/r", "./oci:/host/r"},
		{"file:/host/r", "./file:/host/r"},
		{"kustomizations/dev", "./kustomizations/dev"},
		{"charts/app", "./charts/app"},
		{"app", "./app"},
		{"-x", "./-x"},
		{"..", "./.."},
		// Already a local path: nothing is added.
		{".", "."},
		{"./app", "./app"},
		{"/abs/charts/app", "/abs/charts/app"},
	}
	binary := ToolSpec{Source: ToolSourceBinary}
	withArgs := ToolSpec{Source: ToolSourceBinary, Args: []string{"--set", "a=b"}}
	box := ToolSpec{Source: ToolSourceContainer, Image: "img:1"}
	boxArgs := ToolSpec{Source: ToolSourceContainer, Image: "img:1", Args: []string{"--set", "a=b"}}
	scope := targetOnly("/repo")

	for _, c := range cases {
		for _, sub := range []string{"lint", "template"} {
			if got := helmArgs(scope, c.dir, binary, sub).Args; !reflect.DeepEqual(got, []string{sub, c.want}) {
				t.Errorf("helm %s %q: args = %q, want the folder as %q", sub, c.dir, got, c.want)
			}
			if got := helmArgs(scope, c.dir, withArgs, sub).Args; !reflect.DeepEqual(got, []string{sub, "--set", "a=b", c.want}) {
				t.Errorf("helm %s %q with arguments: args = %q, want the folder as %q", sub, c.dir, got, c.want)
			}
		}
		if got := kustomizeArgs(scope, c.dir, binary).Args; !reflect.DeepEqual(got, []string{"build", c.want}) {
			t.Errorf("kustomize %q: args = %q, want the folder as %q", c.dir, got, c.want)
		}
		if got := kustomizeArgs(scope, c.dir, withArgs).Args; !reflect.DeepEqual(got, []string{"build", "--set", "a=b", c.want}) {
			t.Errorf("kustomize %q with arguments: args = %q, want the folder as %q", c.dir, got, c.want)
		}

		// In a container the last argument of the image is the same path, and
		// the folder is still resolved from the working directory.
		for _, tc := range []toolCmd{
			helmArgs(scope, c.dir, box, "lint"), helmArgs(scope, c.dir, boxArgs, "template"),
			kustomizeArgs(scope, c.dir, box), kustomizeArgs(scope, c.dir, boxArgs),
		} {
			if last := tc.Args[len(tc.Args)-1]; last != c.want {
				t.Errorf("container %q: last argument %q, want %q (args %q)", c.dir, last, c.want, tc.Args)
			}
			if i := indexOf(tc.Args, "-w"); i < 0 || tc.Args[i+1] != "/scan" {
				t.Errorf("container %q: working directory in %q", c.dir, tc.Args)
			}
		}
	}

	// From a subdirectory of the mount the working directory carries the
	// relative part, the folder argument stays relative to it.
	sub := renderScope{target: "/repo/deploy", mount: "/repo", rel: "deploy"}
	tc := kustomizeArgs(sub, "github.com/org/repo", box)
	if i := indexOf(tc.Args, "-w"); i < 0 || tc.Args[i+1] != "/scan/deploy" || tc.Args[len(tc.Args)-1] != "./github.com/org/repo" {
		t.Errorf("container from a subdirectory: %q", tc.Args)
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
		// DEVDESK-763 asked for no "./" in the text of a finding, so that the
		// folder is shown under its own name; DEVDESK-768 refines it, because
		// the text has two kinds of content. What NAMES the folder (the title
		// and the file) keeps its own name, "./" never appears there. What the
		// user is told to TYPE (a step "Run helm lint ... again") is a command
		// that is copied, and for a folder starting with "-" the bare name is
		// read by helm and kustomize as an option: `helm lint -x` fails where
		// `helm lint ./-x` works. So the "./" is allowed in a step, and only as
		// the folder argument of the command, in the form asPathArg gives the
		// tool. The text around it is checked to be free of "./".
		if strings.Contains(f.Title, "./") {
			t.Errorf("%s: the title carries the ./ of the argument: %q", f.ID, f.Title)
		}
		var typed []string // the commands the steps tell the user to run
		for _, step := range f.Steps {
			for _, folder := range []string{dashChart, dashOverlay} {
				for _, cmd := range []string{"helm lint ", "helm template ", "helm dependency build ", "kustomize build "} {
					if strings.Contains(step, cmd+folder) {
						t.Errorf("%s: step %q writes `%s%s`, which the tool reads as an option", f.ID, step, cmd, folder)
					}
					// DEVDESK-772: a name with "=" is shown quoted, ./ inside the quotes
					// (zsh reads a bare x==ls as x=/usr/bin/ls). Either way it is the
					// command's folder argument, and the command is the same.
					for _, arg := range []string{asPathArg(folder), "'" + asPathArg(folder) + "'"} {
						if strings.Contains(step, cmd+arg) {
							typed = append(typed, cmd+asPathArg(folder))
							step = strings.ReplaceAll(step, arg, "")
							break
						}
					}
				}
			}
			if strings.Contains(step, "./") {
				t.Errorf("%s: step %q carries a ./ outside the command's folder argument", f.ID, step)
			}
		}
		switch f.ID {
		case HelmLintID:
			if !slices.Contains(typed, "helm lint "+asPathArg(dashChart)) {
				t.Errorf("%s: no step tells the user to run `helm lint %s`: %q", f.ID, asPathArg(dashChart), f.Steps)
			}
		case K8sRenderID:
			if !slices.Contains(typed, "kustomize build "+asPathArg(dashOverlay)) {
				t.Errorf("%s: no step tells the user to run `kustomize build %s`: %q", f.ID, asPathArg(dashOverlay), f.Steps)
			}
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
