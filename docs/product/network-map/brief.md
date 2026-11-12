# Chemin d'un appel réseau (network-map) : brief

> Le rapport WSL (`spike-wsl.md`) est livré par une PR distincte.

> Brief établi à partir des stories Plane DEVDESK-325 à DEVDESK-335 (S-01 à S-09), de `docs/architecture/network.md`, de `internal/netiface`, `internal/ports` et du stage `route` de `internal/netcheck`, et mis à jour après les spikes du 2026-11-12. Les arbitrages de l'utilisateur sont marqués « Décidé » ; ce qui reste ouvert est marqué **À CONFIRMER** et repris en fin de document.
>
> Convention de marquage : `Hypothèse:` = supposé non vérifié ; **À CONFIRMER** = décision qui revient à l'utilisateur ; « (story S-xx) » = repris d'une story Plane ; « (code) » = lu dans le dépôt.

## Vision

Dans la vue Network de DevDesk, un onglet « Path » montre, depuis cette machine, par où passe l'appel vers une cible : source, interface de sortie, passerelle, puis chaque saut, avec l'honnêteté d'un outil qui ne prétend jamais répondre pour une autre machine.

## Problème

- Quand un appel n'atteint pas sa cible, le dev ne sait pas si le problème est **local, dans son réseau ou plus loin** (S-09).
- Quand un VPN capture la route par défaut, les checks actuels rapportent le symptôme (pas de réponse ICMP, pas de connexion TCP) ; le stage `route` de `netcheck` nomme l'interface de sortie et la passerelle, mais **rien n'indique les sauts suivants** (code : `runRoute`, `RouteHop{Interface, Source, Gateway}`).
- Sous WSL2, le premier saut est un switch virtuel et non un routeur ; sous Docker Desktop, le chemin interne d'un conteneur n'existe pas sur la machine. Un affichage naïf induit en erreur (S-06, S-07).

## Pourquoi pas simplement rétablir l'ancien trace

`docs/architecture/network.md` (§3.47) explique que l'ancien trace a été **supprimé, pas réécrit** :

- il tournait dans un conteneur `--network host`, donc répondait pour la VM de Docker Desktop et non pour la machine (D57) ;
- sa réécriture reposait sur **trois inconnues non mesurées** : lire les ICMP `TIME_EXCEEDED` sans socket brute sur trois plateformes, un TTL par paquet que `pro-bing` n'expose pas, un mode TCP.

Ce projet reprend la première inconnue **et seulement sur Linux** (Epic A), comme un spike mesuré avant tout code produit. Il ne contredit pas la décision de §3.47 : le trace s'exécute **dans le processus DevDesk**, sans conteneur et sans binaire externe. Conséquence à documenter à la livraison : `network.md` devra dire que le trace revient, et pourquoi il répond désormais pour la bonne machine.

## Utilisateurs

- **Mainteneur de DevDesk** (Epic A) : veut savoir si le chemin d'un appel est réalisable avant d'investir (S-01, S-02).
- **Dev / DevSecOps utilisateur de DevDesk** (Epic B) : sur Linux natif ou WSL2, parfois derrière un VPN, avec ou sans Docker Engine. Le cas de l'utilisateur actuel est WSL2 + Docker Desktop (S-06, S-02 : « poste Windows de l'utilisateur »).

## Solutions existantes

| Existant | Ce qu'il couvre | Ce qu'il ne couvre pas |
|---|---|---|
| Stage `route` de `netcheck` (code) | Interface, adresse source, passerelle pour une adresse, sans privilège, ~2 ms (`go-netroute`) | Les sauts au-delà de la passerelle |
| `internal/netiface` (code) | Interfaces de **cette machine** (nom, état, MTU, MAC, adresses v4/v6, compteurs), sans conteneur | Table de routage, ARP, pare-feu (retirés, §3.44) |
| `internal/ports` (code) | Sockets locaux et processus | Hors sujet |
| Ancien trace (supprimé, §3.47) | Sauts depuis un conteneur `--network host` | Répondait pour la mauvaise machine |
| `traceroute`, `mtr`, `tracepath` | Hypothèse: ils existent sur la plupart des postes Linux | **Non étudiés ici.** Le produit exclut de toute façon de lancer un binaire externe (S-03a). |

## Périmètre MVP

Deux epics, dans cet ordre.

**Epic A, « Spike trace natif »** (module Plane du même nom)

| Story | Plane | Contenu | Livrable |
|---|---|---|---|
| S-01 | DEVDESK-325 | Mesurer L1 à L7 sur Linux natif, sur la machine Linux native (Debian 13, noyau 6.12, amd64, utilisateur non-root, accès ssh), cible de référence `1.1.1.1` | `docs/product/network-map/spike-linux.md` + binaire autonome de spike (code jetable sur la branche `spike/nettrace`, jamais fusionnée ; seuls les documents sont fusionnés) |
| S-02 | DEVDESK-326 | Mesurer W1 à W5 sur WSL2 NAT et mirrored (mesures faites dans la distro WSL par l'équipe d'agents ; l'utilisateur bascule le mode) | `docs/product/network-map/spike-wsl.md` |

**Epic B, « Onglet Path »** (conditionné par le go de l'Epic A)

| Story | Plane | Contenu | Dépend de |
|---|---|---|---|
| S-03a | DEVDESK-327 | Moteur de trace natif `internal/nettrace` | S-01 (go) |
| S-03b | DEVDESK-328 | Onglet « Path » : saisie de la cible, lancement, annulation, pré-remplissage depuis une ligne `route` de Diagnostics | S-03a |
| S-04 | DEVDESK-329 | Colonne : source, interface + adresse source, passerelle, sauts | S-03a, S-03b |
| S-05a | DEVDESK-330 | Conteneur comme source (Docker Engine) | S-04 |
| S-05b | DEVDESK-331 | Bridge affiché seulement s'il existe dans les interfaces lues sur cette machine | S-05a |
| S-06 | DEVDESK-332 | Docker Desktop : étape « VM de l'engine », boîte noire | S-05a, S-05b, S-07 |
| S-07 | DEVDESK-333 | Reconnaître WSL2, étiqueter source et premier saut | S-04, résultat de S-02 |
| S-08 | DEVDESK-334 | Plateformes ou modes non supportés : onglet grisé, message clair, rien émis | S-03a |
| S-09 | DEVDESK-335 | Montrer jusqu'où l'appel va quand il échoue (sans déduire de cause) | S-03a |

Contraintes de produit portées par les stories : sans privilège, sans binaire externe, sans conteneur ; sondes en parallèle (8 sauts en moins de 5 s) ; arrêt en 30 s au plus ; annulation sans fuite de sockets ; un saut muet est rapporté muet, jamais comme une panne ; la source est étiquetée « cette machine » ; les lectures d'engine ne démarrent aucun conteneur.

## Hors périmètre

- Intégration TUI dans le spike (Epic A) ; macOS ; Windows natif ; WSL1 (S-01, S-02).
- Mode TCP ; diagnostic automatique de la cause (S-09).
- **IPv6** : le spike est IPv4 seul (décidé), donc S-03a l'exclut (« si le spike ne le couvre pas »).
- Docker Desktop vu de l'intérieur ; Podman ; réseaux overlay ; Kubernetes ; VM nommées (S-05a, S-05b, S-06).
- Un graphe, les switchs (S-04) ; le réseau de Windows vu depuis WSL (S-07).
- Support effectif de macOS et Windows : seulement un onglet grisé (S-08).

## Plateformes

| Plateforme | Statut | Comportement |
|---|---|---|
| Linux natif | Supporté | Onglet disponible si S-01 réussit |
| WSL2, mode NAT | Supporté si W1 réussit en NAT **et** en mirrored, et si le mode est distinguable (W2). **Mesuré : supporté, avec réserves** (`spike-wsl.md`) | Sinon **WSL2 entier est grisé** (S-08, décidé) |
| WSL2, mode mirrored | Idem. **Mesuré : supporté, avec réserves** (`spike-wsl.md`) | Idem |
| macOS | Grisé | Message plateforme + raison, rien émis (S-08) |
| Windows natif | Grisé | Idem |
| WSL1 | Grisé | Idem. La détection de WSL1 est vérifiée à l'implémentation de S-08, pas par le spike (décidé) |
| Docker Desktop | Boîte noire | Étape « VM de l'engine », chemin interne non visible (S-06) |

## Règle go / no-go

1. **L'Epic A précède l'Epic B.** Aucune story de l'Epic B ne démarre avant le verdict de S-01.
2. **S-01 échoue => l'Epic B est abandonné** (S-01).
3. **Linux OK mais un mode WSL2 KO => on livre, et WSL2 entier est grisé** (S-02, S-08 ; décidé : le mode actif n'étant pas toujours distinguable, on ne grise pas un seul mode). Même règle si W2 conclut que le mode est indistinguable. **Si les deux modes échouent et que Linux natif réussit, on livre pour Linux natif seul, et `spike-wsl.md` l'écrit en toutes lettres.**
4. S-01 échoue si l'un des critères L1 à L7 échoue, avec trois exceptions décidées : L3 (plan B : socket bloquant avec délai de lecture) ; L6 (on assouplit le seuil de S-03a, pas de no-go) ; L7 (correction du prototype puis re-mesure, no-go seulement si la fuite est structurelle). L4 et L5 sont bloquants. Le détail critère par critère est dans `feasibility.md`.
5. Un spike qui ne conclut pas (mesure impossible, environnement indisponible) n'est ni un go ni un no-go : il est consigné « non évalué » avec la raison, et la décision revient à l'utilisateur.

## Hypothèses à valider

| # | Hypothèse | Source | Comment la valider |
|---|---|---|---|
| H1 | UDP + `IP_RECVERR` (stdlib ou `x/sys`) suffit sans privilège | S-01 | L1, L2 |
| H2 | Le netpoller Go réveille sur la file d'erreurs ; plan B : socket bloquant avec délai de lecture | S-01 | L3 |
| H3 | Passer `x/sys` en dépendance directe est acceptable | S-01 | **Décidé : oui.** Aujourd'hui `golang.org/x/sys v0.49.0 // indirect` dans `go.mod` (code) ; le passage se fait avec le code produit de S-03a, avec Rule 303 (tidy/verify) et documentation |
| H4 | Le comportement de S-01 se retrouve en WSL2 NAT ; en mirrored les Time Exceeded peuvent ne jamais arriver | S-02 | W1. **Levée** : les Time Exceeded arrivent en mirrored comme en NAT (`spike-wsl.md`) |
| H5 | S-02 donne une détection fiable du mode WSL2 | S-07 | W2. **Confirmée** : `wslinfo --networking-mode` rend `nat` ou `mirrored` (`spike-wsl.md`, WSL 2.7.13.0 ; non testé sur un WSL plus ancien) |
| H6 | Le stage `route` fournit interface et passerelle | S-04 | Déjà confirmé par la lecture du code (`Env.Route` rend `RouteHop`) ; reste à choisir comment le réutiliser depuis l'onglet |
| H7 | L'onglet se branche comme les autres tabs de `netdiag` ; l'affichage passe par un `datatable` (sinon S devient M) | S-03b, S-04 | Revue de `internal/ui/netdiag` à l'architecte |
| H8 | Les infos d'un conteneur (réseau, IP, passerelle) se lisent par l'inspection d'engine existante | S-05a | Non vérifié ici. `internal/docker/networks.go` expose `InspectNetwork` ; l'inspection de conteneur reste à confirmer |
| H9 | `internal/netiface` suffit pour tester l'existence du bridge | S-05b | W5 (côté WSL) ; test unitaire avec fausses interfaces |
| H10 | L'absence du bridge sur la machine est le bon signal d'un engine distant ou en VM | S-06 | W4, W5 |
| H11 | WSL1 se détecte de façon fiable | S-08 | Vérifiée à l'implémentation de S-08, aucun constat dans S-02 (décidé) |
| H12 | Un saut muet est le plus souvent un filtrage ICMP | S-09 | Information seulement : l'UI n'en déduit aucune cause |

## Libellés d'interface

Les stories décrivent les textes en français. La Rule 129 impose l'anglais US dans l'UI. Proposition de formulation (à valider à l'implémentation, pas encore une décision) :

| Story | Libellé proposé |
|---|---|
| « cette machine » (S-04) | `this machine` |
| « WSL2 (VM), pas l'hôte Windows » (S-07) | `WSL2 (VM), not the Windows host` |
| « hôte Windows (switch virtuel) » (S-07) | `Windows host (virtual switch)` |
| « mode WSL2 indéterminé » (S-07) | `WSL2 mode undetermined` |
| « VM de l'engine » / « chemin interne non visible » (S-06) | `Engine VM` / `internal path not visible` |
| « pas de réponse au-delà » (S-09) | `no response beyond` |
| muet (S-03a, S-09) | `silent` (le mot « muet » ne désigne pas une panne) |
| « indéterminé » (S-05a) | `undetermined` |

## Métriques de succès

- **Epic A** : les deux rapports (`spike-linux.md`, `spike-wsl.md`) existent, chaque critère L1 à L7 et W1 à W5 y a un verdict avec sa mesure, et le go / no-go est tranché par la règle ci-dessus. Mesurable sans ambiguïté.
- **Epic B** : **aucune métrique d'usage n'est définie dans les stories.** À CONFIRMER (voir Questions ouvertes). Les critères d'acceptation des stories servent de seule définition de « fait » en attendant.

## Risques

| Risque | Effet | Parade |
|---|---|---|
| Time Exceeded jamais reçus en WSL2 mirrored (S-02) | WSL2 grisé en entier (décidé) | Règle go/no-go (3), W1. Ne s'est pas produit : ils arrivent (`spike-wsl.md`) |
| Cible qui filtre l'UDP : jamais reconnue comme atteinte | Trace qui va jusqu'au TTL max ou à 30 s | L5 exige un résultat explicite ; S-09 n'en déduit aucune cause |
| Routeurs qui limitent leurs ICMP : faux sauts muets, plus probable avec des sondes parallèles | Colonne trompeuse | Un saut muet est affiché « silent », jamais comme une panne (S-09) ; mesurer en L4 et L6 |
| Mode WSL2 non distinguable alors qu'un mode échoue | On ne sait pas lequel griser | Décidé : WSL2 entier est grisé |
| Les deux modes WSL2 échouent | Le poste WSL2 de l'utilisateur n'est pas couvert | Décidé : on livre pour Linux natif seul, et `spike-wsl.md` l'écrit |
| Le prototype est jetable (branche `spike/nettrace` jamais fusionnée) | Le code mesuré disparaît | Le rapport consigne le hash du commit et les mesures ; seuls les documents sont fusionnés |
| Bridge d'un réseau Docker défini par l'utilisateur au nom non standard | Bridge jugé absent à tort | Hypothèse: le nom du bridge doit être lu dans l'inspection du réseau, pas déduit. À examiner à l'architecte avant S-05b |
| Le rapport collable contient des adresses de sauts (privées et publiques) | Fuite involontaire en collant | Le binaire de spike le dit en en-tête du rapport (S-01 : rapport collable tel quel) |
| Émettre des paquets vers une cible choisie par l'utilisateur depuis une TUI | Surprise réseau | S-08 : rien n'est émis sur une plateforme non supportée ; l'envoi ne part que sur action explicite (S-03b) |
| `network.md` dit aujourd'hui que le trace est supprimé | Doc contradictoire à la livraison | Mettre à jour `docs/architecture/network.md` dans le même commit que S-03a |

## Questions ouvertes

Les décisions de l'utilisateur du 2026-11-12 sont reprises dans `feasibility.md` (section « Décisions de l'utilisateur ») et marquées « Décidé » dans les deux documents. Il reste **À CONFIRMER** :

1. Raccourcis de lancement et d'annulation de l'onglet (`enter` / `esc`, S-03b : à confirmer avec le keymap) et de pré-remplissage depuis Diagnostics.
2. Libellés UI en anglais US (tableau ci-dessus).
3. Métriques de succès de l'Epic B.
4. Un réglage utilisateur du TTL maximum dans l'Epic B (`traceroute_max_hops` a été supprimé en §3.47) : non tranché, le spike fixe 30.

Tranchés par le spike : le signal du mode WSL2 est `wslinfo --networking-mode` (W2) ; le client Docker Desktop vu depuis la distro est `/mnt/wsl/docker-desktop/cli-tools/usr/bin/docker`, pas `/mnt/c/…` (W4). Voir `spike-wsl.md`.
