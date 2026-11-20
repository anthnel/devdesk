package main

// DEVDESK-580, PR4 (W6), the command half of `nexus-bench record`: --dataset-as-admin
// sets RecordOptions.DatasetAsAdmin, so that the data set of a bench whose anonymous
// access is cut is compared with the admin listing. It is a flag of record only,
// off by default (nothing changes without it), it takes no value, and it needs no
// cluster access of its own (the stored secrets, which the run reads anyway, hold
// the admin password).

import (
	"slices"
	"strings"
	"testing"
)

func TestRecordDatasetAsAdminIsOffByDefault(t *testing.T) {
	r := newRecordRig(t)
	if code, stdout, stderr := r.run(); code != 0 {
		t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if r.opts.DatasetAsAdmin {
		t.Error("DatasetAsAdmin is on with no flag: the comparison must stay anonymous")
	}
}

func TestRecordPassesDatasetAsAdminToTheLibrary(t *testing.T) {
	for _, args := range [][]string{
		{"--dataset-as-admin"},
		{"--dataset-as-admin=true"},
		{"--allow-cache-effects", "--dataset-as-admin", "--request-timeout", "45s", "--campaign", "access"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			r := newRecordRig(t)
			code, stdout, stderr := r.run(args...)
			if code != 0 {
				t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
			}
			if r.calls != 1 {
				t.Fatalf("record was called %d time(s), want 1", r.calls)
			}
			if !r.opts.DatasetAsAdmin {
				t.Error("DatasetAsAdmin is off, want it on")
			}
			requireClean(t, stdout, stderr)
		})
	}
}

func TestRecordDatasetAsAdminFalseKeepsTheAnonymousComparison(t *testing.T) {
	r := newRecordRig(t)
	if code, stdout, stderr := r.run("--dataset-as-admin=false"); code != 0 {
		t.Fatalf("exit code = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if r.opts.DatasetAsAdmin {
		t.Error("DatasetAsAdmin is on with --dataset-as-admin=false")
	}
}

// Like --allow-cache-effects, the flag reaches for no cluster.
func TestRecordDatasetAsAdminNeverReachesForTheCluster(t *testing.T) {
	r := newRecordRig(t)
	code, stdout, stderr := r.run("--dataset-as-admin")
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

func TestRecordDatasetAsAdminIsABooleanFlag(t *testing.T) {
	for _, args := range [][]string{
		{"--dataset-as-admin=maybe"},
		{"--dataset-as-admin", "--dataset-as-admin=maybe"},
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
			r.h.requireNoCalls()
			r.requireRefusedBeforeRecord()
		})
	}
}

func TestDatasetAsAdminBelongsToRecordOnly(t *testing.T) {
	for _, sub := range []string{"seed", "up", "status", "reset", "cut-upstream", "restore-upstream"} {
		t.Run(sub, func(t *testing.T) {
			r := newRecordRig(t)
			code, stdout, stderr := r.runCommand(sub, "--dataset-as-admin")
			if code != 2 {
				t.Errorf("exit code = %d, want 2", code)
			}
			if stdout != "" || strings.TrimSpace(stderr) == "" {
				t.Errorf("stdout %q, stderr %q: want an empty stdout and a message", stdout, stderr)
			}
			r.h.requireNoCalls()

			code, helpOut, _ := r.runCommand(sub, "-h")
			if code != 0 {
				t.Fatalf("help exit code = %d", code)
			}
			if strings.Contains(helpOut, "dataset-as-admin") {
				t.Errorf("the help of %s lists --dataset-as-admin:\n%s", sub, helpOut)
			}
		})
	}
}

func TestRecordHelpExplainsDatasetAsAdmin(t *testing.T) {
	r := newRecordRig(t)
	code, stdout, stderr := r.run("-h")
	if code != 0 {
		t.Fatalf("exit code = %d\nstderr: %s", code, stderr)
	}
	_, flags, _ := strings.Cut(stdout, "Flags:")
	_, after, found := strings.Cut(flags, "-dataset-as-admin")
	if !found {
		t.Fatalf("record -h lists no --dataset-as-admin:\n%s", stdout)
	}
	entry, _, _ := strings.Cut(after, "\n  -")
	entry = strings.ToLower(entry)
	if !strings.Contains(entry, "anonymous") || !strings.Contains(entry, "admin") {
		t.Errorf("the --dataset-as-admin help does not say it replaces the anonymous listing with the admin one:\n%s", entry)
	}
	r.h.requireNoCalls()
}
