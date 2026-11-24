package scan

import "strings"

// The renderers (kustomize, helm) read a repository that is not trusted, and an
// overlay that names `git@host:o/r` or `https://host/m.yaml` makes kustomize fetch
// it, with the user's ssh agent, proxy and credentials, before anything was
// validated (DEVDESK-767). A renderer therefore runs with the network cut: a
// native one with offlineEnviron, one from an image with containerRun.Offline.
// kubeconform is not cut: it downloads the schemas it validates against.

// deadProxy is the proxy a native renderer is pointed at: nothing listens on
// port 0, so every HTTP or HTTPS request fails at once, without a name lookup.
const deadProxy = "http://127.0.0.1:0"

// noGitTransport is a GIT_ALLOW_PROTOCOL that names no transport git knows, so
// git refuses every one of them, `file` included (`transport 'file' not
// allowed`): "file" would let an overlay read any git repository of this machine.
const noGitTransport = "devdesk-none"

// offlineNames are the variables offlineEnviron replaces or removes, upper case:
// the proxy ones in both casings, the git transport list, the CGI marker that
// makes Go ignore HTTP_PROXY, the locale kustomize's messages are read in, and
// what would let git reach a host with the user's identity (the ssh agent, the
// ssh and proxy commands).
var offlineNames = map[string]bool{
	"HTTP_PROXY": true, "HTTPS_PROXY": true, "ALL_PROXY": true, "NO_PROXY": true,
	"REQUEST_METHOD": true, "GIT_ALLOW_PROTOCOL": true, "LC_ALL": true,
	"SSH_AUTH_SOCK": true, "GIT_TERMINAL_PROMPT": true, "GIT_SSH": true, "GIT_SSH_COMMAND": true, "GIT_PROXY_COMMAND": true,
}

// offlineEnviron is base with the network cut for a native renderer: no proxy
// of the host, no ssh agent or command, no transport at all for git (so neither
// ssh, https nor file is ever started, and git never asks for a password), a
// dead proxy for everything else, and the C locale so the messages
// remoteBlocked reads are not translated. base is not modified and the result
// shares no memory with it.
func offlineEnviron(base []string) []string {
	env := make([]string, 0, len(base)+8)
	for _, kv := range base {
		name, _, _ := strings.Cut(kv, "=")
		if !offlineNames[strings.ToUpper(name)] {
			env = append(env, kv)
		}
	}
	return append(env,
		"GIT_ALLOW_PROTOCOL="+noGitTransport, "GIT_TERMINAL_PROMPT=0",
		"HTTP_PROXY="+deadProxy, "HTTPS_PROXY="+deadProxy,
		"http_proxy="+deadProxy, "https_proxy="+deadProxy,
		"NO_PROXY=", "no_proxy=",
		"LC_ALL=C",
	)
}

// remoteBlocked reports whether detail, what kustomize printed, says a remote
// base was refused because the network is cut. The caller asks only for a run
// that was offline: with the network on, the same words are an overlay that
// does not build.
//
// From a binary, the markers were measured on kustomize v5.8.3 and git 2.47.3
// run with offlineEnviron's environment: git refuses the transport, and Go's
// HTTP client cannot reach the dead proxy. From an image, they are what
// `--network none` is expected to print (not measured, no engine was reachable):
// a name that does not resolve or a network that is unreachable, while
// accumulating a resource, or a kustomize image without git.
func remoteBlocked(detail string, from ToolSource) bool {
	switch from {
	case ToolSourceBinary:
		return (strings.Contains(detail, "transport '") && strings.Contains(detail, "' not allowed")) ||
			strings.Contains(detail, "proxyconnect tcp: dial tcp 127.0.0.1:0")
	case ToolSourceContainer:
		if !strings.Contains(detail, "accumulating resources from") {
			return false
		}
		for _, marker := range []string{"no such host", "Could not resolve host", "network is unreachable", "no 'git' program"} {
			if strings.Contains(detail, marker) {
				return true
			}
		}
	}
	return false
}
