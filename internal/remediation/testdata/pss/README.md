# Pod Security fixture (DEVDESK-686)

`trivy-0.76.2.json` is a real Trivy output, reduced, recorded on the manifests in
`manifests/`. It is the reference the Pod Security table in `internal/remediation/pss.go`
is checked against (`TestThePodSecurityTableMatchesTrivy`), and the input of the
seccomp-origin integration test in `internal/scan`.

- Trivy: 0.76.2
- Checks bundle: `sha256:b591e9190cff4aa4e385b1a25bccfd9ecf178d3c77fd194e1410b7ce7e1df058`
  (downloaded 2026-11-22; the bundle updates independently of the Trivy version)
- `pod-all.yaml` covers the 13 baseline rules, KSV-0001/0012/0030/0105/0106/0121 and the two
  PSS-linked rules without an anchor (KSV-0117, KSV-0119); `pod-volumes.yaml` adds KSV-0028.
- `seccomp-*.yaml` cover KSV-0104: Unconfined at the pod, Unconfined at the container, absent,
  and a Deployment with three containers (RuntimeDefault, Unconfined, absent).

Re-record (from this directory, Trivy installed, checks bundle already downloaded):

    trivy config --format json --quiet manifests | jq '{Results: [.Results[] | {Target, Type, Misconfigurations: [.Misconfigurations[]? | select(.Status=="FAIL") | {ID, Title, Message, Status, PrimaryURL, References, CauseMetadata: {StartLine: .CauseMetadata.StartLine, EndLine: .CauseMetadata.EndLine}}]}]}' > trivy-0.76.2.json

Targets are relative to `manifests/`. The file holds no absolute path.
