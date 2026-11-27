param([int]$Port = 22222)
$ErrorActionPreference = 'Stop'
$dockerCli = (Get-Command docker -ErrorAction Stop).Source
Push-Location (Join-Path $PSScriptRoot '..')
try {
    $existing = & $dockerCli ps -a --filter 'name=^/yaare-test-openssh$' --format '{{.Names}}'
    if ($LASTEXITCODE -ne 0) { throw 'Could not query Docker.' }
    if ($existing) {
        $label = & $dockerCli inspect --format '{{index .Config.Labels "yaare.test"}}' yaare-test-openssh
        if ($label -ne 'openssh') { throw 'The container name is already in use outside the OpenSSH fixture.' }
        & $dockerCli start yaare-test-openssh
    } else {
        & $dockerCli run -d --name yaare-test-openssh --label 'yaare.test=openssh' -p "127.0.0.1:${Port}:22" ubuntu:24.04 sleep infinity
    }
    if ($LASTEXITCODE -ne 0) { throw 'OpenSSH test server did not start.' }
    & $dockerCli cp tests/install-fixture-packages.sh yaare-test-openssh:/tmp/yaare-install-packages.sh
    if ($LASTEXITCODE -ne 0) { throw 'Could not copy the fixture package installer.' }
    & $dockerCli cp tests/openssh/bootstrap.sh yaare-test-openssh:/tmp/yaare-openssh.sh
    if ($LASTEXITCODE -ne 0) { throw 'Could not copy the OpenSSH fixture.' }
    & $dockerCli exec yaare-test-openssh bash /tmp/yaare-openssh.sh
    if ($LASTEXITCODE -ne 0) { throw 'OpenSSH test server setup failed.' }
    Write-Output "Local OpenSSH/SFTP: 127.0.0.1:$Port, fixture user: yaaretest. Test data is isolated inside the container."
} finally { Pop-Location }
