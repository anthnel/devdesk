# TUI — Index des règles

Les règles TUI sont réparties dans 5 fichiers thématiques :

| Fichier | Règles | Contenu |
|---------|--------|---------|
| [`tui-layout.md`](tui-layout.md) | 101, 107, 108, 111, 112, 123, 124, 130, 134, 137, 138 | Layout général, keybindings, viewport, tabs, shortcuts header, format descriptions, shortcuts obviousness |
| [`tui-theme.md`](tui-theme.md) | 102, 105, 115, 117, 118, 119, 127, 129 | Couleurs, background lipgloss, helpers `theme.*`, langue |
| [`tui-forms.md`](tui-forms.md) | 103, 104, 113, 114, 120, 121, 131, 132, 133, 135 | Formulaires, modales, focus, icônes de statut, cycle de valeurs, WrappedInput, assignation des touches |
| [`tui-tables.md`](tui-tables.md) | 106, 116, 122, 125, 136, 139 | Tables, largeurs de colonnes, cellules texte brut + couleur par `Style`, filter bar, corps jamais remplacé (spinner ou message) |
| [`tui-behavior.md`](tui-behavior.md) | 109, 110, 126, 128 | Bubble Tea (Cmd/Update), cache de scan, messages footer (trois niveaux, un composant) |

## Règles critiques (résumé)

**Rule 110** — Ne JAMAIS modifier le modèle dans un `Cmd` → race condition. Seul `Update()` modifie le modèle.

**Rule 122** — `Cell` retourne du texte brut (il est mesuré), la couleur passe par `Style`. Un `style.Render(...)` dans `Cell` est tronqué au milieu de sa séquence et bave sur toutes les lignes suivantes.

**Rule 128** — Un message de footer passe par `components.FooterMessage` : trois niveaux (info neutre, warning orange, erreur rouge), toujours centrés, une seule implémentation. Le chargement d'une table s'affiche dans le footer avec un spinner, jamais dans le corps.

**Rule 139** — Le corps d'un `datatable` est toujours `m.table.View()`, jamais un spinner ni un message : une table vide (chargement, rien à lister, filtre sans résultat) reste une table — en-tête, pas de ligne. Le compte va dans `GetHeaderInfo`, le statut dans le footer.

**Rule 129** — Tout texte UI et tous les logs en **English US**.

**Rule 133** — `bubbles/textarea` est interdit. Utiliser `components.WrappedInput` pour tout champ texte multi-lignes.
