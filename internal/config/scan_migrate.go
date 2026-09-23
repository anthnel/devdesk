package config

// legacyScanKeys are the flat `scan:` keys that `categories:` and `tools:`
// replaced. They are read at load, carried over by migrateScanSection, and
// cleared, so they leave the file on the next save — every tag is omitempty
// for that reason alone.
type legacyScanKeys struct {
	TrivySource       string `yaml:"trivy_source,omitempty"`
	TrivyPath         string `yaml:"trivy_path,omitempty"`
	TrivyImage        string `yaml:"trivy_image,omitempty"`
	UseTrivyServer    bool   `yaml:"use_trivy_server,omitempty"`
	TrivyServer       string `yaml:"trivy_server,omitempty"`
	IgnoreUnfixed     bool   `yaml:"ignore_unfixed,omitempty"`
	IgnoreEOL         bool   `yaml:"ignore_eol,omitempty"`
	GitleaksSource    string `yaml:"gitleaks_source,omitempty"`
	GitleaksPath      string `yaml:"gitleaks_path,omitempty"`
	GitleaksImage     string `yaml:"gitleaks_image,omitempty"`
	GitleaksConfig    string `yaml:"gitleaks_config,omitempty"`
	GitleaksHistory   bool   `yaml:"gitleaks_history,omitempty"`
	PlumberSource     string `yaml:"plumber_source,omitempty"`
	PlumberPath       string `yaml:"plumber_path,omitempty"`
	PlumberImage      string `yaml:"plumber_image,omitempty"`
	PlumberConfig     string `yaml:"plumber_config,omitempty"`
	KubeconformSource string `yaml:"kubeconform_source,omitempty"`
	KubeconformPath   string `yaml:"kubeconform_path,omitempty"`
	KubeconformImage  string `yaml:"kubeconform_image,omitempty"`
	KubernetesVersion string `yaml:"kubernetes_version,omitempty"`
	HelmSource        string `yaml:"helm_source,omitempty"`
	HelmPath          string `yaml:"helm_path,omitempty"`
	HelmImage         string `yaml:"helm_image,omitempty"`
	KustomizeSource   string `yaml:"kustomize_source,omitempty"`
	KustomizePath     string `yaml:"kustomize_path,omitempty"`
	KustomizeImage    string `yaml:"kustomize_image,omitempty"`

	EnableVuln      bool `yaml:"enable_vuln,omitempty"`
	EnableSecret    bool `yaml:"enable_secret,omitempty"`
	EnableMisconfig bool `yaml:"enable_misconfig,omitempty"`
	EnableLicense   bool `yaml:"enable_license,omitempty"`
	EnableCIScore   bool `yaml:"enable_ci_score,omitempty"`
	EnableK8sSchema bool `yaml:"enable_k8s_schema,omitempty"`
}

// migrateScanSection carries the flat `scan:` keys into `tools:` and
// `categories:`.
//
// migrateGitLabSection's shape: before the defaults, field by field, and only
// where the new value is empty, so a hand-edited file that carries both keeps
// what was written later. A bool can only be carried as "on": false is both
// "unset" and "off", so no guard could tell the two apart, and a legacy `true`
// is the only value that says anything.
//
// The categories are carried whole or not at all: a `categories:` block that is
// present is an answer, even with everything off, and mixing it with the old
// switches would make one category's state depend on which file wrote it.
func migrateScanSection(s *ScanConfig) {
	l := s.Legacy
	if l == (legacyScanKeys{}) {
		return
	}
	defer func() { s.Legacy = legacyScanKeys{} }()

	t := &s.Tools
	carryTool(&t.Trivy.ToolConfig, l.TrivySource, l.TrivyPath, l.TrivyImage, "")
	carryTool(&t.Gitleaks.ToolConfig, l.GitleaksSource, l.GitleaksPath, l.GitleaksImage, l.GitleaksConfig)
	carryTool(&t.Plumber, l.PlumberSource, l.PlumberPath, l.PlumberImage, l.PlumberConfig)
	carryTool(&t.Kubeconform.ToolConfig, l.KubeconformSource, l.KubeconformPath, l.KubeconformImage, "")
	carryTool(&t.Helm, l.HelmSource, l.HelmPath, l.HelmImage, "")
	carryTool(&t.Kustomize, l.KustomizeSource, l.KustomizePath, l.KustomizeImage, "")

	t.Trivy.Server.Enabled = t.Trivy.Server.Enabled || l.UseTrivyServer
	carry(&t.Trivy.Server.URL, l.TrivyServer)
	t.Trivy.IgnoreUnfixed = t.Trivy.IgnoreUnfixed || l.IgnoreUnfixed
	t.Trivy.IgnoreEOL = t.Trivy.IgnoreEOL || l.IgnoreEOL
	t.Gitleaks.History = t.Gitleaks.History || l.GitleaksHistory
	carry(&t.Kubeconform.KubernetesVersion, l.KubernetesVersion)

	if s.Categories.isZero() {
		s.Categories = legacyCategories(l)
	}
}

// carryTool fills one tool's shared settings from its legacy keys.
func carryTool(dst *ToolConfig, source, path, image, configPath string) {
	carry(&dst.Source, source)
	carry(&dst.Binary, path)
	carry(&dst.Image, image)
	carry(&dst.Config, configPath)
}

func carry(dst *string, legacy string) {
	if *dst == "" {
		*dst = legacy
	}
}

// legacyCategories translates the six `enable_*` switches.
//
// Misconfiguration absorbs two of them: `enable_misconfig` ticks Trivy,
// `enable_k8s_schema` ticks kubeconform and both renderers — which served
// already whenever they were installed, and now become required. That is the
// one behaviour this migration changes, on purpose: what is ticked is what the
// dashboard reports as missing.
//
// All six off is a file written before any of them existed, the historical
// "never configured" reading: it gets the defaults, as it always did.
func legacyCategories(l legacyScanKeys) ScanCategories {
	c := DefaultScanCategories()
	if !l.EnableVuln && !l.EnableSecret && !l.EnableMisconfig &&
		!l.EnableLicense && !l.EnableCIScore && !l.EnableK8sSchema {
		return c
	}
	c.Vuln.Enabled = l.EnableVuln
	c.Secret.Enabled = l.EnableSecret
	c.License.Enabled = l.EnableLicense
	c.CI.Enabled = l.EnableCIScore
	c.Misconfig.Enabled = l.EnableMisconfig || l.EnableK8sSchema
	if l.EnableK8sSchema {
		var tools []string
		if l.EnableMisconfig {
			tools = append(tools, ToolTrivy)
		}
		c.Misconfig.Tools = append(tools, ToolKubeconform, ToolHelm, ToolKustomize)
	}
	return c
}
