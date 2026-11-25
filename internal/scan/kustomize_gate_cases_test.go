package scan

import (
	"strconv"
	"strings"
)

// DEVDESK-771: the native kustomize gate. Go never applies a proxy to the
// loopback, so offlineEnviron cannot stop a native kustomize that reads a
// `kustomization.yaml` naming http://127.0.0.1:8080/x.yaml from sending a GET.
// A closed whitelist (kustomize_gate.go) reads the overlay first and refuses
// to launch kustomize unless every reference it can find is local.
//
// This file is data only: the cases that kustomize_gate_wiring_test.go runs
// through RunKubeconform and kustomize_gate_test.go (build tag gate_red) runs
// through vetOverlay. It names no symbol of the gate, so it always compiles.

// loopbackURL is the reference of every remote case: a GET to it is what the gate
// must prevent.
const loopbackURL = "http://127.0.0.1:8080/x.yaml"

// gateCase is one overlay and what the gate must say about it.
type gateCase struct {
	name string
	// files is the repository, relative to its root, slash separated.
	files map[string]string
	// overlay is the directory the gate is asked about, relative to the root;
	// "overlay" when empty.
	overlay string
	// file, field and ref are the expected verdict. file is relative to the
	// root, slash separated. field is the YAML path of the offending entry, with
	// list indices (resources[1], patches[0].path, openapi.path). ref is the value
	// handed to localRef, "" to leave it unchecked.
	file, field, ref string
	// wire marks a case the end to end tests also run: a single leaf overlay, so
	// that k8s.Discover finds one overlay and the render finding is unique.
	wire bool
}

func (c gateCase) overlayDir() string {
	if c.overlay == "" {
		return "overlay"
	}
	return c.overlay
}

// ovl is a repository whose overlay holds kustomization (and the other files, as
// path, content pairs).
func ovl(kustomization string, extra ...string) map[string]string {
	files := map[string]string{"overlay/kustomization.yaml": kustomization}
	for i := 0; i+1 < len(extra); i += 2 {
		files[extra[i]] = extra[i+1]
	}
	return files
}

// q is s as a YAML double quoted scalar, so that no reference is read as anything
// but a string.
func q(s string) string { return strconv.Quote(s) }

// plugin is a builtin plugin configuration with these lines after its header.
func plugin(kind string, lines ...string) string {
	return "apiVersion: builtin\nkind: " + kind + "\nmetadata:\n  name: p\n" + strings.Join(lines, "\n") + "\n"
}

// inline is text as the one entry of the list under key, written as a block scalar:
// the way an inline plugin configuration is given.
func inline(key, text string) string {
	var b strings.Builder
	b.WriteString(key + ":\n  - |\n")
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		b.WriteString("    " + line + "\n")
	}
	return b.String()
}

const overlayFile = "overlay/kustomization.yaml"

// gateRemoteCases: a reference that is not local, in every field of kustomize
// 5.8.3 that carries one (resources, bases, components, crds, configurations,
// openapi, the patches, the replacements, the generators' files, the helm fields,
// and the generators, transformers and validators in every form).
var gateRemoteCases = []gateCase{
	// ── the plain fields ──
	{name: "resources", wire: true, files: ovl("resources:\n  - ok.yaml\n  - " + q(loopbackURL) + "\n"),
		file: overlayFile, field: "resources[1]", ref: loopbackURL},
	{name: "bases", wire: true, files: ovl("bases:\n  - " + q(loopbackURL) + "\n"),
		file: overlayFile, field: "bases[0]", ref: loopbackURL},
	{name: "components", wire: true, files: ovl("components:\n  - " + q(loopbackURL) + "\n"),
		file: overlayFile, field: "components[0]", ref: loopbackURL},
	{name: "crds", files: ovl("crds:\n  - " + q(loopbackURL) + "\n"),
		file: overlayFile, field: "crds[0]", ref: loopbackURL},
	{name: "configurations", files: ovl("configurations:\n  - " + q(loopbackURL) + "\n"),
		file: overlayFile, field: "configurations[0]", ref: loopbackURL},
	{name: "openapi.path", files: ovl("openapi:\n  path: " + q(loopbackURL) + "\n"),
		file: overlayFile, field: "openapi.path", ref: loopbackURL},
	{name: "patches[].path", files: ovl("patches:\n  - path: " + q(loopbackURL) + "\n    target:\n      kind: Deployment\n"),
		file: overlayFile, field: "patches[0].path", ref: loopbackURL},
	{name: "patchesJson6902[].path", files: ovl("patchesJson6902:\n  - target:\n      group: apps\n      version: v1\n" +
		"      kind: Deployment\n      name: web\n    path: " + q(loopbackURL) + "\n"),
		file: overlayFile, field: "patchesJson6902[0].path", ref: loopbackURL},
	{name: "patchesStrategicMerge[]", files: ovl("patchesStrategicMerge:\n  - local.yaml\n  - " + q(loopbackURL) + "\n"),
		file: overlayFile, field: "patchesStrategicMerge[1]", ref: loopbackURL},
	{name: "replacements[].path", files: ovl("replacements:\n  - path: " + q(loopbackURL) + "\n"),
		file: overlayFile, field: "replacements[0].path", ref: loopbackURL},

	// ── the generators of configMaps and secrets ──
	{name: "configMapGenerator files, key=path", files: ovl("configMapGenerator:\n  - name: c\n    files:\n      - k=" + loopbackURL + "\n"),
		file: overlayFile, field: "configMapGenerator[0].files[0]", ref: loopbackURL},
	{name: "configMapGenerator files, bare path", files: ovl("configMapGenerator:\n  - name: c\n    files:\n      - " + q(loopbackURL) + "\n"),
		file: overlayFile, field: "configMapGenerator[0].files[0]", ref: loopbackURL},
	{name: "configMapGenerator envs", files: ovl("configMapGenerator:\n  - name: c\n    envs:\n      - " + q(loopbackURL) + "\n"),
		file: overlayFile, field: "configMapGenerator[0].envs[0]", ref: loopbackURL},
	{name: "configMapGenerator env", files: ovl("configMapGenerator:\n  - name: c\n    env: " + q(loopbackURL) + "\n"),
		file: overlayFile, field: "configMapGenerator[0].env", ref: loopbackURL},
	{name: "secretGenerator files", files: ovl("secretGenerator:\n  - name: s\n    files:\n      - k=" + loopbackURL + "\n"),
		file: overlayFile, field: "secretGenerator[0].files[0]", ref: loopbackURL},
	{name: "secretGenerator envs", files: ovl("secretGenerator:\n  - name: s\n    envs:\n      - " + q(loopbackURL) + "\n"),
		file: overlayFile, field: "secretGenerator[0].envs[0]", ref: loopbackURL},
	{name: "secretGenerator env", files: ovl("secretGenerator:\n  - name: s\n    env: " + q(loopbackURL) + "\n"),
		file: overlayFile, field: "secretGenerator[0].env", ref: loopbackURL},
	{name: "the second generator and the second file", files: ovl("configMapGenerator:\n  - name: a\n    files: [ok.txt]\n" +
		"  - name: b\n    files:\n      - ok.txt\n      - k=" + loopbackURL + "\n"),
		file: overlayFile, field: "configMapGenerator[1].files[1]", ref: loopbackURL},

	// ── helm ──
	{name: "helmGlobals.chartHome", files: ovl("helmGlobals:\n  chartHome: " + q(loopbackURL) + "\n"),
		file: overlayFile, field: "helmGlobals.chartHome", ref: loopbackURL},
	{name: "helmGlobals.configHome", files: ovl("helmGlobals:\n  configHome: " + q(loopbackURL) + "\n"),
		file: overlayFile, field: "helmGlobals.configHome", ref: loopbackURL},
	{name: "helmCharts[].repo", files: ovl("helmCharts:\n  - name: c\n    repo: " + q("https://127.0.0.1:8443/charts") + "\n"),
		file: overlayFile, field: "helmCharts[0].repo", ref: "https://127.0.0.1:8443/charts"},
	{name: "helmCharts[].valuesFile", files: ovl("helmCharts:\n  - name: c\n    valuesFile: " + q(loopbackURL) + "\n"),
		file: overlayFile, field: "helmCharts[0].valuesFile", ref: loopbackURL},
	{name: "helmCharts[].additionalValuesFiles", files: ovl("helmCharts:\n  - name: c\n    additionalValuesFiles:\n      - local.yaml\n      - " + q(loopbackURL) + "\n"),
		file: overlayFile, field: "helmCharts[0].additionalValuesFiles[1]", ref: loopbackURL},
	{name: "helmCharts[].chartHome", files: ovl("helmCharts:\n  - name: c\n    chartHome: " + q(loopbackURL) + "\n"),
		file: overlayFile, field: "helmCharts[0].chartHome", ref: loopbackURL},
	{name: "helmChartInflationGenerator[].chartRepoUrl", files: ovl("helmChartInflationGenerator:\n  - chartName: c\n    chartRepoUrl: " + q("https://127.0.0.1:8443/charts") + "\n"),
		file: overlayFile, field: "helmChartInflationGenerator[0].chartRepoUrl", ref: "https://127.0.0.1:8443/charts"},
	{name: "helmChartInflationGenerator[].helmBin", files: ovl("helmChartInflationGenerator:\n  - chartName: c\n    helmBin: /usr/bin/helm\n"),
		file: overlayFile, field: "helmChartInflationGenerator[0].helmBin", ref: "/usr/bin/helm"},
	{name: "helmChartInflationGenerator[].extraArgs", files: ovl("helmChartInflationGenerator:\n  - chartName: c\n    extraArgs:\n      - --debug\n"),
		file: overlayFile, field: "helmChartInflationGenerator[0].extraArgs"},

	// ── generators, transformers and validators: inline configuration ──
	{name: "transformers inline PatchTransformer", files: ovl(inline("transformers", plugin("PatchTransformer", "path: "+q(loopbackURL)))),
		file: overlayFile, field: "transformers[0].path", ref: loopbackURL},
	{name: "transformers inline PatchJson6902Transformer", files: ovl(inline("transformers", plugin("PatchJson6902Transformer", "path: "+q(loopbackURL)))),
		file: overlayFile, field: "transformers[0].path", ref: loopbackURL},
	{name: "transformers inline PatchStrategicMergeTransformer", files: ovl(inline("transformers", plugin("PatchStrategicMergeTransformer", "paths:", "  - ok.yaml", "  - "+q(loopbackURL)))),
		file: overlayFile, field: "transformers[0].paths[1]", ref: loopbackURL},
	{name: "transformers inline ReplacementTransformer", files: ovl(inline("transformers", plugin("ReplacementTransformer", "replacements:", "  - path: "+q(loopbackURL)))),
		file: overlayFile, field: "transformers[0].replacements[0].path", ref: loopbackURL},
	{name: "transformers inline ValueAddTransformer", files: ovl(inline("transformers", plugin("ValueAddTransformer", "targetFilePath: "+q(loopbackURL), "value: x"))),
		file: overlayFile, field: "transformers[0].targetFilePath", ref: loopbackURL},
	{name: "generators inline ConfigMapGenerator files", files: ovl(inline("generators", plugin("ConfigMapGenerator", "files:", "  - k="+loopbackURL))),
		file: overlayFile, field: "generators[0].files[0]", ref: loopbackURL},
	{name: "generators inline SecretGenerator envs", files: ovl(inline("generators", plugin("SecretGenerator", "envs:", "  - "+q(loopbackURL)))),
		file: overlayFile, field: "generators[0].envs[0]", ref: loopbackURL},
	{name: "generators inline SecretGenerator env", files: ovl(inline("generators", plugin("SecretGenerator", "env: "+q(loopbackURL)))),
		file: overlayFile, field: "generators[0].env", ref: loopbackURL},
	{name: "generators inline HelmChartInflationGenerator repo", files: ovl(inline("generators", plugin("HelmChartInflationGenerator", "name: c", "repo: "+q("https://127.0.0.1:8443/charts")))),
		file: overlayFile, field: "generators[0].repo", ref: "https://127.0.0.1:8443/charts"},
	{name: "validators inline PatchTransformer", files: ovl(inline("validators", plugin("PatchTransformer", "path: "+q(loopbackURL)))),
		file: overlayFile, field: "validators[0].path", ref: loopbackURL},

	// ── generators, transformers and validators: a file, a directory ──
	{name: "transformers, a plugin file", wire: true,
		files: ovl("transformers:\n  - tx.yaml\n", "overlay/tx.yaml", plugin("PatchTransformer", "path: "+q(loopbackURL))),
		file:  "overlay/tx.yaml", field: "path", ref: loopbackURL},
	{name: "generators, a plugin file", wire: true,
		files: ovl("generators:\n  - gen.yaml\n", "overlay/gen.yaml", plugin("ConfigMapGenerator", "files:", "  - k="+loopbackURL)),
		file:  "overlay/gen.yaml", field: "files[0]", ref: loopbackURL},
	{name: "validators, a plugin file", wire: true,
		files: ovl("validators:\n  - ../val/v.yaml\n", "val/v.yaml", plugin("PatchStrategicMergeTransformer", "paths:", "  - "+q(loopbackURL))),
		file:  "val/v.yaml", field: "paths[0]", ref: loopbackURL},
	// ── a local base that carries the URL ──
	{name: "a base", wire: true, files: ovl("resources:\n  - ../base\n", "base/kustomization.yaml", "resources:\n  - "+q(loopbackURL)+"\n"),
		file: "base/kustomization.yaml", field: "resources[0]", ref: loopbackURL},
	{name: "a base two levels down", wire: true,
		files: ovl("resources:\n  - ../a\n", "a/kustomization.yaml", "bases:\n  - ../b\n", "b/kustomization.yaml", "resources:\n  - ok.yaml\n  - "+q(loopbackURL)+"\n"),
		file:  "b/kustomization.yaml", field: "resources[1]", ref: loopbackURL},
	{name: "a component", wire: true,
		files: ovl("components:\n  - ../comp\n", "comp/kustomization.yaml", "apiVersion: kustomize.config.k8s.io/v1alpha1\nkind: Component\npatches:\n  - path: "+q(loopbackURL)+"\n"),
		file:  "comp/kustomization.yaml", field: "patches[0].path", ref: loopbackURL},
	{name: "a base reached by a resource that is a directory with a dot in its name", wire: true,
		files: ovl("resources:\n  - ./v1.2/base\n", "overlay/v1.2/base/kustomization.yaml", "resources:\n  - "+q(loopbackURL)+"\n"),
		file:  "overlay/v1.2/base/kustomization.yaml", field: "resources[0]", ref: loopbackURL},
}

// remoteRefs are references no overlay may name, each one in resources: the form
// of the loopback the story is about, and every other form localRef refuses.
var remoteRefs = []string{
	"http://127.0.0.1:8080/x.yaml", "https://127.0.0.1/x.yaml", "http://localhost:9/x.yaml", "http://[::1]:9/x.yaml",
	"http://127.0.0.2:9/x.yaml", "http://x.localhost:9/x.yaml", "HTTP://127.0.0.1/x.yaml",
	"git@github.com:o/r", "ssh://git@h/o/r", "file:///tmp/r", "git::https://h/o/r",
	"github.com/o/r", "GitHub.com/o/r//s?ref=v1", "u@h/x", "/abs/overlay", `\\srv\s`, `C:\x`, "x?ref=1",
}

// gateUncheckedCases: overlays the gate cannot judge, so kustomize is not launched
// either. field and ref stay empty; file is the file that could not be judged
// when the case says one.
var gateUncheckedCases = []gateCase{
	{name: "an anchor", wire: true, files: ovl("resources:\n  - &a base\n  - ok.yaml\n"), file: overlayFile},
	{name: "an alias", files: ovl("x: &a [base]\nresources: *a\n"), file: overlayFile},
	{name: "a merge key", files: ovl("patches:\n  - &p\n    path: p.yaml\n  - <<: *p\n"), file: overlayFile},
	{name: "an explicit tag", files: ovl("resources: !!seq\n  - base\n"), file: overlayFile},
	{name: "a custom tag", files: ovl("resources:\n  - !custom base\n"), file: overlayFile},
	{name: "a duplicate key", files: ovl("resources:\n  - a\nresources:\n  - " + loopbackURL + "\n"), file: overlayFile},
	{name: "a duplicate key, case ignored", wire: true, files: ovl("resources:\n  - a\nResources:\n  - b\n"), file: overlayFile},
	{name: "a duplicate key inside an entry", files: ovl("patches:\n  - path: a.yaml\n    path: b.yaml\n"), file: overlayFile},
	{name: "a key that is not ASCII (a Cyrillic e)", files: ovl("r\u0435sources:\n  - " + loopbackURL + "\n"), file: overlayFile},
	{name: "two documents", files: ovl("resources:\n  - a\n---\nresources:\n  - b\n"), file: overlayFile},
	{name: "invalid YAML", files: ovl("resources: [a\n"), file: overlayFile},
	{name: "a top-level key kustomize does not know", wire: true, files: ovl("frobnicate:\n  - x\n"), file: overlayFile},
	{name: "a reference that is not a string (a mapping)", files: ovl("resources:\n  - {url: " + loopbackURL + "}\n"), file: overlayFile},
	{name: "a reference that is not a string (a number)", files: ovl("resources:\n  - 5\n"), file: overlayFile},
	{name: "two kustomization files in a directory", wire: true,
		files: ovl("resources: []\n", "overlay/kustomization.yml", "resources: []\n")},
	{name: "a kustomization file that is a directory", files: map[string]string{"overlay/kustomization.yaml/x": ""}, file: overlayFile},
	{name: "a base that holds an anchor",
		files: ovl("resources:\n  - ../base\n", "base/kustomization.yaml", "x: &a [1]\nresources: [ok.yaml]\n"), file: "base/kustomization.yaml"},
	{name: "a path that leaves the limit", wire: true, files: ovl("resources:\n  - ../../outside\n")},
	{name: "a path that leaves the limit, through a patch file", files: ovl("patches:\n  - path: ../../outside/p.yaml\n")},
	{name: "a plugin of another apiVersion, inline",
		files: ovl(inline("transformers", "apiVersion: example.com/v1\nkind: Label\nmetadata:\n  name: p\n")), file: overlayFile},
	{name: "a plugin without apiVersion, inline",
		files: ovl(inline("transformers", "kind: PatchTransformer\nmetadata:\n  name: p\npath: p.yaml\n")), file: overlayFile},
	{name: "a builtin plugin of a kind the gate does not know, inline",
		files: ovl(inline("generators", plugin("Frobnicator"))), file: overlayFile},
	{name: "a function annotation (kubernetes.io), inline",
		files: ovl(inline("transformers", "apiVersion: builtin\nkind: PatchTransformer\nmetadata:\n  name: p\n  annotations:\n"+
			"    config.kubernetes.io/function: |\n      exec:\n        path: /bin/sh\npath: p.yaml\n")), file: overlayFile},
	{name: "a function annotation (k8s.io), inline",
		files: ovl(inline("validators", "apiVersion: builtin\nkind: PatchTransformer\nmetadata:\n  name: p\n  annotations:\n"+
			"    config.k8s.io/function: |\n      container:\n        image: x\npath: p.yaml\n")), file: overlayFile},
	{name: "a plugin file of another apiVersion",
		files: ovl("generators:\n  - gen.yaml\n", "overlay/gen.yaml", "apiVersion: example.com/v1\nkind: Gen\nmetadata:\n  name: g\n"), file: "overlay/gen.yaml"},
	{name: "a plugin file with an anchor",
		files: ovl("transformers:\n  - tx.yaml\n", "overlay/tx.yaml", plugin("PatchTransformer", "x: &a 1", "path: p.yaml")), file: "overlay/tx.yaml"},
	// A directory named as a generator, transformer or validator is never followed:
	// kustomize would read plugin configurations in it that the gate cannot vouch
	// for, whatever its kustomization says. The verdict is on the entry itself.
	{name: "transformers, a directory with a clean kustomization",
		files: ovl("transformers:\n  - ../tx\n", "tx/kustomization.yaml", "resources: []\n"), file: overlayFile, field: "transformers[0]"},
	{name: "generators, a directory",
		files: ovl("generators:\n  - gen\n", "overlay/gen/kustomization.yaml", "resources: []\n"), file: overlayFile, field: "generators[0]"},
	{name: "validators, a directory",
		files: ovl("validators:\n  - ../val\n", "val/kustomization.yaml", "resources: []\n"), file: overlayFile, field: "validators[0]"},
	{name: "transformers, a directory that carries a remote reference",
		files: ovl("transformers:\n  - ../tx\n", "tx/kustomization.yaml", "patches:\n  - path: "+q(loopbackURL)+"\n"), file: overlayFile, field: "transformers[0]"},
	// An ordinary base is still followed, and an anchor in its kustomization is refused.
	{name: "a component that holds an anchor",
		files: ovl("components:\n  - ../comp\n", "comp/kustomization.yaml", "x: &a 1\n"), file: "comp/kustomization.yaml"},
}

// gateLocalFiles is an overlay that uses every field of kustomize 5.8.3 that
// carries a reference, with a local reference everywhere, and data (a URL in a
// literal, an inline patch, an image name with a port) where a URL is not a
// reference. The gate must say OK, and kustomize is launched exactly as before.
func gateLocalFiles() map[string]string {
	return map[string]string{
		"overlay/kustomization.yaml": `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
metadata:
  name: local
namespace: demo
namePrefix: p-
nameSuffix: -s
commonLabels:
  app: demo
commonAnnotations:
  team: x
labels:
  - pairs:
      tier: web
images:
  - name: nginx
    newName: localhost:5000/nginx
    newTag: "1.27"
replicas:
  - name: web
    count: 2
generatorOptions:
  disableNameSuffixHash: true
vars:
  - name: SVC
    objref:
      kind: Service
      name: s
      apiVersion: v1
sortOptions:
  order: fifo
buildMetadata: [managedByLabel]
resources:
  - deploy.yaml
  - ../base
  - ./v1.2/dir
  - ./github.com/o/r
  - a b/c.yaml
bases:
  - ../base2
components:
  - ../comp
crds:
  - crd.yaml
configurations:
  - cfg.yaml
openapi:
  path: schema.json
patches:
  - path: patch.yaml
    target:
      kind: Deployment
  - target:
      kind: Deployment
    patch: |-
      - op: replace
        path: /spec/template/spec/containers/0/image
        value: http://127.0.0.1:8080/an-inline-patch-is-data
patchesJson6902:
  - target:
      group: apps
      version: v1
      kind: Deployment
      name: web
    path: json.yaml
patchesStrategicMerge:
  - sm.yaml
  - |-
    apiVersion: apps/v1
    kind: Deployment
    metadata:
      name: web
      annotations:
        note: http://127.0.0.1:8080/an-inline-patch-is-data
  - apiVersion: v1
    kind: ConfigMap
    metadata:
      name: inline-mapping
      annotations:
        note: http://127.0.0.1:8080/an-inline-mapping-is-data
replacements:
  - path: repl.yaml
  - source:
      kind: ConfigMap
      name: c1
      fieldPath: data.k
    targets:
      - select:
          kind: Deployment
        fieldPaths:
          - spec.template.metadata.labels.k
configMapGenerator:
  - name: c1
    files:
      - key=data.txt
      - plain.txt
    envs:
      - a.env
    env: b.env
    literals:
      - url=http://127.0.0.1:8080/a-literal-is-data
secretGenerator:
  - name: s1
    files:
      - key=secret.txt
    envs:
      - c.env
    env: d.env
helmGlobals:
  chartHome: charts
  configHome: helmcfg
helmCharts:
  - name: c
    valuesFile: values.yaml
    additionalValuesFiles:
      - extra.yaml
    chartHome: charts
helmChartInflationGenerator:
  - chartName: c
generators:
  - gen.yaml
  - |
    apiVersion: builtin
    kind: SecretGenerator
    metadata:
      name: inline-gen
    files:
      - k=local.txt
    envs:
      - e.env
transformers:
  - tx.yaml
  - |
    apiVersion: builtin
    kind: ReplacementTransformer
    metadata:
      name: inline-repl
    replacements:
      - path: repl2.yaml
validators:
  - val.yaml
`,
		"overlay/deploy.yaml":                       "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\n  annotations:\n    a: &x http://127.0.0.1:8080/data\n    b: *x\n",
		"overlay/patch.yaml":                        "# a data file is never parsed: anchors, tags and URLs are its own business\nx: &a !!str http://127.0.0.1:8080/y\ny: *a\n",
		"overlay/gen.yaml":                          plugin("ConfigMapGenerator", "files:", "  - k=local.txt", "envs:", "  - a.env", "env: b.env"),
		"overlay/tx.yaml":                           plugin("PatchTransformer", "path: patch.yaml"),
		"overlay/val.yaml":                          plugin("PatchStrategicMergeTransformer", "paths:", "  - sm.yaml"),
		"overlay/v1.2/dir/kustomization.yaml":       "resources:\n  - cm.yaml\n",
		"overlay/github.com/o/r/kustomization.yaml": "resources:\n  - cm.yaml\n",
		"base/kustomization.yaml":                   "resources:\n  - cm.yaml\ncomponents:\n  - ../comp\n",
		"base/cm.yaml":                              "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: base\n",
		"base2/kustomization.yaml":                  "resources:\n  - cm.yaml\n",
		"comp/kustomization.yaml":                   "apiVersion: kustomize.config.k8s.io/v1alpha1\nkind: Component\npatches:\n  - path: p.yaml\n",
	}
}
