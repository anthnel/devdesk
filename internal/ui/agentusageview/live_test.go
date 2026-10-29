package agentusageview

import (
	"errors"
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/agentusage"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// Live updates, step 4 (DEVDESK-45): the poll chain (answer, wait pollInterval,
// read, answer...) and the rule that stops it. Option A of the plan:
// quietPeriod 5 min, a settled session or a deleted transcript ends the chain.
// Leave/resume (step 5) and the header's live/paused slot (step 6) are not here.

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// ── Helpers ──────────────────────────────────────────────────────────────────

// usageLine is one assistant transcript line for message msgID.
func usageLine(msgID, model string, in, out int) string {
	return fmt.Sprintf(`{"type":"assistant","uuid":"u-%[1]s","message":{"id":%[1]q,"model":%[2]q,"role":"assistant","stop_reason":"end_turn","content":[],"usage":{"input_tokens":%[3]d,"output_tokens":%[4]d,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`,
		msgID, model, in, out)
}

// appendLine appends line and a newline to path.
func appendLine(t *testing.T, path, line string) {
	t.Helper()
	appendRaw(t, path, line+"\n")
}

func appendRaw(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		t.Error(err)
		return
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	if _, err := f.WriteString(s); err != nil {
		t.Error(err)
	}
}

// subDir is the sub-agents directory of the copied session.
func subDir(parent string) string {
	return filepath.Join(strings.TrimSuffix(parent, ".jsonl"), "subagents")
}

// finishTester flips the fixture's one unfinished sub-agent (a02) to finished,
// so nothing keeps the session alive but the recency of its files.
func finishTester(t *testing.T, parent string) {
	t.Helper()
	p := filepath.Join(subDir(parent), "agent-a02.meta.json")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(raw), `"finished":false`, `"finished":true`, 1)
	if edited == string(raw) {
		t.Fatal("the fixture's a02 meta no longer says finished:false")
	}
	if err := os.WriteFile(p, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
}

// ageAll sets every file of the session (and its directories) d in the past.
func ageAll(t *testing.T, parent string, d time.Duration) {
	t.Helper()
	when := time.Now().Add(-d)
	for _, root := range []string{parent, strings.TrimSuffix(parent, ".jsonl")} {
		err := filepath.WalkDir(root, func(p string, _ fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			return os.Chtimes(p, when, when)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// ticksOf keeps the poll ticks among msgs.
func ticksOf(msgs []tea.Msg) []usagePollTickMsg {
	var got []usagePollTickMsg
	for _, m := range msgs {
		if tk, ok := m.(usagePollTickMsg); ok {
			got = append(got, tk)
		}
	}
	return got
}

// runCmd runs cmd with a short poll interval and returns what it produced.
func runCmd(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	testutil.FastTimers(t, &pollInterval)
	return batchMsgs(t, cmd)
}

// answered feeds msg to m (which must have a read in flight) and returns the
// model and the messages the returned Cmd produced.
func answered(t *testing.T, m Model, msg usageDeltaMsg) (Model, []tea.Msg) {
	t.Helper()
	// Before Update: pollTickCmd freezes pollInterval when it is built.
	testutil.FastTimers(t, &pollInterval)
	updated, cmd := m.Update(msg)
	return updated.(Model), runCmd(t, cmd)
}

// pollTick delivers a tick of gen to m.
func pollTick(t *testing.T, m Model, gen int) (Model, []tea.Msg) {
	t.Helper()
	updated, cmd := m.Update(usagePollTickMsg{gen: gen})
	return updated.(Model), batchMsgs(t, cmd)
}

// outputOf is the output tokens of the agent of that sub-agent type.
func outputOf(t *testing.T, m Model, agentType string) int64 {
	t.Helper()
	for _, a := range m.usage.Agents {
		if a.Type == agentType {
			return a.Tokens.Output
		}
	}
	t.Fatalf("no %q agent in the usage", agentType)
	return 0
}

func headerTokens(t *testing.T, m Model) string {
	t.Helper()
	for _, h := range m.GetHeaderInfo("") {
		if h.Key == "Tokens" {
			return h.Value
		}
	}
	t.Fatal("no Tokens header")
	return ""
}

// driver runs Cmds the way Bubble Tea does (concurrently) and feeds the poll
// messages back to Update on the test goroutine. Other messages (spinner ticks)
// are dropped, so no unrelated chain keeps it busy.
type driver struct {
	t       *testing.T
	m       Model
	ch      chan tea.Msg
	pending int
	reads   int // usageDeltaMsg delivered
}

func newDriver(t *testing.T, m Model, cmd tea.Cmd) *driver {
	d := &driver{t: t, m: m, ch: make(chan tea.Msg, 256)}
	d.run(cmd)
	return d
}

func (d *driver) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	d.pending++
	go func() { d.ch <- cmd() }()
}

// until pumps messages until done(model) holds (true), nothing is pending any
// more (false), or the deadline passes (false).
func (d *driver) until(done func(Model) bool, deadline time.Duration) bool {
	d.t.Helper()
	limit := time.After(deadline)
	for {
		if done(d.m) {
			return true
		}
		if d.pending == 0 {
			return false
		}
		select {
		case msg := <-d.ch:
			d.pending--
			d.deliver(msg)
		case <-limit:
			return false
		}
	}
}

func (d *driver) deliver(msg tea.Msg) {
	switch v := msg.(type) {
	case tea.BatchMsg:
		for _, c := range v {
			d.run(c)
		}
	case usageDeltaMsg:
		d.reads++
		d.feed(v)
	case usagePollTickMsg:
		d.feed(v)
	}
}

func (d *driver) feed(msg tea.Msg) {
	updated, cmd := d.m.Update(msg)
	d.m = updated.(Model)
	d.run(cmd)
}

func hasAgents(m Model) bool { return len(m.usage.Agents) > 0 && !m.reading }

// ── The chain: answer → tick → read ──────────────────────────────────────────

func TestAnActiveSessionAnswerSchedulesTheNextTickAndNoRead(t *testing.T) {
	m, _, cmd := openedFixture(t)
	msg := firstDelta(t, cmd)

	m, msgs := answered(t, m, msg)

	ticks := ticksOf(msgs)
	if len(ticks) != 1 || ticks[0].gen != m.agentGen {
		t.Fatalf("want one tick of gen %d, got %+v", m.agentGen, ticks)
	}
	if reads := usageMsgs(msgs); len(reads) != 0 {
		t.Errorf("a tick and a read must never coexist in a generation, got %d reads", len(reads))
	}
	if m.reading {
		t.Error("no read is in flight while the tick is pending")
	}
}

func TestATickOfTheCurrentGenerationStartsExactlyOneRead(t *testing.T) {
	m, _ := atFixture(t)

	m, msgs := pollTick(t, m, m.agentGen)

	reads := usageMsgs(msgs)
	if len(reads) != 1 {
		t.Fatalf("want one read, got %d (%+v)", len(reads), msgs)
	}
	if reads[0].gen != m.agentGen || reads[0].delta.Full() || reads[0].err != nil {
		t.Errorf("poll read: gen=%d full=%v err=%v, want current gen, incremental, clean",
			reads[0].gen, reads[0].delta.Full(), reads[0].err)
	}
	if !m.reading {
		t.Error("reading must be true while the poll read is in flight")
	}
	if m.agentsLoading {
		t.Error("a poll read never shows the footer spinner (Rule 128)")
	}
	if got := ticksOf(msgs); len(got) != 0 {
		t.Errorf("the read's Cmd must not carry a tick, got %+v", got)
	}
}

func TestAPollReadAndItsAnswerLeaveTheFooterQuiet(t *testing.T) {
	m, parent := atFixture(t)
	appendLine(t, parent, usageLine("msg_quiet", "model-alpha", 1, 7))

	m, msgs := pollTick(t, m, m.agentGen)
	reads := usageMsgs(msgs)
	if len(reads) != 1 {
		t.Fatalf("want one read, got %d", len(reads))
	}
	m, _ = answered(t, m, reads[0])

	if m.agentsLoading || m.agentsErr {
		t.Errorf("agentsLoading=%v agentsErr=%v after a poll, want false, false", m.agentsLoading, m.agentsErr)
	}
	// Not footer(m): at the agents level that includes the breadcrumb line.
	if m.footer.IsSet() {
		t.Errorf("a poll must not set a footer message, got %q", m.footer.Text())
	}
	if got := strings.TrimSpace(ansiRe.ReplaceAllString(m.footer.View(120, m.status()), "")); got != "" {
		t.Errorf("a poll must not write a footer message, got %q", got)
	}
}

func TestTicksThatDoNotBelongToTheChainAreIgnored(t *testing.T) {
	cases := map[string]func(t *testing.T) (Model, int){
		"old generation": func(t *testing.T) (Model, int) {
			m, _ := atFixture(t)
			return m, m.agentGen - 1
		},
		"future generation": func(t *testing.T) (Model, int) {
			m, _ := atFixture(t)
			return m, m.agentGen + 1
		},
		"a read is in flight": func(t *testing.T) (Model, int) {
			m, _ := atFixture(t)
			m, msgs := pollTick(t, m, m.agentGen) // starts the read, not answered
			if len(usageMsgs(msgs)) != 1 {
				t.Fatal("precondition: the first tick starts a read")
			}
			return m, m.agentGen
		},
		"sessions level": func(t *testing.T) (Model, int) {
			m, _ := atFixture(t)
			gen := m.agentGen
			m, _ = opened(t, m, "esc")
			return m, gen
		},
		"sessions level, current generation": func(t *testing.T) (Model, int) {
			m, _ := atFixture(t)
			m, _ = opened(t, m, "esc")
			return m, m.agentGen
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			m, gen := setup(t)
			before, reading := m.View(), m.reading

			got, msgs := pollTick(t, m, gen)

			if len(msgs) != 0 {
				t.Errorf("a stray tick must start nothing, got %+v", msgs)
			}
			if got.reading != reading || got.View() != before {
				t.Error("a stray tick must leave the model unchanged")
			}
		})
	}
}

// ── Criterion 1: a new message shows up ──────────────────────────────────────

func TestANewMessageReachesTheScreenThroughTickAndRead(t *testing.T) {
	m, parent := atFixture(t)
	sub := filepath.Join(subDir(parent), "agent-a01.jsonl")
	coder, orchestrator, header := outputOf(t, m, "coder"), mainOutput(t, m), headerTokens(t, m)

	appendLine(t, sub, usageLine("msg_live1", "model-gamma", 3, 1000))
	m, msgs := pollTick(t, m, m.agentGen)
	reads := usageMsgs(msgs)
	if len(reads) != 1 {
		t.Fatalf("want one read, got %d", len(reads))
	}
	m, _ = answered(t, m, reads[0])

	if got := outputOf(t, m, "coder"); got != coder+1000 {
		t.Errorf("coder output = %d, want %d", got, coder+1000)
	}
	if got := mainOutput(t, m); got != orchestrator {
		t.Errorf("orchestrator output moved: %d, want %d", got, orchestrator)
	}
	if got := headerTokens(t, m); got == header {
		t.Errorf("the Tokens header still reads %s", got)
	}
}

// The whole chain on real files and the production interval: the agent writes,
// and the screen is current in under 3 seconds (S-05 criterion 1).
func TestANewMessageIsOnScreenInUnderThreeSeconds(t *testing.T) {
	if testing.Short() {
		t.Skip("wall-clock test with the production poll interval")
	}
	if pollInterval != 2*time.Second {
		t.Fatalf("pollInterval = %v, this test needs the production value", pollInterval)
	}
	m, parent, cmd := openedFixture(t)
	d := newDriver(t, m, cmd)
	if !d.until(hasAgents, 5*time.Second) {
		t.Fatal("the first read never answered")
	}
	sub := filepath.Join(subDir(parent), "agent-a01.jsonl")
	want := outputOf(t, d.m, "coder") + 1000

	t0 := time.Now()
	appendLine(t, sub, usageLine("msg_wall", "model-gamma", 1, 1000))
	ok := d.until(func(m Model) bool { return outputOf(t, m, "coder") == want }, 3*time.Second)

	if elapsed := time.Since(t0); !ok {
		t.Fatalf("coder output not up to date after %v (want %d, polling stopped=%v)", elapsed, want, d.pending == 0)
	}
}

// ── Criterion 2: a new sub-agent joins the tree ──────────────────────────────

func TestANewSubAgentJoinsTheTreeWithoutLeavingTheView(t *testing.T) {
	testutil.FastTimers(t, &pollInterval)
	m, parent, cmd := openedFixture(t)
	d := newDriver(t, m, cmd)
	if !d.until(hasAgents, 5*time.Second) {
		t.Fatal("the first read never answered")
	}
	d.m, _ = opened(t, d.m, "down")
	d.m, _ = opened(t, d.m, "down") // main, coder, tester
	key, ok := d.m.selectedAgentKey()
	if !ok {
		t.Fatal("no selected agent")
	}
	titleBefore := d.m.View()[:strings.Index(d.m.View(), "\n")]

	sub := subDir(parent)
	meta := `{"agentType":"planner","spawnDepth":1,"finished":false}`
	if err := os.WriteFile(filepath.Join(sub, "agent-a00.meta.json"), []byte(meta), 0o600); err != nil {
		t.Fatal(err)
	}
	appendLine(t, filepath.Join(sub, "agent-a00.jsonl"), usageLine("msg_plan", "model-alpha", 2, 30))

	if !d.until(func(m Model) bool { return strings.Contains(m.View(), "planner") }, 5*time.Second) {
		t.Fatalf("the planner never appeared:\n%s", d.m.View())
	}
	if d.m.level != levelAgents {
		t.Error("the view must stay at the agents level")
	}
	if got, _ := d.m.selectedAgentKey(); got != key {
		t.Errorf("selection moved from %q to %q", key, got)
	}
	if after := d.m.View(); after[:strings.Index(after, "\n")] != titleBefore {
		t.Error("the first line of the view changed")
	}
}

// ── The stop rule ────────────────────────────────────────────────────────────

func TestASettledSessionStopsTheChain(t *testing.T) {
	parent := copyFixture(t)
	finishTester(t, parent)
	ageAll(t, parent, 10*time.Minute)
	m := withSessions(t, parent)
	m, cmd := opened(t, m, "right")
	msg := firstDelta(t, cmd)

	m, msgs := answered(t, m, msg)

	if len(msgs) != 0 {
		t.Fatalf("a session quiet for 10 min (> quietPeriod) must schedule nothing, got %+v", msgs)
	}
	if m.reading || m.agentsLoading {
		t.Errorf("reading=%v agentsLoading=%v, want false, false", m.reading, m.agentsLoading)
	}
	if !strings.Contains(m.View(), "├─ coder") {
		t.Error("the rows stay on screen after the chain stops")
	}
	// Stale ticks of the old chain, delivered by hand, start no read.
	for i := range 5 {
		var got []tea.Msg
		m, got = pollTick(t, m, m.agentGen)
		if len(got) != 0 {
			t.Fatalf("tick %d started something on a settled session: %+v", i, got)
		}
	}
}

func TestASessionQuietForLessThanQuietPeriodKeepsPolling(t *testing.T) {
	cases := map[string]struct {
		finish bool
		age    time.Duration
	}{
		"4 minutes of silence":                           {finish: true, age: 4 * time.Minute},
		"an unfinished sub-agent keeps it alive an hour": {finish: false, age: time.Hour},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			parent := copyFixture(t)
			if c.finish {
				finishTester(t, parent)
			}
			ageAll(t, parent, c.age)
			m := withSessions(t, parent)
			m, cmd := opened(t, m, "right")

			_, msgs := answered(t, m, firstDelta(t, cmd))

			if ticks := ticksOf(msgs); len(ticks) != 1 {
				t.Errorf("want one tick, got %+v", msgs)
			}
		})
	}
}

// Settled is judged against the time of the read (usageDeltaMsg.at), not
// against a clock the handler reads on its own.
func TestSettledIsJudgedAtTheTimeOfTheRead(t *testing.T) {
	for name, c := range map[string]struct {
		after     time.Duration
		wantTicks int
	}{
		"read long after the last write": {after: 10 * time.Minute, wantTicks: 0},
		"read right after it":            {after: time.Second, wantTicks: 1},
	} {
		t.Run(name, func(t *testing.T) {
			parent := copyFixture(t)
			finishTester(t, parent) // files are as fresh as they get
			m := withSessions(t, parent)
			m, cmd := opened(t, m, "right")
			msg := firstDelta(t, cmd)
			msg.at = time.Now().Add(c.after)

			_, msgs := answered(t, m, msg)

			if got := len(ticksOf(msgs)); got != c.wantTicks {
				t.Errorf("%d ticks, want %d", got, c.wantTicks)
			}
		})
	}
}

func TestCtrlRAfterTheChainStoppedReadsFullyAndStopsAgainIfStillQuiet(t *testing.T) {
	parent := copyFixture(t)
	finishTester(t, parent)
	ageAll(t, parent, 10*time.Minute)
	m := withSessions(t, parent)
	m, cmd := opened(t, m, "right")
	m, _ = answered(t, m, firstDelta(t, cmd))

	m, cmd = opened(t, m, "ctrl+r")
	full := firstDelta(t, cmd)
	if !full.delta.Full() {
		t.Fatal("ctrl+r must read everything again")
	}
	m, msgs := answered(t, m, full)
	if len(msgs) != 0 {
		t.Errorf("still quiet: no tick after the full read, got %+v", msgs)
	}

	// A line written meanwhile brings the chain back.
	appendLine(t, parent, usageLine("msg_back", "model-alpha", 1, 2))
	m, cmd = opened(t, m, "ctrl+r")
	m, msgs = answered(t, m, firstDelta(t, cmd))
	ticks := ticksOf(msgs)
	if len(ticks) != 1 {
		t.Fatalf("a fresh write must resume polling, got %+v", msgs)
	}

	// The tick is not enough: replayed through Update it must start a read.
	// A chain that stopped earlier must not leave the view deaf to its ticks.
	m, msgs = pollTick(t, m, ticks[0].gen)
	if reads := usageMsgs(msgs); len(reads) != 1 || reads[0].delta.Full() {
		t.Errorf("the resumed tick must start one incremental read, got %+v", msgs)
	}
	if m.pollStopped {
		t.Error("the chain must be live again after the full read")
	}
}

func TestADeletedTranscriptStopsTheChain(t *testing.T) {
	m, parent := atFixture(t)
	if err := os.Remove(parent); err != nil {
		t.Fatal(err)
	}
	m, msgs := pollTick(t, m, m.agentGen)
	reads := usageMsgs(msgs)
	if len(reads) != 1 || !errors.Is(reads[0].err, os.ErrNotExist) {
		t.Fatalf("precondition: the read must fail with ErrNotExist, got %+v", reads)
	}

	m, msgs = answered(t, m, reads[0])

	if len(msgs) != 0 {
		t.Errorf("a deleted transcript ends the polling, got %+v", msgs)
	}
	if !m.agentsErr || !strings.Contains(footer(m), "Failed to read this session") {
		t.Errorf("the failure must show in the footer:\n%s", footer(m))
	}
	if !strings.Contains(m.View(), "├─ coder") {
		t.Error("the rows already shown stay")
	}
	if _, again := pollTick(t, m, m.agentGen); len(again) != 0 {
		t.Errorf("a late tick must not restart a dead chain, got %+v", again)
	}
}

func TestOtherErrorsKeepPollingAndAreLoggedOncePerSeries(t *testing.T) {
	buf := captureLog(t)
	m, _ := atFixture(t)
	boom := errors.New("boom")
	fail := func(err error) []tea.Msg {
		var msgs []tea.Msg
		m, msgs = pollTick(t, m, m.agentGen)
		if len(usageMsgs(msgs)) != 1 {
			t.Fatal("the tick must start a read")
		}
		m, msgs = answered(t, m, usageDeltaMsg{gen: m.agentGen, err: err})
		return msgs
	}

	for i := range 3 {
		if ticks := ticksOf(fail(boom)); len(ticks) != 1 {
			t.Fatalf("failure %d: want a tick to retry, got %+v", i, ticks)
		}
	}
	if n := strings.Count(buf.String(), logTag); n != 1 {
		t.Errorf("3 failures in a row: %d log lines, want 1", n)
	}

	// A wrapped ErrNotExist still stops it (errors.Is, not equality).
	if msgs := fail(fmt.Errorf("read session: %w", os.ErrNotExist)); len(ticksOf(msgs)) != 0 {
		t.Errorf("a wrapped ErrNotExist must stop the chain, got %+v", msgs)
	}
}

func TestASuccessEndsTheSeriesSoTheNextFailureIsLoggedAgain(t *testing.T) {
	buf := captureLog(t)
	m, _ := atFixture(t)
	step := func(msg usageDeltaMsg) {
		var msgs []tea.Msg
		m, msgs = pollTick(t, m, m.agentGen)
		if len(usageMsgs(msgs)) != 1 {
			t.Fatal("the tick must start a read")
		}
		m, _ = answered(t, m, msg)
	}
	step(usageDeltaMsg{gen: m.agentGen, err: errors.New("boom")})
	step(runDelta(t, m.agentGen, m.agg.Cursor()))
	step(usageDeltaMsg{gen: m.agentGen, err: errors.New("boom")})

	if n := strings.Count(buf.String(), logTag); n != 2 {
		t.Errorf("%d log lines, want 2 (two series)", n)
	}
}

// A parent swapped for a directory is a real, non-ErrNotExist failure: the
// session may come back, so the chain goes on.
func TestARealTransientFailureKeepsThePollingAlive(t *testing.T) {
	m, parent := atFixture(t)
	aside := parent + ".aside"
	if err := os.Rename(parent, aside); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	m, msgs := pollTick(t, m, m.agentGen)
	reads := usageMsgs(msgs)
	if len(reads) != 1 || reads[0].err == nil || errors.Is(reads[0].err, os.ErrNotExist) {
		t.Fatalf("precondition: want a failure that is not ErrNotExist, got %+v", reads)
	}

	_, msgs = answered(t, m, reads[0])

	if len(ticksOf(msgs)) != 1 {
		t.Errorf("the chain must go on, got %+v", msgs)
	}
}

// ── ctrl+r typed during a read that fails (review point) ─────────────────────

func TestAFailingReadDoesNotLoseACtrlRTypedMeanwhile(t *testing.T) {
	m, _, cmd := openedFixture(t)
	_ = firstDelta(t, cmd) // the open's read is in flight; its answer will be an error
	m, _ = opened(t, m, "ctrl+r")
	if !m.fullPending {
		t.Fatal("precondition: ctrl+r during a read is pending")
	}

	m, msgs := answered(t, m, usageDeltaMsg{gen: m.agentGen, err: errors.New("boom")})

	reads := usageMsgs(msgs)
	if len(reads) != 1 || !reads[0].delta.Full() || reads[0].err != nil {
		t.Fatalf("the pending full read must still run once, got %+v", msgs)
	}
	if m.fullPending || !m.reading || !m.agentsLoading {
		t.Errorf("fullPending=%v reading=%v agentsLoading=%v, want false, true, true",
			m.fullPending, m.reading, m.agentsLoading)
	}
	if got := ticksOf(msgs); len(got) != 0 {
		t.Errorf("a tick next to the full read would put two reads in a generation: %+v", got)
	}
	m, _ = answered(t, m, reads[0])
	if m.agentsErr || !strings.Contains(m.View(), "├─ coder") {
		t.Error("the full read recovers the screen")
	}
}

// ── Concurrency (-race) ──────────────────────────────────────────────────────

// A writer appends to the parent and two sub-agents in random chunks (lines cut
// anywhere) and creates a third sub-agent half way, while the view polls. Once
// the writer is done the view must hold exactly what a full parse reads.
func TestPollingWhileFilesAreWrittenEndsEqualToAFullParse(t *testing.T) {
	testutil.FastTimers(t, &pollInterval)
	seed := time.Now().UnixNano()
	t.Logf("seed %d", seed)
	rng := rand.New(rand.NewSource(seed))

	m, parent, cmd := openedFixture(t)
	d := newDriver(t, m, cmd)
	if !d.until(hasAgents, 5*time.Second) {
		t.Fatal("the first read never answered")
	}
	sub := subDir(parent)
	targets := []string{parent, filepath.Join(sub, "agent-a01.jsonl"), filepath.Join(sub, "agent-a02.jsonl")}
	newSub := filepath.Join(sub, "agent-a09.jsonl")

	const lines = 60
	var plan [][2]string // target, text of one chunk
	for i := range lines {
		tgt := targets[rng.Intn(len(targets))]
		text := usageLine(fmt.Sprintf("msg_race%d", i), "model-alpha", rng.Intn(50), rng.Intn(500)) + "\n"
		for len(text) > 0 {
			n := min(1+rng.Intn(80), len(text))
			plan = append(plan, [2]string{tgt, text[:n]})
			text = text[n:]
		}
		if i == lines/2 {
			plan = append(plan, [2]string{"META", ""})
			plan = append(plan, [2]string{newSub, usageLine("msg_racenew", "model-beta", 4, 44) + "\n"})
		}
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for _, step := range plan {
			if step[0] == "META" {
				meta := `{"agentType":"planner","spawnDepth":1,"finished":false}`
				if err := os.WriteFile(filepath.Join(sub, "agent-a09.meta.json"), []byte(meta), 0o600); err != nil {
					t.Error(err)
				}
				continue
			}
			appendRaw(t, step[0], step[1])
			time.Sleep(time.Duration(rng.Intn(3)) * time.Millisecond)
		}
	}()
	written := make(chan struct{})
	go func() { wg.Wait(); close(written) }()

	// Poll while the writer works, then until the screen has converged.
	for working := true; working; {
		select {
		case <-written:
			working = false
		default:
			if !d.until(func(Model) bool { return false }, 5*time.Millisecond) {
				time.Sleep(time.Millisecond) // nothing pending: do not spin
			}
		}
	}
	files, err := agentusage.LocateSession(parent)
	if err != nil {
		t.Fatal(err)
	}
	want, err := agentusage.ParseSession(files)
	if err != nil {
		t.Fatal(err)
	}
	ok := d.until(func(m Model) bool { return reflect.DeepEqual(m.usage, want) }, 10*time.Second)

	if !ok {
		t.Fatalf("the polled usage never matched a full parse (reads=%d)\n got  %+v\n want %+v", d.reads, d.m.usage, want)
	}
	if d.reads < 3 {
		t.Errorf("only %d reads: the view did not poll while the files changed", d.reads)
	}
}
