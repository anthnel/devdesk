package scan

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/anthnel/devdesk/internal/k8s"
)

// Why a KSV-0104 finding fired (DEVDESK-686, S-01). Trivy words the finding the
// same for an explicit Unconfined and for a missing seccomp profile, and
// points at the container's block rather than at the line that says
// Unconfined, so neither the message nor the span tells the two apart. The
// manifest is read again at scan time, the way the Kubernetes fixes read it
// (§3.80): the document at the reported line, the container by its name inside
// the reported lines.
const (
	SeccompUnconfined = "unconfined"
	SeccompAbsent     = "absent"
)

// seccompContainer is how KSV-0104 names its container: lower case and double
// quotes, unlike the other KSV rules ("Container 'api' of …").
var seccompContainer = regexp.MustCompile(`container "([^"]+)"`)

// isSeccompRule reports whether id is KSV-0104, in any of the three spellings
// Trivy and the cache have used. This package cannot import remediation, where
// RuleKey lives.
func isSeccompRule(id string) bool {
	id = strings.ReplaceAll(strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(id)), "AVD-"), "-", "")
	return id == "KSV0104" || id == "KSV104"
}

// indexDocuments decodes a manifest once; a variable so a test can count the
// decodes.
var indexDocuments = k8s.IndexDocuments

// annotateSeccomp is annotateSeccompContext with no way to cancel it.
func annotateSeccomp(root string, findings []Finding) {
	annotateSeccompContext(context.Background(), root, findings)
}

// annotateSeccompContext sets SeccompOrigin on the kubernetes KSV-0104 findings
// of a directory scan, reading and decoding each file once however many
// findings point into it. It never fails the scan: whatever it cannot confirm
// leaves the origin empty, which reads as "unknown". It stops between files
// once ctx is done.
func annotateSeccompContext(ctx context.Context, root string, findings []Finding) {
	byFile := map[string][]int{}
	for i, f := range findings {
		if f.Source == SourceTrivyMisconfig && f.IaCType == "kubernetes" && isSeccompRule(f.ID) {
			byFile[f.File] = append(byFile[f.File], i)
		}
	}
	if len(byFile) == 0 {
		return
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		log.Printf("WARN [scan] seccomp origin: cannot open %s: %v", root, err)
		return
	}
	defer func() { _ = dir.Close() }()

	for file, indexes := range byFile {
		if ctx.Err() != nil {
			return
		}
		name := filepath.FromSlash(file)
		if !filepath.IsLocal(name) {
			log.Printf("WARN [scan] seccomp origin: %s is outside the scanned directory", file)
			continue
		}
		content, err := k8s.ReadManifestIn(dir, name)
		if err != nil {
			log.Printf("WARN [scan] seccomp origin: cannot read %s: %v", file, err)
			continue
		}
		documents := indexDocuments(content)
		for _, i := range indexes {
			if ctx.Err() != nil {
				return
			}
			findings[i].SeccompOrigin = seccompOriginIn(documents, findings[i])
		}
	}
}

// seccompOrigin classifies one KSV-0104 finding from the manifest's bytes:
// SeccompUnconfined, SeccompAbsent, or "" when it cannot tell (the file
// changed since Trivy read it, the container is not at the reported lines, a
// profile is now set).
func seccompOrigin(content []byte, f Finding) string {
	return seccompOriginIn(k8s.IndexDocuments(content), f)
}

// seccompOriginIn is seccompOrigin on a manifest that is already decoded.
func seccompOriginIn(documents *k8s.DocumentIndex, f Finding) string {
	m := seccompContainer.FindStringSubmatch(f.Message)
	if m == nil || f.Line <= 0 {
		return ""
	}
	doc, ok := documents.At(f.Line)
	if !ok {
		return ""
	}
	// The message names the container, nothing else: two containers of that
	// name inside the reported lines (an init container and a container may
	// share one) cannot be told apart, so none is picked.
	var (
		spec  k8s.PodSpec
		match *yaml.Node
		found int
	)
	for _, candidate := range k8s.PodSpecs(doc) {
		for _, c := range candidate.Containers {
			if k8s.Scalar(c, "name") != m[1] {
				continue
			}
			if f.EndLine > 0 && (c.Line < f.Line || c.Line > f.EndLine) {
				continue
			}
			spec, match = candidate, c
			found++
		}
	}
	if found != 1 {
		return ""
	}
	switch t := k8s.SeccompType(doc, spec, match); {
	case strings.EqualFold(t, "unconfined"):
		return SeccompUnconfined
	case t == "":
		return SeccompAbsent
	}
	return ""
}
