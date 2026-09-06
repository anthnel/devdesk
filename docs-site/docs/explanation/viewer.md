# Viewer

DevDesk's document viewer is a single, read-only pane used to display files, container inspection output, and container logs. This page explains the model behind it — why it has the shape it does, and the tradeoffs behind its design decisions.

## One destination, three producers

The viewer is a shared destination fed by different sources:

| Producer | Trigger | Document |
|---|---|---|
| Workspaces | `enter` on a file | Detected file content |
| Containers | `enter` | `docker inspect` JSON |
| Containers | `L` | Container logs |

The viewer only has two independent axes of behavior: **display** (toggled with `f`) and **syntax highlighting** (toggled with `c`). "Plain text" isn't a separate display mode — it's just a display with highlighting turned off. Keeping it that way avoids ending up with one screen reachable through two different toggles.

### The `f` toggle is strictly binary

Even though a document's `display` field can hold three values (raw, tree, rendered), `f` only ever switches between "the document as it is" and "the one view its kind derives from it" — never among three things. A document kind derives *at most one* alternate view: JSON and XML derive a tree, Markdown derives a rendered form, and no kind derives both. This invariant is what keeps the toggle meaningfully binary regardless of how many display values exist internally, and it's computed by a single function rather than duplicated at each of the places that need to agree — the toggle itself, the initial display chosen when a document opens, and the shortcut list shown to the user. If those disagreed, the UI would offer a display it then refuses to render.

| Kind | `f` switches between | Derived by |
|---|---|---|
| JSON, XML | Tree view and raw text | Streaming token parser |
| Markdown | Rendered form and raw source | Marker-stripping renderer |
| Everything else | Nothing — the toggle is hidden | — |

### Markdown rendering is marker removal, not parsing

The rendered Markdown view is produced from the *same token stream* as the raw view, with recognized markers removed. Every marker that gets stripped corresponds to a syntax class the tokenizer already identified: a heading, bold, italics, strikethrough, a bullet, a blockquote prefix, a code fence. Nothing is re-parsed, and nothing is inferred from what a character merely *looks like* — which is why links, tables, and horizontal rules are left untouched rather than reformatted. A link's `[` and `]` arrive from the tokenizer as plain text, indistinguishable from a literal bracket in prose, so rendering it as a clickable-looking link would require guessing — exactly what this approach avoids everywhere else. Instead, the link text and its URL are simply colored differently, which captures most of the practical benefit without the guesswork.

This approach deliberately breaks one invariant that otherwise holds throughout the tokenizer: concatenating the tokens back together no longer reproduces the original input, because markers are dropped. That's acceptable because everything downstream — search, line-splitting, highlighting — operates on the token stream, not the raw document, so the omission is a known limit rather than a source of bugs. The raw view remains character-for-character faithful.

The `c` (highlighting) toggle is intentionally orthogonal: turning off syntax color in a rendered Markdown view does not bring the asterisks back. Rendering and coloring are two independent operations, and conflating them would recreate the same "one screen reachable two ways" problem the design avoids elsewhere.

## Sources carry capabilities via narrow interfaces

A document carries a reference to its `Source` rather than just its bytes — questions like "can this be reloaded," "can this be followed like `tail -f`," and "does this support timestamps" are properties of *where the document came from*, not of the document itself. These are exposed as three single-method interfaces (`Timestamped`, `Followable`, `Pageable`), each probed with a type assertion, rather than one large interface every source must fully implement. The reasoning: a wide interface invites a source to implement two of four methods and silently do nothing for the rest, whereas a source either implements a one-method interface correctly or doesn't implement it at all — there's no half-satisfied state to reason about.

### Following logs: why it polls instead of streaming a subprocess

An earlier version of "follow" (`F`) shelled out to `docker logs -f` via a subprocess and streamed its output directly into the viewport — reusing the underlying `docker` CLI's own follow behavior rather than reimplementing it. In practice, that was the wrong tradeoff: `docker logs -f` can only be interrupted with Ctrl-C, but a suspended TUI application does not intercept Ctrl-C, so leaving a document open in follow mode killed the entire application and handed the terminal back in whatever state the child process left it. A capability whose only way out crashes the program isn't a usable capability.

Follow is therefore implemented as a periodic reload on a timer instead — the same load path used for a manual refresh, called automatically at an interval. The tradeoff is stated explicitly rather than hidden: this means a new log line can be delayed by up to one interval, and a burst of output larger than the tail window can be missed between two reads. A separate "open in system pager" action still exists for cases that need true streaming, at the cost of losing in-app search and highlighting.

A few consequences of building follow this way, each independently worth knowing:

- Stopping follow needs to invalidate any timer tick that was already scheduled before the stop — otherwise a tick in flight when the user turns follow off can still land and silently restart it.
- A followed document opens scrolled to the bottom, since that's where new content appears; every other way of opening a document (first load, manual reload, toggling timestamps) scrolls to the top, since those all mean "here's the document."
- The loading spinner is suppressed during automatic follow reloads — a spinner blinking every couple of seconds reads as something being wrong, when in fact everything is working as intended. A quieter "Following" indicator communicates the live state instead.

## Log level filtering: unlabeled lines inherit context

Verbosity filtering (`v`) cycles through a *minimum* level (all → trace → debug → info → warn → error) rather than exposing four independent toggles, because that's what "minimum severity" naturally means, and log levels form an ordered scale rather than an unordered set.

A log line with no explicit level annotation inherits the level of the line above it, until a blank line breaks the chain. This matters because a stack trace is typically a dozen unlabeled lines following one labeled error — without inheritance, filtering to "warnings and above" would hide the entire stack trace and defeat the point of looking at it. A line with no level *before* any labeled line has appeared passes every filter (it's not filtered out at any level), on the theory that an unclassified line is better shown than hidden. The known cost: an unrelated unlabeled line that happens to follow a labeled `INFO` line inherits `INFO` too, even if it isn't actually related.

## File kind detection is declared, not sniffed

The viewer works with a small, explicitly declared set of document kinds (log, YAML, TOML, Markdown, Dockerfile, shell script, and a few more) rather than trying to *guess* a file's kind from its content. The reasoning is that many of the obvious heuristics are simply wrong often enough to be unusable: a file starting with `---` is Markdown front matter as often as it's a YAML document, and a line like `[section]` is ordinary prose in a large fraction of files that happen to contain one.

JSON and XML are the one exception, and only partially: content sniffing is used for them, but *only* when a file has no extension at all — a `.md` file that happens to open with an angle bracket is not treated as malformed XML. Kind detection therefore has two tiers: a name-based lookup first (matching the basename before the extension, since `Dockerfile` has no extension and `Dockerfile.dev`'s `.dev` extension matches nothing), and content sniffing only as a fallback for extensionless files.

A shebang line (`#!/usr/bin/env bash`) is treated as an explicit declaration rather than a guess, on the reasoning that a shebang is the file *telling you* what interpreter runs it — functionally equivalent to an extension, just spelled differently. It's honored only for extensionless files and only for a small allow-list of known shells, because misidentifying, say, a Python script as a shell script for coloring purposes would actively mislead a reader, which is worse than leaving it uncolored.

YAML and TOML are colored but have no derived tree view: both have real structure, but the parsers used for them don't currently preserve key order, and for a viewer that reflects the file as written, order is part of the content worth preserving. Programs (Dockerfiles, shell scripts) are never candidates for a tree view at all — a program has an inherent read order, and a tree would misrepresent that.

## Syntax coloring is read off actual lexer output

The token-class-to-color mapping used for syntax highlighting was derived empirically, by inspecting what each language's lexer actually emits, rather than assumed. This mattered in practice: the TOML lexer, for instance, emits every key (including table headers) under one generic token type that had no color mapping, so a TOML document rendered almost entirely without color despite having real structure worth highlighting. Markdown's lexer similarly emits an entire category of tokens that had gone unmapped, leaving rendered Markdown nearly colorless. Each of these was only caught by actually reading lexer output rather than assuming a "reasonable" mapping would cover it.

A related subtlety: some token subtype checks in the underlying lexer library use integer-division comparisons that can silently match far more broadly than intended, so a naive mapping can end up making an entire category of *keywords* (not just the intended subset, like `true`/`false`/`null`) look like literals. Getting this right required verifying the split with a dedicated equality-based test rather than trusting the subtype check.

Five syntax classes — bold, italic, strikethrough, and headings among them — are given a text attribute (bold, italic, strikethrough) rather than a distinct color. This matters specifically for the *rendered* Markdown view: once markers like `**` and `~~` have been stripped, weight and style are the only remaining visual signal that two runs of text were ever different — a color alone would say it more weakly than a bold word says it in any color theme.

## Structural details worth knowing

A few implementation choices that fall out of the reasoning above:

- **Adjacent tokens of the same class are merged.** The Markdown lexer's inline rules end in a catch-all that tokenizes character-by-character, so without merging, an ordinary paragraph would produce roughly one token per character — millions of tokens for a large file. Merging is safe because it only moves token boundaries, never changes the underlying text.
- **Order is content.** Both the JSON and XML parsers read a raw token stream rather than decoding into a generic map, specifically because a generic map would lose the file's original key order. For the same reason, the derived tree view is deliberately not sortable or searchable — sorting would destroy the order the parser went out of its way to preserve, and a text filter would orphan child nodes from parents that no longer match.
- **The tree is a table; the text pane is a viewport**, and that split is deliberate: a tree cell holds exactly one syntax class, so a table-style per-cell color works fine there, whereas the text pane needs multiple colors within a single line and so is rendered as free-form styled text instead.
- **Line wrapping happens on tokens, before coloring is applied**, never on already-colored text. A colored (ANSI-escaped) line can't be safely cut at an arbitrary column, because the wrap logic would count escape-sequence bytes as visible width and could cut in the middle of an escape sequence, corrupting everything rendered afterward.
- **Line numbers reflect the source document, gaps included.** When a filter or verbosity setting hides lines, the visible line numbers still skip the hidden ones rather than renumbering consecutively — that's what makes "line N" a stable, quotable reference. The gutter holding line numbers is deliberately excluded from what search can match, and its width is subtracted from the available column width *before* wrapping is computed, not after (doing it after would push every wrapped line past the right margin by exactly the gutter's width).

## Search: filtering and highlighting are one computation

A single function decides both whether a line survives a search filter and which spans within it get highlighted — never two separate calculations for what's really one question. This guarantees, by construction, that a line which passed the filter always has at least one highlighted span; if filtering and highlighting were computed separately, they could disagree, producing a line that's shown as matching but has nothing visibly highlighted on it. That case-sensitivity is a parameter of this same single calculation, rather than a second flag read independently by the filter — it decides which lines survive *and* which spans light up together, so the two can never drift out of sync.

A few consequences:

- A match is represented as an additional span over already-tokenized (plain) text, applied *after* filtering and only to the lines about to be drawn — never as a color layered on top of an already-finished, styled line.
- In a log line, level coloring and match highlighting are independent and both apply — a highlighted match inside an `ERROR` line doesn't erase the error coloring for the rest of the line.
- Turning off syntax coloring (`c`) has no effect on search highlighting — an occurrence of the search term isn't "syntax," and hiding it when the user asked to see occurrences of it would defeat the point of searching.

## Go-to-line is a modal prompt, not inline

Jumping to a specific line (`g`) opens a small prompt that captures all keyboard input until it's dismissed — similar to how a confirmation dialog behaves — rather than letting keystrokes fall through to the document underneath. This matters because a bare digit needs to mean "part of the line number I'm typing," not "scroll the document," which would be ambiguous if the prompt didn't fully own input while open.

| Input | Result |
|---|---|
| Empty | Nothing happens, silently — treated as a change of mind |
| Past the end of the document | An explicit "document has N lines" message |
| A line currently hidden by an active filter | An explicit "line N is hidden by the filter" message, and the view does not move |
| A visible line | That line scrolls to the top of the pane |

The "hidden by filter" case is called out specifically because the alternative — silently jumping to the nearest *visible* line instead — would report success while landing on a different line than the one requested, which is a worse failure mode than an honest refusal: the user has no way to know the number under the cursor isn't the one they asked for.

## What moved into the viewer

The container logs pane that used to exist as a separate, bespoke implementation was folded into this shared viewer wholesale — its viewport handling, line wrapping, ANSI stripping, scrolling, reload, follow, and timestamp toggle all moved here rather than being reimplemented. One consequence worth knowing if you're debugging platform-specific pager behavior: the external-pager code path used to be broken on Windows because of a quoting mismatch between how Go escapes command-line arguments and how `cmd.exe` interprets them — a path like `%TEMP%\devdesk-logs.txt` was being mangled before `cmd.exe` ever saw it, so the pager silently failed and returned control instantly, with no visible error. That was fixed by removing the temporary file entirely and piping output directly into the pager instead, which sidesteps the quoting problem altogether. A failed pager launch is now reported explicitly rather than failing silently and looking like the user simply closed it right away.
