package status

import "time"

// ComponentType represents the check type
type ComponentType string

const (
	TypeHTTP  ComponentType = "http"
	TypeHTTPS ComponentType = "https"
	TypeICMP  ComponentType = "icmp"
	TypeDNS   ComponentType = "dns"
	TypeSSL   ComponentType = "ssl"
)

// StatusType represents the possible states
type StatusType string

const (
	StatusOK      StatusType = "OK"
	StatusDown    StatusType = "DOWN"
	StatusError   StatusType = "ERROR"
	StatusWarning StatusType = "WARNING"
)

// ComponentStatus represents a component's state
type ComponentStatus struct {
	Name         string
	Type         ComponentType
	Target       string
	Status       StatusType
	ResponseTime time.Duration
	Error        string
	Timestamp    time.Time

	// SSL-specific fields (optional, only populated for SSL type)
	SSLDaysLeft *int       // Pointer to allow nil for non-SSL types
	SSLExpires  *time.Time // Certificate expiration date
	SSLIssuer   string     // Issuer CommonName
}

// String returns a textual representation of the status
func (s StatusType) String() string {
	return string(s)
}
