# Mesure de l'accès à un Nexus : routage, authentification, realm Bearer, redirections, `Link` (NX-05, DEVDESK-508, DEVDESK-580 PR4)

Note de mesure du 2026-11-20. Story de mesure, avec des **écritures sur le banc** (W5 et W6, voir « Méthode ») :
aucun code produit n'a été touché ; les tests ajoutés figent ce que la mesure donne, y compris quand elle
contredit un critère ou le code (décision de l'utilisateur : une mesure qui contredit est **figée**, jamais
corrigée dans le code).

| | |
|---|---|
| Version de Nexus mesurée | **`3.98.1-01` COMMUNITY** (`nexusVersion` et `edition` de `internal/nexusbench/descriptor.json`, en-tête `Server: Nexus/3.98.1-01 (COMMUNITY)` et en-tête `nexus:` des 43 fixtures) |
| Banc | cluster k0s `datomir`, namespace `nexus-bench`, REST, UI et dépôts par chemin sur `http://127.0.0.1:30481` (NodePort), connecteur Docker de `docker-group` sur `http://127.0.0.1:30482` ; HTTP seulement, aucun reverse proxy ; aucun Ingress ni DNS de sous-domaine (rapporté, aucun fichier ne le prouve) |
| État avant mesure | `nominal` : jeu de données conforme, `DockerToken` actif, accès anonyme ouvert (rapporté par l'orchestrateur, sortie de `mise run nexus-bench:status` non versée) |
| État après mesure | `Provision` au retour de chaque écriture, puis contrôle du jeu de données, des realms (comme avant, comparés comme ensemble : `Provision` ajoute `DockerToken` en fin de liste) et de l'accès anonyme (le test gardé le vérifie, rapporté : le test d'intégration n'a pas été rejoué pour cette PR) |
| Réponses brutes | `docs/measurements/nexus-access/` (43 fichiers, copies octet pour octet de `internal/nexusfixture/fixtures/access/`) ; le catalogue nominal des mêmes dépôts : `docs/measurements/nexus-catalog/` (NX-03) |

Convention de lecture : **Fait mesuré** = observé sur le banc, la réponse brute est jointe (fixture). **Rapporté** =
dit par l'orchestrateur ou la session du 2026-11-20 sans fichier versé. **Rejoué** = comportement de DevDesk obtenu par
un test sur les fixtures, sans banc. **Inférence** = déduit de la lecture du code, non exécuté. **Hypothèse** = non
vérifié. Les expressions « verdict » et « refused » sont celles de `imagesearch.Classify`.

## Méthode et contraintes

Les 43 fixtures ont été écrites en `pending` (requête seule) puis enregistrées par `nexus-bench record`, avec Basic
envoyé d'emblée (`admin`, `bench-pull`), un **mauvais mot de passe public** de l'utilisateur `admin` (`badcred`) ou sans
identifiants (`anon`), expurgées (`Authorization` retiré, jeton remplacé par `<redacted>`, en-têtes de réponse filtrés)
et vérifiées contre les fuites. Les fixtures marquées `flow: bearer` suivent le défi : un 401 `Bearer`, la requête du
jeton sur le realm (même origine exigée), la requête rejouée avec `Bearer <jeton>`. Celles marquées `flow: redirects`
suivraient un 3xx de même origine ; il n'y en a eu aucun.

| Phase | État du banc | Fixtures | Écriture |
|---|---|---|---|
| Lecture | `nominal` | `routing-*` (9), `bearer-*` (6), `realm-probe-*` (2), `redirects-*` (7), `tags-page-hosted-n1` (1) : 25 | aucune |
| W5 | `realm-off` : `DockerToken` retiré des realms actifs (`PUT /service/rest/v1/security/realms/active`) | `w5-*` (10) | oui, rattrapée par `Provision` |
| W6 | `anon-off` : accès anonyme coupé (`PUT /service/rest/v1/security/anonymous`) ; le jeu de données est comparé en `admin` (`DatasetAsAdmin`) | `w6-*` (8) | oui, rattrapée par `Provision` |

W5 et W6 sont écrites par `TestIntegrationDevDeskAccessVariants` (`internal/nexusbench`, gardé par
`DEVDESK_NEXUS_BENCH=1` et `DEVDESK_NEXUS_BENCH_SCENARIOS=1`) : le test écrit, enregistre en processus les fixtures `w5-`
puis `w6-`, appelle `Provision` à chaque retour (enregistré par `t.Cleanup` avant la première écriture) et contrôle
l'état nominal à la fin. **W7 (connecteur HTTP dédié de `docker-hosted`) et W8 (essai de `docker.subdomain`) n'ont pas été
faites** : aucun connecteur n'existe pour `docker-hosted` ni `docker-proxy` (`httpPort: null`), aucun sous-domaine n'a été
tenté. Aucun HEAD ni GET de manifeste ou de blob à travers le proxy ou le groupe (un manifeste lu à travers le proxy le met
en cache) : le HEAD de manifeste et le GET de blob ne sont mesurés que sur `docker-hosted`.

Écritures sur le banc : W5 et W6 uniquement, toutes deux rattrapées ; aucun point irréversible. Les écritures de
`cachedonly` (W1 à W4, PR3) ne sont pas répétées.

Le jeu de données du banc (descripteur) : `docker-hosted` = `api` (tags `1.0.0`, `1.1.0`), `team/api`, `tools/cli`,
`shared/base` ; amont `bench-upstream` = `mirror/cached`, `mirror/uncached`, `shared/base`.

## Réponses par famille

Les trois identités : `admin`, `pull` (`bench-pull`), `anon` ; `badcred` = `admin` avec un mauvais mot de passe.

### Routage (fixtures `routing-tags-*`, `bearer-catalog-*`, plus le catalogue de NX-03)

| Mode | Requête | Réponse |
|---|---|---|
| par chemin, port HTTP 30481 | `GET /v2/docker-hosted/api/tags/list` | **200** `{"name":"api","tags":["1.0.0","1.1.0"]}` pour `admin`, `pull` **et** `anon` |
| par `/repository/<r>`, port HTTP | `GET /repository/docker-hosted/v2/api/tags/list` | **200**, même corps, trois identités |
| par connecteur, port 30482 (groupe) | `GET /v2/api/tags/list` | **200**, même corps, trois identités |
| catalogue par chemin / connecteur | `GET /v2/docker-hosted/_catalog?n=1000`, `GET /v2/_catalog?n=1000` (30482) | **200** pour `anon` (`bearer-catalog-*--anon`), sans défi |

Le même `tags/list` de `api` est donc identique par les trois chemins. Aucun `Link` sur ces listes (DevDesk n'envoie pas `n`).

### Authentification (fixtures `bearer-*`, `realm-probe-*`, `w5-*`, `w6-*`)

| Caller | Requête | Nominal | `DockerToken` retiré (W5) | Accès anonyme coupé (W6) |
|---|---|---|---|---|
| `admin`, `pull` (Basic) | catalogue, `tags/list` | **200** | **200** (admin) | **200** (admin) |
| `badcred` | catalogue | **401**, `WWW-Authenticate: BASIC realm="Sonatype Nexus Repository Manager"`, corps vide | idem | idem |
| `anon` | catalogue, `tags/list` | **200**, aucun défi | **200**, aucun défi | **401** `Bearer realm="http://127.0.0.1:30482/v2/token",service=…,scope="repository:*:pull"` (30482) ; `realm=".../v2/docker-hosted/token"` (chemin hosted) |
| `anon` | `GET /v2/` | **401** `Bearer realm="http://127.0.0.1:30482/v2/token"` (30482) ; `realm=".../repository/docker-hosted/v2/token"` (30481) | défi **inchangé** | défi inchangé |

Le flux enregistré de `GET /v2/` (`bearer-ping-*`, `w5-bearer-ping-*`) : 401 `Bearer` ; requête du jeton sur le realm
(`GET /v2/token?service=<realm>` sur 30482, `GET /repository/docker-hosted/v2/token?service=<realm>` sur 30481) : **200**
`{"token":"…"}`, y compris pour un appelant anonyme ; `GET /v2/` rejoué avec le jeton : **401 de nouveau** (nominal : le même défi
`Bearer` ; W5 : `BASIC realm=…`). Le jeton obtenu ne rend donc pas la requête acceptée sur ce banc. W6 : le défi de l'accès coupé
est reconnu, mais ce que Nexus répond à la requête du jeton de ce realm n'a **pas** été mesuré (les fixtures `w6-catalog-*`
n'ont pas `flow: bearer`).

### Sonde du realm (fixtures `realm-probe-ping-*--anon`, `flow: bearer`)

`GET /v2/` avec `X-Forwarded-Host: realm-probe.invalid` et `X-Forwarded-Proto: https` : le realm devient
`https://realm-probe.invalid/v2/token` (30482) et `https://realm-probe.invalid/repository/docker-hosted/v2/token` (30481).
Nexus construit donc le realm à partir de l'hôte et du schéma **annoncés**. L'enregistreur a **refusé de suivre** ce realm (hors
`Origins`) : la fixture ne porte que le 401 (en-tête `nexus: … (realm off-origin, not followed)`).

### Redirections (fixtures `redirects-*`, `flow: redirects`)

| Requête | Réponse |
|---|---|
| `GET /v2` sur 30481 (sans `/` final) | **200** (admin), `Docker-Distribution-Api-Version`, corps vide ; **401** `Bearer` (anon) |
| `GET /v2` sur 30482 | **404** (page HTML) |
| `GET /v2/docker-hosted` | **404** (page HTML) |
| `GET /repository/docker-hosted/v2` | **404** (page HTML) |
| `HEAD /v2/docker-hosted/api/manifests/1.0.0` | **200**, `Docker-Content-Digest: sha256:241d539e…`, aucun 3xx |
| `GET /v2/docker-hosted/api/blobs/sha256:dac904bd…` | **200**, 3072 octets, aucun 3xx (corps non conservé : seul le résumé est dans la fixture) |

**Aucune réponse de la campagne, ni de `catalog` ni de `cachedonly`, ne porte un statut 3xx ou un en-tête `Location`.**

### `Link` des tags (fixture `tags-page-hosted-n1--admin`)

`GET /v2/docker-hosted/api/tags/list?n=1` : **200** `{"name":"api","tags":["1.0.0"]}` (`n` est respecté),
`Link: <http://127.0.0.1:30481/v2/docker-hosted/api/tags/list?n=1&last=1.0.0>; rel="next"` : URL **absolue, même origine**.
La seconde page n'a pas été enregistrée. La pagination du catalogue (forme `last=shared%2Fbase`, doublons du groupe) est celle de
NX-03 et de PR1 (`page-group-n1-p0..p5` etc.).

## Critères de NX-05, un par un

### CA1 : tags/list par chemin, par connecteur, par sous-domaine

**Fait mesuré** : par chemin (30481, `/v2/<dépôt>/…`) et par `/repository/<dépôt>/v2/…` : 200 ; par connecteur du groupe
(30482) : 200 ; les 404 sont ceux de la racine d'un hosted sur le port HTTP (`catalog-hosted-root-30481`, NX-03), de `/v2` sur le
connecteur et de `/v2/docker-hosted` (ci-dessus). **Sous-domaine : non mesuré** (aucun DNS de sous-domaine, aucune règle Traefik
étudiée, W8 non faite ; le connecteur de sous-domaine est documenté Pro par Sonatype, **hypothèse** non vérifiée sur Community
3.98). Connecteur dédié de `docker-hosted` : non créé (W7 non faite). Erreur de schéma `http://` : non rencontrée.
**Rejoué** : `TestURLsDevDeskCallsAreTheMeasuredOnes` (`internal/ui/oci_resources`) : le navigateur, `CatalogSearcher` et le client
registre appellent exactement les URL qui ont répondu (voir CA5).

### CA2 : le défi `401`, l'URL du realm, la même origine

**Fait mesuré** : le défi d'un `401` anonyme (`GET /v2/`) est `Bearer`, avec un realm sur la **même origine** que la requête
(même schéma, même hôte:port), le chemin du connecteur ou du dépôt ; `DockerToken` actif ou retiré, le défi est le même. Un
mauvais mot de passe n'obtient **pas** de défi `Bearer` mais `BASIC realm="Sonatype Nexus Repository Manager"`. Avec l'accès anonyme
coupé, un appel anonyme de catalogue est défié `Bearer` avec `scope="repository:*:pull"` et un realm par dépôt
(`/v2/docker-hosted/token` pour le chemin hosted). Realm **hors origine** : non reproductible sans reverse proxy ; la sonde montre que
le realm suit l'hôte annoncé (voir « Sonde »).

### CA3 : redirections

**Fait mesuré** : aucune, pour `tags`, `_catalog`, le HEAD de manifeste et le GET de blob sur `docker-hosted`, et pour les trois formes
de `/v2` sans barre ou de chemin partiel. Pas de redirection hors origine non plus (blob store fichier, pas de S3 : rapporté, aucun fichier ne le prouve ; pas de
reverse proxy). Redirection hors origine : **non concluant** ; D142 (refus) n'est exercée que par les tests écrits à la main.
`GET /v2` sans barre sur 30481 est servi directement (200 ou 401), pas redirigé.

### CA4 : `Link` et `n`

**Fait mesuré** : `n` est respecté (tags comme catalogue, NX-03) ; le `Link` est absolu, même origine, `last=` porte le dernier nom
(échappé `%2F` dans un catalogue). DevDesk ne pagine pas les tags (pas de `n` envoyé, donc pas de `Link`).
**Rejoué** : `TestTagsLinkOnMeasuredNexusAnswers` (`internal/oci`) : `resolveNext` rend l'URL attendue et `sameOrigin` la juge suivable ;
la marche du catalogue est celle de PR1 (`TestCatalogWalk*`, `internal/oci`).

### CA5 : admin, pull, anonyme, par mode

**Fait mesuré** : les trois accès répondent 200 sur tous les modes mesurés (chemin, `/repository/<r>`, connecteur) pour `tags/list`
et pour les catalogues, y compris l'anonyme, qui n'est pas défié. Seul `GET /v2/` anonyme est défié (`Bearer`). Le `pull` n'a pas été
mesuré dans les états W5 et W6 (seuls `admin`, `anon`, `badcred`).

### CA6 : H4, classée

| Point de H4 | Classement | Appui |
|---|---|---|
| routage par chemin (port HTTP principal, sans Traefik) | **confirmé** | `routing-tags-hosted-path`, `-repository` |
| routage par connecteur (groupe) | **confirmé** pour le groupe sur 30482 | `routing-tags-group-connector-30482` ; un connecteur pour `docker-hosted` et `docker-proxy` : **non concluant** (W7 non faite) |
| routage par sous-domaine | **non concluant** | non mesuré : DNS absent, connecteur probablement Pro, W8 non faite |
| realm Bearer sur la même origine | **confirmé** (nominal, W5, W6) | `bearer-ping-*`, `w5-*`, `w6-*` |
| realm Bearer hors origine derrière un reverse proxy | **non concluant** | non reproductible ; la sonde `X-Forwarded-*` montre que le realm **suit l'hôte annoncé**, ce qui rend l'hypothèse plausible sans la prouver sur une topologie réelle |
| redirections de même origine | **aucune mesurée** (contredit l'attente d'en trouver) | `redirects-*` |
| redirections hors origine | **non concluant** | non reproductible sans reverse proxy ni S3 |
| `Link`, forme et `n` | **confirmé** (absolu, même origine, `n` respecté) | `tags-page-hosted-n1`, NX-03 |

Chaque fixture est une réponse brute expurgée, conservée pour NX-08 (voir l'index).

## Comportement de DevDesk, rejoué

Verdicts réels (`imagesearch.Classify`, via `CatalogSearcher`, `TestCatalogVerdictsOnMeasuredNexusAccess` et le navigateur) :

| Cas | Verdict | Cause |
|---|---|---|
| `badcred`, catalogue du connecteur ou d'un membre par chemin, nominal, W5 ou W6 | **`refused`** (« unsupported or refused ») | `*oci.StatusError{401, SourceRegistry}`, `Challenge` = le défi `BASIC`, message `unexpected auth challenge: BASIC realm=…` ; **aucune** requête de jeton |
| `admin`, `pull` (Basic) | `ok` | une seule requête, Basic d'emblée, aucune sur `/v2/token` (`TestValidCredentialsNeverEnterTheBearerFlowOnNexus`) |
| `anon`, nominal et W5 | `ok` | 200, aucun défi |
| `anon`, W6 | **non épinglé** | le client va au realm (même origine, `scope=repository:*:pull`) ; ce que Nexus y répond n'est pas mesuré |
| `GET /v2/` anonyme (flux d'un client Docker) | 401 simple | le client suit 401, jeton, nouvelle requête avec le jeton, et rend un `*oci.StatusError{401, SourceRegistry}` (« registry returned 401 ») : le défi de la seconde réponse n'est pas relevé |
| realm `https://realm-probe.invalid/…` annoncé par la sonde | **suivi** | requête de jeton envoyée à l'hôte hors origine, Basic inclus si des identifiants sont configurés |

Tests : `internal/oci/registry_fixture_test.go` (`TestValidCredentialsNeverEnterTheBearerFlowOnNexus`, `TestBearerFlowOnMeasuredNexusAnswers`,
`TestMeasuredRealmStaysOnTheRegistryOrigin`, `TestRedirectsOnMeasuredNexusAnswers`, `TestTagsLinkOnMeasuredNexusAnswers`),
`internal/imagesearch/access_fixture_test.go`, `internal/ui/oci_resources/browser_nexus_routing_fixture_test.go`. Chacun vérifie d'abord
le statut enregistré de ses fixtures et échoue en les nommant si la mesure change ; une requête que Nexus n'a pas servie est une erreur
du test (le faux la nomme). Une mutation d'une fixture (statut, défi, realm, `Location`, tags, `Link`) rougit au moins un de ces tests.

## Contradictions avec les critères de NX-08b et avec le code

| # | Contradiction | Source |
|---|---|---|
| 1 | **NX-08b critère 3** (« les realms hors origine sont refusés comme mesuré ») : **rien de mesuré ne montre un refus** (le banc ne produit pas de realm hors origine, et l'enregistreur seul a refusé de suivre la sonde) ; et **le code ne refuse pas** un realm d'une autre origine : `checkTokenRealm` ne refuse que par schéma (http derrière https, schéma qui n'est ni http ni https). Un realm `https` hors origine est suivi, avec Basic si des identifiants sont configurés. Figé par `TestMeasuredRealmStaysOnTheRegistryOrigin`. | `realm-probe-ping-*`, `internal/oci/registry.go` |
| 2 | **NX-08b critère 3** (« le client suit le flux Bearer : 401, jeton, requête ») : suivi, mais le flux **n'aboutit jamais** sur ce banc : la requête rejouée avec le jeton est refusée de nouveau (401) ; et avec des identifiants valides ou un mauvais mot de passe, DevDesk **n'entre pas** dans le flux (Basic d'emblée, 200 ; ou 401 `BASIC`, pas de défi `Bearer`). Le flux n'est entré que par un appelant anonyme sur `GET /v2/` ou, accès anonyme coupé, sur un catalogue. | `bearer-ping-*`, `w5-bearer-ping-*`, `bearer-catalog-*--badcred` |
| 3 | **NX-08b critère 4** (redirections « suivi ou refus hors origine ») : **aucune redirection** mesurée ; il n'y a rien à suivre ni à refuser, les réponses directes sont figées. Le refus hors origine reste couvert par les tests écrits à la main : `TestRegistryRedirectOffOriginSendsNoCredentials`, `TestRegistryRedirectRoundTripRefused`, `TestRegistryRedirectRefusalDoesNotLeakTarget`, `TestListRegistryTagsNextOffOriginIsAnError`, `TestClientRefusesOffOriginRedirect`, `TestBlobRedirectOffOriginDropsCredentials`. | `redirects-*` |
| 4 | **NX-05 CA3** (signaler les redirections et leur origine) : aucune à signaler. | idem |
| 5 | **Code** : un mauvais mot de passe est « refusé » sans que l'utilisateur sache que c'est le mot de passe (le défi `BASIC` est dans l'erreur, pas dans le message affiché). | `bearer-catalog-*--badcred` |
| 6 | **Code** : une identité enregistrée pour l'hôte (`docker login 127.0.0.1:30481`) n'est **pas** retrouvée pour une entrée dont l'URL porte `/repository/<dépôt>` (la clé de recherche est l'adresse complète, chemin compris). Constaté en écrivant le test, non mesuré sur le banc. | `TestURLsDevDeskCallsAreTheMeasuredOnes` |
| 7 | **Code** : le client rend « registry returned 401 » après un flux Bearer qui a obtenu un jeton, sans dire que le jeton n'a pas été accepté. | `bearer-ping-*` |
| 8 | **NX-08b critère 2** (`failed` avec cause typée) : **non obtenu** sur `access`. Aucun 5xx dans la campagne, tout échec est `refused` (401 `BASIC`, 401 après jeton). Le `failed` typé 502 vient de la campagne `cachedonly` (cut1), pas de celle-ci. | toutes les fixtures `access` |

Les tests W6 anonymes (`internal/oci/registry_fixture_test.go`, autour de la ligne 454 ; `internal/imagesearch/access_fixture_test.go`,
autour de la ligne 170) épinglent `SourceToken` et « pas `ok` » **parce que le faux n'a aucune réponse enregistrée pour la requête de jeton**
(seule la requête est attendue, une erreur du faux la nomme) : ce n'est pas un verdict mesuré. Le verdict réel d'un appel anonyme sous W6
n'est **pas mesuré**.

**Constat de sécurité** : `checkTokenRealm` (`internal/oci/registry.go`) suit un realm `https` d'une autre origine et lui envoie les
identifiants Basic configurés. Comportement existant, figé par `internal/oci/registry_fixture_test.go` (autour des lignes 613-639). Correction
conditionnelle proposée, **non implémentée** : n'envoyer les identifiants qu'à un realm de même origine que le registre, ou le demander
explicitement.

Rien n'est corrigé dans le code. Critère 7 de NX-08b : tenu pour cette campagne (fixtures seules, `mise run test` sans banc, chaque
fixture expurgée, porte la version de Nexus et le scénario).

## Corrections conditionnelles proposées (non implémentées, non créées dans Plane)

À arbitrer par l'utilisateur ; chacune est conditionnée à une mesure ci-dessus.

1. **Realm hors origine** : si la décision de produit est de refuser, comme le dit NX-08b critère 3, ajouter à `checkTokenRealm` un refus
   d'un realm dont l'origine diffère de celle du registre, avec une exception pour Docker Hub (`auth.docker.io`) ; sans elle, retirer la
   phrase du critère. La mesure (la sonde) montre qu'un Nexus derrière un reverse proxy peut annoncer un autre hôte : c'est le cas où le
   refus compte.
2. **Mauvais mot de passe** : distinguer, dans `Classify` ou dans le message, un 401 à défi `BASIC` (identifiants refusés) d'un 401 à défi
   `Bearer` (identifiants à fournir).
3. **Flux Bearer** : dire, après un jeton obtenu, que la requête est de nouveau refusée.
4. **Recherche des identifiants** : retrouver aussi la clé de l'hôte pour une entrée dont l'URL porte un chemin.

## Limites

- Une seule version (`3.98.1-01`), **Community**, un banc, HTTP seulement : pas de TLS, pas de reverse proxy, pas d'instance d'entreprise.
- **Non faits** : W7 (connecteur dédié de `docker-hosted`), W8 (`docker.subdomain`), toute mesure par sous-domaine ; realm et redirection
  hors origine ; HEAD et GET de manifeste ou de blob à travers le proxy ou le groupe ; la requête de jeton sous W6 ; le compte `pull` sous
  W5 et W6 ; la seconde page des tags.
- Le GET du blob est enregistré en résumé (3072 octets non conservés) : `fakenexus` ne sert pas ce résumé, le fait est épinglé depuis le fichier,
  sans rejeu.
- `bearer-ping-*` et `realm-probe-ping-*` enregistrent la même requête (`GET /v2/`, `anon`) avec des réponses différentes : `fakenexus` refuse deux
  enregistrements d'une requête, un test choisit un sous-ensemble par état (`bearer-`, `w5-`, `w6-`). Un rejeu en bloc de la campagne est refusé.
- Les trois catalogues `catalog-*-repository--*` et `catalog-group-connector-30482--{pull,anon}` ont été enregistrés sans `n` ; le faux les sert
  pour `n=1000` par tolérance (inférence pour ces cibles, mesurée pour le groupe en admin).
- Les sorties de `status`, l'état du banc après W5 et W6 et l'exécution de `TestIntegrationDevDeskAccessVariants` sont **rapportés par la session
  du 2026-11-20**, non rejouables par une fixture.

## Index des fixtures (`internal/nexusfixture/fixtures/access/`, copies dans `docs/measurements/nexus-access/`)

| Famille | Fichiers (suffixe `--<identité>.http`) | État | Critères |
|---|---|---|---|
| `routing-tags-hosted-path`, `routing-tags-hosted-repository`, `routing-tags-group-connector-30482` | `--admin`, `--pull`, `--anon` | nominal | CA1, CA5 |
| `bearer-catalog-hosted-path`, `bearer-catalog-group-connector-30482` | `--anon`, `--badcred` | nominal, `flow: bearer` | CA2, CA5 |
| `bearer-ping-30481`, `bearer-ping-30482` | `--anon` (3 échanges) | nominal, `flow: bearer` | CA2 |
| `realm-probe-ping-30481`, `realm-probe-ping-30482` | `--anon` (X-Forwarded-*) | nominal, `flow: bearer` | CA2, H4 |
| `redirects-v2-no-slash-30481`, `redirects-v2-no-slash-30482`, `redirects-hosted-path-root`, `redirects-repository-v2-no-slash`, `redirects-head-hosted-manifest-api`, `redirects-get-hosted-blob-api` | `--admin` (+ `--anon` pour le premier) | nominal, `flow: redirects` | CA3 |
| `tags-page-hosted-n1` | `--admin` | nominal | CA4 |
| `w5-ping-*`, `w5-bearer-ping-*`, `w5-catalog-hosted-path`, `w5-catalog-group-connector-30482` | `--anon` (pings), `--admin`, `--anon`, `--badcred` (catalogues) | `realm-off` | CA2, CA5 |
| `w6-ping-*`, `w6-catalog-hosted-path`, `w6-catalog-group-connector-30482` | `--anon` (pings), `--admin`, `--anon`, `--badcred` (catalogues) | `anon-off` | CA2, CA5 |
