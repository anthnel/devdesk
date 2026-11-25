package scan

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/k8s"
)

// DEVDESK-771: how renderKustomizations uses the gate. Everything here goes
// through RunKubeconform with a scripted runner and names no symbol of the gate
// itself, so these tests run (and fail) before kustomize_gate.go exists.
//
// The gate runs when kustomize is a native binary, the network is cut
// (allow_remote_bases off) and only then. It is asked about each overlay after
// scope.cut() and before kustomizeArgs. On a refusal kustomize is not launched
// and the finding is K8S-RENDER, HIGH, with no Repro:
//   - a reference that is not local: remoteBlockedHeadline, whose Message names
//     the file, the field and the reference, each as %q;
//   - an overlay that cannot be judged (closed whitelist, fails closed): the
//     title and the Steps of its own.

const uncheckedTitle = "Overlay not built: its references could not be checked"

var nativeKustomize = ToolSpec{Source: ToolSourceBinary, Binary: "kustomize"}

// gateRunner answers git (the repository root, or "not a git repository" when
// gitRoot is empty), `kustomize build` and kubeconform with a success.
func gateRunner(t *testing.T, gitRoot string) *scriptedRunner {
	t.Helper()
	r := &scriptedRunner{}
	r.reply = func(tc toolCmd) ([]byte, error) {
		if tc.Name == "git" {
			if len(tc.Args) > 0 && tc.Args[0] == "config" {
				return nil, &exitError{Code: 1} // core.worktree is not set
			}
			if gitRoot == "" {
				return nil, &exitError{Code: 128, Stderr: "fatal: not a git repository"}
			}
			return []byte(gitRoot + "\n"), nil
		}
		if subcommandOf(tc) == "build" {
			return []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: built\n"), nil
		}
		return []byte(`{"resources":[]}`), nil
	}
	useRunner(t, r)
	return r
}

func runGate(t *testing.T, target string, kustomize ToolSpec, allowRemote bool) *KubeconformReport {
	t.Helper()
	opts := KubeconformOptions{KubernetesVersion: "1.36.0", Kustomize: kustomize, AllowRemoteBases: allowRemote}
	report, err := RunKubeconform(t.Context(), target, ToolSpec{Source: ToolSourceBinary, Binary: "kubeconform"}, opts, nil)
	if err != nil {
		t.Fatalf("RunKubeconform: %v", err)
	}
	return report
}

func renderFindings(report *KubeconformReport) []Finding {
	var out []Finding
	for _, f := range report.Findings {
		if f.ID == K8sRenderID {
			out = append(out, f)
		}
	}
	return out
}

func builds(r *scriptedRunner) []toolCmd {
	return r.callsTo(func(tc toolCmd) bool { return subcommandOf(tc) == "build" })
}

func gitCalls(r *scriptedRunner) []toolCmd {
	return r.callsTo(func(tc toolCmd) bool { return tc.Name == "git" })
}

// onlyRenderFinding is the one K8S-RENDER finding of a run in which kustomize
// was not launched.
func onlyRenderFinding(t *testing.T, r *scriptedRunner, report *KubeconformReport) Finding {
	t.Helper()
	if got := builds(r); len(got) != 0 {
		t.Errorf("kustomize was launched %d time(s), want never: %q", len(got), got)
	}
	found := renderFindings(report)
	if len(found) != 1 {
		t.Fatalf("%d K8S-RENDER findings, want 1: %+v", len(found), report.Findings)
	}
	if len(report.Unrendered) != 0 {
		t.Errorf("Unrendered = %v: the overlay was refused, not skipped", report.Unrendered)
	}
	return found[0]
}

func checkRefusalShape(t *testing.T, f Finding, dir string) {
	t.Helper()
	if f.ID != K8sRenderID || f.Severity != SeverityHigh || f.Source != SourceKustomize || f.IaCType != "kustomize" {
		t.Errorf("finding = %s %s %s %s: a refusal is a K8S-RENDER finding, HIGH, from kustomize", f.ID, f.Severity, f.Source, f.IaCType)
	}
	if want := dir + "/kustomization.yaml"; f.File != want {
		t.Errorf("File = %q, want the overlay's own %q", f.File, want)
	}
	// A copied `kustomize build` would run what the gate refused to run.
	if f.Repro != "" {
		t.Errorf("Repro = %q, want none", f.Repro)
	}
	if len(f.Steps) == 0 || len(stepProblems(f.Steps)) > 0 {
		t.Errorf("Steps = %q (%v), want steps that follow the convention", f.Steps, stepProblems(f.Steps))
	}
}

// ── A reference that is not local ────────────────────────────────────────────

func TestANativeKustomizeIsNotLaunchedOnAnOverlayThatNamesAnotherHost(t *testing.T) {
	for _, c := range gateRemoteCases {
		if !c.wire {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			isolateHome(t)
			repo := writeRepo(t, c.files)
			r := gateRunner(t, "")

			report := runGate(t, repo, nativeKustomize, false)

			f := onlyRenderFinding(t, r, report)
			checkRefusalShape(t, f, c.overlayDir())
			if f.Title != remoteBlockedHeadline {
				t.Errorf("Title = %q, want %q", f.Title, remoteBlockedHeadline)
			}
			if f.Resolution != remoteBlockedResolution {
				t.Errorf("Resolution = %q, want the one of a blocked remote base", f.Resolution)
			}
			if want := renderSteps(SourceKustomize, c.overlayDir(), ToolSourceBinary, "", true); !slices.Equal(f.Steps, want) {
				t.Errorf("Steps = %q, want the steps of a blocked remote base %q", f.Steps, want)
			}
			// The message names where, which field and what, each as %q.
			for _, want := range []string{c.file, c.field, c.ref} {
				if want != "" && !strings.Contains(f.Message, fmt.Sprintf("%q", want)) {
					t.Errorf("Message = %q, want it to name %q", f.Message, want)
				}
			}
		})
	}
}

// Every form of reference the whitelist refuses ends the same way, through the
// public path: the loopback of the story, an address of the whole 127/8 block,
// a name, an ssh or git form, an absolute path.
func TestEveryNonLocalFormIsRefusedBeforeKustomizeRuns(t *testing.T) {
	for _, ref := range remoteRefs {
		t.Run(ref, func(t *testing.T) {
			isolateHome(t)
			repo := writeRepo(t, ovl("resources:\n  - "+q(ref)+"\n"))
			r := gateRunner(t, "")

			f := onlyRenderFinding(t, r, runGate(t, repo, nativeKustomize, false))

			if f.Title != remoteBlockedHeadline || !strings.Contains(f.Message, fmt.Sprintf("%q", ref)) {
				t.Errorf("title %q, message %q: want %q and the reference as %%q", f.Title, f.Message, remoteBlockedHeadline)
			}
		})
	}
}

// ── An overlay the gate cannot judge ─────────────────────────────────────────

func TestAnOverlayWhoseReferencesCannotBeCheckedIsNotBuilt(t *testing.T) {
	for _, c := range gateUncheckedCases {
		if !c.wire {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			isolateHome(t)
			repo := writeRepo(t, c.files)
			r := gateRunner(t, "")

			f := onlyRenderFinding(t, r, runGate(t, repo, nativeKustomize, false))

			checkUncheckedFinding(t, f, c.overlayDir(), c.file)
		})
	}
}

func checkUncheckedFinding(t *testing.T, f Finding, dir, file string) {
	t.Helper()
	checkRefusalShape(t, f, dir)
	if f.Title != uncheckedTitle {
		t.Errorf("Title = %q, want %q", f.Title, uncheckedTitle)
	}
	if file != "" && !strings.Contains(f.Message, fmt.Sprintf("%q", file)) {
		t.Errorf("Message = %q, want it to name the file %q", f.Message, file)
	}
	// Dedicated Steps (uncheckedOverlaySteps): not the ones of a build that
	// failed, not the ones of a blocked remote base, and they name the overlay.
	for what, other := range map[string][]string{
		"a failed build":        renderSteps(SourceKustomize, dir, ToolSourceBinary, "boom", false),
		"a blocked remote base": renderSteps(SourceKustomize, dir, ToolSourceBinary, "", true),
	} {
		if slices.Equal(f.Steps, other) {
			t.Errorf("Steps are the ones of %s: %q", what, f.Steps)
		}
	}
	if !strings.Contains(strings.Join(f.Steps, " "), dir) {
		t.Errorf("Steps = %q, want them to name the overlay %q", f.Steps, dir)
	}
	if f.Resolution == "" || f.Resolution == remoteBlockedResolution || f.Resolution == kustomizeRenderResolution {
		t.Errorf("Resolution = %q, want one of its own", f.Resolution)
	}
}

// A symbolic link is followed when it stays inside the limit, and refused as
// unjudgeable when it leaves it, whatever is on the other side.
func TestASymbolicLinkOutOfTheLimitIsNotFollowed(t *testing.T) {
	isolateHome(t)
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "kustomization.yaml"), "resources:\n  - "+q(loopbackURL)+"\n")
	repo := writeRepo(t, ovl("resources:\n  - link\n"))
	if err := os.Symlink(outside, filepath.Join(repo, "overlay", "link")); err != nil {
		t.Skipf("no symbolic link here: %v", err)
	}
	r := gateRunner(t, "")

	f := onlyRenderFinding(t, r, runGate(t, repo, nativeKustomize, false))

	checkUncheckedFinding(t, f, "overlay", "")
}

// ── An overlay that is all local ─────────────────────────────────────────────

// A local overlay is built as it always was: same program, same arguments, same
// directory, same environment (the cut network), whatever fields it uses.
func TestAnOverlayWithOnlyLocalReferencesIsBuiltAsBefore(t *testing.T) {
	for _, tc := range []struct {
		name string
		tool ToolSpec
	}{
		{"plain", nativeKustomize},
		{"a configured binary and the user's arguments",
			ToolSpec{Source: ToolSourceBinary, Binary: "/opt/k/kustomize", Args: []string{"--load-restrictor=LoadRestrictionsNone"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateHome(t)
			repo := writeRepo(t, gateLocalFiles())
			layout, err := k8s.Discover(repo)
			if err != nil || len(layout.Kustomizations) == 0 {
				t.Fatalf("Discover = %+v, %v: the fixture has no overlay", layout, err)
			}
			r := gateRunner(t, "")

			report := runGate(t, repo, tc.tool, false)

			if f := renderFindings(report); len(f) != 0 {
				t.Errorf("a local overlay got findings: %+v", f)
			}
			got := builds(r)
			if len(got) != len(layout.Kustomizations) {
				t.Fatalf("%d builds, want one per overlay %v: %q", len(got), layout.Kustomizations, got)
			}
			for i, dir := range layout.Kustomizations {
				want := kustomizeArgs(targetOnly(repo).cut(), dir, tc.tool, false)
				if got[i].Name != want.Name || !slices.Equal(got[i].Args, want.Args) || got[i].Dir != want.Dir {
					t.Errorf("%s: ran %s %q in %q, want %s %q in %q", dir, got[i].Name, got[i].Args, got[i].Dir, want.Name, want.Args, want.Dir)
				}
				if got[i].Env == nil || !slices.Equal(got[i].Env, want.Env) {
					t.Errorf("%s: Env differs from the cut network's", dir)
				}
				checkOffline(t, got[i].Env)
			}
		})
	}
}

// ── What does not go through the gate ────────────────────────────────────────

// With allow_remote_bases the user said the repository is trusted: the gate is not
// asked (so no git either, for a binary), kustomize runs as it did, whole
// environment included.
func TestAllowRemoteBasesDoesNotAskTheGate(t *testing.T) {
	isolateHome(t)
	repo := writeRepo(t, ovl("resources:\n  - "+q(loopbackURL)+"\n"))
	r := gateRunner(t, "")

	report := runGate(t, repo, nativeKustomize, true)

	if f := renderFindings(report); len(f) != 0 {
		t.Errorf("findings = %+v, want none: the build was allowed and answered", f)
	}
	got := builds(r)
	if len(got) != 1 {
		t.Fatalf("%d builds, want 1: %q", len(got), got)
	}
	want := kustomizeArgs(targetOnly(repo), "overlay", nativeKustomize, false)
	if got[0].Name != want.Name || !slices.Equal(got[0].Args, want.Args) || got[0].Dir != want.Dir {
		t.Errorf("ran %s %q in %q, want %s %q in %q", got[0].Name, got[0].Args, got[0].Dir, want.Name, want.Args, want.Dir)
	}
	if got[0].Env != nil {
		t.Errorf("Env = %q, want nil: the network was not cut", got[0].Env)
	}
	if calls := gitCalls(r); len(calls) != 0 {
		t.Errorf("git was asked (%q): the gate must not be, nor its limit computed", calls)
	}
}

// From an image the network is cut by the engine (`--network none`): nothing to
// gate, and the argv is what it was.
func TestAnImageKustomizeIsNotGated(t *testing.T) {
	isolateHome(t)
	noProxyEnvironment(t)
	useProxy(t, forwardOf(fullProxy))
	forEachEngine(t, func(t *testing.T, _ string) {
		repo := writeRepo(t, ovl("resources:\n  - "+q(loopbackURL)+"\n"))
		r := gateRunner(t, "")
		image := ToolSpec{Source: ToolSourceContainer}

		report := runGate(t, repo, image, false)

		if f := renderFindings(report); len(f) != 0 {
			t.Errorf("findings = %+v, want none: the engine cuts the network, the gate is not asked", f)
		}
		got := builds(r)
		if len(got) != 1 {
			t.Fatalf("%d builds, want 1: %q", len(got), got)
		}
		want := kustomizeArgs(targetOnly(repo).cut(), "overlay", image, false)
		if got[0].Name != want.Name || !slices.Equal(got[0].Args, want.Args) || !slices.Equal(got[0].Env, want.Env) {
			t.Errorf("ran %s %q, want %s %q", got[0].Name, got[0].Args, want.Name, want.Args)
		}
		if !networkNoneBefore(got[0].Args, DefaultKustomizeImage) {
			t.Errorf("no `--network none` before the image: %q", got[0].Args)
		}
	})
}

// ── The limit of the paths ───────────────────────────────────────────────────

// The gate follows a reference that leaves the scanned directory when it stays
// inside the git repository holding it, and refuses what leaves the repository.
// Without git the limit is the scanned directory itself.
func TestTheGateLimitIsTheGitRepositoryNotTheTarget(t *testing.T) {
	// repo/deploy/base           carries the URL
	// repo/deploy/overlays/prod  resources: [<ref>]      (the scanned directory is repo/deploy/overlays)
	tests := []struct {
		name string
		ref  string
		git  bool
		// want is the title of the only finding.
		want string
	}{
		{"a base beside the target, git repository", "../../base", true, remoteBlockedHeadline},
		{"the same base, no git: the target is the limit", "../../base", false, uncheckedTitle},
		{"a path beyond the repository, git repository", "../../../../outside", true, uncheckedTitle},
		{"a path beyond the target, no git", "../../../outside", false, uncheckedTitle},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			repo := filepath.Join(base, "repo")
			useHome(t, filepath.Join(base, "home"), nil)
			mkdirs(t, filepath.Join(repo, ".git"))
			writeFile(t, filepath.Join(repo, "deploy/base/kustomization.yaml"), "resources:\n  - "+q(loopbackURL)+"\n")
			writeFile(t, filepath.Join(repo, "deploy/overlays/prod/kustomization.yaml"), "resources:\n  - "+q(tt.ref)+"\n")
			writeFile(t, filepath.Join(base, "outside/kustomization.yaml"), "resources:\n  - "+q(loopbackURL)+"\n")
			gitRoot := ""
			if tt.git {
				gitRoot = repo
			}
			r := gateRunner(t, gitRoot)

			f := onlyRenderFinding(t, r, runGate(t, filepath.Join(repo, "deploy", "overlays"), nativeKustomize, false))

			if f.Title != tt.want {
				t.Errorf("Title = %q, want %q\n%s", f.Title, tt.want, f.Message)
			}
		})
	}
}
