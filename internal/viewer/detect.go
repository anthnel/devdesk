package viewer

import (
	"path/filepath"
	"strings"
)

// detection is what the name and the content managed to say about a document.
//
// Declared separates "the name said JSON" from "the content started with a
// brace". Only the first is worth reporting a parse failure for: a Markdown
// file that happens to open with a tag is not a broken XML document, and
// telling the user it is would be noise about a file that is displaying fine.
type detection struct {
	Kind     Kind
	Declared bool
}

// extensionKinds are the extensions that decide, on their own, what a file is.
//
// A log is only ever recognised here or declared by its producer — never
// sniffed from the content. "This looks like a log" is not a decidable
// question, and the registry `provider` field is the precedent: declared, never
// inferred from the URL, so nothing depends on what happened to be seen first.
var extensionKinds = map[string]Kind{
	".json": KindJSON,
	".xml":  KindXML,
	".log":  KindLog,
}

// DetectKind is what a document is, judged by its name and then its content.
func DetectKind(name string, data []byte) Kind {
	return detect(name, data).Kind
}

func detect(name string, data []byte) detection {
	ext := strings.ToLower(filepath.Ext(name))
	if kind, ok := extensionKinds[ext]; ok {
		return detection{Kind: kind, Declared: true}
	}
	// Any other extension is taken at its word: a .md, a .go or a .txt is text,
	// and sniffing it would open a Markdown file as a tree the first time
	// someone started one with a tag.
	if ext != "" {
		return detection{Kind: KindPlain}
	}
	return detection{Kind: sniff(data)}
}

// leadingNoise is what may sit in front of a document's first real character:
// whitespace, and a byte-order mark an editor left behind. Without the mark in
// this set, a BOM would hide the very byte this function exists to read.
const leadingNoise = " \t\r\n\ufeff"

// sniff guesses at a file with no extension at all — a Dockerfile, a Makefile,
// a dump someone redirected into a name. It looks at the first meaningful byte
// and nothing else; a document that then fails to parse simply falls back to
// text, with no complaint, because nothing claimed it was structured.
func sniff(data []byte) Kind {
	head := data
	if len(head) > sniffSize {
		head = head[:sniffSize]
	}
	trimmed := strings.TrimLeft(string(head), leadingNoise)
	switch {
	case strings.HasPrefix(trimmed, "{"), strings.HasPrefix(trimmed, "["):
		return KindJSON
	case strings.HasPrefix(trimmed, "<"):
		return KindXML
	default:
		return KindPlain
	}
}
