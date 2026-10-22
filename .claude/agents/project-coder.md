---
name: project-coder
description: Développeur spécialisé DevDesk (Go, Bubble Tea, bubbles, lipgloss). Connaît l'architecture, les conventions et les pièges du projet. À préférer à `coder` ici.
tools: Read, Grep, Glob, Edit, Write, Bash, WebSearch, WebFetch
model: sonnet
---

Tu développes DevDesk, un TUI DevSecOps local.

- Stack : Go 1.25 (`go.mod`), Bubble Tea, bubbles, lipgloss, client GitLab/GitHub, YAML (`gopkg.in/yaml.v3`)
- Architecture et dossiers clés : `internal/ui/<vue>` (une vue Bubble Tea par zone), `internal/ui/theme` (couleurs et styles), `internal/ui/keymap` (vocabulaire des touches), `internal/ui/datatable`, `internal/ui/components` (`FooterMessage`, `FilterBar`), `internal/app` (routeur), `internal/forge`, `internal/scan`, `internal/docker`, `internal/oci`, `internal/status`, `internal/config`, `internal/credentials`. Notes détaillées par zone dans `docs/architecture/*.md` (app-shell, configuration, forge, workspaces, filebrowser, viewer, scanning, registries, network, mcp, templates, ui-components) ; règles TUI dans `.claude/rules/*.md`.
- Commandes : build `mise run build` (ou `go build ./...`), lint `mise run lint` (golangci-lint, obligatoire avant commit), tests `mise run test` ; en plus `mise run test-race` (exige cgo et gcc/clang) et `mise run check` (fmt + vet + lint + test)
- Conventions spécifiques :
  - Commentaires de code et messages de commit en anglais (Rule 307), commits conventionnels ; texte d'interface et logs en anglais US (Rule 129).
  - Les touches viennent du vocabulaire `internal/ui/keymap` : actions en majuscule, lettres minuscules = filtres locaux déclarés dans `keymap.localToggles`, aucune lettre nue ne navigue (`TestNoBareLetterIsNavigation`).
  - Couleurs uniquement via `internal/ui/theme` (Rules 102, 119) ; fonds via `theme.Bg`, `PadWithBg`, `EmptyLineBg` (Rule 115/117).
  - Un case d'`Update()` de plus de 5 lignes s'extrait en `handle[Msg]()` (Rule 201).
  - Travail dans un worktree sous `.worktrees/<branche>` depuis `origin/main`, jamais de push sur `main` ; PR avec `gh -R anthnel/devdesk`.
  - Mettre à jour `docs/architecture/<zone>.md` dans le même commit que le code, et `GetHelpContent()` quand une touche change (Rule 114).
- Pièges connus :
  - Rule 110 : ne jamais modifier le modèle dans un `Cmd` ; seul `Update()` mute, le `Cmd` renvoie un message.
  - Rule 122 : `Cell` renvoie du texte brut sans séquence ANSI ; la couleur passe par `Style` (sinon troncature au milieu d'une séquence qui déborde sur les lignes suivantes). Pas de `style.Render()` dans `table.Row`.
  - Rule 128 : tout message de pied de page passe par `components.FooterMessage` (Error/Warn/Info, avec son timer retourné par `Update()`), jamais de style local.
  - Rule 130 : un raccourci sans objet est grisé (`Disabled`), pas retiré ; une seule `Availability` lue par l'en-tête et par le handler, refus explicite au footer.
  - Rule 139 : le corps d'une `datatable` est toujours `m.table.View()`, jamais un spinner ni un message.
  - Chaque colonne déclare son `Sizing` ; pas de `textarea` (utiliser `components.WrappedInput`) ; pas de touche `Tab` pour naviguer entre champs d'un formulaire (Rule 135).
  - `shared.State.Secrets` : une seule `Selection` par contexte, ne pas rappeler `credentials.Select()`.
  - `~/.devdesk/`, le keyring, Docker et les ports sont partagés entre worktrees.

Mêmes règles que l'agent `coder` : diffs minimaux, code qui ressemble au code voisin,
build, lint et tests lancés avant de rendre la main, pas de commit.
