package trust

import (
	_ "embed"
	"slices"
)

// dhiKey is Docker Hardened Images' signing key, dhi-2 — the only one the
// keyring marks active. It is compiled in, never fetched: fetching it would
// rest the trust on a network answer that could carry an attacker's key along
// with their image (§3.82).
//
// Cross-checked on 2026-09-25 from three sources that agree —
// registry.scout.docker.com/keyring/dhi/latest.pub, dhi.io/keyring/2.pub, and
// github.com/docker-hardened-images/keyring publickey/dhi-2.pub — SHA-256 of
// the DER form 118ba556dd52f4aec67018efd316c285c783cd3e54cc0f4527605715c643887c.
// dhi-1 is left out: inactive, with no reason given, and it verifies none of the
// images measured.
//
//go:embed keys/dhi-2.pub
var dhiKey []byte

// builtinRules is B: publishers DevDesk knows, each one entered only after a
// real `cosign verify` succeeded (§3.82). A publisher whose identity has not
// been measured does not enter, even a well-known one.
var builtinRules = []Rule{
	// Measured 2026-09-25 on gcr.io/distroless/static-debian12:nonroot.
	{
		Match: "gcr.io/distroless/**", Mode: ModeKeyless,
		Issuer:  "https://accounts.google.com",
		Subject: "keyless@distroless.iam.gserviceaccount.com",
		Source:  SourceBuiltin, Origin: "built-in: distroless",
	},
	// Measured 2026-09-25 on cgr.dev/chainguard/{static,python,node,wolfi-base}:
	// the same identity on all four.
	{
		Match: "cgr.dev/chainguard/**", Mode: ModeKeyless,
		Issuer:  "https://token.actions.githubusercontent.com",
		Subject: "https://github.com/chainguard-images/images/.github/workflows/release.yaml@refs/heads/main",
		Source:  SourceBuiltin, Origin: "built-in: Chainguard",
	},
	// Measured 2026-09-25 on dhi.io/static:20250419. Key mode, with the
	// signature in the OCI referrers — hence --experimental-oci11, always.
	{
		Match: "dhi.io/**", Mode: ModeKey, TLog: true,
		Keys:   []Key{{Ref: "embedded: dhi-2", PEM: dhiKey}},
		Source: SourceBuiltin, Origin: "built-in: Docker Hardened Images",
	},
}

// Builtin returns the built-in rules, in the order they are looked up.
func Builtin() []Rule { return slices.Clone(builtinRules) }
