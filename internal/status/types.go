package status

import "time"

// ComponentType représente le type de check
type ComponentType string

const (
	TypeHTTP  ComponentType = "http"
	TypeHTTPS ComponentType = "https"
	TypeICMP  ComponentType = "icmp"
	TypeDNS   ComponentType = "dns"
	TypeSSL   ComponentType = "ssl"
)

// StatusType représente les états possibles
type StatusType string

const (
	StatusOK      StatusType = "OK"
	StatusDown    StatusType = "DOWN"
	StatusError   StatusType = "ERROR"
	StatusWarning StatusType = "WARNING"
)

// ComponentStatus représente l'état d'un composant
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

// String retourne une représentation textuelle du status
func (s StatusType) String() string {
	return string(s)
}
