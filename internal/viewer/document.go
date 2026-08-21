// Package viewer parses a document into something a read-only pane can show:
// a tree for JSON and XML, levelled lines for a log, plain text for the rest.
//
// It knows nothing about Bubble Tea, lipgloss or the theme. What a token or a
// level is *coloured* is decided in internal/ui/viewer; what one *is* is decided
// here.
package viewer

// Kind is what a document turned out to be.
type Kind string

const (
	// KindAuto asks Open to decide from the name and, failing that, the content.
	// It is what a producer passes when it does not know — a file browser.
	KindAuto       Kind = ""
	KindPlain      Kind = "plain"
	KindJSON       Kind = "json"
	KindXML        Kind = "xml"
	KindLog        Kind = "log"
	KindYAML       Kind = "yaml"
	KindTOML       Kind = "toml"
	KindMarkdown   Kind = "markdown"
	KindDockerfile Kind = "dockerfile"
	KindShell      Kind = "shell"
)

// Structured reports whether the kind has a tree to walk. It is the one question
// the view asks before offering the tree display, so it is answered here rather
// than by a switch written out at each call site.
//
// YAML and TOML are absent by decision, not by oversight. Both have a structure
// — a tree could be drawn — but neither has a parser *here* that preserves the
// file's order, and order is content: a document read back through a
// map[string]any is a different document from the one on disk, which is why the
// JSON and XML parsers read a token stream. yaml.v3 could do it (yaml.Node keeps
// order and comments, and it is already a dependency); TOML would cost another
// one. Until that is worth doing, both are coloured text and `f` is hidden for
// them (Rule 130).
func (k Kind) Structured() bool {
	return k == KindJSON || k == KindXML
}

// Renderable reports whether the kind has a *rendered* display — the same
// document with its markup applied rather than shown.
//
// It is the second half of the one question `f` asks. A kind has at most one
// derived display: a tree when it is Structured, a rendered form when it is
// Renderable, never both. That is what keeps the toggle binary at every moment
// and stops one screen being reachable two ways.
//
// Markdown is the only kind that qualifies, and it qualifies because its markup
// is *decoration*: the markers exist to be replaced by weight, italics and
// indentation, so hiding them loses nothing the reader wanted. A Dockerfile and
// a shell script have no such layer — their punctuation is the program.
func (k Kind) Renderable() bool {
	return k == KindMarkdown
}

// String is the kind as the header prints it.
func (k Kind) String() string {
	if k == KindAuto {
		return string(KindPlain)
	}
	return string(k)
}

// Document is a parsed document, ready to be displayed.
//
// Kind is what it *turned out* to be, which is not always what was asked for: a
// malformed JSON is a plain document that remembers why (ParseErr). Refusing to
// open it would hide exactly the content needed to fix it.
type Document struct {
	Name string
	Kind Kind

	// Text is the normalized content: no ANSI escapes, no carriage returns.
	// Every display reads it, the tree included — the tree is a second view of
	// this, never a replacement for it.
	Text string

	// Root is the parsed tree, for a structured kind only.
	Root *Node

	// Lines carries a level per line, for KindLog only.
	Lines []LogLine

	// ParseErr is set when a kind the *name* declared failed to parse. A kind
	// merely sniffed from the content never sets it: a Markdown file opening
	// with a tag is not a broken XML document, and saying so would be noise.
	ParseErr error
}

// Open parses data into a Document.
//
// kind is what the producer declares — `docker logs` is a log whatever it looks
// like — or KindAuto to let the name and the content decide.
func Open(name string, kind Kind, data []byte) Document {
	doc := Document{Name: name, Text: Normalize(data)}

	declared := kind != KindAuto
	if !declared {
		det := detect(name, data)
		kind, declared = det.Kind, det.Declared
	}
	doc.Kind = kind

	switch kind {
	case KindJSON:
		root, err := parseJSON(doc.Text)
		if err != nil {
			return doc.fallBackToText(err, declared)
		}
		doc.Root = root

	case KindXML:
		root, err := parseXML(doc.Text)
		if err != nil {
			return doc.fallBackToText(err, declared)
		}
		doc.Root = root

	case KindLog:
		doc.Lines = ParseLog(doc.Text)
	}

	return doc
}

// fallBackToText turns a document that would not parse into a plain one, and
// keeps the reason only when it is worth telling the user about.
func (d Document) fallBackToText(err error, declared bool) Document {
	d.Kind = KindPlain
	d.Root = nil
	if declared {
		d.ParseErr = err
	}
	return d
}

// NodeCount is what the header shows for a structured document.
func (d Document) NodeCount() int {
	return countNodes(d.Root)
}

func countNodes(n *Node) int {
	if n == nil {
		return 0
	}
	total := 1
	for _, child := range n.Children {
		total += countNodes(child)
	}
	return total
}
