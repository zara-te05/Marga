# Instala (o actualiza) marga en Windows sin Scoop ni winget:
#
#   irm https://raw.githubusercontent.com/zara-te05/Marga/main/scripts/instalar.ps1 | iex
#
# Descarga el último release, lo deja en %LOCALAPPDATA%\Programs\marga y agrega esa
# carpeta al PATH de tu usuario. Si hay otro marga.exe antes en el PATH, te lo dice.
$ErrorActionPreference = 'Stop'

$repo    = 'zara-te05/Marga'
$destino = Join-Path $env:LOCALAPPDATA 'Programs\marga'
$arq     = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }

$rel     = Invoke-RestMethod "https://api.github.com/repos/$repo/releases/latest"
$version = $rel.tag_name.TrimStart('v')
$zip     = "marga_${version}_windows_$arq.zip"
$asset   = $rel.assets | Where-Object name -eq $zip
if (-not $asset) { throw "El release $($rel.tag_name) no tiene $zip" }

Write-Host "Instalando marga $($rel.tag_name) en $destino..."
$tmp = Join-Path ([IO.Path]::GetTempPath()) ("marga-" + [guid]::NewGuid())
New-Item -ItemType Directory -Force -Path $tmp, $destino | Out-Null
Invoke-WebRequest $asset.browser_download_url -OutFile (Join-Path $tmp $zip)
Expand-Archive (Join-Path $tmp $zip) -DestinationPath $tmp -Force
Copy-Item (Join-Path $tmp 'marga.exe') (Join-Path $destino 'marga.exe') -Force
Remove-Item $tmp -Recurse -Force

$pathUsuario = [Environment]::GetEnvironmentVariable('Path', 'User')
if (($pathUsuario -split ';') -notcontains $destino) {
    [Environment]::SetEnvironmentVariable('Path', "$destino;$pathUsuario", 'User')
    Write-Host "Agregué $destino a tu PATH (abre una terminal nueva para que lo tome)."
}
$env:Path = "$destino;$env:Path"

$otros = @(Get-Command marga.exe -All -ErrorAction SilentlyContinue | Where-Object { $_.Source -ne (Join-Path $destino 'marga.exe') })
if ($otros.Count -gt 0) {
    Write-Warning "Hay otro marga.exe en el PATH que puede tapar a este: $($otros[0].Source)"
    Write-Warning "prueba: bórralo (o quítalo del PATH) y corre: marga version"
}
& (Join-Path $destino 'marga.exe') version --sin-red
Write-Host "Listo. Siguiente paso: marga init"
