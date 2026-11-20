# Mesure d'un proxy Nexus « cached only » : amont joignable puis coupé (NX-04, DEVDESK-507, DEVDESK-580 PR3)

Note de mesure du 2026-11-20. Story de mesure, avec des **écritures sur le banc** (W1 à W4, voir « Méthode ») :
aucun code produit n'a été touché ; les tests ajoutés figent ce que la mesure donne, y compris quand elle
contredit un critère ou le code (décision de l'utilisateur : une mesure qui contredit est **figée**, jamais
corrigée dans le code).

| | |
|---|---|
| Version de Nexus mesurée | **`3.98.1-01` COMMUNITY** (`nexusVersion` et `edition` de `internal/nexusbench/descriptor.json`, en-tête `Server: Nexus/3.98.1-01 (COMMUNITY)` et en-tête `nexus:` des 17 fixtures) |
| Banc | cluster k0s `datomir`, namespace `nexus-bench`, REST et UI sur `http://127.0.0.1:30481` (NodePort), connecteur Docker de `docker-group` sur `http://127.0.0.1:30482` ; `docker-proxy` a pour amont `bench-upstream` (registre `registry:3.0.0`, mis à l'échelle à 0 pour la coupure) |
| État avant mesure | `nominal` : amont prêt, `dataset: matches` (rapporté par l'orchestrateur, sortie de `mise run nexus-bench:status` non versée) ; `mirror/cached` en cache dans `docker-proxy`, `mirror/uncached` non |
| État après mesure | `restore-upstream` puis `reset` (W2, W4), `status` : `matches` (rapporté par l'orchestrateur) |
| Réponses brutes | `docker-proxy` : `docs/measurements/nexus-cachedonly/` (17 fichiers, copies octet pour octet de `internal/nexusfixture/fixtures/cachedonly/`) ; groupe nominal : `docs/measurements/nexus-catalog/` et la campagne `catalog` (NX-03) |

Convention de lecture : **Fait mesuré** = observé sur le banc, la réponse brute est jointe (fixture). **Rapporté** =
mesuré par l'orchestrateur sur le banc sans fichier versé (sortie de `status`, durée de 20 s, test d'intégration) ;
non vérifiable depuis les fixtures. **Rejoué** = verdict de DevDesk obtenu par un test sur les fixtures, sans banc.
**Inférence** = déduit de la lecture du code, non exécuté. **Hypothèse** = non vérifié.

## Méthode et contraintes

Les 17 fixtures ont été écrites en `pending` (requête seule, étape 10 du plan) puis enregistrées par
`nexus-bench record`, GET et HEAD, avec Basic envoyé d'emblée (`admin`, `bench-pull`) ou sans identifiants
(`anon`), expurgées (`Authorization` retiré, en-têtes de réponse filtrés) et vérifiées contre les fuites. La
requête est celle que DevDesk envoie : `GET /v2/docker-proxy/_catalog?n=1000` (le proxy est un membre atteint par
son chemin, `/v2/<dépôt>/`, DEVDESK-579), `GET .../tags/list`, `HEAD .../manifests/1.0` avec le `Accept` de
`oci.ManifestDigest`. Séquence jouée par l'orchestrateur (étape 11 du plan `DEVDESK-580-plan.md`) :

1. `mise run nexus-bench:status` : nominal.
2. `nexus-bench record --campaign cachedonly --state nominal --only 'nominal-[a-y]*'` (lectures) ; puis `status` : le
   `tags/list` de l'image non cachée **n'a pas** modifié le cache (`dataset: matches`) (rapporté).
3. **W3** `record ... --allow-cache-effects --only 'nominal-zz-*'` : le HEAD de `mirror/uncached` ; `status` :
   `dataset: differs` (le HEAD a mis l'image en cache) (rapporté).
4. **W4** `mise run nexus-bench:reset` ; `status` : `matches`.
5. **W1** `mise run nexus-bench:cut-upstream` (amont mis à l'échelle à 0).
6. `record --campaign cachedonly --state upstream-cut --request-timeout 45s --only 'cut1-*'` : la première lecture
   du proxy après la coupure (Nexus répond au bout d'environ 20 s, rapporté) ; elle **auto-bloque** le proxy.
7. `record ... --allow-cache-effects --only 'cut2-*'` : toutes les autres lectures, proxy bloqué.
8. **Mesure de DevDesk lui-même** (pas une fixture) : `TestIntegrationDevDeskOnACutUpstream`
   (`internal/nexusbench`, gardé par `DEVDESK_NEXUS_BENCH_SCENARIOS=1`), après une nouvelle coupure et un proxy non
   bloqué : voir « Limites ».
9. **W2** `mise run nexus-bench:restore-upstream`, **W4** `mise run nexus-bench:reset` ; `status` : `matches`.
10. Preuve gelée : copie des fixtures dans `docs/measurements/nexus-cachedonly/`.

Écritures sur le banc : W1 (couper l'amont), W2 (le rétablir), W3 (un HEAD qui écrit dans le cache du proxy), W4
(`reset`, qui vide ce que W3 a mis en cache). Toutes rattrapées ; aucun point irréversible.

Jeu de données (descripteur) : amont `bench-upstream` = `mirror/cached:1.0` (`sha256:fc522a36…`, **en cache** dans le
proxy), `mirror/uncached:1.0` (`sha256:b1968983…`, non cachée), `shared/base:1.0` ; `docker-hosted` = `api`, `team/api`,
`tools/cli`, `shared/base`.

## Réponses par fixture

Tout est en `admin` sauf mention. « Proxy bloqué » = Nexus a répondu `Remote Auto Blocked until <date>` ; le corps
des cinq 404 en question est `{"errors":[{"code":"UNKNOWN","message":"Remote Auto Blocked until 2026-11-20T14:24:21.987Z","detail":null}]}`.

| Fixture (sans `--<identité>.http`) | État | Requête | Réponse |
|---|---|---|---|
| `nominal-catalog-proxy-path` (`admin`, `pull`, `anon`) | amont joignable | `GET /v2/docker-proxy/_catalog?n=1000` | **200** `["mirror/cached","mirror/uncached","shared/base"]` (identique pour les trois comptes) |
| `nominal-tags-proxy-mirror-cached` | amont joignable | `GET /v2/docker-proxy/mirror/cached/tags/list` | **200** `{"name":"mirror/cached","tags":["1.0"]}` |
| `nominal-tags-proxy-mirror-uncached` | amont joignable | `GET /v2/docker-proxy/mirror/uncached/tags/list` | **200** `{"name":"mirror/uncached","tags":["1.0"]}` |
| `nominal-zz-head-proxy-mirror-uncached` | amont joignable, `effect: caches` | `HEAD /v2/docker-proxy/mirror/uncached/manifests/1.0` | **200**, `Docker-Content-Digest: sha256:b1968983…` (= le digest de l'amont) |
| `cut1-catalog-proxy-path` | amont coupé, proxy pas encore bloqué | `GET /v2/docker-proxy/_catalog?n=1000` | **502** `{"errors":[{"code":"UNKNOWN","message":"unknown","detail":null}]}` (au bout d'environ 20 s, rapporté) |
| `cut2-catalog-proxy-path` (`admin`, `pull`, `anon`) | amont coupé, proxy bloqué | idem | **404**, « Remote Auto Blocked until … » (trois comptes identiques) |
| `cut2-catalog-hosted-path` | idem | `GET /v2/docker-hosted/_catalog?n=1000` | **200** `["api","shared/base","team/api","tools/cli"]` (témoin : hosted inchangé) |
| `cut2-catalog-group-connector-30482` | idem | `GET /v2/_catalog?n=1000` sur 30482 | **200** `["api","shared/base","team/api","tools/cli"]` : les noms `mirror/*` sont **omis sans erreur** |
| `cut2-tags-proxy-mirror-cached` | idem | `GET /v2/docker-proxy/mirror/cached/tags/list` | **404**, « Remote Auto Blocked until … » (même pour l'image en cache) |
| `cut2-tags-proxy-mirror-uncached` | idem | idem, `mirror/uncached` | **404**, « Remote Auto Blocked until … » |
| `cut2-tags-group-mirror-cached` | idem | `GET /v2/mirror/cached/tags/list` sur 30482 | **200** `{"name":"mirror/cached","tags":[]}` (liste **vide**, sans erreur) |
| `cut2-head-proxy-mirror-cached` | idem, `effect: caches` | `HEAD /v2/docker-proxy/mirror/cached/manifests/1.0` | **200**, `Docker-Content-Digest: sha256:fc522a36…`, servi **depuis le cache** |
| `cut2-head-proxy-mirror-uncached` | idem, `effect: caches` | `HEAD …/mirror/uncached/manifests/1.0` | **404**, corps vide (rien à mettre en cache) |

Faits qui en sortent :

- **Amont joignable** : le catalogue du proxy liste ce que l'**amont** détient (`mirror/uncached` y figure alors que
  le proxy n'en a aucun composant) ; son `tags/list` aussi ; un HEAD le rend tirable et **l'écrit dans le cache**. Le
  catalogue et le `tags/list` ne mettent rien en cache (rapporté, `status` entre les phases).
- **Amont coupé, première lecture** : 502 après environ 20 s (rapporté), puis le proxy est **auto-bloqué**.
- **Amont coupé, proxy bloqué** : le catalogue et les `tags/list` du proxy répondent 404, **y compris pour l'image en
  cache**. Le cache n'est donc **pas listable** ; il reste **tirable** (HEAD 200 de `mirror/cached`, 404 pour
  `mirror/uncached`).
- **Le groupe** (connecteur 30482) **omet silencieusement** les noms du proxy dans son catalogue (200 avec les noms du
  hosted seuls) et répond 200 avec une liste de tags vide pour `mirror/cached`. Le hosted n'est pas affecté.

## Critères de NX-04, un par un

### CA1 : image non cachée, amont joignable : listée, tirable, ou ni l'une ni l'autre (H3)

**Fait mesuré** : `mirror/uncached` est **listée** (`_catalog` du proxy, `tags/list` : `["1.0"]`) **et tirable** (HEAD
200 avec le digest de l'amont, `nominal-zz-head-proxy-mirror-uncached`) alors que le proxy n'en détient aucun
composant. Le HEAD **l'écrit dans le cache** (rapporté) ; ni le `tags/list` ni le catalogue ne le font (rapporté).
Verdict de DevDesk **rejoué** : recherche `mirror` sur le proxy, trois comptes : `ok`, deux lignes
(`docker-proxy/mirror/cached`, `docker-proxy/mirror/uncached`), le proxy est nommé dans la ligne `Cached only`.
Le proxy **n'est donc pas limité à son cache** quand son amont répond.

### CA2 : amont coupé, image en cache : ce que renvoient `tags/list` et `_catalog`

**Fait mesuré**, par codes HTTP :

| Lecture | Première, proxy pas bloqué | Ensuite, proxy bloqué |
|---|---|---|
| `_catalog` du proxy | **502** (`UNKNOWN`/`unknown`, ~20 s, rapporté) | **404** « Remote Auto Blocked » (admin, pull, anon) |
| `tags/list` du proxy, `mirror/cached` (en cache) | non mesuré | **404** « Remote Auto Blocked » |
| `tags/list` du proxy, `mirror/uncached` | non mesuré | **404** « Remote Auto Blocked » |
| `_catalog` du groupe | non mesuré | **200**, sans les noms `mirror/*` |
| `tags/list` du groupe, `mirror/cached` | non mesuré | **200**, `"tags":[]` |
| HEAD du proxy, `mirror/cached` | non mesuré | **200**, digest depuis le cache |
| HEAD du proxy, `mirror/uncached` | non mesuré | **404**, corps vide |

Ni la liste du cache, ni une liste vide côté proxy : **une erreur** (502 puis 404). La liste du cache n'est
atteignable par aucune lecture de listing ; seul le HEAD sert encore le cache.

### CA3 : groupe mixte, par membre : listage ensemble, doublon

- **hosted** : le catalogue (`api`, `shared/base`, `team/api`, `tools/cli`) est complet ; seul l'état coupé est une
  fixture de cette campagne (`cut2-catalog-hosted-path`), l'état nominal vient de NX-03 (`catalog-hosted-path-n1000`).
  Il n'est pas « cached only ».
- **proxy** : relais de l'amont (amont joignable), 404 ou 502 (amont coupé).
- **groupe, amont joignable** (NX-03, campagne `catalog`, `page-group-n1000--admin`) : un catalogue **union** des
  membres, six noms, **`shared/base` une seule fois** (le hosted et l'amont le portent) ; un HEAD de cet homonyme
  résout vers le premier membre, le hosted (`manifest-head-group-shared-base--admin`, preuve NX-03 de
  `docs/measurements/nexus-catalog`, pas une fixture embarquée).
  Pas de doublon, mais l'image du second membre est masquée.
- **groupe, amont coupé** : l'union perd les noms du proxy **sans erreur** (`cut2-catalog-group-connector-30482`) ;
  le nom `shared/base` reste, une seule fois (celui du hosted).

Non remesuré ici : le `tags/list` du groupe d'un homonyme à amont coupé (la fixture de NX-03, amont joignable, reste la
seule).

### CA4 : verdict que DevDesk affiche, case par case (**rejoué**, tests sur fixtures)

Recherche `mirror`, membres par chemin (`nx/docker-hosted`, `nx/docker-proxy`), identité `admin` sauf mention.
« Mention » = la ligne `Cached only` du bandeau (la classe `Failed`/`Refused` prime sur elle).

| Cas | Verdict | Cause typée | Ce que dit le bandeau | La mention `Cached only` |
|---|---|---|---|---|
| Proxy, amont joignable (admin, pull, anon) | `ok`, 2 lignes | aucune | `Cached only : nx/docker-hosted (no result), nx/docker-proxy` | présente, **inexacte** (la liste dépasse le cache) |
| Proxy, première lecture après coupure (`cut1`) | **`failed`** | `*oci.StatusError{502, SourceRegistry}` (rejoué ; en réel, voir « Limites ») | `Failed : nx/docker-proxy` | **absente** pour le proxy |
| Proxy bloqué (`cut2`, admin, pull, anon) | **`refused`** (« unsupported or refused ») | `*oci.StatusError{404, SourceRegistry}` | `Refused : nx/docker-proxy` | **absente** pour le proxy |
| Hosted, amont joignable (NX-03) ou coupé (`cut2-`) | `no result` (rien ne contient `mirror`) | aucune | `Cached only : nx/docker-hosted (no result)` | présente, **trop large** (un hosted n'est pas limité à un cache) |
| Groupe sur son connecteur, amont joignable | `ok`, 2 lignes (`mirror/cached`, `mirror/uncached`) | aucune | `Cached only : nx (group)` | présente, inexacte (même raison que le proxy) |
| Groupe sur son connecteur, proxy bloqué | **`no result`** pour `mirror` ; `ok` (`api`, `team/api`) pour `api` | aucune | `Cached only : nx (group) (no result)` | présente et **fausse** : « (no result) » veut dire « rien dans le cache » (aide de la vue), or `mirror/cached` est en cache et tirable |
| `tags/list` du proxy bloqué (vue des tags) | erreur `*oci.StatusError{404}` | oui | (la vue des tags) | sans objet |
| `tags/list` du groupe, proxy bloqué | liste **vide**, pas d'erreur | non | (la vue des tags) | sans objet |
| HEAD (`ManifestDigest`, mise à jour d'image, signature) `mirror/cached`, proxy bloqué | digest `sha256:fc522a36…` (celui du jeu de données) | sans objet | sans objet | sans objet |
| HEAD `mirror/uncached`, proxy bloqué | **erreur non typée** `registry returned 404 for docker-proxy/mirror/uncached:1.0` | **non** | sans objet | sans objet |

`Classify` range un 502 en `failed` (seuls 401, 403, 404, 405 sont des refus) : pas de contradiction avec le code.

**Conclusion** (CA4) : la mention est **exacte pour personne** dans cette mesure.

- **Trop large** : un membre hosted est marqué `Cached only` alors que son catalogue est complet (`listsCacheOnly` décide
  par la configuration, pas par le type du membre). C'est la contradiction que NX-09 attend.
- **Inexacte pour le proxy amont joignable** : « limité au contenu en cache » est faux, le proxy relaie l'amont.
- **Insuffisante amont coupé** : sur un proxy `failed` ou `refused` la mention disparaît (le cache pourtant tirable n'est
  pas signalé), et sur le **groupe** elle reste avec `(no result)`, qui s'y lit « rien en cache » alors que le cache
  contient `mirror/cached`.

### CA5 : H3, classée

**H3 est contredite** (« le contenu listé par un proxy est celui de son cache ») : amont joignable, un proxy liste
l'amont (CA1) ; amont coupé, il ne liste rien (CA2), y compris ce qu'il détient. La mesure alimente **NX-09** et va plus
loin que sa condition de démarrage (hosted marqué à tort : **confirmé**) ; voir les corrections conditionnelles.

## Contradictions avec les critères de NX-08b et le code

| # | Contradiction | Source |
|---|---|---|
| 1 | **NX-08b critère 1** (« le verdict `Cached only` est obtenu pour le proxy et le comportement mesuré (liste du cache…) est vérifié ») : obtenu **seulement amont joignable**. Amont coupé, le proxy est `failed` puis `refused` et n'est **jamais** dans la ligne `Cached only` ; et il n'y a **pas de liste du cache** à vérifier (404/502). Figé par `TestCachedOnlyLineOnMeasuredCutUpstream`. | fixtures `nominal-`, `cut1-`, `cut2-` |
| 2 | **Critère 1, effet d'un HEAD** : tenu (le HEAD répond 200 avec le digest du jeu de données ; il écrit dans le cache, rapporté). Figé par `TestManifestDigestOnMeasuredCutUpstream`. | `nominal-zz-head-…`, `cut2-head-…` |
| 3 | **Critère 2** (« `failed` avec cause typée ») : tenu **sur la fixture** (`*oci.StatusError{502, SourceRegistry}`, `TestFailedNexusAnswerKeepsItsCause`) ; **faux en réel** : DevDesk abandonne à 15 s, avant le 502 de 20 s, donc `failed` par délai dépassé, sans `StatusError` (rapporté, test d'intégration). La cause typée vue en test n'est pas celle de l'utilisateur. | `cut1-catalog-proxy-path--admin`, test d'intégration |
| 4 | **Critère 2 sur le chemin HEAD** : l'erreur de `ManifestDigest` est **non typée** (`fmt.Errorf("registry returned %d for %s:%s")`), pas de `StatusError`. Noté (plan §1.3), figé par un `errors.As` négatif. | `cut2-head-proxy-mirror-uncached--admin` |
| 5 | **Code** : un 404 « Remote Auto Blocked until … » est un **refus** (`refusedStatus`), donc « unsupported or refused », alors que c'est un état **temporaire** du proxy ; DevDesk ne lit pas le corps. | `cut2-catalog-proxy-path--*` |
| 6 | **Code** : le groupe répond 200 sans les noms du proxy ; DevDesk ne peut pas le savoir, la mention dit `(no result)`. | `cut2-catalog-group-connector-30482--admin` |

Rien n'est corrigé dans le code. Critère 7 de NX-08b : tenu pour cette campagne (fixtures seules, `mise run test` sans
banc, chaque fixture expurgée, porte la version de Nexus et le scénario, vérifiée par `TestEmbeddedFixturesLeakNothing`
et `TestFixturesMatchTheBenchVersion`).

## Corrections conditionnelles proposées (non implémentées, non créées dans Plane)

À arbitrer par l'utilisateur ; chacune est conditionnée à une mesure ci-dessus.

1. **NX-09 (hosted marqué à tort)** : confirmée, donc à démarrer : ne marquer `Cached only` que les membres de type proxy
   (nécessite le type du membre dans la découverte).
2. **Libellé** : « limited to cached content » est faux pour un proxy amont joignable (il relaie l'amont) et
   « (no result) = nothing in the cache » est faux pour un groupe amont coupé. À reformuler (ex. « may omit
   uncached images ») ou à retirer l'interprétation de `(no result)`.
3. **404 « Remote Auto Blocked »** : le classer `failed` (temporaire) plutôt que `refused` demande de lire le message du
   corps dans `Classify`/`oci.StatusError` ; pas de code aujourd'hui, et la décision est de produit.
4. **`ManifestDigest`** : rendre l'erreur typée (`*oci.StatusError`) pour tenir le critère 2 sur le chemin HEAD.

## Limites

- **Délai 15 s contre 20 s** : une fixture se rejoue instantanément, elle ne peut pas reproduire l'attente de Nexus (20 s)
  ni l'abandon du client (15 s). Mesuré par `TestIntegrationDevDeskOnACutUpstream` : première lecture après coupure =
  verdict **`failed`** par délai dépassé (pas de 502), deuxième lecture = **`refused`** avec `*oci.StatusError` 404
  (rapporté par la session du 2026-11-20, non rejouable par une fixture ; test gardé, non rejoué ici). Le délai de 20 s
  (H-CUTDELAY) a été mesuré en NX-02b sur une lecture de manifeste non cachée, pas sur `_catalog`.
- **Ordre `cut1` puis `cut2`** : le même `GET _catalog` du proxy a trois réponses selon l'état ; elles ne cohabitent pas
  dans un même faux Nexus (un test choisit un sous-ensemble par état). Un rejeu en bloc de la campagne est refusé par
  `fakenexus` ; une régénération doit rejouer `cut1-` avant `cut2-` après une coupure fraîche.
- **Horodatage** : les cinq corps `Remote Auto Blocked until <date>` changent à chaque enregistrement. Une régénération
  les réécrit (`written`, jamais `unchanged`) sans rien rougir ; ni `record: skip` ni règle de redaction n'ont été
  utilisés (voir `docs/architecture/nexus-bench.md`). Les tests comparent le préfixe et vérifient que la suite est une
  date, jamais sa valeur.
- Une seule version (`3.98.1-01`), **Community**, un banc : pas de reverse proxy, pas d'instance d'entreprise.
- Les sorties de `status` (`dataset: differs` après le HEAD), la durée de 20 s et le test d'intégration sont **rapportés par la session du
  2026-11-20, non rejouables par une fixture** (pas versés comme fichiers).
- `cut1` n'existe qu'en `admin` ; hosted, groupe, `tags/list` et HEAD qu'en `admin` ; `pull` et `anon` seulement sur le
  catalogue du proxy.
- L'état « bloqué » des `cut2-` est **temporaire** : Nexus se débloque seul 21 s (coupure de 100 s) à 79 s (coupure de
  4,5 min) après la coupure, et le `PUT` de `restore-upstream` le lève aussitôt (H-UNBLOCK, NX-02b). Les `cut2-` ne
  décrivent donc que la fenêtre où le proxy est bloqué, pas un état durable. Non mesuré ici : le délai de blocage sur
  `_catalog` (H-CUTDELAY l'a été sur une lecture de manifeste non cachée), un `tags/list` du groupe d'un homonyme amont
  coupé, un proxy à cache vide.

## Index des fixtures (`internal/nexusfixture/fixtures/cachedonly/`, copies dans `docs/measurements/nexus-cachedonly/`)

| Famille | Fichiers (suffixe `--<identité>.http`) | Critères |
|---|---|---|
| `nominal-catalog-proxy-path` | `--admin`, `--pull`, `--anon` | CA1, CA4 |
| `nominal-tags-proxy-mirror-{cached,uncached}` | `--admin` | CA1 |
| `nominal-zz-head-proxy-mirror-uncached` | `--admin` (`effect: caches`) | CA1, effet d'un HEAD |
| `cut1-catalog-proxy-path` | `--admin` | CA2, CA4, critère 2 de NX-08b |
| `cut2-catalog-proxy-path` | `--admin`, `--pull`, `--anon` | CA2, CA4 |
| `cut2-catalog-hosted-path`, `cut2-catalog-group-connector-30482` | `--admin` | CA3, CA4 |
| `cut2-tags-proxy-mirror-{cached,uncached}`, `cut2-tags-group-mirror-cached` | `--admin` | CA2, CA3 |
| `cut2-head-proxy-mirror-{cached,uncached}` | `--admin` (`effect: caches`) | CA2, effet d'un HEAD |

Tests qui les rejouent (sans banc) : `internal/imagesearch/cachedonly_fixture_test.go`
(`TestProxyCatalogOnMeasuredCutUpstream`, `TestHostedAndGroupCatalogOnMeasuredCutUpstream`,
`TestFailedNexusAnswerKeepsItsCause`), `internal/ui/oci_resources/browser_cached_only_fixture_test.go`
(`TestCachedOnlyLineOnMeasuredCutUpstream`, `TestCachedOnlyLineOfAGroupOnItsConnectorOnMeasuredCutUpstream`),
`internal/oci/manifest_fixture_test.go` (`TestManifestDigestOnMeasuredCutUpstream`, `TestTagsOnMeasuredCutUpstream`).
Chacun vérifie d'abord le statut enregistré de ses fixtures et échoue en le nommant si la mesure change.
