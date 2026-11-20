# Mesure de `_catalog` sur Nexus : préfixe, droits, hôte partagé (NX-03, DEVDESK-506)

Note de mesure du 2026-11-17. Story de mesure : aucun code produit n'a été touché, aucune écriture
n'a été faite sur le banc (voir toutefois « Effet de bord sur le banc »).

| | |
|---|---|
| Version de Nexus mesurée | **`3.98.1-01` COMMUNITY** (`nexusVersion` et `edition` de `internal/nexusbench/descriptor.json`, image `docker.io/sonatype/nexus3:3.98.1`, confirmée par l'en-tête `Server: Nexus/3.98.1-01 (COMMUNITY)` et par `info.version` de l'OpenAPI) |
| Banc | cluster k0s `datomir`, namespace `nexus-bench`, REST et UI sur `http://127.0.0.1:30481` (NodePort, conteneur 8081), connecteur Docker de `docker-group` sur `http://127.0.0.1:30482` (NodePort, conteneur 8082) |
| État avant mesure | `mise run nexus-bench:status` : pod `running` (1/1 ready), `rest: ready`, `upstream: ready`, `descriptor: matches`, `dataset: matches (6 images compared)` |
| Réponses brutes | `docs/measurements/nexus-catalog/` (un fichier par appel, voir l'index en fin de note) |

Convention de lecture : **Fait mesuré** = observé sur le banc, la réponse brute est jointe.
**Inférence** = déduit de la lecture du code de DevDesk, non exécuté. **Hypothèse** = non vérifié.

## Méthode et contraintes

- Appels en **lecture seule** (GET, et HEAD sur des manifestes), avec `curl`, depuis `datomir`. Voir
  toutefois l'effet de bord d'un HEAD sur le proxy, plus bas.
- Trois identités : `admin` (rôle `nx-admin`), `bench-pull` (rôle `bench-docker-pull`, privilèges
  `nx-repository-view-docker-*-browse` et `-read`), et **anonyme** (sans identifiants ; l'accès
  anonyme est activé, rôle `nx-anonymous`).
- Pour `admin` et `bench-pull`, Basic **envoyé d'emblée**, comme `oci.ListCatalogContext` (« Basic,
  puis Bearer sur un défi »). Pour le connecteur du groupe, le flux **Bearer** complet (jeton demandé au
  `realm` avec les identifiants, puis `Authorization: Bearer`) a aussi été rejoué pour les trois identités.
- Les mots de passe ont été lus dans `$XDG_STATE_HOME/devdesk/nexus-bench/datomir/nexus-bench/secrets.json`
  (0600, hors dépôt) par un script hors dépôt, sans jamais être affichés ni écrits. Les fichiers versés
  n'ont ni `Authorization` (remplacé par `<redacted>`), ni jeton, ni cookie ; les pages HTML sont résumées ;
  les en-têtes de réponse sont filtrés (liste blanche : `Server`, `Content-Type`, `Content-Length`,
  `Docker-Distribution-Api-Version`, `Docker-Content-Digest`, `Link`, `WWW-Authenticate`).
- Jeu de données (descripteur) : `docker-hosted` = `api` (1.0.0, 1.1.0), `team/api`, `tools/cli`,
  `shared/base:1.0` ; l'amont `bench-upstream` = `shared/base:1.0` (autre digest), `mirror/cached`,
  `mirror/uncached` ; `mirror/cached` est en cache dans `docker-proxy`. `docker-group` = `docker-hosted`
  puis `docker-proxy`.

## Comment cibler chaque dépôt sur ce banc

**Fait mesuré** : hosted, proxy et groupe ne sont **pas** joignables de la même façon.

| Dépôt | Connecteur | Où répond son `_catalog` | Preuve |
|---|---|---|---|
| `docker-hosted` | aucun port dédié (`httpPort: null`), `pathEnabled: true` | `:30481/v2/docker-hosted/_catalog` (200) ; aussi `:30481/repository/docker-hosted/v2/_catalog` (200) | `catalog-hosted-path--*`, `catalog-hosted-repository--*`, `rest-repositories-hosted-docker-hosted--admin` |
| `docker-proxy` | aucun port dédié (`httpPort: null`), `pathEnabled: true` | `:30481/v2/docker-proxy/_catalog` (200) ; aussi `:30481/repository/docker-proxy/v2/_catalog` (200) | `catalog-proxy-path--*`, `catalog-proxy-repository--*`, `rest-repositories-proxy-docker-proxy--admin` |
| `docker-group` | connecteur HTTP `httpPort: 8082` (NodePort 30482), `pathEnabled: null` | `:30482/v2/_catalog` (200) ; aussi `:30481/repository/docker-group/v2/_catalog` (200) ; **pas** `:30481/v2/docker-group/_catalog` (404) | `catalog-group-connector-30482--*`, `catalog-group-repository--*`, `catalog-group-path--*`, `rest-repositories-group-docker-group--admin` |

- Hosted et proxy **partagent** l'hôte `127.0.0.1:30481` : seul le chemin les distingue. Le groupe a son
  propre port.
- Le port du groupe (30482) ne sert que le groupe : `/v2/docker-hosted/_catalog`,
  `/v2/docker-proxy/_catalog` et `/v2/docker-group/_catalog` y répondent 404
  (`catalog-*-path-on-connector-30482--admin`).
- Le `Link` de pagination est une URL absolue en `127.0.0.1:<port>`, l'hôte de la requête (non testé avec un autre en-tête `Host`).

## Critère 1 : `/v2/_catalog` à la racine de l'hôte, compte admin (H1)

Appel littéral du critère (`/v2/_catalog` à la racine de l'hôte), puis les variantes qui existent.
Résultats identiques pour `admin`, `bench-pull` et anonyme (voir le critère 2).

| Cible | URL (compte admin) | HTTP | `Link` | Noms | Préfixés par le dépôt ? |
|---|---|---|---|---|---|
| Racine du port HTTP (là où vivent hosted et proxy) | `:30481/v2/_catalog` | **404** (page HTML Nexus, pas une erreur Docker) | aucun | aucun | sans objet |
| Racine du port du groupe (**seul** cas où la racine répond) | `:30482/v2/_catalog` | 200 | aucun sans `n` | `api`, `mirror/cached`, `mirror/uncached`, `shared/base`, `team/api`, `tools/cli` | **non** |
| hosted, par chemin | `:30481/v2/docker-hosted/_catalog` | 200 | aucun sans `n` | `api`, `shared/base`, `team/api`, `tools/cli` | **non** |
| proxy, par chemin | `:30481/v2/docker-proxy/_catalog` | 200 | aucun sans `n` | `mirror/cached`, `mirror/uncached`, `shared/base` | **non** |
| groupe, par chemin | `:30481/v2/docker-group/_catalog` | 404 | aucun | aucun | sans objet |
| hosted, forme `/repository/<nom>/v2` | `:30481/repository/docker-hosted/v2/_catalog` | 200 | aucun sans `n` | idem hosted par chemin | **non** |
| proxy, forme `/repository/<nom>/v2` | `:30481/repository/docker-proxy/v2/_catalog` | 200 | aucun sans `n` | idem proxy par chemin | **non** |
| groupe, forme `/repository/<nom>/v2` | `:30481/repository/docker-group/v2/_catalog` | 200 | aucun sans `n` | idem groupe connecteur | **non** |

Faits tirés du tableau :

- **Aucune** réponse ne préfixe les noms par le nom du dépôt (`docker-hosted/api` n'apparaît nulle part).
  Un nom de `_catalog` est le nom **interne** à un dépôt (`api`, `team/api`), quelle que soit la forme d'URL.
  `tags/list` en fait autant : `{"name":"api",...}` pour `/v2/docker-hosted/api/tags/list`
  (`tags-hosted-api-path--admin`).
- Le préfixe fait partie du **chemin de l'URL**, pas du contenu : l'image s'appelle `docker-hosted/api` pour
  un client (`/v2/docker-hosted/api/tags/list` répond 200), mais le catalogue du dépôt dit `api`.
- À la racine du port HTTP, **il n'y a pas de catalogue global** : hosted et proxy n'y sont pas réunis,
  la route n'existe tout simplement pas (404). Seul le connecteur du groupe répond à la racine.
- La racine du port du groupe renvoie l'**union** des catalogues des membres : `shared/base` (présent dans
  hosted et dans l'amont) n'y figure qu'une fois ; `mirror/uncached` y figure alors que le proxy n'en a rien
  en cache (le proxy relaie `_catalog` à son amont, constat déjà fait dans `docs/architecture/nexus-bench.md`).
- Sans `n`, **aucun en-tête `Link`**. Avec `n` (DevDesk envoie `n=1000`), `Link: <…?n=2&last=…>; rel="next"`
  apparaît tant qu'il reste des noms (voir « Pagination »).
- Défi d'authentification : `GET /v2/` sans identifiants répond 401 avec un `WWW-Authenticate: Bearer`
  (`v2-ping-30481--anon`, `v2-ping-30482--anon`). Sur le port HTTP le `realm` pointe vers
  `/repository/docker-hosted/v2/token` (la raison de ce choix n'est pas établie), sur le port du groupe vers
  `/v2/token`.

## Critère 2 : même appel avec le compte « pull » (H2)

**Fait mesuré** : `bench-pull` est **accepté**, sans filtrage observable. Pour les huit cibles du tableau
ci-dessus, code, en-têtes et corps sont identiques à ceux de `admin` (comparés fichier par fichier) :
200 avec la même liste de six noms sur le groupe, 4 sur hosted, 3 sur proxy ; 404 là où admin a 404.
Même résultat avec le flux Bearer (`catalog-group-connector-30482-bearer--pull`, jeton obtenu : 200).

Le compte **anonyme** est lui aussi accepté, avec les mêmes listes (rôle `nx-anonymous` : privilèges
`nx-repository-view-*-*-browse` et `-read`, `ctx-role-nx-anonymous--admin`). Particularité : `GET /v2/`
sans identifiants répond 401, mais `_catalog` répond 200 sans défi (`catalog-group-connector-30482--anon`).

**Limite importante (non concluant sur le filtrage)** : les privilèges de `bench-pull` portent sur **tous
les dépôts Docker** (`nx-repository-view-docker-*-*`), et ceux d'anonyme sur tous les dépôts. Aucune
des trois identités n'a un accès restreint à un seul dépôt : un filtrage par privilège (liste partielle sans
erreur) **ne pouvait donc pas se produire** avec ces comptes. Pour le tester il faut un compte limité à un
dépôt (par exemple `nx-repository-view-docker-docker-hosted-*` seul), ce qui est une **écriture** sur le banc
(création d'un rôle et d'un utilisateur), exclue de ce passage. Le risque « filtrage silencieux = faux
no result » reste donc **non exercé** ; il n'est ni constaté ni écarté.

## Critère 3 : REST `/service/rest/v1/repositories/...` pour `bench-pull` et anonyme

Noms utilisés : `docker-group` (et, pour la forme, `docker-hosted`, `docker-proxy`).

| Appel | admin | `bench-pull` | anonyme | `memberNames` |
|---|---|---|---|---|
| `GET /v1/repositories/docker-group` (générique) | 200 | **200** | **200** | **absent** (`"attributes": {}`) pour les trois |
| `GET /v1/repositories/docker/group/docker-group` (détaillé) | 200 | **404** `{"id":"*","message":"\"Repository not found\""}` | **403** (corps vide) | **présent** pour admin seul : `["docker-hosted","docker-proxy"]` |
| `GET /v1/repositories/docker-hosted` et `docker-proxy` (génériques) | 200 | 200 | 200 | sans objet |
| `GET /v1/repositories/docker/hosted/docker-hosted` et `.../proxy/docker-proxy` (détaillés) | 200 | **404** | **403** | sans objet |

(Fichiers : `rest-repositories-*--{admin,pull,anon}`.)

- **Le fait du 2026-08-10 est confirmé pour le fond** (`docs/backlog.md` §3.18) : l'endpoint générique
  répond sans `memberNames`, avec `"attributes": {}`, et l'endpoint détaillé, seul à porter `memberNames` et
  les champs de connecteur (`docker.httpPort`, `docker.subdomain`, `docker.pathEnabled`), est **refusé** au
  compte de pull.
- **Correction de détail** : le code du refus pour `bench-pull` est **404 « Repository not found »** sur
  3.98.1-01, et non 403 comme noté le 2026-08-10 ; l'anonyme reçoit bien **403**. Ce n'est pas la même
  instance : le fait ancien venait d'un Nexus d'entreprise, de version non relevée, avec un compte de pull
  de l'entreprise. Une différence de version ou de privilèges est possible, **non tranchée** ici.
  Conséquence pour DevDesk : le refus du détaillé a deux formes (403, 404) selon l'identité ; la découverte
  des membres (V4 de `stories.md`) les avale toutes les deux.
- `GET /v1/repositories/{nom}` **existe** sur cette version : l'OpenAPI du banc le décrit (« Get repository
  details », réponses 200, 401, 403, 404) à côté de `GET /v1/repositories` (« List repositories ») et des
  endpoints par format et type (`rest-openapi-repositories-extract.json`). Sa forme : résumé
  `{"name","format","type","url","online","attributes"}`. `url` vaut `http://127.0.0.1:30481/repository/<nom>`
  (forme `/repository/`, pas le routage par chemin ni le port du groupe). Pour un proxy, `attributes.proxy.remoteUrl`
  est renseigné, **même pour l'anonyme** (ici l'URL du service amont du banc).
- Les trois identités lisent donc le **type** et l'**URL** d'un dépôt, mais pas ses membres ni la façon de
  l'atteindre (`httpPort`, `pathEnabled`) sans être admin.

## Critère 4 : un groupe dont deux membres cochés partagent un hôte (IS-07)

Cas du banc : `docker-hosted` et `docker-proxy`, membres de `docker-group`, **partagent** l'hôte
`127.0.0.1:30481`. Dans le modèle « hôte + `repo_prefix` » d'IS-07, deux entrées : hôte `127.0.0.1:30481` avec
préfixe `docker-hosted`, et même hôte avec préfixe `docker-proxy`.

- **Fait mesuré** : un parcours de `:30481/v2/_catalog` ne retrouve **aucune** image d'aucun des deux membres
  (404, `catalog-hosted-root-30481--*`). Les deux membres ne répondent pas sur le même `/v2/_catalog` d'hôte :
  chacun répond sur **son** catalogue par chemin (`/v2/docker-hosted/_catalog`, `/v2/docker-proxy/_catalog`).
  Il faut **deux** parcours, un par membre.
- **Fait mesuré** : un parcours unique de **`:30482/v2/_catalog`** (le connecteur du groupe) retrouve bien les
  images des deux membres (6 noms : les 4 de hosted dont `shared/base`, plus `mirror/cached`,
  `mirror/uncached`) en une seule réponse. Mais :
  - les noms ne sont **pas** préfixés, et rien n'indique le **membre** d'origine ; la répartition par
    préfixe de membre (IS-07 critère 2) est impossible sur cette réponse ;
  - `shared/base` existe dans **les deux** membres avec des digests différents. Le groupe le liste une fois
    et résout `shared/base:1.0` vers le **premier** membre, `docker-hosted` (digest `sha256:d9a19506…`,
    `manifest-head-group-shared-base--admin`), pas vers le proxy (`sha256:8486bd6d…`,
    `manifest-head-proxy-shared-base--admin`). Une image du second membre peut donc être **masquée** par
    l'homonyme du premier ;
  - `mirror/uncached` figure au catalogue du proxy (relayé de l'amont, voir plus haut) alors que le proxy n'en
    détient aucun composant : un nom du catalogue d'un proxy ou d'un groupe n'indique donc pas que
    l'image est en cache dans un membre (le catalogue ne dit pas ce que le proxy détient).
- **Inférence** : avec une entrée « hôte `127.0.0.1:30481` + préfixe » telle que modélisée aujourd'hui, DevDesk
  appelle `/v2/_catalog?n=1000` à la racine, reçoit 404 (source registre) et classe le registre
  `unsupported or refused` (`Classify`, `refusedStatus`), pas `no result`. Non exécuté contre le banc.
- La parade R1 (`ErrPrefixNotInCatalog`) n'est **pas atteinte** dans ce cas : elle suppose un catalogue qui
  répond, ici la réponse est un 404 avant tout filtrage.
- **Inférence** (lecture de `verdict.go:66` et `catalog.go:72-76`, non exécutée) : une entrée `127.0.0.1:30482`
  (connecteur du groupe) avec le préfixe `docker-hosted` reçoit un catalogue **non vide** dont aucun nom
  n'est préfixé ; `prefix != "" && !sawPrefix` renvoie alors `ErrPrefixNotInCatalog`, que `Classify` range
  en « unsupported or refused », et non en « no result ». C'est le cas où la parade R1 joue son rôle.

Réponse au critère : **non** pour deux membres adressés par hôte + chemin (deux parcours, aucun à la
racine) ; **oui, mais sans attribution** pour un parcours du connecteur du groupe.

## Pagination (constat annexe, utile pour NX-05)

`n` est respecté et `Link rel="next"` est émis (`page-*`). Sur hosted et proxy (parcours `n=2`), la
pagination est propre : 2 pages, la dernière sans `Link`. **Sur le connecteur du groupe, `n=2` donne un
parcours incohérent** : 5 pages au lieu de 3 pour 6 noms, avec des doublons. Pages : `[api, mirror/cached]`,
`[mirror/uncached, shared/base]`, `[team/api, tools/cli]` mais dont le `Link` renvoie `last=mirror/uncached`
(et non `tools/cli`), puis `[shared/base, team/api]`, puis `[tools/cli]` sans `Link`. `shared/base` et
`team/api` sortent deux fois. Avec `n=1` (6 pages d'un nom, chacune avec le bon `Link`, la dernière sans : `page-group-n1-p0..p5`), `n=6` et
`n=1000` (valeur de DevDesk) il n'y a pas d'anomalie.
Un parcours en une page ne l'exerce pas : cela ne vaut que pour un groupe de plus de `n` noms. Hypothèse :
le groupe fusionne les pages de ses membres ; non tranché. À mesurer dans NX-05 si la pagination d'un groupe
compte.

## Critère 5 : classement des hypothèses

| Hypothèse | Verdict | Réponses brutes | Portée |
|---|---|---|---|
| **H1** : `/v2/_catalog` renvoie les noms préfixés par le dépôt membre | **contredite** | `catalog-*--admin`, `catalog-hosted-root-30481--*`, `tags-hosted-api-path--admin` | Sur `3.98.1-01` Community, pour hosted, proxy et groupe, et pour toutes les formes d'URL essayées : aucun nom préfixé ; à la racine du port HTTP, pas de catalogue du tout. |
| **H2** : un compte de pull peut appeler `_catalog` | **confirmée** (compte du banc : accepté, liste complète, non filtrée) | `catalog-*--pull`, `catalog-group-connector-30482-bearer--pull`, `ctx-role-bench-docker-pull--admin` | Sur `3.98.1-01` Community, avec le compte `bench-pull` du banc. |
| H2, point séparé : filtrage silencieux par un compte à privilèges restreints | **non concluante** | `ctx-role-bench-docker-pull--admin`, `ctx-role-nx-anonymous--admin` | Les privilèges de `bench-pull` et d'anonyme couvrent tous les dépôts Docker : un filtrage ne pouvait pas apparaître. Le risque de faux « no result » n'est ni constaté ni écarté. |
| **IS-07** : plusieurs membres répondent sur le même `/v2/_catalog` d'hôte | **contredite** pour des membres adressés par hôte + chemin ; sur le groupe, un parcours suffit mais **sans attribution au membre** | `catalog-hosted-root-30481--*`, `catalog-*-path--*`, `catalog-group-connector-30482--*`, `manifest-head-*-shared-base--admin` | Sur `3.98.1-01` Community. |

**Contradictions et sections impactées** (liste des reprises possibles, rien n'est décidé ici) :

| Contradiction | Impact |
|---|---|
| H1 contredite | **IS-03** critères 2 et 5 (`repo_prefix` ; parade R1) : le filtre « garde les noms qui commencent par `<préfixe>/` » ne garde rien, et sur le port HTTP il n'y a même pas de réponse. Le catalogue d'un membre existe, par chemin, **sans** préfixe : une recherche par membre devrait appeler `/v2/<préfixe>/_catalog` (ou `/repository/<nom>/v2/_catalog`) et reconstruire `<préfixe>/<nom>` pour la référence. Aussi la mention « Non validé sur registre réel » d'IS-03 (NX-11). **NX-10** : sa condition de démarrage (« NX-03 contredit H1 ou H2 ») est remplie par H1 ; l'alternative par `/v2/<préfixe>/_catalog` existe sans l'API de recherche, à arbitrer avant de promouvoir NX-10. **NX-05** : le routage par chemin est le seul moyen d'atteindre hosted et proxy sur ce banc ; NX-05 mesure le routage. |
| IS-07 contredite | **IS-07** (un parcours par hôte puis répartition par préfixe) : pour des membres à chemin, un parcours **par membre** ; sur le connecteur d'un groupe, un parcours mais pas de répartition possible. `walks.go` (D140, clé de partage de parcours) à reprendre. |
| H2 non concluante | Pas de reprise imposée. Verdict « refused » d'IS-03 : inchangé (aucune contradiction). La mesure du filtrage demande un compte restreint (voir « Points ouverts »). |
| Faits annexes | **NX-07** : le refus du détaillé a deux formes (403, 404) ; la découverte des membres les confond avec « groupe sans membre » (V4). **NX-05** : pagination du groupe. **NX-04** : effet de cache d'un HEAD (ci-dessous). |

## Effet de bord sur le banc

Un `HEAD /v2/docker-proxy/shared/base/manifests/1.0` (avec `Accept` de manifeste) a fait **mettre en cache**
`shared/base:1.0` par `docker-proxy` : après les mesures, `GET /service/rest/v1/components?repository=docker-proxy`
liste `mirror/cached` **et** `shared/base`, et `mise run nexus-bench:status` indique `dataset: differs
(1 image)` (le descripteur dit `cachedInProxy: false` pour le `shared/base` de l'amont). C'est le **seul**
appel de manifeste adressé au proxy ; l'état était `matches` avant. Ce n'est pas une écriture demandée,
mais une conséquence d'une lecture : **un manifeste lu à travers un proxy est mis en cache** (fait mesuré),
ce qui compte pour NX-04 (« cached only »). Rien n'a été supprimé ni remis à zéro. Le fichier
`manifest-head-proxy-shared-base--admin.http` est la trace de l'appel.

Preuves versées (relevées après les mesures, sans aucun HEAD ni GET de manifeste ni `tags/list` sur le proxy) :

- `bench-status-after-measures.txt` : sortie de `mise run nexus-bench:status`, `dataset: differs (1 image)`
  (chemin absolu du descripteur remplacé par son chemin relatif, nom du contexte kube remplacé par `<kube-context>`) ;
- `rest-components-docker-proxy-after-measures--admin.http` : liste REST des composants de `docker-proxy`.
  `shared/base` 1.0 y est avec le digest `sha256:8486bd6d…` (celui de l'amont), `uploader: admin`,
  `lastModified` 2026-11-17T16:34:47 UTC, alors que `mirror/cached` date de la mise en place du banc
  (`uploader: bench-pull`, 14:51 UTC). `uploaderIp` est expurgé ;
- `catalog-proxy-path-after-measures--admin.http` : `_catalog` du proxy, lu après coup. Il liste
  `mirror/cached`, `mirror/uncached`, `shared/base`, comme avant : le catalogue d'un proxy ne montre **pas** le
  cache, c'est la liste des composants qui le montre.

L'attribution à ce HEAD est une **inférence** : c'est le seul appel de manifeste adressé au proxy, et
l'état était `matches` avant ; aucun journal de Nexus n'a été lu.

## Limites

- **Une seule version** (`3.98.1-01`), **Nexus Community**, un banc à six noms de dépôt. Pas de matrice de
  versions (décision du 2026-11-16) ; la forme de `/v1/repositories/{nom}` et le code du refus (403 ou 404)
  peuvent varier selon la version.
- **Pas de recoupe sur l'instance d'entreprise** : décision de l'utilisateur du 2026-11-17, prise en séance
  (`docs/product/nexus-support/stories.md:181` la liste encore comme question ouverte dans le checkout
  principal). Le banc seul fait donc foi : reverse proxy, LDAP, blob store S3, sous-domaines, HTTPS et
  `forceBasicAuth` ne sont pas couverts, et le banc n'a ni sous-domaine ni port dédié pour hosted et proxy.
- Accès par `http://127.0.0.1` seulement ; pas de TLS.
- Identités : un compte de pull aux privilèges larges, l'anonyme, l'admin. Aucun compte restreint (voir H2).
- Petit volume : 6 noms. Les plafonds de DevDesk (10 pages de 1000, 100 lignes) ne sont pas exercés.
- Les verdicts DevDesk cités sont des **inférences** de lecture de code (`internal/imagesearch/verdict.go`,
  `catalog.go`, `internal/oci/catalog.go`), non exécutées contre le banc.

## Points ouverts

1. **Filtrage par privilège** : créer sur le banc un rôle et un utilisateur limités à un seul dépôt Docker,
   puis rejouer `_catalog` sur le groupe et sur hosted. Écriture sur le banc : à valider avec l'utilisateur.
2. **Cache du proxy** : `shared/base:1.0` est maintenant en cache dans `docker-proxy`. Remise à l'état du
   descripteur par suppression du composant (REST `DELETE`, écriture) ou par `reset` (NX-02b, non livré) :
   à décider ; NX-04 doit partir d'un état connu.
3. **Pagination du groupe** : confirmer (NX-05) le comportement de `Link` et les doublons au-delà de `n`.
4. **Décision de conception** pour H1 : atteindre le catalogue d'un membre par `/v2/<préfixe>/_catalog`
   (reconstruire le préfixe) ou par la recherche REST (NX-10). Hors périmètre de cette note.
5. **Version du fait du 2026-08-10** : 403 contre 404 pour le détaillé du compte de pull ; la version de
   l'instance d'alors n'est pas connue.

## Index des réponses brutes (`docs/measurements/nexus-catalog/`)

Nommage : `<scénario>--<identité>.http` avec `admin`, `pull`, `anon`. Chaque fichier contient en commentaires
le scénario, la version de Nexus, la date et l'identité, puis la requête (sans secret), puis la réponse
(statut, en-têtes utiles, corps). `rest-openapi-repositories-extract.json` est un extrait de l'OpenAPI.

| Famille | Fichiers | Contenu |
|---|---|---|
| `catalog-*` | `catalog-hosted-root-30481`, `catalog-hosted-path`, `catalog-proxy-path`, `catalog-group-path`, `catalog-hosted-repository`, `catalog-proxy-repository`, `catalog-group-repository`, `catalog-group-connector-30482` (chacun en `--admin`, `--pull`, `--anon`) ; `catalog-{hosted,proxy,group}-path-on-connector-30482--admin` ; `catalog-group-connector-30482-bearer--{admin,pull,anon}` | critères 1 et 2 |
| `v2-ping-*` | `v2-ping-30481--anon`, `v2-ping-30482--anon` | défi Bearer |
| `page-*` | `page-group-n1-p0..p5`, `page-group-n2-p0..p4`, `page-hosted-n2-p0..p1`, `page-proxy-n2-p0..p1`, `page-group-n1000`, `page-group-n6` (`--admin`) | pagination |
| `rest-*` | `rest-repositories-{docker-group,group-docker-group,docker-hosted,docker-proxy,hosted-docker-hosted,proxy-docker-proxy}` en `--admin`, `--pull`, `--anon` ; `rest-openapi-repositories-extract.json` | critère 3 |
| `bench-status-*`, `*-after-measures` | `bench-status-after-measures.txt`, `catalog-proxy-path-after-measures--admin`, `rest-components-docker-proxy-after-measures--admin` | état du banc après les mesures (effet de cache) |
| `ctx-*` | `ctx-role-bench-docker-pull--admin`, `ctx-role-nx-anonymous--admin`, `ctx-anonymous-setting--admin` | privilèges des identités |
| `tags-*`, `manifest-head-*` | `tags-{group,hosted,proxy}-shared-base`, `tags-group-mirror-uncached`, `tags-hosted-api-{path,repository,v2-repository}`, `manifest-head-{group,hosted,proxy}-shared-base` (`--admin`) | IS-07 ; résolution d'un homonyme ; forme `/v2/repository/<nom>/...` (404, D39) |

**Pour NX-08** : ces fichiers portent la version de Nexus, la date et le scénario ; ils servent de
fixtures. Le script qui les a produits n'est pas versé (il lit les secrets) : le rejouer demande le banc, le
fichier de secrets et les mêmes URL (colonne « URL » des tableaux).

**Suite (DEVDESK-511, NX-08a)** : 30 de ces réponses ont été importées comme fixtures, copiées octet pour
octet dans `internal/nexusfixture/fixtures/catalog/` (les fichiers de ce dossier-ci restent la preuve
datée, inchangés). Le test `TestEveryFixtureIsMeasuredOrRecorded` vérifie qu'une fixture est identique à
son fichier d'origine ou porte `# recorded-by: nexus-bench record`. Elles sont rejouées sans le banc par
les tests de `internal/imagesearch` et `internal/registrymgr`, et `mise run nexus-bench:record` les
régénère depuis le banc (voir « Recorded fixtures » dans `docs/architecture/nexus-bench.md`).

**Suite (DEVDESK-579, NX-12)** : le point ouvert 4 est tranché : DevDesk lit le catalogue d'un membre par
`/v2/<préfixe>/_catalog` (les noms reviennent sans le préfixe, DevDesk le reconstruit). Les 12 réponses
`catalog-{hosted,proxy,group}-path--{admin,pull,anon}` et
`catalog-{hosted,proxy,group}-path-on-connector-30482--admin` sont importées comme fixtures (42 au total),
octet pour octet ; les preuves datées de ce dossier ne changent pas. Le parcours est un par membre ;
sur un groupe découvert, une seule unité de recherche (le groupe), sans attribution à un membre. Ce qui
n'est **pas mesuré** : la tolérance `?n=1000` de fakenexus sur `/v2/<dépôt>/_catalog` (hosted, proxy) est
une inférence ; le 404 du groupe par chemin avec `?n=1000` n'est pas toléré (test en `Skip`, DEVDESK-580) ;
que le 404 du groupe vienne de `pathEnabled` sur les trois dépôts ; les autres versions de Nexus, un reverse
proxy, HTTPS ; `/v2/<NomMajuscule>/_catalog` ; un compte restreint à certains dépôts (risque de faux
« no result »). Le catalogue d'un proxy liste l'amont et non son cache (mesuré) : cela contredit le libellé
`Cached only` (NX-09, hors périmètre). Voir `docs/architecture/network.md`.

**Correction** : la phrase plus haut disant que la découverte des membres « les avale toutes les deux »
(403 et 404) est inexacte. Seuls 403 et 401 sont un refus ; un 404 sur l'endpoint détaillé est un **échec de
découverte** (`unreachable`, rien en cache). Le compte de pull reçoit donc aujourd'hui un échec, non un
refus. Le comportement est figé par un test sur la réponse mesurée ; DEVDESK-578 le changera.

**Suite (DEVDESK-578)** : le 404 `Repository not found` du compte de pull sur l'endpoint détaillé est
désormais un **refus** (`denied`, `status: 404`), non plus un échec de découverte : la « Correction »
ci-dessus décrit l'état avant ce correctif. Seul un 404 dont le `message` JSON vaut « Repository not found »
est requalifié. Limite : Nexus répond le même corps pour un groupe réellement absent (voir
`docs/architecture/registries.md`).

**Suite (DEVDESK-580, PR1)** : les requêtes de la forme exacte que DevDesk envoie (`?n=1000`) ont été
enregistrées le 2026-11-20 sur le banc k0s (Nexus 3.98.1-01 Community, `status` : descriptor matches,
dataset matches avant et après), `nexus-bench record --campaign catalog --only '*-n1000--*'`, GET seulement,
pour les trois identités (admin, pull, anon), sans écriture sur le banc. Résultats, identiques pour les trois
comptes : `GET /v2/_catalog?n=1000` sur le port HTTP du hosted (30481) : **404** (page HTML), comme sans `n` ;
`GET /v2/docker-group/_catalog?n=1000` : **404**, comme sans `n` (l'hypothèse « un 404 reste un 404 avec
`?n=1000` » du plan DEVDESK-579, H-4, est donc mesurée) ; `GET /v2/docker-hosted/_catalog?n=1000` : **200**,
`api, shared/base, team/api, tools/cli`, sans `Link`, corps identique à la réponse sans `n` ; `GET
/v2/docker-proxy/_catalog?n=1000` : **200**, `mirror/cached, mirror/uncached, shared/base`, sans `Link`,
identique à la réponse sans `n`. La tolérance `?n=1000` de fakenexus est donc mesurée (non plus inférée) pour
le hosted et le proxy par chemin, et le test `TestCatalogOnTheHTTPPortRootIsRefused` ainsi que
`TestCatalogOfAGroupUnderItsPrefixIsRefused` ne sont plus en `Skip` : verdict `refused`, `StatusError{404}`.
Les 21 réponses de pagination et de listes de tags de ce dossier sont de plus importées comme fixtures
(`page-group-n1-*`, `page-group-n2-*`, `page-hosted-n2-*`, `page-proxy-n2-*`, `page-group-n6`, cinq `tags-*`).
Toujours non mesuré : la tolérance sur les chemins `/repository/<nom>/v2/` et pour les identités pull et anon
du connecteur de groupe.
