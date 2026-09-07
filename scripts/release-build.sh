#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
output_dir="${1:-build/release}"
if [[ "$output_dir" != /* ]]; then
  output_dir="$repo_root/$output_dir"
fi
cd "$repo_root"

commit="$(git rev-parse HEAD)"
epoch="$(git show -s --format=%ct "$commit")"
version="${VERSION:-0.0.${epoch}-g${commit:0:7}}"
ldflags="-X github.com/selimsandal/qbt-proton-guard/internal/buildinfo.Version=${version}"

mkdir -p "$output_dir"

targets=(
  darwin/amd64
  darwin/arm64
  linux/amd64
  linux/arm64
  linux/riscv64
  windows/amd64
  windows/arm64
)

for target in "${targets[@]}"; do
  os="${target%/*}"
  arch="${target#*/}"
  suffix=""
  [[ "$os" == windows ]] && suffix=".exe"
  name="qbt-proton-guard-${os}-${arch}${suffix}"
  echo "Building $name"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    go build -trimpath -ldflags "$ldflags" -o "$output_dir/$name" ./cmd/qbt-proton-guard
done

host_os="$(go env GOOS)"
host_arch="$(go env GOARCH)"
host_suffix=""
[[ "$host_os" == windows ]] && host_suffix=".exe"
native="$output_dir/qbt-proton-guard-${host_os}-${host_arch}${host_suffix}"
if [[ -x "$native" ]]; then
  actual_version="$("$native" version)"
  if [[ "$actual_version" != "$version" ]]; then
    echo "native binary version mismatch: got '$actual_version', want '$version'" >&2
    exit 1
  fi
else
  echo "No native artifact for ${host_os}/${host_arch}; skipping version check" >&2
fi

(
  cd "$output_dir"
  if command -v sha256sum >/dev/null; then
    sha256sum qbt-proton-guard-* > SHA256SUMS
  else
    shasum -a 256 qbt-proton-guard-* > SHA256SUMS
  fi
)

echo "Built release $version in $output_dir"
