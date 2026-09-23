package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// loadScan writes a context file holding this `scan:` block and loads it the
// way the application does.
func loadScan(t *testing.T, scan string) *Config {
	t.Helper()
	setupTmpHome(t)
	dir, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "contexts"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path, err := GetContextPath("default")
	if err != nil {
		t.Fatalf("GetContextPath: %v", err)
	}
	if err := os.WriteFile(path, []byte("scan:\n"+scan), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := LoadContext("default")
	if err != nil {
		t.Fatalf("LoadContext: %v", err)
	}
	return cfg
}

// Every flat key lands in its tool's block.
func TestEveryLegacyScanKeyIsCarriedOver(t *testing.T) {
	cfg := loadScan(t, `
  trivy_source: image
  trivy_path: /opt/trivy
  trivy_image: mirror/trivy
  use_trivy_server: true
  trivy_server: https://trivy:4954
  ignore_unfixed: true
  ignore_eol: true
  gitleaks_source: binary
  gitleaks_path: /opt/gitleaks
  gitleaks_image: mirror/gitleaks
  gitleaks_config: /etc/gitleaks.toml
  gitleaks_history: true
  plumber_source: image
  plumber_path: /opt/plumber
  plumber_image: mirror/plumber
  plumber_config: /etc/plumber.yaml
  kubeconform_source: binary
  kubeconform_path: /opt/kubeconform
  kubeconform_image: mirror/kubeconform
  kubernetes_version: 1.31.0
  helm_source: image
  helm_path: /opt/helm
  helm_image: mirror/helm
  kustomize_source: binary
  kustomize_path: /opt/kustomize
  kustomize_image: mirror/kustomize
  enable_vuln: true
`)
	tools := cfg.Scan.Tools
	check := func(name string, got, want ToolConfig) {
		t.Helper()
		if got.Source != want.Source || got.Binary != want.Binary || got.Image != want.Image ||
			filepath.ToSlash(got.Config) != want.Config {
			t.Errorf("%s = %+v, want %+v", name, got, want)
		}
	}
	check("trivy", tools.Trivy.ToolConfig, ToolConfig{"image", "/opt/trivy", "mirror/trivy", ""})
	check("gitleaks", tools.Gitleaks.ToolConfig, ToolConfig{"binary", "/opt/gitleaks", "mirror/gitleaks", "/etc/gitleaks.toml"})
	check("plumber", tools.Plumber, ToolConfig{"image", "/opt/plumber", "mirror/plumber", "/etc/plumber.yaml"})
	check("kubeconform", tools.Kubeconform.ToolConfig, ToolConfig{"binary", "/opt/kubeconform", "mirror/kubeconform", ""})
	check("helm", tools.Helm, ToolConfig{"image", "/opt/helm", "mirror/helm", ""})
	check("kustomize", tools.Kustomize, ToolConfig{"binary", "/opt/kustomize", "mirror/kustomize", ""})

	if tools.Trivy.Server != (TrivyServerConfig{Enabled: true, URL: "https://trivy:4954"}) {
		t.Errorf("trivy server = %+v", tools.Trivy.Server)
	}
	if !tools.Trivy.IgnoreUnfixed || !tools.Trivy.IgnoreEOL || !tools.Gitleaks.History {
		t.Errorf("a switch was dropped: %+v / %+v", tools.Trivy, tools.Gitleaks)
	}
	if tools.Kubeconform.KubernetesVersion != "1.31.0" {
		t.Errorf("kubernetes version = %q", tools.Kubeconform.KubernetesVersion)
	}
	if cfg.Scan.Legacy != (legacyScanKeys{}) {
		t.Errorf("the legacy keys were not cleared: %+v", cfg.Scan.Legacy)
	}
}

// The old keys leave the file on the next save, and nothing is lost on the way.
func TestTheLegacyScanKeysLeaveTheFileOnSave(t *testing.T) {
	cfg := loadScan(t, "  trivy_image: mirror/trivy\n  enable_ci_score: true\n")
	if err := SaveContext(cfg, "default"); err != nil {
		t.Fatalf("SaveContext: %v", err)
	}
	path, _ := GetContextPath("default")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, key := range []string{"trivy_image", "enable_ci_score", "enable_vuln"} {
		if strings.Contains(string(body), key+":") {
			t.Errorf("%s is still in the saved file:\n%s", key, body)
		}
	}
	again, err := LoadContext("default")
	if err != nil {
		t.Fatalf("LoadContext: %v", err)
	}
	if again.Scan.Tools.Trivy.Image != "mirror/trivy" || !again.Scan.Categories.CI.Enabled {
		t.Errorf("the round trip lost a setting: %+v", again.Scan)
	}
}

func TestTheLegacySwitchesBecomeCategories(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want ScanCategories
	}{
		{
			// Written before any switch existed: the historical defaults.
			name: "no switch at all",
			yaml: "  timeout: 60\n",
			want: DefaultScanCategories(),
		},
		{
			name: "CI alone is a choice",
			yaml: "  enable_ci_score: true\n",
			want: func() ScanCategories {
				c := DefaultScanCategories()
				c.Vuln.Enabled, c.Secret.Enabled, c.CI.Enabled = false, false, true
				return c
			}(),
		},
		{
			name: "misconfig ticks trivy",
			yaml: "  enable_vuln: true\n  enable_misconfig: true\n  enable_license: true\n",
			want: func() ScanCategories {
				c := DefaultScanCategories()
				c.Secret.Enabled, c.Misconfig.Enabled, c.License.Enabled = false, true, true
				return c
			}(),
		},
		{
			// The renderers served whenever installed; they are ticked now.
			name: "the schema alone ticks kubeconform and its renderers",
			yaml: "  enable_k8s_schema: true\n",
			want: func() ScanCategories {
				c := DefaultScanCategories()
				c.Vuln.Enabled, c.Secret.Enabled = false, false
				c.Misconfig = CategoryConfig{Enabled: true, Tools: []string{ToolKubeconform, ToolHelm, ToolKustomize}}
				return c
			}(),
		},
		{
			name: "both misconfiguration switches",
			yaml: "  enable_misconfig: true\n  enable_k8s_schema: true\n",
			want: func() ScanCategories {
				c := DefaultScanCategories()
				c.Vuln.Enabled, c.Secret.Enabled = false, false
				c.Misconfig = CategoryConfig{Enabled: true, Tools: []string{ToolTrivy, ToolKubeconform, ToolHelm, ToolKustomize}}
				return c
			}(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := loadScan(t, tt.yaml).Scan.Categories
			for _, id := range CategoryIDs() {
				g, w := got.Category(id), tt.want.Category(id)
				if g.Enabled != w.Enabled || !slices.Equal(g.Tools, w.Tools) {
					t.Errorf("%s = %+v, want %+v", id, *g, *w)
				}
			}
		})
	}
}

// A `categories:` block that is present is an answer, even all off, and the
// old switches beside it do not override it.
func TestAPresentCategoriesBlockWins(t *testing.T) {
	cfg := loadScan(t, `
  enable_vuln: true
  categories:
    vuln: {enabled: false, tools: [trivy]}
    secret: {enabled: false, tools: [trivy, gitleaks]}
`)
	c := cfg.Scan.Categories
	if c.Vuln.Enabled || c.Secret.Enabled {
		t.Errorf("an all-off block was overridden: %+v", c)
	}
}

// The new keys win field by field over the old ones in a hand-edited file.
func TestANewToolKeyIsNotOverwrittenByAnOldOne(t *testing.T) {
	cfg := loadScan(t, `
  trivy_image: old/trivy
  trivy_path: /opt/trivy
  tools:
    trivy: {image: new/trivy}
`)
	if got := cfg.Scan.Tools.Trivy; got.Image != "new/trivy" || got.Binary != "/opt/trivy" {
		t.Errorf("trivy = %+v, want the new image and the old path", got.ToolConfig)
	}
}
