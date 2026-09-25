# Trust image signatures

DevDesk checks an image's [cosign](https://github.com/sigstore/cosign)
signature before pulling it. Distroless, Chainguard and Docker Hardened Images
are covered out of the box. For any other publisher, declare who is expected to
sign in `~/.devdesk/trust.yaml`. For the model behind this, see
[Image signatures](../explanation/signatures.md).

## Write the policy file

`~/.devdesk/trust.yaml` is global, shared by every context, and edited by hand.
The configuration view does not touch it.

```yaml
version: 1
rules:
  # Your own images, signed with a key
  - match: registry.corp.example/base/*
    key: ~/.devdesk/keys/corp-cosign.pub     # or awskms://…, gcpkms://…
    tlog: false                              # no transparency log entry expected

  # A vendor signing from CI, keyless
  - match: ghcr.io/acme/**
    keyless:
      issuer: https://token.actions.githubusercontent.com
      subject_regexp: ^https://github\.com/acme/.+/\.github/workflows/release\.ya?ml@refs/tags/v.+$

  # Images nobody signs, on purpose: don't warn
  - match: registry.corp.example/legacy/*
    expect: none
```

Each rule has a `match` and **exactly one** mode:

| Mode | Fields | Meaning |
|---|---|---|
| `key` | a path or a KMS URI; `tlog` (default `true`) | the image must be signed by this key |
| `keyless` | `issuer`, and one of `subject` or `subject_regexp` | the Sigstore certificate must carry this identity |
| `expect: none` | — | no signature is expected; nothing is checked or reported |

A key path may start with `~/`. A relative path is read from the directory
that holds `trust.yaml`, and the file must be a PEM public key. A value
containing `://` is passed to cosign as a KMS reference.

## How `match` is read

- It matches the **normalized repository**, without a tag or digest:
  `python` is `docker.io/library/python`.
- `*` matches one path segment; a trailing `/**` matches any depth.
- **The first matching rule wins**, and your rules are read before the
  built-in ones. A rule that an earlier one hides is logged as never used.

## The file is strict

Unlike `config.yaml`, `trust.yaml` rejects anything it does not understand:
an unknown key, a missing `version: 1`, a rule with two modes, or a
`notation:` rule (not supported yet). If there is any error, the **whole file**
is rejected. This is deliberate: a typo such as `isuer:` would otherwise drop the
issuer from a rule without a word.

A rejected file does not fall back to the built-in rules. Every pull is
refused with the error, which names the line, until the file is fixed, and the
Images tab header says `trust.yaml invalid`.

## Check that it works

Open the OCI view (`:oci`). The Images tab's **Sig** column shows each local
image's verdict against the rules. A green check means your rule verified.
A red cross means refused, and `G` on that image will be refused with the rule
named in the footer.

A rule you write *blocks* when the check cannot run at all — an unreachable
registry or Sigstore. That is the difference from a built-in rule, which only
warns.

## Turn verification off for one context

In the configuration view (`:config`), untick **Verify image signatures** on
the `scan` tab, or set it in the context file:

```yaml
scan:
  image_verification: off   # on (default) | off
```

This turns off everything, including your own rules: verdicts, the pull check
and scan findings. The Images tab header then says `Signatures: off`.
