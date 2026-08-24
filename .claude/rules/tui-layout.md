# TUI — Layout & Navigation

### Rule 101 : Les éléments du TUI doivent avoir une cohérence globale

- les fenêtres modales doivent respecter le même layout
- les boutons doivent être les mêmes partout dans l'application
- il doit y avoir une cohérence dans les touches clavier, quand c'est possible une même fonctionnalité est assignée à une même touche clavier.
  Exemple : Enter = valider, Esc = annuler, Espace = toggle, etc.

### Rule 107 : Indicateurs visuels d'état

- Champs actifs : indicateur `▸` ou style distinct
- Boutons au focus : bold + background coloré
- États de status : utiliser les styles prédéfinis (StatusOKStyle, StatusDownStyle, etc.)

### Rule 108 : Responsive design (terminaux)

- Tous les composants doivent gérer `tea.WindowSizeMsg`
- Stocker width/height dans le modèle
- Adapter l'affichage selon les dimensions disponibles

### Rule 111 : Standard keybindings

**Aucune lettre nue n'est de la navigation.** Les alias vim `h j k l g G` ont
été supprimés en entier (§3.26), y compris dans les composants partagés et dans
le `KeyMap` par défaut de `bubbles/viewport`. Une lettre appartient au
vocabulaire d'actions — `internal/ui/keymap`, où la règle est déclarée et
opposée au code par `TestNoBareLetterIsNavigation`.

Ce qu'on achète n'est pas de la place (le gain se concentrait sur `l`, qui
portait quatre sens) mais une règle vérifiable : garder `j`/`k` laisserait une
exception, et ce sont les exceptions qui ont produit les 16 collisions du
relevé. Le coût est assumé — k9s, lazygit et btop gardent tous `hjkl` — et il se
paie une fois.

- **Navigation (liste/table)**:
  - `↑ / ↓`: Move up/down in the list.
  - `PageUp / PageDown`: Scroll by page.
  - `Home`: Go to top.
  - `End`: Go to bottom.
- **Navigation (tabs & drill-down)**:
  - `Tab / Shift+Tab`: Move between breadcrumb tabs.
  - `←`: Go back to parent level (drill up).
  - `→`: Enter selected group/directory (drill down).
  - `Enter`: Select/confirm item (visible only in selection mode, e.g. when browsing from security view).
  - `Esc`: Go back to parent level, or cancel/close modal.
- **Resource Actions** — une **majuscule**, toujours, et son sens est le même
  partout. Le vocabulaire complet est déclaré dans `internal/ui/keymap`, et
  `TestNoViewBindsAnUndeclaredUppercaseKey` parcourt les sources pour vérifier
  qu'aucune vue n'en sort. Les lettres libres y sont listées (`H J Q Z`) :
  une nouvelle action s'y sert, elle ne s'invente pas une touche. `H` y est
  revenue avec §3.47 : elle traçait la route, et la trace a été supprimée parce
  qu'elle répondait pour la VM Docker et non pour la machine (D57). Une lettre
  qu'une action libère se redéclare libre, sinon elle reste réservée à un usage
  qui n'existe plus.

  | | | | |
  |---|---|---|---|
  | `N` Créer | `E` Éditer | `D` Supprimer | `M` Renommer (*mv*) |
  | `S` Scanner | `A` Scanner tout | `F` Se remettre à jour | `C` Sélection de clone |
  | `T` Terminal | `O` IDE | `W` Navigateur | `L` Logs |
  | `V` Pager | `K` Arrêter / tuer | `P` Prune | `B` Navigateur de registries |
  | `G` Pull (*get*) | `U` Login / logout | `X` Exclure | `R` MR · PR |
  | `I` Issues | `Y` Copier le chemin | | |

- **Minuscules** — un filtre ou une bascule d'affichage, jamais une action. Elle
  ne modifie rien, donc son sens est **local** et deux vues peuvent employer la
  même lettre : `l` est le protocole dans netdiag et la sévérité LOW dans
  security. « Local » veut dire **déclaré** : chaque surface énumère ses touches
  dans `keymap.localToggles`, et `TestEveryLowercaseBindingIsDeclared` refuse
  celles qui n'y sont pas.
  - `r` `p` `s` `t` `z` (containers) : filtres d'état, **cumulatifs**, et la
    remise à zéro. Rien d'actif veut dire `running` — c'est l'état de repos de
    la vue, donc elle s'ouvre **sans barre**, et `z` y ramène : arriver et
    appuyer sur `z` donnent le même écran. « Tout » est les quatre jetons
    ensemble ; il n'y a pas de jeton `all`, ce serait un cinquième état à
    sélectionner à côté de quatre vrais. C'est la différence avec netdiag/Ports,
    où rien d'actif veut dire « pas d'avis » et montre tout.
  - `f` `c` `w` `v` `t` (viewer) : affichage, coloration, retour à la ligne,
    verbosité, horodatage.
  - `t` `u` `l` `e` `n` `z` (netdiag/Ports) : filtres de protocole et d'état.
  - `r` (browser OCI) : registry affiché.
  - `c` `h` `m` `l` (security) : sévérités, **cumulatives** — `c`+`h` demande
    « CRITICAL **ou** HIGH », ce qu'un seuil ne sait pas exprimer.

- **Deux exceptions, déclarées** dans `keymap.DeclaredExceptions()` : `c` (test
  de connectivité, inspection réseau OCI) et `ctrl+y` (copier la commande
  `docker run`). Brûler une majuscule globale pour une action présente dans un
  seul sous-écran coûterait plus que ça ne rapporte. Elles sont écrites comme
  exceptions pour que le prochain relevé ne les prenne pas pour des dérives.

- **Une modale est un quatrième espace**, disjoint par le *mode* et non par la
  casse : elle réclame toute touche avant que la vue ne la voie, donc son
  `y`/`n` ne heurte aucune action.

- **Document viewer** (opened from another view; `esc` returns there):
  - `f`: Bascule entre le document tel qu'il est et la **seule** vue que son
    kind en dérive — l'arbre pour JSON et XML, la forme rendue pour Markdown.
    Un kind n'en dérive jamais deux, donc la touche n'est jamais ambiguë ; pour
    un kind qui n'en dérive aucune elle est masquée (Rule 130).
  - `c`: Syntax coloring on/off. La raison a changé avec §3.26 : elle invoquait
    `h`/`l` comme alias réservés de `←`/`→`, ce qui interdisait une touche
    *highlight* en `h` — ces alias n'existent plus, donc l'argument est tombé.
    `c` reste parce que *coloration* est de toute façon un meilleur repère que
    *highlight*, et déplacer une touche pour courir après un motif supprimé
    serait du bruit. **`c` est orthogonale à `f`** : éteindre la couleur d'un
    Markdown rendu ne fait pas réapparaître ses marqueurs — le rendu est un
    affichage, la coloration en est une autre, et un `c` qui révélerait les
    marqueurs serait la seconde voie vers un même écran.
  - `w`: Soft wrap (text display).
  - `v`: Cycle the minimum log level shown (logs only).
  - `/`: Search — filtre **et** surligne. Les lignes sans occurrence disparaissent
    et chaque occurrence des lignes restantes est mise en surbrillance : c'est ce
    qui dit *où* dans une ligne longue. La surbrillance ne dépend pas de `c` (une
    occurrence n'est pas de la coloration syntaxique), survit à `w`, et dans un log
    le niveau garde le reste de la ligne.
  - `ctrl+r` / `F`: Relire une fois / relire en boucle. `F` est une **bascule**,
    et le suivi se fait **dans le viewport** — le document suivi reste collé en
    bas. Il ouvrait `docker logs -f` par `tea.ExecProcess`, dont on ne sort que
    par ctrl+c : le TUI suspendu ne l'intercepte pas, donc ça tuait
    l'application et rendait le terminal dans le mode du processus fils.
  - `V`: Open in the system pager (the one surviving pager path, container logs
    only). C'est lui qui *stream* vraiment, et on en sort par `q`.
- **Control**:
  - `Enter`: Validate, Execute, or Open.
  - `Esc`: Close modal, cancel, or go back.
  - `Space`: Toggle, Select, or Pause/Resume.
- **Sorting**:
  - `.` (dot): Cycle sort column. C'est le seul contrôle de tri ; le
    « sorting menu » en `Shift+S` que cette règle annonçait n'a jamais existé
    dans le code, et `S` appartient au vocabulaire d'actions.
- **Global**:
  - `ctrl+p`: Open command mode — depuis n'importe où, champ focusé compris.
    Remplace `alt+:`, qui n'atteignait pas Terminal.app ni iTerm2 (Option n'y
    est pas Meta par défaut) alors que c'était la seule voie traversant un
    champ. Voir `internal/ui/keymap.CommandMode`.
  - `:`: Open command mode, sauf en édition — là c'est un caractère, dont les
    valeurs comme `https://trivy-server:4954` ont besoin.
  - `/`: Search/Filter.
  - `?`: Open help menu.
  - `q`: Quit view/app.
  - `ctrl+r`: **Rafraîchir, et rien d'autre.** Elle voulait aussi dire « revenir »
    dans les résultats de security et de netdiag, où `esc` suffit.

**Il ne reste que trois combinaisons `Ctrl`**, et `TestOnlyThreeCtrlCombinationsSurvive`
le vérifie : `ctrl+c` (SIGINT), `ctrl+r` (rafraîchir) et `ctrl+p` (la ligne de
commande). Le budget est d'environ quatorze touches et chacune traîne une
contrainte — `ctrl+a` est le préfixe de screen, `ctrl+b` celui de tmux,
`ctrl+s`/`ctrl+q` le contrôle de flux, `ctrl+i`/`ctrl+m`/`ctrl+j`/`ctrl+h` sont
TAB, Entrée, LF et Backspace. Une action qui se réinstallerait derrière `Ctrl`
reprendrait une place que `Shift` donne gratuitement.

`Ctrl+Shift` n'est pas une option : le code de contrôle écrase la casse, donc
`ctrl+a` et `ctrl+shift+a` émettent tous deux 0x01. Les distinguer exige le
protocole clavier Kitty ou `modifyOtherKeys`, que bubbletea v1.3.10 n'active pas
— et même alors l'émulateur se sert d'abord (`ctrl+shift+c/v/t/w/n`).

### Rule 112 : Formulaires dans le viewport (pas de modales)

**Les formulaires de création/édition doivent être affichés dans le viewport principal, pas en modal.**

Les modales sont réservées aux confirmations et messages courts.

```go
// Dans View() — priorité d'affichage
func (m Model) View() string {
    if m.creationForm != nil { return m.creationForm.View() }
    if m.confirmModal != nil {
        return lipgloss.Place(m.width, m.height,
            lipgloss.Center, lipgloss.Center, m.confirmModal.View())
    }
    return m.renderNormalView()
}
```

| Type | Affichage | Exemples |
|------|-----------|----------|
| **Formulaire** | Viewport complet | Création groupe/projet, ajout monitor, édition |
| **Confirmation** | Modal centrée | Suppression, actions dangereuses |
| **Information** | Modal centrée | Rapports, messages d'erreur détaillés |

### Rule 123 : Positionnement de la ligne de tabs associée à une table

**La ligne de tabs doit être placée immédiatement sous la table, toujours visible (jamais scrollée).**

```
┌─────────────────────────────────┐
│  ┌───────────────────────────┐  │
│  │   TABLE (viewport)        │  │
│  └───────────────────────────┘  │  ← bord bas du viewport
│  [tab1]  [tab2 actif]  [tab3]   │  ← ligne de tabs toujours visible
│  contenu du tab actif...        │
└─────────────────────────────────┘
```

**Deux modes :**
- **Navigation** : Tab/Shift+Tab ou ←/→ pour changer d'onglet, tab actif en `ColorSecondary`
- **Breadcrumb** : pas de navigation, actif en surbrillance `ColorSecondary`, parents en `ColorDim`

**Padding gauche obligatoire** (1 caractère pour aligner avec le viewport) :
```go
// ✅ CORRECT
return theme.PadWithBg(theme.Bg(" ") + theme.RenderTabs(tabs, activeIdx), width)
// ❌ INTERDIT : tabs collés au bord gauche
return theme.PadWithBg(theme.RenderTabs(tabs, activeIdx), width)
```

Interdit :
- ❌ Tabs AU-DESSUS de la table
- ❌ Tabs DANS le viewport (seraient scrollés)
- ❌ Navigation de tabs pour un breadcrumb

### Rule 124 : Layout général de l'application

Structure verticale stricte (de haut en bas) :

1. **Header** (fixe) : 7 lignes
2. **Ligne vide** : 1 ligne
3. **Ligne de commande** : 1 ligne
4. **Viewport** (dynamique) : bordure `NormalBorder`, occupe l'espace restant
5. **Footer** (fixe) :
   - **Sans tabs** : ligne vide (1 ligne) + ligne d'information (1 ligne) = **2 lignes**
   - **Avec tabs** : ligne de tabs (1 ligne) + ligne vide (1 ligne) + ligne d'information (1 ligne) = **3 lignes**

Règles du footer :
- **Ligne vide obligatoire** entre le viewport et la première ligne du footer (ligne d'info ou ligne de tabs)
- Ligne d'information toujours rendue, même vide
- Texte centré horizontalement (`lipgloss.Center`)
- Couleur `ColorHighlight` (sauf erreurs → `StatusErrorStyle`)
- **Ligne vide obligatoire** entre la ligne de tabs et la ligne d'information

```go
// Sans tabs : ligne vide + ligne d'info
footerHeight := 2
// Avec tabs : ligne de tabs + ligne vide + ligne d'info
if showTabs { footerHeight = 3 }
viewportHeight := windowHeight - 7 - 1 - 1 - footerHeight
```

Rendu du footer :
```go
// Sans tabs
return theme.EmptyLineBg(width) + "\n" + infoLine

// Avec tabs
return tabBar + "\n" + theme.EmptyLineBg(width) + "\n" + infoLine
```

### Rule 130 : Shortcuts must be dynamic

**`GetShortcuts()` must reflect the current state of the view and selected item.**

Shortcuts that apply only to specific entry types or states must be conditionally included:
- Hidden when the action is not available (no entry selected, wrong type, missing precondition)
- Updated description when the meaning changes based on context

| Shortcut | Condition to show |
|----------|-------------------|
| `enter` (scan details) | Selected entry is a git repo **and** has cached scan results |
| `ctrl+w` (browser) | Selected entry is a git repo |
| `ctrl+s` (scan) | Selected entry is a git repo or a directory with nested git repos |
| `ctrl+n` (new directory) | No git repo is selected |

```go
// Pattern: resolve selected entry, then build shortcuts conditionally
var selectedEntry *Entry
if len(m.entries) > 0 {
    idx := m.table.Cursor()
    if idx >= 0 && idx < len(m.entries) {
        selectedEntry = &m.entries[idx]
    }
}

isGitRepo := selectedEntry != nil && selectedEntry.IsGitRepo
```

Interdit :
- ❌ Showing shortcuts for actions that cannot be performed in the current state
- ❌ Static shortcut lists that ignore the selected item's type

### Rule 134 : Les raccourcis clavier appartiennent au header, jamais au viewport

**Ne jamais intégrer de texte `[touche] action` dans le contenu du viewport.**

Le header affiche déjà tous les raccourcis via `GetShortcuts()` (Rule 130). Dupliquer ces informations dans le viewport crée du bruit visuel, réduit l'espace utile, et désynchronise les deux sources lors de mises à jour.

| Interdit (dans viewport) | Correct (dans header) |
|--------------------------|----------------------|
| `[↑↓/jk] navigate  [c] connectivity test  [esc] close` | `GetShortcuts()` retourne `{Key: "↑↓/jk", Description: "Navigate"}`, etc. |
| `[↑↓/jk] scroll  [g/G] top/bottom  [enter] new test` | `GetShortcuts()` état results retourne les raccourcis de scroll |
| `Press Enter to confirm` en bas d'un formulaire | `GetShortcuts()` retourne `{Key: "enter", Description: "Confirm"}` |

**Pattern obligatoire :**

```go
// ❌ INTERDIT — aide inline dans le viewport
func (f *Form) View() string {
    lines = append(lines,
        theme.EmptyLineBg(w),
        theme.PadWithBg(theme.HelpStyle.Render("  [↑↓/jk] scroll  [enter] confirm  [esc] back"), w),
    )
    return strings.Join(lines, "\n")
}

// ✅ CORRECT — viewport sans aide inline, GetShortcuts() gère tout
func (f *Form) View() string {
    // contenu uniquement, pas de ligne d'aide
    return strings.Join(lines, "\n")
}

// Dans la vue parente (view.go)
func (m Model) GetShortcuts() shortcut.Shortcuts {
    if m.myForm != nil {
        if m.myForm.state == stateResults {
            return []shortcut.Shortcut{
                {Key: "↑↓/jk", Description: "Scroll"},
                {Key: "enter", Description: "Run new test"},
                {Key: "esc", Description: "Go back"},
            }
        }
        return []shortcut.Shortcut{
            {Key: "tab", Description: "Next field"},
            {Key: "enter", Description: "Confirm"},
            {Key: "esc", Description: "Go back"},
        }
    }
    // ...
}
```

**Conséquences :**
- Le viewport gagne de la hauteur utile (suppression de 1–2 lignes fixes)
- `resultVisibleLines()` / `viewportOverhead` doivent être mis à jour en conséquence
- `GetShortcuts()` doit être état-aware (Rule 130) pour refléter les raccourcis disponibles selon l'état courant

Interdit :
- ❌ `theme.HelpStyle.Render("  [key] action  [key] action")` dans le rendu d'un formulaire ou d'une liste
- ❌ Ligne d'aide statique en bas du viewport
- ❌ `GetShortcuts()` non mis à jour quand l'état change (ex : résultats vs saisie)

### Rule 137 : Shortcut description format

**All shortcut descriptions must start with a capital letter and use an imperative verb (action form).**

| ❌ Wrong | ✅ Correct |
|---------|-----------|
| `"scroll"` | `"Scroll"` |
| `"back"` | `"Go back"` |
| `"confirm"` | `"Confirm"` |
| `"new test"` | `"Run new test"` |
| `"next field"` | `"Next field"` |
| `"navigate"` | `"Navigate"` |
| `"quit"` | `"Quit"` |

Applies to all `shortcut.Shortcut{Key: "...", Description: "..."}` definitions across all views.

### Rule 138 : Only non-obvious shortcuts in GetShortcuts()

**`GetShortcuts()` must only surface shortcuts that are not self-evident to the user. Omit universally-known navigation shortcuts.**

#### Always omit from GetShortcuts()

| Shortcut | Reason |
|----------|--------|
| `↑ / ↓` or `j / k` | Universal list navigation — obvious |
| `j / k` alone | Vim aliases — redundant alongside arrow keys |
| `PageUp / PageDown` | Universal scrolling — obvious |
| `Tab / Shift+Tab` | Use `Tab` only, drop `Shift+Tab` — direction is implied |
| `g / G`, `Home / End` | Universal top/bottom — obvious |

#### Key display rules

- Show only **one** key when a shortcut has an alias: prefer the primary key
  - ✅ `↑↓` (not `↑↓ / jk`)
  - ✅ `tab` (not `tab / shift+tab`)
  - ✅ `←→` (not `←→ / hl`)

#### What to show

Only include shortcuts that are **specific to the current view or state**, and that a user cannot reasonably guess:

```go
// ✅ CORRECT — only non-obvious, context-specific shortcuts
return []shortcut.Shortcut{
    {Key: "ctrl+n", Description: "New resource"},
    {Key: "ctrl+s",  Description: "Scan"},
    {Key: "ctrl+d",  Description: "Delete"},
    {Key: "enter",   Description: "Open details"},
    {Key: "/",       Description: "Filter"},
    {Key: "?",       Description: "Help"},
}

// ❌ WRONG — cluttered with obvious navigation
return []shortcut.Shortcut{
    {Key: "↑↓/jk",       Description: "Navigate"},
    {Key: "pgup/pgdown",  Description: "Scroll page"},
    {Key: "tab/shift+tab", Description: "Switch tab"},
    {Key: "ctrl+n",        Description: "New resource"},
}
```
