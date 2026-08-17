# Plan: le viewer colore YAML et TOML, et une recherche montre où elle a trouvé

**Backlog section**: §3.29 (à créer)
**Complexité**: Medium — deux changements indépendants, l'un très petit, l'autre traverse la chaîne de rendu du texte
**Périmètre**: `internal/viewer`, `internal/ui/viewer`, `internal/ui/theme`

---

## 1. Ce qui est demandé

Deux choses, sans lien entre elles au-delà du fait qu'elles vivent dans le viewer :

1. **YAML et TOML sont colorés** comme JSON et XML le sont déjà — `.yaml`, `.yml`, `.toml`
   ouvrent avec la coloration syntaxique, et `c` l'éteint comme partout ailleurs.
2. **Une recherche met ses occurrences en surbrillance.** Aujourd'hui `/` ne fait que
   *filtrer* : les lignes qui ne contiennent pas la requête disparaissent, et sur une
   ligne de 300 caractères conservée rien ne dit *où* la requête a été trouvée.

### Hors périmètre, et dit explicitement

- **Pas d'arbre pour YAML ni pour TOML.** `Structured()` reste faux pour les deux, donc
  `f` reste masqué (Rule 130). Un arbre YAML supposerait un parseur qui préserve l'ordre
  — `yaml.v3` le fait via `yaml.Node`, et il est déjà une dépendance — mais TOML
  demanderait `pelletier/go-toml/v2` et son AST *unstable*, soit une dépendance de plus
  pour une seule vue. « L'ordre est du contenu » (§3.25) : un arbre bâclé sur un
  `map[string]any` serait un document différent de celui du fichier. Si l'arbre est
  voulu plus tard, c'est une section de backlog à lui.
- **Pas de sniff de contenu.** Un fichier sans extension ne devient pas YAML parce qu'il
  commence par `---`, ni TOML parce qu'il contient `[section]` : « ça ressemble à du
  YAML » n'est pas décidable, et c'est exactement l'argument déjà écrit pour `KindLog`.
  L'extension déclare, ou c'est du texte.
- **Pas de saut d'occurrence en occurrence** (`n` / `N`). La surbrillance répond à « où
  est-ce », le scroll existe pour y aller ; et aucune lettre nue n'est de la navigation
  (§3.26), donc `n` n'est pas disponible.

---

## 2. Décision à confirmer avant d'écrire du code

**La recherche continue-t-elle de filtrer ?**

| Option | Comportement | Conséquence |
|---|---|---|
| **A — filtrer *et* surligner** (recommandée) | inchangé : les lignes sans occurrence disparaissent, et sur celles qui restent la requête est en surbrillance | rien ne régresse ; la surbrillance dit *où* dans une ligne longue. Le compteur `matched/total` du header garde son sens, `TestSearchNarrowsTheText` et `TestAnEmptyResultSaysWhy` restent valables |
| B — surligner seulement | toutes les lignes restent, seules les occurrences sont colorées | c'est une autre fonctionnalité : `matchedLines`, `emptyTextMessage()` et le token du bar perdent leur objet, et sur un log de 50 000 lignes une recherche ne servirait plus à rien sans un `n` pour sauter — qui n'existe pas |

Le plan qui suit écrit l'**option A**. C'est la lecture littérale de la demande
(« ajouter la surbrillance »), pas un remplacement.

---

## 3. Ce que le code dit déjà (patterns à suivre, pas à réinventer)

| Catégorie | Source | Pattern |
|---|---|---|
| Détection par extension | `internal/viewer/detect.go:25` | `extensionKinds` map ; déclaré, jamais sniffé |
| Mapping chroma → palette | `internal/viewer/highlight.go:89` | `classOf(kind, TokenType)`, **relevé sur les lexers** |
| Invariant du lexer | `internal/viewer/highlight_test.go:12` | concaténer les tokens reproduit l'entrée à l'octet |
| Couleurs syntaxiques | `internal/ui/theme/manager.go:249` | alias sémantiques assignés dans `ApplyTheme`, aucun thème ne gagne de clé |
| Style d'un token | `internal/ui/viewer/syntax.go:21` | background posé sur **chaque** style (Rule 115) |
| Wrap avant couleur | `internal/ui/viewer/lines.go:89` | la ligne reste des spans jusqu'au rendu |
| Filtre du texte | `internal/ui/viewer/text.go:29` | `rebuildText()` recalcule, jamais `View()` (Rule 110) |

---

## 4. Partie 1 — YAML et TOML

### 4.1 Ce que les lexers chroma émettent réellement

Relevé en exécutant les deux lexers sur un échantillon (même méthode que §3.25, pas une
supposition) :

| chroma | YAML | TOML | classe visée |
|---|---|---|---|
| `Comment` | `# comment` | `# comment` | `ClassComment` ✅ déjà |
| `NameTag` | `app`, `name` (clés) | — | `ClassKey` ✅ déjà |
| `NameOther` | — | `title`, `port`, `app` (clés et noms de table) | **`ClassText` ❌ à corriger** |
| `Punctuation` | `:`, `[` | `=`, `[`, `]`, `.` | `ClassPunct` ✅ déjà |
| `Literal` | `devdesk` (scalaire nu) | — | `ClassString` ✅ déjà |
| `LiteralStringDouble` | — | `"devdesk"` | `ClassString` ✅ déjà |
| `LiteralNumber(Integer/Float)` | `8080` | `8080`, `1.5` | `ClassNumber` ✅ déjà |
| `KeywordConstant` | `true`, `null` | `true` | `ClassLiteral` ✅ déjà |
| `NameNamespace` | `---` | — | `ClassText` — à décider (voir 4.3) |

**Conclusion utile : YAML ne demande aucune modification de `classOf`.** Le seul trou
réel est TOML, dont *toutes* les clés sortent en `NameOther` et resteraient donc de la
couleur du texte — un document TOML colorié sauf ses clés, c'est-à-dire sauf ce qui
mérite la couleur.

### 4.2 Fichiers à modifier

| Fichier | Action | Pourquoi |
|---|---|---|
| `internal/viewer/document.go` | UPDATE | `KindYAML`, `KindTOML` ; `Structured()` inchangé (les deux restent faux) |
| `internal/viewer/detect.go` | UPDATE | `.yaml`, `.yml`, `.toml` dans `extensionKinds` |
| `internal/viewer/highlight.go` | UPDATE | `lexerName` : `yaml`, `toml` ; `classOf` : `NameOther → ClassKey` |
| `internal/viewer/detect_test.go` | UPDATE | les trois extensions, et l'absence de sniff |
| `internal/viewer/highlight_test.go` | UPDATE | l'invariant + une classification par kind |

Rien d'autre : le tour des `switch` sur `Kind` a été fait — `isLog()`, `syncVerbosityToken`
et `rebuildText` testent `== KindLog` et sont indifférents à un kind de plus ; l'en-tête
imprime `Kind.String()`, donc `yaml` / `toml` s'affichent sans un caractère de code.

### 4.3 Tâches

**T1 — les deux kinds**
- `KindYAML Kind = "yaml"`, `KindTOML Kind = "toml"` dans `document.go`.
- `Structured()` **inchangée**, avec un commentaire disant pourquoi YAML n'y est pas :
  il a une structure, mais pas de parseur ici qui préserve l'ordre — l'absence est une
  décision, pas un oubli.
- **Valider** : `go build ./...`

**T2 — la détection**
- `".yaml": KindYAML, ".yml": KindYAML, ".toml": KindTOML` dans `extensionKinds`.
- Le commentaire de `sniff` gagne une phrase : YAML et TOML ne sont jamais devinés.
- **Valider** : `go test ./internal/viewer/ -run TestDetect`

**T3 — le lexer et le mapping**
- `lexerName` : `KindYAML → "yaml"`, `KindTOML → "toml"`.
- `classOf` : dans le `switch t` du cas `chroma.Name`, ajouter `case chroma.NameOther:
  return ClassKey`, commenté avec ce que ça veut dire — `NameOther` est le nom que le
  lexer n'a pas su qualifier davantage, et en TOML c'est précisément une clé.
  *Non gardé par `kind`* : ni le lexer JSON ni le lexer XML n'émettent `NameOther`
  (relevé), et le test de T5 fige leur mapping pour que la régression soit impossible à
  introduire en silence. Si le relevé se révélait faux, la garde `if kind == KindTOML`
  est le repli d'une ligne.
- **Décision `---`** : `NameNamespace` reste `ClassText`. Un séparateur de document YAML
  n'est ni une clé ni une ponctuation de structure, et lui donner `ClassPunct` (donc
  `ColorDim`) l'effacerait alors que c'est un repère qu'on cherche des yeux. Laisser la
  couleur du texte est le choix le moins faux ; à revoir si ça gêne à l'usage.
- **Valider** : `go test ./internal/viewer/`

**T4 — la doc de l'aide (Rule 114, même commit)**
- `view.go` `GetHelpContent()` : la `Description` et la section « Coloring » disent
  aujourd'hui « JSON and XML ». Y ajouter YAML et TOML, en précisant qu'ils sont colorés
  **sans arbre** — sinon l'utilisateur cherche `f` sur un `.yaml` et ne le trouve pas.
- **Valider** : `go test ./internal/ui/viewer/`

**T5 — les tests**
- `TestDetect...` : `app.yaml`, `app.yml`, `Cargo.toml` → les bons kinds ; un `.md`
  commençant par `---` reste `KindPlain` (le sniff ne s'invite pas).
- `TestHighlightingReproducesTheInputExactly` : deux cas de plus (un YAML, un TOML), y
  compris un document mal formé — l'invariant octet-pour-octet est ce sur quoi repose
  l'alignement des lignes rendues avec celles du document.
- `TestYAMLTokensAreClassified` / `TestTOMLTokensAreClassified` : sur le modèle de
  `TestJSONTokensAreClassified`, une occurrence attendue par classe. Le test TOML est
  celui qui a une valeur réelle : sans le mapping `NameOther`, il échoue.
- **Un test de non-régression** : les classifications JSON et XML sont inchangées
  (`TestJSONTokensAreClassified` et `TestXMLTagsAndAttributesAreClassifiedApart` le font
  déjà — il suffit qu'ils continuent de passer, et le dire dans le commentaire de
  `NameOther`).

---

## 5. Partie 2 — la recherche met ses occurrences en surbrillance

### 5.1 Le problème de fond, et pourquoi ça touche cinq fichiers

Une ligne colorée ne peut pas être découpée : la mesure compte les octets d'une séquence
d'échappement comme de la largeur, et la coupe tombe *à l'intérieur* de l'échappement
(Rule 122, un étage plus haut). C'est pour ça que `docLine` garde la ligne en **spans**
et que la couleur n'est posée qu'à la fin, dans `renderSegment`.

Une occurrence de recherche est donc **un span de plus**, pas une couleur posée par
dessus : il faut découper les tokens aux frontières de la requête pendant qu'ils sont
encore du texte brut, exactement comme le wrap. Une requête traverse librement les
frontières de classe (`"name":` = string + punct), donc le découpage se fait sur le texte
*plat* de la ligne et se répercute sur les tokens.

### 5.2 Une seule règle décide « cette ligne matche » et « ici »

Aujourd'hui le filtre est `strings.Contains(strings.ToLower(line.Plain), query)`. Si la
surbrillance calculait ses positions autrement, on aurait **deux calculs pour une même
question** — le motif que §3.12 (`scan.Categorize`) et §3.20 (`SecretVerdict`) ont chacun
dû défaire, avec à chaque fois le même symptôme : un élément compté d'un côté et
introuvable de l'autre. Ici ça donnerait une ligne conservée par le filtre sans une seule
occurrence en surbrillance, et rien pour dire pourquoi.

Donc : **une fonction rend les positions, et le filtre est `len(positions) > 0`.**
« Conservée ⇒ surlignée » devient vrai par construction plutôt que par vigilance.

### 5.3 Fichiers à modifier

| Fichier | Action | Pourquoi |
|---|---|---|
| `internal/viewer/match.go` | **CREATE** | `MatchRanges` + `MarkMatches` — pur, testable sans lipgloss |
| `internal/viewer/match_test.go` | **CREATE** | l'invariant de concaténation, à nouveau |
| `internal/viewer/highlight.go` | UPDATE | `Token` gagne `Match bool` (+ doc) |
| `internal/ui/viewer/lines.go` | UPDATE | `Match` doit survivre au split et au wrap ; `renderSegment` |
| `internal/ui/viewer/text.go` | UPDATE | `rebuildText` : positions → filtre → marquage |
| `internal/ui/viewer/syntax.go` | UPDATE | `matchStyle()` |
| `internal/ui/theme/colors.go` + `manager.go` | UPDATE | `ColorSearchMatch{,Fg}`, alias sémantiques |
| `internal/ui/viewer/view.go` | UPDATE | aide (Rule 114) |
| `internal/ui/viewer/text_test.go` | UPDATE | la surbrillance, et son indépendance de `c` |

### 5.4 Tâches

**T6 — `internal/viewer/match.go`**

```go
// Range is a half-open byte range inside a line.
type Range struct{ Start, End int }

// MatchRanges is every place query occurs in text, case-insensitively.
func MatchRanges(text, query string) []Range

// MarkMatches splits tokens at the range boundaries and marks what falls inside.
func MarkMatches(tokens []Token, ranges []Range) []Token
```

- **Décalages en octets, pas en runes** : les tokens sont des `string`, donc un découpage
  par octet est exact et n'alloue pas de `[]rune`. Le wrap découpe ensuite par rune, ce
  qui est une autre question et reste inchangé.
- **Le repli d'exactitude** : `strings.ToLower` peut changer la longueur en octets (`İ`
  devient deux runes). Quand `len(strings.ToLower(text)) != len(text)`, les décalages du
  texte minusculisé ne désignent plus le texte d'origine — la fonction retombe alors sur
  une recherche sensible à la casse sur le texte original. Trois lignes, un cas exact
  plutôt qu'un décalage silencieux, et le commentaire dit lequel.
- Requête vide → `nil`. Ranges non chevauchantes, croissantes (avance de `End` après
  chaque trouvaille), donc `MarkMatches` fait un seul passage.
- `MarkMatches` **conserve l'invariant** : concaténer la sortie reproduit l'entrée.
- **Valider** : `go test ./internal/viewer/ -run TestMatch`

**T7 — `Token.Match`**
- Champ `Match bool` sur `Token`, et sa doc dit ce qu'il n'est pas : la classe dit ce que
  le texte *est*, `Match` dit que l'utilisateur vient de le chercher. Deux axes, pas une
  neuvième classe — une occurrence dans une clé reste une clé.
- `Tokenize` ne le met jamais : c'est `MarkMatches` qui décide, après le filtre.
- **Valider** : `go build ./...`

**T8 — le transport à travers le split et le wrap**
- `splitTokenLines` (`lines.go:69`) et `wrapTokens` (`lines.go:106`) reconstruisent des
  `viewer.Token{Class:…, Text:…}` — les deux doivent recopier `Match`. **C'est le bug le
  plus probable de tout ce plan** : sans ça la surbrillance marche jusqu'à ce qu'on
  active `w`, puis disparaît sur les lignes longues, précisément celles pour lesquelles
  la fonctionnalité existe. D'où un test avec `w` actif (T11).
- **Valider** : `go test ./internal/ui/viewer/`

**T9 — `rebuildText`**

Ordre imposé — le marquage vient **après** le filtre, sur les seules lignes conservées :

```go
for _, line := range m.lines {
    if isLog && !line.Level.Passes(m.minLevel) { continue }

    var ranges []viewer.Range
    if query != "" {
        if ranges = viewer.MatchRanges(line.Plain, query); len(ranges) == 0 {
            continue          // le filtre EST l'absence de position
        }
    }
    m.matchedLines++

    tokens := viewer.MarkMatches(line.Tokens, ranges)   // no-op si ranges est vide
    segments := [][]viewer.Token{tokens}
    if m.wrap && width > 0 { segments = wrapTokens(tokens, width) }
    …
}
```

- Le coût par frappe est celui d'aujourd'hui à un `ToLower` par ligne près — la même
  allocation que le `strings.Contains(strings.ToLower(...))` remplacé.
- `query` est déjà minusculisé et *trimmé* en tête de `rebuildText` : inchangé.
- **Valider** : `go test ./internal/ui/viewer/`

**T10 — le style, et le cas du log**
- `theme` : `ColorSearchMatch` et `ColorSearchMatchFg`, déclarées dans `colors.go` et
  **assignées dans `ApplyTheme`** — comme `ColorChartBg` et les couleurs syntaxiques, et
  pour la même raison : la palette n'est peuplée que par cette fonction, donc un
  `= ColorHighlight` au niveau du paquet capturerait le zéro et ne suivrait aucun
  changement de thème. Aliases : `ColorSearchMatch = ColorHighlight`,
  `ColorSearchMatchFg = ColorBlack` — aucun thème ne gagne de clé (Rule 119).
- `syntax.go` : `matchStyle()` = fond `ColorSearchMatch`, texte `ColorSearchMatchFg`.
  Une occurrence est en vidéo inversée, comme une ligne sélectionnée : c'est le signal le
  plus urgent de l'écran, il prend le pas sur la couleur de classe.
- `renderSegment` : le `Match` **gagne sur tout le reste**, classe syntaxique comme
  niveau de log.
- **Le commentaire de `renderSegment` doit changer.** Il affirme qu'une ligne de log est
  stylée « en un seul morceau, pour qu'un ERROR se repère d'un coup d'œil ». Ce n'est
  plus vrai : une occurrence dans une ligne de log la coupe en trois. C'est voulu — le
  niveau reste porté par tout le reste de la ligne, donc l'ERROR se repère toujours, et
  une recherche qu'on ne verrait pas dans un log serait la fonctionnalité manquante
  exactement là où les lignes sont les plus longues. Le commentaire doit dire ça, sinon
  le prochain lecteur croira à une régression.
- **Valider** : `go test ./internal/ui/viewer/`

**T11 — les tests**
- `TestSearchHighlightsItsOccurrences` : la ligne rendue contient la séquence de
  `matchStyle` autour de l'occurrence, et le texte dépouillé (`ansi.Strip`) est
  inchangé — on colore, on n'insère rien.
- `TestSearchHighlightSurvivesWrap` : `w` actif, requête au-delà de la largeur d'un
  segment → l'occurrence est toujours surlignée. C'est le test de T8.
- `TestSearchHighlightIgnoresColoringToggle` : `c` éteint, l'occurrence est **toujours**
  surlignée. Une occurrence n'est pas de la coloration syntaxique.
- `TestSearchHighlightsInsideALogLine` : le niveau colore le reste de la ligne, la
  requête son occurrence.
- `TestEveryKeptLineHasAnOccurrence` : sur `mixedLog`, chaque ligne rendue non vide
  porte au moins une occurrence surlignée — l'invariant de 5.2, opposé au code.
- `TestSearchNarrowsTheText` et `TestAnEmptyResultSaysWhy` (existants) doivent passer
  **sans être touchés** : c'est ce qui prouve que l'option A n'a rien retiré.
- `TestEveryTextLineCarriesTheAppBackground` (existant) : le fond d'une occurrence n'est
  pas celui de l'application, et la ligne est toujours paddée par `PadWithBg` — vérifier
  que le test tient tel quel.
- **Valider** : `go test ./internal/ui/viewer/`

---

## 6. Documentation (même commit — Rule 114, Rule 301)

| Fichier | Ce qui change |
|---|---|
| `.claude/CLAUDE.md` § « The document viewer » | le tableau des kinds gagne YAML et TOML ; dire qu'ils sont **déclarés, jamais sniffés** et qu'ils n'ont pas d'arbre, et pourquoi ; une phrase sur la surbrillance et sur la règle unique de 5.2 |
| `.claude/rules/tui-layout.md` Rule 111 § Document viewer | `/` y décrit la recherche : dire qu'elle filtre **et** surligne |
| `docs/backlog.md` | nouvelle section **§3.29**, insérée en tête de §1.1 pour l'entrée courte + la section détaillée à la suite de §3.28 |

Aucun changement dans `internal/ui/keymap` : pas de touche nouvelle. `/` est déjà globale
(Rule 111) et `f c w v t` sont déjà déclarées dans `localToggles` pour cette vue.

---

## 7. Validation

```bash
mise run fmt
mise run vet
mise run lint          # 0 warning — Rule 301
mise run test          # go test -v ./...
mise run test-race     # ./internal/ui/viewer/ au minimum (Rule 110)
mise run build
```

Vérification à l'œil, parce qu'une couleur ne se teste pas entièrement en unitaire :

```bash
mise run dev
# :w  → enter sur un .yaml, puis un .toml         → clés, valeurs, commentaires colorés
#       c                                          → tout redevient du texte
#       f                                          → ne fait rien, et n'est pas proposé
# :c  → enter sur un conteneur (inspect JSON), / + une requête présente deux fois par ligne
#       w                                          → la surbrillance survit au wrap
# :c  → L sur un conteneur bavard, / + une requête → niveau ET occurrence lisibles
```

## 8. Risques

| Risque | Probabilité | Atténuation |
|---|---|---|
| `Match` non recopié dans `splitTokenLines` / `wrapTokens` | **Haute** — deux constructions littérales faciles à manquer | T11 `TestSearchHighlightSurvivesWrap` |
| `NameOther → ClassKey` déteint sur JSON ou XML | Faible — relevé sur les deux lexers | les tests de classification existants ; repli `if kind == KindTOML` |
| Décalages faux sur une casse exotique (`İ`) | Très faible | le repli d'exactitude de T6, testé |
| Ralentissement par frappe sur un gros document | Faible — même coût qu'aujourd'hui | le marquage n'a lieu qu'après le filtre |
| Le fond d'une occurrence casse le padding de fin de ligne (Rule 115) | Faible | `TestEveryTextLineCarriesTheAppBackground` |
| Le lexer YAML de chroma est approximatif sur un scalaire multi-ligne (`\|`, `>`) | Moyenne, **acceptée** | l'invariant octet-pour-octet tient quand même : au pire une couleur est fausse, jamais un caractère |

## 9. Acceptation

- [ ] `.yaml`, `.yml`, `.toml` ouvrent colorés ; `c` éteint ; `f` reste absent
- [ ] Aucun fichier sans extension ne devient YAML ou TOML
- [ ] Une recherche surligne chacune de ses occurrences, avec `w`, avec `c` éteint, et dans un log
- [ ] Toute ligne conservée par la recherche porte au moins une occurrence surlignée
- [ ] `mise run check` passe ; `test-race` propre sur `internal/ui/viewer`
- [ ] Aide, Rule 111, CLAUDE.md et backlog §3.29 à jour dans le même commit
- [ ] Une branche, une PR (`gh pr create -R anthnel/devdesk`) — jamais de push sur `main`
