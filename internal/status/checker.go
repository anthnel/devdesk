package status

import (
	"context"
	"sync"
	"time"

	"gitlab.com/anthnell/devsecops/devdesk/internal/config"
)

// CheckerInterface définit l'interface pour tous les types de checkers
type CheckerInterface interface {
	Check(ctx context.Context, component config.ComponentConfig) ComponentStatus
}

// Checker orchestre la vérification de tous les composants
type Checker struct {
	timeout time.Duration
}

// NewChecker crée un nouveau checker
func NewChecker(timeout time.Duration) *Checker {
	return &Checker{
		timeout: timeout,
	}
}

// CheckAll vérifie tous les composants en parallèle
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

// CheckOne vérifie un composant selon son type
func (c *Checker) CheckOne(ctx context.Context, component config.ComponentConfig) ComponentStatus {
	// Déterminer le type de checker à utiliser
	var checker CheckerInterface

	compType := component.Type
	// Legacy support: si URL est défini mais pas Type, déduire le type
	if compType == "" && component.URL != "" {
		if len(component.URL) > 8 && component.URL[:8] == "https://" {
			compType = "https"
		} else if len(component.URL) > 7 && component.URL[:7] == "http://" {
			compType = "http"
		}
		// Utiliser Target = URL pour legacy
		if component.Target == "" {
			component.Target = component.URL
		}
		// Mettre à jour le type pour que les checkers puissent l'utiliser
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
		// Type inconnu
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
