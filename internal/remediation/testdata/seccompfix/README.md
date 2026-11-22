# Seccomp fix fixture (DEVDESK-688)

Real Trivy outputs, reduced, recorded on the manifests of `before/` and of `after/`. `before/`
holds workloads with no seccomp profile anywhere (KSV-0030 and KSV-0104 fire on every bare
container, init containers included); `after/` holds the same files as the seccomp fix must write
them: `securityContext.seccompProfile.type: RuntimeDefault` on the pod spec of the first
workload, by inserted lines only. The tests read the recorded findings (their real lines and
messages), apply the fix to each and compare the bytes with `after/`
(`k8s_seccomp_trivy_test.go`); the lines and messages are the recorded ones, not written by hand.
`testdata/pss/` is a different fixture and is not touched.

- Trivy: 0.76.2
- Checks bundle: `sha256:b591e9190cff4aa4e385b1a25bccfd9ecf178d3c77fd194e1410b7ce7e1df058`
  (the one of `testdata/pss/README.md`, downloaded 2026-11-22; the bundle updates independently of
  the Trivy version, and the message formats and the annotation behavior come from it)
- `after/*.yaml` were written by hand from the expected edit, then confirmed by Trivy: the
  recorded `after` output has no KSV-0030 and no KSV-0104 left except the second Pod of
  `two-pods.yaml`, which the fix must not touch.
- `deployment-three.yaml`: three containers, one finding each (a fix from any of them is the
  same). `pod-init.yaml`: an init container is signalled too. `cronjob-four-spaces.yaml`: a file
  indented by four spaces. `pod-crlf.yaml`: CRLF line endings, kept by `.gitattributes` here (the
  repository normalizes text to LF). `statefulset-context.yaml`: a `securityContext` the pod
  already has, which only gains the key. `two-pods.yaml`: two documents, the fix of the first
  leaves the second reported.
- What Trivy does not read, so what no recorded output can confirm: `ephemeralContainers`.

Re-record (from this directory, Trivy installed, checks bundle already downloaded):

    for d in before after; do (cd $d && trivy config --format json --quiet --skip-check-update . | jq '{Results: [.Results[] | {Target, Type, Misconfigurations: [.Misconfigurations[]? | select(.Status=="FAIL") | {ID, Title, Message, Status, PrimaryURL, References, CauseMetadata: {StartLine: .CauseMetadata.StartLine, EndLine: .CauseMetadata.EndLine}}]}]}' > ../trivy-0.76.2-$d.json); done

Targets are relative to `before/` and `after/`. The files hold no absolute path.
