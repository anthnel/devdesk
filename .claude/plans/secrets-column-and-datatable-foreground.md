# Plan — colonne Secrets (sec, oci/images, dashboard) + foreground des datatables

**Complexité** : Medium
**Branche** : `dashboard-skeleton` (travail non commité en cours)

## 1. Ce qui est demandé

1. Afficher les secrets détectés dans **`sec`** (inventaire) et **`oci/images`**, dans une
   nouvelle colonne, sur le modèle de la vue **`ws`**.
2. Ajouter l'information au **dashboard**, boîte Health, sous **Repositories** et **Images**.
3. Corriger le **foreground** des datatables (`oci/images`, `oci/network`, `oci/volumes`,
   `sec`, `status/monitor`) : le texte ne prend pas la couleur du thème. Vérifier **tous**
   les datatables.

## 2. Ce que l'exploration a trouvé

### 2.1 Le verdict « secrets » est faux là où il existe, et absent ailleurs

| Endroit | État |
|---|---|
| `cache.WorkspaceScanEntry.Sensitive bool` | existe, alimenté |
| `cache.ImageScanEntry` | **aucun champ** — une image n'a jamais rien enregistré |
| `workspaces/commands.go:175-181` | `f.Source == "gitleaks"` |
| `security/inventory_commands.go:218` (`hasScanSource`) | `f.Source == "gitleaks"` |

Les deux calculs sont la **même boucle recopiée**, et tous deux sont faux depuis que Trivy
détecte lui aussi des secrets : un secret trouvé par Trivy porte `SourceTrivySecret`, donc un
dépôt dont ce sont les seuls secrets se lit **propre**. `scan.Categorize` est censé être le
seul classeur (CLAUDE.md, « scan.Categorize is the only thing that decides a finding's
family ») ; `result.SecretCount` en est déjà le résultat.

### 2.2 Le verdict n'est pas binaire, il est ternaire

Un scan peut ne **pas avoir cherché** de secrets : `EnableSecret` à faux, ou l'outil absent
(`scanner.go:403` et `:429` exigent tous deux `deps.*Available`). Un `false` dans ce cas
affiche l'icône verte « rien trouvé » pour un scan qui n'a rien regardé — exactement ce que
D20 interdit déjà en toutes lettres dans `Scan()` :

> Recording the reason up front is what stops "nothing looked at this image" being reported
> as "this image is fine".

C'est aussi ce qui rend les entrées de cache existantes racontables : une entrée image écrite
avant aujourd'hui n'a pas cherché de secrets, et doit se lire `inconnu`, pas `propre`.

### 2.3 Le foreground : une seule cause, dans `theme`

`theme.DefaultTableStyles()` et `BlurredTableStyles()` règlent `Header` et `Selected` et
**jamais `Cell`** ; `bubbles/table.DefaultStyles()` ne lui donne qu'un `Padding(0,1)`.
`datatable.cellStyle` complète le **fond** d'une cellule sans `Style` (Rule 115) mais pas le
**texte** : toute colonne sans `Style` sort donc dans le foreground par défaut du terminal,
sur lequel le thème n'a aucune prise.

Quatre vues l'avaient contourné à la main, ce qui est la signature du défaut :

| Site | Code |
|---|---|
| `containers/model.go:213` | `lipgloss.NewStyle().Foreground(theme.ColorText)` |
| `oci_resources/table.go:71` | idem |
| `security/inventory_table.go:126` | idem |
| `workspaces/columns.go:121` | idem |

**Le correctif ne peut pas aller dans `DefaultTableStyles()`.** bubbles — et `datatable` —
rendent les cellules puis emballent la ligne entière dans `styles.Selected` : un foreground
sur `Cell` émettrait une séquence à l'intérieur de la ligne sélectionnée, dont le reset
couperait le surlignage en plein milieu. C'est le défaut décrit par la Rule 122. La couleur
doit donc être posée **par cellule, par le renderer qui sait si la ligne est sélectionnée** :
`datatable.cellStyle`.

### 2.4 Périmètre de l'audit « tous les datatables »

Les 15 tables `datatable` sont couvertes par le correctif central (containers, status ×2,
workspaces, security ×2, oci ×3, netdiag ports, explorer ×2, …).

Quatre tables **ne sont pas** des `datatable` et gardent le foreground du terminal :

| Table | Fichier |
|---|---|
| Registries (onglet oci) | `oci_resources/table.go:295` |
| Tags du registry browser | `oci_resources/registry_browser.go:124` |
| Network inspect | `oci_resources/network_inspect_form.go:21` |
| Résultats netdiag | `netdiag/model.go:112` |

Aucune des cinq vues nommées par la demande n'en fait partie. Elles ne peuvent pas être
corrigées de la même façon (même conflit avec `Selected`) : la voie est la migration vers
`datatable`, proposée en suite, hors de ce plan.

## 3. Patterns à reprendre

| Catégorie | Source | Pattern |
|---|---|---|
| Colonne icône | `workspaces/columns.go:73-77` | `Cell` = icône brute, `Style` = verdict (Rule 122) |
| Décoration de ligne | `oci_resources/table.go:27` (`imageRow`) | ce que la donnée ne porte pas voyage sur la ligne |
| Tri-état « connu / inconnu » | `dashboard/posture.go:102` | `(valeur, mesuré)`, `-` et non `0` |
| Nœud d'arbre | `dashboard/sections.go:405` | `narrowBranch(last, label, value)` |
| Cache versionné | `cache/scan_file.go` | champ absent = ancien fichier, jamais une valeur inventée |
| Tests | `dashboard/posture_test.go` | un garde-fou vérifié en cassant le code |

## 4. Fichiers touchés

| Fichier | Action | Pourquoi |
|---|---|---|
| `internal/scan/scanner.go` | UPDATE | `Result.SecretsScanned`, `Result.SecretVerdict()` |
| `internal/cache/image_scan.go` | UPDATE | `Sensitive *bool` |
| `internal/cache/workspace_scan.go` | UPDATE | `Sensitive bool` → `*bool` |
| `internal/ui/theme/secrets.go` | CREATE | état, icône, style — une seule fois pour trois vues |
| `internal/ui/workspaces/{view,columns,commands,messages,actions}.go` | UPDATE | verdict ternaire, style partagé |
| `internal/ui/oci_resources/{table,commands}.go` | UPDATE | colonne + écriture du verdict |
| `internal/ui/security/{inventory_table,inventory_commands,inventory.go}` | UPDATE | colonne + écriture + message |
| `internal/ui/dashboard/{posture,sections}.go` | UPDATE | nœud `secrets` |
| `internal/ui/datatable/render.go` | UPDATE | foreground des cellules non sélectionnées |
| `internal/ui/{containers,oci_resources,security,workspaces}/…` | UPDATE | supprimer les 4 contournements |
| `docs/backlog.md`, `.claude/CLAUDE.md` | UPDATE | décisions + forme des caches |
| tests (voir §6) | CREATE/UPDATE | |

## 5. Tâches

### Phase 1 — le verdict, côté données

**T1. `scan.Result` porte ce qu'il a regardé.**
`SecretsScanned bool` mis à vrai quand une étape secrets (gitleaks ou trivy-secret) s'est
terminée **sans erreur**, à l'endroit où chaque étape range déjà son résultat.
`func (r *Result) SecretVerdict() *bool` : `nil` si rien n'a cherché, sinon
`SecretCount > 0`.
*Mirror* : `missingToolErrors` / D20, même fichier.

**T2. Les deux caches portent le même champ.**
`ImageScanEntry.Sensitive *bool` (`json:"sensitive,omitempty"`) et
`WorkspaceScanEntry.Sensitive bool → *bool`.
La migration est gratuite : l'ancien champ workspace s'écrivait **toujours**
(`json:"sensitive"`, sans `omitempty`), donc un fichier existant décode en pointeur non nul —
rien n'est perdu. Une entrée image existante n'a pas la clé → `nil` → `inconnu`, ce qui est
la vérité.

**T3. Un seul calcul, trois écrivains.**
`workspaces/commands.go`, `oci_resources/commands.go`, `security/inventory_commands.go`
(`storeRescan`, deux branches) appellent `result.SecretVerdict()`.
Suppression de `hasScanSource` et de la boucle `f.Source == "gitleaks"`.

**T4. Le verdict remonte dans les messages.**
`WorkspaceScanCompleteMsg.Sensitive` → `*bool` ; `InventoryScanFinishedMsg` gagne
`Sensitive *bool`, sans quoi la ligne rescannée garde son icône précédente jusqu'au prochain
`ctrl+r`.

### Phase 2 — la colonne

**T5. `internal/ui/theme/secrets.go`.**
```go
type SecretsState int
const (SecretsUnknown SecretsState = iota; SecretsClean; SecretsFound)
func SecretsVerdict(sensitive *bool, scanned bool) SecretsState
func SecretsIcon(SecretsState) string          // Unknown/Trusted/Untrusted
func SecretsStyle(SecretsState) lipgloss.Style // Dim / OK / Error
```
Remplace `workspaces.secretsStyle`, qui décide aujourd'hui la couleur en **comparant la chaîne
d'icône déjà rendue** — un renommage d'icône la casserait en silence.

**T6. `ws` passe sur le type partagé.** `formatSecrets` rend `SecretsIcon(SecretsVerdict(...))`.
L'agrégat d'un répertoire suit l'ordre `trouvé > inconnu > propre` : un sous-dépôt non
regardé ne peut pas rendre le parent « propre ».

**T7. `oci/images` — colonne `Secrets`, largeur 7**, après `Content Size`, avant `C H M L` —
la position qu'elle occupe dans `ws`. `imageRow` porte le verdict.

**T8. `sec` — colonne `Secrets`, largeur 7**, après `Target`. Elle prend un `Less`
(trouvés d'abord) : toutes les autres colonnes de cette table trient.
À 80 colonnes le solveur de `datatable` répartit le manque — `Target` rétrécit, rien ne
déborde.

### Phase 3 — dashboard

**T9. `postureSide` compte les cibles, pas les secrets.**
`Secrets int` (cibles au verdict « trouvé ») et `SecretsKnown int` (cibles dont le verdict
est connu). `add()` prend le `*bool`.

**T10. Nœud `secrets` dans `postureBranches`**, entre `critical` et `unscanned` : les deux
constats de findings d'abord, les deux constats de couverture ensuite ; `oldest` reste le
`IconTreeEnd`. Valeur `-` (dim) quand `SecretsKnown == 0`, sinon `severityCount(Secrets)`.

**T11. Uniquement au palier `wide`.** Les arbres `Repositories` et `Images` n'existent qu'à
`wide` ; aux paliers étroits Health est un bloc de six lignes dont le code dit déjà qu'une
septième ferait déborder l'overview. Health passe de 11 à 12 lignes, ce qui coûte une ligne
aux courbes via `fitCharts` — à vérifier, ainsi que `wideMinHeight = 42`
(`TestTheChosenPalierLosesTheFewestLines`).

### Phase 4 — foreground

**T12. `datatable.cellStyle`** — ligne **non** sélectionnée uniquement :
`Style == nil` → `Cell.Foreground(ColorText).Background(ColorBackground)` ;
`Style != nil` sans foreground → on complète `ColorText`, symétrique du fond déjà complété.
Ligne sélectionnée : inchangée.

**T13.** Les quatre contournements de §2.3 rendent le style zéro et laissent `cellStyle`
décider — la couleur « normale » cesse d'être écrite à quatre endroits.

### Phase 5 — traces

**T14.** `docs/backlog.md` : le verdict ternaire et pourquoi (D20), le classeur unique, le
foreground manquant et pourquoi il ne peut pas aller dans `DefaultTableStyles`, les quatre
tables bubbles restantes. `.claude/CLAUDE.md` : forme des deux caches, la nouvelle colonne.

## 6. Tests

| Test | Vérifie |
|---|---|
| `scan`: un secret Trivy seul rend un verdict « trouvé » | la boucle `gitleaks` échouait ici |
| `scan`: `EnableSecret=false` → `SecretVerdict() == nil` | pas de faux « propre » |
| `scan`: outil absent → `nil` | idem, par l'autre chemin |
| `cache`: un fichier workspace legacy décode en pointeur non nul | migration sans perte |
| `cache`: une entrée image sans clé décode `nil` | ancien scan = inconnu |
| `oci`/`sec`: la colonne rend les trois icônes | rendu |
| `dashboard`: `secrets` compte les cibles, `-` quand rien n'est connu | |
| `dashboard`: le nœud est présent dans les deux colonnes de Health | |
| `datatable`: une colonne sans `Style` porte `ColorText` | **cassé sur le code actuel** |
| `datatable`: la ligne sélectionnée n'émet aucun foreground de cellule | interdit le « correctif » dans `DefaultTableStyles` |

Les deux derniers sont vérifiés en cassant le code, comme les garde-fous de §3.19.

## 7. Validation

```bash
mise run fmt && mise run vet && mise run lint
go test ./...
go test -race ./internal/scan/... ./internal/cache/... ./internal/ui/datatable/... \
        ./internal/ui/dashboard/... ./internal/ui/workspaces/... \
        ./internal/ui/security/... ./internal/ui/oci_resources/...
```

## 8. Risques

| Risque | Probabilité | Atténuation |
|---|---|---|
| `Sensitive bool → *bool` casse la lecture d'un cache existant | Faible | la clé était toujours écrite ; test de décodage legacy |
| La 12ᵉ ligne de Health rogne les courbes ou fait tomber le palier `wide` | Moyenne | hauteurs mesurées, pas constantes ; re-mesure + test de palier |
| Un `Style` de colonne comptait sur le foreground du terminal | Faible | seul `StatusStyle`'s défaut ne pose pas de fg ; il prend `ColorText`, ce qu'il voulait dire |
| La colonne de plus serre `sec` à 80 colonnes | Faible | le solveur répartit ; `Target` est `Flex` |
| Le verdict passe `nil` pour des scans qui trouvaient avant | Attendu | c'est la correction : ils n'avaient rien cherché |

## 9. Hors périmètre (à confirmer séparément)

- Migration des quatre tables `bubbles` restantes vers `datatable` (§2.4).
- Un compte de secrets plutôt qu'un verdict (`3 secrets` au lieu d'une icône) — `ws` fixe la
  convention, et la demande dit « comme dans `ws` ».

## 10. Acceptation

- [ ] `sec` et `oci/images` portent la colonne, avec les trois états
- [ ] Health montre `secrets` sous `Repositories` et sous `Images`
- [ ] Un secret trouvé par Trivy seul est rapporté
- [ ] Un scan qui n'a pas cherché ne dit pas « propre »
- [ ] Tout texte de datatable prend `ColorText` ; le surlignage reste entier
- [ ] `mise run check` vert, `-race` vert
