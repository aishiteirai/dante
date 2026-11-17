#!/usr/bin/env sh
# Instala (ou atualiza) o dante no Linux ou no macOS, sem root.
#
#   curl -fsSL https://raw.githubusercontent.com/aishiteirai/dante/main/scripts/instalar.sh | sh
#
# Baixa o binário da última Release do GitHub, confere o SHA-256 e o coloca em
# ~/.local/bin (ou em DANTE_DIR). Para uma versão específica: DANTE_VERSAO=v0.5.0.
set -eu

repo=aishiteirai/dante
base=${DANTE_URL_BASE:-https://github.com/$repo/releases}
if [ -n "${DANTE_VERSAO:-}" ]; then release="$base/download/$DANTE_VERSAO"; else release="$base/latest/download"; fi
dir=${DANTE_DIR:-$HOME/.local/bin}

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) echo "sistema não suportado: $(uname -s) (no Windows, use scripts/instalar.ps1)" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) echo "processador não suportado: $(uname -m)" >&2; exit 1 ;;
esac
asset="dante-$os-$arch"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "Baixando $asset..."
if ! curl -fsSL -o "$tmp/$asset" "$release/$asset" || ! curl -fsSL -o "$tmp/SHA256SUMS.txt" "$release/SHA256SUMS.txt"; then
  echo "Não consegui baixar de $release." >&2
  echo "Confira se já existe uma Release em https://github.com/$repo/releases" >&2
  exit 1
fi

# Confere a integridade do download.
expected=$(awk -v f="$asset" '$2 == f || $2 == "*"f {print $1}' "$tmp/SHA256SUMS.txt")
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$tmp/$asset" | cut -d' ' -f1)
else
  actual=$(shasum -a 256 "$tmp/$asset" | cut -d' ' -f1)
fi
if [ -z "$expected" ] || [ "$expected" != "$actual" ]; then
  echo "O SHA-256 do download não confere (esperado '$expected', veio '$actual'). Nada foi instalado." >&2
  exit 1
fi

mkdir -p "$dir"
install -m 0755 "$tmp/$asset" "$dir/dante"
"$dir/dante" --versao
echo "Instalado em $dir/dante."
case ":$PATH:" in
  *":$dir:"*) echo "Abra com:  dante" ;;
  *) echo "Atenção: $dir não está no PATH. Adicione ao seu shell:  export PATH=\"$dir:\$PATH\"" ;;
esac
