package ports

import (
	"errors"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// Probe is how the Ports tab learns, without signalling, whether the process it
// asked to stop is still there (DEVDESK-711, S-02b criteria 2 and 3). It reads a
// state and compares it with the identity the row was listed with.

// withState rewrites the state field (the first one after the last ")") of a
// /proc/<pid>/stat line built by statLine.
func withState(line string, state byte) string {
	i := strings.LastIndex(line, ")")
	return line[:i+2] + string(state) + line[i+3:]
}

// ── Reading the state ────────────────────────────────────────────────────────

func TestParseStat(t *testing.T) {
	cases := []struct {
		name      string
		stat      string
		wantState byte
		wantStart uint64
		wantErr   bool
	}{
		{
			name: "a line as the kernel writes it",
			stat: "1234 (bash) S 1000 1234 1234 34816 1234 4194304 1500 22000 0 0 3 1 0 0 20 0 1 0 987654 12345678 1234 " +
				"18446744073709551615 94000000000000 94000000010000 140700000000000 0 0 0 65536 3670020 1266777851 0 0 0 17 3 0 0 0 0 0\n",
			wantState: 'S', wantStart: 987654,
		},
		{name: "running", stat: withState(statLine(4242, "node", 31337), 'R'), wantState: 'R', wantStart: 31337},
		{name: "a zombie", stat: withState(statLine(4242, "node", 31337), 'Z'), wantState: 'Z', wantStart: 31337},
		{name: "dead", stat: withState(statLine(4242, "node", 31337), 'X'), wantState: 'X', wantStart: 31337},
		{name: "stopped", stat: withState(statLine(4242, "node", 31337), 'T'), wantState: 'T', wantStart: 31337},
		{name: "a name with spaces", stat: statLine(4242, "Web Content", 555), wantState: 'S', wantStart: 555},
		{name: "a name with parentheses and spaces", stat: statLine(4242, "a) (b c", 777), wantState: 'S', wantStart: 777},
		{
			name:      "a name that looks like a zombie line",
			stat:      statLine(4242, ") Z 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 (", 888),
			wantState: 'S', wantStart: 888,
		},
		{
			name:      "a zombie whose name looks like a running one",
			stat:      withState(statLine(4242, ") R 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 (", 888), 'Z'),
			wantState: 'Z', wantStart: 888,
		},
		{name: "a name closing with parentheses", stat: withState(statLine(4242, "x))", 999), 'Z'), wantState: 'Z', wantStart: 999},
		{name: "an empty name", stat: statLine(4242, "", 12), wantState: 'S', wantStart: 12},
		{name: "cut before field 22", stat: "4242 (node) S 1 4242 4242 0 -1 4194560 1500 0 0 0 10 5 0 0 20 0 11 0", wantErr: true},
		{name: "cut at the state", stat: "4242 (node) ", wantErr: true},
		{name: "nothing after the name", stat: "4242 (node)", wantErr: true},
		{name: "no closing parenthesis", stat: "4242 node S 1 2 3", wantErr: true},
		{name: "field 22 is not a number", stat: strings.Replace(statLine(4242, "node", 31337), "31337", "abc", 1), wantErr: true},
		{name: "empty input", stat: "", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state, start, err := parseStat([]byte(tc.stat))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseStat = %q, %d, nil; want an error", state, start)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseStat = %v, want %q, %d", err, tc.wantState, tc.wantStart)
			}
			if state != tc.wantState {
				t.Errorf("state = %q, want %q", state, tc.wantState)
			}
			if start != tc.wantStart {
				t.Errorf("start = %d, want %d", start, tc.wantStart)
			}
		})
	}
}

// parseStartTicks keeps meaning "the 22nd field": it is parseStat without the
// state, so the two cannot disagree.
func TestParseStartTicksAgreesWithParseStat(t *testing.T) {
	for _, line := range []string{
		statLine(4242, "node", 31337),
		statLine(4242, "a) (b c", 777),
		withState(statLine(4242, "node", 5), 'Z'),
	} {
		_, want, err := parseStat([]byte(line))
		if err != nil {
			t.Fatalf("parseStat(%q) = %v", line, err)
		}
		got, err := parseStartTicks([]byte(line))
		if err != nil {
			t.Fatalf("parseStartTicks(%q) = %v", line, err)
		}
		if got != want {
			t.Errorf("parseStartTicks = %d, parseStat says %d", got, want)
		}
	}
}

// ── What counts as a zombie ──────────────────────────────────────────────────

func TestZombieStates(t *testing.T) {
	for _, tc := range []struct {
		name    string
		state   byte
		threads int
		want    bool
	}{
		{"zombie, alone", 'Z', 1, true},
		{"dead", 'X', 1, true},
		{"dead, thread count unknown", 'X', 0, true},
		// A process whose main thread called pthread_exit reads Z at field 3
		// while its other threads run on: it is alive.
		{"main thread gone, another thread runs", 'Z', 2, false},
		{"main thread gone, many threads run", 'Z', 9, false},
		// Not knowing the thread count is not knowing it is a zombie.
		{"zombie, thread count unknown", 'Z', 0, false},
		{"zombie, thread count negative", 'Z', -1, false},
		{"sleeping", 'S', 1, false}, {"running", 'R', 1, false}, {"disk sleep", 'D', 1, false},
		{"stopped", 'T', 1, false}, {"tracing stop", 't', 1, false}, {"idle", 'I', 1, false},
		{"lowercase x", 'x', 1, false}, {"lowercase z", 'z', 1, false},
	} {
		if got := isLinuxZombie(tc.state, tc.threads); got != tc.want {
			t.Errorf("%s: isLinuxZombie(%q, %d) = %v, want %v", tc.name, tc.state, tc.threads, got, tc.want)
		}
	}
}

func TestParseThreads(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  string
		want    int
		wantErr bool
	}{
		{"one", "Name:\tx\nState:\tZ (zombie)\nThreads:\t1\nSigQ:\t0/1\n", 1, false},
		{"several", "Name:\tx\nThreads:\t12\nVoluntary:\t0\n", 12, false},
		{"no line", "Name:\tx\nState:\tZ (zombie)\n", 0, true},
		{"not a number", "Threads:\tmany\n", 0, true},
		{"zero", "Threads:\t0\n", 0, true},
		{"empty", "", 0, true},
	} {
		got, err := parseThreads([]byte(tc.status))
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("%s: parseThreads = %d, %v; want %d, error %v", tc.name, got, err, tc.want, tc.wantErr)
		}
	}
}

// The macOS state is tested here as pure functions, on every platform: the
// reader itself only runs on macOS.
func TestDarwinZombieState(t *testing.T) {
	if darwinSZOMB != 5 {
		t.Errorf("darwinSZOMB = %d, want 5 (<sys/proc.h>: SIDL 1, SRUN 2, SSLEEP 3, SSTOP 4, SZOMB 5)", darwinSZOMB)
	}
	for _, tc := range []struct {
		pStat int8
		want  bool
	}{
		{5, true},
		{0, false}, {1, false}, {2, false}, {3, false}, {4, false}, {6, false}, {-5, false},
	} {
		if got := isDarwinZombie(tc.pStat); got != tc.want {
			t.Errorf("isDarwinZombie(%d) = %v, want %v", tc.pStat, got, tc.want)
		}
	}
}

// ── Liveness ─────────────────────────────────────────────────────────────────

func TestStoppedCountsZombieAndExited(t *testing.T) {
	for _, tc := range []struct {
		l       Liveness
		stopped bool
		text    string
	}{
		{ProcessRunning, false, "running"},
		{ProcessZombie, true, "zombie"},
		{ProcessExited, true, "exited"},
	} {
		if got := tc.l.Stopped(); got != tc.stopped {
			t.Errorf("%v.Stopped() = %v, want %v", tc.l, got, tc.stopped)
		}
		if got := tc.l.String(); got != tc.text {
			t.Errorf("Liveness(%d).String() = %q, want %q", int(tc.l), got, tc.text)
		}
	}
	if ProcessRunning != 0 {
		t.Errorf("ProcessRunning = %d: the zero Liveness must be the safe one, running", int(ProcessRunning))
	}
}

func TestLivenessOf(t *testing.T) {
	listed := IdentityForTest(7)
	unreadable := errors.New("the stat file could not be read")
	cases := []struct {
		name         string
		st           procStatus
		err          error
		want         Liveness
		wantErr      bool
		wantSentinel error
	}{
		{name: "the listed process, running", st: procStatus{start: 7}, want: ProcessRunning},
		{name: "the listed process, a zombie", st: procStatus{start: 7, zombie: true}, want: ProcessZombie},
		{name: "another process holds the PID", st: procStatus{start: 8}, want: ProcessExited},
		{name: "another process holds the PID, itself a zombie", st: procStatus{start: 8, zombie: true}, want: ProcessExited},
		{name: "no process holds the PID", err: os.ErrProcessDone, want: ProcessExited},
		{name: "no process, wrapped", err: errors.Join(errors.New("reading /proc"), os.ErrProcessDone), want: ProcessExited},
		{name: "refused to read", err: os.ErrPermission, want: ProcessRunning, wantErr: true, wantSentinel: os.ErrPermission},
		{name: "an unreadable stat counts as running", err: unreadable, want: ProcessRunning, wantErr: true, wantSentinel: unreadable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := livenessOf(listed, tc.st, tc.err)
			if got != tc.want {
				t.Errorf("livenessOf = %v, want %v", got, tc.want)
			}
			if tc.wantErr {
				if err == nil {
					t.Fatal("livenessOf returned no error for an unreadable state")
				}
				if !errors.Is(err, tc.wantSentinel) {
					t.Errorf("errors.Is(%v, %v) = false: the cause was not wrapped with %%w", err, tc.wantSentinel)
				}
			} else if err != nil {
				t.Errorf("livenessOf returned %v, want nil", err)
			}
		})
	}
}

// ── Probe ────────────────────────────────────────────────────────────────────

func TestProbeRefusesWhatItCannotCompare(t *testing.T) {
	cases := []struct {
		name     string
		goos     string
		pid      string
		listed   Identity
		wantType string
		wantIs   error
		wantText string
	}{
		{name: "an unknown identity", goos: "linux", pid: "4242", listed: Identity{}, wantType: "unverifiable"},
		{name: "a deferred identity", goos: "linux", pid: "4242", listed: DeferredIdentityForTest(fakeEpoch), wantType: "unverifiable"},
		{name: "zero", goos: "linux", pid: "0", listed: IdentityForTest(1), wantText: "not a process id"},
		{name: "not a number", goos: "linux", pid: "abc", listed: IdentityForTest(1), wantText: "not a process id"},
		{name: "past int32", goos: "linux", pid: "4294967297", listed: IdentityForTest(1), wantText: "not a process id"},
		{name: "Windows has no probe", goos: "windows", pid: "4412", listed: IdentityForTest(1), wantIs: errors.ErrUnsupported},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := useFakeOS(t, 100, 99, tc.goos)

			got, err := Probe(tc.pid, tc.listed)

			if err == nil {
				t.Fatalf("Probe(%q) = %v, nil; want an error", tc.pid, got)
			}
			if got != ProcessRunning {
				t.Errorf("Probe(%q) = %v with an error, want %v: an unreadable probe counts as running", tc.pid, got, ProcessRunning)
			}
			if tc.wantType == "unverifiable" {
				var uerr *UnverifiableError
				if !errors.As(err, &uerr) {
					t.Errorf("Probe(%q) = %v (%T), want an *UnverifiableError", tc.pid, err, err)
				}
			}
			if tc.wantIs != nil && !errors.Is(err, tc.wantIs) {
				t.Errorf("Probe(%q) = %v, want errors.Is %v", tc.pid, err, tc.wantIs)
			}
			if tc.wantText != "" && !strings.Contains(err.Error(), tc.wantText) {
				t.Errorf("Probe(%q) = %q, want it to say %q", tc.pid, err.Error(), tc.wantText)
			}
			if len(fake.probeCalls) != 0 {
				t.Errorf("procOS.probe was called with %v for a probe that cannot compare", fake.probeCalls)
			}
		})
	}
}

// One read per probe, and nothing is ever sent: the probe is not a signal.
func TestProbeReadsOnceAndSendsNothing(t *testing.T) {
	fake := useFakeOS(t, 100, 99, "linux")
	fake.status[812] = procStatus{start: 7}

	got, err := Probe("812", IdentityForTest(7))

	if err != nil || got != ProcessRunning {
		t.Fatalf("Probe(812) = %v, %v; want running, nil", got, err)
	}
	if !slices.Equal(fake.probeCalls, []int{812}) {
		t.Errorf("procOS.probe was called with %v, want [812]", fake.probeCalls)
	}
	if len(fake.kills) != 0 || len(fake.sigs) != 0 {
		t.Errorf("the probe signalled: kills %v, signals %v", fake.kills, fake.sigs)
	}
	if len(fake.idCalls) != 0 {
		t.Errorf("the probe read the identity separately (%v): one read gives state and stamp", fake.idCalls)
	}
}

func TestProbeSaysWhatBecameOfTheListedProcess(t *testing.T) {
	cases := []struct {
		name    string
		status  *procStatus
		readErr error
		want    Liveness
		wantErr bool
	}{
		{name: "still running", status: &procStatus{start: 7}, want: ProcessRunning},
		{name: "a zombie", status: &procStatus{start: 7, zombie: true}, want: ProcessZombie},
		{name: "the PID was taken by another process", status: &procStatus{start: 9}, want: ProcessExited},
		{name: "no process holds the PID", want: ProcessExited},
		{name: "the read was refused", readErr: os.ErrPermission, want: ProcessRunning, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := useFakeOS(t, 100, 99, "linux")
			if tc.status != nil {
				fake.status[812] = *tc.status
			}
			if tc.readErr != nil {
				fake.statusErr[812] = tc.readErr
			}

			got, err := Probe("812", IdentityForTest(7))

			if got != tc.want {
				t.Errorf("Probe = %v, want %v", got, tc.want)
			}
			if tc.wantErr != (err != nil) {
				t.Errorf("Probe error = %v, want an error: %v", err, tc.wantErr)
			}
			if tc.wantErr && !errors.Is(err, tc.readErr) {
				t.Errorf("errors.Is(%v, %v) = false: the cause was not wrapped with %%w", err, tc.readErr)
			}
			if len(fake.kills) != 0 {
				t.Errorf("the probe signalled %v", fake.kills)
			}
		})
	}
}

// ── The real readers (Linux and macOS): they read, they never signal ─────────

func TestRealStatusOfThisProcess(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skipf("no state reader on %s", runtime.GOOS)
	}

	st, err := realOS{}.probe(os.Getpid())
	if err != nil {
		t.Fatalf("probing this process: %v", err)
	}
	if st.zombie {
		t.Error("this running process reads as a zombie")
	}
	want, err := realOS{}.identity(os.Getpid())
	if err != nil {
		t.Fatalf("reading the identity of this process: %v", err)
	}
	if st.start != want {
		t.Errorf("probe says the start is %d, identity says %d: they must be the same stamp", st.start, want)
	}
}

// Probing a PID that no process holds says so with the sentinel Probe turns into
// "exited"; it reads /proc and signals nothing, so it is allowed under test.
func TestRealProbeOfAMissingPIDSaysGone(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the /proc reader is Linux only")
	}
	if _, err := (realOS{}).probe(unreachablePID); !errors.Is(err, os.ErrProcessDone) {
		t.Errorf("probe(%d) = %v, want errors.Is os.ErrProcessDone", unreachablePID, err)
	}
	got, err := Probe("4194305", IdentityForTest(1))
	if err != nil || got != ProcessExited {
		t.Errorf("Probe of a missing PID = %v, %v; want exited, nil", got, err)
	}
}
