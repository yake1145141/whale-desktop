# 小鲸鱼余额挂件 · Go 桌面版

> 原作者 / Original author：MeteorNOX
>
> Go 桌面移植：whale-desktop contributors
>
> 上游项目：[DeepSeek-Balance-Whale-Widget](https://github.com/MeteorNOX/DeepSeek-Balance-Whale-Widget)
>
> 许可证：MIT

本项目是原 Electron 桌面版的 Go 重写版本。窗口、托盘、余额查询、账本、配置、皮肤、全局键盘监听、拖拽与点击穿透均由 Go 实现；Wails 仅使用系统 WebView2 绘制原有宠物界面。

构建产物是单个 Windows 可执行文件，不再需要 Node.js、npm 或 Electron。

## 功能

- DeepSeek 余额查询，25 秒内存缓存和瞬时错误重试。
- 小鲸鱼记账与平台令牌用量两种模式。
- 加权随机台词、自定义峰谷文案。
- JSONC 配置、台词和峰谷文件，支持注释与尾逗号。
- 完整皮肤包：立绘、GIF、音效、几何参数和独立缩放。
- 透明、无边框、置顶桌宠窗口，隐藏任务栏按钮。
- 拖动、贴边吸附、位置记忆和多显示器工作区约束。
- 透明区域点击穿透，锁定后只保留左上角解锁按钮。
- 时间、余额、全局键盘按键三种显示模式。
- 系统托盘、双开桌宠、水平翻转和 Windows 当前用户开机自启。
- 原有用户数据格式兼容，可直接沿用 `%APPDATA%\dsh-whale-desktop\`。

## 系统要求

- Windows 10/11 x64。
- [Microsoft Edge WebView2 Runtime](https://developer.microsoft.com/microsoft-edge/webview2/)；多数 Windows 10/11 已预装。
- 从源码构建需要 Go 1.24 或更高版本。

## 构建与运行

```powershell
cd F:\whale-desktop
.\build.ps1
.\build\WhaleDesktop.exe
```

`build.ps1` 会先运行全部 Go 测试，再使用 Wails 必需的构建标签生成 GUI 程序：

```text
-tags desktop,production
```

不要直接使用不带标签的 `go build`，否则 Wails 会显示构建标签错误。

生成绿色版 ZIP 和 Windows 安装程序：

```powershell
.\package.ps1 -Version 0.4.0
```

安装程序使用当前用户安装，不要求管理员权限；卸载时保留用户配置、皮肤、账本和日志。
绿色版解压后直接运行 `WhaleDesktop.exe`。

以后台方式启动：

```powershell
.\start-pet.cmd
.\start-pet.cmd --debug
```

调试启动会把应用日志同时写入控制台。运行日志本身位于：

```text
%APPDATA%\dsh-whale-desktop\whale.log
```

## 用户数据

程序不会覆盖已有配置和皮肤。首次运行会创建：

```text
%APPDATA%\dsh-whale-desktop\
├── config.jsonc          主配置
├── lines.jsonc           加权随机台词
├── peaks.jsonc           峰谷文案方案
├── skins\
│   └── default\
│       ├── skin.jsonc
│       ├── whale.png
│       ├── rua.gif
│       └── sounds\
├── .usage.json           30 天余额差账本
├── .window.json          主桌宠位置和独立外观状态
├── .window-secondary.json 第二只桌宠状态
├── webview2\             WebView2 缓存
└── whale.log
```

修改配置后可点托盘菜单“重载配置与台词”，或在宠物菜单点“重载配置”。

## DeepSeek 凭据

API Key 按以下顺序查找，命中即停止：

1. `config.jsonc` 的 `apiKey`
2. 环境变量 `DEEPSEEK_API_KEY`
3. `%USERPROFILE%\.dsh\.credentials.yaml` 中的 `refs.DEEPSEEK_API_KEY`

平台令牌模式还会读取：

1. `config.jsonc` 的 `platformToken`
2. 环境变量 `DEEPSEEK_PLATFORM_TOKEN`
3. DSH 凭据中的同名项

凭据不会写入日志。

## 操作

| 操作 | 效果 |
| --- | --- |
| 左键点鲸鱼 | 打开/关闭当前显示模式的气泡 |
| 点气泡 | 切换到随机台词，再点关闭 |
| 拖动鲸鱼 | 移动桌宠，松手后吸附并保存位置 |
| 右键鲸鱼或点汉堡按钮 | 打开设置菜单 |
| 悬停鲸鱼 | 显示左上角锁定按钮 |
| 点击锁定 | 除锁按钮外全部鼠标穿透 |
| 左键托盘图标 | 显示/隐藏桌宠 |
| 右键托盘图标 | 皮肤、刷新、双开、翻转、配置、自启、退出 |

双开后的两只桌宠分别保存位置、皮肤、翻转、音量和音效组。

## 显示模式

```jsonc
{
  "displayMode": "balance" // balance | time | keyboard
}
```

键盘模式只在选中时安装 Windows 低级键盘观察钩子。按键事件只用于生成气泡内最多三键的组合提示，不拦截输入、不保存历史、不写日志、不上传网络。

Shift + 数字会显示为 `!`、`@`、`#` 等符号。

## 台词格式

台词按相对权重抽取：

```jsonc
{
  "groups": [
    { "w": 45, "builtin": "period" },
    {
      "w": 10,
      "variants": ["好模型... ↓", "好女孩...↓"],
      "style": "B"
    },
    { "w": 3, "gif": true }
  ]
}
```

也支持扁平台词：

```jsonc
{
  "lines": [
    { "text": "余额还有 {balance}", "weight": 9, "style": "B" },
    { "text": "今天也要好好干活", "weight": 2, "style": "A" }
  ]
}
```

可用占位符：`{today}`、`{balance}`、`{currency}`、`{period}`。

样式：`A` 小标题、`B` 大字、`C` 灰色提示、`P` 中号时段。

## 皮肤

皮肤目录位于 `%APPDATA%\dsh-whale-desktop\skins\<名称>\`。复制 `default` 后修改 `skin.jsonc` 和 `whale.png` 即可创建新皮肤。

```jsonc
{
  "displayName": "我的鲸鱼",
  "description": "自定义皮肤",
  "image": "whale.png",
  "gif": "rua.gif",
  "sounds": {
    "duck": {
      "label": "小黄鸭",
      "press": "sounds/Ya1.mp3",
      "release": "sounds/Ya2.mp3"
    }
  },
  "geometry": {
    "img": { "width": 0.5968, "height": 0.5968, "right": 0, "bottom": 0 },
    "bubble": {
      "width": 0.7143,
      "vbox": { "w": 1026, "h": 700 },
      "left": 0.02,
      "top": -0.46
    },
    "text": { "leftPct": 0.4425, "topPct": 0.35, "anchor": "center" },
    "font": { "label": 0.0643, "amount": 0.1248, "period": 0.1014, "hint": 0.0546 },
    "wrapMax": 0.5458,
    "gif": { "leftPct": 0.4425, "topPct": 0.44, "maxW": 0.5458, "maxH": 0.3899 },
    "menuBtn": { "topRatio": 0.4055, "right": 4, "size": 30 },
    "lockBtn": { "size": 28, "offsetX": 0, "offsetY": 0 },
    "windowPadding": 16
  }
}
```

省略 `geometry` 时会继承 Go 内置默认几何。立绘应使用透明背景 cut-out。

## 代码结构

```text
whale-desktop/
├── app.go                     Wails 应用、窗口、托盘和绑定
├── main.go                    嵌入资源与启动入口
├── frontend/                  宠物渲染、菜单和交互
├── assets/                    默认皮肤素材、托盘图标
├── internal/
│   ├── autostart/             Windows 注册表自启
│   ├── balance/               余额和平台用量
│   ├── config/                路径、配置和日志
│   ├── credentials/           配置 → 环境变量 → DSH
│   ├── jsonc/                 注释、尾逗号、原子写入
│   ├── keyboard/              非拦截低级键盘钩子
│   ├── ledger/                每日余额差账本
│   ├── lines/                 加权台词
│   ├── peaks/                 峰谷文案
│   ├── platform/              Win32 窗口、进程和单实例
│   └── skin/                  皮肤扫描和资产编码
├── build.ps1
└── start-pet.cmd
```

## 验证

```powershell
go test ./...
.\build.ps1
```

测试覆盖 JSONC、配置合并、台词权重、余额峰谷判断和账本币种切换。

## 排查

| 现象 | 处理 |
| --- | --- |
| 构建时提示 Wails build tags | 使用 `.\build.ps1`，不要裸跑 `go build` |
| 窗口出现红色前端错误 | 查看 `whale.log`，删除 `%APPDATA%\dsh-whale-desktop\webview2\` 后重启 |
| 余额显示 `--` | 检查 API Key 来源，或点托盘“刷新余额” |
| 键盘监听不可用 | 确认当前系统允许低级键盘钩子并切换到键盘显示模式 |
| 自定义皮肤不可见 | 确认 `skin.jsonc` 可解析且立绘路径存在 |
| 想让桌宠置顶 | 在挂件菜单勾选“总在最前” |

## 许可证与署名

MeteorNOX 创建了原始小鲸鱼挂件与素材。本项目保留其署名、上游链接与 MIT 许可证。再次分发代码或素材时，请同时保留 [LICENSE](LICENSE)。
