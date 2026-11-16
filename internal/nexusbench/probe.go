package nexusbench

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"
)

// probeTimeout bounds one probe, connection included: the wait asks again at
// its next round, so a short one is enough and a silent host costs little.
const probeTimeout = 5 * time.Second

// probeOp names the probe in its errors.
const probeOp = "probe the Nexus status"

// Prober tells whether a Nexus answers on baseURL ("http://host:port").
type Prober interface {
	// Ready returns nil when GET <baseURL>/service/rest/v1/status answers 200,
	// and an error otherwise, whatever the reason.
	Ready(ctx context.Context, baseURL string) error
}

// httpProber is the Prober of the command.
type httpProber struct {
	http *http.Client
}

// NewProber returns the Prober that asks the instance over HTTP. It sends one
// unauthenticated GET, follows no redirection, goes through no proxy, and never
// reads or reports the body of the answer.
func NewProber() Prober {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	// One probe per round: keeping the connection alive would only hold a socket
	// open on a pod that may be restarting.
	transport.DisableKeepAlives = true
	return &httpProber{http: &http.Client{
		Timeout:       probeTimeout,
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// Ready implements Prober. The error is a *transportError (no answer) or an
// *APIError (an answer other than 200); neither prints the URL or the body.
func (pr *httpProber) Ready(ctx context.Context, baseURL string) error {
	target := strings.TrimRight(baseURL, "/") + restStatusPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return &transportError{Op: probeOp, Err: err}
	}
	resp, err := pr.http.Do(req)
	if err != nil {
		return &transportError{Op: probeOp, Err: err}
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, drainBytes))
	if resp.StatusCode != http.StatusOK {
		return &APIError{Op: probeOp, Status: resp.StatusCode}
	}
	return nil
}
