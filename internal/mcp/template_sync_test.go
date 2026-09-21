package mcp

import (
	"errors"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/jobs"
)

func TestTemplateSyncStartAsksTheSessionForOneSlugAndReturnsTheJob(t *testing.T) {
	session := &fakeSession{startedID: 12}
	env := testEnv(nil)
	env.Dispatch = session

	var out actionOut
	callTool(t, connect(t, env), "template_sync_start", map[string]any{"slug": "spring-api"}, &out)

	if out.JobID != 12 || out.Context != "work" {
		t.Errorf("answer = %+v, want job 12 in context work", out)
	}
	if len(session.started) != 1 || session.started[0].Tool != "template_sync_start" ||
		len(session.started[0].Targets) != 1 || session.started[0].Targets[0] != "spring-api" {
		t.Errorf("the session was asked %+v, want one template_sync_start for spring-api", session.started)
	}
}

// The view's own sentence reaches the agent, prefixed with the tool.
func TestTemplateSyncStartPassesTheViewsRefusalOn(t *testing.T) {
	env := testEnv(nil)
	env.Dispatch = &fakeSession{startErr: errors.New("A sync of this template is already running")}

	msg := callToolExpectingError(t, connect(t, env), "template_sync_start", map[string]any{"slug": "x"})
	if !strings.Contains(msg, "template_sync_start") || !strings.Contains(msg, "already running") {
		t.Errorf("error = %s", msg)
	}
}

func TestTemplateSyncStartRefusesWithoutASession(t *testing.T) {
	msg := callToolExpectingError(t, connect(t, testEnv(nil)), "template_sync_start", map[string]any{"slug": "x"})
	if !strings.Contains(msg, ErrNoSession.Error()) {
		t.Errorf("error = %s", msg)
	}
}

// The answer is a job id. There is no field a credential could travel in, and
// the run it starts reports targets by slug.
func TestTemplateSyncStartAnswersWithNothingButTheJob(t *testing.T) {
	env := testEnv(nil)
	env.Dispatch = &fakeSession{startedID: jobs.JobID(1)}

	raw := callToolRaw(t, connect(t, env), "template_sync_start", map[string]any{"slug": "x"})
	for _, banned := range []string{"token", "password", "credential"} {
		if strings.Contains(strings.ToLower(raw), banned) {
			t.Errorf("the answer mentions %q: %s", banned, raw)
		}
	}
}
