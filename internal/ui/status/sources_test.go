package status

import (
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/viewer"
)

func TestCertSourceDeclaresPlainText(t *testing.T) {
	s := certSource{Target: "example.com", Component: "example"}
	if got, want := s.Name(), "certificate · example"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
	if got := s.Kind(); got != viewer.KindPlain {
		t.Errorf("Kind() = %q, want %q", got, viewer.KindPlain)
	}
}

// The dial fails the same way SSLChecker's own bad-host test does — this is a
// network test, not a unit test, since it depends on DNS resolving nothing.
func TestCertSourceLoadReportsAnUnreachableHost(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}
	s := certSource{Target: "this.host.does.not.exist.invalid", Component: "bad", Timeout: 2 * time.Second}
	if _, err := s.Load(); err == nil {
		t.Error("expected an error for a non-existent host")
	}
}
