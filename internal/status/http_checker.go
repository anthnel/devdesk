package status

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/anthnel/devdesk/internal/config"
)

// HTTPChecker checks HTTP/HTTPS endpoints
type HTTPChecker struct {
	client  *http.Client
	timeout time.Duration
}

// NewHTTPChecker creates a new HTTP/HTTPS checker
func NewHTTPChecker(timeout time.Duration) *HTTPChecker {
	return &HTTPChecker{
		client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
		timeout: timeout,
	}
}

// Check checks an HTTP/HTTPS endpoint
func (h *HTTPChecker) Check(ctx context.Context, component config.ComponentConfig) ComponentStatus {
	result := ComponentStatus{
		Name:      component.Name,
		Type:      ComponentType(component.Type),
		Target:    component.Target,
		Timestamp: time.Now(),
	}

	if component.Target == "" {
		result.Status = StatusError
		result.Error = "target not configured"
		return result
	}

	// Build the full URL with the protocol based on the type
	url := buildURL(component.Type, component.Target)

	// Create the request
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		result.Status = StatusError
		result.Error = err.Error()
		return result
	}

	req.Header.Set("User-Agent", "dso-tui/1.0")

	// Measure the response time
	start := time.Now()
	resp, err := h.client.Do(req)
	result.ResponseTime = time.Since(start)

	if err != nil {
		result.Status = StatusDown
		result.Error = err.Error()
		return result
	}
	defer func() { _ = resp.Body.Close() }()

	// Classify based on the HTTP code
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		result.Status = StatusOK
	} else {
		result.Status = StatusError
		result.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	return result
}

// buildURL builds the full URL with the protocol based on the type
// If the target already contains a protocol, it is returned as-is
func buildURL(compType, target string) string {
	// If the URL already starts with http:// or https://, return it as-is
	if len(target) >= 8 && target[:8] == "https://" {
		return target
	}
	if len(target) >= 7 && target[:7] == "http://" {
		return target
	}

	// Otherwise, add the protocol based on the type
	switch compType {
	case "http":
		return "http://" + target
	case "https":
		return "https://" + target
	default:
		// Default to https
		return "https://" + target
	}
}
