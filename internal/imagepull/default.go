package imagepull

import (
	"time"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/imageupdate"
	"github.com/anthnel/devdesk/internal/remediation"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/trust"
)

// Default is the production wiring for a context: its image_verification
// setting, cosign as detection resolved it (tools nil while detection is still
// running — cosign is then looked up on PATH, and a missing one is a Failed
// verdict, not a crash), the registry through the engine's credentials.
func Default(cfg *config.Config, tools *scan.Report) Deps {
	spec := scan.ToolSpec{Source: scan.ToolSourceBinary}
	if tools != nil {
		spec = tools.Spec(scan.ToolCosign)
	}
	if cfg != nil {
		spec.Args = cfg.Scan.Tools.Cosign.Args
	}
	registry := imageupdate.Default()
	verifier := scan.CosignVerifier{Tool: spec, Creds: registryCreds}
	return Deps{
		Enabled:  cfg == nil || cfg.Scan.VerifiesImages(),
		Policy:   loadPolicy,
		Verifier: trust.Cached(verifier, trust.FileStore{}, time.Now),
		Digest: func(ref string) (string, error) {
			r := remediation.ParseRef(ref)
			tag := r.Tag
			if tag == "" {
				tag = "latest"
			}
			return registry.Digest(r, tag)
		},
		Current: func(ref string) string {
			return localDigest(ref, docker.ImageRepoDigests([]string{ref})[ref])
		},
		Pull: docker.PullImageContext,
		Tag:  docker.TagImage,
	}
}

func loadPolicy() (trust.Policy, error) {
	path, err := trust.PolicyPath()
	if err != nil {
		return trust.Policy{}, err
	}
	p, _, err := trust.Load(path)
	return p, err
}

// registryCreds hands cosign what the engine holds for a host, anonymous
// otherwise — the same credentials a pull uses.
func registryCreds(host string) (string, string) {
	user, pass, _ := docker.GetStoredCreds(host)
	return user, pass
}
