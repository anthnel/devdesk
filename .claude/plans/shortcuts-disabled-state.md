# Plan : un raccourci indisponible est grisé, pas supprimé

**Portée retenue** : le mécanisme (une fois, dans `internal/ui/shortcut`) + la
vue `workspaces` comme seule vue migrée et comme référence écrite pour les
suivantes.
**Complexité** : moyenne — le mécanisme est petit, la vue demande de séparer
proprement ce qui relève du mode et ce qui relève de l'état.

## 1. Ce qu'on construit

Un raccourci qui ne s'applique pas maintenant **reste à sa place** et perd la
couleur et la graisse de sa touche. La colonne de raccourcis cesse donc de se
réorganiser à chaque déplacement du curseur — c'est le gain, et c'est ce qui
est testable.

Deux causes de grisage, et deux seulement :

| Cause | Exemple | Connue par |
|---|---|---|
| l'état de la **ligne** sélectionnée | `F` sur un fichier, `W` sur un dépôt sans remote | la vue, à chaque frame |
| une indisponibilité **globale** | `S` et `A` quand ni Trivy ni Gitleaks ne se résolvent | `scan.CheckDependencies`, une fois au montage |

Un changement de **mode** (formulaire, confirmation, sélection) continue de
remplacer la liste entière : ce n'est pas le même vocabulaire, et griser
`enter → Create` pendant qu'on est dans une table afficherait la réunion de
tous les modes.

## 2. Le mécanisme — écrit une fois

Le header n'est pas un objet par vue : le routeur le rend
(`internal/app/app_header.go`) à partir de `GetShortcuts()`. Il n'y a donc rien
à instancier — le champ va sur le type partagé, le rendu dans la seule fonction
qui rend une ligne de raccourci, et chaque vue se contente de le renseigner.

### 2.1 `internal/ui/shortcut/shortcut.go`

```go
type Shortcut struct {
    Key         string
    Description string
    // Disabled : l'action existe dans ce mode mais ne s'applique pas à l'état
    // courant. L'entrée reste affichée, à sa place, la touche en dim — masquer
    // ferait bouger toutes les autres à chaque déplacement du curseur.
    Disabled    bool
}
```

`ToStrings()` choisit le style de la touche selon `Disabled`. **`maxLenKey()`
ignore `Disabled`** : l'alignement ne doit pas dépendre de ce qui est
disponible, sinon la colonne bouge quand même — c'est le point.

### 2.2 `internal/ui/theme`

- `colors.go` : `ColorShortcutDisabled`, déclarée à côté de
  `ColorFooterInfo/Warn/Error`.
- `manager.go` (`ApplyTheme`) : **alias sémantique**, `= ColorDim`. Aucun
  fichier de thème ne gagne de clé — précédent exact des couleurs de footer
  (Rule 128) et des couleurs de syntaxe du viewer. Un thème qui voudra un
  troisième gris le séparera plus tard sans changer une ligne d'appelant.
- `styles.go` : `ShortcutKeyDisabledStyle` = fond app, `ColorShortcutDisabled`,
  **non gras**. Les deux blocs sont à mettre à jour — `styles.go` déclare les
  styles deux fois (init et `RefreshStyles`).

La description reste `ShortcutDescriptionStyle` (déjà `ColorDim`) : le
discriminant est la touche, qui passe de lavande gras à gris plat. Ligne
désactivée = ligne uniformément grise.

## 3. `workspaces` — un seul calcul, deux lecteurs

Le défaut à éviter est celui que `scan.Categorize` et `Result.SecretVerdict`
ont chacun dû défaire : deux règles pour une question, qui finissent par ne
plus dire la même chose. Ici ce serait une touche grisée qui agit quand même,
ou l'inverse.

### 3.1 `internal/ui/workspaces/availability.go` (nouveau)

```go
// actionState : une raison vide veut dire disponible. Le booléen et le motif
// ne peuvent pas diverger parce qu'il n'y a qu'un champ.
type actionState struct{ Reason string }

func (s actionState) Enabled() bool { return s.Reason == "" }

type actionSet struct {
    Enter, Web, Scan, Sync, ScanAll, New, Rename, Delete, Copy actionState
}

func (m Model) actions() actionSet
```

- `GetShortcuts()` : `Disabled: !a.Scan.Enabled()`
- le handler : `if !a.Scan.Enabled() { return m, m.footer.Warn(a.Scan.Reason) }`

**Le dim dit « pas maintenant », la touche pressée dit pourquoi.** Le header
n'a pas la place de porter un motif ; le footer l'a, et c'est un `Warn` au sens
de Rule 128 — l'action ne peut pas être honorée telle que demandée, rien n'a
échoué.

### 3.2 Les prédicats

| Touche | Disponible quand | Aujourd'hui |
|---|---|---|
| `enter` | fichier (→ viewer) **ou** dépôt scanné (→ findings) | masqué sinon |
| `W` | dépôt git **avec un remote** | montré dès `IsGitRepo` — donc muet sur un dépôt sans remote (petit défaut corrigé au passage) |
| `S` | (dépôt git ∨ sous-dépôts) ∧ un scanner résolvable | masqué sur la ligne, **aucune garde sur les outils** |
| `F` | dépôt git ∨ sous-dépôts | masqué sinon |
| `A` | un scanner résolvable | toujours montré |
| `N` | la ligne n'est pas un dépôt git | masqué sinon |
| `M`, `D`, `Y` | une ligne est sélectionnée | `Y` masqué sinon, `M`/`D` toujours montrés |

`T` et `O` restent toujours disponibles : ils retombent sur le répertoire
parcouru quand aucune ligne n'est sélectionnée.

**`m.busy(path)` ne grise pas.** Il change à chaque tick, le spinner de la
ligne le dit déjà, et une entrée qui clignote dirait le contraire de ce que ce
lot cherche. Le handler continue de refuser avec `busyMessage`.

### 3.3 La disponibilité des outils

`scan.CheckDependencies` fait des `exec.LookPath`, des `--version` et un
`docker images -q` : elle **ne peut pas** être appelée dans `New()` ni dans
`View()` (Rule 110). Elle passe par un `Cmd` lancé depuis `Init()`, qui rend un
`DepsCheckedMsg{scan.DependencyStatus}` rangé dans le modèle par `Update()`.
C'est la forme que la vue security avait et a perdue en §3 phase 3, reprise ici
pour une autre raison ; le précédent vivant est `detectTools` du dashboard.

**Tant que la réponse n'est pas arrivée, `S` et `A` sont disponibles.** Ne pas
savoir n'est pas savoir que non : griser d'abord pour dégriser trois frames plus
tard se lit comme une panne, et c'est D20 à l'échelle d'une touche.

Un scanner suffit — Trivy **ou** Gitleaks. Le motif nomme ce qui manque
(« No scanner available — check Trivy and Gitleaks »).

## 4. Fichiers touchés

| Fichier | Action | Pourquoi |
|---|---|---|
| `internal/ui/shortcut/shortcut.go` | UPDATE | le champ, le style conditionnel |
| `internal/ui/shortcut/shortcut_test.go` | UPDATE | rendu grisé, alignement inchangé |
| `internal/ui/theme/colors.go` | UPDATE | `ColorShortcutDisabled` |
| `internal/ui/theme/manager.go` | UPDATE | l'alias dans `ApplyTheme` |
| `internal/ui/theme/styles.go` | UPDATE | `ShortcutKeyDisabledStyle`, **deux blocs** |
| `internal/ui/workspaces/availability.go` | CREATE | `actionSet`, le calcul unique |
| `internal/ui/workspaces/view.go` | UPDATE | `GetShortcuts()` lit `actions()` |
| `internal/ui/workspaces/model.go` | UPDATE | le champ `deps`, `Init()` |
| `internal/ui/workspaces/messages.go` | UPDATE | `DepsCheckedMsg` (Rule 109) |
| `internal/ui/workspaces/commands.go` | UPDATE | `checkDepsCmd` |
| `internal/ui/workspaces/update.go` | UPDATE | les gardes lisent `actions()` |
| `internal/ui/workspaces/actions.go` | UPDATE | les `return m, nil` muets deviennent des `Warn` |
| `internal/ui/workspaces/copy_test.go` | UPDATE | `Y` absent → `Y` présent et grisé |
| `internal/ui/workspaces/view_test.go` | UPDATE | idem pour `enter`, `W`, `S`, `F`, `N` |
| `.claude/rules/tui-layout.md` | UPDATE | Rule 130 : masquer → griser, et où passe la ligne |
| `docs/backlog.md` | UPDATE | §3.48 |

## 5. Tâches

1. **Le champ et le rendu** (`shortcut`, `theme`). Test : une entrée désactivée
   rend une touche sans la couleur ni le gras d'une active, et `maxLenKey()`
   donne le même résultat avec et sans `Disabled`.
2. **`actionSet`** (`availability.go`) + `GetShortcuts()`. Test central :
   **en mode normal, la suite des touches est identique quel que soit l'état de
   la ligne** — seuls `Disabled` et la description de `enter` varient. C'est la
   forme vérifiable de « les shortcuts ne bougent pas ».
3. **Les gardes** (`update.go`, `actions.go`). Test table-driven : pour chaque
   action, si `GetShortcuts()` la marque désactivée, presser sa touche ne change
   rien au modèle et pose un `Warn` non vide.
4. **Les outils** (`DepsCheckedMsg`). Tests : `S`/`A` disponibles avant que le
   message n'arrive ; désactivés après un `DepsCheckedMsg` vide ; disponibles
   avec un seul des deux scanners.
5. **Rule 130 et §3.48**, dans le même commit que le code (Rule 114 vaut pour
   l'aide, la même discipline vaut pour la règle qu'on retourne).

## 6. Validation

```bash
mise run check          # fmt + vet + lint + test
mise run test-race      # les Cmd de deps sont neufs
go run .                # :ws — parcourir un fichier, un dépôt, un répertoire
```

Vérification à l'œil, qu'aucun test ne couvre : le gris de la touche doit se
distinguer du gris de la description sur le thème par défaut. Si `ColorDim`
rend les deux moitiés indistinguables, l'alias existe précisément pour prendre
une autre valeur sans toucher un appelant.

## 7. Risques

| Risque | Probabilité | Parade |
|---|---|---|
| Le grisé ne se voit pas (touche et description au même `ColorDim`) | moyenne | l'alias est séparé dès le départ ; changer sa valeur ne touche aucun appelant |
| Une touche grisée qui agit encore | faible | un seul `actionSet` lu par les deux, et le test de la tâche 3 |
| `CheckDependencies` appelée hors `Cmd` | faible | Rule 110, et `test-race` |
| Rule 130 dit encore « masquer » ailleurs dans le dépôt | moyenne | `grep -rn "Rule 130"` avant de clore |
| Les dix autres vues masquent toujours — incohérence visible | assumée | inscrite en §3.48 comme suite, `workspaces` est la référence |

## 8. Acceptation

- [ ] En mode normal `ws`, la colonne de raccourcis ne se réorganise sur aucun
      déplacement du curseur
- [ ] `S` et `A` grisés sans scanner, et une explication au premier appui
- [ ] Aucune touche grisée n'agit
- [ ] `mise run check` et `mise run test-race` passent
- [ ] Rule 130 et §3.48 à jour dans le même commit
