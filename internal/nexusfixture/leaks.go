package nexusfixture

import (
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/anthnel/devdesk/internal/fixtureleak"
)

// Leak is one finding of CheckLeaks. It names where and which rule, never what
// matched: a field for the matched text would put a secret in a test log.
type Leak struct {
	Fixture string
	Line    int // 1-based
	Rule    string
}

// AllowedHosts are the only hosts a fixture may name, in a URL or a Host
// header: the loopback address the bench is published on and the bench's own
// proxy upstream (the one internal/nexusbench/testdata/descriptor.example.json
// names). The bench's LAN addresses are not allowed. realm-probe.invalid
// (DEVDESK-580) is the reserved name the recorder sends in X-Forwarded-Host to see
// whether a Bearer realm follows it; .invalid never resolves, so it names no host.
var AllowedHosts = []string{"127.0.0.1", "bench-upstream.nexus-bench.svc.cluster.local", "realm-probe.invalid"}

// The rules, by the name a Leak carries.
const (
	ruleAuthorization  = "authorization-not-redacted"
	ruleSession        = "cookie-or-session"
	ruleHeaderAllowed  = "response-header-outside-allowlist"
	ruleRequestHeader  = "request-header-outside-allowlist"
	ruleMalformed      = "malformed-header-line"
	ruleJWT            = "jwt-like-value"
	ruleSecretFormat   = "secret-token-format"
	ruleJSONSecret     = "json-secret-not-redacted"
	ruleHost           = "host-not-allowed"
	rulePrivateAddress = "private-or-link-local-address"
	ruleInternalName   = "internal-name-or-path"
	ruleQuery          = "request-query-outside-allowlist"
	ruleExactTerm      = "exact-term"
)

// sessionKV is the name of the Nexus session cookie, refused anywhere in a fixture.
const sessionKV = "NXSESSIONID"

var (
	// localPath is a user's home directory anchored at the start of a value (after
	// a quote, a blank or "="): "/home/" inside an image path is legitimate.
	// winProfile is a Windows profile path, raw or JSON-escaped.
	localPath  = regexp.MustCompile(`(?:^|["\s=])/(?:root|home|Users)/`)
	winProfile = regexp.MustCompile(`(?i)[a-z]:\\+users`)

	sessionHeaders = []string{"cookie", "set-cookie", "x-nexus-ui"}
	// internalNames are matched ignoring case.
	internalNames = []string{"datomir", "secrets.json", "xdg_state_home"}
	// queryParams are the parameters a request URL may carry (the paging of _catalog);
	// tokenQueryParams are the only ones added on a token path ("account" is never
	// admitted: it names the user).
	queryParams      = []string{"n", "last"}
	tokenQueryParams = []string{"service", "scope"}
	// tokenPath is the path of a Bearer token request, on the connector or under a repository.
	tokenPath = regexp.MustCompile(`^(?:/repository/[A-Za-z0-9._-]+)?/v2/token$`)
	// probeHeaders are the request headers of the realm probe, besides Host,
	// Authorization and Accept; their value goes through probeValueAllowed.
	probeHeaders = []string{"X-Forwarded-Host", "X-Forwarded-Proto"}
)

// lineState is where a line sits in the file: the commented header, or one of
// the parts of an exchange.
type lineState int

const (
	stMeta lineState = iota
	stReqLine
	stReqHeaders
	stRespLine
	stRespHeaders
	stBody
)

// CheckLeaks scans one fixture, line by line (lines count from 1), for what a
// fixture must not hold: an Authorization other than the placeholders, a cookie
// or session header, a request header outside Host, Authorization, Accept and the
// two probe headers (X-Forwarded-Host, X-Forwarded-Proto) with an allowed value, a
// response header outside ResponseHeaderAllowlist, a line of a header section
// that is not "Name: value", a request query parameter other than n and last (and,
// on a token path only, service, whose value goes through the host rules, and
// scope), a
// JWT-like value or another well-known secret format, a JSON secret key whose
// value is not "<redacted>" (or null), a host outside AllowedHosts, a private,
// link-local, CGNAT or unique-local address, an internal name or local path, and
// each term of exact (recorder passwords and their Basic encodings, or an
// external deny list), also in another case, JSON-escaped or URL-escaped; an
// empty term matches nothing. The text is scanned as written, not parsed: a file
// that does not parse is still checked.
func CheckLeaks(fixture string, data []byte, exact []string) []Leak {
	c := &leakScan{fixture: fixture, seen: map[leakKey]bool{}, exact: fixtureleak.ExactVariants(exact)}
	text := string(data)
	state := stMeta
	for i, line := range strings.Split(text, "\n") {
		state = c.scanLine(i+1, line, state)
	}
	c.scanJSONSecrets(text)
	sort.SliceStable(c.leaks, func(i, j int) bool { return c.leaks[i].Line < c.leaks[j].Line })
	return c.leaks
}

type leakKey struct {
	line int
	rule string
}

type leakScan struct {
	fixture string
	exact   []string // lower-cased variants of the exact terms
	leaks   []Leak
	seen    map[leakKey]bool
}

// add records a rule once per line.
func (c *leakScan) add(line int, rule string) {
	k := leakKey{line, rule}
	if c.seen[k] {
		return
	}
	c.seen[k] = true
	c.leaks = append(c.leaks, Leak{Fixture: c.fixture, Line: line, Rule: rule})
}

// scanLine checks one line and returns the state of the next.
func (c *leakScan) scanLine(n int, line string, state lineState) lineState {
	if line == exchangeSep && state != stMeta {
		return stReqLine
	}
	switch state {
	case stReqLine:
		c.scanRequestLine(n, line)
	case stReqHeaders, stRespHeaders:
		c.scanHeader(n, line, state)
	}
	c.scanText(n, line)
	return nextState(state, line)
}

func nextState(state lineState, line string) lineState {
	switch state {
	case stMeta:
		if line == "" {
			return stReqLine
		}
	case stReqLine:
		return stReqHeaders
	case stReqHeaders:
		if line == "" {
			return stRespLine
		}
	case stRespLine:
		return stRespHeaders
	case stRespHeaders:
		if line == "" {
			return stBody
		}
	}
	return state
}

// scanRequestLine checks the query of "METHOD target HTTP/1.1".
func (c *leakScan) scanRequestLine(n int, line string) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return
	}
	path, query, _ := strings.Cut(fields[1], "?")
	onToken := IsTokenPath(path)
	for param := range strings.SplitSeq(query, "&") {
		name, value, _ := strings.Cut(param, "=")
		switch {
		case param == "" || contains(queryParams, name):
		case onToken && contains(tokenQueryParams, name):
			if name == "service" {
				c.scanService(n, value)
			}
		default:
			c.add(n, ruleQuery)
		}
	}
}

// IsTokenPath reports whether a request path (no query) is that of a Bearer token
// request: "/v2/token", or the same under "/repository/<name>".
func IsTokenPath(path string) bool { return tokenPath.MatchString(path) }

// scanService applies the host rules to the value of a token request's service
// parameter, once URL-decoded: a URL (its authority), or a bare host and port.
func (c *leakScan) scanService(n int, value string) {
	decoded, err := url.QueryUnescape(value)
	if err != nil {
		decoded = value
	}
	if len(fixtureleak.URLAuthorities(decoded)) == 0 && !allowedAuthority(decoded) {
		c.add(n, ruleHost)
	}
	if decoded != value {
		c.scanText(n, decoded)
	}
}

func (c *leakScan) scanHeader(n int, line string, state lineState) {
	if line == "" {
		return
	}
	name, value, ok := strings.Cut(line, ":")
	name, value = strings.TrimSpace(name), strings.TrimSpace(value)
	if !ok || name == "" {
		c.add(n, ruleMalformed)
		return
	}
	lower := strings.ToLower(name)
	switch {
	case lower == "authorization" && value != authBasicRedacted && value != authBearerRedacted:
		c.add(n, ruleAuthorization)
	case contains(sessionHeaders, lower):
		c.add(n, ruleSession)
	}
	if state == stRespHeaders && !allowedResponseHeader(name) {
		c.add(n, ruleHeaderAllowed)
	}
	if state == stReqHeaders && !allowedRequestHeader(name) {
		c.add(n, ruleRequestHeader)
	}
	if state == stReqHeaders && lower == "host" && !allowedHost(value) {
		c.add(n, ruleHost)
	}
	if state == stReqHeaders && allowedHeader(probeHeaders, name) && !probeValueAllowed(lower, value) {
		c.add(n, ruleHost)
	}
}

// probeValueAllowed reports whether a probe header (lower-cased name) may carry
// the value: a host of AllowedHosts for X-Forwarded-Host, and exactly http or
// https for X-Forwarded-Proto, the scheme a real probe sends (never a host).
func probeValueAllowed(lowerName, value string) bool {
	if lowerName == "x-forwarded-proto" {
		return value == "http" || value == "https"
	}
	return allowedHost(value)
}

func allowedResponseHeader(name string) bool {
	return allowedHeader(ResponseHeaderAllowlist, name)
}

// allowedRequestHeader admits Host, Authorization and what Redact keeps of a request.
func allowedRequestHeader(name string) bool {
	return allowedHeader([]string{"Host", "Authorization"}, name) ||
		allowedHeader(requestHeaderAllowlist, name) || allowedHeader(probeHeaders, name)
}

func allowedHeader(allowlist []string, name string) bool {
	canonical := http.CanonicalHeaderKey(name)
	for _, a := range allowlist {
		if http.CanonicalHeaderKey(a) == canonical {
			return true
		}
	}
	return false
}

// scanText applies the rules that hold on any line, header or body.
func (c *leakScan) scanText(n int, line string) {
	lower := strings.ToLower(line)
	if strings.Contains(lower, strings.ToLower(sessionKV)) {
		c.add(n, ruleSession)
	}
	if fixtureleak.JWTLike(line) {
		c.add(n, ruleJWT)
	}
	if fixtureleak.SecretFormat(line) {
		c.add(n, ruleSecretFormat)
	}
	for _, authority := range fixtureleak.URLAuthorities(line) {
		if !allowedAuthority(authority) {
			c.add(n, ruleHost)
		}
	}
	c.scanAddresses(n, line)
	c.scanInternal(n, line, lower)
	for _, term := range c.exact {
		if strings.Contains(lower, term) {
			c.add(n, ruleExactTerm)
		}
	}
}

// scanAddresses reports private, link-local, CGNAT and unique-local addresses.
func (c *leakScan) scanAddresses(n int, line string) {
	if fixtureleak.LocalAddresses(line, nil) {
		c.add(n, rulePrivateAddress)
	}
}

// scanInternal reports internal names, bare internal host names and local paths.
func (c *leakScan) scanInternal(n int, line, lower string) {
	for _, name := range internalNames {
		if strings.Contains(lower, name) {
			c.add(n, ruleInternalName)
		}
	}
	for _, name := range fixtureleak.InternalHostNames(line) {
		if !contains(AllowedHosts, strings.ToLower(name)) {
			c.add(n, ruleInternalName)
		}
	}
	if (strings.Contains(line, "/") && localPath.MatchString(line)) || (strings.Contains(line, `\`) && winProfile.MatchString(line)) {
		c.add(n, ruleInternalName)
	}
}

// scanJSONSecrets runs over the whole text, not line by line, because Redact
// replaces a secret value wherever it sits relative to its key. The leak is
// reported at the line of the key; lines are counted from where the previous key
// was, so the scan stays linear.
func (c *leakScan) scanJSONSecrets(text string) {
	line, counted := 1, 0
	for from := 0; ; {
		key, value, ok := nextSecretKey(text, from, true)
		if !ok {
			return
		}
		line += strings.Count(text[counted:key], "\n")
		counted = key
		if keptValueLen(text[value:]) == 0 {
			c.add(line, ruleJSONSecret)
		}
		from = value
	}
}

// allowedAuthority checks the authority of a URL: no user info, and a host in
// AllowedHosts.
func allowedAuthority(authority string) bool {
	return fixtureleak.AllowedAuthority(authority, AllowedHosts)
}

// allowedHost checks a host with or without a port.
func allowedHost(hostPort string) bool {
	return fixtureleak.AllowedHost(hostPort, AllowedHosts)
}
