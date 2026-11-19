package fakekeyring_test

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/anthnel/devdesk/internal/credentials"
	"github.com/anthnel/devdesk/internal/testutil/fakekeyring"
)

// The fake must satisfy the seam of internal/credentials.
var _ credentials.HostStore = (*fakekeyring.Store)(nil)

// installed is what TestMain's Install returned: the store in use for every
// test that does not call Fresh.
var installed *fakekeyring.Store

// TestMain installs the fake first, as every package that isolates the host
// store does (DEVDESK-610). Without it, a test of this package that forgot
// Fresh would reach the real secret store of the machine.
func TestMain(m *testing.M) {
	installed = fakekeyring.Install()
	os.Exit(m.Run())
}

// errLocked stands for a store that cannot be reached: a locked keyring, no
// D-Bus session.
var errLocked = errors.New("keyring is locked")

func TestStoreRoundTrip(t *testing.T) {
	s := fakekeyring.NewStore()

	if err := s.Set("svc", "acct", "s3cret"); err != nil {
		t.Fatalf("Set() = %v, want nil", err)
	}
	got, err := s.Get("svc", "acct")
	if err != nil || got != "s3cret" {
		t.Fatalf("Get() = %q, %v, want %q, nil", got, err, "s3cret")
	}

	// Set replaces, it does not append or refuse.
	if err := s.Set("svc", "acct", "rotated"); err != nil {
		t.Fatalf("Set() on an existing entry = %v, want nil", err)
	}
	if got, _ := s.Get("svc", "acct"); got != "rotated" {
		t.Errorf("Get() after a second Set = %q, want %q", got, "rotated")
	}

	if err := s.Delete("svc", "acct"); err != nil {
		t.Fatalf("Delete() = %v, want nil", err)
	}
	_, err = s.Get("svc", "acct")
	if !errors.Is(err, credentials.ErrNotFound) {
		t.Errorf("Get() after Delete = %v, want an error for which errors.Is(err, credentials.ErrNotFound)", err)
	}
}

func TestStoreGetOfAMissingEntryIsErrNotFound(t *testing.T) {
	s := fakekeyring.NewStore()

	got, err := s.Get("svc", "never-written")
	if !errors.Is(err, credentials.ErrNotFound) {
		t.Errorf("Get() of a missing entry = %v, want an error for which errors.Is(err, credentials.ErrNotFound)", err)
	}
	if got != "" {
		t.Errorf("Get() of a missing entry returned %q, want an empty secret", got)
	}
	if n := len(s.Entries()); n != 0 {
		t.Errorf("a Get of a missing entry left %d entries behind, want 0 (a probe must not write)", n)
	}
}

// TestStoreDeleteOfAMissingEntryIsNotAnError pins the contract of Delete on an
// absent entry: it succeeds, like the production adapter goKeyring.Delete, which
// maps go-keyring's miss to nil. Logout must not fail because the token was
// already gone, and a fake that was stricter than production would let a test
// pass on a path production never takes. KeyringStorage.Delete accepts both nil
// and ErrNotFound, so either would work today; this one is the one that
// mirrors the adapter.
func TestStoreDeleteOfAMissingEntryIsNotAnError(t *testing.T) {
	s := fakekeyring.NewStore()

	if err := s.Delete("svc", "never-written"); err != nil {
		t.Errorf("Delete() of a missing entry = %v, want nil", err)
	}

	// Through the real caller too: the context-scoped storage logs out cleanly.
	fakekeyring.Fresh(t)
	if err := credentials.NewKeyringStorage("ctx").Delete("https://absent.example"); err != nil {
		t.Errorf("KeyringStorage.Delete() of a missing entry = %v, want nil", err)
	}
}

func TestStoreKeepsServicesAndAccountsApart(t *testing.T) {
	s := fakekeyring.NewStore()

	writes := []struct{ service, account, secret string }{
		{"svc-a", "acct", "a-acct"},
		{"svc-b", "acct", "b-acct"}, // same account, other service
		{"svc-a", "other", "a-other"},
		// A separator inside a name must not make two different pairs collide.
		{"svc", "a\x00b", "sep-1"},
		{"svc\x00a", "b", "sep-2"},
	}
	for _, w := range writes {
		if err := s.Set(w.service, w.account, w.secret); err != nil {
			t.Fatalf("Set(%q, %q) = %v", w.service, w.account, err)
		}
	}
	for _, w := range writes {
		got, err := s.Get(w.service, w.account)
		if err != nil || got != w.secret {
			t.Errorf("Get(%q, %q) = %q, %v, want %q", w.service, w.account, got, err, w.secret)
		}
	}
	if n := len(s.Entries()); n != len(writes) {
		t.Errorf("Entries() has %d entries, want %d", n, len(writes))
	}

	// Deleting one entry leaves its neighbours alone.
	if err := s.Delete("svc-a", "acct"); err != nil {
		t.Fatalf("Delete() = %v", err)
	}
	if got, err := s.Get("svc-b", "acct"); err != nil || got != "b-acct" {
		t.Errorf("Get(svc-b, acct) after deleting svc-a/acct = %q, %v, want %q", got, err, "b-acct")
	}
	if got, err := s.Get("svc-a", "other"); err != nil || got != "a-other" {
		t.Errorf("Get(svc-a, other) after deleting svc-a/acct = %q, %v, want %q", got, err, "a-other")
	}
}

func TestStoreEntriesReturnsACopy(t *testing.T) {
	s := fakekeyring.NewStore()
	if err := s.Set("svc", "acct", "s3cret"); err != nil {
		t.Fatal(err)
	}

	e := s.Entries()
	if len(e) != 1 {
		t.Fatalf("Entries() has %d entries, want 1", len(e))
	}
	for k := range e {
		e[k] = "tampered"
	}
	e["injected"] = "x"

	if got, _ := s.Get("svc", "acct"); got != "s3cret" {
		t.Errorf("Get() after mutating the Entries() copy = %q, want %q", got, "s3cret")
	}
	if n := len(s.Entries()); n != 1 {
		t.Errorf("Entries() has %d entries after mutating a copy, want 1", n)
	}
}

func TestUnreachableStoreFailsEveryOperation(t *testing.T) {
	s := fakekeyring.NewUnreachableStore(errLocked)

	ops := []struct {
		name string
		call func() error
	}{
		{"Get", func() error { _, err := s.Get("svc", "acct"); return err }},
		{"Set", func() error { return s.Set("svc", "acct", "s3cret") }},
		{"Delete", func() error { return s.Delete("svc", "acct") }},
	}
	for _, op := range ops {
		t.Run(op.name, func(t *testing.T) {
			err := op.call()
			if !errors.Is(err, errLocked) {
				t.Errorf("%s() = %v, want the error the store was built with", op.name, err)
			}
			if errors.Is(err, credentials.ErrNotFound) {
				t.Errorf("%s() = %v: an unreachable store must not look like a miss, KeyringAvailable would call it reachable", op.name, err)
			}
		})
	}
	if n := len(s.Entries()); n != 0 {
		t.Errorf("an unreachable store holds %d entries after a failed Set, want 0", n)
	}
}

// TestFreshStartsEmptyAndRestores checks the two promises of Fresh: each test
// starts from an empty store, and the store in use before is back at cleanup.
func TestFreshStartsEmptyAndRestores(t *testing.T) {
	// A witness is the store in use when the subtests start. It holds a secret
	// that a Fresh store must not show and that must reappear afterwards.
	witness := fakekeyring.NewStore()
	prev := credentials.SwapHostStoreForTest(witness)
	t.Cleanup(func() { credentials.SwapHostStoreForTest(prev) })

	ks := credentials.NewKeyringStorage("ctx")
	if err := ks.Save("https://h.example", "witness-token"); err != nil {
		t.Fatalf("Save() on the witness = %v", err)
	}

	var firstFresh *fakekeyring.Store

	t.Run("first test writes", func(t *testing.T) {
		firstFresh = fakekeyring.Fresh(t)
		if firstFresh == nil {
			t.Fatal("Fresh() returned nil")
		}
		if n := len(firstFresh.Entries()); n != 0 {
			t.Fatalf("Fresh() store has %d entries, want it empty", n)
		}
		if _, err := ks.Load("https://h.example"); !errors.Is(err, credentials.ErrNotFound) {
			t.Fatalf("Load() under Fresh = %v, want ErrNotFound: the witness secret leaked into the fresh store", err)
		}
		// The code under test reaches the fake that Fresh returned.
		if err := ks.Save("https://h.example", "first-token"); err != nil {
			t.Fatalf("Save() under Fresh = %v", err)
		}
		if n := len(firstFresh.Entries()); n != 1 {
			t.Errorf("the Fresh store holds %d entries after a Save, want 1: Fresh did not install it", n)
		}
	})

	t.Run("second test starts empty again", func(t *testing.T) {
		second := fakekeyring.Fresh(t)
		if second == firstFresh {
			t.Error("Fresh() returned the same store twice, want a new one per test")
		}
		if n := len(second.Entries()); n != 0 {
			t.Errorf("a second Fresh() store has %d entries, want it empty: the first test's secret survived", n)
		}
		if _, err := ks.Load("https://h.example"); !errors.Is(err, credentials.ErrNotFound) {
			t.Errorf("Load() under the second Fresh = %v, want ErrNotFound", err)
		}
	})

	// Both subtests are over and their cleanups ran: the witness is back.
	got, err := ks.Load("https://h.example")
	if err != nil || got != "witness-token" {
		t.Errorf("Load() after the subtests = %q, %v, want %q: Fresh did not restore the previous store", got, err, "witness-token")
	}
}

func TestFreshNestsAndRestoresInOrder(t *testing.T) {
	outer := fakekeyring.Fresh(t)
	ks := credentials.NewKeyringStorage("ctx")
	if err := ks.Save("https://h.example", "outer-token"); err != nil {
		t.Fatal(err)
	}

	t.Run("inner", func(t *testing.T) {
		inner := fakekeyring.Fresh(t)
		if inner == outer {
			t.Error("a nested Fresh() returned the outer store")
		}
		if _, err := ks.Load("https://h.example"); !errors.Is(err, credentials.ErrNotFound) {
			t.Errorf("Load() under the inner Fresh = %v, want ErrNotFound", err)
		}
	})

	if got, err := ks.Load("https://h.example"); err != nil || got != "outer-token" {
		t.Errorf("Load() after the inner test = %q, %v, want %q: the outer Fresh store was not restored", got, err, "outer-token")
	}
}

// TestFreshUnreachableMakesTheHostStoreFail checks FreshUnreachable end to end
// and that it restores the previous store.
func TestFreshUnreachableMakesTheHostStoreFail(t *testing.T) {
	healthy := fakekeyring.Fresh(t)
	ks := credentials.NewKeyringStorage("ctx")

	t.Run("unreachable", func(t *testing.T) {
		s := fakekeyring.FreshUnreachable(t, errLocked)
		if s == nil {
			t.Fatal("FreshUnreachable() returned nil")
		}
		if credentials.KeyringAvailable() {
			t.Error("KeyringAvailable() = true under FreshUnreachable, want false")
		}
		// Pinned to the keyring, an unreachable store falls through to memory.
		// In auto, Select would then try git, whose answer depends on the machine.
		if sel := credentials.Select("ctx", credentials.PreferenceKeyring); sel.Backend != credentials.BackendMemory {
			t.Errorf("Select(keyring) under FreshUnreachable = %q, want %q", sel.Backend, credentials.BackendMemory)
		}
		if err := ks.Save("https://h.example", "x"); !errors.Is(err, errLocked) {
			t.Errorf("Save() under FreshUnreachable = %v, want an error wrapping the given one", err)
		}
		if _, err := ks.Load("https://h.example"); !errors.Is(err, errLocked) || errors.Is(err, credentials.ErrNotFound) {
			t.Errorf("Load() under FreshUnreachable = %v, want the given error and not ErrNotFound", err)
		}
		if err := ks.Delete("https://h.example"); !errors.Is(err, errLocked) {
			t.Errorf("Delete() under FreshUnreachable = %v, want an error wrapping the given one", err)
		}
	})

	if !credentials.KeyringAvailable() {
		t.Error("KeyringAvailable() = false after the unreachable test, want the previous healthy store back")
	}
	if err := ks.Save("https://h.example", "after"); err != nil {
		t.Errorf("Save() after the unreachable test = %v", err)
	}
	if n := len(healthy.Entries()); n != 1 {
		t.Errorf("the previous store holds %d entries, want 1: it was not the one restored", n)
	}
}

// TestInstallPutsTheFakeInUse checks the TestMain entry point: the store Install
// returned is the one the credentials package uses until something else is
// swapped in.
func TestInstallPutsTheFakeInUse(t *testing.T) {
	if installed == nil {
		t.Fatal("Install() returned nil")
	}
	// Install is never restored, so the store in use here is the installed one
	// as long as no test of this package leaked a swap.
	if !credentials.KeyringAvailable() {
		t.Error("KeyringAvailable() = false under the installed fake, want true")
	}
	ks := credentials.NewKeyringStorage("install-check")
	if err := ks.Save("https://h.example", "installed-token"); err != nil {
		t.Fatalf("Save() = %v", err)
	}
	t.Cleanup(func() { _ = ks.Delete("https://h.example") })

	got, err := installed.Get("devdesk", "install-check/https://h.example")
	if err != nil || got != "installed-token" {
		t.Errorf("the store returned by Install() has %q, %v, want %q: Install did not put it in use", got, err, "installed-token")
	}
}

// TestSelectUnderFakeIgnoresTheSessionBus is AC 4: with the fake installed, the
// answer of Select does not depend on the session bus of the machine.
func TestSelectUnderFakeIgnoresTheSessionBus(t *testing.T) {
	fakekeyring.Fresh(t)

	unsetBus := func(t *testing.T) {
		t.Helper()
		t.Setenv("DBUS_SESSION_BUS_ADDRESS", "") // registers the restore
		if err := os.Unsetenv("DBUS_SESSION_BUS_ADDRESS"); err != nil {
			t.Fatal(err)
		}
	}
	deadBus := func(t *testing.T) {
		t.Helper()
		t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/nonexistent")
	}

	type answer struct{ backend, detail string }
	read := func(preference string) answer {
		sel := credentials.Select("x", preference)
		return answer{string(sel.Backend), sel.Detail}
	}

	for _, preference := range []string{credentials.PreferenceAuto, credentials.PreferenceKeyring} {
		t.Run(preference, func(t *testing.T) {
			var without, with answer
			t.Run("no bus variable", func(t *testing.T) {
				unsetBus(t)
				without = read(preference)
			})
			t.Run("bus path does not exist", func(t *testing.T) {
				deadBus(t)
				with = read(preference)
			})

			if without != with {
				t.Errorf("Select(%q) differs with the session bus: unset %+v, dead %+v", preference, without, with)
			}
			if without.backend != string(credentials.BackendKeyring) {
				t.Errorf("Select(%q) backend = %q, want %q under the fake", preference, without.backend, credentials.BackendKeyring)
			}
		})
	}

	// The storage Select returns works under a dead bus too.
	deadBus(t)
	sel := credentials.Select("x", credentials.PreferenceAuto)
	if err := sel.Storage.Save("https://h.example", "tok"); err != nil {
		t.Fatalf("Save() with a dead session bus = %v", err)
	}
	if got, err := sel.Storage.Load("https://h.example"); err != nil || got != "tok" {
		t.Errorf("Load() with a dead session bus = %q, %v, want %q", got, err, "tok")
	}
}

// TestStoreIsSafeForConcurrentUse is meant to run under -race: Cmds of a Bubble
// Tea program read and write the store from their own goroutines.
func TestStoreIsSafeForConcurrentUse(t *testing.T) {
	s := fakekeyring.NewStore()

	const workers, perWorker = 8, 200
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			account := fmt.Sprintf("acct-%d", w)
			for i := 0; i < perWorker; i++ {
				secret := fmt.Sprintf("secret-%d-%d", w, i)
				if err := s.Set("svc", account, secret); err != nil {
					t.Errorf("Set() = %v", err)
					return
				}
				// Only this goroutine writes this account, so it reads back its own write.
				if got, err := s.Get("svc", account); err != nil || got != secret {
					t.Errorf("Get(%q) = %q, %v, want %q", account, got, err, secret)
					return
				}
				// Reads of other accounts and the copy race with the writers.
				_, _ = s.Get("svc", fmt.Sprintf("acct-%d", (w+1)%workers))
				_ = s.Entries()
				if i%50 == 49 {
					if err := s.Delete("svc", "scratch"); err != nil {
						t.Errorf("Delete() = %v", err)
						return
					}
				}
			}
		}()
	}
	wg.Wait()

	if n := len(s.Entries()); n != workers {
		t.Errorf("Entries() has %d entries after the run, want %d (one account per worker)", n, workers)
	}
}

// TestFreshIsSafeAgainstAConcurrentReader checks the seam itself: a Cmd that
// outlives its test reads the current store while Fresh's cleanup swaps it
// back. Run under -race.
func TestFreshIsSafeAgainstAConcurrentReader(t *testing.T) {
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				credentials.KeyringAvailable()
			}
		}
	}()

	for i := 0; i < 50; i++ {
		t.Run(fmt.Sprintf("round-%d", i), func(t *testing.T) {
			fakekeyring.Fresh(t)
		})
	}
	close(stop)
	wg.Wait()
}
