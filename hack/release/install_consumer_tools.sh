#!/usr/bin/env bash
set -Eeuo pipefail

destination=${1:?usage: install_consumer_tools.sh DESTINATION}
"$(dirname "${BASH_SOURCE[0]}")/install_helm.sh" "${destination}"
curl -fsSLo "${destination}/kind" https://kind.sigs.k8s.io/dl/v0.32.0/kind-linux-amd64
echo "50030de23cf40a18505f20426f6a8506bedf13c6e509244bd1fa9463721b0f54  ${destination}/kind" | shasum -a 256 -c
curl -fsSLo "${destination}/kubectl" https://dl.k8s.io/release/v1.37.0/bin/linux/amd64/kubectl
echo "6129359f4e1f3848a5572ccb0b26cf28b8ca08cef38c95a765b2f64a2c961a2f  ${destination}/kubectl" | shasum -a 256 -c
chmod +x "${destination}/kind" "${destination}/kubectl"
