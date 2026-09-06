package status

import (
	"context"
	"fmt"
	"time"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/prometheus-community/pro-bing"
)

// ICMPChecker checks connectivity via ICMP (ping)
type ICMPChecker struct {
	timeout time.Duration
	count   int
}

// NewICMPChecker creates a new ICMP checker
func NewICMPChecker(timeout time.Duration, count int) *ICMPChecker {
	return &ICMPChecker{
		timeout: timeout,
		count:   count,
	}
}

// Check checks a host via ICMP ping
func (i *ICMPChecker) Check(ctx context.Context, component config.ComponentConfig) ComponentStatus {
	result := ComponentStatus{
		Name:      component.Name,
		Type:      ComponentType(component.Type),
		Target:    component.Target,
		Timestamp: time.Now(),
	}

	if component.Target == "" {
		result.Status = StatusError
		result.Error = "target not configured"
		return result
	}

	// Create the pinger
	pinger, err := probing.NewPinger(component.Target)
	if err != nil {
		result.Status = StatusError
		result.Error = err.Error()
		return result
	}

	// Configuration
	pinger.Count = i.count
	pinger.Timeout = i.timeout
	pinger.SetPrivileged(false) // Unprivileged mode (no sudo needed)

	// Run the ping
	start := time.Now()
	err = pinger.Run()
	elapsed := time.Since(start)

	if err != nil {
		result.Status = StatusDown
		result.Error = err.Error()
		return result
	}

	// Retrieve the statistics
	stats := pinger.Statistics()

	// Check for packet loss
	if stats.PacketsRecv == 0 {
		result.Status = StatusDown
		result.Error = fmt.Sprintf("100%% packet loss (%d/%d)", stats.PacketsRecv, stats.PacketsSent)
		result.ResponseTime = elapsed
		return result
	}

	// Compute the average RTT
	result.ResponseTime = stats.AvgRtt
	result.Status = StatusOK

	// If there is partial packet loss, note it as a warning
	if stats.PacketLoss > 0 {
		result.Error = fmt.Sprintf("%.0f%% packet loss", stats.PacketLoss)
	}

	return result
}
