# Brief — pss-profile-fixes

## Idée

Permettre de choisir un profil Pod Security Standards cible (`baseline` ou `restricted`)
comme réglage de scan, et que DevDesk (Trivy seul) filtre les findings Kubernetes sur ce
profil, propose des corrections automatiques sûres pour les manifestes bruts, et dise
précisément ce qui manque pour le reste, sans l'écrire.

## Problème

Aujourd'hui, sur un manifeste Kubernetes du catalogue, l'utilisateur voit des findings
`KSV-*` sans savoir lesquels sont requis par un profil Pod Security Standards, ni comment
s'y conformer. Le catalogue de correctifs (`internal/remediation/k8s.go`) ne couvre que
trois règles (KSV-0017 `privileged` → `false`, KSV-0001 `allowPrivilegeEscalation` →
`false` ou insertion, K8S-API-REMOVED), loin de ce qu'un profil demande.

Contraintes déjà décidées dans le code (`docs/architecture/scanning.md`, « Kubernetes
manifests in the catalog », §3.78 et §3.80) :

- l'édition se fait sur les octets (commentaires, ancres et fins de ligne conservés), avec
  le diff montré à la confirmation et un re-scan pour juger l'occurrence corrigée ;
- `runAsNonRoot`, `readOnlyRootFilesystem`, `drop ALL`, limites de ressources et seccomp
  ont été écartés exprès : ils satisfont la règle mais peuvent empêcher le démarrage du
  conteneur, ou n'ont pas de valeur universelle ;
- Helm et Kustomize sont refusés.

Le profil `restricted` cumule `baseline` et exige justement plusieurs de ces règles
risquées : la tension est là, d'où la décision de ne corriger que le sûr.

## Décisions validées (utilisateur, 2026-11-22)

- DevDesk garde Trivy seul. Kyverno est écarté : le spike DEVDESK-685 est un no-go, Trivy
  couvre déjà 97,7 % des violations du jeu standard
  (`docs/measurements/2026-11-22-kyverno-apply.md`).
- Corrections sûres seulement : uniquement celles qui ne peuvent pas empêcher le démarrage
  du conteneur (par exemple `hostNetwork`, `hostPID`, `hostIPC` à `false`, seccomp
  `RuntimeDefault` quand il est absent). Pour tout le reste, DevDesk dit précisément ce
  qui manque pour le profil, sans écrire.
- Le profil cible est un réglage de scan (`baseline` ou `restricted`) qui filtre les
  findings et les propositions de correction.

### Décisions du 2026-11-22 (après la faisabilité)

1. Le réglage MARQUE et FILTRE (étiquette PSS + filtre), il ne masque rien. Valeurs
   `all | baseline | restricted`, défaut `all` (aucun changement pour les contextes
   existants). Il ne concerne que les checks Pod Security, jamais les autres KSV.
   (Remplace la recommandation « absent par défaut » du brief initial.)
2. `hostNetwork`, `hostPID`, `hostIPC` → `false` est proposé PARTOUT, y compris pour un
   DaemonSet, avec un avertissement dans le diff de confirmation (« cela inverse un choix
   explicite de l'auteur ; peut casser des agents de nœud »), jamais écrit sans
   confirmation.
3. Sous `baseline`, KSV-0104 n'est retenu que sur un `Unconfined` explicite (un seccomp
   absent n'est signalé que sous `restricted`, KSV-0030). KSV-0022 : le dire dans le texte,
   sans filtrer.

Hypothèses reprises de l'architect, à valider : une correction par finding (comme
aujourd'hui), le « rendre conforme en lot » est Won't pour ce jalon ; le réglage est par
contexte (YAML) et modifiable depuis la vue de configuration ; les correctifs s'appliquent
aussi aux conteneurs init et éphémères quand le pod spec est concerné.

## Faisabilité (architect, 2026-11-22, faits vérifiés avec Trivy 0.76.2 sauf mention)

- Le profil d'un check vient de l'ancre `#baseline` / `#restricted` du lien PSS dans
  `References` de chaque finding Trivy. DevDesk jette `References` aujourd'hui
  (`internal/scan/trivy.go`, `TrivyMisconfiguration`). Les specs de conformité de Trivy
  omettent KSV-0009 et KSV-0010. Retenu : une table maintenue à la main (~20 entrées),
  indexée par `RuleKey` dans `internal/remediation`, avec un test sur une sortie Trivy
  enregistrée. Un finding dont l'ancre PSS est absente de la table est étiqueté « profil
  d'après Trivy, sans correctif ».
- Correctifs sûrs : privileged (KSV-0017) et allowPrivilegeEscalation (KSV-0001) existent
  déjà. Nouveaux : `hostIPC`/`hostNetwork`/`hostPID` → `false` (KSV-0008/0009/0010,
  effort S) ; `seccompProfile` `RuntimeDefault` au niveau du pod quand absent partout
  (KSV-0104 baseline, KSV-0030 restricted, effort M). Refus si un conteneur ou le pod met
  `Unconfined`/`Localhost`, si des annotations seccomp sont présentes, pour un pod Windows,
  un `securityContext` en ligne, ou un pod spec partagé par alias YAML. Chemin du pod spec :
  Pod `spec`, Deployment/StatefulSet/DaemonSet/ReplicaSet/Job `spec.template.spec`, CronJob
  `spec.jobTemplate.spec.template.spec`.
- Sans correctif sûr, DevDesk dit ce qui manque dans le refus grisé existant : KSV-0103,
  0022, 0023, 0024, 0002, 0025/0026/0027, 0028, 0012, 0105, 0106 (≈14 règles).
- Réglage `scan.pss_profile` : nouveau champ de configuration sans migration (modèle
  `base_image_track`) ; étiquette PSS et filtre dans l'onglet Misconfigurations et son
  en-tête ; projection `pss_profile` dans le MCP `scan_result`.
- Effort par étape : table (S), réglage (S), onglet (M), correctif host* (S), correctif
  seccomp (M), textes « ce qui manque » (M). « Rendre le fichier conforme en un seul diff »
  = L, reporté.

## Découpe (voir `stories.md`)

| Story | Contenu | Priorité | Effort |
|---|---|---|---|
| S-01 | voir le profil PSS de chaque finding, régler le profil cible (table, réglage, étiquette, filtre, en-tête, MCP) | Must | M |
| S-02 | corriger hostNetwork / hostPID / hostIPC | Must | S |
| S-03 | corriger un seccomp absent (RuntimeDefault au niveau du pod) | Should | M |
| S-04 | dire ce qui manque quand aucune correction sûre n'existe | Must | M |

## MVP

(Précisé par les décisions du 2026-11-22 ci-dessous : le réglage marque et filtre, il ne
masque rien.)

- Réglage de scan `scan.pss_profile` : `all | baseline | restricted`, défaut `all`
  (comportement actuel inchangé).
- Chaque finding Pod Security indique le profil qui l'exige ; un filtre de l'onglet
  Misconfigurations le restreint au profil cible.
- Les corrections nouvelles sont sûres seulement (hostNetwork, hostPID, hostIPC, seccomp),
  avec le mécanisme existant : octets conservés, diff à la confirmation, re-scan.
- Pour chaque finding requis par le profil mais sans correctif sûr, DevDesk affiche ce qui
  manque (le champ et la valeur attendue) sans rien écrire.
- Manifestes bruts uniquement, comme aujourd'hui.

## Non-objectifs

- Aucune correction risquée (`runAsNonRoot`, `readOnlyRootFilesystem`, `drop ALL`,
  limites de ressources, ou toute valeur qui peut empêcher le démarrage).
- Pas de Helm ni de Kustomize.
- Pas de Kyverno.
- Pas de profil `privileged` à corriger : il n'impose aucune restriction, rien à corriger.
- Pas de nouvelle source de findings : Trivy seul.

## Métriques de succès

- Avec un profil cible choisi, chaque finding affiché est requis par ce profil, et
  l'utilisateur voit lequel.
- Sur un manifeste de test non conforme, toutes les corrections sûres du profil sont
  proposées et, une fois acceptées, le re-scan juge les occurrences corrigées.
- Aucune correction proposée ne peut empêcher le démarrage d'un conteneur (vérifié par la
  revue de la liste des correctifs, pas par mesure).
- Chaque finding requis sans correctif sûr affiche ce qui manque, et aucun fichier n'est
  modifié pour lui.
- Sans profil choisi, aucun changement observable.

## Risques

- Libellé : DevDesk dit « aucune violation détectée par Trivy pour <profil> », jamais
  « conforme ». Trivy dévie de la norme (KSV-0104 et KSV-0022 plus stricts que le standard)
  et ne lit que les types de ressources qu'il connaît.
- Seccomp `RuntimeDefault` peut casser à l'exécution une charge qui tournait en `Unconfined` ;
  le re-scan ne le voit pas. Le diff de confirmation porte l'avertissement.
- `hostNetwork: false` peut casser des agents de nœud (DaemonSet compris, décision 2) ;
  une correction qui ne gêne pas le démarrage peut quand même casser le service. Le diff à
  la confirmation reste le garde-fou.
- Une table KSV → profil maintenue à la main peut dériver des versions de Trivy ; un test
  sur une sortie Trivy enregistrée et l'étiquette « profil d'après Trivy, sans correctif »
  limitent la dérive.
- Le profil `restricted` laissera beaucoup de findings « manque » sans correction, ce qui
  peut décevoir par rapport à l'attente d'une conformité automatique.
- Faits non vérifiés : refus de validation de l'API pour un pod Windows (seccomp,
  `hostNetwork` exigé par HostProcess) ; capacité de distinguer un `Unconfined` explicite
  d'un seccomp absent pour KSV-0104 (décision 3).

## Questions ouvertes tranchées

- Valeur par défaut du réglage : `all` (décision 1, remplace « absent »).
- Findings hors profil masqués ou relégués : le réglage ne masque rien ; il marque et
  offre un filtre ; les autres KSV ne sont jamais concernés (décision 1).
- `restricted` : montrer ce qui manque même pour les règles jamais corrigées : oui
  (S-04, critère 3).
- Conteneurs init et éphémères, types de gabarits de pod : couverts par un réglage au
  niveau du pod ; Pod, Deployment, StatefulSet, DaemonSet, ReplicaSet, Job, CronJob
  (faisabilité). Hypothèse : à valider pour init/éphémères.
- Portée du réglage : par contexte, modifiable depuis la vue de configuration. Hypothèse :
  à valider (reprise de l'architect).

## Questions ouvertes restantes

- Le filtre est-il désactivé à l'ouverture de l'onglet (lecture retenue de « ne masque
  rien ») ou actif et levable d'une touche ? Hypothèse retenue : désactivé.
- L'étiquette PSS est-elle visible sous `all` (information ajoutée) ou seulement sous un
  profil choisi (« aucun changement observable » à la lettre) ? Hypothèse retenue :
  visible.
- L'avertissement du diff pour host* vaut-il pour tous les types ou le DaemonSet
  seulement ? Hypothèse retenue : tous.
- L'avertissement « peut casser à l'exécution » de seccomp dans le diff est-il voulu ?
  Hypothèse retenue : oui.
- Faisabilité à confirmer par l'architect à l'estimation du plan : distinction `Unconfined`
  explicite / seccomp absent pour KSV-0104 ; nom des deux règles restantes des ≈14 sans
  correctif sûr.
