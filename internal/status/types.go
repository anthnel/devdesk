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

// ErrorStage says where a failed check broke, when the check can tell. The
// zero value means unspecified: the failure is the target's own (an unreachable
// host, a certificate error), as it was before checks could cross a proxy.
type ErrorStage string

const (
	// StageProxy marks a failure of the proxy step: the proxy could not be
	// reached, refused CONNECT, did not answer, or is not supported.
	StageProxy ErrorStage = "proxy"
)

// ComponentStatus represents a component's state
type ComponentStatus struct {
	Name         string
	Type         ComponentType
	Target       string
	Status       StatusType
	ResponseTime time.Duration
	Error        string
	ErrorStage   ErrorStage // empty = unspecified
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
