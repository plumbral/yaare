# macOS 桌面版说明

Yaare 提供 macOS 通用安装包（同时包含 Apple 芯片与 Intel 架构），最低要求 **macOS Ventura 13.3**，应用标识为 `app.yaare`。

## 安装与首次打开

1. 在 [GitHub Releases](https://github.com/plumbral/yaare/releases) 的 **Assets** 中下载 `.dmg` 镜像（例如 `yaare-0.1.0.dmg`），Apple 芯片与 Intel 机型通用。
2. 双击打开，将 **Yaare.app** 拖入「应用程序」，待复制完成后推出磁盘映像。
3. 如需核对文件完整性，可执行 `shasum -a 256 yaare-0.1.0.dmg`，并与同一版本 `SHA256SUMS` 中的摘要比对。

> 当前构建采用临时签名（ad hoc），首次打开时 Gatekeeper 可能会拦截提示，按 [苹果官方说明](https://support.apple.com/en-us/102445) 允许打开即可。

## 数据目录与钥匙串

- **数据目录**：`~/Library/Application Support/Yaare`
- 首次保存密码或私钥时，Yaare 会在当前用户的默认钥匙串中创建 `app.yaare` / `credential-master-v1`，用于加密数据库中的凭据。同步或导入需要处理多条凭据时，每批只读取一次主密钥，批次结束即清零该副本。下一批或单独读写凭据时会重新访问钥匙串，届时若已锁定，需先解锁。
- **更新后重新授权**：由于采用临时签名，覆盖安装新版本后，系统可能会再次弹出钥匙串访问授权提示。
- **换机迁移**：请使用 [本地加密备份](Local_Encrypted_Config.md) 或 [配置同步](Sync_and_Backup.md)。仅复制数据库文件无法迁移钥匙串中的主密钥，新设备将无法解密原有凭据。

## 常用快捷键

- `⌘T`：新建标签，`⌘W`：关闭当前标签
- `⌘1` / `⌘2` / `⌘3`：切换至主机、身份、命令页
- 终端内：`⌘C` 复制选区、`⌘V` 粘贴、`⌘F` 搜索，`Ctrl+C` 正常发送至远端

以上快捷键均可在设置中自定义。

## 从源码打包 DMG

在仓库根目录准备好项目指定版本的 Go 与 Bun，执行：

```sh
bun install --frozen-lockfile
bun run generate
bun run package -platform darwin/universal
```

打包完成后，应用与通用 `.dmg` 会输出至 `build/darwin-universal/`。自动发布流程会挂载镜像并校验签名、版本号、双架构以及「应用程序」快捷方式，详见 [版本发布流程](releases/README.md)。

> 最低版本定为 macOS 13.3 的原因：Go 1.27 要求 macOS 13+，前端构建目标为 Safari 16.4，系统内置 WKWebView 从 Ventura 13.3 起提供对应的 WebKit 特性（参考：[MyGo 配置](https://github.com/egoist/mygo/blob/v0.10.0/docs/configuration.md#macos) · [Go 系统要求](https://go.dev/doc/go1.26#darwin) · [Vite 构建目标](https://vite.dev/config/build-options#build-target) · [Safari 16.4 WebKit](https://webkit.org/blog/13966/webkit-features-in-safari-16-4/)）。

## 原生测试与验证进度

目前已在 M1（macOS 27.2.1）上实机验证了原生构建、启动与重开、钥匙串读写以及 SSH / SFTP / SCP。macOS 13.3 与 Intel 机型尚待实机验证。

在 macOS 上可单独运行钥匙串和卷文件名测试（钥匙串测试会创建独立的临时钥匙串，不影响当前用户日常钥匙串）：

```sh
YAARE_TEST_MAC_KEYCHAIN=1 go test ./internal/adapters/secrets -run '^TestMacDisposableKeychain$' -count=1 -v
go test ./internal/adapters/files -run '^TestMacVolumeNames$' -count=1 -v
```
