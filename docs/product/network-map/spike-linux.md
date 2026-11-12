# Spike Linux natif (S-01, DEVDESK-325, tâche DEVDESK-385) : rapport de mesure

Adresses et identifiants anonymisés.

Prototype jetable, branche locale `spike/nettrace` (non publiée, jamais fusionnée), commit `11cead91`, code dans `spike/nettrace/main.go` (stdlib seule, `syscall`, aucune dépendance `x/sys`, IPv4 seul). Un socket UDP par TTL (1 à 30), toutes les sondes partent en parallèle, `IP_TTL` + `IP_RECVERR` posés dans `net.Dialer.Control`, lecture de la file d'erreurs par `RawConn.Read` (netpoller) avec `recvmsg(MSG_ERRQUEUE|MSG_DONTWAIT)`, annulation par `context`.

## Environnement (lu sur la machine par le binaire)

| Élément | Valeur |
|---|---|
| Machine | Linux native (S-01), utilisateur `<user>` (non-root, aucun `sudo`, aucun `setcap`) |
| OS | Debian GNU/Linux 13 (trixie), linux/amd64 |
| Noyau | 6.12.x (Debian 13) |
| Go (compilation) | go1.27.4 (`CGO_ENABLED=0`, binaire ELF statique, vérifié par `file`) |
| uid / CapEff | 1000 / `0000000000000000` |
| Cible publique | `1.1.1.1` (paramètre `-target`) ; cible muette `192.0.2.1` (paramètre `-unreachable`) |
| Durée de sonde | 3 s (`-probe-timeout`), délai global 30 s, TTL max 30 |
| Exécutions | 5 exécutions complètes, plus 1 exécution à `PATH` vide, plus une relance de 5 exécutions vers plusieurs cibles (voir L4) |

## Commande exacte de reproduction

```sh
git checkout spike/nettrace   # commit 11cead91
SHA=$(git rev-parse --short HEAD)
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-X main.commit=$SHA" -o nettrace ./spike/nettrace
scp nettrace <hôte>:<dossier temporaire>/
ssh <hôte> 'cd <dossier temporaire> && ./nettrace'                 # x5
ssh <hôte> 'cd <dossier temporaire> && env -i PATH= ./nettrace'    # L1 (c)
# le binaire trace 1.1.1.1 ; options : -target -unreachable -max-ttl -probe-timeout -deadline -cycles -extra-targets
```

La branche `spike/nettrace` est un code jetable local, non publié : cette commande décrit la procédure, le code n'est pas dans le dépôt. Ce rapport est la trace durable du prototype.

Le binaire a ensuite été supprimé de la machine Linux native (dossier temporaire retiré). Rien d'autre n'y a été installé ni modifié.

## Mesures brutes

### Chemin vers 1.1.1.1 (exécution 1, 11 sauts)

TTL 1 `192.168.x.x` 3.34 ms ; 2 `192.168.x.x` 3.03 ms ; 3 `10.x.x.x` 14.93 ms ; 4 `10.x.x.x` 15.69 ms ; 5 `a.b.x.x` 16.68 ms ; 6 `a.b.x.x` 16.84 ms ; 7 muet ; 8 `a.b.x.x` 19.30 ms ; 9 `a.b.x.x` 23.72 ms ; 10 `a.b.x.x` 28.37 ms ; 11 `1.1.1.1` 19.10 ms (cible atteinte, Port Unreachable).

### Tableau par exécution

| Exécution | Résultat du trace principal | Sauts avec adresse | TTL muets | Cible atteinte au TTL | Durée |
|---|---|---|---|---|---|
| 1 | cible atteinte | 10 | 7 | 11 | 3.001 s |
| 2 | cible atteinte | 7 | 7 à 22 | 23 | 3.001 s |
| 3 | cible non atteinte, TTL maximum | 6 | 7 à 30 | - | 3.003 s |
| 4 | cible atteinte | 7 | 7 à 15 | 16 | 3.003 s |
| 5 | cible non atteinte, TTL maximum | 6 | 7 à 30 | - | 3.004 s |
| `PATH` vide | cible atteinte | 9 | 1, 7, 11 à 19 | 20 | 3.001 s |

Constat non prévu : le nombre de sauts qui répondent varie d'une exécution à l'autre (6 à 10), et le TTL de la cible atteinte varie (11, 23, 16, 20, ou jamais). Hypothèse (non vérifiée) : limitation du débit des ICMP par des routeurs ou par la cible face à 30 sondes simultanées. Conséquence : le « TTL le plus bas qui atteint la cible » n'est pas le nombre réel de sauts du chemin (11 dans l'exécution 1).

### L1 : sans privilège, sans binaire externe, sans conteneur

| Mesure | Résultat |
|---|---|
| uid, CapEff (6 exécutions) | 1000, `0000000000000000` |
| `setsockopt` `IP_TTL` / `IP_RECVERR` | aucune erreur (`EPERM` jamais vu), 6 exécutions |
| imports directs (`GOOS=linux go list -f '{{.Imports}}' ./spike/nettrace`) | `context encoding/binary errors flag fmt net net/netip os runtime sort strings sync syscall time` : pas d'`os/exec` |
| `PATH` vide (`env -i PATH=`) | même résultat que `PATH` normal, tous verdicts OK |
| conteneur | aucun démarré, aucun moteur utilisé |

### L2 : au moins 3 sauts avec adresse, TTL croissants

Sauts avec adresse : 10, 7, 6, 7, 6 (exécutions 1 à 5). Ordre croissant de TTL : vrai dans les 5. Minimum 6 >= 3.

### L3 : netpoller

Lecture par `RawConn.Read`, aucun `time.Sleep` ni boucle d'attente dans le code de lecture. Attente maximale d'une sonde qui a reçu une réponse, pour un délai de lecture de 3 s : 28.4, 30.9, 75.4, 107.2, 131.3 ms (exécutions 1 à 5). Note : la « latence » et l'attente sont la même variable ; le seul test indépendant du verdict est `latence max < délai / 2`. Le verdict repose donc sur la lecture du code (attente par le netpoller) plus l'absence d'atteinte du délai : aucune réponse n'a attendu le délai. Les attentes de 75 à 131 ms (supérieures à ce que les autres sauts suggèrent) ne sont pas expliquées. Plan B non nécessaire.

### L4 : saut muet

Définition stricte, adoptée dans tous les rapports. Deux cas à distinguer : (a) « routeur muet puis routeur qui répond » (cas net) et (b) « muet puis cible » (les TTL muets entre le dernier routeur et la cible atteinte sont des sondes qui ont atteint la cible, pas des routeurs : exécutions 2 et 4, non comptées).

Première série (vers `1.1.1.1`) : cas net dans l'exécution 1 (TTL 7 muet, TTL 8 répond) et dans l'exécution à `PATH` vide (TTL 1 muet, TTL 2 répond), soit **1 exécution nette sur 5 plus `PATH` vide** (recompte à partir du tableau par exécution). Exécutions 2 et 4 : cas (b) seulement. Exécutions 3 et 5 : aucun TTL ne répond après un muet.

Relance (5 exécutions, `-cycles 5 -extra-targets 8.8.8.8,9.9.9.9,208.67.222.222`, même procédure, binaire supprimé ensuite) : vers `1.1.1.1`, cas net dans les 5 exécutions (TTL 7 muet puis 8 répond ; TTL 1 muet puis 2 répond en plus dans 2 d'entre elles). Vers `8.8.8.8` : TTL 7 muet puis 8 répond dans 3 exécutions sur 5 (aucun muet dans les 2 autres). Vers `9.9.9.9` : cas net dans les 5 (TTL 7 muet puis 8 répond ; 10 muet puis 11 répond ; l'exécution 4 n'a que le TTL 10 muet puis 11 répond). `208.67.222.222` : non atteinte, non exploitée. Aucune exécution KO. Cumul : cas net observé dans 7 exécutions sur 11 vers `1.1.1.1` (exéc. 1, `PATH` vide, relance 1 à 5), plus les cibles `8.8.8.8` et `9.9.9.9`.

### L5 : arrêt en 30 s au plus, résultat explicite

| Cas | Résultat explicite | Durée (exéc. 1 à 5) |
|---|---|---|
| cible muette `192.0.2.1`, TTL max 30 | cible non atteinte, TTL maximum | 3.003 / 3.002 / 3.001 / 3.003 / 3.003 s |
| `1.1.1.1` avec TTL max abaissé à 5 | cible non atteinte, TTL maximum | 19 / 22 / 22 / 28 / 33 ms |
| `192.0.2.1`, délai global 1.5 s < durée de sonde 20 s | cible non atteinte, délai écoulé | 1.501 / 1.501 / 1.502 / 1.501 / 1.501 s |
| `1.1.1.1` atteinte | cible atteinte | voir tableau du trace principal |

Les trois résultats sont distincts dans les cinq exécutions, et tous les arrêts sont très inférieurs à 30 s. **Écart** : aucune cible dont le chemin dépasse réellement 30 sauts n'a été trouvée ; le cas « TTL maximum » est mesuré avec un TTL max abaissé à 5 et avec la cible muette.

### L6 : sondes en parallèle, moins de 5 s

| Exécution | Base de mesure | Durée |
|---|---|---|
| 1 | chemin réel présumé (cible atteinte au TTL 11 >= 8) | 3.001 s |
| 2 | chemin réel présumé (cible atteinte au TTL 23) | 3.001 s |
| 3 | cible non atteinte, TTL borné à 8 (consigné) | 3.001 s |
| 4 | chemin réel présumé (cible atteinte au TTL 16) | 3.003 s |
| 5 | cible non atteinte, TTL borné à 8 (consigné) | 3.002 s |

Min / médiane / max : 3.001 / 3.001 / 3.003 s. Durée de sonde choisie : 3 s. Parallélisme : les exécutions 2 à 5 ont au moins 2 TTL muets (jusqu'à 24 TTL muets dans les exécutions 3 et 5) résolus ensemble en 3 s ; des délais en série dépasseraient largement 5 s. Un seul TTL muet (exécution 1) ne le prouve pas à lui seul. Limite : le test ne vérifie que la durée, pas la complétude du résultat (exécutions 3 et 5 : 6 sauts sur 8 répondent). La marge de 5 s dépend de la durée de sonde de 3 s.

### L7 : annulation sans fuite

100 cycles par exécution (annulation 150 ms après le début, vers la cible muette ; au moins 22 sondes non résolues à chaque annulation) : descripteurs 7 -> 7, goroutines 1 -> 1, dans les 5 exécutions. Les « maximums » (7 descripteurs, 1 goroutine) sont échantillonnés **après** chaque trace, pas pendant : ce n'est pas un pic (un trace ouvre environ 30 sockets). Les 2 goroutines vues dans l'exécution à `PATH` vide sont la goroutine d'annulation de la mesure, pas une fuite (1 après). Délai d'arrêt après annulation : min 108 à 119 µs, médiane 206 à 226 µs, max 367 à 723 µs. Aucune fuite, aucune correction nécessaire.

## Verdict par critère

| Critère | Verdict | Mesure clé | Effet de l'échec (feasibility.md) |
|---|---|---|---|
| L1 | OK | uid 1000, CapEff 0, `setsockopt` sans erreur, pas d'`os/exec`, `PATH` vide identique | Epic B abandonné |
| L2 | OK | 6 à 10 sauts avec adresse (5/5), minimum requis 3 | Epic B abandonné |
| L3 | OK (netpoller, lecture du code + délai jamais atteint) | attente max 131 ms pour un délai de 3 s | plan B, pas de no-go |
| L4 | OK (décision utilisateur : exécution sans cas net neutre) | cas net : exéc. 1 (TTL 7 → 8), `PATH` vide (TTL 1 → 2), 5/5 de la relance ; aucun KO | Epic B abandonné |
| L5 | OK (5/5) | arrêt en 3.0 s, trois résultats distincts | Epic B abandonné |
| L6 | OK (5/5, durée seulement) | 3.001 à 3.003 s ; N >= 8 sur 3 exéc., TTL borné à 8 sur 2 | seuil de S-03a assoupli, pas de no-go |
| L7 | OK (5/5) | 100 cycles, fd 7 -> 7, goroutines 1 -> 1 | correction puis re-mesure ; no-go si fuite structurelle |

## Verdict global

**Fonctionne** : aucun critère en échec, le trace natif sans privilège fonctionne sur Linux natif (Debian 13, noyau 6.12).

## Décisions de l'utilisateur (interprétations)

- **L4** : une exécution sans « routeur muet puis routeur qui répond » est neutre ; L4 est OK si au moins une exécution observe le cas et qu'aucune n'est KO. « Muet puis cible » n'est pas un cas net.
- **L6** : cible atteinte à un TTL N >= 8 => N est retenu comme chemin réel présumé (N n'est pas le nombre réel de sauts, voir « Constat non prévu ») ; cible non atteinte => TTL borné à 8, consigné.
- **L5** : un TTL maximum abaissé à 5 plus la cible muette `192.0.2.1` suffisent pour le cas « TTL maximum ».

## Notes et limites de la mesure

- Variabilité : le nombre de sauts qui répondent varie d'une exécution à l'autre (hypothèse de limitation de débit ICMP avec 30 sondes simultanées, non vérifiée). À étudier pour S-03a (étaler les envois ?).
- L2 : « TTL croissants » est tautologique (les sauts sont rendus dans l'ordre des TTL) ; seul le minimum de 3 sauts avec adresse est réellement mesuré.
- L4 : « le trace ne s'arrête pas à un saut muet » est vrai par construction (sondes parallèles, un TTL ne dépend pas du précédent).
- L1 : le test à `PATH` vide n'est pas discriminant (le binaire n'exécute rien) ; l'import vérifié (pas d'`os/exec`) fait foi. « Aucun `sudo` » n'est pas mesuré : seuls uid et CapEff le sont.
- L5 : l'état « délai écoulé » exige une sonde de 20 s et un délai de 1,5 s ; le plafond de 30 s n'est jamais approché (arrêts à environ 3 s).
- L7 : ne couvre pas l'annulation pendant `Dial` ou `Write` (l'annulation à 150 ms tombe quand les sondes attendent en lecture).
- Course connue dans `spike/nettrace/main.go` (lignes 322 à 330, variable `cancelledAt` écrite par la goroutine d'annulation) : à corriger si le code est repris pour S-03a.
