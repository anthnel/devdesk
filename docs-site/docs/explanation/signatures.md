# Image Signatures

A CVE scan tells you what is known to be wrong with an image. It cannot tell
you whether the image is the one its publisher released: a tag republished
with altered content shows nothing catalogued, so it scans clean. DevDesk
answers that second question with [cosign](https://github.com/sigstore/cosign)
signatures, checked before every pull and shown beside every image.

This page explains the model. To write your own rules, see
[Trust image signatures](../how-to/trust-image-signatures.md).

## Three sources of trust

Whom to expect a signature from is decided by the first rule that matches the
image's repository. Rules come from three places, and they are asked in this
order:

| Source | Where it lives | What it covers |
|---|---|---|
| **Yours** | `~/.devdesk/trust.yaml` | whatever you declare — a corporate registry, a vendor you rely on |
| **Built-in** | compiled into DevDesk | distroless (`gcr.io/distroless/**`), Chainguard (`cgr.dev/chainguard/**`), Docker Hardened Images (`dhi.io/**`) |
| **Continuity** | nothing to configure | an image with no rule, whose version in use *is* signed: a new version is expected to be signed by the same identity |

A built-in publisher is added only after a real `cosign verify` against its
images succeeded — a well-known name whose identity was not measured does not
enter. The Docker Hardened Images key is compiled in rather than fetched:
fetching it would let a network answer carry an attacker's key along with
their image.

Continuity never believes an identity it reads. The identities on the image
in use are *claims*; each is verified on that image before being asked of the
new one. Otherwise a forged certificate sitting next to an attacker's own valid
signature would read as Verified.

## Verdicts, and what they do

A check ends in one of five verdicts:

| Verdict | Meaning |
|---|---|
| Verified | the expected identity, or key, signed this digest |
| Identity mismatch | a valid signature — by someone else. The one verdict that says the content was replaced |
| Unsigned | no signature, or none the expected key verifies. Under a rule, this is what a republished tag looks like |
| Failed | the question could not be asked: registry, Sigstore or cosign unreachable, a timeout |
| No policy | nothing to ask: no rule, and nothing to continue from |

What a verdict *does* depends on who asked for the guarantee:

| Verdict | Your rule | Built-in | Continuity |
|---|---|---|---|
| Identity mismatch | block | block | block |
| Unsigned | block | block | warn |
| Failed | block | warn | warn |

A rule you wrote asks for a guarantee, so not being able to check blocks. A
built-in rule is DevDesk's declaration, not yours: a proxy that filters
Sigstore must not stop every Chainguard pull for someone who configured
nothing. Continuity declares nothing at all, and publishers do stop signing.

A key-mode rule never reaches Failed — cosign reports a wrong key and an
unreachable registry with the same exit code, so key mode fails closed and
reads both as Unsigned. A built-in key rule (Docker Hardened Images) therefore
blocks where a keyless one would warn.

## Where it applies

**Every pull.** `G` in the registry browser, `G` on an update, and an agent's
`image_pull_start` all go through one path, and a test fails the build if
anything else calls the engine's pull. The sequence:

1. The tag is resolved to a digest.
2. That digest is checked.
3. DevDesk pulls `repo@digest`, then tags it back to the name you asked for.

Pulling the tag instead would let it move between the check and the pull —
which is precisely the attack. A refusal is a footer error naming the rule; a
warning lets the pull go ahead and says why afterwards.

**Nothing runs unpulled.** Launching a container passes `--pull=never`, so an
image reaches the machine through a verified pull or not at all. DevDesk's own
tool images — Trivy, cosign, the network diagnostics image — are pulled by
their own `run` and are a declared exception.

**The `Sig` column.** The OCI view's Images tab and a scan's Remediation tab
show each image's verdict as a glyph:

| Cell | Meaning |
|---|---|
| green check | a proven signature |
| red cross | refused — an unexpected identity, or no signature where a rule asks for one |
| orange warning | unsigned where nothing required it |
| `?` | the check could not run |
| hourglass | still checking |
| hammer | built or loaded here: there is no published signature to ask about |
| `-` | no rule to check against |

Green is deliberate. The Update column's "up to date" check mark stays grey,
because it is the ordinary resting state. A Verified signature is a rare,
proven guarantee whose failure is shown in red, so it gets a colour of its own.

Checks run in the background, four at a time, and are cached in
`~/.devdesk/cache/signature-verdicts.json`. A Failed verdict is never cached,
so that it cannot outlast the network coming back.

**Remediation.** A candidate base image is checked against the rules *and* by
continuity with the base it would replace. A blocked candidate stays listed —
a tag republished by someone else is worth seeing — but `space` will not choose
it and names the rule. A warning is repeated in the `ctrl+o` confirmation.

**Scans.** With the Misconfiguration category on, a repository scan checks
every `FROM` of every Dockerfile, build stages included. Only a proven
violation becomes a finding, anchored on its `FROM` line:

| ID | Verdict | Severity |
|---|---|---|
| `DEVDESK-SIG-001` | identity mismatch | CRITICAL |
| `DEVDESK-SIG-002` | unsigned under a rule | HIGH |

## Turning it off

`scan.image_verification` is set per context, `on` by default. Only the value
`off` disables it, so a typo still verifies. When it is off, verdicts, pulls and
scan findings all stop, and this overrides your own rules. It is visible, not
silent: the Images tab header says `Signatures: off`. The switch is per context
because whether to check depends on the environment — a context isolated from
the network, for example. Whom to trust is a fact about the world, so the rules
themselves are global.

A `trust.yaml` that does not load is never treated as empty. Falling back to
the built-in rules would drop yours without a word, so every pull is refused
with the error until the file is fixed, and the header says
`trust.yaml invalid`.
