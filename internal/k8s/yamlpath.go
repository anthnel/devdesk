package k8s

import (
	"bytes"
	"errors"
	"io"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Span is where a node sits in a file, 1-based and inclusive.
type Span struct {
	Line    int
	EndLine int
}

// Locate finds the document of a multi-document file whose kind and
// metadata.name match, then follows a JSON pointer ("/spec/template/spec/
// containers/0") down into it.
//
// kubeconform reports a validation error by pointer and never by line, and a
// finding without a line is one nobody can jump to or fix. This is what turns
// the one into the other, by reading the file rather than by guessing: when a
// step of the pointer does not exist, the answer is not found, never the
// nearest line that does.
//
// The Line of a mapping entry is its key's — for a nested mapping the value
// starts on the next line, and the key is what a reader looks for. EndLine is
// the last line of the value.
//
// An empty name matches the first document of that kind, which is what a
// resource without metadata.name amounts to.
func Locate(content []byte, kind, name, pointer string) (Span, bool) {
	doc, ok := findDocument(content, kind, name)
	if !ok {
		return Span{}, false
	}
	node, keyLine := doc, doc.Line
	for _, seg := range splitPointer(pointer) {
		node = resolveAlias(node)
		switch node.Kind {
		case yaml.MappingNode:
			key, value, found := mappingEntry(node, seg)
			if !found {
				return Span{}, false
			}
			node, keyLine = value, key.Line
		case yaml.SequenceNode:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(node.Content) {
				return Span{}, false
			}
			node = node.Content[i]
			keyLine = node.Line
		default:
			return Span{}, false
		}
	}
	return Span{Line: keyLine, EndLine: max(lastLine(node), keyLine)}, true
}

// findDocument returns the top-level mapping of the matching document.
func findDocument(content []byte, kind, name string) (*yaml.Node, bool) {
	dec := yaml.NewDecoder(bytes.NewReader(content))
	for {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) || err != nil {
			return nil, false
		}
		if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
			continue
		}
		root := doc.Content[0]
		if root.Kind != yaml.MappingNode || scalarAt(root, "kind") != kind {
			continue
		}
		if name == "" {
			return root, true
		}
		if _, meta, ok := mappingEntry(root, "metadata"); ok && scalarAt(resolveAlias(meta), "name") == name {
			return root, true
		}
	}
}

// DocumentField returns the span of one top-level key of the matching
// document — its apiVersion line, typically.
func DocumentField(content []byte, kind, name, key string) (Span, bool) {
	return Locate(content, kind, name, "/"+key)
}

func mappingEntry(m *yaml.Node, key string) (*yaml.Node, *yaml.Node, bool) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i], m.Content[i+1], true
		}
	}
	return nil, nil, false
}

func scalarAt(m *yaml.Node, key string) string {
	if m.Kind != yaml.MappingNode {
		return ""
	}
	if _, v, ok := mappingEntry(m, key); ok && v.Kind == yaml.ScalarNode {
		return v.Value
	}
	return ""
}

func resolveAlias(n *yaml.Node) *yaml.Node {
	for n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	return n
}

// lastLine is the last line any node of the subtree starts on. A multi-line
// scalar ends further down than it starts; counting its own newlines covers
// the literal and folded styles, which are the ones a manifest uses.
func lastLine(n *yaml.Node) int {
	last := n.Line
	if n.Kind == yaml.ScalarNode && (n.Style == yaml.LiteralStyle || n.Style == yaml.FoldedStyle) {
		last = n.Line + strings.Count(strings.TrimRight(n.Value, "\n"), "\n") + 1
	}
	for _, c := range n.Content {
		last = max(last, lastLine(c))
	}
	return last
}

// splitPointer decodes an RFC 6901 pointer into its reference tokens.
func splitPointer(pointer string) []string {
	pointer = strings.TrimPrefix(pointer, "/")
	if pointer == "" {
		return nil
	}
	parts := strings.Split(pointer, "/")
	for i, p := range parts {
		parts[i] = strings.ReplaceAll(strings.ReplaceAll(p, "~1", "/"), "~0", "~")
	}
	return parts
}
