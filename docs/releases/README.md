# Yaare 版本发布流程

## 发版步骤

1. 在本目录新建对应版本的更新说明（如 `v0.1.0.md`）。发布标题按版本号生成，GitHub Release 和客户端离线更新日志共用这份文件。
2. 修改根目录 `package.json` 中的版本号，连同更新说明一起提交。
3. 推送同名 Git 标签，例如稳定版 `v0.1.0` 或预发布版 `v0.2.0-rc.1`。桌面安装包、浏览器前端和 Docker 服务的发布版本都取自标签。

推送标签后，Actions 默认构建并校验 Windows、Linux 和 macOS 安装包，生成 GitHub Release 草稿。草稿附带桌面安装文件和 `SHA256SUMS`，其中列出各附件的 SHA-256 校验值。[发布工作流](../../.github/workflows/release.yml)

如需同时发布 Docker 镜像，在推送标签前将仓库的 Actions 变量 `YAARE_RELEASE_DOCKER` 设为 `true`。工作流在原生 AMD64 和 ARM64 Linux runner 上构建镜像，测试首次初始化、管理员登录和容器重建后的 SSH 凭据解密。两种架构均通过后，工作流发布多架构镜像，将固定镜像摘要的 `compose.yaml` 附到草稿，并把该文件列入 `SHA256SUMS`。如果 Docker 发布失败，桌面安装包通过校验后仍会生成草稿。

核对草稿后在 GitHub 发布。启用 Docker 发布时，工作流使用仓库的 `GITHUB_TOKEN` 和 `packages: write` 权限。已有同名容器包需要向当前仓库授予 Actions 写权限。若要开放匿名拉取，在 GitHub Packages 中将容器包设为 **Public**。当 GitHub **Latest** 指向最高稳定版本且该版本附带 `compose.yaml` 时，Actions 将相应的镜像摘要标记为 `:latest`。[稳定版镜像工作流](../../.github/workflows/docker-latest.yml)

## 本地手动打包

如需在本地复现某个平台的发布包，在对应系统下运行 `bun run package -platform <目标>`：

| 构建系统 | 目标参数 | 生成文件 |
| --- | --- | --- |
| Windows | `windows/amd64,windows/arm64` | 两个 `.exe` 安装程序 |
| Linux | `linux/amd64,linux/arm64` | 两种架构的 `.deb`、`.rpm`、`.tar.gz`，以及共用的 `install.sh` |
| macOS | `darwin/universal` | Apple 芯片与 Intel 共用的 `.dmg` |

> Linux 构建环境需保留归档内可执行文件的执行权限；macOS 需在原生环境下使用系统工具生成并校验 `.dmg`。
