package nexusbench

import (
	"context"
	"slices"
	"strings"
	"testing"
)

// The preflight of the recorder (DEVDESK-580, PR2, step 7c): OpRecord, appended
// after OpReset. `nexus-bench record --state ...` reads the Deployment and the
// pods of the upstream (ReadUpstream) to compare the state of the bench with the
// one the run declares, and nothing else: no write, no tunnel, no Service, no
// claim, no node. Same scripted kubectl as preflight_upstream_ops_test.go.

// recordQuestions are the questions of OpRecord, in the order the tests expect.
var recordQuestions = []string{"get deployments", "list pods"}

func TestRunPreflightOpRecordIsAppended(t *testing.T) {
	if OpRecord != OpReset+1 {
		t.Errorf("OpReset, OpRecord = %d, %d: want OpRecord right after OpReset", OpReset, OpRecord)
	}
	for v, want := range []Op{OpUp, OpProvision, OpDown, OpStatus, OpSeed, OpCut, OpRestore, OpReset} {
		if want != Op(v) {
			t.Errorf("operation #%d = %d: operations are appended, never renumbered", v, want)
		}
	}
}

func TestRunPreflightOpRecordAsksToReadTheUpstreamOnly(t *testing.T) {
	p := Defaults()
	f := newFakeKubectl(t, p).ours()
	res, err := RunPreflight(context.Background(), f, p, OpRecord)
	if err != nil {
		t.Fatalf("RunPreflight: %v", err)
	}
	if got, want := asked(f), inNamespace(p, recordQuestions...); !slices.Equal(got, want) {
		t.Errorf("questions =\n  %q\nwant\n  %q", got, want)
	}
	if want := (PreflightResult{NamespaceExists: true, NamespaceOurs: true}); res != want {
		t.Errorf("result = %+v, want %+v (no node address: the recorder goes through the loopback)", res, want)
	}
	for _, q := range f.asked {
		if q.allNamespaces || q.namespace != p.Namespace {
			t.Errorf("question %q is not scoped to the namespace of the run", q)
		}
	}
	for _, line := range f.callLines() {
		if strings.Contains(line, "nodes") || strings.HasPrefix(line, "get services") {
			t.Errorf("record issued %q: it needs no node and no Service", line)
		}
	}
	requireReadOnly(t, f)
}

func TestOpRecordDoesNotReadTheNode(t *testing.T) {
	if OpRecord.readsNode() {
		t.Error("OpRecord.readsNode() = true, want false")
	}
}

func TestRunPreflightOpRecordHonorsTheNamespaceParameter(t *testing.T) {
	p := Defaults()
	p.Namespace = "nexus-bench-2"
	p.UpstreamURL = upstreamFor(p.Namespace)
	f := newFakeKubectl(t, p).ours()
	if _, err := RunPreflight(context.Background(), f, p, OpRecord); err != nil {
		t.Fatalf("RunPreflight: %v", err)
	}
	got := asked(f)
	if len(got) != len(recordQuestions) {
		t.Fatalf("asked %d questions, want %d: %q", len(got), len(recordQuestions), got)
	}
	for _, q := range got {
		if !strings.HasSuffix(q, " -n nexus-bench-2") {
			t.Errorf("question %q is not scoped to the namespace of the run", q)
		}
	}
}

func TestRunPreflightOpRecordStopsAtTheFirstRefusedQuestion(t *testing.T) {
	all := inNamespace(Defaults(), recordQuestions...)
	for i, q := range recordQuestions {
		verb, resource := denialOf(q)
		t.Run(verb+" "+resource, func(t *testing.T) {
			p := Defaults()
			f := newFakeKubectl(t, p).ours().deny(verb, resource)
			_, err := RunPreflight(context.Background(), f, p, OpRecord)
			be := requireDeployKind(t, err, Forbidden)
			requireMentions(t, err, append([]string{verb}, strings.Split(resource, "/")...), nil)
			requireRepeatsTheQuestion(t, be.Check, verb, resource)
			if got, want := asked(f), all[:i+1]; !slices.Equal(got, want) {
				t.Errorf("questions = %q, want them to stop at the refused one: %q", got, want)
			}
			requireReadOnly(t, f)
		})
	}
}

func TestRunPreflightOpRecordRefusesTheNamespace(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(f *fakeKubectl)
		kind    ErrKind
		mention []string
	}{
		{"absent", func(f *fakeKubectl) {}, NamespaceMissing, []string{"nexus-bench", "does not exist"}},
		{
			"foreign: managed by someone else",
			func(f *fakeKubectl) {
				f.withNamespace(map[string]string{managedByKV: "helm", "kubernetes.io/metadata.name": f.p.Namespace})
			},
			NamespaceForeign, []string{"nexus-bench", managedByKV},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Defaults()
			f := newFakeKubectl(t, p)
			tt.setup(f)
			_, err := RunPreflight(context.Background(), f, p, OpRecord)
			_ = requireDeployKind(t, err, tt.kind)
			requireMentions(t, err, tt.mention, nil)
			requireReadOnly(t, f)
		})
	}
}
