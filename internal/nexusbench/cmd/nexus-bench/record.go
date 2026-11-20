package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/anthnel/devdesk/internal/nexusbench"
)

// fixtureDenylistEnv names the file of the external deny list: one term per line, the
// instance names and hosts that must never appear in a recorded fixture and that
// the repository itself must not carry (DEVDESK-511).
const fixtureDenylistEnv = "DEVDESK_FIXTURE_DENYLIST"

// record regenerates the recorded fixtures from the running bench. It reaches
// the bench through its loopback ports only, so it opens no kubectl runner, unless
// --state asks it to check the state of the bench: then it reads the upstream, with
// read-only calls, before anything is sent. The order is the one of the refusals:
// the descriptor of this bench, the deny list, the stored secrets, the state, then
// the library, which compares the data set itself. The descriptor is not written.
func (s *session) record() int {
	if s.recordFlags.requestTimeout < 0 {
		return s.fail(&nexusbench.BenchError{
			Kind:  nexusbench.Unclassified,
			Cause: "--request-timeout is negative",
			Check: "give a duration such as 15s or 45s",
		})
	}
	desc, err := s.previousDescriptor()
	if err != nil {
		return s.fail(err)
	}
	deny, err := s.denyTerms()
	if err != nil {
		return s.fail(err)
	}
	secrets, err := s.storedSecrets()
	if err != nil {
		return s.fail(err)
	}
	if err := s.checkBenchState(); err != nil {
		return s.fail(err)
	}
	report, err := s.d.record(s.ctx, nexusbench.RecordOptions{
		Origins:           nexusbench.RecordOrigins(desc, s.restHost),
		Descriptor:        desc,
		Secrets:           secrets,
		Dir:               s.recordFlags.dir,
		Campaigns:         s.recordFlags.campaigns,
		Only:              s.recordFlags.only,
		DenyTerms:         deny,
		State:             s.recordState(),
		AllowCacheEffects: s.recordFlags.allowCacheEffects,
		DatasetAsAdmin:    s.recordFlags.datasetAsAdmin,
		RequestTimeout:    s.recordFlags.requestTimeout,
	})
	if errors.Is(err, context.Canceled) {
		// Nothing is written before every answer passed.
		_, _ = fmt.Fprintln(s.stderr, "cancelled; no fixture was written")
		s.askForReset(s.stderr, report)
		return exitFailure
	}
	if err != nil {
		code := s.fail(err)
		s.askForReset(s.stderr, report)
		return code
	}
	s.printRecordReport(report)
	return exitOK
}

// recordState maps --state to the state the library records in. With no --state
// the run is not filtered, as before the states existed.
func (s *session) recordState() nexusbench.BenchState {
	switch s.recordFlags.state {
	case stateNominal:
		return nexusbench.StateNominal
	case stateUpstreamCut:
		return nexusbench.StateUpstreamCut
	}
	return nexusbench.StateAny
}

// checkBenchState reads the upstream of the bench, with read-only kubectl calls,
// and refuses a bench that is not in the state --state declares. It does nothing
// without --state: such a run reaches for no cluster.
func (s *session) checkBenchState() error {
	declared := s.recordFlags.state
	if declared == "" {
		return nil
	}
	runner, err := s.d.newRunner(s.p.Context)
	if err != nil {
		return stateReadError(err)
	}
	if _, err := s.d.preflight(s.ctx, runner, s.p, nexusbench.OpRecord); err != nil {
		return stateReadError(err)
	}
	got, err := s.d.readUpstream(s.ctx, runner, s.p)
	if err != nil {
		return stateReadError(err)
	}
	if (declared == stateNominal && got == nexusbench.UpstreamReady) || (declared == stateUpstreamCut && got == nexusbench.UpstreamCut) {
		return nil
	}
	return &nexusbench.BenchError{
		Kind:  nexusbench.Unclassified,
		Cause: fmt.Sprintf("the run declares the state %s and the upstream of the bench is %s: no fixture was looked at", declared, upstreamWord(got)),
		Check: stateAdvice(declared, got),
	}
}

// stateReadError turns what stopped the read of the cluster into an error that is
// safe to print: a BenchError says its own cause, anything else could carry what a
// sub-process printed.
func stateReadError(err error) error {
	var be *nexusbench.BenchError
	if errors.As(err, &be) || errors.Is(err, context.Canceled) {
		return err
	}
	return &nexusbench.BenchError{
		Kind:  nexusbench.Unclassified,
		Cause: "the cluster could not be read to check the state of the bench",
		Check: "check kubectl and the context, or leave out --state to record without checking",
		Err:   err,
	}
}

// upstreamWord names the state of the upstream in a message.
func upstreamWord(state nexusbench.UpstreamState) string {
	switch state {
	case nexusbench.UpstreamCut:
		return "cut (no replica)"
	case nexusbench.UpstreamStarting:
		return "starting"
	case nexusbench.UpstreamReady:
		return "ready"
	}
	return "missing"
}

// stateAdvice says what brings the upstream to the state the run declares.
func stateAdvice(declared string, got nexusbench.UpstreamState) string {
	switch {
	case got == nexusbench.UpstreamMissing:
		return upAdvice
	case got == nexusbench.UpstreamStarting:
		return "wait until `mise run nexus-bench:status` says the upstream is ready, then record again"
	case declared == stateNominal:
		return "run `mise run nexus-bench:restore-upstream`, then record again"
	}
	return "run `mise run nexus-bench:cut-upstream`, then record again"
}

// denyTerms reads the external deny list when DEVDESK_FIXTURE_DENYLIST names a
// file. Blank lines and lines that start with # are skipped. The terms are never
// printed.
func (s *session) denyTerms() ([]string, error) {
	file := s.getenv(fixtureDenylistEnv)
	if file == "" {
		return nil, nil
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, &nexusbench.BenchError{
			Kind:  nexusbench.Unclassified,
			Cause: fixtureDenylistEnv + " names a file that cannot be read",
			Check: "check the path and the permissions of the deny list, or unset " + fixtureDenylistEnv,
			Err:   err,
		}
	}
	var terms []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			terms = append(terms, line)
		}
	}
	return terms, nil
}

// storedSecrets opens the secrets store and returns what up stored in it. The
// passwords come from the store only: no flag carries one.
func (s *session) storedSecrets() (nexusbench.Secrets, error) {
	if err := s.resolveSecretsPath(); err != nil {
		return nexusbench.Secrets{}, err
	}
	store, err := s.d.newStore(s.p.SecretsPath)
	if err != nil {
		return nexusbench.Secrets{}, err
	}
	secrets, found, err := store.Load()
	if err != nil {
		return nexusbench.Secrets{}, &nexusbench.BenchError{
			Kind:  nexusbench.Unclassified,
			Cause: "the secrets file of the bench cannot be read",
			Check: "check the permissions and the content of the secrets file, then " + upAdvice,
			Err:   err,
		}
	}
	if !found || secrets.AdminPassword == "" {
		return nexusbench.Secrets{}, &nexusbench.BenchError{
			Kind:  nexusbench.Unclassified,
			Cause: "the bench has no admin password stored: it was never provisioned, or its provisioning did not finish",
			Check: upAdvice,
		}
	}
	return secrets, nil
}

// upAdvice is what to run when the bench was not provisioned.
const upAdvice = "run `mise run nexus-bench:up` to provision the bench"

// printRecordReport names every fixture of the run, then sums up each campaign.
func (s *session) printRecordReport(r nexusbench.RecordReport) {
	type count struct{ written, unchanged, skipped int }
	counts := map[string]*count{}
	tally := func(entry string) *count {
		campaign, _, _ := strings.Cut(entry, "/")
		if counts[campaign] == nil {
			counts[campaign] = &count{}
		}
		return counts[campaign]
	}
	for _, name := range r.Written {
		tally(name).written++
		_, _ = fmt.Fprintf(s.stdout, "  written    %s\n", name)
	}
	for _, name := range r.Unchanged {
		tally(name).unchanged++
		_, _ = fmt.Fprintf(s.stdout, "  unchanged  %s\n", name)
	}
	for _, name := range r.Skipped {
		tally(name).skipped++
		_, _ = fmt.Fprintf(s.stdout, "  skipped    %s\n", name)
	}
	if len(counts) == 0 {
		_, _ = fmt.Fprintln(s.stdout, "record: no fixture to record")
		return
	}
	campaigns := make([]string, 0, len(counts))
	for c := range counts {
		campaigns = append(campaigns, c)
	}
	sort.Strings(campaigns)
	for _, c := range campaigns {
		n := counts[c]
		_, _ = fmt.Fprintf(s.stdout, "record: %s: %d written, %d unchanged, %d skipped\n", c, n.written, n.unchanged, n.skipped)
	}
	s.askForReset(s.stdout, r)
}

// askForReset names the fixtures that were sent with a cache effect and asks for a
// reset of the bench: the proxy can hold an image the data set does not list. It
// says nothing for a run that sent none.
func (s *session) askForReset(w io.Writer, r nexusbench.RecordReport) {
	if len(r.CacheEffects) == 0 {
		return
	}
	for _, name := range r.CacheEffects {
		_, _ = fmt.Fprintf(w, "  cache effect  %s\n", name)
	}
	_, _ = fmt.Fprintln(w, "record: the fixtures above were sent with a cache effect, so docker-proxy can hold an image the data set does not list: run `mise run nexus-bench:reset` before the next recording or measure")
}
