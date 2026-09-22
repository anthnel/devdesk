package k8s

import (
	"bytes"
	"errors"
	"io"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// What a fix needs to edit a manifest without re-serialising it (§3.80): the
// node to change, and where in the bytes it sits. yaml.v3 knows the first and
// only the line and column of the second; these turn one into the other, so
// the edit is a byte range and everything around it — comments, anchors,
// indentation, quoting elsewhere — stays exactly as the author wrote it.

// DocumentAt returns the top-level mapping of the document that holds line.
func DocumentAt(content []byte, line int) (*yaml.Node, bool) {
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
		if root.Kind == yaml.MappingNode && root.Line <= line && line <= lastLine(root) {
			return root, true
		}
	}
}

// Containers returns every container mapping of a workload, wherever its pod
// template sits — spec.containers for a Pod, spec.template.spec for a
// Deployment, spec.jobTemplate.spec.template.spec for a CronJob — along with
// init and ephemeral containers.
func Containers(doc *yaml.Node) []*yaml.Node {
	var out []*yaml.Node
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		n = resolveAlias(n)
		if n.Kind != yaml.MappingNode {
			return
		}
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, value := n.Content[i].Value, resolveAlias(n.Content[i+1])
			if (key == "containers" || key == "initContainers" || key == "ephemeralContainers") && value.Kind == yaml.SequenceNode {
				for _, c := range value.Content {
					if c = resolveAlias(c); c.Kind == yaml.MappingNode {
						out = append(out, c)
					}
				}
				continue
			}
			walk(value)
		}
	}
	walk(doc)
	return out
}

// Entry returns the key and value nodes of one key of a mapping.
func Entry(m *yaml.Node, key string) (*yaml.Node, *yaml.Node, bool) {
	m = resolveAlias(m)
	if m.Kind != yaml.MappingNode {
		return nil, nil, false
	}
	k, v, ok := mappingEntry(m, key)
	if ok {
		v = resolveAlias(v)
	}
	return k, v, ok
}

// Scalar returns a mapping's scalar value at key, or "".
func Scalar(m *yaml.Node, key string) string {
	return scalarAt(resolveAlias(m), key)
}

// Offset turns a node's line and column into a byte offset. yaml.v3 counts
// columns in characters, so the line is walked rune by rune.
func Offset(content []byte, line, column int) (int, bool) {
	start, ok := LineStart(content, line)
	if !ok {
		return 0, false
	}
	at := start
	for c := 1; c < column; c++ {
		if at >= len(content) || content[at] == '\n' {
			return 0, false
		}
		_, size := utf8.DecodeRune(content[at:])
		at += size
	}
	return at, true
}

// LineStart is the byte offset where a 1-based line begins.
func LineStart(content []byte, line int) (int, bool) {
	if line < 1 {
		return 0, false
	}
	at := 0
	for l := 1; l < line; l++ {
		i := bytes.IndexByte(content[at:], '\n')
		if i < 0 {
			return 0, false
		}
		at += i + 1
	}
	return at, true
}

// LineEnding is the file's own line ending, so an inserted line does not mix
// "\n" into a CRLF file.
func LineEnding(content []byte) string {
	if bytes.Contains(content, []byte("\r\n")) {
		return "\r\n"
	}
	return "\n"
}

// PlainScalarSpan is where a plain (unquoted) scalar's text sits in content.
// A quoted or block scalar is refused: its bytes are not its value, and an
// edit computed from the value would cut the quoting in half.
func PlainScalarSpan(content []byte, n *yaml.Node) (int, int, bool) {
	if n == nil || n.Kind != yaml.ScalarNode || n.Style != 0 {
		return 0, 0, false
	}
	start, ok := Offset(content, n.Line, n.Column)
	if !ok || start+len(n.Value) > len(content) || string(content[start:start+len(n.Value)]) != n.Value {
		return 0, 0, false
	}
	return start, start + len(n.Value), true
}

// OwnLineIndent returns the indentation before a node when the node is the
// first thing on its line — a key a new sibling can be inserted above. A key
// that follows "- " on a sequence item's line is not: inserting above it
// would land outside the item.
func OwnLineIndent(content []byte, n *yaml.Node) (lineStart int, indent string, ok bool) {
	lineStart, ok = LineStart(content, n.Line)
	if !ok {
		return 0, "", false
	}
	at, ok := Offset(content, n.Line, n.Column)
	if !ok {
		return 0, "", false
	}
	prefix := content[lineStart:at]
	if len(bytes.Trim(prefix, " ")) != 0 {
		return 0, "", false
	}
	return lineStart, string(prefix), true
}
