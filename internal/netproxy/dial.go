package netproxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

// maxConnectAnswer caps what is read while waiting for the answer to CONNECT: a
// proxy that never ends its header cannot make the check buffer without bound.
const maxConnectAnswer = 64 << 10

// Dialer opens TLS connections along the path an https request would take. The
// zero value follows the proxy environment variables.
type Dialer struct {
	// Proxy picks the proxy; nil means FromEnvironment.
	Proxy Selector
	// DialContext opens the TCP connection (to the proxy, or to the target when
	// direct); nil means (&net.Dialer{}).DialContext.
	DialContext func(ctx context.Context, network, addr string) (net.Conn, error)
	// ProxyTLSConfig configures TLS to an https:// proxy; nil means system roots.
	ProxyTLSConfig *tls.Config
}

// DialTLS connects to hostport (the port is required) the way an https request
// to it would travel, then completes the handshake with cfg (ServerName
// defaults to the host). ctx bounds the whole: proxy dial, CONNECT, handshake.
//
// A failure of the proxy step is a *Error; a failure of the target itself (its
// handshake, its certificate) is returned as it would be without a proxy. The
// returned connection owns the tunnel: closing it closes the socket.
func (d Dialer) DialTLS(ctx context.Context, hostport string, cfg *tls.Config) (*tls.Conn, error) {
	if err := checkTarget(hostport); err != nil {
		return nil, err
	}
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		return nil, err
	}
	sel := d.Proxy
	if sel == nil {
		sel = FromEnvironment
	}
	proxy, err := sel(&url.URL{Scheme: "https", Host: hostport})
	if err != nil {
		return nil, &Error{Step: StepSelect, Err: err}
	}
	if proxy != nil && proxy.Scheme != "http" && proxy.Scheme != "https" {
		return nil, &Error{Proxy: proxy.Host, Step: StepScheme, Err: errors.New(proxy.Scheme)}
	}

	conn, err := d.tunnel(ctx, proxy, hostport)
	if err != nil {
		return nil, err
	}

	tc := tls.Client(conn, targetConfig(cfg, host))
	if err := tc.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return tc, nil
}

// checkTarget refuses a hostport holding a byte at or below space, or DEL: the
// target is written into the CONNECT line and its Host header, where a CR or LF
// would end the line and inject a header and a space would split the authority.
// It runs before anything else, so nothing is dialed and no selector is asked.
// The error quotes the value (%q), never raw.
func checkTarget(hostport string) error {
	for i := 0; i < len(hostport); i++ {
		if c := hostport[i]; c <= 0x20 || c == 0x7f {
			return fmt.Errorf("netproxy: invalid target %q: control character or space at offset %d", hostport, i)
		}
	}
	return nil
}

// tunnel returns a connection to hostport: direct when proxy is nil, otherwise
// through proxy by CONNECT. One deadline, ctx's, covers every step; on failure
// the connection is closed. The deadline is left set so the caller's handshake
// stays under it.
func (d Dialer) tunnel(ctx context.Context, proxy *url.URL, hostport string) (net.Conn, error) {
	dial := d.DialContext
	if dial == nil {
		dial = (&net.Dialer{}).DialContext
	}
	if proxy == nil {
		conn, err := dial(ctx, "tcp", hostport)
		if err != nil {
			return nil, err
		}
		if dl, ok := ctx.Deadline(); ok {
			_ = conn.SetDeadline(dl)
		}
		return conn, nil
	}

	addr := proxyAddr(proxy)
	raw, err := dial(ctx, "tcp", addr)
	if err != nil {
		return nil, &Error{Proxy: addr, Step: StepDial, Err: err}
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = raw.SetDeadline(dl)
	}
	// A cancellation unblocks the reads and writes below by closing the socket.
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	conn, perr := d.connect(ctx, raw, proxy, addr, hostport)
	if perr != nil {
		stop()
		return nil, perr
	}
	if !stop() {
		// The cancellation fired after the CONNECT succeeded: the socket is closed.
		_ = conn.Close()
		return nil, &Error{Proxy: addr, Step: StepConnect, Err: ctx.Err()}
	}
	return conn, nil
}

// connect runs TLS to the proxy when it is https://, then the CONNECT exchange,
// on conn. It closes conn on failure.
func (d Dialer) connect(ctx context.Context, conn net.Conn, proxy *url.URL, addr, hostport string) (net.Conn, *Error) {
	fail := func(step Step, status int, err error) (net.Conn, *Error) {
		_ = conn.Close()
		if errors.Is(err, net.ErrClosed) && ctx.Err() != nil {
			err = ctx.Err() // our own AfterFunc closed it: the cause is the context's
		}
		return nil, &Error{Proxy: addr, Step: step, StatusCode: status, Err: err}
	}

	if proxy.Scheme == "https" {
		cfg := &tls.Config{}
		if d.ProxyTLSConfig != nil {
			cfg = d.ProxyTLSConfig.Clone()
		}
		if cfg.ServerName == "" {
			cfg.ServerName = proxy.Hostname()
		}
		tc := tls.Client(conn, cfg)
		if err := tc.HandshakeContext(ctx); err != nil {
			return fail(StepProxyTLS, 0, err)
		}
		conn = tc
	}

	if _, err := io.WriteString(conn, connectRequest(proxy.User, hostport)); err != nil {
		return fail(StepConnect, 0, err)
	}
	br := bufio.NewReader(io.LimitReader(conn, maxConnectAnswer))
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		return fail(StepConnect, 0, err)
	}
	// The body of a CONNECT answer is the tunnel; it is not read or closed here.
	if resp.StatusCode != http.StatusOK {
		return fail(StepConnect, resp.StatusCode, nil)
	}
	if n := br.Buffered(); n > 0 {
		// Bytes read past the header belong to the tunnel; give them back first.
		peeked, _ := br.Peek(n)
		return &bufferedConn{Conn: conn, r: io.MultiReader(bytes.NewReader(bytes.Clone(peeked)), conn)}, nil
	}
	return conn, nil
}

// bufferedConn is a net.Conn whose reads start with bytes already taken off it.
type bufferedConn struct {
	net.Conn
	r io.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.r.Read(p) }

// connectRequest is the CONNECT request for hostport, with Basic credentials
// from user when the proxy URL carries any.
func connectRequest(user *url.Userinfo, hostport string) string {
	req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n", hostport, hostport)
	if user != nil {
		password, _ := user.Password()
		cred := base64.StdEncoding.EncodeToString([]byte(user.Username() + ":" + password))
		req += "Proxy-Authorization: Basic " + cred + "\r\n"
	}
	return req + "\r\n"
}

// proxyAddr is the proxy's host:port, without userinfo, with the scheme's
// default port when the URL has none.
func proxyAddr(proxy *url.URL) string {
	if proxy.Port() != "" {
		return proxy.Host
	}
	port := "80"
	if proxy.Scheme == "https" {
		port = "443"
	}
	return net.JoinHostPort(proxy.Hostname(), port)
}

// targetConfig is a copy of cfg (or a fresh config) that verifies host unless
// cfg names another server.
func targetConfig(cfg *tls.Config, host string) *tls.Config {
	c := &tls.Config{}
	if cfg != nil {
		c = cfg.Clone()
	}
	if c.ServerName == "" {
		c.ServerName = host
	}
	return c
}
