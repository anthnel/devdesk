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
