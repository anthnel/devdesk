package explorer

import (
	"testing"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/indicator"
)

const actCtx = "default"

func actRunning(t *testing.T, kind jobs.Kind, ctx, target string) []jobs.Run {
	t.Helper()
	reg := jobs.New()
	reg.Start(jobs.NewImmediateRun(kind, command.ViewGitExplorer, ctx, "x", target))
	return reg.Snapshot()
}

func actFailure(kind jobs.Kind, ctx, target, detail string) jobs.Failure {
	return jobs.Failure{FailureKey: jobs.FailureKey{Kind: kind, Target: target, Context: ctx}, Detail: detail}
}

func TestRowKinds(t *testing.T) {
	tests := []struct {
		name string
		node *TreeNode
		want []jobs.Kind
	}{
		{"real node: delete only (H1)", &TreeNode{FullPath: "alpha/api"}, []jobs.Kind{jobs.KindDelete}},
		{"placeholder: delete and create", &TreeNode{FullPath: "alpha/new", Creating: true}, []jobs.Kind{jobs.KindDelete, jobs.KindCreate}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rowKinds(tt.node)
			if len(got) != len(tt.want) {
				t.Fatalf("rowKinds = %v, want %v", got, tt.want)
			}
			for _, k := range tt.want {
				found := false
				for _, g := range got {
					found = found || g == k
				}
				if !found {
					t.Errorf("rowKinds = %v, missing %v", got, k)
				}
			}
		})
	}
}

func TestRowActivity(t *testing.T) {
	real := &TreeNode{FullPath: "alpha/api"}
	placeholder := &TreeNode{FullPath: "alpha/new", Creating: true}
	realNew := &TreeNode{FullPath: "alpha/new"}

	tests := []struct {
		name     string
		node     *TreeNode
		runs     []jobs.Run
		failures []jobs.Failure
		state    indicator.State
		want     string // Detail.String(); "" = zero detail
	}{
		{"running delete", real, actRunning(t, jobs.KindDelete, actCtx, "alpha/api"), nil,
			indicator.Running, "● Deleting alpha/api"},
		{"running create on a placeholder", placeholder, actRunning(t, jobs.KindCreate, actCtx, "alpha/new"), nil,
			indicator.Running, "● Creating alpha/new"},
		{"running create on a real node (H1)", realNew, actRunning(t, jobs.KindCreate, actCtx, "alpha/new"), nil,
			indicator.Idle, ""},
		{"delete failure", real, nil, []jobs.Failure{actFailure(jobs.KindDelete, actCtx, "alpha/api", "check logs")},
			indicator.Failed, "⊗ alpha/api: last delete failed — check logs"},
		{"create failure on a real node (Q1-C)", realNew, nil, []jobs.Failure{actFailure(jobs.KindCreate, actCtx, "alpha/new", "check logs")},
			indicator.Idle, ""},
		{"failure in another context", real, nil, []jobs.Failure{actFailure(jobs.KindDelete, "other", "alpha/api", "check logs")},
			indicator.Idle, ""},
		{"run in another context", real, actRunning(t, jobs.KindDelete, "other", "alpha/api"), nil,
			indicator.Idle, ""},
		{"index run on the same string", real, actRunning(t, jobs.KindIndex, actCtx, "alpha/api"), nil,
			indicator.Idle, ""},
		{"clone failure on the same string", real, nil, []jobs.Failure{actFailure(jobs.KindClone, actCtx, "alpha/api", "x")},
			indicator.Idle, ""},
		{"running masks a delete failure", real,
			actRunning(t, jobs.KindDelete, actCtx, "alpha/api"),
			[]jobs.Failure{actFailure(jobs.KindDelete, actCtx, "alpha/api", "check logs")},
			indicator.Running, "● Deleting alpha/api · earlier failure: delete (see :jobs)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{jobs: tt.runs, failures: tt.failures, contextName: actCtx}
			got := m.rowActivity(tt.node)
			if got.State != tt.state {
				t.Errorf("State = %v, want %v", got.State, tt.state)
			}
			if s := got.Detail.String(); s != tt.want {
				t.Errorf("Detail = %q, want %q", s, tt.want)
			}
		})
	}
}

func TestHandleJobsChangedKeepsFailuresAndContext(t *testing.T) {
	m := newTestModel(t)
	msg := jobs.ChangedMsg{
		Failures: []jobs.Failure{actFailure(jobs.KindDelete, actCtx, "alpha/api", "check logs")},
		Context:  actCtx,
	}
	m = feed(t, m, msg)
	if m.contextName != actCtx {
		t.Errorf("contextName = %q, want %q", m.contextName, actCtx)
	}
	if len(m.failures) != 1 || m.failures[0].Target != "alpha/api" {
		t.Fatalf("failures = %+v", m.failures)
	}
	msg.Failures[0].Target = "mutated"
	if m.failures[0].Target != "alpha/api" {
		t.Error("failures aliases the broadcast: the model must hold a copy")
	}
	m = feed(t, m, jobs.ChangedMsg{Context: "other"})
	if m.contextName != "other" || len(m.failures) != 0 {
		t.Errorf("a new snapshot replaces the old: context %q, failures %v", m.contextName, m.failures)
	}
}
