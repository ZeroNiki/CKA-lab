#!/usr/bin/env bash
# Base setup for every node: kernel prerequisites, containerd, kubeadm/kubelet/kubectl.
# Idempotent so `vagrant provision` can be re-run.
set -euo pipefail

NODE_IP="${1:?usage: common.sh NODE_IP}"

# Single place to change versions.
K8S_VERSION="1.37"
CONTAINERD_VERSION="2.4.2"
RUNC_VERSION="1.5.2"

export DEBIAN_FRONTEND=noninteractive

# Some networks time out on github.com/<repo>/releases/download/ while the API path to the
# same asset works. Try the direct URL first: the API allows only 60 anonymous calls per hour per IP.
# Kept inline in each script: shell provisioners upload the script alone, /vagrant may be stale.
fetch_release() {
  local repo="$1" tag="$2" asset="$3" dest="$4" id
  if curl -fsSL --retry 2 --retry-delay 3 --max-time 120 -o "$dest" \
    "https://github.com/${repo}/releases/download/${tag}/${asset}"; then
    return 0
  fi
  echo "direct download of ${asset} failed, trying the GitHub API" >&2
  id="$(curl -fsSL --retry 5 --retry-delay 5 --retry-all-errors \
    "https://api.github.com/repos/${repo}/releases/tags/${tag}" |
    jq -er --arg name "$asset" '.assets[] | select(.name == $name) | .id')"
  curl -fsSL --retry 5 --retry-delay 5 --retry-all-errors -H 'Accept: application/octet-stream' -o "$dest" \
    "https://api.github.com/repos/${repo}/releases/assets/${id}"
}

# kubelet refuses to run with swap on by default.
swapoff -a
sed -i '/\sswap\s/ s/^[^#]/#&/' /etc/fstab

cat >/etc/modules-load.d/k8s.conf <<'EOF'
overlay
br_netfilter
EOF
modprobe overlay
modprobe br_netfilter

cat >/etc/sysctl.d/k8s.conf <<'EOF'
net.bridge.bridge-nf-call-iptables  = 1
net.bridge.bridge-nf-call-ip6tables = 1
net.ipv4.ip_forward                 = 1
EOF
sysctl --system >/dev/null

# generic/* boxes enable DNSSEC validation in systemd-resolved; networks that strip
# DNSSEC records make every lookup fail, so trust the upstream resolver instead.
mkdir -p /etc/systemd/resolved.conf.d
cat >/etc/systemd/resolved.conf.d/lab.conf <<'EOF'
[Resolve]
DNSSEC=no
EOF
systemctl restart systemd-resolved
for _ in $(seq 1 15); do
  getent hosts pkgs.k8s.io >/dev/null && break
  sleep 2
done

apt-get update -qq
apt-get install -y -qq apt-transport-https ca-certificates curl gpg jq >/dev/null

# Kubernetes 1.36+ needs containerd 2.x; Ubuntu 22.04 ships 1.x, so install upstream binaries.
if ! /usr/local/bin/containerd --version 2>/dev/null | grep -q "v${CONTAINERD_VERSION}"; then
  fetch_release containerd/containerd "v${CONTAINERD_VERSION}" \
    "containerd-${CONTAINERD_VERSION}-linux-amd64.tar.gz" /tmp/containerd.tgz
  tar -C /usr/local -xzf /tmp/containerd.tgz
  curl -fsSL --retry 5 --retry-delay 5 --retry-all-errors -o /etc/systemd/system/containerd.service \
    "https://raw.githubusercontent.com/containerd/containerd/v${CONTAINERD_VERSION}/containerd.service"
  rm -f /tmp/containerd.tgz
fi
if ! /usr/local/sbin/runc --version 2>/dev/null | grep -q "${RUNC_VERSION}"; then
  fetch_release opencontainers/runc "v${RUNC_VERSION}" runc.amd64 /usr/local/sbin/runc
  chmod 0755 /usr/local/sbin/runc
fi

mkdir -p /etc/containerd
if [[ ! -f /etc/containerd/config.toml ]]; then
  /usr/local/bin/containerd config default >/etc/containerd/config.toml
fi
# kubeadm configures kubelet with the systemd cgroup driver; the runtime must match.
sed -i 's/SystemdCgroup = false/SystemdCgroup = true/' /etc/containerd/config.toml
if ! grep -q 'SystemdCgroup = true' /etc/containerd/config.toml; then
  echo "SystemdCgroup = true not found in /etc/containerd/config.toml" >&2
  exit 1
fi
systemctl daemon-reload
systemctl enable --now containerd >/dev/null
systemctl restart containerd

if [[ ! -f /etc/apt/keyrings/kubernetes-apt-keyring.gpg ]]; then
  mkdir -p /etc/apt/keyrings
  curl -fsSL --retry 5 --retry-delay 5 --retry-all-errors "https://pkgs.k8s.io/core:/stable:/v${K8S_VERSION}/deb/Release.key" |
    gpg --dearmor -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg
fi
echo "deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] https://pkgs.k8s.io/core:/stable:/v${K8S_VERSION}/deb/ /" \
  >/etc/apt/sources.list.d/kubernetes.list

if ! dpkg -s kubeadm >/dev/null 2>&1; then
  apt-get update -qq
  apt-get install -y -qq kubelet kubeadm kubectl >/dev/null
  apt-mark hold kubelet kubeadm kubectl >/dev/null
fi

# Without --node-ip kubelet advertises the NAT management address, which other nodes cannot reach.
echo "KUBELET_EXTRA_ARGS=--node-ip=${NODE_IP}" >/etc/default/kubelet
systemctl enable kubelet >/dev/null

# crictl talks to containerd without extra flags (handy in Troubleshooting tasks).
cat >/etc/crictl.yaml <<'EOF'
runtime-endpoint: unix:///run/containerd/containerd.sock
image-endpoint: unix:///run/containerd/containerd.sock
EOF
