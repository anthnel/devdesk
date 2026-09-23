# Scanners — catégories, outils, onglet Tools, statut au dashboard

**Statut (2026-09-23) : PR 1 et PR 2 implémentées** (branche
`feat/scan-tools-config`, voir §3.86 du backlog pour ce qui a été fait et les
écarts). PR 3 et 4 à faire.
Base : `main` à `80ab2fc0` (#257, §3.80 livré — kubeconform, helm, kustomize).

Écarts de la PR 1 au texte ci-dessous, à reprendre dans les suivantes :

- `Category` et `CategoryID` existaient déjà dans `scan` (la famille d'un
  finding) : la table s'appelle `ToolCategory`, ses identifiants
  `CategoryIDVuln`…
- `Report` ne porte pas `Required` : `Missing(categories)` et
  `CanScan(categories, target)` prennent les catégories en argument.
- `Detect` prend `config.ScanTools`, pas tout `ScanConfig`.
- Les identifiants et les défauts vivent aussi dans `config`
  (`ToolIDs`, `CategoryIDs`, `DefaultScanCategories`), faute d'import possible ;
  un test garde l'accord. Pas d'accesseur dans `Tool` : `ScanTools.Tool(id)`.
- `ToolConfig.Config` existe déjà (gitleaks, plumber) ; `Args` et
  `ReservedArgs` attendent la PR 4.

Écarts de la PR 2 :

- Pas de `ScanToolsChangedMsg` : le routeur compare lui-même les réglages de
  la dernière détection (`scan.SameDetection`).
- Les vues reçoivent `shared.ScanToolsMsg` plutôt que de lire `shared.State`
  (seuls le dashboard et l'explorateur tiennent `shared.State`). Le dashboard,
  lui, lit `shared.State.Tools`.
- Le moteur et git sont dans `Report` (`EngineVersion`, `GitAvailable`,
  `GitVersion`) : l'onglet Tools de la PR 3 les y trouvera, avec
  `scan.CleanVersion`.

## Contexte

L'utilisateur choisit aujourd'hui des **catégories** (six cases dans l'onglet
`scan`), jamais des **outils** : le lien catégorie → outil est écrit en dur dans
`Scanner.missingToolErrors` (`internal/scan/scanner.go:582`), et l'indice de la
case Secrets dit « Gitleaks » alors que Trivy cherche aussi des secrets.

Constats qui motivent le plan :

| # | Constat | Où |
|---|---|---|
| 1 | Le dashboard compare la machine à une liste **fixe** (`knownTools`) : Plumber est « manquant » avec le score CI éteint, et depuis #257 helm et kustomize le sont aussi — alors que §3.80 a décidé qu'ils ne seraient « jamais signalés manquants ». | `internal/ui/dashboard/sections.go` `toolsBlock`, `missingTools`, `knownTools` |
| 2 | L'onglet `scan` compte ~40 champs et 9 titres de groupe, soit ~70 lignes ; `Model.height` est stocké et **rien ne défile** : le bas est coupé. | `internal/ui/configuration/view.go` `View` |
| 3 | Chaque outil ajoute 3 champs à `ScanConfig`, 5 à `DependencyStatus`, 3 à `ScanOptions`, 3 à `toolConfig()`, une `*Spec()`, un bloc dans `CheckDependencies`, un bloc dans `detectTools` et une constante `knownTools`. Six outils, six copies. | `config.go`, `scanner.go`, `options.go`, `tool_source.go`, `dashboard/model.go` |
| 4 | Plumber absent n'est pas signalé par `missingToolErrors` (sauté en silence dans `plumber_target.go`). | `internal/scan/scanner.go:582` |
| 5 | Le mode serveur Trivy éteint **la catégorie** Misconfiguration entière, alors que seul Trivy est concerné — kubeconform, lui, n'a pas de serveur. | `configuration/model.go` `isDisabled`, `serverModeFields` |
| 6 | `CheckDependencies` est appelé indépendamment par le dashboard, `ws`, `oci`, `templates` et chaque `NewScanner`. | — |

## Décisions prises

| Question | Réponse |
|---|---|
| Modèle | Une table déclarative **catégorie × outils** dans `internal/scan`, seule source de vérité. Un outil est **requis** s'il est coché dans une catégorie activée. |
| kubeconform | Devient un **outil de la catégorie Misconfiguration** (à côté de Trivy), plutôt qu'une septième catégorie « K8s schema » : `Categorize` range déjà ses findings dans cet onglet (#257). |
| helm, kustomize | Des outils de Misconfiguration qui **dépendent** de kubeconform (ils rendent, ils ne détectent pas). Choisis explicitement : cochés, ils sont requis ; décochés, ils ne servent pas même installés. **Révise la décision de §3.80** (« optionnels, jamais signalés manquants ») : l'utilisateur veut que ses choix disent ce qui est requis. |
| Moteur de conteneurs, Git | Toujours requis : ce sont des outils de plateforme, pas des scanners. Affichés dans l'onglet Tools, sans réglage (le moteur se règle dans `app`). |
| Réglages par outil | `source`, `binary`, `image`, `config` (seulement pour les outils qui ont un fichier de règles), `args`. Plus les réglages propres à l'outil (serveur Trivy, historique Gitleaks, version Kubernetes…), qui quittent l'onglet `scan`. |
| Onglet `scan` | Les catégories et leurs outils (cases imbriquées), la remédiation, les limites. |
| Onglet `tools` (nouveau) | Un groupe par outil, l'état dans le titre du groupe (requis / disponible / source / version), les réglages dessous, **sur deux colonnes**. |
| Navigation deux colonnes | `↓` parcourt la colonne de gauche puis enchaîne en haut de celle de droite. `←→` (cycle) et `tab` (onglets) sont déjà pris par Rule 135. |
| Dashboard | Une ligne, hauteur fixe : `All tools are available 󰗠` / `Some tools are missing 󰅙`. Le détail est dans l'onglet Tools. |
| Détection | **Une seule**, tenue par le routeur dans `shared.State` et lue par toutes les vues. Relancée au démarrage, au changement de contexte, quand un réglage d'outil change, et sur `ctrl+r` (dashboard et onglet Tools). Le tick périodique du dashboard ne la relance plus. |

## Modèle

### Tables (`internal/scan/toolbox.go`)

```go
type ToolID string // "trivy", "gitleaks", "plumber", "kubeconform", "helm", "kustomize"

type Tool struct {
    ID           ToolID
    Name         string   // "Trivy" — ce que l'utilisateur tape pour l'installer
    Binary       string   // nom sur PATH
    DefaultImage string
    VersionArgs  []string
    HasConfig    bool     // trivy, gitleaks, plumber
    ReservedArgs []string // drapeaux que DevDesk pose lui-même, refusés dans args
}

type CategoryID string // "vuln", "secret", "misconfig", "license", "ci"

type CategoryTool struct {
    Tool      ToolID
    Role      string       // "security rules", "API schema", "renders charts"
    DependsOn ToolID       // helm, kustomize → kubeconform
    Targets   []TargetType // gitleaks: directory seulement ; trivy/secret : les deux
    Server    bool         // false quand un serveur Trivy ne sait pas le faire
}

type Category struct {
    ID    CategoryID
    Label string
    Tools []CategoryTool
}
```

| Catégorie | Outils (défaut coché en gras) | Cibles |
|---|---|---|
| Vulnerabilities | **Trivy** | dir, image |
| Secrets | **Trivy**, **Gitleaks** | Trivy : dir, image — Gitleaks : dir |
| Misconfiguration | **Trivy**, Kubeconform → (Helm, Kustomize) | Trivy : dir, image — les autres : dir |
| Licenses | **Trivy** | dir |
| CI | **Plumber** | dir |

Fonctions pures, testées sans machine :

- `Required(c config.ScanConfig) []ToolID` — catégorie activée ∧ outil coché ∧
  dépendance cochée.
- `Report` = `map[ToolID]ToolStatus{Required, Available, Source, Binary, Image, Version}`,
  plus `EngineAvailable`, `ImageScanSocket` (inchangés). Remplace les 30 champs
  de `DependencyStatus` ; `TrivySpec()`… deviennent `Report.Spec(id)`.
- `Report.Missing() []ToolID` — requis et indisponibles, dans l'ordre de la table.
- `Report.CanScan(t TargetType) bool` — au moins un outil coché,
  disponible et applicable à `t` dans une catégorie activée. Remplace les
  lectures directes de `deps.TrivyAvailable` (10 sites) pour griser `S`/`A`
  (`reasonNoScanner`).
- `missingToolErrors` se lit sur la table : Plumber est signalé comme les autres
  (constat 4).

### Schéma de config

```yaml
scan:
  categories:
    vuln:      { enabled: true,  tools: [trivy] }
    secret:    { enabled: true,  tools: [trivy, gitleaks] }
    misconfig: { enabled: false, tools: [trivy] }
    license:   { enabled: false, tools: [trivy] }
    ci:        { enabled: false, tools: [plumber] }
  tools:
    trivy:
      source: auto          # auto | binary | image
      binary: ""
      image: ""
      config: ""            # trivy.yaml
      args: []
      server: { enabled: false, url: "" }
      ignore_unfixed: false
      ignore_eol: false
    gitleaks:    { source, binary, image, config, args, history }
    plumber:     { source, binary, image, config, args }
    kubeconform: { source, binary, image, args, kubernetes_version: 1.36.0 }
    helm:        { source, binary, image, args }
    kustomize:   { source, binary, image, args }
  base_image_track: same-line
  timeout: …                # limites inchangées
```

Côté Go : une struct typée par outil, qui embarque un `ToolConfig{Source,
Binary, Image, Config, Args}` commun. La table `Tool` porte un accesseur
`func(*config.ScanConfig) *config.ToolConfig`, pour que détection, résolution
et formulaire bouclent sur la table au lieu de copier un bloc par outil.
Une catégorie éteinte **garde** sa liste d'outils, comme `trivy_server` garde
son adresse quand la case est décochée.

### Migration (`internal/config/scan_migrate.go`)

Même forme que `migrateGitLabSection` : avant les défauts, champ par champ,
seulement là où la nouvelle valeur est vide, et les clés plates vidées pour
qu'elles quittent le fichier à la sauvegarde suivante (struct `legacyScan`
embarquée, tags `omitempty`).

| Ancienne clé | Nouvelle |
|---|---|
| `<tool>_source/_path/_image` | `tools.<tool>.source/binary/image` |
| `use_trivy_server`, `trivy_server` | `tools.trivy.server.enabled/url` |
| `ignore_unfixed`, `ignore_eol` | `tools.trivy.*` |
| `gitleaks_config`, `gitleaks_history` | `tools.gitleaks.config/history` |
| `plumber_config` | `tools.plumber.config` |
| `kubernetes_version` | `tools.kubeconform.kubernetes_version` |
| `enable_vuln/secret/license/ci_score` | `categories.*.enabled`, outils par défaut |
| `enable_misconfig`, `enable_k8s_schema` | `misconfig.enabled` = l'un ou l'autre ; `tools` = trivy si le premier, kubeconform **+ helm + kustomize** si le second (ils servaient déjà dès qu'installés — ils deviennent requis, voir « Risques ») |

L'heuristique « tout éteint = jamais configuré » ne s'applique plus qu'à un
fichier **sans** `categories` : un `categories` présent est une réponse, même
tout éteint.

## Interface

### Onglet `scan`

```
  󰒃 Categories

  [x] Vulnerabilities                     Trivy
  [x] Secrets
        [x] Trivy
        [x] Gitleaks                      git history, repositories only
  [ ] Misconfiguration
        [x] Trivy                         security rules
        [ ] Kubeconform                   API schema, repositories only
              [ ] Helm                    renders charts first
              [ ] Kustomize               builds overlays first
  [ ] Licenses                            Trivy
  [ ] CI                                  Plumber

  Remediation / Limits (inchangés)
```

- Une catégorie à un seul outil n'a pas de sous-ligne : l'outil est écrit en
  gris à droite (rien à choisir).
- Sous une catégorie décochée, les outils sont grisés (`RenderCheckboxDisabled`)
  mais restent affichés : la mise en page ne bouge pas.
- Serveur Trivy actif : **la ligne Trivy** de Misconfiguration et de Licenses
  est grisée, pas la catégorie (constat 5). Licenses n'ayant plus d'outil
  utilisable, sa case est grisée aussi.
- Décocher le dernier outil d'une catégorie activée est refusé : `Warn`
  « A category needs at least one tool — turn the category off instead »
  (Rule 128, Rule 130 : le refus n'est jamais silencieux).
- Les `field` gagnent un `Depth int` pour l'indentation ; le focus reste une
  liste plate. Nouveau type de champ `kindToolToggle` (catégorie, outil) dont
  l'accesseur lit/écrit l'appartenance à `categories.<id>.tools`.

### L'effet d'une case, expliqué selon son état

Aujourd'hui une case a **un** indice fixe (`field.hint`, affiché dans le footer
quand elle a le focus), qui nomme l'outil — « Trivy », « Gitleaks » — sans dire
ce que change le fait de cocher. Chaque case dit désormais **ce qui se passe
dans l'état où elle est**, et l'indice change quand on appuie sur `space` :
l'utilisateur voit l'effet de son geste au moment où il le fait.

- `field` gagne `hintOn` / `hintOff` (cases seulement) ; `hint` reste pour les
  autres champs. `current().hint` devient une méthode qui lit l'état de la case
  dans la config en cours — un seul endroit décide du texte affiché.
- Le texte est un **état**, pas un événement : il passe par `Status` (Rule 128,
  pas de minuteur), comme l'indice actuel.
- Chaque texte dit **deux choses** quand elles s'appliquent : ce qui est
  scanné ou non, et ce qui devient requis ou non. C'est ce second point qui
  relie la case à la ligne du dashboard.
- Une ligne, centrée, anglais US (Rule 129) ; assez court pour tenir à 80
  colonnes.

Exemples (formulation à finaliser à l'implémentation) :

| Case | Cochée | Décochée |
|---|---|---|
| Secrets › Gitleaks | Git history is searched for secrets — gitleaks is required | Only the files on disk are searched, by Trivy — gitleaks is not required |
| Misconfiguration › Kubeconform | Manifests are checked against the Kubernetes API schema — kubeconform is required | Manifests are only linted for security by Trivy |
| Kubeconform › Helm | Charts are linted and rendered, then validated — helm is required | Charts are not validated; they are counted as Not rendered |
| Kubeconform › Kustomize | Overlays are built, then validated — kustomize is required | Overlays are not validated; they are counted as Not rendered |
| Gitleaks › Scan git history | Every commit is searched — slower | Only the current tree is searched |
| Trivy › Use Trivy server | Scans go to the server; Trivy no longer checks misconfigurations or licenses | Trivy runs locally |

Une case **grisée** a son propre texte, qui dit pourquoi : « Not supported by
a Trivy server », « Turn Misconfiguration on first ». Ce qui répond à la
question qu'un utilisateur se pose devant une case qui ne réagit pas.

### Onglet `tools`

Placé après `scan` : `app · forge · scan · tools · network · mcp · status`.

```
  Platform                                   │  Gitleaks  󰗠 required · binary · 8.30.1
    Docker        󰗠 29.7.2                   │    Source ⌄ › auto
    Git           󰗠 2.51.0                   │    Binary   ›
                                             │    Image    › zricethezav/gitleaks
  Trivy  󰗠 required · image · 0.71.2         │    Config   › ~/.gitleaks.toml
    Source ⌄ › auto                          │    Args     ›
    Binary   ›                               │    [ ] Scan git history
    Image    › aquasec/trivy                 │
    Config   ›                               │  Plumber  not used
    Args     › --skip-dirs vendor            │    …
    [ ] Use Trivy server                     │
    Server   ›                               │  Kubeconform  󰅙 required · missing
    [ ] Ignore unfixed                       │    …
    [ ] Ignore end-of-life                   │
```

- **État dans le titre du groupe**, lecture seule :
  requis + disponible → `IconOK` vert ; requis + absent → `IconError` rouge
  (Rule 121) ; non requis → `not used` en `DimStyle`, disponible ou non —
  un outil qu'on n'utilise pas n'a pas à alarmer. Suivi de la source effective
  et de la version quand l'outil a répondu.
- **Deux colonnes** : groupes répartis en ordre, coupe au plus proche de la
  moitié des lignes. Chaque ligne construite à la main, colonne gauche
  complétée par `PadWithBg` (Rule 115, pas de `JoinHorizontal`). Largeur
  minimale mesurée (tête la plus large + valeur raisonnable, ~2 × 58) ;
  en dessous, une colonne.
- `config` n'est affiché que pour `Tool.HasConfig` ; `args` pour tous.
- `args` : édité comme un texte (séparé par espaces, guillemets respectés,
  découpeur maison — pas de dépendance), stocké en liste YAML. Un drapeau de
  `Tool.ReservedArgs` (`--format`, `--scanners`, `--output`, `--server`,
  `--report-*`, `-output`, `--print`, `--score`, `--provider`…) est refusé
  à la saisie, avec son nom (Rule 128 `Error`, le champ garde l'ancienne
  valeur).
- **Détection** : l'onglet lit le `Report` partagé (voir « Détection
  partagée ») et n'en lance aucune lui-même ; `…` (`unknownValue`) tant qu'il
  n'est pas là ou qu'une détection est en vol. Le calcul de `Required` est pur
  et suit immédiatement chaque case cochée, sans attendre de détection.
- `ctrl+r` sur l'onglet redemande une détection (Rule 111 : refresh, et rien
  d'autre).

### Défilement (tous les onglets)

`View` calcule toutes les lignes, puis une fenêtre de `m.height` lignes qui
garde le champ focalisé visible (marge d'une ligne). En deux colonnes, c'est la
ligne du champ dans sa colonne qui compte. Règle le constat 2 pour tous les
onglets, y compris les suivants.

### Dashboard

`toolsBlock` devient une ligne, sans libellé :

- chargement : `Tools …` (inchangé) ;
- `Report.Missing()` vide : `All tools are available 󰗠` (`StatusOKStyle`) ;
- sinon : `Some tools are missing 󰅙` (`StatusDownStyle`).

`knownTools`, `missingTools`, les constantes `tool*` et `detectTools`
disparaissent : le bloc lit le `Report` partagé. `refreshAll` (le tick
périodique) ne détecte plus ; `ctrl+r` (`handleReload`) redemande une détection
en plus de ce qu'il fait déjà. La hauteur du bloc ne dépend plus des données :
le commentaire qui justifiait l'exception s'en va.

### Détection partagée

Une détection lance des processus (`docker images -q` par outil en mode image,
`<tool> --version` par outil) : la faire une fois et la lire partout est à la
fois plus juste et moins coûteux.

- **Où elle vit.** `shared.State.Tools` devient `*scan.Report` (il était
  `[]ToolInfo`, écrit par le dashboard et lu par personne). `nil` = pas encore
  de réponse.
- **Qui la lance.** Le routeur, et lui seul, dans un `Cmd` qui reçoit une
  **copie** de `ScanConfig` (Rule 110) et rend `ScanToolsDetectedMsg{Gen,
  Report}`. Le routeur l'écrit dans `shared.State` dans son `Update`.
- **Quand.**

  | Déclencheur | Message |
  |---|---|
  | démarrage | `Init` du routeur |
  | changement de contexte | `handleContextSwitchComplete` |
  | réglage d'outil enregistré (source, binary, image) | `ScanToolsChangedMsg`, émis par la vue config |
  | `ctrl+r` sur le dashboard ou l'onglet Tools | `ScanToolsDetectRequestMsg` |

  Une case catégorie/outil ne redemande **pas** de détection : elle ne change
  que `Required`, qui se recalcule à la lecture (`Report` garde la
  disponibilité, `scan.Required(cfg)` dit ce qui est requis — la vue combine
  les deux).
- **Détections qui se chevauchent.** Chaque demande incrémente `Gen` ; un
  résultat dont `Gen` n'est pas le dernier est jeté. Sans ça, une détection
  lente lancée avant un changement de source pourrait arriver après la
  suivante et réafficher l'ancien état.
- **Qui la lit.** Dashboard, onglet Tools, `ws`, `oci`, `:sec`, `templates`
  — leurs `DepsCheckedMsg` et leurs `checkDepsCmd` disparaissent. Tant que
  `Report` est `nil`, `S`/`A` restent offerts (Rule 130 : ne pas savoir n'est
  pas savoir que non).
- **Les scans.** `NewScanner` reçoit le `Report` (copié au lancement du
  `Cmd`) au lieu de refaire une détection par scan. La résolution garde son
  échec bruyant : un outil disparu entre la détection et le scan échoue à
  l'exécution, avec le message de l'étage, comme aujourd'hui.
- **Le MCP.** `scan_start` passe par les mêmes vues, donc par le même
  `Report` ; rien à ajouter.

## Exécution des outils

- `ToolSpec` gagne `Config` et `Args`.
- **Position des `args`** : après la sous-commande et avant les arguments de
  DevDesk et la cible. kubeconform utilise le paquet `flag` de Go, qui s'arrête
  au premier argument positionnel — des drapeaux placés après la cible seraient
  lus comme des fichiers. Les outils cobra (trivy, gitleaks, plumber, helm,
  kustomize) acceptent les deux positions.
- helm reçoit ses `args` sur `lint` **et** sur `template` : documenter que seuls
  les drapeaux communs aux deux ont un sens (`--values`, `--set`…).
- **`config`** : même traitement que `gitleaks_config` (§3.50) — chemin rendu
  absolu au chargement, monté à une position fixe à la racine du conteneur en
  mode image, refusé avant lancement s'il est illisible (`docker run -v` crée
  un répertoire sinon). Trivy : `--config /trivy.yaml`. Un `trivy.yaml` qui
  pose `format:` casse le parsing : à mesurer, et à surcharger par la ligne de
  commande (qui gagne sur le fichier chez Trivy) plutôt qu'à interdire.
- La commande affichée (`Get*Command`) inclut `config` et `args` : ce qui est
  montré est ce qui tourne (D19).

## Découpage — quatre PR

### PR 1 — le modèle, sans changement visible

1. `internal/scan/toolbox.go` : `Tool`, `Category`, les tables, `Required`,
   `Report`, `Missing`, `CanScan`, `Spec`.
2. `config` : `ToolConfig`, structs par outil, `Categories`, migration,
   `ExpandPaths` et défauts sur la table.
3. `CheckDependencies` → `Detect(c config.ScanConfig) Report`, une boucle sur
   la table. `ScanOptions` porte `Categories` + `Tools` ; `toolConfig()` disparaît.
4. Les consommateurs de `DependencyStatus` passent à `Report` (`ws`, `oci`,
   `:sec`, `templates`, MCP `context_get`/`scan_tools`, `about`).
5. `missingToolErrors` et les étages du scanner lisent les catégories.
6. `fields.go` : pointeurs mis à jour, mise en page de l'onglet `scan` inchangée.
7. Backlog : nouvelle entrée §3.86 (catégories et outils), §3.80 annoté sur
   helm/kustomize.

Tests : migration (chaque clé, fichier mixte, `categories` présent tout éteint),
`Required` (dépendance helm → kubeconform, catégorie éteinte), `CanScan` par
type de cible, `TestEveryConfiguredOptionReachesTheScanner` étendu à **tous**
les outils de la table (il aurait attrapé le bug plumber de #257).

### PR 2 — détection partagée, dashboard et disponibilité

1. `shared.State.Tools` → `*scan.Report` ; détection dans le routeur,
   `ScanToolsDetectedMsg` avec génération, `ScanToolsChangedMsg`,
   `ScanToolsDetectRequestMsg`.
2. `ws`, `oci`, `:sec`, `templates` lisent le `Report` partagé ;
   `DepsCheckedMsg` et leurs `Cmd` de détection supprimés. `NewScanner` prend
   le `Report`.
3. Dashboard : `toolsBlock` sur `Report.Missing()`, une ligne ; `refreshAll`
   ne détecte plus ; `ctrl+r` redétecte.
4. `S`/`A` grisés sur `CanScan`.
5. Aide (Rule 114) du dashboard ; `docs/architecture/app-shell.md` (nouveau
   message inter-vues, `shared.State`).

Tests : Plumber absent et CI éteint → « All tools are available » ; helm absent
et non coché → idem ; coché → « Some tools are missing » ; hauteur constante ;
un résultat de génération périmée est jeté ; `ctrl+r` sur le dashboard émet la
demande ; changement de contexte → nouvelle détection ; `Report` nil →
`S` offert.

### PR 3 — l'interface de configuration

1. Défilement dans `View`.
2. Onglet `scan` : catégories imbriquées, `kindToolToggle`, refus du dernier
   outil, grisé serveur Trivy par outil.
3. Indices selon l'état (`hintOn` / `hintOff`, texte de la case grisée), sur
   les cases des onglets `scan` et `tools`.
4. Onglet `tools` : groupes par outil générés depuis la table, titre d'état,
   deux colonnes, lecture du `Report` partagé, `ScanToolsChangedMsg` quand un
   champ source/binary/image s'enregistre, `ctrl+r` → détection.
5. `GetShortcuts` inchangé dans sa forme (Rule 130 : même ensemble de touches
   sur les deux onglets), `GetHelpContent` mis à jour.

Tests : focus `↓` gauche → droite ; repli une colonne sous la largeur minimale ;
le champ focalisé est toujours dans la fenêtre ; aucune ligne plus large que
`m.width` ; `TestNoViewStylesItsOwnFooterMessage` et les tests de raccourcis
existants passent ; `space` sur une case change le texte du footer ;
`TestEveryCheckboxExplainsBothStates` parcourt les onglets `scan` et `tools`
et échoue, en nommant la case, sur un `hintOn` ou un `hintOff` vide.

### PR 4 — `config` et `args`

1. `ToolSpec.Config/Args`, insertion dans les six constructeurs de commande,
   montage des fichiers de config.
2. Champs `Config`/`Args` dans l'onglet `tools`, validation `ReservedArgs`.
3. Docs : `docs/architecture/scanning.md`, `configuration.md`,
   `docs-site/docs/reference/configuration.md`, `explanation/scanning.md`.

Tests : position des `args` par outil (kubeconform avant la cible), drapeau
réservé refusé, config montée en mode image, commande affichée = commande lancée.

### Hors plan

- Ouvrir la configuration **sur l'onglet Tools** depuis le dashboard
  (`:config tools`, ou une touche) : le parseur ne passe d'arguments qu'aux
  commandes de contexte aujourd'hui.

## Risques

| Risque | Parade |
|---|---|
| Un utilisateur avec `enable_k8s_schema` et sans helm voit « Some tools are missing » après migration | Voulu (ses choix disent ce qui est requis) ; noté dans le CHANGELOG via le message de commit et dans §3.86. Décocher Helm le fait disparaître. |
| `args` casse le JSON de sortie | `ReservedArgs` par outil ; le parsing échoue déjà proprement (`recordStageError`), mais le refus à la saisie dit **quel** drapeau. |
| Onglet `tools` illisible sur un terminal étroit | Repli une colonne + défilement. |
| Migration qui écrase une valeur nouvelle | Champ par champ, seulement si vide — le précédent `forge:`. |
| Deux colonnes et Rule 135 | Aucune touche nouvelle : l'ordre de focus reste la liste plate. |
| Un outil installé pendant que DevDesk tourne n'est pas vu (plus de détection périodique) | `ctrl+r` sur le dashboard ou l'onglet Tools ; l'aide des deux le dit. |
| Une détection périmée écrase une plus récente | Numéro de génération, résultat ancien jeté. |
