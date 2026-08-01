package status

import (
	"context"
	"testing"
	"time"

	"gitlab.com/anthnell/devsecops/devdesk/internal/config"
)

func TestNewChecker(t *testing.T) {
	timeout := 5 * time.Second
	checker := NewChecker(timeout)

	if checker == nil {
		t.Fatal("NewChecker() returned nil")
	}
	if checker.timeout != timeout {
		t.Errorf("Expected timeout %v, got %v", timeout, checker.timeout)
	}
}

func TestCheckerUnknownType(t *testing.T) {
	checker := NewChecker(5 * time.Second)
	ctx := context.Background()

	component := config.ComponentConfig{
		Name:   "unknown-test",
		Type:   "unknown-type",
		Target: "example.com",
	}

	result := checker.CheckOne(ctx, component)

	if result.Status != StatusError {
		t.Errorf("Expected status ERROR for unknown type, got %s", result.Status)
	}
	if result.Error == "" {
		t.Error("Expected error message for unknown type")
	}
	if result.Name != "unknown-test" {
		t.Errorf("Expected name 'unknown-test', got '%s'", result.Name)
	}
}

func TestCheckerLegacyURLSupport(t *testing.T) {
	checker := NewChecker(5 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tests := []struct {
		name         string
		component    config.ComponentConfig
		expectedType string
	}{
		{
			name: "HTTPS URL legacy",
			component: config.ComponentConfig{
				Name: "legacy-https",
				URL:  "https://example.com",
			},
			expectedType: "https",
		},
		{
			name: "HTTP URL legacy",
			component: config.ComponentConfig{
				Name: "legacy-http",
				URL:  "http://example.com",
			},
			expectedType: "http",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := checker.CheckOne(ctx, tt.component)
			if string(result.Type) != tt.expectedType {
				t.Errorf("Expected type '%s', got '%s'", tt.expectedType, result.Type)
			}
		})
	}
}

func TestCheckAll(t *testing.T) {
	checker := NewChecker(5 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	components := []config.ComponentConfig{
		{
			Name:   "test-http-1",
			Type:   "http",
			Target: "http://example.com",
		},
		{
			Name:   "test-http-2",
			Type:   "https",
			Target: "https://example.com",
		},
		{
			Name:   "test-dns",
			Type:   "dns",
			Target: "example.com",
		},
	}

	results := checker.CheckAll(ctx, components)

	// Verify we got results for all components
	if len(results) != len(components) {
		t.Errorf("Expected %d results, got %d", len(components), len(results))
	}

	// Verify each result has required fields
	for i, result := range results {
		if result.Name == "" {
			t.Errorf("Result %d has empty name", i)
		}
		if result.Target == "" {
			t.Errorf("Result %d has empty target", i)
		}
		if result.Status == "" {
			t.Errorf("Result %d has empty status", i)
		}
		if result.Timestamp.IsZero() {
			t.Errorf("Result %d has zero timestamp", i)
		}
	}
}

func TestCheckAllEmptyComponents(t *testing.T) {
	checker := NewChecker(5 * time.Second)
	ctx := context.Background()

	results := checker.CheckAll(ctx, []config.ComponentConfig{})

	if len(results) != 0 {
		t.Errorf("Expected 0 results for empty components, got %d", len(results))
	}
}

func TestCheckOneHTTP(t *testing.T) {
	checker := NewChecker(5 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	component := config.ComponentConfig{
		Name:    "http-test",
		Type:    "http",
		Target:  "http://example.com",
		Timeout: 5,
	}

	result := checker.CheckOne(ctx, component)

	// Verify basic fields
	if result.Name != "http-test" {
		t.Errorf("Expected name 'http-test', got '%s'", result.Name)
	}
	if result.Type != TypeHTTP {
		t.Errorf("Expected type HTTP, got %s", result.Type)
	}
	if result.Target != "http://example.com" {
		t.Errorf("Expected target 'http://example.com', got '%s'", result.Target)
	}

	// Should have a status (OK or DOWN depending on network)
	if result.Status != StatusOK && result.Status != StatusDown {
		t.Errorf("Expected status OK or DOWN, got %s", result.Status)
	}

	// Should have a timestamp
	if result.Timestamp.IsZero() {
		t.Error("Expected non-zero timestamp")
	}
}

func TestCheckOneHTTPS(t *testing.T) {
	checker := NewChecker(5 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	component := config.ComponentConfig{
		Name:    "https-test",
		Type:    "https",
		Target:  "https://example.com",
		Timeout: 5,
	}

	result := checker.CheckOne(ctx, component)

	if result.Name != "https-test" {
		t.Errorf("Expected name 'https-test', got '%s'", result.Name)
	}
	if result.Type != TypeHTTPS {
		t.Errorf("Expected type HTTPS, got %s", result.Type)
	}
	if result.Status != StatusOK && result.Status != StatusDown {
		t.Errorf("Expected status OK or DOWN, got %s", result.Status)
	}
}

func TestCheckOneDNS(t *testing.T) {
	checker := NewChecker(5 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	component := config.ComponentConfig{
		Name:    "dns-test",
		Type:    "dns",
		Target:  "example.com",
		Timeout: 5,
	}

	result := checker.CheckOne(ctx, component)

	if result.Name != "dns-test" {
		t.Errorf("Expected name 'dns-test', got '%s'", result.Name)
	}
	if result.Type != TypeDNS {
		t.Errorf("Expected type DNS, got %s", result.Type)
	}
	if result.Status != StatusOK && result.Status != StatusDown {
		t.Errorf("Expected status OK or DOWN, got %s", result.Status)
	}
}

func TestCheckOneSSL(t *testing.T) {
	checker := NewChecker(5 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	component := config.ComponentConfig{
		Name:    "ssl-test",
		Type:    "ssl",
		Target:  "example.com",
		Timeout: 5,
	}

	result := checker.CheckOne(ctx, component)

	if result.Name != "ssl-test" {
		t.Errorf("Expected name 'ssl-test', got '%s'", result.Name)
	}
	if result.Type != TypeSSL {
		t.Errorf("Expected type SSL, got %s", result.Type)
	}

	// SSL check should populate SSL-specific fields if successful
	if result.Status == StatusOK {
		if result.SSLDaysLeft == nil {
			t.Error("Expected SSLDaysLeft to be populated for successful SSL check")
		}
		if result.SSLExpires == nil {
			t.Error("Expected SSLExpires to be populated for successful SSL check")
		}
	}
}

func TestCheckOneWithContextCancellation(t *testing.T) {
	checker := NewChecker(30 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	component := config.ComponentConfig{
		Name:    "timeout-test",
		Type:    "https",
		Target:  "https://httpbin.org/delay/10", // Slow endpoint
		Timeout: 30,
	}

	result := checker.CheckOne(ctx, component)

	// Should fail due to context timeout
	if result.Status == StatusOK {
		t.Error("Expected check to fail due to context timeout")
	}
}

func TestCheckAllParallelExecution(t *testing.T) {
	checker := NewChecker(5 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Create multiple components
	components := make([]config.ComponentConfig, 5)
	for i := 0; i < 5; i++ {
		components[i] = config.ComponentConfig{
			Name:   "test-" + string(rune('0'+i)),
			Type:   "https",
			Target: "https://example.com",
		}
	}

	start := time.Now()
	results := checker.CheckAll(ctx, components)
	elapsed := time.Since(start)

	// Parallel execution should take roughly the same time as a single check
	// Not 5x the time (which would indicate serial execution)
	if elapsed > 12*time.Second {
		t.Errorf("CheckAll took too long (%v), might not be running in parallel", elapsed)
	}

	if len(results) != 5 {
		t.Errorf("Expected 5 results, got %d", len(results))
	}
}

func TestStatusTypeString(t *testing.T) {
	tests := []struct {
		status   StatusType
		expected string
	}{
		{StatusOK, "OK"},
		{StatusDown, "DOWN"},
		{StatusError, "ERROR"},
		{StatusWarning, "WARNING"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.status.String()
			if result != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}
