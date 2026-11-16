package oci

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// DEVDESK-412 step 3: oci.Client (template tags and manifest) keeps its
// credentials on the origin they were meant for and is bounded in time. Server
// A plays the registry, server B another origin (same host, other port). The
// blob download is deliberately not covered here: it keeps its own policy
// (step 4), and these tests must not pin what happens to a redirected blob.

const (
	clientTestUser = "user"
	clientTestPass = "S3CRETPW"
)

// clientTestDigest is the digest of the layer the fixtures serve (clientTarGz):
// the blob is checked against the digest of the manifest (DEVDESK-412 step 5).
var clientTestDigest = sha256Digest(clientFixtureArchive())

// clientOp is one public entry point of oci.Client that starts with a request
// the origin guard must cover.
type clientOp struct {
	name string
	run  func(c *Client) error
}

func clientOps() []clientOp {
	return []clientOp{
		{"ListTags", func(c *Client) error {
			_, err := c.ListTags("tpl")
			return err
		}},
		{"DownloadTemplate", func(c *Client) error {
			_, err := c.DownloadTemplate("tpl", "v1")
			return err
		}},
	}
}

// basicHeader is the Authorization value the client must send for the test
// credentials.
func basicHeader() string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(clientTestUser+":"+clientTestPass))
}

// authsOf returns the Authorization header of every recorded hit whose
// "host path" contains pathPart, in arrival order.
func authsOf(l *hitLog, pathPart string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for i, h := range l.hits {
		if strings.Contains(h, pathPart) {
			out = append(out, l.auth[i])
		}
	}
	return out
}

func TestClientRefusesOffOriginRedirect(t *testing.T) {
	for _, op := range clientOps() {
		t.Run(op.name, func(t *testing.T) {
			var bHits, aHits hitLog
			b := recordingServer(t, &bHits)
			a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				aHits.record(r)
				// Every request of the registry, tags or manifest, is sent to
				// the other origin with a signed query.
				http.Redirect(w, r, b.URL+"/x?sig=SECRET", http.StatusTemporaryRedirect)
			}))
			t.Cleanup(a.Close)

			c := NewClient(a.URL, clientTestUser, clientTestPass)
			err := op.run(c)

			if !errors.Is(err, ErrOffOriginRedirect) {
				t.Fatalf("err = %v, want errors.Is ErrOffOriginRedirect", err)
			}
			if n := bHits.count(); n != 0 {
				t.Errorf("the other origin received %d requests, want 0", n)
			}
			if got := bHits.anyAuth(); got != "" {
				t.Errorf("the other origin saw Authorization %q", got)
			}
			if got := authsOf(&aHits, ""); len(got) != 1 || got[0] != basicHeader() {
				t.Errorf("registry Authorization headers = %q, want one Basic header", got)
			}
			for _, leak := range []string{"sig=SECRET", "SECRET", clientTestPass, "/x"} {
				if strings.Contains(err.Error(), leak) {
					t.Errorf("error %q leaks %q", err, leak)
				}
			}
		})
	}
}

// clientTarGz serves as the layer the same-origin test downloads.
func clientTarGz(t *testing.T) []byte {
	t.Helper()
	return clientFixtureArchive()
}

func TestClientFollowsSameOriginRedirect(t *testing.T) {
	archive := clientTarGz(t)

	cases := []struct {
		op       clientOp
		moved    string // a path the registry redirects to, same origin
		fromPart string // path part of the first hop
	}{
		{clientOps()[0], "/v2/tpl/moved-tags", "/tags/list"},
		{clientOps()[1], "/v2/tpl/moved-manifests/v1", "/manifests/"},
	}
	for _, tc := range cases {
		t.Run(tc.op.name, func(t *testing.T) {
			var hits hitLog
			a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.record(r)
				switch {
				case strings.Contains(r.URL.Path, "/moved-tags"):
					_, _ = fmt.Fprint(w, `{"tags":["v1"]}`)
				case strings.Contains(r.URL.Path, "/moved-manifests/"):
					_, _ = fmt.Fprintf(w, `{"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar+gzip","digest":%q}]}`, clientTestDigest)
				case strings.Contains(r.URL.Path, "/blobs/"):
					_, _ = w.Write(archive)
				default:
					http.Redirect(w, r, tc.moved, http.StatusFound)
				}
			}))
			t.Cleanup(a.Close)

			c := NewClient(a.URL, clientTestUser, clientTestPass)
			if err := tc.op.run(c); err != nil {
				t.Fatalf("same-origin redirect refused or failed: %v", err)
			}

			first := authsOf(&hits, tc.fromPart)
			second := authsOf(&hits, "/moved-")
			if len(first) != 1 || len(second) != 1 {
				t.Fatalf("hops: first %d, second %d, want 1 and 1", len(first), len(second))
			}
			if first[0] != basicHeader() {
				t.Errorf("first hop Authorization = %q, want the Basic header", first[0])
			}
			if second[0] != basicHeader() {
				t.Errorf("second hop Authorization = %q, want the Basic header", second[0])
			}
		})
	}
}

func TestNewClientIsBounded(t *testing.T) {
	c := NewClient("http://registry.test", clientTestUser, clientTestPass)

	if templateClientTimeout != time.Minute {
		t.Errorf("templateClientTimeout = %v, want 1 minute (decision Q2)", templateClientTimeout)
	}
	if c.httpClient.Timeout != templateClientTimeout {
		t.Errorf("httpClient.Timeout = %v, want templateClientTimeout (%v)", c.httpClient.Timeout, templateClientTimeout)
	}
	if c.httpClient.CheckRedirect == nil {
		t.Fatal("httpClient.CheckRedirect = nil, want the origin guard")
	}

	if c.blobClient.Timeout != templateClientTimeout {
		t.Errorf("blobClient.Timeout = %v, want templateClientTimeout (%v)", c.blobClient.Timeout, templateClientTimeout)
	}
	if c.blobClient.CheckRedirect == nil {
		t.Error("blobClient.CheckRedirect = nil, want the blob redirect policy")
	}

	// The hook is the guard itself, not just any function: another origin is
	// refused, the same origin is accepted, without any network traffic.
	first, _ := http.NewRequest(http.MethodGet, "http://registry.test/v2/tpl/manifests/v1", nil)
	other, _ := http.NewRequest(http.MethodGet, "http://cdn.test/x", nil)
	same, _ := http.NewRequest(http.MethodGet, "http://registry.test/elsewhere", nil)
	if err := c.httpClient.CheckRedirect(other, []*http.Request{first}); !errors.Is(err, ErrOffOriginRedirect) {
		t.Errorf("CheckRedirect(other origin) = %v, want ErrOffOriginRedirect", err)
	}
	if err := c.httpClient.CheckRedirect(same, []*http.Request{first}); err != nil {
		t.Errorf("CheckRedirect(same origin) = %v, want nil", err)
	}
}
