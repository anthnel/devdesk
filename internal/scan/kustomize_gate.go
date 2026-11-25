package scan

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// The native kustomize gate (DEVDESK-771). Go never applies a proxy to the
// loopback, so offlineEnviron cannot stop a native kustomize that reads an
// overlay naming http://127.0.0.1:8080/x.yaml from sending a GET to a service of
// this machine. The overlay is therefore read first, and kustomize is started
// only when every reference in it, and in everything it pulls in, can only name
// a path of this repository.
//
// The gate is a closed whitelist, not a list of what to refuse: a reference is
// accepted when localRef says it can only be a local path, the YAML is read the
// closed way (no anchor, alias, merge key, explicit tag, duplicate key, key that
// is not ASCII, second document), every field of the kustomization is one the
// gate knows, and every plugin is a builtin one. What it cannot judge is refused
// as such: kustomize is not started either. The fields and the plugin kinds are
// the ones of kustomize v5.8.3 (api v0.21.3, types.Kustomization and the
// builtinhelpers factories).

// gateKind is what the gate says about an overlay.
type gateKind int

const (
	// gateOK: every reference is local, kustomize may be started.
	gateOK gateKind = iota
	// gateRemote: a reference that is not a path of the repository.
	gateRemote
	// gateUnchecked: something the gate cannot judge, or cannot read.
	gateUnchecked
)

func (k gateKind) String() string {
	switch k {
	case gateOK:
		return "ok"
	case gateRemote:
		return "remote"
	case gateUnchecked:
		return "unchecked"
	}
	return fmt.Sprintf("gateKind(%d)", int(k))
}

// gateVerdict is the answer for one overlay. File is relative to the limit, with
// forward slashes; Field is the YAML path of the entry, with list indices
// (resources[1], patches[0].path); for an inline plugin configuration it is the
// field of the kustomization followed by the field of the plugin
// (transformers[0].path), for a plugin file the field inside that file. Ref is
// what localRef was given.
type gateVerdict struct {
	Kind                     gateKind
	File, Field, Ref, Reason string
}

// gateLimits bound what the gate reads of a repository that is not trusted.
type gateLimits struct {
	// Depth is the number of levels of overlay below the first; Visited the
	// number of distinct kustomizations read.
	Depth, Visited int
	// FileBytes is the size of one file; TotalBytes of every file read for one
	// overlay.
	FileBytes, TotalBytes int64
}

// defaultGateLimits keep the gate itself cheap: YAML read into nodes costs about a
// hundred times the size of the file in memory, so a megabyte per file and eight
// for an overlay bound what a repository that is not trusted can make it spend.
var defaultGateLimits = gateLimits{Depth: 16, Visited: 256, FileBytes: 1 << 20, TotalBytes: 8 << 20}

// localRef reports whether ref can only name a path of this repository. It
// accepts a non-empty string without a control character, without ':' (a scheme,
// a port, a drive), '@' (an ssh user) or '?' (a query), that is neither absolute
// nor starts with a separator, and whose first segment is not github.com (which
// kustomize reads as a git repository even when such a folder exists). reason is
// "" when it is local, and says why not otherwise.
func localRef(ref string) (ok bool, reason string) {
	switch {
	case ref == "":
		return false, "it is empty"
	case strings.IndexFunc(ref, unicode.IsControl) >= 0:
		return false, "it holds a control character"
	case strings.ContainsAny(ref, ":@?"):
		return false, "it holds a character that a URL, an address or a drive uses"
	case strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, `\`) || filepath.IsAbs(ref):
		return false, "it is an absolute path"
	}
	if first, _, _ := strings.Cut(ref, "/"); strings.EqualFold(first, "github.com") {
		return false, "kustomize reads a name that starts with github.com as a git repository"
	}
	return true, ""
}

// ── Reading a file the closed way ────────────────────────────────────────────

// readGateYAML reads one file of an overlay and returns the mapping at the top of
// its only document. A reason says why it cannot be judged, and the node is nil.
func readGateYAML(file string, maxBytes int64) (*yaml.Node, string) {
	data, reason := readGateFile(file, maxBytes)
	if reason != "" {
		return nil, reason
	}
	return parseGateYAML(data)
}

// readGateFile reads a regular file of at most maxBytes.
func readGateFile(file string, maxBytes int64) ([]byte, string) {
	maxBytes = max(maxBytes, 0)
	info, err := os.Stat(file)
	if err != nil {
		return nil, "it cannot be read (" + errReason(err) + ")"
	}
	if !info.Mode().IsRegular() {
		return nil, "it is not a regular file"
	}
	tooBig := fmt.Sprintf("it is larger than %d bytes", maxBytes)
	if info.Size() > maxBytes {
		return nil, tooBig
	}
	f, err := os.Open(file) //nolint:gosec // a file of the repository being scanned, vetted above
	if err != nil {
		return nil, "it cannot be read (" + errReason(err) + ")"
	}
	defer func() { _ = f.Close() }()
	// The size may have changed since the stat: one byte more says it did.
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, "it cannot be read (" + errReason(err) + ")"
	}
	if int64(len(data)) > maxBytes {
		return nil, tooBig
	}
	return data, ""
}

// errReason is err without the path an os error carries.
func errReason(err error) string {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Err.Error()
	}
	return err.Error()
}

// reasonNoDocument is the reason for a file with nothing in it but comments.
const reasonNoDocument = "it holds no YAML document"

// parseGateYAML returns the mapping at the top of the only document of data.
func parseGateYAML(data []byte) (*yaml.Node, string) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, reasonNoDocument
		}
		return nil, "it is not valid YAML"
	}
	var next yaml.Node
	if err := dec.Decode(&next); !errors.Is(err, io.EOF) {
		return nil, "it holds more than one YAML document"
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, "its top level is not a mapping"
	}
	top := doc.Content[0]
	if problem := nodeProblem(top); problem != "" {
		return nil, "it holds " + problem
	}
	return top, ""
}

// nodeProblem names what makes a node something the gate does not read: kustomize
// resolves anchors, aliases and merge keys, applies tags, matches field names
// without regard to case (and, for a non-ASCII letter, to Unicode folding) and
// keeps the last of two equal keys, so a reading that does not do the same is
// not a reading of what kustomize will do.
func nodeProblem(n *yaml.Node) string {
	switch {
	case n.Kind == yaml.AliasNode:
		return "an alias"
	case n.Anchor != "":
		return "an anchor"
	case n.Style&yaml.TaggedStyle != 0:
		return "an explicit tag"
	}
	switch n.Kind {
	case yaml.MappingNode:
		seen := map[string]bool{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i]
			if problem := keyProblem(key, seen); problem != "" {
				return problem
			}
			if problem := nodeProblem(n.Content[i+1]); problem != "" {
				return problem
			}
		}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			if problem := nodeProblem(c); problem != "" {
				return problem
			}
		}
	}
	return ""
}

func keyProblem(key *yaml.Node, seen map[string]bool) string {
	if problem := nodeProblem(key); problem != "" {
		return problem
	}
	switch {
	case key.Kind == yaml.ScalarNode && key.Value == "<<":
		return "a merge key"
	case key.Kind != yaml.ScalarNode || key.Tag != "!!str":
		return "a key that is not a string"
	}
	for i := 0; i < len(key.Value); i++ {
		if key.Value[i] >= 0x80 {
			return "a key that is not ASCII"
		}
	}
	folded := strings.ToLower(key.Value)
	if seen[folded] {
		return "a duplicate key"
	}
	seen[folded] = true
	return ""
}

// ── What each field is ───────────────────────────────────────────────────────

type gateRuleKind int

const (
	// gateData: no reference (a name, a label, an inline patch, a literal).
	gateData gateRuleKind = iota
	// gateFile: a reference to a file, not followed.
	gateFile
	// gateBase: a reference to a file or to a directory with a kustomization,
	// which is read in turn.
	gateBase
	// gateFileSource: a files entry of a generator, `path` or `key=path`.
	gateFileSource
	// gatePlugin: a generator, transformer or validator, inline or the path of a
	// configuration file. A directory is refused: kustomize reads the resources of
	// its kustomization as plugin configurations, which the gate does not follow.
	gatePlugin
	// gatePatch: a strategic merge patch, inline or a path.
	gatePatch
	// gateNever: a value that names a program or its arguments: always refused.
	gateNever
	gateList
	gateMap
)

// gateRule says how the value of a field is judged: a list judges each of its
// elements by elem, a mapping each of its entries by fields (an entry it does
// not list holds no reference).
type gateRule struct {
	kind   gateRuleKind
	elem   *gateRule
	fields map[string]gateRule
}

func listOf(r gateRule) gateRule { return gateRule{kind: gateList, elem: &r} }

func mapOf(fields map[string]gateRule) gateRule { return gateRule{kind: gateMap, fields: fields} }

func lookupRule(fields map[string]gateRule, key string) (gateRule, bool) {
	for name, r := range fields {
		if strings.EqualFold(name, key) {
			return r, true
		}
	}
	return gateRule{}, false
}

var (
	dataRule       = gateRule{}
	fileRule       = gateRule{kind: gateFile}
	baseRule       = gateRule{kind: gateBase}
	fileSourceRule = gateRule{kind: gateFileSource}
	pluginRule     = gateRule{kind: gatePlugin}
	patchRule      = gateRule{kind: gatePatch}
	neverRule      = gateRule{kind: gateNever}
)

// pathOnly is a mapping whose `path` holds a reference: a patch, a replacement.
var pathOnly = mapOf(map[string]gateRule{"path": fileRule})

// kvSources are the fields of a generator of configMaps or secrets that name a
// file: a configMapGenerator entry and the configuration of the builtin plugin
// hold the same ones.
var kvSources = map[string]gateRule{
	"files": listOf(fileSourceRule),
	"envs":  listOf(fileRule),
	"env":   fileRule,
}

// kustomizationRules lists every top level field of a kustomization of
// kustomize v5.8.3. A field that is not here is refused as unknown (kustomize
// rejects it too, but the gate does not rely on that).
var kustomizationRules = map[string]gateRule{
	"apiVersion": dataRule, "kind": dataRule, "metadata": dataRule,
	"namePrefix": dataRule, "nameSuffix": dataRule, "namespace": dataRule,
	"commonLabels": dataRule, "labels": dataRule, "commonAnnotations": dataRule,
	"images": dataRule, "imageTags": dataRule, "replicas": dataRule, "vars": dataRule,
	"sortOptions": dataRule, "generatorOptions": dataRule, "buildMetadata": dataRule,

	"resources": listOf(baseRule), "bases": listOf(baseRule), "components": listOf(baseRule),
	"crds": listOf(fileRule), "configurations": listOf(fileRule),
	"openapi": pathOnly,

	"patches": listOf(pathOnly), "patchesJson6902": listOf(pathOnly),
	"patchesStrategicMerge": listOf(patchRule),
	"replacements":          listOf(pathOnly),

	"configMapGenerator": listOf(mapOf(kvSources)),
	"secretGenerator":    listOf(mapOf(kvSources)),

	"helmGlobals": mapOf(map[string]gateRule{"chartHome": fileRule, "configHome": fileRule}),
	// A chart name is a reference too: helm pulls what an oci:// name says.
	"helmCharts": listOf(mapOf(map[string]gateRule{
		"name": fileRule, "repo": fileRule, "valuesFile": fileRule, "additionalValuesFiles": listOf(fileRule), "chartHome": fileRule,
	})),
	"helmChartInflationGenerator": listOf(mapOf(map[string]gateRule{
		"chartName": fileRule, "chartRepoUrl": fileRule, "chartHome": fileRule, "helmHome": fileRule, "values": fileRule,
		"helmBin": neverRule, "extraArgs": neverRule,
	})),

	"generators": listOf(pluginRule), "transformers": listOf(pluginRule), "validators": listOf(pluginRule),
}

// builtinPlugins are the kinds of apiVersion builtin that kustomize v5.8.3 loads
// (builtinhelpers), with the fields of their configuration that name a file. The
// kinds with no such field read nothing from the file system: they take names,
// labels, an image, a count or a policy, so they are accepted as they are.
var builtinPlugins = map[string]map[string]gateRule{
	"ConfigMapGenerator": kvSources, "SecretGenerator": kvSources,
	"PatchTransformer": {"path": fileRule}, "PatchJson6902Transformer": {"path": fileRule},
	"PatchStrategicMergeTransformer": {"paths": listOf(patchRule)},
	"ReplacementTransformer":         {"replacements": listOf(pathOnly)},
	"ValueAddTransformer":            {"targetFilePath": fileRule},
	"HelmChartInflationGenerator": {
		"name": fileRule, "chartHome": fileRule, "configHome": fileRule, "repo": fileRule, "valuesFile": fileRule,
		"additionalValuesFiles": listOf(fileRule),
	},

	"AnnotationsTransformer": nil, "LabelTransformer": nil, "NamespaceTransformer": nil,
	"PrefixTransformer": nil, "SuffixTransformer": nil, "PrefixSuffixTransformer": nil,
	"ImageTagTransformer": nil, "ReplicaCountTransformer": nil, "HashTransformer": nil,
	"IAMPolicyGenerator": nil,
}

// functionAnnotations make kustomize run a program or a container for a plugin.
var functionAnnotations = []string{"config.kubernetes.io/function", "config.k8s.io/function"}

// kustomizationNames are the file names kustomize recognizes.
var kustomizationNames = []string{"kustomization.yaml", "kustomization.yml", "Kustomization"}

// ── The walk ─────────────────────────────────────────────────────────────────

// gate is one run of vetOverlay.
type gate struct {
	ctx     context.Context //nolint:containedctx // one run of the gate, never stored
	root    string          // the limit, absolute, symbolic links resolved
	lim     gateLimits
	visited map[string]bool // directories entered, by real path
	count   int             // kustomizations read
	read    int64           // bytes read
	bases   []string        // the directories the kustomization being read pulls in
	limit   string          // what the limit is called in a reason
	cache   *gateCache
}

// gateCache remembers, for the whole pass of a scan, the directories whose
// kustomization and everything it pulls in were judged local, so that overlays
// that share a base read it once. Only a verdict of OK is kept, and only once the
// whole overlay is OK: a directory met while a cycle was still open is not known
// to be fine before that. A nil cache remembers nothing.
type gateCache struct {
	mu sync.Mutex
	ok map[string]bool
}

func (c *gateCache) has(real string) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ok[real]
}

func (c *gateCache) add(dirs map[string]bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ok == nil {
		c.ok = map[string]bool{}
	}
	for d := range dirs {
		c.ok[d] = true
	}
}

// gateRun is what a caller that asks about many overlays of one scan adds.
type gateRun struct {
	cache *gateCache
	// limit is how the limit of the paths is called in a reason: "the scanned
	// folder" or "the repository" (the default).
	limit string
}

// gateSite is where a value was read: file is the file it is in (relative to
// the limit), dir the directory its references are relative to (the one of the
// kustomization, also for a plugin file), depth how far below the first overlay.
type gateSite struct {
	file  string
	dir   string
	depth int
}

func (s gateSite) remote(field, ref, reason string) gateVerdict {
	return gateVerdict{Kind: gateRemote, File: s.file, Field: field, Ref: ref, Reason: reason}
}

func (s gateSite) unchecked(field, ref, reason string) gateVerdict {
	return gateVerdict{Kind: gateUnchecked, File: s.file, Field: field, Ref: ref, Reason: reason}
}

// vetOverlay reads the overlay directory (absolute) and everything it pulls in,
// inside root (absolute; the gate resolves symbolic links itself).
func vetOverlay(ctx context.Context, root, overlay string, lim gateLimits) gateVerdict {
	return vetOverlayIn(ctx, root, overlay, lim, gateRun{})
}

// vetOverlayIn is vetOverlay for one pass of a scan.
func vetOverlayIn(ctx context.Context, root, overlay string, lim gateLimits, run gateRun) gateVerdict {
	if run.limit == "" {
		run.limit = "the repository"
	}
	unchecked := func(reason string) gateVerdict { return gateVerdict{Kind: gateUnchecked, Reason: reason} }
	root, err := filepath.Abs(root)
	if err != nil {
		return unchecked("the limit of the paths cannot be resolved")
	}
	if root, err = filepath.EvalSymlinks(root); err != nil {
		return unchecked("the limit of the paths cannot be resolved")
	}
	if overlay, err = filepath.Abs(overlay); err != nil {
		return unchecked("the overlay cannot be resolved")
	}
	real, err := filepath.EvalSymlinks(overlay)
	switch {
	case isMissing(err):
		return gateVerdict{} // kustomize reports it, and fetches nothing
	case err != nil:
		return unchecked("the overlay cannot be resolved")
	case !within(root, real):
		return unchecked("the overlay is outside " + run.limit)
	}
	g := &gate{ctx: ctx, root: root, lim: lim, visited: map[string]bool{}, limit: run.limit, cache: run.cache}
	v := g.dir(real, 0)
	if v.Kind == gateOK {
		run.cache.add(g.visited)
	}
	return v
}

// within reports whether p is root or below it, lexically.
func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// isMissing reports whether err says a path is not there (or has a file where a
// directory should be): nothing for kustomize to load, so nothing to fetch.
func isMissing(err error) bool {
	return err != nil && (errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR))
}

// rel is p relative to the limit, with forward slashes.
func (g *gate) rel(p string) string {
	r, err := filepath.Rel(g.root, p)
	if err != nil {
		return filepath.ToSlash(p)
	}
	return filepath.ToSlash(r)
}

// dir judges the kustomization of one directory (a real path inside the limit)
// and, through it, everything it pulls in. A directory with none holds nothing
// to follow.
func (g *gate) dir(real string, depth int) gateVerdict {
	if g.visited[real] || g.cache.has(real) {
		return gateVerdict{}
	}
	g.visited[real] = true
	where := gateSite{file: g.rel(real), dir: real, depth: depth}
	switch {
	case g.ctx.Err() != nil:
		return where.unchecked("", "", "the check was cancelled")
	case depth > g.lim.Depth:
		return where.unchecked("", "", fmt.Sprintf("overlays are nested more than %d levels deep", g.lim.Depth))
	}
	file, v := g.kustomizationFile(real)
	if file == "" {
		return v
	}
	if g.count++; g.count > g.lim.Visited {
		return where.unchecked("", "", fmt.Sprintf("more than %d kustomizations are pulled in", g.lim.Visited))
	}
	site := gateSite{file: g.rel(file), dir: real, depth: depth}
	bases, v := g.kustomization(file, site)
	if v.Kind != gateOK {
		return v
	}
	// The kustomization is out of memory before its bases are read in turn.
	for _, base := range bases {
		if v := g.dir(base, depth+1); v.Kind != gateOK {
			return v
		}
	}
	return gateVerdict{}
}

// kustomization judges the file of a kustomization. The directories it pulls in
// are returned, not read: this function's YAML is released first.
func (g *gate) kustomization(file string, site gateSite) ([]string, gateVerdict) {
	// The kustomization file may itself be a link: what is read is what it names.
	if target, err := filepath.EvalSymlinks(file); err != nil || !within(g.root, target) {
		return nil, site.unchecked("", "", "the kustomization file leaves "+g.limit+" through a symbolic link")
	}
	node, reason := g.readYAML(file)
	if reason == reasonNoDocument {
		reason = "the kustomization file is empty"
	}
	if reason != "" {
		return nil, site.unchecked("", "", reason)
	}
	g.bases = nil
	v := g.applyMap(mapOf(kustomizationRules), node, "", site, true)
	bases := g.bases
	g.bases = nil
	return bases, v
}

// kustomizationFile is the kustomization file of dir, "" when there is none, or a
// verdict when there are several.
func (g *gate) kustomizationFile(dir string) (string, gateVerdict) {
	var found []string
	for _, name := range kustomizationNames {
		p := filepath.Join(dir, name)
		if _, err := os.Lstat(p); err == nil {
			found = append(found, p)
		} else if !isMissing(err) {
			return "", gateSite{file: g.rel(p)}.unchecked("", "", "it cannot be read ("+errReason(err)+")")
		}
	}
	switch len(found) {
	case 0:
		return "", gateVerdict{}
	case 1:
		return found[0], gateVerdict{}
	}
	return "", gateSite{file: g.rel(found[0])}.unchecked("", "", "the directory holds more than one kustomization file")
}

// readYAML reads a file of the overlay, within what is left of the ceilings.
func (g *gate) readYAML(file string) (*yaml.Node, string) {
	data, reason := readGateFile(file, min(g.lim.FileBytes, g.lim.TotalBytes-g.read))
	if reason != "" {
		return nil, reason
	}
	g.read += int64(len(data))
	return parseGateYAML(data)
}

// clipLen is how many characters of a name from the repository a message keeps.
const clipLen = 200

// clip is s cut to clipLen characters, with an ellipsis when it was longer: a
// field, a reference or a kind is the repository's own text, of any length.
func clip(s string) string {
	if utf8.RuneCountInString(s) <= clipLen {
		return s
	}
	return string([]rune(s)[:clipLen]) + "…"
}

func join(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

func isNull(n *yaml.Node) bool { return n.Kind == yaml.ScalarNode && n.Tag == "!!null" }

// apply judges the value n of field by r.
func (g *gate) apply(r gateRule, n *yaml.Node, field string, s gateSite) gateVerdict {
	if isNull(n) || r.kind == gateData {
		return gateVerdict{}
	}
	switch r.kind {
	case gateList:
		return g.applyList(r, n, field, s)
	case gateMap:
		return g.applyMap(r, n, field, s, false)
	case gateNever:
		return g.applyNever(n, field, s)
	case gatePatch:
		if n.Kind == yaml.MappingNode { // inline, as data; kustomize rejects it
			return gateVerdict{}
		}
	}
	if n.Kind != yaml.ScalarNode || n.Tag != "!!str" {
		return s.unchecked(field, "", "a reference that is not a string")
	}
	return g.applyString(r.kind, n.Value, field, s)
}

func (g *gate) applyList(r gateRule, n *yaml.Node, field string, s gateSite) gateVerdict {
	if n.Kind != yaml.SequenceNode {
		return s.unchecked(field, "", "a value that is not a list")
	}
	for i, e := range n.Content {
		if v := g.apply(*r.elem, e, fmt.Sprintf("%s[%d]", field, i), s); v.Kind != gateOK {
			return v
		}
	}
	return gateVerdict{}
}

// applyMap judges the entries of a mapping. closed is the kustomization itself,
// where a field that kustomizationRules does not list is refused; elsewhere an
// entry no rule lists holds no reference.
func (g *gate) applyMap(r gateRule, n *yaml.Node, field string, s gateSite, closed bool) gateVerdict {
	if n.Kind != yaml.MappingNode {
		return s.unchecked(field, "", "a value that is not a mapping")
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := n.Content[i].Value
		sub, known := lookupRule(r.fields, key)
		if !known && closed {
			return s.unchecked(join(field, key), "", "a field that kustomize v5.8.3 does not have")
		}
		if !known {
			continue
		}
		if v := g.apply(sub, n.Content[i+1], join(field, key), s); v.Kind != gateOK {
			return v
		}
	}
	return gateVerdict{}
}

// applyNever refuses any value of a field that names a program to run or its
// arguments.
func (g *gate) applyNever(n *yaml.Node, field string, s gateSite) gateVerdict {
	var parts []string
	if n.Kind == yaml.ScalarNode {
		parts = append(parts, n.Value)
	}
	for _, c := range n.Content {
		parts = append(parts, c.Value)
	}
	ref := strings.Join(parts, " ")
	if ref == "" {
		return gateVerdict{}
	}
	return s.remote(field, ref, "it names a program to run or arguments for it")
}

func (g *gate) applyString(kind gateRuleKind, value, field string, s gateSite) gateVerdict {
	switch kind {
	case gateFileSource:
		parts := strings.Split(value, "=")
		if len(parts) > 2 {
			return s.unchecked(field, value, "a files entry with more than one equal sign")
		}
		return g.reference(parts[len(parts)-1], false, field, s)
	case gatePlugin:
		if strings.Contains(value, "\n") {
			return g.inlinePlugin(value, field, s)
		}
		return g.pluginPath(value, field, s)
	case gatePatch:
		if strings.Contains(value, "\n") {
			if inlineResources(value) {
				return gateVerdict{}
			}
			return s.unchecked(field, "", "text that is neither a path nor a patch")
		}
		return g.reference(value, false, field, s)
	}
	return g.reference(value, kind == gateBase, field, s)
}

// inlineResources reports whether text is what kustomize reads as an inline
// patch: documents that are Kubernetes objects.
func inlineResources(text string) bool {
	dec := yaml.NewDecoder(strings.NewReader(text))
	found := false
	for {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return found
		}
		if err != nil || doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || !hasKey(doc.Content[0], "kind") {
			return false
		}
		found = true
	}
}

// hasKey reports whether n is a mapping with a key of exactly this name.
func hasKey(n *yaml.Node, name string) bool {
	return mapGet(n, name) != nil
}

// mapGet is the value of the key spelled exactly name in the mapping n.
func mapGet(n *yaml.Node, name string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == name {
			return n.Content[i+1]
		}
	}
	return nil
}

// reference judges one reference: it must be local, and below the limit once
// symbolic links are resolved. A directory is noted to be read in turn when
// follow says so.
func (g *gate) reference(ref string, follow bool, field string, s gateSite) gateVerdict {
	real, found, v := g.resolve(ref, field, s)
	if v.Kind != gateOK || !found {
		return v
	}
	// Only a regular file or a directory: kustomize blocks on a pipe.
	info, err := os.Stat(real)
	switch {
	case err != nil:
		return s.unchecked(field, ref, "it cannot be read ("+errReason(err)+")")
	case info.IsDir():
		if follow {
			g.bases = append(g.bases, real)
		}
	case !info.Mode().IsRegular():
		return s.unchecked(field, ref, "it is neither a regular file nor a directory")
	}
	return gateVerdict{}
}

// resolve turns a reference of the kustomization at s.dir into a real path. found
// is false for a path that is not there: kustomize reports it and fetches nothing.
func (g *gate) resolve(ref, field string, s gateSite) (real string, found bool, v gateVerdict) {
	if ref == "" {
		return "", false, gateVerdict{} // the directory of the kustomization itself
	}
	if ok, reason := localRef(ref); !ok {
		return "", false, s.remote(field, ref, reason)
	}
	cand := filepath.Join(s.dir, filepath.FromSlash(ref))
	if !within(g.root, cand) {
		return "", false, s.unchecked(field, ref, "it leaves "+g.limit)
	}
	real, err := filepath.EvalSymlinks(cand)
	switch {
	case isMissing(err):
		return "", false, gateVerdict{}
	case err != nil:
		return "", false, s.unchecked(field, ref, "it cannot be resolved ("+errReason(err)+")")
	case !within(g.root, real):
		return "", false, s.unchecked(field, ref, "it leaves "+g.limit+" through a symbolic link")
	}
	return real, true, gateVerdict{}
}

// pluginPath judges an entry of generators, transformers or validators that is
// not inline: a plugin configuration file. A directory is refused, not followed:
// kustomize reads the resources of its kustomization as plugin configurations
// (and runs what they say), where the gate reads them as resources.
func (g *gate) pluginPath(ref, field string, s gateSite) gateVerdict {
	real, found, v := g.resolve(ref, field, s)
	if v.Kind != gateOK || !found {
		return v
	}
	info, err := os.Stat(real)
	switch {
	case err != nil:
		return s.unchecked(field, ref, "it cannot be read ("+errReason(err)+")")
	case info.IsDir():
		return s.unchecked(field, ref, "a plugin directory cannot be checked")
	}
	file := gateSite{file: g.rel(real), dir: s.dir, depth: s.depth}
	node, reason := g.readYAML(real)
	if reason != "" {
		return file.unchecked("", "", reason)
	}
	return g.pluginConfig(node, "", file)
}

// inlinePlugin judges a plugin configuration written in the kustomization.
func (g *gate) inlinePlugin(text, field string, s gateSite) gateVerdict {
	node, reason := parseGateYAML([]byte(text))
	if reason != "" {
		return s.unchecked(field, "", "an inline plugin configuration: "+reason)
	}
	return g.pluginConfig(node, field, s)
}

// pluginConfig judges the configuration of a plugin: a builtin one of a known
// kind, that runs no function, whose fields that name a file are local.
func (g *gate) pluginConfig(node *yaml.Node, prefix string, s gateSite) gateVerdict {
	scalar := func(name string) string {
		if n := mapGet(node, name); n != nil && n.Kind == yaml.ScalarNode && n.Tag == "!!str" {
			return n.Value
		}
		return ""
	}
	if version := scalar("apiVersion"); version != "builtin" {
		return s.unchecked(join(prefix, "apiVersion"), "", fmt.Sprintf("a plugin of apiVersion %q: only builtin plugins are read", clip(version)))
	}
	kind := scalar("kind")
	fields, known := builtinPlugins[kind]
	if !known {
		return s.unchecked(join(prefix, "kind"), "", fmt.Sprintf("a builtin plugin of kind %q, which the gate does not know", clip(kind)))
	}
	if annotations := mapGet(mapGet(node, "metadata"), "annotations"); annotations != nil && annotations.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(annotations.Content); i += 2 {
			for _, name := range functionAnnotations {
				if strings.EqualFold(annotations.Content[i].Value, name) {
					return s.unchecked(join(prefix, "metadata.annotations"), "", "a function annotation, which makes kustomize run a program")
				}
			}
		}
	}
	return g.apply(mapOf(fields), node, prefix, s)
}
