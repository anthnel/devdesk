// Package netproxy decides which proxy a connection to a host travels through,
// the way an https request to that host would (DEVDESK-581), so that code which
// opens its own connections follows the same path as net/http.
package netproxy

import (
	"net/http"
	"net/url"
)

// Selector says which proxy carries a connection to u; nil, nil means direct.
// A nil Selector behaves like FromEnvironment.
//
// An error a Selector returns ends up in the text of a *Error (StepSelect) shown
// to the user, so it must never contain the raw proxy URL, which may carry
// credentials: describe the problem without echoing the value.
type Selector func(u *url.URL) (*url.URL, error)

// FromEnvironment is http.ProxyFromEnvironment(&http.Request{URL: u}): the same
// function and the same once-per-process cache as the HTTPS monitor.
func FromEnvironment(u *url.URL) (*url.URL, error) {
	return http.ProxyFromEnvironment(&http.Request{URL: u})
}

// ForRequest adapts s to http.Transport.Proxy. A nil s falls back to
// FromEnvironment, so the zero value follows the proxy environment variables.
func (s Selector) ForRequest(r *http.Request) (*url.URL, error) {
	if s == nil {
		return FromEnvironment(r.URL)
	}
	return s(r.URL)
}

// NewTransport returns http.DefaultTransport.Clone() with Proxy = s.ForRequest,
// so timeouts and limits match the default transport. A nil s is equivalent to
// FromEnvironment.
func NewTransport(s Selector) *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = s.ForRequest
	return t
}
