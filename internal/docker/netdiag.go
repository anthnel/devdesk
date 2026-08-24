package docker

import (
	"strconv"
	"strings"
)

// DiagResult holds the output and success status of a diagnostic command
type DiagResult struct {
	Success bool
	Output  string
}

// runDiagHost runs a command in an ephemeral container with --network host.
//
// Only the route traces are left here. DNS, ICMP, TCP, TLS and HTTP moved to
// internal/netcheck, the socket table to internal/ports and the interfaces to
// internal/netiface, all of which answer from the DevDesk process — because
// --network host is the Docker Desktop VM's network namespace on Windows and
// macOS, not the machine's, so every one of them was answering for a resolver,
// a routing table and a set of adapters the user is not on.
//
// A traceroute needs raw sockets and a tool worth not reimplementing, so it
// stays, and it keeps that caveat: it answers for the container's view.
func runDiagHost(image string, args []string) DiagResult {
	if runner.LookPath() != nil {
		return DiagResult{false, "docker not found"}
	}
	cmdArgs := append([]string{"run", "--rm", "--network", "host", image}, args...)
	output, err := dockerCombined(cmdArgs...)
	out := strings.ReplaceAll(string(output), "\r\n", "\n")
	out = strings.ReplaceAll(out, "\r", "\n")
	out = strings.TrimSpace(out)
	return DiagResult{err == nil, out}
}

// hopWait is how long a trace waits for each hop's reply, in seconds.
//
// It stays a constant while the hop limit is configurable, because the two
// multiply: a second setting would let a caller build a fifteen-minute trace
// out of two numbers that each look reasonable on their own.
const hopWait = "1"

// RunTraceroute executes traceroute -m <maxHops> -w 1 <target> in the network
// tool container.
func RunTraceroute(image, target string, maxHops int) DiagResult {
	return runDiagHost(image, []string{"traceroute", "-m", strconv.Itoa(maxHops), "-w", hopWait, target})
}

// RunTCPTraceroute executes tcptraceroute -m 30 -w 1 <target> <port> in the network tool container.
// The exit code is ignored: tcptraceroute returns non-zero when the destination is not reached within
// the hop limit, but the partial trace is still useful output.
func RunTCPTraceroute(image, target, port string, maxHops int) DiagResult {
	if runner.LookPath() != nil {
		return DiagResult{false, "docker not found"}
	}
	args := []string{"run", "--rm", "--network", "host", image,
		"tcptraceroute", "-m", strconv.Itoa(maxHops), "-w", hopWait, target, port}
	output, _ := dockerCombined(args...)
	out := strings.ReplaceAll(string(output), "\r\n", "\n")
	out = strings.ReplaceAll(out, "\r", "\n")
	out = strings.TrimSpace(out)
	return DiagResult{out != "", out}
}
