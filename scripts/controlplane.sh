#!/usr/bin/env bash
# kubeadm init + Calico on the control-plane node.
set -euo pipefail

TOKEN="${1:?usage: controlplane.sh TOKEN CONTROLPLANE_IP}"
CONTROLPLANE_IP="${2:?usage: controlplane.sh TOKEN CONTROLPLANE_IP}"

CALICO_VERSION="3.33.0"
POD_CIDR="10.244.0.0/16"

if [[ ! -f /etc/kubernetes/admin.conf ]]; then
  # The advertise address puts the private IP into the API server certificate,
  # so the trainer on the host can reach 192.168.56.10:6443.
  kubeadm init \
    --apiserver-advertise-address="${CONTROLPLANE_IP}" \
    --token "${TOKEN}" \
    --token-ttl 0 \
    --pod-network-cidr="${POD_CIDR}" \
    --node-name controlplane
fi

install -d -o vagrant -g vagrant -m 0700 /home/vagrant/.kube
install -o vagrant -g vagrant -m 0600 /etc/kubernetes/admin.conf /home/vagrant/.kube/config

export KUBECONFIG=/etc/kubernetes/admin.conf

if ! kubectl get namespace tigera-operator >/dev/null 2>&1; then
  kubectl create -f "https://raw.githubusercontent.com/projectcalico/calico/v${CALICO_VERSION}/manifests/operator-crds.yaml"
  kubectl create -f "https://raw.githubusercontent.com/projectcalico/calico/v${CALICO_VERSION}/manifests/tigera-operator.yaml"
fi
if ! kubectl get installation default >/dev/null 2>&1; then
  kubectl wait --for condition=established --timeout=120s crd/installations.operator.tigera.io
  kubectl apply -f - <<EOF
apiVersion: operator.tigera.io/v1
kind: Installation
metadata:
  name: default
spec:
  calicoNetwork:
    # Use the kubelet --node-ip (private network), not the first NIC (vagrant NAT).
    nodeAddressAutodetectionV4:
      kubernetes: NodeInternalIP
    ipPools:
      - cidr: ${POD_CIDR}
        encapsulation: VXLANCrossSubnet
        natOutgoing: Enabled
        nodeSelector: all()
EOF
fi

# Shell conveniences similar to the exam environment.
if ! grep -q 'alias k=kubectl' /home/vagrant/.bashrc; then
  cat >>/home/vagrant/.bashrc <<'EOF'
source <(kubectl completion bash)
alias k=kubectl
complete -o default -F __start_kubectl k
EOF
fi
