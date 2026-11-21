# Brief — CI avec de vrais tests en moins de 10 minutes

> Statut : stories et faisabilité validées, backlog en relecture (voir `stories.md` et `feasibility.md`).
> Dossier renommé depuis `dagger-ci-datomir` après le pivot décrit plus bas.
> Pièces jointes : `research.md` (recherche sur Dagger, runners, sécurité), `measures.md`
> (mesures du 2026-11-21 sur la durée des tests) et `feasibility.md` (faisabilité rapide de l'architect).

## Vision en une phrase

Rendre les tests de DevDesk assez rapides pour qu'une CI complète, tests `-race` inclus,
tourne en moins de 10 minutes sur les runners GitHub-hosted gratuits du dépôt public, sans
aucun code externe exécuté sur datomir.

## Problème

- Aujourd'hui les tests sont désactivés en CI (décision du mainteneur D149, car trop lents).
  Le job `Test` de `.github/workflows/ci.yml` est un `echo` vert, conservé uniquement parce que
  le ruleset `main-protection` exige un check nommé `Test`. Une PR peut donc être fusionnée
  sans qu'aucun test n'ait tourné côté serveur : tout repose sur `mise run ci` lancé à la main.
- `mise run ci` exécute les tests deux fois (`test` puis `test-race`) : environ 632 s à chaud
  sur 22 cœurs, 832 s sur 2 cœurs, soit les « environ 15 minutes » ressenties.
- Cause mesurée (voir `measures.md`) : le goulot n'est ni la compilation ni `-race`. Le paquet
  `internal/app` occupe 99,5 % du temps mur (302 s sans race, 327 s avec) par des attentes
  réelles (`tea.Tick` dans `testutil.Msgs`), pas du calcul. Trois tests de thème en font 131 s
  à eux seuls (cas de 10,09 s chacun ; origine diagnostiquée, voir « Faisabilité »). 30 tests de plus d'1 s
  pèsent 280 s ; les quelque 675 autres tests, environ 21 s. Aucun test n'appelle `t.Parallel()`.

## Personas

- **Le mainteneur unique** (utilisateur principal) : veut un signal de CI fiable avant de
  fusionner, et une boucle locale (`mise run ci`) supportable.
- **Les agents IA** (orchestrateur et sous-agents lancés par le mainteneur) : lancent les tests
  en local, dans des worktrees ; tout gain de durée réduit leurs temps d'attente.
- **Un contributeur externe éventuel** (aucun aujourd'hui) : ses PR doivent passer par les runners
  GitHub-hosted, jamais par du matériel du mainteneur.

## Solutions existantes (voir `research.md`)

- Runners GitHub-hosted standard : gratuits et illimités sur dépôt public (4 vCPU, 16 Go),
  isolés de datomir. Cache Go possible via `actions/cache` / `setup-go` (quota 10 Go par dépôt).
- `go test -race` couvre `go test` : la double exécution n'apporte rien de plus en détection.
- Découpage par paquet : ne peut pas descendre sous le plafond d'`internal/app` tant que ce
  paquet reste un bloc de 302 s.
- Dagger, ARC, Depot, Earthly : étudiés puis écartés ou reportés (historique ci-dessous).

## Historique des pivots

1. **Idée initiale** : un module Dagger identique en local, en CI et pour les agents IA, exécuté
   sur des runners self-hosted dans k0s (datomir) avec un moteur Dagger persistant.
2. **Recherche** (`research.md`) : Dagger n'accélère rien par lui-même, il apporte un cache
   persistant ; aucun benchmark public d'un moteur Dagger persistant sur des tests Go `-race` ;
   Dagger Cloud n'a pas de palier gratuit décrit sur sa page de prix actuelle (Starter à 20 $/mois) ;
   le moteur Kubernetes tourne privilégié dans tous les exemples officiels ; GitHub déconseille les
   runners self-hosted sur dépôt public (« almost never ») ; le compte `anthnel` est de type User,
   donc pas d'organisation ni de groupes de runners pour restreindre l'accès par dépôt.
3. **Mesure** (`measures.md`) : un seul paquet, `internal/app`, domine par des attentes réelles ;
   la compilation instrumentée ne pèse que 30 s (22 cœurs) à 116 s (2 cœurs). Un cache de
   moteur, quel qu'il soit, ne corrige pas une attente de 10 s dans un test.
4. **Pivot validé** : réparer les tests, puis rendre le job `Test` réel sur GitHub-hosted. Dagger et
   datomir sont écartés pour ce besoin : les preuves ne les justifient pas, et un moteur privilégié
   sur un dépôt public, partagé avec des agents IA, est un risque de sécurité sans bénéfice mesuré.

## Hypothèses à valider

| # | Hypothèse | Comment la valider |
|---|-----------|--------------------|
| H1 | Les 10,09 s des cas de thème viennent d'un délai réel (attente d'une réponse de terminal ou timeout) qu'une horloge ou un délai injectable supprime sans perdre la couverture du comportement | **Validée sous une autre forme** : le délai est réel, mais c'est le rafraîchissement de 10 s du dashboard (vue de départ), pas le thème ; aucun seam de production nécessaire (S-01) |
| H2 | Les 30 tests de plus d'1 s d'`internal/app` attendent pour la même raison (délais réels) | **Invalidée** : cinq causes distinctes (A à E, plus F en calcul), d'où S-01, S-02a, S-02b, S-02c |
| H3 | `internal/app` descend sous 90 s avec `-race` après S-01 et S-02a | Mesure locale, consignée (projection de l'architect : environ 84 s après S-01 seule, 63 s avec S-02a) |
| H4 | Un runner GitHub-hosted 4 vCPU exécute `go test -race ./...` en moins de 10 min une fois `internal/app` réparé. **Estimé, non mesuré** : les autres paquets en race pèsent environ 730 s d'`Elapsed` cumulé sur 22 cœurs ; leur parallélisme réel sur 4 cœurs est inconnu (estimation de l'architect : 4 à 7 min de tests plus 1 à 2 min de mise en place) | Pré-contrôle local `taskset -c 0-3` (avant S-06), puis S-06 (PR jetable) ; plan B : S-08a puis S-08b |
| H5 | Supprimer le run sans `-race` ne fait perdre aucune couverture (pas de test réservé à l'absence du détecteur) | Inventaire dans S-04 (fichiers `//go:build !race`, tests qui testent `race.Enabled`) ; l'architect n'en a trouvé aucun |
| H6 | Le cache Go de GitHub Actions tient dans le quota de 10 Go avec un `GOCACHE` instrumenté (1,4 Gio mesurés en local) ; le cache n'est sauvegardé que depuis `main` | S-05, puis S-06 |
| H7 | Le réglage d'approbation des workflows de PR externes est au niveau par défaut ou plus strict | Vérification dans S-06 |

## Périmètre MVP

1. **Réparer `internal/app`** : supprimer les attentes réelles par cause, côté tests uniquement
   (S-01 : dashboard de la vue de départ ; S-02a : timer de footer ; S-02b et S-02c en options).
   `t.Parallel()` est une option (S-03, Could, seuil de 15 s). Cible : `internal/app` sous 90 s
   avec `-race`. `testutil.Msgs` n'est pas modifié.
2. **Ne plus exécuter les tests deux fois** : un seul run `-race` dans `mise run ci` et en CI.
3. **Rendre le job `Test` réel** : remplacer l'`echo` par `mise run test-race`, exécuté sur toute PR
   (y compris docs-only, le court-circuit docs-only reste réservé à `Lint and build`), avec cache Go
   (sauvegardé depuis `main` seulement), timeout de 15 min et le même nom `Test` pour le ruleset
   `main-protection`. Cela remplace la décision D149.
4. **Valider sur un vrai runner** par une PR jetable, avec mesure chiffrée consignée dans
   `docs/measurements/`.

## Non-objectifs

- Dagger, runners self-hosted sur datomir, actions-runner-controller, Dagger Cloud.
- Agents IA sur k0s via un moteur partagé.
- Restreindre les PR externes (on vérifie seulement le réglage d'approbation par défaut).
- Tests sur Windows et macOS.
- Optimiser `internal/scan` (145 s en race), `internal/ui/oci_resources` (124 s) et
  `internal/agentusage` (100 s), sauf si la CI dépasse encore 10 minutes (stories conditionnelles
  S-08a, puis S-08b).
- Modifier `testutil.Msgs` (518 appels dans 21 paquets).

## Métriques de succès

- Durée de la CI de bout en bout sous 10 min sur 5 runs GitHub-hosted consécutifs.
- `internal/app` sous 90 s en local avec `-race` (mesure de départ : 326,8 s).
- Zéro échec instable sur 10 runs consécutifs.
- `mise run ci` local : au plus la moitié des 632 s mesurées au départ pour les deux étapes de test sur
  22 cœurs à chaud (soit 316 s), à confirmer par la mesure de S-04.

## Risques

- **R1** : même réparé, `internal/app` laisse la CI au-dessus de 10 min parce que `-race` sur 4 vCPU
  est plus lent que prévu (H4). Parade : pré-contrôle `taskset -c 0-3`, puis S-08a et S-08b ; en dernier
  recours seulement, `-race` ciblé (hors MVP, décision de l'utilisateur).
- **R2** : une horloge injectable change le comportement de production ou masque un vrai défaut
  de séquencement. Parade : critères qui exigent que le comportement réel (délai de 10 s) reste
  couvert par au moins un test de la valeur configurée, et règle 110 (aucune modification d'état
  dans une `Cmd`).
- **R3** : `t.Parallel()` introduit de l'instabilité (HOME partagé, `fakekeyring.Install` global,
  variables d'environnement, ports). Parade : S-03 est Could, exclut les tests concernés, et exige
  10 runs sans échec avec `-race` et `-count=2`.
- **R4** : le job `Test` bloque les fusions à cause d'un test instable, ou fait attendre les PR de
  documentation (décision : `Test` s'exécute toujours, coût en durée accepté). Parade : critères de
  S-05 et S-06.
- **R5** : le cache Go instrumenté évince les autres caches du dépôt (quota 10 Go).
- **R6** : la perte de `-v` en CI rend un échec moins lisible. Parade : S-04 impose une sortie
  exploitable en cas d'échec.
- **R7** : un test réservé à l'absence du détecteur de race ne tourne plus nulle part (H5).
- **R8** : le garde `internal/ui/testutil/mise_ci_guard_test.go` est modifié par S-04 et par S-05 ;
  l'oublier casse `TestMiseCIRunsStepsSequentiallyInOrder` ou `TestMiseCIMatchesWorkflowSteps`. Parade :
  mise à jour dans le même commit que la tâche ou le workflow.
- **R9** : un routeur de test construit avec `testConfig()` brut réintroduit l'attente de 10 s sans bruit.
  Parade : commentaire sur `autoConfig` et garde de durée optionnel (S-02c).

## Faisabilité

Détail dans `feasibility.md` (architect, 2026-11-21). Verdict : **faisable**.

- **Cause réelle des 10 s** : ni `testutil.Msgs` ni le thème. Avec `app.theme: auto`, l'Init de la vue
  de départ est différé jusqu'à la réponse du terminal. La vue de départ par défaut est le dashboard,
  dont l'Init arme trois timers réels (1 s, 5 s et 10 s). Les tests qui vident la `Cmd` attendent le
  rafraîchissement de 10 s (`Status.RefreshInterval`, non raccourcissable par la config). Le timer de
  `tea.Tick` part à la construction de la `Cmd`, d'où 10,09 s et non 10,00 s. Elle touche 14 tests,
  environ 243 s sur les 327 s d'`internal/app` en race.
- **Correctif** : une ligne dans le helper de test `autoConfig` (`DefaultView = "workspaces"`), sans
  aucun changement de code de production. Prototype vérifié sur une copie jetable : 47 tests de thème
  verts en race, 8,7 s cumulées.
- **Gain estimé** pour `internal/app` en race (projection, sans recompiler) : 327 s vers environ 84 s
  après S-01, environ 63 s avec S-02a (timer de footer de 3 s), environ 47 s avec S-02b et S-02c.
  L'objectif de 90 s est atteint, mais la marge après S-01 seule est de 6 s.
- **Autres causes** (H2 invalidée) : timer de footer armé avant `FastTimers` (7 tests), détection des
  vrais scanners de l'hôte au démarrage (6 tests, résultat dépendant du poste), autres ticks et ping
  réel sur 127.0.0.1, calcul sous race (laissé tel quel).
- **Efforts** : toutes les stories sont S ou M, aucun L. S-08 est scindée en S-08a (`scan`,
  `oci_resources`) et S-08b (`agentusage`).
- **Inconnu restant** : H4, la durée de la suite complète en race sur 4 vCPU, non mesurée (estimation
  de 5 à 9 min avec la mise en place). Pré-contrôle local `taskset -c 0-3` avant S-06.
- **Points d'attention CI** : le garde `mise_ci_guard_test.go` (S-04 et S-05) ; le cache GOCACHE par
  `actions/cache` (que `mise-action` ne fournit pas), sauvegardé seulement depuis `main` ; le job `Test`
  sans `needs` pour ne pas sérialiser les jobs ; gcc sur `ubuntu-latest` à confirmer dans le journal.

## Idée reportée : Dagger

Un module Dagger exécuté à l'identique en local, en CI et par les agents IA, avec un moteur
persistant sur k0s, reste une piste valable pour deux besoins qui ne sont pas ceux du MVP :
**l'identité exacte entre l'exécution locale et la CI**, et **des agents IA sur k0s qui lancent
les tests dans un environnement partagé**. À réévaluer si l'un de ces deux besoins devient
bloquant. Un spike local antérieur existe : `docs/product/dagger-ci-release/` (2026-11-11 au
2026-11-20, verdict « non tranché », voir `note-decision.md`) ; ne pas refaire cette évaluation.
Avant de rouvrir : (1) refaire la mesure pour vérifier qu'un cache persistant apporterait
encore un gain une fois `internal/app` réparé ; (2) trancher la question du moteur privilégié
(rootless sous Kubernetes non documenté, voir `research.md`) ; (3) décider de l'isolation entre
la CI et les agents. Tracé par la story S-09 (Won't).

## Questions ouvertes

Aucune question bloquante : les décisions de l'utilisateur sont consignées dans `stories.md`
(section « Décisions prises »). Restent à trancher plus tard : livrer ou non le garde de durée de
S-02c, et la confirmation par la mesure du seuil local de 316 s.
