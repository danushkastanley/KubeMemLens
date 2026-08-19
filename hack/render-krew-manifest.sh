#!/bin/sh
set -eu

if [ "$#" -ne 3 ]; then
  echo "usage: $0 <version> <checksums-file> <output>" >&2
  exit 2
fi

version=$1
checksums_file=$2
output=$3
version_number=${version#v}

checksum() {
  filename=$1
  value=$(awk -v filename="$filename" '$2 == filename { print $1 }' "$checksums_file")
  if [ -z "$value" ]; then
    echo "missing checksum for $filename" >&2
    exit 1
  fi
  printf '%s' "$value"
}

linux_amd64=$(checksum "kube-memlens_${version_number}_linux_amd64.tar.gz")
linux_arm64=$(checksum "kube-memlens_${version_number}_linux_arm64.tar.gz")
darwin_amd64=$(checksum "kube-memlens_${version_number}_darwin_amd64.tar.gz")
darwin_arm64=$(checksum "kube-memlens_${version_number}_darwin_arm64.tar.gz")
windows_amd64=$(checksum "kube-memlens_${version_number}_windows_amd64.zip")
windows_arm64=$(checksum "kube-memlens_${version_number}_windows_arm64.zip")

sed \
  -e "s/{{VERSION}}/$version/g" \
  -e "s/{{VERSION_NUMBER}}/$version_number/g" \
  -e "s/{{SHA_LINUX_AMD64}}/$linux_amd64/g" \
  -e "s/{{SHA_LINUX_ARM64}}/$linux_arm64/g" \
  -e "s/{{SHA_DARWIN_AMD64}}/$darwin_amd64/g" \
  -e "s/{{SHA_DARWIN_ARM64}}/$darwin_arm64/g" \
  -e "s/{{SHA_WINDOWS_AMD64}}/$windows_amd64/g" \
  -e "s/{{SHA_WINDOWS_ARM64}}/$windows_arm64/g" \
  deploy/krew/memlens.yaml.tmpl > "$output"

if grep -q '{{' "$output"; then
  echo "unresolved Krew manifest placeholder" >&2
  exit 1
fi
