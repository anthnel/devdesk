package main

// Tests of the `record` subcommand (DEVDESK-511, plan step 8, DEVDESK-575),
// written before the code. They pin down, on the harness of main_test.go, one new
// member of `deps`:
//
//	record func(ctx context.Context, o nexusbench.RecordOptions) (nexusbench.RecordReport, error)
//
// (nexusbench.RecordFixtures in defaultDeps), and the subcommand `record`.
//
// What the tests fix, and the report says again:
//
//   - Order: parameters (flags, variables, defaults) -> --rest-host check ->
//     descriptor (the one of a provisioned bench: schema 2, same context and
//     namespace, else "run `mise run nexus-bench:up`") -> external deny list ->
//     secrets store -> record. No kubectl: newRunner, newForwarder, newCopier,
//     preflight and every cluster call are never made, and neither is the REST
//     listing of the command (readImages): the data set comparison lives in
//     RecordFixtures, and a drifted data set reaches the command as the BenchError
//     of that library call.
//   - The command builds RecordOptions.Origins from the descriptor: one entry per
//     loopback host and per port the descriptor publishes (REST and Docker), key
//     "host:port", value "http://host:port". With --rest-host H, the host of every
//     value is H (the key stays the descriptor's). --rest-host is loopback only,
//     as restore-upstream's (127.0.0.1 and ::1), and refused before any dependency.
//   - Secrets come from the store at --secrets / NEXUS_BENCH_SECRETS (default
//     path as the other commands), never from a flag. No --admin-password,
//     --pull-password or --password exists (usage error, exit 2) and the help names
//     none. A store with nothing in it is a refusal that says to run up.
//   - DEVDESK_FIXTURE_DENYLIST (read through getenv) names a file with one term
//     per line, blank lines and lines starting with # skipped. Its terms go to
//     RecordOptions.DenyTerms and are never printed. An unreadable file is a
//     refusal (exit 1) before record.
//   - Record-only flags: --campaign (repeatable), --only (glob), --dir. They have
//     no NEXUS_BENCH_* variable (the simplest choice).
//   - Report: one line per campaign "record: <campaign>: N written, M unchanged,
//     K skipped" (campaigns in name order, a campaign being the part of an entry
//     before "/"), after one line per fixture naming it, with the reason for a
//     skipped one. A failure prints the BenchError (Cause, Check) on stderr and
//     exits 1; the underlying error is never printed.
//   - The descriptor is never written. No timeout is pinned (a hung bench ends
//     with Ctrl-C).

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/nexusbench"
)

// ---- rig ---------------------------------------------------------------------------

// denylistVar names the file of the external deny list.
const denylistVar = "DEVDESK_FIXTURE_DENYLIST"

// recordStore is a secrets store whose content a test chooses.
type recordStore struct {
	secrets nexusbench.Secrets
	found   bool
	err     error
}

var _ nexusbench.SecretStore = (*recordStore)(nil)

func (s *recordStore) Load() (nexusbench.Secrets, bool, error) { return s.secrets, s.found, s.err }
func (s *recordStore) Save(nexusbench.Secrets) error           { return nil }
func (s *recordStore) Remove() error                           { return nil }

// recordRig is the harness plus the fake of deps.record and the descriptor of a
// seeded bench.
type recordRig struct {
	t *testing.T
	h *harness

	// programmable
	report nexusbench.RecordReport
	err    error
	store  nexusbench.SecretStore // nil: the harness store, which holds storeMarker

	// recorded
	calls int
	opts  nexusbench.RecordOptions
}

// defaultReport is what a nominal run reports: three fixtures of one campaign
// recorded, checked or skipped.
func defaultReport() nexusbench.RecordReport {
	return nexusbench.RecordReport{
		Written:   []string{"catalog/catalog-group--anon", "catalog/rest-repositories--admin"},
		Unchanged: []string{"catalog/tags-list--pull"},
		Skipped:   []string{"catalog/state-cut--admin: record: skip: needs the upstream cut"},
	}
}

// newRecordRig writes the descriptor of the default bench, seeded, at the
// harness descriptor path.
func newRecordRig(t *testing.T) *recordRig {
	t.Helper()
	r := &recordRig{t: t, h: newHarness(t), report: defaultReport()}
	r.writeBench(nil)
	return r
}

// writeBench writes the descriptor of a seeded bench whose parameters are the
// defaults changed by mod (the upstream URL follows the namespace).
func (r *recordRig) writeBench(mod func(*nexusbench.Params)) []byte {
	r.t.Helper()
	p := nexusbench.Defaults()
	if mod != nil {
		mod(&p)
		p.UpstreamURL = "http://bench-upstream." + p.Namespace + ".svc.cluster.local:5000"
	}
	return writeDescriptorAt(r.t, r.h.descriptor, p, previousDeployment, previousProvisioning, nexusbench.SeedResult{Images: nexusbench.Dataset()})
}

// run executes `nexus-bench record <args>` with the fake record.
func (r *recordRig) run(args ...string) (code int, stdout, stderr string) {
	r.t.Helper()
	return r.runCommand(append([]string{"record"}, args...)...)
}

// runCommand executes a whole command line with the fake record.
func (r *recordRig) runCommand(args ...string) (code int, stdout, stderr string) {
	r.t.Helper()
	d := r.h.deps()
	if r.store != nil {
		inner := d.newStore
		d.newStore = func(path string) (nexusbench.SecretStore, error) {
			if _, err := inner(path); err != nil {
				return nil, err
			}
			return r.store, nil
		}
	}
	d.record = func(_ context.Context, o nexusbench.RecordOptions) (nexusbench.RecordReport, error) {
		r.h.log("record")
		r.calls++
		r.opts = o
		if r.err != nil {
			return nexusbench.RecordReport{}, r.err
		}
		return r.report, nil
	}
	var out, errOut bytes.Buffer
	code = run(r.h.ctx, args, r.h.getenv, &out, &errOut, d)
	return code, out.String(), errOut.String()
}

// requireRefusedBeforeRecord fails when record was called.
func (r *recordRig) requireRefusedBeforeRecord() {
	r.t.Helper()
	if r.calls != 0 {
		r.t.Errorf("record was called %d time(s), want none; events: %v", r.calls, r.h.eventList())
	}
}

// restKey is the "host:port" of the REST API on the loopback.
func restKey(httpNodePort int) string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(httpNodePort))
}

// ---- nominal run -------------------------------------------------------------------

func TestRecordRunsTheLibraryWithoutAnyClusterAccess(t *testing.T) {
	r := newRecordRig(t)
	before, err := os.ReadFile(r.h.descriptor)
	if err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := r.run()
	if code != 0 {
		t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	// The secrets store is built, then record runs: nothing else. In particular
	// no kubectl runner, no port-forward, no skopeo, no preflight, and no listing
	// of the command's own: the data set check is the library's.
	if got, want := r.h.eventList(), []string{"newStore", "record"}; !slices.Equal(got, want) {
		t.Errorf("events = %v, want %v", got, want)
	}
	if calls := r.h.runner.calls(); len(calls) != 0 {
		t.Errorf("kubectl was run: %v", calls)
	}
	wantPath, err := nexusbench.DefaultSecretsPath(nexusbench.Defaults().Context, nexusbench.Defaults().Namespace, r.h.getenv)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.h.newStorePaths, []string{wantPath}) {
		t.Errorf("newStore paths = %v, want the default %q", r.h.newStorePaths, wantPath)
	}

	o := r.opts
	if o.Secrets.AdminPassword != storeMarker || o.Secrets.PullPassword != storeMarker {
		t.Errorf("RecordOptions.Secrets = %+v, want what the store holds", o.Secrets)
	}
	if o.Descriptor.SchemaVersion != 2 || o.Descriptor.Namespace != nexusbench.Defaults().Namespace ||
		o.Descriptor.NexusVersion != previousProvisioning.NexusVersion || len(o.Descriptor.Dataset) != len(nexusbench.Dataset()) {
		t.Errorf("RecordOptions.Descriptor is not the one on disk: %+v", o.Descriptor)
	}
	if o.Dir != "" || len(o.Campaigns) != 0 || o.Only != "" || len(o.DenyTerms) != 0 {
		t.Errorf("defaults: Dir %q, Campaigns %v, Only %q, DenyTerms %v; want all empty", o.Dir, o.Campaigns, o.Only, o.DenyTerms)
	}

	if after, err := os.ReadFile(r.h.descriptor); err != nil || !bytes.Equal(before, after) {
		t.Errorf("the descriptor was changed (err=%v)", err)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	requireClean(t, stdout, stderr)
}

func TestRecordBuildsItsOriginsFromTheLoopbackHostsOfTheDescriptor(t *testing.T) {
	r := newRecordRig(t)
	if code, stdout, stderr := r.run(); code != 0 {
		t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	rest := restKey(nexusbench.Defaults().HTTPNodePort)
	if got := r.opts.Origins[rest]; got != "http://"+rest {
		t.Errorf("origin of the REST API %s = %q, want %q (the origins: %v)", rest, got, "http://"+rest, r.opts.Origins)
	}
	// The descriptor also lists the node (192.168.1.31): the admin and pull
	// passwords are sent to the loopback only.
	for key, base := range r.opts.Origins {
		if !strings.HasPrefix(key, "127.0.0.1:") || base != "http://"+key {
			t.Errorf("origin %q -> %q: want a loopback key and its own http URL", key, base)
		}
	}
}

func TestRecordPrintsOneLinePerCampaignAndNamesEveryFixture(t *testing.T) {
	r := newRecordRig(t)
	r.report = nexusbench.RecordReport{
		Written:   []string{"tags/tags-list--pull", "catalog/catalog-group--anon", "catalog/rest-repositories--admin"},
		Unchanged: []string{"catalog/catalog-hosted--anon"},
		Skipped:   []string{"catalog/state-cut--admin: record: skip: needs the upstream cut", "catalog/two-steps--anon: holds 2 exchanges, only one is recorded yet"},
	}
	code, stdout, stderr := r.run()
	if code != 0 {
		t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	lines := strings.Split(stdout, "\n")
	for _, want := range []string{
		"record: catalog: 2 written, 1 unchanged, 2 skipped",
		"record: tags: 1 written, 0 unchanged, 0 skipped",
	} {
		if !slices.Contains(lines, want) {
			t.Errorf("stdout has no line %q:\n%s", want, stdout)
		}
	}
	if slices.Index(lines, "record: catalog: 2 written, 1 unchanged, 2 skipped") > slices.Index(lines, "record: tags: 1 written, 0 unchanged, 0 skipped") {
		t.Errorf("the campaigns are not in name order:\n%s", stdout)
	}
	for _, name := range []string{
		"catalog/catalog-group--anon", "catalog/rest-repositories--admin", "catalog/catalog-hosted--anon",
		"catalog/state-cut--admin", "catalog/two-steps--anon", "tags/tags-list--pull",
		"needs the upstream cut", "only one is recorded yet",
	} {
		if !strings.Contains(stdout, name) {
			t.Errorf("stdout does not mention %q:\n%s", name, stdout)
		}
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	requireClean(t, stdout, stderr)
}

func TestRecordWithNothingToRecordSaysSoAndSucceeds(t *testing.T) {
	r := newRecordRig(t)
	r.report = nexusbench.RecordReport{}
	code, stdout, stderr := r.run()
	if code != 0 {
		t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "record:") {
		t.Errorf("stdout says nothing of the run:\n%s", stdout)
	}
	requireClean(t, stdout, stderr)
}

// ---- flags and variables -------------------------------------------------------------

func TestRecordPassesItsOwnFlagsToTheLibrary(t *testing.T) {
	dir := t.TempDir()
	for _, tt := range []struct {
		name          string
		args          []string
		wantCampaigns []string
		wantOnly      string
		wantDir       string
	}{
		{"one campaign", []string{"--campaign", "catalog"}, []string{"catalog"}, "", ""},
		{"the flag repeated", []string{"--campaign", "catalog", "--campaign=tags"}, []string{"catalog", "tags"}, "", ""},
		{"a glob", []string{"--only", "catalog-*"}, nil, "catalog-*", ""},
		{"a glob with =", []string{"--only=rest-*--admin"}, nil, "rest-*--admin", ""},
		{"a folder", []string{"--dir", dir}, nil, "", dir},
		{"all of them", []string{"--campaign", "catalog", "--only", "catalog-*", "--dir=" + dir}, []string{"catalog"}, "catalog-*", dir},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := newRecordRig(t)
			code, stdout, stderr := r.run(tt.args...)
			if code != 0 {
				t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
			}
			if !slices.Equal(r.opts.Campaigns, tt.wantCampaigns) || r.opts.Only != tt.wantOnly || r.opts.Dir != tt.wantDir {
				t.Errorf("Campaigns %v, Only %q, Dir %q; want %v, %q, %q", r.opts.Campaigns, r.opts.Only, r.opts.Dir, tt.wantCampaigns, tt.wantOnly, tt.wantDir)
			}
			requireClean(t, stdout, stderr)
		})
	}
}

func TestRecordReadsTheCommonParametersFromTheFlagsAndTheVariables(t *testing.T) {
	t.Run("the variables", func(t *testing.T) {
		r := newRecordRig(t)
		r.writeBench(func(p *nexusbench.Params) { p.Context, p.Namespace, p.HTTPNodePort = "ctx-env", "nb-env", 31007 })
		r.h.env["NEXUS_BENCH_CONTEXT"], r.h.env["NEXUS_BENCH_NAMESPACE"], r.h.env["NEXUS_BENCH_HTTP_NODEPORT"] = "ctx-env", "nb-env", "31007"
		code, stdout, stderr := r.run()
		if code != 0 {
			t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
		}
		if r.opts.Descriptor.Namespace != "nb-env" {
			t.Errorf("the descriptor read is of namespace %q, want nb-env", r.opts.Descriptor.Namespace)
		}
		if _, ok := r.opts.Origins[restKey(31007)]; !ok {
			t.Errorf("no origin for the REST port of the variable: %v", r.opts.Origins)
		}
	})
	t.Run("a flag beats its variable", func(t *testing.T) {
		r := newRecordRig(t)
		r.writeBench(func(p *nexusbench.Params) { p.Context, p.Namespace, p.HTTPNodePort = "ctx-flag", "nb-flag", 31008 })
		r.h.env["NEXUS_BENCH_CONTEXT"], r.h.env["NEXUS_BENCH_NAMESPACE"], r.h.env["NEXUS_BENCH_HTTP_NODEPORT"] = "ctx-env", "nb-env", "31007"
		code, stdout, stderr := r.run("--context", "ctx-flag", "--namespace", "nb-flag", "--http-nodeport", "31008")
		if code != 0 {
			t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
		}
		if r.opts.Descriptor.Namespace != "nb-flag" {
			t.Errorf("the descriptor read is of namespace %q, want nb-flag", r.opts.Descriptor.Namespace)
		}
		if _, ok := r.opts.Origins[restKey(31008)]; !ok {
			t.Errorf("no origin for the REST port of the flag: %v", r.opts.Origins)
		}
	})
	t.Run("the descriptor flag beats its variable", func(t *testing.T) {
		r := newRecordRig(t)
		other := filepath.Join(t.TempDir(), "other", "descriptor.json")
		p := nexusbench.Defaults()
		p.Namespace = "nb-other"
		p.UpstreamURL = "http://bench-upstream.nb-other.svc.cluster.local:5000"
		writeDescriptorAt(t, other, p, previousDeployment, previousProvisioning, nexusbench.SeedResult{Images: nexusbench.Dataset()})
		code, stdout, stderr := r.run("--namespace", "nb-other", "--descriptor", other)
		if code != 0 {
			t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
		}
		if r.opts.Descriptor.Namespace != "nb-other" {
			t.Errorf("the descriptor read is of namespace %q, want the one of --descriptor", r.opts.Descriptor.Namespace)
		}
	})
	t.Run("the secrets path", func(t *testing.T) {
		r := newRecordRig(t)
		path := filepath.Join(t.TempDir(), "secrets.json")
		code, stdout, stderr := r.run("--secrets", path)
		if code != 0 {
			t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
		}
		if !slices.Equal(r.h.newStorePaths, []string{path}) {
			t.Errorf("newStore paths = %v, want [%s]", r.h.newStorePaths, path)
		}
	})
}

// ---- the descriptor ------------------------------------------------------------------

func TestRecordNeedsTheDescriptorOfThisBench(t *testing.T) {
	for _, tt := range []struct {
		name  string
		setup func(r *recordRig)
	}{
		{"no descriptor", func(r *recordRig) {
			if err := os.Remove(r.h.descriptor); err != nil {
				t.Fatal(err)
			}
		}},
		{"not JSON", func(r *recordRig) {
			if err := os.WriteFile(r.h.descriptor, []byte("not json"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"another namespace", func(r *recordRig) {
			r.writeBench(func(p *nexusbench.Params) { p.Namespace = "nb-elsewhere" })
		}},
		{"another context", func(r *recordRig) {
			r.writeBench(func(p *nexusbench.Params) { p.Context = "ctx-elsewhere" })
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := newRecordRig(t)
			tt.setup(r)
			code, stdout, stderr := r.run()
			if code != 1 {
				t.Errorf("exit code = %d, want 1\nstderr: %s", code, stderr)
			}
			if !strings.Contains(stderr, "nexus-bench:up") {
				t.Errorf("stderr does not say to run nexus-bench:up:\n%s", stderr)
			}
			requireClean(t, stdout, stderr)
			// Parameters, then the descriptor, then the secrets: nothing was opened.
			r.h.requireNoCalls()
			r.requireRefusedBeforeRecord()
		})
	}
}

// ---- --rest-host -----------------------------------------------------------------------

func TestRecordAcceptsOnlyALoopbackRestHost(t *testing.T) {
	rest := restKey(nexusbench.Defaults().HTTPNodePort)
	for _, tt := range []struct{ name, host, wantURL string }{
		{"the default", "", "http://" + rest},
		{"the loopback", "127.0.0.1", "http://" + rest},
		{"the IPv6 loopback", "::1", "http://[::1]:" + strconv.Itoa(nexusbench.Defaults().HTTPNodePort)},
	} {
		t.Run("accepts "+tt.name, func(t *testing.T) {
			r := newRecordRig(t)
			var args []string
			if tt.host != "" {
				args = []string{"--rest-host", tt.host}
			}
			code, stdout, stderr := r.run(args...)
			if code != 0 {
				t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
			}
			if got := r.opts.Origins[rest]; got != tt.wantURL {
				t.Errorf("REST origin = %q, want %q", got, tt.wantURL)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty on the loopback", stderr)
			}
		})
	}
	for _, host := range []string{"192.168.1.31", "fd00::1", "nexus.lan", "10.0.0.1", "localhost", "127.0.0.2", "::ffff:127.0.0.1", "0:0:0:0:0:0:0:1"} {
		t.Run("refuses "+host, func(t *testing.T) {
			r := newRecordRig(t)
			before, _ := os.ReadFile(r.h.descriptor)
			code, stdout, stderr := r.run("--rest-host", host)
			if code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			lower := strings.ToLower(stderr)
			if !strings.Contains(lower, "127.0.0.1") || !strings.Contains(lower, "record") {
				t.Errorf("stderr does not say that record always goes through 127.0.0.1:\n%s", stderr)
			}
			requireClean(t, stdout, stderr)
			r.h.requireNoCalls() // before any dependency, the secrets store included
			r.requireRefusedBeforeRecord()
			if after, _ := os.ReadFile(r.h.descriptor); !bytes.Equal(before, after) {
				t.Error("the descriptor was changed")
			}
		})
	}
	for _, host := range []string{"a/b", "user@host", "bad host", "http://127.0.0.1", "127.0.0.1:30481"} {
		t.Run("not a host "+host, func(t *testing.T) {
			r := newRecordRig(t)
			code, stdout, stderr := r.run("--rest-host=" + host)
			if (code != 1 && code != 2) || strings.TrimSpace(stderr) == "" {
				t.Errorf("exit code = %d, stderr %q", code, stderr)
			}
			requireClean(t, stdout, stderr)
			r.h.requireNoCalls()
		})
	}
}

// ---- secrets -----------------------------------------------------------------------------

func TestRecordRefusesWhenTheStoreHoldsNoSecret(t *testing.T) {
	for _, tt := range []struct {
		name string
		mod  func(r *recordRig)
	}{
		{"nothing stored", func(r *recordRig) { r.store = &recordStore{found: false} }},
		{"the file cannot be read", func(r *recordRig) {
			r.store = &recordStore{err: errors.New("read: permission denied: " + secretMarker)}
		}},
		{"the store cannot be opened", func(r *recordRig) { r.h.newStoreErr = benchError() }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := newRecordRig(t)
			tt.mod(r)
			code, stdout, stderr := r.run()
			if code != 1 {
				t.Errorf("exit code = %d, want 1\nstderr: %s", code, stderr)
			}
			if strings.TrimSpace(stderr) == "" {
				t.Error("stderr is empty: the refusal says nothing")
			}
			requireClean(t, stdout, stderr)
			r.requireRefusedBeforeRecord()
			if r.h.has("newRunner") {
				t.Error("record opened a kubectl runner")
			}
		})
	}
	t.Run("nothing stored says to run up", func(t *testing.T) {
		r := newRecordRig(t)
		r.store = &recordStore{found: false}
		code, _, stderr := r.run()
		if code != 1 || !strings.Contains(stderr, "nexus-bench:up") {
			t.Errorf("stderr does not say to run nexus-bench:up:\n%s", stderr)
		}
	})
}

// A password never comes from the command line, and the usage names no such flag.
func TestRecordHasNoPasswordFlag(t *testing.T) {
	for _, args := range [][]string{
		{"--admin-password", "x"}, {"--pull-password=x"}, {"--password", "x"}, {"--admin-password=" + secretMarker},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			r := newRecordRig(t)
			code, stdout, stderr := r.run(args...)
			if code != 2 {
				t.Errorf("exit code = %d, want 2", code)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
			requireClean(t, stdout, stderr)
			r.h.requireNoCalls()
		})
	}
	r := newRecordRig(t)
	for _, args := range [][]string{{"record", "-h"}, {"help"}} {
		_, stdout, _ := r.runCommand(args...)
		if strings.Contains(strings.ToLower(stdout), "-password") {
			t.Errorf("%v names a password flag:\n%s", args, stdout)
		}
	}
}

func TestRecordPrintsNoSecretWhatEverHappens(t *testing.T) {
	for _, failure := range []bool{false, true} {
		name := "success"
		if failure {
			name = "failure"
		}
		t.Run(name, func(t *testing.T) {
			r := newRecordRig(t)
			if failure {
				r.err = benchError()
			}
			code, stdout, stderr := r.run()
			if want := map[bool]int{false: 0, true: 1}[failure]; code != want {
				t.Errorf("exit code = %d, want %d", code, want)
			}
			requireClean(t, stdout, stderr)
			for _, s := range []string{storeMarker, secretMarker} {
				if strings.Contains(stdout+stderr, s) {
					t.Errorf("output carries a secret %q", s)
				}
			}
		})
	}
}

// ---- the external deny list ------------------------------------------------------------------

func TestRecordReadsTheExternalDenyList(t *testing.T) {
	file := filepath.Join(t.TempDir(), "deny.txt")
	content := "# instance names, kept out of the repository\n\nprod-nexus-eu\n  Registry.Internal.Example  \n#not-a-term\r\nlast-term"
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	r := newRecordRig(t)
	r.h.env[denylistVar] = file
	code, stdout, stderr := r.run()
	if code != 0 {
		t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	want := []string{"prod-nexus-eu", "Registry.Internal.Example", "last-term"}
	if !reflect.DeepEqual(r.opts.DenyTerms, want) {
		t.Errorf("DenyTerms = %q, want %q", r.opts.DenyTerms, want)
	}
	for _, term := range want {
		if strings.Contains(stdout+stderr, term) {
			t.Errorf("the output carries the deny list term %q:\nstdout: %s\nstderr: %s", term, stdout, stderr)
		}
	}
}

func TestRecordWithoutADenyListSendsNoTerm(t *testing.T) {
	r := newRecordRig(t)
	delete(r.h.env, denylistVar)
	if code, stdout, stderr := r.run(); code != 0 {
		t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if len(r.opts.DenyTerms) != 0 {
		t.Errorf("DenyTerms = %q, want none", r.opts.DenyTerms)
	}
}

func TestRecordRefusesADenyListThatCannotBeRead(t *testing.T) {
	r := newRecordRig(t)
	r.h.env[denylistVar] = filepath.Join(t.TempDir(), "missing.txt")
	code, stdout, stderr := r.run()
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, denylistVar) {
		t.Errorf("stderr does not name %s:\n%s", denylistVar, stderr)
	}
	requireClean(t, stdout, stderr)
	r.requireRefusedBeforeRecord()
}

// ---- the library refuses ------------------------------------------------------------------------

// A drifted data set is the library's refusal: the command shows it and does not
// compare anything itself.
func TestRecordShowsTheRefusalOfTheLibraryAndExitsWithOne(t *testing.T) {
	r := newRecordRig(t)
	r.err = &nexusbench.BenchError{
		Kind:  nexusbench.Unclassified,
		Cause: "docker-hosted differs from the data set of the descriptor (1 image(s))",
		Check: "run `mise run nexus-bench:reset` to bring the bench back to its initial state",
		Err:   errors.New("listing: Authorization: Basic " + secretMarker),
	}
	code, stdout, stderr := r.run()
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	for _, want := range []string{"docker-hosted differs from the data set of the descriptor", "nexus-bench:reset"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr does not carry %q:\n%s", want, stderr)
		}
	}
	requireClean(t, stdout, stderr)
	if strings.Contains(stdout, "written") {
		t.Errorf("stdout prints a report for a refused run:\n%s", stdout)
	}
	if r.calls != 1 {
		t.Errorf("record was called %d times, want 1", r.calls)
	}
	// The command reads no listing of its own, before or after.
	for _, e := range r.h.eventList() {
		if strings.HasPrefix(e, "readImages:") || strings.HasPrefix(e, "preflight:") {
			t.Errorf("the command made a call of its own: %s (events: %v)", e, r.h.eventList())
		}
	}
	if got, want := r.h.eventList(), []string{"newStore", "record"}; !slices.Equal(got, want) {
		t.Errorf("events = %v, want %v", got, want)
	}
}

func TestRecordShowsAnErrorThatIsNotABenchError(t *testing.T) {
	r := newRecordRig(t)
	r.err = errors.New("the fixtures folder cannot be read")
	code, stdout, stderr := r.run()
	if code != 1 || !strings.Contains(stderr, "the fixtures folder cannot be read") {
		t.Errorf("exit code = %d, stderr %q; want 1 and the message", code, stderr)
	}
	requireClean(t, stdout, stderr)
}

func TestRecordCancelledSaysSo(t *testing.T) {
	r := newRecordRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r.h.ctx = ctx
	r.err = context.Canceled
	code, stdout, stderr := r.run()
	if code != 1 || !strings.Contains(stderr, "cancelled") {
		t.Errorf("exit code = %d, stderr %q; want 1 and a cancellation message", code, stderr)
	}
	requireClean(t, stdout, stderr)
}

// ---- help and usage -----------------------------------------------------------------------------

func TestRecordHelpListsItsFlags(t *testing.T) {
	for _, flag := range []string{"-h", "--help"} {
		t.Run(flag, func(t *testing.T) {
			r := newRecordRig(t)
			code, stdout, stderr := r.run(flag)
			if code != 0 {
				t.Fatalf("exit code = %d", code)
			}
			if !strings.HasPrefix(stdout, "Usage: nexus-bench record [flags]") {
				t.Errorf("the help does not start with its usage line:\n%s", stdout)
			}
			for _, f := range []string{"campaign", "only", "dir", "rest-host", "namespace", "context", "http-nodeport", "secrets", "descriptor", "upstream-url"} {
				if !strings.Contains(stdout, f) {
					t.Errorf("the help of record does not list %q:\n%s", f, stdout)
				}
			}
			for _, f := range []string{"accept-eula", "password"} {
				if strings.Contains(stdout, f) {
					t.Errorf("the help of record lists %q, which it does not take:\n%s", f, stdout)
				}
			}
			summary := strings.ToLower(summaryOf(t, stdout))
			for _, w := range []string{"fixture", "data set"} {
				if !strings.Contains(summary, w) {
					t.Errorf("the summary of record does not mention %q:\n%s", w, summary)
				}
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty", stderr)
			}
			r.h.requireNoCalls()
			r.requireRefusedBeforeRecord()
		})
	}
	t.Run("the rest-host flag says loopback only", func(t *testing.T) {
		r := newRecordRig(t)
		_, stdout, _ := r.run("-h")
		_, flags, _ := strings.Cut(stdout, "Flags:")
		_, afterRest, found := strings.Cut(flags, "-rest-host")
		if !found {
			t.Fatalf("record -h lists no --rest-host:\n%s", stdout)
		}
		entry, _, _ := strings.Cut(afterRest, "\n  -")
		if !strings.Contains(entry, "127.0.0.1") || !strings.Contains(strings.ToLower(entry), "loopback") || strings.Contains(strings.ToLower(entry), "clear text") {
			t.Errorf("the --rest-host help of record does not say it takes a loopback address only:\n%s", entry)
		}
	})
}

func TestGeneralHelpListsRecord(t *testing.T) {
	r := newRecordRig(t)
	code, stdout, _ := r.runCommand("help")
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	entry := entryOf(stdout, "record")
	if entry == "" {
		t.Fatalf("the general help has no entry for record:\n%s", stdout)
	}
	if lower := strings.ToLower(entry); !strings.Contains(lower, "fixture") {
		t.Errorf("the entry of record does not say it regenerates the recorded fixtures:\n%s", entry)
	}
}

func TestRecordUsageErrorsExitWithTwo(t *testing.T) {
	for _, args := range [][]string{
		{"--bogus"},
		{"extra"},
		{"--campaign"},
		{"--only"},
		{"--dir"},
		{"--timeout", "soon"},
		{"--http-nodeport", "abc"},
		{"--namespace"},
		{"--accept-eula"},
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
