package status

import (
	"context"
	"testing"
	"time"

	"gitlab.com/anthnell/devsecops/devdesk/internal/config"
)

// ── buildURL ─────────────────────────────────────────────────────────────────

func TestBuildURL(t *testing.T) {
	tests := []struct {
		compType string
		target   string
		want     string
	}{
		{"https", "https://example.com/path", "https://example.com/path"},
		{"http", "http://example.com", "http://example.com"},
		{"https", "example.com", "https://example.com"},
		{"http", "example.com", "http://example.com"},
		{"unknown", "example.com", "https://example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.compType+":"+tt.target, func(t *testing.T) {
			got := buildURL(tt.compType, tt.target)
			if got != tt.want {
				t.Errorf("buildURL(%q, %q) = %q, want %q", tt.compType, tt.target, got, tt.want)
			}
		})
	}
}

// ── NewHTTPChecker ────────────────────────────────────────────────────────────

func TestNewHTTPChecker(t *testing.T) {
	c := NewHTTPChecker(5 * time.Second)
	if c == nil {
		t.Fatal("NewHTTPChecker() returned nil")
	}
	if c.timeout != 5*time.Second {
		t.Errorf("expected timeout 5s, got %v", c.timeout)
	}
	if c.client == nil {
		t.Error("http client should not be nil")
	}
}

func TestHTTPChecker_EmptyTarget(t *testing.T) {
	c := NewHTTPChecker(2 * time.Second)
	result := c.Check(context.Background(), config.ComponentConfig{
		Name: "empty-target",
		Type: "http",
	})
	if result.Status != StatusError {
		t.Errorf("expected StatusError for empty target, got %s", result.Status)
	}
}

func TestHTTPChecker_InvalidURL(t *testing.T) {
	c := NewHTTPChecker(2 * time.Second)
	result := c.Check(context.Background(), config.ComponentConfig{
		Name:   "bad-url",
		Type:   "http",
		Target: "://not a url",
	})
	if result.Status == StatusOK {
		t.Error("expected non-OK status for invalid URL")
	}
}

func TestHTTPChecker_Timestamp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}
	c := NewHTTPChecker(5 * time.Second)
	before := time.Now()
	result := c.Check(context.Background(), config.ComponentConfig{
		Name:   "ts-test",
		Type:   "https",
		Target: "https://example.com",
	})
	after := time.Now()

	if result.Timestamp.Before(before) || result.Timestamp.After(after) {
		t.Errorf("timestamp %v not within expected range [%v, %v]", result.Timestamp, before, after)
	}
}

// ── NewDNSChecker ─────────────────────────────────────────────────────────────

func TestNewDNSChecker(t *testing.T) {
	c := NewDNSChecker(3*time.Second, "")
	if c == nil {
		t.Fatal("NewDNSChecker() returned nil")
	}
	if c.timeout != 3*time.Second {
		t.Errorf("expected timeout 3s, got %v", c.timeout)
	}
}

func TestDNSChecker_EmptyTarget(t *testing.T) {
	c := NewDNSChecker(2*time.Second, "")
	result := c.Check(context.Background(), config.ComponentConfig{
		Name: "no-target",
		Type: "dns",
	})
	if result.Status != StatusError {
		t.Errorf("expected StatusError for empty target, got %s", result.Status)
	}
}

func TestDNSChecker_UnresolvableHost(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}
	c := NewDNSChecker(2*time.Second, "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := c.Check(ctx, config.ComponentConfig{
		Name:   "bad-host",
		Type:   "dns",
		Target: "this.domain.definitely.does.not.exist.invalid",
	})
	if result.Status == StatusOK {
		t.Error("expected non-OK status for unresolvable host")
	}
}

// TestCheckerResultFields verifies that Name, Type, and Target are echoed from
// the component config without requiring a network call — uses an empty target
// so the checker returns immediately with StatusError before any I/O.
func TestCheckerResultFields(t *testing.T) {
	tests := []struct {
		name         string
		compType     string
		expectedType ComponentType
		check        func(ctx context.Context, c config.ComponentConfig) ComponentStatus
	}{
		{
			name:         "dns propagates Name and Type",
			compType:     "dns",
			expectedType: TypeDNS,
			check:        NewDNSChecker(1*time.Second, "").Check,
		},
		{
			name:         "ssl propagates Name and Type",
			compType:     "ssl",
			expectedType: TypeSSL,
			check:        NewSSLChecker(1 * time.Second).Check,
		},
		{
			name:         "http propagates Name and Type",
			compType:     "http",
			expectedType: TypeHTTP,
			check:        NewHTTPChecker(1 * time.Second).Check,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.check(context.Background(), config.ComponentConfig{
				Name: "my-check",
				Type: tt.compType,
				// empty target so the checker returns StatusError immediately
			})
			if result.Name != "my-check" {
				t.Errorf("Name: expected 'my-check', got %q", result.Name)
			}
			if result.Type != tt.expectedType {
				t.Errorf("Type: expected %q, got %q", tt.expectedType, result.Type)
			}
			if result.Status != StatusError {
				t.Errorf("Status: expected StatusError for empty target, got %s", result.Status)
			}
		})
	}
}

// ── NewSSLChecker ─────────────────────────────────────────────────────────────

func TestNewSSLChecker(t *testing.T) {
	c := NewSSLChecker(5 * time.Second)
	if c == nil {
		t.Fatal("NewSSLChecker() returned nil")
	}
	if c.timeout != 5*time.Second {
		t.Errorf("expected timeout 5s, got %v", c.timeout)
	}
}

func TestSSLChecker_EmptyTarget(t *testing.T) {
	c := NewSSLChecker(2 * time.Second)
	result := c.Check(context.Background(), config.ComponentConfig{
		Name: "no-ssl-target",
		Type: "ssl",
	})
	if result.Status != StatusError {
		t.Errorf("expected StatusError for empty target, got %s", result.Status)
	}
}

func TestSSLChecker_BadHost(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}
	c := NewSSLChecker(2 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := c.Check(ctx, config.ComponentConfig{
		Name:   "bad-ssl",
		Type:   "ssl",
		Target: "this.host.does.not.exist.invalid",
	})
	if result.Status == StatusOK {
		t.Error("expected non-OK for non-existent SSL host")
	}
}

// ── NewICMPChecker ────────────────────────────────────────────────────────────

func TestNewICMPChecker(t *testing.T) {
	c := NewICMPChecker(3*time.Second, 3)
	if c == nil {
		t.Fatal("NewICMPChecker() returned nil")
	}
	if c.timeout != 3*time.Second {
		t.Errorf("expected timeout 3s, got %v", c.timeout)
	}
	if c.count != 3 {
		t.Errorf("expected count 3, got %d", c.count)
	}
}

func TestICMPChecker_EmptyTarget(t *testing.T) {
	c := NewICMPChecker(2*time.Second, 1)
	result := c.Check(context.Background(), config.ComponentConfig{
		Name: "icmp-empty",
		Type: "icmp",
	})
	if result.Status != StatusError {
		t.Errorf("expected StatusError for empty target, got %s", result.Status)
	}
}
