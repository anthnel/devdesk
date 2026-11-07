# DEVDESK-141 — `app.motion` setting (A-02)

Story: `docs/product/activity-indicator/stories.md` §A-02 (D3, D7, D10).
Builds on DEVDESK-137 (`internal/ui/indicator`) and DEVDESK-139 (one animation
chain, `ensureTick`, `setMotionStatic`, `SetBlinkPhase`), both merged at b3d32b2.
User decision (open question 7, 2026-11-07): the setting **is** editable in
`:cfg`, as a ←→ cycle field (Rule 132), in addition to the YAML.

## What already exists (and is not redone here)

DEVDESK-139 shipped the router half: `App.motionStatic`, `setMotionStatic(on)`
(pins `anim.blinkOn`, hands the view on screen an "on" frame), and
`cadence(running, wantsBlink, static)`, which refuses a blink-only chain under
static motion and keeps the 100 ms chain while jobs run (footer spinners).
Tests already cover the setter (`TestStaticMotionNeverStartsAChainForIndicators`,
`TestStaticMotionPinsThePhaseOn`, `TestStaticMotionHandsTheViewOnScreenAnOnFrame`).
The comment on `motionStatic` says "Nothing sets it yet". The `indicator.Legend`
already ends with "…with app.motion: static in the context file or in :cfg." —
the user decision makes that sentence true; it is not changed.

So A-02 is: the config key + validation, three call sites that feed the router
from the config, the `:cfg` field with its help and footer warning, the docs.

## Questions for the user

**Q1 — a non-scalar value (`motion: [a, b]`, `motion: {x: 1}`).** Criterion 5
says loading never fails on an invalid value. With a plain `string` field, a
*scalar* typo (`motion: flash`, `motion: 3`, `motion: Static`) loads fine and is
handled below; a *list or map* is a YAML type error and makes `LoadContext`
fail with `failed to parse context`, exactly as it does today for
`gauge_glyph`, `container_engine` and every other `app.*` key.

- **A (recommended)**: plain `string`; non-scalar values stay a load error, like
  every other `app.*` key, and the doc states it. Zero new mechanism.
- **B**: a named type `config.Motion` with a tolerant `UnmarshalYAML` (the
  `config.Rate` precedent): a non-scalar logs `WARN [config] app.motion …` and
  decodes to blink. ~25 lines + 1 test; but `:cfg` then cannot show what was
  written (the raw node is gone), and it makes one `app.*` key behave unlike
  its neighbours.

The plan below implements **A**. If B is chosen, only step 1 changes (type +
`UnmarshalYAML` + one test); steps 2–4 are unaffected (the `:cfg` field reference
becomes `(*string)(&c.App.Motion)`).

**Q2 — write `blink` into the file, or leave the key absent?** Every other
closed-set `app.*` key (`gauge_glyph`, `secret_backend`, `container_engine`) is
*written* by `applyDefaults` when empty, so the next save — any `:cfg` keystroke
saves the whole file — puts `motion: blink` in the user's context file. The
consequence matters here because A-06's checkpoint (one week after this story)
has an explicit outcome **"go with `app.motion: static` by default"**: once
files say `blink`, flipping the default no longer reaches them, and a
migration cannot tell "chose blink" from "got blink by default".

- **A**: write `blink` (the convention). Simplest; a later default flip only
  reaches contexts never saved since, unless a migration resets `blink`
  (which would override users who really chose it).
- **B (recommended)**: leave it empty in memory and in the file
  (`yaml:"motion,omitempty"`); empty means "the current default"
  (`defaultMotion`, one constant). `:cfg` shows an empty value as
  `blink (default)` via a small `field.def` display fallback; the first ←→
  writes an explicit value. Cost: ~10 lines in `fields.go` + 2 tests. Keeps
  A-06's third outcome a one-constant change.

The steps below implement **B**; the A variant is noted inline where it differs.

## Hypotheses (low consequence, prudent choice taken)

- Hypothesis: **an invalid value is kept as read**, not rewritten — the
  `scan.base_image_track` precedent (`TestAConfiguredBaseImageTrackIsKept`),
  not `network.proxy_port`'s. It is what lets `:cfg` show and flag the typo
  (a normalised value would leave the view nothing to detect), and a save does
  not silently rewrite the user's file. It is written back unchanged on save
  until the user cycles the field.
- Hypothesis: **matching is exact and case-sensitive** (`static` only), like
  every other closed-set key. `Static` or `"static "` (quoted, with a space)
  is invalid → blinks + WARN. Only `static` turns the motion off, so any typo
  falls on the default (the `image_verification` "only `off` turns it off"
  reasoning, inverted to the safe side here: blinking is the default).
- Hypothesis: **the `:cfg` warning is a derived `Status` at `LevelWarning`,
  shown while the `app` tab is active** (the tab `:cfg` opens on, where the
  field lives), and it replaces that tab's field hint. A 3-second `Warn`
  message would be gone before the user reads it and has no event to fire
  from (`Init` returns nil and the view is cached) — that is the D47 argument
  `components.Status.Level` exists for. Other tabs keep their hints.
- Hypothesis: **label `Motion`, in the Appearance group, right after
  `Gauge glyph`**; values `blink`, `static` (default first).
- Hypothesis: the WARN is logged on every load (startup, context switch, a
  status-view reload after a monitor CRUD). Accepted: same as
  `network.proxy_port`, and those loads are user-driven, never periodic.
- Hypothesis: a hand edit of the YAML while DevDesk runs takes effect at the
  next context switch or restart, and a `:cfg` save in between overwrites it
  (the documented hazard of `agent_usage.prices`). This is criterion 4's
  "comme `app.gauge_glyph`" — no file watching.

## Target design (summary)

```
context YAML ──load──▶ config.applyDefaults ── "" stays "" (= DefaultMotion, Q2-B); invalid → WARN, kept
                              │
                              ▼
                      AppConfig.Motion  ──StaticMotion()──▶ App.motionStatic
                              │                  (newWithSize · context switch · ConfigSavedMsg)
                              │                                 │
                              │                                 ▼
                              │                    cadence(…, static) → no blink chain
                              │                    setMotionStatic → FrameMsg{Blink:true}
                              ▼
                 :cfg "Motion" cycle field ─←→─▶ persist ─▶ ConfigSavedMsg
                 MotionValid() false ─▶ footer Status{Level: Warning} on the app tab
```

One computation per question, read by everyone: `AppConfig.StaticMotion()`
(router) and `AppConfig.MotionValid()` (config log + `:cfg` footer).

## Step 1 — the config key and its validation · S · first

**Files**: `internal/config/config.go`; new `internal/config/motion_test.go`;
`docs/architecture/configuration.md` (same commit).

**Interfaces** (in `config.go`, beside `GaugeGlyph`):

```go
// In AppConfig, after GaugeGlyph:
// Motion decides whether a running row's activity dot blinks: "blink" or
// "static" (a steady ●, no periodic redraw). Empty means DefaultMotion and is
// left empty, so a change of default reaches every context that never chose.
// Per context only: there is deliberately no environment variable (D3).
Motion string `yaml:"motion,omitempty"`

// Values app.motion accepts.
const (
    MotionBlink  = "blink"
    MotionStatic = "static"
    // DefaultMotion is what an absent app.motion means.
    DefaultMotion = MotionBlink
)

// Motions lists the values app.motion names, default first.
func Motions() []string { return []string{MotionBlink, MotionStatic} }

// EffectiveMotion is the motion in force: Motion when it is in the set,
// DefaultMotion when it is empty or unrecognised.
func (a AppConfig) EffectiveMotion() string

// StaticMotion reports whether rows must not blink (EffectiveMotion == static).
// Only "static" turns the motion off; a typo falls on the default.
func (a AppConfig) StaticMotion() bool

// MotionValid reports whether app.motion holds a value of the closed set.
// Empty counts as valid: it means "not set", which is the default.
func (a AppConfig) MotionValid() bool

// warnInvalidMotion logs an unrecognised app.motion, which is kept as read
// (the base_image_track precedent). It writes nothing.
func warnInvalidMotion(a AppConfig)
```

- `applyDefaults` calls `warnInvalidMotion(cfg.App)` next to the `GaugeGlyph`
  default. It does **not** write a default (Q2-B). `Default()` leaves `Motion`
  empty.
- *Variant Q2-A*: tag `yaml:"motion"`, `applyDefaults` writes `MotionBlink`
  into an empty value, `Default()` sets it; `EffectiveMotion` is then just a
  guard for invalid values.
- Log line, exact prefix required by criterion 5:
  `log.Printf("WARN [config] app.motion %q is not %s or %s, blinking", v, MotionBlink, MotionStatic)`.

**Tests** (`motion_test.go`, table-driven where it fits; capture the log with
`log.SetOutput(&buf)` and restore `log.Writer()` in `t.Cleanup` — no `t.Parallel`):

| Input `app.motion` | After `applyDefaults` | `EffectiveMotion()` | `StaticMotion()` | `MotionValid()` | Log |
|---|---|---|---|---|---|
| absent (`""`) | `""` (Q2-A: `"blink"`) | `blink` | false | true | nothing |
| `"blink"` | `"blink"` | `blink` | false | true | nothing |
| `"static"` | `"static"` | `static` | true | true | nothing |
| `"flash"` | `"flash"` (kept) | `blink` | false | false | contains `WARN [config] app.motion "flash"` |
| `"Static"` | `"Static"` (kept) | `blink` | false | false | WARN |

- `applyDefaults` returns `nil` for every row above (criterion 5: load never fails).
- `Default().App.StaticMotion() == false` (criterion 1 for a brand-new install).
- Round trip through a real file: `t.Setenv("HOME", t.TempDir())` and
  `USERPROFILE`, write a context YAML with `app:\n  motion: flash\n`,
  `LoadContext` → no error, `Motion == "flash"`; `SaveContext` then reload →
  still `"flash"` (kept, not rewritten). Same with `motion: 3` (YAML int into a
  string field) → `"3"`, invalid, loads.
- Q2-B: a context loaded without the key and saved has **no** `motion:` line in
  the file (read it back as text).
- Criterion 6 guard, `TestMotionHasNoEnvironmentOverride`: walk the module's
  `.go` files (skip `.worktrees`, `vendor`), fail on any `Getenv(`/`LookupEnv(`
  whose string literal contains `MOTION` (case-insensitive), naming file:line.
  Cheap, and it is the only way the "no env var" criterion is checkable.

**Doc (same commit)**: `configuration.md` — add `motion` to the `App` list
(line 9) and a paragraph after `app.gauge_glyph`: values, default, per context
only (no env var, D3), absent stays absent and means `DefaultMotion` (Q2-B, and
why: the A-06 checkpoint may flip the default), invalid value kept + WARN + blinks, exact-case match,
where it is applied (startup, context switch, every save), editable in `:cfg`,
hand edits while running apply at the next context switch/restart and are
overwritten by a `:cfg` save, and (Q1-A) a list/map value fails the load like
any `app.*` key.

**Done when**: `go test ./internal/config/` passes; `mise run lint` clean.

## Step 2 — feed the router from the config · S · after 1 · parallel with 3

**Files**: `internal/app/app.go`, `internal/app/configuration.go`,
`internal/app/context.go`, `internal/app/animation.go` (helper);
new `internal/app/motion_test.go`; `docs/architecture/app-shell.md` (same commit).

**Interface** (in `animation.go`, next to `setMotionStatic`, which moves there
or stays in `app.go` — coder's choice, one place):

```go
// applyMotion points the animation at the motion a config asks for. It is
// called wherever the router takes a new config — the context switch and
// every save — like theme.SetGaugeGlyph; the constructor sets the field
// directly because it has no Cmd to return.
func (a *App) applyMotion(cfg *config.Config) tea.Cmd {
    return a.setMotionStatic(cfg.App.StaticMotion())
}
```

Call sites:

1. `newWithSize`: `motionStatic: cfg.App.StaticMotion()` in the `App` literal
   (`anim.blinkOn` is already `true`; no Cmd, no I/O).
2. `handleContextSwitchComplete`: after `reinitializeViews()` (so the rebuilt
   view on screen receives the pinned frame), add `a.applyMotion(msg.Config)`
   to the returned `tea.Batch`.
3. `handleConfigSaved`: after `a.createView(a.currentView)`, add
   `a.applyMotion(msg.Config)` to the returned `tea.Batch`. Unconditional, no
   new `ConfigSavedMsg` flag — cheap and idempotent, the `SetGaugeGlyph` comment
   already says why.

Rewrite the two "Nothing sets it yet / until that setting is wired" comments
(`App.motionStatic`, `setMotionStatic`).

Static → blink needs nothing more: `setMotionStatic(false)` returns nil and the
`Update` wrapper's `ensureTick` restarts a 500 ms chain if the view on screen
wants one. Blink → static while a blink-only chain is alive: the chain's next
tick finds no cadence and is not renewed (one trailing tick, ≤ 500 ms, which
sends no `FrameMsg` because `blinkOn` is already pinned). Keep it — A-03's
reviewed behaviour; do not add a second stop path.

**Tests** (`motion_test.go`; reuse `router`, `blinkRouter`, `stubAnimationEvery`,
`tickAt`, `lastFrame`, `testConfig` from the existing test files):

- `newWithSize` with `App.Motion = "static"` → `a.motionStatic == true`; with
  `""`, `"blink"`, `"flash"` → false (criteria 1, 5).
- `Update(configuration.ConfigSavedMsg{Config: static})` → `a.motionStatic`
  true; the returned Cmd carries no `animationTickMsg`
  (`testutil.MsgOf[animationTickMsg]` false). **Caveat for the tester**:
  `handleConfigSaved` drops every view except `:cfg` and rebuilds the current
  one, so a `blinkView` installed before the message is gone after it; install
  it (or reinstall it) *after* the message, then assert `a.ensureTick() == nil`
  and that `a.Update(tea.KeyMsg{Type: tea.KeyDown})` records no interval
  (criterion 3, through the real save path).
- Same with a static config followed by a blink config: `motionStatic` false,
  then with a `blinkView{wants: true}` on screen `ensureTick()` returns a chain
  recorded at `animationBlinkInterval` (criterion 4, no restart).
- `Update(ContextSwitchCompleteMsg{Config: static, ContextName: "b", Secrets: …})`
  → `motionStatic` true; switching back to a blink config → false
  (criterion 4, per-context D3). Pattern: `failures_test.go:221`.
- Static with a run in the registry: chain still at `animationFastInterval`,
  every `FrameMsg.Blink == true` — already `TestStaticMotionPinsThePhaseOn`;
  add one variant reaching static through `ConfigSavedMsg` rather than the setter.
- A live blink chain, then `ConfigSavedMsg{static}`, then `tickAt(a, …)` for
  the live seq → the returned Cmd carries no `animationTickMsg` and
  `a.anim.ticking == false` (the chain dies at its next tick).
- Run under `mise run test-race` for `internal/app` (the setter calls
  `view.Update` synchronously from `Update` — must stay race-free).

**Doc (same commit)**: `app-shell.md` §"One tick, one chain", the
`motionStatic` bullet: who sets it now (constructor, context switch, save, via
`AppConfig.StaticMotion`), and the trailing-tick behaviour on a switch to static.

**Done when**: `go test ./internal/app/ -run 'Motion|Animation|Blink'` and
`go test -race ./internal/app/` pass.

## Step 3 — the `:cfg` field, its footer warning, its help · S · after 1 · parallel with 2

**Files**: `internal/ui/configuration/fields.go`, `model.go` or `view.go`
(status derivation), `view.go` (help); new
`internal/ui/configuration/motion_field_test.go`; existing
`shortcuts_test.go` (one table row).

**Field** (Appearance group, after `Gauge glyph`):

```go
const motionLabel = "Motion" // named once: the footer warning and tests read it

cycle(motionLabel, func(c *config.Config) *string { return &c.App.Motion },
    config.Motions(), "static keeps a running row's dot steady — no blinking, no periodic redraw"),
```

It is an ordinary cycle field: written and persisted on each ←→
(`persist(saved{})` → `ConfigSavedMsg` → step 2 applies it). Not in
`settlesOnBlur` (nothing to settle). Rule 132 rendering comes from the existing
cycle path in `view.go` (which reads `field.Value`); Rule 135 keys from the
existing cycle handling.

**Q2-B only — the empty value's display.** Add to `field` a `def string`
("what an empty value means", cycle only) set by a constructor
`cycleWithDefault(label, ref, options, def, hint)`:

- `Value`: for `kindCycle` with `*str == ""` and `def != ""`, return
  `def + " (default)"` (display only; nothing is written).
- `Cycle`: an empty value starts from `def`'s index (so from `blink (default)`,
  `→` → `static`, `←` → `static`), then writes an explicit value.

The motion field uses `cycleWithDefault(..., config.DefaultMotion, ...)`.
*Variant Q2-A*: plain `cycle(...)`, no `def`.

**Footer** — replace the inline `Status{Text: m.hint(m.current())}` in
`RenderFooter` by a method, so the rule is one place:

```go
// status is the derived footer line (Rule 128): an app.motion the file holds
// outside its set is a standing fact, so it is a warning-level Status on the
// app tab rather than a message that would expire; otherwise the focused
// field's hint.
func (m Model) status() sharedcomponents.Status
```

Text when `!m.config.App.MotionValid()` and the active tab is `app`:
`fmt.Sprintf("%s %q is not %s or %s — rows keep blinking", motionLabel, v, config.MotionBlink, config.MotionStatic)`,
`Level: sharedcomponents.LevelWarning`. Messages (error/warn/info) still take
precedence (Rule 128) — `FooterMessage.View` already does that.

**Help** (`GetHelpContent`, a section beside "Gauge glyph", Rule 114):
title `Motion`; body states: `blink` (default) makes a running row's dot blink
once a second; `static` keeps it a steady `<glyph>` — same glyph and colour,
no periodic redraw; per context; applied as you cycle it; a value the file holds
that is neither keeps blinking and the footer says so. The glyph is
`indicator.Glyph(indicator.Running, true)`, **never a literal** (Rule 125
activity-column prohibitions). Nothing in `GetShortcuts()` changes.

**Tests** (`motion_field_test.go`; follow `model_test.go`'s helpers —
`fieldNamed`, `focusOn`, HOME isolation — since a cycle persists to disk):

- The app tab has a `Motion` field, `kindCycle`, `Options == config.Motions()`,
  placed in the `Appearance` group.
- Focused on Motion with `"blink"`: `→` → config holds `"static"` and the Cmd
  yields a `ConfigSavedMsg` whose `Config.App.Motion == "static"`; `→` again →
  `"blink"` (wraps); `←` from `"blink"` → `"static"`.
- Q2-B: with `Motion == ""` the row reads `blink (default)`; `→` writes
  `"static"`, and a second `→` writes `"blink"` (explicit — no longer shown as
  default). `field.Value` of a cycle field without `def` is unchanged
  (regression: `Theme`, `Gauge glyph`).
- `space` and `enter` on Motion leave the value unchanged (Rule 135); add
  `{"Motion", "←→", []string{"space"}}` to the `shortcuts_test.go` table.
- Invalid `"flash"`: the rendered field row shows `flash`; `RenderFooter`
  contains `Motion "flash" is not blink or static` while the app tab is active
  **whichever field is focused** (focus `Theme`, then `Motion`); on another tab
  (`tab` once) the footer shows that tab's hint, not the warning.
- From `"flash"`, one `→` (or `←`) → the value is in `config.Motions()`
  (it lands on `static` for either arrow: `field.Cycle` falls back to index 0
  then steps — expected, a two-value set) and the warning is gone from the footer.
- A footer message still beats the warning: after a forced
  `m.footer.Error(...)`, `RenderFooter` shows the error text.
- `GetHelpContent()` has a section titled `Motion` whose body contains
  `blink`, `static` and `indicator.Glyph(indicator.Running, true)`.
- Existing `layout_test.go` still passes (the app tab is one row taller; it
  scrolls when short — check at the sizes the layout tests use).

**Done when**: `go test ./internal/ui/configuration/` passes; lint clean.

## Step 4 — user docs and product-doc closure · S · last (same PR)

**Files**:
- `docs-site/docs/reference/configuration.md`: add the `app:` row —
  `motion` | string | `blink` | `blink` \| `static` — whether a running row's
  activity dot blinks; `static` keeps it steady. Per context; an unknown value
  blinks and is logged. (Observation, out of scope: `gauge_glyph` has no row
  there either.)
- `docs/architecture/configuration.md`: the `Configuration view` section, if it
  lists the app-tab fields, gains `Motion` (step 1 already wrote the key
  paragraph; this is only the view part).
- `docs/product/activity-indicator/stories.md` (A-02 criterion 4 and 5,
  question 7) and `ux.md` (§5 reservation on "or in :cfg", open point 1): mark
  question 7 **resolved — editable in `:cfg`** (2026-11-07). The legend text
  itself does not change.

**Done when**: `mise run check` green on the branch.

## Dependencies and parallelism

```
Step 1 (config) ──┬──▶ Step 2 (router, internal/app)            ──┐
                  └──▶ Step 3 (:cfg, internal/ui/configuration) ──┴──▶ Step 4 (docs)
```

Steps 2 and 3 touch disjoint packages and only share step 1's API
(`Motion`, `Motions()`, `StaticMotion()`, `MotionValid()`, the constants):
they can be given to two coders at once. One PR, one commit per step; each
step's architecture doc is in its own commit (criterion 7).

## Targeted test commands

```bash
go test ./internal/config/ -run 'Motion'
go test ./internal/app/ -run 'Motion|Animation|Blink|ConfigSaved|ContextSwitch'
go test -race ./internal/app/            # needs cgo / a C compiler
go test ./internal/ui/configuration/
go test -count=2 ./internal/config/ ./internal/ui/configuration/   # HOME isolation (DEVDESK-36)
mise run lint
```

## Edge cases (consolidated)

| Case | Expected |
|---|---|
| Key absent / new install | blinks; Q2-B: nothing written, `:cfg` shows `blink (default)`; Q2-A: `motion: blink` written on the next save |
| `static` | steady `●` (same glyph and colour as running), no blink chain; jobs still tick at 100 ms for footer spinners with the phase pinned on |
| `flash`, `Static`, `3` | blinks; `WARN [config] app.motion …` per load; kept as read and saved back unchanged; `:cfg` app tab shows a warning-level status |
| list / map value | Q1: A → load error like any `app.*` key (documented); B → WARN + blink |
| Invalid, then ←/→ in `:cfg` | becomes `static` (either arrow), warning disappears, router applies it on the save |
| Blink → static while a blink chain lives | one trailing tick ≤ 500 ms, not renewed, no extra `FrameMsg` |
| Static → blink with a blinking view on screen | `Update` wrapper's `ensureTick` starts a 500 ms chain on that same `Update` |
| Context A static, B blink | switch flips `motionStatic` both ways; nothing global |
| YAML edited by hand while running | applied at next context switch / restart; a `:cfg` save in between overwrites it |
| Environment variable | none exists; guarded by `TestMotionHasNoEnvironmentOverride` |

## Risks and irreversible points

- **The YAML key `app.motion` and its values `blink`/`static` are the durable
  commitment** (story hypothesis): once in users' context files, renaming
  either needs a migration like `gitlab:` → `forge:`. The plan uses the story's
  names.
- **Q2 is the other irreversible point.** With Q2-A, every context file gains
  `motion: blink` on its next save, and from then on "chose blink" and "got the
  default" are indistinguishable — A-06's "static by default" outcome could no
  longer reach those users without overriding real choices. With Q2-B the flip
  is `DefaultMotion = MotionStatic` plus docs, and only explicit choices stay.
- Low risk on the router: the mechanism is A-03's and already tested; the only
  new paths are three call sites. `-race` on `internal/app` is the check.
