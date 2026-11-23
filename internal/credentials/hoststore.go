package credentials

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/zalando/go-keyring"
)

// hostStoreUnderTestMessage is the panic text of refuseUnderTest. It names what
// to do, because the person reading it is a developer whose test just reached
// the real secret store of the machine (DEVDESK-610).
const hostStoreUnderTestMessage = "credentials: a test reached the host secret store (%s) " +
	"without isolation; call fakekeyring.Install() in TestMain or fakekeyring.Fresh(t) " +
	"(DEVDESK-610, docs/architecture/configuration.md)"

// HostStore is the host secret manager as KeyringStorage and KeyringAvailable
// see it. The contract on a missing entry:
//   - Get returns an error for which errors.Is(err, ErrNotFound). go-keyring's
//     own ErrNotFound is not recognized here; goKeyring maps it.
//   - Delete returns nil.
//
// An implementation that returns any other error for a missing entry is seen as
// a real failure of the store: KeyringAvailable reports it unreachable.
type HostStore interface {
	Get(service, account string) (string, error)
	Set(service, account, secret string) error
	Delete(service, account string) error
}

// hostBox lets an atomic.Pointer hold an interface value, so a Cmd reading the
// current store while a t.Cleanup restores the previous one is not a data race.
// It also carries the count of calls to that store that outlived
// keyringTimeout (see boundedHost): a swap starts a fresh count.
type hostBox struct {
	store HostStore
	stuck atomic.Int32
}

var currentStore atomic.Pointer[hostBox]

// defaultBox holds the production store when no test swapped it. It is a
// single value so that its stuck count lasts for the whole process.
var defaultBox = &hostBox{store: goKeyring{}}

// hostReachableUnderTest is set only by the TestMain of this package, whose
// tests mock go-keyring themselves and run git against a hermetic config. In
// any other test binary it stays false, so the real store is refused.
var hostReachableUnderTest atomic.Bool

// currentHost returns the store in use, goKeyring{} unless a test swapped it,
// with every call bounded by keyringTimeout (DEVDESK-745).
func currentHost() HostStore {
	if b := currentStore.Load(); b != nil {
		return boundedHost{box: b}
	}
	return boundedHost{box: defaultBox}
}

// SwapHostStoreForTest installs s and returns the store it replaces. It panics
// outside a test binary and when s is nil. Only _test.go files and
// internal/testutil/fakekeyring may call it.
func SwapHostStoreForTest(s HostStore) HostStore {
	if !testing.Testing() {
		panic("credentials: SwapHostStoreForTest called outside a test binary")
	}
	if s == nil {
		panic("credentials: SwapHostStoreForTest called with a nil HostStore; pass the store to restore, as returned by the previous call")
	}
	if prev := currentStore.Swap(&hostBox{store: s}); prev != nil {
		return prev.store
	}
	return goKeyring{}
}

// refuseUnderTest panics with hostStoreUnderTestMessage when the code runs in a
// test binary that has not allowed the real host store. It panics rather than
// returning an error: an error would make Select fall back to memory and the
// test would pass, leaving the guard silent. op names the operation reached.
func refuseUnderTest(op string) {
	if testing.Testing() && !hostReachableUnderTest.Load() {
		panic(fmt.Sprintf(hostStoreUnderTestMessage, op))
	}
}

// goKeyring is the production HostStore and the only place go-keyring is
// called. Every method runs the test guard first.
type goKeyring struct{}

func (goKeyring) Get(service, account string) (string, error) {
	refuseUnderTest("keyring get")
	secret, err := keyring.Get(service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	return secret, err
}

func (goKeyring) Set(service, account, secret string) error {
	refuseUnderTest("keyring set")
	return keyring.Set(service, account, secret)
}

func (goKeyring) Delete(service, account string) error {
	refuseUnderTest("keyring delete")
	err := keyring.Delete(service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
