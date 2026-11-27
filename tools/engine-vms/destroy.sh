#!/usr/bin/env bash
# Delete the engine test VMs, their profile and the dedicated project (if then empty).
# Usage: destroy.sh [--yes]. Only touches resources inside the devdesk-engine-tests project.
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
if [[ ${1:-} != --yes ]]; then
  read -r -p "Delete the VMs and the project $DD_PROJECT on $DD_HOST? [y/N] " ans
  [[ $ans == y || $ans == Y ]] || die "aborted"
fi
host_run incus project show "$DD_PROJECT" >/dev/null 2>&1 || { log "project $DD_PROJECT absent, nothing to do"; exit 0; }
for e in "${ENGINES[@]}"; do
  if vm_exists "$e"; then log "deleting $(vm_name "$e")"; inc delete "$(vm_name "$e")" --force; fi
done
others=$(inc list -f csv -c n)
[[ -z $others ]] || die "project $DD_PROJECT still holds instances not created here ($others); left in place"
inc profile delete "$DD_PROFILE" >/dev/null 2>&1 || true
for img in $(inc image list -f csv -c f); do inc image delete "$img"; done
host_run incus project delete "$DD_PROJECT"
log "project $DD_PROJECT deleted"
