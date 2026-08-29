# Plan : la vue explorer passe aux icônes (§3.56)

**Demande** : remplacer la colonne `Type` par une icône, donner une icône à
`Visibility`, réordonner les colonnes, et enrichir le thème pour colorer les
icônes séparément.
**Complexité** : Medium — 1 worktree, ~10 fichiers, 3 décisions à trancher.

## Ce qui est demandé, relu

| # | Demande | Traduction |
|---|---|---|
| 1 | La colonne `Type` devient une icône | Colonne sans titre, 2 cellules, `datatable.IconColumnWidth` (Rule 125) |
| 2 | Les icônes de `exp` diffèrent de celles de `ws` | `ws` utilise `IconGitBranch` et `IconDirectory` — l'explorer prend deux glyphes neufs |
| 3 | `Visibility` porte une icône | public / internal / private pour GitLab, public / private pour GitHub |
| 4 | Ordre : Icône, Name, Slug, Visibility, Role, CI, Created, Activity | `CI` remonte de la fin à après `Role` |
| 5 | Le thème sait colorer chaque icône | Rôles d'icône nommés + clés de thème, sur le modèle des couleurs de sévérité |

## L'état actuel

`internal/ui/forge/explorer/columns.go` déclare huit colonnes :
`Type(13) · Name · Slug · Visibility(12) · Role · Created · Activity · CI`.
`Type` rend `nodeTypeLabel(v, node)` — « Group » / « Project » ou
« Organization » / « Repository » selon la forge — et **porte la case à cocher**
du mode clone (`row.go:checkboxIcon`). `SortColumn: columnType` (0) ouvre la
table triée par type, ce qui met les groupes avant les projets.

## Les trois décisions

### D1 — Où va la case à cocher du mode clone ?

`Type` disparaît, et c'est elle qui portait la case. Rule 125 interdit une
colonne d'icône plus large que deux cellules, et
`TestAnIconColumnIsUntitledAndTwoCellsWide` le vérifie — donc « case + glyphe »
côte à côte est exclu.

**Retenu** : en `ModeSelecting` la colonne d'icône rend la **case**, et sa
**couleur reste celle du kind**. C'est exactement ce que le mécanisme demandé en
(5) rend possible : la colonne dit l'état de sélection par la forme et le
groupe-ou-dépôt par la teinte, donc rien n'est perdu.

| Rejeté | Pourquoi |
|---|---|
| Élargir la colonne à 4 pendant la sélection | échoue au test de Rule 125, et `C` décalerait toute la table |
| Préfixer la case dans `Name` | interdit par Rule 125 (« un glyphe préfixé dans la cellule d'une colonne de texte ») |
| Une colonne de case dédiée | déjà rejeté en §3.16 : quatre cellules sur tous les écrans pour n'en servir qu'un |

### D2 — Que devient l'en-tête de `Visibility` ? — **tranché**

La cellule ne fait plus qu'un glyphe, mais l'en-tête réserve
`max(MinWidth, len(Title)+2)` dès que la colonne trie (`widths.go:askFor`) —
donc « Visibility » la fige à 12 cellules pour afficher un caractère.

**Retenu** : le titre reste `Visibility`, et c'est le **comparateur** qui part.
Sans `Less`, `askFor` rend `MinWidth` tel quel : la colonne descend à 10, la
largeur exacte de son propre en-tête, et gagne deux cellules sans que le lecteur
ait à décoder un titre abrégé. Un glyphe de visibilité sous un mot entier n'a
besoin d'aucune légende.

Ce qu'on paie : `.` ne trie plus par visibilité. C'est déjà le cas de `Slug`,
`Role` et `CI` dans cette table — trois valeurs dans un ordre que personne ne
reconnaîtrait (public avant private ? l'inverse ?) ne font pas un tri, et la
colonne reste filtrable par le nom comme avant.

### D3 — Par quoi la table est-elle triée à l'ouverture ?

La colonne d'icône ne déclare pas de `Less` (Rule 125), donc `SortColumn: 0`
n'est plus tenable — `datatable.New` le ramènerait à `-1` en silence.

**Retenu** : `SortColumn: -1`, l'ordre de la forge. **Ce n'est pas une
régression** : `loadChildren` empile les namespaces puis les dépôts, et un tri
stable par `string(node.Type)` ascendant sur cette liste est l'identité. L'écran
d'ouverture est donc bit-pour-bit celui d'aujourd'hui, et `-1` devient en prime
un arrêt du cycle `.` — l'ordre de la forge redevient atteignable, ce que le tri
par type ne permettait pas.

## Le mécanisme de thème (5)

Pas de table `glyphe → couleur` : un glyphe est une valeur, pas un nom, et une
map clé-par-codepoint se lit comme du bruit en revue. On suit le précédent des
couleurs de sévérité et de `CIScoreStyle` — un **rôle nommé**, une couleur
sémantique, une fonction de style.

```go
// internal/ui/theme/iconcolors.go (nouveau)
type IconRole string

const (
    IconRoleNamespace   IconRole = "namespace"   // groupe GitLab / organisation GitHub
    IconRoleRepository  IconRole = "repository"  // projet GitLab / dépôt GitHub
    IconRoleVisPublic   IconRole = "vis-public"
    IconRoleVisInternal IconRole = "vis-internal"
    IconRoleVisPrivate  IconRole = "vis-private"
)

func IconColor(role IconRole) lipgloss.Color   // switch, défaut ColorText
func IconStyle(role IconRole) lipgloss.Style   // Foreground(IconColor(role))
```

- Cinq variables dans `colors.go` (`ColorIconNamespace`, …), assignées dans
  `ApplyTheme` **après** la palette — le motif des couleurs de syntaxe et de
  footer, sinon un `= ColorSecondary` au niveau paquet capturerait le zéro.
- Cinq clés `omitempty` dans `Theme` (`icon_namespace`, `icon_repository`,
  `icon_vis_public`, `icon_vis_internal`, `icon_vis_private`) passées par
  `applyColor(t.X, fallback)` — un fichier de thème peut donc les surcharger,
  aucun n'y est obligé.
- Rule 119 tenue : aucun hex ne sort de `colors.go`.

**Défauts proposés**, et l'argument derrière chacun :

| Rôle | Couleur | Pourquoi |
|---|---|---|
| namespace | `ColorSecondary` | la couleur de structure de l'application (en-têtes, onglets) |
| repository | `ColorPrimary` | la feuille, distincte du contenant |
| public | `ColorOK` | ouvert : c'est ce qui mérite d'être repéré sans lire |
| internal | `ColorWarn` | restreint mais pas fermé |
| private | `ColorDim` | l'état majoritaire. Rule 122 : une couleur portée par toutes les lignes n'informe de rien |

**Sous le curseur, l'icône perd sa couleur** : la ligne sélectionnée est rendue
entière par `styles.Selected` (Rule 122). C'est le comportement de toutes les
colonnes colorées de l'application, pas une exception d'ici.

## Les glyphes

Aucun ne doit heurter `ws` (`IconGitBranch`, `IconDirectory`, `fileicon`).

| Constante | Codepoint | Nerd Font | Sert à |
|---|---|---|---|
| `IconNamespace` | `U+F0849` | `nf-md-account_group` | groupe / organisation |
| `IconRepository` | `U+F401` | `nf-oct-repo` | projet / dépôt |
| `IconVisibilityPublic` | `U+F01E7` | `nf-md-earth` | public |
| `IconVisibilityInternal` | `U+F0498` | `nf-md-shield` | internal (GitLab) |
| `IconVisibilityPrivate` | — | `theme.IconLock` existant | private |

C'est la convention de GitLab et de GitHub elles-mêmes (globe / bouclier /
cadenas). **Un codepoint faux rend un tofu**, et rien dans la CI ne l'attrape :
la vérification à l'œil sur un vrai terminal est une étape du plan, pas un
détail.

## Fichiers touchés

| Fichier | Action | Quoi |
|---|---|---|
| `internal/ui/theme/icons.go` | UPDATE | 4 glyphes |
| `internal/ui/theme/colors.go` | UPDATE | 5 variables |
| `internal/ui/theme/manager.go` | UPDATE | 5 clés `Theme` + 5 `applyColor` dans `ApplyTheme` |
| `internal/ui/theme/iconcolors.go` | CREATE | `IconRole`, `IconColor`, `IconStyle` |
| `internal/ui/theme/iconcolors_test.go` | CREATE | chaque rôle a une couleur, un rôle inconnu retombe sur `ColorText`, un thème surcharge |
| `internal/ui/forge/explorer/columns.go` | UPDATE | colonne d'icône, réordonnancement, `Visibility` sans comparateur, constantes |
| `internal/ui/forge/explorer/row.go` | UPDATE | la case et le kind dans une cellule, le kind dans le style |
| `internal/ui/forge/explorer/view.go` | UPDATE | `nodeKindIcon`, `visibilityIcon`, légende d'aide, texte de `.` |
| `internal/ui/forge/explorer/model.go` | UPDATE | `SortColumn: -1` |
| `internal/ui/forge/explorer/*_test.go` | UPDATE | tris, glyphes, ordre des colonnes |
| `.claude/rules/tui-tables.md` | UPDATE | Rule 125 : la couleur d'une colonne d'icône passe par `Style` et un rôle de thème |
| `docs/architecture/forge.md` | UPDATE | l. 96-101 (la « rough edge » du mot Organization change de place), l. 188 et 212-214 (la case ne ride plus sur `Type`) |
| `docs/backlog.md` | UPDATE | §3.56 |

## Tâches

1. **Thème d'abord, seul.** Glyphes, couleurs, `iconcolors.go`, son test.
   `go test ./internal/ui/theme/` passe avant qu'une vue ne bouge.
2. **Vérifier les glyphes à l'œil** — `mise run dev`, ou un `fmt.Println` des
   cinq constantes. Un tofu ici invalide tout le reste.
3. **Les deux résolveurs de l'explorer** : `nodeKindIcon(node)` et
   `visibilityIcon(node)` dans `view.go`, à côté de `nodeTypeLabel` et
   `visibilityLabel` qui **restent** — la légende d'aide les lit.
4. **`row.go`** : `iconCell(r)` rend la case en `ModeSelecting` et le kind
   sinon ; `iconStyle(r)` rend `theme.IconStyle` du kind dans les deux cas.
5. **`columns.go`** : nouvel ordre, colonne d'icône `Title: ""` /
   `SizingFixed` / `IconColumnWidth` / ni `Less` ni `Search`, `Visibility` à 10
   sans `Less`,
   suppression de `colTypeMin` et `columnType`.
6. **`model.go`** : `SortColumn: -1`.
7. **Aide (Rule 114)** : section « Row Icons » nommant les glyphes avec
   `v.Namespace` / `v.Repository` — c'est là que le vocabulaire de la forge
   survit à la disparition de la colonne — plus la légende de visibilité, et
   le texte de `.` corrigé.
8. **Tests** : réécrire les trois tests de tri, l'absence de tri sur `Visibility`, et ajouter
   « le glyphe dit le kind », « la case remplace le glyphe en sélection »,
   « la couleur du kind survit à la sélection ».
9. **Docs et règle**, dans le même commit que le code.

## Validation

```bash
gofmt -l . && go build ./... && go vet ./... && go test ./... && golangci-lint run
go test ./internal/ui/datatable/ -run TestAnIconColumnIsUntitledAndTwoCellsWide -v
go test ./internal/ui/vocabtest/ -v
```

Plus un balayage de largeur 40→250 sur `explorerColumns` vérifiant
`RenderedWidth() == width-2` (Rule 116) — la table perd une colonne large et en
gagne une étroite, c'est exactement le cas où l'ordre d'abandon des `Optional`
change.

## Risques

| Risque | Probabilité | Parade |
|---|---|---|
| Un codepoint faux → tofu | **Haute** | tâche 2, à l'œil, avant tout le reste |
| Le kind devient invisible en mode clone | Moyenne | il passe dans la couleur de la case (D1) ; à confirmer à l'écran |
| `vocabtest` casse | Faible | les glyphes ne portent aucun nom de forge ; `nodeTypeLabel` survit dans l'aide |
| Le tri par défaut change de comportement | Faible | démontré identique ci-dessus, et un test le fige |
| Régression de largeur à 80 colonnes | Moyenne | balayage de largeur dans la validation |

## Acceptation

- [ ] Colonnes dans l'ordre : icône, Name, Slug, Visibility, Role, CI, Created, Activity
- [ ] Colonne d'icône sans titre, 2 cellules, ni `Less` ni `Search`
- [ ] Les glyphes de `exp` ne sont ceux d'aucune ligne de `ws`
- [ ] Trois visibilités distinguables sans légende, la quatrième (absente) vide
- [ ] Un fichier de thème peut redéfinir les cinq couleurs d'icône
- [ ] Aucun hex hors `colors.go`, aucun `Render` dans un `Cell`
- [ ] Aide et docs à jour dans le même commit
- [ ] `mise run check` vert
