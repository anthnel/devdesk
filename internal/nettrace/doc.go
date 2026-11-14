// Package nettrace traces the path to an IPv4 target, hop by hop, from inside
// the DevDesk process.
//
// The trace used to need a container (or a traceroute binary) because sending
// probes with a chosen TTL and reading the ICMP answers takes raw-ish sockets.
// Here the probes are sent by the process itself, over unprivileged ICMP and
// UDP sockets, so nothing is started and no container is needed. The package
// never starts a child process (see the source guard test).
//
// Only Linux is supported: elsewhere Supported and Trace return ErrUnsupported
// and the caller is expected to grey the feature out.
package nettrace
