#!/usr/bin/env bash
# Create (or complete) the engine test VMs in the dedicated Incus project.
# Usage: create.sh [docker|podman|all]   (default: all). Idempotent.
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
mapfile -t todo < <(engines_of "${1:-all}" | tr ' ' '\n')

# Project and profile: created only if absent, never altered otherwise.
if ! host_run incus project show "$DD_PROJECT" >/dev/null 2>&1; then
  log "creating project $DD_PROJECT"
  host_run incus project create "$DD_PROJECT" \
    -c features.profiles=true -c features.images=true \
    -c features.storage.volumes=true -c features.networks=false \
    -c user.devdesk=engine-tests
fi
if ! inc profile show "$DD_PROFILE" >/dev/null 2>&1; then
  log "creating profile $DD_PROFILE (2 vCPU, 3 GiB, 30 GiB disk per VM)"
  inc profile create "$DD_PROFILE"
  inc profile set "$DD_PROFILE" limits.cpu=2 limits.memory=3GiB user.devdesk=engine-tests
  inc profile device add "$DD_PROFILE" root disk path=/ pool="$DD_POOL" size=30GiB
  inc profile device add "$DD_PROFILE" eth0 nic network="$DD_NETWORK" name=eth0
fi

for e in "${todo[@]}"; do
  vm=$(vm_name "$e")
  if ! vm_exists "$e"; then
    log "launching $vm from $DD_IMAGE"
    inc launch "$DD_IMAGE" "$vm" --vm -p "$DD_PROFILE" -c user.devdesk=engine-tests
  elif [[ $(vm_status "$e") != RUNNING ]]; then
    inc start "$vm"
  fi
  wait_agent "$e"
  if has_snapshot "$e" clean; then
    log "$vm already provisioned with its clean snapshot; use reset.sh to restore"
    continue
  fi
  log "$vm: provisioning (takes a few minutes)"
  vm_root "$e" mkdir -p /etc/dd
  inc file push - "$vm/etc/dd/versions.env" <"$DD_DIR/versions.env"
  inc file push --mode 0755 - "$vm/usr/local/bin/dd-use" <"$DD_DIR/vm/dd-use.sh"
  vm_root "$e" bash -s -- "$e" <"$DD_DIR/vm/provision.sh"
  # A ZFS volume can only be restored to its latest snapshot, so there is one
  # baseline snapshot, taken on line n; reset.sh switches to n1 after restoring.
  vm_root "$e" dd-use n
  vm_root "$e" sync
  inc stop "$vm"
  inc snapshot create "$vm" clean
  inc start "$vm"
  wait_agent "$e"
  log "$vm ready on line n; snapshot clean taken"
done
