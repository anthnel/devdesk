package nexusbench

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/anthnel/devdesk/internal/nexusfixture"
	"github.com/anthnel/devdesk/internal/nexusfixture/fakenexus"
)

// Step 7b of DEVDESK-580 (PR2): a fixture that says "flow: bearer" or "flow:
// redirects" holds several exchanges, and the recorder follows them as DevDesk
// would, within limits.
//
// flow: bearer. The first exchange is sent as DevDesk sends it (Basic for admin,
// pull and badcred, nothing for anon). On a 401 whose WWW-Authenticate is a
// Bearer challenge, the recorder follows the challenge only when its realm is
// one of the origins of RecordOptions.Origins (matched on host:port, like the
// Host of a fixture) and the path of the realm is a token path: it sends the
// token request (the identity's Basic, the service and scope of the challenge),
// then the first request again with "Bearer <token>": three exchanges. A realm
// anywhere else ends the flow there: nothing is sent to it, the file holds the
// 401 only and a note, "realm off-origin, not followed" (an origin the run does
// not know) or "realm not followed" (another refusal). A 401 of the token
// request (the badcred identity) ends it too: two exchanges. No 401 at all: one
// exchange and the note "no challenge". The token read is a term no file and no
// output may hold, whatever the place; an answer that holds it where redaction
// would not show it is a refusal, as for a password.
//
// flow: redirects. A 3xx whose Location is an origin of Origins (and a request
// of the closed list) is followed, up to 5 hops, each one a recorded exchange
// that carries the same credentials as the first; a Location anywhere else is
// recorded and not followed, with the note "off-origin redirect, not followed",
// another origin of the run (the REST one) included;
// a chain that goes past 5 hops is an error and nothing is written.
//
// The file keeps its flow header. The note is written in the file; where is the
// recorder's choice. The recorded file is compared with what it holds, all of its
// exchanges, to say "unchanged".
//
// Chosen here, to be confirmed at review: the realm and the Location are matched
// on their host:port against the keys of Origins (the real Nexus names
// 127.0.0.1:30482, the test server listens elsewhere), and the request goes to the
// base URL of that key; a probe host that cannot resolve (realm-probe.invalid) is
// never reached, which a successful run proves.

const (
	recFlowToken   = "flowtok-Pq83MzV1xYw0"
	recRealm       = "http://127.0.0.1:30482/v2/token"
	recNoteRealm   = "realm off-origin, not followed"
	recNoteRealmNF = "realm not followed"
	recNoteNoChall = "no challenge"
	recNoteRedir   = "off-origin redirect, not followed"
)

// bearerConfig is what the bench of a Bearer flow does.
type bearerConfig struct {
	token       string
	realm       string // "" = recRealm
	noChallenge bool   // the first request is answered 200
	tokenStatus int    // 0 = 200, or the 401 a wrong password gets
	expiresIn   int    // 0 = 300
	catalog     string // "" = recCatalogBody
	echoBody    bool   // the retry answer carries the token in a field redaction does not know
	echoHeader  bool   // the retry answer carries the token in a header the fixture drops
}

// bearerBench is a registry that asks for a Bearer token on /v2/_catalog and
// serves it on /v2/token for the bench passwords of recPasswords.
type bearerBench struct {
	srv  *httptest.Server
	mu   sync.Mutex
	seen []recSeen
	cfg  bearerConfig
}

func newBearerBench(t *testing.T, cfg bearerConfig) *bearerBench {
	t.Helper()
	if cfg.token == "" {
		cfg.token = recFlowToken
	}
	b := &bearerBench{cfg: cfg}
	b.srv = httptest.NewServer(http.HandlerFunc(b.serve))
	t.Cleanup(b.srv.Close)
	return b
}

func (b *bearerBench) update(f func(*bearerConfig)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	f(&b.cfg)
}

func (b *bearerBench) requests() []recSeen {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.seen)
}

func (b *bearerBench) serve(w http.ResponseWriter, r *http.Request) {
	user, pass, basic := r.BasicAuth()
	b.mu.Lock()
	b.seen = append(b.seen, recSeen{method: r.Method, uri: r.RequestURI, user: user, header: r.Header.Clone()})
	c := b.cfg
	b.mu.Unlock()

	h := w.Header()
	h.Set("Server", recServer)
	h.Set("Set-Cookie", "NXSESSIONID=rec-session; Path=/")
	h.Set("Content-Type", "application/json")
	if r.URL.Path == "/v2/token" {
		switch {
		case c.tokenStatus != 0:
			w.WriteHeader(c.tokenStatus)
		case basic && pass != recPasswords[user]:
			w.WriteHeader(http.StatusUnauthorized)
		default:
			expires := c.expiresIn
			if expires == 0 {
				expires = 300
			}
			body, _ := json.Marshal(map[string]any{"token": c.token, "expires_in": expires})
			_, _ = w.Write(body)
		}
		return
	}
	if !c.noChallenge && r.Header.Get("Authorization") != "Bearer "+c.token {
		realm := c.realm
		if realm == "" {
			realm = recRealm
		}
		h.Set("Docker-Distribution-Api-Version", "registry/2.0")
		h.Set("WWW-Authenticate", `Bearer realm="`+realm+`",service="`+recRealm+`",scope="registry:catalog:*"`)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errors":[{"code":"UNAUTHORIZED","message":"authentication required"}]}`))
		return
	}
	body := c.catalog
	if body == "" {
		body = recCatalogBody
	}
	if c.echoBody {
		body = `{"repositories":[],"hint":"` + c.token + `"}`
	}
	if c.echoHeader {
		h.Set("X-Echo", c.token)
	}
	_, _ = w.Write([]byte(body))
}

// flowEnv is a recEnv whose group connector origin is a Bearer registry.
func flowEnv(t *testing.T, cfg bearerConfig) (*recEnv, *bearerBench) {
	t.Helper()
	e := newRecEnv(t)
	b := newBearerBench(t, cfg)
	e.opts.Origins[recDockerHost] = b.srv.URL
	return e, b
}

// hitCounter is an origin the recorder must never reach.
func hitCounter(t *testing.T) (srv *httptest.Server, hits *atomic.Int32) {
	t.Helper()
	hits = new(atomic.Int32)
	srv = httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	t.Cleanup(srv.Close)
	return srv, hits
}

// ---- helpers ------------------------------------------------------------------------

// requireNoTerm fails when a file under dir holds a term, as is or URL-escaped.
func requireNoTerm(t *testing.T, dir string, terms ...string) {
	t.Helper()
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data := mustRead(t, path)
		for _, term := range terms {
			for _, form := range []string{term, url.QueryEscape(term)} {
				if bytes.Contains(data, []byte(form)) {
					t.Errorf("%s holds a term that must never reach a file", path)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// requireNoTermIn fails when an error or an output holds a term.
func requireNoTermIn(t *testing.T, what, text string, terms ...string) {
	t.Helper()
	for _, term := range terms {
		if strings.Contains(text, term) {
			t.Errorf("%s holds a term that must never be shown: %q", what, text)
		}
	}
}

// requireFlowRecorded is requireRecorded for a fixture of several exchanges: it
// reads back, writes back to the same bytes, leaks nothing (the token included)
// and says who wrote it and when.
func requireFlowRecorded(t *testing.T, path string, exchanges int, terms ...string) nexusfixture.Fixture {
	t.Helper()
	data := mustRead(t, path)
	f := parseRecorded(t, path)
	if got := nexusfixture.Format(f); !bytes.Equal(got, data) {
		t.Errorf("Format(Parse(file)) differs from the file:\n%s\n--- want\n%s", got, data)
	}
	if leaks := nexusfixture.CheckLeaks(f.Name, data, slices.Concat(recExact(), terms)); len(leaks) != 0 {
		t.Errorf("the recorded file leaks: %+v", leaks)
	}
	if f.Meta.RecordedBy != recRecordedBy || f.Meta.Measured != "2026-11-18" || f.Meta.Record == nexusfixture.RecordPending {
		t.Errorf("recorded-by %q, measured %q, record %q: not the header of a recording", f.Meta.RecordedBy, f.Meta.Measured, f.Meta.Record)
	}
	if len(f.Exchanges) != exchanges {
		t.Fatalf("%d exchange(s), want %d:\n%s", len(f.Exchanges), exchanges, data)
	}
	for i, ex := range f.Exchanges {
		if ex.Status == 0 {
			t.Errorf("exchange %d has no answer", i+1)
		}
	}
	return f
}

// queryOf parses the raw query of a target.
func queryOf(t *testing.T, target string) (path string, query url.Values) {
	t.Helper()
	path, raw, _ := strings.Cut(target, "?")
	query, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatalf("the query of %q does not parse: %v", target, err)
	}
	return path, query
}

// ---- flow: bearer ----------------------------------------------------------------------

func TestRecordFlowBearerFollowsTheChallengeInThreeExchanges(t *testing.T) {
	for _, id := range []string{"admin", "pull", "anon"} {
		t.Run(id, func(t *testing.T) {
			e, bench := flowEnv(t, bearerConfig{})
			user := map[string]string{"admin": "admin", "pull": "bench-pull", "anon": ""}[id]
			wantBasic := ""
			if user != "" {
				wantBasic = "Basic " + recBasic(user, recPasswords[user])
			}
			path := e.addMeta("catalog-bearer--"+id, []string{recMetaBearer}, "GET", recDockerHost, "/v2/_catalog")

			rep, err := e.record()
			if err != nil {
				t.Fatalf("RecordFixtures: %v", err)
			}
			if want := []string{"catalog/catalog-bearer--" + id}; !slices.Equal(rep.Written, want) || len(rep.Skipped) != 0 {
				t.Errorf("report = %+v, want only %v written: a flow fixture is recorded, not skipped", rep, want)
			}

			// What went over the wire.
			seen := bench.requests()
			if len(seen) != 3 {
				t.Fatalf("the bench saw %d request(s), want 3 (the call, the token, the call with it): %+v", len(seen), seen)
			}
			if seen[0].uri != "/v2/_catalog" || seen[0].header.Get("Authorization") != wantBasic {
				t.Errorf("request 1 = %s with %q, want the call as DevDesk sends it (%q)", seen[0].uri, seen[0].header.Get("Authorization"), wantBasic)
			}
			tokenPath, query := queryOf(t, seen[1].uri)
			if tokenPath != "/v2/token" || query.Get("service") != recRealm || query.Get("scope") != "registry:catalog:*" {
				t.Errorf("request 2 = %s (query %v), want the token request of the challenge", seen[1].uri, query)
			}
			if seen[1].header.Get("Authorization") != wantBasic {
				t.Errorf("the token request carries %q, want the Basic of the identity (%q)", seen[1].header.Get("Authorization"), wantBasic)
			}
			if seen[2].uri != "/v2/_catalog" || seen[2].header.Get("Authorization") != "Bearer "+recFlowToken {
				t.Errorf("request 3 = %s with %q, want the first request again with the Bearer token", seen[2].uri, seen[2].header.Get("Authorization"))
			}

			// What was written.
			f := requireFlowRecorded(t, path, 3, recFlowToken)
			if f.Meta.Flow != nexusfixture.FlowBearer {
				t.Errorf("flow = %q, want it kept", f.Meta.Flow)
			}
			ex1, ex2, ex3 := f.Exchanges[0], f.Exchanges[1], f.Exchanges[2]
			wantAuth := ""
			if user != "" {
				wantAuth = "Basic <redacted>"
			}
			if ex1.Status != 401 || !strings.Contains(ex1.Header.Get("WWW-Authenticate"), `realm="`+recRealm+`"`) {
				t.Errorf("exchange 1 = %d %q, want the 401 and its challenge", ex1.Status, ex1.Header.Get("WWW-Authenticate"))
			}
			tp, q := queryOf(t, ex2.Target)
			if ex2.Host != recDockerHost || tp != "/v2/token" || q.Get("service") != recRealm || q.Get("scope") != "registry:catalog:*" || ex2.Status != 200 {
				t.Errorf("exchange 2 = %s %s on %s -> %d, want the token request on %s", ex2.Method, ex2.Target, ex2.Host, ex2.Status, recDockerHost)
			}
			var tokenBody struct {
				Token     string `json:"token"`
				ExpiresIn int    `json:"expires_in"`
			}
			if err := json.Unmarshal(ex2.Body, &tokenBody); err != nil || tokenBody.Token != "<redacted>" || tokenBody.ExpiresIn != 300 {
				t.Errorf("token body = %q (%v), want the token redacted and expires_in kept", ex2.Body, err)
			}
			if ex3.Target != "/v2/_catalog" || ex3.Status != 200 || string(ex3.Body) != recCatalogBody {
				t.Errorf("exchange 3 = %s -> %d %q, want the catalog", ex3.Target, ex3.Status, ex3.Body)
			}
			for i, want := range []string{wantAuth, wantAuth, "Bearer <redacted>"} {
				if got := f.Exchanges[i].RequestHeader.Get("Authorization"); got != want {
					t.Errorf("exchange %d: Authorization = %q, want %q", i+1, got, want)
				}
			}

			// The token is nowhere.
			requireNoTerm(t, e.dir, recFlowToken)
		})
	}
}

func TestRecordFlowBearerRecordedFileIsUnchangedOnTheNextRun(t *testing.T) {
	e, _ := flowEnv(t, bearerConfig{})
	e.addMeta("catalog-bearer--admin", []string{recMetaBearer}, "GET", recDockerHost, "/v2/_catalog")
	if _, err := e.record(); err != nil {
		t.Fatalf("first run: %v", err)
	}
	after := recSnapshot(t, e.dir)
	rep, err := e.record()
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if want := []string{"catalog/catalog-bearer--admin"}; !slices.Equal(rep.Unchanged, want) || len(rep.Written) != 0 {
		t.Errorf("report = %+v, want %v unchanged: the first exchange of the file is the request, and the three answers are the same", rep, want)
	}
	requireSameFiles(t, after, recSnapshot(t, e.dir))
}

// sameAnswer looks at every exchange: an answer that differs in the second or
// the third one is a new answer.
func TestRecordFlowBearerChangeInALaterExchangeIsANewAnswer(t *testing.T) {
	tests := []struct {
		name   string
		change func(c *bearerConfig)
	}{
		{"the token step", func(c *bearerConfig) { c.expiresIn = 600 }},
		{"the answer with the token", func(c *bearerConfig) { c.catalog = `{"repositories":["api"]}` }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, bench := flowEnv(t, bearerConfig{})
			path := e.addMeta("catalog-bearer--admin", []string{recMetaBearer}, "GET", recDockerHost, "/v2/_catalog")
			if _, err := e.record(); err != nil {
				t.Fatalf("first run: %v", err)
			}
			bench.update(tt.change)
			rep, err := e.record()
			if err != nil {
				t.Fatalf("second run: %v", err)
			}
			if want := []string{"catalog/catalog-bearer--admin"}; !slices.Equal(rep.Written, want) || len(rep.Unchanged) != 0 {
				t.Errorf("report = %+v, want it written again: %s changed", rep, tt.name)
			}
			requireFlowRecorded(t, path, 3, recFlowToken)
		})
	}
}

func TestRecordFlowBearerStopsAtARealmItDoesNotFollow(t *testing.T) {
	other, hits := hitCounter(t)
	tests := []struct{ name, realm, note string }{
		{"another origin on the loopback", other.URL + "/v2/token", recNoteRealm},
		{"the reserved probe host", "http://realm-probe.invalid/v2/token", recNoteRealm},
		{"the right origin and a path that is no token path", "http://127.0.0.1:30482/service/rest/v1/security/users", recNoteRealmNF},
		{"the right origin and the token path with a suffix", "http://127.0.0.1:30482/v2/token/extra", recNoteRealmNF},
		{"the right origin and another scheme", "ftp://127.0.0.1:30482/v2/token", recNoteRealmNF},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hits.Store(0)
			e, bench := flowEnv(t, bearerConfig{realm: tt.realm})
			path := e.addMeta("catalog-bearer--admin", []string{recMetaBearer}, "GET", recDockerHost, "/v2/_catalog")

			rep, err := e.record()
			if err != nil {
				t.Fatalf("RecordFixtures: %v", err)
			}
			if want := []string{"catalog/catalog-bearer--admin"}; !slices.Equal(rep.Written, want) {
				t.Errorf("Written = %v, want %v: not following is a measurement, not a failure", rep.Written, want)
			}
			if got := len(bench.requests()); got != 1 {
				t.Errorf("the bench saw %d request(s), want only the first one", got)
			}
			if n := hits.Load(); n != 0 {
				t.Errorf("the other origin received %d request(s), with the credentials of the bench", n)
			}
			f := requireFlowRecorded(t, path, 1, recFlowToken)
			if f.Exchanges[0].Status != 401 {
				t.Errorf("status = %d, want the 401 alone", f.Exchanges[0].Status)
			}
			if data := string(mustRead(t, path)); !strings.Contains(data, tt.note) {
				t.Errorf("the file does not carry the note %q:\n%s", tt.note, data)
			} else if tt.note == recNoteRealmNF && strings.Contains(data, recNoteRealm) {
				t.Errorf("the file carries the off-origin note %q for a realm on an origin of the run:\n%s", recNoteRealm, data)
			}
			requireNoTerm(t, e.dir, recFlowToken)
		})
	}
}

func TestRecordFlowBearerStopsWhenTheTokenIsRefused(t *testing.T) {
	e, bench := flowEnv(t, bearerConfig{})
	path := e.addMeta("catalog-bearer--badcred", []string{recMetaBearer}, "GET", recDockerHost, "/v2/_catalog")

	rep, err := e.record()
	if err != nil {
		t.Fatalf("RecordFixtures: %v", err)
	}
	if want := []string{"catalog/catalog-bearer--badcred"}; !slices.Equal(rep.Written, want) {
		t.Errorf("Written = %v, want %v", rep.Written, want)
	}
	seen := bench.requests()
	if len(seen) != 2 {
		t.Fatalf("the bench saw %d request(s), want 2 (the call, the refused token): %+v", len(seen), seen)
	}
	wantBasic := "Basic " + recBasic("admin", fakenexus.BadPassword)
	for i, s := range seen {
		if got := s.header.Get("Authorization"); got != wantBasic {
			t.Errorf("request %d carries %q, want Basic admin with %q", i+1, got, fakenexus.BadPassword)
		}
	}
	f := requireFlowRecorded(t, path, 2, recFlowToken)
	if f.Exchanges[0].Status != 401 || f.Exchanges[1].Status != 401 {
		t.Errorf("statuses = %d, %d, want the challenge and the refused token, both 401", f.Exchanges[0].Status, f.Exchanges[1].Status)
	}
	if tp, _ := queryOf(t, f.Exchanges[1].Target); tp != "/v2/token" {
		t.Errorf("exchange 2 = %s, want the token request", f.Exchanges[1].Target)
	}
}

func TestRecordFlowBearerWithNoChallengeIsOneExchangeAndANote(t *testing.T) {
	e, bench := flowEnv(t, bearerConfig{noChallenge: true})
	path := e.addMeta("catalog-bearer--admin", []string{recMetaBearer}, "GET", recDockerHost, "/v2/_catalog")
	rep, err := e.record()
	if err != nil {
		t.Fatalf("RecordFixtures: %v", err)
	}
	if want := []string{"catalog/catalog-bearer--admin"}; !slices.Equal(rep.Written, want) {
		t.Errorf("Written = %v, want %v", rep.Written, want)
	}
	if got := len(bench.requests()); got != 1 {
		t.Errorf("the bench saw %d request(s), want 1: nothing asked for a token", got)
	}
	f := requireFlowRecorded(t, path, 1, recFlowToken)
	if f.Exchanges[0].Status != 200 {
		t.Errorf("status = %d, want the 200 the bench gave", f.Exchanges[0].Status)
	}
	if data := string(mustRead(t, path)); !strings.Contains(data, recNoteNoChall) {
		t.Errorf("the file does not carry the note %q:\n%s", recNoteNoChall, data)
	}
}

// A 401 that is not a Bearer challenge is not followed either: it has no realm.
func TestRecordFlowBearerWithABasicChallengeIsNotFollowed(t *testing.T) {
	e := newRecEnv(t)
	e.docker.answer("admin", "/v2/_catalog", recAnswer{
		status: http.StatusUnauthorized, contentType: "application/json",
		header: http.Header{"Www-Authenticate": {`Basic realm="Sonatype Nexus Repository Manager"`}},
		body:   `{"errors":[{"code":"UNAUTHORIZED"}]}`,
	})
	path := e.addMeta("catalog-bearer--admin", []string{recMetaBearer}, "GET", recDockerHost, "/v2/_catalog")
	if _, err := e.record(); err != nil {
		t.Fatalf("RecordFixtures: %v", err)
	}
	requireFlowRecorded(t, path, 1)
	if got := len(e.docker.requests()); got != 1 {
		t.Errorf("the bench saw %d request(s), want 1", got)
	}
}

// ---- the token never reaches a file or an output ---------------------------------------------

func TestRecordFlowBearerRefusesAnAnswerThatEchoesTheToken(t *testing.T) {
	tests := []struct {
		name string
		cfg  bearerConfig
	}{
		{"in a field of the body", bearerConfig{echoBody: true}},
		{"in a header the fixture drops", bearerConfig{echoHeader: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, _ := flowEnv(t, tt.cfg)
			e.addGood()
			e.addMeta("z-bad--admin", []string{recMetaBearer}, "GET", recDockerHost, "/v2/_catalog")
			be := e.refuse("z-bad--admin")
			requireNoTermIn(t, "the error", be.Error(), recFlowToken)
			requireNoTerm(t, e.dir, recFlowToken)
		})
	}
}

func TestRecordFlowBearerNeverPrintsTheTokenOnSuccess(t *testing.T) {
	e, _ := flowEnv(t, bearerConfig{})
	e.addMeta("catalog-bearer--admin", []string{recMetaBearer}, "GET", recDockerHost, "/v2/_catalog")
	rep, err := e.record()
	if err != nil {
		t.Fatalf("RecordFixtures: %v", err)
	}
	for _, list := range [][]string{rep.Written, rep.Unchanged, rep.Skipped, rep.CacheEffects} {
		requireNoTermIn(t, "the report", strings.Join(list, "\n"), recFlowToken)
	}
}

// ---- flow: redirects ----------------------------------------------------------------------------

func TestRecordFlowRedirectsFollowsASameOriginChain(t *testing.T) {
	e := newRecEnv(t)
	hop1 := "http://127.0.0.1:30482/v2/docker-hosted/_catalog"
	hop2 := "http://127.0.0.1:30482/v2/_catalog?n=1000"
	e.docker.answer("admin", "/v2/_catalog", recAnswer{status: http.StatusFound, header: http.Header{"Location": {hop1}}})
	e.docker.answer("admin", "/v2/docker-hosted/_catalog", recAnswer{status: http.StatusTemporaryRedirect, header: http.Header{"Location": {hop2}}})
	e.docker.answer("admin", "/v2/_catalog?n=1000", recAnswer{contentType: "application/json", body: recCatalogBody})
	path := e.addMeta("moved-twice--admin", []string{recMetaRedirects}, "GET", recDockerHost, "/v2/_catalog")

	rep, err := e.record()
	if err != nil {
		t.Fatalf("RecordFixtures: %v", err)
	}
	if want := []string{"catalog/moved-twice--admin"}; !slices.Equal(rep.Written, want) {
		t.Errorf("Written = %v, want %v", rep.Written, want)
	}
	seen := e.docker.requests()
	if len(seen) != 3 {
		t.Fatalf("the bench saw %d request(s), want 3 (the call and its two hops): %+v", len(seen), seen)
	}
	for i, wantURI := range []string{"/v2/_catalog", "/v2/docker-hosted/_catalog", "/v2/_catalog?n=1000"} {
		if seen[i].uri != wantURI || seen[i].user != "admin" || seen[i].method != "GET" {
			t.Errorf("request %d = %s %s as %q, want GET %s as admin: a hop of the same origin carries the credentials", i+1, seen[i].method, seen[i].uri, seen[i].user, wantURI)
		}
	}

	f := requireFlowRecorded(t, path, 3)
	if f.Meta.Flow != nexusfixture.FlowRedirects {
		t.Errorf("flow = %q, want it kept", f.Meta.Flow)
	}
	for i, want := range []struct {
		target string
		status int
	}{{"/v2/_catalog", 302}, {"/v2/docker-hosted/_catalog", 307}, {"/v2/_catalog?n=1000", 200}} {
		ex := f.Exchanges[i]
		if ex.Target != want.target || ex.Status != want.status || ex.Host != recDockerHost || ex.RequestHeader.Get("Authorization") != "Basic <redacted>" {
			t.Errorf("exchange %d = %s on %s -> %d (%q), want %s on %s -> %d with the placeholder", i+1, ex.Target, ex.Host, ex.Status, ex.RequestHeader.Get("Authorization"), want.target, recDockerHost, want.status)
		}
	}
	if got := f.Exchanges[0].Header.Get("Location"); got != hop1 {
		t.Errorf("Location of exchange 1 = %q, want %q kept", got, hop1)
	}

	// The next run reads the same chain: unchanged.
	after := recSnapshot(t, e.dir)
	rep, err = e.record()
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if want := []string{"catalog/moved-twice--admin"}; !slices.Equal(rep.Unchanged, want) {
		t.Errorf("second run: %+v, want %v unchanged", rep, want)
	}
	requireSameFiles(t, after, recSnapshot(t, e.dir))
}

func TestRecordFlowRedirectsDoesNotLeaveTheOrigin(t *testing.T) {
	other, hits := hitCounter(t)
	// The REST origin is an origin of the run, and still not the one of the request.
	restLocation := "http://" + recRESTHost + "/v2/_catalog"
	for _, location := range []string{other.URL + "/v2/_catalog", "http://realm-probe.invalid/v2/_catalog", restLocation} {
		t.Run(location, func(t *testing.T) {
			hits.Store(0)
			e := newRecEnv(t)
			e.docker.answer("admin", "/v2/_catalog", recAnswer{status: http.StatusFound, header: http.Header{"Location": {location}}})
			path := e.addMeta("moved-away--admin", []string{recMetaRedirects}, "GET", recDockerHost, "/v2/_catalog")

			rep, err := e.record()
			if err != nil {
				t.Fatalf("RecordFixtures: %v", err)
			}
			if want := []string{"catalog/moved-away--admin"}; !slices.Equal(rep.Written, want) {
				t.Errorf("Written = %v, want %v: not following is a measurement", rep.Written, want)
			}
			if n := hits.Load(); n != 0 {
				t.Errorf("the other origin received %d request(s), with the bench credentials", n)
			}
			if got := len(e.docker.requests()); got != 1 {
				t.Errorf("the bench saw %d request(s), want 1", got)
			}
			if got := len(e.rest.fixtureRequests()); got != 0 {
				t.Errorf("the REST origin received %d request(s), with the bench credentials", got)
			}
			f := requireFlowRecorded(t, path, 1)
			if f.Exchanges[0].Status != http.StatusFound || f.Exchanges[0].Header.Get("Location") != location {
				t.Errorf("exchange = %d, Location %q, want the redirect itself, kept", f.Exchanges[0].Status, f.Exchanges[0].Header.Get("Location"))
			}
			if data := string(mustRead(t, path)); !strings.Contains(data, recNoteRedir) {
				t.Errorf("the file does not carry the note %q:\n%s", recNoteRedir, data)
			}
		})
	}
}

func TestRecordFlowRedirectsStopsAtFiveHopsAndWritesNothing(t *testing.T) {
	e := newRecEnv(t)
	e.addGood()
	self := "http://127.0.0.1:30482/v2/_catalog"
	e.docker.answer("admin", "/v2/_catalog", recAnswer{status: http.StatusFound, header: http.Header{"Location": {self}}})
	e.addMeta("z-loop--admin", []string{recMetaRedirects}, "GET", recDockerHost, "/v2/_catalog")

	be := e.refuse("z-loop--admin")
	if !strings.Contains(strings.ToLower(be.Error()), "redirect") {
		t.Errorf("the error %q does not say it is about redirects", be.Error())
	}
	// The call and five hops at most: a sixth redirect is the error.
	if got := len(e.docker.requests()); got < 5 || got > 6 {
		t.Errorf("the bench saw %d request(s), want the call and up to 5 hops (5 or 6)", got)
	}
}

func TestRecordFlowRedirectsOnAnAnswerThatDoesNotMoveIsOneExchange(t *testing.T) {
	e := newRecEnv(t)
	e.docker.answer("admin", "/v2/_catalog", recAnswer{contentType: "application/json", body: recCatalogBody})
	path := e.addMeta("not-moved--admin", []string{recMetaRedirects}, "GET", recDockerHost, "/v2/_catalog")
	if _, err := e.record(); err != nil {
		t.Fatalf("RecordFixtures: %v", err)
	}
	requireFlowRecorded(t, path, 1)
	if data := string(mustRead(t, path)); strings.Contains(data, "not followed") {
		t.Errorf("the file carries a note about a redirect that never happened:\n%s", data)
	}
}
