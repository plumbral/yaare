$ErrorActionPreference = 'Stop'
Push-Location (Join-Path $PSScriptRoot '..')
try {
    & bun scripts/generate-brand-assets.ts
    if ($LASTEXITCODE -ne 0) { throw 'Yaare brand asset generation failed.' }
} finally {
    Pop-Location
}