#!/usr/bin/env bash
# Cluster addons that tasks rely on as environment (not as the subject of an exercise).
# Runs on controlplane as user vagrant, after all nodes have joined.
set -euo pipefail

METRICS_SERVER_VERSION="0.9.1"
LOCAL_PATH_VERSION="0.0.37"
ENVOY_GATEWAY_VERSION="1.10.0"
# Helm 3 matches the course material; Helm 4 changes some flags.
HELM_VERSION="3.22.1"

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

echo "Waiting for 3 Ready nodes..."
deadline=$((SECONDS + 600))
until [[ "$(kubectl get nodes --no-headers 2>/dev/null | awk '$2 == "Ready"' | wc -l)" -eq 3 ]]; do
  if ((SECONDS > deadline)); then
    echo "timeout: not all nodes are Ready" >&2
    kubectl get nodes >&2 || true
    exit 1
  fi
  sleep 5
done

if ! command -v helm >/dev/null; then
  curl -fsSL --retry 5 --retry-delay 5 --retry-all-errors -o /tmp/helm.tgz "https://get.helm.sh/helm-v${HELM_VERSION}-linux-amd64.tar.gz"
  tar -C /tmp -xzf /tmp/helm.tgz linux-amd64/helm
  sudo install -m 0755 /tmp/linux-amd64/helm /usr/local/bin/helm
  rm -rf /tmp/helm.tgz /tmp/linux-amd64
fi

# etcdctl must match the etcd that kubeadm runs, for the backup/restore tasks.
if ! command -v etcdctl >/dev/null; then
  etcd_version="$(kubeadm config images list 2>/dev/null | awk -F: '/etcd/ {print $2}' | sed 's/-.*//')"
  fetch_release etcd-io/etcd "v${etcd_version}" "etcd-v${etcd_version}-linux-amd64.tar.gz" /tmp/etcd.tgz
  tar -C /tmp -xzf /tmp/etcd.tgz
  sudo install -m 0755 "/tmp/etcd-v${etcd_version}-linux-amd64/etcdctl" /usr/local/bin/etcdctl
  sudo install -m 0755 "/tmp/etcd-v${etcd_version}-linux-amd64/etcdutl" /usr/local/bin/etcdutl
  rm -rf /tmp/etcd.tgz "/tmp/etcd-v${etcd_version}-linux-amd64"
fi

if ! kubectl -n kube-system get deployment metrics-server >/dev/null 2>&1; then
  fetch_release kubernetes-sigs/metrics-server "v${METRICS_SERVER_VERSION}" components.yaml /tmp/metrics-server.yaml
  kubectl apply -f /tmp/metrics-server.yaml
  # kubelet serving certs are self-signed in a kubeadm lab.
  kubectl -n kube-system patch deployment metrics-server --type=json \
    -p '[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--kubelet-insecure-tls"}]'
fi

if ! kubectl get storageclass local-path >/dev/null 2>&1; then
  kubectl apply -f "https://raw.githubusercontent.com/rancher/local-path-provisioner/v${LOCAL_PATH_VERSION}/deploy/local-path-storage.yaml"
fi

if ! helm -n envoy-gateway-system status eg >/dev/null 2>&1; then
  # The chart also installs the Gateway API CRDs.
  helm install eg oci://docker.io/envoyproxy/gateway-helm \
    --version "v${ENVOY_GATEWAY_VERSION}" \
    -n envoy-gateway-system --create-namespace --wait --timeout 5m
fi
if ! kubectl get gatewayclass eg >/dev/null 2>&1; then
  kubectl apply -f - <<'EOF'
apiVersion: gateway.networking.k8s.io/v1
kind: GatewayClass
metadata:
  name: eg
spec:
  controllerName: gateway.envoyproxy.io/gatewayclass-controller
EOF
fi

echo "Addons ready."
