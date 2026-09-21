package status

import (
	"context"
	"sync"
	"time"

	"github.com/anthnel/devdesk/internal/config"
)

// CheckerInterface defines the interface for every checker type
type CheckerInterface interface {
	Check(ctx context.Context, component config.ComponentConfig) ComponentStatus
}

// Checker orchestrates checking all the components
type Checker struct {
	timeout time.Duration
}

// NewChecker creates a new checker
func NewChecker(timeout time.Duration) *Checker {
	return &Checker{
		timeout: timeout,
	}
}

// CheckAll checks all the components in parallel
func (c *Checker) CheckAll(ctx context.Context, components []config.ComponentConfig) []ComponentStatus {
	results := make([]ComponentStatus, len(components))
	var wg sync.WaitGroup

	for i, comp := range components {
		wg.Add(1)
		go func(index int, component config.ComponentConfig) {
			defer wg.Done()
			results[index] = c.CheckOne(ctx, component)
		}(i, comp)
	}

	wg.Wait()
	return results
}

// CheckOne checks a component according to its type
func (c *Checker) CheckOne(ctx context.Context, component config.ComponentConfig) ComponentStatus {
	// Determine which checker type to use
	var checker CheckerInterface

	compType := EffectiveType(component)
	// Legacy support: a component with only a URL takes it as its target, and
	// the inferred type, so the checkers can use both.
	if component.Type == "" && component.URL != "" {
		if component.Target == "" {
			component.Target = component.URL
		}
		component.Type = compType
	}

	// Factory pattern
	switch compType {
	case "http", "https":
		timeout := time.Duration(component.Timeout) * time.Second
		if timeout == 0 {
			timeout = c.timeout
		}
		checker = NewHTTPChecker(timeout)

	case "icmp":
		timeout := time.Duration(component.Timeout) * time.Second
		if timeout == 0 {
			timeout = c.timeout
		}
		count := component.Count
		if count == 0 {
			count = 4
		}
		checker = NewICMPChecker(timeout, count)

	case "dns":
		timeout := time.Duration(component.Timeout) * time.Second
		if timeout == 0 {
			timeout = c.timeout
		}
		checker = NewDNSChecker(timeout, component.Nameserver)

	case "ssl":
		timeout := time.Duration(component.Timeout) * time.Second
		if timeout == 0 {
			timeout = c.timeout
		}
		checker = NewSSLChecker(timeout)

	default:
		// Unknown type
		return ComponentStatus{
			Name:      component.Name,
			Type:      ComponentType(compType),
			Target:    component.Target,
			Status:    StatusError,
			Error:     "unknown component type: " + compType,
			Timestamp: time.Now(),
		}
	}

	return checker.Check(ctx, component)
}

// EffectiveType is the type a component is checked as: its own, or for a legacy
// entry that carries only a URL, http or https by the URL's scheme. Empty when
// nothing can be inferred, which CheckOne reports as an unknown type.
//
// It is exported so a caller can select components by type before probing them,
// rather than probing everything and filtering the results.
func EffectiveType(component config.ComponentConfig) string {
	if component.Type != "" || component.URL == "" {
		return component.Type
	}
	switch {
	case len(component.URL) > 8 && component.URL[:8] == "https://":
		return "https"
	case len(component.URL) > 7 && component.URL[:7] == "http://":
		return "http"
	}
	return ""
}
