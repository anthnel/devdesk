# Keybindings

DevDesk's keyboard vocabulary has one rule at its core: **no bare letter is
navigation.** Arrow keys and `Tab`/`Shift+Tab` navigate; a lowercase letter
is always a local filter or toggle, an uppercase letter is always a
resource action, and there are no vim-style `hjkl` aliases.

## Navigation

| Key | Action |
|---|---|
| `↑` / `↓` | Move up/down in a list or table |
| `PageUp` / `PageDown` | Scroll by page |
| `Home` / `End` | Go to top / bottom |
| `Tab` / `Shift+Tab` | Move between breadcrumb tabs |
| `←` | Go back to the parent level (drill up) |
| `→` | Enter the selected group/directory (drill down) |
| `Enter` | Select / confirm (in selection mode) |
| `Esc` | Go back to the parent level, or cancel/close a modal |

## Resource actions — uppercase, same meaning everywhere

| Key | Action | Key | Action |
|---|---|---|---|
| `N` | Create | `E` | Edit |
| `D` | Delete | `M` | Rename (*mv*) |
| `S` | Scan | `A` | Scan all |
| `F` | Refresh (*fetch*) | `C` | Clone selector |
| `T` | Terminal | `O` | Open in IDE |
| `W` | Open in browser | `L` | Logs |
| `V` | Pager | `K` | Stop / kill |
| `P` | Prune | `B` | Registry browser |
| `G` | Pull (*get*) | `U` | Login / logout |
| `X` | Exclude | `R` | MR · PR |
| `I` | Issues | `Y` | Copy path |

Two declared exceptions to "uppercase for actions": `o` (open the resolved
CI pipeline, security/results/CI tab only) and `ctrl+y` (copy the `docker
run` command) — narrow enough that reserving a global uppercase for either
would cost more than it buys.

## Lowercase — local filters and toggles, view by view

Lowercase letters never modify anything; they're local and can repeat
across views. Examples: `r`/`p`/`s`/`t`/`z` in Containers are cumulative
state filters; `c`/`h`/`m`/`l` in Security are cumulative severity filters;
`t`/`u`/`l`/`e`/`n`/`z` in Network Diagnostics filter protocol/state. Each
view declares its own set — see `internal/ui/keymap` for the full,
per-view list.

## Global

| Key | Action |
|---|---|
| `ctrl+p` | Open command mode — from anywhere, including inside a focused field |
| `:` | Open command mode too, except while editing a text field |
| `/` | Search / filter |
| `?` | Open help |
| `q` | Quit view/app |
| `ctrl+r` | Refresh — nothing else |

Only three `Ctrl` combinations exist in the whole app: `ctrl+c` (SIGINT),
`ctrl+r` (refresh), `ctrl+p` (command line). Most other `Ctrl` letters are
already claimed by the terminal, `screen`, or `tmux` (`ctrl+a`, `ctrl+b`,
flow control on `ctrl+s`/`ctrl+q`, and `ctrl+i`/`m`/`j`/`h` are literally
Tab/Enter/LF/Backspace), so a fourth would be reclaiming ground `Shift`
already gives for free.

## Forms

| Key | Role |
|---|---|
| `↑` / `↓` | Move between fields |
| `←` / `→` | Cycle a closed-set field's value |
| `Space` | Toggle a checkbox |
| `Enter` | Confirm the focused button / action |
| `Esc` | Cancel / close |

`Tab`/`Shift+Tab` is reserved for switching tabs and never moves focus
between form fields.

## Document viewer

Opened from another view (a file in Workspaces, a log in Containers);
`Esc` returns to where it was opened from.

| Key | Action |
|---|---|
| `f` | Toggle the document's derived view (tree for JSON/XML, rendered form for Markdown) |
| `c` | Syntax coloring on/off |
| `w` | Soft wrap |
| `v` | Cycle the minimum log level shown (logs only) |
| `/` | Search — filters **and** highlights matches |
| `s` | Case-sensitive search toggle |
| `n` | Line numbers, from the document's own numbering |
| `g` | Go to a line (prompts for a number) |
| `ctrl+r` / `F` | Reload once / follow (tail) |
| `V` | Open in the system pager (container logs only) |
| `Y` | Copy the document's content to the clipboard |
| `O` | Open the file in the configured IDE (files only) |
