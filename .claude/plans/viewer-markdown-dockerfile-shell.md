# Plan: le viewer colore Markdown, Dockerfile et shell — et rend le Markdown

**Branche**: `viewer-markdown-dockerfile-shell` (worktree `../devdesk-viewer-markdown-dockerfile-shell`)
**Backlog**: §3.32
**Complexité**: Medium

## Ce qui est demandé

1. Coloration syntaxique pour Markdown, Dockerfile et scripts shell.
2. Afficher un Markdown « en format brut ou pas » — donc deux affichages : la source avec
   ses marqueurs, et un rendu où les marqueurs disparaissent et deviennent du style.

## Ce que la sonde des lexers a établi

chroma 2.27.0 embarque `markdown`, `docker` et `bash`. Sortie réelle (mesurée, pas supposée) :

| Lexer | Types émis qui comptent |
|---|---|
| markdown | `GenericHeading`, `GenericSubheading`, `GenericStrong`, `GenericEmph`, `GenericDeleted`, `Keyword` (puces, `>`), `LiteralStringBacktick`, `NameTag`/`NameAttribute` (lien), `LiteralString` (délimiteurs de fence) |
| docker | `Keyword` (FROM/RUN/COPY), `LiteralString`, `NameVariable`, `Operator`, `Comment`, `Error` (les `\n`) |
| bash | `Keyword` (if/then/fi), `NameBuiltin`, `NameVariable`, `LiteralString*`, `CommentPreproc` (shebang), `Operator`, `Punctuation` |

Quatre conséquences :

- **La catégorie `Generic` n'est mappée nulle part** — un Markdown arriverait quasi incolore.
  C'est ce qui impose de nouvelles classes.
- **`Keyword` nu n'est émis par aucun des quatre lexers actuels** — JSON, YAML, TOML et XML
  n'émettent que `KeywordConstant`. Séparer les deux est donc une modification
  **à régression nulle**, et c'est vérifiable par un test.
- **Le lexer markdown émet un token par caractère** pour la prose (`"S"`, `"o"`, `"m"`, `"e"`).
  À 5 MiB (`viewer.MaxSize`) c'est ~5 M de `Token` — il faut coalescer.
- Bonus mesuré : une fence ```` ```go ```` est déjà sous-lexée en Go par chroma. Le rendu
  markdown hérite gratuitement de la coloration du code.

## Décisions

### D1 — `f` porte l'axe « brut ↔ dérivé », il n'y a pas de troisième écran

`f` fait déjà arbre ↔ texte. Le rendu markdown est la **même question** : la vue dérivée
contre le document tel qu'il est. Donc `display` gagne `displayRendered`, mais la bascule
reste binaire à tout instant — un kind a au plus une vue dérivée. Aucune touche nouvelle,
`f` est déjà déclaré pour le viewer dans `internal/ui/keymap`.

Un `.md` **s'ouvre rendu**, comme un `.json` s'ouvre sur son arbre. `c` reste orthogonal :
il éteint la couleur, il ne fait pas réapparaître les marqueurs.

En-tête : `markdown · rendered` / `markdown · raw`.

### D2 — le rendu est le même flux de tokens, marqueurs retirés

`RenderMarkdown([]Token) []Token` retire ce que **le lexer a déjà identifié** comme
marqueur, et rien d'autre. Ce n'est pas une implémentation de Markdown, et le commentaire
le dira.

| Entrée | Sortie |
|---|---|
| `GenericHeading "# Title\n"` | `"Title\n"`, ClassHeading |
| `GenericStrong "**bold"` + `"**"` | `"bold"` + `""` |
| `GenericEmph "*"`, `"emph"`, `"*"` | `""`, `"emph"`, `""` |
| `GenericDeleted "~~gone~~"` | `"gone"` |
| `LiteralStringBacktick` autour d'un code inline | les backticks tombent, la classe reste |
| `Keyword "-"` (puce) | `"•"` |
| `Keyword "\n> "` (citation) | `"\n│ "` |
| délimiteurs de fence et le nom du langage | supprimés |

Tout le reste passe intact. **Les liens ne sont pas touchés** : leurs crochets arrivent en
`Text` nu, indistinguables d'un `[` de prose, et les reconstruire serait de la devinette —
ce que ce paquet refuse partout ailleurs. Ils sont déjà colorés (texte en clé, URL en
attribut). Idem tableaux et règles horizontales : hors périmètre, dits comme tels.

Conséquence assumée : **l'invariant « concaténer les tokens reproduit l'entrée » ne tient
plus en mode rendu** — c'est le but. Il reste vérifié en mode brut, et `docLine.Plain` est
reconstruit depuis les tokens rendus, donc recherche et wrap suivent sans rien savoir.

### D3 — cinq classes de plus, deux couleurs de plus

`TokenClass` passe de 8 à 13. Trois des cinq nouvelles ne portent **pas de couleur** mais un
attribut, ce qui est le seul moyen de rendre `**gras**` et `~~barré~~` distinguables une
fois les marqueurs partis.

| Classe | Style | Pourquoi |
|---|---|---|
| `ClassKeyword` | `ColorSyntaxKeyword` = `ColorPrimary` | `FROM`, `RUN`, `if`, `fi` — c'est *la* chose à colorer dans un Dockerfile |
| `ClassHeading` | `ColorSyntaxHeading` = `ColorSecondary`, gras | titres |
| `ClassStrong` | `ColorText` + `Bold` | attribut, pas teinte |
| `ClassEmph` | `ColorText` + `Italic` | idem |
| `ClassStrike` | `ColorText` + `Strikethrough` | sans lui, `~~x~~` rendu est identique à du texte nu |

Deux mappings de plus, pour que shell et Dockerfile ne soient pas colorés à moitié :
`NameVariable*` / `NameBuiltin` / `NameFunction` → `ClassKey`, la famille « identifiant »
où vivent déjà les clés JSON, TOML et YAML.

Les deux couleurs sont des **alias sémantiques assignés dans `ApplyTheme`**, comme
`ColorChartBg` et les couleurs de footer : aucun fichier de thème ne gagne de clé.

`ColorSyntaxKeyword` vaut aujourd'hui la même chose que `ColorSyntaxLiteral`. C'est
l'argument déjà tenu pour `ColorFooterError` : le choix est invisible dans le thème par
défaut et cesse de l'être dans un thème qui les sépare — et `KeywordConstant` (`true`,
`null`) n'est pas `Keyword` (`if`).

### D4 — Dockerfile et shell sont reconnus par leur **nom**, pas par leur contenu

`Dockerfile` n'a pas d'extension : il tombe aujourd'hui dans `sniff()`. On ajoute
`basenameKinds`, consulté **avant** l'extension — sinon `Dockerfile.dev` a l'extension
`.dev` et retombe en texte.

| Reconnu | Kind |
|---|---|
| `.md`, `.markdown`, `.mkd` | `KindMarkdown` |
| `.sh`, `.bash`, `.zsh`, `.ksh` | `KindShell` |
| `.dockerfile` | `KindDockerfile` |
| basename `dockerfile`, `containerfile`, préfixe `dockerfile.` | `KindDockerfile` |
| basename `.bashrc`, `.zshrc`, `.profile`, `.bash_profile` | `KindShell` |

**Le shebang, lui, est sniffé** — et c'est une exception à écrire, pas à laisser passer.
`sniff()` ne s'applique qu'aux fichiers **sans extension du tout**, et `#!/usr/bin/env bash`
n'est pas un indice : c'est le fichier qui déclare son interpréteur, la déclaration la plus
explicite qu'un script porte. C'est l'inverse de `---` en tête d'un fichier, qui est du
front matter Markdown aussi souvent que du YAML. À dire dans le commentaire de `sniff`,
sinon le prochain relevé y verra une dérive.

### D5 — coalescence des tokens adjacents de même classe

`Tokenize` termine par une passe qui fusionne les runs voisins de même classe. Elle préserve
l'invariant de concaténation, supprime les tokens vides, et ramène un paragraphe Markdown
de N caractères à un token. Sans elle, un `.md` de 5 MiB alloue des millions de `Token`.
`Match` n'est jamais posé par `Tokenize` (c'est `MarkMatches`, après le filtre), donc la
fusion ne peut pas écraser une occurrence.

## Fichiers touchés

| Fichier | Action | Pourquoi |
|---|---|---|
| `internal/viewer/document.go` | UPDATE | 3 `Kind`, `Renderable()`, `Structured()` inchangé |
| `internal/viewer/detect.go` | UPDATE | `basenameKinds`, extensions, shebang |
| `internal/viewer/highlight.go` | UPDATE | 5 classes, `lexerName`, `classOf`, coalescence |
| `internal/viewer/markdown.go` | CREATE | `RenderMarkdown` |
| `internal/ui/theme/colors.go` | UPDATE | 2 déclarations d'alias |
| `internal/ui/theme/manager.go` | UPDATE | 2 assignations dans `ApplyTheme` |
| `internal/ui/viewer/syntax.go` | UPDATE | 5 branches de `syntaxStyle` |
| `internal/ui/viewer/model.go` | UPDATE | `displayRendered`, `applyDocument`, `toggleDisplay`, `formatLabel` |
| `internal/ui/viewer/lines.go` | UPDATE | `buildLines(doc, highlight, rendered)` |
| `internal/ui/viewer/update.go` | UPDATE | `FilterBarVisible` : « pas l'arbre » au lieu de « displayText » |
| `internal/ui/viewer/view.go` | UPDATE | `GetShortcuts` (Rule 130), `GetHelpContent` (Rule 114) |
| `.claude/CLAUDE.md` | UPDATE | section viewer |
| `docs/backlog.md` | UPDATE | §3.32 |
| `.claude/plans/viewer-markdown-dockerfile-shell.md` | CREATE | ce plan |

Aucune dépendance nouvelle : chroma embarque déjà tous ses lexers (c'est ce que les +4,0 Mo
du binaire ont acheté). `internal/ui/keymap` n'est pas touché : `f` et `c` y sont déjà
déclarés pour le viewer.

## Tâches

1. **Kinds + détection** — `KindMarkdown` / `KindDockerfile` / `KindShell`, `basenameKinds`,
   shebang, `Renderable()`.
2. **Classes + mapping + coalescence** — lu sur la sonde, pas deviné ; `KeywordConstant`
   reste `ClassLiteral`.
3. **Thème** — les 2 alias, les 5 branches de `syntaxStyle`.
4. **Rendu Markdown** — `RenderMarkdown`, câblé dans `buildLines`.
5. **La vue** — `displayRendered`, ouverture par défaut, `f`, en-tête, barre de filtre.
6. **Aide et raccourcis** — Rule 114 et Rule 130, dans le même commit.
7. **Documentation** — CLAUDE.md et §3.32.

## Tests

`internal/viewer`
- `TestDockerfileAndShellAreRecognisedByName` — `Dockerfile`, `Dockerfile.dev`, `Containerfile`, `deploy.sh`, `.bashrc`
- `TestAShebangDeclaresAShellScript` — fichier sans extension
- `TestAKeywordConstantIsStillALiteral` — **la garde de non-régression** de D3
- `TestMarkdownDockerfileAndShellTokensAreClassified`
- `TestHighlightingReproducesTheInputExactly` — table étendue aux trois kinds (mode brut)
- `TestAdjacentRunsOfOneClassAreCoalesced` — la garde de D5, sur un paragraphe de prose
- `TestRenderedMarkdownDropsTheMarkers`
- `TestRenderedMarkdownKeepsFencedCodeColored`
- `TestOnlyMarkdownIsRenderable`

`internal/ui/viewer`
- `TestMarkdownOpensRendered`
- `TestFSwitchesBetweenRenderedAndRaw` — et l'en-tête dit lequel
- `TestRawMarkdownShowsItsMarkers`
- `TestSearchWorksInRenderedMarkdown`
- `TestADockerfileReachesTheScreenColored`, `TestAShellScriptReachesTheScreenColored`
- `TestNeitherMarkdownNorShellOffersTheTree`
- `TestColoringOffKeepsTheMarkersHidden` — `c` et `f` sont orthogonaux

## Validation

```bash
go test ./internal/viewer/ ./internal/ui/viewer/ ./internal/ui/theme/ ./internal/ui/keymap/ -v
mise run check     # fmt + vet + lint + test
```

## Risques

| Risque | Probabilité | Parade |
|---|---|---|
| Le lexer markdown explose en tokens par caractère | **certaine** (mesurée) | D5, avec test |
| Le split `Keyword` / `KeywordConstant` change JSON/YAML/TOML | faible — aucun n'émet `Keyword` nu, vérifié sur les 4 lexers | `TestAKeywordConstantIsStillALiteral` |
| Le retrait des marqueurs casse la recherche | moyenne | `Plain` reconstruit depuis les tokens rendus ; test dédié |
| `~~x~~` et `**x**` indistinguables une fois rendus | certaine sans D3 | attributs `Bold` / `Italic` / `Strikethrough` |
| Un `.md` rendu perd de l'information (liens, tableaux) | assumée | `f` rend la source telle quelle, à un caractère près |

## Hors périmètre, dit explicitement

Tableaux, règles horizontales, listes imbriquées ré-indentées, réécriture des liens,
largeur de rendu adaptée au pane. Le mode brut reste la source exacte, donc rien n'est
perdu — c'est ce qui rend ces limites acceptables plutôt qu'à corriger.

## Acceptation

- [ ] `.md`, `Dockerfile`, `*.sh` s'ouvrent colorés
- [ ] `.md` s'ouvre rendu, `f` bascule vers le brut et retour, l'en-tête le dit
- [ ] `c` éteint la couleur sans révéler les marqueurs
- [ ] `/` filtre et surligne dans les deux modes
- [ ] JSON, XML, YAML, TOML et les logs sont **inchangés**
- [ ] `mise run check` passe
- [ ] CLAUDE.md et §3.32 écrits dans le même commit
