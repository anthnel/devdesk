package nexusbench

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/nexusfixture"
)

// The tests of the fixture recorder (DEVDESK-511, step 7; Plane DEVDESK-574).
// RecordFixtures regenerates the recorded Nexus answers of internal/nexusfixture
// from the bench, and it sends the bench credentials to do so: every guard of
// the plan has a test here, and every test runs against an httptest server on
// the loopback that stands in for the bench. Nothing here reaches a cluster, the
// secrets file or the versioned descriptor.
//
// The contract these tests fix (the plan names the types and the function; the
// rest is the simplest reading of its "Step 7" and of the task of DEVDESK-574):
//
//   - the request to replay is read from the fixture file itself, including a
//     request-only file ("# record: pending"); the file is first scanned with
//     nexusfixture.CheckLeaks, so a request header outside Host, Authorization
//     and Accept, or a query other than n and last, refuses it;
//   - everything is validated before the first request: the origins (each must
//     be a loopback URL, a key must be a host:port of the descriptor, a fixture
//     host must have an origin), the method (GET), the target (closed list of
//     path shapes, no query but n and last, no "..", never /manifests/ or
//     /blobs/), the credentials of each identity;
//   - then the data set is compared with the descriptor, anonymously, on the
//     origin of the descriptor's REST host:port (RecordOptions.Descriptor.Hosts[0]
//     and REST.NodePort), through ListImagesAnonymously: a listing that differs
//     or cannot be read refuses the run (unknown is a refusal, as nothing says
//     the bench still holds the data set), before any fixture request;
//   - then every fixture is requested once (the exchange method of the client,
//     the only sender of credentials), the answer is searched for the real
//     passwords (raw, any header or body), redacted, written to text, parsed
//     again and checked with CheckLeaks (exact terms: both passwords and both
//     Basic encodings, plus DenyTerms), and only when every fixture passed are
//     the files written, atomically;
//   - every error is a *BenchError (Cause, Check, Err kept for errors.As) that
//     names the fixture and, for a leak, the rule, and never a secret;
//   - report entries are "<campaign>/<fixture name>", Skipped ones are followed
//     by ": <reason>".
//
// Ambiguities settled here, to be confirmed at review: the dataset check lives
// in the library (the plan puts it in the command, step 8); the listing is
// anonymous, so a bench that disabled anonymous access cannot be recorded; Dir
// empty means nexusfixture.SourceDir(); the HTTP client is not injectable
// (guard 1 of guard_test.go forces newClient, and the httptest origin is the
// injection point).

const (
	recAdminPW = "rec-admin-Qx7vK2mP"
	recPullPW  = "rec-pull-Zr4tN8wL"
	recServer  = "Nexus/3.98.1-01 (COMMUNITY)"

	recRESTHost   = "127.0.0.1:30481"
	recDockerHost = "127.0.0.1:30482"

	recCatalogBody = `{"repositories":["api","mirror/cached","mirror/uncached","shared/base","team/api","tools/cli"]}`
	recGroupTarget = "/service/rest/v1/repositories/docker/group/docker-group"
	recGoodTarget  = "/service/rest/v1/repositories"

	recRecordedBy = "nexus-bench record"
)

var (
	recNow = time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	// recOld is the modification time every fixture of a test starts with, so a
	// rewrite shows.
	recOld = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

	recPasswords = map[string]string{"admin": recAdminPW, "bench-pull": recPullPW}
)

func recBasic(user, password string) string {
	return base64.StdEncoding.EncodeToString([]byte(user + ":" + password))
}

// recExact is what the recorder must search every answer for.
func recExact() []string {
	return []string{recAdminPW, recPullPW, recBasic("admin", recAdminPW), recBasic("bench-pull", recPullPW)}
}

// ---- the fake bench -----------------------------------------------------------

type recSeen struct {
	method, uri, user string
	header            http.Header
}

type recAnswer struct {
	status      int
	contentType string
	header      http.Header
	body        string
}

// recBench is one origin of the bench: an httptest server that answers what a
// test registered, keyed by account and request URI ("" for anonymous), checks
// the Basic credentials it receives against the passwords of recPasswords, adds
// to every answer a session cookie and a header the fixtures must drop, and
// remembers every request.
type recBench struct {
	srv     *httptest.Server
	mu      sync.Mutex
	seen    []recSeen
	answers map[string]recAnswer
	server  string
}

func newRecBench(t *testing.T) *recBench {
	t.Helper()
	b := &recBench{answers: map[string]recAnswer{}, server: recServer}
	b.srv = httptest.NewServer(http.HandlerFunc(b.serve))
	t.Cleanup(b.srv.Close)
	return b
}

func (b *recBench) serve(w http.ResponseWriter, r *http.Request) {
	user, pass, basic := r.BasicAuth()
	b.mu.Lock()
	b.seen = append(b.seen, recSeen{method: r.Method, uri: r.RequestURI, user: user, header: r.Header.Clone()})
	answer, found := b.answers[user+" "+r.RequestURI]
	server := b.server
	b.mu.Unlock()

	h := w.Header()
	if r.Header.Get("Authorization") != "" && (!basic || pass != recPasswords[user]) {
		// Nexus names itself on a refusal too (the NX-03 401s carry Server).
		if server != "" {
			h.Set("Server", server)
		}
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if server != "" {
		h.Set("Server", server)
	}
	h.Set("Set-Cookie", "NXSESSIONID=rec-session; Path=/")
	h.Set("X-Content-Type-Options", "nosniff")
	if !found {
		answer = recAnswer{status: http.StatusNotFound, contentType: "application/json", body: `{"message":"no answer registered"}`}
	}
	if answer.contentType != "" {
		h.Set("Content-Type", answer.contentType)
	}
	for k, v := range answer.header {
		h[k] = v
	}
	if answer.status == 0 {
		answer.status = http.StatusOK
	}
	w.WriteHeader(answer.status)
	_, _ = w.Write([]byte(answer.body))
}

func (b *recBench) answer(user, uri string, a recAnswer) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.answers[user+" "+uri] = a
}

func (b *recBench) setServer(v string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.server = v
}

func (b *recBench) requests() []recSeen {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.seen)
}

// fixtureRequests is what the bench saw besides the listings of the data set.
func (b *recBench) fixtureRequests() []recSeen {
	var out []recSeen
	for _, s := range b.requests() {
		if !strings.HasPrefix(s.uri, componentsPath) {
			out = append(out, s)
		}
	}
	return out
}

// recListing is what docker-hosted or docker-proxy list when the bench holds
// the data set the descriptor describes.
func recListing(repo string) []ListedImage {
	var out []ListedImage
	for _, img := range Dataset() {
		hosted := repo == dockerHostedRepository && img.Location == LocationHosted
		cached := repo == dockerProxyRepository && img.PullThrough
		if hosted || cached {
			out = append(out, ListedImage{Name: img.Name, Tag: img.Tag, Digest: img.Digest})
		}
	}
	return out
}

func (b *recBench) serveListing(repo string, images []ListedImage) {
	b.serveListingFor("", repo, images)
}

// serveListingFor answers the listing of repo to one account ("" is anonymous).
func (b *recBench) serveListingFor(user, repo string, images []ListedImage) {
	items := []map[string]any{}
	for _, img := range images {
		assets := []map[string]any{}
		if img.Digest != "" {
			assets = append(assets, map[string]any{
				"path":     "v2/" + img.Name + "/manifests/" + img.Tag,
				"checksum": map[string]string{"sha256": strings.TrimPrefix(img.Digest, "sha256:")},
			})
		}
		items = append(items, map[string]any{"name": img.Name, "version": img.Tag, "assets": assets})
	}
	body, err := json.Marshal(map[string]any{"items": items, "continuationToken": nil})
	if err != nil {
		panic(err)
	}
	b.answer(user, componentsPath+"?repository="+repo, recAnswer{contentType: "application/json", body: string(body)})
}

func (b *recBench) serveDataset() {
	for _, repo := range []string{dockerHostedRepository, dockerProxyRepository} {
		b.serveListing(repo, recListing(repo))
	}
}

// ---- the environment ----------------------------------------------------------

func recDescriptor() Descriptor {
	return BuildDescriptor(Defaults(), DeployResult{},
		ProvisionResult{NexusVersion: "3.98.1-01", Edition: "COMMUNITY", EULAAccepted: true},
		SeedResult{Images: Dataset()})
}

type recEnv struct {
	t            *testing.T
	dir          string
	rest, docker *recBench
	opts         RecordOptions
}

func newRecEnv(t *testing.T) *recEnv {
	t.Helper()
	e := &recEnv{t: t, dir: t.TempDir(), rest: newRecBench(t), docker: newRecBench(t)}
	e.rest.serveDataset()
	e.opts = RecordOptions{
		Origins:    map[string]string{recRESTHost: e.rest.srv.URL, recDockerHost: e.docker.srv.URL},
		Descriptor: recDescriptor(),
		Secrets:    Secrets{AdminPassword: recAdminPW, PullPassword: recPullPW},
		Dir:        e.dir,
		Now:        func() time.Time { return recNow },
	}
	return e
}

const recPendingHeader = "# scenario: recorder test\n# nexus: 3.90.0-02 COMMUNITY (stale note)\n# measured: 2026-11-01\n" +
	"# account: recorder test account\n# redacted: recorder test redaction\n"

// recRequest is a request-only fixture text.
func recRequest(method, host, target string, auth bool, extra ...string) string {
	var b strings.Builder
	b.WriteString(recPendingHeader + "# record: pending\n\n")
	b.WriteString(method + " " + target + " HTTP/1.1\nHost: " + host + "\n")
	if auth {
		b.WriteString("Authorization: Basic <redacted>\n")
	}
	b.WriteString("Accept: application/json\n")
	for _, line := range extra {
		b.WriteString(line + "\n")
	}
	return b.String()
}

// add writes a fixture file and gives it the old modification time.
func (e *recEnv) add(campaign, name, content string) string {
	e.t.Helper()
	dir := filepath.Join(e.dir, campaign)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		e.t.Fatal(err)
	}
	path := filepath.Join(dir, name+".http")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		e.t.Fatal(err)
	}
	if err := os.Chtimes(path, recOld, recOld); err != nil {
		e.t.Fatal(err)
	}
	return path
}

// pending adds a GET request-only fixture of the catalog campaign; the identity
// is the suffix of the name, as for a real fixture.
func (e *recEnv) pending(name, host, target string) string {
	e.t.Helper()
	return e.add("catalog", name, recRequest("GET", host, target, !strings.HasSuffix(name, "--anon")))
}

// addGood adds a fixture that sorts first and would record without a problem, to
// show that a refusal on another fixture leaves it alone (all or nothing).
func (e *recEnv) addGood() string {
	e.t.Helper()
	e.rest.answer("admin", recGoodTarget, recAnswer{contentType: "application/json", body: `[]`})
	return e.pending("a-good--admin", recRESTHost, recGoodTarget)
}

func (e *recEnv) record() (RecordReport, error) {
	return RecordFixtures(context.Background(), e.opts)
}

func (e *recEnv) requireNoRequest() {
	e.t.Helper()
	for name, b := range map[string]*recBench{"rest": e.rest, "docker": e.docker} {
		if got := b.requests(); len(got) != 0 {
			e.t.Errorf("the %s origin received %d request(s), want none: the run had to be refused first (first: %s %s)",
				name, len(got), got[0].method, got[0].uri)
		}
	}
}

func (e *recEnv) requireNoFixtureRequest() {
	e.t.Helper()
	if got := len(e.rest.fixtureRequests()) + len(e.docker.requests()); got != 0 {
		e.t.Errorf("%d fixture request(s) were sent, want none", got)
	}
}

// refuse runs the recorder and requires a refusal that names fixture (when not
// empty) and changes no file.
func (e *recEnv) refuse(fixture string) *BenchError {
	e.t.Helper()
	before := recSnapshot(e.t, e.dir)
	rep, err := e.record()
	be := requireRecordRefusal(e.t, err)
	if fixture != "" && !strings.Contains(err.Error(), fixture) {
		e.t.Errorf("the error %q does not name the fixture %q", err, fixture)
	}
	if len(rep.Written) != 0 {
		e.t.Errorf("Written = %v after a refusal, want none", rep.Written)
	}
	requireSameFiles(e.t, before, recSnapshot(e.t, e.dir))
	return be
}

// ---- helpers ------------------------------------------------------------------

func requireRecordRefusal(t *testing.T, err error) *BenchError {
	t.Helper()
	if err == nil {
		t.Fatal("RecordFixtures succeeded, want a refusal")
	}
	var be *BenchError
	if !errors.As(err, &be) {
		t.Fatalf("error %T is not a *BenchError: %v", err, err)
	}
	if be.Cause == "" || be.Check == "" {
		t.Errorf("the BenchError is incomplete: cause %q, check %q", be.Cause, be.Check)
	}
	for _, secret := range recExact() {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("the error text carries a secret: %q", err)
		}
	}
	return be
}

type recFile struct {
	data []byte
	mod  time.Time
}

func recSnapshot(t *testing.T, dir string) map[string]recFile {
	t.Helper()
	out := map[string]recFile{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path) //nolint:gosec // a file of the test's own temporary folder
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out[path] = recFile{data: data, mod: info.ModTime()}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func requireSameFiles(t *testing.T, before, after map[string]recFile) {
	t.Helper()
	for path, was := range before {
		now, ok := after[path]
		switch {
		case !ok:
			t.Errorf("%s disappeared", path)
		case !bytes.Equal(was.data, now.data):
			t.Errorf("%s was rewritten:\n%s", path, now.data)
		case !was.mod.Equal(now.mod):
			t.Errorf("%s was touched (modification time %v, was %v)", path, now.mod, was.mod)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			t.Errorf("%s appeared (a temporary file left behind?)", path)
		}
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // a file of the test's own temporary folder
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func parseRecorded(t *testing.T, path string) nexusfixture.Fixture {
	t.Helper()
	data := mustRead(t, path)
	f, err := nexusfixture.Parse("catalog", filepath.Base(path), data)
	if err != nil {
		t.Fatalf("the recorded file does not parse: %v\n%s", err, data)
	}
	return f
}

// requireRecorded checks a file the recorder wrote: it reads back, writes back
// to the same bytes, leaks nothing and says who wrote it and when.
func requireRecorded(t *testing.T, path, measured string) nexusfixture.Fixture {
	t.Helper()
	data := mustRead(t, path)
	f := parseRecorded(t, path)
	if got := nexusfixture.Format(f); !bytes.Equal(got, data) {
		t.Errorf("Format(Parse(file)) differs from the file:\n%s\n--- want\n%s", got, data)
	}
	if leaks := nexusfixture.CheckLeaks(f.Name, data, recExact()); len(leaks) != 0 {
		t.Errorf("the recorded file leaks: %+v", leaks)
	}
	m := f.Meta
	if m.RecordedBy != recRecordedBy {
		t.Errorf("recorded-by = %q, want %q", m.RecordedBy, recRecordedBy)
	}
	if m.Measured != measured {
		t.Errorf("measured = %q, want %q", m.Measured, measured)
	}
	if m.NexusVersion != "3.98.1-01" || m.Edition != "COMMUNITY" {
		t.Errorf("nexus = %s %s, want 3.98.1-01 COMMUNITY", m.NexusVersion, m.Edition)
	}
	if m.Record == nexusfixture.RecordPending {
		t.Error("the fixture is still pending")
	}
	if len(f.Exchanges) != 1 || f.Exchanges[0].Status == 0 {
		t.Fatalf("exchanges = %+v, want one with an answer", f.Exchanges)
	}
	return f
}

func recSorted(in []string) []string {
	out := slices.Clone(in)
	sort.Strings(out)
	return out
}

// ---- what a recording is -------------------------------------------------------

func TestRecordFillsAPendingFixture(t *testing.T) {
	e := newRecEnv(t)
	e.docker.answer("admin", "/v2/_catalog", recAnswer{
		contentType: "application/json",
		header:      http.Header{"Docker-Distribution-Api-Version": {"registry/2.0"}},
		body:        recCatalogBody,
	})
	path := e.pending("catalog-group--admin", recDockerHost, "/v2/_catalog")

	rep, err := e.record()
	if err != nil {
		t.Fatalf("RecordFixtures: %v", err)
	}
	if want := []string{"catalog/catalog-group--admin"}; !slices.Equal(rep.Written, want) || len(rep.Unchanged) != 0 || len(rep.Skipped) != 0 {
		t.Errorf("report = %+v, want only %v written", rep, want)
	}
	f := requireRecorded(t, path, "2026-11-18")
	ex := f.Exchanges[0]
	if ex.Status != 200 || string(ex.Body) != recCatalogBody {
		t.Errorf("answer = %d %q, want 200 and the catalog", ex.Status, ex.Body)
	}
	if ex.Method != "GET" || ex.Target != "/v2/_catalog" || ex.Host != recDockerHost {
		t.Errorf("request = %s %s on %s, want the one of the file", ex.Method, ex.Target, ex.Host)
	}
	if got := ex.RequestHeader.Get("Authorization"); got != "Basic <redacted>" {
		t.Errorf("request credentials = %q, want the placeholder", got)
	}
	if got := ex.Header.Get("Server"); got != recServer {
		t.Errorf("Server = %q, want %q", got, recServer)
	}
	if got := ex.Header.Get("Docker-Distribution-Api-Version"); got != "registry/2.0" {
		t.Errorf("Docker-Distribution-Api-Version = %q, want registry/2.0", got)
	}
	for name := range ex.Header {
		if !slices.ContainsFunc(nexusfixture.ResponseHeaderAllowlist, func(a string) bool { return http.CanonicalHeaderKey(a) == name }) {
			t.Errorf("the fixture keeps the header %s, which is not on the allowlist", name)
		}
	}
	if f.Meta.Scenario != "recorder test" || f.Meta.Account != "recorder test account" || f.Meta.Redacted != "recorder test redaction" {
		t.Errorf("the free text of the header was not kept: %+v", f.Meta)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if runtime.GOOS != "windows" && info.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v, want 0644", info.Mode().Perm())
	}
	// What was written loads like any embedded campaign.
	if loaded, err := nexusfixture.LoadFS(os.DirFS(e.dir), "catalog"); err != nil || len(loaded) != 1 {
		t.Errorf("LoadFS = %d fixtures, %v, want the recorded one", len(loaded), err)
	}
}

func TestRecordedByIsSetByTheRecorderOnly(t *testing.T) {
	e := newRecEnv(t)
	e.docker.answer("admin", "/v2/_catalog", recAnswer{contentType: "application/json", body: recCatalogBody})
	text := strings.Replace(recRequest("GET", recDockerHost, "/v2/_catalog", true), "# record: pending\n", "# recorded-by: written by hand\n# record: pending\n", 1)
	path := e.add("catalog", "claimed--admin", text)
	if _, err := e.record(); err != nil {
		t.Fatalf("RecordFixtures: %v", err)
	}
	requireRecorded(t, path, "2026-11-18")
}

func TestRecordSendsTheRequestOfTheFixtureAndNothingElse(t *testing.T) {
	e := newRecEnv(t)
	for _, user := range []string{"admin", "bench-pull", ""} {
		status, body := 200, `{"name":"docker-group"}`
		switch user {
		case "bench-pull":
			status, body = 404, `{"message":"Repository not found"}`
		case "":
			status, body = 403, ""
		}
		e.rest.answer(user, recGroupTarget, recAnswer{status: status, contentType: "application/json", body: body})
	}
	for _, id := range []string{"admin", "pull", "anon"} {
		e.pending("rest-group--"+id, recRESTHost, recGroupTarget)
	}
	if _, err := e.record(); err != nil {
		t.Fatalf("RecordFixtures: %v", err)
	}

	got := map[string]recSeen{}
	for _, s := range e.rest.fixtureRequests() {
		if _, dup := got[s.user]; dup {
			t.Errorf("the account %q was asked twice", s.user)
		}
		got[s.user] = s
	}
	if len(got) != 3 {
		t.Fatalf("the bench saw requests as %v, want admin, bench-pull and anonymous once each", got)
	}
	for user, s := range got {
		if s.method != "GET" || s.uri != recGroupTarget {
			t.Errorf("%q sent %s %s, want GET %s", user, s.method, s.uri, recGroupTarget)
		}
		if s.header.Get("Accept") != "application/json" {
			t.Errorf("%q: the Accept header of the fixture was not sent", user)
		}
		if s.header.Get("Cookie") != "" {
			t.Errorf("%q sent a cookie", user)
		}
		auth := s.header.Values("Authorization")
		switch {
		case user == "" && len(auth) != 0:
			t.Errorf("the anonymous fixture was sent with credentials: %d header(s)", len(auth))
		case user != "" && (len(auth) != 1 || strings.Contains(auth[0], "<redacted>")):
			t.Errorf("%q: credentials = %v, want exactly one real Basic header, not the placeholder", user, auth)
		}
	}
	for name, status := range map[string]int{"rest-group--admin": 200, "rest-group--pull": 404, "rest-group--anon": 403} {
		f := parseRecorded(t, filepath.Join(e.dir, "catalog", name+".http"))
		if f.Exchanges[0].Status != status {
			t.Errorf("%s: status %d, want %d", name, f.Exchanges[0].Status, status)
		}
	}
	// The data set is read with no credential at all.
	listings := 0
	for _, s := range e.rest.requests() {
		if strings.HasPrefix(s.uri, componentsPath) {
			listings++
			if s.method != "GET" || s.header.Get("Authorization") != "" {
				t.Errorf("the data set listing was %s with credentials %q", s.method, s.header.Get("Authorization"))
			}
		}
	}
	if listings != 2 {
		t.Errorf("%d listing(s) of the data set, want docker-hosted and docker-proxy", listings)
	}
	if got := e.docker.requests(); len(got) != 0 {
		t.Errorf("the docker origin received %d request(s) though no fixture is recorded there", len(got))
	}
}

func TestRecordKeepsTheBodyOfAnErrorAnswer(t *testing.T) {
	e := newRecEnv(t)
	body := "{\n  \"id\" : \"*\",\n  \"message\" : \"\\\"Repository not found\\\"\"\n}"
	e.rest.answer("bench-pull", recGroupTarget, recAnswer{status: 404, contentType: "application/json", body: body})
	path := e.pending("rest-group--pull", recRESTHost, recGroupTarget)
	if _, err := e.record(); err != nil {
		t.Fatalf("RecordFixtures: %v", err)
	}
	ex := requireRecorded(t, path, "2026-11-18").Exchanges[0]
	if ex.Status != 404 || string(ex.Body) != body {
		t.Errorf("answer = %d %q, want the 404 and its body kept as sent", ex.Status, ex.Body)
	}
}

func TestRecordKeepsTheContentLengthOfARedactedBody(t *testing.T) {
	e := newRecEnv(t)
	body := `{"tokenType":"Bearer","token":"abcdefghijklmnopqrstuvwxyz"}`
	e.docker.answer("admin", "/v2/_catalog", recAnswer{contentType: "application/json", body: body})
	path := e.pending("catalog--admin", recDockerHost, "/v2/_catalog")
	if _, err := e.record(); err != nil {
		t.Fatalf("RecordFixtures: %v", err)
	}
	ex := requireRecorded(t, path, "2026-11-18").Exchanges[0]
	if !strings.Contains(string(ex.Body), `"<redacted>"`) || strings.Contains(string(ex.Body), "abcdefghijkl") {
		t.Fatalf("body = %q, want the token redacted", ex.Body)
	}
	// TestVerbatimBodiesMatchTheirContentLength reads every embedded fixture:
	// a length that is still the one of the unredacted body would turn it red.
	if cl := ex.Header.Get("Content-Length"); cl != "" && cl != strconv.Itoa(len(ex.Body)) {
		t.Errorf("Content-Length = %s but the written body has %d bytes", cl, len(ex.Body))
	}
}

// The recorder keeps what the bench sends. Left alone, the transport asks for
// gzip, decodes the answer and drops its Content-Length, which is not what a
// fixture holds: the recorder asks for no compression.
func TestRecordAsksForNoCompression(t *testing.T) {
	e := newRecEnv(t)
	gz := newGzipBench(t, recCatalogBody)
	e.opts.Origins[recDockerHost] = gz.srv.URL
	path := e.pending("catalog--admin", recDockerHost, "/v2/_catalog")
	if _, err := e.record(); err != nil {
		t.Fatalf("RecordFixtures: %v", err)
	}
	if got := gz.encodings(); len(got) != 1 || strings.Contains(strings.ToLower(got[0]), "gzip") {
		t.Errorf("Accept-Encoding of the fixture request(s) = %q, want one request that does not ask for gzip", got)
	}
	ex := requireRecorded(t, path, "2026-11-18").Exchanges[0]
	if string(ex.Body) != recCatalogBody {
		t.Errorf("body = %q, want the catalog as the bench sent it", ex.Body)
	}
	if got := ex.Header.Get("Content-Length"); got != strconv.Itoa(len(recCatalogBody)) {
		t.Errorf("Content-Length = %q, want %d (the length the bench sent)", got, len(recCatalogBody))
	}
}

func TestRecordedByOneRunReadsBackAsUnchangedOnTheNext(t *testing.T) {
	e := newRecEnv(t)
	answer := recAnswer{contentType: "application/json", body: recCatalogBody}
	e.docker.answer("admin", "/v2/_catalog", answer)
	path := e.pending("catalog--admin", recDockerHost, "/v2/_catalog")
	if _, err := e.record(); err != nil {
		t.Fatalf("first run: %v", err)
	}
	first := mustRead(t, path)

	// Same answer, another day: nothing is written, not even the date.
	if err := os.Chtimes(path, recOld, recOld); err != nil {
		t.Fatal(err)
	}
	before := recSnapshot(t, e.dir)
	e.opts.Now = func() time.Time { return recNow.Add(24 * time.Hour) }
	rep, err := e.record()
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if want := []string{"catalog/catalog--admin"}; !slices.Equal(rep.Unchanged, want) || len(rep.Written) != 0 {
		t.Errorf("report = %+v, want %v unchanged and nothing written", rep, want)
	}
	requireSameFiles(t, before, recSnapshot(t, e.dir))
	if !bytes.Equal(first, mustRead(t, path)) {
		t.Error("the file changed")
	}

	// A different answer is written, with its own date.
	e.docker.answer("admin", "/v2/_catalog", recAnswer{contentType: "application/json", body: `{"repositories":["api"]}`})
	rep, err = e.record()
	if err != nil {
		t.Fatalf("third run: %v", err)
	}
	if len(rep.Written) != 1 || len(rep.Unchanged) != 0 {
		t.Errorf("report = %+v, want the changed answer written", rep)
	}
	ex := requireRecorded(t, path, "2026-11-19").Exchanges[0]
	if string(ex.Body) != `{"repositories":["api"]}` {
		t.Errorf("body = %q, want the new answer", ex.Body)
	}
}

func TestRecordLeavesAnImportedFixtureAloneWhenItsAnswerIsTheSame(t *testing.T) {
	e := newRecEnv(t)
	e.docker.answer("admin", "/v2/_catalog", recAnswer{
		contentType: "application/json",
		header:      http.Header{"Docker-Distribution-Api-Version": {"registry/2.0"}},
		body:        recCatalogBody,
	})
	imported := "# scenario: imported from a measurement\n" +
		"# nexus: 3.98.1-01 COMMUNITY (internal/nexusbench/descriptor.json, nexusVersion)\n# measured: 2026-11-17\n" +
		"# account: admin (nx-admin, Basic, sent preemptively)\n# redacted: as measured\n\n" +
		"GET /v2/_catalog HTTP/1.1\nHost: 127.0.0.1:30482\nAuthorization: Basic <redacted>\n\n" +
		"HTTP/1.1 200 OK\nServer: Nexus/3.98.1-01 (COMMUNITY)\nDocker-Distribution-Api-Version: registry/2.0\n" +
		"Content-Type: application/json\nContent-Length: " + strconv.Itoa(len(recCatalogBody)) + "\n\n" + recCatalogBody + "\n"
	e.add("catalog", "imported--admin", imported)
	before := recSnapshot(t, e.dir)

	rep, err := e.record()
	if err != nil {
		t.Fatalf("RecordFixtures: %v", err)
	}
	if want := []string{"catalog/imported--admin"}; !slices.Equal(rep.Unchanged, want) || len(rep.Written) != 0 {
		t.Errorf("report = %+v, want %v unchanged", rep, want)
	}
	requireSameFiles(t, before, recSnapshot(t, e.dir))
}

func TestRecordWritesAtomically(t *testing.T) {
	e := newRecEnv(t)
	e.docker.answer("admin", "/v2/_catalog", recAnswer{contentType: "application/json", body: recCatalogBody})
	path := e.pending("catalog--admin", recDockerHost, "/v2/_catalog")
	was, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.record(); err != nil {
		t.Fatalf("RecordFixtures: %v", err)
	}
	now, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// A temporary file renamed over the old one is a new file; writing in place
	// would keep the same one and show a half-written fixture to a reader.
	if os.SameFile(was, now) {
		t.Error("the file was rewritten in place, want a temporary file and a rename")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "catalog--admin.http" {
		names := make([]string, len(entries))
		for i, en := range entries {
			names[i] = en.Name()
		}
		t.Errorf("the folder holds %v, want the fixture alone (no temporary file left)", names)
	}
}

func TestRecordAcceptsTheShapesOfTheClosedList(t *testing.T) {
	e := newRecEnv(t)
	targets := []struct{ host, target string }{
		{recDockerHost, "/v2/"},
		{recDockerHost, "/v2/_catalog"},
		{recDockerHost, "/v2/_catalog?n=1000"},
		{recDockerHost, "/v2/_catalog?n=5&last=api"},
		{recDockerHost, "/v2/team/api/tags/list"},
		{recRESTHost, "/repository/docker-hosted/v2/_catalog"},
		{recRESTHost, "/repository/docker-hosted/v2/_catalog?n=1000"},
		{recRESTHost, "/service/rest/v1/repositories"},
		{recRESTHost, recGroupTarget},
	}
	for i, tt := range targets {
		b := e.docker
		if tt.host == recRESTHost {
			b = e.rest
		}
		b.answer("admin", tt.target, recAnswer{contentType: "application/json", body: `{"n":` + strconv.Itoa(i) + `}`})
		e.pending("shape-"+strconv.Itoa(i)+"--admin", tt.host, tt.target)
	}
	rep, err := e.record()
	if err != nil {
		t.Fatalf("RecordFixtures refused a shape of the closed list: %v", err)
	}
	if len(rep.Written) != len(targets) {
		t.Errorf("%d fixtures written, want %d", len(rep.Written), len(targets))
	}
	for _, s := range append(e.rest.fixtureRequests(), e.docker.requests()...) {
		if s.method != "GET" {
			t.Errorf("a %s request was sent", s.method)
		}
	}
	// The query goes out as the file wrote it, parameter for parameter.
	if !slices.ContainsFunc(e.docker.requests(), func(s recSeen) bool { return s.uri == "/v2/_catalog?n=5&last=api" }) {
		t.Errorf("the bench never received /v2/_catalog?n=5&last=api: %+v", e.docker.requests())
	}
}

// DEVDESK-580: the Link of a Nexus group page carries the last name with its slash
// escaped ("last=shared%2Fbase"), so the recorder must be able to send that form
// back. Only %2F in the value of last is let through; every other percent escape,
// and any escape in the path, stays refused.
func TestRefusedRequestHandlesAnEscapedSlashInLast(t *testing.T) {
	tests := []struct {
		target  string
		refused bool
	}{
		{target: "/v2/_catalog?n=2&last=shared%2Fbase"},
		{target: "/v2/_catalog?n=2&last=shared%2fbase"},
		{target: "/v2/_catalog?n=1&last=a%2Fb%2Fc"},
		{target: "/v2/docker-hosted/_catalog?n=2&last=shared%2Fbase"},
		{target: "/v2/_catalog?n=2&last=shared/base"}, // the literal form stays accepted

		{target: "/v2/_catalog?n=2&last=a%2e%2e", refused: true},
		{target: "/v2/_catalog?n=2&last=a%2F..", refused: true},
		{target: "/v2/_catalog?n=2&last=%00", refused: true},
		{target: "/v2/_catalog?n=2&last=%2", refused: true},
		{target: "/v2/_catalog?n=2&last=%252F", refused: true},
		{target: "/v2/_catalog?n=2&last=a%20b", refused: true},
		{target: "/v2/_catalog?n=2&last=a%3Fb", refused: true},
		{target: "/v2/_catalog?n=%2F", refused: true},
		{target: "/v2/_catalog?token=a%2Fb", refused: true},
		{target: "/v2/docker%2Fhosted/_catalog", refused: true},
		{target: "/v2/%2e%2e/_catalog", refused: true},
		{target: "/v2/_catalog%2Fx", refused: true},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			got := refusedRequest("GET", tt.target)
			switch {
			case tt.refused && got == "":
				t.Errorf("refusedRequest(GET, %q) = \"\", want a refusal", tt.target)
			case !tt.refused && got != "":
				t.Errorf("refusedRequest(GET, %q) = %q, want it accepted", tt.target, got)
			}
		})
	}
}

func TestRecordQueryParamAllowsOnlyAnEscapedSlashInLast(t *testing.T) {
	for param, want := range map[string]bool{
		"n=2":                true,
		"last=shared%2Fbase": true,
		"last=shared%2fbase": true,
		"last=a%2F%2Fb":      true,
		"last=shared/base":   true,
		"last=":              true,
		"last=a%2e":          false,
		"last=%2":            false,
		"last=%252F":         false,
		"last=%":             false,
		"n=%2F":              false,
		"n=2%2F":             false,
		"token=%2F":          false,
		"last=a b":           false,
		"last=a%2Fb&n=1":     false,
	} {
		if got := recordQueryParam.MatchString(param); got != want {
			t.Errorf("recordQueryParam.MatchString(%q) = %v, want %v", param, got, want)
		}
	}
}

// End to end on the bench stand-in: a page fixture whose request carries the
// escaped last is requested exactly as the file wrote it, answered and written.
func TestRecordFillsAPendingPageWithAnEscapedLast(t *testing.T) {
	e := newRecEnv(t)
	const target = "/v2/_catalog?n=2&last=shared%2Fbase"
	body := `{"repositories":["team/api","tools/cli"]}`
	e.docker.answer("admin", target, recAnswer{contentType: "application/json", body: body})
	path := e.pending("page-group-n2-p2--admin", recDockerHost, target)

	rep, err := e.record()
	if err != nil {
		t.Fatalf("RecordFixtures refused a page request with an escaped last: %v", err)
	}
	if want := []string{"catalog/page-group-n2-p2--admin"}; !slices.Equal(rep.Written, want) {
		t.Errorf("Written = %v, want %v", rep.Written, want)
	}
	f := requireRecorded(t, path, "2026-11-18")
	if ex := f.Exchanges[0]; ex.Target != target || ex.Status != 200 || string(ex.Body) != body {
		t.Errorf("recorded %s answered %d %q, want %s answered 200 and the page", ex.Target, ex.Status, ex.Body, target)
	}
	var got []string
	for _, s := range e.docker.requests() {
		got = append(got, s.uri)
	}
	if want := []string{target}; !slices.Equal(got, want) {
		t.Errorf("the bench received %v, want exactly %v", got, want)
	}
}

// ---- guards: refused before any request -----------------------------------------

func TestRecordRefusesWhatItMayNotSend(t *testing.T) {
	tests := []struct {
		name, method, host, target string
		extra                      []string
	}{
		{name: "HEAD", method: "HEAD", target: "/v2/_catalog"},
		{name: "POST", method: "POST", target: "/service/rest/v1/repositories"},
		{name: "DELETE", method: "DELETE", target: "/service/rest/v1/repositories/docker-hosted"},
		{name: "a manifest", target: "/v2/team/api/manifests/1.0.0"},
		{name: "a manifest through a repository path", target: "/repository/docker-proxy/v2/mirror/uncached/manifests/1.0"},
		{name: "a blob", target: "/v2/team/api/blobs/sha256:abcd"},
		{name: "a blob hidden in a tags path", target: "/v2/x/blobs/tags/list"},
		{name: "a manifest in the REST path", target: "/service/rest/v1/repositories/docker-proxy/manifests/x"},
		{name: "the tags of a repository path outside the bench", target: "/repository/maven-central/v2/team/api/tags/list"},
		{name: "another REST path", target: "/service/rest/v1/security/users"},
		{name: "the components listing", target: "/service/rest/v1/components"},
		{name: "the root", target: "/"},
		{name: "a query that is not paging", target: "/v2/_catalog?token=abc"},
		{name: "a paging query with a stranger", target: "/v2/_catalog?n=1000&foo=1"},
		{name: "a query on the REST path", target: "/service/rest/v1/repositories?format=docker"},
		{name: "a dot-dot in a REST path", target: "/service/rest/v1/repositories/../security/users"},
		{name: "a dot-dot in a tags path", target: "/v2/a/../../service/rest/v1/security/users/tags/list"},
		{name: "an encoded dot-dot", target: "/service/rest/v1/repositories/%2e%2e/security/users"},
		{name: "an absolute URL", target: "http://evil.example/v2/_catalog"},
		{name: "a scheme-relative URL", target: "//evil.example/v2/_catalog"},
		{name: "a request header outside the allowlist", target: "/v2/_catalog", extra: []string{"X-Forwarded-For: 10.0.0.1"}},
		{name: "a cookie in the request", target: "/v2/_catalog", extra: []string{"Cookie: a=b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newRecEnv(t)
			e.addGood()
			method, host := tt.method, tt.host
			if method == "" {
				method = "GET"
			}
			if host == "" {
				host = recRESTHost
			}
			e.add("catalog", "z-bad--admin", recRequest(method, host, tt.target, true, tt.extra...))
			_ = e.refuse("z-bad--admin")
			e.requireNoRequest()
		})
	}
}

func TestRecordSkipsWhatItCannotRecordYet(t *testing.T) {
	e := newRecEnv(t)
	skip := strings.Replace(recRequest("GET", recDockerHost, "/v2/_catalog", true), "# record: pending\n", "# record: skip: no stable answer to record\n", 1) +
		"\nHTTP/1.1 200 OK\nServer: Nexus/3.98.1-01 (COMMUNITY)\n\n" + recCatalogBody + "\n"
	e.add("catalog", "skipped--admin", skip)
	two := nexusfixture.Format(nexusfixture.Fixture{
		Meta: nexusfixture.Meta{
			Scenario: "token step", NexusVersion: "3.98.1-01", Edition: "COMMUNITY", Measured: "2026-11-17",
			Account: "admin", Redacted: "as measured",
		},
		Exchanges: []nexusfixture.Exchange{
			{Method: "GET", Target: "/v2/", Host: recDockerHost, RequestHeader: http.Header{}, Status: 401, Header: http.Header{"Server": {recServer}}, Stub: "<empty body>"},
			{Method: "GET", Target: "/v2/_catalog", Host: recDockerHost, RequestHeader: http.Header{}, Status: 200, Header: http.Header{"Server": {recServer}}, Body: []byte(recCatalogBody)},
		},
	})
	e.add("catalog", "two-exchanges--admin", string(two))
	before := recSnapshot(t, e.dir)

	rep, err := e.record()
	if err != nil {
		t.Fatalf("RecordFixtures: %v", err)
	}
	if len(rep.Written) != 0 || len(rep.Unchanged) != 0 || len(rep.Skipped) != 2 {
		t.Fatalf("report = %+v, want both fixtures skipped", rep)
	}
	for _, want := range []string{"catalog/skipped--admin", "no stable answer to record", "catalog/two-exchanges--admin", "exchange"} {
		if !strings.Contains(strings.Join(rep.Skipped, "\n"), want) {
			t.Errorf("Skipped = %q, want it to say %q", rep.Skipped, want)
		}
	}
	e.requireNoFixtureRequest()
	requireSameFiles(t, before, recSnapshot(t, e.dir))
}

func TestRecordRefusesAnOriginThatIsNotAnOriginOfTheBench(t *testing.T) {
	tests := []struct {
		name    string
		arrange func(e *recEnv)
		fixture string
	}{
		{"a non-loopback address", func(e *recEnv) { e.opts.Origins[recDockerHost] = "http://10.0.0.5:30482" }, ""},
		{"localhost", func(e *recEnv) { e.opts.Origins[recDockerHost] = "http://localhost:30482" }, ""},
		{"user information", func(e *recEnv) { e.opts.Origins[recDockerHost] = "http://user:x@127.0.0.1:30482" }, ""},
		{"a host that starts like the loopback", func(e *recEnv) { e.opts.Origins[recDockerHost] = "http://127.0.0.1.evil.example:30482" }, ""},
		{"an origin no fixture uses, still not loopback", func(e *recEnv) {
			e.opts.Origins[recDockerHost] = "http://192.168.1.10:30482"
			e.pending("z-bad--admin", recRESTHost, recGoodTarget) // nothing is left on the docker origin
		}, ""},
		{"a host:port that is not in the descriptor", func(e *recEnv) {
			e.opts.Origins["127.0.0.1:30499"] = e.docker.srv.URL
			e.add("catalog", "z-bad--admin", recRequest("GET", "127.0.0.1:30499", "/v2/_catalog", true))
		}, ""},
		{"a host that is not in the descriptor", func(e *recEnv) {
			e.opts.Origins["127.0.0.2:30482"] = e.docker.srv.URL
			e.add("catalog", "z-bad--admin", recRequest("GET", "127.0.0.2:30482", "/v2/_catalog", true))
		}, ""},
		{"a fixture host with no origin", func(e *recEnv) { delete(e.opts.Origins, recDockerHost) }, "z-bad--admin"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newRecEnv(t)
			e.addGood()
			e.pending("z-bad--admin", recDockerHost, "/v2/_catalog")
			tt.arrange(e) // may replace z-bad--admin
			_ = e.refuse(tt.fixture)
			e.requireNoRequest()
		})
	}
}

func TestRecordRefusesAnIdentityItHasNoCredentialFor(t *testing.T) {
	tests := []struct {
		name    string
		secrets Secrets
		fixture string
	}{
		{"no pull password", Secrets{AdminPassword: recAdminPW}, "z-bad--pull"},
		{"no admin password", Secrets{PullPassword: recPullPW}, "z-bad--admin"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newRecEnv(t)
			e.opts.Secrets = tt.secrets
			e.pending(tt.fixture, recRESTHost, recGroupTarget)
			be := e.refuse(tt.fixture)
			// Both refusals tell the user to bring the bench up, in the same words.
			if !strings.Contains(be.Check, "mise run nexus-bench:up") {
				t.Errorf("Check = %q, want it to name `mise run nexus-bench:up`", be.Check)
			}
			e.requireNoRequest()
		})
	}
}

func TestRecordRefusesAnUnparsableFixture(t *testing.T) {
	e := newRecEnv(t)
	e.addGood()
	e.add("catalog", "garbled--admin", "not a fixture\n")
	_ = e.refuse("garbled--admin")
	e.requireNoRequest()
}

// A fixture that does not parse may hold a secret somebody pasted into it, and
// the parse error quotes the value it rejected: the error that reaches the
// terminal and the logs names the fixture, never what the file says. The
// parse error stays in Err, for errors.As, and is never printed.
func TestRecordRefusalOfAnUnparsableFixtureQuotesNothingOfTheFile(t *testing.T) {
	const planted = "plant-Vz81QkT3xW"
	tests := []struct {
		name    string
		planted string
		old     string
		repl    string
	}{
		// A measured value is only quoted when it is too short to be a date.
		{"a rejected header value", "pl-Vz8", "# measured: 2026-11-01", "# measured: pl-Vz8"},
		{"a version that is not one", planted, "# nexus: 3.90.0-02 COMMUNITY (stale note)", "# nexus: " + planted + " COMMUNITY"},
		{"an unknown header", planted, "# measured: 2026-11-01", "# measured: 2026-11-01\n# " + planted + ": x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newRecEnv(t)
			text := strings.Replace(recRequest("GET", recDockerHost, "/v2/_catalog", true), tt.old, tt.repl, 1)
			if !strings.Contains(text, tt.planted) {
				t.Fatal("the test did not plant its value")
			}
			e.add("catalog", "garbled--admin", text)
			be := e.refuse("garbled--admin")
			for field, got := range map[string]string{"Cause": be.Cause, "Check": be.Check, "Error()": be.Error()} {
				if strings.Contains(got, tt.planted) {
					t.Errorf("%s = %q quotes the file", field, got)
				}
			}
			if be.Err == nil {
				t.Error("Err is nil, want the parse error kept for errors.As")
			}
			e.requireNoRequest()
		})
	}
}

// What the recorder refuses to follow: a link could carry the run, which sends
// the bench credentials and rewrites files, to a file or a folder that is not
// the one of the fixtures. The refusal comes before anything is sent.
func TestRecordRefusesACampaignThatIsASymbolicLink(t *testing.T) {
	for _, tt := range []struct {
		name      string
		campaigns []string
	}{
		{"asked for by name", []string{"linked"}},
		{"found in the folder", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := newRecEnv(t)
			e.rest.answer("admin", recGoodTarget, recAnswer{contentType: "application/json", body: `[]`})
			outside := t.TempDir()
			folder := filepath.Join(outside, "real")
			if err := os.Mkdir(folder, 0o750); err != nil {
				t.Fatal(err)
			}
			victim := filepath.Join(folder, "victim--admin.http")
			if err := os.WriteFile(victim, []byte(recRequest("GET", recRESTHost, recGoodTarget, true)), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(folder, filepath.Join(e.dir, "linked")); err != nil {
				t.Skipf("no symbolic link here: %v", err)
			}
			e.opts.Campaigns = tt.campaigns
			before := recSnapshot(t, outside)

			rep, err := e.record()
			be := requireRecordRefusal(t, err)
			if !strings.Contains(be.Error(), "linked") {
				t.Errorf("the error %q does not name the campaign", be)
			}
			if len(rep.Written) != 0 {
				t.Errorf("Written = %v after a refusal, want none", rep.Written)
			}
			requireSameFiles(t, before, recSnapshot(t, outside))
			e.requireNoRequest()
		})
	}
}

func TestRecordRefusesAFixtureThatIsASymbolicLink(t *testing.T) {
	for _, tt := range []struct {
		name string
		// target returns the path the link points to, in a fresh environment.
		target func(t *testing.T, e *recEnv) string
	}{
		{"to a file outside the folder", func(t *testing.T, e *recEnv) string {
			path := filepath.Join(t.TempDir(), "victim.http")
			if err := os.WriteFile(path, []byte(recRequest("GET", recRESTHost, recGoodTarget, true)), 0o600); err != nil {
				t.Fatal(err)
			}
			return path
		}},
		{"to a file of the same campaign", func(t *testing.T, e *recEnv) string {
			return e.pending("real--admin", recRESTHost, recGoodTarget)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := newRecEnv(t)
			e.rest.answer("admin", recGoodTarget, recAnswer{contentType: "application/json", body: `[]`})
			target := tt.target(t, e)
			link := filepath.Join(e.dir, "catalog")
			if err := os.MkdirAll(link, 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(link, "linked--admin.http")); err != nil {
				t.Skipf("no symbolic link here: %v", err)
			}
			before := mustRead(t, target)

			_ = e.refuse("linked--admin")
			if after := mustRead(t, target); !bytes.Equal(before, after) {
				t.Errorf("the file the link points to was rewritten:\n%s", after)
			}
			e.requireNoRequest()
		})
	}
}

func TestRecordRefusesACancelledContext(t *testing.T) {
	e := newRecEnv(t)
	e.docker.answer("admin", "/v2/_catalog", recAnswer{contentType: "application/json", body: recCatalogBody})
	e.pending("catalog--admin", recDockerHost, "/v2/_catalog")
	before := recSnapshot(t, e.dir)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RecordFixtures(ctx, e.opts); !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled in its chain", err)
	}
	requireSameFiles(t, before, recSnapshot(t, e.dir))
}

// ---- guards: the folder, the campaigns, the filter -------------------------------

func TestRecordRefusesAFolderThatIsNotThere(t *testing.T) {
	e := newRecEnv(t)
	e.pending("catalog--admin", recDockerHost, "/v2/_catalog")
	e.opts.Dir = filepath.Join(e.dir, "nowhere")
	_ = e.refuse("nowhere")
	e.requireNoRequest()
}

func TestRecordResolvesAnEmptyFolderFromTheSourceTree(t *testing.T) {
	// An empty Dir is nexusfixture.SourceDir(): the test never records there, it
	// asks for a campaign that does not exist and sees the run refused on it.
	if _, err := nexusfixture.SourceDir(); err != nil {
		t.Skipf("the source tree is not reachable here: %v", err)
	}
	e := newRecEnv(t)
	e.opts.Dir = ""
	e.opts.Campaigns = []string{"no-such-campaign"}
	_ = e.refuse("no-such-campaign")
	e.requireNoRequest()
}

func TestRecordCampaignsAndFilter(t *testing.T) {
	setup := func(t *testing.T) *recEnv {
		e := newRecEnv(t)
		e.docker.answer("admin", "/v2/_catalog", recAnswer{contentType: "application/json", body: recCatalogBody})
		e.pending("a--admin", recDockerHost, "/v2/_catalog")
		e.pending("b--admin", recDockerHost, "/v2/_catalog")
		e.add("other", "c--admin", recRequest("GET", recDockerHost, "/v2/_catalog", true))
		return e
	}
	tests := []struct {
		name      string
		campaigns []string
		only      string
		want      []string
	}{
		{"every campaign by default", nil, "", []string{"catalog/a--admin", "catalog/b--admin", "other/c--admin"}},
		{"one campaign", []string{"other"}, "", []string{"other/c--admin"}},
		{"a glob on the fixture names", nil, "a-*", []string{"catalog/a--admin"}},
		{"a campaign and a glob", []string{"catalog"}, "b-*", []string{"catalog/b--admin"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := setup(t)
			e.opts.Campaigns, e.opts.Only = tt.campaigns, tt.only
			rep, err := e.record()
			if err != nil {
				t.Fatalf("RecordFixtures: %v", err)
			}
			if got := recSorted(rep.Written); !slices.Equal(got, tt.want) {
				t.Errorf("written = %v, want %v", got, tt.want)
			}
			// What was not selected is neither asked nor touched.
			if got := len(e.docker.requests()); got != len(tt.want) {
				t.Errorf("%d request(s) for %d fixture(s)", got, len(tt.want))
			}
			for _, name := range []string{"catalog/a--admin", "catalog/b--admin", "other/c--admin"} {
				pending := strings.Contains(string(mustRead(t, filepath.Join(e.dir, name+".http"))), "# record: pending")
				if pending == slices.Contains(tt.want, name) {
					t.Errorf("%s: pending = %v though it was selected = %v", name, pending, slices.Contains(tt.want, name))
				}
			}
		})
	}
	for _, tt := range []struct {
		name      string
		campaigns []string
		only      string
		fixture   string
	}{
		{"an unknown campaign", []string{"nope"}, "", "nope"},
		{"a campaign that climbs out of the folder", []string{"../catalog"}, "", "../catalog"},
		{"a glob that is not one", nil, "[", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := setup(t)
			e.opts.Campaigns, e.opts.Only = tt.campaigns, tt.only
			_ = e.refuse(tt.fixture)
			e.requireNoRequest()
		})
	}
}

// ---- guards: the bench is the one the fixtures describe --------------------------

func TestRecordRefusesABenchOfAnotherVersion(t *testing.T) {
	for name, server := range map[string]string{
		"another version": "Nexus/3.99.0-01 (COMMUNITY)",
		"another edition": "Nexus/3.98.1-01 (PRO)",
		"no Server":       "",
		"not a Nexus":     "nginx/1.25.3",
	} {
		t.Run(name, func(t *testing.T) {
			e := newRecEnv(t)
			e.addGood()
			e.docker.setServer(server)
			e.docker.answer("admin", "/v2/_catalog", recAnswer{contentType: "application/json", body: recCatalogBody})
			e.pending("z-bad--admin", recDockerHost, "/v2/_catalog")
			be := e.refuse("z-bad--admin")
			if !strings.Contains(be.Check, "mise run nexus-bench:up") {
				t.Errorf("Check = %q, want it to name `mise run nexus-bench:up`", be.Check)
			}
		})
	}
}

func TestRecordRefusesABenchWhoseDataSetDiffers(t *testing.T) {
	zero := "sha256:" + strings.Repeat("0", 64)
	hosted, proxy := dockerHostedRepository, dockerProxyRepository
	tests := []struct {
		name    string
		arrange func(e *recEnv)
		reset   bool // the Check says to run the reset task
	}{
		{"a hosted digest changed", func(e *recEnv) {
			l := recListing(hosted)
			l[0].Digest = zero
			e.rest.serveListing(hosted, l)
		}, true},
		{"a hosted image is missing", func(e *recEnv) { e.rest.serveListing(hosted, recListing(hosted)[1:]) }, true},
		{"the proxy caches an image the data set does not (the NX-03 HEAD)", func(e *recEnv) {
			e.rest.serveListing(proxy, append(recListing(proxy), ListedImage{Name: "shared/base", Tag: "1.0", Digest: zero}))
		}, true},
		{"the proxy lost its cached image", func(e *recEnv) { e.rest.serveListing(proxy, nil) }, true},
		{"the listing is refused", func(e *recEnv) {
			e.rest.answer("", componentsPath+"?repository="+hosted, recAnswer{status: http.StatusUnauthorized})
		}, false},
		{"the listing carries no digest", func(e *recEnv) {
			l := recListing(hosted)
			l[0].Digest = ""
			e.rest.serveListing(hosted, l)
		}, false},
		{"the descriptor lists no data set", func(e *recEnv) { e.opts.Descriptor.Dataset = nil }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newRecEnv(t)
			e.addGood()
			e.docker.answer("admin", "/v2/_catalog", recAnswer{contentType: "application/json", body: recCatalogBody})
			e.pending("z-bad--admin", recDockerHost, "/v2/_catalog")
			tt.arrange(e)
			be := e.refuse("")
			if tt.reset && !strings.Contains(be.Check, "mise run nexus-bench:reset") {
				t.Errorf("Check = %q, want it to name `mise run nexus-bench:reset`", be.Check)
			}
			// The comparison comes before any fixture request: a drifted bench is
			// not asked for answers that would be recorded as its own.
			e.requireNoFixtureRequest()
		})
	}
}

// ---- guards: what the answer may hold ---------------------------------------------

func TestRecordWritesNothingWhenOneAnswerLeaks(t *testing.T) {
	link := func(v string) http.Header {
		return http.Header{"Link": {`<http://127.0.0.1:30482/v2/_catalog?last=` + v + `>; rel="next"`}}
	}
	jsonBody := func(v string) string { return `{"repositories":[],"note":"` + v + `"}` }
	tests := []struct {
		name   string
		answer recAnswer
		deny   []string
		rule   string // the rule the error names; empty: only the fixture is asserted
	}{
		{"the admin password in a JSON body", recAnswer{contentType: "application/json", body: jsonBody(recAdminPW)}, nil, "exact-term"},
		{"the pull password in a JSON body", recAnswer{contentType: "application/json", body: jsonBody(recPullPW)}, nil, "exact-term"},
		{"the admin Basic encoding in a JSON body", recAnswer{contentType: "application/json", body: jsonBody(recBasic("admin", recAdminPW))}, nil, "exact-term"},
		{"the pull Basic encoding in a JSON body", recAnswer{contentType: "application/json", body: jsonBody(recBasic("bench-pull", recPullPW))}, nil, "exact-term"},
		{"a password in a header of the allowlist", recAnswer{contentType: "application/json", header: link(recAdminPW), body: `{}`}, nil, "exact-term"},
		{"a password in a header the file would drop", recAnswer{contentType: "application/json", header: http.Header{"X-Echo": {recAdminPW}}, body: `{}`}, nil, ""},
		{"a password in an HTML body the stub would hide", recAnswer{contentType: "text/html", body: "<html><title>t</title>" + recPullPW + "</html>"}, nil, ""},
		{"a private address", recAnswer{contentType: "application/json", body: `{"ip":"10.1.2.3"}`}, nil, "private-or-link-local-address"},
		{"a home directory", recAnswer{contentType: "application/json", body: `{"path":"/home/alice/.config"}`}, nil, "internal-name-or-path"},
		{"a term of the external deny list", recAnswer{contentType: "application/json", body: `{"host":"acme-secret-host"}`}, []string{"acme-secret-host"}, "exact-term"},
		{"a Location off the bench", recAnswer{status: 302, header: http.Header{"Location": {"http://evil.example/steal"}}}, nil, "host-not-allowed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newRecEnv(t)
			e.addGood()
			e.opts.DenyTerms = tt.deny
			e.docker.answer("admin", "/v2/_catalog", tt.answer)
			e.pending("z-bad--admin", recDockerHost, "/v2/_catalog")
			be := e.refuse("z-bad--admin")
			if tt.rule != "" && !strings.Contains(be.Error(), tt.rule) {
				t.Errorf("error %q does not name the rule %q", be, tt.rule)
			}
			for _, term := range []string{"acme-secret-host", "10.1.2.3", "/home/alice", "evil.example"} {
				if strings.Contains(be.Error(), term) {
					t.Errorf("error %q repeats what leaked (%q)", be, term)
				}
			}
		})
	}
}

func TestRecordRefusesABodyThatIsDeclaredJSONAndIsNot(t *testing.T) {
	tests := []struct {
		name, contentType, body string
		refused                 bool
	}{
		// A stray quote can hide a secret key from the redaction, which reads
		// the text, so such a body is refused instead of kept.
		{"a stray quote before a secret key", "application/json", `{"a":"b"", "token":"s3cr3t-token-value"}`, true},
		{"a truncated body", "application/json", `{"repositories":["api"`, true},
		{"a charset parameter", "application/json; charset=utf-8", `{"a":}`, true},
		{"a valid body with a trailing newline", "application/json", "{\n  \"a\" : 1\n}\n", false},
		{"an empty JSON body", "application/json", "", false},
		{"a body that is not declared JSON", "text/plain", `{"a":`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newRecEnv(t)
			e.docker.answer("admin", "/v2/_catalog", recAnswer{contentType: tt.contentType, body: tt.body})
			path := e.pending("z-bad--admin", recDockerHost, "/v2/_catalog")
			if tt.refused {
				_ = e.refuse("z-bad--admin")
				return
			}
			if _, err := e.record(); err != nil {
				t.Fatalf("RecordFixtures: %v", err)
			}
			requireRecorded(t, path, "2026-11-18")
		})
	}
}

func TestRecordFollowsNoRedirect(t *testing.T) {
	var elsewhere atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { elsewhere.Add(1) }))
	t.Cleanup(other.Close)

	e := newRecEnv(t)
	location := other.URL + "/v2/_catalog"
	e.docker.answer("admin", "/v2/_catalog", recAnswer{status: http.StatusFound, header: http.Header{"Location": {location}}})
	path := e.pending("moved--admin", recDockerHost, "/v2/_catalog")
	if _, err := e.record(); err != nil {
		t.Fatalf("RecordFixtures: %v", err)
	}
	ex := requireRecorded(t, path, "2026-11-18").Exchanges[0]
	if ex.Status != http.StatusFound {
		t.Errorf("status = %d, want the 302 itself", ex.Status)
	}
	if got := ex.Header.Get("Location"); got != location {
		t.Errorf("Location = %q, want %q kept for NX-05", got, location)
	}
	if n := elsewhere.Load(); n != 0 {
		t.Errorf("the redirect was followed: the other origin received %d request(s), with the bench credentials", n)
	}
}

func TestRecordRefusesAnAnswerBeyondTheSizeCap(t *testing.T) {
	e := newRecEnv(t)
	e.addGood()
	e.docker.answer("admin", "/v2/_catalog", recAnswer{contentType: "text/plain", body: strings.Repeat("x", maxResponseBytes+1)})
	e.pending("z-bad--admin", recDockerHost, "/v2/_catalog")
	be := e.refuse("z-bad--admin")
	var tooLarge *tooLargeError
	if !errors.As(be, &tooLarge) {
		t.Errorf("error %v does not carry a *tooLargeError", be)
	}
}
