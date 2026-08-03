# TUI — Formulaires & Focus

### Rule 103 : Navigation standardisée dans les formulaires

- `↑ / ↓` pour naviguer entre les champs d'un formulaire
- Un indicateur visuel (`theme.IconCircleSmall`) doit montrer l'élément focusé
- `Enter` pour valider les boutons d'action uniquement (voir Rule 135)

### Rule 104 : Structure des fenêtres modales

- Bordure arrondie (NormalBorder) avec couleur appropriée au contexte
- Titre en haut utilisant `theme.TitleStyle`
- Message au centre
- Boutons en bas avec espacement cohérent
- Instructions d'aide avec `theme.HelpStyle` tout en bas
- Choix sécuritaire par défaut (ex: "No" pour les confirmations de suppression)

### Rule 113 : Espacement des champs de formulaire

| Layout | Séparateur après le champ |
|--------|--------------------------|
| **Multi-ligne** : libellé ↵ contenu | `\n\n` (ligne vide) |
| **Mono-ligne** : libellé + contenu sur la même ligne | `\n` |
| **Checkboxes** | `\n` entre elles, `\n\n` après le groupe |

Les fonctions `renderField()` ne doivent **PAS** inclure le séparateur final.

### Rule 114 : Système d'aide intégré (`?`)

**Chaque vue implémente `help.Provider` via `GetHelpContent()`, toujours en anglais.**

Obligations :
- Mise à jour obligatoire dans le même commit quand une fonctionnalité / keybinding change
- Toute nouvelle vue doit implémenter `help.Provider` avec un contenu complet

Contenu attendu : `Title`, `Description`, `KeyBindings`, `Sections`

Fichiers :
- `internal/ui/help/help.go` — composant et interface `Provider`
- `internal/ui/*/view.go` ou `model.go` — implémentation par vue

### Rule 120 : Indicateur de focus et rendu des champs de formulaire

**Indentation : 2 caractères.** Quand focusé, `theme.IconCircleSmall` se place à la colonne 0.

| État | Préfixe |
|------|---------|
| Focusé | `IconCircleSmall + " "` → `● Label` |
| Non focusé | `"  "` (2 espaces) → `  Label` |

`theme.IconCircleSmall` (`\ueb8a`) ne contient PAS d'espace — toujours ajouter `" "` après.

**Séparateur label/valeur** : `theme.IconChevronRight` (`\uf054`) + `" "`. **Pas de `:`**.

**Espace avant le chevron obligatoire** : il doit toujours y avoir un espace entre le texte du label et `IconChevronRight`.

```go
// ✅ Mono-ligne focusé
theme.KeyStyle.Render(theme.IconCircleSmall + " Server " + theme.IconChevronRight + " ") + input.View()

// ✅ Multi-ligne
theme.KeyStyle.Render(theme.IconCircleSmall + " Name " + theme.IconChevronRight) + "\n"
theme.Bg("  ") + input.View()

// ❌ INTERDIT — pas d'espace avant le chevron
theme.KeyStyle.Render(theme.IconCircleSmall + " Name" + theme.IconChevronRight) + "\n"
theme.KeyStyle.Render("▸ Server: ") + input.View()
```

**Listes de sélection** : `IconCircleSmall` pour l'item sélectionné, `"  "` pour les autres.

**Couleur de focus** : `theme.ColorHighlight` pour tous les types de champs.

**Cursor TextInput** : configurer via `theme.StyleTextInput(&myInput)` (met `Prompt = ""`).

**Checkboxes** : via `theme.RenderCheckbox()`.

**Il n'y a pas de radio buttons.** Pour un ensemble fermé de valeurs, le contrôle
est le champ à cycle `←→` (Rule 132) — quel que soit le nombre de valeurs, y
compris deux. `theme.RenderRadioButton()` a été supprimé avec ses deux derniers
appelants (§3.9 du backlog) ; le recréer localement est interdit.

Interdit :
- ❌ `▸` comme indicateur de focus (utiliser `theme.IconCircleSmall`)
- ❌ `:` comme séparateur label/valeur (utiliser `theme.IconChevronRight`)
- ❌ `ColorPrimary` pour les éléments focusés (utiliser `ColorHighlight`)
- ❌ Recréer `RenderCheckbox` localement
- ❌ Réintroduire des radio buttons, sous quelque forme que ce soit

### Rule 131 : Padding haut des formulaires dans le viewport

**Tout formulaire affiché dans le viewport principal doit commencer par exactement une ligne vide (padding de 1 ligne en haut).**

```go
// ✅ CORRECT — une seule ligne vide
func (f *Form) View() string {
    return theme.EmptyLineBg(f.width) + "\n" +
        f.renderFields()
}

// ❌ INTERDIT — deux lignes vides (EmptyLineBg + "\n\n")
func (f *Form) View() string {
    return theme.EmptyLineBg(f.width) + "\n\n" + f.renderFields()
}

// ❌ INTERDIT — formulaire collé au bord supérieur du viewport
func (f *Form) View() string {
    return f.renderFields()
}
```

Ne s'applique pas aux modales (positionnées par `lipgloss.Place`).

### Rule 132 : Champs à liste fermée (cycle de valeurs)

**Tout champ dont la valeur appartient à un ensemble fini doit utiliser le pattern cycle ←→.**

C'est le **seul** contrôle admis pour un ensemble fermé. Les radio buttons ne
sont pas une alternative pour deux ou trois valeurs : ils n'existent plus dans
l'application (Rule 120). Les checkboxes restent pour les booléens indépendants,
ce qui est autre chose qu'un choix exclusif.

#### Visuel

```
  Visibility  󰅂 private        ← non focusé
● Visibility  󰅂 internal       ← focusé (KeyStyle)
```

Structure de la ligne :

| Partie | Valeur |
|--------|--------|
| Indicateur de focus | `theme.IconCircleSmall + " "` (focusé) ou `"  "` (non focusé) |
| Label | texte du champ, ex. `"Visibility"` |
| Icône de sélection | `" " + theme.IconSelect + " "` |
| Séparateur | `theme.IconChevronRight + " "` |
| Valeur courante | texte brut avec `ColorText` |

#### Navigation

- `←` / `→` : passer à la valeur précédente / suivante (cycling)
- `Tab` / `Shift+Tab` : naviguer vers le champ précédent / suivant

Ne pas utiliser `Enter` pour cycler — `Enter` confirme le formulaire ou avance au champ suivant.

#### Implémentation de référence

```go
// Rendu
func (f *Form) renderCycleField(label string, options []string, idx int, fieldIdx int) string {
    selectLabel := label + " " + theme.IconSelect + " "
    value := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorText).Render(options[idx])
    if f.focusedField == fieldIdx {
        return theme.KeyStyle.Render(theme.IconCircleSmall+" "+selectLabel+theme.IconChevronRight+" ") + value
    }
    return theme.Bg("  "+selectLabel+theme.IconChevronRight+" ") + value
}

// Gestion des touches
case "left":
    if f.focusedField == fieldIdx {
        f.idx = (f.idx - 1 + len(f.options)) % len(f.options)
    }
case "right":
    if f.focusedField == fieldIdx {
        f.idx = (f.idx + 1) % len(f.options)
    }
```

Interdit :
- ❌ Utiliser `Enter` pour cycler les valeurs
- ❌ Ouvrir une liste déroulante pour 2–4 valeurs (réserver aux longues listes, cf. `renderTemplateList`)
- ❌ Hardcoder la couleur de la valeur (utiliser `ColorText`)
- ❌ Utiliser des radio buttons pour un ensemble fermé (Rule 120)

### Rule 121 : Iconographie circulaire pour les statuts

Les indicateurs de statut dans les tableaux et listes doivent utiliser les icônes circulaires :

| État | Icône |
|------|-------|
| Succès / OK | `theme.IconOK` (`\uf058`) |
| Erreur / Échec | `theme.IconError` (`\uf057`) |
| Avertissement | `theme.IconWarning` (`\uf071`) |

Applicable aux : résultats de scans, statuts pipeline, indicateurs UP/DOWN.
Non applicable aux : icônes de type d'objet (dossiers, fichiers, logo GitLab).

### Rule 133 : Champs texte multi-lignes — `WrappedInput` obligatoire

**`bubbles/textarea` est interdit.** Son background interne n'est pas contrôlable par lipgloss et produit des artefacts visuels (gutter `┃`, fond terminal entre les lignes).

Utiliser **`components.WrappedInput`** (`internal/ui/components/wrapped_input.go`) pour tout champ texte qui doit s'afficher sur plusieurs lignes.

#### API

```go
// Création
field := components.NewWrappedInput(wrapWidth, charLimit, "placeholder text")

// Redimensionnement — appeler dans le handler tea.WindowSizeMsg
field.SetDisplayWidth(terminalWidth)

// Focus / Blur
field.Focus()   // appelé dans updateFocus()
field.Blur()    // appelé dans updateFocus()

// Dans Update() — passer les messages clavier quand le champ a le focus
field, cmd = field.Update(msg)

// Dans View() — retourne le bloc de contenu multi-lignes, prêt à concaténer
return labelStr + "\n" + field.View()

// Récupération de la valeur à la soumission
value := field.Value()
```

#### Paramètre `wrapWidth`

`wrapWidth` est le nombre de **runes** par ligne visuelle avant retour à la ligne (word-wrap au dernier espace, hard-break si aucun espace).

| Contexte | Valeur recommandée |
|----------|--------------------|
| Description GitLab (groupe/projet) | `80` |
| Champ notes court | `60` |
| Champ texte long | `100` |

```go
// ✅ CORRECT
const descWrapWidth = 80
descInput := components.NewWrappedInput(descWrapWidth, 250, "description (optional)")

// ❌ INTERDIT
descInput := textarea.New()
```

#### Comportement

- Champ vide + focusé → curseur bloc seul affiché
- Champ vide + non focusé → placeholder en `ColorDim`
- Champ rempli → texte word-wrappé, chaque ligne paddée avec `theme.PadWithBg` jusqu'à `displayWidth`
- Curseur bloc inversé (`ColorText` bg / `ColorBackground` fg) à la position exacte du caret

Interdit :
- ❌ `bubbles/textarea` dans n'importe quelle vue
- ❌ Recréer localement la logique de word-wrap ou de curseur
- ❌ Oublier d'appeler `SetDisplayWidth` dans le handler `tea.WindowSizeMsg`

### Rule 135 : Assignation stricte des touches clavier dans les formulaires ⚠️

**Chaque touche a un rôle unique et non-ambigu.** Ces règles s'appliquent à tous les formulaires de l'application sans exception.

| Touche | Rôle exclusif |
|--------|---------------|
| `↑ / ↓` | Naviguer entre les champs d'un formulaire |
| `Tab / Shift+Tab` | Switcher entre les onglets (tabs) **uniquement** — jamais pour la navigation entre champs |
| `Space` | Toggler une checkbox — **seule** touche autorisée pour ce rôle |
| `Enter` | Valider un bouton d'action ou déclencher une action spécifique — **jamais** pour toggler une checkbox |
| `← / →` | Changer de niveau dans un tableau (drill-down/up) **et** cycler les valeurs d'un champ à liste fermée |
| `Esc` | Annuler / fermer / revenir au niveau parent |

#### Règles détaillées

**`Tab / Shift+Tab` — tabs uniquement**
- Réservé au switch entre onglets (`bubbles/table` breadcrumb, result tabs, etc.)
- Ne doit **jamais** servir à avancer/reculer entre champs de formulaire
- Si une vue n'a pas d'onglets, `Tab` ne doit rien faire (ou être ignoré)

**`↑ / ↓` — navigation entre champs**
- Seules touches pour déplacer le focus d'un champ à l'autre dans un formulaire
- Dans une liste/table hors formulaire : déplacement ligne par ligne (Rule 111)

**`Space` — toggle checkbox**
- Seule touche valide pour cocher/décocher une checkbox
- `Enter` sur une checkbox doit être ignoré ou avancer au champ suivant, jamais toggler

**`Enter` — validation / action**
- Valide le bouton actuellement focusé
- Déclenche une action contextuelle (ex : lancer un scan, ouvrir un détail)
- Ne doit **pas** toggler une checkbox

**`← / →` — navigation tableau et cycle de valeurs**
- Dans un tableau : `←` remonte d'un niveau (drill up), `→` descend d'un niveau (drill down)
- Sur un champ à liste fermée (cycle field) : `←` valeur précédente, `→` valeur suivante (Rule 132)
- Ne doit **pas** déplacer le focus entre champs de formulaire

#### Violations courantes à éviter

```go
// ❌ INTERDIT — Tab pour naviguer entre champs
case "tab":
    m.focusedField = (m.focusedField + 1) % fieldCount

// ❌ INTERDIT — Enter pour toggler une checkbox
case "enter":
    if m.focusedField == fieldMyCheckbox {
        m.myCheckbox = !m.myCheckbox
    }

// ✅ CORRECT — Space pour toggler
case " ":
    if m.focusedField == fieldMyCheckbox {
        m.myCheckbox = !m.myCheckbox
    }

// ✅ CORRECT — ↑/↓ pour naviguer entre champs
case "up":
    m.focusedField = max(m.focusedField-1, 0)
case "down":
    m.focusedField = min(m.focusedField+1, fieldCount-1)
```
