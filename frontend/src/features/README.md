# 功能模块

当前入口为 app/Workbench.tsx，使用 Astryx Neutral。主机、身份、命令和设置占据主区域；连接、身份和命令的创建/编辑使用统一居中弹窗。每个 SSH 会话保留终端、目录位置和 SFTP 侧栏状态。

connections/useSSHSessions 按页面、会话和尝试编号隔离真实 SSH；支持跳板、连接代理和端口转发，认证与逐跳指纹验证由 Go 后端负责。终端只显示远端字节，xterm 动态加载，输出有界并受解析确认与 Channel 背压控制。

workspace/SftpPanel 对接真实 FileService，提供本地授权目录与远程文件管理、传输队列。RemoteTextWorkspace 在每个会话的主区域编辑远程 UTF-8 文本，RemoteCodeEditor 延迟加载 CodeMirror，复用简洁输入框样式。保存使用内容版本检查与同目录临时文件替换；冲突保留草稿并提供服务器版本对比，断线不自动重放保存。

文件内容与临时认证内容不写入前端存储或日志。
