# 功能模块

当前入口为 app/Workbench.tsx，使用 Astryx Neutral。主机、身份、命令和设置占据主区域；连接、身份和命令的创建/编辑使用统一居中弹窗。每个 SSH 会话保留终端、目录位置和 SFTP 侧栏状态。

connections/useSSHSessions 按页面、会话和尝试编号隔离真实 SSH；支持跳板、连接代理和端口转发，认证与逐跳指纹验证由 Go 后端负责。终端只显示远端字节，xterm 动态加载，输出有界并受解析确认与 Channel 背压控制。

workspace/SftpPanel 对接真实 FileService，提供本地授权目录与远程文件管理、传输队列。RemoteTextWorkspace 在每个会话的主区域编辑远程 UTF-8 文本，RemoteCodeEditor 延迟加载 CodeMirror，复用简洁输入框样式。保存使用内容版本检查与同目录临时文件替换；冲突保留草稿并提供服务器版本对比，断线不自动重放保存。

文件内容与临时认证内容不写入前端存储或日志。

SSH 终端请求服务器的默认登录 shell。服务器的欢迎信息、登录记录和提示符按接收顺序进入同一终端的回滚缓冲区。
在空白 Bash 提示符下，可以通过会话更多操作中的「Bash 终端集成…」启用本次会话的目录报告。集成就绪后可从 SFTP 切换终端目录或使用 ZMODEM 快捷上传。支持 OSC 7 的 shell 可自行报告目录。
Linux OpenSSH 主机还支持按远程账户自动加载 Bash 集成。安装和移除均先预览启动文件和账户密钥文件的改动，确认后对后续登录生效。
