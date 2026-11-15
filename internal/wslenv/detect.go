package wslenv

import (
	"context"
	"net"
	"os"
	"runtime"
)

const (
	osReleasePath = "/proc/sys/kernel/osrelease"
	procRoutePath = "/proc/net/route"
)

// Probe is the set of system reads Gather performs. Tests replace every field.
type Probe struct {
	GOOS           string
	Getenv         func(string) string
	ReadFile       func(string) ([]byte, error)
	InterfaceNames func() ([]string, error)
	NetworkingMode func(ctx context.Context) (string, error)
}

// SystemProbe returns the Probe backed by the real system.
func SystemProbe() Probe {
	return Probe{
		GOOS:           runtime.GOOS,
		Getenv:         os.Getenv,
		ReadFile:       os.ReadFile,
		InterfaceNames: interfaceNames,
		NetworkingMode: wslinfoNetworkingMode,
	}
}

// interfaceNames lists the names of this machine's network interfaces.
func interfaceNames() ([]string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(ifaces))
	for _, i := range ifaces {
		names = append(names, i.Name)
	}
	return names, nil
}

// Gather collects Facts through p. It reads nothing off Linux, and reads the
// WSL-only facts (interfaces, routes, networking mode) only under WSL. A read
// error leaves the matching fact empty.
func Gather(ctx context.Context, p Probe) Facts {
	if p.GOOS != "linux" {
		return Facts{}
	}
	f := Facts{DistroName: p.Getenv("WSL_DISTRO_NAME")}
	if b, err := p.ReadFile(osReleasePath); err == nil {
		f.OSRelease = string(b)
	}
	if !isWSL(f) {
		return f
	}
	if names, err := p.InterfaceNames(); err == nil {
		f.Interfaces = names
	}
	if b, err := p.ReadFile(procRoutePath); err == nil {
		f.ProcNetRoute = b
	}
	// A probe that ignores ctx must still report a cancelled context.
	if err := ctx.Err(); err != nil {
		f.NetworkingModeErr = err
		return f
	}
	f.NetworkingMode, f.NetworkingModeErr = p.NetworkingMode(ctx)
	return f
}

// Detect is Classify(Gather(ctx, SystemProbe())).
func Detect(ctx context.Context) Info { return Classify(Gather(ctx, SystemProbe())) }
