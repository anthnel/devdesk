package nexusbench

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/anthnel/devdesk/internal/testutil/loopback"
)

// fakeRegistry is the smallest OCI distribution registry that skopeo can push
// to and pull from: blobs by monolithic or chunked upload, manifests by tag and
// by digest, and an optional Basic challenge. It lives on the loopback of the
// test process and speaks plain HTTP, which is what the HTTP fallback of a
// docker reference with TLS verification off meets on the bench.
type fakeRegistry struct {
	srv        *httptest.Server
	user, pass string // empty: the registry is open

	mu        sync.Mutex
	blobs     map[string][]byte
	uploads   map[string][]byte
	manifests map[string]fakeManifest // "<name>:<tag or digest>"
	served    int                     // requests that passed the challenge
	refused   int                     // requests refused by the challenge
}

type fakeManifest struct {
	contentType string
	body        []byte
}

// newFakeRegistry starts a registry; user and pass, when set, are demanded with
// HTTP Basic on every request.
func newFakeRegistry(t *testing.T, user, pass string) *fakeRegistry {
	t.Helper()
	r := &fakeRegistry{
		user: user, pass: pass,
		blobs:     map[string][]byte{},
		uploads:   map[string][]byte{},
		manifests: map[string]fakeManifest{},
	}
	r.srv = httptest.NewServer(r)
	t.Cleanup(r.srv.Close)
	return r
}

// host is "127.0.0.1:<port>", the host part of a docker reference.
func (r *fakeRegistry) host() string { return r.srv.Listener.Addr().String() }

func (r *fakeRegistry) manifest(name, ref string) (fakeManifest, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.manifests[name+":"+ref]
	return m, ok
}

// knows tells whether the repository holds any manifest.
func (r *fakeRegistry) knows(name string) bool {
	for key := range r.manifests {
		if strings.HasPrefix(key, name+":") {
			return true
		}
	}
	return false
}

func (r *fakeRegistry) counts() (served, refused int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.served, r.refused
}

func digestOfBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ServeHTTP implements http.Handler.
func (r *fakeRegistry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.user != "" {
		if u, p, ok := req.BasicAuth(); !ok || u != r.user || p != r.pass {
			r.refused++
			w.Header().Set("WWW-Authenticate", `Basic realm="fake registry"`)
			http.Error(w, `{"errors":[{"code":"UNAUTHORIZED","message":"authentication required"}]}`, http.StatusUnauthorized)
			return
		}
	}
	r.served++

	rest, ok := strings.CutPrefix(req.URL.Path, "/v2/")
	if !ok {
		http.NotFound(w, req)
		return
	}
	switch {
	case rest == "":
		w.WriteHeader(http.StatusOK)
	case strings.Contains(rest, "/blobs/uploads/"):
		name, id, _ := strings.Cut(rest, "/blobs/uploads/")
		r.serveUpload(w, req, name, id)
	case strings.Contains(rest, "/blobs/"):
		_, dg, _ := strings.Cut(rest, "/blobs/")
		r.serveBlob(w, req, dg)
	case strings.Contains(rest, "/manifests/"):
		name, ref, _ := strings.Cut(rest, "/manifests/")
		r.serveManifest(w, req, name, ref)
	default:
		http.NotFound(w, req)
	}
}

func (r *fakeRegistry) serveUpload(w http.ResponseWriter, req *http.Request, name, id string) {
	body, _ := io.ReadAll(req.Body)
	location := func(id string) string { return "/v2/" + name + "/blobs/uploads/" + id }
	switch req.Method {
	case http.MethodPost:
		if dg := req.URL.Query().Get("digest"); dg != "" { // monolithic upload
			r.storeBlob(w, name, dg, body)
			return
		}
		id = strconv.Itoa(len(r.uploads) + 1)
		r.uploads[id] = nil
		w.Header().Set("Location", location(id))
		w.Header().Set("Docker-Upload-UUID", id)
		w.WriteHeader(http.StatusAccepted)
	case http.MethodPatch:
		r.uploads[id] = append(r.uploads[id], body...)
		w.Header().Set("Location", location(id))
		w.Header().Set("Range", fmt.Sprintf("0-%d", max(0, len(r.uploads[id])-1)))
		w.WriteHeader(http.StatusAccepted)
	case http.MethodPut:
		data := append(r.uploads[id], body...)
		delete(r.uploads, id)
		r.storeBlob(w, name, req.URL.Query().Get("digest"), data)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (r *fakeRegistry) storeBlob(w http.ResponseWriter, name, dg string, data []byte) {
	if got := digestOfBytes(data); got != dg {
		http.Error(w, fmt.Sprintf(`{"errors":[{"code":"DIGEST_INVALID","message":"got %s, want %s"}]}`, got, dg), http.StatusBadRequest)
		return
	}
	r.blobs[dg] = data
	w.Header().Set("Docker-Content-Digest", dg)
	w.Header().Set("Location", "/v2/"+name+"/blobs/"+dg)
	w.WriteHeader(http.StatusCreated)
}

func (r *fakeRegistry) serveBlob(w http.ResponseWriter, req *http.Request, dg string) {
	data, ok := r.blobs[dg]
	if !ok {
		http.Error(w, `{"errors":[{"code":"BLOB_UNKNOWN"}]}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Docker-Content-Digest", dg)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	if req.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

func (r *fakeRegistry) serveManifest(w http.ResponseWriter, req *http.Request, name, ref string) {
	switch req.Method {
	case http.MethodPut:
		body, _ := io.ReadAll(req.Body)
		m := fakeManifest{contentType: req.Header.Get("Content-Type"), body: body}
		dg := digestOfBytes(body)
		r.manifests[name+":"+ref] = m
		r.manifests[name+":"+dg] = m
		w.Header().Set("Docker-Content-Digest", dg)
		w.Header().Set("Location", "/v2/"+name+"/manifests/"+dg)
		w.WriteHeader(http.StatusCreated)
	case http.MethodGet, http.MethodHead:
		m, ok := r.manifests[name+":"+ref]
		if !ok {
			// An unknown repository is NAME_UNKNOWN, an unknown tag of a known one is
			// MANIFEST_UNKNOWN: the two texts skopeo turns into "name unknown" and
			// "manifest unknown".
			if !r.knows(name) {
				http.Error(w, `{"errors":[{"code":"NAME_UNKNOWN","message":"repository name not known to registry"}]}`, http.StatusNotFound)
				return
			}
			http.Error(w, `{"errors":[{"code":"MANIFEST_UNKNOWN","message":"manifest unknown"}]}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", m.contentType)
		w.Header().Set("Docker-Content-Digest", digestOfBytes(m.body))
		w.Header().Set("Content-Length", strconv.Itoa(len(m.body)))
		if req.Method == http.MethodGet {
			_, _ = w.Write(m.body)
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// TestFakeRegistryItself checks the stand-in with plain HTTP, so a failure of
// the skopeo test below can be blamed on the right side.
func TestFakeRegistryItself(t *testing.T) {
	reg := newFakeRegistry(t, "admin", "s3cret")

	resp, err := http.Get(reg.srv.URL + "/v2/") //nolint:noctx // a request to a test server
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized || !strings.HasPrefix(resp.Header.Get("WWW-Authenticate"), "Basic") {
		t.Errorf("without credentials: %d %q, want a Basic challenge", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
	}

	req, _ := http.NewRequest(http.MethodGet, reg.srv.URL+"/v2/", nil) //nolint:noctx // a request to a test server
	req.SetBasicAuth("admin", "s3cret")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("with credentials: %d, want 200", resp.StatusCode)
	}
	if served, refused := reg.counts(); served != 1 || refused != 1 {
		t.Errorf("served/refused = %d/%d, want 1/1", served, refused)
	}
}

// TestSkopeoAgainstFakeRegistry runs the real skopeo of the host against a
// registry of the test process. It needs no cluster and no network beyond the
// loopback, and is skipped where skopeo is not installed. It is what the
// stand-in tests cannot say: that skopeo falls back to HTTP for a docker
// reference with TLS verification off, reads the credentials from the
// authentication file, honors --preserve-digests, and that the failures it
// words come out in the kinds classifySkopeo maps them to.
func TestSkopeoAgainstFakeRegistry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skopeo is not run on Windows")
	}
	if _, err := exec.LookPath("skopeo"); err != nil {
		t.Skip("skopeo is not installed on this host")
	}
	// Nothing of the developer's own configuration may reach skopeo: a login of
	// the host would hide a missing --*-no-creds.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_RUNTIME_DIR", home)
	t.Setenv("XDG_CONFIG_HOME", home)

	copier, err := NewSkopeoCopier()
	if err != nil {
		t.Fatalf("NewSkopeoCopier: %v", err)
	}
	img := Dataset()[2] // team/api:1.0.0, a name of two components
	layout := t.TempDir()
	refs, err := WriteLayout(layout, []DatasetImage{img})
	if err != nil {
		t.Fatalf("WriteLayout: %v", err)
	}
	src := ImageRef{Transport: "oci", Location: layout + ":" + refs[0]}
	admin := &Credentials{User: "admin", Password: "s3cret-admin"}
	ctx := context.Background()

	reg := newFakeRegistry(t, admin.User, admin.Password)
	dst := ImageRef{Transport: "docker", Location: reg.host() + "/team/api:1.0.0"}

	t.Run("push without credentials is refused", func(t *testing.T) {
		err := copier.Copy(ctx, src, dst, CopyOptions{PreserveDigests: true})
		requireSkopeoKind(t, err, RegistryRefused)
	})

	t.Run("push with the wrong password is refused", func(t *testing.T) {
		err := copier.Copy(ctx, src, dst, CopyOptions{DestCreds: &Credentials{User: "admin", Password: "wrong"}, PreserveDigests: true})
		requireSkopeoKind(t, err, RegistryRefused)
	})

	t.Run("push over the HTTP fallback with the credentials of the authentication file", func(t *testing.T) {
		if err := copier.Copy(ctx, src, dst, CopyOptions{DestCreds: admin, PreserveDigests: true}); err != nil {
			t.Fatalf("Copy: %v", err)
		}
		m, ok := reg.manifest("team/api", "1.0.0")
		if !ok {
			t.Fatal("the registry has no manifest team/api:1.0.0 after the push")
		}
		// --preserve-digests: the registry holds the manifest of the layout,
		// byte for byte.
		if got := digestOfBytes(m.body); got != img.Digest {
			t.Errorf("manifest digest in the registry = %s, want the data set digest %s", got, img.Digest)
		}
		if _, ok := reg.manifest("team/api", img.Digest); !ok {
			t.Errorf("the registry has no manifest by the digest %s", img.Digest)
		}
		if served, _ := reg.counts(); served == 0 {
			t.Error("no request reached the registry with the right credentials")
		}
	})

	t.Run("manifest digest", func(t *testing.T) {
		got, err := copier.ManifestDigest(ctx, dst, admin)
		if err != nil {
			t.Fatalf("ManifestDigest: %v", err)
		}
		if got != img.Digest {
			t.Errorf("digest = %s, want %s", got, img.Digest)
		}
	})

	t.Run("manifest digest of the layout", func(t *testing.T) {
		got, err := copier.ManifestDigest(ctx, src, nil)
		if err != nil {
			t.Fatalf("ManifestDigest: %v", err)
		}
		if got != img.Digest {
			t.Errorf("digest = %s, want %s", got, img.Digest)
		}
	})

	t.Run("an unknown tag is a proven absence", func(t *testing.T) {
		_, err := copier.ManifestDigest(ctx, ImageRef{Transport: "docker", Location: reg.host() + "/team/api:9.9.9"}, admin)
		requireSkopeoKind(t, err, SeedFailed)
		if !errors.Is(err, errImageAbsent) {
			t.Errorf("error %v does not carry errImageAbsent (manifest unknown)", err)
		}
	})

	t.Run("an unknown repository is a proven absence (NAME_UNKNOWN)", func(t *testing.T) {
		_, err := copier.ManifestDigest(ctx, ImageRef{Transport: "docker", Location: reg.host() + "/nobody/home:1.0"}, admin)
		if !errors.Is(err, errImageAbsent) {
			t.Errorf("error %v does not carry errImageAbsent (name unknown)", err)
		}
	})

	t.Run("a refusal is not an absence", func(t *testing.T) {
		for name, creds := range map[string]*Credentials{"no credentials": nil, "wrong password": {User: "admin", Password: "wrong"}} {
			_, err := copier.ManifestDigest(ctx, ImageRef{Transport: "docker", Location: reg.host() + "/nobody/home:1.0"}, creds)
			if err == nil {
				t.Fatalf("%s: no error", name)
			}
			if errors.Is(err, errImageAbsent) {
				t.Errorf("%s: a refusal was taken for an absence: %v", name, err)
			}
		}
	})

	t.Run("pull to a directory with the credentials", func(t *testing.T) {
		out := t.TempDir()
		if err := copier.Copy(ctx, dst, ImageRef{Transport: "dir", Location: out}, CopyOptions{SrcCreds: admin, PreserveDigests: true}); err != nil {
			t.Fatalf("Copy: %v", err)
		}
		raw, err := os.ReadFile(filepath.Join(out, "manifest.json")) //nolint:gosec // a folder of this test
		if err != nil {
			t.Fatalf("the pull wrote no manifest.json: %v", err)
		}
		if got := digestOfBytes(raw); got != img.Digest {
			t.Errorf("pulled manifest digest = %s, want %s", got, img.Digest)
		}
	})

	t.Run("pull without credentials from a registry that wants them is refused", func(t *testing.T) {
		err := copier.Copy(ctx, dst, ImageRef{Transport: "dir", Location: t.TempDir()}, CopyOptions{})
		requireSkopeoKind(t, err, RegistryRefused)
	})

	t.Run("an open registry needs no credentials", func(t *testing.T) {
		open := newFakeRegistry(t, "", "")
		to := ImageRef{Transport: "docker", Location: open.host() + "/mirror/cached:1.0"}
		if err := copier.Copy(ctx, src, to, CopyOptions{PreserveDigests: true}); err != nil {
			t.Fatalf("Copy: %v", err)
		}
		if got, err := copier.ManifestDigest(ctx, to, nil); err != nil || got != img.Digest {
			t.Errorf("ManifestDigest = %q, %v; want %s", got, err, img.Digest)
		}
	})

	t.Run("a port nobody listens on is unreachable", func(t *testing.T) {
		port := loopback.ReservePort(t)
		to := ImageRef{Transport: "docker", Location: "127.0.0.1:" + strconv.Itoa(port) + "/x:1"}
		err := copier.Copy(ctx, src, to, CopyOptions{})
		requireSkopeoKind(t, err, RegistryUnreachable)
	})

	t.Run("a host that is not the loopback is refused before skopeo starts", func(t *testing.T) {
		err := copier.Copy(ctx, src, ImageRef{Transport: "docker", Location: "192.0.2.1:30481/x:1"}, CopyOptions{})
		if err == nil {
			t.Fatal("a LAN address was accepted")
		}
	})
}

// requireSkopeoKind fails unless err is a BenchError of the kind, and says
// nothing of what skopeo printed.
func requireSkopeoKind(t *testing.T, err error, kind ErrKind) {
	t.Helper()
	if err == nil {
		t.Fatalf("no error, want a BenchError of kind %d", kind)
	}
	var be *BenchError
	if !errors.As(err, &be) {
		t.Fatalf("error %T is not a *BenchError: %v", err, err)
	}
	if be.Kind != kind {
		t.Errorf("Kind = %d, want %d; message: %v", be.Kind, kind, err)
	}
	for _, leak := range []string{"FATA", "s3cret", "dial tcp", "level=", "Trying to pull"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("the message carries %q of the output of skopeo: %q", leak, err.Error())
		}
	}
}
