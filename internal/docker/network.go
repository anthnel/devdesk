package docker

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"unicode"
)

// PortInfo represents a parsed row from ss -tupan output.
type PortInfo struct {
	Protocol  string
	State     string
	LocalAddr string
	PeerAddr  string
	PID       string
	Process   string
}

// RunSS runs ss -tupan inside an ephemeral Docker container with --net=host --pid=host
// and parses the output into a slice of PortInfo.
// RunSS runs ss inside an ephemeral Docker container with --net=host --pid=host --user=root.
// numeric=true adds -n (show raw IPs/ports); numeric=false lets ss resolve to DNS names.
func RunSS(image string, numeric bool) ([]PortInfo, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not found")
	}
	ssFlags := "-tupa"
	if numeric {
		ssFlags = "-tupan"
	}
	// --privileged + --pid=host lets ss -p read /proc/<pid>/fd on the host.
	// Mount host DNS files so ss can resolve hostnames when -n is not set.
	// nsswitch.conf is needed so getaddrinfo() uses "files" + "dns" order.
	args := []string{
		"run", "--rm", "--net=host", "--pid=host", "--privileged",
		"-v", "/etc/resolv.conf:/etc/resolv.conf:ro",
		"-v", "/etc/hosts:/etc/hosts:ro",
		"-v", "/etc/nsswitch.conf:/etc/nsswitch.conf:ro",
		image, "ss", ssFlags,
	}
	cmd := exec.Command("docker", args...)
	output, err := cmd.CombinedOutput()
	raw := string(output)
	if err != nil {
		return nil, fmt.Errorf("ss command failed: %w\n%s", err, raw)
	}
	ports := parseSSOutput(raw)
	// Log raw output when process info is missing to ease debugging.
	if len(ports) > 0 && ports[0].PID == "" {
		preview := raw
		if len(preview) > 400 {
			preview = preview[:400]
		}
		log.Printf("DEBUG [docker/network] ss output (process info absent — check image has iproute2 ss): %s", preview)
	}
	return ports, nil
}

// KillProcess sends SIGKILL to a process on the host via a privileged container.
func KillProcess(image, pid string) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker not found")
	}
	args := []string{"run", "--rm", "--pid=host", "--privileged", image, "kill", "-9", pid}
	cmd := exec.Command("docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("kill -9 %s failed: %w\n%s", pid, err, string(output))
	}
	return nil
}

// parseSSOutput parses the text output of ss -tupan into a slice of PortInfo.
// It handles two formats produced by different iproute2 versions:
//   - process info on the same line (older): "tcp LISTEN 0 128 *:22 *:* users:(("sshd",pid=1,fd=3))"
//   - process info on the next indented line (newer iproute2):
//     "tcp LISTEN 0 128 *:22 *:*\n\t\tusers:(("sshd",pid=1,fd=3))"
func parseSSOutput(raw string) []PortInfo {
	var result []PortInfo
	lines := strings.Split(raw, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "Netid") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 6 {
			// Could be a continuation line with process info (starts with "users:(")
			if len(result) > 0 && strings.HasPrefix(line, "users:(") {
				result[len(result)-1].Process, result[len(result)-1].PID = parseSSProcess(line)
			}
			continue
		}
		pi := PortInfo{
			Protocol:  fields[0],
			State:     fields[1],
			LocalAddr: fields[4],
			PeerAddr:  fields[5],
		}
		if len(fields) >= 7 {
			pi.Process, pi.PID = parseSSProcess(fields[6])
		} else if i+1 < len(lines) {
			// Check the next line for process info (newer iproute2 wraps to next line)
			next := strings.TrimSpace(lines[i+1])
			if strings.HasPrefix(next, "users:(") {
				pi.Process, pi.PID = parseSSProcess(next)
				i++ // consume the continuation line
			}
		}
		result = append(result, pi)
	}
	return result
}

// parseSSProcess extracts process name and PID from the ss process column.
// Input format: users:(("sshd",pid=1234,fd=3)) or users:(("a",pid=1,fd=2),("b",pid=3,fd=4))
func parseSSProcess(s string) (name, pid string) {
	// Extract first process name: quoted string after `("`
	if _, after, ok := strings.Cut(s, `("`); ok {
		if name, _, ok = strings.Cut(after, `"`); !ok {
			name = ""
		}
	}
	// Extract first PID: digits after "pid="
	if _, rest, ok := strings.Cut(s, "pid="); ok {
		end := strings.IndexFunc(rest, func(r rune) bool {
			return !unicode.IsDigit(r)
		})
		if end < 0 {
			end = len(rest)
		}
		pid = rest[:end]
	}
	return
}
