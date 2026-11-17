#!/usr/bin/env sh
# Compila o dante para todas as plataformas suportadas.
# Uso: scripts/compilar.sh VERSAO PASTA   (ex.: scripts/compilar.sh v0.5.0 dist)
set -eu

version=${1:-dev}
out=${2:-dist}
mkdir -p "$out"

for target in windows/amd64 windows/arm64 linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  os=${target%/*}
  arch=${target#*/}
  ext=""
  [ "$os" = windows ] && ext=".exe"
  echo "compilando $os/$arch"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath \
    -ldflags "-s -w -X main.version=$version" \
    -o "$out/dante-$os-$arch$ext" ./cmd/dante
done

(cd "$out" && sha256sum dante-* > SHA256SUMS.txt)
echo "pronto: $out/"
