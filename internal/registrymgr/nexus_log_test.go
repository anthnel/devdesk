package registrymgr

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/oci"
)

// DEVDESK-412 step 8 (group E): the log line of an unreachable format endpoint
// advises an admin account. That advice is for a Nexus that refused the account
// (403); after a refused redirect to another origin it is wrong and misleading.
const nexusAdminAdvice = "nx-repository-admin"

func TestNexusLogHasNoAdminAdviceAfterAnOriginRefusal(t *testing.T) {
	logs := nexusLog(t)
	env := startNexus(t, "other port", func(w http.ResponseWriter, r *http.Request, other string) {
		if !strings.Contains(r.URL.Path, "/group/") {
			_, _ = io.WriteString(w, nexusGroupNoMembers)
			return
		}
		http.Redirect(w, r, other+nexusLeakPath+"?"+nexusSigned, http.StatusTemporaryRedirect)
	})

	_, _ = (&NexusDetector{}).DetectGroup(context.Background(), env.info())

	if !strings.Contains(logs.String(), oci.ErrOffOriginRedirect.Error()) {
		t.Fatalf("the log does not mention the origin refusal (the test checks nothing): %s", logs.String())
	}
	for _, advice := range []string{nexusAdminAdvice, "docker login"} {
		if strings.Contains(logs.String(), advice) {
			t.Errorf("the log gives the advice %q after an origin refusal: %s", advice, logs.String())
		}
	}
}

// A 403 from Nexus keeps its advice.
func TestNexusLogKeepsTheAdminAdviceOnARefusedAccount(t *testing.T) {
	logs := nexusLog(t)
	env := startNexus(t, "other port", func(w http.ResponseWriter, r *http.Request, _ string) {
		if !strings.Contains(r.URL.Path, "/group/") {
			_, _ = io.WriteString(w, nexusGroupNoMembers)
			return
		}
		w.WriteHeader(http.StatusForbidden)
	})

	members, err := (&NexusDetector{}).DetectGroup(context.Background(), env.info())

	if err != nil || len(members) != 0 {
		t.Fatalf("DetectGroup = %v, %v, want no members and no error", members, err)
	}
	for _, advice := range []string{nexusAdminAdvice, "docker login"} {
		if !strings.Contains(logs.String(), advice) {
			t.Errorf("the log lost the advice %q for a 403: %s", advice, logs.String())
		}
	}
}
