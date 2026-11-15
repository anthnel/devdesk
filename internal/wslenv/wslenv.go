// Package wslenv detects WSL2 and its networking mode from plain facts.
package wslenv

import "net/netip"

// Mode is the WSL2 networking mode.
type Mode int

const (
	// ModeUndetermined is the zero value: the mode could not be established.
	ModeUndetermined Mode = iota
	// ModeNAT is the default WSL2 mode (virtual switch).
	ModeNAT
	// ModeMirrored mirrors the Windows interfaces into the distro.
	ModeMirrored
)

// Facts are the raw observations Classify decides from.
type Facts struct {
	DistroName        string
	OSRelease         string
	NetworkingMode    string
	NetworkingModeErr error
	Interfaces        []string
	ProcNetRoute      []byte
}

// Info is the classification of Facts.
type Info struct {
	WSL2         bool
	Mode         Mode
	NATInterface string
	// NATGateway is the zero Addr when unknown (mode not NAT, or no usable
	// default route): consumers must test NATGateway.IsValid().
	NATGateway netip.Addr
}
