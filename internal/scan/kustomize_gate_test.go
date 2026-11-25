package scan

// TODO(coder, DEVDESK-771): remove the `gate_red` build tag above (the first line of
// this file) as soon as internal/scan/kustomize_gate.go exists and defines the
// symbols below. The tag is the only way to have these tests written before the
// functions they call: without it the package does not compile, and every test
// of internal/scan, including the wiring tests that must run red on their own,
// would fail to build rather than fail for the right reason. Without the tag
// (the state of this commit), `go test ./internal/scan` ignores this file; with it,
// `go vet -tags gate_red ./internal/scan` says `undefined: localRef` until the
// gate exists. Once the tag is gone, also add the sample of the new Steps
// function to stepSamples in steps_guard_test.go:
//
//	"uncheckedOverlaySteps": {uncheckedOverlaySteps("overlays/dev", "an anchor in kustomization.yaml")},
//
// (TestEveryStepsFunctionHasASample fails until then.)
//
// The contract these tests pin (kustomize_gate.go, package scan):
//
//	type gateKind int // gateOK, gateRemote, gateUnchecked
//	type gateVerdict struct {
//		Kind                     gateKind
//		File, Field, Ref, Reason string
//	}
//	type gateLimits struct {
//		Depth, Visited         int   // levels of overlay below the first; distinct kustomizations read
//		FileBytes, TotalBytes int64 // one file; every file read for one overlay
//	}
//	var defaultGateLimits = gateLimits{Depth: 16, Visited: 256, FileBytes: 1 << 20, TotalBytes: 8 << 20}
//
//	// localRef: ok when ref can only name a path of this repository; reason is
//	// "" then, and says why not otherwise.
//	func localRef(ref string) (ok bool, reason string)
//	// readGateYAML reads one file of an overlay and returns the mapping at the
//	// top of its only document, or a reason it cannot be judged ("" and a
//	// non-nil node on success).
//	func readGateYAML(file string, maxBytes int64) (*yaml.Node, string)
//	// vetOverlay reads the overlay directory (absolute) and everything it pulls
//	// in, inside root (absolute; the gate resolves symbolic links itself). File
//	// is relative to root, with forward slashes; Field is the YAML path of the
//	// entry with list indices (resources[1], patches[0].path, openapi.path,
//	// configMapGenerator[0].files[0]); for an inline plugin configuration it is
//	// the field of the kustomization followed by the field of the plugin
//	// (transformers[0].path); for a plugin file it is the field inside that file
//	// (path, paths[0]). Ref is what localRef was given: the part after the
//	// first "=" of a files[] entry.
//	func vetOverlay(ctx context.Context, root, overlay string, lim gateLimits) gateVerdict

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ── localRef ─────────────────────────────────────────────────────────────────

func TestLocalRefAcceptsOnlyWhatCanNameAPathOfTheRepository(t *testing.T) {
	accepted := []string{
		"base", "../base", "./x.yaml", "v1.2/base", "./github.com/o/r", "a b/c.yaml", "x.yaml", "../../notes/infra.yaml",
		"overlays/prod", ".", "..",
	}
	for _, ref := range accepted {
		t.Run("accepts "+strconv.Quote(ref), func(t *testing.T) {
			if ok, reason := localRef(ref); !ok || reason != "" {
				t.Errorf("localRef(%q) = %v, %q, want true and no reason", ref, ok, reason)
			}
		})
	}
	refused := []string{
		"http://127.0.0.1:8080/x.yaml", "https://127.0.0.1/x.yaml", "http://localhost:9/x.yaml", "http://[::1]:9/x.yaml",
		"http://127.0.0.2:9/x.yaml", "http://x.localhost:9/x.yaml", "HTTP://127.0.0.1/x.yaml",
		"git@h:o/r", "ssh://h/o/r", "file:///tmp/r", "git::https://h/o/r",
		"github.com/o/r", "GitHub.com/o/r//s?ref=v1", "GITHUB.COM/o/r", "github.com",
		"u@h/x", "/abs", "/", `\\srv\s`, `\x`, `C:\x`, "x?ref=1", "x:y",
		"", "a\x00b", "a\nb", "a\tb", "a\rb", "a\x7fb", "a\x1bb",
	}
	for _, ref := range refused {
		t.Run("refuses "+strconv.Quote(ref), func(t *testing.T) {
			if ok, reason := localRef(ref); ok || reason == "" {
				t.Errorf("localRef(%q) = %v, %q, want false and a reason", ref, ok, reason)
			}
		})
	}
}

// ── readGateYAML ─────────────────────────────────────────────────────────────

func TestReadGateYAMLRefusesWhatItCannotJudge(t *testing.T) {
	big := "resources: []\n" + strings.Repeat("# padding\n", 100)
	tests := []struct {
		name    string
		content string
		limit   int64
	}{
		{"invalid YAML", "resources: [a\n", 0},
		{"an anchor", "resources:\n  - &a base\n", 0},
		{"an alias", "x: &a [base]\nresources: *a\n", 0},
		{"a merge key", "a: &a {path: p}\npatches:\n  - <<: *a\n", 0},
		{"a merge key alone", "patches:\n  - <<: {path: p}\n", 0},
		{"an explicit tag on a value", "resources: !!seq\n  - base\n", 0},
		{"an explicit tag on a scalar", "resources:\n  - !!str base\n", 0},
		{"a custom tag", "resources:\n  - !custom base\n", 0},
		{"a tag on a key", "!!str resources:\n  - base\n", 0},
		{"a duplicate key", "resources: [a]\nresources: [b]\n", 0},
		{"a duplicate key, case ignored", "resources: [a]\nResources: [b]\n", 0},
		{"a duplicate key, upper case", "resources: [a]\nRESOURCES: [b]\n", 0},
		{"a duplicate key inside an entry", "patches:\n  - path: a\n    path: b\n", 0},
		{"a duplicate key, case ignored, inside an entry", "patches:\n  - path: a\n    Path: b\n", 0},
		{"a key that is not ASCII (a Cyrillic e)", "r\u0435sources: [a]\n", 0},
		{"a key that is not ASCII, quoted", "\"resources\u00a0\": [a]\n", 0},
		{"a key that is not ASCII, inside an entry", "patches:\n  - p\u0430th: a\n", 0},
		{"two documents", "resources: [a]\n---\nresources: [b]\n", 0},
		{"a document after a comment document", "---\nresources: [a]\n---\n# nothing\n---\nresources: [b]\n", 0},
		{"a sequence at the top", "- resources\n", 0},
		{"a scalar at the top", "resources\n", 0},
		{"a file over the limit", big, 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "kustomization.yaml")
			writeFile(t, file, tt.content)
			limit := tt.limit
			if limit == 0 {
				limit = 1 << 20
			}

			node, reason := readGateYAML(file, limit)

			if node != nil || reason == "" {
				t.Errorf("readGateYAML = %v, %q, want no node and a reason", node, reason)
			}
		})
	}
}

func TestReadGateYAMLRefusesAFileThatIsNotThere(t *testing.T) {
	dir := t.TempDir()
	mkdirs(t, filepath.Join(dir, "adir"))
	for name, file := range map[string]string{
		"missing":   filepath.Join(dir, "nope.yaml"),
		"directory": filepath.Join(dir, "adir"),
	} {
		t.Run(name, func(t *testing.T) {
			if node, reason := readGateYAML(file, 1<<20); node != nil || reason == "" {
				t.Errorf("readGateYAML = %v, %q, want no node and a reason", node, reason)
			}
		})
	}
}

func TestReadGateYAMLRefusesAFileItCannotRead(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permissions are not enforced here")
	}
	file := filepath.Join(t.TempDir(), "kustomization.yaml")
	writeFile(t, file, "resources: [a]\n")
	if err := os.Chmod(file, 0); err != nil {
		t.Fatal(err)
	}

	if node, reason := readGateYAML(file, 1<<20); node != nil || reason == "" {
		t.Errorf("readGateYAML = %v, %q, want no node and a reason", node, reason)
	}
}

func TestReadGateYAMLReturnsTheTopMappingOfAnOrdinaryFile(t *testing.T) {
	tests := []struct {
		name, content string
	}{
		{"block style", "resources:\n  - base\n  - ./x.yaml\n"},
		{"flow style", "{resources: [base, ./x.yaml]}\n"},
		{"comments", "# an overlay\nresources:\n  - base # the base\n"},
		{"a document marker", "---\nresources:\n  - base\n"},
		{"quoted keys and values", "\"resources\":\n  - 'base'\n  - \"a b/c.yaml\"\n"},
		{"a block scalar", "patches:\n  - patch: |-\n      - op: add\n        path: /a\n        value: http://127.0.0.1:8080/data\n"},
		{"non-ASCII in a value", "namePrefix: \u00e9-\nresources:\n  - base\n"},
		{"a key that merely contains an angle bracket", "commonLabels:\n  \"a<<b\": c\n"},
		{"a long but plain file", "resources: []\n" + strings.Repeat("# padding\n", 50)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "kustomization.yaml")
			writeFile(t, file, tt.content)

			node, reason := readGateYAML(file, 1<<20)

			if reason != "" || node == nil || node.Kind != yaml.MappingNode {
				t.Errorf("readGateYAML = %+v, %q, want the top mapping and no reason", node, reason)
			}
		})
	}
}

// ── vetOverlay ───────────────────────────────────────────────────────────────

func TestDefaultGateLimits(t *testing.T) {
	want := gateLimits{Depth: 16, Visited: 256, FileBytes: 1 << 20, TotalBytes: 8 << 20}
	if defaultGateLimits != want {
		t.Errorf("defaultGateLimits = %+v, want %+v", defaultGateLimits, want)
	}
}

// vet writes files under a fresh root and asks the gate about overlay (relative to
// it). The root is handed over as the temporary directory names it.
func vet(t *testing.T, files map[string]string, overlay string, lim gateLimits) gateVerdict {
	t.Helper()
	root := writeRepo(t, files)
	return vetOverlay(t.Context(), root, filepath.Join(root, filepath.FromSlash(overlay)), lim)
}

func checkVerdict(t *testing.T, got gateVerdict, kind gateKind, file, field, ref string) {
	t.Helper()
	if got.Kind != kind {
		t.Fatalf("verdict = %+v, want kind %v", got, kind)
	}
	switch kind {
	case gateOK:
		if got.File != "" || got.Field != "" || got.Ref != "" || got.Reason != "" {
			t.Errorf("verdict = %+v, want nothing but the kind", got)
		}
	case gateRemote, gateUnchecked:
		if got.Reason == "" {
			t.Errorf("verdict = %+v, want a reason", got)
		}
		if file != "" && got.File != file {
			t.Errorf("File = %q, want %q", got.File, file)
		}
		if field != "" && got.Field != field {
			t.Errorf("Field = %q, want %q", got.Field, field)
		}
		if ref != "" && got.Ref != ref {
			t.Errorf("Ref = %q, want %q", got.Ref, ref)
		}
	}
}

// A reference that is not local, in each field of kustomize 5.8.3 that carries
// one, in each of the forms a generator, a transformer or a validator can take,
// directly or through a base: the verdict names the file, the field and the value.
func TestVetOverlayRefusesANonLocalReferenceInEveryField(t *testing.T) {
	for _, c := range gateRemoteCases {
		t.Run(c.name, func(t *testing.T) {
			got := vet(t, c.files, c.overlayDir(), defaultGateLimits)

			checkVerdict(t, got, gateRemote, c.file, c.field, c.ref)
			if got.File != c.file || got.Field != c.field {
				t.Errorf("File, Field = %q, %q, want exactly %q, %q", got.File, got.Field, c.file, c.field)
			}
		})
	}
}

func TestVetOverlayRefusesEveryNonLocalForm(t *testing.T) {
	for _, ref := range remoteRefs {
		t.Run(ref, func(t *testing.T) {
			got := vet(t, ovl("resources:\n  - "+q(ref)+"\n"), "overlay", defaultGateLimits)

			checkVerdict(t, got, gateRemote, overlayFile, "resources[0]", ref)
		})
	}
}

func TestVetOverlayCannotJudgeWhatItDoesNotKnow(t *testing.T) {
	for _, c := range gateUncheckedCases {
		t.Run(c.name, func(t *testing.T) {
			got := vet(t, c.files, c.overlayDir(), defaultGateLimits)

			checkVerdict(t, got, gateUnchecked, c.file, c.field, "")
		})
	}
}

// A key=path entry whose shape kustomize itself rejects (two equal signs) is not
// read as a local path by the gate either.
func TestVetOverlayDoesNotTrustAFilesEntryWithTwoEqualSigns(t *testing.T) {
	got := vet(t, ovl("configMapGenerator:\n  - name: c\n    files:\n      - a=b=c\n"), "overlay", defaultGateLimits)

	if got.Kind == gateOK {
		t.Errorf("verdict = %+v, want a refusal or an unjudgeable overlay", got)
	}
}

// The overlay is all local: every field, every form, data where a URL is not a
// reference, a base reached twice, and a plugin file that does not exist.
func TestVetOverlayAcceptsAnOverlayThatIsAllLocal(t *testing.T) {
	files := gateLocalFiles()
	for _, dir := range []string{"overlay", "base", "comp"} {
		t.Run(dir, func(t *testing.T) {
			checkVerdict(t, vet(t, files, dir, defaultGateLimits), gateOK, "", "", "")
		})
	}
}

// What is not there is kustomize's to report: it holds no reference to follow,
// and kustomize fetches nothing for a local path that is missing.
func TestVetOverlayLeavesAMissingLocalPathToKustomize(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
	}{
		{"a resource file that is missing", ovl("resources:\n  - ../notes/infra.yaml\n")},
		{"a base directory that is missing", ovl("resources:\n  - ../missing\n")},
		{"a base directory without a kustomization", ovl("resources:\n  - ../empty\n", "empty/cm.yaml", "kind: ConfigMap\n")},
		{"a plugin file that is missing", ovl("transformers:\n  - nope.yaml\n")},
		{"an empty overlay", ovl("resources: []\n")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkVerdict(t, vet(t, tt.files, "overlay", defaultGateLimits), gateOK, "", "", "")
		})
	}
}

// A builtin plugin whose references are local is fine, whatever the kind.
func TestVetOverlayAcceptsBuiltinPluginsWithLocalReferences(t *testing.T) {
	for name, cfg := range map[string]string{
		"ConfigMapGenerator":             plugin("ConfigMapGenerator", "files:", "  - k=data.txt", "envs:", "  - a.env", "env: b.env"),
		"SecretGenerator":                plugin("SecretGenerator", "files:", "  - data.txt", "envs:", "  - a.env"),
		"PatchTransformer":               plugin("PatchTransformer", "path: patch.yaml"),
		"PatchTransformer, inline patch": plugin("PatchTransformer", "patch: |-", "  - op: add", "    path: /a", "    value: http://127.0.0.1:8080/data"),
		"PatchJson6902Transformer":       plugin("PatchJson6902Transformer", "path: patch.json"),
		"PatchStrategicMergeTransformer": plugin("PatchStrategicMergeTransformer", "paths:", "  - sm.yaml"),
		"ReplacementTransformer":         plugin("ReplacementTransformer", "replacements:", "  - path: repl.yaml"),
		"ValueAddTransformer":            plugin("ValueAddTransformer", "targetFilePath: deploy.yaml", "value: x"),
		"HelmChartInflationGenerator":    plugin("HelmChartInflationGenerator", "name: c", "valuesFile: values.yaml", "chartHome: charts"),
	} {
		t.Run(name+", inline", func(t *testing.T) {
			checkVerdict(t, vet(t, ovl(inline("transformers", cfg)), "overlay", defaultGateLimits), gateOK, "", "", "")
		})
		t.Run(name+", a file", func(t *testing.T) {
			files := ovl("generators:\n  - plugin.yaml\n", "overlay/plugin.yaml", cfg)
			checkVerdict(t, vet(t, files, "overlay", defaultGateLimits), gateOK, "", "", "")
		})
	}
}

// ── The limit: symbolic links and `..` ───────────────────────────────────────

func TestVetOverlayDoesNotFollowASymbolicLinkOutOfTheRoot(t *testing.T) {
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "kustomization.yaml"), "resources:\n  - "+q(loopbackURL)+"\n")
	writeFile(t, filepath.Join(outside, "p.yaml"), "kind: ConfigMap\n")
	for name, files := range map[string]map[string]string{
		"a resource directory":   ovl("resources:\n  - link\n"),
		"a resource file":        ovl("resources:\n  - link/p.yaml\n"),
		"a patch file":           ovl("patches:\n  - path: link/p.yaml\n"),
		"a plugin directory":     ovl("transformers:\n  - link\n"),
		"a symbolic link, twice": ovl("resources:\n  - ../mid\n", "mid/kustomization.yaml", "resources:\n  - ../overlay/link\n"),
	} {
		t.Run(name, func(t *testing.T) {
			root := writeRepo(t, files)
			if err := os.Symlink(outside, filepath.Join(root, "overlay", "link")); err != nil {
				t.Skipf("no symbolic link here: %v", err)
			}

			got := vetOverlay(t.Context(), root, filepath.Join(root, "overlay"), defaultGateLimits)

			checkVerdict(t, got, gateUnchecked, "", "", "")
		})
	}
}

func TestVetOverlayFollowsASymbolicLinkThatStaysInsideTheRoot(t *testing.T) {
	root := writeRepo(t, ovl("resources:\n  - link\n", "base/kustomization.yaml", "resources:\n  - "+q(loopbackURL)+"\n"))
	if err := os.Symlink(filepath.Join(root, "base"), filepath.Join(root, "overlay", "link")); err != nil {
		t.Skipf("no symbolic link here: %v", err)
	}

	got := vetOverlay(t.Context(), root, filepath.Join(root, "overlay"), defaultGateLimits)

	checkVerdict(t, got, gateRemote, "base/kustomization.yaml", "resources[0]", loopbackURL)
}

// A root that is itself reached through a link is the same root.
func TestVetOverlayResolvesTheRootItself(t *testing.T) {
	real := writeRepo(t, ovl("resources:\n  - ../base\n", "base/kustomization.yaml", "resources: [cm.yaml]\n"))
	link := filepath.Join(t.TempDir(), "root")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("no symbolic link here: %v", err)
	}

	got := vetOverlay(t.Context(), link, filepath.Join(link, "overlay"), defaultGateLimits)

	checkVerdict(t, got, gateOK, "", "", "")
}

// ── The ceilings ─────────────────────────────────────────────────────────────

// chain is an overlay d0 that pulls in d1, which pulls in d2, ... n directories.
func chain(n int) map[string]string {
	files := map[string]string{}
	for i := 0; i < n; i++ {
		body := "resources:\n  - ok.yaml\n"
		if i+1 < n {
			body = fmt.Sprintf("resources:\n  - ../d%d\n", i+1)
		}
		files[fmt.Sprintf("d%d/kustomization.yaml", i)] = body
	}
	return files
}

// fan is an overlay that pulls in n distinct bases.
func fan(n int) map[string]string {
	var list strings.Builder
	files := map[string]string{}
	for i := 0; i < n; i++ {
		fmt.Fprintf(&list, "  - ../b%d\n", i)
		files[fmt.Sprintf("b%d/kustomization.yaml", i)] = "resources:\n  - ok.yaml\n"
	}
	files["d0/kustomization.yaml"] = "resources:\n" + list.String()
	return files
}

func padded(body string, n int) string {
	return body + strings.Repeat("#", n) + "\n"
}

func TestVetOverlayStopsAtItsCeilings(t *testing.T) {
	roomy := gateLimits{Depth: 16, Visited: 256, FileBytes: 1 << 20, TotalBytes: 1 << 30}
	tests := []struct {
		name  string
		files map[string]string
		lim   gateLimits
		want  gateKind
	}{
		{"a chain, default depth", chain(5), defaultGateLimits, gateOK},
		{"a chain far deeper than the default depth", chain(30), defaultGateLimits, gateUnchecked},
		{"a chain deeper than a small depth", chain(6), gateLimits{Depth: 2, Visited: 256, FileBytes: 1 << 20, TotalBytes: 1 << 30}, gateUnchecked},
		{"one level under a small depth", chain(2), gateLimits{Depth: 2, Visited: 256, FileBytes: 1 << 20, TotalBytes: 1 << 30}, gateOK},

		{"a hundred bases, default", fan(100), defaultGateLimits, gateOK},
		{"three hundred bases, default", fan(300), defaultGateLimits, gateUnchecked},
		{"ten bases, a small ceiling", fan(10), gateLimits{Depth: 16, Visited: 3, FileBytes: 1 << 20, TotalBytes: 1 << 30}, gateUnchecked},
		{"a base named ten times counts once", func() map[string]string {
			f := fan(1)
			f["d0/kustomization.yaml"] = "resources:\n" + strings.Repeat("  - ../b0\n", 10)
			return f
		}(), gateLimits{Depth: 16, Visited: 3, FileBytes: 1 << 20, TotalBytes: 1 << 30}, gateOK},
		{"a diamond counts its base once", map[string]string{
			"d0/kustomization.yaml":   "resources:\n  - ../a\n  - ../b\n",
			"a/kustomization.yaml":    "resources:\n  - ../base\n",
			"b/kustomization.yaml":    "resources:\n  - ../base\n",
			"base/kustomization.yaml": "resources:\n  - ok.yaml\n",
		}, gateLimits{Depth: 16, Visited: 4, FileBytes: 1 << 20, TotalBytes: 1 << 30}, gateOK},

		{"a cycle a to b to a is read once", map[string]string{
			"d0/kustomization.yaml": "resources:\n  - ../b\n",
			"b/kustomization.yaml":  "resources:\n  - ../d0\n",
		}, defaultGateLimits, gateOK},
		{"a directory that pulls in itself", map[string]string{
			"d0/kustomization.yaml": "resources:\n  - .\n",
		}, defaultGateLimits, gateOK},
		{"a cycle that also carries a remote base is still caught", map[string]string{
			"d0/kustomization.yaml": "resources:\n  - ../b\n",
			"b/kustomization.yaml":  "resources:\n  - ../d0\n  - " + q(loopbackURL) + "\n",
		}, defaultGateLimits, gateRemote},

		{"a file over its ceiling", map[string]string{"d0/kustomization.yaml": padded("resources: []\n", 200)},
			gateLimits{Depth: 16, Visited: 256, FileBytes: 64, TotalBytes: 1 << 30}, gateUnchecked},
		{"the same file under the default ceiling", map[string]string{"d0/kustomization.yaml": padded("resources: []\n", 200)},
			defaultGateLimits, gateOK},
		{"a five MiB file, default ceiling", map[string]string{"d0/kustomization.yaml": padded("resources: []\n", 5<<20)},
			defaultGateLimits, gateUnchecked},
		{"files that pass one by one and not together", map[string]string{
			"d0/kustomization.yaml": padded("resources:\n  - ../a\n  - ../b\n  - ../c\n", 300),
			"a/kustomization.yaml":  padded("resources: []\n", 300),
			"b/kustomization.yaml":  padded("resources: []\n", 300),
			"c/kustomization.yaml":  padded("resources: []\n", 300),
		}, gateLimits{Depth: 16, Visited: 256, FileBytes: 1000, TotalBytes: 1000}, gateUnchecked},
		{"the same files with room for all of them", map[string]string{
			"d0/kustomization.yaml": padded("resources:\n  - ../a\n  - ../b\n  - ../c\n", 300),
			"a/kustomization.yaml":  padded("resources: []\n", 300),
			"b/kustomization.yaml":  padded("resources: []\n", 300),
			"c/kustomization.yaml":  padded("resources: []\n", 300),
		}, roomy, gateOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := vet(t, tt.files, "d0", tt.lim)

			if got.Kind != tt.want {
				t.Errorf("verdict = %+v, want kind %v", got, tt.want)
			}
		})
	}
}

// A cancelled context never ends in OK: the gate does not guess what it did not read.
func TestVetOverlayChecksItsContext(t *testing.T) {
	root := writeRepo(t, chain(3))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	got := vetOverlay(ctx, root, filepath.Join(root, "d0"), defaultGateLimits)

	if got.Kind == gateOK {
		t.Errorf("verdict = %+v, want anything but OK for a cancelled context", got)
	}
}

func TestVetOverlayCannotJudgeAFileItCannotRead(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permissions are not enforced here")
	}
	root := writeRepo(t, ovl("resources:\n  - ../base\n", "base/kustomization.yaml", "resources: []\n"))
	if err := os.Chmod(filepath.Join(root, "base", "kustomization.yaml"), 0); err != nil {
		t.Fatal(err)
	}

	got := vetOverlay(t.Context(), root, filepath.Join(root, "overlay"), defaultGateLimits)

	checkVerdict(t, got, gateUnchecked, "base/kustomization.yaml", "", "")
}
