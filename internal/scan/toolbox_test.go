package scan

import (
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
)

// The configuration declares the identifiers and the default ticks, because it
// cannot import this package; the table is the source of truth. The two must
// name the same tools and categories, and agree on what a new context ticks.
func TestTheTablesAndTheConfigurationAgree(t *testing.T) {
	var tools []string
	for _, tool := range Tools() {
		tools = append(tools, string(tool.ID))
		var set config.ScanTools
		if set.Tool(string(tool.ID)) == nil {
			t.Errorf("%s has no settings block in config.ScanTools", tool.ID)
		}
	}
	if !slices.Equal(tools, config.ToolIDs()) {
		t.Errorf("tools = %v, config.ToolIDs() = %v", tools, config.ToolIDs())
	}

	defaults := config.DefaultScanCategories()
	var cats []string
	for _, cat := range Categories() {
		cats = append(cats, string(cat.ID))
		var ticked []string
		for _, ct := range cat.Tools {
			if ct.Default {
				ticked = append(ticked, string(ct.Tool))
			}
		}
		if got := defaults.Category(string(cat.ID)).Tools; !slices.Equal(got, ticked) {
			t.Errorf("%s: the configuration ticks %v by default, the table %v", cat.ID, got, ticked)
		}
	}
	if !slices.Equal(cats, config.CategoryIDs()) {
		t.Errorf("categories = %v, config.CategoryIDs() = %v", cats, config.CategoryIDs())
	}
}

// A dependency names a tool of the same category, or the tick that decides it
// could never be made.
func TestADependencyIsATickInTheSameCategory(t *testing.T) {
	for _, cat := range Categories() {
		for _, ct := range cat.Tools {
			if ct.DependsOn == "" {
				continue
			}
			if !slices.ContainsFunc(cat.Tools, func(o CategoryTool) bool { return o.Tool == ct.DependsOn }) {
				t.Errorf("%s/%s depends on %s, which is not in the category", cat.ID, ct.Tool, ct.DependsOn)
			}
		}
	}
}

func TestRequired(t *testing.T) {
	misconfig := func(enabled bool, tools ...string) config.ScanCategories {
		c := categories()
		c.Misconfig = config.CategoryConfig{Enabled: enabled, Tools: tools}
		return c
	}
	tests := []struct {
		name string
		c    config.ScanCategories
		want []ToolID
	}{
		{"the defaults", config.DefaultScanCategories(), []ToolID{ToolTrivy, ToolGitleaks}},
		{"nothing on", categories(), nil},
		// A category that is off keeps its ticks, and requires none of them.
		{"a category off", misconfig(false, "trivy", "kubeconform"), nil},
		{"CI on", categories(CategoryIDCI), []ToolID{ToolPlumber}},
		{"helm with kubeconform", misconfig(true, "kubeconform", "helm"), []ToolID{ToolKubeconform, ToolHelm}},
		// helm renders for kubeconform; without it, it serves nothing.
		{"helm without kubeconform", misconfig(true, "trivy", "helm"), []ToolID{ToolTrivy}},
		// Installed or not, an unticked tool is not required.
		{"kubeconform unticked", misconfig(true, "trivy"), []ToolID{ToolTrivy}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Required(tt.c); !slices.Equal(got, tt.want) {
				t.Errorf("Required = %v, want %v", got, tt.want)
			}
		})
	}
}

// Missing is what the dashboard reports: what is required and absent — plumber
// absent with CI off is not missing, which it used to be.
func TestMissingIsRequiredAndAbsent(t *testing.T) {
	deps := binaries(ToolTrivy)

	if got := deps.Missing(config.DefaultScanCategories()); !slices.Equal(got, []ToolID{ToolGitleaks}) {
		t.Errorf("Missing = %v, want gitleaks alone", got)
	}
	if got := deps.Missing(categories(CategoryIDVuln)); len(got) != 0 {
		t.Errorf("Missing = %v with only vulnerabilities on, want nothing", got)
	}
	if got := deps.Missing(categories(CategoryIDCI)); !slices.Equal(got, []ToolID{ToolPlumber}) {
		t.Errorf("Missing = %v with CI on, want plumber", got)
	}
}

// CanScan answers per target: gitleaks alone can scan a directory and nothing
// else, and a tool that is there but unticked does not count.
func TestCanScanDependsOnTheTarget(t *testing.T) {
	gitleaksOnly := binaries(ToolGitleaks)
	secrets := categories(CategoryIDSecret)

	if !gitleaksOnly.CanScan(secrets, TargetDirectory) {
		t.Error("gitleaks cannot scan a directory for secrets")
	}
	if gitleaksOnly.CanScan(secrets, TargetImage) {
		t.Error("gitleaks was counted as able to scan an image")
	}
	if gitleaksOnly.CanScan(categories(CategoryIDVuln), TargetDirectory) {
		t.Error("an unticked tool was counted as a scanner")
	}
	if (Report{}).CanScan(config.DefaultScanCategories(), TargetDirectory) {
		t.Error("an empty report can scan")
	}
}

// A report that does not hold a tool still names its default image, so the
// error that says it is missing can say what to pull.
func TestAnUnknownToolStatusNamesItsDefaultImage(t *testing.T) {
	st := (Report{}).Status(ToolKubeconform)
	if st.Available || st.Source != ToolSourceNone || st.Image != DefaultKubeconformImage {
		t.Errorf("Status = %+v, want unavailable with the default image", st)
	}
}

// Plumber is reported like every other tool (§3.86): with CI on and plumber
// absent, the scan says the pipeline was not graded rather than nothing.
func TestAMissingPlumberIsReported(t *testing.T) {
	s := newScannerWithDeps(scanFor(CategoryIDCI), everyTool())

	errs := s.missingToolErrors(TargetDirectory)
	if len(errs) != 1 || !strings.Contains(errs[0], "plumber") || !strings.Contains(errs[0], DefaultPlumberImage) {
		t.Errorf("errors = %v, want plumber reported missing with its image", errs)
	}
	if errs := s.missingToolErrors(TargetImage); len(errs) != 0 {
		t.Errorf("image: errors = %v, want none — an image has no pipeline", errs)
	}
}

// A tool ticked in two categories is one missing tool, not two.
func TestAToolMissingForTwoCategoriesIsReportedOnce(t *testing.T) {
	s := newScannerWithDeps(scanFor(CategoryIDVuln, CategoryIDLicense), Report{})

	if errs := s.missingToolErrors(TargetDirectory); len(errs) != 1 {
		t.Errorf("errors = %v, want trivy reported once", errs)
	}
}

// Installed and unticked, a renderer is not used: its charts are reported as
// not rendered, as if it were absent.
func TestAnUntickedRendererIsNotUsed(t *testing.T) {
	opts := k8sSchema()
	opts.Categories.Misconfig = opts.Categories.Misconfig.With(config.ToolHelm, false)
	s := newScannerWithDeps(opts, renderingDeps())

	got := s.kubeconformOptions()
	if available(got.Helm) {
		t.Error("helm was handed to the stage although it is unticked")
	}
	if !available(got.Kustomize) {
		t.Error("kustomize, ticked and installed, was not handed to the stage")
	}
}
