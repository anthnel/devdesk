package fakecli

import (
	"strings"
	"testing"
)

// failedT stands for a test that Install fails: Fatalf records the message and
// ends the goroutine the way testing does, here through a panic the caller
// recovers.
type failedT struct {
	testing.TB
	msg string
}

type fatal struct{}

func (f *failedT) Fatalf(format string, args ...any) {
	f.msg = strings.TrimSpace(format)
	panic(fatal{})
}

// attempt runs fn and reports whether it ended with Fatalf.
func attempt(fn func()) (failed bool) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(fatal); !ok {
				panic(r)
			}
			failed = true
		}
	}()
	fn()
	return false
}

// A test binary whose TestMain never called Main or AsTool must not install the
// fake: it would run the whole suite again each time the fake is started.
func TestInstallRefusesABinaryWithoutAsTool(t *testing.T) {
	asToolCalled.Store(false)
	t.Cleanup(func() { asToolCalled.Store(true) })

	ft := &failedT{TB: t}
	if !attempt(func() { Install(ft, nil, "docker") }) {
		t.Fatal("Install went on although the binary never called AsTool")
	}
	if !strings.Contains(ft.msg, "fakecli.Main or fakecli.AsTool") {
		t.Errorf("message = %q, want it to name the fix", ft.msg)
	}
	ft = &failedT{TB: t}
	if !attempt(func() { (&Fake{}).InstallAt(ft, t.TempDir(), "docker") }) {
		t.Error("InstallAt went on although the binary never called AsTool")
	}
}

// Installing the same name twice must fail instead of writing through the first
// link into the test binary.
func TestAToolNameInstalledTwiceIsRefused(t *testing.T) {
	asToolCalled.Store(true)
	ft := &failedT{TB: t}
	if !attempt(func() { Install(ft, nil, "docker", "docker") }) {
		t.Fatal("Install accepted the same tool twice")
	}
	if !strings.Contains(ft.msg, "installing") {
		t.Errorf("message = %q, want an installation failure", ft.msg)
	}
}
