package nexusfixture

import (
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"sort"
	"strings"
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
// names). The bench's LAN addresses are not allowed.
var AllowedHosts = []string{"127.0.0.1", "bench-upstream.nexus-bench.svc.cluster.local"}

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
	jwtLike = regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}`)
	// secretFormat matches well-known secret formats: GitHub and GitLab tokens,
	// AWS access key ids, and the first line of a PEM private key.
	secretFormat = regexp.MustCompile(
		`gh[pousr]_[A-Za-z0-9]{36,}|glpat-[A-Za-z0-9_-]{20,}|(?:AKIA|ASIA)[0-9A-Z]{16}|-----BEGIN [A-Z ]*PRIVATE KEY-----`)
	// urlAuth reads the authority of a URL, also when its slashes are JSON-escaped.
	urlAuth = regexp.MustCompile(`(?i)https?:\\?/\\?/([^\s/"'<>?#\\,;)]+)`)
	// ipv4Like needs a non-numeric character before the address, not a word boundary:
	// "ip10.1.2.3" holds one.
	ipv4Like = regexp.MustCompile(`(?:^|[^0-9])(\d{1,3}(?:\.\d{1,3}){3})`)
	// ipv6Local starts a unique-local (fc00::/7) or link-local (fe80::/10) address.
	ipv6Local = regexp.MustCompile(`(?i)(?:^|[^0-9a-f])(?:f[cd][0-9a-f]{2}|fe[89ab][0-9a-f]):`)
	// bareHost is a host name in an internal zone; the group is the name.
	bareHost = regexp.MustCompile(`(?i)([a-z0-9_-]+(?:\.[a-z0-9_-]+)*\.(?:home\.arpa|lan|internal|local))(?:$|[^a-z0-9_-])`)
	// localPath is a user's home directory anchored at the start of a value (after
	// a quote, a blank or "="): "/home/" inside an image path is legitimate.
	// winProfile is a Windows profile path, raw or JSON-escaped.
	localPath  = regexp.MustCompile(`(?:^|["\s=])/(?:root|home|Users)/`)
	winProfile = regexp.MustCompile(`(?i)[a-z]:\\+users`)

	cgnat = netip.MustParsePrefix("100.64.0.0/10")

	sessionHeaders = []string{"cookie", "set-cookie", "x-nexus-ui"}
	// internalNames are matched ignoring case.
	internalNames = []string{"datomir", "secrets.json", "xdg_state_home"}
	// queryParams are the only parameters a request URL may carry (the paging of _catalog).
	queryParams = []string{"n", "last"}
	// jsonEscaper writes a term as it sits inside a JSON string.
	jsonEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, `/`, `\/`)
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
// or session header, a request header outside Host, Authorization and Accept, a
// response header outside ResponseHeaderAllowlist, a line of a header section
// that is not "Name: value", a request query parameter other than n and last, a
// JWT-like value or another well-known secret format, a JSON secret key whose
// value is not "<redacted>" (or null), a host outside AllowedHosts, a private,
// link-local, CGNAT or unique-local address, an internal name or local path, and
// each term of exact (recorder passwords and their Basic encodings, or an
// external deny list), also in another case, JSON-escaped or URL-escaped; an
// empty term matches nothing. The text is scanned as written, not parsed: a file
// that does not parse is still checked.
func CheckLeaks(fixture string, data []byte, exact []string) []Leak {
	c := &leakScan{fixture: fixture, seen: map[leakKey]bool{}, exact: exactVariants(exact)}
	text := string(data)
	state := stMeta
	for i, line := range strings.Split(text, "\n") {
		state = c.scanLine(i+1, line, state)
	}
	c.scanJSONSecrets(text)
	sort.SliceStable(c.leaks, func(i, j int) bool { return c.leaks[i].Line < c.leaks[j].Line })
	return c.leaks
}

// exactVariants lists, lower-cased, the forms of each term that can sit in a
// fixture: as given, JSON-escaped, URL-escaped as a query value and as a path.
func exactVariants(terms []string) []string {
	var out []string
	for _, term := range terms {
		if term == "" {
			continue
		}
		for _, v := range []string{term, jsonEscaper.Replace(term), url.QueryEscape(term), url.PathEscape(term)} {
			out = append(out, strings.ToLower(v))
		}
	}
	return out
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
	_, query, _ := strings.Cut(fields[1], "?")
	for param := range strings.SplitSeq(query, "&") {
		if name, _, _ := strings.Cut(param, "="); param != "" && !contains(queryParams, name) {
			c.add(n, ruleQuery)
		}
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
}

func allowedResponseHeader(name string) bool {
	return allowedHeader(ResponseHeaderAllowlist, name)
}

// allowedRequestHeader admits Host, Authorization and what Redact keeps of a request.
func allowedRequestHeader(name string) bool {
	return allowedHeader([]string{"Host", "Authorization"}, name) || allowedHeader(requestHeaderAllowlist, name)
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
	if jwtLike.MatchString(line) {
		c.add(n, ruleJWT)
	}
	if secretFormat.MatchString(line) {
		c.add(n, ruleSecretFormat)
	}
	if strings.Contains(lower, "http") {
		for _, m := range urlAuth.FindAllStringSubmatch(line, -1) {
			if !allowedAuthority(m[1]) {
				c.add(n, ruleHost)
			}
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
	// The cheap tests keep the regular expressions off the lines that cannot match.
	if strings.Contains(line, ".") {
		for _, m := range ipv4Like.FindAllStringSubmatch(line, -1) {
			if addr, err := netip.ParseAddr(m[1]); err == nil && (addr.IsPrivate() || addr.IsLinkLocalUnicast() || cgnat.Contains(addr)) {
				c.add(n, rulePrivateAddress)
			}
		}
	}
	if strings.Count(line, ":") > 1 && ipv6Local.MatchString(line) {
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
	if strings.Contains(line, ".") {
		for _, m := range bareHost.FindAllStringSubmatch(line, -1) {
			if !contains(AllowedHosts, strings.ToLower(m[1])) {
				c.add(n, ruleInternalName)
			}
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
	if strings.Contains(authority, "@") {
		return false
	}
	return allowedHost(authority)
}

// allowedHost checks a host with or without a port.
func allowedHost(hostPort string) bool {
	host := hostPort
	if h, _, err := net.SplitHostPort(hostPort); err == nil {
		host = h
	}
	host = strings.Trim(strings.ToLower(host), "[]")
	return contains(AllowedHosts, host)
}
