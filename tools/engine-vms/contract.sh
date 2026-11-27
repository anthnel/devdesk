#!/usr/bin/env bash
# Run the engine contract tests (the *_contract_test.go files) in a test VM (OC-09a1, DEVDESK-806; labels and
# cleanup, OC-09a2, DEVDESK-943; recording, OC-09a3, DEVDESK-944).
# Usage: contract.sh verify <docker|podman> <n|n1> [--mode rootful|rootless] [--no-reset]
#        contract.sh record <docker|podman> <n|n1> [--mode rootful|rootless] [--keep-out <dir>]
#   verify     run the contracts and compare what they see to the recorded fixtures
#   record     run the contracts in recording mode: they write their exchanges, redacted, to a directory of the
#              VM; once the run and the sentinel check passed, that one directory comes back and is imported
#              on this machine (see below)
#   --mode     docker is rootful only; podman defaults to rootless (the mode kind and most users have)
#   --no-reset keep the VM as it is instead of restoring its clean snapshot first (faster to iterate;
#              the line installed in the VM must still be the one asked for). Refused with record: the
#              fixtures describe what a clean VM answers, so a recording always starts from the clean snapshot.
#   --keep-out import into <dir> (absolute) instead of internal/enginefixture/fixtures: a dry run, the
#              repository is not touched (record only)
# The gate comes first and applies on this machine: without DEVDESK_ENGINE_CONTRACT=1 in the
# environment nothing happens (exit 0); with CI, GITHUB_ACTIONS, GITLAB_CI, BUILDKITE or TF_BUILD set
# nothing happens either (exit 1), even with the variable. Set the variable for the one command, as
# in `DEVDESK_ENGINE_CONTRACT=1 mise run engine-contract -- docker n`, and do not export it: an
# exported variable opens the gate for every later `go test` of the shell. The Go tests apply the same
# gate again in the VM (internal/testutil/enginecontract).
# Exit codes: 0 done or skipped, 1 refused or failed, 2 invalid arguments.
#
# What it does, in order: builds one test binary per package that has a contract (linux/amd64,
# -trimpath), restores the VM (unless --no-reset), pulls CONTRACT_IMAGE (versions.env, pinned by digest)
# into the VM, once: this script is the only place that pulls, the Go code never does. It then starts a
# sentinel container WITHOUT any label, copies the binaries and versions.env to /tmp/ddc-<run> in the VM,
# runs them there with an explicit environment, under root (rootful) or the unprivileged user (rootless),
# checks that the sentinel survived, then removes the sentinel by its id and the directory, on success or
# not. The contracts label what they create with devdesk.test=<run> and remove only what carries that
# label, so a sentinel that is gone means a cleanup that touched a container without the label: the
# run fails. When verifying, nothing is copied back.
#
# Recording (record). The test binary gets DEVDESK_FIXTURE_RECORDING=1 and DEVDESK_FIXTURE_OUT=<dir>/out in its
# explicit environment, and only there: nothing of this machine's environment is passed through. <dir>/out is made
# before the run, for the user of the mode (the recorder writes as that user). Each exchange is redacted and runs
# through the leak check in the VM before it is written. Only when the contracts and the sentinel check passed, the
# one directory <dir>/out comes back as a tar, nothing else leaves the VM. That tar is untrusted input: it is
# listed first and refused when an entry is not <campaign>/<name>.cli (an absolute path, "..", a link, a special
# file, a name outside that shape) or when it is larger than 16 MiB; it is then extracted into a new private
# temporary directory (mktemp -d, no --same-owner, no --same-permissions), and
# `go run ./internal/enginefixture/cmd/engine-fixture-import --from <that directory>` parses every file again,
# checks the version against N and N-1 and runs the leak check with the external deny list
# (DEVDESK_FIXTURE_DENYLIST, a file outside the repository, read from this machine's environment) and the
# identities of this machine, before anything is written to internal/enginefixture/fixtures (or to --keep-out).
# The deny list is required for a recording: without it (unset, unreadable, no active term) the script refuses
# before any connection, and the import is run with --require-denylist as well. The strict extraction lives
# in unpack_recorded.sh, which the tests source.
# The contracts also need the mark of a test VM, /etc/dd/current (written by dd-use.sh): the lab check of
# enginecontract refuses to create or remove a resource anywhere else.
# Variables: the DD_ENGINE_VMS_* ones of lib.sh.
set -euo pipefail

usage() {
  echo "usage: contract.sh verify <docker|podman> <n|n1> [--mode rootful|rootless] [--no-reset]" >&2
  echo "       contract.sh record <docker|podman> <n|n1> [--mode rootful|rootless] [--keep-out <dir>]" >&2
  exit 2
}

# The gate. Nothing above reads the environment, nothing below runs before it. The markers are the
# same, in the same order, as engineguard.CIMarkers; the variable is checked first.
if [[ ${DEVDESK_ENGINE_CONTRACT:-} != 1 ]]; then
  echo "contract.sh: skipped: it reaches a real container engine; run it as DEVDESK_ENGINE_CONTRACT=1 mise run engine-contract -- <docker|podman> <n|n1>, outside CI, and do not export the variable" >&2
  exit 0
fi
for marker in CI GITHUB_ACTIONS GITLAB_CI BUILDKITE TF_BUILD; do
  if [[ -n ${!marker:-} ]]; then
    echo "contract.sh: refused: CI is set (${marker}=${!marker}); contract tests never run in CI, even with DEVDESK_ENGINE_CONTRACT=1" >&2
    exit 1
  fi
done

action=${1:-} engine=${2:-} line=${3:-}
case $action in verify | record) ;; *) usage ;; esac
case $engine in docker | podman) ;; *) usage ;; esac
case $line in n | n1) ;; *) usage ;; esac
shift 3
mode="" reset=1 keep="" into_args=()
while (($#)); do
  case $1 in
    --mode) (($# >= 2)) || usage; mode=$2; shift 2 ;;
    --no-reset) reset=0; shift ;;
    --keep-out) (($# >= 2)) || usage; keep=$2; shift 2 ;;
    *) usage ;;
  esac
done
if [[ $action == record && $reset == 0 ]]; then
  echo "contract.sh: --no-reset is refused with record: the fixtures describe what a clean VM answers, so a recording always restores the clean snapshot first" >&2
  exit 2
fi
if [[ -n $keep ]]; then
  [[ $action == record ]] || usage
  [[ $keep == /* ]] || { echo "contract.sh: --keep-out takes an absolute directory, not '$keep'" >&2; exit 2; }
  into_args=(--into "$keep")
fi
if [[ -z $mode ]]; then
  if [[ $engine == podman ]]; then mode=rootless; else mode=rootful; fi
fi
case $mode in rootful | rootless) ;; *) usage ;; esac
if [[ $engine == docker && $mode == rootless ]]; then
  echo "contract.sh: the docker VM is rootful; --mode rootless needs podman" >&2
  exit 2
fi

# A recording needs the external deny list (the names only you know: domains, projects, customers): the import
# refuses without it (--require-denylist), and so does this script, before anything is built or sent to the VM.
if [[ $action == record ]]; then
  if [[ -z ${DEVDESK_FIXTURE_DENYLIST:-} ]] || ! grep -qsv '^[[:space:]]*\(#\|$\)' -- "$DEVDESK_FIXTURE_DENYLIST"; then
    echo "contract.sh: refused: record needs a deny list: set DEVDESK_FIXTURE_DENYLIST to a file of terms (domains, project and customer names), one per line, outside the repository (it is unset, unreadable or has no active term)" >&2
    exit 1
  fi
fi

. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
. "$(dirname "${BASH_SOURCE[0]}")/unpack_recorded.sh"
root=$(cd "$DD_DIR/../.." && pwd)

# The one image of the contracts is alpine from Docker Hub, pinned by the digest of its index: nothing else
# is started, and nothing that moves. Checked before anything is built or sent to the VM.
[[ ${CONTRACT_IMAGE:-} =~ ^docker\.io/library/alpine@sha256:[0-9a-f]{64}$ ]] ||
  die "CONTRACT_IMAGE in versions.env must be docker.io/library/alpine@sha256:<64 hex>, it is '${CONTRACT_IMAGE:-}'"

# Same shape as NewRunID in enginecontract: the day in UTC, then four random bytes.
run=$(date -u +%Y%m%d)-$(od -An -N4 -tx1 /dev/urandom | tr -d ' \n')
vmdir=/tmp/ddc-$run
work=$(mktemp -d)
touched=0 # set once this script created the directory in the VM
sentinel="" # the id of the container without a label, once this script created it

# Run a command in the VM as the user of the mode: the unprivileged one (rootless) or root. The sentinel
# must live in the store the contracts use: the store of rootless podman is the user's, not root's.
vm_as_mode() {
  if [[ $mode == rootless ]]; then vm_user "$engine" "$@"; else vm_root "$engine" "$@"; fi
}

# shellcheck disable=SC2329 # called by the EXIT trap
cleanup() {
  rm -rf "$work"
  # Only an id: a failed creation must not turn into a removal of whatever the output said.
  if [[ $sentinel =~ ^[0-9a-f]{12,64}$ ]]; then vm_as_mode "$engine" rm -f "$sentinel" >/dev/null 2>&1 || true; fi
  if ((touched)); then vm_root "$engine" rm -rf "$vmdir" >/dev/null 2>&1 || true; fi
}
trap cleanup EXIT

log "engine-contract: $engine line $line, $mode, run $run, mode $action"

# One test binary per package that holds a contract.
mapfile -t packages < <(git -C "$root" ls-files '*_contract_test.go' | xargs -n1 dirname | sort -u)
((${#packages[@]})) || die "no *_contract_test.go file in the repository"
binaries=()
for pkg in "${packages[@]}"; do
  bin=$(printf '%s' "$pkg" | tr '/' '-').test
  log "building $pkg"
  (cd "$root" && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c -trimpath -o "$work/$bin" "./$pkg")
  binaries+=("$bin")
done

if ((reset)); then
  "$DD_DIR/reset.sh" "$engine" "$line"
else
  vm_exists "$engine" || die "$(vm_name "$engine") does not exist; run create.sh"
fi
current=$(vm_root "$engine" cat /etc/dd/current)
[[ $current == "$line" ]] || die "$(vm_name "$engine") is on line $current, not $line; run reset.sh $engine $line"

# No -p: it must fail when the path exists (a symlink planted under /tmp, another run), and only a
# directory this script created is removed afterwards.
vm_root "$engine" mkdir -m 0755 "$vmdir"
touched=1
for f in "${binaries[@]}"; do
  vm_root "$engine" tee "$vmdir/$f" <"$work/$f" >/dev/null
  vm_root "$engine" chmod 0755 "$vmdir/$f"
done
vm_root "$engine" tee "$vmdir/versions.env" <"$DD_DIR/versions.env" >/dev/null
# The recorder writes here, as the user of the mode: the chown below hands it over when that user is not root.
if [[ $action == record ]]; then
  vm_root "$engine" mkdir -m 0700 "$vmdir/out"
fi
if [[ $mode == rootless ]]; then vm_root "$engine" chown -R "$DD_USER" "$vmdir"; fi

# The one image the contracts start, pinned by digest in versions.env, pulled here and nowhere else
# (the contracts run `--pull=never`).
vm_as_mode "$engine" pull "$CONTRACT_IMAGE" >/dev/null

# The sentinel: a container created without the label, the negative control of the cleanup. It is
# checked after the contracts and must still be there.
sentinel=$(vm_as_mode "$engine" run -d --pull=never --name "ddc-sentinel-$run" "$CONTRACT_IMAGE" sleep 3600) || sentinel=""
if [[ ! $sentinel =~ ^[0-9a-f]{12,64}$ ]]; then
  # The engine may have made the container all the same: remove it by its name before giving up.
  printed=$sentinel
  sentinel="ddc-sentinel-$run"
  vm_as_mode "$engine" rm -f "$sentinel" >/dev/null 2>&1 || true
  sentinel=""
  die "the sentinel was not created: $engine printed ${printed:-nothing}"
fi

# The sentinel is a resource of somebody else that a listing (`ps`) of the contracts shows, with an id that
# changes at every run: the contracts get it as DEVDESK_ENGINE_CONTRACT_SENTINEL and the recorder redacts it
# (id and name) to one stable role. Only the full id `run -d` prints is accepted, so the variable is 64 hex.
[[ $sentinel =~ ^[0-9a-f]{64}$ ]] || die "the sentinel id is not the 64 hexadecimal characters run -d prints: $sentinel"

# Explicit environment: nothing of the VM's own (a CI variable, a proxy) reaches a contract.
vars=(
  "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
  "DEVDESK_ENGINE_CONTRACT=1"
  "DEVDESK_ENGINE_CONTRACT_TOOL=$engine"
  "DEVDESK_ENGINE_CONTRACT_RUN=$run"
  "DEVDESK_ENGINE_CONTRACT_IMAGE=$CONTRACT_IMAGE"
  "DEVDESK_ENGINE_CONTRACT_SENTINEL=$sentinel"
  "DEVDESK_ENGINE_VERSIONS=$vmdir/versions.env"
)
# Only a recording sets these, and only here, in the environment of the test binary in the VM.
if [[ $action == record ]]; then
  vars+=("DEVDESK_FIXTURE_RECORDING=1" "DEVDESK_FIXTURE_OUT=$vmdir/out")
fi
status=0 passed=0 skipped=0 failed=0
for f in "${binaries[@]}"; do
  log "running $f in $(vm_name "$engine") ($mode)"
  out=$work/$f.out
  if [[ $mode == rootless ]]; then
    vm_user "$engine" env -i "${vars[@]}" "HOME=/home/$DD_USER" "XDG_RUNTIME_DIR=/run/user/$DD_UID"       "DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$DD_UID/bus"       "$vmdir/$f" -test.run '^TestContract' -test.count=1 -test.v -test.timeout 30m 2>&1 | tee "$out" || status=$?
  else
    vm_root "$engine" env -i "${vars[@]}" HOME=/root       "$vmdir/$f" -test.run '^TestContract' -test.count=1 -test.v -test.timeout 30m 2>&1 | tee "$out" || status=$?
  fi
  # Top-level results only: go test -v indents the subtests.
  passed=$((passed + $(grep -c '^--- PASS: ' "$out" || true)))
  skipped=$((skipped + $(grep -c '^--- SKIP: ' "$out" || true)))
  failed=$((failed + $(grep -c '^--- FAIL: ' "$out" || true)))
done

# The sentinel check. Whatever the contracts did, it comes next: nothing between them and it can leave
# the script, so a failed run still says whether the cleanup stayed within its label.
if ! vm_as_mode "$engine" inspect "$sentinel" >/dev/null 2>&1; then
  # Two different failures: the engine does not answer (the sentinel could not be checked), or it answers
  # and the sentinel is not there (the cleanup went beyond its label).
  if vm_as_mode "$engine" version >/dev/null 2>&1; then
    log "engine-contract: FAILED: the cleanup touched a container without the label (sentinel $sentinel, run $run)"
  else
    log "engine-contract: FAILED: $engine did not answer after the contracts, the sentinel $sentinel could not be checked (run $run)"
  fi
  status=1
fi
vm_as_mode "$engine" rm -f "$sentinel" >/dev/null 2>&1 || true
sentinel=""

log "engine-contract: $passed passed, $skipped skipped, $failed failed (run $run)"
if [[ $action == record ]] && ((status == 0 && passed > 0)); then
  log "engine-contract: bringing the recorded fixtures back from $vmdir/out"
  vm_root "$engine" tar -C "$vmdir/out" -cf - . | head -c "$((max_archive + 1))" >"$work/recorded-out" || die "the recorded fixtures could not be brought back (or the archive is larger than $max_archive bytes)"
  unpack_recorded
  log "engine-contract: importing on this machine${keep:+ into $keep}"
  (cd "$root" && go run ./internal/enginefixture/cmd/engine-fixture-import --from "$recorded" --require-denylist ${into_args[@]+"${into_args[@]}"}) || status=1
fi
if ((status)); then
  log "engine-contract: FAILED ($engine line $line, $mode, run $run, exit $status)"
elif ((passed == 0)); then
  # An engine outside N and N-1 is skipped in verify mode, and so is a contract that found no
  # fixture: the exit code stays 0, but nothing was checked and the message must not say it was.
  log "engine-contract: WARNING: nothing was done by $action: no contract passed ($skipped skipped); see the SKIP lines above"
else
  log "engine-contract: done ($engine line $line, $mode, run $run)"
fi
exit "$status"
