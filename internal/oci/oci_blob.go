package oci

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// This file holds what the layer blob download needs on top of Client: the
// redirect policy, the SSRF guard on the dialed address, the proxy allowance,
// the error rewriting and the byte cap. See DEVDESK-412.

// errBlobRedirectLocal is the refusal of a blob redirect to a loopback or
// link-local address from a registry that is not itself local, so a registry
// cannot make DevDesk query a service on this machine or the cloud metadata
// endpoint (SSRF). It names no URL: the target may be signed.
var errBlobRedirectLocal = errors.New("refusing redirect to a loopback or link-local address")

// errBlobDialLocal is the refusal of dialControl to connect to such an address,
// whatever name led there (DNS rebinding included).
var errBlobDialLocal = errors.New("refusing to connect to a loopback or link-local address")

// isLocalHost reports whether host (no port) is local: localhost, any name under
// .localhost (compared without case, with or without the final dot), or a literal
// loopback, link-local or unspecified address, IPv4-mapped forms included. The
// unspecified address counts because Linux connects it to this machine. Any
// other name is not local here, whatever /etc/hosts or the DNS say: only the
// dial control sees what it resolves to.
func isLocalHost(host string) bool {
	name := strings.ToLower(strings.TrimSuffix(host, "."))
	if name == "localhost" || strings.HasSuffix(name, ".localhost") {
		return true
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && isLocalAddr(addr)
}

func isLocalAddr(addr netip.Addr) bool {
	addr = addr.Unmap().WithZone("")
	return addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsUnspecified()
}

// isLocalURL reports whether the host of rawURL is local (see isLocalHost). A URL
// that does not parse is not local.
func isLocalURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	return err == nil && isLocalHost(u.Hostname())
}

// blobDialControl is a net.Dialer Control that refuses to connect to a loopback
// or link-local address unless allowLocal. It runs on the address about to be
// dialed, so a name that resolves to such an address (DNS rebinding) is caught
// where followBlobRedirect, which only sees names, cannot. An address that
// cannot be read is refused.
func blobDialControl(allowLocal bool) func(network, address string, c syscall.RawConn) error {
	return dialControl(allowLocal, nil)
}

// dialControl is blobDialControl that also lets through the address of a proxy
// the transport was told to use (see proxyAddrs): a local proxy such as
// cntlm on 127.0.0.1 is the user's own configuration, not a redirect target.
func dialControl(allowLocal bool, proxies *proxyAddrs) func(network, address string, c syscall.RawConn) error {
	return func(_, address string, _ syscall.RawConn) error {
		if allowLocal {
			return nil
		}
		ap, err := netip.ParseAddrPort(address)
		if err != nil {
			return errBlobDialLocal
		}
		ap = netip.AddrPortFrom(ap.Addr().Unmap().WithZone(""), ap.Port())
		if isLocalAddr(ap.Addr()) && !proxies.has(ap) {
			return errBlobDialLocal
		}
		return nil
	}
}

// proxyAddrs holds the ip:port of the proxies the blob transport was asked to
// use, filled by the transport's Proxy function just before it dials. A proxy
// that NO_PROXY excludes for a request is therefore never allowed on its
// account. The target of a proxied request is resolved by the proxy, so the
// literal check of followBlobRedirect stays the guard for it.
type proxyAddrs struct {
	resolveMu sync.Mutex // serializes lookups
	resolve   func(host string) ([]netip.Addr, error)

	mu    sync.Mutex // guards seen and addrs
	seen  map[string]bool
	addrs map[netip.AddrPort]bool
}

func newProxyAddrs() *proxyAddrs {
	return &proxyAddrs{resolve: resolveProxyHost, seen: map[string]bool{}, addrs: map[netip.AddrPort]bool{}}
}

func (p *proxyAddrs) has(ap netip.AddrPort) bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.addrs[ap]
}

// add records the addresses proxy resolves to, looking each proxy up until it
// resolves once. A proxy that does not resolve adds nothing, so a local address
// stays refused.
func (p *proxyAddrs) add(proxy *url.URL) {
	port := proxyPort(proxy)
	key := net.JoinHostPort(proxy.Hostname(), strconv.Itoa(int(port)))
	// One lookup at a time, with the result recorded before the next request
	// looks: a concurrent request waits instead of seeing a proxy that is marked
	// known but has no address yet. has takes mu only, so a dial never waits for
	// a lookup.
	p.resolveMu.Lock()
	defer p.resolveMu.Unlock()
	p.mu.Lock()
	done := p.seen[key]
	p.mu.Unlock()
	if done {
		return
	}
	resolved, err := p.resolve(proxy.Hostname())
	if err != nil {
		return // not remembered: the next request looks again
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, addr := range resolved {
		p.addrs[netip.AddrPortFrom(addr.Unmap().WithZone(""), port)] = true
	}
	p.seen[key] = true
}

// proxyPort is the port net/http dials for proxy: the one in its URL, else the
// default of its scheme.
func proxyPort(proxy *url.URL) uint16 {
	if n, err := strconv.ParseUint(proxy.Port(), 10, 16); err == nil {
		return uint16(n)
	}
	switch proxy.Scheme {
	case "https":
		return 443
	case "socks5", "socks5h":
		return 1080
	}
	return 80
}

// resolveProxyHost gives the addresses a proxy host stands for: a literal
// address as is, localhost and the names under it as the two loopback addresses,
// any other name through the resolver, bounded to 5 seconds.
func resolveProxyHost(host string) ([]netip.Addr, error) {
	if addr, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{addr}, nil
	}
	if isLocalHost(host) { // a name under .localhost (an address was handled above)
		return []netip.Addr{netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("::1")}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

// blobTransport is http.DefaultTransport (proxy settings included) with
// blobDialControl on its dialer.
func blobTransport(allowLocal bool) *http.Transport {
	return newBlobTransport(allowLocal, http.ProxyFromEnvironment)
}

// newBlobTransport is blobTransport with the proxy selection given, which lets a
// test choose the proxy: http.ProxyFromEnvironment reads the environment once
// per process.
func newBlobTransport(allowLocal bool, proxyFor func(*http.Request) (*url.URL, error)) *http.Transport {
	proxies := newProxyAddrs()
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = func(req *http.Request) (*url.URL, error) {
		proxy, err := proxyFor(req)
		if proxy != nil {
			proxies.add(proxy)
		}
		return proxy, err
	}
	tr.DialContext = (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   dialControl(allowLocal, proxies),
	}).DialContext
	return tr
}

// errBlobRedirectScheme is the refusal of a blob redirect to anything but
// http or https. It names no URL: the target may be signed.
var errBlobRedirectScheme = errors.New("refusing redirect to a scheme other than http or https")

// followBlobRedirect is the CheckRedirect of Client.blobClient.
//
// Deliberate exception to RefuseOffOriginRedirect (DEVDESK-412, user decision
// Q1b): a layer blob may be served from another origin, the usual way a registry
// hands it to a CDN or an object store. The redirect is followed, but the
// credentials never go with it:
//   - at most MaxRedirects hops, only to http or https;
//   - never from https to http (compared with the previous hop), so a signed URL
//     does not leave in clear;
//   - when the registry is not local, never to localhost or a literal loopback
//     or link-local address (SSRF; blobDialControl covers names that resolve
//     there);
//   - once the chain left the origin of the first request (scheme, host and
//     port), Authorization is deleted from every following request, including
//     one that comes back to the origin. net/http copies the header to the same
//     host name on another port, across a scheme change and to a subdomain, and
//     it copies it before this function runs, so deleting it here is what counts.
//     The Referer net/http sets (the previous hop's full, possibly signed, URL)
//     goes the same way.
func followBlobRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= MaxRedirects {
		return ErrTooManyRedirects
	}
	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		return errBlobRedirectScheme
	}
	first := via[0].URL
	if via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
		return fmt.Errorf("%w (%s)", errInsecureRedirect, req.URL.Host)
	}
	if !isLocalHost(first.Hostname()) && isLocalHost(req.URL.Hostname()) {
		return fmt.Errorf("%w (%s)", errBlobRedirectLocal, req.URL.Host)
	}
	left := !sameOrigin(first.String(), req.URL.String())
	for _, prev := range via[1:] {
		left = left || !sameOrigin(first.String(), prev.URL.String())
	}
	if left {
		req.Header.Del("Authorization")
		req.Header.Del("Referer")
	}
	return nil
}

// blobFetchError rewrites a *url.Error of the blob download so that it carries
// the host of the failing hop at most: its URL may hold a signed query or a
// private path. The cause stays reachable through errors.Is and errors.As.
func blobFetchError(err error) error {
	var ue *url.Error
	if !errors.As(err, &ue) {
		return err
	}
	if u, perr := url.Parse(ue.URL); perr == nil && u.Host != "" {
		return fmt.Errorf("download layer from %s: %w", u.Host, ue.Err)
	}
	return fmt.Errorf("download layer: %w", ue.Err)
}

// blobCap bounds the compressed bytes taken off the network for one layer: the
// content limit, a tar header and padding per file, and 1 MiB of gzip framing.
// The decompressed Limits do not see everything the network carries: PAX and GNU
// extension headers and their padding are not file content (the standard library
// caps each at 1 MiB), so this is the only bound on those bytes. 0 means no cap (Limits{} or no byte limit).
func blobCap(lim Limits) int64 {
	if lim.Bytes <= 0 {
		return 0
	}
	return lim.Bytes + int64(lim.Files)*2048 + 1<<20
}

// cappedReader reads from r, accepting at most left bytes: one more byte
// available makes it fail with ErrArchiveTooLarge instead of reading it.
type cappedReader struct {
	r    io.Reader
	left int64
}

func (c *cappedReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if c.left <= 0 {
		var probe [1]byte
		n, err := c.r.Read(probe[:])
		if n > 0 {
			return 0, ErrArchiveTooLarge
		}
		return 0, err
	}
	if int64(len(p)) > c.left {
		p = p[:c.left]
	}
	n, err := c.r.Read(p)
	c.left -= int64(n)
	return n, err
}

// ErrBlobDigestMismatch is returned when the layer received is not the one the
// manifest names. No entry is returned with it.
var ErrBlobDigestMismatch = errors.New("layer content does not match its digest")

// errBlobDigestForm is the refusal of a layer digest that is not sha256 and 64
// lowercase hex digits. It does not repeat the digest: it is what the registry
// wrote and may be long or hold control characters.
var errBlobDigestForm = errors.New("unsupported layer digest: only sha256 is supported")

// parseLayerDigest returns the hex part of digest when it is "sha256:" followed
// by exactly 64 lowercase hex digits, and errBlobDigestForm otherwise. The
// digest goes into the blob URL, so nothing else (a path, a query, another
// algorithm) is accepted.
func parseLayerDigest(digest string) (hexPart string, err error) {
	hexPart, ok := strings.CutPrefix(digest, "sha256:")
	if !ok || len(hexPart) != sha256.Size*2 {
		return "", errBlobDigestForm
	}
	for i := 0; i < len(hexPart); i++ {
		if c := hexPart[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return "", errBlobDigestForm
		}
	}
	return hexPart, nil
}

// checkBlobDigest drains what the archive reader left unread of body, so the
// bytes after the gzip stream are part of the digest too, then compares the
// hash of everything received with wantHex. The drain is bounded by the reader
// it is given (blobCap, or the client timeout when there is no cap); a blob over
// the cap comes back as ErrArchiveTooLarge, not as a mismatch.
func checkBlobDigest(body io.Reader, sum hash.Hash, wantHex string) error {
	if _, err := io.Copy(io.Discard, body); err != nil {
		return err
	}
	if got := hex.EncodeToString(sum.Sum(nil)); got != wantHex {
		return fmt.Errorf("%w (expected sha256:%.12s...)", ErrBlobDigestMismatch, wantHex)
	}
	return nil
}
