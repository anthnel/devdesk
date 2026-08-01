# TUI — Tables

### Rule 106 : Standardisation des tables

- Utiliser `theme.DefaultTableStyles()` pour toutes les tables
- Headers en bold avec ColorSecondary (voir Rule 118)
- Ligne sélectionnée avec ColorHighlight
- Bordures cohérentes

### Rule 116 : Calcul des largeurs de colonnes

**La ligne sélectionnée doit s'étendre jusqu'à la bordure droite du viewport.**

`bubbles/table` applique `Padding(0, 1)` par cellule → +2 chars par colonne.

```
availableForContent = width - 2 (viewport borders) - numColumns × 2 (cell padding)
```

Pattern obligatoire :
```go
func (m *Model) resize(width, height int) {
    available := width - 2 - numCols*2

    columns[0].Width = int(float64(available) * 0.20)
    columns[1].Width = int(float64(available) * 0.30)
    columns[2].Width = int(float64(available) * 0.25)
    // Dernière colonne absorbe les erreurs d'arrondi
    columns[3].Width = available - columns[0].Width - columns[1].Width - columns[2].Width

    m.table.SetColumns(columns)
}
```

Pattern colonnes fixes + une flexible :
```go
fixedWidths := 10 + 18 + 14
flexWidth   := max(m.width - fixedWidths - 2 - numCols*2, 20)
```

Interdit :
- ❌ `usableWidth := width` sans soustraire les bordures viewport
- ❌ Colonnes en pourcentage sans ajouter le reste à la dernière
- ❌ Oublier le cell padding (`numColumns × 2`)
- ❌ `Background()` sur `s.Cell` (masque `s.Selected.Background()`)

### Rule 122 : Valeurs de cellules — texte brut OBLIGATOIRE ⚠️

**Les valeurs dans `table.Row{...}` doivent toujours être du texte brut, sans séquences ANSI.**

**Pourquoi :** `bubbles/table` appelle `runewidth.Truncate()` qui ne comprend pas les séquences ANSI. Un string stylé est tronqué au milieu d'une séquence, qui bleed sur toutes les lignes suivantes (artefacts `|`, `?`, etc.).

| Interdit | Alternative |
|----------|-------------|
| `theme.DimStyle.Render(text)` dans `table.Row` | `text` brut |
| `theme.StatusErrorStyle.Render(icon + " error")` dans `table.Row` | `icon + " error"` brut |
| N'importe quel `style.Render(...)` dans `table.Row` | Texte ou icône sans `Render()` |

```go
// ✅ CORRECT
rows = append(rows, table.Row{
    name,
    theme.IconError + " error",  // texte brut, pas de Render()
    "7 mins ago",
})
m.myTable.SetStyles(theme.TableStylesForState("error"))  // styling via SetStyles()

// ❌ INTERDIT
rows = append(rows, table.Row{
    name,
    theme.StatusErrorStyle.Render(theme.IconError + " error"),  // DANGER
    theme.DimStyle.Render("scanning"),                          // DANGER
})
```

Checklist :
- [ ] Aucun `style.Render(...)` dans les valeurs de `table.Row{}`
- [ ] Icônes de statut en texte brut : `theme.IconError + " error"`
- [ ] Spinners en texte brut : `frame + " scanning"`
- [ ] Styling de ligne via `SetStyles()` ou `TableStylesForState/Severity()`

### Rule 125 : Alignement des colonnes d'icônes

**Toute colonne d'icône (ou icône + texte court) doit être alignée à gauche.**

Les icônes Nerd Font ont une largeur variable selon le terminal. L'alignement à gauche est le seul qui garantit un rendu cohérent.

| Type de contenu | Alignement |
|-----------------|------------|
| Icône seule | **Gauche** |
| Icône + texte court | **Gauche** |
| Texte alphanumérique | Gauche (défaut) |

Interdit :
- ❌ `.Align(lipgloss.Center)` sur une colonne d'icônes
- ❌ `.Align(lipgloss.Right)` sur une colonne d'icônes

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
- ✅ `filterBar.ExtraHeight()` must be added to `GetFooterHeight()` return value
- ✅ `filterBar.View()` must be prepended in `RenderFooter()` when `filterBar.IsVisible()`
- ✅ `filterBar.InEditMode()` must propagate via the view's `InEditMode()` method
- ✅ `"/"` key must always call `filterBar.ActivateSearch()`
- ❌ Filter bar must NOT be rendered inside the viewport (`View()` / `renderNormalView()`)
- ❌ `filterBar.ExtraHeight()` must NOT be subtracted from table height (app router handles it)
- ❌ `filterInput textinput.Model` fields in view models (use FilterBar instead)
