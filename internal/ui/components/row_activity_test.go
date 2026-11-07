package components

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/indicator"
)

const (
	apiPath = "/home/u/w/app/api"
	webPath = "/home/u/w/app/web"
	rowCtx  = "work"
)

// rowRun builds a run of kind holding one item on target in state.
func rowRun(id jobs.JobID, kind jobs.Kind, target string, state jobs.ItemState) jobs.Run {
	r := jobs.NewRun(kind, "", rowCtx, "label", target)
	r.ID = id
	r.Items[0].State = state
	return r
}

// rowFailure builds a remembered failure; age orders failures (larger = newer).
func rowFailure(kind jobs.Kind, target, detail string, age int) jobs.Failure {
	return jobs.Failure{
		FailureKey: jobs.FailureKey{Kind: kind, Target: jobs.TargetKey(target), Context: rowCtx},
		Detail:     detail,
		At:         time.Unix(1_700_000_000+int64(age), 0),
	}
}

func TestDescribeRow(t *testing.T) {
	isolateRowHome(t)
	other := rowFailure(jobs.KindSync, apiPath, "sync failed — check logs", 1)
	otherCtx := rowFailure(jobs.KindScan, apiPath, "check logs", 1)
	otherCtx.Context = "elsewhere"
	display := rowFailure(jobs.KindScan, apiPath, "check logs", 1)
	display.Display = "api (alias)"

	tests := []struct {
		name  string
		q     RowQuery
		state indicator.State
		want  string // Detail.String(); "" = zero detail
	}{
		{"scan running",
			RowQuery{Runs: []jobs.Run{rowRun(1, jobs.KindScan, apiPath, jobs.ItemRunning)}, Target: apiPath},
			indicator.Running, "● Scanning ~/w/app/api"},
		{"sync running",
			RowQuery{Runs: []jobs.Run{rowRun(1, jobs.KindSync, webPath, jobs.ItemRunning)}, Target: webPath},
			indicator.Running, "● Syncing ~/w/app/web"},
		{"scan queued",
			RowQuery{Runs: []jobs.Run{rowRun(1, jobs.KindScan, apiPath, jobs.ItemQueued)}, Target: apiPath},
			indicator.Queued, "◴ Queued: scan ~/w/app/api"},
		{"failure with the repeated verb",
			RowQuery{Failures: []jobs.Failure{rowFailure(jobs.KindScan, apiPath, "scan failed — check logs", 1)}, Target: apiPath},
			indicator.Failed, "⊗ ~/w/app/api: last scan failed — check logs"},
		{"failure with the bare cause",
			RowQuery{Failures: []jobs.Failure{rowFailure(jobs.KindScan, apiPath, "check logs", 1)}, Target: apiPath},
			indicator.Failed, "⊗ ~/w/app/api: last scan failed — check logs"},
		{"multi-line cause becomes one line",
			RowQuery{Failures: []jobs.Failure{rowFailure(jobs.KindScan, apiPath, "line1\nline2\tx", 1)}, Target: apiPath},
			indicator.Failed, "⊗ ~/w/app/api: last scan failed — line1 line2 x"},
		{"prefix then multi-line cause",
			RowQuery{Failures: []jobs.Failure{rowFailure(jobs.KindScan, apiPath, "scan failed — a\r\n  b", 1)}, Target: apiPath},
			indicator.Failed, "⊗ ~/w/app/api: last scan failed — a b"},
		{"another kind's prefix is kept",
			RowQuery{Failures: []jobs.Failure{rowFailure(jobs.KindScan, apiPath, "sync failed — x", 1)}, Target: apiPath},
			indicator.Failed, "⊗ ~/w/app/api: last scan failed — sync failed — x"},
		{"empty cause ends at failed",
			RowQuery{Failures: []jobs.Failure{rowFailure(jobs.KindScan, apiPath, "", 1)}, Target: apiPath},
			indicator.Failed, "⊗ ~/w/app/api: last scan failed"},
		{"blank cause ends at failed",
			RowQuery{Failures: []jobs.Failure{rowFailure(jobs.KindScan, apiPath, " \n\t ", 1)}, Target: apiPath},
			indicator.Failed, "⊗ ~/w/app/api: last scan failed"},
		{"running masks a failure",
			RowQuery{Runs: []jobs.Run{rowRun(1, jobs.KindScan, apiPath, jobs.ItemRunning)}, Failures: []jobs.Failure{other}, Target: apiPath},
			indicator.Running, "● Scanning ~/w/app/api · earlier failure: sync (see :jobs)"},
		{"queued masks a failure",
			RowQuery{Runs: []jobs.Run{rowRun(1, jobs.KindScan, apiPath, jobs.ItemQueued)}, Failures: []jobs.Failure{other}, Target: apiPath},
			indicator.Queued, "◴ Queued: scan ~/w/app/api · earlier failure: sync (see :jobs)"},
		{"newest failure wins, the older is listed",
			RowQuery{Failures: []jobs.Failure{other, rowFailure(jobs.KindDelete, apiPath, "delete failed — busy", 2)}, Target: apiPath},
			indicator.Failed, "⊗ ~/w/app/api: last delete failed — busy · earlier failure: sync (see :jobs)"},
		{"newest wins whatever the input order",
			RowQuery{Failures: []jobs.Failure{rowFailure(jobs.KindDelete, apiPath, "busy", 2), other}, Target: apiPath},
			indicator.Failed, "⊗ ~/w/app/api: last delete failed — busy · earlier failure: sync (see :jobs)"},
		{"running and two failures, Kinds order",
			RowQuery{
				Runs:     []jobs.Run{rowRun(1, jobs.KindScan, apiPath, jobs.ItemRunning)},
				Failures: []jobs.Failure{rowFailure(jobs.KindDelete, apiPath, "x", 2), other},
				Target:   apiPath,
			},
			indicator.Running, "● Scanning ~/w/app/api · earlier failures: sync, delete (see :jobs)"},
		{"kinds filter hides a failure (D5)",
			RowQuery{Failures: []jobs.Failure{other}, Target: apiPath, Kinds: []jobs.Kind{jobs.KindScan}},
			indicator.Idle, ""},
		{"kinds filter hides a running item",
			RowQuery{Runs: []jobs.Run{rowRun(1, jobs.KindSync, apiPath, jobs.ItemRunning)}, Target: apiPath, Kinds: []jobs.Kind{jobs.KindScan}},
			indicator.Idle, ""},
		{"another context is invisible",
			RowQuery{Failures: []jobs.Failure{otherCtx}, Target: apiPath},
			indicator.Idle, ""},
		{"another target is invisible",
			RowQuery{Runs: []jobs.Run{rowRun(1, jobs.KindScan, webPath, jobs.ItemRunning)}, Target: apiPath},
			indicator.Idle, ""},
		{"idle container sentence has no glyph (D8)",
			RowQuery{Target: apiPath, ObservedText: "Running since 3 hr ago"},
			indicator.Idle, "Running since 3 hr ago"},
		{"nothing at all",
			RowQuery{Target: apiPath},
			indicator.Idle, ""},
		{"observed state takes the imposed glyph",
			RowQuery{Target: apiPath, Observed: indicator.Restarting, ObservedText: "Restarting — 4 restarts, last exit code 1"},
			indicator.Restarting, "◕ Restarting — 4 restarts, last exit code 1"},
		{"failure beats observed",
			RowQuery{
				Failures: []jobs.Failure{rowFailure(jobs.KindScan, apiPath, "check logs", 1)},
				Target:   apiPath, Observed: indicator.Restarting, ObservedText: "Restarting — 4 restarts",
			},
			indicator.Failed, "⊗ ~/w/app/api: last scan failed — check logs"},
		{"observed running without a job keeps its state",
			RowQuery{Target: apiPath, Observed: indicator.Running, ObservedText: "Agent running — last transcript line 4 s ago"},
			indicator.Running, "● Agent running — last transcript line 4 s ago"},
		{"observed running without a job or text",
			RowQuery{Target: apiPath, Observed: indicator.Running},
			indicator.Running, ""},
		{"observed failed without a failure keeps its state",
			RowQuery{Target: apiPath, Observed: indicator.Failed, ObservedText: "Exited with code 137 — 5 min ago"},
			indicator.Failed, "⊗ Exited with code 137 — 5 min ago"},
		{"observed failed without a failure or text",
			RowQuery{Target: apiPath, Observed: indicator.Failed},
			indicator.Failed, ""},
		{"observed queued without a job keeps its state",
			RowQuery{Target: apiPath, Observed: indicator.Queued, ObservedText: "Waiting for a slot"},
			indicator.Queued, "◴ Waiting for a slot"},
		{"observed queued without a job or text",
			RowQuery{Target: apiPath, Observed: indicator.Queued},
			indicator.Queued, ""},
		{"multi-line observed text becomes one line",
			RowQuery{Target: apiPath, Observed: indicator.Restarting, ObservedText: "Restarting — 4 restarts\r\nlast exit\tcode 1\n"},
			indicator.Restarting, "◕ Restarting — 4 restarts last exit code 1"},
		{"multi-line idle observed text becomes one line",
			RowQuery{Target: apiPath, ObservedText: "Running since\n3 hr ago"},
			indicator.Idle, "Running since 3 hr ago"},
		{"colon cause loses its kind prefix",
			RowQuery{Failures: []jobs.Failure{rowFailure(jobs.KindScan, apiPath, "scan failed: boom", 1)}, Target: apiPath},
			indicator.Failed, "⊗ ~/w/app/api: last scan failed — boom"},
		{"capitalized prefix is dropped",
			RowQuery{Failures: []jobs.Failure{rowFailure(jobs.KindScan, apiPath, "Scan failed — x", 1)}, Target: apiPath},
			indicator.Failed, "⊗ ~/w/app/api: last scan failed — x"},
		{"capitalized colon prefix is dropped",
			RowQuery{Failures: []jobs.Failure{rowFailure(jobs.KindScan, apiPath, "Scan failed: x", 1)}, Target: apiPath},
			indicator.Failed, "⊗ ~/w/app/api: last scan failed — x"},
		{"image reference is verbatim",
			RowQuery{Runs: []jobs.Run{rowRun(1, jobs.KindPull, "nginx:1.27", jobs.ItemRunning)}, Target: "nginx:1.27"},
			indicator.Running, "● Pulling nginx:1.27"},
		{"failure Display wins",
			RowQuery{Failures: []jobs.Failure{display}, Target: apiPath},
			indicator.Failed, "⊗ api (alias): last scan failed — check logs"},
		{"item Display wins",
			RowQuery{Runs: []jobs.Run{rowRun(1, jobs.KindScan, apiPath, jobs.ItemRunning).WithDisplay(apiPath, "api (alias)")}, Target: apiPath},
			indicator.Running, "● Scanning api (alias)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.q.Context = rowCtx
			got, ok := describeNoPanic(t, tt.q)
			if !ok {
				return
			}
			if got.State != tt.state {
				t.Errorf("State = %v, want %v", got.State, tt.state)
			}
			if tt.want == "" {
				if !got.Detail.IsZero() {
					t.Errorf("Detail = %+v, want zero", got.Detail)
				}
			} else if s := got.Detail.String(); s != tt.want {
				t.Errorf("Detail = %q, want %q", s, tt.want)
			}
			if tt.want != "" && strings.ContainsAny(got.Detail.String(), "\n\r\t") {
				t.Errorf("Detail spans more than one line: %q", got.Detail.String())
			}
			// One computation, two readers: the pastille and the footer agree.
			if rs := RowState(tt.q); rs != got.State {
				t.Errorf("RowState = %v, DescribeRow.State = %v", rs, got.State)
			}
			// The footer glyph is the steady one of the state, never a literal.
			if g := got.Detail.Glyph; g != "" && g != indicator.Glyph(got.State, true) {
				t.Errorf("Glyph = %q, want %q", g, indicator.Glyph(got.State, true))
			}
		})
	}
}

// describeNoPanic runs DescribeRow and reports a panic as a test failure, so
// one broken case does not stop the others.
func describeNoPanic(t *testing.T, q RowQuery) (got RowActivity, ok bool) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("DescribeRow panicked: %v", r)
			ok = false
		}
	}()
	return DescribeRow(q), true
}

// An observed state with no job and no failure behind it (an exited container,
// a running agent) is the row's state: DescribeRow must neither panic nor
// disagree with RowState.
func TestObservedStateWithoutAnyJob(t *testing.T) {
	isolateRowHome(t)
	for _, obs := range []indicator.State{indicator.Running, indicator.Queued, indicator.Failed, indicator.Restarting} {
		for _, text := range []string{"", "some sentence"} {
			t.Run(fmt.Sprintf("%v/%q", obs, text), func(t *testing.T) {
				q := RowQuery{Context: rowCtx, Target: apiPath, Observed: obs, ObservedText: text}
				if rs := RowState(q); rs != obs {
					t.Errorf("RowState = %v, want %v", rs, obs)
				}
				got, ok := describeNoPanic(t, q)
				if !ok {
					return
				}
				if got.State != obs {
					t.Errorf("State = %v, want %v", got.State, obs)
				}
				if text == "" && !got.Detail.IsZero() {
					t.Errorf("Detail = %+v, want zero", got.Detail)
				}
				if text != "" && got.Detail.String() != indicator.Glyph(obs, true)+" "+text {
					t.Errorf("Detail = %q, want glyph + text", got.Detail.String())
				}
			})
		}
	}
}

// RowState follows indicator.Resolve over every combination of running, queued
// and remembered failure, with an observed state in the background.
func TestRowStateFollowsResolve(t *testing.T) {
	isolateRowHome(t)
	observed := []indicator.State{indicator.Idle, indicator.Restarting, indicator.Running, indicator.Queued, indicator.Failed}
	for _, obs := range observed {
		for mask := range 8 {
			running, queued, failed := mask&1 != 0, mask&2 != 0, mask&4 != 0
			q := RowQuery{Context: rowCtx, Target: apiPath, Observed: obs, ObservedText: "observed"}
			if running {
				q.Runs = append(q.Runs, rowRun(1, jobs.KindScan, apiPath, jobs.ItemRunning))
			}
			if queued {
				q.Runs = append(q.Runs, rowRun(2, jobs.KindSync, apiPath, jobs.ItemQueued))
			}
			if failed {
				q.Failures = append(q.Failures, rowFailure(jobs.KindDelete, apiPath, "x", 1))
			}
			want := indicator.Resolve(indicator.Activity{
				Running: running, Queued: queued, Failed: failed, Observed: obs,
			})
			label := fmt.Sprintf("observed=%v running=%v queued=%v failed=%v", obs, running, queued, failed)
			if got := RowState(q); got != want {
				t.Errorf("%s: RowState = %v, want %v", label, got, want)
			}
			got, ok := describeNoPanic(t, q)
			if !ok {
				t.Errorf("%s: DescribeRow panicked", label)
				continue
			}
			if got.State != want {
				t.Errorf("%s: DescribeRow.State = %v, want %v", label, got.State, want)
			}
		}
	}
}

// Several running items: the first in run order is the one named.
func TestDescribeRowNamesTheFirstRunningItem(t *testing.T) {
	isolateRowHome(t)
	q := RowQuery{
		Context: rowCtx, Target: apiPath,
		Runs: []jobs.Run{
			rowRun(1, jobs.KindSync, apiPath, jobs.ItemRunning),
			rowRun(2, jobs.KindScan, apiPath, jobs.ItemRunning),
		},
	}
	if got, want := DescribeRow(q).Detail.String(), "● Syncing ~/w/app/api"; got != want {
		t.Errorf("Detail = %q, want %q", got, want)
	}
}

// Every declared kind has working phrases (criterion 8): a kind added without
// a verb would fall back to "Working".
func TestEveryKindHasPhrases(t *testing.T) {
	isolateRowHome(t)
	for _, kind := range jobs.Kinds() {
		k := string(kind)
		t.Run(k, func(t *testing.T) {
			run := DescribeRow(RowQuery{Context: rowCtx, Target: apiPath,
				Runs: []jobs.Run{rowRun(1, kind, apiPath, jobs.ItemRunning)}}).Detail.String()
			if strings.Contains(run, "Working") || !strings.Contains(run, kind.Verb()) {
				t.Errorf("running sentence %q must use %q", run, kind.Verb())
			}
			queued := DescribeRow(RowQuery{Context: rowCtx, Target: apiPath,
				Runs: []jobs.Run{rowRun(1, kind, apiPath, jobs.ItemQueued)}}).Detail.String()
			if !strings.Contains(queued, "Queued: "+k+" ") {
				t.Errorf("queued sentence %q lacks %q", queued, "Queued: "+k)
			}
			failed := DescribeRow(RowQuery{Context: rowCtx, Target: apiPath,
				Failures: []jobs.Failure{rowFailure(kind, apiPath, "check logs", 1)}}).Detail.String()
			if !strings.Contains(failed, "last "+k+" failed") {
				t.Errorf("failed sentence %q lacks %q", failed, "last "+k+" failed")
			}
			if ev := FailureEvent(kind, apiPath, "check logs"); !strings.HasPrefix(ev, "Failed to "+k+" ") {
				t.Errorf("event %q lacks %q", ev, "Failed to "+k+" ")
			}
		})
	}
}

func TestFailureEvent(t *testing.T) {
	isolateRowHome(t)
	tests := []struct {
		name   string
		kind   jobs.Kind
		target string
		cause  string
		want   string
	}{
		{"readable cause", jobs.KindScan, webPath, "trivy could not pull the database",
			"Failed to scan ~/w/app/web — trivy could not pull the database"},
		{"repeated verb is cut", jobs.KindScan, webPath, "scan failed — check logs",
			"Failed to scan ~/w/app/web — check logs"},
		{"empty cause", jobs.KindScan, webPath, "", "Failed to scan ~/w/app/web"},
		{"colon after the repeated verb", jobs.KindScan, "/x", "scan failed: boom", "Failed to scan /x — boom"},
		{"capitalized repeated verb", jobs.KindScan, "/x", "Scan failed — x", "Failed to scan /x — x"},
		{"capitalized repeated verb with colon", jobs.KindScan, "/x", "Scan failed: x", "Failed to scan /x — x"},
		{"bare prefix only", jobs.KindScan, "/x", "scan failed", "Failed to scan /x"},
		{"multi-line cause", jobs.KindClone, webPath, "fatal: x\nhint: y", "Failed to clone ~/w/app/web — fatal: x hint: y"},
		{"image reference verbatim", jobs.KindPull, "nginx:1.27", "denied", "Failed to pull nginx:1.27 — denied"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FailureEvent(tt.kind, tt.target, tt.cause)
			if got != tt.want {
				t.Errorf("FailureEvent = %q, want %q", got, tt.want)
			}
			if strings.ContainsAny(got, "\n\r\t") {
				t.Errorf("event spans more than one line: %q", got)
			}
		})
	}
}

// The footer glyph is the steady one: the footer never blinks.
func TestTheFooterGlyphIsTheSteadyOne(t *testing.T) {
	isolateRowHome(t)
	got := DescribeRow(RowQuery{Context: rowCtx, Target: apiPath,
		Runs: []jobs.Run{rowRun(1, jobs.KindScan, apiPath, jobs.ItemRunning)}})
	if want := indicator.Glyph(indicator.Running, true); got.Detail.Glyph != want {
		t.Errorf("running glyph = %q, want %q", got.Detail.Glyph, want)
	}
}

// hostileTargets are names a hostile peer could choose: a container name, a
// repository path, an image reference. None may reach the terminal raw.
var hostileTargets = []struct{ name, in string }{
	{"escape sequence", "evil\x1b[31mred"},
	{"newline", "a\nb"},
	{"carriage return", "a\rb"},
	{"C1 CSI rune", "a\u009bb"},
	{"invalid 0x9B byte", "a\x9bb"},
}

// footerForbiddenRunes is what no footer text may contain as a rune; the raw
// 0x9B byte (invalid UTF-8) is checked separately with IndexByte.
const footerForbiddenRunes = "\x1b\n\r\u009b"

func assertTerminalSafe(t *testing.T, label, got string) {
	t.Helper()
	if i := strings.IndexAny(got, footerForbiddenRunes); i >= 0 {
		t.Errorf("%s carries a control character at %d: %q", label, i, got)
	}
	if i := strings.IndexByte(got, 0x9b); i >= 0 {
		t.Errorf("%s carries a raw 0x9B byte at %d: %q", label, i, got)
	}
}

// A target is untrusted text like the cause: every path into a sentence puts
// it on one line, with no control sequence.
func TestTargetIsSanitizedInEverySentence(t *testing.T) {
	isolateRowHome(t)
	for _, h := range hostileTargets {
		t.Run(h.name, func(t *testing.T) {
			active := rowRun(1, jobs.KindScan, apiPath, jobs.ItemRunning)
			active.Items[0].Display = h.in
			got := DescribeRow(RowQuery{Runs: []jobs.Run{active}, Context: rowCtx, Target: apiPath})
			assertTerminalSafe(t, "active Detail", got.Detail.String())

			named := rowFailure(jobs.KindScan, apiPath, "boom", 1)
			named.Display = h.in
			got = DescribeRow(RowQuery{Failures: []jobs.Failure{named}, Context: rowCtx, Target: apiPath})
			assertTerminalSafe(t, "failure Display Detail", got.Detail.String())

			raw := rowFailure(jobs.KindScan, h.in, "boom", 1)
			got = DescribeRow(RowQuery{Failures: []jobs.Failure{raw}, Context: rowCtx, Target: h.in})
			assertTerminalSafe(t, "failure Target Detail", got.Detail.String())

			assertTerminalSafe(t, "FailureEvent", FailureEvent(jobs.KindScan, h.in, "boom"))
		})
	}
}

// Unicode format characters (Cf) reorder or hide text without being control
// characters: they are dropped from the cause, the observed text and the target.
func TestFormatCharactersAreRemoved(t *testing.T) {
	isolateRowHome(t)
	const cf = "\u202a\u202b\u202c\u202d\u202e\u2066\u2067\u2068\u2069\u200b\u200c\u200d\u200e\u200f"
	tests := []struct {
		name string
		got  func(in string) string
	}{
		{"cause in FailureEvent", func(in string) string { return FailureEvent(jobs.KindScan, "/x", in) }},
		{"target in FailureEvent", func(in string) string { return FailureEvent(jobs.KindScan, in, "boom") }},
		{"cause in Detail", func(in string) string {
			f := rowFailure(jobs.KindScan, apiPath, in, 1)
			return DescribeRow(RowQuery{Failures: []jobs.Failure{f}, Context: rowCtx, Target: apiPath}).Detail.String()
		}},
		{"ObservedText", func(in string) string {
			return DescribeRow(RowQuery{Context: rowCtx, Target: apiPath, ObservedText: in}).Detail.String()
		}},
		{"active target", func(in string) string {
			r := rowRun(1, jobs.KindScan, apiPath, jobs.ItemRunning)
			r.Items[0].Display = in
			return DescribeRow(RowQuery{Runs: []jobs.Run{r}, Context: rowCtx, Target: apiPath}).Detail.String()
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.got("ab" + cf + "cd")
			if i := strings.IndexFunc(got, func(r rune) bool { return strings.ContainsRune(cf, r) }); i >= 0 {
				t.Errorf("format character kept at %d: %q", i, got)
			}
			if !strings.Contains(got, "abcd") && !strings.Contains(got, "ab cd") {
				t.Errorf("visible text lost: %q", got)
			}
		})
	}
}

// A cause is bounded before it is processed: a megabyte of free text yields a
// short sentence, quickly.
func TestOversizedTextIsBounded(t *testing.T) {
	isolateRowHome(t)
	huge := strings.Repeat("x ", 512*1024) // 1 MiB
	const limit = 600                      // 512 runes plus the fixed words
	tests := []struct {
		name string
		got  func() string
	}{
		{"FailureEvent cause", func() string { return FailureEvent(jobs.KindScan, "/x", huge) }},
		{"Detail cause", func() string {
			f := rowFailure(jobs.KindScan, apiPath, huge, 1)
			return DescribeRow(RowQuery{Failures: []jobs.Failure{f}, Context: rowCtx, Target: apiPath}).Detail.String()
		}},
		{"ObservedText", func() string {
			return DescribeRow(RowQuery{Context: rowCtx, Target: apiPath, ObservedText: huge}).Detail.String()
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Now()
			got := tt.got()
			if n := len([]rune(got)); n > limit {
				t.Errorf("result is %d runes, want at most %d", n, limit)
			}
			if d := time.Since(start); d > time.Second {
				t.Errorf("took %v", d)
			}
		})
	}
}

// Ordinary input is untouched by the hardening.
func TestOrdinaryInputIsUnchanged(t *testing.T) {
	isolateRowHome(t)
	if got, want := FailureEvent(jobs.KindPull, "ghcr.io/o/app:1.2", "unauthorized: bad token"),
		"Failed to pull ghcr.io/o/app:1.2 — unauthorized: bad token"; got != want {
		t.Errorf("FailureEvent = %q, want %q", got, want)
	}
	if got, want := FailureEvent(jobs.KindScan, "/x", "échec réseau 日本語 ✓"),
		"Failed to scan /x — échec réseau 日本語 ✓"; got != want {
		t.Errorf("FailureEvent = %q, want %q", got, want)
	}
	got := DescribeRow(RowQuery{Context: rowCtx, Target: apiPath, ObservedText: "exited (0) 3 min ago"})
	if got.Detail.String() != "exited (0) 3 min ago" {
		t.Errorf("observed text = %q", got.Detail.String())
	}
}
