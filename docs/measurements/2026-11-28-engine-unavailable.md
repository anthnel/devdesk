# Docker et Podman quand le moteur est absent, arrêté ou refuse l'accès : relevé des réponses (OC-24, DEVDESK-822, étape 0)

Note de mesure du 2026-11-28. Aucun code produit n'a été touché et aucun libellé de l'interface n'est proposé ici : ce document
donne les textes et les codes de sortie, pas la classification qui en sera tirée.

Convention de lecture : **Fait mesuré** = observé, le texte est reproduit tel quel. **Non mesuré** = dit comme tel.

## Machine et versions

| | |
|---|---|
| Machine | VM Incus de test (projet des VM jetables, une par moteur), Ubuntu 24.04, noyau 6.8, cgroup v2, lancée depuis le snapshot `clean` |
| Docker | **N = 29.10.1**, **N-1 = 28.5.2**, Docker Engine rootful, mesuré en root ; `docker version` donne la même version côté client et côté serveur |
| Podman | **N = 6.2.1**, **N-1 = 5.8.11**, build statique tiers, mesuré **rootless** (compte `fixture-user`, uid 2000, linger actif) et **rootful** (root) |
| Image | `docker.io/library/alpine@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf` (celle des contrats, seulement pour P5) |
| Verbes sondés | `ps --all --format <gabarit à 7 champs de l'étape 0>`, `image ls --format {{.ID}}`, `volume ls --format {{.ID}}`, `network ls --format {{.ID}}`, `inspect <64 × a>`, `network inspect bridge` |
| Méthode | un script shell dans la VM ; pour chaque commande, argv, code de sortie, tailles et contenus de stdout et stderr (par `cat -A`, donc à l'octet près) ; `reset.sh <moteur> <n\|n1>` avant et après chaque série |

Le nom du compte non privilégié est remplacé par `fixture-user`, aucun nom d'hôte ni adresse privée n'apparaît ; l'adresse
`127.0.0.1` est la boucle locale. Les horodatages `time="..."` de Podman sont remplacés par `<time>`. Chaque ligne des tableaux
vaut pour les six verbes **sauf mention contraire** (section « Ce qui change selon le verbe »).

## Textes relevés

Chaque texte est le contenu intégral de stderr, un retour à la ligne final inclus. `<ver>` est la version du client.

**Docker**

| Id | Texte | Versions |
|---|---|---|
| DK-A | `Cannot connect to the Docker daemon at <endpoint>. Is the docker daemon running?` | 29 et 28 |
| DK-B | `failed to connect to the docker API at <endpoint>; check if the path is correct and if the daemon is running: dial unix <chemin>: connect: no such file or directory` | 29 seulement |
| DK-C | `permission denied while trying to connect to the docker API at unix:///var/run/docker.sock` | 29 seulement |
| DK-D | `permission denied while trying to connect to the Docker daemon socket at unix:///var/run/docker.sock: Get "http://%2Fvar%2Frun%2Fdocker.sock/v1.51/<route>": dial unix /var/run/docker.sock: connect: permission denied` ; `<route>` vaut `containers/json?all=1` pour `ps`, `_ping` (méthode `Head`, pas `Get`) pour `image ls`, `volumes`, `networks`, `containers/<id>/json`, `networks/bridge` selon le verbe | 28 seulement |

`<endpoint>` est `unix:///var/run/docker.sock`, `unix:///tmp/<fichier>.sock` ou `tcp://127.0.0.1:1` selon le cas.

**Podman**

| Id | Texte (deux lignes) | Versions |
|---|---|---|
| PM-A | `Cannot connect to Podman. Please verify your connection to the Linux system using `podman system connection list`, or try `podman machine init` and `podman machine start` to manage a new Linux VM`<br>`Error: unable to connect to Podman socket: Get "http://d/v6.2.1/libpod/_ping": dial unix <chemin>: connect: <cause>` | 6.2.1 |
| PM-B | les mêmes deux lignes, avec `v5.8.11` et la seconde ligne terminée par `: unix://<chemin>` en plus (`...connect: <cause>: unix://<chemin>`) | 5.8.11 |
| PM-C | `error opening namespace handles: Permission denied` | 6.2.1 |
| PM-D | `Failed to obtain podman configuration: mkdir /root/libpod: permission denied` | 5.8.11 |
| PM-E | `Error: creating runtime static files directory "/home/fixture-user/.local/share/containers/storage/libpod": mkdir /home/fixture-user/.local/share/containers/storage/libpod: permission denied` | 6.2.1 et 5.8.11 |
| PM-F | `time="<time>" level=error msg="cannot find UID/GID for user fixture-user: no subuid ranges found for user \"fixture-user\" in /etc/subuid - check rootless mode in man pages."`<br>`time="<time>" level=warning msg="Using rootless single mapping into the namespace. This might break some images. Check /etc/subuid and /etc/subgid for adding sub*ids if not using a network user"` | 6.2.1 et 5.8.11 |

Le code de sortie de Podman pour une erreur de commande est **125** ; PM-C et PM-D sortent en **1**.
`<cause>` vaut `no such file or directory` (pas de fichier socket), `connection refused` (fichier présent, rien n'écoute) ou
`permission denied` (fichier présent, accès refusé).

## Docker (rootful), 29.10.1 et 28.5.2

| Cas | Mise en place | Code | stderr 29.10.1 | stderr 28.5.2 |
|---|---|---|---|---|
| D0 binaire absent | `PATH` sans le moteur | 127 | voir ci-dessous | voir ci-dessous |
| D1 démon arrêté, fichier socket resté | `systemctl stop docker.socket docker.service`, puis les verbes en root | 1 | DK-A, `unix:///var/run/docker.sock` | DK-A, idem |
| D1b démon arrêté, fichier socket supprimé | D1 puis `rm /var/run/docker.sock /run/docker.sock` | 1 | DK-B, `unix:///var/run/docker.sock` | DK-A, `unix:///var/run/docker.sock` |
| D2 socket lié sans `listen` | `DOCKER_HOST=unix:///tmp/probe-stale.sock`, socket créé par `bind` seul | 1 | DK-A, `unix:///tmp/probe-stale.sock` | DK-A, idem |
| D2c chemin de socket absent | `DOCKER_HOST=unix:///tmp/none.sock` | 1 | DK-B, `unix:///tmp/none.sock` | DK-A, `unix:///tmp/none.sock` |
| D3 activation par socket | `docker.service` arrêté, `docker.socket` actif, puis `docker ps` | **0** | aucune sortie | aucune sortie |
| D4 permission | utilisateur hors du groupe `docker`, démon actif | 1 | DK-C | DK-D |
| D5 TCP refusé | `DOCKER_HOST=tcp://127.0.0.1:1` | 1 | DK-A, `tcp://127.0.0.1:1` | DK-A, idem |

D0, texte exact (identique en 29 et 28, la VM n'ayant pas d'autre client) :
`env: ‘docker’: No such file or directory` (code 127, par `env`), `bash: line 1: docker: command not found` (code 127, par bash).
Sur ce poste, `exec.LookPath("docker")` avec un `PATH` sans le moteur rend
`exec: "docker": executable file not found in $PATH`, et `errors.Is(err, exec.ErrNotFound)` est vrai (Go 1.27.4). Un client lancé
après un `LookPath` réussi n'atteint donc jamais ces textes de shell : le cas « binaire absent » se tranche avant l'exécution.

Faits propres à Docker :

- D1 : après `systemctl stop docker.socket docker.service`, `/var/run/docker.sock` et `/run/docker.sock` **subsistent** (type
  `s`, propriétaire root, groupe `docker`, mode `rw-rw----`) : le fichier reste, plus rien n'écoute.
- D3 : `systemctl stop docker.service` avertit `Stopping 'docker.service', but its triggering units are still active:
  docker.socket`. Après `docker ps` (code 0, sortie vide, aucune erreur), `docker.service` est de nouveau `active`.
- Pour `inspect` et `network inspect`, un échec de connexion écrit en plus `[]` sur **stdout** (3 octets : `[]` et un retour à la
  ligne), avec le même code 1 et le même stderr que les autres verbes. Les autres verbes ont un stdout vide.

## Podman, 6.2.1 et 5.8.11

Le client est ici soit local (il ouvre lui-même le stockage), soit distant (`--remote`, ou `CONTAINER_HOST` / `--url`).

| Cas | Mise en place | Code | stderr 6.2.1 | stderr 5.8.11 |
|---|---|---|---|---|
| P0 binaire absent | `PATH` sans le moteur | 127 | comme D0 avec `podman` | comme D0 avec `podman` |
| P1 local rootless, sans service | `podman ps ...` ; `podman.socket` et `podman.service` utilisateur inactifs | **0** | vide | vide |
| P1r local rootful, sans service | idem en root | **0** | vide | vide |
| P2a distant rootless, aucun socket | `podman --remote ...`, service utilisateur arrêté, `/run/user/2000/podman` absent | 125 | PM-A, `/run/user/2000/podman/podman.sock`, `no such file or directory` | PM-B, idem |
| P2b `CONTAINER_HOST` seul | `CONTAINER_HOST=unix:///run/user/2000/podman/podman.sock`, sans `--remote` | 125 | comme P2a | comme P2a |
| P2c distant rootful, aucun socket | `podman --remote ...` en root | 125 | PM-A, `/run/podman/podman.sock`, `no such file or directory` | PM-B, idem |
| P2d chemin de socket quelconque | `--remote --url unix:///tmp/none.sock` | 125 | PM-A, `/tmp/none.sock`, `no such file or directory` | PM-B, idem |
| P2e socket démarré puis arrêté | `systemctl --user start` puis `stop podman.socket podman.service`, fichier resté | 125 | PM-A, `connection refused` | PM-B, idem |
| P2f activation par socket (rootless) | `podman.socket` actif, `podman.service` arrêté, puis `podman --remote ps` | **0** | vide | vide |
| P3 permission, socket rootful | `systemctl start podman.socket` (root, `root:root`, `rw-rw----`) ; en `fixture-user` : `podman --remote --url unix:///run/podman/podman.sock ...` | 125 | PM-A, `permission denied` | PM-B, idem |
| P3b socket rootful actif, en root | `podman --remote ps` | **0** | vide | vide |
| P3c socket rootful arrêté, fichier resté | `systemctl stop podman.socket podman.service`, en root | 125 | PM-A, `connection refused` | PM-B, idem |
| P4a stockage `chmod 000`, propriétaire `fixture-user` | `podman ps ...` | **0** | vide | vide |
| P4b sans `XDG_RUNTIME_DIR` | `env -u XDG_RUNTIME_DIR podman ps ...` | **0** | vide | vide |
| P4c `XDG_RUNTIME_DIR=/root` | `podman ps ...` en `fixture-user` | 1 | PM-C | PM-D |
| P4d stockage `root:root` mode 000, processus de l'utilisateur arrêtés | `podman ps ...` | 125 | PM-E | PM-E |
| P5 `subuid` absent, VM neuve | lignes `fixture-user` retirées de `/etc/subuid` et `/etc/subgid`, `podman ps ...` | **0** | PM-F | PM-F |
| P5 `run` après `pull`, même configuration | `podman run --rm --pull=never <image> true` | **0** | 10 lignes `level=warning msg="Additional gid=<n> is not present in the user namespace, skip setting it"` (gid 1, 2, 3, 4, 6, 10, 11, 20, 26, 27, ...) | idem |

Faits propres à Podman :

- P1 : `podman ps` local réussit sans aucun service, en rootless comme en rootful : il n'y a pas de cas « démon arrêté » en local.
- P2b : `CONTAINER_HOST` seul **suffit** à passer en client distant, sans `--remote`.
- P2a, P2c, P2d, P2e, P3, P3c : la même première ligne `Cannot connect to Podman. Please verify your connection to the Linux system
  ...` accompagne les trois causes ; seule la fin de la seconde ligne (`no such file or directory`, `connection refused`,
  `permission denied`) les distingue.
- P2e et P3c : le fichier socket **subsiste** après l'arrêt de l'unité (`srw-rw----`, propriétaire de l'unité) : la cause est
  `connection refused`, pas `no such file or directory`.
- P4a et P4b ne produisent aucune erreur : un `chmod 000` sur un stockage que l'utilisateur possède est sans effet, car le client
  rootless s'exécute en root dans son espace de noms utilisateur ; l'absence de `XDG_RUNTIME_DIR` est tolérée. Seul un stockage
  appartenant à un autre uid (P4d) refuse l'accès.
- P5 : `podman ps` et `podman run` aboutissent avec le seul mapping simple ; la VM neuve (« cold ») émet PM-F sur chaque verbe
  qui ouvre le stockage la première fois ; une fois le stockage créé (« after »), stderr redevient vide. Le premier verbe de la
  série porte le texte, pas forcément `ps`. Le `reset` entre séries rend la mesure reproductible.
- `network inspect bridge` échoue même quand le moteur répond : Podman n'a pas de réseau `bridge` par défaut (`podman`)
  et sort 125 avec `Error: network bridge: unable to find network with name or ID bridge: network not found` ; `inspect` d'un
  identifiant inconnu sort 125 avec `Error: no such object: "<id>"`, les deux avec `[]` sur stdout.

## Ce qui change selon le verbe

Rien, dans tous les cas où le moteur est injoignable : même code de sortie, même stderr (à la route près pour Docker 28, DK-D).
Les deux seules différences sont `[]` sur stdout pour `inspect` et `network inspect` (échec de connexion comprise), et la route
dans le texte DK-D de Docker 28.5.2. Quand le moteur répond, `inspect` d'un identifiant inconnu et `network inspect bridge`
sortent en erreur (Podman : 125, voir ci-dessus) : ce sont des erreurs de commande, pas d'indisponibilité.

## Ce que cela dit pour la classification

1. **Docker, motif stable** : en 29 comme en 28, un démon arrêté dont le socket est resté (D1, D2) et un `tcp://` refusé (D5)
   s'écrivent `Cannot connect to the Docker daemon at <endpoint>. Is the docker daemon running?` (DK-A), code 1. C'est le seul
   motif commun aux deux versions pour « démon injoignable ».
2. **Docker, le texte change entre 29 et 28 pour le même cas** : socket absent (D1b, D2c) donne DK-B en 29 (`failed to connect to
   the docker API at ...`) mais DK-A en 28 ; l'accès refusé (D4) donne DK-C en 29 et DK-D en 28. Aucune chaîne n'est commune
   aux deux pour ces cas : seule la fin `permission denied` de DK-D, et la proximité `permission denied while trying to connect`
   de DK-C et de DK-D, le sont.
3. **Docker, un même texte pour plusieurs causes** : DK-A sert « démon arrêté », « socket qui n'écoute pas » et « TCP refusé »
   (et, en 28, « socket absent »). Rien dans le texte ne dit si le démon est arrêté ou le socket mal placé ; seul `<endpoint>`
   varie.
4. **Docker, cas sans erreur** : l'activation par socket (D3) fait démarrer le démon, la commande réussit, code 0, sortie vide ;
   un moteur « arrêté » mais activable n'est pas détectable par une erreur.
5. **Podman, motif stable** : en 6.2.1 et 5.8.11, le client distant écrit toujours `Cannot connect to Podman. ...` puis
   `Error: unable to connect to Podman socket: Get "http://d/v<ver>/libpod/_ping": dial unix <chemin>: connect: <cause>`, code
   125 ; la version est dans l'URL, et 5.8.11 ajoute `: unix://<chemin>` en fin de ligne. La fin `: connect: <cause>` est la seule
   partie qui sépare « pas de socket » (`no such file or directory`), « rien n'écoute » (`connection refused`) et « accès refusé »
   (`permission denied`).
6. **Podman, local sans démon** : `podman ps` local aboutit sans service (P1, P1r) ; ni « arrêté » ni « socket absent » n'existent
   tant que le client n'est pas distant (`--remote`, `--url` ou `CONTAINER_HOST`, qui suffit seul, P2b). Une machine Podman
   arrêtée sous macOS ou Windows n'a pas été mesurée ici (Non mesuré) ; P2 en est seulement l'analogue.
7. **Podman, accès refusé local** : un `chmod 000` du stockage de l'utilisateur ne produit rien (P4a) ; les vrais échecs locaux
   sont `error opening namespace handles: Permission denied` (6.2.1) contre `Failed to obtain podman configuration: mkdir
   /root/libpod: permission denied` (5.8.11), code 1 pour une racine d'exécution inaccessible (P4c), et PM-E, code 125, pour un
   stockage inaccessible (P4d). Les deux textes de P4c n'ont rien en commun ; `permission denied` en fin de ligne les rapproche.
8. **Podman, subuid absent** (P5) ne bloque rien : `ps` et `run` aboutissent (code 0) avec des lignes `level=error` et
   `level=warning` sur stderr. Un classificateur qui lirait stderr sans regarder le code de sortie en ferait une erreur à tort.
9. **Binaire absent** : un `PATH` sans le moteur donne 127 et un texte de shell (`command not found`,
   `No such file or directory`) ; `exec.LookPath` rend `exec: "<moteur>": executable file not found in $PATH`, reconnaissable par
   `errors.Is(err, exec.ErrNotFound)` sans lire de texte.
10. **Code de sortie** : Docker 1, Podman 125 (distant et erreurs de commande) ou 1 (P4c) ; les codes ne séparent pas les causes,
    ils séparent seulement les moteurs.

## Ce qui n'a pas pu être mesuré

- Machine Podman arrêtée sur macOS ou Windows (`podman machine`) : hors des VM de test, P2 en est l'analogue.
- Docker rootless, Docker Desktop, contextes Docker (`docker context`) : hors du périmètre de l'étape 0.
- `podman run` du cas P5 sur une VM neuve sans image préalable : l'image a été tirée avant, avec les `subuid` présents.
- Les versions mesurées sont celles de `tools/engine-vms/versions.env` au 2026-11-28 ; ces textes sont du texte des clients et
  peuvent changer à la prochaine montée de version.

## État des VM

`reset.sh <moteur> <n|n1>` a été lancé avant chaque série et la dernière commande de la mesure est un `reset.sh` sur la ligne
`n` ; les deux VM sont dans l'état du snapshot `clean`, ligne N.
