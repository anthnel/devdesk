# Chemin d'un appel réseau : faisabilité et critères binaires du spike

> Le rapport WSL (`spike-wsl.md`) est livré par une PR distincte.

> Critères binaires L1 à L7 et W1 à W5, lecture des critères d'acceptation de S-01, S-02 et S-03a (stories DEVDESK-325 à DEVDESK-335, `docs/architecture/network.md`, `internal/netiface`, `internal/ports`, stage `route` de `internal/netcheck`) ; ce n'est pas une estimation de l'architecte. Ce qui n'est pas déductible est marqué **À CONFIRMER**.
>
> Chaque critère donne : un **énoncé binaire** (vrai ou faux, pas « à peu près »), une **méthode de mesure**, et l'**effet de l'échec**, fixé avant de mesurer. Tous les effets sont **Décidés** : soit par une story ou la règle go/no-go du brief, soit par l'utilisateur (arbitrage du 2026-11-12, repris ci-dessous).

## Verdict de faisabilité

**Avant le spike : plausible, à mesurer.** Sur Linux, un socket UDP non privilégié avec `IP_TTL` et `IP_RECVERR` reçoit, dans sa file d'erreurs, les ICMP Time Exceeded avec l'adresse du routeur : c'était l'hypothèse H1 des stories, tenue pour non acquise.

**Après le spike.** Linux natif (Debian 13, noyau 6.12) : **fonctionne**, L1 à L7 OK (`spike-linux.md`). WSL2 en NAT et en mirrored : **supporté, avec réserves**, W1 OK dans les deux modes, mode distinguable (W2) ; réserves : pas de preuve que le premier saut est l'hôte Windows en mirrored (W3), pas de conteneur réel observé pour W5 (`spike-wsl.md`). Les mesures WSL2 ont été faites dans la distro WSL par l'équipe d'agents ; l'utilisateur a basculé le mode.

Déjà acquis par la lecture du code :

- le stage `route` donne interface, adresse source et passerelle sans privilège, ~2 ms (`Env.Route` -> `RouteHop{Interface, Source, Gateway}`, `go-netroute`) ;
- `internal/netiface.List` donne les interfaces de **cette machine** sans conteneur ; il suffit à tester l'existence d'un bridge par son nom ;
- WSL se reconnaît déjà par `WSL_DISTRO_NAME` dans `internal/browser`, `internal/clipboard` et `internal/ui/terminal` (même signal) ; `/proc/version` est cité par S-02 comme second signal ; le mode (NAT ou mirrored) se lit par `wslinfo --networking-mode` (constat, `spike-wsl.md`, W2) ;
- `golang.org/x/sys v0.49.0` est dans `go.mod` en `// indirect` ; il passe en dépendance directe (décidé, voir conditions de mesure).

## Conditions de mesure communes (S-01, S-02)

- Le prototype est **un binaire autonome**, lançable sur un autre poste (S-01) : `CGO_ENABLED=0`, `file` / `ldd` le disent statique ; son rapport est **collable tel quel** (S-01).
- Le rapport donne : OS, version du noyau, version de Go, mode réseau (pour WSL), la cible, le détail de chaque mesure, et pour chaque critère « OK / KO » avec l'effet décidé ici.
- Le prototype est lancé en **utilisateur sans privilège** (UID différent de 0, `CapEff` vide, lu dans `/proc/self/status` et consigné par le binaire).
- La cible publique est un **paramètre** du binaire, jamais codé en dur, et consignée dans le rapport. **Cible de référence : `1.1.1.1`. Décidé.**
- Nombre d'exécutions par critère : **5**, tous doivent passer. Cycles de répétition pour L7 : **100**. **Décidé.**
- TTL maximum du spike : **30**. **Décidé.** (Le réglage `traceroute_max_hops` a été supprimé avec l'ancien trace, `network.md` §3.47 ; le spike le fixe en dur, et la question d'un réglage utilisateur relève de l'Epic B.)
- **IPv4 uniquement. Décidé.** IPv6 est donc hors S-03a (« si le spike ne le couvre pas »).
- **`golang.org/x/sys` passe en dépendance directe. Décidé.** Il est aujourd'hui `// indirect` dans `go.mod` (v0.49.0) ; le passage se fait dans le code produit de S-03a (`go mod tidy` + `go mod verify`, Rule 303, et documentation).
- **Lieu de mesure de S-01 : la machine Linux native** (Debian 13, noyau 6.12, amd64), utilisateur non-root, accès ssh. **Décidé.** Le rapport consigne ces valeurs lues sur la machine, pas copiées d'ici. Les mesures WSL2 (S-02) sont faites dans la distro WSL2 du poste Windows, par l'équipe d'agents ; l'utilisateur bascule le mode.
- **Prototype : code jetable sur la branche `spike/nettrace`, jamais fusionnée. Décidé.** Seuls les documents sont fusionnés, dont `docs/product/network-map/spike-linux.md` (S-01) et `spike-wsl.md` (S-02). Le rapport du binaire est donc la trace durable du prototype : il consigne le hash du commit de la branche.
- Le prototype ne démarre aucun conteneur ; si un critère exige un conteneur (W4, W5), il est lancé à la main, en dehors du prototype.

## Règle d'effet par défaut

L'échec d'un critère L signifie « S-01 ne fonctionne pas » => **Epic B abandonné** (S-01), sauf si la ligne du critère prévoit un plan B, un assouplissement ou une correction (L3, L6, L7). Pour un critère W, l'effet est borné à WSL2 : **WSL2 est grisé** (S-02, S-08), à condition que Linux natif ait réussi. Si W2 conclut que le mode est indistinguable, ou si **un seul** des deux modes échoue à W1, **WSL2 entier est grisé** (décision du 2026-11-12, voir W1 et W2).

---

## Linux natif : L1 à L7

### L1. Sans privilège, sans binaire externe, sans conteneur

- **Énoncé.** Un utilisateur sans privilège (UID != 0, `CapEff` = 0) peut ouvrir un socket UDP, y positionner `IP_TTL` et `IP_RECVERR`, et émettre une sonde ; le binaire n'exécute aucun programme externe ni conteneur.
- **Méthode.** (a) Lancer le binaire sous un compte ordinaire sans `setcap` ni `sudo` ; le rapport consigne UID et `CapEff`. (b) Le paquet de trace n'importe pas `os/exec` (inspection de ses imports directs : `go list -f '{{.Imports}}'`). (c) Lancer avec `PATH` vide et sans moteur de conteneurs disponible : le résultat est identique. (d) `setsockopt` réussit sans `EPERM` (le rapport consigne l'erreur éventuelle, texte brut dans le détail, jamais dans un résumé affichable, Rule 129).
- **Effet de l'échec.** S-01 ne fonctionne pas => Epic B abandonné. **Décidé** (S-01 : « sans privilège, sans dépendance externe et sans conteneur »).

### L2. Réception des Time Exceeded : au moins 3 sauts avec adresse

- **Énoncé.** Vers une cible publique, le prototype obtient **au moins 3 sauts**, chacun avec l'adresse du routeur qui a répondu, rendus dans l'ordre croissant de TTL.
- **Méthode.** Une sonde par TTL ; le Time Exceeded est lu dans la file d'erreurs (`recvmsg` avec `MSG_ERRQUEUE`) et l'adresse du routeur est celle de l'émetteur de l'erreur (`sock_extended_err` / offender). Chaque sonde est reliée à son TTL (par socket ou par port : choix du spike). Rapport : TTL, adresse, latence. Un saut à adresse privée compte comme un saut.
- **Effet de l'échec.** S-01 ne fonctionne pas => Epic B abandonné. **Décidé** (S-01, critère 1).

### L3. Netpoller Go

- **Énoncé.** L'attente de la file d'erreurs rend la main **sur l'arrivée de l'erreur**, sans boucle d'attente active ; si ce n'est pas le cas, le plan B (socket bloquant avec délai de lecture) tient les mesures L5 et L6.
- **Méthode.** Lecture par `RawConn.Read` (le netpoller de Go) avec un délai de lecture large. Pour chaque sonde qui a reçu une réponse, le temps d'attente est de l'ordre de la latence mesurée, **très inférieur au délai** (le délai n'a pas réveillé la lecture). Aucune boucle `time.Sleep` / polling dans le code de lecture. Si la lecture ne se réveille pas : refaire L5 et L6 avec le plan B et consigner lequel des deux chemins est retenu.
- **Effet de l'échec.** Plan B retenu, **pas de no-go**. Si le plan B échoue aussi à L5 ou L6, c'est l'échec de L5 ou L6. **Décidé** (S-01, hypothèse 2).

### L4. Saut muet

- **Énoncé.** Un routeur qui ne répond pas est rapporté **muet** (avec son TTL) sans arrêter le trace avant la cible : les TTL suivants sont encore sondés et rapportés.
- **Méthode.** Soit un chemin réel contenant un saut muet, soit un laboratoire : espace de noms réseau avec un routeur qui jette ses ICMP Time Exceeded (la mise en place demande des droits, **pas le prototype**, qui reste sans privilège). **Cas net (définition stricte) : un routeur muet puis un routeur qui répond.** « Muet puis cible » n'est pas un cas net : les TTL muets entre le dernier routeur et la cible atteinte sont des sondes qui ont atteint la cible. Le rapport montre au moins un cas net. Une exécution sans cas net est neutre (décision 9).
- **Effet de l'échec.** S-01 ne fonctionne pas => Epic B abandonné. **Décidé** (utilisateur, 2026-11-12 : L4 bloquant).

### L5. Arrêt en 30 s au plus avec résultat explicite

- **Énoncé.** Vers une cible inatteignable (ou si le TTL maximum est atteint), le prototype s'arrête en **au plus 30 s** et rend un résultat explicite qui distingue « cible atteinte », « cible non atteinte, TTL maximum » et « cible non atteinte, délai écoulé ».
- **Méthode.** Mesure de l'horloge murale (`time`) sur une cible qui ne répond pas (par exemple une adresse du bloc de documentation `192.0.2.0/24`, jamais routée sur Internet). Cas « TTL maximum » : faute de cible dont le chemin dépasse réellement 30 sauts, TTL maximum abaissé à 5 vers `1.1.1.1`, et cible muette `192.0.2.1` (décision 10). Cas « délai écoulé » : configuration inversée, durée de sonde supérieure au délai global (par exemple sonde de 20 s, délai global de 1,5 s). Rapport : durée, résultat. Pas de seuil intermédiaire.
- **Effet de l'échec.** S-01 ne fonctionne pas => Epic B abandonné. **Décidé** (utilisateur, 2026-11-12 : L5 bloquant).

### L6. 8 sauts en moins de 5 s, sondes en parallèle

- **Énoncé.** Vers une cible joignable à 8 sauts, le résultat est **complet en moins de 5 s**, parce que les sondes de tous les TTL partent en parallèle.
- **Méthode.** Cible de référence `1.1.1.1`. On relève d'abord le TTL de la cible atteinte N (chemin réel présumé : N n'est pas toujours le nombre réel de sauts, voir `spike-linux.md`) : **si N est au moins 8, la mesure porte sur ce chemin** (les 8 premiers TTL au moins, résultat complet) ; **sinon le TTL est borné à 8** et la borne est consignée. Durée entre le lancement et le résultat complet, sur 5 exécutions, valeurs min / médiane / max consignées. La durée de sonde choisie est consignée ; elle doit rester inférieure à 5 s, y compris quand un saut est muet.
- **Effet de l'échec.** **Pas de no-go : on assouplit le seuil de S-03a** (le critère « moins de 5 s » est réécrit à partir des durées mesurées). **Décidé** (utilisateur, 2026-11-12). Si le seuil ne peut pas être réécrit parce que les sondes ne sont pas réellement parallèles (durée proche de la somme des délais), c'est un défaut de conception du prototype : corriger puis re-mesurer.

### L7. Annulation sans fuite de sockets

- **Énoncé.** Un trace en cours, annulé (contexte annulé), s'arrête et **ne laisse ni descripteur ni goroutine** derrière lui.
- **Méthode.** Annuler en plein trace (une sonde muette en attente), sur 100 cycles consécutifs. Comparer avant et après : nombre d'entrées de `/proc/self/fd` et `runtime.NumGoroutine()`. Aucune croissance. Consigner le délai d'arrêt après annulation (sans seuil : les stories n'en fixent pas).
- **Effet de l'échec.** Correction du prototype puis re-mesure ; **no-go seulement si la fuite est structurelle** (impossible à fermer avec l'API retenue). **Décidé** (utilisateur, 2026-11-12). Le rapport consigne la fuite, la correction et la mesure suivante.

### Verdict S-01

**Fonctionne** si L1 à L7 sont OK (L3 par le netpoller ou par le plan B). **Ne fonctionne pas** sinon, avec l'effet de chaque ligne en échec. Le verdict, le tableau L1 à L7 et les mesures sont écrits dans `docs/product/network-map/spike-linux.md` (S-01).

---

## WSL2 : W1 à W5 (S-02)

Chaque critère est évalué **pour chaque mode** (NAT, puis mirrored) et consigné séparément. La mesure est faite dans la distro WSL2 du poste Windows, par l'équipe d'agents ; l'utilisateur bascule le mode réseau. Les résultats vont dans `docs/product/network-map/spike-wsl.md`.

### W1. Mêmes mesures en NAT et en mirrored

- **Énoncé.** Dans le mode considéré, L1 à L7 sont tous OK.
- **Méthode.** Lancer le binaire de S-01 dans la distro WSL2 sous chaque mode. Rapport par mode : verdict L1 à L7 et premier saut. Risque connu à surveiller : en mirrored, les Time Exceeded peuvent ne jamais arriver (S-02).
- **Effet de l'échec.** Un mode en échec grise **WSL2 entier** (S-08), car le mode actif n'est pas toujours distinguable (W2) ; on livre pour Linux natif. **Décidé** (S-02, règle go/no-go du brief, puis utilisateur 2026-11-12 : WSL2 entier). Si **les deux** modes échouent et que Linux natif réussit, on livre pour Linux natif seul et **le rapport (`spike-wsl.md`) l'écrit explicitement** : le poste WSL2 de l'utilisateur n'est alors pas couvert. **Décidé.**

### W2. Détection de WSL et de son mode

- **Énoncé.** Le rapport établit, pour chaque mode, que l'environnement est WSL2 et **quel mode** est actif, par un signal qui distingue NAT de mirrored ; sinon il constate que le mode n'est pas distinguable.
- **Méthode.** Consigner `WSL_DISTRO_NAME`, le contenu de `/proc/version`, les interfaces et adresses (comme `internal/netiface`), la route par défaut, `/etc/resolv.conf`, et tout autre signal que le spike trouve. **Constat** (`spike-wsl.md`) : `wslinfo --networking-mode` (dans la distro) rend `nat` ou `mirrored` ; l'interface et la route par défaut diffèrent aussi. Le critère est vrai si au moins un signal donne des valeurs **différentes et stables** entre les deux modes sur toutes les exécutions.
- **Effet de l'échec.** Mode « indéterminé » : la colonne affiche `WSL2 mode undetermined` sans deviner et S-07 passe de S à M. **Décidé** (S-07 : critère et contexte d'effort). Si le mode est indistinguable, **WSL2 entier est grisé** (le mode en échec ne peut pas être exclu). **Décidé** (utilisateur, 2026-11-12). Le signal de détection est établi par le spike : `wslinfo --networking-mode` (WSL 2.7.13.0).

### W3. Le premier saut est l'hôte Windows

- **Énoncé.** L'adresse du premier saut (TTL 1) est identifiée comme l'hôte Windows par une **preuve reproductible**.
- **Méthode.** Comparer l'adresse du TTL 1 à la passerelle de la route par défaut de la distro (la même information que le stage `route`) et à l'interface de sortie. En NAT : la passerelle du NAT est le switch virtuel de l'hôte Windows (S-07) ; consigner la preuve utilisée. En mirrored : consigner si une preuve existe. « Pas de réponse au TTL 1 » est un résultat, consigné tel quel.
- **Effet de l'échec.** NAT sans preuve : pas d'étiquette `Windows host (virtual switch)`. Mirrored sans preuve : la passerelle n'est pas étiquetée hôte Windows (**Décidé**, S-07, critère mirrored).

### W4. Nature du `docker` visible depuis la distro

- **Énoncé.** Le rapport établit, **sans ambiguïté**, si le `docker` visible est Docker Desktop (binaire sous `/mnt/c/…`) ou un Engine installé dans la distro.
- **Méthode.** Chemin résolu du binaire `docker` (`command -v docker`, lien résolu), présence d'un démon dans la distro, et réponse de l'engine sur son nom et son système. **Constat** (`spike-wsl.md`) : l'intégration WSL de Docker Desktop expose le client sous `/mnt/wsl/docker-desktop/cli-tools/usr/bin/docker`, pas sous `/mnt/c/…` que cite S-02. Le critère est un **constat** : « non établi » est l'échec.
- **Effet de l'échec.** La colonne indique le doute plutôt que de deviner. **Décidé** (S-06, critère 4).

### W5. Bridge d'un conteneur côté distro

- **Énoncé.** Le rapport établit si le bridge du réseau d'un conteneur de test **existe** dans les interfaces lues dans la distro.
- **Méthode.** L'utilisateur lance à la main un conteneur sur le réseau par défaut puis sur un réseau défini ; le binaire liste les interfaces (`net.Interfaces`, comme `internal/netiface`) et compare à l'engine. Répéter pour chaque nature d'engine trouvée en W4 et pour chaque mode. Le critère est un **constat** : « non établi » est l'échec.
- **Effet de l'échec.** Bridge absent : aucun bridge n'est affiché, et le cas Docker Desktop passe par l'étape `Engine VM` (**Décidé**, S-05b et S-06). Bridge présent : il est une étape entre le conteneur et l'hôte (**Décidé**, S-05b).

### Verdict S-02

Un verdict **par mode** : « fonctionne » si W1 est OK, avec les constats W2 à W5. **Le verdict d'ensemble est « WSL2 supporté » seulement si les deux modes fonctionnent et que le mode est distinguable (W2) ; sinon WSL2 entier est grisé** (décidé). Si les deux modes échouent et que Linux natif est OK, le rapport l'écrit en toutes lettres. Les constats déterminent S-07 (étiquettes, effort S ou M), S-06 (boîte noire) et S-08 (WSL2 grisé).

---

## Effort par story (tel que porté par Plane)

| Story | Effort | Remarque |
|---|---|---|
| S-01 spike Linux | M | |
| S-02 spike WSL2 | M | Mesure dans la distro WSL2 |
| S-03a moteur `internal/nettrace` | M | Conditionné au go de S-01 |
| S-03b onglet Path | M | Dépend de `netdiag` ; raccourcis à confirmer |
| S-04 colonne de chemin | S | M si l'affichage ne passe pas par un `datatable` |
| S-05a conteneur source | M | |
| S-05b bridge vérifié | S | |
| S-06 Docker Desktop | S | |
| S-07 WSL2 | S | M si W2 échoue |
| S-08 plateformes grisées | S | La détection de WSL1 est vérifiée à l'implémentation de S-08, pas par le spike (décidé) |
| S-09 échec | S | |

## Intégration (indications, à valider par l'architecte)

- Nouveau paquet pur `internal/nettrace` (S-03a), sans Bubble Tea, comme `internal/ports` et `internal/netiface` ; le contexte annule (Rule 110 : le `Cmd` fait l'I/O, `Update` seul modifie le modèle).
- Onglet « Path » ajouté aux quatre onglets de `internal/ui/netdiag` (Diagnostics, Ports, Interfaces, Forward). Corps toujours un tableau (Rule 139), messages dans le pied de page (Rule 128), action grisée sur plateforme non supportée (Rule 130), textes en anglais US (Rule 129).
- Mise à jour de `docs/architecture/network.md` à la livraison : le trace revient, répond depuis cette machine, et `H` ou la touche choisie doit être déclarée dans le keymap.

---

## Décisions de l'utilisateur (2026-11-12), toutes marquées « Décidé » ci-dessus

1. Échecs : L4 et L5 bloquants (no-go) ; L6 : on assouplit le seuil de S-03a ; L7 : correction puis re-mesure, no-go seulement si la fuite est structurelle.
2. W2 indistinguable, ou un seul mode WSL2 en échec à W1 : WSL2 entier est grisé.
3. Les deux modes WSL2 en échec et Linux natif OK : on livre pour Linux natif seul, et le rapport l'écrit.
4. WSL1 : aucun constat dans S-02 ; la détection est vérifiée à l'implémentation de S-08.
5. Mesure : 5 exécutions par critère, 100 cycles d'annulation (L7), TTL maximum 30, IPv4 seul, `x/sys` en dépendance directe.
6. Cible publique de référence `1.1.1.1` ; L6 : si la cible est atteinte à un TTL N >= 8, N est retenu comme chemin réel présumé et on mesure dessus, sinon le TTL est borné à 8 et consigné.
7. Prototype : code jetable sur la branche locale `spike/nettrace`, non publiée et jamais fusionnée ; seuls les documents (dont `spike-linux.md`) sont fusionnés.
8. Lieu de mesure de S-01 : la machine Linux native (Debian 13, noyau 6.12, amd64), utilisateur non-root, accès ssh.
9. L4 neutre : une exécution sans « routeur muet puis routeur qui répond » est neutre ; L4 est OK si au moins une exécution est nette et qu'aucune n'est KO. « Muet puis cible » n'est pas un cas net.
10. L5 : le cas « TTL maximum » est mesuré avec le TTL maximum abaissé à 5 et la cible muette `192.0.2.1`, faute de cible dont le chemin dépasse 30 sauts ; l'état « délai écoulé » exige une configuration inversée (durée de sonde supérieure au délai global).

## Tranché par le spike

- Signal du mode WSL2 : `wslinfo --networking-mode` (`nat` ou `mirrored`), W2, `spike-wsl.md`.
- Chemin du client Docker Desktop vu depuis la distro : `/mnt/wsl/docker-desktop/cli-tools/usr/bin/docker`, pas `/mnt/c/…`, W4, `spike-wsl.md`.

## Reste À CONFIRMER

1. Raccourcis de lancement et d'annulation de l'onglet (`enter` / `esc` proposés par S-03b) et raccourci de pré-remplissage depuis une ligne `route` de Diagnostics.
2. Libellés UI en anglais US (propositions dans `brief.md`).
3. Métriques de succès de l'Epic B.
4. Un réglage utilisateur du TTL maximum dans l'Epic B (`traceroute_max_hops` a été supprimé en §3.47) : non tranché ; le spike fixe 30.
