#!/usr/bin/env bash
# Runs as root INSIDE a VM (pushed by create.sh). Idempotent.
# Usage: provision.sh <docker|podman>; needs /etc/dd/versions.env and /usr/local/bin/dd-use.
set -euo pipefail
engine=$1
export DEBIAN_FRONTEND=noninteractive
# shellcheck disable=SC1091
. /etc/dd/versions.env
DD_USER="dd"
DD_UID=2000

apt-get update -qq
apt-get install -y -qq ca-certificates curl gnupg jq uidmap iptables slirp4netns passt \
  dbus-user-session conntrack >/dev/null

# Unprivileged user with a lingering systemd user manager (rootless runs, kind rootless).
id "$DD_USER" >/dev/null 2>&1 || useradd -m -u "$DD_UID" -s /bin/bash "$DD_USER"
loginctl enable-linger "$DD_USER"

# Delegate the cgroup v2 controllers kind needs to user services (rootless Podman, kind).
install -d /etc/systemd/system/user@.service.d
cat >/etc/systemd/system/user@.service.d/delegate.conf <<'CONF'
[Service]
Delegate=cpu cpuset io memory pids
CONF
systemctl daemon-reload
systemctl restart "user@$DD_UID.service"

cat >/etc/sysctl.d/99-dd-engine.conf <<'CONF'
fs.inotify.max_user_watches=524288
fs.inotify.max_user_instances=512
CONF
printf '%s\n' overlay br_netfilter ip_tables iptable_nat ip6_tables ip6table_nat >/etc/modules-load.d/dd-engine.conf
sysctl --system >/dev/null
systemctl restart systemd-modules-load.service || true

# Downloads are checked against the pinned sha256 and cached for dd-use.
cache=/opt/dd/cache
install -d "$cache"
fetch() { # url dest sha256
  if [[ -f $2 ]] && echo "$3  $2" | sha256sum -c --quiet 2>/dev/null; then return; fi
  curl -fsSL --retry 3 -o "$2" "$1"
  echo "$3  $2" | sha256sum -c --quiet || { rm -f "$2"; echo "checksum mismatch: $1" >&2; exit 1; }
}
fetch "https://dl.k8s.io/release/v$KUBECTL_VERSION/bin/linux/amd64/kubectl" "$cache/kubectl-$KUBECTL_VERSION" "$KUBECTL_SHA256"
install -m 0755 "$cache/kubectl-$KUBECTL_VERSION" /usr/local/bin/kubectl
fetch "https://github.com/kubernetes-sigs/kind/releases/download/v$KIND_N_VERSION/kind-linux-amd64" "$cache/kind-$KIND_N_VERSION" "$KIND_N_SHA256"
fetch "https://github.com/kubernetes-sigs/kind/releases/download/v$KIND_N1_VERSION/kind-linux-amd64" "$cache/kind-$KIND_N1_VERSION" "$KIND_N1_SHA256"

case $engine in
  docker)
    install -d -m 0755 /etc/apt/keyrings
    [[ -f /etc/apt/keyrings/docker.asc ]] || curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
    echo "deb [arch=amd64 signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu noble stable" >/etc/apt/sources.list.d/docker.list
    apt-get update -qq
    usermod -aG docker "$DD_USER" 2>/dev/null || true
    ;;
  podman)
    fetch "https://github.com/mgoltzsche/podman-static/releases/download/v$PODMAN_N_VERSION/podman-linux-amd64.tar.gz" "$cache/podman-$PODMAN_N_VERSION.tgz" "$PODMAN_N_SHA256"
    fetch "https://github.com/mgoltzsche/podman-static/releases/download/v$PODMAN_N1_VERSION/podman-linux-amd64.tar.gz" "$cache/podman-$PODMAN_N1_VERSION.tgz" "$PODMAN_N1_SHA256"
    # Second Compose provider, next to podman-compose (never replaces it). Hard failure on a sum mismatch.
    fetch "https://github.com/docker/compose/releases/download/v$DOCKER_COMPOSE_VERSION/docker-compose-linux-x86_64" "$cache/docker-compose-$DOCKER_COMPOSE_VERSION" "$DOCKER_COMPOSE_SHA256"
    install -m 0755 "$cache/docker-compose-$DOCKER_COMPOSE_VERSION" /usr/local/bin/docker-compose
    # Which provider `podman compose` runs is chosen per call with PODMAN_COMPOSE_PROVIDER=<path>
    # (or compose_providers in containers.conf). Nothing is set VM-wide: with both installed and
    # no choice, podman tries docker-compose first, so measurements always name the provider.
    grep -q "^$DD_USER:" /etc/subuid || echo "$DD_USER:200000:65536" >>/etc/subuid
    grep -q "^$DD_USER:" /etc/subgid || echo "$DD_USER:200000:65536" >>/etc/subgid
    ;;
esac
echo "$engine" >/etc/dd/engine
echo "provisioned $engine"
