# Plan : le serveur MCP passe dans le TUI, en HTTP, et il agit

**Source** : `docs/backlog.md` §3.61
**Base** : `0e93d24`
**Complexité** : **Large** — 1 transport remplacé, 1 secret nouveau, 1 boucle
d'invocation à travers `Update()`, 5 outils d'action, 4 vues touchées, 2 tests à
supprimer avec leur raison.

## Résumé

§3.61 renverse §3.38 sur trois axes : stdio devient du Streamable HTTP servi par
le process du TUI, la lecture seule devient une écriture non destructrice, et le
contexte fixé au démarrage devient celui de la session. Ce plan les applique
dans l'ordre où chaque étape reste vérifiable seule.

**Le livrable qui n'est pas dans le tableau des outils est le plus important** :
la boucle d'invocation. Un handler HTTP qui traverse `Update()` sans jamais
toucher le modèle, avec une corrélation qui n'a pas de fenêtre temporelle. Le
reste — le token, les cinq outils, l'onglet de configuration — est mécanique une
fois qu'elle tient.

## Vérifications préalables — faites

| Point | Résultat |
|---|---|
| Le SDK sait servir en HTTP | ✅ `NewStreamableHTTPHandler` et `StreamableServerTransport`, `mcp/streamable.go:232` et `:789` de `go-sdk@v1.7.0`. Pas de mise à jour de dépendance |
| `newServer(env)` est déjà séparé de `Serve` | ✅ `internal/mcp/server.go` — il existe pour que les tests le pilotent sur un transport en mémoire. C'est exactement le point d'accroche du handler HTTP |
| Le routeur possède déjà le registre de travaux | ✅ `App.jobs`, `internal/app/app.go:184` |
| Un run est admis en **un seul endroit** | ✅ `handleStartJobs`, `internal/app/jobs.go:40` — c'est le seul appelant de `a.jobs.Start`, et c'est là que le `JobID` est alloué. Décide toute l'architecture, voir ci-dessous |
| Le modèle est un pointeur, donc adressable avant `p.Run()` | ✅ `app.New` rend `*App` (`app.go:138`), `main.go` construit `p` juste après |
| `mcp.expose` refuse déjà un nom inconnu | ✅ `exposedTools`, `internal/mcp/tools.go` — les outils d'action s'y déclarent sans rien changer |
| Le store de secrets est adressé par une URL | ✅ `Storage.Get/Set/Delete(url string)`, `internal/credentials/storage.go:12` — un token de serveur y tient sous une URL sentinelle |
| L'onglet `mcp` existe déjà dans la vue configuration | ✅ `internal/ui/configuration/fields.go:337` — un `toggle` et un `static` qui affiche `dk mcp --context …`, à réécrire |

**Reste à vérifier à la main, et aucun test Go ne peut le faire** : qu'un agent
dans une sandbox `sbx` atteint bien `http://host.docker.internal:7777` une fois
la network policy ouverte. C'est l'hypothèse dont dépend le choix du bind, et la
phase 1 livre de quoi la tester en une commande.

## La découverte qui décide de l'architecture

`jobs.StartMsg` est **le point de passage unique** où un run est admis et son
`JobID` alloué, dans `Update()`, par le routeur (`handleStartJobs`). Les vues ne
font que décrire le run et le travail ; le routeur les stampe du contexte
courant (D68) et alloue l'identifiant.

Ça donne trois choses d'un coup :

1. **Le handler HTTP n'a aucun run à construire.** Il envoie une requête, la vue
   construit le run comme elle le fait pour une touche, le routeur l'admet.
   Aucune duplication de `scanRun`, `batchScanCmd`, `syncSpec`.
2. **L'identifiant à rendre à l'agent existe déjà**, et à un endroit précis.
3. **Le stamp de contexte est le précédent exact** de ce qu'il faut ajouter pour
   la corrélation : une donnée que le run porte, écrite par le routeur, lue à
   la fin.

## Patterns à reprendre, pas à réinventer

| Catégorie | Source | Motif |
|---|---|---|
| Vocabulaire déclaré + test qui l'oppose au code | `internal/mcp/tools.go` + `TestTheServerRegistersExactlyTheDeclaredTools` | la table des outils est déjà de cette forme ; les actions s'y ajoutent |
| Une vue demande, le parent admet | `RegistryPullRequestedMsg` (§3.60), `browser_bridge.go:85` | la forme exacte d'une requête d'action venue d'ailleurs |
| Donnée stampée par le routeur, portée par le run | `run.Context` / `handleStartJobs` (D68) | le modèle de `run.Origin` |
| Un refus nommé, lu par deux moitiés de la vue | `shortcut.Availability`, `workspaces/availability.go` | le message d'erreur MCP est le même `Reason` que le grisage |
| Message de footer à trois niveaux | `components.FooterMessage` (Rule 128) | l'échec de bind |
| Réglage scalaire de contexte | `configuration/fields.go` — `toggle`, `text`, `static` | `mcp.listen` et le bouton « révéler le token » |

## Décisions

**D1 — Le handler HTTP ne construit aucun run.** Il envoie un message de
requête ; la vue construit le run. L'alternative — le routeur bâtit le run
lui-même — dupliquerait `scanRun`, `syncSpec` et les options de scan, c'est-à-dire
exactement ce que §3.58 a passé une entrée à unifier.

**D2 — La corrélation passe par le run, pas par une fenêtre temporelle.**
`jobs.Run` gagne un champ `Origin`, zéro pour le clavier. Un `a.pendingInvoke`
consommé par le prochain `StartMsg` serait une fenêtre d'un cycle `Update` dans
laquelle une touche peut s'intercaler — et le bug serait un `job_id` rendu à
l'agent pour le scan que l'utilisateur vient de lancer à la main. `Origin`
voyage sur le message de requête, la vue le repasse à `jobs.Start`, le routeur
le lit là où il alloue l'identifiant. C'est la forme de `run.Context`.

**D3 — Un outil d'action rend le `JobID`, depuis `handleStartJobs`.** C'est le
seul endroit où il existe. Le canal de réponse est **bufferisé à 1** : `Update()`
ne doit jamais bloquer sur un client qui a raccroché.

**D4 — Un refus réutilise `shortcut.Availability.Reason`.** « Aucun scanner
installé », « scan déjà en cours » sont déjà des constantes nommées lues par le
header (pour griser) et par le handler (pour refuser). L'erreur MCP est la
troisième lectrice, et Rule 130 disait déjà qu'il n'y a qu'un calcul.

**D5 — Les outils de lecture gardent `internal/cache/readonly.go`.** Le modèle
est là, dans le même process, et le lire serait la data race que Rule 110
interdit. Le bénéfice serait nul : §3.38 a vérifié que tout ce qu'ils répondent
est sur disque ou ailleurs. La propriété « un outil de lecture ne peut rien
décider » survit intacte.

**D6 — Le serveur est possédé par le routeur**, démarré et arrêté sur le
contexte. `main.go` ne fait que lui passer le `*tea.Program` avant `p.Run()`.

**D7 — Le token vit dans le store de secrets sous `devdesk://mcp`.** L'interface
`Storage` est adressée par URL ; une URL sentinelle est ce qui existe déjà pour
les entrées qui ne sont pas une forge.

**D8 — La vue cible est créée à la demande avant de recevoir une invocation.**
Les vues sont paresseuses ; un `scan_start` avant que l'utilisateur ait ouvert
`ws` doit marcher.

**D9 — `TestNothingInThisPackageWritesToStdout` et
`TestTheMCPBranchIsTakenBeforeAnythingPrints` sont supprimés, pas adaptés.**
Leur raison entière était que dans stdio *stdout est le canal*. Les garder
laisserait deux tests qui se lisent comme des contraintes encore vraies.

**D10 — `dk mcp` disparaît sans alias de transition.** Une sous-commande qui
répondrait « utilisez le TUI » est un chemin de code à maintenir pour une
phrase.

## Phases

### Phase 1 — Le transport bascule, à iso-fonctionnalité

Rien de nouveau n'est exposé. C'est la phase qui doit être vérifiable seule.

- `internal/mcp/http.go` : `Handler(env *Env) (http.Handler, error)` bâti sur
  `newServer` + `NewStreamableHTTPHandler`. `Serve`/`StdioTransport` s'en vont.
- `internal/config` : `MCPConfig.Listen string`, défaut `127.0.0.1:7777`,
  posé par `config.Default()` **et** par la migration d'un fichier écrit avant
  la clé — un `listen` vide doit valoir le défaut et non « n'écoute nulle part »
  (c'est D12 : la valeur zéro ne doit pas signifier un choix).
- `internal/app/mcp.go` : `startServerCmd` — ouvre le listener et sert. C'est de
  l'I/O, donc un `Cmd`, qui rend `MCPServerStartedMsg{Addr, Err}`.
- `main.go` : suppression de `runMCP`, du `flag.FlagSet`, du dispatch en tête de
  `main()`. `m := app.New(cfg)` puis `p := tea.NewProgram(m, …)` puis
  `m.AttachProgram(p)` avant `p.Run()` — écrit une seule fois, avant que la
  boucle démarre, donc hors de toute course.
- Suppression : `mcpserver.Serve`, `Refused()`, `main_test.go` (D9).

**Vérification** : `mise run dev` avec `mcp.enabled: true`, puis depuis l'hôte
un POST `initialize` sur `http://127.0.0.1:7777/`; puis depuis la sandbox le
même sur `http://host.docker.internal:7777/`, après
`sbx policy allow network "localhost:7777"`.

### Phase 2 — Le token

- `internal/mcp/token.go` : génération (32 octets d'aléa, base64url), lecture et
  écriture via `credentials.Storage` sous `devdesk://mcp`.
- Le handler refuse tout ce qui n'a pas `Authorization: Bearer <token>`, en 401,
  **avant** de lire le corps. La comparaison est en temps constant
  (`crypto/subtle`).
- **Si `credentials.Select()` a rendu un `MemoryStorage`, le serveur ne démarre
  pas** et `MCPServerStartedMsg.Err` le dit. Un token régénéré à chaque session
  casserait la configuration de l'agent une fois par lancement, et la panne
  serait attribuée à l'agent.
- Le token est créé à la première activation, jamais réécrit ensuite.

**Vérification** : la même requête qu'en phase 1 sans en-tête → 401 ; avec →
la réponse d'`initialize`.

### Phase 3 — La boucle d'invocation

C'est la phase à écrire lentement.

- `internal/mcp/invoke.go` : `InvokeMsg{ID InvokeID, Tool string, Args any,
  Reply chan InvokeResult}` et `InvokeResult{JobID, Err}`. `Reply` est
  **bufferisé à 1** (D3).
- Côté handler : `p.Send(InvokeMsg{…})` puis
  `select { case r := <-reply: …; case <-req.Context().Done(): p.Send(AbandonMsg{ID}) }`.
  Sans le `Abandon`, un agent qui coupe fuit une entrée de map par appel.
- Côté routeur (`internal/app/mcp.go`) : `a.pendingInvokes map[InvokeID]chan
  InvokeResult`, **mutée depuis `Update()` seulement**.
- `jobs.Run` gagne `Origin jobs.Origin` ; `jobs.StartMsg` le porte (D2).
  `handleStartJobs` : après `id := a.jobs.Start(run)`, si `run.Origin` nomme une
  invocation, répondre et retirer l'entrée.
- `jobs.StartFrom(origin, run, work)` à côté de `Start` / `StartInContext` —
  aucun appelant existant ne change.

**Tests** (`internal/app`) :

| Test | Ce qu'il empêche |
|---|---|
| `TestAnInvocationGetsTheIDOfItsOwnRun` | une touche pressée entre l'invocation et le `StartMsg` vole la réponse — c'est le bug que D2 supprime, et il faut qu'un test le prouve |
| `TestAnAbandonedInvocationLeavesNoPendingEntry` | la fuite de map |
| `TestUpdateNeverBlocksOnAReplyChannel` | le canal non bufferisé |
| `TestNoMCPCodePathReadsTheModel` | scan de source : le paquet `mcp` ne référence pas `*app.App` |

### Phase 4 — Les outils de lecture des travaux

`jobs_list` et `jobs_get`, et ils sont **les premiers outils qui ne peuvent pas
lire le disque** : ils passent par la boucle de la phase 3, avec une réponse qui
n'est pas un `JobID` mais un instantané. `InvokeResult` gagne donc un `Payload`.

`jobs_get` rend le **contexte du run** (§3.61) : un job lancé dans A pendant que
l'utilisateur bascule sur B continue dans A, et `run.Context` le dit déjà.

`jobs_cancel` n'invente aucune règle : `Kind.Cancellable()` répond, et son refus
est celui de la table D7 de §3.58.

### Phase 5 — Les outils d'action

Cinq, déduits du vocabulaire de `internal/ui/keymap` (§3.61) :

| Outil | Touche | Vue | Message de requête |
|---|---|---|---|
| `scan_start` | `S` | `ws`, `oci` | `ScanRequestedMsg{Targets, Origin}` |
| `scan_all_start` | `A` | `ws`, `oci` | idem, cible vide |
| `sync_start` | `F` | `ws` | `SyncRequestedMsg{Paths, Origin}` |
| `pull_start` | `G` | `oci` | `RegistryPullRequestedMsg` **existe déjà**, + `Origin` |
| `clone_start` | `C` | `explorer` | `CloneRequestedMsg{Targets, Origin}` |

Chacun :

1. le routeur crée la vue si elle n'existe pas (D8) ;
2. lui transmet la requête ; la vue résout la cible, consulte son
   `Availability` et rend soit `jobs.StartFrom(...)`, soit un refus portant le
   `Reason` (D4) ;
3. un refus est renvoyé comme erreur d'outil, pas comme un job qui échoue.

**Un test par outil** dans `internal/app`, plus deux transversaux :

| Test | Ce qu'il empêche |
|---|---|
| `TestEveryActionToolMapsToADeclaredUppercaseKey` | un outil d'action qui n'est pas dans `keymap.Actions()` — la règle de sélection de §3.61 devient vérifiable |
| `TestNoDestructiveActionIsExposed` | la liste noire de §3.61 (`D P M N`, `K` sur un conteneur) opposée à la table des outils |

### Phase 6 — Le contexte courant

- `ContextSwitchCompleteMsg` arrête le serveur si le nouveau contexte a
  `mcp.enabled: false`, le (re)démarre sinon, et le redémarre aussi si `listen`
  a changé.
- Chaque réponse d'outil porte le nom du contexte qui l'a servie.
- Une notification MCP est émise au changement.

**Test** : `TestSwitchingToADisabledContextStopsTheServer`, et son inverse.

### Phase 7 — Ce que l'utilisateur voit

- **Le footer** : `MCPServerStartedMsg.Err` non nil → `m.footer.Error(...)`,
  **et seulement si `mcp.enabled` est vrai** (§3.61). Rule 128 : c'est un
  `Error`, le système a refusé le bind.
- **La vue configuration**, onglet `mcp` : le `static("Command", "dk mcp
  --context …")` est faux dès la phase 1 et doit partir avec elle. Il devient
  l'URL à donner à l'agent, plus un `text` pour `listen`, plus une action
  « révéler le token ».
- **`docs/architecture/mcp.md`** réécrit dans le même commit que le code qu'il
  décrit, comme le demande `.claude/CLAUDE.md`.
- **Rule 114** : `GetHelpContent` de la vue configuration mentionne le token.

## Ce qui n'est pas dans les phases

- **La rotation du token** (question ouverte 4 de §3.61). Un bouton qui
  invalide la configuration de l'agent sans le lui dire ; à trancher avant de
  l'écrire, pas pendant.
- **Quitter `dk` avec un job en vol lancé par un agent** (question ouverte 3).
  Le comportement actuel — la sortie coupe tout — reste, et le plan ne le
  change pas.
- **Resources et prompts MCP** (question ouverte 2, héritée de §3.38).
- **Docker natif Linux** (question ouverte 1). `listen` est un réglage, donc le
  jour venu il n'y aura rien à réécrire.

## Risques

| Risque | Ce qui le contient |
|---|---|
| `Update()` bloque sur un canal | D3 (buffer 1) + `TestUpdateNeverBlocksOnAReplyChannel` |
| Un outil finit par lire le modèle | D5 + `TestNoMCPCodePathReadsTheModel` |
| Une action destructrice se glisse dans la table | `TestNoDestructiveActionIsExposed` |
| Le token part sur GitHub dans un `.mcp.json` committé | rien dans le code ne peut l'empêcher ; c'est écrit dans §3.61 et ça doit l'être dans l'aide de la vue configuration |
| `host.docker.internal` ne suffit pas | vérification manuelle de la phase 1, **avant** d'écrire les phases 3 à 5 |

## Ordre de livraison

Les phases 1 et 2 sont livrables ensemble et forment un serveur strictement
équivalent à celui d'aujourd'hui, en HTTP. **La vérification manuelle de la
phase 1 est un point de non-retour** : si un agent en sandbox n'atteint pas le
serveur, tout le reste attend, parce que c'est le seul motif de l'entrée.

Les phases 3 à 5 sont un second lot ; 6 et 7 un troisième.
