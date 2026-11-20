# Note de décision : Dagger pour les vérifications locales et CI (spike)

Date : 2026-11-20. Source : `stories.md` (S1, S2, S4, S5), `brief.md`, `research.md` (prix relevés le 2026-11-11), comptes rendus de S1 et S2, entrée du journal « Dagger for local checks and CI: spike scope » (2026-11-11).

Règle de lecture : tout chiffre vient des comptes rendus de S1 et S2. Ce qui manque est écrit « non mesuré » ou « non consigné ». Aucune mesure n'a été ajoutée à la rédaction de cette note.

## 1. Résumé et verdict

**Verdict : non tranché, à décider par l'utilisateur.**

Pourquoi « adopté » est exclu : S4 n'a pas été faite (comptes Entire et Dagger Cloud non vérifiés, aucun essai). Le critère « zéro coût, Entire compris » est donc non mesuré, et `stories.md` (S5.2) n'autorise « adopté » que si les six critères sont atteints.

Ce que les mesures disent, pour la boucle locale seulement :

- Dagger 1.0 bêta (v1.0.0-beta.27) exécute les six vérifications dans un conteneur Linux, `test-race` compris, depuis un PC Windows. Verdicts identiques à la CI GitHub sur les 4 commits sains.
- À chaud, `dagger check` dure 298,7 s contre 410,5 s pour `mise run ci` (rapport 0,73, seuil 1,2). La référence `mise run ci` ne tourne pas jusqu'au bout sous Windows (voir section 6).
- Le module écrit à la main fait 82 lignes (seuil 150).
- Réserves : verdict GitHub du commit en échec déduit et non obtenu par un run, divergence `mise.toml` / `go.mod` non vérifiée dans le conteneur, 4 tests sautés non expliqués, bêta instable (voir sections 7 et 8).

Deux issues possibles, au choix de l'utilisateur :

- **Abandonner** : on s'en tient aux mesures et à `mise run ci`. Voir section 10.
- **Faire S4** (30 min maximum, sans carte) pour mesurer le critère manquant. Voir section 10.

## 2. Tableau des six critères d'adoption

Formulations de S5.1. Résultats : atteint, atteint avec réserve, non atteint, non mesuré.

| # | Critère | Seuil | Valeur mesurée | Source | Résultat |
|---|---|---|---|---|---|
| 1 | Mêmes verdicts que la référence GitHub Actions sur 5 commits | zéro écart non expliqué | 4 commits sains : Dagger six OK, GitHub six success, aucun écart. Commit en échec `a70bc1c0` : Dagger `test` et `test-race` en ERROR ; GitHub : verdict déduit (non lancé) : `test` échoue, `test-race` sauté. Un écart, expliqué : Dagger exécute les vérifications indépendamment | Section 4 (S2) | **Atteint avec réserve.** S2.1 demande un run GitHub sur le commit en échec ; le verdict de référence est déduit, pas observé |
| 2 | `test-race` utilisable en local | aboutit dans un délai jugé utilisable | Vert dans le conteneur (S1, `CGO_ENABLED=1`, gcc de l'image) ; 295,9 s à chaud seul (S2) | Sections 3 et 4 | **Atteint** sur la mesure. Le jugement « utilisable » est celui de l'utilisateur (S2.6) : non consigné, à confirmer |
| 3 | Performance : durée `dagger check` / durée `mise run ci`, même machine, à chaud | ≤ 1,2 | 298,7 s / 410,5 s = 0,73 (médiane de 3, commit `2f383dec`). Hors `test-race` : 282,4 s / 410,5 s = 0,69 | Section 4 (S2) | **Atteint avec réserve.** `mise run ci` échoue à `test` sous Windows : sa durée est celle d'un run en échec, pas d'un run complet |
| 4 | Une seule source de versions | aucun littéral Go ou golangci-lint dans le module ; divergence `mise.toml` / `go.mod` consignée | Aucun littéral trouvé. Épinglages acceptés : mise v2026.10.18 et digest de `debian:bookworm-slim`. La divergence (`go = "1.27"` contre `1.27.3`) est connue mais son effet dans le conteneur n'a pas été vérifié | Section 3 (S1.6) | **Atteint avec réserve** : la partie « littéral » est atteinte ; la partie « divergence » est consignée, non vérifiée dans le conteneur |
| 5 | Zéro coût, Entire compris | aucun coût, Gate Entire comprise | Aucun essai. Coût documenté : Dagger Cloud Starter 20 $/mois, aucun palier gratuit documenté (page de prix du 2026-11-11). Moteur et CLI locaux : sans coût de Dagger | Section 5 (S4) ; `research.md` §2 | **Non mesuré** : S4 non faite, comptes Entire et Dagger Cloud non vérifiés |
| 6 | Module ≤ 150 lignes environ | ≤ 150 lignes écrites à la main | 82 lignes (`main.go`), plus des fichiers générés non comptés | Section 3 (S1.7) | **Atteint** |

Vérification du mapping contre S5.1 : les six intitulés et seuils correspondent. Deux points diffèrent d'un « atteint » simple et sont marqués comme tels : le critère 1 (référence GitHub du commit en échec déduite) et le critère 4 (divergence non vérifiée). Le critère 3 est atteint avec la réserve de la référence incomplète.

Décompte : 2 atteints, 3 atteints avec réserve, 0 non atteint, 1 non mesuré. Le critère 5 suffit à interdire « adopté » (S5.2).

## 3. Section S1 : `dagger check` local

Contexte : branche `spike/dagger`, commit `2aa0cf75`, local, jamais poussé. **Cette branche n'existe plus dans ce dépôt à ce jour** : le module n'est plus consultable ici, seuls les comptes rendus et cette note subsistent. Dagger v1.0.0-beta.27, moteur Docker Desktop 29.9.2, CLI lancé sous PowerShell.

| Critère S1 | Résultat | Constat |
|---|---|---|
| 1. Liste des vérifications | OK | Six vérifications listées. Dagger ajoute un 7e check, `go-sdk:stale`, désactivé par `check-generated = false` dans `dagger.toml` |
| 2. Commit sain, utilisateur non root | OK | Six vertes à froid en 6 min 26 s, tests en uid 1000. 4 tests sautés. Les « 3 sautés en root » et « 9 sans git » de la référence ont disparu ; les 4 sauts (Windows ou opt-in) ne sont pas expliqués plus avant |
| 3. `test-race` | OK | Via `mise run test-race` avec `CGO_ENABLED=1`, gcc installé par apt dans l'image |
| 4. Échec volontaire | OK | Code de sortie 1, `ci/test` nommé, les six rapportées. L'option `--fail-fast` existe |
| 5. Contexte, caches, worktree | Partiel | Exclusions `.worktrees`, `bin`, `.git`. Caches `GOMODCACHE`, `GOCACHE` et mise. Le `.git` d'un worktree est un fichier inutilisable dans le conteneur : `git describe` échoue, la version reste « dev » |
| 6. Versions | OK | Aucun littéral de version Go ou golangci-lint. Divergence `mise.toml` / `go.mod` non vérifiée dans le conteneur |
| 7. Taille | OK | 82 lignes écrites à la main (`main.go`), plus des fichiers générés |

Rappel (S1.3) : sous Windows, `test-race` ne tourne pas par `mise` faute de gcc. S2 a pourtant constaté que le poste a un gcc (scoop mingw) : la prémisse « pas de compilateur C sur l'hôte » est fausse pour ce poste. `test-race` dans le conteneur aboutit quand même.

Durées mesurées en S1 :

| Cas | Durée |
|---|---|
| À froid | 6 min 26 s |
| À chaud, sans changement | 1,6 s |
| À chaud, après un changement de code | environ 5 min (`test` et `test-race` ne profitent pas du cache) |

Non consigné : durée d'installation (de la commande d'installation à un `dagger check -l` fonctionnel), raison du choix de Docker Desktop plutôt que Podman, statut contourné ou bloquant de chaque friction (voir section 8 pour la liste).
Non testé : code de sortie hors du wrapper, arm64.

## 4. Section S2 : comparaison avec `mise run ci` sur 5 commits

Commits : `9c6c7825`, `2f383dec`, `5f76cc16`, `56166bf8` (sains), `a70bc1c0` (échec volontaire, branche jetable `spike/dagger-fail`, non poussée). Worktree `.worktrees/spike-dagger-eval`. Logs : `C:\Users\anthoni\spike-s2\` (poste de l'utilisateur, hors dépôt).

### Verdicts

- Les 4 commits sains : Dagger six OK, GitHub six success. Zéro écart non expliqué.
- `a70bc1c0` : côté Dagger, `test` et `test-race` en ERROR. Côté GitHub, verdict déduit (run non lancé) : `test` échoue, `test-race` sauté. L'écart est expliqué : Dagger exécute les vérifications indépendamment, la CI GitHub enchaîne.

Le tableau détaillé par commit et par vérification n'est pas repris dans les comptes rendus : non consigné ici, à chercher dans les logs.

### Durées

| Mesure | Dagger | `mise run ci` | Rapport |
|---|---|---|---|
| À chaud, `2f383dec`, médiane de 3 | 298,7 s | 410,5 s | 0,73 |
| À chaud, hors `test-race` | 282,4 s | 410,5 s | 0,69 |
| À froid | 413 s | 504 s | non calculé |
| `test-race` seul à chaud | 295,9 s | non exécutable (voir section 6) | - |

Repère informatif de S2.4 : CI GitHub, médiane 322 s sur les 10 derniers runs verts (min 311 s, max 453 s). Ce repère concerne GitHub sans Dagger, pas une CI Dagger.

### Échec volontaire

- Dagger nomme `test` et `TestSpikeBroken`, à 283-302 s.
- `mise run ci` échoue à 424,6 s, au milieu de dizaines d'échecs Windows sans rapport.
- Avec `--fail-fast` : 284,2 s, les deux vérifications restent en ERROR.
- Appréciation de l'utilisateur sur la localisation la plus rapide (S2.5) : non consignée, à compléter par l'utilisateur.

### Réserves de mesure

- `test` dure environ 280 s en conteneur comme sur l'hôte (`internal/app` seul : 409 s sur l'hôte). Cause non isolée.
- À froid, l'image du moteur n'a pas été retirée : le « froid » inclut apt, mise et les outils, mais pas le pull du moteur.
- Une seule machine ; médiane de 3 sur un seul commit, une mesure sur les autres (risque R8 de `stories.md`).

## 5. Section S4 : Gate Dagger sur un trail Entire

**S4 n'a pas été faite.** Les comptes Entire et Dagger Cloud n'ont pas été vérifiés (condition d'entrée de S4.1) ; aucun essai, aucune capture, aucun coût engagé.

- Verdict (S4.1) : **non mesuré, payant.** Source : `research.md`, page de prix du 2026-11-11 : Starter 20 $/mois, Scale 200 $/mois, aucun palier gratuit documenté. Le caractère « payant » vient de cette page, pas d'une observation.
- Question de S4.5 : « les contextes `Lint and build` et `Test` exigés par `main-protection` peuvent-ils être remplacés ou alimentés par une Gate Dagger ? » : **non documenté.** `research.md` §5 le dit : comment une Gate Dagger les remplacerait n'est pas documenté. Aucune observation ne le complète.
- S4.6 : `main-protection` et `ci.yml` n'ont pas été modifiés. Aucun Cloud check n'a été activé, donc rien à couper.
- Rappel de contexte (`research.md`) : l'intégration Entire passe par Dagger Cloud, en « public preview ».

## 6. Comparaison avec `mise run ci`

### Ce que Dagger apporte

- **Isolation conteneur** : OS Linux, système de fichiers propre, tests en utilisateur non root (uid 1000).
- **`test-race` sans configuration locale** : gcc vient de l'image, `CGO_ENABLED=1` y est fixé. Sur le poste mesuré, gcc existe pourtant (scoop mingw) : l'apport ne s'est pas vérifié comme nécessaire ici.
- **Un `test` qui passe** là où `mise run ci` échoue sous Windows (séparateurs de chemin, noms de fichiers dans `agentusage`, `app`, `config`, `engine`, `imageupdate`, `jobs`). `mise run ci` ne peut donc pas être lancé de bout en bout sur ce poste.
- **Vérifications indépendantes** : en cas d'échec, `test` et `test-race` sont rapportées ensemble (sans `--fail-fast`).
- Durée à chaud inférieure : 298,7 s contre 410,5 s. Écart à relativiser : la durée de `mise run ci` est celle d'un run en échec.

### Ce que Dagger coûte

- **Un moteur de conteneurs** (Docker Desktop 29.9.2 ici), le moteur Dagger étant lui-même un conteneur privilégié (`research.md`).
- **Une bêta** : v1.0.0-beta.27, changements fréquents (`research.md` : beta.20 à .27 en 3 semaines). Frictions en section 8.
- **Du temps** : à froid 6 min 26 s (S1) à 413 s (S2) ; à chaud environ 5 min après un changement, car `test` et `test-race` ne profitent pas du cache.
- **Un module à maintenir** : 82 lignes écrites à la main, plus des fichiers générés, plus `dagger.toml`.
- **Moins de fidélité sur `.git`** : en worktree, `git describe` échoue et la version est « dev ».
- **Un coût possible côté Cloud** (Starter 20 $/mois, aucun palier gratuit documenté), uniquement si on active l'intégration Entire ; non mesuré.

### `mise run ci` reste-t-il suffisant ?

- Pour la cohérence des versions et l'enchaînement des six vérifications : oui. Dagger n'apporte pas de cohérence de versions que mise ne donne pas déjà (`research.md`, constat de départ).
- Sous Windows sur ce poste : non, il échoue à `test` pour des raisons propres à Windows. Il reste suffisant sur Linux et macOS : non mesuré ici.
- Quel que soit le verdict, `mise run ci` reste la référence de la CI et de S2, et n'est pas remplacé par la note. Aucune mesure ici ne dit qu'il faille le retirer.
- La réponse « suffisant ou non » pour la boucle locale dépend du besoin : isolation Linux sur un poste Windows (Dagger la donne) ou simple cohérence (mise la donne). Ce choix est celui de l'utilisateur.

## 7. Limites

- **La durée d'une CI GitHub avec Dagger n'a PAS été mesurée** : les runners on-prem sont hors spike (S3 retiré). Un éventuel verdict « adopté » ne vaudrait que pour la boucle locale. Passer la CI sur Dagger exigerait une nouvelle décision et une nouvelle mesure.
- Verdict GitHub du commit en échec déduit, pas observé.
- `mise run ci` ne tourne pas complètement sous Windows : le rapport 0,73 compare un run complet de Dagger à un run de `mise` arrêté à `test`.
- Une machine, un moteur (Docker Desktop), PowerShell seul ; médiane de 3 sur un commit seulement.
- Cause de la durée de `test` (environ 280 s, `internal/app` 409 s sur l'hôte) non isolée.
- « À froid » n'inclut pas le pull de l'image du moteur.
- 4 tests sautés non expliqués (S1.2).
- Divergence `mise.toml` / `go.mod` non vérifiée dans le conteneur.
- Non testé : code de sortie hors du wrapper, arm64, Podman, WSL, macOS, Linux natif.
- Coût Cloud et Gate Entire non mesurés (S4 non faite).
- Le module de la branche `spike/dagger` n'existe plus dans ce dépôt ; les mesures ne sont pas reproductibles à partir de ce dépôt seul.
- Les logs de S2 sont sur le poste de l'utilisateur (`C:\Users\anthoni\spike-s2\`), hors du dépôt.

## 8. Apprentissage et frictions

### Frictions de la 1.0 bêta (v1.0.0-beta.27)

| Friction | Statut contourné / bloquant |
|---|---|
| `dagger.exe` se bloque sous Git Bash ; fonctionne sous PowerShell | Non consigné ; PowerShell utilisé |
| `dagger generate -y` nécessaire après chaque changement d'API du module | Non consigné |
| Volumes de cache créés en root : `chown` dans l'image | Contournée (chown dans l'image) |
| Sortie PowerShell redirigée en UTF-16 | Non consigné |
| 7e check `go-sdk:stale` ajouté par Dagger | Contournée (`check-generated = false`) |
| `.git` d'un worktree inutilisable dans le conteneur (`git describe` échoue, version « dev ») | Non contournée |

Le statut de chaque friction n'a pas été consigné au fil du spike (S1.7 le demandait) : à compléter.

### Ce que les mesures apprennent

- Dagger ajoute des checks que le projet n'a pas demandés (7e check) : `dagger check -l` ne liste pas « exactement six » sans configuration.
- Les caches (`GOMODCACHE`, `GOCACHE`, mise) rendent un relancement sans changement quasi instantané (1,6 s), mais ne profitent pas à `test` ni `test-race` après un changement de code : environ 5 min restent à payer.
- Un worktree n'est pas utilisable tel quel : son `.git` est un fichier, donc la version et le marquage VCS sont perdus dans le conteneur.
- Sous Windows, Git Bash bloque `dagger.exe` ; PowerShell est le point d'entrée qui marche.
- Les tests tournent bien en non root, avec 4 sauts au lieu de 3 + 9 de la référence.
- `mise run ci` ne couvre pas Windows de bout en bout : c'est le test qui échoue, pas l'absence de compilateur C (le poste en a un).
- Le moteur Dagger se prête au rôle de bac à sable Linux sur un poste Windows, sans changer la source des versions (mise reste la source).

### Points de l'utilisateur

À compléter par l'utilisateur : ce qu'il dit avoir appris sur Dagger (objectif d'apprentissage déclaré, S5.6). Cette note n'en déclare aucun à sa place.

## 9. Journal de temps

**Non consigné.** Les heures par story (S1, S2, S4, S5) n'ont pas été journalisées. Cette note ne se prononce donc ni sur le total, ni sur le respect de la time-box de 14 h (S5.5 non satisfait).

Durées machine mesurées (pas du temps de travail) :

| Mesure | Durée |
|---|---|
| S1, froid | 6 min 26 s |
| S1, chaud sans changement | 1,6 s |
| S1, chaud après changement | environ 5 min |
| S2, Dagger à chaud (médiane de 3) | 298,7 s |
| S2, Dagger à froid | 413 s |
| S2, `mise run ci` à chaud (médiane de 3) | 410,5 s |
| S2, `mise run ci` à froid | 504 s |
| S2, `test-race` Dagger seul à chaud | 295,9 s |

## 10. Suites selon l'issue

Dans les deux cas, seule cette note (et éventuellement une mention dans `docs/journal.md`) est fusionnée par PR ; le module reste sur sa branche ou est supprimé (S5.7). La branche `spike/dagger` n'existant plus ici, le module n'est de toute façon plus dans ce dépôt.

### Issue A : abandonner

Supprimé :
- la branche `spike/dagger` (déjà absente de ce dépôt), la branche jetable `spike/dagger-fail` (non poussée) et le worktree `.worktrees/spike-dagger-eval` ;
- les comptes Dagger Cloud et Entire éventuels (aucun n'a été vérifié ni créé pour cet essai, d'après les comptes rendus) ;
- toute idée de Gate Dagger et de CI Dagger (S3 déjà retiré).

Gardé :
- les mesures de cette note (durées, verdicts, frictions) ;
- `mise run ci` (S0), qui reste la référence, avec sa limite sous Windows ;
- `.github/workflows/ci.yml` et le ruleset `main-protection`, inchangés.

Critère 5 restant « non mesuré » : il n'est pas atteint par abandon, il est sans objet.

### Issue B : faire S4 (30 min maximum, sans carte)

Gardé : tout ce qui précède (mesures, module, branche si elle est recréée, `mise run ci`).
Ajouté :
- vérifier avant tout essai qu'un compte Entire et un compte Dagger Cloud existent sans carte ; sinon verdict « non mesuré, payant » confirmé et issue A ;
- essai sur `dagger.toml` du module (à reconstruire, la branche n'existe plus) ;
- arrêt à la première demande de paiement ;
- consigner coût, budget, plan, palier gratuit éventuel et la réponse à « Gate Dagger et `main-protection` » (oui / non / non documenté) ;
- couper les Cloud checks activés et laisser `main-protection` intact.

Si S4 donne un coût nul et que les réserves des critères 1, 3 et 4 sont jugées acceptables, un verdict « adopté pour la boucle locale » devient possible, avec la limite de la section 7. Étapes suivantes alors proposées, chacune sous réserve d'une nouvelle décision : fusion du module, décision sur l'exécution en CI (avec nouvelle mesure), B (CI sur Entire). Une ADR n'est écrite qu'après le go/no-go de S4, comme le dit le journal du 2026-11-11 (entrée présente sur la branche `docs/journal-entry-dagger-network-path-image-search`, absente de `main` à ce jour).

Si S4 révèle un coût, le critère 5 devient non atteint et le verdict est « abandonné ».
