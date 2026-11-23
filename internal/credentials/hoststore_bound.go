package credentials

import (
	"errors"
	"sync"
	"time"
)

// keyringTimeout bounds every call to the host secret store (DEVDESK-745).
//
// go-keyring has no deadline of its own. On Linux, a read from a locked Secret
// Service collection asks the daemon to unlock it, and go-keyring then waits
// for the prompt's Completed signal with no timeout: in an SSH session, where
// no prompt can be shown or answered, the wait never ends. Select runs before
// the first frame, so the screen stayed blank.
//
// Three seconds is longer than a store that answers ever takes (a few
// milliseconds over D-Bus) and short enough for a launch that has to give up
// on it. A variable only so the tests of this package can shorten it.
var keyringTimeout = 3 * time.Second

// ErrKeyringUnresponsive is returned by a host store call that did not answer
// within keyringTimeout, and at once by every later call while that one is
// still blocked.
var ErrKeyringUnresponsive = errors.New("the host secret store did not answer in time")

// boundedHost runs each call of the store in box with a deadline.
//
// A call that misses the deadline is abandoned, not cancelled: the HostStore
// interface has no context and go-keyring offers no way to stop a wait, so the
// goroutine running it stays blocked until the store answers, possibly for the
// rest of the process (one goroutine and one D-Bus connection). While such a
// call is pending, every new call fails at once with ErrKeyringUnresponsive
// instead of waiting again and leaking another goroutine: a store judged mute
// is not probed again until it has answered the call that was stuck.
type boundedHost struct{ box *hostBox }

func (h boundedHost) Get(service, account string) (string, error) {
	return h.call(func(s HostStore) (string, error) { return s.Get(service, account) })
}

func (h boundedHost) Set(service, account, secret string) error {
	_, err := h.call(func(s HostStore) (string, error) { return "", s.Set(service, account, secret) })
	return err
}

func (h boundedHost) Delete(service, account string) error {
	_, err := h.call(func(s HostStore) (string, error) { return "", s.Delete(service, account) })
	return err
}

// hostResult is what a call to the store produced. panicked carries a panic
// (the DEVDESK-610 test guard) back to the caller's goroutine, so it still
// fails the test that reached the store.
type hostResult struct {
	secret   string
	err      error
	panicked any
}

func (r hostResult) unwrap() (string, error) {
	if r.panicked != nil {
		panic(r.panicked)
	}
	return r.secret, r.err
}

func (h boundedHost) call(op func(HostStore) (string, error)) (string, error) {
	box := h.box
	if box.stuck.Load() > 0 {
		return "", ErrKeyringUnresponsive
	}

	var mu sync.Mutex
	abandoned := false
	done := make(chan hostResult, 1)
	go func() {
		var r hostResult
		defer func() {
			if p := recover(); p != nil {
				r.panicked = p
			}
			// The result is delivered under the lock, so the caller either
			// sees it or has already counted this call as stuck, never neither.
			mu.Lock()
			defer mu.Unlock()
			if abandoned {
				box.stuck.Add(-1)
			}
			done <- r
		}()
		r.secret, r.err = op(box.store)
	}()

	timer := time.NewTimer(keyringTimeout)
	defer timer.Stop()
	select {
	case r := <-done:
		return r.unwrap()
	case <-timer.C:
	}

	mu.Lock()
	defer mu.Unlock()
	select {
	case r := <-done:
		return r.unwrap()
	default:
	}
	abandoned = true
	box.stuck.Add(1)
	return "", ErrKeyringUnresponsive
}
