# DevDesk Backlog

**Last Updated:** 2026-08-30

Open work for DevDesk: known defects, technical debt, and planned features.
Replaces the former `todo.md` at the repository root. Items completed there
(network topology visualisation, inter-container ping/curl) have been dropped
rather than carried over.

---

## 1. Known defects

**Rien d'ouvert.** D59, D62 et D63 sont fermés le 2026-08-28, et **D64** — la
boîte Health rendant les certificats dans le vocabulaire des moniteurs, signalé
et corrigé le même jour — avec eux. **D65**, l'inventaire `:sec` affirmant
« Nothing scanned yet » avant d'avoir lu ses caches, a été signalé et fermé le
2026-08-29, et **D66** — la colonne CI rendant les mots de GitHub au lieu de
ceux de l'interface — le même jour. **D67** — un scan de `ws` reprenant l'écran
à chaque dépôt terminé — est signalé et fermé le même jour encore, et **D68** —
un scan survivant à un changement de contexte et écrivant ses résultats dans le
cache du nouveau — avec lui. Les deux se lisent ensemble : D67 est ce qui rend
ordinaire de changer de vue pendant un scan, donc ce qui rend D68 atteignable.
**D69** — le dashboard comptant les CRITICAL de dépôts supprimés depuis leur
scan, que ni `ws` ni `:sec` ne pouvaient montrer — est signalé et fermé le
2026-08-30. [§1.3](#13-open) est vide pour la première fois depuis D12, et tout
ce qui suit est en [§1.1](#11-fixed).

Trois défauts d'une même famille ont été fermés les 2026-08-23 et 2026-08-24, et
ils se lisent ensemble. Il n'y a plus un seul `--network host` dans
l'application : ce qui interroge cette machine tourne dans ce process, et le seul
conteneur qui reste interroge un réseau Docker, ce qu'aucun process hôte ne peut
faire. **D55** — l'onglet Ports listait les sockets de la VM
Docker — est fermé par
[§3.43](#343-longlet-ports-lit-la-machine--internalports). **D57 était le même
défaut dans l'onglet Topology**, où il était total : pas une interface ni une
route en commun avec la machine. **D58** a été trouvé en lisant le code de D57 :
un échec partiel y faisait afficher « aucune route », « aucun voisin » et
« aucune chaîne ». Les deux sont fermés par
[§3.44](#344-longlet-topology-devient-longlet-interfaces--done), qui a supprimé
trois sections sur quatre plutôt que de les traduire, et déplacé la question de
la route dans le pipeline de Diagnostics — puis par
[§3.47](#347-la-trace-de-route-est-supprimée-et-networktool_image-avec--done),
qui a supprimé la trace de route et avec elle le dernier `--network host`.

**D56** est fermé depuis le 2026-08-25 par
[§3.50](#350-un-fichier-de-règles-gitleaks-est-monté-et-un-scan-qui-na-rien-lu-nest-plus-propre--done),
et ce qu'il a coûté n'est pas ce qu'on croyait : le montage manquant n'était que
la moitié, l'autre étant une tolérance écrite pour un comportement que gitleaks
n'a pas.

**D59** était le plus ancien des trois : un scan lancé sur une arborescence
oubliait les dépôts situés plus bas que trois niveaux, et ceux derrière un lien
symbolique, sans jamais dire combien il en écartait. La garde de cycle qu'il
fallait pour suivre les liens ne pouvait pas passer par les chemins résolus —
`filepath.EvalSymlinks` ne résout pas une jonction Windows — donc elle passe par
`os.SameFile`.

**D62** n'avait rien à voir avec les précédents : il était dans la vue
configuration, pas dans ce qu'elle configure. Trouvé en cherchant pourquoi
`scan.plumber_config` semblait ignoré par plumber — il ne l'était pas, il
n'avait jamais été écrit. Le routeur a gagné le point de sortie qu'il n'avait
pas (`LeavingView`), et `esc` fait enfin quelque chose.

**D63** était une famille plutôt qu'un défaut : une touche annoncée qui n'agit
pas. Ce qui la ferme est d'avoir **créé la relation qui manquait** entre les
deux moitiés — une action s'annonce désormais par la constante que le handler
teste, jamais par sa lettre — et trois tests de source refusent le reste. Le
chiffre que l'entrée disait « inconnu, et c'est le problème » est maintenant
zéro pour ce qui est vérifiable, et déclaré hors de portée pour ce qui ne l'est
pas.

D39, before them, was the registry browser addressing a group's
members one way to browse them and another way to pull them; it is closed by
[§3.18](#318-a-registry-member-is-an-address-not-a-url--repo_prefix), which is
what it existed for. D40, found the same day and on the same screen, was the
thing §3.18 blocked on and had already been closed on its own.

D1 through D61 are all fixed or, in D35's case, deliberately
downgraded to a stale reading with a way to refresh it. §1.1 records what each was and why the
chosen fix was the right one — including the three that were answered by
*removing* something rather than making it work: D8's write-only CRUD flags,
D21's unreachable clamp and D36's never-filled cache.

The five that stayed open longest — D4, D8, D9, D10 and D11 — were parked not
because they were hard but because each altered something the user already saw,
so they needed a deliberate call rather than a drive-by fix. All five were then
decided together and fixed in one pass; see [§1.2](#12-the-five-parked-defects).

### 1.1 Fixed

**D69 — le dashboard comptait les CRITICAL de cibles supprimées, que ni `ws` ni
`:sec` ne pouvaient montrer. Corrigé.** Signalé et fermé le 2026-08-30.

Le rapport tenait en une phrase : « le dashboard m'affiche 3 crit pour les
workspaces mais je ne les vois pas ni dans `ws` ni dans `sec` ». Les trois
écrans lisent le même fichier — `~/.devdesk/cache/workspace-scans.json` — et
deux d'entre eux écartaient ce que le troisième additionnait.

Un cache de scan survit à ce qu'il décrit. Un `rm -rf` fait hors de
l'application n'est observé par personne, et rien ne retire l'entrée : elle
nomme un chemin qui n'existe plus, pour toujours. `:sec` le savait et
réconciliait à la lecture (`isGone`) ; `ws` ne listait de toute façon que le
disque. `readPosture`, cinquième lecteur des deux mêmes caches, sommait tout.

```go
// avant — readPosture
for _, entry := range workspaces.GetAll() {
    p.Repositories.add(entry.Critical, entry.Sensitive, entry.ScannedAt)
}
```

Ce qui rend le défaut coûteux n'est pas l'écart de trois : c'est **le seul
écran à annoncer le nombre était le seul où l'on ne peut pas aller voir**. Le
dashboard ne détaille rien ; il envoie vers `:sec`, qui montrait quatre
CRITICAL de moins sans rien pouvoir en dire. Un compteur qu'aucune vue ne sait
détailler n'est pas un compteur.

**Et il y a un troisième lecteur.** `internal/mcp` (`scan_inventory`) tenait sa
propre copie de toute la règle — la couture `listImages`, le `localImages`, le
`isGone` — avec des commentaires qui se répondaient d'un fichier à l'autre. La
règle était donc écrite deux fois, et absente à l'endroit où elle manquait. Ce
n'est pas une coïncidence : une règle recopiée est une règle que le lecteur
suivant ne trouve pas.

Chacune a une maison, et le partage suit ce dont la chose a besoin :

| | Où | Pourquoi |
|---|---|---|
| `RepositoryGone(path)` | `internal/cache` | pure — `os.Stat`, `os.IsNotExist`, rien d'autre |
| `ImageGone(name, present, known)` | `internal/cache` | pure — l'appelant fournit l'énumération, le cache ignore le démon |
| `docker.ImageNames()` | `internal/docker` | l'appel au démon, et le `(nil, false)` qui garde tout |

`os.IsNotExist` et rien d'autre : un partage lent ou un droit manquant n'est pas
une suppression, et écarter là-dessus ferait disparaître du compteur des dépôts
bien présents.

`docker.ImageNames` est un **var**, et c'est la couture de test que les trois
paquets déclaraient chacun pour soi. `internal/mcp` garde la sienne
(`listImages`) parce qu'`images_list` projette l'`Image` entière — taille, âge,
conteneurs — ce qu'un ensemble de noms ne dit pas.

Effet de bord, et il va dans le bon sens : `unscanned` était faux aussi. Un
dépôt supprimé compensait exactement un dépôt jamais scanné dans
`uncovered(inventaire, Targets)`, donc la boîte annonçait `0 unscanned` d'un
ensemble où il en restait à scanner. Le plancher à zéro ne sert plus que du côté
des images.

**La moitié « images » est corrigée avec elle.** Un `docker rmi` après un scan
laissait ses CRITICAL dans l'arbre Images de la même façon. Ce qui restait à
trancher était l'appel au démon : `readPosture` ne lisait que des fichiers.
L'asymétrie retenue est celle de Rule 110 — l'énumération est faite par le Cmd
et passée en paramètre, `os.Stat` reste dedans. Lire un chemin est de la même
nature que lire les deux fichiers de cache ; interroger un service qui peut être
arrêté ne l'est pas. Cela coûte un `docker image ls` par tour lent, sur
l'horloge qui fait déjà un `docker system df`, et cela rend `readPosture`
testable sans toucher à la couture.

Le garde compte double ici. Un démon éteint garde toutes les entrées : sans lui
la boîte Images afficherait `0 CRITICAL`, et sur un écran qui ne liste rien
ligne par ligne, c'est la seule mauvaise réponse que personne n'irait vérifier.

**D68 — un scan qui survivait à un changement de contexte écrivait ses résultats
dans le cache du *nouveau* contexte. Corrigé.** Trouvé en écrivant le registre
de travaux, corrigé le 2026-08-29.

Le nom du contexte était lu **dans le Cmd**, à la fin du scan :

```go
// avant — dans scanOneRepoCmd, après le retour de Trivy
wc, cErr := cache.NewWorkspaceScanCache(config.CurrentContextName())
```

Le cache des dépôts est scopé par contexte, et à raison : deux contextes
pointent légitimement vers des racines de workspaces différentes
(`internal/cache/scan_context_test.go`). Un `A` sur douze dépôts tourne des
minutes ; changer de contexte pendant ce temps est une chose ordinaire à faire
précisément parce que le scan tourne en fond — c'est ce que le poste 1 vient de
rendre possible. Les résultats qui atterrissaient ensuite étaient écrits sous le
contexte affiché à l'instant où Trivy a rendu la main.

**Le dommage est silencieux et double.** Le contexte de lancement perd des
comptes qu'il a demandés — la ligne reste à `-` et se rescanne indéfiniment. Le
contexte d'arrivée en gagne qu'il n'a pas demandés, sur des chemins qui peuvent
ne rien vouloir dire chez lui. Rien ne le signale : les deux écritures
réussissent.

Trois écrivains partageaient le défaut, et le troisième aggravait le premier :

| Écrivain | Ce qu'il écrivait |
|---|---|
| `scanOneRepoCmd` (`ws`) | les comptes d'un dépôt scanné |
| `storeRescan` (`:sec`) | les comptes d'un dépôt rescanné depuis l'inventaire |
| `deleteScanCacheCmd` (`ws`) | la purge de `ctrl+a`, sur sa **propre** goroutine |

Le dernier est le plus vicieux : la purge et le scan qui la remplace partaient
dans le même `tea.Batch`, chacun lisant le contexte pour lui-même. Rien ne
garantissait qu'ils lisent le même — la purge pouvait vider un contexte pendant
que le scan remplissait l'autre.

#### Ce qui corrige

`Run.Context` existait déjà : le routeur l'estampe au lancement (D8, poste 2).
Ce qui manquait est qu'il n'allait nulle part — la course était enregistrée sous
le bon nom pendant que son travail en lisait un autre. `jobs.StartMsg.Work`
devient donc un **constructeur** :

```go
Work func(contextName string) tea.Cmd
```

Le routeur l'appelle dans `Update`, avec le nom qu'il vient d'estamper. Le nom
est lu **une fois**, et chaque commande du lot partage la chaîne — un changement
trois dépôts plus tard ne peut plus l'atteindre. `jobs.Start` reste pour le
travail qui n'en dépend pas, et un site de lancement qui n'a pas besoin du nom
se lit comme avant.

Ce que ça donne aux sites de lancement : `purgeAndScan` dans `ws`, et un seul
constructeur dans `:sec`, où la purge et le rescan partent ensemble. **La purge
ne peut plus viser un autre contexte que le scan qui la remplace** — non pas
parce qu'on y a fait attention, mais parce qu'ils n'ont plus qu'un seul nom
entre eux.

#### Ce qui n'est délibérément pas corrigé

Trois lectures de `CurrentContextName()` restent dans les vues, et elles sont
justes :

| Lecture | Pourquoi elle est juste |
|---|---|
| `loadScanCacheCmd`, `loadInventoryCmd` | un **chargement** montre le contexte courant, par définition |
| l'en-tête de `:sec`, le titre de la configuration | ils *nomment* le contexte courant |

La distinction est celle-là et pas « dans un Cmd ou pas » : ce qui doit porter
l'estampe est ce qui **écrit** un résultat que quelqu'un a demandé sous un nom.

Le cache des **images** n'est pas concerné : il n'est pas scopé, et ne pas
l'être est délibéré (§3.39) — sa clé est une référence Docker locale, qui répond
de la machine et non d'un contexte. Les résultats complets ne le sont pas non
plus, dans les deux cas : ils sont adressés par chemin ou par nom d'image.

Les deux tests de régression construisent la situation exacte — écrire sous
`launched-in` pendant que le contexte courant est `switched-to` — et vérifient
les **deux** moitiés : le contexte de lancement a l'entrée, celui d'arrivée n'a
rien. Les deux échouent sur l'ancien code.

**D67 — un scan de `ws` ramenait la vue `ws` à chaque dépôt terminé, rendant
l'application inutilisable pendant tout le lot. Corrigé.** Signalé à l'usage le
2026-08-29, corrigé le même jour.

`handleWorkspaceScanComplete` posait `a.currentView = command.ViewWorkspaces` à
chaque `WorkspaceScanCompleteMsg`. Un `A` sur une arborescence de douze dépôts
émet douze messages étalés sur la durée du lot : partir au dashboard rendait la
main à `ws` au dépôt suivant, et ainsi de suite jusqu'au dernier. **Plus le scan
est long, plus longtemps l'utilisateur est retenu sur la vue qu'il regarde
justement parce qu'elle n'a rien de neuf à montrer.**

Le garde `if a.currentView != command.ViewSecurity` en dit l'histoire : la vue
security a été exemptée quand le vol lui a coûté un spinner bloqué (son propre
scan se terminait dans la mauvaise vue). L'exemption traitait le symptôme dans
la seule vue où il avait été observé, alors que le défaut était le vol lui-même.

#### Deux conséquences que le vol masquait

Elles étaient là depuis le début et invisibles, précisément parce que `ws` était
toujours ramenée à l'écran :

| Message | Ce qu'il devenait hors de `ws` |
|---|---|
| `WorkspaceScanStartingMsg` | absent du switch du routeur : servi par `forwardToActiveView`, donc **remis à la vue courante** et perdu |
| `WorkspaceSyncStartingMsg` / `CompleteMsg` | idem — un sync fini ailleurs laissait `syncingPaths` marqué et le `syncRun` sans sa ligne |
| `EntryDeletedMsg` | idem — un delete est confirmé dans une modale puis tourne seul, et son marqueur n'est levé que là |

Un dépôt marqué occupé et jamais démarqué tient sa ligne contre toute autre
action pour la vie de la vue, ce que `busy()` refuse avec le bon message pour la
mauvaise raison.

#### Le correctif

`routeToView(target, msg)` remplace les deux helpers qui faisaient déjà cela
pour `oci` et `:sec` (`routeToOCIImagesView`, `routeToSecurityView`) : le
message est remis à la vue **qui a lancé le travail**, sans toucher à
`currentView`. Le switch du routeur regroupe les huit messages de travail long
sous un même en-tête, et la vue security n'a plus besoin d'exemption — plus
personne ne lui prend l'écran.

Ce qui rend le correctif sûr est que `setEntries` ne touche pas
`scanningPaths` : le rechargement que provoque le retour dans la vue
(`Init` → `EntriesLoadedMsg`) laisse en place ce qui tourne encore.
`TestAReloadKeepsWorkThatIsStillRunning` le fige, parce que c'est désormais la
seule chose entre un scan et une ligne marquée pour toujours.

**Ce que ce correctif ne fait pas** : la chaîne `spinner.Tick` de `ws` s'arrête
quand la vue quitte l'écran, donc son spinner ne tourne pas en son absence — il
repart au retour, par `Init`. C'est sans conséquence visible (personne ne
regarde) et c'est le [§3.58](#358-un-registre-de-travaux-et-la-vue-jobs--done) qui le
règle proprement, en faisant tenir **une** chaîne par le routeur. Le compteur
par dossier et la vue `jobs` demandés en même temps y sont aussi : ce défaut-ci
est livré seul parce qu'il l'est.

`TestLongRunningWorkReportsWithoutTakingTheScreen` parcourt les huit messages
depuis les quatre vues concernées — trente-deux cas dont le seul énoncé est
« l'écran ne bouge pas, et le message arrive quand même ».

**D66 — la colonne CI d'un contexte GitHub affichait `in_pr…`, et un build cassé
en orange. Corrigé.** Signalé à l'usage le 2026-08-29 sur le dépôt `devdesk`
lui-même, corrigé le même jour.

`Repository.CIStatus` documente un vocabulaire — « success », « failed »,
« running »… — et `lastWorkflowConclusion` affirmait en commentaire le rendre
« in the same words the GitLab backend reports ». Il rendait ceux de GitHub. Sur
les neuf issues qu'Actions produit, **deux seulement** s'écrivent pareil :

| GitHub | Ce que la colonne faisait |
|---|---|
| `success`, `skipped` | ✅ correct, par coïncidence orthographique |
| `failure` | `failu…` **en orange** |
| `cancelled` | `cance…` (GitHub met deux `l`) |
| `in_progress` | `in_pr…` — ce que l'utilisateur a vu |
| `queued`, `timed_out`, `neutral`, `action_required`, `stale` | brut, tronqué à six cellules |

**Le pire n'est pas la troncature, c'est la couleur.** `pipelineStatusStyle`
peint son `default:` en `StatusWarningStyle` : un build cassé se lisait comme
une mise en garde, dans la seule colonne qui existe pour qu'un build cassé se
voie. Le rouge n'était atteint que par le mot `failed`, que GitHub n'écrit
jamais.

#### Pourquoi rien ne l'a vu

Le contrat n'était qu'un commentaire, et **deux tests figeaient le défaut comme
intention** : `TestARunningWorkflowReportsItsStatus` affirmait
`CIStatus == "in_progress"` — le mot de GitHub, pris pour celui de
l'interface — et `TestPipelineStatusLabels` portait la ligne
`{"scheduled", "scheduled"}, // unknown statuses fall through verbatim`.

#### Le correctif

Le vocabulaire devient une liste (`internal/forge/cistatus.go`) plutôt qu'une
phrase : onze constantes et `CIStatuses()`, faite pour être parcourue par un
test. Le backend GitHub traduit dessus (`ciStatusOf`), et **une valeur inconnue
rend la chaîne vide** — ce que `Repository.CIStatus` documentait déjà pour un
backend sans équivalent, et la différence entre une cellule vide et six
caractères de l'orthographe interne d'une plateforme.

Quatre issues GitHub n'ont pas de correspondant et sont repliées, chacune sur
son argument : `timed_out` et `startup_failure` sur `failed` (GitLab tue un job
en dépassement de temps en `failed`, donc c'est ce que GitLab aurait rapporté) ;
`action_required` sur `manual` ; `neutral` et `stale` sur `skipped`, le seul
gris que ce vocabulaire possède.

#### Le second défaut, trouvé en écrivant le garde

En parcourant `CIStatuses()` depuis la vue, `created` et `scheduled` — deux
statuts que **GitLab** produit — tombaient eux aussi dans le `default:` et
imprimaient leur propre nom. Le défaut n'était donc pas propre à GitHub : il
était propre à un contrat que personne ne pouvait vérifier. Les deux rejoignent
les trois autres façons qu'a un pipeline de n'avoir pas démarré.

Les deux tests qui manquaient : `TestTheBackendOnlyEmitsDeclaredStatuses`
refuse une sortie que `CIStatuses()` ne déclare pas ;
`TestEveryDeclaredCIStatusHasAnIcon` refuse un statut dont la vue n'a pas de
glyphe. Le `default:` de la vue survit aux deux — il est inatteignable pour un
backend qui tient sa promesse, et c'est ce que doit heurter un backend qui ne la
tient pas.

**D65 — l'inventaire `:sec` affirmait « Nothing scanned yet » avant d'avoir lu
les caches, puis affichait la liste. Corrigé.** Signalé à l'usage le
2026-08-29, corrigé le même jour.

Le message n'était pas faux par accident : il était affirmé sur une question
non encore posée. `renderInventoryView` branchait sur `len(Items()) == 0`, et
une table vide recouvre deux faits opposés — « les deux caches ont répondu,
il n'y a rien » et « `loadInventoryCmd` est encore en vol ». La vue n'avait
aucun moyen de les distinguer parce que rien dans le modèle ne portait la
différence.

C'est Rule 139 dans l'autre sens. La règle dit deux choses, et seule la
première était tenue ici :

| La règle | L'inventaire `:sec` |
|---|---|
| le corps ne se remplace pas par un spinner | ✅ tenu — il n'y en avait pas |
| le message vide est **conditionné à la fin du chargement** | ❌ absent |
| le chargement se dit au footer, avec un spinner | ❌ absent |

**Le correctif est un champ, pas un rendu.** `inventoryLoading` est vrai dès la
construction — `Init` charge inconditionnellement, donc un modèle qui naîtrait
en « pas de chargement » rendrait le message le temps d'une frame avant même
que le premier `Cmd` ne parte — et faux quand `InventoryLoadedMsg` arrive.
`renderInventoryView` garde alors la table à l'écran, en-tête et colonnes
compris, et `status()` renvoie `Loading scan inventory...` avec spinner.

#### La chaîne du spinner s'arrêtait avant d'avoir servi

Trouvé en écrivant le test, pas à la lecture. `handleSpinnerTick` retournait
`m, nil` dès que rien n'était en cours de scan : le `Tick` émis par `Init`
mourait au premier passage, donc un spinner de chargement serait resté sur la
frame zéro — ce qui se lit comme un blocage, exactement ce que Rule 139 dit
d'éviter.

Les deux conditions sont maintenant une seule fonction, `spinnerAlive()` —
`inventoryLoading || inventoryScanning()`. Elle est lue aux deux endroits qui
décident de la chaîne : `handleSpinnerTick` pour programmer la suivante,
`spinnerTickIfIdle` pour refuser d'en démarrer une seconde. Les poser
séparément est précisément ce qui ferait tourner les frames à double vitesse
quand un rescan démarre pendant un chargement.

`reloadInventory` prend son tick **avant** de lever le drapeau, pour la même
raison : `spinnerTickIfIdle` lit `spinnerAlive`, donc lever d'abord lui ferait
répondre « déjà en route » à propos de la chaîne que ce rechargement essaie de
démarrer.

#### Pourquoi aucun test ne l'avait vu

`inventoryModel`, l'assembleur de tous les tests de la vue, nourrit un
`InventoryLoadedMsg` avant de rendre quoi que ce soit. Il produisait donc
toujours un modèle ayant déjà chargé, et l'état signalé n'existait dans aucune
fixture. `TestNothingScannedYetWaitsForTheCachesToAnswer` part de `New` et de
la seule `WindowSizeMsg`, ce qui est ce que le routeur fait à l'ouverture ;
`TestTheEmptyMessageAppearsOnceTheCachesAnswer` tient l'autre moitié, sans
quoi une vue qui ne quitterait jamais le chargement passerait le premier.


**D64 — la boîte Health rendait les certificats dans le vocabulaire des
moniteurs, et y perdait l'état qui demande une action. Corrigé.** Signalé à
l'usage le 2026-08-28, corrigé le même jour.

L'arbre `Certs` portait `up` / `down` / `error`, les trois nœuds de `Monitors`,
alimentés par le même `countStatuses`. Deux choses en sortaient fausses :

| Ce qui est vrai du certificat | Ce que SSLChecker rend | Où il atterrissait |
|---|---|---|
| valide, loin de l'échéance | `OK` | `up` |
| expire dans 30 jours ou moins | `WARNING` | `error` |
| expire dans moins de 7 jours | `ERROR` | `error` |
| **périmé** | `ERROR` | `error` |
| hôte injoignable, chaîne vide, cible absente | `DOWN` / `ERROR` | `down` / `error` |

Le nœud `error` recevait donc trois faits qui ne se règlent pas de la même
façon — un service déjà cassé, un renouvellement qu'il est encore temps de
faire, et une mesure qui n'a pas eu lieu — et `up` nommait « joignable » ce qui
veut dire « valide ». **L'état sur lequel on décide quelque chose était le seul
à n'avoir jamais de ligne à lui.**

**La cause n'est pas le rendu, c'est `StatusType`.** Elle a quatre valeurs pour
tout le monde et n'en garde que trois utiles ici ; un certificat en a quatre à
dire. `status.CertState` les dérive de `SSLDaysLeft` — dont l'absence est
exactement le cas « rien n'a pu être lu » — et l'arbre en rend une par nœud :
`valid`, `to renew`, `expired`, `error`. Le seuil de renouvellement est la
constante que SSLChecker utilisait déjà, désormais nommée
(`status.CertRenewWindowDays`) : deux seuils qui divergent feraient dire
`to renew` au dashboard de ce que `:status` affiche encore en vert.

**Trouvé en corrigeant, et de la même famille :** le nœud `expiry` prenait le
minimum de *tous* les jours restants, négatifs compris, donc un certificat
périmé depuis deux jours s'y lisait `-2 days`. Il ne regarde plus que ce qui
court encore ; rien devant se lit `-`, et le compte des périmés est au nœud
au-dessus.

**`:status` avait le même défaut, en plus discret.** Son onglet Certificates
rendait `formatSSLStatus` depuis `StatusType` : un certificat périmé et un qui
expire dans six jours y portaient la même icône, et la colonne `Days Left`
d'à côté était le seul moyen de les séparer — deux cellules à lire pour un fait
qui en tient dans une. Elle passe par `CertStateOf`, et sa couleur par
`certStatusStyle` plutôt que par `componentStatusStyle` : `to renew` n'a pas de
`StatusType` à donner à `theme.StatusStyle`, et un certificat périmé y prendrait
la couleur de celui qu'on n'a pas pu lire.

Le glyphe et la couleur de chaque état sont dans le thème
(`theme.CertStateIcon`, `theme.CertStateStyle`), appelés par les deux vues :
deux tables de correspondance finiraient par diverger sur la seule qui compte,
celle qui sépare `expired` de `error`.

---

**D63 — une touche annoncée qui n'agit pas. Fermé, la famille avec.** Les deux
instances trouvées le 2026-08-27 en câblant `o` sur l'onglet CI, l'une par
lecture du code, l'autre par l'utilisateur ; la garde posée le 2026-08-28.

Rule 130 interdit **une touche grisée qui agit quand même**, et
`shortcut.Availability` rend ça inexprimable : un seul champ, deux lecteurs.
Rien ne gardait le sens inverse — **une touche affichée qui n'agit pas** — et la
vue security en portait deux, dans le même état :

| Annoncé | Ce que le handler liait |
|---|---|
| `{Key: "o", Description: "Open ref"}` | `case keymap.Web:` c'est-à-dire `W` |
| `{Key: "esc/⌫", Description: "Back"}` | `case "esc":` seul — le commentaire disait d'ailleurs « backspace **was** an alias » |

Dans les deux cas la touche affichée ne faisait rien, et celle qui marchait
n'était jamais montrée.

**Pourquoi aucun test ne les a vues, et pourquoi un test naïf n'y arriverait pas
non plus.** Les tests existants épinglaient *les deux moitiés de la
contradiction séparément* : `TestShortcutsFollowTheState` affirmait que la
colonne dit `o`, `TestScanDetailsOpensTheReference` pressait `keymap.Web` — les
deux passaient, rien ne les rapprochait.

Et la raison de fond est structurelle : **la touche annoncée est une chaîne
d'affichage, la touche liée est un nom de touche bubbletea**, et il n'existe
aucune relation mécanique entre les deux. `↑↓` se lie par `case "up"`, `esc/⌫`
par `case "esc"`. Un relevé grossier sur `internal/ui` sortait 52 candidats dont
l'écrasante majorité étaient des libellés d'aide (`Context`, `Images`,
`Disk Usage`).

#### Ce qui la ferme : créer la relation là où elle peut exister

C'est ce que l'entrée annonçait, et c'est ce qui a été fait. **Une action
s'annonce par sa constante**, jamais par sa lettre — 56 `{Key: "S"}` sont
devenus `{Key: keymap.Scan}` dans neuf vues, `GetShortcuts` et `GetHelpContent`
confondus, puisqu'une aide qui ment ment autant qu'une colonne.

Trois tests dans `announced_test.go`, à côté de ceux de §3.26 :

| Test | Ce qu'il refuse |
|---|---|
| `TestNoAnnouncementSpellsAnActionOutInFull` | `{Key: "S"}` — c'est la moitié qui rend les deux autres possibles : `"S"` est une chaîne comme un libellé d'aide, `keymap.Scan` est le token du handler |
| `TestEveryAnnouncedActionIsBoundInItsPackage` | une vue qui affiche `S Scan` sans que rien dans son paquet ne réponde `keymap.Scan` |
| `TestEveryAnnouncedToggleIsBoundInItsPackage` | la moitié minuscule, qui n'a pas de constante mais reste **une lettre** — donc la comparaison y est exacte plutôt que bruyante. C'est celui qui attrape l'instance rapportée |

**Une annonce se reconnaît à porter `Key` *et* `Description`**, pas à son nom de
type : `shortcut.Shortcut` et `help.KeyBinding` ont la paire tous les deux, un
élément dans un littéral de slice n'a pas de type à lire, et
`shortcut.HeaderInfo` — la seule autre chose qui a un `Key` — porte `Value` à la
place. Une `Key` écrite comme constante de paquet est résolue, ce qui rend
vérifiable le correctif appliqué à la main pour `o` (`openPipelineKey`) plutôt
que seulement soigné.

Les trois échouent sur les formes exactes du défaut, vérifié en les
réintroduisant une par une.

#### Ce qui n'est pas couvert, et pourquoi c'est le bon découpage

`↑↓`, `enter/esc`, `tab / shift+tab` sont des chaînes d'affichage sans
contrepartie à comparer. Rule 138 en tient déjà l'essentiel hors de l'écran, et
deviner là rendrait les 52 candidats — le chiffre que cette entrée citait comme
« inconnu, et c'est le problème » est désormais **zéro pour ce qui est
vérifiable, et hors de portée pour le reste** ; ce n'est pas la même chose que
de l'ignorer.

La granularité est le **paquet**, pas l'état de la vue : une touche liée dans un
onglet et annoncée dans un autre passe. Le savoir demanderait de piloter la vue,
ce qui est l'alternative que §3.28 a déjà pesée et écartée. Ce que ces tests
attrapent est la touche liée *nulle part* — ce qu'étaient les deux instances.

C'est le pendant de `internal/ui/keymap`, qui vérifiait qu'aucune vue ne *lie*
une touche hors vocabulaire sans jamais vérifier qu'elle *annonce* ce qu'elle
lie.

---

**D59 — un scan lancé sur une arborescence oubliait certains dépôts, en
silence. Fermé.** Signalé à l'usage, reproduit sur fixture le 2026-08-24, la
profondeur corrigée le jour même et le reste le 2026-08-28.

`S` sur un répertoire non-git scanne `entry.SubRepoPaths`, que le walk remplit.
**Deux causes, mesurées, indépendantes l'une de l'autre**, plus trois aggravants
de la même famille — chacun décidant qu'un manque se voit ou non.

**1. La profondeur était limitée à 3, et ce n'était écrit nulle part.** Un
`monorepos/client/2026/api/.git` était invisible pour `S`, `F` et `A` alors que
le répertoire, lui, se parcourait normalement à la main. C'est le « parfois » du
rapport : ça dépendait de la profondeur à laquelle les dépôts se trouvaient.

**La limite est partie, sans rien pour la remplacer**, parce que la question
était mal posée : ce qui borne le walk est qu'**il s'arrête à chaque dépôt qu'il
trouve**, donc le `node_modules` d'un dépôt n'est jamais parcouru. Cette coupe
faisait déjà tout le travail. Aucune liste d'exclusions par nom n'a été ajoutée :
ce serait réintroduire l'omission silencieuse que ce défaut *est*, et
`node_modules` peut contenir un dépôt — npm installe depuis git.

**2. Un lien symbolique ou une jonction vers un répertoire était ignoré.**
`e.IsDir()` vient de `os.ReadDir` et ne suit pas le lien :

```
entry a         IsDir=true   type=d---------  statIsDir=true
entry linked    IsDir=false  type=?---------  statIsDir=true   ← ignorée
```

`leadsToDir` répond pour le walk **et** pour `table.go`, qui affichait le lien
comme un fichier — ni parcourable, ni scannable, sans que rien ne dise pourquoi.
Le test est « pas un fichier ordinaire » plutôt que « est un lien symbolique »,
et c'est délibéré : Go rapporte une jonction Windows en `ModeSymlink` ou en
`ModeIrregular` selon la version, et `os.Stat` répond pareil dans les deux cas.
Un fichier ordinaire ne coûte aucun appel système.

#### La garde de cycle, et pourquoi elle n'est pas un chemin résolu

L'entrée annonçait « en suivant les inodes ou les chemins résolus ». **Les
chemins résolus ne marchent pas**, et c'est le seul endroit où la correction a
dû s'écarter du plan. Mesuré ici, sur une jonction :

| Appel | Réponse |
|---|---|
| `os.Readlink` | la cible |
| `filepath.EvalSymlinks` | **le lien lui-même** |
| `os.SameFile` | `true` |

Une garde par chemin résolu voit donc deux noms pour un même répertoire et ne se
déclenche jamais — vérifié : la première version de la correction a bouclé
jusqu'à ce que Windows refuse le chemin devenu trop long, ce qui *ressemble* à
une terminaison. Et la jonction est précisément ce contre quoi le défaut a été
rapporté.

`mayFollow` pose donc deux questions par `os.SameFile`, et **seul un lien les
paie** :

- **la cible est-elle sur le chemin par lequel on est venu** — c'est la boucle.
  Tous les répertoires traversés sont des préfixes du chemin du lien, donc la
  chaîne se reconstruit sans rien mémoriser ; la remontée va jusqu'à la racine
  du système de fichiers et non jusqu'à la base, parce qu'un lien *au-dessus* de
  la base y ramène la base ;
- **a-t-elle déjà été entrée par un autre lien** — ce n'est pas une boucle mais
  un doublon : les mêmes dépôts sous un second nom, donc un second scan de
  chacun.

#### Les trois aggravants

**Le `os.ReadDir` avalé** est logué et **compté**. `subRepoScan` porte `Skipped`
à côté de `Repos`, et c'est le plus important des trois : c'est ce qui décide
qu'un manque se voit.

**Le compte atteint l'écran par deux chemins**, et ils répondent à deux
situations différentes :

| Situation | Ce qui le dit |
|---|---|
| des dépôts ont été trouvés, l'action tourne | `warnSkipped` pose un `Warn` (Rule 128) — rien n'a échoué, la demande ne peut pas être honorée en entier |
| aucun dépôt trouvé, l'action est refusée | `reasonUnread` remplace `reasonNoScanTarget` |

Le second est le vrai correctif : « Not a git repository, and no repository
nested under it » est une **affirmation** que le walk n'a pas le droit de faire
quand il n'a pas pu tout lire. Ne pas savoir n'est pas savoir que non — c'est
Rule 130 dans son propre vocabulaire. C'est aussi la seule raison qui porte un
nombre, donc une fonction plutôt qu'une constante ; les autres disent la même
chose à chaque fois.

Un sync en lot le porte sur `syncRun.unreadable` et non comme message de footer :
la ligne du run est celle qui survit à la minuterie de trois secondes, et c'est
déjà là que le reste du bilan est rendu.

**Le dépôt *bare* n'était pas reconnu** — `HEAD`, `objects` et `refs` sont à la
racine, et il porte de l'historique, donc c'est une cible de scan comme une
autre. `holdsRepo` est la règle, et elle est lue dans **le listing du
répertoire** plutôt que demandée à `os.Stat`. Ce n'est pas un détail :

| Walk sur le cache de modules Go (le pire cas) | Temps |
|---|---|
| ancien walk, un `os.Stat(.git)` par répertoire | 75 ms |
| trois noms, trois `os.Stat` échoués par répertoire | **334 ms** |
| trois noms lus dans le `ReadDir` déjà fait | **65 ms** |

Le nouveau walk est donc **plus rapide que l'ancien tout en trouvant strictement
plus**. Le prix est un `ReadDir` sur la racine d'un dépôt, et un dépôt est
précisément là où le walk s'arrête.

Quatorze tests, dont trois qui n'auraient pas tourné sur la machine du rapport :
`os.Symlink` réclame `SeCreateSymbolicLinkPrivilege` sous Windows, qu'une
session ordinaire n'a pas — mais `mklink /J` n'en réclame aucun, et une jonction
est ce qu'il y avait dans le rapport. `linkDir` fait donc une jonction sous
Windows et un lien symbolique ailleurs, plutôt que de sauter sur la seule
plateforme qui compte ici.

`TestALinkPointingAtAnAncestorDoesNotLoop` est celui qui n'« échoue » pas sans
la garde : il ne rend pas la main.

---

**D62 — un champ texte de la vue configuration quitté sans bouger le curseur
n'était jamais écrit, et l'écran continuait d'afficher la valeur saisie.
Corrigé.** Signalé à l'usage le 2026-08-27, reproduit sur le log et le fichier
de contexte le jour même, fermé le 2026-08-28.

Saisi : `scan.plumber_config`. Ce qu'en disait le fichier après coup :

```
~/.devdesk/config-dev.yaml:199    plumber_config: ""
```

et les trois runs qui ont suivi, aucun ne portait `--config`.

**La cause tenait en une ligne : rien ne committait le champ focusé quand la vue
était quittée.** `commitFocused()` n'était appelé que depuis `switchTab` et
`moveField`, donc un champ texte n'était appliqué et persisté que par `tab`,
`shift+tab`, `↑` ou `↓`. Le log dit exactement ça :

```
14:10:22 Switching to view: configuration
14:10:44 Switching to view: workspaces        ← ctrl+p, sans avoir bougé le curseur
```

et le fichier est resté daté de 14:07.

**Ce qui le rendait durable plutôt que passager, et c'était le vrai défaut.**
Les vues sont mises en cache par le routeur et `configuration.Init()` ne fait
rien : au retour dans `:cfg`, `focusedField` et `m.input` étaient tels qu'ils
étaient, donc **le champ affichait toujours le chemin saisi pendant que le
fichier tenait l'ancienne valeur**. Rien à l'écran ne distinguait une valeur
écrite d'une valeur seulement tapée, et c'est ce qui a fait chercher le défaut
du côté du scan : la vue confirmait le réglage à chaque visite.

Ce n'était pas propre à plumber. Tout `kindText` et tout `kindInteger` des six
onglets était concerné, et le pire cas était `forge.url` — le commit porte
`saved{forgeChanged: true}` et le message qui dit de se reconnecter, donc le
quitter sans committer laissait une session ouverte contre une adresse que
l'utilisateur croyait avoir changée, sans le message.

#### Le choix qui restait à faire, et ce qui l'a tranché

L'entrée laissait deux réponses ouvertes pour un refus sur le chemin de sortie —
`commitFocused` peut refuser, et un refus n'a plus de curseur où retomber
puisque la vue s'en va :

1. le routeur gagne un point « cette vue est quittée » et un refus **annule le
   changement de vue** ;
2. le champ est committé à la sortie et un refus **abandonne la saisie** en le
   disant au footer.

**La 1, et pour une raison technique plutôt que par préférence : la 2 ne peut
pas dire ce qu'elle fait.** Le footer appartient à la vue (Rule 128), et la vue
est précisément ce qui quitte l'écran — le message expliquant la valeur
abandonnée partirait avec elle. Une réponse indicible est la même silence d'un
étage plus bas, c'est-à-dire ce défaut-ci.

#### Ce qui a été fait

**`app.LeavingView`**, une cinquième interface optionnelle du routeur :
`Leave() (tea.Model, tea.Cmd, bool)`. `switchView` l'appelle avant de changer de
vue, `switchContext` avant de changer de contexte — le second parce qu'il
reconstruit toutes les vues contre un autre fichier, donc une saisie non
committée y serait perdue *et* perdue contre la mauvaise config. Un `false`
annule le changement et **transmet le `Cmd` de la vue** à sa place, donc le refus
n'est pas silencieux. Réentrer dans la vue déjà à l'écran ne commit rien : `:cfg`
depuis `:cfg` n'est pas une sauvegarde.

La configuration est la seule vue qui l'implémente, et c'est normal — c'est la
seule qui garde une valeur dans un widget plutôt que dans son modèle jusqu'à ce
qu'une touche bouge le curseur.

**`esc` commit sans bouger.** Elle tombait dans la branche « tout le reste
appartient à l'input » et allait au `textinput`, qui l'ignore : la seule touche
qu'on essaie pour « fermer » un champ ne faisait rien du tout. Elle re-bind
l'input ensuite, donc `007` dans un champ entier devient le `7` réellement
stocké — distinguer une valeur écrite d'une valeur tapée est l'autre moitié de
ce qui rendait le défaut durable.

`esc` est annoncée bien que Rule 138 la range parmi les touches évidentes,
parce que ce qu'elle fait ici ne l'est pas ; et elle est **grisée là où elle ne
ferait rien** (Rule 130). `settlesOnBlur` est cette question, et c'est exactement
l'ensemble sur lequel `commitFocused` agit : texte, entier, `Forge` et
`Secret backend`. Une case et un champ à cycle ordinaire ont déjà écrit au
moment où le curseur pourrait partir — griser et ne rien faire coïncident, ce
qui est la forme que Rule 130 demande.

Neuf tests : les quatre du côté vue (`Leave` écrit, `Leave` porte les
conséquences de `forge.url`, un refus refuse de partir, `esc` écrit sans bouger,
`esc` montre ce qui est stocké, et le grisage par champ) et six du côté routeur,
dont **`TestAViewWithoutTheInterfaceIsLeftUntouched`** — le repli silencieux est
le comportement de toutes les autres vues et il mérite d'être tenu, c'est la
leçon de `TestEveryViewSuppliesItsHeaderAndHelp`.

---

**D56 — `scan.gitleaks_config` ne pouvait pas fonctionner en mode Docker, et
son échec se lisait « aucun secret ». Corrigé.** Trouvé en écrivant
[§3.42](#342-plumber--un-score-de-sécurité-de-pipeline-par-dépôt--done), qui a besoin
exactement du même réglage, vérifié à l'exécution le 2026-08-24 et fermé le
2026-08-25 par
[§3.50](#350-un-fichier-de-règles-gitleaks-est-monté-et-un-scan-qui-na-rien-lu-nest-plus-propre--done).

Le chemin hôte partait tel quel dans le conteneur, où le fichier n'était pas ;
gitleaks mourait avant d'avoir lu un octet, sortait en `1` avec stdout vide, et
`RunGitleaks` lisait ça comme « il a tourné et n'a rien trouvé ». Le dépôt
ressortait avec `SecretsScanned` à `true`, un `SecretVerdict()` non nul valant
`false`, et une icône verte dans `ws`.

**Ce que la correction a appris, et qui change l'entrée.** Le montage manquant
n'était que la moitié du défaut. L'autre moitié est la tolérance elle-même :
elle a été écrite pour un comportement que gitleaks n'a pas. Mesuré sur
v8.30.1 :

| Situation | code | stdout |
|---|---|---|
| dépôt propre | **0** | `[]` |
| secrets trouvés | 1 | le rapport |
| `--config` introuvable **ou** illisible | 1 | **vide** |

Un dépôt propre ne sort donc **jamais** en `1` avec un stdout vide : c'est le
rapport qui sépare un résultat d'un échec, et la branche qui avalait le second
n'avait aucun cas légitime. C'est ce qui rend la correction plus large que le
montage — elle couvre le fichier absent, le TOML malformé, et tout fatal qui
n'écrit pas de rapport, dans les **deux** modes.

**Pourquoi les tests ne l'ont pas vu.** `TestACustomGitleaksConfigIsPassedThrough`
n'exerçait que le mode binaire, là où ses trois voisins immédiats vérifiaient
les deux ; et il vérifiait que le drapeau était **présent**, jamais que le
chemin était **atteignable** — `--config /etc/gitleaks.toml` passe des deux
côtés et ne veut dire la même chose dans aucun des deux.

---

**D61 — la ligne sélectionnée s'arrêtait avant la bordure droite dès qu'une
colonne tombait à zéro. Corrigé.** Mesuré au rendu le 2026-08-25, pas déduit,
et fermé le même jour par
[§3.45](#345-datatable--chaque-colonne-déclare-sa-nature-et-les-largeurs-suivent--done).

`availableFor` soustrayait le padding de **toutes** les colonnes en amont du
calcul ; `contentWidth` (`render.go`) ne le compte que pour celles dont la
largeur est non nulle, et `headerLine` comme `rowLine` sautent les autres. Une
colonne évincée emportait donc ses deux cellules sans les rendre à personne.

| Table | Terminal | Intérieur | Ligne rendue, avant | Écart | Après |
|---|---|---|---|---|---|
| Interfaces | 80 | 78 | 74 | **4** | 78 |
| Interfaces | 84 | 82 | 82 | 0 | 82 |
| Ports | 88 | 86 | 84 | **2** | 86 |
| Ports | 96 | 94 | 94 | 0 | 94 |

L'écart valait exactement `2 × (nombre de colonnes à zéro)`, et il disparaissait
dès qu'aucune ne l'était. Il touchait toute table dont les colonnes fixes
saturent la largeur — Interfaces sous ~82 colonnes, Ports sous ~96 — donc pas
seulement les deux mesurées.

C'était Rule 116 en défaut : la règle existe pour que la ligne sélectionnée
atteigne la bordure droite du viewport, et elle ne le faisait pas aux largeurs
où une table évince. À ne pas confondre avec la falaise que décrivait
[§3.49](#349-net--k-dit-quand-elle-ne-peut-pas-et-les-adresses-se-séparent-par-famille--done) :
celle-là était le fait que `MinWidth` n'est pas un plancher, elle était
délibérée et elle était écrite ; ici c'était la largeur *rendue* qui ne
correspondait plus au viewport, ce qui n'a jamais été voulu. Les deux sont
tombées ensemble, et pour la même raison : une colonne se retire entière, donc
elle rend son padding.

**Ce qui le tient maintenant est la forme de l'invariant, pas une ligne de
code.** Il était énoncé sur la somme des largeurs, et cette somme était juste
pendant que la ligne était courte. Il est énoncé sur la **portée rendue** —
`datatable.RenderedWidth()`, qui est `contentWidth` sur la sortie du solveur —
et c'est la seule forme qui sache faire la différence. Les onze tests de mise en
page des vues sont passés à cette forme, et ils échouaient tous avec l'ancienne
sur une mise en page correcte.

Un second défaut a été trouvé en écrivant ce test-là : `InterfacesModel.resize`
passait `max(width-2, 20)` à un `Resize` qui soustrait déjà les bordures, donc
la ligne de cet onglet s'arrêtait **deux cellules avant la bordure à toutes les
largeurs**, évincement ou pas. Le `max(…, 20)` était le même genre de garde : un
plancher au-dessus de ce que le terminal offre rend la table plus large que le
viewport, ce qui est exactement le débordement que Rule 116 interdit. Les deux
sont partis.

**D60 — le dashboard réclamait en permanence un outil réseau nommé « Net Diag »,
alors que l'image est là. Corrigé.** Signalé le 2026-08-25, fermé le même jour.

La boîte Host liste ce qui manque, et elle le lit contre `knownTools` — pas
contre ce que la détection a rendu, délibérément : « un outil que la détection
ne rend plus est absent, et le compter hors du dénominateur le ferait
disparaître au lieu de le signaler ». Le raisonnement est bon ; il suppose que
les deux listes portent les mêmes noms.

[§3.47](#347-la-trace-de-route-est-supprimée-et-networktool_image-avec--done) a
renommé `network.tool_image` en `network.connectivity_image` et, avec lui, la
sonde du dashboard : `detectTools` rend désormais un outil nommé
`"Connectivity"`. `knownTools` est restée sur `"Net Diag"`. Les conséquences
sont les deux moitiés du même défaut :

| | |
|---|---|
| `"Net Diag"` figure au dénominateur et personne ne le détecte | déclaré manquant à chaque frame, `busybox` présent ou non |
| `"Connectivity"` est détecté et ne figure pas au dénominateur | jamais vérifié : l'image peut manquer, rien ne le dit |

Un utilisateur voit donc `Missing tools ├ Net Diag ✗` sur une machine où
`docker image inspect busybox` répond — et la seule information que la ligne
existait pour donner, elle, est perdue.

**Le commentaire disait « Elle doit rester en phase avec detectTools ».** Il
était juste et n'a rien empêché : c'est la forme du défaut, pas un oubli
d'auteur. Deux littéraux pour un seul nom finissent par diverger, quel que soit
le commentaire posé à côté.

La correction est donc en deux temps, et le second est ce qui compte :

- les cinq noms deviennent des **constantes** (`toolDocker`, `toolTrivy`,
  `toolGitleaks`, `toolConnectivity`, `toolGit`), lues par `detectTools` et par
  `knownTools`. Un renommage ne peut plus n'atteindre qu'une des deux ;
- `TestTheDetectedToolsAreExactlyTheKnownOnes` **exécute** la détection et
  compare les noms rendus à `knownTools`. Une constante ne protège pas d'un
  outil ajouté d'un côté seulement, et seule une exécution voit les deux listes
  ensemble. Le test n'affirme rien sur la *disponibilité*, qui dépend de la
  machine ; les noms n'en dépendent pas.

C'est la même leçon que `TestTheForgeVocabularyMatchesTheConfig` et
`TestTheProviderVocabularyMatchesTheConfig` : deux vocabulaires que rien
n'oppose l'un à l'autre dérivent, et le test est la seule chose qui puisse les
tenir.

**D57 — l'onglet Topology affichait le réseau de la VM Docker, pas celui de la
machine. Corrigé** par
[§3.44](#344-longlet-topology-devient-longlet-interfaces--done). Vérifié à
l'écran le 2026-08-24 sous Windows, en cherchant si l'onglet pouvait se passer de
l'image ; fermé le même jour.

C'est **D55 dans une autre vue**, et en pire : là où l'onglet Ports partageait au
moins un numéro de port avec l'hôte par coïncidence de forwarding, ici il n'y a
**aucun recouvrement d'aucune sorte**.

Ce que l'onglet lit :

```
eth0        10.254.254.3/24     docker0     172.17.0.1/16
services1   10.254.254.6/32     br-73a880…  172.19.0.1/16
veth69d515f@if2                 br-7d0ff2…  172.20.0.1/16
default via 10.254.254.1 dev eth0
```

Ce que la machine a :

```
Ethernet 2   192.168.1.21/24    ProtonVPN  10.2.0.2/32     Tailscale  169.254.83.107/16
Wi-Fi, Wi-Fi 3, Wi-Fi 4, Connexion réseau Bluetooth, vEthernet (Default Switch), vEthernet (WSL)
default via 192.168.1.1 dev Ethernet 2   +   default dev ProtonVPN
```

**Pas une interface en commun, pas une route en commun.** Les cinq sondes sont
`runDiagHost`, c'est-à-dire `docker run --rm --network host`, et sur Docker
Desktop ce namespace est celui de la VM Linux. Donc :

| Section | Ce qui s'affiche |
|---|---|
| Network Interfaces | `eth0`, `docker0`, les `br-*`, les `veth*` de la VM |
| Routes | la table de la VM, passerelle `10.254.254.1` |
| ARP / Neighbours | **3 entrées**, toutes internes à la VM |
| Firewall | les chaînes `DOCKER`, `DOCKER-USER`, `DOCKER-FORWARD` |

Ni ProtonVPN, ni Tailscale, ni la carte Ethernet, ni les deux routes par défaut
concurrentes — c'est-à-dire exactement ce qu'on vient chercher dans un
diagnostic réseau. Un utilisateur qui demande « suis-je sur le VPN » obtient une
réponse qui ne parle pas de sa machine, et rien à l'écran ne le dit : le titre
est « Topology », pas « Docker Topology ».

**Une différence réelle avec D55**, et elle a décidé du correctif : ici, montrer
la VM était **faux mais pas inutile**. `docker0`, les `br-*`, les `veth*` et les
chaînes `DOCKER` répondent à une vraie question — pourquoi mon conteneur
n'atteint pas X. Les sockets de la VM, eux, n'intéressaient personne. Ce n'était
donc pas « remplacer » mais « dire de quoi on parle, puis décider quoi garder ».

**Ce qui a été gardé l'est là où la question se pose** : le test de connectivité
OCI (`:oci` → Networks → `enter` → `c`) lance un conteneur **dans un réseau
Docker choisi**, ce qui est correct par construction — et c'est le seul endroit
de l'application qui démarre encore un conteneur. Tout le reste répond depuis ce
process.

**Le défaut est clos des deux côtés.** §3.44 a rapatrié les interfaces et
déplacé la question de la route dans le pipeline de Diagnostics ;
[§3.47](#347-la-trace-de-route-est-supprimée-et-networktool_image-avec--done) a
supprimé la trace de route, qui était le dernier `--network host` de
l'application. Il n'en reste aucun, ce qui est la forme vérifiable de
« DevDesk ne prétend plus répondre pour une machine qui n'est pas la vôtre ».

**Sous Linux le défaut n'existe pas**, pour la raison qui l'a fait passer
inaperçu partout ailleurs : `--network host` y est bien le namespace de la
machine.

L'aide le disait à moitié : la section « Where the checks run » nommait bien
l'onglet Topology parmi ce qui tourne dans l'image et dans la VM. Mais elle est
derrière `?`, et l'onglet lui-même n'en dit rien — c'est la moitié qui compte.

---

**D58 — un échec partiel de l'onglet Topology s'affichait « aucune route »,
« aucun voisin », « aucune chaîne ». Corrigé** par
[§3.44](#344-longlet-topology-devient-longlet-interfaces--done). Trouvé en
lisant `fetchTopoDataCmd` pour D57, le 2026-08-24, et fermé le même jour.

`fetchTopoDataCmd` lance cinq commandes en parallèle et **une seule d'entre elles
peut faire échouer le lot** :

```go
if addrRes.Success {  ifaces = parseIPAddr(addrRes.Output)  } else { err = … }
if linkRes.Success {  ifaces = parseIPLink(…)  } else { log.Printf("WARN"…) }
if routeRes.Success { routes = parseIPRoute(routeRes.Output) }   // sinon : nil, sans un mot
neighbours    := parseIPNeigh(neighRes.Output)                   // Success jamais consulté
firewall, src := parseFirewall(fwRes.Output)                     // Success jamais consulté
```

Si `ip addr show` réussit et que les trois autres échouent — un `--privileged`
refusé, une image sans `iptables`, un démon qui s'arrête entre deux des cinq
`docker run` —, alors `err` est nil, `loadErr` est vidé, et l'écran affiche :

```
  No routes found
  No neighbours found
  No chains found
```

**Une machine sans route n'existe pas.** C'est D20 dans sa forme la plus
littérale, trois fois sur un même écran : « je n'ai pas pu regarder » rendu comme
« il n'y a rien ». Et c'est la vue qui a le moins de chances d'être crue à
l'envers — trois sections vides d'un coup ressemblent à une machine en panne
plutôt qu'à un outil en panne.

Deux détails qui l'aggravent :

- **`parseIPNeigh` et `parseFirewall` reçoivent la sortie d'échec** et la
  parsent comme des données. En pratique le message d'erreur de Docker ne
  ressemble à aucune ligne de leur grammaire, donc le résultat est vide plutôt
  que faux — c'est de la chance, pas une décision.
- **`ip -s link` est le seul échec journalisé** (`WARN`), et il est aussi le seul
  des quatre dont la perte est invisible : les interfaces s'affichent quand même,
  avec `MTU 0` et zéro erreur RX/TX. Un compteur d'erreurs à zéro parce que
  personne n'a regardé est le même défaut, sur une colonne au lieu d'une section.

**Ce qui a été fait**, et c'est §3.44 qui l'a rendu court : avec cinq sources
réduites à deux, la règle tient en une phrase et il n'y avait plus d'excuse pour
ne pas la tenir. `netiface.List` ne rend une erreur que si les interfaces
elles-mêmes n'ont pas pu être lues ; les compteurs qui échouent laissent
`RxErrors` et `TxErrors` à **`nil`**, et la vue affiche `-` — jamais `0`. C'est
le `*bool` de `SecretVerdict()` à l'échelle d'une colonne, et
`TestACounterNobodyReadIsADashAndNeverAZero` échoue si le zéro revient.

Une lecture qui échoue **garde les lignes** et les date (« interfaces as of
2 min ago »), la règle que l'onglet Ports suivait déjà : elles ne sont pas
fausses, elles sont datées, et les jeter viderait l'onglet sur un incident
passager.

---

**D55 — l'onglet Ports listait les sockets de la VM Docker, pas ceux de la
machine. Corrigé** par §3.43. Vérifié à l'écran le 2026-08-23 sous Windows,
fermé le 2026-08-24.

`RunSS` lançait `docker run --rm --net=host --pid=host --privileged`, et sur
Docker Desktop `--net=host` est le namespace **de la VM Linux**. La vue montrait
donc les sockets et les processus de la VM. Ce n'était pas une hypothèse : les
deux relevés côte à côte n'avaient rien en commun.

`Get-NetTCPConnection -State Listen` sur l'hôte — 38 sockets, dont :

```
::           11434   Listen   22328      (ollama)
::            7680   Listen    4604
::            5357   Listen       4      (System)
::             445   Listen       4
0.0.0.0      62841   Listen   32428
127.0.0.1     6463   Listen   20696
192.168.1.21   139   Listen       4
```

L'onglet Ports au même instant :

```
tcp  LISTEN  0.0.0.0:5000     125  dockerd
tcp  LISTEN  0.0.0.0:51031     75  rpc.statd
tcp  LISTEN  0.0.0.0:111       76  rpcbind
tcp  LISTEN  [::]:111          76  rpcbind
tcp  LISTEN  [::]:59517        75  rpc.statd
```

`rpcbind` et `rpc.statd` sont les démons NFS de la VM Docker Desktop, et des PID
à deux chiffres ceux d'un système qui vient de démarrer. **Pas un des 38 sockets
de l'hôte n'était listé.** Le seul port apparaissant des deux côtés était 5000,
et c'était une coïncidence de forwarding : côté Windows il appartenait aux PID
28360 et 32852, côté vue à `dockerd` PID 125 — l'autre bout du même tunnel, pas
le même socket.

**`K` était la moitié grave.** Il tuait par `docker run --pid=host --privileged
<image> kill -9 <pid>`, donc dans le même namespace : l'utilisateur croyait avoir
libéré le port qui le gênait et avait tué un démon de la VM. Rien à l'écran ne le
disait, et le port restait pris.

Le défaut était **de la même famille que celui que §3.33 a corrigé** : DNS, ICMP,
TCP, TLS et HTTP répondaient pour la VM et ont été rapatriés dans
`internal/netcheck` pour cette raison exacte. La phrase est écrite en tête de
`runDiagHost` depuis ; personne ne l'avait tirée pour `RunSS` et `KillProcess`,
qui étaient restés.

**Sous Linux le défaut n'existait pas** : `--net=host` y est bien le namespace de
la machine. C'est ce qui l'a fait passer inaperçu — la vue était juste sur la
plateforme où elle a probablement été écrite.

Ce que la correction a coûté et rapporté est en §3.43.

---

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

**Rien.** D59, D62 et D63 sont partis le 2026-08-28 et cette section est vide
pour la première fois depuis D12 — ce qui suit est de l'histoire, pas du travail
en attente.

Ce qui reste sous cette rubrique est **D35**, qui n'est ni ouvert ni fermé : une
lecture périmée avec un moyen de la rafraîchir. Elle est gardée ici parce que
c'est une décision plutôt qu'un correctif, et les entrées qui la suivent disent
ce que les défauts précédents étaient et par quoi ils ont été fermés.

D39, avant eux, était le navigateur de registries adressant les membres d'un
groupe d'une façon pour les parcourir et d'une autre pour les tirer ; il est
fermé par [§3.18](#318-a-registry-member-is-an-address-not-a-url--repo_prefix).
D12, D13 et D14 ont tous été fermés par §3.8. D21 et D36 ont fermé tout ce qui
les précédait. Tous sont en §1.1.

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

Supersedes §3.5. The implementation plan (`dashboard-resources-plan.md`) has
been removed from `.claude/plans/` now that the work is shipped.

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

> **§3.61 renverse trois de ses décisions** — le transport, la lecture seule et
> le contexte fixé au démarrage — et supprime `dk mcp`. Ce qui suit reste le
> compte rendu de ce qui a été livré le 2026-08-23, et les raisons qui n'ont pas
> bougé (le `Match` d'un secret, `container_logs`, l'allow-list) y sont écrites
> une seule fois.

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

### 3.41 Se passer de `netshoot` — ce qui se réécrit en Go, et ce qu'on abandonne — **analysé, suite en §3.43**

L'analyse est faite et **l'option 1 a été prise** : §3.43 a sorti l'onglet Ports
et `K` de l'image, qui ne sert plus qu'à la trace de route et à l'onglet
Topology. Ce qui suit reste l'énoncé d'origine ; les deux mesures qu'il demandait
sont rapportées en §3.43, et l'une des deux a décidé de l'implémentation.

L'image `nicolaka/netshoot` (`network.tool_image`) était la dernière dépendance
de DevDesk à un conteneur pour des fonctions qui ne sont pas Docker. La question
est de savoir ce qu'il resterait si elle disparaissait.

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

**Vérifié le 2026-08-23, et l'hypothèse tient** : voir **D55** en §1.3 pour les
deux relevés côte à côte. Pas un des 38 sockets en écoute de l'hôte Windows
n'apparaît dans l'onglet, qui montre `rpcbind` et `rpc.statd` — les démons NFS
de la VM.

L'analyse change donc de nature : ce n'est plus « peut-on se passer de l'image »
mais **« il faut s'en passer »**, au moins pour l'onglet Ports et pour `K`. Ce
qui suit reste valable, mais se lit comme un plan de correction plutôt que comme
une évaluation.

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
   `network.tool_image` survit avec un nom qui redevient exact. — *Prise, en deux
   temps : §3.43 pour l'onglet Ports et `K`, §3.44 pour l'onglet Topology, qui
   n'avait été relevé par personne dans cette analyse et porte le même défaut
   (D57).*
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
renoncer à rien et qu'elle isole la question restante. — *C'est ce qui a été
fait. Une fois §3.44 passée, il ne reste que la trace de route, donc les options
2 et 3 sont tout ce qu'il restera à trancher.*

#### Ce que ça retirerait aussi

- `--privileged`, `--pid=host` et les trois montages de `/etc` disparaissent du
  code ; DevDesk cesse de demander à Docker des droits qu'il n'utilise que pour
  lire une table.
- **`ports_list` redeviendrait exposable en MCP** (§3.38 l'a écarté précisément
  parce que `RunSS` démarre un conteneur privilégié). Sans conteneur, l'argument
  tombe.
- L'onglet Ports fonctionnerait **sans Docker installé**, ce qui est aujourd'hui
  une condition pour voir ses propres ports.

#### À vérifier avant de planifier — **mesuré**

L'hypothèse du namespace était vérifiée (D55), et les deux mesures qui restaient
sont prises. Les chiffres sont en §3.43 ; en un mot :

1. **Un socket en écoute sans processus attribuable existe** — 5 sur 189 ici — et
   son PID nul devient une chaîne vide, pas `"0"`, parce que `K` agit sur ce
   champ.
2. **Le PID n'exige aucun privilège ; le *nom* du processus, si.** Et c'est
   l'énumération en masse qui le contourne, sans élévation. Le renoncement
   redouté n'a donc pas lieu : la colonne Process est remplie pour tous les
   processus de la machine.

### 3.42 `plumber` — un score de sécurité de pipeline, par dépôt — **done**

Fait le 2026-08-27, en trois PR, après quatre arbitrages de conception le
2026-08-25 et 2026-08-26 et trois mesures. Le journal d'implémentation — ce que
chaque PR a appris, y compris ce que le plan n'avait pas vu — est dans
[`.claude/plans/plumber-ci-score.md`](../.claude/plans/plumber-ci-score.md).

| PR | Ce qui est entré |
|---|---|
| 1 | le réglage : `enable_ci_score`, les quatre `plumber_*`, `PlumberSpec`, la case **CI**, un groupe **Plumber**, et plumber dans la boîte Host |
| 2 | l'outil tourne : la commande, le parsing avec la jointure `code → severity`, la règle de cible, `Result.CIVerdict()`, `WorkspaceScanEntry.CIScore *string` |
| 3 | les deux écrans : la colonne `CI` dans `ws`, le cinquième onglet, la ligne de tête |

**Ce qui reste, et ne bloque rien** : un jeton GitLab *valide mais sous-doté*
n'a pas pu être mesuré, faute d'instance GitLab configurée sur cette machine. Un
`401` tombe sur `2` ; un `403` devrait suivre, mais la section a montré deux
fois ce que valent les déductions ici. Le pendant GitHub a été mesuré au
passage et va dans l'autre sens : un jeton sous-doté ne fait **pas** retenir le
score, il désactive un contrôle, l'inscrit dans `partialControls`, et note quand
même — la lettre peut donc être optimiste sans que rien ne le dise.

Ce qui suit est la décision et ce que les mesures en ont fait.

[`getplumber/plumber`](https://github.com/getplumber/plumber) lit la
[`getplumber/plumber`](https://github.com/getplumber/plumber) lit la
configuration CI d'un dépôt — `.gitlab-ci.yml`, workflows GitHub Actions — la
passe dans un moteur de politiques Rego, et en tire un **Plumber Score** : une
lettre de A à E, des points sur 100, et les findings qui l'expliquent.

La demande : un réglage sous `scan:`, une colonne **CI Score** dans `ws`, le
détail dans un onglet de plus à côté de CVE, Secrets, Licenses et Misconfig, un
outil résolu depuis le PATH **ou** depuis une image Docker comme Trivy et
Gitleaks, et un fichier de configuration global référençable depuis la config.

**Tout ce qui suit est relevé sur `plumber 0.4.40`, installé sur cette machine,
et sur une exécution réelle** — pas sur la documentation, qui diffère de la CLI
sur au moins deux points (elle annonce un code de sortie `3` qui n'existe pas, et
ne dit pas que le chemin GitHub est local).

> **Ces deux reproches étaient les miens, et ils étaient faux.** Le 2026-08-25 a
> repris la mesure avec l'image et sans les identifiants de cette machine : le
> code `3` existe, et le chemin GitHub interroge l'API. Les deux corrections
> sont écrites ci-dessous, à leur place, plutôt qu'en note — mais le paragraphe
> reste, parce que ce qu'il illustre vaut mieux que ce qu'il affirmait : une
> mesure prise dans un environnement qu'on n'a pas inventorié mesure
> l'environnement autant que l'outil.

#### Deux chemins, et un seul interroge la forge

C'est le fait qui gouverne tout le reste, et il n'est écrit que dans
`plumber analyze --help` :

| Chemin | Déclenché par | Ce qu'il lit | Token |
|---|---|---|---|
| **GitLab** | remote GitLab, ou `--gitlab-url` + `--project` | l'**API** : configuration CI, réglages du projet, protection de branche | `GITLAB_TOKEN` **requis**, sinon exit 2 |
| **GitHub** | origin GitHub, sans `--gitlab-url`/`--project` | les fichiers locaux `.github/workflows` **et** l'API pour la protection de branche | `GH_TOKEN` / `GITHUB_TOKEN` / `gh auth`, sinon **dégradé** |

**Le chemin GitHub n'est pas hors ligne, et c'est la correction la plus lourde
de cette section.** Mesuré le 2026-08-25 : sans identifiants, plumber écrit
`GitHub auth: none — running in degraded mode (workflow-content controls only)`,
signale `GitHub branch-protection fetch failed; branchMustBeProtected will see
zero branches`, **retient le score** et sort en **3**.

L'affirmation inverse tenait à un détail de cette machine : `gh` y est
authentifié, et plumber le lit (`GitHub auth: gh CLI (~/.config/gh)`). Retirer
`gh` du `PATH` — même binaire 0.4.40, même dépôt, même seconde — fait passer la
sortie de `1` à `3`. C'est la même erreur de méthode que D55 et D57, à une
échelle plus petite : on mesurait l'environnement en croyant mesurer l'outil.

**Conséquence pour DevDesk**, si DevDesk ne passe pas de jeton : un conteneur
n'a ni `gh` ni token, donc *tout* dépôt GitHub ressortirait dégradé, et le `?`
deviendrait l'état ordinaire de la colonne plutôt que son cas limite.

**Tranché le 2026-08-26 : le contexte décide de la cible, et il fournit le
jeton.** Un contexte cible une forge et une seule (§3.6), donc :

- contexte **GitHub** → seuls les dépôts dont le remote est celui de
  `forge.url` sont scannés par plumber ; contexte **GitLab**, pareil de leur
  côté ;
- le jeton est celui de la session du contexte, et il ne part que vers l'hôte
  configuré. C'est **mot pour mot `internal/git.tokenForRemote`** (§3.17), qui
  compare l'hôte du remote à celui de `forge.url` et ne rend rien sinon — écrit
  pour empêcher qu'un jeton personnel parte vers le Gitea d'un client, et qui
  répond ici à la même question ;
- un dépôt d'une autre forge n'est donc **pas scanné sans jeton**, il n'est
  **pas scannable** : cellule vide. C'est la décision 2 ci-dessus, généralisée
  de « pas de remote, remote étranger » à « pas la forge de ce contexte ».

Deux conséquences, et les deux vont dans le bon sens :

- **Le `?` redevient l'exception.** Avec un jeton, le chemin GitHub tourne
  complet — exit 0 ou 1, une lettre. Le dégradé ne reste que pour un jeton
  absent, expiré ou sous-doté, ce qui est bien un accident et non le régime
  normal.
- **`--provider` est déclaré, jamais deviné.** Le contexte dit quelle forge
  c'est, donc DevDesk le passe explicitement au lieu de laisser plumber renifler
  le remote — sur le précédent du `provider` des registries et du `forge.type`
  lui-même.

Cinq conséquences :

1. **Le token n'est nécessaire que sur GitLab**, et alors la règle de §3.17
   s'applique telle quelle : `internal/git.tokenForRemote` compare l'hôte du
   remote à celui de `forge.url` et ne rend rien sinon. `workspaces_dir` contient
   par construction des dépôts clonés d'ailleurs, et `GITLAB_TOKEN` est une
   variable d'environnement — donc lisible dans l'environnement du processus fils,
   ce qui est déjà l'arrangement retenu pour `http.extraHeader` au clone.
2. **Un dépôt GitLab sans remote joignable ou sans token n'est pas scannable.**
   La cellule est **vide**, pas `-` : c'est la distinction que les quatre colonnes
   de sévérité font déjà entre « jamais scanné » et « ne peut pas l'être ».
3. **Sur GitLab, le score décrit une branche du serveur, pas le HEAD local.**
   `--branch` existe, mais la branche courante n'est peut-être pas poussée.
   **Tranché le 2026-08-25 : on passe la branche courante et on accepte
   l'échec.** L'alternative — assumer la branche par défaut — mettrait dans la
   colonne une lettre qui décrit autre chose que ce que l'utilisateur regarde,
   et il faudrait alors le dire à l'écran, comme `renderTraceHeader` disait que
   la route était celle du conteneur. Or c'est précisément cette phrase-là qui a
   fini par ne plus suffire : §3.47 a supprimé la trace parce qu'un résultat
   juste sur une autre machine reste faux ici. Un score juste sur une autre
   branche est la même chose. Une branche non poussée doit donc rendre le même
   « personne n'a regardé » qu'un dépôt non scannable — par quel code de sortie,
   c'est à mesurer avec le point 2 de « Ce qui reste ouvert ». Sur GitHub la
   question ne se pose pas — c'est l'arbre de travail.
4. **Il ne s'applique pas aux images**, comme Gitleaks. La colonne n'existe que
   dans `ws`, et l'onglet est vide sur un résultat d'image.
5. **Le mode Docker n'a pas le même besoin selon le chemin.** GitHub exige que le
   dépôt soit monté ; GitLab n'a besoin que du réseau, du token et du fichier de
   configuration. Un seul montage de la cible couvre les deux, mais la raison
   diffère, et c'est ce qui décide si un dépôt sans `.git` peut être scanné.

#### Ce que la CLI offre — `plumber analyze`

| Drapeau | Défaut | Ce qu'il fait |
|---|---|---|
| `--config` | `.plumber.yaml` | chemin du fichier de configuration |
| `--output`, `-o` | — | écrit les résultats JSON dans un fichier |
| `--score` | `false` | bannière sur stdout, **et le bloc `plumberScore` dans le JSON** |
| `--provider` | auto | force `github` ou `gitlab` |
| `--gitlab-url` | auto | instance GitLab |
| `--github-url` | `api.github.com` | hôte d'API GitHub **Enterprise** |
| `--project` | auto | chemin du projet, déduit du remote git |
| `--branch` | branche par défaut | branche analysée (chemin GitLab) |
| `--min-score` / `--min-points` | — / `100` | portes de sortie |
| `--print` | `true` | rapport lisible **et barre de progression sur un tty** |
| `--controls` / `--skip-controls` | — | restreindre les contrôles joués |
| `--ci-config-path` | auto | chemin du fichier CI |

Codes de sortie, **quatre**, remesurés le 2026-08-25 sur le binaire 0.4.40 et
sur les images 0.4.40 et 0.4.42, à chaque fois par une exécution réelle :

| Code | Sens | Mesuré sur |
|---|---|---|
| `0` | porte tenue | — |
| `1` | porte non tenue : un score existe et il est sous le seuil | dépôt GitHub avec `gh` authentifié, 1 critique |
| `2` | erreur d'exécution | `GITLAB_TOKEN` absent ; jeton GitLab invalide (401) ; `--config` introuvable ; `--config` de version inconnue ; dépôt non détecté |
| `3` | **score retenu** : l'analyse a tourné sur des données incomplètes | dépôt GitHub sans identifiants |

**Le `3` existe, contrairement à ce que cette section a affirmé jusqu'ici.** La
première mesure ne l'avait jamais vu parce qu'aucune exécution n'avait dégradé —
voir le chemin GitHub ci-dessus. C'est le code le plus important des quatre pour
DevDesk : il correspond exactement à l'état `?` de la colonne, et il évite d'avoir
à lire `dataCollectionDegraded` dans le JSON pour le savoir.

**`1` est le cas courant** — le défaut de `--min-points` est 100, donc le moindre
finding le déclenche. Comme pour Gitleaks, un code non nul n'est pas un échec ;
et ici la séparation est propre, ce qui est précisément ce que D56 n'avait pas :

| Code | Cellule |
|---|---|
| `0`, `1` | la lettre — il a répondu |
| `3` | `?` — il a répondu qu'il ne pouvait pas conclure |
| `2` | vide — il n'a pas répondu |

Les confondre mettrait un échec dans la colonne comme s'il était un verdict, ce
qui est D56 un étage plus haut. La différence avec gitleaks est que plumber
**dit** laquelle des trois situations c'est, sans qu'on ait à deviner d'après la
présence d'un rapport.

**Cinq drapeaux ne doivent jamais être exposés dans la configuration**, et ce
ne sont pas des options de scan : ce sont des écritures sortantes.

| Drapeau | Ce qu'il envoie, et où |
|---|---|
| `--score-push` | la posture du dépôt, au service hébergé `score.getplumber.io` |
| `--score-endpoint` | choisit cette destination |
| `--badge` | **écrit sur le projet** : crée ou met à jour un badge de score |
| `--mr-comment` | **écrit sur le projet** : poste ou met à jour un commentaire de merge request |
| `--platform` | y poussait les résultats complets — **il n'apparaît plus** dans l'aide de 0.4.40 ni de 0.4.42 |

Les deux du milieu ont été relevés le 2026-08-25 dans `analyze --help` ; ils
manquaient à cette liste, et ils sont pires que `--score-push` : celui-ci publie
un score, ceux-là modifient le dépôt de quelqu'un. Que `--score-push` soit un
no-op hors CI ne change rien à l'argument : l'absence du champ est la garantie,
et c'est la forme qu'ont déjà les trois garanties de secret de §3.38.

`--platform` a disparu de l'aide, ce qui n'est pas une raison de le retirer de
la liste — un drapeau non documenté peut rester accepté, et la liste est une
liste de ce qu'on n'écrit pas.

#### Le JSON, relevé sur une exécution

Un run sur un workflow GitHub écrit pour déclencher cinq contrôles. Ce qui compte :

- **Une issue ne porte pas sa sévérité.** Ses clés sont `code` (`ISSUE-701`),
  `docUrl`, `fingerprint`, `identity.fields` (dont `file`), `job`, `url`, plus
  des champs propres au contrôle (`uses`, `scriptLine`). La sévérité vit
  **ailleurs**, dans `plumberScore.codeLosses[]`, indexée par `code`.
  **Donc `--score` n'est pas optionnel pour DevDesk** : sans lui il n'y a aucune
  sévérité, donc pas de colonne Severity, pas de jetons de filtre, rien à trier.
  Le remplissage d'un `Finding` passe par une jointure `code → severity`.
- **Le vocabulaire tombe juste** : `critical`, `high`, `medium`, `low`, en
  minuscules, sans `unknown`. C'est exactement `SeverityLevel` après passage en
  majuscules, donc les quatre jetons cumulatifs de la barre de filtre marchent
  sans traduction.
- **Une issue n'a ni titre ni description.** `docUrl` pointe la page du code, et
  `plumber explain <code>` est la commande faite pour ça — donc `Title` se
  construit du `code` et du `controlName` du bloc qui la porte, et `Resolution`
  peut porter la commande. Rien à inventer, mais rien de gratuit non plus.
- **`url` est un chemin hôte absolu suffixé `:<ligne>`**
  (`C:\...\.github\workflows\ci.yml:8`). `File` et `Line` s'en tirent, mais il
  faut le rendre relatif au dépôt avant de l'afficher — et en mode Docker ce
  chemin sera celui du conteneur.
- **`fingerprint` existe**, sur le modèle de Gitleaks : de quoi bâtir un `X` plus
  tard, si plumber a un fichier d'exclusion. À ne pas fabriquer autrement (c'est
  l'argument qui réserve `X` aux findings Gitleaks).
- **`dataCollectionDegraded` et `degradedReasons` sont le quatrième état**, et le
  plus important pour la colonne. Sur le run relevé :
  `["branch protection could not be fetched; branch controls were not evaluated"]`,
  et la CLI répond « the score is withheld ». Le JSON continue pourtant d'écrire
  `"score": "E"`. **Reprendre cette lettre telle quelle serait afficher un E pour
  des contrôles qui n'ont pas tourné** — le même défaut que D56, sur un autre
  outil. Un run dégradé doit rendre le même « personne n'a regardé » qu'un run
  absent.
- `ciMissing` est un état de plus : un dépôt sans CI n'a pas un mauvais score, il
  n'a pas de pipeline.

Le reste du document est un bloc `<contrôle>Result` par contrôle — 23 sur ce run
— chacun avec `controlName`, `status`, `skipped`, `issues`, `metrics`.

#### La configuration — `scan.plumber_*`, sur le patron de Trivy et Gitleaks

```yaml
scan:
  enable_ci_score: false     # off par défaut, comme les autres étapes
  plumber_source: auto       # auto | binary | image
  plumber_path: ""           # binaire hors PATH
  plumber_image: ""          # défaut getplumber/plumber
  plumber_config: ""         # --config : le fichier global demandé
```

`DependencyStatus` gagne `PlumberAvailable`, `PlumberSource`, `PlumberVersion`,
`PlumberBinary`, `PlumberImage`, et une méthode `PlumberSpec() ToolSpec`.
**Le chemin du binaire va sur le `ToolSpec`, pas en paramètre positionnel** —
c'est D27 : `trivy_path` est resté non lu longtemps précisément parce que le
couple `(source, image)` n'avait nulle part où le porter. La version se lit par
`plumber version` (vérifié), qui écrit aussi une ligne « une version est
disponible » à ne pas confondre avec la version installée.

`binary` échoue bruyamment plutôt que de retomber sur Docker, pour la raison déjà
écrite : c'est le repli silencieux qui rend un réglage invisible, puisque les
scans continuent de marcher avec autre chose que ce qui a été demandé.

Vue configuration : un groupe **Plumber** dans l'onglet `scan`, à côté de Trivy
et de Gitleaks, et `enable_ci_score` dans **Scanners** avec les quatre autres.
La table de `fields.go` prend cinq lignes de plus, chacune avec son unique
accesseur pointeur.

#### Le fichier de configuration global — le précédent est écrit

« Global » veut dire : un fichier pour tous les dépôts du contexte, désigné par
un chemin absolu, au lieu du `.plumber.yaml` que plumber cherche dans chaque
dépôt. C'est mot pour mot ce que `scan.gitleaks_config` est déjà.

`scan.gitleaks_config` ne marchait pas en mode Docker — le chemin hôte partait
tel quel dans le conteneur, gitleaks n'y trouvait rien, sortait en `1` avec
stdout vide, et DevDesk lisait ça comme « aucun secret ». C'était **D56**, et
[§3.50](#350-un-fichier-de-règles-gitleaks-est-monté-et-un-scan-qui-na-rien-lu-nest-plus-propre--done)
l'a fermé le 2026-08-25 : le corriger d'abord évitait d'écrire la même erreur
deux fois, la seconde en connaissance de cause.

Ce qu'il laisse à copier, quatre points :

| | |
|---|---|
| le point de montage | une constante du paquet, à la racine du conteneur, avec un test disant qu'elle n'est pas sous la cible montée |
| le drapeau | `--config` désigne le montage, jamais le chemin hôte, et un test refuse que ce dernier atteigne le conteneur |
| le chemin | rendu absolu au chargement : un relatif ne veut pas dire la même chose des deux côtés de la frontière |
| l'échec | ce qui sépare un résultat d'un échec est **mesuré**, pas supposé — et pour plumber c'est `1` contre `2`, relevé plus haut |

Le quatrième est celui que §3.50 n'avait pas prévu et qui a coûté le plus : la
moitié de D56 n'était pas le montage manquant mais une tolérance sur le code de
sortie, écrite pour un comportement que gitleaks n'a pas. Les codes de plumber
sont relevés, ce qui met §3.42 du bon côté — à condition de ne pas y ajouter de
tolérance non mesurée.

#### La colonne dans `ws`

**Tranché le 2026-08-25 : titre `CI`, `SizingFixed`, largeur 4, valeur la
lettre seule.** Les points sont dans l'onglet, où il y a la place. La demande
disait « CI Score », et c'est le titre qui coûte, pas la valeur : huit cellules
pour afficher une lettre. Les quatre colonnes de sévérité s'appellent `C H M L`
pour cette raison exacte, et une cinquième colonne de comptage à côté d'elles
hérite de leur convention plutôt que d'en ouvrir une deuxième.

**La prémisse chiffrée de cet arbitrage a changé depuis
[§3.45](#345-datatable--chaque-colonne-déclare-sa-nature-et-les-largeurs-suivent--done)**,
et il faut le noter plutôt que de laisser le chiffre dater. « Onze colonnes,
120 cellules fixes, ~156 colonnes de terminal demandées » décrivait une table
qui débordait ; `MinWidth` est maintenant un plancher et une douzième colonne
dégrade en **retirant** des colonnes entières au lieu de déborder. L'argument
survit donc affaibli — quatre cellules valent toujours mieux que huit — mais il
ne repose plus sur un dépassement de largeur. Ce qui le porte désormais, c'est
la convention `C H M L`.

Une conséquence à décider en même temps que la colonne : **`CI` ne déclare pas
`Optional`**, comme les quatre colonnes de sévérité et pour la même raison. Sur
les onze colonnes actuelles, deux seulement sont `Optional` — `Scanned` et
`Modified` — et ce sont celles qui cèdent en premier quand la place manque. Une
colonne de verdict qui disparaîtrait sur un terminal étroit rendrait « pas de
score » et « pas de place » indiscernables, ce qui est le défaut que les quatre
états de la cellule existent pour éviter.

Quatre états, un de plus que les colonnes voisines :

| Cellule | Sens |
|---|---|
| `A`…`E` | un score, d'un run complet |
| `?` | run **dégradé** (`dataCollectionDegraded`) **ou** `ciMissing` |
| `-` | jamais scanné |
| *(vide)* | ne peut pas l'être — pas un dépôt, pas de remote, remote étranger, branche non poussée |

**Tranché le 2026-08-25 : les deux causes du `?` ne sont pas distinguées dans la
cellule.** Quatre cellules n'ont pas à porter une distinction que l'écran d'à
côté porte : l'onglet dit laquelle des deux, et en toutes lettres. Ce que la
colonne doit garantir, c'est que ni l'une ni l'autre ne se confonde avec une
lettre — le JSON écrit `"score": "E"` sur un run dégradé dont la CLI dit
pourtant *the score is withheld*, et reprendre cette lettre serait D56 sur un
autre outil.

`WorkspaceScanEntry` gagne donc `CIScore *string` et non `string` : `nil` veut
dire que personne n'a regardé, ce qui est exactement l'argument de
`Sensitive *bool`. `ImageScanEntry` ne gagne rien. Comme le champ n'a jamais été
écrit, un fichier de cache existant décode en `nil`, ce qui est la vérité sur lui.

La couleur passe par `Style`, jamais par `Cell` (Rule 122), et suit la discipline
de couleur : `A` est l'état nominal et reste en couleur de texte, `-`, le vide et
`?` sont `DimStyle`, la couleur est dépensée sur `D` et `E`. Une fonction
`theme.CIScoreStyle(state)` sur le modèle de `theme.SecretsState` — **l'état, pas
la chaîne rendue** : décider la couleur d'après ce qui a été imprimé est
précisément ce que `ws` faisait et qu'il a fallu défaire.

#### L'onglet

`SourcePlumber = "plumber"`, `CategoryCIScore`, une ligne dans `Categorize`, une
dans `tabCategory`, `TabCIScore = 4`. La classification se fait **sur la source
et rien d'autre** (§3.12) : pas de reconnaissance à la présence d'un champ, c'est
ce qui avait produit des findings comptés dans le header et absents de tout
onglet.

**Un score n'est pas un finding**, et c'est ce qui distingue cet onglet des
quatre autres. Les issues sont les lignes de la table ; la lettre, les points et
l'état dégradé décrivent le **run** et n'ont donc aucune ligne où aller.

**Tranché le 2026-08-25 : une ligne de tête dans l'onglet CI, au-dessus de la
table** — un libellé `Score`, `theme.IconChevronRight`, puis `C · 61/100` ou
`withheld — branch protection could not be fetched` (Rule 120 : le séparateur est
le chevron, jamais `:`). Le header a été
écarté pour deux raisons. La raison d'un run dégradé est une **phrase**
(`degradedReasons`), et `buildInfoLines` aligne des valeurs courtes sur sept
lignes en jetant le reste **en silence** — c'est déjà ce qui a fait tomber cinq
champs de cette vue. Et un champ de header s'affiche sur les cinq onglets : un
fait sur une étape pendant qu'on lit les findings d'une autre.

L'hybride — la lettre au header, la raison dans l'onglet — a été écarté aussi :
c'est un même fait à deux endroits, et le prix en est écrit deux fois dans ce
backlog (§3.9 pour le thème, §3.12 pour `SecretVerdict`).

La lettre apparaît donc à deux échelles et une seule fois chacune : la colonne
`CI` de `ws` pour la vue d'ensemble, la ligne de tête pour le détail. Le libellé de
l'onglet porte le **nombre d'issues** comme ses quatre voisins — `CI (7)`, pas
`CI (E)` : une lettre à la place d'un compte romprait la seule colonne que la
barre d'onglets tient (`view.go:143`).

Sur `Result`, la forme à suivre est celle des secrets : `CIScanned bool` écrit
par une étape qui **réussit**, et une seule fonction qui décide du verdict —
deux calculs de la même question sont ce que `SecretVerdict()` a eu à défaire.

Les `<contrôle>Result` qui passent ne sont pas des findings et ne vont dans aucun
onglet ; ils sont pourtant ce qui donne son sens au score. Les ignorer d'abord,
et le noter.

**Ce que `mcp.enabled: true` veut dire a changé, et rien ne le redemande.**
Sous §3.38 le réglage disait « un serveur en lecture seule, sur stdio, lancé par
le client, sans aucun port » ; il dit maintenant « un port sur le loopback, et
le palier d'action avec, `expose` étant vide par défaut ». Un contexte qui avait
dit oui au premier n'est pas réinterrogé sur le second — et il serait
reconnaissable, un fichier écrit avant §3.61 ne portant pas de `listen`.

Le défaut plus étroit était donc disponible et n'a pas été pris, délibérément :
le réglage a toujours voulu dire « un agent peut atteindre ce contexte »,
l'élargissement est le sujet même de l'entrée, et une migration serait de la
cérémonie autour d'une décision que son seul utilisateur venait de prendre.
C'est écrit ici et dans le commentaire de `MCPConfig` plutôt que laissé à
découvrir.

#### Ce qui reste ouvert

Les quatre arbitrages de conception ont été pris le 2026-08-25 et sont écrits là
où ils s'appliquent — `--branch`, le titre de la colonne, les deux causes du
`?`, et où va le score. Le prérequis est levé : **D56 est corrigé** par
[§3.50](#350-un-fichier-de-règles-gitleaks-est-monté-et-un-scan-qui-na-rien-lu-nest-plus-propre--done),
qui laisse à `scan.plumber_config` un précédent complet à copier — un point de
montage nommé dans le paquet, un drapeau qui désigne le montage, un chemin rendu
absolu au chargement, et une règle disant ce qui sépare un résultat d'un échec.

Les trois mesures ont été prises le 2026-08-25. Elles ont répondu, et deux
d'entre elles ont corrigé la section.

#### 1. Ce que l'image a besoin de voir

Relevé sur `getplumber/plumber` 0.4.40 et 0.4.42, exécutions réelles :

| | |
|---|---|
| **`analyze` ne prend pas de chemin** | il travaille sur le répertoire courant. Le montage doit donc être le **répertoire de travail** : `-w /scan`, pas un argument positionnel — `analyze /scan` est refusé |
| **le dépôt monté ne suffit pas** | l'image tourne en `uid 65532`, donc git refuse : `fatal: detected dubious ownership in repository at '/scan'`, et plumber répond *could not determine the provider: not in a git repository* |
| **la levée** | `-e GIT_CONFIG_COUNT=1 -e GIT_CONFIG_KEY_0=safe.directory -e GIT_CONFIG_VALUE_0=/scan`. Par l'environnement, donc compatible avec un montage en lecture seule et avec un utilisateur non root — `--user` ne l'est pas sous Windows |
| **`--config` accepte un chemin hors du dépôt** | vérifié des deux côtés : en binaire sur un fichier d'un autre répertoire, et dans le conteneur sur un fichier monté séparément. Le montage de §3.50 s'applique donc tel quel |
| **git est dans l'image** | `/usr/bin/git` ; la racine ne contient que `/plumber`, le binaire — un point de montage nommé `/plumber.yaml` est libre, `/plumber` ne l'est pas |

**Le chemin GitHub a deux modes, et le choix entre eux n'est pas neutre.**
Mesuré le 2026-08-26, en cherchant si `--provider` dispensait du bricolage
`safe.directory` — il n'en dispense pas, mais le message d'erreur en a révélé un
second : *« GitHub local scan needs a git repository (run inside a clone), or
pass `--project owner/repo` for a remote scan »*.

| Mode | Ce que le conteneur exige | Ce qui est noté |
|---|---|---|
| **local** | le dépôt monté sur le répertoire de travail, git utilisable (donc `safe.directory`), **et** un jeton pour la protection de branche | l'arbre de travail, plus l'API |
| **distant** (`--project owner/repo`) | **rien qu'un jeton** — ni montage, ni git, ni `-w` | la branche par défaut **du serveur** |

Le mode distant a été vérifié avec un jeton : exit 1, score complet, conteneur
nu. Il rendrait le mode Docker trivial et donnerait au chemin GitHub la forme du
chemin GitLab.

**Mais il change ce que la colonne veut dire**, et c'est ce qui doit décider :

- le mode **local** note ce qui est sur le disque — ce que l'utilisateur
  regarde, éditions non commitées comprises. C'est ce que font déjà Trivy et
  Gitleaks dans cette vue, et `ws` est une liste de copies de travail : une
  cellule qui parlerait du serveur serait la seule de la ligne à parler d'autre
  chose ;
- le mode **distant** note ce qui est publié. Il donne à GitHub le problème de
  branche que la conséquence 3 réserve à GitLab — l'arbitrage « on passe la
  branche courante et on accepte l'échec » deviendrait alors la règle des deux
  forges, ce qui simplifie d'un côté et ment un peu plus de l'autre.

Recommandation : **local**, pour la cohérence de la ligne, le montage et le
`safe.directory` étant mesurés et bon marché. Le distant reste le repli naturel
pour un dépôt dont le clone local est illisible. **À trancher avant l'étape 3 du
plan.**

**Et un `--config` fautif est bruyant, ce qui est la différence avec gitleaks.**
Fichier absent : `Error: configuration file not found`, exit 2. Version
inconnue : `Error: configuration error: unsupported config version`, exit 2. Le
mode d'échec de D56 — un outil qui meurt sans le dire et dont le silence se lit
comme un résultat propre — n'est donc pas reproductible ici. La garde de §3.50
reste néanmoins nécessaire pour l'autre raison, celle qui n'a rien à voir avec
la détection : un `-v` sur un chemin hôte absent **crée un répertoire**.

#### 2. Ce que fait un échec d'authentification, et une branche inconnue

| Situation | Code | Ce que plumber dit |
|---|---|---|
| GitLab, `GITLAB_TOKEN` absent | **2** | `GITLAB_TOKEN environment variable is required for GitLab analysis` |
| GitLab, jeton invalide | **2** | l'erreur porte le `401 Unauthorized` de l'API |
| GitHub, sans identifiants | **3** | mode dégradé, score retenu |
| `--branch` inconnue, chemin **GitHub** | **1** | rien : le drapeau est sans effet sur ce chemin, qui lit l'arbre de travail |

Le dernier confirme l'arbitrage du 2026-08-25 sur `--branch` du côté où il ne
coûte rien : passer la branche courante sur un dépôt GitHub est inoffensif.

**Ce qui n'a pas pu être mesuré, et pourquoi :** un jeton GitLab *valide mais
sans droits* sur le projet. Aucun contexte de cette machine n'a d'instance
GitLab configurée, donc il n'y avait pas de jeton à sous-doter. Un `401` tombe
sur `2` ; un `403` devrait faire de même, puisque les deux remontent par le même
chemin d'erreur de l'API — mais c'est une déduction, pas une mesure, et la
section vient de montrer deux fois ce que valent les déductions ici. À reprendre
quand un contexte GitLab existera.

#### 3. La licence et le poids

| | |
|---|---|
| Licence | **MPL-2.0** (dépôt et label `org.opencontainers.image.licenses` de l'image concordent) |
| Image | **89,8 Mo**, `getplumber/plumber:latest` — entre busybox (6,81 Mo) et netshoot (874 Mo) |
| Entrypoint | `/plumber`, `WorkingDir` `/`, `User` `plumber` (uid 65532) |
| Amont | actif — 783 étoiles, un commit le jour de la mesure ; 0.4.42 est sortie pendant qu'on écrivait cette section |

Rien n'entre dans le binaire DevDesk : c'est une image à tirer ou un binaire à
installer, comme Trivy et Gitleaks.

#### 4. Ce qu'un finding ne dit pas encore — suite en §3.51

Mesuré à l'usage le 2026-08-27 : plumber n'analyse pas le `.gitlab-ci.yml` du
dépôt mais la configuration que **GitLab** en dérive, includes et composants
résolus transitivement côté serveur. Un finding porte donc sur un job que le
dépôt ne contient pas, et son lien `#L<n>` renvoie à l'entrée `include:`
responsable plutôt qu'à la ligne fautive. Ce que DevDesk peut en faire — montrer
le document fusionné, et remonter au fichier amont à son SHA — est
[§3.51](#351-le-yaml-fusionné-dans-le-viewer-et-un-finding-plumber-qui-sait-où-il-pointe).

### 3.43 L'onglet Ports lit la machine — `internal/ports` — **done**

Fait le 2026-08-24. C'est l'option 1 de §3.41, prise pour la raison que D55 a
établie : la question n'était plus « peut-on se passer de l'image » mais « il
faut s'en passer », au moins pour l'onglet Ports et pour `K`.

#### Ce qui a bougé

| | Avant | Après |
|---|---|---|
| lecture des sockets | `ss -tupan` dans `docker run --rm --net=host --pid=host --privileged` | `gopsutil/v4/net` dans le process DevDesk |
| `K` | `docker run --pid=host --privileged <image> kill -9 <pid>` | `os.FindProcess` + `Kill()` |
| dépendance Docker de l'onglet | obligatoire | **aucune** |
| dépendance Go nouvelle | — | **aucune** : `gopsutil/v4/net` était déjà importé par `internal/metrics/host.go` |
| taille du binaire | 27,97 Mo | **28,09 Mo** (+0,11) — `gopsutil/v4/process`, qui n'était pas encore lié |

`internal/docker/ports.go` et son test disparaissent (197 lignes avec
`netdiag.go`, dont 134 ici). `internal/ports` les remplace par deux fichiers, et
c'est un paquet plutôt qu'un fichier de plus dans `docker/` parce que ce qu'il
fait n'a plus rien à voir avec Docker — le laisser là aurait été garder le nom du
défaut.

#### Les deux mesures que §3.41 demandait avant de planifier

Prises sur la machine, et la première a décidé de l'implémentation :

**Le nom du processus exige des droits ; le PID non.** `process.NewProcess(pid).
Name()` passe par `OpenProcess` sous Windows : **98 sockets sur 189** ont répondu
*Accès refusé*, c'est-à-dire tous les services de la machine. `Processes()` lit un
snapshot Toolhelp32 à la place et a nommé **294 processus sur 294 en 11 ms**, sans
élévation. L'énumération en masse n'est donc pas une optimisation de l'appel par
PID : c'est la différence entre une colonne Process remplie et une colonne vide.
C'était exactement le « vrai renoncement possible » que §3.41 demandait de
mesurer plutôt que de deviner — et il n'a pas lieu.

**Cinq sockets sur 189 n'ont aucun processus attribuable** (PID 0). Le PID est ce
sur quoi `K` agit, donc un PID nul devient une chaîne vide et pas `"0"` : rendu
`"0"` la ligne aurait l'air tuable, et le processus 0 est un groupe entier sous
Unix. `Kill` le refuse une seconde fois, sur le modèle de la double garde de
§3.23.

#### Trois choix qui ne sont pas cosmétiques

- **`ESTABLISHED` est raccourci en `ESTAB`.** La colonne State fait dix cellules,
  donc l'orthographe longue est tronquée en `ESTABLISHE` — et le jeton de filtre
  de `e` se lit mieux court.
- **Un socket datagramme non connecté devient `UNCONN`.** Linux rapporte `NONE`,
  Windows ne rapporte rien : sans normalisation le même socket se lisait
  différemment selon la plateforme, et les filtres `l`/`e` avec lui.
- **`n` ne résout que la moitié hôte.** `ss` sans `-n` transformait aussi 22 en
  `ssh` ; Go résout un nom vers un port et pas l'inverse, donc honorer cette
  moitié demanderait d'embarquer une copie de `/etc/services` et d'appeler ça la
  réponse du système. En dire moins vaut mieux que dire ce que le système n'a pas
  dit. C'est le seul renoncement de l'opération.

Le cache de résolution inverse est **au niveau du paquet**, parce que les
recherches tournent dans un `Cmd` et qu'un `Cmd` ne touche pas au modèle
(Rule 110). Il mémorise **aussi les échecs** : une machine parlant à des hôtes
sans enregistrement PTR les redemanderait à chaque tick de deux secondes — la
rafale que le cache existe pour éviter, arrivant par les échecs au lieu des
succès. Les adresses génériques, loopback et non spécifiées ne sont jamais
demandées.

#### Ce que ça a rendu possible

**`ports_list` existe en MCP.** §3.38 l'avait refusé parce que `RunSS` démarrait
un conteneur privilégié et que la promesse du serveur est de ne pas agir sur la
machine ; sans conteneur, l'argument part avec. Il ne résout jamais d'adresse :
`net_check` est le seul outil qui touche au réseau, et un listing qui demanderait
discrètement le reverse DNS de chaque pair trouvé en serait un second.

**L'onglet Ports fonctionne sans Docker installé**, ce qui était jusqu'ici une
condition pour voir ses propres ports.

**`K` signale avec les droits que DevDesk a.** C'est le changement visible : le
processus d'un autre utilisateur, ou un service, revient maintenant refusé par le
système au lieu de réussir contre la mauvaise machine.

#### Ce qui reste dans l'image, et pourquoi

`network.tool_image` sert encore à deux choses, et le commentaire de
`NetworkConfig.ToolImage` le dit maintenant : **la trace de route** (`H`), qui
demande des sockets ICMP bruts — la règle écrite dans `netcheck/env.go` est
« DevDesk must not need root », et un traceroute en Go la contredirait sous
Windows et sous Linux — et **l'onglet Topology**, qui a besoin de netfilter.

Les deux répondent toujours pour la VM. `renderTraceHeader` le dit à l'écran, et
la section d'aide « Where the checks run » a été réécrite pour ne plus prétendre
que l'onglet Ports en fait partie.

**L'onglet Topology, lui, ne le dit nulle part**, et §3.41 ne l'avait pas relevé.
Vérifié aussitôt après : c'est le même défaut, sans le moindre recouvrement entre
la VM et la machine — **D57**, et **D58** trouvé en lisant le même code. Le
remplacement et ce qu'on en supprime sont en §3.44 ; ce qui restera après elle
est la trace de route seule, donc les options 2 et 3 de §3.41.

#### Une garde de source plutôt qu'une convention

`TestNothingHereRunsASubprocessOrTalksToDocker` refuse `os/exec` et
`internal/docker` dans `internal/ports`. La correction n'est pas « appeler Docker
autrement », c'est « ne pas appeler Docker », et c'est une propriété de la source
et non d'un comportement : un helper ajouté là qui lancerait un processus
remettrait le défaut sans faire échouer quoi que ce soit d'autre. Même forme que
`internal/ui/keymap` et que les deux tests de stdout de §3.38.

### 3.44 L'onglet Topology devient l'onglet Interfaces — **done**

Fait le 2026-08-24. Suite de §3.41 pour la seconde des deux fonctionnalités
restées dans l'image, et correctif de **D57** (la vue montrait la VM) et de
**D58** (un échec partiel se lisait comme une absence).

La règle donnée était simple : **ce qui ne s'affiche pas facilement sur les trois
plateformes est supprimé, pas traduit.** Ce qui suit l'applique source par
source, en mesurant plutôt qu'en supposant.

#### Les quatre sources, et ce qu'elles sont devenues

Tout est relevé le 2026-08-24 sur cette machine, sans élévation.

| Section | Remplacement natif | Nouvelle dépendance | Verdict |
|---|---|---|---|
| Interfaces — nom, état, adresses, MTU | `net.Interfaces()`, **stdlib** | aucune | **gardée** |
| Erreurs RX/TX par interface | `gopsutil/v4/net.IOCounters(true)` | aucune (déjà là) | **gardée** |
| Routes | `go-netroute`, une implémentation pour les trois plateformes | `github.com/libp2p/go-netroute` | **déplacée dans Diagnostics** |
| ARP / Neighbours | rien de portable — détail plus bas | — | **supprimée** |
| Firewall | rien du tout | — | **supprimée** |

**Les deux premières sont gratuites et exactes.** `net.Interfaces()` rend les 10
interfaces de la machine en 4,6 ms, et `IOCounters(true)` en rend 10 aussi en
2,6 ms : **0 interface sur 10 sans ligne de compteurs correspondante**, les noms
concordent au caractère près, donc les erreurs tombent sur la bonne ligne sans
table de correspondance. Le seul écueil relevé est écrit dans le code et rendu à
l'écran : la loopback rend **`MTU = -1`** sous Windows, là où `ip` rend 65536 —
la cellule affiche `-` plutôt qu'un nombre que la machine ne veut pas dire.

L'onglet **gagne au passage le MAC**, que `net.Interfaces()` donne pour rien et
que l'ancienne vue jetait. C'est ce sur quoi un bail DHCP, un port de switch et
un inventaire de parc sont tous indexés, donc c'est le champ qu'on vient copier.

#### Les routes : la question a changé de forme, et c'est ce qui a tout décidé

L'entrée d'origine recommandait de garder la table de routage au prix de **trois
implémentations** — `GetIpForwardTable2` sous Windows, `/proc/net/route` sous
Linux, `x/net/route` sous macOS — et signalait que c'était le seul poste qui
coûtait vraiment.

`github.com/libp2p/go-netroute` fait les trois derrière une seule API, et ne
dépend que de `x/net` et `x/sys`, **tous deux déjà dans le graphe**. Mais il ne
liste pas la table : il répond « quelle route gagne pour cette destination »
(`GetBestRoute2` sous Windows, une requête netlink `RTM_GETROUTE` sous Linux, la
socket de routage sur les BSD). Mesuré ici : **~2 ms par requête, sans
élévation**.

C'est ce décalage qui a fait le design. Une requête se pose **sur une cible**,
donc elle n'appartient pas à un écran de topologie : elle appartient au pipeline
de Diagnostics, qui a déjà une cible. Relevé à l'exécution :

```
dl.google.com  ->  [OK] Traffic leaves through ProtonVPN   src 10.2.0.2
192.168.1.1    ->  [OK] Traffic leaves through Ethernet 2  src 192.168.1.21
```

C'est-à-dire la question du split tunnel, répondue là où elle se pose. L'étage
`route` s'insère après `resolve`, **ne barre rien** (une table illisible ne doit
pas cascader sur le port et le certificat — l'argument de `reach`), et prend ses
adresses du check `resolve` plutôt que de résoudre une seconde fois : sur un nom
en round-robin, les deux pourraient diverger et la route décrirait une adresse
qu'aucun autre check n'a touchée.

Trois décisions y sont pinnées par un test :

- **Un lookup qui échoue est `Unknown`, jamais `Fail`.** Windows rend
  `ERROR_NETWORK_UNREACHABLE` pour une IPv6 sur un réseau sans IPv6 ; le traduire
  en « il n'y a pas de route » serait D20 une fois de plus. Le message brut est
  en outre **localisé**, donc il part dans un fact et jamais dans le résumé
  (Rule 129).
- **Une famille sans route sur deux est un `Warn`**, avec son compte. C'est le
  cas qui vaut d'être signalé : un client qui préfère IPv6 attend avant de
  retomber sur IPv4, et tous les autres checks n'en montrent que le symptôme.
- **Une passerelle absente se dit** (« directly connected ») plutôt que de rendre
  une cellule vide, qui se lirait comme une mesure manquante.

#### Pourquoi l'ARP n'est pas portable, mesuré

Ce n'est pas « plus difficile en Go », c'est **du contenu qui disparaît**. Relevé
côte à côte dans le même conteneur :

```
$ ip neigh show
10.254.254.1  dev eth0      lladdr 5a:94:ef:e4:0c:dd  REACHABLE
fe80::50ed:dcff:fe63:8855 dev services1 lladdr 52:ed:dc:63:88:55  STALE

$ cat /proc/net/arp
IP address     HW type  Flags  HW address           Mask  Device
10.254.254.1   0x1      0x2    5a:94:ef:e4:0c:dd    *     eth0
```

Deux pertes, et chacune touche ce que la section affichait :

1. **L'entrée IPv6 n'est pas là.** `/proc/net/arp` est IPv4 seul.
2. **Les états n'existent pas.** `0x2` est `ATF_COM` — complet. La section avait
   une légende de six états (`REACHABLE`, `PERMANENT`, `STALE`, `DELAY`,
   `INCOMPLETE`, `FAILED`), avec trois couleurs, et `/proc` n'en distingue que
   deux. Les récupérer demande **netlink**, donc une bibliothèque ou du socket à
   la main, sur la plateforme où le défaut D57 n'existe même pas.

Côté Windows, `GetIpNetTable2` **n'est pas exposé par `x/sys/windows`** —
vérifié : `MibIpForwardRow2` y est, `MibIpNetRow2` non. Il faudrait déclarer la
structure et l'appel à la main. Côté macOS, `x/net/route` avec `RTF_LLINFO`.

Soit trois implémentations dont deux à écrire depuis zéro, pour une section dont
le contenu serait amputé sur celle des trois où il était complet. **Supprimée.**

#### Pourquoi le pare-feu n'est pas traduisible

`FirewallChain{Name, Policy, Rules}` est une **structure iptables**. `INPUT`,
`FORWARD`, `OUTPUT` avec une politique par défaut ne sont pas un concept réseau,
c'est le modèle de Netfilter.

Windows a des **profils** — Domain, Private, Public — avec une action par défaut
entrante et sortante, et des règles à plat. Relevé sans élévation sur cette
machine : les trois profils actifs, `DefaultInboundAction = NotConfigured`, et
**391 règles activées**. macOS a `pf`, qui est encore un troisième modèle.

Rien de tout ça ne rentre dans trois colonnes nommées chaîne / politique /
nombre de règles sans mentir sur la forme. **Supprimée.**

Le pare-feu était par ailleurs la seule raison du `--privileged` dans
`docker/topology.go`, donc sa suppression retire **le dernier conteneur
privilégié de l'application** après §3.43.

#### D58 s'est corrigé ici, et c'est §3.44 qui l'a rendu court

Avec deux sources au lieu de cinq, la règle tient en une phrase : **chaque source
porte son propre état**. `netiface.List` ne rend une erreur que si les interfaces
elles-mêmes n'ont pas pu être lues ; des compteurs qui échouent laissent
`RxErrors`/`TxErrors` à `nil`, et la cellule affiche `-`. Un compteur d'erreurs
à zéro parce que personne n'a regardé est exactement le `*bool` de
`SecretVerdict()` sous un autre nom, et
`TestACounterNobodyReadIsADashAndNeverAZero` échoue si le zéro revient.

#### L'onglet est renommé, pas supprimé

À une section, « Topology » ne décrivait plus rien. Le nom devient **Interfaces**,
qui est exactement ce que la vue montre — et la question « je sors par quelle
carte, avec quelle MTU, quel MAC » reste légitime. Le supprimer aurait obligé à
reloger les interfaces dans Diagnostics ou dans Ports, où ni l'une ni l'autre n'a
de place.

La section rendue à la main dans un `viewport` devient une **`datatable`** : tri,
filtre et recherche gratuits, et §3.45 s'y appliquera. Conséquence à ne pas
manquer, et elle a un test : la table a une boîte de recherche là où l'ancien
onglet n'avait aucun champ, donc `InEditMode()` devait cesser de rendre `false`
en dur — sinon un `:` tapé dans la requête ouvre la ligne de commande.

#### Ce que ça donne

| | |
|---|---|
| `internal/netiface` | nouveau — les interfaces, lues dans le process |
| `internal/netcheck` | + l'étage `route`, `Env.Route`, `RouteHop` |
| `internal/docker/topology.go` | **supprimé**, `--privileged` avec |
| `internal/ui/netdiag/topology_model.go` | **supprimé** (792 lignes), plus 729 lignes de tests de parsers |
| lignes | **-231 net**, pour une capacité en plus |
| binaire | 28,08 → 28,14 Mo (**+0,05**) |

`network.tool_image` ne sert plus qu'à **une** chose côté hôte, la trace de
route — et à une autre, côté réseau Docker, que personne n'avait comptée. C'est
[§3.47](#347-la-trace-de-route-est-supprimée-et-networktool_image-avec--done),
qui a supprimé la première et renommé le réglage d'après la seconde.


### 3.45 `datatable` — chaque colonne déclare sa nature, et les largeurs suivent — **done**

Tranché puis fait le 2026-08-25. Ce qui suit est la décision, puis ce que
l'écriture en a appris.

L'entrée s'appelait « des largeurs de colonnes qui regardent le contenu », et
c'était la moitié du sujet. Mesurer le contenu ne dit pas quoi faire quand il ne
rentre pas, et c'est là que sont les défauts.

#### Ce qu'une colonne ne sait pas dire

`Column` porte `MinWidth` et `Flex`, et ces deux nombres répondent pour quatre
questions distinctes :

| Le fait | Exemple | Ce qui le porte aujourd'hui |
|---|---|---|
| la largeur est connue à la compilation | `CRIT` fait 6, `Secrets` 7 | `MinWidth`, comme si c'était une demande |
| la largeur dépend du contenu | `Target`, `Peer Address`, `Remote` | `MinWidth` + `Flex`, sans jamais regarder le contenu |
| la colonne peut disparaître | `IPv6`, `Rx Err` | rien — `shrink` vide ce qui est flexible, dans l'ordre où ça se trouve |
| la colonne ne peut pas être tronquée | un compte, une icône | rien — `142` s'affiche `14…` |

Le dernier est le plus grave et le moins visible : une abréviation se voit, un
chiffre faux non.

#### La mesure

`solveWidths` sur les colonnes réelles de l'onglet Ports :

| largeur du terminal | Proto | State | Local Address | Peer Address | PID | Process |
|---|---|---|---|---|---|---|
| 100 | 7 | 10 | 26 | 26 | 7 | 10 |
| 146 | 7 | 10 | 26 | 26 | 7 | **56** |
| 200 | 7 | 10 | 26 | 26 | 7 | **110** |

À 200 colonnes, Process reçoit 110 cellules pour `svchost.exe` pendant que Peer
Address reste figée à 26 et coupe `[2606:2800:220:1:248:1893:25c8:1946]:443`.
`Flex` dit *qui* reçoit le surplus, jamais *qui en a besoin*.

#### Ce que la colonne déclare

Deux champs, orthogonaux parce que les quatre combinaisons existent dans
l'application :

```go
type Sizing int

const (
	SizingUnset   Sizing = iota // refusé par un test — voir plus bas
	SizingFixed                 // la largeur est celle déclarée, exactement
	SizingContent               // min(contenu mesuré, MaxWidth), plancher MinWidth
)

type Column[T any] struct {
	…
	Sizing       Sizing
	Optional     bool // peut disparaître quand la table ne rentre pas
	MaxWidth     int  // 0 = pas de plafond ; n'a de sens que sur SizingContent
	TruncateHead bool // tronquer le début plutôt que la fin
}
```

| | `Fixed` | `Content` |
|---|---|---|
| **non `Optional`** | `CRIT`, `Secrets`, `Scanned` | `Target`, `Interface`, `Name` |
| **`Optional`** | `Rx Err`, `Tx Err` | `IPv6`, `Peer Address`, `Remote` |

**`Fixed` + `Optional` est ce qui a écarté un enum unique à trois valeurs**
(`Fixed` / `Essential` / `Optional`). Les compteurs d'erreurs d'Interfaces ont
une largeur exacte *et* sont ce qu'on lâche en premier : à 100 colonnes, les
lâcher rend 20 cellules aux deux colonnes d'adresses, qui en ont 10 chacune.
Une seule valeur par colonne ne sait pas le dire.

**`MaxWidth` n'a pas de défaut**, et `0` veut dire pas de plafond. Un défaut
global choisi une fois s'appliquerait à des colonnes dont personne n'a regardé
le contenu — et une adresse IPv6 complète fait 45 cellules, donc le premier
défaut « raisonnable » qui vient à l'esprit retronque ce qu'on vient de
corriger. Il est posé explicitement sur les seules colonnes qui portent des
valeurs sans borne : `Remote` dans `ws`, `Target` dans `:sec`, `Image` dans les
conteneurs.

**`TruncateHead` est un drapeau par colonne, et le défaut est la queue**
(`fit` d'aujourd'hui). Le sens dépend de la colonne et non du `Sizing`, ce qui
est exactement ce qui interdit d'en faire une règle : une URL veut sa fin, une
adresse IP veut son début.

```
Remote, TruncateHead        Remote, en queue          IPv4, en queue (défaut)
…group/sub/project.git      https://gitlab.com/…      192.168.1.…
…group/autre-projet.git     https://gitlab.com/…      192.168.4.…
```

Trois lignes identiques n'identifient rien, et c'est ce que la troncature en
queue donne sur une URL serrée à son `MinWidth` — c'est-à-dire précisément quand
la colonne est la plus difficile à lire. Tronquer toutes les colonnes `Content`
en tête corrigerait les URL, les références d'image et les chemins d'un coup, et
casserait les adresses : le préfixe dit de quel réseau il s'agit, et c'est lui
qui disparaîtrait.

Le laisser à la vue, dans `Cell`, n'est pas possible : `Cell` ne connaît pas la
largeur à laquelle elle sera rendue, et c'est délibéré — c'est ce qui rend la
mesure possible.

#### `MinWidth` redevient un vrai plancher, et `Optional` est ce qui le permet

C'est le cœur de l'entrée. Aujourd'hui `MinWidth` est une demande : `takeFrom`
retire une cellule à la fois à la plus large sans jamais s'arrêter, donc une
colonne descend à zéro et disparaît. Lui donner un plancher **sans
échappatoire** ferait déborder la table dès que la somme des planchers dépasse
la largeur disponible — Rule 116 en défaut pour de bon, et une règle de plus à
inventer sur place.

`Optional` est l'échappatoire. L'ordre de dégradation :

1. les colonnes `Content` rétrécissent vers leur `MinWidth`, en tronquant ;
2. puis les colonnes `Optional` sont **retirées**, la plus à droite d'abord, et
   le calcul reprend — retirer une colonne rend aussi ses deux cellules de
   padding, ce qui est D61 ;
3. dernier recours, quand il ne reste que des colonnes non `Optional` qui ne
   rentrent pas : les retirer quand même, toujours de droite à gauche.

**Le point 3 fait de la déclaration une préférence et non une garantie, et c'est
délibéré** : mieux vaut moins de colonnes justes que toutes fausses. L'ordre des
colonnes est déjà un ordre d'importance dans les tables mesurées, donc droite à
gauche dégrade dans le bon sens —

```
Target  Secrets  CRIT  HIGH  MED  LOW  Scanned
Target  Secrets  CRIT  HIGH  MED  LOW
Target  Secrets  CRIT
Target
```

— et ce qui reste est exact à chaque étape. L'alternative, tronquer les colonnes
fixes en dernier recours, garde toutes les colonnes et rend `142` en `14…` : la
table dit alors quelque chose de faux au lieu de dire moins, et rien à l'écran
ne distingue les deux.

#### Quand mesurer, et sur quoi

**Toutes les lignes visibles.** Les lignes filtrées, à l'écran ou non. Une
recherche qui ne garde que des adresses courtes doit rendre la place, sinon le
filtre ne sert à rien visuellement ; et mesurer les seules lignes à l'écran
ferait bouger les colonnes à chaque flèche — la danse qu'on cherche à éviter,
déclenchée par la touche la plus utilisée. 197 lignes × 6 colonnes sur Ports.

**Aux moments visibles seulement**, jamais sur un tick périodique :
redimensionnement, changement de filtre ou de jeton, changement d'onglet,
drill-down, `ctrl+r`. Le coût est écrit plutôt que découvert : une adresse plus
longue apparue entre deux recalculs reste tronquée jusqu'au suivant. Le cliquet
— élargir sans jamais rétrécir — est plus agréable, mais il faut alors décider
ce qui le remet à zéro, et le tri comme le filtre sont chacun une réponse
défendable ; c'est trois questions de plus pour un confort.

**`datatable` ne peut pas distinguer un tick d'un changement de population** :
les deux arrivent par `SetItems`. C'est donc la vue qui le dit — `SetItems` ne
remesure jamais, `Remeasure()` est explicite. Une exception, et une seule : le
premier remplissage non vide remesure de lui-même, sinon les largeurs restent à
`MinWidth` pour la vie de la vue et rien à l'écran ne le dit.

#### Le contrat sur `Cell`, et le seul test qui compte

`Cell` rend du texte brut (Rule 122), et c'est précisément ce qui permet de le
mesurer : une cellule portant déjà sa couleur mesurerait ses octets
d'échappement, ce que Rule 122 existe pour rendre inexprimable. La mesure passe
par `lipgloss.Width` sur la sortie de `Cell`, avant tout style — la même
inversion que `render.go`.

**`Cell` devient officiellement pure et bon marché.** Le rendu l'appelle déjà
une fois par cellule visible ; la mesure l'appelle une fois par cellule de
*toutes* les lignes visibles, hors écran comprises. Rien ne peut le vérifier —
c'est une closure arbitraire — donc c'est une phrase dans la doc du paquet, pas
un test.

Ce qui *est* vérifiable, et doit l'être : **aucune colonne ne laisse son
`Sizing` à `SizingUnset`**. La valeur zéro ne veut rien dire de raisonnable ici,
et les deux candidats sont pires que l'absence :

- « fixe, non évinçable » ne change rien tant qu'une table n'est pas annotée,
  mais plus aucune colonne n'est évinçable au départ — le dernier recours décide
  tout, et le comportement actuel disparaît sans être remplacé ;
- « content, évinçable » est plus proche d'aujourd'hui, mais rend `Target`
  retirable par défaut, dans les vingt tables à la fois. C'est D12 sous un autre
  nom : un zéro qui ment sur ce que personne n'a déclaré.

`TestEveryColumnDeclaresItsSizing` nomme la table et la colonne, sur le modèle de
`TestEveryFieldCarriesTheAccessorItsKindNeeds` de la vue configuration. Coût :
les 93 déclarations de colonnes ont été regardées une fois, colonne par colonne.

#### Le nom du nombre, et ce qui n'a pas été renommé

`MinWidth` reste `MinWidth`, y compris sur une colonne `Fixed` où il *est* la
largeur. Le renommer en `Width` ne coûtait rien — les 93 déclarations sont de
toute façon toutes touchées par l'annotation — donc seule la lisibilité décidait,
et l'asymétrie penche dans l'autre sens que ça en avait l'air :

| | `Width: 26` | `MinWidth: 26` |
|---|---|---|
| une colonne `Content` rendue à 46 | **faux** | vrai |
| une colonne `Fixed` rendue à 6 | vrai | vrai, et redondant |

`Width` mentirait sur une colonne qui dépasse ce nombre dès qu'il y a de la
place ; `MinWidth` n'est que redondant là où le minimum se trouve être aussi le
maximum. Un nom redondant se supporte, un nom faux non. Deux champs exclusifs
(`Width` pour `Fixed`, `MinWidth` pour `Content`) ont été écartés pour la raison
habituelle : quatre champs de largeur sur une `Column`, et une structure qui en
porte deux invite à remplir les deux.

Une chose qui ne change pas non plus : `askFor` réserve `largeur(titre) + 2` à
toute colonne triable, `Fixed` comprise. Une largeur exacte plus étroite que son
propre en-tête trié perd la flèche ; c'est un défaut de déclaration, pas un cas
à gérer.

#### Ce que ça corrige ailleurs

Toutes les tables sont des `datatable` depuis §3.21, donc le gain est général.
Les cas les plus visibles : `Remote` dans `ws` (une URL longue à côté de colonnes
de comptage à un chiffre), `Target` dans l'inventaire `:sec`, `Image` dans les
conteneurs, et les deux colonnes d'adresses d'Interfaces, dont §3.49 a écrit la
falaise plutôt que de la corriger. D61 tombe avec le point 2.

#### Ce que l'implémentation a mesuré

93 colonnes annotées dans 14 fichiers. Les largeurs, au rendu, sur les deux
tables que D61 avait mesurées :

| | avant | après |
|---|---|---|
| Ports, 146 colonnes | Peer Address **26**, Process **56** | Peer **40**, Process **43** |
| Ports, 200 colonnes | Peer Address **26**, Process **110** | Peer **40**, Process **97** |
| Interfaces, 80 colonnes | IPv4 **0**, IPv6 **0** — et MTU, MAC, les compteurs tous rendus | IPv4 **18**, IPv6 **29**, MTU/MAC/compteurs **retirés** |
| Interfaces, 100 colonnes | IPv4 8, IPv6 8 | IPv4 **18**, IPv6 **22** |

`[2606:2800:220:1:248:1893:25c8:1946]:443` fait 40 cellules ; c'est exactement
ce que Peer Address reçoit dès qu'il y a la place, et le plafond n'a pas eu à
être posé sur cette colonne — la mesure suffit. Et la portée rendue vaut
`largeur − 2` à toutes les largeurs essayées, ce qui est la forme vérifiable de
Rule 116 (voir D61).

#### Quatre choses que l'écriture a apprises

**`Resize` est un moment de mesure, et ça a supprimé la moitié du câblage.**
L'entrée prévoyait de câbler `Remeasure()` dans chaque vue à chaque moment
utile. Mais un redimensionnement *est* un de ces moments — l'utilisateur vient
de changer la place disponible et le contenu n'a pas bougé — et plusieurs vues
appellent déjà `Resize` juste après `SetItems` parce qu'elles recalculent leur
hauteur. Promouvoir la mesure dans `Resize` couvre donc gratuitement les
registries, les résultats de netdiag et les tags du browser. Il ne reste que
**quatre** appels explicites : le drill-down de l'explorateur, le changement de
répertoire de `ws`, le changement d'onglet de `:sec`, et le pliage de l'arbre du
viewer. Les tables rafraîchies au tick — conteneurs, ports, interfaces, status —
n'appellent pas `Resize`, donc elles ne remesurent pas, ce qui est précisément
la propriété recherchée.

**`refreshRows` de `ws` a dû être décroché de `setEntries`.** Les deux passaient
par le même point, et l'un d'eux arrive plusieurs fois par seconde pendant qu'un
scan tourne : la mesure aurait fait bouger les colonnes au rythme du spinner.
C'est la distinction « même population redécorée » contre « nouvelle
population », et elle n'était pas exprimée dans le code avant d'en avoir besoin.

**Un plancher de un, pas de zéro.** `MinWidth` redevient un plancher, mais une
colonne *gardée* ne peut pas descendre en dessous d'une cellule non plus — une
colonne à zéro ne rend rien alors que son padding est déjà dépensé, ce qui est
D61 mot pour mot. « Trop étroite pour servir » et « pas là » doivent rester deux
états, et le second est un retrait. Conséquence visible : un `MinWidth` négatif
est lu comme une cellule et non comme une offre de largeur aux voisines.

**Cinq constantes sont mortes de leur belle mort.** `numColumns` (explorateur,
`:sec`, `ws`), `numMonitorColumns` et `numSSLColumns` n'existaient que pour
nourrir le `largeur − 2 − N×2` des tests de mise en page. Le nombre de colonnes
déclarées ne décide plus du budget, donc elles n'avaient plus de lecteur —
`golangci-lint` les a nommées avant qu'on y pense.

#### Le test qui garde tout ça

`TestEveryColumnDeclaresItsSizing` est un test **de source** : il parcourt
`internal/ui`, trouve les littéraux `datatable.Column[…]` — y compris les
éléments d'un `[]datatable.Column[T]{{…}, {…}}`, qui ne portent pas de type — et
échoue en nommant fichier, ligne et titre de colonne. Il ne pouvait pas être un
test d'exécution : les colonnes vivent dans quatorze instanciations d'un type
générique, donc il n'y a aucune table unique à parcourir, et seule la source les
voit toutes. Le précédent est `internal/ui/keymap`, qui lit les `switch` pour la
même raison.

### 3.46 `ws` — une icône par ligne, et la colonne Type disparaît — **done**

Fait le 2026-08-24, sur le modèle d'`eza` : une colonne de glyphe à gauche du
nom, et la colonne Type supprimée.

```
󰊢 devdesk            main ↑3
󰉋 clients
 go.mod
󰍔 README.md
 .gitlab-ci.yml
󰆍 build.sh
```

#### Les quatre décisions, et ce qu'elles ont donné

| | Décidé |
|---|---|
| Que devient la colonne Type ? | **supprimée** — `detectProjectType`, `formatProjectType` et `Entry.ProjectType` avec |
| Le dépôt git garde-t-il un glyphe ? | **oui, trois genres** : dépôt, répertoire, fichier |
| L'icône est-elle colorée ? | **non** |
| La colonne a-t-elle un en-tête ? | **non**, comme `eza` |

**Ce qu'on remplace n'était pas ce qu'on ajoute**, et l'entrée d'origine s'y
attardait : la colonne Type ne montrait pas un type de *fichier* mais un type de
*projet* — `detectProjectType` cherchait `go.mod` ou `package.json` et répondait
« ce répertoire est un projet Go ». La fonctionnalité a été jugée sans intérêt,
donc elle part entière plutôt que d'être traduite.

**Le troisième glyphe est celui qui justifie la colonne.** Un dépôt git et un
répertoire ordinaire se distinguent maintenant à l'œil, ce qui n'était pas le
cas : la colonne Git Status ne trahit un dépôt que s'il a une branche lisible,
donc un HEAD détaché, un dépôt vide ou un dépôt que git refuse de lire
affichaient une cellule vide et ressemblaient à n'importe quel répertoire. Or
**la moitié des touches de la vue agissent sur `IsGitRepo`** — `S`, `F`, `A`,
`D`, `enter` — et Rule 130 les fait apparaître ou disparaître en conséquence :
l'utilisateur voyait l'aide changer sans que la ligne dise pourquoi.

Un répertoire contenant des dépôts *imbriqués* se lit toujours comme un
répertoire ordinaire. `S`, `F` et `A` agissent dessus aussi, donc un quatrième
glyphe se défendrait ; trois est ce qui a été demandé, et la ligne n'avait de
toute façon aucun autre moyen de le dire.

**Pas de couleur**, contre `eza` : la discipline de `datatable` est qu'une
couleur présente partout n'informe de rien, et une colonne où chaque ligne porte
une teinte affaiblirait celles qui signalent vraiment quelque chose — les
sévérités, les secrets, le statut git.

#### `internal/ui/fileicon`

La table est déclarée, jamais devinée, et la règle de résolution est celle
d'`internal/viewer/detect.go` : **le nom de base est consulté avant
l'extension**, parce que `Dockerfile` n'a pas d'extension et que `Dockerfile.dev`
en a une qui n'est dans aucune table. `.gitlab-ci.yml` tombe du bon côté sans
rien ajouter à la règle. Rien n'est reniflé dans le contenu.

Ce n'est **pas** la table `Kind` du viewer : il y a une douzaine de `Kind`, assez
pour choisir un lexer, et des dizaines d'icônes dont plusieurs partagent un
`Kind` — `.js`, `.ts`, `.py` et `.rs` seraient tous « texte ». Deux questions sur
la même entrée, donc deux tables.

**Les glyphes vivent dans le paquet et non dans `theme/icons.go`**, seul endroit
où ce paquet s'écarte du précédent `ForgeIcon`. Là, le glyphe est dans `theme` et
le vocabulaire dans `internal/forge` parce qu'un paquet de domaine ne doit pas
importer l'UI ; ici les deux moitiés sont de l'UI, et une table dont la clé est
dans un fichier et la valeur dans un autre ne se lit pas une entrée à la fois.
`theme` garde les icônes que l'application nomme — `IconDirectory`, `IconFile`,
`IconGitBranch` — et le paquet les emprunte pour ce qu'il ne sait pas nommer.

**Vingt glyphes ont été ajoutés et vérifiés à l'œil**, un par un, en les rendant
dans un terminal : un codepoint faux ne casse rien, il affiche une tofu box, ce
qu'aucun test ne peut voir. Deux enseignements de ce passage :

- **CSS et HTML étaient inversés** au premier jet. Les quatre `language_*` de
  MDI forment une suite contiguë en ordre alphabétique — csharp, css3, html5,
  javascript — et c'est cet ordre qui tranche, parce que les glyphes sont
  indiscernables les uns des autres dans un diff. C'est écrit dans le code.
- **Ce qui n'a pas de glyphe sûr n'en invente pas.** `.kt`, `.scala`, `.hs`,
  `.zig`, `.tf` et le reste tombent sur `IconCodeFile` : « ceci est du code »
  est moins que nommer le langage et beaucoup plus qu'une boîte vide.

Trois tests tiennent la table honnête sans rien savoir des glyphes :
`TestEveryExtensionKeyStartsWithADot` et `TestEveryTableKeyIsLowercase`
attrapent les entrées **inatteignables** — `filepath.Ext` rend `.go` et la
recherche minusculise, donc une clé `go` ou `README` ne matcherait jamais et le
fichier retomberait en silence sur le glyphe générique — et
`TestNoGlyphIsEmptyOrCarriesStyling` interdit la séquence ANSI que Rule 122
proscrit dans une cellule.

#### Deux choses trouvées en chemin

**Le garde `vocabtest` a sauté sur `.gitlab-ci.yml`**, et il avait raison de
poser la question. C'est un **nom de fichier**, pas du vocabulaire : le fichier
s'appelle ainsi sur le disque quelle que soit la plateforme visée par le
contexte, et un contexte GitHub qui contient un pipeline GitLab veut quand même
le bon glyphe dessus. Exception déclarée avec sa raison, sur le modèle de
`keymap.DeclaredExceptions()`.

**`project_type` a été retiré de l'outil MCP `workspaces_list` aussi.** Il en
avait sa **propre copie** — `projectSignatures` dans `internal/mcp` — donc
supprimer la colonne ne l'aurait pas touché, et la duplication serait devenue un
exemplaire unique qui survit à ce qui l'a motivé.

#### Ce que ça coûte

Une police Nerd Font était **déjà** requise et la colonne Type affichait déjà
`IconGo` et `IconDocker` : ce n'est pas une exigence nouvelle, seulement plus
visible. L'espace est neutre — la colonne Type faisait quatre cellules, l'icône
en prend deux — et la vue reste à onze colonnes.

Les tests lisant la table par index nomment désormais les trois qu'ils utilisent
(`colIcon`, `colName`, `colGitStatus`) : une colonne ajoutée à gauche déplace
trois constantes au lieu de chaque assertion.


### 3.47 La trace de route est supprimée, et `network.tool_image` avec — **done**

Fait le 2026-08-24. §3.41 posait la question « se passer de netshoot », §3.43 et
§3.44 en ont retiré quatre usages sur cinq, et cette entrée retire le
cinquième — en supprimant la fonctionnalité plutôt qu'en la réécrivant. Elle
**ferme D57**.

#### L'inventaire, et l'usage que personne n'avait compté

Après §3.44, le réglage avait deux lecteurs, et le relevé a montré que ce
n'étaient pas la même question :

| Site | Ce qu'il lance | Namespace | Verdict |
|---|---|---|---|
| `netdiag` → `traceCmd` | `traceroute` / `tcptraceroute` | `--network host` — **la VM** | **supprimé** |
| `oci_resources` → `RunDiagnosticContainer` | `ping`, `nc`, `wget` | `--network <networkID>` — **un réseau Docker** | **gardé** |

Le second se trouve sous `:oci` → onglet Networks → `enter` → `c`, ce qui
explique pourquoi trois entrées successives sur netdiag ne l'avaient jamais
croisé. Il est **correct par construction** : « ce conteneur en atteint-il un
autre sur ce bridge » n'a pas de réponse depuis un process hôte. C'est la
distinction qui décide de tout ici — ce qui interroge *cette machine* doit tourner
dans ce process, ce qui interroge *un réseau Docker* doit tourner dedans.

#### Pourquoi la trace part au lieu d'être réécrite

Trois raisons, dans l'ordre où elles pèsent.

**Elle répondait pour la mauvaise machine.** `--network host` est le namespace de
la VM Linux sur Docker Desktop : la trace partait de `10.254.254.3` et suivait la
route de la VM, pas celle de la machine — la même erreur que D55 et D57, sur le
dernier endroit où elle subsistait. La vue le disait (« Traced from the Docker
network tool container »), ce qui est mieux que rien et ne rend pas le résultat
juste pour autant.

**Ce qu'on venait y chercher a une meilleure réponse.** La question posée en
pratique est « pourquoi je n'atteins pas cet hôte », et sa cause la plus fréquente
sur un poste de travail est un VPN qui capture la route par défaut. Le check
**Local route** de §3.44 y répond depuis cette machine, en 2 ms, en nommant
l'interface et l'adresse source. Le chemin saut par saut est réellement perdu ;
il était de toute façon celui de la VM.

**La réécrire coûtait cher et n'était pas acquise.** L'issue 4 de la version
précédente de cette entrée listait trois inconnues — lire le `TIME_EXCEEDED` ICMP
sans socket brute sur trois plateformes, un TTL par paquet que `pro-bing`
n'expose pas, et le mode TCP qui demande de lire l'ICMP d'erreur d'une connexion
sortante. C'était un projet, pour une fonctionnalité dont la valeur venait de
retomber.

#### Ce que ça supprime

| | |
|---|---|
| `internal/docker/netdiag.go` | **entier** — `runDiagHost`, `RunTraceroute`, `RunTCPTraceroute`, `DiagResult` |
| `internal/ui/netdiag/traceroute_formatter.go` | **entier**, 278 lignes |
| `traceCmd`, `traceDoneMsg`, `handleTraceDone`, `startTrace`, `renderTrace`, `traceWorthOffering`, `m.tracing` | le pipeline complet de la touche |
| `network.traceroute_max_hops` | le réglage, son défaut, son champ dans la vue de configuration |
| `keymap.Trace` | **`H` redevient une lettre libre** |
| lignes | **-769 net** |
| binaire | 28,14 → 28,09 Mo (**-0,05**) — soit +0,01 Mo pour §3.44 et §3.47 réunies |

**`H` est remise dans `free`**, à côté de `J`, `Q` et `Z`. Une lettre qu'une
action vient de libérer se redéclare libre, sinon elle reste réservée à un usage
qui n'existe plus — et `TestFreeLettersAreActuallyFree` la vérifie comme les
autres.

#### D57 est fermé, et c'est vérifiable

**Plus aucun `--network host` dans l'application.** C'était la dernière, après
que §3.33 eut rapatrié DNS/ICMP/TCP/TLS/HTTP, §3.43 la table des sockets et
§3.44 les interfaces. Ce qui reste de Docker dans DevDesk ne prétend plus jamais
répondre pour cette machine.

#### Le réglage est renommé, pas supprimé

`network.tool_image` → **`network.connectivity_image`**. Le nom ne disait rien
parce que le réglage servait deux choses sans rapport ; il n'en sert plus qu'une,
donc il est nommé d'après elle, et son commentaire dit ce que l'image doit
porter : `ping`, `nc`, `wget`. **busybox** couvre les trois en 6,81 Mo, contre
874 Mo pour `nicolaka/netshoot` — facteur 128 — et c'est le nouveau défaut.

`curl` était le quatrième besoin ; il est devenu `wget -S -O-`, qui est dans les
**deux** images et rend la même ligne de statut et les mêmes en-têtes, mesuré
côte à côte. Ce n'est donc pas un repli conditionnel : c'est une commande, qui
marche partout.

**La chaîne de migration a trois maillons** —
`docker.network_tool_image` → `network.tool_image` → `network.connectivity_image`
— et un fichier peut se trouver à n'importe quel point dessus. Les deux renommages
tournent avant les défauts, du plus ancien au plus récent, chacun effaçant sa clé
une fois reportée. L'ordre est l'essentiel : `yaml.Unmarshal` n'est pas strict
ici, donc un bloc non migré est ignoré en silence et l'image revient au défaut
sans que rien à l'écran ne le dise — un utilisateur pointant sur son propre
miroir verrait la sonde tirer de Docker Hub. `TestTheImageSurvivesBothRenames`
couvre les deux orthographes retirées, `TestTheNewestKeyWins` un fichier qui les
porte toutes les trois.

Seul le **défaut** change : un `config.yaml` qui nomme déjà `nicolaka/netshoot`
n'est pas réécrit. Ce que le fichier déclare n'est jamais corrigé d'office.

#### Ce qui est abandonné avec la trace

`tcptraceroute` et son repli — construits une heure plus tôt dans cette même
branche — partent avec la fonctionnalité qu'ils servaient. L'arrangement méritait
d'être noté au passage, parce qu'il resservira : un shell qui décide lui-même
dans **un seul conteneur**, avec des marqueurs explicites en tête de sortie
(`TCP:` / `ICMP:`), plutôt qu'un `command -v` en conteneur séparé qui double la
latence, ou qu'un message d'erreur reniflé dans la langue de l'image. C'est
l'arrangement `IPTABLES:` / `UNAVAILABLE:` de `RunFirewallRules`, que §3.44 avait
supprimé avec le pare-feu.

### 3.48 Un raccourci sans objet est grisé, pas supprimé — **done**

Fait le 2026-08-25. Rule 130 disait de **masquer** l'entrée d'une action qui ne
s'applique pas ; elle dit maintenant de la **griser**. Le mécanisme est écrit une
fois, et une seule vue est migrée — `workspaces`, qui porte les deux causes et
sert de référence aux suivantes.

#### Ce qu'on achète

La colonne de raccourcis ne se réorganise plus quand le curseur descend d'une
ligne. C'est le seul argument, et il suffit : on la lit du coin de l'œil, et une
liste qui bouge sous le regard ne se lit plus. Sur la vue `workspaces`, cinq
entrées apparaissaient et disparaissaient — `enter`, `W`, `S`, `F`, `N`, `Y` —
donc la colonne se recomposait à presque chaque déplacement.

`TestTheShortcutColumnDoesNotMoveWithTheCursor` est la forme vérifiable de la
phrase : sur chaque ligne du fixture, et sur une liste vide, la **suite des
touches** doit être identique. Ce qui varie est `Disabled`, plus le libellé de
`enter` — qui ouvre un fichier dans le viewer et les findings d'un dépôt scanné,
deux actions qui ne peuvent pas s'appliquer ensemble.

#### La ligne : mode contre état

| | Ce qui change | Pourquoi |
|---|---|---|
| **Mode** — formulaire, confirmation, sélection | la liste entière | ce n'est pas le même vocabulaire ; griser `enter → Create` dans une table afficherait la réunion de tous les modes |
| **État** dans un mode | l'entrée reste, grisée | la colonne ne doit pas bouger |

Deux causes de grisage, et deux seulement : l'état de la **ligne**, et une
indisponibilité **globale**. Une opération en cours n'en est pas une — elle
change à chaque tick, le spinner de la ligne le dit déjà, et une entrée qui
clignote dit le contraire du point.

#### Un seul calcul, deux lecteurs

`internal/ui/workspaces/availability.go`. Un `actionSet` de
`actionState{Reason string}`, raison vide = disponible : un seul champ, donc le
booléen et le motif ne peuvent pas diverger. `GetShortcuts` le lit pour griser,
`m.guard` pour refuser. C'est la forme de `MatchRanges` du viewer — une seule
réponse pour deux questions — et l'inverse de ce que `scan.Categorize` et
`Result.SecretVerdict` ont eu chacun à défaire.

Le motif n'est jamais dans le header : il n'y a pas la place, et une colonne de
raisons se lirait moins bien qu'un gris. Il va au footer quand l'utilisateur
appuie quand même, en `Warn` (Rule 128) : **le gris dit « pas maintenant », la
touche pressée dit pourquoi.**

`TestNoGreyedKeyEverActs` parcourt chaque touche sur chaque ligne du fixture et
vérifie qu'une touche grisée ne change ni le mode, ni les scans en cours, ni le
sync, ni n'ouvre de modale — avec un compteur qui refuse de passer si aucune
touche grisée n'a été rencontrée, sinon la boucle passerait en ne testant rien.

#### Les outils, et la règle qu'ils imposent

`S` et `A` étaient offertes sans qu'on ait jamais regardé si un scanner
existait : sans Trivy ni Gitleaks, le scan partait et produisait un rapport
vide. `scan.CheckDependencies` lance des `exec.LookPath`, un `--version` par
outil et un `docker images -q`, donc elle passe par un `Cmd` (`DepsCheckedMsg`)
— jamais dans `New` ni dans `View` (Rule 110). Un seul scanner suffit.

**Ne pas savoir n'est pas savoir que non.** `deps` est un pointeur : `nil` veut
dire que personne n'a encore regardé, et `S`/`A` restent offertes jusqu'à ce que
la réponse arrive. Griser trois frames pour dégriser ensuite se lit comme une
panne — c'est D20 à l'échelle d'une touche, et c'est le `*bool` de
`SecretVerdict` un écran plus loin.

La vue prêtée pour une sélection ne lance pas la vérification : elle n'offre ni
`S` ni `A`, donc ce serait trois sous-processus pour une question que personne
ne pose.

#### Deux défauts trouvés en chemin

- **`W` était offerte sur un dépôt sans remote.** Le raccourci était conditionné
  à `IsGitRepo` alors que `openInBrowser` exige `GitRemoteURL` et retournait
  `m, nil` sinon. Le prédicat lit maintenant le remote, et l'appui dit pourquoi.
- **Une liste vide n'affichait aucune ligne d'info.** `RenderFooter` rendait deux
  lignes vides au lieu de la ligne d'info que Rule 124 budgète, donc tout message
  de footer y était avalé — et c'est exactement là qu'un refus atterrit (`Y` sur
  une liste vide n'a rien à copier). Un refus que personne ne peut lire est pire
  que pas de refus.

#### `N` reste allumée, et c'est le contre-exemple utile

Rule 130 masquait `N` quand un dépôt git était sélectionné. Mais `N` crée un
répertoire dans le répertoire **parcouru** — elle n'agit pas sur la sélection.
Le masquage disait donc « sans objet » d'une action qui marchait ; la griser
répéterait le mensonge, et la refuser serait une régression. Elle sort du
dispositif.

C'est la règle générale que l'entrée dégage : **le grisage décrit ce que
l'action peut faire, pas ce qu'on préférerait que l'utilisateur ne fasse pas.**

#### Les autres vues — fait

Toutes migrées dans la foulée. `shortcut.Availability` a été remontée dans
`internal/ui/shortcut` : le type ne portait qu'un champ et huit paquets en
avaient besoin. Les helpers de test sont dans `internal/ui/testutil`
(`ShortcutDisabled`, `ShortcutEnabled`, `HasShortcut`, `ShortcutKeys`), pour la
même raison.

| Vue | Grisé | Continue de remplacer la liste |
|---|---|---|
| `dashboard` | `tab` sans second onglet, `R`/`I` sans session | — |
| `configuration` | `←→` et `space` selon le kind du champ focusé | — |
| `explorer` | `W` sans page web ; tout le reste pendant un chargement | les modes, l'écran déconnecté |
| `security` | inventaire `enter`/`S`/`A`/`/`, résultats `X`, détails `o` | inventaire / résultats / détails, la recherche qui a le clavier |
| `netdiag` | `/` et `.` pendant la lecture des interfaces | les onglets, les états, la recherche |
| `viewer` | `←→`, `w`, `v`, `/` selon l'affichage | le **kind du document** |
| `oci_resources` | images `enter`/`N`/`S`/`D`, registries `N`/`E`/`U`/`D`/`→`/`←`, browser `enter`, les contrôles des trois formulaires | les onglets, les formulaires, le `docker pull` en cours |
| `containers`, `status` | rien — leurs seules conditions étaient déjà des modes | |

**Le kind d'un document est un mode, pas un état.** C'est la seule vue où la
ligne se déplace, et elle se déplace dans l'autre sens : le viewer montre **un**
document, ouvrir un autre document est une nouvelle invocation. Griser `t`, `F`,
`V` et `v` sur chaque fichier texte ajouterait quatre lignes grises permanentes
pour dire « pas pour ce genre de chose, jamais » — ce qui n'est pas « pas
maintenant ». Ce qui varie *dans* un document — l'arbre contre le texte — est
grisé comme partout ailleurs.

**Une action se justifie, un contrôle non.** Le refus au footer vaut pour le
vocabulaire majuscule, `enter` et `→`. Pour `←→` sur un champ qui n'est pas à
cycle, `space` sur ce qui n'est pas une case, `tab` quand il n'y a qu'un onglet,
le grisage suffit : il n'y a rien à expliquer, et une ligne de footer à chaque
flèche perdue dans un formulaire serait du bruit.

#### Trois défauts trouvés en migrant

- **L'onglet Images annonçait une frame de spinner comme une touche.** Pendant
  un scan, `N`, `S` et `D` étaient remplacées par une entrée lisant
  `󰑐  scanning...`, dans la colonne qui liste les raccourcis — un état déguisé
  en binding, alors que la cellule Scanned de la ligne porte déjà ce spinner
  (Rule 139). `TestNoShortcutAdvertisesAGlyphAsAKey` teste la zone à usage privé
  d'Unicode plutôt qu'une liste de glyphes, donc rien n'est à tenir en phase
  avec `theme/icons.go`.
- **Le dashboard n'avait aucun message de footer.** Il budgète une ligne d'info
  (Rule 124) et la laissait vide en permanence, donc `R` et `I` sans session
  tombaient dans le silence sans nulle part où dire pourquoi.
- **`R` vérifiait moins que `I`.** Elle ouvrait une URL construite depuis un
  backend dont la session n'était pas vérifiée. Un seul `forgeLinks()` pour les
  deux touches referme ça.

#### N'ont pas été touchées

Des touches toujours annoncées et parfois inertes, qui n'ont jamais été masquées
et sortent donc du périmètre de cette entrée : `K`, `D`, `T`, `L`, `enter` de
`containers` sur une liste vide, `K` de netdiag/Ports sur une ligne sans PID,
`N` et `D` de l'explorer sans nœud sélectionné. Elles relèvent de la même règle
et se grisent de la même façon, mais c'est du grisage neuf, pas une migration.


### 3.49 `net` — `K` dit quand elle ne peut pas, et les adresses se séparent par famille — **done**

Fait le 2026-08-25. Deux défauts voisins, tous les deux dans le prolongement de
§3.43 (le kill signale avec les droits de DevDesk) et de §3.48 (une touche sans
objet est grisée). Ils étaient nommés « hors périmètre » à la fin de §3.48 ; les
voici.

#### Deux questions, et une seule a une réponse avant l'appui

| | Réponse | Où |
|---|---|---|
| « y a-t-il un processus à signaler ? » | **oui, avant l'appui** — la socket porte un PID ou non | grisage (Rule 130) |
| « le système va-t-il accepter le signal ? » | **non** — seul l'essai le dit | message d'échec |

**Pas de PID, pas de touche.** `List` laisse `PID` vide quand le système
n'attribue pas la socket — un PID de zéro n'est pas un processus, et une ligne
affichant `0` aurait l'air tuable alors que 0 est un groupe de processus entier
sous Unix. `K` est donc grisée sur ces lignes, et sur une table vide. Elle était
annoncée partout et ne se plaignait qu'une fois pressée.

**Une ligne avec un PID reste allumée même quand le refus est certain.** C'est
la ligne de partage : le header dit ce que l'application peut *tenter*, pas ce
que le système va accorder. Griser d'après une supposition de droits serait
mentir dans l'autre sens — et sur Windows la seule façon de savoir est
d'appeler `OpenProcess`, c'est-à-dire de faire l'essai.

#### Le refus du système ne disait pas pourquoi

`handleKillResult` journalisait `msg.err` puis affichait `Failed to kill PID
1234` : l'erreur était jetée avant l'écran, donc « accès refusé » et « ce
processus n'existe plus » devenaient la même phrase — et l'utilisateur n'avait
aucune raison de soupçonner qu'il lui fallait des droits. C'est le cas le plus
fréquent depuis §3.43, précisément parce que le kill ne part plus dans un
conteneur privilégié.

`killFailureMessage` classe avec `errors.Is` à travers le `%w` que `ports.Kill`
applique déjà :

| Sentinelle | Message |
|---|---|
| `os.ErrPermission` | `Refused by the system — DevDesk cannot signal PID N` |
| `os.ErrProcessDone` | `PID N is no longer running` |
| le reste | `Failed to kill PID N — check logs` |

**L'erreur de la plateforme ne touche jamais l'écran.** Mesuré sur cette
machine : PID 4 répond `OpenProcess: Accès refusé.` — en français. C'est la
règle du stage `route` (§3.44) : une chaîne localisée va dans un fait ou dans un
log, jamais dans une phrase que l'application compose.

#### Ce que la mesure a corrigé dans le commentaire

La branche « already gone » **ne se déclenche pas sous Windows**, et c'est
mesuré plutôt que supposé : un PID inexistant échoue `OpenProcess` avec
`ERROR_INVALID_PARAMETER`, qui ne correspond à aucune des deux sentinelles et
tombe donc dans le message générique. Mapper ce code serait une supposition — un
argument réellement invalide renvoie la même chose — et la ligne disparaît au
rafraîchissement suivant, deux secondes plus tard. La branche est gardée parce
que `ESRCH` correspond bien à `os.ErrProcessDone` sous Unix.

C'est aussi ce qui a fait écrire la règle en toutes lettres : **le header
répond de ce que l'application peut tenter, le footer de ce que le système a
répondu.**

#### Et l'onglet Interfaces sépare IPv4 et IPv6

Une colonne par famille, au lieu d'une colonne `Addresses` qui les mêlait sur
une ligne. On lit une notation de haut en bas plutôt qu'un mélange de gauche à
droite, et le filtre atteint les deux.

**Le tri se fait dans `List`, pas dans la vue.** Chaque adresse y est encore un
`net.IP`, donc la famille est un fait : `To4()` répond aussi pour une adresse
IPv4-mappée (`::ffff:192.0.2.1`), ce qui est correct — c'est une adresse IPv4,
quelle que soit la notation d'arrivée. Une vue qui redécouperait
`AddressList()` analyserait un texte que ce paquet vient de produire, et devrait
décider ce que veut dire une entrée illisible : une question qui n'existe que
lorsqu'on a jeté le type. D'où `IPv4 []string` et `IPv6 []string` en place
d'`Addresses`.

Une famille sans adresse rend un tiret grisé et non une cellule vide : une
machine sans IPv6 n'a pas d'adresse IPv6, ce qui est un fait sur elle, alors
qu'une cellule blanche se lit comme une lecture qui a échoué. C'est la
distinction que fait déjà la colonne MAC.

**Les deux colonnes sont flexibles, et c'est ce qui empêche l'une de
disparaître.** `datatable.shrink` reprend d'abord aux colonnes flexibles et
toujours à la plus large, donc deux d'entre elles se nivellent l'une contre
l'autre. Avec le flex sur IPv6 seule — c'était le premier jet — elle absorbait
tout le déficit et se rendait **à largeur zéro, en-tête compris, dès une
centaine de colonnes**, ce qui est un terminal ordinaire. Mesuré au rendu, pas
déduit.

En dessous d'environ 88 colonnes, les six colonnes fixes prenaient tout et les
deux colonnes d'adresses étaient évincées. Cette falaise appartenait à la table
et non au découpage — la colonne `Addresses` unique avait la même — et elle a
été écrite plutôt que dissimulée : `MinWidth` était une demande et non un
plancher, et en donner un au solveur changerait toutes les tables de
l'application.

[§3.45](#345-datatable--chaque-colonne-déclare-sa-nature-et-les-largeurs-suivent--done)
a changé toutes les tables de l'application. `MinWidth` est un plancher, et ce
qui cède au-delà est une colonne entière : MTU, puis MAC, puis les deux
compteurs, parce que ce sont elles qui déclarent `Optional` ici. À 80 colonnes
les adresses reçoivent 18 et 29 cellules au lieu de zéro chacune. C'est le
scénario qui a justifié le point 2 de §3.45, et c'était la bonne décision de
l'écrire ici en attendant plutôt que de bricoler un plancher local.

### 3.50 Un fichier de règles gitleaks est monté, et un scan qui n'a rien lu n'est plus « propre » — **done**

Fait le 2026-08-25. C'est [D56](#11-fixed), et c'est le prérequis que
[§3.42](#342-plumber--un-score-de-sécurité-de-pipeline-par-dépôt--done) s'était donné :
`scan.plumber_config` est le même réglage pour un autre outil, donc le corriger
ici évite de l'écrire deux fois faux.

#### Ce que la mesure a changé au plan

L'entrée D56 prévoyait deux choses : monter le fichier, et faire d'un `--config`
illisible une erreur. La seconde s'est révélée plus large que prévu, parce que
le comportement de gitleaks n'est pas celui que le code supposait. Relevé sur
**v8.30.1**, image `zricethezav/gitleaks`, trois exécutions réelles :

| Situation | code | stdout |
|---|---|---|
| dépôt propre | **0** | `[]`, 3 octets |
| secrets trouvés | 1 | le rapport JSON |
| `--config` introuvable | 1 | **0 octet** |
| `--config` présent mais TOML invalide | 1 | **0 octet** |

Le commentaire que `RunGitleaks` portait — « exit 1 with no report means it ran
and found nothing » — décrit donc quelque chose que gitleaks ne fait pas. Un
dépôt propre sort en `0` et écrit `[]` ; c'est le **rapport** qui sépare un
résultat d'un échec, jamais le code de sortie seul. La branche qui avalait
« exit 1, stdout vide » n'avait aucun cas légitime, et c'est elle qui
transformait l'échec du `--config` en icône verte.

Conséquence : la correction est plus large que le montage. Elle couvre le
fichier absent, le TOML malformé, et n'importe quel fatal qui n'écrit pas de
rapport — dans les **deux** modes, sans que DevDesk ait à lire le message de
gitleaks pour deviner ce qui s'est passé. C'est ce qu'un `os.Stat` préalable
n'aurait pas su faire : un fichier présent et illisible par gitleaks passe le
stat.

#### Les trois décisions que D56 attachait

**1. Un `--config` illisible est une erreur, dans les deux modes.** Elle vient
du point ci-dessus, et elle arrive avec la raison de gitleaks : `exitError`
porte déjà son stderr, donc l'utilisateur lit *unable to load gitleaks config,
err: While parsing config: toml: expected character =* plutôt qu'un « Failed ».
C'est la seule chose qui puisse le dire — rien du côté DevDesk ne sait si le
fichier manquait ou ne se parsait pas.

**2. Un chemin relatif est fixé au chargement.** `ExpandPaths` le rend absolu
contre le répertoire de travail de DevDesk. Un relatif ne veut pas dire la même
chose des deux côtés de la frontière Docker, et maintenant que le fichier est
monté les deux lectures ne désigneraient pas le même fichier. Le résoudre une
fois, au chargement, fait que le chemin monté et le chemin affiché dans
`:config` sont la même chaîne. Un chemin vide reste vide : le transformer en
répertoire courant donnerait un répertoire à parser comme du TOML.

**3. Le point de montage est écrit, pas supposé.** `gitleaksConfigMount =
"/gitleaks.toml"`, à la racine du conteneur, parce que c'est le seul endroit où
rien d'autre ne peut être : la cible est montée sur `/scan`, donc aucun fichier
du dépôt scanné ne peut atterrir à côté, et la racine de l'image est un arbre
Alpine ordinaire sans `/gitleaks.toml` (vérifié). Un test énonce l'invariant
plutôt que la valeur : le montage n'est pas sous `containerScanPath`.

#### Une quatrième garde, pour une raison qui n'est pas la détection

`checkGitleaksConfig` refuse le fichier avant de démarrer quoi que ce soit, et
ce n'est **pas** un second calcul de la question précédente : c'est un effet de
bord de `docker run` qui est en jeu. Un `-v` sur un chemin hôte qui n'existe pas
n'échoue pas — il **crée un répertoire** à cet endroit et le monte. Mesuré sur
Docker Desktop 29.7.2 : un `ghost/nope.toml/` est apparu sur le disque, parent
compris. Une faute de frappe dans `gitleaks_config` sèmerait donc des
répertoires, une fois par scan.

#### La mesure de bout en bout

Une règle qui n'existe que dans le fichier monté se déclenche, avec la commande
que DevDesk construit :

```
docker run --rm -v <cible>:/scan:ro -v <config>:/gitleaks.toml:ro \
  zricethezav/gitleaks detect --source /scan --gitleaks-ignore-path /scan \
  --report-format json --report-path /dev/fd/1 --no-git --config /gitleaks.toml
```

→ `exit 1`, 452 octets de JSON, un finding `devdesk-marker`. Avant la
correction, la même intention donnait 0 octet et un dépôt réputé propre.

Le `-v` est ajouté **avant le nom de l'image** — tout ce qui vient après est
l'argv de gitleaks — et un test compare les deux positions dans la chaîne
plutôt que de faire confiance à l'ordre des `append`.

#### Ce que ça laisse pour §3.42

`scan.plumber_config` a maintenant un précédent complet à copier : un point de
montage nommé dans le paquet, un drapeau qui désigne le montage, un chemin rendu
absolu au chargement, et une règle sur ce qui distingue un résultat d'un échec.
Reste à mesurer ce que l'image `getplumber/plumber` a besoin de voir, ce qui est
le point 1 de « Ce qui reste ouvert » de §3.42.

### 3.51 Le YAML fusionné dans le viewer, et un finding plumber qui sait où il pointe — **fait, autrement que prévu**

Deux moitiés séparables, nées de la même mesure : **plumber n'analyse pas le
`.gitlab-ci.yml` du dépôt, il analyse ce que GitLab en fait**, et rien dans
DevDesk ne montre ce que c'est.

Relevé sur `devsecops/examples/.../notes-mvp/notes-backend`, dont le fichier
déclare **un seul** `include` :

| | fichier local | configuration résolue |
|---|---|---|
| includes | 1 | **15** (7 fichiers de template, 8 composants) |
| jobs | 3 | **21** |
| images | 0 | **9** |
| YAML | 15 lignes | **120 Ko**, 2 756 lignes |

L'expansion est transitive et c'est **le serveur** qui la fait. Un utilisateur
qui lit un finding plumber lit donc le verdict d'un document qu'il n'a jamais
vu, et qui n'existe nulle part sur sa machine.

#### Moitié 1 — afficher le fusionné

**La destination est le viewer, pas un écran de plus.** `internal/viewer` a déjà
trois producteurs (`ws` sur un fichier, `containers` sur `enter` et sur `L`) ;
celui-ci est le quatrième. `KindYAML` est déclaré, coloré, cherchable — il n'y a
rien à construire pour l'affichage, seulement une `Source`, sur le modèle
d'`inspectSource`.

Sept décisions, chacune reprise d'une règle qui existe déjà :

- **Le document est demandé au forge du contexte, et à lui seul.** C'est la règle
  de §3.17 et §3.42 : `git.SameHost` d'abord, `LoadForgeToken` seulement une fois
  l'hôte reconnu — un dépôt d'un autre hôte ne fait pas atteindre le magasin de
  secrets. Sans session, la touche est grisée avec un motif (Rule 130).
- **C'est la configuration d'une *branche côté serveur*, pas du working tree.**
  La branche courante est passée comme `Scanner.ciOptions` le fait, et **le
  header doit le dire** : un template modifié localement et non poussé n'est pas
  dans ce document, et rien d'autre à l'écran ne le distingue d'une expansion
  locale. C'est D35 sous une autre forme — une lecture juste dont la fraîcheur
  n'est pas celle qu'on croit.
- **GitHub n'a pas d'équivalent, et ça se déclare.** Il n'y a pas d'expansion
  côté serveur pour les `uses:` ; la branche GitHub de plumber lit
  `.github/workflows` en local. C'est une différence de *ce que la plateforme
  peut promettre*, donc elle appartient à `forge.Shape` — précédent
  `Shape.PermanentDelete` —, pas à une condition écrite dans la vue.
- **Rien n'est écrit sur disque.** Le viewer prend une `Source`, donc les octets
  restent en mémoire. Le fichier temporaire supprimé par §3.25 ne revient pas.
- **La lecture est réseau, donc un `Cmd`**, annulable, avec le spinner au footer
  et la table qui reste (Rule 139) — 120 Ko sur une instance interne, mais le
  chiffre n'est pas garanti.
- **La touche vient de `keymap.Free()`** — `J`, `Q`, `Z` — et
  `TestFreeLettersAreActuallyFree` se met à jour dans le même commit. Une action
  ne s'invente pas une touche (§3.26).
- **Le graphe d'includes vaut le document.** `includes[]` est ce qui explique
  *pourquoi* le fusionné contient 21 jobs, et ça ne rentre pas dans un document
  YAML. Reste à trancher : un second affichage, ou des lignes de commentaire
  synthétisées en tête du document. La seconde option ment sur le contenu — le
  fusionné ne les contient pas — donc la première est probablement la bonne.

#### Moitié 2 — cibler la ligne signalée

C'est la demande qui a motivé l'entrée, et elle part d'une gêne réelle. Relevé
sur `additional-services/image-import/totem-web`, dont le `.gitlab-ci.yml` fait
**8 lignes** :

```
HIGH ISSUE-411  pipelineMustNotExecuteUnverifiedScripts
job:  .skopeo-check-image-update-base
File: …/totem-web/-/blob/825403b0…/.gitlab-ci.yml#L4
```

L4 est la dernière ligne de l'entrée `include:`. Le job n'est pas dans ce
fichier — il est **deux niveaux plus bas**, dans le composant
`skopeo/check-image-update@2.5`, tiré par `docker-image-external-build`. Le
`#L4` est donc un *renvoi vers l'include responsable*, pas une localisation ; et
rien à l'écran ne dit que c'en est un, ce qui est le vrai défaut.

**Ce n'est pas un bug de plumber.** L'API renvoie deux choses sans lien : le
graphe (`includes[]`) et le texte (`merged_yaml`). Il n'y a **aucune provenance
par ligne**, donc pointer l'include est le maximum disponible depuis le dépôt
qu'on lui a demandé.

**Mais `includes[]` porte de quoi retrouver la ligne, et c'est mesuré.** Chaque
entrée porte un `blob` **épinglé sur un SHA** et nommant le fichier exact :

```json
{ "type": "component",
  "location": "…/components/skopeo/check-image-update@2.5",
  "blob": "…/components/skopeo/-/blob/0576cd05…/templates/check-image-update/template.yml",
  "raw": null,
  "context_project": "devsecops/pipelines/templates/docker/docker-image-external-build" }
```

En récupérant ce fichier **à ce SHA** et en y cherchant la clé du job, on obtient
la localisation exacte. Vérifié de bout en bout :

| | |
|---|---|
| fichier amont au SHA épinglé | 224 lignes |
| `.skopeo-check-image-update-base:` | **L208** |
| le motif qui a déclenché ISSUE-411 | **L106** et **L115** |

soit le permalien que le finding aurait dû porter :
`…/components/skopeo/-/blob/0576cd05…/templates/check-image-update/template.yml#L208`.

**Deux niveaux de précision, et il faut les distinguer plutôt que promettre le
second :**

1. **Toujours** — ouvrir le fusionné positionné sur le job. Le nom du job est
   une clé exacte du document, et le viewer sait déjà filtrer et surligner
   (`MatchRanges`, §3.29). Les lignes fautives *sont* là, sous les yeux.
2. **Quand le job est défini textuellement dans un include** — remonter au
   fichier amont, à son SHA, à sa ligne. Un aller-retour API par entrée du
   graphe, à la demande, pour **un** finding : 4 appels sur `totem-web`, 15 sur
   `notes-backend`. Jamais pour tous les findings d'un scan.

**Le cas 2 échoue légitimement**, et il doit alors retomber sur le cas 1 en le
disant : un job dont le nom est construit (`$[[ inputs.job-name ]]`), assemblé
par `extends` ou par `!reference`, n'apparaît littéralement dans aucun include.
Un « introuvable » silencieux ferait croire que le job vient de nulle part.

**Ce qu'on ne tente pas** : reconstruire une correspondance ligne → origine sur
tout le document. Il faudrait refaire l'expansion — résoudre chaque include,
appliquer `extends`, `!reference` et les `inputs` — c'est réimplémenter le moteur
CI de GitLab, et le résultat serait faux quelque part sans qu'on sache où. Le
serveur donne déjà la bonne réponse ; on ne va pas en fabriquer une seconde.

**Une fragilité à écrire dans le code plutôt qu'à découvrir** : pour un
composant, `raw` est **null** — seul `blob` renseigne. Le projet, le SHA et le
chemin s'en extraient par la forme d'URL `/-/blob/<sha>/<path>`, puis le contenu
se lit par `repository/files/<path>/raw?ref=<sha>`. C'est de la chirurgie sur une
URL, et c'est la seule source ; un test doit fixer la forme attendue.

#### Le cache — deux, pas un, et un seul a une péremption

Demandé le 2026-08-27 : garder le document construit, pour le réutiliser d'un
finding à l'autre et d'une session à l'autre sans rescan. C'est juste, et ça se
sépare en deux caches dont les propriétés n'ont rien à voir.

| | ce qu'il garde | clé | péremption |
|---|---|---|---|
| **documents amont** | le contenu d'un fichier d'include **à un SHA** | (projet, chemin, sha) | **aucune** |
| **YAML fusionné** | le document résolu | (hôte, projet, branche) | réelle, et **non décidable hors ligne** |

**Le premier est gratuit, et c'est celui qui sert la demande.** Un blob à un SHA
est immuable par construction, donc l'entrée n'a pas d'âge, pas de
rafraîchissement, pas de bouton. C'est exactement le cache qui répond à « les
autres findings » : un second finding dans le même composant tape dedans sans un
appel réseau. Et `includes[].blob` *porte* le SHA — il n'y a rien à deviner.

**Le second porte un piège qu'il faut écrire plutôt que découvrir : le fusionné
peut changer sans que le dépôt bouge.** `totem-web` inclut avec `ref: "1"`, un
ref mutable ; le template derrière (SHA `fe477132…`) peut avancer sans un seul
commit chez l'utilisateur. Une clé sur le HEAD du dépôt consommateur est donc
insuffisante, et la fraîcheur ne se vérifie qu'en **redemandant au serveur** —
ce que le cache existe pour éviter.

Conséquence : **c'est un cache à âge visible, pas un cache à invalidation.**
Le précédent est dans le paquet — la colonne Members de l'onglet Registries
affiche `count · TimeAgo(discovered_at)` pour cette raison exacte : *un cache
dont l'âge n'est pas visible a l'air courant quoi qu'il contienne*. Même
traitement ici, et `ctrl+r` redemande.

**Une invalidation partielle est gratuite, et son asymétrie doit être écrite.**
La réponse du lint porte un `context_sha` par entrée, et celui de l'entrée
racine est le HEAD que **le serveur** a pour cette branche. Le comparer au HEAD
local ne coûte aucun réseau :

- différent ⇒ **le document est périmé**, on redemande ;
- égal ⇒ **on ne sait rien**, à cause du ref mutable ci-dessus.

Écrire cette asymétrie noir sur blanc est la moitié du travail : sans elle, le
prochain lecteur la prendra pour une invalidation et croira le cache sûr.

**La portée suit la règle de §3.39, et la règle donne la réponse.** Ce qui
décide est de savoir si la **clé** est par contexte. Un document fusionné est
identifié par (hôte, chemin de projet, branche) — pas par le chemin du clone
local, pas par le contexte. Deux contextes visant le même GitLab et le même
projet regardent le même document. Donc **non scopé**, comme `ImageScanCache`.
Et c'est la clé sur l'identité *distante* plutôt que sur le chemin local qui
fait que deux clones du même projet partagent une entrée — ce qu'une clé sur le
chemin manquerait.

**Le document doit être au moins aussi frais que le finding.** Un finding vient
d'un scan à T1, une entrée de cache de T0. Si T0 < T1, le job a pu bouger et la
ligne sur laquelle on saute n'est pas celle que le finding visait. L'entrée
enregistre donc le jeu de SHA dont elle est issue, et le saut doit savoir dire
« ce document est antérieur au scan » plutôt que de sauter en silence. Sans ça
c'est la forme de D35 : une lecture périmée présentée comme courante.

**Taille et forme.** 120 Ko pour un document, mesuré. Donc l'index d'un côté et
les documents en fichiers adressés par contenu de l'autre — la forme des deux
caches de scan (`workspace-results/<sha256>.json`), pas un blob dans l'index. Un
plafond sur le modèle de `scan.max_cached_reports: 50`.

**Le piège de §3.38 est disponible ici aussi** : `internal/cache/readonly.go`
existe parce que trois écritures se cachaient derrière ce qui ressemblait à une
lecture — le `MkdirAll` des constructeurs **et** celui des `Load*`, qui créait le
répertoire d'un résultat absent. Aucun outil MCP n'a besoin de ce cache-ci, mais
le constructeur ne doit pas écrire sur un chemin de lecture, ou il faudra une
seconde `readonly.go`.

#### Ce qui a été construit, et les trois choses que la mesure a démenties

Fait le 2026-08-27. `:sec` → un dépôt → `enter` → onglet CI → **`o`**. Le
document arrive en YAML coloré et cherchable, avec les findings écrits dedans.
Trois points du plan ci-dessus n'ont pas survécu au contact du réel, et chacun a
été démenti par une mesure plutôt que par un avis.

**1. La touche n'est pas une majuscule libre.** Le plan disait « une lettre de
`keymap.Free()` ». `o` est une **dérogation déclarée**
(`keymap.exceptions`, surface `security/results/ci-tab`), sur le précédent de
`c` et pour la raison écrite là-bas : l'action n'existe que sur un onglet, et
brûler une des trois dernières majuscules libres pour ça coûte plus que ça ne
rapporte. `o` plutôt qu'une autre lettre parce qu'elle veut **déjà** dire
« ouvrir ce que cet écran désigne » dans l'état détail de la même vue.

**2. Il n'y a pas de gouttière : les findings sont écrits en commentaires
YAML**, sur la ligne de définition du job.

```yaml
skopeo-check-image-update:  # plumber: ISSUE-411 (HIGH)
```

C'est mieux à trois titres, et ça n'a rien coûté au viewer : le commentaire
voyage avec la ligne quel que soit le repli — donc `docLine` et `wrapTokens` ne
sont pas touchés —, le lexer YAML le colore déjà, et `/ISSUE-411` le trouve.
Les numéros de ligne, que le plan supposait nécessaires, deviennent un confort
plutôt qu'un prérequis : le finding est *sur* la ligne.

**3. `scriptLine` n'est pas une ancre**, alors que c'est ce qui avait rendu la
moitié 2 crédible. Deux mesures sur un pipeline résolu réel :

| | |
|---|---|
| la même ligne de script | **douze occurrences** dans un document — le `before_script` est inliné dans chaque job qui le référence, donc un match texte ne désigne aucun job |
| où elles tombent | dans des **scalaires de bloc**, où un `#` n'est pas un commentaire mais du texte de script — annoter là réécrirait le pipeline au lieu de le décrire |

Une clé de job, elle, est un mapping de premier niveau, unique, et un
commentaire y est un commentaire. `scriptLine` est parsé et **affiché dans la
vue détail**, ce qui est sa vraie valeur : sans lui, un ISSUE-411 nomme un job
de quarante lignes sans dire laquelle.

#### Deux corrections à ce que cette section a affirmé

**« GitLab résout les jobs cachés et les supprime du fusionné » est faux.** Il
les **quote** : toute clé commençant par un point ressort comme `".job":`, et
sur le document mesuré **6 des 13 clés de premier niveau** le sont. L'erreur
vient d'un grep cherchant un début de ligne non quoté, et `jobKeyOf` la
répétait — il renvoyait le nom guillemets compris, qui ne correspond à aucun job
que plumber nomme. Le symptôme est le pire de sa catégorie : **une
non-correspondance de nom se manifeste comme une absence d'annotation, jamais
comme une erreur**, donc elle passe deux relectures et tous les tests sur
fixture. C'est la vérification de bout en bout sur le vrai document qui l'a
attrapée.

**Un résultat déjà sur disque n'a pas de champ `Job`.** Il est né avec cette
section ; chaque scan antérieur porte le job dans sa `Description` et nulle part
ailleurs. Un lecteur qui ne regarderait que le champ ne trouverait rien sur
**tous** les scans existants, et « rescanne chaque cible » n'est pas un
correctif qu'un utilisateur devine. `jobOf` lit le champ puis retombe sur la
description — sûr parce que la chaîne est la nôtre, écrite par `plumberFinding`
comme `"job: " + issue.Job` : on relit sa propre sortie, on ne devine pas celle
d'un outil.

#### Ce qui n'a pas été construit

- **La remontée au fichier amont** (niveau 2 de la moitié 2) : suivre
  `includes[].blob` jusqu'à la ligne du composant. La mécanique est mesurée et
  reste valable — c'est la seule partie du plan qui tienne telle quelle.
- **Le cache**, décrit ci-dessus. Rien n'est mis en cache : chaque `o` redemande
  le document au forge.
- **Les numéros de ligne du viewer**, renvoyés à une PR à eux : c'est une
  fonctionnalité du viewer entier, elle touche chaque document et le repli, où
  une ligne de continuation ne doit pas être renumérotée.
- **Les findings sans ancre** — projet, includes — n'apparaissent pas dans le
  document, par décision : l'onglet CI les liste, et sur `notes-backend` c'est
  la totalité des six.

#### Ce que ça vaut au-delà de l'ergonomie

Le score d'un dépôt applicatif est surtout celui de ses templates : les six
`ISSUE-403` de `notes-backend` visent des composants situés deux à trois niveaux
au-dessus, qu'aucune de ses équipes ne peut corriger. Rendre la provenance
visible, c'est ce qui permet de router un finding vers le dépôt qui peut
réellement le traiter — et de voir qu'une même faute comptée sur vingt dépôts
consommateurs n'est qu'**une** faute.

### 3.52 La colonne CI se colore, et migre vers `:sec` — **done**

Fait le 2026-08-27. Trois changements, demandés ensemble : les couleurs, le
retrait de la ligne de score, et la colonne dans l'inventaire.

Demandé le 2026-08-27. La correspondance voulue :

| Lettre | Couleur | Alias |
|---|---|---|
| **A**, **B** | vert | `ColorOK` — le vert des icônes de statut (Rule 121) |
| **C** | orange | `ColorSeverityMedium` |
| **D** | rouge clair | `ColorSeverityHigh` |
| **E** | rouge foncé | `ColorSeverityCritical` |

Les cinq en **gras**.

#### Ce qui existe déjà, et ce qui change réellement

`CIScoreStyle` (`internal/ui/theme/ciscore.go`) fait **déjà** trois cinquièmes du
travail : `C`, `D` et `E` passent par `SeverityTextStyle("MEDIUM" | "HIGH" |
"CRITICAL")`, donc les trois teintes demandées sont déjà les bonnes. Il reste
deux changements :

1. **A et B prennent le vert** — aujourd'hui ils rendent en couleur de texte
   ordinaire, délibérément (voir ci-dessous).
2. **Le gras s'applique aux cinq** — `SeverityTextStyle` met `Bold(true)` sur
   CRITICAL et HIGH **seulement**, donc `C` n'est pas en gras et `A`/`B` non
   plus.

**Le gras ne se prend pas dans `SeverityTextStyle`.** Y ajouter `Bold` pour
MEDIUM mettrait en gras **chaque finding MEDIUM de l'application** — la table
des résultats, l'inventaire de `:sec`, tout ce qui affiche une sévérité.
`CIScoreStyle` applique donc `.Bold(true)` sur le style qu'elle retourne, et ne
touche pas la fonction partagée. C'est le même raisonnement que Rule 122 sur
`Cell` et `Style` : la couleur appartient à la colonne, pas au vocabulaire
qu'elle emprunte.

#### Ce que ça renverse, et l'argument qui le justifie

Il faut le dire franchement : le code porte aujourd'hui la décision **inverse**,
et elle est écrite trois fois — dans le commentaire de `CIScoreStyle`, dans
`TestOnlyTheBadGradesAreColoured`, et dans la discipline de couleur de Rule 122
(« l'état nominal et majoritaire → couleur de texte par défaut, **pas** de
vert »). L'argument d'origine était qu'un A vert sur chaque dépôt bien configuré
n'informe personne et affaiblit D et E.

**Cet argument ne tient pas dans cette colonne-ci, et c'est ce qui justifie le
changement.** Une colonne de sévérité n'a qu'un axe ; la colonne CI a **quatre
états**, dont trois sont des absences (`-` jamais scanné, cellule vide non
gradable, `?` score retenu) — et les trois rendent en `DimStyle`. L'absence de
couleur y est donc **déjà prise** par les absences. Un `A` en couleur de texte
ordinaire se distingue d'un `-` en gris par une nuance, sur une cellule de
quatre caractères. Le vert ne dit pas « bravo » : il dit **« ceci est une note »**
par opposition à « il n'y en a pas », ce qui est exactement la distinction que
la colonne existe pour porter et la seule que le rendu actuel fait mal.

Le gras va dans le même sens : il sépare une note d'une absence avant même que
la teinte soit lue.

#### Ce qu'il faut mettre à jour dans le même commit

Sans ça, le dépôt garderait des textes affirmant le contraire de son code :

- le commentaire de `CIScoreStyle`, qui explique pourquoi A n'est pas coloré ;
- **`TestOnlyTheBadGradesAreColoured`** — il passe encore mécaniquement (A et B
  restent identiques entre eux, C/D/E restent distincts d'eux), mais son nom et
  son commentaire affirment l'inverse de l'intention. Le renommer et lui faire
  vérifier la correspondance demandée : cinq lettres, cinq styles, A = B, et
  chacun des cinq en gras ;
- la ligne « pas de vert » de la **discipline de couleur de Rule 122**
  (`.claude/rules/tui-tables.md`), qui doit nommer cette colonne comme le cas où
  la couleur sépare une valeur d'une absence plutôt que deux valeurs entre
  elles.

`TestEveryAbsenceIsDim` n'est pas touché et doit le rester : les trois absences
restent grises, c'est ce qui donne son sens au vert.

#### Ce que ça ne touche pas

`CIScoreCell` — la cellule reste du texte brut, sans séquence d'échappement
(Rule 122). Et le fond : `SeverityTextStyle` pose déjà `Background(ColorBackground)`,
donc le style du vert doit le poser aussi, ou la cellule dépouille de son fond
tout ce qui la suit sur la ligne (Rule 115).

#### Ce qui a été construit, et les deux changements qui se sont ajoutés

**1. Les styles sont ceux de cette colonne, pas ceux d'une sévérité.**
`CIScoreStyle` construit son style au lieu d'emprunter `SeverityTextStyle`, et
`ciGradeStyle` porte la table des cinq lettres. Le piège annoncé était réel :
`SeverityTextStyle` ne met `Bold` que sur CRITICAL et HIGH, donc un `C` emprunté
rendrait plus léger qu'un `D` pour une raison qui appartient à une table de CVE.
Une lettre que l'outil ajouterait plus tard reçoit la graisse et **aucune
couleur** — le vert nominal est une affirmation, et l'attribuer à une note que
personne n'a définie serait une supposition.

**2. La ligne de score disparaît de l'onglet CI.** Elle coûtait deux lignes à
cet onglet — la ligne et son blanc — à chaque ouverture, pour énoncer une lettre
que l'inventaire porte désormais par cible. Un onglet qui montre moins de
résultats que ses quatre voisins, et une mise en page qui saute à chaque
bascule, ne valaient pas une valeur déjà présente sur l'écran précédent.
`resultsHeadLines`, `ciScoreHeadLines` et `renderCIScoreLine` sont supprimés ;
`TestEveryTabGivesTheTableTheSameHeight` fixe ce qu'on y gagne.

**3. `:sec` gagne la colonne**, sous les quatre règles de `ws` : présente
seulement si le réglage l'est, jamais `Optional`, jamais triable — `datatable`
réserve `largeur(titre)+2` à la flèche d'une colonne triable, ce qui coûterait
six cellules au lieu de quatre dans la table la plus serrée de l'application —
et les trois absences distinguées par l'état.

**Ce que la migration a demandé de décider.** Une ligne de l'inventaire vient du
cache, qui ne stocke que la lettre ; or la distinction entre « jamais scanné »
(`-`) et « pas gradable d'ici » (cellule vide) tient au **remote**, que le cache
ne porte pas. Trois options, et une seule est honnête :

| | |
|---|---|
| lire le remote au chargement | un `git remote get-url` par ligne de dépôt, dans un `Cmd` — **retenu** |
| rendre `?` faute de mieux | ferait dire « score retenu » de chaque dépôt d'un autre forge : D20 |
| ajouter un champ au cache | une entrée existante décoderait en `false`, c'est-à-dire un mensonge |

Le coût est borné par `ciForgeURL`, qui rend la chaîne vide quand la colonne est
éteinte : **une colonne que personne n'affiche ne paie pas d'appel git**. Et un
remote illisible n'est pas gradable — ce n'est pas un repli mais la bonne
réponse : plumber n'aurait pas résolu de projet non plus.

`InventoryScanFinishedMsg` porte la lettre pour la raison exacte qui lui fait
porter le verdict de secrets : sans elle, une ligne rescannée garderait la note
de son scan précédent à côté de compteurs tout neufs, jusqu'au prochain
`ctrl+r`.

#### Ce qui a été mis à jour avec le code

Comme l'entrée l'exigeait, pour que le dépôt ne garde pas des textes affirmant
le contraire de son code : le commentaire de `CIScoreStyle`,
`TestOnlyTheBadGradesAreColoured` — remplacé par
`TestTheFiveGradesAreColouredAndBold`, `TestABadGradeIsNeverTheNominalColour` et
`TestAnUnknownGradeClaimsNoColour` —, la discipline de couleur de Rule 122, et
la section CI de `.claude/CLAUDE.md`.

La ligne de Rule 122 n'est pas supprimée mais **assortie de son exception, avec
son critère** : une colonne où l'absence de couleur est *déjà prise* par des
absences grises. Ce n'est pas « c'est important » — si les absences d'une
colonne se distinguaient déjà, le vert y redeviendrait du bruit.

### 3.53 Le viewer numérote ses lignes, va à l'une d'elles, et sait compter la casse — **done**

Fait le 2026-08-27. Trois demandes d'un coup, toutes dans le demi-écran texte :
`n` affiche les numéros de ligne, `g` va à l'une d'elles, `s` décide si la
recherche `/` tient compte de la casse. Aucune ne touche l'arbre, où les trois
sont grisées (Rule 130).

#### Les numéros sont ceux du document, et c'est tout l'intérêt

`docLine` porte un `Num`, posé une seule fois dans `buildLines`. Tout ce qui
suit travaille sur une tranche **filtrée** — une recherche, une verbosité — donc
un index dans ce qui est à l'écran serait un autre nombre, et précisément celui
qu'il ne faut pas montrer : une gouttière qui se renumérote sous un filtre est
pire que pas de gouttière du tout, parce qu'elle a l'air de répondre.

Quatre propriétés, chacune avec son test :

- **La gouttière n'est pas cherchable**, par construction : elle n'entre jamais
  dans `docLine.Plain`, donc `MatchRanges` ne la voit pas. C'est ce que
  l'implémentation naïve — préfixer, puis filtrer — casserait en passant tous les
  autres tests de la fonctionnalité. D'où le test qui cherche `2` et n'attend
  aucune ligne.
- **Elle sort de la largeur *avant* l'enroulement.** `wrapTokens` compte des
  runes et ne sait rien de ce qu'on met devant un segment : enrouler à la
  largeur pleine puis préfixer pousse **chaque** rangée au-delà de la marge, de
  la largeur exacte de la gouttière. Sur toutes les lignes à la fois, donc ça se
  lit comme un défaut de bordure et pas comme un défaut de gouttière.
- **Une ligne enroulée numérote sa première rangée seulement.** Un numéro dit où
  une ligne source *commence* ; le répéter affirmerait que le document contient
  plusieurs lignes portant le même.
- **En `DimStyle`**, comme toute valeur présente sur chaque rangée : une couleur
  qui apparaît partout n'informe de rien, et celle-ci concurrencerait les niveaux
  de log et les surbrillances de recherche, qui sont ce que l'œil cherche.

#### `g` refuse deux fois plutôt que de faire quelque chose d'adjacent

Le prompt est un **mode** : il prend toute touche avant que le panneau n'en voie
une, comme une modale de confirmation. Sans ça un chiffre défilerait *en plus*
d'être saisi, et `esc` quitterait la vue en laissant le champ focusé.

`rowOfLine`, reconstruit avec le panneau, est la seule chose qui sait où une
ligne du document a atterri : une rangée n'est pas une ligne dès qu'un filtre en
a supprimé et qu'un enroulement en a coupé. Elle retient la **première** rangée
d'une ligne enroulée, donc un saut ne tombe jamais au milieu de l'une.

| Le numéro | Ce qui se passe |
|---|---|
| vide | rien, en silence — c'est un changement d'avis, pas une erreur |
| au-delà de la fin | `Document has N lines` |
| masqué par le filtre | `Line N is hidden by the filter`, et **rien ne bouge** |
| à l'écran | cette ligne passe en haut du panneau, comme le fait `less` |

Le cas masqué est celui qui valait d'être écrit. Sauter à la plus proche visible
aurait annoncé une réussite tout en mettant un **autre** numéro sous le curseur :
c'est la forme de D20 — une absence rendue comme autre chose. L'utilisateur lève
le filtre, ou pas.

Le champ refuse tout ce qui n'est pas un chiffre (`textinput.Validate`), donc la
soumission n'a que deux échecs à expliquer au lieu de trois — et le troisième
aurait été le seul que l'utilisateur pouvait être empêché de créer.

#### `s` est un paramètre du calcul, pas une seconde lecture

`MatchRanges(text, query, caseSensitive)`. La casse décide **quelles lignes
survivent** *et* **quels fragments sont surlignés** : un filtre qui aurait
consulté le drapeau de son côté serait exactement le second calcul que cette
fonction existe pour empêcher — ce que `scan.Categorize` et
`Result.SecretVerdict` ont chacun dû défaire, avec le même symptôme les deux
fois.

La garde `İ` — `strings.ToLower` peut changer la longueur en octets, donc un
décalage dans la copie pliée nomme un autre octet de l'original — appartient
désormais à la branche qui plie, et à elle seule : la branche sensible ne plie
pas, elle n'a rien à rendre exact.

**La bascule re-filtre la requête déjà posée** plutôt que de la vider : comparer
les deux lectures est ce pour quoi on appuie sur la touche, et retaper en ferait
une corvée. Le jeton `Aa` n'apparaît que quand elle est allumée (Rule 136) :
l'état de repos — insensible — reste sans barre.

`syncVerbosityToken` devient `syncFilterTokens` et construit la liste entière :
la barre disparaît quand rien n'est actif, donc ce qui doit être juste est
*quels jetons existent*, une question à une réponse.

#### Le créneau du footer, et `components.BarFrame`

Le prompt occupe le créneau de la barre de filtre, exclusivement. Les deux sont
une ligne dans le même rectangle, extrait de `FilterBar.View()` en
`components.BarFrame(width, inner)` — deux implémentations du cadre seraient
libres de diverger sur l'endroit où sont les coins. `GetFooterHeight` répond 2
dans les deux cas : c'est ce qui empêche le panneau de se redimensionner sous le
lecteur quand le prompt s'ouvre par-dessus une recherche active.

#### `g` était un alias vim retiré, et elle revient avec un autre sens

§3.26 a supprimé `g`/`G` comme alias de `home`/`end`. Ce que la règle interdit
est la lettre **à la place** d'une touche structurelle ; ici elle ouvre un prompt
et le saut prend un **argument**, ce que `home` et `end` ne savent pas exprimer.
Elle sort donc de `retiredAliases` dans `keymap_test.go`, avec sa raison écrite
là : une lettre qui reprend un sens quitte la liste de celles qui n'en ont plus,
sinon la liste ment. C'est `H` revenue dans `free` en §3.47, pris dans l'autre
sens. `j` et `k` y restent — elles ne sont que `down` et `up` sous un autre nom.

Le choix de `g` est celui de l'utilisateur, contre la proposition d'une
majuscule libre (`J`). Le coût était connu d'avance et il est celui-là :
l'amendement du test, et les trois textes qui affirmaient que `g` avait disparu
de l'application.

#### Ce qui a été trouvé en passant

`TestScrollKeys` appuyait sur `g` puis `G` en attendant qu'il ne se passe rien
— les deux n'ont jamais été liées, donc le test vérifiait que le panneau était
déjà en haut. Il presse `home` et `end`, qui sont les touches que la vue lie
réellement.

Et, hors sujet mais bloquant : `internal/command/doc_test.go` lisait la liste des
commandes dans `.claude/CLAUDE.md`, d'où l'architecture est partie avec (#141).
Les deux tests échouaient sur `main` en disant de les repointer, ce qui est
exactement ce qu'ils devaient faire — le fichier lu est une donnée du test, pas
une propriété du parser. Ils lisent `docs/architecture/app-shell.md`.

#### Ce qui a été mis à jour avec le code

`docs/architecture/viewer.md` (la recherche, la gouttière, le saut, le créneau),
`docs/architecture/app-shell.md` (le retour de `g`), Rule 111 (les minuscules du
viewer, les trois touches, et la clause `g`/`G`), Rule 136 (`BarFrame`, et un
seul occupant du créneau), et `GetHelpContent` — les trois touches et une
section « Line numbers, and going to one ».

### 3.54 L'onglet Certificates range ses colonnes par nature — **done**

Fait le 2026-08-29. Signalé ainsi : « status monitor certificates, les colonnes
ne prennent pas en largeur tout l'espace disponible alors que services monitor
oui ». L'ordre demandé est **Name, Host, Issuer, Status, Days Left, Expires**.

#### Ce que la plainte décrivait, et ce qu'elle ne décrivait pas

Mesuré avant de toucher quoi que ce soit : `RenderedWidth()` vaut l'intérieur du
viewport à **toutes** les largeurs, de 40 à 250 colonnes, pour les deux tables.
Rule 116 tenait ; rien ne s'arrêtait avant la bordure. Ce qui se voyait n'était
donc pas de l'espace perdu à droite mais de l'espace mal **réparti** : à 120
colonnes, Services donnait 76 des 108 utiles à ses deux colonnes d'identité,
Certificates seulement 45 sur 106 — parce que `Status` en réserve 12 pour un
glyphe, `Days Left` 11 pour un en-tête que ses valeurs (« 42 », « -3 », « - »)
n'approchent jamais, et que `Issuer`, la seule colonne extensible de la moitié
droite, tirait la ligne du mauvais côté.

Nommer la cause a compté : « les colonnes ne prennent pas toute la largeur » et
« la largeur va aux mauvaises colonnes » se corrigent différemment, et la
première aurait mené à chercher un défaut de calcul qui n'existe pas.

#### L'ordre groupe par nature

Les trois colonnes qui suivent leur contenu — `Name`, `Host`, `Issuer` — sont
maintenant à gauche et se partagent le surplus entre elles ; les trois de
largeur fixe sont packées à droite, où un glyphe et deux valeurs courtes
occupent exactement ce qu'elles déclarent. Aucune largeur, aucun `Flex`, aucun
`MinWidth` n'a bougé : c'est un réordonnancement, et rien d'autre.

Le gain est celui-là, mesuré à 120 colonnes : `Issuer` cesse d'être coincée
derrière deux champs fixes, et la lecture va de l'identité vers l'échéance au
lieu d'alterner.

#### La dégradation change d'ordre, et c'est voulu

`drop` retire la colonne `Optional` la plus à droite d'abord. Elle était
`Issuer` ; elle est maintenant `Expires`. C'est le bon sens de la perte :
`Expires` et `Days Left` disent la même échéance, l'une en absolu et l'autre en
relatif, et `Days Left` reste — donc c'est la redondante qui part la première,
et l'émetteur du certificat survit à l'étroitesse.

Relevé après coup, colonnes rendues :

| Largeur | Colonnes présentes |
|---|---|
| 60 | Name, Host, Status |
| 80 | Name, Host, Status, Days Left |
| 100 | + Issuer |
| 120 et au-delà | les six |

#### Les tests ne comptent plus les cellules

Trois tests de `internal/ui/status/view_test.go` lisaient leurs cellules par
indice, ce qui est une seconde déclaration de l'ordre — et celle qui pourrit en
silence : un réordonnancement les aurait laissés verts en comparant les
mauvaises colonnes. Ils passent par `sslCell(t, row, "Issuer")`, qui résout le
titre dans `sslColumns()` et échoue en le nommant si la colonne disparaît.

### 3.55 Une icône en première colonne est une colonne — **done**

Fait le 2026-08-29. Demandé ainsi : « dans la vue ws l'icone est en première
colonne sur deux caractères […] il n'y a pas de titre sur la colonne d'icone.
Applique ce pattern à la vue sec et c'est de cette manière qu'il faut afficher
les datatables avec icones en première colonne ».

#### Ce que l'inventaire faisait

`displayName()` rendait `theme.IconDocker + " " + shortName()` **à l'intérieur**
de la cellule Target. Le glyphe était donc dans la colonne identifiante, et la
colonne identifiante est en `SizingContent` : elle mesurait le glyphe avec le
nom, et réservait deux cellules pour lui à toutes les largeurs — dans la table
la plus étroite de l'application, celle dont le commentaire de `displayName`
disait déjà que la place manquait.

`displayName` et `shortName` devenaient alors identiques une fois le glyphe
retiré — les deux branches ne différaient que par l'icône — donc la première a
disparu plutôt que d'être vidée.

#### Le motif, et où il était déjà

`ws` et `containers` le faisaient déjà : une colonne sans titre, `SizingFixed`,
sans `Less` ni `Search`. Trois tables, trois constantes locales, et deux valeurs
différentes — `colIconFixed = 2` côté workspaces, `statusColumnWidth = 3` côté
containers. L'écart ne se voit pas sur un écran : il se voit quand on passe de
`ws` à `ct`, où le même glyphe est une cellule plus loin du nom.

`datatable.IconColumnWidth` est maintenant la seule déclaration, et les trois
tables la lisent. `containers` a donc perdu une cellule, ce qui n'était le choix
de personne — le 3 n'était justifié nulle part.

| | |
|---|---|
| Titre | vide |
| Largeur | `datatable.IconColumnWidth` (2) : le glyphe, plus une cellule pour ne pas toucher le texte |
| `Sizing` | `SizingFixed` |
| `Less` / `Search` | aucun — pas de texte à taper, et un comparateur coûterait deux cellules de flèche |

#### Ce que le test attrape, et ce qu'il laisse à la revue

`TestAnIconColumnIsUntitledAndTwoCellsWide` (`internal/ui/datatable`) parcourt
les sources et refuse une colonne sans titre qui déclare une largeur à elle.
C'est la moitié invisible du problème : trois tables chacune correcte seule et
différentes côte à côte.

L'autre moitié — une icône collée dans une cellule de texte — n'est pas
détectable dans la source : rien n'y distingue `IconDocker + " " + name` d'un
nom qui commencerait par un glyphe. Elle reste une question de revue, écrite
dans Rule 125.

#### Conséquences

`inventoryColumnCritical` passe de 2 à 3, la colonne insérée étant en tête.
`Target` perd deux cellules sur ses deux bornes (`MinWidth` 24 → 22, `MaxWidth`
60 → 58) : ce qu'elle mesure est désormais le nom seul, donc garder les
anciennes bornes lui aurait rendu la largeur du glyphe en plus de la sienne.

### 3.56 L'explorer passe aux icônes, et le thème apprend à les colorer — **done**

Fait le 2026-08-29. Demandé ainsi : « la colonne type doit être remplacée par une
icone […] les icones de la vue exp doivent être différente de la vue ws. La
colonne visibilité doit contenir une icone […] Réordonne les colonnes […]
Enrichit le theme pour permettre de préciser des couleurs différentes pour les
différentes icones ». Puis, après le plan : « enlève le filtre sur visibility et
garde le titre actuel ».

#### Ce que la table faisait

Huit colonnes ouvertes par `Type`, treize cellules pour écrire « Group » ou
« Organization » sur chaque ligne — et un mot que le compte personnel de GitHub
rendait faux, ce que `forge.md` documentait comme une aspérité assumée.
`Visibility` en dépensait douze pour un mot de sept, et `CI` fermait la marche,
loin de `Role` qui est la seule autre chose que la forge dise du dépôt.

#### Ce qu'elle fait

`icône · Name · Slug · Visibility · Role · CI · Created · Activity`.

La première colonne suit Rule 125 : sans titre, deux cellules, ni comparateur ni
clé de recherche. Les onze cellules rendues vont à `Name` et `Slug`, les deux
colonnes qui identifient une ligne.

`Visibility` garde son mot entier au-dessus d'un glyphe, et **perd son tri**.
C'est le tri qui payait la différence : `askFor` réserve deux cellules de flèche
sur toute colonne triable, triée ou non, donc la colonne descend de douze à dix
— la largeur exacte de son propre en-tête — sans que le lecteur ait à décoder un
titre abrégé. Public avant private, ou l'inverse ? Trois valeurs dans un ordre
que personne ne reconnaîtrait ne font pas un tri.

Une visibilité inconnue rend une cellule **vide**. Le champ est celui du
backend ; une forge qui gagnerait une quatrième valeur afficherait sinon l'une
des trois existantes — une réponse fausse là où rien est une réponse vraie.

#### La case à cocher

`Type` portait la case du mode clone. Rule 125 fige la colonne d'icône à deux
cellules, donc la case **remplace** le glyphe au lieu de se poser à côté.

Ce qui rend le partage honnête est la couleur : `iconStyle` peint le kind dans
les deux modes, donc une ligne cochée dit encore groupe ou dépôt — par la teinte
plutôt que par la forme. C'est la seule raison pour laquelle les rôles de
couleur d'icône existent, et pas de la décoration.

#### Le tri d'ouverture

`SortColumn` pointait sur `Type`, qui n'existe plus et ne pourrait pas trier de
toute façon. Il passe à `-1`, l'ordre de la forge — et **ce n'est pas un
changement de comportement** : `loadChildren` empile les namespaces puis les
dépôts, et le tri par type ascendant qu'il remplace est l'identité sur cette
liste. Ce qui est gagné est que `.` a désormais « pas de tri » comme arrêt, donc
l'ordre de la forge redevient atteignable après qu'on s'en est éloigné. Un tri
par type ne pouvait pas l'exprimer.

#### Le thème

Cinq rôles (`theme.IconRole`), cinq couleurs sémantiques, cinq clés de thème
surchargeables, `IconColor` et `IconStyle` dans `theme/iconcolors.go`.

**La clé est un rôle et non un glyphe**, et c'est le seul point de conception à
retenir : une table `U+F0849 → ColorSecondary` ne se relit pas — rien sur la
ligne ne dit si l'entrée est juste, donc une erreur y est indiscernable d'un
choix. Un rôle se discute. C'est la forme qu'ont déjà `SeverityTextStyle`,
`CIScoreStyle` et `ForgeIcon`.

Elles sont surchargeables par un fichier de thème, contrairement aux couleurs de
syntaxe qui sont des alias fermés : une icône est la première chose vue sur une
ligne, donc c'est la partie de la palette sur laquelle un utilisateur a le plus
de chances d'avoir un avis.

Les défauts portent chacun un argument. Public est vert parce que c'est l'état
qui mérite d'être repéré sans lire ; internal prend la teinte d'avertissement,
restreint sans être fermé ; **private est gris** parce que c'est la majorité — et
une couleur que toutes les lignes portent n'informe de rien (Rule 122), qui est
l'argument des compteurs de sévérité pour leurs zéros.

#### Les glyphes

`nf-md-account_group`, `nf-oct-repo`, `nf-md-earth`, `nf-md-shield`, plus
`IconLock` qui existait. Aucun n'est de l'ensemble `ws` — `ws` liste ce qui est
sur le disque, `exp` ce que la forge détient, et une ligne identique dans les
deux prétendrait qu'il s'agit du même objet. `TestTheExplorerGlyphsAreNotTheWorkspaceOnes`
le vérifie.

#### Ce que la vocabulaire devient

Les mots de la forge quittent la table avec `Type`. Ils ne sont pas perdus : ils
passent dans la légende **Row Icons** de l'aide, seul appelant restant de
`nodeTypeLabel`. L'aspérité de `forge.md` — le compte personnel qui se dit
« Organization » — n'est donc plus répétée sur chaque ligne ; elle tient en une
phrase, à un endroit où une phrase a la place d'être juste.

### 3.57 `ws` et `:sec` colorent leur colonne d'icône — **done**

Fait le 2026-08-29. Demandé ainsi : « mets aussi en place la coloration des
icones (première colonne) dans ws et sec ». Suite directe de
[§3.56](#356-lexplorer-passe-aux-icônes-et-le-thème-apprend-à-les-colorer--done),
qui a construit le mécanisme et ne l'a câblé que dans l'explorer.

#### Trois rôles de plus

`IconRoleDirectory`, `IconRoleFile`, `IconRoleImage` — et `IconRoleRepository`
est **réutilisé**, pas dupliqué : un dépôt git est le même objet dans `ws`, dans
`:sec` et dans l'explorer, où il est simplement distant. Les trois vues n'en
affichent d'ailleurs pas le même glyphe, ce qui est précisément l'argument pour
que la clé soit un sens et non un codepoint.

| Rôle | Couleur | L'argument |
|---|---|---|
| `directory` | `ColorSecondary` | la couleur du namespace : les deux sont *ce qui contient* des dépôts, sur une forge et sur un disque. Deux rôles et non un alias, pour qu'un thème puisse les séparer |
| `file` | `ColorDim` | la ligne à laquelle rien ne s'applique — `W`, `S` et `F` y sont grisés (Rule 130) — donc le gris redit ce que la colonne de raccourcis dit déjà |
| `image` | `ColorHighlight` | pas un troisième violet : `Primary` et `Secondary` sont un mauve et un lavande à un cran l'un de l'autre, et la première colonne de `:sec` a exactement deux valeurs — le seul endroit où elles se toucheraient sans rien d'autre pour les séparer |

#### La granularité est celle de l'action

`ws` se colore en **trois** classes, pas une par langage. C'est la même
tripartition que fait déjà `availability.go` : un dépôt se scanne, se
synchronise et s'ouvre sur sa forge ; un répertoire s'entre ; un fichier se lit.
`.go` contre `.rs` ne change aucun raccourci de la ligne, donc une teinte par
type de fichier serait de la décoration.

#### Ce que ça renverse

`docs/architecture/workspaces.md` documentait le contraire : « **No colour**,
against `eza`. A colour on every row informs no one, and it would weaken the
ones that signal something ». La note est réécrite plutôt que supprimée — elle
avait raison sur `eza`, dont la couleur suit le *type de fichier*, et c'est
exactement ce qui n'est pas fait ici.

Le coût qu'elle annonçait reste réel : chaque ligne porte désormais une teinte.
Il est amorti en gardant la classe fichier **en gris** — la ligne à laquelle
rien ne s'applique est celle qui recule — pour que ce que l'œil attrape en
premier reste un compteur de sévérité ou un git status sale, et non la cellule
la plus à gauche.

#### Ce qui n'a pas été fait

Une couleur par langage dans `fileicon` (le comportement d'`eza` et de `lsd`).
Ce serait environ vingt-cinq rôles pour distinguer des lignes qui offrent les
mêmes actions ; à demander explicitement si le rendu final le réclame.

### 3.58 Un registre de travaux, et la vue `jobs` — **done**

Plan détaillé : [`jobs-registry.plan.md`](../.claude/plans/jobs-registry.plan.md).

Quatre comptabilités parallèles suivent aujourd'hui le travail en cours —
`workspaces.scanningPaths` / `syncingPaths` / `deletingPaths`,
`oci_resources.scanningImages`, le marqueur `Scanning` de l'inventaire `:sec`,
et les cinq états de l'écran de clone — et **aucune ne voit les autres**. Le
`busy()` de `ws` ne sait donc pas qu'un scan a été lancé sur le même dépôt
depuis `:sec`, et les deux écrivent la même entrée de cache. Chacune tient en
plus sa propre frame de spinner.

Le registre (`internal/jobs`) est un modèle à deux niveaux — des *runs* qui
portent des *items* — détenu par le routeur et diffusé par message plutôt que
partagé par pointeur. Il fait tenir **une** chaîne `spinner.Tick`, donne à
chaque vue une réponse commune à « qu'est-ce qui tourne ? », et rend possible
ce que D67 ne fait pas :

- le compteur par dossier de `ws` descend au footer, la colonne ne portant plus
  qu'un spinner — le `N/M` ne dit alors plus que la couverture **stabilisée** ;
- une vue `:jobs` liste les travaux en cours, tous types confondus ;
- l'annulation devient possible là où le type de travail la permet ;
- l'estampe de contexte au lancement corrige un batch qui traverse un
  changement de contexte et écrit dans le cache du nouveau.

L'écran de clone de l'explorer est le prototype à généraliser : c'est déjà une
vue de jobs, pour un run et un seul type.

Huit postes, dont [D67](#11-fixed) était le premier — livré seul parce qu'il ne
dépend d'aucun des autres. **Les huit sont livrés** (2026-08-29), et deux
défauts ont été trouvés en chemin plutôt que signalés à l'usage :
[D67](#11-fixed) et [D68](#11-fixed).

#### État

| Poste | |
|---|---|
| 1 — le retour forcé | **fait**, [D67](#11-fixed) |
| 2 — `internal/jobs`, la diffusion, la chaîne de tick | **fait** |
| 3 — `ws` branché (le compteur au footer, le spinner en colonne) | **fait** |
| 4 — `:sec` et `oci` branchés | **fait** |
| 5 — l'estampe de contexte | **fait**, [D68](#11-fixed) |
| 6 — la vue `:jobs` | **fait** |
| 7 — le clone rebranché | **fait** |
| 8 — l'annulation | **fait** |

Le poste 2 est délibérément **inerte** : le registre existe, le routeur le
détient et diffuse, et rien ne l'alimente. C'est ce qui le rend vérifiable seul —
les vues continuent exactement comme avant, et le seul appelant en production est
le changement de contexte, qui doit dire aux vues reconstruites ce qui tourne
encore. Le modèle et la mécanique du routeur sont décrits dans
[`app-shell.md`](architecture/app-shell.md).

Ce que le poste 3 a livré, et qui est la demande d'origine : `ws` lit le
registre au lieu de tenir trois maps de chemins. `busy()` voit donc enfin ce
qu'une autre vue a lancé sur le même dépôt — c'était le défaut de fond, pas une
conséquence. La ligne **répertoire** de la colonne Scanned tourne au lieu de
compter, et le compteur passe au footer sous les trois formes de D9. La
`spinner.Model` de `ws`, son `spinnerFrameIdx`, son handler `spinner.TickMsg` et
le `syncRun` disparaissent.

Trois points tranchés en écrivant, au-delà de ce que le plan fixait :

- **`jobs.StartMsg` porte le travail** (`Work tea.Cmd`) au lieu d'être batché à
  côté de lui. L'ordre compte — une transition qui nomme un run non encore admis
  est refusée, et un `tea.Batch` n'en donne aucun — et un `tea.Sequence` à chaque
  site de lancement rendait le lancement **inlisible en test** : lire le `Cmd`
  l'exécute, donc une assertion « quel run a été enregistré ? » lançait les scans
  pour de vrai. Le routeur est le séquenceur ; c'est une chose à faire juste au
  lieu de six.
- **`jobs.Reporter`** : chaque message de progression répond sa propre
  transition. La correspondance « un sync refusé pour arbre sale est un *skip*,
  et la raison vaut d'être gardée » est du vocabulaire de `workspaces` ; un
  routeur qui la connaîtrait grossirait d'une branche par vue.
- **Le message de départ attend son tour dans le pool.** `scanOneRepoCmd`
  l'émettait *avant* le sémaphore, donc les douze dépôts d'un lot se déclaraient
  running à l'instant du dispatch : douze spinners pour quatre workers. Sans ce
  correctif D6 aurait été décoratif.

Le poste 4 branche les deux dernières comptabilités de scan. `:sec` y gagne le
plus : `scanTarget.Scanning` était un drapeau que la vue posait et effaçait
elle-même, et `handleInventoryLoaded` devait le reporter à la main à chaque
rechargement — le cache ne dit rien d'un scan qui n'a pas fini d'y écrire, donc
un rafraîchissement en cours de rescan effaçait le spinner et laissait la ligne
pour réglée. **La réconciliation disparaît au lieu d'être corrigée** : il n'y a
plus rien à reporter quand l'état n'a jamais été là.

Et le marqueur devient vrai pour un scan lancé **ailleurs**, ce qui est la
seconde moitié de la demande 2 : l'inventaire liste exactement ce que `ws` et
l'onglet Images scannent, donc un rescan lancé depuis l'un des deux est le même
travail sur la même entrée de cache — et cette vue n'avait aucun moyen de le
savoir.

Le poste 5 ferme [D68](#11-fixed), qui n'existait pas comme demande : il a été
trouvé en écrivant le registre, et c'est le poste 1 qui l'a rendu atteignable —
tant que le scan ramenait l'écran à `ws`, changer de contexte pendant un lot
n'était pas quelque chose qu'on faisait. `jobs.StartMsg.Work` devient un
constructeur `func(contextName string) tea.Cmd` que le routeur appelle avec le
nom qu'il vient d'estamper, ce qui fait du `Run.Context` de D8 autre chose qu'un
champ d'affichage : c'est maintenant **le** nom, celui que le travail utilise.

Le poste 6 livre la vue. Elle ne possède rien : elle ne lance aucun travail, ne
tient aucune minuterie et ne va rien chercher — ses lignes *sont* l'instantané.
C'est ce qui la rend possible du tout, et c'est aussi ce qui décide de son
vocabulaire : il n'y a pas de `ctrl+r`, parce qu'il n'y a aucune opération
derrière qui pourrait être indisponible.

Deux décisions valent d'être écrites, parce qu'aucune n'était dans le plan :

- **La première colonne est l'état, pas le kind.** Le plan disait l'icône du
  kind ; une liste de jobs se parcourt pour trouver celui qui a échoué et celui
  qui tourne encore, donc le glyphe répond à la question avec laquelle on
  arrive, et c'est aussi la colonne où va le spinner — la forme de la table des
  conteneurs. Le kind garde sa colonne de texte, qui porte l'état dans son
  `Search` : `/failed` et `/scan` marchent tous deux, et la colonne de glyphes
  reste hors du filtre (Rule 125).
- **Pas de rôle d'icône par kind.** Rule 125 demande une couleur venue d'un
  rôle, mais les rôles existent pour les icônes qui nomment un *objet* — un
  namespace, un dépôt, une image — là où la palette doit pouvoir les séparer.
  Un état a déjà une couleur, et cinq rôles de plus donneraient à un thème cinq
  façons de rendre `failed` non rouge. La table des conteneurs colore déjà son
  glyphe d'état par les styles sémantiques ; c'est le précédent, et il tient.

Le compteur du dashboard, reporté du poste 4, arrive avec elle — c'est la vue
`:jobs` qui lui donne son sens. Il est la **seule** ligne que le dashboard peut
dire honnêtement : il ne lance rien, donc il ne passe aucune origine et obtient
toujours la forme dégradée de D9. La phrase est partagée
(`components.JobsStatusLine`) avec le footer de `ws` : deux copies d'un renoncement
délibéré seraient chacune libres de renoncer différemment.

Le poste 7 rebranche le clone (D3), et c'est celui qui a demandé le plus au
paquet. `cloneList` tenait cinq états, une tranche de lignes, un index par
chemin et sa propre frame de spinner : la cinquième comptabilité, celle que le
plan désignait comme le prototype à généraliser. Elle *rend* le run maintenant,
et n'en possède rien — les lignes sont `Run.Items`, la frame est celle du
routeur, « terminé » est `Run.Finished()`.

**Le clone est le seul run *ouvert*.** Tous les autres sites de lancement
connaissent leur liste complète au moment de dispatcher, ce qui est ce qui rend
« 8 en attente » énonçable (D10) ; la marche du clone *est* ce qui les trouve.
`NewOpenRun` enregistre donc un run sans cible, `Discover` en ajoute une à
chaque trouvaille, et **un run ouvert n'est jamais terminé** quoi que disent ses
items : une marche qui a trouvé trois dépôts et cloné les trois n'a pas fini, et
la régler là arrêterait le spinner et imprimerait un résumé que le dépôt suivant
dément. `CloneRunFinishedMsg` la scelle, par `jobs.Sealer` — une seconde
interface parce qu'un canal fermé ne nomme aucune cible.

Deux choses qui n'étaient pas prévues et qu'il fallait faire ici :

- **`esc` passe par le registre** (`jobs.CancelOpenMsg`). Appeler le `cancel` du
  pipeline directement arrêterait la marche dans le dos du registre et
  laisserait `:jobs` affirmer que le run s'est terminé tout seul — exactement la
  distinction pour laquelle `Run.cancelled` existe. C'est un morceau du poste 8
  qui arrive tôt, et le laisser dehors aurait fait mentir la vue livrée au
  poste 6.
- **Annuler scelle.** Ce qui aurait fermé le run est précisément ce qu'on vient
  d'arrêter ; le laisser ouvert le garderait non réglé pour toute la session,
  chaîne de spinner comprise, sans plus rien qui puisse le clore.

Le message nomme un **kind** et non un identifiant : un run progressif
appartient à un écran qui possède l'affichage tant qu'il tourne, donc il y en a
un à la fois et la vue peut le désigner sans tenir de `JobID` — l'état de
registre que D1 garde hors des vues.

Les tests de l'explorer conduisent maintenant un vrai registre à côté de la vue
et lui rendent son instantané, ce qui est exactement ce que fait le routeur. Ce
n'est pas un contournement : l'écran ne détient plus l'état, donc un test qui
n'assertait que sur la vue n'assertait plus sur rien.

Le poste 8 ferme la série avec `K`. Le plan annonçait « il ne reste que la
touche, le garde et les tests », en s'appuyant sur le fait que le
`context.CancelFunc` était en place depuis le poste 2. **Il ne l'était pas** :
`Registry.Attach` existait et n'avait aucun appelant, et les trois sites de scan
passaient `context.Background()`. `K` sur une cible en vol n'aurait donc rien
coupé — une touche qui ment, ce qui est pire que pas de touche.

Les trois sites créent maintenant un contexte annulable **dans le Cmd** et
posent la fonction sur le message qui dit que le scan a démarré. Elle voyage sur
ce message et pas sur un appel à part parce que les deux sont un seul événement :
le registre la range dans le même `Update` qui marque l'item running
(`Transition.Cancel`), donc il n'existe pas de fenêtre où la ligne tourne et où
la touche ne fait rien. `Attach` disparaît, fondu dans `Advance`.

`Run.Stoppable()` est la question posée sur **ce run** et non sur son kind, et
le delete est le cas pour lequel elle existe : un item, en vol, d'un kind qu'on
ne doit jamais couper. `!Finished()` offrirait la touche puis la refuserait —
exactement le refus silencieux que Rule 130 supprime.

Un défaut trouvé au passage et corrigé ici : `scanOneImageCmd` émettait son
message de départ **avant** le sémaphore, donc chaque image d'un lot se déclarait
running à l'instant du dispatch — douze lignes qui tournent sur quatre workers.
C'est le défaut que le poste 3 avait corrigé dans `ws` et le poste 4 dans
`:sec` ; l'onglet Images l'avait gardé, et D6 est décoratif sans cet ordre.

Deux écarts au plan, tous deux du même genre :

- **`spinnerFrameIdx` reste** dans `:sec` et dans `oci`. Le plan disait de le
  retirer ; il sert aussi à animer un **chargement** — la lecture des caches,
  la liste des images, une action `docker` sur une ligne — et un chargement
  n'est pas un job. Ce qui est passé au registre est la frame des cellules de
  *scan*, ce qui était l'objet de D5. `spinnerAlive` de `:sec` ne répond donc
  plus que du chargement : y garder le rescan aurait été une seconde chaîne à
  côté de celle du routeur.
- **Le compteur au dashboard est reporté au poste 6.** Le plan le listait ici,
  mais il n'a pas d'emplacement décidé, et la vue `:jobs` est ce qui lui donne
  son sens (« 2 jobs running » ne mène nulle part sans elle).

Deux points tranchés en écrivant le paquet (poste 2), au-delà de ce que le plan
fixait :

- **`RunCancelled` existe.** Le plan ne dérivait l'état d'un run que de ses
  items, et un run annulé s'y lisait `done` une fois ses items retombés — une
  vue de jobs incapable de distinguer « terminé » de « tu l'as arrêté » perd
  précisément le fait qu'on vient y chercher. C'est un booléen, et la
  précédence est écrite : annulé prime sur échoué, parce que les items d'un scan
  annulé échouent *à cause* de l'annulation.
- **`Kind.Cancellable()` est dans le paquet**, pas dans la vue. C'est la table
  de D7, et un kind ajouté sans réponse répondrait « non » par défaut — la
  réponse prudente, donc celle que rien ne signalerait. Le test parcourt
  `Kinds()` et refuse un kind absent de la table.

### 3.59 Créer et supprimer sont du travail, pas une attente — **done**

`N` et `D` dans l'explorer sont des appels réseau, et l'arbre ne répondait à
aucun des deux pendant qu'il durait. La création appelait `handleRefresh()` en
sortant : la table se vidait et se relistait entièrement, donc pendant tout
l'aller-retour l'utilisateur voyait un corps blanc et un footer muet — puis la
chose demandée, sans rien qui relie les deux.

**La ligne arrive à l'écran quand la requête part.** `insertCreatingNode` pose
un `TreeNode{Creating: true}` au niveau parcouru, clé sur le chemin *prédit*
depuis le parent et le slug ; le spinner occupe la colonne d'icône, là où sera
le glyphe de kind. Quand la forge répond, `settleCreating` remplace le
placeholder par le vrai nœud **sur place** — pas de refresh : la forge vient de
dire ce qu'elle a fait, et relister l'arbre pour l'apprendre viderait la table
une seconde fois. Un échec retire la ligne et le dit au footer.

Les deux passent par le registre (`KindCreate`, `KindDelete`) plutôt que par un
drapeau local, et **ce n'est pas parce qu'un run à un item en vaudrait la
peine** — c'est pour trois choses qu'un drapeau ne donne pas :

- la frame vient du **broadcast**. La chaîne de spinner de cette vue s'arrête
  sur `!m.loading` (`handleSpinnerTick`), donc une frame prise dessus gèlerait
  sur la frame zéro dès l'arbre stabilisé — c'est l'argument de D5, atteint par
  l'autre bout ;
- `busy()` répond depuis le registre, donc il voit le travail lancé **ailleurs** ;
- `Run.Context` est estampillé au lancement, donc une création qui survit à un
  changement de contexte ne recharge plus l'arbre de celui d'après. C'est la
  famille de [D68](#11-fixed), refermée là où elle restait ouverte.

Ce que le registre ne donne pas, et qu'il a fallu écrire : la ligne optimiste
elle-même. Le registre suit le travail, il ne pose pas de nœud dans l'arbre.

Trois conséquences, chacune la raison d'une pièce de la forme :

| | |
|---|---|
| Le chemin est une **prédiction** | la ligne a besoin d'une clé avant qu'il y ait une réponse. La forge est libre d'en renvoyer une autre, donc `Target` voyage sur le message — c'est la seule chose qui puisse encore retrouver la ligne posée |
| La ligne est **inerte** tant qu'elle tourne | `actionable()` la refuse, `GetShortcuts` grise `D`, et la touche pressée dit laquelle des deux raisons s'applique (Rule 130). Un placeholder ne porte pas d'identifiant, donc une action le visant enverrait un ID vide |
| L'échec va au **footer**, pas à `m.error` | `m.error` remplace l'arbre par un écran d'erreur — le seul endroit d'où la disparition de la ligne ne se voit pas (Rule 128) |

`KindCreate` répond `false` à `Cancellable()` : une requête déjà partie ne se
dé-envoie pas, et un projet dont le template est à moitié appliqué est un état
que la forge détient. Le test qui parcourt `Kinds()` a refusé le kind tant que
la table D7 ne l'avait pas.

### 3.60 `G` tire une image comme un travail, et la table le montre — **done**

`G` dans le browser de registres était la dernière action de cette vue à ne pas
passer par le registre. Elle appelait `docker.PullImage` derrière son propre
écran de statut, donc le pull n'existait que là : invisible dans `:jobs`,
absent de la chaîne de spinner, et — ce qui se voyait le plus — la table Images
ne disait rien pendant qu'une image se téléchargeait. Le scan direct
(`ctrl+s`), juste à côté dans le même écran, passait par `jobs.Start` depuis
§3.58.

`KindPull` était déclaré depuis §3.58 — son verbe, sa réponse à `Cancellable()`
dans la table D7 — et n'était **utilisé nulle part**. C'était le seul kind dans
ce cas ; le test qui parcourt `Kinds()` vérifiait un vocabulaire dont un mot ne
servait à rien.

Le browser n'appelle donc plus Docker lui-même : il émet
`RegistryPullRequestedMsg`, exactement comme `requestDirectScan` émet sa
demande de scan, et le parent l'admet.

**`G` ferme le browser et pose l'utilisateur dans l'onglet Images**, et l'écran
de statut du browser est supprimé avec ce qui le portait — `browserStateStatus`,
`operation`, `imageName`, `viewStatus`, `SetOperationSuccess/Error`,
`OperationImageName`. Il prenait tout le panneau pour un `theme.SpinnerMessage`
(la forme que Rule 139 tient hors d'un corps de table) et il le faisait sur le
seul écran d'où ni la progression ni le résultat ne se voient : l'image tirée
apparaît dans la liste des images, pas dans la liste des tags. La ligne qui
tourne dans Images dit strictement plus, et à l'endroit où on la cherche.

Le changement de vue a lieu **une fois, sur la touche qui le demande** — ce
n'est pas un message de travail qui déplace l'écran dans le dos de
l'utilisateur, ce que D67 interdit. La distinction est celle-là : répondre à
une action, ou reprendre l'écran pendant qu'on est ailleurs.

Conséquence à ne pas manquer : `handleRegistryPullComplete` commençait par
`if m.registryBrowser == nil { return m, nil }`. Le browser étant désormais
fermé bien avant la réponse, cette garde aurait avalé le `fetchImages()` qui
fait apparaître l'image — le pull aurait « marché » sans que rien n'arrive à
l'écran avant le tick de dix secondes.

**La ligne arrive à l'écran quand la requête part.** Un pull lancé depuis le
browser vise presque toujours une image qui n'est *pas* locale — c'est la
raison même de parcourir un registre — donc décorer « la ligne » ne décorait
rien dans le cas courant. `imageRows` synthétise une ligne pour tout nom en vol
absent de `m.images`, le spinner dans la colonne ID (Rule 122 : `Cell` rend le
texte, `Style` le colore). Elle est remplacée par la vraie quand
`fetchImages()` répond, ce que `handleRegistryPullComplete` déclenchait déjà.

| | |
|---|---|
| La clé retombe sur le **nom** | `Image.ID` est vide sur un placeholder, donc deux pulls simultanés partageraient une clé vide et `datatable` n'en verrait qu'un |
| L'état vient du **registre**, jamais d'un drapeau | `pullingNames()` est recalculé à chaque `jobs.ChangedMsg`, donc rien à remettre à zéro et rien qui puisse rester allumé — c'est ce que §3.58 a acheté |
| Le placeholder ne porte **aucune** donnée d'image | taille, scan, secrets : le pull n'en connaît encore rien, et inventer un `-` par colonne se lirait comme une réponse |

#### Implémenter `jobs.Reporter` ne suffit pas : il faut être **routé**

Les deux messages implémentaient `Transition()`, et
`var _ jobs.Reporter = RegistryPullStartingMsg{}` le prouvait à la
compilation — mais rien ne les dispatchait par `routeWork`, la liste explicite
de `internal/app/app.go`. `Transition()` n'était donc jamais appelé : le run
restait `queued` pour la vie de la session, et la ligne tournait sans fin.

L'assertion de compilation prouve qu'un message **sait** rapporter, jamais
qu'il rapporte. C'est le trou que `TestAPullIsRegisteredAndAdvancedByItsOwnMessages`
ferme, dans `internal/app` et non dans la vue : le défaut était celui du
routeur.

Au passage, `RegistryPullCompleteMsg` n'atteignait la vue que par le
`default: forwardToActiveView` — donc seulement tant que la vue OCI était à
l'écran. Quitter l'onglet pendant un pull perdait la complétion, et avec elle
le `fetchImages()` qui fait apparaître l'image. `routeWork` passe par
`routeToView`, qui livre que la vue soit affichée ou non.

#### `K` devait couper pour de bon

`Kind.Cancellable()` répond `true` pour `pull` depuis §3.58 — « a docker pull
resumes by layer ». Enregistrer le pull sans lui donner de contexte annulable
rendait donc `K` **offerte et non grisée** dans `:jobs`, `CancelItem` ne
trouvant aucun `cancel` à appeler, ne faisant rien, et retournant quand même
`true` : le refus silencieux que Rule 130 existe pour supprimer, en pire —
l'action prétendait avoir réussi.

`dockerCmd` gagne donc un `Ctx`, honoré par `exec.CommandContext` quand il est
posé, et `PullImageContext` est le seul appel mutant du paquet à en prendre un.
Un `Ctx` nil construit exactement la commande d'avant, ce qui est ce qui garde
le changement additif : une suppression qui en porterait un offrirait une coupe
que D7 interdit, et `TestOtherMutationsCarryNoContext` le vérifie.

Le contexte voyage sur le message de départ, comme celui du scan et pour la
même raison : il est créé dans le `Cmd`, donc le registre le range dans l'Update
même qui marque l'item running — en deux temps il y aurait une fenêtre où la
ligne tourne sans pouvoir être arrêtée.

Un pull coupé se règle en `ItemFailed`, comme un scan coupé : le message de fin
porte l'erreur du processus tué et ne sait pas la distinguer d'un échec réseau.
C'est le comportement du scan depuis §3.58 ; les séparer demanderait que
l'annulation soit lisible dans l'erreur, ce qu'aucun des deux ne fait
aujourd'hui.

### 3.61 Le serveur MCP passe dans le TUI, en HTTP, et il agit — **done**

§3.38 est renversée sur trois axes. Ce n'est pas une extension : trois de ses
décisions tombent, et une quatrième est sauvée autrement.

| §3.38 | Ce qui la fait tomber |
|---|---|
| stdio, pas de HTTP en v1 | **un agent en conteneur ne peut pas exécuter le binaire hôte.** C'était la question ouverte 1 de §3.38 ; c'est devenu le cas d'usage principal — l'agent tourne dans une sandbox `sbx`, et stdio n'y arrive pas |
| lecture seule, aucun tier `act` | un agent qui sait ce qui a été scanné mais ne peut pas lancer le scan fait faire à l'utilisateur le geste qu'il vient de lui décrire |
| un contexte par process, fixé au démarrage | il n'y a plus de process à part : le serveur est celui de la session ouverte, donc son contexte est celui de l'écran |
| « la lecture seule achète l'absence de verrou » | **sauvée autrement**, et mieux — voir plus bas |
| « un agent qui se trompe rencontre une modale ; ici il n'y a personne » | **rendue sans objet**, pas levée — voir plus bas |

#### Les décisions

| # | | |
|---|---|---|
| 1 | Transport | **Streamable HTTP**, servi par le process du TUI. `NewStreamableHTTPHandler` existe dans le SDK déjà présent (v1.7.0, `mcp/streamable.go`) |
| 2 | Écoute | `mcp.listen`, défaut `127.0.0.1:7777`. Le LAN est exclu **par construction** |
| 3 | Activation | `mcp.enabled: false` par défaut — inchangé, c'est le bon modèle |
| 4 | Portée | le **contexte courant de la session**, qui change quand l'utilisateur en change |
| 5 | Écriture | oui : cloner, scanner, synchroniser, tirer. **Aucune action destructrice**, pas même derrière une confirmation |
| 6 | Autorisation | `mcp.expose` reste l'allow-list unique, plus un bearer token tiré du store de secrets |
| 7 | Contrat | toute action rend un `job_id` immédiatement ; l'agent interroge `jobs_get` |
| 8 | stdio | **supprimé**, avec la sous-commande `dk mcp` |

#### Le loopback suffit, sandbox comprise

Il n'y a pas d'arbitrage à faire entre « joignable depuis la sandbox » et « pas
sur le LAN ». Sur Docker Desktop — l'hôte est Windows — `host.docker.internal`
depuis un conteneur atteint les services liés au **loopback de l'hôte** : la VM
proxifie. Donc `127.0.0.1` est à la fois le bind le plus fermé possible et
celui qui marche.

Et il y a un second verrou, gratuit : la **network policy de `sbx`**. Le port
doit être autorisé explicitement (`sbx policy allow network "localhost:7777"`),
faute de quoi la requête est refusée en 403 avec son motif. Une sandbox n'a
donc pas accès au serveur parce qu'elle est une sandbox, mais parce que
quelqu'un l'a dit.

`listen` est un réglage plutôt qu'un `127.0.0.1` en dur pour une seule raison,
et elle n'est pas la configurabilité : sur un Docker natif Linux
`host.docker.internal` ne suffit pas, il faudrait binder la gateway du bridge.
Ce cas n'existe pas ici et n'est pas traité — mais un littéral dans le code
demanderait de le réécrire, un réglage demandera de le documenter.

**`url` serait un champ de client.** Le serveur choisit une adresse d'écoute ;
c'est l'agent qui a une URL. Nommer le réglage `url` ferait croire que DevDesk
sait où on le joint, ce qui est faux dès qu'un conteneur passe par
`host.docker.internal`.

#### Un seul écrivain, donc toujours pas de verrou

C'était l'objection sérieuse. `~/.devdesk/` n'a aucun verrou et rien n'avertit
quand deux écrivains se croisent : c'est ce que la lecture seule de §3.38
achetait. Écrire depuis un **second process** le reperdrait entièrement.

Un serveur **hébergé par le TUI** ne crée pas de second écrivain. Il y a un
process, une boucle `Update()`, et c'est déjà la seule chose qui a le droit
d'écrire (Rule 110). La garantie change de forme sans se dégrader : elle passe
de « le serveur n'écrit pas » à « le serveur écrit par le même chemin que le
clavier ».

La contrainte qui en découle est stricte : **le handler HTTP ne touche jamais
le modèle.** Il fait exactement ce que fait une touche.

```
handler HTTP  →  p.Send(mcp.InvokeMsg{ID, Tool, Args, Reply})
                   ↓
                 Update()  — jobs.Start, puis dispatche les Cmds
                   ↓
                 Update()  — à la complétion, écrit dans Reply
                   ↓
handler HTTP  ←  répond
```

`tea.Program.Send()` est la seule porte légale ; `main.go` a déjà le `p`. Ce
qui manque est la corrélation `ID → chan`, une map **possédée par le routeur et
mutée depuis `Update()` seul**. L'annulation de la requête HTTP doit libérer le
handler *et* retirer l'entrée, sinon un agent qui coupe fuit une goroutine et
une case de map par appel.

**`jobs.Registry` reste non concurrent, et il ne faut pas y toucher.** Son
commentaire dit pourquoi (`registry.go`) : un mutex dirait qu'un `Cmd` peut
l'écrire, et c'est précisément ce qui doit rester faux. Cette architecture le
respecte — le serveur n'est pas un écrivain de plus, c'est une source de
messages de plus.

**Les outils de lecture ne changent pas de chemin.** Ils continuent de passer
par `internal/cache/readonly.go` et par le daemon, comme aujourd'hui. Lire le
modèle serait tentant — il est là — et ce serait la data race que Rule 110
existe pour interdire. Le bénéfice est nul par-dessus le marché : §3.38 a
vérifié que tout ce qu'ils répondent est sur disque ou ailleurs. `readonly.go`
survit intact, avec sa propriété la plus utile : **un outil de lecture ne peut
rien décider.**

#### Les jobs sont le contrat, pas un détail d'implémentation

Cloner, scanner, synchroniser, tirer sont **déjà** des jobs — `jobs.Kind` les
nomme depuis §3.58. Donc un outil d'action ne rend pas un résultat, il rend un
identifiant :

| Outil | Rend |
|---|---|
| `clone_start`, `scan_start`, `scan_all_start`, `sync_start`, `pull_start` | `{job_id}`, tout de suite |
| `jobs_list`, `jobs_get` | l'état, l'avancement, le verdict |
| `jobs_cancel` | et `Kind.Cancellable()` répond déjà pour lui |

`jobs_cancel` n'a **aucune règle à inventer** : la table D7 de §3.58 dit qu'un
scan et un pull se coupent proprement, qu'un clone, une synchro, une création
et une suppression non. Un refus vient de cette table, écrite une fois, et pas
d'un jugement porté ici sur ce qu'un agent a le droit d'arrêter.

C'est ça qui justifie le couplage au TUI, et c'est le seul argument qui le
justifie : `jobs.Registry` est le seul état que le disque ne porte pas. Un
serveur headless ne peut pas répondre « le scan tourne encore ».

#### La surface d'action se déduit, elle ne s'invente pas

DevDesk a déjà l'inventaire exhaustif de ce qu'il sait faire : le vocabulaire
majuscule de `internal/ui/keymap`, tenu par
`TestNoViewBindsAnUndeclaredUppercaseKey`. La règle de sélection s'écrit donc
en une phrase — **un outil par entrée du vocabulaire qui garde un sens sans
écran** — et un test peut parcourir les deux tables plutôt que réviser une
liste.

| Retenu | |
|---|---|
| `C` `S` `A` `F` `G` | cloner, scanner, scanner tout, se remettre à jour, tirer |
| `K` | mais sur un **job**, pas sur un conteneur — voir ci-dessous |

| Écarté | Pourquoi |
|---|---|
| `D` `P` `K`(conteneur) | destructrices — décision 5 |
| `N` `M` | créer et renommer ne sont pas destructifs, mais **leur seul défaire est `D`**, qui n'est pas exposée. Une action irréversible parce qu'on a retiré son inverse est pire qu'une action destructrice assumée |
| `T` `O` `W` `V` `L` `B` `R` `I` | ouvrent un process interactif ou un écran sur le bureau de quelqu'un. Un agent n'en fait rien |
| `U` | login/logout touche au store de secrets (§3.9) |
| `X` `Y` | exclure un finding écrit dans la configuration (décision : pas en v1) ; copier un chemin vise le presse-papier de l'hôte, sans objet ici |

#### Aucune action destructrice — et pourquoi la modale ne revient pas

§3.38 refusait le tier `act` avec une phrase : *« un agent qui se trompe de
ligne dans le TUI rencontre une modale ; ici il n'y a personne. »* Le TUI
tournant désormais par définition, il **y a** quelqu'un, et l'objection semble
levée. Elle ne l'est pas : elle est rendue sans objet, ce qui vaut mieux.

Une modale de confirmation ouverte par un appel MCP bloquerait l'agent sur un
événement que l'utilisateur ne regarde peut-être pas — il est dans son éditeur,
pas dans DevDesk. L'appel pend une heure, ou le client abandonne et l'agent
conclut que DevDesk ne répond pas. On aurait échangé une action dangereuse
contre un dialogue invisible.

Donc **la classe d'actions qui avait besoin d'une modale n'est pas exposée**.
C'est la même forme que les trois garanties de §3.38 : `contextGetOut` n'a pas
de champ pour un secret, `finding` n'a pas de `Match`, `expose` est une
allow-list. Une action jamais enregistrée ne peut pas être mal confirmée.
`mcp.expose` reste donc la seule autorisation, et il n'y a **pas** de
`mcp.allow_writes` — ce serait un second booléen disant à peu près la même
chose que le premier.

#### Le contexte courant, et ce qu'il faut ajouter pour que ce soit tenable

§3.38 refusait de suivre le contexte courant, et son motif reste vrai : *ça
change ce que le serveur répond sous l'agent, en pleine conversation.* La
décision 4 l'accepte, à deux conditions :

1. **Chaque réponse porte le nom du contexte qui l'a servie.** Un agent qui
   compare deux réponses voit le changement au lieu de le subir.
2. **Une notification MCP au changement de contexte.** Le protocole a des
   notifications serveur→client ; c'est sa réponse native, et elle vaut mieux
   qu'un champ que l'agent peut ne pas relire.

Un job lancé dans le contexte A pendant que l'utilisateur bascule sur B
continue dans A — il a ses paramètres. `jobs_get` doit donc dire dans quel
contexte le job tourne, faute de quoi l'agent lit un résultat en croyant qu'il
parle de B.

Corollaire de la décision 4 : basculer vers un contexte où `mcp.enabled` est
faux **arrête** le serveur, et l'inverse le démarre. Le réglage est par
contexte ; il serait incohérent que le serveur survive au contexte qui
l'autorisait.

#### Le token, et le réglage qu'il ne faut pas écrire

Un serveur en lecture qui fuit une liste de dépôts est ennuyeux. Un serveur qui
**clone et scanne** sur le loopback est atteignable par n'importe quel process
local — un `postinstall` npm, une extension d'éditeur. La policy `sbx` protège
la sandbox ; elle ne protège pas l'hôte de lui-même. Le bearer token n'est donc
pas une option.

Et il heurte une règle du dépôt : **aucun secret que DevDesk détient n'est
écrit dans un fichier que DevDesk possède** (§3.9).

| | |
|---|---|
| dans `config.yaml` | ❌ exactement ce que §3.9 a supprimé |
| **dans le store de secrets** (`internal/credentials`), révélé à la demande dans la vue configuration pour être collé dans la config de l'agent | ✅ retenu |
| éphémère par session | cohérent avec le cycle de vie, mais il faudrait le recoller à chaque lancement — inutilisable |
| aucun | défendable pour un serveur en lecture, plus du tout ici |

**Si `credentials.Select()` retombe sur `MemoryStorage`, le serveur refuse de
démarrer** et le dit. Un token qui ne survit pas au redémarrage casserait la
configuration de l'agent en silence, une fois par session : la panne serait
attribuée à l'agent, jamais au store. C'est la forme de `mcp.Refused()`, qui
nomme le réglage *et* le contexte pour la même raison.

Ce que DevDesk ne peut pas empêcher, et qu'il faut donc écrire : un token collé
dans un `.mcp.json` committé part sur GitHub au premier push. Il va dans la
configuration **utilisateur** de l'agent.

#### Un second `dk` : le footer le dit, et seulement s'il y avait quelque chose à lancer

Deux sessions ouvertes — deux worktrees, deux contextes — et la seconde échoue
au bind. Elle **continue sans serveur** et pose un message de footer : perdre le
TUI parce qu'un port est pris serait disproportionné.

Au sens de Rule 128 c'est un **`Error`** : le système a refusé une opération.
Pas un `Warn` — rien ici n'est « ne peut pas être honoré tel que demandé », le
bind a échoué.

Et il n'est posé **que si `mcp.enabled` est vrai dans ce contexte**. Sinon rien
n'a été tenté, donc il n'y a rien à signaler ; un message annonçant l'échec
d'un serveur que personne n'a demandé se lirait comme une panne.

#### Ce que le renversement supprime

- la sous-commande `dk mcp`, son `flag.FlagSet`, son `--context` et la dizaine
  de lignes en tête de `main()` ;
- `mcp.Refused()` sous sa forme actuelle — le refus n'a plus de stderr où
  aller, il devient le message de footer ci-dessus ;
- `TestTheMCPBranchIsTakenBeforeAnythingPrints`, qui gardait un ordre
  d'instructions qui n'existe plus ;
- `TestNothingInThisPackageWritesToStdout`, **et c'est le point à ne pas
  manquer** : sa raison entière était que dans stdio *stdout est le canal*.
  Sans stdio, écrire sur stdout ne corrompt plus rien. Le garder laisserait un
  test qui se lit comme une contrainte encore vraie, ce qui est pire que pas de
  test — quelqu'un le lira comme une propriété du paquet.

Ajouté en face : le handler HTTP, la map de corrélation, le token, `mcp.listen`,
et l'onglet de configuration passe de deux scalaires à trois plus une action
« révéler le token ».

#### Non retenu

- **Toute action destructrice** — supprimer, purger, arrêter, tuer un
  conteneur. Ni exposée, ni derrière une confirmation, ni derrière un réglage.
- **Créer et renommer**, parce que leur seul défaire est une action
  destructrice.
- **L'écriture de configuration** — monitors, registries, bascule de contexte.
  Pas en v1.
- **stdio**, et avec lui la possibilité de répondre quand `dk` n'est pas lancé.
  C'est le coût assumé de la décision 4 : DevDesk conduit, l'agent est passager.
- **Tout bind hors du loopback**, y compris `0.0.0.0` derrière un réglage.
- **`container_logs`** — inchangé depuis §3.10 et §3.38 : les logs portent des
  variables d'environnement et des DSN de façon routinière.
- **Le `Match` d'un finding de secret** — inchangé, et c'est une propriété du
  schéma, pas un filtre.

#### Le plan

Le plan était [`.claude/plans/mcp-server-in-tui.plan.md`], sept phases en trois
lots ; il a été retiré à la livraison, cette entrée étant le compte rendu qui
dure. Ce que la préparation a établi et que cette entrée
ne pouvait pas savoir : `jobs.StartMsg` est le **point de passage unique** où un
run est admis et son identifiant alloué (`handleStartJobs`, `internal/app/jobs.go`).
Un outil d'action n'a donc aucun run à construire — il envoie une requête, la
vue la bâtit comme pour une touche, et le routeur rend l'identifiant. La
corrélation voyage sur le run, à la manière du stamp de contexte de D68 : un
`pendingInvoke` consommé par le prochain `StartMsg` aurait une fenêtre d'un
cycle `Update` où une touche s'intercale, et l'agent recevrait l'identifiant du
scan que l'utilisateur venait de lancer à la main.

#### Ce que la réalisation a changé à l'énoncé

Fait le 2026-09-05, en cinq commits. **Neuf décisions ont dû être prises contre
ce qui est écrit plus haut ou dans le plan**, et chacune parce que l'énoncé se
contredisait, nommait quelque chose de déjà pris, ou demandait au SDK ce qu'il
ne garantit pas.

**Le point de non-retour a été franchi d'abord.** Un agent en sandbox `sbx`
atteint bien `http://host.docker.internal:7777` une fois
`sbx policy allow network "localhost:7777"` passée sur l'hôte, avec le serveur
lié à `127.0.0.1`. C'était l'hypothèse dont dépendait le choix du bind et le seul
motif de l'entrée ; tout le reste a été écrit après l'avoir vue répondre.

**Les phases 3 et 4 ont fusionné, et la frontière a bougé pour une raison.** La
boucle d'invocation seule n'a pas d'appelant : la tester demanderait de
l'échafaudage de test dans du code de production. `jobs_list` et `jobs_get` sont
son premier consommateur, et ils sont en **lecture**, donc le mécanisme a été
validé avant qu'aucune action n'existe.

**La map de corrélation n'existe pas pour une réponse immédiate.** Le plan la
prévoyait en phase 3 ; une réponse rendue dans l'`Update` qui reçoit la demande
n'en a pas besoin — le canal bufferisé à un suffit, et il est collecté quand les
deux côtés lâchent. Elle est arrivée avec les actions, où la réponse est
différée d'un cycle. La déclarer plus tôt aurait été ce que §3.60 reprochait à
`KindPull` : un mot du vocabulaire que rien n'emploie.

**`Run.Origin` était déjà pris** — c'est la vue qui a démarré le run, ce dont
`:jobs` a besoin pour renvoyer l'utilisateur au bon écran. Le champ de
corrélation s'appelle `Invocation`.

**`jobs.WithInvocation` est un décorateur, pas un quatrième constructeur.**
`Start`, `StartInContext` et `StartCancellable` varient déjà sur deux axes ; une
variante portant l'invocation de chacun en ferait six. La vue emballe la
commande qu'elle rendait déjà, au seul endroit qui sait qui a demandé.

**La notification MCP au changement de contexte a été abandonnée**, et remplacée
par mieux. `ServerSession.Log` du SDK ne part que si le client a posé un niveau
de log : c'est une garantie qui tient quand elle veut. Le redémarrage du serveur
sur bascule **coupe les sessions ouvertes**, ce qu'aucun client ne peut rater —
il perd sa session et réinitialise plutôt que de se mettre silencieusement à
lire un autre contexte. C'est `Close` et non `Shutdown` : une requête en vol
appartient au contexte qu'on quitte.

**`clone_start` n'est pas livré.** `C` ouvre une sélection que l'utilisateur
construit en parcourant l'arbre de la forge, puis un second écran pour la
destination ; `handleCloneDestinationSelected` la résout par `rootNodes()` et
`m.selection`, l'état d'un arbre que quelqu'un a parcouru. Un agent n'a rien de
tout ça. L'exposer demande un chemin de résolution sans arbre — un chemin de
groupe vers un ensemble de nœuds — ce qui est une fonctionnalité à spécifier.
La règle du vocabulaire tient ; ce vers quoi elle pointe n'existe pas encore.

**`jobs_cancel` a été corrigé contre sa première version**, qui refusait un
clone au motif que `Kind.Cancellable()` dit non. C'était lire la table D7 de
§3.58 de travers : **la file s'arrête toujours**, et seul le travail déjà en vol
n'est coupé que là où le couper ne laisse rien derrière. Le seul refus est un
run déjà réglé.

**Deux choses ont dû être ajoutées ailleurs**, et ni l'une ni l'autre n'était
prévue :

- `components.PostFooterMsg`. Le routeur n'a pas de footer — `RenderFooter` est
  celui de la vue active — donc l'échec de bind n'avait aucun chemin vers
  l'écran. Toutes les vues offrent déjà les messages non traités à
  `FooterMessage.Handle`, donc une diffusion suffit ; elle porte son propre ID
  parce que la minuterie est celle de l'émetteur.
- `kindSecret` dans la vue configuration. Une ligne statique en clair aurait été
  le seul endroit de l'application montrant un secret non masqué — le token de
  forge et le mot de passe de registry posent tous deux `EchoPassword`.

**Le tampon de contexte était aux trois quarts déjà là** : `workspaces_list`,
`registries_list` et `scan_inventory` portaient déjà `context`. Ce qui manquait
était `scan_result` et les réponses nouvelles. Le test qui tient la règle trouve
les réponses **là où elles sont rendues** — le deuxième résultat d'un handler
dont le premier est `*sdk.CallToolResult` — et non par leur nom : une première
version filtrait sur le suffixe `Out` et réclamait le champ à `socketOut` et
`containerOut`, qui sont des lignes *dans* une réponse.

**Un refus de plus que prévu, et il est structurel** : un store de secrets qui
ne persiste pas empêche le serveur de démarrer. `credentials.Select` retombe sur
la mémoire quand aucun store hôte ne répond, donc un token serait régénéré à
chaque lancement — cassant la configuration de l'agent une fois par session, en
silence, avec la panne attribuée à l'agent.

#### Ce qui reste ouvert

Les quatre questions ci-dessous n'ont pas bougé, plus une cinquième que la
réalisation a créée : **`clone_start`**, ci-dessus.

1. **Docker natif Linux.** `host.docker.internal` n'y atteint pas le loopback
   de l'hôte ; il faudrait binder la gateway du bridge, ce qui expose le
   serveur à tous les conteneurs de la machine et pas seulement à la sandbox. À
   traiter quand le cas existera, pas avant.
2. **Resources et prompts MCP.** La question ouverte 2 de §3.38 est intacte :
   un résultat de scan est adressable et immuable, donc naturellement une
   *resource*. Un `job_id` l'est aussi, et une resource qui change est
   exactement ce à quoi servent les notifications.
3. **Quitter `dk` avec un job en vol lancé par un agent.** Le TUI demande-t-il
   confirmation ? L'agent perd sa réponse dans tous les cas, mais un clone coupé
   laisse un demi-dépôt sur le disque — c'est pour ça que `Kind.Cancellable()`
   répond non.
4. **La rotation du token.** Un bouton dans la vue configuration invalide la
   configuration de l'agent sans que rien ne le lui dise. Peut-être n'y a-t-il
   rien de mieux à faire que de l'écrire.

### 3.62 Le binaire sait ce qu'il est, `:about` le dit, et les releases se publient seules — **done**

Jusqu'ici `dk` ne savait rien de lui-même : aucun tag dans le dépôt, aucune
variable de version, aucun `-ldflags`. Un rapport de bug ne pouvait nommer la
build qu'il décrivait, et rien à l'écran ne disait d'où venait le binaire.

#### `internal/version` — trois sources, un ordre

La question « quelle version tourne » a trois réponses possibles, et elles ne se
valent pas :

| Source | Donne | Quand |
|---|---|---|
| `-ldflags -X` | la version | goreleaser en release, `mise run build` en local |
| métadonnées VCS (`-buildvcs=auto`) | le commit, la date, l'état modifié | tout build fait dans un dépôt |
| `Main.Version` de `debug.BuildInfo` | la version | `go install pkg@v0.2.0`, et là seulement |

`Get()` les consulte dans cet ordre et **rien n'écrase jamais un `-X`** : un
drapeau est une affirmation délibérée, les métadonnées un défaut. Le test qui
tient cet invariant est celui qui vaut la peine — sans lui, un binaire de
release construit depuis un tag annoncerait le commit du build plutôt que la
version demandée, ce qui est précisément l'erreur que la chaîne existe pour
éviter.

**Un binaire qui ne sait pas répond `dev`, pas `v0.0.0`.** C'est la même
distinction que `unknown` pour le commit : ne pas savoir et prétendre à une
valeur sont deux choses différentes, et un numéro inventé est ce qu'on citerait
dans un rapport de bug en croyant dire quelque chose.

**Seule la version passe par `-ldflags`.** Le commit et la date, le compilateur
les embarque déjà ; les poser aussi ferait deux sources pour une réponse. Et
`git describe` est appelé **sans `--always`** : sans tag il échoue et le repli
est `dev`, là où `--always` promeut un SHA au rang de numéro de version.

#### `:about` — le binaire, pas la machine

La vue est une **vue** et non une touche majuscule, pour la raison que `:jobs`
est une vue : le vocabulaire majuscule agit sur une ressource *dans* un écran,
et celle-ci est un écran (Rule 111). Pas d'alias d'une lettre non plus — `a`
vaut mieux que cet écran, qu'on ouvre une fois pour lire une version.

**Ce qu'elle montre est le binaire, pas l'environnement.** Les versions de
Trivy, gitleaks, plumber et Docker sont sur le dashboard et y restent : elles
décrivent la machine, elles changent sans que DevDesk soit reconstruit, et il
faut aller les chercher. Ici rien n'est mesuré — tout est connu au démarrage —
et **c'est ce qui justifie l'absence de `ctrl+r`** : une touche qui ne pourrait
jamais devenir disponible n'est pas une touche grisée, c'est une touche qui
n'appartient pas à la vue (Rule 130).

La version est aussi dans le header du **dashboard**, et d'aucun autre écran :
c'est la vue d'accueil, donc la seule où l'information se lit sans avoir été
cherchée. La répéter partout coûterait une colonne de header par vue pour une
valeur qui ne change jamais en cours de session.

Deux gardes de source ont eu leur mot à dire, et les deux avaient raison :
`TestEveryTypeableViewIsDocumented` a refusé la vue tant que `app-shell.md` ne
la nommait pas, et `TestNoViewNamesAForge` a refusé l'URL du dépôt. La seconde
est une exception déclarée : `https://github.com/anthnel/devdesk` n'est pas du
vocabulaire de forge — la valeur ne dépend d'aucun contexte et ne changerait pas
si l'utilisateur configurait GitLab. C'est une adresse, au même titre que le
chemin de `~/.devdesk`.

#### Étape 2 — la publication

**release-please plutôt que semantic-release, et la raison n'est pas une
préférence.** Les deux lisent les mêmes conventional commits et décident la même
version ; ce qui les sépare est par où la version arrive sur `main` :
semantic-release **pousse le tag directement**, release-please ouvre une **PR de
release** que le merge transforme en tag et en Release. Ce dépôt interdit le
push direct sur `main` — le mirror Entire le rejette, et ce refus est la seule
protection qui existe puisque la branch protection est indisponible sur ce plan.
semantic-release ouvrirait donc une seconde voie vers `main`, précisément celle
que la règle ferme. Le coût est un merge de plus par release.

`goreleaser` construit ensuite les six cibles sur le tag et les attache à la
Release. La cross-compilation est triviale parce que le projet est **cgo-free**
(`go-keyring` parle à wincred, Keychain et Secret Service sans lui) : une seule
machine `ubuntu-latest` couvre linux, darwin et windows en amd64 et arm64, en
28 s mesurées. `CGO_ENABLED=0` est posé explicitement plutôt que laissé au
défaut de cross-compilation — une dépendance qui se mettrait à en avoir besoin
fait alors échouer la release au lieu de produire un build linux seul.

**Le partage des rôles est écrit dans les deux configurations** : release-please
décide la version et possède le CHANGELOG, goreleaser possède les artefacts et
rien d'autre. D'où `release.mode: append` et `changelog.disable: true` — l'un ou
l'autre laissé à son défaut ferait écrire à goreleaser un second jeu de notes
par-dessus celles avec lesquelles la PR a été relue.

`.Tag` et non `.Version` dans les `-ldflags` : goreleaser retire le `v` initial
du second, là où `mise run build` estampille ce que rend `git describe`, qui le
garde. Un binaire de release et un binaire local doivent épeler leur version de
la même façon.

**L'inconnue de la chaîne est levée.** Le tag est posé par GitHub Actions, donc
il naît sur GitHub et non via le mirror ; restait à savoir si la réplication
GitHub → mirror, que le CLAUDE.md documente pour `main`, couvre
`refs/tags/*`. Vérifié le 2026-09-06 en posant `v0.0.0-mirror-test` par l'API
GitHub : `git fetch origin --tags` le ramène. Rien n'oblige donc à poser un tag
à la main, et `git describe` reste juste en local.

Un dernier point n'est pas dans les fichiers et serait oublié : *Settings →
Actions → Allow GitHub Actions to create and approve pull requests*. Aucun
workflow ne peut se donner ce droit, et le premier run échoue sur la PR qu'il ne
peut pas ouvrir avec un message qui ne dit pas que c'est ça. Il est **déjà
actif** — vérifié plutôt que supposé, par
`gh api repos/anthnel/devdesk/actions/permissions/workflow`.

**Les 180 commits de `main` sont conventional, sans exception** — vérifié plutôt
que supposé, et c'est la seule condition d'entrée de l'outil. Le squash-merge y
est pour beaucoup : le titre de la PR devient le sujet du commit, donc c'est
lui, et lui seul, qui doit être conforme.

---

## 4. Existing plans

Detailed plans live in `.claude/plans/`. One is outstanding:

- [`platform_compatibility_improvements.md`](../.claude/plans/platform_compatibility_improvements.md)
  — Docker-layer platform portability. Referenced from the old `todo.md`.

Plans for shipped work are removed from `.claude/plans/` once their feature
lands and the corresponding §3.x entry above is marked **done** — the backlog
entry is the lasting record, not the plan file.
