# LumeDAV

LumeDAV 是一款轻量、现代的 Windows WebDAV 服务端。它使用 Go + Wails 构建，将服务管理集中在桌面 EXE 中，并提供适配手机、平板和桌面的 Web 文件访问页面。

![LumeDAV Logo](assets/lumedav-logo-v3.png)

## 主要功能

- 多共享文件夹与自定义监听端口
- Windows 系统托盘、关闭后后台运行和带状态检测/一键修复的开机自启动
- WebDAV 与响应式 Web 文件管理器
- 多用户、只读/读写权限及账号启用/停用
- 多个临时目录访问及自动过期
- 管理员一次性邀请注册
- 登录失败限速、IP 临时封禁和会话过期
- 回收站、文件恢复和清空管理
- 运行仪表盘、在线会话和流量统计
- 图片支持按钮、滚轮和手机双指缩放；视频支持流式播放、进度拖动、音量与全屏
- Word、Excel、PowerPoint 通过本机 LibreOffice 转 PDF 预览
- 文件夹一键打包下载，小目录流式 ZIP、大目录支持断点续传和自动清理
- 普通文件下载显示进度、传输速度，并支持随时取消
- 文件夹大小统计，可查看总容量、文件数、子文件夹数和统计耗时
- 登录页支持在私人设备记住密码，并可通过邀请码注册账号
- Web 账号菜单支持安全退出，服务端会立即撤销当前登录会话
- EXE 可选择大文件夹压缩缓存磁盘，并显示容量、占用与空间保护状态
- 文件读取、搜索、上传和预览提供轻量状态动效与明确的处理提示
- 跨共享目录搜索及名称、时间、大小排序
- 适合配合 88FRP 等端口映射工具使用

## 快速开始

1. 从 [Releases](../../releases) 下载带版本号的 `LumeDAV-v*-windows-amd64-setup.exe` 安装包。
2. 安装时可自由选择 C、D 或其他本地磁盘；升级安装不会删除 `%AppData%/LumeDAV` 中的配置。
3. 添加一个或多个共享文件夹。
4. 设置管理员账号、密码和端口。
5. 启动服务，在浏览器打开界面显示的地址。
6. WebDAV 客户端使用 `http://127.0.0.1:端口/dav/`。

LumeDAV 默认监听 `127.0.0.1:8088`，不会检测、启动或修改 88FRP。需要局域网直连时，可在 EXE 中切换为“局域网与本机”。

> 公网 HTTP 会明文传输 WebDAV Basic Auth。通过公网使用时，请在反向代理或穿透服务侧启用 HTTPS。

### 可选预览引擎

- Office：主机安装 LibreOffice 后自动支持 DOC/DOCX、XLS/XLSX、PPT/PPTX 和 OpenDocument 格式。本机转换结果会缓存为 PDF。

## 从源码构建

需要：

- Go 1.25+
- Node.js 18+
- Wails v2
- Microsoft Edge WebView2 Runtime

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@latest
wails build -clean -nsis -installscope user
```

正式安装包位于 `build/bin/LumeDAV-v*-windows-amd64-setup.exe`。安装版使用稳定的 `LumeDAV.exe` 路径，自启动会在每次打开与保存设置时自动核验并修复旧路径。

## 配置数据

配置文件位于 `%AppData%/LumeDAV/config.json`。密码只保存 bcrypt 哈希。请勿公开分享自己的配置文件。

## License

[MIT](LICENSE)
