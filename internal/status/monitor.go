package status

import (
	"context"
	"time"

	"github.com/anthnel/devdesk/internal/config"
)

// MonitorResult contains the results of a background monitoring check
type MonitorResult struct {
	Components []ComponentStatus
	Timestamp  time.Time
}

// RunCheck performs a one-shot check of all components and returns the results.
// This is designed to be called from a Bubble Tea Cmd.
func RunCheck(cfg *config.Config) MonitorResult {
	if len(cfg.Status.Components) == 0 {
		return MonitorResult{
			Components: nil,
			Timestamp:  time.Now(),
		}
	}

	timeout := time.Duration(cfg.Status.Timeout) * time.Second
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	checker := NewChecker(timeout)
	ctx := context.Background()
	results := checker.CheckAll(ctx, cfg.Status.Components)

	return MonitorResult{
		Components: results,
		Timestamp:  time.Now(),
	}
}
