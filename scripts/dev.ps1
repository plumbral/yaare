param([switch]$Web)
$ErrorActionPreference = 'Stop'
Push-Location (Join-Path $PSScriptRoot '..')
try {
    if ($Web) { & bun run dev:web } else { & bun run dev }
    if ($LASTEXITCODE -ne 0) { throw 'Development server exited with an error.' }
} finally {
    Pop-Location
}
