package ports

import (
	"testing"
)

// Helpers for the tests of this package that reach the process seams
// (DEVDESK-708). The seams are package-level variables, so no test in this
// package may call t.Parallel: two tests swapping them would see each other.

// fakeOS is a substitute osProcs. It records every PID it was asked to kill and
// never signals anything.
type fakeOS struct {
	created   map[int]int64 // creation time per PID, in ms; opaque
	createErr map[int]error // a read error per PID
	killErr   error         // returned by every kill
	kills     []int         // PIDs kill was called with, in order
}

func (f *fakeOS) createTime(pid int) (int64, error) {
	if err, ok := f.createErr[pid]; ok {
		return 0, err
	}
	return f.created[pid], nil
}

func (f *fakeOS) kill(pid int) error {
	f.kills = append(f.kills, pid)
	return f.killErr
}

// useFakeOS replaces the four seams (selfPID, parentPID, goos, procOS) and
// restores them when the test ends.
func useFakeOS(t *testing.T, self, parent int, goosName string) *fakeOS {
	t.Helper()
	oldSelf, oldParent, oldGOOS, oldProc := selfPID, parentPID, goos, procOS
	t.Cleanup(func() {
		selfPID, parentPID, goos, procOS = oldSelf, oldParent, oldGOOS, oldProc
	})

	fake := &fakeOS{
		created:   map[int]int64{},
		createErr: map[int]error{},
	}
	selfPID = func() int { return self }
	parentPID = func() int { return parent }
	goos = goosName
	procOS = fake
	return fake
}

// allowRealSignals lets realOS.kill send a real signal for the rest of this
// test. Only a test that signals a child it started itself may call it; never
// one that aims at any other PID.
func allowRealSignals(t *testing.T) {
	t.Helper()
	realSignalsAllowed.Store(true)
	t.Cleanup(func() { realSignalsAllowed.Store(false) })
}
