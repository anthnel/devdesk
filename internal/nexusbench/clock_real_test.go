package nexusbench

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRealClock(t *testing.T) {
	clk := NewClock()

	t.Run("a done context wins, even for a zero duration", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		for _, d := range []time.Duration{0, time.Hour} {
			if err := clk.Sleep(ctx, d); !errors.Is(err, context.Canceled) {
				t.Errorf("Sleep(%v) with a cancelled context = %v, want context.Canceled", d, err)
			}
		}
	})

	t.Run("zero or negative durations return at once", func(t *testing.T) {
		for _, d := range []time.Duration{0, -time.Hour} {
			if err := clk.Sleep(context.Background(), d); err != nil {
				t.Errorf("Sleep(%v) = %v", d, err)
			}
		}
	})

	t.Run("cancelling interrupts a long sleep", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(10*time.Millisecond, cancel)
		start := time.Now()
		if err := clk.Sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
			t.Fatalf("Sleep = %v, want context.Canceled", err)
		}
		if time.Since(start) > 30*time.Second {
			t.Error("Sleep did not return when the context was cancelled")
		}
	})

	t.Run("a short sleep completes and Now moves forward", func(t *testing.T) {
		before := clk.Now()
		if err := clk.Sleep(context.Background(), 5*time.Millisecond); err != nil {
			t.Fatalf("Sleep = %v", err)
		}
		if !clk.Now().After(before) {
			t.Error("Now did not move forward")
		}
	})
}
