package scan

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// DEVDESK-771: what the closed whitelist adds to kustomize_gate_test.go. A chart
// name is a reference (helm pulls what an oci:// name says), and a kustomization
// file that is a link out of the limit is not read.

func TestVetOverlayRefusesAChartNamedByAnAddress(t *testing.T) {
	const oci = "oci://127.0.0.1:5000/charts/c"
	for name, tc := range map[string]struct {
		files map[string]string
		field string
	}{
		"helmCharts":                  {ovl("helmCharts:\n  - name: " + q(oci) + "\n"), "helmCharts[0].name"},
		"helmChartInflationGenerator": {ovl("helmChartInflationGenerator:\n  - chartName: " + q(oci) + "\n"), "helmChartInflationGenerator[0].chartName"},
		"an inline plugin":            {ovl(inline("generators", plugin("HelmChartInflationGenerator", "name: "+q(oci)))), "generators[0].name"},
	} {
		t.Run(name, func(t *testing.T) {
			checkVerdict(t, vet(t, tc.files, "overlay", defaultGateLimits), gateRemote, overlayFile, tc.field, oci)
		})
	}
}

func TestVetOverlayDoesNotReadAKustomizationFileThatLeavesTheRoot(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "kustomization.yaml")
	writeFile(t, outside, "resources: []\n")
	root := writeRepo(t, map[string]string{"overlay/cm.yaml": "kind: ConfigMap\n"})
	if err := os.Symlink(outside, filepath.Join(root, "overlay", "kustomization.yaml")); err != nil {
		t.Skipf("no symbolic link here: %v", err)
	}

	got := vetOverlay(t.Context(), root, filepath.Join(root, "overlay"), defaultGateLimits)

	checkVerdict(t, got, gateUnchecked, overlayFile, "", "")
}

// A generators, transformers or validators entry that names a directory is not
// followed: kustomize reads the resources of its kustomization as plugin
// configurations (and, with --enable-helm or --enable-exec, runs what they say),
// where the gate would read them as resources.
func pluginDirectoryCases(url string) map[string]map[string]string {
	return map[string]map[string]string{
		"A, a builtin patch whose path is an address": ovl("transformers:\n  - ../plug\n",
			"plug/kustomization.yaml", "resources:\n  - t.yaml\n",
			"plug/t.yaml", plugin("PatchTransformer", "path: "+q(url))),
		"B, a helm chart from an address": ovl("generators:\n  - ../plug\n",
			"plug/kustomization.yaml", "resources:\n  - g.yaml\n",
			"plug/g.yaml", plugin("HelmChartInflationGenerator", "name: c", "repo: "+q(url))),
		"C, a plugin that is not builtin and runs a function": ovl("validators:\n  - ../plug\n",
			"plug/kustomization.yaml", "resources:\n  - v.yaml\n",
			"plug/v.yaml", "apiVersion: example.com/v1\nkind: V\nmetadata:\n  name: v\n  annotations:\n"+
				"    config.kubernetes.io/function: |\n      exec:\n        path: /bin/sh\n"),
	}
}

func TestVetOverlayDoesNotFollowAPluginDirectory(t *testing.T) {
	for name, files := range pluginDirectoryCases(loopbackURL) {
		t.Run(name, func(t *testing.T) {
			got := vet(t, files, "overlay", defaultGateLimits)

			checkVerdict(t, got, gateUnchecked, overlayFile, "", "")
			if got.Field == "" || got.Ref != "../plug" {
				t.Errorf("verdict = %+v, want the field and the reference of the directory", got)
			}
		})
	}
}

// The overlays above are not started: with the real kustomize and a server that
// counts, nothing is sent, and the witness run (allow_remote_bases) shows the
// server would have been reached.
func TestARealKustomizeIsNotStartedOnAPluginDirectory(t *testing.T) {
	kustomize := kustomizeWithGit(t)
	if _, err := exec.LookPath("kubeconform"); err != nil {
		t.Skip("no kubeconform on PATH")
	}
	isolateHome(t)
	for _, name := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy"} {
		t.Setenv(name, "")
	}
	hits, port := countingServer(t, "127.0.0.1:0")
	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/A-patch.yaml"
	repo := writeRepo(t, pluginDirectoryCases(url)["A, a builtin patch whose path is an address"])
	run := func(allow bool) *KubeconformReport {
		report, err := RunKubeconform(t.Context(), repo, ToolSpec{Source: ToolSourceBinary, Binary: "kubeconform"},
			KubeconformOptions{Kustomize: ToolSpec{Source: ToolSourceBinary, Binary: kustomize}, AllowRemoteBases: allow}, nil)
		// The witness run may fail to start kubeconform (absent on CI) after
		// kustomize already reached the server: only the gated run must succeed.
		if err != nil && !allow {
			t.Fatalf("RunKubeconform: %v", err)
		}
		return report
	}

	found := renderFindings(run(false))

	if n := hits.Load(); n != 0 {
		t.Errorf("the server got %d request(s), want 0", n)
	}
	if len(found) != 1 || found[0].Title != uncheckedTitle {
		t.Errorf("findings = %+v, want one %q", found, uncheckedTitle)
	}
	run(true)
	if hits.Load() < 1 {
		t.Fatal("with allow_remote_bases kustomize did not reach the server either: this test cannot see a request here")
	}
}

// A base shared by several overlays of one scan is read once, and an overlay is
// remembered only when all of it was judged local.
func TestTheGateRemembersWhatItJudgedLocalAcrossOverlays(t *testing.T) {
	root := writeRepo(t, map[string]string{
		"a/kustomization.yaml":    "resources:\n  - ../base\n",
		"b/kustomization.yaml":    "resources:\n  - ../base\n",
		"base/kustomization.yaml": "resources: []\n",
	})
	run := gateRun{cache: &gateCache{}}

	first := vetOverlayIn(t.Context(), root, filepath.Join(root, "a"), defaultGateLimits, run)
	// The base is gone from disk: only the memory of the first overlay can say it is fine.
	if err := os.WriteFile(filepath.Join(root, "base", "kustomization.yaml"), []byte("resources: [&a x]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	second := vetOverlayIn(t.Context(), root, filepath.Join(root, "b"), defaultGateLimits, run)
	uncached := vetOverlay(t.Context(), root, filepath.Join(root, "b"), defaultGateLimits)

	checkVerdict(t, first, gateOK, "", "", "")
	checkVerdict(t, second, gateOK, "", "", "")
	checkVerdict(t, uncached, gateUnchecked, "base/kustomization.yaml", "", "")
}

func TestTheGateDoesNotRememberAnOverlayThatFailed(t *testing.T) {
	root := writeRepo(t, map[string]string{
		"a/kustomization.yaml": "resources:\n  - ../b\n  - " + q(loopbackURL) + "\n",
		"b/kustomization.yaml": "resources:\n  - ../a\n",
		"c/kustomization.yaml": "resources:\n  - ../b\n",
	})
	run := gateRun{cache: &gateCache{}}

	failed := vetOverlayIn(t.Context(), root, filepath.Join(root, "a"), defaultGateLimits, run)
	again := vetOverlayIn(t.Context(), root, filepath.Join(root, "c"), defaultGateLimits, run)

	checkVerdict(t, failed, gateRemote, "a/kustomization.yaml", "resources[1]", loopbackURL)
	checkVerdict(t, again, gateRemote, "a/kustomization.yaml", "resources[1]", loopbackURL)
}

func TestTheGateNamesItsLimitAndClipsWhatTheRepositoryWrites(t *testing.T) {
	root := writeRepo(t, ovl("resources:\n  - ../../outside\n"))

	scanned := vetOverlayIn(t.Context(), root, filepath.Join(root, "overlay"), defaultGateLimits, gateRun{limit: "the scanned folder"})
	repo := vetOverlay(t.Context(), root, filepath.Join(root, "overlay"), defaultGateLimits)

	if !strings.Contains(scanned.Reason, "the scanned folder") || !strings.Contains(repo.Reason, "the repository") {
		t.Errorf("reasons = %q, %q, want the scanned folder, then the repository", scanned.Reason, repo.Reason)
	}
	long := strings.Repeat("é", 500)
	msg := remoteMessage(gateVerdict{Kind: gateRemote, File: "f", Field: long, Ref: long, Reason: long})
	if n := utf8.RuneCountInString(msg); n > 4*(clipLen+1)+200 {
		t.Errorf("the message has %d characters, want the long names cut at %d", n, clipLen)
	}
	if !strings.Contains(msg, "…") {
		t.Errorf("message = %q, want an ellipsis", msg)
	}
}

func TestAnEmptyKustomizationIsSaidToBeEmpty(t *testing.T) {
	for name, content := range map[string]string{"empty": "", "comments": "# nothing here\n"} {
		t.Run(name, func(t *testing.T) {
			got := vet(t, ovl(content), "overlay", defaultGateLimits)

			checkVerdict(t, got, gateUnchecked, overlayFile, "", "")
			if got.Reason != "the kustomization file is empty" {
				t.Errorf("Reason = %q", got.Reason)
			}
		})
	}
}
