# EasyClash

极简、易用、智能的 Windows 桌面代理客户端。[GitHub](https://github.com/zhang673101454/EasyClash)

打开订阅即可上网；国内直连、国外走代理；支持双订阅互相拉取节点，解决「订阅域名被墙、冷启动无节点」的常见难题。

![EasyClash 宣传图](docs/screenshots/promo.png)

---

## 产品特点

### 极简

- 小窗口、无边框，信息密度克制，没有多余面板
- 点订阅即用，再点同一条关闭；核心操作不超过两步
- 「−」收起到右侧**悬浮窗**，不占任务栏；「×」隐藏到托盘，进程常驻
- 安装包内置 mihomo、GeoIP/GeoSite，无需在线下载规则库

### 易用

- **订阅管理**：添加 / 编辑 URL 与备注、删除、流量进度条与到期时间
- **刷新流量与节点**：一条按钮同时更新配额与节点缓存；可走当前已开代理拉取被墙订阅
- **双订阅引导**：先启用能用的订阅并开代理，再刷新或切换到另一条即可（无需别的工具、无需手动拷文件）
- **托盘**：右键菜单显示「开启代理 / 关闭代理」及勾选状态；左键唤醒主窗口
- **自动选路**：可选定时测速，自动切换到延迟更低的节点
- **开机自启**（设置内可选）

### 智能

- **智能模式**（默认）：内置 `GEOSITE,cn` + `GEOIP,CN` 直连，其余走代理；无需维护规则列表
- **订阅域名直连**：自动为订阅 URL 域名添加 `DIRECT`，避免「无节点时拉订阅误走 PROXY」的死循环
- **本地节点缓存**：刷新成功后写入 `providers/*.source`，mihomo 离线可读；切换前若无缓存会**静默预拉**
- **TUN 模式**（可选）：虚拟网卡接管流量，需管理员权限；与智能模式二选一
- **一键测速**：对节点组测延迟并切到最快

---

## 典型用法

### 单订阅

1. 连接页点 **+** 粘贴订阅 URL → 确认添加  
2. **点击该订阅** → 自动开代理、拉节点、可选自动选最快  
3. 节点页可手动切换线路；设置里可调智能 / TUN、自动选路  

### 双订阅（一条能直连拉取，一条被墙）

1. 添加两条订阅  
2. **先点击**能正常使用的订阅 → 开代理  
3. 在另一条上点 **刷新**（或切换过去，会自动静默拉取）  
4. 再**点击**被墙那条 → 切换使用  

### 窗口与托盘

| 操作 | 效果 |
|------|------|
| 点击订阅 | 启用 / 关闭该订阅与代理 |
| − | 收起为右侧悬浮窗（延迟、网速、开关） |
| × | 隐藏到托盘（未退出） |
| 悬浮窗双击 / 底部按钮 | 恢复主窗口 |
| 悬浮窗右键 | 隐藏到托盘 |
| 托盘右键 | 显示主窗口 / 开启或关闭代理 / 退出 |

真正退出请用托盘 **退出**。

---

## 技术栈

Go + Wails v2 + Vue 3 + Pinia + TailwindCSS，底层 [mihomo](https://github.com/MetaCubeX/mihomo)（Clash.Meta）。  
配置与数据目录：`%APPDATA%\EasyClash\`（订阅列表、节点缓存、mihomo 配置与日志）。

---

## 下载

[GitHub Releases](https://github.com/zhang673101454/EasyClash/releases) — 推荐 `EasyClash-amd64-installer.exe`（已内置 mihomo、wintun、Geo 数据）。

绿色版需自行在同目录放置 `mihomo.exe`（及 TUN 所需的 `wintun.dll`）。

---

## 开发与打包

<details>
<summary>环境要求、开发运行、打包命令（点击展开）</summary>

### 环境

```bat
go version    rem 1.21+
node -v       rem 18+
wails version rem v2.15.0
```

```bat
go install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0
```

### 准备 mihomo

开发时放到项目根或 `resources\mihomo.exe`；打包前务必存在 `resources\mihomo.exe` 与 `resources\wintun.dll`（可用 `scripts\fetch-wintun.ps1`）。

Geo 数据：`powershell -ExecutionPolicy Bypass -File scripts\fetch-geodata.ps1`

### 开发运行

```bat
cd /d e:\code\easy-clash
wails dev
```

### 打包

```bat
powershell -ExecutionPolicy Bypass -File scripts\build-windows.ps1 -Nsis
```

产物：`build\bin\EasyClash.exe`、`build\bin\EasyClash-amd64-installer.exe`

</details>

---

## 常见问题

**Q: 订阅链接在浏览器打不开，EasyClash 也没节点？**  
国内常见原因是订阅域名被墙。请按「双订阅」流程：先用**另一条能用的订阅**开代理，再**刷新**被墙那条；或换手机热点 / 机场备用链接。

**Q: 刷新流量和刷新节点是一回事吗？**  
是。EasyClash 的「刷新流量与节点」会同时更新配额（响应头）并把完整订阅内容缓存到本地，供 mihomo 加载。

**Q: 点开关 / 托盘「开启代理」没反应？**  
需至少添加一条订阅；若未启用任何订阅，会尝试自动启用第一条。

**Q: 找不到 mihomo？**  
绿色版请把 `mihomo.exe` 放在 exe 同目录或 `resources\`；安装包已内置。

**Q: 关窗口后进程还在？**  
设计如此。用托盘 **退出** 才会结束进程；× 与悬浮窗右键只是隐藏。

**Q: TUN 打不开？**  
需**管理员身份运行** EasyClash，且同目录有 `wintun.dll`。

**Q: 已连接但上不了网？**  
检查节点是否有效（避免选用占位节点 `127.0.0.1`）；尝试刷新订阅或换节点；查看 `%APPDATA%\EasyClash\mihomo.log`。
