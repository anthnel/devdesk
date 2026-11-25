package scan

import (
	"bytes"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Flux repositories (DEVDESK-783). The kustomize-controller of Flux builds an
// overlay with LoadRestrictionsNone, bounded to the root of the artifact (the
// root of the git repository), so an aggregator such as kustomizations/dev may
// list, as resources, Flux Kustomization files of neighbouring folders. kustomize
// with its default restriction refuses them, and the scan reported a HIGH for
// something Flux builds without a problem.
//
// An overlay that kustomize refused for the restriction is built once more
// without it when it is a Flux one: fluxOverlay reads its kustomization the way
// the gate (DEVDESK-771) does and looks for a listed FILE one of whose documents
// is a resource of a group *.toolkit.fluxcd.io.

const (
	// fluxGroupSuffix ends the apiVersion group of every Flux resource
	// (kustomize.toolkit.fluxcd.io, helm.toolkit.fluxcd.io, source.toolkit.fluxcd.io…).
	fluxGroupSuffix = ".toolkit.fluxcd.io"

	// loadRestrictorFlag is the one argument DevDesk adds to build as Flux does.
	loadRestrictorFlag     = "--load-restrictor"
	loadRestrictorNoneFlag = loadRestrictorFlag + "=LoadRestrictionsNone"

	// fluxBuiltNote ends the Description of a finding of an overlay that was built
	// as Flux builds it and still failed: the failure is not the restrictor's.
	fluxBuiltNote = "Built as Flux's kustomize-controller does: files outside the overlay folder allowed, within the repository."

	// fluxKeptNote ends the Description of a finding of a Flux overlay the restrictor
	// refused and that was not built again, because allow_remote_bases turns the
	// check of its references off (DEVDESK-783).
	fluxKeptNote = "This overlay lists Flux resources, which Flux builds without the load restrictor. DevDesk keeps it " +
		"while scan.tools.kustomize.allow_remote_bases is true, because that setting turns off its check of what an " +
		"overlay references."

	// maxFluxDocuments bounds the documents of one file that are looked at.
	maxFluxDocuments = 1024
)

// userSetsLoadRestrictor reports whether args, the user's own arguments, already
// choose the load restrictor, in the `--load-restrictor=X` or the
// `--load-restrictor X` form. What the user wrote is theirs: DevDesk adds
// nothing next to it and does not build again.
func userSetsLoadRestrictor(args []string) bool {
	for _, a := range args {
		if a == loadRestrictorFlag || strings.HasPrefix(a, loadRestrictorFlag+"=") {
			return true
		}
	}
	return false
}

// fluxOverlay reports whether the overlay (absolute), inside root (absolute; the
// links are resolved here as vetOverlayIn does), is a Flux one: its kustomization
// lists in `resources` or `bases` at least one FILE with a document whose
// apiVersion group is exactly <name>.toolkit.fluxcd.io.
//
// It reads with the gate's rules: a reference must be localRef, inside root once
// the links are resolved, a regular file (a pipe is never opened) of at most
// lim.FileBytes. A folder is not followed, and `components` and `patches` are
// not looked at. Whatever it cannot read counts as "not Flux".
func fluxOverlay(root, overlay string, lim gateLimits) bool {
	root, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	if root, err = filepath.EvalSymlinks(root); err != nil {
		return false
	}
	if overlay, err = filepath.Abs(overlay); err != nil {
		return false
	}
	real, err := filepath.EvalSymlinks(overlay)
	if err != nil || !within(root, real) {
		return false
	}
	g := &gate{root: root, lim: lim, limit: "the repository"}
	file, v := g.kustomizationFile(real)
	if file == "" || v.Kind != gateOK {
		return false
	}
	// The kustomization file may itself be a link: what is read is what it names.
	if target, err := filepath.EvalSymlinks(file); err != nil || !within(root, target) {
		return false
	}
	node, reason := g.readYAML(file)
	if reason != "" {
		return false
	}
	site := gateSite{file: g.rel(file), dir: real}
	seen := map[string]bool{}
	for _, field := range []string{"resources", "bases"} {
		if g.listsFluxFile(fieldNamed(node, field), field, site, seen) {
			return true
		}
	}
	return false
}

// fieldNamed is the value of the field of a kustomization, whatever the case of
// its name: kustomize matches field names without regard to it.
func fieldNamed(n *yaml.Node, name string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if strings.EqualFold(n.Content[i].Value, name) {
			return n.Content[i+1]
		}
	}
	return nil
}

// listsFluxFile reports whether an entry of the list is a regular file that holds
// a Flux document. seen holds the files already read, by real path.
func (g *gate) listsFluxFile(list *yaml.Node, field string, site gateSite, seen map[string]bool) bool {
	if list == nil || list.Kind != yaml.SequenceNode {
		return false
	}
	for _, e := range list.Content {
		if e.Kind != yaml.ScalarNode || e.Tag != "!!str" {
			continue
		}
		real, found, v := g.resolve(e.Value, field, site)
		if v.Kind != gateOK || !found || seen[real] || len(seen) >= g.lim.Visited {
			continue
		}
		seen[real] = true
		data, reason := readGateFile(real, min(g.lim.FileBytes, g.lim.TotalBytes-g.read))
		if reason != "" { // a folder, a pipe, a file too large or unreadable
			continue
		}
		g.read += int64(len(data))
		if hasFluxDocument(data) {
			return true
		}
	}
	return false
}

// hasFluxDocument reports whether a document of the YAML stream data has an
// apiVersion of a Flux group. A stream that stops being YAML ends the search.
func hasFluxDocument(data []byte) bool {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	for range maxFluxDocuments {
		var doc yaml.Node
		if err := dec.Decode(&doc); err != nil {
			return false
		}
		if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 {
			continue
		}
		if v := mapGet(doc.Content[0], "apiVersion"); v != nil && v.Kind == yaml.ScalarNode && isFluxAPIVersion(v.Value) {
			return true
		}
	}
	return false
}

// isFluxAPIVersion reports whether an apiVersion is <name>.toolkit.fluxcd.io/<version>
// with one label before the suffix: a group that goes on after it, or that only
// ends like it, is not Flux's.
func isFluxAPIVersion(apiVersion string) bool {
	group, version, ok := strings.Cut(apiVersion, "/")
	if !ok || version == "" || strings.Contains(version, "/") {
		return false
	}
	label, ok := strings.CutSuffix(group, fluxGroupSuffix)
	return ok && label != "" && !strings.Contains(label, ".")
}
