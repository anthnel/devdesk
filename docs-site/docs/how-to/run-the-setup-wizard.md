# Run the setup wizard

`dk setup` walks you through creating a context one question at a time,
instead of hand-editing YAML or clicking through the Configuration view field
by field. It's the fastest way to get a new context running, and the one
thing the TUI can't do for itself: guide a user who has no context yet.

```bash
dk setup
```

This runs standalone — no context is loaded first, so it works even before
`~/.devdesk/` exists at all.

## What it asks, in order

1. **Terminal font check** — shows a handful of Nerd Font glyphs and asks if
   they rendered correctly, before anything else risks looking broken.
2. **Context name** — lowercase letters, digits, hyphens, or `default`.
3. **Secret backend** — `auto`, `keyring`, or `git-credential`; checks live
   whether the choice is actually reachable before you commit to it.
4. **Container engine** — `auto`, `docker`, or `podman`; checks live whether
   the selected engine's daemon actually answers, not just whether the
   binary is on `PATH`.
5. **Theme** — the built-in theme plus, network permitting, any additional
   ones fetched from the DevDesk repository.
6. **Forge** — platform (GitLab/GitHub), instance URL, default
   group/organization, default visibility, clone method, and an access
   token, validated live against the real API before it's saved.

Every step explains itself, and every field except the context name can be
left blank to configure later — the wizard still creates the context either
way. `←`/`→` cycles a closed-set answer, `Enter` confirms and advances,
`Esc` goes back a step (or quits, from the first one).

## What it writes

The wizard calls the same save path the Configuration view uses
(`config.SaveContext`), so the result is identical to configuring by hand —
see [Configure a context](configure-a-context.md) for where the file lands
and how contexts are switched. If a context with that name already exists,
it asks before overwriting.

When it finishes, it reports the path it wrote and any warning (a token that
couldn't be saved, for instance) — then `dk` starts the full TUI against the
context you just created.
