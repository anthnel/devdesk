package nexusbench

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

const (
	// clientTimeout bounds every REST call, connection included.
	clientTimeout = 15 * time.Second
	// maxResponseBytes is the most the client reads from one answer. A larger
	// one is an error, and its content is never decoded.
	maxResponseBytes = 1 << 20
	// drainBytes is how much of an unwanted answer is read so the connection
	// can be reused.
	drainBytes = 4 << 10
)

// APIError is a REST call the instance answered with a status outside 2xx. It
// carries the operation and the status only: no URL, no request body, no answer
// body, because any of them could hold a credential.
type APIError struct {
	// Op says what the call was for, e.g. "create user bench-pull".
	Op     string
	Status int
	// RemoteURLBlocked is true for a 400 whose validation errors name
	// proxy.remoteUrl: Nexus 3.98.1 resolves the host of a proxy's remote URL
	// when it saves the repository and refuses one that does not resolve. Only
	// this verdict is kept, never the text of the answer.
	RemoteURLBlocked bool
	// RemoteURLPrivate is true for a 400 whose validation errors name
	// proxy.remoteUrl and say its host resolves to a private address: the SSRF
	// protection of Nexus 3.98 refuses it unless the host is allowed (H-SSRF).
	// Like RemoteURLBlocked, only the verdict is kept.
	RemoteURLPrivate bool
}

// Error returns "<Op>: HTTP <status> <text>".
func (e *APIError) Error() string {
	return strings.TrimSpace(fmt.Sprintf("%s: HTTP %d %s", e.Op, e.Status, http.StatusText(e.Status)))
}

// transportError is a call that got no answer: connection refused, reset,
// timeout. The cause stays in Err; its text carries the URL and is not printed.
type transportError struct {
	Op  string
	Err error
}

func (e *transportError) Error() string { return "no answer to " + e.Op }
func (e *transportError) Unwrap() error { return e.Err }

// tooLargeError is an answer past maxResponseBytes.
type tooLargeError struct{ Op string }

func (e *tooLargeError) Error() string {
	return fmt.Sprintf("the answer to %s is larger than %d bytes", e.Op, maxResponseBytes)
}

// tooManyError is a listing past maxListedComponents.
type tooManyError struct{ Op string }

func (e *tooManyError) Error() string {
	return fmt.Sprintf("the answer to %s lists more than %d components", e.Op, maxListedComponents)
}

// decodeError is an answer that is not the JSON the client expects.
type decodeError struct {
	Op  string
	Err error
}

func (e *decodeError) Error() string { return "unexpected answer to " + e.Op }
func (e *decodeError) Unwrap() error { return e.Err }

// client is the REST client of one Nexus instance. It follows no redirection
// (an answer that moves the call elsewhere would carry the credentials with
// it), reads at most maxResponseBytes of an answer, and never logs a header.
type client struct {
	base     string
	user     string
	password string
	http     *http.Client
}

// newClient returns a client for baseURL, authenticating with Basic auth as user
// unless user is empty (the calls that need no account).
func newClient(baseURL, user, password string) *client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// The bench is reached directly: a proxy named by the environment would
	// receive the credentials.
	transport.Proxy = nil
	return &client{
		base:     strings.TrimRight(baseURL, "/"),
		user:     user,
		password: password,
		http: &http.Client{
			Timeout:       clientTimeout,
			Transport:     transport,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// closeIdle drops the connections the client keeps open.
func (c *client) closeIdle() { c.http.CloseIdleConnections() }

// request is one REST call.
type request struct {
	op          string
	method      string
	path        string
	query       url.Values
	contentType string
	body        []byte
}

// response is what the instance answered.
type response struct {
	status int
	header http.Header
	body   []byte
	// remoteURLBlocked is the verdict of remoteURLRefused on a 400; the body of
	// an answer outside 2xx is read for it and dropped.
	remoteURLBlocked bool
	// remoteURLPrivate is the verdict of remoteURLPrivate on the same 400.
	remoteURLPrivate bool
}

// do sends r and returns the answer whatever its status. The error is a
// *transportError or a *tooLargeError.
func (c *client) do(ctx context.Context, r request) (response, error) {
	target := c.base + r.path
	if len(r.query) > 0 {
		target += "?" + r.query.Encode()
	}
	var body io.Reader
	if r.body != nil {
		body = bytes.NewReader(r.body)
	}
	req, err := http.NewRequestWithContext(ctx, r.method, target, body)
	if err != nil {
		return response{}, &transportError{Op: r.op, Err: err}
	}
	req.Header.Set("Accept", "application/json")
	if r.contentType != "" {
		req.Header.Set("Content-Type", r.contentType)
	}
	if c.user != "" {
		req.SetBasicAuth(c.user, c.password)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return response{}, &transportError{Op: r.op, Err: err}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		refused, _ := io.ReadAll(io.LimitReader(resp.Body, drainBytes))
		badRequest := resp.StatusCode == http.StatusBadRequest
		return response{
			status: resp.StatusCode, header: resp.Header,
			remoteURLBlocked: badRequest && remoteURLRefused(refused),
			remoteURLPrivate: badRequest && remoteURLPrivate(refused),
		}, nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return response{}, &transportError{Op: r.op, Err: err}
	}
	if len(data) > maxResponseBytes {
		return response{}, &tooLargeError{Op: r.op}
	}
	return response{status: resp.StatusCode, header: resp.Header, body: data}, nil
}

// exchange sends one GET and returns the status, the headers and the body of
// the answer whatever the status, a 404 included (unlike do, which drops the
// body of an answer outside 2xx): it is what the fixture recorder keeps. target
// is the path and the raw query, as a fixture records it, appended to the base
// of the client. Only header is sent, plus the Basic credentials of the client
// when it has any; the Authorization value of header is a placeholder in a
// fixture and is never sent. No redirection is followed, and an answer past
// maxResponseBytes is a *tooLargeError whatever its status. Any other method
// than GET, or a target that is not an absolute path, is refused before
// anything is sent.
func (c *client) exchange(ctx context.Context, method, target string, header http.Header) (status int, h http.Header, body []byte, err error) {
	const op = "read the answer to a recorded request"
	if method != http.MethodGet {
		return 0, nil, nil, errors.New("exchange sends GET requests only")
	}
	if !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") {
		return 0, nil, nil, errors.New("exchange needs a target that is a path")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+target, nil)
	if err != nil {
		return 0, nil, nil, &transportError{Op: op, Err: err}
	}
	for name, values := range header {
		if http.CanonicalHeaderKey(name) == "Authorization" {
			continue
		}
		for _, v := range values {
			req.Header.Add(name, v)
		}
	}
	// A recorded answer is read as the instance sends it: left alone, the
	// transport asks for gzip and hands back a decoded body without its
	// Content-Length, which is not what a fixture holds. The recorded request is
	// the one of the fixture file, so this header is not recorded.
	req.Header.Set("Accept-Encoding", "identity")
	if c.user != "" {
		req.SetBasicAuth(c.user, c.password)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, nil, &transportError{Op: op, Err: err}
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return 0, nil, nil, &transportError{Op: op, Err: err}
	}
	if len(data) > maxResponseBytes {
		return 0, nil, nil, &tooLargeError{Op: op}
	}
	return resp.StatusCode, resp.Header, data, nil
}

// expect sends r and turns a status outside 2xx into an *APIError.
func (c *client) expect(ctx context.Context, r request) (response, error) {
	resp, err := c.do(ctx, r)
	if err != nil {
		return resp, err
	}
	if resp.status < 200 || resp.status > 299 {
		return resp, &APIError{Op: r.op, Status: resp.status, RemoteURLBlocked: resp.remoteURLBlocked, RemoteURLPrivate: resp.remoteURLPrivate}
	}
	return resp, nil
}

// remoteURLRefused reports whether body is a Nexus validation error list with
// an entry on proxy.remoteUrl whose message says the host did not resolve:
// [{"id":"HelperBean.proxy.remoteUrl","message":"... Failed to resolve host: ..."}].
// Only that verdict leaves the function: no text of the answer is kept. The
// content type of the answer is vendor specific, so only the JSON is read; a
// body past drainBytes was cut and is not trusted.
func remoteURLRefused(body []byte) bool {
	return remoteURLMessageHas(body, "resolve host")
}

// remoteURLPrivate reports whether body is a validation error list with an entry
// on proxy.remoteUrl that says the host resolves to a private address, which is
// what the SSRF protection of Nexus 3.98 answers (observed on 3.98.1-01:
// "Proxy URL blocked: Host resolves to private/local IP address(es): ..."). Same
// rules as remoteURLRefused: only the verdict leaves the function.
func remoteURLPrivate(body []byte) bool {
	return remoteURLMessageHas(body, "resolves to private")
}

// remoteURLMessageHas reports whether body is a validation error list with an
// entry on proxy.remoteUrl whose message contains fragment, case-insensitive.
func remoteURLMessageHas(body []byte, fragment string) bool {
	if len(body) > drainBytes {
		return false
	}
	var errs []validationError
	if err := json.Unmarshal(body, &errs); err != nil {
		return false
	}
	return slices.ContainsFunc(errs, func(e validationError) bool {
		return strings.HasSuffix(e.ID, "proxy.remoteUrl") &&
			strings.Contains(strings.ToLower(e.Message), fragment)
	})
}

// validationError is one entry of a Nexus validation error list. The message is
// only matched by remoteURLRefused, never stored or shown.
type validationError struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

// get reads path and decodes the JSON answer into out (nil: not decoded). A 404
// is found == false without an error.
func (c *client) get(ctx context.Context, op, path string, query url.Values, out any) (found bool, err error) {
	resp, err := c.do(ctx, request{op: op, method: http.MethodGet, path: path, query: query})
	if err != nil {
		return false, err
	}
	switch {
	case resp.status == http.StatusNotFound:
		return false, nil
	case resp.status < 200 || resp.status > 299:
		return false, &APIError{Op: op, Status: resp.status}
	}
	if out == nil {
		return true, nil
	}
	if err := json.Unmarshal(resp.body, out); err != nil {
		return false, &decodeError{Op: op, Err: err}
	}
	return true, nil
}

// read is get for an object that has to exist: a 404 is an *APIError.
func (c *client) read(ctx context.Context, op, path string, query url.Values, out any) error {
	found, err := c.get(ctx, op, path, query, out)
	if err != nil {
		return err
	}
	if !found {
		return &APIError{Op: op, Status: http.StatusNotFound}
	}
	return nil
}

// write sends payload as JSON with method and expects a 2xx.
func (c *client) write(ctx context.Context, op, method, path string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return &decodeError{Op: op, Err: err}
	}
	_, err = c.expect(ctx, request{op: op, method: method, path: path, contentType: "application/json", body: data})
	return err
}
