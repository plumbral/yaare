$ErrorActionPreference = 'Stop'
$label = & docker inspect --format '{{index .Config.Labels "yaare.test"}}' yaare-test-openssh
if ($LASTEXITCODE -ne 0 -or $label -ne 'openssh') { throw 'Start the project OpenSSH fixture first.' }
& docker exec yaare-test-openssh sh /tmp/yaare-install-packages.sh xauth x11-utils
if ($LASTEXITCODE -ne 0) { throw 'Could not install the X11 test tools.' }
