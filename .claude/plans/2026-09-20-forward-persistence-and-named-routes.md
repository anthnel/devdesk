# §3.75 + §3.74 — Forwards persistants et routes `*.localhost`

## Contexte

Deux entrées du backlog, dépendantes l'une de l'autre :

- **§3.75** — les forwards ne vivent qu'en mémoire (`internal/forward.Registry`,
  détenu par le routeur) et disparaissent à la fermeture de DevDesk. Tous les
  forwards deviennent persistants, TCP brut compris.
- **§3.74** — un forward peut porter un nom `*.localhost` : un reverse proxy
  HTTP unique route selon l'en-tête `Host`, sur un port unique
  (`network.proxy_port`).

Toutes les décisions sont dans le backlog ; ce plan ne les rouvre pas. Il dit
**dans quel ordre écrire, où, et comment vérifier**.

## Décisions déjà prises (rappel)

| Question | Réponse |
|---|---|
| Portée de la persistance | tous les forwards, pas seulement les nommés |
| Fichier | `~/.devdesk/forwards.yaml`, pas `config.yaml` (un forward n'appartient à aucun contexte) |
| Écriture | depuis `Update()`, dans un `Cmd` qui reçoit une **copie** de la liste (règle 110) |
| Échec à la réouverture | l'entrée reste ; la ligne est « non liée », `Last error` renseigné |
| Désactiver sans supprimer | oui — `space` (Pause/Resume, règle 111), pas de nouvelle lettre majuscule |
| Proxy | un seul listener, un seul port, `network.proxy_port` (8080 par défaut, par contexte) |
| Formulaire | champ `Type` (`TCP` / `HTTP`) en premier ; champs affichés selon le type ; le champ masqué garde sa valeur |
| Nom | pas de complétion du suffixe ; `.localhost` obligatoire et bloquant ; unique ; aide explicite |
| Cibles de conteneurs | abandonnées (`Label` supprimé en #231) — `Target` reste une adresse |

## Livraison en deux PR

Chacune est livrable et testable seule.

1. **PR A — persistance (§3.75, TCP seulement).** Aucun changement de forme du
   formulaire. Un utilisateur qui n'utilise que le TCP voit déjà le gain.
2. **PR B — routes HTTP (§3.74).** Le réglage `proxy_port`, le proxy, le champ
   `Type`, la colonne `Name`.

PR B ne se construit qu'une fois PR A mergée : elle ajoute un *type* d'entrée à
un fichier et à un registre qui doivent déjà exister.

Chaque PR : un worktree neuf **depuis le sandbox** (`git worktree add -b <branche>
.worktrees/<branche> main`), commits dans le sandbox, push / PR / merge depuis
l'hôte (host-relay).

---

## PR A — Persistance des forwards

### A1. Le fichier — `internal/forward/store.go` (nouveau)

```go
// Entry is what forwards.yaml keeps of one forward.
type Entry struct {
    LocalPort int    `yaml:"local_port"`
    Target    string `yaml:"target"`
    Paused    bool   `yaml:"paused,omitempty"`
}

type file struct {
    Version  int     `yaml:"version"`
    Forwards []Entry `yaml:"forwards"`
}
```

- `Load(path) ([]Entry, error)` : un fichier absent est une liste vide, pas une
  erreur. Un YAML illisible est une **erreur nommée** : on ne l'écrase pas (le
  premier `Save` qui suivrait détruirait ce que l'utilisateur peut encore
  réparer à la main).
- `Save(path, []Entry) error` : **écriture atomique** — fichier temporaire dans
  le même dossier, `Sync`, puis `os.Rename`. Le dossier est créé si absent
  (`0o700`).
- `Version: 1` dès le premier jour : PR B ajoute des champs, et un fichier écrit
  par une version future doit être reconnu.
- Le chemin vient de `config.ConfigDir()` ; il est **injecté** dans le registre
  (pas de `os.UserHomeDir()` dans le package), pour que les tests écrivent dans
  `t.TempDir()`.

Tests : aller-retour ; fichier absent ; YAML corrompu (l'erreur nomme le
fichier, il reste intact) ; pas de fichier temporaire laissé après une écriture
qui échoue.

### A2. Le registre — `internal/forward/forward.go`

Le registre passe de « forwards actifs » à « forwards **voulus** », dont certains
sont liés.

- `Forward` gagne `State` :

  | État | Sens |
  |---|---|
  | `StateLive` | listener lié |
  | `StatePaused` | désactivé par l'utilisateur, listener fermé, entrée gardée |
  | `StateUnbound` | voulu mais non lié (port pris, cible arrêtée à la réouverture), `LastErr` renseigné |

- **Création (`Open`) : comportement inchangé.** Sonde de la cible, bind, et un
  échec ne crée **aucune ligne** — le formulaire reste ouvert et le footer nomme
  le refus. Seule la *réouverture au démarrage* garde une entrée en échec : une
  faute de frappe à la création doit être refusée, une cible pas encore
  démarrée au lancement ne doit pas effacer la route.
- `Restore(entries []Entry)` : pour chaque entrée, tente le bind ; `Paused` →
  `StatePaused` sans tenter ; échec → `StateUnbound`. Retourne un résumé
  `Restored{Live, Paused, Unbound int}` pour le footer.
- `Toggle(id)` : `Live` → `Paused` (ferme le listener) ; `Paused` ou `Unbound` →
  retente le bind (sonde puis listen). Un échec laisse `StateUnbound` avec
  `LastErr`, sans erreur remontée — la ligne le dit.
- `Close(id)` supprime définitivement (entrée retirée du fichier au `Save`
  suivant).
- `Entries() []Entry` : la vue persistable, dans l'ordre d'ouverture — c'est ce
  que le routeur copie pour le `Cmd` d'écriture. Le registre ne sait pas
  écrire, il ne fait que décrire.

Le mutex existant couvre les nouveaux champs ; le verrou n'est toujours jamais
tenu pendant une E/S (sonde, bind).

### A3. Le routeur — `internal/app/forward.go`

- **Au démarrage**, là où `forward.New()` est créé (`app.go:210`) : un `Cmd`
  `restoreForwardsCmd()` — lit le fichier, appelle `Restore`, renvoie
  `forward.RestoredMsg{Summary, Err}`. La lecture et les binds sont de l'E/S :
  hors `Update`.
- `handleForwardRestored` : diffuse le snapshot, puis un footer
  `Restored N forwards` (`Info`) ou, s'il y a des échecs, `N forwards could not
  be bound — see the Forward tab` (`Warn`). Un fichier illisible : `Error`, et
  le fichier n'est pas touché.
- **Sauvegarde** : après `handleForwardOpened` (succès), `handleForwardClose`
  et le nouveau `handleForwardToggle`, un `saveForwardsCmd(entries)` reçoit
  `Registry.Entries()` **copié dans `Update`** et écrit. Il renvoie
  `ForwardsSavedMsg{Err}` ; un échec est loggé (`log.Printf("ERROR
  [app/forward] save: %v", err)`) et affiché en `Error` — perdre une écriture en
  silence est exactement ce que la persistance doit ne pas faire.
- Écritures qui se croisent : deux `Cmd` de sauvegarde peuvent partir à la suite ;
  la **dernière** liste doit gagner. Un compteur d'époque (comme `mcpEpoch`) sur
  `saveForwardsCmd`, ou une écriture sérialisée par un mutex du store — je
  prends le mutex : c'est plus simple et l'écriture est atomique.

### A4. La vue — `internal/ui/netdiag/forward_model.go`

- `space` = Pause/Resume. Dans `GetShortcuts()` : entrée présente, `Disabled`
  quand aucune ligne n'est sélectionnée (règle 130), avec une raison au
  handler (`reasonNoForwardSelected`, constante nommée).
- La colonne `Live` affiche `paused` (`DimStyle`) ou `-` pour une ligne non
  liée ; `Last error` porte la raison. Une ligne en pause ou non liée est en
  `DimStyle` (règle 122 : couleur via `Style`, jamais dans `Cell`).
- `GetHeaderInfo` : le décompte distingue `live / total` quand une ligne n'est
  pas liée.
- `GetHelpContent` (règle 114) : dit que les forwards sont enregistrés dans
  `~/.devdesk/forwards.yaml`, rouverts au lancement, et ce que `space` fait.
- `K` supprime la ligne **et** son entrée ; la confirmation existante l'annonce
  (« Delete this forward? It will not be restored at the next launch »).

### A5. Docs — `docs/architecture/network.md`

La phrase actuelle (« gone on exit, because a listener cannot outlive its
process ») devient fausse : la réécrire — le *listener* meurt avec le process,
l'*intention* est enregistrée et rouverte. Décrire `State`, le fichier, la
réouverture, l'écriture atomique, et le risque des deux instances. §3.75 passe
à **done** dans le backlog quand PR A est mergée (le statut « non engagé » ne
vaut plus).

### A6. Tests (PR A)

- `store_test.go` (ci-dessus).
- `forward_test.go` : `Restore` — un port libre est `Live`, un port pris est
  `Unbound` avec `LastErr` et **l'entrée reste** dans `Entries()` ; `Paused`
  n'ouvre rien ; `Toggle` pause puis reprend sur le même port ; `Toggle` sur une
  ligne non liée dont le port s'est libéré la lie ; `Close` retire l'entrée.
- `app` : `restoreForwardsCmd` avec un store dans `t.TempDir()` ; `Open` réussi
  écrit ; échec d'écriture → `Error` au footer. Les tests qui inspectent un `Cmd`
  portant le timer du footer utilisent `testutil.FastTimers`.
- `netdiag` : `space` sur une ligne `Live`/`Paused`/`Unbound` ; le jeu de touches
  ne change pas d'un état à l'autre (`testutil.ShortcutKeys`).

---

## PR B — Routes HTTP `*.localhost`

### B0. À mesurer avant d'écrire du code

Le backlog le dit : **un navigateur sous Windows et macOS** n'a pas été mesuré.
Ouvrir `http://foo.localhost:PORT` dans Chrome, Edge, Firefox (Windows) et
Safari (macOS) contre un serveur d'essai. Ce n'est faisable que sur l'hôte, pas
dans le sandbox. **Si Windows échoue, la feature est remise en cause** — on
s'arrête et on rouvre §3.74 au lieu de construire.

Deuxième mesure, dans la même séance : les navigateurs essaient souvent `::1`
avant `127.0.0.1` pour `*.localhost`. Le proxy ne lie que `127.0.0.1` (règle du
package : loopback IPv4 seulement). Vérifier que le repli sur IPv4 est
immédiat ; sinon décider de lier aussi `[::1]`.

### B1. Config — `network.proxy_port`

- `NetworkConfig.ProxyPort int \`yaml:"proxy_port"\`` ; `DefaultProxyPort = 8080`
  ; posé dans le même bloc de défauts que `PortsRefreshInterval`
  (`config.go:514`) et dans la config par défaut (`config.go:591`), pour qu'un
  fichier antérieur se comporte comme avant.
- Bornes 1024–65535 à la lecture ; une valeur hors bornes est ramenée au défaut
  et le fait est loggé.
- Vue de configuration (`internal/ui/configuration/fields.go`, groupe réseau) :
  `integer("Proxy port", …, 1024, 65535, "Port shared by every named route, e.g.
  http://api.localhost:8080")`.
- `docs/architecture/configuration.md` : une ligne sur le champ.

### B2. Le store — nouveaux champs

`Entry` gagne `Name string \`yaml:"name,omitempty"\`` — **le type se déduit du
nom** dans le fichier (le formulaire, lui, a un champ `Type`). Une entrée avec
`Name` n'a pas de `LocalPort` (omis). `Version` reste 1 : les champs sont
additifs et omis quand vides, donc un fichier PR A se lit sans migration.

### B3. Le proxy — `internal/forward/proxy.go` (nouveau)

- Un `http.Server` unique sur `127.0.0.1:<proxy_port>`, détenu par le registre.
  Il est **lié à la première route active et fermé avec la dernière**.
- Table `host → route` protégée par le même mutex que les entrées. Le nom du
  `Host` est mis en minuscules, le port et un point final retirés avant la
  recherche.
- Handler :
  - route trouvée → `httputil.ReverseProxy` avec `Rewrite` vers
    `http://<Target>` (le `Upgrade` WebSocket est géré par la bibliothèque, ce
    qui compte pour le rechargement à chaud) ;
  - `Host` inconnu ou `localhost` seul → **404** avec une page texte qui liste
    les routes connues ;
  - cible qui ne répond pas → **502** avec un texte lisible (`ErrorHandler`), et
    `LastErr` de la route renseigné.
- Compteurs par route : `Total` = requêtes, `Active` = requêtes en cours
  (un WebSocket ouvert compte tant qu'il dure).
- **Aucun en-tête CORS n'est ajouté.** Le proxy ne rend joignable rien qui ne
  le soit déjà sur la boucle locale.
- `SetProxyPort(port)` : ferme puis relie ; un échec de bind rend **toutes** les
  routes `StateUnbound` avec la raison (décision 1 du backlog). Un `Local port`
  TCP égal au port du proxy est refusé (`ErrProxyPortTaken`) à la création et
  à la reprise d'un forward.

`Registry.OpenRoute(name, target)` : valide le nom (`.localhost` obligatoire,
caractères d'un nom d'hôte, unicité insensible à la casse), sonde la cible,
enregistre la route, lie le proxy si nécessaire. Sentinelles nouvelles
(`ErrBadRouteName`, `ErrRouteNameTaken`, `ErrProxyPortTaken`) ; le libellé reste
dans `forwardRefusal` (règle 129).

### B4. Le routeur

- `forward.OpenMsg` gagne `Name string` ; `Name != ""` ⇒ route. (Le message ne
  porte pas de `Type` : le type est le fait du formulaire, la requête le dit par
  le nom.)
- **Changement de contexte** : si `network.proxy_port` change, un `Cmd` appelle
  `SetProxyPort`. Il porte une **époque** (`forwardProxyEpoch`, sur le modèle de
  `mcpEpoch`) : deux changements rapides ne doivent pas laisser un listener périmé
  lié sur un port que plus personne n'attend. Les forwards TCP ne bougent pas.
- Le routeur passe le port courant au démarrage (`Restore`) et à chaque
  `reinitializeViews` qui change la config.

### B5. Le formulaire — `internal/ui/netdiag/forward_form.go`

- Champs : `Type` (cycle `←→`, règle 132, `TCP` par défaut), `Local port`,
  `Name`, `Target`. `↑↓` saute ceux qui ne s'appliquent pas au type ; changer de
  type replace le focus sur un champ affiché.
- Le champ masqué **garde sa valeur** tant que le formulaire est ouvert ; seul
  le type courant est soumis.
- Validation **bloquante** dans le formulaire, seulement pour ce qui se décide
  sans E/S : en `HTTP`, un `Name` vide ou qui ne se termine pas par `.localhost`
  est refusé, avec un `Warn` au footer. Le reste (unicité, port du proxy, sonde)
  vient du registre, comme aujourd'hui — pas de seconde copie des règles
  (commentaire existant sur `ForwardForm`).
- `forwardLabelWidth` recalculé : `Type` et `Local port` ne changent rien, mais
  `Name` est plus long que `Target` ; l'alignement des chevrons reste la seule
  contrainte.
- Aide (`GetHelpContent`) : décrit chaque type — nom obligatoirement en
  `.localhost`, port du proxy = `network.proxy_port`, URL à utiliser
  (`http://api.localhost:PORT`), TCP ouvre un port local vers une adresse
  quelconque. Rien de cela dans le viewport (règle 134).

### B6. La table

- Colonne `Name`, `Sizing: SizingContent`, `Optional: true`, vide en `DimStyle`
  pour un TCP (règle 122). Pas de colonne `Type`.
- Une route affiche `network.proxy_port` dans `Local` ; `K` ne supprime qu'une
  route ; `space` met une route en pause (elle sort de la table de routage, le
  proxy se ferme avec la dernière route active).

### B7. Docs et backlog

- `docs/architecture/network.md` : une section « Named routes », ce que le
  proxy ne fait pas (HTTPS, noms hors `.localhost`, clients au résolveur
  maison — les limites du backlog).
- §3.74 et §3.75 passent à **done** ; les limites restent écrites.

### B8. Tests (PR B)

- `proxy_test.go` : deux routes sur un même port, chacune atteint sa cible
  d'essai selon le `Host` ; `Host` inconnu → 404 avec la liste ; cible arrêtée →
  502 ; un WebSocket traverse ; le port est retiré du `Host` ; comparaison
  insensible à la casse.
- `forward_test.go` : nom sans `.localhost` refusé ; doublon refusé ; `Local
  port` = port du proxy refusé ; la dernière route fermée libère le port ;
  `SetProxyPort` vers un port pris rend les routes `Unbound` sans supprimer
  d'entrée.
- `config_test.go` : défaut 8080, valeur hors bornes ramenée au défaut.
- Formulaire : `Type` change les champs affichés ; `↑↓` saute ; un nom saisi est
  retrouvé après un aller-retour TCP → HTTP ; nom sans suffixe bloque avec un
  `Warn` ; `TestEveryLowercaseBindingIsDeclared` et
  `TestNoViewBindsAnUndeclaredUppercaseKey` restent verts (aucune touche
  nouvelle).

---

## Vérification (chaque PR)

Règle 301 : `mise run check` (fmt, vet, lint, test) ; `mise run test-race` —
le proxy et le registre partagent un mutex entre goroutines d'accept et
`Update`, c'est là que la course se verrait.

Sur l'hôte, à la main (le sandbox n'a ni terminal réel ni navigateur) :
1. Ouvrir un forward TCP, quitter, relancer : la ligne revient `Live`.
2. Occuper le port avec un autre processus, relancer : la ligne revient `non
   liée` avec `Last error`, l'entrée est toujours dans le fichier.
3. `space` met en pause, relancer : la ligne reste en pause.
4. (PR B) Deux routes, `http://api.localhost:8080` et `http://app.localhost:8080`
   dans un navigateur, un rechargement à chaud à travers le proxy.
5. (PR B) Changer `proxy_port` dans un autre contexte et y basculer : les routes
   sont resservies sur le nouveau port.

## Risques

| Risque | Parade |
|---|---|
| Deux instances écrivent `forwards.yaml` : la seconde écrase la première, et sa réouverture échoue sur des ports déjà pris | écriture atomique ; l'état `Unbound` absorbe l'échec sans rien perdre ; pas de verrou de fichier en v1 (à reprendre si l'usage l'exige) |
| Fichier corrompu écrasé par la première sauvegarde | `Load` refuse et **ne réécrit pas** tant qu'il n'a pas lu |
| `*.localhost` non résolu dans un navigateur | mesure B0, avant tout code de PR B |
| Basculer de contexte change le port du proxy sous les pieds de l'utilisateur | dit au footer (`Info`) ; les routes ne sont pas perdues |
| Un port par défaut (8080) déjà occupé | visible : routes `Unbound` avec la raison ; réglable dans la vue de configuration |

## Hors périmètre

HTTPS et certificats locaux ; noms hors `.localhost` ; un serveur DNS ; le
verrouillage de fichier entre instances ; le pré-remplissage depuis un
conteneur (supprimé avec `Label`).
