# Instala (ou atualiza) o dante no Windows, sem administrador.
#
#   irm https://raw.githubusercontent.com/aishiteirai/dante/main/scripts/instalar.ps1 | iex
#
# Baixa o executável da última Release do GitHub, confere o SHA-256 e o coloca em
# %LOCALAPPDATA%\dante, que vai para o PATH do usuário.
# Para uma versão específica:  $env:DANTE_VERSAO = "v0.5.0"  antes de rodar.

$ErrorActionPreference = 'Stop'
# O Windows PowerShell 5.1 ainda pode tentar TLS antigo, que o GitHub recusa.
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$repo = 'aishiteirai/dante'
$base = if ($env:DANTE_URL_BASE) { $env:DANTE_URL_BASE } else { "https://github.com/$repo/releases" }
$api = if ($env:DANTE_API) { $env:DANTE_API } else { "https://api.github.com/repos/$repo" }

# Sem DANTE_VERSAO: a Release mais recente. O link "latest" do GitHub ignora pré-releases
# (ex.: v0.5.0-alpha); sem nenhuma versão estável, usa a mais recente de todas.
$versao = $env:DANTE_VERSAO
if (-not $versao) {
    try {
        Invoke-WebRequest -Uri "$base/latest/download/SHA256SUMS.txt" -Method Head -UseBasicParsing | Out-Null
    } catch {
        $releases = @(Invoke-RestMethod -Uri "$api/releases?per_page=1" -UseBasicParsing)
        if ($releases.Count -eq 0 -or -not $releases[0].tag_name) {
            throw "Ainda não há Release publicada em https://github.com/$repo/releases"
        }
        $versao = $releases[0].tag_name
        Write-Host "Sem versão estável; usando a pré-release $versao."
    }
}
$release = if ($versao) { "$base/download/$versao" } else { "$base/latest/download" }

$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
$asset = "dante-windows-$arch.exe"
$dir = Join-Path $env:LOCALAPPDATA 'dante'
$exe = Join-Path $dir 'dante.exe'
$tmp = Join-Path $env:TEMP "dante-$PID.exe"

New-Item -ItemType Directory -Force -Path $dir | Out-Null

Write-Host "Baixando $asset..."
try {
    Invoke-WebRequest -Uri "$release/$asset" -OutFile $tmp -UseBasicParsing
    $sums = Invoke-WebRequest -Uri "$release/SHA256SUMS.txt" -UseBasicParsing
} catch {
    Write-Host "Não consegui baixar de $release" -ForegroundColor Red
    Write-Host "Confira se já existe uma Release em https://github.com/$repo/releases" -ForegroundColor Red
    throw
}

# Confere a integridade do download.
$text = if ($sums.Content -is [byte[]]) { [Text.Encoding]::UTF8.GetString($sums.Content) } else { $sums.Content }
$expected = ($text -split "`n" | Where-Object { $_ -match "\s\*?$([regex]::Escape($asset))\s*$" } | Select-Object -First 1) -replace '\s.*$', ''
$actual = (Get-FileHash -Algorithm SHA256 $tmp).Hash.ToLower()
if (-not $expected -or $actual -ne $expected.ToLower()) {
    Remove-Item $tmp -Force
    throw "O SHA-256 do download não confere (esperado '$expected', veio '$actual'). Nada foi instalado."
}

Move-Item -Force $tmp $exe

# PATH do usuário (vale nas próximas janelas; nesta, já ajustamos abaixo).
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (-not (($userPath -split ';') -contains $dir)) {
    [Environment]::SetEnvironmentVariable('Path', ($userPath.TrimEnd(';') + ";$dir").TrimStart(';'), 'User')
}
if (-not (($env:Path -split ';') -contains $dir)) { $env:Path += ";$dir" }

Write-Host ""
& $exe --versao
Write-Host "Instalado em $exe. Abra com:  dante" -ForegroundColor Green
