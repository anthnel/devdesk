package nexusbench

import (
	"context"
	"time"
)

// Clock is the time the waits of the bench read and spend. Tests swap it for a
// fake that never waits, so no test sleeps for real.
type Clock interface {
	// Now returns the current time.
	Now() time.Time
	// Sleep waits for d, or until ctx is done. It returns ctx.Err() when ctx is
	// done, before sleeping if it already was, and nil after the whole wait. A
	// duration of zero or less returns at once (after the ctx check).
	Sleep(ctx context.Context, d time.Duration) error
}

// realClock is the Clock of the command: the wall clock.
type realClock struct{}

// NewClock returns the real clock.
func NewClock() Clock { return realClock{} }

// Now implements Clock.
func (realClock) Now() time.Time { return time.Now() }

// Sleep implements Clock.
func (realClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
