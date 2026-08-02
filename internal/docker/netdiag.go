package docker

import (
	"fmt"
	"strings"
)

// DiagResult holds the output and success status of a diagnostic command
type DiagResult struct {
	Success bool
	Output  string
}

// runDiagHost runs a command in an ephemeral container with --network host
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

// RunPing executes ping -c 3 <target> in the network multitool container
func RunPing(image, target string) DiagResult {
	return runDiagHost(image, []string{"ping", "-c", "3", target})
}

// RunDNS executes dig <target> [@dnsServer] in the network multitool container.
// If dnsServer is empty, the container's default resolver is used.
func RunDNS(image, target, dnsServer string) DiagResult {
	args := []string{"dig", target}
	if strings.TrimSpace(dnsServer) != "" {
		args = append(args, "@"+strings.TrimSpace(dnsServer))
	}
	return runDiagHost(image, args)
}

// RunReverseDNS executes dig -x <ip> [@dnsServer] for PTR (reverse DNS) lookup.
// If dnsServer is empty, the container's default resolver is used.
func RunReverseDNS(image, ip, dnsServer string) DiagResult {
	args := []string{"dig", "-x", ip}
	if strings.TrimSpace(dnsServer) != "" {
		args = append(args, "@"+strings.TrimSpace(dnsServer))
	}
	return runDiagHost(image, args)
}

// RunTraceroute executes traceroute -m 30 -w 1 <target> in the network tool container
func RunTraceroute(image, target string) DiagResult {
	return runDiagHost(image, []string{"traceroute", "-m", "30", "-w", "1", target})
}

// RunTCPTraceroute executes tcptraceroute -m 30 -w 1 <target> <port> in the network tool container.
// The exit code is ignored: tcptraceroute returns non-zero when the destination is not reached within
// the hop limit, but the partial trace is still useful output.
func RunTCPTraceroute(image, target, port string) DiagResult {
	if runner.LookPath() != nil {
		return DiagResult{false, "docker not found"}
	}
	args := []string{"run", "--rm", "--network", "host", image, "tcptraceroute", "-m", "30", "-w", "1", target, port}
	output, _ := dockerCombined(args...)
	out := strings.ReplaceAll(string(output), "\r\n", "\n")
	out = strings.ReplaceAll(out, "\r", "\n")
	out = strings.TrimSpace(out)
	return DiagResult{out != "", out}
}

// RunNetcat executes nc -zv -w 2 <target> <port> in the network multitool container
func RunNetcat(image, target, port string) DiagResult {
	return runDiagHost(image, []string{"nc", "-zv", "-w", "2", target, port})
}

// RunCurl executes curl against the target, trying HTTPS then HTTP if port is 443 or 80 respectively
func RunCurl(image, target, port string) DiagResult {
	scheme := "http"
	if port == "443" {
		scheme = "https"
	}
	url := fmt.Sprintf("%s://%s:%s", scheme, target, port)
	return runDiagHost(image, []string{"curl", "-sS", "-I", "--max-time", "3", url})
}

// sslCertScript extracts the issuer, subject and expiry of the certificate served
// by "$1:$2". The target and port are read from the positional parameters and are
// never interpolated into the script text — see RunSSLCert.
const sslCertScript = `echo | openssl s_client -showcerts -servername "$1" -connect "$1:$2" 2>/dev/null | ` +
	`openssl x509 -inform pem -noout -text | grep "Issuer\|Subject\|Not After"`

// RunSSLCert checks the SSL certificate details for the target:port.
//
// The pipeline requires a shell, so target and port are passed as positional
// arguments ("sh -c <script> sh <target> <port>") rather than being formatted
// into the script. This keeps them as argv values that the shell never parses,
// so a target such as `example.com; rm -rf /` cannot break out of the script.
func RunSSLCert(image, target, port string) DiagResult {
	return runDiagHost(image, []string{"/bin/sh", "-c", sslCertScript, "sh", target, port})
}
