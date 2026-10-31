# Configure a context

A context is a complete, isolated configuration — forge credentials,
workspaces directory, registry, scan settings. Use several when you switch
between environments (work, personal, a client) and don't want their
settings or credentials to mix.

## Create a new context

The fastest way is `dk setup` from a terminal, before launching the TUI —
see [Run the setup wizard](run-the-setup-wizard.md). It asks one question at
a time (context name, secret backend, container engine, theme, forge) and
writes the same file this page describes editing by hand.

From inside the TUI, open the command palette (`ctrl+p`) and run:

```
:ctx new my-context
```

`ctx new <name>` creates the context with defaults, then switches to it. A
new context has no forge credentials yet, so you land on the authentication
screen (`:git-auth`); a `Context "my-context" created` notice confirms it.

Context names must be `default` or match `^[a-z0-9-]+$` — lowercase letters,
digits, hyphens. You cannot create a context named `new`, `list` or `-`: they
are command words. A name that already exists gives a warning and changes
nothing.

!!! warning "`ctx <name>` no longer creates a context"
    Earlier versions created a context when you switched to a name that did
    not exist. Now `ctx <name>` only switches to an existing one, so a typo
    cannot leave an empty context behind. Use `ctx new <name>` to create.

## Where it lives on disk

| Context    | File                              |
| ---------- | ---------------------------------- |
| `default`  | `~/.devdesk/config.yaml`           |
| any other  | `~/.devdesk/config-<name>.yaml`    |

The active context's name is tracked in `~/.devdesk/.current-context`.

## List and switch

```
:ctx               # show every context (same as :ctx list)
:ctx work          # switch to "work"
:ctx -             # go back to the previous context
```

`ctx -` works like `cd -`: run it twice and you are back where you started.
The previous context is remembered for the current session only. Right after
startup there is none, and DevDesk says `No previous context in this session`.

If the name does not exist, nothing is created:

```
Context "wrok" not found — did you mean "work"? Use :ctx new wrok to create it
```

A name with capitals or other invalid characters gets `Invalid context name`.
Switching to the context you are already on says `Already on context "work"`.

### Complete a name

After `ctx ` the suggestion list shows your contexts. The current one is
marked `(current)`; `-` (previous context) and `new` (create a context)
follow. Eight rows show at once; the list scrolls.

1. Type the start of a name: `ctx wo`.
2. Press `tab` to highlight the first row, and `tab` again to write it into
   the line.
3. Press `enter` to switch.

If your text is the start of several contexts (`ctx w` with `web` and `work`),
`enter` does not guess: nothing switches and the list says which names match.
Press `tab` to pick one, then `enter`. `esc` closes the line without any
effect.

## Edit settings

Everything under `app:`, `forge:`, `registry:`, `scan:`, `network:`, `mcp:`
is editable from the **Configuration** view (`:config`) — closed-set fields
(like `forge.type`) cycle with `←/→`, the rest are text fields. See the
[configuration reference](../reference/configuration.md) for every key, or
edit the YAML file directly and restart.

## Credentials stay out of the file

Forge tokens and registry passwords are never written to `config.yaml`.
DevDesk picks one credential backend per context — the host's keyring first,
falling back to the git credential helper, then in-memory only as a last
resort that doesn't survive a restart. `app.secret_backend` pins the choice
(`auto`, `keyring`, `git-credential`) if the default order isn't what you
want.

## Set the workspaces directory

```yaml
app:
  workspaces_dir: ~/workspaces   # the default
```

This is what the **Workspaces** view scans for local git repositories, and
where the forge explorer clones into by default.
