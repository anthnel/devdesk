package scan

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/dockerfile"
	"github.com/anthnel/devdesk/internal/oci"
	"github.com/anthnel/devdesk/internal/trust"
)

// The signature check of the base images a repository builds on (§3.82).
//
// A base image in use that violates its rule is worse than any candidate: it is
// what the next build takes. It is a finding of the Misconfiguration category,
// anchored on the FROM line, on the build-context check's model (§3.81): DevDesk
// runs it, no scanner is ticked for it. Every stage counts, a build stage too —
// a compromised builder writes the binary the final image ships.
//
// A finding states a fact, so only a verdict that proves something produces
// one: a signature by someone else, or none where a rule asks for one. A check
// that could not run, or whose Unsigned was inferred from a failure (key mode,
// trust.ErrUnproven), is an error of the stage, never a finding.

// The rule ids, in DevDesk's own namespace.
const (
	SignatureMismatchID = "DEVDESK-SIG-001"
	SignatureUnsignedID = "DEVDESK-SIG-002"
)

// signatureStageTimeout bounds the stage: a few cosign runs per base image.
const signatureStageTimeout = 5 * time.Minute

// SignatureDeps is the production wiring of a check: the user's policy, cosign
// as tool resolved it and cached, the registry through the engine's
// credentials. The scan and every pull use this one.
func SignatureDeps(tool ToolSpec) trust.CheckDeps {
	return trust.CheckDeps{
		Policy:   trust.LoadDefault,
		Verifier: trust.Cached(CosignVerifier{Tool: tool, Creds: registryCreds}, trust.FileStore{}, time.Now),
		Digest:   registryDigest,
	}
}

// signatureDeps is SignatureDeps, swapped by tests.
var signatureDeps = SignatureDeps

// registryCreds hands cosign what the engine holds for a host, anonymous
// otherwise — the same credentials a pull uses.
func registryCreds(host string) (string, string) {
	user, pass, _ := docker.GetStoredCreds(host)
	return user, pass
}

// registryDigest asks the registry what ref's tag points to now. A reference
// with no tag is `latest`, as the engine reads it.
func registryDigest(ref string) (string, error) {
	host, repo, _ := strings.Cut(trust.Repository(ref), "/")
	registry := host
	if host == "docker.io" {
		registry = ""
	}
	user, pass, _ := docker.GetStoredCreds(host)
	return oci.ManifestDigest(oci.RegistryAPIBase(registry), repo, tagOf(ref), user, pass)
}

// tagOf is a reference's tag, "latest" when it has none.
func tagOf(ref string) string {
	s, _, _ := strings.Cut(ref, "@")
	if i := strings.LastIndex(s, ":"); i > strings.LastIndex(s, "/") {
		return s[i+1:]
	}
	return "latest"
}

// checksSignatures reports whether the scan runs the check: a directory, the
// Misconfiguration category on, and a context that verifies images.
func (s *Scanner) checksSignatures(targetType TargetType) bool {
	return s.checksBuildContext(targetType) && s.options.ImageVerification != config.ImageVerificationOff
}

// runSignatureStage checks every base image of every Dockerfile under target,
// each reference once however many stages name it.
func runSignatureStage(ctx context.Context, target string, deps trust.CheckDeps, result *Result, mu *sync.Mutex, notify func(ProgressUpdate)) {
	const stage, label = "signature", "Base image signatures"
	notify(ProgressUpdate{Stage: stage, Label: label, Status: StageRunning})
	ctx, cancel := context.WithTimeout(ctx, signatureStageTimeout)
	defer cancel()

	findings, errs := checkBaseImageSignatures(ctx, target, deps)
	mu.Lock()
	defer mu.Unlock()
	result.Findings = append(result.Findings, findings...)
	for _, err := range errs {
		recordStageError(result, "signature", err)
	}
	status := StageDone
	if len(errs) > 0 {
		status = StageError
	}
	notify(ProgressUpdate{Stage: stage, Label: label, Status: status})
}

func checkBaseImageSignatures(ctx context.Context, target string, deps trust.CheckDeps) ([]Finding, []error) {
	files, _, err := dockerfile.Find(target)
	if errors.Is(err, fs.ErrNotExist) {
		// The scanners asked to read it say so themselves: one missing
		// directory is one error, not one per stage — build-context's rule.
		return nil, nil
	}
	if err != nil {
		return nil, []error{err}
	}
	verdicts := map[string]trust.Result{}
	reported := map[string]bool{}
	var findings []Finding
	var errs []error
	for _, rel := range files {
		content, err := os.ReadFile(filepath.Join(target, filepath.FromSlash(rel))) //nolint:gosec // a Dockerfile of the directory being scanned
		if err != nil {
			log.Printf("ERROR [scan/signature] read %s: %v", rel, err)
			continue
		}
		for _, st := range dockerfile.Parse(content).Stages {
			if st.Kind != dockerfile.KindImage || st.Image == "" {
				continue
			}
			res, seen := verdicts[st.Image]
			if !seen {
				res, _ = trust.Check(ctx, st.Image, "", deps)
				verdicts[st.Image] = res
			}
			if f, ok := signatureFinding(rel, st, res); ok {
				findings = append(findings, f)
			} else if res.Decision == trust.Block && (res.Verdict == trust.Failed || !res.Proven()) && !reported[st.Image] {
				// A rule asked for a guarantee that could not be checked — or
				// whose answer was inferred from a failure: said once per
				// image, as the stage's error, not as a fact.
				reported[st.Image] = true
				errs = append(errs, fmt.Errorf("%s: %s", st.Image, res.Reason()))
			}
		}
	}
	return findings, errs
}

// signatureFinding is the finding a verdict makes, if it proves something.
func signatureFinding(file string, st dockerfile.Stage, res trust.Result) (Finding, bool) {
	f := Finding{
		Source: SourceSignature, IaCType: "dockerfile",
		File: file, Line: st.Line, EndLine: st.Line,
		Message: res.Reason(),
		Resolution: "Pick a candidate whose signature holds in the Remediation tab. If the publisher changed its " +
			"signing identity legitimately, update the rule in ~/.devdesk/trust.yaml.",
	}
	switch {
	case res.Verdict == trust.IdentityMismatch:
		f.ID, f.Severity = SignatureMismatchID, SeverityCritical
		f.Title = "Base image " + st.Image + " is signed by an unexpected identity"
		f.Description = "The image carries a valid signature, but not by the identity its rule expects. " +
			"A tag republished with altered content and re-signed by someone else looks exactly like this, and no CVE scan would show it."
	case res.Verdict == trust.Unsigned && res.Decision == trust.Block && res.Proven():
		f.ID, f.Severity = SignatureUnsignedID, SeverityHigh
		f.Title = "Base image " + st.Image + " is not signed as its rule requires"
		f.Description = "A rule says images of this repository are signed, and this digest carries no signature it accepts. " +
			"A republished tag has a new digest that none of the old signatures cover, which is what this looks like."
	default:
		return Finding{}, false
	}
	return f, true
}
