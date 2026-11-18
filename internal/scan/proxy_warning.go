package scan

import (
	"fmt"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/netproxy"
)

// RunsInContainer reports whether one of ids would run from an image: from the
// detection when it holds the tool, else from the configured source, where only
// "image" is a container for certain (auto may find a binary). A tool that
// cannot run runs nowhere, so it never counts.
func RunsInContainer(r *Report, tools config.ScanTools, ids []ToolID) bool {
	for _, id := range ids {
		if st, ok := r.status(id); ok {
			if st.Available && st.Source == ToolSourceContainer {
				return true
			}
			continue
		}
		if cfg := tools.Tool(string(id)); cfg != nil && cfg.Source == config.ToolSourceImage {
			return true
		}
	}
	return false
}

// status is the detection of one tool, and whether the report holds it. It is
// safe on a nil report.
func (r *Report) status(id ToolID) (ToolStatus, bool) {
	if r == nil {
		return ToolStatus{}, false
	}
	st, ok := r.Tools[id]
	return st, ok
}

// ContainerProxyWarning is the footer text when fwd names a loopback proxy and a
// containerised tool is about to run; "" otherwise. A scanner container has its
// own loopback, so a proxy on this machine's cannot be reached from it. The text
// carries the variable as it was read and the display form of its value, never
// the raw value, so no credential reaches the footer.
func ContainerProxyWarning(fwd netproxy.Forward, containerised bool) string {
	if !containerised {
		return ""
	}
	variable, display, ok := fwd.Loopback()
	if !ok {
		return ""
	}
	return fmt.Sprintf("%s is %s, on this machine's loopback: scanner containers cannot reach it", variable, display)
}
