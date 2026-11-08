# LiveMix Studio

LiveMix Studio 是直播切片桌面客户端。当前版本以 Qt 6 + C++ Widgets 实现全部界面，
并已接入真实的视频录制、mp4 直链下载、本地视频导入、一键成片（ASR 转写、LLM 选片、ffmpeg 裁剪、
剪映草稿生成与按需下载）、JSON 配置与 SQLite 业务数据持久化。

> 工作台、一键成片主链路、任务中心、源视频库与设置页已接入真实数据与外部服务，
> 统计与动态均来自本地 SQLite；提示词管理仍为界面演示，演示内容集中在
> `src/model/DemoData.h`。

## 已实现界面

- 工作台：真实统计卡片（素材 / 草稿 / 下载 / 运行任务）、流水线概览、最近项目、
  最近动态、快捷操作；空库与数据库不可用时有独立状态
- 源视频库：真实录制/下载/本地导入列表、录制或下载中状态与进度、停止/取消、悬停删除、添加视频弹窗、
  本地视频缩略图预览（ffmpeg 截帧 + 时长/分辨率/体积）、卡片封面与按钮播放视频、
  应用内播放器（播放/暂停、进度拖动、加载与错误状态）、搜索过滤
- AI 成片结果：真实剪映草稿卡片；点击「打开草稿文件夹」时才下载草稿与素材到本地
- 成片流水线：ASR 整片识别的等待过程（波形、任务动态、估算进度与轮询心跳）、
  完成后的整稿转写、AI 选片过程与候选切片、草稿逐步生成、可点击阶段切换；
  总进度、用时、预计剩余与失败状态均来自真实过程事件
- 提示词管理：列表、只读查看、新建/编辑弹窗、全屏编辑器
- 任务中心：一键成片任务持久化，运行中实时进度、失败重试、完成后查看成片
- 设置：服务连接、存储与输出、AI 与 ASR、快捷键、通用（本地 JSON 持久化与系统集成）
- 设计说明：完整设计系统、配色、字体、快捷键说明
- 通用框架：无边框窗口、红黄绿窗口按钮、窗口拖拽/缩放、标题栏上下文、
  自定义侧边栏与状态栏、Toast、带背景模糊的模态层

## 技术栈

- Qt 6.12.0 / C++17（本地开发与 CI 打包统一使用该版本；macOS 目标系统下限为 14.4）
- Qt Core、Gui、Widgets、Svg、Sql、Network、Multimedia / MultimediaWidgets
- SQLite（Qt SQLite 驱动）
- ffmpeg / ffprobe（命令行子进程）
- 豆包 ASR（api.jcaigc.cn/v1/audio/transcriptions，分句 + 逐词时间线）、LLM 网关（api.jcaigc.cn）、剪映小助手（capcut-mate.jcaigc.cn）
- CMake 3.21+

未引入 `CLAUDE.md` 中禁止的 Qt 模块。所有控件均为自绘或 Widgets 组合，
颜色、间距、字体与状态样式集中管理，便于跨平台保持一致的视觉效果。

## 视频录制与本地数据

### 录制流程

- **线上链接**：支持 `http / https / rtmp / rtsp`。添加后先对 http(s) 链接做一次小范围内容
  探测：mp4 / mov 直链直接通过 HTTP 下载原文件（临时文件 + 原子改名，失败不留半截文件），
  下载完成后用 ffprobe 校验，并在音轨不是 AAC 时用 ffmpeg 仅转码音频、视频保持直拷；
  m3u8 / 直播流 / 未知协议则交给 ffmpeg 以 HLS 格式录制到本地
  （`index.m3u8` + `segment_*.ts`）。停止录制时向 ffmpeg 发送退出指令，等待其收尾后更新数据库。
  直播录制使用 `-c copy` 直接封装，要求源流是 H.264 / AAC 等 TS 兼容编码；不兼容时 ffmpeg 会报错，
  错误信息会保存到视频库卡片中。
- **导入本地视频**：支持拖拽和文件选择对话框，只接受视频容器
  （mp4 / mov / mkv / flv / ts / m2ts / avi / webm / m4v / mpg / mpeg / wmv / 3gp / m3u8，
  单文件 ≤ 10 GB）。导入前用 ffprobe 校验必须包含视频流，纯音频或封面图不会通过；
  校验通过后读取时长、大小和流信息。原始文件保留在原位置，仅在视频库登记。
- **源视频库**：从 SQLite 读取真实数据；录制/下载中的卡片实时显示进度并支持停止或取消；
  完成后可继续进入「一键成片」流程。在线直链来源标记为「在线下载」，直播流标记为「直播录制」。
- **删除**：只移除视频库记录，不删除本地录制文件。

### 视频预览

- 视频库卡片使用 ffmpeg 异步截取封面帧（缓存复用），点击封面中央的播放按钮打开应用内播放器。
- MP4 / MOV 等本地视频直接由 `QMediaPlayer` 播放；HLS 录像先转封装开头 60 秒立即播放，
  完整文件在后台继续转封装并显示进度，完成后无缝切换到完整时长（缓存复用，写入 `.part` 后原子改名）。
- 播放器支持播放/暂停、点击或拖动进度条定位、空格快捷键；加载、缓冲、播放结束与失败状态均有明确反馈。
- 新增 Qt Multimedia / MultimediaWidgets 依赖；打包脚本会校验对应框架与后端插件。

### 配置与业务数据边界

| 数据 | 存储 | 位置 | 职责 |
| --- | --- | --- | --- |
| 用户配置 | JSON | `QStandardPaths::AppConfigLocation/config.json` | 服务 API、录制/存储路径、HLS 参数、AI 默认值、通用偏好等 |
| 业务数据 | SQLite | `QStandardPaths::AppDataLocation/livemix.db` | 源视频与录制会话 |

- `ConfigStore` 只负责 JSON 配置；`MediaRepository` 只负责 SQLite 业务表；
  `RecordingService` 读取配置、驱动 ffmpeg 或 HTTP 直链下载，并通过仓储落库。三者职责不交叉。
- 主要表结构：
  - `media_sources`：源视频（标题、来源类型、URI、本地路径、状态、时长、大小、ffprobe JSON、时间戳）。
  - `recording_sessions`：录制会话（关联 `source_id`、输出路径、容器、状态、起止时间、时长、大小、错误信息）。
  - `asr_results`：ASR 转写缓存（`source_id` 主键、源文件指纹、分句时间线 JSON，兼容逐字时间线）。
  - `clip_drafts`：一键成片草稿（关联 `source_id`、`draft_id`/`draft_url`、主题、话题、
    成片实际时长、切片起止时间、本地下载路径）。
  - `clip_tasks`：一键成片任务（状态、步骤、进度、消息、草稿数、起止时间），供任务中心回看。
  - 使用 `PRAGMA user_version` 管理 schema 版本（当前为 4）；
    外键级联删除保证删除源视频时缓存、草稿与任务记录一并清理。
- 应用启动时会把上次未正常结束的录制标记为中断；已有播放列表的部分录制会保留为可用并附带警告。
  mp4 直链下载只有在完整落盘后才会改名为正式文件，未完成的下载不会留下可用素材。
- 默认录制目录为 `Movies/LiveMix/Recordings`，可在「设置 → 存储与输出」中修改并保存。
- 调试/测试可覆盖路径：`LIVEMIX_CONFIG_PATH`、`LIVEMIX_DB_PATH`、`LIVEMIX_FFMPEG_DIR`、`LIVEMIX_LOG_DIR`。

### 设置项

「设置」页右上角的「保存设置」会把所有可编辑项写入 `config.json`；「测试连接并保存」会先保存服务配置，再请求 `<API 地址>/models` 验证连通性。

- **服务连接**：API 地址与密钥持久化；同一密钥用于 LLM 网关、豆包 ASR 与剪映小助手上传。测试结果区分连接成功、密钥无效（401/403）、服务可达但返回其它 HTTP 状态、网络错误和超时。密钥保存在本地配置文件中，macOS/Linux 下文件权限为 `0600`。剪映小助手 API 地址 `https://capcut-mate.jcaigc.cn` 为编译期内置默认值（`src/core/ServiceEndpoints.h`），不写入配置文件，设置页不提供修改入口；豆包 ASR 与 LLM 共用这里的网关地址。
- **存储与输出**：草稿/成片/缓存/录制目录、HLS 分段长度、缓存上限与自动清理；录制目录和分段长度会立即用于之后启动的录制。
- **AI 与 ASR**：内置提示词（财经类直播提示词从 live-mixer 后端导入；另含服装切片场景的 3 个代表性版本，其中「默认提示词」为结构化合规模板），均为只读内置项；SRT 导出偏好；成片时长由所选提示词控制。
- **快捷键**：当前为展示说明，尚无播放器可绑定。
- **通用**：
  - 外观主题：保存 `dark` / `system` 偏好；当前版本仅提供深色主题。
  - 开机时自动启动：macOS 写入 `~/Library/LaunchAgents/com.livemix.studio.plist`，Windows 写入当前用户 `Run` 注册表项，下次登录生效；从 DMG 直接运行时需先把应用拖入 Applications。
  - 关闭窗口时最小化到托盘/菜单栏：默认开启；关闭按钮会隐藏窗口并继续运行录制任务，可通过托盘菜单「显示主窗口」或「退出 LiveMix」恢复/退出。
  - 任务完成时播放提示音、系统通知：录制完成时蜂鸣并通过系统托盘通知；系统托盘不可用时仅显示应用内 Toast。
- **关于**：显示构建版本；检查更新仍为本地提示。

## 一键成片（直播切片）

在源视频库选择已就绪的视频，点击「一键成片」执行完整链路。直播录制阶段仍只保存
`index.m3u8` + `segment_*.ts`，便于崩溃/中断后恢复；点击一键成片时先无损合成一份
完整 MP4（`<录制目录>/index.mp4`），已合成则直接复用。之后的音频提取、ASR 与裁剪
全部按本地 MP4 的处理方式进行。

1. **提取 MP3**：`FfmpegUtils` 用 ffmpeg 从完整 MP4 转成 16kHz 单声道 64kbps MP3。
   音轨比视频短时补尾部静音，音轨晚于视频起点时补开头静音，并统一截到源 MP4
   的时长，保证 ASR 时间线与视频时间轴一致。
2. **ASR 转写**：整段音频经剪映小助手 `upload_file` 取得公网 URL 后，只提交一个识别
   任务到网关的豆包 ASR `POST https://api.jcaigc.cn/v1/audio/transcriptions`
   （模型 `volc.seedasr.auc`，`verbose_json` 分句时间线，并带
   `timestamp_granularities[]=word` 请求逐词时间线）。网关是同步接口，识别完成后
   一次性返回整稿；不拆分音频，避免破坏上下文与说话人分离。等待期间过程页按
   音频时长与服务速度生成单调的估算进度，并展示已等待时长、预计剩余与状态心跳。
   转写结果写入 SQLite `asr_results`，以缓存版本 + 源文件大小 + 修改时间作为指纹
   （HLS 录像使用 `index.m3u8`，重新合成 MP4 不会改变指纹）；
   重复一键成片直接复用缓存，不重复计费。旧版缓存没有词级时间线，会随版本号自动失效。
3. **AI 选片**：单段转写最长 30 分钟。按 ASR 实际时长分段：不超过 30 分钟整段
   生成一条成片；更长时段数取总时长 / 30 分钟向上取整，再按段数均分总时长
   （40 分钟 → 2×20 分钟，66 分钟 → 3×22 分钟）。每段切点在目标时间前后最多
   找 5 分钟的句号，找不到则取离目标最近的句段边界；每段打包成带行号的 ASR
   列表调用 LLM
   （`POST https://api.jcaigc.cn/v1/chat/completions`，默认 `qwen3.8-flash`）。
   系统提示词使用界面选择的提示词（内置提示词从 live-mixer 后端导入），
   用户提示词追加固定输出格式；模型返回 `indices`（句段索引）与
   `title / description / topics`。
4. **合并与裁剪**：按索引取出 ASR 句段，相邻间隔 ≤500ms 时合并为一段；
   每段用 ffmpeg 从完整 MP4 裁剪并重新编码为 H.264/AAC MP4。
   本地 MP4 直接使用原文件；HLS 录像在成片准备阶段已经合成好 `index.mp4`，
   避免直接对分片 seek 造成切片偏短和字幕错位。
5. **生成草稿**：所有裁剪片段经 `upload_file` 转成 URL，按累计时间铺到同一条
   `add_videos` 主视频轨，再调用 `add_captions`；字幕只取被选中的 ASR 句段，
   按标点拆句并去掉标点，行的起止时间优先取 ASR `words` 的词级时间线，
   再累加片段在成片时间轴上的偏移。网关没有返回 `words`（或词流与文本对不上）时，
   才退回按文字长度分摊，并在运行日志中给出提示。
   每条成片随机选用一套字体、文字颜色、描边颜色与入场动画，字幕位置固定在
   `transform_x=0 / transform_y=-800`。
6. **按需下载**：草稿生成后只落库；点击「打开草稿文件夹」时才调用 `get_draft`，
   下载草稿文件与视频素材到「设置 → 存储与输出 → 剪映草稿目录」。下载先写入
   根目录下的隐藏暂存目录（`.livemix-staging`），全部文件就绪后再原子改名到正式
   目录，并把素材路径改写为剪映占位符相对路径后打开本地目录。剪映的目录监听会
   看到一份完整出现的新草稿并直接收录，无需重启客户端。

### 一键成片过程页

过程页按设计稿分为顶部总览（源视频、提示词、用时、预计剩余、总进度）、三阶段轨道
与三个过程面板，数据来自 `LiveClipService` 通过 `PipelineUi::Event` 推送的真实事件：

- **ASR 转写**：整段音频只提交一个识别任务，过程页按「准备中 → 识别中 → 解析结果 →
  已完成」展示等待过程：整段波形与发光播放头、任务动态（提取音轨 → 提交任务 →
  整片识别 → 解析时间轴 → 说话人分离 → 交给 AI 选片）、已等待与预计剩余、每 2 秒
  一次的状态心跳，以及进度、轮询、音频信息三张卡片。进度按音频时长与服务速度估算
  并明确标注「估算」，识别完成前不展示任何伪造转写；完成后一次性展示整稿并自动进入
  AI 选片。命中本地 ASR 缓存时直接回放完整转写。说话人与置信度使用网关返回的可选
  字段，未返回时显示「—」或通用说话人标签，不伪造数据。
- **AI 选片**：按转写时长决定整段处理或均衡切成每段不超过 30 分钟的多段，逐段流式调用 LLM（SSE）。模型的
  `reasoning_content` 会按句读与换行切分成要点，在底部固定的「实时思考」面板中
  逐条展示最新几条，最新要点高亮并带光标，同时显示已输出字数与字/秒速率；
  历史日志只记录规划、读取、选入等关键节点，不再被长 reasoning 刷屏。
  长时间没有新 token、429/5xx 重试时在面板中显示状态，避免界面看起来卡死。
  每段开始时对应候选卡片进入“评估中”，完成时写入标题、时间区间、时长、字幕数与
  话题；token 用量与速率取自流式 `usage` 的输出 tokens。
- **生成剪映草稿**：选片完成后预建草稿列表，按真实执行顺序展示「裁剪切片 → 上传素材 →
  创建剪映草稿 → 添加视频 → 添加字幕 → 保存草稿」六步进度与耗时；点击任一条草稿可
  固定在右侧查看详情，生成中的草稿逐条推进。
- **阶段与离开页面**：顶部三阶段卡片可点击回看已完成阶段；离开页面任务继续运行，
  完成后停留在完成态，可「再跑一次」或「查看成片」；失败时定位到出错阶段，
  并保留已经生成的草稿。

- 服务地址：剪映小助手为编译期内置地址（`src/core/ServiceEndpoints.h`）；
  豆包 ASR 与 LLM 共用「设置 → 服务连接」中的网关地址与 API 密钥，同一密钥覆盖三项服务。
- 词级字幕依赖网关把豆包上游的 `words` 透传到 `verbose_json` 的 `segments[]`；
  网关未升级时客户端仍可运行，字幕自动退回按文字长度分摊，运行日志会给出提示。
- 相关模块：`src/api`（HTTP / ASR / LLM / 剪映小助手）、
  `src/services/LiveClipService`（流水线编排）、`src/services/DraftDownloader`（按需下载）、
  `src/media/FfmpegUtils`（音视频处理）。
- 任务在独立线程运行，ASR、选片与草稿步骤通过 `PipelineUi::Event` 实时反馈到过程页；
  关闭窗口默认最小化到托盘，任务继续。

### 任务中心

- 一键成片启动时在 SQLite 创建任务；任务中心实时显示当前步骤、进度与消息。
- 离开页面后任务继续进行，点击「查看进度」可回到流水线继续查看。
- 完成后点击「查看成片」进入 AI 成片结果；失败任务显示原因并可重试。
- 应用退出或异常中断的任务会在下次启动时标记为失败，已缓存的 ASR 结果仍可复用。

## 运行日志

应用启动时由 `Logger` 安装 Qt 消息处理器，`qDebug / qInfo / qWarning / qCritical`
的输出统一写入平台惯例的日志目录，同时镜像到 stderr 便于终端调试：

- **macOS**：`~/Library/Logs/LiveMix Studio/`（不写入 `.app` 包内，安装到 `/Applications` 后仍可写）
- **Windows**：`%LOCALAPPDATA%\LiveMix\LiveMix Studio\logs\`（安装目录 `Program Files` 对普通用户只读）
- **其它平台**：进程所在目录下的 `logs/`
- 每天一个文件，以本地日期命名：`2026-10-24.log`；跨天运行时自动切换到新文件。
- 最多保留 30 个日期日志文件，超出后按日期删除最旧的。
- 可通过环境变量 `LIVEMIX_LOG_DIR` 覆盖日志目录。
- 日志行格式：`2026-10-24 21:30:05.123 [WARN] 消息 (文件:行号)`。

## 构建

### 一键编译（推荐）

**macOS / Linux**

```bash
./scripts/build.sh                    # Debug 构建
./scripts/build.sh release            # Release 构建
./scripts/build.sh release --clean    # 清理后 Release
./scripts/build.sh release --run      # 编译后直接启动
```

脚本会自动查找 Homebrew / qmake / `QT_ROOT` 中的 Qt 6，并输出产物路径。
常用参数：

| 参数 | 说明 |
| --- | --- |
| `debug` / `release` | 构建类型，默认 Debug |
| `--clean` | 编译前清理构建目录 |
| `--run` | 编译成功后启动应用 |
| `-j N` / `--jobs N` | 并行编译线程数 |
| `--qt PATH` | 手动指定 Qt 前缀 |
| `--build-dir DIR` | 自定义构建目录，默认 `build` |
| `-- <参数>` | 透传额外 CMake 参数 |

**Windows**

```bat
:: 默认 Debug
scripts\build.bat

:: Release
scripts\build.bat -Configuration Release

:: 清理后 Release
scripts\build.bat -Configuration Release -Clean

:: 编译后直接启动
scripts\build.bat -Configuration Release -Run
```

`build.bat` 是双击友好的入口，实际逻辑在 `build.ps1`。脚本会尝试从
`C:\Qt\<version>\msvc*_64`、`QT_ROOT`、`QTDIR` 查找 Qt，也可以显式指定：

```bat
scripts\build.bat -QtPath C:\Qt\6.12.0\msvc2022_64
```

### 打包 macOS DMG

打包前先下载固定版本的静态 FFmpeg（二进制不会提交到仓库）：

```bash
./scripts/fetch-ffmpeg.sh
```

FFmpeg 默认从项目 COS 镜像
`https://ffmpeg-1426793176.cos.ap-guangzhou.myqcloud.com/ffmpeg` 下载；
如需切换镜像，设置 `LIVEMIX_FFMPEG_MIRROR`（详见 `ffmpeg/README.md`）。

DMG 脚本会自动构建 Release 版本、部署 Qt 框架与插件、把 `ffmpeg` / `ffprobe`
放入应用包、补充 ad-hoc 签名，并生成带 Applications 快捷方式的拖拽安装包：

```bash
./scripts/pack-macos.sh
```

默认产物为 `dist/LiveMixStudio-<version>-<arch>.dmg`，同时生成对应的 `.sha256` 校验文件。
可用参数：

| 参数 | 说明 |
| --- | --- |
| `--clean` | 打包前清理 Release 构建目录 |
| `--skip-build` | 跳过编译，直接打包已有的 `build-dmg/LiveMixStudio.app` |
| `-j N` / `--jobs N` | 并行编译线程数 |
| `--qt PATH` | 手动指定 Qt 前缀 |
| `--ffmpeg-dir DIR` | 包含 `ffmpeg` / `ffprobe` 的目录，默认自动查找 `third_party/ffmpeg/macos-<arch>/bin` |
| `--build-dir DIR` | 构建目录，默认 `build-dmg` |
| `--output DIR` | DMG 输出目录，默认 `dist` |
| `--sign IDENTITY` | 指定 codesign 签名身份 |
| `--keep-staging` | 保留临时部署目录，便于排查打包问题 |

示例：

```bash
# 干净打包
./scripts/pack-macos.sh --clean

# 复用上次编译结果重新打包
./scripts/pack-macos.sh --skip-build

# Developer ID 签名
./scripts/pack-macos.sh \
  --sign "Developer ID Application: Your Company (TEAMID)"
```

需要 macOS、Xcode Command Line Tools 和 `macdeployqt`。默认使用 ad-hoc 签名，适合本地安装；
正式对外分发建议使用 Developer ID 签名并完成 Apple 公证。

### 打包 Windows 安装包

```powershell
# 1. 下载固定版本的静态 FFmpeg（默认从项目 COS 镜像下载）
#    可设置 $env:LIVEMIX_FFMPEG_MIRROR 覆盖镜像地址（见 ffmpeg/README.md）
.\scripts\fetch-ffmpeg.ps1

# 2. 编译、windeployqt 部署 Qt、生成 NSIS 安装包
.\scripts\pack-windows.ps1 -Clean
```

默认产物为 `dist\LiveMixStudio-<version>-win64-setup.exe`，同时生成 `.sha256` 校验文件。
安装包内包含 `LiveMixStudio.exe`、Qt 运行库以及 `ffmpeg.exe` / `ffprobe.exe`。
需要 Qt 6.12.0 的 MSVC 套件与 NSIS 3（CI 会按需安装）。

### 持续集成

`.github/workflows/ci.yml` 在 push、tag、Pull Request 和手动触发时构建：

- macOS x64 / arm64 DMG（`macos-15-intel`、`macos-15`）
- Windows x64 NSIS 安装包（`windows-latest`）

工作流在打包前通过 `scripts/fetch-ffmpeg.*` 从项目 COS 镜像下载固定版本（当前 FFmpeg 6.1.1）
的静态二进制，校验 SHA-256 后交给 CMake 复制进应用包。`third_party/ffmpeg/` 已加入
`.gitignore`，二进制不会入库；同一次构建的产物可通过 Actions 的 Artifacts 下载。

### 手动构建

**macOS**

```bash
brew install qt cmake
cmake -S . -B build -DCMAKE_BUILD_TYPE=Release
cmake --build build -j
open build/LiveMixStudio.app
```

如果 CMake 找不到 Qt：

```bash
cmake -S . -B build -DCMAKE_PREFIX_PATH="$(brew --prefix qt)"
```

**Windows**

以 Qt 6.12.0 的 MSVC 套件为例：

```powershell
cmake -S . -B build -G "Visual Studio 17 2022" -A x64 `
  -DCMAKE_PREFIX_PATH=C:\Qt\6.12.0\msvc2022_64
cmake --build build --config Release
.\build\Release\LiveMixStudio.exe
```

项目的 `/utf-8` 编译选项保证中文界面源码在 MSVC 下正常渲染。
发布时使用 `windeployqt`（Windows）或 `macdeployqt`（macOS）收集运行时依赖。

## 目录结构

```text
.
├── CMakeLists.txt
├── .github/workflows/ci.yml         # GitHub Actions：macOS DMG + Windows 安装包
├── scripts/
│   ├── build.sh                     # macOS / Linux 一键构建
│   ├── build.ps1                    # Windows PowerShell 构建逻辑
│   ├── build.bat                    # Windows 双击入口
│   ├── pack-macos.sh                # macOS DMG 打包
│   ├── fetch-ffmpeg.sh              # 下载 macOS 静态 FFmpeg
│   ├── fetch-ffmpeg.ps1             # 下载 Windows 静态 FFmpeg
│   ├── LiveMixStudio.nsi            # NSIS 安装包模板
│   └── pack-windows.ps1             # Windows NSIS 安装包
├── src/
│   ├── main.cpp                     # 应用入口 / 开发预览参数
│   ├── app/
│   │   ├── ApplicationStyle.*       # 设计 Token、字体、全局样式
│   │   └── MainWindow.*             # 窗口框架、页面路由、弹窗编排
│   ├── api/
│   │   ├── HttpClient.*             # 工作线程内同步 HTTP / 上传 / 下载
│   │   ├── AsrClient.*              # 网关豆包 ASR（audio/transcriptions）
│   │   ├── LlmClient.*              # LLM 网关选片（chat/completions）
│   │   └── CapCutMateClient.*       # 剪映小助手上传、建草稿、加素材与下载
│   ├── core/
│   │   ├── AppConfig.*              # JSON 配置加载/保存
│   │   ├── AutoStart.*              # macOS LaunchAgent / Windows Run 键
│   │   ├── Logger.*                 # 按天滚动日志 / 30 文件上限
│   │   ├── MediaUtils.*             # 时长、目录大小、文件名工具
│   │   └── ServiceEndpoints.*       # 内置服务地址（剪映小助手，不可配置）
│   ├── data/
│   │   ├── ClipTypes.*              # ASR / 切片 / 草稿领域模型与 JSON 编解码
│   │   ├── MediaRepository.*        # SQLite 业务数据访问（源视频 / ASR 缓存 / 草稿）
│   │   └── MediaTypes.h             # 源视频 / 录制会话模型
│   ├── media/
│   │   ├── FfmpegLocator.*          # 定位内置 ffmpeg / ffprobe
│   │   ├── FfmpegUtils.*            # 合成完整 MP4、提取 MP3、按时间裁剪视频
│   │   ├── MediaProbe.*             # ffprobe 元数据读取
│   │   └── RecordingService.*       # mp4 直链下载 / HLS 录制与 ffmpeg 进程管理
│   ├── services/
│   │   ├── LiveClipService.*        # 一键成片流水线（MP4 → ASR → LLM → 裁剪 → 草稿）
│   │   ├── CaptionTiming.*          # 字幕按标点断句 + 词级时间对齐
│   │   └── DraftDownloader.*        # 点击「打开草稿文件夹」后的草稿下载与本地化
│   ├── ui/
│   │   ├── Icons.*                  # SVG 图标绘制
│   │   ├── Controls.*               # 按钮、卡片、Chip、输入框、进度条等
│   │   ├── WindowChrome.*           # 标题栏、侧边栏、状态栏、页面基类
│   │   ├── Overlays.*               # 模态层、全屏编辑器
│   │   └── VideoPreview.*           # 应用内视频播放器（QMediaPlayer）
│   ├── pages/                       # 八个独立业务页面
│   └── model/
│       ├── DefaultPrompts.h         # 从 live-mixer 后端导入的默认选片提示词
│       ├── GarmentPrompts.h         # 服装切片场景的 3 个代表性历史版本提示词
│       └── DemoData.h               # 提示词页面的演示数据
└── third_party/ffmpeg/              # 构建时下载的静态二进制（gitignore）
```

### 后续接功能的位置

- **页面导航**：`MainWindow::navigateTo`
- **弹窗/全屏编排**：`MainWindow::openAddSource`、`openStartPipeline`、`openPromptEditor`
- **页面业务联动**：各 Page 对外只暴露语义化 signals，不直接访问其它页面
- **线上源与配置**：`RecordingService`（HTTP 下载 / ffmpeg 进程）、`ConfigStore`（JSON 配置）、
  `MediaRepository`（SQLite 业务数据）
- **一键成片**：`LiveClipService` 负责流水线编排，`CapCutMateClient` / `AsrClient` /
  `LlmClient` 负责外部接口；页面只通过信号接收进度与结果
- **其余真实数据**：源视频库、任务中心、成片结果与工作台已接入 `MediaRepository`；
  提示词管理仍使用 `DemoData.h`（财经类直播提示词已从后端导入），
  可继续用业务服务替换静态组装逻辑
- **统一视觉**：修改 `Theme` 中的设计 Token，避免页面内散落颜色值

## 开发预览参数

为了方便逐页截图和视觉回归，应用支持以下隐藏参数：

```bash
# 指定页面
LiveMixStudio --page workspace

# 打开页面并展示某个弹窗或提示词页面
LiveMixStudio --modal add        # add | add-local | start | prompt | viewer
LiveMixStudio --page prompt --modal prompt   # 新建提示词

# 打开全屏提示词编辑器（edit / view）
LiveMixStudio --fullscreen edit

# 直接输出窗口截图后退出；--screenshot-delay 可延后截图（毫秒）
LiveMixStudio --page material --screenshot /tmp/material.png
LiveMixStudio --page material --click "一键成片,开始" \
  --screenshot /tmp/pipeline.png --screenshot-delay 3000

# 添加源视频弹窗预选一个本地文件，验证缩略图预览
LiveMixStudio --modal add-local --local-file /path/demo.mp4 \
  --screenshot /tmp/add-local.png

# 查看流水线第 3 步
LiveMixStudio --pipeline-step 3

# 按顺序触发多个按钮 / 滚动到页面底部（用于视觉回归）
LiveMixStudio --page settings --click "通用"
LiveMixStudio --page spec --scroll-bottom
```
