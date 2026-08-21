package viewer

import (
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// TokenClass is what a run of text is, for colouring purposes. It is a small
// closed set of this package's own rather than chroma's several hundred token
// types: the theme has to have an opinion about every one of them, and a dozen
// is a number a palette can carry.
//
// The last three carry a text *attribute* rather than a colour, and they are the
// only ones that do. Markdown needs it: once the rendered display has taken the
// `**` and the `~~` away, weight and strikethrough are the only thing left
// saying the two runs were ever different, and a hue would say it less — a bold
// word is bold in any theme.
type TokenClass int

const (
	ClassText TokenClass = iota
	ClassKey
	ClassString
	ClassNumber
	ClassLiteral // true, false, null
	ClassKeyword // if, FROM, func — a word of the language, not a value
	ClassPunct
	ClassTag
	ClassAttr
	ClassComment
	ClassHeading
	ClassStrong
	ClassEmph
	ClassStrike
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
	return coalesce(out)
}

// coalesce merges neighbouring runs of the same class into one token.
//
// It is not a tidiness pass, it is what makes Markdown affordable. chroma's
// markdown lexer ends its inline rules with a catch-all single-character
// alternative, so ordinary prose comes back
// **one token per character** — "Some" is four tokens. At the viewer's 5 MiB
// ceiling (MaxSize) that is millions of Token values, each carrying a string
// header, for a document whose prose is a handful of runs.
//
// It preserves the invariant Tokenize promises — the concatenation is unchanged,
// only the boundaries move — and it drops empty runs, which several lexers emit
// between groups. Match is deliberately not consulted: Tokenize never sets it
// (MarkMatches does, after the filter), so there is no occurrence here to merge
// away.
func coalesce(tokens []Token) []Token {
	out := tokens[:0]
	for _, token := range tokens {
		if token.Text == "" {
			continue
		}
		if n := len(out); n > 0 && out[n-1].Class == token.Class {
			out[n-1].Text += token.Text
			continue
		}
		out = append(out, token)
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
	case KindMarkdown:
		return "markdown"
	case KindDockerfile:
		return "docker"
	case KindShell:
		return "bash"
	default:
		return ""
	}
}

// classOf maps a chroma token type onto our palette.
//
// The mapping was read off the lexers rather than guessed: chroma emits
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
//
// Markdown, Dockerfile and shell were read the same way, and each said something
// the guess would have missed: the whole Generic category was unmapped, so
// Markdown arrived almost colourless; a Dockerfile instruction and a shell `if`
// come out as a bare Keyword, which no earlier kind ever emitted; and a shell
// variable arrives as NameVariable, which fell through to ordinary text.
func classOf(kind Kind, t chroma.TokenType) TokenClass {
	switch t.Category() {
	case chroma.Comment:
		return ClassComment
	case chroma.Generic:
		return genericClass(t)
	case chroma.Keyword:
		// KeywordConstant is `true`, `false`, `null` — a value. Every other
		// keyword is a word of the language: `if`, `fi`, `FROM`, `func`.
		//
		// Splitting them is free of consequence for what already worked, and
		// that is a checked fact rather than a hope: the JSON, YAML, TOML and
		// XML lexers emit KeywordConstant and never a bare Keyword, so the
		// second branch below is reached only by the three kinds added with it.
		// TestAKeywordConstantIsStillALiteral is what keeps that true.
		//
		// The test is equality and not InSubCategory, which would answer true
		// for every keyword there is: chroma implements a sub-category as
		// `t/100 == other/100`, and a bare Keyword shares that quotient with
		// KeywordConstant. Written the obvious way, this branch swallowed the
		// other one whole.
		if t == chroma.KeywordConstant {
			return ClassLiteral
		}
		return ClassKeyword
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
		case chroma.NameVariable, chroma.NameVariableGlobal, chroma.NameVariableInstance,
			chroma.NameVariableClass, chroma.NameVariableMagic,
			chroma.NameBuiltin, chroma.NameBuiltinPseudo, chroma.NameFunction:
			// The identifier family: a shell variable, a builtin, a function
			// name inside a fenced code block. They join the JSON, YAML and TOML
			// keys rather than taking a colour of their own — "a name this
			// document defines or uses" is one idea, and a Dockerfile whose
			// $ARGs were the ordinary text colour was coloured half way.
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

// genericClass maps chroma's Generic family, which is where a markup lexer puts
// everything that is markup rather than code.
//
// It is Markdown's whole vocabulary: the category was mapped nowhere before, so
// a Markdown document reached the screen very nearly colourless. GenericDeleted
// is strikethrough here and not "a deleted line of a diff" — this application
// has no diff lexer, and the markdown lexer is what emits it.
func genericClass(t chroma.TokenType) TokenClass {
	switch t {
	case chroma.GenericHeading, chroma.GenericSubheading:
		return ClassHeading
	case chroma.GenericStrong:
		return ClassStrong
	case chroma.GenericEmph:
		return ClassEmph
	case chroma.GenericDeleted:
		return ClassStrike
	default:
		return ClassText
	}
}
