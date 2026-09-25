package imagepull

import (
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/scan"
)

// Default is the production wiring for a context: its image_verification
// setting, and the check the scan uses too (scan.SignatureDeps) — cosign as
// detection resolved it (tools nil while detection is still running: cosign is
// then looked up on PATH, and a missing one is a Failed verdict, not a crash).
func Default(cfg *config.Config, tools *scan.Report) Deps {
	spec := scan.ToolSpec{Source: scan.ToolSourceBinary}
	if tools != nil {
		spec = tools.Spec(scan.ToolCosign)
	}
	if cfg != nil {
		spec.Args = cfg.Scan.Tools.Cosign.Args
	}
	check := scan.SignatureDeps(spec)
	return Deps{
		Enabled:  cfg == nil || cfg.Scan.VerifiesImages(),
		Policy:   check.Policy,
		Verifier: check.Verifier,
		Digest:   check.Digest,
		Current: func(ref string) string {
			return LocalDigest(ref, docker.ImageRepoDigests([]string{ref})[ref])
		},
		Pull: docker.PullImageContext,
		Tag:  docker.TagImage,
	}
}
