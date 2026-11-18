package main

import (
	"context"
	"errors"
	"fmt"
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
// the bench through its loopback ports only, so it opens no kubectl runner. The
// order is the one of the refusals: the descriptor of this bench, the deny list,
// the stored secrets, then the library, which compares the data set itself. The
// descriptor is not written.
func (s *session) record() int {
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
	report, err := s.d.record(s.ctx, nexusbench.RecordOptions{
		Origins:    nexusbench.RecordOrigins(desc, s.restHost),
		Descriptor: desc,
		Secrets:    secrets,
		Dir:        s.recordFlags.dir,
		Campaigns:  s.recordFlags.campaigns,
		Only:       s.recordFlags.only,
		DenyTerms:  deny,
	})
	if errors.Is(err, context.Canceled) {
		// Nothing is written before every answer passed.
		_, _ = fmt.Fprintln(s.stderr, "cancelled; no fixture was written")
		return exitFailure
	}
	if err != nil {
		return s.fail(err)
	}
	s.printRecordReport(report)
	return exitOK
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
}
