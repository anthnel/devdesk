// Package nettrace traces the path to an IPv4 target, hop by hop, from inside
// the DevDesk process.
//
// The trace used to need a container (or a traceroute binary) because probing
// with a chosen TTL and reading what the routers answer takes more than the
// standard library offers. Here the process does it itself: one connected UDP
// socket per TTL, with IP_TTL set and IP_RECVERR enabled, then a read of the
// socket's error queue for the ICMP answer. No raw ICMP socket is opened, so
// nothing is started and no container is needed. The package never starts a
// child process (see the source guard test).
//
// Only Linux is supported: elsewhere Supported and Trace return ErrUnsupported
// and the caller is expected to grey the feature out.
package nettrace
