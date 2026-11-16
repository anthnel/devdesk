package oci

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// DEVDESK-412 step 4, hardening 4: SSRF. A blob redirect to a literal loopback
// or link-local host, or to localhost, is refused unless the registry is itself
// local (errBlobRedirectLocal, host only in the message), and the transport's
// dial control refuses such an address even when a name resolves to it (DNS
// rebinding). Fixtures come from oci_blob_redirect_test.go and
// oci_blob_hardening_test.go.

// 4a. SSRF, redirect check: when the registry is not local, the blob may not be
// redirected to a literal loopback or link-local host, or to localhost. When the
// registry is itself local, it may.
func TestFollowBlobRedirectLocalTargets(t *testing.T) {
	const remote = "https://registry.example.com/v2/tpl/blobs/sha256:abc"
	const remoteHTTP = "http://registry.example.com/v2/tpl/blobs/sha256:abc"

	refused := []string{
		"https://127.0.0.1/p/SECRET?sig=SECRET",
		"https://127.1.2.3:9000/p/SECRET?sig=SECRET",
		"https://[::1]/p/SECRET?sig=SECRET",
		"https://[::1]:9000/p/SECRET?sig=SECRET",
		"https://169.254.169.254/latest/meta-data/SECRET?sig=SECRET",
		"https://169.254.0.1:8080/p/SECRET?sig=SECRET",
		"https://[fe80::1]/p/SECRET?sig=SECRET",
		"https://[febf::1]:9000/p/SECRET?sig=SECRET",
		"https://localhost/p/SECRET?sig=SECRET",
		"https://LOCALHOST:8080/p/SECRET?sig=SECRET",
		"https://[::ffff:127.0.0.1]/p/SECRET?sig=SECRET", // IPv4-mapped loopback
		// The unspecified address reaches the service on 127.0.0.1 on Linux.
		"https://0.0.0.0/p/SECRET?sig=SECRET",
		"https://0.0.0.0:9000/p/SECRET?sig=SECRET",
		"https://[::]/p/SECRET?sig=SECRET",
		"https://[::]:9000/p/SECRET?sig=SECRET",
		"https://[::ffff:0.0.0.0]/p/SECRET?sig=SECRET",
		// Names the resolver maps to loopback by themselves.
		"https://localhost./p/SECRET?sig=SECRET",
		"https://LocalHost.:8080/p/SECRET?sig=SECRET",
		"https://foo.localhost/p/SECRET?sig=SECRET",
		"https://FOO.Localhost.:8080/p/SECRET?sig=SECRET",
		"https://k3d-registry.localhost:5000/p/SECRET?sig=SECRET",
	}
	for _, to := range refused {
		for _, from := range []string{remote, remoteHTTP} {
			t.Run("refused/"+to+"/from "+from[:strings.Index(from, ":")], func(t *testing.T) {
				req := mustReq(t, to)
				if strings.HasPrefix(from, "http:") {
					// Keep the scheme rule out of the way: the target is
					// plain http in the redirect from an http registry.
					req = mustReq(t, "http"+strings.TrimPrefix(to, "https"))
				}
				err := followBlobRedirect(req, []*http.Request{mustReq(t, from)})
				if !errors.Is(err, errBlobRedirectLocal) {
					t.Fatalf("followBlobRedirect = %v, want errors.Is errBlobRedirectLocal", err)
				}
				if strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "/p/") ||
					strings.Contains(err.Error(), "/latest") {
					t.Errorf("error %q carries the target's path or query", err)
				}
				if !strings.Contains(err.Error(), req.URL.Hostname()) {
					t.Errorf("error %q does not name the host %q", err, req.URL.Hostname())
				}
			})
		}
	}

	allowed := []struct{ name, from, to string }{
		{"public name", remote, "https://cdn.example.net/p"},
		{"public IPv4", remote, "https://203.0.113.5/p"},
		{"public IPv6", remote, "https://[2001:db8::1]/p"},
		{"private network is not refused", remote, "https://10.0.0.5/p"},
		{"name containing localhost", remote, "https://notlocalhost.example.net/p"},
		{"registry on 127.0.0.1", "http://127.0.0.1:5000/v2/x", "http://127.0.0.1:9000/p"},
		{"registry on 127.0.0.1, target ::1", "http://127.0.0.1:5000/v2/x", "http://[::1]:9000/p"},
		{"registry on ::1", "http://[::1]:5000/v2/x", "http://127.0.0.1:9000/p"},
		{"registry on localhost", "https://localhost:5000/v2/x", "https://127.0.0.1:9000/p"},
		{"registry on link-local", "http://169.254.1.1:5000/v2/x", "http://169.254.169.254/p"},
		{"registry on 0.0.0.0", "http://0.0.0.0:5000/v2/x", "http://127.0.0.1:9000/p"},
		{"registry on ::", "http://[::]:5000/v2/x", "http://127.0.0.1:9000/p"},
		{"registry on localhost.", "http://localhost.:5000/v2/x", "http://127.0.0.1:9000/p"},
		{"registry on a .localhost name", "http://k3d-registry.localhost:5000/v2/x", "http://127.0.0.1:9000/p"},
		{"name ending in localhost without the dot", "https://registry.example.com/v2/x", "https://mylocalhost/p"},
		{"name containing .localhost.example", "https://registry.example.com/v2/x", "https://a.localhost.example/p"},
	}
	for _, tc := range allowed {
		t.Run("allowed/"+tc.name, func(t *testing.T) {
			if err := followBlobRedirect(mustReq(t, tc.to), []*http.Request{mustReq(t, tc.from)}); err != nil {
				t.Fatalf("followBlobRedirect = %v, want nil", err)
			}
		})
	}
}

// 4b. SSRF, end to end: a registry on a public name redirects the blob to a
// literal loopback address; the redirect is refused and the target, a live
// server, is never asked.
func TestBlobRedirectToLocalTargetIsRefusedEndToEnd(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "localhost"} {
		t.Run(host, func(t *testing.T) {
			var regHits, targetHits hitLog
			target := archiveHost(t, false, &targetHits, clientTarGz(t))
			a := startServer(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				regHits.record(r)
				if isManifestRequest(r) {
					_, _ = fmt.Fprint(w, blobManifest())
					return
				}
				http.Redirect(w, r, "http://"+host+":"+portOfServer(target)+blobPrivatePath+"?"+blobSignedQuery, http.StatusTemporaryRedirect)
			}))

			c := withTransport(NewClient("http://registry.test:"+portOfServer(a), clientTestUser, clientTestPass), dialRouted(a.Listener.Addr().String()))
			tpl, err := c.DownloadTemplate("tpl", "v1")

			if !errors.Is(err, errBlobRedirectLocal) {
				t.Fatalf("err = %v, want errors.Is errBlobRedirectLocal", err)
			}
			if tpl != nil {
				t.Errorf("template = %+v, want nil", tpl)
			}
			if n := targetHits.count(); n != 0 {
				t.Errorf("the local target received %d requests, want 0", n)
			}
			wantNoLeak(t, err, blobPrivatePath)
		})
	}
}

// 4c. SSRF, DNS rebinding: the dial control of the blob transport refuses a
// loopback or link-local address unless the registry is local. It runs on the
// address actually dialed, so a name that resolves to such an address is caught
// even though the redirect check only sees names.
func TestBlobDialControl(t *testing.T) {
	local := []string{
		"127.0.0.1:80",
		"127.255.255.254:443",
		"[::1]:443",
		"[::ffff:127.0.0.1]:443",
		"169.254.169.254:80",
		"169.254.0.1:443",
		"[fe80::1]:80",
		"[febf::1]:80",
		"0.0.0.0:80",
		"[::]:443",
		"[::ffff:0.0.0.0]:443",
	}
	remote := []string{
		"93.184.216.34:443",
		"203.0.113.5:80",
		"10.0.0.5:443",
		"192.168.1.10:443",
		"[2001:db8::1]:443",
	}

	for _, addr := range local {
		err := blobDialControl(false)("tcp", addr, nil)
		if !errors.Is(err, errBlobDialLocal) {
			t.Errorf("blobDialControl(false)(%q) = %v, want errBlobDialLocal", addr, err)
		}
		if errors.Is(err, errBlobRedirectLocal) {
			t.Errorf("blobDialControl(false)(%q) = the redirect refusal, want the dial one", addr)
		}
		if err := blobDialControl(true)("tcp", addr, nil); err != nil {
			t.Errorf("blobDialControl(true)(%q) = %v, want nil (local registry)", addr, err)
		}
	}
	for _, addr := range remote {
		for _, allowLocal := range []bool{false, true} {
			if err := blobDialControl(allowLocal)("tcp", addr, nil); err != nil {
				t.Errorf("blobDialControl(%v)(%q) = %v, want nil", allowLocal, addr, err)
			}
		}
	}
	// What cannot be read as an address fails closed.
	if err := blobDialControl(false)("tcp", "not-an-address", nil); !errors.Is(err, errBlobDialLocal) {
		t.Errorf("blobDialControl(false)(\"not-an-address\") = %v, want errBlobDialLocal", err)
	}
}

// 4e. What counts as local, by name and by literal address, for both the redirect
// check and the registry that gets to use local targets (isLocalURL).
func TestIsLocalHost(t *testing.T) {
	local := []string{
		"localhost", "LOCALHOST", "localhost.", "LocalHost.",
		"foo.localhost", "FOO.LOCALHOST", "foo.bar.localhost.", "k3d-registry.localhost",
		"127.0.0.1", "127.9.9.9", "::1", "::ffff:127.0.0.1",
		"169.254.169.254", "fe80::1", "fe80::1%eth0",
		"0.0.0.0", "::", "::ffff:0.0.0.0",
	}
	for _, h := range local {
		if !isLocalHost(h) {
			t.Errorf("isLocalHost(%q) = false, want true", h)
		}
	}
	notLocal := []string{
		"", "example.com", "mylocalhost", "localhost.example.com", "a.localhost.example",
		"localhostx", "10.0.0.5", "192.168.1.1", "172.16.0.1", "93.184.216.34", "2001:db8::1",
	}
	for _, h := range notLocal {
		if isLocalHost(h) {
			t.Errorf("isLocalHost(%q) = true, want false", h)
		}
	}
}

func TestIsLocalURL(t *testing.T) {
	for raw, want := range map[string]bool{
		"http://k3d-registry.localhost:5000": true,
		"https://registry.localhost":         true,
		"http://localhost.:5000":             true,
		"http://0.0.0.0:5000":                true,
		"http://[::]:5000":                   true,
		"http://127.0.0.1:5000":              true,
		"https://registry.example.com":       false,
		"https://mylocalhost":                false,
		"":                                   false,
		"://bad":                             false,
	} {
		if got := isLocalURL(raw); got != want {
			t.Errorf("isLocalURL(%q) = %v, want %v", raw, got, want)
		}
	}
}
