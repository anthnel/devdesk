package agentusage

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Tests of Activity and Settled (DEVDESK-44 step 5, criterion 1): a finished
// session is no longer read periodically. Decisions: Settled needs no
// sub-agent with finished:false and no change for `quiet`; a StateUnknown
// sub-agent does not keep the session active. The clock is injected through
// timeNow and explicit mtimes: nothing sleeps.

const actQuiet = 5 * time.Minute

// actBase is the instant the fake clock starts at.
var actBase = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// metaUnknown has no finished key, so its state is StateUnknown.
const metaUnknown = `{"agentType":"coder"}`

// setClock stubs timeNow with a clock the test moves by assigning *now.
func setClock(t *testing.T, at time.Time) *time.Time {
	t.Helper()
	now := at
	orig := timeNow
	timeNow = func() time.Time { return now }
	t.Cleanup(func() { timeNow = orig })
	return &now
}

// touch sets a file's mtime.
func touch(t *testing.T, path string, at time.Time) {
	t.Helper()
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

// putAt writes a file with the given mtime.
func (s *sess) putAt(path string, data []byte, at time.Time) {
	s.t.Helper()
	s.put(path, data)
	touch(s.t, path, at)
}

// metaSet describes a session: the meta body of each sub-agent ("" = no meta).
type metaSet map[string]string

// build lays the session out with every file's mtime at `at`.
func (s *sess) build(metas metaSet, at time.Time) {
	s.t.Helper()
	s.putAt(s.parent(), cat(ln("p1"), ln("p2")), at)
	for id, body := range metas {
		s.putAt(s.sub(id), ln("s-"+id), at)
		if body != "" {
			s.putAt(s.metaPath(id), []byte(body), at)
		}
	}
}

func TestActivityBeforeAnyApplyIsZero(t *testing.T) {
	setClock(t, actBase)
	s := newSess(t)
	s.build(metaSet{"a1": metaUnfinished}, actBase)
	a := NewAggregate(s.parent())
	nows := []time.Time{{}, actBase, actBase.Add(actQuiet), actBase.Add(1000 * time.Hour)}
	check := func(stage string) {
		t.Helper()
		if got := a.Activity(); got != (Activity{}) {
			t.Errorf("%s: Activity = %+v, want the zero value", stage, got)
		}
		for _, now := range nows {
			for _, q := range []time.Duration{0, actQuiet} {
				if a.Activity().Settled(now, q) {
					t.Errorf("%s: Settled(%v, %v) = true, want false", stage, now, q)
				}
			}
		}
	}
	check("new aggregate")
	d, err := ReadDelta(a.Cursor())
	if err != nil {
		t.Fatal(err)
	}
	check("delta read but not applied")
	if err := a.Apply(d); err != nil {
		t.Fatal(err)
	}
	before := a.Activity()
	if before == (Activity{}) {
		t.Fatal("setup: the first Apply must set the activity")
	}
	if err := a.Apply(d); !errors.Is(err, ErrStaleDelta) {
		t.Fatalf("second Apply = %v, want ErrStaleDelta", err)
	}
	if got := a.Activity(); got != before {
		t.Errorf("a stale Apply changed the activity: %+v then %+v", before, got)
	}
}

func TestSettledTable(t *testing.T) {
	last := actBase
	tests := []struct {
		name string
		act  Activity
		now  time.Time
		want bool
	}{
		{"exactly quiet, nothing unfinished", Activity{LastChange: last}, last.Add(actQuiet), true},
		{"one ns short of quiet", Activity{LastChange: last}, last.Add(actQuiet - time.Nanosecond), false},
		{"long past quiet", Activity{LastChange: last}, last.Add(100 * time.Hour), true},
		{"fresh change", Activity{LastChange: last}, last, false},
		{"unfinished blocks at any age", Activity{LastChange: last, Unfinished: 1}, last.Add(1000 * time.Hour), false},
		{"several unfinished", Activity{LastChange: last, Unfinished: 3}, last.Add(1000 * time.Hour), false},
		{"zero LastChange is never settled", Activity{}, last.Add(1000 * time.Hour), false},
		{"clock before the last change", Activity{LastChange: last}, last.Add(-time.Hour), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.act.Settled(tc.now, actQuiet); got != tc.want {
				t.Errorf("%+v.Settled(%v, %v) = %v, want %v", tc.act, tc.now, actQuiet, got, tc.want)
			}
		})
	}
}

// Q2 A: only finished:false counts; unknown, finished, and a sub-agent with
// no (or unreadable) meta do not.
func TestActivityUnfinishedCount(t *testing.T) {
	tests := []struct {
		name  string
		metas metaSet
		want  int
	}{
		{"no sub-agent", nil, 0},
		{"one unfinished", metaSet{"a1": metaUnfinished}, 1},
		{"one finished", metaSet{"a1": metaFinished}, 0},
		{"one unknown", metaSet{"a1": metaUnknown}, 0},
		{"finished is not a boolean", metaSet{"a1": `{"agentType":"coder","finished":"no"}`}, 0},
		{"no meta file", metaSet{"a1": ""}, 0},
		{"mixed", metaSet{"a1": metaUnfinished, "b2": metaFinished, "c3": metaUnknown, "d4": metaUnfinished, "e5": ""}, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setClock(t, actBase)
			s := newSess(t)
			s.build(tc.metas, actBase)
			a := NewAggregate(s.parent())
			readApply(t, a)
			act := a.Activity()
			if act.Unfinished != tc.want {
				t.Errorf("Unfinished = %d, want %d", act.Unfinished, tc.want)
			}
			far := actBase.Add(100 * time.Hour)
			if got, want := act.Settled(far, actQuiet), tc.want == 0; got != want {
				t.Errorf("Settled(far) = %v, want %v", got, want)
			}
		})
	}
}

// The plan: LastChange is the newest mtime among the parent, the sub-agents
// and the metas, clamped to the time of the read.
func TestActivityLastChangeIsTheNewestMtime(t *testing.T) {
	tests := []struct {
		name                    string
		parent, sub, metaT, now time.Duration // offsets from actBase
		want                    time.Duration
	}{
		{"parent newest", 5 * time.Minute, time.Minute, 2 * time.Minute, time.Hour, 5 * time.Minute},
		{"sub-agent transcript newest", time.Minute, 7 * time.Minute, 2 * time.Minute, time.Hour, 7 * time.Minute},
		{"meta newest", time.Minute, 2 * time.Minute, 9 * time.Minute, time.Hour, 9 * time.Minute},
		{"future mtime counts as the read time", time.Minute, 2 * time.Minute, time.Hour, 10 * time.Minute, 10 * time.Minute},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setClock(t, actBase.Add(tc.now))
			s := newSess(t)
			s.putAt(s.parent(), ln("p1"), actBase.Add(tc.parent))
			s.putAt(s.sub("a1"), ln("s1"), actBase.Add(tc.sub))
			s.putAt(s.metaPath("a1"), []byte(metaFinished), actBase.Add(tc.metaT))
			a := NewAggregate(s.parent())
			readApply(t, a)
			if got, want := a.Activity().LastChange, actBase.Add(tc.want); !got.Equal(want) {
				t.Errorf("LastChange = %v, want %v", got, want)
			}
		})
	}
}

// LastChange follows what changed, not what was looked at: an empty delta
// (including one where only an mtime moved) does not advance it, a non-empty
// one does and un-settles the session again.
func TestActivityFollowsRealChanges(t *testing.T) {
	clock := setClock(t, actBase)
	s := newSess(t)
	s.build(metaSet{"a1": metaFinished}, actBase)
	a := NewAggregate(s.parent())
	readApply(t, a)
	if got := a.Activity(); !got.LastChange.Equal(actBase) || got.Unfinished != 0 {
		t.Fatalf("after the first read: %+v, want LastChange %v and no unfinished", got, actBase)
	}
	if !a.Activity().Settled(actBase.Add(actQuiet), actQuiet) {
		t.Fatal("a finished session read once is settled after quiet")
	}

	// An empty read much later: nothing changed, so LastChange stays.
	*clock = actBase.Add(time.Hour)
	if d := readApply(t, a); !d.Empty() {
		t.Fatal("nothing was written, the delta must be Empty")
	}
	if got := a.Activity().LastChange; !got.Equal(actBase) {
		t.Errorf("an Empty delta moved LastChange to %v", got)
	}

	// Only the mtime moves (same size, same content): still not a change.
	touch(t, s.parent(), actBase.Add(2*time.Hour))
	*clock = actBase.Add(2 * time.Hour)
	if d := readApply(t, a); !d.Empty() {
		t.Fatal("an mtime alone must leave the delta Empty")
	}
	if got := a.Activity().LastChange; !got.Equal(actBase) {
		t.Errorf("a bare mtime change moved LastChange to %v", got)
	}

	// New lines: a real change.
	at := actBase.Add(3 * time.Hour)
	*clock = at
	appendFile(t, s.parent(), ln("p3"))
	touch(t, s.parent(), at)
	if d := readApply(t, a); d.Empty() {
		t.Fatal("appended lines must give a non-empty delta")
	}
	if got := a.Activity().LastChange; !got.Equal(at) {
		t.Fatalf("LastChange = %v, want %v", got, at)
	}
	act := a.Activity()
	if act.Settled(at.Add(actQuiet-time.Nanosecond), actQuiet) {
		t.Error("a new change must un-settle the session until quiet has passed again")
	}
	if !act.Settled(at.Add(actQuiet), actQuiet) {
		t.Error("settled again once quiet has passed without change")
	}

	// A sub-agent flips to unfinished (same-size meta): news, and it blocks.
	at = actBase.Add(4 * time.Hour)
	*clock = at
	s.putAt(s.metaPath("a1"), []byte(metaUnfinished), at)
	if d := readApply(t, a); d.Empty() {
		t.Fatal("a meta change must give a non-empty delta")
	}
	act = a.Activity()
	if act.Unfinished != 1 || !act.LastChange.Equal(at) {
		t.Fatalf("after the flip: %+v, want 1 unfinished and LastChange %v", act, at)
	}
	if act.Settled(at.Add(1000*time.Hour), actQuiet) {
		t.Error("an unfinished sub-agent keeps the session active however old")
	}

	// ... and back to finished.
	at = actBase.Add(5 * time.Hour)
	*clock = at
	s.putAt(s.metaPath("a1"), []byte(metaFinished), at)
	readApply(t, a)
	act = a.Activity()
	if act.Unfinished != 0 || !act.LastChange.Equal(at) {
		t.Fatalf("after finishing: %+v, want 0 unfinished and LastChange %v", act, at)
	}
	if act.Settled(at.Add(actQuiet-time.Nanosecond), actQuiet) || !act.Settled(at.Add(actQuiet), actQuiet) {
		t.Error("finishing is a change: settled only after quiet from that moment")
	}
}

// A Full delta (here: the cursor is rewound) recomputes the activity from the
// files; it does not keep a stale unfinished count.
func TestActivityAfterRewoundCursor(t *testing.T) {
	setClock(t, actBase)
	s := newSess(t)
	s.build(metaSet{"a1": metaUnfinished}, actBase)
	a := NewAggregate(s.parent())
	readApply(t, a)
	touch(t, s.metaPath("a1"), actBase)
	s.putAt(s.metaPath("a1"), []byte(metaFinished), actBase)
	d, err := ReadDelta(a.Cursor().Rewound())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(d); err != nil {
		t.Fatal(err)
	}
	if got := a.Activity().Unfinished; got != 0 {
		t.Errorf("Unfinished = %d after a Full read of a finished session, want 0", got)
	}
}

// A file that cannot be read keeps the session unsettled, whichever file it is
// (the parent or a sub-agent) and however it fails (IssueOpenFailed, the open
// itself fails; IssueReadError, the read fails mid-way). Decision: such a
// session is NEVER settled, whatever else is quiet, so a poller keeps retrying
// and a transient failure heals. It is settled again after a successful read
// plus quiet.
func TestActivityWithAnUnreadableFile(t *testing.T) {
	far := actBase.Add(1000 * time.Hour)
	for _, tc := range []struct {
		name  string
		onSub bool
		kind  IssueKind
	}{
		{"parent open failed", false, IssueOpenFailed},
		{"parent read error", false, IssueReadError},
		{"sub-agent open failed", true, IssueOpenFailed},
		{"sub-agent read error", true, IssueReadError},
	} {
		for metaName, meta := range map[string]string{"finished": metaFinished, "unknown": metaUnknown} {
			t.Run(tc.name+"/"+metaName, func(t *testing.T) {
				clock := setClock(t, actBase)
				s := newSess(t)
				s.build(metaSet{"a1": meta}, actBase)
				target := s.parent()
				if tc.onSub {
					target = s.sub("a1")
				}
				orig := openTranscript
				failing := true
				swapOpen(t, func(path string) (transcriptFile, error) {
					if !failing || path != target {
						return orig(path)
					}
					if tc.kind == IssueOpenFailed {
						return nil, errors.New("injected open failure")
					}
					f, err := orig(path)
					if err != nil {
						return nil, err
					}
					return &faultyFile{transcriptFile: f, limit: 0}, nil
				})
				a := NewAggregate(s.parent())
				readApply(t, a)
				if !hasAnyIssue(a.Usage(), tc.kind) {
					t.Fatalf("setup: no %v issue in %+v", tc.kind, a.Usage().Diagnostics)
				}
				if a.Activity().Settled(far, actQuiet) {
					t.Error("an unreadable file must keep the session unsettled, however quiet")
				}
				*clock = far // still failing on later passes: still unsettled
				readApply(t, a)
				if a.Activity().Settled(far.Add(time.Hour), actQuiet) {
					t.Error("still unsettled while the file stays unreadable")
				}
				failing = false
				readApply(t, a) // the issue clears
				if hasAnyIssue(a.Usage(), tc.kind) {
					t.Fatal("setup: the issue must clear once the file reads")
				}
				if !a.Activity().Settled(far.Add(time.Hour), actQuiet) {
					t.Error("settled again after a successful read and quiet")
				}
			})
		}
	}
}

// hasAnyIssue reports whether any file of u carries an issue of the given kind.
func hasAnyIssue(u Usage, kind IssueKind) bool {
	for _, fi := range u.Diagnostics.Files {
		if fi.Kind == kind {
			return true
		}
	}
	return false
}

// LastChange never goes back: when the newest file disappears (a sub-agent
// removed, which gives a Full delta), the older survivors must not pull it
// earlier, or the session would be declared settled sooner than it should.
func TestActivityLastChangeNeverMovesBack(t *testing.T) {
	setClock(t, actBase.Add(time.Hour))
	s := newSess(t)
	newest := actBase.Add(10 * time.Minute)
	s.putAt(s.parent(), ln("p1"), actBase)
	s.putAt(s.sub("a1"), ln("s1"), newest)
	s.putAt(s.metaPath("a1"), []byte(metaFinished), actBase)
	a := NewAggregate(s.parent())
	readApply(t, a)
	if got := a.Activity().LastChange; !got.Equal(newest) {
		t.Fatalf("setup: LastChange = %v, want %v", got, newest)
	}
	for _, p := range []string{s.sub("a1"), s.metaPath("a1")} {
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
	}
	d := readApply(t, a)
	if !d.Full() {
		t.Fatalf("setup: removing a sub-agent must give a Full delta (Empty=%v)", d.Empty())
	}
	act := a.Activity()
	if act.LastChange.Before(newest) {
		t.Errorf("LastChange went back from %v to %v", newest, act.LastChange)
	}
	if act.Settled(newest.Add(actQuiet-time.Nanosecond), actQuiet) {
		t.Error("settled before quiet has passed since the newest file's last change")
	}
}

func TestActivityUnreadableParentAndUnfinishedSubAgent(t *testing.T) {
	setClock(t, actBase)
	s := newSess(t)
	s.build(metaSet{"a1": metaUnfinished}, actBase)
	orig := openTranscript
	swapOpen(t, func(path string) (transcriptFile, error) {
		if path == s.parent() {
			return nil, errors.New("injected open failure")
		}
		return orig(path)
	})
	a := NewAggregate(s.parent())
	readApply(t, a)
	if got := a.Activity().Unfinished; got != 1 {
		t.Errorf("Unfinished = %d, want 1 (the sub-agent's meta was read)", got)
	}
}

func TestActivityLeaksNoPath(t *testing.T) {
	setClock(t, actBase)
	s := newSessIn(t, "SENTINEL-DO-NOT-LEAK-dir")
	s.build(metaSet{"a1": metaUnfinished}, actBase)
	a := NewAggregate(s.parent())
	readApply(t, a)
	act := a.Activity()
	for _, out := range []string{fmt.Sprint(act), fmt.Sprintf("%+v", act), fmt.Sprintf("%#v", act)} {
		if strings.Contains(out, "SENTINEL") || strings.Contains(out, s.dir) || strings.Contains(out, incSessUUID) {
			t.Errorf("Activity prints a path or a name: %q", out)
		}
	}
	typ := reflect.TypeOf(act)
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.Type != reflect.TypeOf(time.Time{}) && f.Type.Kind() != reflect.Int && f.Type.Kind() != reflect.Bool {
			t.Errorf("field %s has type %v: Activity carries a time and a count, nothing that can hold a path", f.Name, f.Type)
		}
	}
}

// Criterion 1, testable here: once a pass has read everything, a session where
// nothing is written costs no transcript open and no meta read, settled or not.
func TestQuietSessionIsNeverOpenedAgain(t *testing.T) {
	tests := []struct {
		name        string
		meta        string
		wantSettled bool
	}{
		{"finished sub-agents, settled", metaFinished, true},
		{"unknown sub-agents, settled", metaUnknown, true},
		{"unfinished sub-agents, still active", metaUnfinished, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setClock(t, actBase)
			s := newSess(t)
			s.build(metaSet{"a1": tc.meta, "b2": tc.meta, "c3": tc.meta}, actBase)
			p := countOpens(t)
			metas := countMetaReads(t)
			a := NewAggregate(s.parent())
			if d := readApply(t, a); d.Empty() {
				t.Fatal("the first delta cannot be Empty")
			}
			if p.calls != 4 || *metas != 3 {
				t.Fatalf("first pass: %d open calls, %d meta reads, want 4 and 3", p.calls, *metas)
			}
			act := a.Activity()
			far := actBase.Add(24 * time.Hour)
			if got := act.Settled(far, actQuiet); got != tc.wantSettled {
				t.Fatalf("Settled(far) = %v, want %v", got, tc.wantSettled)
			}
			*p, *metas = probe{}, 0
			for i := 0; i < 10; i++ {
				if d := readApply(t, a); !d.Empty() || d.Full() {
					t.Fatalf("pass %d: Empty=%v Full=%v, want true and false", i, d.Empty(), d.Full())
				}
			}
			if p.calls != 0 || *metas != 0 {
				t.Errorf("10 quiet passes: %d open calls, %d meta reads, want 0 and 0", p.calls, *metas)
			}
			if got := a.Activity(); got != act {
				t.Errorf("Activity changed across quiet passes: %+v then %+v", act, got)
			}
		})
	}
}
