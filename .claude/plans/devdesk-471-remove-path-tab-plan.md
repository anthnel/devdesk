# DEVDESK-471 (P2-02) — Retirer l'onglet Path : le trace devient le sous-niveau de Diagnostics

Branche `feat/devdesk-471-remove-path-tab`, base `147593c0` (P2-01 / DEVDESK-470 mergé).
Sources lues : `internal/ui/netdiag/{model,update,view,header,trace_form,trace_level,path_model,path_view,path_from_route,path_source_flow}.go`,
les tests `path_*`, `trace_*`, `netdiag_path_tab_test.go`, `internal/app/{bar_attach,netdiag,netdiag_path_guard,activity_guard}_test.go`,
`internal/ui/keymap/keymap.go:289`, `docs/architecture/network.md` (l. 839-1093, 1300+), D144, D150, D152 (`docs/backlog.md`),
audit `.claude/plans/net-view-ux-audit.md` (checkout principal, option P2, tableau de correspondance l. 85-94).

## Questions bloquantes

1. **AC6, `esc` sur le formulaire prérempli depuis Status.** Aujourd'hui `OriginView` n'agit que sur les
   résultats (`handleKeyResults`) ; sur le formulaire (Status `H` sur un moniteur icmp/dns, sans auto-run),
   `esc` ne fait rien (`handleKeyInput`, `case "esc": return m, nil`). L'AC dit « `esc` revient à Status ».
   - (a) Ajouter : `esc` sur le formulaire, quand `OriginView != ""`, émet `BackToOriginMsg` (et le raccourci
     `esc Go back` apparaît dans le header du formulaire dans ce cas seulement).
   - (b) Ne rien changer : l'AC6 vise la non-régression du chemin résultats (auto-run) ; on ajoute seulement
     les tests de non-régression.
   - **Recommandation : (b)** : c'est le comportement actuel, la story ne retire que l'onglet Path, et (a) change une
     touche hors du périmètre de la story. Si l'utilisateur veut (a), c'est la seule sous-étape 4b, de taille S.
     Sans réponse, l'étape 4 applique (b).
2. **`enter` après un trace terminé, dans le sous-niveau.** D152 point 2 interdisait toute relance dans le
   sous-niveau. L'AC2 y fait déjà lancer le trace par `enter` (état prêt), et l'AC3 y rouvre `ctrl+o`.
   - (a) `enter` relance aussi après un trace terminé ou annulé (« Trace again »), comme l'onglet Path le
     faisait (`TestPathDoneEnterRelaunchesAndEscEdits`).
   - (b) `enter` ne lance que depuis l'état prêt ; pour relancer, il faut `esc`, puis `H` ou Trace Path.
   - **Recommandation : (a)** : l'AC7 demande de reprendre les comportements `path_*`, et l'ensemble de touches
     reste le même entre « prêt » et « terminé » (Rule 130). Sans réponse, le plan applique (a).

## Hypothèses (choix prudents, à valider par l'orchestrateur)

- H1. Choisir une source avec `ctrl+o` ne lance rien : le sous-niveau revient à l'état prêt, la cible est
  conservée et la colonne est vidée. Seul `enter` lance un trace (contrainte S-03b : rien ne part sans action explicite).
- H2. `H` est grisé, et refusé avec sa raison dans le footer, quand la cible des résultats ne peut pas être tracée
  (`parsePathTarget` la refuse, par exemple un littéral IPv6). Avant, l'onglet se préremplissait et c'est `enter` qui refusait.
  Ordre des raisons dans `pathFromRoute()` : plateforme, puis ligne de route, puis cible.
- H3. Les préfixes de log `ERROR [netdiag/path] …` ne changent pas. Ils nomment le moteur, pas une surface clavier,
  et 6 tests les vérifient.
- H4. Aucun fichier `path_*.go` n'est renommé : moins de conflits, `git blame` conservé, et les listes des gardes
  restent valides. Seuls quelques tests sont renommés quand leur nom ment (« Tab », « Field »).
- H5. Le sous-niveau ne montre ni la barre de filtre de Diagnostics ni aucune barre : footer de 3 lignes dans tous
  les états. Le footer propre au trace relève de P2-03.
- H6. Dans l'état prêt, le footer n'affiche aucun statut : le header dit `enter Trace the path` et `Target`.
  Le texte du footer du trace relève de P2-03.
- H7. La garde « un trace tourne déjà » de `H` et de Trace Path devient inatteignable : une fois l'onglet retiré,
  un trace en cours implique `StateTrace`, et `esc` annule avant de pouvoir sortir. La garde est gardée par défense,
  avec un seul texte `reasonPathBusy` (« A path trace is already running — esc cancels it »).
  `reasonTraceBusy`, qui cite « the Path tab », disparaît.

## Exigences non fonctionnelles

- Aucun paquet ne part sans `enter` explicite (S-03b, D144 point 4) : ni `H`, ni un changement de source ne lancent.
- Rule 110 : l'état change dans `Update` seulement ; `launch` garde ses Cmd purs (`startPathRun`).
- Rule 124 : footer de Diagnostics à 3 lignes dans le sous-niveau, quel que soit l'état. La hauteur déclarée est
  égale à la hauteur rendue.
- Rule 130 : `H`, `enter` et `ctrl+o` sont grisés avec la même raison que le refus ; l'ensemble des touches est le même
  en état prêt et en état terminé.
- `mise run test-race` : le générateur (`gen`) et l'annulation (`releaseRun`) ne changent pas, et le canal reste celui
  d'aujourd'hui.

## Options

| | A. Le sous-niveau pilote `pathModel`, qui perd son champ (retenue) | B. Garder `PathModel.input` caché comme porteur de la cible |
|---|---|---|
| Principe | Nouvel état `pathReady` (cible connue, rien envoyé), `ready(tg)` remplace `prefill`, le clavier du sous-niveau est explicite | Le champ invisible garde la cible ; `enter` le relit |
| Pour | Une seule saisie (R2 supprimé), plus de « champ caché » qui reprend le clavier (`handleDoneError`), `InEditMode` simple, et un code mort retiré | Diff plus petit dans `path_model.go` |
| Contre | Une vingtaine de tests au niveau `pm` à réécrire (le helper `launched` concentre l'essentiel) | Un état `pathEditing` inatteignable mais vivant, la garde `TestTraceLevelLettersDoNotReachTheHiddenField` à maintenir, `barView` mort, et `unused` à surveiller |

Option recommandée : **A**. Elle tient en 3 étapes livrables qui gardent la suite verte, l'onglet restant présent
pendant l'étape 1.

## Modèle cible

```
PathModel.state : pathReady (zéro, cible connue ou vide, rien envoyé) → pathRunning → pathDone (résultat ou errText)
Model.state     : StateInput / StateResults --(Trace Path | H)--> StateTrace ; traceOrigin = l'état d'où l'on vient
StateTrace      : esc (trace en cours) → annule → pathDone ; esc (sinon) → m.state = traceOrigin, sans rien réinitialiser
                  enter (prêt|terminé) → pm.launch(pm.target) ; ctrl+o → picker ; ↑↓ pgup pgdown home end → table
```

Interfaces (signatures finales, fin de l'étape 3) :

```go
// path_model.go
const ( pathReady pathState = iota; pathRunning; pathDone )
func (pm *PathModel) ready(tg pathTarget)                         // remplace prefill : state=pathReady, target=tg, source=hostSource(), picker=nil, clearRun()
func (pm *PathModel) launch(tg pathTarget) (*PathModel, tea.Cmd) // inchangé + refuse silencieusement tg.Host == "" (défense)
func (pm *PathModel) handleKey(msg tea.KeyPressMsg) (*PathModel, tea.Cmd) // picker > ctrl+o > nav > running:esc > ready|done:enter
// trace_level.go
func (pm *PathModel) startFromForm(tg pathTarget) tea.Cmd         // ready(tg) puis launch(tg)
func (m *Model) openTrace(origin ViewState, tg pathTarget)        // m.traceOrigin=origin, m.state=StateTrace, pm.ready(tg)
func (m *Model) inTrace() bool                                    // m.activeTab == tabDiagnostics && m.state == StateTrace
func (m *Model) handleKeyTrace(msg tea.KeyPressMsg) (*Model, tea.Cmd)
func (m *Model) escFromTrace() (*Model, tea.Cmd)                  // annule si en cours ; sinon retour à traceOrigin
func (m *Model) traceShortcuts() shortcut.Shortcuts               // seul constructeur des raccourcis du sous-niveau
// path_from_route.go
func (m *Model) pathFromRoute() shortcut.Availability             // traceable() → ligne Local route → parsePathTarget(host)
func (m *Model) tracePathFromRoute() (*Model, tea.Cmd)            // H : openTrace(StateResults, tg), aucun Cmd de trace
// model.go
traceOrigin ViewState // StateInput ou StateResults
```

Raccourcis du sous-niveau (`traceShortcuts`) :
- sélecteur ouvert (un mode, la liste est remplacée) : `enter Trace from it`, `esc Close` ;
- trace en cours (un mode) : `esc Cancel the trace` ;
- prêt et terminé (même ensemble de touches) : `tab Switch tab`, `enter Trace the path` en état prêt ou
  `Trace again` en état terminé (grisé si `!launchable`), `ctrl+o Trace from…` (grisé si `!sourceChoosable`),
  `esc Back to the results` ou `Back to the form` selon `traceOrigin`, `? Help`.

---

## Étape 1 — Le sous-niveau devient autonome, et `H` y mène (l'onglet Path existe encore) — effort M

Hypothèse de taille : environ 150 lignes de production dans 6 fichiers, environ 15 tests nouveaux ou adaptés dans 3 fichiers.

Fichiers de production :
- `path_model.go` : ajouter `pathReady` (l'ajouter après `pathDone` dans cette étape, pour ne pas changer la valeur
  zéro `pathEditing` dont l'onglet a encore besoin). `ready(tg)` : comme `prefill`, mais avec `pm.target = tg`,
  `input.SetValue(tg.Host)`, `input.Blur()`, sans focus. `handleKey` : `pathReady` traité comme `pathDone`
  (`enter` lance `launch(pm.target)`, `esc` passe en édition, comportement de l'onglet).
  `handleDoneError` : `state = pathDone` (plus `pathEditing` ni `input.Focus()`), `errText` gardé.
  `launch` : pas d'effet si `tg.Host == ""`.
- `path_source_flow.go`, `chooseSource` : si `pm.target.Host != ""`, passer en `pathReady` en gardant la cible (H1) ;
  sinon, comportement actuel (édition, focus), qui ne sert plus qu'à l'onglet.
- `path_from_route.go` : `pathFromRoute()` vérifie, dans l'ordre, `m.traceable()`, la ligne `CheckRoute`, puis
  `parsePathTarget(host)` (H2, raison = `capitalize(err.Error())`). `tracePathFromRoute` : `buildTarget` (le log et
  l'erreur actuels sont gardés), puis `openTrace(StateResults, tg)`. Il ne retourne aucun Cmd de trace, et ne touche
  plus `activeTab`. Si un trace tourne déjà : `Warn(reasonPathBusy)` et aucun changement (H7).
  `prefill` est gardé tant que l'onglet existe (l'étape 3 le supprime).
- `trace_level.go` : `startFromForm` passe par `ready` puis `launch`. Nouveau `openTrace`. `handleKeyTrace` est
  explicite : `esc` → `escFromTrace` ; touche de navigation → table ; `pathSourceKey` → `pm.openPicker()` ;
  `enter` hors trace en cours → `pm.launch(pm.target)` ; tout le reste est ignoré. `escFromTrace` revient à
  `traceOrigin`, et n'appelle `restoreFormFocus` que pour `StateInput`. `traceShortcuts` suit le tableau ci-dessus.
- `trace_form.go` : `traceFromForm` appelle `openTrace(StateInput, tg)`, puis le lancement (`startFromForm`) et la notice.
- `model.go` : champ `traceOrigin`.
- `update.go`, `handleKey` : un sélecteur ouvert prend toutes les touches, `tab` compris, quand
  `(m.activeTab == tabPath || m.inTrace()) && pickerOpen()`. `InEditMode` : `if m.inTrace() { return m.pathModel.pickerOpen() }`.
- `header.go` : `GetFooterHeight` vaut 3 si `m.inTrace()`, sinon `3 + filterBar.ExtraHeight()`. `RenderFooter` n'ajoute
  la barre de filtre de Diagnostics que si `!m.inTrace()` (H5). Aide (`GetHelpContent`) : lignes `H (results)`,
  `enter / ctrl+o / esc (trace)`, et suppression de la ligne « enter / ctrl+r / ctrl+o … Do nothing ».
- `internal/ui/keymap/keymap.go` : nouvelle exception `ctrl+o` sur la surface `netdiag/trace`, avec sa raison en anglais
  (ouvre un sélecteur et ne change rien par lui-même ; l'action n'existe que sur ce sous-niveau ; aucune lettre du
  vocabulaire ne convient ; même accord que l'ancien onglet, pour la mémoire des doigts). L'exception `netdiag/path`
  reste jusqu'à l'étape 2.

Tests à écrire avant le code (paquet `netdiag`, sauf mention contraire) :
- `TestHOnTheRouteRowOpensTheTraceLevelWithoutTracing` (remplace `TestHOnTheRouteRowOpensPathPrefilledWithoutTracing`).
  Résultats sur `example.com`, ligne Local route, `H` → `state == StateTrace`, `activeTab == tabDiagnostics`,
  `pm.state == pathReady`, `pm.target.Host == "example.com"`, `pm.gen` inchangé, aucun appel au moteur (`fe`).
  Le titre affiche `Network › Path › example.com`, le header contient `Target`.
- `TestTraceLevelReadyEnterLaunches` : depuis l'état prêt, `enter` → `pm.running()`, `gen+1`, Cmd non nil (non exécuté).
- `TestTraceLevelDoneEnterTracesAgain` (Q2 a) : un trace terminé, puis `enter` → un nouveau run (`gen+1`, lignes vidées).
- `TestTraceLevelSourceKeyOpensThePicker` : en état prêt, `ctrl+o` → `pickerOpen()`, Cmd de lecture des sources.
  `TestTraceLevelPickerHoldsTabAndEsc` : `tab` ne change pas d'onglet, `esc` ferme le sélecteur et reste en `StateTrace`.
  `TestTraceLevelChoosingASourceReturnsToReadyWithoutTracing` (H1) : cible gardée, `pathReady`, rien lancé.
- `TestTraceLevelSourceKeyWhileRunningIsInert` : la touche est ignorée sans changement d'état.
  C'est l'adaptation de `TestTraceLevelNoRelaunchOrSourceKeyWhileRunning`.
- `TestTraceLevelFromResultsEscCancelsThenReturnsToTheResults` (AC4) : jeton `p` actif et ligne Route sélectionnée,
  puis `H`, `enter` et 2 sauts. `esc` → `pathDone`, toujours en `StateTrace` ; `esc` → `StateResults`, avec la même ligne
  sélectionnée, le jeton actif, le même verdict, `OriginView` inchangé et `results` intacts.
  `TestTraceLevelReadyEscGoesStraightBack` : `H` puis `esc` → `StateResults`.
- `TestTraceLevelEscCancelsThenGoesBackToTheForm` : existant, garder (origine formulaire).
- `TestTraceLevelHidesTheDiagnosticsFilterBar` (H5, Rule 124) : avec `p` actif, `H` → `GetFooterHeight() == 3`, et le footer
  rendu fait 3 lignes sans le jeton `problems`. Après `esc`, la barre revient (`3 + ExtraHeight`).
- `TestHIsGreyedWithThePlatformReasonAndSendsNothing` (AC5) : moteur non supporté → `H` grisé dans `GetShortcuts()`,
  `H` → footer `Warn` avec `pathUnsupportedReason`, état `StateResults`, `pm.gen` et `pm.target` inchangés.
- `TestHIsGreyedOnATargetThePathCannotTrace` (H2) : cible `::1` → raison du parseur, rien ouvert.
- `TestTraceLevelReadyAndDoneShareTheKeySet` (Rule 130, `testutil.ShortcutKeys`) ; `TestTraceLevelEscLabelFollowsTheOrigin`.
- `TestTraceLevelInEditModeOnlyWithThePicker`.
- `TestTraceLevelAFailedRunStaysInTheSubLevelWithItsError` : remplace `…DoesNotLeakTheKeyboardToTheHiddenField`.
  Un `pathDoneMsg` avec une erreur de résolution → `pathDone`, `errText` persistant en `LevelError`, et `enter` relance.
- Adapter : `path_from_route_test.go` (`TestHWhileAPathTraceRunsSwitchesTab…` devient `TestHWhileATraceRunsWarnsAndChangesNothing`,
  avec un état forcé), `path_from_route_extra_test.go` (`TestHPrefillForgetsThePreviousPathTarget` : la cible devient
  celle des résultats, et l'ancienne adresse est vidée), `path_platform_test.go` (`TestPathHPrefillsTheHostAndEnterStaysGreyed`
  devient le test AC5 ci-dessus), et `path_model_test.go` / `path_column_test.go` pour `handleDoneError` : là où l'on
  attendait `InEditMode()` et le focus, on attend `pathDone` et l'erreur.
- `internal/ui/keymap` : `TestTraceSourceKeyIsADeclaredException`, surface `netdiag/trace`, raison non vide
  (nouveau fichier `trace_source_exception_test.go`).

Docs, dans le même commit :
- `docs/architecture/network.md` : réécrire le paragraphe « `H` has a second producer » (l. 1025-1037 : sous-niveau,
  état prêt, `pathFromRoute` à 3 raisons) et le paragraphe du sous-niveau (l. 1039-1063 : `enter`, `ctrl+o`, `esc` vers
  l'origine, `traceOrigin`, pas de barre de filtre).
- `docs/architecture/app-shell.md` l. 584 : exception `netdiag/trace`.
- `.claude/rules/tui-layout.md` l. 110 : citer `netdiag/trace`.
- `docs-site/docs/reference/keybindings.md` : ligne `H` et touches du sous-niveau.
- Entrée de backlog D153 : brouillon en bas de ce plan, à archiver par `archivist`.

Commande ciblée : `go test ./internal/ui/netdiag/ -run 'Trace|TestH|Path' -count=1 && go test ./internal/ui/keymap/ -count=1`

Critère de fin : tous les tests ci-dessus sont verts, `go test ./internal/ui/netdiag/ ./internal/ui/keymap/ ./internal/app/` passe,
et `golangci-lint run ./internal/ui/...` est propre. L'onglet Path marche encore (ses tests passent).

## Étape 2 — L'onglet disparaît de l'interface (4 onglets) — effort M

Dépend de l'étape 1. Hypothèse de taille : environ 120 lignes de production retirées dans 7 fichiers, 9 fichiers de test adaptés.

Fichiers de production :
- `model.go` : supprimer `tabPath` ; `tabCount = 4` ; commentaires de `activeTab` et de `StateTrace`.
- `update.go` : supprimer les branches `tabPath` (`InEditMode`, `handleKey`, `handlePaste`) ; la vérification du
  sélecteur devient `m.inTrace() && pickerOpen()` ; commentaire « all five » remplacé par « all four, and the trace sub-level ».
- `view.go` : supprimer `case tabPath`.
- `header.go` : supprimer les branches `tabPath` de `GetShortcuts`, `GetHeaderInfo`, `GetFooterHeight`, `RenderFooter`
  et `activeFooter` ; `RenderTabs` reçoit 4 libellés ; supprimer `pathFooterHeight`. Aide : `Description` devient
  « Four-tab view … ; Diagnostics also traces the path to the target (Trace Path, or H on the Local route row) » ;
  ligne `tab / shift+tab` avec 4 onglets ; suppression des 3 lignes `(Path)` ; la section « Path Tab — How it works »
  devient « Diagnostics Tab — Tracing the path », avec un corps qui ne parle plus de saisir dans un champ
  (la cible vient du formulaire, `enter` dans le sous-niveau lance) ; « the Path tab sends » devient « the path trace sends ».
- `trace_level.go` / `path_from_route.go` : supprimer `reasonTraceBusy` et n'utiliser que `reasonPathBusy` (H7).
- `internal/ui/keymap/keymap.go` : supprimer l'exception `netdiag/path`.

Tests :
- `netdiag_path_tab_test.go` : renommer en `trace_routing_test.go` (`git mv`) et remplacer le helper `onPathTab` par
  `traceLevelModel`. Correspondances :
  `TestPathIsTheFifthTab` → `TestTabCountIsFour` ;
  `TestPathIsReachedByTabAloneAndCyclesBothWays` → `TestTabCyclesTheFourTabsBothWays` (aussi `model_test.go:80-104`) ;
  `TestTabLeavesPathWhileTheFieldIsEditing` → `TestTabLeavesTheTraceLevelAndComesBack` ;
  `TestPathRenderIsTheTableInEveryState` → sous-niveau prêt, en cours, terminé ;
  `TestPathFooterHeightIsFiveInEveryState` → `TestTraceLevelFooterHeightIsThreeInEveryState` (sélecteur compris) ;
  `TestPathFooterHoldsTheBarAndTheTabLabel` → `TestTabRowListsFourTabsAndNoTargetBar` ;
  `TestPathEditModeFollowsTheField` → couvert par `TestTraceLevelInEditModeOnlyWithThePicker` ;
  `TestPathPasteReachesTheField` et `…DroppedWhileTracing` → `TestTraceLevelDropsAPaste` (en état prêt et en cours) ;
  `TestRunningCoversAPathTrace`, `TestWantsBlinkFollowsATraceOnThePathTab`, `TestAFrameBlinksThePathDot`,
  `TestPathMessagesAreRoutedWhateverTabIsOnScreen`, `TestPathFollowsTheWindowSize` et
  `TestPathFooterMessageIsClearedByItsOwnTimer` → mêmes assertions depuis le sous-niveau ;
  `TestPathFieldCursorBlinks` et `TestPathModelForwardsTheCursorBlinkToTheField` → `TestFormTargetCursorBlinksAfterLeavingTheTrace` ;
  `TestHelpDescribesFiveTabsAndPathKeys` → `TestHelpDescribesFourTabsAndTheTraceKeys` (pas de « Path tab », ni de
  « Five-tab ») ; `TestPathShortcutsAndHeaderComeFromThePathModel` et `TestPathEnterGreysOutWithoutChangingTheKeySet`
  → la même chose sur `traceShortcuts` ; `TestPathResolveTimeoutComesFromCheckSettings` : garder.
- `path_column_layout_test.go` (`longRun`), `path_container_layout_test.go` (l. 70 et 213) : construire avec
  `traceLevelModel(t, longHost)` au lieu de `onPathTab` + `Type`. Les cibles longues passent par `targetInput`
  (vérifier `CharLimit` 256, qui suffit pour un nom de 253 caractères).
- `path_source_help_test.go` : nouveau titre de section. `path_bridge_help_test.go` et `path_engine_help_test.go` :
  vérifier que le texte attendu reste dans la section renommée.
- `trace_level_test.go`, `TestTraceLevelTableSizeMatchesThePathTab` → `…MatchesThePathModel` (assertion inchangée).
- `trace_form_validation_test.go`, `TestTraceFormValidationTextMatchesThePathTab` → `…MatchesTheTraceParser`.
- `internal/ui/keymap/path_source_exception_test.go` : supprimer après avoir déplacé son assertion dans
  `trace_source_exception_test.go`, qui vérifie aussi l'absence de `netdiag/path` (aucune perte de comportement).
- `internal/app/bar_attach_test.go` : `TestPathTabBarIsAttachedToTheFrame` → `TestDiagnosticsFilterBarIsAttachedToTheFrame`
  (des résultats injectés et `p`, ou `/` sur Diagnostics) ; `TestPathTabBarStaysJoinedWhileTheBodyIsTrimmed` →
  `TestInterfacesSearchBarStaysJoinedWhileTheBodyIsTrimmed` (`tab tab /`, h de 12 à 24, préfixe `│ /`).
  L'intention (DEVDESK-457) est gardée sur une autre barre.
- `internal/app/netdiag_path_guard_test.go` : renommer `TestActivityGuardScansThePathTabSources` en
  `TestActivityGuardScansTheTraceSources` et mettre à jour son commentaire. Les listes de
  `activity_guard_test.go:253-256` ne changent pas (aucun fichier supprimé ni renommé), mais leur commentaire est mis à jour.

Docs, dans le même commit :
- `network.md` : l. 883-890 (puce « Path tab » → « trace sub-level », `tabCount = 4`) ; titre l. 1300 → « The trace
  sub-level — streaming, cancellation, resolution » ; paragraphe « Coupling with the Path tab » (l. 1088-1093) :
  le remplacer par « one model, one place » ; occurrences « the Path tab » l. 970, 1115, 1217, 1270-1280, 1585.
- `.claude/CLAUDE.md`, résumé : « + a Path tab (native path trace … » → « + a path trace under Diagnostics (Trace Path
  button, or `H` on the Local route row; `enter` starts it, `ctrl+o` picks the source …) » ; garder les mentions
  DEVDESK-333/334/335.
- `docs-site/docs/explanation/network.md` et `docs-site/docs/reference/keybindings.md` (l. 83, section « Path tab »
  l. 97-114 → « Diagnostics — trace sub-level »).
- `docs/architecture/ui-components.md` l. 732 (supprimer la puce « the Path tab's target field ») et l. 807.
- `.claude/rules/tui-forms.md` l. 368 : retirer « Path tab field » de la liste hors périmètre.
- `.claude/rules/tui-layout.md` : retirer `netdiag/path`.
- `docs/architecture/app-shell.md` l. 584 : une seule exception, `netdiag/trace`.

Commande ciblée : `go test ./internal/ui/netdiag/ ./internal/ui/keymap/ -count=1 && go test ./internal/app/ -run 'Bar|Netdiag|ActivityGuard' -count=1`

Critère de fin : `grep -rn "tabPath" internal/` ne renvoie rien ; `grep -rn "Path tab" internal/ docs/architecture .claude/CLAUDE.md`
ne renvoie que des mentions historiques volontaires (backlog) ; la suite est verte.

## Étape 3 — `PathModel` perd son champ cible — effort M

Dépend de l'étape 2. Hypothèse de taille : environ 90 lignes de production retirées, environ 25 tests de niveau `pm`
adaptés, surtout par le helper.

Fichiers de production :
- `path_model.go` : supprimer `input`, `pathEditing` (`pathReady` devient la valeur zéro, en tête de l'énumération),
  `pathInputCharLimit`, `InEditMode`, `handleEditKey`, `handlePaste`, `updateInput`, `launchFromInput`, la branche
  `tea.PasteMsg` de `update` et le repli `pm.input.Update` (il ne reste que `pm.footer.Handle(msg)`).
  `handleKey` : le sélecteur, puis `ctrl+o`, la navigation, `esc` en cours (annule), et `enter` en état prêt ou
  terminé (lance). `esc` hors trace en cours n'est pas traité ici (c'est `Model` qui le traite).
  `resize` ne dimensionne plus de champ. `newPathModel` ne crée plus de `textinput`.
- `path_from_route.go` : supprimer `prefill`.
- `path_source_flow.go` : `chooseSource` → `pathReady` sans condition (cible gardée), et plus de focus.
- `path_view.go` : supprimer `pathBarLabel`, `barView` et `shortcuts()` (remplacé par `Model.traceShortcuts`).
- `trace_level.go` : `startFromForm` sans `_ = pm.prefill`. `handleKeyTrace` peut alors déléguer à `pm.update(msg)` pour
  tout sauf `esc` hors trace en cours, et doit toujours ignorer les lettres.
- `update.go` : repli final `m.pathModel.footer.Handle(msg)` à la place de `m.pathModel.update(msg)` (il n'y a plus de
  curseur à faire clignoter dans `pm`).

Tests :
- `path_model_test.go` : le helper `launched(t, pm, target)` devient `pm.ready(pathTarget{Host: target})` puis
  `pmFeed(Key("enter"))`, ce qui garde intacts une quinzaine de tests (reconciliation, hops, résolution, annulation,
  générations, contexte, header). Correspondances :
  `TestPathNewModelIsAnEditableEmptyTable` → `TestPathNewModelIsAReadyEmptyTable` ;
  `TestPathBarShowsTheTargetFieldAndTheLaunchKeyIsListed` → couvert à l'étape 1 (`enter Trace the path` en état prêt) ;
  `TestPathRefusedPortIsNotATraceState` et `TestPathBareTargetsAreNotPortErrors` → au niveau du formulaire, avec le même
  tableau de cas, dans `trace_form_validation_test.go` (`traceFromForm`) ;
  `TestPathPasteReachesTheFieldWhileEditing` et `TestPathRunningIgnoresTypingAndPaste` → `TestPathIgnoresLettersAndPasteInEveryState` ;
  `TestPathDoneEnterRelaunchesAndEscEdits` → `TestPathDoneEnterRelaunches` (le retour par `esc` est couvert à l'étape 1) ;
  `TestPathShortcutsPerState` → couvert par les tests `traceShortcuts` de l'étape 1.
- `path_source_flow_test.go` : la fabrique monte un `pm` prêt sur une cible ; `TestPathSourceChangeClearsTheColumn` et
  `TestPathRelaunchAndPrefillResetTheContainer` (« prefill » → « ready ») attendent `pathReady`, cible gardée.
  `TestPathEscKeepsTheSourceAndTheRows` : `esc` d'un trace en cours.
- `path_platform_test.go` : `TestPathOpensWithTheReasonAtWarning` → `TestTraceLevelCannotOpenOnAnUnsupportedPlatform`
  (le bouton et `H` sont refusés avec la raison) ; `TestPathSupportedPlatformHasNoStatusWhenEditing` → `…WhenReady` ;
  les autres passent par `ready` + `enter` / `ctrl+o`.
- `path_column_test.go` (l. 248, 317), `path_column_layout_test.go` (l. 134), `path_flow_test.go` (l. 27) et
  `path_noresponse_test.go` (l. 307) : remplacer `Type(...)` par `ready(...)` et `InEditMode()` par `state == pathDone`.
- `trace_level_test.go`, `TestTraceLevelLettersDoNotReachTheHiddenField` → `TestTraceLevelLettersAreInert`
  (état, cible et `gen` inchangés).

Docs : `network.md`, retirer les mentions du champ (`prefill`, « hidden field », `handleDoneError` qui redonne le focus,
« Target › » dans la barre) ; décrire `pathReady`.

Commande ciblée : `go test ./internal/ui/netdiag/ -count=1 && go test -race ./internal/ui/netdiag/`

Critère de fin : `grep -n "pathEditing\|barView\|launchFromInput\|prefill\|pm.input" internal/ui/netdiag/*.go` ne renvoie rien ;
lint propre (`unused`) ; suite verte, race comprise.

Parallélisable : une fois la production de l'étape 3 posée, l'adaptation des tests peut être répartie entre 2 `coder`
sur des fichiers disjoints : (a) `path_model_test.go` et `path_flow_test.go` ; (b) `path_source_flow_test.go`,
`path_platform_test.go` et `path_column*_test.go`.

## Étape 4 — Ouverture depuis Status (AC6) et vérification finale — effort S

Dépend de l'étape 2. Avec la réponse (b) à la question 1, elle peut se faire en parallèle de l'étape 3 (tests seulement,
fichiers disjoints).
- 4a (tests) : `netdiag/origin_trace_test.go` : `NewWithTarget(cfg, {Host:"example.com", Port:443})` avec
  `OriginView = command.ViewStatus`, résultats injectés (le même helper que `routeResults`) → 4 onglets ; Diagnostics
  prérempli (`targetInput`, `portInput`) ; `H`, `enter`, `esc`, `esc` → `StateResults` ; puis `esc` → Cmd qui produit
  `BackToOriginMsg{Origin: ViewStatus}`. `internal/app/netdiag_test.go` : garder `TestEscFromNetdiagReturnsToItsOrigin`
  et vérifier que la vue reçue a `tabCount` onglets (via `RenderFooter` : 4 libellés, pas de « Path »).
- 4b (seulement si la réponse à Q1 est (a)) : `update.go handleKeyInput`, `esc` avec `OriginView != ""` → `BackToOriginMsg` ;
  `header.go`, raccourci `esc Go back` sur le formulaire dans ce cas ; aide ; `network.md` ; test
  `TestEscOnAPrefilledFormGoesBackToTheOrigin`.
- Final : `mise run ci` (fmt-check, vet, lint, build, test, test-race). Le trace ne tourne que sous Linux (D149 : la CI
  ne lance pas les tests). Lancer `mise run test` et `mise run test-race` sous Linux ou WSL2 ; sous Windows, comparer avec
  les 12 paquets déjà en échec sur `origin/main` (note de DEVDESK-470).

Critère de fin : les AC 1 à 7 sont cochés, `mise run ci` est vert sous Linux, et l'entrée D153 a été passée à `archivist`.

## Dépendances

`1 → 2 → 3` en séquence (mêmes fichiers). `4a` après 2, en parallèle de 3. `4b` après 2 et seulement si Q1 = (a).
Une PR, fusionnée en squash : le commit unique sur `main` porte le code, les tests et les docs (AC7 : « même commit »).

## Risques

- R1 (moyen) : couplage du sélecteur et de `tab`. Un sélecteur ouvert doit garder `tab`, sinon on change d'onglet avec un
  sélecteur ouvert qui attend une liste. Couvert par `TestTraceLevelPickerHoldsTabAndEsc` et par `Init` (`closePicker`).
- R2 (moyen) : l'état gardé au retour (Rule 138). `resetToForm` ne doit pas être appelé, et le filtre et la sélection
  doivent survivre. Couvert par le test AC4 « résultats ».
- R3 (faible) : hauteur du footer (Rule 124). La barre de filtre de Diagnostics fuyait dans le sous-niveau
  (`GetFooterHeight` = `3 + ExtraHeight` en `StateTrace`). C'est un défaut latent de P2-01, corrigé à l'étape 1.
- R4 (faible) : à l'étape 1, l'onglet et le sous-niveau partagent `pm`. Un passage par l'onglet peut laisser `pm` en
  `pathEditing` puis revenir au sous-niveau ; `handleKeyTrace` est explicite (aucune lettre n'est transmise), donc il
  n'y a pas de fuite. L'état disparaît à l'étape 3.
- R5 (faible) : `bar_attach_test` (DEVDESK-457) utilisait la barre Path. L'intention est reprise sur deux autres barres,
  sans quoi la garde de jointure ne couvrirait plus le cas « corps rogné ».
- R6 (faible) : réécriture de l'aide. Les tests d'aide (`path_*_help_test.go`) vérifient des phrases précises. Garder
  ces phrases dans la section renommée.
- Points irréversibles : aucun. Pas de schéma de config, pas d'API publique, pas de fichier utilisateur. La décision
  produit (une seule saisie, plus d'onglet) est réversible par un revert.

## Brouillon d'entrée de backlog (pour `archivist`, pas d'ADR : local et réversible)

**D153 — DEVDESK-471 (P2-02) : l'onglet Path est retiré ; le trace n'existe plus que dans le sous-niveau de Diagnostics.**
Décidé : (1) La vue réseau a quatre onglets : Diagnostics, Ports, Interfaces, Forward. (2) `H` sur la ligne Local route
ouvre le sous-niveau (`StateTrace`) sur la cible des résultats, à l'état prêt, et ne lance jamais rien ; `enter` lance
(D144 point 4 conservé, son « onglet Path » remplacé). (3) Dans le sous-niveau, `enter` lance ou relance, `ctrl+o` ouvre
« Trace from » (exception déclarée, surface `netdiag/trace`, qui remplace `netdiag/path`), et `esc` annule le trace en
cours puis revient à l'écran d'origine (formulaire ou résultats) avec son état. Remplace le point 2 de D152 ; le coût
accepté au point 1 de D152 (champ de l'onglet écrasé) disparaît ; remplace le point 1 de D144. (4) `PathModel` n'a plus
de champ cible : la cible vient du formulaire, saisie une seule fois. (5) `H` est grisé, avec sa raison, sur une
plateforme qui ne trace pas et sur une cible que le trace refuse. (6) Changer de source revient à l'état prêt et ne lance
rien. Hypothèses : H1 à H7 du plan `.claude/plans/devdesk-471-remove-path-tab-plan.md`. **Plane :** DEVDESK-471.
Sources : ce plan, D144, D150 point 5, D152, l'audit `.claude/plans/net-view-ux-audit.md` (option P2).
