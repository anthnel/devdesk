# Commands

Press `ctrl+p` (or `:` when no text field has focus) to open the command
palette, then type any of these. The header always shows the hint
`ctrl+p Commands`.

| Command | Alias | Switches to |
|---|---|---|
| `dashboard` | `d` | Dashboard |
| `status` | `s` | Status |
| `git-auth` | `ga` | Forge authentication view |
| `git-explorer` | `ge` | Forge explorer view |
| `workspaces` | `ws`, `w` | Workspaces |
| `security` | `sec` | Security scanner |
| `templates` | `tpl` | Repository template catalog |
| `containers` | `cont`, `ct` | Containers |
| `oci-resources` | `oci` | OCI Resources |
| `netdiag` | `net` | Network Diagnostics (Diagnostics, Ports, Interfaces and Forward tabs) |
| `configuration` | `config`, `cfg` | Configuration |
| `jobs` | `j` | Jobs |
| `about` | `version` | Build info |
| `context <name>` | `ctx <name>` | Switch to the existing context `<name>` (it never creates one) |
| `context -` | `ctx -` | Return to the previous context of this session |
| `context new <name>` | `ctx new <name>` | Create context `<name>` and switch to it |
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
opens the command palette, from anywhere. A bare `:` does the same, but only
when nothing is focused — inside a text field it's an ordinary character,
which values like `https://trivy-server:4954` need.

## Suggestions

The palette is a framed box under the header: your input on top, a list of
suggestions below. It is titled `Go to`, or `Contexts` once you are choosing a
context (see below). The view stays visible around it. Opening it shows every
view and command, with its aliases and a one-line description. Typing narrows
the list, best match first, and highlights the first row. The list shows up to
eight rows; on a short terminal it shows fewer.

| Key | Effect |
|---|---|
| `↑` / `↓` | Move the highlight (no wrap) |
| `tab` | Write the highlighted name into the line, without running it |
| `enter` | Open the highlighted row; after `context `, run the text as typed (see below) |
| `esc` | Close the palette |

`tab` and `enter` are greyed in the header while no row is highlighted. An
`enter` on an empty line, or on text that matches nothing, says so on the
palette's bottom edge and changes nothing; the message goes at the next key.
The palette keeps the keyboard while it is open: other keys do not reach the
view behind it.

### Context names

After `context ` (or `ctx `) the list shows your contexts instead of commands:
the current one is marked `(current)`, then come `-` (previous context) and
`new` (create a context). The list shows eight rows at a time and scrolls.

- `tab` highlights the first row; a second `tab` writes the highlighted name
  into the line.
- `enter` runs what you typed. If the text is a prefix of several contexts
  (`ctx w` with `web` and `work`), nothing switches and a warning says which
  names match; press `tab` to choose one, then `enter`.
- `esc` closes the palette without doing anything.

A name that matches no context is an error, not a new context:
`Context "wrok" not found — did you mean "work"? Use :ctx new wrok to create it`.
`ctx -` flips between the two last contexts of the session; at startup it says
`No previous context in this session`. The previous context is kept in memory
only. You cannot create a context named `new`, `list` or `-`. `ctx` alone, or `ctx list`, opens the list of contexts.

## CLI subcommands

The `dk` binary itself takes one subcommand:

| Subcommand | Runs |
|---|---|
| `dk` (no argument) | The full TUI |
| `dk setup` | A standalone context-creation wizard — see [Run the setup wizard](../how-to/run-the-setup-wizard.md) |

There is no flag-parsing framework behind this — `setup` is a single literal
match on `os.Args[1]`, and it's the one thing that has to run *before* any
context is loaded, since it's what creates the first one.
