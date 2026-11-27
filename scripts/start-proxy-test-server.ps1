$ErrorActionPreference = 'Stop'
Push-Location (Join-Path $PSScriptRoot '..')
try {
    $dockerCli = (Get-Command docker -ErrorAction Stop).Source
    $networkName = 'yaare-test-routing'
    $containerName = 'yaare-test-proxies'
    $networkId = & $dockerCli network ls --filter "name=^${networkName}$" --format '{{.ID}}'
    if (-not $networkId) {
        & $dockerCli network create --label yaare.test=proxy $networkName
        if ($LASTEXITCODE -ne 0) { throw '无法创建隔离代理测试网络。' }
    } else {
        $label = & $dockerCli network inspect --format '{{index .Labels "yaare.test"}}' $networkName
        if ($label -ne 'proxy') { throw '同名网络并非本项目代理测试网络，未修改。' }
    }
    $sshInfo = & $dockerCli inspect yaare-test-openssh | ConvertFrom-Json
    if ($LASTEXITCODE -ne 0 -or $sshInfo[0].Config.Labels.'yaare.test' -ne 'openssh') { throw '请先启动本项目 OpenSSH 隔离测试容器。' }
    if (-not $sshInfo[0].NetworkSettings.Networks.$networkName) {
        & $dockerCli network connect $networkName yaare-test-openssh
        if ($LASTEXITCODE -ne 0) { throw '无法将测试 SSH 服务接入测试网络。' }
    }
    $sshInfo = & $dockerCli inspect yaare-test-openssh | ConvertFrom-Json
    $sshAddress = $sshInfo[0].NetworkSettings.Networks.$networkName.IPAddress
    $existing = & $dockerCli ps -a --filter "name=^${containerName}$" --format '{{.Names}}'
    if (-not $existing) {
        & $dockerCli run -d --name $containerName --label yaare.test=proxy --network $networkName -p 127.0.0.1:23080:3128 -p 127.0.0.1:23081:3129 -p 127.0.0.1:23082:1080 -p 127.0.0.1:23083:1081 ubuntu:24.04 sleep infinity
        if ($LASTEXITCODE -ne 0) { throw '无法启动隔离代理容器，请检查测试端口是否被占用。' }
    } else {
        $label = & $dockerCli inspect --format '{{index .Config.Labels "yaare.test"}}' $containerName
        if ($label -ne 'proxy') { throw '同名容器并非本项目测试容器，未修改。' }
        & $dockerCli start $containerName
        if ($LASTEXITCODE -ne 0) { throw '无法启动已有代理测试容器。' }
    }
    & $dockerCli cp tests/install-fixture-packages.sh "${containerName}:/tmp/yaare-install-packages.sh"
    if ($LASTEXITCODE -ne 0) { throw '无法复制测试依赖安装脚本。' }
    & $dockerCli cp tests/openssh/bootstrap-proxy.sh "${containerName}:/tmp/yaare-bootstrap-proxy.sh"
    if ($LASTEXITCODE -ne 0) { throw '无法复制代理服务配置。' }
    & $dockerCli exec --env "YAARE_SSH_IP=$sshAddress" $containerName sh /tmp/yaare-bootstrap-proxy.sh
    if ($LASTEXITCODE -ne 0) { throw '隔离代理服务初始化失败。' }
} finally { Pop-Location }
