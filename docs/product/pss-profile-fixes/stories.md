# Stories — pss-profile-fixes

Décisions de l'utilisateur et faisabilité (architect, 2026-11-22) : voir `brief.md`,
sections « Décisions » et « Faisabilité ». Stories validées par l'utilisateur et créées dans
Plane le 2026-11-22 (module « Profil Pod Security Standards », S-01 = DEVDESK-686,
S-02 = DEVDESK-687, S-03 = DEVDESK-688, S-04 = DEVDESK-689 ; la dépendance S-04 → S-01 est
écrite dans la description, l'outil de relations de Plane étant indisponible).

Rappels qui valent pour toutes les stories :

- Manifestes Kubernetes bruts du catalogue uniquement (Helm et Kustomize restent refusés).
- Trivy est la seule source de findings. DevDesk ne dit jamais « conforme » : au mieux
  « aucune violation détectée par Trivy pour <profil> ».
- Une correction par finding (comme aujourd'hui). Édition sur les octets, diff montré à la
  confirmation, jamais d'écriture avant confirmation, re-scan pour juger l'occurrence
  corrigée.
- Un correctif n'est proposé que s'il ne peut pas empêcher le démarrage du conteneur.
- Ordre de livraison conseillé : S-01, S-02, S-04, S-03.

## Epic : Profil Pod Security Standards sur les manifestes Kubernetes

### S-01 — Voir le profil PSS de chaque finding et régler le profil cible
Epic : Profil Pod Security Standards — Priorité : Must — Effort : M (table S + réglage S + onglet M ; haut de la fourchette M) — Plane : DEVDESK-686

En tant que développeur qui scanne un manifeste Kubernetes du catalogue, je veux voir pour
chaque finding Pod Security le profil (`baseline` ou `restricted`) qui l'exige, et choisir
un profil cible, afin de savoir quels findings comptent pour ce profil et de pouvoir
n'afficher que ceux-là.

**Critères d'acceptation**
1. Étant donné un contexte existant sans `scan.pss_profile`, quand je scanne un manifeste,
   alors le réglage vaut `all` et la liste des findings est identique à celle d'avant cette
   story (mêmes findings, même ordre, aucun masqué) ; le YAML du contexte n'est pas réécrit.
2. Étant donné un manifeste avec un conteneur `privileged: true` (KSV-0017) et un pod sans
   `seccompProfile` (KSV-0030), quand l'onglet Misconfigurations les affiche, alors KSV-0017
   porte l'étiquette `baseline`, KSV-0030 l'étiquette `restricted`, et un finding KSV hors
   Pod Security n'en porte aucune ; un finding dont l'ancre PSS n'est pas dans la table
   de DevDesk porte le profil lu dans Trivy et la mention « profil d'après Trivy, sans
   correctif ».
3. Étant donné `scan.pss_profile: baseline` et le filtre activé, quand l'onglet s'affiche,
   alors les findings Pod Security `restricted` sont masqués, les `baseline` restent, et
   tout finding KSV hors Pod Security reste affiché ; avec `restricted` et le filtre
   activé, les findings `baseline` et `restricted` sont tous affichés (`restricted` cumule
   `baseline`).
4. Étant donné un profil cible choisi, quand j'ouvre l'onglet, alors le filtre est
   désactivé (rien n'est masqué) ; une touche l'active et le désactive ; l'en-tête indique
   le profil cible, l'état du filtre et le nombre de findings masqués ; si le filtre actif
   ne laisse aucun finding Pod Security, l'en-tête dit « aucune violation détectée par
   Trivy pour <profil> » et jamais « conforme ».
5. Étant donné `scan.pss_profile: baseline` et deux findings KSV-0104, l'un sur un
   `seccompProfile: Unconfined` explicite, l'autre sur un seccomp simplement absent, quand
   le filtre est actif, alors seul le premier est retenu pour `baseline` ; le second
   n'est retenu que sous `restricted` (où il apparaît avec KSV-0030) ; KSV-0022 reste
   retenu sous le profil que Trivy lui donne, même s'il est plus strict que la norme.
6. Étant donné la vue de configuration, quand je règle `scan.pss_profile` sur `restricted`,
   alors la valeur est enregistrée dans le YAML du contexte ; avec une valeur hors
   `all | baseline | restricted` (saisie ou YAML édité à la main), la saisie est refusée
   en listant les valeurs valides, ou, au chargement, le réglage retombe sur `all` avec un
   avertissement au pied de page, sans rien masquer.
7. Étant donné un `scan_result` demandé au serveur MCP, quand un finding est Pod Security,
   alors il porte `pss_profile` (`baseline` ou `restricted`) ; un finding hors Pod Security
   n'a pas ce champ.

**Hors périmètre** : toute correction (S-02, S-03), les textes « ce qui manque » (S-04),
masquer ou supprimer un finding sans que l'utilisateur ait activé le filtre, le profil
`privileged`, lire dynamiquement les ancres de Trivy comme source unique (la table est
maintenue à la main, avec un test sur une sortie Trivy enregistrée), filtrer d'autres
checks que Pod Security.
**Hypothèses** :
- Hypothèse: à valider — le réglage est par contexte (YAML du contexte, nouveau champ sans
  migration, sur le modèle de `base_image_track`) et modifiable depuis la vue de
  configuration.
- Hypothèse: à valider — « le réglage marque et filtre, il ne masque rien » se lit ainsi :
  le filtre est un basculeur de l'onglet, désactivé à l'ouverture ; le réglage choisit le
  profil visé et le compte « masqués » de l'en-tête. L'alternative (filtre actif à
  l'ouverture, levable d'une touche) contredit plus nettement « ne masque rien ».
- Hypothèse: à valider — sous `all`, l'étiquette PSS est quand même affichée : c'est un
  ajout d'information, pas un filtre. Le brief disait « sans profil choisi, aucun
  changement observable » ; l'étiquette en est la seule entorse.
- Hypothèse: à valider — DevDesk sait distinguer, pour KSV-0104, un `Unconfined` explicite
  d'un seccomp absent (message de Trivy ou relecture des octets du manifeste). Fait non
  vérifié ; si impossible, critère 5 à renégocier (décision 3 de l'utilisateur).
- Conséquence du modèle : un finding reste affiché ou masqué selon son seul profil ; les
  correctifs (S-02, S-03) suivent l'existence du finding, pas l'état du filtre.
**Questions ouvertes** : filtre désactivé à l'ouverture (hypothèse ci-dessus) et étiquette
visible sous `all` : à confirmer.
**Dépendances** : aucune.

### S-02 — Corriger hostNetwork, hostPID et hostIPC
Epic : Profil Pod Security Standards — Priorité : Must — Effort : S — Plane : DEVDESK-687

En tant que développeur qui scanne un manifeste Kubernetes, je veux que DevDesk propose de
mettre `hostNetwork`, `hostPID` et `hostIPC` à `false` afin de lever en une confirmation
trois violations du profil `baseline`, sans éditer le fichier à la main.

**Critères d'acceptation**
1. Étant donné un manifeste avec `hostNetwork: true` (KSV-0009), `hostPID: true`
   (KSV-0010) ou `hostIPC: true` (KSV-0008), quand je demande la correction du finding,
   alors le diff de confirmation ne change que cette valeur de `true` à `false` ; les
   commentaires, ancres, fins de ligne et tous les autres octets sont conservés.
2. Étant donné ce même diff, quand il est affiché, alors il porte l'avertissement « cela
   inverse un choix explicite de l'auteur ; peut casser des agents de nœud », et rien
   n'est écrit tant que je n'ai pas confirmé ; si j'annule, le fichier est identique octet
   pour octet.
3. Étant donné un DaemonSet avec `hostNetwork: true`, quand je demande la correction,
   alors elle est proposée comme pour les autres types (pas de refus propre au DaemonSet),
   avec l'avertissement du critère 2.
4. Étant donné chacun des types Pod, Deployment, StatefulSet, DaemonSet, ReplicaSet, Job
   et CronJob, quand je corrige un de ces champs, alors c'est celui du pod spec du bon
   chemin (Pod `spec`, Deployment/StatefulSet/DaemonSet/ReplicaSet/Job
   `spec.template.spec`, CronJob `spec.jobTemplate.spec.template.spec`) qui est modifié.
5. Étant donné une correction confirmée, quand le re-scan s'exécute, alors l'occurrence
   corrigée est jugée corrigée ; si le même pod porte aussi `hostPID: true`, ce finding
   reste ouvert (une correction par finding).
6. Étant donné un pod spec partagé par un alias YAML, ou un champ qu'on ne peut pas
   localiser sans ambiguïté, quand je demande la correction, alors le raccourci est grisé
   avec la raison, comme pour les correctifs existants, et rien n'est écrit.

**Hors périmètre** : retirer la clé au lieu de la mettre à `false`, corriger plusieurs
champs en un seul diff, Helm et Kustomize, avertir sur l'usage réel du réseau de l'hôte
par la charge (DevDesk ne le sait pas).
**Hypothèses** :
- Hypothèse: à valider — l'avertissement du critère 2 s'affiche pour les trois champs et
  tous les types, pas seulement pour DaemonSet (la décision de l'utilisateur ne distingue
  pas).
- Hypothèse: à valider — les gardes de refus (alias, ambiguïté) sont celles de KSV-0017 et
  KSV-0001, réutilisées.
**Questions ouvertes** : aucune.
**Dépendances** : aucune ; l'étiquette `baseline` de ces findings arrive avec S-01.

### S-03 — Corriger un seccomp absent par RuntimeDefault
Epic : Profil Pod Security Standards — Priorité : Should — Effort : M — Plane : DEVDESK-688

En tant que développeur qui vise le profil `restricted`, je veux que DevDesk propose
d'ajouter `seccompProfile: RuntimeDefault` au niveau du pod quand aucun seccomp n'est défini
afin de lever KSV-0030 (et KSV-0104 sur un seccomp absent) sans éditer le fichier, et de
n'être refusé que lorsqu'un réglage existant rend l'ajout ambigu ou risqué.

**Critères d'acceptation**
1. Étant donné un pod sans `seccompProfile` ni sur le pod ni sur aucun conteneur (init et
   éphémères compris), quand je demande la correction de KSV-0030 ou de KSV-0104, alors le
   diff ajoute `securityContext.seccompProfile.type: RuntimeDefault` au niveau du pod spec
   du bon chemin (mêmes chemins que S-02), avec l'indentation du fichier et tous les autres
   octets conservés.
2. Étant donné un pod qui a déjà un `securityContext` en bloc (par exemple avec
   `runAsUser`), quand je demande la correction, alors la clé est ajoutée dans ce bloc sans
   toucher aux clés existantes.
3. Étant donné un des cas suivants : un conteneur (init ou éphémère inclus) ou le pod met
   `Unconfined` ou `Localhost`, une annotation seccomp est présente, le pod est Windows
   (`spec.os.name: windows`), le `securityContext` est écrit en ligne (`{...}`), ou le pod
   spec est partagé par un alias YAML, quand je demande la correction, alors le raccourci
   est grisé avec la raison précise du cas, et rien n'est écrit.
4. Étant donné le diff de confirmation, quand il s'affiche, alors il avertit qu'un
   `RuntimeDefault` peut casser à l'exécution une charge qui tournait sans filtre seccomp
   et que le re-scan ne le détecte pas ; rien n'est écrit sans confirmation.
5. Étant donné un pod à trois conteneurs sans seccomp, quand je confirme la correction d'un
   seul de leurs findings, alors le re-scan juge corrigées les occurrences des trois
   conteneurs (le réglage est au niveau du pod).
6. Étant donné un finding KSV-0104 sur un `Unconfined` explicite, quand je demande la
   correction, alors elle est refusée (cas du critère 3) et le texte renvoie à ce qu'il
   faut changer à la main (voir S-04).

**Hors périmètre** : `Localhost`, remplacer un `Unconfined` ou un `Localhost` existant,
seccomp au niveau du conteneur, pods Windows, annotations seccomp, `securityContext` en
ligne, valider l'effet à l'exécution.
**Hypothèses** :
- Hypothèse: à valider — le correctif est proposé tant que le finding existe, quel que
  soit le profil cible ou l'état du filtre (sous `baseline` le finding « absent » est
  hors profil, donc rarement vu).
- Hypothèse: à valider — réglage posé au niveau du pod pour couvrir aussi init et
  éphémères ; leurs `Unconfined` éventuels déclenchent le refus.
- Hypothèse: à valider — l'avertissement du critère 4 est voulu (issu du risque noté par
  l'architect, pas de décision explicite de l'utilisateur).
- Fait non vérifié : refus de validation de l'API pour un pod Windows (seccomp,
  `hostNetwork` exigé par HostProcess). Le refus du critère 3 est de prudence.
**Questions ouvertes** : aucune bloquante.
**Dépendances** : aucune ; priorité Should car le gain est le seul KSV-0030 / KSV-0104
« absent » et le risque d'exécution est le plus élevé des correctifs nouveaux.

### S-04 — Voir précisément ce qui manque quand aucune correction sûre n'existe
Epic : Profil Pod Security Standards — Priorité : Must — Effort : M — Plane : DEVDESK-689

En tant que développeur qui vise un profil, je veux que le refus grisé d'un finding sans
correction sûre me dise ce qui manque (champ et valeur attendue pour ce profil) afin de
corriger le manifeste moi-même sans chercher la règle ailleurs.

**Critères d'acceptation**
1. Étant donné un finding parmi KSV-0103, 0022, 0023, 0024, 0002, 0025, 0026, 0027, 0028,
   0012, 0105 et 0106, quand je tente la correction, alors le refus grisé existant énonce
   le champ à renseigner et la valeur attendue pour le profil du finding, dans un texte
   propre à cette règle (un test table-driven vérifie qu'aucune des douze règles n'a de
   texte vide ou générique).
2. Étant donné un tel finding, quand j'affiche le refus, alors aucun fichier n'est modifié
   et le raccourci reste grisé avec ce texte comme raison.
3. Étant donné le profil `restricted` et un finding qu'aucune story ne corrigera jamais
   (par exemple `runAsNonRoot` absent, KSV-0012), quand j'affiche le refus, alors le texte
   dit ce qui manque quand même, sans proposer de correction.
4. Étant donné un finding dont l'ancre PSS n'est pas dans la table (profil lu dans Trivy),
   quand j'affiche le refus, alors il dit que le profil vient de Trivy, que DevDesk n'a pas
   de correctif ni de texte pour cette règle et renvoie à la description du check dans
   Trivy ; aucun champ ni valeur n'est inventé.
5. Étant donné KSV-0022, quand j'affiche son texte, alors il dit que le check de Trivy est
   plus strict que le standard Pod Security ; le finding n'est pas filtré pour autant (S-01,
   critère 5).
6. Étant donné un finding qui a un correctif sûr (KSV-0017, KSV-0001, KSV-0008 à 0010,
   KSV-0030, KSV-0104 absent), quand je demande la correction, alors le texte « ce qui
   manque » n'est pas utilisé à la place ; c'est le diff de confirmation qui s'ouvre
   (ou, si S-03 ne s'applique pas, la raison de son refus).

**Hors périmètre** : proposer d'écrire ces correctifs, lister toutes les règles de Trivy
(seules les ≈14 sans correctif sûr de la table), traduire ou citer le texte de la norme
PSS en entier, valeur universelle pour `runAsUser`, limites de ressources ou capabilities.
**Hypothèses** :
- Hypothèse: à valider — ≈14 règles dans le périmètre ; l'architect en liste douze
  identifiées ici, les deux restantes à nommer à l'estimation du plan.
- Hypothèse: à valider — les textes sont rédigés en anglais dans l'UI (Rule 129), validés
  par relecture, pas par mesure.
- Hypothèse: à valider — KSV-0104 « absent » est couvert par S-03 ; avant S-03, son refus
  utilise un texte de cette story.
**Questions ouvertes** : aucune.
**Dépendances** : S-01 (table des règles et profil par `RuleKey`). Sans S-03, KSV-0030 et
KSV-0104 absent restent des refus avec texte.

## Won't / plus tard

- **Rendre le fichier conforme en un seul diff** (lot : toutes les corrections sûres d'un
  pod ou d'un fichier d'un coup) : Won't pour ce jalon, effort L. Reporter ; une correction
  par finding.
- **Helm et Kustomize** : toujours refusés ; aucune lecture de templates ni de overlays.
- **Corrections risquées** : `runAsNonRoot`, `readOnlyRootFilesystem`, `drop ALL`, limites
  de ressources, `runAsUser`, capabilities. Écartées exprès (§3.78, §3.80), même sous
  `restricted`.
- **Profil `privileged`** : n'impose rien, rien à corriger.
- **Kyverno** et toute autre source de findings : no-go du spike DEVDESK-685.
- **Table lue dynamiquement** depuis les `References` de Trivy sans table maintenue à la
  main : plus tard, quand les specs de conformité de Trivy couvriront KSV-0009 et
  KSV-0010.
- **Seccomp `Localhost`**, pods Windows et annotations seccomp : refusés (S-03), à
  reprendre si un besoin réel apparaît.
- **Réglage par scan** (au lieu de par contexte) : à reprendre si le besoin se confirme.
