# Configuration — the schema and the view

> DevDesk architecture notes. Referenced from `.claude/CLAUDE.md`;
> read this file when working on the code it describes.

## Configuration System

Config loaded from `~/.devdesk/config.yaml` with schema defined in `internal/config/config.go`:
- `App` - Global settings (theme, default view, workspaces dir, `show_hidden_files`, `secret_backend`)
- `Status` - Monitoring settings (refresh interval, components)
- `Forge` - the platform this context targets: `type`, URL and clone settings
- `Registry` - OCI registry configuration (see Registry model below)
- `Scan` - Security scanning (Trivy, Gitleaks)
- `Network` - what the netdiag view runs on: the tool image, and the dials
  `internal/netcheck` used to hardcode
- `MCP` - the read-only MCP server (§3.38): `enabled`, off by default, and
  `expose`, an allow-list of tool names

**`forge:` was `gitlab:`, and the rename is migrated rather than announced.**
A context targets one platform (§3.6), so the section is named after the role
rather than after the one implementation there was, and it gains a `type:` —
`gitlab` or `github`, **declared, never sniffed**, on the registry `provider`
precedent. An absent `type` means `gitlab`, which is what every file written
before the key existed meant.

`migrateGitLabSection` runs **first** in `applyDefaults`, and that ordering is
the whole of it. Every default below writes into `cfg.Forge`, so a migration
placed after them finds a block that is no longer empty, takes the
field-by-field path, and silently drops the user's own `parallel_jobs: 7` — that
is not a hazard imagined for the comment, it is what the first draft did and
what `TestAConfigCarryingRetiredKeysStillLoads` caught. The other half of the
reason is `docker:` → `network:`'s: `yaml.Unmarshal` is not strict here, so an
un-migrated block is dropped in silence, and the silence would point a context
at no host at all.

**A whole-block copy when `forge:` is absent, field by field when both are
present.** The distinction exists for one field: `IncludeArchived` is a bool, so
"unset" and "deliberately false" are the same value and no per-field guard can
tell them apart — it can only be carried over when the new block says nothing at
all. When both are present the new one wins field by field: its values were
written later, and overwriting them with the old ones would undo the edit that
created the situation.

`forge.type` is stated in `internal/config` and in each backend's `Shape()`, and
`TestTheForgeVocabularyMatchesTheConfig` holds the two in step — config must not
import a backend, and a backend must not be the authority on what a config file
may say, so a test is the only thing that can. Same arrangement as the registry
`provider` names.

The legacy secret migration is unaffected and the order is worth knowing:
`credentials.MigrateLegacySecrets` reads the **raw file** at router
construction, before anything can save, so a plaintext `gitlab.token` reaches
the store before the rename can rewrite the file without it.

**`network:` was `docker:`, and the image key has been renamed twice since.**
The one key `docker:` held was never a Docker setting, and the four other tabs of
the configuration view are each named after the section they write. Then the
setting itself lost its readers one by one — `ss` to §3.43, `ip`/`iptables` to
§3.44, `traceroute` to §3.47 — until only the OCI connectivity test was left, so
it is now named after it.

The chain is `docker.network_tool_image` → `network.tool_image` →
`network.connectivity_image`, and a file may sit at **any point** on it. Both
renames run in `applyDefaults` **before** the defaults, oldest first, each
clearing its key so it leaves the file on the next save (the precedent is
`RegistryItem.AuthEnabled`). The order is the whole of it: `yaml.Unmarshal` is
not strict here, so an un-migrated block is dropped in silence and a user
pointing at their own mirror would find the probe pulling from Docker Hub.
`TestTheImageSurvivesBothRenames` and `TestTheNewestKeyWins` are what make that
checkable rather than commented.

**No secret goes in this file.** `GitLabConfig` has no `Token` and
`RegistryConfig` has no `Password`; both live in the host secret store (see
Credentials Management). Do not add a secret-bearing field back — the schema is
what makes the guarantee checkable.

Config is injected into views at creation. Use `config.Save()` to persist changes.

## Configuration view — `internal/ui/configuration`

Edits every **scalar** setting a context carries, in six tabs (`app`, the
forge's own name, `scan`, `network`, `mcp`, `status`). The second is titled after the
platform the context targets — `gitlab` or `github` — rather than after the
section key: `forge:` is what the file says, and no user calls it that. Lists stay where they are consulted: monitors keep
their CRUD in `status`, registries keep `RegistryForm` in `oci-resources`.
Duplicating them here would be the opposite of the point.

Tabs are not decoration. Rule 135 reserves `Tab` for tabs and `↑↓` for fields,
so a tabbed form is the only layout where both keys have exactly one job.

Settings are declared as a table of `field` values in `fields.go`, each holding
**one pointer accessor** into the config (`func(*config.Config) *string`) rather
than a get/set pair. Twenty-nine settings with two closures each is where the
copy-paste defects of §2 came from; one reference means the read and the write
cannot disagree about which setting they mean.
`TestEveryFieldCarriesTheAccessorItsKindNeeds` and
`TestNoTwoFieldsAddressTheSameSetting` are what keep the table honest.

Fields inside a tab are grouped under a heading with a Nerd Font icon
(`SubTitleStyle`, the same treatment the security form used): `scan` separates
**Scanners**, **Trivy**, **Gitleaks**, **Plumber** and **Limits**. `group()` stamps the
heading onto a contiguous run rather than each field carrying its own, so a run
cannot be split by a typo and render its heading twice —
`TestEachTabRendersItsGroupHeadingsOnceInOrder` pins that.

**The header names the context, and the title does not.** A configuration
belongs to one, and editing `workspaces_dir` in the wrong context is otherwise
silent because the fields look identical in all of them — but that is what
`GetHeaderInfo` says here as it does in every other view, so a title repeating
it was the same fact twice on one screen. `GetTitle()` is `󰙨 Configuration`.

**The context's file path is a row in the form, not a header field.** It sits in
**Paths** under `workspaces_dir`, because that is what it belongs beside; the
header is for what changes as the user moves, and the file does not. It is the
one `kindStatic` field — shown, never written, and `Model.settleFocus` walks the
cursor past it in whichever direction it was already moving, since a focus
indicator on a row no key acts upon says the opposite of what is true. The
**Paths** group therefore reads three paths and then the checkbox that qualifies
them (`show_hidden_files`): a checkbox wedged between two value rows breaks the
column they share.

Chevrons and values are aligned on one column per tab, padded on the **head**
(label plus a cycle field's select icon) rather than on the label — padding the
label leaves a cycle field's chevron two cells right of every other. Checkboxes
are excluded from the measurement: they have no value, so a long checkbox label
would push every value right for nothing. `theme.RenderCheckbox` already emits
the focus indicator, so the view must not add a second.

| Kind | Control | Persists |
|---|---|---|
| closed set | cycle `←→` (Rule 132) | immediately |
| boolean | checkbox, `Space` only | immediately |
| text / integer | `textinput` | on blur, **after validation** |

**A refused value keeps the cursor on its field.** An unparseable integer or a
malformed Trivy address is reported (Rule 128) and *not* written — coercing to
zero is how `trivy_server: ":"` reached a config file in the first place.

Two settings are special-cased, matched by label:

- **Theme** applies as it is cycled, not on blur — otherwise the user chooses
  blind.
- **Secret backend** is confirmed when focus *leaves* the field, not on every
  `←→`, and **nothing is migrated between backends**. §3.9 removed the option
  that wrote a token to two stores at once; copying one here would rebuild it.
  Declining restores the previous value.

**`forge.url` belongs to this view, not to the auth view.** Both used to write
it, so neither was authoritative and editing it in one left the other stale. The
auth view now shows it read-only, points at `:config`, and owns only the token
and the act of logging in — which is where the §3.9 line falls: this view's
contract is "everything here goes to `config.yaml`", and a token never does.

**`Forge` is the first field of the Connection group, above the URL.**
Everything below it reconfigures from it — the URL example, the visibility set,
two labels, the tab's own title and icon — so it has to be above them for the
user to watch that happen.

It is a cycle field (Rule 132) whose values are `config.ForgeTypes()`, and it is
**settled on blur**, not on every `←→`: the change closes the session, and
cycling through the list would close it once per keypress, including on the way
back to where it started. That is the secret backend's shape, minus the
question — changing the platform is neither forbidden nor confirmed, the user is
told what it did.

**Changing the URL *or* the platform closes the client-side session**
(`ForgeChanged` on the message) and says so. One flag for two settings, because
the consequence is one: the session was opened against something the config no
longer describes. Nothing is revoked and no token is deleted — the user changed
an address or a platform, not their credentials.

The URL field is recognised by **accessor identity**
(`f.str(cfg) == &cfg.Forge.URL`), not by label: two tabs could both hold a field
called "URL".

**The host is detected, and an explicit choice is never overwritten.**
`forge.DetectType` recognises `gitlab.com` and `github.com` and **nothing else**
— self-hosted is the case that matters and `git.acme.com` could be either, so an
unrecognised host changes nothing rather than guessing confidently. Detection
re-runs when the URL is committed, but only while `forgeTouched` is false: once
the user has moved the Forge field, typing a URL must not contradict them. That
flag is the whole difference between helpful and possessive. Probing
(`/api/v4/version` against `/api/v3/`) was rejected — a round trip per commit,
and it fails on instances that authenticate those endpoints.

**Two things follow a platform change, and both would be silent bugs without
it.** The field table is rebuilt, because it is computed once from the type and
the router *keeps* this view on a save — so nothing else would. And a
`default_visibility` the new platform does not have is coerced to its most
private: GitHub.com has no `internal`, and a context carrying it would hold a
value the server refuses while the cycle field opened on a value absent from its
own list.

`ConfigSavedMsg` goes to the router, which drops every view *except this one* so
they rebuild against the saved config — keeping the configuration view is what
stops a save throwing away the cursor after every keystroke. `BackendChanged`
is separate because it is the one change no view can rebuild itself into: the
router has to resolve a fresh `credentials.Selection`.

