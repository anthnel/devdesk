# Docker et Podman : ce que `inspect` dit de l'image, des volumes et des réseaux d'un conteneur (OC-25, DEVDESK-823, étape 0)

Note de mesure du 2026-11-28. Aucun code produit n'a été touché. Le document donne les formes et les valeurs observées, puis
ce qu'elles permettent pour `ListContainerUsage` ; il ne fixe aucun libellé.

Convention de lecture : **Fait mesuré** = observé sur les six configurations sauf mention contraire. **Non mesuré** = dit comme tel.

## Machine et versions

| | |
|---|---|
| Machine | VM Incus de test (une par moteur), Ubuntu 24.04, 2 vCPU, lancée depuis le snapshot `clean` ; `reset.sh <moteur> <n\|n1>` avant et après chaque série |
| Docker | **N = 29.10.1**, **N-1 = 28.5.2**, rootful (client et serveur de même version) |
| Podman | **N = 6.2.1**, **N-1 = 5.8.11**, build statique tiers, **rootless** (compte `fixture-user`, uid 2000) et **rootful** |
| Image | **pas l'image des contrats** : elle n'est pas dans le snapshot `clean` et la consigne interdisait tout pull. Une image locale `ddprobe/app` (rootfs de `sleep` et de ses bibliothèques, `import`) porte deux tags, `1` et `2`, pour un seul ID. Sa forme d'ID est celle de toute image ; un `RepoDigests` n'existe pas (non mesuré pour une image tirée) |
| Ressources | tout porte le label `devdesk.test=probe-oc25` et les noms `ddc-oc25-*` ; supprimé par le reset |
| Méthode | un script dans la VM : pour chaque commande, argv, code de sortie, stdout, stderr dans des fichiers ; traité ensuite avec `jq`. Code de sortie 0 sur les 42 commandes de chaque série ; le seul stderr non vide est la progression de `podman import` (« Writing manifest... ») |

Les identifiants sont remplacés par des placeholders stables : `IMG` (ID d'image complet, 64 hex), `NET-<nom>` (ID de réseau
complet), `CTR-<nom>` (ID de conteneur complet). Le compte non privilégié est `fixture-user`. Aucune adresse ni nom d'hôte.

## Les conteneurs sondés

Tous sur l'image `ddprobe/app` (tags `1` et `2`, même ID `IMG`).

| Nom | État | Créé avec | Réseau demandé | Volume |
|---|---|---|---|---|
| run | running | tag `1` | `ddc-oc25-net` | `ddc-oc25-vol` (nommé) |
| stopped | exited | tag `1` | `ddc-oc25-net` | `ddc-oc25-vol` |
| created | created (jamais démarré) | tag `2` | `ddc-oc25-net` | `ddc-oc25-vol2` |
| none | running | tag `1` | `--network none` | aucun |
| default | running | tag `2` | aucun (défaut) | aucun |
| two | running | tag `1` | `ddc-oc25-net`, puis `network connect ddc-oc25-net2` | bind `/tmp` et volume anonyme |
| byid2 | running | l'ID complet de l'image (`sha256:<64>`) | défaut | aucun |

## (a) `inspect --format '{{json .}}' <id1> <id2>` : forme

Argv : `<moteur> inspect --format {{json .}} <id>...`, code 0, stderr vide. **Une ligne JSON par conteneur**, dans l'ordre des
arguments (docker et podman) ; deux identifiants donnent deux lignes.

| Champ | Docker 29 et 28 | Podman 6 et 5 |
|---|---|---|
| `Id` | 64 hex | 64 hex |
| `Name` | `/ddc-oc25-run` (avec `/`) | `ddc-oc25-run` (sans) |
| `Image` | `sha256:<64 hex>` (71 caractères) | `<64 hex>` (64 caractères, sans préfixe) |
| `ImageName` | absent | `localhost/ddprobe/app:1` ; voir (c) pour le piège |
| `Config.Image` | ce qui a été tapé : `ddprobe/app:1`, ou `sha256:<64>` si créé par ID | `localhost/ddprobe/app:1` (la référence normalisée, le tag **résolu en nom**) |
| `State.Status` | `running`, `exited`, `created` | idem |
| `NetworkSettings.Networks` | objet dont la **clé est le nom** du réseau | idem, mais voir (b) |
| `Networks[k].NetworkID` | ID complet du réseau (64 hex) | ID complet (64 hex) ; `none` pour le réseau `none` |
| `Mounts[].Type` | `volume`, `bind` | idem |
| `Mounts[].Name` | nom du volume ; **64 hex pour un volume anonyme** ; absent pour un bind | idem |
| `HostConfig.NetworkMode` | le nom du réseau (`ddc-oc25-net`, `bridge`, `none`) ; pour `two`, le premier | `bridge` pour tout réseau utilisateur ; `none` ; `pasta` en rootless sans réseau demandé |

Le volume de `stopped` et de `created` est rendu comme celui de `run` : un conteneur arrêté ou jamais démarré a ses `Mounts`.

## (b) Réseaux : le cas par cas

| Cas | Docker 29 et 28 | Podman rootful (6 et 5) | Podman rootless (6 et 5) |
|---|---|---|---|
| Réseau nommé, en marche ou arrêté | clé = nom, `NetworkID` = ID complet | idem | idem |
| Réseau nommé, conteneur `created` | clé = nom, **`NetworkID` vide (`""`)** | clé = nom, `NetworkID` ID complet | idem |
| `--network none` | clé `none`, `NetworkID` = ID réel du réseau `none` (de `network ls`) | clé `none`, **`NetworkID` = `"none"`** (pas un ID ; `none` n'est pas dans `network ls`) | idem |
| Aucun réseau demandé | clé `bridge`, `NetworkID` = ID du réseau `bridge` | clé `podman`, `NetworkID` = ID du réseau `podman` | **`Networks` vaut `null`**, `HostConfig.NetworkMode` = `pasta` |
| Deux réseaux | deux clés, deux `NetworkID` | idem | idem |

Le réseau par défaut de podman, `podman`, a un ID de 64 hex comme les autres (rootful) ; en rootless, un conteneur sans réseau
demandé n'a **aucune entrée** : il n'est sur aucun réseau géré, il ne peut donc rien retenir. Un `Networks` nul n'est pas une
erreur de forme ici.

## (c) Image : deux conteneurs d'une image, image à deux tags

| Fait | Docker | Podman |
|---|---|---|
| Deux conteneurs d'une même image | même `Image` (`sha256:IMG`) | même `Image` (`IMG`) |
| Image à deux tags | `image ls` : **deux lignes**, même `ID` (`sha256:<64>` complet avec `--no-trunc`), `Repository` et `Tag` distincts | `image ls` : **une ligne** par image, `Id` 64 hex sans préfixe, `names` / `Names` listent les deux tags |
| Conteneur créé avec le tag `2` | `Config.Image` = `ddprobe/app:2`, `Image` = `sha256:IMG` | `ImageName` = `localhost/ddprobe/app:2` |
| Conteneur créé avec l'**ID** de l'image | `Config.Image` = `sha256:<64>` | **`ImageName` = `localhost/ddprobe/app:2`, un des deux tags, pas celui tapé ni l'ID** : une référence ambiguë, jamais la source de la jointure |
| `image inspect --format '{{json .RepoTags}} {{.Id}}'` | tags et `sha256:<64>` | tags (`localhost/...`) et `<64>` sans préfixe |

`ps -a --filter ancestor=ddprobe/app:1 -q --no-trunc`, sur les 7 conteneurs : **docker rend 7** (le filtre résout le tag en ID :
l'image est la même), **podman rend 4** (seulement les conteneurs créés avec ce nom : ni `created` ni `default` créés avec `:2`, ni `byid2`).
Le filtre `ancestor` n'a pas la même sémantique sur les deux moteurs.

## (d) Pour vérifier la jointure

| Commande | Docker | Podman |
|---|---|---|
| `image ls --no-trunc --format '{{json .}}'`, champ ID | `"ID":"sha256:<64>"` = le `Image` de `inspect` | `"Id"` (64 hex, **pas `ID`**), la même chaîne que `Image` de `inspect` ; le modèle `{{json .}}` rend l'image entière (`Id`, `Names`, `names`, `History`...) |
| `network ls --no-trunc --format '{{json .}}'` | `{"ID":"<64>","Name":"...",...}` | **clés en minuscules** `{"id":"<64>","name":"...",...}`, rendu du JSON natif du réseau ; pas de `none` ni de `host` |
| `network inspect <nom> --format '{{json .Containers}}'` | objet `<id conteneur 64>` → `{Name, EndpointID, MacAddress, IPv4Address, IPv6Address}` ; **seulement les conteneurs en marche** (`run` et `two`, pas `stopped` ni `created`) | objet `<id conteneur 64>` → `{name, interfaces}` ; **seulement les conteneurs en marche** (2 sur 4) |
| `volume ls --format '{{json .}}'` | `Name` (volumes anonymes : 64 hex) | `Name`, `Mountpoint`, `Labels`... |
| `ps -a --filter network=<nom> -q --no-trunc` | 4 conteneurs (run, stopped, created, two) | 4 (idem) |
| `ps -a --filter volume=<nom> -q --no-trunc` | 2 (run, stopped) | 2 (idem) |

Les `NetworkID` des conteneurs, sauf ceux notés « vide » ou `none` plus haut, sont **égaux** aux ID de `network ls --no-trunc` sur les
quatre configurations (comparés sur 64 hex ou sur le préfixe de 12).

## (e) Coût : `ps -a -q --no-trunc` puis un `inspect` groupé sur 50 conteneurs

57 conteneurs au total (50 + 7), la moitié créés seulement, l'autre en marche, sur la VM de 2 vCPU, cinq passes chacune ; durées en ms.

| Configuration | `ps -a -q --no-trunc` | `inspect` groupé | Taille de la sortie |
|---|---|---|---|
| Docker 29.10.1 | 20 à 29 | 37 à 48 | 234 Ko |
| Docker 28.5.2 | 17 à 21 | 36 à 37 | 251 Ko |
| Podman 6.2.1 rootful | 38 à 41 | 70 à 72 | 355 Ko |
| Podman 6.2.1 rootless | 52 à 89 | 83 à 119 | 360 Ko |
| Podman 5.8.11 rootful | 37 à 40 | 71 à 76 | 353 Ko |
| Podman 5.8.11 rootless | 47 à 53 | 84 à 86 | 358 Ko |

Au pire, 0,2 s et 360 Ko toutes les 10 s pour 57 conteneurs : ordre de grandeur supportable. Non mesuré : plusieurs centaines de
conteneurs, une machine chargée, le client à distance.

## Ce que cela dit pour `ListContainerUsage`

**Conteneurs.** La liste complète est celle de `ps -a -q --no-trunc` (arrêtés et créés inclus) ; l'`inspect` groupé les lit tous
en une commande, une ligne par conteneur, à joindre par `Id` (jamais par position).

**Image.** Jointure sûre : `Image` de l'`inspect`, avec le préfixe `sha256:` retiré s'il est là, contre l'ID de l'image (de
`image ls --no-trunc`, même forme). Les ID courts de 12 hex (listes actuelles) se comparent par préfixe de ces 64 hex. Ne jamais
partir de `Config.Image`, de `ImageName` ou d'un tag : docker rend ce qui a été tapé (tag ou ID), podman un nom normalisé, et
`ImageName` d'un conteneur créé par ID est un tag quelconque de l'image. Deux tags d'une image donnent un même ID : le comptage
par ID les compte une fois, c'est voulu (supprimer par ID est refusé tant qu'un conteneur en dépend, cf. R4 du plan). Un
conteneur arrêté ou `created` compte comme utilisateur (même `Image`).

**Volume.** `Mounts[]` avec `Type == "volume"` et `Name` : fiable sur les six configurations, y compris arrêté et `created`. Un
volume anonyme porte un nom de 64 hex (un conteneur peut le détenir, il se classe comme les autres). Un `bind` n'a pas de nom : à
ignorer, il ne concerne aucun volume.

**Réseau.** Jointure sûre : la clé de `Networks` est le **nom** du réseau ; c'est la seule valeur présente dans tous les cas. Le
`NetworkID` est présent la plupart du temps mais manque pour un conteneur docker `created` (`""`) et vaut `none` (pas un ID)
pour le réseau `none` de podman. Donc : joindre par le nom (clé), traiter le `NetworkID` comme une vérification facultative, et ne
jamais s'y fier seul. Le nom peut se confondre entre deux réseaux seulement s'ils ont le même nom, ce que le moteur interdit.
`HostConfig.NetworkMode` ne suffit pas (`bridge` pour tout réseau utilisateur sous podman).

**Ne pas utiliser** `network inspect <réseau> --format '{{json .Containers}}'` : il ne liste que les conteneurs **en marche**
(`stopped` et `created` manquent), donc un réseau « libre » selon lui serait encore retenu. Même remarque pour
`ps --filter network=` contre `inspect` : le filtre inclut les arrêtés (4 de 4 ici) et pourrait servir de contre-épreuve,
mais il n'a pas été comparé hors de ce cas simple.

**À classer « inconnu »** (ni libre ni utilisé, la suppression garde son refus du moteur) :
- tout conteneur dont l'`inspect` ne répond pas ou dont la ligne est absente de la réponse groupée (une ligne manquante ne vaut
  jamais « inutilisé ») ;
- un `Networks` qui n'est ni un objet ni `null`, ou un `Image` vide : forme illisible, à marquer inconnue pour le réseau ou l'image,
  sans changer les volumes (cohérent avec `TestAnUnreadableNetworksShapeLeavesVolumesKnown` du plan) ;
- podman rootless : un conteneur sans réseau demandé a `Networks: null` (`pasta`), ce qui veut dire « sur aucun réseau géré »
  et donc « n'utilise aucun réseau utilisable ». C'est un fait connu, pas un inconnu, mais seulement si `NetworkMode` est
  `pasta` ou `slirp4netns` ; un `null` avec un autre mode reste inconnu ;
- un conteneur docker `created` dont `NetworkID` est vide : le nom (clé) suffit à le rattacher au réseau, donc connu ; c'est
  la valeur vide qui ne doit pas être lue comme « aucun réseau » ;
- l'image d'un conteneur par `ImageName` seul : inutilisable.

**Ambigu ou impossible sur ces mesures** : la correspondance entre une **référence** (`app:1`) et un conteneur ne peut pas se
faire sur podman par `ancestor` (4 sur 7) ; seule la jointure par ID est sûre. Un réseau dont le nom est celui d'un autre moteur
(`podman` pour docker, `bridge` pour podman) n'est pas distingué ici : un seul moteur à la fois.

## Non mesuré

- le conteneur en pause, `restarting`, `dead` ; la forme de `Image` d'une image supprimée sous un conteneur ;
- l'erreur d'un `inspect` groupé dont un identifiant a disparu entre le `ps` et l'`inspect` (course) ;
- une image tirée d'un registre (`RepoDigests`) ; `ImageName` d'une image `<none>` sous podman ;
- `network connect` sur un conteneur arrêté, `Networks` sous un pod podman, un réseau `macvlan`/`ipvlan`/`host` ;
- Docker rootless, Podman distant, Compose.
- la jointure de l'image sur une image **tirée**, multi-plateforme ou d'un magasin containerd : `Image` de l'`inspect` contre l'ID de `image ls` (l'image sondée est importée) ;
- `network rm` d'un réseau auquel est attaché un conteneur **arrêté** : le refus du moteur n'est pas mesuré, `NetworkUsers` est le seul garde-fou ;
- un `inspect` groupé d'environ 500 conteneurs : la ligne de commande Windows (32 767 caractères) est dépassée, la lecture échoue fermée ;
- un `Networks` vide `{}` avec `NetworkMode` `container:<id>`, `host`, macvlan, ipvlan, pods ;
- l'ordre des lignes de l'`inspect` groupé n'est mesuré que pour deux arguments (celui des arguments) : rien ne doit s'y fier.
