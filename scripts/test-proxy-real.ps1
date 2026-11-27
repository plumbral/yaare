param([switch]$StartServers)
$ErrorActionPreference = 'Stop'
Push-Location (Join-Path $PSScriptRoot '..')
try {
    $null = New-Item -ItemType Directory -Path build -Force
    if ($StartServers) {
        & .\scripts\start-openssh-test-server.ps1
        & .\scripts\test-ssh-routing-real.ps1
        & .\scripts\start-proxy-test-server.ps1
    }
    $dockerCli = (Get-Command docker -ErrorAction Stop).Source
    $fingerprints = @()
    foreach ($kind in @('ed25519', 'ecdsa', 'rsa')) {
        $line = & $dockerCli exec yaare-test-openssh ssh-keygen -lf "/etc/ssh/ssh_host_${kind}_key.pub"
        if ($LASTEXITCODE -ne 0) { throw '无法读取隔离 SSH 服务器公钥。' }
        $fingerprints += ($line -split ' ')[1]
    }
    foreach ($port in @(22223,22224)) {
        $line = & $dockerCli exec yaare-test-openssh ssh-keygen -lf "/opt/yaare-jump-hosts/host_$port.pub"
        if ($LASTEXITCODE -ne 0) { throw '无法读取内部 SSH 服务器公钥。' }
        $fingerprints += ($line -split ' ')[1]
    }
    $env:YAARE_PROXY_TEST = '1'
    $env:YAARE_OPENSSH_FINGERPRINT = $fingerprints -join ','
    & go test ./internal/adapters/ssh -run TestRealProxy -count=1 -timeout=120s -v | Tee-Object -FilePath build/proxy-real-test.log
    if ($LASTEXITCODE -ne 0) { throw '真实代理与 SSH 验证失败，请查看 build/proxy-real-test.log。' }
} finally {
    Remove-Item Env:YAARE_PROXY_TEST, Env:YAARE_OPENSSH_FINGERPRINT -ErrorAction SilentlyContinue
    Pop-Location
}
