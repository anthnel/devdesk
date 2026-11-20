package main

// Step 7c of DEVDESK-580 (PR2), the command half of `nexus-bench record`: three
// flags and what --state does. The tests of record_cmd_test.go (no flag, no
// cluster) stay as they are; the rig is theirs (newRecordRig).
//
//   - --state nominal|upstream-cut. Without it nothing changes: the command
//     opens no runner, runs no preflight and reads no upstream (the events stay
//     [newStore record]), and RecordOptions.State is nexusbench.StateAny (no fixture
//     is filtered by its state: a run that never asked for a state behaves as before
//     the states existed, DEVDESK-580). With it the
//     command, after the descriptor, the deny list and the secrets and BEFORE the
//     library runs, builds the runner, runs the preflight OpRecord (a read of the
//     Deployment and the pods), reads the upstream (ReadUpstream, read only) and
//     compares: nominal wants UpstreamReady, upstream-cut wants UpstreamCut, any
//     other state refuses, with exit 1, a message that names the declared state,
//     and record never called, so not a single request is sent. nominal maps to
//     nexusbench.StateNominal (the zero value), upstream-cut to
//     nexusbench.StateUpstreamCut. Nothing else of the cluster is touched.
//   - --allow-cache-effects sets RecordOptions.AllowCacheEffects, with no cluster
//     access of its own.
//   - --request-timeout sets RecordOptions.RequestTimeout; the default is 15s
//     (the option may carry 0 or 15s, both mean 15 s), shown in the help.
//   - The three flags are record's: the other subcommands refuse them (usage
//     error). An unknown --state or an unparsable duration is a usage error too
//     (exit 2, before any dependency); a negative duration is refused, before
//     record.
//   - When the report lists fixtures sent with a cache effect, the output names them
//     and asks for a reset; a report without any says nothing of a reset.

import (
	"bytes"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/nexusbench"
)

// requireOnlyTheStateReads fails when the command did more to the cluster than
// the preflight OpRecord and the read of the upstream.
func requireOnlyTheStateReads(t *testing.T, r *recordRig) {
	t.Helper()
	if got, want := r.h.opsEvents(), []string{"preflight:OpRecord", "readUpstream"}; !slices.Equal(got, want) {
		t.Errorf("cluster events = %v, want %v (all: %v)", got, want, r.h.eventList())
	}
	r.h.requireNotCalled("newForwarder", "newCopier", "newInitial", "deploy", "provision", "ensureUpstream", "seed",
		"cutUpstream", "restoreUpstream", "reset", "unblockProxy", "destroy", "readImages:docker-hosted", "readImages:docker-proxy")
}

func requireDefaultTimeout(t *testing.T, got time.Duration) {
	t.Helper()
	if got != 0 && got != 15*time.Second {
		t.Errorf("RequestTimeout = %v, want 0 or 15s: the default is 15 s", got)
	}
}

// ---- no flag: nothing changes ---------------------------------------------------------------

func TestRecordWithoutTheNewFlagsGivesTheLibraryTheDefaults(t *testing.T) {
	r := newRecordRig(t)
	if code, stdout, stderr := r.run(); code != 0 {
		t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	// Without --state nothing is filtered, as before the states existed:
	// StateAny, not StateNominal, which would skip a fixture that declares a state.
	if r.opts.State != nexusbench.StateAny || r.opts.AllowCacheEffects {
		t.Errorf("State %q, AllowCacheEffects %v, want StateAny and false", r.opts.State, r.opts.AllowCacheEffects)
	}
	requireDefaultTimeout(t, r.opts.RequestTimeout)
	if len(r.h.preflightOps) != 0 || r.h.has("readUpstream") || r.h.has("newRunner") {
		t.Errorf("a run with no --state reached for the cluster: %v", r.h.eventList())
	}
}

func TestRecordPassesTheNewFlagsToTheLibrary(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		upstream    nexusbench.UpstreamState
		wantState   nexusbench.BenchState
		wantAllow   bool
		wantTimeout time.Duration // 0 = the default
		wantCluster bool
	}{
		{"cache effects", []string{"--allow-cache-effects"}, nexusbench.UpstreamReady, nexusbench.StateAny, true, 0, false},
		{"a timeout", []string{"--request-timeout", "45s"}, nexusbench.UpstreamReady, nexusbench.StateAny, false, 45 * time.Second, false},
		{"a timeout with =", []string{"--request-timeout=2m"}, nexusbench.UpstreamReady, nexusbench.StateAny, false, 2 * time.Minute, false},
		{"nominal", []string{"--state", "nominal"}, nexusbench.UpstreamReady, nexusbench.StateNominal, false, 0, true},
		{"upstream-cut", []string{"--state=upstream-cut"}, nexusbench.UpstreamCut, nexusbench.StateUpstreamCut, false, 0, true},
		{
			"all of them", []string{"--state", "upstream-cut", "--allow-cache-effects", "--request-timeout", "45s", "--campaign", "catalog"},
			nexusbench.UpstreamCut, nexusbench.StateUpstreamCut, true, 45 * time.Second, true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRecordRig(t)
			r.h.upstreamState = tt.upstream
			code, stdout, stderr := r.run(tt.args...)
			if code != 0 {
				t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
			}
			if r.calls != 1 {
				t.Fatalf("record was called %d time(s), want 1", r.calls)
			}
			if r.opts.State != tt.wantState || r.opts.AllowCacheEffects != tt.wantAllow {
				t.Errorf("State %q, AllowCacheEffects %v; want %q, %v", r.opts.State, r.opts.AllowCacheEffects, tt.wantState, tt.wantAllow)
			}
			if tt.wantTimeout == 0 {
				requireDefaultTimeout(t, r.opts.RequestTimeout)
			} else if r.opts.RequestTimeout != tt.wantTimeout {
				t.Errorf("RequestTimeout = %v, want %v", r.opts.RequestTimeout, tt.wantTimeout)
			}
			if reachedCluster := len(r.h.preflightOps) > 0; reachedCluster != tt.wantCluster {
				t.Errorf("the cluster was reached = %v, want %v (events %v)", reachedCluster, tt.wantCluster, r.h.eventList())
			}
			requireClean(t, stdout, stderr)
		})
	}
}

// ---- --state: the cluster is read before the library runs -----------------------------------------

func TestRecordWithAStateReadsTheClusterThenRecords(t *testing.T) {
	for _, tt := range []struct {
		state    string
		upstream nexusbench.UpstreamState
	}{
		{"nominal", nexusbench.UpstreamReady},
		{"upstream-cut", nexusbench.UpstreamCut},
	} {
		t.Run(tt.state, func(t *testing.T) {
			r := newRecordRig(t)
			before := r.writeBench(func(p *nexusbench.Params) { p.Context, p.Namespace = "ctx-rec", "nb-rec" })
			r.h.upstreamState = tt.upstream
			code, stdout, stderr := r.run("--state", tt.state, "--context", "ctx-rec", "--namespace", "nb-rec")
			if code != 0 {
				t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
			}
			requireOnlyTheStateReads(t, r)
			if !r.h.before("newRunner", "preflight:OpRecord") || !r.h.before("preflight:OpRecord", "readUpstream") || !r.h.before("readUpstream", "record") {
				t.Errorf("want newRunner, the preflight, the read of the upstream, then record; events: %v", r.h.eventList())
			}
			if len(r.h.preflightOps) != 1 || r.h.preflightOps[0] != nexusbench.OpRecord {
				t.Errorf("preflight ops = %v, want [OpRecord]", r.h.preflightOps)
			}
			if r.h.preflightRunner != nexusbench.Runner(r.h.runner) || r.h.readUpstreamRunner != nexusbench.Runner(r.h.runner) {
				t.Error("the preflight and ReadUpstream were not given the runner of the deps")
			}
			if p := r.h.preflightParams[0]; p.Context != "ctx-rec" || p.Namespace != "nb-rec" {
				t.Errorf("preflight params = %+v, want the context and the namespace of the flags", p)
			}
			if got := r.h.newRunnerContexts; !slices.Equal(got, []string{"ctx-rec"}) {
				t.Errorf("runners built for %v, want [ctx-rec]", got)
			}
			if r.calls != 1 {
				t.Errorf("record was called %d time(s), want 1", r.calls)
			}
			if after, err := os.ReadFile(r.h.descriptor); err != nil || !bytes.Equal(after, before) {
				t.Errorf("the descriptor was changed (err=%v)", err)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty", stderr)
			}
			requireClean(t, stdout, stderr)
		})
	}
}

func TestRecordRefusesABenchThatIsNotInTheStateItDeclares(t *testing.T) {
	tests := []struct {
		declared string
		read     nexusbench.UpstreamState
		readName string
	}{
		{"nominal", nexusbench.UpstreamCut, "cut"},
		{"nominal", nexusbench.UpstreamStarting, "starting"},
		{"nominal", nexusbench.UpstreamMissing, "missing"},
		{"upstream-cut", nexusbench.UpstreamReady, "ready"},
		{"upstream-cut", nexusbench.UpstreamStarting, "starting"},
		{"upstream-cut", nexusbench.UpstreamMissing, "missing"},
	}
	for _, tt := range tests {
		t.Run(tt.declared+" but "+tt.readName, func(t *testing.T) {
			r := newRecordRig(t)
			r.h.upstreamState = tt.read
			code, stdout, stderr := r.run("--state", tt.declared)
			if code != 1 {
				t.Errorf("exit code = %d, want 1\nstderr: %s", code, stderr)
			}
			if !strings.Contains(stderr, tt.declared) {
				t.Errorf("stderr does not name the declared state %q:\n%s", tt.declared, stderr)
			}
			requireOnlyTheStateReads(t, r)
			r.requireRefusedBeforeRecord()
			if strings.TrimSpace(stdout) != "" {
				t.Errorf("stdout = %q, want nothing: no fixture was looked at", stdout)
			}
			requireClean(t, stdout, stderr)
		})
	}
}

func TestRecordWithAStateStopsWhenTheClusterCannotBeRead(t *testing.T) {
	tests := []struct {
		name       string
		arrange    func(r *recordRig)
		wantEvents []string // the cluster events that must have happened
	}{
		{"no runner", func(r *recordRig) { r.h.newRunnerErr = errors.New("no kubectl: " + secretMarker) }, nil},
		{"the preflight refuses", func(r *recordRig) { r.h.preflightErr = benchError() }, []string{"preflight:OpRecord"}},
		{"the upstream cannot be read", func(r *recordRig) { r.h.upstreamErr = benchError() }, []string{"preflight:OpRecord", "readUpstream"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRecordRig(t)
			tt.arrange(r)
			code, stdout, stderr := r.run("--state", "nominal")
			if code != 1 {
				t.Errorf("exit code = %d, want 1\nstderr: %s", code, stderr)
			}
			if got := r.h.opsEvents(); !slices.Equal(got, tt.wantEvents) {
				t.Errorf("cluster events = %v, want %v", got, tt.wantEvents)
			}
			r.requireRefusedBeforeRecord()
			if strings.TrimSpace(stderr) == "" {
				t.Error("stderr says nothing of the failure")
			}
			requireClean(t, stdout, stderr)
		})
	}
}

// Without --state the flags that stay off the cluster stay off the cluster.
func TestRecordFlagsWithoutAStateNeverReachForTheCluster(t *testing.T) {
	r := newRecordRig(t)
	code, stdout, stderr := r.run("--allow-cache-effects", "--request-timeout", "45s")
	if code != 0 {
		t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if got, want := r.h.eventList(), []string{"newStore", "record"}; !slices.Equal(got, want) {
		t.Errorf("events = %v, want %v", got, want)
	}
	if calls := r.h.runner.calls(); len(calls) != 0 {
		t.Errorf("kubectl was run: %v", calls)
	}
}

// ---- bad values and other subcommands --------------------------------------------------------------

func TestRecordUsageErrorsOfTheNewFlagsExitWithTwo(t *testing.T) {
	for _, args := range [][]string{
		{"--state", "sideways"},
		{"--state", "Upstream-Cut"},
		{"--state"},
		{"--request-timeout", "soon"},
		{"--request-timeout"},
		{"--allow-cache-effects=maybe"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			r := newRecordRig(t)
			code, stdout, stderr := r.run(args...)
			if code != 2 {
				t.Errorf("exit code = %d, want 2", code)
			}
			if stdout != "" || strings.TrimSpace(stderr) == "" {
				t.Errorf("stdout %q, stderr %q: want an empty stdout and a message", stdout, stderr)
			}
			requireClean(t, stderr)
			r.h.requireNoCalls()
			r.requireRefusedBeforeRecord()
		})
	}
}

func TestRecordRefusesANegativeRequestTimeoutBeforeRecording(t *testing.T) {
	r := newRecordRig(t)
	code, stdout, stderr := r.run("--request-timeout", "-5s")
	if code == 0 {
		t.Errorf("exit code = 0, want a refusal\nstdout: %s", stdout)
	}
	if !strings.Contains(strings.ToLower(stderr), "timeout") {
		t.Errorf("stderr does not say it is about the timeout:\n%s", stderr)
	}
	r.requireRefusedBeforeRecord()
	requireClean(t, stdout, stderr)
}

func TestTheNewRecordFlagsBelongToRecordOnly(t *testing.T) {
	for _, args := range [][]string{
		{"seed", "--state", "nominal"},
		{"up", "--allow-cache-effects"},
		{"status", "--request-timeout", "1s"},
		{"reset", "--state", "upstream-cut"},
		{"cut-upstream", "--allow-cache-effects"},
		{"restore-upstream", "--request-timeout", "1s"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			r := newRecordRig(t)
			code, stdout, stderr := r.runCommand(args...)
			if code != 2 {
				t.Errorf("exit code = %d, want 2", code)
			}
			if stdout != "" || strings.TrimSpace(stderr) == "" {
				t.Errorf("stdout %q, stderr %q: want an empty stdout and a message", stdout, stderr)
			}
			r.h.requireNoCalls()
		})
	}
}

// ---- help --------------------------------------------------------------------------------------------

func TestRecordHelpListsTheNewFlags(t *testing.T) {
	r := newRecordRig(t)
	code, stdout, stderr := r.run("-h")
	if code != 0 {
		t.Fatalf("exit code = %d\nstderr: %s", code, stderr)
	}
	_, flags, _ := strings.Cut(stdout, "Flags:")
	entry := func(name string) string {
		_, after, found := strings.Cut(flags, "-"+name)
		if !found {
			t.Errorf("record -h lists no --%s:\n%s", name, stdout)
			return ""
		}
		entry, _, _ := strings.Cut(after, "\n  -")
		return entry
	}
	if e := entry("state"); !strings.Contains(e, "nominal") || !strings.Contains(e, "upstream-cut") {
		t.Errorf("the --state help does not name both states:\n%s", e)
	}
	if e := entry("request-timeout"); !strings.Contains(e, "15s") {
		t.Errorf("the --request-timeout help does not give its default, 15s:\n%s", e)
	}
	if e := strings.ToLower(entry("allow-cache-effects")); !strings.Contains(e, "cache") || !strings.Contains(e, "reset") {
		t.Errorf("the --allow-cache-effects help does not say it can cache an image and call for a reset:\n%s", e)
	}
	r.h.requireNoCalls()
}

func TestOtherSubcommandsHelpDoNotListTheRecordFlags(t *testing.T) {
	for _, sub := range []string{"seed", "reset", "status", "up"} {
		t.Run(sub, func(t *testing.T) {
			r := newRecordRig(t)
			code, stdout, _ := r.runCommand(sub, "-h")
			if code != 0 {
				t.Fatalf("exit code = %d", code)
			}
			for _, f := range []string{"allow-cache-effects", "request-timeout"} {
				if strings.Contains(stdout, f) {
					t.Errorf("the help of %s lists --%s:\n%s", sub, f, stdout)
				}
			}
		})
	}
}

// ---- the report of a run that could cache an image --------------------------------------------------

func TestRecordAsksForAResetAfterACacheEffect(t *testing.T) {
	r := newRecordRig(t)
	r.report = nexusbench.RecordReport{
		Written:      []string{"catalog/head-proxy--admin", "catalog/rest-repositories--admin"},
		CacheEffects: []string{"catalog/head-proxy--admin"},
	}
	code, stdout, stderr := r.run("--allow-cache-effects")
	if code != 0 {
		t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	var line string
	for _, l := range strings.Split(stdout, "\n") {
		if strings.Contains(l, "head-proxy--admin") && strings.Contains(strings.ToLower(l), "cache effect") {
			line = l
		}
	}
	if line == "" {
		t.Errorf("no line names catalog/head-proxy--admin as a cache effect:\n%s", stdout)
	}
	if !strings.Contains(strings.ToLower(stdout), "reset") {
		t.Errorf("the output does not ask for a reset of the bench:\n%s", stdout)
	}
	if !strings.Contains(stdout, "record: catalog: 2 written, 0 unchanged, 0 skipped") {
		t.Errorf("the summary of the campaign is gone:\n%s", stdout)
	}
	requireClean(t, stdout, stderr)
}

func TestRecordSaysNothingOfAResetWhenNothingCouldHaveCached(t *testing.T) {
	r := newRecordRig(t)
	code, stdout, stderr := r.run("--allow-cache-effects")
	if code != 0 {
		t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if strings.Contains(strings.ToLower(stdout+stderr), "reset") || strings.Contains(strings.ToLower(stdout), "cache effect") {
		t.Errorf("a run that cached nothing talks of a reset or a cache effect:\n%s%s", stdout, stderr)
	}
}
