package viewer

import (
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// TokenClass is what a run of text is, for colouring purposes. It is a small
// closed set of this package's own rather than chroma's several hundred token
// types: the theme has to have an opinion about every one of them, and eight is
// a number a palette can carry.
type TokenClass int

const (
	ClassText TokenClass = iota
	ClassKey
	ClassString
	ClassNumber
	ClassLiteral // true, false, null
	ClassPunct
	ClassTag
	ClassAttr
	ClassComment
)

// Token is a run of text and what it is.
type Token struct {
	Class TokenClass
	Text  string

	// Match says the current search found this run. It is a second axis, not a
	// ninth class: Class is what the text *is*, and an occurrence inside a key is
	// still a key. Tokenize never sets it — MarkMatches does, after the filter.
	Match bool
}

// Tokenize splits text for highlighting.
//
// chroma is used as a **lexer and nothing else**. Its formatters write their own
// ANSI colour and reset sequences, and a reset inside a line ends the app
// background for everything after it on that line (Rule 115) — the terminal's
// own background then shows through to the right margin. Emitting classes and
// letting lipgloss paint them is what keeps the background under our control.
//
// The invariant every caller depends on: concatenating the tokens' text
// reproduces the input exactly. A lexer that dropped or added a byte would
// desynchronise the rendered lines from the document's real ones.
func Tokenize(kind Kind, text string) []Token {
	name := lexerName(kind)
	if name == "" {
		return []Token{{Class: ClassText, Text: text}}
	}
	lexer := lexers.Get(name)
	if lexer == nil {
		return []Token{{Class: ClassText, Text: text}}
	}
	iterator, err := lexer.Tokenise(nil, text)
	if err != nil {
		// A lexer that will not run is not worth a message: the document is
		// displayed uncoloured, which is what `c` would have done anyway.
		return []Token{{Class: ClassText, Text: text}}
	}

	raw := iterator.Tokens()
	out := make([]Token, 0, len(raw))
	for _, token := range raw {
		out = append(out, Token{Class: classOf(kind, token.Type), Text: token.Value})
	}
	return out
}

// lexerName is the chroma lexer for a kind, or "" for a kind that has none.
//
// A log has none deliberately: its colour comes from its level, one whole line
// at a time, which is a different question from what a token is.
func lexerName(kind Kind) string {
	switch kind {
	case KindJSON:
		return "json"
	case KindXML:
		return "xml"
	case KindYAML:
		return "yaml"
	case KindTOML:
		return "toml"
	default:
		return ""
	}
}

// classOf maps a chroma token type onto our palette.
//
// The mapping was read off the four lexers rather than guessed: chroma emits
// NameTag for a JSON object key *and* for an XML tag, which is why the kind is
// a parameter. Both happen to resolve to the same colour today; naming them
// apart is what lets a theme separate them later without this function being
// wrong in the meantime.
//
// The reading is what settled YAML and TOML too. YAML needed nothing: its keys
// come out as NameTag, its scalars as Literal, its `true`/`null` as
// KeywordConstant, all of which already landed somewhere sensible. TOML needed
// one line — every one of its keys, table headers included, comes out as
// NameOther, which fell through to ClassText and left a TOML document coloured
// everywhere except the thing worth colouring.
func classOf(kind Kind, t chroma.TokenType) TokenClass {
	switch t.Category() {
	case chroma.Comment:
		return ClassComment
	case chroma.Keyword:
		return ClassLiteral
	case chroma.Punctuation, chroma.Operator:
		return ClassPunct
	case chroma.Literal:
		if t.InSubCategory(chroma.LiteralNumber) {
			return ClassNumber
		}
		return ClassString
	case chroma.Name:
		switch t {
		case chroma.NameAttribute:
			return ClassAttr
		case chroma.NameTag:
			if kind == KindXML {
				return ClassTag
			}
			return ClassKey
		case chroma.NameOther:
			// "A name the lexer could not qualify further", which in TOML is
			// precisely a key. It is not guarded on the kind because neither the
			// JSON nor the XML lexer emits it — TestJSONTokensAreClassified and
			// TestXMLTagsAndAttributesAreClassifiedApart are what keep that true,
			// and `if kind == KindTOML` is the one-line fallback if it stops being.
			return ClassKey
		default:
			return ClassText
		}
	default:
		return ClassText
	}
}
