package nexusbench

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, drainBytes))
		return response{status: resp.StatusCode, header: resp.Header}, nil
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

// expect sends r and turns a status outside 2xx into an *APIError.
func (c *client) expect(ctx context.Context, r request) (response, error) {
	resp, err := c.do(ctx, r)
	if err != nil {
		return resp, err
	}
	if resp.status < 200 || resp.status > 299 {
		return resp, &APIError{Op: r.op, Status: resp.status}
	}
	return resp, nil
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
