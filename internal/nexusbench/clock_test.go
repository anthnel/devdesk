package nexusbench

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeClock is the Clock of the tests: Sleep never waits, it moves the clock
// forward by the requested duration. Destroy (step 7) and the readiness wait
// (step 4) run against it, so no test sleeps for real.
//
// Hypotheses about the Clock contract, which the real clock (clock.go) must keep:
//   - Sleep returns ctx.Err() as soon as ctx is done, before and instead of
//     sleeping, and nil after sleeping the whole duration;
//   - a caller never sleeps for zero or less (that would be a busy loop).
type fakeClock struct {
	t *testing.T

	now    time.Time
	sleeps []time.Duration // every Sleep call, in order

	// onSleep, when set, runs at the start of the n-th Sleep (1-based), before
	// ctx is looked at: a test cancels the context from it.
	onSleep func(n int)

	// maxSleeps stops a runaway loop: past it the fake reports the failure and
	// returns an error instead of letting the code under test spin forever.
	maxSleeps int
}

var _ Clock = (*fakeClock)(nil)

// fakeClockStart is the instant every fake clock starts at.
var fakeClockStart = time.Date(2026, time.November, 16, 12, 0, 0, 0, time.UTC)

func newFakeClock(t *testing.T) *fakeClock {
	t.Helper()
	return &fakeClock{t: t, now: fakeClockStart, maxSleeps: 5000}
}

// Now implements Clock.
func (c *fakeClock) Now() time.Time { return c.now }

// Sleep implements Clock.
func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	c.t.Helper()
	c.sleeps = append(c.sleeps, d)
	if c.onSleep != nil {
		c.onSleep(len(c.sleeps))
	}
	if d <= 0 {
		c.t.Errorf("Sleep(%v): a wait must sleep for a positive duration", d)
	}
	if len(c.sleeps) > c.maxSleeps {
		c.t.Errorf("more than %d sleeps: the code under test loops without end", c.maxSleeps)
		return errors.New("fake clock: runaway loop")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c.now = c.now.Add(d)
	return nil
}

// elapsed is how far the clock moved since it started.
func (c *fakeClock) elapsed() time.Duration { return c.now.Sub(fakeClockStart) }

func TestFakeClock(t *testing.T) {
	t.Run("sleeping moves the clock and records the duration", func(t *testing.T) {
		clk := newFakeClock(t)
		for _, d := range []time.Duration{2 * time.Second, 3 * time.Second} {
			if err := clk.Sleep(context.Background(), d); err != nil {
				t.Fatalf("Sleep(%v) = %v", d, err)
			}
		}
		if got, want := clk.elapsed(), 5*time.Second; got != want {
			t.Errorf("elapsed = %v, want %v", got, want)
		}
		if len(clk.sleeps) != 2 {
			t.Errorf("recorded %d sleeps, want 2", len(clk.sleeps))
		}
	})

	t.Run("a done context stops the sleep and leaves the clock alone", func(t *testing.T) {
		clk := newFakeClock(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := clk.Sleep(ctx, time.Second); !errors.Is(err, context.Canceled) {
			t.Errorf("Sleep with a cancelled context = %v, want context.Canceled", err)
		}
		if clk.elapsed() != 0 {
			t.Errorf("the clock moved by %v although the sleep was cancelled", clk.elapsed())
		}
	})

	t.Run("onSleep can cancel the context at the n-th sleep", func(t *testing.T) {
		clk := newFakeClock(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		clk.onSleep = func(n int) {
			if n == 2 {
				cancel()
			}
		}
		if err := clk.Sleep(ctx, time.Second); err != nil {
			t.Fatalf("first Sleep = %v", err)
		}
		if err := clk.Sleep(ctx, time.Second); !errors.Is(err, context.Canceled) {
			t.Errorf("second Sleep = %v, want context.Canceled", err)
		}
	})
}
