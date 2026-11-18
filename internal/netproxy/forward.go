package netproxy

import (
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
)

// ForwardedVariables are the variables a container receives, in this order,
// each name followed by its other casing. ALL_PROXY and FTP_PROXY are not read.
var ForwardedVariables = []string{
	"HTTP_PROXY", "http_proxy",
	"HTTPS_PROXY", "https_proxy",
	"NO_PROXY", "no_proxy",
}

// redacted stands for a proxy value that could not be parsed, so it has no
// display form.
const redacted = "[redacted]"

// forwardPair is one variable in both casings, with the value they both carry.
type forwardPair struct {
	names [2]string
	value string
	// read is the name the value was read from, with the case it had.
	read string
}

// Forward is a snapshot of the proxy variables as a child process must receive
// them. It holds raw values, credentials included, and never prints them: String
// and GoString show the display form. A Forward kept in an unexported field is
// walked by fmt, which cannot call String there, so the values sit behind a
// function: fmt prints an address for it, with every verb (a pointer to a struct
// would be followed by %s). The zero value forwards nothing.
type Forward struct {
	get func() []forwardPair
}

// pairs returns the variables held, none for the zero value.
func (f Forward) pairs() []forwardPair {
	if f.get == nil {
		return nil
	}
	return f.get()
}

// containerForward reads the environment once, like http.ProxyFromEnvironment.
var containerForward = sync.OnceValue(func() Forward {
	return ForwardFromLookup(os.LookupEnv)
})

// ContainerForward is the proxy environment of this process, read once.
func ContainerForward() Forward { return containerForward() }

// ForwardFromLookup reads the variables through lookup. For each of HTTP, HTTPS
// and NO, the first non-empty value of the upper case name then the lower case
// name is given to both names. The value is not validated.
func ForwardFromLookup(lookup func(string) (string, bool)) Forward {
	getenv := func(name string) string {
		v, _ := lookup(name)
		return v
	}
	var pairs []forwardPair
	for i := 0; i < len(ForwardedVariables); i += 2 {
		names := [2]string{ForwardedVariables[i], ForwardedVariables[i+1]}
		if read, value := firstNonEmpty(names, getenv); read != "" {
			pairs = append(pairs, forwardPair{names: names, value: value, read: read})
		}
	}
	if len(pairs) == 0 {
		return Forward{}
	}
	return Forward{get: func() []forwardPair { return pairs }}
}

// Empty reports whether there is nothing to forward.
func (f Forward) Empty() bool { return len(f.pairs()) == 0 }

// Names lists the variables to pass, in ForwardedVariables order.
func (f Forward) Names() []string {
	var names []string
	for _, p := range f.pairs() {
		names = append(names, p.names[:]...)
	}
	return names
}

// Environ returns base without any of the six forwarded names, followed by
// NAME=value for every name to forward. base is not modified; when there is
// nothing to forward it is returned as is.
func (f Forward) Environ(base []string) []string {
	if f.Empty() {
		return base
	}
	env := make([]string, 0, len(base)+len(f.pairs())*2)
	for _, kv := range base {
		name, _, _ := strings.Cut(kv, "=")
		if !slices.Contains(ForwardedVariables, name) {
			env = append(env, kv)
		}
	}
	for _, p := range f.pairs() {
		for _, name := range p.names {
			env = append(env, name+"="+p.value)
		}
	}
	return env
}

// Loopback reports whether a proxy the container would use is on this machine,
// where it cannot be reached from inside the container. The HTTPS pair is tried
// before the HTTP pair and the first loopback value wins. variable is the name
// that was read and display carries no credential.
func (f Forward) Loopback() (variable, display string, ok bool) {
	for _, upper := range []string{"HTTPS_PROXY", "HTTP_PROXY"} {
		for _, p := range f.pairs() {
			if p.names[0] != upper {
				continue
			}
			if u, usable := parseProxyValue(p.value); usable && isLoopbackHost(u.Hostname()) {
				return p.read, Display(u), true
			}
		}
	}
	return "", "", false
}

// Redact removes every credential held by f from s: a raw proxy value that
// carries credentials becomes its display form (or [redacted] when it is
// unusable), and its userinfo and its password disappear, as written in the
// value, as Go escapes them and decoded. A value with no credentials is left as
// the tool wrote it, so a diagnostic still names what was dialed. For an
// unusable value holding "@", everything before the last "@" is taken for a
// userinfo. A secret shorter than 4 bytes is only removed in the full value and
// in user:password@, since replacing it anywhere would damage the text; a bare
// user name shorter than 4 bytes is not removed at all.
func (f Forward) Redact(s string) string { return f.Redactor()(s) }

// Redactor is Redact with the replacements prepared once, for a caller that
// redacts many lines.
func (f Forward) Redactor() func(string) string {
	var r redaction
	for _, p := range f.pairs() {
		if p.names[0] != "NO_PROXY" {
			r.addValue(p.value)
		}
	}
	return r.replacer()
}

// redaction collects what Redact removes. strings.Replacer tries its arguments
// in order at each position, so replacer lists the longest kinds first.
type redaction struct {
	values    [][2]string // raw value and what stands for it
	userinfos []string    // removed together with their "@"
	secrets   []string    // replaced wherever they appear
}

// addValue records one raw proxy value and the credentials it holds. A value
// with none is not recorded: replacing it by its display form would only distort
// the text around it.
func (r *redaction) addValue(value string) {
	display := redacted
	u, usable := parseProxyValue(value)
	hasCredentials := !usable && strings.Contains(value, "@")
	if usable {
		display = Display(u)
		if u.User != nil {
			hasCredentials = true
			r.addUserinfo(u.User.String())
		}
	}
	if !hasCredentials {
		return
	}
	r.values = append(r.values, [2]string{value, display})
	rest := value[len(explicitScheme.FindString(value)):]
	if usable {
		// The authority ends at the first "/", "?" or "#".
		if end := strings.IndexAny(rest, "/?#"); end >= 0 {
			rest = rest[:end]
		}
	}
	// Written out, whatever the escaping: up to the last "@".
	if at := strings.LastIndex(rest, "@"); at >= 0 {
		r.addUserinfo(rest[:at])
	}
}

// addUserinfo records a userinfo as written, user[:password], and its decoded
// form. The secret is the password, or the user name when the password is
// absent or empty.
func (r *redaction) addUserinfo(info string) {
	user, password, _ := strings.Cut(info, ":")
	secret := password
	if secret == "" {
		secret = user
	}
	for _, v := range unescapedToo(info) {
		// A bare short name would match the local part of an address.
		if v != "" && (strings.Contains(v, ":") || len(v) >= 4) {
			r.userinfos = append(r.userinfos, v)
		}
	}
	for _, v := range unescapedToo(secret) {
		if len(v) >= 4 {
			r.secrets = append(r.secrets, v)
		}
	}
}

// unescapedToo returns s and, when it differs and decodes, its percent-decoded form.
func unescapedToo(s string) []string {
	if d, err := url.PathUnescape(s); err == nil && d != s {
		return []string{s, d}
	}
	return []string{s}
}

// replacer builds the replacements once and returns the function applying them.
func (r *redaction) replacer() func(string) string {
	var olds []string
	byLength := func(a, b string) int { return len(b) - len(a) }
	slices.SortStableFunc(r.values, func(a, b [2]string) int { return byLength(a[0], b[0]) })
	for _, v := range r.values {
		olds = append(olds, v[0], v[1])
	}
	slices.SortStableFunc(r.userinfos, byLength)
	for _, info := range slices.Compact(r.userinfos) {
		olds = append(olds, info+"@", "")
	}
	slices.SortStableFunc(r.secrets, byLength)
	for _, secret := range slices.Compact(r.secrets) {
		olds = append(olds, secret, redacted)
	}
	if len(olds) == 0 {
		return func(s string) string { return s }
	}
	return strings.NewReplacer(olds...).Replace
}

// String shows the variables with the proxies in display form, never raw. A
// NO_PROXY value with a non-printable character is not shown, since it ends up
// on a terminal.
func (f Forward) String() string {
	if f.Empty() {
		return "no proxy variables"
	}
	parts := make([]string, 0, len(f.pairs()))
	for _, p := range f.pairs() {
		shown := redacted
		if p.names[0] == "NO_PROXY" {
			if isPrintable(p.value) {
				shown = p.value
			}
		} else if u, ok := parseProxyValue(p.value); ok {
			shown = Display(u)
		}
		parts = append(parts, p.names[0]+"="+shown)
	}
	return strings.Join(parts, " ")
}

// GoString is String, so %#v shows no raw value either.
func (f Forward) GoString() string { return f.String() }
