package status

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/anthnel/devdesk/internal/config"
)

// DNSChecker checks DNS resolution
type DNSChecker struct {
	timeout    time.Duration
	nameserver string
}

// NewDNSChecker creates a new DNS checker
func NewDNSChecker(timeout time.Duration, nameserver string) *DNSChecker {
	return &DNSChecker{
		timeout:    timeout,
		nameserver: nameserver,
	}
}

// Check checks the DNS resolution of a hostname
func (d *DNSChecker) Check(ctx context.Context, component config.ComponentConfig) ComponentStatus {
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

	// Create a resolver with a timeout
	resolver := &net.Resolver{}

	// If a specific nameserver is configured
	if d.nameserver != "" {
		resolver = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				dialer := net.Dialer{
					Timeout: d.timeout,
				}
				return dialer.DialContext(ctx, network, d.nameserver+":53")
			},
		}
	}

	// Create a context with a timeout
	ctxWithTimeout, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	// Measure the resolution time
	start := time.Now()
	addrs, err := resolver.LookupHost(ctxWithTimeout, component.Target)
	result.ResponseTime = time.Since(start)

	if err != nil {
		result.Status = StatusDown
		result.Error = err.Error()
		return result
	}

	if len(addrs) == 0 {
		result.Status = StatusError
		result.Error = "no addresses found"
		return result
	}

	result.Status = StatusOK
	// Optional: store the number of resolved addresses in Error as info
	if len(addrs) > 1 {
		result.Error = fmt.Sprintf("%d addresses", len(addrs))
	}

	return result
}
