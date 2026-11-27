#!/usr/bin/env bash
# Print the installed versions in each VM and fail if they differ from versions.env.
# Usage: verify.sh [docker|podman|all] [--smoke] [--matrix]
#   --smoke   also run a container and a minimal kind cluster (created, then deleted)
#   --matrix  check both lines (n and n1) by restoring the clean snapshot and selecting each line first;
#             without it the line currently installed in the VM is checked
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
target=all smoke=0 matrix=0
for a in "$@"; do
  case $a in --smoke) smoke=1 ;; --matrix) matrix=1 ;; *) target=$a ;; esac
done
mapfile -t todo < <(engines_of "$target" | tr ' ' '\n')
fail=0
bad() { log "MISMATCH: $*"; fail=1; }
expect() { [[ $2 == "$3" ]] || bad "$1: got '$2', expected '$3'"; }

check_vm() { # engine
  local e=$1 cur U
  cur=$(vm_root "$e" cat /etc/dd/current)
  U=${cur^^}
  local -n dockerv="DOCKER_${U}_VERSION" podmanv="PODMAN_${U}_VERSION" kindv="KIND_${U}_VERSION" node="KIND_${U}_NODE"
  log "== $(vm_name "$e") line $cur"
  log "kernel: $(vm_root "$e" uname -r); cgroup fs: $(vm_root "$e" stat -fc %T /sys/fs/cgroup)"
  expect "cgroup" "$(vm_root "$e" stat -fc %T /sys/fs/cgroup)" cgroup2fs
  local deleg
  deleg=$(vm_root "$e" cat "/sys/fs/cgroup/user.slice/user-$DD_UID.slice/user@$DD_UID.service/cgroup.controllers")
  log "controllers delegated to $DD_USER: $deleg"
  for c in cpu memory io pids; do [[ " $deleg " == *" $c "* ]] || bad "controller $c not delegated"; done
  expect "kubectl" "$(vm_root "$e" kubectl version --client -o json | jq -r .clientVersion.gitVersion)" "v$KUBECTL_VERSION"
  expect "kind" "$(vm_root "$e" kind version | sed -E 's/^kind v([0-9.]+).*/\1/')" "$kindv"
  log "kind: $(vm_root "$e" kind version)"
  case $e in
    docker)
      expect "docker server" "$(vm_root "$e" docker version -f '{{.Server.Version}}')" "$dockerv"
      expect "docker client" "$(vm_root "$e" docker version -f '{{.Client.Version}}')" "$dockerv"
      ;;
    podman)
      expect "podman" "$(vm_user "$e" podman version -f '{{.Server.Version}}')" "$podmanv"
      log "podman rootless: $(vm_user "$e" podman info -f '{{.Host.Security.Rootless}}'), cgroup manager $(vm_user "$e" podman info -f '{{.Host.CgroupManager}}')"
      ;;
  esac
  ((smoke)) || return 0
  # After a restore the engine may still be starting.

  for _ in $(seq 1 30); do
    if [[ $e == docker ]]; then vm_root "$e" docker info >/dev/null 2>&1 && break; else vm_user "$e" podman info >/dev/null 2>&1 && break; fi
    sleep 2
  done
  local run=vm_root provider=docker kenv=()
  if [[ $e == podman ]]; then run=vm_user provider=podman; kenv=(KIND_EXPERIMENTAL_PROVIDER=podman); fi
  if [[ $e == docker ]]; then vm_root "$e" docker run --rm hello-world | grep -q 'Hello from Docker' || bad "docker hello-world"
  else vm_user "$e" podman run --rm quay.io/podman/hello | grep -qi hello || bad "podman rootless run"; fi
  local name=dd-smoke
  log "kind smoke ($provider, node ${node%%@*})"
  # The first start after a snapshot restore is slow (cold disk cache) and can make
  # kubeadm time out; one retry on a warm VM is enough.
  local out try
  local ok=0
  for try in 1 2; do
    if out=$("$run" "$e" env "${kenv[@]}" kind create cluster --name "$name" --image "$node" --wait 240s 2>&1); then ok=1; break; fi
    log "kind create attempt $try failed on $provider"
    "$run" "$e" env "${kenv[@]}" kind delete cluster --name "$name" >/dev/null 2>&1 || true
  done
  ((ok)) || { bad "kind create on $provider"; printf '%s\n' "$out" | grep -E '^(ERROR|error)' | head -4 >&2; }
  "$run" "$e" env "${kenv[@]}" kubectl --context "kind-$name" get nodes --no-headers >&2 || bad "kubectl get nodes"
  "$run" "$e" env "${kenv[@]}" kind delete cluster --name "$name" >/dev/null 2>&1 || bad "kind delete on $provider"
}

for e in "${todo[@]}"; do
  vm_exists "$e" || die "$(vm_name "$e") does not exist; run create.sh"
  [[ $(vm_status "$e") == RUNNING ]] || { inc start "$(vm_name "$e")"; wait_agent "$e"; }
  if ((matrix)); then
    for v in n1 n; do "$DD_DIR/reset.sh" "$e" "$v" >&2; check_vm "$e"; done
  else
    check_vm "$e"
  fi
done
((fail == 0)) || die "verification failed"
log "all versions match versions.env"
