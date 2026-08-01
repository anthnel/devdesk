package gitlab

import (
	"os"
	"os/exec"
)

// Clone clones a Git repository to the target path
// repoURL is the full URL to clone (HTTPS or SSH)
// targetPath is the absolute path where the repo should be cloned
func Clone(repoURL, targetPath string) error {
	cmd := exec.Command("git", "clone", repoURL, targetPath)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

// DirExists checks if a directory exists at the given path
func DirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}
