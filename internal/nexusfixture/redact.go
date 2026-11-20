package nexusfixture

import (
	"encoding/json"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// redacted is what replaces a credential or a secret value in a fixture.
const redacted = "<redacted>"

// Authorization values a fixture may carry: the scheme and nothing else.
const (
	authBasicRedacted  = "Basic " + redacted
	authBearerRedacted = "Bearer " + redacted
)

// ResponseHeaderAllowlist is the closed list of response headers a fixture
// keeps: everything else (Date, Set-Cookie, proxy and Nexus internals) is
// dropped by Redact and refused by CheckLeaks.
var ResponseHeaderAllowlist = []string{
	"Server", "Docker-Distribution-Api-Version", "WWW-Authenticate", "Location", "Link",
	"Docker-Content-Digest", "Content-Type", "Content-Length",
}

// requestHeaderAllowlist is what Redact keeps of a request, besides the
// normalized Authorization.
var requestHeaderAllowlist = []string{"Accept"}

// RawExchange is an exchange as the recorder read it from a Nexus: nothing is
// redacted yet, so it must never be written to a file as is.
type RawExchange struct {
	Method, Target, Host string
	RequestHeader        http.Header
	Status               int
	Header               http.Header
	Body                 []byte
}

var htmlTitle = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

// addressPlaceholder replaces a private, link-local or CGNAT IPv4 address (with
// its port) in the message of a JSON error.
const addressPlaceholder = "<redacted-address>"

// Redact turns a raw exchange into one that is safe to write to a fixture: the
// Authorization value becomes a placeholder, request and response headers are
// filtered to their allowlists, an empty body is marked, a JSON body has the
// values of its secret keys replaced in place and the private addresses of its
// message, detail and error values hidden, and any other body (HTML included) is
// summarized: nothing tells where a secret sits in it. It does not modify raw.
func Redact(raw RawExchange) Exchange {
	ex := Exchange{
		Method: raw.Method, Target: raw.Target, Host: raw.Host,
		RequestHeader: redactRequestHeader(raw.RequestHeader),
		Status:        raw.Status,
		Header:        http.Header{},
	}
	for _, name := range ResponseHeaderAllowlist {
		if values := raw.Header.Values(name); len(values) > 0 {
			ex.Header[http.CanonicalHeaderKey(name)] = append([]string(nil), values...)
		}
	}
	media := mediaType(raw.Header.Get("Content-Type"))
	switch {
	case len(raw.Body) == 0:
		ex.Stub = stubEmpty
	case isHTML(media, raw.Body):
		ex.Stub = htmlStub(raw.Body)
	case isJSON(media):
		ex.Body = []byte(redactAddresses(redactSecretJSON(string(raw.Body))))
	default:
		ex.Stub = omittedStub(media, len(raw.Body))
	}
	return ex
}

func redactRequestHeader(in http.Header) http.Header {
	out := http.Header{}
	if auth := in.Get("Authorization"); auth != "" {
		scheme, _, _ := strings.Cut(strings.TrimSpace(auth), " ")
		switch strings.ToLower(scheme) {
		case "basic":
			out.Set("Authorization", authBasicRedacted)
		case "bearer":
			out.Set("Authorization", authBearerRedacted)
		}
		// Any other scheme carries a secret we cannot place: the header is dropped.
	}
	for _, name := range requestHeaderAllowlist {
		if values := in.Values(name); len(values) > 0 {
			out[http.CanonicalHeaderKey(name)] = append([]string(nil), values...)
		}
	}
	// A probe header is kept only with a value CheckLeaks admits; otherwise it is dropped.
	for _, name := range probeHeaders {
		for _, v := range in.Values(name) {
			if v = strings.TrimSpace(v); probeValueAllowed(strings.ToLower(name), v) {
				out.Add(name, v)
			}
		}
	}
	return out
}

// mediaType is the lower-case media type of a Content-Type value, without its
// parameters; "" when the value is absent or does not parse.
func mediaType(contentType string) string {
	media, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}
	return media
}

func isJSON(media string) bool {
	return media == "application/json" || strings.HasSuffix(media, "+json")
}

func isHTML(media string, body []byte) bool {
	if media == "text/html" {
		return true
	}
	head := strings.ToLower(strings.TrimSpace(string(body[:min(len(body), 64)])))
	return strings.HasPrefix(head, "<!doctype html") || strings.HasPrefix(head, "<html")
}

var plainMediaType = regexp.MustCompile(`^[a-z0-9.+_-]+/[a-z0-9.+_-]+$`)

// omittedStub summarizes a body that is neither JSON nor HTML as
// "<body omitted: N bytes, media/type>". The media type comes from the server,
// so it is kept only when it is made of plain characters.
func omittedStub(media string, size int) string {
	if !plainMediaType.MatchString(media) {
		media = "unknown"
	}
	return stubOmittedStart + " " + strconv.Itoa(size) + " bytes, " + media + ">"
}

// htmlStub summarizes an HTML body as "<HTML body omitted: N bytes, <title>T</title>>"
// (the title is left out when there is none). The title is flattened to one
// line so that the stub stays one.
func htmlStub(body []byte) string {
	stub := stubHTMLStart + " " + strconv.Itoa(len(body)) + " bytes"
	if m := htmlTitle.FindSubmatch(body); m != nil {
		stub += ", <title>" + strings.Join(strings.Fields(string(m[1])), " ") + "</title>"
	}
	return stub + ">"
}

// redactSecretJSON replaces the value of each secret JSON key with "<redacted>",
// whatever the value is: a string, a number, a literal, or a whole object or
// array (balanced, strings skipped). A null value is kept: it holds no secret.
// Only the replaced values change, every other byte stays as sent. A value that
// is cut short is replaced up to the end of the text.
func redactSecretJSON(body string) string {
	var b strings.Builder
	last := 0
	for from := 0; ; {
		_, value, ok := nextSecretKey(body, from, false)
		if !ok {
			break
		}
		end := value + keptValueLen(body[value:])
		if end == value {
			end = valueEnd(body, value)
			b.WriteString(body[last:value])
			b.WriteString(`"` + redacted + `"`)
			last = end
		}
		from = end
	}
	b.WriteString(body[last:])
	return b.String()
}

// secretKeySuffixes end the lower-cased name of a key that holds a secret
// ("accessToken", "proxy_password", "apiKey"); "tokenType" does not end with one.
var secretKeySuffixes = []string{
	"token", "tokens", "password", "passwords", "passwd", "passphrase", "secret", "secrets",
	"apikey", "api_key", "api-key", "privatekey", "private_key", "private-key",
	"credential", "credentials", "uploaderip",
}

// decodeKey returns a JSON key, as written between its quotes, with its escapes
// decoded: "pass\u0077ord" is "password".
func decodeKey(raw string) string {
	if !strings.Contains(raw, `\`) {
		return raw
	}
	var key string
	if json.Unmarshal([]byte(`"`+raw+`"`), &key) == nil {
		return key
	}
	return strings.ReplaceAll(raw, `\`, "")
}

// isSecretKey reports whether a JSON key, as written between its quotes, names a
// secret.
func isSecretKey(raw string) bool {
	lower := strings.ToLower(decodeKey(raw))
	for _, suffix := range secretKeySuffixes {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

const jsonBlanks = " \t\r\n"

func skipBlanks(text string, i int) int {
	for i < len(text) && strings.IndexByte(jsonBlanks, text[i]) >= 0 {
		i++
	}
	return i
}

// stringEnd returns the index just after the string that opens at start, and
// whether it was closed. An unclosed string ends at the end of the text, or, with
// lineBound, at the first line break: a JSON string never holds one, and a scan
// of a whole fixture file must not let a stray quote shift every later pairing.
func stringEnd(text string, start int, lineBound bool) (int, bool) {
	for i := start + 1; i < len(text); i++ {
		switch text[i] {
		case '\\':
			if !lineBound || i+1 >= len(text) || text[i+1] != '\n' {
				i++ // the escaped character
			}
		case '"':
			return i + 1, true
		case '\n':
			if lineBound {
				return i, false
			}
		}
	}
	return len(text), false
}

// nextSecretKey finds the first member at or after from whose key names a
// secret. It returns where the key starts and where its value starts (after the
// colon and the blanks).
func nextSecretKey(text string, from int, lineBound bool) (key, value int, ok bool) {
	return nextKey(text, from, lineBound, isSecretKey)
}

// nextKey finds the first member at or after from whose key, as written between
// its quotes, satisfies match. It returns where the key starts and where its value
// starts (after the colon and the blanks).
func nextKey(text string, from int, lineBound bool, match func(rawKey string) bool) (key, value int, ok bool) {
	for i := from; i < len(text); {
		q := strings.IndexByte(text[i:], '"')
		if q < 0 {
			break
		}
		start := i + q
		end, closed := stringEnd(text, start, lineBound)
		i = end
		if !closed {
			continue
		}
		if colon := skipBlanks(text, end); colon < len(text) && text[colon] == ':' && match(text[start+1:end-1]) {
			return start, skipBlanks(text, colon+1), true
		}
	}
	return 0, 0, false
}

// valueEnd returns the index just after the JSON value that starts at start: a
// string, an object or array up to its matching bracket (strings skipped), or a
// bare number or literal. A value cut short ends at the end of the text.
func valueEnd(text string, start int) int {
	if start >= len(text) {
		return start
	}
	switch text[start] {
	case '"':
		end, _ := stringEnd(text, start, false)
		return end
	case '{', '[':
		depth := 0
		for i := start; i < len(text); i++ {
			switch text[i] {
			case '"':
				end, _ := stringEnd(text, i, false)
				i = end - 1
			case '{', '[':
				depth++
			case '}', ']':
				if depth--; depth == 0 {
					return i + 1
				}
			}
		}
		return len(text)
	}
	i := start
	for i < len(text) && strings.IndexByte(",}]"+jsonBlanks, text[i]) < 0 {
		i++
	}
	return i
}

// keptValueLen is the length of the value at the start of rest when it holds no
// secret, "<redacted>" or null, and 0 for any other value.
func keptValueLen(rest string) int {
	const placeholder = `"` + redacted + `"`
	switch {
	case strings.HasPrefix(rest, placeholder):
		return len(placeholder)
	case strings.HasPrefix(rest, "null") && (len(rest) == 4 || strings.IndexByte(",}]"+jsonBlanks, rest[4]) >= 0):
		return 4
	}
	return 0
}

// addressKeys are the JSON keys whose values are error text, where a server may
// name an address it could not reach (lower case).
var addressKeys = []string{"message", "detail", "error"}

func isAddressKey(raw string) bool {
	return contains(addressKeys, strings.ToLower(decodeKey(raw)))
}

// redactAddresses replaces each private, link-local or CGNAT IPv4 address, with
// its port when there is one, by "<redacted-address>" in the values of the
// message, detail and error keys. An address anywhere else is left as sent, for
// CheckLeaks to refuse. Every other byte stays as sent.
func redactAddresses(body string) string {
	var b strings.Builder
	last := 0
	for from := 0; ; {
		_, value, ok := nextKey(body, from, false, isAddressKey)
		if !ok {
			break
		}
		end := valueEnd(body, value)
		b.WriteString(body[last:value])
		b.WriteString(hideAddresses(body[value:end]))
		last, from = end, end
	}
	b.WriteString(body[last:])
	return b.String()
}

// hideAddresses applies the address rule to one JSON value.
func hideAddresses(value string) string {
	if !strings.Contains(value, ".") {
		return value
	}
	var b strings.Builder
	last := 0
	for _, m := range ipv4Like.FindAllStringSubmatchIndex(value, -1) {
		start, end := m[2], m[3]
		if !isLocalAddress(value[start:end]) {
			continue
		}
		b.WriteString(value[last:start])
		b.WriteString(addressPlaceholder)
		last = end + portLen(value[end:])
	}
	b.WriteString(value[last:])
	return b.String()
}

// portLen is the length of a ":port" (one to five digits) at the start of s, or 0.
func portLen(s string) int {
	if !strings.HasPrefix(s, ":") {
		return 0
	}
	n := 1
	for n < len(s) && n <= 5 && s[n] >= '0' && s[n] <= '9' {
		n++
	}
	if n == 1 {
		return 0
	}
	return n
}
