# Plan : uniformiser les messages du footer

**Complexité** : Medium — 1 nouveau composant, 1 ajout au thème, 9 paquets de vues touchés, ~15 fichiers de tests à reprendre.

## 1. Ce qui est demandé

1. **Tous** les messages du footer sont centrés.
2. Trois niveaux, trois couleurs :
   - **info** → une couleur plus neutre qu'aujourd'hui,
   - **warning** → l'orange des CVE MEDIUM,
   - **error** → le rouge des CVE CRITICAL.
3. Supprimer les messages verts et les messages alignés à gauche.
4. Quand un `datatable` charge ses données : plus de spinner dans le corps de la table — un **message d'info avec spinner, dans le footer uniquement**.

## 2. Ce que le relevé montre

### 2.1 Il y a huit implémentations du même footer

| Paquet | État | Rendu | Alignement actuel |
|---|---|---|---|
| `configuration` | `footerError`, `footerInfo`, `hint` | `view.go:148-158` + `centeredInfo` | erreur **gauche**, info centrée |
| `containers` | `errorMsg`, `actionLine()` | `view.go:53-63` | erreur **gauche**, action **gauche** (`Bg("  ")+…`) |
| `gitlab/explorer` | `footerError`, `footerInfo`, `cloneStatusLine`, `selectionStatusLine` | `view.go:139-160` + `renderInfoText` | erreur **gauche**, info centrée |
| `netdiag` (×3 modèles) | `footerError`/`footerInfo` sur `Model`, `portsModel`, `topologyModel` | `header.go:137-157` | erreur **gauche**, info centrée |
| `oci_resources` | `errorMsg`, `infoMsg`, `actionLine()` | `view.go:74-108` + `highlightLine` | centré |
| `security` | `statusMessage` | `view.go:134-139` | **vert**, **gauche** |
| `workspaces` | `footerError`, `footerInfo`, `syncStatusLine`, `selectionMessage` | `view.go:106-150` | erreur **gauche**, info centrée |
| `viewer` | `footerError`, `footerInfo` | `view.go:46-59` | erreur **gauche**, info centrée |

Trois helpers font déjà la même chose sous trois noms : `configuration.centeredInfo`,
`explorer.renderInfoText`, `oci_resources.highlightLine` — plus quatre blocs
`lipgloss.NewStyle().Foreground(ColorHighlight)…Align(Center)` recopiés à la main.
C'est cette duplication qui a laissé les erreurs à gauche partout : personne n'a
jamais centré la branche `StatusErrorStyle`.

**Le vert demandé est ici** : `security/view.go:138` rend `statusMessage` en
`theme.StatusOKStyle` (`#a6e3a1`, gras, non centré), et `security/view.go:50`
le **redouble dans le viewport** sous le panneau de warnings.

**Il n'existe aucun niveau *warning*** : tout est soit erreur, soit info. Une
bonne moitié des « infos » actuelles sont en fait des refus (« Scan already in
progress », « Not supported in Trivy client-server mode », « Nothing selected »).

### 2.2 Les couleurs demandées existent déjà

`manager.go:75-110` — `ColorError` (`#f38ba8`) **est** `SeverityCritical`, et
`ColorWarn` (`#fab387`) **est** `SeverityMedium`. Les alias doivent viser les
noms **severity**, parce que c'est la référence donnée et que ça survit à un
thème qui les sépare.

`ColorHighlight` (`#f9e2af`) porte l'info aujourd'hui : c'est un jaune, à un cran
de l'orange du warning. Les deux niveaux seraient indiscernables — d'où la
demande d'une couleur neutre.

### 2.3 Les spinners dans le corps des tables

| Site | Message |
|---|---|
| `containers/view.go:170` | `Loading containers...` |
| `oci_resources/view.go:460` | `Loading images...` |
| `oci_resources/view.go:475` | `Loading networks...` |
| `oci_resources/view.go:490` | `Loading volumes...` |
| `gitlab/explorer/view.go:57` | `Loading GitLab groups...` |
| `status/view.go:111` | `Checking components...` |
| `oci_resources/browser_view.go:104` | `Searching registries...` |

Chacun **remplace** la table : la vue perd son en-tête et ses colonnes le temps
du chargement, puis les retrouve — un saut de mise en page à chaque `ctrl+r`.

Hors périmètre (aucune table derrière) : `gitlab/auth` « Authenticating… »
(formulaire), `netdiag/view.go` « Running diagnostics… » (écran de progression),
`browser_view.go:149` « Pulling… » (écran d'opération), « Loading templates… »
(formulaire). `netdiag/topology_model.go:380` est un `viewport`, pas un
`datatable` : traité en phase 6, optionnelle.

## 3. Décisions

### D1 — Un composant, pas un helper

`internal/ui/components/footer_message.go`. Un helper de rendu laisserait l'état
(texte, niveau, minuterie) recopié dans huit modèles, ce qui est exactement la
situation actuelle. Le composant porte les deux.

```go
type Level int
const (LevelInfo Level = iota; LevelWarning; LevelError)

// Status est ce que la vue dérive à chaque frame (progression, hint,
// chargement). Il n'a pas de minuterie et ne vit pas dans le composant.
type Status struct {
    Text    string
    Spinner bool // préfixe la frame courante du spinner
}

type FooterMessage struct { … }

// Appelés depuis Update() uniquement (Rule 110). Retournent la minuterie.
func (f *FooterMessage) Info(text string) tea.Cmd
func (f *FooterMessage) Warn(text string) tea.Cmd
func (f *FooterMessage) Error(text string) tea.Cmd
func (f *FooterMessage) Clear()
func (f *FooterMessage) Handle(msg tea.Msg) bool  // consomme ClearFooterMsg
func (f *FooterMessage) SetSpinnerFrame(frame string)
func (f *FooterMessage) IsSet() bool

// Lecture seule.
func (f *FooterMessage) View(width int, s Status) string
```

**`View` retourne toujours une ligne pleine largeur**, vide comprise : Rule 124
budgète dessus, et c'est ce qui rend `GetFooterHeight()` inchangé partout.

**Précédence dans `View`** : erreur → warning → info → status. La raison est
déjà écrite dans `oci_resources/view.go:99` et `containers/view.go:76` : un échec
que l'utilisateur n'a pas lu prime sur la progression de ce qui tourne encore.

**`Status` est un paramètre, pas un champ.** `actionLine()` se dérive de
`BusyLabels()` et change sans événement ; le calculer dans `View()` est ce qui se
fait aujourd'hui et reste en lecture seule. Le mettre dans le composant
obligerait à le pousser depuis `Update`, donc à trouver un événement qui n'existe
pas.

**La minuterie est gardée par un identifiant.** `ClearFooterMsg{id}` ne nettoie
que si l'id correspond au message courant. Deux bénéfices : le type peut être
partagé entre paquets sans qu'une minuterie orpheline efface le message d'une
autre vue, et le bug actuel disparaît (un message posé à t+2,9 s est effacé à
t+3 s par la minuterie du précédent, dans les huit vues).

### D2 — Trois alias sémantiques, aucune clé de thème nouvelle

Précédent : les couleurs de syntaxe du viewer (`colors.go:60-77`) et
`ColorChartBg`. Dans `colors.go` + `ApplyTheme` :

```go
ColorFooterInfo  = ColorText              // #cdd6f4 — neutre
ColorFooterWarn  = ColorSeverityMedium    // #fab387 — l'orange MEDIUM
ColorFooterError = ColorSeverityCritical  // #f38ba8 — le rouge CRITICAL
```

Puis dans `styles.go` : `FooterInfoStyle`, `FooterWarnStyle`, `FooterErrorStyle`.
Warning et erreur en gras, info non — la hiérarchie passe par la graisse autant
que par la teinte, et une info neutre en gras redeviendrait une alerte.

> **Choix à confirmer** : `ColorText` (texte normal de l'application) pour
> l'info. L'autre candidat est `ColorDim`, plus discret mais assez pâle pour
> qu'un message de trois secondes passe inaperçu. Le plan retient `ColorText`.

`StatusOKStyle` n'apparaît plus dans aucun footer : le vert reste réservé aux
icônes de statut (Rule 121) et aux compteurs du dashboard.

### D3 — Le chargement est une info du footer, la table reste à l'écran

Le corps rend la table quelle que soit la situation. Un `datatable` vide rend son
en-tête et ses lignes vides : la mise en page ne bouge plus entre le chargement
et les données.

Les vues passent `Status{Text: "Loading images…", Spinner: true}` tant que
`m.loading && len(items) == 0`. Le message « No X found » est **gardé sur la
condition de chargement** — sinon la table annonce l'absence de ce qu'elle est en
train de charger, ce qu'elle fait déjà pour `networks` et `volumes` au premier
frame.

La frame du spinner est poussée par `SetSpinnerFrame` depuis le handler
`spinner.TickMsg` que chaque vue possède déjà. C'est la frame brute, jamais
`spinner.View()` : le style appartient au footer (même raison que
`security/inventory.go:236`).

### D4 — Les trois niveaux ont une définition, pas une intuition

| Niveau | Sens | Exemples relevés |
|---|---|---|
| **Error** | une opération a échoué ou le système a refusé | `Failed to save — check logs`, `Prune failed`, `Delete failed`, `Failed to kill PID %s`, `Scan failed for %s` |
| **Warning** | l'action ne peut pas être honorée telle que demandée, mais rien n'a échoué | `Scan already in progress`, `Already killing PID`, `Not supported in Trivy client-server mode`, `Nothing selected — press space…`, `No PID available for this entry`, `All images are already scanned`, `Only Gitleaks findings can be added to .gitleaksignore`, `No stored result for %s`, `Terminal not detected`, `No registries configured` |
| **Info** | un fait neutre ou une opération réussie | `Image pulled: %s`, `Process %s terminated`, `Added %s to .gitleaksignore`, `GitLab URL changed — sign in again with :gla`, `Paused — press space to resume`, les lignes de progression et les hints |

Ce tableau est le contenu de la phase 5 ; il est dans le plan pour être discuté
**avant** d'être appliqué, parce que c'est le seul endroit où le plan change ce
que l'application dit, et pas seulement comment elle le dit.

## 4. Phases

### Phase 1 — Thème
- `colors.go` : les trois variables + peuplement dans `ApplyTheme`.
- `styles.go` : `FooterInfoStyle`, `FooterWarnStyle`, `FooterErrorStyle`, et un
  style de spinner de footer réutilisant `SpinnerStyle()`.
- Test : les trois couleurs suivent un `ApplyTheme` d'un thème qui déclare des
  severity différentes (le thème par défaut les fait coïncider avec
  `ColorError`/`ColorWarn`, donc un test sur le défaut ne prouverait rien).

### Phase 2 — Le composant
- `internal/ui/components/footer_message.go` + `footer_message_test.go`.
- Tests : mapping niveau→style ; ligne toujours pleine largeur ; centrage ;
  précédence erreur > warning > info > status ; `ClearFooterMsg` d'un id périmé
  n'efface rien ; `Status{Spinner:true}` rend la frame puis le texte ; un texte
  plus large que le viewport est tronqué (`theme.TruncateWidth`) et non replié —
  une ligne de footer qui passe à deux lignes casse l'arithmétique de Rule 124.

### Phase 3 — Migration des vues (une vue = un commit)
Ordre du plus simple au plus intriqué : `viewer`, `security`, `configuration`,
`containers`, `workspaces`, `gitlab/explorer`, `oci_resources`, `netdiag`.

Pour chacune : remplacer les champs `footerError`/`footerInfo`/`errorMsg`/
`infoMsg`/`statusMessage` par un `components.FooterMessage`, brancher
`Handle(msg)` dans `Update`, rendre via `View(width, status)`, et supprimer
la minuterie locale (`clearFooterCmd`, `clearFooterMsgCmd`, `clearInfoMsgCmd`,
`portsClearFooterCmd`) et le helper local (`centeredInfo`, `renderInfoText`,
`highlightLine`).

Deux points particuliers :
- **`security`** : supprimer aussi le doublon du viewport (`view.go:48-51`) — le
  footer porte le message, le corps n'a pas à le répéter (esprit de Rule 134).
- **`netdiag`** : les trois modèles gardent chacun leur `FooterMessage` ;
  `header.go` continue de choisir celui de l'onglet actif. Une seule instance
  partagée ferait survivre un message d'un onglet à l'autre.

### Phase 4 — Le chargement passe au footer
Les sept sites du §2.3 : retirer le bloc `SpinnerMessage` du corps, rendre la
table, garder le « No X found » derrière la condition de chargement, et alimenter
`Status{Spinner:true}`. `GetFooterHeight()` est inchangé (la ligne d'info est
déjà toujours rendue).

### Phase 5 — Reclassement des messages
Appliquer le tableau D4 : `Info(…)` → `Warn(…)` sur les sites listés. Aucune
autre modification de texte.

### Phase 6 (optionnelle) — `netdiag/topology`
Le chargement de la topologie est un `viewport`, pas un `datatable`. Même
traitement pour la cohérence, ou laissé tel quel — à décider.

### Phase 7 — Garde-fous et docs
- **Test source-level**, sur le modèle des tests de `keymap` (parse AST de
  `internal/ui/**/*.go`) : `TestNoViewStylesItsOwnFooterMessage` échoue en
  nommant fichier et ligne si `StatusErrorStyle`, `StatusOKStyle`,
  `StatusWarningStyle` ou un `Align(lipgloss.Center)` littéral apparaissent dans
  une fonction dont le nom contient `Footer` ou `InfoLine`. C'est ce qui empêche
  la neuvième implémentation.
- **Test** : `TestNoDatatableViewRendersALoadingBody` — aucun appel à
  `theme.SpinnerMessage` dans les fichiers de vue listés au §2.3.
- Docs : réécriture de **Rule 128** (`.claude/rules/tui-behavior.md`) autour des
  trois niveaux, du centrage et du composant obligatoire ; ajout de la règle
  « le chargement d'une table est un message de footer » dans
  `.claude/rules/tui-tables.md` ; section « Les messages du footer » dans
  `.claude/CLAUDE.md` ; entrée **§3.29** dans `docs/backlog.md`.

## 5. Fichiers touchés

| Fichier | Action |
|---|---|
| `internal/ui/theme/colors.go`, `manager.go`, `styles.go` | UPDATE — trois alias, trois styles |
| `internal/ui/components/footer_message.go` | CREATE |
| `internal/ui/components/footer_message_test.go` | CREATE |
| `internal/ui/{viewer,security,configuration,containers,workspaces}/{model,update,view}.go` | UPDATE |
| `internal/ui/gitlab/explorer/{model,update,view,clone,delete,messages}.go` | UPDATE |
| `internal/ui/oci_resources/{model,view,images,registries,resources,browser_bridge,browser_view}.go` | UPDATE |
| `internal/ui/netdiag/{model,header,update,ports_model,topology_model,run}.go` | UPDATE |
| `internal/ui/status/view.go` | UPDATE — chargement au footer |
| `internal/app/footer_test.go` | CREATE — les deux tests de garde |
| ~15 `*_test.go` de vues | UPDATE — assertions de footer |
| `.claude/rules/tui-behavior.md`, `tui-tables.md`, `.claude/CLAUDE.md`, `docs/backlog.md` | UPDATE |

## 6. Risques

| Risque | Probabilité | Mitigation |
|---|---|---|
| Rule 124 : une ligne de footer qui se replie décale tout le viewport | Moyenne | `View` tronque à la largeur (`theme.TruncateWidth`) et un test l'exige |
| Rule 115 : un fond manquant sur la ligne centrée montre le fond du terminal | Moyenne | Un seul rendu, `Background(ColorBackground)` + `Width(width)`, testé une fois |
| Les tests de vues cassent en nombre | **Élevée** | Attendu : migration par vue, un commit par vue, `go test ./internal/ui/<vue>` à chaque étape |
| Le reclassement info→warning change ce que l'app dit | Moyenne | Isolé en phase 5, tableau D4 validé avant application |
| `netdiag` a trois modèles avec le même champ | Faible | Un `FooterMessage` par modèle, `header.go` inchangé dans sa logique de sélection |
| Régression visuelle non couverte par les tests | Moyenne | Vérification manuelle par vue avec `mise run dev` |

## 7. Validation

```bash
mise run fmt
mise run vet
mise run lint          # obligatoire avant commit (Rule 301)
mise run test
mise run test-race     # Rule 110 — le composant est touché depuis Update
mise run build
```

## 8. Acceptation

- [ ] Aucun message de footer n'est aligné à gauche
- [ ] Aucun message de footer n'est vert
- [ ] Les trois niveaux existent, avec les couleurs demandées
- [ ] Le chargement d'une table apparaît dans le footer, avec spinner, et la
      table reste à l'écran
- [ ] Un seul rendu de message de footer dans toute l'application, et un test
      qui échoue si un neuvième apparaît
- [ ] `mise run check` passe
- [ ] Rules 124, 128, 115 et 110 tiennent
