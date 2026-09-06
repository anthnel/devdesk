# Configure a context

A context is a complete, isolated configuration — forge credentials,
workspaces directory, registry, scan settings. Use several when you switch
between environments (work, personal, a client) and don't want their
settings or credentials to mix.

## Create a new context

Open the command palette (`ctrl+p`) and go to the **Configuration** view
(`:config`), or:

```
:ctx my-context
```

`ctx <name>` switches to a context, creating it with defaults if it doesn't
exist yet. Context names must be `default` or match `^[a-z0-9-]+$` —
lowercase letters, digits, hyphens.

## Where it lives on disk

| Context    | File                              |
| ---------- | ---------------------------------- |
| `default`  | `~/.devdesk/config.yaml`           |
| any other  | `~/.devdesk/config-<name>.yaml`    |

The active context's name is tracked in `~/.devdesk/.current-context`.

## List and switch

```
:context list      # show every context
:ctx work          # switch to "work"
```

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
