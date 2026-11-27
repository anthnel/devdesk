#!/usr/bin/env bash
# Run the engine contract tests (the *_contract_test.go files) in a test VM (OC-09a1, DEVDESK-806).
# Usage: contract.sh verify <docker|podman> <n|n1> [--mode rootful|rootless] [--no-reset]
#   verify     run the contracts and compare what they see to the recorded fixtures
#   --mode     docker is rootful only; podman defaults to rootless (the mode kind and most users have)
#   --no-reset keep the VM as it is instead of restoring its clean snapshot first (faster to iterate;
#              the line installed in the VM must still be the one asked for)
# The gate comes first and applies on this machine: without DEVDESK_ENGINE_CONTRACT=1 in the
# environment nothing happens (exit 0); with CI, GITHUB_ACTIONS, GITLAB_CI, BUILDKITE or TF_BUILD set
# nothing happens either (exit 1), even with the variable. Set the variable for the one command, as
# in `DEVDESK_ENGINE_CONTRACT=1 mise run engine-contract -- docker n`, and do not export it: an
# exported variable opens the gate for every later `go test` of the shell. The Go tests apply the same
# gate again in the VM (internal/testutil/enginecontract).
# Exit codes: 0 done or skipped, 1 refused or failed, 2 invalid arguments.
#
# What it does, in order: builds one test binary per package that has a contract (linux/amd64,
# -trimpath), restores the VM (unless --no-reset), copies the binaries and versions.env to
# /tmp/ddc-<run> in the VM, runs them there with an explicit environment, under root (rootful) or the
# unprivileged user (rootless), then removes the directory, on success or not. Nothing is copied back.
# Variables: the DD_ENGINE_VMS_* ones of lib.sh.
set -euo pipefail

usage() {
  echo "usage: contract.sh verify <docker|podman> <n|n1> [--mode rootful|rootless] [--no-reset]" >&2
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
[[ $action == verify ]] || usage
case $engine in docker | podman) ;; *) usage ;; esac
case $line in n | n1) ;; *) usage ;; esac
shift 3
mode="" reset=1
while (($#)); do
  case $1 in
    --mode) (($# >= 2)) || usage; mode=$2; shift 2 ;;
    --no-reset) reset=0; shift ;;
    *) usage ;;
  esac
done
if [[ -z $mode ]]; then
  if [[ $engine == podman ]]; then mode=rootless; else mode=rootful; fi
fi
case $mode in rootful | rootless) ;; *) usage ;; esac
if [[ $engine == docker && $mode == rootless ]]; then
  echo "contract.sh: the docker VM is rootful; --mode rootless needs podman" >&2
  exit 2
fi

. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
root=$(cd "$DD_DIR/../.." && pwd)

# Same shape as NewRunID in enginecontract: the day in UTC, then four random bytes.
run=$(date -u +%Y%m%d)-$(od -An -N4 -tx1 /dev/urandom | tr -d ' \n')
vmdir=/tmp/ddc-$run
work=$(mktemp -d)
touched=0 # set once this script created the directory in the VM
# shellcheck disable=SC2329 # called by the EXIT trap
cleanup() {
  rm -rf "$work"
  if ((touched)); then vm_root "$engine" rm -rf "$vmdir" >/dev/null 2>&1 || true; fi
}
trap cleanup EXIT

log "engine-contract: $engine line $line, $mode, run $run, mode verify"

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
if [[ $mode == rootless ]]; then vm_root "$engine" chown -R "$DD_USER" "$vmdir"; fi

# Explicit environment: nothing of the VM's own (a CI variable, a proxy) reaches a contract.
vars=(
  "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
  "DEVDESK_ENGINE_CONTRACT=1"
  "DEVDESK_ENGINE_CONTRACT_TOOL=$engine"
  "DEVDESK_ENGINE_CONTRACT_RUN=$run"
  "DEVDESK_ENGINE_VERSIONS=$vmdir/versions.env"
)
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

log "engine-contract: $passed passed, $skipped skipped, $failed failed (run $run)"
if ((status)); then
  log "engine-contract: FAILED ($engine line $line, $mode, run $run, exit $status)"
elif ((passed == 0)); then
  # An engine outside N and N-1 is skipped in verify mode, and so is a contract that found no
  # fixture: the exit code stays 0, but nothing was checked and the message must not say it was.
  log "engine-contract: WARNING: nothing was verified: no contract passed ($skipped skipped); see the SKIP lines above"
else
  log "engine-contract: done ($engine line $line, $mode, run $run)"
fi
exit "$status"
