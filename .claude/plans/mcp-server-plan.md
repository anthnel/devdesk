# §3.38 — Un serveur MCP en lecture seule

Plan d'implémentation. Les décisions déjà arrêtées sont dans `docs/backlog.md`
§3.38 ; ce fichier ne les répète pas, il tranche ce qui restait ouvert et
découpe le travail.

## Les trois questions ouvertes, tranchées

### 1. Transport — **stdio seul, comme prévu**

Décidé par l'utilisateur le 2026-08-23. Pas de HTTP en v1, donc pas de token,
pas de bind, pas d'agent en conteneur. Le transport reste derrière l'interface
du SDK pour que ça reste un flag plus tard.

### 2. Resources et prompts MCP — **outils seuls en v1**

Un résultat de scan *est* adressable et immuable, l'argument tenait. Mais une
*resource* se lit par URI après énumération : elle ne se filtre pas et ne se
pagine pas, et c'est précisément ce dont `scan_result` a besoin — un scan
d'image produit des milliers de findings. Exposer les deux ferait deux chemins
vers la même donnée, dont un inutilisable sur les gros scans.

Un *prompt* qui embarquerait la doctrine du dépôt — UNKNOWN n'est pas pire que
CRITICAL, un `Sensitive` nil veut dire que personne n'a regardé — est
séduisant, et c'est de la documentation dans un emballage protocolaire. Ce
serait la seule partie du serveur à pouvoir devenir fausse sans que rien
n'échoue.

### 3. SDK — **`github.com/modelcontextprotocol/go-sdk` v1.7.0**

Mesuré le 2026-08-23 : **+2,31 Mo** pour le SDK seul (24,98 → 27,29 Mo), et non
les +6,34 Mo qu'un serveur isolé coûte — DevDesk lie déjà `oauth2`, `x/sync`,
`x/time`, `net/http` et `encoding/json`, que le SDK réclame. Go 1.25.0 minimum,
le dépôt est sur 1.25.5.

Retenu pour la garantie de compatibilité v1 : c'est le seul des deux à en
porter une, et la table des révisions de spec est déclarée par le SDK plutôt
que suivie à la main.

## Deux corrections à l'énoncé

### `redact_secret_matches` disparaît

L'entrée le déclarait comme réglage à `true` par défaut, et classait par
ailleurs le `Match` d'un finding de secret en **non retenu** — « la chaîne :
jamais ». Les deux ne tiennent pas ensemble : un réglage dont l'autre valeur est
refusée est un paramètre qu'il faut ignorer, ce que §3.39 vient de retirer
ailleurs pour cette raison exacte.

Il y a pire. `false` est la valeur zéro d'un `bool`, donc **tout fichier écrit
avant l'arrivée de la clé décoderait à « ne pas caviarder »** — la forme exacte
de D12, où `auth_enabled` absent envoyait des identifiants.

Donc : pas de champ, pas de réglage. Le schéma des outils n'a aucun endroit où
porter la chaîne, ce qui est la garantie que `context_get` obtient déjà par
construction (§3.9).

```yaml
mcp:
  enabled: false
  expose: []
```

### Rien n'écrit sur stdout sauf le protocole

Contrainte que l'énoncé ne nomme pas et qui casse tout si elle tombe : en stdio,
**stdout est le canal**. Un `fmt.Println` sur un chemin atteint par `dk mcp`
corrompt la trame, et le client rapporte une erreur de parsing JSON qui ne
désigne rien.

`main()` en fait aujourd'hui quatre — chargement de config, répertoire, fichier
de log, erreur fatale. Le chemin `dk mcp` doit donc écrire ses diagnostics sur
**stderr** et rien d'autre, refus inclus. Un test de source, à la manière de
`TestNoViewStylesItsOwnFooterMessage`, refuse `fmt.Print*` et `os.Stdout` dans
`internal/mcp`.

## Découpage

Chaque étape est un commit et une PR.

| # | Contenu | Sort |
|---|---|---|
| 1 | La sous-commande, le réglage, le squelette du serveur, la table d'outils déclarés + son test de contrat, et `context_list` pour prouver le câblage | **faite** |
| 2 | `scan_inventory`, `scan_result` — et la lecture qui **ne met pas à niveau** le cache | **faite** |
| 3 | `context_get`, `workspaces_list` | **faite** |
| 4 | `registries_list` — cache seul, jamais le réseau. **`registry_tags` abandonné**, voir le journal | **faite** |
| 5 | `containers_list`, `images_list`, `ports_list` | |
| 6 | `net_check` | |
| 7 | Le sixième onglet de la vue configuration, `.claude/CLAUDE.md`, `docs/backlog.md` | |

### Étape 1 en détail

- `main.go` lit `os.Args` avant tout le reste. Aucun framework CLI : une
  sous-commande, deux flags.
- `dk mcp [--context <name>]` — sans le flag, le contexte courant, résolu une
  fois au démarrage. Le process en sert un seul, pour la vie du process.
- `mcp.enabled: false` → refus sur **stderr** nommant le réglage *et* le
  contexte, sortie non nulle. Nommer le contexte est ce qui évite l'heure perdue
  à activer le réglage dans le mauvais.
- `internal/mcp` : `Serve(ctx, cfg, contextName) error`, une table
  `[]toolDef{Name, Description, Handler}` et `expose` appliqué comme
  **allow-list** au moment de l'enregistrement — un outil non listé n'existe
  pas, plutôt qu'il refuse.
- `TestEveryDeclaredToolHasAHandler` et sa réciproque, dans l'esprit de
  `keymap` et de `AllViewNames()`.

### L'exception de l'étape 2

`readScanCacheFile` met à niveau en place un cache antérieur aux contextes dès
la première ouverture, et attribue les entrées héritées au contexte courant. Un
serveur ouvert sur X réclamerait donc pour X des entrées que personne ne lui a
données — un chemin de lecture qui écrit, et qui décide.

Le cache d'images n'a plus le problème depuis §3.39 : il replie, il n'attribue
pas. C'est **`WorkspaceScanCache` seul** qui a besoin d'une ouverture en lecture
pure. Un test vérifie qu'un fichier hérité est servi tel quel et que son mtime
n'a pas bougé.

## Journal

### Étape 1 — faite le 2026-08-23

Coût réel, étape 1 comprise : **+2,76 Mo** (24,98 → 27,74 Mo).

Trois choses se sont décidées en écrivant :

- **`io.EOF` n'est pas un échec.** Un serveur stdio se termine quand le client
  ferme le tube ; le rapporter avec un code de sortie non nul donne à certains
  clients de quoi afficher un plantage pour un serveur qui a marché. Le
  sentinelle est comparée plutôt que le message, parce que l'`ErrServerClosing`
  du SDK vit dans un paquet `internal` et n'est pas atteignable.
- **Un nom inconnu dans `expose` est refusé.** Une coquille dans une allow-list
  expose *moins* que demandé et rien n'échoue — tout marche, en silence, avec un
  outil en moins. C'est la panne que personne ne remarque.
- **La table d'outils porte une closure, pas un handler.** `sdk.AddTool` est
  générique sur les types d'entrée et de sortie, donc une table homogène ne peut
  pas tenir des handlers qui diffèrent des deux côtés. Le test de contrat fait
  donc un aller-retour client/serveur en mémoire plutôt que de lire la table :
  c'est la seule façon de voir une closure qui enregistre sous un autre nom, ou
  deux fois, ou pas du tout. Ce harnais (`connect`, `callTool`) sert les six
  étapes suivantes.

### Étape 2 — faite le 2026-08-23

**L'exception était plus large que prévu : les deux caches écrivent à l'ouverture,
et un troisième chemin aussi.**

Le plan ne visait que `readScanCacheFile` et son attribution. En écrivant, trois
écritures sont apparues sur ce qui devait être un chemin de lecture :

1. l'attribution des entrées héritées au contexte ouvrant — celle qui était
   prévue, et la seule qui *décide* quelque chose ;
2. le repli de §3.39, réécrit au premier open d'un cache d'images. Il ne décide
   rien de nuisible — il est déterministe — mais c'est un second process qui
   écrit un fichier sans verrou au-dessus ;
3. le `MkdirAll` que font les deux constructeurs **et** les deux
   `Load*ScanResult`, qui résolvent leur chemin par un helper créant le
   répertoire de résultats. Lire un résultat absent créait le répertoire où il
   n'était pas.

D'où `internal/cache/readonly.go`, qui **retourne des maps plutôt qu'un cache**.
Un cache en lecture seule dont le `Set` ne fait rien, ou renvoie une erreur que
personne ne regarde, est un piège posé pour le prochain appelant ; sans cache il
n'y a pas de `Set`, et l'écriture devient inexprimable au lieu d'être interdite
par revue. C'est la forme de Rule 122, une couche plus haut.

`parseScanCacheFile` est la lecture sans la mise à niveau ; `readScanCacheFile`
reste la lecture *avec*, pour le TUI. Un fichier hérité est servi à quel que soit
le contexte qui demande, parce que personne n'a encore décidé à qui il est — et
ce n'est pas ici que ça se décide.

**`scan.Category` est un int sans `String()`.** Une conversion aurait produit une
rune ; `go vet` l'a dit. Les quatre valeurs sont un vocabulaire sur lequel un
agent filtre, donc elles font partie du contrat de l'outil et non d'une énum
interne — `categoryName` les nomme, avec le défaut de `Categorize` : une source
inconnue reste visible dans la famille qu'on regarde en premier.

**Le `Match` ne sort pas parce qu'il n'y a pas de champ pour lui.** `finding` est
une projection qui nomme chacun des champs qu'elle copie, donc un champ ajouté à
`scan.Finding` arrive ici comme un oubli et non comme une fuite.
`TestTheMatchedStringOfASecretNeverLeaves` cherche la chaîne **dans la réponse
sérialisée** et pas un nom de champ : un `Match` recopié un jour dans un `Title`
passerait une vérification de forme. Vérifié en échec en rajoutant le champ.

### Étape 3 — faite le 2026-08-23

Binaire : **27,86 Mo** (+0,12 Mo sur l'étape 2).

**Le lecteur de statut git était déjà dupliqué avant d'arriver ici.**
`workspaces/entry.go` lançait quatre commandes avec son propre `execGit` pendant
qu'`internal/git/sync.go` recomptait la divergence dans `divergence` pour le
sync. Un troisième consommateur rendait la troisième copie évidente, donc
`git.ReadStatus` a été remonté et la vue délègue — un seul corps de fonction
changé, aucun site d'appel touché, `execGit` supprimé.

**Le refactor a introduit une régression, et le test l'a attrapée.** Ma première
version faisait échouer toute la lecture quand `rev-parse --abbrev-ref HEAD`
échouait — ce qui est le cas d'un dépôt fraîchement initialisé, sans commit,
c'est-à-dire précisément l'état où un dépôt n'a **que** des fichiers non suivis.
Il rapportait donc zéro fichier non suivi. Le garde-fou est maintenant
`rev-parse --git-dir`, et la branche est lue par `symbolic-ref` (qui répond pour
un HEAD non né) avec `rev-parse` en repli (qui répond pour un HEAD détaché).
La vue avait le même défaut pendant quelques minutes.

**`workspaces_list` liste des dépôts, pas des répertoires.** C'est la différence
entre un écran qu'on navigue et une réponse à une question : un agent qui demande
ce qui est en local veut les dépôts, à n'importe quelle profondeur, pas un niveau
d'arbre qu'il faudrait ensuite parcourir un appel à la fois. Il ne descend pas
*dans* un dépôt — un `.git` à l'intérieur d'un dépôt est un sous-module ou une
copie vendorée.

**`behind_is_stale` est un champ, pas une ligne de documentation.** D35 dit que
le compteur vient de `@{u}` et que rien ici ne fetch ; un agent lit des champs,
pas des commentaires.

**La garde contre les champs porteurs de secret a un critère de type.** Écrite
sur le nom seul, elle refusait `Scan.Secrets` — un booléen disant si l'étape
tourne. Un secret est une **chaîne** : un `bool` ne peut pas en porter un, donc
le kind fait partie de la règle plutôt que d'une exception à déclarer. Le nom a
été changé quand même (`secret_scanning`), parce que « secrets » sous un bloc
« scan » se lit comme « ce contexte a des secrets ». Vérifiée en échec en
ajoutant un `Token string` à `forgeOut`.

### Étape 4 — faite le 2026-08-23

Binaire : **27,89 Mo**.

#### `registry_tags` n'est pas constructible tel que §3.38 le décrit

L'entrée le voulait « depuis le cache de groupes, jamais le réseau ». Il n'y a
**pas de cache de tags** : le cache de groupes tient les *membres* qu'une
découverte a trouvés, et les tags sont récupérés en HTTP quand le browser
cherche. Rien ne les stocke.

Les chercher ici demanderait un identifiant pour chaque registry que quelqu'un
fait vraiment tourner, et la décision 6 de la même entrée est qu'**aucun outil
ne lit le store de §3.9**. Un listing anonyme répondrait « aucun tag » pour un
registry privé — une absence lue comme un vide, ce qui est la forme de D20 —
donc pire que de ne pas exister.

Ce que DevDesk sait et qu'un agent n'obtient pas à moindre coût, c'est la
**configuration** des registries et ce que la découverte a trouvé. C'est
`registries_list`, et c'est tout ce que cette étape livre.

#### Les registries quittent `context_get`

`context_get` en portait une copie plus mince. Deux outils qui répondent à une
même question, c'est la forme que D12, D24 et D25 ont chacun prise — et un agent
qui a deux sources doit en plus deviner laquelle fait foi. `registries_list` est
la seule.

#### La découverte est un pointeur

`discovery: null` veut dire que personne n'a jamais sondé ce groupe ;
`discovery.members: []` veut dire que quelqu'un a sondé et que ce n'en est pas
un. C'est exactement la distinction que le cache existe pour tenir (§3.8,
décision 3), et elle serait perdue si les deux revenaient en liste vide. Même
discipline que `Sensitive *bool`.

Un groupe que personne n'a sondé n'est **pas sondé ici** : une découverte est un
appel réseau *et* une écriture (elle met son résultat en cache), et ce serveur
ne fait ni l'un ni l'autre.
