# 测试

在项目根目录安装锁定依赖并运行 `bun run generate`，然后在 Windows PowerShell 中执行：

```powershell
.\scripts\check.ps1
```

脚本检查生成的主题和 TypeScript 类型，运行前端回归测试以及 `go test ./...`、`go vet ./...` 和 `go build ./...`。需要区分执行环境的组件测试按脚本中的参数隔离运行。

后端 SSH 测试使用回环 TCP 服务和临时工作区。手动检查终端时，可以运行 `go run ./tests/ssh-fixture` 启动回环夹具。启动桌面程序时使用 `--yaare-data-dir=<绝对路径>` 指向独立测试目录。

`scripts/test-openssh-real.ps1`、`scripts/test-ssh-routing-real.ps1` 和 `scripts/test-zmodem-real.ps1` 等扩展测试使用当前 Docker 上下文中的本机 OpenSSH 测试容器。Linux 密钥环测试使用 `sh scripts/test-linux-keyring.sh` 创建的临时 D-Bus 会话。

浏览器版的 SSH Agent 和端口转发验收需要 Go 和本机 Linux Docker 引擎（含 Compose）。在仓库根目录运行

```sh
go test -tags docker_integration ./internal/webserver -run '^TestDockerAgentAndForwardOwnership$' -count=1 -timeout=15m -v
```

每次运行都会从当前源码构建浏览器服务，并启动专用的 SSH Agent。测试分别验证普通公钥和仅接受证书的 SSH 服务、本地与远程转发、SOCKS5、页面间的控制权限及断开后的端口释放。浏览器服务使用随机回环端口，结束后自动移除本次的容器、网络和卷。首次运行需要下载 Debian 镜像及 OpenSSH 软件包。同一基础镜像和安装脚本会复用本地的 OpenSSH 测试镜像，测试密钥在每轮启动后单独生成。

启动 `bun run dev:web` 后，可以打开 `http://127.0.0.1:5173/test-pages/virtual-list.html` 检查虚拟列表。`frontend/test-pages/` 中的页面只作为开发测试入口。
