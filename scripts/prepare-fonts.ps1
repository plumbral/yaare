param([string]$Python = 'python', [switch]$DownloadOnly)
$ErrorActionPreference = 'Stop'
Push-Location (Join-Path $PSScriptRoot '..')
try {
    $fontWorkRoot = Join-Path (Get-Location).Path 'build\font-work'
    New-Item -ItemType Directory -Path $fontWorkRoot -Force | Out-Null
    $manifest = Get-Content -LiteralPath scripts\fonts-manifest.json -Raw -Encoding UTF8 | ConvertFrom-Json
    foreach ($source in $manifest.inputs) {
        $target = Join-Path $fontWorkRoot $source.file
        if (-not (Test-Path -LiteralPath $target)) {
            Invoke-WebRequest -UseBasicParsing -Uri $source.url -OutFile $target
        }
        if ((Get-FileHash -LiteralPath $target -Algorithm SHA256).Hash.ToLowerInvariant() -ne $source.sha256) {
            throw ('Font source checksum mismatch: ' + $source.file)
        }
    }
    if (-not $DownloadOnly) {
        & $Python scripts/compress-fonts.py $fontWorkRoot
        if ($LASTEXITCODE -ne 0) { throw 'Font compression failed. Install fonttools[woff]==4.62.1 for the selected Python.' }
    }
} finally { Pop-Location }
