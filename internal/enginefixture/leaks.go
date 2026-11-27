package enginefixture

import (
	"fmt"
	"net/netip"
	"os"
	"os/user"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/anthnel/devdesk/internal/fixtureleak"
	"github.com/anthnel/devdesk/internal/secretenv"
)

// What replaces the identity of the machine that recorded a fixture. The recorder
// (OC-09a) writes them and CheckLeaks accepts them.
const (
	PlaceholderUser = "fixture-user"
	PlaceholderHost = "fixture-host"
	PlaceholderHome = "/home/" + PlaceholderUser
)

// The rules, by the name a Leak carries.
const (
	ruleSecretFormat   = "secret-token-format"
	ruleJWT            = "jwt-like-value"
	ruleHost           = "host-not-allowed"
	rulePrivateAddress = "private-or-link-local-address"
	ruleInternalName   = "internal-name"
	rulePersonalPath   = "personal-path"
	ruleLabIdentity    = "lab-identity"
	ruleLocalIdentity  = "local-identity"
	ruleExactTerm      = "exact-term"
	ruleSensitiveEnv   = "sensitive-env-value"
	ruleInvisible      = "invisible-character"
)

// redactedValue is what stands for the value of a sensitive variable.
const redactedValue = "<redacted>"

// Leak is one finding of CheckLeaks. It names where and which rule, never what
// matched: a field for the matched text would put a secret in a test log.
type Leak struct {
	Fixture string
	Line    int // 1-based, in the file; a leak in a base64 stream is at the line of its header
	Rule    string
}

// LabTerms are the names of the test laboratory (tools/engine-vms/README.md): they
// are public there and must not appear in a fixture, which records an engine, not
// the lab that ran it.
var LabTerms = []string{"dd", "dd-docker", "dd-podman", "jakku"}

// AllowedHosts are the only hosts a fixture may name in a URL: the loopback and the
// registries an engine contacts by default.
var AllowedHosts = []string{"127.0.0.1", "localhost", "index.docker.io", "registry-1.docker.io", "docker.io", "quay.io"}

// EngineBridgeNets are the default networks of the engines, the only private
// addresses a fixture may hold: the address pools of Docker (172.17.0.0/16 to
// 172.31.0.0/16) and the networks of Podman (10.88.0.0/16 and 10.89.0.0/16). Any
// other private range is the recording machine's own network.
var EngineBridgeNets = engineBridgeNets()

func engineBridgeNets() []netip.Prefix {
	var nets []netip.Prefix
	for second := 17; second <= 31; second++ {
		nets = append(nets, netip.MustParsePrefix(fmt.Sprintf("172.%d.0.0/16", second)))
	}
	return append(nets, netip.MustParsePrefix("10.88.0.0/16"), netip.MustParsePrefix("10.89.0.0/16"))
}

// ImageHomes lists the user homes a container image keeps, which the personal-path
// rule would otherwise report: the name under /home, mapped to the reason (the
// image that keeps it). It starts empty, and each entry says why.
var ImageHomes = map[string]string{}

// AllowedInternalNames lists the names in an internal zone that an engine or a
// cluster writes itself and that say nothing about the recording machine, mapped to
// the reason. An entry that starts with "*." admits any name under that suffix. Each
// entry says why; a name that is not listed is reported by internal-name.
var AllowedInternalNames = map[string]string{
	"host.docker.internal":     "the name Docker Desktop and --add-host give to the host (ExtraHosts)",
	"host.containers.internal": "the name Podman gives to the host (ExtraHosts, /etc/hosts of a container)",
	"*.svc.cluster.local":      "the in-cluster service names of Kubernetes (kind)",
}

// AllowedSensitiveEnvNames lists the variables whose name looks sensitive and whose
// value is public, mapped to the reason: the rule sensitive-env-value lets them
// through. Each entry says why.
var AllowedSensitiveEnvNames = map[string]string{
	"GPG_KEY": "the python image sets it to the id of the public key that signs its sources",
}

// identityStopList holds the names too common to be an identity: a machine called
// by one of them is not told apart by it.
var identityStopList = []string{
	"root", "user", "admin", "runner", "ubuntu", "docker", "podman", "ci", "build", "test",
	"localhost", "debian", "fedora", "alpine", "node",
}

// minIdentityLen is the shortest name worth looking for.
const minIdentityLen = 3

// LocalIdentityTerms keeps, of the names given, those worth looking for in a
// fixture: at least three characters, not in the stop list (any case) and not a
// replacement name ("fixture-..."). The order is kept.
func LocalIdentityTerms(names ...string) []string {
	var out []string
	for _, name := range names {
		name = strings.TrimSpace(name)
		lower := strings.ToLower(name)
		if utf8.RuneCountInString(name) < minIdentityLen || slices.Contains(identityStopList, lower) ||
			strings.HasPrefix(lower, "fixture-") {
			continue
		}
		out = append(out, name)
	}
	return out
}

// MachineIdentities are the identities of the machine that runs the test: its host
// name and the name of its user (os/user and $USER), filtered by
// LocalIdentityTerms and without duplicates. The test hands them to CheckLeaksAs.
func MachineIdentities() []string {
	var names []string
	if h, err := os.Hostname(); err == nil {
		names = append(names, h)
	}
	if u, err := user.Current(); err == nil {
		names = append(names, u.Username)
	}
	names = append(names, os.Getenv("USER"))
	var out []string
	for _, term := range LocalIdentityTerms(names...) {
		if !slices.ContainsFunc(out, func(t string) bool { return strings.EqualFold(t, term) }) {
			out = append(out, term)
		}
	}
	return out
}

var (
	// pathPrefix is what may precede a home directory: the start of the line, a
	// blank or a character that opens or separates a value. "/home/" inside an
	// image path ("team/home/api") has a letter before it and is not a home.
	// A "file://" URL and a WSL mount of a Windows drive (/mnt/c/Users) are homes too.
	pathPrefix = `(?:^|[\s"'=:,|(\[]|file://)`
	unixHome   = regexp.MustCompile(pathPrefix + `(?:/var/home|/home|/Users|/mnt/[A-Za-z]/(?i:users))/([^/\s"'<>:,;|()\[\]\\]+)`)
	rootHome   = regexp.MustCompile(pathPrefix + `/root(?:$|[^A-Za-z0-9_.-])`)
	// winHome is a Windows profile, raw or JSON-escaped.
	winHome = regexp.MustCompile(`(?i)[a-z]:\\+users\\+([^\\\s"'/]+)`)
	// envAssignment is NAME= ; the value is read from what follows.
	envAssignment = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)=`)
)

// CheckLeaks reads one fixture file and reports what a fixture must not hold:
// the rules secret-token-format and jwt-like-value, host-not-allowed (a URL
// authority outside AllowedHosts, or with user info), private-or-link-local-address
// (outside EngineBridgeNets), internal-name (a name in an internal zone),
// personal-path, lab-identity (LabTerms), exact-term (each term of exact, the
// external deny list), sensitive-env-value (NAME=value for a name secretenv deems
// sensitive, unless the value is "<redacted>") and invisible-character (a
// bidirectional control or a zero-width character, which the text encoding lets
// through and a reviewer cannot see).
//
// A valid file is read by its parts: the headers (the argv included, so a pending
// file is checked), and each stream decoded, so that base64 does not hide a leak; a
// leak in a base64 stream is reported at the line of its header. An invalid file is
// read as raw text, line by line: a broken file is not a way round the check.
//
// The names (lab, exact terms) are matched at the word, ignoring the case, also
// JSON-escaped and URL-escaped. A word boundary is any character that is not a
// letter, a digit or an underscore: "-", "/", "@" and "." all are, so a host name
// is seen at the start, in the middle or at the end of a fully qualified name. A
// leak is reported once per line and rule, and never repeats the text found.
func CheckLeaks(fixture string, data []byte, exact []string) []Leak {
	return CheckLeaksAs(fixture, data, exact, nil)
}

// CheckLeaksAs is CheckLeaks with the identities of the machine that runs the test
// (the host name, the user name; see MachineIdentities), reported as
// local-identity. They are given, not read, so that a test can plant one.
func CheckLeaksAs(fixture string, data []byte, exact, local []string) []Leak {
	s := &leakScan{
		fixture:   fixture,
		seen:      map[leakKey]bool{},
		lab:       fixtureleak.ExactVariants(labTerms(false)),
		labStrict: fixtureleak.ExactVariants(labTerms(true)),
		local:     fixtureleak.ExactVariants(local),
		exact:     fixtureleak.ExactVariants(exact),
	}
	for _, u := range leakUnits(data) {
		s.examine(u.line, u.text)
	}
	sort.SliceStable(s.leaks, func(i, j int) bool { return s.leaks[i].Line < s.leaks[j].Line })
	return s.leaks
}

type leakKey struct {
	line int
	rule string
}

type leakScan struct {
	fixture           string
	lab, local, exact []string // lower-cased variants
	labStrict         []string // the lab terms too short for a loose boundary
	leaks             []Leak
	seen              map[leakKey]bool
}

// add records a rule once per line.
func (s *leakScan) add(line int, rule string) {
	k := leakKey{line, rule}
	if s.seen[k] {
		return
	}
	s.seen[k] = true
	s.leaks = append(s.leaks, Leak{Fixture: s.fixture, Line: line, Rule: rule})
}

// leakUnit is a piece of text to examine and the line it is reported at.
type leakUnit struct {
	line int
	text string
}

// leakUnits cuts the file into what is examined. A valid file gives its header
// lines, the lines of its text streams and the decoded lines of its base64 streams
// (at the line of their header); anything else gives its raw lines.
func leakUnits(data []byte) []leakUnit {
	lines := strings.Split(string(data), "\n")
	f, err := parse("", "", "", data, false)
	headerEnd := slices.Index(lines, "")
	if err != nil || headerEnd < 0 {
		return rawUnits(lines)
	}
	units := rawUnits(lines[:headerEnd])
	if f.Meta.Record == RecordPending {
		return units
	}
	// After the blank line come "--- stdout", the stdout lines, "--- stderr" and the
	// stderr lines. The length of each stream, not a marker, tells where it ends.
	next := headerEnd + 2
	for _, st := range []struct {
		name string
		data []byte
	}{{"stdout", f.Exchange.Stdout}, {"stderr", f.Exchange.Stderr}} {
		enc, text := encodeStream(st.data)
		if enc == encBase64 {
			header := 1 + slices.IndexFunc(lines[:headerEnd], func(l string) bool { return strings.HasPrefix(l, "# "+st.name+": ") })
			for _, l := range strings.Split(string(st.data), "\n") {
				units = append(units, leakUnit{header, l})
			}
		} else {
			for i, l := range strings.Split(text, "\n") {
				units = append(units, leakUnit{next + i + 1, l})
			}
		}
		next += strings.Count(text, "\n") + 1 + 1 // the stream, then the next marker
	}
	return units
}

func rawUnits(lines []string) []leakUnit {
	units := make([]leakUnit, 0, len(lines))
	for i, l := range lines {
		units = append(units, leakUnit{i + 1, l})
	}
	return units
}

// examine applies every rule to one line of text.
func (s *leakScan) examine(n int, text string) {
	if text == "" {
		return
	}
	s.examineText(n, text)
	if strings.IndexFunc(text, isInvisible) >= 0 {
		s.add(n, ruleInvisible)
		// A zero-width character can split a name: read the line without them too.
		s.examineText(n, strings.Map(func(r rune) rune {
			if isInvisible(r) {
				return -1
			}
			return r
		}, text))
	}
}

func (s *leakScan) examineText(n int, text string) {
	if fixtureleak.SecretFormat(text) {
		s.add(n, ruleSecretFormat)
	}
	if fixtureleak.JWTLike(text) {
		s.add(n, ruleJWT)
	}
	for _, authority := range fixtureleak.URLAuthorities(text) {
		if !fixtureleak.AllowedAuthority(authority, AllowedHosts) {
			s.add(n, ruleHost)
		}
	}
	if fixtureleak.LocalAddresses(text, EngineBridgeNets) {
		s.add(n, rulePrivateAddress)
	}
	for _, name := range fixtureleak.InternalHostNames(text) {
		if !allowedInternalName(name) {
			s.add(n, ruleInternalName)
			break
		}
	}
	if personalPath(text) {
		s.add(n, rulePersonalPath)
	}
	lower := strings.ToLower(text)
	for _, w := range []struct {
		rule     string
		variants []string
	}{{ruleLabIdentity, s.lab}, {ruleLocalIdentity, s.local}, {ruleExactTerm, s.exact}} {
		if containsWord(lower, w.variants) {
			s.add(n, w.rule)
		}
	}
	if containsStrictWord(lower, s.labStrict) {
		s.add(n, ruleLabIdentity)
	}
	if sensitiveEnv(text) {
		s.add(n, ruleSensitiveEnv)
	}
}

// personalPath reports a home directory that is not the replacement one or an image's.
func personalPath(text string) bool {
	if strings.Contains(text, "/") {
		// JSON-escaped slashes ("\/home\/x") are read as slashes.
		for _, t := range []string{text, strings.ReplaceAll(text, `\/`, "/")} {
			if rootHome.MatchString(t) {
				return true
			}
			for _, m := range unixHome.FindAllStringSubmatch(t, -1) {
				if !allowedHome(m[1]) {
					return true
				}
			}
		}
	}
	if strings.Contains(text, `\`) {
		for _, m := range winHome.FindAllStringSubmatch(text, -1) {
			if !allowedHome(m[1]) {
				return true
			}
		}
	}
	return false
}

func allowedHome(name string) bool {
	if name == PlaceholderUser {
		return true
	}
	_, listed := ImageHomes[name]
	return listed
}

// sensitiveEnv reports a NAME=value of a sensitive variable whose value is neither
// empty nor "<redacted>". A quoted value is a value (X_PASSWORD="hunter2" leaks);
// MY_TOKEN= and MY_TOKEN="" do not, and nor does a name of AllowedSensitiveEnvNames.
func sensitiveEnv(text string) bool {
	if !strings.Contains(text, "=") {
		return false
	}
	for _, m := range envAssignment.FindAllStringSubmatchIndex(text, -1) {
		name := text[m[2]:m[3]]
		if _, public := AllowedSensitiveEnvNames[name]; public || !secretenv.Default().Sensitive(name) {
			continue
		}
		if !emptyOrRedacted(text[m[1]:]) {
			return true
		}
	}
	return false
}

// emptyOrRedacted reads what follows "NAME=": nothing, a blank, a pair of quotes or a
// lone quote (the one that closes a JSON string) and the value "<redacted>", quoted or
// not, are no value; anything else is one.
func emptyOrRedacted(rest string) bool {
	// A JSON-escaped quote (\") is a quote.
	if strings.HasPrefix(rest, `\"`) || strings.HasPrefix(rest, `\'`) {
		rest = rest[1:]
	}
	if rest != "" && (rest[0] == '"' || rest[0] == '\'') {
		rest = rest[1:]
		if rest == "" || strings.ContainsRune(`"'`, rune(rest[0])) || strings.HasPrefix(rest, `\"`) || strings.HasPrefix(rest, `\'`) {
			return true // "" or a lone closing quote, escaped or not
		}
	}
	return rest == "" || strings.HasPrefix(rest, redactedValue) || strings.ContainsRune(" \t,]}", rune(rest[0]))
}

// containsWord reports whether one of the lower-cased variants sits in lower as a
// word: not glued to a letter, a digit or an underscore on either side.
func containsWord(lower string, variants []string) bool {
	for _, v := range variants {
		for from := 0; from < len(lower); {
			i := strings.Index(lower[from:], v)
			if i < 0 {
				break
			}
			i += from
			if j := i + len(v); !endsInWord(lower[:i]) && !startsWithWord(lower[j:]) {
				return true
			}
			from = i + 1
		}
	}
	return false
}

// strictBoundaries are the characters that may stand next to a short lab term (a
// term under minIdentityLen characters, "dd"): a blank, a quote, a path or value
// separator. Neither "." nor "-" is one, so "dd.conf", "yyyy-MM-dd" and "dd-mm" are
// not the lab user.
const strictBoundaries = " \t\"'/:@,;()[]=\\|<>"

// containsStrictWord is containsWord with the strict boundaries: the start or the
// end of the line, or one of strictBoundaries, on each side.
func containsStrictWord(lower string, variants []string) bool {
	for _, v := range variants {
		for from := 0; from < len(lower); {
			i := strings.Index(lower[from:], v)
			if i < 0 {
				break
			}
			i += from
			j := i + len(v)
			if (i == 0 || strings.IndexByte(strictBoundaries, lower[i-1]) >= 0) &&
				(j == len(lower) || strings.IndexByte(strictBoundaries, lower[j]) >= 0) {
				return true
			}
			from = i + 1
		}
	}
	return false
}

// labTerms returns the lab terms that need strict boundaries (short ones) or the
// others.
func labTerms(short bool) []string {
	var out []string
	for _, t := range LabTerms {
		if (utf8.RuneCountInString(t) < minIdentityLen) == short {
			out = append(out, t)
		}
	}
	return out
}

// allowedInternalName reports a name of AllowedInternalNames, or under one of its
// "*." entries.
func allowedInternalName(name string) bool {
	lower := strings.ToLower(name)
	for entry := range AllowedInternalNames {
		entry = strings.ToLower(entry)
		if suffix, wild := strings.CutPrefix(entry, "*"); wild {
			if len(lower) > len(suffix) && strings.HasSuffix(lower, suffix) {
				return true
			}
		} else if lower == entry {
			return true
		}
	}
	return false
}

func isWordRune(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

func endsInWord(s string) bool {
	r, size := utf8.DecodeLastRuneInString(s)
	return size > 0 && isWordRune(r)
}

func startsWithWord(s string) bool {
	r, size := utf8.DecodeRuneInString(s)
	return size > 0 && isWordRune(r)
}

// isInvisible reports a character that is valid text and that a reviewer cannot
// see: the zero-width characters and direction marks (U+200B to U+200F), the
// bidirectional embeddings and overrides (U+202A to U+202E), the word joiner
// (U+2060), the isolates (U+2066 to U+2069) and the byte order mark (U+FEFF).
func isInvisible(r rune) bool {
	switch {
	case r >= 0x200b && r <= 0x200f, r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
		return true
	}
	return r == 0x2060 || r == 0xfeff
}
