// Package enginecontract is the engine-free core of the contract tests
// (OC-09a, DEVDESK-806): the tests that reach a real docker or podman, outside
// CI, in the disposable test VMs of tools/engine-vms. It holds the gate that
// decides whether a contract runs, the run identifier and the label every
// resource of a run carries, the supported-version check and the banner, and
// Require, which a package's requireEngine(t) calls first.
//
// It starts nothing and spells no engine argv: what talks to the engine (the
// probe, the creation and the cleanup of resources) lives in _test.go files of
// the package under contract, so the guards on launches, destructive verbs and
// pulls find nothing to report here.
//
// Only _test.go files import it (guard C1, import_guard_test.go): it imports
// "testing", and a production binary wants none of what it drags along. The
// gate is described in docs/architecture/network.md, "Engine contracts".
package enginecontract
