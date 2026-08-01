# TUI — Index des règles

Les règles TUI sont réparties dans 5 fichiers thématiques :

| Fichier | Règles | Contenu |
|---------|--------|---------|
| [`tui-layout.md`](tui-layout.md) | 101, 107, 108, 111, 112, 123, 124, 130, 134, 137, 138 | Layout général, keybindings, viewport, tabs, shortcuts header, format descriptions, shortcuts obviousness |
| [`tui-theme.md`](tui-theme.md) | 102, 105, 115, 117, 118, 119, 127, 129 | Couleurs, background lipgloss, helpers `theme.*`, langue |
| [`tui-forms.md`](tui-forms.md) | 103, 104, 113, 114, 120, 121, 131, 132, 133, 135 | Formulaires, modales, focus, icônes de statut, cycle de valeurs, WrappedInput, assignation des touches |
| [`tui-tables.md`](tui-tables.md) | 106, 116, 122, 125 | Tables, largeurs de colonnes, cellules texte brut |
| [`tui-behavior.md`](tui-behavior.md) | 109, 110, 126, 128 | Bubble Tea (Cmd/Update), cache de scan, messages footer avec timer |

## Règles critiques (résumé)

**Rule 110** — Ne JAMAIS modifier le modèle dans un `Cmd` → race condition. Seul `Update()` modifie le modèle.

**Rule 122** — Jamais de `style.Render(...)` dans `table.Row{}` → artefacts visuels sur toutes les lignes suivantes.

**Rule 129** — Tout texte UI et tous les logs en **English US**.

**Rule 133** — `bubbles/textarea` est interdit. Utiliser `components.WrappedInput` pour tout champ texte multi-lignes.
