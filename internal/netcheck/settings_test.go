package netcheck

import (
	"context"
	"crypto/x509"
	"testing"
	"time"
)

// The zero Settings must not mean "wait forever" and "send no packets".
//
// Settings is a plain struct, so a caller can build one field by field and miss
// one — and an unfilled CheckTimeout would be a dial with no deadline, which is
// a hang rather than a verdict. Every entry point normalizes, so no stage has
// to check.
func TestZeroSettingsFallBackToTheDefaults(t *testing.T) {
	got := Settings{}.Normalized()

	if got != DefaultSettings() {
		t.Errorf("Settings{}.Normalized() = %+v, want %+v", got, DefaultSettings())
	}
}

// A negative value is the same mistake as a zero one, and gets the same answer.
func TestANegativeSettingIsRefusedTheSameWay(t *testing.T) {
	got := Settings{CheckTimeout: -1, PingCount: -3, ExpiryWarnWindow: -time.Hour}.Normalized()

	if got != DefaultSettings() {
		t.Errorf("a negative Settings normalized to %+v, want the defaults", got)
	}
}

// What the caller did state is left alone.
func TestNormalizeKeepsWhatWasStated(t *testing.T) {
	want := Settings{CheckTimeout: 30 * time.Second, PingCount: 10, ExpiryWarnWindow: 72 * time.Hour}

	if got := want.Normalized(); got != want {
		t.Errorf("Normalized() = %+v, want it untouched at %+v", got, want)
	}
}

// The reachability stage sends what the settings ask for, not a constant.
func TestThePingCountComesFromSettings(t *testing.T) {
	var sent int
	env := fakeEnv{ping: func(_ context.Context, _ string, count int) (PingStats, error) {
		sent = count
		return PingStats{Sent: count, Received: count, AvgRTT: time.Millisecond}, nil
	}}

	var prior Results
	runReach(context.Background(), target(), env, Settings{PingCount: 7}.Normalized(), &prior)

	if sent != 7 {
		t.Errorf("the stage sent %d echo requests, want the 7 the settings asked for", sent)
	}
}

// The expiry check warns on the window the settings name.
//
// Asserted at both ends of one certificate rather than on one: a window read
// from the wrong place would still produce a Warn for some inputs, and a test
// that only ever looked at one would pass on the constant it replaced.
func TestTheExpiryWindowComesFromSettings(t *testing.T) {
	now := time.Now()
	leaf := &x509.Certificate{
		NotBefore: now.Add(-24 * time.Hour),
		NotAfter:  now.Add(45 * 24 * time.Hour),
	}

	if got := expiryCheck(leaf, now, 30*24*time.Hour).Verdict; got != OK {
		t.Errorf("a certificate 45 days out is %v under a 30-day window, want OK", got)
	}
	if got := expiryCheck(leaf, now, 60*24*time.Hour).Verdict; got != Warn {
		t.Errorf("a certificate 45 days out is %v under a 60-day window, want Warn", got)
	}
}

// SystemEnv carries the timeout it was built with, so a run cannot be bounded
// by one number while the config says another.
func TestSystemEnvTakesItsTimeoutFromSettings(t *testing.T) {
	env, ok := SystemEnv(Settings{CheckTimeout: 3 * time.Second}).(systemEnv)
	if !ok {
		t.Fatal("SystemEnv no longer returns a systemEnv")
	}
	if env.timeout != 3*time.Second {
		t.Errorf("timeout = %v, want the 3s the settings asked for", env.timeout)
	}

	zero, _ := SystemEnv(Settings{}).(systemEnv)
	if zero.timeout != DefaultCheckTimeout {
		t.Errorf("a zero Settings produced a %v timeout, want the default %v", zero.timeout, DefaultCheckTimeout)
	}
}
