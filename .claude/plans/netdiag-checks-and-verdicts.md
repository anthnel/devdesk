# Netdiag answers questions, not tools

The Diagnostics tab runs seven tools and shows seven rows named after them. The
objective it exists for is narrower and better posed: **is this host reachable,
and is its certificate chain sound.** This plan turns the tool runs into
**checks with verdicts**, derives them from the target instead of asking the
user to tick boxes, and moves everything but traceroute out of Docker.

Four PRs. The fourth — whether the explanation layer is consumed by an MCP
client or by an in-app LLM (§3.10) — is deliberately left open; the first three
are identical either way.

## The problem, in four measured parts

**1. `--network host` is not this host.** On Docker Desktop the runner lands in
the `docker-desktop` VM's network namespace, not the machine's. Measured on the
development box:

```
$ docker run --rm --network host nicolaka/netshoot sh -c 'hostname; ip -4 addr'
docker-desktop
    inet 10.254.254.3/24 ... eth0
    inet 172.17.0.1/16 ... docker0

$ ... cat /etc/resolv.conf   ->  nameserver 10.254.254.7
$ Get-DnsClientServerAddress ->  ProtonVPN {10.2.0.1} · Ethernet 2 {192.168.1.2}
```

The host resolves through a VPN the container has never heard of. A host
reachable only over that VPN reads as unreachable, and a name that resolves
differently inside the corporate network resolves wrong. **The diagnostic
answers for a network the user is not on**, and says nothing about it.

The same defect is visible in the code without running anything:
`defaultDNSServer()` (`validation.go:78`) reads `/etc/resolv.conf`, which does
not exist on Windows, so the DNS Server field is empty by default on the
platform this is developed on.

**2. The chain is never verified.** `sslCertScript` (`docker/netdiag.go:88`)
pipes `s_client -showcerts` into `openssl x509 -noout -text`, and `x509` reads
only the **first** PEM block — the leaf. The intermediates are fetched and
thrown away, `Verify return code` goes to `2>/dev/null`, and there is no
hostname match, no relative expiry, no negotiated version. On the one question
the view exists to answer, the current answer is *we did not look*, rendered
green.

That is D20's shape, and the repository has already paid for it once:
`Result.SecretVerdict()` returns a `*bool` precisely because "nobody looked"
displayed as a clean result is a defect, not a nuance.

**3. `Status` is the process exit code, so the column is not comparable.**
`nc` exiting 1 means "port closed" — an answer. `curl` exiting 1 means a dozen
things. The two render as the same red `FAIL`.

**4. `RunCurl` keys the scheme on `port == "443"`.** `:8443` is probed in the
clear, fails, and reads as "the host is down".

### And it is a duplicate

`internal/status` already answers three of these questions natively:

| | `internal/status` | `internal/ui/netdiag` |
|---|---|---|
| ICMP | `pro-bing` (`icmp_checker.go`) | `docker run … ping` |
| DNS | `net.Resolver` with a chosen nameserver (`dns_checker.go:42`) | `docker run … dig` + a parser |
| HTTP | `net/http`, redirects controlled (`http_checker.go`) | `docker run … curl -I` |

Two implementations of three questions, answering from two different network
stacks. This is the shape the backlog tracks elsewhere (text-filter matching at
five sites, column arithmetic at twelve) — with the aggravating factor that here
the two do not agree.

## The shape: a check, not a tool run

A row is **a question that got an answer**, not a binary that ran. The relation
is one-to-many in both directions, which is exactly why the tool cannot be the
unit:

| One tool, several checks | Several tools, one check |
|---|---|
| TLS → chain complete · hostname match · expiry · version · self-signed | reachability = ICMP **or** a completed TCP connect |

### Five verdicts

| Verdict | Meaning |
|---|---|
| `OK` | the objective is met |
| `Warn` | met, but something is off (cert in 9 days, TLS 1.0, chain completed via AIA) |
| `Fail` | the objective is not met |
| `NotApplicable` | no meaning here, or blocked upstream |
| `Unknown` | **we could not look** |

`Unknown` is not a courtesy: it is the value the SSL check should have been
returning all along. `Warn` and `Fail` are separated by whether the objective is
met, never by how alarming the finding feels — the same rule the footer levels
use (Rule 128).

### One rule decides

`scan.Categorize` exists because two rules answered "which family is this
finding" and disagreed; `SecretVerdict` exists because two loops answered "does
this carry a secret". Both symptoms were identical: something counted in one
place and absent from another.

So: a stage sets the verdict of the checks it produces, and **one** function
folds them into the headline (`Summarize`). The `NotApplicable` cascade is
applied by the pipeline, never by a stage — a stage that decided it was
irrelevant would be the second rule.

## The pipeline: layers, not a flat batch

```
resolve  →  reach  →  connect (tcp)  →  tls  →  http
```

A stage whose dependency failed does not run: its checks come back
`NotApplicable` carrying `Because: <the check that blocked it>`. That is what
turns seven red rows into **one red row and a named point of rupture** — the
whole readability argument, and what makes the checkboxes unnecessary rather
than merely unfashionable.

**Traceroute leaves the default path.** It is the only expensive stage
(`-m 30 -w 1`, up to 30 s against sub-second neighbours), and cost was the only
honest reason a checkbox ever existed. It becomes a key on the results screen,
surfaced by the check that failed (Rule 130), never automatic: 30 seconds
nobody asked for is D13's shape.

## Where the core lives

`internal/netcheck`, a new package. `internal/status` is **left alone** in these
four PRs.

The two notions differ: `status` polls declared components on a timer and
reports up/down; `netcheck` answers a one-shot question about one target and
explains it. Folding them now would grow the diff without buying anything. But
the duplication above is real, so it is recorded here as the explicit
follow-up: **`status` migrates onto `netcheck` once `netcheck` is settled.** Not
writing that down is how the duplicate re-installs itself.

### The seam

`internal/docker`'s `runner` indirection is the precedent: the network
primitives sit behind a small interface so tests need neither a network nor
Docker.

```go
package netcheck

type Verdict int // OK, Warn, Fail, NotApplicable, Unknown

type Check struct {
    ID      CheckID
    Title   string
    Verdict Verdict
    Summary string   // one line — the Evidence column
    Facts   []Fact   // observed key/value pairs
    Because CheckID  // set only when NotApplicable: what blocked it
    Raw     string   // tool output, when a tool was involved
}

type Target struct {
    Host     string
    Port     int
    Resolver string // "" = the system resolver
}

// stages are a declared table, like configuration/fields.go and keymap
type stage struct {
    id        StageID
    dependsOn StageID // "" for the first
    run       func(context.Context, Target, Env) []Check
}

func Run(ctx context.Context, t Target, env Env) []Check
func Summarize([]Check) Verdict
```

`Env` carries the injected primitives (resolver, dialer, TLS handshake, HTTP
client, pinger). A fake `Env` is what makes the whole package testable offline.

### What stays in Docker

Traceroute and TCP traceroute, and nothing else — they need raw sockets and a
tool worth not reimplementing. `validateTarget` therefore stays needed: those
two still take the target as argv into a container.

Six of the eight runners in `internal/docker/netdiag.go` lose their only caller
and go, `sslCertScript` among them — which retires the shell-injection surface
`validation_test.go:33` was written to guard, rather than continuing to guard
it.

## The four PRs

Each is mergeable alone. They land in order; `docs/backlog.md` conflicts when
branches share a base, so they are not cut in parallel.

### PR 1 — `internal/netcheck`

The package: types, the stage table, native DNS / ICMP / TCP / TLS / HTTP, the
verdict rules, the `NotApplicable` cascade, `Summarize`. Table-driven tests
against a fake `Env`, no network, no Docker.

**No UI change. netdiag is untouched and still works.**

Done when: the chain is genuinely verified (each intermediate, hostname match,
expiry in days, negotiated version, self-signed vs incomplete told apart); the
resolver honours a chosen nameserver on every platform; `go test` passes with no
network; package coverage ≥ 80 %.

### PR 2 — the deterministic explanation

Per `CheckID`, three fields: **observed / what it means / what to do**, filled
from typed data. Table-driven, English US (Rule 129), offline, no model.

This is what was actually asked for, and it is where most of the value is.
§3.10 concedes the point about its own later stages: once the deterministic
summariser exists it answers most of the question without a model.

### PR 3 — the view

- The form drops to three fields (Target, Port, DNS). `enter` runs. `testDef`,
  the seven checkboxes, `fieldPing..fieldSSL` and most of `run.go` go.
- Results: one `datatable` of checks. `FilterBar` with lowercase toggle tokens
  for the verdicts (Rule 111, Rule 136) — the component netdiag/Ports already
  uses.
- The headline verdict goes in `GetHeaderInfo`, beside Context — the way
  security carries `Findings`.
- `enter` opens the detail: the explanation plus the raw output; `f` keeps its
  meaning (Rule 111).
- Traceroute on a key, shown only when a check that justifies it failed
  (Rule 130).
- `GetShortcuts` and `GetHelpContent` updated in the same commit (Rule 114).
- Deleted with their last caller: `dns_formatter.go` and its test (DNS is
  native, there is no `dig` output left to parse), and the six Docker runners
  above. `traceroute_formatter.go` stays — traceroute stays.

Done when: netdiag package coverage does not fall below its current **84.8 %**
(the project floor is 80 %); `mise run check` is clean; the results table holds
Rule 116 at 46 columns, as §3.21 established for this table.

### PR 4 — open

The explanation reaches a model either through an **MCP server** DevDesk exposes
(`dk mcp`, stdio, read-only) or through the **in-app client** of §3.10.
Recommendation on record: MCP, with §3.10 decision 4 *scoped* rather than
reversed — it binds DevDesk as a sender, and an MCP server sends nothing;
pseudonymisation is structurally unavailable there for want of a display step to
restore in. Both remain compatible. **Nothing in PRs 1–3 depends on the answer.**

## Settled, so it is not re-litigated

| # | Question | Decision |
|---|---|---|
| 1 | Scope | **Rewrite.** The tool-picking mode goes; two paths to one question is what the theme picker and the secret backend each had to lose. |
| 2 | Core location | **New `internal/netcheck`.** `status` untouched here, its migration recorded as the follow-up. |
| 3 | Model integration | **Open — PR 4.** |
| 4 | Traceroute | **On a key**, suggested by the failing check. Never automatic. |
| 5 | Config keys | **None in v1.** Named constants: timeouts, cert-expiry warn threshold, hop limit. Every scalar added is a row in `configuration/fields.go` and its tests; it is added when someone asks. |
| 6 | Shipping | **Four sequential PRs**, one worktree each, cut from an updated `main`. |

## Deferred, explicitly

Check sets derived from the port (a Postgres and a web server do not pose the
same questions — and §3.8 already settled this class of choice the other way:
declared, never sniffed); Presidio; the shortcut letter for the explanation
(only `H J Q Y Z` are free and none of them means *explain*); the `status`
migration; tcpdump capture.
