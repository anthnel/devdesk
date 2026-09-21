# §3.2 — Remédiation de sécurité : corrigibles, images de base candidates, écriture du Dockerfile

## Contexte

§3.2 du backlog cadre l'auto-patch (le SAST local est déjà fait). Trois
positions y sont posées et ce plan les tient pour acquises :

1. **Tout est déterministe.** La classe OS/dépendance vient du rapport Trivy,
   la version corrigée aussi ; le `FROM` se parse, les tags se listent, et
   qu'un bump corrige est une **mesure** — le re-scan — jamais un avis.
2. **Pas de LLM embarqué.** Le jugement (montées majeures, changement de
   distribution, CVE sans correctif) passe par un client externe via MCP.
3. **DevDesk n'écrit le Dockerfile que si l'utilisateur le demande et le
   confirme.** Ni commit, ni branche, ni push.

## Décisions prises

| Question | Réponse |
|---|---|
| Découpage | un seul plan, trois PR (A, B, C), point d'arrêt après A |
| Écran de B/C | un onglet **Remediation** dans les résultats de scan d'un workspace (`:sec`) |
| Tags proposés | **même ligne** par défaut (même majeure, même suffixe de variante) ; réglable par `scan.base_image_track` (`same-line` \| `next-major`) |
| Stages | **tous les stages** : une CVE d'un stage de build peut finir dans l'image livrée (toolchain, bibliothèques liées statiquement) |
| Touche d'écriture | `ctrl+o`, **exception déclarée** (`keymap.DeclaredExceptions()`) sur `security/results/remediation-tab` |
| Déclenchement des re-scans | à la demande (`S` sur l'onglet), jamais à l'ouverture |
| Candidats | 3 au plus par image, plus l'image actuelle comme référence « avant » |
| Mode de scan des candidats | `trivy image --image-src remote` : aucun `docker pull`, le stockage d'images local reste intact |
| Cache des résultats de B | **à part** du cache d'images de `:sec` (sinon les candidats apparaîtraient dans l'inventaire comme des images de l'utilisateur) |
| Outils MCP (candidats, re-scan) | hors de ce plan — PR ultérieure |

### Pourquoi `ctrl+o`

- **Précédent :** `^O` est « Write Out » dans nano, l'éditeur de terminal le
  plus répandu.
- **Sans conflit en mode raw :** bubbletea passe le terminal en raw, ce qui
  désactive `VDISCARD`, le seul rôle de `^O` dans un tty (macOS/BSD).
- **Les autres candidats sont exclus :**
  - `ctrl+w` ferme l'onglet dans les navigateurs, dans les terminaux web et,
    selon la configuration, dans VS Code ;
  - `ctrl+s` est le contrôle de flux (XOFF) ;
  - `ctrl+d` est EOF, et « delete » dans le vocabulaire.

`TestOnlyThreeCtrlCombinationsSurvive` laisse passer une exception déclarée ;
c'est le chemin qu'a pris `ctrl+y`.

---

## Phase A — les corrigibles (PR 1)

Sans réseau, sans nouvel écran.

### A1. La classe et l'écosystème dans `Finding`

`internal/scan/scanner.go` : deux champs, tous deux `omitempty`.

- `Class` : `os-pkgs` \| `lang-pkgs` \| `""`, recopié de `TrivyResult.Class`.
- `Ecosystem` : `TrivyResult.Type` (`alpine`, `debian`, `gomod`, `npm`,
  `pip`, `cargo`, …).

`parseTrivyOutput` (`internal/scan/trivy.go`) les remplit pour les
vulnérabilités seulement.

**Un résultat en cache n'a pas ces champs** : sa classe est inconnue. Il n'est
jamais compté comme OS ; il le devient au prochain scan.

### A2. `internal/remediation` — le regroupement, pur

Nouveau package, sans I/O :

- `Group(findings []scan.Finding) []Fix`, une entrée par (écosystème, paquet).
  Une `Fix` porte :
  - la version installée ;
  - la version cible, c'est-à-dire la plus haute des `FixedIn` de ses CVE.
    La comparaison est semver quand les deux versions se parsent ; sinon, les
    versions distinctes sont listées et aucune n'est choisie ;
  - les identifiants des CVE et la pire sévérité.
- `Command(fix) (string, bool)` : une table par écosystème.

  | Écosystème | Commande |
  |---|---|
  | `gomod` | `go get pkg@vX` (le `v` est ajouté s'il manque) |
  | `npm` / `yarn` / `pnpm` | `npm install pkg@X` / `yarn add pkg@X` / `pnpm add pkg@X` |
  | `pip` / `pipenv` / `poetry` | `pip install pkg==X` / … |
  | `cargo` | `cargo update -p pkg --precise X` |
  | `alpine` | `apk upgrade pkg` |
  | `debian` / `ubuntu` | `apt-get install --only-upgrade pkg` |
  | autre | pas de commande : la version cible seule |

  `false` signifie « pas de commande connue », jamais une commande inventée.
- **`FixCommand` est dérivé de `Command`**, qui remplace le texte
  `"Update pkg to X"`. Le champ reste pour le cache et pour MCP.

### A3. Affichage

- **Résultats (`internal/ui/security`) :** un champ `GetHeaderInfo`
  « Fixable », au format `12 · 8 base / 4 deps`, avec la classe inconnue
  comptée à part si elle existe.
  - Rule 139 : rien dans le corps.
  - Rule 122 : `DimStyle` pour les zéros.
- **Détails :** la classe (« Base image package » / « Application
  dependency ») et la commande, à la place du texte actuel.
- **Aide :** `GetHelpContent` explique ce que signifie « Fixable » (Rule 114).

### A4. Tests

- `parseTrivyOutput` : classe et écosystème recopiés, absents pour les
  secrets, licences et misconfigs.
- `Group` en table : plusieurs CVE sur un même paquet donnent la version
  cible la plus haute ; des versions non comparables ne donnent aucun choix ;
  deux écosystèmes pour un même nom restent séparés.
- `Command` : chaque ligne de la table, plus l'écosystème inconnu.
- **Un résultat en cache sans `Class`** n'est pas compté comme OS.

**Point d'arrêt :** revue d'A en usage réel avant d'attaquer B.

---

## Phase B — images de base candidates, prouvées par re-scan (PR 2)

### B0. Vérifications avant de coder

À mesurer, pas à supposer. Chaque résultat est consigné dans le §3.2.

1. **`--image-src remote`** fonctionne dans les trois modes de Trivy : binaire,
   conteneur (`wrapTrivy`, sans socket Docker monté) et serveur (`--server`).
2. **La pagination de `/v2/<repo>/tags/list`** sur Docker Hub, pour une
   image officielle à gros volume (`library/node`) : `n`/`last` ou en-tête
   `Link`, et combien de tags reviennent sans pagination.
3. **Le peuplement de l'inventaire `:sec`** : confirmer qu'il se fait depuis
   le cache d'images, ce qui justifie un cache séparé pour B.

### B1. Un seul listeur de tags, hors de l'UI

Deux implémentations existent : `internal/oci/oci.go:51` (sans
authentification) et `internal/ui/oci_resources/commands.go` —
`fetchRegistryTags`, avec le challenge Bearer (`exchangeBearerToken`,
`parseBearerChallenge`) et la réécriture Docker Hub.

- La seconde est **déplacée** dans `internal/oci`, avec la pagination
  mesurée en B0.
- Le navigateur de registry l'appelle à cet endroit.
- `oci.Client.ListTags` s'y rallie ou disparaît, selon ses appelants
  (`entire graph impact`).
- Les identifiants viennent de `docker.GetStoredCreds` et des registries
  configurés ; un registry inconnu est interrogé en anonyme.

Refactor sans changement de comportement pour le navigateur : ses tests
existants passent inchangés.

### B2. `internal/dockerfile` — le parseur

- **Ce que `Parse(content []byte) (File, error)` extrait :**
  - chaque instruction `FROM`, avec les **positions en octets** de la
    référence d'image dans le fichier (le patch de C remplace ces octets et
    rien d'autre) ;
  - le nom du stage (`AS name`), `--platform`, le digest.
- **Continuations de ligne :** `\` et la directive `# escape=`.
- **`ARG` globaux** (avant le premier `FROM`) : leur valeur par défaut est
  résolue. Une référence `${VAR}` sans défaut est marquée *non résolvable* :
  elle est affichée sans candidat, avec la raison.
- **Ce qui n'est pas une image** et est marqué comme tel, sans erreur :
  `FROM scratch`, et un `FROM` qui nomme un stage précédent.
- **Localisation de la valeur :** quand l'image vient d'un `ARG`, la position
  retenue est celle de la valeur **dans la ligne `ARG`**. C'est l'endroit où
  elle est écrite, donc celui que C modifiera.

### B3. Référence d'image et politique de tags

`internal/remediation/tags.go`, pur :

- `ParseRef` découpe `registry/repo:tag@digest`, en appliquant les défauts
  Docker Hub (`library/`).
- `SplitTag` découpe un tag en version + suffixe de variante :
  - `3.20.1-alpine3.19` → version `3.20.1`, variante `alpine3.19` ;
  - `bookworm-slim` → pas de version, variante `bookworm-slim` ;
  - `latest` → pas de version.
- `Candidates(current, tags, track, max)` :
  - même variante exacte ;
  - version strictement supérieure ;
  - `same-line` : même majeure ; `next-major` : majeure actuelle ou suivante ;
  - tri décroissant, `max` premiers.
- **Pas de version parsable** (`latest`, un nom de code) : aucun candidat, et
  une raison nommée.
- **Référence épinglée par digest :** les candidats sont proposés sur le tag
  s'il existe ; digest seul → aucun candidat, raison nommée.

### B4. Scan distant

- `scan.Options` gagne `ImageSource` (`""` \| `remote`). `trivyArgs` ajoute
  `--image-src remote` pour une cible image quand l'option est posée.
- Le scan des candidats ne lance que le stage vulnérabilités, en respectant
  `ignore_unfixed` / `ignore_eol` comme les autres scans.

### B5. Configuration

- `scan.base_image_track` dans `ScanConfig` (`internal/config/config.go`) :
  `same-line` par défaut, écrit au chargement comme `container_engine` (un
  ensemble fermé a besoin d'une valeur de départ).
- **Portée :** le réglage est **par contexte**, comme tout le bloc `scan:` ;
  il n'existe pas de niveau de configuration au-dessus des contextes.
- La vue configuration, onglet Scan, l'affiche en **champ cyclique** `←→`
  (Rule 132).
- `docs/architecture/configuration.md` est mis à jour.

### B6. L'onglet Remediation

`internal/ui/security` : un nouvel onglet dans les résultats. `tabCount`
augmente ; il n'a pas de catégorie dans `tabCategory`, comme `TabCIScore`.

- **Présence :** l'onglet existe pour une cible workspace. Pour une cible
  image, il est présent mais vide, et son en-tête dit pourquoi (« No source
  Dockerfile for an image »). L'ensemble des onglets ne change pas selon la
  cible (Rule 130).
- **Découverte :** les Dockerfiles du workspace, reconnus comme `fileicon`
  les reconnaît (`Dockerfile`, `Dockerfile.*`, `*.Dockerfile`). Profondeur
  bornée ; `.git`, `vendor` et `node_modules` sont ignorés.
- **Table (`datatable`)** — une ligne par (fichier, stage, image) : l'image
  actuelle, puis ses candidats en dessous.

  | Colonne | Sizing |
  |---|---|
  | File | content, optionnelle |
  | Stage | content |
  | Image | content |
  | CRIT | fixed |
  | HIGH | fixed |
  | Δ vs current | fixed |
  | Scanned | fixed, `theme.TimeAgo` |

  Les cellules sont en texte brut, la couleur passe par `Style` (Rule 122).
- **`S` :** scanne l'image actuelle et les candidats non scannés, via le
  registre de jobs.
  - Une même image présente dans plusieurs stages est dédupliquée : un seul
    scan.
  - La concurrence respecte `max_concurrent_scans`.
  - Pendant les scans, le spinner est au footer (Rule 128).
- **Cache :** `internal/cache/remediation.go`, clé = référence d'image
  complète, valeur = comptes par sévérité + `ScannedAt`. Il est séparé du
  cache d'images (voir B0).
- **Raisons de grisage** (constantes du package, Rule 130) : pas de
  Dockerfile, image non résolvable, pas de candidat, scanner absent.
- **Aide et raccourcis** mis à jour (Rules 114, 137, 138).

### B7. Tests

- Parseur : table de Dockerfiles (multi-stage, `ARG` avec et sans défaut,
  `--platform`, digest, `scratch`, référence de stage, continuations,
  `# escape=`), avec les positions en octets vérifiées.
- `SplitTag` / `Candidates` : les deux politiques, variantes, tags non
  parsables, plafond.
- `trivyArgs` avec `ImageSource: remote`.
- Listeur de tags : serveur HTTP de test avec challenge Bearer et pagination.
- Onglet :
  - ensemble de touches identique entre les états (`testutil.ShortcutKeys`) ;
  - corps toujours une table (Rule 139) ;
  - largeur rendue (Rule 116) ;
  - `TestEveryColumnDeclaresItsSizing`.
- Config : défaut écrit au chargement, cycle dans la vue.

---

## Phase C — écrire le Dockerfile (PR 3)

### C1. Sélection et diff

- `space` choisit un candidat pour son stage (un seul par stage ; `space` à
  nouveau le retire). La ligne actuelle ne se sélectionne pas.
- `enter` ouvre le **diff** des lignes concernées dans le viewer, avec
  `esc` pour revenir.
- **Condition :** seul un candidat **scanné** se sélectionne. Un candidat non
  prouvé ne va pas dans un patch, ce qui est la position du §3.2. Sinon,
  `space` est grisé et refusé au footer (Rule 130).

### C2. `dockerfile.Rewrite`

`Rewrite(content []byte, edits []Edit) ([]byte, error)` :

- remplace **uniquement** les octets repérés en B2 ;
- recopie tout le reste tel quel : commentaires, CRLF, absence de newline
  final ;
- refuse deux modifications qui se chevauchent.

### C3. L'écriture — `ctrl+o`

1. `ctrl+o` sans sélection : grisé et refusé au footer (`Warn`).
2. Sinon, un **modal** (Rule 104, choix par défaut **No**) :
   - il liste les fichiers et les lignes `FROM`/`ARG` modifiés ;
   - il donne **l'état git** de chaque fichier (`internal/git` si un helper
     existe, sinon `git status --porcelain -- <file>` et
     `git ls-files --error-unmatch`) :
     - suivi et propre → rien de plus à dire ;
     - modifications non commitées → « Undoing with git checkout would also
       discard your own uncommitted changes » ;
     - non suivi / hors d'un dépôt → « Not tracked by git — this write cannot
       be undone with git ».
   - Aucun de ces cas ne bloque.
3. Sur **Yes**, un `Cmd` (Rule 110 : aucune mutation du modèle dedans) :
   1. relit le fichier et compare son **empreinte** (sha256) à celle prise au
      calcul du diff. Si elle diffère, il refuse avec un message au footer et
      le diff est à recalculer ;
   2. écrit de façon **atomique** : fichier temporaire dans le même
      répertoire, permissions recopiées, puis `os.Rename` (qui remplace aussi
      sous Windows) ;
   3. renvoie `RemediationWrittenMsg` ou `RemediationWriteErrorMsg`.
4. `Update` : un `Info` au footer (« Dockerfile updated — review with git
   diff »). Le Dockerfile est reparsé, et l'image nouvellement écrite devient
   la ligne « actuelle ».
5. Déclaration de l'exception `ctrl+o` dans `internal/ui/keymap/keymap.go`,
   avec son `Why`. Mise à jour de Rule 111 (`tui-layout.md`, section des
   exceptions : trois au lieu de deux).

### C4. Tests

- `Rewrite` : fichiers de référence (CRLF, sans newline final,
  commentaires, `ARG`, plusieurs stages), chevauchement refusé.
- Empreinte différente → aucune écriture.
- Écriture atomique : les permissions sont conservées, et aucun fichier
  temporaire ne reste après une erreur.
- Modal : choix par défaut No ; `esc` n'écrit rien ; les trois messages
  d'état git.
- `ctrl+o` grisé sans sélection, refusé avec une raison.
- `TestOnlyThreeCtrlCombinationsSurvive` passe grâce à l'exception déclarée,
  et échoue sans elle (vérifié contre le code sans la déclaration).

---

## Documentation, à chaque PR

- `docs/backlog.md` §3.2 : état de chaque phase, résultats de B0.
- `docs/architecture/scanning.md` : `Class`/`Ecosystem`, `internal/remediation`,
  l'onglet, le cache séparé.
- `docs/architecture/configuration.md` : `scan.base_image_track`.
- `.claude/rules/tui-layout.md` : l'exception `ctrl+o` (PR 3).

## Vérification, à chaque PR

`mise run check` (fmt, vet, lint, test) et `mise run test-race`. Essai sur
l'hôte (Docker réel, registry réel) avec un workspace dont le Dockerfile a
une image de base vieillie (par exemple `alpine:3.18`) :

- **A :** les compteurs.
- **B :** les candidats et les deltas.
- **C :** l'écriture, puis `git diff` sur le dépôt de test.
