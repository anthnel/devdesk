package wslenv

import (
	"slices"
	"strings"
)

// mirroredInterface exists in the distro only under mirrored networking.
const mirroredInterface = "loopback0"

// Classify turns Facts into Info. A zero Info means "not WSL2".
func Classify(f Facts) Info {
	if !isWSL(f) || !strings.Contains(strings.ToUpper(f.OSRelease), "WSL2") {
		return Info{}
	}
	info := Info{WSL2: true}
	if f.NetworkingModeErr != nil {
		return info
	}
	hasLoopback0 := slices.Contains(f.Interfaces, mirroredInterface)
	switch strings.ToLower(strings.TrimSpace(f.NetworkingMode)) {
	case "nat":
		if hasLoopback0 {
			return info
		}
		info.Mode = ModeNAT
		if iface, gw, ok := defaultGateway(f.ProcNetRoute); ok {
			info.NATInterface = iface
			info.NATGateway = gw
		}
	case "mirrored":
		if !hasLoopback0 {
			return info
		}
		info.Mode = ModeMirrored
	}
	return info
}

// isWSL reports whether the facts describe a WSL distro (1 or 2). Either signal
// suffices: WSL_DISTRO_NAME is empty under `env -i` or from a service, and
// osrelease alone then identifies WSL.
func isWSL(f Facts) bool {
	return f.DistroName != "" || strings.Contains(strings.ToLower(f.OSRelease), "microsoft")
}
