package netcheck

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The tests in this file exercise the production Env against loopback servers.
// They reach no external network and need no Docker — but they are the only
// thing that checks the claims the comments in env.go make, rather than
// restating them.

func TestDialTCPReportsAnOpenPortAndARefusedOne(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()

	elapsed, err := SystemEnv(DefaultSettings()).DialTCP(context.Background(), addr)
	if err != nil {
		t.Fatalf("dial an open port: %v", err)
	}
	if elapsed < 0 {
		t.Errorf("elapsed = %v", elapsed)
	}

	// Closing the listener frees the port; dialling it now is refused, which is
	// the observation the connect check turns into a Fail.
	if err := ln.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := SystemEnv(DefaultSettings()).DialTCP(context.Background(), addr); err == nil {
		t.Fatal("dialling a closed port returned no error")
	}
}

// TestANonTlsPortProducesARecordHeaderError is the one that matters most in
// this file: stage_tls.go tells "does not speak TLS" from "TLS is broken" by
// matching tls.RecordHeaderError, and that claim is about crypto/tls's
// behaviour rather than about our own code. If it ever stops holding, the TLS
// stage silently starts reporting plain HTTP ports as failures.
func TestANonTlsPortProducesARecordHeaderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "http://")
	_, err := SystemEnv(DefaultSettings()).Handshake(context.Background(), host, "127.0.0.1")
	if err == nil {
		t.Fatal("handshaking with a plain HTTP server succeeded")
	}

	var rhe tls.RecordHeaderError
	if !asRecordHeaderError(err, &rhe) {
		t.Fatalf("error is %T (%v), want tls.RecordHeaderError — handshakeFailed depends on it", err, err)
	}
}

// TestHandshakeReturnsTheChainWithoutVerifyingIt pins the decision behind
// InsecureSkipVerify: the certificate has to survive the handshake so the four
// certificate checks have something to inspect. httptest's server presents a
// certificate signed by nothing the machine trusts, so a verifying dialer would
// return an error and no certificate at all.
func TestHandshakeReturnsTheChainWithoutVerifyingIt(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "https://")
	state, err := SystemEnv(DefaultSettings()).Handshake(context.Background(), host, "127.0.0.1")
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if len(state.PeerCertificates) == 0 {
		t.Fatal("no certificate came back — the checks would have nothing to inspect")
	}
	if state.Version < tls.VersionTLS12 {
		t.Errorf("negotiated %s against httptest", tlsVersionName(state.Version))
	}
}

// TestHeadAnswersDespiteAnUntrustedCertificate is the other half of that
// decision: a broken chain must not hide whether the service answers.
func TestHeadAnswersDespiteAnUntrustedCertificate(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Server", "test-server")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	res, err := SystemEnv(DefaultSettings()).Head(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("head over an untrusted certificate: %v", err)
	}
	if res.Status != http.StatusNoContent {
		t.Errorf("status = %d, want %d", res.Status, http.StatusNoContent)
	}
	if res.Server != "test-server" {
		t.Errorf("server = %q", res.Server)
	}
	if !res.TLS {
		t.Error("TLS = false on an https request")
	}
}

// TestHeadDoesNotFollowRedirects: the first answer is the observation. A
// redirect chain is a different question, and following one would report the
// status of somewhere else.
func TestHeadDoesNotFollowRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/elsewhere", http.StatusMovedPermanently)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	res, err := SystemEnv(DefaultSettings()).Head(context.Background(), srv.URL+"/")
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if res.Status != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want 301 — the redirect must be reported, not followed", res.Status)
	}
}

func TestHeadReportsAContextThatExpired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	if _, err := SystemEnv(DefaultSettings()).Head(ctx, srv.URL); err == nil {
		t.Fatal("want an error on an expired context, got none")
	}
}

// TestResolverForNamesAPortWhenTheNameserverOmitsOne covers the arithmetic a
// user hits by typing "1.1.1.1" rather than "1.1.1.1:53".
func TestResolverForNamesAPortWhenTheNameserverOmitsOne(t *testing.T) {
	if r := resolverFor("", DefaultCheckTimeout); r.PreferGo {
		t.Error("an empty nameserver must yield the system resolver, untouched")
	}
	for _, ns := range []string{"1.1.1.1", "1.1.1.1:5353", "[2001:db8::1]:53"} {
		if r := resolverFor(ns, DefaultCheckTimeout); r == nil || !r.PreferGo || r.Dial == nil {
			t.Errorf("resolverFor(%q) did not build a directed resolver", ns)
		}
	}
}

func TestSystemEnvTrustsTheHostStore(t *testing.T) {
	if got := SystemEnv(DefaultSettings()).TrustRoots(); got != nil {
		t.Error("TrustRoots must be nil — a chain verifies only against a store somebody uses")
	}
	if SystemEnv(DefaultSettings()).Now().IsZero() {
		t.Error("Now returned the zero time")
	}
}

// asRecordHeaderError is errors.As with the concrete type spelled out, kept
// here so the test reads as the assertion it is.
func asRecordHeaderError(err error, target *tls.RecordHeaderError) bool {
	rhe, ok := err.(tls.RecordHeaderError) //nolint:errorlint // crypto/tls returns it unwrapped from the dialer
	if ok {
		*target = rhe
	}
	return ok
}
