package scan

import "github.com/anthnel/devdesk/internal/config"

// OptionsFromConfig builds the scan options a context is configured with.
//
// This is the only way ScanOptions is assembled. Three call sites used to do it
// by hand — the security form, the workspaces list and the images list — and
// they had drifted: only the form set IgnoreEOL, so the option applied when
// scanning from the form and silently did not from either list (D26).
//
// OnProgress is deliberately left nil: it is per-scan wiring, not configuration,
// and belongs to whoever starts the scan.
func OptionsFromConfig(c config.ScanConfig) ScanOptions {
	return ScanOptions{
		EnableVuln:      c.EnableVuln,
		EnableSecret:    c.EnableSecret,
		EnableLicense:   c.EnableLicense,
		EnableMisconfig: c.EnableMisconfig,
		GenerateSBOM:    c.GenerateSBOM,
		SBOMOutputDir:   c.SBOMOutputDir,
		TrivyImage:      c.TrivyImage,
		GitleaksImage:   c.GitleaksImage,
		TrivyServer:     c.TrivyServer,
		IgnoreUnfixed:   c.IgnoreUnfixed,
		IgnoreEOL:       c.IgnoreEOL,
		GitleaksHistory: c.GitleaksHistory,
		GitleaksConfig:  c.GitleaksConfig,
	}
}
