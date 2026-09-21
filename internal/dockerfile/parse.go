// Package dockerfile reads the base images a Dockerfile builds on, and where in
// the file each one is written.
//
// It exists for the security remediation (§3.2): a scan says which packages are
// vulnerable, and the Dockerfile is where the base image that carries them is
// named. Every image it reports comes with the byte range that spells it, so a
// later edit replaces those bytes and nothing else — comments, line endings and
// the rest of the file are never re-serialized.
//
// It is a reader, not a validator. It does not run the instructions, does not
// know about --build-arg overrides, and a Dockerfile it cannot make sense of
// yields fewer stages rather than an error.
package dockerfile

import (
	"regexp"
	"strings"

	"github.com/anthnel/devdesk/internal/patch"
)

// Span is patch.Span: a range of bytes, End exclusive. It is spelled here as
// well so a reader of this file does not have to go looking, and because every
// span this parser produces exists to be handed to patch.Rewrite.
type Span = patch.Span

// Kind says what a FROM instruction names.
type Kind int

const (
	// KindImage is an image reference, resolved or not.
	KindImage Kind = iota
	// KindScratch is `FROM scratch`: the empty image, nothing to scan or bump.
	KindScratch
	// KindStage is a FROM that names an earlier stage of the same file.
	KindStage
)

// Stage is one FROM instruction.
type Stage struct {
	// Line is the 1-based line of the FROM keyword.
	Line int
	// Name is the `AS name` of the stage, empty if it has none.
	Name string
	// Platform is the --platform flag as written (possibly a variable).
	Platform string
	Kind     Kind
	// Written is the image token exactly as it appears after FROM, so a view
	// can show `${BASE}` next to what it resolves to.
	Written string
	// Image is the reference once build args are substituted. Empty for a
	// scratch or stage FROM, and when Unresolved says why it could not be.
	Image      string
	Unresolved string
	// Span is where the image is spelled, and Editable says whether replacing
	// those bytes changes this stage's image. When the reference comes from an
	// ARG default, Span is that default's value in the ARG line: the one place
	// the image is actually written. Several stages can share it.
	Span         Span
	Editable     bool
	NoEditReason string
}

// File is a parsed Dockerfile.
type File struct {
	Stages []Stage
}

// argValue is a global ARG (declared before the first FROM) and where its
// default is written.
type argValue struct {
	value      string
	hasDefault bool
	span       Span
}

type line struct {
	off  int
	text string
}

type piece struct {
	off  int
	text string
}

type token struct {
	span Span
	text string
}

var directive = regexp.MustCompile(`^#\s*([A-Za-z]+)\s*=\s*(\S+)\s*$`)

// Parse reads the FROM instructions of a Dockerfile.
func Parse(content []byte) File {
	lines := splitLines(content)
	esc := escapeChar(lines)

	var file File
	args := map[string]argValue{}
	names := map[string]bool{}
	inStage := false

	for i := 0; i < len(lines); {
		trimmed := strings.TrimSpace(lines[i].text)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			i++
			continue
		}
		first := i
		pieces, next := gather(lines, i, esc)
		i = next
		tokens := tokenize(pieces)
		if len(tokens) == 0 {
			continue
		}
		switch strings.ToUpper(tokens[0].text) {
		case "ARG":
			if !inStage {
				readArgs(tokens[1:], args)
			}
		case "FROM":
			stage := readFrom(tokens[1:], args, names)
			stage.Line = first + 1
			file.Stages = append(file.Stages, stage)
			if stage.Name != "" {
				names[strings.ToLower(stage.Name)] = true
			}
			inStage = true
		}
	}
	return file
}

// splitLines cuts content into physical lines, keeping each one's byte offset.
// A trailing \r is dropped from the text but the offsets stay absolute.
func splitLines(b []byte) []line {
	var lines []line
	start := 0
	for i := 0; i <= len(b); i++ {
		if i < len(b) && b[i] != '\n' {
			continue
		}
		if i == len(b) && start == len(b) {
			break
		}
		text := string(b[start:i])
		text = strings.TrimSuffix(text, "\r")
		lines = append(lines, line{off: start, text: text})
		start = i + 1
	}
	return lines
}

// escapeChar reads the `# escape=` parser directive, which only counts at the
// very top of the file.
func escapeChar(lines []line) byte {
	for _, l := range lines {
		m := directive.FindStringSubmatch(strings.TrimSpace(l.text))
		if m == nil {
			break
		}
		if strings.EqualFold(m[1], "escape") && len(m[2]) == 1 && (m[2] == "`" || m[2] == `\`) {
			return m[2][0]
		}
	}
	return '\\'
}

// gather collects the physical lines of the instruction starting at lines[i],
// following the escape character across line breaks. Comment and blank lines
// inside a continuation are skipped, as Docker does.
func gather(lines []line, i int, esc byte) (pieces []piece, next int) {
	for i < len(lines) {
		text := strings.TrimRight(lines[i].text, " \t")
		if text == "" || text[len(text)-1] != esc {
			pieces = append(pieces, piece{off: lines[i].off, text: lines[i].text})
			return pieces, i + 1
		}
		pieces = append(pieces, piece{off: lines[i].off, text: text[:len(text)-1]})
		i++
		for i < len(lines) {
			t := strings.TrimSpace(lines[i].text)
			if t != "" && !strings.HasPrefix(t, "#") {
				break
			}
			i++
		}
	}
	return pieces, i
}

// tokenize splits pieces on spaces and tabs, with absolute byte spans.
func tokenize(pieces []piece) []token {
	var tokens []token
	for _, p := range pieces {
		start := -1
		for j := 0; j <= len(p.text); j++ {
			space := j == len(p.text) || p.text[j] == ' ' || p.text[j] == '\t'
			switch {
			case !space && start < 0:
				start = j
			case space && start >= 0:
				tokens = append(tokens, token{
					span: Span{Start: p.off + start, End: p.off + j},
					text: p.text[start:j],
				})
				start = -1
			}
		}
	}
	return tokens
}

// readArgs records `ARG NAME[=default] ...` into args. A later declaration of
// the same name replaces the earlier one, as it does for a following FROM.
func readArgs(tokens []token, args map[string]argValue) {
	for _, t := range tokens {
		name, value, hasDefault := strings.Cut(t.text, "=")
		if name == "" {
			continue
		}
		a := argValue{}
		if hasDefault {
			start := t.span.Start + len(name) + 1
			end := t.span.End
			if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
				value = value[1 : len(value)-1]
				start++
				end--
			}
			a = argValue{value: value, hasDefault: true, span: Span{Start: start, End: end}}
		}
		args[name] = a
	}
}

// readFrom reads what follows the FROM keyword: flags, the image, `AS name`.
func readFrom(tokens []token, args map[string]argValue, names map[string]bool) Stage {
	var st Stage
	i := 0
	for i < len(tokens) && strings.HasPrefix(tokens[i].text, "--") {
		if k, v, ok := strings.Cut(tokens[i].text, "="); ok && strings.EqualFold(k, "--platform") {
			st.Platform = v
		}
		i++
	}
	if i >= len(tokens) {
		st.Unresolved = "FROM names no image"
		return st
	}
	image := tokens[i]
	if i+2 < len(tokens) && strings.EqualFold(tokens[i+1].text, "AS") {
		st.Name = tokens[i+2].text
	}
	st.Written = image.text

	switch {
	case strings.EqualFold(image.text, "scratch"):
		st.Kind = KindScratch
	case names[strings.ToLower(image.text)]:
		st.Kind = KindStage
	default:
		resolveImage(&st, image, args)
	}
	return st
}

// resolveImage fills in Image, Span and Editable for an image token.
func resolveImage(st *Stage, image token, args map[string]argValue) {
	resolved, ok, sole := expand(image.text, args)
	if !ok {
		st.Unresolved = "the reference uses a build arg with no default"
		return
	}
	st.Image = resolved
	if !strings.Contains(image.text, "$") {
		st.Span, st.Editable = image.span, true
		return
	}
	if sole != nil && sole.hasDefault {
		// The whole reference is one variable: the default in its ARG line is
		// where the image is written. A --build-arg on the command line can
		// still override it, which a file cannot show.
		st.Span, st.Editable = sole.span, true
		return
	}
	st.NoEditReason = "the reference is assembled from build args, so no single place in the file spells it"
}

var reference = regexp.MustCompile(`\$(?:\{([A-Za-z_][A-Za-z0-9_]*)(?:(:?-)([^}]*))?\}|([A-Za-z_][A-Za-z0-9_]*))`)

// expand substitutes ${NAME}, $NAME, ${NAME:-default} and ${NAME-default}
// from the global ARGs. ok is false when a variable has no value; sole is the
// ARG when s is nothing but one reference to it.
func expand(s string, args map[string]argValue) (out string, ok bool, sole *argValue) {
	ok = true
	whole := reference.FindStringSubmatchIndex(s)
	out = reference.ReplaceAllStringFunc(s, func(m string) string {
		sub := reference.FindStringSubmatch(m)
		name := sub[1]
		if name == "" {
			name = sub[4]
		}
		a, declared := args[name]
		switch {
		case declared && a.hasDefault && (a.value != "" || sub[2] != ":-"):
			return a.value
		case sub[2] != "":
			return sub[3]
		}
		ok = false
		return m
	})
	if whole != nil && whole[0] == 0 && whole[1] == len(s) {
		sub := reference.FindStringSubmatch(s)
		name := sub[1]
		if name == "" {
			name = sub[4]
		}
		if a, declared := args[name]; declared {
			sole = &a
		}
	}
	return out, ok, sole
}
