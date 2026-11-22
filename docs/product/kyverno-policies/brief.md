# Brief — kyverno-policies

Plane : DEVDESK-10 (`[§3.85] Kyverno : appliquer les politiques propres de l'organisation aux manifestes Kubernetes`, **Backlog**, recadré le 2026-11-22 sur les politiques d'organisation, non cadré). Le spike S0 est DEVDESK-685.
Source : `docs/backlog.md` §3.85 (première moitié ; la seconde, ce que kube-linter aurait vu, est DEVDESK-11 et reste hors de ce brief).

## Statut (2026-11-22)

**Le spike S0 (DEVDESK-685) est terminé : verdict NO-GO pour le jeu standard `restricted` de Kyverno.** Note de mesure : [`docs/measurements/2026-11-22-kyverno-apply.md`](../../measurements/2026-11-22-kyverno-apply.md). Mesuré avec Kyverno 1.19.2 et Trivy 0.76.2 sur 4 dépôts publics (dont un volontairement vulnérable) ; Kyverno 1.20 et la version du cluster ne sont pas mesurés.

- Recouvrement avec Trivy **97,7 %** (899 couples sur 920 ; seuil de no-go : 85 %) : Trivy voit déjà l'essentiel. 21 couples résiduels sur 10 documents et 5 politiques, 9 vrais positifs sur 10 vérifiés.
- Hors cluster, le moteur tient : 38/38 verdicts justes (228/228 sur six types de charge).
- Mais : des **faux verts silencieux** (politique au YAML mal formé, répertoire de politiques vide ou inexistant : code 0, rien sur stderr), un rapport **sans fichier, sans ligne et sans champ `rule`**, un rattachement à un fichier et une ligne de 73,1 % (98,9 % avec un run par fichier ; 45,3 % en rendu Helm/Kustomize).

**Décision de l'utilisateur (2026-11-22)** : DEVDESK-10 reste en Backlog, **recadré sur les politiques propres de l'organisation** (S7 de `stories.md`) ; le jeu standard est couvert par Trivy et les stories d'implémentation S1 à S6 sont non retenues après le no-go. S7 est la seule piste restante ; ses prérequis (un jeu réel, le recouvrement avec Trivy mesuré sur ces politiques, la version de Kyverno du cluster, le risque des faux verts silencieux) sont dans `stories.md`.

Le reste de cette page est l'état du brief avant le run, conservé pour mémoire ; les passages qui ne valent plus sont signalés.

## Idée

DevDesk applique des politiques Kyverno aux manifestes Kubernetes d'un dépôt scanné, pour faire respecter des règles que Trivy et kubeconform ne connaissent peut-être pas. **Premier objectif (2026-11-22) : vérifier en strict avec les jeux de politiques standard fournis par Kyverno** (dépôt `kyverno/policies`, profil `restricted` des Pod Security Standards) ; **écarté le même jour par le no-go du spike S0** (voir Statut). **Piste restante : les politiques propres à l'organisation** (S7), non cadrée.

## Problème

§3.80 a écarté Kyverno pour ne pas multiplier les outils : `kyverno apply` n'embarque aucune politique, ce n'est donc pas un catalogue de plus. Aujourd'hui, DevDesk scanne les manifestes avec Trivy (sécurité, dont des contrôles `KSV-*` proches des Pod Security Standards) et kubeconform (validité de schéma). Le jeu standard `restricted` de Kyverno recoupe vraisemblablement une grande partie de ces `KSV-*` : la question n'est plus « l'organisation a-t-elle des règles que personne n'applique », mais « le jeu standard strict voit-il des violations que Trivy ne voit pas déjà ». Si la réponse est non, l'étage n'apporte rien et c'est un résultat acceptable.

## Décisions (validées avec l'utilisateur)

- Source des politiques : **un répertoire local** (réglage envisagé `scan.kyverno_policies`). L'étage ne tourne que s'il est renseigné. Pas de dépôt git ni d'URL dans ce jalon.
- **Provenance (précisée le 2026-11-22)** : l'utilisateur **clone lui-même** `kyverno/policies` dans un répertoire et renseigne `scan.kyverno_policies` ; DevDesk ne livre ni ne télécharge rien (non-objectif maintenu).
- **Premier objectif (2026-11-22)** : le jeu standard de Kyverno en strict. « Strict » = profil **`restricted`** des PSS, au-dessus de `baseline`. **Mesuré et écarté (no-go de S0)** : Trivy couvre 97,7 % des violations. Les politiques d'organisation (S7 de `stories.md`) sont la piste restante et le cadre de DEVDESK-10.
- Les décisions suivantes (répertoire local, provenance, affichage, gravité, périmètre) étaient prévues pour l'étage du jeu standard ; elles ne sont plus engagées après le no-go et ne se reprennent qu'avec S7, à réévaluer sur un jeu réel (la gravité de la politique ne différencie rien dans le jeu standard : les 19 politiques sont `medium`).
- Affichage : les violations sont des findings de la catégorie **Misconfiguration**, onglet Misconfigurations existant, colonne Source `kyverno` (même décision que pour kubeconform, §3.80).
- Gravité : celle de la politique (annotation `policies.kyverno.io/severity`) ; **Medium** par défaut si elle est absente.
- Périmètre : **jalon 1 = manifestes bruts** (ceux que `k8s.Discover` trouve) ; **jalon 2 = rendu Helm et Kustomize**, partagé avec l'étage `k8s-schema` plutôt que refait.
- Les règles `mutate` ne servent pas de correctif (elles réécrivent la ressource entière, réserve de §3.78).

## Le jeu standard visé (constat web du 2026-11-22, non remesuré)

Reconfirmé sur le clone par le critère 1 de S0 (note de mesure : dossier `pod-security-vpol/`, 12 + 7 = 19 politiques, toutes `ValidatingPolicy` en `policies.kyverno.io/v1`, toutes de gravité `medium`, `release-1.19` portant encore la variante `ClusterPolicy` dans `pod-security/`) ; la ligne ci-dessous est le constat web d'origine ; sources : `https://github.com/kyverno/policies`, `https://kyverno.io/docs/guides/pod-security/`.

- Sur `main`, le dossier est `pod-security-vpol/` (sous-dossiers `baseline/` et `restricted/`, plus `EXCEPTIONS.md`). L'ancien chemin `pod-security/restricted` renvoie une 404 sur `main`.
- `restricted/` : 7 politiques (`disallow-capabilities-strict`, `disallow-privilege-escalation`, `disallow-proc-mount-strict`, `require-run-as-non-root-user`, `require-run-as-nonroot`, `restrict-seccomp-strict`, `restrict-volume-types`). `baseline/` : 12 d'après son `kustomization.yaml` (nombre à vérifier). Le profil `restricted` des PSS incluant `baseline`, le jeu strict est de l'ordre de **19 politiques (à vérifier)**.
- **Variante CEL** : `main` ne livre que du CEL en `policies.kyverno.io/v1` (`ValidatingPolicy`, `validationActions: [Audit]`, gravité par annotation, `minversion` 1.19.1 sur la politique lue).
- **Variante héritée `ClusterPolicy`** : le README de `main` dit que les exemples `ClusterPolicy`, `Policy` et leurs équivalents de nettoyage ont été retirés. La documentation et le site de Kyverno mentionnent encore des chemins `pod-security` et `pod-security-cel` ; quel ref du dépôt porte encore la variante héritée n'est pas établi (aucune release affichée) : à vérifier. La contradiction de Kyverno 1.20 sur les `ClusterPolicy` (rejet ou simple avertissement) devient donc une question de compatibilité de version et de cluster plus ancien, plus un risque pour l'utilisateur tant qu'il utilise `main`.
- Les politiques lues utilisent une variable CEL `exceptions.allowedImages` ; son évaluation par le CLI hors cluster est à mesurer.

## Points à vérifier (faisabilité)

Résultat du spike S0 (Kyverno 1.19.2) : le rapport JSON ne donne **ni fichier, ni ligne, ni champ `rule`** (ressource par kind, namespace, nom seulement) ; politique invalide, aucun manifeste, répertoire vide : voir le critère 8 de la note (faux verts silencieux pour une politique mal formée et un répertoire de politiques vide ou inexistant) ; le mode d'exécution et la table des outils n'ont pas été examinés (pas de code écrit). Liste d'origine, avant le run :

- Un rapport Kyverno (`--policy-report --output-format json`) donne-t-il un numéro de ligne ? Sinon, comment le retrouver (pointeur de ressource → ligne, comme `internal/k8s` le fait pour kubeconform) ?
- Le mode d'exécution de l'outil (`auto | binary | image`) et son entrée dans la table des outils (`internal/scan/toolbox.go`, §3.86) : quelle catégorie, quelle dépendance, quel signalement s'il est absent.
- Comportement sur politique invalide, aucun manifeste, politique de type `generate` ou `verifyImages` (peu probable dans le jeu `restricted`, mais pas exclu).

## Risques identifiés par la critique

Hypothèses à tester d'abord (le spike S0 de `stories.md` les a mesurées le 2026-11-22 ; résultat entre parenthèses, détail dans la note de mesure) :

- **A'. Valeur au-delà de Trivy (remplace l'ancienne hypothèse A, « l'utilisateur a un jeu d'organisation non appliqué ailleurs »).** Le jeu standard `restricted` apporte des violations que Trivy (`KSV-*` et ses contrôles Pod Security) ne voit pas déjà. **C'est LE critère de valeur et le risque principal de no-go** : le profil `restricted` recoupe fortement les `KSV-*`. Mesure : mêmes manifestes, mêmes ressources, taux de recouvrement O et valeur résiduelle (seuils T1 et T2 de S0). Un no-go « Trivy voit déjà l'essentiel » est un résultat acceptable. **(Mesuré : O = 97,7 %, seuil de no-go 85 % : no-go.)**
- **B. Verdict fiable hors cluster. (Mesuré : 38/38 verdicts justes ; mais faux verts silencieux sur politique mal formée et répertoire vide ou inexistant.)** Les politiques donnent un verdict fiable sans cluster. Le risque est plus faible qu'avec un jeu d'organisation (les politiques lues ne semblent pas utiliser `apiCall`, `configMap`, `request.*`), mais la variable `exceptions` n'est pas testée.
- **C. Rattachement sans ambiguïté. (Mesuré : 73,1 % avec un run par dépôt, 98,9 % avec un run par fichier, 45,3 % en rendu Helm/Kustomize.)** Une violation se rattache à un fichier et une ligne (base + overlays Kustomize, rendu Helm), alors que le rapport ne porte que des références d'objet.

Autres points relevés :

- Version : le jeu `main` annonce une version minimale de Kyverno 1.19.1 ; Kyverno 1.20 n'exécuterait plus les `ClusterPolicy` d'après la documentation (le code et la doc se contredisent ; **non mesuré par S0**, faute de binaire 1.20 : sur 1.19.2, une `ClusterPolicy` charge avec un avertissement sur stdout) ; le backlog §3.85 cite 1.19.1. Une newsletter dit 1.20.0 étiquetée le 12 novembre, sans note de version officielle trouvée.
- Format JSON du rapport non vérifié : champs `severity`, `rule`, `resources`, `message` ; un document JSON par rapport. Si toutes les politiques du jeu portent la même gravité, la règle « gravité de la politique » ne différencie rien.
- Politique invalide et répertoire vide : codes de sortie et messages inconnus ; risque de faux vert.
- Gravité `Audit` / `Enforce` ignorée ; valeurs de `policies.kyverno.io/severity` divergentes selon les sources.
- Doublons avec les `KSV-*` de Trivy : désormais le sujet central, plus un détail.
- Règles autogen : les politiques CEL lues listent explicitement `Deployment`, `CronJob`, etc. ; à confirmer.
- Version du CLI à aligner avec celle du cluster.

## Décision : spike d'abord (2026-11-22, précisée le même jour)

L'utilisateur a décidé de **faire d'abord un spike de mesure** avant toute story d'implémentation : une demi-journée (à valider), livrable `docs/measurements/<date>-kyverno-apply.md`, qui se termine par une décision **go / no-go / pivot (Trivy + contrôles Rego)** selon des seuils validés avant le run. **Précision :** le spike mesure le jeu standard `restricted` de `kyverno/policies`, cloné par l'utilisateur, et non plus un jeu d'organisation ; le recouvrement avec Trivy est le critère de valeur, et un no-go motivé par ce recouvrement est un résultat acceptable. Détail, critères et seuils révisés : story S0 de `stories.md`. Les stories d'implémentation (jalons 1 et 2, gravité, doublons) ne devaient être écrites qu'après un go ; leurs pistes sont listées en fin de `stories.md`. Entrées à fournir par l'utilisateur : où cloner et quel ref, la version du CLI à mesurer, les dépôts de test, qui lance les commandes et sur quelle machine. Il n'avait plus de jeu de politiques à fournir.

**Résultat (2026-11-22) : le spike est terminé, verdict no-go** (voir Statut en tête de page). Aucune story d'implémentation du jeu standard n'est écrite ni créée ; S1 à S6 sont notées « non retenues après le no-go » dans `stories.md`. Les politiques d'organisation (S7) sont la piste restante et rouvrent la question de la valeur au-delà de Trivy sur un jeu réel.

## Non-objectifs

- Pas de téléchargement de politiques (git, URL) par DevDesk, pas de catalogue livré avec DevDesk : l'utilisateur fournit lui-même ses politiques (maintenu).
- Pas de jeu standard de Kyverno dans DevDesk : écarté par le no-go de S0 (Trivy le couvre). Les politiques propres à l'organisation (S7) sont la piste restante, non cadrée.
- Pas de correctif automatique à partir de `mutate`.
- Pas de contrôles croisés entre objets ni de sondes liveness/readiness (DEVDESK-11).
- Pas de client de cluster : DevDesk lit des fichiers.
