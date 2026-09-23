package scan

import (
	"os/exec"
	"slices"
	"strings"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/engine"
)

// The scanners, the categories they serve, and which of them a context needs —
// declared once (§3.86).
//
// A category is what a scan looks for; a tool is what runs it. The link between
// the two used to be written out wherever it was needed — in the stage gates,
// in missingToolErrors, in the dashboard's fixed list of known tools — and each
// copy answered a slightly different question: plumber was never reported
// missing by a scan, and helm was reported missing by the dashboard although no
// setting asked for it. Everything now reads these two tables.

// ToolID names one scanner. The values are the configuration's own keys.
type ToolID string

const (
	ToolTrivy       ToolID = config.ToolTrivy
	ToolGitleaks    ToolID = config.ToolGitleaks
	ToolPlumber     ToolID = config.ToolPlumber
	ToolKubeconform ToolID = config.ToolKubeconform
	ToolHelm        ToolID = config.ToolHelm
	ToolKustomize   ToolID = config.ToolKustomize
)

// Tool is what DevDesk knows about one scanner, whatever it is configured with.
type Tool struct {
	ID ToolID
	// Name is how the tool is written for a person — and what they type to
	// install it.
	Name string
	// Binary is the executable's name on PATH.
	Binary       string
	DefaultImage string
	// VersionArgs make the tool print its version on stdout.
	VersionArgs []string
	// HasConfig is a tool that reads a rules file of its own.
	HasConfig bool
	// Lost is what a scan did not do because the tool is missing, for the error
	// that says so.
	Lost string
}

var toolTable = []Tool{
	{ID: ToolTrivy, Name: "Trivy", Binary: "trivy", DefaultImage: DefaultTrivyImage,
		VersionArgs: []string{"--version"}, HasConfig: true,
		Lost: "nothing was scanned"},
	{ID: ToolGitleaks, Name: "Gitleaks", Binary: "gitleaks", DefaultImage: DefaultGitleaksImage,
		VersionArgs: []string{"version"}, HasConfig: true,
		// Not "no secret scan was run": Trivy runs one too, so naming what
		// Gitleaks alone contributes is what keeps this accurate when only one
		// of the two is missing.
		Lost: "git history was not scanned for secrets"},
	// `plumber version` writes the installed version to stdout and an upgrade
	// notice — "plumber v0.4.44 is available (you have 0.4.40)" — to stderr.
	// toolVersion reads stdout only, so the two cannot be confused; measured
	// rather than assumed, because reporting the available version as the
	// installed one is the kind of thing nobody notices for months.
	{ID: ToolPlumber, Name: "Plumber", Binary: "plumber", DefaultImage: DefaultPlumberImage,
		VersionArgs: []string{"version"}, HasConfig: true,
		Lost: "the CI configuration was not graded"},
	{ID: ToolKubeconform, Name: "Kubeconform", Binary: "kubeconform", DefaultImage: DefaultKubeconformImage,
		VersionArgs: []string{"-v"},
		Lost:        "Kubernetes manifests were not validated against the API schema"},
	{ID: ToolHelm, Name: "Helm", Binary: "helm", DefaultImage: DefaultHelmImage,
		VersionArgs: []string{"version", "--short"},
		Lost:        "Helm charts were not rendered"},
	{ID: ToolKustomize, Name: "Kustomize", Binary: "kustomize", DefaultImage: DefaultKustomizeImage,
		VersionArgs: []string{"version"},
		Lost:        "Kustomize overlays were not built"},
}

// Tools lists every scanner, in the table's order.
func Tools() []Tool { return slices.Clone(toolTable) }

// ToolByID returns one scanner's description.
func ToolByID(id ToolID) (Tool, bool) {
	i := slices.IndexFunc(toolTable, func(t Tool) bool { return t.ID == id })
	if i < 0 {
		return Tool{}, false
	}
	return toolTable[i], true
}

// CategoryID names one category. The values are the configuration's own keys.
//
// Spelled CategoryID… rather than Category…, which names the family a finding
// is filed under (category.go) — the tab it lands in, not the setting that
// asked for it.
type CategoryID string

const (
	CategoryIDVuln      CategoryID = config.CategoryVuln
	CategoryIDSecret    CategoryID = config.CategorySecret
	CategoryIDMisconfig CategoryID = config.CategoryMisconfig
	CategoryIDLicense   CategoryID = config.CategoryLicense
	CategoryIDCI        CategoryID = config.CategoryCI
)

// CategoryTool is one tool's part in one category.
type CategoryTool struct {
	Tool ToolID
	// Role is what the tool contributes to the category, for a person.
	Role string
	// DependsOn is a tool this one only serves: helm and kustomize render what
	// kubeconform validates, and detect nothing on their own.
	DependsOn ToolID
	// Targets are what the tool can scan in this category.
	Targets []TargetType
	// Server is whether a Trivy server can do this part — false where the
	// client-server protocol does not support it.
	Server bool
	// Default is ticked in a new context.
	Default bool
}

// Applies reports whether the tool scans this kind of target here.
func (ct CategoryTool) Applies(t TargetType) bool {
	return slices.Contains(ct.Targets, t)
}

// ToolCategory is one category and the tools that can serve it.
type ToolCategory struct {
	ID    CategoryID
	Label string
	Tools []CategoryTool
}

var (
	bothTargets   = []TargetType{TargetDirectory, TargetImage}
	directoryOnly = []TargetType{TargetDirectory}
)

var categoryTable = []ToolCategory{
	{ID: CategoryIDVuln, Label: "Vulnerabilities", Tools: []CategoryTool{
		{Tool: ToolTrivy, Targets: bothTargets, Server: true, Default: true},
	}},
	// Complementary rather than redundant: Gitleaks reads git history, Trivy
	// reads the files and an image's layers — which is what gives an image scan
	// a secret stage at all.
	{ID: CategoryIDSecret, Label: "Secrets", Tools: []CategoryTool{
		{Tool: ToolTrivy, Role: "files and image layers", Targets: bothTargets, Server: true, Default: true},
		{Tool: ToolGitleaks, Role: "git history", Targets: directoryOnly, Default: true},
	}},
	// kubeconform is a tool of this category rather than a category of its own:
	// Categorize already files its findings under Misconfiguration (§3.80).
	{ID: CategoryIDMisconfig, Label: "Misconfiguration", Tools: []CategoryTool{
		{Tool: ToolTrivy, Role: "security rules", Targets: bothTargets, Default: true},
		{Tool: ToolKubeconform, Role: "API schema", Targets: directoryOnly},
		{Tool: ToolHelm, Role: "renders charts first", DependsOn: ToolKubeconform, Targets: directoryOnly},
		{Tool: ToolKustomize, Role: "builds overlays first", DependsOn: ToolKubeconform, Targets: directoryOnly},
	}},
	{ID: CategoryIDLicense, Label: "Licenses", Tools: []CategoryTool{
		{Tool: ToolTrivy, Targets: directoryOnly, Default: true},
	}},
	// An image has no pipeline, so there is nothing to grade.
	{ID: CategoryIDCI, Label: "CI", Tools: []CategoryTool{
		{Tool: ToolPlumber, Targets: directoryOnly, Default: true},
	}},
}

// Categories lists every category, in the table's order.
func Categories() []ToolCategory { return slices.Clone(categoryTable) }

// Uses reports whether one tool runs for one category: the category is on, the
// tool is ticked in it, and so is the tool it depends on.
func Uses(c config.ScanCategories, cat CategoryID, tool ToolID) bool {
	i := slices.IndexFunc(categoryTable, func(tc ToolCategory) bool { return tc.ID == cat })
	if i < 0 {
		return false
	}
	return uses(c, categoryTable[i], tool)
}

func uses(c config.ScanCategories, cat ToolCategory, tool ToolID) bool {
	set := c.Category(string(cat.ID))
	if set == nil || !set.Enabled || !set.Has(string(tool)) {
		return false
	}
	i := slices.IndexFunc(cat.Tools, func(ct CategoryTool) bool { return ct.Tool == tool })
	if i < 0 {
		return false
	}
	dep := cat.Tools[i].DependsOn
	return dep == "" || set.Has(string(dep))
}

// Required lists the tools a context needs, in the tool table's order: every
// tool some enabled category uses. A tool nobody ticked is not required, even
// installed — nor is it missing when it is absent.
func Required(c config.ScanCategories) []ToolID {
	var out []ToolID
	for _, tool := range toolTable {
		for _, cat := range categoryTable {
			if uses(c, cat, tool.ID) {
				out = append(out, tool.ID)
				break
			}
		}
	}
	return out
}

// ToolStatus is what detection found for one scanner.
type ToolStatus struct {
	Available bool
	Source    ToolSource
	// Binary is what to exec when Source is ToolSourceBinary.
	Binary string
	// Image is the OCI image, the configured one or the default.
	Image string
	// Version is the tool's own answer, best-effort and uncleaned.
	Version string
}

// Report is one detection over this machine: where each scanner runs from, and
// whether the container engine is there to run images at all.
type Report struct {
	Tools map[ToolID]ToolStatus
	// EngineAvailable reports whether the configured container engine answered.
	// Without it no image-sourced tool can run, whichever engine it is.
	EngineAvailable bool
	// ImageScanSocket is why an image scan in a container may be impossible
	// even when the engine is present: trivy inspects a host-held image through
	// the engine's socket, and rootless podman has none unless `podman system
	// service` is running. Empty means the mount cannot be made — see
	// ImageScanBlocked.
	ImageScanSocket string

	// The platform tools: not scanners, and required whatever is ticked —
	// the engine runs every container view, git every repository one (§3.86).
	// EngineVersion and GitVersion are the tools' own answers, uncleaned.
	EngineVersion string
	GitAvailable  bool
	GitVersion    string
}

// Status returns one tool's detection. A tool the report does not hold is
// unavailable, and still names its default image — the error that says it is
// missing tells the user what to pull.
func (r Report) Status(id ToolID) ToolStatus {
	if st, ok := r.Tools[id]; ok {
		return st
	}
	st := ToolStatus{Source: ToolSourceNone}
	if tool, ok := ToolByID(id); ok {
		st.Image = tool.DefaultImage
	}
	return st
}

// Available reports whether a tool can run.
func (r Report) Available(id ToolID) bool { return r.Status(id).Available }

// Spec is how one resolved tool is handed to its command builder. Only Trivy
// inspects a host-held image, so only Trivy gets the engine socket.
func (r Report) Spec(id ToolID) ToolSpec {
	st := r.Status(id)
	spec := ToolSpec{Source: st.Source, Binary: st.Binary, Image: st.Image}
	if id == ToolTrivy {
		spec.HostSocket = r.ImageScanSocket
	}
	return spec
}

// Missing lists the required tools that cannot run, in the tool table's order.
func (r Report) Missing(c config.ScanCategories) []ToolID {
	var out []ToolID
	for _, id := range Required(c) {
		if !r.Available(id) {
			out = append(out, id)
		}
	}
	return out
}

// AllAvailable reports whether nothing this context needs is missing: every
// required scanner, and the two platform tools. It is the dashboard's one line.
func (r Report) AllAvailable(c config.ScanCategories) bool {
	return len(r.Missing(c)) == 0 && r.EngineAvailable && r.GitAvailable
}

// CanScan reports whether a scan of this kind of target would run anything: at
// least one tool that some enabled category uses, that applies to the target,
// and that is available.
func (r Report) CanScan(c config.ScanCategories, t TargetType) bool {
	for _, cat := range categoryTable {
		for _, ct := range cat.Tools {
			if ct.Applies(t) && uses(c, cat, ct.Tool) && r.Available(ct.Tool) {
				return true
			}
		}
	}
	return false
}

// ImageScanBlocked reports why an image cannot be scanned in a container, or ""
// when it can.
//
// It answers before the keypress, which is what Rule 130 needs: the action is
// greyed out with this reason rather than attempted and failed. A directory
// scan mounts only the directory, so it stays available either way — the socket
// is the image path's problem alone.
func (r Report) ImageScanBlocked(engineName string, server string) string {
	if r.ImageScanSocket != "" || server != "" {
		// A Trivy server does the inspection itself; no socket is needed.
		return ""
	}
	return engineName + " socket not found — run `" + engineName +
		" system service` or set a Trivy server"
}

// Detect works out where each scanner runs from, for one context's tool
// settings.
//
// It replaces CheckDependencies, which resolved each tool in a block of its
// own: six tools, six copies, and one left out of the options a scan detected
// with was silently resolved with its defaults — what happened to plumber until
// §3.80. It loops over the table, so a tool is either in it or nowhere.
func Detect(tools config.ScanTools) Report {
	r := Report{Tools: make(map[ToolID]ToolStatus, len(toolTable))}
	eng := engine.Current()
	if path, err := exec.LookPath(eng.Binary); err == nil && path != "" {
		r.EngineAvailable = true
		r.ImageScanSocket = eng.HostSocket()
		r.EngineVersion = toolVersion(path, "version", "--format", eng.Templates.Version)
	}
	if path, ok := locateBinary("", "git"); ok {
		r.GitAvailable = true
		r.GitVersion = toolVersion(path, "--version")
	}
	for _, tool := range toolTable {
		set := tools.Tool(string(tool.ID))
		image := orDefault(set.Image, tool.DefaultImage)
		res := resolveTool(set.Source, set.Binary, tool.Binary, image, r.EngineAvailable, tool.VersionArgs...)
		r.Tools[tool.ID] = ToolStatus{
			Available: res.Available,
			Source:    res.Source,
			Binary:    res.Binary,
			Image:     image,
			Version:   res.Version,
		}
	}
	return r
}

// SameDetection reports whether two sets of tool settings would detect the
// same thing: the same source, binary and image for every tool. The router
// re-detects on a saved configuration only when they differ — a filter such as
// ignore_unfixed changes what a scan reports, never where a tool runs from.
func SameDetection(a, b config.ScanTools) bool {
	for _, tool := range toolTable {
		x, y := a.Tool(string(tool.ID)), b.Tool(string(tool.ID))
		if x.Source != y.Source || x.Binary != y.Binary || x.Image != y.Image {
			return false
		}
	}
	return true
}

// CleanVersion reduces a tool's own version answer to the number: the first
// line, without the engine prefix containerToolVersion adds or the preamble
// some tools print ("git version", Trivy's "Version:").
func CleanVersion(v string) string {
	v = strings.TrimSpace(v)
	if name := engine.Current().Name + ":"; strings.HasPrefix(v, name) {
		v = strings.TrimSpace(strings.TrimPrefix(v, name))
	}
	v = strings.TrimSpace(strings.TrimPrefix(v, "docker:"))
	if idx := strings.IndexByte(v, '\n'); idx >= 0 {
		v = v[:idx]
	}
	v = strings.TrimPrefix(v, "git version ")
	v = strings.TrimPrefix(v, "Version: ")
	return v
}
