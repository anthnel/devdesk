package nexusfixture

import (
	"bytes"
	"fmt"
	"net/http"
	"net/textproto"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Identity is the account a fixture was recorded as; it comes from the file
// name suffix only.
type Identity string

// The identities of the NX-03 bench.
const (
	Admin     Identity = "admin"
	Pull      Identity = "pull"
	Anonymous Identity = "anon"

	// BadCredentials is a known account (admin) with a wrong, public password
	// (fakenexus.BadPassword): the bench answers it as it answers any bad login.
	BadCredentials Identity = "badcred"
)

// The values of the optional "flow" header: how the recorder follows a request
// that is answered by a challenge or a redirect.
const (
	FlowBearer    = "bearer"    // follow one Bearer challenge, then retry
	FlowRedirects = "redirects" // follow same-origin redirects, record each hop
)

// EffectCaches is the only value of the optional "effect" header: the request
// can cache an image in the proxy.
const EffectCaches = "caches"

// RecordPending marks a request-only fixture that the recorder has yet to fill.
const RecordPending = "pending"

const (
	fileSuffix    = ".http"
	identitySep   = "--"
	exchangeSep   = "###"
	httpVersion   = "HTTP/1.1"
	stubEmpty     = "<empty body>"
	stubHTMLStart = "<HTML body omitted:"
	// stubOmittedStart opens the summary of a body that is neither JSON nor HTML.
	stubOmittedStart = "<body omitted:"
)

// Meta is the commented header of a fixture. Free text is kept raw so that
// Format loses nothing.
type Meta struct {
	Scenario     string // the "# scenario:" header, free text; Fixture.Scenario is the file name's part
	NexusVersion string // first token of the nexus header
	Edition      string // second token of the nexus header
	NexusNote    string // the text in parentheses after the edition, without them
	Measured     string // raw value: a date, optionally followed by ", free text"
	Account      string
	Redacted     string
	State        string // "" = nominal
	Flow         string // "" = one exchange, or FlowBearer / FlowRedirects
	Effect       string // "" = none, or EffectCaches
	Record       string // "" = replay, "replay", "pending" or "skip: <reason>"
	RecordedBy   string // "" for an imported NX-03 file
}

// Exchange is one request and its answer. A request-only exchange (a pending
// fixture) has Status 0 and no response part.
type Exchange struct {
	Method, Target, Host string // Target = path + raw query as recorded
	RequestHeader        http.Header
	Status               int
	Reason               string      // reason phrase as recorded; http.StatusText when empty
	Header               http.Header // response, whitelisted
	Body                 []byte      // verbatim; nil when Stub != ""
	Stub                 string      // "<empty body>" or the HTML summary, as written
}

// Fixture is one file: a header and one or more exchanges.
type Fixture struct {
	// Name is the file name without ".http"; Scenario is the part of Name before
	// the identity suffix. Meta.Scenario is a different thing: the free text of the
	// "# scenario:" header, which need not match the file name.
	Campaign, Name, Scenario string
	Identity                 Identity
	Meta                     Meta
	Exchanges                []Exchange
}

var (
	versionPattern = regexp.MustCompile(`^\d+(\.\d+)+(-[0-9A-Za-z.]+)?$`)

	requiredHeaders = []string{"scenario", "nexus", "measured", "account", "redacted"}
	optionalHeaders = []string{"state", "flow", "effect", "record", "recorded-by"}

	requestOrder  = []string{"Authorization", "Accept"}
	responseOrder = []string{
		"Server", "Docker-Distribution-Api-Version", "Www-Authenticate", "Location", "Link",
		"Docker-Content-Digest", "Content-Type", "Content-Length",
	}
	// displayNames are the header names whose written form is not the
	// canonical one Go uses as a map key.
	displayNames = map[string]string{"Www-Authenticate": "WWW-Authenticate"}
)

func parseErr(fileName, format string, args ...any) error {
	return fmt.Errorf("nexusfixture: %s: %s", fileName, fmt.Sprintf(format, args...))
}

// Parse reads one fixture file. fileName is the base name, with ".http". A file
// is LF only: a carriage return anywhere is refused. A body line made of "###"
// preceded by any number of backslashes is read with one backslash less (Format
// writes it with one more), so that a body never reads as the exchange separator.
func Parse(campaign, fileName string, data []byte) (Fixture, error) {
	f := Fixture{Campaign: campaign}
	name, ok := strings.CutSuffix(fileName, fileSuffix)
	if !ok {
		return f, parseErr(fileName, "the file name must end in %s", fileSuffix)
	}
	f.Name = name
	i := strings.LastIndex(name, identitySep)
	if i < 0 {
		return f, parseErr(fileName, "no identity suffix, want --admin, --pull, --anon or --badcred")
	}
	f.Scenario, f.Identity = name[:i], Identity(name[i+len(identitySep):])
	switch f.Identity {
	case Admin, Pull, Anonymous, BadCredentials:
	default:
		return f, parseErr(fileName, "unknown identity suffix %q, want admin, pull, anon or badcred", string(f.Identity))
	}

	if bytes.IndexByte(data, '\r') >= 0 {
		return f, parseErr(fileName, "the file has a carriage return (CRLF line endings): fixtures use LF only")
	}

	meta, rest, err := parseMeta(string(data))
	if err != nil {
		return f, parseErr(fileName, "%v", err)
	}
	f.Meta = meta

	chunks := splitExchanges(rest)
	for n, chunk := range chunks {
		ex, err := parseExchange(chunk, meta.Record == RecordPending)
		if err != nil {
			if len(chunks) > 1 {
				return f, parseErr(fileName, "exchange %d: %v", n+1, err)
			}
			return f, parseErr(fileName, "%v", err)
		}
		f.Exchanges = append(f.Exchanges, ex)
	}
	return f, nil
}

// nextLine cuts the first line of s; ok is false when s is empty.
func nextLine(s string) (line, rest string, ok bool) {
	if s == "" {
		return "", "", false
	}
	line, rest, _ = strings.Cut(s, "\n")
	return line, rest, true
}

// parseMeta reads the "# key: value" lines and the blank line after them.
func parseMeta(text string) (Meta, string, error) {
	raw := map[string]string{}
	for strings.HasPrefix(text, "#") {
		var line string
		line, text, _ = nextLine(text)
		body, ok := strings.CutPrefix(line, "# ")
		if !ok {
			return Meta{}, "", fmt.Errorf("malformed header line, want \"# key: value\"")
		}
		key, value, _ := strings.Cut(body, ":")
		if !isKnownHeader(key) {
			return Meta{}, "", fmt.Errorf("unknown header %q", key)
		}
		if _, dup := raw[key]; dup {
			return Meta{}, "", fmt.Errorf("header %q given twice", key)
		}
		raw[key] = strings.TrimSpace(value)
	}
	for _, key := range requiredHeaders {
		if raw[key] == "" {
			return Meta{}, "", fmt.Errorf("missing %s header", key)
		}
	}
	m := Meta{
		Scenario: raw["scenario"], Measured: raw["measured"], Account: raw["account"], Redacted: raw["redacted"],
		State: raw["state"], Flow: raw["flow"], Effect: raw["effect"], Record: raw["record"], RecordedBy: raw["recorded-by"],
	}
	if err := parseNexus(&m, raw["nexus"]); err != nil {
		return Meta{}, "", err
	}
	if err := checkMeasured(m.Measured); err != nil {
		return Meta{}, "", err
	}
	if err := checkFlowAndEffect(m); err != nil {
		return Meta{}, "", err
	}
	if err := checkRecord(m.Record); err != nil {
		return Meta{}, "", err
	}
	line, rest, ok := nextLine(text)
	if !ok || line != "" {
		return Meta{}, "", fmt.Errorf("a blank line must separate the headers from the request")
	}
	return m, rest, nil
}

func isKnownHeader(key string) bool {
	for _, k := range requiredHeaders {
		if k == key {
			return true
		}
	}
	for _, k := range optionalHeaders {
		if k == key {
			return true
		}
	}
	return false
}

// parseNexus reads "<version> <EDITION> [(free text)]".
func parseNexus(m *Meta, v string) error {
	version, rest, _ := strings.Cut(v, " ")
	edition, rest, _ := strings.Cut(strings.TrimSpace(rest), " ")
	rest = strings.TrimSpace(rest)
	if !versionPattern.MatchString(version) {
		return fmt.Errorf("nexus header: %q is not a version", version)
	}
	if edition == "" {
		return fmt.Errorf("nexus header: the edition is missing after the version")
	}
	m.NexusVersion, m.Edition = version, edition
	if rest == "" {
		return nil
	}
	inner, ok := strings.CutPrefix(rest, "(")
	if ok {
		inner, ok = strings.CutSuffix(inner, ")")
	}
	if !ok {
		return fmt.Errorf("nexus header: the text after the edition must be in parentheses")
	}
	m.NexusNote = inner
	return nil
}

func checkMeasured(v string) error {
	if len(v) < len("2006-01-02") {
		return fmt.Errorf("measured header: %q does not start with a YYYY-MM-DD date", v)
	}
	if _, err := time.Parse("2006-01-02", v[:10]); err != nil {
		return fmt.Errorf("measured header: does not start with a YYYY-MM-DD date")
	}
	if after := v[10:]; after != "" && !strings.HasPrefix(after, ",") {
		return fmt.Errorf("measured header: the date may only be followed by \", free text\"")
	}
	return nil
}

func checkFlowAndEffect(m Meta) error {
	if m.Flow != "" && m.Flow != FlowBearer && m.Flow != FlowRedirects {
		return fmt.Errorf("flow header: %q is not %s or %s", m.Flow, FlowBearer, FlowRedirects)
	}
	if m.Effect != "" && m.Effect != EffectCaches {
		return fmt.Errorf("effect header: %q is not %s", m.Effect, EffectCaches)
	}
	return nil
}

func checkRecord(v string) error {
	if reason, ok := strings.CutPrefix(v, "skip: "); ok && strings.TrimSpace(reason) != "" {
		return nil
	}
	if v == "" || v == "replay" || v == RecordPending {
		return nil
	}
	return fmt.Errorf("record header: %q is not replay, pending or skip: <reason>", v)
}

// splitExchanges cuts the text after the header at each "###" line.
func splitExchanges(text string) []string {
	parts := strings.Split(text, "\n"+exchangeSep+"\n")
	for i := 0; i < len(parts)-1; i++ {
		parts[i] += "\n" // the line break the separator consumed
	}
	return parts
}

func parseExchange(text string, pending bool) (Exchange, error) {
	var ex Exchange
	line, text, _ := nextLine(text)
	fields := strings.Fields(line)
	if len(fields) != 3 || fields[2] != httpVersion {
		return ex, fmt.Errorf("request line must be \"METHOD target %s\"", httpVersion)
	}
	ex.Method, ex.Target = fields[0], fields[1]

	header, text, blank, err := readHeaders(text)
	if err != nil {
		return ex, fmt.Errorf("request: %w", err)
	}
	ex.Host = header.Get("Host")
	header.Del("Host")
	ex.RequestHeader = header
	if ex.Host == "" {
		return ex, fmt.Errorf("the request has no Host header")
	}

	if !blank || text == "" {
		if pending {
			return ex, nil
		}
		return ex, fmt.Errorf("the response is missing (only a pending fixture may hold a request alone)")
	}
	if pending {
		return ex, fmt.Errorf("a pending fixture must not carry a response")
	}
	return ex, parseResponse(&ex, text)
}

func parseResponse(ex *Exchange, text string) error {
	line, text, _ := nextLine(text)
	rest, ok := strings.CutPrefix(line, httpVersion+" ")
	if !ok {
		return fmt.Errorf("status line must be \"%s <code> <reason>\"", httpVersion)
	}
	code, reason, _ := strings.Cut(rest, " ")
	status, err := strconv.Atoi(code)
	if err != nil || status < 100 || status > 599 {
		return fmt.Errorf("status code is not a number from 100 to 599")
	}
	// Format trims the status line, so a blank or padded phrase cannot round-trip.
	ex.Status, ex.Reason = status, strings.TrimSpace(reason)

	header, text, blank, err := readHeaders(text)
	if err != nil {
		return fmt.Errorf("response: %w", err)
	}
	if !blank {
		return fmt.Errorf("a blank line must separate the response headers from the body")
	}
	ex.Header = header

	body := strings.TrimSuffix(text, "\n")
	if body == stubEmpty || isOmittedStub(body) {
		ex.Stub = body
	} else {
		ex.Body = []byte(unescapeBody(body))
	}
	return nil
}

// isOmittedStub reports whether a body is one of the two summaries Redact writes
// in place of a body: "<HTML body omitted: ...>" or "<body omitted: ...>".
func isOmittedStub(body string) bool {
	return (strings.HasPrefix(body, stubHTMLStart) || strings.HasPrefix(body, stubOmittedStart)) && strings.HasSuffix(body, ">")
}

// isSeparatorLike reports whether a body line is "###" preceded by any number
// of backslashes: the lines Format escapes and Parse unescapes.
func isSeparatorLike(line string) bool {
	return strings.HasSuffix(line, exchangeSep) && strings.Trim(line[:len(line)-len(exchangeSep)], `\`) == ""
}

// mapSeparatorLikeLines applies fn to each body line that isSeparatorLike.
func mapSeparatorLikeLines(body string, fn func(string) string) string {
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		if isSeparatorLike(l) {
			lines[i] = fn(l)
		}
	}
	return strings.Join(lines, "\n")
}

func escapeBody(body []byte) []byte {
	return []byte(mapSeparatorLikeLines(string(body), func(l string) string { return `\` + l }))
}

func unescapeBody(body string) string {
	return mapSeparatorLikeLines(body, func(l string) string { return l[1:] })
}

// readHeaders reads "Name: value" lines up to a blank line. blank is false when
// the text ended first.
func readHeaders(text string) (h http.Header, rest string, blank bool, err error) {
	h = http.Header{}
	for {
		var line string
		var ok bool
		line, text, ok = nextLine(text)
		switch {
		case !ok:
			return h, "", false, nil
		case line == "":
			return h, text, true, nil
		case strings.HasPrefix(line, "HTTP/"):
			return nil, "", false, fmt.Errorf("a blank line must separate the request from the response")
		}
		name, value, found := strings.Cut(line, ":")
		if !found || name == "" {
			return nil, "", false, fmt.Errorf("malformed header line, want \"Name: value\"")
		}
		h.Add(textproto.CanonicalMIMEHeaderKey(name), strings.TrimPrefix(value, " "))
	}
}

// Format writes a fixture in the file format; Format(Parse(b)) == b for a file
// the recorder or the NX-03 import wrote.
func Format(f Fixture) []byte {
	var b bytes.Buffer
	writeMeta(&b, f.Meta)
	for i, ex := range f.Exchanges {
		if i == 0 {
			b.WriteString("\n")
		} else {
			b.WriteString(exchangeSep + "\n")
		}
		writeExchange(&b, ex)
	}
	return b.Bytes()
}

func writeMeta(b *bytes.Buffer, m Meta) {
	nexus := m.NexusVersion + " " + m.Edition
	if m.NexusNote != "" {
		nexus += " (" + m.NexusNote + ")"
	}
	lines := []struct{ key, value string }{
		{"scenario", m.Scenario}, {"nexus", nexus}, {"measured", m.Measured},
		{"account", m.Account}, {"redacted", m.Redacted},
	}
	for _, l := range lines {
		fmt.Fprintf(b, "# %s: %s\n", l.key, l.value)
	}
	for _, l := range []struct{ key, value string }{
		{"state", m.State}, {"flow", m.Flow}, {"effect", m.Effect}, {"record", m.Record}, {"recorded-by", m.RecordedBy},
	} {
		if l.value != "" {
			fmt.Fprintf(b, "# %s: %s\n", l.key, l.value)
		}
	}
}

func writeExchange(b *bytes.Buffer, ex Exchange) {
	fmt.Fprintf(b, "%s %s %s\nHost: %s\n", ex.Method, ex.Target, httpVersion, ex.Host)
	writeHeaders(b, ex.RequestHeader, requestOrder, "Host")
	if ex.Status == 0 {
		return
	}
	reason := ex.Reason
	if reason == "" {
		reason = http.StatusText(ex.Status)
	}
	b.WriteString("\n" + strings.TrimSpace(fmt.Sprintf("%s %d %s", httpVersion, ex.Status, reason)) + "\n")
	writeHeaders(b, ex.Header, responseOrder)
	b.WriteString("\n")
	if ex.Stub != "" {
		b.WriteString(ex.Stub)
	} else {
		b.Write(escapeBody(ex.Body))
	}
	b.WriteString("\n")
}

// writeHeaders writes the headers in a fixed order (the preferred names first,
// then the others sorted) so that a regeneration does not reshuffle a file.
func writeHeaders(b *bytes.Buffer, h http.Header, preferred []string, skip ...string) {
	skipped := map[string]bool{}
	for _, s := range skip {
		skipped[s] = true
	}
	var names []string
	for _, p := range preferred {
		if _, ok := h[p]; ok && !skipped[p] {
			names = append(names, p)
		}
	}
	var others []string
	for k := range h {
		if skipped[k] || contains(preferred, k) {
			continue
		}
		others = append(others, k)
	}
	sort.Strings(others)
	for _, name := range append(names, others...) {
		display := name
		if d, ok := displayNames[name]; ok {
			display = d
		}
		for _, v := range h[name] {
			fmt.Fprintf(b, "%s: %s\n", display, v)
		}
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
