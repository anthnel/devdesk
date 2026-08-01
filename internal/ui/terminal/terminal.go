// Package terminal provides terminal emulator auto-detection for opening new
// windows, tabs, or running commands in a detached terminal process.
package terminal

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// ForDir returns (binary, args, ok) to open a new terminal window at path.
// Returns ("", nil, false) when no supported terminal is detected.
func ForDir(path string) (string, []string, bool) {
	// Windows Terminal (wt.exe) — works from WSL2 and native Windows
	if os.Getenv("WT_SESSION") != "" {
		if distro := os.Getenv("WSL_DISTRO_NAME"); distro != "" {
			// Use -p to apply the WSL profile (fonts, background, etc.) and
			// wsl.exe --cd for reliable directory navigation.
			return "wt.exe", []string{"-p", distro, "--", "wsl.exe", "-d", distro, "--cd", path}, true
		}
		return "wt.exe", []string{"-d", path}, true
	}
	// Kitty
	if os.Getenv("KITTY_WINDOW_ID") != "" {
		return "kitty", []string{"--directory", path}, true
	}
	// WezTerm
	if os.Getenv("WEZTERM_PANE") != "" || os.Getenv("WEZTERM_UNIX_SOCKET") != "" {
		return "wezterm", []string{"start", "--cwd", path, "--", Shell()}, true
	}
	// Alacritty
	if os.Getenv("ALACRITTY_SOCKET") != "" {
		return "alacritty", []string{"--working-directory", path}, true
	}
	// COSMIC Terminal (System76/COSMIC Desktop)
	if isCOSMICSession() {
		if _, err := exec.LookPath("cosmic-term"); err == nil {
			return "cosmic-term", []string{"--working-directory", path}, true
		}
	}
	// VTE-based terminals (GNOME Terminal, XFCE4, Tilix)
	if os.Getenv("VTE_VERSION") != "" {
		if _, err := exec.LookPath("gnome-terminal"); err == nil {
			return "gnome-terminal", []string{"--working-directory=" + path}, true
		}
		if _, err := exec.LookPath("xfce4-terminal"); err == nil {
			return "xfce4-terminal", []string{"--working-directory", path}, true
		}
		if _, err := exec.LookPath("tilix"); err == nil {
			return "tilix", []string{"--working-directory", path}, true
		}
	}
	// TERM_PROGRAM (macOS / Hyper)
	switch os.Getenv("TERM_PROGRAM") {
	case "iTerm.app":
		return "open", []string{"-a", "iTerm", path}, true
	case "Hyper":
		return "hyper", []string{path}, true
	}
	return "", nil, false
}

// ForCmd returns (binary, args, ok) to open a new terminal window running innerArgs
// as the foreground command (e.g. []string{"docker", "exec", "-it", id, "/bin/bash"}).
// Returns ("", nil, false) when no supported terminal is detected.
func ForCmd(innerArgs []string) (string, []string, bool) {
	// Windows Terminal
	if os.Getenv("WT_SESSION") != "" {
		return "wt.exe", append([]string{"--"}, innerArgs...), true
	}
	// Kitty
	if os.Getenv("KITTY_WINDOW_ID") != "" {
		return "kitty", append([]string{"--"}, innerArgs...), true
	}
	// WezTerm
	if os.Getenv("WEZTERM_PANE") != "" || os.Getenv("WEZTERM_UNIX_SOCKET") != "" {
		return "wezterm", append([]string{"start", "--"}, innerArgs...), true
	}
	// Alacritty
	if os.Getenv("ALACRITTY_SOCKET") != "" {
		return "alacritty", append([]string{"-e"}, innerArgs...), true
	}
	// COSMIC Terminal
	if isCOSMICSession() {
		if _, err := exec.LookPath("cosmic-term"); err == nil {
			return "cosmic-term", append([]string{"--"}, innerArgs...), true
		}
	}
	// VTE-based terminals
	if os.Getenv("VTE_VERSION") != "" {
		if _, err := exec.LookPath("gnome-terminal"); err == nil {
			return "gnome-terminal", append([]string{"--"}, innerArgs...), true
		}
		if _, err := exec.LookPath("xfce4-terminal"); err == nil {
			return "xfce4-terminal", append([]string{"--"}, innerArgs...), true
		}
		if _, err := exec.LookPath("tilix"); err == nil {
			return "tilix", append([]string{"--"}, innerArgs...), true
		}
	}
	return "", nil, false
}

// Shell returns the user's default shell, falling back to %COMSPEC% on Windows
// or /bin/sh on Unix.
func Shell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	if runtime.GOOS == "windows" {
		if s := os.Getenv("COMSPEC"); s != "" {
			return s
		}
		return "cmd.exe"
	}
	return "/bin/sh"
}

// isCOSMICSession returns true when running inside a COSMIC desktop session.
func isCOSMICSession() bool {
	desktop := os.Getenv("XDG_CURRENT_DESKTOP")
	return strings.Contains(strings.ToUpper(desktop), "COSMIC")
}
