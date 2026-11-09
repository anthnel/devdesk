# Indicateur d'activité uniforme — stories

Brouillon du 2026-11-06, mis à jour le 2026-11-06 avec les décisions de l'utilisateur et la revue de faisabilité de l'architect. Contexte : `brief.md` (décisions, risques), `research.md` et `ux.md` (glyphes, couleurs, maquettes, libellés). Aucun item Plane ni issue GitHub n'est créé. Les efforts S / M / L sont ceux **révisés par l'architect** (voir « Faisabilité » en fin de fichier) ; aucune story n'est L.
## Backlog Plane (créé le 2026-11-06)

Projet DEVDESK, état Backlog, label `story`. Modules : « E1 - Fondations (indicateur d'activité) », « E2 - Migration des vues (indicateur d'activité) », « E3 - Documentation (indicateur d'activité) ». DEVDESK-99 est absorbée par A-13 (DEVDESK-151) et passée en Cancelled. Les dépendances sont citées dans la description de chaque work item (les relations Plane sont indisponibles : HTTP 404).

| Story | Work item | Module | Priorité Plane |
|---|---|---|---|
| A-00 | DEVDESK-136 | E1 | high |
| A-01 | DEVDESK-137 | E1 | high |
| A-02 | DEVDESK-141 | E1 | high |
| A-03 | DEVDESK-139 | E1 | high |
| A-04 | DEVDESK-138 | E1 | high |
| A-05 | DEVDESK-140 | E1 | high |
| A-06 | DEVDESK-142 | E2 | high |
| A-16 | DEVDESK-143 | E2 | high |
| A-07 | DEVDESK-144 | E2 | high |
| A-08 | DEVDESK-145 | E2 | high |
| A-09a | DEVDESK-146 | E2 | high |
| A-09a2 | DEVDESK-218 | E2 | high |
| A-09b | DEVDESK-153 | E2 | high |
| A-10a | DEVDESK-147 | E2 | medium |
| A-10b | DEVDESK-148 | E2 | medium |
| A-11 | DEVDESK-149 | E2 | medium |
| A-12 | DEVDESK-150 | E2 | medium |
| A-13 | DEVDESK-151 | E2 | medium |
| A-14 | DEVDESK-152 | E2 | medium |
| A-17 | DEVDESK-154 | E3 | high |

Règles du dépôt visées : Rules 114 (aide `?`), 116 (étendue rendue), 121 (amendée, voir D9), 122 (`Cell` en texte brut, couleur par `Style`), 125 (colonne d'icône), 128 (footer, error > warn > info > status), 129 (UI en anglais US), 130 (raccourcis grisés), 139 (corps = table). Tout le texte d'interface et tous les journaux de ces stories sont en anglais US.
Glossaire : **pastille** = l'indicateur de la première colonne ; **repos** = l'icône habituelle de la ligne ou une cellule vide ; **clé d'échec** = (type d'opération, cible, contexte).

## Décisions tranchées (2026-11-06)

Prises par l'utilisateur ; ne pas rouvrir. La colonne de droite dit où elles se retrouvent dans les stories.

| # | Décision | Effet dans les stories |
|---|---|---|
| D1 | **R1** : une nouvelle colonne de pastille est **acceptée** pour les 7 tables sans colonne d'icône (4 tables OCI, liste de clone, ports, agents de `:agent-usage`). Le non-objectif « pas de nouvelle colonne » est supprimé. | A-09a, A-09a2 (navigateur de registre, 8e colonne, sorti de A-09a), A-09b, A-10b, A-12, A-13 : largeur, Rule 116 à 60 colonnes, Rule 125 (sans titre, `IconColumnWidth`, `SizingFixed`, ni `Less` ni `Search`) |
| D2 | **R2** : critère de succès 5 = « **un seul tick anime les lignes** ». Les spinners de chargement global du footer gardent leurs chaînes. | A-03 (critère 4), A-16 (test garde-fou) |
| D3 | Réglage `app.motion: blink \| static`, **par contexte seulement** (aucune variable d'environnement globale), défaut `blink`. | A-02 |
| D4 | Couleurs d'état : **alias fermés** du thème (pas de rôles `icon_*` surchargeables). Les rôles `icon_*` ne servent qu'à l'icône de repos. | A-01 (critère 1 corrigé) |
| D5 | Chaque vue n'affiche que les échecs des **opérations relançables depuis elle**. | A-04, A-07, A-08, A-09a, A-10, A-11 |
| D6 | Le critère de coloration du coût d'`:agent-usage` (ex-DEVDESK-99, `theme.LoadWarnPercent` / `theme.LoadCriticalPercent` déjà existants) **reste dans A-13**. A-13 reste M et ne dépend pas de S-09 en code. | A-13 (critère 7) |
| D7 | Glyphes (Unicode standard, pas de Nerd Font) : `●`/`○` en cours (clignote, seul état animé, `ColorSecondary`) ; `◴` en file (fixe ; **remplace l'ancien glyphe en quart U+25D4 partout**) ; `⊗` échec (rouge, fixe) ; `◐` suspendu (agent idle, conteneur paused, orange) ; `◕` conteneur restarting (orange, fixe) ; `◉` **terminé uniquement** (job done, agent done) ; `⊘` job cancelled ; `⊖` job skipped ou conteneur exited code 0 ; `◌` unknown ou created (couleur neutre, pas dim). Mode sans mouvement : `●` fixe. Phase éteinte : `○`, même couleur. Fréquence 1 Hz, « en file » statique, restarting fixe. L'utilisateur distingue `●` de `◉` dans son terminal (pas de repli `✓`). | A-01, A-06, A-13, A-14 ; `ux.md` §1 |
| D8 | Conteneur `running` : **aucune pastille** (état nominal, Rule 122) ; la ligne montre son icône habituelle. `◉` ne désigne plus qu'un état « terminé ». Le footer d'un conteneur running reste « Running since 3 hr ago », sans glyphe en préfixe. | A-14 ; `ux.md` §1, §3.5, §5 (légende), §2.4 |
| D9 | **Rule 121 : amendement accepté.** La colonne d'activité remplace `IconOK` / `IconError` / `IconWarning` pour les états d'activité. | A-01 (critère 10) : amendement des Rules 121 (`tui-forms.md`) et 125 (`tui-tables.md`) |
| D10 | Libellés du footer et de la légende `?` (`ux.md` §1.2 et §5) **validés** (anglais US). La phrase « or in :cfg » de la légende suppose que `app.motion` est modifiable dans la vue de configuration : **à confirmer** (question ouverte 7). | A-05, A-01 (critère 9), A-02 |
| D11 | DEVDESK-99 est **absorbée** par ce chantier ; elle sera close à la création du backlog (A-13 reprend son contenu). Elle n'est pas modifiée dans Plane maintenant. | A-13 ; question ouverte 6 |

**Intégré depuis la revue de l'architect** (efforts, découpages, fusions) : A-15 fusionnée dans A-01 ; A-07a et A-07b fusionnées en A-07 ; A-09 redécoupée en A-09a (M) et A-09b (S) ; A-10 redécoupée en A-10a (M) et A-10b (M) ; A-06 passe à S avec **un second point de contrôle à la livraison de A-07** (H5 et R3) ; A-12 passe à S ; A-17 est réduite (la documentation d'architecture suit le code, voir A-17).

**Retenu des recommandations architect / UX** (ni tranché par l'utilisateur, ni dans la liste des questions qu'il garde ouvertes ; à contester si besoin) : mémorisateur placé dans `jobs.Registry` (R5) ; chaîne D5 renommée « animation », une seule (Q1) ; priorité sur une ligne **en cours > en file > échec mémorisé > état observé > repos** (Q3) ; la cause d'une erreur est un `Error` de 3 s à l'arrivée, puis un `Status` persistant pour la ligne sélectionnée (Q4) ; colonne `Scanned` = texte statique `scanning` en `DimStyle` pendant un scan (Q5) ; le clignotement continue sous le curseur (Q6).

## Ordre de livraison

```
Vague 1 (parallèles) : A-01 pastille + légende   A-04 mémorisateur   A-05 footer par ligne   (A-00 spike, en parallèle)
Vague 2              : A-03 tick partagé  ->  A-02 réglage app.motion
Pilote               : A-06 :jobs + squelette A-16  ->  GO / NO-GO sur le clignotement (1er point de contrôle : H1, H2)
Après go             : A-07 ws  ->  2e point de contrôle (H5, R3)
Vague 3 (après le 2e point, parallélisables, chacune retire son entrée de la liste d'exceptions de A-16) :
  A-08 :sec · A-09a OCI images -> A-09b OCI autres tables · A-10a arbre · A-10b clone list ·
  A-11 templates · A-12 ports · A-13 :agent-usage · A-14 containers
Clôture              : PR finale A-16 (liste d'exceptions supprimée ; suppression de StatusColumn, AdvanceSpinner, spinnerFrame) + A-17
```

A-04 est indépendant, mais il doit être livré **avant le pilote** pour que la vague 3 ne l'attende pas. Écart assumé avec l'ordre « fondations, vues, footer, aide, garde-fou, doc » : le footer par ligne (A-05) et la légende (dans A-01) sont des mécanismes partagés dont chaque migration dépend ; les livrer après les vues obligerait à les écrire neuf fois. Le garde-fou (A-16) arrive tôt, avec une liste d'exceptions (vues pas encore migrées) qui ne fait que diminuer, pour que les PR intermédiaires ne dérivent pas.

| Story | Titre court | Epic | MoSCoW | Effort | Dépend de |
|---|---|---|---|---|---|
| A-00 | Spike : inventaire des vues à opération longue | E1 | Must | S | — |
| A-01 | Composant pastille, glyphes, alias de couleur, légende (absorbe A-15), amendement Rules 121 / 125 | E1 | Must | M | — |
| A-02 | Réglage `app.motion` | E1 | Must | S | A-01, A-03 |
| A-03 | Tick partagé du clignotement | E1 | Must | M (risque élevé) | A-01 |
| A-04 | Mémorisateur central des échecs | E1 | Must | M | — |
| A-05 | Footer par ligne | E1 | Must | M | A-01 |
| A-06 | Pilote `:jobs` + go/no-go | E2 | Must | S (+ une semaine d'usage) | A-01, A-02, A-03, A-05 ; A-04 livré avant |
| A-16 | Test garde-fou transverse | E2 | Must | M | A-06 (squelette dans la PR du pilote) |
| A-07 | `ws` : opération en cours et échec (fusion de A-07a et A-07b) | E2 | Must | M | A-04, A-06 |
| A-08 | `:sec` | E2 | Must | M | A-04, A-06, 2e point de contrôle (A-07) |
| A-09a | OCI : images | E2 | Must | M | A-04, A-06, 2e point de contrôle |
| A-09a2 | OCI : navigateur de registre (8e colonne) | E2 | Must | S | A-09a |
| A-09b | OCI : networks, volumes, registries | E2 | Must | S | A-09a |
| A-10a | Explorer : arbre (create et delete) | E2 | Should | S | A-06, 2e point de contrôle |
| A-10b | Explorer : liste de clone | E2 | Should | M | A-06, 2e point de contrôle |
| A-11 | Templates | E2 | Should | S | A-06, 2e point de contrôle |
| A-12 | Netdiag : ports | E2 | Should | S | A-06, 2e point de contrôle |
| A-13 | `:agent-usage` (absorbe DEVDESK-99) | E2 | Should | M | A-06, 2e point de contrôle |
| A-14 | Containers | E2 | Should | M | A-06, 2e point de contrôle |
| A-17 | Documentation (réduite) | E3 | Must | S | toutes les migrations livrées |

18 stories + 1 spike = 19 entrées. Epics : **E1 Fondations** (A-00 à A-05), **E2 Migration des vues** (A-06 à A-14, A-16), **E3 Documentation** (A-17). A-15, A-07a et A-07b n'existent plus (fusionnées).

## Vocabulaire des glyphes (référence : `ux.md` §1)

| Glyphe | Sens | Couleur (alias fermé) | Mouvement |
|---|---|---|---|
| `●` / `○` | opération en cours (action instantanée `busy` comprise) | `ColorActivityRunning` (= `ColorSecondary`) | clignote à 1 Hz, `○` même couleur ; `●` fixe avec `app.motion: static` |
| `◴` | en file | `ColorActivityNeutral` (= `ColorText`) | fixe |
| `⊗` | échec (opération échouée, conteneur `dead` ou `exited` non nul) | `ColorActivityFailed` (rouge) | fixe, jamais |
| `◐` | suspendu : agent `idle`, conteneur `paused` | `ColorActivitySuspended` (orange) | fixe |
| `◕` | conteneur `restarting` | `ColorActivitySuspended` | fixe |
| `◉` | terminé : job `done`, agent `done` (et rien d'autre) | `ColorActivityNeutral` | fixe |
| `⊘` | job `cancelled` | `ColorActivityNeutral` | fixe |
| `⊖` | job `skipped`, conteneur `exited` code 0 | `ColorActivityNeutral` | fixe |
| `◌` | agent `unknown`, conteneur `created` | `ColorActivityNeutral` (pas dim) | fixe |
| repos | icône habituelle de la ligne, ou cellule vide ; **conteneur `running` : icône habituelle** | rôle `IconRole*` existant | — |

---

## Epic E1 — Fondations

### A-00 — Spike : inventorier les vues à opération longue (DEVDESK-136)
Epic : E1 — Priorité : Must — Effort : S

En tant que mainteneur de DevDesk, je veux un inventaire daté de toutes les vues qui lancent une opération longue ou affichent un spinner par ligne, afin de figer le périmètre de la migration sans angle mort.

**Critères d'acceptation**
1. Étant donné le code à la date du spike, quand l'inventaire est établi, alors il liste pour chaque vue : ses spinners (par ligne ou globaux), ses états d'échec mémorisés à la main, sa première colonne actuelle et sa source d'état (registre, calculé par la vue). Le tableau de la section « Faisabilité » (constats du code) en couvre la moitié.
2. Étant donné les vues non inventoriées jusqu'ici (dashboard, status, filebrowser, viewer en suivi, setup, auth forge), quand l'inventaire les couvre, alors chacune est classée « migrer », « hors lot » ou « sans objet », avec une phrase de justification.
3. Étant donné chaque spinner trouvé, quand il est classé, alors il est marqué « par ligne » (remplacé par la pastille) ou « chargement global » (reste au footer, Rule 128).
4. Étant donné les copies manuelles d'échec (`security.scanTarget.Failed`, `failedScans` d'OCI et toute autre), quand l'inventaire s'achève, alors la liste est complète et chacune renvoie à la story qui la remplace.
5. Le livrable tient en une page et se termine par la liste finale des vues à migrer, que l'utilisateur valide.

**Hors périmètre** : tout code livré.
**Questions ouvertes** : aucune. Les vues « hors lot » et « sans objet » de l'inventaire sont acceptées par l'utilisateur (2026-11-06) ; le navigateur de registre OCI en sort pour A-09a2 (DEVDESK-218).

### A-01 — Composant pastille : glyphes, alias de couleur, deux sources d'état, légende, amendement des règles (DEVDESK-137)
Epic : E1 — Priorité : Must — Effort : M (absorbe l'ancienne A-15)

En tant que développeur de DevDesk, je veux un composant unique qui dessine la pastille d'une ligne à partir d'un état, avec sa légende d'aide, afin que toutes les vues parlent le même vocabulaire visuel sans choisir elles-mêmes glyphe ou couleur.

**Critères d'acceptation**
1. Étant donné un état parmi ceux du vocabulaire des glyphes (D7), quand la cellule est construite, alors elle renvoie du texte brut mesurable d'un seul glyphe, sans espace final (Rule 122) ; `datatable` la complète à `datatable.IconColumnWidth` ; la couleur ne passe que par `Style`, via des **alias fermés** `ColorActivityRunning`, `ColorActivityNeutral`, `ColorActivitySuspended`, `ColorActivityFailed`, assignés dans `ApplyTheme` comme `ColorFooter*` (D4). Aucun rôle `icon_*` surchargeable pour les états ; les rôles `IconRole*` existants ne servent qu'à l'icône de **repos**.
2. Étant donné l'ensemble des états du composant (repos exclu), quand on compare leurs glyphes, alors chaque sens a son glyphe propre (test) : `●` et `○` sont réservés à « en cours », `◉` à « terminé » (job done, agent done) ; l'état se lit sans la couleur, y compris sous `NO_COLOR` et pour un orange fixe (`◐`, `◕`) face à un rouge fixe (`⊗`).
3. Étant donné l'état « repos », quand la cellule est construite, alors elle renvoie l'icône de ligne fournie par la vue, ou une cellule vide de même largeur si la vue n'en a pas (jamais `◌`). Un conteneur `running` est un repos (D8).
4. Étant donné un état fourni par le registre ou par le mémorisateur, ou un état calculé par la vue (état docker, état d'agent), quand la pastille est construite, alors les deux sources produisent le même rendu pour le même état (deux sources, un seul chemin de rendu). Nouveau champ `Column.Indicator func(T) indicator.State` dans `datatable`, phase lue au rendu (`SetBlinkPhase(on bool)`, sans reconstruire ni remesurer) ; `StatusColumn` et `AdvanceSpinner` cohabitent jusqu'à la dernière migration. La précédence `busy` > état fourni par la vue est écrite une fois dans `datatable`.
5. Étant donné plusieurs opérations simultanées sur une ligne, quand la pastille est construite, alors une seule fonction (`indicator.Resolve`) applique **en cours > en file > échec mémorisé > état observé > repos** (`busy` compte comme « en cours »), et un test fige l'ordre.
6. Étant donné le clignotement, quand la phase est éteinte, alors le glyphe est `○` dans la même couleur que `●` ; avec `app.motion: static`, le glyphe est `●` fixe ; « en file » (`◴`) ne clignote jamais ; `◕` (restarting) est fixe.
7. Étant donné le thème par défaut et chaque thème livré, quand on mesure les quatre alias, alors chacun atteint 3:1 sur `ColorBackground` **et** sur `ColorTableLineSelected` (test de contraste dans `theme/`, mêmes seuils que les couleurs de jetons de DEVDESK-98) ; `unknown` et `created` sont en `ColorActivityNeutral`, jamais en `ColorDim`. Les rapports de `ux.md` §2.1 sont des calculs à la main, à confirmer par ce test (question ouverte 8).
8. Étant donné un `Style` appliqué à la cellule, quand la ligne est sélectionnée, alors le glyphe reste lisible (Rule 122, sélection normale ; sous une ligne colorée en entier, le glyphe porte l'information). La Rule 116 tient avec `runewidth.EastAsianWidth = true`.
9. Étant donné la table de glyphes, quand une vue construit son aide `?`, alors la légende partagée (texte validé, `ux.md` §5, anglais US, Rules 114 et 129) en est **dérivée** : une seule source, ajouter ou changer un glyphe la met à jour sans second endroit à éditer. `:jobs`, `:agent-usage` et containers étendent la légende de base au lieu de la remplacer (extensions de `ux.md` §5, sans `◉ running` pour containers). La phrase « or in :cfg » est à confirmer (question ouverte 7).
10. Étant donné la Rule 121 (`.claude/rules/tui-forms.md`) et la Rule 125 (`.claude/rules/tui-tables.md`), quand la story est livrée, alors la Rule 121 est amendée (la colonne d'activité remplace `IconOK` / `IconError` / `IconWarning` pour les états d'activité ; les autres colonnes d'état, Verdict ou Update par exemple, ne changent pas) et la Rule 125 gagne un paragraphe (la première colonne d'une vue à opération longue est la pastille ; la couleur dit l'état et non le type d'opération ; pas de vert pour « terminé »). Les amendements sont écrits dans la même PR que le composant (règle « documentation dans le même commit ») et s'appliquent dès la première vue migrée (A-06).

**Hors périmètre** : le tick et le clignotement (A-03), le réglage (A-02), le footer (A-05), toute migration de vue.
**Hypothèses** : Hypothèse: le nom exact des alias est une proposition de `ux.md` ; seule la règle « alias fermés » est tranchée.
**Questions ouvertes** : aucune propre à cette story (glyphes, couleurs, priorité, rôles sont tranchés ; test de contraste : question ouverte 8).

### A-02 — Réglage `app.motion` (DEVDESK-141)
Epic : E1 — Priorité : Must — Effort : S

En tant qu'utilisateur sensible au mouvement (ou sur une liaison lente), je veux désactiver le clignotement des pastilles dans la configuration du contexte, afin de garder l'information d'état sans animation.

**Critères d'acceptation**
1. Étant donné une installation neuve ou une configuration qui ne contient pas le réglage, quand une opération démarre, alors la pastille clignote (`app.motion: blink`, défaut).
2. Étant donné `app.motion: static`, quand une opération est en cours, alors la pastille montre `●` fixe, qui diffère du repos, de l'échec et des autres états ; couleur et glyphe restent ceux de « en cours ».
3. Étant donné `app.motion: static`, quand une ou plusieurs opérations sont en cours, alors aucun redessin périodique n'est émis pour les pastilles (test : `ensureTick` ne planifie aucun tick d'animation).
4. Étant donné le réglage modifié dans le fichier du contexte (ou dans la vue de configuration, **à confirmer**, question ouverte 7), quand l'utilisateur revient sur une vue, alors le nouveau comportement s'applique sans redémarrer l'application : lu au démarrage, au changement de contexte et sur `ConfigSavedMsg`, comme `app.gauge_glyph`.
5. Étant donné une valeur invalide dans le YAML, quand la configuration est chargée, alors l'application garde `blink`, journalise `WARN [config] app.motion …` et, si la vue de configuration expose le champ, son footer l'indique ; le chargement n'échoue pas.
6. Étant donné le réglage, quand il est par contexte (D3), alors il n'existe **aucune** variable d'environnement globale pour le même effet ; la valeur est répétée dans chaque contexte qui la veut.
7. Le réglage est décrit dans `configuration.md` (même commit que le code) et, s'il est exposé dans `:cfg`, dans l'aide `?` de la vue de configuration (Rule 114) ; si c'est un champ cyclique, il suit la Rule 132.

**Hors périmètre** : détection d'une préférence système (aucune convention terminal équivalente à `prefers-reduced-motion`, voir `research.md`) ; variable d'environnement ; touche de bascule à chaud (budget de touches serré, Rule 111).
**Hypothèses** : Hypothèse: un réglage de configuration suffit à couvrir l'esprit de WCAG 2.2.2 (pause/arrêt) pour un terminal. Le nom de la clé YAML est le seul engagement durable du chantier : le renommer plus tard demande une migration.
**Questions ouvertes** : la modification dans `:cfg` (question ouverte 7).

### A-03 — Tick partagé du clignotement (DEVDESK-139)
Epic : E1 — Priorité : Must — Effort : M (risque élevé : touche le cœur du routeur)

En tant qu'utilisateur, je veux que toutes les pastilles clignotent en phase à partir d'un seul tick, afin que l'animation soit sobre et ne coûte qu'un redessin à la fois.

**Critères d'acceptation**
1. Étant donné au moins une pastille « en cours » dans une vue détenue par le routeur, quand le tick tourne, alors la demi-période de phase est de 500 ms (1 Hz, un flash par seconde, sous le seuil de 3 flashs par seconde), la phase est déduite de l'heure portée par le message de tick (`on = (t.UnixMilli() / 500) % 2 == 0`) et toutes les pastilles lisent la même phase.
2. Étant donné plus aucune pastille « en cours » dans aucune vue et plus aucun job actif, quand le dernier tick est traité, alors le tick n'est plus renouvelé et aucun message périodique ne circule (test).
3. Étant donné une opération démarrée alors que le tick est arrêté, quand l'opération entre en cours, alors le tick repart une seule fois ; une chaîne remplacée ne peut pas coexister avec la nouvelle (numéro de séquence, comme D5).
4. Étant donné toute l'application, quand on cherche les ticks qui animent des lignes, alors il n'y en a qu'un : la chaîne D5 du routeur, renommée « animation » (D2). Les chaînes `spinner.TickMsg` de chargement global au footer restent hors périmètre du test.
5. Étant donné `app.motion: static` (A-02), quand seules des pastilles sont en cours, alors le tick n'est jamais lancé.
6. Étant donné une vue qui veut faire clignoter une ligne hors registre (action instantanée `busy`, agent `running`, chargement des enfants d'un groupe), quand le routeur termine un `Update`, alors il l'interroge par une interface optionnelle `WantsBlink() bool` (sur le modèle de `FormView` et `LeavingView`) puis appelle `ensureTick` : signal par niveau, pas par front. Testé avec `go test -race`.
7. Étant donné la cadence, quand un spinner de footer en dépend (registre actif), alors le tick bat à 100 ms ; sinon à 500 ms. La phase qui change n'envoie qu'un `jobs.FrameMsg` (sans copie du registre) à la vue à l'écran, plus la phase courante à l'entrée (`switchView`) ; `ChangedMsg` reste réservé aux changements du registre.
8. Étant donné la mesure `TEA_TRACE` du coût de redessin à 1 Hz (hypothèse H2), quand la story est livrée, alors le résultat (octets émis par seconde avec 1 et 20 lignes clignotantes) est consigné et transmis à l'utilisateur dans la PR.
9. `app-shell.md` décrit le tick (et ce qu'il devient de la chaîne D5) dans le même commit.

**Hors périmètre** : les spinners de chargement global du footer (Rule 128) ; ralentir le spinner du footer des jobs ; tout autre mouvement.
**Hypothèses** : Hypothèse: envelopper `App.Update` (un `update` interne plus un appel final) est faisable sans casser le spinner du footer ; c'est le risque élevé de la story.
**Questions ouvertes** : aucune (fréquence 1 Hz tranchée, D7).

### A-04 — Mémorisateur central du dernier échec par cible (DEVDESK-138)
Epic : E1 — Priorité : Must — Effort : M

En tant qu'utilisateur, je veux que l'échec d'une opération sur une cible reste visible dans les vues qui peuvent la relancer, jusqu'à ce que je réessaie, afin de ne pas perdre un échec en changeant de vue.

**Critères d'acceptation**
1. Étant donné une opération qui échoue sur une cible dans un contexte, quand l'échec est enregistré, alors le mémorisateur le garde sous la clé (type d'opération, cible, contexte) avec la cause lisible ; au plus un échec par clé. Il vit **dans `jobs.Registry`** : posé par `Apply` (`failed`), effacé par `Start` (la clé est admise) et par `Apply` `done` ; écrit seulement depuis l'`Update` du routeur (Rule 110). Les runs terminés sont élagués au-delà de `MaxFinishedRuns`, le mémorisateur n'en dépend donc pas.
2. Étant donné un échec mémorisé, quand la même clé démarre une nouvelle tentative ou réussit, alors l'échec est effacé à cette transition.
3. Étant donné un échec mémorisé, quand la cible est supprimée (`jobs.ForgetTargetMsg{Target}` envoyé par une vue pour une suppression hors registre, ou `delete` `done` du registre), alors l'échec est effacé ; quand l'utilisateur appuie sur `ctrl+r` ou quitte puis rouvre la vue, alors l'échec reste.
4. Étant donné un échec dans le contexte A, quand l'utilisateur est dans le contexte B, alors cet échec n'est pas montré ; en revenant dans A, il l'est encore.
5. Étant donné l'arrêt de l'application, quand elle redémarre, alors aucun échec n'est restauré (aucune écriture disque : test qui vérifie qu'aucun fichier n'est créé). Plafond de 500 clés, les plus anciennes sortent d'abord.
6. Étant donné une vue qui lit le mémorisateur, quand elle le fait, alors elle reçoit une copie (`ChangedMsg.Failures`), jamais un pointeur sur l'état du routeur.
7. Étant donné les deux copies manuelles actuelles (`security.scanTarget.Failed`, `failedScans` d'OCI), quand cette story est livrée, alors elles sont encore en place ; leur retrait relève de A-08 et A-09a, ce qui garde cette story livrable seule.
8. Étant donné la clé, quand deux vues désignent la même cible, alors elles produisent la même clé : `Item.Target` (clé de cache de scan), chemins passés par `filepath.Clean` au lancement ; un dépôt a son chemin absolu en `ws` et dans `:sec`, une image sa référence brute (`repo:tag`) dans OCI et dans `:sec`. Un test de clé commune couvre les images sans tag (`<none>`) et les références écrites autrement (`nginx` contre `docker.io/library/nginx`). Une image et un dépôt ne sont pas la même cible.
9. Étant donné D5, quand une vue lit le mémorisateur, alors elle ne retient que les types d'opération qu'elle peut relancer. Table indicative de l'architect : `ws` scan, sync, delete ; `:sec` scan ; OCI scan, pull ; explorer create, delete, clone ; templates scan, sync. Conséquence : un échec de sync dans `ws` n'apparaît pas dans `:sec`. Les types réellement mémorisés restent la question ouverte 2.
10. `app-shell.md` décrit le mémorisateur (clé, durée de vie, remplacement des copies manuelles) dans le même commit.

**Hors périmètre** : persistance, expiration automatique, échec d'actions instantanées (restent au footer), affichage (A-06 et suivantes).
**Questions ouvertes** : identité canonique et types d'opération mémorisés (question ouverte 2).

### A-05 — Footer par ligne : ce qui s'exécute, ou la cause de l'erreur (DEVDESK-140)
Epic : E1 — Priorité : Must — Effort : M

En tant qu'utilisateur, je veux que le footer dise, pour la ligne sélectionnée, ce qui s'exécute ou pourquoi elle est en erreur, afin de comprendre la pastille sans ouvrir `:jobs`.

**Critères d'acceptation**
1. Étant donné une ligne sélectionnée dont la pastille est « en cours », quand le footer s'affiche, alors il nomme l'opération au niveau `Status` de `components.FooterMessage` (Rule 128), avec le glyphe de la pastille en préfixe en texte brut, selon les phrases validées de `ux.md` §1.2 (ex. `● Scanning ~/w/app/api`).
2. Étant donné une ligne sélectionnée en erreur, quand l'échec arrive, alors un `Error` de 3 s le dit (`Failed to scan … — <cause>`) ; ensuite, tant que la ligne est sélectionnée, un `Status` persistant donne la cause lisible (`⊗ <cible>: last scan failed — <cause>`), jamais `err.Error()` brut non rédigé pour l'écran.
3. Étant donné une progression globale en cours (« 3/12 ») ou un bilan de sync, quand l'utilisateur sélectionne une ligne, alors la progression et le bilan restent affichés : un seul constructeur dans `components`, à côté de `JobsStatusLine`, compose « détail de ligne · progression globale » et tronque d'abord le détail de ligne (précédence error > warn > info > status inchangée).
4. Étant donné un message `Error`, `Warn` ou `Info` avec minuteur, quand il est affiché, alors il prend le pas sur le détail de ligne et celui-ci revient à l'expiration.
5. Étant donné une ligne au repos, quand elle est sélectionnée, alors le footer ne montre aucun texte d'activité. Un conteneur `running` garde « Running since 3 hr ago », **sans glyphe en préfixe** (D8).
6. Étant donné plusieurs opérations sur la ligne, quand le footer s'affiche, alors il nomme l'état gagnant (priorité de A-01) et, si un échec est masqué, l'ajoute en suffixe (`· earlier failure: sync (see :jobs)`).
7. Étant donné un terminal étroit, quand le texte dépasse la largeur, alors il est tronqué proprement et la hauteur du footer ne change pas (Rule 124).
8. Le texte est identique pour la même opération dans toutes les vues (un seul constructeur de phrase, test).

**Hors périmètre** : le câblage dans chaque vue (fait dans sa story de migration) ; les messages d'actions instantanées.
**Questions ouvertes** : aucune (libellés validés, D10).

---

## Epic E2 — Migration des vues

### A-06 — Pilote : `:jobs` avec la pastille, et décision go/no-go sur le clignotement (DEVDESK-142)
Epic : E2 — Priorité : Must — Effort : S (+ une semaine d'usage avant la décision)

En tant qu'utilisateur de `:jobs`, je veux voir l'état de chaque run et de chaque cible dans la première colonne via la pastille, afin de repérer ce qui tourne et ce qui a échoué, et de juger si le clignotement m'aide avant qu'il soit étendu partout.

**Critères d'acceptation**
1. Étant donné la table des runs et celle des cibles, quand elles s'affichent, alors la première colonne est la pastille (sans titre, `datatable.IconColumnWidth`, `SizingFixed`, ni `Less` ni `Search`) et le spinner de ligne actuel est supprimé. `:jobs` a déjà la colonne : on remplace le glyphe et on retire la frame.
2. Étant donné les issues, quand elles s'affichent, alors `done` est `◉`, `failed` `⊗` (rouge), `cancelled` `⊘`, `skipped` `⊖`, `queued` `◴`, `running` `●`/`○` ; tous différents entre eux ; `done` n'est pas vert.
3. Étant donné un run « en cours », quand le tick bat, alors la pastille clignote ; avec `app.motion: static`, elle est `●` fixe (A-02).
4. Étant donné une ligne sélectionnée, quand le footer s'affiche, alors il donne l'état détaillé (A-05) ; la colonne Kind garde l'état dans son `Search` pour que `/failed` fonctionne toujours.
5. Étant donné un terminal de 60 colonnes, quand la table s'affiche, alors la Rule 116 tient (étendue rendue = largeur − 2, via `RenderedWidth()`) aux deux niveaux.
6. L'aide `?` contient la légende de base et l'extension `:jobs` (`◉ done · ⊘ cancelled · ⊖ skipped`) ; `GetShortcuts()` est inchangé (Rule 130).
7. Le squelette du test garde-fou (A-16) est livré dans la même PR, avec sa liste d'exceptions.
8. **Premier point de contrôle (H1, H2).** Après une semaine d'usage réel, l'utilisateur répond **go**, **go avec `app.motion` à `static` par défaut**, ou **no-go** sur le clignotement ; la réponse est consignée avec la mesure H2 (A-03) et l'absence de régression `mise run test-race` sur `internal/app`, avant toute autre migration. Un no-go ne perd aucune fondation : le réglage passe à `static` par défaut, la pastille, le mémorisateur et le footer restent.
9. **Second point de contrôle (H5, R3)**, consigné à la livraison de A-07 (`ws`) et avant d'ouvrir A-08 à A-14 : le pilote ne valide ni H5 (sa première colonne est déjà un glyphe d'état, aucune icône n'y est remplacée), ni le signal par niveau R3 (tout y vient du registre), ni le mémorisateur.

**Hors périmètre** : l'échec mémorisé de A-04 (`:jobs` lit déjà le registre), le footer global « 2 jobs running ».
**Hypothèses** : Hypothèse: `:jobs` est le meilleur pilote pour la décision sur le clignotement parce qu'il a déjà une colonne de glyphe, un spinner fourni par le routeur et un registre.
**Questions ouvertes** : vue pilote et plan B d'un no-go (question ouverte 1).

### A-16 — Test garde-fou transverse (DEVDESK-143)
Epic : E2 — Priorité : Must — Effort : M

En tant que mainteneur, je veux un test qui scanne les sources et échoue quand une vue migrée s'écarte de la règle, afin que l'uniformité survive aux prochaines vues.

**Critères d'acceptation**
1. Étant donné la liste des vues migrées, quand le test s'exécute à l'exécution, alors la colonne 0 de chaque table migrée est une colonne `Indicator` (sans titre, `IconColumnWidth`, `SizingFixed`, ni `Less` ni `Search`) ; sinon il échoue en nommant la vue.
2. Étant donné les sources, quand le test les scanne, alors il échoue en nommant fichier, ligne et colonne pour : (a) tout `tea.Tick` qui produit un message d'animation de ligne hors de `internal/app` ; (b) tout appel à `AdvanceSpinner`, tout `StatusColumn:` et tout champ `SpinnerFrame` ou `frame` lu par un `Cell`, dans une vue migrée ; (c) `portsSpinnerCmd` (doit avoir disparu) ; (d) avec `app.motion: static`, `ensureTick` renvoyant autre chose que `nil` quand seules des pastilles sont en cours. C'est la forme du critère de succès 5 (D2) : **un seul tick anime les lignes** ; les chaînes de chargement global du footer sont hors périmètre du test.
3. Étant donné les glyphes d'état de toutes les vues migrées, quand le test les compare, alors il n'en trouve aucun en double entre deux états de sens différents.
4. Étant donné chaque vue migrée construite à 60 colonnes, quand la table est rendue, alors la Rule 116 tient, mesurée par `RenderedWidth()` et jamais par la somme des colonnes déclarées.
5. Étant donné chaque vue migrée, quand on lit son aide, alors elle contient la légende (A-01) et son pied de page porte le détail de la ligne sélectionnée (A-05).
6. Étant donné les vues pas encore migrées, quand le test s'exécute, alors elles sont dans une liste d'exceptions déclarée dans le test ; ajouter une exception échoue à la relecture, en retirer une est la seule évolution permise.
7. Étant donné la dernière migration livrée, quand la liste d'exceptions est vide, alors elle est supprimée du test, et `StatusColumn`, `AdvanceSpinner` et `spinnerFrame` sont supprimés de `datatable` dans la même PR finale.
8. Le test ne se greffe pas sur `TestNoTableViewRendersALoadingBody` (il vise les corps, pas les lignes) ; `TestNoShortcutAdvertisesAGlyphAsAKey` et `TestEveryJobReporterIsRoutedToTheRegistry` continuent de passer à chaque PR de migration.

**Hors périmètre** : la qualité visuelle (revue du ux-designer) ; les tests propres à chaque vue.
**Questions ouvertes** : aucune.

### A-07 — `ws` : pastille pour les opérations en cours et l'échec par ligne (DEVDESK-144)
Epic : E2 — Priorité : Must — Effort : M (fusion de l'ancienne A-07a et A-07b, A-04 étant livré)

En tant qu'utilisateur de `ws`, je veux voir sur la ligne d'un dépôt qu'un scan ou une synchronisation tourne, ou qu'il a échoué et pourquoi, afin de ne pas lancer deux fois la même opération et de savoir quoi relancer sans relire un journal.

**Critères d'acceptation**
1. Étant donné la table de `ws`, quand elle s'affiche, alors la première colonne est la pastille et l'icône de fichier, dossier ou dépôt revient au repos (même colonne, `IconColumnWidth`).
2. Étant donné un scan, une synchronisation (`F`) ou une suppression en cours sur un dépôt, quand la table s'affiche, alors sa pastille clignote, le footer nomme l'opération pour la ligne sélectionnée, et les colonnes Scanned et Git Status montrent un texte statique (`scanning`, `syncing`, `deleting`) en `DimStyle`, sans frame.
3. Étant donné une synchronisation de plusieurs dépôts avec un bilan, quand l'utilisateur sélectionne une ligne, alors le bilan et la progression « 3/12 » restent affichés (A-05, critère 3).
4. Étant donné une opération qui se termine sans erreur, quand elle finit, alors la pastille redevient l'icône de la ligne (pas de vert).
5. Étant donné un scan ou une synchronisation en échec sur un dépôt, quand l'échec est posé, alors la pastille de cette ligne devient `⊗` rouge fixe et le footer donne la cause quand elle est sélectionnée ; seule la ligne en échec est rouge après une synchronisation de plusieurs dépôts.
6. Étant donné la pastille rouge, quand l'utilisateur relance l'opération sur cette ligne, alors la pastille passe à « en cours » ; à la réussite elle redevient l'icône de la ligne. Après `ctrl+r`, ou une sortie puis un retour dans `ws`, la pastille rouge est toujours là.
7. Étant donné un échec de **scan** posé depuis `ws`, quand l'utilisateur ouvre `:sec` pour le même dépôt, alors il y est visible (vérifié de bout en bout dans A-08, critère de succès 4) ; un échec de **sync** reste propre à `ws` (D5).
8. Étant donné les raccourcis de `ws`, quand une ligne a une opération en cours, alors la grisure des touches ne change pas (Rule 130, `availability.go` reste la seule source).
9. Terminal de 60 colonnes : Rule 116 tient ; l'aide `?` porte la légende.

**Hors périmètre** : toute nouvelle colonne (`ws` a déjà sa colonne d'icône) ; l'agir depuis la ligne.
**Note** : l'architect conseille de ne garder deux stories (opération en cours, puis échec) que si A-04 prend du retard.
**Questions ouvertes** : aucune propre à cette story.

### A-08 — `:sec` : pastille et retrait de la copie d'échec (DEVDESK-145)
Epic : E2 — Priorité : Must — Effort : M

En tant qu'utilisateur de `:sec`, je veux la même pastille et les mêmes échecs de scan que dans `ws` et OCI, afin de ne plus perdre un échec en changeant de vue.

**Critères d'acceptation**
1. Étant donné l'inventaire, quand il s'affiche, alors la première colonne est la pastille (icône de dépôt ou d'image au repos) et le spinner de ligne actuel est supprimé.
2. Étant donné `security.scanTarget.Failed`, quand cette story est livrée, alors le champ est supprimé et l'inventaire lit le mémorisateur (A-04), pour les scans seulement (D5).
3. Étant donné un scan en cours sur une ligne, quand la table s'affiche, alors la colonne `Scanned` affiche le texte statique `scanning` en `DimStyle` (jamais un ancien « 3 days ago » trompeur, aucune frame) et la pastille clignote.
4. Étant donné un échec de scan posé depuis `ws` pour le même dépôt, quand l'utilisateur ouvre `:sec`, alors la ligne est `⊗` rouge et le reste jusqu'au prochain essai ; un nouvel essai l'efface. Il en va de même pour une image avec OCI (critère de succès 4).
5. Étant donné les résultats d'un scan déjà en cache, quand une ligne est au repos, alors rien du cache disque n'est modifié par l'affichage (Rule 126 inchangée).
6. Terminal de 60 colonnes : Rule 116 tient ; l'aide `?` porte la légende ; le footer par ligne est câblé (A-05).

**Hors périmètre** : les onglets de résultats, la colonne CI.
**Questions ouvertes** : aucune propre à cette story.

### A-09a — OCI : images (pastille, nouvelle colonne, retrait de la copie d'échec) (DEVDESK-146)
Epic : E2 — Priorité : Must — Effort : M

En tant qu'utilisateur de la vue OCI, je veux la pastille sur chaque image pendant un scan, un pull ou une suppression, et voir un échec de scan posé ailleurs, afin d'avoir un seul langage visuel.

**Critères d'acceptation**
1. Étant donné la liste d'images, qui n'a pas de colonne d'icône (la première est l'ID, 14 cellules), quand elle s'affiche, alors une **nouvelle colonne de pastille** est ajoutée en première position (D1) : sans titre, `datatable.IconColumnWidth`, `SizingFixed`, ni `Less` ni `Search` (Rule 125) ; l'icône d'image est le repos ; les spinners de ligne de `busy.go` et des colonnes `Scanned` et ID sont supprimés (`scanning`, `pulling` en texte statique `DimStyle`).
2. Étant donné la nouvelle colonne (4 cellules rendues avec le rembourrage), quand le terminal fait 60 colonnes, alors la Rule 116 tient (`RenderedWidth()` = largeur − 2), les colonnes `Optional` qui tombent plus tôt sont documentées, et les tests de largeur et les seuils écrits dans `network.md` sont recalés dans le même commit.
3. Étant donné `failedScans`, quand cette story est livrée, alors le champ est supprimé et la vue lit le mémorisateur (A-04) pour les scans et les pulls (D5) ; un échec de scan posé depuis `:sec` pour la même image y est visible et s'efface au prochain essai.
4. Étant donné un pull en cours, quand la table s'affiche, alors la ligne fantôme du pull montre la pastille ; un `docker pull` en cours dans son écran d'opération (sans table) n'est pas modifié (exception déclarée de la Rule 139).
5. Étant donné une suppression d'image (action instantanée, `busy`), quand elle tourne, alors la pastille `●`/`○` apparaît ; son échec reste au footer.
6. L'aide `?` porte la légende ; le footer par ligne est câblé ; les raccourcis n'embarquent aucune frame de spinner (`TestNoShortcutAdvertisesAGlyphAsAKey`).

**Hors périmètre** : mise à jour d'image, vérification de signature, le navigateur de registre (A-09a2), les trois autres tables (A-09b).
**Questions ouvertes** : identité commune entre `:sec` et OCI (question ouverte 2).

### A-09a2 — OCI : navigateur de registre (pastille) (DEVDESK-218)
Epic : E2 — Priorité : Must — Effort : S

Scindée de A-09a sur décision de l'utilisateur (2026-11-09).

En tant qu'utilisateur du navigateur de registre OCI, je veux la pastille sur chaque tag pendant un scan, afin d'avoir le même langage visuel que dans le reste de l'application.

**Critères d'acceptation**
1. Étant donné le navigateur de registre (liste des tags, `registry_browser.go`, écran `oci/registry-browser`), dont les colonnes C/H/M/L portent aujourd'hui une frame propre (`tagCVEColumn`, `r.frame`, avancée par `tagScanSpinnerIdx` dans `browser_keys.go`), quand il s'affiche, alors une **8e colonne de pastille** (hors des 7 tables de D1, décision utilisateur du 2026-11-06) est ajoutée en première position (sans titre, `IconColumnWidth`, `SizingFixed`, ni `Less` ni `Search`, Rule 125) ; un scan de tag en cours y montre `●`/`○` (source : le registre, scan) ; la frame et l'index de frame locaux sont supprimés ; les colonnes C/H/M/L affichent un texte statique en `DimStyle` pendant le scan (comme `scanning` ailleurs) ; Rule 116 tient à 60 colonnes (seuils et tests recalés) ; les recherches du navigateur gardent leur spinner de chargement global au footer ; l'aide `?` porte la légende. Le navigateur n'a pas d'échec mémorisé à retirer.

**Hors périmètre** : la liste d'images (A-09a), les trois autres tables (A-09b).
**Dépendances** : A-09a (DEVDESK-146).

### A-09b — OCI : networks, volumes, registries (DEVDESK-153)
Epic : E2 — Priorité : Must — Effort : S

En tant qu'utilisateur de la vue OCI, je veux la même pastille sur les networks, volumes et registries, afin de voir qu'un remove ou un login tourne.

**Critères d'acceptation**
1. Étant donné ces trois tables, qui n'ont ni icône ni colonne de pastille, quand elles s'affichent, alors la même colonne de pastille que A-09a est ajoutée en première position (D1) ; repos = cellule vide de même largeur si la table n'a pas d'icône.
2. Étant donné un remove, un login ou un logout en cours (actions instantanées, `busy`), quand il tourne, alors la pastille `●`/`○` l'indique ; son échec reste au footer.
2bis. Étant donné la table des registries, dont la colonne Members affiche un spinner `refreshing` pour un groupe en rafraîchissement (`membersCell`, `table.go:405-408`, carte `refreshingGroups`), quand le groupe se rafraîchit, alors la pastille `●`/`○` de la ligne l'indique (état calculé par la vue, signal `WantsBlink`, A-03 critère 6), le spinner de la cellule est supprimé (texte statique en `DimStyle`) et le rafraîchissement périodique ne déclenche aucun clignotement hors de l'opération.
3. Étant donné les quatre tables d'OCI migrées, quand cette story est livrée, alors l'`AdvanceSpinner` et la chaîne locale de spinner de ligne d'OCI sont supprimés.
4. Terminal de 60 colonnes : Rule 116 tient pour chaque table ; l'aide `?` porte la légende.

**Hors périmètre** : le spinner de chargement global d'OCI (reste au footer).

**Livrée (2026-11-09, DEVDESK-153, D107)** : les trois tables prennent `datatable.IndicatorColumn` ; `refreshing` d'un groupe est un état `Running` de la ligne (Members en texte statique) ; `advanceBusySpinners`, `busyTick`, `spinnerFrameIdx` et les `AdvanceSpinner` d'OCI sont supprimés. Le spinner de chargement reste au footer.

### A-10a — Explorer : arbre (DEVDESK-147)
Epic : E2 — Priorité : Should — Effort : S

En tant qu'utilisateur de l'explorer de forge, je veux voir sur chaque nœud qu'un dépôt est en création ou suppression, afin de suivre l'opération sans lire un journal.

**Critères d'acceptation**
1. Étant donné l'arbre, quand un dépôt est en création ou suppression (jobs `create` et `delete`, source : le registre, sans `WantsBlink`), alors la première colonne montre la pastille (l'icône de dossier ou de dépôt existe déjà : repos) et le spinner de nœud actuel est supprimé.
2. Étant donné le chargement global de l'arbre (premier chargement, walk de l'index), quand il s'exécute, alors il reste un spinner au footer (Rule 128).
3. Étant donné un `create` ou un `delete` en échec, quand l'utilisateur sélectionne la ligne, alors le footer donne la cause (kinds relançables depuis l'explorer, D5).
4. Étant donné un `create` ou un `delete` non annulable, quand il est en cours, alors la pastille ne laisse pas croire qu'il peut l'être (la touche `K` garde sa raison grisée).
5. Terminal de 60 colonnes : Rule 116 tient ; l'aide `?` porte la légende.

**Hors périmètre** : l'index de forge, l'authentification, la liste de clone (A-10b), le chargement des enfants d'un groupe (aucun spinner par nœud n'existe : `TreeNode.Loading` n'est jamais rendu sur une ligne, le `→` entre aussitôt dans le groupe ; le chargement reste global au footer).
**Hypothèses** : Hypothèse: le type du nœud (groupe, dépôt) reste lisible par le nom pendant l'opération (H5, vérifiée au second point de contrôle).

### A-10b — Explorer : liste de clone (DEVDESK-148)
Epic : E2 — Priorité : Should — Effort : M

En tant qu'utilisateur de l'explorer de forge, je veux voir sur chaque dépôt d'un clone multiple s'il est en file, en cours, terminé ou en échec, afin de suivre le lot sans lire un journal.

**Critères d'acceptation**
1. Étant donné la liste de clone, dont la première colonne est Repository (pas d'icône), quand elle s'affiche, alors une **nouvelle colonne de pastille** est ajoutée en première position (D1, Rule 125) ; les cinq états actuels de la colonne Status sont projetés sur la pastille sans en perdre un (en file `◴`, en cours `●`/`○`, terminé, en échec `⊗`, et l'état restant) ; la colonne Status est conservée.
2. Étant donné un lot de N clones, qui tournent en parallèle (`forge.pull.parallel_jobs`), quand il tourne, alors les clones en cours clignotent (plusieurs lignes peuvent clignoter en même temps) et les autres, en file, sont des `◴` fixes ; la frame D5 déjà présente dans la colonne Status (`clone_list.go:167-176`) est supprimée avec le reste des frames de ligne ; la progression globale du clone reste affichée au footer.
3. Étant donné un clone en échec, quand l'utilisateur sélectionne la ligne, alors le footer donne la cause et la progression globale reste affichée.
4. Terminal de 60 colonnes : Rule 116 tient avec la colonne ajoutée (seuils et tests recalés) ; l'aide `?` porte la légende.

**Hors périmètre** : l'index de forge, l'authentification, l'arbre (A-10a).
**Hypothèses** : Hypothèse: « terminé » d'un clone se représente par `◉` (terminé uniquement, D7) ; à confirmer au moment de projeter les cinq états.

### A-11 — Templates : pastille sur le catalogue (DEVDESK-149)
Epic : E2 — Priorité : Should — Effort : S

En tant qu'utilisateur de `:templates`, je veux voir sur chaque modèle qu'un chargement ou une synchronisation tourne ou a échoué, afin de savoir quels modèles sont à jour.

**Critères d'acceptation**
1. Étant donné le catalogue, quand il s'affiche, alors la première colonne est la pastille, avec l'icône de la source ou du modèle au repos (la colonne d'icône existe déjà).
2. Étant donné une synchronisation en cours (ou un chargement de fichier), quand elle tourne, alors les lignes concernées clignotent ; à la fin sans erreur, l'icône revient.
3. Étant donné un échec de scan ou de synchronisation sur une source, quand il est posé, alors la ligne est `⊗` rouge fixe, le footer donne la cause, et un nouvel essai l'efface (kinds relançables depuis templates, D5).
4. Étant donné le message de progression de la synchronisation (`jobs.Reporter` avec son `case` dans le routeur), quand la story est livrée, alors `TestEveryJobReporterIsRoutedToTheRegistry` passe toujours.
5. Terminal de 60 colonnes : Rule 116 tient ; l'aide `?` porte la légende.

**Hors périmètre** : les sources et la récupération de fichiers. Il n'y a aucun spinner de ligne à retirer aujourd'hui.

### A-12 — Netdiag : pastille pour les ports (DEVDESK-150)
Epic : E2 — Priorité : Should — Effort : S

En tant qu'utilisateur de la vue réseau, je veux voir sur quelle ligne de ports une action tourne, afin de savoir ce qui attend un résultat.

**Critères d'acceptation**
1. Étant donné la table des ports, qui n'a pas de colonne d'icône (la première est Proto) et qui est la seule table de netdiag avec un spinner de ligne (`K`, arrêt d'un processus), quand elle s'affiche, alors une **nouvelle colonne de pastille** est ajoutée en première position (D1, Rule 125) ; l'action `K` en cours y montre `●`/`○` (`busy`) ; son échec reste au footer ; le spinner de ligne et `portsSpinnerCmd` sont supprimés.
2. Étant donné les tables des diagnostics (checks) et des interfaces, qui n'ont aucun spinner de ligne, quand la story est livrée, alors elles ne changent pas (sans objet pour la pastille) ; la progression reste au footer.
3. Étant donné le suivi des ports en temps réel, quand la liste se rafraîchit toutes les n secondes, alors aucune pastille ne clignote pour un rafraîchissement périodique (donnée, pas opération).
4. Étant donné un test dont le résultat est un échec réseau (TCP refusé, TLS invalide), quand il se termine, alors le **résultat** reste dans sa colonne propre (Verdict) ; la pastille rouge est réservée à l'échec de l'**opération** (recommandation du PM et de l'architect, qui dépend de la question ouverte 4).
5. Étant donné le chargement global d'un onglet, quand il s'exécute, alors il reste un spinner au footer (Rule 139).
6. Terminal de 60 colonnes : Rule 116 tient avec la colonne ajoutée (seuils de `network.md` recalés) ; l'aide `?` porte la légende.

**Hors périmètre** : l'action `H` ; le comportement des checks et des interfaces.
**Questions ouvertes** : aucune (question ouverte 4 résolue le 2026-11-09, option A).

### A-13 — `:agent-usage` : états d'agent en pastille et coloration du coût (absorbe DEVDESK-99) (DEVDESK-151)
Epic : E2 — Priorité : Should — Effort : M
Statut : **livrée, en revue** (DEVDESK-151). Écart assumé au critère 3 : la colonne State est placée **après** Duration (option B de la question de largeur) pour que Tools et Duration restent visibles à 80 colonnes ; la pastille porte l'état quand State disparaît (dès 83 colonnes). Seuils recalés (+4) : Breakdown dès 110, Est. cost dès 142, Cache write dès 155.

En tant qu'utilisateur de DevDesk, je veux voir l'état de chaque agent (en cours, inactif, terminé, inconnu) dans la première colonne, y compris sur un terminal étroit où la colonne State disparaît, et repérer un agent coûteux, afin de repérer un agent inactif ou onéreux d'un coup d'œil.

**Critères d'acceptation**
1. Étant donné le tableau des agents, qui n'a pas de colonne d'icône (la première est Agent), quand il s'affiche, alors une **nouvelle colonne de pastille** est ajoutée en première position (D1) : sans titre, `IconColumnWidth`, `SizingFixed`, ni `Less` ni `Search` (Rule 125), avec un glyphe et une couleur par état `running`, `idle`, `done`, `unknown` pris dans `Agent state` (DEVDESK-46) sans nouvelle règle de calcul.
2. Étant donné les quatre états, quand on compare les glyphes, alors ils sont quatre glyphes différents (test) : `running` `●`/`○` clignote (lavande) ; `idle` `◐` orange fixe ; `done` `◉` couleur neutre (pas de vert) ; `unknown` `◌` couleur neutre, **pas dim** (contraste, D7).
3. Étant donné un terminal de 60 colonnes où la colonne State est abandonnée, quand le tableau s'affiche, alors la pastille reste visible et la Rule 116 tient ; les seuils de largeur documentés (colonne Breakdown « visible dès 106 colonnes », Est. cost) sont recalés dans les tests et dans `agent-usage.md`, dans le même commit.
4. Étant donné la colonne State en texte, quand la story est livrée, alors elle est conservée.
5. Étant donné un agent `running` dans une session active, quand le tick bat, alors sa pastille clignote (état calculé par la vue, `WantsBlink`) ; avec `app.motion: static`, elle est `●` fixe ; la lecture incrémentale toutes les 2 s (donnée) n'ajoute aucun tick d'animation.
6. Étant donné une ligne sélectionnée, quand le curseur passe dessus, alors l'état reste lisible sans la couleur (glyphe) et le footer l'écrit en toutes lettres (`ux.md` §1.2) ; l'aide `?` porte la légende et l'extension `:agent-usage` (`◐ idle (no activity for 5 min) · ◉ done · ◌ unknown`).
7. Étant donné la cellule « Est. cost » d'un agent, quand son coût atteint les seuils orange ou rouge (`theme.LoadWarnPercent` = 75, `theme.LoadCriticalPercent` = 90, `theme.LoadTextStyle`, déjà dans `internal/ui/theme/gauge.go`) en part du coût estimé total de la session, alors elle prend la couleur correspondante via `Style` ; sous le seuil, couleur de texte ; `-` reste dim ; sans au moins deux agents chiffrés, rien n'est coloré. Repris de DEVDESK-99 ; **ce n'est pas un indicateur d'activité** : c'est une exception assumée par l'utilisateur (D6) au principe « la couleur dit l'état », sans lien avec les alias d'activité.

**Hors périmètre** : icône de type orchestrateur / sous-agent (elle supposerait une seconde colonne ; D1 n'en accepte qu'une, celle de la pastille), teinte par famille de modèle, icônes dans la liste des sessions, jauge de coût (S-09), variante sans Nerd Font.
**Hypothèses** : Hypothèse (de DEVDESK-99): l'icône d'état vaut surtout sur terminal étroit ; sur un terminal large elle répète la colonne State.
**Note** : le critère 7 n'a **aucune dépendance de code** envers S-09 (DEVDESK-97), qui n'est pas livrée : les constantes existent déjà. L'architect proposait de sortir ce critère en story séparée ; l'utilisateur l'a gardé ici (D6), A-13 reste M. DEVDESK-99 sera close à la création du backlog (D11).
**Questions ouvertes** : aucune propre à cette story (quand clore DEVDESK-99 : question ouverte 6).

### A-14 — Containers : états observés docker en pastille (DEVDESK-152)
Epic : E2 — Priorité : Should — Effort : M

En tant qu'utilisateur de la vue containers, je veux voir l'état observé de chaque conteneur qui n'est pas nominal dans la première colonne, afin de repérer ceux qui redémarrent, sont en pause, arrêtés ou morts, sans spinner par ligne.

**Critères d'acceptation**
1. Étant donné la table des conteneurs, quand elle s'affiche, alors la première colonne est la pastille, calculée par la vue depuis les états observés par docker ; le spinner de ligne et l'`AdvanceSpinner` sont supprimés.
2. Étant donné un conteneur `running` (état nominal, Rule 122), quand il s'affiche, alors il n'a **aucune pastille** : la ligne montre son icône habituelle (D8) ; `◉` n'est pas utilisé ici. Hypothèse: l'icône de repos est l'icône de type de conteneur ; son rôle `IconRole*` est à préciser à l'implémentation.
3. Étant donné les autres états, quand on compare leurs glyphes, alors ils sont tous différents entre sens différents (test) : `restarting` `◕` orange fixe ; `paused` `◐` orange fixe ; `created` `◌` neutre ; `exited` code ≠ 0 `⊗` rouge fixe ; `exited` code 0 `⊖` neutre ; `dead` `⊗` rouge fixe.
4. Étant donné une action instantanée (`docker stop`, `restart`, `pause`, `unpause`, `rm`) en cours, quand elle s'exécute, alors la pastille `●`/`○` apparaît (`busy`, priorité « en cours », signal `WantsBlink`), puis l'icône ou le glyphe d'état observé revient au sondage suivant ; son échec reste au footer (non-objectif).
5. Étant donné les filtres d'état `r p s t z` (Rule 111), quand ils changent, alors leur comportement et leur vocabulaire ne changent pas.
6. Étant donné un rafraîchissement périodique des métriques, quand il s'exécute, alors aucun clignotement n'est déclenché (donnée, pas opération) ; `restarting` ne clignote pas (état observé).
7. Étant donné une ligne sélectionnée, quand le footer s'affiche, alors il détaille l'état (`ux.md` §1.2) ; pour un conteneur `running`, « Running since 3 hr ago » **sans glyphe en préfixe**.
8. Terminal de 60 colonnes : Rule 116 tient ; les jauges CPU/mémoire (`Cut`/`TailStyle`) ne sont pas touchées ; l'aide `?` décrit les glyphes de la légende de base et l'extension containers (`◕ restarting · ◐ paused · ◌ created · ⊖ exited (code 0) · ⊗ exited (non-zero code) or dead · ●/○ an action (stop, restart, pause, resume, delete) is running`, sans `◉ running`) et remplace l'aide existante (`view.go`).

**Hors périmètre** : migration des actions instantanées vers le registre ; les jauges.
**Questions ouvertes** : aucune (exited code 0 et restarting tranchés, D7).

**Livrée (2026-11-09, DEVDESK-152, D106)** : l'état et le code de sortie viennent du texte `Status` de `docker ps` (`docker.ParseStatus`), pas de `docker inspect` ; l'icône de repos est `IconContainer`, rôle `IconRoleContainer` (clé de thème `icon_container`, défaut `ColorText`). La légende de l'extension containers est amendée (D10 of the activity-indicator stories, critère 8) : « an action (stop, restart, pause, resume, delete) is running », la vue n'ayant pas de `kill`.

---

## Epic E3 — Documentation

### A-17 — Documentation (réduite) : règles `.claude/rules/`, `docs-site`, carte de `.claude/CLAUDE.md` (DEVDESK-154)
Epic : E3 — Priorité : Must — Effort : S

En tant que contributeur, je veux que la règle de la pastille soit écrite là où les agents la lisent, afin que la prochaine vue l'applique sans réinventer un spinner.

`.claude/CLAUDE.md` impose de mettre à jour `docs/architecture/*.md` **dans le même commit que le code** : `app-shell.md` suit A-03 et A-04, `configuration.md` suit A-02, les seuils de largeur suivent leur migration. Les amendements des Rules 121 et 125 sont dans A-01 (D9).

**Critères d'acceptation**
1. Étant donné `docs/architecture/app-shell.md`, `configuration.md` et les notes de largeur, quand la story est livrée, alors une relecture confirme que le tick partagé (et ce qu'il devient de la chaîne D5), le mémorisateur d'échecs, la frontière entre pastille par ligne et spinner de chargement global, `app.motion` et les nouveaux seuils sont décrits, sans doublon ni contradiction.
2. Étant donné `.claude/rules/tui-tables.md`, quand la story est livrée, alors la Rule 125 renvoie au test garde-fou (A-16), et les Rules 128 et 139 renvoient à la pastille pour le cas des spinners de ligne.
3. Étant donné `docs-site/docs/reference/`, quand la story est livrée, alors la légende des glyphes y figure, sans `◉ running` pour containers.
4. Étant donné `.claude/CLAUDE.md`, quand la story est livrée, alors sa carte d'architecture mentionne le composant et le mémorisateur.

**Hors périmètre** : la documentation des vues non migrées.

---

## Questions ouvertes

Rien ci-dessous n'est tranché ; la recommandation du PM, quand il y en a une, est une proposition. Les décisions prises sont dans le tableau en tête de fichier et dans `brief.md`.

1. **Vue pilote et plan B du go/no-go** (A-06). Recommandation: `:jobs` pour la décision sur le clignotement (déjà une colonne de glyphe, un spinner fourni par le routeur, un registre) ; alternative: `ws`, plus exposée mais sans état d'échec aujourd'hui. Plan B d'un no-go : `app.motion` à `static` par défaut partout (la pastille, le mémorisateur et le footer restent), ou arrêt du chantier ?
2. **Identité de la cible et types d'opération mémorisés** (A-04). La clé (`Kind`, `Target`, `Context`) reprend celle du registre ; une image et un dépôt ne sont pas la même cible (pas de pont) ; cas résiduels : images sans tag, références écrites autrement. Quels types d'opération le mémorisateur garde-t-il (scan seulement, ou aussi sync, clone, pull, create, delete) ? La table de A-04 (critère 9) est indicative de l'architect.
3. **Vues hors inventaire** (A-00) : **close le 2026-11-06.** Filebrowser, remédiation `:sec` et sessions `:agent-usage` : hors lot, accepté. Vues « sans objet » (dashboard, status, viewer, setup, auth forge, etc.) : acceptées. Le navigateur de registre OCI est sorti de A-09a vers A-09a2 (8e colonne, DEVDESK-218).
4. **Netdiag : échec de test réseau** (A-12) : résultat de ligne (recommandation du PM et de l'architect) ou échec d'opération mémorisé ?
   **Résolu (2026-11-09, DEVDESK-150)** : option A, le résultat reste dans la colonne Verdict ; pas de `⊗` ni de mémorisation d'échec pour un test réseau. La pastille rouge reste réservée à l'échec d'une opération.
5. **Découpage des PR** : une PR par story (recommandation du PM et de l'architect, avec A-08 et A-09a qui se relisent mieux ensemble mais restent deux PR) ou regroupement des vues Should ?
6. **Quand clore DEVDESK-99.** La décision D11 dit « à la création du backlog » ; question gardée à la demande de l'utilisateur, à confirmer ou à retirer.
7. **Confirmation du réglage dans `:cfg`.** La phrase « or in :cfg » de la légende (A-01) et les critères 4 et 7 de A-02 supposent que `app.motion` est modifiable dans la vue de configuration (champ cyclique `←→`, Rule 132, comme `app.gauge_glyph`). À vérifier par l'architect ; sinon retirer la phrase.
   **Résolu (2026-11-07, DEVDESK-141)** : le réglage est exposé dans `:cfg` (champ cyclique `←→`, onglet `app`) en plus du YAML ; une clé absente reste absente tant qu'on ne cycle pas. La phrase est conservée.
8. **Test de contraste.** Les rapports de `ux.md` §2.1 (au moins 3:1 pour chaque alias sur le fond et sur la ligne sélectionnée) sont des calculs à la main sur les valeurs Catppuccin par défaut ; le test de A-01 (critère 7) doit les confirmer pour chaque thème livré.

Note de méthode : `research.md` ne contient pas de section de critique distincte de la recherche ; les risques de `brief.md` viennent de la liste transmise avec l'idée et des conclusions de la recherche.

---

## Faisabilité (architect)

**Annexe conservée telle quelle (analyse de l'architect). Statut au 2026-11-06 : les révisions d'efforts, de découpage et de fusion sont intégrées au corps des stories et à l'ordre de livraison ci-dessus ; les décisions D1 à D11 tranchent les points marqués « TRANCHÉ » ci-dessous, et les stories font foi en cas d'écart.** Les efforts de la colonne « PM » et les stories A-15, A-07a, A-07b, A-09, A-10 citées ci-dessous n'existent plus telles quelles.

Revue rapide du 2026-11-06, sur `main` à `7cd8ce7`. Pas de plan d'implémentation détaillé ni d'ADR à ce stade. **Verdict : faisable**, à quatre conditions : (1) trancher la contradiction « pas de nouvelle colonne » (R1) **[TRANCHÉ, D1 : option A]** ; (2) reformuler le critère de succès 5 (R2) **[TRANCHÉ, D2]** ; (3) donner au routeur un moyen de savoir qu'une vue veut faire clignoter une ligne hors registre (R3) **[intégré à A-03, critère 6]** ; (4) placer le mémorisateur dans `internal/jobs.Registry` plutôt qu'à côté (R5) **[intégré à A-04, critère 1]**.

### Constats du code (base des estimations)

- **La chaîne D5 existe déjà et tourne à 10 Hz** (`jobSpinnerInterval = spinner.Dot.FPS`, `internal/app/jobs.go`). Elle sert deux lecteurs : la frame nue mise dans les cellules (`ws` Scanned / Git Status, `:sec` Scanned, OCI Scanned et ID, arbre de l'explorer, `:jobs`) et la frame stylée des footers (`RenderedFrame` : `ws`, clone, templates, filebrowser). Chaque tick diffuse un `jobs.ChangedMsg` complet (copie du registre) à toutes les vues tenues, et les vues qui portent la frame dans leurs lignes les reconstruisent à chaque tick.
- **`datatable` a un second mécanisme de spinner par ligne**, `busy` / `StatusColumn` / `AdvanceSpinner`, pour les actions instantanées hors registre : containers (stop, kill, remove), OCI (4 tables : remove, login, logout), netdiag ports (kill). Il est animé par la chaîne locale de chaque vue (le `spinner.TickMsg` de containers et d'OCI, `portsSpinnerCmd` dans ports). C'est une seconde famille de chaînes de ligne, hors D5.
- **Au moins 13 chaînes `spinner.TickMsg` locales** servent le chargement global au footer (containers, netdiag, interfaces, OCI, browser OCI, security, explorer, auth, agent-usage, status, setup, viewer, plus `components`). Le brief les garde, à raison.
- **Première colonne actuelle** (base de R1) :

| Vue / table | 1re colonne | Activité par ligne aujourd'hui | Source |
|---|---|---|---|
| `:jobs` (runs, cibles) | glyphe d'état, `IconColumnWidth` | frame D5 dans le glyphe | registre |
| `ws` | icône fichier, dossier ou dépôt | frame D5 dans Scanned et Git Status | registre (scan, sync, delete) |
| `:sec` inventaire | icône dépôt ou image | frame D5 dans Scanned ; `scanTarget.Failed` | registre (scan) + copie locale |
| explorer (arbre) | icône | frame D5 dans la cellule d'icône | registre (create, delete) + chargement local |
| templates | icône | aucune (footer et touches grisées) | registre (scan, sync) |
| containers | glyphe d'état docker | `busy` + chaîne de la vue | calculé + `busy` |
| filebrowser (hors liste) | icône | aucune (delete au footer) | registre (delete) |
| OCI images | **ID (14 cellules), pas d'icône** | frame D5 dans Scanned et ID (pull) ; `busy` (remove) ; `failedScans` | registre (scan, pull) + `busy` + copie |
| OCI networks, volumes, registries | **texte, pas d'icône** | `busy` + chaîne de la vue | `busy` |
| clone list | **Repository, pas d'icône** | colonne Status, 5 états | registre (clone, run ouverte) |
| netdiag ports | **Proto, pas d'icône** | `busy` (kill) + `portsSpinnerCmd` | `busy` |
| netdiag checks, interfaces | texte | **aucun spinner de ligne** (progression au footer) | sans objet |
| `:agent-usage` agents | **Agent, pas d'icône** | colonne State en texte | calculé |

- **Les échecs existent déjà dans le registre** : `Item.State = failed` et `Item.Detail` porte la cause. Mais les runs terminés sont élagués au-delà de `MaxFinishedRuns = 20`, donc on ne peut pas en tirer un mémorisateur fiable.
- **Les couleurs d'état de `:jobs` passent par les styles de statut sémantiques**, pas par `theme.IconStyle`. C'est une décision écrite dans `app-shell.md` (« five new roles would give a theme five ways to make `failed` not red »), qui contredit le critère 1 de A-01.
- **`app.gauge_glyph`** est le précédent exact du réglage « sans mouvement » : chaîne à valeurs fermées, défaut sûr, appliquée au démarrage, au changement de contexte et à chaque sauvegarde.

### 1. Efforts révisés

**[INTÉGRÉ]** sauf A-13 : l'architect proposait M sans le critère 7 (coloration du coût, story séparée S) ; l'utilisateur l'a gardé dans A-13 (D6), qui reste M.

| Story | PM | Architect | Justification (une ligne) |
|---|---|---|---|
| A-00 | S | S | Le tableau ci-dessus en couvre la moitié ; il reste les vues hors liste (status, dashboard, viewer, setup, auth) et la validation par l'utilisateur. |
| A-01 | M | **M** | Nouveau type de colonne dans `datatable` (`Indicator`, lu au rendu comme `spinnerFrame`), paquet de glyphes, alias de couleur, tests d'unicité et de contraste ; `StatusColumn` cohabite, son retrait vient à la fin. |
| A-15 | S | **fusion dans A-01** | La légende se dérive de la même table de glyphes ; deux PR obligeraient à éditer deux fois une seule source. |
| A-02 | S | S | Précédent `gauge_glyph` + champ cyclique dans `:cfg` + aide `?` ; dépend de A-03 pour son critère 3. |
| A-03 | M | **M (risque élevé)** | Touche le cœur du routeur (D5), ajoute la demande de clignotement des vues (R3) et sépare la phase du `ChangedMsg`. |
| A-04 | M | M | Une table dans `Registry`, une copie dans `ChangedMsg`, `ForgetTargetMsg`, identité canonique ; pas de spike séparé nécessaire (voir §3). |
| A-05 | M | M | Un constructeur de phrase dans `components`, composé avec `JobsStatusLine` sur la même ligne `Status`. |
| A-06 | M | **S** (+ une semaine d'usage) | `:jobs` a déjà la colonne et la frame sur la ligne ; on remplace le glyphe et on retire la frame. |
| A-16 | M | M | Test runtime (colonne 0 = `Indicator`) + test source (ni `StatusColumn`, ni `AdvanceSpinner`, ni frame dans un `Cell` des vues migrées) + liste d'exceptions qui ne fait que diminuer. |
| A-07a + A-07b | M + M | **fusion A-07 : M** | Avec A-04 et A-01 livrés, l'échec n'est qu'un état de plus lu dans le snapshot ; garder deux stories seulement si A-04 est en retard. |
| A-08 | M | M | Retrait de `Failed`, Scanned en texte statique, la pastille remplace l'icône existante. |
| A-09 | M | **L : à redécouper** | Quatre tables, une colonne à ajouter (R1), lignes fantômes de pull, `failedScans`, `busy` et la chaîne locale, tests de largeur à recaler. |
| ↳ A-09a | — | M | Images : scan, pull, remove, `failedScans`, Scanned et ID sans frame. |
| ↳ A-09b | — | S | Networks, volumes, registries : `busy` (remove, login) vers la pastille, retrait de l'`AdvanceSpinner` d'OCI. |
| A-10 | L | **L : à redécouper** | Deux écrans aux sources différentes (registre + chargement local d'un groupe ; run ouverte). |
| ↳ A-10a | — | M | Arbre : create, delete et chargement des enfants d'un groupe (état calculé, R3) ; l'icône existe déjà. |
| ↳ A-10b | — | M | Clone list : colonne à ajouter (R1), 5 états à projeter sans perte, progression globale gardée. |
| A-11 | S | S | Icône existante, deux kinds du registre ; pas de spinner de ligne à retirer aujourd'hui. |
| A-12 | M | **S** | Seule la table des ports a un spinner de ligne (kill) ; checks et interfaces sont « sans objet » (aucun spinner de ligne). Une colonne à ajouter (R1). |
| A-13 | M | **M sans le critère 7** | Colonne à ajouter (R1), états calculés avec demande de clignotement (R3), recalage des seuils de largeur (Breakdown, Est. cost). Sortir le critère 7 (coloration du coût) en story séparée (S) : ce n'est pas un indicateur d'activité. |
| A-14 | M | M | Glyphe d'état et `busy` déjà dans la colonne 0 ; il reste le passage par la pastille, la décision `exited` 0 et le retrait de l'`AdvanceSpinner`. |
| A-17 | S | **S, réduit** | `.claude/CLAUDE.md` impose de mettre à jour `docs/architecture/*.md` **dans le même commit que le code** : `app-shell.md` suit A-03 et A-04, `configuration.md` suit A-02. A-17 garde les règles `.claude/rules/`, `docs-site` et la carte de `.claude/CLAUDE.md`. |

### 2. Risques techniques (classés)

Statut : R1 **TRANCHÉ** (D1, option A) ; R2 **TRANCHÉ** (D2) ; R3, R4, R5 intégrés aux critères de A-01, A-03 et A-04 ; R6 à R10 repris dans les critères (tests de clé commune, de largeur, de phase).

| # | Gravité | Risque | Ce qui est faisable / atténuation |
|---|---|---|---|
| R1 | **Bloquant** | **« Pas de nouvelle colonne » est impossible à tenir** pour OCI (4 tables), la clone list, les ports et `:agent-usage` : aucune n'a de colonne d'icône (tableau ci-dessus). Les mettre en première colonne coûte 4 cellules rendues (2 + rembourrage). À 60 colonnes, des colonnes `Optional` tombent plus tôt, et les seuils documentés (Breakdown « visible dès 106 colonnes ») bougent. | Option A (recommandée) : accepter une colonne de pastille pour ces 7 tables, écrite comme exception au non-objectif. Option B : mettre la pastille dans une cellule existante (2 premières cellules de l'ID d'OCI, colonne State des ports), ce qui casse la forme de la Rule 125 et que le garde-fou A-16 ne peut plus vérifier. Option C : ne migrer que les vues qui ont une colonne d'icône. **Décision de l'utilisateur.** |
| R2 | **Bloquant (critère)** | Le critère de succès 5, « un seul tick d'animation dans l'application », est faux tel qu'écrit : les spinners de chargement du footer gardent leurs 13 chaînes locales, et la chaîne D5 nourrit aussi le spinner du footer des jobs à 10 Hz. | Faisable : **un seul tick anime les lignes**. On fusionne le clignotement dans la chaîne D5 du routeur (voir §3, Q1) ; les chaînes de chargement du footer restent hors périmètre. Le test garde-fou couvre : (a) aucun `tea.Tick` produisant un message d'animation de ligne hors de `internal/app` ; (b) aucun appel à `AdvanceSpinner`, aucun `StatusColumn:` et aucun champ `SpinnerFrame` ou `frame` lu par un `Cell` dans une vue migrée ; (c) `portsSpinnerCmd` supprimé ; (d) avec « sans mouvement », `ensureTick` renvoie `nil` quand seules des pastilles sont en cours. Reformuler le critère 5 dans le brief (PM). |
| R3 | Élevée | **Le routeur ne sait pas qu'une vue veut faire clignoter une ligne hors registre** : `busy` des actions instantanées, agent `running`, chargement des enfants d'un groupe, éventuellement conteneur `restarting`. Aujourd'hui le tick ne vit que si `jobs.Running() > 0`. Un signal par front (une vue envoie « je commence » / « j'ai fini ») reproduit le défaut que D5 a corrigé : une chaîne morte que rien ne relance. | Signal par niveau : interface optionnelle `BlinkingView { WantsBlink() bool }`, sur le modèle de `FormView` et `LeavingView`. Le routeur l'interroge sur les vues tenues **à la fin de chaque `Update`**, puis appelle `ensureTick`. Coût : au plus 16 assertions de type par message. Cela demande d'envelopper `App.Update` (un `update` interne plus un appel final), ce qui change sa structure : à faire dans A-03 et à tester avec `go test -race`. |
| R4 | Élevée | **Composant à deux sources dans `datatable`** : étendre ou remplacer `StatusColumn` et `busy` ? Remplacer d'un coup casse containers, OCI et ports avant leur migration. | **Étendre** : nouveau champ `Column.Indicator func(T) indicator.State`, rendu par `rowLine` avec la phase lue au rendu (`SetBlinkPhase(on bool)`, sans reconstruire ni remesurer : largeur fixe). La précédence `busy` (action instantanée locale) > état fourni par la vue est écrite **une fois** dans `datatable`. L'état fourni par la vue est lui-même construit par **un** helper partagé, `indicator.Resolve(running, queued bool, failure *jobs.Failure, observed indicator.State)`. `StatusColumn`, `AdvanceSpinner` et `spinnerFrame` restent jusqu'à la dernière migration, puis sont supprimés (A-16, critère 7). |
| R5 | Moyenne | **Mémorisateur central** : s'il est tenu à côté du registre, il faut synchroniser deux structures ; s'il est tiré des runs, il perd ses échecs à l'élagage (`MaxFinishedRuns`). | Le mettre **dans `jobs.Registry`**, mis à jour dans `Start` (les cibles admises effacent leur clé) et dans `Apply` (`failed` pose la clé avec `Detail` ; `done` l'efface). On n'y écrit que depuis l'`Update` du routeur, comme pour le reste du registre (Rule 110), et la copie voyage dans `ChangedMsg.Failures`. Ajouter `jobs.ForgetTargetMsg{Target}`, envoyé par une vue à la suppression d'une cible hors registre (remove d'une image), et effacer la clé au `delete` `done` du registre. Hypothèse : plafond de 500 clés, les plus anciennes sortent d'abord (en mémoire, sans disque). |
| R6 | Moyenne | **Identité de cible entre vues** (H4). | Le code la donne déjà : `Item.Target` est la clé du cache de scan. Un dépôt a son chemin absolu en `ws` et dans `:sec` ; une image a sa référence brute (`img.Name()`, `repo:tag`) dans OCI et dans `:sec`. La clé reste celle du registre, (`Kind`, `Target`, `Run.Context`), avec `filepath.Clean` sur les chemins au lancement. Risque résiduel : les images sans tag (`<none>`) et les références écrites autrement (`nginx` contre `docker.io/library/nginx`). À couvrir par un test de clé commune dans A-04 ; pas de spike séparé. |
| R7 | Moyenne | **Recalage des tests de largeur** : ajouter une colonne (R1) change les tests de la Rule 116 et les seuils écrits dans `agent-usage.md`, `network.md`, etc. | Compté dans les efforts de A-09a, A-10b, A-12 et A-13. Le test à 60 colonnes de A-16 doit passer par `RenderedWidth()`, jamais par la somme des colonnes déclarées (Rule 116). |
| R8 | Faible (gain) | **Coût de redessin** (H2). | Le coût **baisse** : aujourd'hui, quand un job tourne, chaque tick à 10 Hz copie le registre et reconstruit les lignes de toutes les vues tenues. Une fois la frame sortie des `Cell`, une phase qui change ne reconstruit rien (lecture au rendu) ; il suffit d'envoyer la phase à la vue à l'écran, plus la phase courante à l'entrée (`switchView`). Le renderer de Bubble Tea ne réécrit que les lignes modifiées, donc une ligne par pastille clignotante sur SSH. A-03 doit séparer « la frame ou la phase a bougé » (`jobs.FrameMsg`, sans snapshot) de « le registre a changé » (`ChangedMsg`). |
| R9 | Faible | **Garde-fous existants**. | `TestAnIconColumnIsUntitledAndTwoCellsWide` passe tel quel si la colonne de pastille est construite par un constructeur de `datatable` (littéral interne avec `IconColumnWidth` nu, déjà accepté) ; il ne voit pas les appels, d'où le test runtime de A-16. `TestNoTableViewRendersALoadingBody` n'est pas touché (il vise les corps, pas les lignes) ; ne pas y greffer A-16. `TestNoShortcutAdvertisesAGlyphAsAKey` reste valable. `TestEveryJobReporterIsRoutedToTheRegistry` doit passer à chaque PR de migration. |
| R10 | Faible | **Phase testable**. | La phase se calcule à partir de l'heure portée par le message de tick (`on = (t.UnixMilli() / 500) % 2 == 0`), pas d'un compteur. Elle survit donc à un changement de cadence et un test l'injecte. L'intervalle reste une `var` (précédent `jobSpinnerInterval`, `testutil.FastTimers`). Tests : rendu `SetBlinkPhase(true/false)` sur une ligne ; arrêt de la chaîne quand le registre est vide et qu'aucune vue ne demande de clignotement ; pas de chaîne avec « sans mouvement » ; numéro de séquence (chaîne remplacée ignorée). |

**Points irréversibles** : aucun en code (mémoire de session, extension de `datatable`). Le seul engagement durable est **le nom de la clé YAML** du réglage : la renommer plus tard demande une migration, comme `gitlab:` vers `forge:`. Un no-go sur le clignotement (A-06) ne perd aucune fondation : le réglage passe à `static` par défaut, et la pastille, le mémorisateur et le footer restent.

### 3. Réponses techniques aux questions ouvertes

Statut : Q2 **TRANCHÉ** (D3 : par contexte seulement, sans variable d'environnement ni touche à chaud) ; Q8 **TRANCHÉ** (D4) ; Q1, Q3, Q4, Q5, Q6 intégrés aux stories (recommandations retenues, voir le tableau en tête) ; Q7 **TRANCHÉ** par D5 pour la règle (une vue n'affiche que les échecs de ce qu'elle peut relancer), la liste des types mémorisés reste la question ouverte 2 ; Q9 reste la question ouverte 4.

- **Q1. Tick et D5** (question 6). Une seule chaîne de routeur, la D5 renommée « animation » : pas de seconde chaîne lente à côté. Elle vit tant que `jobs.Running() > 0` **ou** qu'une vue tenue répond `WantsBlink()` (R3), et jamais pour les pastilles avec « sans mouvement ». Cadence : 100 ms tant qu'un spinner de footer en dépend (registre actif), 500 ms sinon. Phase : demi-période de **500 ms**, soit un clignotement par seconde, sous le seuil de 3 flashs par seconde. Choisir 1 Hz ou 2 Hz exactement reste une question d'UX ; le code ne change que d'une constante. Le spinner du footer des jobs (10 Hz) reste. Le ralentir à son tour relève du produit, non requis.
- **Q2. Réglage « sans mouvement »** (question 5). Clé proposée : **`app.motion: blink | static`**, défaut `blink`, à côté de `gauge_glyph` dans la section `App` (`internal/config`). Mécanisme copié sur `app.gauge_glyph` (`configuration.md`) : lu au démarrage, au changement de contexte et sur `ConfigSavedMsg`, sans redémarrage (critère 4 de A-02) ; champ cyclique `←→` dans `:cfg` (Rule 132). Toute valeur autre que `static` vaut `blink`, avec un log `WARN [config] app.motion …` (critère 5 de A-02). Le routeur garde la valeur, puisque c'est lui qui possède le tick. **Portée** : la section `app` est par contexte, comme `gauge_glyph`. Une préférence d'accessibilité par contexte oblige à la répéter dans chaque contexte. Une variable d'environnement globale (`DEVDESK_MOTION=static`) coûte S et couvre ce cas ; un vrai fichier global coûterait M (seul précédent : `trust.yaml`). Portée, variable d'environnement et touche à chaud : **décision produit**. Je déconseille la touche à chaud : budget de touches serré (Rule 111).
- **Q3. Priorité entre plusieurs opérations sur une ligne** (question 4). Règle simple, écrite une fois dans `indicator.Resolve` : **en cours > en file > échec mémorisé > état observé par la vue > repos**. `busy` (action instantanée) compte comme « en cours » et passe avant le registre dans le texte du footer. Raison : « en cours » est le fait le plus récent et il est transitoire ; un échec d'un autre kind masqué par une opération en cours réapparaît quand elle se termine, puisque sa clé n'a pas changé. Le footer nomme l'état gagnant et, si un échec est masqué, l'ajoute en suffixe. La formulation relève de l'UX.
- **Q4. Cause d'erreur au footer** (question 8). Les deux, selon la Rule 128, qui distingue événement et état : l'**événement** (l'échec arrive) reste un `Error` de 3 s, comme aujourd'hui ; la **cause persistante** de la ligne sélectionnée est un `Status`, sans minuteur, recalculé à chaque frame, comme une progression. La précédence error > warn > info > status ne change pas. Il n'y a qu'un texte `Status` : le constructeur de A-05 compose « détail de ligne · progression globale », dans une seule fonction de `components`, à côté de `JobsStatusLine`, en tronquant d'abord le détail de ligne.
- **Q5. Colonne Scanned pendant un scan** (question 9). Recommandation technique : le texte statique **`scanning`** en `DimStyle`, sans frame, identique dans `ws`, `:sec` et OCI (même règle pour `syncing` et `deleting` dans Git Status, `pulling` dans l'ID d'OCI). Il tient dans les 14 cellules, ne trompe pas comme un ancien « 3 days ago » et ne force aucune reconstruction par tick (R8). Le mot exact relève de l'UX.
- **Q6. Clignotement sous le curseur** (question 10). Il continue. Sous la sélection normale, §3.72 (`preserveColumnColors`) garde la couleur propre de chaque cellule, repeinte sur `ColorTableLineSelected` : la couleur de la pastille reste visible. Le test de contraste de A-01 doit donc mesurer chaque couleur de pastille **aussi** sur `ColorTableLineSelected`. Sous une sélection colorée en entier (`SelectedStyles` : erreur de containers, sévérité des findings), la couleur tombe, mais le clignotement est porté par le **texte** de la cellule (alternance de glyphes) et survit. C'est le raisonnement déjà écrit dans `rowLine` pour `busy`.
- **Q7. Identité de cible** (question 7). Voir R6 : (`Kind`, `Target`, `Context`) tels que le registre les porte, avec `Target` = clé de cache. Une même image et un même dépôt ne sont **pas** la même cible : ce sont deux clés différentes, sans pont. **Quels kinds une vue affiche** relève du produit. Recommandation technique : une vue affiche les échecs des kinds **qu'elle peut relancer** (`ws` : scan, sync, delete ; `:sec` : scan ; OCI : scan, pull ; explorer : create, delete, clone ; templates : scan, sync). Sinon la pastille resterait rouge sur une ligne où « la prochaine tentative » est impossible, ce qui contredit la décision 2. Avec cette règle, un échec de sync dans `ws` **n'apparaît pas** dans `:sec`.
- **Q8. Couleurs** (question 3, partie technique). Recommandation : des **alias sémantiques fermés**, assignés dans `ApplyTheme` comme les couleurs du footer, et **non** des rôles `icon_*` surchargeables. C'est la décision déjà écrite pour `:jobs` dans `app-shell.md`. L'icône de repos garde son `IconRole` actuel. Cela demande de corriger le critère 1 de A-01. Les glyphes et le glyphe « en file » relèvent de l'UX. Hypothèse prudente : « en file » ne clignote pas (glyphe statique distinct), ce qui réduit le mouvement ; à confirmer par l'UX.
- **Q9. Échec d'un test réseau** (question 13) : je suis d'accord avec le PM, c'est un résultat de ligne (colonne Verdict). La table des checks n'a d'ailleurs aucune opération par ligne : elle est « sans objet » pour la pastille.
- **Relèvent du produit ou de l'UX, sans arbitrage ici** : vue pilote et issue d'un no-go (questions 1 et 2), glyphes (question 3), `exited` 0 et `restarting` (question 11), coloration du coût et icône de type d'agent (question 12), vues hors inventaire (question 14), H5 (question 15), mention de DEVDESK-99 (question 17).

### 4. Dépendances, ordre et découpage en PR

```
Vague 1 (parallèles) : PR1 A-01 + A-15   PR2 A-04   PR3 A-05   (A-00 à valider en parallèle)
Vague 2              : PR4 A-03 (dépend de l'API SetBlinkPhase de A-01)  →  PR5 A-02
Pilote               : PR6 A-06 + squelette A-16 (liste d'exceptions)  →  GO / NO-GO
Vague 3 (après go, parallélisables entre elles, chacune retire son entrée de la liste A-16) :
  A-07 ws (fusion a+b)  ·  A-08 :sec  ·  A-09a OCI images  ·  A-09b OCI autres tables
  A-10a arbre  ·  A-10b clone list  ·  A-11 templates  ·  A-12 ports  ·  A-13 agent-usage  ·  A-14 containers
Clôture              : PR finale A-16 (liste vide supprimée ; suppression de StatusColumn, AdvanceSpinner, spinnerFrame) + A-17 réduit
```

- **Une PR par story**, comme le recommande le PM, à une exception près : A-15 entre dans A-01. A-08 et A-09a se relisent mieux ensemble (même colonne Scanned, même mémorisateur), mais restent deux PR.
- Avant le go, A-07, A-08 et A-09a ne dépendent plus que de A-04 et du pilote. A-04 étant indépendant, il doit être **livré avant le pilote** pour que la vague 3 ne l'attende pas.
- **Go / no-go du pilote** : `:jobs` valide le clignotement (H1), la mesure H2 et le réglage, mais **ni H5** (sa première colonne est déjà un glyphe d'état, aucune icône n'y est remplacée), **ni le signal par niveau R3** (tout y vient du registre), **ni le mémorisateur**. Je propose de garder `:jobs` pour la décision sur le clignotement et de mettre un **second point de contrôle, sur H5 et R3, à la livraison de A-07 (`ws`)**, avant d'ouvrir les autres migrations. Critères à consigner au go : réponse go / go sans mouvement par défaut / no-go ; mesure de redessin (octets émis par seconde avec 1 et 20 lignes clignotantes, `TEA_TRACE` ou compteur du renderer) ; absence de régression `mise run test-race` sur `internal/app`.

### 5. État de S-09 (DEVDESK-97)

Statut : **TRANCHÉ** (D6) : le critère de coloration du coût reste dans A-13, sans dépendance de code à S-09.

- **Les seuils existent déjà dans le code** : `theme.LoadWarnPercent = 75` et `theme.LoadCriticalPercent = 90` (`internal/ui/theme/gauge.go`), avec `theme.LoadTextStyle(pct)`. Ils viennent des jauges de charge de containers (§3.71) et sont testés (`gauge_test.go`, `containers/update_test.go`).
- **S-09 n'est pas livrée** : la liste des sessions de `:agent-usage` n'a que Project, Last active et Session ; il n'y a ni colonne Billed, ni jauge de coût, ni tri par coût (`internal/ui/agentusageview/columns.go`).
- Conséquence : le critère 7 de A-13 **n'a aucune dépendance de code envers S-09**. Les constantes sont disponibles et le « Bloqué par : S-09 » peut être retiré. Il reste deux questions de produit : le critère appartient-il à ce chantier (voir A-13, recommandation : story séparée), et quelle référence prendre pour le seuil.

### Décisions demandées à l'utilisateur (issues de cette revue)

**Toutes tranchées le 2026-11-06** : 1 = option A (D1) ; 2 = reformulation du critère 5, footers hors périmètre (D2) ; 3 = `app.motion` par contexte seul, **sans** variable d'environnement (D3, écart avec la recommandation de l'architect) ; 4 = alias fermés (D4) ; 5 = échecs des seules opérations relançables depuis la vue (D5) ; 6 = critère 7 gardé dans A-13 (D6, écart avec la recommandation de l'architect).

1. **R1, la colonne de pastille dans les 7 tables qui n'ont pas de colonne d'icône** : A, une colonne acceptée en exception (recommandé) ; B, pastille dans une cellule existante ; C, ne pas migrer ces tables.
2. **R2, reformulation du critère de succès 5** en « un seul tick anime les lignes », les spinners de chargement du footer restant hors périmètre (recommandé), ou extension de la règle aux footers (hors brief, coût L).
3. **Portée du réglage** : `app.motion` par contexte seul, ou par contexte avec une variable d'environnement globale (recommandé, S).
4. **Couleurs d'état** : alias fermés (recommandé, conforme à `app-shell.md`) ou rôles `icon_*` surchargeables par thème (critère 1 actuel de A-01).
5. **Kinds affichés par vue** : échecs des seules opérations relançables depuis la vue (recommandé), ou tous les kinds pour une même cible.
6. **A-13, critère 7** : sorti en story séparée (recommandé) ou gardé, sans dépendance à S-09 dans les deux cas.
