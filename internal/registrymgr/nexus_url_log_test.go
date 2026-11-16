package registrymgr

import (
	"context"
	"strings"
	"testing"
)

// DEVDESK-412 final audit: a Nexus URL that cannot be read is a settled answer,
// and its log line gives the cause, not the URL (it may hold a password).
func TestNexusUnreadableURLIsLoggedWithoutTheURL(t *testing.T) {
	logs := nexusLog(t)
	info := RegistryInfo{Provider: ProviderNexus, URL: "https://alice:" + nexusTestPass + "@nexus.example/%zz/repository/docker-group"}

	members, err := (&NexusDetector{}).DetectGroup(context.Background(), info)

	if err != nil || len(members) != 0 {
		t.Fatalf("DetectGroup = %v, %v, want no members and no error", members, err)
	}
	if !strings.Contains(logs.String(), "cannot parse Nexus URL") {
		t.Fatalf("the failure was not logged (the test checks nothing): %q", logs.String())
	}
	for _, leak := range []string{nexusTestPass, "alice", "nexus.example"} {
		if strings.Contains(logs.String(), leak) {
			t.Errorf("the log holds %q: %s", leak, logs.String())
		}
	}
}
