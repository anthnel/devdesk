package configuration

import (
	"fmt"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// The scan and tools tabs are generated from internal/scan's tables (§3.86):
// a category or a tool added there appears here without a line written for
// it — which is the defect the tables exist to end.

// Reasons a checkbox cannot move, said in the footer in place of its hint.
const (
	reasonTrivyServer = "Not supported by a Trivy server"
	reasonLastTool    = "A category needs at least one tool — turn the category off instead"
)

// categoryHints say what a category's checkbox does, in each state. Each names
// what is scanned and what becomes required, which is what ties the box to the
// dashboard's tools line.
var categoryHints = map[scan.CategoryID][2]string{
	scan.CategoryIDVuln: {
		"Packages are checked for known CVEs — trivy is required",
		"No CVE check — trivy is not required for it"},
	scan.CategoryIDSecret: {
		"Files, image layers and git history are searched for secrets",
		"No secret search — its tools are not required for it"},
	scan.CategoryIDMisconfig: {
		"Dockerfiles, IaC and Kubernetes manifests are checked",
		"No misconfiguration check — its tools are not required for it"},
	scan.CategoryIDLicense: {
		"Package licenses are reported — repositories only, trivy is required",
		"No license check — trivy is not required for it"},
	scan.CategoryIDCI: {
		"The pipeline is graded — this context's forge only, plumber is required",
		"No CI grade — plumber is not required"},
}

// toolHints say what ticking a tool inside a category changes. A category with
// one tool has no tool rows, so it has no entry here.
var toolHints = map[scan.CategoryID]map[scan.ToolID][2]string{
	scan.CategoryIDSecret: {
		scan.ToolTrivy: {
			"Files and image layers are searched for secrets — trivy is required",
			"Only git history is searched, by Gitleaks — repositories only"},
		scan.ToolGitleaks: {
			"Git history is searched for secrets — gitleaks is required",
			"Only the files on disk are searched, by Trivy — gitleaks is not required"},
	},
	scan.CategoryIDMisconfig: {
		scan.ToolTrivy: {
			"Dockerfiles, IaC and manifests are linted for security — trivy is required",
			"Trivy's security rules do not run"},
		scan.ToolKubeconform: {
			"Manifests are checked against the Kubernetes API schema — kubeconform is required",
			"Manifests are only linted for security, by Trivy"},
		scan.ToolHelm: {
			"Charts are linted and rendered, then validated — helm is required",
			"Charts are not validated; they are counted as Not rendered"},
		scan.ToolKustomize: {
			"Overlays are built, then validated — kustomize is required",
			"Overlays are not validated; they are counted as Not rendered"},
	},
}

// categoryFields is the Categories group: one checkbox per category, and under
// it one per tool when there is a choice to make.
func categoryFields() []field {
	var out []field
	for _, cat := range scan.Categories() {
		out = append(out, categoryRow(cat))
		// One tool is nothing to choose: its name sits beside the category.
		if len(cat.Tools) < 2 {
			continue
		}
		for _, ct := range cat.Tools {
			out = append(out, toolRow(cat, ct))
		}
	}
	return out
}

// categoryOf reads one category of the configuration being edited.
func categoryOf(id scan.CategoryID) func(*config.Config) *config.CategoryConfig {
	return func(c *config.Config) *config.CategoryConfig { return c.Scan.Categories.Category(string(id)) }
}

func categoryRow(cat scan.ToolCategory) field {
	ref := categoryOf(cat.ID)
	f := toggle(cat.Label, func(c *config.Config) *bool { return &ref(c).Enabled }, "")
	f.hintOn, f.hintOff = categoryHints[cat.ID][0], categoryHints[cat.ID][1]
	if len(cat.Tools) == 1 {
		f.Note = toolName(cat.Tools[0].Tool)
	}
	// A category whose every tool is a part of Trivy a server cannot do has
	// nothing left to run in server mode: Licenses.
	if !serverCanServe(cat) {
		f.locked = func(c *config.Config) string {
			if c.Scan.Tools.Trivy.Server.Enabled {
				return reasonTrivyServer
			}
			return ""
		}
	}
	return f
}

func toolRow(cat scan.ToolCategory, ct scan.CategoryTool) field {
	ref := categoryOf(cat.ID)
	tool := string(ct.Tool)
	f := field{
		Label: toolName(ct.Tool),
		Kind:  kindToggle,
		Depth: 1,
		Note:  toolNote(cat, ct),
		get:   func(c *config.Config) bool { return ref(c).Has(tool) },
		set:   func(c *config.Config, on bool) { *ref(c) = ref(c).With(tool, on) },
	}
	f.hintOn, f.hintOff = toolHints[cat.ID][ct.Tool][0], toolHints[cat.ID][ct.Tool][1]
	if ct.DependsOn != "" {
		f.Depth = 2
	}

	dependency := toolName(ct.DependsOn)
	f.locked = func(c *config.Config) string {
		switch {
		case ct.Tool == scan.ToolTrivy && !ct.Server && c.Scan.Tools.Trivy.Server.Enabled:
			return reasonTrivyServer
		case !ref(c).Enabled:
			return "Turn " + cat.Label + " on first"
		case ct.DependsOn != "" && !ref(c).Has(string(ct.DependsOn)):
			return "Tick " + dependency + " first"
		}
		return ""
	}
	// Unticking the tool that leaves an enabled category with nothing to run
	// is refused rather than turning the category off behind the user's back:
	// the category's own box is the switch for that.
	f.guard = func(c *config.Config, on bool) string {
		if on || !ref(c).Enabled {
			return ""
		}
		after := c.Scan.Categories
		*after.Category(string(cat.ID)) = ref(c).With(tool, false)
		for _, other := range cat.Tools {
			if scan.Uses(after, cat.ID, other.Tool) {
				return ""
			}
		}
		return reasonLastTool
	}
	return f
}

// toolNote is what a tool contributes to a category, and where it cannot go
// when the category's other tools can.
func toolNote(cat scan.ToolCategory, ct scan.CategoryTool) string {
	note := ct.Role
	if !ct.Applies(scan.TargetImage) && categoryScansImages(cat) {
		note += ", repositories only"
	}
	return note
}

func categoryScansImages(cat scan.ToolCategory) bool {
	for _, ct := range cat.Tools {
		if ct.Applies(scan.TargetImage) {
			return true
		}
	}
	return false
}

// serverCanServe reports whether a category keeps a tool to run when Trivy is
// in client-server mode.
func serverCanServe(cat scan.ToolCategory) bool {
	for _, ct := range cat.Tools {
		if ct.Tool != scan.ToolTrivy || ct.Server {
			return true
		}
	}
	return false
}

func toolName(id scan.ToolID) string {
	tool, ok := scan.ToolByID(id)
	if !ok {
		return string(id)
	}
	return tool.Name
}

// toolIcons head each tool's group on the tools tab.
var toolIcons = map[scan.ToolID]string{
	scan.ToolTrivy:       theme.IconTarget,
	scan.ToolGitleaks:    theme.IconToml,
	scan.ToolPlumber:     theme.IconGitBranch,
	scan.ToolKubeconform: theme.IconKubernetes,
	scan.ToolHelm:        theme.IconKubernetes,
	scan.ToolKustomize:   theme.IconKubernetes,
	scan.ToolCosign:      theme.IconLock,
}

// Platform tools, shown first on the tools tab: required whatever is ticked,
// and set elsewhere — the engine in the app tab — so they have no settings.
const (
	platformEngine = "engine"
	platformGit    = "git"
)

// toolFields is the tools tab: the platform tools, then one group per scanner
// with where it runs from and its own options.
func toolFields() []field {
	out := group("Platform", theme.IconTools,
		platformRow("Engine", platformEngine),
		platformRow("Git", platformGit),
	)
	for _, tool := range scan.Tools() {
		out = append(out, group(tool.Name, toolIcons[tool.ID], toolSettings(tool)...)...)
	}
	return out
}

func platformRow(label, which string) field {
	f := static(label, "", "Required whatever is ticked — the engine is chosen in the app tab")
	f.platform = which
	return f
}

// toolSettings are one scanner's rows. Every tool has the three that say
// where it runs from; the rest are its own.
func toolSettings(tool scan.Tool) []field {
	id := string(tool.ID)
	set := func(c *config.Config) *config.ToolConfig { return c.Scan.Tools.Tool(id) }

	fields := []field{
		cycle("Source", func(c *config.Config) *string { return &set(c).Source },
			[]string{config.ToolSourceAuto, config.ToolSourceBinary, config.ToolSourceImage},
			"binary fails rather than falling back to an image"),
		text("Binary", func(c *config.Config) *string { return &set(c).Binary },
			"Empty resolves "+tool.Binary+" on PATH"),
		text("Image", func(c *config.Config) *string { return &set(c).Image },
			"Empty uses "+tool.DefaultImage),
	}
	if tool.HasConfig {
		fields = append(fields, text("Config", func(c *config.Config) *string { return &set(c).Config },
			"Path to a rules file, made absolute when saved"))
	}
	fields = append(fields, argsField(tool, func(c *config.Config) *[]string { return &set(c).Args }))
	fields = append(fields, ownSettings[tool.ID]()...)
	for i := range fields {
		fields[i].Tool = id
	}
	return fields
}

// ownSettings are the settings only one tool has.
var ownSettings = map[scan.ToolID]func() []field{
	scan.ToolTrivy: func() []field {
		server := toggle(useTrivyServerLabel, func(c *config.Config) *bool { return &c.Scan.Tools.Trivy.Server.Enabled }, "")
		server.hintOn = "Scans go to the server; Trivy no longer checks misconfigurations or licenses"
		server.hintOff = "Trivy runs locally"
		unfixed := toggle("Ignore unfixed", func(c *config.Config) *bool { return &c.Scan.Tools.Trivy.IgnoreUnfixed }, "")
		unfixed.hintOn, unfixed.hintOff = "Only vulnerabilities with a fix are reported", "Every vulnerability is reported, fixed or not"
		eol := toggle("Ignore end-of-life", func(c *config.Config) *bool { return &c.Scan.Tools.Trivy.IgnoreEOL }, "")
		eol.hintOn, eol.hintOff = "Findings in end-of-life packages are dropped", "End-of-life packages are reported like any other"
		return []field{
			server,
			validated(trivyServerLabel, func(c *config.Config) *string { return &c.Scan.Tools.Trivy.Server.URL },
				"Address used only while the checkbox above is on",
				func(v string) error { return scan.ValidateTrivyServer(v) }),
			unfixed,
			eol,
		}
	},
	scan.ToolGitleaks: func() []field {
		history := toggle("Scan git history", func(c *config.Config) *bool { return &c.Scan.Tools.Gitleaks.History }, "")
		history.hintOn, history.hintOff = "Every commit is searched — slower", "Only the current tree is searched"
		return []field{history}
	},
	scan.ToolPlumber: func() []field { return nil },
	scan.ToolKubeconform: func() []field {
		return []field{validated("Kubernetes version", func(c *config.Config) *string { return &c.Scan.Tools.Kubeconform.KubernetesVersion },
			"x.y.z or master — decides which apiVersions count as removed",
			scan.ValidateKubernetesVersion)}
	},
	scan.ToolHelm:      func() []field { return nil },
	scan.ToolKustomize: func() []field { return nil },
	scan.ToolCosign:    func() []field { return nil },
}

// The two Trivy server rows are named once: tests and the footer find them by
// label, and "Server" alone would say nothing in a tab of six tools.
const (
	useTrivyServerLabel = "Use Trivy server"
	trivyServerLabel    = "Server"
)

// argsField is a tool's extra arguments, edited as one line and stored as a
// list. A flag DevDesk sets itself is refused as it is typed, by name, and the
// field keeps its old value (Rule 128).
func argsField(tool scan.Tool, ref func(*config.Config) *[]string) field {
	hint := "Extra arguments, after the subcommand — quotes keep a value with spaces together"
	if tool.ID == scan.ToolHelm {
		hint = "Extra arguments for lint and template alike — --values, --set"
	}
	return field{Label: "Args", Kind: kindText, list: ref, hint: hint}
}

// applyList parses a line of arguments and writes it, or refuses it.
func (f field) applyList(c *config.Config, line string) error {
	args, err := scan.SplitArgs(line)
	if err != nil {
		return fmt.Errorf("%s: %w", f.Label, err)
	}
	if tool, ok := scan.ToolByID(scan.ToolID(f.Tool)); ok {
		if err := tool.CheckArgs(args); err != nil {
			return err
		}
	}
	*f.list(c) = args
	return nil
}
