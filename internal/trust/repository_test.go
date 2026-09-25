package trust_test

import (
	"testing"

	"github.com/anthnel/devdesk/internal/remediation"
	"github.com/anthnel/devdesk/internal/trust"
)

// trust.Repository reads a reference the way remediation.ParseRef does; it
// cannot call it (remediation imports scan, which implements trust.Verifier),
// so this holds the two in step.
func TestRepositoryAgreesWithRemediation(t *testing.T) {
	for _, ref := range []string{
		"python", "python:3.12", "docker.io/library/python:3.12", "index.docker.io/bitnami/redis:7",
		"bitnami/redis@sha256:abc", "gcr.io/distroless/static-debian12:nonroot",
		"localhost:5000/demo:1", "registry.corp.example:8443/a/b/c:1@sha256:x", "cgr.dev/chainguard/static",
	} {
		r := remediation.ParseRef(ref)
		host := r.Registry
		if host == "" {
			host = "docker.io"
		}
		if got, want := trust.Repository(ref), host+"/"+r.Repository; got != want {
			t.Errorf("Repository(%q) = %q, remediation reads %q", ref, got, want)
		}
	}
}
