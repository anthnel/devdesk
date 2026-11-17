package main

import (
	"fmt"
	"io"
)

const usageText = `nexus-bench: a disposable Nexus Community bench on a k0s cluster (DEVDESK-504)

Usage: nexus-bench <subcommand> [flags]

Subcommands:
  up         check the cluster, deploy the bench and its upstream registry, wait
             for Nexus, provision it, seed the data set with skopeo and write
             the descriptor
  provision  provision a running Nexus again (idempotent) and write the descriptor
  down       delete the bench namespace and wait until it is gone
  seed       put the data set in a bench that up provisioned (needs skopeo on
             this machine, which mise does not install) and write the descriptor
  status     read the cluster and say how the bench is doing: pod, REST, upstream,
             descriptor, data set (changes nothing)
  render     print the manifests (contacts nothing)
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
	"up":        "Check the cluster, deploy the bench and its upstream, wait for Nexus, provision it, seed the data set and write the descriptor. Seeding needs skopeo on this machine (mise does not install it) unless NEXUS_BENCH_UPSTREAM_URL points at an upstream of your own.",
	"provision": "Provision a running Nexus again (idempotent) and write the descriptor.",
	"down":      "Delete the bench namespace, wait until it is gone, then remove its stored passwords.",
	"seed":      "Put the data set in a bench that up provisioned (images in docker-hosted and in the upstream, one pulled through docker-proxy) and write the descriptor. Needs skopeo on this machine; mise does not install it.",
	"status":    "Read the cluster and the descriptor and report the pod, REST, upstream and data set; nothing is changed.",
	"render":    "Print the manifests for these parameters; the cluster is not contacted.",
}

// printSubcommandHelp writes the help of one subcommand to w.
func printSubcommandHelp(w io.Writer, name string, flags *flagSet) {
	_, _ = fmt.Fprintf(w, "Usage: nexus-bench %s [flags]\n\n%s\n\nA flag beats its NEXUS_BENCH_* variable, which beats the default.\n\nFlags:\n", name, subcommandSummary[name])
	flags.printHelp(w)
}
