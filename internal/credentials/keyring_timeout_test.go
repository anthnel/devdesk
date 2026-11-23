package credentials

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// hangingHost is a HostStore that never answers until it is released: what the
// Secret Service does when its collection is locked and the unlock prompt it
// raised cannot be shown or answered (an SSH session, DEVDESK-745).
type hangingHost struct {
	release chan struct{}
	calls   *atomic.Int32
}

func (h hangingHost) wait() { h.calls.Add(1); <-h.release }

func (h hangingHost) Get(string, string) (string, error) { h.wait(); return "", ErrNotFound }
func (h hangingHost) Set(string, string, string) error   { h.wait(); return nil }
func (h hangingHost) Delete(string, string) error        { h.wait(); return nil }

// useHangingHost installs a store that blocks every call until the test ends,
// or until the returned release is called. The release runs before the store
// is swapped back, so no goroutine is left blocked once the test is over.
func useHangingHost(t *testing.T) (h hangingHost, release func()) {
	t.Helper()
	h = hangingHost{release: make(chan struct{}), calls: new(atomic.Int32)}
	prev := SwapHostStoreForTest(h)
	var once sync.Once
	release = func() { once.Do(func() { close(h.release) }) }
	t.Cleanup(func() { SwapHostStoreForTest(prev) })
	t.Cleanup(release)
	return h, release
}

// shortKeyringTimeout shortens the bound for the test, so the cases that do
// not check the production value do not each wait three seconds.
func shortKeyringTimeout(t *testing.T) {
	t.Helper()
	prev := keyringTimeout
	keyringTimeout = 50 * time.Millisecond
	t.Cleanup(func() { keyringTimeout = prev })
}

// returnsWithin runs fn and fails the test if it has not returned after limit.
func returnsWithin[T any](t *testing.T, limit time.Duration, fn func() T) T {
	t.Helper()
	done := make(chan T, 1)
	go func() { done <- fn() }()
	select {
	case v := <-done:
		return v
	case <-time.After(limit):
		t.Fatalf("still blocked after %v: a host store that never answers freezes the caller", limit)
		var zero T
		return zero
	}
}

// watchdog is the bound plus a margin for a loaded CI runner.
const watchdog = 5 * time.Second

// silenceWords is what a Detail says when the host store did not answer: that
// it did not, the likely cause, and the two ways out.
var silenceWords = []string{"did not answer", "locked", "unlock it", "app.secret_backend"}

func requireSilenceExplained(t *testing.T, detail string) {
	t.Helper()
	for _, want := range append(silenceWords, KeyringName()) {
		if !strings.Contains(detail, want) {
			t.Errorf("Detail = %q, want it to contain %q", detail, want)
		}
	}
}

// This case keeps the production bound: it is the one that proves DevDesk's
// launch no longer waits forever, and how long it does wait.
func TestSelectDoesNotWaitForAHostStoreThatNeverAnswers(t *testing.T) {
	useHangingHost(t)
	hermeticGitWithHelper(t, "store")

	start := time.Now()
	sel := returnsWithin(t, watchdog, func() Selection { return Select("work", PreferenceKeyring) })
	if waited := time.Since(start); waited < 3*time.Second {
		t.Errorf("Select gave up after %v, want the 3s bound", waited)
	}

	if sel.Backend != BackendMemory {
		t.Fatalf("Backend = %q, want %q", sel.Backend, BackendMemory)
	}
	if !strings.Contains(sel.Detail, "Nothing is saved") {
		t.Errorf("Detail = %q, want it to say that nothing is saved", sel.Detail)
	}
	requireSilenceExplained(t, sel.Detail)
}

func TestSelectAutoSkipsASilentHostStore(t *testing.T) {
	tests := []struct {
		name   string
		helper string
		want   Backend
	}{
		{"to the git helper when one is usable", "manager", BackendGitCredential},
		{"to memory when none is", "store", BackendMemory},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shortKeyringTimeout(t)
			useHangingHost(t)
			hermeticGitWithHelper(t, tt.helper)

			sel := returnsWithin(t, watchdog, func() Selection { return Select("work", PreferenceAuto) })

			if sel.Backend != tt.want {
				t.Fatalf("Backend = %q, want %q", sel.Backend, tt.want)
			}
			requireSilenceExplained(t, sel.Detail)
		})
	}
}

// An absent store keeps its own message: "did not answer" is only for a store
// that is there and silent, which the user can do something about.
func TestSelectKeepsTheAbsentStoreMessage(t *testing.T) {
	unreachableKeyring(t)
	hermeticGitWithHelper(t, "store")

	for _, pref := range []string{PreferenceAuto, PreferenceKeyring} {
		if d := Select("work", pref).Detail; strings.Contains(d, "did not answer") {
			t.Errorf("Select(%q).Detail = %q, says the store did not answer although it is absent", pref, d)
		}
	}
}

// Every KeyringStorage operation is bounded, not only the probe: a token read
// later in the session would otherwise freeze the view that asked for it.
func TestKeyringStorageOperationsAreBounded(t *testing.T) {
	tests := []struct {
		name string
		call func(*KeyringStorage) error
	}{
		{"Save", func(k *KeyringStorage) error { return k.Save("https://h.example", "secret") }},
		{"Load", func(k *KeyringStorage) error { _, err := k.Load("https://h.example"); return err }},
		{"Delete", func(k *KeyringStorage) error { return k.Delete("https://h.example") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shortKeyringTimeout(t)
			useHangingHost(t)
			k := NewKeyringStorage("work")

			err := returnsWithin(t, watchdog, func() error { return tt.call(k) })

			if !errors.Is(err, ErrKeyringUnresponsive) {
				t.Fatalf("%s error = %v, want ErrKeyringUnresponsive", tt.name, err)
			}
			if !strings.Contains(err.Error(), KeyringName()) {
				t.Errorf("%s error = %q, want it to name %q", tt.name, err, KeyringName())
			}
		})
	}
}

// Once a call is stuck, the store is not asked again while it stays stuck:
// each new call fails at once instead of freezing for the bound again and
// leaving one more goroutine behind.
func TestASilentHostStoreIsNotProbedAgain(t *testing.T) {
	shortKeyringTimeout(t)
	h, _ := useHangingHost(t)

	if KeyringAvailable() {
		t.Fatal("KeyringAvailable() = true for a store that never answers")
	}
	start := time.Now()
	for range 5 {
		if KeyringAvailable() {
			t.Fatal("KeyringAvailable() = true for a store that never answers")
		}
		if _, err := NewKeyringStorage("work").Load("https://h.example"); !errors.Is(err, ErrKeyringUnresponsive) {
			t.Fatalf("Load error = %v, want ErrKeyringUnresponsive", err)
		}
	}
	if waited := time.Since(start); waited >= keyringTimeout {
		t.Errorf("ten later calls took %v, want them to fail at once", waited)
	}
	if got := h.calls.Load(); got != 1 {
		t.Errorf("the store was called %d times, want 1", got)
	}
}

// A store that answers the stuck call (the user unlocked it) is asked again.
func TestAHostStoreThatAnswersLateIsUsedAgain(t *testing.T) {
	shortKeyringTimeout(t)
	h, release := useHangingHost(t)

	if KeyringAvailable() {
		t.Fatal("KeyringAvailable() = true for a store that never answers")
	}
	release()

	deadline := time.Now().Add(watchdog)
	for !KeyringAvailable() {
		if time.Now().After(deadline) {
			t.Fatal("the store answered its stuck call but is still treated as silent")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := h.calls.Load(); got < 2 {
		t.Errorf("the store was called %d times, want it asked again after it answered", got)
	}
}

// A store that answers just after the bound is not counted as stuck forever:
// the stuck count goes back to zero once the late call returns.
func TestTheStuckCountReturnsToZero(t *testing.T) {
	shortKeyringTimeout(t)
	_, release := useHangingHost(t)
	box := currentStore.Load()

	for range 3 {
		_ = KeyringAvailable()
	}
	if got := box.stuck.Load(); got != 1 {
		t.Fatalf("stuck = %d after one timed-out call and two refused ones, want 1", got)
	}
	release()
	deadline := time.Now().Add(watchdog)
	for box.stuck.Load() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("stuck = %d after the store answered, want 0", box.stuck.Load())
		}
		time.Sleep(5 * time.Millisecond)
	}
}
