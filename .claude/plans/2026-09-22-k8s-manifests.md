# Manifestes Kubernetes, Kustomize et Helm — lint, validation, remédiation (§3.80)

## Contexte

§3.80 du backlog : DevDesk doit analyser les manifestes Kubernetes d'un dépôt (YAML brut, Kustomize, Helm), sans jamais toucher un cluster. Les problèmes doivent remonter dans le tableau de résultats de scan et, quand c'est sûr, être corrigés comme les Dockerfiles (§3.78/§3.84).

Constats vérifiés pendant l'exploration :
- **Trivy détecte déjà** les règles `KSV-*`, avec leur span et leur `Resolution`. Il rend Helm nativement, mais pour Kustomize il ne lit que la base. En revanche, `TrivyMisconfiguration.Type` (`kubernetes`/`helm`/`dockerfile`…) est décodé puis **jeté** : `internal/scan/trivy.go:216-236` ne le reporte pas sur `scan.Finding`.
- **kube-linter est écarté** : il recouvre Trivy sur la sécurité et ne donne pas de numéro de ligne (`Diagnostic{Message}` porte un `TODO` à ce sujet). Il ne pourrait donc pas alimenter la remédiation.
- **Kyverno est reporté** : il n'embarque aucune politique et ne rend ni Helm ni Kustomize. Il a sa place comme « policy-as-code apporté par l'utilisateur », dans une entrée de backlog séparée.
- **kubeconform** (v0.8.0) comble le seul manque réel, la validité par rapport au schéma de l'API. Sa sortie JSON donne `filename, kind, name, status, msg, validationErrors[{path, msg}]`. Il n'y a pas de ligne, mais le chemin JSON se résout en ligne avec `yaml.v3`.

Décisions de l'utilisateur :
1. Ne pas multiplier les outils : **Trivy + kubeconform**, sans kube-linter, et Kyverno plus tard.
2. **Tous les outils sont traités de la même manière** : kubeconform, helm et kustomize sont des outils externes (`auto|binary|image`), comme Trivy, Gitleaks et plumber. Pas de bibliothèque embarquée.
3. helm et kustomize sont **optionnels, utilisés s'ils sont présents**, pour rendre les charts et overlays avant validation.
4. La version de Kubernetes ciblée est un **réglage de config**. Par défaut une version récente fixée dans le code ; vide signifie master.

## Travail préalable

`git worktree add -b feat/k8s-manifests .worktrees/k8s-manifests origin/main`, créé depuis le sandbox. Un commit par phase.

## Phase 0 — corriger le gabarit avant de le copier

`NewScanner` (`internal/scan/scanner.go:458-469`) ne transmet à `CheckDependencies` que les champs Trivy et Gitleaks. Les réglages plumber sont donc ignorés par les scans réels, et seul le dashboard les honore. De plus, `PlumberPath` n'est pas expansé (`config.go:830-834`).
- Ajouter `PlumberSource/Path/Image` dans `ScanOptions` (`options.go`) et les transmettre depuis `NewScanner`. Expanser `~` dans `PlumberPath`.
- Test : un `ScanOptions` avec `PlumberSource: binary` produit un `DependencyStatus` en mode binaire.
- Commit `fix(scan): real scans honour the plumber source settings`.

## Phase A — typer les findings IaC (Trivy, sans nouvel outil)

- Ajouter `scan.Finding.IaCType string` (`scanner.go:99-159`), rempli dans la boucle misconfig de `trivy.go` avec `firstNonEmpty(misconf.Type, result.Type)`.
- MCP : exposer `iac_type` dans la projection `finding` et dans `expose()` (`internal/mcp/scan_tools.go:135-155, 325-347`).
- Onglet Misconfigurations : ajouter une colonne `Type` (`SizingContent`, `Optional`), seulement pour `TabMisconfig`. Il faut donc une variante de `findingColumns()` (`internal/ui/security/findings.go:39-68`) plutôt qu'une colonne partagée par tous les onglets. Le détail (`details.go`) affiche aussi le type.
- `header.go:68-72` : le libellé `ctrl+o` reste « Apply built-in fix » sur cet onglet, ce qui est déjà le cas.
- Tests : décodage d'un JSON Trivy contenant un `Type: kubernetes` (inline dans `parse_test.go`) et projection MCP.

## Phase B — kubeconform, outil externe

**Config** (`internal/config/config.go`, `ScanConfig`) :
- `KubeconformSource/Path/Image` (`kubeconform_source|_path|_image`, image par défaut `ghcr.io/yannh/kubeconform`)
- `EnableK8sSchema bool` (`enable_k8s_schema`), avec le même traitement que `EnableCIScore` dans la migration « tout désactivé » (`config.go:552-562`)
- `KubernetesVersion string` (`kubernetes_version`), avec un défaut fixé dans le code
- Expansion de `~` dans le chemin.

**Détection** : `KubeconformSpec()` dans `tool_source.go`, et des champs `Kubeconform*` dans `DependencyStatus`, `CheckDependencies`, `ScanOptions`/`OptionsFromConfig` et `NewScanner`. `missingToolErrors` doit signaler l'outil manquant quand l'étage est demandé sur un répertoire (ce que plumber ne fait pas aujourd'hui).

**Étage** (`internal/scan/kubeconform.go`, `kubeconform_parse.go`), branché dans `Scanner.Scan` seulement pour `TargetDirectory`, sur le modèle de l'étage plumber (`scanner.go:617-652`) :
- Arguments : `-output json -strict -summary=false -kubernetes-version V -cache <dir> -schema-location default`. Le cache est `~/.devdesk/cache/kubeconform`, monté dans `/cache` en mode image, et la cible est montée en lecture seule sur `/scan` (voir `wrapTrivy`).
- **Pas de `-ignore-missing-schemas`.** Un « could not find schema » sur un groupe natif (liste fermée : `""`, `apps`, `batch`, `networking.k8s.io`, `policy`, `rbac.authorization.k8s.io`, `autoscaling`, `extensions`…) devient le finding « apiVersion not served by Kubernetes V », en HIGH. Sur un groupe CRD, le document est ignoré et compté comme « non validé ». C'est ce qui rend détectables les API retirées.
- Mapping : `invalid` donne un finding par `validationErrors[i]`. `error` donne un finding pour un YAML illisible, et pour un schéma absent selon la règle précédente. La sévérité est HIGH, puisque l'`apply` serait refusé.
- Champs du finding : `Source: "kubeconform"`, `IaCType: "kubernetes"`, `ID` stable (`K8S-SCHEMA` / `K8S-API-REMOVED`), `File` relatif, `Message` = msg, et `Title` combinant kind, name et chemin.
- **Ligne** : fonction pure `locateYAMLPath(content, kind, name, pointer) (line, endLine, ok)`, basée sur `yaml.v3` `Node` et placée dans `internal/k8s/yamlpath.go`. Elle trouve le document par kind+name, puis descend le pointeur JSON. Si la résolution échoue, la ligne vaut 0 ; on n'invente jamais de ligne.
- **Fichiers exclus** de la validation brute : `templates/` sous un `Chart.yaml`, et tout répertoire contenant un `kustomization.yaml|yml|Kustomization` (les patches y sont partiels). Ceux-là ne sont validés qu'après rendu (phase C). Sans le rendu, le Result porte une note « N charts / overlays not rendered » dans `Errors`, et la note est journalisée.

**Catégorie** : `category.go` fait passer `Source "kubeconform"` en `CategoryMisconfiguration`. `sourceDisplay` affiche « schema ». L'onglet Misconfigurations reste le seul tableau, avec la colonne Type de la phase A.

**Configuration et UI** : un groupe « Kubeconform » dans `internal/ui/configuration/fields.go:293-348` (source, binary, image, version K8s), plus la case « K8s schema » dans le groupe Scanners. S'y ajoutent la liste d'outils du dashboard (`dashboard/model.go:595-613`, `sections.go:976-985`, à garder synchronisée), `about/view.go`, et le texte d'aide dans `header.go:248`.

**Tests** : fixtures `internal/scan/testdata/kubeconform_*.json` (invalid, error schéma natif, error CRD). Un cas pour `stageOf`/`byStage` (`scan_test.go:27-67`), les arguments en fonctions pures (image/binary, cache), `locateYAMLPath` en table (multi-documents, séquences, clé absente) et la détection dans `tool_source_test.go`.

## Phase C — rendu Helm et Kustomize, optionnel

- `HelmSource/Path/Image` (image par défaut `alpine/helm`) et `KustomizeSource/Path/Image` (image par défaut `registry.k8s.io/kustomize/kustomize:v5`), en config, en détection et dans la vue de configuration, comme ci-dessus. Aucune case de plus : ils servent l'étage kubeconform et n'ont pas d'étage propre.
- Découverte des cibles : fonction pure `internal/k8s/discover.go`, qui liste les répertoires `Chart.yaml` et les racines Kustomize. Une racine Kustomize référencée par une autre (base d'un overlay) n'est pas rendue seule ; seules les feuilles le sont.
- Pour chaque chart, si helm est disponible : `helm template <chart>` (valeurs par défaut) sur stdout, puis kubeconform lit la sortie sur stdin (`-` avec `-stdin-filename`, à vérifier contre la v0.8.0). `File` vaut le template d'origine, lu dans le commentaire `# Source:` du rendu, avec `Line = 0`, puisqu'une ligne rendue n'est pas une ligne de template.
- `helm lint <chart>`, puisque helm est déjà là : les lignes `[ERROR]` donnent HIGH, les `[WARNING]` donnent LOW, les `[INFO]` sont ignorées. `Source: "helm-lint"`, `IaCType: "helm"`. La sortie est du texte : le parseur est testé sur des fixtures réelles, et une ligne non reconnue est journalisée, pas perdue en silence.
- Pour chaque overlay Kustomize feuille, si kustomize est disponible : `kustomize build <dir>` sur stdout, puis kubeconform. `File` vaut le `kustomization.yaml` de l'overlay, et le Title porte kind/name.
- Un échec de rendu (dépendance de chart non vendorisée, par exemple) produit une erreur d'étage par cible via `recordStageError`. Le scan continue.
- Si helm ou kustomize est absent : ce n'est pas une erreur d'outil manquant, c'est la note « not rendered » de la phase B.

## Phase D — remédiation

**D1 — catalogue KSV, remplacement de valeur** (`internal/remediation/misconfig.go`) :
- `Rule.Fix` est aujourd'hui gardé par `IsDockerfileName`. Les nouvelles règles se gardent sur `f.IaCType == "kubernetes"` et `f.Source == "trivy-misconfig"`, et déclinent sur `helm` avec la raison « rendered from a Helm template — edit values or the template ».
- Commencer par les cas où la clé existe déjà et où la valeur s'inverse : `privileged: true → false`, `allowPrivilegeEscalation: true → false`. Le span vient de `Line/EndLine`, puis on cherche la clé exacte dans ce span. Chaque ID est vérifié contre les sources rego de trivy-checks, comme en §3.84, avant d'entrer au catalogue.

**D2 — insertion de clé, sans re-sérialiser** (`internal/patch/yamlinsert.go`) :
- Placer un `yaml.v3` `Node` sur le mapping cible (`securityContext` du conteneur), calculer le point d'insertion et l'indentation à partir de `Node.Line/Column`, puis rendre un `patch.Edit` d'insertion. Ainsi, commentaires, ancres et mise en forme restent intacts, et `patch.Rewrite`, `Diff` et `WriteIfUnchanged` sont réutilisés tels quels.
- Règles candidates : `allowPrivilegeEscalation: false` absent, `seccompProfile.type: RuntimeDefault`. Sont déclinées, avec un motif écrit dans le source (même leçon que `USER 1000` en §3.78) : `runAsNonRoot`, `readOnlyRootFilesystem` et `capabilities.drop: [ALL]`, parce que la règle passerait alors que le pod pourrait échouer à l'exécution. Les limites de ressources sont déclinées aussi, faute de valeur universelle.

**D3 — API retirées (kubeconform)** : une table fermée des migrations qui ne sont qu'un renommage d'`apiVersion`, vérifiée contre le guide de dépréciation Kubernetes. Par exemple `batch/v1beta1 CronJob → batch/v1`, `policy/v1beta1 PodDisruptionBudget → policy/v1`, `autoscaling/v2beta2 → autoscaling/v2`. Les migrations structurelles, comme `networking.k8s.io/v1beta1 Ingress`, sont déclinées. Le span est la ligne `apiVersion:` du document, trouvée par `locateYAMLPath`.

**UI** : rien de neuf. `ctrl+o` sur l'onglet Misconfigurations (`misconfig_write.go:80-140`), les quatre raisons de grisage, le diff, puis le job `verify fix`, qui relance maintenant aussi kubeconform. Seul `FixFor` doit accepter les findings `kubeconform`, en plus de `CategoryMisconfiguration`, qui les couvre déjà.

## Documentation, dans les mêmes commits

- `docs/architecture/scanning.md` : une section kubeconform (étage, fichiers exclus, schéma natif vs CRD, cache, rendu), et l'extension du catalogue de correctifs.
- `docs/architecture/configuration.md` et `docs-site/docs/reference/configuration.md` : les nouvelles clés.
- `docs/backlog.md` : §3.80 passe de « à explorer » à « fait », avec le tableau des décisions (kube-linter écarté et pourquoi, Kyverno reporté). Nouvelle entrée pour Kyverno (politiques apportées par l'utilisateur) et pour les sondes et contrôles croisés que seul kube-linter fournissait.
- Aide `GetHelpContent` de la vue security, pour le type et les sources.

## Vérification

- À chaque phase : `mise run check` (fmt, vet, lint, test) et `mise run test-race` si un compilateur C est disponible.
- Tests ciblés : `go test ./internal/scan/... ./internal/remediation/... ./internal/patch/... ./internal/k8s/... ./internal/mcp/... ./internal/ui/security/...`.
- Bout en bout, depuis l'hôte (le sandbox n'a ni trivy ni kubeconform), sur un dépôt de test contenant :
  - un `Deployment` en `privileged: true` sans `allowPrivilegeEscalation` ;
  - un `CronJob` en `batch/v1beta1` ;
  - un champ inconnu (`imagePullPolicyy`) ;
  - un chart Helm ;
  - un overlay Kustomize.
- Attendu : les KSV et les findings « schema » apparaissent dans Misconfigurations avec la colonne Type. Les lignes sont justes pour le YAML brut et à 0 pour les rendus. `ctrl+o` corrige privileged, l'insertion et le CronJob, puis `verify fix` constate la disparition du finding. Sans helm ni kustomize, la note « not rendered » apparaît, sans erreur d'outil.
- MCP : `scan_result` rend `iac_type` et les findings kubeconform.
