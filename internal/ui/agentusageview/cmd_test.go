package agentusageview

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/agentusage"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// Live updates, step 2 (DEVDESK-45): the messages and Cmds of the poll chain.

// copyFixture copies the basic fixture into t.TempDir() and returns its parent
// transcript, so a test can append to it and delete it.
func copyFixture(t *testing.T) string {
	t.Helper()
	src := filepath.Join(filepath.Dir(fixtureParent), "..", "..")
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	rel, _ := filepath.Rel(src, fixtureParent)
	return filepath.Join(dst, rel)
}

// runDelta runs readDeltaCmd and returns its answer.
func runDelta(t *testing.T, gen int, c agentusage.Cursor) usageDeltaMsg {
	t.Helper()
	cmd := readDeltaCmd(gen, c)
	if cmd == nil {
		t.Fatal("readDeltaCmd returned nil")
	}
	got, ok := cmd().(usageDeltaMsg)
	if !ok {
		t.Fatalf("readDeltaCmd answered %T, want usageDeltaMsg", cmd())
	}
	return got
}

func TestReadDeltaCmdFirstReadIsFullAndStampedWithGenAndTime(t *testing.T) {
	stamp := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	old := now
	now = func() time.Time { return stamp }
	t.Cleanup(func() { now = old })

	agg := agentusage.NewAggregate(copyFixture(t))
	got := runDelta(t, 7, agg.Cursor())

	if got.gen != 7 {
		t.Errorf("gen = %d, want 7", got.gen)
	}
	if got.err != nil {
		t.Fatalf("err = %v", got.err)
	}
	if !got.delta.Full() {
		t.Error("the first read of a fresh aggregate must be Full")
	}
	if !got.at.Equal(stamp) {
		t.Errorf("at = %v, want the stubbed now %v", got.at, stamp)
	}
}

func TestReadDeltaCmdAfterApplyIsEmptyUntilRewound(t *testing.T) {
	agg := agentusage.NewAggregate(copyFixture(t))
	first := runDelta(t, 1, agg.Cursor())
	if err := agg.Apply(first.delta); err != nil {
		t.Fatal(err)
	}

	again := runDelta(t, 1, agg.Cursor())
	if again.err != nil || again.delta.Full() || !again.delta.Empty() {
		t.Errorf("unchanged files: err=%v full=%v empty=%v, want an empty incremental delta",
			again.err, again.delta.Full(), again.delta.Empty())
	}
	if rewound := runDelta(t, 1, agg.Cursor().Rewound()); !rewound.delta.Full() {
		t.Error("a Rewound cursor must give a Full read")
	}
}

func TestReadDeltaCmdOnADeletedParentErrsWithoutAPath(t *testing.T) {
	parent := copyFixture(t)
	agg := agentusage.NewAggregate(parent)
	if err := os.Remove(parent); err != nil {
		t.Fatal(err)
	}

	got := runDelta(t, 2, agg.Cursor())

	if !errors.Is(got.err, os.ErrNotExist) {
		t.Fatalf("err = %v, want one that satisfies errors.Is(os.ErrNotExist)", got.err)
	}
	if strings.Contains(got.err.Error(), filepath.Dir(parent)) {
		t.Errorf("the error names a directory: %v", got.err)
	}
}

func TestPollTickCmdYieldsATickOfItsGeneration(t *testing.T) {
	testutil.FastTimers(t, &pollInterval)
	if got, ok := pollTickCmd(3)().(usagePollTickMsg); !ok || got.gen != 3 {
		t.Errorf("pollTickCmd(3) = %+v, want usagePollTickMsg{gen: 3}", got)
	}
}

// A caller may inspect a Cmd, then run it again: the wait happens once.
func TestPollTickCmdRunTwiceWaitsOnlyOnce(t *testing.T) {
	old := pollInterval
	pollInterval = 150 * time.Millisecond
	t.Cleanup(func() { pollInterval = old })
	cmd := pollTickCmd(5)

	start := time.Now()
	first := cmd()
	if time.Since(start) < 100*time.Millisecond {
		t.Fatal("the first run must wait for the interval")
	}
	second := time.Now()
	again := cmd()
	if d := time.Since(second); d > 50*time.Millisecond {
		t.Errorf("the second run waited %v, want no wait", d)
	}
	if first != again {
		t.Errorf("the answer must be kept: %+v then %+v", first, again)
	}
}

func TestResumeCmdAnswersAtOnce(t *testing.T) {
	if _, ok := resumeCmd()().(agentsResumeMsg); !ok {
		t.Error("resumeCmd must return agentsResumeMsg")
	}
}

// Production values are pinned, like the footer's three seconds.
func TestPollIntervalAndQuietPeriodProductionValues(t *testing.T) {
	if pollInterval != 2*time.Second {
		t.Errorf("pollInterval = %v, want 2s", pollInterval)
	}
	if quietPeriod != 5*time.Minute {
		t.Errorf("quietPeriod = %v, want 5m", quietPeriod)
	}
}

// Rule 110: the Cmd holds a Cursor copy, so the model's aggregate can be used
// while the read runs. Meaningful under `go test -race`.
func TestReadDeltaCmdDoesNotTouchTheAggregateItsCursorCameFrom(t *testing.T) {
	agg := agentusage.NewAggregate(copyFixture(t))
	cmd := readDeltaCmd(1, agg.Cursor())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		cmd()
	}()

	d, err := agentusage.ReadDelta(agg.Cursor())
	if err != nil {
		t.Fatal(err)
	}
	if err := agg.Apply(d); err != nil {
		t.Fatal(err)
	}
	_ = agg.Usage()
	_ = agg.Activity()
	wg.Wait()
}
