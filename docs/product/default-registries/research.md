# Registres par défaut à la création d'un contexte : recherche documentaire

Date de consultation de toutes les sources : 2026-11-21.

Convention : **[Fait]** = lu dans la source citée ; **[Observé]** = constaté par une requête HTTP directe via l'outil de lecture web (ne voit ni statut ni en-têtes de façon fiable, voir limites) ; **[Inférence]** = déduction de l'auteur ; **[Pas de donnée]** = rien trouvé.

## Question de recherche

À la création d'un contexte DevDesk, préconfigurer Docker Hub (`docker.io`) pour qu'on puisse chercher et puller une image sans avoir configuré de registre. MVP : Docker Hub seul. Ensuite, éventuellement d'autres registres publics anonymes.

## Synthèse (5 points)

1. **Précédent solide pour un « Hub implicite »** : `docker pull` utilise Docker Hub par défaut ; Podman Desktop livre Docker Hub, Quay, GitHub et GCR préconfigurés ; Portainer livre « Docker Hub (anonymous) » visible par tous. Aucun de ces précédents ne demande de configuration préalable pour le Hub.
2. **Supprimable : précédent partiel.** Podman Desktop permet de retirer un registre (étapes génériques, rien ne dit que les préconfigurés font exception). Portainer ne permet que de le *masquer*, et l'accès anonyme reste actif côté Docker. Docker Hub ne se « supprime » pas dans la CLI Docker.
3. **Docker Hub anonyme (dernier état documenté)** : 100 pulls / 6 h par IPv4 ou IPv6 /64 ; 200 pour un compte Personal ; illimité en payant. Le durcissement annoncé en février 2025 (10 pulls/h) n'a pas été appliqué ; Docker promet 6 mois de préavis. La recherche `hub.docker.com/v2/search/repositories` a répondu sans authentification lors du test, mais aucune limite anonyme chiffrée n'est documentée.
4. **Hors MVP** : le pull anonyme est possible partout (avec jeton Bearer anonyme sur la plupart), mais le *parcours* dans un TUI diverge fortement : `/v2/_catalog` inutilisable ou vide sur la plupart des registres (seul mcr.microsoft.com a renvoyé un catalogue complet lors du test). La recherche est une API propriétaire par registre, pas un standard.
5. **Preuve directe de la friction : faible.** Je n'ai trouvé aucun retour d'utilisateur ni issue disant « je ne peux pas puller tant que je n'ai pas configuré un registre » dans un outil de type TUI/GUI. Seule la friction voisine de Podman (erreur de nom court sans registre de recherche) est documentée.

## 1. Comportement par défaut d'outils comparables

| Outil | Registre par défaut | Supprimable / modifiable | Source et statut |
|---|---|---|---|
| Docker CLI / Engine | Docker Hub : « By default, `docker pull` pulls images from Docker Hub » | Pas de liste de registres à éditer ; on qualifie le nom d'image | [Fait] https://docs.docker.com/reference/cli/docker/image/pull/ |
| Podman (`registries.conf`) | Aucun par défaut dans le format ; liste `unqualified-search-registries` fournie par la distribution. Si absente : erreur « short name and no search registries are defined » | Oui, fichier système ou `$HOME/.config/containers/registries.conf` | [Fait] https://github.com/containers/image/blob/main/docs/containers-registries.conf.5.md (la doc ne donne pas de défaut) ; erreur : [Fait] https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/10/html/building_running_and_managing_containers/working-with-container-registries |
| Podman selon distribution | RHEL 8/10 (exemple de doc) : `registry.access.redhat.com`, `registry.redhat.io`, `docker.io`. Fedora 33 (blog Red Hat) : `registry.fedoraproject.org`, `registry.access.redhat.com`, `registry.centos.org`, `docker.io`. `quay.io` n'apparaît que dans des exemples, pas comme défaut livré. | Oui | [Fait, secondaire] résultats de recherche vers https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/10/deploy-configuring_container_registries et https://www.redhat.com/fr/blog/manage-container-registries. **Pas de donnée** pour Debian/Ubuntu/Arch ni pour les versions Fedora actuelles. |
| Podman (alias courts) | Fichier `shortnames.conf` du projet containers : alias nom court vers nom complet (Fedora, Ubuntu, Debian, UBI…) | n/a | [Fait] https://github.com/containers/shortnames/blob/main/shortnames.conf |
| Podman Desktop | Préconfiguré : Docker Hub, Red Hat Quay, GitHub, Google Container Registry (« To ease usage of the most popular container registries, Podman Desktop has pre-configured registries ») | Suppression décrite pour tout registre (Settings > Registries > Remove) ; la page ne dit pas si les préconfigurés sont exclus | [Fait] https://podman-desktop.io/docs/containers/registries |
| Rancher Desktop | Registre par défaut = Docker Hub (`docker.io`, organisation `library`) pour la résolution des noms (page « Allowed images ») ; pas de liste de registres préconfigurée trouvée | Miroirs via provisioning (`registries.yaml`) | [Fait] résumé de recherche vers https://docs.rancherdesktop.io/ui/preferences/container-engine/allowed-images/ ; **pas de donnée** sur une liste de registres dans l'UI |
| Portainer | « Docker Hub (anonymous) » intégré, visible de tous | Masquable seulement ; l'accès anonyme intégré à Docker continue de fonctionner, l'entrée réapparaît pour qui n'a aucun autre registre. Le masquage serait réservé à l'édition Business (source tierce, non vérifiée) | [Fait, secondaire] https://docs.portainer.io/portainer-documentation/sts/admin/registries (via résumé de recherche, page non relue directement) |
| Harbor | Aucun point de terminaison préconfiguré trouvé ; Docker Hub est un fournisseur proposé quand on crée un point de terminaison de réplication, à la main | Création manuelle | [Fait] https://goharbor.io/docs/main/administration/configuring-replication/create-replication-endpoints/ ; état d'une installation neuve : **pas de donnée** confirmée |
| Artifactory | Pas de donnée trouvée (non recherché en profondeur) | n/a | **Pas de donnée** |
| k9s, lazydocker, dive, ctop | Non investigués dans cette recherche : aucune source lue. | n/a | **Pas de donnée** |

**Précédent « un registre public préconfiguré, supprimable » :**
- [Fait] Podman Desktop : registres publics préconfigurés ET opération Remove disponible (exception éventuelle des préconfigurés non documentée).
- [Fait] Portainer : préconfiguré, mais masquable uniquement.
- [Inférence] Le précédent Podman Desktop est le plus proche de l'idée ; le schéma « préconfiguré mais retirable » est donc réaliste, mais seul Docker Hub y est la valeur sûre.

## 2. Docker Hub en accès anonyme (état 2026-11-21)

- [Fait] Non authentifié : **100 pulls par 6 h**, par adresse IPv4 ou sous-réseau IPv6 /64. Personal : 200. Pro/Team/Business : illimité. Source : https://docs.docker.com/docker-hub/usage/pulls/ (la page ne porte aucune date ou « dernière mise à jour »).
- [Fait] Un pull = vérification de version + téléchargement ; la vérification seule ne compte pas ; une image multi-arch compte un pull par architecture. Dépassement : HTTP 429, « You have reached your pull rate limit… ». Même source.
- [Fait] Vérification du quota possible par HEAD (ne compte pas) sur `registry-1.docker.io/v2/ratelimitpreview/test/manifests/latest` avec jeton anonyme obtenu auprès de `auth.docker.io` ; en-têtes `ratelimit-limit`, `ratelimit-remaining`, `docker-ratelimit-source`. Même source.
- [Fait] Limite « abuse » séparée, tous accès Hub (pages, API, pulls), par IP, « de l'ordre de milliers de requêtes par minute », identique pour tous les niveaux. Source : https://docs.docker.com/docker-hub/usage/
- [Fait] Annonce du 21 février 2025 : 10 pulls/h pour les non authentifiés ; mise à jour du 8 avril 2025 : changements prévus au 1er avril 2025 **non appliqués**, limites actuelles maintenues, tout durcissement futur annoncé **au moins 6 mois à l'avance**. Source : https://www.docker.com/blog/revisiting-docker-hub-policies-prioritizing-developer-experience/ (l'outil de lecture signale que la page contient aussi du contenu daté 2026, non détaillé ; **à relire par un humain** pour confirmer qu'aucune annonce plus récente n'existe). Une recherche de « 2026 » n'a trouvé aucune annonce de changement.
- [Fait] API Hub : limitée « par minute », chiffre non donné ; en-têtes `X-RateLimit-*`, `Retry-After` sur 429 ; « Most Docker Hub API endpoints require you to authenticate ». La page ne mentionne pas la recherche ni ne précise les exceptions. Source : https://docs.docker.com/reference/api/hub/latest/
- [Observé] `GET https://hub.docker.com/v2/search/repositories?query=nginx&page_size=2` a renvoyé un JSON valide (`count` 296619) sans erreur d'authentification. L'outil ne montre pas les en-têtes de la requête ; son caractère anonyme est probable mais non prouvé. Point d'API non documenté dans la référence officielle (voir ci-dessus) : [Inférence] risque de stabilité contractuelle.
- [Pas de donnée] Limite chiffrée anonyme de la recherche ou du listing de tags Hub. Ce qui exige un login : pas de liste officielle trouvée ; privé/push évidemment.
- [Inférence] Pour DevDesk, ce sont les **pulls** qui comptent contre 100/6 h (par IP, donc partagé derrière un NAT/proxy d'entreprise) ; la recherche et les tags relèvent de la limite d'abus, bien plus haute.

## 3. Registres candidats (hors MVP)

Limites de méthode : l'outil de lecture web ne montre pas les en-têtes ni toujours le statut. Un « 401 » signifie qu'une requête non authentifiée à cet URL est refusée ; il ne dit pas si un jeton anonyme (Bearer) ouvre l'accès. Les constats « Observé » sont à rejouer avec `curl -i` avant toute décision.

| Registre | Pull anonyme | `/v2/_catalog` | API de recherche | Jeton Bearer anonyme |
|---|---|---|---|---|
| ghcr.io | Oui pour les paquets publics (visibilité privée par défaut à la création, à rendre public) ; un identifiant périmé peut provoquer un refus (`docker logout ghcr.io`) | [Observé] 401 | Pas de donnée trouvée | [Fait] `GET /v2/` renvoie 401 avec `Bearer realm="https://ghcr.io/token"` et un scope par dépôt ; le jeton est donc par dépôt (pas de scope catalogue). Sources : https://www.github.com/orgs/community/discussions/25426, https://github.com/orgs/community/discussions/50487, https://issues.redhat.com/browse/PROJQUAY-9012 |
| quay.io | Oui ; Red Hat : pas de restriction des pulls anonymes, limitation seulement en cas sévère (dizaines de requêtes/s par IP) ; 429 par IP et par endpoint, OCI et API Quay. Sources : https://access.redhat.com/articles/5531191, https://access.redhat.com/solutions/6218921 | [Observé] `{"repositories":[]}` : répond mais vide, donc inutilisable pour parcourir | [Observé] `GET /api/v1/find/repositories?query=nginx` a renvoyé des résultats publics (JSON paginé, `has_additional`), anonymat non prouvé. La doc lue concerne Quay auto-hébergé, pas quay.io | Pas de donnée |
| registry.k8s.io | Oui (images publiques du projet Kubernetes) | [Observé] 404 | Aucune trouvée | [Observé] `/v2/pause/tags/list` répond 307 vers `europe-west1-docker.pkg.dev/v2/k8s-artifacts-prod/images/...` (redirection vers Artifact Registry) ; jeton : pas de donnée. Aucune source documentaire lue |
| mcr.microsoft.com | Oui | [Observé] catalogue complet renvoyé (JSON ~158 000 caractères, Azure/AKS/Arc…) sans auth visible | Pas de donnée (une UI web de découverte existe, selon https://github.com/microsoft/containerregistry) | Pas de donnée ; [Observé] `/v2/dotnet/runtime/tags/list` répond directement (très long, ~310 000 caractères) |
| gcr.io | Pull anonyme des images publiques : pas confirmé par une source pour `gcr.io` (la doc lue concerne `pkg.dev`). Les URL `gcr.io` sont désormais servies par Artifact Registry, source : https://docs.cloud.google.com/artifact-registry/docs/upgrade | [Observé] 401 | Pas de donnée | Pas de donnée |
| public.ecr.aws | Oui (un pull anonyme est possible, limites par IP plus basses, sans chiffre officiel dans les résultats lus ; le chiffre « 10/s, 500 Go/mois » vient d'un blog tiers non vérifié : https://zenn.dev/bicstone/articles/amazon-ecr-public-gallery?locale=en) | [Observé] 401 (`/v2/_catalog` et `/v2/`) | Pas de donnée (une galerie web existe, API non trouvée) | Pas de donnée ; [Observé] `/v2/` répond 401, ce qui est compatible avec un flux Bearer, non démontré. |

- [Inférence] Pour « chercher » dans un TUI : un **catalogue générique `_catalog` n'est exploitable que sur mcr.microsoft.com** parmi les six (test unique, à rejouer). Les autres exigeraient soit une API de recherche propre au registre (Quay `find/repositories`, Hub `search/repositories`), soit une saisie directe du nom d'image avec liste des tags via `/v2/<nom>/tags/list`.
- [Inférence] Le jeton Bearer anonyme est le mécanisme standard du flux d'authentification Docker v2 (confirmé explicitement ici pour ghcr.io et Docker Hub) ; un client qui gère déjà Hub gère probablement les autres, mais cela n'a pas été testé.

## 4. Preuves sur la friction « configurer un registre avant de puller »

**Pour :**
- [Fait] Podman : erreur « image name provided is a short name and no search registries are defined in the registries config file » quand la liste est vide ; remède = éditer `registries.conf` ou qualifier le nom. Sources : https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/10/html/building_running_and_managing_containers/working-with-container-registries, https://bugzilla.redhat.com/show_bug.cgi?id=1701922, https://forum.linuxfoundation.org/discussion/comment/35067/ (un cas « podman pull alpine fails » dans un lab de formation). C'est une friction réelle, mais de nom court, pas d'écran de configuration d'outil.
- [Fait] Choix de conception des outils graphiques : Podman Desktop et Portainer préinstallent des registres « pour faciliter l'usage » (citation Podman Desktop). [Inférence] cela suggère que les éditeurs jugent la configuration préalable indésirable ; ce n'est pas une mesure de la friction.

**Contre / absence :**
- [Pas de donnée] Aucune issue ou discussion trouvée où un utilisateur d'un outil comparable se plaint de devoir ajouter un registre avant de puller. Pas de données d'usage sur DevDesk dans cette recherche.
- [Fait] Un utilisateur de Docker CLI n'a jamais cette friction : Hub est implicite. [Inférence] l'attente de départ des utilisateurs Docker est donc « ça marche sans configuration ».
- Le dépôt DevDesk n'a pas été lu pour cette recherche : le comportement actuel d'un contexte vierge n'est pas établi ici.

## Risques à noter pour la décision

- [Fait] Limite anonyme partagée par IP ou /64 IPv6 : en entreprise derrière un proxy, le quota est mutualisé. [Inférence] le TUI devrait afficher le quota restant (HEAD gratuit sur `ratelimitpreview/test`) et proposer le login.
- [Fait] Podman : une liste de recherche non qualifiée a un risque de squat de noms (mentionné par la doc Red Hat). Préconfigurer plusieurs registres publics comme registres de recherche par défaut aggraverait ce point ; sur ce sujet, un seul registre par défaut (MVP) est cohérent. [Inférence]

## Ce que ça confirme, nuance, laisse inconnu (5 lignes)

1. Confirme : le Hub implicite est la norme (Docker CLI, Rancher Desktop, Podman Desktop, Portainer) ; la préconfiguration du seul Docker Hub sans authentification est fonctionnelle (100 pulls/6 h par IP, recherche Hub répondant sans login lors d'un test).
2. Nuance : aucune preuve directe de friction chez des utilisateurs d'outils comparables ; seule la friction Podman « nom court sans registre » est documentée ; le « supprimable » n'est démontré que pour Podman Desktop (et pas explicitement pour les préconfigurés).
3. Nuance : le quota anonyme est par IP/6 h et mutualisé derrière un NAT ; une annonce de durcissement (10/h) a été repoussée sans date, avec préavis de 6 mois promis.
4. Nuance hors MVP : seuls les pulls sont uniformes ; `/v2/_catalog` n'a été observé complet que chez mcr.microsoft.com (vide chez quay.io, 401 ghcr.io/gcr.io/public.ecr.aws, 404 registry.k8s.io) ; la recherche passe par des API propres.
5. Inconnu : limite chiffrée anonyme des API de recherche/tags Hub ; endpoints Hub de recherche non documentés officiellement ; défauts Podman actuels par distribution ; k9s/lazydocker/dive/ctop/Artifactory ; en-têtes et jetons anonymes réels des six registres (à rejouer avec `curl -i`).

## Sources (consultées le 2026-11-21)

- https://docs.docker.com/docker-hub/usage/
- https://docs.docker.com/docker-hub/usage/pulls/
- https://docs.docker.com/reference/api/hub/latest/
- https://docs.docker.com/reference/cli/docker/image/pull/
- https://www.docker.com/blog/revisiting-docker-hub-policies-prioritizing-developer-experience/
- https://hub.docker.com/v2/search/repositories?query=nginx&page_size=2 (requête de test)
- https://github.com/containers/image/blob/main/docs/containers-registries.conf.5.md
- https://github.com/containers/shortnames/blob/main/shortnames.conf
- https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/10/html/building_running_and_managing_containers/working-with-container-registries
- https://www.redhat.com/fr/blog/manage-container-registries
- https://bugzilla.redhat.com/show_bug.cgi?id=1701922
- https://forum.linuxfoundation.org/discussion/comment/35067/
- https://podman-desktop.io/docs/containers/registries
- https://docs.rancherdesktop.io/ui/preferences/container-engine/allowed-images/
- https://docs.portainer.io/portainer-documentation/sts/admin/registries
- https://goharbor.io/docs/main/administration/configuring-replication/create-replication-endpoints/
- https://www.github.com/orgs/community/discussions/25426
- https://github.com/orgs/community/discussions/50487
- https://issues.redhat.com/browse/PROJQUAY-9012
- https://access.redhat.com/articles/5531191
- https://access.redhat.com/solutions/6218921
- https://docs.cloud.google.com/artifact-registry/docs/upgrade
- https://github.com/microsoft/containerregistry
- https://zenn.dev/bicstone/articles/amazon-ecr-public-gallery?locale=en (tiers, non vérifié)
- Requêtes directes de test : https://ghcr.io/v2/_catalog, https://quay.io/v2/_catalog, https://quay.io/api/v1/find/repositories?query=nginx, https://registry.k8s.io/v2/_catalog, https://registry.k8s.io/v2/pause/tags/list, https://mcr.microsoft.com/v2/_catalog, https://mcr.microsoft.com/v2/dotnet/runtime/tags/list, https://gcr.io/v2/_catalog, https://public.ecr.aws/v2/_catalog, https://public.ecr.aws/v2/

Note de méthode : plusieurs entrées reposent sur des résumés de recherche (pages non relues intégralement : Portainer, Harbor, Rancher Desktop, Red Hat, GitHub Discussions) ; les requêtes « Observé » ont été faites via un outil de lecture web qui n'expose ni statut ni en-têtes de manière fiable.
