package scan

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// DEVDESK-742. An overlay that `kustomize build` builds where it lives must not
// be reported as an overlay that does not build because of how DevDesk ran
// kustomize. The repository is
//
//	deploy/base/{kustomization.yaml,cm.yaml}
//	deploy/overlays/prod/kustomization.yaml     resources: [../../base]
//	deploy/overlays/broken/kustomization.yaml   resources: [../../missing]
//
// scanned from its root and from deploy/overlays, with kustomize as a binary
// and as an image. The binary sees the whole host filesystem; the image sees
// what is mounted at /scan. Measured with kustomize v5.8.3: built from a copy
// of deploy/overlays alone, prod fails with "accumulating resources from
// '../../base': evalsymlink failure on …: no such file or directory".
//
// With the git repository mounted, prod builds from deploy/overlays too. When
// nothing wider than the target can be mounted (here git does not answer),
// DevDesk cannot tell a reference it hid from a broken one: prod gets the
// HIGH finding with kustomize's whole message, like broken, which is broken
// wherever it is built.
func TestAnOverlayIsNotBlamedForWhatTheMountHides(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(base, "repo")
	useHome(t, filepath.Join(base, "home"), nil)
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(repo, filepath.FromSlash(rel))
		mkdirs(t, filepath.Dir(p))
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mkdirs(t, filepath.Join(repo, ".git"))
	write("deploy/base/kustomization.yaml", "resources:\n- cm.yaml\n")
	write("deploy/base/cm.yaml", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: base\n")
	write("deploy/overlays/prod/kustomization.yaml", "resources:\n- ../../base\n")
	write("deploy/overlays/broken/kustomization.yaml", "resources:\n- ../../missing\n")

	binary := ToolSpec{Source: ToolSourceBinary, Binary: "kustomize"}
	image := ToolSpec{Source: ToolSourceContainer}
	sub := filepath.Join(repo, "deploy", "overlays")
	// want maps each overlay's kustomization, relative to the target, to the
	// severity of its K8S-RENDER finding, "" for none.
	tests := []struct {
		name      string
		target    string
		kustomize ToolSpec
		gitRoot   string // "" when git does not answer
		want      map[string]SeverityLevel
	}{
		{"binary, repository root", repo, binary, repo, map[string]SeverityLevel{
			"deploy/overlays/prod/kustomization.yaml": "", "deploy/overlays/broken/kustomization.yaml": SeverityHigh}},
		{"binary, sub-directory", sub, binary, repo, map[string]SeverityLevel{
			"prod/kustomization.yaml": "", "broken/kustomization.yaml": SeverityHigh}},
		{"image, repository root", repo, image, repo, map[string]SeverityLevel{
			"deploy/overlays/prod/kustomization.yaml": "", "deploy/overlays/broken/kustomization.yaml": SeverityHigh}},
		{"image, sub-directory of a git repository", sub, image, repo, map[string]SeverityLevel{
			"prod/kustomization.yaml": "", "broken/kustomization.yaml": SeverityHigh}},
		{"image, sub-directory and no git", sub, image, "", map[string]SeverityLevel{
			"prod/kustomization.yaml": SeverityHigh, "broken/kustomization.yaml": SeverityHigh}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useRunner(t, &scriptedRunner{reply: func(tc toolCmd) ([]byte, error) {
				if tc.Name == "git" {
					if len(tc.Args) > 0 && tc.Args[0] == "config" {
						return nil, &exitError{Code: 1} // core.worktree is not set
					}
					if tt.gitRoot == "" {
						return nil, &exitError{Code: 128, Stderr: "fatal: not a git repository"}
					}
					return []byte(tt.gitRoot + "\n"), nil
				}
				return fakeKustomizeBuild(t, tc)
			}})
			opts := KubeconformOptions{KubernetesVersion: "1.36.0", Kustomize: tt.kustomize}
			kubeconform := ToolSpec{Source: ToolSourceBinary, Binary: "kubeconform"}

			report, err := RunKubeconform(t.Context(), tt.target, kubeconform, opts, nil)
			if err != nil {
				t.Fatalf("RunKubeconform: %v", err)
			}
			got := map[string]Finding{}
			for _, f := range report.Findings {
				if f.ID == K8sRenderID {
					got[f.File] = f
				}
			}
			for file, severity := range tt.want {
				f, found := got[file]
				switch {
				case severity == "" && found:
					t.Errorf("%s: %s %q, but it builds where it lives\n%s", file, f.Severity, f.Title, f.Message)
				case severity != "" && !found:
					t.Errorf("%s: no K8S-RENDER finding among %v, want %s", file, report.Findings, severity)
				case found && f.Severity != severity:
					t.Errorf("%s: %s %q, want %s", file, f.Severity, f.Title, severity)
				}
			}
			for file, ref := range map[string]string{
				"prod/kustomization.yaml": "'../../base'", "broken/kustomization.yaml": "'../../missing'",
			} {
				if f, ok := got[file]; ok && !strings.Contains(f.Message, "accumulating resources from "+ref) {
					t.Errorf("%s lost kustomize's message: %q", file, f.Message)
				}
			}
		})
	}
}

// fakeKustomizeBuild answers like kustomize build for the directory references
// of a kustomization, with the filesystem a binary or a container would see: a
// binary resolves against the host, a container only inside its /scan mount.
// Every other invocation (kubeconform on stdin) gets a clean answer.
func fakeKustomizeBuild(t *testing.T, tc toolCmd) ([]byte, error) {
	t.Helper()
	build := -1
	for i, a := range tc.Args {
		if a == "build" {
			build = i
		}
	}
	if build < 0 || build == len(tc.Args)-1 {
		return []byte(`{"resources":[]}`), nil
	}
	dir := tc.Args[len(tc.Args)-1]

	// resolve maps a path the tool names to a host path, or "" when the tool
	// cannot see it.
	var resolve func(p string) string
	if tc.Dir != "" { // a binary: relative to its working directory, anywhere on the host
		resolve = func(p string) string { return filepath.Join(tc.Dir, filepath.FromSlash(p)) }
	} else { // an image: only what is mounted at /scan, from its -w
		host, workdir := "", ""
		for i := 0; i+1 < len(tc.Args); i++ {
			switch {
			case tc.Args[i] == "-v" && strings.HasSuffix(tc.Args[i+1], ":"+containerScanPath+":ro"):
				host = strings.TrimSuffix(tc.Args[i+1], ":"+containerScanPath+":ro")
			case tc.Args[i] == "-w":
				workdir = tc.Args[i+1]
			}
		}
		if host == "" || !strings.HasPrefix(workdir, containerScanPath) {
			t.Fatalf("no read-only /scan mount and working directory under it in %s", tc)
		}
		resolve = func(p string) string {
			inContainer := path.Clean(path.Join(workdir, p))
			if inContainer != containerScanPath && !strings.HasPrefix(inContainer, containerScanPath+"/") {
				return ""
			}
			return filepath.Join(host, filepath.FromSlash(strings.TrimPrefix(inContainer, containerScanPath)))
		}
	}

	content, err := os.ReadFile(filepath.Join(resolve(dir), "kustomization.yaml"))
	if err != nil {
		return nil, &exitError{Code: 1, Stderr: "Error: unable to find one of 'kustomization.yaml' in " + dir}
	}
	var k struct {
		Resources []string `yaml:"resources"`
	}
	if err := yaml.Unmarshal(content, &k); err != nil {
		t.Fatal(err)
	}
	for _, ref := range k.Resources {
		target := resolve(path.Join(dir, ref))
		if _, statErr := os.Stat(target); target == "" || statErr != nil {
			return nil, &exitError{Code: 1, Stderr: "Error: accumulating resources: accumulation err='accumulating resources from '" +
				ref + "': evalsymlink failure: no such file or directory'"}
		}
	}
	return []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: base\n"), nil
}
