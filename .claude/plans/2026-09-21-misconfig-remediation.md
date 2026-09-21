# §3.78 — Remédier une misconfiguration : le MCP d'abord, un catalogue borné ensuite

## Contexte

§3.2 a construit une remédiation pour **une** classe de findings : les CVE de
paquets système, corrigées en déplaçant l'image de base. L'onglet
Misconfigurations, lui, ne propose toujours que de lire.

§3.78 tranche la suite en **deux étages qui ne se concurrencent pas** :

1. le serveur MCP rend à l'agent appelant tout ce qu'il faut pour corriger
   n'importe quelle règle — c'est ce qui couvre le cas général ;
2. un catalogue écrit ici corrige, dans le TUI, les quelques règles Dockerfile
   qui reviennent le plus — c'est ce qui sert l'utilisateur sans agent.

L'ordre compte : **l'étage 2 du backlog est la phase A de ce plan.** Il ne
demande aucune décision d'interface, débloque immédiatement le cas ouvert, et
c'est en le livrant qu'on verra quelles règles méritent un correctif écrit à la
main.

## Décisions prises

| # | Décision | Motif |
|---|---|---|
| 1 | Le serveur MCP **continue de n'écrire aucun fichier** | `internal/mcp/scan_tools.go:66` — « This server writes nothing at all ». DevDesk mesure, l'agent écrit, DevDesk re-mesure. Un outil MCP qui appliquerait le patch déplacerait la responsabilité du fichier dans un process que l'utilisateur ne regarde pas |
| 2 | Le catalogue est **délibérément fini** | Il ne cherche pas à couvrir Trivy, il couvre ce qui se répète. Une règle absente n'est pas un manque : c'est le cas de la phase A |
| 3 | Pas de quatrième scanner | Semgrep sait produire un span + un remplacement (`--dryrun --json` → `start.offset`, `end.offset`, `extra.fix`), mais ses règles et celles de Trivy sont deux catalogues sans correspondance. Deux comptes qui se contredisent à l'écran coûtent plus que l'autofix ne rapporte |
| 4 | Pas de LLM embarqué | La règle du projet n'est pas « pas de LLM », elle est « mesuré, jamais inféré ». Le LLM du cas ouvert est celui de l'agent appelant, et c'est le re-scan qui le juge |
| 5 | Dans le TUI, `ctrl+o` sur l'onglet Misconfigurations | Pas un septième onglet. La sélection y désigne déjà la règle, et `ctrl+o` est déjà déclaré en exception (`keymap.DeclaredExceptions()`) avec le sens « écrire le fichier », qui s'étend sans se déformer |
| 6 | La nature de la cible est **un champ**, pas une heuristique | Un chemin qui ressemble à une référence d'image existe. Deviner, c'est envoyer l'agent écrire dans un rootfs |

---

## Phase A — le MCP rend tout ce qu'il faut (PR 1)

Aucune interface, aucun nouveau paquet. Quatre champs et leur propagation.

### A1. Trois champs perdus au parsing

`internal/scan/trivy.go:217-232` construit le `Finding` d'une misconfiguration
et jette trois choses que Trivy a déjà rendues.

**Rien n'est à ajouter au décodage** : `TrivyMisconfiguration` (ligne 63) porte
déjà `Message`, `Status` et `CauseMetadata.EndLine` — vérifié le 2026-09-21.
Les trois sont décodés depuis toujours et jetés à la construction du `Finding`.
Le travail est donc de les **porter**, pas de les lire.

Sur `scan.Finding` :

```go
// EndLine is the last line of the block a misconfiguration faults, from
// Trivy's CauseMetadata. Line alone says where the block starts, which is not
// enough to know what to replace. Zero means unknown — an old cached result,
// or a rule that reports a point rather than a span.
EndLine int `json:"end_line,omitempty"`
// Message is the concrete instance of the rule ("Specify at least 1 USER
// command"), where Description is the rule's generic text.
Message string `json:"message,omitempty"`
// Status is what Trivy concluded for this rule on this target.
Status string `json:"status,omitempty"`
```

**Trois champs, pas plus.** Ils sont vides sur tout résultat déjà en cache, ce
qui se lit comme « inconnu » et jamais comme « zéro » — même convention que
`Class`/`Ecosystem` en §3.2. Aucune migration de cache : les champs
réapparaissent au scan suivant.

### A2. La nature de la cible

`scan_result` prend un `target` et ne dit pas ce que c'est. Une
misconfiguration d'une image pointe un fichier du rootfs, qu'aucun agent ne
peut éditer ; celle d'un dépôt pointe un fichier sur disque.

Sur `scanResultOut` (`internal/mcp/scan_tools.go`) :

```go
TargetKind string `json:"target_kind" jsonschema:"image or repository; a
  finding's file is a path inside the image for an image target and is not
  editable, while for a repository it is relative to the repository root"`
Root string `json:"root,omitempty" jsonschema:"absolute path a repository
  target's file paths are relative to; absent for an image target"`
```

`TargetKind` vient de la même source que l'inventaire (`inventory()` sépare
déjà images et chemins), **pas** d'une inspection de la chaîne.

### A3. La projection

Ajouter `EndLine`, `Message`, `Status` à la structure `finding`
(`scan_tools.go:135`), avec leurs `jsonschema`. L'interdit de `Match` ne bouge
pas : aucun des trois ne peut porter un secret — ils viennent du scanner de
misconfiguration, qui ne lit pas de valeurs.

Mettre à jour `toolDescription("scan_result")` pour dire la boucle que l'agent
peut fermer : corriger, `workspace_scan_start`, `jobs_get`, `scan_result`, et
constater que l'AVD ID a disparu.

### A4. Tests

| Test | Ce qu'il fixe |
|---|---|
| `TestAMisconfigurationCarriesItsSpan` | `EndLine` survit du JSON Trivy au `Finding` |
| `TestAnOldCachedResultHasNoSpan` | `EndLine` à zéro se lit « inconnu », et rien ne le compte comme une ligne |
| `TestAnImageTargetSaysItsFilesAreNotOnDisk` | `TargetKind == "image"` et `Root` absent |
| `TestARepositoryTargetCarriesItsRoot` | `Root` absolu, `File` relatif, et leur jointure existe |
| `TestTheMatchedStringOfASecretNeverLeaves` (existant) | inchangé — vérifier qu'il couvre les trois nouveaux champs |

---

## Phase B — le catalogue, et `ctrl+o` sur l'onglet Misconfigurations (PR 2)

À écrire **après** avoir vu, via la phase A, quelles règles reviennent. Les
quatre ci-dessous sont l'hypothèse de départ, pas la liste arrêtée.

### B0. Sortir la machinerie d'écriture de `internal/dockerfile`

`Rewrite`, `Diff` et `WriteIfUnchanged` ne connaissent rien aux Dockerfiles :
elles opèrent sur des octets et des spans. Les déplacer dans `internal/patch`,
avec leurs tests. C'est un renommage — aucune signature ne change, et
`internal/dockerfile` importe le nouveau paquet.

À faire **en premier et seul dans son commit** : un déplacement mêlé à une
fonctionnalité rend la revue impossible.

### B1. Le catalogue — `internal/remediation/misconfig.go`

Pur, sans I/O, comme le reste du paquet.

```go
// Rule is a misconfiguration this application knows how to fix, keyed by the
// AVD ID Trivy reports. The catalog is deliberately finite: it covers what
// recurs, not what Trivy detects. A rule absent from it is not a gap — it is
// what the MCP server hands to a calling agent instead.
type Rule struct {
    AVDID string
    Title string
    // Fix returns the edits that satisfy the rule on content, or nil with a
    // reason when this instance is not one the catalog handles. It never
    // guesses: an instance it cannot read exactly is one it declines.
    Fix func(content []byte, f scan.Finding) ([]patch.Edit, string)
}
```

**`Fix` décline plutôt que d'approximer.** C'est la contrepartie du catalogue
fini : une correction douteuse coûte plus cher qu'une absence, puisque
l'absence renvoie à la phase A.

Les quatre premières règles, toutes Dockerfile, toutes à span connu :

| Règle | Correction |
|---|---|
| utilisateur root | insérer un `USER` avant le premier `CMD`/`ENTRYPOINT` |
| base en `:latest` | remplacer le tag — réutilise le résolveur de §3.2 |
| `HEALTHCHECK` absent | insérer la directive |
| `apt-get upgrade` | retirer l'invocation de la ligne `RUN` |

### B2. `ctrl+o` sur l'onglet

Le flux de §3.2 phase C est repris tel quel : sélection → diff → confirmation
par défaut sur **No** → `patch.WriteIfUnchanged`.

Grisage (Rule 130), une seule `Availability` lue par l'en-tête et par le
handler :

| État | Raison |
|---|---|
| la règle n'est pas au catalogue | `reasonNoFixForRule` — c'est aussi ce qui rend la frontière des deux étages **visible** plutôt que devinée |
| la cible est une image | `reasonFileNotOnDisk` |
| `Fix` a décliné cette instance | la raison que `Fix` a rendue |

### B3. Le re-scan de vérification

**Après** l'écriture, jamais avant : c'est ce qui distingue « écrit » de
« corrigé ». Un travail au sens de `internal/jobs` (§3.58), donc annulable et
visible dans `:jobs`. Le verdict est **binaire** — l'AVD ID est là ou il n'y
est plus — et non un delta : les colonnes « avant / après » de l'onglet
Remediation n'ont aucun sens ici, il faut une colonne d'état.

### B4. Plusieurs règles sur un même fichier

Hors périmètre de cette PR. Une correction par validation. `patch.Rewrite` sait
déjà refuser deux éditions qui se recouvrent (`ErrConflict`), donc le lot est
une fonctionnalité à ajouter, pas un défaut à éviter.

### B5. Tests

| Test | Ce qu'il fixe |
|---|---|
| un test par règle, sur un Dockerfile d'exemple | l'édition produite, et le fichier inchangé ailleurs |
| `TestAFixDeclinesRatherThanApproximates` | une instance illisible rend une raison, jamais une édition |
| `TestARuleOutsideTheCatalogIsGreyedNotHidden` | Rule 130 |
| `TestAnImageTargetCannotBeWritten` | le grisage précède le handler |
| `TestShortcutKeys` (par vue) | l'ensemble des touches ne change pas d'un état à l'autre |

---

## Hors périmètre

- **YAML / Kubernetes / Terraform.** `Rewrite` par span n'y suffit pas : la
  correction est souvent *ajouter une clé dans un mapping*, ce qui demande un
  parseur préservant indentation et commentaires. La phase A les couvre sans
  rien construire, puisque l'agent édite du texte.
- **L'onglet CI (plumber).** Même forme — un constat, pas un patch — et la
  même question se posera, après.
- **Semgrep.** Si le catalogue devient trop gros à maintenir, sa sortie
  `--dryrun --json` se verse dans `patch.Edit` sans adaptateur. La phase A rend
  ce besoin peu probable.

## Documentation, à chaque PR

- `docs/architecture/scanning.md` — les champs de `Finding`, le catalogue
- `docs/architecture/mcp.md` — `TargetKind`, `Root`, la boucle de vérification
- `docs/backlog.md` §3.78 — l'état
- `GetHelpContent()` (Rule 114) et `GetShortcuts()` pour la phase B

## Vérification, à chaque PR

`mise run check` (fmt + vet + lint + test), et `mise run test-race` pour toute
PR touchant un `Cmd`.
