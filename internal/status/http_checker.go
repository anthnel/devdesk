package status

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"gitlab.com/anthnell/devsecops/devdesk/internal/config"
)

// HTTPChecker vérifie les endpoints HTTP/HTTPS
type HTTPChecker struct {
	client  *http.Client
	timeout time.Duration
}

// NewHTTPChecker crée un nouveau checker HTTP/HTTPS
func NewHTTPChecker(timeout time.Duration) *HTTPChecker {
	return &HTTPChecker{
		client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
		timeout: timeout,
	}
}

// Check vérifie un endpoint HTTP/HTTPS
func (h *HTTPChecker) Check(ctx context.Context, component config.ComponentConfig) ComponentStatus {
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

	// Construire l'URL complète avec le protocole selon le type
	url := buildURL(component.Type, component.Target)

	// Créer la requête
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		result.Status = StatusError
		result.Error = err.Error()
		return result
	}

	req.Header.Set("User-Agent", "dso-tui/1.0")

	// Mesurer le temps de réponse
	start := time.Now()
	resp, err := h.client.Do(req)
	result.ResponseTime = time.Since(start)

	if err != nil {
		result.Status = StatusDown
		result.Error = err.Error()
		return result
	}
	defer func() { _ = resp.Body.Close() }()

	// Classifier selon le code HTTP
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		result.Status = StatusOK
	} else {
		result.Status = StatusError
		result.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	return result
}

// buildURL construit l'URL complète avec le protocole selon le type
// Si le target contient déjà un protocole, il est retourné tel quel
func buildURL(compType, target string) string {
	// Si l'URL commence déjà par http:// ou https://, la retourner telle quelle
	if len(target) >= 8 && target[:8] == "https://" {
		return target
	}
	if len(target) >= 7 && target[:7] == "http://" {
		return target
	}

	// Sinon, ajouter le protocole selon le type
	switch compType {
	case "http":
		return "http://" + target
	case "https":
		return "https://" + target
	default:
		// Par défaut, utiliser https
		return "https://" + target
	}
}
