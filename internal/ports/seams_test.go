package ports

import (
	"os"
	"testing"
	"time"
)

// Helpers for the tests of this package that reach the process seams
// (DEVDESK-708, DEVDESK-709). The seams are package-level variables, so no test
// in this package may call t.Parallel: two tests swapping them would see each
// other.

// fakeEpoch is the instant a fakeOS clock starts at.
var fakeEpoch = time.Date(2026, time.November, 23, 12, 0, 0, 0, time.UTC)

// fakeOS is a substitute osProcs. It records every PID it was asked to kill and
// never signals anything.
type fakeOS struct {
	created   map[int]int64 // creation time per PID, in ms; opaque
	createErr map[int]error // a read error per PID

	ids     map[int]uint64 // raw identity per PID; a PID with no entry reads as gone
	idErr   map[int]error  // an identity read error per PID
	idCalls []int          // PIDs identity was called with, in order

	status     map[int]procStatus // what probe reads per PID; a PID with no entry reads as gone
	statusErr  map[int]error      // a probe read error per PID
	probeCalls []int              // PIDs probe was called with, in order

	killErr    error        // returned by every kill
	kills      []int        // PIDs kill was called with, in order
	killedWith []Identity   // the identity kill was called with, in order
	sigs       []stopSignal // the signal kill was asked to send, in order

	clock    time.Time     // what the `now` seam answers
	readStep time.Duration // how far every identity read moves the clock
}

func (f *fakeOS) createTime(pid int) (int64, error) {
	if err, ok := f.createErr[pid]; ok {
		return 0, err
	}
	return f.created[pid], nil
}

func (f *fakeOS) identity(pid int) (uint64, error) {
	f.idCalls = append(f.idCalls, pid)
	f.clock = f.clock.Add(f.readStep)
	if err, ok := f.idErr[pid]; ok {
		return 0, err
	}
	raw, ok := f.ids[pid]
	if !ok {
		return 0, os.ErrProcessDone
	}
	return raw, nil
}

// probe answers from status and statusErr and, like the real one, sends nothing.
func (f *fakeOS) probe(pid int) (procStatus, error) {
	f.probeCalls = append(f.probeCalls, pid)
	if err, ok := f.statusErr[pid]; ok {
		return procStatus{}, err
	}
	st, ok := f.status[pid]
	if !ok {
		return procStatus{}, os.ErrProcessDone
	}
	return st, nil
}

func (f *fakeOS) kill(pid int, want Identity, sig stopSignal) error {
	f.kills = append(f.kills, pid)
	f.killedWith = append(f.killedWith, want)
	f.sigs = append(f.sigs, sig)
	return f.killErr
}

// useFakeOS replaces the five seams (selfPID, parentPID, goos, now, procOS) and
// restores them when the test ends.
func useFakeOS(t *testing.T, self, parent int, goosName string) *fakeOS {
	t.Helper()
	oldSelf, oldParent, oldGOOS, oldNow, oldProc := selfPID, parentPID, goos, now, procOS
	t.Cleanup(func() {
		selfPID, parentPID, goos, now, procOS = oldSelf, oldParent, oldGOOS, oldNow, oldProc
	})

	fake := &fakeOS{
		created:   map[int]int64{},
		createErr: map[int]error{},
		ids:       map[int]uint64{},
		idErr:     map[int]error{},
		status:    map[int]procStatus{},
		statusErr: map[int]error{},
		clock:     fakeEpoch,
	}
	selfPID = func() int { return self }
	parentPID = func() int { return parent }
	goos = goosName
	now = func() time.Time { return fake.clock }
	procOS = fake
	return fake
}

// filetimeOf is the Windows FILETIME (100 ns ticks since 1601) of an instant.
func filetimeOf(t time.Time) uint64 {
	const unixEpochAsFiletime = 116444736000000000
	return uint64(t.UnixNano()/100) + unixEpochAsFiletime //nolint:gosec // test instants are after 1970
}

// allowRealSignals lets realOS.kill send a real signal for the rest of this
// test. Only a test that signals a child it started itself may call it; never
// one that aims at any other PID.
func allowRealSignals(t *testing.T) {
	t.Helper()
	realSignalsAllowed.Store(true)
	t.Cleanup(func() { realSignalsAllowed.Store(false) })
}
