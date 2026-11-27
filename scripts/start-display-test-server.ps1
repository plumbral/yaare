$ErrorActionPreference = 'Stop'
$dockerCli = (Get-Command docker -ErrorAction Stop).Source
$name = 'yaare-display-fixture'
$ssh = 'yaare-test-openssh'
$network = 'yaare-test-display'

function Invoke-DisplayDocker([string[]]$Arguments) {
    $result = & $dockerCli @Arguments
    if ($LASTEXITCODE -ne 0) { throw ('Display fixture operation failed: ' + $Arguments[0]) }
    return $result
}

$label = Invoke-DisplayDocker @('inspect', $ssh, '--format', '{{index .Config.Labels "yaare.test"}}')
if ($label -ne 'openssh') { throw 'Start the project OpenSSH fixture first.' }
if (-not (Invoke-DisplayDocker @('network','ls','--filter',('name=^' + $network + '$'),'--format','{{.ID}}'))) {
    $null = Invoke-DisplayDocker @('network','create','--label','yaare.test=display',$network)
} elseif ((Invoke-DisplayDocker @('network','inspect',$network,'--format','{{index .Labels "yaare.test"}}')) -ne 'display') {
    throw 'The display test network name is already in use.'
}
$sshNetworks = Invoke-DisplayDocker @('inspect',$ssh,'--format','{{json .NetworkSettings.Networks}}') | ConvertFrom-Json
if (-not $sshNetworks.$network) { $null = Invoke-DisplayDocker @('network','connect',$network,$ssh) }
$existing = @(Invoke-DisplayDocker @('ps','-a','--filter',('name=^/' + $name + '$'),'--format','{{.Names}}'))
if ($existing -contains $name) {
    $labels = Invoke-DisplayDocker @('inspect',$name,'--format','{{json .Config.Labels}}') | ConvertFrom-Json
    if ($labels.'yaare.test.display' -ne '1') { throw 'This container is not an owned display fixture.' }
    $null = Invoke-DisplayDocker @('start',$name)
} else {
    $null = Invoke-DisplayDocker @('run','-d','--name',$name,'--label','yaare.test.display=1','--network',$network,'-e','YAARE_DESKTOP_TEST_PASSWORD=Yaare-Desktop-Test-Only!','ubuntu:24.04','sleep','infinity')
}
$fixtureDir = Join-Path $PSScriptRoot '../tests/display'
$null = Invoke-DisplayDocker @('cp',(Join-Path $PSScriptRoot '../tests/install-fixture-packages.sh'),($name + ':/tmp/yaare-install-packages.sh'))
$null = Invoke-DisplayDocker @('cp',(Join-Path $fixtureDir 'entry.sh'),($name + ':/usr/local/bin/yaare-display-fixture'))
$null = Invoke-DisplayDocker @('cp',(Join-Path $fixtureDir 'setup.sh'),($name + ':/tmp/yaare-display-setup.sh'))
$null = Invoke-DisplayDocker @('exec',$name,'sh','/tmp/yaare-display-setup.sh')
& $dockerCli exec $name test -S /tmp/.X11-unix/X99
if ($LASTEXITCODE -ne 0) {
    $null = Invoke-DisplayDocker @('exec','--detach',$name,'sh','-c','exec sh /usr/local/bin/yaare-display-fixture > /tmp/yaare-display.log 2>&1')
}
& $dockerCli exec $name bash -c 'for i in {1..100}; do if DISPLAY=:99 xdpyinfo >/dev/null 2>&1 && (: </dev/tcp/127.0.0.1/5900) 2>/dev/null && (: </dev/tcp/127.0.0.1/3389) 2>/dev/null; then exit 0; fi; sleep 0.1; done; tail -n 12 /tmp/yaare-display.log; exit 1'
if ($LASTEXITCODE -ne 0) { throw 'The X11, VNC and RDP fixture did not become ready.' }
Write-Output 'Display fixture started on the isolated Docker network.'
