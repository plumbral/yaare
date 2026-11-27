param([switch]$StartServer)
$ErrorActionPreference = 'Stop'
Push-Location (Join-Path $PSScriptRoot '..')
try {
    $null = New-Item -ItemType Directory -Path build -Force
    if ($StartServer) { & .\scripts\start-openssh-test-server.ps1 }
    $dockerCli = (Get-Command docker -ErrorAction Stop).Source
    & $dockerCli cp tests/openssh/bootstrap-jump-hosts.sh yaare-test-openssh:/tmp/yaare-bootstrap-jump-hosts.sh
    if ($LASTEXITCODE -ne 0) { throw '无法复制隔离跳板测试配置。' }
    & $dockerCli exec yaare-test-openssh sh /tmp/yaare-bootstrap-jump-hosts.sh
    if ($LASTEXITCODE -ne 0) { throw '无法启动容器内部 SSH 测试端口。' }
    $fingerprints = @()
    foreach ($kind in @('ed25519', 'ecdsa', 'rsa')) {
        $line = & $dockerCli exec yaare-test-openssh ssh-keygen -lf "/etc/ssh/ssh_host_${kind}_key.pub"
        if ($LASTEXITCODE -ne 0) { throw '无法读取测试服务器公钥。' }
        $fingerprints += ($line -split ' ')[1]
    }
    foreach ($port in @(22223,22224)) {
        $line = & $dockerCli exec yaare-test-openssh ssh-keygen -lf "/opt/yaare-jump-hosts/host_$port.pub"
        if ($LASTEXITCODE -ne 0) { throw '无法读取容器内部服务器公钥。' }
        $fingerprints += ($line -split ' ')[1]
    }
    $env:YAARE_SSH_ROUTING_TEST = '1'
    $env:YAARE_OPENSSH_FINGERPRINT = $fingerprints -join ','
    & go test ./internal/adapters/ssh -run TestRealSSHRouting -count=1 -timeout=120s -v | Tee-Object -FilePath build/ssh-routing-real-test.log
    if ($LASTEXITCODE -ne 0) { throw '跳板机与端口转发验证失败，请查看 build/ssh-routing-real-test.log。' }
} finally {
    Remove-Item Env:YAARE_SSH_ROUTING_TEST, Env:YAARE_OPENSSH_FINGERPRINT -ErrorAction SilentlyContinue
    Pop-Location
}
