# Recherche — Dagger en local et en CI, runners sur datomir

Date de consultation de toutes les sources : 2026-11-21. Convention : **[F]** fait sourcé,
**[E]** estimation (non sourcée, à mesurer), **[ND]** pas de donnée trouvée.
Les pages ont été lues via un outil de résumé automatique (pas de lecture brute) : les citations
sont celles que l'outil a restituées, à reconfirmer sur la page avant décision.

## Question de recherche reformulée

Quelle est la façon la moins chère et la moins risquable d'avoir (a) les mêmes vérifications en
local et en CI, (b) `go test -race ./...` (environ 15 min aujourd'hui) sous 10 min, (c) sans coût
d'exécution, (d) sans exposer datomir à du code externe, et (e) utilisable par des agents IA
tournant sur datomir ? Le module Dagger + runners k0s n'est qu'une option parmi d'autres.

## Synthèse (5 points)

1. **[F] Les runners GitHub-hosted standard sont gratuits et illimités sur dépôt public** (4 vCPU,
   16 Go RAM). Ils éliminent le risque « code externe sur datomir ». Mais le cache y est perdu à
   chaque job sauf via `actions/cache` (quota 10 Go par dépôt, éviction LRU). **[ND]** : aucune
   mesure publiée du gain d'un `GOCACHE` chaud sur `go test -race` ; le gain sur le dépôt
   DevDesk doit être mesuré.
2. **[F] GitHub déconseille les runners self-hosted sur dépôt public** (« almost never »). Les
   groupes de runners (restriction par dépôt) exigent une organisation ; le compte `anthnel` est
   de type **User** (API GitHub), donc pas d'organisation, pas de groupes. Les runners
   restent possibles au niveau dépôt ; l'approbation des workflows de forks est le garde-fou
   documenté, avec une faille connue (contributeur déjà fusionné = sans approbation).
3. **[F] Dagger n'accélère rien par lui-même** : le gain vient d'un moteur dont le cache survit.
   Le moteur Kubernetes documenté est un **DaemonSet** (cache sur le disque du nœud), avec une
   option **StatefulSet + PVC** dans le chart Helm (désactivée par défaut). Il tourne
   **privilégié** dans tous les exemples officiels ; **[ND]** : aucune source officielle sur un moteur
   rootless sous Kubernetes.
4. **[F] Les connexions client sont marquées expérimentales** dans les docs récentes
   (`_EXPERIMENTAL_DAGGER_RUNNER_HOST`, `kube-pod://`), mais la page /self-hosting actuelle parle de
   `DAGGER_ENGINE` (divergence, voir plus bas). `tcp://` n'a aucune authentification.
5. **[F] `-race` coûte 2 à 20x en temps et 5 à 10x en mémoire** (doc Go) et exige cgo + un
   compilateur C. **[ND]** : aucun retour d'expérience publié sur `go test -race` sous Dagger avec cache
   persistant ; aucun benchmark avant/après de cache de moteur Dagger pour des tests Go.

## Tableau des alternatives

| Alternative | Coût | Gain attendu | Effort | Limites / risques |
|---|---|---|---|---|
| **Statu quo** (tests en local, faux job `Test`) | 0 | 0 | 0 | Tests non vérifiés en CI ; hors critère « CI complète < 10 min ». |
| **GitHub-hosted + `actions/cache`/`setup-go`** (GOCACHE + GOMODCACHE) | 0 sur dépôt public [F] | **[E]** : un cache chaud évite la recompilation ; blog tiers : « roughly half » du temps de CI (non spécifique à `-race`) | Faible | Quota 10 Go/dépôt, éviction LRU [F] ; un GOCACHE `-race` volumineux pousse les autres caches dehors [F] ; builds parallèles = écritures concurrentes (ADR setup-go) [F] ; cache sans clé de fusion entre branches : à vérifier [ND]. |
| **GitHub-hosted + découpage par package** (matrice, gotestsum, `shard`) | 0 (minutes illimitées, dépôt public) [F] | Mattermost : environ 60 % de temps de test en moins (cas unique, non transposable) [F] | Moyen | Chaque shard recompile ; équilibrage difficile (Go #33527, #65814 : écarts jusqu'à 2x) [F]. |
| **`-race` ciblé** (race seulement sur les paquets concurrents, ou seulement sur `main`/nuit) | 0 | **[E]** gain direct, car le surcoût est 2-20x [F] | Faible | Réduit la couverture de PR ; décision de politique, pas technique. |
| **ARC seul + PVC pour GOCACHE** (sans Dagger) | 0 (matériel de datomir) | **[E]** équivalent au cache persistant de Dagger | Moyen | Mêmes risques runner self-hosted/dépôt public ; DinD = privilégié, mode Kubernetes = pod par job sans privilèges mais « elevated privileges with the Kubernetes API server » si jobs sans conteneur [F]. **[ND]** : retour d'expérience PVC + GOCACHE. |
| **Dagger + moteur persistant (k0s)** | 0 (moteur OSS) ; Cloud optionnel | **[ND]** mesuré ; **[E]** équivalent à un GOCACHE persistant | Élevé | Voir section Dagger ; ajoute une couche (module, moteur, version CLI = version moteur [F]). |
| **mise + cache** | 0 | Aucun en soi : mise installe les outils, ne cache pas la compilation | Déjà en place | Déjà utilisé (`jdx/mise-action`). **[ND]** : pas de source collectée sur son cache. |
| **Earthly** | — | — | — | **[F]** Earthly Cloud/Satellites arrêtés le 16 juillet 2025, projet OSS en maintenance sans nouvelles PR ; Earthly renvoie vers Dagger. À écarter. |
| **Depot** (CI ou cache distant, `GOCACHEPROG`) | **[ND]** prix non vérifiés ; service payant | Benchmark éditeur (Tailscale) : -22 % sur un build d'environ 1 min ; « slight » pour les tests [F, source éditeur] | Faible | Service tiers hébergé, payant (à confirmer) ; contredit « sans coût ». `GOCACHEPROG` exige Go 1.24+ (OK ici : Go 1.27). Alternatives OSS : `gobuildcache`, `gocacheprog` [F, existence seulement]. |
| **Nix** | — | — | — | **[ND]** : pas recherché. |
| **Bazel** | — | — | — | **[ND]** : pas recherché. |
| **Runner GitHub plus gros** | Payant (plans Team/Enterprise Cloud uniquement) [F] | — | — | Non disponible sur compte User gratuit : à vérifier [ND]. |

## Dagger spécifiquement

**Version courante [F]** : v0.21.12, publiée le 2026-11-17 (API GitHub releases). Le changelog v0.21 (22 mai 2026)
indique que Dagger « replaces the BuildKit solver with its own engine » (plus gros changement du moteur
depuis le début du projet). Donc l'âge des blogs et issues BuildKit-era est un risque de péremption.

**Déploiement Kubernetes [F]** (docs.dagger.io/self-hosting, chart `helm/dagger`) :
- Chart Helm recommandé (`oci://registry.dagger.io/dagger-helm`), « works best when the CI runners also run in the cluster ».
- Par défaut : un moteur par nœud (DaemonSet), cache sur le disque du nœud ; nœud remplacé = cache perdu.
- Les runners joignent le moteur de leur nœud par un socket `hostPath`.
- « Runners outside the cluster use a StatefulSet with a persistent volume instead. »
- values.yaml : `engine.kind: DaemonSet` par défaut ; `StatefulSet` « for running multiple Engines per K8s node » ;
  `engine.statefulSet.persistentVolumeClaim.enabled: false`, `storage: 10Gi` par défaut, `accessModes: [ReadWriteOnce]`.
  Aucun `securityContext`/`privileged` visible dans le contenu retourné (pas confirmé que le chart ne l'impose pas).
- `kube-pod://` : accès contrôlé par les permissions Kubernetes du runner.
- `tcp://` : « no authentication », à restreindre à un réseau privé ou à protéger par certificats clients.

**Privilèges [F]** : l'exemple Docker utilise `--privileged` ; la page OpenShift (via recherche) demande de
donner le SCC `privileged` au compte de service du moteur. **[ND]** : aucune source officielle ou issue
trouvée sur un moteur Dagger rootless sous Kubernetes. Kubernetes 1.37 a passé `KubeletInUserNamespace`
en bêta et `hostUsers: false` existe depuis 1.33 [F, hors Dagger] : piste à tester, non validée avec Dagger.

**Stabilité [F]** : la doc v0.21.7 dit que `_EXPERIMENTAL_DAGGER_RUNNER_HOST` « is experimental and may change ».
La page /self-hosting actuelle parle de `DAGGER_ENGINE` à définir dans chaque job, sans préciser ce nom
dans le changelog v0.21 (voir divergences).

**Concurrence de plusieurs clients sur un moteur [ND]** : pas de doc officielle trouvée sur les limites.
Issues liées (anciennes) : #7387 (blocage à la fermeture d'un second client dans la même trace),
#6894 (deadlock), PR #7395 (retrait de `--oci-max-parallelism`, contrôle CPU par cgroups suggéré) ;
OOM évoqué comme hypothèse non confirmée dans #7387. Le changelog cite « race in nested client sessions »
et « persistent cache lookup errors » corrigés (versions non vérifiées). **[E]** : plusieurs jobs
concurrents (`test` + `test-race` + agents) contiendront CPU et mémoire sur un même moteur ; à tester.

**Cache et purge [F]** : GC actif par défaut ; `engine.json` (depuis v0.15.0) avec `maxUsedSpace`, `reservedSpace`,
`minFreeSpace`, `sweepSize` (exemple de doc : 200GB / 10GB / 20% / 50%) ; `gc.enabled: false` possible ;
la config exige un redémarrage du moteur ; purge manuelle : `dagger core engine local-cache prune --use-default-policy`.
`keepBytes` est déprécié. Les noms de clés varient selon les versions : vérifier sur la doc de la version déployée.
Le cache des volumes (`CacheVolume`) survit entre sessions d'un même moteur ; sur runner éphémère avec moteur
auto-provisionné il est perdu (docs Dagger) [F]. Empoisonnement de cache entre zones de confiance : **[ND]**.

**Agents IA sur k0s (question de l'utilisateur) [F/E]** : oui en principe, un pod d'agent avec le CLI Dagger
et une variable pointant le moteur (`kube-pod://…` demande des droits Kubernetes `pods/exec` sur le
moteur ; `tcp://` n'a pas d'authentification). **[E]** : un moteur partagé entre CI et agents partage aussi le
cache, donc le risque d'empoisonnement ; deux moteurs séparés coûtent deux caches. Aucune source sur ce
choix.

**Dagger Cloud : gratuit ou payant ? [F, avec divergence]**
- dagger.io/pricing (lu aujourd'hui) : Starter 20 $/mois (1 utilisateur, traces 1 mois), Scale 200 $/mois,
  Enterprise sur devis ; usage facturé en plus (0,5 ¢ par CPU-minute et par Go-minute, etc. ; « le plus élevé
  du plan ou de l'usage »). Boutons « Try for free » ; **la page ne décrit pas de palier gratuit**.
- Article de blog du 2024-02-12 : visualisation des pipelines « free for individuals » ; plan Team 50 $/mois.
  Ancien, et la grille a changé depuis.
- **Nécessaire ? [F]** Non : le moteur est OSS, Cloud est optionnel (traces, cache distribué), cf. brief ;
  la page de pricing ne le dit pas elle-même. Le cache d'un moteur persistant auto-hébergé n'en dépend pas.

## Preuves pour et contre le problème / la solution

Pour le problème (le cache est la clé) :
- [F] Doc Dagger : sur runner éphémère (GitHub Actions), le daemon est recréé à chaque run et le cache perdu.
- [F] Doc Go : `-race` = 2-20x de temps, 5-10x de mémoire ; donc le coût de `test-race` est structurel.
- [F] Blog (gaqzi, dev.to) : cacher `GOCACHE` a réduit le temps de CI « roughly in half » (cas unique, sans `-race`).
- [F] Un projet tiers note qu'un cache Dagger vide sur runner frais retélécharge tout l'arbre de dépendances (anecdotique).

Contre ou non démontré :
- [ND] Aucun benchmark avant/après d'un cache de moteur Dagger persistant pour des tests Go, ni pour `-race`.
- [F] Depot : -22 % sur un build avec cache distant, « slight » pour les tests : le cache aide moins que le
  compile seul ne le laisserait croire.
- [F] Go : les résultats de test ne se rejouent pas si les variables d'environnement lues par les tests
  changent (ex. SHA du commit) ; un passage de chemin de fichier à `go test` n'est jamais caché (blog gaqzi).
- [ND] Piège cgo sous Dagger : seulement indirect. `-race` exige `CGO_ENABLED=1` + gcc (doc Go) ; une image
  Alpine sans `gcc`/`musl-dev` échoue (issues Go). Les caches se règlent par `GOCACHE`/`GOMODCACHE` montés en
  `CacheVolume` (docs Dagger). Un issue Go (#61608) demande le cache des résultats `-race` ; je n'ai pas
  vérifié son état, ne pas s'y fier.
- [ND] Tests qui ont besoin de Docker ou du réseau sous Dagger : pas de source. Pour DevDesk : des paquets
  touchent Docker/Podman, réseau, keyring (CLAUDE.md du dépôt) ; à inventorier avant de promettre une
  exécution dans un conteneur Dagger.
- [F] Earthly, concurrent direct, a cessé son service cloud en 2025 faute de revenus : risque de pérennité
  à garder en tête pour tout outil de CI tiers, y compris Dagger Cloud (aucune source ne dit que Dagger est en danger).

## Sécurité : runners self-hosted et dépôt public

Faits sourcés :
- [F] GitHub, « Secure use » : « self-hosted runners should almost never be used for public repositories »
  car n'importe qui peut ouvrir une PR et compromettre l'environnement. Les runners self-hosted « can be
  persistently compromised by untrusted code » ; les runners JIT n'exécutent qu'un job, mais réutiliser le matériel
  « can risk exposing information from the environment ». `pull_request_target` : à éviter, et ne jamais y
  extraire du code de fork.
- [F] Gestion des accès : les groupes de runners limitent par dépôt ; **par défaut seuls les dépôts privés
  accèdent aux runners d'un groupe** ; GitHub recommande ces runners uniquement pour des dépôts privés.
  Les groupes supplémentaires exigent une organisation (plan Team) ; `anthnel` est de type User (API) : **pas de groupes**, donc
  runner au niveau dépôt uniquement. Dans ce cas, le défaut « seulement privés » du groupe par défaut ne
  s'applique pas ; **[ND]** : comportement exact d'un runner de dépôt public.
- [F] Paramètres Actions : par défaut, les contributeurs first-time exigent une approbation ; options plus
  strictes « tous les contributeurs externes ». Faille documentée : un contributeur dont une PR a déjà
  été fusionnée saute l'approbation. Les workflows de forks n'ont pas accès aux secrets. GitHub avertit qu'avec un
  runner self-hosted du code malveillant s'exécute automatiquement si l'approbation est contournée ou accordée.
- [F] ARC, doc « deploy runner scale sets » : les workflows « are designed to run arbitrary code », cluster
  partagé de production = risque ; mode `dind` = conteneur DinD privilégié ; mode `kubernetes` = pod par job dans
  le même namespace ; autoriser des jobs sans conteneur en mode Kubernetes peut donner au pod du runner des
  privilèges élevés sur l'API Kubernetes ; scale à zéro si `minRunners`/`maxRunners` non définis.
- [E] Pour DevDesk, la condition `head.repo.full_name == github.repository` + approbation « tous les externes » +
  job sans secrets + NetworkPolicy sortante limitée réduisent le risque, mais un contributeur avec droit d'écriture
  (ou un compte compromis) reste un vecteur ; un moteur Dagger privilégié aggrave l'impact. **[ND]** : bonnes pratiques
  ARC officielles spécifiques à l'isolation réseau.
- [F, secondaire] GitHub avait annoncé en décembre 2025 une redevance de 0,002 $/min sur les runners self-hosted
  (dépôts publics exclus), puis l'a reportée ; état à mi-novembre 2026 confirmé seulement par des sources secondaires.

## Divergences entre sources

1. **Nom de la variable de connexion** : docs v0.21.7 et pages « scaling » : `_EXPERIMENTAL_DAGGER_RUNNER_HOST`
   (explicitement expérimentale). Page /self-hosting actuelle : `DAGGER_ENGINE`. Le changelog v0.21 ne mentionne pas de renommage.
   À trancher en lisant les notes de version ou le code de la v0.21.12.
2. **Dagger Cloud gratuit ?** Blog 2024 + FAQ : gratuit pour les individus. Page pricing actuelle : Starter 20 $/mois
   avec essai, sans palier gratuit décrit. Sites tiers (sept. 2026) : un plan Individual gratuit existerait.
3. **Moteur Kubernetes** : docs actuelles = DaemonSet par nœud ; StatefulSet/PVC présenté comme l'option pour des runners
   hors du cluster. Le brief envisageait un StatefulSet pour des runners dans le cluster : non documenté comme cas nominal.
4. **Pages 404** : plusieurs URL de docs Dagger (custom-runner, deployment/openshift, integrations/kubernetes) renvoient 404 ; les
   informations proviennent de résumés de moteur de recherche et de /self-hosting. À reconfirmer.

## Absences de données à combler (si la décision l'exige)

- Temps réel de `go test -race ./...` sur un runner GitHub-hosted à cache chaud (mesure sur le dépôt : [ND]).
- Répartition du temps : compilation `-race` vs exécution (non mesurée ; `go test -json` / `-v` pourrait la donner).
- Nix, Bazel, ARC + PVC, mise cache : non étudiés en profondeur.
- Rootless Dagger sous k0s ; concurrence multi-clients ; empoisonnement de cache.
- Prix actuels de Depot ; disponibilité de runners plus gros sur compte User.

## Sources (consultées le 2026-11-21)

- https://dagger.io/pricing
- https://dagger.io/blog/new-dagger-cloud-pricing (2024-02-12)
- https://docs.dagger.io/self-hosting
- https://raw.githubusercontent.com/dagger/dagger/main/helm/dagger/values.yaml
- https://api.github.com/repos/dagger/dagger/releases/latest (v0.21.12, 2026-11-17)
- https://dagger.io/changelog/
- https://docs.dagger.io/configuration/engine (via recherche ; GC, engine.json)
- https://docs.dagger.io/reference/configuration/custom-runner (via recherche ; la page a renvoyé 404 en lecture directe)
- https://docs.dagger.io/reference/deployment/openshift (via recherche seulement)
- https://github.com/dagger/dagger/issues/7387 , /7392 , /pull/7395 (via recherche)
- https://docs.dagger.io/1237/persistent-cache-with-dagger , https://docs.dagger.io/manuals/developer/cache-volumes (via recherche)
- https://go.dev/doc/articles/race_detector
- https://golang.org/issue/61608 , /33527 , /65814
- https://dev.to/gaqzi/working-with-gos-test-cache-on-ci-be-fast-by-avoiding-work-while-doing-the-important-work-21kd
- https://mattermost.com/blog/cutting-test-runtime-by-60-with-selective-parallelism-in-go/
- https://runs-on.com/docs/guides/languages/go/
- https://depot.dev/blog/go-remote-cache
- https://earthly.dev/blog/shutting-down-earthfiles-cloud/
- https://docs.github.com/en/actions/reference/security/secure-use
- https://docs.github.com/en/actions/how-tos/manage-runners/self-hosted-runners/manage-access
- https://docs.github.com/en/actions/reference/runners/github-hosted-runners
- https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/enabling-features-for-your-repository/managing-github-actions-settings-for-a-repository
- https://docs.github.com/en/actions/managing-workflow-runs-and-deployments/managing-workflow-runs/approving-workflow-runs-from-public-forks
- https://docs.github.com/en/actions/how-tos/manage-runners/use-actions-runner-controller/deploy-runner-scale-sets
- https://docs.github.com/en/actions/concepts/runners/self-hosted-runners
- https://api.github.com/users/anthnel (type : User)
- https://resources.github.com/actions/2026-pricing-changes-for-github-actions/ , https://www.theregister.com/2025/12/17/github_charge_dev_own_hardware/ (via recherche)
- https://kubernetes.io/blog/2026/09/04/kubernetes-v1-37-rootless-beta/ (via recherche)
