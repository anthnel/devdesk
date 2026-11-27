package enginefixture

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Tool is the engine a fixture was recorded from.
type Tool string

// The tools a fixture may name. kind and kubectl join them with OC-59 and OC-68.
const (
	Docker Tool = "docker"
	Podman Tool = "podman"
)

// Mode is how the engine ran when the fixture was recorded.
type Mode string

// The modes a fixture may name.
const (
	Rootful  Mode = "rootful"
	Rootless Mode = "rootless"
)

// Values of the "record" header. A pending file holds an argv and no answer: the
// recorder writes it before it has run anything, and it is never versioned.
const (
	RecordPending = "pending"
	RecordReplay  = "replay"
	// recordSkipPrefix starts "skip: <reason>".
	recordSkipPrefix = "skip: "
)

// Values of the "recorded-by" header.
const (
	recordedBySynthetic = "synthetic"
	// RecordedByContract marks a fixture the contract recorder wrote
	// (mise run engine-contract:record); the importer accepts nothing else.
	RecordedByContract = "engine-contract:record"
)

const (
	fileSuffix = ".cli"
	stdoutMark = "--- stdout\n"
	stderrMark = "--- stderr\n"

	encText   = "text"
	encBase64 = "base64"

	// base64Columns is the width of a base64 line, so a diff of a recorded binary
	// stays readable.
	base64Columns = 76
	// maxExit is the largest exit code of a process.
	maxExit = 255
)

// Meta is what a fixture says about itself: the headers of the file.
type Meta struct {
	Tool       Tool
	Version    string
	Mode       Mode
	Recorded   string // YYYY-MM-DD
	Scenario   string // free text of the "# scenario:" header
	Redacted   string // what the recorder replaced, or "none"
	Record     string // "", "replay", "pending" or "skip: <reason>"
	RecordedBy string // "", "engine-contract:record" or "synthetic"
}

// Exchange is the engine's answer to one argv.
type Exchange struct {
	Argv   []string // without the binary name
	Exit   int
	Stdout []byte
	Stderr []byte
}

// Fixture is one parsed file. Exit, Stdout and Stderr are unset when the file is
// pending.
type Fixture struct {
	Campaign string // the directory the file was read from
	Name     string // the file name without ".cli"
	Scenario string // the part of Name before "--<tool>-<version>-<mode>"
	Meta     Meta
	Exchange Exchange
}

// headerOrder is the one order the headers are written in.
var headerOrder = []string{
	"tool", "version", "mode", "recorded", "scenario", "redacted",
	"record", "recorded-by", "argv", "exit", "stdout", "stderr",
}

// answerHeaders are the headers a pending file cannot carry.
var answerHeaders = []string{"exit", "stdout", "stderr"}

var versionPattern = regexp.MustCompile(`^\d+(\.\d+)+(-[0-9A-Za-z.]+)?$`)

// Parse reads one fixture file. campaign and fileName come from where the file
// was found; fileName ends in ".cli" and agrees with the headers. Anything Parse
// could not write back exactly is an error that names the file, never the bytes
// found.
func Parse(campaign, fileName string, data []byte) (Fixture, error) {
	stem, ok := strings.CutSuffix(fileName, fileSuffix)
	if !ok {
		return Fixture{}, fail(fileName, "the file name must end in %q", fileSuffix)
	}
	return parse(campaign, fileName, stem, data, true)
}

// parse is Parse; checkName asks that the name say what the headers say. The leak
// check reads a file under any name and passes false.
func parse(campaign, fileName, stem string, data []byte, checkName bool) (Fixture, error) {
	if i := bytes.IndexByte(data, '\r'); i >= 0 {
		return Fixture{}, fail(fileName, "carriage return at byte %d: a fixture uses LF only", i)
	}
	values, body, err := readHeaders(fileName, data)
	if err != nil {
		return Fixture{}, err
	}

	f := Fixture{Campaign: campaign, Name: stem}
	if f.Meta, err = metaOf(fileName, values); err != nil {
		return Fixture{}, err
	}
	if checkName {
		if f.Scenario, err = scenarioOf(fileName, stem, f.Meta); err != nil {
			return Fixture{}, err
		}
	}
	if f.Exchange.Argv, err = parseArgv(fileName, values["argv"]); err != nil {
		return Fixture{}, err
	}
	if f.Meta.Record == RecordPending {
		for _, key := range answerHeaders {
			if _, has := values[key]; has {
				return Fixture{}, fail(fileName, "a pending file cannot carry the header %q", key)
			}
		}
		if len(body) != 0 {
			return Fixture{}, fail(fileName, "a pending file stops after the blank line that follows the headers")
		}
		return f, nil
	}
	if err := readAnswer(fileName, &f.Exchange, values, body); err != nil {
		return Fixture{}, err
	}
	return f, nil
}

// ParseError is what Parse returns: the message names the file and may quote the
// value it refused, which is fine for a file of the repository. Rule is the same
// sentence without any value ("tool <value> is neither docker nor podman"), for a
// caller that reads a file it does not trust and must not echo what it found
// (Import, for a file that came out of a VM).
type ParseError struct {
	File string
	Rule string
	msg  string
}

func (e *ParseError) Error() string { return e.msg }

// verbPattern matches a formatting verb of a message template.
var verbPattern = regexp.MustCompile(`%[-+# 0-9.]*[a-zA-Z]`)

// fail builds an error that names the file.
func fail(fileName, format string, args ...any) error {
	return &ParseError{
		File: fileName,
		Rule: verbPattern.ReplaceAllString(format, "<value>"),
		msg:  fmt.Sprintf("enginefixture: %s: %s", fileName, fmt.Sprintf(format, args...)),
	}
}

// readHeaders splits the file into its headers, in canonical order, and what
// follows the blank line that ends them.
func readHeaders(fileName string, data []byte) (map[string]string, []byte, error) {
	values := map[string]string{}
	last := -1
	for line, rest := 1, data; ; line++ {
		nl := bytes.IndexByte(rest, '\n')
		if nl < 0 {
			return nil, nil, fail(fileName, "the headers are not ended by a blank line")
		}
		text := string(rest[:nl])
		rest = rest[nl+1:]
		if text == "" {
			return values, rest, nil
		}
		key, value, err := splitHeader(fileName, line, text)
		if err != nil {
			return nil, nil, err
		}
		idx := indexOf(headerOrder, key)
		switch {
		case idx < 0:
			return nil, nil, fail(fileName, "line %d: unknown header %q", line, key)
		case hasKey(values, key):
			return nil, nil, fail(fileName, "line %d: duplicate header %q", line, key)
		case idx < last:
			return nil, nil, fail(fileName, "line %d: header %q is out of order (canonical order: %s)", line, key, strings.Join(headerOrder, ", "))
		}
		last = idx
		values[key] = value
	}
}

func hasKey(m map[string]string, key string) bool {
	_, ok := m[key]
	return ok
}

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}

// splitHeader cuts "# key: value". The value has no leading or trailing space and
// is not empty; apart from argv (JSON escapes what it must) it holds no control
// character, so a header cannot hide text from a reviewer.
func splitHeader(fileName string, line int, text string) (key, value string, err error) {
	rest, ok := strings.CutPrefix(text, "# ")
	if !ok {
		return "", "", fail(fileName, "line %d: not a header (want \"# key: value\")", line)
	}
	key, value, ok = strings.Cut(rest, ": ")
	if !ok || key == "" {
		return "", "", fail(fileName, "line %d: malformed header (want \"# key: value\", one space after the colon)", line)
	}
	if value == "" || value != strings.TrimSpace(value) {
		return "", "", fail(fileName, "line %d: the value of %q is empty or has a space at either end", line, key)
	}
	if !utf8.ValidString(value) {
		return "", "", fail(fileName, "line %d: the value of %q is not valid UTF-8", line, key)
	}
	if key != "argv" && strings.IndexFunc(value, isControl) >= 0 {
		return "", "", fail(fileName, "line %d: the value of %q holds a control character", line, key)
	}
	return key, value, nil
}

// isControl reports a C0 control, DEL or a C1 control.
func isControl(r rune) bool { return r < 0x20 || (r >= 0x7f && r <= 0x9f) }

// metaOf validates the headers that describe the fixture.
func metaOf(fileName string, values map[string]string) (Meta, error) {
	required := []string{"tool", "version", "mode", "recorded", "scenario", "redacted", "argv"}
	for _, key := range required {
		if !hasKey(values, key) {
			return Meta{}, fail(fileName, "missing header %q", key)
		}
	}
	m := Meta{
		Tool: Tool(values["tool"]), Version: values["version"], Mode: Mode(values["mode"]),
		Recorded: values["recorded"], Scenario: values["scenario"], Redacted: values["redacted"],
		Record: values["record"], RecordedBy: values["recorded-by"],
	}
	if m.Tool != Docker && m.Tool != Podman {
		return Meta{}, fail(fileName, "tool %q is neither docker nor podman", m.Tool)
	}
	if !versionPattern.MatchString(m.Version) {
		return Meta{}, fail(fileName, "version %q is not a dotted version number", m.Version)
	}
	if m.Mode != Rootful && m.Mode != Rootless {
		return Meta{}, fail(fileName, "mode %q is neither rootful nor rootless", m.Mode)
	}
	if t, err := time.Parse(time.DateOnly, m.Recorded); err != nil || t.Format(time.DateOnly) != m.Recorded {
		return Meta{}, fail(fileName, "recorded %q is not a date (YYYY-MM-DD)", m.Recorded)
	}
	if !validRecord(m.Record) {
		return Meta{}, fail(fileName, "record %q is not replay, pending or \"skip: <reason>\"", m.Record)
	}
	if m.RecordedBy != "" && m.RecordedBy != recordedBySynthetic && m.RecordedBy != RecordedByContract {
		return Meta{}, fail(fileName, "recorded-by %q is neither %s nor %s", m.RecordedBy, recordedBySynthetic, RecordedByContract)
	}
	if m.Record != RecordPending {
		for _, key := range answerHeaders {
			if !hasKey(values, key) {
				return Meta{}, fail(fileName, "missing header %q", key)
			}
		}
	}
	return m, nil
}

func validRecord(v string) bool {
	if v == "" || v == RecordReplay || v == RecordPending {
		return true
	}
	reason, ok := strings.CutPrefix(v, recordSkipPrefix)
	return ok && reason != "" && !strings.HasPrefix(reason, " ")
}

// scenarioOf reads the scenario out of the file name and checks that the rest of
// the name says what the headers say.
func scenarioOf(fileName, stem string, m Meta) (string, error) {
	suffix := "--" + string(m.Tool) + "-" + m.Version + "-" + string(m.Mode)
	scenario, ok := strings.CutSuffix(stem, suffix)
	if !ok {
		return "", fail(fileName, "the name must end in %q, as the tool, version and mode headers say", suffix+fileSuffix)
	}
	if scenario == "" || strings.Contains(scenario, "--") {
		return "", fail(fileName, "the scenario part of the name (%q) is empty or holds \"--\"", scenario)
	}
	return scenario, nil
}

// parseArgv reads the JSON array of the argv header. The only accepted writing is
// the one Format produces.
func parseArgv(fileName, value string) ([]string, error) {
	var argv []string
	if err := json.Unmarshal([]byte(value), &argv); err != nil {
		return nil, fail(fileName, "argv is not a JSON array of strings: %v", err)
	}
	if len(argv) == 0 {
		return nil, fail(fileName, "argv is empty")
	}
	for i, word := range argv {
		if strings.ContainsAny(word, "\r\n") {
			return nil, fail(fileName, "argv element %d holds a line break", i)
		}
	}
	if encodeArgv(argv) != value {
		return nil, fail(fileName, "argv is not in canonical form (compact JSON, no HTML escapes)")
	}
	return argv, nil
}

// encodeArgv writes the argv as one line of compact JSON. encoding/json escapes
// the C0 controls but writes DEL and the C1 controls (U+007F to U+009F) as they
// are; they are written \u00xx here, so the header holds no raw control character
// and Parse, which compares against this writing, accepts nothing else.
func encodeArgv(argv []string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(argv); err != nil {
		return "null" // unreachable for a []string; null is refused by Parse
	}
	var out strings.Builder
	for _, r := range strings.TrimSuffix(b.String(), "\n") {
		if r >= 0x7f && isControl(r) {
			fmt.Fprintf(&out, "\\u%04x", r)
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

// streamHeader is a parsed "<n> bytes, text|base64".
type streamHeader struct {
	n   int
	enc string
}

func parseStreamHeader(fileName, name, value string) (streamHeader, error) {
	count, enc, ok := strings.Cut(value, " bytes, ")
	if !ok {
		return streamHeader{}, fail(fileName, "%s header %q is not \"<n> bytes, text|base64\"", name, value)
	}
	n, err := strconv.Atoi(count)
	if err != nil || n < 0 || strconv.Itoa(n) != count {
		return streamHeader{}, fail(fileName, "%s length %q is not a plain count", name, count)
	}
	if enc != encText && enc != encBase64 {
		return streamHeader{}, fail(fileName, "%s encoding %q is neither text nor base64", name, enc)
	}
	return streamHeader{n: n, enc: enc}, nil
}

// readAnswer reads the exit code and the two streams.
func readAnswer(fileName string, x *Exchange, values map[string]string, body []byte) error {
	exit, err := strconv.Atoi(values["exit"])
	if err != nil || exit < 0 || exit > maxExit || strconv.Itoa(exit) != values["exit"] {
		return fail(fileName, "exit %q is not an integer from 0 to %d", values["exit"], maxExit)
	}
	x.Exit = exit

	outHeader, err := parseStreamHeader(fileName, "stdout", values["stdout"])
	if err != nil {
		return err
	}
	errHeader, err := parseStreamHeader(fileName, "stderr", values["stderr"])
	if err != nil {
		return err
	}
	body, ok := bytes.CutPrefix(body, []byte(stdoutMark))
	if !ok {
		return fail(fileName, "the body does not start with %q", strings.TrimSuffix(stdoutMark, "\n"))
	}
	// Each stream is followed by a line feed, and stdout by the stderr marker.
	if x.Stdout, body, err = readStream(fileName, "stdout", outHeader, body, []byte("\n"+stderrMark), false); err != nil {
		return err
	}
	if x.Stderr, _, err = readStream(fileName, "stderr", errHeader, body, []byte("\n"), true); err != nil {
		return err
	}
	return nil
}

// readStream reads one stream of declared length from the start of body and
// returns it with what follows its terminator. The length is what delimits a
// text stream, so a line that looks like a marker inside it is data. A base64
// stream is delimited by its terminator, which its alphabet cannot contain. The
// last stream must end the file.
func readStream(fileName, name string, h streamHeader, body, term []byte, last bool) (data, rest []byte, err error) {
	if h.enc == encBase64 {
		return readBase64(fileName, name, h, body, term, last)
	}
	tail := []byte(nil)
	if h.n <= len(body) {
		tail = body[h.n:]
	}
	if h.n > len(body) || !terminates(tail, term, last) {
		return nil, nil, fail(fileName, "%s declares %d bytes but %d were found before its end", name, h.n, foundLength(body, term, last))
	}
	data = body[:h.n]
	if !textSafe(data) {
		return nil, nil, fail(fileName, "%s is declared text but holds a byte that only base64 may carry", name)
	}
	return bytes.Clone(data), tail[len(term):], nil
}

func terminates(tail, term []byte, last bool) bool {
	if last {
		return bytes.Equal(tail, term)
	}
	return bytes.HasPrefix(tail, term)
}

// streamEnd is where a stream of unknown length ends in body, or -1.
func streamEnd(body, term []byte, last bool) int {
	if last {
		if bytes.HasSuffix(body, term) {
			return len(body) - len(term)
		}
		return -1
	}
	return bytes.Index(body, term)
}

// foundLength is what an error reports as the length actually found.
func foundLength(body, term []byte, last bool) int {
	if i := streamEnd(body, term, last); i >= 0 {
		return i
	}
	return len(body)
}

func readBase64(fileName, name string, h streamHeader, body, term []byte, last bool) (data, rest []byte, err error) {
	end := streamEnd(body, term, last)
	if end < 0 {
		return nil, nil, fail(fileName, "%s has no end (the line feed that closes it, then the next marker)", name)
	}
	text := string(body[:end])
	data, err = base64.StdEncoding.DecodeString(strings.ReplaceAll(text, "\n", ""))
	if err != nil {
		return nil, nil, fail(fileName, "%s is not valid base64: %v", name, err)
	}
	if len(data) != h.n {
		return nil, nil, fail(fileName, "%s declares %d bytes but %d were found before its end", name, h.n, len(data))
	}
	if textSafe(data) {
		return nil, nil, fail(fileName, "%s is base64 but would be text: the only writing of text is text", name)
	}
	if wrapBase64(data) != text {
		return nil, nil, fail(fileName, "%s is not in canonical base64 (padded, %d columns per line)", name, base64Columns)
	}
	return data, body[end+len(term):], nil
}

// textSafe reports whether the bytes may be written as text: valid UTF-8 with no
// control character but the line feed and the tab (an escape sequence, DEL or a C1
// control would hide text from a reviewer or act on a terminal). A carriage return
// is a control character: it goes through base64.
func textSafe(b []byte) bool {
	if !utf8.Valid(b) {
		return false
	}
	for _, r := range string(b) {
		if r != '\n' && r != '\t' && isControl(r) {
			return false
		}
	}
	return true
}

func wrapBase64(data []byte) string {
	enc := base64.StdEncoding.EncodeToString(data)
	var lines []string
	for len(enc) > base64Columns {
		lines = append(lines, enc[:base64Columns])
		enc = enc[base64Columns:]
	}
	return strings.Join(append(lines, enc), "\n")
}

// Format writes a fixture. The name and the campaign are not part of the bytes;
// the headers are. For a file Parse accepts, Format(Parse(b)) == b.
func Format(f Fixture) []byte {
	var b bytes.Buffer
	header := func(key, value string) { fmt.Fprintf(&b, "# %s: %s\n", key, value) }
	m := f.Meta
	header("tool", string(m.Tool))
	header("version", m.Version)
	header("mode", string(m.Mode))
	header("recorded", m.Recorded)
	header("scenario", m.Scenario)
	header("redacted", m.Redacted)
	if m.Record != "" {
		header("record", m.Record)
	}
	if m.RecordedBy != "" {
		header("recorded-by", m.RecordedBy)
	}
	header("argv", encodeArgv(f.Exchange.Argv))
	if m.Record == RecordPending {
		b.WriteString("\n")
		return b.Bytes()
	}
	x := f.Exchange
	header("exit", strconv.Itoa(x.Exit))
	outEnc, outText := encodeStream(x.Stdout)
	errEnc, errText := encodeStream(x.Stderr)
	header("stdout", fmt.Sprintf("%d bytes, %s", len(x.Stdout), outEnc))
	header("stderr", fmt.Sprintf("%d bytes, %s", len(x.Stderr), errEnc))
	b.WriteString("\n" + stdoutMark + outText + "\n" + stderrMark + errText + "\n")
	return b.Bytes()
}

// encodeStream picks the one writing of a stream: text when it is safe, base64
// otherwise.
func encodeStream(data []byte) (enc, text string) {
	if textSafe(data) {
		return encText, string(data)
	}
	return encBase64, wrapBase64(data)
}
