package status

import (
	"context"
	"fmt"
	"time"

	"github.com/anthnel/devdesk/internal/config"
)

// SSLChecker checks SSL/TLS certificates
type SSLChecker struct {
	timeout time.Duration
}

// NewSSLChecker creates a new SSLChecker
func NewSSLChecker(timeout time.Duration) *SSLChecker {
	return &SSLChecker{
		timeout: timeout,
	}
}

// Check checks a host's SSL certificate
func (s *SSLChecker) Check(ctx context.Context, component config.ComponentConfig) ComponentStatus {
	result := ComponentStatus{
		Name:      component.Name,
		Type:      TypeSSL,
		Target:    component.Target,
		Timestamp: time.Now(),
	}

	if component.Target == "" {
		result.Status = StatusError
		result.Error = "target not configured"
		return result
	}

	// Measure connection time
	start := time.Now()
	certs, err := FetchCertificateChain(component.Target, s.timeout)
	result.ResponseTime = time.Since(start)

	if err != nil {
		result.Status = StatusDown
		result.Error = err.Error()
		return result
	}
	if len(certs) == 0 {
		result.Status = StatusError
		result.Error = "no certificates found"
		return result
	}

	// Use leaf certificate (first in chain)
	cert := certs[0]

	// Extract certificate data
	notAfter := cert.NotAfter
	issuer := cert.Issuer.CommonName
	if issuer == "" {
		// Fallback to Organization if CommonName is empty
		if len(cert.Issuer.Organization) > 0 {
			issuer = cert.Issuer.Organization[0]
		} else {
			issuer = "Unknown"
		}
	}
	daysLeft := int(time.Until(notAfter).Hours() / 24)

	// Populate SSL-specific fields
	result.SSLExpires = &notAfter
	result.SSLDaysLeft = &daysLeft
	result.SSLIssuer = issuer

	// Determine status based on days left
	if daysLeft < 0 {
		result.Status = StatusError
		result.Error = "certificate expired"
	} else if daysLeft < 7 {
		result.Status = StatusError
		result.Error = fmt.Sprintf("expires in %d days", daysLeft)
	} else if daysLeft <= CertRenewWindowDays {
		result.Status = StatusWarning
		result.Error = fmt.Sprintf("expires in %d days", daysLeft)
	} else {
		result.Status = StatusOK
	}

	return result
}
