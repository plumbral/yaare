param([switch]$StartServer)
$ErrorActionPreference = 'Stop'
Push-Location (Join-Path $PSScriptRoot '..')
try {
    $null = New-Item -ItemType Directory -Path build -Force
    if ($StartServer) { & .\scripts\start-openssh-test-server.ps1 }
    $dockerCli = (Get-Command docker -ErrorAction Stop).Source
    $fingerprints = @()
    foreach ($kind in @('ed25519', 'ecdsa', 'rsa')) {
        $publicKey = & $dockerCli exec yaare-test-openssh ssh-keygen -lf "/etc/ssh/ssh_host_${kind}_key.pub"
        if ($LASTEXITCODE -ne 0) { throw 'Cannot read isolated fixture public host key.' }
        $fingerprints += ($publicKey -split ' ')[1]
    }
    $env:YAARE_OPENSSH_TEST = '1'
    $env:YAARE_OPENSSH_FINGERPRINT = $fingerprints -join ','
    & go test ./internal/adapters/ssh ./internal/desktop/ipc -run TestRealOpenSSH -count=1 -timeout 30m -v | Tee-Object -FilePath build/openssh-real-test.log
    if ($LASTEXITCODE -ne 0) { throw '真实 OpenSSH/SFTP 验证失败，请查看 build/openssh-real-test.log。' }
} finally {
    Remove-Item Env:YAARE_OPENSSH_TEST, Env:YAARE_OPENSSH_FINGERPRINT -ErrorAction SilentlyContinue
    Pop-Location
}
