package ports

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
)

// Tests of the processes DevDesk refuses to signal (DEVDESK-708, S-01a
// criteria 1 to 4). None of them sends a real signal: every one substitutes the
// seams through useFakeOS, and none aims at the real PID 1 or the real parent.

// TestReasonsAreTheAgreedWording pins the four sentences of the stories, word
// for word, and the zero value's silence.
func TestReasonsAreTheAgreedWording(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"constant self", ReasonSelf, "This is DevDesk itself"},
		{"constant parent", ReasonParent, "This process started DevDesk"},
		{"constant init", ReasonInit, "PID 1 is the system's init process"},
		{"constant windows", ReasonWindowsSystem, "Windows system process"},
		{"Reason self", ProtectedSelf.Reason(), "This is DevDesk itself"},
		{"Reason parent", ProtectedParent.Reason(), "This process started DevDesk"},
		{"Reason init", ProtectedInit.Reason(), "PID 1 is the system's init process"},
		{"Reason windows", ProtectedWindowsSystem.Reason(), "Windows system process"},
		{"Reason unprotected", Unprotected.Reason(), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %q, want %q", tt.got, tt.want)
			}
		})
	}
}

// TestUnprotectedIsTheZeroValue: a Protection nobody set means "not protected".
func TestUnprotectedIsTheZeroValue(t *testing.T) {
	var p Protection
	if p != Unprotected {
		t.Errorf("zero Protection = %d, want Unprotected (%d)", p, Unprotected)
	}
}

// TestProtectionFor is the pure rule: who is refused, in which order, and when
// the parent's creation time is read at all.
func TestProtectionFor(t *testing.T) {
	tests := []struct {
		name            string
		pid, self, par  int
		windows         bool
		parentBornFirst bool
		want            Protection
		wantReads       int // calls to parentBornFirst
	}{
		{"DevDesk itself", 500, 500, 400, false, false, ProtectedSelf, 0},
		{"parent born before DevDesk", 400, 500, 400, false, true, ProtectedParent, 1},
		{"parent born after DevDesk: PID reused", 400, 500, 400, false, false, Unprotected, 1},
		{"PID 1", 1, 500, 400, false, false, ProtectedInit, 0},
		{"parent is PID 1: init wins", 1, 500, 1, false, true, ProtectedInit, 0},
		{"DevDesk is PID 1 (container)", 1, 1, 0, false, false, ProtectedSelf, 0},
		{"PID 4 on Windows", 4, 500, 400, true, false, ProtectedWindowsSystem, 0},
		{"PID 0 on Windows", 0, 500, 400, true, false, ProtectedWindowsSystem, 0},
		{"PID 4 off Windows is nothing special", 4, 500, 400, false, false, Unprotected, 0},
		{"unknown parent (0) is ignored", 0, 500, 0, false, true, Unprotected, 0},
		{"unknown parent (0) is ignored on another PID", 4242, 500, 0, false, true, Unprotected, 0},
		{"unknown parent (0) on Windows still protects PID 0", 0, 500, 0, true, true, ProtectedWindowsSystem, 0},
		{"any other PID", 4242, 500, 400, false, true, Unprotected, 0},
		{"any other PID on Windows", 4242, 500, 400, true, true, Unprotected, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reads := 0
			h := hostView{
				self:    tt.self,
				parent:  tt.par,
				windows: tt.windows,
				parentBornFirst: func() bool {
					reads++
					return tt.parentBornFirst
				},
			}
			if got := protectionFor(tt.pid, h); got != tt.want {
				t.Errorf("protectionFor(%d) = %d (%q), want %d (%q)",
					tt.pid, got, got.Reason(), tt.want, tt.want.Reason())
			}
			if reads != tt.wantReads {
				t.Errorf("parentBornFirst called %d times, want %d: the creation time is read only for the parent's own PID", reads, tt.wantReads)
			}
		})
	}
}

// TestParentStartedFirst: the parent counts as the one that started DevDesk
// unless both creation times prove it was born after. Anything unreadable, and
// a tie, is protected (decision Q1, prudence).
func TestParentStartedFirst(t *testing.T) {
	const self, parent = 500, 400
	errDenied := errors.New("access denied")
	tests := []struct {
		name      string
		created   map[int]int64
		createErr map[int]error
		want      bool
	}{
		{"parent born before", map[int]int64{parent: 1000, self: 2000}, nil, true},
		{"parent born after: its PID was reused", map[int]int64{parent: 3000, self: 2000}, nil, false},
		{"same millisecond: protected", map[int]int64{parent: 2000, self: 2000}, nil, true},
		{"parent time unreadable: protected", map[int]int64{self: 2000}, map[int]error{parent: errDenied}, true},
		{"own time unreadable: protected", map[int]int64{parent: 3000}, map[int]error{self: errDenied}, true},
		{"both unreadable: protected", nil, map[int]error{parent: errDenied, self: errDenied}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := useFakeOS(t, self, parent, "linux")
			for pid, ms := range tt.created {
				fake.created[pid] = ms
			}
			for pid, err := range tt.createErr {
				fake.createErr[pid] = err
			}
			if got := parentStartedFirst(parent, self); got != tt.want {
				t.Errorf("parentStartedFirst(%d, %d) = %v, want %v", parent, self, got, tt.want)
			}
			if len(fake.kills) != 0 {
				t.Errorf("reading creation times signalled %v", fake.kills)
			}
		})
	}
}

// TestProtectionOfReadsTheSeams: the exported entry point feeds the rule from
// selfPID, parentPID, goos and procOS, and answers with the exact reason.
func TestProtectionOfReadsTheSeams(t *testing.T) {
	const self, parent = 500, 400
	tests := []struct {
		name       string
		goos       string
		parentTime int64 // creation time given to the parent; DevDesk's is 2000
		pid        string
		want       Protection
		wantReason string
	}{
		{"DevDesk itself", "linux", 1000, "500", ProtectedSelf, "This is DevDesk itself"},
		{"parent born before", "linux", 1000, "400", ProtectedParent, "This process started DevDesk"},
		{"parent born after", "linux", 3000, "400", Unprotected, ""},
		{"PID 1", "linux", 1000, "1", ProtectedInit, "PID 1 is the system's init process"},
		{"PID 4 on Windows", "windows", 1000, "4", ProtectedWindowsSystem, "Windows system process"},
		{"PID 0 on Windows", "windows", 1000, "0", ProtectedWindowsSystem, "Windows system process"},
		{"PID 4 on Linux", "linux", 1000, "4", Unprotected, ""},
		{"PID 0 on Linux", "linux", 1000, "0", Unprotected, ""},
		{"ordinary PID", "linux", 1000, "4242", Unprotected, ""},
		{"empty PID", "linux", 1000, "", Unprotected, ""},
		{"not a number", "linux", 1000, "abc", Unprotected, ""},
		{"negative", "linux", 1000, "-1", Unprotected, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := useFakeOS(t, self, parent, tt.goos)
			fake.created[parent] = tt.parentTime
			fake.created[self] = 2000

			got := ProtectionOf(tt.pid)
			if got != tt.want {
				t.Errorf("ProtectionOf(%q) = %d, want %d", tt.pid, got, tt.want)
			}
			if got.Reason() != tt.wantReason {
				t.Errorf("ProtectionOf(%q).Reason() = %q, want %q", tt.pid, got.Reason(), tt.wantReason)
			}
			if len(fake.kills) != 0 {
				t.Errorf("ProtectionOf signalled %v", fake.kills)
			}
		})
	}
}

// TestProtectionOfReadsNoCreationTimeForAnOrdinaryPID: ProtectionOf runs on
// every render of the header, so the creation time (OpenProcess on Windows) is
// read only when the PID is the parent's.
func TestProtectionOfReadsNoCreationTimeForAnOrdinaryPID(t *testing.T) {
	fake := useFakeOS(t, 500, 400, "linux")
	counter := &countingOS{fakeOS: fake}
	procOS = counter

	for _, pid := range []string{"500", "1", "4242", "0", "", "abc"} {
		ProtectionOf(pid)
	}
	if counter.reads != 0 {
		t.Errorf("createTime was read %d times for PIDs other than the parent", counter.reads)
	}

	ProtectionOf("400")
	if counter.reads == 0 {
		t.Error("createTime was not read for the parent's PID: the born-before rule cannot be applied")
	}
}

// countingOS counts creation-time reads on top of a fakeOS.
type countingOS struct {
	*fakeOS
	reads int
}

func (c *countingOS) createTime(pid int) (int64, error) {
	c.reads++
	return c.fakeOS.createTime(pid)
}

// TestProtectedErrorMessage: the error names the PID and the reason, and is
// classifiable through errors.As (also when wrapped), so the UI can tell a
// refusal from a failure.
func TestProtectedErrorMessage(t *testing.T) {
	var err error = &ProtectedError{PID: 4242, Protection: ProtectedSelf}
	if got, want := err.Error(), "PID 4242 not stopped: This is DevDesk itself"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}

	wrapped := fmt.Errorf("while stopping: %w", err)
	var pe *ProtectedError
	if !errors.As(wrapped, &pe) {
		t.Fatal("errors.As does not find a *ProtectedError through a wrapper")
	}
	if pe.PID != 4242 || pe.Protection != ProtectedSelf {
		t.Errorf("got PID %d, protection %d", pe.PID, pe.Protection)
	}
}

// TestKillRefusesAProtectedPIDWithoutSignalling is S-01a criterion 4: a direct
// call to Kill refuses DevDesk, its direct parent born before it, PID 1, and
// PIDs 0 and 4 on Windows. It names the reason and sends no signal.
func TestKillRefusesAProtectedPIDWithoutSignalling(t *testing.T) {
	const self, parent = 500, 400
	errDenied := errors.New("access denied")
	tests := []struct {
		name      string
		goos      string
		created   map[int]int64
		createErr map[int]error
		pid       string
		want      Protection
	}{
		{"DevDesk itself", "linux", nil, nil, "500", ProtectedSelf},
		{"DevDesk itself on Windows", "windows", nil, nil, "500", ProtectedSelf},
		{"parent born before", "linux", map[int]int64{parent: 1000, self: 2000}, nil, "400", ProtectedParent},
		{"parent born at the same time", "linux", map[int]int64{parent: 2000, self: 2000}, nil, "400", ProtectedParent},
		{"parent whose creation time cannot be read", "linux", map[int]int64{self: 2000}, map[int]error{parent: errDenied}, "400", ProtectedParent},
		{"parent, DevDesk's creation time unreadable", "linux", map[int]int64{parent: 3000}, map[int]error{self: errDenied}, "400", ProtectedParent},
		{"parent born before, on Windows", "windows", map[int]int64{parent: 1000, self: 2000}, nil, "400", ProtectedParent},
		{"PID 1", "linux", nil, nil, "1", ProtectedInit},
		{"PID 1 on Windows", "windows", nil, nil, "1", ProtectedInit},
		{"PID 4 on Windows", "windows", nil, nil, "4", ProtectedWindowsSystem},
		{"PID 0 on Windows", "windows", nil, nil, "0", ProtectedWindowsSystem},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := useFakeOS(t, self, parent, tt.goos)
			for pid, ms := range tt.created {
				fake.created[pid] = ms
			}
			for pid, err := range tt.createErr {
				fake.createErr[pid] = err
			}

			err := Kill(tt.pid)
			if err == nil {
				t.Fatalf("Kill(%q) returned nil, want a refusal", tt.pid)
			}
			var pe *ProtectedError
			if !errors.As(err, &pe) {
				t.Fatalf("Kill(%q) = %v (%T), want a *ProtectedError", tt.pid, err, err)
			}
			if pe.Protection != tt.want {
				t.Errorf("Protection = %d (%q), want %d (%q)", pe.Protection, pe.Protection.Reason(), tt.want, tt.want.Reason())
			}
			if wantPID, _ := strconv.Atoi(tt.pid); pe.PID != wantPID {
				t.Errorf("PID = %d, want %d", pe.PID, wantPID)
			}
			if !strings.Contains(err.Error(), tt.want.Reason()) {
				t.Errorf("error %q does not name the reason %q", err.Error(), tt.want.Reason())
			}
			if !strings.Contains(err.Error(), tt.pid) {
				t.Errorf("error %q does not name PID %s", err.Error(), tt.pid)
			}
			if len(fake.kills) != 0 {
				t.Errorf("a signal was sent to %v: the refusal must come before any signal", fake.kills)
			}
		})
	}
}

// TestKillSignalsWhatIsNotProtected is the counter-case: the rule refuses
// exactly the listed PIDs and nothing around them, so a stale or unrelated PID
// still reaches procOS.kill, once.
func TestKillSignalsWhatIsNotProtected(t *testing.T) {
	const self, parent, grandparent = 500, 400, 300
	tests := []struct {
		name    string
		goos    string
		parent  int // what parentPID returns
		created map[int]int64
		pid     string
		want    int
	}{
		{"parent born after DevDesk: its PID was reused", "linux", parent, map[int]int64{parent: 3000, self: 2000}, "400", 400},
		{"grandparent: only the direct parent is refused (Q1)", "linux", parent, map[int]int64{grandparent: 100, parent: 1000, self: 2000}, "300", 300},
		{"grandparent on Windows", "windows", parent, map[int]int64{grandparent: 100, parent: 1000, self: 2000}, "300", 300},
		{"PID 4 off Windows", "linux", parent, nil, "4", 4},
		{"unknown parent (0): the parent rule is ignored", "linux", 0, nil, "4242", 4242},
		{"any other PID", "linux", parent, map[int]int64{parent: 1000, self: 2000}, "4242", 4242},
		{"any other PID on Windows", "windows", parent, nil, "4242", 4242},
		{"PID 2 is not PID 1", "linux", parent, nil, "2", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := useFakeOS(t, self, tt.parent, tt.goos)
			for pid, ms := range tt.created {
				fake.created[pid] = ms
			}

			if err := Kill(tt.pid); err != nil {
				t.Fatalf("Kill(%q) = %v, want nil", tt.pid, err)
			}
			if len(fake.kills) != 1 || fake.kills[0] != tt.want {
				t.Errorf("kills = %v, want [%d]", fake.kills, tt.want)
			}
		})
	}
}

// TestAGrandparentIsNotProtected states decision Q1 directly: only the direct
// parent is refused; the process that started the parent (a terminal, ssh, a
// multiplexer) is not, by either entry point.
func TestAGrandparentIsNotProtected(t *testing.T) {
	const self, parent, grandparent = 500, 400, 300
	fake := useFakeOS(t, self, parent, "linux")
	fake.created[grandparent] = 100
	fake.created[parent] = 1000
	fake.created[self] = 2000

	if got := ProtectionOf(strconv.Itoa(grandparent)); got != Unprotected {
		t.Errorf("ProtectionOf(grandparent) = %q, want Unprotected", got.Reason())
	}
	if got := ProtectionOf(strconv.Itoa(parent)); got != ProtectedParent {
		t.Errorf("sanity: ProtectionOf(parent) = %q, want %q", got.Reason(), ReasonParent)
	}

	var pe *ProtectedError
	if err := Kill(strconv.Itoa(grandparent)); errors.As(err, &pe) {
		t.Errorf("Kill(grandparent) refused: %v", err)
	}
	if len(fake.kills) != 1 || fake.kills[0] != grandparent {
		t.Errorf("kills = %v, want [%d]", fake.kills, grandparent)
	}
}

// TestKillWithoutAParentPIDIgnoresTheParentRule: when parentPID answers 0 (the
// parent lives outside this PID namespace), no creation time is read and no PID
// is taken for the parent.
func TestKillWithoutAParentPIDIgnoresTheParentRule(t *testing.T) {
	fake := useFakeOS(t, 500, 0, "linux")
	counter := &countingOS{fakeOS: fake}
	procOS = counter

	for _, pid := range []string{"0", "4242", "400"} {
		if got := ProtectionOf(pid); got != Unprotected {
			t.Errorf("ProtectionOf(%q) = %q, want Unprotected", pid, got.Reason())
		}
	}
	if counter.reads != 0 {
		t.Errorf("createTime read %d times with no parent PID", counter.reads)
	}
}

// TestKillOnLinuxKeepsRefusingAnythingThatIsNotAProcessID: PID 0 off Windows is
// no protected process, it is not a process id; the old refusal stands and no
// signal is sent.
func TestKillOnLinuxKeepsRefusingAnythingThatIsNotAProcessID(t *testing.T) {
	for _, pid := range []string{"0", "-1", "", "abc"} {
		t.Run(fmt.Sprintf("%q", pid), func(t *testing.T) {
			fake := useFakeOS(t, 500, 400, "linux")
			err := Kill(pid)
			if err == nil {
				t.Fatalf("Kill(%q) returned nil", pid)
			}
			var pe *ProtectedError
			if errors.As(err, &pe) {
				t.Errorf("Kill(%q) = %v: not a protected process, just not a process id", pid, err)
			}
			if len(fake.kills) != 0 {
				t.Errorf("signalled %v", fake.kills)
			}
		})
	}
}

// TestProtectedErrorIsNotAnOSError: a refusal of ours must not read as the
// system's, or the UI would show "permission denied" for it.
func TestProtectedErrorIsNotAnOSError(t *testing.T) {
	useFakeOS(t, 500, 400, "linux")
	err := Kill("500")
	if errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrProcessDone) {
		t.Errorf("Kill(self) = %v reads as an operating system error", err)
	}
}
