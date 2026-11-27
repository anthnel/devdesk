#!/usr/bin/env bash
# Installed as /usr/local/bin/dd-use in each VM. Switches the engine and kind to
# one line of the matrix: dd-use n | n1. Idempotent; run as root.
set -euo pipefail
v=${1:-}
case $v in n | n1) ;; *) echo "usage: dd-use n|n1" >&2; exit 2 ;; esac
# shellcheck disable=SC1091
. /etc/dd/versions.env
engine=$(cat /etc/dd/engine)
upper=${v^^}
pick() { local n="${1}_${upper}${2:+_$2}"; echo "${!n}"; } # pick PODMAN VERSION -> $PODMAN_N_VERSION

case $engine in
  docker)
    ver=$(pick DOCKER)
    apt-mark unhold docker-ce docker-ce-cli >/dev/null 2>&1 || true
    DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --allow-downgrades \
      "docker-ce=$ver" "docker-ce-cli=$ver" containerd.io >/dev/null
    apt-mark hold docker-ce docker-ce-cli >/dev/null
    systemctl enable --now docker >/dev/null
    ;;
  podman)
    ver=$(pick PODMAN VERSION)
    tar -xzf "/opt/dd/cache/podman-$ver.tgz" --strip-components=1 -C /
    ;;
esac
kv=$(pick KIND VERSION)
install -m 0755 "/opt/dd/cache/kind-$kv" /usr/local/bin/kind
echo "$v" >/etc/dd/current
echo "dd-use: $engine on line $v"
