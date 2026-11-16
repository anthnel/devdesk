// Package nexusbench is the disposable Nexus Community bench (DEVDESK-504,
// NX-01): a single Nexus pod in a dedicated namespace of the k0s cluster,
// deployed, provisioned over its REST API and destroyed with one command, so
// that the registry measurements of the NX stories run against a real Nexus.
//
// The package is a development tool, not product code: the dk binary does not
// import it, and none of its tests run against a cluster unless
// DEVDESK_NEXUS_BENCH=1 is set.
//
// This file holds nothing but documentation. params.go turns defaults,
// environment variables and flags into a validated Params value; kubectl.go is
// the only file that starts a process but for the git check of secrets.go, and
// RunPreflight only reads the cluster. secrets.go keeps the passwords in a file
// outside the repository; descriptor.go writes the versioned description of the
// bench, which holds no secret. nexusapi.go is the REST client and provision.go the
// idempotent provisioning built on it; initialpassword.go reads the initial admin
// password of the pod through the Runner. deploy.go applies the manifests and
// waits for Nexus, and probe.go is the HTTP readiness probe it asks; see
// docs/architecture/nexus-bench.md.
package nexusbench
