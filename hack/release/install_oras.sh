#!/usr/bin/env bash
set -Eeuo pipefail

destination=${1:?usage: install_oras.sh DESTINATION}
version=1.3.3
archive="oras_${version}_linux_amd64.tar.gz"
checksum=9ce999f8d2de03fc03968b29d743077a58783e545e5eaa53917ca177352d0e59

mkdir -p "${destination}/oras-extract"
curl -fsSLo "${destination}/${archive}" \
  "https://github.com/oras-project/oras/releases/download/v${version}/${archive}"
printf '%s  %s\n' "${checksum}" "${destination}/${archive}" | shasum -a 256 -c
tar -xzf "${destination}/${archive}" -C "${destination}/oras-extract" oras
mv "${destination}/oras-extract/oras" "${destination}/oras"
chmod +x "${destination}/oras"
