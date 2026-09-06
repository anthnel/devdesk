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
// and belongs to whoever starts the scan. So is LoadForgeToken, which needs the
// context's secret store — the caller has it, this function does not.
//
// It takes the whole Config rather than its Scan section because the CI stage
// needs the forge: which platform a context targets is what decides whether a
// repository is graded at all (§3.42), and that is not a scan setting.
func OptionsFromConfig(cfg *config.Config) ScanOptions {
	c := cfg.Scan
	// The address is kept in the config even when the checkbox is off, so
	// switching it back on does not lose what was typed — but a scan must not
	// see it until the checkbox says client-server mode is wanted.
	trivyServer := ""
	if c.UseTrivyServer {
		trivyServer = c.TrivyServer
	}
	return ScanOptions{
		EnableCIScore:   c.EnableCIScore,
		PlumberSource:   c.PlumberSource,
		PlumberPath:     c.PlumberPath,
		PlumberImage:    c.PlumberImage,
		PlumberConfig:   c.PlumberConfig,
		Forge:           cfg.Forge,
		EnableVuln:      c.EnableVuln,
		EnableSecret:    c.EnableSecret,
		EnableLicense:   c.EnableLicense,
		EnableMisconfig: c.EnableMisconfig,
		TrivySource:     c.TrivySource,
		TrivyPath:       c.TrivyPath,
		TrivyImage:      c.TrivyImage,
		GitleaksSource:  c.GitleaksSource,
		GitleaksPath:    c.GitleaksPath,
		GitleaksImage:   c.GitleaksImage,
		TrivyServer:     trivyServer,
		IgnoreUnfixed:   c.IgnoreUnfixed,
		IgnoreEOL:       c.IgnoreEOL,
		GitleaksHistory: c.GitleaksHistory,
		GitleaksConfig:  c.GitleaksConfig,
	}
}
