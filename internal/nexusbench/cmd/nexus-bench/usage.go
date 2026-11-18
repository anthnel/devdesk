package main

import (
	"fmt"
	"io"
)

const usageText = `nexus-bench: a disposable Nexus Community bench on a k0s cluster (DEVDESK-504)

Usage: nexus-bench <subcommand> [flags]

Subcommands:
  up         check the cluster, deploy the bench and its upstream registry, wait
             for Nexus, provision it, put docker-proxy back in service when the
             upstream was cut, seed the data set with skopeo and write the
             descriptor
  provision  provision a running Nexus again (idempotent) and write the descriptor
  down       delete the bench namespace and wait until it is gone
  seed       put the data set in a bench that up provisioned (needs skopeo on
             this machine, which mise does not install) and write the descriptor
  status     read the cluster and say how the bench is doing: pod, REST, upstream,
             descriptor, data set (changes nothing)
  render     print the manifests (contacts nothing)
  cut-upstream      scale the bench upstream to zero replicas and wait until no pod
                    is left, so the proxy can only serve what it cached (changes
                    nothing else; no descriptor)
  restore-upstream  bring the upstream back, put the proxy back in service and check
                    the images against the data set (needs skopeo on this machine)
  reset      delete docker-group, docker-proxy, docker-hosted and the upstream
             storage, then provision and seed again: the data set is back to its
             initial state, the pod, the namespace and the passwords are kept
             (needs skopeo on this machine)
  record     regenerate the recorded Nexus fixtures from the bench: replay their
             requests through the loopback ports (no kubectl) once the data set
             is checked, and rewrite the files whose answer changed
  help       print this text

Run nexus-bench <subcommand> -h for the flags of a subcommand. Every flag has a
NEXUS_BENCH_* variable; a flag beats its variable, which beats the default.

Through mise: mise run nexus-bench:up -- --namespace nexus-bench-2

The EULA of Nexus Community Edition is accepted only when you say so
(--accept-eula or NEXUS_BENCH_ACCEPT_EULA=yes); read it first.

Exit codes: 0 success, 1 failure (the cause and what to check are on stderr),
2 mistake in the command line. go run (so mise run nexus-bench:*) does not pass
the code on: it prints "exit status 2" and exits 1. Code 2 is only visible when
the compiled binary is run directly (go build ./internal/nexusbench/cmd/nexus-bench).
`

// printUsage writes the general help to w.
func printUsage(w io.Writer) {
	_, _ = io.WriteString(w, usageText)
}

// subcommandSummary is the line each subcommand help starts with.
var subcommandSummary = map[string]string{
	"up":               "Check the cluster, deploy the bench and its upstream, wait for Nexus, provision it, put docker-proxy back in service when the upstream was cut, seed the data set and write the descriptor. Seeding needs skopeo on this machine (mise does not install it) unless NEXUS_BENCH_UPSTREAM_URL points at an upstream of your own.",
	"provision":        "Provision a running Nexus again (idempotent) and write the descriptor.",
	"down":             "Delete the bench namespace, wait until it is gone, then remove its stored passwords.",
	"seed":             "Put the data set in a bench that up provisioned (images in docker-hosted and in the upstream, one pulled through docker-proxy) and write the descriptor. Needs skopeo on this machine; mise does not install it.",
	"status":           "Read the cluster and the descriptor and report the pod, REST, upstream and data set; nothing is changed.",
	"render":           "Print the manifests for these parameters; the cluster is not contacted.",
	"cut-upstream":     "Scale the bench upstream to zero replicas and wait until no pod of it is left: the proxy then serves only what it cached. Nothing else is changed and the descriptor is not written.",
	"restore-upstream": "Bring the upstream back to one ready replica, put docker-proxy back in service (its configuration is sent again and its cache invalidated) and check the images of the upstream and the cached ones against the data set. Needs skopeo on this machine; mise does not install it.",
	"reset":            "Bring the data set back to its initial state without recreating the instance: delete docker-group, docker-proxy and docker-hosted and replace the claim bench-upstream-data, then provision and seed again and write the descriptor. The Nexus pod, nexus-data, the namespace and the stored passwords are kept. Needs skopeo on this machine; mise does not install it.",
	"record":           "Regenerate the recorded Nexus fixtures from the bench: each fixture request is replayed through the loopback ports with the stored credentials, after the data set of the bench has been compared with the descriptor, and the redacted answers are written back only when every one passed. Uses no kubectl. DEVDESK_FIXTURE_DENYLIST names a file of extra terms (one per line) no fixture may hold.",
}

// printSubcommandHelp writes the help of one subcommand to w.
func printSubcommandHelp(w io.Writer, name string, flags *flagSet) {
	_, _ = fmt.Fprintf(w, "Usage: nexus-bench %s [flags]\n\n%s\n\nA flag beats its NEXUS_BENCH_* variable, which beats the default.\n\nFlags:\n", name, subcommandSummary[name])
	flags.printHelp(w)
}
