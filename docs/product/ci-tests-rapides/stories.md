# Stories — CI avec de vrais tests en moins de 10 minutes

Brief : `brief.md`. Mesures de départ : `measures.md` (2026-11-21). Faisabilité et efforts :
`feasibility.md` (architect, 2026-11-21). Backlog créé dans Plane le 2026-11-21 (modules « Tests rapides »,
« CI avec vrais tests », « Idée reportée »). Priorités MoSCoW, efforts S/M/L (aucun L ne reste).

## Ordre de livraison recommandé

1. S-01, en parallèle de S-04 (fichiers disjoints : tests d'`internal/app` d'un côté, `mise.toml` et
   le garde `mise_ci_guard_test.go` de l'autre).
2. S-02a.
3. Pré-contrôle local sur 4 cœurs : `taskset -c 0-3 go test -race -count=1 ./...` (voir S-06).
4. S-05, puis S-06, puis S-07.
5. En option, parallélisables : S-02b (Should), S-02c (Could), S-03 (Could).
6. S-08a puis S-08b, seulement si S-06 mesure plus de 10 min.

S-05 dépend de S-04 (cohérence du garde) et de S-01 + S-02a (durée).

## Vue d'ensemble

| ID | Plane | Titre | Priorité | Effort |
|---|---|---|---|---|
| S-01 | DEVDESK-643 | Les 14 tests bloqués par le dashboard de la vue de départ n'attendent plus 10 s | Must | S |
| S-02a | DEVDESK-644 | Les 7 tests à timer de footer n'attendent plus ; `internal/app` sous 90 s en race | Must | S |
| S-02b | DEVDESK-645 | `internal/app` ne lance plus les vrais trivy, gitleaks et plumber de l'hôte | Should | S |
| S-02c | DEVDESK-646 | Les autres attentes réelles d'`internal/app` (ticks, ping réel) disparaissent | Could | S |
| S-03 | DEVDESK-647 | `t.Parallel()` sur les tests d'`internal/app` qui le supportent | Could | M |
| S-04 | DEVDESK-648 | Un seul run de tests (`-race`) dans `mise run ci` | Must | S |
| S-05 | DEVDESK-649 | Le job `Test` exécute toujours les vrais tests en CI | Must | M |
| S-06 | DEVDESK-650 | Validation chiffrée sur un vrai runner | Must | M |
| S-07 | DEVDESK-651 | Décision D149 remplacée, documentation alignée sur la mesure | Should | S |
| S-08a | DEVDESK-652 | Si la CI dépasse 10 min : `internal/scan` et `oci_resources` | Could (conditionnelle) | M |
| S-08b | DEVDESK-384 (existant, rattaché au module) | Si la CI dépasse encore 10 min : `internal/agentusage` | Could (conditionnelle) | M |
| S-09 | DEVDESK-653 | Dagger (reporté, référence au spike antérieur) | Won't | sans objet |

Note S-08b : aucun work item créé, car DEVDESK-384 (« Réduire la durée des tests de internal/agentusage
sous -race », Backlog, créé le 2026-11-12) couvre le même périmètre. Décision prise par l'utilisateur :
DEVDESK-384 est réutilisé à la place de S-08b et rattaché au module « CI avec vrais tests » ; ses nom, état,
priorité et description sont inchangés.

Les dépendances (S-05 après S-04, S-01 et S-02a ; S-06 après S-05 ; S-08a après S-06) sont écrites dans les
descriptions Plane : l'API de relations est indisponible (404, édition Community).

## Décisions prises (utilisateur, 2026-11-21)

Les questions ouvertes de la version précédente sont toutes tranchées.

- **Job `Test` et PR docs-only** : le job `Test` exécute TOUJOURS les tests, y compris sur une PR qui
  ne touche que de la documentation. Le court-circuit docs-only reste réservé au job `Lint and build`.
  Le trou de classification de `docs-only.sh` (des `.md` lus par des tests : `.claude/CLAUDE.md`,
  `docs/architecture/configuration.md`, `docs/architecture/network.md`) est donc sans objet pour
  `Test`. Coût accepté (hypothèse) : une PR de documentation attend la durée complète des tests au
  lieu de quelques secondes.
- **Priorités** : S-02b = Should, S-02c = Could, S-03 = Could.
- **S-03** : seuil de gain de 15 s sur `internal/app` en race ; sous ce seuil, la story est fermée
  sans changement de code.
- **Timeout du job `Test`** : 15 minutes.
- **Seuil local de `mise run ci`** : au plus la moitié des 632 s mesurées (soit 316 s), à confirmer
  par la mesure de S-04.
- **Si la CI dépasse encore 10 min** : traiter S-08a puis S-08b d'abord ; un `-race` ciblé
  (paquets concurrents ou `main`) seulement en dernier recours, et c'est l'utilisateur qui le décide.
- **Hors périmètre global** : `testutil.Msgs` n'est PAS modifié (518 appels dans 21 paquets, certains
  tests y cherchent justement un message de tick, par exemple `TestTheRefreshTickIsScheduled`). Toutes
  les causes se corrigent au site d'appel. Seuls son commentaire et la règle 128 sont corrigés (S-02a).

Conventions des critères (valables pour toutes les stories de code) : les touches de test se
construisent via `internal/ui/testutil` ; règle 110 Bubble Tea respectée (aucune modification d'état
dans une `Cmd`) ; commentaires et messages de commit en anglais ; `go test -count=2` passe (HOME isolé,
`isolateHome(t)`) ; ports obtenus par `internal/testutil/loopback`.

---

## Epic « Tests rapides »

### S-01 — Les 14 tests bloqués par le dashboard de la vue de départ n'attendent plus 10 s
Plane : DEVDESK-643 — Epic : Tests rapides — Priorité : Must — Effort : S

En tant que mainteneur, je veux que les 14 tests d'`internal/app` qui attendent le rafraîchissement de 10 s
du dashboard (vue de départ par défaut) s'exécutent sans attente réelle afin de retirer environ 243 s des
327 s en race du paquet qui occupe 99,5 % du temps mur des tests.

Les 14 tests (cause A de `feasibility.md`) :
- les trois de la première version de la story : `TestAnInvalidUnusedCandidateWarnsAtStartup`,
  `TestThemeAutoFollowsTheTerminalAnswer`, `TestThemeAutoCandidateThatCannotBeUsedGivesDefaultAndOneWarning` ;
- `TestThemeAutoTimeoutFallsBackToDarkSilently`, `TestChoosingAutoInSessionReusesTheCachedAnswerWithoutAskingAgain`,
  `TestANamedThemeChosenInSessionReplacesAutoAndIsKept`, `TestBackgroundColorMsgIsNotForwardedToTheView`,
  `TestThemeAutoAnswersOnceAndCachesTheFirst` ;
- `TestAnInvalidThemeLightAndALightAnswerWarnOnce`, `TestAnInvalidThemeDarkAndATimeoutWarnOnce`,
  `TestAnInvalidThemeDarkAndADarkAnswerWarnOnce`, `TestBothCandidatesInvalidWarnOnceEach` ;
- `TestAnAnswerThatChangesThePaletteRebuildsTheViews`, `TestAnAnswerThatChangesThePaletteKeepsTheConfigurationView`.

**Critères d'acceptation**
1. Étant donné le code et les tests actuels, quand la story est terminée, alors une section datée de
   `docs/measurements/` consigne la cause : la commande qui porte l'attente (l'Init différé de la vue de
   départ, le dashboard, qui arme `scheduleRefresh` à `Status.RefreshInterval` = 10 s, avec un plancher),
   le fichier et la ligne de chaque élément du chemin, la raison pour laquelle chaque cas dure 10,09 s et
   non 10,00 s (le timer de `tea.Tick` part à la construction de la `Cmd`), et la mesure avant/après.
2. Étant donné les 14 tests, quand `go test -race -count=1 -run '<les 14 noms>' ./internal/app` est lancé
   sur le poste de mesure de `measures.md`, alors les 14 passent, aucun ne dépasse 1 s, et leur durée
   cumulée est inférieure à 10 s (contre environ 243 s avant).
3. Étant donné le comportement de production, quand la story est terminée, alors aucune valeur de
   production n'est modifiée (`themeDetectTimeout`, `Status.RefreshInterval`, plancher de 10 s du
   dashboard) et aucun fichier de production n'est changé : le correctif est dans le helper de test
   `autoConfig` (`cfg.App.DefaultView = "workspaces"`, une vue dont l'Init ne porte aucun timer), qui
   couvre `autoRouter` et `autoRouterWith`.
4. Étant donné chaque cas de repli du thème `auto` (réponse reçue, réponse absente, candidat inutilisable),
   quand les tests sont réparés, alors chacun reste couvert par au moins un test, et le délai de thème
   de 500 ms reste épinglé par `TestThemeAutoAppliesTheDarkPaletteAtOnceAndAsksOnce`.
5. Étant donné une régression volontaire du repli de thème (par exemple l'avertissement émis deux fois),
   quand les 14 tests tournent, alors au moins l'un d'eux échoue.
6. Étant donné `autoConfig`, quand on le lit, alors un commentaire en anglais explique pourquoi la vue de
   départ est forcée (un routeur auto construit avec `testConfig()` brut réintroduit l'attente de 10 s
   sans bruit).
7. Étant donné `go test -race -count=2 ./internal/app`, quand il est lancé, alors il est vert sur les deux
   passes.

**Hors périmètre** : les autres tests lents d'`internal/app` (S-02a, S-02b, S-02c) ; `testutil.Msgs`
(518 appels dans 21 paquets, non modifié) ; `t.Parallel()` (S-03) ; toute modification du thème visible par
l'utilisateur ; un seam de production pour le plancher du dashboard (écarté : il laisserait les appels réels
à `docker` dans ces tests).
**Hypothèses** : Hypothèse: le prototype de l'architect (une ligne dans `autoConfig`, 47 tests de thème verts
en race, 8,7 s cumulées) se reproduit sur le poste de mesure. Hypothèse: un garde « aucun test au-dessus
de 5 s » éventuel est traité dans S-02c, pas ici.
**Questions ouvertes** : aucune.

---

### S-02a — Les 7 tests à timer de footer n'attendent plus ; `internal/app` passe sous 90 s en race
Plane : DEVDESK-644 — Epic : Tests rapides — Priorité : Must — Effort : S

En tant que mainteneur, je veux que les 7 tests dont le timer de footer de 3 s est armé avant `FastTimers`
n'attendent plus en temps réel afin que `go test -race ./internal/app` passe de 326,8 s à moins de 90 s
(avec S-01 livrée).

Les 7 tests (cause B) : `TestErrorFooterPerOperation`, `TestHandlerMaps*` (3 tests),
`TestContextLoadErrorPostsFooterAndStaysNotReady`, `TestAFailedBindIsReportedOnScreen`,
`TestOCISuccessfulRemovalClearsTheFailure`. Mécanisme : `drive()` (`context_exec_test.go:32-34`) appelle
`FastTimers(&FooterMsgDuration)` après la construction de la `Cmd` ; le `time.NewTimer` de 3 s est déjà armé.

**Critères d'acceptation**
1. Étant donné les 7 tests, quand `FastTimers(&FooterMsgDuration)` est appelé avant l'`Update` qui construit
   la `Cmd` (dans `drive` et dans les tests concernés), alors chacun dure moins de 1 s en race (contre
   environ 21 s cumulées avant) et passe.
2. Étant donné S-01 livrée, quand `go test -race -count=1 ./internal/app` est lancé sur le poste de mesure,
   alors la durée du paquet est inférieure à 90 s sur 3 runs consécutifs (projection : environ 63 s après
   A et B, 84 s après A seul) et aucun test de premier niveau ne dépasse 10 s. Si S-01 n'est pas livrée, le
   critère est évalué sur l'ensemble des deux stories.
3. Étant donné le même paquet sans `-race`, quand `go test -count=1 ./internal/app` est lancé, alors sa
   durée est inférieure à 90 s (contre 301,9 s ; projection : environ 38 s).
4. Étant donné la valeur de production du message de footer (3 s), quand la story est terminée, alors elle
   est inchangée, `TestAMessageGetsThreeSeconds` (paquet `components`) passe sans modification, et un test
   qui vérifie la durée ou la persistance d'un message de footer échoue si cette valeur est cassée.
5. Étant donné `testutil.Msgs`, quand la story est terminée, alors sa sémantique est inchangée, son
   commentaire (`testutil.go:165-168`) et la section « Tests — the timer really does sleep » de la règle 128
   (`.claude/rules/tui-behavior.md`) disent que le timer part à la construction de la `Cmd` et que
   `FastTimers` doit être appelé avant l'`Update` qui la construit, et `docs/architecture/ui-components.md`
   (qui cite `FastTimers`) est mis à jour dans le même commit.
6. Étant donné `go test -race -count=2 ./internal/app`, quand il est lancé, alors il est vert (HOME isolé,
   aucun fichier laissé par la première passe).
7. Étant donné la mesure, quand la story est terminée, alors la durée avant/après de chaque test du groupe
   et celle du paquet sont consignées dans `docs/measurements/`.

**Hors périmètre** : modifier `testutil.Msgs` ; les causes C, D, E (S-02b, S-02c) ; la cause F (calcul sous
race : `TestTheOverlayNeverCutsAnEscapeSequence`, `TestThemeAutoDetectionIsRaceFree`, laissés tels quels) ;
`t.Parallel()` (S-03) ; les paquets `internal/scan`, `oci_resources`, `agentusage` (S-08a, S-08b).
**Hypothèses** : Hypothèse: les projections de l'architect (sans recompiler) se vérifient ; la marge après
A seul est de 6 s seulement, d'où l'intérêt de B. Hypothèse: les 675 autres tests pèsent environ 41 s en race.
**Questions ouvertes** : aucune.

---

### S-02b — `internal/app` ne lance plus les vrais trivy, gitleaks et plumber de l'hôte
Plane : DEVDESK-645 — Epic : Tests rapides — Priorité : Should — Effort : S

En tant que mainteneur, je veux que les tests d'`internal/app` ne dépendent plus des scanners installés sur le
poste afin que leur résultat et leur durée soient les mêmes sur mon poste et sur un runner (où ces outils
sont absents).

Cause C : `App.Init` lance `detectScanTools`, qui appelle `scan.Detect` (`internal/app/scan_tools.go:39`) ;
les vrais binaires `trivy`, `gitleaks` et `plumber` s'exécutent (`--version`). 6 tests à environ 1,3 s
(7,9 s en race), dont `TestNothingRunsTheStartViewInitTwice`, `TestCtxDashAlternatesBetweenTwoContexts`,
`TestPreviousContextIsNeverPersisted`.

**Critères d'acceptation**
1. Étant donné un `PATH` contenant un faux `trivy`, `gitleaks` et `plumber` qui échouent s'ils sont
   exécutés, quand `go test -race ./internal/app` est lancé, alors aucun test ne les exécute et tous
   passent (le résultat ne dépend plus des outils du poste).
2. Étant donné les tests qui lisent `scanToolsDetectedMsg`, quand l'inventaire est fait
   (`grep scanToolsDetectedMsg`), alors chacun utilise un résultat de détection maîtrisé, ou est conservé
   tel quel avec la raison consignée, et au moins un test couvre le câblage réel (la valeur de production
   du seam est `scan.Detect`).
3. Étant donné le seam (`detectTools = scan.Detect`), quand il est neutralisé, alors il l'est dans `TestMain`,
   comme `containerForward` et `proxyForward`, jamais dans du code non test ; `go vet` et `golangci-lint`
   passent sans nouvelle exception.
4. Étant donné les 6 tests du groupe, quand ils tournent en race, alors leur durée cumulée est inférieure
   à 2 s (contre 7,9 s).
5. Étant donné `go test -race -count=2 ./internal/app`, quand il est lancé, alors il est vert ; la note
   d'architecture concernée sous `docs/architecture/` mentionne le seam.

**Hors périmètre** : le comportement de `scan.Detect` en production ; les autres causes (S-01, S-02a, S-02c).
**Hypothèses** : Hypothèse: aucun test ne s'appuie sur la vraie détection (l'inventaire du critère 2 le
vérifie). Hypothèse: le chemin de code diffère en CI, où ces outils sont absents ; cette story supprime
cette différence.
**Questions ouvertes** : aucune.

---

### S-02c — Les autres attentes réelles d'`internal/app` (ticks, ping réel sur 127.0.0.1) disparaissent
Plane : DEVDESK-646 — Epic : Tests rapides — Priorité : Could — Effort : S

En tant que mainteneur, je veux supprimer les dernières attentes réelles d'`internal/app` afin de ramener le
paquet vers environ 47 s en race (projection, causes A à E) et de rendre la suite plus déterministe.

Causes D et E : `TestAgentUsagePollingAddsNoAnimationTick` (poll de 2 s, `agentusageview.pollTickCmd`),
`TestPasteIntoCommandPaletteReachesTheField` (clignotement du curseur bubbles, environ 1 s),
`TestTraceRoundTripKeepsTheScreenHeight` (vrai ICMP sur 127.0.0.1 via `netcheck.systemEnv.Ping`, à deux
tailles d'écran, 4,8 s).

**Critères d'acceptation**
1. Étant donné `TestAgentUsagePollingAddsNoAnimationTick` et `TestPasteIntoCommandPaletteReachesTheField`,
   quand ils tournent, alors ils n'attendent plus de tick réel (`FastTimers` sur le poll d'agentusage, curseur
   non vidé ou désactivé) et vérifient toujours ce qu'ils vérifiaient (aucun tick d'animation ajouté ; le
   collage atteint le champ).
2. Étant donné `TestTraceRoundTripKeepsTheScreenHeight`, quand il tourne, alors le diagnostic s'exécute sur
   une étape factice (aucun ICMP réel) et le test vérifie toujours la hauteur d'écran aux deux tailles.
3. Étant donné les trois tests, quand ils tournent en race, alors leur durée cumulée est inférieure à 2 s
   (contre environ 7,9 s).
4. Étant donné un garde de durée (« aucun test de `internal/app` au-dessus de 5 s en race »), quand la
   story est terminée, alors soit il est livré et son échec nomme le test et sa durée, soit la raison de ne
   pas le livrer (sensibilité à la charge du runner) est consignée.
5. Étant donné `go test -race -count=2 ./internal/app`, quand il est lancé, alors il est vert.

**Hors périmètre** : la cause F (calcul sous race), laissée telle quelle ; `testutil.Msgs` (non modifié) ;
`t.Parallel()` (S-03).
**Hypothèses** : Hypothèse: le garde de durée est optionnel ; s'il est livré, ses exclusions éventuelles sont
listées avec leur raison. Hypothèse: le diagnostic netdiag garde sa couverture sans réseau réel.
**Questions ouvertes** : livrer ou non le garde de durée (à décider au moment de la story).

---

### S-03 — `t.Parallel()` sur les tests d'`internal/app` qui le supportent sans nouvelle instabilité
Plane : DEVDESK-647 — Epic : Tests rapides — Priorité : Could — Effort : M

En tant que mainteneur, je veux exécuter en parallèle les tests d'`internal/app` qui n'ont pas d'état partagé
afin de gagner du temps mur au-delà de S-02a, sans introduire d'échec instable.

**Critères d'acceptation**
1. Étant donné `internal/app` après S-01 et S-02a, quand on mesure `go test -race -count=1 ./internal/app`
   avant et après la story (médiane de 3 runs chacun), alors le gain est consigné dans `docs/measurements/` ;
   s'il est inférieur à 15 s, la story est refermée sans changement de code.
2. Étant donné un test qui touche un état global, quand l'inventaire est fait, alors il n'appelle pas
   `t.Parallel()` : tout test qui construit un routeur (`theme.ApplyTheme` modifie `theme.CurrentThemeName`),
   écrit une variable de paquet via `FastTimers`, utilise `t.Setenv` (incompatible avec `t.Parallel`),
   `isolateHome(t)` ou `routerAt`, `fakekeyring.Install` ou `fakekeyring.Fresh`, les seams `proxyForward` et
   `containerForward`, ou un port de `loopback.ReservePort`. La liste des exclusions et leur raison est
   consignée (au moins 198 des 707 tests touchent directement un de ces états).
3. Étant donné les tests rendus parallèles, quand `go test -race -count=10 ./internal/app` est lancé, alors
   le résultat est vert à chaque passe et le détecteur de race ne signale rien.
4. Étant donné `go test -race -count=2 ./...`, quand il est lancé, alors il est vert.
5. Étant donné la règle de test du dépôt (« Install et Fresh sont globaux, donc pas de `t.Parallel` »), quand
   un test du paquet en dépend, alors il reste séquentiel et aucune garde existante (`TestHostStoreGuard*`)
   ne change.

**Hors périmètre** : `t.Parallel()` dans d'autres paquets ; rendre `fakekeyring` compatible avec le
parallélisme ; toute refonte de l'isolation de HOME ; la contrainte « 90 s sur 2 cœurs » (elle relève de
S-02a, pas du parallélisme).
**Hypothèses** : Hypothèse: après S-01 et S-02a il reste environ 700 tests à moins de 0,1 s chacun ; le gain
est probablement inférieur à 15 s, et la story sera alors fermée sans changement de code (hypothèse de
l'architect). Hypothèse: les tests construits par `routerAt` ne sont pas parallélisables tant que HOME est
partagé par process.
**Questions ouvertes** : aucune.

---

## Epic « CI avec vrais tests »

### S-04 — Un seul run de tests (`-race`) dans `mise run ci`
Plane : DEVDESK-648 — Epic : CI avec vrais tests — Priorité : Must — Effort : S

En tant que mainteneur, je veux que `mise run ci` exécute les tests une seule fois, avec `-race`, afin de retirer
environ 303 s de la durée mesurée (632 s vers 329 s sur 22 cœurs) sans perdre de couverture.

**Critères d'acceptation**
1. Étant donné `mise run ci`, quand la tâche est lancée, alors la suite s'exécute dans l'ordre `fmt-check`,
   `vet`, `lint`, `build`, `test-race`, sans étape `test` séparée, et s'arrête à la première étape en échec ;
   le garde `internal/ui/testutil/mise_ci_guard_test.go` est mis à jour dans le même commit (`ciSteps` sans
   `test`, `localOnlySteps = {"test-race"}` jusqu'à S-05, commentaires D149 corrigés) et
   `TestMiseCIRunsStepsSequentiallyInOrder` passe.
2. Étant donné les tests du dépôt, quand l'inventaire est fait, alors la liste des fichiers ou tests qui ne
   s'exécutent qu'en l'absence du détecteur (`//go:build !race`, lecture de `race.Enabled`) est consignée
   (l'architecte n'en a trouvé aucun) ; si elle n'est pas vide, chacun est adapté pour tourner sous `-race`
   ou conservé dans une étape distincte nommée, avec la raison.
3. Étant donné un test qui échoue en `-race`, quand `mise run ci` s'arrête, alors la sortie permet de
   retrouver le nom du test, le paquet et le message d'échec sans relancer avec `-v`.
4. Étant donné la tâche `mise run test` (sans race), quand elle est appelée seule, alors elle existe toujours
   et fonctionne (boucle locale rapide) mais n'est plus appelée par `ci`.
5. Étant donné le poste de mesure de `measures.md`, quand `mise run ci` est mesuré avant et après sur cache
   chaud, alors la durée de l'étape de test passe d'au moins 632 s à celle d'un seul run `-race` (329 s
   attendus avant S-01 et S-02a) et la mesure est consignée ; le seuil local de `mise run ci` (au plus la
   moitié des 632 s, soit 316 s) est confirmé par la mesure, ou réévalué une fois S-01 et S-02a fusionnées
   si S-04 seule ne l'atteint pas (329 s de tests attendus avant ces stories).
6. Étant donné `test-race` sur une machine sans compilateur C, quand la tâche est lancée, alors elle échoue
   avec un message qui nomme le compilateur C manquant (Go affiche `cgo: C compiler "gcc" not found`) ;
   comportement documenté, non régressé.
7. Étant donné la description de la tâche `ci` dans `mise.toml` et la liste des contrôles de `mise run ci`
   dans `.claude/CLAUDE.md`, quand elles sont relues, alors elles énumèrent `fmt-check`, `vet`, `lint`,
   `build`, `test-race` (un seul run de test) et non plus « six contrôles » dont deux de test, dans le même
   commit que le changement de la tâche.

**Hors périmètre** : le contenu du workflow GitHub (S-05) ; la réparation des tests (S-01, S-02a) ; la ligne
« tests are disabled in CI » de `.claude/CLAUDE.md` (S-05).
**Hypothèses** : Hypothèse: aucun test n'est réservé à l'absence de `-race` (H5 du brief ; inventaire de
l'architect : aucun `//go:build` mentionnant race, aucun `race.Enabled`). Hypothèse: un développeur sans
compilateur C garde `mise run test` pour la boucle locale rapide.
**Questions ouvertes** : aucune.

---

### S-05 — Le job `Test` exécute toujours les vrais tests en CI
Plane : DEVDESK-649 — Epic : CI avec vrais tests — Priorité : Must — Effort : M

En tant que mainteneur, je veux que le job `Test` de `.github/workflows/ci.yml` exécute `mise run test-race` sur
toute PR, y compris une PR de documentation seule, afin qu'aucune PR ne puisse être fusionnée sans que les tests
aient réellement tourné, tout en gardant le nom `Test` exigé par le ruleset `main-protection`.

**Critères d'acceptation**
1. Étant donné une PR dont tous les tests passent, quand le workflow s'exécute, alors le job nommé exactement
   `Test` appelle `mise run test-race` (étape écrite exactement ainsi, job placé après `lint` dans le fichier,
   et non plus un `echo`) et se termine en succès ; `localOnlySteps` est vidé dans
   `internal/ui/testutil/mise_ci_guard_test.go` dans le même commit et `TestMiseCIMatchesWorkflowSteps` passe.
2. Étant donné une PR avec un test qui échoue volontairement, quand le workflow s'exécute, alors le check
   `Test` est rouge, la fusion est bloquée par `main-protection`, et le nom du test en échec et son message
   sont visibles dans le journal du job sans télécharger d'artefact.
3. Étant donné une PR qui ne modifie que `docs/` ou des fichiers Markdown (y compris un `.md` lu par un test,
   comme `.claude/CLAUDE.md` ou `docs/architecture/network.md`), quand le workflow s'exécute, alors le job
   `Test` exécute réellement `mise run test-race` (l'étape n'est jamais sautée) et son check est vert après
   cette exécution ; seul `Lint and build` peut être court-circuité par le mécanisme docs-only.
4. Étant donné deux runs successifs sur la même base, quand le second démarre, alors le cache Go (GOCACHE et
   GOMODCACHE, chemins lus par `go env GOCACHE GOMODCACHE`) est restauré par `actions/cache` (ni `mise-action`,
   qui ne met pas GOCACHE en cache, ni `actions/setup-go`, qui installerait un second Go) et la ligne de
   restauration est visible dans le journal ; la clé comprend l'OS, la version de Go résolue, `go.sum` et le
   mot « race » ; la sauvegarde n'a lieu que sur `push` vers `main` (`actions/cache/save`), les PR ne
   font que restaurer ; la taille enregistrée est inférieure à 5 Gio (1,4 Gio mesurés en local), pour laisser
   de la place au quota de 10 Go.
5. Étant donné un run à cache chaud et un run à cache vide, quand on compare la durée de l'étape `test-race`
   (ou d'une étape de compilation seule `go test -race -run '^$' ./...`), alors la durée à cache chaud est
   inférieure à celle à cache vide, et les deux sont consignées.
6. Étant donné la configuration du workflow, quand elle est relue, alors : le job `Test` a un
   `timeout-minutes: 15` explicite (un test bloqué rend le check rouge, jamais « en cours » indéfiniment) ;
   il n'a pas de `needs` (les deux jobs tournent en parallèle, la durée de bout en bout vaut le plus long) ;
   `mise-action` n'installe que Go (`install_args: go`, pas golangci-lint, goreleaser ni uv) ; le job ne reçoit
   aucun secret ; les noms `Test` et `Lint and build` n'ont pas changé, le ruleset n'est pas à reconfigurer.
7. Étant donné les commentaires de `ci.yml` qui expliquaient le faux job `Test`, la ligne « tests are
   disabled in CI (maintainer decision, D149) » de `.claude/CLAUDE.md` et les commentaires D149 du garde
   `mise_ci_guard_test.go`, quand la story est terminée, alors ils sont remplacés, dans le même commit, par des
   énoncés exacts en anglais : tests `-race` en CI sur toute PR, y compris docs-only pour le job `Test`, cache,
   timeout de 15 min.

**Hors périmètre** : le dimensionnement du runner (on reste sur `ubuntu-latest`) ; la matrice Windows et
macOS ; la mesure sur 5 et 10 runs (S-06) ; la classification de `docs-only.sh` pour `Lint and build`.
**Hypothèses** : Hypothèse (acceptée par l'utilisateur): une PR de documentation attend désormais la durée
complète des tests au lieu de quelques secondes. Hypothèse: le job `Test` démarre toujours (pas de
`paths-ignore`, déjà le cas), sinon le check requis reste « en attente » ; s'il avait un jour besoin de la
portée des fichiers modifiés, il refait lui-même l'étape de portée (`checkout` avec `fetch-depth: 2`) plutôt
que d'utiliser `needs`, qui sérialiserait les jobs. Hypothèse: `concurrency: cancel-in-progress` peut annuler
la sauvegarde du cache d'un run de `main` si des fusions se suivent de près : sans gravité. Hypothèse: un
runner `ubuntu-latest` a gcc (confirmé dans le journal de S-06).
**Questions ouvertes** : aucune.

---

### S-06 — Validation chiffrée sur un vrai runner : 5 runs sous 10 min et 10 runs sans échec instable
Plane : DEVDESK-650 — Epic : CI avec vrais tests — Priorité : Must — Effort : M

En tant que mainteneur, je veux mesurer la CI réelle sur des runners GitHub-hosted afin de confirmer que le
critère « CI complète en moins de 10 minutes, sans test instable » est atteint avant de fermer l'idée.

**Critères d'acceptation**
1. Étant donné S-01 et S-02a fusionnées, quand le pré-contrôle local `taskset -c 0-3 go test -race -count=1 ./...`
   est lancé, alors sa durée est consignée avant tout run sur runner ; si elle dépasse 10 min, elle est
   signalée à l'utilisateur, qui décide de lancer S-08a avant S-06.
2. Étant donné une PR jetable (jamais fusionnée, fermée à la fin) basée sur `main`, qui contient S-01 à S-05
   déjà fusionnées et un changement trivial **hors docs** (sans lui, `Lint and build` est court-circuité et la
   durée de bout en bout est sous-estimée), quand le workflow tourne 10 fois consécutives par `gh run rerun`,
   l'un après l'autre (`cancel-in-progress`), alors aucun run n'échoue ; un échec éventuel est analysé
   (instabilité ou régression) et corrigé, par un bug séparé, avant de refaire le compte de zéro.
3. Étant donné ces runs, quand on lit leur durée de bout en bout (du démarrage du workflow à la fin du dernier
   job, par `gh run view`), alors elle est inférieure à 10 min pour au moins 5 runs consécutifs, qui peuvent
   être les 5 premiers des 10 runs du critère 2, et les durées sont consignées.
4. Étant donné la mesure, quand elle est consignée, alors un fichier daté dans `docs/measurements/` donne
   pour chaque run : durée par job, durée du cache restauré, durée de `test-race`, paquets les plus lents ;
   et pour l'ensemble : version de l'image du runner, nombre de vCPU lu pendant le run, version de gcc
   (`gcc --version` dans le journal), taille du cache ; le fichier ne corrige aucune mesure antérieure
   (une mesure qui contredit un critère est figée et notée).
5. Étant donné le premier run (cache vide, après `gh cache delete` préalable) et les suivants (cache chaud),
   quand les durées sont comparées, alors les deux sont rapportées séparément et le critère de 10 min
   s'applique aux deux.
6. Étant donné le paramètre d'approbation des workflows de PR externes, quand il est lu dans les réglages
   Actions, alors la valeur est notée ; si elle est moins stricte que « first-time contributors require
   approval », elle est signalée à l'utilisateur sans être modifiée par la story.
7. Étant donné un résultat supérieur à 10 min, quand la mesure est consignée, alors S-08a est déclenchée
   d'abord (puis S-08b), avec les paquets les plus lents du run comme point de départ ; un `-race` ciblé
   n'est envisagé qu'en dernier recours, sur décision de l'utilisateur.

**Hors périmètre** : fixer les réglages de sécurité des PR externes ; tout runner self-hosted ; la correction
des tests instables trouvés (bugs séparés).
**Hypothèses** : Hypothèse: les deux jobs (`Lint and build` et `Test`) tournent en parallèle, donc la durée de
bout en bout vaut le plus long des deux (à vérifier dans `ci.yml`). Hypothèse: un runner `ubuntu-latest` de
dépôt public a 4 vCPU et 16 Go (`research.md`). Hypothèse (H4, non mesurée): après S-01 et S-02a, 4 à 7 min de
tests plus 1 à 2 min de mise en place et de compilation à froid. Hypothèse: 10 runs par re-runs du même commit
testent la variation de l'environnement, pas celle du code. Hypothèse: un cache de `main` existe pour les runs
à cache chaud (le cache n'est sauvegardé que depuis `main`). Divergences possibles sur le runner : daemon
Docker présent, scanners absents, pas de TTY, ICMP.
**Questions ouvertes** : aucune.

---

### S-07 — Décision D149 remplacée, documentation alignée sur la mesure
Plane : DEVDESK-651 — Epic : CI avec vrais tests — Priorité : Should — Effort : S

En tant que mainteneur (et agent IA qui lit les consignes du dépôt), je veux que la décision D149 soit remplacée
par une décision qui cite la mesure afin de ne plus croire que « les tests sont désactivés en CI ».

Les mises à jour de `ci.yml`, de la description de la tâche `ci` et de `.claude/CLAUDE.md` ont rejoint S-04 et
S-05 (elles deviennent fausses au moment de ces fusions ; la documentation voyage avec le code). S-07 garde
l'entrée de décision, qui cite la mesure de S-06.

**Critères d'acceptation**
1. Étant donné `docs/backlog.md`, quand la décision est enregistrée, alors une nouvelle entrée de décision
   remplace D149 (D149 reste visible, marquée « remplacée par D<n> » avec la date et le lien vers la mesure
   de S-06) et énonce la raison du changement (le goulot `internal/app` est réparé).
2. Étant donné la nouvelle entrée, quand elle est lue, alors elle énonce les décisions de cette idée : tests
   `-race` exécutés en CI sur toute PR (y compris docs-only pour le job `Test`), timeout de 15 min, cache
   sauvegardé depuis `main` seulement, et la durée visée est celle mesurée par S-06.
3. Étant donné une recherche de `D149` et de « tests are disabled in CI » dans le dépôt, quand elle est
   lancée en excluant les entrées historiques datées de `docs/backlog.md`, `docs/product/` et `.claude/plans/`
   (non versionné), alors aucune occurrence ne présente encore la décision comme en vigueur (en particulier
   `.github/workflows/ci.yml`, `.claude/CLAUDE.md` et `internal/ui/testutil/mise_ci_guard_test.go`).
4. Étant donné les règles de commit du dépôt, quand les changements sont livrés, alors messages et
   commentaires sont en anglais.

**Hors périmètre** : les notes d'architecture propres à la réparation des tests (déjà dans S-01, S-02a et
S-02b) ; `ci.yml`, la tâche `ci` et `CLAUDE.md` (S-04 et S-05) ; la documentation d'une éventuelle
réévaluation de Dagger (S-09).
**Hypothèses** : Hypothèse: la numérotation des décisions suit la dernière entrée de `docs/backlog.md` (l'architect
ou le tech-writer lit le fichier pour attribuer le numéro). Hypothèse: S-06 est terminée avant S-07 (l'entrée
cite sa mesure).
**Questions ouvertes** : aucune.

---

### S-08a — Si la CI dépasse 10 min : réduire `internal/scan` et `internal/ui/oci_resources`
Plane : DEVDESK-652 — Epic : CI avec vrais tests — Priorité : Could (conditionnelle) — Effort : M

En tant que mainteneur, je veux réduire la durée en `-race` d'`internal/scan` et d'`internal/ui/oci_resources`,
seulement si S-06 mesure plus de 10 minutes, afin de passer sous le seuil sans retirer `-race`.

Condition d'entrée : S-06 mesure au moins un run de bout en bout supérieur à 10 minutes. S-08a passe avant
S-08b.

**Critères d'acceptation**
1. Étant donné la mesure de S-06, quand la story démarre, alors la liste des paquets et tests qui font dépasser
   les 10 minutes est consignée, avec leur durée sur runner (pas celle du poste local).
2. Étant donné `internal/scan` (144,8 s en race, dont `TestPlumberIsResolvedLikeTheOtherTwo` 29,4 s et
   `TestAutoKeepsTheOldBinaryFirstResolution` 26,3 s contre 0,1 s sans race), quand la cause commune est traitée
   (le binaire de test instrumenté est réexécuté comme faux outil par `installToolAt`), alors la durée du paquet
   en race est inférieure à 60 s, ou une raison chiffrée de ne pas aller plus loin est consignée.
3. Étant donné `internal/ui/oci_resources` (130,4 s en race, 18,9 s sans), quand ses tests les plus lents sont
   traités, alors le paquet passe sous 60 s en race, ou une raison chiffrée est consignée.
4. Étant donné les modifications, quand `go test -race -count=2 ./...` est lancé, alors il est vert, les
   tests modifiés vérifient les mêmes comportements (résolution des outils inchangée) et une régression
   volontaire de la résolution fait toujours échouer au moins un test.
5. Étant donné la CI après modification, quand 5 runs consécutifs sont mesurés, alors tous sont sous 10 minutes ;
   sinon S-08b est lancée, puis, en dernier recours seulement, la décision d'un `-race` ciblé est remise à
   l'utilisateur.

**Hors périmètre** : `internal/agentusage` (S-08b) ; retirer `-race` des PR ; découper en matrice de jobs.
**Hypothèses** : Hypothèse: piste de l'architect : un faux outil compilé une fois, sans race, dans `TestMain`,
ou un script. Hypothèse: la cause commune vaut aussi pour `oci_resources` (non isolée dans `measures.md`).
**Questions ouvertes** : aucune (la politique d'un `-race` ciblé est décidée : dernier recours, par l'utilisateur).

---

### S-08b — Si la CI dépasse encore 10 min : réduire `internal/agentusage`
Plane : DEVDESK-384 (existant, rattaché au module) — Epic : CI avec vrais tests — Priorité : Could (conditionnelle) — Effort : M

En tant que mainteneur, je veux réduire la durée en `-race` d'`internal/agentusage`, seulement si la CI dépasse
encore 10 minutes après S-08a, afin de passer sous le seuil sans retirer `-race`.

Condition d'entrée : S-06 mesure plus de 10 minutes et S-08a est livrée sans suffire.

**Critères d'acceptation**
1. Étant donné la mesure de S-06 (et celle de S-08a), quand la story démarre, alors la part d'`agentusage`
   dans le dépassement est consignée avec sa durée sur runner.
2. Étant donné `internal/agentusage` (105,7 s en race, 12,0 s sans), quand ses tests de plafonds (calcul en
   volume sous race) sont traités, alors le paquet passe sous 60 s en race, ou une raison chiffrée de ne pas
   aller plus loin est consignée.
3. Étant donné un test de plafond allégé (jeu de données plus petit), quand une régression volontaire casse
   le plafond, alors le test échoue toujours (l'assertion n'est pas affaiblie).
4. Étant donné les modifications, quand `go test -race -count=2 ./...` est lancé, alors il est vert et les
   tests `TestConcurrent*` tournent toujours sous `-race`.
5. Étant donné la CI après modification, quand 5 runs consécutifs sont mesurés, alors tous sont sous 10 minutes ;
   sinon la décision d'un `-race` ciblé est remise à l'utilisateur, en dernier recours.

**Hors périmètre** : `internal/scan` et `oci_resources` (S-08a) ; retirer `-race` des PR ; découper en matrice.
**Hypothèses** : Hypothèse: des jeux de données plus petits suffisent à garder la même assertion (piste de
l'architect).
**Questions ouvertes** : aucune.

---

## Epic « Idée reportée »

### S-09 — Dagger sur k0s pour les agents IA et l'identité local/CI (reporté)
Plane : DEVDESK-653 — Epic : Idée reportée — Priorité : Won't — Effort : sans objet

En tant que mainteneur, je veux garder la trace de l'idée Dagger, de la raison de son report et de la condition de
sa réévaluation afin de ne pas refaire la recherche ni rouvrir le débat sans fait nouveau.

**Référence antérieure** : le dossier `docs/product/dagger-ci-release/` (brief, research, stories et
`note-decision.md`) consigne un spike Dagger local mené du 2026-11-11 au 2026-11-20, pour les vérifications
locales et CI. Son verdict est « non tranché, à décider par l'utilisateur » (le critère de coût, Entire compris,
n'a pas été mesuré). Il est cité ici pour ne pas refaire cette évaluation : toute réévaluation part de cette
note, pas de zéro.

**Critères d'acceptation**
1. Étant donné le dossier `docs/product/ci-tests-rapides/`, quand on lit `brief.md`, alors une section
   « Idée reportée : Dagger » donne la raison du report (preuves de `measures.md` : goulot dans `internal/app`,
   pas la compilation ni `-race` ; moteur privilégié sur dépôt public partagé avec les agents IA ; compte de
   type User sans groupes de runners ; Dagger Cloud sans palier gratuit décrit ; aucun benchmark public) et
   renvoie à `docs/product/dagger-ci-release/note-decision.md`.
2. Étant donné le même document, quand on le lit, alors la condition de réévaluation est explicite : un besoin
   avéré d'identité exacte entre exécution locale et CI, ou d'agents IA sur k0s qui lancent les tests ; et la
   réévaluation reprend la note du spike du 2026-11-20 au lieu de la refaire.
3. Étant donné le dépôt à la fin du MVP, quand on le parcourt, alors il ne contient ni module Dagger (aucun
   `dagger.json`, aucun dossier de module), ni manifeste d'actions-runner-controller, ni référence à un runner
   `self-hosted` ou à `datomir` dans `.github/` ; les seules traces de Dagger sont documentaires
   (`docs/product/dagger-ci-release/` et ce dossier), le module du spike n'étant plus consultable
   (branche `spike/dagger` supprimée).

**Hors périmètre** : toute implémentation ; toute nouvelle recherche ; refaire le spike.
**Hypothèses** : Hypothèse: aucun des deux déclencheurs n'est à l'ordre du jour avant la fin du MVP.
**Questions ouvertes** : aucune.
