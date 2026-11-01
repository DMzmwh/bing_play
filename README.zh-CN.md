# Bing Play（兵 Play）

[English](README.md) | 简体中文

Bing Play 是一款跨平台桌面 BT 客户端，集「DHT 资源发现 + 种子下载 + 内置视频播放」于一体。后端使用 Go + Wails v3，前端使用 Vue 3 + Naive UI，最终打包为单个原生应用，可运行在 Windows、macOS 与 Linux。

项目坚持本地优先：爬虫抓到的资源、下载任务、设置、播放记录全部保存在本机 SQLite 中，不需要注册账号，也不依赖自建云端服务。

## 项目简介

### 项目定位

Bing Play 面向个人用户的 P2P 内容管理场景，把通常分散在多个工具里的三件事整合进一个桌面程序：

1. **发现资源**：启动后自动连接 BitTorrent DHT 网络，持续抓取种子元信息（名称、InfoHash、体积）。
2. **管理下载**：把资源加入下载队列，按优先级调度下载，支持暂停、继续、重试、取消、修复与断点恢复。
3. **直接播放**：下载完成的文件与本地视频都可以在应用内播放，正在进行中的任务也支持「边下边播」。

### 核心功能

- 内置 DHT 爬虫，后台自动抓取种子资源并落库。
- 种子资源页支持搜索、排序、分页、批量下载与复制磁力链接。
- 下载中心提供实时速度、进度、剩余时间、优先级与完整的任务操作。
- 播放器基于 ArtPlayer，支持拖拽本地视频、播放列表、边下边播与自动格式转换。
- 多语言（英文 / 简体中文 / 繁体中文）与深/浅主题、自定义强调色。
- 重启后自动恢复未完成的下载任务。

### 设计目标

- **本地优先**：除 BT/DHT 网络流量与可选的更新检查外，所有数据与逻辑都在本机完成。
- **单文件分发**：前端构建产物通过 `go:embed` 打进 Go 二进制；运行时资源（语言包、数据库迁移、FFmpeg）放在同级 `resources` 目录。
- **生命周期清晰**：HTTP 服务、DHT 爬虫、下载引擎全部由 Fx 生命周期管理，退出时按「HTTP 先停 → 引擎后停」的顺序释放资源，避免端口占用与悬空连接。
- **IPC 与流媒体分离**：业务调用走 Wails 绑定；本地 HTTP 服务只负责用 `Range` 提供视频字节流。
- **国际化彻底**：所有界面文案（包括播放器控件、错误提示、HTTP 报错）都来自后端语言包，前端不硬编码提示语。

### 适用场景

- 想在桌面端获得「搜资源 → 下载 → 播放」完整闭环，又不想安装浏览器插件的用户。
- 需要参考 Wails v3 + Fx + GORM/SQLite + `anacrolix/torrent` + FFmpeg 组合实现的开发者。

> 版本范围说明：当前版本中「找片」（`/find`）与「影片库」（`/library`）两个页面仅为导航占位页，尚未实现具体功能；已实现页面为「播放」「我的下载」「种子资源」「设置」。

## 技术栈说明

### 后端（Go 1.26）

| 领域 | 技术 / 依赖 | 版本 | 用途 |
|---|---|---|---|
| 桌面壳 | `github.com/wailsapp/wails/v3` | `v3.0.0-beta.16` | 原生窗口、绑定、事件、更新器 |
| 依赖注入 | `go.uber.org/fx` | `v1.24.0` | 模块装配与生命周期管理 |
| ORM | `gorm.io/gorm` | `v1.31.2` | 数据访问 |
| SQLite 驱动 | `modernc.org/sqlite` | `v1.57.0` | 纯 Go SQLite（无需 CGO） |
| SQLite 适配 | `github.com/libtnb/sqlite` | `v1.2.2` | GORM Dialector |
| 数据库迁移 | `github.com/golang-migrate/migrate/v4` | `v4.19.1` | 读取 `resources/migrations` 执行迁移 |
| BT 引擎 | `github.com/anacrolix/torrent` | `v1.61.1-0.20261024051421-5f95cd33fd29` | 下载、做种、流式读取 |
| DHT 客户端 | `github.com/anacrolix/dht/v2` | `v2.24.0` | 爬虫底层 DHT |
| DHT 爬虫 | `github.com/DMzmwh/bing-dhtcrawler` | `v0.2.1` | 元数据抓取 |
| 去重 | `github.com/tylertreat/BoomFilters` | `v0.0.0-20251117164519` | InfoHash Bloom 去重 |
| 国际化 | `github.com/nicksnyder/go-i18n/v2` | `v2.6.1` | JSON 语言包解析 |
| 日志 | `log/slog` + `gopkg.in/natefinch/lumberjack.v2` | `v2.2.1` | 结构化日志与文件轮转 |
| 媒体处理 | FFmpeg / FFprobe | 随包分发 | 格式探测与转码 |

### 前端

| 技术 | 版本 | 用途 |
|---|---|---|
| Vue | `3.5.41` | 界面框架（Composition API） |
| TypeScript | `5.7.3` | 类型检查（`vue-tsc`） |
| Naive UI | `2.45.3` | 组件库与主题 |
| Vue Router | `4.5.1` | 页面路由 |
| ArtPlayer | `5.4.0` | 视频播放器 |
| Vite | `6.4.1` | 开发服务器与生产构建 |
| `@wailsio/runtime` | latest | Wails 绑定、事件、更新事件 |
| `@vicons/ionicons5`、`vfonts` | `^0.13.0`、`0.0.3` | 图标与字体 |
| `sass-embedded` | `^1.103.1` | SCSS 支持 |

### 构建与打包

- Wails v3 CLI（`wails3`）+ Taskfile（根 `Taskfile.yml` 与 `build/**/Taskfile.yml`）。
- Windows 使用 NSIS 生成安装包；macOS 生成 DMG；Linux 生成 AppImage / deb / rpm / Arch 包。

### 运行端口

| 端口 | 用途 | 说明 |
|---|---|---|
| `9245` | Vite 开发服务器 | 仅开发模式，`wails3 task dev` |
| `9090` | 本地 HTTP 视频服务 | 固定监听 `127.0.0.1:9090`，前后端约定一致 |
| `51414` | DHT 爬虫 UDP 端口 | 可在设置中修改，需与下载引擎端口错开 |

## 快速开始

### 环境要求

- **Go 1.26.0 及以上**（`go.mod` 声明 `go 1.26.0`）。
- **Node.js 20 LTS 及以上 + npm**（Vite 6 要求 Node 18+）。
- **Wails v3 CLI**，建议安装与库版本匹配的 beta 版本：
  ```bash
  go install github.com/wailsapp/wails/v3/cmd/wails3@latest
  wails3 version
  ```
- **平台依赖**：
  - Windows：Windows 10/11，WebView2 运行时（系统通常自带）。
  - macOS：Xcode Command Line Tools。
  - Linux：GTK 3 与 WebKitGTK（`webkit2gtk-4.1`），具体包名请参考 Wails 官方文档对应发行版的说明。
- **FFmpeg（可选但强烈建议）**：把 `ffmpeg`、`ffprobe` 放到 `resources/bin/ffmpeg/<goos>-<goarch>/`（例如 `windows-amd64`）。缺失时应用仍可运行，但无法进行格式转换；程序也会回退查找系统 `PATH` 中的 FFmpeg。

### 安装依赖

```bash
# 1. 安装前端依赖
npm --prefix frontend ci

# 2. 生成 Wails 绑定（构建任务也会自动执行）
wails3 task common:generate:bindings
```

### 本地启动

```bash
wails3 task dev
```

- 开发模式会启动 Vite 开发服务器（默认 **http://localhost:9245**）并打开 Wails 桌面窗口，前后端均支持热更新。
- 开发模式下数据库位于 `./data/db/bt.db`，下载目录为 `./data/downloads`。
- 本地视频流服务地址为 **http://127.0.0.1:9090**（桌面应用内部使用，通常无需手动访问）。

### 生产构建与自检

```bash
wails3 task build                  # 产物：./bin/bing-play（Windows 为 bing-play.exe）
go test ./internal/...             # 后端单元测试
go build .                         # 校验主程序可编译
npm --prefix frontend run build    # vue-tsc 类型检查 + 生产前端构建
```

## 功能特性

### 1. DHT 资源发现

- 应用启动时自动拉起 DHT 爬虫（默认开启，可在设置中关闭）。
- 默认监听 UDP `51414`，可配置范围 `1024–65535`，并刻意与下载引擎端口错开。
- 拉取种子元数据超时 10 秒；使用容量 100 万、误判率 1% 的 Bloom Filter 对 InfoHash 去重。
- 抓取结果写入 SQLite，同时在 `resources/crawler/` 下保存 DHT 节点快照（`nodes.json`）。
- 爬虫的「开关 / 端口」修改后立即生效，无需重启应用。
- 停止时同步关闭 DHT 客户端与底层 UDP socket，确保端口立刻释放。

### 2. 种子资源页

- 分页浏览已抓取资源，每页 `50 / 100 / 200` 可选并在本地记忆。
- 支持按资源名称或 InfoHash 搜索。
- 支持四种排序：最新创建、最早创建、体积从大到小、体积从小到大。
- 展示由状态机派生的资源状态（未下载 / 获取元信息 / 等待下载 / 下载中 / 已暂停 / 校验中 / 等待重试 / 已完成 / 失败 / 已取消 / 文件丢失 / 文件损坏 / 本地可用等）。
- 支持复制磁力链接、单条下载与批量下载，批量结果会提示「成功 / 跳过 / 失败」数量。

### 3. 下载中心

- 「下载中」与「下载完成」两个标签页，支持任务暂停、继续、重试、取消、修复、调整优先级、打开下载目录。
- 默认运行参数：

| 参数 | 默认值 | 含义 |
|---|---|---|
| 最大并发任务数 | 5 | 同时下载的任务数量，超出部分按优先级排队 |
| 最大重试次数 | 3 | 失败后自动重试次数，超过后标记为失败 |
| 元信息超时 | 10 分钟 | 等待种子元信息的最长时间 |
| 重试延迟 | 30 秒 | 自动重试前的等待时间 |
| 进度采样间隔 | 1 秒 | 进度采样与事件推送频率 |
| 进度落库间隔 | 10 秒 | 降低 SQLite 写入频率 |
| 停滞判定 | 3 分钟 | 长时间无新增数据时触发自动重试 |

- 采用「任务生命周期 × 本地文件可用性」两条正交状态轴，并派生前端展示态；设计细节见 [资源状态机文档](docs/resource_state_machine.md)。
- 应用重启时自动恢复未完成的下载任务。
- 界面更新以事件推送为主：`download:progress`（约每秒一次）与 `download:state-changed`；另有 5 秒一次的静默 `ListActive` 轮询作为兜底。

### 4. 播放与边下边播

- 基于 ArtPlayer，播放器控件文案同样来自语言包（`art.player` 分组）。
- 支持把本地视频文件拖到播放区：后端校验格式并登记为本地资源，通过事件推送给前端。
- 左侧播放列表列出可播放的本地资源，宽度可在 `200–360` 像素间拖动并持久化。
- 种子资源边下边播：HTTP 服务支持 `Range` / `206`，预读 4 MB，并对正在播放的文件提升分片优先级。
- 播放前格式流水线：
  - 用 FFprobe 探测封装、视频/音频编码与位深。
  - 判定能否直接播放；遇到系统缺少 HEVC 解码器、封装/编码不支持、位深过高时自动转换。
  - 优先硬件编码（Windows 使用 NVENC，macOS 使用 VideoToolbox），失败自动回退 `libx264` 软件编码。
  - 转换前检查磁盘余量；计费网络下自动推迟转换。
  - 输出统一为 H.264 + AAC 的 MP4（`+faststart`），转换进度可通过 `/video/transcode/{jobId}` 轮询。
- 本地文件与已完成校验的种子统一走 `/video/{id}`（支持 `Range`），拖动进度条体验一致。

### 5. 设置与个性化

- 界面语言：English / 简体中文 / 繁體中文，首次启动跟随系统语言。
- 下载目录：可视化选择并校验，修改后对之后新建的任务生效。
- 爬虫开关与监听端口：保存后立即生效。
- 主题：深色 / 浅色；强调色支持 `#RRGGBB`（默认 `#3b82f6`）。
- 支持「保存设置」与「恢复默认值」，所有校验错误均由后端返回本地化文案。

### 6. 国际化与主题适配

- 语言包位于 `resources/locales/active.en.json`、`active.zh-CN.json`、`active.zh-TW.json`，按业务分组（`nav.*`、`video.*`、`resources.*`、`download.*`、`settings.*`、`update.*`、`art.player`）。
- 前端通过后端 i18n 服务加载文案，Naive UI 组件文案与 ArtPlayer 控件文案随语言切换同步更新。
- 品牌图标提供深/浅两套 PNG，随主题自动切换，用于导航栏与播放页标题。

### 7. 自动更新检查

- 启动时初始化 Wails Updater，后台每 30 分钟检查一次更新。
- 前端监听 `Updater.Events.UpdateAvailable` 并弹出本地化提示。
- **发布前注意**：`internal/utils/updater/updater.go` 中当前使用的是 Wails 示例仓库地址，需替换为你自己的 Release 仓库与校验文件配置。

## 项目结构

```
bing_play/
├── main.go                        # 入口：嵌入前端产物与窗口图标，启动 Fx + Wails
├── app/
│   ├── app.go                     # 组合全部 Fx 模块
│   └── wails3_app.go              # 窗口参数、服务注册、退出顺序、更新器
├── internal/
│   ├── config/                    # 运行配置（按 dev / prod 构建标签区分）
│   ├── db/                        # GORM 初始化、迁移、DAO、数据模型
│   ├── dhtcrawler/                # DHT 爬虫服务、资源落库、节点快照
│   ├── torrentmgr/                # BT 引擎、任务状态机、下载调度与恢复
│   ├── transcode/                 # 格式探测、转换决策、FFmpeg 任务调度
│   ├── server/                    # 本地 HTTP 服务与视频/流处理器
│   ├── service/                   # 暴露给前端的 Wails 服务（种子/视频/资源/设置/i18n）
│   ├── i18n/                      # go-i18n 语言包与系统语言识别
│   ├── settings/                  # 设置键名与分组定义
│   ├── filetype/                  # 视频类型识别（扩展名 + MIME）
│   ├── logger/                    # slog + lumberjack 日志初始化
│   ├── container/                 # 通用容器与依赖装配
│   └── utils/                     # 路径、更新器、IP 等工具
├── frontend/
│   ├── src/views/                 # Play / Download / Resources / Settings / Layout / Find* / Library*
│   ├── src/components/            # ArtPlayer 封装、品牌图标组件
│   ├── src/i18n/                  # 从后端加载语言包、切换语言
│   ├── src/settings/              # 设置状态、主题与强调色应用
│   ├── src/assets/icons/          # 深/浅主题品牌图标
│   └── bindings/                  # 自动生成的 Wails 绑定（不提交版本库）
├── resources/
│   ├── locales/                   # 英 / 简中 / 繁中语言包
│   ├── migrations/                # SQL 迁移文件（成对 up/down）
│   └── bin/ffmpeg/<goos>-<goarch>/ # 各平台 FFmpeg（默认不入库，仅保留占位目录）
├── build/                         # 各平台构建资源、Taskfile、图标与安装脚本
├── docs/resource_state_machine.md # 资源状态机设计方案
├── Taskfile.yml                   # 构建 / 开发 / 打包任务入口
└── AGENTS.md                      # 项目开发规范
```

说明：

- `Find.vue`、`Library.vue` 为占位页面；`hello` 示例服务仍保留在代码中但未在导航中使用。
- `frontend/bindings` 由 `wails3 generate bindings` 生成，属于构建产物，不提交。
- `go.work`（含本机模块路径）为本地开发用文件，不提交；正常构建使用 `go.mod` 中的依赖版本。

## 使用说明

### 常用场景

1. **播放本地视频**：进入「播放」页，把视频文件拖到中间播放区。后端会校验文件类型并登记资源，前端自动刷新播放列表并开始播放。
2. **发现并下载资源**：爬虫在后台持续抓取。进入「种子资源」页，搜索或排序后勾选一条或多条资源，点击「批量下载」；进度会实时出现在「我的下载」。
3. **边下边播**：先开始下载任务，再到「播放」页选择该资源对应的文件。应用会按 `Range` 读取种子数据，并优先下载当前播放所需分片。
4. **修改下载目录**：进入「设置 → 通用设置 → 下载目录」。修改仅对之后新建的任务生效，已有任务保持原目录。
5. **恢复未完成任务**：直接重启应用，未完成的下载会自动重新进入队列（已暂停的任务保持暂停状态）。

### 设置项参考

| 设置键 | 分组 | 默认值 | 说明 |
|---|---|---|---|
| `language` | 通用 | 跟随系统，兜底 `en` | `en` / `zh-CN` / `zh-TW` |
| `download.dir` | 通用 | `<数据目录>/downloads` | 保存时校验，仅影响新任务 |
| `crawler.enabled` | 通用 | `true` | 修改后立即生效 |
| `crawler.port` | 通用 | `51414` | `1024–65535`，修改后立即生效 |
| `ui.theme_mode` | 界面 | `dark` | `dark` / `light` |
| `ui.accent_color` | 界面 | `#3b82f6` | 需为 `#RRGGBB` |
| `ui.resource_page_size` | 隐藏 | `100` | 资源页就地保存（`50/100/200`） |
| `ui.sidebar_width` | 隐藏 | `260` | 播放列表拖动调整时保存（`200–360`） |

### 本地 HTTP 接口

HTTP 服务固定监听 `127.0.0.1:9090`，仅用于媒体传输：

| 方法与路径 | 作用 |
|---|---|
| `POST /video/open` | 登记拖入播放区的本地文件路径 |
| `GET /video/prepare/{id}` | 探测资源并返回播放方案（直接播放 / 需要转换） |
| `GET /video/transcode/{jobId}` | 查询转换进度与结果 |
| `GET /video/cache/{name}` | 以 `Range` 提供转换后的缓存文件 |
| `GET /video/{id}` | 播放资源（本地文件或种子，支持范围请求与分片优先级） |
| `GET /video/local/{id}` | 本地媒体预留入口；当前为开发阶段占位实现（写死测试路径），前端未使用 |
| `GET /stream/{id}` | 兼容用的流式入口 |

### 数据与日志位置

| 运行模式 | 数据目录 | 数据库 | 默认下载目录 |
|---|---|---|---|
| 开发（`!prod`） | `./data` | `./data/db/bt.db` | `./data/downloads` |
| 生产（`prod`） | `os.UserConfigDir()/bing-play`（Windows `%AppData%\bing-play`，Linux `~/.config/bing-play`，macOS `~/Library/Application Support/bing-play`） | `<数据目录>/db/bt.db` | `<数据目录>/downloads` |

- 日志通过 lumberjack 轮转，写入数据目录下的日志文件。
- 爬虫节点快照与运行期数据位于 `resources/crawler/`。
- 数据库迁移使用 `file://resources/migrations`，因此**发布时必须保证 `resources` 目录与可执行文件同目录**（构建任务会自动拷贝）。

## 部署指南

### 1. 发布前准备

1. **更新应用元信息**：编辑 `build/config.yml` 中的 `info`（`productName`、`productIdentifier`、`version`、`companyName`、`copyright`）。当前仍是模板值（`My Product`、`com.mycompany.myproduct`）。
2. **准备 FFmpeg**：为目标平台补充 `resources/bin/ffmpeg/<goos>-<goarch>/ffmpeg`、`ffprobe`（仓库只保留占位目录，二进制不入库）。
3. **配置更新源**：修改 `internal/utils/updater/updater.go` 中的仓库地址与校验文件名，避免指向示例仓库。
4. **图标资源**：应用图标源文件为 `build/appicon.png`，可用
   ```bash
   wails3 generate icons -input build/appicon.png \
     -macfilename build/darwin/icons.icns \
     -windowsfilename build/windows/icon.ico
   ```
   重新生成 Windows/macOS 图标；Android 与 iOS 图标分别位于 `build/android/app/src/main/res/mipmap-*` 与 `build/ios/Assets.xcassets`。

### 2. 构建与打包

```bash
# Windows：生成 exe（含图标资源）并打包 NSIS 安装程序
wails3 task build
wails3 task package

# macOS：构建通用二进制并打包 DMG
wails3 task darwin:build
wails3 task darwin:package

# Linux：构建并生成 AppImage / deb / rpm / Arch 包
wails3 task linux:build
wails3 task linux:package
```

注意事项：

- Windows 构建会先执行 `wails3 generate syso`，把 `build/windows/icon.ico` 写入可执行文件资源；打包 NSIS 需要本机安装 NSIS（`makensis`）。
- macOS 对外分发需要签名与公证，相关任务已提供在 `build/darwin/Taskfile.yml`。
- 根任务 `wails3 task build` 会先执行 `copy-resources` 把 `resources/` 拷贝到 `bin/`；若直接调用平台子任务（如 `darwin:build`），请先执行 `wails3 task copy-resources`。分发时需同时提供该目录，或直接使用生成的安装包。
- 前端生产构建由 `common:build:frontend` 自动执行，无需单独运行。

### 3. 服务器与 Docker（模板任务）

根 `Taskfile.yml` 提供了模板自带的 `build:server`、`run:server`、`build:docker`、`run:docker` 任务。当前应用以桌面形态为主（Wails 窗口、Windows 下 `windowsgui` 子系统），如需无界面部署，请先确认这些任务在你的场景中可用。

## 贡献指南

### 参与流程

1. **Fork 仓库并新建分支**：仓库托管在 Gitee（`git@gitee.com:bing_7/bing_play.git`），请从 `master` 派生功能分支或修复分支。
2. **先阅读再修改**：改动前先查看相关 Service、DAO 与模块装配关系，优先复用现有 Fx 模块与基础设施，避免重复造轮子。
3. **保持项目风格**：沿用现有命名与目录结构，避免无关重构、过度抽象和过度拆分文件；未经确认不要引入新的第三方依赖。
4. **国际化约定**：所有面向用户的文案必须来自 `resources/locales/*.json`，并按业务分类归组，禁止在 Vue 组件里硬编码提示语。
5. **数据库变更**：在 `resources/migrations/` 新增成对的 `up` / `down` 迁移，并在提交说明中写明兼容性与迁移方案。
6. **测试与自检**：
   ```bash
   go test ./internal/...
   npm --prefix frontend run build
   ```
   新增后端逻辑时尽量补充单元测试；无法运行测试时请说明原因，不要声称测试通过。
7. **提交与 Pull Request**：提交信息聚焦「为什么改」，PR 描述中说明改动内容、关键实现与实际执行的验证结果。

### 安全与数据

- 不要在代码、日志或提交内容中泄露密钥、令牌或个人信息。
- 未经确认不要执行删除数据、覆盖重要文件等破坏性操作。

## 许可证信息

本项目采用 **MIT License** 开源协议，详见 [LICENSE](LICENSE) 文件。

---

相关文档：[资源状态机设计](docs/resource_state_machine.md) · [开发规范](AGENTS.md)
