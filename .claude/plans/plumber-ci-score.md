# plumber — un score de sécurité de pipeline, par dépôt

Plan d'implémentation de [§3.42](../../docs/backlog.md). La conception y est
tranchée et les mesures y sont consignées ; ce fichier ne les répète pas, il dit
dans quel ordre écrire et où chaque décision se pose dans le code.

**État au 2026-08-26** : aucune décision bloquante, un choix à faire avant
l'étape 3, une mesure impossible ici.

---

## Ce qui est déjà tranché

| | |
|---|---|
| La cible | le contexte cible une forge (§3.6), donc seuls les dépôts de **cette** forge sont scannés. Les autres : cellule vide, pas « scanné sans jeton » |
| Le jeton | celui de la session du contexte, et il ne part que vers l'hôte de `forge.url` — `internal/git.tokenForRemote` (§3.17) |
| `--provider` | déclaré depuis `forge.type`, jamais deviné du remote |
| `--branch` | on passe la branche courante et on accepte l'échec (chemin GitLab ; sans effet sur le chemin GitHub local) |
| La colonne | titre `CI`, `SizingFixed`, largeur 4, **pas** `Optional`, valeur la lettre seule |
| Le `?` | run dégradé et `ciMissing` ne sont pas distingués dans la cellule — l'onglet le dit |
| Le score | une ligne de tête dans l'onglet CI, jamais dans le header |
| L'onglet | `CI (n)` porte le **nombre d'issues**, comme ses quatre voisins |

## Ce qui reste à décider

**Un seul point, avant l'étape 3 : mode local ou mode distant sur le chemin
GitHub.** Les deux sont mesurés et la recommandation est *local* — voir §3.42,
« Le chemin GitHub a deux modes ». Le local note l'arbre de travail comme Trivy
et Gitleaks ; le distant note la branche par défaut du serveur et rendrait le
conteneur trivial. Le choix change ce que la colonne veut dire, pas la quantité
de code.

## Ce qui ne peut pas être mesuré ici

Un jeton GitLab **valide mais sous-doté** (403). Aucun contexte de cette machine
n'a d'instance GitLab. Un `401` tombe sur `2` ; un `403` devrait suivre, mais
c'est une déduction — §3.42 a montré deux fois ce qu'elles valent. À reprendre
quand un contexte GitLab existera, et à traiter d'ici là comme un `2`.

---

## Trois PR

Chacune se teste seule et la première ne casse rien si la suite attend.

### PR 1 — le réglage, visible et inerte — **fait le 2026-08-27**

**`internal/config`**

```yaml
scan:
  enable_ci_score: false     # off par défaut, comme les autres étapes
  plumber_source: auto       # auto | binary | image
  plumber_path: ""           # binaire hors PATH
  plumber_image: ""          # défaut getplumber/plumber
  plumber_config: ""         # --config : le fichier global
```

`plumber_config` est rendu **absolu au chargement**, comme `gitleaks_config`
depuis §3.50 : même raison, un relatif ne veut pas dire la même chose des deux
côtés de la frontière Docker. Une ligne dans `ExpandPaths`, rien de plus —
`absolute()` existe.

**`internal/scan`** — `DependencyStatus` gagne `PlumberAvailable`,
`PlumberSource`, `PlumberVersion`, `PlumberBinary`, `PlumberImage` et
`PlumberSpec() ToolSpec`. `CheckDependencies` le résout comme les deux autres :
`auto` prend le binaire s'il y en a un, l'image sinon ; `binary` **échoue
bruyamment** plutôt que de retomber sur Docker (D27).

Le chemin du binaire va **sur le `ToolSpec`**, jamais en paramètre positionnel.
C'est D27 mot pour mot : `trivy_path` est resté non lu longtemps parce que le
couple `(source, image)` n'avait nulle part où le porter.

La version se lit par `plumber version`, qui écrit **aussi** une ligne « une
version est disponible » — à ne pas confondre avec la version installée
(mesuré : `plumber v0.4.44 is available (you have 0.4.42)`).

**`internal/ui/configuration`** — la case **CI** dans le groupe *Scanners* avec
les quatre autres, et un groupe **Plumber** à côté de Trivy et Gitleaks. Cinq
lignes dans `fields.go`, chacune avec son unique accesseur pointeur.

**Tests** : le round-trip YAML, l'absolutisation, la résolution des trois
sources, `TestEveryFieldCarriesTheAccessorItsKindNeeds` et
`TestNoTwoFieldsAddressTheSameSetting` couvrent le reste tout seuls.

**Ce que l'écriture a ajouté au plan :**

- **Le dashboard nomme plumber.** `knownTools` et `detectTools` gagnent
  `toolPlumber` : si l'utilisateur coche CI et que plumber n'est pas installé,
  la boîte Host est le seul endroit qui le dira avant le premier scan. Les deux
  listes sont tenues par des constantes depuis D60, qui est exactement le défaut
  qu'un ajout à une seule des deux recrée.
- **Un test a trouvé une régression que le plan n'avait pas vue.** La compat
  « tout désactivé veut dire jamais configuré » forçait `enable_vuln` et
  `enable_secret` à `true` — donc cocher **CI seul** et décocher les quatre
  autres se faisait écraser à chaque chargement. `EnableCIScore` entre donc dans
  la **condition** sans jamais entrer dans ce qui est **écrit** : un fichier
  antérieur aux quatre booléens l'a à `false` lui aussi, donc la migration part
  encore, mais « les quatre éteints et CI allumé » est une configuration
  délibérée et cesse d'être réécrite.
- **La notice de mise à jour de `plumber version` part sur stderr**, la version
  installée sur stdout. `toolVersion` lit stdout seul, donc les deux ne peuvent
  pas être confondues — mesuré plutôt que supposé, parce qu'afficher la version
  *disponible* comme la version *installée* est le genre de chose que personne
  ne remarque pendant des mois.

### PR 2 — l'outil tourne, le cache retient — **fait le 2026-08-27**

**`internal/scan/plumber.go`** — le constructeur d'arguments, sur le patron de
`gitleaks.go` :

- `--score` n'est **pas optionnel** : sans lui il n'y a aucune sévérité dans le
  JSON, donc pas de colonne Severity, pas de jetons de filtre, rien à trier ;
- `--provider` depuis `forge.type` ;
- mode Docker : le dépôt monté sur le **répertoire de travail** (`-w`), le
  `safe.directory` levé par l'environnement
  (`GIT_CONFIG_COUNT=1`/`KEY_0`/`VALUE_0`), et `plumber_config` monté comme
  §3.50 monte celui de gitleaks — un nom de point de montage **écrit dans le
  paquet**, pas choisi au site d'appel. `/plumber.yaml` est libre ; `/plumber`
  ne l'est pas, c'est le binaire ;
- **jamais** `--score-push`, `--score-endpoint`, `--badge`, `--mr-comment`,
  `--platform` : ce sont des écritures sortantes, et l'absence du champ est la
  garantie (§3.38).

Les codes de sortie, mesurés, et **rien ne se déduit du silence** — c'est la
leçon de D56 :

| Code | Sens | Ce que DevDesk en fait |
|---|---|---|
| `0`, `1` | un score existe | la lettre |
| `3` | score retenu, données incomplètes | `?` |
| `2` | erreur d'exécution | échec de l'étape, avec le message de plumber |

**Le remplissage d'un `Finding` passe par une jointure `code → severity`.** Une
issue ne porte pas sa sévérité ; elle vit dans `plumberScore.codeLosses[]`,
indexée par `code`. Le vocabulaire tombe juste (`critical`/`high`/`medium`/`low`,
sans `unknown`), donc les quatre jetons cumulatifs marchent sans traduction.
`Title` se construit du `code` et du `controlName` ; `url` est un chemin hôte
suffixé `:<ligne>`, à rendre relatif au dépôt — et en mode Docker c'est le chemin
du conteneur.

**`internal/scan`** — `SourcePlumber = "plumber"`, `CategoryCIScore`, une ligne
dans `Categorize`. La classification se fait **sur la source et rien d'autre**
(§3.12) : pas de reconnaissance à la présence d'un champ.

`Result` gagne `CIScore` et `CIScanned bool`, ce dernier écrit par une étape qui
**réussit**, et **une seule** fonction décide du verdict — deux calculs de la
même question sont ce que `SecretVerdict()` a eu à défaire.

**L'étape dans le scanner** est conditionnée à `enable_ci_score`, à la
disponibilité de l'outil, **et** à la règle de cible : le remote du dépôt doit
être celui de `forge.url`. Jamais sur une image — plumber lit une configuration
CI, une image n'en a pas, et l'onglet est vide sur un résultat d'image.

**`internal/cache`** — `WorkspaceScanEntry.CIScore *string` : `nil` veut dire
que personne n'a regardé, exactement l'argument de `Sensitive *bool`.
`ImageScanEntry` ne gagne rien. Le champ n'ayant jamais été écrit, un fichier
existant décode en `nil`, ce qui est la vérité sur lui.

**Tests** : les quatre codes de sortie, la jointure sévérité, le montage et sa
position relative au nom de l'image, le refus d'un dépôt d'une autre forge, et
qu'aucun des cinq drapeaux sortants n'apparaisse jamais dans la commande
construite. Les fixtures sont **deux exécutions réelles** de plumber 0.4.40 sur
un même dépôt, ne différant que par la présence d'un jeton GitHub ; leurs
chemins sont neutralisés, rien d'autre n'est retouché.

**Le choix est pris : mode local.** La colonne parle du disque comme les autres
cellules de la ligne, et `--branch` reste sans effet sur ce chemin, ce qui laisse
l'arbitrage du 2026-08-25 à GitLab seul.

**Ce que l'écriture a appris :**

- **Par cible, pas par lot.** Un lot de scans porte des dépôts aux remotes et
  aux branches différents, donc un seul jeu d'options pour tout le lot noterait
  la mauvaise chose — ou noterait un dépôt d'une autre forge, ce que la règle
  existe pour empêcher. `Scanner.ciOptions` lit le remote de *ce* dépôt et
  refuse ; `ciOptionsFor` est la règle nue, testable sans dépôt sur disque.
- **`OptionsFromConfig` prend le `*config.Config` entier**, plus seulement sa
  section `scan` : la forge décide si un dépôt est noté, et ce n'est pas un
  réglage de scan. Douze sites d'appel, mécaniques.
- **Le JSON sort par deux routes, et c'est mesuré.** Dans un conteneur
  `--output /dev/stdout` marche ; le binaire natif Windows n'y écrit **rien**.
  Le mode binaire écrit donc dans un fichier temporaire qu'on lit et qu'on
  supprime. `--print=false` est ce qui garde stdout parsable.
- **Le run dégradé note *mieux* que le run complet.** §3.42 disait « le JSON
  continue d'écrire `"score": "E"` ». C'est pire : sur les deux fixtures, le run
  dégradé lit **B/79** là où le complet lit **E/30**, parce qu'un contrôle qui
  n'a pas tourné n'a rien trouvé. Reprendre la lettre flatterait un dépôt
  précisément quand on en sait le moins.
- **`partialControls` est un troisième état, plus faible que le retenu.** Un
  jeton *valide mais sous-doté* ne fait pas retenir le score : il désactive un
  contrôle, l'inscrit dans `partialControls`, et note quand même (exit 1). C'est
  la réponse GitHub à la moitié de la mesure 2 qu'on ne pouvait pas faire côté
  GitLab. La cellule affiche donc la lettre, qui peut être optimiste — à écrire
  quelque part en PR 3.
- **`toolCmd` gagne `Env`**, pour qu'un jeton n'aille jamais dans argv, qui est
  lisible depuis la liste des processus. `String()` ne le rend jamais : la
  commande est journalisée et affichée.
- **`git.SameHost` a déménagé.** La comparaison d'hôte vivait dans
  `internal/ui/workspaces`, où le sync décide si le jeton part ; un paquet de
  domaine ne peut pas importer une vue, et deux copies d'une comparaison d'hôte
  est la façon dont un dépôt finit par être digne de confiance pour une
  fonctionnalité et pas pour l'autre.
- **La vue security reçoit le magasin de secrets.** Sans lui, un rescan lancé
  depuis `:sec` n'aurait pas de jeton et rendrait `?` là où le même rescan lancé
  depuis `ws` rend une lettre : deux écrans notant un même dépôt différemment,
  ce que §3.11 et §3.12 ont chacun eu à défaire.

### PR 3 — les deux écrans

**`ws`** — la colonne `CI`. Quatre états : `A`…`E`, `?`, `-` (jamais scanné),
vide (pas scannable). La couleur passe par `Style`, jamais par `Cell`
(Rule 122), et suit la discipline : `A` est nominal et garde la couleur de
texte, `-`/vide/`?` sont `DimStyle`, la couleur est dépensée sur `D` et `E`.
Une fonction `theme.CIScoreStyle(state)` sur le modèle de `theme.SecretsState`
— **l'état, pas la chaîne rendue**.

**`security`** — `TabCIScore = 4`, le libellé `CI (n)`, une ligne dans
`tabCategory`, et la ligne de tête au-dessus de la table : un libellé `Score`,
`theme.IconChevronRight`, puis `C · 61/100` ou
`withheld — branch protection could not be fetched`.

Les `<contrôle>Result` qui **passent** ne sont pas des findings et ne vont dans
aucun onglet ; ils sont pourtant ce qui donne son sens au score. Les ignorer
d'abord, et le noter.

**Tests** : les quatre états de la cellule, la ligne de tête dans ses deux
formes, `TestEveryFindingIsCountedExactlyOnce` et
`TestTheTabCountsAgreeWithTheResultCounters` qui existent déjà et doivent
continuer de passer avec un cinquième onglet.

---

## Ce qu'il ne faut pas refaire

- **Ne pas ajouter de tolérance non mesurée sur un code de sortie.** La moitié
  de D56 n'était pas le montage manquant mais une tolérance écrite pour un
  comportement que gitleaks n'a pas. Les codes de plumber sont mesurés ; s'en
  tenir à la table.
- **Ne pas reprendre la lettre d'un run dégradé.** Le JSON écrit
  `"score": "E"` alors que la CLI dit *the score is withheld*. C'est D56 sur un
  autre outil.
- **Ne pas exposer les cinq drapeaux sortants**, même « pour tester ».
- **Ne pas deviner la forge du remote** : le contexte la déclare.
