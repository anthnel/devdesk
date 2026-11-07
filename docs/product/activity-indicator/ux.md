# Indicateur d'activité uniforme : conception UX

Date : 2026-11-06, mis à jour le 2026-11-06 avec les décisions de l'utilisateur (`stories.md`, tableau « Décisions tranchées », D7 à D10). Entrées : `brief.md`, `stories.md` (dont « Faisabilité (architect) »), `research.md`, Rules 111/118/121/122/125/128/139, `internal/ui/theme/` (`icons.go`, `iconcolors.go`, `colors.go`, `manager.go`).
Tout le texte d'interface cité (footer, aide) est en anglais US (Rule 129) ; le reste du document est en français.

**Questions bloquantes : aucune.** Les choix de glyphes, de phase éteinte, de fréquence, de `restarting`, d'`exited` code 0 et de conteneur `running` sont **tranchés** (§6). Deux points restent à confirmer (§6). Les hypothèses sont marquées `Hypothèse :`.

## 0. Principes retenus

1. **Forme de base ● (imposée).** Tous les états sont des cercles ou des glyphes ronds de même emprise (`●` `○` `◴` `◐` `◕` `◉` `◌` `⊗` `⊘` `⊖`, blocs Unicode « Geometric Shapes » et « Mathematical Operators », pas des Nerd Fonts). Conséquence utile : la pastille ne dépend pas d'un Nerd Font, ce qui allège le backlog §3.98 pour cette colonne (les icônes de repos, elles, en dépendent encore).
2. **Un état = un glyphe unique dans toute l'application, plus une couleur.** L'unicité est globale (test de A-16 : pas deux sens pour un même point de code). La lisibilité, elle, se juge entre états qui apparaissent *dans la même vue* (matrice §2.4).
3. **La couleur est redondante partout.** Jamais deux états qui ne diffèrent que par la teinte.
4. **Rouge jamais clignotant.** Seule la pastille « en cours » bouge (WCAG 2.3.1 : le seuil « flash rouge » ne peut donc pas s'appliquer).
5. **Cellule = texte brut d'un seul glyphe** (Rule 122) ; `datatable` complète à `IconColumnWidth` (2). Le `Cell` ne met pas d'espace final lui-même : `datatable` mesure avec `runewidth`, et ces glyphes sont « ambigus » en largeur Est-asiatique (2 cellules dans un terminal CJK) ; un glyphe + espace y mesurerait 3 et serait tronqué. Voir §5.

## 1. Tableau des états

### 1.1 Alphabet global

Couleurs : alias **fermés**, assignés dans `ApplyTheme` (comme `ColorFooter*`), pointant sur des couleurs existantes de `colors.go`. Noms proposés pour l'API ; les valeurs sont des noms existants.

| Alias proposé | Pointe sur (`colors.go`) | Valeur par défaut (Catppuccin) |
|---|---|---|
| `ColorActivityRunning` | `ColorSecondary` | `#b4befe` (lavande) |
| `ColorActivityNeutral` | `ColorText` | `#cdd6f4` |
| `ColorActivitySuspended` | `ColorFooterWarn` (= `ColorSeverityMedium`) | `#fab387` (orange) |
| `ColorActivityFailed` | `ColorFooterError` (= `ColorSeverityCritical`) | `#f38ba8` (rouge) |

Pourquoi lavande pour « en cours » : ni vert (exclu), ni jaune (`ColorHighlight` = focus de formulaire, Rule 120), ni rouge/orange ; c'est déjà la couleur « actif » (onglet, en-tête), donc le sens est cohérent. `ColorDim` (`#585b70`) n'est **pas** utilisé : voir §2, il tombe à environ 2,5:1 sur le fond et 1,9:1 sous la sélection normale.

| Glyphe | Sens | Alias | Clignote |
|---|---|---|---|
| `●` (allumé) / `○` (éteint) | opération en cours | `ColorActivityRunning` | oui, 1 Hz |
| `◴` | en file | `ColorActivityNeutral` | non |
| `⊗` | échec (opération échouée, conteneur `dead` ou `exited` non nul) | `ColorActivityFailed` | non, jamais |
| `◐` | suspendu : agent `idle`, conteneur `paused` | `ColorActivitySuspended` | non |
| `◕` | conteneur `restarting` | `ColorActivitySuspended` | non (fixe, tranché) |
| `◉` | terminé, **et rien d'autre** : job `done`, agent `done` | `ColorActivityNeutral` | non |
| `⊘` | annulé (job `cancelled`) | `ColorActivityNeutral` | non |
| `⊖` | sans action : job `skipped`, conteneur `exited` code 0 | `ColorActivityNeutral` | non |
| `◌` | inconnu / pas encore démarré : agent `unknown`, conteneur `created` | `ColorActivityNeutral` | non |
| repos | icône habituelle de la ligne (rôle `IconRole*` existant) ou cellule vide. **Un conteneur `running` est au repos** : état nominal (Rule 122), aucune pastille | rôle existant | non |
| mode « sans mouvement » | `●` fixe (la phase allumée) | `ColorActivityRunning` | non |

`●` et `○` sont réservés à « en cours » : aucun autre état ne les emploie, donc une capture figée sur l'une ou l'autre phase reste sans ambiguïté. `◉` est réservé à « terminé » (job `done`, agent `done`). Glyphes, phase éteinte `○` et mode `●` fixe : tranchés (D7).

### 1.2 Par vue : glyphe, texte équivalent au footer

Le footer porte le glyphe d'état en préfixe, en texte brut (Rule 128 interdit la couleur propre ; le préfixe relie le texte à la pastille sans passer par la couleur). Phrases construites par un seul constructeur (A-05, critère 8). **Exception : un conteneur `running` n'a pas de pastille, son footer n'a donc pas de glyphe en préfixe** (D8). Libellés validés par l'utilisateur (D10).

| État | Glyphe (allumé / éteint) | Couleur | Clignote | Footer (ligne sélectionnée, niveau `Status`) |
|---|---|---|---|---|
| Opération en cours | `●` / `○` | Running | oui | `● Scanning ~/w/app/api` ; `● Syncing ~/w/app/web` ; `● Removing nginx:1.27` |
| En file | `◴` / `◴` | Neutral | non | `◴ Queued: scan ~/w/app/api` |
| Échec | `⊗` / `⊗` | Failed | non | `⊗ ~/w/app/api: last scan failed — trivy could not pull the database` |
| `:jobs` done | `◉` | Neutral | non | `◉ Scan of ~/w/app/api finished 2 min ago` |
| `:jobs` failed | `⊗` | Failed | non | comme « Échec » |
| `:jobs` cancelled | `⊘` | Neutral | non | `⊘ Scan of ~/w/app/api was cancelled` |
| `:jobs` skipped | `⊖` | Neutral | non | `⊖ Scan of ~/w/app/api skipped — result already cached` |
| `:jobs` en cours / en file | `●`/`○` ; `◴` | Running ; Neutral | oui ; non | comme ci-dessus |
| `:agent-usage` running | `●` / `○` | Running | oui | `● Agent running — last transcript line 4 s ago` |
| `:agent-usage` idle | `◐` | Suspended | non | `◐ Agent idle — no transcript line for 7 min` |
| `:agent-usage` done | `◉` | Neutral | non | `◉ Agent done — ran 12 min` |
| `:agent-usage` unknown | `◌` | Neutral | non | `◌ Agent state unknown — transcript has no end marker` |
| Conteneur running | (aucune pastille : icône habituelle) | — | non | `Running since 3 hr ago` (sans glyphe en préfixe) |
| Conteneur restarting | `◕` | Suspended | non | `◕ Restarting — 4 restarts, last exit code 1` |
| Conteneur paused | `◐` | Suspended | non | `◐ Paused` |
| Conteneur created | `◌` | Neutral | non | `◌ Created, never started` |
| Conteneur exited (code ≠ 0) | `⊗` | Failed | non | `⊗ Exited with code 137 — 5 min ago` |
| Conteneur exited (code 0) | `⊖` | Neutral | non | `⊖ Exited cleanly (code 0) — 5 min ago` |
| Conteneur dead | `⊗` | Failed | non | `⊗ Dead — the engine could not stop it cleanly` |
| Action instantanée sur conteneur (stop, kill, remove) | `●` / `○` | Running | oui | `● Stopping web-1` |

Hypothèse : les libellés exacts (« last transcript line 4 s ago ») dépendent des données disponibles dans chaque vue ; seule la structure `glyphe + phrase` est prescrite.

Priorité sur une ligne, déjà décidée : **en cours > en file > échec > état observé > repos** (une seule fonction, `indicator.Resolve`). Quand un échec est masqué par une opération en cours, le footer l'ajoute en suffixe : `● Scanning ~/w/app/api · earlier failure: sync (see :jobs)`.

### 1.3 Choix tranchés (décision 7, 2026-11-06)

Les options étudiées avant la décision sont abandonnées ; il ne reste que le choix et sa raison.

- **Phase éteinte du clignotement : `○`, même couleur lavande.** Une capture figée se lit « quelque chose est là, en mouvement », jamais « rien » ; la couleur ne change pas (pas de scintillement de teinte) ; le contraste reste celui de l'état allumé (environ 7:1 sur la sélection). Écartés : `●` atténué en `ColorDim` (proche d'une cellule vide, échoue 3:1 : environ 2,5:1 sur le fond, 1,9:1 sous le curseur), `●` en `ColorText` (confondable avec un état terminé).
- **Conteneur `restarting` : `◕` orange fixe.** C'est un état observé, pas une opération lancée par DevDesk ; pas de dépendance au signal `WantsBlink` pour ce cas ; un `restarting` en boucle de crash change plus vite que le sondage docker, donc un clignotement synchronisé sur le tick serait trompeur ; `○` reste réservé à « en cours ».
- **« En file » : `◴` statique, `ColorActivityNeutral`.** Une file n'est pas une activité ; faire clignoter ce qui attend fait croire que ça avance, et un lot de 12 scans compterait 11 clignotants de plus sans information.
- **Conteneur `running` : aucune pastille** (D8). L'état nominal ne se colore pas (Rule 122, « color discipline ») ; la ligne garde son icône habituelle, et la pastille ne signale que ce qui s'écarte du nominal.

## 2. Test de lisibilité

Hypothèse : les rapports de contraste ci-dessous sont des calculs à la main (luminance relative WCAG) sur les valeurs par défaut Catppuccin (fond `#1e1e2e`, sélection `#313244`) ; `theme/contrast_test.go` doit les confirmer (A-01, critère 6).

### 2.1 Contraste des couleurs de pastille (critère WCAG 1.4.11, seuil 3:1)

| Couleur | Sur fond `#1e1e2e` | Sur sélection `#313244` |
|---|---|---|
| Running `#b4befe` | environ 9:1 | environ 7:1 |
| Neutral `#cdd6f4` | environ 11:1 | environ 8:1 |
| Suspended `#fab387` | environ 9:1 | environ 7:1 |
| Failed `#f38ba8` | environ 7:1 | environ 5:1 |
| (écarté) Dim `#585b70` | environ 2,5:1 : échoue | environ 1,9:1 : échoue |

Écart **corrigé** dans `stories.md` (A-13, critère 2 ; D7) : la version précédente prévoyait `unknown` en `DimStyle`. Cela échoue, et la sélection normale (§3.72) conserve la couleur de la cellule : un `◌` dim devient presque invisible sous le curseur. Correctif retenu et tranché : `unknown` et `created` en `ColorActivityNeutral` ; c'est le glyphe `◌` (anneau en pointillé) qui dit « vide ». Le test de contraste de A-01 vérifie chaque alias sur les deux fonds.

### 2.2 Les quatre épreuves

| # | Épreuve | Résultat | Où ça échoue, et le correctif |
|---|---|---|---|
| a | **Sans couleur** (`NO_COLOR`, monochrome) : dix glyphes tous distincts | passe, avec une paire faible | `●` contre `◉` : un point plein contre un anneau à point, proches à petite taille dans certaines polices. Ils se croisent dans `:jobs` et `:agent-usage` (en cours contre terminé), pas dans containers (un conteneur `running` n'a pas de pastille, D8). Correctifs : (1) le clignotement les sépare dès qu'on regarde deux secondes ; (2) le footer écrit l'état. L'utilisateur les distingue dans son terminal : **pas de repli `✓`** (D7). |
| b | **Sans mouvement** (capture figée, ou réglage `static`) | passe | La phase éteinte `○` est un anneau lavande, réservé : un cliché pris à l'instant « éteint » se lit « en cours ». En mode `static`, `●` fixe : le seul état qui bouge perd son mouvement mais garde glyphe et couleur, tous deux uniques parmi les états de la même vue. |
| c | **Daltonisme rouge/vert** (deutéranopie, protanopie) | passe | Le thème n'emploie aucun vert. Rouge `#f38ba8` et orange `#fab387` convergent vers des jaunes-gris proches : ils se distinguent par le glyphe (`⊗` contre `◐`/`◕`), c'est la raison d'être de la règle. Lavande et texte convergent aussi vers des gris-bleu : `●`/`○` contre `◉` les sépare. |
| d | **Sous le curseur, sélection normale** | passe | La Rule 118 révisée (§3.72) garde la couleur de chaque cellule, repeinte sur `ColorTableLineSelected` ; le gras est ajouté. Toutes les couleurs de pastille dépassent 3:1 dessus (§2.1). Le clignotement continue : la phase est lue au rendu. |
| d' | **Sous une ligne colorée en entier** (erreur de containers, `busy`, sévérité) | passe, par le glyphe | La couleur de la pastille tombe (fond plein `ColorError`, texte `ColorBlack`). Le glyphe reste : `⊗` noir sur rouge, et pour l'action en cours `●`/`○` qui alterne dans le texte de la cellule (raisonnement de `rowLine` pour `busy`, Q6 de l'architect). |

### 2.3 Cas où l'état est ambigu au seul coup d'œil, et réponse

- Repos (icône de ligne) contre `◌` (vide, créé, inconnu) : l'icône de repos est un glyphe Nerd Font de type de ligne, `◌` est un anneau en pointillé : distincts. Si la vue n'a pas d'icône, le repos est une cellule vide (jamais `◌`).
- `⊗` rouge pour `exited` non nul et pour `dead` : même glyphe, sens « cassé ». Le statut texte du conteneur (colonne Status) et le footer distinguent les deux.

### 2.4 Matrice de coexistence (ce qui doit rester distinguable dans une même vue)

| Vue | États qui coexistent |
|---|---|
| `:jobs` | `●`/`○`, `◴`, `⊗`, `◉`, `⊘`, `⊖` |
| `ws`, `:sec`, OCI | `●`/`○`, `◴`, `⊗`, icône de repos |
| `:agent-usage` | `●`/`○`, `◐`, `◉`, `◌` |
| containers | icône de repos (conteneur `running`), `◕`, `◐`, `◌`, `⊗`, `⊖`, `●`/`○` (action instantanée) |

Chaque ligne a des glyphes deux à deux distincts ; la seule paire faible est `●` contre `◉` (a), qui ne se croise plus dans containers.

## 3. Maquettes

Conventions : seule la première colonne est spécifiée ; les autres colonnes sont illustratives. Largeurs indicatives (cadre de 60 et de 120 colonnes), à confirmer par `RenderedWidth()` (Rule 116) à l'implémentation. `←` à droite d'un cadre est une annotation, hors écran. La phase affichée est l'allumée ; une ligne est redessinée en phase éteinte quand c'est utile. La ligne sélectionnée est repeinte entièrement sur `ColorTableLineSelected`, gras.

### 3.1 `:jobs` (runs)

60 colonnes :

```
┌─ Jobs ───────────────────────────────────────────────────┐
│    Kind     Target                 State     Age         │
│ ●  scan     ~/w/app/api            running   now         │ ← selected
│ ◴  scan     ~/w/app/web            queued    now         │
│ ⊗  sync     ~/w/app/infra          failed    3 min ago   │
│ ◉  scan     ~/w/app/docs           done      9 min ago   │
│ ⊘  clone    acme/tools             cancelled 12 min ago  │
│ ⊖  scan     ~/w/app/lib            skipped   14 min ago  │
└──────────────────────────────────────────────────────────┘

        ● Scanning ~/w/app/api · 3/12 scans
```

Même ligne sélectionnée en phase éteinte : `│ ○  scan     ~/w/app/api            running   now         │`.

120 colonnes (colonnes ajoutées : Context, Duration) :

```
┌─ Jobs ───────────────────────────────────────────────────────────────────────────────────────────────────────────────┐
│    Kind     Target                         Context     State       Duration   Age                                    │
│ ●  scan     ~/workspaces/app/api           default     running     00:41      now                                    │ ← selected
│ ◴  scan     ~/workspaces/app/web           default     queued      -          now                                    │
│ ⊗  sync     ~/workspaces/app/infra         default     failed      00:07      3 min ago                              │
│ ◉  scan     ~/workspaces/app/docs          default     done        00:52      9 min ago                              │
│ ⊘  clone    acme/tools                     default     cancelled   00:03      12 min ago                             │
│ ⊖  scan     ~/workspaces/app/lib           default     skipped     -          14 min ago                             │
└──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┘

                                     ● Scanning ~/workspaces/app/api · 3/12 scans
```

Le texte de State reste dans la colonne et dans son `Search` (`/failed` marche toujours, A-06 critère 4).

### 3.2 `ws`

60 colonnes :

```
┌─ Workspaces ─────────────────────────────────────────────┐
│    Name                 Git Status      Scanned          │
│ ●  api                  main            scanning         │ ← selected
│ ◴  web                  main *2         3 days ago       │
│ ⊗  infra-tf             dev +1 -3       5 days ago       │
│ 󰉋  docs                 main            -                │
│    notes.md                             -                │
└──────────────────────────────────────────────────────────┘

        ● Syncing ~/w/app/api
```

Ici `󰉋` est l'icône de repos d'un dossier (`IconDirectory`, rôle `IconRoleDirectory`) ; le fichier `notes.md` a l'icône de fichier (non rendue dans ce texte brut). Les lignes au repos montrent leur icône ; les trois autres montrent la pastille. Le cadre ne bouge pas. La colonne Scanned dit `scanning` pendant l'opération (texte statique, décision de l'architect Q5), et l'ancien « 3 days ago » ne s'affiche pas.

120 colonnes (colonnes ajoutées : Branch, Remote) :

```
┌─ Workspaces ─────────────────────────────────────────────────────────────────────────────────────────────────────────┐
│    Name                    Branch        Git Status        Remote                         Scanned                    │
│ ●  api                     main          main              git@forge:acme/api.git         scanning                   │ ← selected
│ ◴  web                     main          main *2           git@forge:acme/web.git         3 days ago                 │
│ ⊗  infra-tf                dev           dev +1 -3         git@forge:acme/infra-tf.git    5 days ago                 │
│ 󰉋  docs                    main          main              -                              -                          │
└──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┘

                               ⊗ ~/w/app/infra-tf: last sync failed — remote rejected the fetch (see :jobs)
```

(dernière ligne : exemple de footer si `infra-tf` était sélectionnée.)

### 3.3 `:sec` (inventaire)

60 colonnes :

```
┌─ Security ───────────────────────────────────────────────┐
│    Target               Scanned       CI    C  H  M  L   │
│ ●  api                  scanning      -     -  -  -  -   │ ← selected
│ ⊗  web                  2 days ago    -     -  -  -  -   │
│ 󰋜  infra-tf             1 hr ago      B     0  1  4  2   │
│ 󰡨  nginx:1.27           5 min ago     A     0  0  0  3   │
└──────────────────────────────────────────────────────────┘

        ● Scanning ~/w/app/api · 3/12 scans
```

Les lignes `infra-tf` (dépôt) et `nginx:1.27` (image) sont au repos : icône `IconRoleRepository`, `IconRoleImage`. Les compteurs à `0` restent `DimStyle` (Rule 122).

120 colonnes : mêmes colonnes avec Path et Last result en plus ; la première colonne ne change pas de forme.

```
┌─ Security ───────────────────────────────────────────────────────────────────────────────────────────────────────────┐
│    Target                      Path / reference               Scanned        CI    Crit  High  Med  Low  Secrets    │
│ ●  api                         ~/workspaces/app/api           scanning       -     -     -     -    -    -          │ ← selected
│ ⊗  web                         ~/workspaces/app/web           2 days ago     -     -     -     -    -    -          │
│ 󰋜  infra-tf                    ~/workspaces/app/infra-tf      1 hr ago       B     0     1     4    2    0          │
│ 󰡨  nginx:1.27                  docker.io/library/nginx        5 min ago      A     0     0     0    3    0          │
└──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┘

                               ⊗ ~/w/app/web: last scan failed — trivy could not pull the database
```

### 3.4 `:agent-usage` (niveau agents)

60 colonnes. La colonne State en texte est abandonnée sous ce seuil (A-13, critère 3) : la pastille est alors la seule source d'état visible, et le footer la double.

```
┌─ Agent usage › app (Sessions › ~/w/app › 1a2b3c4d) ──────┐
│    Agent            Model        Total    Share          │
│ ●  orchestrator     opus-4       412k     61%            │ ← selected
│ ◐  reviewer         sonnet-4     120k     18%            │
│ ◉  coder            sonnet-4     98k      14%            │
│ ◌  tester           haiku-4      45k      7%             │
└──────────────────────────────────────────────────────────┘
 Agents  Breakdown

        ● Agent running — last transcript line 4 s ago
```

120 colonnes. La colonne State en texte est conservée, avec la pastille en plus :

```
┌─ Agent usage › app (Sessions › ~/w/app › 1a2b3c4d) ──────────────────────────────────────────────────────────────────┐
│    Agent            Model       State     Tools   Duration   Total    Share   Est. cost   Breakdown                  │
│ ●  orchestrator     opus-4      running   142     00:38:12   412k     61%     $4.20       ██████▒▒░░                 │ ← selected
│ ◐  reviewer         sonnet-4    idle      31      00:07:40   120k     18%     $0.80       ████▒░░░░░                 │
│ ◉  coder            sonnet-4    done      77      00:14:05   98k      14%     $0.62       ███▒░░░░░░                 │
│ ◌  tester           haiku-4     unknown   9       00:02:11   45k      7%      $0.10       ██░░░░░░░░                 │
└──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┘
 Agents  Breakdown

                               ● Agent running — last transcript line 4 s ago
```

Aucun tick d'animation n'est ajouté par la lecture incrémentale de 2 s (donnée, pas opération). Seul `running` clignote.

### 3.5 Containers

Un conteneur `running` n'a **aucune pastille** (D8) : sa première colonne montre l'icône habituelle de la ligne. Dans les maquettes, `i` désigne cette icône (Nerd Font, rôle existant, non rendue en texte brut).

60 colonnes :

```
┌─ Containers ─────────────────────────────────────────────┐
│    Name            Image          Status       CPU       │
│ i  web-1           nginx:1.27     Up 3 hr      ░░░░░░    │ ← selected
│ ●  db-1            postgres:16    Stopping...  ░░░░░░    │
│ ◕  worker-1        app:latest     Restarting   ░░░░░░    │
│ ◐  cache-1         redis:7        Paused       ░░░░░░    │
│ ⊗  job-7           app:latest     Exited (137) ░░░░░░    │
│ ⊖  seed-1          app:latest     Exited (0)   ░░░░░░    │
│ ◌  new-1           app:latest     Created      ░░░░░░    │
└──────────────────────────────────────────────────────────┘

        Running since 3 hr ago
```

120 colonnes (Ports, Mem, Update en plus) :

```
┌─ Containers ─────────────────────────────────────────────────────────────────────────────────────────────────────────┐
│    Name            Image             Status         Ports            CPU       Mem       Update   Sig               │
│ i  web-1           nginx:1.27        Up 3 hr        0.0.0.0:80->80   ░░░░░░    ░░░░░░    -        -                 │ ← selected
│ ●  db-1            postgres:16       Stopping...    5432             ░░░░░░    ░░░░░░    -        -                 │
│ ◕  worker-1        app:latest        Restarting     -                ░░░░░░    ░░░░░░    -        -                 │
│ ◐  cache-1         redis:7           Paused         6379             ░░░░░░    ░░░░░░    -        -                 │
│ ⊗  job-7           app:latest        Exited (137)   -                ░░░░░░    ░░░░░░    -        -                 │
│ ⊖  seed-1          app:latest        Exited (0)     -                ░░░░░░    ░░░░░░    -        -                 │
│ ◌  new-1           app:latest        Created        -                ░░░░░░    ░░░░░░    -        -                 │
└──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┘

                                           Running since 3 hr ago
```

Le footer d'un conteneur `running` reste « Running since 3 hr ago », sans glyphe en préfixe. Sur une ligne en erreur sélectionnée (`job-7`), la ligne entière est `ColorBlack` sur `ColorError` (`SelectedStyles`), le glyphe `⊗` reste lisible par sa forme. Sur `db-1` (action en cours), `●` alterne avec `○` dans le texte de la cellule, y compris sous cette sélection.

### 3.6 Footer : ligne en cours, ligne en erreur (Rule 128)

Précédence : error > warn > info > status. Un détail de ligne est de niveau `Status` (sans minuteur, recalculé à chaque trame), composé « détail de ligne · progression globale ». Le détail est tronqué d'abord (Rule 124 : la hauteur ne change pas).

Ligne en cours (`ws`, ligne `api` sélectionnée, sync de 12 dépôts) :

```
(blanc)
        ● Syncing ~/w/app/api · 3/12 repositories
```

Niveau `Status`, texte de couleur ordinaire. Pas de spinner de footer ici : la pastille bouge déjà sur la ligne ; un spinner doublerait le mouvement.

Ligne en erreur, en trois temps :

| Temps | Niveau | Texte | Couleur |
|---|---|---|---|
| t = 0 à 3 s (l'échec arrive) | `Error`, avec minuteur | `Failed to scan ~/w/app/web — trivy could not pull the database` | `ColorFooterError`, gras |
| t > 3 s, ligne `web` sélectionnée | `Status`, persistant | `⊗ ~/w/app/web: last scan failed — trivy could not pull the database` | `ColorFooterInfo`, normal |
| t > 3 s, autre ligne sélectionnée | `Status` | le détail de cette ligne, ou la progression globale seule | idem |

La pastille rouge fixe persiste jusqu'à la prochaine tentative sur la ligne ; seul le texte de footer passe de l'événement à l'état (décision de l'architect, Q4). Si une opération est en cours sur la même ligne, l'échec masqué est en suffixe (§1.2). Si un autre `Error` arrive pendant les 3 s, il prend le pas (précédence inchangée).

## 4. Fréquence et « en file »

**Tranché (D7) : 1 Hz (500 ms allumé, 500 ms éteint), phase déduite de l'horloge.**

| | 1 Hz (recommandé) | 2 Hz |
|---|---|---|
| WCAG 2.3.1 (≤ 3 flashs/s) | 1 flash/s : marge de 3 | 2 flashs/s : sous le seuil, marge de 1,5 |
| Perception | pouls calme, ne distrait pas pendant la lecture d'un tableau | réveille l'œil, se lit comme une alerte |
| Coût (SSH) | une réécriture de cellule par ligne par seconde | le double |
| Cadence du tick routeur | 500 ms (déjà prévue par l'architect) | 250 ms |
| Détection de l'état gelé | un blocage de plus de 1 s se voit | idem, plus vite |

Justification complémentaire : la surface qui clignote est minuscule (une cellule par ligne), la variation de luminance est modeste (anneau contre disque, même couleur), et rien ne clignote en rouge. Toutes les pastilles clignotent **en phase** : un décalage entre lignes créerait un effet de vague qui augmente le flash perçu. La phase vient du temps du message de tick (`on = (t.UnixMilli() / 500) % 2 == 0`) : survit à un changement de cadence, testable. 2 Hz resterait une constante à changer si le pilote la jugeait trop lente (le premier point de contrôle peut le rouvrir).

**« En file » : statique** (`◴`, `ColorActivityNeutral`). Voir §1.3. Passage `◴` à `●`/`○` à la prise en charge ; le tick démarre alors, une seule fois.

## 5. Cas limites

1. **Plusieurs pastilles simultanées dans une vue.** Toutes en phase, pas de plafond. Un lot de N scans donne 1 en cours + (N−1) en file : un seul clignotant (le tick est partagé) et N−1 `◴` fixes ; la progression globale (« 3/12 ») reste au footer. Si 50 % des lignes visibles clignotent, la surface reste faible et la fréquence est de 1 Hz ; pas de réduction supplémentaire à prévoir. Le réglage `app.motion: static` reste la sortie (A-02).
2. **Ligne en cours et en échec à la fois** : priorité décidée ; l'échec réapparaît à la fin de l'opération, sa clé n'ayant pas changé. Footer avec suffixe (§1.2).
3. **Largeur des glyphes.** `●` `○` `◐` `◕` `◴` `◉` `◌` `⊗` `⊘` `⊖` sont, pour la plupart, de largeur « ambiguë » (la largeur de `◴` est à vérifier dans la rangée de test) (1 cellule en locale occidentale, 2 dans un terminal configuré en Est-asiatique) et certains Nerd Fonts les élargissent. `IconColumnWidth` = 2 couvre les deux cas, à condition que le `Cell` renvoie **le glyphe seul** et que `datatable` complète à 2 avec `runewidth`. Test à ajouter : la Rule 116 tient avec `runewidth.EastAsianWidth = true`. Un glyphe de 2 cellules colle alors au texte voisin : accepté, la colonne n'a pas de marge de plus.
4. **Terminal sans Nerd Font** (note seulement, hors périmètre, voir backlog §3.98). La pastille n'est pas affectée (glyphes Unicode standard), contrairement aux icônes de repos, qui s'afficheront comme des carrés. Mais certains polices de console (ex. Consolas sous Windows, hypothèse non vérifiée) n'ont pas `◴` `◕` `◌` `⊗` `⊘` `⊖` et les substituent par une police de repli, donc parfois plus larges. À consigner pour §3.98 ; l'utilisateur a confirmé le rendu dans son terminal (D7) ; les autres terminaux restent une note.
5. **`NO_COLOR` et terminaux 16 couleurs** : les quatre alias retombent sur des couleurs distinctes ou sur la couleur par défaut ; l'unicité des glyphes garantit la lecture. Aucun cas ne repose sur la teinte.
6. **Conteneur `exited` 0** : `⊖` neutre, tranché (D7). Un arrêt volontaire n'a rien à corriger ; rouge partout diluerait la couleur d'alerte (Rule 122 « color discipline ») et noierait les vrais `exited` non nuls et les `dead`.
7. **Rafraîchissement périodique** (métriques containers, ports en direct, lecture agents de 2 s) : ne déclenche aucun clignotement (donnée).
8. **Action `docker stop` instantanée** : la pastille `●`/`○` apparaît par l'état `busy` (priorité « en cours »), puis l'icône habituelle (conteneur `running`) ou le glyphe d'état observé revient au prochain sondage ; un échec reste au footer (non-objectif).
9. **Focus de formulaire.** `IconCircleSmall` (``, Nerd Font) signale le champ actif dans les formulaires. C'est une forme proche de `●` mais dans un autre écran, jamais dans une table : pas de collision, à rappeler dans l'aide.
10. **Ligne sans icône de repos** : cellule vide de largeur `IconColumnWidth`, jamais `◌`.

### Libellé de la légende de l'aide `?` (Rule 114, anglais US)

Une seule source dérivée de la table de glyphes (A-01 absorbe A-15). Base, présente dans chaque vue migrée :

```
Title: Activity indicator
Description: The first column shows what is happening on a row. The glyph carries the
meaning; the color repeats it. Nothing here relies on color alone.

  ●  ○   Running (blinks). Pauses to a steady ● when motion is off.
  ◴      Queued, waiting to start.
  ⊗      Failed. Stays until the next attempt on that row; the footer gives the cause.
  (icon) Idle: the row's usual icon, or an empty cell.

The footer describes the selected row's state in words.
Turn the blinking off with app.motion: static in the context file or in :cfg.
```

Libellés **validés** (D10), à une réserve : la fin de la dernière phrase (« or in :cfg ») suppose que `app.motion` est modifiable dans la vue de configuration. **À confirmer** (architect, comme `app.gauge_glyph`) ; sinon retirer « or in :cfg ».

Extension propre à `:jobs` : `◉ done · ⊘ cancelled · ⊖ skipped`.
Extension propre à `:agent-usage` : `◐ idle (no activity for 5 min) · ◉ done · ◌ unknown`.
Extension propre aux containers : `◕ restarting · ◐ paused · ◌ created · ⊖ exited (code 0) · ⊗ exited (non-zero code) or dead · ●/○ an action (stop, kill, remove) is running`. Pas de ligne `running` : un conteneur qui tourne montre son icône habituelle (D8).

## 5 bis. Écarts avec les user stories et les règles

| Où | Écart | Proposition |
|---|---|---|
| A-01, critère 1 (`theme.IconStyle(rôle)` avec rôles `IconRole*` surchargeables) | Les couleurs d'état sont des alias fermés, pas des rôles `icon_*` (l'architect Q8 et `app-shell.md` disent déjà non). Les rôles `IconRole*` ne servent qu'à l'icône de **repos**. | Intégré : A-01, critère 1 (D4). |
| A-13, critère 2 (`unknown` en dim) | `ColorDim` échoue à 3:1, surtout sous la sélection. | Intégré : `unknown` en `ColorActivityNeutral`, glyphe `◌` (A-13, critère 2). |
| A-01, critère 2 / A-02, critère 2 | « glyphe statique différent du repos et de l'erreur » : satisfait par `●` fixe. | Intégré : `●` nommé dans A-02, critère 2. |
| **Rule 121** (`IconOK`, `IconError`, `IconWarning` pour tout statut de tableau) | Les pastilles `◉` `⊗` `◐` remplacent ces icônes dans la première colonne d'activité, ce qui contredit la lettre de la règle (`:jobs`, containers). | **Amendement accepté (D9)** : Rule 121 « ne s'applique pas à la colonne d'activité ; les autres colonnes d'état gardent `IconOK`/`IconError`/`IconWarning` ». Toute colonne Verdict ou Update reste inchangée. Écrit avec le composant (A-01, critère 10). |
| Rule 125 | La colonne est sans titre, `SizingFixed`, sans `Less`/`Search` : conforme. Elle devient aussi la colonne d'activité, et non plus seulement d'icône de type. | Paragraphe ajouté à la Rule 125 avec le composant (A-01, critère 10) ; renvoi au garde-fou dans A-17. |
| Rule 122, couleur discipline | Pas de vert pour terminé : conforme. Cinq glyphes en `ColorText` pour des états différents n'est pas de la « couleur partout » : c'est de la couleur neutre. Le clignotement lavande est le seul accent. | Aucun. |
| A-14, critère 2 (« `dead` rouge fixe », « `running` non vert ») | Remplacé par la décision 8 : un conteneur `running` n'a **aucune pastille** (icône habituelle) ; `◉` ne désigne plus qu'un état « terminé ». | Intégré : A-14, critères 2 et 3. |
| A-15 (légende) fusionnée dans A-01 par l'architect | La légende est dérivée de la table de glyphes, texte au §5. | Intégré : A-01, critère 9. |
| Aide containers existante (`view.go:220`) | Elle décrit `IconCaretRight`, `IconSmallPause`, etc. | À remplacer par la légende ci-dessus (A-14, critère 8). |

## 6. Décisions de l'utilisateur et points restants

### Tranchés (2026-11-06)

| Ancienne question | Décision |
|---|---|
| Q1 Jeu de glyphes dans le terminal | Gardé tel quel (D7) : `●` et `◉` se distinguent, **pas de repli `✓`** ; l'ancien glyphe en quart (U+25D4) remplacé partout par `◴` |
| Q2 Phase éteinte | `○`, même couleur (D7) |
| Q3 Fréquence | 1 Hz (D7) |
| Q4 Conteneur `exited` code 0 | `⊖` neutre (D7) |
| Q5 `restarting` | `◕` orange fixe, ne clignote pas (D7) |
| Q6 « En file » | `◴` statique (D7) |
| Q7 Amendement de la Rule 121 | Accepté (D9) |
| Q8 `unknown` / `created` | Couleur neutre, pas dim (D7) |
| Q9 Libellés de la légende `?` et du footer | Validés (D10), sauf la réserve « or in :cfg » ci-dessous |
| Q10 Conteneur `running` en `◉` | **Remplacé** : aucune pastille, icône habituelle (D8) ; `◉` = terminé uniquement |

### Restent à confirmer

1. **« or in :cfg »** dans la légende : `app.motion` est-il modifiable dans la vue de configuration ? À vérifier par l'architect (`stories.md`, question ouverte 7). **Résolu (2026-11-07, DEVDESK-141)** : modifiable dans `:cfg` en plus du YAML ; une clé absente reste absente. La phrase est conservée.
2. **Test de contraste** : les rapports du §2.1 sont des calculs à la main ; le test de A-01 (critère 7) doit les confirmer, pour chaque alias, sur le fond et sur `ColorTableLineSelected` (`stories.md`, question ouverte 8).
