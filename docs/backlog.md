# DevDesk Backlog

**Last Updated:** 2026-08-23

Open work for DevDesk: known defects, technical debt, and planned features.
Replaces the former `todo.md` at the repository root. Items completed there
(network topology visualisation, inter-container ping/curl) have been dropped
rather than carried over.

---

## 1. Known defects

**None open.** D39, the last one, was the registry browser addressing a group's
members one way to browse them and another way to pull them; it is closed by
[§3.18](#318-a-registry-member-is-an-address-not-a-url--repo_prefix), which is
what it existed for. D40, found the same day and on the same screen, was the
thing §3.18 blocked on and had already been closed on its own.

D1 through D51 are all fixed or, in D35's case, deliberately
downgraded to a stale reading with a way to refresh it. §1.1 records what each was and why the
chosen fix was the right one — including the three that were answered by
*removing* something rather than making it work: D8's write-only CRUD flags,
D21's unreachable clamp and D36's never-filled cache.

The five that stayed open longest — D4, D8, D9, D10 and D11 — were parked not
because they were hard but because each altered something the user already saw,
so they needed a deliberate call rather than a drive-by fix. All five were then
decided together and fixed in one pass; see [§1.2](#12-the-five-parked-defects).

### 1.1 Fixed

**D39 — every discovered group member browsed, and none of them could be
pulled. Fixed** by §3.18. Found on 2026-08-10 against a real Nexus, closed on
2026-08-23.

`NexusDetector` synthesised each member as `host + "/repository/" + name`, and
two consumers read that one URL without building the same request from it:
`GET /repository/dhi-io-proxy/v2/eclipse-temurin/tags/list` answered **200**,
while the reference Docker builds from the same string —
`/v2/repository/dhi-io-proxy/…` — answered **404**. So the tags table filled
correctly and `G` on any row of it could not work. That was the worse half:
nothing on screen said the reference would not resolve.

**It was never a format string to correct.** Which of four addressing forms
applies is a per-repository Nexus setting (`docker.httpPort`, `docker.httpsPort`,
`docker.subdomain`), and the endpoint carrying it answers 403 to an ordinary pull
account — the same endpoint that would say what a group's members are. So the
addressing is **declared**, on the `provider` precedent: `repo_prefix`, on
`config.RegistryItem` and on `cache.RegistryGroupMember`. The pair
`(url, repo_prefix)` is the address, both directions derive from it, and browse
and pull can no longer mean two different repositories.

It is applied **once**, in `submitSearch` — the prefix is then already part of
`MultiRegistryTag.Repo`, so `multiImageName` and `registryAPIURL` needed no
change. Teaching both of them about it would have been two places free to drift,
which is what the defect already was.

**What the fix cost elsewhere is the interesting part**, and it is D40's
territory: once several entries answer to one host, a URL identifies an instance
rather than an entry. So `entryFor` resolves by entry key, `resultFilter` holds a
key rather than a URL, `MultiRegistryTag` carries one, and `memberKey` takes the
prefix — a member's URL alone no longer tells two members of one group apart. The
Registries tab and the browser's labels show the address rather than the URL for
the same reason: one line per proxy is otherwise one host repeated.

`NexusDetector` no longer synthesises anything. A group written as a path
prefix serves its members the same way, so it emits `(host, memberName)`, which
is consistent by construction; reached through a connector of its own — a port,
a subdomain — it emits the group's URL and **no** prefix, because which connector
a member answers on is exactly what it cannot read. An empty prefix against a
bare host is wrong and visibly so; the synthesised path was wrong and plausible.

**D54 — la vue d'auth proposait un token GitLab en exemple, quelle que soit la
forge. Corrigé.** Signalé le 2026-08-22, juste après D53.

Le placeholder du champ token était `glpat-xxxxxxxxxxxxxxxxxxxx`, écrit en dur
dans `auth/model.go`. Un utilisateur GitHub se voyait donc montrer la forme d'un
token GitLab comme exemple de ce qu'il devait coller.

**Ce qui vaut d'être retenu, c'est pourquoi le garde-fou ne l'a pas vu.**
`internal/ui/vocabtest` cherche les *noms* des plateformes dans les littéraux —
et `glpat-` n'en nomme aucune. Un préfixe de token est spécifique à une forge
**sans dire laquelle**, ce qui est précisément la forme qu'une garde sur les noms
ne peut pas voir. Le test a marché à côté à chaque exécution de §3.6, étapes 5 à
8 comprises.

Les marqueurs incluent maintenant les préfixes (`glpat-`, `ghp_`,
`github_pat_`, `gho_`, `ghs_`) en plus des deux noms, et la garde a été vérifiée
en échec en remettant l'ancienne valeur : elle nomme le fichier, la ligne, le
marqueur et le littéral.

`Vocabulary.TokenPlaceholder` le porte, et sa forme a changé avec son emploi :
c'était `glpat-…` pour une phrase d'aide, c'est un exemple de valeur maintenant,
parce que c'est ce qu'un placeholder est. La prose sur les préfixes vit dans
`TokenHelp`, où il y a la place d'en nommer plusieurs — et l'aide GitHub s'en
sert pour distinguer un token classic d'un fine-grained, ce que D53 a montré
utile : `/user/orgs` ne renvoie rien pour un fine-grained tant que chaque
organisation ne l'a pas approuvé, ce qui se lit « vous n'appartenez à aucune ».

**D53 — l'explorer GitHub s'ouvrait vide sur un compte personnel. Corrigé.**
Signalé le 2026-08-22, quelques minutes après la livraison de §3.6 étape 7, par
la première utilisation réelle avec un token GitHub.

`RootNamespaces` ne listait que les organisations. Un compte GitHub personnel
n'appartient à aucune, donc l'explorer affichait « No organizations found — You
may not have access to any GitHub organizations » : un écran vide, un message
qui suggère un problème de droits, et aucun dépôt visible alors que le token
marchait parfaitement.

**Le raisonnement qui excluait le compte personnel était écrit, argumenté, et
faux sur ses propres termes** — c'est ce qui le rendait convaincant :

> Un namespace personnel n'est pas une organisation : il ne peut être ni créé ni
> supprimé, donc le lister mettrait dans l'arbre une ligne dont la moitié des
> actions se retirent.

Or *aucune* organisation GitHub ne peut être créée ni supprimée par l'API — le
backend refuse déjà les deux pour toutes. Le compte personnel n'est donc pas
moins capable qu'une organisation, il l'est **plus** : c'est le seul namespace
où un dépôt peut être créé et supprimé. L'argument s'appliquait à une différence
qui n'existe pas.

Trois choses tombent du correctif :

- **L'ID du namespace personnel est la chaîne vide.** C'est le mot que
  l'interface emploie déjà — `forge.NewRepository.NamespaceID` documente
  « vide veut dire le namespace de l'utilisateur » — et c'est ce que le `org` de
  `Repositories.Create` attend. Passer le login à la place renverrait 404 :
  GitHub refuse de traiter un utilisateur comme une organisation, ce qui est la
  distinction même que cette ligne porte.
- **Ses enfants viennent de `/user/repos` avec `Affiliation: owner`.** Sans
  l'affiliation, la liste porte aussi tous les dépôts où l'utilisateur est
  collaborateur — qui appartiennent à quelqu'un d'autre, et apparaîtraient deux
  fois dans un arbre qui montre les deux.
- **Il est en tête.** C'est là que sont les dépôts d'un compte solo ; l'enterrer
  sous une liste d'organisations cacherait la seule ligne que la moitié d'entre
  eux possède.

**Les tests existants passaient, et pour la mauvaise raison.** Leur faux serveur
ne répondait pas à `/api/v3/user`, donc `personalNamespace` échouait en silence
et la ligne n'apparaissait pas — un test de pagination qui comptait trois
namespaces en aurait compté quatre. Corriger le faux serveur d'abord, puis
compter, est ce qui a rendu les quatre nouveaux tests capables de mordre ; ils
ont été vérifiés en échec sur le code d'avant.

**Une aspérité assumée :** la colonne Type lit le vocabulaire, donc la ligne du
compte personnel affiche « Organization ». GitHub appelle l'union « Owner »,
mais mettre ce mot dans `Vocabulary.Namespace` ferait lire « Default parent
owner » dans la configuration, ce qui est pire — et un troisième kind de nœud
réintroduirait le type somme que la décision « deux types » existe pour éviter.
Une cellule inexacte vaut mieux que l'un ou l'autre.

**D52 — un compteur du dashboard qui n'a pas pu être lu s'affichait `0`.
Corrigé** par §3.6 étape 3, qui est aussi ce qui l'a trouvé — l'étape 1 a dû
décider ce que `forge.DashboardStats` pouvait promettre, et cinq `int` ne
savaient pas dire « personne n'a regardé ».

`FetchDashboardStats` émettait **cinq requêtes indépendantes** et avalait
l'erreur de chacune : le champ restait à zéro, et `countValue` rendait un `0`
grisé — exactement ce qu'il rend quand il n'y a réellement aucune merge request
assignée. Un token dont le scope ne couvre pas les issues, une instance qui
limite le débit, un endpoint indisponible : les trois donnaient un dashboard
qui disait « rien à faire ». C'était D20 dans cinq champs.

Chaque compteur est un `*int`, `nil` voulant dire que rien n'a compté, et
`maybeCountValue` rend `-`. Le précédent est `Sensitive *bool` des caches de
scan, où la même distinction avait dû être ajoutée après coup.

Deux tests, et le premier a été vérifié en échec sur un `maybeCountValue` qui
rendrait `0` : un compteur lu qui vaut zéro **reste** un `0`, parce que c'est
une réponse ; un compteur que personne n'a pu lire est un `-`.

**D51 — `V` (pager) n'a jamais marché sous Windows : il revenait aussitôt, sans
rien dire. Corrigé.** Signalé le 2026-08-22, juste après D50.

Deux défauts empilés, et c'est l'empilement qui l'a rendu invisible si
longtemps.

**Le premier : la ligne de commande était corrompue.** `PagerCmd` construisait

```
docker logs --tail 500 ID > "%TEMP%\devdesk-logs.txt" 2>&1 && more "%TEMP%\devdesk-logs.txt"
```

et le passait à `exec.Command("cmd", "/c", script)`. Go échappe les guillemets
internes d'un argument en `\"` quand il assemble la ligne de commande Windows,
et **`cmd.exe` ne connaît pas cet échappement** : il lit les antislashs comme
faisant partie du chemin. cmd recevait donc
`C:\C:\Users\...\devdesk-logs.txt\`, répondait « La syntaxe du nom de fichier,
de répertoire ou de volume est incorrecte », et rendait la main immédiatement.
Mesuré en passant la chaîne exacte dans `exec.Command`, pas déduit.

**Le second : l'échec était muet.** `handlePagerExit` journalisait l'erreur et
rechargeait. Un pager qui ne démarre pas revient en quelques millisecondes,
donc l'événement est *indiscernable* d'un pager qu'on quitte tout de suite :
`V` avait l'air d'une touche qui ne fait rien. La ligne de log était là depuis
le début, et personne ne lit un log pour savoir pourquoi une touche n'a rien
fait. Elle dit maintenant `Pager failed — check logs` (Rule 128).

**Le fichier temporaire est parti avec les guillemets, parce que sa raison
d'être était fausse.** Le commentaire disait que la branche Windows écrivait un
fichier « parce que `more` ne sait pas lire un tube comme `less` ». `more` lit
parfaitement un tube — `dir | more` est son usage canonique, vérifié. Les deux
branches ne diffèrent donc plus que par le shell et le nom du pager :

```
cmd /c  docker logs --tail 500 ID 2>&1 | more
sh  -c  docker logs --tail 500 ID 2>&1 | ${PAGER:-less} -R
```

Rien n'est écrit sur le disque, et aucun chemin n'a besoin de guillemets.
`TestThePagerCommandCarriesNoQuote` porte sur la **chaîne**, pas sur la
plateforme : le piège n'est pas propre à Windows, c'est un guillemet dans un
script confié à `exec.Command`, et la branche Unix n'a pas plus de raison d'en
porter un.

Ce qui reste vrai et qu'il faut garder en tête : l'ID du conteneur est
interpolé dans une chaîne de shell. C'est sûr parce qu'il vient de `docker ps`
— ne pas étendre ce motif à une valeur que l'utilisateur tape.

**D50 — `F` dans le viewer tuait l'application et abîmait le terminal.
Corrigé.** Signalé le 2026-08-22 en suivant les logs d'un conteneur.

`Followable.FollowCmd` rendait un `*exec.Cmd` que la vue lançait par
`tea.ExecProcess` : `docker logs -f`, TUI suspendu, terminal rendu au fils.
**On ne sort de `docker logs -f` que par ctrl+c**, et un TUI suspendu ne
l'intercepte pas — le signal allait donc au groupe de processus et emportait
DevDesk avec lui. Le terminal revenait ensuite dans le mode que le fils avait
posé, et des touches ne répondaient plus. Observé, pas déduit.

Le raisonnement d'origine est écrit dans le code et il était *juste* :
« suivre, c'est ce que `docker logs -f` fait déjà, et le refaire contre un
viewport serait refaire `less +F` en moins bien ». Juste, et à côté : il compare
deux façons d'**afficher** un flux, alors que ce qui manquait était une
**sortie**. Une capacité dont la seule sortie tue l'application n'en est pas
une, si bien affiche-t-elle.

`Followable` rend donc un `time.Duration`, et suivre est **`ctrl+r` sur une
horloge** : même `loadCmd`, même `DocumentLoadedMsg`, même handler, plus un
`followTickMsg`. C'est la forme de l'onglet Ports, qui relit `ss` toutes les
deux secondes depuis toujours. La source donne la cadence parce qu'elle seule
sait ce qu'une lecture lui coûte.

Trois conséquences, chacune avec un test :

- **C'est la génération qui arrête une boucle, pas le drapeau.** `tea.Tick`
  bloque tout son intervalle, donc un tick programmé avant l'arrêt arrive quand
  même ; relancer avant qu'il n'atterrisse laisserait deux boucles à lire pour
  la vie de la vue. `stopFollowing` incrémente `followGen` et c'est le seul
  endroit où le suivi s'arrête — `esc` compris, puisque le routeur garde la vue
  et qu'une boucle oubliée derrière elle continuerait à lancer un `docker`
  toutes les deux secondes pour un document que personne ne regarde.
- **Un document suivi atterrit en bas**, là où sont les nouvelles lignes. Toutes
  les autres arrivées atterrissent en haut : une première lecture, un
  rechargement et la bascule d'horodatage veulent tous dire « voici le
  document ».
- **`m.loading` reste faux pendant une lecture de suivi.** Le spinner du footer
  appartient à un chargement qu'on attend ; un spinner qui clignote toutes les
  deux secondes se lit comme une panne de ce qui marche. La ligne
  `Following — F to stop` est ce qui dit que le volet est vivant, dérivée à
  chaque frame plutôt que posée (Rule 128).

Le compromis est écrit plutôt que découvert : **ça interroge, ça ne diffuse
pas.** Une ligne peut attendre un intervalle, et une rafale plus longue que
`logsTail` est perdue entre deux lectures. `V` diffuse toujours, et on quitte
`less` par `q` — ce qui est exactement la différence qui compte ici.

**D49 — `mise run install` posait un binaire que Windows refuse de lancer.
Corrigé.** Signalé le 2026-08-22 : `mise run install` réussit, puis `dk` répond
*« The term 'dk' is not recognized »*.

`go build -o` prend le nom **au pied de la lettre**, là où `go install` aurait
ajouté `.exe`. Le fichier atterrissait donc en `dk`, sans extension. C'est un PE
parfaitement valide — d'où l'absence de toute erreur au build — mais PowerShell
et cmd résolvent une commande nue par `PATHEXT`, et un fichier sans extension n'y
figure pas. Il ne se lance même pas par chemin explicite : PowerShell répond
*« Cannot run a document in the middle of a pipeline »*, c'est-à-dire qu'il le
prend pour un document.

`$(go env GOEXE)` est exactement ce que `go install` aurait ajouté, et il est
vide partout ailleurs. Trois tâches le portent maintenant : `install`, `build`,
et le `run` qui exécute le binaire de `build`.

**`build` avait le même défaut et personne ne l'avait vu**, parce que son seul
consommateur est `mise run run`, qui passe par bash — et bash, lui, exécute un PE
sans extension. Le défaut n'attendait que quelqu'un tapant `.\bin\dk` dans le
terminal où il travaille déjà.

**La seconde moitié du symptôme n'est pas un défaut du dépôt** : `$GOPATH/bin`
n'est pas sur le `PATH` de cette machine, donc `dk.exe` ne se résout pas plus que
`dk`. La tâche **annonce désormais où elle a posé le binaire** plutôt que de le
tester : savoir si `dk` se résoudra est une question sur le `PATH` du shell
*appelant*, la tâche tourne sous bash, et une vérification faite là répondrait
pour le mauvais shell tout en étant crue. Dire où le fichier est allé laisse le
lecteur trancher lui-même — c'est précisément ce qu'un « command not found »
trois secondes plus tard ne lui dit pas.

**D48 — la vue `config` ne nommait pas son contexte là où les autres le font, et
son groupe Paths se lisait de travers. Corrigé.** Signalé le 2026-08-22 en
ouvrant `:cfg`.

Trois choses, un seul défaut : **le même fait rendu au mauvais endroit**.

- Le contexte était dans le **titre du viewport** (`󰙨 Configuration · dev`) et
  nulle part dans le header. Les huit autres vues le mettent dans le header
  (`{Key: "Context"}`), donc l’œil qui a appris où regarder ne le trouvait pas,
  et la seule vue où se tromper de contexte est coûteuse était celle qui le
  disait ailleurs. Le contexte est passé dans `GetHeaderInfo`, et le titre ne le
  répète pas : deux fois le même fait sur un écran n’en dit pas plus qu’une.
- Le **chemin du fichier** était un champ de header. Un header dit ce qui change
  quand l’utilisateur se déplace ; ce chemin ne change pas. Il est descendu dans
  **Paths**, sous `workspaces_dir`, auprès des deux chemins auxquels il
  appartient.
- **Show hidden files** était coincée *entre* `workspaces_dir` et `log_file`. Une
  checkbox n’a ni chevron ni valeur : plantée au milieu, elle coupe en deux la
  colonne que les lignes à valeur partagent. Elle passe en fin de groupe, où elle
  qualifie les trois chemins au lieu d’en séparer deux.

Le chemin est le premier champ **`kindStatic`** : montré, jamais écrit.
`Model.settleFocus` fait passer le curseur par-dessus, **dans le sens où il
allait déjà** — `↓` atterrit dessous, `↑` au-dessus, plutôt que de renvoyer le
curseur d’où il vient. Un indicateur de focus sur une ligne qu’aucune touche ne
modifie dirait le contraire de ce qui est vrai.

`TestThePathsGroupEndsWithItsCheckbox`, `TestTheConfigFileRowIsReadOnly`,
`TestTheCursorStepsPastTheReadOnlyRow`, `TestTheTitleDoesNotRepeatTheContext` et
`TestTheHeaderNoLongerCarriesTheFilePath` tiennent les cinq bouts.

**D47 — la table des ports continuait de s'afficher après l'arrêt de Docker, sans
rien dire qu'elle était morte. Corrigé.** Signalé le 2026-08-22 en arrêtant
Docker puis en ouvrant l'onglet Ports.

`handleData` (`ports_model.go:272`) retournait **avant** `SetItems` sur erreur,
donc la table gardait le dernier fetch réussi. Le message d'erreur, lui, expirait
au bout de trois secondes (Rule 128). Passé ce délai il ne restait qu'un tableau
de ports périmés, rafraîchi toutes les deux secondes en échec silencieux, et
**rien à l'écran ne disait qu'il était mort**. C'est la forme de D20 : « on n'a
pas pu regarder » rendu comme une donnée.

**Vider la table aurait été le mauvais correctif.** Sur un tick à deux secondes,
un hoquet transitoire ferait clignoter la table à vide. Et les lignes ne sont pas
fausses — elles sont **datées**. Ce qui manquait n'est pas leur contenu, c'est
leur âge.

Rule 128 sépare déjà les deux : un événement va dans un message (trois
secondes), un **état** va dans `Status`, qui n'a pas de minuterie. « Docker est
injoignable » est un état. La ligne de statut porte donc
`Docker unreachable — ports as of 42 s ago` (`theme.TimeAgo`, Rule 127), le
message de footer disparaît, et l'état s'efface au premier fetch réussi — sans
quoi un démon revenu se lirait encore comme mort.

Trois conséquences, chacune avec un test :

- **La péremption prime sur la pause.** Une pause est ce que l'utilisateur a
  demandé, un Docker injoignable ne l'est pas, et une seule des deux fait mentir
  les lignes.
- **Un fetch qui n'a jamais réussi le dit** plutôt que de dater le vide :
  `TimeAgo` rend la valeur zéro par une chaîne vide, donc « ports as of » aurait
  traîné dans le vide.
- **Le corps vide ne prétend plus qu'il n'y a pas de ports.** « No active ports
  found » affirme quelque chose sur l'hôte ; quand rien n'a pu être lu, il dit
  « No ports could be read ».

**Le même trou existait dans Topology, en plus étroit.** Son bandeau d'erreur
était déjà persistant, donc visible — ce qui manquait, c'est que les sections
en dessous viennent d'un chargement antérieur. Un `ctrl+r` qui échoue affiche
maintenant `Refresh failed — sections last loaded: 5 min ago`, et un **premier**
chargement qui échoue reste une erreur simple : avec rien à l'écran, il n'y a
rien à dater et « showing the load from » serait un mensonge.

**Et ce bandeau a quitté le panneau pour le footer**, sur relecture d'une
capture. Un bloc rouge au milieu des sections mettait la chose la plus lourde de
l'écran à l'intérieur de ce dont elle parlait, alors que la section
« Network Interfaces » n'avait plus qu'à dire ce que disent ses voisines quand
elles ne tiennent rien — « No interfaces found », en `DimStyle`. La ligne
`ctrl+r — retry` est partie avec : l'en-tête annonce déjà `ctrl+r` (Rule 134).

**Et `Routing Table` s'est révélée muette** dans la foulée : elle portait un
`&& tm.loadErr == ""` sur son message vide, du temps où l'erreur se dessinait
dans le panneau et lui tenait lieu de contenu. L'erreur étant partie au footer,
la garde ne laissait plus qu'un titre suivi de rien, seule des quatre sections.
Un titre sans rien dessous se lit comme un défaut de rendu, pas comme une
section vide. Un test parcourt maintenant les quatre.

Ça a demandé un champ : **`components.Status` gagne un `Level`**, de valeur zéro
`LevelInfo`, donc aucun statut écrit avant ne change de rendu. Un état dérivé
peut être un échec aussi légitimement qu'une progression — « Docker
injoignable » n'est pas un événement survenu une fois, c'est ce qui est vrai
maintenant — et il lui faut **la couleur d'une erreur avec la durée de vie d'un
état**. Seul `Status` peut donner les deux : un message expire en trois
secondes, ce qui est D47 en un champ. La ligne de péremption des ports prend le
même niveau, puisque c'est la même condition.

**Le test de connectivité OCI n'était pas concerné.** C'est une requête unique
avec sa réponse : `SetResult` pose un `resultErr` persistant, il n'y a pas de
tick, donc pas de donnée qui vieillit à l'écran. Vérifié plutôt que supposé.


**D46 — le CPU Docker du dashboard montait à 1400 %, et la RAM additionnait des
fractions de touts différents. Corrigé.** Signalé depuis une vraie session. Le
1400 % n'était pas une erreur d'arithmétique : c'était un chiffre juste sous une
étiquette fausse. Les deux lignes de la boîte Docker sont maintenant des parts
de ce que le daemon possède, donc de 0 à 100 comme celles de Host.

**Ce que `docker stats` donne, mesuré et non supposé.** `.CPUPerc` est relatif à
**un cœur** : Docker calcule `(cpuDelta/systemDelta) × onlineCPUs × 100`. Un
conteneur lancé avec quatre boucles occupées a été relevé à **398,36 %**.
`FetchAggregateMetrics` sommait ces valeurs, donc le total allait de 0 à
`NCPU × 100` — 1600 % sur ce daemon-ci, et le 1400 % signalé voulait dire
« quatorze cœurs occupés ».

Trois conséquences, dont une seule était visible :

- **Deux échelles sous un même mot.** Host affiche `cpu.Percent(0, false)`, la
  moyenne sur tous les cœurs, de 0 à 100. Docker affichait une somme relative
  aux cœurs. Les deux passaient par `percentValue`, sur une ligne appelée `CPU`,
  à la même position dans deux boîtes voisines — et le commentaire d'`Aggregate`
  invitait explicitement à les comparer.
- **La courbe était morte au-dessus d'un cœur.** `chartBlock(…, 100)` pose
  `WithMaxValue(100)` et `WithNoAutoMaxValue()` : tout ce qui dépasse est
  écrêté. Au-delà d'un cœur occupé la courbe CPU de Docker était une ligne
  droite en haut, donc plate exactement quand il y avait quelque chose à voir.
- **La ligne RAM était pire, et c'était bien un calcul faux.** `.MemPerc` est la
  part de la limite **du conteneur lui-même**. Relevé sur un vrai daemon : un
  conteneur plafonné à 256 MiB annonce `344KiB / 256MiB` → 0,13 %, un autre
  `5.324MiB / 15.18GiB` → 0,03 %. Les additionner donne 0,16 %, qui n'est un
  pourcentage de rien. Ça ne se voyait pas tant qu'aucun conteneur ne déclarait
  `--memory`, puisque tous les dénominateurs valaient alors le total du daemon.

**Le dénominateur vient de `docker info`, pas de l'hôte.** `FetchCapacity` lit
`{{.NCPU}}` et `{{.MemTotal}}` — ce que le *daemon* voit, ce qui sous Windows et
macOS est l'allocation de la VM et non la machine. Lire le compte de cœurs de
l'hôte aurait été faux sur exactement les deux plateformes où Docker n'est pas
l'hôte, et le commentaire d'`Aggregate` reconnaissait déjà cette VM.

Il est relu **à chaque agrégat** plutôt que mémoïsé. Les 240 ms que ça coûte
(mesurées, contre les 1 à 2 s que `docker stats` dépense déjà sur la même
horloge) achètent ceci : une Docker Desktop reconfigurée change ce nombre sous
un DevDesk qui tourne, et un memo diviserait par l'ancien jusqu'à la fin du
processus — sans que rien ne le dise, puisqu'un mauvais dénominateur produit un
pourcentage parfaitement plausible.

**Une capacité illisible rend l'agrégat indisponible**, elle ne le rend pas brut.
Les sommes ne sont pas des pourcentages ; sans leur dénominateur il n'y a rien
d'honnête à afficher, et `-` est un mot que ce dashboard a déjà.

**`parseSize` ne connaissait pas les unités binaires.** `MemUsage` est la seule
colonne que Docker formate en MiB/GiB, et `"15.18GiB"` tombait à travers tous
les cas décimaux jusqu'au `"B"` nu, échouait à lire `"15.18Gi"` comme un nombre,
et rendait **0**. L'ordre de la table compte : les suffixes binaires finissent
aussi par `B`, donc une table décimale d'abord fait correspondre `GB` à la queue
de `GiB` et ne les atteint jamais. Un test pinait l'ancienne limite (« binary
unit is not decoded ») ; il pinait le défaut.

**`parsePair` est `parseNetIO` renommée pour ce qu'elle fait.** Les deux moitiés
d'un `MemUsage` sont « utilisé » et « limite », pas « reçu » et « émis » : le
parsing est identique, les mots ne le sont pas, et une lecture mémoire passant
par une fonction appelée `parseNetIO` se lirait comme une erreur à chaque appel.

**Ce qui n'a pas changé : le CPU par conteneur, dans la vue `containers`.** Il
reste relatif à un cœur, comme `docker stats` — c'est ce que voit quelqu'un qui
compare les deux, et le normaliser là rendrait DevDesk incohérent avec l'outil
qu'il enveloppe. La normalisation appartient à l'agrégat, qui est le seul
endroit où l'on prétend parler de la machine.

Vérifié bout en bout sur le daemon : quatre boucles occupées, le conteneur à
398,36 %, l'agrégat à **25,02 %** — quatre cœurs sur seize.

**D45 — dans les résultats de security, le bord bas du tableau remontait dès
qu'un filtre s'activait, `.` ne faisait rien, et les filtres n'étaient annoncés
nulle part. Corrigé.** Signalé depuis une vraie session, en trois symptômes qui
n'en font qu'un : la barre de filtre de ce tableau était **déclarée mais jamais
dessinée**.

`GetFooterHeight()` comptait ses deux lignes (Rule 136) et `RenderFooter()` n'en
rendait aucune dans l'état résultats — le routeur retirait donc deux lignes au
viewport et rien ne venait les occuper. Le tableau rétrécissait, sa bordure
basse montait, et les tokens que l'utilisateur venait d'activer n'étaient
visibles nulle part. `TestFooterHeightMatchesWhatIsRendered` couvrait déjà
l'inventaire et l'état résultats *au repos* ; les deux cas où la barre s'ouvre
manquaient, et c'est exactement là que l'écart valait 2.

Deux autres choses étaient déclarées sans être atteignables, et elles se tiennent
toutes les trois :

- **`.` ne triait rien.** La touche est revenue au tri quand le seuil de
  sévérité est devenu quatre tokens (§3.26), mais aucune colonne n'a jamais reçu
  de `Less` : `CycleSort` retournait sur sa première ligne et le header
  annonçait une touche inerte. Les quatre colonnes trient maintenant, et le
  tableau garde `SortColumn: -1` — l'ordre du scanner reste le point de départ
  et `datatable` en fait une étape du cycle, donc `.` y ramène.
- **`/` vidait le tableau.** Déclarer des tokens rend `Searchable()` vrai, donc
  `/` ouvrait bien une recherche — contre aucune colonne `Search`, ce qui ne
  correspond à rien et filtre tout. La barre affichait `/ search...` par-dessus
  le marché. ID, Title (avec le paquet et le fichier) et Source cherchent
  maintenant ; Severity non, parce que c/h/m/l sont déjà ça.

**La sévérité se trie par rang, pas par ordre alphabétique.** CRITICAL n'est
alphabétiquement voisin d'aucun niveau dont il est proche — HIGH, LOW, MEDIUM —
donc un tri décroissant mettrait MEDIUM en tête et enterrerait ce pour quoi la
vue est ouverte. UNKNOWN passe sous LOW : c'est l'absence de score, pas une
prétention à pire que critique, et c'est aussi pourquoi il n'a pas de token.

**Un quatrième défaut est tombé en route** : `handleResultsState` n'avait pas la
garde `InEditMode()` que l'inventaire a depuis qu'il a une barre. Pendant une
recherche, `c` basculait le token CRITICAL au lieu de s'écrire, `esc` quittait
les résultats au lieu d'annuler la recherche et `enter` ouvrait un finding au
lieu de valider. La recherche répond maintenant avant tout le reste.

**Les raccourcis disent enfin ce que la vue fait.** `c` `h` `m` `l` étaient liés
et annoncés nulle part — il fallait ouvrir l'aide pour apprendre que la vue
filtrait. `.` était offert sur trois onglets sur quatre, reste de l'époque où il
cyclait le seuil ; c'est le tri, donc il vaut partout. Et pendant une recherche
le header ne montre plus que `enter/esc`, comme la vue ports : lister des
touches qui ne font plus ce qu'elles annoncent est pire que n'en lister aucune.

Sept tests, dont les six qui échouent sans le correctif —
`GetFooterHeight() = 5, RenderFooter() emitted 3 lines` étant le symptôme
signalé, à la ligne près.

**D44 — le spinner d'un conteneur qu'on arrête restait figé. Corrigé.** Signalé
depuis une vraie session : `K` → `Stop`, la ligne prend bien le spinner à la
place de son icône d'état, et la frame ne bouge plus.

La frame ne tourne que tant qu'un `spinner.TickMsg` est reprogrammé, et le
handler ne le reprogramme que si quelque chose tourne — `BusyLabels()` non vide,
ou `m.loading`. Or `Init` lance une boucle qui **s'éteint dès que la liste est
arrivée** : le premier tick reçu par une vue chargée et au repos ne retourne
aucun `Cmd`. Une action démarrée après ce moment-là posait donc son marqueur sur
une boucle déjà morte, et personne ne la relançait. Le rafraîchissement
périodique n'y change rien : `RefreshTickMsg` ne repasse pas par `m.loading`.

Ce qui rend le défaut coûteux, c'est ce qu'il fige : `docker stop` prend les dix
secondes du délai de grâce par défaut, et le spinner existe précisément pour dire
que ces dix secondes ne sont pas un blocage. Une frame arrêtée dit l'inverse de
ce pour quoi elle est là.

`startAction` retourne maintenant `tea.Batch(cmd, m.busyTick())` — le même
`busyTick()` que `oci_resources` (§3.22) et que le kill de netdiag, qui
l'avaient tous les deux et que ce défaut ne touchait donc pas. Relancer une
boucle déjà vivante ne coûte rien : bubbles étiquette chaque tick, donc le
premier accepté périme l'autre et il n'en reste qu'une.

Deux tests. `TestAnActionRestartsTheSpinner` vérifie d'abord qu'une vue chargée
ne tick plus — sans quoi il ne prouverait rien — puis que l'action programme un
tick ; il passe une commande factice, parce que la vraie shell out vers docker
et que `testutil.Msgs` exécute tout ce qu'on lui donne.
`TestTheSpinnerKeepsTickingWhileAnActionRuns` tient l'autre moitié : la liste est
chargée et à l'écran pendant tout l'arrêt, donc `m.loading` ne dit rien de ce
qui tourne.

**D43 — tenir `→` enfoncée empilait le même chemin dans le fil d'Ariane.
Corrigé.** Signalé depuis une vraie session :
`󰉋 devsecops   󰉋 devsecops   󰉋 devex   󰉋 devex   󰉋 coder-golang-sandbox   󰉋 coder-golang-sandbox`.

Le chargement d'un répertoire est un `Cmd`. Entre `navigateIn` et l'arrivée de
`EntriesLoadedMsg`, `currentPath` désigne déjà l'enfant tandis que la table
tient encore les lignes du **parent** — et `navigateIn` lit la sélection dans la
table. Une seconde pression relisait donc la même ligne, empilait le nouveau
`currentPath` sur `navigationStack`, et le fil d'Ariane gagnait un doublon. Le
`cursorStack` en gagnait un aussi, donc `←` demandait ensuite une pression de
trop.

**Le doublon était le symptôme bénin.** Avec le curseur déplacé entre les deux
pressions — `→ ↓ →`, trivial en tenant les touches — la seconde entrait dans un
**frère** du répertoire qu'on venait d'ouvrir, empilé comme s'il y était niché.
Le fil d'Ariane affirmait alors une hiérarchie qui n'existe pas sur le disque.

Une seule notion corrige les deux, plus une troisième que le relevé a exhibée :
`listingPath`, le répertoire d'où viennent les lignes actuellement dans la
table. Il est égal à `currentPath` exactement quand la table montre là où la vue
se dit ; entre une navigation et l'atterrissage du chargement, il ne l'est pas.

- `navigateIn` **refuse** tant que `listingPath != currentPath` : les lignes en
  main ne sont pas celles de ce répertoire, il n'y a donc rien à y ouvrir. Ce
  n'est pas un verrou à relâcher — c'est une question posée aux lignes.
- `EntriesLoadedMsg` et `LoadErrorMsg` portent le répertoire dont ils parlent, et
  Update **jette** ceux qui ne parlent pas de `currentPath`. C'est le troisième
  défaut, non signalé et de la même famille : `→` puis `←` laisse deux
  chargements en vol, et rien ne garantissait lequel arriverait en dernier — les
  lignes de l'enfant pouvaient se poser sous le fil d'Ariane du parent.

**`←` n'est délibérément pas gardée** : elle lit la pile, pas la table, donc
elle est juste quoi qu'il arrive — et le chargement qu'elle double est
maintenant jeté à l'arrivée. Garder les deux aurait avalé des frappes sans rien
acheter.

Quatre tests, et les quatre échouent sans le correctif — dont
`tabCount() = 3, want 2`, le doublon signalé, à l'unité près.

**D42 — the background ignored the theme, because two package-level variables
rendered a string before `main()` ran. Fixed.** Reported from WSL Arch: the
application was black under `default` and `catppuccin-mocha`, navy blue under
`catppuccin-macchiato`. The theme was loading correctly the whole time — its
colour simply never reached the terminal.

`dashboard/sections.go` declared `unknownValue` and `unavailableValue` as
`var`s initialised by `theme.DimStyle.Render(…)`. That runs during package
initialisation, and the first render is what trips the `sync.Once` by which
lipgloss memoises the terminal's colour profile — **permanently**
(`Renderer.ColorProfile`, lipgloss v1.1.0). The profile was therefore decided
before the line in `main()` that sets `COLORTERM=truecolor`, which exists
precisely because WSL does not forward it. The whole TUI then ran in ANSI256,
where each theme's background is quantised onto the 256-colour palette:

| Theme | Declared | Emitted | Read as |
|---|---|---|---|
| default, mocha | `#1e1e2e` | `48;5;232` (`#080808`) | black |
| macchiato | `#24273a` | `48;5;17` (`#00005f`) | navy blue |
| frappé | `#303446` | `48;5;59` (`#5f5f5f`) | grey |

Two functions instead of two `var`s is the whole fix: the first render then
happens in `View()`, long after `main()`. Measured under a pty, the profile goes
from `1` (ANSI256) to `0` (TrueColor) and `#24273a` is emitted as
`48;2;36;39;58` — the theme's own colour.

The same `var` carried a second defect it is worth naming, because it is the one
that would have survived a narrower fix: a string rendered at package
initialisation is frozen on the **default** theme's colours and follows no
subsequent `ApplyTheme`. That is the trap already documented on `ColorChartBg`
in `ApplyTheme`, met a second time in a place nothing was watching.

`TestNoPackageLevelVarRendersAString` (`internal/ui/theme`) is what closes the
class rather than the instance: it walks the AST of every non-test file in the
repository and refuses a package-level `var` initialised by a `.Render()` or by
any call into `theme`. It was checked against a reintroduced canary, so it is
known not to pass vacuously.

**What this does not fix.** `main()` only recovers `COLORTERM` when `WT_SESSION`
is set — Windows Terminal and nothing else. Under tmux, VS Code's terminal or
SSH the variable is not forwarded and the application still falls back to
ANSI256. `export COLORTERM=truecolor` is the workaround; the proper answer is an
`app.color_profile` setting in the configuration view, on the same argument as
`app.terminal_new_window` — a capability of the environment is declared, not
sniffed. Not done here, and not urgent.

**D41 — the Registries table gave two columns a negative width below 80
columns. Fixed** by §3.21, which is also what found it. `resizeRegistryTable`
floored the flexible URL column at 20 *after* the remainder had been computed —
Rule 116's named failure mode — so the last column absorbed the whole shortfall
and went to `-40` at 40 columns and `-20` at 60. The sum stayed exact, which is
why the invariant as usually stated never caught it. The solver shares the
shortfall out instead, and the three width tests added with the migration assert
**non-negative widths as well as the sum** — checked against the old
arithmetic, where they fail.

Not reached before now because nobody had run the tab at that width; the
symptom would have been the header and the selected row disagreeing about where
the table ends.

**D40 — the browser picker keyed its selection on the entry URL, so two entries
sharing a host shared one checkbox. Fixed.** `browserRegistryEntry` now carries
a `key`, and the checkbox, the group tri-state, `submitSearch` and the persisted
exclusions all read it — through one `selected(entry)` accessor rather than
seven separate map lookups, which is what stops the next site from picking a
different identity.

The key is **the slug for a standalone registry, and the group's slug plus the
member's URL for a member**. Neither half of that is arbitrary:

- The URL cannot serve, which is the defect: the form enforces slug uniqueness,
  not URL uniqueness, so two registries declared on one host ticked and
  unticked together — and stayed unticked, since the exclusion is what is
  remembered.
- `Slug` alone cannot serve either, which is the trap §1.3 flagged: a member
  carries its *group's* slug, so keying on it would have given a whole group one
  checkbox. `TestGroupMembersKeepIndependentCheckboxes` is the guard, and it
  passed before the fix as well as after — it pins the property the obvious key
  would have broken, not the defect.
- Within a group the members are told apart by **URL, not alias**:
  `cleanMemberAlias` strips `-proxy`, `-hosted` and `-local`, so
  `docker-io-proxy` and `docker-io-hosted` both display as `docker-io`. §3.18
  has since changed what distinguishes a member — they share one host and carry a
  `repo_prefix` each — and `memberKey` was the one place that had to follow: it
  takes the prefix as a third segment, so a member is keyed on its *address*
  rather than on a URL that is now the group's.

`browser-selection.json` keeps its shape; only what the strings mean changes.
An exclusion written by an earlier build is a URL, matches no key, and the entry
arrives **checked** — the safe direction, and the reason no migration was
written for it: the file records what a user unticked in a picker, and offering
it back over-selected costs one keystroke where guessing wrong costs a silent
omission from every search.

Two tests were written first and both failed on the old code — one on the
checkbox, one on the exclusion surviving a close and reopen, which is the half
that outlives the session. What was *not* touched: `entryFor(url)`
(`browser_tags.go:92`) still resolves a *result* by URL and collides the same
way. It is unreachable today (a search over two entries on one host queries the
same URL twice) and it is §3.18 that makes it live, because a result would then
have to carry the prefix to be attributable at all.

**D41 — the pull reference carried the URL scheme. Fixed.** Found while
answering §3.8's one open question, not by a test.

`multiImageName` built a reference by concatenating the configured registry URL
with the repository and tag, and nothing anywhere stripped the scheme:

```
https://registry.example.com             →  https://registry.example.com/api:v1
https://nexus.example.com/repository/dhi →  https://nexus.example.com/repository/dhi/alpine:3.19
```

`docker pull` rejects both — a scheme is not part of a Docker reference. This
was never specific to groups: it reached every registry a user wrote with a
scheme, which the form does not discourage and which this package's own examples
use. Groups only multiplied it, because member URLs are synthesised from the
group's, so one scheme in the config became eight unpullable references.

**The same root cause had a second effect that reads as unrelated.** The
Docker Hub alias check was a string comparison against `docker.io` and
`registry-1.docker.io`, in three places. A Hub configured as `https://docker.io`
matched none of them, so `normalizeRepoForRegistry` withheld the `library/`
prefix and every bare image name resolved to a repository that does not exist.
`registryAPIURL` had it too, returning `https://docker.io` instead of the API
host.

One helper now answers "what is this registry's host" and one answers "is this
the Hub", and the three sites use them. `registryAPIURL` is the single place
that still *keeps* a scheme, and that is deliberate: an explicit `http://` is
how a registry on a plain-HTTP port is reached, so upgrading it would break that
registry rather than fix anything.

Every case in `TestThePullReferenceNeverCarriesAScheme` and
`TestTheHubIsRecognisedWhicheverWayItIsWritten` fails on the pre-fix code. The
existing test covered only scheme-less URLs, which is why it never said
anything.

**D36 — `CachedGroups` and `CachedProjects` were invalidated and never filled.
Removed rather than populated.** `shared.State` declared both and three call
sites cleared them; no production code ever wrote a value into either, so the
"every explorer open refetches" complaint was about a cache that had been nil at
every moment of its life.

Two things settled it against filling them:

- **They could only ever be read while deliberately empty.** The explorer keeps
  its own tree for as long as it exists, and `createView` rebuilds a view only
  after dropping it — which happens on a config save, a context switch or a
  logout. Those are precisely the three sites that cleared this cache.
- **The shape is wrong.** §3.16 made the explorer a lazily-walked, paginated
  tree. A flat `[]*Group` cannot say which level was fetched, and filling one
  needs the full API walk §3.16 removed *because* it froze the view for minutes.
  The right cache for a tree is the tree, and the explorer has it.

Leaving the fields in place was the real risk: they read as a cache someone had
not got round to filling, and the obvious way to fill them is the walk that was
deliberately abolished. The reasoning is recorded where they were declared.

Two tests asserted on them and now assert on `GitLabStats` — the one of the
three the dashboard actually fills, and which nothing else covered. Both were
checked against a build with the `GitLabStats = nil` lines removed, and both
fail on it.

**D21 — an unreachable focus clamp in `ConnectivityTestForm`. Removed**, the way
D5 was. Cycling the test type can take `numFields()` from 4 to 3, and both
handlers clamped the focus against that; neither could fire, because cycling
only happens inside `focusedField == cFieldType`, so the focus is 1 and
`numFields()` is never below 3. `TestCyclingTheTypeNeverStrandsTheFocus` was
already written to pin the invariant rather than the code, and passes unchanged
— which is what a test written that way is for.

**D37 — the workspaces breadcrumb printed whole paths on Windows. Fixed.**
Reported from use. `pathBaseName` split on `"/"` alone, and every path in that
view comes from `filepath.Join` and `os.ReadDir` — so on Windows it found
nothing to cut and each tab carried the full absolute path:

```
󰋜 home   󰉋 C:\Users\anthoni\workspaces\anthnell   󰉋 C:\Users\anthoni\workspaces\anthnell\devsecops
```

Three of those overflow the line, and none of them says where the user is any
better than one word would. `filepath.Base` is the whole fix.

The test builds its paths with `filepath.Join` for the same reason the view
does, and so only distinguishes the two implementations where the separator is
not `"/"` — which is precisely where the defect was. CI runs on Linux, so it is
green there either way; it was checked against the pre-fix code on Windows,
where it reproduces the line above exactly.

**D38 — the workspaces spinner had been frozen on frame zero since it was
written. Fixed**, and found while wiring §3.17's sync into the same mechanism.

`spinner.TickMsg`'s handler stops scheduling the next tick once nothing is
running — correctly, since there is no reason to rebuild the rows sixty times a
second for a settled table. But nothing ever started the chain again: `Init`'s
died on its first tick, and a scan beginning ten minutes later inherited a dead
chain. The `⠋ scanning` cell never advanced.

It went unnoticed because a stationary braille dot reads as a *marker*, not as a
stalled animation. The security view had already hit this and grown
`spinnerTickIfIdle`; the workspaces view had the same shape and none of the fix.

The restart lives in the two Starting handlers — the single funnel every path
goes through — and reads the maps *before* recording its own path, so the first
repository starts a chain and the second does not start a second one. Starting
two is the opposite defect and makes the frames advance at twice the rate.
`TestTheFirstScanOrSyncRestartsTheSpinnerChain` covers both, for both actions,
and fails on the pre-fix code.

One window is left deliberately: an action started before `Init`'s first tick
arrives doubles the chain for the life of the view. It is one frame interval
wide and costs a fast spinner.

**Two settings had a second, non-persisting writer. Both removed** once the
configuration view gave them a home.

`status` adjusted `refresh_interval` with `+` and `-`, **in memory only**. The
running interval and `status.refresh_interval` could therefore disagree, the
header reported the running one, and the adjustment was lost on restart. Same
shape as `gitlab.url` in two views: one setting, two holders, one of which does
not persist.

`:theme` opened a picker that loaded a theme *and* wrote `app.theme` to disk —
a second writer for a setting the configuration view now owns, and one that
bypassed its form. The command, its overlay, `internal/app/theme.go`,
`CommandTheme` and `ThemeListMsg`/`ThemeAppliedMsg`/`ThemeErrorMsg` are gone;
`applyThemeNow` swaps the palette without saving, because the view already did.

Together they removed 264 lines against 44 added.


**D28 — logging out of GitLab left the session behind. Fixed** by giving
`auth.LogoutCompleteMsg` a router handler. Reported from use, not found by
reading.

Logging in went through the router: `handleAuthResult` calls `setAuthenticated`,
which fills `sharedState` with the client, the user and — as they load — the
group and project caches. Logging out did not. `LogoutCompleteMsg` was consumed
by the auth view, which reset its own `authenticated`, `user` and token input,
and nothing else.

So after a logout the explorer went on browsing projects and the header went on
naming a signed-out user, because both read `sharedState.CurrentUser` and
`sharedState.GitLabClient`, which nobody had cleared. The asymmetry is the
defect: one direction of a two-way transition had an owner and the other did
not.

`clearAuthenticated()` is now `setAuthenticated()`'s mirror and drops the caches
with the session — they were read through the client that just stopped being
valid. Every view but the auth view is dropped too: clearing `sharedState` does
not empty a table the explorer already loaded. The auth view is kept because it
is on screen and has just written "Logged out successfully".

Five tests in `internal/app/logout_test.go`, all confirmed to fail with the
handler removed.


**"dark" named a theme no picker could show. Fixed** in `applyDefaults`, found
by the configuration view's own field test.

`LoadTheme` accepted `""`, `"dark"` and `"default"` as the built-in theme, but
`ListThemes` only ever offered `"default"` — so the default config named a
theme absent from every list. `:theme` escaped it by reading
`theme.CurrentThemeName` rather than `cfg.App.Theme`; the configuration view
binds a cycle field straight to the setting, and a cycle whose current value is
outside its options jumps somewhere arbitrary on the first press.

`applyDefaults` now normalises `"dark"` to `"default"` at load, so the third
name disappears from files as they are rewritten. `LoadTheme` still accepts it,
which costs nothing and covers a file not yet touched.

Found by `TestEveryCycleFieldDefaultsToOneOfItsOptions`, which asserts a
property of the whole field table rather than of any one field.


**D27 — the custom tool paths were read by nothing, and the source could not be
chosen. Fixed** by `internal/scan/tool_source.go`, found while planning the
configuration view.

`scan.trivy_path` and `scan.gitleaks_path` were declared in the schema,
defaulted in `Default()`, and tilde-expanded in `ExpandPaths` — and no reader
was ever written. `CheckDependenciesWithImages` called `exec.LookPath("trivy")`
and the command builders hard-coded `toolCmd{Name: "trivy"}` /
`{Name: "gitleaks"}`. Setting a path did nothing, silently. Somebody thought the
field mattered, since it is expanded on load.

The signature is why: the function took the two image names and nothing else, so
there was nowhere to pass a path. The same shape ran through every builder as
the pair `source ToolSource, image string`.

The other half is that resolution tried the binary first and only reached for
Docker in the `else`, so a binary on `PATH` always won: asking for the pinned
image while Trivy happened to be installed was not expressible.

- `ToolSpec{Source, Binary, Image}` replaces the `(source, image)` pair
  everywhere. It is a net *reduction* in argument count — `GetTrivyCommand` had
  eight positional parameters, which is why nobody threaded a ninth through.
- `scan.trivy_source` / `scan.gitleaks_source` take `auto | binary | image`, and
  default to `auto`, which is the historical resolution exactly — an existing
  config cannot change meaning on upgrade.
- **`binary` does not fall back to Docker.** That silent fallback is what kept
  D27 invisible: a path that was never read still produced working scans, run by
  something other than what was asked for.

All three invariants confirmed to bite: ignoring the configured path,
reinstating the Docker fallback, and hard-coding the tool name in the builder
each fail their own test.
**The scan caches ignored the configuration context. Fixed** by
`internal/cache/scan_file.go`, found while planning the configuration view.

`config.yaml` is per context — `LoadContext` reads `config-<name>.yaml` — but
all six caches lived flat under `~/.devdesk/cache/`, and `browser-selection.json`
was the only one with a context dimension. `workspaces_dir` and the registry
list being per context, two contexts legitimately hold different roots and
different images in one namespace.

Nothing showed it, because the caches were only ever *queried*: you ask about
the image in front of you, and the answer is right whoever wrote it. The
inventory view planned for §2 of the configuration-view plan *lists* everything
the cache holds, which is what would have put another context's findings on
screen. So this is a latent inaccuracy fixed before the change that would have
exposed it, not a defect anyone reported.

`ImageScanCache` and `WorkspaceScanCache` are now bound to a context at
construction; their method signatures are unchanged. The legacy flat file is
recognised by `Contexts == nil` after unmarshalling into the versioned struct —
no field matches — and is **upgraded on the first open rather than at the next
write**. Deferring it would let each context that opened the file claim the
legacy entries in turn, making ownership depend on write order;
`TestAFlatImageCacheMigratesIntoTheOpeningContext` is what pins that, and it
fails when the write-back is removed.

`config.CurrentContextName()` came out of it, collapsing the
`GetCurrentContext` / fall back to `"default"` pair that `Load` and `Save`
already each carried a copy of.
**D26 — a scan option applied from the form and silently did not from the
lists. Fixed** by `scan.OptionsFromConfig`, found while planning the
configuration view (`.claude/plans/configuration-view-plan.md`).

`ScanOptions` was assembled by hand in three places — `security/scan.go:31`,
`workspaces/actions.go:143` and `oci_resources/images.go:79`. The last two were
byte-for-byte identical and read the config; the first read the form's transient
values and was the only one of the three that set `IgnoreEOL`. So ticking
"ignore EOL" applied when scanning from the security form and did nothing when
scanning from the images list or the workspaces list, with `--ignore-status
end_of_life` silently absent from the Trivy command.

Same family as D24 and D25: three copies of a block, one of them drifted, and
nothing said so. The form now persists and reads back through the one builder,
so its behaviour is unchanged and there is a single definition of what a
configured scan is.

`TestEveryConfiguredOptionReachesTheScanner` is deliberately not a test that
`IgnoreEOL` is carried. It walks the field names `config.ScanConfig` and
`scan.ScanOptions` share and asserts every one of them arrives, so a tenth
option added to both without plumbing it through fails there rather than
shipping. Confirmed to bite by removing the line: `OptionsFromConfig did not
carry IgnoreEOL: got false, want true`.

**The command line took focus from the render path.** Found in phase 5.
`renderHeader` called `a.commandInput.Focus()` whenever `commandMode` was set —
a mutation inside `View()`, which Rule 110 makes read-only. A
`bubbles/textinput` drops every key it receives while blurred, so the command
line only accepted typing because a render happened to have run first. Focus is
now taken in `enterCommandMode` and released alongside the existing `Blur()`
calls on the way out. In production the ordering held, so nothing was visibly
broken; the coupling surfaced the moment a test drove the router without
rendering, and the same latent bug would bite anyone reordering the loop.

**Two views' help advertised keys that do nothing.** Found while writing the
phase 2 tests, not previously recorded.

The status view's `GetHelpContent` documented `n` for "Add a new monitor" while
the binding has been `ctrl+n` since Rule 111 standardised it, and the `/` filter
was missing altogether; the empty-state message told the user to "Press [n]" too.
The containers view advertised `S` (shell in a new window) in `GetShortcuts()`
without documenting it at all.

The workspaces view had it too, found in phase 3: `n` and `Enter` documented
where the bindings are `ctrl+n` and `enter`, `/` undocumented, and the same
stale "Press [n]" in its empty state.

`explorer` made it four in a row, though only just: every key was documented
except `/`. The check caught it on its first run.

`security` broke the streak — its help documents every key its header
advertises, across all four states.

`oci_resources` then produced the worst instance, in phase 4. The Networks and
Volumes tabs documented `n` for a binding that has been `ctrl+n` since Rule 111
renamed it — the same drift `status` and `workspaces` carried — but the **whole
Registries tab was undocumented**: `ctrl+n`, `e`, `l` and `L` appeared nowhere,
so four actions on a tab were reachable only by guessing.

That view needed one adjustment to the check: its help qualifies most keys with
the tab they belong to (`enter (Images)`, `ctrl+n (Networks)`), so the
parenthetical comes off before matching. That convention is worth keeping — with
four tabs sharing a keymap, an unqualified `enter` would be ambiguous.

So the pattern across five views: the drift is a Rule 111 rename the help did
not follow, plus whole surfaces added later and never documented at all.

All five are fixed, and every view in `internal/ui` now asserts that each key
`GetShortcuts()` advertises appears in `GetHelpContent()` — the check that would
have caught the drift when it was introduced.

**The explorer acted on a different row than the one highlighted.** Found in
phase 3, and the most serious defect the coverage work has turned up.

`handleKeyMsg` resolved the cursor against `sortedItems(currentItems())` — the
unfiltered list — while the table was built from the filtered one. With a filter
active the two indices disagree, so `ctrl+d`, `p`, `→` and `ctrl+w` all acted on
whatever happened to sit at that index in the *unfiltered* list. Filtering to a
single project and pressing `ctrl+d` scheduled a different group for deletion.
`GetShortcuts` had it too, so even the advertised `ctrl+w` keyed off the wrong
node.

Both now go through `visibleItems()`, the single list `updateTableRows` builds
from. Pinned by `TestActionsResolveTheRowTheUserCanSee`, which filters to a row
that sits at a different index in each list — the fixtures were chosen so the
two cannot coincide.

**A stranded cursor in the workspaces table.** `bubbles/table.SetRows` does not
clamp the cursor when the row count shrinks. Drilling into a directory with
fewer entries — or narrowing the filter — left the cursor past the end: nothing
highlighted, and `enter`, `ctrl+d`, `r` and `ctrl+s` all silently did nothing
until the user pressed an arrow key. `updateTableData` now clamps.

The containers view avoids this by calling `GotoTop()` after every filter
change; workspaces had no equivalent. **`explorer` had it too**, and only on the
filter path — its drill-down was already safe because it calls `GotoTop()`.
Fixed the same way.

**Two tabs reported nothing when Docker was down.** Found in phase 4.
`handleImagesList` has always set a footer message on failure;
`handleNetworksList` and `handleVolumesList` only logged, so with the daemon
stopped those two tabs showed an empty table — indistinguishable from "you have
no networks". Both now match their sibling.

**The Registries tab opened empty.** `switchTab` focused the table and fired the
`docker login` status check, but never called `updateRegistryTable()` — so the
tab stayed blank until an asynchronous Docker call answered, even though the
registries come from the config file and were available immediately.

**A search counter with no floor.** `RegistryBrowser.AddRegistryTags`
decremented `pendingSearches` unconditionally. A duplicate or late response drove
it negative, and the *next* search then started from that base: `IsSearching()`
stayed false while requests were genuinely in flight, so the spinner never
showed. Floored at zero.

**Footer messages with no timer.** `explorer.handleDeleteComplete` set
`footerError` and returned `nil`, so a failed delete left "Delete failed — check
logs" on screen until something else overwrote it. Rule 128 caps footer messages
at three seconds, and the same file's `BrowserOpenedMsg` handler already did it
correctly. Now returns `clearFooterErrorCmd()`.

The security view had it worse: **Rule 128 was not honoured anywhere in it.**
`statusMessage` was set in three places — "Added x to .gitleaksignore", "Failed
to ignore secret", "No references available" — and there was no clear timer in
the package at all, so whichever happened last stayed on screen until a tab
switch happened to reset it. `clearStatusCmd` / `clearStatusMsg` added and all
three wired, pinned by `TestFooterMessagesExpire`.

**D7** (`extractTarGz` kept parent references in archive paths), **D2** (the
"permanent delete" checkbox was documented as locked but was not) and **D5** (an
unreachable focus clamp in `CreationForm`) were fixed together — three small,
independent defects with no shared code.

`extractTarGz` now routes every member name through `sanitizeArchivePath`, which
normalises the separators and rejects anything resolving outside the root.
Cleaning happens *after* the leading separator is stripped, not before:
`path.Clean("/../x")` returns `"/x"`, which would have absorbed the traversal
silently instead of exposing it. Traversal that resolves back inside the root
(`templates/../README.md`) is kept, normalised. Backslashes are folded to `/`
first, so `..\..\etc\passwd` cannot pass as an ordinary filename on a tar reader
that treats it as one.

The delete modal carries a `locked` flag, set only by
`NewDeleteConfirmModalPermanent`. Rather than merely making the toggle a no-op,
navigation skips the checkbox entirely (`minFocus()` / `cycleFocus()`) and the
line renders dimmed: a focusable control that ignores every key is more
confusing than one that is plainly not there.

The `CreationForm` clamp was deleted and replaced by a comment recording why it
cannot fire, so it is not reintroduced defensively. A test pins the invariant it
was guarding.

Both self-annulling tests became real assertions:
`TestDeleteConfirmModalPermanentCheckboxIsStillToggleable` →
`TestDeleteConfirmModalPermanentCheckboxIsLocked` (plus navigation and
confirmation cases), and `TestExtractTarGzPreservesParentTraversalInKeys` →
`TestExtractTarGzRejectsParentTraversal` (table-driven over five escape shapes)
alongside `TestExtractTarGzNormalisesContainedTraversal`.

**D1** (`ReportModal.View()` emitted invalid UTF-8), **D3** (`wrapInputLines`
looped forever when `wrapWidth <= 0`) and **D6** (`wordWrap` measured bytes) were
fixed together, since D1 and D6 were the same byte-vs-rune defect.

Rather than patch each call site, the truncation logic moved into
`internal/ui/theme/text.go` per Rule 117: `StringWidth`, `TruncateWidth` (keeps
the head) and `TruncateTailWidth` (keeps the tail, for paths). All three measure
terminal columns, so double-width glyphs are handled too, not just multibyte
ones.

A **fourth site carried the same defect** and was not recorded here: `truncate`
in `internal/ui/security/model.go` sliced bytes exactly like D1 and rendered
Trivy finding titles. It is now `theme.TruncateWidth` and the local helper is
gone.

A **sixth site** closed the family out, in the last package of phase 3:
`parseVersion` in `internal/ui/security` truncated its fallback with
`result[:15]`. Version strings are ASCII in practice, so this was the
lowest-risk of the six — but it is the same pattern, and it is now
`theme.TruncateWidth`. Grepping for `[:` on strings across `internal/ui` now
returns nothing but slice indexing.

The same function had a second defect: it scanned for the version number by
taking the first whitespace-separated token starting with `v`, so
`gitleaks version 8.18.2` reported its version as **"version"**. A leading `v`
now only counts when a digit follows it.

A **fifth site** turned up during the phase 3 tests: `firstOutputLine` in
`internal/ui/netdiag` sliced `line[:maxLen-3]` to fill the Output column of the
results table, so any diagnostic whose first line contained a multibyte rune
could be cut in half and bleed across the rows below (Rule 122). Now
`theme.TruncateWidth`, pinned by `TestFirstOutputLineKeepsMultibyteRunesIntact`.
Worth grepping for `[:` on strings when the remaining phases land.

The two pinned tests that `t.Skip()`d became real assertions
(`TestReportModalViewKeepsMultibyteRunesIntact`,
`TestWordWrapMeasuresColumnsNotBytes`).

One call site was deliberately left alone: `truncateResultLine` in
`internal/ui/oci_resources/connectivity_form.go` counts runes rather than bytes,
so it is correct — merely imprecise on double-width glyphs. Folding it into the
theme helpers is a cleanup, not a defect fix.

**Every scan came back empty.** Introduced by the `internal/scan` split itself
and caught within the hour by the coverage pass that followed it — the clearest
argument yet for the surface → split → cover order.

Extracting the subprocess seam collapsed two statements into one return:

```go
return stdout.Bytes(), waitErr(tc.Name, cmd.Wait(), stderr.String())
```

Operands are evaluated before the call, so `stdout.Bytes()` snapshots the buffer
**before** `cmd.Wait()` runs — and `os/exec` fills that buffer from goroutines
only Wait is guaranteed to have finished. `Bytes()` returns a slice header with
the length at that moment, so later writes are invisible: the report was always
empty, every scan reported no findings, and the exit code alone survived. The
code it replaced got this right by accident of being written as separate
statements.

The read now lives in `finish`, after Wait, with the reasoning recorded next to
it so it is not re-collapsed. Pinned by `TestTheReportOnStdoutIsWhatComesBack`,
which is the test that failed first when the seam was finally exercised.

Worth stating plainly: this is the one defect in this list that the tests
*introduced* rather than merely found, and it never reached a commit. A
mechanical-looking refactor changed evaluation order, which is exactly what a
package with no tests under it cannot tell you.

**Context isolation in `GitCredentialStorage` never worked.** Found while
planning the coverage work, not previously recorded.

The three methods carried the DevDesk context in the `path` field of the git
credential protocol (`path=devdesk/context/<name>`). Git discards that field
unless `credential.useHttpPath` is set, which is off by default, so every
credential was keyed on `protocol://host` alone. Two contexts pointing at the
same GitLab host silently overwrote each other, and the last one to authenticate
won for all of them. Verified against a real `git credential` store before and
after the fix; it affects every helper (GCM, wincred, osxkeychain), since git
strips the path before the helper is ever called.

**Fix:** pass `-c credential.useHttpPath=true` on each invocation. Scoping it to
the call leaves the user's git configuration alone — setting it globally would
change credential resolution for every repository on the machine. Pinned by
`TestGitCredentialIsolatesContexts`.

Existing users must re-authenticate: credentials saved under the old
path-stripped key no longer match. Those credentials were ambiguous across
contexts anyway.

Two smaller defects went with it:

- `Delete` had no timeout, while `Save` and `Load` bounded themselves to 2 s
  precisely so an unconfigured helper could not freeze the TUI. Logout could
  hang indefinitely.
- The timeout killed only the direct `git` child, though its comment claimed
  otherwise. The helper git spawns survives and holds the output pipes open, so
  `Run` kept blocking. All three calls now share one `runCredential` helper built
  on `exec.CommandContext` plus `WaitDelay`, which bounds the wait on those pipes.

`internal/credentials/helper.go` (`HelperStorage`, 137 lines) was deleted rather
than fixed: nothing outside its own tests constructed it. It was also broken —
`detectHelper()` returns whatever `git config credential.helper` holds, so a
common value like `store --file=/path` became the single unfindable command
`credential-store --file=/path` — and it hardcoded `protocol=https`, unlike
`GitCredentialStorage`, which reads the scheme from the URL.

**D15 — `esc` never reached a view that was not editing.** The router answered
`esc` itself through `maybeQuitCommandMode`, forwarding it only when the view
reported `InEditMode()`. Every esc-to-go-back handler behind that gate was dead
code: the explorer's `esc` → `handleDrillUp` could not fire, though Rule 111
lists `esc` as the third way up.

**Fix:** the router no longer names `esc` at all — it falls through to the
default branch and is forwarded like any other key. What made this smaller than
it looked is that the router had nothing left to do with `esc` by the time the
branch ran: `handleCommandMode` answers first whenever the command line is open,
so `maybeQuitCommandMode` was resetting an already-closed line and asking for a
redundant resize. It is deleted.

The interesting half is what the gate had done to the views. Two of them
declared themselves *editing* in states holding no field at all, for one reason
— it was the only way to be handed `esc`. The security view said so outright:
`InEditMode()` was documented as *"returns true when the view needs to handle
ESC key"*, and claimed `StateScanning`, `StateResults` and `StateDetails`;
netdiag claimed `StateRunning` and `StateDetails`. That claim costs more than it
buys, because `InEditMode()` also governs `:`, `q` and `?` — so the security
results screen, the one place a user most wants to jump elsewhere, was the one
place `:` did nothing. Both predicates are now about focus and nothing else, and
those states get the command line, the help overlay and quit back.

Left alone deliberately: the containers view claims `stateLogs`, where `q`
really is the view's own key for leaving the log pane. That is key ownership,
not a workaround for `esc`.

**D16 — `:netdiag` was documented but not accepted.** One missing map entry.
**D17 — the completion catalogue was a subset of the parser.** Eight commands
listed against fourteen views plus aliases, kept by hand in two places.

**Fix, for both:** one map. `viewNames` in `parser.go` maps every accepted
spelling to its view, the key equal to the `ViewType` being the full name and
every other key an alias; `GetAliases()` and the new `FullNames()` derive from
it, and the completion engine derives from those. Two tests hold the two ends
together — everything the parser accepts is suggested, everything suggested
parses — so the lists cannot drift apart again. `ParseCommand` lost its chain of
`if mainCmd ==` comparisons to the same treatment, `actionNames` and
`actionAliases`, and `Parse` is now a lookup in `viewNames` rather than a third
copy of it.

Two aliases documented in `CLAUDE.md` but accepted nowhere came out of this —
`gle` for the explorer and `w` for workspaces — the same defect as D16, found by
the test rather than by reading. `ViewNet` was renamed `ViewNetdiag` and its
value changed from `net` to `netdiag`, so the full name matches the view, the
package and the documentation; `net` remains an alias.

**D18 — the header overflowed a narrow window.** `buildShortcutLines` meant to
clip the shortcut block to its column — the comment said *"Truncate if wider
than col2Width"* — but called `theme.PadWithBg`, which returns content untouched
once it is already at or past the target. Below roughly 100 columns the header
rendered wider than the window and wrapped, costing the viewport a row.

**Fix:** `lipgloss.NewStyle().MaxWidth(col2Width)`, which truncates on rune
boundaries without cutting an escape sequence in half — the corruption Rule 122
is about. Width 80 is now part of `TestEveryHeaderRowFillsTheWidth`, and a
second test checks no row ends inside an escape sequence, which is the failure a
naive slice would have produced.

**D20 — an image scanned with no scanner installed was reported as clean.**
Found by the phase 6 coverage pass, and the most serious defect this repository
has recorded: a security feature that says an image is fine when nothing looked
at it.

`Scanner.Scan` **skips** a stage whose tool is unavailable rather than failing
it (`if s.options.EnableVuln && s.deps.TrivyAvailable`), so with no trivy the
result carried **no errors and no findings**. `scanOneImageCmd` reports a
failure only when there are errors *and* no findings, so the image came back
with zero counts, those counts were written to the scan cache with a fresh
timestamp, and Rule 126 kept them until an explicit rescan. Enter then opened an
empty report.

Nothing upstream caught it: `internal/ui/oci_resources` performs no dependency
check at all, unlike the security view, which gates its scan on
`canStart := m.deps.TrivyAvailable || m.deps.GitleaksAvailable`
(`security/view.go:147`). Every entry point was affected — `ctrl+s`, `A`,
`ctrl+a` and the delegated `LaunchBatchScanMsg` all reach the same
`batchScanCmd`.

**Fix:** `Scan` now records a missing tool as an error, in
`missingToolErrors`, before any stage starts. That is one change in the package
where the knowledge lives and it closes the defect for every caller — the OCI
images view needed no change at all, because `scanOneImageCmd`'s existing
"errors and no findings" condition then reports the failure by itself. Adding a
dependency check to the view was considered and dropped: with no scanner the
scan returns instantly, so a pre-flight guard buys nothing a clear error does
not.

The precision that makes it usable is in **what is not reported**. A stage
skipped because it does not apply to the target type is not missing anything —
gitleaks scans a working tree, so a secret scan of an image was never going to
run — and reporting those would train the user to ignore the warnings panel.
Only stages that would otherwise have run are named, and each message says what
to install (`install trivy or pull aquasec/trivy`).

This reversed a decision the tests had recorded.
`TestAStageWithoutItsToolIsSkippedSilently` asserted that a missing tool was
*not* an error, on the grounds that "the dashboard already says the tool is
absent". That reasoning does not survive contact with the result panel, where
"no secrets found" and "nothing looked for secrets" are the same screen. It is
now `TestAStageWithoutItsToolIsReportedRatherThanSkippedSilently`, and the
partial case is reported for the same reason as the total one.

Pinned by `TestAScanThatCouldRunNoScannerIsNotACleanScan` and, in the view,
`TestAScanWithNoScannerInstalledIsReportedAsAFailure` — the inverted test from
phase 6, turned around, now also asserting that **nothing reaches the cache**.

**D22 — one stray `:` broke every scan, from every view, permanently.** Reported
from a real run, as a Trivy failure nobody could trace back to DevDesk:

```
trivy vuln
exit status 1: FATAL Fatal error flag error: unable to convert flags to
options: invalid server address format: parse ":": missing protocol scheme
```

The Trivy server field held `":"`. Nothing validated it: the value went straight
from `m.trivyServerInput.Value()` into `--server` and into `config.yaml`.

Three things compounded, and the middle one is the reason this was worth
recording rather than just fixing:

- **A `:` typed into that field is a character, not the command line.** That is
  correct and deliberate — §3.7 records that the field's placeholder is
  `https://trivy-server:4954`, so it *has* to accept two colons to hold a valid
  value, which is precisely why `alt+:` exists. Pinned by
  `TestAColonTypedIntoTheTrivyServerFieldIsACharacter`, so nobody "fixes" the
  wrong end of this.
- **The field is persisted on every option toggle** (`saveOptionsToConfig` calls
  `config.Save`), so one keystroke reached the config file.
- **Every view reads it from there** — `oci_resources/images.go:87` and
  `workspaces/actions.go:158` both take `config.Scan.TrivyServer` — so a field
  the user only ever saw in the security view broke image scans and workspace
  scans too, until the value was found by reading the YAML.

**Fix**, in the three places that each own part of it:

- `serverAddr` in `internal/scan/trivy_args.go` trims the value, treats
  all-space as *unset*, and refuses anything that is not an absolute URL —
  checking `Hostname()` rather than `Host`, because `http://:` parses with a
  host of `":"` and no hostname. All three builders (`trivyArgs`,
  `trivyMisconfigArgs`, `sbomArgs`) go through it, so no stage can be the one
  that was not checked.
- The message names the setting rather than the parser:
  `trivy server address ":" is not a URL (want http://host:port) — fix or clear
  scan.trivy_server`. That is what makes it actionable from the OCI images view,
  where there is no field to look at.
- The security form refuses to start a scan while the address is unusable
  (`scan.ValidateTrivyServer`, sharing the builders' rule so the two cannot
  disagree), and trims both the server and the Gitleaks config path before
  persisting them.

Deliberately not done: silently dropping an unusable address, or rewriting the
config on load. The user asked for client-server mode; running locally instead
without saying so is the same class of quiet substitution as D20.

### 1.2 The five parked defects

D4, D8, D9, D10 and D11 were each recorded rather than fixed on discovery,
because each changed something the user already saw. They were decided together
and fixed in one pass. Three of the five had a test written to fail *on the
fix*, and all three did.

**D4 — `tab` navigated the delete confirmation.** Rule 135 reserves `tab` for
switching tabs and assigns field navigation to `↑ / ↓` exclusively; the modal
had it exactly inverted, cycling on `tab` and clamping on `↑ / ↓`. `↑ / ↓` now
cycle, which is what keeps every control reachable in one direction, and the
`tab` cases are gone. The explorer no longer advertises "tab Navigate" while the
modal is open, and `↑↓` is not advertised in its place — Rule 138 calls it
obvious. The permanent variant still skips its locked checkbox, so cycling there
toggles between the two buttons.

**D8 — write-only CRUD flags.** `creating`, `editing` and `confirming` were
assigned in five places and read in none, and `ComponentFormCancelledMsg` — the
only thing that would have reset two of them — could never be sent. Deleted, all
of it. Nothing else changed: the phase 2 tests had deliberately asserted on
`componentForm` / `confirmModal` rather than on the flags, which is what made
this safe a phase later.

**D9 — the containers list opened Z→A.** `New()` set neither `sortColumn` nor
`sortAsc`, so both took their zero value. Both are now set explicitly, so the
default is stated rather than inherited. `TestDefaultSortIsNameDescending`
became `TestDefaultSortIsNameAscending`, and the cycle test walks forward from
ascending. The `loadedModel` helper still sets the sort itself even though it
now matches the constructor: those tests should say which order they rely on.

**D10 — a registry failure was invisible.** `renderTemplateList` returned early
when the field was unfocused and the warning sat below that return, so a form
opened after the OCI registry failed offered a Template field reading "none"
with nothing to distinguish "the registry is down" from "there are no
templates". The warning moved into `renderTemplateWarning` and is appended in
both branches — it explains why the list is empty, so it belongs wherever the
list is.

**D11 — CRITICAL and HIGH rendered identically.** `getSeverityStyle` composed
CRITICAL by hand as `ColorError` + `Bold`, which is byte-for-byte the
`theme.StatusErrorStyle` it returned for HIGH. The palette moved to
`theme.SeverityTextStyle`, next to the `TableStylesForSeverity` it draws from,
and the view delegates (Rule 102). An unrecognised severity now falls back to
the info colour rather than to `DimStyle`, so it is still legible.

The pattern worth keeping: when a defect is recorded rather than fixed, write
the test **inverted** — asserting the current behaviour and saying so. D9, D10
and D11 each had one, and each failed the moment the fix landed, which is how
the stale test and the stale backlog entry got found together.

### 1.3 Open

**Nothing is open.** D39 was the last, and §3.18 closed it on 2026-08-23 — see
§1.1 for what it was and what the fix cost on the screens around it. D40, which
it needed closed with it, had already been fixed on its own. D52, trouvé en
écrivant §3.6 étape 1, a été corrigé à l'étape 3. D21 and D36 closed everything
that preceded them. All of them are in §1.1.

What remains below is **D35**, which is not open and not closed: a stale reading
with a way to refresh it, kept here because that is a decision rather than a
fix.

D12, D13 and D14 were all fixed by §3.8 — see "The three defects it closed"
there for what each turned out to be. D14's inverted test failed the moment the
fix landed, which is what the pattern is for, and has been turned around.

**D35 — the "unpulled" count is only as fresh as the last fetch. Mitigated by
§3.17, not closed.** `detectGitStatus` computes it with
`rev-list --count HEAD..@{u}` (`workspaces/entry.go:90`), and `@{u}` is the local
remote-tracking ref. Nothing in DevDesk moved it, so the column read `0` on a
repository forty commits behind.

`s` now does: sync fetches first and always, so a repository it touches — even
one it *refuses* to fast-forward — comes out with a true count. What remains is
that `loadEntries` still does not fetch, and must not: a directory listing that
hits the network on every drill-down is a different defect. So a repository not
synced since the app opened still shows a number nobody has checked. There is
now a way to make it true, and the one feature that acts on it never trusts it —
which is the difference between a lie and a stale reading.

**D34 — the explorer paginated nothing. Fixed.** Every list in
`internal/ui/gitlab/explorer/api.go` was built with `PerPage: 100, Page: 1`, at
five sites, so a group with more than 100 subgroups or 100 projects was silently
truncated: the explorer showed fewer children than it had, and a recursive clone
skipped repositories without saying so.

`listAll` now walks every page and the four list calls go through it — one loop
rather than four copies, which is how one of them would have ended up wrong. It
terminates on `NextPage <= Page` rather than `NextPage == 0` alone: that also
stops a server pointing back at the page just served, and unlike a page cap it
is a bound that cannot cut a legitimate response short. `perPage` is the one
place the 100 is written.

Three tests assert both halves of a group's children and the pull's own walk
follow every page, and were checked against the pre-fix code — all three fail on
it. A fourth pins the runaway guard.

**D25 — status acted on the wrong monitor under a filter. Fixed** by §2 step 6,
which is also what found it. `getSelectedComponentIndex` replayed the sort by
hand and then indexed, but never applied the text filter the rows had already
been through; the SSL branch walked `m.components` by counting, unfiltered the
same way. Under a filter `e` edited and `ctrl+d` deleted a monitor the user was
not looking at. Same family as D24, and the ninth and last copy of the block.
`TestAFilteredSelectionEditsTheRowTheUserSees` was written before the fix and
failed on the old code.

**D24 — workspaces acted on the wrong directory under a filter. Fixed** by §2
step 5, which is also what found it. The rows were filtered and `m.entries` was
not, and every action resolved the cursor against `m.entries` — so under a
filter they acted on whatever sat at that index in the *unfiltered* list.
Filtering five entries down to `empty-dir` and pressing `ctrl+d` asked to delete
`devdesk`. `ctrl+s`, `r`, `enter`, `ctrl+o` and `ctrl+w` were all wrong the same
way. This is the defect class the component was built to remove, stated in its
package doc, and it was live in the one view where the consequence is a deleted
directory. `TestAFilteredSelectionActsOnTheRowTheUserSees` was written before
the fix and failed on the old code.

**D23 — an unreachable repository manager read as "not a group". Fixed** in
§3.8 step 3, which is also what found it. `NexusDetector.fetchRepoMeta` returned
one bare `ok=false` for both "the manager answered no" and "the manager could
not be asked", and `DetectGroup` collapsed both into `nil, nil`. Harmless while
the answer was discarded on every browser open; not harmless once step 3 cached
it, since one unreachable minute would have erased what was last known. The
error now propagates and a failed discovery is not written through.

**D21** also sat in that package but was **independent of §3.8**, and was fixed
without waiting for it — see §1.1.

D20 and D22 are fixed — see §1.1.

D15–D19 were found by the phase 5 pass, were unrelated to §3.8, and are fixed —
see §1.1. Each had been recorded with an inverted test asserting the broken
behaviour; those tests are what failed when the fix landed, and each has been
turned around to assert the fixed behaviour instead.

**D12 — `AuthEnabled` has no effect on browse or discovery. Fixed** (§3.8 step
2); the description below is what it was. The flag is
honoured in exactly three places: the `docker login` fired on form submit, the
`Logged` column, and the URL list `registryLoginStatusCmd` checks. Neither code
path that actually talks to a registry consults it. `submitSearch`
(`browser_keys.go:142`) calls `docker.GetStoredCreds(credURL)`
unconditionally and hands the result to `searchRegistryTagsCmd`;
`detectRegistryGroupCmd` (`commands.go:563`) does the same before calling into
`registrymgr`. A registry the user has marked as needing no authentication will
still have the host's stored credentials sent to it whenever any other registry
on that host has been logged into — which, given that Docker keys credentials by
host, is the normal case for a Nexus instance. This is the defect the
`anonymous` mode in §3.8 exists to make expressible; today there is no way to
say "do not send credentials here" at all.

**D13 — the browser cannot be dismissed while it is resolving.**
`handleKeyMsg` returns `nil` for every key in `browserStateResolving`
(`browser_keys.go:70`), `esc` included, and that state is entered
unconditionally on open whenever any registry is configured. The detections run
concurrently with an 8 s timeout each, so the wedge is bounded at roughly eight
seconds — but it is eight seconds during which the application ignores the user,
on a screen they may have opened by mistake. §3.8 removes the state rather than
the symptom: with members read from config and cache, there is nothing to
resolve and the form renders immediately.

A smaller thing in the same handler, not worth its own entry:
`HandleGroupDetected` matches the incoming result against `reg.URL`, so two
registries configured with the same URL collide and the second result overwrites
the first. The slug introduced in §3.8 is the natural key to match on instead.

**D14 — the registry filter shows a raw URL for group members.**
`registryFilterLabel()` (`browser_tags.go:63`) resolves the active filter by
searching `b.registries`, which holds only the configured top-level entries.
Discovered members are not in that list, so filtering to one falls through to
returning `b.registryFilter` — the full synthesised URL — where every other row
in the same view shows a short alias. The fix follows from §3.8 rather than
preceding it: once members are persisted they are resolvable, and the filter
gains a group level at the same time.

Pinned inverted by `TestAGroupMembersFilterLabelIsStillARawURL`, which asserts
the raw URL today and asserts the configured registry's alias alongside it as
the contrast.

**D21 is fixed** — see §1.1. It was the last entry in this section.

---

## 2. Technical debt

### Test coverage

**80.7 % overall — the agreed 80 % target is met.**

| Phase | Scope | Status |
|---|---|---|
| 0 | `internal/ui/testutil` Bubble Tea harness | **done** (100 %) |
| 1 | Leaf components and pure helpers | **done** except the `ui/theme` complement (~55 stmts) |
| 2 | Mid-size view state machines (`status`, `containers`, `dashboard`, `gitlab/auth`) | **done** |
| 3 | Large views (`workspaces`, `explorer`, `security`, `netdiag`) | **done** |
| 4 | `ui/oci_resources` | **done** — 43.4 %, the rest deferred to phase 6 |
| 5 | Router and I/O seams (`app`, `scan`, `docker`) | **done** — `app` 90.5 %, `scan` 99.2 %, `docker` 65.4 % |
| 6 | Remainder to reach 80 % | **done** — 73.8 % → 80.7 % |

**Phase 5 was complete when phase 6 began.** The three packages needed three
different seams: `app` needed only a constructor that does not read the
terminal, `docker` needed `dockerRunner`, and `scan` needed `commandRunner` plus
pure argument builders. None of them needed a mocking library.

**Phase 6 was one package.** `internal/ui/oci_resources` held 1 509 of the
2 900 uncovered statements in the project, so it was the only one that had to
move: 43.4 % → **73.0 %**, which carried the total from 73.8 % to 80.7 % on its
own. It was left at 43.4 % in phase 4 on the grounds that the rest "wants the
seam phase 5 builds"; that turned out to be half right. What `commands.go`
needed was not the `dockerRunner` seam — which is unexported and therefore
unreachable from this package — but the *other* technique phase 5 produced.

**A fake tool on PATH reaches further than a seam does.** `internal/scan`
installed copies of the test binary as `trivy` and `docker` to steer detection;
the same trick covers `commands.go` end to end, including the error handling in
`internal/docker` underneath it, with no export added anywhere. One extension
was needed: a single fake answers many different commands here — `docker image
ls` and `docker network inspect` reach the same file — so the reply is selected
by **the longest matching invocation prefix** (`faketool_test.go`) rather than
being fixed for the process. A second env var makes each fake append its
invocation to a file, which is what lets a test assert *what docker was asked*;
that is how the "untagged images are scanned by ID, cached by name" rule is
pinned.

The registry half needed no seam at all: every entry point takes the base URL as
an argument, so `httptest` covers the tag search, the bearer-token exchange and
Nexus group detection. `fetchDockerHubTagsMeta` is the one exception — it builds
a `hub.docker.com` URL itself — and swapping `ociHTTPClient` for one whose
transport rewrites the host covers it without changing production code.

`tea.Sequence` had to be taught to `testutil.Msgs`. `tea.Batch` answers with the
exported `tea.BatchMsg`, but the sequence equivalent is unexported, so a
sequenced command reported as one opaque message and the commands inside it
never ran — which is why `scanOneImageCmd` sat at 4.3 % with tests around it.
Its underlying type is `[]tea.Cmd`, so reflection recovers them without
depending on the name (`testutil.sequenced`). That is what made the scan
commands testable, and D20 is what fell out of testing them.

What is deliberately left uncovered in `oci_resources`, at 73.0 %: the
launch-form renderers and the remaining `keys.go` / `view.go` branches. Those
are layout, and pinning them means pinning pixels.

The packages still below target are `internal/registrymgr` (18.5 % at the time,
**30.6 %** since §3.8 step 4 covered the dispatch), `internal/oci` (37.3 %),
`internal/status` (64.8 %), `internal/docker` (65.4 %) and `internal/cache`
(71.3 %) — none of which the 80 % figure needs. `oci` is the one still worth
doing on its own merits: its gap is the registry HTTP paths that need a
manifest-plus-gzipped-layer fixture rather than the single-response stubs used
so far. What remains uncovered in `registrymgr` is the Nexus REST client, which
*is* exercised — from `oci_resources`, against `httptest`, where the command
that calls it lives; per-package coverage just does not count it.

Phase 1 progress:

| Package | Before | Now |
|---|---|---|
| `internal/ui/testutil` | — | **100 %** (new) |
| `internal/ui/shortcut` | 0 % | **100 %** |
| `internal/ui/help` | 0 % | **96.7 %** |
| `internal/ui/components` | 0 % | **80.2 %** |
| `internal/oci` | 0 % | **37.3 %** |
| `internal/docker` | 7.3 % | **65.4 %** (phase 5, pulled forward — see below) |
| `internal/gitlab` | 7.5 % | **100 %** |
| `internal/credentials` | 29.5 % | **95.9 %** (98.2 % before §3.9 doubled the package) |

`internal/oci` stops at 37.3 % because the remaining statements are registry HTTP
paths (`DownloadTemplate`, `listCatalog`, `ListTemplates`) that need a fuller
`httptest` fixture — a manifest plus a gzipped layer — rather than the
single-response stubs used so far.

Phase 5's blocker is cleared for `docker`: the package now routes every CLI
invocation through the `dockerRunner` seam in `internal/docker/exec.go`, so tests
drive argument building and output parsing against canned output.

`internal/scan` (0 % → **99.2 %**) closed the phase, and it is the package where
the three-step order earned its keep most visibly. The split extracted
`commandRunner` and the pure argument builders — which is what closed D19 — and
the completion pass immediately failed on a defect the split had just
introduced: the report was read before `cmd.Wait()`, so every scan came back
empty (§1.1). The seam existed for a full commit before anything used it, and
that gap is exactly where the defect lived.

Two techniques from it are reusable:

- **The test binary stands in for the tool.** `TestMain` notices a set of
  environment variables and, instead of running the suite, behaves as a scanner
  does — a report on stdout, progress on stderr, a chosen exit code. That is what
  covers the *production* runner (`cliRunner`) rather than only the code above
  the seam, with no trivy or gitleaks installed and no compiler at test time.
- **Detection is steered through `PATH`.** `CheckDependenciesWithImages` probes
  the machine, which is precisely what a test must not do. Copying the test
  binary into `t.TempDir()` as `trivy`, `gitleaks` or `docker` and pointing
  `PATH` at it makes every branch reachable and deterministic — binary preferred
  over image, image absent, daemon unreachable, version unreadable. A per-argument
  refusal knob is what separates `docker images` from `docker run` when both are
  the same fake.

Five statements are left uncovered and stay that way: two error paths in
`AddToGitleaksIgnore` that need an injected filesystem, and a `StderrPipe`
fallback that `Run` cannot reach. Covering them costs more structure than the
branches are worth.

`internal/gitlab` needed no seam: every function takes a `*gitlabclient.Client`
built from a base URL, so an `httptest` server standing in for the API covers
the whole package. `Clone` is exercised against a throwaway local repository
rather than mocked, and skips when no `git` binary is on `PATH`.

Phase 2, complete:

| Package | Before | Now |
|---|---|---|
| `internal/ui/status` | 0 % | **93.9 %** |
| `internal/ui/status/components` | 0 % | **93.2 %** |
| `internal/ui/containers` | 0 % | **87.2 %** |
| `internal/ui/dashboard` | 0 % | **81.5 %** |
| `internal/ui/gitlab/auth` | 0 % | **94.5 %** |

The view layer needed no seam either, for the reason `internal/ui/testutil`
documents: constructors are pure and `Update()` is a pure function, so feeding
synthetic messages fully determines the resulting state. No test executes a
command Update returns — they shell out to Docker, hit the GitLab API, sleep for
a second or open a browser — so the assertions are on model state instead. The
few places that must touch disk (`config.Load` after a save, `config.Save`)
redirect `HOME` and `USERPROFILE` at a temporary directory, the same trick
`internal/credentials` uses.

Three constraints the phase surfaced, worth knowing before phases 3–5:

- **Colour has to be forced to test styling.** Under `go test` lipgloss detects
  no TTY, falls back to the Ascii profile and strips every escape sequence — so
  any assertion about colour passes whatever the code does. `containers` calls
  `lipgloss.SetColorProfile(termenv.TrueColor)` for the tests that need it and
  restores it afterwards; that is what makes the Rule 122 check (no escape
  sequences in `table.Row` cells) real rather than vacuous. Verified by styling
  a cell on purpose and watching the test fail. `termenv` moved to a direct
  dependency for this.
- **`bubbles/table` keeps its styles unexported**, so `refreshSelectionStyle` is
  asserted by looking for the error-selection escape sequence in the rendered
  table rather than by reading `Styles()`.
- **`sort.Slice` is not stable**, so fixtures must give every sortable column a
  total order or the expected sequences are ambiguous.

Phase 3, in progress:

| Package | Before | Now |
|---|---|---|
| `internal/ui/netdiag` | 15.9 % | **86.1 %** |
| `internal/ui/workspaces` | 0 % | **81.3 %** |
| `internal/ui/gitlab/explorer` | 0 % | **91.5 %** |
| `internal/ui/security` | 0 % | **83.7 %** |

**Phase 3 is complete.** The three-step order held on all four: surface pass
58.8 % / 60.3 % / 58.1 % / 52.9 %, split with the figure unchanged to the
statement every time, completion pass to 86.1 % / 81.3 % / 91.5 % / 83.7 %.

`workspaces` added one technique worth reusing: its filesystem commands
(`createWorkspace`, `deleteEntry`, `renameEntry`, `loadEntries`, `enrichEntry`)
are **executed** rather than asserted on identity, against `t.TempDir()` and a
throwaway git repository. That is what proves the branch, remote and dirty-tree
counters are read correctly; a stub would only prove the stub works. It skips
when `git` is not on `PATH`, like `internal/gitlab` does. Only the Docker- and
desktop-backed commands are left alone.

`explorer` extended that to the API layer, which is why it reaches 91.5 % —
higher than either of the others despite having the largest untestable-looking
surface. The rule that emerged: **execute the command whenever the dependency
can be stood up locally**, and only fall back to asserting on model state when
it cannot.

| Dependency | Treatment |
|---|---|
| GitLab API | executed against `httptest` — the SDK takes a base URL |
| OCI registry | executed against `httptest` |
| git | executed against a seeded repository, skipped without `git` on `PATH` |
| Desktop browser | not executed; the guard branches are driven, the launch is not |

The clone tests are the clearest case: the "GitLab host" is a local directory
holding seeded repositories, so the pipeline really clones and really writes
the directory tree. That is what proves the tree mirrors the group hierarchy,
that an existing checkout is skipped rather than clobbered, and that a group
whose children were never browsed is walked mid-run. `cloneURL` was extracted
to make the SSH and HTTPS URL shapes assertable — `gitlab.Clone`
reports only an exit status, so the URL it was handed is not observable through
the error.

Phase 4, `internal/ui/oci_resources`, complete: 0 % → **43.4 %**, with the
surface pass at 25.1 % and the split leaving it unchanged to the statement.
Phase 6 took it the rest of the way, to **73.0 %**.

It was the one package that stopped short of the 80 % target, and deliberately.
What remained was `commands.go` (every `docker` invocation),
`connectivity_form.go` and the launch-form renderers — roughly 1 200 statements
judged at the time to want the seam phase 5 builds rather than more view tests.

Worth correcting, because the reasoning was half wrong and the correction is
reusable: `connectivity_form.go` (196 statements, 0 %) needed **nothing at
all**. It is a self-contained form whose only I/O is one command it returns and
never runs, so it was testable the whole time and was skipped by association
with the files around it. Judge a file by its own dependencies, not by the
package it sits in.

The three-step order held here too, on the largest package of the lot: 6 039
lines across eleven files, of which `update.go` (1 707) and
`registry_browser.go` (918) were the last two over the ceiling. The surface pass
was 25.1 %, well below the 52–60 % the phase 3 packages reached, and that turned
out not to matter: what a split needs is a net under *the code being moved*, and
`update.go` was at 60/86 functions when it was cut. Judge the surface pass by
the target file, not by the package total.

`security` added the last variant of the same idea: its cache commands write to
`~/.devdesk`, so `TestMain` redirects `HOME` and `USERPROFILE` at a temporary
directory for the whole package and the purge and save commands are **executed**.
That redirect is not optional there — every checkbox toggle calls `config.Save`,
so without it the tests would rewrite the developer's own configuration.

Its most worthwhile tests are not about the state machine at all: they are about
`extractMeaningfulLines`, which reduces a wall of Trivy and Gitleaks stderr to
the one line that explains a failure. What it *discards* — INFO lines, progress
bars, the doubled `Fatal error / run error:` wrapping — is the whole feature,
and the fallback that shows the raw text rather than an empty panel is what
stops a novel log format leaving the user with nothing.

Two handlers are deliberately left uncovered in `containers`: `s` and `S` call
`detectShell`, which runs `docker exec` synchronously *inside* `Update`. The
tests drive those keys only in states that return before reaching it. The same
shape appears in `status.reloadConfigAndCheck`, which calls `config.Load()` from
`Update`. Neither is a Rule 110 violation — nothing mutates the model from a
`Cmd` — but I/O in `Update` blocks the event loop and is untestable without a
seam. Worth a look when phase 5 gets to the router.

`internal/credentials` needed no seam either. `git credential` is steerable
through the environment, so the tests redirect `HOME`, `GIT_CONFIG_GLOBAL` and
`GIT_CONFIG_NOSYSTEM` at a temporary directory and run the real binary against a
throwaway `store` helper — nothing reaches the developer's keychain. Running the
real git is what surfaced the context-isolation defect recorded in §1.1; a stub
would have frozen the broken behaviour instead. The 2 s timeout is covered by
installing a credential helper that sleeps.

The host secret store added by §3.9 has a seam already, supplied by the library:
`keyring.MockInit()` swaps `zalando/go-keyring`'s provider for an in-memory one
and `MockInitWithError` for one that fails, which is how "no D-Bus session on a
headless box" is tested on a developer machine that has a working keychain. It
is a package-level global, so no test in `internal/credentials` may run in
parallel — the cost of the seam being free.

### Files over the 800-line ceiling

The project's own coding rules cap files at 800 lines. **None now exceeds it.**

`internal/app/app.go` (1552 lines — the 1333 recorded earlier was stale) was the
last one. It was split into `keys.go`, `command_line.go`, `context.go`,
`theme.go`, `gitlab.go`, `scan_details.go`, `selection.go`, `help_overlay.go`
and `view.go`; the largest is 242 and `app.go` itself is 337. Unlike the earlier
splits this one was not purely mechanical, and the coverage figure moved with it
(72.4 % → 74.7 %): the two `maybe*` handlers, the two cached-result paths, the
two delegated-scan handlers and the two picker overlays were each one function
duplicated twice, and folding them together removed statements rather than
covering them. `New()` also gained a `newWithSize(cfg, w, h)` seam so the
constructor can be exercised — `New` itself reads the terminal size from
`os.Stdout` and panics when there is none, which is always the case under
`go test`.

`internal/ui/oci_resources/update.go` (1707 lines — the 1556 recorded earlier
was stale) was split into `keys.go`, `images.go`, `resources.go`,
`registries.go`, `results.go`, `launch.go`, `table.go`, `layout.go` and
`browser_bridge.go`; `update.go` itself is 364. `registry_browser.go` (918) was
split into `browser_keys.go`, `browser_tags.go`, `browser_state.go` and
`browser_view.go`; it is 243. The largest file in the package is now
`commands.go` at 598, which was already under the ceiling.

`internal/ui/security/model.go` (1991 lines, the largest file in the project)
was split into `update.go`, `form.go`, `scan.go`, `findings.go`, `details.go`,
`view.go`, `warnings.go`, `header.go` and `messages.go`; the largest is 299 and
`model.go` itself is 245. `warnings.go` is the one worth noticing: the
scan-error parser is pure string handling with no dependency on the model at
all, and pulling it out of a 2000-line file is what made it obvious it deserved
tests of its own.

`internal/ui/netdiag/model.go` (1114 lines) was split into `validation.go`,
`update.go`, `run.go`, `view.go` and `header.go`; the largest is now 314 lines
and `model.go` itself is 172. `topology_model.go` stays at 752 — under the
ceiling, and its parsers and renderer belong together.

`internal/ui/workspaces/model.go` (1299 lines) was split into `messages.go`,
`table.go`, `entry.go`, `actions.go` and `update.go`; the largest is 450 and
`model.go` itself is 129. `entry.go` is the one worth noticing: the pure and
filesystem-only helpers now sit together instead of at the bottom of a
1300-line model, which is what made them straightforward to cover.

`internal/ui/gitlab/explorer/model.go` (1402 lines) was split into `update.go`,
`table.go`, `navigation.go`, `pull.go`, `create.go`, `delete.go`, `api.go` and
`messages.go`; the largest is 227 and `model.go` itself is 206. `api.go` is the
one worth noticing: gathering every GitLab call into one file is what made the
`httptest` pass straightforward — the routes to fake are visible in one place
rather than scattered through a 1400-line model.

`internal/docker/client.go` (1176 lines — the 1069 recorded earlier was stale)
was split into `exec.go`, `containers.go`, `images.go`, `networks.go`,
`volumes.go`, `registry.go`, `launch.go`, `system.go` and `parse.go`; the largest
is now 269 lines. The former `network.go` became `ports.go`, since it reports the
host's listening sockets via `ss` rather than Docker networks — the name was
free for the `docker network` family.

This interacts with the coverage work: writing several thousand statements of
tests against these files before splitting them freezes their current structure.
Decide the order deliberately. Splitting `docker` first was the cheap case — its
7.3 % coverage meant almost no tests were pinned to the old shape.

**The order settled on for phase 3 is: surface tests, then split, then complete
coverage** — per package, so each split has a net under it without the tests
being written against a layout that is about to change. It held across all four
packages, with the coverage figure unchanged to the statement every time
(58.8 %, 60.3 %, 58.1 %, 52.9 %), including on the 1991-line `security/model.go`.
Use it for phases 4 and 5. The discipline that makes it work is
asserting on behaviour rather than on internals — no test named a file, and the
only ones that reach into the model do so for state the view has no other way to
expose.

It held for `internal/app` too (9.3 % → 72.4 % → split → 90.5 %), with one
qualification worth recording: the coverage figure moved across that split, from
72.4 % to 74.7 %. That is not drift in the tests — it is the only split so far
that also deduplicated, and removing a duplicated branch removes uncovered
statements. When a split is purely mechanical the figure should still be
identical to the statement; when it is not, say which it was.

The router is where this pass paid for itself. Four of the five defects it found
(D15–D18) are invisible from any single view: `esc` swallowed before it is
forwarded, a documented command the parser rejects, a completion catalogue that
has drifted from the parser, a header that overflows its window. A view's own
tests drive its `Update` directly and so never see the router at all. All five
are now fixed (§1.1); the `esc` one turned out to be holding two views' notion of
"editing" hostage, which no view could have reported on its own either.

### Table plumbing is written out again in every view — **closed**

**Rien ne reste.** `internal/ui/datatable` est le seul mécanisme de table de
l'application : **17 instances dans 8 paquets**, et `bubbles/table` n'est plus
importé hors du paquet que pour son type `Styles` (`containers/model.go`,
`security/findings.go`, `theme/styles.go`). Une nouvelle table s'y écrit ; il
n'y a pas de seconde façon d'en construire une.

L'inventaire ci-dessous est ce que l'entrée décrivait à son ouverture — quinze
tables câblées à la main, dont le *look* était partagé et la mécanique jamais.
Il est conservé parce que les six étapes qui le suivent disent ce que chaque
migration a trouvé, et que deux d'entre elles ont trouvé un défaut réel (D24,
D25).

| Paquet | Tables (à l'ouverture de l'entrée) |
|---|---|
| `oci_resources` | `imageTable`, `networkTable`, `volumeTable`, `registryTable`, `tagTable` (browser), `table` (network inspect) |
| `status` | `monitorTable`, `sslTable` |
| `netdiag` | `resultsTable`, ports `table` |
| `containers`, `explorer`, `security`, `workspaces` | one each |

**Ce qui est arrivé après « quinze sur quinze ».** Le décompte de quinze était
juste et n'était pas final : quatre tables n'avaient jamais été inventoriées ici
et sont parties en **§3.21** — Registries, les tags du registry browser,
network-inspect et les résultats netdiag. **§3.22** a ensuite ajouté au composant
la notion de ligne occupée (`Key`, `StatusColumn`, `MarkBusy`), et l'arbre du
viewer (**§3.25**) est né `datatable` — d'où 17 plutôt que 19.

Une exception subsiste, documentée plutôt que subie : `workspaces` garde sa
propre notion d'occupé (`scanningPaths`, `syncingPaths`, `deletingPaths`), parce
que scan et sync s'y excluent par dépôt, qu'un sync vise un arbre et non une
ligne, et que le spinner n'y dépense pas la même cellule selon l'opération.
C'est l'étape 2 de **§3.23**, où la réponse « ça reste une exception » est tenue
pour légitime.

#### What was duplicated

| Concern | Copies | Where |
|---|---|---|
| Column-width arithmetic (Rule 116) | 12, ~290 lines | `oci_resources/layout.go:51-131` (×4), `status/update.go:186-217` (×2), `containers/update.go:872-886`, `workspaces/view.go:54-96`, `security/findings.go:128-144`, `netdiag/ports_model.go:314-330`, `explorer/model.go:180-205`, `registry_browser.go:229-260`, `network_inspect_form.go:93-112` |
| Sort comparator scaffold | 3 | `explorer/table.go:81-113`, `containers/update.go:631-667`, `oci_resources/table.go:33-67` |
| Sort arrows in headers | 3, verbatim | `explorer/table.go:130-166`, `containers/update.go:800-832`, `oci_resources/table.go:130-148` |
| Text-filter matching | 5 | `workspaces/table.go:31-42`, `explorer/table.go:15-31`, `oci_resources/table.go:17-30`, `netdiag/ports_model.go:274-302`, `containers` `filteredContainers` |
| Cursor clamp on row shrink | 2 of 8 | present: `workspaces/table.go:69`, `explorer/table.go:54` — absent elsewhere |
| `getSelectedX()` | 9 | `containers/update.go:236`, `oci_resources/images.go:33`, `registries.go:14`, `resources.go:47,56`, `browser_state.go:97,106`, `status/update.go:230` |

The three sort comparators are the same eight lines around a different `switch`;
so are the three arrow blocks, down to the `sortColIndex` / `baseTitles` maps and
`arrow := " ▲"`. The five filter loops all lowercase the query and run
`strings.Contains` over N fields.

#### The width clamps broke the invariant they existed to protect

Rule 116 requires `sum(col_widths) == available` so the selected row reaches the
right viewport border. Every site enforces it the same way — last column absorbs
the remainder — and then several add a per-column `max(…, floor)` *after* the
remainder is computed, which silently pushes the sum over `available`.

`workspaces/view.go` is the clearest case. With `numColumns = 11`,
`available = width - 24`, and 105 columns of fixed width, `Remote` clamps at 10
and `Modified` clamps at 15 (`view.go:64,83-85`). **Below a 154-column terminal
the widths sum to 130 against an `available` that is smaller** — 96 at width 120,
an overflow of 34. Same class at `security/findings.go:143` (below 72 columns),
`registry_browser.go:240` (`flexTag` floors at 8, so the last column can go
negative), `oci_resources/layout.go:66,89,108,123` and
`netdiag/ports_model.go:325`.

One solver that distributes the *shortfall* across flexible columns instead of
clamping each one independently removes the whole class. It is also the only way
to test the invariant once rather than eleven times.

#### The cursor was coupled to the pipeline by hand

Each `getSelectedX()` replays filter-then-sort to map a cursor back to a domain
object:

```go
sorted := m.sortedImages(m.filteredImages())
return &sorted[m.imageTable.Cursor()]
```

Nothing ties that ordering to the one `updateImageTable` used to build the rows.
If they drift, the action lands on the wrong object with no error. This is the
duplication worth removing on correctness grounds rather than volume.

The missing clamp is the same coupling seen from the other side: `oci_resources`
compensates with `GotoTop()` on every filter toggle
(`keys.go:56,127,158,192,224`), which throws away the scroll position;
`containers` and `status` do neither.

#### Proposed shape — `internal/ui/datatable`

`bubbles/table` takes `[]table.Row` (plain `[]string`), so a purely declarative
config cannot resolve a cursor back to a domain object — the column has to know
how to extract from `T`. Go 1.25, so generics are available:

```go
type Column[T any] struct {
    Title    string
    MinWidth int                 // floor
    Flex     int                 // 0 = fixed at MinWidth; >0 = share of the leftover
    Cell     func(T) string      // plain text — Rule 122 by construction
    Less     func(a, b T) bool   // nil = not sortable
    Search   func(T) string      // nil = not searchable
}

type Config[T any] struct {
    Columns     []Column[T]
    Tokens      []components.FilterToken
    TokenMatch  func(item T, active map[string]bool) bool
    DefaultSort int
    RowState    func(T) string   // -> theme.TableStylesForState / ForSeverity
}

func New[T any](cfg Config[T]) Model[T]

func (m *Model[T]) SetItems(items []T)         // filter + sort + rows + clamp, one path
func (m *Model[T]) Selected() (T, bool)        // replaces the nine getSelectedX
func (m *Model[T]) Resize(width, height int)   // Rule 116, once
func (m *Model[T]) Update(tea.Msg) (Model[T], tea.Cmd) // ↑↓/jk, pgup/pgdn, g/G, `.`, `/`
func (m *Model[T]) FilterBar() *components.FilterBar   // for RenderFooter / GetFooterHeight
func (m *Model[T]) InEditMode() bool
```

The point is not the line count — roughly 500 lines out of the views against
~280 in the component, so the net saving is modest. The point is that Rules 116,
122 and 136 stop being conventions checked in review. A `Cell func(T) string`
gives styled text nowhere to go; a single solver makes the width invariant
testable; `SetItems` is the only place a cursor can be left dangling.

What stays in the views: column definitions and their extractors, domain actions
(`ctrl+d`, `ctrl+s`, `enter`), tabs, forms, and the explorer's drill-down —
sorting and filtering already apply to the current level only.

#### Three that will not fit the config cleanly

- **`status`** — two tables sharing one viewport with alternating focus
  (`DefaultTableStyles` / `BlurredTableStyles` per tab, `update.go:364-371`).
  Two `datatable.Model` plus a focus helper, not a multi-table abstraction.
- **`security/findings`** — filters by tab *and* severity before the text query,
  and resets the cursor to the top on tab change (`findings.go:53`), which is the
  opposite of what `SetItems` should do by default. Needs an explicit reset call.
- **`netdiag` ports** — the lazy rebuild (`tableReady` / `lastTableWidth`,
  `ports_model.go:346-378`) exists to keep scroll position across a 2 s tick.
  That is exactly what `SetItems` must guarantee, so the code goes away — but it
  is the migration step that has to prove it.

#### Suggested order

One view per PR, risk ascending:

1. `datatable` plus tests (width invariant, clamp, sort, filter) — no view migrated
2. `oci_resources` networks + volumes — simplest, no sort
3. `netdiag` ports — proves scroll preservation on live data
4. `containers`, then `oci_resources` images — prove sort, arrows, `RowState`
5. `workspaces`, `explorer` — prove clamp and drill-down
6. `security`, `status` — the two special cases

Step 1 is worth landing on its own: the width solver and its test pin the
invariant before any view depends on it, which is the ordering the phase-3
coverage work already showed pays off (surface tests, then move, then complete).

#### Step 1 as built — `internal/ui/datatable`

**The open question is settled, and the sketch was wrong about it.**
`RowState func(T) string` assumed per-row styling. There is no such thing:
`TableStylesForState` and `TableStylesForSeverity` both only alter `Selected`,
and both are re-applied from the *cursor's* item — `containers` and `security`
have the same `refreshSelectionStyle`, each replaying filter-then-sort by hand to
find out what is under it. So the field is

```go
SelectedStyles func(T) table.Styles
```

The view returns the styles for the selected item; the component never learns
what a severity or a container state is, which is what the question was really
asking. It also names what it is — bubbles/table has no per-row styling, and
that absence is *why* Rule 122 exists.

What the component holds that the views did not:

- **One filtered, sorted slice**, kept. `Selected()` reads it instead of
  recomputing, so the cursor cannot point at one ordering while the screen shows
  another. That was the defect class worth removing, not the line count.
- **One width solver** (`widths.go`). It distributes the *shortfall* across the
  columns instead of letting each defend its own floor, so the Rule 116 sum holds
  at every width. `TestTheWidthsAlwaysSumToWhatIsAvailable` sweeps six layouts
  across widths 0–200; the workspaces shape that overflows by 34 columns at width
  120 has its own test.
- **Cursor clamping in one place.** `SetItems` clamps both ends and otherwise
  leaves the cursor alone, which is what `netdiag`'s lazy-rebuild workaround
  exists to achieve. `GotoTop` stays explicit for `security`'s tab change.

Two defects were found by the tests while writing it, both mine, both in code the
views would have inherited: the cursor did not come back from `-1` when rows
returned after an empty filter, and `CycleSort` got stuck flipping the direction
of a column with no comparator. The second is fixed by settling the invariant in
`New` — `sortColumn` is `-1` or sortable, never anything else — which let the
matching guards in `sorted` and `nextSortable` be deleted rather than covered,
per the D5 precedent.

`Cell func(T) string` is what makes Rule 122 structural: a styled value has
nowhere to go. 98.9 % covered; the package total went 81.6 % → 81.9 %.

#### Step 2 as built — `oci_resources` networks and volumes

Both tables are `datatable.Model[T]` now. What left the view: two
`resize*Table` functions, two `update*Table` functions, two `getSelectedX`
bodies, and the `networks` / `volumes` slices — the tables hold their own items,
so there was no second copy left to drift.

Two things the migration turned up:

- **`/` had to stop being unconditional.** Neither tab searches, and neither
  renders the filter bar in its footer, so a component that claimed `/` would
  have opened a search whose result — everything filtered out — the user could
  not see the reason for. `Update` now ignores `/` unless something is
  searchable, and `Searchable()` is exported so a view can decide whether to
  advertise it (Rule 130).
- **The volumes table was one of the overflowing copies.** It clamped its last
  column at 20 after the remainder was computed, which is the exact shape the
  backlog describes. It is the solver's job now.

`TestTheResourceTablesHoldTheWidthInvariant` checks the sum across six widths,
and was confirmed to bite by removing the `Resize` call: `networks at width 60:
the columns sum to 56, want 50`.

#### Step 3 as built — `netdiag` ports

The step that had to prove `SetItems` preserves scroll on live data, because the
`tableReady` / `lastTableWidth` / `lastTableHeight` trio existed for nothing
else: the table refreshes every two seconds and rebuilding it threw away where
the user was looking. **All three fields are gone**, along with `applyFilters`,
`tableColumns`, `buildRows`, `rebuildTable` and the `filtered` slice —
`ports_model.go` lost 290 lines and gained the config.

`TestScrollSurvivesTheTwoSecondRefresh` is the one that matters, and it was
confirmed to bite by adding a `GotoTop` after `SetItems`: `cursor = 0 after a
refresh, want it left at 2`.

Two things this view forced into the component:

- **A row-level search pass.** netdiag matched a query against all six fields
  joined; the other four filter loops matched per field. Per field alone would
  have quietly dropped `"tcp 22"`-shaped matches on migration, so
  `matchesQuery` now tries each column *and* the joined row. That is a superset
  of either behaviour, so no view loses matches and the other five gain the same
  thing when they migrate.
- **`TokenMatch` got its first real client.** The proto and state groups are OR
  within a group and AND between them, and `numeric` / `paused` are tokens that
  report a mode rather than filtering. `matchesActive` makes the distinction
  that matters: a group with nothing on does not filter at all, which is not the
  same as matching nothing.

`internal/ui/netdiag` is at 85.6 %; the project total moved 81.9 % → 81.8 %,
the difference being the component's statements now counted against a view that
no longer has its own.

Steps 4–6 stand as written.

#### Step 4 as built — `containers`, then `oci_resources` images

The step that had to exercise `SelectedStyles`, the field whose design was
settled in step 1 and which nothing in production used. It holds: containers is
the only one of the fifteen tables that varies its selection colour by row, and
`refreshSelectionStyle` — which replayed filter-then-sort on *every cursor move*
to find out what the cursor was on — is now four lines taking a
`docker.Container` and returning `table.Styles`.

What left the two views: two `sortField` enums, two `sortableColumns` slices,
two `cycleSort` functions, two sort-arrow blocks (each two maps and a loop
rewriting ten headers), two `sorted*` comparators totalling seventeen cases,
two `filtered*` loops, both `getSelectedX`, and four width calculations.

Three things the migration turned up:

- **The images width copy could go negative.** It clamped Name at 20 and handed
  the entire shortfall to Scanned, which is `available - 76` below that clamp —
  negative under 96 columns. The Rule 116 *sum* was still right, which is why it
  was never caught: the total lands on the nose while one column is -6 wide.
  `TestColumnsFitTheWidth` sweeps from 60 now and checks each column is
  non-negative, not just the total.
- **The state is not a column** in containers — it is the icon prefixed to the
  image — but the filter has always matched it. The Image column searches image
  and state both, which is the same shape of decision as step 3's joined row:
  the migration is where a behaviour with no column of its own gets noticed.
- **A row type rather than a captured pointer.** The images table shows the scan
  cache, whether a scan is running, and the alias-substituted name — none of
  which lives on `docker.Image`, and none of which the columns can reach,
  because they are built once in `New`. `imageRow` carries the decoration, so
  the `C` column sorts by the same number it prints where the old comparator
  looked the entry up a second time. `Selected()` returns the row and the view
  takes `.Image` off it.

Behaviour gained, in both: the cursor is clamped when a filter shortens the list
— `TestSelectionResolvesThroughSortAndFilter` no longer needs its `SetCursor`
call — and `pgup`/`pgdown` work (Rule 111). In images, the alias the Name column
actually shows became searchable; it was not before, which reads as a bug the
moment the column says one name and the query wants the other
(`TestTheFilterMatchesBothTheAliasAndTheRawName`, confirmed to bite).

`containers` 84.9 %, `oci_resources` 76.1 %. Five of fifteen tables migrated.

Steps 5 and 6 stand as written.

#### Step 5 as built — `workspaces`, then `explorer`

The step that was meant to prove the clamp and the drill-down. It proved
something else first: **workspaces was resolving every action against the
unfiltered list** (D24 above). The rows were filtered, `m.entries` was not, and
seven copies of

```go
idx := m.table.Cursor()
if idx < 0 || idx >= len(m.entries) { return m, nil }
entry := m.entries[idx]
```

each turned a cursor into the rows into an index into a different list. `ctrl+d`
on a filtered list named a directory the user could not see. That is exactly
what the package doc describes as "the duplication worth removing on correctness
grounds rather than volume", and it was not hypothetical.

`selectedIdx` went with it. The modal that read it back after the user confirmed
was the second place the two lists could disagree, and a row index means nothing
once the list it indexed is not the list on screen — `pendingEntry` holds the
entry.

The explorer was the well-behaved one: `visibleItems()` already sorted and
filtered before resolving. It still had two:

- `expandToPath` walked `currentItems()` and set the cursor to the index it
  found there. Under a non-default sort that is a different ordering from the
  rows, and *both indices are in range*, so nothing clamped the mistake away —
  creating a resource highlighted whichever one shared the index. Confirmed to
  bite: `cursor is on "sub", want legacy`.
- Four handlers guarded with `cursor >= len(items)`, which lets bubbles' `-1`
  through. `ctrl+d` on an empty group indexed `[-1]`. They read `Selected()`
  now, which has one failure mode and returns it as a bool.

Both width calculations were wrong in the way this refactor keeps finding, and
in opposite directions. Workspaces clamped Modified back up to 15 *after*
handing it the remainder, so the columns overflowed by up to 34 at width 120 —
the shape `TestTheWorkspacesLayoutFitsANarrowTerminal` was written against in
step 1. The explorer's seven ratios kept the sum exact and starved the columns
instead: at 80 they gave Type 5 and Created 8, neither wide enough for its own
header. A correct sum is not a correct layout, and only one of the two is what
Rule 116 actually says.

Three things the views forced into the component:

- **`SetCursor`, clamped.** Workspaces restores a position per directory level
  on the way back up, and the explorer does the same after a refresh.
- **The selected row is pinned to the content width.** Workspaces did this by
  hand (`styles.Selected.Width(m.width - 2)`) and it was the right instinct:
  column widths count cells, and a Nerd Font icon does not always render as wide
  as it counts, so the highlight stopped short of the border by whatever the
  icons disagreed by. It is a no-op when they agree, so every table gets it.
- **`rebuild` sets the cursor to `-1` on an empty list** rather than leaving the
  old index. An empty list was the one state where the cursor could still point
  past the end — `Selected()` reported nothing either way, but the invariant is
  worth having whole.

`workspaces` 81.7 %, `explorer` 90.1 %, `datatable` 96.2 %. Seven of fifteen
tables migrated.

#### Step 6 as built — `security`, then `status`

The two the plan set aside as not fitting the config cleanly. Both turned out to
fit — by keeping something the component deliberately does not model.

**`security`** filters by tab and by severity before any query, and resets the
cursor to the top when the tab changes. Both stay in the view, and that is the
right answer rather than a concession: the tab and the severity decide which
findings *exist*, where a `FilterBar` query narrows a list that is already
settled. So the view filters and hands the result over, then calls `GotoTop`
explicitly — the call `SetItems` deliberately does not make. It is the second
client of `SelectedStyles`, colouring the selected row by the severity under the
cursor.

**`status`** is two tables sharing a viewport with alternating focus, and it is
two `datatable.Model` plus a four-line `applyTabFocus`, exactly as the plan
guessed. `Focus` and `Blur` carry the styles, so the four `SetStyles` calls at
every tab switch went with them. The one text query drives *both* tables so the
header counts agree with each other whichever tab is showing, so the query stays
in the view too — same call as security's, for the same reason.

**And status was carrying D25.** `getSelectedComponentIndex` sorted and then
indexed without ever applying the filter the rows had been through. It is the
ninth and last copy of the block, and the second of the nine that was actually
wrong. Two out of nine is the answer to whether this refactor was worth doing on
correctness grounds: the duplication was not equivalent, it had drifted, and
nothing said so.

Two smaller things:

- The findings Title was truncated by hand at `width-3` before going into the
  row. bubbles truncates every cell to its column width with the same ellipsis
  (`table.go:429`), so this only ever cost three characters of title. Nothing
  else depended on the width when building rows, so the resize handler stopped
  rebuilding them — and `NewWithPreloadedResult` stopped needing a
  `WindowSizeMsg` to fill its table.
- `matchesQuery` settles an inconsistency nobody chose: status' monitor loop
  matched name, target and type; the SSL loop left type out.

`security` 85.5 %, `status` 93.1 %.

#### §2 done — fifteen of fifteen

What it removed, across the six steps: twelve width calculations (five of them
wrong — three that overflowed, two that starved), nine `getSelectedX` (two of
them wrong, D24 and D25), eight sort-arrow blocks, six `sortField` enums and
their `cycleSort`, eleven filter loops, and the `tableReady` trio.

What it bought is not the line count — roughly 1500 lines out of the views
against 560 in the component and its tests. It is that Rules 116, 122 and 136
stopped being conventions checked in review. `Cell func(T) string` gives styled
text nowhere to go. One solver makes the width invariant testable, and every
view now sweeps it from a width narrow enough to hurt. `SetItems` is the only
place a cursor can be left dangling, and `Selected()` reads the slice the rows
were built from, so the two cannot part.

Both defects it found were the same shape and neither was hypothetical: filter a
list, act on the highlighted row, watch the wrong object get deleted.


#### Colour in the cells — `datatable` renders its own rows

The sentence above — "`Cell func(T) string` gives styled text nowhere to go" —
was right about the danger and wrong about the price. Fifteen tables in one
palette read as fifteen tables with nothing to say, and the three commented-out
`theme.StatusOKStyle.Render(...)` lines in `status/view.go` were what that cost
looked like: somebody wanted a colour, hit Rule 122, and gave up.

It was never a Bubble Tea limitation, and not a lipgloss one either. It is one
line of `bubbles/table`:

```go
m.styles.Cell.Render(style.Render(runewidth.Truncate(value, width, "…")))
```

The value is measured *before* it is styled, and runewidth counts an escape
sequence's bytes as width — `"running"` in a colour measures 28 against 7
visible. So it is truncated in a column twice wide enough, the cut lands inside
the escape, and the unterminated sequence bleeds down the table. `bubbles
v1.0.0` — the latest, still on bubbletea v1 — has the identical line, so no
upgrade reaches it.

`render.go` inverts the order instead: `Cell` stays plain and is what gets
measured, `Style func(T) lipgloss.Style` colours the finished cell. The failure
becomes unexpressible rather than forbidden by review, which is the same trade
the rest of §2 made. bubbles keeps the state — rows, columns, cursor, focus,
height — and what moved is the drawing and the scroll offset its viewport kept
unexported.

Two properties fell out of it that were not obvious from the outside:

- **The selected row ignores `Style`.** It is handed to `styles.Selected` whole,
  and a colour inside closes with a reset that takes the selection background
  with it for the rest of the line — the highlight would stop mid-row. It
  renders exactly as it did before, and a test compares it character for
  character against the uncoloured table.
- **Every cell off the selected row carries an explicit background.** Plain
  cells emitted nothing, so the app's viewport style covered them; the first
  coloured cell would have stripped that background from everything to its
  right. A column declaring only a foreground gets `ColorBackground` filled in,
  so Rule 115 cannot be half-implemented per column.

The discipline matters more than the mechanism: a zero count, a `-` and a
never-scanned target are `DimStyle`, and the nominal majority state — a
`running` container — keeps the default text colour. Colouring it would put a
colour on the whole table and a signal on none of it.


### The race detector sees only what the tests run

**It runs locally now.** `mise run test-race` needs cgo and therefore a C
compiler on `PATH`; without one it fails with `cgo: C compiler "gcc" not found`,
which is what this entry used to be about. The Windows development machine has
one — `gcc 16.1.0` (MinGW-w64, `x86_64-posix-seh`), with `go env CGO_ENABLED`
reading `1` and `CC` reading `gcc` — so the check is available before a push
rather than only after one.

Measured on 2026-08-14 with `go test -race -count=1 ./...`, so nothing came from
the test cache: **no data race, across all 29 packages that have tests** (three
have none). It costs two to three minutes wall-clock, and two packages are most
of it — `internal/ui/oci_resources` at 47 s and `internal/scan` at 33 s.

**CI still runs it, and that is not redundant.** `.github/workflows/ci.yml` runs
`mise run test-race` on every push and pull request on `ubuntu-latest`. A race is
a scheduling accident, so a second machine with a different core count and a
different scheduler is a second sample rather than a repeat of the first — and
CI is what covers a contributor whose machine has no toolchain.

What has not changed is the limit worth keeping in mind: **the detector only
sees code the tests actually execute**, and coverage is 80.7 %. Rule 110
violations in untested paths remain invisible whichever machine runs it. The two
efforts compound, so this stays an argument for the coverage phases rather than a
substitute for them.

`internal/scan` is the package it has most to say about, since `Scanner.Scan` is
the only place in the application that fans out to concurrent goroutines writing
one shared result. Its tests drive all five stages at once, so that fan-out is
under the detector — and now under it locally, where a `Cmd` is being changed,
rather than only where the change is being reported on.

---

## 3. Planned features

Carried over from `todo.md`, except §3.7.

### 3.1 Network diagnostics

- **Port-forwarding manager** — an interactive dashboard to manage port
  redirections to the host machine.

### 3.2 Interactive security remediation

- **Auto-patch assistance** — after a Trivy scan, offer to generate a patch or an
  updated `Dockerfile` that bumps the base image version to clear critical CVEs.
- **Local SAST** — wire security linters (Gitleaks for secrets, IaC linters)
  directly into the Workspace view.

### 3.3 OCI build and cache analyser

- **Layer visualisation** — analyse a local image and show the size of each
  layer, in the spirit of `dive`. Intended approach:
  - Integrated via `google/go-containerregistry` ("daemonless", no dependency on
    the Docker daemon).
  - Talk to OCI registries directly, pulling manifests and configs without
    downloading the whole image.
  - Stream layers in memory (`tar`) to rebuild the filesystem tree.
  - Compute wasted space by detecting whiteout files (`.wh.`).
- **Cache-miss analysis** — explain why a build's cache was invalidated, e.g.
  "`package.json` changed, invalidating the cache".

### 3.4 Spontaneous dev containers

- **Configuration injection** — inject the developer's dotfiles (vim, zsh,
  aliases) when opening a terminal inside a container.
- **Hot volume mount** — mount the current working directory into a running
  container on the fly, to test a local script without rebuilding the image.

### 3.5 Resource dashboard (embedded mini-htop) — **superseded by §3.19**

Asked for braille charts of live CPU, RAM and network I/O **for every container**,
plus user-defined saturation thresholds.

§3.19 keeps the charts and moves them. Per-container series belong to the
`containers` view, where a row *is* a container; the dashboard has no row to hang
one on, so it shows aggregates. The visual alerts are not carried over — a
threshold is a setting, and settings live in the configuration view, so it needs
its own entry rather than a line here.

### 3.6 GitHub support alongside GitLab, one active forge per context — **done**

Support GitHub as well as GitLab, with **exactly one backend active per
configuration context**. A context targets one forge; switching forge means
switching context.

**Les neuf étapes sont faites.** Un contexte déclare sa plateforme dans
`forge.type`, `internal/forge` dit ce que DevDesk lui demande, deux backends y
répondent, et l'application entière — vues, vocabulaire, configuration,
commandes — parle de « la forge » plutôt que de GitLab.

Ce que chaque étape a trouvé est consigné sous le découpage ; l'entrée est
conservée en entier parce que la moitié de sa valeur est ce qu'elle a fallu
corriger en cours de route. Quatre décisions de l'énoncé se sont révélées
fausses en écrivant le code, et chacune est nommée là où elle a cédé :

| Ce que l'entrée disait | Ce qu'il a fallu faire |
|---|---|
| l'étape 2 met l'interface dans `shared.State` | impossible tant que rien ne l'implémente : le backend GitLab passe devant (étape 1) |
| le vocabulaire est porté par `shared.State` | il se résout depuis la configuration : l'écran « non authentifié » a besoin des mots avant qu'une session existe (étape 5) |
| le cycle de forge n'aura qu'une valeur | il en a deux depuis l'étape 4, ce qui a permis de tester la bascule pour de vrai (étape 6) |
| rendre la complétion consciente de la forge | sans objet : un écran nommé d'après le rôle n'a rien à filtrer par plateforme (étape 8) |

Cinq défauts trouvés en chemin, tous corrigés : **D52** (un compteur du
dashboard illisible affiché `0`), une ligne de `.claude/CLAUDE.md` fausse depuis
le commit initial, l'ordre de la migration `gitlab:` → `forge:` qui perdait
silencieusement un réglage, **D53** (l'explorer GitHub vide sur un compte
personnel) et **D54** (un token GitLab proposé en exemple sur GitHub).

**Les deux derniers sont venus de la première utilisation réelle, pas d'une
relecture**, et chacun dit quelque chose de différent :

- **D53** est le seul qui ait échappé à la relecture *et* aux tests. Le
  raisonnement qui l'a produit était écrit et argumenté — ce qui le rendait
  convaincant — et il portait sur une différence qui n'existe pas.
- **D54** a échappé à une garde écrite pour l'attraper. `vocabtest` cherche les
  *noms* des plateformes, et `glpat-` n'en nomme aucune : un préfixe de token est
  spécifique à une forge sans dire laquelle. La garde connaît les préfixes
  maintenant.

#### What was coupled to GitLab (relevé'ouverture)

| Surface | Size |
|---|---|
| `internal/gitlab` + `internal/ui/gitlab/{auth,explorer}` | ~3 900 lines |
| Direct uses of the SDK types outside `internal/gitlab` | **0** — 41 when step 3 started, 66 at the entry's opening |

Le tableau est historique : `internal/gitlab` n'existe plus, et le SDK n'est
importé que par `internal/forge/gitlab`.

The concrete SDK type leaks into `internal/shared/state.go:57`
(`GitLabClient *gitlabclient.Client`), so every consumer is bound to go-gitlab
rather than to a DevDesk abstraction. That field is the load-bearing change: an
interface there is what makes a second forge possible at all.

Also GitLab-shaped, **et tous traités depuis** : `GitLabConfig` (URL, token,
clone method, pull settings) est `ForgeConfig` sous `forge:` avec un `type:`
(étape 4), `shared.GitLabStats` est `forge.DashboardStats` (étape 3). Restent
les noms de vues `gitlab-auth` / `gitlab-explorer` et leurs alias `gla` / `gle`
dans `internal/command/parser.go`, qui sont l'étape 8.

Two findings from the design review that the count above does not capture:

**The client doubled as the authentication flag. Corrigé à l'étape 3.** Three
sites branched on `m.shared.GitLabClient != nil` to decide whether the user was
logged in, with `shared.IsAuthenticated` sitting beside them saying the same
thing. They were inside the 66 and needed a semantic change rather than a type
substitution, which is why they are called out here.

**La surface de commandes était dupliquée quatre fois. Ce n'est plus vrai —
D16/D17 l'ont effondrée**, et cette entrée l'affirmait encore. Ce qui suit est
ce qu'elle décrivait, conservé parce que c'est le raisonnement qui a rendu
l'étape 0 obligatoire :

| Emplacement | Rôle |
|---|---|
| `parser.go` `viewMap` | `ParseCommand()` — celle qui faisait autorité |
| `parser.go` `commands` | `Parse()`, héritée, doublon de la précédente |
| `parser.go` `GetAliases()` | alimentait la complétion |
| `completion.go` `buildCommands` | une quatrième liste en dur, **périmée** |

`buildCommands` proposait `gitlab-auth` mais pas `gitlab-explorer`, et omettait
`workspaces`, `security` et `net` — ces commandes marchaient et n'étaient jamais
suggérées.

Aujourd'hui `viewNames` est la seule table : `Parse`, `GetAliases`, `FullNames`
et `buildCommands` en dérivent, et deux tests tiennent les deux bouts —
`TestEverythingThatParsesCanBeCompleted` et `TestEverythingSuggestedCanBeRun`.
Ajouter une dimension de forge s'y fait en un endroit.

**Un troisième bord manquait, et il était faux depuis le commit initial.** Les
deux tests ci-dessus opposent le parser à la complétion ; rien n'opposait l'un
ou l'autre à la **documentation**. `.claude/CLAUDE.md` annonçait
« `containers` or `c` » alors que `c` a toujours résolu vers `context` — jamais
vrai, pas une dérive. C'est exactement D16 dans l'autre sens (`:netdiag`
documenté et refusé par le parser), et D16 avait été trouvé par un test.

`internal/command/doc_test.go` ferme les deux sens, sur le modèle de source-scan
d'`internal/ui/keymap` :

- `TestEveryDocumentedCommandParsesToWhatItClaims` lit les puces de la liste de
  CLAUDE.md et exige que chaque orthographe soit acceptée **et** que toutes
  celles d'une même puce mènent au même endroit — c'est la puce qui affirme
  qu'elles sont synonymes. C'est ce qui a nommé le défaut :
  `"containers" resolves to view/containers, "c" to context/`.
- `TestEveryTypeableViewIsDocumented` ferme le sens de D16 : une vue qu'on peut
  taper et que la documentation ne nomme pas est une vue que personne ne
  trouvera. Seuls les **noms complets** sont exigés — quels alias montrer est un
  choix éditorial, un nom complet absent ne l'est pas.

Les deux ont été vérifiés en échec : le premier sur la ligne `c`, le second en
retirant `netdiag` de la liste.

**Ce qui n'a pas été fait, et pourquoi.** L'étape 0 devait aussi rendre
exprimable « ça parse mais ce n'est pas suggéré ». Le mécanisme n'a **aucun
client** avant l'étape 8 — aucune orthographe d'aujourd'hui n'a de raison d'être
cachée — et une machinerie dont la liste d'exceptions est vide est précisément
ce que YAGNI interdit. Elle se fait à l'étape 8, avec les anciens noms comme
premiers clients. Ce que l'étape 0 laisse derrière elle est ce dont l'étape 8 a
réellement besoin : une seule table, et un test qui refuse une documentation qui
ment.

**Question laissée ouverte, pas tranchée en passant :** `c` abrège `context`
alors que `ctx` le fait déjà et que `containers` commence par un c. Que la
documentation se soit trompée dès le premier jour est un indice sur le mnémonique
plutôt que sur la documentation. Le code est cohérent et testé depuis toujours,
donc c'est la documentation qui a été corrigée ; réassigner `c` est une décision
de vocabulaire clavier, qui appartient à `internal/ui/keymap` et à personne
d'autre.

#### Model mismatches to settle before coding

These are not implementation details; they decide what the abstraction can even
promise.

- **Nesting — settled.** The explorer is a tree of groups → subgroups →
  projects. GitHub has no nested groups, but it does have **organisations**, and
  a user can belong to several. Flattening everything to one level was
  considered and rejected: someone in five orgs would get a wall of repositories
  with no way to tell them apart by owner. The abstraction therefore **declares
  its depth** — `MaxDepth: 1` for GitHub (orgs at level 1, repositories at level
  2), unbounded for GitLab. This costs nothing in the UI: the drill-down already
  handles two levels, and a user with no organisation simply sees a flat list.
- **Deletion.** `DeleteGroup` / `DeleteProject` implement GitLab's two-step
  permanent delete (schedule, then purge under the renamed
  `-deletion_scheduled-<id>` path). GitHub deletes immediately and has no
  equivalent, so the "permanent" checkbox is meaningless there. The `locked`
  flag added in §1.1 is the hook for that: a GitHub backend would set it and
  leave the box out of reach, as the already-scheduled GitLab case does.
- **Dashboard counters.** `FetchDashboardStats` reads `X-Total` from five list
  endpoints. GitHub has no equivalent header for these; the counts come from the
  search API (`search/issues?q=is:open+is:pr+assignee:@me`), with different rate
  limits and semantics.
- **Vocabulary.** Group/project/merge request vs organisation/repository/pull
  request. The UI must pick per-backend labels or a neutral vocabulary; Rule 129
  applies either way.

#### Settled: vocabulary, commands, screens and forge selection

The organising distinction, which the mismatches above blur: the two forges
differ in **words** and in **shapes**, and only the first is a presentation
problem.

| Difference | Kind | Handled by |
|---|---|---|
| "Group" vs "Organization" | word | vocabulary table |
| "Merge Request" vs "Pull Request" | word | vocabulary table |
| Token label, placeholder, help URL | word | vocabulary table |
| Icon and display name | word | vocabulary table |
| Nested namespaces | **shape** | declared depth (above) |
| Visibility set — 3 values vs 2 | **shape** | forge-supplied option list |
| Role model — int levels vs strings | **shape** | backend returns a humanised role |
| Two-step permanent delete | **shape** | the `locked` flag, §1.1 |
| Dashboard counters | **shape** | different endpoints, above |

Words are cheap and settle in one pass. Shapes are the actual work, and neither
neutral nor per-forge wording helps with them. Keeping the two apart is what
stops the vocabulary layer from quietly growing conditionals.

**Vocabulary is per-forge, not neutral.** A GitLab user says *group*, a GitHub
user says *repository*; "namespace" is a third language nobody speaks, and it
makes the application read as an abstraction layer rather than a tool. The
wording lives in a single `Vocabulary` value per forge — name, icon, namespace
singular/plural, repository, change-request, token label and placeholder, help
URL, visibility set, role names — resolved once from the active context and
carried on `shared.State`, which every view already receives.

The rule that keeps it maintainable: **no view interpolates a forge name into a
string literal.** That is enforceable the way this project already pins
invariants — a test grepping `internal/ui` for `"GitLab"` / `"GitHub"`, on the
model of the existing check that every key in `GetShortcuts()` appears in
`GetHelpContent()`.

**Sites déplacés — vingt-cinq, pas douze.** Le relevé à la main donnait la liste
ci-dessous ; le scan des sources qu'a écrit l'étape 5 en a trouvé le double, dont
l'aide de trois vues, le message de changement de backend de secrets et la ligne
« Navigation » du dashboard. La liste est conservée pour ce qu'elle disait de
chaque site, pas pour son décompte.

| Location | Literal |
|---|---|
| `explorer/view.go` | `IconGitlab + " GitLab Explorer"` |
| `explorer/view.go` | `"GitLab not authenticated … with :gitlab-auth (or :gla)"` — nom **et** commande |
| `explorer/view.go` | `"No groups found … any GitLab groups."` |
| `explorer/view.go` | `"Loading GitLab groups..."` |
| `explorer/view.go` | `nodeTypeLabel()` → `"Group"` / `"Project"` |
| `components/creation_form.go` | `resourceTypes = []string{"Group", "Project"}` |
| `auth/view.go` | `IconUser + " Gitlab Authentication"` — mauvaise icône *et* mauvaise casse à côté de celle de l'explorer |
| `auth/view.go` | `"GitLab URL: "` (affichage en lecture seule) et son libellé |
| `auth/view.go` | `"not configured — set it in :config, gitlab tab"` — commande **et** nom d'onglet |
| `auth/view.go` | titre, description, et le texte affirmant que le token `must` commencer par `glpat-` |
| `dashboard/view.go` | `IconGitlab + " GitLab"`, `"Authenticate with :gitlab-auth"` |
| `dashboard/view.go` | `"Merge Requests:"` |
| `configuration/fields.go` | le titre d'onglet `"gitlab"` |

Two of these are shapes wearing a word's clothes. `AccessLevelName()`
(`tree.go:52`) maps GitLab's numeric levels to Owner/Maintainer/…; GitHub uses
`admin`/`maintain`/`push`/`triage`/`pull`, which do not align one-to-one — so the
**backend returns an already-humanised role string** rather than an integer the
UI translates. And `internal` visibility does not exist on GitHub.com, so
`CreationForm`'s three hardcoded values have to come from the forge. Nothing to
undo for the token prefix: `glpat-` appears only in help text, never validated.

**Un seul écran d'authentification, et il s'appelle `git-auth`.** Il n'y en a
jamais deux : un contexte cible une forge, donc un écran qui demanderait
laquelle serait un écran qui ignore ce que le contexte dit déjà. La commande est
`git-auth` / `ga`, et l'explorer — qui sert les deux forges de la même façon —
est `git-explorer` / `ge`. Le préfixe `git-` est ce qui rend les deux devinables
l'une depuis l'autre.

`ViewType` reste stable : c'est une clé de `a.views` et un `case` du routeur, et
le renommer ferait toucher au routeur à chaque forge ajoutée. Seuls les noms
tapés changent.

Ce qui parse, et ce qui est suggéré, ne sont pas la même liste :

| Tapé | Parse | Suggéré |
|---|---|---|
| `git-auth`, `ga` | oui | **oui** |
| `git-explorer`, `ge` | oui | **oui** |
| `gitlab-auth`, `gla`, `github-auth`, `gha` | oui | non |
| `gitlab-explorer`, `gle`, `github-explorer`, `ghe` | oui | non |

Délibérément permissif : il n'y a qu'une vue d'authentification, donc `gla` tapé
dans un contexte GitHub doit y mener plutôt qu'échouer. Punir la mémoire
musculaire ne rapporte rien. **Le filtrage se fait à la complétion, pas à
l'analyse** — les anciens noms marchent toujours et ne sont plus proposés.
C'est aussi ce qui rend l'étape 0 (une seule table de commandes) obligatoire
plutôt que souhaitable : quatre listes désynchronisées ne peuvent pas porter
une distinction parse/suggère.

Les messages qui citent une commande (`explorer/view.go:258`,
`dashboard/view.go:85`) citent la nouvelle orthographe ; une fois le vocabulaire
centralisé, c'est un champ de plus sur la même structure.

**L'écran d'auth s'adapte, il ne choisit pas.** Il lit la forge du contexte et
en tire son titre, son icône, le libellé et le placeholder du token, l'URL
d'aide sur les scopes, et le nom cité dans ses messages. Il possède le token et
l'acte de se connecter, rien d'autre — c'est la ligne que §3.9 a tracée et elle
ne bouge pas.

**Donc la forge se choisit là où l'URL se règle : dans la vue configuration.**
La version précédente de cette entrée mettait la forge en champ 0 du formulaire
d'auth, au-dessus de l'URL. Ce n'est plus possible et c'est mieux ainsi : §3.9 a
retiré l'URL de l'auth view — les deux vues l'écrivaient, donc aucune ne faisait
autorité — et la forge est exactement le même genre de réglage. Un champ qui
gouverne le placeholder d'un autre doit être dans le même écran que lui.

**Un seul onglet de forge, nommé d'après la forge active.** `gitlab` devient
`github` quand le contexte cible GitHub ; il n'y a pas deux onglets dont un
serait inerte. C'est la même décision que pour l'écran d'auth, et pour la même
raison : un onglet `github` visible mais sans effet dans un contexte GitLab dit
le contraire de « une seule forge par contexte ».

```
┌ app ┬ github ┬ scan ┬ network ┬ status ┐

  Connection
● Forge            󰅂 github
  URL              󰅂 https://github.com
  Default org      󰅂 acme
  Default visibility 󰅂 public

  Clone
  Clone method     󰅂 https
  Parallel jobs    󰅂 8
  ☐ Include archived repositories
```

Les sept champs sont ceux d'aujourd'hui ; deux seulement dépendent de la forge,
et chacune est une **forme**, pas un mot :

- **Le jeu de visibilité** est fourni par la forge. `internal` n'existe pas sur
  GitHub.com, donc le cycle a trois valeurs ici et deux là. C'est le premier
  client réel de « la forge déclare sa forme ».
- **« Default parent group » / « Default org »** est un mot, pas une forme : le
  champ pointe le même réglage, le vocabulaire décide de son libellé.

**La section du fichier devient `forge:`, avec un `type:`.** Le précédent est
`docker:` → `network:` (§3.34) : `applyDefaults` porte `gitlab.*` vers
`forge.*` **avant** de remplir les défauts, puis vide l'ancien bloc pour qu'il
quitte le fichier à la sauvegarde suivante. L'ordre est tout : `yaml.Unmarshal`
n'est pas strict, donc un bloc non migré est perdu en silence — et ici le silence
donnerait un contexte pointant sur gitlab.com par défaut. `type` absent vaut
`gitlab`, ce qui est ce que tout fichier existant veut dire.

**La détection propose, l'utilisateur dispose.** Le sniffing d'URL seul n'est pas
fiable : `github.com` et `gitlab.com` sont triviaux, mais l'auto-hébergé est le
cas qui compte et `git.acme.com` peut être l'un ou l'autre. Sonder
(`/api/v4/version` vs `/api/v3/`) coûte un aller-retour et échoue sur les
instances qui authentifient ces endpoints. Un sélecteur obligatoire seul est une
friction sur les deux cas les plus courants. Donc :

- ~~**Forge est le premier champ**~~ **(fait, étape 6)** Forge est le premier champ du groupe **Connection**, au-dessus de l'URL —
  c'est ce qui permet à tout ce qui suit de se reconfigurer sous les yeux de
  l'utilisateur.
- C'est un **champ à cycle** (`←` / `→`, Rule 132), pré-rempli par détection sur
  l'hôte.
- La détection **rejoue quand l'URL change, et seulement tant que l'utilisateur
  n'a pas touché au champ Forge** — un drapeau *dirty*. Sans lui, la détection
  écrase un choix explicite, ce qui est la différence entre serviable et
  envahissant.
- Le préfixe du token (`glpat-` contre `ghp_` / `github_pat_`) est un second
  signal, utilisé pour **avertir** dans l'écran d'auth, jamais pour basculer :
  la forge a déjà été choisie ailleurs, et un écran qui ne la choisit pas ne
  peut pas la changer.
- **Changer la forge ferme la session du contexte**, exactement comme changer
  l'URL le fait déjà — un seul drapeau, `ForgeChanged`, parce que la conséquence
  est une. C'est le même appel :
  on ne l'interdit pas, on dit ce que ça a fait. Le gel « une fois authentifié »
  de la version précédente était la bonne intention au mauvais endroit — la
  session est ce qui devient faux, pas le réglage.

`RegistryForm` est le précédent pour le refus plutôt que la correction : une
URL dont l'hôte contredit la forge déclarée n'est pas réécrite, elle est
signalée.

#### Settled: SDKs Go, et les CLIs ne sont pas dans le périmètre

**Décision : `go-gitlab` et `go-github`.** DevDesk parle aux forges en HTTP,
depuis son propre process, avec ses propres tokens. `gh` et `glab` ne sont ni
requis ni invoqués — ni pour l'API, ni pour lire un token.

Ce que les CLIs avaient pour elles était réel : l'authentification est déjà
résolue chez elles, device flow OAuth compris ; `gh api` et `glab api` sont des
passe-plats REST/GraphQL, donc aucun endpoint hors abstraction n'est hors de
portée ; et il n'y a pas de version de SDK à suivre pour deux forges.

Quatre choses ont pesé plus lourd, et la première est décisive :

- **L'isolation par contexte est incompatible avec la façon dont ces outils
  stockent l'auth.** Les deux tiennent un état global par hôte
  (`~/.config/gh/hosts.yml`), là où un contexte DevDesk veut *un token différent
  pour le même hôte*. Piloter `gh auth switch` depuis le TUI muterait l'état
  global de l'utilisateur — exactement la faute que §1.1 a évitée en cantonnant
  `credential.useHttpPath` à l'invocation. La voie propre est `GH_TOKEN` /
  `GITLAB_TOKEN` par appel, mais alors DevDesk possède toujours les tokens et le
  bénéfice principal a disparu. **C'est un contrat de l'application qui est en
  jeu, pas une préférence** : « un contexte, ses identifiants » est ce que §3.9
  et les caches par contexte ont construit.
- **Deux dépendances dures de plus.** Aujourd'hui DevDesk a besoin de `git`, et
  de `docker` seulement pour ce qui s'en sert. Exiger `gh` **et** `glab` pour la
  fonctionnalité forge — qui est le cœur — est une régression de friction
  d'installation, sur toutes les plateformes à la fois.
- **Un spawn de process par appel**, contre une connexion HTTP réutilisée. Le
  dashboard émet cinq appels à lui seul et l'explorer pagine (D34 : `listAll`
  parcourt *toutes* les pages). Un clone de groupe en fait des centaines.
- **La testabilité régresse.** `internal/gitlab` est à **100 %** sans aucune
  couture, parce que `gitlabclient.WithBaseURL` accepte l'URL d'un
  `httptest.Server` — `fakeGitLab` dans `client_test.go` enregistre chaque
  requête. Passer par un exécutable remettrait le paquet dans la position dont
  `internal/docker` n'est pas sorti (65,4 %), avec une couture fabriquée à la
  main ; et une sortie de CLI mise en dur encode des hypothèses sur l'outil au
  lieu de tester contre lui.

**La source de token optionnelle est écartée aussi, et c'est le seul point qui
change par rapport à la recommandation.** Elle proposait, quand un contexte n'a
pas de token, d'en lire un avec `gh auth token --hostname <host>` ou
`glab auth status`. C'est séduisant et ça coûte trois choses qui ne se voient
pas au moment de l'écrire : une dépendance molle — absente, elle donne un
chemin qui marche chez l'auteur et pas chez l'utilisateur ; **une seconde façon
pour un secret d'entrer dans le store**, alors que §3.9 vient d'en supprimer une
(l'option qui écrivait dans deux backends à la fois) ; et un token dont la
portée a été décidée ailleurs, pour d'autres besoins, sans que DevDesk sache
dire lequel. Rien n'empêche l'utilisateur de coller la sortie de
`gh auth token` dans le champ ; c'est la même commodité sans le couplage.

Ce que la décision rend vrai pour la suite : **l'abstraction `forge` n'a pas
besoin de couture d'exécution.** Chaque backend est un SDK derrière une
interface, chacun prend une URL de base, donc chacun se teste contre un
`httptest.Server` comme `internal/gitlab` le fait déjà — et l'étape 6 (déplacer
GitLab derrière l'interface) conserve sa couverture au lieu d'en négocier une.

`go-github` s'ajoute à `go.mod` à l'étape 7, pas avant : les étapes 0 à 6 se
font contre GitLab seul.

#### Sketch of the work

0. ~~**Une seule table de commandes**, dont `Parse`, `GetAliases` et
   `buildCommands` dérivent.~~ **Faite** — la table l'était déjà (D16/D17), et
   ce qui manquait était le troisième bord : `internal/command/doc_test.go`
   oppose désormais CLAUDE.md au parser, dans les deux sens. Il a trouvé une
   ligne fausse depuis le commit initial. La distinction « parse mais n'est pas
   suggéré » est reportée à l'étape 8, où elle a un client.
1. ~~Définir l'abstraction `forge` à partir de ce que le code consomme
   réellement.~~ **Faite** — `internal/forge` : les types du domaine,
   l'interface, et `Shape`. Ce que le fait de l'écrire a tranché est plus bas.
2. ~~Implémenter le backend GitLab en déplaçant le code existant derrière
   l'interface.~~ **Faite** — `internal/forge/gitlab`, seul endroit de DevDesk
   qui sache que go-gitlab existe. Ce qu'il cache et ce qu'écrire l'a trouvé
   sont plus bas.
3. ~~Remplacer `shared.State.GitLabClient` par cette interface.~~ **Faite** —
   `Forge` et `CurrentUser` sont dans `shared.State`, `internal/gitlab` et
   `explorer/api.go` ont disparu, et D52 est corrigé au passage. Détails plus
   bas.
4. ~~`GitLabConfig` devient `ForgeConfig`, section `forge:` avec un `type:`.~~
   **Faite** — migrée depuis `gitlab:` en tête d'`applyDefaults`, et l'ordre
   s'est révélé être tout le sujet. Détails plus bas.
5. ~~Extraire la `Vocabulary` et y déplacer chaque littéral du tableau
   ci-dessus, avec le test qui les empêche de revenir.~~ **Faite** — et le test
   a trouvé plus de sites que le tableau n'en listait. Détails plus bas.
6. ~~**L'onglet de configuration devient adaptatif** : titre et jeu de
   visibilité tirés de la forge active, champ `Forge` en tête du groupe
   Connection, détection sur l'hôte avec son drapeau *dirty*, et la fermeture de
   session quand il change.~~ **Faite.** Détails plus bas.
7. ~~Implémenter le backend GitHub. C'est ici que `go-github` entre dans
   `go.mod`, et nulle part avant.~~ **Faite** — et l'abstraction a tenu :
   l'interface n'a pas bougé d'une méthode. Détails plus bas.
8. ~~Renommer les vues et les commandes en `git-auth` / `ga` et
   `git-explorer` / `ge`, garder les anciennes qui parsent sans être suggérées,
   et rendre la complétion consciente de la forge.~~ **Faite** — sauf le
   dernier tiers, qui s'est dissous. Détails plus bas.

Les étapes 5 et 6 tiennent seules et améliorent le code sans que GitHub soit en
vue, comme l'étape 0 l'a fait. Les étapes 1–6 sont un refactor de code qui
marche, avec les tests déjà en place, et c'est ce qui rend l'étape 7 abordable.

#### Ce que l'étape 1 a tranché

Cinq décisions, chacune parce que le code les demandait :

- **L'identité est opaque, le chemin ne l'est pas.** Un `Namespace` et un
  `Repository` portent un `ID` et un `Path`. L'`ID` adresse l'objet auprès du
  backend et ne veut rien dire dehors — GitLab a besoin d'un nombre pour créer
  sous un parent (`CreateGroupOptions.ParentID` est un `*int64`), GitHub adresse
  par `owner`. Le `Path` est celui qu'on lit, dont on construit l'URL de clone,
  et sur lequel repose la suppression permanente de GitLab. Un appelant qui
  parse un `ID` est un appelant lié à une forge ; le type est une `string` pour
  que l'arithmétique ne soit pas écrivable. Vérifié au passage : chaque
  identifiant de go-gitlab est un `any` qui accepte le chemin, donc rien
  n'oblige à garder le nombre visible.
- **Un namespace et un dépôt sont deux types**, pas un nœud avec un champ
  `kind`. Seul un namespace a des enfants ; seul un dépôt a un statut CI et une
  suppression programmée. Deux types rendent « descendre dans un dépôt »
  inexprimable plutôt qu'interdit par revue — le même troc que
  `Cell func(T) string` pour la Rule 122. Le `TreeNode` de l'explorer reste :
  *expanded*, *loading*, *parent*, *depth* sont de l'état de vue, dont une forge
  n'a pas d'avis.
- **La décoration est une option, pas un défaut.** `ChildrenOptions.Decorated`
  remplit le rôle et le statut CI, ce qui coûte **deux requêtes par dépôt** sur
  GitLab : sur un groupe de deux cents dépôts, 400 appels pour un badge et un
  rôle qu'un clone ne regarde jamais. L'explorer la demande, la marche du clone
  non — c'est la propriété que `discoverGroupChildren` avait déjà et que
  l'interface devait garder exprimable.
- **`MaxNamespaceDepth` se lit par `CanNestUnder`, jamais par comparaison.** Le
  sentinelle « ≤ 0 veut dire non borné » est un piège classique, et la
  comparaison évidente (`depth < max`) refuse en plus le namespace racine sur
  GitHub, où le maximum est 1. Deux tests tiennent les deux bouts et les deux
  ont été vérifiés en échec.
- **`DashboardStats` porte des `*int`, et ça a trouvé D52.** Les cinq compteurs
  viennent de cinq requêtes indépendantes dont chacune peut échouer seule ;
  `int` ne sait pas dire « personne n'a regardé », et le dashboard rend
  aujourd'hui un `0` grisé pour un compteur qu'il n'a pas pu lire. Le précédent
  est `Sensitive *bool` des caches de scan.

**Ce que l'étape 1 ne fait pas :** rien n'implémente encore l'interface, et rien
ne l'appelle. La complétude est vérifiée par un `stubForge` dans le fichier de
test et un `var _ Forge = (*stubForge)(nil)` — une interface que personne
n'implémente compile quoi qu'elle déclare, ce qui est la façon dont une étape
qui ne fait que définir se trompe. Le stub reste dans le test : l'étape 3 est
celle qui gagne des appelants voulant un double, et un paquet d'aide écrit avant
ses consommateurs devinerait ce dont ils ont besoin.

#### Ce que l'étape 2 a trouvé

`internal/forge/gitlab` implémente l'interface en entier, avec les tests
`httptest` du paquet `internal/gitlab` — le harnais est déplacé avec le code
qu'il exerce. C'est la propriété qui a fait choisir les SDK contre les CLIs :
le SDK prend une URL de base, donc un serveur de test tient lieu d'instance et
le backend n'a besoin d'aucune couture.

Ce que le backend cache, et que chaque élément avait fui dans une vue :
la pagination (D34), les identifiants numériques là où un chemin ne passe pas,
la suppression permanente en deux appels et son chemin renommé, et la traduction
d'un niveau d'accès en mot.

Quatre choses qui ne se lisent pas dans l'énoncé :

- **L'utilisateur est résolu une fois par listing, pas une fois par ligne.** Les
  recherches de rôle ont besoin de l'ID de l'appelant, et le demander dans la
  boucle ajouterait une requête par groupe — ce qui aurait porté à **trois** par
  dépôt une décoration qui en coûte déjà deux. `decoratingAs` le fait une fois
  et retourne 0 quand rien n'est décoré, ce qui est aussi ce qui empêche N
  requêtes de membre quand la session est morte.
  `TestADecoratedListingResolvesTheUserOnce` compte l'appel.
- **Une décoration qui échoue ne fait pas échouer le listing, un listing qui
  échoue n'est jamais une liste vide.** Les deux sens ont un test, et c'est la
  même règle que D20 vue de deux côtés : une colonne en moins vaut mieux qu'un
  explorer vide, et un explorer vide ne doit jamais vouloir dire « ce groupe ne
  contient rien ».
- **`MaxNamespaceDepth` est 0 pour GitLab**, non borné, alors que la doc GitLab
  annonce 20 sur les instances auto-hébergées. La limite est configurable et
  l'API ne la publie pas : la déclarer serait affirmer ce que le backend ne peut
  pas vérifier, et refuser une création légitime. Le serveur refuse avec son
  propre message, ce qui est mieux qu'une supposition.
- **go-gitlab réessaie les 5xx**, en backoff exponentiel — un test rendant 500
  a mis **35 secondes**, mesuré. Ce n'est pas une régression (c'est le défaut du
  SDK, déjà en place), et ça vaut d'être su : une instance en panne fait attendre
  l'utilisateur une demi-minute derrière un spinner. Les tests utilisent 403,
  qui n'est pas réessayé et qui est de toute façon l'échec réaliste — un token
  dont le scope ne couvre pas l'endpoint.

**Une correction de l'étape 1 :** `ChildrenOptions` est devenu `BrowseOptions` et
couvre aussi `RootNamespaces`. La marche du clone part d'une racine et ne paie
de décoration à aucun niveau ; l'étape 1 n'avait donné l'option qu'aux enfants,
ce qui aurait fait payer à un clone une requête de rôle par groupe racine.

**Ce que l'étape 2 ne fait pas.** `internal/gitlab` et `explorer/api.go` sont
intacts et portent encore leurs propres chemins : les déplacer demande que
`TreeNode` porte un `ID` opaque et un `Path`, ce qui est l'étape 3. Le
recouvrement est donc réel — la pagination et le listing existent en deux
exemplaires — et il dure exactement une PR. Le supprimer ici aurait voulu dire
fusionner les étapes 2 et 3 en un diff de plus de mille lignes.

#### Ce que l'étape 3 a trouvé

`internal/gitlab` et `explorer/api.go` ont disparu : le premier a été déplacé en
entier — sa session dans `internal/forge/gitlab/auth.go`, ses appels d'API dans
le backend — et le second ne garde que la conversion des types de la forge vers
le `TreeNode` que la vue rend. Le recouvrement d'une PR annoncé à l'étape 2 est
donc refermé, et la pagination n'existe plus qu'à un endroit.

- **`CurrentUser` est mémoïsé sur le backend, et ce n'est pas une optimisation.**
  Chaque listing décoré a besoin de l'ID de l'appelant pour ses recherches de
  rôle ; le demander à chaque fois ajoutait **une requête par listing** que le
  code d'avant ne faisait pas, puisqu'il lisait l'ID d'une session que la vue
  tenait déjà. Trouvé par un test de l'explorer qui rendait des lignes non
  décorées sans rien dire, parce que le faux serveur ne servait pas `/user`.
- **`IsAuthenticated` est le drapeau, `Forge` est ce qu'on appelle.** Le client
  était les deux : `GitLabClient != nil` décidait « l'utilisateur est-il
  connecté » à trois endroits, avec `IsAuthenticated` juste à côté qui disait la
  même chose. `CurrentUser` devient une **valeur** pour la même raison — une
  seconde façon de poser une question est la façon dont deux réponses finissent
  par diverger. Le header montre l'utilisateur quand il y a un **nom**, pas
  quand le drapeau est levé : une session dont l'utilisateur n'a pas pu être lu
  affichait sinon un `@` tout seul.
- **`clearAuthenticated` est le seul endroit où une session est démontée.** Trois
  sites écrivaient les trois ou quatre champs à la main — le changement de
  backend de secrets, le changement d'URL GitLab et le changement de contexte —
  ce qui est exactement la forme de D28. Ils l'appellent.
- **Le clone et la marche partagent un backend, et les tests ont dû l'admettre.**
  `cloneURL` était construit depuis `spec.gitlabURL` pendant que la marche
  utilisait `spec.client` : deux hôtes, ce que la production n'a jamais eu mais
  dont les tests vivaient. `NewWithClient(client, host)` prend les deux
  séparément, donc une seule valeur sert les deux — et c'est la production qui
  décide, pas le test.
- **Les tests déplacés ont été supprimés, pas dupliqués.** Pagination, coût de la
  décoration, dégradation d'une décoration refusée, traduction d'un niveau
  d'accès, forme de l'URL de clone : tous appartiennent au backend et y sont
  testés. `explorer/api_test.go` garde ce qui est à l'explorer — quelles options
  il demande, et comment il convertit ce qu'il reçoit.

Couverture du projet : 80,7 % → **82,1 %**.

#### Ce que l'étape 4 a trouvé

**L'ordre était le sujet, et le premier jet s'est trompé.** La migration devait
tourner « avant les défauts », ce que l'entrée disait déjà — mais elle a d'abord
été posée à côté de celle de `docker:` → `network:`, qui est en bas
d'`applyDefaults`. Or **tous** les défauts écrivent dans `cfg.Forge` : la
migration trouvait alors un bloc qui n'était plus vide, prenait le chemin
champ-par-champ, et ne migrait que l'URL — le `parallel_jobs: 7` de
l'utilisateur remplacé en silence par le 4 par défaut.

Ce n'est pas un risque imaginé pour le commentaire : c'est ce qu'a attrapé
`TestAConfigCarryingRetiredKeysStillLoads`, qui existait déjà pour une autre
raison. `migrateGitLabSection` est la **première** instruction
d'`applyDefaults`, et le commentaire dit pourquoi.

**Deux chemins, et la distinction n'existe que pour un champ.**
`IncludeArchived` est un booléen : « non renseigné » et « délibérément faux »
sont la même valeur, donc aucune garde par champ ne peut les distinguer. Il ne
peut être repris que lorsque le nouveau bloc ne dit rien du tout — d'où la copie
en bloc quand `forge:` est absent, et le champ-par-champ quand les deux sont
présents. Dans ce second cas le nouveau gagne : ses valeurs ont été écrites plus
tard, et les écraser par les anciennes annulerait l'édition qui a créé la
situation.

**Le vocabulaire est tenu par un test, pas par une convention.** `forge.type`
est écrit dans `internal/config` et dans le `Shape()` de chaque backend :
`config` ne doit pas importer un backend, et un backend ne doit pas faire
autorité sur ce qu'un fichier de configuration peut dire.
`TestTheForgeVocabularyMatchesTheConfig` est la seule chose qui puisse les tenir
ensemble — même montage que les noms de `provider` des registries (§3.8).

**La migration des secrets n'est pas touchée, et l'ordre valait d'être
vérifié :** `credentials.MigrateLegacySecrets` lit le **fichier brut** à la
construction du routeur, avant que quoi que ce soit puisse sauvegarder. Un
`gitlab.token` en clair atteint donc le store avant que le renommage ne
réécrive le fichier sans lui.

**L'onglet de configuration s'appelle toujours `gitlab`, et c'est juste.**
L'étape 6 le rend adaptatif — titre tiré de la forge active — ce qui donne
« gitlab » aujourd'hui. Les deux messages qui citent « `:config`, gitlab tab »
restent donc vrais.

#### Ce que l'étape 5 a trouvé

**Le tableau des littéraux était incomplet, et c'est le test qui l'a dit.** Il
listait douze sites ; le scan des sources sous `internal/ui` en a trouvé
**vingt-cinq**, dont l'aide de trois vues, le message de changement de backend
de secrets et la ligne « Navigation » du dashboard qui énumère les noms de vues.
Écrire la garde d'abord aurait été plus rapide que dresser la liste à la main —
c'est la leçon, et c'est la même qu'à l'étape 0.

**La vocabulaire se résout depuis la configuration, pas depuis la session.**
L'entrée disait « porté par `shared.State` ». Ça ne marche pas : l'écran « non
authentifié » de l'explorer et le titre de la vue d'auth ont besoin des mots
**avant** qu'une session existe, donc une valeur accrochée à un `Forge` vivant
manquerait exactement là où elle sert le plus. Chaque vue a un petit `vocab()`,
et la config est ce qu'elles tiennent déjà toutes.

**Un nom de commande n'est pas du vocabulaire.** C'est de l'identité de routage,
la même pour les deux forges (l'étape 8 en fait `git-auth`), donc un message qui
en cite une la construit depuis `command.ViewGitlabAuth` plutôt que de l'écrire.
C'est aussi ce qui fait qu'il survivra au renommage au lieu de lui survivre en
silence — trois messages citaient `:gla` ou `:gitlab-auth` en dur.

**L'icône reste au thème, mais les deux tables tapent sur la même constante.**
`theme.ForgeIcon` et `forge.VocabularyFor` sont deux tables parce qu'un paquet
de domaine ne doit pas importer l'UI. Elles ne sont pas tenues par un test :
elles commutent toutes les deux sur `config.ForgeGitLab` / `config.ForgeGitHub`,
donc il n'existe qu'une seule orthographe de « gitlab » dans l'application et
une forge ne peut pas être à moitié ajoutée.

**Trois défauts d'affichage corrigés en passant**, chacun invisible tant que
chaque vue écrivait ses mots :

- La vue d'auth affichait « Gitlab Authentication » et l'explorer « GitLab
  Explorer ». Deux casses pour un nom propre, sur deux titres voisins.
- L'aide de la vue d'auth affirmait que le token « **must** start with
  `glpat-` ». Rien ne le vérifie : c'est un indice, et dire « doit » de ce que
  personne n'impose est la façon dont un utilisateur en vient à croire qu'un
  token qui marche est cassé. C'est devenu « usually starts with ».
- La boîte Code du dashboard portait l'icône GitLab en dur ; elle suit la forge.

**Ce qui n'est pas dans le vocabulaire, et délibérément :** le jeu de visibilité
(une *forme*, sur `Shape`), le rôle humanisé (celui du backend, parce que les
nombres de GitLab et les mots de GitHub ne s'alignent pas), et le préfixe du
token comme *contrôle* — c'est `TokenPlaceholder`, un indice.

**Le libellé court a demandé un champ de plus.** La colonne de libellés du
dashboard fait onze cellules : « Merge Request » n'y tient pas, et le tronquer
donnerait « Merge Req… » sur une forge et « Pull Requ… » sur l'autre.
`ChangeRequestShort` porte l'initialisme — `MR`, `PR` — et c'est le seul endroit
où une abréviation valait un champ.

#### Ce que l'étape 6 a trouvé

**Le cycle n'a pas qu'une valeur, et c'est tant mieux.** L'entrée annonçait
« se fait contre GitLab seul : le cycle n'a qu'une valeur ». Faux depuis
l'étape 4 : `config.ForgeTypes()` en offre deux, et `github` est sélectionnable
avant que son backend existe. C'est ce qui a permis de tester la bascule pour de
vrai — le vocabulaire change, l'onglet se renomme, la visibilité se corrige —
plutôt que d'écrire un cas dégénéré et d'espérer.

**Deux choses suivent un changement de plateforme, et chacune serait un bug
silencieux sans elle :**

- **La table de champs est reconstruite.** Elle est calculée une fois depuis le
  type — titre de l'onglet, icône, exemple d'URL, deux libellés et la liste des
  visibilités — et le routeur **garde** cette vue lors d'une sauvegarde (c'est
  ce qui empêche une sauvegarde de jeter le curseur). Rien d'autre ne la
  reconstruirait.
- **Une `default_visibility` que la nouvelle plateforme n'a pas est ramenée à la
  plus privée.** GitHub.com n'a pas d'`internal` : un contexte qui la portait
  garderait une valeur que le serveur refuse, pendant que le champ à cycle
  s'ouvrirait sur une valeur absente de sa propre liste.

**Le drapeau `forgeTouched` est toute la différence entre serviable et
envahissant.** La détection rejoue quand l'URL est validée, mais seulement tant
que l'utilisateur n'a pas bougé le champ Forge. Sans lui, taper une URL
contredirait un choix explicite.

**`DetectType` ne reconnaît que les deux instances publiques**, et refuse de
deviner autrement. `gitlab.acme.test` et `github.acme.test` renvoient `""` —
l'auto-hébergé est le cas qui compte, et un hôte peut porter n'importe quel nom.
Le test qui vaut le plus est `TestAPathIsNotAHost` : un contrôle sur la *chaîne*
plutôt que sur l'hôte analysé dirait « GitLab » pour
`https://evil.test/gitlab.com`.

**`GitLabURLChanged` devient `ForgeChanged`, un seul drapeau pour deux
réglages.** L'URL et la plateforme ont la même conséquence — la session a été
ouverte contre quelque chose que la configuration ne décrit plus — et deux
drapeaux pour une conséquence sont deux façons de dire une chose.

**Le champ est réglé au *blur*, pas à chaque `←→`.** C'est la forme du backend
de secrets moins la question : cycler à travers la liste fermerait la session une
fois par appui, y compris au retour vers la valeur de départ. Rien n'est demandé
— changer de plateforme n'est ni interdit ni confirmé, on dit ce que ça a fait.

**Une table de moins.** `Shape` a quitté le backend pour `internal/forge`, où
elle est indexée par type : la vue configuration a besoin du jeu de visibilités
**avant** qu'une session existe, exactement comme le vocabulaire. Le `Shape()`
de l'interface délègue, donc il n'y a toujours qu'une table — la méthode reste
pour un consommateur qui tient une session et ignore le type.

#### Ce que l'étape 7 a trouvé

**L'interface n'a pas bougé d'une méthode.** C'est le résultat qui compte : elle
avait été dérivée de ce que le code consomme, contre une seule forge, et la
seconde s'y est logée sans rien y ajouter. Les endroits où GitHub ne peut pas
faire ce que GitLab fait sont ceux que `Shape` déclarait déjà.

**Un paquet de plus, et il était inévitable.** `internal/forge` ne doit pas
importer une implémentation, et un backend ne doit pas importer son frère : il
fallait quelque chose au-dessus des deux pour choisir. `internal/forge/session`
est ce switch, et `auth.go` y a déménagé depuis le backend GitLab. Le store de
secrets n'est pas de forme *forge* — `Storage` est indexé par URL — donc charger
et oublier un token est le même code des deux côtés ; seule l'ouverture d'une
session diffère, et seulement par le constructeur qu'elle appelle.

Conséquence à connaître : un contexte qui change de plateforme **sans changer
d'hôte** retrouve le token qu'il avait. C'est juste — c'est le même hôte qui
demande.

**GitHub refuse ce qu'il ne sait pas exprimer, au lieu de faire à côté.** Il n'y
a pas d'endpoint REST pour créer ou supprimer une **organisation** : les deux
renvoient une erreur qui nomme la raison, là où la tentation aurait été de créer
un dépôt sous le compte de l'utilisateur et d'appeler ça une organisation. La
suppression permanente est refusée pour la même raison : GitHub supprime tout de
suite, donc honorer le drapeau annoncerait une distinction que la plateforme ne
fait pas.

**Un commit initial, c'est un commit — et c'est pour ça que c'est quatre
appels.** `Repositories.CreateFile` en boucle produit un commit par fichier :
un template de trois fichiers arriverait en trois commits, et chacun aurait
besoin du SHA rendu par le précédent, ce qui est une chaîne et non un lot.
L'API git data prend le tout d'un coup — un blob par fichier, un arbre, un
commit **sans parent**, puis la ref. Sans parent est ce qui en fait un commit
*initial* : sur un dépôt qui en a déjà un, la création de la ref échoue, ce qui
est la bonne issue — ce n'est pas un moyen de réécrire l'historique.

**Trois décisions plus petites, chacune un défaut évité :**

- ~~**Le compte personnel n'est pas dans `RootNamespaces`.**~~ **C'était un
  bug, signalé et corrigé le jour même.** Le raisonnement — « un namespace
  personnel ne peut être ni créé ni supprimé, donc la moitié des actions se
  retireraient de la ligne » — est faux sur ses propres termes : *aucune*
  organisation GitHub ne peut être créée ni supprimée par l'API, donc le compte
  personnel n'est pas moins capable qu'une organisation mais **plus** — c'est le
  seul namespace où un dépôt peut être créé et supprimé. Le coût était le cas
  courant : un compte personnel n'appartient à aucune organisation, donc
  l'explorer s'ouvrait vide sur la forme de compte que la plupart des gens ont.
  Voir « D53 » en §1.1.
- **`visibilityOf` lit `Visibility` avant `Private`.** Le premier est ce
  qu'Enterprise remplit avec `internal` ; ne lire que le booléen rendrait un
  dépôt Enterprise `internal` comme `private`, ce qui est autre chose.
- **Un workflow en cours rapporte son *status*.** Une exécution en vol n'a pas
  de conclusion, et retomber sur la chaîne vide ferait lire « pas de CI du tout »
  pour un dépôt dont le build tourne.

**L'URL configurée est l'hôte *web*.** Sur Enterprise l'API vit sous `/api/v3/`,
que `WithEnterpriseURLs` ajoute — confondre les deux enverrait l'utilisateur sur
`https://git.acme.test/api/v3/acme/api` quand il demande à ouvrir un dépôt dans
son navigateur. Deux tests tiennent les deux bouts, dont un qui vérifie que
github.com **ne** reçoit **pas** ce traitement : son API est `api.github.com`.

**Les rôles ne s'alignent pas, et c'est pourquoi le backend les humanise.**
GitLab rend un nombre, GitHub un jeu de booléens, et l'appartenance à une
organisation GitHub est *admin* ou *member* — inventer les trois niveaux
intermédiaires pour coller à GitLab rapporterait une distinction que la
plateforme ne fait pas.

Coût : **+0,55 Mo** sur le binaire (24,4 → 25,0). Couverture :
`internal/forge/github` 89,8 %, `internal/forge/session` 100 %, projet 82,2 %.

#### Ce que l'étape 8 a trouvé

**« Rendre la complétion consciente de la forge » n'a plus d'objet, et c'est le
signe que la décision de §3.6 était la bonne.** L'entrée l'écrivait quand les
commandes étaient `gitlab-auth` et `github-auth` : il aurait fallu ne suggérer
que celle de la forge active. Avec un seul écran nommé d'après le *rôle*, il n'y
a plus rien à filtrer par forge — il reste seulement des noms retirés, qui ne
sont suggérés dans aucun contexte. Le tiers de l'étape qui disparaît est celui
que la nomenclature rendait inutile.

**Le mécanisme reporté à l'étape 0 a trouvé ses clients, et il y en a dix.**
`legacyNames` porte `gitlab-auth`, `gla`, `github-auth`, `gha`, leurs
équivalents explorer, et les plus anciens `explorer` / `exp`. Les orthographes
GitHub **n'ont jamais été acceptées avant** et sont là pour la même raison que
les GitLab : quelqu'un dont le contexte vise GitHub tapera `gha` avant `ga`, et
avoir raison vaut mieux qu'être cohérent sur ce qui existait.

Le report était juste : construit à l'étape 0, le mécanisme aurait eu une liste
d'exceptions vide.

**Le test de documentation a fait son travail au premier lancement.**
`TestEveryTypeableViewIsDocumented`, écrit à l'étape 0, a échoué en nommant
`git-auth` et `git-explorer` comme typables et absentes de CLAUDE.md — avant
qu'aucun humain ne relise quoi que ce soit. C'est exactement le défaut qu'il a
été écrit pour attraper, et il l'a attrapé sur le premier renommage venu.

**Trois renommages de plus, décidés en constatant qu'ils mentaient :**

- `internal/ui/gitlab/{auth,explorer}` → `internal/ui/forge/…`. Le chemin
  apparaît dans chaque ligne d'import de ces vues, et l'explorer parcourt GitHub
  depuis l'étape 7.
- `shared.State.GitLabStats` → `ForgeStats`. Le champ porte un
  `forge.DashboardStats` depuis l'étape 3 : un nom qui dit GitLab dans un
  contexte GitHub est précisément ce que cette entrée passe son temps à
  supprimer.
- `internal/app/gitlab.go` → `forge.go`, avec `GitLabAutoLoginMsg` →
  `ForgeAutoLoginMsg` et les identifiants du dashboard.

Ce sont des identifiants, donc invisibles à l'utilisateur — mais ils sont lus
par la prochaine personne qui cherchera où vit une session, et un fichier
`app/gitlab.go` qui ouvre des sessions GitHub est un faux indice.

**Trois messages sont restés justes tout seuls.** L'écran « non authentifié » de
l'explorer, le pied de page du changement d'URL et l'aide du dashboard citent
tous une commande : elle vient de `internal/command` depuis l'étape 5, donc le
renommage les a suivis sans qu'on y touche. C'était l'argument de l'étape 5, et
c'est la première fois qu'il se vérifie.

### 3.7 Command mode from inside a text field — **done**

`alt+:` now opens the command line from anywhere, including a focused text
input. A bare `:` keeps its old, conditional behaviour, so muscle memory
survives.

The problem: `handleKeyMsg` routed `:` through `maybeEnterInCommandMode`, which
asked the view whether it was in edit mode (`FormView.InEditMode()`) and, if it
was, forwarded the keystroke to the active input. Command mode was therefore
unreachable from any form, filter box or search field — most of the application.

The security view is the case that settles it. `InEditMode()` is true there for
the target path, Trivy server and Gitleaks config fields, and the Trivy server
placeholder is `https://trivy-server:4954` — the field has to accept **two**
colons to hold a valid value. Forwarding `:` to the input is not a bug; it is
the only correct behaviour, which is precisely why `:` cannot be the
authoritative key. The only way in was to move focus to a control that takes no
text and press `:` there, so reachability depended on which widget was focused
and nothing said so.

It was worse in `StateScanning`, `StateResults`, `StateDetails` and while a
confirm modal is open: `InEditMode()` is true and there is no field to move
focus to. `:` was forwarded to the view, which has no `case` for it — no view in
the application handles `:` itself — and dropped silently.

#### Why `alt+`, not `ctrl+`

`ctrl+:` cannot be made to work, and this is the note that should stop anyone
reintroducing it. A terminal encodes Ctrl by clearing bits, which only covers
ASCII `@` through `_` (0x40–0x5F). `:` is 0x3A: Ctrl+: sends a plain `:` or
nothing. Reporting it as a distinct key needs the Kitty keyboard protocol or
xterm's `modifyOtherKeys`, and bubbletea v1.3.10 implements neither. A
`case "ctrl+:"` would be dead code.

Alt has no such limit — a terminal sends ESC then the key, and bubbletea reports
that as the key carrying `Alt`. The alternative considered was a free
`ctrl+<letter>` (`b g l p t u v x z` are unused; `ctrl+i m j h [` are Tab, Enter,
LF, Backspace and Esc and must be left alone), which is marginally more portable
but loses the `:` in the gesture.

#### What shipped

- `altCommandModeKey` is handled in `handleKeyMsg` **before** the `InEditMode()`
  fork, so no view can claim it. The router sees every `tea.KeyMsg` first, which
  is what makes the binding unconditional.
- `enterCommandMode()` extracted; `maybeEnterInCommandMode` now only answers the
  bare `:`.
- `testutil.Key` understands an `alt+` prefix, building the key with the `Alt`
  modifier rather than the five literal runes `alt+:`. Both round-trip through
  `String()`, but only one is the message the application actually receives.
- Every `GetShortcuts()` and `GetHelpContent()` advertising `:` now advertises
  `alt+:` (Rules 114, 130, 137). The dashboard's Navigation help section carries
  the nuance in prose, so the other eight sites stay one line each.
- The header still renders `:` as the inactive prompt: it is the command line's
  visual marker, not a key legend, and `:` remains valid whenever no field has
  focus.

Two things the ripple list got wrong, corrected here: `app_test.go:30` is a
layout fixture for `buildShortcutLines`, not an assertion about the binding, so
it was left alone. And `CommandModeView` / `AllowCommandMode` was not merely
made redundant by this change — **it was already dead code**. The router only
consulted it when `InEditMode()` was true, and netdiag's implementation returns
true only on the topology tab, where `InEditMode()` is unconditionally false. It
could never fire. Deleted, along with its one implementation;
`TestCommandModeOnTheTopologyTab` became
`TestTheTopologyTabNeverBlocksCommandMode` and records why.

### 3.8 Docker registry groups — **done**

Two shapes had to be supported: a remote registry with or without
authentication, and a **group** fronting several remote registries, itself
reachable anonymously or not. The second was the one the model could not
express — the parent/child relation lived in memory for the lifetime of one
browser session, rediscovered over the network on every open, behind a blocking
spinner.

All six steps shipped. **D12, D13 and D14 are closed**, and D23 was found and
fixed on the way.

*The model as it stands is described in `.claude/CLAUDE.md` under "Registry
model", and this section is not a second copy of it.* What follows is why it has
that shape — the decisions the code is answerable to, and the three places where
building it changed one.

#### The six decisions

| Ref | Decision |
|---|---|
| A | **One list, one discriminator, one parent pointer** — `kind: registry \| group` plus `parent` on `RegistryItem`. Two parallel lists and a recursive `Children` tree were both rejected. |
| 1 | The parent is referenced by **slug**, not by URL. |
| 3 | Discovered members live in a **disk cache**, not in `config.yaml`. |
| 4 | The Registries tab gets **drill-down** (`←` / `→`), not an indented tree and not a `Group` column. |
| 5 | **No purely logical groups.** A group always corresponds to a real repository-manager group. |
| F | `provider` is a **declared field**, never sniffed from the URL. |

**A.** A Nexus group *is* a pullable registry as well as a container, so
splitting `registries` and `registry_groups` would have duplicated the form, the
table and the credential handling to model a distinction the server does not
make.

**1.** The URL was the de facto identifier, threaded through `registryLoginStatus`,
the scan cache keys and the message types — which is exactly why it is the wrong
thing to hang a parent link on: editing a group's URL would silently orphan its
members. The slug replaces it **only** as the parent link and the cache key;
everything Docker-facing stays keyed on the URL, because Docker is.

**3.** Discovered members are derived data with a server as their source of
truth, and `config.yaml` is what the user declares. The cache mirrors
`ImageScanCache` and `WorkspaceScanCache` rather than inventing a fourth
persistence shape. The `Members` column showing `count · TimeAgo` (Rule 127) is
**part of what makes the cache safe**, not decoration: a cache with no visible
age looks current whatever it holds, which would be worse than the re-detection
it replaced.

**4.** An indented tree reads faster at five registries but breaks the moment
the table is sorted or the FilterBar narrows it, and drill-down is already the
explorer's pattern.

**5.** Grouping unrelated registries under a user-invented name has no server to
discover from, no shared credential to inherit and no group URL to pull through
— a display-only concept carrying the weight of a real one. If arbitrary
grouping is wanted later it is a saved-selection feature in the browser, not a
change to the registry model.

**F.** `NexusDetector.CanHandle` returned true whenever `ManagementURL` was
non-empty, which made that field an implicit "this is Nexus" flag and the
detector list effectively single-vendor. `Detector` now states `Provider()` and
`DetectGroup` dispatches on the declared value, falling back to a
`GenericDetector` that discovers nothing. Registration order decides nothing —
there is a test that swaps it to prove so — and a plain registry costs no HTTP
call.

#### Credential inheritance: `docker login` is keyed on host

The constraint that shaped the auth model, worth stating because it is not
obvious and it settles more than it looks:

> **`docker login` takes a registry host, not a path.** `~/.docker/config.json`
> is keyed on `host[:port]`, so a group and all eight proxies behind it share
> one single credential entry, because they share `nexus.example.com`.

`AuthEnabled bool` became `AuthMode`:

| Case | Group | Member |
|---|---|---|
| Remote registry, no auth | — | `kind: registry`, `anonymous` |
| Remote registry, auth | — | `kind: registry`, `credentials` |
| Group with auth fronting proxies | `credentials` | `inherit` |
| Anonymous group | `anonymous` | `inherit` |

Three consequences, all now enforced at load rather than merely intended:

- `inherit` is not a convenience, it is the **only** thing the credential store
  can represent for a path-based group. `inherit` on an entry with no group is
  refused for the mirror reason.
- **A member-level `credentials` does not exist.** It only becomes meaningful
  for a group whose members are on different hosts, which decision 5 rules out;
  a member declaring one is refused at load, naming the two modes it may take.
  §3.9 changed what supporting it would *cost* — DevDesk now has a store keyed
  by whatever string it likes — without changing the conclusion: it would take
  DevDesk out of `docker login`'s model and into maintaining its own registry
  auth, for a case that has not been shown to exist. `RegistryItem` still has no
  password field, by construction.
- What a member *can* override is **`anonymous`** — do not send the group's
  credentials to this one. That is the safety valve, and it is the mechanism
  D12 needed.

#### Browse and pull share a URL — answered

The question left open when the model was frozen: path-based Nexus answers the
registry API at `/repository/<name>/v2/...`, but a dedicated HTTP connector port
per repository is the older arrangement, and if the two forms differ a member
needs a third URL. The note said to check it against the real instance rather
than reason about it. **Checked on 2026-08-09: `docker pull
nexus.../repository/<name>/<image>:<tag>` works.** Path routing is in place, one
URL per member is enough, and `multiImageName` was right to build the pull
reference out of the browse URL.

It was wrong about the *scheme* — that is D41 in §1.1, found by this check, and
never specific to groups.

#### What building it added to the design

- **Derived slugs step aside; declared ones never do.** Two registries aliased
  `prod` is ordinary, and the slug DevDesk derives for the second is DevDesk's
  own doing, so it becomes `prod-2`. A slug the *file* declares is a link
  target: renaming it to resolve a clash would move one group's members under
  another, so a duplicate is an error at load and the form refuses to write one.
  A **dangling `parent` is an error** too — it can only come from a hand-edit,
  and keeping it leaves an entry nothing can reach.
- **Two migrations, one of them deliberately over-declaring.** A pre-`kind`
  entry with a `management_url` becomes `kind: group, provider: nexus` — that
  field *was* the group marker. Decision F would then have silently stopped
  discovering groups whose only marker was a Nexus-shaped URL, so an entry whose
  URL contains `/repository/` migrates the same way. It over-declares: a Nexus
  *hosted* repository lives under `/repository/` too and is not a group. That is
  the deliberate half — detection answers "not a group" for it exactly as
  before, `kind: group` is visible in the table and one keystroke from being
  corrected, whereas dropping a real group's discovery would not be. A kind the
  file *states* is never second-guessed.
- **The provider vocabulary is stated twice, on purpose.** `internal/config`
  owns what a file may say, `internal/registrymgr` owns what can be detected,
  and neither should import the other to say so.
  `TestTheProviderVocabularyMatchesTheConfig` stops them drifting, including a
  check that config offers no provider a detector cannot be selected for.
- **An empty discovery is stored; a failed one is not.** "Asked, and it is not a
  group" is an answer, and not storing it is what makes a non-group get probed
  forever. This is **D23**: `NexusDetector.fetchRepoMeta` returned a bare
  `ok=false` for both "the manager said no" and "the manager could not be
  asked", which was harmless while the answer was discarded on every open and
  stopped being harmless the moment it was cached — one unreachable minute would
  have erased what was last known. The test pins both halves, since a fix making
  *every* answer an error would pass one of them alone.
- **Toggling a partial group completes it** rather than clearing it, and
  `theme.RenderCheckboxTri` exists because half a group selected is not the same
  statement as none — rendering them alike is how a user unchecks something they
  did not mean to.
- **The remembered selection stores what was *un*checked**, per context. Storing
  the exceptions is what makes a member discovered since the last visit arrive
  checked, rather than sitting out of every search because it did not exist when
  the selection was saved.
- **One deviation from Rule 111, recorded.** It offers `h`/`l` as aliases for
  `←`/`→`, but `l` is already login on this tab and a key has one role
  (Rule 135). The arrows are the drill-down; `h`/`l` are not bound.

#### The three defects it closed

**D12** — `anonymous` meant nothing: `docker login` is keyed on host, so one
login against a Nexus instance authenticated every repository it served. Closed
on **both** registry-facing paths, `detectRegistryGroupCmd` and the browser's
`credsFor`, which now read the mode before looking anything up and send nothing
— not even a configured username — when it says anonymous. Each has a test
asserting the refusal *and* a sibling asserting credentials still flow when the
mode allows it, so neither can pass by breaking authentication outright.

One correction to the plan: `TestManagementCredentialsAreLookedUpByHostAlone`
was listed as an inverted test to turn around, and it was not asserting broken
behaviour — stripping the repository path before a management-host lookup was
right and stays. What was missing was the gate in *front* of that lookup, so the
test was made explicit about its mode rather than reversed.

**D13** — the blocking resolving state. `browserStateResolving`, `entryGroups`,
`pendingDetections`, `HandleGroupDetected` and `finalizeEntries` are all gone:
the browser builds its entries from config plus cache in its constructor and
returns no command at all (`TestOpeningTheBrowserIssuesNoCommand`). It opens on
the first frame, answers `esc`, and works offline. The smaller thing recorded
under D13 went with it — nothing matches on `reg.URL` any more, so two
registries configured with the same URL no longer collide.

**D14** — a member's filter label was a synthesised URL. `registryFilterLabel`
resolves through the browser's entries, which now include members, so a member
reads `prod/dhi`. The filter gained the group level at the same time: `r` stops
on the group first, then on each registry, then off, and `resultFilter` replaced
the bare URL string so "this group" and "this registry" are different values
rather than one field meaning two things.

**What this left unfinished, found later — and since fixed.** The member URL
kept the shape the table above records — `host + /repository/<name>` — because
nothing had yet pulled from one. It browsed and could not pull (D39), and the
addressing it stood in for turned out not to be derivable at all, so it is
declared instead:
[§3.18](#318-a-registry-member-is-an-address-not-a-url--repo_prefix) replaced the
synthesised URL with the pair `(url, repo_prefix)`. The selection map also stayed
keyed on the URL when entry identity moved to the slug (D40, since fixed).

### 3.9 Every secret goes to a host secret manager, and radio buttons go away — **done**

No secret DevDesk holds is written to a file DevDesk owns. Tokens and registry
passwords go to the Windows Credential Manager, the macOS Keychain or a Secret
Service implementation on Linux, through one storage and one only.

#### What was wrong

Three of the five storage paths were plaintext on disk, and the option the UI
labelled "secure" was one of them.

| Secret | Destination | Protection |
|---|---|---|
| Forge token | `~/.devdesk/credentials-<ctx>.json` | **plaintext JSON**, 0600 |
| Forge token | `gitlab.token` in `contexts/<ctx>/config.yaml` | **plaintext YAML** |
| Forge token | git credential helper | whatever the helper does |
| Registry password | `~/.docker/config.json` via `docker login` | whatever Docker's `credsStore` does |
| Registry password | `registry.password` in `config.yaml` | **plaintext YAML**, read at `explorer/create.go:43,132` |

`ChainStorage.Save` wrote to **every** storage in the chain, and all three
construction sites built it as `NewChainStorage(FileStorage,
GitCredentialStorage)`. Choosing **"Save to Git Credential Manager (secure)"**
therefore stored the token in the credential manager *and* in
`~/.devdesk/credentials-<context>.json` in plaintext. Reads made it worse:
`FileStorage` was first and `ChainStorage.Load` returned the first hit, so the
plaintext file was authoritative and the credential manager was never consulted
while it existed. The secure backend was decorative in both directions.

The other option was no better in a different way: **"Save token to config file
(less secure)"** set `saveToHelper = false`, so nothing reached the chain and
the token landed only in `config.yaml`. Both options put the token in plaintext;
the "secure" one did it twice.

And logout did not clean up: `handleLogoutComplete` cleared
`m.config.GitLab.Token` in memory with no `config.Save` behind it, so the token
survived in `contexts/<ctx>/config.yaml`.

#### The distinction that decided the design

Delegating to `git credential` delegates to **whatever helper git happens to be
configured with**. If that is `store`, the token lands in `~/.git-credentials`
in plaintext — the same failure, relocated. Only `manager` (GCM),
`osxkeychain`, `libsecret` and `wincred` reach a real OS store. "Goes through
git credential" is not the same claim as "encrypted at rest", and the
requirement was the second one.

So the host store is the primary path and git credential is kept as an explicit
alternative, for users who want their tokens where GCM already puts everything
else. Only the first can promise what the requirement asks; the second is the
pragmatic option and stays reachable without becoming the default.

#### What shipped

**`KeyringStorage` over `zalando/go-keyring`** (`credentials/keyring.go`), one
implementation covering all three platforms with no cgo: wincred on Windows, the
`security` binary on macOS, D-Bus Secret Service elsewhere. Entries are filed
under service `devdesk`, account `<context>/<url>`, so two contexts pointing at
the same host keep separate secrets — the property `GitCredentialStorage` needed
`credential.useHttpPath` to get (§1.1).

`KeyringAvailable()` probes with a **read** of an account that is never written.
A miss proves the backend answered and simply holds nothing; anything else is
the backend being absent. It runs on every launch, including on machines where
the store turns out to be unusable, so it must not be able to leave anything
behind.

**`Select(context, preference)`** (`credentials/select.go`) returns a
`Selection{Storage, Backend, Detail}` and picks exactly one destination — host
store, else git credential, else memory. Writing to several at once is what
produced the defect above, so the chain is gone rather than reordered.
`app.secret_backend` pins the head of that list (`auto`, `keyring`,
`git-credential`). A pinned backend that turns out to be unreachable falls
through to memory rather than silently to the other one: someone who asked for
the keyring should not be handed a git helper without being told.

`gitHelperUsable()` refuses `store` by name and refuses an unset helper, and
accepts everything else. There is no list of good helpers to check against —
enumerating them would only mean rejecting the next one someone installs.

**The fallback is worse on purpose.** With no store reachable the answer is
`MemoryStorage` — session-only, re-authenticate each launch — and the auth view
says so in `ColorWarn`. That is deliberately worse UX than a file, and that is
the point: a fallback that is silently insecure is how the "secure" option came
to exist.

**`FileStorage` and `ChainStorage` are deleted.** `MemoryStorage` gained a mutex:
it is now one instance shared by the Cmd goroutines of every view, which the
per-call construction it used to get had hidden.

**`GitLabConfig.Token` and `RegistryConfig.Password` are out of the schema.**
Parsing and ignoring them would have left the secret on disk forever for every
existing user, so `MigrateLegacySecrets` runs on load and on every context
switch: it reads the two fields straight from the YAML —
`config.ReadLegacySecrets` — moves them into the store, and deletes them from
the file. The rewrite edits the parsed YAML tree rather than round-tripping
through `Config`, which would rewrite every key including the defaults
`applyDefaults` filled in. A secret the store refused to take stays in the file;
losing it would be worse than leaving it. A secret with no URL has no key to be
filed under and no host it could be used against, so it is dropped — and the
auth view reports every one of these outcomes in words.

`Auth.Authenticate(url, token)` lost its `saveCredentials` parameter and always
stores; `AuthenticateOnly` is the auto-login path, whose token already came out
of the store. Logout deletes from the store, and there is no longer a config
copy to forget about — which is what closes the last defect above.

`registry.password` had no UI to set it and one reader, `explorer/create.go`.
That reader now asks the store, keyed by the registry URL, and treats a miss as
"anonymous registry" — which is the common case.

**The radios are gone**, with `SaveToHelper` / `SaveToConfig`, the `saveOption`
field, `theme.RenderRadioButton` (its only two call sites), and the `ctrl+s` /
`ctrl+f` shortcuts and their help section. The form went from five fields to
three, now named `fieldURL` / `fieldToken` / `fieldSubmit` instead of the
integers 0–4. Removing the `case " "` had a side effect worth recording: space
could not previously be typed into the URL or token field, because the radio
handler swallowed it before the input saw it.

Rules 120 and 132 in `.claude/rules/tui-forms.md` now say cycle fields are the
only control for a closed set, whatever its size, and that checkboxes remain for
independent booleans.

#### What a user has to do

Nothing, on any platform. The migration is automatic and the auth view reports
what it did. Two consequences are worth knowing:

- On a headless Linux box with no D-Bus session and no git helper, DevDesk now
  asks for the token on each launch instead of reading it from a plaintext file.
  That is the intended trade, and it is stated on screen rather than inferred.
- The entry is visible in the host's own UI (`Credential Manager`, `Keychain
  Access`, `seahorse`) under `devdesk`, which is where a user should be able to
  revoke it.

#### One rough edge, left rough on purpose

On Linux, `go-keyring` calls `Unlock` on the login collection before every read,
including the availability probe. On a desktop whose keyring unlocks with the
session password — the default on GNOME and KDE — that returns immediately. On
one configured with a separately-locked keyring, it raises the agent's unlock
prompt at startup, before the TUI has drawn its first frame, and blocks until it
is answered.

Wrapping the probe in a timeout would make this worse, not better: it would
leave a prompt on screen that nobody is waiting on, and answer "no store
available" for a machine that has a perfectly good one — sending the user to the
memory fallback because their keyring was locked. Every other client of the
Secret Service behaves the same way, including git's own `libsecret` helper. The
prompt is the user's keyring policy working; suppressing it is not DevDesk's
call to make.

Windows and macOS have no equivalent: `CredRead` is silent for the current user,
and `security find-generic-password` only prompts for items the calling binary
is not on the ACL of — which, for items DevDesk itself wrote, it is.

---

### 3.10 An inference-backed explainer for network diagnostics — **superseded by §3.38**

Netdiag runs the tests but leaves the interpretation to the user. The first —
and for now only — AI feature is an **explainer over diagnostic results that
DevDesk already holds**. It sends a few kilobytes of structured facts, needs no
new privilege, and touches no packet payload.

**§3.38 supersedes this.** The direction reverses: DevDesk serves what it knows
over a read-only MCP server, and the agent does the interpreting. That deletes
the client, the provider config, the pseudonymiser and its inverse, the
confirmation pane and the streaming — and it moves decision 4's guarantee from
DevDesk to the user, once, at `mcp.enabled`. What survives is this entry's
reasoning: allow-list construction beats scrubbing, and a Gitleaks match can
never be pseudonymised because the payload *is* the secret.

#### Settled

| # | Question | Decision |
|---|---|---|
| 1 | Scope of v1 | **Explainer only.** No capture. tcpdump is a later input to the same explainer, not part of this. |
| 2 | Local server lifecycle | **DevDesk consumes an endpoint.** It never starts, stops or supervises an inference server. |
| 3 | Protocol | **One OpenAI-compatible client** (`POST /v1/chat/completions`). Covers ollama, llama.cpp, vLLM, LM Studio and hosted providers. No second native client. |
| 4 | Redaction | **Nothing sensitive reaches a model, local or remote.** Not a per-provider policy — one rule, no exception for `localhost`. |
| 5 | Snaplen | Headers-only is the default **when capture arrives**. Out of scope for v1 by decision 1. |
| 6 | Provider scope | **One per context.** |

Decisions 2 and 6 cost nothing. Consuming an endpoint means the provider is a
URL, a model name and a token — a `AIConfig` block in the context's
`config.yaml`, which is per-context by construction. The token goes to the store
from §3.9 like any other; it must never land in the YAML. The GPU question that
motivated hosting (Docker on macOS runs a Linux VM with no Metal access, so
unified memory is unreachable from a container) disappears with it: the user
runs `ollama serve` natively, or a container, or nothing, and DevDesk does not
care.

#### The hard part is decision 4, and Presidio only half-answers it

Reference: [Docker agent PII protection with
Presidio](https://k33g.org/p/20260716-docker-agent-pii-protection-presidio) —
an analyzer/anonymizer pair behind HTTP, hooked on `before_llm_call`, replacing
detected spans with typed tokens.

Two things transfer and one does not.

**What does not transfer: the entity set.** Presidio detects *PII* — names,
emails, phone numbers, credit cards, IBANs, national IDs, IP addresses. What
DevDesk holds is mostly not PII: internal hostnames, private ranges, resolver
addresses, listening sockets and process names, registry and forge URLs,
Gitleaks matches. `IP_ADDRESS` is the only real overlap. A DevDesk payload could
pass Presidio clean while still describing the whole internal network. Presidio
is therefore a **second net, not the mechanism**.

**What transfers: fail-closed, and the deterministic layers.** The post notes
its own default is fail-open and that production needs `PII_FAIL_CLOSED=1`.
Given §3.9, fail-closed is the only acceptable mode here: if the redactor is
unreachable, nothing is sent, and the user is told why. And the post's two
non-NER layers — a deny-list and structural rules by column — are the parts
that actually caught things reliably. That is the direction to build in.

#### Allow-list construction beats scrubbing

The post scrubs because it hooks arbitrary agent traffic and cannot know what is
in it. DevDesk is not in that position: it **assembles the payload itself** from
`m.results`, which is already typed and structured. So the payload should be
built field by field from an explicit allow-list, not produced as a blob and
then cleaned.

The difference matters: **a field never included cannot fail to be redacted.**
Scrubbing is a filter that can miss; construction is a whitelist that cannot.
Presidio then runs over the assembled payload as a check on the construction,
and any hit is a bug in the allow-list, not a routine save.

#### The tension: the sensitive data *is* the diagnostic data

Redacting addresses out of a network diagnostic destroys the diagnostic. A model
told `[IP_REDACTED]` resolves to `[IP_REDACTED]` can conclude nothing, and the
answer that comes back is unreadable.

The resolution is **consistent pseudonymisation that preserves the analytically
relevant class**, not blanket redaction:

| Real | Sent | Preserved |
|---|---|---|
| `api.corp.internal` | `host-1` | identity across the payload |
| `10.2.3.4` | `private-a` | RFC1918, and same-subnet relations |
| `203.0.113.9` | `public-b` | routable, distinct from private |
| `10.2.3.7` | `private-c` | same /24 as `private-a` |

The hypotheses the model is asked to rank depend on structure — private vs
public, same subnet or not, resolves or not, port open or filtered, which TLS
stage failed — never on the literal octets. So this loses nothing. "host-1
resolves to private-a but hop 5 is public-c, so the route leaves your network"
is exactly as useful as the version with real addresses.

The mapping stays in memory, and the response is **restored locally before
display**, so the user reads real names. The blog post does not do this
round-trip — its tokens are one-way — but DevDesk needs it, because unlike a
CSV of customers its payload is *entirely* made of identifiers.

#### Confirmation before send

The assembled, pseudonymised payload is rendered in the viewport before it
leaves, for every provider. Decision 4 says local and remote are treated alike,
so there is no "trusted endpoint" shortcut. This costs one keypress and is what
makes the feature auditable without reading the source.

#### Bubble Tea shape

Streaming is the only delicate part. Rule 110 forbids mutating the model inside
a `Cmd`, so the pattern is a channel plus a `Cmd` that reads one chunk and
returns a `streamChunkMsg{gen, text}` which re-arms itself — the same shape as
the spinner, reusing the generation counter already in `run.go:33` to discard a
superseded stream.

Cancellation is mandatory, not optional: a local model on CPU can take minutes.
A `context.CancelFunc` lives in the model and `esc` cancels. **D13 is the
warning** — a state the user cannot leave while something resolves is a defect
this repository already has once.

No new view and no command. The explanation is a results tab under the table
(Rule 123), triggered by a key on the results screen. A dedicated view turns
this into a chat product, which is not what is being asked for. With no provider
configured the shortcut is absent rather than erroring (Rule 130), and all
strings are English US (Rule 129).

#### Sketch of the work

1. `internal/ai`: config block, an OpenAI-compatible client behind a small
   interface so tests inject a fake — the `runner` indirection in
   `internal/docker` is the precedent — and the token read from the §3.9 store.
2. The payload builder: allow-listed fields out of `m.results`, plus the
   pseudonymiser and its inverse. Table-driven tests, like the existing
   `dns_formatter` and `traceroute_formatter` parsers. **This is the feature; do
   it first and it is testable with no endpoint at all.**
3. The prompt: observed facts only, and an instruction to name which test each
   claim rests on. The raw results stay on screen next to the explanation —
   the narrative never replaces the data.
4. Streaming, cancellation, and the results tab.
5. The confirmation pane.
6. Optional and last: Presidio as a fail-closed second net over the assembled
   payload, behind a config flag. Two containers and a spaCy model is heavy
   for a few kilobytes of already-structured text, and step 2 is what actually
   provides the guarantee.

Later, and explicitly not now: capture as an additional input (bounded by `-c`
and `-G`, `-s 96` by default so payloads cannot be captured at all), and the
deterministic flow summariser that would have to precede it — a pcap does not
fit in a context window, and once the summariser exists it answers most of the
question without a model. `nicolaka/netshoot` (`config.go:218`) already ships
`tcpdump` and `tshark`, and the privileged host-namespace runner exists
(`ports.go:36`, `topology.go:47`), so the missing piece is the analysis, not the
plumbing.

Adjacent candidates, ranked, none settled: Trivy remediation (§3.2 — low
sensitivity, but any suggested base-image bump must be verified by a re-scan,
never trusted); Gitleaks triage (highest value since false positives dominate,
highest risk since the payload *is* the secret — possibly viable by sending rule
name, path and entropy with the match withheld); container log explanation
(logs carry env vars and DSNs routinely).

### 3.16 The explorer clones, workspaces syncs — **done**

`p` on a group or a project in the explorer cloned the subtree into a
workspace. It worked, and it was the least designed path in the application: a
single `Cmd` covering the whole subtree behind a modal showing `"Pulling..."`,
which on a large group is several minutes indistinguishable from a freeze —
observed, not theorised.

`c` now opens a selection mode over the tree, `enter` starts a pipeline that
discovers and clones at once, and the list is the progress view and the report.
The design below came out of a brainstorm and was built as stated; the two
prerequisites landed ahead of it.

#### Settled

| # | Question | Decision |
|---|---|---|
| 1 | What the explorer does | **Clones what is missing.** A repository already on disk is skipped untouched. |
| 2 | What updates an existing clone | **Nothing here** — a `sync` feature in the workspaces view (§3.17). A dirty working copy is a property of a working copy, so it belongs to the view that owns what is on disk. |
| 3 | The name | **Clone, not Pull.** It was never the behaviour that was wrong, only the label; `pull` is freed for §3.17. |
| 4 | Disk layout | **Mirror from the forge root** under the target directory. |
| 5 | Target directory | **Keep borrowing the workspaces view**, as today. |
| 6 | `gitlab.pull.target_dir` | **Deleted** — done, see below. |
| 7 | Discovery and cloning | **Pipelined.** The list fills as discovery finds repositories and a row starts spinning as soon as it is found. |
| 8 | Selection | A new mode: **several roots at once** — parent folders *and* individual projects — confirmed in one go. |
| 9 | Modals | **Yes/no confirmations only** (Rule 112). The list is the progress view and the report. |
| 10 | Selecting a group | **Takes everything under it.** Drill in to deselect what you do not want. |
| 11 | How a selection is stored | **Roots plus exclusions**, never a positive list of repositories. |
| 12 | Cancelling | **Stops the pipeline, never a clone.** Discovery is cancelled and no new clone is issued; the ones running are awaited. |
| 13 | The list afterwards | **Discarded on `esc`.** The result is what workspaces shows; there is nothing to keep. |
| 14 | The selection control | **No change to `datatable`.** The view supplies the check state through the `Cell` seam that already exists. |

Decision 2 is the load-bearing one. It draws a line that holds: **the explorer
creates what does not exist, workspaces reconciles what does.** The explorer
then never needs to know what a dirty working tree is, and the per-row states
collapse to five — to clone, cloning, cloned, already present, failed.

Decision 4 is what decision 8 forces. Today `nodeSlug` keeps only the **last
segment** of `FullPath` (`tree.go:69-72`), so `acme/platform/backend` pulled
into `~/ws` lands at `~/ws/backend`. With several groups selected at once,
`acme/platform` and `other/platform` would both land on `~/ws/platform` and
silently merge. Mirroring the full path from the forge root cannot collide and
matches what the explorer shows.

Decision 7 is the one that answers the freeze. Building a complete list first
means walking the whole tree through the API before anything happens, which
just moves the dead screen one step earlier. Pipelining removes it entirely, at
the cost of the total only being known at the end — the header counts up
(`47 found…`) instead of announcing a total. Reviewing the full list before
anything starts is given up deliberately; selecting the groups is the act of
decision.

Decision 11 is forced by decision 7, and this is the part worth keeping. A
**positive** list of the chosen repositories cannot be built when a group is
ticked without enumerating its children first — which is the full API walk, run
at selection time. That is the freeze pipelining was chosen to remove, moved one
screen earlier. **Roots plus exclusions represents "this group, minus these"
without knowing what the group contains**, so nothing has to be discovered
before the user confirms. It is the only representation compatible with
decision 7. `RegistryBrowser` already stores its selection as the entries that
were *un*checked (`registry_browser.go:201-202`) — same shape, weaker reason.

Three things fall out of it:

- **The tri-state needs no discovery.** A group renders `CheckSome` exactly when
  some exclusion path is a descendant of it, which is known by construction: an
  exclusion is only ever created by a keystroke on a node already on screen. So
  a group nobody has expanded still displays correctly.
  `theme.CheckState` and `theme.RenderCheckboxTri` already exist and are shared,
  not browser-local.
- **Deselection costs only what it inspects.** Drilling into a group to untick
  something fetches that one level — the lazy navigation that already exists.
- **The overlap question disappears.** Ticking a group and then a descendant is
  meaningless, because the descendant is already implied; unticking makes an
  exclusion and re-ticking removes it. There is no ambiguous case left to rule
  on.

What has to be accepted: **the confirmation screen cannot state a repository
count** — only `3 groups · 1 project · 4 exclusions`. The number appears as
discovery runs, which is the same trade decision 7 already made.

Keys fit Rule 135 unchanged: `←→` drills, `Space` toggles, each keeping one job.

Decision 5 costs nothing: `openBrowser` replaces only `views[ViewWorkspaces]`
and never drops the explorer (`selection.go:37-41`), so a multi-selection in
progress survives the round trip the way `pullTargetNode` does today, and
`PullSelectionCancelledMsg` already returns without losing it.

Decision 12 dissolves the partial-directory question rather than answering it:
a clone is never interrupted, so it never leaves half a repository behind. It
does require **two cancellation scopes**, which is the part to get right.
Discovery is HTTP reads and cancels through a `context` safely; a clone is a
`git clone` writing into a directory, and a `context` that kills it recreates
exactly the mess this decision avoids. So the context covers discovery, and the
scheduler simply stops issuing work.

The cost is that **cancelling is not instant** — up to `ParallelJobs` clones
keep running, which on large repositories is visible. The view has to say so
(`cancelling — 3 clones finishing`), or `esc` reads as ignored. A second `esc`
must not force: forcing is the partial directory, back again.

Decision 13 holds for the successes, which workspaces lists. It loses the
**failures**: a clone that failed wrote nothing, so nothing on disk records it.
That is acceptable because re-running the same selection is self-correcting —
what exists is skipped, what is missing is retried — but the failures must still
reach `log.Printf` and the footer (Rule 128) while the view is alive, or a user
who looks away for three minutes never learns that three repositories failed.

Decision 14 was expected to be the one piece of real work left and turned out
not to be. `Column[T].Cell` is a `func(T) string` the view supplies, so a column
rendering a checkbox glyph computed from the exclusion set is expressible today;
`Update` is a whitelist switch with no `default`, so `Space` is never consumed
and reaches the view. It is the same seam as `SelectedStyles`, for the same
stated reason — the package does not learn what a selection is any more than it
learned what a severity is.

The state must **not** move into `datatable`: `SetItems` replaces the items on
every drill-down, while the selection spans levels the table has never shown. A
selection kept there would be lost on the first `→`.

The real cost sits in the explorer. Columns are built once in `New` and cannot
reach the live model, so the check state has to be carried on a **row type** —
the `imageRow` pattern — moving the table from `Model[*TreeNode]` to
`Model[explorerRow]`. Mechanical, but not free. And `RenderCheckboxTri` styles
its output, so it cannot go in a cell (Rule 122); only the raw icons can.

This judgement flips the day a **second** table needs a selection. For one,
generalising into `datatable` would be speculative.

#### Two prerequisites, both defects in their own right — both done

**A lighter fetch for discovery — done.** The recursive walk cost one call for
subgroups plus one for projects per group, plus **two more per project** —
`fetchLastPipelineStatus` and `fetchProjectAccessLevel`. Two hundred
repositories was 400+ calls for a CI status and a role no clone reads.

`listGroupChildren` is now the paginated, undecorated half both callers share;
`discoverGroupChildren` builds nodes from it and decorates nothing, while
`loadChildren` decorates as before. The two are **not** interchangeable, which
is why the clone also stopped writing what it finds onto `node.Children`:
discovery nodes on the tree the view renders would blank the role and CI
columns for every group a clone had passed through.

That write was also **Rule 110** — a `Cmd` assigning a field `Update` reads —
so the race is gone as a side effect rather than as a patch. It was going to be
removed by the rework anyway; the lighter fetch made keeping it actively
harmful, which is what brought it forward.

**Pagination — done.** Every list in `api.go` stopped at the first page, so a
group of 101 projects enumerated 100. With a list on screen the view would have
stated a count and been wrong, which is why this was a prerequisite rather than
a nicety. Fixed ahead of the rework; see D34.

#### What the rework fixes for free

- **Rule 110 — already gone.** `node.Children` was written inside a `Cmd` on
  the same `*TreeNode` values `Update` reads (`navigation.go:24`, `:70`). The
  lighter discovery fetch removed the write, so the rework inherits a walk with
  no shared state to race on.
- **The four unread `gitlab.pull.*` settings are resolved, two each way.**
  `ParallelJobs` becomes how many rows spin at once and `IncludeArchived` a
  discovery filter; `TargetDir` and `MaxDepth` are deleted. Leaving one declared
  and unread is not an outcome.
- **`gitlab.pull.target_dir` is gone.** It duplicated `app.workspaces_dir` —
  same meaning, and defaults differing by a single letter (`~/workspace` against
  `~/workspaces`), so setting the wrong one changed nothing and said nothing.
  Decision 5 leaves it no role at all.
- **`gitlab.pull.max_depth` is gone**, and not merely for being unread. A depth
  bound **contradicts decision 10**: tick a group, have discovery stop at level
  five, and you get less than you asked for with nothing saying so — the silent
  truncation of D34, reintroduced as a feature. Exclusions express the same
  intent precisely: "only the top level" is drilling in and unticking the
  subgroups, which is explicit and visible. Deleting it also removes the name
  clash with §3.6, which settles a *different* `MaxDepth` — the depth a forge
  declares, 1 for GitHub and unbounded for GitLab.

Both removals landed ahead of the rework, being independent of it;
`TestAConfigCarryingRetiredKeysStillLoads` covers the configs already on disk.

#### Forge neutrality

The mode has to be built on `TreeNode`, not on GitLab vocabulary. §3.6 settles
that the abstraction declares its depth — GitHub is organisations at level 1 and
repositories at level 2 — so selecting "parent folders" means selecting
organisations there, and decision 4 mirrors a path that is simply shallower.
§3.6 also lists `GitLabConfig`, pull settings included, as GitLab-shaped and due
to move; the fate of the two remaining settings should anticipate that.

#### What was built, and where it differs from the design

Nothing was given up. Two things the design left open resolved in the building:

- **The checkbox rides on the Type cell**, not on a column of its own. A column
  costs four cells on every screen to say nothing on all but one of them, and at
  80 columns the explorer has none to spare. Type is the left-most column, so
  the box still sits where a checkbox belongs; `colTypeMin` went from 10 to 13,
  because a Nerd Font glyph does not always render as narrow as `runewidth`
  counts it.
- **A failed walk gets a row naming the group** (`cloneWalkFailed`). The design
  named five row states, all of them repository states, and said nothing about a
  group that cannot be listed. Folding it into a repository's error would
  attribute it to one repository out of however many were never discovered.

The key is `c`, not `p`: decision 3 frees `pull` for §3.17, and a key still
reading `p` for an operation renamed to clone is the label problem again.

`components.ReportModal` went with the modal it was written for — its only
caller — which is decision 9 taking effect rather than a separate cleanup.

**Not verified by hand yet**: a group of more than a hundred projects, and a
cancellation with clones genuinely in flight. Both are covered by tests
(`pipeline_test.go`, `TestPaginationStopsWhenTheServerRepeatsAPage`), against a
fake forge and local git remotes.

### 3.17 `sync` in the workspaces view — **done**

The counterpart to §3.16 decision 2: the explorer creates what is missing,
workspaces reconciles what exists. Updating a clone, and every question about a
dirty working copy, lives here.

The survey's reading held. `workspaces.Entry` already carried `GitBranch`,
`GitRemote`, `GitModified`, `GitUntracked`, `GitUnpushed` and `GitUnpulled`, and
`detectGitStatus` already filled them on every listing; what was missing was the
**action**, not the knowledge. What the survey did not anticipate is that most of
the work would be in deciding what sync refuses, and in where a credential is
allowed to go.

#### The seven decisions

| # | Question | Answer |
|---|---|---|
| 1 | One row or a selection | **Follow `ctrl+s`'s rule.** A git repository syncs itself, a plain directory syncs every repository nested under it. A selection mode would be a second targeting model in one view, and the aggregation this view already does is the same aggregation sync needs. |
| 2 | Its own screen, like the clone list | **No — decorate the rows.** The clone opens a list because discovery *invents* its rows; here every repository is already on screen, so a list would print the same names twice. The spinner goes in the Git Status cell exactly as a scan's goes in Scanned. |
| 3 | Push in scope | **No.** Sync is the pull direction. Publishing is a separate intent with separate failure modes (protected branches, write scopes), and nothing about "reconcile what exists" implies it. `GitUnpushed` stays informational. |
| 4 | A dirty tree | **Fetch, then refuse.** Also for a divergence and a detached HEAD. No merge commit, no rebase, no stash — a divergence is a decision about someone's unpublished work, and guessing at it destroys hours in a keystroke that cannot be undone. |
| 5 | Skip versus failure | **Two different things.** A skip means the repository is as its owner left it; a failure means DevDesk could not find out. An unreachable remote is a failure, an uncommitted change is not. |
| 6 | Which token | **Only for the configured GitLab host.** See below — this is the one decision with a security consequence. |
| 7 | A sync-all key | **No.** At the root the user syncs each top-level directory. `Shift+S` collides with Rule 111's sort menu, and a second key is not worth inventing one. |

#### Decision 6, and why `internal/git` exists

`git_ops.go` moved out of `internal/gitlab` into a package of its own. The
reason is not tidiness: **cloning could assume the configured token and syncing
cannot.** The explorer clones from one forge, known in advance. The workspaces
view holds whatever the user has cloned — GitHub, a customer's Gitea, a bare
path on a share — and `http.extraHeader` would put DevDesk's personal access
token on the wire to any of them.

So the *caller* decides (`workspaces.tokenForRemote`, host-matched against
`gitlab.url`), and the package that runs git is named after git. Deciding a
credential's destination inside a package named after one forge is how the
default that must not exist gets written. A foreign remote never reaches the
loader at all, so the secret store is not even read for it —
`TestAForeignRemoteNeverTouchesTheSecretStore`, and a table covering ports,
case, and the `evil-gitlab.example.com.attacker.net` suffix trick.

`nonInteractiveEnv` is shared with `Clone` for a reason established the hard way
in §3.16: a `git fetch` is a network call like any other, and left to itself it
reaches the credential helper — which writes to the console over the rendered
frame and then waits on a browser. Nothing about that was specific to cloning.

#### D35, mitigated rather than closed

Sync **fetches first, always, whatever the tree looks like**, and that ordering
is the point rather than an implementation detail. A repository sync *declines*
still comes out of it knowing how far behind it is, because the fetch happened
either way — `TestARefusedSyncStillFetchedAndKnowsHowFarBehindItIs`. A refusal
that taught the user nothing would be worse than no feature.

But the column still reads stale **until a sync runs**: `loadEntries` does not
fetch, and it must not — a directory listing that hits the network on every
drill-down is a different defect. So D35 stays open, downgraded: there is now a
way to make the number true, and the one thing that acts on it never trusts it.

#### The rest, as built

- **`Model.busy` guards both directions.** A scan reads the working tree while a
  fast-forward rewrites it, and the visible result is a report describing a tree
  that no longer exists. Only the scan side had a guard before; `ctrl+a`'s purge
  needed one too, or a syncing row's counts are blanked with nothing on the way
  to replace them.
- **The footer line is rendered from the run, not assigned to `footerInfo`.** A
  batch outlives the three seconds Rule 128 gives a footer message: a progress
  line set on the first repository would vanish while the tenth was still
  fetching. Its own timer only drops a *settled* run, so a sync started inside
  those three seconds is not wiped by the previous one's tick.
- **`gitlab.pull.parallel_jobs` bounds this too.** One number meaning "how many
  git network operations at once" beats two the user has to keep in step, even
  though the setting sits under `gitlab:` and a workspace repository need not be
  a GitLab one.
- **`workspaces.New` takes the secret store.** `NewForSelection` passes nil and
  says why: a view lent out to answer one question never syncs.
- **D38 came out of this.** Reusing the scan's spinner mechanism is what
  revealed that the mechanism had never worked: the chain died after `Init` and
  nothing restarted it, so the scan spinner had been frozen on frame zero all
  along. Fixed for both actions.

Ten tests in `internal/git/sync_test.go` drive real repositories against local
remotes — a fake would have to model exactly the thing under test, what git
considers a fast-forward and what it considers dirty, so it could only ever
confirm the author's idea of those rules. Three of the view's tests were checked
against the pre-fix code and fail on it.

**Verified by hand** against a real private GitLab remote on 2026-08-09, which
is also the confirmation that `tokenForRemote` picks the token up — the path the
tests could only follow as far as the decision.

### 3.15 The scan form is deleted (phase 3) — **done**

The last phase of
[`configuration-view-plan.md`](../.claude/plans/configuration-view-plan.md).
2 538 lines removed against 530 added, across 32 files.

Both prerequisites had landed: `applyServerModeConstraints` lives in the
configuration view's `update.go`, and `scan.ValidateTrivyServer` is wired to its
`trivy_server` field.

**`StateScanning` went with `StateInput`**, which the plan did not anticipate.
`startScan` had two callers — the form, and the router's image fallback — and
once both were gone the whole in-place scanning machinery had no user:
the progress channel, `scanGen`, `cancelScan`, `waitForProgressCmd`,
`purgeScanCacheCmd`, `ScanCompleteMsg`, `ScanProgressMsg`, `StartScanMsg`. The
inventory rescans in the background with a spinner on the row (§3.11), so
nothing waits on a whole screen for one target. `scan.go` emptied out except
`hasScanSource`, which moved next to its one remaining caller.

**Three fields died with the form**, as recorded in the plan: `homeState` (the
inventory is the only landing state left, so `goHome` stopped branching), `deps`
with `checkDependencies` and `DepsCheckedMsg` (the Start button was its last
reader, the header having stopped showing tool versions in §3.12), and the ~15
option mirrors.

**The browser bridge went at both ends.** The form borrowed the workspaces view
for a directory and the images view for an image; the explorer borrows the
workspaces view for a clone destination, and that is the only borrow left. So
`app/selection.go` stopped being parameterised over who is borrowing, and the
images view lost `NewForSelection`, `ImageSelectedMsg`, `SelectionCancelledMsg`,
`ResetSelectionMsg`, `selectionMode` and the six render sites that read it.
`resetSelectionModeFor` collapsed to dropping one view.

**A missing stored result now rescans in the list it came from.** `enter` on a
scanned row asks the router for the result file; when it is gone,
`rescanInOrigin` hands the target's name to that list and stays there, rather
than opening a security view on nothing. `workspaces.ScanRequestMsg` and
`ociresources.ScanRequestMsg` were **declared and unhandled** before this — dead
types carrying exactly the right shape — and they have handlers now.
`LaunchBatchScanMsg`, `LaunchSingleImageScanMsg` and `handleLaunchScan` went with
the form: they carried the options it had collected, and options come from the
configuration view.

**Coverage.** The deleted tests were covering live code incidentally, and two
regressions had to be repaired rather than accepted: the explorer's borrow
(`handleDirectorySelected`, `leaveSelectionMode`, `returnToOrigin` all fell to
0 %, exercised only by the security selection tests that went) and
`openSecurityView` (the two result handlers' success path). Both have their own
tests now, as do the two `ScanRequestMsg` handlers, `Init`, `handleSpinnerTick`
and the details viewport's scrolling. `internal/ui/security` 86.5 % → 87.2 %,
`internal/app` unchanged at 87.0 %, **no package lower than before**; project
total 81.5 % → 81.4 %, the residue of deleting a well-covered package's code.

`internal/ui/security/scan_test.go` was deleted whole. Its two durable
invariants live elsewhere: `ValidateTrivyServer` refusing `":"` is
`internal/scan/command_test.go`, and `alt+:` reaching the router rather than a
text field is `internal/app/command_mode_test.go`.

### 3.14 Remove SBOM generation — **done**

Drop the feature entirely: the two settings, the scan stage, the Trivy command
builders, the two controls, the field on `Result`, and the documentation. No
inert remains — no option that can be set and not read, no function with no
caller.

**Why after phase 3** (the deletion of `internal/ui/security/form.go`, §3.15,
now shipped) and not before: the form addresses its fields by index, and SBOM is index 6 of
thirteen. Removing it now renumbers everything above it —

```
before : 6=sbom  7=trivyServer  8=ignoreUnfixed  9=ignoreEOL  10=gitleaksConfig  11=history  12=button
after  :         6=trivyServer  7=ignoreUnfixed  8=ignoreEOL   9=gitleaksConfig  10=history  11=button
```

— across `totalFields`, `isServerIncompatibleField`, `isTextInputField`,
`focusTextField`, `Model.InEditMode`, the space-toggle switch, the right column
of `renderInputView` and `renderStartButton`, plus the tests that pin those
indices. All of it is thrown away when the form is deleted. Doing the removal
after phase 3 skips that phase completely: the form's SBOM checkbox,
`applyServerModeConstraints`, `generateSBOM` and the renumbering all disappear
with the file that holds them.

Everything below was established by survey before phase 3 shipped. Phase 3 has
since removed the form, so the renumbering described above no longer applies and
the security-view rows of the tables below are already gone -- what is left is
the list from `internal/config` down.

#### What goes

| File | What |
|---|---|
| `internal/config/config.go` | `ScanConfig.GenerateSBOM`, `ScanConfig.SBOMOutputDir`, and the `expand(c.Scan.SBOMOutputDir)` line in `applyDefaults` |
| `internal/scan/options.go` | the two assignments in `OptionsFromConfig` |
| `internal/scan/scanner.go` | `ScanOptions.GenerateSBOM`, `ScanOptions.SBOMOutputDir`, `Result.SBOMPath`, the SBOM stage in `Scan`, and `\|\| s.options.GenerateSBOM` in `missingToolErrors` |
| `internal/scan/trivy.go` | `GetSBOMCommand`, `GenerateSBOM` |
| `internal/scan/trivy_args.go` | `sbomArgs`, `sbomSubcommand`, `sbomFileName`, the `containerOutputPath` constant, and the `path/filepath` import |
| `internal/ui/configuration/fields.go` | the "Generate SBOM" toggle, the "SBOM output dir" text field, and the `serverModeFields` entry |
| `internal/ui/configuration/update.go` | the `GenerateSBOM = false` line in `applyServerModeConstraints` |
| `internal/ui/security/warnings.go` | the `"sbom generation failed: "` prefix |

Two strings to reword rather than delete: the "Trivy server" description in
`fields.go` ("Client-server mode; disables misconfig, license and SBOM") and the
scan-types section of `GetHelpContent`.

#### Six things the survey settled

1. **`Result.SBOMPath` is written by the stage and read by nothing.** The help
   claims "If SBOM was generated, its path is shown above the tabs"; no view
   reads the field. There is nothing to replace, only to remove — and the help
   line is wrong today, independently of this removal.
2. **No migration is needed for `config.yaml`.** `config.Load` calls
   `yaml.Unmarshal` without `KnownFields(true)`, so a file still carrying
   `generate_sbom:` or `sbom_output_dir:` loads unchanged and the keys are
   dropped at the next `config.Save`.
3. **No migration is needed for the scan caches** either. Stored results are
   JSON and `encoding/json` ignores unknown fields, so a cached report carrying
   `sbom_path` reads back fine.
4. **`containerOutputPath` dies with `sbomArgs`** — nothing else mounts a
   writable output directory. **`dockerSocketMount` must survive**: `wrapTrivy`
   uses it too. Deleting both together is the easy mistake, and the build
   catches it.
5. **`applyServerModeConstraints` exists twice** — in the form and in
   `internal/ui/configuration/update.go`. Only the second survives phase 3, but
   until then both force `GenerateSBOM = false` and both must be handled or they
   disagree.
6. **`TestEveryConfiguredOptionReachesTheScanner` needs no edit** — it walks the
   field names `config.ScanConfig` and `scan.ScanOptions` share, so removing the
   fields from both keeps it green. **The survey was wrong about what it
   catches**, and a probe during the removal established the truth: it does
   *not* fail when only one side is done. A field present in one struct and not
   the other is not *shared*, so the walk never visits it and the test passes.
   What it catches is the field left in **both** structs but not carried by
   `OptionsFromConfig` — verified by putting `GenerateSBOM` back in both and
   watching it fail with `did not carry GenerateSBOM`. The half-done state it
   was claimed to guard is caught by the compiler instead, which is why the
   removal was still safe.

#### Tests

Delete: the SBOM cases in `internal/scan/command_test.go`
(`TestTheSBOMCommandIsShownLikeTheOthers`,
`TestAnImageSBOMWithNoOutputDirectoryUsesTheWorkingDirectory`,
`TestTheSBOMFileNameIsDerivedFromTheTarget`,
`TestTheSBOMPathIsTheHostPathNotTheContainerPath`,
`TestTheSBOMOutputMountIsWritable`, and the `sbomArgs` line in the
server-address test) and in `internal/scan/execute_test.go`
(`TestAFailedSBOMYieldsNoPath`, `TestTheSBOMPathComesBackOnSuccess`, and the
unsupported-target-type case).

Adjust `internal/scan/scan_test.go`: the `"sbom"` branch of `stageOf`,
`everyStage()`, four stage scripts, the `SBOMPath` assertions, the expected
stage lists (`"misconfig,sbom,trivy-secret,vuln"` loses one), the per-stage
error count (**6 → 5**), and the assertion that the SBOM stage narrates no
progress. Also `internal/ui/configuration/model_test.go` (server mode) and
`internal/ui/security/warnings_test.go` (the prefix).

The `internal/ui/security` tests that mention SBOM — the server-mode case, the
field-index table, and `"Generate SBOM"` in the offered-options list — go with
the form in phase 3 and need no work here.

#### Documentation

`.claude/CLAUDE.md` (the feature list and the `trivy.go` line), `README.md`,
`docs/CODEMAPS/backend.md`, `docs/CODEMAPS/data.md`,
`docs/CODEMAPS/dependencies.md`.

Noted while surveying and **not caused by this change**: `docs/CODEMAPS/data.md`
and `backend.md` describe a `SBOM []SBOMComponent` field and a `SBOMComponent`
struct that **exist nowhere in the code**. The codemaps are stale there already;
worth removing along with the rest rather than leaving a type nothing declares.

`docs/backlog.md` §1.1 mentions `sbomArgs` in the record of an earlier fix.
That is a historical entry and should be left as written — the removal gets its
own entry rather than rewriting what happened.

#### Validation

```bash
go build ./... && go vet ./... && mise run lint && go test ./...
grep -rin "sbom" --include=*.go .   # expected: no match
```

Plus one manual check: a `config.yaml` carrying `generate_sbom: true` must still
load without error.

#### What it took

Executed as surveyed, with three departures worth recording.

**`internal/ui/security/header.go` was missing from the survey** — the
`GetHelpContent` "Scan Types" section carried an `SBOM Generation:` line. The
survey listed the help text under "two strings to reword" but named only
`fields.go`; this one is a deletion, not a rewording.

**`TestEveryTrivyBuilderRefusesAnUnusableServer` would have been left checking a
single builder.** It asserted that `trivyMisconfigArgs` and `sbomArgs` both
refuse `":"`, and the second was being deleted. `trivySecretArgs` also takes a
server address and was covered by nothing, so it took the SBOM line's place —
the test now means what its name says again rather than shrinking to one case.

**Two codemap blocks were fiction, not merely stale.** The survey noted `SBOM
[]SBOMComponent` and `SBOMComponent` exist nowhere. Checking the rest of the
same block, neither do `Vulnerability`, `Secret`, `Misconfig` or `License`: the
real `Result` carries one flat `Findings []Finding`, and which family a finding
belongs to comes from `scan.Categorize`. Removing only the SBOM lines would have
left four fabricated types looking reviewed and correct, so the `Result` block in
`backend.md` and `data.md` was rewritten to describe what the code actually
declares.

Point 2 of the survey — that no config migration is needed — is now a test
rather than a claim: `TestAConfigCarryingTheRetiredSBOMKeysStillLoads` writes a
`config.yaml` carrying `generate_sbom` and `sbom_output_dir` and asserts it
loads with the surrounding settings intact. It would fail the day someone adds
`KnownFields(true)` to the loader without thinking about the files already on
disk.

Coverage: `internal/scan` 98.1 %, `internal/config` 91.0 % and
`internal/ui/security` 87.2 % all unchanged; `internal/ui/configuration`
83.4 % → 83.2 % and the project total 81.4 % → 81.3 %, both purely the
arithmetic of deleting a covered statement — a function-by-function diff shows
every percentage identical.

### 3.13 A sortable column keeps room for its sort arrow — **done**

**D33 — the sort arrow was truncated on any column narrower than its own
header plus two.** Reported from use on the inventory's `CRIT` and `HIGH`, which
asked for 5 and rendered `CRIT ▼` into it.

`Column.MinWidth` is the view's statement about the column's *content*.
`titleFor` then appends an arrow to the header, and `solveWidths` knew nothing
about those two cells — so the component silently widened the thing it was
sizing. The fix belongs there rather than in the view: `askFor` reserves
`width(Title) + sortArrowWidth` for any column carrying a `Less`, and the arrow
strings are named constants so the renderer and the solver cannot drift.

Reserved for **every** sortable column, not only the sorted one: reserving on
demand would resize the column each time `.` moved the sort and shift every
column beside it.

Auditing the application afterwards, `CRIT` and `HIGH` are the **only** two
columns the reserve changes — every other sortable column already had the room.
The four count columns were then pinned to one width (`countColumnWidth`), since
the reserve alone would leave `CRIT`/`HIGH` at 6 and `MED`/`LOW` at 5: four
adjacent columns of the same kind, ragged.

`TestTheWidthsAlwaysSumToWhatIsAvailable` gained the inventory's shape, because
raising what a column asks for is another way to push the total past what is
available and Rule 116 has to survive it.

### 3.12 The Secrets tab shows both scanners, and one rule decides where a finding goes — **done**

Found by reviewing the security header after §3.11, and fixed with it. Four
defects, one cause: nothing owned the question "what kind of finding is this?".

**D29 — two classifiers, and three findings fell between them.**
`Result.CountFindings` switched on `Source` alone and sent everything unmatched
to the severity counters; `countFindingsByTab` switched on `Source` plus
`PkgName` plus `Match`. They disagreed on a `trivy` finding with no `PkgName`,
on an undeclared source, and on a `trivy` finding carrying a `Match` — each of
which was **counted in the header and shown in no tab at all**. Same family as
D24, D25 and D26: two copies of a rule, one of them drifted, nothing said so.
`scan.Categorize` is the only rule now, and it switches on the source alone.

**D30 — Trivy's secrets were parsed and dropped.** `TrivyResult.Secrets` and
`TrivySecret` were declared and unmarshalled into; nothing ever ranged over
them. The help claimed "Secret Scan … (Gitleaks + Trivy)" throughout. Same shape
as D27: declared, populated, read by nothing, silent about it. They are read
now, under a source of their own — `trivy-secret`, which is also what let the
classification stop guessing from `Match`.

**D31 — an image scan ran a secret scan and threw it away.** Trivy's default
scanners for an image are `vuln,secret`, and `trivyArgs` passed no `--scanners`
flag for that target type. So every image scan paid for secret detection whose
output was discarded — and would have reported each secret twice once they were
read. The vulnerability stage now says `--scanners vuln` explicitly.

**D32 — `i` on a Trivy secret would have written a fingerprint that matches
nothing.** `.gitleaksignore` is keyed on a Gitleaks fingerprint;
`AddToGitleaksIgnore` falls back to building one from file, rule and line when
the finding has none. With Trivy secrets in the same tab, `i` would have written
that fabrication and reported "Added … to .gitleaksignore" for a line Gitleaks
will never match and Trivy never reads. It is offered for Gitleaks findings only
now, and refused with a reason otherwise (Rule 128, Rule 130).

Gitleaks and Trivy are **not redundant** — one reads git history, the other the
target's content — so both run when `scan.enable_secret` is set, in separate
stages with separate progress rows and separate error messages. Only Trivy's
half applies to an image, which is what gives an image a secret scan at all.

**The security header now carries the context and one count, and nothing else.**
`buildInfoLines` renders exactly seven lines and drops the rest in silence; the
results state sat at exactly seven, so an eighth field would have vanished. The
tool versions answered the dashboard's question, `Filter` read `ALL`
permanently, and `Secrets`/`Licenses` duplicated the tab bar one line below. The
context was the one thing missing, and it is the view where it matters most: the
workspace scan cache is scoped to a context, so identical repository rows mean
different things in two of them (§3.39 later un-scoped the image half, which
never was). `parseVersion`, `looksLikeVersion` and `renderSeverityBar` went
with their only caller.

### 3.11 `security` becomes an inventory — **phase 2 done**

Phase 2 of [`configuration-view-plan.md`](../.claude/plans/configuration-view-plan.md).
Phases 0, 0b and 0c shipped as D26, the per-context scan caches and D27; phase 1
shipped the configuration view. This is the landing page that replaces the form,
and phase 3 is what deletes the form.

`:sec` opened on a form asking what to scan and with which options. Every one of
those options now comes from the configuration view (§1.1, D26), and what gets
scanned is either an image the registry knows or something under
`workspaces_dir` — so the form was asking two questions that had already been
answered elsewhere. It now opens on **everything this context has scanned**,
read from the two scan caches: one `datatable` over images and repositories,
sorted by CRITICAL descending, `theme.TimeAgo` for the age (Rule 127).

`enter` opens a row's stored findings, `ctrl+s` rescans one, `ctrl+a` purges and
rescans all (Rule 126), `ctrl+r` reloads from the caches.

Four things settled while building it:

- **The inventory runs its own scans.** With the options in the config there is
  nothing left to carry to whoever would run one, which is the whole reason the
  cross-view delegation existed. It writes to the same two caches, so a rescan
  here and a `ctrl+s` in the images list are the same operation.
- **`ctrl+a` purges the counts, not the rows.** The rows *are* the list of what
  has been scanned; dropping them would empty the view for the length of the
  scans and lose the targets entirely on a close. A purged row prints `-`, not
  `0` — nothing found and nothing known are different answers, and zero is the
  one that reads as clean.
- **A reload keeps an in-flight scan's marker.** The cache says nothing about a
  scan that has not finished writing to it, so a refresh landing mid-rescan
  would clear the spinner and leave the row looking settled.
- **A finished rescan is routed to the security view wherever the user is**
  (`routeToSecurityView`), for the reason `routeToOCIImagesView` already exists:
  the router forwards everything else to the active view only, and a lost
  completion leaves a row spinning for the life of the view.

`homeState` records where `esc` and `ctrl+r` return to from the results — the
inventory for a view opened on `:sec`, the form for one opened with a target
prefilled. A scan that *fails* uses it too: one started from the inventory must
not land the user on a form they never opened. The field disappears in phase 3,
when there is only one answer left.

`datatable.Config` gained `SortDesc`. Ascending is the useless end of a count
column, and cycling `.` past it on every open is not a default. A direction with
no sortable column to apply it to is dropped along with the column, or the first
`.` would open on descending with the arrow on nothing.

Not touched, and deliberately: `OriginView` (it carries navigation, not options
— and gains a third origin), the dependency banner (the dashboard already shows
`shared.State.Tools`), and the form itself, which stays reachable through
`NewWithTarget` and `NewWithImageTarget` until phase 3.

Coverage: `internal/ui/security` 85.6 % → 85.8 %, project total 81.3 % → 81.4 %.

### 3.18 A registry member is an address, not a URL — `repo_prefix` — **done**

Done on 2026-08-23. §3.8 gave a group its members; this is about *reaching* one.
It closes D39. D40, which it needed closed with it, was fixed on its own — see
§1.1.

Found on 2026-08-10 trying to browse a single proxy inside a Nexus group. All
measurements below are from that instance — `pic-nexus.spw.dev.wallonie.be`, 64
repositories, 22 of them docker — and every one of them was an anonymous GET.

#### What was measured

Four ways to reach the same image, and no two of them derive from each other:

| Form | `…/tags/list` | What `docker pull` asks for | Pull |
|---|---|---|---|
| **A** host, repo `dhi-io-proxy/eclipse-temurin` | 200 | `/v2/dhi-io-proxy/eclipse-temurin/…` → 200 | ✅ |
| **B** host `…/repository/dhi-io-proxy`, repo `eclipse-temurin` | 200 | `/v2/repository/dhi-io-proxy/…` → 404 | ❌ |
| **C** group subdomain, repo `eclipse-temurin` | 200 | `/v2/eclipse-temurin/…` → 200 | ✅ |
| **D** host, repo `docker-unsecure-group/eclipse-temurin` | 404 | — | — |

B is what `NexusDetector` builds today, and it is the one form that cannot pull.
D is the same trick as A applied to the group, and it does not work: a group is
reachable only through its own connector.

And A is settled **per repository**, not per instance:

| Proxy | A | B |
|---|---|---|
| `dhi-io-proxy` | 200 | 200 |
| `k8s-io-proxy` | 200 | 200 |
| `docker-io-proxy` | 404 | 200 |
| `quay-io-proxy` | 404 | 200 |

`docker-io-proxy/library/nginx` answering B and not A is the decisive pair: the
image is there and reachable, the path-prefix route is not. Whether a Docker
repository answers on a path prefix, a dedicated connector port or a subdomain is
a setting on **that repository** — `docker.httpPort`, `docker.httpsPort`,
`docker.subdomain`.

#### Why no synthesis can be right

Those three fields live in the repository's detailed configuration, behind
`GET /service/rest/v1/repositories/{format}/{type}/{name}` — which answers **403**
here for an ordinary pull account. The public
`GET /service/rest/v1/repositories/{name}` returns the summary shape,
`"attributes": {}`, carrying neither `memberNames` nor the connector fields.

So the one endpoint that would say what the members are is also the one that would
say how to reach them, and an instance that refuses it refuses both. That settles
the question the same way §3.8 decision F settled `provider`: **the addressing is
declared, not sniffed.** Guessing is what D39 already is.

#### The design — one field

`repo_prefix`, on `config.RegistryItem` and on `cache.RegistryGroupMember`. The
four connector modes collapse onto the pair `(url, repo_prefix)`:

| Mode | `url` | `repo_prefix` |
|---|---|---|
| path-based routing | `pic-nexus.spw.dev.wallonie.be` | `dhi-io-proxy` |
| HTTP/HTTPS connector | `pic-nexus.spw.dev.wallonie.be:8082` | — |
| subdomain routing | `dhi-io-proxy.spw.dev.wallonie.be` | — |
| group connector (form C) | `pic-nexus-docker-unsecure-group.spw…` | — |

Both directions derive from that one pair, which is the whole point: the prefix
goes in front of the repository name, the host stays `url`, and browse and pull
cannot disagree about which repository they mean — the disagreement being exactly
what D39 is.

Apply it **once**, in `submitSearch`, where the entry is in hand. The prefix is
then already part of `MultiRegistryTag.Repo`, so `multiImageName` needs no change
and neither does `registryAPIURL`. Teaching both of them about the prefix would
be two places free to drift — the same argument that gave the configuration view
one pointer accessor instead of a get/set pair.

#### What it unlocks

The request this came from: **one checkbox per proxy**. Declare a proxy per line
(`kind: registry`, `url: <host>`, `repo_prefix: dhi-io-proxy`) and the picker
built in §3.8 already does the rest. No discovery in the critical path — which
matters precisely because discovery is what is 403 here.

That is what **D40** had to be fixed for: those entries all share one host, and
until the picker keyed its selection on the entry rather than on the URL, one
per proxy meant one checkbox for all of them. It is fixed, so this no longer
waits on anything.

#### Scope

| Site | Change |
|---|---|
| `config.RegistryItem` | `repo_prefix` field; absent in an existing config and that stays valid |
| `config/registries.go` | validate at load: no leading or trailing `/`, refused on `kind: group` |
| `registrymgr.GroupMember` | carry a prefix instead of a synthesised URL |
| `NexusDetector` | emit `(host, prefix)`; leave the prefix empty when it cannot know |
| `cache.RegistryGroupMember` | carry it, so a discovered member can too |
| `RegistryForm` | one text field, shown for `kind: registry` |
| `submitSearch` | prepend the prefix to the repo, once |
| `entryFor` | resolve a result by entry key, not by URL — every member of a host answers to the same URL once the prefix carries the difference, so a result becomes unattributable (the one site D40 deliberately left alone) |
| `memberKey` | follow whatever tells two members apart once it is no longer the URL |
| Registries tab | show it — an entry whose URL is a bare host says nothing on its own |

Deliberately not done:

- **No probing to pick the form.** Trying A, then B, then a connector port is
  several requests per member per search to answer what the config can state, and
  it would make a member's address depend on which probe answered first.
- **No prefix on a group.** Form D is 404. A group's address is its connector,
  and that is its `url`.
- **No attempt to make `NexusDetector` fill the prefix in** where the detailed
  endpoint is refused. An empty prefix against a bare host is wrong and visibly
  so; a synthesised `/repository/` URL is wrong and plausible, which is D39.

#### The tests, written first

All three were written before the change and checked against the pre-fix code.

- `TestAPrefixedRegistryBrowsesAndPullsTheSameRepository` — the pair D39 fails:
  the browse asks `<host>/v2/<prefix>/<repo>/tags/list` and the pull reference is
  `<host>/<prefix>/<repo>:<tag>`. It failed on both halves before the fix.
- `TestARegistryWithNoPrefixIsUnchanged` — byte-identical requests to before,
  which is what makes this an addition rather than a new address for everything
  that already worked.
- `TestARepoPrefixOnAGroupIsRefused` and
  `TestARepoPrefixWithASlashAtEitherEndIsRefused` — normalization refuses both
  rather than correcting them, so the join has nothing to guess about.

Two more came out of doing it: `TestAMemberIsAddressedByPrefixAndNeverBySynthesisedPath`
pins what the detector emits in each of the four connector shapes, and
`TestTwoPrefixesOnOneHostAreTwoEntries` is D40 seen from the case that makes it
ordinary.

#### What it turned out to touch

The scope table above held, with one addition the design implied and did not
name: **the results themselves**. `MultiRegistryTag` and both registry messages
carry an `EntryKey`, because a tag arriving from `nexus.example.com` no longer
says which of that instance's proxies answered — so the result filter, the filter
labels and the last-updated enrichment are all keyed on the entry now. Keeping
`RegistryURL` beside it is deliberate: it is what the pull reference is built
from, and only that.

`registryRef(url, prefix)` is the one place an entry is turned into something a
user reads, and the Registries tab, the browser's picker and its filter labels
all go through it. It is the head of the pull reference, exactly — which is what
makes it the honest thing to show.

### 3.19 The dashboard stops reflowing, and gains resource charts — **done**

Supersedes §3.5. Full plan, with what each phase cost:
[`dashboard-resources-plan.md`](../.claude/plans/dashboard-resources-plan.md).

Two things, and they turn out to be one. The view fills in as its data lands,
and it has no live resource metrics.

#### The reflow is structural

`render*Section` builds a line count that depends on state — GitLab is 3 lines
loading, 7 loaded, 4 when not connected; OCI 3 → 6; Tools 3 → 2+N. `View()`
stacks the sections and equalises the two columns on the taller one, so **a
value landing in the right column moves the left column too**.

The fix is a rule: **a section declares its height and fills it. Data changes
values, never line count.** Which needs three value states where there are
currently two:

| State | Rendered | Meaning |
|---|---|---|
| unknown | `-` `DimStyle` | not measured yet |
| unavailable | `n/a` + one dim line | Docker absent, GitLab signed out |
| zero | `0` `DimStyle` | measured, and it is zero |

Today "Docker not available" *replaces* the block. With a skeleton the labels
stay — the user sees what the dashboard would show, which is itself an answer.

**And it makes the age mandatory.** Once a stale value looks exactly like a
fresh one, "when was this true" has to be on screen. `m.lastRefresh` is stored
today and never rendered; that stops being acceptable, not as polish but as the
price of the placeholders.

The vertical budget is already overspent — left column ≈22 lines, right ≈20,
viewport gets `height - 11`, so a 30-row terminal cuts the rest **in silence**.
Hence one `viewport` over the whole content (the idiom in four views already),
not one per column: two scrolling columns means two cursors.

#### The layout — no outer frame, four boxes, two tabs, three tiers

Text-only boxes on one side, chart-bearing boxes on the other. At 4K a column is
≈118 cells, and a 100-cell braille chart holds 200 samples — over three minutes
of history. That is a graph, not an ornament.

**The dashboard is the one view that is not one thing**: every other is a table
or a form, so its border surrounds one object. Seven heterogeneous cards under a
single frame say nothing about which value belongs with which. So the router
grows a frame opt-out — default framed, unlike `HeaderView`'s silent half — and
the dashboard draws **one titled box per logical group**. `renderTitleLine`
gives a frameless view a titled rule with no corners, so `GetTitle()` keeps a
reader.

**Four boxes, because at four the framing is free**: two stacked boxes cost 5
chrome lines per column against today's 3, and the outer frame gives 2 back. At
seven it costs six lines on a budget that already truncates in silence. The
groups regroup by question asked rather than by data source — `Code` (GitLab
plus workspaces: the explorer creates, workspaces reconciles), `Health`
(monitors, certificates, security posture), `Host (Windows)` (CPU, RAM, disk,
tools — this machine's binaries, measured by the same probe), `Docker (VM)`.

**A tab exists only for content with no view of its own.** `Overview` and
`Resources`; a `Health` tab would be a fourth copy of rows `:status` and `:sec`
already own.

**The tier decides where a fact is, never whether it exists.** A terminal knows
columns and rows, not pixels — two font sizes on one 4K screen are two
terminals. `compact` (<100 wide or <26 high) stacks one column; `standard` fills
16 lines exactly; `wide` (≥180 × ≥45) opens a **third** column, because two
columns at 240 cells is framed emptiness. That third column holds the
`Resources` tab's content, which is what makes the scheme safe: inline at 4K,
one `Tab` away below it, never absent. One function computes the tier, for the
same reason `scan.Categorize` alone decides a finding's family.

The overflow is therefore designed away rather than scrolled; the router's
`viewport` stays only as a safety net below 26 rows. And the sample history
belongs to the model rather than to the chart: `ntcharts.Resize` rescales its own
ring buffer, so a tier change would truncate the history at the moment the user
enlarged the window to see more of it.

#### What was measured, on 2026-08-14

Windows 11, 16 cores, 33.4 GB, Docker Desktop, 9 containers / 2 running. None of
this is quoted from documentation.

**`gopsutil/v4@v4.26.7`** builds with `CGO_ENABLED=0` — same no-cgo constraint
that picked `zalando/go-keyring`. `mem` 0 ms, `net` 5 ms, `disk.Usage` **1 ms**,
`cpu.Percent(500ms)` 501 ms.

⚠️ **`load.Avg()` returns `{0,0,0}` with `err=nil` on Windows.** It does not
fail — it produces a number indistinguishable from data. Load average is
therefore displayed on **no** platform: a metric present on two of three is
worse than one present on none, because its absence reads as "idle".

Two more that change the code: `cpu.Percent(interval, …)` blocks for the
interval, so the sampler uses `cpu.Percent(0, false)`; and `net.IOCounters` is
cumulative, so a rate is a delta and the **first** sample has none — it prints
`-`, not `0`.

**`docker stats --no-stream` costs 1365 / 1982 / 1993 ms**, against 603 ms for
`docker system df`. A 2-second chart tick would keep the CLI running
continuously, which is what forces **three clocks**: ~1 s for the gopsutil
sample (5 ms), ~5 s for `docker stats` (2 s), and the existing
`status.refresh_interval` for GitLab, `system df`, workspaces and tools. One
tick driving all three is exactly what makes the cheap call wait on the
expensive one.

`calculateDiskUsage` (`du -sh` over the workspaces tree) leaves the periodic
refresh with them. It is the one call whose cost grows with the user's data, and
`disk.Usage` answers the useful question — how much room is left — in a
millisecond.

#### Host and Docker are not on one axis

`workspaces_dir` is `C:\Users\anthoni\workspaces`, so `dk.exe` runs natively on
Windows: gopsutil reports Windows, `docker stats` reports usage *inside* the
Docker Desktop VM, whose footprint is a subset of the Windows totals. Both true,
**not additive**. So two sections and a label naming the measurement point —
`Host (Windows)`, `Docker (VM)`.

Running `dk` *inside* WSL is the misleading case: gopsutil would read the
distro's `/proc`, and Docker Desktop's containers live in another distro, so
they would appear nowhere. Detectable via `/proc/version`, and to be said in the
label rather than hidden.

#### `ntcharts` — and the version is the whole question

`NimbleMarkets/ntcharts` (MIT, 776★) is the only charting library written *for*
Bubble Tea. `termui`, `termdash` and `tvxwidgets` are TUI frameworks that want to
own the event loop; `asciigraph` has no lipgloss.

⚠️ **`@latest` is v2, and v2 requires `charm.land/bubbletea/v2` + `lipgloss/v2`.**
A plain `go get` would drag the whole application into Bubble Tea v2. **The
version is `v0.5.1`**, the last of the v1 line, compiled and run against
bubbletea v1.3.10 / lipgloss v1.1.0 / bubbles v0.21.0.

Three properties that meet the house rules:

- **`Draw()`, never `DrawColumnsOnly()`.** `Draw()` styles the whole canvas, so a
  style carrying `Background(theme.ColorBackground)` fills the troughs.
  `DrawColumnsOnly()` styles only the columns and lets the terminal's native
  background through the gaps — the exact defect Rule 115 forbids.
- Every rendered line is **exactly `width` cells** (`len=40`, `len=48`
  verified), so Rule 116's arithmetic is untouched.
- The model owns its ring buffer, so there is no history to write — but `Push()`
  belongs in `Update()`, never in a `Cmd` (Rule 110).

#### What the dashboard starts saying

Free space on the workspaces volume and the Docker root; **reclaimable** Docker
space, which is already in the `system df` output the view parses and discards;
security posture from `ImageScanCache` and `WorkspaceScanCache` — targets
scanned, open CRITICALs, **targets never scanned**, oldest scan — which finally
connects the dashboard to §3.11's inventory without running one; the nearest
certificate expiry, from components already in memory; and the refresh age.

The coverage figure took the place of a HIGH tally, and the reason is that it
**decides something**: it names the targets the whole box says nothing about,
and the answer is to run a scan. One more HIGH changed no decision the CRITICAL
above it had not already taken. It is the one figure not read from the caches —
`readPosture` counts what has been scanned, and the inventory it is subtracted
from (`docker system df`, the workspaces count) is already in the model, so the
subtraction happens in the view. It reports `(n, measured)` rather than an `int`:
an inventory not yet loaded would otherwise render `0`, and *nothing left to
scan* is the exact opposite of *not known yet*. It is floored at zero, because
the cache outlives a deleted image.

The Code box carries, under the path it describes, **what that tree occupies** —
not the volume's fill level. The two answer different questions and only one of
them is actionable: deleting a workspace gives the space back, whereas the
volume mixes the workspaces in with everything else on the machine. The Host box
keeps free space, which is the other half.

That is a `du` by another name, and this same section had removed one — so the
conditions it comes back under are the point:

- It has **its own command**, off the one-level `os.ReadDir` that counts the
  workspaces. That count is cheap and has no reason to pay the walk's price.
- It runs **once per slow round**, not on every dashboard refresh, which is what
  the removed `du -sh` did.
- **Never two at once.** `Model.measuringSize` exists only for this: it is the
  one call in the view that can outlast the interval that triggers it, and a
  slow round has no way to know. The flag is raised in `Update()` (Rule 110),
  and lowered by `WorkspaceSizeMsg`.
- Measured rather than assumed: `C:\Users\anthoni\projects`, 2.38 GiB, **811 ms**
  cold. Well inside a 30-second round.
- Nothing is excluded, `.git` included — a clone costs its history as much as its
  working tree, and the history is usually the heavier half. Symlinks are not
  followed, which rules out both cycles and double counting.
- A directory it cannot read makes the result `Partial`, and the node says so.
  An underreported total with nothing to mark it reads as a measurement.

#### Two columns inside a box

Health and Docker split their lower half in two — `sideBySide`, assembled line by
line with `PadWithBg` and never `lipgloss.JoinHorizontal`, which inserts bare
spaces that let the terminal's own background through (Rule 115).

Health's four trees stacked ran to nineteen lines, which made it the tallest box
of its row and took those lines off the three charts above it. Split, it is
eleven. The split is by **subject, not by kind**: supervision with the
repositories it watches, certificates with the images.

Three things the split forced, each of which would be a defect without it:

- **A narrower value column** inside a split box (`narrowTreeLabelWidth`).
  At 180 columns — the narrowest `wide` — half a box is 27 cells, and the wide
  column would leave 8 for the value. `3 days ago` is 10.
- **The certificate's name is gone** from the expiry node, which now hangs from
  `Certs` rather than floating above the trees. The days are what decide
  something; `:status` owns the named list.
- **Both columns pad their first tree to the same height.** The expiry gives the
  certificates one node more, so without it `Repositories` would open a line
  above `Images` and the two lower trees would read as a staircase.

A two-cell gutter is taken **off the left column**, not added to the right: a
value filling its half otherwise touches the next tree's elbow and the two read
as one.

#### What moved out of duplication

Three figures were in two boxes each, and the second copy was dropped:

| Figure | Was | Now |
|---|---|---|
| the workspaces path | Storage's first line, and the Code tree | Code only, under the tree that talks about it |
| free space | Host's `Disk` row, and Storage's `free` | Storage, which details the volume |
| image and volume sizes | Docker (VM) as `8 (1.2GB)`, Storage as reclaimable only | Docker counts, Storage sizes |

That last split is the general rule the boxes now follow: **Docker (VM) answers
"how many", Storage answers "how much space".**

Storage was thin because it read one figure out of `docker system df` and
discarded the rest. It now carries both trees the command already pays for — the
volume (capacity, used with its percentage, free) and Docker's own breakdown,
**build cache included**. That fourth row was parsed, summed into the
reclaimable total, and its own size thrown away — and it is the one that most
often answers where the disk went.

#### Un lien symbolique ne pèse rien, et CI l'a dit avant nous

`metrics.Size` ajoutait la taille de l'entrée d'un lien symbolique. WalkDir ne
suit pas le lien — c'était acquis, et c'est la moitié qui allait de soi — mais ce
qu'il rend pour lui est la **longueur du chemin qu'il désigne** : la taille d'un
arbre bougeait donc quand on renommait un dossier ailleurs. Le parcours ne
compte plus que les fichiers réguliers.

Ce qui compte autant que le défaut : `TestSizeDoesNotFollowSymlinks` existait, et
il **se saute sous Windows**, où créer un lien demande un privilège que le compte
de test n'a pas. Il n'a donc jamais tourné sur la machine de développement, et
c'est CI — Linux — qui l'a exécuté pour la première fois. Un test qui se saute
sur la seule machine où on le lance ne dit rien du tout, et rien ne le signale.

Deferred: workspace hygiene (`3 dirty, 2 behind`, §3.17). Right data, but the
walk cost grows with the repository count. Rejected: listening ports — `ss`
needs a privileged container.

#### Bubble Tea v2 is deliberately not bundled

v2 is GA (bubbletea v2.0.8, lipgloss v2.0.6, bubbles v2.1.1); v1 is not
deprecated; and **nothing here needs v2** — ntcharts v0.5.1 delivers braille and
multi-series stream charts on v1, verified by running it.

The reason for separating them, above all others: in v2 `msg.String()` returns
`"space"` instead of `" "`, and there are **10 `case " ":` across 10 files**. The
compiler says nothing — `case " ":` stays valid Go and simply stops matching.
Rule 135 makes `Space` the only key allowed to tick a checkbox, so a careless
migration silently breaks every checkbox in the application. That has to surface
in a pull request that does nothing else.

| Migration work | Extent | Nature |
|---|---|---|
| Imports → `charm.land/*/v2` | 154 files / 301 | mechanical |
| `tea.KeyMsg` → `tea.KeyPressMsg` | 96 | compiler-found |
| `View() string` → `View() tea.View` | 11 views + router | **architecture** |
| `lipgloss.Color` as a type → `color.Color` | 46, nearly all in `colors.go` | one file |
| `WithWhitespaceBackground` → `WithWhitespaceStyle` | 13 | mechanical |
| `AdaptiveColor` / `TerminalColor` | 0 | no `compat` needed |

The rules paid for themselves: Rule 119 (no hex outside `colors.go`) reduces
lipgloss v2's largest change to one file, and `datatable` rendering its own rows
(Rule 122) blunts the `bubbles/table` changes. The one real piece of
architecture is that the router types views as `tea.Model` (`app.go:47`); the
better answer is the inverse — DevDesk declares its own `View` interface and
only `*App` stays a `tea.Model`.

And ntcharts v2 is no reason to hurry: its `go.mod` carries
`replace charm.land/bubbletea/v2 => github.com/neomantra/bubbletea/v2` under the
comment *"Awaiting upstream merges"*. Migrating for it today would trade a stable
v1 stack for a dependency on a fork.

#### Tests worth writing first

- Each section rendered unknown / loaded / unavailable has the **same line
  count** — without it the next section added reintroduces the reflow.
- `-` and `0` are distinguishable, and an unavailable source keeps its labels.
- A cumulative counter read once yields **no** rate; a counter reset drops the
  sample rather than rendering a negative throughput.
- Every chart cell carries a background (Rule 115) and every chart line is
  exactly the column width (Rule 116).
- Load average is displayed nowhere — pins the Windows trap against someone
  re-adding it because it works on Linux.
- Every fact rendered at `wide` is present, inline or in a tab, at `compact`;
  and the overview fits at 30 rows without the safety-net viewport scrolling.
- Only the dashboard is frameless — the router's opt-out stays an exception
  rather than a habit.

---

### 3.20 Secrets are shown where they are found, and table text takes the theme — **done**

Plan : [`secrets-column-and-datatable-foreground.md`](../.claude/plans/secrets-column-and-datatable-foreground.md).

Deux demandes sans rapport, sauf qu'elles se corrigent dans les mêmes tables.

#### Le verdict « secrets » était faux là où il existait

Deux calculs, la même boucle recopiée — `workspaces/commands.go` et
`hasScanSource` dans `security/inventory_commands.go` — tous deux écrits « une
finding dont `Source` vaut `gitleaks` ». C'était vrai jusqu'à ce que Trivy se
mette à trouver des secrets lui aussi : depuis, un dépôt dont c'étaient les
seuls se lisait **propre**, et une image n'avait aucun champ de secrets du tout.
`scan.Categorize` est le seul classeur (§3.11) et `SecretCount` en est le
résultat ; `Result.SecretVerdict()` est désormais le seul calcul du verdict.

#### Et il n'est pas binaire

Une étape secrets peut ne pas avoir tourné pour trois raisons : l'option est
coupée, l'outil est absent (`scanner.go` exige `deps.*Available` sur les deux
étapes), ou l'étape a échoué. Un `false` dans ces cas-là est une icône verte
apposée à un scan qui n'a **rien regardé** — ce que D20 interdit déjà mot pour
mot dans `Scan()` : *« nothing looked at this image » ne doit pas se lire « this
image is fine »*. Le verdict est donc un `*bool` : `nil` = personne n'a cherché.

`Result.SecretsScanned` porte le fait, mis à vrai par une étape qui **aboutit** ;
c'est le scanner qui sait quelles étapes ont tourné, et les trois vues n'avaient
pas à le redécouvrir depuis les options.

| Cache | Avant | Après |
|---|---|---|
| `WorkspaceScanEntry.Sensitive` | `bool` | `*bool` |
| `ImageScanEntry.Sensitive` | *absent* | `*bool` |

La migration est gratuite, et pas par chance : l'ancien champ workspace
s'écrivait **toujours** (`json:"sensitive"`, sans `omitempty`), donc un fichier
existant décode en pointeur non nul, verdict compris. Une entrée d'image n'a pas
la clé et décode `nil` — ce qui est la vérité sur elle : le scan qui l'a écrite
n'avait pas d'étape secrets.

#### Où ça s'affiche

Une colonne `Secrets` de 7 cellules dans `oci/images` et dans l'inventaire
`:sec`, à l'iconographie de `ws` — `theme.SecretsState` la porte maintenant pour
les trois vues, ce qui a supprimé le style de `ws` qui décidait sa couleur **en
comparant la chaîne d'icône déjà rendue**.

Elle ne trie ni ici ni là. `datatable` réserve `largeur(titre) + 2` à une colonne
triable pour sa flèche, et neuf cellules pour un glyphe se paieraient sur
`Target` à 80 colonnes, dans la table la plus serrée de l'application.

`inventoryColumnCritical` est passé de 1 à 2 : une constante d'indice périmée ne
trie pas mal, elle **ne trie plus** — `datatable` laisse tomber une direction qui
ne désigne aucune colonne triable. Deux tests indexaient leurs cellules par
numéro ; ils résolvent la colonne par son titre maintenant, la flèche retirée et
la comparaison exacte, sans quoi « Content Size » répond à une recherche de « C ».

Dans le dashboard, un nœud `secrets` entre `critical` et `unscanned`, dans les
deux arbres de Health — donc au palier `wide` seulement, les deux autres étant
déjà à leur plafond de six lignes. Il compte des **cibles**, pas des secrets :
deux dépôts sont deux décisions, quarante fuites dans le même n'en font qu'une,
et `:sec` détaille. `postureSide.SecretsKnown` existe pour la même raison que le
reste de cette section : sans lui un inventaire entier scanné sans étape secrets
afficherait `0`, c'est-à-dire « aucune cible n'en porte ». Un verdict connu sur
une partie suffit à afficher le compte — c'est alors un plancher, et un plancher
non nul se décide.

#### Le texte des tables ne venait pas du thème

`theme.DefaultTableStyles()` et `BlurredTableStyles()` règlent `Header` et
`Selected`, jamais `Cell` ; `bubbles/table.DefaultStyles()` ne lui donne qu'un
padding. `datatable.cellStyle` complétait le **fond** d'une cellule sans `Style`
(Rule 115) et pas le **texte** : toute colonne sans `Style` sortait donc dans le
foreground par défaut du terminal. Quatre vues l'avaient contourné à la main —
`containers`, `oci_resources`, `security`, `workspaces`, le même
`Foreground(theme.ColorText)` recopié, ce qui est la forme que prend un défaut
manquant.

**Le correctif ne peut pas aller dans `DefaultTableStyles()`**, et c'est ce qui
décide où il va : les cellules sont rendues puis la ligne entière est passée à
`styles.Selected`, donc une couleur sur `Cell` ouvrirait une séquence dont le
reset referme le surlignage au milieu de la ligne. La couleur se pose par
cellule, dans le renderer qui sait si la ligne est sélectionnée. Un test l'énonce
à l'envers (`TestTheSelectedRowKeepsItsHighlightWhole`) pour que le
« correctif » évident ne repasse pas.

Le padding d'une cellule sort en segments de sa seule couleur de fond ; exiger un
foreground sur une espace serait exiger une séquence qui ne change rien à
l'écran. Le garde-fou ne porte donc que sur les segments qui montrent du texte.

#### Ce qui n'est pas couvert

Quatre tables ne sont pas des `datatable` et gardent le foreground du terminal :
Registries, le browser de tags, network-inspect et les résultats netdiag. Aucune
n'est dans les vues signalées, et elles ne se corrigent pas de la même façon —
même conflit avec `Selected`. La voie est la migration vers `datatable`, non
faite ici.

#### Tests écrits en premier

- Un secret trouvé par **Trivy seul** est un verdict — la fixture est exactement
  l'entrée que l'ancienne règle ratait, puisqu'elle ne contient aucune finding
  `gitleaks`.
- Les trois façons de ne pas avoir cherché rendent `nil` : option coupée, outil
  absent, étape en erreur.
- Un fichier de cache legacy relit ses deux verdicts ; une entrée d'image sans la
  clé se lit `nil`.
- Les trois icônes sont distinctes dans les deux tables, et une ligne purgée par
  `ctrl+a` perd son verdict avec ses compteurs.
- Le nœud `secrets` compte les cibles, et affiche `-` quand aucun verdict n'est
  connu.
- Les trois garde-fous du foreground **tombent sur le code d'avant**, vérifié en
  le remettant.

---

### 3.21 The last four tables move to `datatable` — **done**

Sorti de §3.20 : le correctif du foreground n'atteint que les `datatable`, et
quatre tables n'en sont pas. Elles rendent leur texte dans la couleur par défaut
du terminal, sur laquelle le thème n'a pas prise, et elles ne peuvent pas être
corrigées là où elles sont — c'est la migration ou rien.

| Table | Fichier | Ce qu'elle fait à la main |
|---|---|---|
| Registries (onglet OCI) | `oci_resources/table.go:311`, `layout.go:56` | largeurs, curseur → objet par indice, **deux sources de lignes** |
| Tags du registry browser | `oci_resources/registry_browser.go:124` | largeurs, échange de styles au focus |
| Network inspect | `oci_resources/network_inspect_form.go:21` | largeurs, `SelectedContainer()` indexe `f.containers` |
| Résultats netdiag | `netdiag/view.go:152` | largeurs, **table reconstruite** à chaque mise à jour |

#### Pourquoi ça ne peut pas se régler sur place

`theme.DefaultTableStyles()` ne pose pas de foreground sur `Cell`, et lui en
poser un casserait la ligne sélectionnée de ces tables exactement comme il
casserait celle d'une `datatable` : les cellules sont rendues, puis la ligne
entière passe à `styles.Selected`, dont le reset intérieur referme le
surlignage au milieu. Le seul endroit où la couleur peut être décidée est un
renderer qui sait si la ligne est sélectionnée — et c'est ce que `datatable`
est.

#### Ce que chacune gagne d'autre

- **Rule 122 devient inexprimable.** Aujourd'hui elle n'y tient que par revue.
  Le danger est réel mais **latent** : `membersCell` rend
  `m.spinner.View() + "refreshing"` dans une cellule, et un spinner bubbles par
  défaut n'émet aucune séquence — vérifié, `View()` rend `"⣾ "` — donc rien ne
  bave aujourd'hui. Il suffit d'un `s.Style = …` pour que si.
- **Rule 116 en un seul endroit.** Les quatre recalculent leurs largeurs à la
  main ; `network_inspect_form` va jusqu'à écrire `columns[2].Width = available
  - flexName - fixedIPv4`, ce que le solveur fait pour toutes.
- **`Selected()` ne peut plus mentir.** Trois d'entre elles résolvent le curseur
  en indexant la tranche d'origine. Sans tri ni filtre c'est correct — et c'est
  précisément ce qui rend l'ajout d'un tri dangereux, puisque rien ne signale la
  dépendance.
- **Les résultats netdiag garderaient leur position.** La table y est
  reconstruite (`table.New`) à chaque mise à jour, donc le curseur retombe en
  haut ; `SetItems` ne le déplace que s'il est sorti de la fenêtre.

#### L'ordre, et le seul morceau non trivial

Les trois petites d'abord — network-inspect, tags, résultats netdiag — qui sont
des colonnes fixes sur une tranche : row type, `Cell`, et les largeurs tombent.

**Registries est le seul cas de forme.** Elle affiche deux populations dans la
même table : les entrées de configuration (`updateRegistryTable`) et les membres
découverts d'un groupe (`updateGroupMemberTable`), avec `←`/`→` entre les deux.
`datatable.Model[T]` est générique sur un seul `T`, donc il faut un type de
ligne qui porte les deux — le patron est `explorerRow`, qui existe pour la même
raison. C'est aussi là que se trouve le `SetStyles` de focus/blur, que
`Focus`/`Blur` portent déjà dans `datatable` (Rule 118).

Non compris : donner un tri ou un filtre à ces tables. La migration doit se voir
uniquement à la couleur du texte.

#### Ce que ça a donné

Les quatre sont migrées et il ne reste **aucune `bubbles/table`** dans
l'application : le paquet n'est plus importé ailleurs que pour son type
`Styles`.

La contrainte « uniquement la couleur du texte » a tenu, à une chose près qui
n'est pas une régression : la table des tags **avait déjà** un tri, et il est
passé à `CycleSort`. Le cycle est identique — Tag ↑, Tag ↓, Updated ↑,
Updated ↓, retour — parce que `datatable` cycle exactement les colonnes qui
portent un `Less`, et il n'y en a que ces deux. Les flèches d'en-tête, écrites
à la main dans `rebuildTagTable`, tombent avec. Une seule chose manquait pour
que ce soit un remplacement exact : **`datatable.SetSort`**, le pendant écrivain
de `SortState` — `submitSearch` remet l'ordre par défaut au début d'une nouvelle
recherche, et faire le tour du cycle avec `CycleSort` n'est pas ça.

Le filtre texte de la table des tags, lui, **reste dans la vue** : il réduit les
tags *avant* que la table les voie, comme le filtre par registre à côté de lui,
et sa barre est rendue dans le footer de la vue OCI, pas dans celle de la table.
C'est le cas `security` / `status` déjà documenté.

**Registres : `registryRow` porte l'indice de l'entrée de config**, `-1` pour un
membre découvert. C'était prévu comme la seule difficulté de forme, et ça a payé
tout de suite : `getSelectedRegistry` indexait `m.registries` par numéro de
ligne, ce qui n'est juste que tant que la table ne trie ni ne filtre.

**Trois défauts trouvés en migrant**, chacun avec un test vérifié contre
l'ancien code :

- **D41**, ci-dessus (§1.1) : URL clampée après le reste, deux colonnes
  négatives sous 80 colonnes.
- **network-inspect soustrayait les bordures deux fois** — la vue lui passe déjà
  la largeur de contenu — donc ses colonnes totalisaient deux cellules de moins
  que la place disponible, à *toutes* les largeurs, et la ligne sélectionnée
  s'arrêtait avant la bordure droite.
- **netdiag débordait sous 46 colonnes** : la colonne Output était plancherée à
  10 *après* le calcul du reste — le même geste que D41, mais ici c'est la somme
  qui casse plutôt qu'une largeur qui devient négative. Mesuré : 38 cellules de
  colonnes pour 32 disponibles à 40 colonnes.

Le spinner « refreshing » de la colonne Members passe au *frame* brut plutôt
qu'à `spinner.View()` : c'était la Rule 122 latente que §3.21 nommait, et un
test l'interdit maintenant.

Enfin, la table de résultats netdiag **garde son curseur** : elle était
reconstruite par `table.New` à chaque mise à jour, donc un résultat tardif
ramenait la ligne sous le curseur de l'utilisateur en haut.

---

### 3.22 A row says what it is, and what is happening to it — **done**

Plan détaillé :
[`datatable-row-status-and-busy.md`](../.claude/plans/datatable-row-status-and-busy.md).

`datatable` gagne une **colonne de statut** et la notion de **ligne occupée**.
Suite directe de §3.21 : maintenant que les quinze tables passent par un seul
renderer, c'est le seul endroit où ça peut être écrit une fois.

#### Le trou

`handleConfirmYes` déclenche la `Cmd` et retourne **sans toucher au modèle**
(`oci_resources/results.go:17`, `containers/update.go:463`). Rien à l'écran ne
dit qu'une action tourne, et `docker/containers.go:150` appelle `docker stop`
sans `-t` : dix secondes de délai de grâce par défaut, donc dix secondes de
table qui a l'air gelée. C'est le symptôme que §3.16 a enregistré pour la modale
`"Pulling..."` du clone.

**L'intention était là et a perdu son lecteur.** `containers.Model.pendingAction`
est écrit à six endroits, dont trois phrases humaines — `"Stopping web"`,
`"Restarting api"`, `"Pruning containers..."` — et quatre tests l'affirment.
**Aucun code de rendu ne le lit.** Le champ est surchargé : il porte aussi la
clé de routage de la modale (`"confirm-delete"`, `"confirm-prune"`), que
`handleConfirmYes`, lui, lit bien. Un champ, deux sens, et celui que personne ne
lit est celui qui devait se voir.

#### La décision de forme

Deux choses partagent un glyphe et ne doivent pas partager une fonction :
**ce que l'objet est** (`running`, `exited`, `paused` — docker, au prochain
refresh) et **ce qui lui arrive** (`stopping`, `removing` — DevDesk, le temps
d'une commande). Les fusionner dans le glyphe est le but ; les fusionner dans
l'API ferait ré-implémenter la préséance dans chaque table. `datatable` porte
une règle : **occupé gagne sur l'état** — et pour une raison qui se dit, `exited`
étant précisément ce qui est sur le point de cesser d'être vrai.

**Ça ne peut pas être une closure.** L'invariant du paquet est que les colonnes
sont construites une fois et ne ferment sur rien ; un `Busy func(T) string` sur
la `Config` devrait fermer sur la map des actions en cours du modèle, ce que
`imageRow` existe pour éviter. D'où la séparation identité / état : `Key func(T) string`
sur la config, l'ensemble occupé tenu par la table et écrit depuis `Update`.

Ce que ça achète et qui compte le plus : **`IsBusy(key)` répond avant que la
ligne existe**, donc c'est aussi le garde-fou du chemin de confirmation. Rien
n'empêche aujourd'hui un second `ctrl+d` sur une suppression lente de lancer un
second `docker rmi` — le second échoue en « No such image » et l'utilisateur
voit `Action failed` sur une suppression qui a marché.

#### Hors périmètre, volontairement

Les **scans** (leur spinner est dans la colonne Scanned et ils ne bloquent pas
l'objet de la même façon) et **`prune`**, qui n'agit sur aucune ligne et relève
d'une ligne de footer rendue depuis l'état de l'opération, comme `syncStatusLine`
de §3.17 — pas de `footerInfo`, dont le timer de 3 s expirerait en cours.

#### Ce que ça a donné

`datatable` porte `Key`, `StatusColumn`, `MarkBusy`/`ClearBusy`/`IsBusy`/
`BusyLabels`/`AdvanceSpinner`. **Containers est le premier client** et l'icône
d'état a quitté la cellule Image pour la colonne 0 — ce qui explique enfin
pourquoi cette colonne devait chercher sur `Image + State` : elle compensait le
fait que l'icône n'était pas là où elle appartenait.

Le curseur n'est **pas** verrouillé, conformément à la discussion : c'est
l'objet qui l'est. Trois tests le pinnent — le curseur bouge, le marqueur reste
avec le conteneur, et une seconde action sur le même conteneur est refusée.

**Trois défauts trouvés en implémentant :**

- **`ContainerActionMsg.ID` portait le *nom*, pas l'ID.** Les cinq commandes le
  remplissaient depuis leur argument `name`. Rien ne l'avait attrapé parce que
  le seul lecteur était une ligne de log, où un nom se lit très bien. Ça devient
  bloquant ici — le marqueur est indexé sur l'ID, donc un message portant un nom
  ne l'aurait jamais levé et la ligne aurait tourné à vie. Le message porte les
  deux maintenant.
- **`pendingAction` était deux champs en un** : la clé de routage de la modale,
  lue par `handleConfirmYes`, et une phrase pour l'utilisateur que **rien n'a
  jamais rendue**. Porter les deux est précisément pourquoi personne n'a vu que
  la seconde n'avait pas de lecteur — le champ était visiblement utilisé. Il ne
  garde que la clé ; ce que l'utilisateur lit vient de `BusyLabels()`.
- **La vue containers n'avait aucun timer de footer** (Rule 128) : `errorMsg`
  était posé et laissé jusqu'à ce qu'un succès ultérieur l'efface, donc un échec
  pouvait rester sous un écran sans rapport pendant des minutes. Six sites ont
  reçu `clearErrorCmd()`.

Les tests d'action portaient tous sur `pendingAction`, c'est-à-dire sur un champ
que rien n'affichait ; ils portent maintenant sur `actionLine()` et le footer
rendu — sur ce que l'utilisateur voit. Trois d'entre eux exécutaient le timer de
3 s via `testutil.MsgOf`, ce qui ajoutait neuf secondes à la suite ; deux le
faisaient sur le chemin du pager, où exécuter la commande **lancerait réellement
le processus** si le garde-fou tombait. Ils affirment l'état.

#### Étendu aux cinq autres tables

| Table | Clé | Cellule dépensée | Actions |
|---|---|---|---|
| `oci` images | ID de l'image | `ID` — ne trie ni ne cherche | suppression |
| `oci` networks | ID du réseau | `ID` | suppression |
| `oci` volumes | nom | `Driver` — un volume n'a pas d'ID, donc son **nom** est la seule cellule intouchable | suppression |
| `oci` registries | URL | `Logged` — exactement ce que l'opération va changer | login, logout |
| `netdiag` ports | **PID** | `State` | kill |

**Une seule table a gagné une colonne** : containers, la seule dont l'état vaut
une colonne à lui. Partout ailleurs le spinner prend une cellule existante —
c'est l'argument de §3.16 sur la case à cocher du clone, une colonne coûtant des
cellules sur tout l'écran pour ne rien dire sur toutes les lignes sauf une.

Deux clés méritent la note. **Ports est indexé sur le PID, pas sur la socket** :
toutes les lignes d'un processus tournent ensemble, ce qui est ce qui se passe —
le kill les prend toutes. **Registries est indexé sur l'URL**, seul endroit où
la règle de D40 ne s'applique pas, et elle ne s'applique pas parce que
l'*opération* est à portée d'hôte : un `docker login` change bien la réponse
pour toutes les entrées de cet hôte.

`oci_resources` ramasse `BusyLabels()` sur **les quatre onglets**, pas
seulement l'actif : une action lancée sur Images continue après `tab`, et un
spinner qui se serait arrêté parce que l'utilisateur a regardé ailleurs se
lirait comme un gel au retour.

**Deux messages ne portaient aucune identité.** `NetworkActionMsg` et
`VolumeActionMsg` n'avaient que `Action` et `Err` — survivable tant que la seule
chose qu'ils déclenchaient était un refetch de la liste, bloquant dès qu'il faut
lever un marqueur de la ligne où il a été posé. `ImageActionMsg`, lui, avait le
même décalage que celui des conteneurs mais **délibérément** : son champ `ID`
portait le nom pour que le footer n'affiche pas un hash, et un test le
documentait. Il porte les deux maintenant.

**Prune a sa propre ligne**, dans les quatre onglets : il n'agit sur aucune
ligne, donc marquer toutes les lignes dirait faux.

**`workspaces` est laissé en dehors, et c'est un choix** — les raisons, et ce
qui resterait à faire, sont en
[§3.23](#323-workspaces-et-la-notion-doccupé--deux-mécanismes-pour-une-question).

---

### 3.23 `workspaces` et la notion d'occupé — **étape 1 faite**

Sorti de §3.22 : les six autres tables passent par `datatable`, `workspaces`
garde le sien. Ce n'est pas un oubli — c'est la vue **d'où vient le design**,
et elle est aussi la seule où le remplacement n'est pas mécanique.

#### Ce qu'elle a déjà, et qui marche

| | |
|---|---|
| `scanningPaths`, `syncingPaths` | deux maps de chemins absolus, tenues séparées **exprès** : savoir laquelle détient le dépôt est ce qui permet à la vue de le dire |
| `busy(path)` | le garde-fou, avec un message unique (`busyMessage`) — la réponse de l'utilisateur est la même dans les deux cas : attendre |
| le spinner | dans la cellule Git Status pour un sync, Scanned pour un scan |
| `syncStatusLine` | la ligne de progression, rendue depuis l'état du run et non posée en `footerInfo`, dont le timer de 3 s expirerait au milieu d'un lot |

Autrement dit : §3.22 a généralisé ce que cette vue faisait déjà. La dette
n'est pas qu'il lui manque quelque chose, c'est qu'il y a **deux
implémentations de la même idée** dans l'application.

#### Le vrai trou, et il est petit — **corrigé**

**La suppression n'était pas couverte.** `handleConfirmDelete` appelait
`deleteEntry` et retournait sans rien marquer — le défaut exact de §3.22, sur
la seule action de cette vue que sa propre machinerie ne connaissait pas.
`deleteEntry` fait un `os.RemoveAll` récursif, ce qui n'est instantané que sur
un petit répertoire : un `node_modules` ou un dépôt de plusieurs Go prend des
secondes, et rien ne le disait. Il n'y avait pas non plus de garde-fou, donc un
second `D` lançait un second `os.RemoveAll` dont l'échec était rapporté à
l'utilisateur alors que la suppression avait réussi.

Réparé **sans rien migrer**, exactement comme annoncé : un troisième map
`deletingPaths` à côté des deux autres, `busy(path)` étendu, et la cellule
dépensée est `Git Status` — un répertoire en train de disparaître n'a plus de
statut git à annoncer, ce qui en fait la cellule la moins chère de la ligne.

Quatre points qui ne se lisent pas dans l'énoncé :

- **La garde est posée deux fois.** `startDelete` refuse avant d'ouvrir la
  modale — poser la question puis décliner la réponse est le seul ordre qui
  fasse perdre du temps. `handleConfirmDelete` redemande, parce qu'un sync de
  lot marque ses dépôts depuis un `Cmd` : un chemin peut être pris pendant que
  la modale est à l'écran, et c'est ce handler qui émet l'appel irréversible.
- **Le marqueur est levé à chaque issue, échec compris.** `deleteEntry`
  construit son message en **un seul site**
  (`EntryDeletedMsg{Path: path, Error: os.RemoveAll(path)}`) : l'oubli du
  chemin sur la branche d'échec — qui aurait bloqué la ligne pour la durée de
  vie de la vue — devient inexprimable plutôt qu'interdit par revue.
- **`busy()` est un seul prédicat**, donc `S` et `F` héritent de la garde sans
  une ligne de plus, dans les deux sens.
- **`anyBusy()` remplace les deux copies** de
  `len(scanningPaths) > 0 || len(syncingPaths) > 0` qui décidaient de la chaîne
  du spinner. Un troisième map aurait sinon dû être retenu à trois endroits.

Neuf tests, dont six ont été vérifiés en échec sur le code d'avant : la garde,
la seconde garde, l'héritage par `S` et `F`, le marquage, le spinner dans
`Git Status` et la levée du marqueur sur les deux issues. Le dixième test
envisagé — forcer `os.RemoveAll` à échouer — a été abandonné plutôt que rendu
fragile : `RemoveAll` sur un chemin absent renvoie `nil`, et les moyens de le
faire échouer ne sont pas les mêmes sous Windows et sous Linux. C'est le site
unique de construction qui porte l'invariant à sa place.

#### Pourquoi la migration complète n'est pas mécanique

`datatable.MarkBusy` répond à « un objet, une action ». Ici la notion est
autre :

- **Scan et sync s'excluent mutuellement par dépôt**, et la vue doit dire
  *lequel* des deux détient le chemin. Une map unique `key → label` porterait
  le libellé mais pas la distinction que `busy()` exploite.
- **Le sync vise un arbre, pas une ligne.** `s` sur un répertoire simple
  synchronise tous les dépôts imbriqués dessous : un appui marque N chemins qui
  ne sont pas tous des lignes visibles au même niveau de drill-down.
- **Le spinner ne va pas dans la même cellule** selon l'opération : `rowsFor`
  (`columns.go:133`) écrit `frame + " syncing"` dans Git Status pour un sync, et
  `formatScanColumns` décore les colonnes de scan pour un scan — alors que
  `StatusColumn` est unique par table.

Aucun de ces trois points n'est rédhibitoire, mais chacun demande une décision
plutôt qu'un remplacement, et les trois portent sur du code qui fonctionne. Le
risque de régression est réel et le gain visible est nul.

#### Découpage

1. ~~**La suppression d'abord**, seule, parce que c'est le seul défaut
   observable — et elle ne demande aucune décision.~~ **Faite.**
2. **Reste ouvert** : décider si `datatable` doit apprendre l'exclusion
   mutuelle et la colonne variable, ou si `workspaces` reste l'exception
   documentée. La deuxième réponse est légitime : une exception qui s'explique
   en trois lignes coûte moins qu'une abstraction qui porte un cas unique — et
   l'étape 1 vient de la rendre plus légitime encore, puisque le seul défaut
   observable qui plaidait pour la migration n'existe plus. Il ne reste que de
   la symétrie, et la symétrie ne se paie pas en risque de régression sur du
   code qui marche.

---

### 3.24 The dashboard says what to do about it — **done**

Cinq retouches sur les boîtes du dashboard, sans plan séparé. Elles ont un
point commun : chacune remplace un chiffre exact par la chose qui se décide.

**Une ligne vide en dernière position, dans toutes les boîtes.** Elle est
ajoutée à la hauteur de la **rangée** (`trailingBlank`, `innerHeights`), pas au
rendu de chaque section : `padTo` remplit ensuite, donc la boîte la plus haute
de la rangée en reçoit exactement une et les autres davantage. C'était elle qui
touchait sa bordure basse — et c'est celle que l'œil lit en premier.

**Les outils manquants sont nommés.** « 4 of 5 available » posait la question
qu'il ne répondait pas : lequel installer. La boîte Host tient sur une ligne
quand tout est là (`all available ✓`) et liste un nœud par manquant sinon. Les
manquants se lisent contre `knownTools`, pas contre ce que la détection a rendu
— un outil absent de la détection est absent tout court.

C'est **le seul bloc du dashboard dont la hauteur suit ses données**, et
`TestEverySectionKeepsItsHeightWhateverItsState` le sait : les trois états y
partagent désormais un même inventaire, ce qui laisse le test attraper tout ce
qui bouge réellement d'un rafraîchissement à l'autre. L'exception se paie une
fois, au démarrage, et se justifie ainsi : un outil installé ne se désinstalle
pas entre deux tours, là où un compte change à chaque fois.

**Images, volumes et réseaux pendent d'une racine `Resources`.** Trois lignes de
premier niveau se lisaient comme trois sujets, alors que ce sont des objets d'un
même daemon. **Networks entre au passage** : c'est une ressource que `:oci` gère
et que le dashboard ne comptait pas — la seule des trois que `docker system df`
ignore, faute d'octets à déclarer, d'où le `docker network ls` séparé dans
`FetchOCIStats`. Une liste qui échoue laisse le compte à zéro plutôt que
d'emporter les tailles avec elle.

**Le pourcentage d'occupation passe entre parenthèses** : `290 GB  58 %` se lit
comme deux faits côte à côte, `290 GB (58 %)` comme une mesure et sa part.

**La ligne « Updated » quitte le footer.** Elle datait des valeurs figées à `-`,
mais les trois horloges du dashboard tournent à la seconde, aux cinq secondes et
à la trentaine : le plus vieux fait à l'écran n'a jamais une minute, donc
`TimeAgo` répondait `now` en permanence. Une ligne dont la valeur ne change
jamais n'informe de rien, et celle-ci coûtait la seule ligne d'information de la
vue. `Model.lastRefresh` part avec elle — un champ que plus personne ne lit se
lit comme une donnée qu'on a oublié d'afficher.

---

### 3.25 A viewer for documents — text, JSON, XML and logs — **done**

Plan : [`document-viewer.md`](../.claude/plans/document-viewer.md).

Une vue `viewer`, ouverte par le routeur à la demande d'une autre vue, jamais par
son nom. Trois producteurs : `enter` sur un fichier dans `workspaces`, `i` et `l`
dans `containers`. C'est la forme qu'a déjà la vue security — une destination
ouverte depuis deux listes — et elle est câblée pareil.

**Ce que ça supprime est le vrai résultat.** Le panneau de logs de `containers`
— `viewState`, un `viewport`, le wrap, le strip ANSI, les touches de défilement,
le reload, le follow, la bascule timestamps et le pager externe — n'avait rien
de spécifique aux conteneurs sauf les trois derniers, et ces trois-là sont des
propriétés de **l'origine du texte**, pas du panneau. Le panneau part donc en
entier dans le viewer et `update.go` passe de 707 à 548 lignes. `wrapLines` et
la normalisation ANSI/CR sont **déplacés**, commentaires compris : leurs raisons
(systemd colore sa sortie, les barres de progression écrasent avec `\r`) valent
pour n'importe quel texte affiché, pas pour les seuls logs.

**Deux axes, pas quatre noms.** La demande parlait d'arbre, de raw, de plain text
et d'une bascule de coloration. Il n'y a que deux faits indépendants : l'affichage
(`f` : arbre ↔ texte) et la couleur (`c`). « Plain text », c'est le texte sans
couleur. Un troisième affichage aurait donné deux chemins vers un même écran —
la forme que §3.9 a retirée au backend de secrets et que la commande `:theme`
a emportée avec elle.

**Le document porte sa source, pas ses octets.** Recharger, suivre, re-fetcher
avec des timestamps sont des questions posées à l'origine. `viewer.Source` a
trois capacités optionnelles — `Timestamped`, `Followable`, `Pageable` — sondées
par assertion de type, comme le routeur sonde `FooterView`. Chacune n'a **qu'une
méthode** : l'avertissement de `HeaderView` porte sur une vue qui en fournit deux
sur quatre et ne satisfait rien en silence ; une interface à une méthode n'a pas
d'état à moitié satisfait. Seul `logsSource` les implémente toutes les trois, et
c'est exactement ce que le viewer affiche : `t`, `ctrl+f` et `e` n'apparaissent
que pour ce document-là (Rule 130).

**Une ligne sans niveau hérite de celle du dessus.** C'est la décision sur
laquelle repose tout le filtre : une stack trace, c'est douze lignes sans niveau,
et un filtre réglé sur « ≥ warn » qui les avalerait détruirait précisément ce
qu'on est venu lire. L'héritage se chaîne, et une ligne vide le coupe — sinon un
seul ERROR colorerait la moitié du fichier. Le prix est assumé : une ligne
réellement indépendante, sans niveau, suivant un INFO, est filtrée avec lui.
Une ligne qu'aucun niveau ne précède reste `LevelUnknown` et passe **tous** les
filtres.

**La verbosité est un minimum qui cycle** (`v` : all → trace → debug → info →
warn → error), pas quatre bascules indépendantes. C'est ce que veut dire
« verbosité », et les niveaux de log sont monotones : personne ne veut warn sans
error. Un seul token dans la `FilterBar` (Rule 136), qui disparaît à `all`.

**Le format d'un log est déclaré, jamais reniflé.** « Ça ressemble à un log »
n'est pas une question décidable ; le précédent est le champ `provider` des
registries — déclaré, jamais déduit de l'URL. JSON et XML gardent leur reniflage,
mais uniquement pour un fichier **sans extension** : un `.md` qui commence par
une balise n'est pas un XML cassé, et le dire serait du bruit sur un fichier qui
s'affiche très bien. D'où `detection.Declared` : une erreur de parsing n'est
signalée que si le **nom** l'avait annoncé.

**L'ordre est du contenu.** Les deux parseurs lisent un flux de tokens
(`json.Decoder.Token`, `xml.Decoder.Token`) et non une valeur décodée :
`map[string]any` perd l'ordre du fichier, et une configuration relue par ordre
alphabétique est un autre document. C'est aussi pourquoi les colonnes de l'arbre
ne déclarent **ni `Less` ni `Search`** — trier détruirait ce que le parseur a pris
soin de garder, et un filtre texte masquerait les parents en orphelinant leurs
enfants. `.` et `/` ne sont donc pas liés dans l'arbre, et Rule 138 le dit par
omission.

**L'arbre est un `datatable`, le texte un `viewport`.** L'arbre y a droit pour
une raison qui mérite d'être dite : **une cellule d'arbre ne porte qu'une seule
classe de syntaxe**, donc un `Style` par cellule suffit — `datatable` ne sait pas
exprimer plusieurs couleurs dans une cellule, et n'a jamais à le faire ici. Le
panneau texte, lui, n'est pas une table : Rule 122 ne s'y applique pas, mais
Rule 115 si, et chaque style de token pose son fond explicitement.

**Le wrap se fait sur des tokens, pas sur du texte coloré.** Une ligne déjà
habillée ne peut pas être coupée : la mesure compte les octets d'échappement
comme de la largeur, et la coupe tombe au milieu d'une séquence — le même piège
que Rule 122 décrit pour les cellules. `docLine` garde donc la ligne sous forme
de spans, `wrapTokens` la découpe pendant qu'elle est encore brute, et la couleur
est posée après.

**chroma sert de lexer et de rien d'autre.** Ses formatters écrivent leurs
propres séquences ANSI et leurs resets, et un reset au milieu d'une ligne emporte
le fond de l'application jusqu'à la marge (Rule 115). La correspondance
`TokenType → TokenClass` a été **relevée sur les deux lexers**, pas devinée : ils
émettent `NameTag` pour une clé JSON *et* pour une balise XML, d'où le `kind` en
paramètre. L'invariant sur lequel tout repose — concaténer les tokens redonne
l'entrée exactement — a son propre test.

Le coût est mesuré et consigné : **19,9 Mo → 24,0 Mo**, soit +4,0 Mo (+21 %),
parce que chroma embarque tous ses lexers. C'était le compromis accepté contre
~250 lignes de gestion d'échappements et de CDATA écrites à la main, et contre
l'absence de toute route vers Go, YAML ou Dockerfile.

**Les couleurs de syntaxe sont des alias sémantiques** posés dans `ApplyTheme`,
comme `ColorChartBg` : aucun des six fichiers de thème ne gagne une clé, et tous
récupèrent une palette cohérente. Les niveaux de log n'en reçoivent **aucune** —
`StatusErrorStyle`, `StatusWarningStyle` et `DimStyle` veulent déjà dire ça.

**`ViewViewer` n'est pas dans `viewNames`** : `:viewer` ouvrirait un écran qui dit
qu'il n'y a rien dedans, et `app.default_view` le proposerait comme vue
d'atterrissage — le défaut pour lequel `ViewNames()` avait été séparé de
`FullNames()`. `AllViewNames()` est né pour ça : les deux tests de contrat du
routeur l'itèrent, parce qu'une vue ouverte par le routeur s'affiche dans le même
viewport et échoue dans le même silence.

Trois collisions de touches ont été résolues plutôt qu'acceptées : le follow
passe de `f` à `ctrl+f` (`f` est la bascule d'affichage ; `ctrl+r` et `ctrl+f` se
lisent maintenant comme « recharger une fois » / « recharger en continu »),
`h` et `l` restent libres parce que Rule 111 en fait les alias de `←`/`→` — c'est
justement le drill-down de l'arbre, d'où `c` pour la coloration — et `q` ne ferme
plus rien : c'est la touche de sortie de l'application, et le panneau de logs
était le seul écran à l'avaler.

Les tests du panneau de logs ont **suivi le code** plutôt que d'être supprimés :
défilement, wrap, reflow au redimensionnement, reload, retour de pager. C'est ce
qui fait de ça un déplacement et non une réécriture.

Non retenu : un réglage `app.syntax_highlight`. Le viewer ouvre avec la couleur
et `c` la bascule pour la session ; rien n'est persisté, donc rien ne peut être
en désaccord. Vingt-neuf réglages suffisent (YAGNI).

---

### 3.26 Une touche, un sens — le clavier passe en majuscules — **done**

Relevé complet des 184 liaisons des 15 surfaces à `90f178e` : 16 collisions, où
la même touche ne veut pas dire la même chose selon la vue, et 6 risques de
portabilité. Les deux se soignent par la même décision, mais c'est la contrainte
du terminal qui la dicte — pas le goût.

#### Le budget réel, et il est petit

| Famille | Contrainte | Disponible |
|---|---|---|
| `Ctrl`+lettre | N'encode que l'ASCII 0x40–0x5F, et le tty en confisque quatre : **`ctrl+i` = TAB**, **`ctrl+m` = Entrée**, **`ctrl+j` = LF**, **`ctrl+h` = Backspace**. `ctrl+a`/`ctrl+b` sont les préfixes screen et tmux ; `ctrl+c`/`ctrl+d` sont SIGINT et EOF ; `ctrl+s`/`ctrl+q` sont le contrôle de flux. | ≈ 14 |
| `Alt`+touche | Option n'est pas Meta sur macOS tant que l'utilisateur ne l'active pas. L'application ne reçoit rien. | 0 |
| `Ctrl`+`Shift` | Le code de contrôle écrase la casse : `ctrl+a` et `ctrl+shift+a` émettent tous deux 0x01. Les distinguer exige le protocole clavier Kitty ou `modifyOtherKeys`, que **bubbletea v1.3.10 n'active pas** (c'est `WithKeyboardEnhancements()` en v2). Et même alors l'émulateur se sert d'abord : `ctrl+shift+c/v/t/w/n` sont copier, coller, onglet, fermer, fenêtre. | 0 |
| `Shift`+lettre | Aucune. Passe sur tout émulateur, toute plateforme, à travers SSH et tmux. | 26 |

`Shift`+lettre **est** une combinaison à deux touches : deux doigts, aucun départ
accidentel. C'est elle qui porte le vocabulaire d'actions — non par défaut, mais
parce que les deux autres familles sont amputées ou inutilisables.

#### `alt+:` ne marche pas sur macOS, et c'est la voie d'entrée principale

§3.7 a résolu l'entrée en mode commande depuis un champ focusé, et le
raisonnement de `keys.go:7-10` sur `ctrl+:` est juste : `:` vaut 0x3A, hors de la
plage que Ctrl encode. Mais le repli choisi hérite d'un autre défaut. Sur
Terminal.app et iTerm2, `Option+Shift+;` émet un caractère littéral ; la touche
n'atteint jamais l'application. C'est annoncé dans le `GetShortcuts()` de chaque
vue, et c'est la seule voie qui traverse un champ focusé.

**`ctrl+p` la remplace** : libre dans toute l'application, aucun caractère de
contrôle tty, aucun préfixe de multiplexeur, et le sens est déjà appris —
*palette*. Elle se place où est `alt+:`, **avant** le test `InEditMode()`.
`:` reste, inopérant en édition : ce n'est pas un idiome vim mais celui de la
ligne de commande, partagé avec less, ranger et k9s. `alt+:` est **supprimée**
sans dépréciation douce — un alias qui marche sur deux plateformes sur trois est
ce qui pourrit le plus vite.

#### Trois espaces de noms disjoints

C'est la forme qui rend la règle vérifiable par un test, et c'est le seul intérêt
de la formuler ainsi :

- une **majuscule** est une action, et son sens est global à l'application ;
- une **minuscule** est un filtre ou une bascule d'affichage, ne modifie rien,
  et peut donc se répéter d'une vue à l'autre ;
- le **reste** est structurel et ne change jamais.

Une quarantaine d'actions pour 26 lettres : la règle ne tient qu'après fusion des
synonymes (`K` = arrêter et tuer, `D` = supprimer et retirer, `T` = terminal et
shell) et parce que les bascules d'affichage sortent du compte. Elles occupent
21 lettres ; `H J Q Y Z` restent libres.

| Touche | Sens | Remplace |
|---|---|---|
| `N` | Créer une ressource depuis ce contexte | `ctrl+n` |
| `E` | Éditer la ressource sélectionnée | `e` |
| `D` | Supprimer la ressource sélectionnée | `ctrl+d` |
| `M` | Renommer (*mv*) | `r` |
| `S` | Scanner la cible sélectionnée | `ctrl+s` |
| `A` | Scanner tout (modale : case « purger le cache d'abord ») | `A` *et* `ctrl+a` |
| `F` | Se remettre au niveau de la source — fetch puis fast-forward, ou suivre un flux | `s` · `ctrl+f` |
| `C` | Entrer en sélection de clone | `c` |
| `T` | Ouvrir un terminal ou un shell | `t` · `s` |
| `O` | Ouvrir dans l'IDE configuré | `ctrl+o` |
| `W` | Ouvrir une URL dans le navigateur | `ctrl+w` · `o` |
| `L` | Ouvrir les logs | `l` |
| `V` | Ouvrir dans le pager système | `e` |
| `K` | Arrêter, tuer (modale : Stop / Restart, ou SIGKILL) | `K` · `r` · `ctrl+k` |
| `P` | Prune — supprimer les ressources inutilisées | `p` |
| `B` | Ouvrir le navigateur multi-registries | `b` |
| `G` | Pull (*get*) l'image ou le tag | `p` |
| `U` | Login / logout registry — bascule sur l'état de la ligne | `l` *et* `L` |
| `X` | Exclure — ajouter à `.gitleaksignore` | `i` |
| `R` | Ouvrir les merge requests · PR | `m` |
| `I` | Ouvrir les issues | `i` |

Les minuscules restantes ne modifient rien, donc leur sens est local et deux vues
peuvent employer la même lettre sans se contredire : `a` (containers, actifs
seuls), `f` `c` `w` `v` `t` (viewer), `t` `u` `l` `e` `n` `z` (netdiag/Ports),
`f` (netdiag/détail), `r` (browser, registry affiché), et `c` `h` `m` `l` pour
les sévérités de security.

#### Trois actions disparaissent sans perdre leur fonction

C'est ce qui fait tenir le budget, et chacune corrige un défaut au passage.

**`Restart` devient un bouton de la modale de `K`.** `stopSelectedContainer`
(`update.go:217`) et `restartSelectedContainer` (`update.go:226`) agissent
aujourd'hui sans confirmation, contrairement à `ctrl+d`. Avec Verr.Maj actif, un
`k` de défilement arrête le conteneur sélectionné — et `restart` est le pire des
deux, puisque c'est un stop+start qui coupe les connexions en cours. La
confirmation était nécessaire de toute façon ; elle rend la seconde action
gratuite.

**`Purger puis tout scanner` devient une case à cocher dans la modale de `A`**,
sur le motif que `DeleteConfirmModal` emploie déjà avec `permanentlyRemove`.
`A` et `ctrl+a` ne se distinguaient que par le modificateur, et rien dans leur
forme ne disait lequel purgeait : c'est la paire la plus proche d'une perte de
données involontaire de l'application. L'option destructrice devient un geste
délibéré, et `ctrl+a` cesse par la même occasion de heurter le préfixe de screen.

**Lancer un conteneur depuis une image prend `N`**, sans collision : c'est bien
« créer une ressource depuis la ligne sélectionnée », et rien d'autre ne se crée
depuis l'onglet Images. `ctrl+e` disparaît.

Et **`Inspect` passe sur `enter`** — non lié dans `containers`, et déjà le geste
d'inspection dans OCI/Networks. C'est ce qui libère `i` pour les issues.

#### Les alias vim partent en entier

`h` `j` `k` `l` `g` `G` disparaissent, y compris ceux que §3.25 vient
d'introduire dans le viewer (`h`/`l` pour plier-déplier un nœud, `j`/`k`/`g`/`G`
pour défiler le texte), ainsi que `b`/`f` en demi-page dans le détail security.
`home`/`end` couvrent déjà `g`/`G`.

Le gain se concentre sur `l`, qui portait quatre sens ; le reste ne libère rien.
Ce qu'on achète n'est donc pas la place mais une règle vérifiable — *aucune
lettre nue n'est de la navigation*. Garder `j`/`k` laisserait une exception, et
ce sont les exceptions qui ont produit l'état actuel. L'onglet Registries avait
d'ailleurs déjà tranché seul dans ce sens (`keys.go:174-175`, `l` y est Login).

Le coût est assumé : k9s, lazygit et btop gardent tous `hjkl`, et le public de
DevDesk est terminal-natif. Il se paie une fois.

**Une justification de §3.25 tombe avec eux.** `tui-layout.md:42-44` défend `c`
pour la coloration en expliquant qu'une touche *highlight* ne peut pas être `h`,
« puisque Rule 111 réserve `h`/`l` comme alias de `←`/`→` ». Ces alias
disparaissent, donc `h` est libre et l'argument ne tient plus. **`c` reste, la
raison est réécrite** : *coloration* est de toute façon un meilleur repère que
*highlight*, et déplacer une touche livrée le jour même pour courir après un
motif supprimé serait du bruit. Mais laisser la raison en l'état induirait en
erreur le prochain lecteur.

#### `pgup`/`pgdown` meurent dans trois vues, et la forme en est la cause

`datatable` les gère depuis toujours (`datatable.go:415`). Ce sont les vues qui
les interceptent et les jettent, parce qu'elles filtrent par liste blanche au
lieu de transmettre par défaut :

- `status/update.go:247` — la liste extérieure ne les cite pas, donc elles
  n'atteignent jamais `handleTableNavigation`, **qui les gère pourtant en
  ligne 284**. Le code est écrit, il est mort.
- `oci_resources/keys.go:107` (Networks) et `:138` (Volumes) — cases explicites
  pour `↑↓`, `g`, `G`, puis `return m, nil`.
- `oci_resources/connectivity_form.go:186` — même forme.

Les vues qui terminent par `return m, m.table.Update(msg)` — explorer,
containers, Registries — n'ont aucun de ces trous. **La correction n'est donc pas
d'allonger les listes blanches mais de les remplacer**, sinon la prochaine touche
que `datatable` gagnera mourra au même endroit et il faudra refaire ce relevé.

#### Ce qui disparaît encore

| | |
|---|---|
| `1` `2` `3` `4` | Saut direct aux onglets de security — `tab` suffit, et l'exception d'une seule vue est précisément ce qu'on démonte |
| `S` (shell), `T` (terminal) *nouvelle fenêtre* | La variante devient un réglage de la vue configuration : la capacité dépend de l'environnement — l'aide dit déjà qu'elle n'existe pas sous WSL, et à travers SSH il n'y a aucune fenêtre à ouvrir. Un réglage absent vaut mieux qu'une touche inerte |
| `backspace` (détail security) | Alias d'`esc` unique dans l'application |
| `r` (résultats netdiag) | « Nouveau diagnostic » est un retour arrière → `esc` |
| `ctrl+e` `ctrl+o` `ctrl+w` `ctrl+s` `ctrl+a` `ctrl+d` `ctrl+n` `ctrl+k` `ctrl+f` | Passent en majuscule. Ne survivent que `ctrl+r`, `ctrl+p` et `ctrl+c` |

`ctrl+r` garde un seul sens — **rafraîchir, jamais « revenir »** —, ce qui corrige
les résultats de security et de netdiag où `esc` suffit ; et `.` reste le tri,
ce qui déplace le filtre de sévérité de security.

**Ce filtre change de nature, pas seulement de touche.** Rule 136 a déjà le
composant qu'il lui faut, `NewFilterBarWithTokens`, employé par netdiag/Ports :
quatre bascules cumulatives `c` `h` `m` `l` valent mieux qu'un cycle, parce que
« CRITICAL **et** HIGH » est la question qu'on se pose réellement et qu'un cycle
ne sait pas la poser.

#### Confirmations

`components.ConfirmModal` existe, focus par défaut sur « No » — rien à
construire.

| Action | |
|---|---|
| `ctrl+k` — SIGKILL sur un processus de l'hôte (`ports_model.go:336`) | **À ajouter en priorité.** Ce n'est pas un conteneur qu'on relance, c'est le processus de quelqu'un, et rien ne confirme aujourd'hui |
| `K` — arrêter, `r` — redémarrer | À ajouter, absorbées dans la même modale |
| `space` — pause / reprise | **Aucune.** Réversible et instantané ; une modale vue dix fois par heure entraîne à taper `y` sans lire, ce qui est exactement ce qui rend sans valeur celle de `ctrl+d` |

#### Deux exceptions locales, déclarées

`c` (test de connectivité, inspection réseau OCI) et `ctrl+y` (copier la commande
`docker run`, formulaire de lancement) restent où ils sont : brûler deux
majuscules globales pour des actions présentes dans un seul sous-écran à faible
densité coûterait plus que ça ne rapporte. Elles doivent être **écrites comme
exceptions dans la règle**, sinon le prochain relevé les comptera comme des
dérives et quelqu'un les « corrigera ».

#### Ce qui reste à surveiller

`a` et `A` coexistent dans containers et OCI/Images — filtre « tous » d'un côté,
« scanner tout » de l'autre. Les deux espaces de noms sont disjoints par
construction, mais c'est la seule paire où la casse seule sépare une bascule
d'une action. Si elle gêne à l'usage, c'est le filtre qui bouge, pas l'action.

`F` est la plus lâche des 21 : elle mutualise le sync de workspaces et le follow
du viewer sous « se remettre au niveau de la source ». La généralisation est
juste au bon niveau d'abstraction, ou forcée — c'est la ligne à rejeter en
premier, et `H J Q Y Z` sont libres.

#### Ce que l'implémentation a ajouté

Le relevé portait sur 15 surfaces ; le **scan de source** qui vérifie la règle en
a trouvé quatre de plus, et c'est l'argument pour lui plutôt que pour une revue :

- **Les modales de confirmation lient `Y`/`N`.** Ce n'est pas une collision — une
  modale réclame toute touche avant que la vue ne la voie, exactement comme
  `InEditMode` — donc c'est un **quatrième espace de noms**, disjoint par le
  *mode* et non par la casse. Déclaré, pas effacé.
- **`bubbles/viewport` a son propre `KeyMap`**, qui répond `j/k/u/d/b/f`.
  L'overlay d'aide est le seul endroit qui lui passe une touche brute : il
  défilait donc sur des lettres dans le dos de l'application. `arrowOnlyScroll()`
  le remplace.
- **`netdiag/topology_model.go`** était absent du relevé.
- **`CreationForm.isOnTextField()`** n'existait que pour empêcher `j`/`k` de
  naviguer pendant la frappe. Une touche de navigation qui doit demander « es-tu
  en train de taper ? » est une touche qui ne devrait pas être une lettre ; elle
  est morte avec les alias.

Trois composants ont bougé plutôt que d'être dupliqués :

- **`DeleteConfirmModal` → `OptionConfirmModal`.** Il servait la seule
  suppression, d'où son nom ; il porte maintenant deux questions, et
  `PermanentlyRemove` aurait été un mensonge pour la purge. Le libellé de la case
  est un paramètre, le warning aussi — une option non destructrice n'en mérite
  pas.
- **`ChoiceModal`** est neuf, pour `K`. `ConfirmModal` pose une question fermée
  et `OptionConfirmModal` une question fermée avec variante ; ni l'une ni l'autre
  ne sait proposer **deux actions distinctes**. Une case à cocher aurait fait
  d'un choix exclusif une bascule, ce que Rule 132 interdit.
- **Les jetons de sévérité sont ceux de `datatable`**, qui portait déjà `Tokens`
  et `TokenMatch` : rien à construire. La table de findings a donc gagné une
  `FilterBar`, et le footer de security résout **une** barre
  (`activeFilterBar()`) plutôt que trois — sans quoi `GetFooterHeight` et
  `RenderFooter` peuvent se contredire d'une ligne.

**`U` décide sa direction depuis la ligne**, pas depuis la touche : `l` et `L`
faisaient répondre à l'utilisateur une question que la colonne Logged affichait
déjà, et se tromper de touche faisait silencieusement l'inverse.

**Livré en deux PR** : le vocabulaire, `ctrl+p` et les alias vim (#70), puis le
tableau des 21, les modales, le filtre de sévérité et le réglage (#71).

#### Non retenu

**Un remappage configurable.** Ce serait la réponse évidente à « chacun ses
touches », et c'est le contraire du problème : la difficulté n'est pas que les
touches déplaisent, c'est qu'elles ne veulent pas dire la même chose d'une vue à
l'autre. Un fichier de bindings rendrait l'incohérence configurable au lieu de la
supprimer, et `GetShortcuts()` devrait alors rendre des touches qu'aucune règle
ne garantit.

**Garder `j`/`k` seuls.** Ils ne collisionnent avec rien et la navigation vim
plairait au public visé. Mais une règle avec une exception n'est plus vérifiable
par un test, et c'est tout ce qu'on achète ici.

**Un préfixe façon vim (`g` puis `t`).** Il rendrait le budget de touches
illimité, au prix d'un état clavier que rien dans l'application n'a aujourd'hui —
et il réintroduirait par la porte de service l'idiome qu'on vient de retirer.

---

### 3.27 `containers` — la colonne Ports dit ce qu'elle montre, les filtres se voient — **done**

Deux demandes sur la même vue, et elles ont le même fond : **la vue affiche ce
que Docker a imprimé, pas ce que l'utilisateur est venu lire.** La colonne Ports
recopie une chaîne du CLI, et le seul filtre à portée de main ne laisse aucune
trace à l'écran.

#### La colonne recopie `docker ps` — **done**

`containers.go:40` demande `{{.Ports}}` dans le `--format`, `:66` range la chaîne
telle quelle, et `model.go:207-210` la rend sans y toucher : `Cell` retourne
`c.Ports`, sans `Less` ni `Search`. La colonne n'est donc pas une colonne, c'est
un passe-plat.

Ce que Docker imprime pour un seul `-p 80:80` :

```
0.0.0.0:80->80/tcp, :::80->80/tcp
```

Trente-trois caractères pour **un** port publié, dans une colonne à
`MinWidth: 16`. Trois raisons cumulées :

- **Docker écrit deux entrées pour une publication dual-stack.** L'IPv4 et
  l'IPv6 sont la même publication ; les afficher deux fois double la largeur
  sans ajouter un fait.
- **Le port du conteneur est répété alors qu'il n'est presque jamais la
  question.** Celui qu'on cherche est le port de l'hôte — c'est celui vers
  lequel pointer un navigateur ou un `curl`.
- **`0.0.0.0` et `127.0.0.1` sont écrits en toutes lettres** alors qu'ils ne
  portent qu'un bit d'information : exposé à tout le monde, ou seulement à la
  machine. C'est précisément la distinction qui compte pour la sécurité, et elle
  est noyée dans le bruit.

Un quatrième cas passe inaperçu : `docker ps` imprime aussi `80/tcp` tout court
pour un port simplement `EXPOSE`, jamais publié. Ce n'est pas une association,
et l'afficher dans la même colonne que les autres laisse croire qu'on peut s'y
connecter.

#### Ce que devient la colonne

Le port de l'hôte, et trois icônes Nerd Font pour le reste — la portée du bind,
la famille, le protocole. La forme visée, condensée sur une ligne :

```
󰋜 8080  󰛳 443
```

Trois conséquences, chacune une décision :

**Le parsing descend dans `internal/docker`.** `Container.Ports string` devient
une liste structurée, parsée là où la sortie du CLI est déjà lue. Le précédent
est `parseSSOutput` (`ports.go:77`), qui a fait exactement ce chemin pour la
table des ports de netdiag : la vue ne doit pas apprendre à lire du Docker.
C'est aussi ce qui rend la colonne **cherchable** — filtrer par numéro de port
est le besoin évident, et il est impossible tant que la cellule est une chaîne
opaque.

**La fusion dual-stack est le gain gratuit.** `0.0.0.0:80` et `:::80` sont une
publication ; les réunir en une entrée portant les deux marqueurs de famille
supprime la moitié de la largeur sans perdre quoi que ce soit. À faire avant
même le reste — c'est le seul point où on ne renonce à rien.

**Masquer le port du conteneur est une perte, et elle est assumée.** `8080->80`
et `8080->8080` deviennent identiques à l'écran. C'est le bon compromis pour la
lecture courante, mais il y a un cas où l'information manque : diagnostiquer un
reverse proxy qui tape le mauvais port interne. Elle reste atteignable par
`enter`, qui ouvre l'inspection JSON dans le viewer (§3.25) — ce qui n'est vrai
que depuis §3.25, et vaut d'être écrit ici plutôt que redécouvert.

Le rendu final, une entrée par publication :

| | Portée | Icône |
|---|---|---|
| `󰛳 80` | toutes les interfaces | `IconNetwork` |
| `󰋜 5432` | cette machine seulement | `IconHome` |
| `󰒋 8080` | une adresse nommée | `IconServer` |
| `󰌾 6379` | déclaré par l'image, publié par personne | `IconLock` |

Aucune icône nouvelle : les quatre existaient déjà dans `theme/icons.go`.

**Deux choix laissés ouverts ci-dessus, tranchés à l'implémentation, et l'un des
deux contre le plan.**

**La portée passe par le glyphe, pas par la couleur.** Le plan voulait
« la couleur porte *toutes interfaces*, pas un troisième glyphe ». C'est
impossible : `Style` colore une **cellule**, et une cellule porte plusieurs
publications qui ne partagent pas leur portée — styler dans `Cell` étant
précisément ce que Rule 122 interdit. Et ce serait de toute façon le mauvais
outil : publier sur toutes les interfaces est ce que `-p 80:80` fait par défaut,
donc c'est l'état majoritaire, et le colorer mettrait une couleur sur presque
toutes les cellules non vides pour un signal sur aucune. La colonne n'a donc
**aucune couleur**, ce qui est la discipline de Rule 122 appliquée telle quelle.

**Le protocole n'est nommé que s'il n'est pas tcp**, comme le plan le
recommandait. **La famille v4/v6 n'est jamais nommée**, ce que le plan
n'envisageait pas — mais le raisonnement qui écarte un marqueur `tcp` écarte un
marqueur dual-stack aussi fort : après la fusion, presque toute publication est
soit les deux familles, soit la seule que l'hôte a. Et rien ne se décide dessus :
on se connecte à un port d'hôte par son nom, et le résolveur choisit la famille.
Elle reste sur `PortBinding.V4`/`V6` pour qui en aurait besoin ; elle ne monte
pas à l'écran.

`isPort` est le garde-fou qui manquait au plan : sans lui, toute entrée sans
`->` se lit comme un port `EXPOSE` **portant son propre texte**, et une sortie
docker inattendue atterrit dans la colonne en ayant l'air d'un port joignable.

#### Les filtres, comme dans netdiag/Ports — **done**

`containers` avait un filtre texte (`/`, la `FilterBar` de `datatable`) et **un
filtre invisible** : `a` basculait `showAll` et relançait `docker ps --all`. Rien
à l'écran ne disait que la liste était restreinte — sauf un mot dans le header,
`4 (Active)`, c'est-à-dire un filtre affiché à l'endroit précis où Rule 136 dit
qu'un filtre ne va pas, et dans un vocabulaire que les jetons de la barre ne
partagent pas.

C'est le mécanisme de netdiag/Ports, repris tel quel : des jetons déclarés sur le
`datatable`, visibles dans la `FilterBar` quand ils sont actifs, cumulatifs, et
`z` qui les remet tous à zéro **et** vide la recherche.

**Le point qui n'était pas mécanique a été tranché dans l'autre sens.** La note
ci-dessus proposait qu'un jeton puisse déclencher un re-fetch, sur le précédent
du jeton `numeric` de Ports. C'est ce qu'il ne fallait pas faire : `numeric`
change ce que `ss` *imprime*, donc seul le démon peut répondre, alors qu'un état
de conteneur est déjà dans la ligne. `fetchContainers` charge donc **toujours**
tout (`docker ps -a`) et le filtre est local — un état revient dans la frame où
il est pressé, sans aller-retour. Un filtre ne peut de toute façon que restreindre
ce que la table détient : garder la portée côté démon aurait voulu dire que les
lignes masquées n'existaient pas non plus localement, et donc qu'aucun jeton ne
pouvait les ramener sans recharger.

**Quels jetons** : quatre, `running` `paused` `stopped` `transient`, et pas un
par état Docker. La contrainte est que la table soit **totale** — un état que
nul jeton ne nomme serait un conteneur masqué sans que rien à l'écran dise quel
filtre le masque. `stateToken` a donc une branche par défaut : `exited` et `dead`
y tombent aujourd'hui, et tout ce que Docker se mettra à rapporter demain. Ce
sont exactement les quatre groupes que `containerStateStyle` colore déjà, donc la
barre et la colonne d'état disent la même chose.

Pas de jeton `all` : « tout », c'est les quatre jetons ensemble, ce qu'ils sont
littéralement. Un cinquième jeton à côté de quatre vrais états serait une valeur
d'un autre ordre dans la même liste.

**La sélection vide est le défaut de la vue, pas « tout ».** C'est là que ça
diverge de netdiag/Ports, et le premier jet s'était trompé : il gardait la règle
de Ports — rien d'actif montre tout — et exprimait le défaut par un jeton
`running` **actif au départ**. La barre était donc à l'écran dès la première
frame, et `z` menait à un écran sur lequel la vue ne s'ouvre jamais. Deux
états pour une même intention, et le plus courant des deux coûtait deux lignes de
viewport en permanence.

`matchContainerTokens` lit donc la sélection vide comme `defaultStateToken`. Ce
que ça achète :

- la vue s'ouvre **sans barre**, sur les conteneurs qui tournent ;
- `z` y ramène, donc arriver et appuyer sur `z` sont un seul écran ;
- la barre apparaît **au moment où l'utilisateur a un avis**, et nomme alors
  exactement les états montrés.

Le coût est assumé et écrit : la portée d'ouverture n'est inscrite nulle part à
l'écran — précisément ce qu'on reprochait à `a`. La différence est que `a` ne
disait jamais rien, alors qu'ici le premier appui sur une touche d'état met la
barre debout.

Deux autres conséquences écrites plutôt que découvertes :

- `GetHeaderInfo` ne rend plus que le compte. Le label de portée est parti là où
  Rule 136 le veut.
- Le message de table vide ne peut plus se conditionner à
  `FilterBar().IsVisible()` : la barre cachée ne veut plus dire « liste non
  filtrée », c'est exactement l'état par défaut. `emptyLabel` tranche donc sur ce
  que la table **détient** — des lignes qu'on ne voit pas veulent dire que le
  filtre les cache. C'est justement sur une machine dont tous les conteneurs sont
  arrêtés que la différence compte, et c'est le seul écran où la portée
  d'ouverture se dit à l'écran.

`t` reprend la lettre que l'ancien volet de logs employait pour les horodatages.
Le sens d'une minuscule est local par construction (`keymap.localToggles`), donc
la lettre revient sans son ancien sens ; `TestTheLogsPaneShortcutsAreGone` le
vérifie sur ce que la touche *dit*, pas sur le fait qu'elle soit liée.

Les deux moitiés de §3.27 sont livrées.

#### Le troisième client de la même mécanique

§3.26 déplace déjà le filtre de sévérité de `security` vers des jetons de
`FilterBar`, pour la même raison : un cycle ne sait pas exprimer
« CRITICAL **et** HIGH ». Avec `containers`, trois vues convergent sur le même
composant. Si une quatrième suit, c'est le signe que `FilterBar` doit devenir la
seule façon de filtrer une table — et que Rule 136 doit le dire à l'impératif
plutôt qu'en exemple.

#### Non retenu

**Une colonne par facette** (Host, Port, Proto, Scope). Elle rendrait tout triable
et cherchable sans rien inventer, mais `containers` a déjà dix colonnes et déborde
à 80 colonnes : quatre de plus pour un fait que la plupart des lignes ne portent
pas est l'inverse du problème posé. C'est le même arbitrage que §3.16 pour la case
à cocher du clone et §3.22 pour le spinner — une colonne coûte des cellules sur
tous les écrans pour ne rien dire sur presque toutes les lignes.

**Garder la chaîne brute en `Search` pendant qu'on affiche la version condensée.**
Séduisant — on chercherait ce que Docker a écrit tout en lisant autre chose — et
c'est exactement le défaut que Rule 122 décrit une couche plus haut : ce qui est
mesuré, affiché et cherché doit être la même valeur, sinon un filtre trouve une
ligne que l'utilisateur ne voit pas correspondre.

---

### 3.28 Un état juste et invisible — la garde qui manque aux tests

Deux bugs livrés le même jour, dans deux vues, avec la même forme : **l'état
était correct et l'écran ne le montrait pas.**

| | L'état, juste | Ce qui manquait |
|---|---|---|
| `containers` (#72) | la modale de `K` existait et prenait le clavier | `View`, `InEditMode`, `GetShortcuts` l'ignoraient — un `K` et la vue était morte |
| `security` (#73) | la ligne était `Scanning`, la frame avançait | la cellule portait sa couleur, donc elle était tronquée dans sa séquence et ne rendait rien |

Aucun des deux n'est un défaut de logique. Les deux sont des défauts de
**restitution**, et c'est la catégorie que la suite de tests ne couvre pas.

#### Pourquoi les tests ne les ont pas vus

Ils vérifient qu'une touche **fait** quelque chose — un `Cmd` est retourné, un
champ change — jamais que l'écran **le dit**. Deux angles morts précis :

- **Le profil de couleur.** `lipgloss` n'émet aucune séquence hors TTY, donc une
  cellule stylée est indiscernable d'une cellule brute. `withTrueColor` existe
  pourtant déjà dans cinq paquets ; il n'était simplement jamais posé sur les
  chemins « action en cours ».
- **Les états transitoires.** Un scan qui tourne, une modale ouverte, une ligne
  occupée : ce sont exactement les états que personne ne rend, parce qu'ils sont
  pénibles à atteindre et qu'on croit tenir la vérité en lisant le modèle.

`ansiPrefix` et `withTrueColor` sont dupliqués dans `containers` et `security`,
ce qui est le signal habituel : le besoin est commun, l'outil ne l'est pas.

#### Ce que la garde doit vérifier

Trois propriétés, et chacune a déjà échoué une fois :

1. **Aucune cellule ne porte de séquence d'échappement.** Rule 122 par
   construction plutôt que par revue — c'est la propriété qui se teste le plus
   mécaniquement, puisqu'elle se lit sur `Table().Rows()`.
2. **Une modale ouverte est visible et déclarée.** Elle prend le clavier en
   priorité 1, donc une modale qui ne rend rien ne fait pas que déplaire : elle
   tue la vue jusqu'à `esc`, sans que rien ne le dise.
3. **Un spinner avance.** Une frame figée se lit comme un scan planté, et le
   test qui vérifie qu'elle s'affiche ne dit rien de son mouvement.

#### Où ça vit, et c'est l'arbitrage à trancher

Le premier point est mécanique et pourrait s'écrire une fois pour les **16
tables** de l'application — `datatable` est le seul endroit qui les connaisse
toutes. Mais `datatable` ne sait pas amener une vue dans un état intéressant :
une garde qui ne s'exécute que sur une table vide ne vérifie rien.

Donc probablement un **helper exporté** que chaque vue appelle après avoir atteint
ses états — `datatable.AssertPlainCells(t, m.Table().Rows())` — plus la remontée
de `withTrueColor` dans `internal/ui/testutil`, où il aurait dû naître. Le coût
est qu'il faut se rappeler de l'appeler ; le bénéfice est qu'il porte le message
d'erreur qui explique *pourquoi*, ce qu'un test local réécrit à chaque fois.

L'alternative — un test de contrat qui instancie chaque vue et la pilote — est
séduisante et probablement hors de portée : les états transitoires s'atteignent
par des chemins différents dans chaque vue, ce qui est précisément ce qui les
rend difficiles à couvrir.

#### Ce qui est déjà bon, et qu'il s'agit de ne pas laisser dériver

Rien ne viole Rule 122 aujourd'hui : `datatable` stampe `spinner.Dot.Frames`,
`oci_resources` et `workspaces` aussi, et aucun `Cell` n'appelle `Render`. **Cette
entrée est une garde contre la récidive, pas un correctif** — et elle mérite
d'être écrite parce que la récidive a eu lieu deux fois en une journée, sur du
code qui venait d'être relu.

#### Non retenu

**Interdire la couleur dans les vues et tout décider dans `datatable`.** Ce serait
imperméable, et c'est le contraire de ce que `Style` existe pour faire : une
couleur dépend souvent de la ligne, et la table ne connaît pas le domaine.

**Se fier à la revue.** C'est ce qui a été fait, sur les deux. Le commentaire
d'`oci_resources` disant « the spinner's frame, not its `View()` » était écrit,
juste, et à trois fichiers de l'endroit où il fallait le lire.

---

### 3.29 Le viewer colore YAML et TOML, et une recherche montre où elle a trouvé — **done**

Deux ajouts sans lien entre eux au-delà de la vue qui les porte.

#### YAML et TOML : quatre lignes de mapping, et une décision

`.yaml`, `.yml`, `.toml` entrent dans `extensionKinds`, `lexerName` gagne deux
cas. Aucune dépendance et aucun octet de plus sur le binaire : chroma embarque
tous ses lexers, c'est ce que les +4.0 MB de §3.25 ont acheté.

Le mapping a été **relevé sur les lexers**, pas supposé — la méthode de §3.25 —
et c'est ce qui a évité de livrer la moitié du travail :

| | Ce que chroma émet | Ce qu'il fallait faire |
|---|---|---|
| **YAML** | clés en `NameTag`, scalaires en `Literal`, `true`/`null` en `KeywordConstant` | **rien** — tout tombait déjà juste |
| **TOML** | **toutes** ses clés, en-têtes de table `[app]` comprises, en `NameOther` | une ligne : `NameOther → ClassKey` |

Sans cette ligne, un `.toml` sortait colorié partout **sauf ses clés**, c'est-à-dire
sauf ce qui mérite la couleur. Le mapping n'est pas gardé par le `kind` : ni le
lexer JSON ni le lexer XML n'émet `NameOther`, et les deux tests de
classification existants sont ce qui maintient l'affirmation.

**Aucun des deux n'a d'arbre, et c'est une décision.** `Structured()` les exclut :
les deux ont une structure, mais aucun parseur *ici* ne préserve l'ordre du
fichier, et l'ordre est du contenu (§3.25). `yaml.v3` saurait le faire —
`yaml.Node` garde l'ordre et les commentaires, et c'est déjà une dépendance —
tandis que TOML en coûterait une autre. Tant que ça ne vaut pas la peine, ce sont
des textes colorés et `f` reste masqué (Rule 130). L'absence est écrite dans le
commentaire de `Structured()` pour qu'elle ne se relise pas comme un oubli.

**Ni l'un ni l'autre n'est deviné du contenu.** Un fichier qui commence par `---`
est de l'entête Markdown aussi souvent qu'un flux YAML, et une ligne `[section]`
est de la prose dans la moitié des fichiers qui en portent une. C'est l'argument
déjà écrit pour `KindLog`, et `TestYAMLAndTOMLAreNeverInferredFromContent` en fait
une garde.

#### La recherche : une seule règle décide « ça matche » et « ici »

`/` filtrait déjà — les lignes sans occurrence disparaissent — mais sur une ligne
de 300 caractères conservée, rien ne disait **où**. Elle surligne maintenant, et
elle continue de filtrer : ajouter la surbrillance, pas la remplacer.

Le point de conception est ailleurs. Le filtre était
`strings.Contains(strings.ToLower(line.Plain), query)` ; si la surbrillance avait
calculé ses positions de son côté, on aurait eu **deux calculs pour une question**
— exactement ce que `scan.Categorize` (§3.12) et `Result.SecretVerdict` (§3.20)
ont chacun dû défaire, avec deux fois le même symptôme : un élément compté d'un
côté, introuvable de l'autre. Ici : une ligne conservée par le filtre sans une
seule occurrence visible, et rien pour dire pourquoi.

Donc `MatchRanges` est la seule chose qui décide qu'une ligne matche, et elle dit
où dans la même réponse : le filtre **est** `len(ranges) > 0`. « Une ligne
conservée porte au moins une occurrence surlignée » devient vrai par construction,
et `TestEveryLineTheSearchKeptCarriesAnOccurrence` l'oppose au code.

Ce qui en découle, chaque point avec son test :

- **Une occurrence est un span de plus, jamais une couleur posée sur une ligne
  finie.** `MarkMatches` coupe les tokens tant qu'ils sont bruts, pour la raison
  même qui fait exister `docLine` : une ligne stylée ne peut pas être coupée, la
  mesure comptant les octets d'un échappement comme de la largeur (Rule 122, un
  étage plus haut). Le marquage passe **après** le filtre, donc seulement sur les
  lignes qui vont être dessinées, et il conserve l'invariant octet-pour-octet de
  `Tokenize`.
- **`matchStyle` gagne sur la classe et sur le niveau.** Une ligne de log sort en
  niveau, occurrence, niveau : le niveau porte toujours le reste, donc un ERROR se
  repère encore d'un coup d'œil, et une recherche invisible dans un log manquerait
  précisément là où les lignes sont les plus longues. Le commentaire de
  `renderSegment` a été réécrit, parce qu'il affirmait le contraire.
- **`c` n'a pas voix au chapitre.** Une occurrence n'est pas de la coloration
  syntaxique ; éteindre les couleurs est la façon de lire un document en texte
  brut, et une recherche qu'on n'y verrait plus serait la seule chose que ça
  coûterait.
- **Couper un token le copie** (`withText`, `appendSpan`) au lieu d'en
  reconstruire un littéral. Un littéral doit nommer chaque champ pour le garder,
  et c'est ainsi qu'une surbrillance marche jusqu'à ce que la ligne soit assez
  longue pour être wrappée — le seul cas pour lequel elle existe. Vérifié en
  cassant la propagation : `TestSearchHighlightSurvivesWrap` échoue.
- **Décalages en octets, pas en runes** : un token porte une `string`. Et quand
  `strings.ToLower` change la longueur en octets (`İ`), la recherche retombe sur
  une recherche sensible à la casse plutôt que de surligner **à côté** du résultat.

`ColorSearchMatch`/`Fg` sont des alias sémantiques assignés dans `ApplyTheme`,
comme les couleurs syntaxiques : aucun thème ne gagne de clé (Rule 119).

#### Non retenu

**Surligner sans filtrer.** C'est une autre fonctionnalité : `matchedLines`,
`emptyTextMessage()` et le token du `FilterBar` perdent leur objet, et sur un log
de 50 000 lignes une recherche ne servirait plus à rien sans un `n` pour sauter
d'occurrence en occurrence — qui n'existe pas, aucune lettre nue n'étant de la
navigation (§3.26).

**`n` / `N` pour circuler entre les occurrences.** Même raison. La surbrillance
répond à « où est-ce », le scroll existe pour y aller.

**Un arbre YAML via `yaml.Node`.** Faisable, et hors sujet : la demande portait
sur la coloration, et un arbre pour YAML sans arbre pour TOML ferait de `f` une
touche qui marche sur un des deux formats ajoutés. À faire ensemble ou pas du
tout.

**Deviner YAML sur `---`.** Voir plus haut : c'est de l'entête Markdown aussi
souvent qu'un flux YAML.

---

### 3.30 Un message, trois niveaux, un composant — **done**

Le footer était écrit huit fois. Chaque vue portait sa paire de champs
(`footerError`/`footerInfo`, ou `errorMsg`/`infoMsg`, ou `statusMessage`), sa
minuterie locale (`clearFooterCmd`, `clearFooterMsgCmd`, `clearInfoMsgCmd`,
`portsClearFooterCmd`) et son bloc lipgloss — dont trois copies du même helper
sous trois noms (`centeredInfo`, `renderInfoText`, `highlightLine`).

**Le défaut que ça produit est structurel, pas cosmétique.** Personne n'a jamais
centré la branche d'erreur, dans aucune des huit : les erreurs étaient donc
alignées à gauche partout et les notices centrées, et la moitié des vues
mélangeaient les deux dans le même `RenderFooter`. Le vert du `statusMessage` de
`security` était le seul de son espèce, aligné à gauche lui aussi, et redoublé
dans le viewport sous le panneau de warnings.

**Il n'existait aucun niveau *warning*.** Tout était erreur ou info, et une
bonne moitié des « infos » étaient des refus : `Scan already in progress`,
`Not supported in Trivy client-server mode`, `Nothing selected`, `All images are
already scanned`. Elles s'affichaient dans le jaune des notices, à un cran de
rien.

`components.FooterMessage` porte le texte, le niveau et la minuterie. Trois
niveaux définis par **ce qui s'est passé** — `Error` : une opération a échoué ou
le système l'a refusée ; `Warn` : l'action ne peut pas être honorée telle que
demandée, mais rien n'a échoué ; `Info` : un fait neutre ou une réussite.

Les couleurs sont des alias sémantiques assignés dans `ApplyTheme`, comme celles
de la syntaxe du viewer : aucun fichier de thème ne gagne de clé. Elles visent
les noms **severity** et non `ColorError`/`ColorWarn`, que le thème par défaut
rend identiques — le choix est invisible aujourd'hui et cesse de l'être dans un
thème qui les sépare. `ColorFooterInfo` est `ColorText` : `ColorHighlight` est un
jaune à un cran de l'orange du warning, donc les deux niveaux étaient
indiscernables.

**L'expiration est identifiée** (`ClearFooterMsg{ID}`). C'est ce qui rend le
type partageable entre paquets, et ça corrige au passage un défaut que les huit
implémentations avaient toutes sans exception : un message posé à t+2,9 s était
effacé à t+3 s par la minuterie du précédent.

**`Status` est le second argument de `View`, et il n'a pas de minuterie.** Une
progression, un hint, un chargement sont des états dérivés à chaque frame, pas
des événements — un lot de syncs survit aux trois secondes qu'un message obtient.
C'est un paramètre plutôt qu'un champ parce qu'il est dérivé : la ligne d'action
de `containers` vient de `BusyLabels()`, qui change sans événement pour la
pousser. Précédence : **erreur → warning → info → status**.

**Le chargement d'une table passe au footer, avec un spinner, et la table reste
à l'écran.** Sept corps s'y substituaient un spinner : chacun perdait son en-tête
et ses colonnes le temps de chaque `ctrl+r` puis les retrouvait — un saut de mise
en page à chaque rafraîchissement. Conséquence obligatoire, et c'était déjà faux
sur deux onglets : le message vide est conditionné à la fin du chargement, sinon
la table annonce l'absence de ce qu'elle cherche.

Deux détails qui ont failli passer :

- La frame est le `spinner.View()` **rendu**, pas une frame brute : chaque vue
  style déjà son spinner avec `theme.SpinnerStyle()`, et le restyler
  imbriquerait une séquence dans une autre. La mesure passe par
  `lipgloss.Width` — l'inverse de la règle d'une cellule (Rule 122), et la
  différence tient à qui mesure.
- `Paused — press space to resume` dans netdiag/Ports était posé sans minuterie
  et resterait donc affiché trois secondes puis disparaîtrait alors que l'onglet
  est toujours en pause. C'est un `Status`, pas un message.

Deux tests source-level tiennent la ligne, sur le modèle de ceux de
`internal/ui/keymap` : `TestNoViewStylesItsOwnFooterMessage` refuse un
`StatusErrorStyle`/`StatusOKStyle`/`StatusWarningStyle`/`ColorHighlight` dans un
`RenderFooter` ou un `renderInfoLine`, et `TestNoTableViewRendersALoadingBody`
refuse un `theme.SpinnerMessage` dans une vue à table. Le seul écran qui se
remplit légitimement d'un spinner — le browser de registries pendant un pull, qui
n'a pas de table derrière — est une **exception déclarée**, comme
`keymap.DeclaredExceptions()`.

**La minuterie dort pour de vrai, et la suite le payait.** `tea.Tick` bloque sa
durée entière et `testutil.Msgs` exécute tout le lot : quatre tests inspectaient
un `Cmd` portant la minuterie et payaient trois secondes chacun — c'était déjà
vrai des huit minuteries locales, personne ne l'avait relevé. Un test qui veut
voir un message expirer construit maintenant `ClearFooterMsg{ID: …}` ; les quatre
qui doivent drainer le `Cmd` appellent
`testutil.FastTimers(t, &components.FooterMsgDuration)`. La suite complète est
passée de plus d'une minute d'attente pure à cinq secondes.

Le plan est dans
[`.claude/plans/footer-messages-unification.md`](../.claude/plans/footer-messages-unification.md).

### 3.31 `ws` montre les fichiers cachés, si on le lui demande — **done**

La vue `workspaces` sautait toute entrée commençant par un point, sans que rien
ne le dise et sans moyen de revenir dessus. C'est un défaut le jour où le dépôt
qu'on cherche s'appelle `.dotfiles`.

`app.show_hidden_files` décide, et `false` reste le défaut : c'est ce que la vue
a toujours fait, donc aucun fichier existant ne change de sens et il n'y a rien à
migrer. Le réglage se règle dans la vue `configuration`, onglet `app`, sous
`Workspaces dir` — il qualifie la racine que ce champ déclare.

**Pas de touche.** Une minuscule serait légale (Rule 111 : une bascule
d'affichage locale), mais elle ferait deux écrivains pour un réglage — ce que
§3.9 a démonté en supprimant le picker de thème, qui écrivait `app.theme` dans le
dos du formulaire. Un réglage, un endroit. La propagation est gratuite :
`handleConfigSaved` jette toutes les vues sauf `configuration`, donc `ws` se
reconstruit contre le config sauvé.

**Une seule règle, deux lecteurs.** Il y avait deux filtres indépendants — le
listing (`table.go`) et la marche à la recherche des dépôts imbriqués
(`walkSubRepos`, qui alimente les cibles de `S`, `F` et `A`). `isHidden(name,
showHidden)` est consulté par les deux : ce que la vue montre est ce qu'elle
scanne et synchronise, et deux règles pour cette question laisseraient un dépôt
être une ligne visible et une cible invisible en même temps.

La conséquence est assumée et écrite dans le code plutôt que découverte : réglage
activé, `S` sur un dossier descend aussi dans ce que `.venv`, `.terraform` ou
`.cache` embarquent. La limite de profondeur (3) est ce qui la borne.

`TestTheSettingReachesNestedDiscovery` est la garde qui relie les deux bouts :
sans elle, un futur `enrichEntry` pourrait oublier le paramètre et les deux
filtres reprendraient leur vie séparée en silence.

**Au passage, l'aide de `ws` mentait depuis §3.26.** Écrire une section pour le
nouveau réglage a montré le reste : `GetHelpContent` annonçait toujours `↑/k`,
`→/l`, `←/h` — les alias vim supprimés en entier — une section « Terminal »
décrivant un `t` qui n'existe plus à côté d'un `T` dont le comportement est
désormais un réglage, et **`s` pour synchroniser** dans deux sections alors que
c'est `F`. Le vide de la vue disait `Press [ctrl+n]`, une touche disparue *et*
une ligne d'aide dans le viewport que Rule 134 interdit : elle est retirée, le
header offre déjà `N`. Trois commentaires de `GetShortcuts` nommaient encore
`ctrl+w`, `ctrl+s` et `ctrl+n`.

Rien de tout ça n'est vérifié par un test : `internal/ui/keymap` oppose les
*sources* à la règle et attrape une touche qu'une vue **lie**, pas une touche
qu'une vue **raconte**. L'aide est donc la seule surface où une touche morte
survit sans que rien ne le dise — ce qui vaut d'être noté pour le prochain
relevé.

### 3.32 Le viewer colore Markdown, Dockerfile et shell — et rend le Markdown — **done**

Deux demandes, et la seconde est la plus intéressante : colorer trois formats de
plus, et pouvoir lire un Markdown « en format brut ou pas ».

#### Ce que la sonde des lexers a établi

Le mapping `classOf` est documenté comme **lu sur les lexers, pas deviné**, donc
la première action a été de faire tourner `markdown`, `docker` et `bash` sur des
échantillons et de regarder la sortie. Quatre faits mesurés ont décidé de la
forme du reste :

- **La catégorie `Generic` n'était mappée nulle part.** C'est tout le vocabulaire
  de Markdown — titres, gras, italique, barré — donc un `.md` serait arrivé
  quasiment incolore. C'est ce qui impose de nouvelles classes plutôt qu'un
  simple ajout de lexer.
- **Aucun des quatre lexers déjà en place n'émet un `Keyword` nu.** JSON, YAML,
  TOML et XML n'émettent que `KeywordConstant`. Séparer les deux est donc une
  modification **à régression nulle** — et vérifiable, ce qui vaut mieux que
  probable.
- **Le lexer markdown émet un token par caractère** pour la prose : sa dernière
  règle inline est une alternative attrape-tout d'un caractère. À 5 MiB
  (`viewer.MaxSize`) c'est des millions de `Token` pour un paragraphe qui compte
  une poignée de runs.
- **Une fence ```` ```go ```` est déjà sous-lexée en Go par chroma.** Le rendu en
  hérite gratuitement : un bloc de code garde la coloration de son langage.

#### `f` porte un axe, pas un écran de plus

`f` faisait « arbre ↔ texte ». Le rendu Markdown est **la même question** : le
document tel qu'il est, contre la seule vue que son kind en dérive. Donc `display`
gagne `displayRendered`, mais la bascule reste binaire à tout instant — un kind
dérive au plus une vue, `Structured()` ou `Renderable()`, jamais les deux, et
`TestNoKindHasTwoDerivedDisplays` le dit.

`Model.derived()` est **une** fonction pour cette raison : `f`, l'affichage
d'ouverture et `GetShortcuts` doivent donner la même réponse, ou la vue propose
un affichage qu'elle refusera ensuite de montrer (Rule 130). Un `.md` s'ouvre
donc rendu, comme un `.json` s'ouvre sur son arbre.

**`c` reste orthogonale.** Éteindre la couleur d'un Markdown rendu ne fait pas
réapparaître ses marqueurs : le rendu est un affichage, la coloration en est une
autre. Un `c` qui révélerait les marqueurs serait la seconde voie vers un même
écran — exactement ce que §3.9 a démonté sur le backend de secrets et ce qui a
emporté la commande `:theme`.

#### Le rendu est le même flux de tokens, marqueurs retirés

`RenderMarkdown` ne parse rien. Chaque marqueur qu'il retire est un que **le
lexer a déjà identifié** — un titre, un gras, une emphase, un barré, une puce, un
préfixe de citation, une fence. Rien n'y décide à partir de ce à quoi un
caractère ressemble.

C'est ce qui trace la limite, et la limite est le meilleur de la décision :
**les liens, les tableaux et les filets horizontaux passent intacts.** Les
crochets d'un lien arrivent en `Text` nu, indistinguables d'un crochet de prose,
donc reconstruire un lien serait précisément la devinette que ce paquet refuse
partout ailleurs. Le texte du lien et son URL sont colorés séparément à la place,
ce qui est l'essentiel de ce que le rendu aurait apporté.

L'invariant que ça casse est celui de `Tokenize` : **la concaténation ne
reproduit plus l'entrée**. C'est le but. Tout l'aval s'en accommode parce qu'il
lit les tokens et non le document — `splitTokenLines` reconstruit le texte de
chaque ligne à partir d'eux, donc la recherche filtre et surligne ce qui est
réellement à l'écran. Et l'affichage brut reste la source au caractère près, ce
qui fait des omissions ci-dessus des limites plutôt que des pertes.

Un détail qui a demandé deux essais : la coalescence (ci-dessous) **soude** les
backticks d'ouverture, le nom du langage et parfois le premier run du corps en un
seul token. Le délimiteur est donc retiré **à la ligne** et non reconnu en
entier, sans quoi un bloc dont le code commence par une chaîne perdait sa
première ligne avec la fence. `TestAFencedBodyStartingWithAStringSurvives` tient
ce cas.

#### Cinq classes de plus, deux couleurs seulement

`TokenClass` passe de 8 à 13, et trois des nouvelles ne portent **pas de teinte**
mais un attribut :

| Classe | Style | Pourquoi |
|---|---|---|
| `ClassKeyword` | `ColorSyntaxKeyword` = `ColorPrimary` | `FROM`, `RUN`, `if`, `fi` |
| `ClassHeading` | `ColorSyntaxHeading` = `ColorSecondary`, gras | titres |
| `ClassStrong` | `ColorText` + `Bold` | attribut, pas teinte |
| `ClassEmph` | `ColorText` + `Italic` | idem |
| `ClassStrike` | `ColorText` + `Strikethrough` | sans lui, `~~x~~` rendu est du texte nu |

Une fois les `**` et les `~~` partis, la graisse est la seule chose qui reste à
dire que deux runs étaient différents — et elle le dit mieux qu'une teinte, parce
qu'un mot en gras est en gras dans tous les thèmes alors qu'une couleur doit
avoir été apprise.

Contrepartie : un style qui ne porte qu'un attribut est exactement celui qu'on
écrit sans fond. Ils en posent un comme tous les autres (Rule 115), et
`TestEveryRenderedStyleCarriesTheAppBackground` le demande à chacun d'eux
directement plutôt que d'espérer le voir dans un rendu.

Les deux couleurs sont des **alias sémantiques assignés dans `ApplyTheme`**,
comme `ColorChartBg` et les couleurs de footer : aucun fichier de thème ne gagne
de clé. `ColorSyntaxKeyword` vaut aujourd'hui `ColorSyntaxLiteral` — invisible
dans le thème par défaut, et un nom pour un thème qui voudra les séparer. C'est
l'argument déjà tenu pour `ColorFooterError` contre `ColorError`.

#### La séparation `Keyword` / `KeywordConstant`, et le piège qui va avec

`KeywordConstant` est `true`, `false`, `null` — une valeur, donc `ClassLiteral` ;
tout autre mot-clé est un mot du langage, donc `ClassKeyword`.

Écrite de la façon évidente, la branche était fausse : chroma implémente une
sous-catégorie par `t/100 == other/100`, donc `InSubCategory(KeywordConstant)`
répond **vrai pour n'importe quel mot-clé** et avalait l'autre branche en
silence. Le symptôme était visible et discret à la fois — les puces Markdown
restaient des `-` au lieu de devenir des `•`, parce qu'elles arrivent en
`Keyword` et repartaient en `ClassLiteral`. Le test est donc une égalité, et
`TestAKeywordConstantIsStillALiteral` garde les quatre kinds d'origine.

#### La coalescence, sans laquelle Markdown n'est pas abordable

`Tokenize` finit par une passe qui fusionne les runs voisins de même classe. Ce
n'est pas du rangement : c'est la parade au troisième fait mesuré plus haut. Elle
préserve l'invariant de concaténation — seules les frontières bougent — et
supprime les runs vides que plusieurs lexers émettent entre leurs groupes.
`Match` n'est délibérément pas consulté : `Tokenize` ne le pose jamais,
`MarkMatches` le fait, après le filtre.

Mesuré sur l'échantillon de test : 43 tokens au lieu d'environ 120, et un
paragraphe de prose revient en **un** token au lieu d'un par lettre.

#### Reconnu par le nom, jamais par le contenu — sauf le shebang

`Dockerfile` n'a pas d'extension et `Dockerfile.dev` en a une (`.dev`) qui n'est
dans aucune table : une recherche par extension d'abord l'aurait classé texte
avant même de lire le nom. D'où `basenameKinds` et le préfixe `dockerfile.`
**avant** `extensionKinds`. Un dotfile fait l'inverse et atterrit dans la table
des extensions, parce que `filepath.Ext(".bashrc")` rend le nom entier ; quelle
table le trouve est un détail d'implémentation, qu'il soit trouvé ne l'est pas.

**Le shebang est la seule exception, et elle est écrite comme telle** — dans
`sniff()`, dans `TestAShebangDeclaresAShellScript`, et ici. `#!/usr/bin/env bash`
n'est pas un indice sur ce à quoi le fichier ressemble : c'est le fichier qui
nomme l'interpréteur qui doit l'exécuter, ce qui a le rang d'une extension moins
l'extension. À comparer avec `---` en tête d'un fichier, qui est du front matter
Markdown aussi souvent que du YAML — c'est pourquoi l'un est lu et l'autre pas.
Elle ne s'applique que là où `sniff` s'appliquait déjà (aucune extension du tout)
et qu'à une courte liste de shells : répondre à un `#!/usr/bin/env python` avec
le lexer bash colorerait un fichier Python de travers, ce qui est pire que de le
laisser nu puisque ça a l'air délibéré.

#### Ce que ça n'a pas coûté

Aucune dépendance : chroma embarque tous ses lexers, ce que les +4,0 Mo du
binaire avaient déjà payé. Aucune touche : `f` et `c` étaient déjà déclarées pour
le viewer dans `internal/ui/keymap`, donc le relevé de §3.26 n'a pas bougé.

#### Non retenu

- **glamour**, ou tout moteur de rendu Markdown. Ses formatteurs émettent leurs
  propres séquences ANSI et leurs propres resets — exactement ce que §3.29 a
  refusé aux formatteurs de chroma, et pour la même raison (Rule 115 : un reset
  en milieu de ligne emporte le fond jusqu'à la marge). Une sortie déjà colorée
  ne peut pas non plus être coupée pour le wrap.
- **Un arbre pour Markdown.** Ce serait la troisième vue dérivée d'un même kind,
  donc `f` ambiguë ; et un plan de document n'est pas ce qu'on ouvre un fichier
  pour lire.
- **Tableaux, filets horizontaux, listes imbriquées ré-indentées.** Le lexer ne
  les distingue pas, donc les rendre serait deviner. L'affichage brut reste la
  source exacte, ce qui rend la limite tenable.
- **Une classe par variable shell.** `NameVariable`, `NameBuiltin` et
  `NameFunction` rejoignent `ClassKey`, la famille « identifiant » où vivent déjà
  les clés JSON, TOML et YAML. « Un nom que ce document définit ou utilise » est
  une seule idée.

Le plan est dans
[`.claude/plans/viewer-markdown-dockerfile-shell.md`](../.claude/plans/viewer-markdown-dockerfile-shell.md).

---

### 3.33 Netdiag répond à des questions, pas à des outils — **done**

L'onglet Diagnostics lançait sept outils et affichait sept lignes portant leur
nom. L'objectif pour lequel il existe est plus étroit et mieux posé : **cet hôte
est-il joignable, et sa chaîne de certificats est-elle saine.** Une ligne est
désormais une question qui a reçu une réponse.

Trois PR : `internal/netcheck` (#87), les explications déterministes (#88), la
vue (#89). Une quatrième — l'explication passée à un modèle — est tranchée par
§3.38 : elle passe par un serveur MCP en lecture seule, et non par le client de
§3.10, qui tombe. Rien dans les trois premières n'en dépend. Le plan complet est
dans `.claude/plans/netdiag-checks-and-verdicts.md`.

#### Quatre défauts, mesurés

**`--network host` n'est pas cet hôte.** Sur Docker Desktop le conteneur
atterrit dans l'espace de noms réseau de la VM `docker-desktop` — relevé sur la
machine de développement : `hostname` répond `docker-desktop`, les adresses sont
`10.254.254.3/24` et `172.17.0.1/16`, le résolveur est `10.254.254.7`, quand la
machine résout par `10.2.0.1` (ProtonVPN actif) ou `192.168.1.2`. Le diagnostic
répondait pour un réseau sur lequel l'utilisateur n'est pas, et ne le disait
jamais. Le même défaut se lisait sans rien lancer : `defaultDNSServer()` ouvrait
`/etc/resolv.conf`, absent sous Windows.

**La chaîne n'était jamais vérifiée.** `sslCertScript` passait
`s_client -showcerts` dans `openssl x509 -noout -text`, et `x509` ne lit que le
premier bloc PEM — la feuille. Les intermédiaires étaient téléchargés puis
jetés, `Verify return code` partait dans `2>/dev/null`, et il n'y avait ni
correspondance de nom, ni échéance relative, ni version négociée. Sur la seule
question que la vue existe pour poser, la réponse était « on n'a pas regardé »,
rendue en vert. C'est la forme de D20, et `SecretVerdict` retourne un `*bool`
pour exactement cette raison.

**`Status` était le code de sortie du process**, donc la colonne n'était pas
comparable : `nc` qui sort en 1 dit « port fermé », `curl` qui sort en 1 dit une
douzaine de choses. **Et `RunCurl` choisissait le schéma sur `port == "443"`**,
donc `:8443` était sondé en clair, échouait, et se lisait « l'hôte est mort ».

**C'était aussi un doublon.** `internal/status` répond déjà nativement à ICMP
(pro-bing), DNS (`net.Resolver` avec nameserver choisi) et HTTP (`net/http`).
Deux implémentations de trois questions, répondant depuis deux piles réseau
différentes, et ne s'accordant pas.

#### Ce qui remplace les cases à cocher

Cinq verdicts, **valeur zéro `Unknown`** : un constat que personne n'a rempli ne
doit pas se lire comme un succès. `Warn` et `Fail` se séparent sur « l'objectif
est-il atteint », jamais sur l'inquiétude que ça inspire — la règle des niveaux
de footer (Rule 128). `NotApplicable` a deux sens, distingués par `Because` :
sans objet ici, ou bloqué en amont.

Un pipeline en couches — resolve, reach, connect, tls, http — où un étage dont
la dépendance a échoué ne tourne pas : ses constats reviennent `NotApplicable`
en nommant la cause **d'origine**, pas la cause immédiate. Sept lignes rouges
deviennent une ligne rouge et un point de rupture nommé. C'est ce qui rend les
cases inutiles plutôt que démodées : il n'y a plus rien à sélectionner.

Deux dépendances méritent d'être lues deux fois, et un test tient chacune.
**`reach` ne bloque rien** : ICMP est filtré sur une large part des hôtes
parfaitement joignables, donc un ping muet ne doit pas supprimer la question
qu'on est venu poser. **`http` dépend de `connect`, pas de `tls`** : une chaîne
cassée ne doit pas cacher que le service répond.

`Summarize` est le seul agrégat, pour la raison que `scan.Categorize` et
`SecretVerdict` ont chacun dû défaire.

#### La poignée de main n'est pas vérifiée, et c'est le point

`crypto/tls` qui avorte sur une racine inconnue effondrerait quatre questions en
une chaîne d'erreur et emporterait le certificat qu'il faut inspecter. La
vérification est donc faite par l'étage, qui distingue une racine inconnue, une
chaîne incomplète, un certificat auto-signé et un nom qui ne correspond pas.

Il signale aussi un serveur **qui n'envoie pas ses intermédiaires alors même que
la vérification réussit** : la plateforme locale complète parfois une chaîne
qu'un autre client ne complétera pas. C'est le bug classique qui marche sur le
poste du développeur et casse en production, et aucune version de l'ancien
contrôle ne pouvait le voir.

« Ce port ne parle pas TLS » est une **observation** et non une heuristique sur
le numéro de port : c'est `tls.RecordHeaderError`, que `crypto/tls` émet
lui-même. Un test le vérifie contre un vrai serveur HTTP en clair, parce que
c'est une affirmation sur le comportement de la bibliothèque et non sur le
nôtre. Deviner d'après le port serait l'heuristique que §3.8 a écartée ailleurs.

#### `Reason` — un verdict, plusieurs métiers

Un `Fail` de chaîne a trois causes aux remèdes différents : installer une CA,
déployer le fichier de chaîne complet, ou découvrir ce qui s'intercale dans la
connexion — un proxy d'entreprise fait exactement ça. Le verdict ne les sépare
pas, le résumé le fait en prose, et `Reason` est ce sur quoi une explication —
ou une sérialisation — peut brancher.

#### Les explications vivent dans `netcheck`

Observé / ce que ça signifie / quoi faire, en table déclarative, hors-ligne.
La connaissance porte sur le domaine et non sur la mise en page, et la PR 4
sérialisera les constats **avec** leurs explications : dans la couche UI, ce qui
les consomme devrait importer une vue Bubble Tea.

Deux règles, toutes deux vérifiées : `Means` énonce une conséquence et non une
paraphrase du résumé, et `Do` est **vide sur un succès** — une action inventée
pour une ligne verte apprend au lecteur à sauter le champ sur celles qui en ont
une.

#### La vue

Le formulaire passe de onze champs à quatre, et `enter` lance depuis n'importe
lequel : avec trois champs et un bouton il n'y a rien d'autre que ça puisse
vouloir dire.

**Les étages sont chaînés par messages**, pas lancés en un seul `Cmd`. C'est ce
qui permet au footer de nommer la question en cours, et ça compte précisément
sur le cas qui mérite un diagnostic : un hôte injoignable épuise ses délais l'un
après l'autre, et un spinner sans rien à côté est indistinguable d'un blocage —
la leçon de la modale `"Pulling..."` de §3.16. Les résultats accumulés voyagent
**dans le message** ; `netcheck.RunStep` copie pour ça, donc rien n'est partagé
entre `Update` et un `Cmd` (Rule 110).

La table reste à l'écran pendant que le pipeline avance et se remplit au fur et
à mesure (Rule 139) — un corps remplacé par un spinner perdrait aussi les lignes
déjà répondues. Le verdict global va dans `GetHeaderInfo`, comme security porte
`Findings`.

**Un seul jeton de filtre, `p`**, et non un par verdict : dix lignes n'ont pas
besoin de quatre filtres cumulatifs, elles ont besoin que le bruit disparaisse.

**Le tracé de route est la seule sonde qui sort encore un process**, parce que
c'est la seule qui doit : il faut des sockets bruts et un outil qu'il ne vaut
pas la peine de réécrire. Il est offert sur `H` **seulement quand un constat
désigne le chemin** (Rule 130) — un certificat qui ne vérifie pas n'est pas un
problème de routage, et trente secondes de tracé n'y répondraient pas. Le
panneau dit qu'il a tourné dans le conteneur, donc qu'il peut légitimement
contredire les constats au-dessus de lui.

#### Ce que ça supprime

`dns_formatter.go` et son test (le DNS est natif, il n'y a plus de sortie `dig`
à parser), six des huit runners de `internal/docker/netdiag.go` —
`sslCertScript` compris, ce qui retire la surface d'injection shell que
`validation_test.go` gardait — et `defaultDNSServer()`. `traceroute_formatter.go`
reste : le tracé reste.

#### Ce qui reste écarté

Des jeux de constats dérivés du port (§3.8 a tranché cette classe de choix dans
l'autre sens : déclaré, jamais reniflé), Presidio, la lettre de raccourci d'une
explication par modèle, la **migration de `internal/status` sur `netcheck`** —
consignée ici pour que le doublon ne se réinstalle pas — et la capture tcpdump.

### 3.34 L'onglet `docker` devient `network`, et netdiag cesse de coder ses délais en dur — **done**

L'onglet `docker` de la vue configuration portait **un** réglage,
`network_tool_image`, et ce n'était pas un réglage Docker : il nomme le
conteneur dans lequel tournent les tracés de route et la table des ports. Les
quatre autres onglets portent le nom de la section de config qu'ils écrivent, ce
qui vaut renommer la clé YAML et pas seulement l'étiquette — sans quoi le seul
onglet sur le point de grossir serait le seul dont le nom ne dit pas où
atterrissent ses valeurs.

**La migration est le vrai risque, et elle est silencieuse.** `LoadContext`
appelle `yaml.Unmarshal` sans `KnownFields`, donc un bloc `docker:` non migré est
**ignoré sans un mot** : l'image reviendrait à `nicolaka/netshoot`, et quelqu'un
qui pointe vers son propre miroir verrait ses tracés retomber sur Docker Hub sans
rien à l'écran pour le dire. `applyDefaults` reporte donc
`docker.network_tool_image` dans `network.tool_image` **avant** les valeurs par
défaut, puis vide l'ancienne clé pour qu'elle quitte le fichier à la prochaine
sauvegarde — le précédent est `RegistryItem.AuthEnabled`.
`TestANetworkToolImageSurvivesTheRename` et `TestTheNewKeyWinsOverTheOldOne`
tiennent les deux bouts : la nouvelle clé gagne toujours, l'ancienne n'est qu'une
source de migration et jamais un second écrivain.

#### Ce que l'onglet a gagné

`internal/netcheck/env.go` disait de ses constantes qu'elles *« deviennent des
réglages quand quelqu'un les demande, pas avant »*. Quelqu'un a demandé.

| Réglage | Remplace |
|---|---|
| `check_timeout` | cinq constantes — 5 s pour le DNS et le dial, 8 s pour TLS et HTTP, 4 s pour le ping |
| `ping_count` | `pingCount` |
| `cert_expiry_warn_days` | `expiryWarnWindow` |
| `traceroute_max_hops` | le `-m 30` de `docker/netdiag.go` |
| `ports_refresh_interval` | le tick de 2 s |

**Un délai pour cinq.** Le partage 5 / 5 / 8 / 8 / 4 n'est argumenté nulle part :
il se lit comme cinq estimations séparées, pas comme une conception. Cinq lignes
de formulaire pour une seule idée — *combien de temps une sonde attend une
réponse* — c'est précisément ce que §3.33 a retiré à cette vue en supprimant ses
sept cases à cocher.

Le défaut est **l'ancien maximum** (8 s), pas une moyenne : rien de ce qui répond
aujourd'hui ne se met à échouer. Le coût est énoncé plutôt que découvert — un
hôte injoignable passe maintenant 8 s sur le DNS au lieu de 5 — et il est
acceptable pour la raison que §3.16 a payée : le pipeline annonce l'étape en
cours (`StageTitle`), donc l'attente se lit au lieu de ressembler à un blocage.

#### Trois choses restent en dur, et chacune pour une raison

- **L'attente par saut du traceroute (`-w 1`)** se multiplie avec le nombre de
  sauts. Deux réglages laisseraient construire un tracé de quinze minutes à
  partir de deux nombres qui semblent chacun raisonnables.
- **`MinVersion: VersionTLS10`** — la poignée de main *sonde*, elle ne sécurise
  pas. Signaler une version périmée est tout l'objet ; un réglage ne pourrait que
  rendre l'outil aveugle à ce qu'il existe pour trouver.
- **`InsecureSkipVerify`** — l'étape TLS vérifie la chaîne elle-même pour dire
  *quelle* partie a lâché. Un réglage ici écraserait quatre constats en une
  chaîne d'erreur.

#### `Settings` voyage à côté de `Env`, pas dessus

`Env` est la couture vers le réseau ; `PingCount` et `ExpiryWarnWindow` sont lus
par des **étapes** et par aucun appel réseau. Les mettre derrière la couture
obligerait chaque fake à répondre pour une préférence.

`Settings.Normalized()` comble ce qui est ≤ 0, à **chaque** point d'entrée — un
`0` écrit à la main devient le défaut au lieu d'un dial sans échéance, c'est-à-dire
un blocage au lieu d'un verdict. La vue appelle `checkSettings()` par exécution
plutôt qu'à la construction : la vue configuration reconstruit le modèle à chaque
sauvegarde, et une exécution déjà en vol porte sa copie dans la chaîne de
messages — les deux ne peuvent donc pas diverger à mi-pipeline.
`portsTickCmd` porte la même garde pour la même raison, au dernier lecteur plutôt
qu'au constructeur : un intervalle nul ferait tirer `tea.Tick` sans pause, soit un
`docker exec` par frame.

### 3.35 `Y` copie le chemin de la ligne sélectionnée — **done**

`ws` est un explorateur de fichiers, et la chose qu'on veut en sortir le plus
souvent n'est ni un terminal ni un IDE : c'est le chemin lui-même, pour le coller
ailleurs. Il n'y avait aucun moyen de l'obtenir autrement qu'en le relisant à
l'écran et en le retapant — un chemin absolu de six segments, à la main.

**La touche est `Y`, prise dans la liste des libres.** C'est *yank*, et la lettre
y figurait déjà avec sa réserve écrite : une modale l'emploie pour « Yes », mais
une modale réclame toute touche avant que la vue ne la voie, donc les deux ne
sont jamais joignables en même temps. `free` passe donc à `J Q Z`, et les deux
documents qui recopiaient la liste sont corrigés au passage — ils annonçaient
encore `H`, que `Trace` occupe depuis §3.33. Une liste de disponibles périmée est
pire que pas de liste : c'est exactement ce contre quoi
`TestFreeLettersAreActuallyFree` existe, et il ne relit pas la documentation.

#### Pas de repli sur le répertoire courant

`T` et `O` en ont un — `resolveTargetPath` remonte au chemin parcouru quand aucune
ligne n'est sélectionnée — et c'est juste pour eux : ils agissent sur *un lieu*,
donc « ici » est une réponse sensée. `Y` répond à « qu'est-ce que cette ligne »,
et rendre silencieusement le répertoire parent serait une réponse **fausse**
plutôt qu'absente : le presse-papier ne dit pas d'où vient ce qu'il contient, donc
l'erreur ne se découvrirait qu'au collage, ailleurs. Sans ligne, la touche ne fait
rien et n'est pas annoncée (Rule 130).

**Un fichier compte autant qu'un répertoire.** Toutes les autres actions de la vue
visent un dossier — `S`, `F`, `T`, `O` filtrent sur `IsDir` ou sur `IsGitRepo` —
donc un résolveur écrit sur leur modèle aurait sauté le seul cas que l'utilisateur
a nommé en premier. `copyTarget` est séparée de `copyPath` pour cette raison :
elle rend le chemin choisi sans écrire nulle part, donc un test peut vérifier
*quelle* ligne a été prise sans toucher au presse-papier de la machine — qui n'est
ni le sien à prendre, ni disponible sur un runner sans session graphique.

#### Ce que le footer dit, et ce qu'il ne répète pas

`Full path copied to the clipboard` — pas le nom de l'entrée, qui est sous le
curseur : le répéter serait le même fait deux fois sur un écran. Ce qui n'est pas
à l'écran, c'est la **forme** de ce qui a atterri — le chemin absolu et non le nom
— et c'est ce que la ligne dit.

L'échec est une **erreur** au sens de la Rule 128, pas une notice : `WriteAll`
échoue pour des raisons extérieures à l'application (pas de propriétaire de
sélection sous X, pas de `pbcopy`), et un échec silencieux laisse coller ce qui
s'y trouvait avant — un chemin plausible, pris pour le bon. `PathCopiedMsg` porte
le chemin pour la ligne de log, seule à pouvoir nommer ce qu'on essayait de
copier.

### 3.36 `:sec` nomme une image par son alias de registry — **done**

L'inventaire affichait la clé de cache brute
(`nexus.example.com/docker-hosted/agent-base:1.0`) là où l'onglet Images d'`:oci`
substitue depuis toujours l'alias configuré (`nx/agent-base:1.0`). C'est la table
la plus serrée de l'application — `Target` partage sa largeur avec Secrets, quatre
colonnes de sévérité et Scanned — donc le préfixe est exactement ce qui pousse
hors de la cellule la partie qui identifie l'image. La substitution existait
(`docker.ApplyAliases`) ; il manquait de l'appeler, et un endroit d'où l'appeler
deux fois sans la réécrire.

#### L'invariant, et pourquoi il coûte un champ de plus

**`scanTarget.Name` est la clé de cache et ne bouge pas.** C'est ce que résolvent
`enter`, `S` et `A`, et c'est ce que `AddToGitleaksIgnore` reçoit via
`m.targetPath` : un alias qui atteindrait cette valeur ferait écrire un
`.gitleaksignore` dans un répertoire qui n'existe pas. La règle était déjà écrite
pour le pliage de `~` — *le nom affiché est dérivé, la clé est portée* — et c'est
elle qui impose `targetLabel` **à côté** de `targetPath` plutôt qu'un pliage
appliqué sur place. Les deux champs sont lus dans des buts opposés : l'un est
montré, l'autre est écrit sur le disque.

L'alias voyage donc sur la ligne (`Display`), estampillé par `setInventory` à côté
de la frame du spinner, pour les deux mêmes raisons : les lignes arrivent d'un
`Cmd`, qui ne doit pas lire le modèle (Rule 110), et une fonction de colonne est
construite une fois dans `New` et ne peut atteindre ni l'un ni l'autre. C'est le
patron `imageRow`, une deuxième fois.

`displayName()` retombe sur `Name` quand `Display` est vide : une ligne fabriquée
à la main — les fixtures des tests en construisent — doit rester lisible plutôt
que d'afficher une cellule vide.

#### Où vit l'adaptateur, et pourquoi pas dans l'un des deux paquets

`internal/docker` ne connaît pas `internal/config`, délibérément : c'est le pilote
de la CLI Docker, et `docker.RegistryAlias` est son type propre pour cette raison
— un pilote qui apprend le schéma d'un fichier YAML ne peut plus être appelé sans
lui. L'inverse est pire : `config` décrit ce que l'utilisateur écrit, et n'a
aucune raison de dépendre de la façon dont Docker nomme les choses.

`internal/ui/registryalias` est donc **au-dessus des deux**, et côté UI parce que
ses deux appelants sont des vues qui affichent la même image et doivent l'écrire
pareil. Il porte une fonction. `From` préserve l'ordre de déclaration, et ce n'est
pas cosmétique : `ApplyAliases` retient le **premier** préfixe qui matche, donc
c'est l'ordre qui départage deux registries dont l'un préfixe l'autre. Une entrée
sans alias ou sans URL est écartée plutôt que portée avec une chaîne vide, qui
ferait matcher tout nom d'image.

#### Trier sur la clé, chercher les deux

Le tri reste sur `Name` : un alias est un nom d'affichage que l'utilisateur peut
renommer, et trier dessus déplacerait toutes les lignes d'un registry le jour où
il en change. La recherche prend les deux noms — c'est ce que l'onglet Images fait
déjà, et pour la raison qui y est écrite : une colonne qui affiche un nom pendant
que la requête en veut un autre se lit comme un défaut.

Les trois messages de footer qui nomment une cible sont pliés aussi. Le footer
tronque à la largeur (Rule 128), et le préfixe est précisément ce qui s'y
dépenserait. Ils passent par `labelFor`, qui **résout la ligne** plutôt que de
plier la chaîne à l'aveugle : un chemin absolu de dépôt peut légitimement
commencer par une URL de registry configurée, et seule la ligne sait de quel
cache le nom vient.
### 3.37 L'inventaire de `:sec` ne liste plus ce qui n'existe plus — **done**

Une image supprimée par `D` continuait d'apparaître dans `:sec`. La suppression
retire l'image et relance `fetchImages()`, mais ne touche jamais
`ImageScanCache` — et l'inventaire *est* ce cache. `deleteScanCacheCmd` existait
déjà dans le même fichier ; seul le purge de `A` l'appelait.

Le défaut n'était pas sur un chemin mais sur quatre :

| Chemin | Ce qui restait |
|---|---|
| `D` sur une image (`:oci`) | l'entrée `ImageScanCache` |
| `P` (prune) | toutes les entrées des images supprimées |
| `D` sur un dépôt (`:ws`) | l'entrée `WorkspaceScanCache` |
| un `docker rmi` ou un `rm -rf` hors DevDesk | idem |

#### Réconcilier au chargement plutôt que cascader

Quatre cascades auraient corrigé trois cas et demi : le prune supprime un
ensemble d'images qu'il ne nomme pas, et le quatrième chemin est le monde
extérieur, qui n'appelle rien. Une règle unique au chargement les couvre tous —
`loadInventoryCmd` écarte une image absente de `docker image ls` et un chemin de
dépôt qui n'existe plus.

Le coût est réel et assumé : `:sec` ne lisait que deux fichiers, il fait
maintenant un `docker image ls` à chaque ouverture et à chaque `ctrl+r`. C'est
ce que la vue OCI fait déjà, et c'est le prix d'une seule règle plutôt que de
quatre appelants à ne pas oublier.

#### Un échec n'est pas une absence

C'est le garde-fou sur lequel toute la conception repose. « Docker n'est pas
lancé » et « l'image a disparu » sont le même silence vu de l'appelant, et lire
le premier comme le second viderait l'inventaire de toutes ses images à l'arrêt
du démon. `localImages` rend donc un **second retour** disant si elle a pu
savoir : une énumération ratée conserve tout. `isGone` teste `os.IsNotExist` et
rien d'autre — une erreur de permission ou un partage non monté veut dire que le
chemin n'a pas pu être *lu*, ce qui n'est pas la même affirmation.

C'est le même défaut que D20 sous un autre jour : afficher une cible qui n'existe
plus est une ligne périmée, en cacher une qui existe est un mensonge.

#### Cacher, pas supprimer

L'entrée et son résultat stocké restent sur le disque. Une réponse transitoire —
un démon qui revient avec une liste plus courte, un partage pas encore monté — ne
doit pas détruire un scan que personne n'a demandé de purger, et `A` ne rescanne
que les lignes présentes, donc une entrée cachée ne coûte rien en attendant.

#### `listImages` est une variable de paquet

Pour cette seule raison, et la production ne la réassigne jamais — le précédent
est `FooterMsgDuration`. La réconciliation fait dépendre le loader de ce que le
démon détient, et un test ne peut pas *pull* une image : sans cette couture, les
allers-retours par les vrais fichiers de cache se réduiraient à vérifier qu'une
fixture est absente, ce qu'ils réussiraient pour la mauvaise raison. Deux d'entre
eux ont été corrigés dans le même mouvement — les dépôts fixtures sont désormais
de vrais répertoires temporaires, et le test de cloisonnement par contexte
déclare son image *pullée* pour que son absence ne puisse venir que du contexte.

### 3.38 Un serveur MCP en lecture seule — le modèle vient à DevDesk — **done**

**§3.10 partait dans l'autre sens : DevDesk assemblait un payload, le
pseudonymisait, le montrait, l'envoyait à un endpoint qu'il fallait configurer,
et affichait la réponse en streaming.** Le sens s'inverse. DevDesk **expose ce
qu'il sait** par un serveur MCP, et l'agent — Claude Code, ou n'importe quel
client MCP — vient le lire. §3.10 est remplacée par cette entrée.

#### Ce qui est arrêté

| # | Question | Décision |
|---|---|---|
| 1 | Sens | DevDesk est **serveur**, l'agent est client. §3.10 tombe. |
| 2 | Transport | **stdio**, par une sous-commande `dk mcp`. Pas de HTTP en v1. |
| 3 | Périmètre | **Lecture seule.** Aucun tier `act` — ni maintenant, ni dans le champ de cette entrée. |
| 4 | Activation | `mcp.enabled: false` par défaut. `dk mcp` refuse en nommant le réglage et le contexte. |
| 5 | Portée | **Un contexte par process** — `dk mcp --context <name>`. |
| 6 | Secrets | Aucun outil ne lit le store de §3.9, et le `Match` d'un finding de secret ne sort jamais. |
| 7 | Exposition | **Allow-list explicite** de noms d'outils, jamais une deny-list. |

#### Pourquoi §3.10 tombe

Ce que le renversement supprime, et qui n'a donc pas à être écrit : le client
OpenAI-compatible, le bloc `AIConfig` et son token, le pseudonymiseur et son
inverse, le panneau de confirmation, le streaming en Bubble Tea — le canal, le
`Cmd` qui se réarme, le compteur de génération — et la `context.CancelFunc` que
D13 rendait obligatoire. C'est la moitié d'une fonctionnalité qui disparaît
parce qu'un protocole existe pour ça.

Le coût est réel et tient en une phrase : **l'explication n'est plus dans le
TUI.** L'utilisateur la lit dans son agent. §3.10 s'inquiétait déjà d'en faire
un produit de chat et refusait une vue dédiée ; MCP va au bout de ce
raisonnement — DevDesk ne parle pas du tout au modèle.

**La garantie change de main, et c'est plus honnête.** §3.10 rendait DevDesk
responsable de ce qui atteint un modèle, à chaque envoi, par une
pseudonymisation. Ici la responsabilité est celle de l'utilisateur, une fois, au
moment où il bascule `mcp.enabled`. Le troc est assumé parce que la promesse de
§3.10 n'était pas tenable jusqu'au bout : la pseudonymisation préserve la classe
analytique d'une adresse, elle ne pouvait rien pour un `Match` Gitleaks, où le
payload *est* le secret. Une garantie qui vaut pour les adresses et pas pour les
secrets est plus dangereuse qu'un réglage explicite, parce qu'elle se lit comme
si elle valait pour tout.

#### Un TUI ne peut pas servir du stdio

Bubble Tea possède stdin et stdout en entier : il n'y a pas de place pour un
second protocole dans le même process. Le serveur est donc une **sous-commande
headless**, `dk mcp`, que le client lance lui-même. `main()` part aujourd'hui
directement dans le TUI sans lire un seul argument, donc c'est une quinzaine de
lignes en tête de `main.go` et aucun framework CLI.

| | stdio (`dk mcp`) | Streamable HTTP servi par le TUI |
|---|---|---|
| Cycle de vie | le client lance et tue | le TUI doit tourner |
| Authentification | aucune — le process est déjà l'utilisateur | bearer token obligatoire, bind sur `127.0.0.1` |
| État visible | ce qui est sur disque | la session vivante |
| Agent en conteneur | impossible — il ne peut pas exécuter le binaire hôte | seule voie |

**L'état vivant n'est pas le sujet**, et c'est ce qui rend stdio suffisant : les
contextes, `config.yaml`, les deux caches de scan et le cache de groupes de
registry sont sur disque ; les conteneurs, les images et la forge sont ailleurs.
Un process headless répond aux mêmes questions qu'une session ouverte. Le
transport reste derrière l'interface du SDK pour que HTTP soit un flag et non
une réécriture.

#### La lecture seule achète l'absence de verrou

`~/.devdesk/` n'a aucun verrou — le CLAUDE.md le dit déjà pour les worktrees, et
rien n'avertit quand deux écrivains se croisent. Un serveur qui n'écrit rien
retire la question : il peut tourner pendant que le TUI tourne, dans un autre
contexte, sans que personne ait à y penser.

**Une exception, et elle est précise.** `readScanCacheFile` met à niveau en place
un cache antérieur aux contextes, dès la première ouverture, et son commentaire
dit pourquoi : le propriétaire des entrées devient *le contexte courant au
moment de la mise à niveau*. Un serveur ouvert avec `--context X` réclamerait
donc pour X des entrées que personne ne lui a attribuées — un chemin de lecture
qui écrit, et qui décide. Le serveur doit donc lire **sans mettre à niveau** :
il sert les entrées héritées et laisse la décision de propriété au TUI, où
quelqu'un est présent pour la voir.

#### Les outils

La règle de sélection : **exposer ce que DevDesk sait et qu'un agent ne peut pas
obtenir à moindre coût.** Un agent sait lancer `docker ps` et `trivy` tout seul ;
il ne sait pas ce que la notion de contexte veut dire ici, ni ce qui a déjà été
scanné.

| Outil | Ce qu'il apporte |
|---|---|
| `context_list` / `context_get` | l'abstraction contexte : `workspaces_dir`, alias de registry, URL de forge. **Sans secret par construction** — le schéma n'a aucun champ pour en porter (§3.9) |
| `scan_inventory` | l'inventaire de `:sec` : cibles, quatre compteurs, `Sensitive *bool`, âge. Déjà réconcilié (§3.37) |
| `scan_result` | les findings d'une cible, filtrés par sévérité et **paginés** — un scan d'image en produit des milliers |
| `workspaces_list` | les dépôts, leurs métadonnées git (ahead/behind, arbre sale) et leur état de scan |
| `registries_list` / `registry_tags` | depuis le cache de groupes, **jamais le réseau** — la règle du browser |
| `containers_list` / `images_list` / `ports_list` | l'inventaire, sans que l'agent invente ses flags |
| `net_check` | le pipeline `internal/netcheck` : cinq verdicts et leur `Because` |

`net_check` est la pièce pour laquelle cette entrée vaut la peine. C'est le seul
outil qui touche le réseau, et il est retenu quand même : il *lit* le réseau
sans rien changer sur l'hôte, DevDesk le lance déjà sur une touche sans
confirmation, et son résultat est structuré et déterministe — « le nom résout,
le port accepte, la chaîne est incomplète à l'intermédiaire » est ce qu'un
modèle sait exploiter, contrairement à un code de sortie de `curl`. Le résidu se
dit plutôt qu'il ne se découvre : un agent peut le pointer sur n'importe quel
hôte, donc c'est une primitive de sonde autant qu'un diagnostic.

Le `Sensitive *bool` traverse tel quel, `nil` compris. Un serveur qui le rendrait
`false` referait D20 à travers un protocole.

#### Non retenu

- **Tout tier `act`.** Scanner, cloner, synchroniser, arrêter un conteneur,
  purger : hors du serveur, et pas dans un second niveau désactivé par défaut.
  Un agent qui se trompe de ligne dans le TUI rencontre une modale ; ici il n'y
  a personne.
- **`container_logs`.** §3.10 l'avait déjà classé — les logs portent des
  variables d'environnement et des DSN de façon routinière. Un log se lit dans
  le viewer, où l'utilisateur voit ce qu'il transmet.
- **Le `Match` d'un finding de secret.** Règle, chemin, ligne, entropie : oui.
  La chaîne : jamais. C'est la conclusion à laquelle §3.10 arrivait déjà pour le
  triage Gitleaks.
- **Presidio.** Sans objet ici : DevDesk n'assemble plus le payload, donc il n'y
  a rien à re-vérifier après construction.
- **HTTP en v1**, et le token qui va avec.

#### Le réglage

```yaml
mcp:
  enabled: false               # dk mcp refuse en nommant le réglage et le contexte
  expose: []                   # allow-list de noms d'outils ; vide = tous
  redact_secret_matches: true
```

Une septième section dans `Config`, et un sixième onglet dans la vue
configuration pour ses deux scalaires — `expose` est une liste, donc elle reste
au fichier, comme les monitors et les registries restent là où on les consulte.

`expose` est une allow-list et non une deny-list pour la raison que §3.10 avait
déjà formulée dans l'autre sens : **un champ jamais inclus ne peut pas rater sa
redaction.**

#### Ce que ça ne coûte pas

Aucun impact TUI : pas de vue, pas de touche, pas de message. Un paquet
`internal/mcp` qui appelle les mêmes paquets que les vues, plus un SDK. Une
table d'outils déclarés et un test qui vérifie que chacun a un handler et
réciproquement — l'esprit de `internal/ui/keymap` et de `AllViewNames()`.

#### Questions ouvertes

1. **L'agent en conteneur.** Si un agent tournant en sandbox doit atteindre le
   serveur, stdio ne suffit pas et HTTP n'est plus « plus tard » mais v1, avec
   son token et son bind.
2. **Resources et prompts MCP.** Un résultat de scan est adressable et immuable
   (`devdesk://ctx/dev/scan/<cible>`), donc naturellement une *resource* plutôt
   qu'un tool ; un prompt « trie ce scan » embarquerait la doctrine du dépôt —
   UNKNOWN n'est pas pire que CRITICAL, un `Sensitive` nil veut dire que
   personne n'a regardé. À trancher au plan.
3. **Le SDK Go**, à choisir et à vérifier au moment d'écrire — le terrain bouge.

#### Ce que la réalisation a changé à l'énoncé

Fait le 2026-08-23, en sept étapes. Le plan détaillé et son journal sont dans
[`.claude/plans/mcp-server-plan.md`](../.claude/plans/mcp-server-plan.md).

Trois décisions ont dû être prises **contre** ce qui est écrit plus haut, et
chacune parce que l'énoncé se contredisait ou demandait l'impossible :

**`redact_secret_matches` n'existe pas.** Le réglage était déclaré à `true` par
défaut alors que la même entrée classe la chaîne d'un finding de secret en
« jamais exposée » : son autre valeur était donc refusée, ce qui en fait un
paramètre qu'il faut ignorer — l'argument de §3.39. Pire, `false` est la valeur
zéro d'un `bool`, donc tout fichier écrit avant la clé aurait décodé à « ne pas
caviarder » : D12 exactement. Le schéma des outils n'a aucun champ où porter la
chaîne, ce qui est la garantie que `context_get` obtient déjà par construction.

**`registry_tags` n'est pas constructible.** L'entrée le voulait « depuis le
cache de groupes » : il n'y a pas de cache de tags. Le cache de groupes tient les
*membres*, et les tags sont récupérés en HTTP quand le browser cherche. Les
chercher ici demanderait un identifiant pour chaque registry que quelqu'un fait
tourner, alors que la décision 6 est qu'aucun outil ne lit le store de §3.9. Un
listing anonyme répondrait « aucun tag » pour un registry privé — une absence lue
comme un vide, c'est-à-dire D20.

**`ports_list` est abandonné**, décision de l'utilisateur. `docker.RunSS` est
`docker run --rm --net=host --pid=host --privileged`, le même appel que
`KillProcess` à la commande près. Rien de persistant ne change sur l'hôte, mais
la promesse de ce serveur est qu'il n'agit pas sur la machine, et démarrer un
conteneur privilégié est agir sur elle. Les sockets restent lisibles dans `:net`.

Une contrainte que l'énoncé ne nommait pas et qui casse tout si elle tombe :
**rien n'écrit sur stdout sauf le protocole**. `main()` en faisait quatre sur le
chemin TUI, et une seule avant le serveur ferait rapporter au client une erreur
de parsing qui ne désigne rien. Deux tests de source la tiennent.

Et l'exception de lecture seule était plus large que prévu : **trois** chemins
d'écriture traînaient sur ce qui devait être une lecture — l'attribution des
entrées héritées, le repli de §3.39, et le `MkdirAll` des constructeurs *et* des
`Load*ScanResult`. `internal/cache/readonly.go` retourne des **maps et pas un
cache** : sans cache il n'y a pas de `Set`, donc l'écriture est inexprimable
plutôt qu'interdite par revue.

Deux questions ouvertes sur trois sont tranchées : le SDK est
`modelcontextprotocol/go-sdk` v1.7.0 (garantie de compatibilité v1, table des
révisions de spec déclarée par le SDK), et il n'y a **ni resources ni prompts** —
une resource se lit par URI après énumération, donc elle ne se filtre ni ne se
pagine, ce dont `scan_result` a précisément besoin ; un prompt embarquant la
doctrine du dépôt serait la seule partie du serveur à pouvoir devenir fausse sans
que rien n'échoue. La troisième reste ouverte : **l'agent en conteneur**, qui
demanderait HTTP en v1 avec son token et son bind.

Coût : **+2,99 Mo** sur le binaire (24,98 → 27,97 Mo).

### 3.39 Le cache de scan d'images cesse d'être scopé au contexte — **done**

Fait le 2026-08-23. Signalé par l'utilisateur : changer de contexte fait
disparaître les compteurs de CVE des images, alors que les images, elles, n'ont
pas bougé.

#### Ce qui décide, c'est la clé — pas la configuration

La phase 0b de [`configuration-view-plan.md`](../.claude/plans/configuration-view-plan.md)
avait scopé les **deux** caches de scan au contexte, avec un seul argument pour
les deux : « la configuration est par contexte ». C'est vrai du cache des
workspaces et faux de celui des images, et la différence tient à ce sur quoi
chacun est indexé :

| Cache | Clé | Scopé |
|---|---|---|
| `WorkspaceScanCache` | chemin absolu, atteint via `workspaces_dir` | **oui** — `workspaces_dir` est par contexte, donc un même chemin peut désigner un travail différent |
| `ImageScanCache` | une référence Docker locale | **non** — `docker image ls` répond pour la machine, pas pour une configuration |

Deux contextes qui regardent `nginx:1.25` regardent les mêmes octets. Les scoper
faisait donc perdre, à chaque bascule, des compteurs pour des images qui
n'avaient pas changé.

**Et l'incohérence était déjà dans le code.** Les résultats complets n'ont
*jamais* été scopés : `SaveImageScanResult` / `LoadImageScanResult` adressent par
SHA256 du nom de l'image, sans contexte nulle part. L'index et les blobs à côté
de lui se contredisaient, et c'est l'index qui avait tort — le disque gardait des
findings que la vue déclarait absents.

#### Ce qui change

`NewImageScanCache()` ne prend plus de contexte : un paramètre qu'il faut ignorer
est pire que pas de paramètre, et le compilateur a trouvé les neuf appels.

Le fichier revient à une carte plate, versionnée : `{version: 2, entries: {…}}`.
`readImageScanCacheFile` connaît les trois formes qu'il a eues —

- **v2**, telle quelle ;
- **v1**, une carte par contexte, repliée en une seule ;
- **v0**, la carte nue d'avant les contextes, qui *est* déjà cette forme et n'a
  qu'une version à recevoir.

**Une collision dans le repli se tranche, elle ne se signale pas.** Deux
contextes ayant scanné la même image est le cas ordinaire — c'est une image, et
tous les deux l'ont vue — donc le `ScannedAt` le plus récent gagne : l'image n'a
pas changé entre les deux, la base de vulnérabilités si.

Le repli est **écrit au premier open**, pas différé au premier `Set`, pour la
raison qui valait déjà pour la migration vers les contextes : un fichier laissé
dans l'ancienne forme serait replié à chaque ouverture, donc ce qu'il contient
dépendrait de quand il a été lu pour la dernière fois.

`internal/cache/scan_file.go` garde la forme scopée, qui reste celle du cache des
workspaces — et que le cache d'images lit encore, uniquement pour la replier.

#### Le coût, énoncé plutôt que découvert

**Les options de scan sont par contexte** (`enable_vuln`, `ignore_unfixed`,
`enable_secret`…), donc une entrée partagée a pu être produite sous celles d'un
autre contexte. C'est le prix accepté : la colonne `Scanned` porte l'âge, et `S`
rescanne. Le verdict de secrets n'est pas touché — `Sensitive` vaut `nil` quand
aucune étape n'a cherché, ce qu'écrit de toute façon un scan lancé secrets
éteints.

L'inventaire de `:sec` montre l'asymétrie à l'écran : un dépôt scanné sous un
autre contexte reste caché, une image non. C'est l'inverse de ce que
`TestTheInventoryListsOnlyTheCurrentContext` affirmait, et ce test a été retourné
plutôt que supprimé.

#### Les tests

Écrits d'abord, et rouges sur le code d'avant :
`TestImageEntriesAreSharedBetweenContexts`,
`TestAScopedImageCacheIsFoldedBackIntoOne`,
`TestTheNewerScanWinsWhenTwoContextsHoldTheSameImage`,
`TestTheFoldIsWrittenBackOnTheFirstOpen`,
`TestAFlatImageCacheIsReadAsItStands`, `TestSuccessiveWritesAccumulate`,
`TestDeleteRemovesTheEntry`, et
`TestTheInventoryHidesAnotherContextsRepositoriesButNotItsImages` pour les deux
moitiés de l'asymétrie.

### 3.40 Un verdict global `UNKNOWN` sur un hôte parfaitement sain

Trouvé en vérifiant `net_check` à la main le 2026-08-23, contre
`example.com:443` sous Windows :

```
verdict: UNKNOWN
  resolve         OK      example.com resolves to 2 addresses
  icmp            UNKNOWN ICMP could not be probed
  tcp             OK      Port 443 accepted the connection in 12 ms
  tls-chain       OK      Chain verifies against the system trust store
  http            OK      Service answered with HTTP 200 over https
```

Neuf contrôles sur dix passent, et le verdict global dit `UNKNOWN`.

**Ce n'est pas un défaut de `Summarize`, et c'est ce qui rend l'entrée
intéressante.** `Verdict.severity()` classe délibérément Unknown **au-dessus** de
Warn, avec une raison écrite : « ne pas avoir regardé est pire qu'avoir regardé
et trouvé un défaut ». Sur un contrôle isolé c'est juste. Sur l'agrégat, ça donne
le mot le moins informatif de la table à un hôte dont on vient d'établir qu'il
répond.

**Ce qui a changé, c'est la fréquence.** L'ICMP demande des sockets bruts ;
`pro-bing` les obtient sous Linux, pas sous Windows sans privilèges. Donc sur la
machine de développement l'ICMP rend `UNKNOWN` **systématiquement**, et le
verdict global aussi — un mot qui ne varie jamais n'informe de rien. Dans le TUI
la table est juste à côté, ce qui limite les dégâts ; par MCP, un agent lit le
verdict global d'abord et peut s'arrêter là.

#### Trois pistes, aucune tranchée

1. **`Summarize` ignore un `Unknown` isolé quand un contrôle plus tardif a
   réussi.** Un `tcp` OK établit que l'hôte répond, donc l'`icmp` inconnu ne dit
   plus rien sur la joignabilité. C'est le plus juste et le plus délicat :
   « plus tardif » veut dire connaître l'ordre des étages depuis `Summarize`, qui
   ne le connaît pas aujourd'hui.
2. **L'ICMP rend `NotApplicable` plutôt qu'`Unknown` quand la plateforme ne peut
   pas sonder.** `NotApplicable` est déjà exclu de l'agrégat. C'est le plus
   simple, et c'est défendable : « cette machine ne peut pas poser la question »
   n'est pas la même chose que « la question est restée sans réponse ». Le risque
   est de masquer un vrai échec ICMP derrière la même case.
3. **Un second champ dans la sortie MCP** — le verdict des seuls contrôles qui
   ont pu regarder, à côté du verdict global. Le moins invasif, et le plus
   proche de deux réponses à une question, ce que ce dépôt refuse ailleurs.

La 2 est probablement la bonne, et elle demande de savoir distinguer « pas de
privilèges » de « pas de réponse » dans `stage_reach.go` — ce que `pro-bing`
rapporte différemment selon la plateforme, donc à vérifier avant de décider.

**Ce n'est pas dans §1.3.** Le comportement est celui que le code déclare, avec
sa raison écrite ; c'est une décision à revisiter, pas un défaut à corriger — le
statut de D35.

### 3.41 Se passer de `netshoot` — ce qui se réécrit en Go, et ce qu'on abandonne

À analyser. L'image `nicolaka/netshoot` (`network.tool_image`) est la dernière
dépendance de DevDesk à un conteneur pour des fonctions qui ne sont pas Docker.
La question est de savoir ce qu'il resterait si elle disparaissait.

#### Le périmètre réel est plus petit qu'il n'en a l'air

§3.33 a déjà fait la moitié du travail sans le formuler ainsi : DNS, ICMP, TCP,
TLS et HTTP sont partis dans `internal/netcheck`, qui répond depuis le process
DevDesk. `RunPing`, `RunDNS`, `RunNetcat`, `RunCurl` et `RunSSLCert` **n'existent
plus** — il n'y a pas de code mort à supprimer.

Il reste **trois sites d'appel en production**, pour deux fonctionnalités :

| Site | Fonction | Ce que le conteneur apporte |
|---|---|---|
| `netdiag/run.go:71,73` | `RunTraceroute`, `RunTCPTraceroute` (`H`) | sockets bruts + `traceroute`/`tcptraceroute` |
| `netdiag/ports_model.go:63` | `RunSS` (onglet Ports) | `ss -tupan` avec `--net=host --pid=host --privileged` |
| `netdiag/ports_model.go:70` | `KillProcess` (`K`) | `kill -9` dans le même conteneur privilégié |

Soit 197 lignes en tout (`docker/netdiag.go` 63, `docker/ports.go` 134).

#### L'argument principal n'est pas la dépendance, c'est la justesse

`--net=host` sur Docker Desktop est le namespace **de la VM**, pas celui de la
machine. Le dépôt le dit déjà — c'est la raison écrite en tête de `runDiagHost`,
et c'est pourquoi §3.33 a rapatrié les cinq autres sondes.

**La même phrase s'applique à `RunSS` et à `KillProcess`, et personne ne l'a
tirée.** Si elle tient, alors sous Windows et macOS l'onglet Ports liste les
sockets de la VM Linux et non ceux de la machine, et `K` tue un processus de la
VM. Ce serait un défaut silencieux et exactement du genre que ce dépôt classe en
§1.1 — une vue qui répond à côté sans rien dire.

**À vérifier avant tout le reste**, et c'est peu coûteux : ouvrir `:net`,
onglet Ports, sous Windows, et chercher un port que seul l'hôte écoute (le
serveur de développement d'un projet, par exemple). S'il n'apparaît pas,
l'analyse change de nature : ce n'est plus « peut-on se passer de l'image »
mais « il faut s'en passer ».

#### Ce qui se réécrit, et avec quoi

**`gopsutil/v4` est déjà une dépendance directe, et `gopsutil/v4/net` est déjà
importé** par `internal/metrics/host.go`. Le remplacement de `ss` ne coûterait
donc **aucune dépendance nouvelle** et probablement rien sur le binaire.

`net.ConnectionStat` porte `Laddr`, `Raddr`, `Status` et `Pid` — tout ce que
`PortInfo` a besoin sauf le *nom* du processus, que `gopsutil/v4/process` donne
depuis le PID. Les implémentations sont :

| Plateforme | Comment | Sous-processus |
|---|---|---|
| Windows | `GetExtendedTcpTable` / `GetExtendedUdpTable` (iphlpapi) | non |
| Linux | `/proc` | non |
| macOS, FreeBSD | `lsof -i tcp -i udp` | **oui** |

macOS est donc le cas à assumer : `lsof` est livré avec le système, donc ce n'est
pas une dépendance à installer, mais c'est un exec là où les deux autres
plateformes font un appel système. À mettre en balance avec ce qu'on retire — un
conteneur privilégié.

**`KillProcess` est le cas facile, et il devrait partir même si rien d'autre ne
bouge.** `os.FindProcess` + `Kill()` est une ligne, sans conteneur, sans
privilège Docker, et — si l'hypothèse du namespace tient — il tue le bon
processus là où l'actuel tue celui de la VM. C'est un candidat à faire seul, en
premier, comme l'étape 1 de §3.23.

**Le traceroute est le seul qui résiste.** Il demande d'émettre avec un TTL
croissant et d'écouter les `ICMP Time Exceeded` en retour, donc un socket ICMP
brut. `golang.org/x/net/ipv4`/`ipv6` (déjà en dépendance indirecte) exposent ce
qu'il faut, mais :

- sous **Windows**, un socket ICMP brut demande l'élévation ;
- sous **Linux**, il demande root ou `CAP_NET_RAW` — les sockets ICMP datagram
  non privilégiés existent mais dépendent de `ping_group_range` ;
- `pro-bing`, déjà là, fait le ping et **pas** le traceroute.

Et la règle que DevDesk s'est donnée est écrite dans `netcheck/env.go` :
« DevDesk must not need root ». Un traceroute en Go la contredit sur au moins
deux plateformes sur trois.

#### Ce qu'on abandonnerait : le traceroute, et rien d'autre

C'est la réponse à la question posée. Trois options, par ordre de préférence
provisoire :

1. **Garder l'image pour `H` seul.** L'image reste, mais elle ne sert plus qu'à
   une touche, et `RunSS`/`KillProcess` cessent d'en dépendre. Le réglage
   `network.tool_image` survit avec un nom qui redevient exact.
2. **Abandonner le traceroute.** `H` disparaît, l'image aussi, et
   `network.tool_image` avec elle. Ce qu'on perd est réel : quand un `net_check`
   échoue au TCP, la question suivante est « où ça s'arrête », et c'est la seule
   chose qui y répond. Mais elle y répond déjà **pour la VM et pas pour la
   machine** sous Docker Desktop, ce que `renderTraceHeader` affiche — donc sur
   deux plateformes sur trois on abandonnerait une réponse qui n'était pas à la
   bonne question.
3. **Traceroute en Go, qui échoue proprement sans privilèges.** Le plus de
   travail, et il faudrait décider quoi afficher quand ça refuse — ce qui est le
   même problème que §3.40, un mot qui ne varie jamais.

L'option 1 est probablement la bonne première étape, parce qu'elle ne demande de
renoncer à rien et qu'elle isole la question restante.

#### Ce que ça retirerait aussi

- `--privileged`, `--pid=host` et les trois montages de `/etc` disparaissent du
  code ; DevDesk cesse de demander à Docker des droits qu'il n'utilise que pour
  lire une table.
- **`ports_list` redeviendrait exposable en MCP** (§3.38 l'a écarté précisément
  parce que `RunSS` démarre un conteneur privilégié). Sans conteneur, l'argument
  tombe.
- L'onglet Ports fonctionnerait **sans Docker installé**, ce qui est aujourd'hui
  une condition pour voir ses propres ports.

#### À vérifier avant de planifier

1. L'hypothèse du namespace, ci-dessus. Tout en dépend.
2. Ce que `gopsutil` rend sur les trois plateformes pour un socket en écoute
   sans processus attribuable — la colonne PID est ce sur quoi `K` agit, et un
   PID vide ne doit pas donner une ligne qui prétend pouvoir être tuée.
3. Si le PID exige des privilèges pour les processus d'autres utilisateurs.
   `ss -p` les obtient parce que le conteneur est privilégié ; un DevDesk non
   élevé verra probablement moins. C'est un vrai renoncement possible, à mesurer
   plutôt qu'à deviner.

## 4. Existing plans

Detailed plans live in `.claude/plans/`. Two are outstanding:

- [`platform_compatibility_improvements.md`](../.claude/plans/platform_compatibility_improvements.md)
  — Docker-layer platform portability. Referenced from the old `todo.md`.
- [`dashboard-resources-plan.md`](../.claude/plans/dashboard-resources-plan.md)
  — §3.19, the dashboard skeleton and resource charts.
