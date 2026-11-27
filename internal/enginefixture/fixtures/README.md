# Recorded engine fixtures

This directory holds the recorded engine fixtures that `internal/enginefixture`
embeds (`go:embed`): one `.cli` file per exchange, in
`fixtures/<campaign>/<scenario>--<tool>-<version>-<mode>.cli`.

The file exists so that `go:embed` compiles while no fixture is versioned. No
real fixture is versioned before OC-09b (DEVDESK-805, user decision); the
mechanisms are tested on the synthetic files of `testdata/synthetic`, each
marked `recorded-by: synthetic`.

The format, the replay and the rules are described in
`docs/architecture/network.md`, "Recorded fixtures — `internal/enginefixture`".

Files here are written by `mise run engine-contract:record` (OC-09a3, DEVDESK-944), never by hand: the contracts
record in a test VM, redacted, and `internal/enginefixture/cmd/engine-fixture-import` checks the batch again on the
workstation (format, one configuration, N and N-1, leak check) before it writes the files here. A file that records
nothing new is not rewritten, a file marked `record: skip: <reason>` is not touched, and nothing is deleted. Record
with `--keep-out <dir>` to look at the result first. See `tools/engine-vms/README.md`, "Contract runs".
