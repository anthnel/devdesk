package containers

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/anthnel/devdesk/internal/secretenv"
)

// maskedValue replaces the value of a sensitive variable in an inspect (OC-13,
// S9). It has one length whatever the secret's, since a length is a hint, and
// it is ASCII: a terminal, a clipboard and a test all treat it the same way.
const maskedValue = "********"

// Mask makes the inspect Maskable: the viewer keeps the bytes of Load and shows
// what this returns.
func (s inspectSource) Mask(raw []byte, matcher *secretenv.Matcher) ([]byte, int, error) {
	return maskInspectEnv(raw, matcher)
}

// jsonFrame is one open object or array of the document being walked.
type jsonFrame struct {
	object    bool
	key       string // the key whose value is being read, for an object
	expectKey bool   // the next string of an object is a key
	afterFlag bool   // array only: the previous element was -e or --env
}

// textEdit replaces raw[start:end].
type textEdit struct {
	start, end  int
	replacement string
}

// maskInspectEnv replaces the value of every sensitive variable of Config.Env,
// and of the -e and --env options of Config.CreateCommand, in the output of
// `docker inspect` or `podman inspect`, and returns how many it replaced.
//
// CreateCommand is the argv of the `podman run` that made the container, so a
// value given with -e is there as well as in Env. The elements masked are the
// one after `-e` or `--env`, and the glued forms `--env=NAME=VALUE` and
// `-eNAME=VALUE`; `--env-file` and its path are left alone, a path is not a
// value. Everything else in the array (the image, the names, the other
// options) is untouched. An argument of the container's own command that looks
// like `-e NAME=VALUE` is masked too: a false positive hides a harmless value.
//
// The document is edited where it stands rather than decoded and encoded again:
// a map would reorder the keys, and the reader is looking at the engine's own
// layout. Only the span of each sensitive string changes, so what is left is
// byte for byte the engine's output. The rewritten string is encoded again, so
// a name or a value that was written with escapes (\u00e9, a surrogate pair)
// comes back as the characters themselves. Cmd, the labels and everything else are out
// of scope (OC-37 for the logs).
//
// An entry is NAME=VALUE, cut at the first "=": an entry with no "=" has no
// value to hide and is left as it is. An empty value is masked like any other,
// so the mask says nothing about whether a value exists.
//
// A document that cannot be read to its end is refused with an error and no
// output: handing it back unchanged would show the very values this exists to
// hide.
func maskInspectEnv(raw []byte, matcher *secretenv.Matcher) ([]byte, int, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var (
		stack  []jsonFrame
		edits  []textEdit
		tokens int
	)
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, 0, err
		}
		tokens++

		if delim, ok := tok.(json.Delim); ok {
			switch delim {
			case '{', '[':
				stack = append(stack, jsonFrame{object: delim == '{', expectKey: delim == '{'})
			default:
				stack = stack[:len(stack)-1]
				valueDone(stack)
			}
			continue
		}

		text, isString := tok.(string)
		if n := len(stack); n > 0 && stack[n-1].object && stack[n-1].expectKey {
			stack[n-1].key, stack[n-1].expectKey = text, false
			continue
		}
		if array := inConfigArray(stack); array != "" {
			edit, ok, err := maskElement(array, &stack[len(stack)-1], raw, int(dec.InputOffset()), text, isString, matcher)
			if err != nil {
				return nil, 0, err
			}
			if ok {
				edits = append(edits, edit)
			}
		}
		valueDone(stack)
	}
	if tokens == 0 {
		return nil, 0, errors.New("empty inspect output")
	}
	if len(stack) != 0 {
		return nil, 0, io.ErrUnexpectedEOF
	}

	var out bytes.Buffer
	last := 0
	for _, e := range edits {
		out.Write(raw[last:e.start])
		out.WriteString(e.replacement)
		last = e.end
	}
	out.Write(raw[last:])
	return out.Bytes(), len(edits), nil
}

// valueDone tells the enclosing object that its current value is complete, so
// the next string it reads is a key.
func valueDone(stack []jsonFrame) {
	if n := len(stack); n > 0 && stack[n-1].object {
		stack[n-1].expectKey = true
	}
}

// inConfigArray says which of the two arrays of a container's Config the token
// being read is an element of: "Env", "CreateCommand", or "" for anywhere else.
// The array is held under that key of the object held under "Config", whose own
// parent is the top-level array (or the document itself).
func inConfigArray(stack []jsonFrame) string {
	n := len(stack)
	if n != 3 && n != 4 {
		return ""
	}
	if n == 4 && stack[0].object {
		return ""
	}
	if stack[n-1].object || !stack[n-2].object || !stack[n-3].object || stack[n-3].key != "Config" {
		return ""
	}
	switch stack[n-2].key {
	case "Env", "CreateCommand":
		return stack[n-2].key
	}
	return ""
}

// maskElement handles one element of Config.Env or Config.CreateCommand.
func maskElement(array string, frame *jsonFrame, raw []byte, end int, text string, isString bool, matcher *secretenv.Matcher) (textEdit, bool, error) {
	if array == "Env" {
		if !isString {
			return textEdit{}, false, nil
		}
		return maskEntry(raw, end, "", text, matcher)
	}
	// CreateCommand: the element after a bare -e or --env is the entry itself,
	// the glued forms carry it after their option, and nothing else is one.
	afterFlag := frame.afterFlag
	frame.afterFlag = false
	if !isString {
		return textEdit{}, false, nil
	}
	switch {
	case afterFlag:
		return maskEntry(raw, end, "", text, matcher)
	case text == "-e" || text == "--env":
		frame.afterFlag = true
	case strings.HasPrefix(text, "--env="):
		return maskEntry(raw, end, "--env=", strings.TrimPrefix(text, "--env="), matcher)
	case strings.HasPrefix(text, "-e") && !strings.HasPrefix(text, "--"):
		return maskEntry(raw, end, "-e", strings.TrimPrefix(text, "-e"), matcher)
	}
	return textEdit{}, false, nil
}

// maskEntry builds the edit for one NAME=VALUE entry that ends at offset end in
// raw, written after prefix (an option glued to it, or nothing), or reports that
// there is nothing to hide in it. A sensitive entry whose span cannot be located
// is an error, never a pass-through.
func maskEntry(raw []byte, end int, prefix, entry string, matcher *secretenv.Matcher) (textEdit, bool, error) {
	name, _, hasValue := strings.Cut(entry, "=")
	if !hasValue || !matcher.Sensitive(name) {
		return textEdit{}, false, nil
	}
	start, ok := stringStart(raw, end)
	if !ok {
		return textEdit{}, false, errors.New("cannot locate an environment entry to mask")
	}
	var literal bytes.Buffer
	enc := json.NewEncoder(&literal)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(prefix + name + "=" + maskedValue); err != nil {
		return textEdit{}, false, err
	}
	return textEdit{start: start, end: end, replacement: strings.TrimSuffix(literal.String(), "\n")}, true, nil
}

// stringStart finds the opening quote of the string literal that ends at end
// (just past its closing quote). Inside a literal a quote is always escaped, so
// the first one met going backwards with an even run of backslashes before it is
// the opening one.
func stringStart(raw []byte, end int) (int, bool) {
	if end < 2 || end > len(raw) || raw[end-1] != '"' {
		return 0, false
	}
	for i := end - 2; i >= 0; i-- {
		if raw[i] != '"' {
			continue
		}
		backslashes := 0
		for j := i - 1; j >= 0 && raw[j] == '\\'; j-- {
			backslashes++
		}
		if backslashes%2 == 0 {
			return i, true
		}
	}
	return 0, false
}
