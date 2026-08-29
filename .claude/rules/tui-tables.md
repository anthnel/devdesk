# TUI — Tables

### Rule 106 : Standardisation des tables

- Utiliser `theme.DefaultTableStyles()` pour toutes les tables
- Headers en bold avec ColorSecondary (voir Rule 118)
- Ligne sélectionnée avec ColorHighlight
- Bordures cohérentes

### Rule 116 : la ligne rendue occupe exactement l'intérieur du viewport

**La ligne sélectionnée doit s'étendre jusqu'à la bordure droite du viewport.**

Le calcul appartient à `internal/ui/datatable` et à lui seul. Une vue passe la
**largeur complète du viewport, bordures comprises** — `Resize` en retire les
bordures et le padding que `bubbles/table` ajoute par cellule (`Padding(0, 1)`,
donc +2 par colonne rendue).

```go
func (m *Model) resize(width, height int) {
    m.table.Resize(width, max(height-1, 1)) // pas de -2, pas de max(…, 20)
}
```

Soustraire les bordures une seconde fois est le défaut le plus discret de la
règle : la table est alors correcte à *toutes* les largeurs et se termine deux
cellules trop tôt à toutes aussi. Un plancher local (`max(…, 20)`) est l'erreur
symétrique — il rend la table plus large que le viewport quand le terminal est
étroit, ce qui est précisément le débordement interdit.

#### L'invariant s'énonce sur la portée **rendue**

```go
// ✅ CORRECT
if got, want := m.table.RenderedWidth(), width-2; got != want { … }

// ❌ INTERDIT — faux dès qu'une colonne est retirée
total := 0
for _, col := range m.table.Table().Columns() { total += col.Width }
if want := width - 2 - len(cols)*2; total != want { … }
```

Une colonne retirée faute de place ne rend rien — ni en-tête, ni cellule, ni
padding — et rend ses deux cellules au budget. La somme des colonnes déclarées
plus deux chacune demande donc moins que ce que la ligne occupe, et échoue sur
une mise en page correcte. C'est D61 vu de l'autre côté.

#### Chaque colonne déclare sa nature (§3.45)

`Sizing` n'a **pas** de valeur par défaut : `SizingFixed` (largeur exacte) ou
`SizingContent` (la largeur du contenu, plancher `MinWidth`, plafond
`MaxWidth`). `TestEveryColumnDeclaresItsSizing` parcourt les sources et échoue
en nommant fichier, ligne et colonne.

`MinWidth` est un **plancher**, pas une demande. Ce qui cède au-delà est une
colonne **entière** : les `Optional` d'abord, la plus à droite en premier, puis
les autres si besoin. Mieux vaut moins de colonnes justes que toutes fausses —
tronquer une colonne de comptage rendrait `142` en `14…`, et rien à l'écran ne
distingue les deux.

Interdit :
- ❌ Calculer des largeurs de colonnes dans une vue
- ❌ Soustraire les bordures avant `Resize`, ou clamper la largeur passée
- ❌ Vérifier Rule 116 en sommant les colonnes déclarées
- ❌ Laisser une colonne sans `Sizing`
- ❌ `Background()` sur `s.Cell` (masque `s.Selected.Background()`)

### Rule 122 : `Cell` mesure, `Style` colore — jamais l'inverse ⚠️

**`Cell` retourne du texte brut, sans séquence ANSI. La couleur passe par `Style`,
et par rien d'autre.**

**Pourquoi.** Une cellule est mesurée et tronquée *avant* d'être habillée, et la
mesure passe par `runewidth`, qui compte les octets d'une séquence d'échappement
comme de la largeur. Une chaîne de 7 cellules visibles portant une couleur mesure
28 : elle est donc tronquée dans une colonne deux fois assez large, et la coupe
tombe *à l'intérieur* de l'échappement — `"\x1b[38;2;166;2…"`. La séquence non
terminée bave ensuite sur toutes les lignes suivantes.

C'est une limitation de `bubbles/table`, pas de Bubble Tea ni de lipgloss, et
elle est inchangée dans `bubbles v1.0.0`. `internal/ui/datatable` rend donc ses
propres lignes (`render.go`) : le texte est mesuré tant qu'il est brut, la
couleur est appliquée après. **L'interdit porte donc sur `Cell`, pas sur la
couleur.**

```go
// ✅ CORRECT — le texte est mesurable, la couleur est décidée à part
{
    Title: "Severity", MinWidth: 10,
    Cell:  func(f scan.Finding) string { return string(f.Severity) },
    Style: func(f scan.Finding) lipgloss.Style {
        return theme.SeverityTextStyle(string(f.Severity))
    },
}

// ❌ INTERDIT — la couleur entre dans ce qui sera mesuré
{
    Cell: func(f scan.Finding) string {
        return theme.SeverityTextStyle(string(f.Severity)).Render(string(f.Severity))
    },
}
```

#### Ligne sélectionnée

**`Style` n'est pas consulté pour la ligne sous le curseur.** Elle est passée
entière à `styles.Selected`, et une couleur à l'intérieur se referme par un
reset qui emporte le fond de sélection pour tout le reste de la ligne : le
surlignage s'arrêterait au milieu. Le surlignage répond à « où suis-je », et
aucune couleur de colonne ne vaut de le perdre. Une colonne ne peut pas demander
l'inverse.

#### Fond

lipgloss n'hérite pas d'un fond (Rule 115), et le style du viewport ne couvre
que les cellules qui n'émettent rien. `render.go` donne donc un fond explicite à
**chaque** cellule d'une ligne non sélectionnée, colorée ou non — sinon une
seule cellule colorée dépouillerait de son fond tout ce qui la suit. Une colonne
qui ne déclare qu'un `Foreground` reçoit `ColorBackground` automatiquement.

#### Discipline de couleur

Une couleur qui apparaît partout n'informe de rien :

- un compteur à `0`, un `-`, une valeur absente → `theme.DimStyle` ;
- l'état nominal et majoritaire (un conteneur `running`) → couleur de texte par
  défaut, **pas** de vert ;
- la couleur est réservée à ce qui mérite d'être repéré sans lire.

**L'exception, et ce qui la définit : une colonne où l'absence de couleur est
déjà prise.** La colonne `CI` a trois absences — jamais scanné, non gradable,
score retenu — et les trois rendent en `DimStyle`. Un `A` en couleur de texte
ordinaire ne s'y distingue d'un `-` gris que par une nuance, sur quatre
cellules. Le vert y sépare donc **une note d'une absence**, pas deux valeurs
nominales l'une de l'autre : c'est ce que la règle ci-dessus interdit ailleurs,
et ce qu'elle demande ici. Le critère est celui-là et pas « c'est important » —
si les absences d'une colonne se distinguaient déjà, le vert redeviendrait du
bruit.

Checklist :
- [ ] Aucun `style.Render(...)` dans ce que retourne `Cell`
- [ ] Icônes de statut en texte brut : `theme.IconError + " error"`
- [ ] Spinners en texte brut : `frame + " scanning"`
- [ ] Couleur par cellule via `Style`, jamais via `Cell`
- [ ] Couleur de la ligne sélectionnée via `SelectedStyles` / `TableStylesForState/Severity()`
- [ ] Les zéros et les placeholders sont `DimStyle`, pas colorés

### Rule 125 : Alignement des colonnes d'icônes

**Toute colonne d'icône (ou icône + texte court) doit être alignée à gauche.**

Les icônes Nerd Font ont une largeur variable selon le terminal. L'alignement à gauche est le seul qui garantit un rendu cohérent.

| Type de contenu | Alignement |
|-----------------|------------|
| Icône seule | **Gauche** |
| Icône + texte court | **Gauche** |
| Texte alphanumérique | Gauche (défaut) |

#### Une icône en première colonne **est** une colonne

Quand la première colonne d'un `datatable` porte un glyphe, elle est une
colonne à part entière — pas un préfixe collé dans la cellule de texte voisine.

| | |
|---|---|
| Titre | **vide**. Le glyphe se lit d'un coup d'œil ; un en-tête nommerait ce qui n'a pas besoin de l'être, et `eza` n'en met pas non plus. |
| Largeur | `datatable.IconColumnWidth` (2) : le glyphe, plus une cellule pour qu'il ne touche pas le texte. Deux et non une — un glyphe Nerd Font rend en double largeur sur certains terminaux, et une seule cellule le tronquerait là. |
| `Sizing` | `SizingFixed`. Rien à mesurer. |
| `Less` / `Search` | **aucun**. Elle n'ajoute aucun texte que quelqu'un puisse taper, donc le filtre reste sur les colonnes de noms ; et un comparateur coûterait deux cellules de plus pour loger sa flèche de tri. |

```go
// ✅ CORRECT — le glyphe a sa colonne
{
    Title: "", Sizing: datatable.SizingFixed, MinWidth: datatable.IconColumnWidth,
    Cell: func(r row) string { return r.icon() },
},
{
    Title: "Name", Sizing: datatable.SizingContent, MinWidth: 16,
    Cell:   func(r row) string { return r.Name },
    Search: func(r row) string { return r.Name },
},

// ❌ INTERDIT — le glyphe collé dans la colonne identifiante
{
    Title: "Name", Sizing: datatable.SizingContent, MinWidth: 18,
    Cell: func(r row) string { return theme.IconDocker + " " + r.Name },
},
```

**Ce que le collage coûte**, et c'est ce qui décide : la colonne identifiante
dépense sa largeur pour ce qui n'est pas le nom, et un `SizingContent` mesure
alors le glyphe avec — donc la colonne la plus disputée de la table réserve
deux cellules pour une icône, à toutes les largeurs.

#### La couleur d'une icône passe par un **rôle**, jamais par le glyphe

Rule 122 vaut ici comme ailleurs : `Cell` rend le glyphe nu, `Style` le colore.
Ce que Rule 125 ajoute est **d'où vient la couleur** — de `theme.IconStyle(role)`,
et d'un rôle déclaré dans `theme/iconcolors.go`.

```go
// ✅ CORRECT — la vue nomme un sens, le thème répond une couleur
Cell:  func(r row) string { return nodeKindIcon(r.node) },
Style: func(r row) lipgloss.Style { return theme.IconStyle(nodeKindRole(r.node)) },

// ❌ INTERDIT — la vue décide de la teinte
Style: func(r row) lipgloss.Style {
    return lipgloss.NewStyle().Foreground(theme.ColorSecondary)
},
```

**Un codepoint n'est pas un nom.** Une table `U+F0849 → ColorSecondary` ne se
relit pas : rien sur la ligne ne dit si l'entrée est juste, donc une erreur y est
indiscernable d'un choix. Un rôle se discute, et c'est pour ça qu'il est la clé.

Les cinq rôles sont surchargeables par un fichier de thème
(`icon_namespace`, `icon_repository`, `icon_vis_*`) — contrairement aux couleurs
de syntaxe, qui sont des alias fermés : une icône est la première chose vue sur
une ligne, donc c'est la partie de la palette sur laquelle un utilisateur a le
plus de chances d'avoir un avis.

**Sous le curseur la couleur disparaît**, comme celle de toute colonne colorée :
la ligne sélectionnée est rendue entière par `styles.Selected` (Rule 122). Une
colonne d'icône ne peut donc pas être le *seul* porteur d'une information —
c'est pourquoi le kind de l'explorer reste lisible autrement (le glyphe hors
sélection, la position dans l'arbre dedans).

Les trois tables concernées sont `ws`, `containers` et l'inventaire `:sec`, et
elles déclarent la même constante. `TestAnIconColumnIsUntitledAndTwoCellsWide`
(`internal/ui/datatable`) parcourt les sources et refuse une colonne sans titre
qui invente sa largeur. Il ne voit pas l'autre moitié — une icône collée dans
une cellule de texte est indiscernable d'un nom qui commence par un glyphe —
qui reste une question de revue.

Interdit :
- ❌ `.Align(lipgloss.Center)` sur une colonne d'icônes
- ❌ `.Align(lipgloss.Right)` sur une colonne d'icônes
- ❌ Un titre au-dessus d'une colonne de glyphes
- ❌ Une largeur locale (`colIconFixed`, `statusColumnWidth`, un `2` littéral) au lieu de `datatable.IconColumnWidth`
- ❌ Un glyphe préfixé dans la cellule d'une colonne de texte
- ❌ Une couleur d'icône choisie dans la vue au lieu d'un rôle de `theme.IconStyle`
- ❌ Une table de couleurs indexée par le glyphe plutôt que par le sens

### Rule 139 : Le chargement d'une table s'affiche dans le footer

**Quand un `datatable` charge ses données, la table reste à l'écran** et le
chargement est rendu par `components.Status{Text: "...", Spinner: true}` dans le
footer (Rule 128). Le corps ne se remplace jamais par un spinner.

**Pourquoi.** Un corps qui se substitue au tableau perd son en-tête et ses
colonnes le temps de chaque `ctrl+r`, puis les retrouve : la mise en page saute
à chaque rafraîchissement, et le tableau vide qui reste dit exactement la même
chose sans bouger.

```go
// ✅ CORRECT — la table reste, le footer parle
func (m Model) renderImagesView() string {
    if _, loading := m.loadingLabel(); loading {
        return m.imageTable.View()
    }
    if len(m.imageTable.Visible()) == 0 && !m.imageTable.FilterBar().IsVisible() {
        return theme.DimStyle.Render("No images found")
    }
    return m.imageTable.View()
}

func (m Model) status() sharedcomponents.Status {
    if text, ok := m.loadingLabel(); ok {
        return sharedcomponents.Status{Text: text, Spinner: true}
    }
    return sharedcomponents.Status{Text: m.actionLine()}
}

// ❌ INTERDIT — le corps se remplace par un spinner
if m.loading && len(m.images) == 0 {
    return theme.SpinnerMessage(m.spinner.View(), "Loading images...")
}
```

**Le message vide est conditionné à la fin du chargement.** Sans cette garde, la
table annonce l'absence de ce qu'elle est en train de chercher — ce que les
onglets Networks et Volumes faisaient à la première frame.

La frame du spinner est poussée depuis le handler `spinner.TickMsg` :
`m.footer.SetSpinnerFrame(m.spinner.View())`. Sans cet appel le spinner reste sur
la frame zéro, ce qui se lit comme un blocage.

`TestNoTableViewRendersALoadingBody` (`internal/ui/components`) refuse un
`theme.SpinnerMessage` dans les vues à table. Un écran d'opération sans table
derrière (un `docker pull` en cours) est une exception **déclarée dans le test**.

### Rule 136 : Filter Bar for Tables

**Every table view must use `components.FilterBar` for search and toggle filters. No ad-hoc filter implementations.**

#### Behaviour

| State | Bar visible | Description |
|-------|-------------|-------------|
| No filter | ❌ | Bar is completely hidden |
| Toggle filter active | ✅ | Bar shows dimmed placeholder + active token(s) |
| Search mode (after `/`) | ✅ | Bar shows focused text input + active token(s) |
| Search confirmed (query set) | ✅ | Bar shows dimmed placeholder + query token + toggle tokens |

#### Visual

The filter bar forms a **closed rectangle** that visually connects with the viewport's bottom border:

```
┌─────────────────────────────────────────┐   ← viewport top border
│  table rows...                          │
└─────────────────────────────────────────┘   ← viewport bottom border (= rectangle top)
│  / cursor...         [tcp]  ·  [LISTEN] │   ← content line with │ sides
└─────────────────────────────────────────┘   ← rectangle bottom border
```

Both lines use `theme.ColorViewportBorder` for `│`, `└`, `┘`, `─` characters.

#### Placement — Footer, not viewport

The filter bar is rendered in `RenderFooter()`, **not** in the viewport content.

- `GetFooterHeight()` returns `baseFooterHeight + filterBar.ExtraHeight()`
- `RenderFooter()` prepends `filterBar.View()` when `filterBar.IsVisible()`
- Table height formula does **not** subtract `filterBar.ExtraHeight()` — the app router handles that via `GetFooterHeight()`

#### Integration Pattern

```go
// In Model struct
filterBar components.FilterBar  // or components.NewFilterBarWithTokens(tokens)

// In GetFooterHeight()
func (m Model) GetFooterHeight() int {
    return 2 + m.filterBar.ExtraHeight() // base (empty line + info) + filter bar
}

// In RenderFooter()
func (m Model) RenderFooter(width int) string {
    var parts []string
    if m.filterBar.IsVisible() {
        parts = append(parts, m.filterBar.View())
    }
    parts = append(parts, theme.EmptyLineBg(width), infoLine)
    return strings.Join(parts, "\n")
}

// In resize() / WindowSizeMsg handler — NO ExtraHeight subtraction
tableHeight := m.height - overhead  // overhead = header row only, NOT filterBar.ExtraHeight()
m.table.SetHeight(tableHeight)

// In Update() / key handler — forward keys when in search mode
if m.filterBar.InEditMode() {
    m.filterBar, cmd = m.filterBar.Update(msg)
    m.applyFilters()
    return m, cmd
}
// "/" key activates search
case "/":
    return m, m.filterBar.ActivateSearch()

// In InEditMode() — propagate to app router
func (m Model) InEditMode() bool {
    return m.filterBar.InEditMode() || /* other edit states */
}

// In View() / renderNormalView() — table only, NO filter bar here
func (m Model) renderNormalView() string {
    return m.table.View()
}

// In applyFilters() — use filterBar.SearchQuery() for text filtering
query := strings.ToLower(m.filterBar.SearchQuery())
```

#### Toggle filter tokens (views with dedicated filter keys)

Use `components.NewFilterBarWithTokens()` and update token active state when toggle keys are pressed:

```go
// Init
fb := components.NewFilterBarWithTokens([]components.FilterToken{
    {Label: "tcp"},
    {Label: "udp"},
})

// On key "t"
fb.SetTokenActive("tcp", !fb.IsTokenActive("tcp"))
```

#### Rules

- ✅ Use `components.FilterBar` — never re-implement the filter bar locally
- ✅ Le cadre lui-même est `components.BarFrame(width, inner)` : c'est ce que
  `FilterBar.View()` appelle, et la seule voie pour ce qui partage ce créneau
  sans être un filtre — le prompt d'aller-à-la-ligne du viewer (§3.53). Deux
  implémentations du rectangle seraient libres de diverger sur l'endroit où sont
  les coins.
- ✅ Un seul occupant du créneau à la fois, et la hauteur ne dépend pas duquel —
  sinon le panneau se redimensionne sous le lecteur quand l'un s'ouvre par-dessus
  l'autre.
- ✅ `filterBar.ExtraHeight()` must be added to `GetFooterHeight()` return value
- ✅ `filterBar.View()` must be prepended in `RenderFooter()` when `filterBar.IsVisible()`
- ✅ `filterBar.InEditMode()` must propagate via the view's `InEditMode()` method
- ✅ `"/"` key must always call `filterBar.ActivateSearch()`
- ❌ Filter bar must NOT be rendered inside the viewport (`View()` / `renderNormalView()`)
- ❌ `filterBar.ExtraHeight()` must NOT be subtracted from table height (app router handles it)
- ❌ `filterInput textinput.Model` fields in view models (use FilterBar instead)
