# Stories — arrêt sûr de processus (`safe-process-stop`)

Version du 2026-11-22, après la faisabilité de l'architect et les décisions D-A à D-D de l'utilisateur (voir « Décisions appliquées »). Le backlog est créé dans Plane (module « Arrêt sûr de processus », identifiants `DEVDESK-n` dans la table « Correspondance Plane »). Les identifiants S-01a à S-04 restent les noms de travail ; chaque work item Plane renvoie à ce fichier.

Les efforts (S, M) viennent de l'architect (faisabilité rapide). Les textes d'interface sont en anglais US (Rule 129). Les stories énoncent le comportement ; les contraintes techniques décidées sont regroupées sous « Contrainte de conception » quand elles s'imposent au coder.

Dans tout l'epic, « l'onglet Ports » désigne `internal/ui/netdiag` (onglet Ports) et « `K` » son action d'arrêt, telle qu'elle existe aujourd'hui (`confirmKill`, `killSelected`, `ports.Kill`).

Définition de fini commune : tests (table-driven, `mise run ci` vert), `docs/architecture/network.md` (section Ports et `internal/ports`) mis à jour dans le même commit.

## Décisions appliquées (2026-11-22, après la faisabilité)

- **D-A — Identité lue au listing.** L'identité (heure de création) est lue AU LISTING, une fois par PID distinct qui tient un socket. Repli : lecture à l'ouverture de la confirmation si le coût mesuré sous Windows (189 sockets) dépasse un seuil fixé par mesure dans S-01b.
- **D-B — Identité illisible : grisé d'avance, refus en profondeur.** `K` est GRISÉ avec la raison `Cannot verify which process this is` (Rule 130) ; en profondeur, `ports` refuse d'envoyer le signal avec `Cannot verify which process PID N is — not stopped`.
- **D-C — Fenêtre résiduelle acceptée.** Sans pidfd (macOS, Linux < 5.4), la fenêtre de quelques microsecondes entre la vérification et le signal est ACCEPTÉE et documentée. Garantie complète sous Linux >= 5.4 et Windows (handle ouvert avant la comparaison).
- **D-D — Windows, handle refusé.** Si l'ouverture du handle est refusée, le message dit `Refused by the system` (cause réelle) plutôt que `Cannot verify`. Recommandation de l'architect. Hypothèse: à valider (voir S-01b).

Décisions antérieures : Q3 (identité illisible = refus), Q4 (Windows = arrêt forcé direct avec libellé honnête, `WM_CLOSE` en S-04), Q5 (S-03 reste Could, go/no-go après S-01 et S-02), Q7 (slug).

## Stratégie de test (architect)

- **Seams dans `internal/ports`** : `selfPID`, `parentPID` et `procOS` (lecture d'identité, ouverture de handle, envoi de signal) sont des variables de package substituables en test. Aucun test ne dépend de la machine.
- **Garde à l'exécution** : dans un binaire de test, tout vrai signal envoyé par `ports` sans seam substitué fait paniquer le test (même principe que le garde `DEVDESK-610` du trousseau). Test statique associé si possible.
- **Vrai signal** : réservé à des tests d'intégration qui lancent un processus enfant de test (`exec.Command` du binaire de test ou `sleep`) et appellent `Wait()` en `t.Cleanup` pour ne laisser ni zombie ni orphelin.
- **Jamais de vrai Stop sur PID 1 ni sur le parent** : ces cas se testent avec les seams (`selfPID`, `parentPID` renvoient des valeurs choisies) et la vérification que `procOS.Signal` n'est pas appelé.
- **Tests UI par injection de messages** (`tea.Msg` dans `Update`, sans vrai terminal) : réordonnancement du tableau entre l'ouverture de la confirmation et `Yes`, tick de sonde, tick d'une génération périmée après `esc`.
- Les clés de test passent par `internal/ui/testutil` (`Key`, `Keys`), jamais un `tea.KeyPressMsg` littéral.

## Bug lié

Un bug existant est créé par l'orchestrateur (hors de ce backlog, identifiant à renseigner par lui) : « la confirmation de l'onglet Ports relit la ligne sélectionnée au moment du Yes alors que le rafraîchissement continue ; si l'ordre des sockets change, Yes peut arrêter le processus d'une autre ligne » (`internal/ui/netdiag/ports_model.go`, environ l. 281-283 et 492-505). S-01a le corrige par construction (cible capturée à l'ouverture de la confirmation) : le bug se ferme avec S-01a.

---

## Epic : Arrêt sûr de processus

Rendre l'arrêt d'un processus depuis DevDesk sûr (bon processus, pas DevDesk, pas un processus critique) et, quand le système le permet, doux d'abord.

Ordre de livraison : S-01a et S-02a ensemble, puis S-01b, puis S-02b. S-03 (spike) et S-04 seulement sur décision de l'utilisateur.

### S-01a — Ne jamais arrêter DevDesk ni un processus critique, et arrêter la ligne confirmée
Epic : Arrêt sûr de processus — Priorité : Must — Effort : S

En tant que développeur qui libère un port depuis l'onglet Ports, je veux que `K` refuse de toucher DevDesk, son parent direct ou le processus d'initialisation, et qu'il arrête exactement le processus que la confirmation m'a montré, afin de ne jamais tuer par erreur ma propre session, la machine ou le processus d'une autre ligne.

Lié au bug « la confirmation relit la ligne sélectionnée au moment du Yes » (voir « Bug lié ») : cette story le corrige.

**Critères d'acceptation**
1. Étant donné une ligne dont le PID est celui de DevDesk lui-même (cas réel : l'écoute d'un port-forward ou du serveur MCP), quand l'utilisateur appuie sur `K`, alors l'action est grisée avec la raison `This is DevDesk itself` (Rule 130) et aucune confirmation ne s'ouvre ; la ligne reste visible.
2. Étant donné une ligne dont le PID est celui du parent direct de DevDesk (seulement si ce parent est né avant DevDesk) ou le PID 1 (init sous Linux, `launchd` sous macOS), quand l'utilisateur appuie sur `K`, alors l'action est grisée avec une raison nommée : `This process started DevDesk` ou `PID 1 is the system's init process`.
3. Étant donné Windows et une ligne dont le PID est 0 ou 4 (System), quand l'utilisateur appuie sur `K`, alors l'action est grisée avec `Windows system process` plutôt que de laisser le système répondre « Access denied ».
4. Étant donné un appel direct à `ports.Kill` avec le PID de DevDesk, de son parent direct (né avant DevDesk), 1, ou 0 et 4 sous Windows (sans passer par l'interface), quand il est exécuté, alors il retourne une erreur nommant la raison et n'envoie aucun signal (garde en profondeur : l'interface n'est pas la seule protection).
5. Étant donné la confirmation ouverte sur la ligne `node` (PID 4242), quand le rafraîchissement du tableau change l'ordre des sockets avant que l'utilisateur réponde, alors `Yes` agit sur le PID 4242 capturé à l'ouverture et jamais sur la ligne alors sélectionnée ; le texte de la confirmation montre le nom et le PID capturés.
6. Étant donné la confirmation ouverte, quand l'utilisateur déplace la sélection ou que le processus disparaît du tableau rafraîchi, alors la confirmation reste sur sa cible capturée (le texte ne change pas) ; `No` ou `esc` la ferme sans signal.

**Hors périmètre** : vérification de l'identité à chaque signal (S-01b) ; arrêt propre (S-02b) ; libellé Windows (S-02a, livré avec cette story) ; protection des autres ancêtres (terminal, `ssh`, multiplexeur) ; liste exhaustive de processus critiques (le système refuse déjà, `killFailureMessage` explique).
**Hypothèses** :
- Hypothèse: le parent direct est protégé seulement s'il est né avant DevDesk (un parent mort dont le PID a été repris par un processus plus jeune n'est pas « celui qui a lancé DevDesk »). Si l'heure de création du parent est illisible, on le protège par prudence. À valider.
- Hypothèse: critères 1 et 2 restent vrais sous WSL2 (identités de la VM Linux).
- Hypothèse: PID 0 et 4 sous Windows sont les seuls ajouts justifiés par le dépôt (PID 4 mesuré refusé dans le commentaire de `killFailureMessage`).
**Hypothèse retenue, non encore validée** : Q1 (seul le parent direct est refusé ; ancêtres plus hauts plus tard, avec avertissement).
**Questions ouvertes** : aucune bloquante.

### S-01b — Vérifier l'identité du processus à chaque signal
Epic : Arrêt sûr de processus — Priorité : Must — Effort : M
Dépend de : S-01a (livrer S-01a d'abord ; les seams `selfPID`/`parentPID`/`procOS` et la cible capturée y sont posés).

En tant que développeur qui libère un port, je veux que DevDesk n'envoie le signal que si le processus visé est encore celui qui était listé, afin qu'un PID réutilisé entre l'affichage et l'envoi ne fasse jamais arrêter un autre programme.

**Critères d'acceptation**
1. Étant donné un rafraîchissement du tableau, quand il se termine, alors l'identité (heure de création) de chaque PID distinct qui tient un socket a été lue une seule fois et est rattachée aux lignes de ce PID (valeur brute propre à l'OS).
2. Étant donné une ligne listant `node` (PID 4242), quand à l'instant du signal le PID 4242 a une autre identité que celle lue au listing (processus sorti et PID repris), alors aucun signal n'est envoyé, le pied de page affiche en niveau Warn `node (PID 4242) is no longer the process that was listed — table refreshed`, et le tableau est relu.
3. Étant donné une ligne dont l'identité est inchangée, quand l'utilisateur confirme, alors le signal est envoyé et le comportement existant (point d'activité, message de refus du système, `PID N is no longer running`) est inchangé.
4. Étant donné une ligne dont l'identité n'a pas pu être lue au listing, quand l'utilisateur appuie sur `K`, alors l'action est grisée avec la raison `Cannot verify which process this is` (Rule 130), la ligne reste visible, et aucune confirmation ne s'ouvre.
5. Étant donné un appel direct à `ports.Kill` pour un PID dont l'identité ne peut pas être vérifiée, quand il est exécuté, alors il retourne l'erreur `Cannot verify which process PID N is — not stopped` et n'envoie aucun signal.
6. Étant donné Windows et l'ouverture du handle refusée (ou, en repli, la lecture refusée à l'ouverture de la confirmation), quand le signal est tenté, alors aucun signal n'est envoyé et le message dit `Refused by the system — DevDesk cannot signal PID N` (cause réelle), pas `Cannot verify`.
7. Étant donné la mesure du coût de lecture d'identité sous Windows (189 sockets, sans élévation), quand elle dépasse le seuil fixé dans la story (Hypothèse: à fixer par mesure avant le code ; valeur de départ proposée : 100 ms ajoutées à un rafraîchissement), alors l'identité est lue à l'ouverture de la confirmation au lieu du listing, et le critère 2 reste vrai ; la mesure et le seuil retenu sont notés dans `docs/measurements/` et `docs/architecture/network.md`.

**Contrainte de conception (architect, décisions D-A à D-C)** : le handle est ouvert d'abord (pidfd Linux >= 5.4, handle Windows), puis l'identité est comparée sur la valeur brute propre à l'OS (ticks Linux, FILETIME Windows, `timeval` macOS), pas sur la valeur en millisecondes de gopsutil. Sans pidfd (macOS, Linux < 5.4), la fenêtre résiduelle de quelques microsecondes entre la vérification et le signal est acceptée et documentée dans `docs/architecture/network.md`.
**Hors périmètre** : processus protégés (S-01a) ; arrêt propre (S-02b) ; réutilisation de l'identité par un écran de processus (S-03).
**Hypothèses** :
- Hypothèse: à valider (D-D) : sous Windows, `Refused by the system` s'applique quand l'ouverture du handle est refusée au moment du signal, ou quand le repli de lecture à la confirmation est refusé. Pour les PID dont la lecture au listing est refusée (98 sur 189 sans élévation), `K` est grisé d'avance avec `Cannot verify which process this is` (critère 4) : la cause réelle (accès refusé) n'y est pas nommée. À trancher : nommer aussi la cause dans la raison grisée ?
- Hypothèse: les valeurs brutes ne se comparent qu'entre elles, jamais entre OS ; la valeur est opaque pour l'interface.
- Hypothèse: le seuil du critère 7 est un budget par rafraîchissement ; il ne change pas la cadence (2 s).
**Questions ouvertes** : D-D à valider par l'utilisateur ; seuil du critère 7.

### S-02a — Dire la vérité sous Windows : arrêt forcé direct
Epic : Arrêt sûr de processus — Priorité : Must — Effort : S
À livrer avec S-01a.

En tant que développeur sous Windows qui arrête un processus depuis l'onglet Ports, je veux que DevDesk dise que l'arrêt est immédiat et sans sauvegarde, afin de ne jamais croire à un arrêt propre qui n'existe pas pour ce processus.

**Critères d'acceptation**
1. Étant donné la confirmation ouverte sur un processus Windows (`node.exe`, PID 4412), quand l'utilisateur la lit, alors elle dit : `Terminate node.exe (PID 4412)? Windows has no clean stop for this process: it is ended at once and cannot save anything.`
2. Étant donné cette confirmation, quand l'utilisateur confirme, alors le processus est arrêté en un seul geste : pas d'attente, pas de seconde confirmation.
3. Étant donné Windows, quand la touche, la confirmation ou le pied de page sont affichés, alors aucun libellé ne promet ni ne suggère un arrêt propre (la touche reste `K` « Kill » tant que Q6 n'est pas tranchée).
4. Étant donné Linux ou macOS, quand S-02b n'est pas encore livrée, alors le texte et le comportement de la confirmation sont inchangés.

**Hors périmètre** : `WM_CLOSE` (S-04) ; arrêt propre Linux et macOS (S-02b) ; changement du libellé de la touche (Q6).
**Hypothèses** :
- Hypothèse: l'arrêt propre sous Windows n'est réalisable que par `WM_CLOSE` (processus à fenêtre) ou `CTRL_BREAK` (enfant lancé par DevDesk dans un nouveau groupe) ; ni l'un ni l'autre ne s'applique à un processus arbitraire de l'onglet Ports sans fenêtre (H5 du brief). `os.Process.Signal(Interrupt)` n'est pas implémenté sous Windows en Go.
**Décision** : Q4 (Windows : arrêt forcé direct, libellé honnête à la première livraison).
**Questions ouvertes** : aucune.

### S-02b — Arrêt propre d'abord (Linux, macOS), puis forcer sur confirmation
Epic : Arrêt sûr de processus — Priorité : Must — Effort : M
Dépend de : S-01b (l'identité est revérifiée à chaque signal, y compris le SIGKILL de la seconde étape).

En tant que développeur qui arrête un serveur de dev ou une base locale depuis l'onglet Ports sous Linux ou macOS, je veux qu'un arrêt propre soit tenté d'abord, puis qu'on me propose de forcer, afin de ne pas perdre de données par un arrêt brutal inutile, sans que l'interface se fige pendant l'attente.

**Critères d'acceptation**
1. Étant donné la confirmation ouverte sur `nginx` (PID 812), quand l'utilisateur la lit, alors elle annonce un arrêt propre en deux temps : `Ask nginx (PID 812) to stop?` suivi de `Sends SIGTERM and waits up to 5 s. You can force it afterwards.` ; elle n'annonce plus un SIGKILL.
2. Étant donné un SIGTERM envoyé, quand le processus disparaît dans les 5 s (ou devient zombie, compté arrêté, Hypothèse: à valider), alors la ligne et son point d'activité s'arrêtent, le pied de page affiche en Info `nginx (812) stopped`, et aucune seconde confirmation n'est demandée.
3. Étant donné un processus encore vivant 5 s après le SIGTERM, quand le délai expire, alors une seconde confirmation s'ouvre : `nginx (PID 812) did not exit within 5 s. Force kill (SIGKILL)? It gets no chance to shut down cleanly.` ; refuser laisse le processus en l'état sans nouveau signal ; confirmer envoie SIGKILL ; jamais de SIGKILL sans cette confirmation.
4. Étant donné l'attente en cours, quand l'utilisateur appuie sur `esc`, alors l'attente s'arrête (le SIGTERM déjà envoyé n'est pas rappelé), le pied de page dit `Stopped waiting for nginx (812) — the stop request was already sent`, et un tick de sonde arrivé après `esc` est ignoré (génération périmée) ; pendant toute l'attente, le tableau continue de se rafraîchir et les touches répondent.
5. Étant donné la seconde étape, quand l'identité du processus a changé depuis le SIGTERM (PID repris), alors aucun SIGKILL n'est envoyé et le message de S-01b critère 2 est affiché : le garde-fou s'applique à chaque signal.
6. Étant donné un SIGTERM refusé (EPERM) ou un processus déjà sorti, quand le signal est tenté, alors aucune attente n'a lieu et les messages existants s'affichent (`Refused by the system — DevDesk cannot signal PID N`, `PID N is no longer running`).
7. Étant donné un arrêt en attente, quand l'utilisateur appuie sur `K` sur une autre ligne, alors l'action est grisée avec `Another stop is already in progress` (Hypothèse: un seul arrêt en attente à la fois, à valider) ; elle se dégrise à la fin ou à l'annulation de l'attente.

**Contrainte de conception** : Rule 110 : `Update()` seul modifie le modèle ; l'attente est un tick (environ 250 ms) plus une sonde qui renvoient des messages portant une génération ; `esc` incrémente la génération.
**Hors périmètre** : Windows (S-02a, S-04) ; arrêt des enfants ; signaux au choix ; durée réglable ; avertissement sur les ancêtres.
**Hypothèses** :
- Hypothèse: à valider : 5 s fixe (Q2), sondée environ toutes les 250 ms. Pas de source ; Docker attend 10 s, Kubernetes 30 s. Choix de confort dans un TUI, à ajuster à l'usage (H4).
- Hypothèse: à valider : un zombie compte comme arrêté (il ne répond ni à SIGTERM ni à SIGKILL, il ne tient plus de socket ; le dire serait un confort).
- Hypothèse: à valider : un seul arrêt en attente à la fois (critère 7).
- Hypothèse: l'arrêt vise le seul PID listé, pas son groupe.
- Hypothèse: sous macOS, SIP peut refuser un signal même à root ; le refus du système est expliqué par le message existant.
**Hypothèse retenue, non encore validée** : Q2 (5 s fixe) ; Q6 (libellé `K` Kill → Stop une fois l'arrêt propre en place).
**Questions ouvertes** : aucune bloquante.

### S-03 — Spike : que voit-on des processus lancés par DevDesk ?
Epic : Arrêt sûr de processus — Priorité : Could — Effort : S (spike borné)

En tant que mainteneur de DevDesk, je veux savoir ce que `Children` de DevDesk montre pendant un scan natif, un scan `docker run` et un clone, afin de décider, avec des faits, s'il faut un écran des processus liés à DevDesk.

**Question précise** : lire les `Children` (gopsutil) du processus DevDesk pendant (a) un scan natif (Trivy ou Gitleaks), (b) un scan par `docker run`, (c) un clone git ; que voit-on (PID, nom, durée de vie) et à quel coût de lecture, sous Linux et Windows (macOS si disponible) ?

**Critères d'acceptation**
1. Étant donné les trois scénarios, quand le spike est mené, alors une note datée dans `docs/measurements/` donne pour chacun les processus enfants vus, leur nom et leur durée de vie, et dit explicitement si le client `docker` seul est vu (le conteneur étant celui du démon).
2. Étant donné la note, quand elle est rendue, alors elle propose un go ou un no-go sur S-03a à d (liste en lecture seule des descendants, arrêt sûr, lien workspace, lien port) avec la raison, et laisse la décision à l'utilisateur.
3. Étant donné un go de l'utilisateur, quand il est donné, alors seulement alors S-03a à d sont rédigées et créées dans Plane (aucun travail d'implémentation avant).

**Durée fixée** : 1 jour au plus (Hypothèse: à valider).
**Hors périmètre** : tout code livré ; S-03a à d ; liste de tous les processus.
**Hypothèses** : H6 et H7 du brief (DevDesk ne conserve aucun PID aujourd'hui, `internal/jobs` ne stocke qu'un état et un `CancelFunc` ; la valeur d'un écran face à `:jobs`, Forward, la vue conteneurs et l'onglet Ports n'est pas démontrée).
**Si go, pistes de découpage** : S-03a lister les descendants de DevDesk en lecture seule ; S-03b y ajouter l'arrêt sûr de S-01 et S-02 ; S-03c les processus liés à un workspace (répertoire courant) ; S-03d les processus liés à un port (jointure avec l'onglet Ports). Reste ouvert, seulement en cas de go : piste A (PID notés au lancement) ou B (descendants de DevDesk) ; un orphelin fait-il partie du besoin ?
**Questions ouvertes** : go ou no-go après le spike (décision de l'utilisateur).

Rappel des faits qui motivent le spike (lus dans le code, 2026-11-22) : aucun PID n'est conservé par le registre des jobs ni par un site de lancement du TUI ; les scans (`internal/scan`) sont annulables par `:jobs` (le contexte fait tuer le seul enfant direct, un `docker run` peut laisser le conteneur), le clone (`internal/git/ops.go`) ne l'est pas, les port-forwards (`internal/forward`) ne sont pas des processus (ils ont le PID de DevDesk, que S-01a interdit d'arrêter), les terminaux et IDE détachés ne sont pas suivis (`launchDetachedCmd`).

### S-04 — Demander la fermeture d'une application à fenêtre sous Windows avant de forcer
Epic : Arrêt sûr de processus — Priorité : Could (voire Won't, voir ci-dessous) — Effort : M
Dépend de : S-02b (attente et seconde confirmation) et S-02a.

En tant que développeur sous Windows qui arrête une application à fenêtre depuis l'onglet Ports, je veux qu'une demande de fermeture soit envoyée à ses fenêtres avant l'arrêt forcé afin que l'application puisse enregistrer ce qu'elle a ouvert.

**Critères d'acceptation**
1. Étant donné un processus Windows avec au moins une fenêtre de premier niveau dans la session de l'utilisateur, quand l'utilisateur confirme, alors une demande de fermeture est envoyée à ses fenêtres (équivalent de `taskkill` sans `/f`) puis l'attente et la seconde confirmation de S-02b (5 s, `Force terminate?`) s'appliquent.
2. Étant donné un processus sans fenêtre (serveur en console, service), quand l'utilisateur appuie sur `K`, alors le comportement de S-02a s'applique (arrêt forcé direct, libellé honnête).
3. Étant donné une application qui ignore la fermeture ou affiche une boîte « enregistrer ? » sans réponse, quand 5 s passent, alors la seconde confirmation est proposée et rien n'est forcé sans elle.
4. Étant donné un processus d'une autre session (service, autre utilisateur), quand l'utilisateur confirme, alors on se comporte comme s'il n'avait pas de fenêtre : critère 2.

**Hors périmètre** : `CTRL_BREAK` pour un enfant lancé par DevDesk ; arrêt des enfants.
**Hypothèses** : Hypothèse: `WM_CLOSE` est ignoré par une application console ou bloquée et limité à la session ; la plupart des processus qui tiennent un port n'ont pas de fenêtre (H5), donc la valeur pour le cas d'usage Ports est faible. Hypothèse: l'énumération des fenêtres d'un PID passe par `x/sys/windows`, déjà dépendance directe (Rule 303).
**Décision** : Q4 (repoussée, Could). Proposition du pm : la basculer en Won't si, un mois après S-02a, l'utilisateur n'a pas rencontré le cas d'une application à fenêtre arrêtée depuis Ports ; à trancher par l'utilisateur.

---

## Correspondance Plane

Module : « Arrêt sûr de processus » (projet DEVDESK, créé le 2026-11-22 ; six work items, label `story`, état Backlog). Priorité Plane : Must = high, Could = low. Relations non disponibles sur cette édition : les dépendances sont écrites dans les descriptions.

| Story | Work item | Priorité | Effort | Dépend de |
|---|---|---|---|---|
| S-01a | DEVDESK-708 | Must (high) | S | — |
| S-01b | DEVDESK-709 | Must (high) | M | S-01a |
| S-02a | DEVDESK-710 | Must (high) | S | livré avec S-01a |
| S-02b | DEVDESK-711 | Must (high) | M | S-01b |
| S-03 (spike) | DEVDESK-712 | Could (low) | S | après S-01 et S-02 |
| S-04 | DEVDESK-713 | Could (low) | M | S-02a, S-02b |

## Won't / plus tard

| Élément | Statut | Pourquoi |
|---|---|---|
| Gestionnaire de tâches complet (tous les processus, tri, recherche) | Won't (décision de l'utilisateur, 2026-11-22) | Valeur non démontrée face à htop, btop, bottom, Gestionnaire des tâches ; dérive de périmètre |
| CPU et mémoire par processus | Won't | Hors périmètre sans le gestionnaire complet |
| Arbre de processus et arrêt des enfants | Won't pour cet epic | Non-objectif décidé ; `Process.Kill` ne tue pas les enfants |
| Signaux au choix (SIGHUP, SIGSTOP, SIGUSR1...) | Won't | Non-objectif décidé |
| Élévation automatique des droits | Won't | Non-objectif décidé |
| Contrôle des processus de l'hôte Windows depuis WSL2 | Won't | Non-objectif provisoire du brief |
| Durée d'attente réglable | Plus tard | 5 s fixe retenu (Q2, non encore validé) |
| Protection des ancêtres plus hauts que le parent direct | Plus tard | Hypothèse retenue, non encore validée (Q1) |
| Détection d'un zombie pour le dire | Plus tard | Le zombie est compté arrêté (S-02b) ; le dire serait un confort |
| Arrêt propre Windows pour un processus sans fenêtre | Impossible en général | `CTRL_BREAK` n'atteint qu'un groupe créé par DevDesk ; ni `Interrupt` ni gopsutil `SendSignal` ne sont implémentés sous Windows |

## Questions ouvertes (ce que seul l'utilisateur peut trancher)

1. **D-D** : sous Windows, `Refused by the system` quand le handle est refusé (recommandation de l'architect). Valider ; et dire si la raison grisée au listing (`Cannot verify which process this is`) doit aussi nommer la cause réelle (accès refusé) quand elle est connue.
2. **Q1** : parent direct seulement refusé (retenu par défaut), et protéger le parent illisible par prudence (S-01a).
3. **Q2** : 5 s fixe, non configurable (S-02b).
4. **Q6** : `K` « Kill » devient « Stop » une fois S-02b livrée.
5. **Q8** : le jugement de l'utilisateur est la seule mesure de succès.
6. **S-02b** : zombie compté arrêté ; un seul arrêt en attente à la fois.
7. **S-03** : go ou no-go après le spike ; **S-04** : Could ou Won't.
8. **S-01b** : seuil de coût de lecture d'identité (à fixer par mesure).
