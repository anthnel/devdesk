# Spike WSL2 (S-02, DEVDESK-326) : rapport de mesure

Adresses et identifiants anonymisés.

Critères et effets : `feasibility.md` ; rapport Linux : `spike-linux.md` (même dossier `docs/product/network-map/`, ajoutés par une autre PR : à fusionner avant celle-ci).

Mesure du 2026-11-12, modes **NAT** puis **mirrored** (bascule faite par l'utilisateur). Mesures faites dans la distro WSL par l'équipe d'agents. Même prototype que S-01 (branche locale `spike/nettrace`, non publiée, commit `11cead91`). Aucun code du projet modifié.

## Environnement

| Élément | Valeur |
|---|---|
| Hôte | Windows 11 Pro |
| WSL | 2.7.13.0, noyau `6.18.33.2-microsoft-standard-WSL2` (`/proc/version` : `#1 SMP PREEMPT_DYNAMIC Thu Jun 18 21:54:43 UTC 2026`) |
| Distros | archlinux (défaut, Running, WSL2), docker-desktop (Running), une autre distribution (Stopped). Seule archlinux utilisée. |
| `%UserProfile%\.wslconfig` (mesure NAT) | absent => NAT par défaut |
| Mode réseau (lu) | `wslinfo --networking-mode` => `nat` |
| Utilisateur | `<user>`, uid 1000 ; non-root, `CapEff` / `CapPrm` = `0000000000000000`, aucun `sudo` utilisé |
| Go (compilation) | go1.27.4, `CGO_ENABLED=0`, `GOOS=linux GOARCH=amd64` |
| Cible publique | `1.1.1.1` ; cible muette `192.0.2.1` |
| Paramètres | probe 3 s, délai global 30 s, TTL max 30, 100 cycles L7 |
| `WSL_DISTRO_NAME` | `archlinux` |

Section NAT : la mesure n'a démarré ni arrêté aucune distro, pas de `wsl --shutdown` / `--terminate`, `.wslconfig` intact, Docker Desktop non touché, aucun conteneur lancé. La section mirrored (« WSL2 mirrored ») a été mesurée ensuite, après bascule du mode par l'utilisateur, puis retour en NAT.

## Mesures brutes

### Constats dans la distro (lecture seule)

```
/proc/version : Linux version 6.18.33.2-microsoft-standard-WSL2 (gcc (GCC) 13.2.0, GNU ld (GNU Binutils) 2.41) #1 SMP PREEMPT_DYNAMIC Thu Jun 18 21:54:43 UTC 2026
WSL_DISTRO_NAME=archlinux
wslinfo --networking-mode : nat          (wslinfo = /usr/sbin/wslinfo)
ip -4 route :
  default via 172.27.128.1 dev eth0 proto kernel
  172.27.x.x/20 dev eth0 proto kernel scope link src 172.27.x.x
ip -br link :
  lo    UNKNOWN <LOOPBACK,UP,LOWER_UP>
  eth0  UP      <BROADCAST,MULTICAST,UP,LOWER_UP>
ip -br addr :
  lo    UNKNOWN 127.0.0.1/8 10.255.255.254/32 ::1/128
  eth0  UP      172.27.x.x/20
/etc/resolv.conf : généré par WSL, nameserver 10.255.255.254
```

Côté Windows (lecture seule, `Get-NetIPAddress`) : `vEthernet (WSL (Hyper-V firewall))` = `172.27.128.1/20`.

### Docker vu depuis la distro

```
command -v docker      : /usr/sbin/docker
readlink -f            : /mnt/wsl/docker-desktop/cli-tools/usr/bin/docker   (lien /usr/sbin/docker -> ce chemin)
docker context ls      : default* (unix:///var/run/docker.sock) ; desktop-linux (npipe:////./pipe/dockerDesktopLinuxEngine) ;
                         <autre contexte> (npipe:////./pipe/<nom>)
docker version         : Client 29.9.2, API 1.56, go1.26.10, linux/amd64, Context default
                         "failed to connect to the docker API at unix:///var/run/docker.sock ... no such file or directory"
docker network inspect bridge : [] + même erreur de connexion
ls /var/run/docker.sock /run/docker.sock : absents ; pgrep dockerd : aucun processus dans la distro
ip link côté distro    : aucune interface docker0 ni br-*
/mnt/wsl/docker-desktop : présent
```

### Trace 1.1.1.1, 5 exécutions complètes (premier saut et résumé)

| Exéc. | Résultat | Sauts avec adresse | TTL muets | Cible au TTL | Durée | TTL 1 |
|---|---|---|---|---|---|---|
| 1 | atteinte | 8 | aucun | 8 | 21 ms | `172.27.128.1` 1.56 ms |
| 2 | atteinte | 8 | 8, 9 | 10 | 3.003 s | `172.27.128.1` 0.41 ms |
| 3 | atteinte | 8 | aucun | 8 | 33 ms | `172.27.128.1` 0.48 ms |
| 4 | atteinte | 7 | 6 | 8 | 3.003 s | `172.27.128.1` 0.59 ms |
| 5 | atteinte | 8 | 8 | 9 | 3.003 s | `172.27.128.1` 0.43 ms |
| `env -i PATH=` | atteinte | 7 | 6 | 8 | 3.003 s | `172.27.128.1` 1.17 ms |

Chemin type (exéc. 1) : 1 `172.27.128.1` ; 2 `10.x.x.1` ; 3 à 7 `a.b.x.x` ; 8 `1.1.1.1`. Le chemin réel compte 8 sauts ou plus (jusqu'à 10 TTL jusqu'à la cible quand des TTL sont muets).

Variabilité : le TTL 1 a répondu dans 6 traces principales sur 6 (5 + `PATH` vide), mais est resté **muet** dans 3 traces principales sur 3 de la relance (voir L4), et dans plusieurs traces supplémentaires de la relance (`8.8.8.8`, `9.9.9.9`). Cause non vérifiée (hypothèse : limitation de débit ICMP après plusieurs traces consécutives).

### L1 à L7 par exécution (verdicts du binaire, 5 exécutions)

| Critère | Exéc. 1 | 2 | 3 | 4 | 5 | Mesure clé |
|---|---|---|---|---|---|---|
| L1 | OK | OK | OK | OK | OK | uid 1000, CapEff 0, `setsockopt` sans erreur ; imports directs sans `os/exec` (`go list` : `context encoding/binary errors flag fmt net net/netip os runtime sort strings sync syscall time`) ; `PATH` vide : même résultat |
| L2 | OK | OK | OK | OK | OK | 8, 8, 8, 7, 8 sauts avec adresse (minimum 3) |
| L3 | OK | OK | OK | OK | OK | attente max 18.7, 18.4, 32.3, 20.3, 25.2 ms pour un délai de 3 s |
| L4 | non évalué | OK | non évalué | OK | OK | voir ci-dessous |
| L5 | OK | OK | OK | OK | OK | cible muette 3.001 à 3.003 s ; TTL max 5 : 12 à 23 ms ; délai 1.5 s : 1.501 à 1.502 s ; trois résultats distincts |
| L6 | OK | OK | OK | OK | OK | chemin réel >= 8 sauts ; 21 ms, 3.003 s, 33 ms, 3.003 s, 3.003 s (min 21 ms, médiane 3.003 s, max 3.003 s) |
| L7 | OK | OK | OK | OK | OK | 100 cycles : descripteurs 8 -> 8, goroutines 1 -> 1 ; arrêt après annulation min 123 µs, médiane 198 à 219 µs, max 331 à 354 µs |

Remarque : 8 descripteurs ici contre 7 sur Linux natif (S-01) (descripteur de plus hérité de l'environnement WSL, non investigué) ; aucune croissance.

**L4 (« saut muet »)** : définition stricte, la même que dans `spike-linux.md` : cas net = un routeur muet puis un routeur qui répond ; « muet puis cible » n'est pas un cas net. Les verdicts par exécution du tableau ci-dessus sont ceux du binaire, qui marque « NOT EVALUATED » quand aucun TTL muet n'est suivi d'un TTL qui répond et compte donc aussi « muet puis cible ». Recompte strict à partir du tableau des traces : cas net dans l'exécution 4 (TTL 6 muet, TTL 7 répond) et dans l'exécution à `PATH` vide (TTL 6 muet, TTL 7 répond) ; exécutions 2 et 5 : « muet puis cible » seulement (neutres) ; exécutions 1 et 3 : aucun TTL muet (neutres). Relance (`-cycles 5 -extra-targets 8.8.8.8,9.9.9.9,208.67.222.222`) : cas net dans les 3 exécutions terminées (1 à 3), d'après le binaire ; le détail par TTL n'est pas consigné ici, ce décompte n'est donc pas recalculé. La relance a été interrompue par un délai d'exécution pendant l'exécution 4 ; les exécutions 4 et 5 ne sont pas faites et ne sont pas exigées (décision utilisateur). Aucune exécution KO.

## Verdict W1..W5, mode NAT

| Critère | Verdict NAT | Mesure clé | Effet de l'échec (feasibility.md) |
|---|---|---|---|
| W1 | **OK** (décision utilisateur : la règle L4 de S-01 s'applique à WSL2) | L1 à L7 : aucun KO (L4 non évalué par le binaire en 1 et 3, neutre) ; L4 : cas net strict sur 1 exécution sur 5 (exéc. 4) + `PATH` vide + relance 1 à 3 (d'après le binaire, non recalculé strictement), neutre sur 4 sur 5 (exéc. 1, 2, 3, 5) | un mode en échec grise WSL2 entier (S-08) ; les deux modes en échec et Linux OK => Linux seul, écrit explicitement |
| W2 | **OK** (NAT et mirrored mesurés, voir « WSL2 mirrored ») | `wslinfo --networking-mode` = `nat` ici, `mirrored` dans l'autre mode ; interface et route par défaut aussi différentes | mode « indéterminé » : `WSL2 mode undetermined`, S-07 passe de S à M ; **WSL2 entier grisé** |
| W3 | **OK** pour NAT (preuve jugée suffisante par l'utilisateur) | TTL 1 = `172.27.128.1` = passerelle de la route par défaut (`default via 172.27.128.1 dev eth0`) = adresse de `vEthernet (WSL (Hyper-V firewall))` lue côté Windows ; TTL 1 répond 6/6 traces principales, mais reste muet dans les 3 traces principales de la relance L4 (voir « Variabilité ») | sans preuve : pas d'étiquette `Windows host (virtual switch)` |
| W4 | **Établi** (décision utilisateur) | client Docker Desktop (`/mnt/wsl/docker-desktop/cli-tools/usr/bin/docker`, pas `/mnt/c/…`) ; aucun démon ni socket dans archlinux ; intégration WSL de Docker Desktop non active pour cette distro ; le moteur est dans la VM de Docker Desktop | la colonne indique le doute plutôt que de deviner |
| W5 | **Tranché sans conteneur** (décision utilisateur) : bridge absent côté distro | pas de `docker0` ni de `br-*` dans `ip link` de la distro ; moteur injoignable depuis la distro. **Aucun conteneur n'a été lancé : le bridge d'un conteneur réel n'est pas observé.** | bridge absent : aucun bridge affiché, Docker Desktop via `Engine VM` ; présent : étape entre conteneur et hôte |

### W2 : signaux relevés en NAT (à comparer au mirrored)

`wslinfo --networking-mode` = `nat` ; `eth0` en `172.27.x.x/20` (adresse privée du switch virtuel) ; route par défaut via `172.27.128.1` ; `resolv.conf` = `10.255.255.254` (adresse également portée par `lo`, `10.255.255.254/32`). Le premier saut est une passerelle privée du bloc `172.27.x.x/20`. Les valeurs mirrored (section « WSL2 mirrored ») diffèrent.

### Conséquence d'ensemble

Voir « Verdict S-02 d'ensemble » dans la section mirrored : les deux modes sont mesurés.

## Décisions appliquées

1. **W1** : la règle L4 de S-01 s'applique à WSL2 (une exécution sans « routeur muet suivi d'un routeur qui répond » est neutre ; L4 OK si au moins une exécution l'observe et aucune n'est KO). W1 NAT = OK. Les exécutions 4 et 5 de la relance L4 ne sont pas exigées (3 exécutions sur 5 faites, interrompues par un délai d'exécution).
2. **W3** : preuve suffisante (passerelle de la route par défaut + adresse de vEthernet lue côté Windows). W3 NAT = OK.
3. **W4** : établi (client Docker Desktop, aucun démon ni socket dans archlinux, intégration WSL non active pour cette distro, moteur dans la VM de Docker Desktop).
4. **W5** : tranché sans conteneur, sur l'absence de `docker0` et de `br-*` côté distro et le moteur injoignable depuis la distro. Aucun conteneur n'a été lancé : le bridge d'un conteneur réel n'est pas observé.
5. **W2** : mirrored mesuré le 2026-11-12, après bascule par l'utilisateur (voir section suivante). Couverture de `wslinfo` jugée suffisante (1 lecture en NAT, 4 en mirrored).
6. **W3 mirrored** : le constat « pas de preuve » suffit, sans étiquette `Windows host (virtual switch)` ; pas de nouvelle mesure sans VPN.

## WSL2 mirrored

Mesure du 2026-11-12, après la section NAT, même binaire (commit 11cead91, recompilé), mêmes options, uid 1000, `CapEff` `0000000000000000`. **L'utilisateur a basculé le mode** (`%UserProfile%\.wslconfig` avec `networkingMode=mirrored`, WSL relancé) ; la mesure n'a modifié ni `.wslconfig` ni arrêté de distro. WSL est ensuite revenu en NAT. `archlinux` était `Stopped` au début de cette série : la première commande `wsl -d archlinux -- …` l'a démarrée (inévitable pour mesurer) ; docker-desktop était `Running`, l'autre distribution `Stopped`. Windows 11 Pro, WSL 2.7.13.0 : prérequis du mode (22H2+, WSL 2.0.0+) remplis.

### Constats dans la distro

```
/proc/version : identique au NAT (6.18.33.2-microsoft-standard-WSL2)
WSL_DISTRO_NAME=archlinux
wslinfo --networking-mode : mirrored   (4 lectures, toutes identiques)
ip -4 route :
  default dev eth0 proto kernel scope link metric 1          (sans passerelle)
  default via 192.168.x.x dev eth3 proto kernel metric 25
  10.x.x.0/24 et deux plages privées larges via 192.168.x.x dev eth3 ; 169.254.x.x/16 dev eth1 ; 192.168.x.x/24 dev eth3 ;
  <IP du serveur VPN> via 192.168.x.x dev eth3 ; 192.168.x.x dev eth3 (x3, métriques 25/26/50)
ip -br link : lo, eth0 UP, eth1 UP, eth2 DOWN, eth3 UP, loopback0 UP, eth4 DOWN, eth5 DOWN
ip -br addr : lo 127.0.0.1/8 + 10.255.255.254/32 ; eth0 10.x.x.2/32 ; eth1 169.254.x.x/16 ; eth3 192.168.x.x/24 ; les autres sans adresse IPv4
resolv.conf : nameserver 10.255.255.254
docker : /usr/sbin/docker -> /mnt/wsl/docker-desktop/cli-tools/usr/bin/docker (identique au NAT)
docker version : client 29.9.2 seul ; « failed to connect to the docker API at unix:///var/run/docker.sock … no such file or directory »
docker network inspect bridge : [] + même erreur ; aucun dockerd, aucun /var/run/docker.sock
bridge dans ip link : aucun docker0 ni br-*
```

Côté Windows (lecture seule, `Get-NetIPAddress`) : `VPN` = `10.x.x.2/32`, une interface Ethernet = `192.168.x.x/24`, `vEthernet (WSL (Hyper-V firewall))` = `172.27.128.1/20`, `vEthernet (Default Switch)` = `172.27.x.x/20`. Les interfaces de la distro reprennent celles de Windows (adresses identiques) : eth0 = VPN, eth3 = l'interface Ethernet.

### Trace 1.1.1.1, 5 exécutions plus `PATH` vide

| Exéc. | Résultat | Sauts avec adresse | TTL muets | Cible au TTL | Durée | TTL 1 |
|---|---|---|---|---|---|---|
| 1 | atteinte | 7 | 7 | 8 | 3.003 s | `10.x.x.1` 15.31 ms |
| 2 | atteinte | 6 | 5 | 7 | 3.003 s | `10.x.x.1` 18.79 ms |
| 3 | atteinte | 7 | 7 | 8 | 3.003 s | `10.x.x.1` 11.85 ms |
| 4 | atteinte | 6 | 5, 7 | 8 | 3.003 s | `10.x.x.1` 15.62 ms |
| 5 | atteinte | 7 | 7 | 8 | 3.003 s | `10.x.x.1` 20.54 ms |
| `env -i PATH=` | atteinte | 7 | 7 | 8 | 3.003 s | `10.x.x.1` 13.06 ms |

Chemin type (exéc. 1) : 1 `10.x.x.1` ; 2 à 6 `a.b.x.x` ; 7 muet ; 8 `1.1.1.1`. Les Time Exceeded **arrivent** en mirrored (le risque connu de S-02 ne s'est pas produit). Le saut `172.27.128.1` du NAT n'existe plus ; le premier saut (`10.x.x.1`) était le TTL 2 en NAT.

### L1 à L7 par exécution (verdicts du binaire)

| Critère | Exéc. 1 | 2 | 3 | 4 | 5 | Mesure clé |
|---|---|---|---|---|---|---|
| L1 | OK | OK | OK | OK | OK | uid 1000, CapEff 0, `setsockopt` sans erreur, `PATH` vide identique |
| L2 | OK | OK | OK | OK | OK | 7, 6, 7, 6, 7 sauts avec adresse (minimum 3) |
| L3 | OK | OK | OK | OK | OK | attente max 15.5, 19.8, 19.2, 16.1, 20.9 ms pour un délai de 3 s |
| L4 | OK | OK | OK | OK | OK | verdicts du binaire (qui compte aussi « muet puis cible »). Recompte strict : cas net dans les exécutions 2 (TTL 5 muet, TTL 6 répond) et 4 (TTL 5 muet, TTL 6 répond) ; exécutions 1, 3, 5 et `PATH` vide : « muet puis cible » seulement (neutres) |
| L5 | OK | OK | OK | OK | OK | cible muette 3.003 s ; TTL max 5 : 13 ms à 3.003 s (exéc. 2) ; délai 1.5 s : 1.501 à 1.502 s |
| L6 | OK | OK | OK | OK | OK | 3.003 s ; exécution 2 : chemin réel de 7 sauts < 8, TTL borné à 8, consigné : les 17 ms viennent de cette mesure L6 bornée, distincte de la trace principale (3.003 s) ; durées L6 : 3.003 s, 17 ms, 3.003 s, 3.003 s, 3.003 s (min 17 ms, médiane 3.003 s, max 3.003 s) |
| L7 | OK | OK | OK | OK | OK | 100 cycles : descripteurs 8 -> 8, goroutines 1 -> 1 ; arrêt après annulation min 115 µs, médiane 207 à 230 µs, max 324 à 734 µs |

### Verdict W1..W5, mode mirrored

| Critère | Verdict mirrored | Mesure clé | Effet de l'échec (feasibility.md) |
|---|---|---|---|
| W1 | **OK** | L1 à L7 OK sur 5/5 ; Time Exceeded reçus (6 à 7 sauts avec adresse) ; L4 : cas net strict sur 2 exécutions sur 5 (2 et 4), 3 neutres, aucun KO | un mode en échec grise WSL2 entier : non déclenché |
| W2 | **OK** | `wslinfo --networking-mode` : `mirrored` (4 lectures) contre `nat` en NAT ; autres signaux différents : NAT = `eth0` 172.27.x.x/20 et route par défaut via 172.27.128.1 ; mirrored = eth0 à eth5 repris de Windows (10.x.x.2/32, 192.168.x.x/24), route par défaut sans passerelle sur eth0 | échec : `WSL2 mode undetermined`, S-07 de S à M, WSL2 entier grisé : non déclenché |
| W3 | **Pas de preuve** | TTL 1 = `10.x.x.1`. La route par défaut de métrique 1 est `dev eth0` sans passerelle ; l'autre (`via 192.168.x.x dev eth3`) n'est pas le TTL 1. Côté Windows, `10.x.x.2/32` est l'interface nommée `VPN` (/32) : `10.x.x.1` est la passerelle du tunnel, pas l'hôte Windows. | mirrored sans preuve : la passerelle n'est **pas étiquetée hôte Windows** (S-07) |
| W4 | **Établi** (mêmes constats qu'en NAT, décision utilisateur) | client Docker Desktop (`/mnt/wsl/docker-desktop/cli-tools/usr/bin/docker`), aucun démon ni socket dans archlinux, moteur injoignable depuis la distro | - |
| W5 | **Tranché sans conteneur** : bridge absent côté distro | aucun `docker0` ni `br-*` dans `ip link`, moteur injoignable. **Aucun conteneur n'a été lancé : le bridge d'un conteneur réel n'est pas observé.** | bridge absent : aucun bridge affiché, Docker Desktop via `Engine VM` |

Remarque W3 : le premier saut dépend de l'interface de sortie de la route par défaut ; ici elle est portée par un VPN (interface VPN côté Windows). Sans VPN le premier saut serait autre ; non mesuré.

### Verdict S-02 d'ensemble

Règles décidées : « WSL2 supporté » seulement si les deux modes fonctionnent (W1) et que le mode est distinguable (W2). NAT : W1 OK. Mirrored : W1 OK. W2 : OK (`wslinfo --networking-mode`, présent dans WSL 2.7.13.0 ; non testé sur un WSL plus ancien). **Verdict : WSL2 supporté** (aucun mode KO ni indéterminé, donc pas de grisage de WSL2 entier). Réserves : W3 sans preuve en mirrored (pas d'étiquette `Windows host (virtual switch)`) ; W5 non observé avec un vrai conteneur ; WSL1 laissé à S-08.

## Commande exacte de reproduction

```sh
# depuis la branche locale spike/nettrace (non publiée, commit 11cead91 ; voir spike-linux.md : le code n'est pas dans le dépôt)
SHA=$(git rev-parse --short HEAD)
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-X main.commit=$SHA" -o nettrace-spike ./spike/nettrace
# copie dans la distro (utilisateur non-root), depuis Windows :
wsl -d archlinux -- cp /mnt/c/<chemin>/nettrace-spike /tmp/nettrace-spike
wsl -d archlinux -- chmod +x /tmp/nettrace-spike
wsl -d archlinux -- id                                    # uid 1000 attendu
wsl -d archlinux -- /tmp/nettrace-spike                   # x5, cible 1.1.1.1 par défaut
wsl -d archlinux -- env -i PATH= /tmp/nettrace-spike      # L1 (c)
wsl -d archlinux -- /tmp/nettrace-spike -cycles 5 -extra-targets 8.8.8.8,9.9.9.9,208.67.222.222   # relance L4 (x5, ~25 s chacune)
# constats : cat /proc/version ; echo $WSL_DISTRO_NAME ; wslinfo --networking-mode ; ip -4 route ; ip -br link ; ip -br addr ;
#            command -v docker ; readlink -f "$(command -v docker)" ; docker context ls ; docker version ; docker network inspect bridge
# côté Windows : Get-NetIPAddress -AddressFamily IPv4 | ? InterfaceAlias -match 'WSL'
wsl -d archlinux -- rm -f /tmp/nettrace-spike             # nettoyage
```

Options du binaire : `-target -unreachable -max-ttl -probe-timeout -deadline -cycles -extra-targets`. Le rapport brut du binaire imprime aussi le `PATH` de l'hôte : PATH Windows omis ici.

## Nettoyage

`/tmp/nettrace-spike` supprimé d'archlinux (vérifié absent, aucun processus `nettrace`) ; copie locale du binaire supprimée ; sorties brutes supprimées après report des mesures utiles dans ce document (idem après la série mirrored). Seule la distro archlinux a été démarrée, par la première commande de la série mirrored (elle était arrêtée). État laissé : WSL est revenu en NAT.
