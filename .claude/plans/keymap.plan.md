# Plan: Une touche, un sens — le clavier passe en majuscules

**Source**: `docs/backlog.md` §3.26 (mergé `ae2d9a7`)
**Base**: `5107211`
**Complexité**: **Large** — 15 surfaces, ~184 liaisons, 3 dissolutions d'action, 1 réglage de config, 1 filtre qui change de nature.

## Résumé

§3.26 a relevé 16 collisions et 6 risques de portabilité, et a tranché : les
**majuscules** portent le vocabulaire d'actions (global), les **minuscules** les
bascules d'affichage (local), le reste est structurel. Ce plan applique ce
tableau, remplace `alt+:` par `ctrl+p`, supprime les alias vim, et corrige la
cause de la mort de `pgup`/`pgdown`.

**Le livrable qui n'est pas dans le tableau est le plus important** : un paquet
`internal/ui/keymap` qui *déclare* le vocabulaire, et les tests qui vérifient
qu'aucune vue n'en sort. Sans lui on refait le relevé dans six mois — c'est
littéralement ce que dit §3.26 (« la forme qui rend la règle vérifiable par un
test, et c'est le seul intérêt de la formuler ainsi »).

## Vérifications préalables — faites

| Point | Résultat |
|---|---|
| `ctrl+p` libre dans l'application | ✅ aucune occurrence (`grep '"ctrl+p"'`) |
| `ctrl+p` parsé par bubbletea v1.3.10 | ✅ `KeyCtrlP = keyDLE` (0x10), `key.go:184` |
| `ctrl+p` vs tty / multiplexeurs | ✅ pas un caractère de contrôle réservé, pas un préfixe screen (`ctrl+a`) ni tmux (`ctrl+b`) |

**Reste à vérifier par toi, avant que la règle n'entre dans `tui-layout.md`** :
que `ctrl+p` arrive bien dans Windows Terminal, iTerm2 et sous tmux. C'est le
contrôle qui a manqué à `alt+:`, et aucun test Go ne peut le faire. La phase 2
livre de quoi le vérifier en trente secondes.

## Patterns à reprendre, pas à réinventer

| Catégorie | Source | Motif |
|---|---|---|
| Contrat vérifié par test | `internal/command` — `AllViewNames()` + `TestEveryViewSuppliesItsHeaderAndHelp` | une liste déclarée, un test qui l'oppose au code |
| Vocabulaire partagé entre deux paquets | `TestTheProviderVocabularyMatchesTheConfig` | les noms sont énoncés des deux côtés, un test les tient en phase |
| Modale + case à cocher | `components.DeleteConfirmModal` (`permanentlyRemove`, `locked`, `minFocus`) | exactement la forme demandée pour `A` |
| Modale à plusieurs boutons | `components.ConfirmModal`, focus par défaut sur « No » | base de la modale de `K` |
| Jetons de filtre cumulatifs | `netdiag/ports_model.go:314-326` + `NewFilterBarWithTokens` | exactement la forme demandée pour les sévérités de security |
| Transmission par défaut | `containers/update.go`, `explorer`, Registries — `return m, m.table.Update(msg)` | la forme qui n'a pas le trou `pgup` |
| Réglage scalaire de contexte | `internal/ui/configuration/fields.go` — un accesseur pointeur par réglage | pour la variante « nouvelle fenêtre » |

## Phases

### Phase 1 — Le vocabulaire, et les tests qui le tiennent (RED d'abord)

**Nouveau paquet `internal/ui/keymap`** (`keymap.go`), sans dépendance sur
bubbletea — c'est une liste de chaînes et rien d'autre.

- 21 constantes d'action (le tableau §3.26 : `N E D M S A F C T O W L V K P B G U X R I`).
- `Actions() map[string]string` — touche → sens, pour le test et pour l'aide.
- `LocalToggles()` — les minuscules déclarées, par vue.
- `DeclaredExceptions()` — `c` (test de connectivité, inspection réseau OCI) et
  `ctrl+y` (copier `docker run`). §3.26 exige qu'elles soient **écrites comme
  exceptions**, sinon le prochain relevé les compte comme des dérives.
- `Free()` — `H J Q Y Z`, pour que le prochain ajout sache où piocher.

**Trois tests, et chacun échoue avant la migration** :

| Test | Ce qu'il empêche |
|---|---|
| `TestNoTwoActionsShareALetter` | la collision que §3.26 vient de démonter |
| `TestNoViewBindsAnUndeclaredUppercaseKey` | scan de `internal/ui/**/*.go` : tout `case "X"` où X est une majuscule seule doit être dans `Actions()` |
| `TestNoBareLetterIsNavigation` | scan pour `"h" "j" "k" "l" "g" "G" "b" "f"` en position de navigation — c'est la règle achetée par la suppression des alias vim |

Le scan de source est brittle, oui. C'est assumé : c'est le prix du contrôle, et
le paquet `command` a déjà ce type de test.

### Phase 2 — Le routeur : `ctrl+p` entre, `alt+:` sort

`internal/app/keys.go` — la plus petite phase et la plus urgente.

- `ctrl+p` remplace `altCommandModeKey`, **au même endroit** : avant tout test
  `InEditMode()`, donc aucun champ ne peut la réclamer.
- `:` reste, inopérant en édition — inchangé.
- `alt+:` **supprimée**, sans alias de transition.
- Le commentaire de `keys.go:7-10` est réécrit : le raisonnement sur `ctrl+:`
  reste juste et doit rester, mais il doit maintenant dire pourquoi le repli
  n'est plus `alt`.

**Testable immédiatement** : `mise run dev`, `ctrl+p` doit ouvrir la ligne de
commande. C'est la vérification manuelle demandée ci-dessus.

### Phase 3 — Remplacer les listes blanches, pas les allonger

C'est le correctif `pgup`/`pgdown`, et §3.26 est catégorique sur la forme.

| Site | État |
|---|---|
| `status/update.go:247` | la liste extérieure ne cite pas `pgup`/`pgdown` ; `:284` les gère, code mort |
| `oci_resources/keys.go:107-119` (Networks) | 4 cases explicites puis `return m, nil` |
| `oci_resources/keys.go:138-150` (Volumes) | idem |
| `oci_resources/connectivity_form.go:186-198` | idem |

Chacun se termine par `return m, m.table.Update(msg)`. Le code mort de
`status:284` disparaît avec sa cause.

### Phase 4 — Les alias vim, en entier

`h j k l g G` partout, plus `b`/`f` en demi-page dans le détail security.
Sites relevés : `workspaces` (×6), `viewer` (×8, livrés par §3.25), `security`
details/findings, `netdiag` update + ports, `explorer` (×4), `oci` images +
networks + volumes + connectivity, `containers`, `components/delete_confirm_modal.go`
(`case "up", "k"` — une modale partagée, donc à ne pas oublier).

`home`/`end` couvrent déjà `g`/`G`.

**Et la justification de §3.25 est réécrite** (`tui-layout.md:42-44`) : `c` reste
la coloration du viewer, mais l'argument « `h` est pris par Rule 111 » tombe
avec les alias. La raison devient : *coloration* est un meilleur repère que
*highlight*.

### Phase 5 — Le tableau des 21, vue par vue

| Vue | Change |
|---|---|
| `containers` | `K` stop→modale · `r`→bouton de la modale · `ctrl+d`→`D` · `p`→`P` · `s`→`T` · `S`→réglage · `l`→`L` · `i`→`enter` · `a` reste |
| `oci`/Images | `ctrl+e`→`N` · `ctrl+d`→`D` · `ctrl+s`→`S` · `ctrl+a`+`A`→`A`+case · `p`→`P` · `b`→`B` |
| `oci`/Networks · Volumes | `ctrl+n`→`N` · `ctrl+d`→`D` · `p`→`P` |
| `oci`/Registries | `ctrl+n`→`N` · `e`→`E` · `l`/`L`→`U` (bascule sur l'état de la ligne) · `ctrl+d`→`D` |
| `oci`/Browser | `r`→reste (minuscule locale) · `p`→`G` (pull) · `ctrl+s`→`S` |
| `workspaces` | `ctrl+n`→`N` · `r`→`M` · `ctrl+d`→`D` · `t`/`T`→`T`+réglage · `ctrl+o`→`O` · `ctrl+w`→`W` · `ctrl+s`→`S` · `s`→`F` · `A`+`ctrl+a`→`A`+case |
| `security`/inventory | `ctrl+s`→`S` · `ctrl+a`→`A`+case |
| `security`/findings | `i`→`X` · `1`–`4` **supprimées** · `.` redevient le tri (phase 7) |
| `security`/details | `o`→`W` · `backspace` supprimée |
| `explorer` | `ctrl+n`→`N` · `ctrl+d`→`D` · `ctrl+w`→`W` · `c`→`C` |
| `status` | `ctrl+n`→`N` · `e`→`E` · `ctrl+d`→`D` |
| `netdiag`/Ports | `ctrl+k`→`K` + **confirmation** · minuscules `t u l e n z` conservées |
| `netdiag`/résultats | `r` supprimée (`esc` suffit) · `ctrl+r` ne veut plus dire « revenir » |
| `dashboard` | `m`→`R` (merge requests · PR) · `i`→`I` (issues) |
| `viewer` | `ctrl+f`→`F` · `e`→`V` · minuscules `f c w v t` conservées |

`ctrl+r` (rafraîchir) et `ctrl+c` survivent partout, seuls avec `ctrl+p`.

### Phase 6 — Les trois dissolutions, et la confirmation qui manquait

- **Modale de `K`** : `components.ConfirmModal` à trois boutons (Stop / Restart /
  Cancel), défaut sur Cancel. Absorbe `restartSelectedContainer`, qui agissait
  sans confirmation alors que c'est un stop+start.
- **Case à cocher de `A`** : « purger le cache d'abord », sur le motif de
  `DeleteConfirmModal.permanentlyRemove`. Fait disparaître `ctrl+a`, la paire la
  plus proche d'une perte de données involontaire de l'application.
- **`ctrl+k` → `K` + confirmation** dans netdiag/Ports. **Prioritaire** : c'est
  un SIGKILL sur un processus de l'hôte, aujourd'hui sans confirmation.
- **`space`** (pause/reprise) : aucune confirmation, délibérément.

### Phase 7 — Le filtre de sévérité change de nature

`security`/findings : `.` redevient le tri (Rule 111), et les sévérités passent
en **quatre bascules cumulatives** `c h m l` via `NewFilterBarWithTokens` —
« CRITICAL **et** HIGH » est la question réelle, et un cycle ne sait pas la
poser. `1`–`4` disparaissent.

### Phase 8 — La variante « nouvelle fenêtre » devient un réglage

`S` (shell) et `T` (terminal) en nouvelle fenêtre disparaissent comme touches.
La capacité dépend de l'environnement — inexistante sous WSL, absurde à travers
SSH — donc un réglage absent vaut mieux qu'une touche inerte. Un champ booléen
dans l'onglet `app` de la vue configuration, un accesseur pointeur, comme les
29 autres.

### Phase 9 — Aide, raccourcis, règles

- `GetShortcuts()` de chaque vue — Rule 130 (dynamiques), 137 (impératif
  capitalisé), 138 (rien d'évident).
- `GetHelpContent()` de chaque vue — Rule 114 impose la mise à jour **dans le
  même commit**.
- `tui-layout.md` : Rule 111 réécrite (alias vim retirés, `Shift+S` mort
  supprimé, justification `c` du viewer corrigée), et la règle des trois espaces
  de noms ajoutée avec ses deux exceptions déclarées.
- `.claude/CLAUDE.md` : la section clavier.

## Validation

```bash
mise run fmt
mise run vet
mise run lint      # obligatoire avant commit (Rule 301)
mise run test
mise run test-race # les Cmd de Bubble Tea (Rule 110)
go build ./...
```

Plus la vérification manuelle de la phase 2 : `ctrl+p` dans Windows Terminal,
iTerm2, tmux.

## Risques

| Risque | Probabilité | Parade |
|---|---|---|
| `ctrl+p` n'arrive pas dans un émulateur | Faible | Vérifié statiquement ; **à confirmer à la main en phase 2, avant tout le reste** |
| Les tests de scan de source cassent au premier refactor cosmétique | Moyenne | Message d'échec qui nomme le fichier, la ligne et la règle |
| Un `GetShortcuts()` oublié annonce une touche morte | **Élevée** — 15 vues | Le test de contrat existant couvre la présence, pas le contenu ; passe manuelle par vue en phase 9 |
| Rupture d'habitude pour un public terminal-natif | Certaine | Assumée par §3.26 : « se paie une fois » |
| Conflit sur `docs/backlog.md` si une autre branche vit | Moyenne | Garder les deux côtés, comme d'habitude |

## Découpage en PR

Une seule PR serait illisible. **Cinq**, chacune verte seule :

| PR | Phases | Pourquoi séparée |
|---|---|---|
| 1 | 1 + 2 | Le vocabulaire, ses tests, et `ctrl+p`. Livre la vérification manuelle avant tout investissement |
| 2 | 3 + 4 | Les deux corrections de *forme*. Aucune touche d'action ne bouge |
| 3 | 5 + 9 (partiel) | Le rebinding, aide comprise |
| 4 | 6 + 7 | Les modales et le filtre — le seul comportement neuf |
| 5 | 8 + 9 (règles) | Le réglage et les documents |

## Acceptation

- [ ] Les trois tests du vocabulaire passent, et échouaient avant
- [ ] `pgup`/`pgdown` fonctionnent dans les 4 vues où ils mouraient
- [ ] Aucune lettre nue n'est de la navigation
- [ ] `ctrl+k` ne tue plus un processus de l'hôte sans confirmation
- [ ] `alt+:` n'apparaît plus nulle part, aide comprise
- [ ] `mise run check` vert, `test-race` vert
