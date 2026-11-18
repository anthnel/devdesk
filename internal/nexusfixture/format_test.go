package nexusfixture_test

import (
	"bytes"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/nexusfixture"
)

// The synthetic inputs below are NOT Nexus answers: they exercise the parser's
// rules only. A real answer is read from the embedded NX-03 files.

const (
	synthHeaders = "# scenario: synthetic scenario\n" +
		"# nexus: 3.98.1-01 COMMUNITY (synthetic)\n" +
		"# measured: 2026-11-17\n" +
		"# account: synthetic account\n" +
		"# redacted: synthetic redaction\n"
	synthRequest = "\nGET /v2/_catalog HTTP/1.1\n" +
		"Host: 127.0.0.1:30482\n" +
		"Authorization: Basic <redacted>\n"
	synthResponse = "\nHTTP/1.1 200 OK\n" +
		"Server: Nexus/3.98.1-01 (COMMUNITY)\n" +
		"Content-Type: application/json\n" +
		"\n" +
		"{\"repositories\":[]}\n"
	synthValid = synthHeaders + synthRequest + synthResponse
)

// readNX03 reads one NX-03 answer from its immutable dated evidence under
// docs/measurements/, never from the embedded copy: the recorder may regenerate
// the embedded files (date, recorded-by, sizes), the evidence never changes, so
// the exact values pinned below stay true.
func readNX03(t *testing.T, name string) []byte {
	t.Helper()
	dir, ok := nexusfixture.EvidenceDir("catalog")
	if !ok {
		t.Fatal("the catalog campaign has no evidence directory")
	}
	data, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(dir), name+".http"))
	if err != nil {
		t.Fatalf("read the evidence of %s: %v", name, err)
	}
	return data
}

func parseNX03(t *testing.T, name string) nexusfixture.Fixture {
	t.Helper()
	f, err := nexusfixture.Parse("catalog", name+".http", readNX03(t, name))
	if err != nil {
		t.Fatalf("Parse(%s): %v", name, err)
	}
	return f
}

func TestParseNX03Fixture(t *testing.T) {
	f := parseNX03(t, "catalog-group-connector-30482--pull")

	if f.Campaign != "catalog" {
		t.Errorf("Campaign = %q, want catalog", f.Campaign)
	}
	if f.Name != "catalog-group-connector-30482--pull" {
		t.Errorf("Name = %q, want the file name without .http", f.Name)
	}
	if f.Scenario != "catalog-group-connector-30482" {
		t.Errorf("Scenario = %q, want the file name without the identity suffix", f.Scenario)
	}
	if f.Identity != nexusfixture.Pull {
		t.Errorf("Identity = %q, want %q (from the file name)", f.Identity, nexusfixture.Pull)
	}

	m := f.Meta
	if m.NexusVersion != "3.98.1-01" || m.Edition != "COMMUNITY" || m.Measured != "2026-11-17" {
		t.Errorf("Meta = {version %q, edition %q, measured %q}, want {3.98.1-01, COMMUNITY, 2026-11-17}",
			m.NexusVersion, m.Edition, m.Measured)
	}
	if !strings.HasPrefix(m.Scenario, "H1/H2/IS-07") {
		t.Errorf("Meta.Scenario = %q, want the scenario header text", m.Scenario)
	}
	if !strings.Contains(m.Account, "bench-pull") {
		t.Errorf("Meta.Account = %q, want the account header text", m.Account)
	}
	if !strings.Contains(m.Redacted, "Authorization header") {
		t.Errorf("Meta.Redacted = %q, want the redacted header text", m.Redacted)
	}
	if m.State != "" || m.Record != "" || m.RecordedBy != "" {
		t.Errorf("optional headers = {state %q, record %q, recorded-by %q}, want all empty on an imported NX-03 file",
			m.State, m.Record, m.RecordedBy)
	}

	if len(f.Exchanges) != 1 {
		t.Fatalf("len(Exchanges) = %d, want 1", len(f.Exchanges))
	}
	ex := f.Exchanges[0]
	if ex.Method != "GET" || ex.Target != "/v2/_catalog" || ex.Host != "127.0.0.1:30482" {
		t.Errorf("request = %s %s on %s, want GET /v2/_catalog on 127.0.0.1:30482", ex.Method, ex.Target, ex.Host)
	}
	if got := ex.RequestHeader.Get("Authorization"); got != "Basic <redacted>" {
		t.Errorf("request Authorization = %q, want the placeholder", got)
	}
	if got := ex.RequestHeader.Get("Host"); got != "" {
		t.Errorf("RequestHeader holds Host = %q: Host is the Exchange.Host field", got)
	}
	if ex.Status != 200 {
		t.Errorf("Status = %d, want 200", ex.Status)
	}
	for k, want := range map[string]string{
		"Server":                          "Nexus/3.98.1-01 (COMMUNITY)",
		"Docker-Distribution-Api-Version": "registry/2.0",
		"Content-Type":                    "application/json",
		"Content-Length":                  "95",
	} {
		if got := ex.Header.Get(k); got != want {
			t.Errorf("response header %s = %q, want %q", k, got, want)
		}
	}
	const wantBody = `{"repositories":["api","mirror/cached","mirror/uncached","shared/base","team/api","tools/cli"]}`
	if string(ex.Body) != wantBody || len(ex.Body) != 95 {
		t.Errorf("Body = %q (%d bytes), want %q (95 bytes): one trailing newline removed, nothing else", ex.Body, len(ex.Body), wantBody)
	}
	if ex.Stub != "" {
		t.Errorf("Stub = %q, want empty for a verbatim body", ex.Stub)
	}
}

func TestParseStubs(t *testing.T) {
	tests := []struct {
		name     string
		wantCode int
		wantStub string
		wantHdr  map[string]string
	}{
		{
			name:     "catalog-hosted-root-30481--anon",
			wantCode: 404,
			wantStub: "<HTML body omitted: 1309 bytes, <title>404 - Sonatype Nexus Repository</title>>",
			wantHdr:  map[string]string{"Content-Type": "text/html;charset=utf-8", "Content-Length": "1309"},
		},
		{
			name:     "rest-repositories-group-docker-group--anon",
			wantCode: 403,
			wantStub: "<empty body>",
			wantHdr:  map[string]string{"Content-Length": "0"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := parseNX03(t, tt.name)
			if f.Identity != nexusfixture.Anonymous {
				t.Errorf("Identity = %q, want %q", f.Identity, nexusfixture.Anonymous)
			}
			if len(f.Exchanges) != 1 {
				t.Fatalf("len(Exchanges) = %d, want 1", len(f.Exchanges))
			}
			ex := f.Exchanges[0]
			if ex.Status != tt.wantCode {
				t.Errorf("Status = %d, want %d", ex.Status, tt.wantCode)
			}
			if ex.Stub != tt.wantStub {
				t.Errorf("Stub = %q, want %q (as written)", ex.Stub, tt.wantStub)
			}
			if ex.Body != nil {
				t.Errorf("Body = %q, want nil when Stub is set", ex.Body)
			}
			for k, want := range tt.wantHdr {
				if got := ex.Header.Get(k); got != want {
					t.Errorf("response header %s = %q, want %q", k, got, want)
				}
			}
			if got := ex.RequestHeader.Get("Authorization"); got != "" {
				t.Errorf("anonymous request carries Authorization = %q", got)
			}
		})
	}
}

func TestParseMultilineVerbatimBody(t *testing.T) {
	f := parseNX03(t, "rest-repositories-group-docker-group--admin")
	ex := f.Exchanges[0]
	if ex.Status != 200 || ex.Stub != "" {
		t.Fatalf("status %d stub %q, want 200 and a verbatim body", ex.Status, ex.Stub)
	}
	body := string(ex.Body)
	if !strings.HasPrefix(body, "{\n  \"name\" : \"docker-group\",") || !strings.HasSuffix(body, "\n}") {
		t.Errorf("body = %q..., want the pretty-printed JSON as Nexus sent it, one trailing newline removed", body)
	}
	if !strings.Contains(body, `"memberNames" : [ "docker-hosted", "docker-proxy" ]`) {
		t.Errorf("body does not carry the members of the group verbatim: %q", body)
	}
}

func TestParseIdentityFromFileName(t *testing.T) {
	tests := []struct {
		file         string
		wantIdentity nexusfixture.Identity
		wantName     string
		wantScenario string
	}{
		{"alpha--admin.http", nexusfixture.Admin, "alpha--admin", "alpha"},
		{"alpha--pull.http", nexusfixture.Pull, "alpha--pull", "alpha"},
		{"alpha--anon.http", nexusfixture.Anonymous, "alpha--anon", "alpha"},
		{"a--b--pull.http", nexusfixture.Pull, "a--b--pull", "a--b"}, // the last separator wins
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			f, err := nexusfixture.Parse("synthetic", tt.file, []byte(synthValid))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if f.Identity != tt.wantIdentity || f.Name != tt.wantName || f.Scenario != tt.wantScenario || f.Campaign != "synthetic" {
				t.Errorf("got {campaign %q, name %q, scenario %q, identity %q}, want {synthetic, %q, %q, %q}",
					f.Campaign, f.Name, f.Scenario, f.Identity, tt.wantName, tt.wantScenario, tt.wantIdentity)
			}
		})
	}
	if nexusfixture.Admin != "admin" || nexusfixture.Pull != "pull" || nexusfixture.Anonymous != "anon" {
		t.Errorf("identity values = %q %q %q, want admin pull anon", nexusfixture.Admin, nexusfixture.Pull, nexusfixture.Anonymous)
	}
}

func TestParseOptionalHeaders(t *testing.T) {
	data := "# scenario: s\n" +
		"# nexus: 3.98.1-01 COMMUNITY (x)\n" +
		"# measured: 2026-11-17, second run\n" +
		"# account: a\n" +
		"# redacted: r\n" +
		"# state: upstream-cut\n" +
		"# record: skip: needs a bearer step\n" +
		"# recorded-by: nexus-bench record\n" +
		synthRequest + synthResponse
	f, err := nexusfixture.Parse("synthetic", "opt--anon.http", []byte(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	m := f.Meta
	if m.State != "upstream-cut" {
		t.Errorf("State = %q, want upstream-cut", m.State)
	}
	if m.Record != "skip: needs a bearer step" {
		t.Errorf("Record = %q, want the raw value", m.Record)
	}
	if m.RecordedBy != "nexus-bench record" {
		t.Errorf("RecordedBy = %q, want nexus-bench record", m.RecordedBy)
	}
	// The free text after the date is kept, so that Format loses nothing.
	if m.Measured != "2026-11-17, second run" {
		t.Errorf("Measured = %q, want the raw value after the header name", m.Measured)
	}
	if got := nexusfixture.Format(f); !bytes.Equal(got, []byte(data)) {
		t.Errorf("Format(Parse(x)) != x for a file with every optional header:\n%s", got)
	}
}

func TestParseRequestOnlyPendingFixture(t *testing.T) {
	data := synthHeaders + "# record: pending\n" + synthRequest
	f, err := nexusfixture.Parse("synthetic", "wait--admin.http", []byte(data))
	if err != nil {
		t.Fatalf("Parse of a request-only file: %v", err)
	}
	if f.Meta.Record != "pending" {
		t.Errorf("Record = %q, want pending", f.Meta.Record)
	}
	// The plan comments Exchanges as nil when pending, yet the recorder reads
	// the request to replay from the fixture itself. Chosen here: one Exchange
	// that carries the request and no response (Status 0).
	if len(f.Exchanges) != 1 {
		t.Fatalf("len(Exchanges) = %d, want 1 request-only exchange", len(f.Exchanges))
	}
	ex := f.Exchanges[0]
	if ex.Method != "GET" || ex.Target != "/v2/_catalog" || ex.Host != "127.0.0.1:30482" {
		t.Errorf("request = %s %s on %s, want GET /v2/_catalog on 127.0.0.1:30482", ex.Method, ex.Target, ex.Host)
	}
	if ex.Status != 0 || ex.Header != nil || ex.Body != nil || ex.Stub != "" {
		t.Errorf("response part = {%d %v %q %q}, want none", ex.Status, ex.Header, ex.Body, ex.Stub)
	}
	if got := nexusfixture.Format(f); !bytes.Equal(got, []byte(data)) {
		t.Errorf("Format(Parse(x)) != x for a request-only file:\n%s", got)
	}
}

func TestTwoExchangesSeparatedByHashes(t *testing.T) {
	data := synthHeaders + synthRequest + synthResponse +
		"###\n" +
		"GET /v2/token?service=x HTTP/1.1\n" +
		"Host: 127.0.0.1:30482\n" +
		"\n" +
		"HTTP/1.1 302 Found\n" +
		"Server: Nexus/3.98.1-01 (COMMUNITY)\n" +
		"Location: http://127.0.0.1:30482/v2/\n" +
		"\n" +
		"<empty body>\n"
	f, err := nexusfixture.Parse("synthetic", "two--anon.http", []byte(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(f.Exchanges) != 2 {
		t.Fatalf("len(Exchanges) = %d, want 2", len(f.Exchanges))
	}
	first, second := f.Exchanges[0], f.Exchanges[1]
	if first.Target != "/v2/_catalog" || first.Status != 200 || string(first.Body) != `{"repositories":[]}` {
		t.Errorf("first exchange = %s %d %q, want /v2/_catalog 200 with its own body (the separator is not part of it)",
			first.Target, first.Status, first.Body)
	}
	if second.Target != "/v2/token?service=x" || second.Status != 302 || second.Stub != "<empty body>" {
		t.Errorf("second exchange = %s %d stub %q, want the token request, 302, <empty body>", second.Target, second.Status, second.Stub)
	}
	if got := second.Header.Get("Location"); got != "http://127.0.0.1:30482/v2/" {
		t.Errorf("second Location = %q", got)
	}
	if second.RequestHeader.Get("Authorization") != "" {
		t.Errorf("the second request inherited the first one's Authorization")
	}
	if got := nexusfixture.Format(f); !bytes.Equal(got, []byte(data)) {
		t.Errorf("Format(Parse(x)) != x with a ### separator:\n%s", got)
	}
}

func TestParseValidSyntheticRoundTrips(t *testing.T) {
	f, err := nexusfixture.Parse("synthetic", "ok--admin.http", []byte(synthValid))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := nexusfixture.Format(f); string(got) != synthValid {
		t.Errorf("Format(Parse(x)) != x:\n%s", got)
	}
}

func TestParseErrors(t *testing.T) {
	without := func(line string) string {
		if !strings.Contains(synthValid, line) {
			t.Fatalf("test bug: %q not in the synthetic file", line)
		}
		return strings.Replace(synthValid, line, "", 1)
	}
	replace := func(old, new string) string {
		if !strings.Contains(synthValid, old) {
			t.Fatalf("test bug: %q not in the synthetic file", old)
		}
		return strings.Replace(synthValid, old, new, 1)
	}

	tests := []struct {
		name string
		file string // default "bad--admin.http"
		data string
		rule string // lower-case word the error must name, besides the file name
	}{
		{name: "missing scenario header", data: without("# scenario: synthetic scenario\n"), rule: "scenario"},
		{name: "missing nexus header", data: without("# nexus: 3.98.1-01 COMMUNITY (synthetic)\n"), rule: "nexus"},
		{name: "missing measured header", data: without("# measured: 2026-11-17\n"), rule: "measured"},
		{name: "missing account header", data: without("# account: synthetic account\n"), rule: "account"},
		{name: "missing redacted header", data: without("# redacted: synthetic redaction\n"), rule: "redacted"},
		{name: "nexus value empty", data: replace("# nexus: 3.98.1-01 COMMUNITY (synthetic)", "# nexus:"), rule: "nexus"},
		{name: "nexus version unreadable", data: replace("3.98.1-01 COMMUNITY", "banana COMMUNITY"), rule: "nexus"},
		{name: "nexus edition missing", data: replace("3.98.1-01 COMMUNITY (synthetic)", "3.98.1-01"), rule: "nexus"},
		{name: "measured is not a date", data: replace("# measured: 2026-11-17", "# measured: yesterday"), rule: "measured"},
		{name: "record value unknown", data: replace("# redacted: synthetic redaction\n", "# redacted: synthetic redaction\n# record: maybe\n"), rule: "record"},
		{name: "unknown identity suffix", file: "bad--root.http", data: synthValid, rule: "identity"},
		{name: "no identity suffix", file: "bad.http", data: synthValid, rule: "identity"},
		{name: "not an http file", file: "bad--admin.txt", data: synthValid, rule: ".http"},
		{name: "empty file", data: "", rule: "scenario"},
		{name: "no blank line between the headers and the request", data: strings.Replace(synthValid, "\nGET ", "GET ", 1), rule: "blank line"},
		{name: "no blank line between the request and the response", data: strings.Replace(synthValid, "\nHTTP/1.1 200", "HTTP/1.1 200", 1), rule: "blank line"},
		{name: "request line without a version", data: replace("GET /v2/_catalog HTTP/1.1", "GET /v2/_catalog"), rule: "request"},
		{name: "request without Host", data: without("Host: 127.0.0.1:30482\n"), rule: "host"},
		{name: "status line with a non numeric code", data: replace("HTTP/1.1 200 OK", "HTTP/1.1 abc OK"), rule: "status"},
		{name: "status line that is not a status line", data: replace("HTTP/1.1 200 OK", "200 OK"), rule: "status"},
		{name: "pending with a response", data: replace("# redacted: synthetic redaction\n", "# redacted: synthetic redaction\n# record: pending\n"), rule: "pending"},
		{name: "response missing without pending", data: synthHeaders + synthRequest, rule: "response"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := tt.file
			if file == "" {
				file = "bad--admin.http"
			}
			f, err := nexusfixture.Parse("synthetic", file, []byte(tt.data))
			if err == nil {
				t.Fatalf("Parse accepted the file: %+v", f)
			}
			msg := strings.ToLower(err.Error())
			if !strings.Contains(msg, strings.ToLower(file)) {
				t.Errorf("error %q does not name the file %q", err, file)
			}
			if !strings.Contains(msg, tt.rule) {
				t.Errorf("error %q does not name the rule %q", err, tt.rule)
			}
		})
	}
}

// Every embedded file must come back byte for byte, so that the recorder (which
// writes with Format) produces the same format as the imported files. The set is
// open: the NX-03 files must be there, a later campaign file is checked too.
// Response headers live in an http.Header (a map): Format fixes their order,
// which must be Server, Docker-Distribution-Api-Version, WWW-Authenticate,
// Content-Type, Content-Length (the order of every NX-03 file), and writes
// WWW-Authenticate with its capitals, not as the canonical Www-Authenticate.
func TestEveryFixtureRoundTrips(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("fixtures", "catalog", "*.http"))
	if err != nil {
		t.Fatal(err)
	}
	present := map[string]bool{}
	for _, path := range files {
		present[strings.TrimSuffix(filepath.Base(path), ".http")] = true
	}
	for _, name := range importedCatalog() {
		if !present[name] {
			t.Errorf("fixtures/catalog lacks the NX-03 file %s.http", name)
		}
	}
	for _, path := range files {
		name := strings.TrimSuffix(filepath.Base(path), ".http")
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			f, err := nexusfixture.Parse("catalog", filepath.Base(path), data)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got := nexusfixture.Format(f); !bytes.Equal(got, data) {
				t.Errorf("Format(Parse(b)) != b\n--- want\n%s\n--- got\n%s", data, got)
			}
		})
	}
}

// Import integrity: a verbatim body that Nexus sent with a Content-Length has
// exactly that many bytes (a stub only summarises its body, so it is skipped).
func TestVerbatimBodiesMatchTheirContentLength(t *testing.T) {
	fixtures, err := nexusfixture.Load("catalog")
	if err != nil {
		t.Fatalf("Load(catalog): %v", err)
	}
	checked := 0
	for _, f := range fixtures {
		for i, ex := range f.Exchanges {
			cl := ex.Header.Get("Content-Length")
			if ex.Stub != "" || cl == "" {
				continue
			}
			n, err := strconv.Atoi(cl)
			if err != nil {
				t.Errorf("%s exchange %d: Content-Length %q is not a number", f.Name, i, cl)
				continue
			}
			if n != len(ex.Body) {
				t.Errorf("%s exchange %d: Content-Length %d but the body has %d bytes", f.Name, i, n, len(ex.Body))
			}
			checked++
		}
	}
	if checked == 0 {
		t.Error("no verbatim body with a Content-Length was checked: the test checks nothing")
	}
}

// Rule of the plan: the format package is imported by the recorder, a dev tool
// of the nexus-bench binary, so it must not drag in "testing". Only the
// fakenexus sub-package may.
func TestProductionCodeDoesNotImportTesting(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	prod := 0
	fset := token.NewFileSet()
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		prod++
		af, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, imp := range af.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); p == "testing" || strings.HasPrefix(p, "testing/") || strings.HasSuffix(p, "/fakenexus") {
				t.Errorf("%s imports %q: the production code of nexusfixture must not", path, p)
			}
		}
	}
	if prod == 0 {
		t.Error("no production Go file in internal/nexusfixture")
	}
}

// ---- Format must never write a file that Parse refuses. ----
//
// Format has no error return and its signature does not change. Chosen: Format
// ESCAPES, Parse UNESCAPES, so that Parse(Format(f)) restores the same body. The
// escape is on the file only (a body line made of "###" or of a backslash
// followed by "###", with any number of leading backslashes, gets one more
// leading backslash; Parse strips one); the Exchange.Body in memory stays
// verbatim. A body line "###" otherwise reads as the exchange separator.

func TestFormatEscapesABodyLineThatLooksLikeTheSeparator(t *testing.T) {
	bodies := map[string]string{
		"separator line in the middle": "first\n###\nlast",
		"separator line first":         "###\nlast",
		"separator line last":          "first\n###",
		"only the separator":           "###",
		"two separator lines":          "a\n###\n###\nb",
		"backslash then separator":     "a\n\\###\nb", // a literal \### line must stay a \### line
		"two backslashes":              "a\n\\\\###\nb",
		"text after the hashes":        "###not a separator\n",
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			base, err := nexusfixture.Parse("synthetic", "esc--admin.http", []byte(synthValid))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			second := base.Exchanges[0]
			second.Target, second.Body = "/v2/", []byte("tail")
			base.Exchanges[0].Body = []byte(body)
			base.Exchanges = append(base.Exchanges, second) // a following exchange makes a mis-split visible

			written := nexusfixture.Format(base)
			got, err := nexusfixture.Parse("synthetic", "esc--admin.http", written)
			if err != nil {
				t.Fatalf("Parse(Format(f)) refused the file Format wrote: %v\n--- file\n%s", err, written)
			}
			if len(got.Exchanges) != 2 {
				t.Fatalf("Parse(Format(f)) has %d exchanges, want 2 (a body line must not split the file)\n--- file\n%s", len(got.Exchanges), written)
			}
			if string(got.Exchanges[0].Body) != body {
				t.Errorf("first body = %q, want %q (restored verbatim)", got.Exchanges[0].Body, body)
			}
			if string(got.Exchanges[1].Body) != "tail" || got.Exchanges[1].Target != "/v2/" {
				t.Errorf("second exchange = %s %q, want /v2/ tail", got.Exchanges[1].Target, got.Exchanges[1].Body)
			}
			if again := nexusfixture.Format(got); !bytes.Equal(again, written) {
				t.Errorf("Format(Parse(Format(f))) != Format(f):\n--- first\n%s\n--- second\n%s", written, again)
			}
		})
	}
}

// ---- CRLF ----

func TestParseRejectsCRLFNamingIt(t *testing.T) {
	tests := map[string]string{
		"whole file in CRLF":           strings.ReplaceAll(synthValid, "\n", "\r\n"),
		"CRLF in the header block":     strings.Replace(synthValid, "# measured: 2026-11-17\n", "# measured: 2026-11-17\r\n", 1),
		"CRLF on the request line":     strings.Replace(synthValid, "GET /v2/_catalog HTTP/1.1\n", "GET /v2/_catalog HTTP/1.1\r\n", 1),
		"CRLF on a response header":    strings.Replace(synthValid, "Content-Type: application/json\n", "Content-Type: application/json\r\n", 1),
		"CRLF after the blank line":    strings.Replace(synthValid, "application/json\n\n", "application/json\n\r\n", 1),
		"CRLF on the status line":      strings.Replace(synthValid, "HTTP/1.1 200 OK\n", "HTTP/1.1 200 OK\r\n", 1),
		"CRLF on the separator":        synthValid + "###\r\n" + synthRequest[1:] + synthResponse,
		"CRLF file with a pending req": strings.ReplaceAll(synthHeaders+"# record: pending\n"+synthRequest, "\n", "\r\n"),
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			f, err := nexusfixture.Parse("synthetic", "crlf--admin.http", []byte(data))
			if err == nil {
				t.Fatalf("Parse accepted a file with a carriage return: %+v", f)
			}
			msg := strings.ToLower(err.Error())
			if !strings.Contains(msg, "crlf--admin") {
				t.Errorf("error %q does not name the file", err)
			}
			if !strings.Contains(msg, "crlf") && !strings.Contains(msg, "carriage return") {
				t.Errorf("error %q must say CRLF or carriage return, not a generic parse failure", err)
			}
		})
	}
}

// ---- FuzzParse ----

// FuzzParse: whatever Parse accepts, Format writes a file that Parse accepts
// again, with the same meta and the same exchanges. The seeds are the embedded
// files and a few synthetic cases; `go test` runs the seeds only.
func FuzzParse(f *testing.F) {
	files, err := filepath.Glob(filepath.Join("fixtures", "*", "*.http"))
	if err != nil || len(files) == 0 {
		f.Fatalf("no embedded fixture to seed the fuzz target with (err %v)", err)
	}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	two := synthValid + "###\n" + synthRequest[1:] + synthResponse
	for _, seed := range []string{
		synthValid,
		two,
		synthHeaders + "# record: pending\n" + synthRequest,
		synthHeaders + "# state: s\n# record: skip: why\n# recorded-by: r\n" + synthRequest + synthResponse,
		strings.ReplaceAll(synthValid, "\n", "\r\n"),
		strings.Replace(synthValid, "{\"repositories\":[]}", "a\n###\nb", 1),
		strings.Replace(synthValid, "{\"repositories\":[]}", "<empty body>", 1),
		strings.Replace(synthValid, "{\"repositories\":[]}", "<HTML body omitted: 3 bytes, x>", 1),
		strings.Replace(synthValid, "Authorization: Basic <redacted>\n", "Authorization: a\nAuthorization: b\n", 1),
		strings.Replace(synthValid, "Host: 127.0.0.1:30482\n", "Host: a\nHost: b\n", 1),
		"", "#", "# scenario:", "\n\n\n", "###\n",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		const file = "fuzz--admin.http"
		first, err := nexusfixture.Parse("fuzz", file, data)
		if err != nil {
			return // a refusal is fine
		}
		written := nexusfixture.Format(first)
		second, err := nexusfixture.Parse("fuzz", file, written)
		if err != nil {
			t.Fatalf("Parse accepted the input but refused Format's output: %v\n--- input\n%q\n--- Format\n%q", err, data, written)
		}
		if !reflect.DeepEqual(first.Meta, second.Meta) {
			t.Fatalf("meta changed through Format:\n%+v\n%+v\n--- input\n%q", first.Meta, second.Meta, data)
		}
		// Format writes http.StatusText for an empty reason phrase, so a status
		// line without one comes back with it: the one intended normalisation.
		withReason := func(exs []nexusfixture.Exchange) []nexusfixture.Exchange {
			out := slices.Clone(exs)
			for i := range out {
				if out[i].Status != 0 && out[i].Reason == "" {
					out[i].Reason = http.StatusText(out[i].Status)
				}
			}
			return out
		}
		if !reflect.DeepEqual(withReason(first.Exchanges), second.Exchanges) {
			t.Fatalf("exchanges changed through Format:\n%+v\n%+v\n--- input\n%q\n--- Format\n%q", first.Exchanges, second.Exchanges, data, written)
		}
		if again := nexusfixture.Format(second); !bytes.Equal(again, written) {
			t.Fatalf("Format is not stable:\n%q\n%q", written, again)
		}
	})
}
