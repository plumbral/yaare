# Yaare 单用户 Docker 部署

一个实例有一个所有者。浏览器工作台和桌面配置同步共用同一个容器。网页中的 SSH 连接从容器发起。网页里的“浏览器文件”是当前页面的临时交换区，关闭页面会释放它；需要留在访问设备上的文件应点“保存到设备”。

## 下载并启动

安装带 Compose 的 Docker，创建一个部署目录，把该版本的 `compose.yaml` 下载到里面。GitHub Releases 提供的文件固定到已经测试的镜像摘要，同一份文件适用于 x86-64 和 ARM64。[下载最新稳定版的 compose.yaml](https://github.com/plumbral/yaare/releases/latest/download/compose.yaml)

在这个目录里运行以下两条命令。第一条会拉取镜像，并提示输入两次管理员密码（12–72 字节）。第二条在后台启动服务。

```sh
docker compose run --rm yaare init
docker compose up -d
```

本机打开 `http://127.0.0.1:4180`，用 `admin` 和刚才设置的密码登录。实例密钥自动保存在独立的 Docker 卷中。以后的启动只需 `docker compose up -d`。

<details>
<summary>手动创建 compose.yaml，使用最新稳定版镜像</summary>

```yaml
name: yaare
services:
  yaare:
    image: ${YAARE_IMAGE:-ghcr.io/plumbral/yaare:latest}
    restart: unless-stopped
    init: true
    read_only: true
    cap_drop: [ALL]
    security_opt: [no-new-privileges:true]
    tmpfs: ["/tmp:rw,nosuid,nodev,noexec,size=128m,mode=1777"]
    ports: ["127.0.0.1:${YAARE_PORT:-4180}:4180"]
    environment:
      YAARE_ORIGIN: ${YAARE_ORIGIN:-http://127.0.0.1:${YAARE_PORT:-4180}}
    volumes:
      - yaare-data:/data
      - yaare-keys:/keys
volumes:
  yaare-data:
  yaare-keys:
```

</details>

## 域名和端口

默认端口只对 Docker 主机开放。使用域名时，将反向代理指向 `127.0.0.1:4180`，保留公开 Host，并启用 HTTPS 和 WebSocket。在部署目录的 `.env` 中设置浏览器实际访问的地址，再执行 `docker compose up -d`。

```dotenv
YAARE_ORIGIN=https://ssh.example.com
```

要更改宿主机端口，在同一文件中设置 `YAARE_PORT=4181`。本机访问地址会随端口调整；使用反向代理时也应更新其上游端口。

## 网页使用哪台机器的资源

打开网页的电脑只需浏览器。即使在外地用笔记本登录，SSH 连接仍从 Yaare 容器发起。浏览器选择文件、下载和 VNC 全屏在当前设备上操作。

| 功能 | 资源位置及用法 |
| --- | --- |
| 本地转发、动态 SOCKS5 | 监听在 Yaare 容器。填写 `127.0.0.1` 时，只有共享该容器网络命名空间的进程能访问。如果要让 Docker 主机上的程序使用，需显式配置容器网络可达的监听地址，并在 Compose 将相应端口发布到主机回环地址。 |
| 远程转发 | 监听在 SSH 目标，能否被目标以外的设备访问取决于监听地址和目标 sshd 配置。 |
| SSH Agent | 可选。将 Docker 主机上为 Yaare 准备的专用 Agent 套接字只读挂载到容器，例如 `/run/yaare-agent/socket`，把容器的 `SSH_AUTH_SOCK` 设为该路径，并授予运行 UID 连接权限。已验证普通密钥和 OpenSSH 用户证书。只读挂载仍允许请求签名，应仅加载打算授权给这个实例的身份。 |
| 图形会话 | 在已连接的 SSH 会话中打开 VNC，可在浏览器内切换全屏和只读。RDP 由 Windows 桌面版调用系统远程桌面客户端。浏览器版目前支持 VNC，SSH X11 图形转发尚未接入网页。 |

## 与桌面共享配置

1. 登录浏览器，在“设置 → 同步与备份”中选择“Yaare 自托管”。保留当前实例地址，设置独立的配置加密密码。浏览器作为受信设备，通过进程内接口参与同步。
2. 在同一页点击“生成设备令牌”。每台桌面各用一个。令牌只展示一次，可记录对应行的设备编号。服务器持久化令牌摘要。
3. 在包含此功能的桌面版中选择同一存储类型，填入实例地址、该桌面的设备令牌和相同的配置加密密码。首次使用“检查并合并”，核对已有配置和逐项冲突。
4. 使用“恢复备份”时先预览，确认替换后该设备暂停自动同步。核对结果后再明确继续。在网页撤销设备令牌会阻止该设备后续访问同步对象。

工作区、密文同步对象及历史分别持久化在数据卷中。SSH/SFTP 上传和下载的用户文件不属于配置同步。设备令牌只授权配置同步。在桌面设置的「同步与备份」中选择 Yaare 自托管，即可配对。

## 升级

在网页的「设置 → 关于」可以查看当前镜像版本和随镜像提供的更新日志。「GitHub Releases」列出可用版本，部署管理员在宿主机上执行升级。

备份后，把新 Release 的 `compose.yaml` 下载到原部署目录。使用上面的 `:latest` 示例时可直接拉取更新。两种方式都执行以下命令，原来的数据卷和密钥卷会继续使用。

```sh
docker compose pull
docker compose up -d
```

## 备份与恢复

结束活跃的终端和文件操作，停止服务，分别备份 `yaare-data` 与 `yaare-keys` 两个卷。数据卷包含工作区和加密凭据，密钥卷用于解密。以下示例使用 Linux 的默认项目名 `yaare`。如果用过 `-p`，请用实际项目名前缀。

```bash
set -e
umask 077
docker volume inspect yaare_yaare-data yaare_yaare-keys >/dev/null
docker compose stop
mkdir -p -m 700 "$HOME/yaare-backups"
stamp=$(date -u +%Y%m%dT%H%M%SZ)
for part in data keys; do
  archive="$HOME/yaare-backups/$part-$stamp.tar"
  docker run --rm --network none -v "yaare_yaare-$part:/backup:ro" debian:12-slim tar -C /backup -cf - . > "$archive"
  sha256sum "$archive" > "$archive.sha256"
done
docker compose start
```

将密钥归档另存到受保护的位置，并保留这一版的 Compose 文件。恢复时先验证归档摘要，将两个同批次的归档分别解到对应的新卷，保留文件所有权和权限。在隔离环境中使用原版 Compose 启动，核对登录、已保存凭据、同步历史和加密配置导出。回退时使用升级前的镜像和配套备份。

## 无人值守安装、密码重置和源码构建

- 自动部署可将密码从密钥管理器通过标准输入交给 `docker compose run --rm -T yaare init --password-file=-`。已有密钥也可以通过 `--key-file` 指定只读挂载的 32 字节文件。初始化和运行服务时使用同一把密钥。
- 重置密码时，停止服务，将新密码文件只读挂载到容器中，例如 `/run/secrets/owner-password`。使用一次性的 Compose 覆盖文件，将服务的 `command` 设为 `["--listen=0.0.0.0:4180", "--assets=/app/public", "--reset-owner-password-file=/run/secrets/owner-password"]`，然后启动服务。用新密码登录后移除覆盖文件和密码挂载，再执行 `docker compose up -d`。网页需要重新登录，桌面配对令牌可在同步设置中单独撤销。
- 从源码构建时，在仓库根目录运行 `docker build -f deployment/docker/Dockerfile -t yaare-web:local .`，在部署目录的 `.env` 中设置 `YAARE_IMAGE=yaare-web:local`，按首次安装步骤启动。
