# What `kyverno apply` with the standard `restricted` set finds beyond Trivy, and how far it can be trusted offline (DEVDESK-685, S0)

Measurement note of 2026-11-22 (commands run between 10:48:43 and 11:07:24 CET, from the timestamps of the first and last run artifacts; the writing of the note came after). No production file was
changed by the measure. Thresholds T1 to T7 and the go / no-go / pivot grid are the ones of `docs/product/kyverno-policies/stories.md`
(story S0), taken as written; the user validated them on 2026-11-22 ("Oui, tels quels"), before the run; none was adjusted after seeing a number. A dated measure is never corrected afterwards: if a
number moves, add a new note. Rule of this folder: a figure comes from a measure (command and output kept), otherwise it
is written "not measured" with the reason. Raw outputs, exact commands, exit codes and durations are in
`~/projects/kyverno-spike/out/` (outside the repository, local to the author's machine, not versioned): one `.json` (stdout), `.err` (stderr) and `.meta`
(command, exit code, seconds, UTC date) per run.

| | |
|---|---|
| Subject | the 19 policies of `pod-security-vpol/baseline` + `restricted` of `kyverno/policies`, applied with `kyverno apply` to Kubernetes manifests, compared with `trivy config` on the same manifests |
| `kyverno version` | `Version: 1.19.2`, `Time: 2026-10-20T22:13:49Z`, `Git commit ID: e63308f1e16c05cd009ac4864be6ec25c66b49be` (binary, installed by mise) |
| Not measured | Kyverno 1.20.x. It is published (`mise ls-remote kyverno` lists `1.20.0`; `gh api repos/kyverno/kyverno/releases` lists `v1.20.0`, `v1.20.0-rc.*`, checked on 2026-11-22 12:09 CET, after the run), and `kyverno/policies` has an `origin/release-1.20` branch. It was not measured because the binary provided for this spike was 1.19.2 and no 1.20 binary was installed during the run (I did not try; not a publication or installability limit). Everything below is measured with **1.19.2**. The cluster's Kyverno version was not read: querying a cluster is out of the scope of S0 and was not done |
| Trivy | `Version: 0.76.2`; check bundle digest `sha256:b591e9190cff4aa4e385b1a25bccfd9ecf178d3c77fd194e1410b7ce7e1df058`; run as `trivy config <dir> --format json` (default settings, no `--severity`, no skip option); it logged `Using existing checks from cache`, and the bundle digest was identical before and after the runs (first downloaded 2026-11-16). Whether a newer bundle exists was not checked beyond what Trivy does by default |
| Others | `helm v4.3.1+gc0d08d0`, `kustomize v5.8.3`, `yq`, `jq`, `python3` 3.14 (stdlib only) |
| Policies | clone `~/projects/kyverno-policies`, `main`, HEAD `85554e1ef32b5b1dc83f22d0e26c11917df748b5` (2026-11-12). The clone was only read; the legacy variant was extracted with `git archive origin/release-1.19` (`693ea0215f96347dbd71203e6d1a824efd12f7c8`, 2026-11-15) into the spike folder |
| Test repositories | `--depth 1` clones, read only: `microservices-demo` `c7cb6d5b`, `argocd-example-apps` `8088f4c0`, `examples` (kubernetes/examples) `22934a04`, `kubernetes-goat` `723a0db4` (deliberately vulnerable, not representative) |
| Machine | Linux 6.12.107 (Debian 13), 22 CPUs. No cluster, no Docker used. The only network use was Trivy's default check-bundle handling |
| Scripts | `out/scripts/` (`run.sh`, `classify.sh`, `index.sh`, `batches.py`, `agg.py`, `analysis.py`, `attr.py`, `c4.py`, `raw.sh`, `rend.sh`); thrown away with the spike folder, not committed |

## Method in one page

1. **Which files are manifests.** `kyverno apply -r <dir>` is not recursive (criterion 8, case e5), so the runs pass explicit files, as DevDesk's discovery would.
   `classify.sh` sorted every `*.yaml`/`*.yml` of each repository: contains `{{` (Helm template), kind `Kustomization` or `Component`,
   no `apiVersion`/`kind`, or a Kubernetes manifest ("k8s" class, inventories `out/inventory-<repo>.tsv`). "Raw mode" (milestone 1) = all "k8s" files:
   51 + 40 + 240 + 17 files, 144 + 50 + 270 + 36 documents (microservices-demo, argocd-example-apps, examples, kubernetes-goat).
2. **Duplicates make `kyverno apply` panic** (criterion 8, case f2; the `c8-*` file names do not all carry the letters of the criterion 8 table: `c8-j5-*` is the `--warnings-as-errors` case of criterion 2 and `c8-l-*` is case k), so the files of a repository were split by a greedy script into batches
   in which no two documents share (group, kind, namespace or `default`, name): 4 + 2 + 7 + 2 batches. Each batch ran once per policy
   (19 runs) and once with all 19 policies. Result: 300 raw-mode runs, 200 render-mode runs, all kept. In a batch, a result maps to exactly one source document.
3. **Render mode** (milestone 2): `helm template rel <chart>` on every `Chart.yaml`, `kustomize build <dir>` on every `kustomization.yaml`
   (`out/rendered/`, summary `out/render-summary.tsv`, errors in `out/render-logs/`). A render is one "file"; the same batching applies. 33 builds were run (8 Helm, 25 Kustomize); 18 produced no resource and 15 did:
   14 Kustomize components of `microservices-demo` (10 fail, e.g. `no resource matches strategic merge patch "Deployment.v1.apps/cartservice.[noNs]"`, 4 build to an empty output), 3 Helm charts fail
   (`apps`: `nil pointer evaluating interface {}.namespace`; `helm-dependency`, `plugins/kustomized-helm`: `missing in charts/ directory`), and the Kustomize build of `plugins/kustomized-helm`: `cannot unmarshal string into Kustomization.patches.0`. (An earlier draft said "20 of 34" and "17 components": wrong, see the corrections section. The attachment table of criterion 5 does not depend on it: it counts only the 15 builds that produced resources.)
4. **Trivy** scanned the same documents: raw mode from a copy of the "k8s" files (`out/stage/`, same relative paths), render mode from `out/rendered/<repo>`. A Trivy
   finding is attached to a document by its `StartLine` (the document whose first key is the last one at or before it), because Trivy's JSON in this version has no resource name in `CauseMetadata`.

## Criterion 1: version and provenance

Verdict: OK for what could be measured; cluster version not measured.

- `kyverno version` and `git rev-parse HEAD`: see the header. The CLI (1.19.2) is not older than the `policies.kyverno.io/minversion` of the policies (16 say `1.19.1`, 3 say `1.17.0`: `disallow-host-probes`, `restrict-apparmor-profiles`, `disallow-proc-mount-strict`).
- Cluster Kyverno version: **not measured** (no cluster queried). Alignment with the CLI: not measured. The criterion's "replay with the cluster's version" therefore did not happen.
- Recount on the clone (`out/c1-inventory.txt`, one line per file, read with `yq`): `pod-security-vpol/baseline` = **12** policies, `pod-security-vpol/restricted` = **7**, total **19**; the brief's folder name and the three numbers (7, 12, 19) are confirmed; the 7 restricted names are the ones of the brief.
  Every one of the 19 is `kind: ValidatingPolicy`, `apiVersion: policies.kyverno.io/v1`, `validationActions: [Audit]`, `policies.kyverno.io/severity: medium`; none has `policies.kyverno.io/scored`.
- Legacy variant: `git ls-tree` shows `pod-security`, `pod-security-cel` and `pod-security-vpol` on `origin/release-1.19`, and only `pod-security-vpol` on `origin/release-1.20` (`3a7e5e95cb2e19c91e928dbc232a5c182903fe53`, 2026-11-07) and on `main`. On `release-1.19`, `pod-security/baseline` has 12 `kyverno.io/v1 ClusterPolicy` directories (it has a `disallow-host-ports-range` that `main` does not) and `pod-security/restricted` has 6 (one fewer than `main`).

## Criterion 2: loading and policy types

Verdict: OK on 1.19.2; the 1.20 contradiction is **not measured**.

| Case | Command (shortened) | Loaded / rejected | Exit | stderr | Note |
|---|---|---|---|---|---|
| 19 `ValidatingPolicy` (`main`) | `kyverno apply baseline restricted -r <files> -p --output-format json` (25 batches, `out/raw__ALL19__*`) | 19 of 19 loaded (19 distinct `.results[].policy` in a batch with workloads), 0 rejected | 0 or 1 (see criterion 8) | empty | no `ClusterPolicy`, no rejection |
| 12 `ClusterPolicy` (`release-1.19`, `pod-security/baseline`) | `out/c2-cp-baseline` | 12 loaded, 0 rejected, 13 results | 0 | empty | 12 lines `Warning: <file>: kyverno.io/v1 ClusterPolicy is deprecated and will be removed in a future release; migrate to ValidatingPolicy, ...` **on stdout, before the JSON** (the output is not parseable as one JSON document: `jq` fails with `Invalid numeric literal at line 1, column 8`) |
| 6 `ClusterPolicy` (`release-1.19`, `pod-security/restricted`) | `out/c2-cp-restricted` | 6 loaded, 0 rejected | 1 (1 `fail`) | empty | same 6 warnings on stdout |
| 6 `ClusterPolicy` with CEL (`pod-security-cel/restricted`) | `out/c2-cel-cp-restricted` | 6 loaded | 1 (1 `fail`) | empty | same warnings on stdout |
| Same with `--warnings-as-errors` | `out/c8-j5-*` | warning counted | 1 | `Error: found 1 deprecation warning(s) and --warnings-as-errors is set` | |

By type, counted on 1.19.2: ValidatingPolicy 19 / loaded 19 / rejected 0; ClusterPolicy (legacy, `release-1.19`) 18 / loaded 18 / rejected 0 (a deprecation warning each). **Kyverno 1.20.x: not measured** (no binary). The question "does 1.20 reject a `ClusterPolicy` or only warn" is left open by this note; only 1.19.2's behaviour (warn on stdout, load) is a measure.
Mixed directory (`out/c8-j*`): a directory with a valid policy and an invalid-CEL policy **fails entirely** (exit 1, empty stdout, see criterion 8); a valid policy plus a `ClusterPolicy` works (3 results from 2 policies, warning on stdout); a valid policy plus a policy whose YAML is malformed works **and drops the malformed one without any message** (exit 1 only because of a `fail`, stderr empty, only 1 policy in the report).

## Criterion 3: report format

Verdict: OK for what DevDesk needs except the **rule**, which does not exist for these policies.

Exploration run (`out/c0-first-try.json`, reproduces the user's first try): `kyverno apply disallow-privileged-containers.yaml --resource microservices-demo/kubernetes-manifests -p --output-format json`, exit 0, stderr empty, one JSON document, summary `{"pass":12,"fail":0,"warn":0,"error":0,"skip":0}`.

One JSON document in every one of the 480 of 500 raw/render runs that produced output (the other 20 have an empty stdout) (never several; the case of namespaced policies producing several reports was not exercised: the 19 policies are cluster-scoped). Raw example of a `fail` (cut; `out/c8-g-violations.json`):

```json
{"kind":"ClusterReport","apiVersion":"openreports.io/v1alpha1","metadata":{"name":"merged"},"source":"",
 "summary":{"pass":1,"fail":1,"warn":0,"error":0,"skip":0},
 "results":[{"source":"KyvernoValidatingPolicy","policy":"disallow-privileged-containers",
  "category":"Pod Security Standards (Baseline) in ValidatingPolicy","severity":"medium",
  "timestamp":{"seconds":1795341684,"nanos":0},"result":"fail","scored":true,
  "resources":[{"kind":"Pod","namespace":"team-a","name":"bad-priv","apiVersion":"v1"}],
  "message":"Privileged mode is disallowed. All containers must set the securityContext.privileged field to `false` or unset the field.",
  "properties":{"cel.validationIndex":"0","process":"background scan"}}]}
```

| Datum | JSON path (measured) |
|---|---|
| policy | `.results[].policy` (also `.source` = `KyvernoValidatingPolicy`, `.category`) |
| rule | **none**: no `rule` key in any of the 11 392 results of the runs. The only trace of which validation failed is `.results[].properties["cel.validationIndex"]` (present on `fail`, absent on `pass`) |
| result | `.results[].result` (`pass`, `fail`, `warn`, `error` seen; `skip` never produced by any run) |
| severity | `.results[].severity` |
| message | `.results[].message` (`success` on `pass`) |
| resource | `.results[].resources[0].{kind,namespace,name,apiVersion}` (always one resource per result); the namespace is `default` when the manifest has none; the name is empty for `generateName` objects |
| totals | `.summary.{pass,fail,warn,error,skip}` |

Severity (cases `out/c3-*`, same policy and same two Pods): annotation absent, the `severity` key is **absent** from the result; `low`, `medium`, `high`, `critical` are copied as is (`critical` stays `critical`); an unknown value (`blocker`) gives an **absent** key (silently dropped). `policies.kyverno.io/scored: "false"` turns `fail` into `warn` and the `scored` key disappears from the result; the exit code is still **1** (summary `fail 0, warn 1`; in text mode, without `-p`, the same case prints `fail: 1, warn: 0`). `validationActions: [Deny]` or `[Audit, Deny]` instead of `[Audit]` changes nothing in the result or the exit code. `--detailed-results` changed nothing visible in the JSON. Result order inside `.results` varies from run to run (e.g. `pass` first, then `fail`, in one run and the reverse in the next).
The 19 policies of the set **all have severity `medium`** (criterion 1), so "severity of the policy" does not differentiate anything for this set.

## Criterion 4: reliability offline

Verdict: OK.

Method: for each of the 19 policies, a compliant manifest and a non-compliant one, written by this spike (not taken from the policies' tests), in `~/projects/kyverno-spike/cases/c4/` (`gen.sh`): one Pod compliant with `restricted` and, per policy, one `jq` mutation that breaks only that control (e.g. `hostNetwork: true`, `procMount: Unmasked`, `runAsUser: 0`, a `nfs` volume, a probe with `httpGet.host`). Expected: `pass` on the compliant one, `fail` on the other, for the policy under test. Wrapped in the six kinds `Pod`, `Deployment`, `StatefulSet`, `DaemonSet`, `Job`, `CronJob` (114 runs, `out/c4-pair__*`).

- Pod: **38 of 38** verdicts correct (19 x 2). All six kinds: **228 of 228** (19 x 2 x 6): 114 `pass` where expected, 114 `fail` where expected; no `skip`, no `error`, no missing result.
- The policies' own suites, run with `kyverno test` (`out/c4-kyverno-test/`): `.kyverno-test` 19 suites, **1121 passed, 0 failed**; `.kyverno-test-controllers` 16 suites, **112 passed, 0 failed**; `.kyverno-test-exceptions` 16 suites, **67 passed, 0 failed** (3 policies have no controllers/exceptions suite: `disallow-host-probes`, `restrict-apparmor-profiles`, `disallow-proc-mount-strict`). The `exceptions.*` variable (used by 16 of the 19 files) therefore evaluates without a cluster, with and without a `PolicyException`.
- Text search in the 19 files: `apiCall` 0, `request.` 0, `namespaceSelector` 0, `resource.` 0, `globalContext` 0; `configMap` 1 file, as a volume field (`has(vol.configMap)`, `restrict-volume-types`), not a context. 16 policies match `replicationcontrollers`; 3 do not (`disallow-host-probes`, `restrict-apparmor-profiles`, `disallow-proc-mount-strict`), so those 3 return no result on a ReplicationController (not even `skip`). No autogen was needed: the policies list `pods`, `deployments`, `statefulsets`, `daemonsets`, `jobs`, `cronjobs` (and `replicasets`) in `matchConstraints`.
- Real manifests: 0 `skip` in all runs; `error` only once per policy (19 `error` results) on one document: `microservices-demo/kustomize/components/without-loadgenerator/delete-loadgenerator.patch.yaml`, a Kustomize patch (`Deployment` without `spec`, `$patch: delete`) that the classifier took for a manifest; message `error: no such key: spec`. Explained, and a consequence of passing a patch file as a manifest.
- Network: the same all-19 run on the largest batch (`examples`, 198 files) under `unshare -rn` (new network namespace, only `lo`, down; `bash` confirms `connect: Network is unreachable`) gave **identical normalised results** (same md5 of the sorted policy/result/resource/message list as the run on the host network), exit and stderr unchanged, same duration (17.73 s against 18.09 s). So no network access was needed by these 19 policies on that batch. `strace` is not installed, so no connection capture was made; only this test.
- Duration: one process start costs about 0.3 s; per-policy runs 0.43 s on average (475 runs, max 2.79 s); all-19 runs 0.36 s to 18.09 s depending on the batch (25 runs, 76.2 s in total).

## Criterion 5: attaching a violation to a file and a line

Verdict: **gap** (below the thresholds with a single run per repository; above with one run per file in raw mode).

The report has no file and no line (criterion 3), so the matching is done on (kind, namespace or `default`, name) against the documents' index (`index.sh`: file, document, kind, namespace, name, line of `metadata.name`, found with `yq`). "Violation" = a failing (document, policy) couple, 759 in raw mode and 161 in render mode (the same couples as criterion 6).

**Raw mode (milestone 1)**, one run per repository (all documents of the repository known), "unambiguous" = exactly one source document matches and has a name:

| Repository | Violations | Unambiguous (file + line) | Ambiguous (same key in several files) | Unnamed (`generateName`) | Not found |
|---|---|---|---|---|---|
| microservices-demo | 37 | 1 | 36 | 0 | 0 |
| argocd-example-apps | 105 | 65 | 32 | 8 | 0 |
| examples | 539 | 411 | 128 | 0 | 0 |
| kubernetes-goat | 78 | 78 | 0 | 0 | 0 |
| **Total** | **759** | **555 (73.1 %)** | 196 | 8 | 0 |

Examples: ambiguous = `Deployment/adservice` exists in `kubernetes-manifests/`, `kustomize/base/` and `release/kubernetes-manifests.yaml` (lines 18, 18, 24 / 768 for other services); unnamed = a `Job` with `metadata.generateName: upgrade-sql-schema` (`argocd-example-apps/sync-waves/manifests.yaml`), reported with an empty `name`.

Workaround measured: one `kyverno apply` per file (91 files of `microservices-demo` and `argocd-example-apps`, `out/c5-perfile/`: **42.0 s in total, about 0.46 s per file** (the 42.0 s was printed by the shell loop that ran them and was not stored in a file; the modification times of the 91 `.json` files in `out/c5-perfile/` span 41.2 s from first to last, which agrees; no `.meta` per file exists), same 105 and 37 `fail` as the batched runs; it also removes the duplicate-panic). With it, the file is known and the line is found unless the name is empty or repeated in the file: **751 of 759 (98.9 %)** (microservices-demo 37, argocd-example-apps 97 of 105, examples 539, kubernetes-goat 78; the last two repositories were computed from the same index, not run per file).

**Render mode (milestone 2)**:

| Mode | Violations | Attachable to the source file or chart | Attachable to the line | Ambiguous | Not found |
|---|---|---|---|---|---|
| Helm (`helm template`; microservices-demo 12, argocd-example-apps 4, kubernetes-goat 8) | 24 | 24 (100 %): each rendered document is preceded by `# Source: <chart>/templates/<file>` | 22 (91.7 %): the template has exactly one `kind: <Kind>` line (the name is templated, so a line is the `kind:` line, not the name) | 0 | 0 |
| Kustomize (`kustomize build`; microservices-demo 72, argocd-example-apps 65) | 137 | 49 (35.8 %) | 49 | 69 (all in microservices-demo: the same object is in the base, `kubernetes-manifests/` and `release/`) | 19 (16 renamed by `namePrefix` in `pre-post-sync` and `kustomize-guestbook`: e.g. `kustomize-guestbook-ui`, 3 generated or without a source document, e.g. `opentelemetrycollector`) |
| **Total render** | **161** | **73 (45.3 %)** | **71 (44.1 %)** | 69 | 19 |

Namespaces set by an overlay were not seen in these repositories (a rendered namespace was `default` everywhere), so that case is **not measured**. Restricting the candidate sources to the files a `kustomization.yaml` references (which would cut most of the 69 ambiguities) was **not measured**.

## Criterion 6: overlap with Trivy (value criterion)

Verdict: **O = 97.7 %**.

### Step 1: the mapping, written before any overlap number

Written at 2026-11-22T09:52:59Z (timestamp inside `out/c6-mapping.md`, kept as is), after reading the 19 policy files and the Trivy bundle's check metadata (`~/.cache/trivy/policy/content/policies/kubernetes/policies/*.rego`, list in `out/trivy-ksv-checks.txt`, 79 `KSV-*` checks), before any Trivy scan of the test repositories. Doubtful cases are counted as covered.

| # | Kyverno policy | Trivy checks on the same control | Doubt |
|---|---|---|---|
| 1 | disallow-capabilities | KSV-0022, KSV-0005, KSV-0119, KSV-0120 | allowed list may differ |
| 2 | disallow-host-namespaces | KSV-0008, KSV-0009, KSV-0010 | |
| 3 | disallow-host-path | KSV-0023, KSV-0006, KSV-0121 | |
| 4 | disallow-host-ports | KSV-0024 | |
| 5 | disallow-host-probes | **none** | |
| 6 | disallow-host-process | KSV-0103 | |
| 7 | disallow-privileged-containers | KSV-0017 | |
| 8 | disallow-proc-mount | KSV-0027 | Kyverno exempts `hostUsers: false` pods |
| 9 | disallow-selinux | KSV-0025 | |
| 10 | restrict-apparmor-profiles | KSV-0002 | Trivy: "runtime/default or undefined", Kyverno also accepts `Localhost` |
| 11 | restrict-seccomp | KSV-0104, KSV-0030 (stricter) | |
| 12 | restrict-sysctls | KSV-0026 | |
| 13 | disallow-capabilities-strict | KSV-0106, KSV-0003, KSV-0022 | |
| 14 | disallow-privilege-escalation | KSV-0001 | |
| 15 | disallow-proc-mount-strict | KSV-0027 | |
| 16 | require-run-as-nonroot | KSV-0012 | |
| 17 | require-run-as-non-root-user | KSV-0105, KSV-0020 (broader) | pod-level `runAsUser` handling not read |
| 18 | restrict-seccomp-strict | KSV-0030, KSV-0104 | |
| 19 | restrict-volume-types | KSV-0028, KSV-0023, KSV-0121 | Trivy's allowed list vs Kyverno's |

Policies with **no** Trivy check: **1** (`disallow-host-probes`). With at least one check: **18** (6 of them with a doubt: 1, 8, 10, 15, 17, 19).
Decision made before computing: a couple is "covered" if the same document has at least one finding of the mapped checks (any severity); couples in files that Trivy did not parse count as residual in the main figure, and the figure without them is given as a second figure.

### Step 2: instance by instance

Unit: the couple (document, Kyverno policy) in `fail`. `out/couples-raw.tsv`, `out/couples-rend.tsv` give each couple with its Trivy match.

| Scope | Couples | Covered | Residual | O |
|---|---|---|---|---|
| Raw mode, all | 759 | 739 | 20 | **97.4 %** |
| &nbsp;&nbsp;microservices-demo | 37 | 37 | 0 | 100.0 % |
| &nbsp;&nbsp;argocd-example-apps | 105 | 100 | 5 | 95.2 % |
| &nbsp;&nbsp;examples | 539 | 524 | 15 | 97.2 % |
| &nbsp;&nbsp;kubernetes-goat (not representative) | 78 | 78 | 0 | 100.0 % |
| Raw mode without kubernetes-goat | 681 | 661 | 20 | 97.1 % |
| Raw mode, files Trivy parsed | 747 | 739 | 8 | 98.9 % |
| Render mode, all | 161 | 160 | 1 | 99.4 % |
| **Raw + render** | **920** | **899** | **21** | **97.7 %** |

Policies that failed at least once: 11 of 19 (raw), 4 (render). Eight policies never failed on these repositories: `disallow-host-probes`, `disallow-host-process`, `disallow-proc-mount`, `disallow-proc-mount-strict`, `disallow-selinux`, `restrict-apparmor-profiles`, `restrict-seccomp`, `restrict-sysctls`. Failing couples per policy (raw): `restrict-seccomp-strict` 199, `disallow-privilege-escalation` 162, `disallow-capabilities-strict` 162, `require-run-as-nonroot` 155, `restrict-volume-types` 40, `disallow-privileged-containers` 10, `disallow-host-path` 9, `disallow-host-ports` 8, `disallow-host-namespaces` 7, `disallow-capabilities` 6, `require-run-as-non-root-user` 1.
Policies with residual couples: `disallow-privilege-escalation` 5, `require-run-as-nonroot` 5, `disallow-capabilities-strict` 5 (4 raw, 1 render), `restrict-seccomp-strict` 3, `restrict-volume-types` 3.
Doubtful cases: none had to be decided case by case beyond the policy-level doubts above, which were already counted covered. Sensitivity (not the main figure): keeping only the **first** listed check per policy (a stricter mapping) gives 720 of 759 raw (94.9 %) and 154 of 161 render (95.7 %), 874 of 920 combined (95.0 %).
Two method limits. (a) "Raw + render" (97.7 %) counts some of the same objects twice, once from the source file and once through a Kustomize/Helm render (e.g. `front-end` is residual #2 in raw mode and #20 in render mode); the raw figure alone, 97.4 %, has no such double count. (b) Coverage is judged at document level: a Trivy finding of a mapped check on any container of the document covers the couple, whereas `stories.md` asks for "the same condition"; this can only overstate coverage. The verdict does not move: the stricter mapping of the sensitivity run above gives 95.0 %, far above 85 %. Trivy detected 16 of the 17 "k8s" files of `kubernetes-goat` (as 235 of 240 for `examples`, 51 of 51 and 40 of 40 for the other two); this has no effect on the figures, the 78 couples of that repository are all covered.

Reading the O of this table: the test repositories are four public ones, one of them a deliberately vulnerable repository; two of the four (`microservices-demo`, `kubernetes-goat`) have a single policy or ten policies failing; the Trivy figure depends on Trivy's default checks at this bundle digest.

## Criterion 7: residual value

Verdict: **OK on the numbers (T2 reached); the value is thin and mostly specific cases.**

Residual: **21 couples**, **10 distinct documents**, **5 distinct policies** (`out/residual-pool.tsv`). Sample: all residual couples were numbered and 10 drawn with `random.seed(685)` (`out/residual-sample.tsv`; 21 is more than 10), each read in its source.

| # | Couple (policy / document) | True positive? | Why Trivy does not report it |
|---|---|---|---|
| 0 | `disallow-privilege-escalation` / `Job` with `generateName`, `argocd-example-apps/helm-hooks/manifests.yaml` | yes: the container has no `securityContext` | Trivy's output on that Job has KSV-0030 and KSV-0106 (lines 12-13) but no KSV-0001 or KSV-0012: probably its checks do not report the unnamed object (an inference; the finding itself, KSV-0001 and KSV-0012 missing for the unnamed Job and present for the named ones, is measured) (the other Jobs of the file, which have a name, are reported) |
| 2 | `disallow-capabilities-strict` / `Deployment/front-end`, `sock-shop/base/front-end-dep.yaml` | yes by the policy text (`drop: [all]` in lower case; the policy requires `ALL`); how Pod Security Admission treats `all` was not measured | KSV-0003 and KSV-0106 are not raised for `drop: [all]` (case-insensitive match) |
| 7 | `require-run-as-nonroot` / `Deployment/etcd`, `examples/_archived/openshift-origin/etcd-controller.yaml` | yes: `securityContext` has only `capabilities: {}` and `privileged: false` | the file is **not parsed by Trivy**: duplicate `selector` key in `spec` (Trivy detected 235 of the 240 "k8s" files of `examples`, 3 of the 5 are this folder) |
| 9 | `disallow-capabilities-strict` / `Deployment/etcd-discovery` (same folder) | yes: no `drop: [ALL]` | same: file not parsed by Trivy |
| 12 | `restrict-seccomp-strict` / `Deployment/etcd-discovery` | yes: no seccomp profile | same |
| 14 | `disallow-privilege-escalation` / `Deployment/openshift` | yes: no `securityContext` | same |
| 16 | `restrict-seccomp-strict` / `Deployment/openshift` | yes: no seccomp profile | same |
| 17 | `restrict-volume-types` / `ReplicationController/redis`, `_archived/storage/redis/redis-controller.yaml` | **no (artefact)**: `volumes: - name: data` has no volume source, the object is incomplete | Trivy raises nothing on a volume without a type |
| 19 | `restrict-volume-types` / `Pod/cephfs`, `_archived/volumes/cephfs/cephfs.yaml` | yes: `cephfs` is not in the policy's allowed list (and is a non-core type) | KSV-0028 and KSV-0023 do not fire on `cephfs` in this bundle |
| 20 | `disallow-capabilities-strict` / `Deployment/front-end` in the rendered `sock-shop` Kustomize output | yes (same source as #2, seen through the render) | same as #2 |

Precision of the sample: **9 true positives of 10 verified = 90 %** (one artefact, #17). Caveat: 5 of the 10 sampled couples (#7, #9, #12, #14, #16) are in the three `openshift-origin` files that Trivy cannot parse (correlated), and #20 is the same object as #2 seen through a render, so the 9 true positives rest on 9 distinct objects of which 3 are correlated files. Count: 21 residual couples, 10 documents, 5 policies. What each residual control would ask of Trivy (no cost estimate):

- `disallow-capabilities-strict` (`drop` value compared case-sensitively to `ALL`): a stricter comparison in KSV-0003 / KSV-0106;
- `disallow-privilege-escalation`, `require-run-as-nonroot` on objects without `metadata.name`: the KSV-0001 / KSV-0012 checks to evaluate nameless workloads (`generateName`);
- `restrict-volume-types` (`cephfs`): a wider disallowed list in KSV-0028;
- `restrict-seccomp-strict`, `disallow-capabilities-strict`, `require-run-as-nonroot` on the OpenShift sample files: nothing to ask of a check; it is the YAML parser (duplicate key) that hides the documents from Trivy;
- `disallow-host-probes`: no Trivy check exists; a new check on `httpGet.host` and `tcpSocket.host` would be needed. It produced **no** violation on these four repositories, so its residual value is not measured.

## Criterion 8: errors and edge cases

Verdict: **gap** (silent false greens; exit codes coherent with the report).

Cases in `~/projects/kyverno-spike/cases/c8/`, outputs `out/c8-*`. Policy under test: `disallow-privileged-containers` (`pol-ok`); violating manifests: one bad Pod and one good Pod (`res-viol`).

| Case | Exit | stdout | stderr (start) | Distinguishable from a clean run by DevDesk? |
|---|---|---|---|---|
| (a) malformed policy YAML, as a directory or as a file | **0** | empty | empty | no from the code or stderr; only by the absence of any report on a run that had workloads |
| (b) valid YAML, invalid CEL (`====`) | 1 | empty | `Error: failed to apply policies on resource bad-priv (failed to compile policy disallow-privileged-containers ([<nil>: Internal error: validating policy compiler 1.0 error: failed to compile policy, spec.validations[0].expression: Invalid value: "variables.allContainers.all(container, container.securityContext.privileged ==== false)": ERROR: <input>:1:79: Syntax error: extraneous input '==' ...` | yes (code and stderr); same error with an undefined variable (`undefined field 'doesNotExist'`), with a clean Pod and with Services only (the error does not depend on a violation); the whole run fails, no partial report |
| (c) empty policy directory | **0** | empty | empty | no |
| (d) nonexistent policy directory | **0** | empty | empty | no |
| (d2) policy directory with a `ConfigMap` as the only file, and the same directory with a valid policy next to it | **0** | empty | empty | no, and the valid policy next to the `ConfigMap` is **also not applied** (order of file names changed nothing; a `README.md` or a `kustomization.yaml` next to it did not matter) |
| (e) empty manifests directory | 0 | empty | empty | no from the output alone |
| (e2) a YAML without `kind` in the manifests (`values.yaml`) | 1 | empty | `Error: failed to load resources (Object 'Kind' is missing in 'just: a plain yaml, not a k8s resource ')` | yes; the whole run fails, so non-manifest YAML must be filtered before |
| (e3) nonexistent manifests path | 1 | empty | `Error: failed to load resources (stat cases/c8/res-missing: no such file or directory)` | yes |
| (e4) manifests with no workload (a Service only) | 0 | empty | empty | no: same output as (c) and (d) |
| (e5) `-r <dir>` whose files are in sub-directories (the `examples` root, `out/c8-e5-*`) | 0 | empty | empty | no; `-r` is not recursive, so nothing is evaluated |
| (f) malformed manifest (`yaml: line 4: did not find expected key`), alone or next to a good manifest | 1 | empty | `Error: failed to load resources (yaml: line 4: did not find expected key)` (**no file name**) | yes; the good manifest is not evaluated either |
| (f2) two documents with the same kind, namespace, name | **2** | empty | `panic: pods "bad-priv" already exists` + Go stack (`k8s.io/client-go/dynamic/fake...NewSimpleDynamicClientWithCustomListKinds`) | yes (code 2, a panic); it happened on the 4 repositories with all documents in one run (`out/c8-duplicate-panic/`) |
| (g) violations present | 1 | one `ClusterReport`, `summary.fail` 1 | empty | yes |
| (h) no violation | 0 | one `ClusterReport`, `summary.pass` 1 | empty | yes |
| (i) legacy `ClusterPolicy` in the policy directory | 0 or 1 | deprecation `Warning:` lines then the JSON | empty | the stdout is not valid JSON (criterion 2) |
| (j) a valid policy + a malformed-YAML policy | 1 (because of a `fail`) | report with only the valid policy | empty | no: the malformed policy is silently missing |
| (k) `--output-format json` piped into `tee` (`out/c8-l-*`) | **0** with violations | report with `fail` 1 | empty | the exit code is 0 because it is the exit code of `tee`; run without a pipe (case g) it is 1 |

Coherence of the exit code with the report, over every run whose stdout is a report (`raw__*`, `rend__*`, `c3-*`, `c8-g*`, `c8-h*`: 491 runs): `fail > 0` or `error > 0`: exit 1 (134 runs); `warn > 0` with `fail 0` and `error 0`: exit 1 (1 run, the `scored: "false"` case); all counters zero (356 runs): exit 0; no run with a nonzero counter and exit 0. An empty stdout with exit 0 happened in 20 runs (a batch whose only file was a `StorageClass`) and means "nothing evaluated". The case of exit 0 with violations reported in `research.md` (1.12.5) was **not seen on 1.19.2**, except through a pipe (case k).
Reliable signal: the **exit code** for compile errors, unreadable manifests and duplicates (1 or 2 with stderr), and the **presence of a report plus its `summary`** for the result (code and report agree). Neither signals an empty, missing or partly invalid set of policies: those four cases (a, c/d, d2, j) exit 0 or 1 with no message and an empty or incomplete report.

## Criterion 9 and the decision

**Thresholds** (T1 to T7 of `stories.md`), one by one, with the value measured by this note:

| # | Threshold | Measured | Status |
|---|---|---|---|
| T1 | reached if O is at most 70 %; O of 85 % or more triggers no-go | **O = 97.7 %** (920 couples, 899 covered); 97.4 % raw, 99.4 % render; each repository 95.2 % to 100 % | **not reached**; and above 85 % (no-go trigger) |
| T2 | at least 10 residual violations on at least 3 resources and 3 policies, precision at least 70 % | 21 couples, 10 documents, 5 policies, precision 9 of 10 = 90 % | **reached** (fewer than 5 residual would have been a no-go: not the case) |
| T3 | at most 1 of 19 rejected by the retained version (the cluster's, else the latest CLI) | 0 of 19 rejected on **1.19.2**; the cluster version and 1.20.x were not measured | **not measured** (0 of 19 on 1.19.2 only; a threshold not measured never counts as reached) |
| T4 | at least 90 % of the 38 verdicts correct (34 of 38) and at most 1 `error`/`skip` unexplained | 38 of 38 (228 of 228 over six kinds); 0 `skip`; 1 explained `error` document | **reached** |
| T5 | milestone 1: at least 90 % of the violations unambiguously attachable; milestone 2: at least 60 % to file or chart | milestone 1: 73.1 % with one run per repository, 98.9 % with one run per file; milestone 2: 45.3 % (file or chart), 44.1 % (line) | milestone 1 **not reached as defined** (reached with the one-run-per-file workaround, 0.46 s per file measured); milestone 2 **not reached** |
| T6 | invalid policy, empty directory and an inconsistent exit code all detectable (code, stderr or report), no silent false green without a workaround | invalid CEL: detected. Empty or missing policy directory, malformed policy YAML, non-policy file in the directory: exit 0 (or 1 for another reason) with nothing on stderr and no report change. No inconsistent exit code seen | **not reached** as written (workarounds would be DevDesk-side checks, not measured as built) |
| T7 | policy, rule, result, resource and message in 100 % of the `fail` results; severity present when the annotation is | policy, result, message, severity, kind, namespace: 100 %; **rule: 0 %** (no such key); resource name empty in 16 of 1840 `fail` results (0.9 %, the `generateName` objects; the 1840 count includes the all-19 runs next to the per-policy runs) | **not reached** |

**Grid, read in order** (first line that applies):

1. **No-go if O is 85 % or more, or fewer than 5 residual violations.** O is 97.7 % (and 97.1 % without the non-representative repository), at or above 85 %. This line applies.

**Verdict: no-go.** Trivy's `KSV-*` checks already report 899 of the 920 failing couples (97.7 %) on the same documents; the remaining 21 couples, 9 of 10 true positives in the sample, come from 10 documents and 5 policies, mostly (12 of 21) three archived files that Trivy cannot parse, then nameless workloads (4), a `drop: [all]` case (2, source and render), and a non-core volume type or incomplete volume (3). This is the "Trivy already sees most of it" outcome that S0 says is an acceptable result.

Scope of the verdict: it holds for the standard Pod Security `baseline` + `restricted` set (19 policies), Kyverno 1.19.2, four public repositories (one deliberately vulnerable) and Trivy 0.76.2 with default settings. It does **not** settle an organisation's own policies (S7), other policy sets, or another Kyverno version.

For the record (S0 asks for them after a no-go), the residual controls and what each would ask of Trivy are listed in criterion 7. The grid lines below the first (go, pivot, T5/T7 reserve) are not applied; for information, T3 is not measured, T6 and T7 are not reached, so even without the overlap the set would not have been a go.

## Blocking questions

None. (Open points that do not block the verdict are listed below.)

## What remains not measured

- Kyverno **1.20.x**: published, but no 1.20 binary was installed or run (see the header); behaviour of the legacy `ClusterPolicy` under 1.20 (reject or warn) not measured (only 1.19.2: warn, on stdout, and load).
- The **cluster's Kyverno version** and its alignment with the CLI (no cluster queried).
- The user's own repositories (none provided); only four public ones, one deliberately vulnerable.
- Several reports for namespaced policies (`Policy`/namespaced types), the `generate`, `mutate` and `verifyImages` paths: not exercised (the 19 policies are `ValidatingPolicy`).
- Namespaces set by a Kustomize overlay (none in these repositories); restricting Kustomize candidates to the files a `kustomization.yaml` references.
- Per-file `kyverno apply` timing on `examples` and `kubernetes-goat` (only 91 files timed).
- Trivy: whether a bundle newer than the digest above exists; Trivy's own Helm rendering (`trivy config` on a chart directory) instead of `helm template`.
- Pod Security Admission's treatment of `drop: [all]` (lower case).
- The precision of the residual sample covers the whole residual pool of this run (10 of 21); a different set of repositories may give other residuals.

## Errors and corrections during the run

- The first run of all-19 on whole repositories panicked (duplicate documents); the per-policy runs of that first attempt (76 runs, all exit 2) were deleted, the four all-19 outputs were kept as evidence in `out/c8-duplicate-panic/`, and the runs were redone in batches.
- A first pass of the case matrix for criterion 8 was run with a quoting mistake (the option string passed as one argument, every case ended in `unknown shorthand flag`) and was deleted and redone; nothing from that pass is used.
- Shell locale (comma decimals) broke the duration field of the first `run.sh`; fixed with `LC_ALL=C` before any run used in this note.
- The first count of `count of failing couples` in a first analysis draft double-counted `helm` couples (file level and line level added together); the table above is the corrected one (24 Helm couples, not 48).
- The time span in the header was first written as "about 11:30" without support; replaced by the first and last artifact timestamps (10:48:43 to 11:07:24).
- Render builds were first stated as "20 of 34 produced no resource, 17 Kustomize components"; `out/render-summary.tsv` has 33 builds, 18 without a resource (14 Kustomize components of `microservices-demo`: 10 fail and 4 are empty; 3 Helm charts; the `plugins/kustomized-helm` Kustomize build). The attachment table does not depend on it.
- "500 runs that produced output" corrected to 480 of 500 (20 empty stdout); all-19 durations corrected from "2.4 s to 18.1 s" to 0.36 s to 18.09 s; the panic case reference corrected from "case i" to f2; T3 aligned on "not measured" in the grid text; the first draft said no 1.20 binary was available, which hid that 1.20.0 is published (the cause is that none was installed).
- The per-file timing (42.0 s) was not stored in a file; it is corroborated by file modification times (41.2 s), see criterion 5.
- Added after review: the scope of the verdict, the two method limits of the overlap figure, the sample correlation caveat, the Trivy detection of 16 of 17 `kubernetes-goat` files.
