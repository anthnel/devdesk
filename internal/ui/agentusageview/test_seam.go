package agentusageview

import (
	"testing"
	"time"
)

// PollIntervalForTest returns the poll interval so a test in another package
// (the router's) can shorten it with testutil.FastTimers before the Update that
// builds the poll Cmd: pollTickCmd freezes the value when it is built. It
// panics outside a test binary, like credentials.SwapHostStoreForTest;
// production never calls it.
func PollIntervalForTest() *time.Duration {
	if !testing.Testing() {
		panic("agentusageview: PollIntervalForTest called outside a test binary")
	}
	return &pollInterval
}
