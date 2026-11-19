package credentials

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

// errSentinelHost is what the mocked go-keyring answers when the guard is
// broken: a guard that does not fire lets this error reach the caller, so a
// test sees "no panic" instead of touching the real host store.
var errSentinelHost = errors.New("sentinel: the real host store must never be reached")

// useGoKeyring makes the production adapter the store in use, and restores the
// previous one at cleanup. It does not call go-keyring.
func useGoKeyring(t *testing.T) {
	t.Helper()
	prev := SwapHostStoreForTest(goKeyring{})
	t.Cleanup(func() { SwapHostStoreForTest(prev) })
}

// withoutAuthorization withdraws the TestMain authorization, as in any package
// that did not isolate the host store, and restores it at cleanup.
func withoutAuthorization(t *testing.T) {
	t.Helper()
	prev := hostReachableUnderTest.Swap(false)
	t.Cleanup(func() { hostReachableUnderTest.Store(prev) })
}

// recoverMessage runs fn and returns the text of its panic, or "" and false
// when fn returned normally.
func recoverMessage(fn func()) (msg string, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			msg, panicked = fmt.Sprint(r), true
		}
	}()
	fn()
	return "", false
}

func TestHostStoreGuardPanicsUnderTest(t *testing.T) {
	tests := []struct {
		name string
		call func()
		op   string // must appear in the panic message
	}{
		{"KeyringAvailable", func() { KeyringAvailable() }, "keyring get"},
		{"Save", func() { _ = NewKeyringStorage("ctx").Save("https://h.example", "secret") }, "keyring set"},
		{"Load", func() { _, _ = NewKeyringStorage("ctx").Load("https://h.example") }, "keyring get"},
		{"Delete", func() { _ = NewKeyringStorage("ctx").Delete("https://h.example") }, "keyring delete"},
		{"runCredential", func() { _, _ = runCredential("fill", "protocol=https\nhost=h.example\n\n") }, "git credential fill"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Even if the guard is broken, nothing below reaches the real store:
			// go-keyring answers with an error instead.
			keyring.MockInitWithError(errSentinelHost)
			t.Cleanup(keyring.MockInit)
			useGoKeyring(t)
			withoutAuthorization(t)

			msg, panicked := recoverMessage(tt.call)
			if !panicked {
				t.Fatalf("%s did not panic: the host store is reachable from a test without isolation", tt.name)
			}
			for _, want := range []string{"DEVDESK-610", "fakekeyring", tt.op} {
				if !strings.Contains(msg, want) {
					t.Errorf("panic message %q does not contain %q", msg, want)
				}
			}
		})
	}
}

func TestHostStoreGuardStandsDownWhenAuthorized(t *testing.T) {
	keyring.MockInit()
	useGoKeyring(t)
	// hostReachableUnderTest is true here, set by TestMain.
	if msg, panicked := recoverMessage(func() { KeyringAvailable() }); panicked {
		t.Fatalf("KeyringAvailable panicked although this package is authorized: %s", msg)
	}
}

// TestHostStoreGuardFailsTheTestThatReachesIt checks the property the story is
// about: the failure names the test that reached the store. It runs the helper
// below in a child test binary, because the helper's panic is the failure.
func TestHostStoreGuardFailsTheTestThatReachesIt(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestHostStoreGuardHelper$", "-test.timeout=60s")
	cmd.Env = append(os.Environ(),
		"DEVDESK_HOSTSTORE_GUARD_HELPER=1",
		// Never let the child look for a session bus, even if the guard is broken.
		"DBUS_SESSION_BUS_ADDRESS=unix:path=/nonexistent",
	)
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("the helper test exited cleanly (err = %v); the guard did not fail it\n%s", err, out)
	}
	for _, want := range []string{"--- FAIL: TestHostStoreGuardHelper", "DEVDESK-610"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("output does not contain %q\n%s", want, out)
		}
	}
}

// TestHostStoreGuardHelper is the test that reaches the store without
// isolation. It only runs as the child of the test above.
func TestHostStoreGuardHelper(t *testing.T) {
	if os.Getenv("DEVDESK_HOSTSTORE_GUARD_HELPER") != "1" {
		t.Skip("child of TestHostStoreGuardFailsTheTestThatReachesIt")
	}
	keyring.MockInitWithError(errSentinelHost)
	t.Cleanup(keyring.MockInit)
	useGoKeyring(t)
	withoutAuthorization(t)
	Select("x", PreferenceAuto)
}

// recordingHost is a HostStore that remembers it was called.
type recordingHost struct {
	err   error // returned by every operation when set
	miss  bool  // Get reports a missing entry
	calls int
}

func (r *recordingHost) Get(service, account string) (string, error) {
	r.calls++
	if r.err != nil {
		return "", r.err
	}
	if r.miss {
		return "", fmt.Errorf("%w: %s/%s", ErrNotFound, service, account)
	}
	return "value", nil
}

func (r *recordingHost) Set(service, account, secret string) error {
	r.calls++
	return r.err
}

func (r *recordingHost) Delete(service, account string) error {
	r.calls++
	return r.err
}

func TestSwapHostStoreForTestReplacesAndRestores(t *testing.T) {
	// The guard must not fire for a swapped store, whatever the authorization.
	withoutAuthorization(t)

	first := &recordingHost{miss: true}
	second := &recordingHost{err: errors.New("locked")}

	orig := SwapHostStoreForTest(first)
	t.Cleanup(func() { SwapHostStoreForTest(orig) })

	if !KeyringAvailable() {
		t.Error("KeyringAvailable() = false on a store that answers with a miss, want true")
	}
	if sel := Select("ctx", PreferenceAuto); sel.Backend != BackendKeyring {
		t.Errorf("Select() backend = %q, want %q", sel.Backend, BackendKeyring)
	}
	if first.calls == 0 {
		t.Error("the swapped store was never called")
	}

	if got := SwapHostStoreForTest(second); got != HostStore(first) {
		t.Errorf("SwapHostStoreForTest returned %v, want the store it replaced", got)
	}
	if KeyringAvailable() {
		t.Error("KeyringAvailable() = true on a store that fails, want false")
	}
	// Pinned to the keyring, an unreachable store falls through to memory.
	if sel := Select("ctx", PreferenceKeyring); sel.Backend != BackendMemory {
		t.Errorf("Select(keyring) on a failing store = %q, want %q", sel.Backend, BackendMemory)
	}

	if got := SwapHostStoreForTest(first); got != HostStore(second) {
		t.Errorf("SwapHostStoreForTest returned %v, want the second store", got)
	}
}

func TestKeyringStorageMapsTheGoKeyringMiss(t *testing.T) {
	keyring.MockInit()
	useGoKeyring(t)

	_, err := NewKeyringStorage("ctx").Load("https://absent.example")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Load() of a missing entry = %v, want an error for which errors.Is(err, ErrNotFound)", err)
	}
	if err := NewKeyringStorage("ctx").Delete("https://absent.example"); err != nil {
		t.Errorf("Delete() of a missing entry = %v, want nil", err)
	}
}
