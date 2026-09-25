# §3.82 — Vérifier la signature d'une image avant de la recommander ou de la tirer

Statut : **plan, rien d'implémenté.** Toutes les décisions sont dans §3.82 du
backlog (2026-09-25) ; ce plan ne les rediscute pas, il les découpe. Là où ce
plan et §3.82 divergent, §3.82 gagne — et l'écart se note ici.
Base : `main` à `efc80712` (#265).

## Contexte

Un registre compromis republie une image sous le même tag ; aucune CVE ne le
révèle. DevDesk recommande des images de base (§3.2, onglet Remediation) et en
tire (`G`, le navigateur de registre, le MCP) sans jamais demander si le contenu
est celui que l'éditeur a publié. Ce plan ajoute cette question, avec Cosign,
et en fait un verdict qui **bloque** ou **avertit** selon qui a déclaré la
règle.

## Rappel des décisions (détail dans §3.82)

- Politique : **C > B > A**. C = `~/.devdesk/trust.yaml`, global, strict. B =
  éditeurs connus, dans le code, une entrée par mesure (distroless au départ).
  A = continuité avec l'image en usage.
- Cosign seul en v1 ; le **mode** de la règle désigne l'outil (`key`,
  `keyless`, `expect: none`, `notation:` réservé et refusé).
- Verdicts `Verified`, `IdentityMismatch`, `Unsigned`, `NoPolicy`, `Failed`.
- Décision par verdict × source :

  | Verdict | C | B | A |
  |---|---|---|---|
  | IdentityMismatch | bloque | bloque | bloque |
  | Unsigned | bloque | bloque | avertit |
  | Failed | bloque | avertit | avertit |

- Classification keyless : 0 / 10 / 11 lus directement ; **tout autre code**
  (1, 12…) relance une vérification permissive (0 ⇒ IdentityMismatch,
  10 ⇒ Unsigned, autre ⇒ Failed). Jamais le texte de stderr.
  `--experimental-oci11` toujours.
- Classification en mode clé : 0 ⇒ Verified, 11 ⇒ tag absent, **tout le reste ⇒
  Unsigned** (« no verifiable signature from the expected key »), donc bloquant
  sous B comme sous C — *fail-closed*, écart au tableau limité au mode clé : une
  panne réseau bloque un pull DHI au lieu d'avertir.
- Pull : tag → digest → vérification → `pull repo@digest` → `tag`. Un seul point
  de passage.
- `scan.image_verification: on | off` par contexte, `on` par défaut ; `off`
  coupe tout et se voit.
- `cosign` dans la table des outils, dans aucune catégorie ; `scan.tools.cosign`.
- Cache : digest + empreinte de règle ; 24 h / 6 h / jamais pour un échec.
- Finding `DEVDESK-SIG-001` (CRITICAL) / `-002` (HIGH), étape `signature` de
  Misconfiguration, ancrée sur le `FROM`.

## Découpage — une étape de mesure, quatre étapes, **une seule PR**

Décidé avec l'utilisateur (2026-09-25) : tout se fait sur une seule branche —
les décisions et mesures de §3.82 déjà commitées, puis l'implémentation — et
part en **une seule PR**. Les quatre étapes ci-dessous restent l'ordre de
travail : chacune est **un ou plusieurs commits** qui compilent et passent
`mise run check` seuls, pour qu'un `git bisect` reste possible dans la
branche. La PR étant squashée, son titre décide seul de l'entrée de
changelog : `feat(images): verify image signatures before recommending or
pulling them (§3.82)`.

Aucun commit ne laisse de **réglage inerte** (la leçon de §3.14) :
`scan.image_verification` n'apparaît dans la vue configuration qu'avec l'étape
qui le lit la première fois (étape 2).

### Étape 0 — mesures faites

**Les sept points sont faits.** Points 1 à 6 le 2026-09-25 dans le sandbox
(Podman y est installé), résultats dans §3.82 (« Mesures de l'étape 0 »).
Point 7 le 2026-09-25 depuis l'hôte, résultat dans §3.82 (« Recoupement de la
clé DHI »). Liste d'origine :

1. ~~**Chainguard et DHI**~~ — **fait le 2026-09-25**, voir §3.82 (Chainguard
   keyless, DHI en mode clé avec `--experimental-oci11`).
2. **Mode clé sans Rekor** : la vérification DHI (bonne clé 0, mauvaise clé 10)
   est faite ; reste une signature *sans* entrée de transparence, dans un
   registre local (`registry:2`), vérifiée avec `--insecure-ignore-tlog` pour
   `tlog: false`.
3. **Identifiants privés** : un `DOCKER_CONFIG` temporaire ne contenant qu'un
   hôte, monté dans le conteneur cosign — cosign l'utilise-t-il bien ?
4. **Concurrence** : quatre `cosign verify` en parallèle sur le même `TUF_ROOT`
   — corruption, verrou, ou rien ? Si ça casse, un sémaphore à 1 autour de la
   première mise à jour TUF.
5. **Podman** : `pull repo@sha256:…` puis `tag` — même résultat que Docker,
   `RepoDigests` renseigné ?
6. **`docker run --pull=never`** sur Docker et Podman, image absente : message
   et code de sortie.
7. ~~**Recouper la clé DHI**~~ — **fait le 2026-09-25, depuis l'hôte**, voir
   §3.82 (« Recoupement de la clé DHI ») : trois sources indépendantes
   concordent (`registry.scout.docker.com`, le dépôt GitHub
   `docker-hardened-images/keyring`, `dhi.io`), empreinte
   `118ba556…3887c`. Docker annonce ses rotations (`dhi-1.pub` inactive,
   `dhi-2.pub` active, en-tête `x-keyid`) — une a déjà eu lieu.

### Étape 1 — le domaine, sans changement visible

**Faite le 2026-09-25.** Écarts au texte ci-dessous, à reprendre dans les
étapes suivantes :

- **L'outil et la config passent à l'étape 2.** `config.ToolCosign`,
  `ScanTools.Cosign`, l'entrée de `toolTable` et `scan.image_verification`
  n'auraient été lus par rien à l'étape 1 — des réglages inertes, que ce plan
  interdit. `CosignVerifier` prend un `ToolSpec` ; l'étape 2 le construit
  depuis `Report.Spec(ToolCosign)`.
- **Le cache vit dans `trust`** (`cache.go`, `FileStore`), pas dans
  `internal/cache` : ce package importe `scan`, qui implémente
  `trust.Verifier` — cycle. Même raison pour `Repository`, réécrit dans `trust`
  plutôt qu'emprunté à `remediation.ParseRef`, avec un test qui garde les deux
  d'accord.
- **Identifiants par `COSIGN_REGISTRY_USERNAME`/`_PASSWORD`**, pas par un
  `DOCKER_CONFIG` temporaire : mesuré, cosign les lit, et un fichier 0600 monté
  n'est pas lisible par l'utilisateur du conteneur. Plus simple, rien sur disque.
- **Pas de `TUF_ROOT` monté** en conteneur : une racine fraîche à chaque run
  (≈3 s, 30 runs concurrents sur une racine vide sans échec), plutôt qu'un
  montage hôte en écriture que l'utilisateur du conteneur ne pourrait pas écrire.
- **`/**` en fin de motif** couvre toute profondeur (`gcr.io/distroless/**`) ;
  `*` reste un segment, comme `path.Match`.
- **`download attestation` refuse `--experimental-oci11`** (mesuré) : le flag
  ne va que sur `verify`.
- **Vérifié contre le vrai cosign** dans le sandbox (test jetable, non commité) :
  distroless, Chainguard et DHI `verified` sous B ; alpine `no-policy` ; une règle
  C à mauvaise identité sur l'image cosign (format bundle) `identity-mismatch`
  via la relance permissive ; la continuité cosign v3.1.3 → v3.1.2 `verified`,
  distroless → alpine `unsigned`.

**`internal/trust`** (nouveau package, pur sauf le chargement du fichier) :

- `policy.go` — `Policy`, `Rule{Match, Mode, Source, Line}`, `Load(path)`.
  `yaml.v3` avec `KnownFields(true)`, `version: 1` obligatoire, exactement un
  mode par règle, `notation:` refusé « not supported yet ». Un fichier invalide
  rend une erreur et **aucune** règle. Fichier absent = politique vide, pas une
  erreur. Règle masquée : un avertissement retourné (pas une erreur).
- `match.go` — normalisation par `remediation.ParseRef` (`python` →
  `docker.io/library/python`), glob sur le dépôt sans tag ni digest, première
  règle gagnante. `Lookup(policy, builtin, ref) (Rule, bool)` applique C puis B.
- `builtin.go` — B, les trois entrées mesurées de §3.82 (distroless et
  Chainguard en keyless, `dhi.io/*` en mode clé), chacune avec la date et la
  commande de la mesure en commentaire. La clé DHI est **embarquée**
  (`//go:embed`), jamais téléchargée — recoupée à l'étape 0.7 (§3.82,
  empreinte `118ba556…3887c`, `dhi-2.pub`, active). L'entrée porte une
  **liste** de clés, vérifiée si l'une d'elles vérifie, mais **seules les clés
  `active`** du dépôt `keyring` y entrent — `dhi-2` seule aujourd'hui ;
  `dhi-1`, `inactive` sans raison donnée et inutile sur les images mesurées,
  est écartée (§3.82). La liste sert quand deux clés sont actives pendant une
  transition.
- `verdict.go` — `Verdict`, `Decision{Block, Warn, None}`, et
  `Decide(v Verdict, src Source) Decision` — le tableau ci-dessus, **une** table
  en code, lue par les trois consommateurs.
- `Verifier` : `Verify(ctx, ref, digest string, rule Rule) (Verdict, error)`
  et `Identities(ctx, ref, digest string) ([]Identity, error)` (indices, pour A).
  **Écart assumé à §3.82** (qui le plaçait dans `internal/remediation`) : trois
  consommateurs — remédiation, pull, scan — donc un package neutre.
- `continuity.go` — A : indices de l'image actuelle → vérification stricte de
  chacun → identité validée → vérification stricte du candidat. Aucune identité
  validée ⇒ `NoPolicy`. L'indice n'est jamais cru : c'est le test.

**`internal/scan/cosign.go`** — l'implémentation de `trust.Verifier`, sur le
modèle de `plumber.go` (`toolCmd`, `cliRunner`, binaire ou image) :

- `cosignVerifyArgs(ref@digest, rule, permissive bool)`, toujours avec
  `--experimental-oci11` ; `classifyCosign(exit int)`, table-driven sur la
  mesure. En mode clé, pas de relance permissive : tout code autre que 0 et 11
  est `Unsigned`, libellé « no verifiable signature from the expected key ».
- `download attestation` pour les indices ; certificat lu avec `crypto/x509`
  (SAN + extension émetteur Fulcio `1.3.6.1.4.1.57264.1.8`, repli `.1.1`) ; pour
  l'ancien format, `optional.Subject`/`Issuer` de la sortie JSON.
- `TUF_ROOT` → `~/.devdesk/cache/sigstore`, monté en conteneur.
- Identifiants : `docker.GetStoredCreds(host)` → `DOCKER_CONFIG` temporaire
  (0600, un seul hôte), monté en lecture seule puis supprimé, cosign lancé
  avec `-u <uid>` du propriétaire — mesuré. Jamais d'argv.
- `tlog: false` ⇒ `--insecure-ignore-tlog` ; sinon jamais.
- Pas de sémaphore autour de TUF : 30 vérifications concurrentes sur un cache
  vide, aucun échec.
- Délai : `context` de la vérification ; dépassé ⇒ `Failed`.

**Outil** : `config.ToolCosign`, `ScanTools.Cosign ToolConfig`, une entrée dans
`toolTable` (hors `categoryTable`), `DefaultCosignImage` **épinglée par
digest** — le vérificateur est l'ancre de confiance, un tag flottant serait
précisément la faille qu'il détecte. `ReservedArgs` : `--key`,
`--certificate-identity`, `--certificate-identity-regexp`,
`--certificate-oidc-issuer`, `--certificate-oidc-issuer-regexp`,
`--insecure-ignore-tlog`, `--experimental-oci11`, `--output`, `-o`.

**Config** : `ScanConfig.ImageVerification string` (`on`/`off`),
`ImageVerifications()`, défaut `on` dans `applyDefaults` — **pas** encore dans la
vue configuration (étape 2).

**Cache** : `internal/cache/signatures.go`, `signature-verdicts.json`, clé
`digest + sha256(règle normalisée)`, durées 24 h / 6 h, `Failed` jamais écrit.

**Tests** :
- `Load` : fichier valide ; coquille (`isuer:`) ⇒ rejet total ; deux modes ⇒
  rejet ; `version` absente ⇒ rejet ; `notation:` ⇒ « not supported yet » ;
  règle masquée ⇒ avertissement ; fichier absent ⇒ vide.
- `Lookup` : normalisation, première règle gagnante, C l'emporte sur B.
- `Decide` : le tableau, une ligne de test par case, citant §3.82.
- `classifyCosign` + la relance permissive, avec un faux runner : chaque ligne
  du tableau de mesure de §3.82, dont le 12 permissif (blob bloqué) ⇒ `Failed`,
  jamais `IdentityMismatch`.
- Mode clé : 10 (ancien format) **et** 1 (bundle, mauvaise clé ; registre
  injoignable) ⇒ `Unsigned` ; `Decide(Unsigned, B)` ⇒ bloque — un test nommé
  pour le cas « DHI passe au format bundle et la clé ne concorde plus ».
- Continuité : un indice falsifié (annoncé mais non validé) ne produit **jamais**
  `Verified`.
- `TestEveryConfiguredOptionReachesTheScanner` reste vert ;
  `SameDetection` couvre cosign.

### Étape 2 — le pull vérifié

**`trust.PullVerified(ctx, ref string, deps PullDeps) (Outcome, error)`** —
`deps` injecte `Digest`, `Verify`, `Pull`, `Tag`, `Enabled` : testable sans
moteur. Séquence : `image_verification` off ⇒ pull direct ; sinon digest
(`oci.ManifestDigest`, déjà là), règle, verdict (cache d'abord), `Decide`.
`Block` ⇒ erreur typée `ErrBlocked{Verdict, Rule}` dont le message **nomme la
règle** (fichier:ligne, ou « built-in: distroless ») ; `Warn` ⇒ pull, avertissement
porté par l'`Outcome` ; sinon pull. Toujours `pull repo@digest` puis
`docker.TagImage` (nouveau, dans `internal/docker/images.go`) — mesuré sur
Docker et Podman. Sous Docker (magasin containerd), `repo@sha256:…` apparaît
aussi dans `RepoTags` : vérifier que la table Images ne le montre pas comme un
second tag (`docker images` n'en affiche qu'un).

**Les deux appelants** passent par là :
- `pullOneImageCmd` (`internal/ui/oci_resources/commands.go:453`) — le
  navigateur de registre **et** le MCP (`handleImagePullRequested`, `mcp.go:84`,
  vérifié : même commande).
- `updateImageCmd` (`image_update.go`, la variable `pullImageContext`).
- Un test source (sur le modèle des `TestNo…` existants) : aucun appel à
  `docker.PullImageContext` hors de `trust`.

**`docker run`** : `--pull=never` dans `buildLaunchArgs`
(`internal/docker/launch.go:26`), selon l'étape 0.6 ; le message d'image absente
dit de la tirer d'abord. `buildLaunchArgs` a deux sorties : `LaunchContainer` et
`BuildLaunchCmd` (le mode `-it` via `tea.ExecProcess`) — les deux en héritent.
`VerifyEntrypoint` (`launch.go:95`) lance lui aussi un `run` sur l'image
choisie : même flag.

**UI** : refus ⇒ `footer.Error` (le système a refusé), avertissement ⇒
`footer.Warn` ; le job `:jobs` porte l'état. Le MCP reçoit l'erreur telle quelle.

**Config** : le champ `Image verification` (cycle `on`/`off`, Rule 132) dans
l'onglet `scan` de la vue configuration ; cosign apparaît dans l'onglet Tools
par la table. Au démarrage, `trust.Load` : erreur ⇒ footer + log nommant la
ligne ; règles C présentes avec `off` ⇒ un log.

**Aide** (`?`, vue OCI) : ce qui est vérifié, et que **un `docker pull` tapé
ailleurs ne l'est pas**.

**Tests** : `PullVerified` × les cinq verdicts × les trois sources ; tag déplacé
entre digest et pull (le faux `Pull` reçoit bien `@digest`) ; `off` ⇒ aucun
appel à `Verify` ; refus nommant la règle.

### Étape 3 — l'onglet Remediation

- Colonne **`Sig`**, icône seule, après `Update` — un paquet partagé sur le
  modèle d'`updatecol`, pour que la vue OCI puisse la reprendre plus tard. Icône
  OK / avertissement orange / erreur rouge / `-` grisé / `?` (Rule 121, 122 :
  `Cell` en texte brut, `Style` pour la couleur).
- Vérification en arrière-plan quand les candidats sont calculés
  (`discoverRemediationCmd`), sur le modèle de `imageupdate.Check` / `Tracker` ;
  l'image en usage d'abord (A en a besoin), puis les candidats, quatre en
  parallèle au plus.
- `canSelectCandidate` (`remediation_write.go:124`) : une raison de plus,
  `reasonSignatureBlocked` — nommant la règle. Tant que le verdict n'est pas
  arrivé, le choix reste permis (Rule 130 : ne pas savoir n'est pas non) ; s'il
  arrive `Block` après le choix, le choix est relâché avec un `Warn`.
- Confirmation de `ctrl+o` : un candidat `Warn` y est rappelé.
- `GetHeaderInfo` : `Signatures: off` quand `image_verification` l'est.
- Aide de la vue sécurité.
- Tests : la colonne, le refus au `space`, le relâchement, l'en-tête `off`,
  l'ensemble des touches inchangé d'un état à l'autre.

### Étape 4 — le finding de l'image en usage

- `internal/scan/signature.go`, étape `signature`, lancée quand Misconfiguration
  est active, la cible un répertoire, `image_verification` `on` — sur le modèle
  exact de `runBuildContextStage` (`buildcontext.go:326`) et de son
  `checksBuildContext`.
- Pour chaque `FROM` d'une étape livrée (`shippedStages`, déjà là) qui nomme une
  image de registre : règle, verdict, `Decide`. `Block` ⇒
  `DEVDESK-SIG-001` (IdentityMismatch, CRITICAL) ou `-002` (Unsigned, HIGH),
  `Source: signature`, ancré sur la ligne. `Failed` ⇒ `recordStageError`, pas de
  finding. `Warn` sous A ⇒ rien : ce n'est pas un fait.
- `Categorize` : sous Misconfiguration. La colonne `CFG` (§3.87) les compte.
- Tests : fixtures Dockerfile ; un `FROM <étape>` ignoré ; un `FROM scratch`
  ignoré ; un `ARG` résolu comme le fait déjà le parseur.

### Documentation, à chaque étape

- `docs/architecture/scanning.md` : cosign, l'étape `signature`, le cache
  (étapes 1 et 4).
- `docs/architecture/network.md` : le pull vérifié (étape 2).
- `docs/architecture/configuration.md` : `trust.yaml` (hors contextes, strict) et
  `scan.image_verification` (étapes 1 et 2).
- `.claude/CLAUDE.md` : `internal/trust` dans la liste d'architecture.
- §3.82 : marqué done au dernier commit, avec les écarts.

### Vérification, à chaque étape

`mise run check` (fmt, vet, lint, test) et `mise run test-race` — les
vérifications tournent dans des `Cmd`. Puis, depuis l'hôte : pull d'une image
distroless (vérifiée), d'une image avec une règle C à mauvaise identité
(bloquée, règle nommée), `off` (rien de vérifié, « Signatures: off » visible).

## Hors plan

- La colonne de signature dans la vue OCI pour les images locales.
- Notation (le nom `notation:` est réservé).
- La provenance SLSA (même interrupteur, plus tard).
- Le SBOM de l'éditeur (§3.91), la CI qui ne signe pas (§3.90).
- Un déploiement Sigstore privé (`trust_root:`).
- L'édition de `trust.yaml` depuis la vue configuration.
- **Les images d'outils de DevDesk** (Trivy, Gitleaks, plumber, l'image netdiag,
  cosign lui-même) : lancées par `docker run`, donc tirées implicitement, et
  **non vérifiées** — exception déclarée. Seule celle de cosign est épinglée par
  digest ; faire de même pour les autres est une entrée à part.

## Risques

- **Le code 1 de cosign** : la relance permissive double le coût d'un échec
  réel (~3 s de plus). Acceptable : c'est le cas rare.
- **La sortie de cosign change entre versions** : l'image est épinglée par
  digest, et `classifyCosign` ne lit que des codes de sortie ; une montée de
  version refait la mesure de §3.82.
- **Les faux positifs de B** : un éditeur qui change de workflow, ou Docker qui
  change de clé DHI, bloque tous les utilisateurs jusqu'à une nouvelle version de
  DevDesk. Atténué par la liste de clés par entrée ; contournement documenté :
  une règle C sur la même portée l'emporte.
- **`--pull=never`** change un comportement existant (le lancement d'une image
  absente) ; le seul appelant de `LaunchContainer` est la vue OCI
  (`commands.go:262`), qui ne lance que des images listées, donc locales.
