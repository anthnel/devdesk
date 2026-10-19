// Package browser opens a URL in the user's default web browser.
package browser

import (
	"os"
	"os/exec"
	"runtime"
)

// Open starts the platform's opener for url and reaps it in the background,
// so a finished opener does not linger as a zombie. It returns once the
// process has started, not once the browser has loaded the page.
func Open(url string) error {
	cmd := command(runtime.GOOS, url)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// command returns the opener for url on goos. Under WSL xdg-open probes the
// desktop with xprop and blocks for minutes when WSLg's X server is down,
// while Start still succeeds — W did nothing and said nothing, then opened
// one tab per keypress long after. explorer.exe hands the URL to the Windows
// default browser and does not depend on X.
func command(goos, url string) *exec.Cmd {
	switch goos {
	case "darwin":
		return exec.Command("open", url)
	case "windows":
		return exec.Command("cmd", "/c", "start", url)
	}
	if os.Getenv("WSL_DISTRO_NAME") != "" {
		if path, err := exec.LookPath("explorer.exe"); err == nil {
			return exec.Command(path, url)
		}
	}
	return exec.Command("xdg-open", url)
}
