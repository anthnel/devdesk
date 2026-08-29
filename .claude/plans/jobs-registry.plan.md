# Plan : un registre de jobs, et la vue qui les montre

**Demande d'origine** : pouvoir changer de vue pendant qu'un scan tourne ; voir
quelque part les scans en cours ; dans `ws`, un spinner dans la colonne Scanned
plutôt qu'un compteur qui s'incrémente, et le compteur au footer comme pour le
clone.

**Ce que la discussion en a fait** : le compteur au footer et la vue des scans
sont deux lectures du même objet manquant — un registre du travail long, tenu
hors des vues. La portée a été élargie à **tout travail long** (scan, sync,
clone, pull, delete) et à une **vue `:jobs`** dédiée.

**Complexité** : Large — 8 postes, plusieurs PRs, un paquet neuf et une vue
neuve. Le poste 1 est autonome et livrable seul.

---

## 1. Ce qui est demandé, relu

| # | Demande | Traduction |
|---|---|---|
| 1 | Changer de vue pendant un scan | Supprimer le retour forcé, router les messages de progression vers la vue qui les attend |
| 2 | Voir les scans en cours | Une vue `:jobs`, deux niveaux (runs → items) |
| 3 | Spinner dans la colonne Scanned de `ws` | La ligne **répertoire** aussi, pas seulement la ligne dépôt |
| 4 | L'incrément au footer | `Status` dérivé (Rule 128), pas un message posé |
| 5 | *(élargi)* Tout travail long, pas seulement les scans | scan, sync, clone, pull, delete |
| 6 | *(élargi)* Prévoir l'annulation | Sémantique par kind, `K` grisée quand elle ne s'applique pas |

---

## 2. L'état actuel

### Le défaut d'origine

`internal/app/app.go:397` — `handleWorkspaceScanComplete` fait
`a.currentView = command.ViewWorkspaces` à **chaque** dépôt terminé. Un batch de
douze dépôts, ce sont douze retours forcés. Le garde `!= ViewSecurity` juste
au-dessus n'existe que pour compenser ce switch.

Le patron correct est déjà écrit à côté, pour les images :
`routeToOCIImagesView` (`internal/app/scan_details.go:113`) met à jour la vue
détenue **sans** toucher `currentView`, avec le commentaire qui dit exactement
ce qui est demandé ici — « Scan progress belongs to the OCI view wherever the
user has gone ». `ws` est la seule des trois vues scannantes à ne pas l'avoir.

### Deux conséquences masquées par ce retour

**`WorkspaceScanStartingMsg` n'est routé nulle part.** Il n'est pas dans le
switch de `app.go`, donc il tombe dans `default → forwardToActiveView`
(`internal/app/keys.go:73`) et part vers la vue active, qui l'ignore. Si
l'utilisateur a quitté `ws`, `scanningPaths[path]` n'est jamais posé — et le
`Complete` correspondant, lui routé, fait un `delete` sur une clé absente.

**Le spinner.** `spinner.TickMsg` est aussi un message `default` : dès qu'on
quitte `ws`, le tick suivant est livré à la vue active et la chaîne de `ws`
meurt. Elle repart au retour, parce que `switchView` appelle `Init()`
(`command_line.go:130`) et que `Init` retourne `m.spinner.Tick`
(`workspaces/update.go:18`). Hors écran elle est figée, ce que personne ne
regarde. **Vérifié** : `setEntries` ne touche pas `scanningPaths` — c'est une
map à part, clé = chemin — donc le retour ne perd pas l'état.

### Le spinner ment déjà

`scanOneRepoCmd` (`workspaces/commands.go:156`) est un `tea.Sequence` : le
`Starting` part **immédiatement**, puis le corps bloque sur un sémaphore de
`runtime.NumCPU()/2`. Sur douze dépôts et quatre workers, huit lignes affichent
« scanning » alors qu'elles attendent leur tour. Le clone, lui, distingue déjà
`cloneQueued` de `cloneRunning`.

### Quatre comptabilités parallèles

| Vue | Ce qu'elle tient | Où |
|---|---|---|
| `ws` | `scanningPaths`, `syncingPaths`, `deletingPaths`, `sync *syncRun` | `workspaces/model.go:74,86,89` |
| `oci` | `scanningImages` | `oci_resources/model.go:34` |
| `:sec` | `scanTarget.Scanning` sur chaque ligne, préservé à travers les reloads | `security/inventory.go:235` |
| explorer | `cloneList` — cinq états, `run *cloneRun`, `frameIdx` | `explorer/clone_list.go:21,50,57` |

Les quatre stampent une frame de spinner à la main, les quatre portent le même
commentaire sur le même piège (Rule 122 : une frame **nue**, jamais un
`View()` rendu). Aucune ne voit ce qu'une autre a lancé : le garde `busy()` de
`ws` (`sync.go:39`) ignore un scan que `:sec` a démarré sur le même dépôt, et
les deux écrivent la même entrée de cache.

### Le précédent à généraliser

`cloneList` **est déjà une vue de jobs**, pour une exécution : cinq états
(`queued`, `running`, `cloned`, `alreadyThere`, `failed`), un spinner par ligne,
une annulation aux sémantiques écrites, et il sert de progression *et* de
rapport. `cloneRun.cancel` (`explorer/pipeline.go:92`) documente le cas dur :

> cancel stops **discovery only**. A `git clone` is never interrupted: a context
> that kills one leaves half a repository on disk.

C'est le modèle du registre, pas son voisin.

---

## 3. Les décisions

Toutes tranchées en discussion. Elles sont écrites ici avec ce qui a été rejeté,
parce que c'est ce qui empêche de les rouvrir à la première difficulté.

### D1 — Diffusion par message, pas pointeur partagé

`shared.State` est un `*shared.State` que le routeur écrit et que dashboard et
explorer lisent : le précédent existe. Mais `ws`, `security` et `oci_resources`
ne le reçoivent pas (`ws` n'a que `Secrets.Storage`, `command_line.go:172`), et
ce serait un état mutable lu depuis `View()`.

**Retenu** : le routeur diffuse un `JobsChangedMsg` portant un **instantané**
des runs et la frame courante du spinner. Chaque vue garde sa copie.

| Rejeté | Pourquoi |
|---|---|
| Pointeur sur `shared.State` | trois signatures de constructeur à changer, et le seul endroit où `View()` lirait ce qu'un autre écrit |
| Chaque vue interroge le registre à la demande | il faudrait un accesseur global, donc un état partagé déguisé |

### D2 — Deux niveaux : un `run`, des `items`

Le clone liste par dépôt, le sync compte par batch, le scan spinne par dépôt.
Une vue plate qui liste deux cents scans est illisible ; une vue qui ne liste
que trois batchs perd le détail que le clone affiche déjà.

**Retenu** : un `Run` (le batch) contenant des `Item` (une cible chacun). La vue
`:jobs` liste les runs, `→` descend aux items, `←` remonte — le vocabulaire
drill-down de Rule 111, déjà celui de l'explorer et de `:sec`.

### D3 — `ModeCloning` devient le niveau item d'un run de kind clone

| Voie | Coût |
|---|---|
| Il disparaît, `:jobs` le remplace | le plus propre, mais on retouche le clone qui marche, et le rôle de **rapport** est à reloger |
| Il reste et publie *en plus* au registre | double comptabilité — la classe de bug que Rule 128 a supprimée pour les footers |
| **Il devient la vue de détail d'un run** | ✅ même code de rendu, un seul propriétaire de l'état, l'explorer garde son écran quand on clone depuis lui |

**Retenu** : la troisième. Avec D2 l'écran de clone *est* le niveau item filtré
sur un run. On ne le réécrit pas, on lui change sa source.

### D4 — Le paquet est `internal/jobs`

Ce n'est plus spécifique aux scans. Le registre y vit, le routeur le détient, la
diffusion en sort.

### D5 — Le routeur possède la chaîne de spinner

**Retenu** : une seule chaîne, celle du routeur, vivante tant que le registre
n'est pas vide ; la frame voyage dans `JobsChangedMsg`. Les quatre `frameIdx`
stampés à la main disparaissent.

C'est ce qui supprime la classe de bug entière (frame figée, chaîne doublée)
plutôt que de la contourner une cinquième fois.

### D6 — `queued` et `running` sont distingués

Le sémaphore le sait déjà, le clone le fait déjà, et `:jobs` est exactement
l'endroit où « 4 tournent, 8 attendent » se lit. Ne pas distinguer ferait du
clone la seule vue honnête.

### D7 — L'annulation est prévue dès la v1, branchée au poste 8

Le `context.CancelFunc` est stocké dans l'`Item` **dès le premier poste qui crée
des jobs** : le rajouter après veut dire retoucher les cinq sites de lancement
une seconde fois.

Sémantique **par kind**, et elle diffère :

| Kind | Annulable | Ce que « annuler » veut dire |
|---|---|---|
| scan | oui, vraiment | Trivy et Gitleaks lisent, couper ne laisse rien derrière |
| clone | partiellement | arrête la découverte, **n'interrompt jamais** un `git clone` en vol (`pipeline.go:92`) |
| sync | partiellement | arrête la file ; un fast-forward en cours est attendu |
| pull | oui | `docker pull` reprend par couches |
| delete | **non** | à moitié supprimé est pire que supprimé |

`K` est grisée sur un job non annulable, avec un motif nommé au footer
(Rule 130).

### D8 — Rétention : le temps de la session, filtrée par contexte

Les runs terminés vivent le temps de la session, plafonnés aux **20 derniers**.

Chaque run est **estampé de son contexte** et la vue **filtre** sur le contexte
courant, plutôt que de purger au changement de contexte — ce qui contredirait
« le temps de la session ». L'estampe sert de toute façon au poste 5.

### D9 — Le footer garde son détail, et dégrade

Chaque vue garde sa ligne détaillée tant que le travail en cours vient d'elle et
d'un seul kind :

```
Scanning — 3/12
Scanning — 3/12 · Syncing — 1/4      ← deux kinds depuis la même vue
3 jobs running — :jobs for details   ← mélangé, ou lancé d'ailleurs
```

`Status` dérivé à chaque frame, jamais un message posé — Rule 128 l'explique :
un message posé au premier dépôt s'effacerait à 3 s pendant que le dixième
tourne. Modèle : `syncStatusLine()` (`workspaces/sync.go:169`).

### D10 — Un run est enregistré au lancement, dans `Update`

Tous les sites de lancement connaissent la liste complète des cibles au moment
où ils dispatchent. Le `Run` et ses `Item` en `queued` sont donc créés **là**,
dans `Update` (Rule 110 — l'écriture ne peut pas venir du `Cmd`). Les messages
existants ne font plus que des transitions.

Conséquence à traiter : `rescanCmd` de `:sec`
(`security/inventory_commands.go:252`) n'émet **aucun** message de départ — il
n'a que `InventoryScanFinishedMsg`. Il lui en faut un, sans quoi ses items
sautent `queued → done` et D6 y serait faux.

---

## 4. Le modèle

`internal/jobs` :

```
Kind      : scan | sync | clone | pull | delete
ItemState : queued | running | done | skipped | failed
RunState  : dérivé des items (running tant qu'un item ne l'est pas)

Item : Target  string     // chemin de dépôt, référence d'image, chemin de groupe
       Display string     // ce que la vue affiche (alias de registry appliqué)
       State   ItemState
       Detail  string     // « already there », le motif d'échec
       cancel  context.CancelFunc

Run  : ID        JobID
       Kind      Kind
       Origin    command.ViewType   // pour y revenir, et pour D9
       Context   string             // D8, et le correctif du poste 5
       Label     string             // « ~/work/perso », « nexus.example.com/team »
       Items     []Item
       StartedAt / EndedAt time.Time
       cancel    context.CancelFunc // au niveau run : arrête la file

Registry : Start(run) · Advance(id, target, state, detail) · Cancel(id) ·
           Snapshot() []Run · Running() int
```

`Snapshot()` retourne une **copie** — c'est ce que `JobsChangedMsg` transporte,
et ce qui rend D1 vrai.

Le compteur du footer se dérive du snapshot, pas d'un champ tenu à part :
`total` est `len(Items)`, `done` en est le compte des états terminaux. Il n'y a
pas de `scanRun` à écrire à côté de `syncRun` — les deux se dérivent.

---

## 5. Les postes

### Poste 1 — `fix:` le retour forcé *(autonome, livrable seul)*

Ne dépend d'aucun des suivants et règle l'essentiel de la gêne.

- `app.go` : `handleWorkspaceScanComplete` ne touche plus `currentView` ; le
  garde `!= ViewSecurity` part avec lui, il n'existait que pour ce switch.
- `app.go` : router `workspaces.WorkspaceScanStartingMsg` comme le `Complete`,
  vers la vue `ws` détenue. Un `routeToWorkspacesView` sur le modèle de
  `routeToOCIImagesView`, ou la généralisation des deux en un helper.
- Idem pour les messages de sync, pour la même raison.

**Tests** : la vue courante ne change pas quand un `Complete` arrive alors qu'on
est ailleurs ; un `Starting` reçu hors de `ws` marque quand même le dépôt.

**Entrée backlog** : un `D` en §1.1 — le défaut est visible par l'utilisateur.

### Poste 2 — `internal/jobs` + diffusion + chaîne de tick

- Le paquet et ses types (§4), avec ses tests unitaires : transitions,
  dérivation de l'état d'un run, plafond des 20, filtre par contexte.
- Le routeur détient un `*jobs.Registry`, émet `JobsChangedMsg` à chaque
  changement, et tient **une** chaîne `spinner.Tick` tant que `Running() > 0`.
- La frame voyage dans le message. Elle est **nue** (Rule 122) : les vues la
  posent en cellule, le footer la reçoit rendue via
  `footer.SetSpinnerFrame(...)` (Rule 128).

Rien n'est encore branché : le registre est vide, les vues continuent comme
avant. C'est délibéré — le poste est vérifiable seul.

### Poste 3 — `ws` branché *(livre les demandes 3 et 4)*

- `scanningPaths` / `syncingPaths` / `deletingPaths` / `sync *syncRun` sont
  remplacés par la lecture du snapshot ; `busy()` et `anyBusy()`
  (`sync.go:39,48`) interrogent le registre — donc voient enfin ce que `:sec` a
  lancé.
- `formatScanColumns` (`view.go:251`) : la ligne **répertoire** rend
  `frame + " scanning"` dès qu'un sous-dépôt tourne, au lieu de
  `IconDirectory + " N/M"` (`view.go:317`). Le `N/M` ne dit plus que la
  couverture **stabilisée**, ce à quoi il sert.
- Le footer applique D9. `syncStatusLine` devient un cas d'une fonction qui
  compose les portions.
- Le `frameIdx` local et le handler `spinner.TickMsg` de `ws` disparaissent.

**Tests** : un répertoire dont un sous-dépôt scanne rend un spinner et pas de
compteur ; le footer rend les trois formes de D9 ; la suite de touches ne change
pas d'un état à l'autre (`testutil.ShortcutKeys`, Rule 130).

### Poste 4 — `:sec` et `oci` branchés

- `oci_resources.scanningImages` → snapshot ; `frameIdx` retiré.
- `:sec` : `scanTarget.Scanning` est dérivé du snapshot, ce qui supprime la
  préservation manuelle de l'in-flight à travers les reloads
  (`inventory.go:235`) — le cache n'a plus à mentir en attendant.
- `rescanCmd` émet un message de départ (D10).
- Le marqueur « en cours » de `:sec` devient vrai pour un scan lancé
  **ailleurs**, ce qui est la deuxième moitié de la demande 2.
- Compteur au dashboard : « 2 jobs running ».

### Poste 5 — `fix:` l'estampe de contexte

`config.CurrentContextName()` est lu **dans le Cmd**, à la fin du scan
(`workspaces/commands.go:197`). Un changement de contexte en cours de batch fait
atterrir les résultats dans le cache du **nouveau** contexte.

Le `Run.Context` de D8 le corrige : le contexte est estampé au lancement et
porté jusqu'à l'écriture. `fix:` distinct, entrée `D` propre en §1.1 — pas un
effet de bord silencieux d'une feature.

### Poste 6 — La vue `:jobs` *(le poste le plus lourd)*

- `command/parser.go` : `ViewJobs`, alias `jobs` et `j` (`parser.go:76`). Pas de
  lettre majuscule — le vocabulaire majuscule est celui des **actions dans une
  vue**, le changement de vue passe par `:` (Rule 111).
- Deux niveaux (D2) : runs, puis `→` sur les items d'un run.
- Colonnes runs : icône de kind (sans titre, `datatable.IconColumnWidth`,
  `Style` par rôle — Rule 125), Kind, Label, Progress (`7/12`), State, Started
  (`theme.TimeAgo`, Rule 127). Chaque colonne déclare son `Sizing` (Rule 116).
- Colonnes items : icône d'état, Target, State, Detail.
- La checklist qu'impose une vue neuve ici : `GetShortcuts()` état-aware
  (Rule 130) et descriptions à l'impératif capitalisées (Rule 137), sans les
  raccourcis évidents (Rule 138) ; `GetHelpContent()` (Rule 114) ;
  `components.FilterBar` (Rule 136) ; chargement au footer et jamais dans le
  corps (Rule 139) ; `FooterMessage` (Rule 128) ; aucun `[touche] action` dans
  le viewport (Rule 134) ; tout en English US (Rule 129).

### Poste 7 — Le clone rebranché (D3)

`cloneList` lit le snapshot au lieu de tenir ses lignes ; `cloneRun` devient la
source d'événements qui alimente `Advance`. Les cinq états du clone sont
l'origine de `ItemState`, donc la correspondance est directe — `cloned` et
`alreadyThere` sont `done` et `skipped`.

L'explorer garde son écran quand on clone depuis lui ; `:jobs` montre le même
run sans le dupliquer.

### Poste 8 — L'annulation

`K` sur un run (arrête la file) et sur un item quand le kind le permet (D7).
Grisée avec motif nommé quand il ne le permet pas (Rule 130). Le
`context.CancelFunc` est déjà en place depuis le poste 2 ; il ne reste que la
touche, le garde et les tests.

---

## 6. Hors périmètre

- **Le scan lui-même** : `scanner.Scan(context.Background(), ...)` reçoit un
  contexte annulable, rien d'autre ne change dans `internal/scan`.
- **Fusionner les caches** : les deux caches de scan restent ce qu'ils sont ; le
  registre parle du travail en vol, pas de ses résultats.
- **Persister les jobs** : le registre est en mémoire, il meurt avec la session
  (D8). Rien n'est écrit sur disque.
- **Une file globale** : les sémaphores restent par batch. Le registre observe,
  il n'ordonnance pas.

---

## 7. Entrées de backlog à écrire

| Entrée | Section |
|---|---|
| `D` — un scan terminé ramenait de force à `ws` | §1.1, poste 1 |
| `D` — un batch traversant un changement de contexte écrivait dans le mauvais cache | §1.1, poste 5 |
| `§3.x` — le registre de jobs et la vue `:jobs` | §3, postes 2–8 |

Rappel `.claude/CLAUDE.md` : plusieurs branches coupées du même commit
conflictent dans `docs/backlog.md`, chaque entrée s'insérant en tête de §1.1. La
résolution est toujours de garder les deux côtés.

---

## 8. Ce qu'il faut relire avant de commencer

| Fichier | Pourquoi |
|---|---|
| `docs/architecture/app-shell.md` | le routeur, les messages inter-vues — à mettre à jour dans le même commit que le poste 2 |
| `docs/architecture/scanning.md` | les deux caches, `scan.Categorize` — poste 4 |
| `docs/architecture/forge.md` | le pipeline de clone — poste 7 |
| `docs/architecture/workspaces.md` | le sync (`F`) — poste 3 |
| `.claude/rules/tui-tables.md` | Rules 116, 122, 125, 136, 139 — postes 3, 6 |
| `.claude/rules/tui-layout.md` | Rules 111, 124, 130, 134, 137, 138 — poste 6 |
| `.claude/rules/tui-behavior.md` | Rules 110, 128 — partout |
