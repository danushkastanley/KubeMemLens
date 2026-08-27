#!/usr/bin/env bash
set -Eeuo pipefail

destination=${1:?usage: install_helm.sh DESTINATION}
mkdir -p "${destination}"
curl -fsSLo "${destination}/helm.tar.gz" https://get.helm.sh/helm-v3.18.4-linux-amd64.tar.gz
echo "f8180838c23d7c7d797b208861fecb591d9ce1690d8704ed1e4cb8e2add966c1  ${destination}/helm.tar.gz" | shasum -a 256 -c
tar -xzf "${destination}/helm.tar.gz" -C "${destination}"
mv "${destination}/linux-amd64/helm" "${destination}/helm"
