#!/usr/bin/env bash
# Restore a VM to its clean snapshot (line n), then switch to n1 if asked (after a campaign) and optionally stop it.
# Usage: reset.sh [docker|podman|all] [n|n1] [--stop]   (default: all n)
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
target=all version=n stop=0 pos=0
for a in "$@"; do
  case $a in
    --stop) stop=1 ;;
    *) if ((pos == 0)); then target=$a; else version=$a; fi; pos=$((pos + 1)) ;;
  esac
done
check_version "$version"
mapfile -t todo < <(engines_of "$target" | tr ' ' '\n')
for e in "${todo[@]}"; do
  vm=$(vm_name "$e")
  vm_exists "$e" || die "$vm does not exist; run create.sh"
  has_snapshot "$e" clean || die "$vm has no clean snapshot; run create.sh"
  log "$vm: restoring clean (line n)"
  inc stop "$vm" --force >/dev/null 2>&1 || true
  inc snapshot restore "$vm" clean
  inc start "$vm"
  wait_agent "$e"
  if [[ $version == n1 ]]; then vm_root "$e" dd-use n1; fi
  if ((stop)); then inc stop "$vm"; log "$vm left stopped"; fi
done
