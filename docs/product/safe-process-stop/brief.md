# Brief — arrêt sûr de processus (`safe-process-stop`)

Mis à jour le 2026-11-22 après la recherche (`research.md`), la critique, la faisabilité de l'architect et les décisions de l'utilisateur du même jour (Q3, Q4, Q5, Q7, puis D-A à D-D après la faisabilité). Ce brief remplace la première version : l'idée « gestionnaire de tâches » est recadrée. Le dossier s'appelait `process-list` (slug provisoire) ; il a été renommé `safe-process-stop` (Q7).

## Vision (une phrase)

Quand un développeur arrête un processus depuis DevDesk, il arrête bien celui qu'il croit arrêter, sans se tuer lui-même ni tuer un processus critique, en laissant d'abord au processus le temps de s'arrêter proprement quand le système le permet.

## Historique de l'idée

- Idée initiale : « un écran pour lister les processus en cours, faut-il des droits admin, arrêter un processus ».
- Première ambition : « remplacer le gestionnaire de tâches » (CPU, mémoire, arrêt propre puis forcé, Linux, Windows, macOS).
- La recherche et la critique ont montré : valeur non démontrée face à htop, btop, bottom et au Gestionnaire des tâches ; dérive de périmètre produit ; arrêt propre quasi inexistant sous Windows ; PID réutilisé non revérifié par `ports.Kill` ; rien n'empêche de tuer DevDesk lui-même ou un processus critique ; coût multiplateforme.
- **Décision de l'utilisateur (2026-11-22)** : d'abord sécuriser l'arrêt de l'onglet Ports ; ensuite, si l'envie reste, un écran réduit aux processus liés à DevDesk (lancés par lui, ou liés à un workspace ou à un port), avec arrêt sûr. Le gestionnaire de tâches complet est écarté ou repoussé.
- La « tension à lever » de la première version (jalon 1 sans CPU ni mémoire ou avec) disparaît : il n'y a plus de gestionnaire de tâches, donc plus de métriques dans le périmètre.

## Problème

Aujourd'hui, `K` dans l'onglet Ports de Diagnostics (`internal/ui/netdiag/ports_model.go`, `internal/ports/ports.go`) envoie un signal à un PID, après une confirmation. Faits lus dans le code :

1. **PID réutilisé non revérifié.** `ports.Kill` fait `os.FindProcess(pid)` puis `p.Kill()` : aucune vérification que le PID désigne encore le processus listé. Entre la lecture du tableau (rafraîchi toutes les 2 s environ), l'ouverture de la confirmation et l'envoi, le processus peut être sorti et son PID repris par un autre.
2. **Se tuer soi-même est possible.** Aucun appel à `os.Getpid` ni `os.Getppid` dans `internal/` ni dans `main.go`. Or le tableau liste les sockets de toute la machine, y compris ceux que DevDesk ouvre lui-même (les écoutes des port-forwards de `internal/forward` et le serveur MCP sont dans le processus DevDesk). Une ligne du tableau peut donc porter le PID de DevDesk, et `K` la tuerait. Déduit de la lecture du code, non reproduit. Idem pour le shell qui a lancé DevDesk ou PID 1 si un socket leur est attribué.
3. **Arrêt brutal seul.** SIGKILL (Unix) ou `TerminateProcess` (Windows) directement ; la confirmation le dit (« The process gets no chance to shut down cleanly ») mais ne propose rien de plus doux, alors qu'un serveur de dev, une base ou un éditeur gagnent à recevoir SIGTERM d'abord.

Cas d'usage visé (preuves communautaires de la recherche, pas de mesure de fréquence) : serveur de dev orphelin qui tient un port (`EADDRINUSE`), `kubectl port-forward` oublié ; geste standard `lsof -i :PORT` puis `kill` (SIGTERM avant `kill -9`).

## Personas

- **Développeur DevSecOps qui utilise DevDesk au quotidien** (l'utilisateur lui-même est le persona principal) : libère un port bloqué sans quitter le TUI, sur Linux, Windows ou macOS.
- **Même développeur sous WSL2** : ne voit et n'arrête que les processus Linux de la VM (voir non-objectifs).

## Solutions existantes (voir `research.md`)

htop, btop, bottom, procs, glances, gotop, Process Explorer, Gestionnaire des tâches Windows (page non consultée), Activity Monitor, `kill` / `taskkill`. Aucun ne s'intègre au contexte DevDesk (ports, workspaces, scans, conteneurs). Cette intégration est l'apport éventuel d'un écran DevDesk, non démontré par une source. Pour la sécurisation de l'arrêt, le périmètre est celui d'un outil qui existe déjà dans DevDesk : le défaut est local, pas concurrentiel.

## Décisions validées

1. Pas de gestionnaire de tâches complet (tous les processus, CPU, mémoire, arbre).
2. D'abord : sécuriser l'arrêt de l'onglet Ports (epic « Arrêt sûr de processus », S-01 et S-02).
3. Ensuite, seulement si l'envie reste : un écran des processus liés à DevDesk (S-03, à raffiner).
4. Pas d'élévation automatique de droits : DevDesk tente et affiche le refus.
5. **Identité illisible : refus (Q3, 2026-11-22).** Si l'identité d'un processus ne peut pas être lue, `K` REFUSE d'arrêter et affiche un message clair (S-01, critère 6). Plus de « envoyer quand même ».
6. **Windows, première livraison (Q4, 2026-11-22).** Arrêt forcé direct avec un libellé honnête ; `WM_CLOSE` reste S-04, Could.
7. **S-03 reste Could (Q5, 2026-11-22)**, avec un go/no-go de l'utilisateur après S-01 et S-02. Le choix de la piste de découverte (A : PID notés au lancement, B : descendants de DevDesk) reste à trancher, mais seulement si le go est donné.
8. **Slug (Q7, 2026-11-22) : `safe-process-stop`.**
9. **Identité lue au listing (D-A, 2026-11-22).** L'heure de création de chaque PID distinct qui tient un socket est lue au listing. Repli : lecture à l'ouverture de la confirmation si le coût mesuré sous Windows (189 sockets) dépasse un seuil fixé par mesure dans S-01b.
10. **Identité illisible : grisé d'avance (D-B).** `K` est grisé avec `Cannot verify which process this is` (Rule 130) ; en profondeur, `ports` refuse d'envoyer le signal avec `Cannot verify which process PID N is — not stopped`. Précise Q3.
11. **Fenêtre résiduelle acceptée (D-C).** Sans pidfd (macOS, Linux < 5.4), la fenêtre de quelques microsecondes entre la vérification et le signal est acceptée et documentée.
12. **Windows, handle refusé (D-D).** Le message dit `Refused by the system` (cause réelle) plutôt que `Cannot verify`. Recommandation de l'architect. Hypothèse: à valider.
13. **Découpage (architect).** S-01a (protégés + cible capturée, S), S-01b (identité à chaque signal, M), S-02a (libellé Windows, S, avec S-01a), S-02b (arrêt propre, M), S-03 devient un spike, S-04 reste Could. Voir `stories.md`.

## Hypothèses retenues par défaut, non encore validées

Recommandations du pm appliquées en attendant l'avis de l'utilisateur ; elles ne sont pas des décisions (voir « Questions ouvertes ») :
- Hypothèse: **Q1** — seul le parent direct est refusé ; les ancêtres plus hauts (terminal, `ssh`, multiplexeur) seront gérés plus tard, avec un avertissement dans la confirmation. À valider.
- Hypothèse: **Q2** — attente de 5 s fixe après SIGTERM, non configurable. À valider.
- Hypothèse: **Q6** — la touche `K` « Kill » devient « Stop » quand l'arrêt propre est en place. À valider.
- Hypothèse: **Q8** — pas de télémétrie ; le jugement de l'utilisateur est la seule mesure de succès. À valider.

## Droits (rappel factuel de la recherche, non mesuré sauf mention)

- **Lister** : pas de droits admin pour PID et nom. Mesuré dans `ports.go` sous Windows : 294 processus nommés en 11 ms (snapshot Toolhelp32), élevé ou non. En revanche, la lecture par PID (heure de création, exécutable, propriétaire) donne « Access denied » pour 98 sockets sur 189 sans élévation. Le détail d'autres utilisateurs ou de processus protégés se restreint ; sous Linux `hidepid=1` protège `cmdline` et `status` des autres, `hidepid=2` cache aussi leurs répertoires (la liste est alors incomplète sans erreur) ; sous macOS, rien de plus sur les processus des autres sans sudo.
- **Arrêter** : ses propres processus, non. Ceux d'un autre utilisateur ou du système, oui. Certains processus Windows protégés refusent même un administrateur (non vérifié par une source ; mesuré dans le code : PID 4, System, répond `ERROR_ACCESS_DENIED`). macOS : `kill(2)` suit POSIX (EPERM si l'UID ne correspond pas) ; l'effet exact de SIP sur les signaux n'est pas établi.
- **DevDesk ne s'élève pas** : le refus du système est affiché (message existant `killFailureMessage`).
- **WSL2** : seuls les processus Linux de la VM sont visibles et arrêtables.

## Hypothèses à valider

| # | Hypothèse | Comment la valider |
|---|---|---|
| H1 | Le besoin d'arrêter un processus depuis DevDesk est réel et fréquent, au point de justifier le travail | Aucune donnée (research : ND). L'utilisateur est le seul juge : s'appuyer sur son usage de `K` dans Ports. Aucune télémétrie connue dans DevDesk (Hypothèse: à confirmer) |
| H2 | Un PID réutilisé entre l'affichage et le signal arrive assez pour mériter un garde-fou | Réel et documenté (gopsutil ne vérifie pas) ; fréquence inconnue. Le garde-fou est peu coûteux : on le fait quoi qu'il arrive |
| H3 | Lire une identité stable du processus (heure de création, ou handle ouvert) est possible sur les trois OS pour les processus que l'utilisateur peut tuer | Faisabilité de l'architect : oui, par handle ouvert d'abord (pidfd Linux >= 5.4, handle Windows) puis valeur brute propre à l'OS (ticks Linux, FILETIME Windows, `timeval` macOS). Reste à mesurer dans S-01b : le coût de la lecture au listing sous Windows (seuil à fixer). Risque connu : la lecture par PID est refusée pour beaucoup de processus sans élévation (K grisé, D-B) |
| H4 | Une attente de 5 s après SIGTERM suffit pour un serveur de dev courant | Hypothèse: à valider à l'usage (serveurs de dev, bases locales). Pas de source : l'attente est un choix produit, non configurable dans S-02 |
| H5 | Sous Windows, la plupart des processus qui tiennent un port (serveurs, services en console) n'ont pas de fenêtre ; aucun arrêt propre n'est donc possible pour eux | Établi en partie par la recherche (WM_CLOSE inopérant sur une application console) ; à confirmer sur des cas réels (node, python, dotnet) |
| H6 | DevDesk ne sait rien aujourd'hui des PID qu'il lance (voir S-03) | Établi par lecture du code ; à confirmer par l'architect |
| H7 | L'écran des processus liés à DevDesk apporte quelque chose que `:jobs`, l'onglet Forward, la vue conteneurs et l'onglet Ports ne donnent pas déjà | Non démontré. Condition de go de S-03 |

## Périmètre MVP

- **Dans** : l'arrêt depuis l'onglet Ports (`K`) devient sûr : refus de tuer DevDesk, son parent direct, PID 1, et PID 0 et 4 sous Windows, cible capturée à l'ouverture de la confirmation (S-01a) ; identité du processus revérifiée à chaque signal, `K` grisé si l'identité est illisible (S-01b, Q3 et D-A à D-C) ; libellé Windows honnête, arrêt forcé direct (S-02a, Q4) ; arrêt propre d'abord sous Linux et macOS, attente bornée sans bloquer l'interface, puis proposition de forcer (S-02b).
- **Conditionnel** : spike sur les processus lancés par DevDesk (S-03, Could ; l'écran lui-même, S-03a à d, seulement après un go de l'utilisateur, Q5), arrêt propre Windows par `WM_CLOSE` (S-04, Could, voire Won't, Q4).
- Mêmes règles pour toutes les plateformes ; sous WSL2 elles s'appliquent à la VM.

## Non-objectifs

- Pas de gestionnaire de tâches complet : pas de liste de tous les processus, pas de CPU ni de mémoire.
- Pas d'élévation automatique de droits (ni `sudo`, ni UAC, ni relance en administrateur).
- Pas d'arbre de processus, pas d'arrêt des enfants avec le parent.
- Pas de signaux au choix (SIGHUP, SIGSTOP, SIGUSR1...) ; seulement arrêt propre et arrêt forcé.
- Pas de contrôle des processus de l'hôte Windows depuis WSL2.
- Pas de réglage de la durée d'attente dans la première livraison.
- Pas de protection de « tous les processus critiques » : liste courte et justifiée (voir S-01), le reste est laissé au système d'exploitation qui refuse.

## Métriques de succès

Le projet n'a, à ma connaissance, pas de télémétrie (Hypothèse: à confirmer) ; les métriques sont donc des garanties testables plus un jugement de l'utilisateur.

1. **Garantie d'identité** : zéro signal envoyé à un processus dont l'identité ne correspond pas à celle listée. Garantie complète sous Linux >= 5.4 et Windows (handle ouvert avant la comparaison) ; garantie résiduelle documentée ailleurs (macOS, Linux < 5.4 : fenêtre de quelques microsecondes acceptée, D-C). Vérifiée par des tests qui simulent un PID réutilisé (seams de `internal/ports`), sur chaque OS de la CI, ou test manuel pour la plateforme sans CI.
2. **Garantie de non-suicide** : une tentative d'arrêt de DevDesk, de son parent direct ou de PID 1 est refusée dans 100 % des cas, par l'interface et par `ports.Kill` lui-même.
3. **Arrêt propre** : sous Linux et macOS, un processus qui gère SIGTERM disparaît sans que l'utilisateur ait à forcer ; un qui l'ignore se voit proposer la suite après 5 s au plus.
4. **Honnêteté Windows** : aucun libellé ne promet un arrêt propre pour un processus sans fenêtre.
5. **Jugement** : l'utilisateur ne quitte plus le TUI pour un `kill` depuis le terminal quand il libère un port (mesure : son propre constat à l'usage, un mois après livraison). Sert aussi de condition de go pour S-03. Hypothèse: c'est la seule mesure de succès (Q8, non encore validée).

## Risques

| Risque | Gravité | Mitigation |
|---|---|---|
| Identité illisible sous Windows sans élévation (98 lectures par PID sur 189 refusées) | Moyenne : le garde-fou ne peut pas s'appliquer à tout ; `K` est grisé pour ces processus, ce que l'utilisateur peut trouver frustrant | Décisions Q3 et D-B : `K` grisé d'avance avec la raison, refus en profondeur dans `ports` ; ces processus refusent en général aussi l'arrêt ; mesurer à l'usage la part de refus |
| Coût de lecture d'identité au listing sous Windows (189 sockets) | Moyenne : un rafraîchissement trop lent dégrade l'onglet | D-A : seuil fixé par mesure dans S-01b ; repli : lecture à l'ouverture de la confirmation |
| Fenêtre résiduelle sans pidfd (macOS, Linux < 5.4) | Faible : quelques microsecondes entre vérification et signal | D-C : acceptée et documentée dans `docs/architecture/network.md` |
| Attente après SIGTERM : état partagé modifié hors de `Update` | Moyenne : course (Rule 110) | S-02b : tick et sonde renvoient des messages portant une génération ; `esc` incrémente la génération ; tests par injection de messages |
| La technique d'identité diffère par OS (heure de création, pidfd Linux ≥ 5.4, handle Windows via `os.Process.WithHandle`) | Moyenne : coût multiplateforme | La story énonce le comportement attendu ; l'architect tranche la technique ; spike si H3 se révèle faux |
| La liste « critique » est incomplète (processus noyau, services) | Faible : le système refuse déjà | Liste courte et justifiée ; refus système conservé et expliqué |
| SIGTERM ignoré, ou processus zombie (aucun effet visible) | Faible | Proposer de forcer après l'attente ; un zombie reste un zombie (Hypothèse: le signaler si lisible, voir S-02) |
| Fausse promesse d'arrêt propre sous Windows | Moyenne : confiance de l'utilisateur | S-02 impose un libellé honnête ; S-04 reste Could |
| S-03 reconstruit un gestionnaire de tâches par petites touches (dérive de périmètre) | Élevée | Périmètre de S-03 limité à ce que DevDesk sait avoir lancé ou lié ; condition de go explicite ; la liste de tous les processus est en Won't |
| Aucun PID n'est conservé aujourd'hui par DevDesk (voir S-03) : S-03 est plus lourd qu'il ne paraît | Moyenne | S-03 reste « à raffiner » avec des pistes de découpage |

## Questions tranchées (2026-11-22)

- **Q3 — Identité illisible** : refuser d'arrêter, avec un message clair (S-01).
- **Q4 — Windows** : arrêt forcé direct avec libellé honnête à la première livraison ; `WM_CLOSE` reste S-04, Could.
- **Q5 — S-03** : reste Could, go/no-go après S-01 et S-02. La piste de découverte (A ou B) n'est à choisir qu'en cas de go.
- **Q7 — Slug** : `safe-process-stop`.

## Questions ouvertes (hypothèses retenues, à valider)

0. **D-D (message Windows)** : `Refused by the system` quand le handle est refusé (recommandation de l'architect). Hypothèse: à valider. La raison grisée au listing (`Cannot verify which process this is`) doit-elle aussi nommer la cause réelle quand elle est connue ?
0 bis. **S-02b** : un zombie compte comme arrêté ; un seul arrêt en attente à la fois. Hypothèse: à valider.
0 ter. **S-04** : Could ou Won't ? Proposition du pm : Won't si le cas d'une application à fenêtre arrêtée depuis Ports ne se présente pas dans le mois qui suit S-02a.

1. **Ancêtres (Q1)** : parent direct seulement refusé (retenu par défaut) ; ancêtres plus hauts (terminal, `ssh`, multiplexeur) : un avertissement dans la confirmation, à étudier plus tard. Les tuer ferme la session de DevDesk. Non encore validé.
2. **Durée d'attente (Q2)** : 5 s fixe, non configurable (retenu par défaut) ; une base de données lente à s'arrêter est le cas qui plaidera pour un réglage. Non encore validé.
3. **Libellé de la touche (Q6)** : `K` « Kill » devient « Stop » une fois l'arrêt propre en place (retenu par défaut). Question de vocabulaire (règles clavier de `internal/ui/keymap`, Rule 130). Non encore validé.
4. **Télémétrie (Q8)** : le jugement de l'utilisateur est la seule mesure de succès (retenu par défaut, aucune télémétrie connue). Non encore validé.
5. **S-03, si go** : piste A (PID notés à chaque lancement) ou B (descendants de DevDesk lus dans la table des processus) ? Un orphelin fait-il partie du besoin ? (voir S-03)
