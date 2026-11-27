# Recorded engine fixtures

This directory holds the recorded engine fixtures that `internal/enginefixture`
embeds (`go:embed`): one `.cli` file per exchange, in
`fixtures/<campaign>/<scenario>--<tool>-<version>-<mode>.cli`.

Since OC-09b2 (DEVDESK-807) it holds 40 real recordings: ten scenarios on four configurations
(docker 29.10.1 and 28.5.2 rootful, podman 6.2.1 and 5.8.11 rootless). The mechanisms are still
tested on the synthetic files of `testdata/synthetic`, each marked `recorded-by: synthetic`.

| Campaign | Scenarios |
|---|---|
| `containers` | `ps-health`, `rm-running`, `inspect-absent` |
| `images` | `rmi-used` |
| `networks` | `rm-used` |
| `volumes` | `ls`, `rm-used`, `prune-unused` |
| `launch` | `name-taken`, `port-taken` |

Each one is the contract of the same name in `internal/docker` (`*_contract_test.go`); the verify mode of
the contracts compares what a real engine answers to these files.

**Never edit a fixture by hand: it is a recording.** To change one, record it again, in a test VM:

    DEVDESK_FIXTURE_DENYLIST=<file of terms, outside the repository> \
    DEVDESK_ENGINE_CONTRACT=1 mise run engine-contract:record -- <docker|podman> <n|n1> [--mode rootful|rootless] [--keep-out <dir>]

`--keep-out <dir>` is the dry run: the result goes to `<dir>` and nothing is versioned. Read the diff before you
commit (the format is meant to be read, and the files are public text kept in the history). A scenario added to a
contract needs its four fixtures and a higher `minVersionedFixtures` (`stale_test.go`) in the same change; a version
bump of an engine (`tools/engine-vms/versions.env`) records the four configurations again.

Files here are written by `mise run engine-contract:record` (OC-09a3, DEVDESK-944): the contracts
record in a test VM, redacted, and `internal/enginefixture/cmd/engine-fixture-import` checks the batch again on the
workstation (format, one configuration, N and N-1, leak check) before it writes the files here. A file that records
nothing new is not rewritten, a file marked `record: skip: <reason>` is not touched, and nothing is deleted. See
`tools/engine-vms/README.md`, "Contract runs", and `docs/architecture/network.md`, "Engine contracts" and "Recorded
fixtures — `internal/enginefixture`".
