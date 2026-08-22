# Plan : la vue `sec` affiche les images par leur alias de registry

**Source** : demande libre — « dans la vue sec, les images devraient utiliser les alias
de registry s'il en existe au lieu d'afficher le nom complet du registre, comme c'est
fait dans la vue oci/images ».
**Complexité** : Small

## Résumé

L'inventaire de `:sec` affiche la clé de cache brute pour une image
(`nexus.example.com/docker-hosted/agent-base:1.0`), là où l'onglet Images d'`:oci`
substitue déjà l'alias configuré (`nx/agent-base:1.0`). C'est la table la plus serrée
de l'application — `Target` partage sa largeur avec Secrets, quatre colonnes de
sévérité et Scanned — donc le préfixe est précisément ce qui pousse le nom utile hors
de la cellule. La substitution existe (`docker.ApplyAliases`) ; il manque le fait de
l'appeler ici, et un endroit d'où l'appeler deux fois sans la réécrire.

## L'invariant qui gouverne tout le reste

**`scanTarget.Name` est la clé de cache et ne bouge pas.** C'est ce que lisent
`loadInventoryResultCmd`, `rescanCmd`, `markScanning`, et `AddToGitleaksIgnore` via
`m.targetPath` (`update.go:22`). La règle est déjà écrite pour le pliage de `~`
(`inventory_table.go:62-65`) : *le nom affiché est dérivé, la clé est portée*. Un alias
posé sur `Name` remonterait jusqu'au scanner et jusqu'au répertoire dans lequel le
`.gitleaksignore` est écrit.

Conséquence : l'alias voyage sur **la ligne**, pas sur la clé — le patron `imageRow`
(`oci_resources/table.go:26`), pour la raison habituelle : les colonnes sont
construites une fois dans `New` et ne peuvent pas atteindre le modèle.

## Patrons à reprendre

| Catégorie | Source | Patron |
|---|---|---|
| Substitution | `internal/docker/registry.go:247` | `ApplyAliases(name, []RegistryAlias)` — premier préfixe qui matche |
| Décoration de ligne | `internal/ui/oci_resources/table.go:151-175` | `imageRows()` construit les alias depuis `m.registries` et stampe `DisplayName` |
| Colonne | `internal/ui/oci_resources/table.go:110-118` | `Cell` = affiché, `Less` = brut, `Search` = **les deux** |
| Estampillage hors Cmd | `internal/ui/security/inventory.go:238` | `setInventory` pose déjà `SpinnerFrame` par ligne, depuis `Update` |
| Nom dérivé | `internal/ui/security/inventory_table.go:66` | `displayName()` — icône + forme courte, la clé reste intacte |

## Fichiers touchés

| Fichier | Action | Pourquoi |
|---|---|---|
| `internal/ui/registryalias/alias.go` | CREATE | adapte `[]config.RegistryItem` vers `[]docker.RegistryAlias` ; deuxième appelant = extraction (Rule 201) |
| `internal/ui/registryalias/alias_test.go` | CREATE | entrée sans alias ignorée, ordre de déclaration préservé |
| `internal/ui/security/inventory_table.go` | UPDATE | champ `Display` sur `scanTarget`, `displayName()` le lit, `Search` prend les deux noms |
| `internal/ui/security/inventory.go` | UPDATE | `setInventory` stampe `Display` ; les trois messages de footer nomment la forme courte |
| `internal/ui/security/header.go` | UPDATE | l'annotation du titre passe par la forme courte |
| `internal/ui/security/model.go` | UPDATE | `targetLabel` à côté de `targetPath` — le second reste la clé |
| `internal/ui/oci_resources/table.go` | UPDATE | `imageRows` appelle l'extraction au lieu de sa boucle locale |
| `internal/ui/security/inventory_alias_test.go` | CREATE | les tests listés plus bas |
| `.claude/CLAUDE.md`, `docs/backlog.md` | UPDATE | section « The security inventory » + entrée §3.36 |

## Où placer l'extraction, et pourquoi pas ailleurs

`internal/docker` ne connaît pas `internal/config` — délibérément : c'est le pilote de
la CLI Docker, et `RegistryAlias` est son propre type pour cette raison. Lui faire
importer le schéma de configuration coupleraient un pilote à un fichier YAML. L'inverse
(`config` important `docker`) est pire.

L'adaptateur va donc **au-dessus des deux**, côté UI, dans un paquet d'une seule
fonction :

```go
// internal/ui/registryalias
func From(items []config.RegistryItem) []docker.RegistryAlias
```

C'est le seul emplacement qui n'invente aucune dépendance nouvelle entre paquets
existants.

## Tâches

### Tâche 1 — `internal/ui/registryalias`

- **Action** : `From()` — ignore une entrée dont `Alias` ou `URL` est vide, préserve
  l'ordre de déclaration (c'est lui qui départage deux préfixes qui se chevauchent,
  puisque `ApplyAliases` prend le premier).
- **Mirror** : la boucle actuelle de `imageRows`, déplacée telle quelle.
- **Valide** : `go test ./internal/ui/registryalias/...`

### Tâche 2 — `oci_resources` appelle l'extraction

- **Action** : remplacer la boucle de `imageRows()` par `registryalias.From(m.registries)`.
- **Valide** : `go test ./internal/ui/oci_resources/...` — aucun test ne doit changer,
  c'est ce qui prouve que l'extraction ne change rien.

### Tâche 3 — `scanTarget` porte son nom affiché

- **Action** : champ `Display string`, stampé dans `setInventory` à côté de
  `SpinnerFrame` :

```go
aliases := registryalias.From(m.config.Registry.Registries)
// ...
if t.Kind == kindImage {
    t.Display = docker.ApplyAliases(t.Name, aliases)
}
```

  `displayName()` lit `Display` et retombe sur `Name` quand il est vide — une ligne
  fabriquée par un test, ou par un chemin qui ne passe pas par `setInventory`, doit
  rester lisible plutôt que vide.
- **Mirror** : `setInventory` / `imageRow.DisplayName`.
- **Valide** : `go test ./internal/ui/security/...`

### Tâche 4 — la colonne cherche les deux noms

- **Action** : `Search: func(t scanTarget) string { return t.Name + " " + t.Display }`.
  `Less` reste sur `Name` : trier sur l'alias regrouperait les images par une chaîne que
  l'utilisateur peut renommer, et déplacerait toutes les lignes d'un registry le jour où
  il en change.
- **Mirror** : `imageColumns()`, commentaire compris — « la colonne dit un nom et la
  requête en veut un autre » est le défaut que ça évite.

### Tâche 5 — le titre et les trois messages

- **Action** : `m.targetLabel` posé partout où `m.targetPath` l'est
  (`NewWithPreloadedResult`, `handleInventoryResultLoaded`), lu par `GetTitle`.
  `targetPath` **reste** la clé — c'est lui que lit `AddToGitleaksIgnore`. Les footers
  de `inventory.go:50`, `:194` et `:223` nomment la forme courte : le footer tronque à
  la largeur (Rule 128), et c'est le préfixe qui mange la place.
- **Risque à surveiller** : ne pas remplacer `targetPath` par erreur — voir Risques.

### Tâche 6 — documentation

- **Action** : `.claude/CLAUDE.md` (§ The security inventory) note que la colonne Target
  substitue l'alias comme l'onglet Images, et que la clé de cache ne bouge pas ;
  `docs/backlog.md` gagne §3.36.

## Validation

```bash
go build ./...
go test ./internal/ui/security/... ./internal/ui/oci_resources/... ./internal/ui/registryalias/...
go test ./...
golangci-lint run
gofmt -l .
```

## Tests à écrire

| Test | Ce qu'il empêche |
|---|---|
| `TestAnImageRowShowsItsRegistryAlias` | la substitution ne se fait pas |
| `TestTheCacheKeyIsNeverAliased` | `Name` mute, donc le rescan et le chargement du résultat visent une clé inexistante |
| `TestARepositoryTargetIsUntouchedByAliases` | un chemin absolu qui commence par une URL de registry configurée |
| `TestTheTargetColumnSearchesBothNames` | l'alias affiché n'est pas cherchable |
| `TestAnImageWithNoConfiguredAliasKeepsItsFullName` | le repli |
| `TestTheTitleShowsTheAliasAndTheIgnorePathDoesNot` | Tâche 5, le risque principal |

## Risques

| Risque | Probabilité | Mitigation |
|---|---|---|
| `m.targetPath` aliasé par mégarde, donc `.gitleaksignore` écrit dans un répertoire inexistant | Moyenne | champ séparé `targetLabel` ; `TestTheTitleShowsTheAliasAndTheIgnorePathDoesNot` |
| Un `scanTarget` construit hors `setInventory` affiche une cellule vide | Moyenne | `displayName()` retombe sur `Name` ; les fixtures existantes (`secrets_column_test.go`, `inventory_test.go`) en construisent à la main |
| Un alias ambigu (deux registries préfixes l'un de l'autre) | Faible | comportement inchangé — `ApplyAliases` prend le premier déclaré, comme dans `:oci` |
| L'extraction casse `:oci` | Faible | Tâche 2 ne doit toucher aucun test |

## Hors périmètre

- Les workspaces (`kindRepo`) : ils plient déjà `~`, et un chemin n'a pas de registry.
- La colonne `Target` de la vue résultats : elle affiche un fichier, pas une image.
- Le tri par alias (voir Tâche 4).

## Acceptation

- [ ] Une image d'un registry aliasé s'affiche `nx/agent-base:1.0` dans `:sec`
- [ ] `enter`, `S` et `A` continuent de viser la bonne entrée de cache
- [ ] `/` trouve la ligne par l'alias **et** par le nom complet
- [ ] `:oci` inchangé, ses tests inchangés
- [ ] `go test ./...`, `golangci-lint run`, `gofmt -l .` verts
