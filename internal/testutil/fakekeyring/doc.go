// Package fakekeyring is a fake of the host secret manager for tests
// (DEVDESK-610). It implements credentials.HostStore in memory, so a test never
// reaches the real Secret Service, Keychain or Credential Manager of the machine
// that runs it.
//
// It imports testing and is meant to be imported from _test.go files only, so
// it never reaches the production binary (rule K3, enforced by TestHostStoreGuard*
// in internal/credentials/hoststore_guard_test.go).
//
// Call Install in the TestMain of a package whose tests reach
// credentials.Select, KeyringStorage or KeyringAvailable, and Fresh(t) in each
// test that stores or reads a secret, so tests share no state.
// FreshUnreachable(t, err) stands for a locked or absent keyring. Install and
// Fresh act on one process-wide state, so they do not combine with t.Parallel.
//
// A test binary that does neither panics at the first access to the real store,
// with a message naming this package: see the guard in internal/credentials
// (hoststore.go, refuseUnderTest).
package fakekeyring
