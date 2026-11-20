package nexusbench

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

// Step 7c of DEVDESK-580 (PR2), the library half: the state of the bench and the
// time a request may take. The command half (--state, --allow-cache-effects,
// --request-timeout, the preflight and the read of the upstream) is in
// cmd/nexus-bench/record_state_cmd_test.go.
//
// RecordOptions.State says in which state the bench is; it is the command that
// reads the cluster to check it, the library only filters. Only the fixtures whose
// "state" header starts with the state are recorded, the others are reported
// skipped, with a reason that names the state, and are never requested. The zero
// value (StateNominal) takes the fixtures with NO state header: a fixture that
// declares another state is never recorded against a bench that is in none. A
// State that is neither StateNominal nor StateUpstreamCut is a refusal before any
// request.
//
// RecordOptions.RequestTimeout bounds one request of the run, the connection
// included; 0 means 15 s (clientTimeout, the current behavior), a negative value
// is a refusal before any request. The upstream reads its first image in 20 s
// when it is cut (NX-02b), so a run against that state asks for 45 s. The delays
// below are scaled down: the default itself is pinned through
// recordRequestTimeout, not by a 15 s sleep.

const recMetaUpstreamCut = "# state: upstream-cut"

func TestRecordStateSelectsTheFixturesOfTheStateOfTheBench(t *testing.T) {
	tests := []struct {
		name        string
		state       BenchState
		wantWritten []string
		wantSkipped []string
		wantRest    int // fixture requests on the rest origin
		wantDocker  int
	}{
		{
			name: "nominal", state: StateNominal,
			wantWritten: []string{"catalog/a-nominal--admin"},
			wantSkipped: []string{"catalog/b-cut--admin", "catalog/c-cut-text--admin"},
			wantRest:    1,
		},
		{
			name: "upstream cut", state: StateUpstreamCut,
			wantWritten: []string{"catalog/b-cut--admin", "catalog/c-cut-text--admin"},
			wantSkipped: []string{"catalog/a-nominal--admin"},
			wantDocker:  2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newRecEnv(t)
			e.opts.State = tt.state
			e.rest.answer("admin", recGoodTarget, recAnswer{contentType: "application/json", body: `[]`})
			e.docker.answer("admin", "/v2/_catalog", recAnswer{contentType: "application/json", body: recCatalogBody})
			e.docker.answer("admin", "/v2/docker-hosted/_catalog", recAnswer{contentType: "application/json", body: recCatalogBody})
			e.pending("a-nominal--admin", recRESTHost, recGoodTarget)
			e.addMeta("b-cut--admin", []string{recMetaUpstreamCut}, "GET", recDockerHost, "/v2/_catalog")
			e.addMeta("c-cut-text--admin", []string{"# state: upstream-cut (the proxy is blocked)"}, "GET", recDockerHost, "/v2/docker-hosted/_catalog")
			before := recSnapshot(t, e.dir)

			rep, err := e.record()
			if err != nil {
				t.Fatalf("RecordFixtures: %v", err)
			}
			if !slices.Equal(rep.Written, tt.wantWritten) {
				t.Errorf("Written = %v, want %v", rep.Written, tt.wantWritten)
			}
			var skipped []string
			for _, s := range rep.Skipped {
				name, reason, _ := strings.Cut(s, ": ")
				skipped = append(skipped, name)
				if !strings.Contains(strings.ToLower(reason), "state") {
					t.Errorf("the skip %q does not say it is a matter of state", s)
				}
			}
			if !slices.Equal(recSorted(skipped), tt.wantSkipped) {
				t.Errorf("Skipped = %v, want %v", rep.Skipped, tt.wantSkipped)
			}
			if got := len(e.rest.fixtureRequests()); got != tt.wantRest {
				t.Errorf("the REST origin saw %d fixture request(s), want %d", got, tt.wantRest)
			}
			if got := len(e.docker.requests()); got != tt.wantDocker {
				t.Errorf("the connector saw %d request(s), want %d: a skipped fixture is never requested", got, tt.wantDocker)
			}
			// A skipped file is left alone, bytes and modification time.
			after := recSnapshot(t, e.dir)
			for _, name := range tt.wantSkipped {
				p := e.dir + "/" + name + ".http"
				if string(before[p].data) != string(after[p].data) || !before[p].mod.Equal(after[p].mod) {
					t.Errorf("%s was touched though it is skipped", name)
				}
			}
		})
	}
}

// The state of a fixture that is in no state of the run, and a run that has all of
// them skipped, ask nothing of the bench: not even the data set.
func TestRecordStateThatSkipsEverythingSendsNoRequest(t *testing.T) {
	e := newRecEnv(t)
	e.opts.State = StateUpstreamCut
	e.pending("a-nominal--admin", recRESTHost, recGoodTarget)
	rep, err := e.record()
	if err != nil {
		t.Fatalf("RecordFixtures: %v", err)
	}
	if len(rep.Written) != 0 || len(rep.Skipped) != 1 {
		t.Errorf("report = %+v, want the one fixture skipped", rep)
	}
	e.requireNoRequest()
}

func TestRecordRefusesAStateItDoesNotKnow(t *testing.T) {
	for _, state := range []BenchState{"upstream", "Upstream-Cut", "proxy-blocked", " "} {
		t.Run(string(state), func(t *testing.T) {
			e := newRecEnv(t)
			e.opts.State = state
			e.addGood()
			be := e.refuse("")
			if !strings.Contains(be.Error(), "state") {
				t.Errorf("the refusal %q does not say it is about the state", be.Error())
			}
			e.requireNoRequest()
		})
	}
}

// ---- the time a request may take ------------------------------------------------------------------

// slowBench answers a catalog after delay, or gives up when the client does.
func slowBench(t *testing.T, delay time.Duration) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Server", recServer)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(recCatalogBody))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRecordRequestTimeoutDefaultsToFifteenSeconds(t *testing.T) {
	if defaultRecordTimeout != clientTimeout {
		t.Errorf("defaultRecordTimeout = %v, want the 15 s clientTimeout of today", defaultRecordTimeout)
	}
	tests := []struct {
		name string
		opts RecordOptions
		want time.Duration
	}{
		{"not set", RecordOptions{}, defaultRecordTimeout},
		{"set", RecordOptions{RequestTimeout: 45 * time.Second}, 45 * time.Second},
		{"set below the default", RecordOptions{RequestTimeout: 2 * time.Second}, 2 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := recordRequestTimeout(tt.opts); got != tt.want {
				t.Errorf("recordRequestTimeout = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRecordGivesUpOnAnAnswerThatTakesLongerThanTheRequestTimeout(t *testing.T) {
	e := newRecEnv(t)
	e.opts.Origins[recDockerHost] = slowBench(t, 1500*time.Millisecond).URL
	e.opts.RequestTimeout = 150 * time.Millisecond
	e.addGood()
	e.pending("z-slow--admin", recDockerHost, "/v2/_catalog")

	start := time.Now()
	be := e.refuse("z-slow--admin")
	if !strings.Contains(be.Error(), "could not be read") {
		t.Errorf("the error %q does not say the answer could not be read", be.Error())
	}
	if took := time.Since(start); took > time.Second {
		t.Errorf("the run took %v: the request was not bounded by RequestTimeout (150ms)", took)
	}
}

func TestRecordWaitsForASlowAnswerWithinTheRequestTimeout(t *testing.T) {
	e := newRecEnv(t)
	e.opts.Origins[recDockerHost] = slowBench(t, 400*time.Millisecond).URL
	e.opts.RequestTimeout = 5 * time.Second
	path := e.pending("slow--admin", recDockerHost, "/v2/_catalog")
	rep, err := e.record()
	if err != nil {
		t.Fatalf("RecordFixtures: %v", err)
	}
	if want := []string{"catalog/slow--admin"}; !slices.Equal(rep.Written, want) {
		t.Errorf("Written = %v, want %v", rep.Written, want)
	}
	requireRecorded(t, path, "2026-11-18")
}

// The timeout is per request: two slow answers that each fit are recorded though
// they do not fit together.
func TestRecordRequestTimeoutBoundsOneRequestNotTheRun(t *testing.T) {
	e := newRecEnv(t)
	e.opts.Origins[recDockerHost] = slowBench(t, 500*time.Millisecond).URL
	e.opts.RequestTimeout = 800 * time.Millisecond
	e.pending("slow-a--admin", recDockerHost, "/v2/_catalog")
	e.pending("slow-b--pull", recDockerHost, "/v2/_catalog")
	rep, err := e.record()
	if err != nil {
		t.Fatalf("RecordFixtures: %v", err)
	}
	if len(rep.Written) != 2 {
		t.Errorf("Written = %v, want both: each request fits in 800ms, the run does not", rep.Written)
	}
}

func TestRecordRefusesANegativeRequestTimeout(t *testing.T) {
	e := newRecEnv(t)
	e.opts.RequestTimeout = -time.Second
	e.addGood()
	be := e.refuse("")
	if !strings.Contains(strings.ToLower(be.Error()), "timeout") {
		t.Errorf("the refusal %q does not say it is about the timeout", be.Error())
	}
	e.requireNoRequest()
}
