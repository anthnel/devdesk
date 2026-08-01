package docker

import (
	"os/exec"
	"strings"
)

// RunIPAddr executes ip addr show in an ephemeral --network host container
// and returns the raw output as a DiagResult.
func RunIPAddr(image string) DiagResult {
	return runDiagHost(image, []string{"ip", "addr", "show"})
}

// RunIPRoute executes ip route show in an ephemeral --network host container
// and returns the raw output as a DiagResult.
func RunIPRoute(image string) DiagResult {
	return runDiagHost(image, []string{"ip", "route", "show"})
}

// RunIPLink executes ip -s link show to retrieve MTU and RX/TX error statistics
// per interface.
func RunIPLink(image string) DiagResult {
	return runDiagHost(image, []string{"ip", "-s", "link", "show"})
}

// RunIPNeigh executes ip neigh show to list ARP/neighbour cache entries.
func RunIPNeigh(image string) DiagResult {
	return runDiagHost(image, []string{"ip", "neigh", "show"})
}

// RunFirewallRules attempts to list firewall rules using iptables-legacy, iptables,
// or nft (in that order). The output is prefixed with "IPTABLES:" or "NFTABLES:" so
// the caller can select the appropriate parser. "UNAVAILABLE:" is returned when none
// of the tools are found in the image.
// Requires --privileged to access netfilter tables.
func RunFirewallRules(image string) DiagResult {
	if _, err := exec.LookPath("docker"); err != nil {
		return DiagResult{false, "docker not found"}
	}
	shellCmd := `if command -v iptables-legacy >/dev/null 2>&1; then ` +
		`echo "IPTABLES:" && iptables-legacy -L -n --line-numbers; ` +
		`elif command -v iptables >/dev/null 2>&1; then ` +
		`echo "IPTABLES:" && iptables -L -n --line-numbers; ` +
		`elif command -v nft >/dev/null 2>&1; then ` +
		`echo "NFTABLES:" && nft list ruleset; ` +
		`else echo "UNAVAILABLE:"; fi`
	args := []string{
		"run", "--rm", "--network", "host", "--privileged",
		image, "/bin/sh", "-c", shellCmd,
	}
	cmd := exec.Command("docker", args...)
	output, err := cmd.CombinedOutput()
	out := strings.ReplaceAll(string(output), "\r\n", "\n")
	out = strings.ReplaceAll(out, "\r", "\n")
	out = strings.TrimSpace(out)
	return DiagResult{err == nil && !strings.HasPrefix(out, "UNAVAILABLE:"), out}
}
