# Configuration & Contexts

DevDesk keeps its settings in a per-context YAML file and edits them through a dedicated configuration view inside the TUI itself. This page explains the shape of that schema and the reasoning behind how the configuration view handles editing, validation, and persistence.

## The configuration schema

Configuration loads from `~/.devdesk/config.yaml`, with its schema defined in `internal/config/config.go`:

- **`App`** — global settings: theme, default view, workspaces directory, whether hidden files are shown, which secret backend to use
- **`Status`** — monitoring settings: refresh interval, the list of monitored components
- **`Forge`** — the platform this context targets: type, URL, and clone settings
- **`Registry`** — OCI registry configuration
- **`Scan`** — security scanning settings (Trivy, Gitleaks)
- **`Network`** — the dials the network diagnostics view runs against (timeouts, ping count, certificate expiry warning threshold, ports refresh interval)
- **`MCP`** — the read-only MCP server: whether it's enabled (off by default) and an allow-list of exposed tool names

**No secret ever lives in this file.** `GitLabConfig` has no `Token` field and `RegistryConfig` has no `Password` field — both are pushed out to the host's secret store instead (see the credentials management notes in the main architecture docs). This is enforced by the schema itself rather than by convention: there's simply no field to accidentally populate.

Configuration is injected into views at construction time, and any change must go through `config.Save()` to actually persist.

### Migrating an old schema forward

The `Forge` section used to be called `gitlab:`, back when GitLab was the only supported backend. Renaming it to `forge:` reflects that a context now targets a *role* (one platform, chosen explicitly) rather than a fixed implementation, and the section gained an explicit `type: gitlab | github` field — declared, never auto-detected from other clues. A missing `type` defaults to `gitlab`, matching what every config file written before the field existed actually meant.

The migration step runs deliberately **first**, before any other default-filling logic touches the `Forge` section. Every other piece of default-filling writes into that same section, so running the migration afterward would find a non-empty block, take a naive field-by-field merge path, and silently drop a user's own customized values (`parallel_jobs`, for instance) that weren't present in the legacy shape. YAML parsing here isn't strict, so a section that fails to migrate cleanly doesn't error — it's silently dropped, which for the forge section would mean a context pointing at no host at all.

The migration itself takes two different paths depending on what's already present: if the new `forge:` block is entirely absent, the old block is copied over wholesale, field by field. If both old and new blocks are present, the new values win field-by-field, since they were written more recently and reverting to old values would undo whatever edit produced the current file. The one field handled specially is `IncludeArchived` — a plain boolean can't distinguish "explicitly set to false" from "never set," so it's only carried forward from the old block when the new block is completely absent.

A similar rename applies to networking: `network:` used to be called `docker:`, from a time when its only field was the image used for a since-removed OCI connectivity test. `NetworkConfig` today holds only the actual netdiag dials.

## The configuration view — `internal/ui/configuration`

The view edits every **scalar** setting a context carries, organized into six tabs: `app`, a tab named after whichever forge platform is active (`gitlab` or `github` — not the literal section name), `scan`, `network`, `mcp`, and `status`. List-shaped settings deliberately stay where they're actually used instead of being duplicated here — monitors keep their full CRUD flow in the status view, registries keep theirs in the OCI resources view.

Tab navigation and field navigation use different keys by design: `Tab`/`Shift+Tab` moves between tabs, `↑`/`↓` moves between fields within a tab — a tabbed form is the one layout where both keys can have exactly one unambiguous job each.

Each editable setting is declared as an entry in a table (`fields.go`) holding a single pointer-accessor into the config struct, rather than a separate getter and setter. A design with roughly two dozen settings each needing a matched pair of closures is exactly what produced copy-paste bugs previously — collapsing to one accessor per field means the code that reads a setting and the code that writes it can't disagree about which field they mean.

| Value kind | Control | When it persists |
|---|---|---|
| closed set of values | left/right cycling | immediately |
| boolean | checkbox (space to toggle) | immediately |
| text / integer | text input | on blur, **after validation** |

A value that fails validation (an unparseable integer, a malformed server address) is reported in the footer and deliberately **not written** — silently coercing an invalid value to zero is exactly how a broken Trivy server address ended up saved to a config file in an earlier version.

### "On blur" has to include leaving the view entirely

Committing a text field's value used to only happen when the user moved focus with the arrow keys or switched tabs — which meant typing a path and then leaving the view via the command line (`ctrl+p`) never actually applied it. Because the router caches views and doesn't re-run initialization on return, coming back to the configuration view would show the typed value on screen while the file on disk still held the old one — the screen appeared to confirm the change on every visit, which is what made the underlying bug hard to track down.

Two things fixed this. First, the view implements the router's `Leave()` hook (see [App Shell & Router](app-shell.md)) — called before any switch away from the view, with a refusal cancelling the switch outright, since only the leaving view's own footer can report a rejected value; reporting it anywhere else would post the message to a screen the user is no longer looking at. Second, `esc` now explicitly commits the focused field rather than falling through and doing nothing, since pressing escape to "close" a field was the single most natural gesture that previously settled nothing at all.

### Two settings that don't follow the general pattern

- **Theme** applies live as it's cycled through, not on blur — waiting until the field loses focus would mean choosing a theme blind, unable to see it applied.
- **Secret backend** commits only when focus leaves the field (not on every keypress while cycling), and switching backends **never migrates an existing secret** between them — an earlier version that copied a token to two backends at once is exactly the kind of dual-write this design avoids reintroducing.

### The forge tab

The `Forge` field is the first field in the Connection group, positioned above the URL, because everything below it — the URL placeholder example, the valid visibility set, several labels, even the tab's own title and icon — reconfigures itself based on this value, so it needs to be visibly "upstream" of the fields that react to it.

Changing either the URL or the platform closes the current client-side session and says so in the footer — one flag covers both settings because the underlying consequence is identical either way: the session was opened against configuration that no longer matches what's on screen. Nothing is revoked and no token is deleted; the user changed an address or a platform, not their credentials.

Host detection is deliberately conservative: it recognizes `gitlab.com` and `github.com` specifically and nothing else, since self-hosted instances are the case that actually matters and a host like `git.acme.com` could plausibly be either platform — guessing wrong would be worse than not guessing. Detection re-runs whenever the URL field is committed, but only until the user has explicitly touched the `Forge` field themselves; after that, typing a new URL must never silently override an explicit choice.

Changing the platform triggers two follow-on effects, both necessary to avoid otherwise-silent inconsistencies: the field table is rebuilt from scratch (since it's computed from the platform type, and the router deliberately keeps this exact view alive across a save rather than rebuilding it), and a `default_visibility` value not supported by the new platform is coerced down to the most private option it does support — GitHub.com, for instance, has no `internal` visibility, and leaving a stale value in place would mean opening a cycle field on a value that isn't even in its own list.
