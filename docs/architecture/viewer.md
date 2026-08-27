# The document viewer — `internal/viewer` + `internal/ui/viewer`

> DevDesk architecture notes. Referenced from `.claude/CLAUDE.md`;
> read this file when working on the code it describes.

## The document viewer — `internal/viewer` + `internal/ui/viewer`

One document, read-only. A **destination with three producers**, the shape the
security view already has:

| Producer | Key | Source | Document |
|---|---|---|---|
| `workspaces` | `enter` on a file | `viewer.FileSource` | detected |
| `containers` | `enter` | `inspectSource` | JSON |
| `containers` | `L` | `logsSource` | log |

Two axes, and they are the whole interface: **display** (`f`) and **highlight**
(`c`). "Plain text" is text with the colour off; a display of its own for it
would be one screen reachable two ways — what §3.9 removed from the secret
backend and what took the `:theme` command with it.

**`f` is one axis with one meaning: the document as it is, against the one view
its kind derives from it.** A kind derives at most one — a tree when
`Kind.Structured()`, a rendered form when `Kind.Renderable()`, never both, and
`TestNoKindHasTwoDerivedDisplays` says so. That is what keeps the toggle binary
at every moment even though `display` has three values, and why `Model.derived()`
is one function rather than a switch at each of the three call sites that need
it: `f`, the opening display and `GetShortcuts` have to give the same answer or
the view offers a display it will then refuse to show (Rule 130).

| Kind | `f` switches between | Derived by |
|---|---|---|
| JSON, XML | the tree and the text | `parseJSON` / `parseXML` |
| Markdown | the rendered form and the raw source | `RenderMarkdown` |
| everything else | nothing — `f` is hidden (Rule 130) | — |

**A rendered Markdown is the same token stream with its markers removed**, and
nothing more. Every marker `RenderMarkdown` takes off is one chroma *already
identified* — a heading, a strong, an emphasis, a strikethrough, a bullet, a
quote prefix, a fence. Nothing there parses, and nothing decides from what a
character looks like: that is why **links, tables and thematic breaks survive
untouched**. A link's `[` and `]` arrive as bare `Text`, indistinguishable from a
bracket in prose, so rebuilding one would be exactly the guesswork this package
refuses everywhere else — the link text and its URL are coloured apart instead,
which is most of what rendering it would have bought.

The one invariant it breaks is Tokenize's: **the concatenation no longer
reproduces the input**. It is the point. Everything downstream copes because it
reads the tokens rather than the document — `splitTokenLines` rebuilds each
line's plain text from them, so the search filters and highlights what is
actually on screen. The raw display is still the source to the character, which
is what makes the omissions above limits rather than losses.

`c` stays orthogonal to all of it: turning the colour off in a rendered document
does **not** put the asterisks back. The rendering is a display, the colouring is
a colouring, and a `c` that revealed markers would be the second way to reach one
screen that the paragraph above rules out.

**A document carries its `Source`, not its bytes.** Reload, follow and the
timestamps toggle are questions for the origin. Three **single-method** optional
interfaces, probed by type assertion like `FooterView`:

| Interface | Unlocks | Implemented by |
|---|---|---|
| `Timestamped` | `t` | `logsSource` |
| `Followable` | `F` | `logsSource` |
| `Pageable` | `V` | `logsSource` |

One method each on purpose: `HeaderView`'s warning is about a view supplying two
of four and satisfying none in silence — a one-method interface has no
half-satisfied state. `WithTimestamps` returns a *new* source rather than
mutating one, so nothing is shared with a command in flight (Rule 110).

**`F` follows in the pane, and `Followable` returns an interval rather than a
command.** It used to return an `*exec.Cmd` run through `tea.ExecProcess`, on
the argument that `docker logs -f` already does this and re-implementing it
against a viewport would be re-implementing `less +F` badly. The argument was
true and beside the point: **the only way out of `docker logs -f` is ctrl+c, and
a suspended TUI does not intercept it** — so leaving the follow killed DevDesk
outright, and gave the terminal back in whatever mode the child had left it,
after which keys stopped answering. Observed, not theorised. A capability whose
only exit kills the application is not one.

Following is therefore `ctrl+r` on a timer: the same `loadCmd`, the same
`DocumentLoadedMsg`, the same handler, plus a `followTickMsg` — the ports tab's
shape. Three things follow from it, each with a test:

- **The generation ends a loop, not the flag.** `tea.Tick` blocks its whole
  interval, so a tick scheduled before `F` stopped still arrives; restarting
  before it lands would leave two loops reading for the life of the view.
  `stopFollowing` bumps `followGen` and is the one place following is turned
  off — `esc` included, since the router keeps this view and a loop left behind
  it would shell out every interval for a document nobody is looking at.
- **A followed document lands at the bottom**, where the new lines are. Every
  other arrival lands at the top, because a first load, a reload and a
  timestamps toggle all mean "here is the document".
- **`m.loading` stays false on a follow read.** The footer spinner belongs to a
  load the user waits on, and one blinking every two seconds reads as a fault in
  the thing that is working. `Following — F to stop` is what says the pane is
  live, derived per frame rather than posted (Rule 128).

The trade is stated rather than discovered: this polls, so a line can wait up to
one interval and a burst longer than `logsTail` is missed between two reads. `V`
still streams, and `less` is left with `q` rather than ctrl+c.

**A log line with no level inherits the one above it.** This is what the filter
rests on: a stack trace is a dozen unlevelled lines, and `≥ warn` swallowing them
destroys exactly what the log was opened for. The chain breaks on a blank line —
otherwise one ERROR colours half the file. A line before any level is
`LevelUnknown` and passes **every** filter. The cost, stated rather than
discovered: a genuinely unrelated unlevelled line after an INFO goes with it.

`v` cycles a **minimum** (`all → trace → debug → info → warn → error`), not four
toggles: that is what "verbosity" means and log levels are monotone. One
`FilterBar` token (Rule 136), gone at `all`.

**Six kinds are declared, never sniffed**: `KindLog`, `KindYAML`, `KindTOML`,
`KindMarkdown`, `KindDockerfile` and `KindShell`. "This looks like a log" is not
decidable, and neither is "this looks like YAML" — a file opening with `---` is
Markdown front matter as often as a YAML stream, and a `[section]` line is prose
in half the files that hold one. The registry `provider` field is the precedent.
JSON and XML keep a content sniff, but only for a file with **no extension**: a
`.md` opening with a tag is not a broken XML document, and saying so would be
noise. Hence `detection.Declared` — a parse failure is reported only when the
*name* claimed the kind.

**A name declares in two ways, and the basename is read first.** `Dockerfile`
carries no extension at all, and `Dockerfile.dev` carries `.dev` — which is in no
table, so an extension-first lookup would settle it as plain text before the name
was ever read. Hence `basenameKinds` and the `dockerfile.` prefix ahead of
`extensionKinds`. A dotfile goes the other way round and lands in the extension
table, because Go's `filepath.Ext(".bashrc")` returns the whole name; which table
holds it is an implementation detail, that it is found is not.

**The shebang is the one exception, and it is written down as one** — in
`sniff()`, in `TestAShebangDeclaresAShellScript`, and here. `#!/usr/bin/env bash`
is not an indication of what a file resembles: it is the file naming the
interpreter it is to be run by, which is an extension's standing minus the
extension. It applies only where `sniff` already did — a file with no extension
at all — and only to a short list of shells, because answering a
`#!/usr/bin/env python` with the bash lexer would colour a Python file wrong,
which is worse than leaving it plain since it looks deliberate.

**YAML, TOML, Dockerfiles and shell scripts are coloured, and have no tree.**
`Structured()` excludes YAML and TOML by decision, not by oversight: both have a
structure, but neither has a parser *here* that preserves the file's order, and
order is content (below). `yaml.v3` could do it — `yaml.Node` keeps order and
comments, and it is already a dependency — while TOML would cost another one.
Until that is worth doing they are text with the colour on, and `f` is hidden for
them (Rule 130). A Dockerfile and a shell script are not excluded from anything:
they are programs, and a program is read in its own order.

The class mapping is **read off the lexers**, not guessed, and each kind added
has said something the guess would have missed:

| Kind | What the reading found |
|---|---|
| YAML | nothing to do — keys arrive as `NameTag`, scalars as `Literal`, `true`/`null` as `KeywordConstant` |
| TOML | every key, table headers included, arrives as `NameOther`, which fell through to `ClassText` — a TOML coloured everywhere except the thing worth colouring |
| Markdown | the whole `Generic` category was unmapped, so a Markdown arrived very nearly colourless |
| Dockerfile, shell | instructions and `if`/`fi` arrive as a **bare `Keyword`**, and a shell variable as `NameVariable`, which fell through to ordinary text |

Neither the JSON nor the XML lexer emits `NameOther`, so the mapping is not
guarded on the kind; the classification tests are what keep that true. No
dependency and no binary growth for any of it: chroma embeds every lexer already,
which is what the +4.0 MB below bought.

**`Keyword` and `KeywordConstant` are separated, and the test is equality.**
`KeywordConstant` is `true`, `false`, `null` — a value, so `ClassLiteral`; every
other keyword is a word of the language, so `ClassKeyword`. The split could not
disturb what already worked, and that is checked rather than hoped: the JSON,
YAML, TOML and XML lexers emit `KeywordConstant` and never a bare `Keyword`, and
`TestAKeywordConstantIsStillALiteral` is what keeps it so. Written the obvious
way it would have been wrong — chroma implements a sub-category as
`t/100 == other/100`, so `InSubCategory(KeywordConstant)` answers **true for every
keyword there is** and silently swallows the other branch. `ColorSyntaxKeyword`
aliases `ColorPrimary`, the literal's colour: invisible in the default theme, and
a name for a theme that wants to separate them — the argument already made for
`ColorFooterError` against `ColorError`.

**Five classes carry a text attribute rather than a colour**, and Markdown is why:
once the rendered display has taken the `**` and the `~~` away, weight and
strikethrough are the only thing left saying two runs were ever different, and a
hue would say it less — a bold word is bold in any theme. `ClassStrong`,
`ClassEmph` and `ClassStrike` therefore keep `ColorText` and set `Bold`, `Italic`
and `Strikethrough`; `ClassHeading` takes `ColorSyntaxHeading` and bold both.
They still set a background like every other style (Rule 115), which is exactly
what an attribute-only style invites you to forget —
`TestEveryRenderedStyleCarriesTheAppBackground` asks each of them directly.

**`Tokenize` coalesces adjacent runs of one class**, and that is not tidiness. The
markdown lexer ends its inline rules with a catch-all single-character
alternative, so ordinary prose comes back **one token per character**: at the
5 MiB `MaxSize` ceiling that is millions of `Token` values for a paragraph that
is a handful of runs. `TestAdjacentRunsOfOneClassAreCoalesced` counts them. It
preserves the concatenation invariant — only the boundaries move — and drops the
empty runs several lexers emit between groups. `Match` is deliberately not
consulted: `Tokenize` never sets it, `MarkMatches` does, after the filter.

**Order is content.** Both parsers read a token stream (`json.Decoder.Token`,
`xml.Decoder.Token`), never a decoded value: `map[string]any` loses the file's
order. That is also why the tree's columns declare **neither `Less` nor
`Search`** — sorting would destroy what the parser took care to keep, and a text
filter would hide parents and orphan their children. `.` and `/` are unbound in
the tree, and Rule 138 says so by omission.

**The tree is a `datatable`, the text pane a `viewport`.** The tree qualifies for
a reason worth stating: **a tree cell carries exactly one syntax class**, so one
`Style` per cell is enough — `datatable` cannot express several colours in one
cell and never has to here. The text pane is not a table, so Rule 122 does not
apply; Rule 115 does, and every token style sets its background.

**Wrapping happens on tokens, never on coloured text.** A styled line cannot be
cut: the measure counts an escape's bytes as width and the cut lands inside the
sequence — Rule 122's hazard, one layer up. `docLine` keeps a line as spans,
`wrapTokens` splits it while it is still plain, and the colour goes on after.

**chroma is a lexer and nothing else.** Its formatters emit their own ANSI and
resets, and a reset mid-line takes the app background to the margin (Rule 115).
The `TokenType → TokenClass` mapping was **read off the lexers**, not guessed:
JSON and XML both emit `NameTag`, for a key and for a tag respectively, hence the
`kind` parameter.
The invariant everything rests on — concatenating the tokens reproduces the input
exactly — has a test of its own. Cost: **+4.0 MB** on the binary (19.9 → 24.0),
because chroma embeds every lexer.

Syntax colours are **semantic aliases assigned in `ApplyTheme`**, like
`ColorChartBg`: no theme file gains a key. Log levels get none —
`StatusErrorStyle`, `StatusWarningStyle` and `DimStyle` already mean that.
`ColorSearchMatch`/`Fg` are the same kind of alias.

**A search filters and highlights, and one rule decides both.** `MatchRanges` is
the only thing that says a line matched, and it says *where* in the same answer:
the filter is `len(ranges) > 0`, so "a line the search kept carries at least one
highlighted occurrence" holds by construction. Two calculations for one question
is what `scan.Categorize` and `Result.SecretVerdict` each had to undo, and the
symptom was identical both times — something counted in one place and absent from
the other. Here it would be a line kept with nothing visible in it.

The consequences, each with a test:

- **A match is one more span, never a colour laid over a finished line.**
  `MarkMatches` cuts the tokens while they are still plain, for the reason
  `docLine` exists at all. It runs *after* the filter, so only on the lines about
  to be drawn, and it keeps Tokenize's byte-for-byte invariant.
- **`matchStyle` overrides the class and the level both.** A log line comes out as
  level, match, level: the level still carries the rest of it, so an ERROR is
  still picked out at a glance, and a search invisible in a log would be missing
  precisely where the lines are longest.
- **`c` has no say over it.** An occurrence is not syntax colouring; turning the
  colours off is how someone reads a document as plain text, and a search they
  then could not see would be the one thing that cost them.
- **Cutting a token copies it** (`withText`, `appendSpan`) rather than rebuilding
  a literal. A literal has to name every field to keep it, which is how a
  highlight works right up until the line is long enough to wrap — the one case it
  exists for. Byte offsets, not runes: a token holds a string. And when
  `strings.ToLower` changes a string's length in bytes (`İ`), the search falls
  back to a case-sensitive one rather than pointing beside the match.
- **The case is a parameter of that same call** (`s`, §3.53), never a second
  reading in the view. It decides which lines survive *and* which spans are
  highlighted, so a filter consulting the flag on its own would be exactly the
  second calculation the rule above exists to prevent. It re-filters the query
  already in force rather than clearing it: comparing the two readings is the
  whole reason to press the key. The `İ` guard belongs to the folding branch
  alone — the sensitive one never folds, so it has nothing to be exact about.

**The bar carries what is filtering, and the two tokens are built together.**
`syncFilterTokens` emits the verbosity token and the `Aa` token in one pass
rather than each toggle setting its own: the bar disappears when nothing is
active (Rule 136), so what has to be right is *which tokens exist at all*, and
that is one question with one answer.

**Line numbers are the document's, and the gutter is not text** (§3.53). `n`
draws them, `docLine.Num` carries them, posted once in `buildLines`. Four things
follow, each with a test:

- **A filtered pane does not renumber.** The numbers keep gaps where a search or
  a verbosity dropped lines — that is the only reading that lets a line be quoted
  by its number, and it is what the gutter is for.
- **The gutter is not searchable, by construction.** It never enters
  `docLine.Plain`, so `MatchRanges` cannot see it. The cheap implementation —
  prefix, then filter — would pass every other test of the feature.
- **The gutter comes off the width before the wrap**, not after. `wrapTokens`
  counts runes and knows nothing about what is put in front of a segment, so
  wrapping at the full width and prefixing afterwards pushes every row past the
  right margin by exactly the gutter — on every line, which reads as a border
  fault rather than as a gutter one.
- **A wrapped line numbers its first row only.** A number says where a source
  line *begins*; repeating it would claim the document holds several lines
  bearing the same one. Dim, like every other value that is on every row.

**`g` goes to a line, and refuses twice rather than doing something adjacent.**
The prompt is a **mode**: it claims every key before the pane sees one, the way a
confirmation modal does, so a digit cannot also scroll and `esc` closes it rather
than leaving the viewer. `rowOfLine`, rebuilt with the pane, is the only thing
that knows where a document line landed — a row is not a line once a filter has
dropped some and a wrap has split others — and it holds the **first** row of a
wrapped line, so a jump never lands inside one.

| The number | What happens |
|---|---|
| empty | nothing, in silence — a change of mind, not a mistake |
| past the end | `Document has N lines` |
| hidden by the filter | `Line N is hidden by the filter`, and **nothing moves** |
| on screen | that line goes to the top of the pane, as `less` does |

The hidden case is the one worth keeping written down: scrolling to the nearest
visible line would report success while putting a different number under the
cursor, which is D20's shape — an absence rendered as something else. The user
can lift the filter, or not.

**The prompt takes the filter bar's slot, exclusively.** Both are one line inside
`components.BarFrame` — extracted from `FilterBar.View()`, so the rectangle that
closes the viewport has one implementation — and `GetFooterHeight` answers 2
either way. That is what stops the pane resizing under the reader when the prompt
opens over an active search. `InEditMode` and `FilterBarVisible` both count it.

**`g` was a retired vim alias, and it came back with a different meaning.**
§3.26 removed `g`/`G` as aliases of `home`/`end`; here the letter opens a prompt
and the jump takes an **argument**, which is precisely what `home` and `end` do
not cover. So it leaves `keymap_test.go`'s `retiredAliases`, with its reason
written there — the shape of `H` returning to `free` in §3.47, taken the other
way round. `j` and `k` stay retired: they are `down` and `up` under another name.

Three key collisions were resolved rather than accepted: follow moved `f` →
`F` (`ctrl+r` and `F` read as *reload once* / *keep reloading*); `h`/`l` are
unbound because no bare letter is navigation and no lowercase letter acts
(§3.26), hence `c` for coloration; and `q` closes nothing — it is the
application's quit key, and the logs pane was the one screen that ate it.

**What this deleted.** The containers logs pane — `viewState`, its `viewport`,
wrap, ANSI stripping, scroll keys, reload, follow, timestamps and the external
pager — moved here whole; `containers/update.go` went from 707 to 548 lines.
`wrapLines` and the ANSI/CR normalisation were **moved**, comments included:
their reasons apply to any text this application shows. Every `docker inspect`
pager path is gone, Windows temp files included. `V` survives for logs alone,
because `less` handles a gigabyte and follows it.

**`PagerCmd` carries no quote, on either branch.** Go's `exec.Command` escapes
an argument's inner quotes as `\"` when it builds a Windows command line, and
`cmd.exe` does not understand that escaping — it reads the backslashes as part
of the path. So `more "%TEMP%\devdesk-logs.txt"` reached cmd as
`C:\C:\Users\...\devdesk-logs.txt\`, which it refused, and `V` returned to
DevDesk instantly with an exit status nobody rendered: on Windows the pager
never once worked. Measured by running the exact string through
`exec.Command`, not deduced.

The temp file went with the quotes, because its stated reason was false: **`more`
reads a pipe** — `dir | more` is its canonical use. The two branches now differ
only in the shell and the pager's name, nothing touches disk, and no path needs
quoting. The container ID is still interpolated into a shell string, which is
safe only because it comes from `docker ps`; do not extend that to a value the
user types.

A failed pager is now **reported** (`Pager failed — check logs`). One that
cannot start comes back in milliseconds and is indistinguishable from one the
user quit at once, which is exactly how a command line broken since the day it
was written went unnoticed — the log line was there all along, and nobody reads
a log to find out why a key did nothing.

