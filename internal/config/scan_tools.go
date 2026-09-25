package config

import "slices"

// The scanners and the categories they serve, as the configuration names them.
//
// The table that says which tool serves which category, what each one needs and
// which are ticked by default lives in internal/scan (toolbox.go): it is the one
// source of truth. This file only declares the identifiers the YAML is written
// with, because the configuration cannot import the package that reads it —
// scan imports config, not the other way round. A test in scan checks that the
// two agree.

// Tool identifiers, as they appear under `scan.tools` and in a category's
// `tools` list.
const (
	ToolTrivy       = "trivy"
	ToolGitleaks    = "gitleaks"
	ToolPlumber     = "plumber"
	ToolKubeconform = "kubeconform"
	ToolHelm        = "helm"
	ToolKustomize   = "kustomize"
	// ToolCosign verifies image signatures (§3.82). It serves no category: it
	// runs before a pull as much as during a scan, and scan.image_verification
	// is its one switch.
	ToolCosign = "cosign"
)

// ToolIDs lists every scanner, in the order the tables use.
func ToolIDs() []string {
	return []string{ToolTrivy, ToolGitleaks, ToolPlumber, ToolKubeconform, ToolHelm, ToolKustomize, ToolCosign}
}

// Category identifiers, as they appear under `scan.categories`.
const (
	CategoryVuln      = "vuln"
	CategorySecret    = "secret"
	CategoryMisconfig = "misconfig"
	CategoryLicense   = "license"
	CategoryCI        = "ci"
)

// CategoryIDs lists every category, in the order the tables use.
func CategoryIDs() []string {
	return []string{CategoryVuln, CategorySecret, CategoryMisconfig, CategoryLicense, CategoryCI}
}

// ToolConfig is what every scanner is configured with: where it runs from, and
// what to run.
type ToolConfig struct {
	Source string `yaml:"source"`           // auto | binary | image
	Binary string `yaml:"binary"`           // custom executable; empty resolves the name on PATH
	Image  string `yaml:"image"`            // OCI image; empty is the tool's default
	Config string `yaml:"config,omitempty"` // rules file, for the tools that read one
	// Args are extra arguments, placed after the tool's subcommand and before
	// DevDesk's own and the target. The flags DevDesk sets itself are refused
	// in the configuration view (scan.Tool.ReservedArgs).
	Args []string `yaml:"args,omitempty"`
}

// TrivyConfig adds what only Trivy has: the client-server mode and its two
// result filters.
type TrivyConfig struct {
	ToolConfig `yaml:",inline"`
	Server     TrivyServerConfig `yaml:"server"`
	// IgnoreUnfixed only shows vulnerabilities that have a fix.
	IgnoreUnfixed bool `yaml:"ignore_unfixed"`
	// IgnoreEOL drops end-of-life package vulnerabilities
	// (--ignore-status end_of_life).
	IgnoreEOL bool `yaml:"ignore_eol"`
}

// TrivyServerConfig is Trivy's client-server mode. The address is kept while
// the mode is off, so switching it back on does not lose what was typed — but a
// scan must not see it until Enabled says the mode is wanted.
type TrivyServerConfig struct {
	Enabled bool   `yaml:"enabled"`
	URL     string `yaml:"url"`
}

// GitleaksConfig adds whether git history is searched (omit --no-git).
type GitleaksConfig struct {
	ToolConfig `yaml:",inline"`
	History    bool `yaml:"history"`
}

// KubeconformConfig adds the release manifests are validated against, as
// kubeconform takes it: a full x.y.z, or "master". It decides whether an
// apiVersion counts as removed. Empty is DefaultKubernetesVersion.
type KubeconformConfig struct {
	ToolConfig        `yaml:",inline"`
	KubernetesVersion string `yaml:"kubernetes_version"`
}

// ScanTools is every scanner's settings, one block per tool.
type ScanTools struct {
	Trivy       TrivyConfig       `yaml:"trivy"`
	Gitleaks    GitleaksConfig    `yaml:"gitleaks"`
	Plumber     ToolConfig        `yaml:"plumber"`
	Kubeconform KubeconformConfig `yaml:"kubeconform"`
	Helm        ToolConfig        `yaml:"helm"`
	Kustomize   ToolConfig        `yaml:"kustomize"`
	Cosign      ToolConfig        `yaml:"cosign"`
}

// Tool returns the settings every scanner shares, by identifier — what lets
// defaults, path expansion and detection loop over the tools instead of
// repeating a block per tool. Nil for an unknown identifier.
func (t *ScanTools) Tool(id string) *ToolConfig {
	switch id {
	case ToolTrivy:
		return &t.Trivy.ToolConfig
	case ToolGitleaks:
		return &t.Gitleaks.ToolConfig
	case ToolPlumber:
		return &t.Plumber
	case ToolKubeconform:
		return &t.Kubeconform.ToolConfig
	case ToolHelm:
		return &t.Helm
	case ToolKustomize:
		return &t.Kustomize
	case ToolCosign:
		return &t.Cosign
	}
	return nil
}

// CategoryConfig is one category: whether it runs, and which of its tools run
// it. A category that is off keeps its list, the way the Trivy server keeps its
// address while the mode is off.
type CategoryConfig struct {
	Enabled bool     `yaml:"enabled"`
	Tools   []string `yaml:"tools"`
}

// Has reports whether a tool is ticked in this category, whether or not the
// category itself is on.
func (c CategoryConfig) Has(tool string) bool {
	return slices.Contains(c.Tools, tool)
}

// With returns the category with a tool ticked or unticked. The list is always
// a new slice: a Config is copied by value between views, and an in-place edit
// would reach into every copy that shares the backing array.
func (c CategoryConfig) With(tool string, on bool) CategoryConfig {
	tools := slices.DeleteFunc(slices.Clone(c.Tools), func(t string) bool { return t == tool })
	if on {
		tools = append(tools, tool)
	}
	c.Tools = tools
	return c
}

// ScanCategories is what a scan looks for.
type ScanCategories struct {
	Vuln      CategoryConfig `yaml:"vuln"`
	Secret    CategoryConfig `yaml:"secret"`
	Misconfig CategoryConfig `yaml:"misconfig"`
	License   CategoryConfig `yaml:"license"`
	CI        CategoryConfig `yaml:"ci"`
}

// Category returns one category by identifier. Nil for an unknown identifier.
func (c *ScanCategories) Category(id string) *CategoryConfig {
	switch id {
	case CategoryVuln:
		return &c.Vuln
	case CategorySecret:
		return &c.Secret
	case CategoryMisconfig:
		return &c.Misconfig
	case CategoryLicense:
		return &c.License
	case CategoryCI:
		return &c.CI
	}
	return nil
}

// isZero is a `categories` block the file does not carry: nothing on and no
// tool ticked anywhere. A block written by DevDesk always ticks tools, since
// the defaults do and a category that is off keeps its list, so an all-off one
// is still an answer. An empty list counts as absent: a zero Config saves
// `tools: []`, which is not a choice anyone made.
func (c ScanCategories) isZero() bool {
	for _, id := range CategoryIDs() {
		cat := c.Category(id)
		if cat.Enabled || len(cat.Tools) > 0 {
			return false
		}
	}
	return true
}

// DefaultScanCategories is what a new context scans for: vulnerabilities and
// secrets, with each category's default tools ticked. The defaults must match
// the table in internal/scan, which a test there checks.
func DefaultScanCategories() ScanCategories {
	return ScanCategories{
		Vuln:      CategoryConfig{Enabled: true, Tools: []string{ToolTrivy}},
		Secret:    CategoryConfig{Enabled: true, Tools: []string{ToolTrivy, ToolGitleaks}},
		Misconfig: CategoryConfig{Tools: []string{ToolTrivy}},
		License:   CategoryConfig{Tools: []string{ToolTrivy}},
		CI:        CategoryConfig{Tools: []string{ToolPlumber}},
	}
}
