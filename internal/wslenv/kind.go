package wslenv

import "strings"

// Kind says which WSL generation, if any, this machine is.
type Kind int

const (
	// KindNone is anything that is not WSL: native Linux, macOS, Windows.
	KindNone Kind = iota
	// KindWSL1 is WSL1, where Linux syscalls are translated by the NT kernel.
	KindWSL1
	// KindWSL2 is WSL2, a real Linux kernel in a lightweight VM.
	KindWSL2
)

// ClassifyKind reads only DistroName and OSRelease. WSL1 reports a kernel
// release with a capital "Microsoft"; WSL2 uses a lower-case "microsoft". The
// WSL1 check is case-sensitive and must run before isWSL, which lower-cases.
func ClassifyKind(f Facts) Kind {
	switch {
	case !isWSL(f):
		return KindNone
	case strings.Contains(f.OSRelease, "Microsoft"):
		return KindWSL1
	default:
		return KindWSL2
	}
}

// GatherKernel collects only the facts ClassifyKind needs: it reads the
// WSL_DISTRO_NAME variable and the kernel release, and nothing else. Unlike
// Gather it runs no process and lists no interface. It reads nothing off Linux.
func GatherKernel(p Probe) Facts {
	if p.GOOS != "linux" {
		return Facts{}
	}
	f := Facts{DistroName: p.Getenv("WSL_DISTRO_NAME")}
	if b, err := p.ReadFile(osReleasePath); err == nil {
		f.OSRelease = string(b)
	}
	return f
}

// DetectKind is ClassifyKind(GatherKernel(SystemProbe())).
func DetectKind() Kind { return ClassifyKind(GatherKernel(SystemProbe())) }
