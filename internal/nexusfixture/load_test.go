package nexusfixture_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/anthnel/devdesk/internal/nexusfixture"
)

// importedCatalog is the set of NX-03 files the plans import (42 files): only
// what the replay steps use, plus the two pings, plus (DEVDESK-579) the twelve
// answers of the catalog read by path: <repository>/_catalog on the HTTP port
// for the three identities, and on the group connector for admin. It is the
// REQUIRED set, not the whole set: a later campaign file (NX-04, NX-05) may sit
// next to them. The bearer files (two exchanges), the manifest HEADs (a read
// through the proxy caches), the "after-measures" files, the page n1/n2 files
// and the tags files are deliberately not imported.
func importedCatalog() []string {
	var names []string
	for _, id := range []string{"admin", "pull", "anon"} {
		for _, s := range []string{
			"catalog-group-connector-30482",
			"catalog-hosted-root-30481",
			"catalog-hosted-repository",
			"catalog-proxy-repository",
			"catalog-group-repository",
			"rest-repositories-docker-group",
			"rest-repositories-docker-hosted",
			"rest-repositories-docker-proxy",
			"rest-repositories-group-docker-group",
		} {
			names = append(names, s+"--"+id)
		}
	}
	names = append(names, "page-group-n1000--admin", "v2-ping-30481--anon", "v2-ping-30482--anon")
	names = append(names, pathCatalog()...)
	sort.Strings(names)
	return names
}

// pathCatalog is the twelve NX-03 answers of the catalog read by path
// (DEVDESK-579): the file names the plan copies, byte for byte, from
// docs/measurements/nexus-catalog/.
func pathCatalog() []string {
	var names []string
	for _, repo := range []string{"hosted", "proxy", "group"} {
		for _, id := range []string{"admin", "pull", "anon"} {
			names = append(names, "catalog-"+repo+"-path--"+id)
		}
		names = append(names, "catalog-"+repo+"-path-on-connector-30482--admin")
	}
	return names
}

func TestLoadCatalogHoldsTheImportedSet(t *testing.T) {
	fixtures, err := nexusfixture.Load("catalog")
	if err != nil {
		t.Fatalf("Load(catalog): %v", err)
	}
	if len(importedCatalog()) != 42 {
		t.Fatalf("test bug: the required set has %d names, the plan says 42", len(importedCatalog()))
	}
	byName := map[string]nexusfixture.Fixture{}
	for i, f := range fixtures {
		byName[f.Name] = f
		if i > 0 && fixtures[i-1].Name >= f.Name {
			t.Errorf("Load(catalog) is not sorted by name: %s then %s", fixtures[i-1].Name, f.Name)
		}
		if f.Campaign != "catalog" {
			t.Errorf("%s: Campaign = %q, want catalog", f.Name, f.Campaign)
		}
		// Version, edition and date are what the recorder regenerates: only
		// their presence is a contract here (the evidence pins the NX-03 values).
		if f.Meta.NexusVersion == "" || f.Meta.Edition == "" || f.Meta.Measured == "" {
			t.Errorf("%s: Meta = {%q %q %q}, want a version, an edition and a date", f.Name, f.Meta.NexusVersion, f.Meta.Edition, f.Meta.Measured)
		}
		if f.Meta.Record == "pending" {
			t.Errorf("%s is pending but Load returned it", f.Name)
		}
	}
	for _, name := range importedCatalog() {
		f, ok := byName[name]
		if !ok {
			t.Errorf("Load(catalog) lacks the NX-03 fixture %s", name)
			continue
		}
		if len(f.Exchanges) != 1 {
			t.Errorf("%s: %d exchanges, want 1 (no multi-exchange file is imported)", f.Name, len(f.Exchanges))
		}
	}
}

func TestLoadCatalogIdentities(t *testing.T) {
	fixtures, err := nexusfixture.Load("catalog")
	if err != nil {
		t.Fatalf("Load(catalog): %v", err)
	}
	counts := map[nexusfixture.Identity]int{}
	for _, f := range fixtures {
		counts[f.Identity]++
		if !strings.HasSuffix(f.Name, "--"+string(f.Identity)) {
			t.Errorf("%s: Identity = %q, want the file name suffix", f.Name, f.Identity)
		}
		if f.Name != f.Scenario+"--"+string(f.Identity) {
			t.Errorf("%s: Scenario = %q, want the name without the suffix", f.Name, f.Scenario)
		}
	}
	// At least the NX-03 counts (nine scenarios x three, the n1000 page as
	// admin, two pings as anon, then the catalog read by path: three scenarios x
	// three identities and three more as admin on the group connector): a later
	// file may only add to them.
	for id, min := range map[nexusfixture.Identity]int{nexusfixture.Admin: 16, nexusfixture.Pull: 12, nexusfixture.Anonymous: 14} {
		if counts[id] < min {
			t.Errorf("identity %s has %d fixtures, want at least %d", id, counts[id], min)
		}
	}
}

func TestLoadedFixturesCarryTheMeasuredFacts(t *testing.T) {
	// Facts the replay tests of the next steps stand on, read here once on the
	// loaded set (a regeneration that changed one must be seen).
	byName := map[string]nexusfixture.Fixture{}
	fixtures, err := nexusfixture.Load("catalog")
	if err != nil {
		t.Fatalf("Load(catalog): %v", err)
	}
	for _, f := range fixtures {
		byName[f.Name] = f
	}
	tests := []struct {
		name       string
		wantStatus int
		wantHost   string
	}{
		{"rest-repositories-group-docker-group--admin", 200, "127.0.0.1:30481"},
		{"rest-repositories-group-docker-group--pull", 404, "127.0.0.1:30481"}, // contradiction C1: not 403
		{"rest-repositories-group-docker-group--anon", 403, "127.0.0.1:30481"},
		{"catalog-group-connector-30482--admin", 200, "127.0.0.1:30482"},
		{"catalog-hosted-root-30481--admin", 404, "127.0.0.1:30481"}, // no shared host catalog (IS-07)
		{"v2-ping-30481--anon", 401, "127.0.0.1:30481"},
		{"v2-ping-30482--anon", 401, "127.0.0.1:30482"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, ok := byName[tt.name]
			if !ok {
				t.Fatalf("%s is not loaded", tt.name)
			}
			ex := f.Exchanges[0]
			if ex.Status != tt.wantStatus {
				t.Errorf("Status = %d, want %d", ex.Status, tt.wantStatus)
			}
			if ex.Host != tt.wantHost {
				t.Errorf("Host = %q, want %q", ex.Host, tt.wantHost)
			}
		})
	}
	ping := byName["v2-ping-30482--anon"].Exchanges[0]
	if got := ping.Header.Get("WWW-Authenticate"); !strings.HasPrefix(got, `Bearer realm="http://127.0.0.1:30482/v2/token"`) {
		t.Errorf("ping WWW-Authenticate = %q, want the Bearer challenge as recorded", got)
	}
	// Pinned for the fake server's catalog tolerance (step 3): the DevDesk-shaped
	// ?n=1000 page and the no-n recording are the same answer on the group connector.
	n1000 := byName["page-group-n1000--admin"].Exchanges[0]
	plain := byName["catalog-group-connector-30482--admin"].Exchanges[0]
	if n1000.Target != "/v2/_catalog?n=1000" || plain.Target != "/v2/_catalog" {
		t.Errorf("targets = %q and %q, want /v2/_catalog?n=1000 and /v2/_catalog", n1000.Target, plain.Target)
	}
}

// The facts the catalog-by-path tests stand on (DEVDESK-579, Nexus 3.98.1-01):
// hosted and proxy answer a path-prefixed catalog (docker.pathEnabled) with their
// own names, unprefixed, to all three identities; the group answers 404 (no
// pathEnabled); the group connector port serves none of the three by path. A
// regeneration that changed one must be seen here.
func TestLoadedPathCatalogFixturesCarryTheMeasuredFacts(t *testing.T) {
	fixtures, err := nexusfixture.Load("catalog")
	if err != nil {
		t.Fatalf("Load(catalog): %v", err)
	}
	byName := map[string]nexusfixture.Fixture{}
	for _, f := range fixtures {
		byName[f.Name] = f
	}
	hosted := []string{"api", "shared/base", "team/api", "tools/cli"}
	proxy := []string{"mirror/cached", "mirror/uncached", "shared/base"}
	tests := []struct {
		name      string
		host      string
		repo      string
		status    int
		wantNames []string // nil: no catalog is expected (a 404 page)
	}{
		{"catalog-hosted-path--admin", "127.0.0.1:30481", "docker-hosted", 200, hosted},
		{"catalog-hosted-path--pull", "127.0.0.1:30481", "docker-hosted", 200, hosted},
		{"catalog-hosted-path--anon", "127.0.0.1:30481", "docker-hosted", 200, hosted},
		{"catalog-proxy-path--admin", "127.0.0.1:30481", "docker-proxy", 200, proxy},
		{"catalog-proxy-path--pull", "127.0.0.1:30481", "docker-proxy", 200, proxy},
		{"catalog-proxy-path--anon", "127.0.0.1:30481", "docker-proxy", 200, proxy},
		{"catalog-group-path--admin", "127.0.0.1:30481", "docker-group", 404, nil},
		{"catalog-group-path--pull", "127.0.0.1:30481", "docker-group", 404, nil},
		{"catalog-group-path--anon", "127.0.0.1:30481", "docker-group", 404, nil},
		{"catalog-hosted-path-on-connector-30482--admin", "127.0.0.1:30482", "docker-hosted", 404, nil},
		{"catalog-proxy-path-on-connector-30482--admin", "127.0.0.1:30482", "docker-proxy", 404, nil},
		{"catalog-group-path-on-connector-30482--admin", "127.0.0.1:30482", "docker-group", 404, nil},
	}
	if len(tests) != len(pathCatalog()) {
		t.Fatalf("test bug: %d cases for %d path fixtures", len(tests), len(pathCatalog()))
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, ok := byName[tt.name]
			if !ok {
				t.Fatalf("%s is not loaded: copy it byte for byte from docs/measurements/nexus-catalog/", tt.name)
			}
			if len(f.Exchanges) != 1 {
				t.Fatalf("%d exchanges, want 1", len(f.Exchanges))
			}
			ex := f.Exchanges[0]
			// The request is exactly the one DevDesk makes without paging: the
			// path of the repository, no n (fakenexus tolerates n=1000 on it).
			if want := "/v2/" + tt.repo + "/_catalog"; ex.Method != "GET" || ex.Target != want {
				t.Errorf("request = %s %s, want GET %s", ex.Method, ex.Target, want)
			}
			if ex.Host != tt.host {
				t.Errorf("Host = %q, want %q", ex.Host, tt.host)
			}
			if ex.Status != tt.status {
				t.Errorf("Status = %d, want %d", ex.Status, tt.status)
			}
			if ex.Header.Get("Link") != "" {
				t.Errorf("Link = %q: a one-page catalog has none (the tolerance needs it)", ex.Header.Get("Link"))
			}
			if tt.wantNames == nil {
				if len(ex.Body) != 0 && ex.Stub == "" {
					t.Errorf("a %d answer holds a body that is not a summarised page: %q", tt.status, ex.Body)
				}
				return
			}
			var body struct {
				Repositories []string `json:"repositories"`
			}
			if err := json.Unmarshal(ex.Body, &body); err != nil {
				t.Fatalf("body %q is not a catalog: %v", ex.Body, err)
			}
			if !reflect.DeepEqual(body.Repositories, tt.wantNames) {
				t.Errorf("names = %v, want %v (relative to the repository, never prefixed with it)", body.Repositories, tt.wantNames)
			}
		})
	}
}

func TestLoadUnknownCampaign(t *testing.T) {
	if _, err := nexusfixture.Load("no-such-campaign"); err == nil {
		t.Fatal("Load(no-such-campaign) = nil error, want an error")
	} else if !strings.Contains(err.Error(), "no-such-campaign") {
		t.Errorf("error %q does not name the campaign", err)
	}
}

func TestCampaignsAndAll(t *testing.T) {
	campaigns := nexusfixture.Campaigns()
	if !sort.StringsAreSorted(campaigns) {
		t.Errorf("Campaigns() = %v, want sorted", campaigns)
	}
	if !slices.Contains(campaigns, "catalog") {
		t.Fatalf("Campaigns() = %v, want it to include catalog", campaigns)
	}
	all, err := nexusfixture.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	var want []nexusfixture.Fixture
	for _, c := range campaigns {
		fixtures, err := nexusfixture.Load(c)
		if err != nil {
			t.Fatalf("Load(%s): %v", c, err)
		}
		want = append(want, fixtures...)
	}
	if !reflect.DeepEqual(all, want) {
		t.Errorf("All() has %d fixtures, want those of every campaign in order (%d)", len(all), len(want))
	}
}

func TestEvidenceDir(t *testing.T) {
	dir, ok := nexusfixture.EvidenceDir("catalog")
	if !ok || dir != "docs/measurements/nexus-catalog" {
		t.Errorf("EvidenceDir(catalog) = %q, %v, want docs/measurements/nexus-catalog, true", dir, ok)
	}
	if dir, ok := nexusfixture.EvidenceDir("no-such-campaign"); ok || dir != "" {
		t.Errorf("EvidenceDir(no-such-campaign) = %q, %v, want \"\", false", dir, ok)
	}
}

func TestSourceDirIsTheFixturesDirectoryOfTheSourceTree(t *testing.T) {
	dir, err := nexusfixture.SourceDir()
	if err != nil {
		t.Fatalf("SourceDir: %v", err)
	}
	if !filepath.IsAbs(dir) || filepath.Base(dir) != "fixtures" {
		t.Errorf("SourceDir() = %q, want an absolute path ending in fixtures", dir)
	}
	want, err := filepath.Abs("fixtures")
	if err != nil {
		t.Fatal(err)
	}
	if dir != want {
		t.Errorf("SourceDir() = %q, want %q (the directory this package's tests run in)", dir, want)
	}
	if st, err := os.Stat(filepath.Join(dir, "catalog")); err != nil || !st.IsDir() {
		t.Errorf("SourceDir()/catalog is not a directory: %v", err)
	}
}

// The contract of SourceDir, which has no argument to vary: it never answers
// with a path that is not there. Under -trimpath runtime.Caller gives a path
// relative to the module (not absolute, not on disk): SourceDir must then return
// an error naming the source tree, not that path. Run with -trimpath to see it.
func TestSourceDirIsAnExistingAbsoluteDirectoryOrAnError(t *testing.T) {
	dir, err := nexusfixture.SourceDir()
	if err != nil {
		if dir != "" {
			t.Errorf("SourceDir() = %q with error %v, want an empty path with an error", dir, err)
		}
		if msg := strings.ToLower(err.Error()); !strings.Contains(msg, "source tree") {
			t.Errorf("error %q must say the source tree cannot be located", err)
		}
		return
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("SourceDir() = %q with no error: not an absolute path", dir)
	}
	if st, statErr := os.Stat(dir); statErr != nil || !st.IsDir() {
		t.Errorf("SourceDir() = %q with no error: not an existing directory (%v)", dir, statErr)
	}
}

// The embedded set and the files on disk must be the same, or a new fixture
// would be silently missing from (or an old one lingering in) the binary.
func TestEmbeddedSetMatchesTheDirectory(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("fixtures", "catalog", "*.http"))
	if err != nil {
		t.Fatal(err)
	}
	var onDisk []string
	for _, p := range files {
		onDisk = append(onDisk, strings.TrimSuffix(filepath.Base(p), ".http"))
	}
	sort.Strings(onDisk)
	fixtures, err := nexusfixture.Load("catalog")
	if err != nil {
		t.Fatalf("Load(catalog): %v", err)
	}
	var loaded []string
	for _, f := range fixtures {
		loaded = append(loaded, f.Name)
	}
	if !reflect.DeepEqual(onDisk, loaded) {
		t.Errorf("fixtures on disk %v\n!= loaded %v", onDisk, loaded)
	}
}

// Provenance: no hand-written answer can pass as measured. A fixture is either
// byte-identical to its dated evidence under docs/measurements/, or it says it
// was produced by the recorder. Every embedded campaign is checked, not only
// catalog: a campaign added later (NX-04, NX-05) must not escape the check.
func TestEveryFixtureIsMeasuredOrRecorded(t *testing.T) {
	campaigns := nexusfixture.Campaigns()
	if len(campaigns) == 0 {
		t.Fatal("Campaigns() is empty: nothing would be checked")
	}
	for _, campaign := range campaigns {
		fixtures, err := nexusfixture.Load(campaign)
		if err != nil {
			t.Fatalf("Load(%s): %v", campaign, err)
		}
		for _, f := range fixtures {
			t.Run(campaign+"/"+f.Name, func(t *testing.T) {
				if err := nexusfixture.CheckProvenance(f, filepath.Join("..", "..")); err != nil {
					t.Error(err)
				}
			})
		}
	}
}

// provenanceRoot builds a repository root holding the evidence of one fixture.
func provenanceRoot(t *testing.T, name string, data []byte) string {
	t.Helper()
	root := t.TempDir()
	dir, ok := nexusfixture.EvidenceDir("catalog")
	if !ok {
		t.Fatal("the catalog campaign has no evidence directory")
	}
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o750); err != nil {
		t.Fatal(err)
	}
	if data != nil {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(dir), name+".http"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestCheckProvenance(t *testing.T) {
	const name = "synthetic--admin"
	recorded := strings.Replace(synthValid, "# redacted:", "# recorded-by: nexus-bench record\n# redacted:", 1)
	foreign := strings.Replace(synthValid, "# redacted:", "# recorded-by: somebody\n# redacted:", 1)
	parse := func(t *testing.T, campaign, text string) nexusfixture.Fixture {
		t.Helper()
		f, err := nexusfixture.Parse(campaign, name+".http", []byte(text))
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		return f
	}
	tests := []struct {
		name     string
		campaign string
		text     string
		evidence []byte // nil: no evidence file; the root always has the directory
		wantErr  bool
	}{
		{name: "recorded needs no evidence", campaign: "catalog", text: recorded},
		{name: "recorded in a campaign with no evidence directory", campaign: "access", text: recorded},
		{name: "imported and identical to its evidence", campaign: "catalog", text: synthValid, evidence: []byte(synthValid)},
		{name: "imported with no evidence file", campaign: "catalog", text: synthValid, wantErr: true},
		{name: "imported and different from its evidence", campaign: "catalog", text: synthValid,
			evidence: []byte(strings.Replace(synthValid, `{"repositories":[]}`, `{"repositories":["x"]}`, 1)), wantErr: true},
		{name: "no recorded-by in a campaign with no evidence directory", campaign: "access", text: synthValid, wantErr: true},
		{name: "a recorded-by that is not the recorder", campaign: "catalog", text: foreign, evidence: []byte(foreign), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := parse(t, tt.campaign, tt.text)
			err := nexusfixture.CheckProvenance(f, provenanceRoot(t, name, tt.evidence))
			switch {
			case tt.wantErr && err == nil:
				t.Errorf("CheckProvenance accepted %s (campaign %s), want an error", f.Name, f.Campaign)
			case tt.wantErr && !strings.Contains(err.Error(), name):
				t.Errorf("error %q does not name the fixture %s", err, name)
			case !tt.wantErr && err != nil:
				t.Errorf("CheckProvenance refused %s: %v", f.Name, err)
			}
		})
	}
}

// ---- LoadFS: the same loader over any fs.FS (an addition to the plan). ----
//
// The plan lists TestLoadRefusesAPendingFixture, but no embedded file is
// pending, so Load cannot be exercised on one. Load(campaign) is specified as
// LoadFS over the embedded files, and LoadFS(fsys, campaign) reads
// <campaign>/*.http from fsys.

func TestLoadRefusesAPendingFixture(t *testing.T) {
	fsys := fstest.MapFS{
		"c/ready--admin.http": {Data: []byte(synthValid)},
		"c/wait--admin.http":  {Data: []byte(synthHeaders + "# record: pending\n" + synthRequest)},
	}
	fixtures, err := nexusfixture.LoadFS(fsys, "c")
	if err == nil {
		t.Fatalf("LoadFS returned %d fixtures with a pending one in the campaign, want an error", len(fixtures))
	}
	msg := err.Error()
	if !strings.Contains(msg, "wait--admin") || !strings.Contains(strings.ToLower(msg), "pending") {
		t.Errorf("error %q must name the pending fixture and say it is pending", err)
	}
}

func TestLoadFSReadsOnlyHTTPFilesSortedByName(t *testing.T) {
	fsys := fstest.MapFS{
		"c/zeta--anon.http":      {Data: []byte(synthValid)},
		"c/alpha--pull.http":     {Data: []byte(synthValid)},
		"c/notes.txt":            {Data: []byte("not a fixture")},
		"c/openapi-extract.js":   {Data: []byte("{}")},
		"other/beta--admin.http": {Data: []byte(synthValid)},
	}
	fixtures, err := nexusfixture.LoadFS(fsys, "c")
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	var names []string
	for _, f := range fixtures {
		names = append(names, f.Name)
		if f.Campaign != "c" {
			t.Errorf("%s: Campaign = %q, want c", f.Name, f.Campaign)
		}
	}
	if want := []string{"alpha--pull", "zeta--anon"}; !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
}

func TestLoadFSAcceptsTwoExchanges(t *testing.T) {
	two := synthValid + "###\nGET /v2/ HTTP/1.1\nHost: 127.0.0.1:30482\n\nHTTP/1.1 200 OK\nServer: Nexus/3.98.1-01 (COMMUNITY)\n\n<empty body>\n"
	fixtures, err := nexusfixture.LoadFS(fstest.MapFS{"c/two--anon.http": {Data: []byte(two)}}, "c")
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	if len(fixtures) != 1 || len(fixtures[0].Exchanges) != 2 {
		t.Errorf("got %d fixtures, want one with two exchanges", len(fixtures))
	}
}

func TestLoadFSNamesTheFileThatDoesNotParse(t *testing.T) {
	fsys := fstest.MapFS{
		"c/good--admin.http": {Data: []byte(synthValid)},
		"c/bad--admin.http":  {Data: []byte(strings.Replace(synthValid, "# account: synthetic account\n", "", 1))},
	}
	_, err := nexusfixture.LoadFS(fsys, "c")
	if err == nil {
		t.Fatal("LoadFS accepted a campaign with an unparseable file")
	}
	if msg := err.Error(); !strings.Contains(msg, "bad--admin") || !strings.Contains(strings.ToLower(msg), "account") {
		t.Errorf("error %q must name the file and the rule", err)
	}
}

func TestLoadFSMissingCampaign(t *testing.T) {
	if _, err := nexusfixture.LoadFS(fstest.MapFS{"c/a--anon.http": {Data: []byte(synthValid)}}, "nope"); err == nil {
		t.Error("LoadFS of a campaign absent from the file system returned no error")
	}
}
