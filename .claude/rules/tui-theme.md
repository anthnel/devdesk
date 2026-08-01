# TUI — Thème, Couleurs & Background

### Rule 102 : Gestion centralisée des couleurs et styles

- Toutes les couleurs doivent être définies dans `internal/ui/theme/colors.go`
- Ne jamais hardcoder de couleurs directement dans les vues
- Les styles réutilisables doivent être définis dans `internal/ui/theme/styles.go`
- Utiliser les constantes du package theme (ColorPrimary, ColorError, etc.)

### Rule 105 : Format des messages d'aide

- Format standard : `[touche] action  [touche] action`
- Affichés avec `theme.HelpStyle` (italique + ColorDim)
- Touches entre crochets `[]`
- Séparés par au moins 2 espaces

### Rule 115 : Gestion du background uniforme (lipgloss)

**lipgloss ne propage PAS le background d'un parent vers ses enfants.**

`JoinHorizontal`/`JoinVertical` insèrent des espaces "nus" qui affichent le fond natif du terminal.

| Interdit | Alternative |
|----------|-------------|
| `lipgloss.JoinHorizontal` pour assembler des blocs | Construire chaque ligne manuellement avec `PadWithBg()` |
| `lipgloss.JoinVertical` pour empiler des lignes | `strings.Join(lines, "\n")` avec chaque ligne paddée |
| `strings.Repeat("\n", n)` pour les lignes vides | `theme.EmptyLineBg(width)` |
| `lipgloss.NewStyle().Render(text)` sans background | `.Background(ColorBackground)` obligatoire |
| Couleur hex hardcodée | Variables globales (`ColorBackground`, `ColorDim`, etc.) |

`JoinHorizontal`/`JoinVertical` acceptables **uniquement** si le résultat est wrappé dans un style `.Background(ColorBackground).Width(w)`.

`lipgloss.Place` → toujours utiliser `WithWhitespaceBackground(theme.ColorBackground)`.

### Rule 117 : Helpers centralisés du package `theme`

**Ne jamais recréer `bg()`, `padWithBg()`, `bgWrap()` localement.**

| Fonction | Usage |
|----------|-------|
| `theme.Bg(s)` | Texte plain avec background app |
| `theme.PadWithBg(content, width)` | Pad jusqu'à `width` avec background |
| `theme.BgLine(s, width)` | Texte + background + pad jusqu'à `width` |
| `theme.BgWrap(s, width)` | Background + pad sur chaque ligne (multi-ligne) |
| `theme.EmptyLineBg(width)` | Ligne vide remplie de background |
| `theme.OverlayBoxStyle()` | Style standard pour les modales overlay |

- **Texte inline** → `theme.Bg("texte")`
- **Ligne complète** → `theme.BgLine("texte", width)`
- **Paragraphe multi-ligne** → `theme.BgWrap(text, width)`
- **Ligne vide** → `theme.EmptyLineBg(width)`
- **Modal overlay** → `theme.OverlayBoxStyle().Render(content)`

### Rule 118 : Uniformisation des couleurs de tableaux et tabs

| Élément | Foreground | Background |
|---------|-----------|------------|
| Header de table | `ColorSecondary` | — |
| Ligne sélectionnée (normal) | `ColorBlack` | `ColorSecondary` |
| Ligne sélectionnée (erreur) | `ColorBlack` | `ColorError` |
| Tab actif | `ColorBlack` | `ColorSecondary` |
| Tab inactif | `ColorDim` | `ColorCommandLineBg` |

Fonctions centralisées obligatoires :
- `theme.DefaultTableStyles()` — style de base
- `theme.TableStylesForState(state)` — `"normal"` ou `"error"`
- `theme.TableStylesForSeverity(severity)` — `"CRITICAL"`, `"HIGH"`, `"MEDIUM"`, `"LOW"`, `"UNKNOWN"`

Interdit :
- ❌ Styles de sélection définis localement dans les vues
- ❌ `ColorPrimary` pour les tabs actifs ou headers (utiliser `ColorSecondary`)

### Rule 119 : Aucune couleur hex en dur en dehors de `colors.go`

**`lipgloss.Color("#...")` ne doit apparaître QUE dans `internal/ui/theme/colors.go`.**

Architecture en 3 niveaux :

| Niveau | Fichier | Rôle |
|--------|---------|------|
| **1. Palette de base** | `colors.go` lignes 6-32 | Couleurs hex brutes (Catppuccin) |
| **2. Couleurs sémantiques** | `colors.go` lignes 34+ | Alias par usage, référencent la palette |
| **3. Styles** | `styles.go`, vues | Styles lipgloss utilisant les couleurs sémantiques |

Si une couleur hex est nécessaire sans équivalent sémantique :
1. Ajouter dans la palette de base (`colors.go`, section hex)
2. Créer un alias sémantique qui référence la palette

```go
// ✅ CORRECT
var ColorMaroon = lipgloss.Color("#eba0ac")  // dans palette de base
ColorSeverityHigh = ColorMaroon              // dans section sémantique

// ❌ INTERDIT
ColorSeverityHigh = lipgloss.Color("#eba0ac")  // hex en dur dans sémantique
```

Les fallbacks de `manager.go` → référencer `colors.go`, jamais des hex.

### Rule 127 : Temps relatif — `theme.TimeAgo`

**Toutes les vues affichant un temps relatif doivent utiliser `theme.TimeAgo(t time.Time) string`** (`internal/ui/theme/timeago.go`).

| Durée | Sortie |
|-------|--------|
| < 1 min | `"now"` |
| < 1 heure | `"59 min ago"` |
| < 24 h | `"23 hr ago"` |
| < 30 jours | `"30 days ago"` |
| < 12 mois | `"11 mo ago"` |
| else | `"99 yr ago"` |
| zéro | `""` |

Interdit : implémentations locales, formats longs, compact sans espace.

### Rule 129 : Langue de l'interface et des logs — English US uniquement

**Tout texte UI et tous les logs doivent être en anglais américain.**

| Catégorie | Règle |
|-----------|-------|
| Labels, titres, messages UI | ✅ English US |
| Messages d'erreur (footer) | ✅ English US |
| Aide (`GetHelpContent`, `GetShortcuts`) | ✅ English US |
| Valeurs de cellules, messages vides | ✅ English US |
| Logs (`log.Printf`) | ✅ English US |
| Commentaires de code | ✅ Français ou anglais acceptés |

```go
// ✅ CORRECT
theme.SpinnerMessage(m.spinner.View(), "Loading workspaces...")
m.footerError = "Failed to refresh — check logs"

// ❌ INTERDIT
theme.SpinnerMessage(m.spinner.View(), "Chargement des workspaces...")
m.footerError = "Échec du rafraîchissement — voir les logs"
```
