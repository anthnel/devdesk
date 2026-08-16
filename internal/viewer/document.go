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
	KindAuto  Kind = ""
	KindPlain Kind = "plain"
	KindJSON  Kind = "json"
	KindXML   Kind = "xml"
	KindLog   Kind = "log"
)

// Structured reports whether the kind has a tree to walk. It is the one question
// the view asks before offering the tree display, so it is answered here rather
// than by a switch written out at each call site.
func (k Kind) Structured() bool {
	return k == KindJSON || k == KindXML
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
