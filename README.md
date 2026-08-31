# EasyClash

极简的代理工具。[GitHub](https://github.com/zhang673101454/EasyClash)

极简桌面代理客户端：打开开关即可代理，一键测速并选择延迟最低的节点。

技术栈：Go + Wails v2 + Vue 3 + Pinia + TailwindCSS，底层引擎为 [mihomo](https://github.com/MetaCubeX/mihomo)（Clash.Meta）。本仓库**不内置节点**，需要自行准备 `mihomo.exe` 并在配置中导入订阅。

---

## 1. 环境要求

在 **cmd** 中检查（不要用旧的 32 位 Go）：

```bat
go version
go env GOARCH
node -v
npm -v
wails version
```

应类似：

| 工具 | 要求 |
|------|------|
| Go | 1.21+，且必须是 **windows/amd64**（`go env GOARCH` 为 `amd64`） |
| Node.js | 18+（当前开发用过 26） |
| Wails CLI | v2.15.0 |
| WebView2 | Windows 11 一般已自带 |

### 1.1 安装 / 重装 64 位 Go

若 `go version` 显示 `windows/386`，必须卸掉 32 位后再装 64 位：

1. 「设置 → 应用」卸载安装在 `Program Files (x86)\Go` 的 Go。
2. 打开 [https://go.dev/dl/](https://go.dev/dl/)，下载 **`go1.27.0.windows-amd64.msi`**（不要选 386）。
3. **关掉所有 cmd / Cursor 终端再重开**，再执行：

```bat
where go
go version
go env GOARCH
```

应看到：

```
D:\Program Files\Go\bin\go.exe
go version go1.27.0 windows/amd64
amd64
```

若 `where go` 先打出 `(x86)` 路径，到系统环境变量 Path 里删掉 `...\Program Files (x86)\Go\bin`。

### 1.2 安装 Wails CLI

用 **64 位 Go** 安装，否则会编出 32 位程序：

```bat
go install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0
```

把 `%USERPROFILE%\go\bin` 加入用户 Path（本机一般为 `C:\Users\你的用户名\go\bin`）。

验证：

```bat
wails version
wails doctor
```

`wails doctor` 里 **Architecture 必须是 amd64**。

---

## 2. 准备 mihomo 内核

应用不会自己实现代理协议，运行时要能找到 `mihomo.exe`。任选一处放置即可：

开发时（`wails dev`）：

- 项目根目录：`e:\code\easy-clash\mihomo.exe`
- 或：`e:\code\easy-clash\resources\mihomo.exe`
- 或：已加入系统 Path，命令名为 `mihomo`

打包后（双击 exe）：

- 与 `EasyClash.exe` 同一目录
- 或该目录下的 `resources\mihomo.exe`

下载： [mihomo Releases](https://github.com/MetaCubeX/mihomo/releases) ，选 Windows amd64，例如 `mihomo-windows-amd64.zip`，解压出 `mihomo.exe` 后改名或直接放到上述目录。

首次启动会把默认配置写到：

```
%APPDATA%\EasyClash\config.yaml
```

一般是 `C:\Users\你的用户名\AppData\Roaming\EasyClash\config.yaml`。

**订阅在界面里设置：** 打开「订阅」页，粘贴机场 Clash / mihomo 订阅 URL 后点「添加」。可添加多条，用「使用中 / 已停用」切换当前启用的订阅。  
「节点」页可查看全部可用节点并点击切换。打开开关后列表才会加载。

---

## 3. 怎么运行（开发）

在项目根目录打开 **cmd**：

```bat
cd /d e:\code\easy-clash
wails dev
```

第一次会执行 `frontend` 的 `npm install` 和 Vite 热更新。成功后弹出 350×500 的无边框窗口。

日常操作：

- 点中间大开关：启动/停止 mihomo，并开关系统代理（`127.0.0.1:7890`）
- 点「一键智能测速」：对节点组测速并切到延迟最低的节点
- 点右上角 ×：窗口隐藏到托盘，**不会退出**
- 托盘菜单：显示主窗口 / 开启关闭代理 / **退出**

真正退出必须用托盘「退出」，否则进程还在。

### 只看前端界面（不连 Go 后端）

Go 绑定在浏览器里不可用，只能看 UI：

```bat
cd /d e:\code\easy-clash\frontend
npm install
npm run build
npx vite preview --host 127.0.0.1 --port 4173
```

浏览器打开 `http://127.0.0.1:4173/`。点开关会失败，属正常。

---

## 4. 怎么打包

在项目根目录：

```bat
cd /d e:\code\easy-clash
wails build -platform windows/amd64 -trimpath -ldflags "-s -w"
```

参数含义：

| 参数 | 作用 |
|------|------|
| `-platform windows/amd64` | 强制 64 位，避免 Wails 跟错架构 |
| `-trimpath` | 去掉本机路径，方便分发 |
| `-ldflags "-s -w"` | 去掉符号表，缩小体积 |

产物：

```
e:\code\easy-clash\build\bin\EasyClash.exe
```

当前大约 12 MB。不要加 `-webview2 embed`，否则会把 WebView2 打进去，体积暴涨。已安装 [UPX](https://upx.github.io/) 时可再加 `-upx` 进一步压缩。

### 分发给别人时

把这些放在同一文件夹：

```
SomeFolder\
  EasyClash.exe
  mihomo.exe          （或 resources\mihomo.exe）
```

对方电脑需要已安装 WebView2（Win10/11 通常已有）。首次运行同样会在 `%APPDATA%\EasyClash\` 生成配置。

---

## 5. 常用命令一览

```bat
rem 环境自检
wails doctor

rem 开发运行
wails dev

rem 正式打包（推荐）
wails build -platform windows/amd64 -trimpath -ldflags "-s -w"

rem 只编前端
cd frontend
npm run build
```

---

## 6. 常见问题

**Q: `go version` 仍是 386？**  
Path 里 32 位 Go 排在 64 位前面。卸掉 `(x86)` 那份，或从 Path 删除对应目录，然后重开终端。

**Q: Cursor / 终端里找不到 `go` 或 `wails`？**  
把 `D:\Program Files\Go\bin` 和 `%USERPROFILE%\go\bin` 加入 Path，然后重启 Cursor。

**Q: 点开关提示找不到 mihomo？**  
按第 2 节把 `mihomo.exe` 放到程序目录或 `resources\`。

**Q: 订阅在哪填？**  
主界面底部「订阅链接」，粘贴后点保存。不要改 `external-controller`，必须保持 `127.0.0.1:9090`。

**Q: 已连接但上不了网 / 测速失败？**  
先确认订阅已保存且机场链接可用。节点写在 `%APPDATA%\EasyClash\config.yaml` 的 `proxy-providers` 里。

**Q: 关窗口后任务栏没了，也关不掉？**  
程序在系统托盘。右键托盘图标选「退出」。

**Q: 打包报 `EasyClash-res.syso` 找不到？**  
多半是 32 位 Wails 在编 64 位包。用 64 位 Go 重新 `go install` Wails，再带 `-platform windows/amd64` 打包。
