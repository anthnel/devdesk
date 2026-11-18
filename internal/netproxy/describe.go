package netproxy

import (
	"net"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// DecisionKind says what the proxy environment variables mean for one URL.
type DecisionKind int

const (
	// DecisionNone: no variable is set for this URL's scheme. It is the zero
	// value, so an unfilled Decision reads as "no proxy".
	DecisionNone DecisionKind = iota
	// DecisionProxied: a proxy carries this URL.
	DecisionProxied
	// DecisionExcluded: a valid variable is set and NO_PROXY names the target.
	DecisionExcluded
	// DecisionLoopback: a valid variable is set, but Go never proxies
	// localhost or a loopback address.
	DecisionLoopback
	// DecisionInvalid: the variable is set and is not a usable proxy URL.
	DecisionInvalid
)

// Decision is what Describe found out about one URL. It carries no credential:
// Proxy is Display's output and an invalid value is named by its variable only.
type Decision struct {
	Kind DecisionKind
	// Variable is the name of the variable read for the URL's scheme, with the
	// case it was read in (HTTPS_PROXY or https_proxy); empty for None.
	Variable string
	// Proxy is Display(proxy) when Kind is DecisionProxied, empty otherwise.
	Proxy string
}

// Describe reports which proxy, if any, applies to u in the process
// environment. The answer comes from FromEnvironment, the function every
// DevDesk client uses; only the variable's name and validity are read here.
func Describe(u *url.URL) Decision {
	return describe(u, os.Getenv, FromEnvironment)
}

// describe is Describe with its inputs injected. sel is not consulted when the
// variables alone settle the answer (None, Invalid), so it may be nil then.
func describe(u *url.URL, getenv func(string) string, sel Selector) Decision {
	var names [2]string
	switch strings.ToLower(u.Scheme) {
	case "https":
		names = [2]string{"HTTPS_PROXY", "https_proxy"}
	case "http":
		names = [2]string{"HTTP_PROXY", "http_proxy"}
	default:
		return Decision{}
	}
	name, value := firstNonEmpty(names, getenv)
	if name == "" {
		return Decision{}
	}
	if !usableProxyValue(value) {
		return Decision{Kind: DecisionInvalid, Variable: name}
	}
	proxy, err := sel(u)
	if err != nil {
		// The error text can quote the proxy URL, credentials included.
		return Decision{Kind: DecisionInvalid, Variable: name}
	}
	if proxy != nil {
		return Decision{Kind: DecisionProxied, Variable: name, Proxy: Display(proxy)}
	}
	// A nil proxy with a valid variable: Go skips a loopback target whatever
	// NO_PROXY says, otherwise NO_PROXY named it.
	if isLoopbackHost(u.Hostname()) {
		return Decision{Kind: DecisionLoopback, Variable: name}
	}
	return Decision{Kind: DecisionExcluded, Variable: name}
}

// firstNonEmpty returns the first of names whose value is not empty, with that
// value, like Go's getEnvAny: upper case first, an empty value is not defined.
// It returns two empty strings when none is set.
func firstNonEmpty(names [2]string, getenv func(string) string) (name, value string) {
	for _, n := range names {
		if v := getenv(n); v != "" {
			return n, v
		}
	}
	return "", ""
}

// explicitScheme matches a value that starts with a URL scheme, as opposed to
// one that only has "://" somewhere in its path or query.
var explicitScheme = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*://`)

// usableProxyValue follows Go's parseProxy (url.Parse, then a retry with
// "http://" in front for a value without a scheme) and is stricter on three
// points, because Go ignores an unusable value and FromEnvironment alone
// cannot tell it from "no proxy":
//   - the host must not be empty;
//   - a value holding "@" must end up with userinfo, otherwise the credential
//     was mangled (an unescaped "/", "?" or "#" before the "@") and the text
//     before it became the host;
//   - the host must be printable UTF-8, since it ends up on screen.
func usableProxyValue(value string) bool {
	_, ok := parseProxyValue(value)
	return ok
}

// parseProxyValue is the parsing behind usableProxyValue: it returns the URL
// the value stands for, and false when the value is not usable.
func parseProxyValue(value string) (*url.URL, bool) {
	explicit := explicitScheme.MatchString(value)
	u, err := url.Parse(value)
	switch {
	case err != nil && explicit:
		return nil, false
	case err == nil && u.Scheme != "" && u.Host != "":
		// Parsed as written.
	case err == nil && explicit:
		// An explicit scheme with no host, such as "http://".
		return nil, false
	default:
		u, err = url.Parse("http://" + value)
		if err != nil {
			return nil, false
		}
	}
	if u.Host == "" || (u.User == nil && strings.Contains(value, "@")) {
		return nil, false
	}
	return u, isPrintable(u.Host)
}

// isPrintable reports whether s is valid UTF-8 made of printable runes only.
func isPrintable(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// isLoopbackHost reports whether host is localhost or a loopback address.
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.Unmap().IsLoopback()
}

// defaultPorts are the ports Display shows when the URL names none.
var defaultPorts = map[string]string{
	"http":    "80",
	"https":   "443",
	"socks5":  "1080",
	"socks5h": "1080",
}

// Display renders a proxy URL as scheme://host:port and nothing else: never
// userinfo, path, query, fragment or IPv6 zone. It is the only way a proxy leaves this
// package for the screen. A scheme with no known default port shows no port
// unless the URL names one. Display(nil) is empty.
func Display(proxy *url.URL) string {
	if proxy == nil {
		return ""
	}
	port := proxy.Port()
	if port == "" {
		port = defaultPorts[strings.ToLower(proxy.Scheme)]
	}
	// The zone of a link-local address can be any text: drop it.
	host, _, _ := strings.Cut(proxy.Hostname(), "%")
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return proxy.Scheme + "://" + host
}
