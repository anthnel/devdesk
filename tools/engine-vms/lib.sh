#!/usr/bin/env bash
# Shared helpers for the engine-vms scripts. Sourced, never run.
# shellcheck disable=SC2034  # variables are used by the scripts that source this file
# Everything is done through `incus --project "$DD_PROJECT"` on the Incus host;
# no instance, profile, volume or network outside that project is touched.

DD_HOST=${DD_ENGINE_VMS_HOST:-jakku.home.arpa}   # SSH host running Incus; "local" runs incus here
DD_PROJECT=${DD_ENGINE_VMS_PROJECT:-devdesk-engine-tests}
DD_POOL=${DD_ENGINE_VMS_POOL:-incus_pool}
DD_NETWORK=${DD_ENGINE_VMS_NETWORK:-incusbr0}
DD_IMAGE=${DD_ENGINE_VMS_IMAGE:-images:ubuntu/24.04}
DD_PROFILE=dd-engine
DD_USER="dd"
DD_UID=2000
DD_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
ENGINES=(docker podman)

log() { printf '[engine-vms] %s\n' "$*" >&2; }
die() { log "ERROR: $*"; exit 1; }

# The project name is the only guard between these scripts and the rest of the
# host: refuse anything that is not a dedicated project.
case $DD_PROJECT in
  devdesk-engine-tests*) ;;
  *) die "DD_ENGINE_VMS_PROJECT must start with devdesk-engine-tests (got $DD_PROJECT)" ;;
esac

# Quote each argument for the remote shell; stdin is passed through.
host_run() {
  if [[ $DD_HOST == local ]]; then "$@"; return; fi
  local q=() a
  for a in "$@"; do q+=("$(printf '%q' "$a")"); done
  ssh -o BatchMode=yes -o ConnectTimeout=10 "$DD_HOST" "${q[*]}"
}
inc() { host_run incus --project "$DD_PROJECT" "$@"; }

vm_name() { printf 'dd-%s' "$1"; }
vm_exists() { inc info "$(vm_name "$1")" >/dev/null 2>&1; }
vm_status() { inc list "^$(vm_name "$1")\$" -f csv -c s; }
has_snapshot() { inc snapshot list "$(vm_name "$1")" -f csv -c n | grep -qx "$2"; }

wait_agent() {
  local vm i; vm=$(vm_name "$1")
  for _ in $(seq 1 60); do
    if inc exec "$vm" -- true >/dev/null 2>&1; then return 0; fi
    sleep 3
  done
  die "$vm: incus agent not reachable after 180 s"
}

# Run a command in a VM as root.
vm_root() { local e=$1; shift; inc exec "$(vm_name "$e")" -- "$@"; }
# Run a command in a VM as the unprivileged user, with its own user manager.
vm_user() {
  local e=$1; shift
  inc exec --cwd "/home/$DD_USER" "$(vm_name "$e")" -- runuser -u "$DD_USER" -- env \
    "HOME=/home/$DD_USER" "XDG_RUNTIME_DIR=/run/user/$DD_UID" \
    "DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$DD_UID/bus" \
    "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin" "$@"
}

check_engine() {
  case $1 in docker | podman) ;; *) die "unknown engine: $1 (docker|podman)" ;; esac
}
engines_of() { if [[ $1 == all ]]; then echo "${ENGINES[@]}"; else check_engine "$1"; echo "$1"; fi; }
check_version() { case $1 in n | n1) ;; *) die "unknown version: $1 (n|n1)" ;; esac; }

# shellcheck source=versions.env
. "$DD_DIR/versions.env"
