package imagesearch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/oci"
)

// hubServer serves body with status and records the requests it receives.
func hubServer(t *testing.T, status int, body string) (*httptest.Server, *atomic.Int32, *atomic.Pointer[http.Request]) {
	t.Helper()
	var n atomic.Int32
	var last atomic.Pointer[http.Request]
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		last.Store(r.Clone(context.Background()))
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	// The package variable is the production seam: point it at the fake.
	old := hubSearchURL
	hubSearchURL = srv.URL
	t.Cleanup(func() { hubSearchURL = old })
	return srv, &n, &last
}

func search(t *testing.T, tg Target, q Query) ([]Image, bool, error) {
	t.Helper()
	return HubSearcher{}.Search(context.Background(), tg, q)
}

const sampleBody = `{"results":[` +
	`{"name":"nginx","is_official":true,"star_count":20000,"description":"Official build"},` +
	`{"name":"bitnami/nginx","star_count":0},` +
	`{"name":"x/y"}]}`

func TestHubSingleAnonymousRequest(t *testing.T) {
	_, n, last := hubServer(t, 200, sampleBody)
	_, partial, err := search(t, Target{Username: "u", Password: "p"}, Query{Text: "nginx"})
	if err != nil {
		t.Fatal(err)
	}
	if partial {
		t.Error("partial = true, want false")
	}
	if got := n.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
	r := last.Load()
	if r == nil {
		t.Fatal("no request reached the server")
	}
	if r.Method != http.MethodGet {
		t.Errorf("method = %s", r.Method)
	}
	if got := r.URL.Query().Get("q"); got != "nginx" {
		t.Errorf("q = %q", got)
	}
	if got := r.URL.Query().Get("n"); got != "50" {
		t.Errorf("n = %q, want 50", got)
	}
	if r.URL.Query().Get("page") != "" {
		t.Error("a page parameter was sent")
	}
	for _, h := range []string{"Authorization", "Cookie", "X-Registry-Auth"} {
		if v := r.Header.Get(h); v != "" {
			t.Errorf("header %s = %q, want none", h, v)
		}
	}
	if _, _, ok := r.BasicAuth(); ok {
		t.Error("basic auth sent")
	}
}

func TestHubQueryIsEscapedAndLimitCapped(t *testing.T) {
	tests := []struct {
		limit int
		text  string
		wantN string
	}{
		{0, "nginx", "50"},
		{200, "nginx", "50"},
		{50, "nginx", "50"},
		{10, "nginx", "10"},
		{5, "a b&n=1", "5"},
	}
	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.limit)+tt.text, func(t *testing.T) {
			_, _, last := hubServer(t, 200, `{"results":[]}`)
			if _, _, err := search(t, Target{}, Query{Text: tt.text, Limit: tt.limit}); err != nil {
				t.Fatal(err)
			}
			if last.Load() == nil {
				t.Fatal("no request reached the server")
			}
			q := last.Load().URL.Query()
			if q.Get("n") != tt.wantN {
				t.Errorf("n = %q, want %q", q.Get("n"), tt.wantN)
			}
			if q.Get("q") != tt.text {
				t.Errorf("q = %q, want %q", q.Get("q"), tt.text)
			}
		})
	}
}

func TestHubMapsResults(t *testing.T) {
	hubServer(t, 200, sampleBody)
	got, _, err := search(t, Target{EntryKey: "hub"}, Query{Text: "nginx"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d images, want 3: %+v", len(got), got)
	}
	nginx, bitnami, xy := got[0], got[1], got[2]

	if nginx.Name != "nginx" || nginx.Repo != "library/nginx" {
		t.Errorf("official: Name=%q Repo=%q, want nginx / library/nginx", nginx.Name, nginx.Repo)
	}
	if !nginx.Official || !nginx.HasStars || nginx.Stars != 20000 || nginx.Description != "Official build" {
		t.Errorf("official fields: %+v", nginx)
	}
	if nginx.EntryKey != "hub" {
		t.Errorf("EntryKey = %q, want hub", nginx.EntryKey)
	}
	if bitnami.Repo != "bitnami/nginx" || bitnami.Official {
		t.Errorf("bitnami: %+v", bitnami)
	}
	// A 0 Hub provided is shown as 0 ...
	if !bitnami.HasStars || bitnami.Stars != 0 {
		t.Errorf("provided zero: HasStars=%v Stars=%d, want true/0", bitnami.HasStars, bitnami.Stars)
	}
	// ... and an absent value stays empty, never 0.
	if xy.HasStars {
		t.Errorf("absent star_count: HasStars = true, want false")
	}
	if xy.Repo != "x/y" || xy.Official {
		t.Errorf("x/y: %+v", xy)
	}
}

func TestHubDisplayNameDropsLibraryButRepoKeepsIt(t *testing.T) {
	hubServer(t, 200, `{"results":[{"name":"library/nginx","is_official":true}]}`)
	got, _, err := search(t, Target{}, Query{Text: "nginx"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	if got[0].Name != "nginx" {
		t.Errorf("Name = %q, want nginx (library/ dropped)", got[0].Name)
	}
	if got[0].Repo != "library/nginx" {
		t.Errorf("Repo = %q, want library/nginx", got[0].Repo)
	}
}

func TestHubDescriptionIsSanitised(t *testing.T) {
	tests := []struct {
		name, desc, want string
	}{
		{"csi", "a\x1b[31mred\x1b[0m\nline", "ared line"},
		{"osc", "x\x1b]0;title\x07y", "xy"},
		{"c0", "a\x00b\x07c\x08d", "abcd"},
		{"c1", "a\u0085b\u009bc", "abc"},
		{"whitespace", "a\r\n\tb   c", "a b c"},
		// ansi.Strip reads "ESC b" as a two-byte escape sequence.
		{"lone esc", "a\x1bb", "a"},
		{"bidi", "a\u202eb\u2066c\u2069d\u200be\u200ff\ufeffg", "abcdefg"},
		{"plain", "Official build", "Official build"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"results":[{"name":"nginx","description":` + strconv.Quote(tt.desc) + `}]}`
			// strconv.Quote emits \x1b, which is not valid JSON: use \u escapes.
			body = strings.NewReplacer(`\x`, `\u00`, `\a`, `\u0007`).Replace(body)
			hubServer(t, 200, body)
			got, _, err := search(t, Target{}, Query{Text: "nginx"})
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 {
				t.Fatalf("got %+v", got)
			}
			d := got[0].Description
			if strings.ContainsAny(d, "\x1b\x00\x07\x08\r\n\t\u0085\u009b\u202e\u2066\u2069\u200b\u200f\ufeff") {
				t.Errorf("control character left in %q", d)
			}
			if d != tt.want {
				t.Errorf("Description = %q, want %q", d, tt.want)
			}
			if strings.Contains(d, "\x1b") {
				t.Errorf("ESC left in %q", d)
			}
		})
	}
}

func TestHubRejectsInvalidNames(t *testing.T) {
	for _, name := range []string{"-rm", "--help", "", "UPPER/x", "a b", "a:b", "a@sha256:abc", "/x", "x/", "a//b", "a/-b", ".hidden", "a\nb", "a_", "_a", "a.", "-", "a-/b", "a._b"} {
		t.Run(name, func(t *testing.T) {
			body := `{"results":[{"name":` + strconv.Quote(name) + `},{"name":"ok/fine"}]}`
			hubServer(t, 200, body)
			got, _, err := search(t, Target{}, Query{Text: "x"})
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].Repo != "ok/fine" {
				t.Errorf("name %q: got %+v, want only ok/fine", name, got)
			}
		})
	}
}

func TestHubKeepsValidNames(t *testing.T) {
	hubServer(t, 200, `{"results":[{"name":"a.b-c_d/e__f"},{"name":"nginx"},{"name":"a/b/c"},{"name":"a--b/c---d"}]}`)
	got, _, err := search(t, Target{}, Query{Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	// The OCI grammar allows "__" and runs of "-" inside a component.
	want := []string{"a.b-c_d/e__f", "library/nginx", "a/b/c", "a--b/c---d"}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %v", got, want)
	}
	for i, w := range want {
		if got[i].Repo != w {
			t.Errorf("got[%d].Repo = %q, want %q", i, got[i].Repo, w)
		}
	}
}

func TestHubStatusErrors(t *testing.T) {
	for _, code := range []int{400, 401, 404, 429, 500, 503, 204, 301} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			hubServer(t, code, `{"results":[{"name":"nginx"}]}`)
			got, _, err := search(t, Target{}, Query{Text: "nginx"})
			var se *oci.StatusError
			if !errors.As(err, &se) {
				t.Fatalf("err = %v, want *oci.StatusError", err)
			}
			if se.Code != code || se.Source != oci.SourceRegistry {
				t.Errorf("StatusError = %+v, want code %d from the registry", se, code)
			}
			if len(got) != 0 {
				t.Errorf("images returned on a %d: %+v", code, got)
			}
		})
	}
}

func TestHubMalformedBody(t *testing.T) {
	hubServer(t, 200, "<html>not json</html>")
	if _, _, err := search(t, Target{}, Query{Text: "nginx"}); err == nil {
		t.Fatal("want an error on a non-JSON body")
	}
}

func TestHubBodyIsLimitedToOneMiB(t *testing.T) {
	// Valid JSON, but larger than 1 MiB: the cap must cut it, hence an error.
	pad := strings.Repeat("a", 1<<20+1024)
	hubServer(t, 200, `{"results":[{"name":"nginx","description":"`+pad+`"}]}`)
	got, _, err := search(t, Target{}, Query{Text: "nginx"})
	if err == nil {
		t.Fatalf("want an error on a body over 1 MiB, got %d images", len(got))
	}
}

func TestHubBodyIsNotReadPastTheCap(t *testing.T) {
	var written atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{"name":"nginx","description":"`))
		chunk := []byte(strings.Repeat("a", 64<<10))
		for i := 0; i < 400; i++ { // up to 25 MiB offered
			n, err := w.Write(chunk)
			written.Add(int64(n))
			if err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	old := hubSearchURL
	hubSearchURL = srv.URL
	t.Cleanup(func() { hubSearchURL = old })
	if _, _, err := search(t, Target{}, Query{Text: "nginx"}); err == nil {
		t.Fatal("want an error")
	}
	if got := written.Load(); got > 8<<20 {
		t.Errorf("server wrote %d bytes before the client gave up, want the read stopped near 1 MiB", got)
	}
}

func TestHubHonoursContext(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-block:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(block); srv.Close() })
	old := hubSearchURL
	hubSearchURL = srv.URL
	t.Cleanup(func() { hubSearchURL = old })

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _, err := HubSearcher{}.Search(ctx, Target{}, Query{Text: "nginx"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want DeadlineExceeded", err)
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	_, _, err = HubSearcher{}.Search(ctx2, Target{}, Query{Text: "nginx"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want Canceled", err)
	}
}

func TestHubEndpointFieldOverridesTheVariable(t *testing.T) {
	srv, n, _ := hubServer(t, 200, sampleBody)
	hubSearchURL = "http://127.0.0.1:1/unreachable"
	got, _, err := HubSearcher{Endpoint: srv.URL}.Search(context.Background(), Target{}, Query{Text: "nginx"})
	if err != nil || len(got) != 3 || n.Load() != 1 {
		t.Errorf("got (%v, %v), requests %d", got, err, n.Load())
	}
}

func TestHubRepoPrefixKeepsOnlyMatchingNames(t *testing.T) {
	hubServer(t, 200, `{"results":[{"name":"nginx"},{"name":"bitnami/nginx"},{"name":"other/nginx"},{"name":"bitnami/redis"}]}`)
	got, _, err := search(t, Target{RepoPrefix: "bitnami"}, Query{Text: "nginx"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Repo != "bitnami/nginx" || got[1].Repo != "bitnami/redis" {
		t.Errorf("got %+v", got)
	}
}

func TestHubNeverReturnsMoreThanTheCap(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`{"results":[`)
	for i := 0; i < 80; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"name":"u/n` + strconv.Itoa(i) + `"}`)
	}
	sb.WriteString(`]}`)
	hubServer(t, 200, sb.String())
	got, partial, err := search(t, Target{}, Query{Text: "n"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > HubLimit {
		t.Errorf("got %d images, want at most %d", len(got), HubLimit)
	}
	if partial {
		t.Error("partial = true, want false")
	}
}

func TestHubDoesNotFollowRedirects(t *testing.T) {
	var hitsB atomic.Int32
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsB.Add(1)
		_, _ = w.Write([]byte(`{"results":[{"name":"nginx"}]}`))
	}))
	t.Cleanup(b.Close)
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, b.URL, http.StatusFound)
	}))
	t.Cleanup(a.Close)
	old := hubSearchURL
	hubSearchURL = a.URL
	t.Cleanup(func() { hubSearchURL = old })

	got, _, err := search(t, Target{}, Query{Text: "nginx"})
	var se *oci.StatusError
	if !errors.As(err, &se) || se.Code != http.StatusFound {
		t.Fatalf("err = %v, want *oci.StatusError with code 302", err)
	}
	if len(got) != 0 {
		t.Errorf("images returned on a redirect: %+v", got)
	}
	if n := hitsB.Load(); n != 0 {
		t.Errorf("redirect target received %d requests, want 0", n)
	}
}
