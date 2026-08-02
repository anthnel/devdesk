package status

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/anthnel/devdesk/internal/config"
)

// DNSChecker vérifie la résolution DNS
type DNSChecker struct {
	timeout    time.Duration
	nameserver string
}

// NewDNSChecker crée un nouveau checker DNS
func NewDNSChecker(timeout time.Duration, nameserver string) *DNSChecker {
	return &DNSChecker{
		timeout:    timeout,
		nameserver: nameserver,
	}
}

// Check vérifie la résolution DNS d'un hostname
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

	// Créer un resolver avec timeout
	resolver := &net.Resolver{}

	// Si un nameserver spécifique est configuré
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

	// Créer un contexte avec timeout
	ctxWithTimeout, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	// Mesurer le temps de résolution
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
	// Optionnel: stocker le nombre d'adresses résolues dans Error comme info
	if len(addrs) > 1 {
		result.Error = fmt.Sprintf("%d addresses", len(addrs))
	}

	return result
}
