package ociresources

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/oci"
)

// DEVDESK-412 step 7: the Docker Hub tag dates are read through a bounded read.
// A page over the cap is an error and gives no partial result; a page exactly at
// the cap is read. The fake Hub never serves more than twice the cap: past that
// its body fails with errServedTooMuch, so a reader with no bound ends in that
// error instead of reading on.

// measuredHubPage is the largest real page measured on 2026-11-16 with the
// request fetchDockerHubTagsMeta builds (page_size=100, library/postgres:
// 530629 bytes; library/node 259560, library/python 443387). The cap has to be at
// least four times it.
const measuredHubPage = 530629

var errServedTooMuch = errors.New("the fake Hub was asked for more than twice the cap")

// hubPage is a body of exactly size bytes: a complete Hub page followed by
// spaces, so a read that stopped at the cap would still parse it. After
// serveLimit bytes it fails instead of going on.
type hubPage struct {
	prefix     string
	size       int
	serveLimit int
	served     int
}

func (p *hubPage) Read(b []byte) (int, error) {
	if p.served >= p.size {
		return 0, io.EOF
	}
	if p.served >= p.serveLimit {
		return 0, errServedTooMuch
	}
	n := len(b)
	if left := p.size - p.served; n > left {
		n = left
	}
	if left := p.serveLimit - p.served; n > left {
		n = left
	}
	for i := 0; i < n; i++ {
		if at := p.served + i; at < len(p.prefix) {
			b[i] = p.prefix[at]
		} else {
			b[i] = ' '
		}
	}
	p.served += n
	return n, nil
}

func (p *hubPage) Close() error { return nil }

// hubPageTransport answers every request with one hubPage.
type hubPageTransport struct{ page *hubPage }

func (rt hubPageTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode:    http.StatusOK,
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		Body:          rt.page,
		ContentLength: -1, // unknown, as with a chunked answer
		Request:       req,
	}, nil
}

const hubTwoTags = `{"results":[` +
	`{"name":"1.27","last_updated":"2026-01-02T03:04:05Z"},` +
	`{"name":"1.26","last_updated":"2025-12-01T00:00:00Z"}]}`

func hubMetaWith(t *testing.T, page *hubPage) MultiRegistryTagsMetaMsg {
	t.Helper()
	previous := ociHTTPClient
	ociHTTPClient = &http.Client{Transport: hubPageTransport{page}}
	t.Cleanup(func() { ociHTTPClient = previous })
	return run(t, loadMultiRegistryTagsMetaCmd(7, "k", "docker.io", "library/nginx")).(MultiRegistryTagsMetaMsg)
}

func TestHubTagsMetaCapCoversARealPage(t *testing.T) {
	if maxHubTagsMetaResponse < 4*measuredHubPage {
		t.Errorf("maxHubTagsMetaResponse = %d, want at least 4 x the largest page measured (%d)", maxHubTagsMetaResponse, 4*measuredHubPage)
	}
	if maxHubTagsMetaResponse > 8<<20 {
		t.Errorf("maxHubTagsMetaResponse = %d, want a cap no larger than 8 MiB", maxHubTagsMetaResponse)
	}
}

func TestHubTagsMetaReadIsBounded(t *testing.T) {
	const limit = maxHubTagsMetaResponse

	t.Run("exactly the cap is read", func(t *testing.T) {
		page := &hubPage{prefix: hubTwoTags, size: limit, serveLimit: 2 * limit}
		msg := hubMetaWith(t, page)

		if msg.Err != nil {
			t.Fatalf("Err = %v, want none", msg.Err)
		}
		if len(msg.Meta) != 2 || msg.Meta["1.27"].IsZero() || msg.Meta["1.26"].IsZero() {
			t.Errorf("Meta = %v, want both tags with their dates", msg.Meta)
		}
	})

	for _, tc := range []struct {
		name string
		size int
	}{
		{"one byte over the cap", limit + 1},
		{"twice the cap", 2 * limit},
		{"an endless body", 1 << 30}, // cut by the fake at twice the cap
	} {
		t.Run(tc.name, func(t *testing.T) {
			page := &hubPage{prefix: hubTwoTags, size: tc.size, serveLimit: 2 * limit}
			msg := hubMetaWith(t, page)

			if !errors.Is(msg.Err, oci.ErrBodyTooLarge) {
				t.Fatalf("Err = %v, want errors.Is oci.ErrBodyTooLarge", msg.Err)
			}
			if msg.Meta != nil {
				t.Errorf("Meta = %v, want none (no partial result)", msg.Meta)
			}
			if page.served > limit+1 {
				t.Errorf("%d bytes were taken off the body, want at most the cap plus one (%d)", page.served, limit+1)
			}
		})
	}
}

// The answers the existing code already handled are read as before.
func TestHubTagsMetaUsualAnswersAreUnchanged(t *testing.T) {
	t.Run("a normal page", func(t *testing.T) {
		msg := hubMetaWith(t, &hubPage{prefix: hubTwoTags, size: len(hubTwoTags), serveLimit: len(hubTwoTags)})
		if msg.Err != nil || len(msg.Meta) != 2 {
			t.Errorf("Err = %v, Meta = %v, want two tags and no error", msg.Err, msg.Meta)
		}
	})
	t.Run("a page with no result", func(t *testing.T) {
		const empty = `{"results":[]}`
		msg := hubMetaWith(t, &hubPage{prefix: empty, size: len(empty), serveLimit: len(empty)})
		if msg.Err != nil || len(msg.Meta) != 0 {
			t.Errorf("Err = %v, Meta = %v, want no tag and no error", msg.Err, msg.Meta)
		}
	})
	t.Run("a body that is not JSON", func(t *testing.T) {
		const junk = `<html>rate limited</html>`
		msg := hubMetaWith(t, &hubPage{prefix: junk, size: len(junk), serveLimit: len(junk)})
		if msg.Err == nil || errors.Is(msg.Err, oci.ErrBodyTooLarge) || strings.Contains(msg.Err.Error(), "rate limited") {
			t.Errorf("Err = %v, want a parse error that does not echo the body", msg.Err)
		}
		if msg.Meta != nil {
			t.Errorf("Meta = %v, want none", msg.Meta)
		}
	})
}
