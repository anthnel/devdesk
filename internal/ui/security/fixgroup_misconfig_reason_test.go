package security

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/scan"
)

// DEVDESK-754 (SC2): a built-in fix the catalog declines for different reasons is
// one row per reason, and a declined fix with no reason says the bare word.

func TestAGroupHoldsOnlyFindingsTheFixDeclinesForTheSameReason(t *testing.T) {
	findings := []scan.Finding{
		mgFinding("KSV-0104", "pod.yaml", "a", mgHigh),
		mgFinding("KSV-0104", "pod.yaml", "b", mgHigh),
		mgFinding("KSV-0104", "pod.yaml", "c", mgHigh),
	}
	reasons := map[string]string{"a": "first reason", "b": "second reason", "c": "first reason"}
	groups := groupMisconfigs(findings, true, func(f scan.Finding) fixStatus {
		return fixStatus{State: fixBuiltInDeclined, Reason: reasons[f.Message]}
	})
	if len(groups) != 2 {
		t.Fatalf("%d groups, want one per reason: %v", len(groups), rowsOf(groups))
	}
	if groups[0].Key == groups[1].Key {
		t.Errorf("both groups have the key %+v", groups[0].Key)
	}
	// The two members that agree share their row, which comes first (more findings).
	if got := messagesOf(groups[0].Members); len(got) != 2 || got[0] != "a" || got[1] != "c" {
		t.Errorf("first group holds %v, want a and c", got)
	}
	if got := groups[0].Status.Label(); got != "Built-in fix unavailable: first reason" {
		t.Errorf("label = %q", got)
	}
}

func TestADeclinedFixWithNoReasonSaysTheBareLabel(t *testing.T) {
	got := fixStatus{State: fixBuiltInDeclined}.Label()
	if got != labelBuiltInUnavailable || strings.Contains(got, ":") {
		t.Errorf("label = %q, want %q with no colon", got, labelBuiltInUnavailable)
	}
}
