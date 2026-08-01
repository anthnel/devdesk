package credentials

import (
	"bytes"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

// GitCredentialStorage stocke les credentials via git credential manager
type GitCredentialStorage struct {
	context string // Contexte DevDesk pour isoler les credentials
}

// NewGitCredentialStorageWithContext crée un GitCredentialStorage context-aware
// Chaque contexte aura ses propres credentials isolés
func NewGitCredentialStorageWithContext(context string) *GitCredentialStorage {
	if context == "" {
		context = "default"
	}
	return &GitCredentialStorage{
		context: context,
	}
}

// Save sauvegarde un token via git credential
// Utilise le champ path pour différencier par contexte
func (g *GitCredentialStorage) Save(urlStr, token string) error {
	// Parser l'URL pour extraire host
	u, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Préparer l'input pour git credential avec le contexte dans le path
	// Format: protocol=https\nhost=gitlab.com\npath=devdesk/context/default\n...
	input := fmt.Sprintf("protocol=%s\nhost=%s\npath=devdesk/context/%s\nusername=oauth2\npassword=%s\n",
		u.Scheme, u.Host, g.context, token)

	// Appeler git credential approve avec timeout pour éviter de bloquer l'UI
	// Si Git Credential Manager n'est pas configuré, le timeout évite que l'application se fige
	cmd := exec.Command("git", "credential", "approve")
	cmd.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// IMPORTANT: Désactiver l'interaction avec le terminal
	// Sans cela, git credential peut afficher des prompts interactifs
	cmd.Env = append(cmd.Environ(),
		"GIT_TERMINAL_PROMPT=0", // Désactive les prompts interactifs
		"GCM_INTERACTIVE=never", // Git Credential Manager en mode non-interactif
	)

	// Utiliser un canal pour détecter la fin de la commande
	done := make(chan error, 1)
	go func() {
		done <- cmd.Run()
	}()

	// Attendre maximum 2 secondes
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("git credential approve failed: %v: %s", err, stderr.String())
		}
	case <-time.After(2 * time.Second):
		// Timeout - tuer le processus et tous ses enfants
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return fmt.Errorf("git credential approve timed out (not configured or waiting for input)")
	}

	return nil
}

// Load charge un token depuis git credential
// Utilise le champ path pour charger les credentials du bon contexte
func (g *GitCredentialStorage) Load(urlStr string) (string, error) {
	// Parser l'URL pour extraire host
	u, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}

	// Préparer l'input pour git credential avec le contexte dans le path
	input := fmt.Sprintf("protocol=%s\nhost=%s\npath=devdesk/context/%s\n",
		u.Scheme, u.Host, g.context)

	// Appeler git credential fill avec timeout pour éviter de bloquer l'UI
	// Si Git Credential Manager n'est pas configuré ou demande une interaction,
	// le timeout évite que l'application se fige
	cmd := exec.Command("git", "credential", "fill")
	cmd.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// IMPORTANT: Désactiver l'interaction avec le terminal
	// Sans cela, git credential peut afficher des prompts interactifs
	cmd.Env = append(cmd.Environ(),
		"GIT_TERMINAL_PROMPT=0", // Désactive les prompts interactifs
		"GCM_INTERACTIVE=never", // Git Credential Manager en mode non-interactif
	)

	// Utiliser un canal pour détecter la fin de la commande
	done := make(chan error, 1)
	go func() {
		done <- cmd.Run()
	}()

	// Attendre maximum 2 secondes
	select {
	case err := <-done:
		if err != nil {
			return "", fmt.Errorf("git credential fill failed: %v: %s", err, stderr.String())
		}
	case <-time.After(2 * time.Second):
		// Timeout - tuer le processus et tous ses enfants
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return "", fmt.Errorf("git credential fill timed out (not configured or waiting for input)")
	}

	// Parser la sortie pour extraire le password
	output := stdout.String()
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "password=") {
			return strings.TrimPrefix(line, "password="), nil
		}
	}

	return "", fmt.Errorf("no credentials found for %s (context: %s)", urlStr, g.context)
}

// Delete supprime un token de git credential
// Utilise le champ path pour supprimer les credentials du bon contexte
func (g *GitCredentialStorage) Delete(urlStr string) error {
	// Parser l'URL pour extraire host
	u, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Préparer l'input pour git credential avec le contexte dans le path
	input := fmt.Sprintf("protocol=%s\nhost=%s\npath=devdesk/context/%s\nusername=oauth2\n",
		u.Scheme, u.Host, g.context)

	// Appeler git credential reject
	cmd := exec.Command("git", "credential", "reject")
	cmd.Stdin = strings.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git credential reject failed: %v: %s", err, stderr.String())
	}

	return nil
}
