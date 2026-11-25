package scan

import (
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// DEVDESK-783: a Flux repository. An overlay such as kustomizations/dev lists, as
// `resources`, FILES of neighbouring folders (../../notes-infra/flux/dev/x.yaml,
// Flux Kustomizations). kustomize's default load restrictor refuses them, and the
// scan reported a K8S-RENDER HIGH for something Flux itself builds: Flux builds
// without the restriction, bounded to the root of the artifact (the root of the
// git repository).
//
// The wiring is "build again on refusal" (renderKustomizations, retryAsFlux):
// kustomize runs as it always did, after the DEVDESK-771 gate, which is unchanged
// and still comes first; only when the refusal is the load restrictor's AND the
// overlay is a Flux one (fluxOverlay) is kustomize run ONE more time, with
// `--load-restrictor=LoadRestrictionsNone` added by DevDesk after `build` and the
// user's arguments: `build <user args> --load-restrictor=LoadRestrictionsNone ./dir`.
//
// This file goes through RunKubeconform with a scripted runner (and, last, the
// real kustomize). The unit tests of fluxOverlay, of kustomizeArgs' fourth
// parameter and of userSetsLoadRestrictor are in kustomize_flux_detect_test.go,
// the named pipes in kustomize_flux_fifo_test.go.

const (
	// loadNone is the flag DevDesk adds, spelled as it is on the command line.
	loadNone = "--load-restrictor=LoadRestrictionsNone"

	// fluxNote is what a finding says when an overlay that was built as Flux builds
	// it still fails: the failure is not the restrictor's any more.
	fluxNote = "Built as Flux's kustomize-controller does: files outside the overlay folder allowed, within the repository"

	// fluxLog is the line a scan logs when it built an overlay as Flux does.
	fluxLog = "Flux repository detected, built without the load restrictor (bounded to the repository)"

	fluxKustomizationDoc = "apiVersion: kustomize.toolkit.fluxcd.io/v1\nkind: Kustomization\nmetadata:\n  name: notes-infra-dev\n" +
		"  namespace: flux-system\nspec:\n  interval: 10m\n  path: ./infra\n  prune: true\n  sourceRef:\n" +
		"    kind: GitRepository\n    name: flux-system\n"

	// A custom resource that is not Flux's: kubeconform skips it, offline too.
	widgetDoc    = "apiVersion: example.com/v1\nkind: Widget\nmetadata:\n  name: w\nspec:\n  size: 1\n"
	configMapDoc = "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: shared\ndata:\n  k: v\n"

	fluxOverlayDir = "kustomizations/dev"
	fluxNeighbour  = "notes-infra/flux/dev/notes-infra-dev.yaml"
)

// ── The scripted runner ──────────────────────────────────────────────────────

// buildAnswer is what `kustomize build` says.
type buildAnswer struct {
	out string
	err error
}

var (
	builtOK = buildAnswer{out: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: built\n"}
	// refusedByRestrictor is what a kustomize with the default restriction says.
	refusedByRestrictor = buildAnswer{err: &exitError{Code: 1, Stderr: loadRestrictorStderr}}
	// failsForAnotherReason is a failure that is not about the restrictor.
	failsForAnotherReason = buildAnswer{err: &exitError{Code: 1,
		Stderr: "Error: no matches for Id Deployment.v1.apps/web.[noNs]; failed to find unique target for patch"}}
)

// restrictedRunner answers `kustomize build` with plain when the argv does not
// hold loadNone and with flagged when it does; git names gitRoot ("" for no
// repository); everything else (kubeconform) answers with nothing to report.
func restrictedRunner(t *testing.T, gitRoot string, plain, flagged buildAnswer) *scriptedRunner {
	t.Helper()
	r := &scriptedRunner{}
	r.reply = func(tc toolCmd) ([]byte, error) {
		switch {
		case tc.Name == "git":
			if len(tc.Args) > 0 && tc.Args[0] == "config" {
				return nil, &exitError{Code: 1} // core.worktree is not set
			}
			if gitRoot == "" {
				return nil, &exitError{Code: 128, Stderr: "fatal: not a git repository"}
			}
			return []byte(gitRoot + "\n"), nil
		case subcommandOf(tc) == "build":
			a := plain
			if slices.Contains(tc.Args, loadNone) {
				a = flagged
			}
			return []byte(a.out), a.err
		}
		return []byte(`{"resources":[]}`), nil
	}
	useRunner(t, r)
	return r
}

// eachKustomize runs fn for a kustomize binary and for the kustomize image, on
// both engines.
func eachKustomize(t *testing.T, fn func(t *testing.T, tool ToolSpec)) {
	t.Helper()
	t.Run("binary", func(t *testing.T) { fn(t, nativeKustomize) })
	t.Run("image", func(t *testing.T) {
		noProxyEnvironment(t)
		forEachEngine(t, func(t *testing.T, _ string) { fn(t, ToolSpec{Source: ToolSourceContainer}) })
	})
}

// fluxFiles is a repository whose overlay lists a file of a neighbouring folder
// (outside, with its content) the way kustomizations/dev does in a Flux repository.
// prefix is the folder of the repository the scan targets ("" when it is the
// root): the neighbour is at the root of the repository, so above the target.
func fluxFiles(prefix, outside, content string) map[string]string {
	up := strings.Repeat("../", 2+len(strings.FieldsFunc(prefix, func(r rune) bool { return r == '/' })))
	return map[string]string{
		path.Join(prefix, fluxOverlayDir, "kustomization.yaml"): "resources:\n  - " + up + outside + "\n",
		outside: content,
	}
}

// fluxGitRepo is a git repository (a .git directory, git itself is scripted),
// whose root is neither the home directory nor above it.
func fluxGitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	isolateHome(t)
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(base, "repo")
	useHome(t, filepath.Join(base, "home"), nil)
	mkdirs(t, filepath.Join(repo, ".git"))
	for name, content := range files {
		writeFile(t, filepath.Join(repo, filepath.FromSlash(name)), content)
	}
	return repo
}

// twoBuilds checks that kustomize ran twice, the second time as the first with
// loadNone added right before the folder (so after the user's arguments), and
// returns both.
func twoBuilds(t *testing.T, r *scriptedRunner, dir string) (first, second toolCmd) {
	t.Helper()
	got := builds(r)
	if len(got) != 2 {
		t.Fatalf("%d builds, want 2 (the refusal, then the build as Flux does): %q", len(got), got)
	}
	first, second = got[0], got[1]
	if slices.Contains(first.Args, loadNone) {
		t.Errorf("the first build already holds %s: %q", loadNone, first.Args)
	}
	if last := first.Args[len(first.Args)-1]; last != "./"+dir {
		t.Fatalf("the first build ends with %q, want %q", last, "./"+dir)
	}
	want := slices.Insert(slices.Clone(first.Args), len(first.Args)-1, loadNone)
	if !slices.Equal(second.Args, want) {
		t.Errorf("the second build is\n %q\nwant the first with the flag before the folder:\n %q", second.Args, want)
	}
	if first.Name != second.Name || first.Dir != second.Dir || !slices.Equal(first.Env, second.Env) {
		t.Errorf("the second build changed the program, the directory or the environment: %s %q %q, was %s %q %q",
			second.Name, second.Dir, second.Env, first.Name, first.Dir, first.Env)
	}
	return first, second
}

// aboutTheRestrictor is the text of a finding a person reads.
func aboutTheRestrictor(f Finding) string { return f.Description + "\n" + f.Message }

// ── The overlay is built again ───────────────────────────────────────────────

// The case of the story: the restrictor refuses, the overlay is a Flux one, so
// kustomize is run once more with the flag, and nothing is reported. It works for
// a binary (which keeps its cut network and the gate) and for an image (which
// keeps `--network none`).
func TestAFluxOverlayRefusedByTheLoadRestrictorIsBuiltAgainWithoutIt(t *testing.T) {
	eachKustomize(t, func(t *testing.T, tool ToolSpec) {
		isolateHome(t)
		repo := writeRepo(t, fluxFiles("", fluxNeighbour, fluxKustomizationDoc))
		r := restrictedRunner(t, "", refusedByRestrictor, builtOK)
		logs := captureLog(t)

		report := runGate(t, repo, tool, false)

		_, second := twoBuilds(t, r, fluxOverlayDir)
		if f := report.Findings; len(f) != 0 {
			t.Errorf("findings = %+v, want none: Flux builds this overlay", f)
		}
		if len(report.Unrendered) != 0 {
			t.Errorf("Unrendered = %v", report.Unrendered)
		}
		line := `[scan/k8s] overlay "` + fluxOverlayDir + `": ` + fluxLog
		if !strings.Contains(logs.String(), line) {
			t.Errorf("the log does not say the overlay was built as Flux does, want a line with\n%s\n got:\n%s", line, logs)
		}
		if tool.Source == ToolSourceBinary {
			checkOffline(t, second.Env) // the retry does not give the network back
		} else if !networkNoneBefore(second.Args, DefaultKustomizeImage) {
			t.Errorf("no `--network none` before the image in the second build: %q", second.Args)
		}
	})
}

// The user's arguments stay where they were, right after `build`; the flag comes
// after them, before the folder. From a binary, as an exact argv.
func TestTheRetryPutsTheFlagAfterTheUsersArguments(t *testing.T) {
	isolateHome(t)
	repo := writeRepo(t, fluxFiles("", fluxNeighbour, fluxKustomizationDoc))
	r := restrictedRunner(t, "", refusedByRestrictor, builtOK)
	tool := ToolSpec{Source: ToolSourceBinary, Binary: "kustomize", Args: []string{"--enable-helm", "--helm-command", "helm"}}

	runGate(t, repo, tool, false)

	first, second := twoBuilds(t, r, fluxOverlayDir)
	if want := []string{"build", "--enable-helm", "--helm-command", "helm", "./" + fluxOverlayDir}; !slices.Equal(first.Args, want) {
		t.Errorf("first = %q, want %q (what it is today)", first.Args, want)
	}
	if want := []string{"build", "--enable-helm", "--helm-command", "helm", loadNone, "./" + fluxOverlayDir}; !slices.Equal(second.Args, want) {
		t.Errorf("second = %q, want %q", second.Args, want)
	}
}

// The restriction is lifted within the repository: a target that is a folder of a
// git repository (the neighbour is above it, in the repository) is the same case,
// for a binary, whose gate then has the repository as its limit, and for an image,
// which mounts the repository.
func TestAFluxOverlayOfAFolderOfAGitRepositoryIsBuiltAgainWithinTheRepository(t *testing.T) {
	const sub = "deployment"
	files := func() map[string]string { return fluxFiles(sub, fluxNeighbour, fluxKustomizationDoc) }

	t.Run("binary", func(t *testing.T) {
		repo := fluxGitRepo(t, files())
		r := restrictedRunner(t, repo, refusedByRestrictor, builtOK)
		logs := captureLog(t)

		report := runGate(t, filepath.Join(repo, sub), nativeKustomize, false)

		twoBuilds(t, r, fluxOverlayDir)
		if len(report.Findings) != 0 {
			t.Errorf("findings = %+v, want none", report.Findings)
		}
		if !strings.Contains(logs.String(), fluxLog) {
			t.Errorf("no log line about Flux in:\n%s", logs)
		}
	})
	t.Run("image", func(t *testing.T) {
		noProxyEnvironment(t)
		forEachEngine(t, func(t *testing.T, _ string) {
			repo := fluxGitRepo(t, files())
			r := restrictedRunner(t, repo, refusedByRestrictor, builtOK)

			report := runGate(t, filepath.Join(repo, sub), ToolSpec{Source: ToolSourceContainer}, false)

			first, second := twoBuilds(t, r, fluxOverlayDir)
			for _, tc := range []toolCmd{first, second} {
				if !slices.Contains(tc.Args, repo+":/scan:ro") || !slices.Contains(tc.Args, "/scan/"+sub) {
					t.Errorf("the repository is not mounted, or the target is not the working directory: %q", tc.Args)
				}
			}
			if len(report.Findings) != 0 {
				t.Errorf("findings = %+v, want none", report.Findings)
			}
		})
	})
}

// ── What does not start a second build ───────────────────────────────────────

// The gate of DEVDESK-771 is unchanged and comes first: an overlay it refuses is
// not built, a Flux one included, and nothing is built again.
func TestTheGateStillComesFirstOnAFluxOverlay(t *testing.T) {
	for name, kustomization := range map[string]struct{ body, title string }{
		"a reference to another host":    {"resources:\n  - ../../" + fluxNeighbour + "\n  - " + q(loopbackURL) + "\n", remoteBlockedHeadline},
		"a field the gate does not know": {"resources:\n  - ../../" + fluxNeighbour + "\nmadeUpField: x\n", uncheckedTitle},
	} {
		t.Run(name, func(t *testing.T) {
			isolateHome(t)
			files := fluxFiles("", fluxNeighbour, fluxKustomizationDoc)
			files[fluxOverlayDir+"/kustomization.yaml"] = kustomization.body
			repo := writeRepo(t, files)
			r := restrictedRunner(t, "", refusedByRestrictor, builtOK)

			f := onlyRenderFinding(t, r, runGate(t, repo, nativeKustomize, false))

			if f.Title != kustomization.title {
				t.Errorf("Title = %q, want %q", f.Title, kustomization.title)
			}
		})
	}
}

// What this change must not touch: an overlay that is not a Flux one is built
// once, and its finding is what it was, byte for byte.
func TestAnOverlayThatIsNotFluxIsNotBuiltAgain(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"a ConfigMap":                 fluxFiles("", "notes/infra.yaml", configMapDoc),
		"a custom resource":           fluxFiles("", "notes/infra.yaml", widgetDoc),
		"a group that only looks one": fluxFiles("", "notes/infra.yaml", strings.Replace(fluxKustomizationDoc, "kustomize.toolkit.fluxcd.io/v1", "kustomize.toolkit.fluxcd.io.evil.com/v1", 1)),
	} {
		t.Run(name, func(t *testing.T) {
			eachKustomize(t, func(t *testing.T, tool ToolSpec) {
				isolateHome(t)
				repo := writeRepo(t, files)
				r := restrictedRunner(t, "", refusedByRestrictor, builtOK)
				logs := captureLog(t)

				report := runGate(t, repo, tool, false)

				if got := builds(r); len(got) != 1 {
					t.Fatalf("%d builds, want 1: %q", len(got), got)
				}
				checkRestrictorFinding(t, report, tool)
				if strings.Contains(logs.String(), "Flux repository") {
					t.Errorf("the log speaks of Flux:\n%s", logs)
				}
			})
		})
	}
}

// checkRestrictorFinding checks the one finding of an overlay the restrictor
// refused and nothing built again: DEVDESK-742's, as it has always been.
func checkRestrictorFinding(t *testing.T, report *KubeconformReport, tool ToolSpec) {
	t.Helper()
	found := renderFindings(report)
	if len(found) != 1 {
		t.Fatalf("%d K8S-RENDER findings, want 1: %+v", len(found), report.Findings)
	}
	f := found[0]
	if f.Severity != SeverityHigh || f.Message != loadRestrictorStderr {
		t.Errorf("severity %s, message %q: the finding itself must not change", f.Severity, f.Message)
	}
	if want := "The directory could not be rendered, so none of its resources were validated."; f.Description != want {
		t.Errorf("Description = %q, want %q", f.Description, want)
	}
	if f.Resolution != loadRestrictorResolution {
		t.Errorf("Resolution = %q, want DEVDESK-742's, byte for byte", f.Resolution)
	}
	if !strings.HasPrefix(f.Title, "Overlay does not build: ") {
		t.Errorf("Title = %q", f.Title)
	}
	if want := []string{
		"Reference a directory that has its own kustomization.yaml, or",
		"move the file below " + fluxOverlayDir + ".",
	}; !slices.Equal(f.Steps, want) {
		t.Errorf("Steps = %q, want %q", f.Steps, want)
	}
	if tool.Source == ToolSourceBinary {
		if want := "kustomize build " + fluxOverlayDir; f.Repro != want || f.RanFrom != ToolSourceBinary {
			t.Errorf("Repro %q from %q, want %q from a binary", f.Repro, f.RanFrom, want)
		}
	}
	if strings.Contains(aboutTheRestrictor(f), "Flux") || strings.Contains(aboutTheRestrictor(f), "allow_remote_bases") {
		t.Errorf("the finding says something it did not:\n%s", aboutTheRestrictor(f))
	}
}

// A user who set the restrictor wrote what they want: DevDesk adds nothing and
// does not build again, in either spelling.
func TestTheUsersLoadRestrictorIsLeftAloneAndNothingIsBuiltAgain(t *testing.T) {
	for name, args := range map[string][]string{
		"the = form, the default":      {"--load-restrictor=LoadRestrictionsRootOnly"},
		"the = form, already none":     {loadNone},
		"a separate value":             {"--load-restrictor", "LoadRestrictionsRootOnly"},
		"among other arguments":        {"--enable-helm", "--load-restrictor=LoadRestrictionsRootOnly"},
		"the separate form, then more": {"--load-restrictor", "LoadRestrictionsRootOnly", "--enable-helm"},
	} {
		t.Run(name, func(t *testing.T) {
			isolateHome(t)
			repo := writeRepo(t, fluxFiles("", fluxNeighbour, fluxKustomizationDoc))
			r := restrictedRunner(t, "", refusedByRestrictor, refusedByRestrictor)
			tool := ToolSpec{Source: ToolSourceBinary, Binary: "kustomize", Args: args}

			report := runGate(t, repo, tool, false)

			got := builds(r)
			if len(got) != 1 {
				t.Fatalf("%d builds, want 1: %q", len(got), got)
			}
			if want := slices.Concat([]string{"build"}, args, []string{"./" + fluxOverlayDir}); !slices.Equal(got[0].Args, want) {
				t.Errorf("argv = %q, want %q: DevDesk added something", got[0].Args, want)
			}
			checkRestrictorFinding(t, report, tool)
		})
	}
}

// A Flux overlay that fails for a reason of its own is not built again, and its
// finding is what it was.
func TestAFluxOverlayThatFailsForAnotherReasonIsNotBuiltAgain(t *testing.T) {
	eachKustomize(t, func(t *testing.T, tool ToolSpec) {
		isolateHome(t)
		repo := writeRepo(t, fluxFiles("", fluxNeighbour, fluxKustomizationDoc))
		r := restrictedRunner(t, "", failsForAnotherReason, builtOK)

		report := runGate(t, repo, tool, false)

		if got := builds(r); len(got) != 1 {
			t.Fatalf("%d builds, want 1: %q", len(got), got)
		}
		found := renderFindings(report)
		if len(found) != 1 || found[0].Message != failsForAnotherReason.err.(*exitError).Stderr {
			t.Fatalf("findings = %+v, want the one failure, as kustomize said it", found)
		}
		if strings.Contains(aboutTheRestrictor(found[0]), "Flux") {
			t.Errorf("the finding speaks of Flux:\n%s", aboutTheRestrictor(found[0]))
		}
	})
}

// An overlay that is not a Flux one keeps the generic step: no flag in it.
func TestTheStepOfAnOverlayThatIsNotFluxHasNoFlag(t *testing.T) {
	eachKustomize(t, func(t *testing.T, tool ToolSpec) {
		isolateHome(t)
		repo := writeRepo(t, fluxFiles("", "notes/infra.yaml", configMapDoc))
		r := restrictedRunner(t, "", failsForAnotherReason, builtOK)

		report := runGate(t, repo, tool, false)

		if got := builds(r); len(got) != 1 {
			t.Fatalf("%d builds, want 1: %q", len(got), got)
		}
		found := renderFindings(report)
		if len(found) != 1 {
			t.Fatalf("%d K8S-RENDER findings, want 1: %+v", len(found), report.Findings)
		}
		if want := []string{"Run kustomize build " + fluxOverlayDir + " and fix the first error it reports."}; !slices.Equal(found[0].Steps, want) {
			t.Errorf("Steps = %q, want %q", found[0].Steps, want)
		}
	})
}

// A build that works is not built twice.
func TestAFluxOverlayThatBuildsIsBuiltOnce(t *testing.T) {
	isolateHome(t)
	repo := writeRepo(t, fluxFiles("", fluxNeighbour, fluxKustomizationDoc))
	r := restrictedRunner(t, "", builtOK, builtOK)

	report := runGate(t, repo, nativeKustomize, false)

	if got := builds(r); len(got) != 1 || slices.Contains(got[0].Args, loadNone) {
		t.Errorf("builds = %q, want one, without the flag", got)
	}
	if len(report.Findings) != 0 {
		t.Errorf("findings = %+v", report.Findings)
	}
}

// ── What still fails ─────────────────────────────────────────────────────────

// An overlay built as Flux builds it that still does not build is still a HIGH,
// of what the second build said, now with the sentence that says how it was
// built, and a Repro that is the command that was run.
func TestAFluxOverlayThatStillFailsIsReportedWithTheNoteAndTheFlag(t *testing.T) {
	eachKustomize(t, func(t *testing.T, tool ToolSpec) {
		isolateHome(t)
		repo := writeRepo(t, fluxFiles("", fluxNeighbour, fluxKustomizationDoc))
		r := restrictedRunner(t, "", refusedByRestrictor, failsForAnotherReason)

		report := runGate(t, repo, tool, false)

		twoBuilds(t, r, fluxOverlayDir)
		found := renderFindings(report)
		if len(found) != 1 {
			t.Fatalf("%d K8S-RENDER findings, want 1: %+v", len(found), report.Findings)
		}
		f := found[0]
		if f.ID != K8sRenderID || f.Severity != SeverityHigh || f.Source != SourceKustomize || f.File != fluxOverlayDir+"/kustomization.yaml" {
			t.Errorf("finding = %s %s %s %s: still a K8S-RENDER HIGH on the overlay", f.ID, f.Severity, f.Source, f.File)
		}
		if want := failsForAnotherReason.err.(*exitError).Stderr; f.Message != want {
			t.Errorf("Message = %q, want what the second build said: %q", f.Message, want)
		}
		if !strings.Contains(aboutTheRestrictor(f), fluxNote) {
			t.Errorf("neither the Description nor the Message says how it was built, want %q in:\n%s", fluxNote, aboutTheRestrictor(f))
		}
		if problems := stepProblems(f.Steps); len(f.Steps) == 0 || len(problems) > 0 {
			t.Errorf("Steps = %q (%v)", f.Steps, problems)
		}
		// The step is the command that was run: a person who types the plain one
		// would read the restrictor's refusal again.
		if want := "Run kustomize build " + loadNone + " " + fluxOverlayDir + " and fix the first error it reports."; len(f.Steps) == 0 || f.Steps[0] != want {
			t.Errorf("Steps = %q, want the first to be %q", f.Steps, want)
		}
		if tool.Source == ToolSourceBinary {
			if want := "kustomize build " + loadNone + " " + fluxOverlayDir; f.Repro != want || f.RanFrom != ToolSourceBinary {
				t.Errorf("Repro %q from %q, want %q from a binary: what was run", f.Repro, f.RanFrom, want)
			}
		} else if f.Repro != "" || f.RanFrom != ToolSourceContainer {
			t.Errorf("Repro %q from %q, want none from an image", f.Repro, f.RanFrom)
		}
	})
}

// DECISION (the user's): with allow_remote_bases a native kustomize is not gated,
// so DevDesk cannot tell what the overlay would read: the restriction is not
// lifted, the overlay is not built again, and the HIGH says why.
func TestAllowRemoteBasesKeepsTheLoadRestrictorOnAFluxOverlay(t *testing.T) {
	isolateHome(t)
	repo := writeRepo(t, fluxFiles("", fluxNeighbour, fluxKustomizationDoc))
	r := restrictedRunner(t, "", refusedByRestrictor, builtOK)
	logs := captureLog(t)

	report := runGate(t, repo, nativeKustomize, true)

	got := builds(r)
	if len(got) != 1 || slices.Contains(got[0].Args, loadNone) {
		t.Fatalf("builds = %q, want one, without the flag", got)
	}
	found := renderFindings(report)
	if len(found) != 1 {
		t.Fatalf("%d K8S-RENDER findings, want 1: %+v", len(found), report.Findings)
	}
	f := found[0]
	if f.Severity != SeverityHigh || f.Message != loadRestrictorStderr {
		t.Errorf("severity %s, message %q: the finding itself must not change", f.Severity, f.Message)
	}
	if !strings.Contains(aboutTheRestrictor(f), "allow_remote_bases") {
		t.Errorf("neither the Description nor the Message says why the restriction was kept (allow_remote_bases) in:\n%s", aboutTheRestrictor(f))
	}
	if strings.Contains(aboutTheRestrictor(f), fluxNote) || strings.Contains(logs.String(), fluxLog) {
		t.Errorf("the overlay was not built as Flux does, and the finding or the log says it was:\n%s\n%s", aboutTheRestrictor(f), logs)
	}
	if want := "kustomize build " + fluxOverlayDir; f.Repro != want {
		t.Errorf("Repro = %q, want %q", f.Repro, want)
	}
}

// From an image the engine cuts or does not cut the network and the mount is the
// limit: the decision above is about a native kustomize only, so the flag goes
// through with the network the user allowed.
func TestAllowRemoteBasesDoesNotStopAFluxOverlayFromAnImage(t *testing.T) {
	noProxyEnvironment(t)
	forEachEngine(t, func(t *testing.T, _ string) {
		isolateHome(t)
		repo := writeRepo(t, fluxFiles("", fluxNeighbour, fluxKustomizationDoc))
		r := restrictedRunner(t, "", refusedByRestrictor, builtOK)

		report := runGate(t, repo, ToolSpec{Source: ToolSourceContainer}, true)

		_, second := twoBuilds(t, r, fluxOverlayDir)
		if slices.Contains(second.Args, "--network") {
			t.Errorf("the network was cut although remote bases are allowed: %q", second.Args)
		}
		if len(report.Findings) != 0 {
			t.Errorf("findings = %+v, want none", report.Findings)
		}
	})
}

// An image sees what is mounted: the target when there is no git repository. A
// neighbour above the target is then out of reach, so the overlay is not a Flux
// one for DevDesk, whatever kustomize said, and the finding is what it was.
func TestAReferenceAboveTheTargetWithoutGitIsNotBuiltAgainFromAnImage(t *testing.T) {
	const sub = "deployment"
	for name, answer := range map[string]buildAnswer{
		"the restrictor":         refusedByRestrictor,
		"a path the mount hides": {err: &exitError{Code: 1, Stderr: containerMissingLocalStderr}},
	} {
		t.Run(name, func(t *testing.T) {
			noProxyEnvironment(t)
			forEachEngine(t, func(t *testing.T, _ string) {
				isolateHome(t)
				repo := writeRepo(t, fluxFiles(sub, fluxNeighbour, fluxKustomizationDoc))
				r := restrictedRunner(t, "", answer, builtOK)

				report := runGate(t, filepath.Join(repo, sub), ToolSpec{Source: ToolSourceContainer}, false)

				if got := builds(r); len(got) != 1 {
					t.Fatalf("%d builds, want 1: %q", len(got), got)
				}
				found := renderFindings(report)
				if len(found) != 1 || found[0].Severity != SeverityHigh {
					t.Fatalf("findings = %+v, want one HIGH", report.Findings)
				}
				if answer.err.(*exitError).Stderr == containerMissingLocalStderr &&
					!strings.Contains(found[0].Resolution, mountResolution) {
					t.Errorf("Resolution = %q, want the hint about the mount", found[0].Resolution)
				}
				if strings.Contains(aboutTheRestrictor(found[0]), "Flux") {
					t.Errorf("the finding speaks of Flux:\n%s", aboutTheRestrictor(found[0]))
				}
			})
		})
	}
}

// ── The real kustomize ───────────────────────────────────────────────────────

// realFluxRepo is a git repository with, under prefix:
//   - kustomizations/dev, an aggregator that lists the Flux files of two
//     neighbouring folders, which are at the root of the repository;
//   - plain/overlay, which lists a file of a neighbouring folder that is not Flux's.
func realFluxRepo(t *testing.T, prefix string) string {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(base, "repo")
	mkdirs(t, repo)
	if out, err := exec.CommandContext(t.Context(), "git", "init", "-q", repo).CombinedOutput(); err != nil { //nolint:gosec // git on PATH, a fresh temp directory
		t.Skipf("git init: %v: %s", err, out)
	}
	up := strings.Repeat("../", 2+len(strings.FieldsFunc(prefix, func(r rune) bool { return r == '/' })))
	files := map[string]string{
		path.Join(prefix, "kustomizations/dev/kustomization.yaml"): "resources:\n  - " + up + "notes-infra/flux/dev/notes-infra-dev.yaml\n  - " +
			up + "notes-api/flux/dev/notes-api-dev.yaml\n",
		"notes-infra/flux/dev/notes-infra-dev.yaml": fluxKustomizationDoc,
		"notes-api/flux/dev/notes-api-dev.yaml": "---\n# the API\n" + strings.Replace(fluxKustomizationDoc, "notes-infra-dev", "notes-api-dev", 1) +
			"---\napiVersion: helm.toolkit.fluxcd.io/v2\nkind: HelmRelease\nmetadata:\n  name: notes-api\n  namespace: flux-system\n" +
			"spec:\n  interval: 10m\n  chart:\n    spec:\n      chart: notes-api\n",
		path.Join(prefix, "plain/overlay/kustomization.yaml"): "resources:\n  - ../shared/widget.yaml\n",
		path.Join(prefix, "plain/shared/widget.yaml"):         widgetDoc,
	}
	for name, content := range files {
		writeFile(t, filepath.Join(repo, filepath.FromSlash(name)), content)
	}
	return repo
}

// The story, with the real kustomize (v5.8.3 or later) and the real kubeconform:
// the aggregator of a Flux repository builds with no finding, and Flux's own
// resources are counted as custom ones nobody validated; the overlay next to it
// that reaches outside its folder for something that is not Flux's is still
// refused by the restrictor, as it has always been. Both are bounded to the git
// repository, whether the scan targets its root or a folder of it.
func TestARealKustomizeBuildsAFluxAggregatorOverlay(t *testing.T) {
	kustomize := kustomizeWithGit(t)
	if _, err := exec.LookPath("kubeconform"); err != nil {
		t.Skip("no kubeconform on PATH")
	}
	isolateHome(t)
	for _, tt := range []struct {
		name   string
		prefix string
	}{
		{"the target is the repository", ""},
		{"the target is a folder of the repository", "deployment"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := realFluxRepo(t, tt.prefix)

			report, err := RunKubeconform(t.Context(), filepath.Join(repo, tt.prefix), ToolSpec{Source: ToolSourceBinary, Binary: "kubeconform"},
				KubeconformOptions{Kustomize: ToolSpec{Source: ToolSourceBinary, Binary: kustomize}}, nil)

			if err != nil {
				t.Fatalf("RunKubeconform: %v", err)
			}
			for _, f := range report.Findings {
				if f.File == "kustomizations/dev/kustomization.yaml" {
					t.Errorf("the Flux aggregator got a finding: %s %s\n%s", f.ID, f.Title, f.Message)
				}
			}
			found := renderFindings(report)
			if len(found) != 1 {
				t.Fatalf("%d K8S-RENDER findings, want 1 (the plain overlay): %+v", len(found), report.Findings)
			}
			f := found[0]
			if want := "plain/overlay/kustomization.yaml"; f.File != want { // relative to the target
				t.Errorf("File = %q, want %q", f.File, want)
			}
			if f.Severity != SeverityHigh || !isLoadRestrictor(f.Message) {
				t.Errorf("severity %s, message %q: want the load restrictor's refusal", f.Severity, f.Message)
			}
			if f.Resolution != loadRestrictorResolution || strings.Contains(aboutTheRestrictor(f), fluxNote) {
				t.Errorf("the finding of an overlay that is not Flux's changed:\nResolution: %s\n%s", f.Resolution, aboutTheRestrictor(f))
			}
			// Only that some custom resource was seen and not validated: a catalog of
			// schemas that knew Flux would lower the count without any bug.
			if report.SkippedCustom < 1 {
				t.Errorf("SkippedCustom = %d, want at least 1", report.SkippedCustom)
			}
		})
	}
}
