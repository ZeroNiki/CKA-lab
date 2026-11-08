#!/usr/bin/env bash
# Cluster addons that tasks rely on as environment (not as the subject of an exercise).
# Runs on controlplane as user vagrant, after all nodes have joined.
set -euo pipefail

METRICS_SERVER_VERSION="0.9.1"
LOCAL_PATH_VERSION="0.0.37"
ENVOY_GATEWAY_VERSION="1.10.0"
# Helm 3 matches the course material; Helm 4 changes some flags.
HELM_VERSION="3.22.1"

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
  curl -fsSL -o /tmp/helm.tgz "https://get.helm.sh/helm-v${HELM_VERSION}-linux-amd64.tar.gz"
  tar -C /tmp -xzf /tmp/helm.tgz linux-amd64/helm
  sudo install -m 0755 /tmp/linux-amd64/helm /usr/local/bin/helm
  rm -rf /tmp/helm.tgz /tmp/linux-amd64
fi

# etcdctl must match the etcd that kubeadm runs, for the backup/restore tasks.
if ! command -v etcdctl >/dev/null; then
  etcd_version="$(kubeadm config images list 2>/dev/null | awk -F: '/etcd/ {print $2}' | sed 's/-.*//')"
  curl -fsSL -o /tmp/etcd.tgz \
    "https://github.com/etcd-io/etcd/releases/download/v${etcd_version}/etcd-v${etcd_version}-linux-amd64.tar.gz"
  tar -C /tmp -xzf /tmp/etcd.tgz
  sudo install -m 0755 "/tmp/etcd-v${etcd_version}-linux-amd64/etcdctl" /usr/local/bin/etcdctl
  sudo install -m 0755 "/tmp/etcd-v${etcd_version}-linux-amd64/etcdutl" /usr/local/bin/etcdutl
  rm -rf /tmp/etcd.tgz "/tmp/etcd-v${etcd_version}-linux-amd64"
fi

if ! kubectl -n kube-system get deployment metrics-server >/dev/null 2>&1; then
  kubectl apply -f "https://github.com/kubernetes-sigs/metrics-server/releases/download/v${METRICS_SERVER_VERSION}/components.yaml"
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
