package ports

import (
	"context"
	"errors"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"

	gnet "github.com/shirou/gopsutil/v4/net"
)

// TestMeasureIdentityCost is a measuring tool, not a regression test
// (DEVDESK-709, criterion 7): it reads the real socket table and every distinct
// PID's identity with no budget, so the number can be written in
// docs/measurements/. It reads; it never signals.
//
//	Linux/macOS:  DEVDESK_MEASURE_IDENTITY=1 go test ./internal/ports -count=1 -v -run '^TestMeasureIdentityCost$'
//	PowerShell:   $env:DEVDESK_MEASURE_IDENTITY=1; go test ./internal/ports -count=1 -v -run '^TestMeasureIdentityCost$'
func TestMeasureIdentityCost(t *testing.T) {
	if os.Getenv("DEVDESK_MEASURE_IDENTITY") != "1" {
		t.Skip("measuring tool: set DEVDESK_MEASURE_IDENTITY=1 to run it")
	}
	const rounds = 5
	ctx := context.Background()

	startedAt := time.Now()
	conns, err := gnet.ConnectionsWithContext(ctx, "inet")
	if err != nil {
		t.Skipf("the socket table is not readable here: %v", err)
	}
	names := processNames(ctx)
	sockets := make([]Socket, 0, len(conns))
	for _, c := range conns {
		sockets = append(sockets, toSocket(c, names))
	}
	withoutIdentity := time.Since(startedAt)

	var pids []int
	for _, s := range sockets {
		if n, err := strconv.Atoi(s.PID); err == nil && n > 0 && !slices.Contains(pids, n) {
			pids = append(pids, n)
		}
	}
	slices.Sort(pids)

	var (
		durations           []time.Duration
		denied, gone, other int
	)
	for round := range rounds {
		denied, gone, other = 0, 0, 0
		roundStart := time.Now()
		for _, pid := range pids {
			if _, err := (realOS{}).identity(pid); err != nil {
				switch {
				case errors.Is(err, os.ErrPermission):
					denied++
				case errors.Is(err, os.ErrProcessDone):
					gone++
				default:
					other++
				}
			}
		}
		durations = append(durations, time.Since(roundStart))
		t.Logf("round %d: %v for %d PIDs", round+1, durations[round], len(pids))
	}
	slices.Sort(durations)

	listStart := time.Now()
	if _, err := List(ctx, false); err != nil {
		t.Logf("List: %v", err)
	}
	listWithIdentity := time.Since(listStart)

	t.Logf("sockets: %d, distinct PIDs: %d", len(sockets), len(pids))
	t.Logf("failures in the last round: permission %d, gone %d, other %d", denied, gone, other)
	t.Logf("identity reads per round: min %v, median %v, max %v", durations[0], durations[len(durations)/2], durations[len(durations)-1])
	t.Logf("table and names without identity: %v; List with identity: %v", withoutIdentity, listWithIdentity)
}
