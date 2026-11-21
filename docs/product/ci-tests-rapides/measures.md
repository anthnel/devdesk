# Mesures : où passe le temps de `mise run ci` (tests)

Date : 2026-11-21. Mesure seule : aucun fichier du dépôt modifié, aucun commit.
Périmètre : les deux étapes de test de `mise run ci` (`test` = `go test -v ./...`, `test-race` = `go test -race ./...` avec `CGO_ENABLED=1`). Les étapes `fmt-check`, `vet`, `lint`, `build` ne sont PAS mesurées ici.

## Matériel et conditions

- 22 cœurs logiques (`nproc`), 30 Gio de RAM (environ 15 Gio disponibles), Linux 6.12 (Debian 13), go1.27.4 linux/amd64, gcc 14.2.0.
- Charge au démarrage : load average 0,30 (run 1), 4,2 (run 2), 12,8 (run 3, effet des runs précédents et de son propre `go test`), 1,3 (runs 2 cœurs). Machine non dédiée (2 sessions ouvertes), pas d'autre charge notable observée.
- `GOCACHE` jetable dans le scratchpad (1,4 Gio avec race et sans race), `GOMODCACHE` par défaut non touché.
- 91 paquets, 19 521 événements de test (parents et sous-tests), 0 `t.Parallel()` dans tout le dépôt (grep sur `*_test.go`).
- Simulation « petit runner » : `taskset -c 0,1` (2 cœurs). Go lit l'affinité, donc `GOMAXPROCS` et `-p` valent 2. Ce n'est qu'une approximation d'un runner 2 vCPU (mêmes disque et RAM que l'hôte). La machine réelle de la CI n'est pas connue.

## Runs

| # | Commande | Durée (mur) | Remarques |
|---|----------|-------------|-----------|
| 1 | `go test -race -run '^$' ./...` (cache vide, 22 c) | 30,3 s | Compilation seule instrumentée, exit 0. |
| 2 | `go test -race -count=1 -json ./...` (cache chaud, 22 c) | 328,6 s | 0 échec, 24 skip. `internal/app` : 326,8 s. |
| 3a | `go test -run '^$' ./...` (cache sans race vide, 22 c) | 15,2 s | Compilation seule non instrumentée. |
| 3 | `go test -count=1 -json ./...` (compilation chaude, 22 c) | 303,4 s | 0 échec, 24 skip. `internal/app` : 301,9 s. |
| 4a | `go test -race ./...` (sans `-count=1`, résultats non encore en cache) | 328,8 s | Réexécute tout : le run 2 avec `-count=1` ne stocke pas de résultat. |
| 4b | `go test -race ./...` (identique, immédiatement après 4a) | 3,2 s | 89 paquets `(cached)` (les 2 restants n'ont pas de tests). |
| 5r | `taskset -c 0,1 go test -race -count=1 -json ./...` (cache chaud) | 529,8 s | 2 cœurs. 0 échec. |
| 5n | `taskset -c 0,1 go test -count=1 -json ./...` (cache chaud) | 302,0 s | 2 cœurs. 0 échec. |
| 6r | `taskset -c 0,1 go test -race -run '^$' ./...` (cache vide) | 116,2 s | Compilation seule instrumentée sur 2 cœurs. |
| 6n | `taskset -c 0,1 go test -run '^$' ./...` (cache vide) | 57,0 s | Compilation seule non instrumentée sur 2 cœurs. |

Aucun run n'a échoué, aucun problème d'environnement (cgo et gcc présents). Les JSON bruts sont dans le scratchpad de la session (`race.json`, `norace.json`, `race2c.json`, `norace2c.json`) ; ils ne sont pas versionnés.

## Top 10 paquets (champ `Elapsed` des événements paquet)

Run 2 (race, 22 c) ; somme des `Elapsed` de tous les paquets : 1 057,5 s.

| Paquet | Race, 22 c | Sans race, 22 c |
|--------|-----------|-----------------|
| internal/app | 326,8 s | 301,9 s |
| internal/scan | 144,8 s | < 1 s (somme des tests : 0,9 s) |
| internal/ui/oci_resources | 130,4 s | 18,9 s |
| internal/agentusage | 105,7 s | 12,0 s |
| internal/ui/agentusageview | 55,7 s | 10,8 s |
| internal/ui/netdiag | 41,1 s | 12,3 s |
| internal/nexusbench | 21,4 s | 9,1 s |
| internal/ui/keymap | 19,8 s | 2,5 s (somme des tests) |
| internal/ui/teaexec | 17,3 s | 14,3 s |
| internal/ui/containers | 12,8 s | 3,0 s |

Trois paquets les plus lents cumulés : race 601,9 s (57 % de la somme 1 057,5 s) ; sans race 335,1 s (77 % de la somme 437,6 s). Les `Elapsed` de paquets s'additionnent sur des paquets exécutés en parallèle : la somme n'est pas une durée mur.

Sur 2 cœurs, le classement et les ordres de grandeur sont les mêmes (race : `internal/app` 326,1 s, `internal/scan` 144,1 s, `oci_resources` 124,2 s, `agentusage` 100,3 s ; sans race : `internal/app` 301,0 s).

## Top 10 tests (tests de premier niveau ; un test parent inclut ses sous-tests)

Race, 22 c :

| Durée | Test |
|-------|------|
| 60,5 s | internal/app TestAnInvalidUnusedCandidateWarnsAtStartup |
| 40,4 s | internal/app TestThemeAutoFollowsTheTerminalAnswer |
| 30,3 s | internal/app TestThemeAutoCandidateThatCannotBeUsedGivesDefaultAndOneWarning |
| 29,4 s | internal/scan TestPlumberIsResolvedLikeTheOtherTwo (0,12 s sans race) |
| 26,3 s | internal/scan TestAutoKeepsTheOldBinaryFirstResolution (0,10 s sans race) |
| 18,2 s | internal/ui/oci_resources TestABatchAnnouncesAndAnswersForEveryImage (0,06 s sans race) |
| 16,4 s | internal/agentusage TestFullReadBoundsNoIDEntriesAcrossFiles (1,55 s sans race) |
| 16,4 s | internal/agentusage TestToolEntriesCapHoldsAcrossMerges (1,69 s sans race) |
| 15,3 s | internal/scan TestNewScannerHonoursEveryToolsSource |
| 15,2 s | internal/scan TestWithNoBinaryTheDockerImageIsTheFallback |

Sans race, 22 c : les trois mêmes tests de `internal/app` en tête (60,5 s, 40,3 s, 30,2 s), puis `internal/ui/teaexec` TestARedrawFollowsAnInstantChild (11,9 s) et sept tests de `internal/app` à 10,1 à 10,6 s.

Répartition dans `internal/app` (707 tests de premier niveau) : 30 tests de plus d'1 s totalisent 280 s sans race (32 tests, 284 s avec race) ; ceux de plus de 5 s totalisent 243 s dans les deux cas. Les quelque 675 autres tests pèsent environ 21 s sans race, 41 s avec race. Le temps de `internal/app` ne dépend presque pas de `-race` (301,9 s contre 326,8 s) : c'est de l'attente réelle, pas du calcul.

Cause constatée de l'attente dans `internal/app` : un dump de goroutines (`-timeout 4s` sur `TestThemeAutoTimeoutFallsBackToDarkSilently`) montre `testutil.Msgs` bloqué dans un `tea.Tick` réel. Les cas de thème durent chacun 10,09 s. Je n'ai pas identifié quel délai précis de 10 s est en jeu, ni diagnostiqué les autres tests lents.

Cause constatée de la lenteur propre à `-race` dans `internal/scan` : `installToolAt` copie le binaire de test (`os.Executable`) comme faux outil, puis le code l'exécute. Le même écart 100 à 300 fois plus lent s'observe dans `oci_resources`. Le mécanisme précis (taille du binaire instrumenté, démarrage du runtime race à chaque exécution) n'est pas isolé.

## Plafond de parallélisme

- Un seul paquet domine : `internal/app` dure 301,9 s sans race et 326,8 s avec race, soit 99,5 % du temps mur dans les deux cas (303,4 s et 328,6 s). Il dépasse à lui seul le seuil de 3 min, et sa durée n'est quasi pas sensible au nombre de cœurs ni à `-race` (301,0 s sur 2 cœurs).
- Aucun test n'appelle `t.Parallel()` : à l'intérieur d'un paquet tout est séquentiel. Le plafond « par paquet » est donc d'environ 302 s (sans race) et 327 s (race).
- Le plafond « par test » (si les tests d'`internal/app` s'exécutaient en parallèle) serait le test le plus long : 60,5 s (6 sous-tests séquentiels de 10,09 s).
- `internal/app` importe 84 des 91 paquets (`go list -deps -test`) : toute modification de code invalide son résultat en cache.

## Mesuré

- `test` + `test-race` à chaud : 632 s (303,4 + 328,6) sur 22 cœurs ; 832 s (302,0 + 529,8) sur 2 cœurs.
- Compilation à froid : race 30,3 s (22 c), 116,2 s (2 c) ; sans race 15,2 s (22 c), 57,0 s (2 c).
- Cache de résultats Go, rien de changé : 3,2 s au lieu de 328,8 s.
- Aucun test en échec, aucune instabilité observée sur ces 5 exécutions complètes (0 échec chaque fois). Un échantillon aussi court ne démontre pas l'absence d'instabilité.
- 24 tests ignorés (identiques avec et sans race), tous par choix de l'environnement : 9 de `nexusbench` (grille `DEVDESK_NEXUS_BENCH` et cluster, jamais en CI), `nettrace` TestLiveTraceToCloudflare (réseau, `DEVDESK_NETTRACE_LIVE`), 2 `wslenv` TestLive* (WSL), `agentusage` TestSessionTotalsMatchCcusage (`DEVDESK_CCUSAGE_REF`, ccusage), `app` TestMeasureRedrawCost (`DEVDESK_MEASURE_REDRAW`), `nexusfixture` (fixture `pending`), plus des « helpers » exécutés en sous-processus (`netproxy`, `netcheck`, `status`, `credentials`, qui se mettent en skip quand ils ne sont pas appelés par leur parent). Aucun test n'a eu besoin de Docker, de cluster ou de secret pour passer sur cette machine ; je n'ai pas vérifié si Docker ou skopeo sont présents (certains tests `nexusbench` se mettent en skip sans eux).
- Non vérifié : que les tests ne touchent pas à `~/.devdesk/` ni au keyring sur cette machine (les fixtures `isolateHome` et `fakekeyring` sont documentées, pas contrôlées ici).

## Estimé (non mesuré)

Les chiffres suivants dérivent des mesures ci-dessus par calcul ; aucun n'a été exécuté.

- **Compilation instrumentée contre exécution** : sur 22 cœurs la compilation race à froid vaut 30 s contre 329 s de run à chaud, soit environ 9 %. Sur 2 cœurs, 116 s contre 530 s, soit environ 18 %. Cumul à froid sur 2 cœurs, supposé additif : 57 + 302 + 116 + 530, soit environ 1 005 s (environ 16,7 min), avant `fmt-check`, `vet`, `lint`, `build`. C'est le même ordre de grandeur que les « environ 15 min » observés.
- **Cache de compilation persistant** : gain au plus la compilation à froid, soit environ 45 s sur 22 cœurs (race + sans race) et environ 173 s sur 2 cœurs, soit moins de 5 % et environ 17 % de la durée à froid. Un cache de résultats n'aide pas `internal/app` (84 dépendances internes sur 91), c'est-à-dire le paquet qui domine ; il n'économise que des paquets non touchés (cas limite sans aucun changement : 3,2 s).
- **Découpage par paquet** : ne peut pas descendre sous le plafond de `internal/app`, environ 327 s en race et 302 s sans race, déjà quasi égal au temps mur de 22 cœurs. Gain estimé nul tant que `internal/app` reste un bloc. Un découpage au niveau des tests (`-run`, ou `t.Parallel` dans `internal/app`) ramènerait ce paquet vers 60 s au mieux (test le plus long) ; le plafond suivant en race serait `internal/scan` (144,8 s) puis `oci_resources` (130,4 s) : environ 2,5 à 3 min de temps mur sur beaucoup de cœurs, sous réserve que les attentes soient réellement parallélisables. Sur 2 cœurs le temps serait borné par le travail de calcul des autres paquets (non mesuré en CPU) et le gain serait plus faible.
- **Abandon de la double exécution** (ne garder que `test-race`, qui exécute les mêmes tests) : environ 303 s de moins, soit 48 % de la partie test sur 22 cœurs (632 s vers 329 s) et 36 % sur 2 cœurs (832 s vers 530 s), plus 15 à 57 s de compilation non instrumentée. Contrepartie non évaluée : le run sans race n'est plus exécuté (comportements différents hors race, tests qui ne s'exécutent qu'en l'absence du détecteur), et `-v` disparaît.

## Conclusion factuelle

- Ce n'est pas la compilation instrumentée qui domine : elle pèse 30 s (22 c) à 116 s (2 c), contre 329 à 530 s d'exécution.
- C'est l'exécution, et surtout un seul paquet : `internal/app` occupe 99,5 % du temps mur de chaque run (302 s sans race, 327 s avec race), par des attentes réelles (`tea.Tick` dans `testutil.Msgs`) et non du calcul. Trois tests de ce paquet font 131 s à eux seuls.
- Le « principalement dans test-race » est vrai en part absolue sur 2 cœurs (530 s sur 832 s) mais pas sur ce poste 22 cœurs, où `test` (303 s) et `test-race` (329 s) pèsent presque autant, car le même paquet `internal/app` bloque les deux.
- Le `-race` ajoute surtout du temps de calcul à `scan`, `oci_resources`, `agentusage`, `agentusageview`, `netdiag` (≈ 600 s de `Elapsed` cumulé en plus hors `internal/app` (1 057,5 - 437,6 - 25)), qui se parallélisent derrière `internal/app` sur beaucoup de cœurs mais coûtent cher sur 2 cœurs (530 s contre 329 s).

## Points ouverts

- `fmt-check`, `vet`, `lint`, `build` non mesurés ; la durée du runner CI réel (cœurs, disque) est inconnue ; la simulation 2 cœurs n'est qu'une approximation.
- Origine exacte des 10,09 s par cas de thème dans `internal/app` et du facteur 100 à 300 de `-race` dans `internal/scan` et `oci_resources` : non diagnostiquées.
- Instabilité : un seul échantillon par configuration, à compléter par des répétitions pour conclure.
- Isolation de `~/.devdesk` et du keyring pendant les tests non contrôlée.
