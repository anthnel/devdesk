package credentials

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// HelperStorage utilise le git credential helper système
type HelperStorage struct {
	helper  string // osxkeychain, libsecret, wincred, etc.
	context string // contexte DevDesk (default, dev, prod, etc.)
}

// NewHelperStorage crée un nouveau HelperStorage
func NewHelperStorage(helper string) *HelperStorage {
	if helper == "" {
		helper = detectHelper()
	}
	return &HelperStorage{
		helper:  helper,
		context: "default", // Par défaut
	}
}

// NewHelperStorageWithContext crée un HelperStorage avec un contexte spécifique
func NewHelperStorageWithContext(helper, context string) *HelperStorage {
	if helper == "" {
		helper = detectHelper()
	}
	if context == "" {
		context = "default"
	}
	return &HelperStorage{
		helper:  helper,
		context: context,
	}
}

// Save sauvegarde un token via git credential helper
// Utilise le champ path pour différencier par contexte
func (h *HelperStorage) Save(url, token string) error {
	cmd := exec.Command("git", "credential-"+h.helper, "store")

	// Ajouter le contexte dans le path pour différencier les credentials
	// Format: protocol=https\nhost=gitlab.com\npath=devdesk/context/default\n...
	input := fmt.Sprintf("protocol=https\nhost=%s\npath=devdesk/context/%s\nusername=oauth2\npassword=%s\n\n",
		extractHost(url), h.context, token)
	cmd.Stdin = strings.NewReader(input)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("credential helper error: %v - %s", err, stderr.String())
	}

	return nil
}

// Load charge un token via git credential helper
// Utilise le champ path pour charger les credentials du bon contexte
func (h *HelperStorage) Load(url string) (string, error) {
	cmd := exec.Command("git", "credential-"+h.helper, "get")

	// Inclure le contexte dans le path pour charger les bons credentials
	input := fmt.Sprintf("protocol=https\nhost=%s\npath=devdesk/context/%s\n\n",
		extractHost(url), h.context)
	cmd.Stdin = strings.NewReader(input)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("credential helper error: %v - %s", err, stderr.String())
	}

	// Parser la sortie pour extraire le password
	lines := strings.Split(stdout.String(), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "password=") {
			return strings.TrimPrefix(line, "password="), nil
		}
	}

	return "", fmt.Errorf("no credentials found for %s (context: %s)", url, h.context)
}

// Delete supprime un token via git credential helper
// Utilise le champ path pour supprimer les credentials du bon contexte
func (h *HelperStorage) Delete(url string) error {
	cmd := exec.Command("git", "credential-"+h.helper, "erase")

	// Inclure le contexte dans le path pour supprimer les bons credentials
	input := fmt.Sprintf("protocol=https\nhost=%s\npath=devdesk/context/%s\n\n",
		extractHost(url), h.context)
	cmd.Stdin = strings.NewReader(input)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("credential helper error: %v - %s", err, stderr.String())
	}

	return nil
}

// detectHelper détecte le git credential helper disponible
func detectHelper() string {
	// Essayer de détecter via git config
	cmd := exec.Command("git", "config", "--global", "credential.helper")
	output, err := cmd.Output()
	if err == nil && len(output) > 0 {
		helper := strings.TrimSpace(string(output))
		// Nettoyer (peut être "osxkeychain" ou "!osxkeychain", etc.)
		helper = strings.TrimPrefix(helper, "!")
		return helper
	}

	// Par défaut selon l'OS (simplifié)
	// En production, on devrait détecter l'OS
	return "store" // Fallback vers git-credential-store
}

// extractHost extrait le host d'une URL
func extractHost(url string) string {
	// Enlever le protocole
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")

	// Enlever le chemin
	parts := strings.Split(url, "/")
	return parts[0]
}
