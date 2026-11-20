package oci

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// DEVDESK-629 (F3, H3): a 404 whose Nexus message is "Remote Auto Blocked" is the
// proxy's upstream being temporarily blocked, not a missing repository. The message is
// read on errors[0].message of the JSON body (read at most maxStatusBody bytes, on a
// 404 only); the body itself never reaches the error.

const (
	blockedJSON = `{"errors":[{"code":"UNKNOWN","message":"Remote Auto Blocked until 2026-11-20T14:24:21.987Z"}]}`
	// blockedText is the text of the error of a blocked 404: no date, no duration.
	blockedText = "registry returned 404: the proxy's upstream is temporarily blocked, try again later"
)

// hijackedTruncated answers a 404 whose Content-Length promises more than is sent,
// then closes the connection.
func hijackedTruncated(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, _ *http.Request) {
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("Hijack: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = buf.WriteString("HTTP/1.1 404 Not Found\r\nContent-Type: application/json\r\nContent-Length: 500\r\n\r\n" +
			`{"errors":[{"message":"Remote Auto Blocked until 2026-11-20T14`)
		_ = buf.Flush()
	}
}

func TestRemoteAutoBlockedIsRecognized(t *testing.T) {
	answer := func(status int, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}
	}
	notBlocked := "registry returned 404"
	cases := []struct {
		name    string
		handler func(t *testing.T) http.HandlerFunc
		status  int
		blocked bool
		text    string // the exact Error()
	}{
		{"blocked message", func(*testing.T) http.HandlerFunc { return answer(404, blockedJSON) }, 404, true, blockedText},
		{"blocked message, body padded but under the bound", func(*testing.T) http.HandlerFunc {
			return answer(404, strings.Repeat(" ", 3000)+blockedJSON)
		}, 404, true, blockedText},
		{"repository not found", func(*testing.T) http.HandlerFunc {
			return answer(404, `{"errors":[{"message":"Repository not found"}]}`)
		}, 404, false, notBlocked},
		{"empty body", func(*testing.T) http.HandlerFunc { return answer(404, "") }, 404, false, notBlocked},
		{"plain text, not JSON", func(*testing.T) http.HandlerFunc {
			return answer(404, "Remote Auto Blocked until 2026-11-20T14:24:21.987Z")
		}, 404, false, notBlocked},
		{"JSON without errors", func(*testing.T) http.HandlerFunc {
			return answer(404, `{"message":"Remote Auto Blocked until 2026-11-20T14:24:21.987Z"}`)
		}, 404, false, notBlocked},
		{"body over 4 KiB", func(*testing.T) http.HandlerFunc {
			return answer(404, strings.Repeat(" ", 5000)+blockedJSON)
		}, 404, false, notBlocked},
		{"body cut short", hijackedTruncated, 404, false, notBlocked},
		{"lower case message", func(*testing.T) http.HandlerFunc {
			return answer(404, `{"errors":[{"message":"remote auto blocked until 2026-11-20T14:24:21.987Z"}]}`)
		}, 404, false, notBlocked},
		{"leading space in the message", func(*testing.T) http.HandlerFunc {
			return answer(404, `{"errors":[{"message":" Remote Auto Blocked until 2026-11-20T14:24:21.987Z"}]}`)
		}, 404, false, notBlocked},
		{"blocked message on a 502", func(*testing.T) http.HandlerFunc { return answer(502, blockedJSON) }, 502, false, "registry returned 502"},
		{"blocked message on a 403", func(*testing.T) http.HandlerFunc { return answer(403, blockedJSON) }, 403, false, "registry returned 403"},
		{"blocked message second of two entries", func(*testing.T) http.HandlerFunc {
			return answer(404, `{"errors":[{"message":"Repository not found"},{"message":"Remote Auto Blocked until 2026-11-20T14:24:21.987Z"}]}`)
		}, 404, false, notBlocked},
	}
	calls := []struct {
		name string
		call func(url string) error
	}{
		{"tags", func(url string) error {
			_, err := ListRegistryTagsContext(context.Background(), url, "docker-proxy/mirror/cached", "", "")
			return err
		}},
		{"catalog", func(url string) error {
			_, _, err := ListCatalogUnderContext(context.Background(), url, "docker-proxy", "", "", 10, 1000)
			return err
		}},
	}
	for _, c := range calls {
		for _, tc := range cases {
			t.Run(c.name+"/"+tc.name, func(t *testing.T) {
				srv := httptest.NewServer(tc.handler(t))
				defer srv.Close()

				err := c.call(srv.URL)

				var se *StatusError
				if !errors.As(err, &se) {
					t.Fatalf("err = %v (%T), want a *StatusError", err, err)
				}
				if se.Code != tc.status || se.Source != SourceRegistry {
					t.Errorf("StatusError = {Code: %d, Source: %v}, want {Code: %d, Source: SourceRegistry}", se.Code, se.Source, tc.status)
				}
				if got := se.UpstreamBlocked(); got != tc.blocked {
					t.Errorf("UpstreamBlocked() = %v, want %v", got, tc.blocked)
				}
				if err.Error() != tc.text {
					t.Errorf("Error() = %q, want %q", err.Error(), tc.text)
				}
				// The body, the date and the message never reach the error.
				for _, leak := range []string{"Remote Auto Blocked", "until", "2026", "errors", "Repository not found"} {
					if strings.Contains(err.Error(), leak) {
						t.Errorf("Error() = %q carries %q from the body", err.Error(), leak)
					}
				}
			})
		}
	}
}
