package ociresources

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// recordingTransport answers every request with a canned Hub tags page and
// remembers which hosts were called.
type recordingTransport struct {
	mu    sync.Mutex
	hosts []string
}

func (rt *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.mu.Lock()
	rt.hosts = append(rt.hosts, req.URL.Host)
	rt.mu.Unlock()

	body := `{"results":[{"name":"1.27","last_updated":"2026-01-02T03:04:05Z"}]}`
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

func (rt *recordingTransport) calls() []string {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return append([]string(nil), rt.hosts...)
}

// DEVDESK-297: the tag dates are asked of the Hub API for every spelling of
// the Hub that oci.IsDockerHub recognises, not only the two bare hostnames.
func TestHubTagsMetaFollowsIsDockerHub(t *testing.T) {
	tests := []struct {
		name     string
		registry string
		wantHub  bool
	}{
		{"bare docker.io", "docker.io", true},
		{"scheme docker.io", "https://docker.io", true},
		{"scheme registry-1 with slash", "https://registry-1.docker.io/", true},
		{"bare registry-1", "registry-1.docker.io", true},
		{"upper case", "DOCKER.IO", true},
		{"private host", "registry.example.com", false},
		{"nexus path", "https://nexus.example.com/repository/x", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := &recordingTransport{}
			previous := ociHTTPClient
			ociHTTPClient = &http.Client{Transport: rt}
			t.Cleanup(func() { ociHTTPClient = previous })

			msg := run(t, loadMultiRegistryTagsMetaCmd(7, "k", tt.registry, "library/nginx")).(MultiRegistryTagsMetaMsg)

			if msg.Err != nil {
				t.Fatalf("Err = %v, want none", msg.Err)
			}
			calls := rt.calls()
			if !tt.wantHub {
				if len(calls) != 0 || msg.Meta != nil {
					t.Errorf("calls = %v, Meta = %v, want no request and an empty answer", calls, msg.Meta)
				}
				return
			}
			if len(calls) != 1 || calls[0] != "hub.docker.com" {
				t.Errorf("calls = %v, want one request to hub.docker.com", calls)
			}
			if _, ok := msg.Meta["1.27"]; !ok {
				t.Errorf("Meta = %v, want the tag 1.27 filled in", msg.Meta)
			}
		})
	}
}
