$ErrorActionPreference='Stop'
Push-Location (Join-Path $PSScriptRoot '..')
try {
 $null = New-Item -ItemType Directory -Path build -Force
 & go build -o build/zmodem-pty.exe ./tests/zmodem-pty
 if($LASTEXITCODE -ne 0){throw '真实 PTY 测试程序编译失败。'}
 $env:YAARE_ZMODEM_TEST='1'
 & bun test tests/zmodem.test.ts
 if($LASTEXITCODE -ne 0){throw 'ZMODEM 真实协议检查未通过，请查看测试输出。'}
} finally {
 Remove-Item Env:YAARE_ZMODEM_TEST -ErrorAction SilentlyContinue
 Pop-Location
}
