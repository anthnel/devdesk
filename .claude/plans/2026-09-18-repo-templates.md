# Repository templates — catalog view and picker

Status: all five steps done (#216, #217, #219, #220 the picker, #221 the scan),
plus the fetch cache behind `F` (below). Where this plan and
`docs/architecture/templates.md` disagree, the doc wins — in particular, registry
discovery (`FromOCI`/`Merge`, the `Discovered` flag, adopt-on-edit) was built and
then removed because nobody used it, so the picker lists declared templates only
and the template is fetched *before* the repository is created. What remains is
listed under "Out of scope" and "Open questions".

The fetch cache landed as one JSON file per slug in
`~/.devdesk/cache/templates/`, not as the shallow clone into a directory that
step 1 sketched: it has to hold `oci` and `local` content too, and one record
carrying its source and read time is what lets an edited entry miss.

## Goal

When a repository is created (GitLab or GitHub, explorer `N`), the user may pick
a template that fills its first commit — or pick none and get an empty
repository, exactly as today. Templates are managed in a dedicated view that
lists them, tags them, and references where they live: a remote git repository,
a local git repository, or an OCI artifact.

## What exists and is kept

- `forge.InitialCommit` — one parentless commit, both backends. The apply path
  stays generic; native forge templates (`POST /repos/{o}/{r}/generate`, GitLab
  project templates) are **not** used: neither crosses from one forge to the other.
- `oci.Client.ListTemplates` / `DownloadTemplate` under
  `registry.templates_repository` — becomes one source among three, and the
  catalog listing becomes *discovery* rather than the only way in.
- The router's selection mode (`internal/app/selection.go`): one view borrows
  another to pick something. Today only `ws` is lent (clone destination); the
  templates view becomes the second lendable view.

## What changes

### 1. Model — `internal/template` (new package)

```go
type SourceKind string // "git" | "local" | "oci"

type Entry struct {
    Slug        string     // identifier, unique in the catalog
    Name        string
    Description string
    Tags        []string   // free-form: java, spring-boot, gitlab-component…
    Source      Source
    Discovered  bool       // from the OCI catalog, read-only until adopted
}

type Source struct {
    Kind   SourceKind
    URL    string // git: clone URL; oci: registry URL
    Path   string // local: directory; oci: repository; git: optional subdirectory
    Ref    string // git/local: branch, tag or SHA; oci: tag
}

type File struct {
    Path       string
    Content    []byte
    Executable bool
}

type Fetcher interface {
    Fetch(ctx context.Context, src Source) ([]File, error)
}
```

- **git**: shallow clone of `Ref` into `~/.devdesk/cache/templates/<slug>`,
  subdirectory re-rooted, `.git` dropped.
- **local**: `git archive <Ref>` semantics — committed content only, so no
  `.git`, no ignored files, no uncommitted edits. A directory that is not a git
  repository is refused with a named reason rather than copied raw.
- **oci**: existing download, rewritten to return `[]File` with bytes and the
  tar header's mode (fixes binaries and `mvnw`/`gradlew` losing `+x`).
- Credentials resolved per host through `internal/credentials`, never stored in
  the catalog.

### 2. Storage — global, not per context

`~/.devdesk/templates.yaml`, one catalog for every context: a Spring Boot
template is as useful to a GitHub context as to a GitLab one. Only the
*discovered* OCI entries depend on the active context (its registry); they are
merged in at load, cached like the registry group members
(`internal/cache`, stale-while-revalidate).

Optional `template.yaml` at a template's root (name, description, tags) is read
when an entry is added, to prefill the form. It is also where variables would
go later (see Out of scope).

### 3. `forge.FileChange` carries bytes

`Content string` → `Content []byte` + `Executable bool`.
- GitLab: `CommitActionOptions.Encoding = base64`, `ExecuteFilemode`.
- GitHub: blob created with `encoding: base64`, tree entry mode `100755` when
  executable.
Tests on both backends for a binary file and an executable bit.

### 4. The templates view — `:templates`, alias `tpl`

`datatable`, Rules 116/122/125/136/139 as usual.

| Column | Sizing | Notes |
|---|---|---|
| (icon) | Fixed, `IconColumnWidth` | source kind, colored through a new `IconRole` |
| Name | Content | `Search` |
| Tags | Content | comma-joined, `Search`, `Optional` |
| Source | Content | URL / path / repository, `Optional` |
| Ref | Content | dim when defaulted |

Actions (existing vocabulary, no new letter):
`N` Create · `E` Edit · `D` Delete · `F` Sync (refetch into the cache) ·
`V` Pager (file tree of the fetched content) · `S` Scan (Trivy + Gitleaks on the
fetched content — a secret in a template ends up in every repository made from it).
`E` on a discovered entry *adopts* it (becomes a declared entry with tags);
`D` on one is greyed out with a reason (Rule 130).

Form in the viewport (Rule 112): Name, Description (`WrappedInput`), Tags,
Kind (`←→` cycle, Rule 132), then the kind's own fields (URL / Path / Ref).

### 5. Picking a template when creating a repository

The form's **Template** field stops being an inline dropdown
(`renderTemplateList`, `maxVisibleTemplates` removed). It becomes a
single-line field:

```
  Template 󰅂 none                    ← default: empty repository
● Template 󰅂 spring-boot-api  java · spring-boot
```

- Default value **none** → empty repository, exactly today's behavior with no
  template chosen. Submitting without touching the field never applies anything.
- `enter` on the field → the explorer asks the router to **lend the templates
  view in selection mode** (`templates.NewForSelection`), same mechanism as the
  clone destination. The user browses the full table: filter by `/` on name and
  tags, sort with `.`, `V` to look inside before choosing.
- In the picker: `enter` picks the row → `TemplateSelectedMsg{Slug}` back to the
  explorer; `esc` → `TemplateSelectionCancelledMsg`, field unchanged.
- Back in the form, `backspace` on the field resets it to **none** (a structural
  control, greyed when already none — Rule 130).
- The form's state (name, description, visibility) survives the round trip: the
  explorer model is kept in `a.views` while the picker is lent.
- Selection mode swaps the whole shortcut list (Rule 130, mode): `enter` Select,
  `esc` Cancel, `/` Filter, `V` Pager; `N/E/D/S/F` are absent there, not greyed.

`internal/app/selection.go` is re-parameterised: its comment says "only the
workspaces view is ever lent" — no longer true, so `leaveSelectionMode` must
delete the view that was lent, not `ViewWorkspaces` unconditionally.

### 6. Applying

`createProject` resolves the chosen slug to an `Entry` in `Update()` (Rule 110),
copies it into the Cmd, then: `CreateRepository` → `Fetcher.Fetch` →
`InitialCommit`. The existing `TemplateError` path stays: the repository exists
whatever the template did, the footer says the template failed. Long fetches go
through `internal/jobs` so `:jobs` shows them.

## Order of work (one PR each)

1. `forge.FileChange` bytes + executable, both backends, tests. No UI change.
2. `internal/template`: model, YAML store, three fetchers, tests (local fetcher
   against a temp repo; git fetcher against a local bare repo; OCI with the
   existing test server).
3. Templates view: list, filter, `N/E/D/F/V`, help (Rule 114),
   `docs/architecture/templates.md`, command alias.
4. Picker: selection mode, form field rewrite, router generalisation,
   explorer wiring. `renderTemplateList` removed.
5. `S` Scan on a template.

## Tests worth writing

- A template with a binary file and an executable script arrives intact on both
  backends (fake servers).
- Local fetcher ignores uncommitted edits and ignored files.
- Creating with the field left on **none** calls `CreateRepository` and never
  `InitialCommit`.
- Picker round trip keeps name/description/visibility typed before `enter`.
- `esc` in the picker leaves the previous choice; `backspace` resets to none.
- Same key set across states of the templates view (Rule 130 test helper).
- A discovered entry cannot be deleted, and says why.

## Out of scope (v1)

- Variable substitution (project name, group, Java package). The `template.yaml`
  format reserves a `variables:` key; nothing reads it yet.
- Pushing by `git push` instead of the API. Revisit only if API size limits bite.
- Sharing the catalog between machines.

## Open questions

- Should `ws` get a "register as template" action? It would need a letter —
  `J`, `Q` or `Z` are the free ones. Deferred until the view exists.
