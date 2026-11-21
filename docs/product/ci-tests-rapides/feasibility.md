# Faisabilité — CI avec de vrais tests en moins de 10 minutes

Date : 2026-11-21. Auteur : architect (mode faisabilité rapide). Entrées : `brief.md`, `stories.md`,
`measures.md`, `research.md`. Aucun fichier du dépôt modifié. Les sondes ont tourné sur une copie
jetable du dépôt, hors du dépôt.

**Verdict : faisable.** La cause des 10 s est trouvée. Un correctif d'une ligne, dans un helper
de test, supprime environ 243 s sur les 327 s d'`internal/app` en race (prototype vérifié). Le
reste du travail est de la CI classique. Le seul vrai inconnu est H4 : la durée de la suite
complète en race sur 4 vCPU.

## 1. Cause des attentes dans `internal/app` (S-01 et S-02)

### Cause A : l'Init différé de la vraie vue de départ, le dashboard (14 tests, environ 243 s)

Ce n'est pas `testutil.Msgs` et ce n'est pas le thème. `Msgs` exécute de façon synchrone les
commandes qu'on lui passe. Le délai vient de l'une d'elles.

- `newWithSize` met `startInitDeferred = pending` (`internal/app/app.go:307`). Avec
  `app.theme: auto`, l'Init de la vue de départ est donc différé jusqu'à la réponse du terminal.
- `settleTheme` (`internal/app/theme_auto.go:215-226`) appelle `runDeferredStartInit`
  (`theme_auto.go:231-240`). La fonction renvoie `view.Init()` de la vue de départ. Avec
  `testConfig()` = `config.Default()`, `DefaultView` vaut `dashboard`.
- `dashboard.Model.Init` (`internal/ui/dashboard/model.go:212-228`) groupe trois timers réels :
  - `scheduleRefresh` (`model.go:655-663`) : `Status.RefreshInterval`, 10 s par défaut
    (`internal/config/config.go:626` et `:727`), avec un plancher (sous 10 s, la valeur devient
    30 s). La config ne permet donc pas de raccourcir ce timer ;
  - `scheduleHostTick` : 1 s ;
  - `scheduleDockerTick` : 5 s.

  Ce même lot fait aussi des E/S réelles : `docker` (stats, system df, image ls), lecture du disque
  et `du`.
- Les tests vident la `Cmd` renvoyée par `answer()`, `allPosts()` ou le timeout avec
  `testutil.Msgs`. Chaque cas attend donc le tick de rafraîchissement de 10 s.

**Pourquoi 10,09 s et non 10,00 s.** `tea.Tick` arme son timer quand la `Cmd` est construite, pas
quand elle s'exécute (`charm.land/bubbletea/v2@v2.1.1/commands.go:154-155`,
`t := time.NewTimer(d)` hors de la closure). Les trois ticks du lot (1 s, 5 s, 10 s) se
chevauchent, et l'attente vaut le plus long, 10 s. Les 0,05 à 0,09 s restants sont du travail
réel : construction du routeur, et E/S du lot exécutées en série après l'échéance.

**Preuves.**
1. Pile de goroutines (`-timeout 5s`) : `Tick.func1`, appelé par `testutil.Msgs`, appelé par
   `answer` (`theme_auto_test.go:91`).
2. Sonde sur une copie jetable : `RefreshInterval` passé de 10 à 13. Le cas
   `TestThemeAutoFollowsTheTerminalAnswer/dark_terminal,_defaults` passe de 10,0 s à **13,06 s**.
3. Ligne de base sur le dépôt : `TestThemeAutoCandidateThatCannotBeUsedGivesDefaultAndOneWarning`
   dure 30,14 s (3 cas de 10,05 s).
4. Prototype de correctif (copie jetable) : une ligne dans `autoConfig` (`theme_auto_test.go:31`),
   `cfg.App.DefaultView = "workspaces"`. Une vue dont l'Init ne porte aucun timer suffit.
   - Résultat : `go test -race -count=1` sur les 47 tests de thème donne 47 succès et 8,7 s
     cumulées.
   - Les trois tests de S-01 tombent à 0,04 s, 0,07 s et 0,02 s.
   - `autoRouter` et `autoRouterWith` passent tous deux par `autoConfig` : un seul correctif
     couvre les 14 tests.

Les 14 tests concernés :
- les trois de S-01 ;
- `TestThemeAutoTimeoutFallsBackToDarkSilently`, `TestChoosingAutoInSessionReusesTheCachedAnswerWithoutAskingAgain`,
  `TestANamedThemeChosenInSessionReplacesAutoAndIsKept`, `TestBackgroundColorMsgIsNotForwardedToTheView`,
  `TestThemeAutoAnswersOnceAndCachesTheFirst` ;
- `TestAnInvalidThemeLightAndALightAnswerWarnOnce`, `TestAnInvalidThemeDarkAndATimeoutWarnOnce`,
  `TestAnInvalidThemeDarkAndADarkAnswerWarnOnce`, `TestBothCandidatesInvalidWarnOnceEach` ;
- `TestAnAnswerThatChangesThePaletteRebuildsTheViews`, `TestAnAnswerThatChangesThePaletteKeepsTheConfigurationView`.

Correctif recommandé : côté test uniquement, sans aucun changement de production. Un seam de
production (variable exportée pour le plancher du dashboard, plus `FastTimers`) a été écarté : il
laisserait les appels réels à `docker` dans ces tests. Or `docker` est présent sur `ubuntu-latest`.

### Les autres causes (H2 invalidée : ce n'est pas une cause unique)

Source : `norace.json` et `race.json` de la mesure du 2026-11-21. Une pile a été relevée pour un
test de chaque groupe. Pour les autres tests d'un groupe, la cause est déduite de leur durée.

| Cause | Tests | Race | Mécanisme |
|---|---|---|---|
| B. Timer de footer de 3 s armé avant `FastTimers` | 7 : `TestErrorFooterPerOperation` (pile vérifiée), `TestHandlerMaps*` ×3, `TestContextLoadErrorPostsFooterAndStaysNotReady`, `TestAFailedBindIsReportedOnScreen`, `TestOCISuccessfulRemovalClearsTheFailure` | 21,2 s | `drive()` (`context_exec_test.go:32-34`) appelle `FastTimers(&FooterMsgDuration)` après la construction de la `Cmd` : le `time.NewTimer` de 3 s est déjà armé. |
| C. E/S réelles de l'hôte au démarrage | 6 tests à environ 1,3 s (`TestNothingRunsTheStartViewInitTwice` (pile vérifiée), `TestCtxDashAlternatesBetweenTwoContexts`, `TestPreviousContextIsNeverPersisted`, …) | 7,9 s | `App.Init` lance `detectScanTools`, qui appelle `scan.Detect` (`internal/app/scan_tools.go:39`). Les vrais binaires `trivy`, `gitleaks` et `plumber` de l'hôte s'exécutent (`--version`). La durée **dépend de la machine**, et le chemin de code diffère en CI, où ces outils sont absents. |
| D. Autres ticks réels | `TestAgentUsagePollingAddsNoAnimationTick` (poll de 2 s, `agentusageview.pollTickCmd`), `TestPasteIntoCommandPaletteReachesTheField` (clignotement du curseur bubbles, environ 1 s) | 3,1 s | Comme B. |
| E. Diagnostic réseau réel sur 127.0.0.1 | `TestTraceRoundTripKeepsTheScreenHeight` | 4,8 s | `runDiagnostics` lance `netcheck.systemEnv.Ping`, un vrai ICMP, à deux tailles d'écran. |
| F. Calcul sous race | `TestTheOverlayNeverCutsAnEscapeSequence` (2,9 s), `TestThemeAutoDetectionIsRaceFree` (1,3 s) | 4,2 s | CPU, pas d'attente. On le laisse tel quel. |

Projection (estimée, sans recompiler) d'`internal/app` en race :
- après A : 326,8 − 243,1 ≈ **84 s** ;
- après A et B : ≈ **63 s** ;
- après A à E : ≈ **47 s**.

Sans race, après A et B : ≈ 38 s. La cible de 90 s est atteinte avec A et B seuls, mais la marge
après A seul est faible (6 s).

**Ne pas modifier `testutil.Msgs`.** La fonction a 518 appels dans 21 paquets. Certains tests y
cherchent justement un message de tick, par exemple `TestTheRefreshTickIsScheduled`. Changer sa
sémantique est à haut risque pour un gain nul : toutes les causes se corrigent au site d'appel.
Il faut en revanche corriger son commentaire (`testutil.go:165-168`) et la section « Tests — the
timer really does sleep » de la règle 128 : le timer part à la construction, donc `FastTimers`
doit être appelé **avant** l'`Update` qui construit la `Cmd`.

## 2. Effort par story

| Story | Effort | Hypothèse | Risque principal |
|---|---|---|---|
| S-01 (élargie à la cause A, 14 tests) | **S** | Une ligne dans `autoConfig`, plus une section datée dans `docs/measurements/`. Prototype déjà vert en race. | Un test futur qui reconstruit un routeur auto avec `testConfig()` brut réintroduit l'attente sans bruit. Parade : commentaire sur `autoConfig`, et éventuellement un garde « aucun test au-dessus de 5 s » (voir S-02c). |
| S-02a (cause B) | **S** | Déplacer `FastTimers(&FooterMsgDuration)` avant la construction de la `Cmd` dans `drive` et les 7 tests. Corriger le commentaire de `Msgs` et la règle 128. | Un test qui vérifie la persistance du message de footer. `TestAMessageGetsThreeSeconds` (paquet `components`) épingle déjà la valeur de production. |
| S-02b (cause C) | **S** | Un seam `detectTools = scan.Detect` dans `internal/app`, neutralisé dans `TestMain`, comme `containerForward`. | Un test qui s'appuie sur la vraie détection. À inventorier : `grep scanToolsDetectedMsg`. |
| S-02c (causes D et E, plus un garde de durée optionnel) | **S** | `FastTimers` sur le poll d'agentusage. Curseur non vidé ou désactivé. Diagnostic sur une étape factice. | E touche le pipeline netdiag : il faut garder la vérification de hauteur d'écran sans réseau réel. |
| S-03 (`t.Parallel`) | **M**, gain probablement inférieur à 15 s | Après S-01 et S-02, il reste environ 700 tests à moins de 0,1 s chacun. Tout routeur construit appelle `theme.ApplyTheme` et modifie `theme.CurrentThemeName` (état global). `FastTimers` écrit des variables de paquet (une course de données si on parallélise). `isolateHome` passe par `t.Setenv`, qui panique avec `t.Parallel`. Au moins 198 des 707 tests touchent directement un de ces états. | Instabilité, pour un gain marginal. Recommandation : **Won't** tant que la CI tient sous 10 min. |
| S-04 (un seul run `-race`) | **S** | `mise.toml` `[tasks.ci]` sans `test`, plus le garde `internal/ui/testutil/mise_ci_guard_test.go` : `ciSteps` sans `test`, `localOnlySteps = {"test-race"}` jusqu'à S-05. Inventaire H5 fait : aucun `//go:build` mentionnant race, aucun `race.Enabled` dans le dépôt. | Oublier le garde : `TestMiseCIRunsStepsSequentiallyInOrder` casse. |
| S-05 (job `Test` réel) | **M** | Voir les risques CI au §3. Il faut répliquer l'étape `scope` dans le job `Test`, ajouter un `actions/cache` pour GOCACHE et GOMODCACHE, vider `localOnlySteps`, et traiter les docs lues par des tests. | Faux vert sur une PR « docs-only » qui modifie un `.md` lu par un test (Q1). |
| S-06 (validation sur runner) | **M** | Surtout du temps d'horloge : au moins 10 runs d'environ 10 min, plus un run à cache vide (`gh cache delete`). Les correctifs d'éventuels tests instables deviennent des bugs séparés. | H4 : la suite race sur 4 vCPU n'est pas mesurée. Divergences d'environnement sur le runner : daemon Docker présent, scanners absents, pas de TTY, ICMP. |
| S-07 (docs, D149) | **S** | Une entrée D, des commentaires et `CLAUDE.md`. | Le critère 5 est trop large (voir §4). |
| S-08 (conditionnelle) | **L, à redécouper** | Deux causes distinctes. | Voir §3. |

## 3. Redécoupages et risques

**Découpage par cause.**
- S-01 : la cause A, avec ses 14 tests. Les 11 tests de S-02 qui partagent cette cause rejoignent
  S-01.
- S-02 devient trois stories :
  - S-02a (B) : **Must**, nécessaire pour garder une marge sous 90 s ;
  - S-02b (C) : Should, pour l'herméticité. Le résultat dépend aujourd'hui des outils installés
    sur le poste ;
  - S-02c (D, E et le garde de durée) : Could.
- S-08 devient deux stories :
  - S-08a : `scan` et `oci_resources`. La cause est commune : le binaire de test instrumenté est
    réexécuté comme faux outil par `installToolAt`. Piste : un faux outil compilé une fois, sans
    race, dans `TestMain`, ou un script. M ;
  - S-08b : `agentusage`. Les tests de plafonds font du calcul en volume sous race. Piste : des
    jeux de données plus petits, sans affaiblir l'assertion. M.

**Risques CI.**
- **Docs-only.** Le job `Test` ne reçoit pas les sorties du job `Lint and build`. Un `needs: lint`
  sérialiserait les deux jobs, et la durée de bout en bout deviendrait lint + test, ce qui casse
  l'hypothèse de S-06. Il faut donc dupliquer l'étape `checkout` (`fetch-depth: 2`) et l'étape
  `scope` dans `Test`. Le job doit toujours démarrer (pas de `paths-ignore`, déjà le cas), sinon
  le check reste « en attente ».
- **Trou de classification.** `docs-only.sh` classe en documentation des fichiers que des tests
  lisent :
  - `.claude/CLAUDE.md` et `docs/architecture/configuration.md` (`internal/credentials/hoststore_guard_test.go`) ;
  - `docs/architecture/network.md` (`internal/netproxy/guard_test.go:744`, `container_guard_test.go:470`).

  Seul `app-shell.md` est exclu. Une fois les tests réels en CI, une PR qui ne touche que ces
  fichiers passerait verte sans tests (Q1).
- **Cache Go.** `jdx/mise-action` ne met en cache que le répertoire des outils mise, pas GOCACHE
  (vérifié dans la doc de l'action, entrées `cache` et `cache_key`). Il faut un `actions/cache`
  séparé, avec les chemins lus par `go env GOCACHE GOMODCACHE`. `actions/setup-go` est à proscrire :
  il installerait un second Go, concurrent de celui de mise.

  Recommandation, à valider dans S-05 :
  - clé comprenant OS, `go version` résolu (`go = "1.27"` flotte sur le dernier 1.27.x), `go.sum`
    et le mot « race » ;
  - **sauvegarde uniquement sur `push` vers `main`** (`actions/cache/save`), restauration seule sur
    les PR. Cela évite que chaque PR consomme le quota de 10 Go (R5). Mesure locale : 1,4 Gio pour
    race et non race.

  `concurrency: cancel-in-progress` peut annuler la sauvegarde d'un run de `main` si des fusions
  se suivent de près : sans gravité.
- **Garde `mise_ci_guard_test.go`.** `TestMiseCIMatchesWorkflowSteps` lit les `run: mise run …`
  de `ci.yml` dans l'ordre des jobs et refuse aujourd'hui `test-race` en CI. S-05 doit vider
  `localOnlySteps` dans le même commit. L'étape doit s'écrire exactement `mise run test-race`, et le
  job `test` rester après `lint` dans le fichier.
- **Outils dans le job Test.** Sans `install_args: go`, `mise-action` installe aussi
  golangci-lint, goreleaser et uv, qui sont inutiles pour les tests.
- **gcc.** gcc est présent sur les images `ubuntu-latest` (à confirmer dans le journal de S-06 par
  un `gcc --version`). Le message de Go sans compilateur nomme `gcc`, pas `clang`.
- **Timeout.** `go test` coupe chaque binaire de paquet à 10 min par défaut et affiche un dump de
  goroutines, ce qui est lisible. `timeout-minutes: 15` au niveau du job couvre le reste.
- **H4 (non mesuré).** La somme des `Elapsed` des paquets autres qu'`internal/app` vaut environ
  730 s en race. Sur 2 cœurs, la suite complète durait 530 s. Sur 4 vCPU, après S-01 et S-02a,
  l'estimation donne 4 à 7 min de tests, plus environ 1 à 2 min de mise en place et de
  compilation à froid. C'est plausible mais pas acquis.

  Pré-contrôle conseillé avant S-06, peu coûteux : `taskset -c 0-3 go test -race -count=1 ./...`
  en local, une fois S-01 et S-02a fusionnées.

## 4. Ordre recommandé et critères à corriger

**Ordre.**
1. S-01, en parallèle avec S-04 (fichiers disjoints : tests d'`internal/app` d'un côté, `mise.toml`
   et le garde de l'autre).
2. S-02a.
3. S-02b et S-02c, en option, parallélisables.
4. Le pré-contrôle sur 4 cœurs.
5. S-05, qui dépend de S-04 (cohérence du garde) et de S-01 + S-02a (durée).
6. S-06.
7. S-07.
8. S-08a et S-08b, seulement si S-06 échoue sur la durée.

**Fusion conseillée.** Les critères 2, 3 et 4 de S-07 (commentaires de `ci.yml`, description de
`ci`, ligne « tests are disabled in CI » de `.claude/CLAUDE.md`) devraient rejoindre S-04 et S-05.
Ils deviennent faux au moment même de ces fusions, et la règle du dépôt veut la doc dans le même
commit que le code. S-07 garde l'entrée D qui remplace D149, puisqu'elle cite la mesure de S-06.

**Critères à corriger.**
- **S-01 : critère 1.** « fichier et ligne du code de production ou du helper `testutil.Msgs` » :
  la cause n'est ni l'un ni l'autre, mais le rafraîchissement du dashboard via l'Init différé
  (§1). Reformuler : « la commande qui porte l'attente ».
- **S-01 : critères 3 et 4.** Ils supposent un délai de thème rendu injectable. Le délai de thème
  (500 ms) est déjà raccourci et épinglé par `TestThemeAutoAppliesTheDarkPaletteAtOnceAndAsksOnce`
  (`theme_auto_test.go:138`), et le correctif recommandé n'ajoute aucun seam. Reformuler :
  - « aucune valeur de production modifiée (`themeDetectTimeout`, `Status.RefreshInterval`,
    plancher de 10 s du dashboard) » ;
  - le critère 4 ne s'applique que si un seam est introduit.
- **S-01 : critère 2.** Il ne vise que 3 tests. L'étendre aux 14 de la cause A.
- **S-02 : hypothèse « les 30 tests partagent la cause de S-01 ».** Invalidée : il y a cinq causes
  (§1).
- **S-02 : critère 4.** Sans objet si `testutil.Msgs` n'est pas modifié, ce qui est recommandé.
- **S-02 : critère 7.** La note à mettre à jour est `docs/architecture/ui-components.md`, qui cite
  `FastTimers`, plus la règle 128 dans `.claude/rules/tui-behavior.md`.
- **S-03 : critère 5.** La contrainte « 2 cœurs, 90 s » relève de S-02, pas du parallélisme.
- **S-04 : critère 6.** « message qui dit que `gcc` ou `clang` est requis » : Go affiche
  `cgo: C compiler "gcc" not found`. Reformuler : « le message nomme le compilateur C manquant ».
- **S-04 : critère 1.** Ajouter : le garde `mise_ci_guard_test.go` est mis à jour
  (`ciSteps`, `localOnlySteps`).
- **S-05 : critère 3.** « la compilation du second run est plus courte » n'est pas observable :
  `go test` ne sépare pas compilation et exécution. Remplacer par :
  - soit une étape séparée `go test -race -run '^$' ./...` (compilation seule) dont on compare la
    durée ;
  - soit « durée de l'étape `test-race` à cache chaud inférieure à celle à cache vide ».
- **S-05 : critère 5.** Il doit préciser le cas d'une PR qui ne touche que des `.md` lus par des
  tests (Q1).
- **S-06 : critère 1.** La « PR jetable portant les changements de S-01 à S-05 » sera déjà sur
  `main`. La PR jetable doit contenir un changement **hors docs** trivial, sinon `docs-only` saute
  les tests et la mesure est nulle.
  - Les 5 runs sous 10 min peuvent être les 5 premiers des 10 runs du critère 2.
  - Les runs se font par `gh run rerun`, l'un après l'autre (`cancel-in-progress`).
  - Le run à cache vide exige un `gh cache delete` préalable.
- **S-07 : critère 5.** « aucune occurrence de D149 » : il faut exclure les entrées historiques
  datées de `docs/backlog.md`, `docs/product/` et `.claude/plans/` (non versionné). Les
  commentaires D149 du garde `mise_ci_guard_test.go` sont à corriger dans S-04 et S-05.

## 5. Questions pour l'utilisateur

1. **Docs lues par des tests et PR docs-only.** Bloquante pour le critère 5 de S-05.
   - (a) Ajouter ces fichiers aux exceptions de `docs-only.sh`, plus un garde de test qui échoue
     si un `_test.go` lit un `.md` du dépôt non listé. **Recommandé.**
   - (b) Lancer toujours les tests, même sur une PR docs-only. Simple, mais chaque PR de docs attend
     environ 10 min.
   - (c) Accepter le trou.
2. **Priorité de S-02b (herméticité vis-à-vis des scanners de l'hôte) et de S-02c.**
   Recommandation : S-02b en Should, S-02c en Could.
3. **S-03.** Seuil de gain, question déjà ouverte. Recommandation : passer S-03 en **Won't** tant
   que la CI tient sous 10 min. Le gain attendu est inférieur à 15 s, pour un risque d'instabilité
   élevé (§2).
4. **Plan B de S-08** (`-race` ciblé) : question déjà ouverte, sans changement.

Hypothèse retenue sans la poser (prudente) : le cache Go n'est sauvegardé que depuis `main`.
