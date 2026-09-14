# Commands

Press `ctrl+p` (or `:` when no text field has focus) to open the command
line, then type any of these:

| Command | Alias | Switches to |
|---|---|---|
| `dashboard` | `d` | Dashboard |
| `status` | `s` | Status |
| `git-auth` | `ga` | Forge authentication view |
| `git-explorer` | `ge` | Forge explorer view |
| `workspaces` | `ws`, `w` | Workspaces |
| `security` | `sec` | Security scanner |
| `containers` | `cont`, `ct` | Containers |
| `oci-resources` | `oci` | OCI Resources |
| `netdiag` | `net` | Network Diagnostics |
| `configuration` | `config`, `cfg` | Configuration |
| `jobs` | `j` | Jobs |
| `about` | `version` | Build info |
| `context <name>` | `ctx <name>` | Switch to context `<name>` |
| `context list` | | List available contexts |
| `quit` | `q` | Exit |

One spelling per command — there is no forge-prefixed alternative
(`gitlab-auth`, `github-explorer`, …) and no second short form to remember.

!!! note "Where this list comes from"
    This table is generated from `internal/command/parser.go`'s `viewNames`
    map, and a test (`TestEveryDocumentedCommandParsesToWhatItClaims` in
    `internal/command/doc_test.go`) checks it against the authoritative copy
    in `docs/architecture/app-shell.md` on every change — so it can't drift
    silently the way it once did.

## `ctrl+p` vs `:`

`ctrl+p` is handled before any form's edit mode can claim it, so it always
opens the command line, from anywhere. A bare `:` does the same, but only
when nothing is focused — inside a text field it's an ordinary character,
which values like `https://trivy-server:4954` need.

## CLI subcommands

The `dk` binary itself takes one subcommand:

| Subcommand | Runs |
|---|---|
| `dk` (no argument) | The full TUI |
| `dk setup` | A standalone context-creation wizard — see [Run the setup wizard](../how-to/run-the-setup-wizard.md) |

There is no flag-parsing framework behind this — `setup` is a single literal
match on `os.Args[1]`, and it's the one thing that has to run *before* any
context is loaded, since it's what creates the first one.
