# §3.1 — Redirecteur de ports non privilégié (4e onglet de `:net`)

## Contexte

§3.1 du backlog tient en une ligne depuis `todo.md` : « *Port-forwarding
manager — an interactive dashboard to manage port redirections to the host
machine* ». L'analyse qui précède ce plan a tranché deux choses :

1. **Les alias de noms via `/etc/hosts` sont abandonnés.** Ils demandent une
   élévation sur les trois plateformes (UAC sous Windows même pour un compte
   administrateur), trois mécanismes distincts, et une réinvocation auto-élevée
   qui est une surface d'élévation de privilèges à sécuriser. Cela irait aussi à
   l'encontre de §3.43/§3.44, qui ont retiré le dernier conteneur privilégié de
   l'application.
2. **Le redirecteur, lui, ne demande aucun droit.** C'est un `net.Listen` + deux
   `io.Copy` dans le process DevDesk. Mesuré dans le sandbox Linux : bind refusé
   sur 80 et 443 en non-root, accepté sur 8080. Windows n'a pas de plage
   réservée. Se limiter aux ports ≥ 1024 rend la fonctionnalité portable sans
   privilège.

Résultat visé : depuis `:net`, ouvrir un port local vers un `host:port`
quelconque — avec pré-remplissage depuis un conteneur en cours d'exécution et
ses ports **non publiés**, qui est le cas d'usage qui a le plus de valeur
(joindre un service sans redémarrer le conteneur).

## Décisions déjà prises

| Question | Réponse |
|---|---|
| Cible | Proxy TCP générique + pré-remplissage depuis un conteneur |
| Persistance | **Session seulement** — rien dans le YAML, aucune migration |
| Emplacement | **4e onglet de `:net`**, à côté de Diagnostics / Ports / Interfaces |

La persistance session-seulement n'est pas qu'une simplification : un listener
ne peut pas survivre au process qui le détient, donc une liste persistée
promettrait ce qu'elle ne peut pas tenir.

## Le risque principal — à mesurer AVANT d'écrire le pré-remplissage conteneur

**C'est une rechute possible de D55.** `docs/architecture/network.md:181-191`
raconte que l'onglet Ports listait les sockets de la VM Docker Desktop en
croyant parler de l'hôte, et que `K` tuait un process de la VM. Le mécanisme
est le même ici : sous Docker Desktop (Windows, macOS), **l'IP d'un conteneur
n'est pas routable depuis l'hôte** — elle l'est sous Docker natif Linux. Un
redirecteur qui composerait `172.17.0.2:80` marcherait sur la machine du
développeur Linux et échouerait sur celle de l'utilisateur.

Deux mesures à faire sur l'**hôte Windows**, avant l'étape 3 :

1. Lancer un conteneur avec un port exposé non publié, lire son IP
   (`docker inspect`), et tenter un `Test-NetConnection <ip> -Port <p>` depuis
   l'hôte. C'est la mesure qui décide.
2. Vérifier que `docker inspect` et `podman inspect` s'accordent sur
   `.NetworkSettings.Networks.*.IPAddress` — même classe de divergence que les
   trois gabarits trouvés en §3.67.

**Ce que le plan fait de ce risque, quelle qu'en soit l'issue** : le
redirecteur **sonde** la cible avant d'ouvrir le listener, et rapporte l'échec
en nommant la cause probable. Il ne détecte pas la plateforme et ne devine
rien — c'est la philosophie de `netcheck` (mesurer, pas supposer), et c'est ce
qui empêche un « ça marche chez moi » silencieux.

## Architecture

### Qui détient les listeners — le routeur, jamais la vue

`internal/app/app.go:343` : `reinitializeViews` fait
`a.views = make(map[...])` à chaque sauvegarde de config et à chaque changement
de contexte. **Un listener détenu par le modèle netdiag fuirait à ce
moment-là**, sans que rien ne le signale.

Le précédent exact est le serveur MCP (`internal/app/mcp.go`) : `net.Listen`
dans un `Cmd` (l'I/O appartient au `Cmd`, Rule 110), le handle revient **sur un
message**, et `Update` est le seul à le stocker. `sharedState` est créé une fois
dans `newWithSize` (`app.go:205`) et muté sur place — il survit donc aux
reconstructions de vues.

Le modèle à suivre est celui de `internal/jobs`, qui est le plus proche :
le routeur détient le registre (`a.jobs = jobs.New()`, `app.go:216`), les vues
ne le reçoivent **pas** ; elles émettent un message de demande et reçoivent un
**instantané** en retour (`jobs.StartMsg` → `handleStartJobs` →
`jobs.ChangedMsg`, `internal/app/jobs.go:18-58`). Les types de messages vivent
dans le paquet du domaine, pas dans `internal/app`, pour que les vues puissent
les importer sans importer le routeur.

### Nouveau paquet `internal/forward`

Aucune dépendance à `internal/docker` ni à l'UI — il ne connaît que des
`host:port`. C'est ce qui le rend testable sans conteneur.

```go
// Forward is one live redirection, as the table reads it.
type Forward struct {
    ID        string    // stable across snapshots; the datatable Key
    LocalPort int
    Target    string    // host:port
    Label     string    // where it came from: a container name, or empty
    Opened    time.Time
    Active    int       // connections currently proxied
    Total     int64     // connections accepted since it opened
    LastErr   string    // last dial failure, empty when healthy
}

type Registry struct { /* mu sync.Mutex; entries map[string]*entry */ }

func New() *Registry
func (r *Registry) Open(localPort int, target, label string) (Forward, error)
func (r *Registry) Close(id string) error
func (r *Registry) CloseAll()
func (r *Registry) List() []Forward   // sorted snapshot, safe to hand to a view
```

Quatre décisions à écrire dans le code, chacune avec son test :

- **`Open` écoute sur `127.0.0.1` uniquement, jamais `0.0.0.0`.** Publier sur
  toutes les interfaces exposerait au LAN un service que le conteneur avait
  justement gardé pour lui — c'est exactement ce que §3.64 cherche à repérer, et
  le faire ici par défaut serait contradictoire. Pas d'option pour l'instant.
- **Les ports < 1024 sont refusés avant l'appel système**, avec une raison
  nommée, plutôt que de laisser remonter un `bind: permission denied` qui ne dit
  pas que la limite est structurelle et sans contournement non privilégié.
- **La cible est sondée avant que le listener n'ouvre.** Un `net.DialTimeout`
  court : ça échoue tout de suite et clairement, plutôt qu'à la première
  connexion, une fois que l'utilisateur a déjà pointé son client dessus. C'est
  aussi ce qui attrape le cas Docker Desktop décrit plus haut.
- **Les compteurs sont atomiques, et le registre a un mutex** — contrairement à
  `internal/jobs`, qui n'en a délibérément pas (`registry.go:18-25` : « *a mutex
  here would say a Cmd may touch it, and that is exactly the thing that must
  stay untrue* »). La différence est réelle et doit être écrite, sinon elle se
  lit comme une incohérence : `jobs` n'est muté que depuis `Update`, alors que
  les goroutines d'accept de ce registre-ci écrivent vraiment dans les
  compteurs. `mise run test-race` est la vérification qui compte.

Boucle d'accept dans une goroutine, une goroutine par connexion, deux `io.Copy`
à contre-sens, `ln.Close()` termine l'accept (l'erreur attendue est
`net.ErrClosed` — même distinction que `http.ErrServerClosed` dans
`mcp.go:107`).

### Câblage routeur

| | |
|---|---|
| `internal/shared/state.go` | + `Forwards *forward.Registry` (créé une fois, comme `jobs`) |
| `internal/app/app.go` | construire le registre dans `newWithSize` |
| `internal/app/forward.go` (nouveau) | `handleForwardOpen` / `handleForwardClose` / `forwardsChanged()`, calqués sur `internal/app/jobs.go` |
| `internal/app/app.go` `Update` | router `forward.OpenMsg` et `forward.CloseMsg` |
| `internal/app/command_line.go:186` | `netdiag.New(a.config)` reçoit le registre, ou plutôt : la vue n'a besoin de rien — elle reçoit `forward.ChangedMsg` |

**Les redirections survivent au changement de contexte** — contrairement au
serveur MCP, qui redémarre parce qu'il répond *pour* un contexte. Une
redirection est un port local vers un `host:port` ; elle n'appartient à aucun
contexte. À écrire comme une décision, pas la subir.

Rien à faire à la fermeture : `tea.Quit` sort du process et l'OS reprend les
ports (`keys.go:42`, le serveur MCP ne fait rien de plus). Il n'existe
d'ailleurs aucun hook de sortie dans l'application — en créer un pour ça serait
une première, et rien ici ne le justifie.

**Comment la table se rafraîchit.** Un tick, comme l'onglet Ports
(`ports_model.go:48-60`) : `Update` lit `Registry.List()`, qui rend un
instantané. Les compteurs n'ont pas besoin d'une fidélité à la connexion près.
Si un jour ils en avaient besoin, le modèle est la pipeline de clone
(`internal/ui/forge/explorer/pipeline.go:92-212`) — canal bufferisé plus un
`Cmd` de lecture réémis à chaque événement — et non `tea.Program.Send`.

**Les forwards ne sont pas des `jobs`.** La tentation existe (`NewOpenRun` +
`Discover` + `Seal` donneraient la table, le spinner et `K` gratuitement), et
elle est à écarter : un job est un travail qui finit, une redirection est un
état qui dure. `Kind.Cancellable()` (`jobs.go:63`) ne liste que scan et pull, et
`Kinds()` est parcouru par un test qui échoue pour un `Kind` sans icône ni
verbe — le coût serait réel pour un mauvais ajustement sémantique.

### L'onglet

`internal/ui/netdiag/forward_model.go` + `forward_view.go`, sur le modèle exact
de `ports_model.go` — une `datatable.Model[forward.Forward]`, pas de
`FilterBar` locale (elle appartient au datatable via `Config.Tokens`).

Colonnes (chacune déclare son `Sizing`, Rule 116/§3.45) :

| Titre | Sizing | Note |
|---|---|---|
| `Local` | Fixed | le port local |
| `Target` | Content | `host:port` |
| `Source` | Content | le nom du conteneur, ou `-` en `DimStyle` |
| `Conns` | Fixed | actives / total ; un `0` en `DimStyle` (Rule 122) |
| `Age` | Fixed | `theme.TimeAgo(f.Opened)` (Rule 127) |
| `State` | Fixed | `StatusColumn` — la cellule que le spinner occupe |

Touches — **aucune lettre nouvelle n'est nécessaire**, ce qui est le bon signe :

- `N` (`keymap.New`, « Create a resource from this context ») ouvre le
  formulaire. **`netdiag` lie déjà `N`** — « New diagnostic », `update.go:278-287`
  — et ce n'est pas une collision : c'est le même sens sur un autre onglet, ce
  qui est précisément ce que Rule 111 attend d'une lettre majuscule.
- `K` (`keymap.Kill`, « Stop, kill ») ferme la redirection sélectionnée, via
  `components.ConfirmModal` comme le kill de l'onglet Ports.
- `/` cherche. **Pas de toggle minuscule** au premier jet : `localToggles`
  (`internal/ui/keymap/keymap.go:206-217`) fait correspondre les fichiers par
  préfixe, et `ui/netdiag/` n'autorise que `p` — un `forward_model.go` qui
  binderait une minuscule devrait donc déclarer sa propre surface. Inutile pour
  une table de quelques lignes.

Ajouter un 4e onglet demande de toucher **trois** endroits, et le `% 3` est le
piège :

- `model.go:31-35` — `tabForward = 3`, et y ajouter un `tabCount = 4` plutôt que
  de laisser deux littéraux `3` se balader.
- `update.go:153-160` — les deux `% 3` deviennent `% tabCount`.
- `header.go:204-209` — la 4e `theme.TabItem{Label: "Forward"}`.

Puis les branches par onglet, toutes dans les mêmes fonctions que Ports :
`View` (`view.go:19-34`), `GetShortcuts` / `GetHeaderInfo` / `GetFooterHeight` /
`RenderFooter` / `activeFooter` (`header.go`), `InEditMode` (`update.go:22-39`),
`resize` et le routage des messages (`update.go:59`, `:71-75`, `:163-167`).

`GetHelpContent` (`header.go:242`) gagne sa section. **À corriger au
passage** : la `Description` et la première entrée de `KeyBindings` disent
encore « Topology » là où le reste du fichier dit « Interfaces ».

### Le formulaire

`internal/ui/netdiag/forward_form.go`, calqué sur
`internal/ui/oci_resources/launch_form.go` — `textinput` + `theme.StyleTextInput`,
navigation `↑↓` (Rule 103/135), `enter` valide, `esc` annule, une ligne vide en
tête (Rule 131), et **aucune ligne d'aide dans le viewport** (Rule 134).

Trois champs : port local, cible (`host:port`), et un champ conteneur en
cycle `←→` (Rule 132) qui, quand il n'est pas vide, **remplit** la cible plutôt
que de la remplacer par un mode à part — un seul chemin d'exécution, le
conteneur n'est qu'une façon de remplir le formulaire.

Le pré-remplissage lit `docker.ListContainers(false)` et ne propose que les
`PortBinding` de scope `docker.ScopeExposed` — `container_ports.go:16-18` les
définit comme « *a port the image declares and nothing published… there is no
host port to connect to* », c'est-à-dire exactement la population qui a besoin
d'une redirection. L'IP vient de `docker.InspectContainer` (JSON complet,
`containers.go:310`) ; si les deux moteurs divergent sur ce champ (mesure 2
ci-dessus), ça devient un champ de `engine.Templates`.

### Disponibilité (Rule 130)

`internal/ui/netdiag/forward_model.go`, sur le modèle mince de
`ports_model.go:472-493` (constantes nommées + une méthode rendant
`shortcut.Availability`, refus explicite au footer en `Warn`) :

- `reasonNoForwardRow` — `K` sur une table vide.
- `reasonPortPrivileged` — un port < 1024 saisi.
- `reasonPortTaken` — le bind a été refusé ; c'est un `Error` au sens de Rule
  128 (le système a dit non), pas un `Warn`.
- `reasonTargetUnreachable` — la sonde a échoué. Le message nomme la cause
  probable quand la cible est une IP de conteneur.

## Découpage

1. **`internal/forward` seul**, avec ses tests. Aucune UI, aucun docker : un
   listener, un proxy, un registre. C'est la moitié qui porte le risque de race
   et elle se teste entièrement en mémoire.
2. **Le câblage routeur** — `shared.State`, `internal/app/forward.go`, le
   routage des deux messages. Toujours pas d'écran.
3. **L'onglet et le formulaire**, cible saisie à la main. À ce stade la
   fonctionnalité est complète et portable.
4. **Le pré-remplissage conteneur**, après les deux mesures. C'est la seule
   étape qui peut se révéler inutile sous Docker Desktop, et la mettre en
   dernier est ce qui empêche qu'elle entraîne les trois autres.

## Vérification

```bash
go build ./...
go test ./internal/forward/ ./internal/ui/netdiag/ ./internal/app/
mise run test-race          # celui qui compte : goroutines + compteurs
mise run lint
```

Tests à écrire, au-delà de la couverture ordinaire :

- un aller-retour complet sur `127.0.0.1` (echo server en mémoire) ;
- `Close` pendant qu'une connexion est ouverte ;
- le refus d'un port < 1024 **sans** appel système ;
- le refus d'un port déjà pris ;
- `TestShortcutKeys` pour l'onglet : le jeu de touches ne change pas d'un état à
  l'autre du même écran (Rule 130) ;
- Rule 116 : `RenderedWidth() == width-2` sur la nouvelle table.

Bout en bout, **depuis l'hôte** (le sandbox n'est pas représentatif : pas de
vrai terminal, pas le Docker de l'hôte) : lancer un conteneur avec un port
exposé non publié, ouvrir une redirection dessus, et vérifier avec un client.
C'est aussi la mesure 1 du risque D55.

**Ce que l'onglet évite comme travail.** Ce n'est pas une vue de premier
niveau, donc rien à ajouter dans `internal/command/parser.go`, ni de `case`
dans `createView`, ni de puce dans la liste de commandes de
`docs/architecture/app-shell.md` — `TestEveryTypeableViewIsDocumented` ne
s'applique pas. Deux garde-fous s'appliquent quand même :

- `TestEveryAnnouncedActionIsBoundInItsPackage` (`keymap/announced_test.go`) —
  un `keymap.X` annoncé exige un `case keymap.X:` dans le même paquet, et les
  touches s'écrivent `keymap.New` / `keymap.Kill`, jamais `"N"` / `"K"`.
- `viewsWithATableBody` (`internal/ui/components/footer_guard_test.go:85-96`) —
  vérifier que `ui/netdiag/view.go` y figure déjà ; s'il n'y est pas, l'ajouter,
  sinon le nouvel onglet est silencieusement non gardé contre Rule 139.

## Documentation à mettre à jour dans le même commit

- `docs/architecture/network.md` — une section après « The socket table », qui
  dit surtout pourquoi le listener appartient au routeur et pourquoi il écoute
  sur loopback.
- `docs/backlog.md` §3.1 — remplacer la ligne héritée de `todo.md` par l'entrée
  réelle : les alias `hosts` écartés avec leur raison, la contrainte ≥ 1024, et
  ce que la mesure D55 aura donné.
- `.claude/CLAUDE.md` — la ligne « Network diagnostics » de l'aperçu projet.
