# Plan: the `docker` tab becomes `network`, and netdiag's constants become settings

**Branch**: `config-header-paths` (same PR as D48)
**Complexity**: Medium — one config section renamed with a migration, one package
threaded with a settings struct, five new rows in the configuration view.

## Summary

The `docker` tab holds exactly one setting, `network_tool_image`, and it is not
a Docker setting: it names the container the **route traces** and the **ports
table** run in. Renaming the tab to `network` is right, and it is right for the
YAML key too — the four other tabs are named after the config section they
write, and breaking that correspondence in the one tab about to grow would be
the worse half of the change.

What it grows with is the survey that was asked for: `internal/netcheck/env.go`
says in as many words that its timeouts *"become settings when someone asks for
them, not before"*. Someone has asked.

## What is hardcoded today, and what it is worth

| Value | Where | Setting? |
|---|---|---|
| `resolveTimeout` 5 s, `dialTimeout` 5 s, `tlsTimeout` 8 s, `httpTimeout` 8 s | `netcheck/env.go` | **one setting**, see below |
| `pingTimeout` 4 s | `netcheck/env.go` | folded into the same one |
| `pingCount` 3 | `netcheck/env.go`, read by `stage_reach.go` | **yes** |
| `expiryWarnWindow` 30 days | `netcheck/env.go`, read by `stage_tls.go` | **yes** |
| traceroute `-m 30` (max hops) | `docker/netdiag.go` | **yes** |
| traceroute `-w 1` (wait per hop) | `docker/netdiag.go` | no — see *Left alone* |
| ports refresh `2 * time.Second` | `netdiag/ports_model.go` | **yes** |
| `network_tool_image` | already a setting | moves tab |

### The timeouts are one setting, not five

Five rows for one idea, and the 5 / 5 / 8 / 8 / 4 split is not argued anywhere
in the code — it reads as five separate guesses rather than as a design. One
`network.check_timeout` covers what a user actually tunes: *how long a probe
waits for an answer*.

**Default 8 s**, the current maximum, so nothing that answers today starts
failing. The cost is stated rather than discovered: an unreachable host now
spends 8 s on DNS instead of 5, and 8 s on the ping instead of 4. The pipeline
is staged and each stage announces itself (`StageTitle`), so the wait is
legible — which is exactly what §3.16 bought, and what makes the slower worst
case acceptable. A user on a slow link raises it; a user who wants a fast
verdict lowers it, which they cannot do at all today.

### Left alone, deliberately

- **Traceroute wait-per-hop (`-w 1`)** — it multiplies with the hop count, so
  two settings would let a user build a 900-second trace out of two numbers that
  each look reasonable. Max hops is the one worth exposing.
- **TLS minimum version (`VersionTLS10`)** — the handshake is *probing*, not
  securing: reporting an old version is the whole point, and a setting could
  only make the tool blind to what it exists to find.
- **`InsecureSkipVerify`** — the tls stage verifies itself so it can name
  *which* part of the chain failed. A setting here would delete four checks.

## Files to change

| File | Action | Why |
|---|---|---|
| `internal/config/config.go` | UPDATE | `DockerConfig` becomes `NetworkConfig` (`yaml:"network"`), new scalars, defaults, migration from `docker:` |
| `internal/config/config_test.go` | UPDATE | pin the migration |
| `internal/netcheck/env.go` | UPDATE | `Settings` struct; `systemEnv` becomes a struct holding it; `SystemEnv(Settings)` |
| `internal/netcheck/run.go` | UPDATE | thread `Settings` through `Run`, `RunStep`, `stage.run` |
| `internal/netcheck/stage_reach.go` | UPDATE | read `PingCount` from settings |
| `internal/netcheck/stage_tls.go` | UPDATE | read `ExpiryWarnWindow` from settings |
| `internal/netcheck/*_test.go` | UPDATE | about ten call sites gain `DefaultSettings()` |
| `internal/docker/netdiag.go` | UPDATE | `RunTraceroute` / `RunTCPTraceroute` take max hops |
| `internal/ui/netdiag/run.go`, `update.go`, `model.go`, `ports_model.go` | UPDATE | pass settings from `cfg.Network` |
| `internal/ui/dashboard/model.go`, `internal/ui/oci_resources/keys.go` | UPDATE | `cfg.Docker.NetworkToolImage` becomes `cfg.Network.ToolImage` |
| `internal/ui/configuration/fields.go` | UPDATE | the `network` tab and its three groups |
| `internal/ui/configuration/*_test.go` | UPDATE | the tab is named in a test |
| `.claude/CLAUDE.md`, `docs/backlog.md` | UPDATE | record it |

## Tasks

### Task 1 — `NetworkConfig`, with a migration

```go
// NetworkConfig holds what the netdiag view uses. It was `docker:` and held one
// image, which was never a Docker setting: it names the container the route
// traces and the ports table run in.
type NetworkConfig struct {
	ToolImage string `yaml:"tool_image"`

	CheckTimeout         int `yaml:"check_timeout"`          // seconds, 1..120, default 8
	PingCount            int `yaml:"ping_count"`             //          1..20,  default 3
	CertExpiryWarnDays   int `yaml:"cert_expiry_warn_days"`  //          1..365, default 30
	TracerouteMaxHops    int `yaml:"traceroute_max_hops"`    //          1..64,  default 30
	PortsRefreshInterval int `yaml:"ports_refresh_interval"` // seconds, 1..60,  default 2
}

// DockerConfig is what `network:` replaced. Read once at load, migrated,
// cleared — so it disappears from the file on the next save. The precedent is
// RegistryItem.AuthEnabled.
//
// Deprecated: use Network.
type DockerConfig struct {
	NetworkToolImage string `yaml:"network_tool_image,omitempty"`
}
```

- **Mirror**: `RegistryItem.AuthEnabled` (`internal/config/config.go:110`) — read
  at load, migrated into the new field, cleared, gone on the next save.
- **Migration order matters**: migrate `docker.network_tool_image` into
  `network.tool_image` **before** the `if ToolImage == ""` default, or an
  existing config silently reverts to `nicolaka/netshoot`. Unmarshal is not
  strict (`internal/config/config.go:520`), so an unmigrated key fails
  *silently* — which is why this needs a test rather than a comment.
- **Validate**: `TestANetworkToolImageSurvivesTheRename`.

### Task 2 — `netcheck.Settings`

```go
// Settings are the user's dials. They are separate from Env because Env is the
// seam to the network and these are policy: the ping count and the expiry
// window are read by stages, not by the network calls.
type Settings struct {
	CheckTimeout     time.Duration
	PingCount        int
	ExpiryWarnWindow time.Duration
}

func DefaultSettings() Settings // the values the constants hold today
func SystemEnv(s Settings) Env  // was SystemEnv()
func Run(ctx, t, env, s) (Results, error)
func RunStep(ctx, t, env, s, id, prior) Results
```

- `stage.run` gains the parameter; `stage_reach` reads `s.PingCount`,
  `stage_tls` reads `s.ExpiryWarnWindow`.
- **Why not on `Env`**: `TrustRoots()` and `Now()` are environment *facts*; a
  ping count is a preference. Putting a preference behind the network seam would
  make every fake responsible for answering for it.
- **The zero value must not mean "no timeout".** `normalize()` fills any
  non-positive field from `DefaultSettings()`, so a caller that forgets one gets
  today's behaviour rather than an infinite dial.
- **Validate**: `TestZeroSettingsFallBackToTheDefaults`,
  `TestThePingCountComesFromSettings`, `TestTheExpiryWindowComesFromSettings`.

### Task 3 — traceroute max hops

`RunTraceroute(image, target string)` becomes
`RunTraceroute(image, target string, maxHops int)`; same for `RunTCPTraceroute`.
`-w 1` stays a constant, for the reason above.

### Task 4 — the ports tick

`newPortsModel(image string)` becomes
`newPortsModel(image string, refresh time.Duration)`; `portsTickCmd` takes the
interval. Guard: a non-positive interval falls back to two seconds, so a
hand-edited `0` does not spin the CPU.

### Task 5 — the `network` tab

```
network
  󰛳 Tools
    Network tool image      Must carry ping, curl, nc, traceroute and ss

  󰔟 Checks
    Check timeout (s)       1..120  How long one probe waits for an answer
    Ping count              1..20   ICMP echo requests per run
    Ports refresh (s)       1..60   How often the Ports tab re-reads ss

   Certificates
    Expiry warning (days)   1..365  A certificate closer than this warns
```

Icons: `theme.IconNetwork`, `theme.IconHourglass`, `theme.IconCertificate`.

- **Mirror**: the `scan` tab's `group()` runs
  (`internal/ui/configuration/fields.go:239`) and the `status` tab's
  `integer(...)` bounds (`internal/ui/configuration/fields.go:288`).
- Tab order stays `app, gitlab, scan, network, status` — `network` keeps
  `docker`'s slot, so nobody's muscle memory moves.

### Task 6 — record it

`.claude/CLAUDE.md`: the configuration-view section gains the tab, and the
netdiag section gains "what is configurable and what is not, and why".
`docs/backlog.md`: a §3 entry — this is a feature, not a defect. The D48 entry
already written stays where it is.

## Validation

```bash
go build ./... && go test ./... && gofmt -l internal/ && golangci-lint run
go test ./internal/config/ -run Network -v
go test ./internal/netcheck/ -v
go test ./internal/ui/configuration/ -v
```

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| An existing `docker:` block is silently dropped — unmarshal is not strict | **High** without the migration | Task 1's migration plus `TestANetworkToolImageSurvivesTheRename` |
| Raising DNS and ping to 8 s makes an unreachable host slower to fail | Certain, by design | Stated above; the staged progress line names what is being waited on |
| `Settings` threading touches about ten test call sites | Certain | Mechanical; `DefaultSettings()` at each |
| A second parameter beside `Env` invites a third | Low | `Settings` is closed: three fields, all user-facing |

## Acceptance

- [ ] `:cfg` shows a `network` tab where `docker` was, with three groups of rows
- [ ] An existing `~/.devdesk/config.yaml` carrying `docker.network_tool_image` keeps its image, and the key is gone after the next save
- [ ] Lowering `check_timeout` visibly shortens a run against an unreachable host
- [ ] `ping_count` changes the Sent / Received facts on the ICMP check
- [ ] Full suite, `gofmt` and `golangci-lint` clean
