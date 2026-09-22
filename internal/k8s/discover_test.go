package k8s

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// writeTree creates files under a temporary root and returns the root.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const deployment = "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api\n"

// A manifest is recognised by what it says, not by where it sits or what it
// is called: a CI file and a compose file are YAML too, and validating them
// against the Kubernetes schema would report every line of them.
func TestDiscoverKeepsOnlyManifests(t *testing.T) {
	root := writeTree(t, map[string]string{
		"deploy/api.yaml":         deployment,
		"deploy/multi.yml":        "foo: bar\n---\n" + deployment,
		"docker-compose.yml":      "services:\n  api:\n    image: api\n",
		".gitlab-ci.yml":          "stages: [build]\n",
		".github/workflows/x.yml": deployment, // hidden directories are not the repository's manifests
		"node_modules/pkg/x.yaml": deployment,
		"broken.yaml":             "apiVersion: [\n",
	})

	layout, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"deploy/api.yaml", "deploy/multi.yml"}
	if !reflect.DeepEqual(layout.Manifests, want) {
		t.Errorf("Manifests = %v, want %v", layout.Manifests, want)
	}
}

// A template is not YAML until helm renders it, so a chart's files are never
// plain manifests; and a subchart is rendered by its parent, not on its own.
func TestDiscoverSetsChartsApart(t *testing.T) {
	root := writeTree(t, map[string]string{
		"charts/api/Chart.yaml":                         "apiVersion: v2\nname: api\n",
		"charts/api/templates/deploy.yaml":              deployment,
		"charts/api/charts/redis/Chart.yaml":            "apiVersion: v2\nname: redis\n",
		"charts/api/charts/redis/templates/deploy.yaml": deployment,
		"plain.yaml": deployment,
	})

	layout, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}

	if want := []string{"charts/api"}; !reflect.DeepEqual(layout.Charts, want) {
		t.Errorf("Charts = %v, want %v", layout.Charts, want)
	}
	if want := []string{"plain.yaml"}; !reflect.DeepEqual(layout.Manifests, want) {
		t.Errorf("Manifests = %v, want %v", layout.Manifests, want)
	}
}

// Only the overlays are rendered: a base is validated through the overlays
// that use it. Files under a Kustomize root are fragments until kustomize has
// merged them, so none of them is a plain manifest.
func TestDiscoverRendersOnlyTheKustomizeLeaves(t *testing.T) {
	root := writeTree(t, map[string]string{
		"k8s/base/kustomization.yaml":         "resources:\n  - deploy.yaml\n",
		"k8s/base/deploy.yaml":                deployment,
		"k8s/overlays/prod/kustomization.yml": "resources:\n  - ../../base\n  - https://example.com/remote\n",
		"k8s/overlays/prod/patch.yaml":        deployment,
		"k8s/overlays/dev/Kustomization":      "bases:\n  - ../../base\n",
	})

	layout, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"k8s/overlays/dev", "k8s/overlays/prod"}
	if !reflect.DeepEqual(layout.Kustomizations, want) {
		t.Errorf("Kustomizations = %v, want %v", layout.Kustomizations, want)
	}
	if len(layout.Manifests) != 0 {
		t.Errorf("Manifests = %v, want none: every file is under a Kustomize root", layout.Manifests)
	}
}
