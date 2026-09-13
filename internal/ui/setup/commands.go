package setup

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/credentials"
	"github.com/anthnel/devdesk/internal/forge/session"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ContextExistsMsg reports whether the named context already has a file on
// disk.
type ContextExistsMsg struct {
	Exists bool
	Err    error
}

func checkContextExistsCmd(name string) tea.Cmd {
	return func() tea.Msg {
		exists, err := config.ContextExists(name)
		return ContextExistsMsg{Exists: exists, Err: err}
	}
}

// TokenValidatedMsg reports the outcome of authenticating the forge token the
// user typed. A nil Err means the token is both valid and saved — Authenticate
// persists it via the resolved Storage as part of the same call.
type TokenValidatedMsg struct {
	Err error
}

func validateTokenCmd(contextName, secretBackend, forgeType, url, token string) tea.Cmd {
	return func() tea.Msg {
		sel := credentials.Select(contextName, secretBackend)
		_, err := session.NewAuth(sel.Storage).Authenticate(context.Background(), forgeType, url, token)
		return TokenValidatedMsg{Err: err}
	}
}

// ContextSavedMsg reports whether the context file was written.
type ContextSavedMsg struct {
	Err error
}

func saveContextCmd(cfg *config.Config, name string) tea.Cmd {
	return func() tea.Msg {
		return ContextSavedMsg{Err: config.SaveContext(cfg, name)}
	}
}

// knownRemoteThemes are the JSON files bundled at the repository root
// (themes/*.json) — not installed anywhere by default; README.md documents
// copying one into ~/.devdesk/themes/ by hand. This is the fixed list the
// wizard offers to fetch automatically instead.
var knownRemoteThemes = []string{
	"catppuccin-mocha", "catppuccin-latte", "catppuccin-frappe", "catppuccin-macchiato",
	"tokyo-night", "one-dark", "dark-plus", "nord",
	"solarized-dark", "solarized-light", "github-dark", "github-light",
}

// remoteThemeBaseURL is a var, not a const, so a test can point it at an
// httptest.Server instead of the real repository.
var remoteThemeBaseURL = "https://raw.githubusercontent.com/anthnel/devdesk/main/themes/"

// remoteThemeFetchTimeout bounds each request. There is no separate
// connectivity check (Rule 110 doesn't forbid it, but there is nothing a
// preliminary probe would tell us that attempting the real fetch does not) —
// an unreachable host fails this fast regardless.
var remoteThemeFetchTimeout = 3 * time.Second

// RemoteThemesFetchedMsg reports the outcome of the one-shot, best-effort
// theme download. Unreachable is set only when the network attempt itself
// failed — never for an individual file's own error, which is not this
// project's fault to explain to the user.
type RemoteThemesFetchedMsg struct {
	Added       []string
	Unreachable bool
}

func fetchRemoteThemesCmd() tea.Cmd {
	return func() tea.Msg {
		existing, _ := theme.ListThemes()
		have := make(map[string]bool, len(existing))
		for _, name := range existing {
			have[name] = true
		}

		dir, err := theme.ThemeDir()
		if err != nil {
			return RemoteThemesFetchedMsg{Unreachable: true}
		}

		client := &http.Client{Timeout: remoteThemeFetchTimeout}
		var added []string
		for _, name := range knownRemoteThemes {
			if have[name] {
				continue
			}
			data, err := fetchRemoteTheme(client, name)
			if err != nil {
				if len(added) == 0 {
					// Nothing has succeeded yet, so this reads as "no
					// network" rather than "this one file is missing" — a
					// 404 on the project's own bundled theme would be a
					// repository mistake, not something worth retrying 11
					// more times for.
					return RemoteThemesFetchedMsg{Unreachable: true}
				}
				continue
			}
			if err := os.MkdirAll(dir, 0755); err != nil {
				continue
			}
			if err := os.WriteFile(filepath.Join(dir, name+".json"), data, 0644); err != nil {
				continue
			}
			added = append(added, name)
		}
		return RemoteThemesFetchedMsg{Added: added}
	}
}

// fetchRemoteTheme downloads one theme file and sanity-checks that it
// actually parses as a theme.Theme before it is ever written to disk.
func fetchRemoteTheme(client *http.Client, name string) ([]byte, error) {
	resp, err := client.Get(remoteThemeBaseURL + name + ".json")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, &themeFetchStatusError{name: name, status: resp.StatusCode}
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var t theme.Theme
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	return data, nil
}

type themeFetchStatusError struct {
	name   string
	status int
}

func (e *themeFetchStatusError) Error() string {
	return "unexpected status " + strconv.Itoa(e.status) + " fetching " + e.name + ".json"
}

// CurrentContextSetMsg reports whether the new context became the active one.
type CurrentContextSetMsg struct {
	Err error
}

func setCurrentContextCmd(name string) tea.Cmd {
	return func() tea.Msg {
		return CurrentContextSetMsg{Err: config.SetCurrentContext(name)}
	}
}
