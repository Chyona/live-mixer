# 与 LiveMix Studio 客户端的 1:1 对照清单

本技能的基准是 LiveMix Studio 客户端（Qt/C++）源码，而不是二手文档。
下表把客户端实现逐文件映射到 Python 实现，便于审计与回归。

## 1. 模块映射

| 客户端（LiveMix/src） | 技能（skill-live/scripts） | 说明 |
| --- | --- | --- |
| `core/ServiceEndpoints.cpp` | `capcut.py`、`assets.py` | 服务基地址与路径前缀 |
| `core/AppConfig.{h,cpp}` | `config.py` | 配置字段、默认值、归一化 |
| `data/MediaRepository.*` | `store.py` | 素材/录制/ASR/草稿表 |
| `data/ClipTypes.*` | `asr.py`、`caption.py` | 转写、词级时间线、字幕 |
| `media/RecordingService.*` | `recording.py` | 录制/下载/导入 |
| （DouyinResolve 内嵌） | `douyin.py` | 抖音直播间解析 |
| `media/MediaProbe.*` | `probe.py` | ffprobe 探测与视频流校验 |
| `media/FfmpegUtils.*` | `ffmpeg.py` | MP3、裁剪、HLS 合成、缩略图 |
| `api/AsrClient.*` | `asr.py` | 豆包 ASR |
| `api/LlmClient.*` | `llm.py` | 选片、音效、翻译 |
| `api/AssetsClient.*` | `assets.py` | BGM/图片/音效 + HMAC |
| `api/CapCutMateClient.*` | `capcut.py`、`draft_download.py` | 剪映小助手接口与草稿下载 |
| `services/CaptionTiming.*` | `caption.py` | 断句、词级对齐、均衡折行 |
| `services/LiveClipService.*` | `pipeline.py` | 一键成片编排与草稿生成 |
| `services/VideoExporter.*` | `video_export.py` | 云端导出成片 |
| `model/DefaultPrompts.h`、`GarmentPrompts.h`、`PromptCatalog.*` | `prompts.py` | 4 套内置提示词 |
| `ui/*` | `cli.py` | 命令行替代界面与过程页 |

## 2. 逐项一致性

### 2.1 服务与模型

- [x] 剪映小助手基地址 `https://capcut-mate.jcaigc.cn`，前缀
      `/openapi/capcut-mate/v1`；
- [x] 素材服务 `https://assets.gogoshine.com/openapi/assets/v1`，
      内置 access/secret 与 HMAC 规范；
- [x] LLM 模型 `qwen3.8-flash`，ASR 模型 `volc.seedasr.auc`；
- [x] 剪映小助手业务错误看 `code`，HTTP 200 也可能是错误；
- [x] 所有剪映小助手素材先 `upload_file` 换 URL；
- [x] 时间单位微秒；列表字段为 JSON 字符串；`scene_timelines` 为数组。

### 2.2 录制

- [x] 在线链接嗅探：`Range: bytes=0-8191`、8 秒超时、8192 字节上限；
- [x] HLS/MIME/`ftyp` 分类与扩展名兜底；
- [x] 直链：`.part` 原子落盘、60 秒空闲超时、长度校验、ffprobe 视频流校验；
- [x] 非 AAC 音轨只转码音频：`-c:v copy -c:a aac -b:a 128k -movflags +faststart`；
- [x] HLS 录制参数：`-c copy -f hls -hls_time 4 -hls_playlist_type event
      -hls_segment_filename segment_%05d.ts index.m3u8`；
- [x] 停止：stdin `q` + 默认 8 秒等待 + kill；
- [x] 目录命名 `<yyyyMMdd-HHmmss>-<标题>`，冲突加 `-2…`；
- [x] 抖音：页面 UA/Referer、`flv_pull_url` → `hls_pull_url_map` →
      `hls_pull_url` → 任意 m3u8，清晰度 HD1/FULL_HD1/SD1/SD2；
- [x] 抖音流 8 秒探测，403/≥400/2xx 语义一致；
- [x] 本地导入扩展名、≤10GB、必须真实视频流、只登记不复制。

### 2.3 一键成片

- [x] HLS 先合成 `index.mp4`，复用 4 个候选 MP4，容差 2s/6s；
- [x] MP3：16kHz 单声道 64kbps，`aresample=async=1:first_pts=0,apad`、
      `-t 源时长`，偏差 >500ms 失败；
- [x] ASR 缓存指纹 `asr-v2:<size>:<mtime_ms>`；
- [x] ASR multipart 字段与 35 分钟超时；
- [x] 条数公式 `[ceil(总时长/30min), floor(总时长/5min)]`；
- [x] 等长时间窗；搜索半径 `min(5min, max(1s, 窗口/10))`；
- [x] 句号优先、最近边界兜底、索引单调、剩余段至少一句；
- [x] LLM 请求参数（temperature 0.2、max_tokens 4000、SSE、usage）；
- [x] 429/5xx 三次、网络两次；严格 JSON 二次尝试；
- [x] 索引去重、越界忽略、空窗跳过；
- [x] 合并间隔 ≤500ms、保留模型顺序；
- [x] 标题 12 字、描述 128 字、话题去 #/去重/≤6；
- [x] 裁剪参数（-ss 前置、libx264 veryfast crf20 yuv420p、AAC 128k、faststart）；
- [x] 切片原速，倍速通过 `scene_timelines`；
- [x] 自动倍速 `1.05 + 0.01 × rand(46)`；固定倍速 clamp 0.5–2.0；
- [x] 画布 1920×1080 / 1080×1920 按第一段探测结果选择；
- [x] 切片时长与请求偏差 >2.5s 时用请求时长。

### 2.4 草稿

- [x] `create_draft` 后所有写接口原样透传 `draft_url`；
- [x] 主轨累计时间、scene 区间、`scale_x/scale_y`；
- [x] 去重时逐段随机 `[1.0, 1.1)`，转场对共用缩放；
- [x] 转场随机一对相邻切片（两段均 >800ms），时长 800ms，35 种候选；
- [x] 字幕：≤15 字、标点/数字分隔、词级对齐、比例兜底、50ms 过滤、
      跨切片重叠保护；
- [x] 12 套字幕样式与 `(0,-800)`；
- [x] 35 种特效、3–5 秒窗口、咔嚓 0.1–1 秒；
- [x] 16 张贴纸（ID/坐标/缩放）、3–10 秒窗口；
- [x] 10 种滤镜、强度 1–30、覆盖全片；
- [x] 去重图片 5 张/20 秒/3% 透明度/随机坐标；
- [x] 13 种音效、usage hint、约 5 处/分钟上限 8、音量 0.1、
      同 URL 重叠去重、重叠分批；
- [x] BGM 随机、音量 0.1、短于成片按整首循环；
- [x] 中英文双字幕：Inter-SemiBold、字号 −2、位置公式、按 transform_y 分批；
- [x] 落库字段：draft_id/url、标题、描述、话题、起止、时长、下载路径。

### 2.5 导出与下载

- [x] `gen_video` 后 1.5 秒首次轮询，之后 3 秒 × 600 次；
- [x] completed/failed/pending/processing 处理；
- [x] 成片下载到 `<导出目录>/<标题>.mp4`，1 小时超时；
- [x] `get_draft` 文件列表；
- [x] `.livemix-staging` 暂存 + 原子改名；
- [x] 素材相对路径 `assets/<videos|audios|images>/<name>_<hash>.<suffix>`；
- [x] 占位符
      `##_draftpath_placeholder_0E685133-18CE-45ED-8CB8-2904A212EC80_##`；
- [x] `config.material_save_mode = 1`；
- [x] `draft_meta_info.json` 的 name/fold/root 与 `file_Path` 改写；
- [x] BOM 提示文件「【剪映草稿】请用剪映打开查看成片.txt」；
- [x] 失败保留暂存目录、重试复用已下载文件。

## 3. 已知差异（均不影响成片/草稿可用性）

| 差异 | 说明 |
| --- | --- |
| 本地素材文件名的 6 位防重名后缀 | 客户端用 Qt `qHash`（Qt 6.12 为 AES 加速哈希，属实现细节）；技能用 SHA-256 前 6 位。同一 URL 稳定、不重名；草稿内路径与播放不受影响。 |
| 状态库/配置默认位置 | 客户端在 Qt `AppDataLocation`；技能默认 `~/.livemix/livemix.db` 与 `~/.livemix/config.json`，可用 `--db` / `--config` / 环境变量对齐。SQLite schema v4 与客户端一致，ASR 缓存 JSON 也使用客户端的 `start_time/end_time` 词级格式。 |
| 界面 | 客户端有过程页、波形、实时思考面板；技能用 stderr 文本进度与 stdout JSON。业务逻辑一致。 |
| 随机序列 | 客户端用 `QRandomGenerator`，技能默认用系统随机源；分布与取值规则相同，具体抽取结果不同。二者默认都不保证跨运行复现；技能的 `--seed` 仅用于调试。 |
| HLS 预览缓存 | 客户端会复用播放器生成的完整 MP4 预览缓存；技能跳过该缓存，但仍会检查 4 个录制目录候选 MP4，再无损合成。最终输入相同。 |
| 抖音反爬/页面结构 | 解析规则同客户端；抖音页面结构或风控变化时两者都可能失效，需要同步更新解析。 |
| SRT 导出 | 客户端设置中保留 `ai.exportSrt`；技能提供 `transcribe --output` 的逐句 JSON，需要 SRT 时可在 Agent 侧转换，未内置 SRT 写出。 |

## 4. 如何验收 1:1

1. **纯逻辑**：`python3 scripts/livemix.py selftest`，18 项全部通过；
2. **媒体处理**：用同一 mp4 运行 `clip` 与客户端，对比：
   - 切片起止时间与时长（允许编码取整毫秒差）；
   - 成片时长 = `Σ round(切片时长/倍速)`；
   - 字幕条数、文本、起止时间；
   - `add_videos` / `add_captions` / `add_audios` 请求字段；
3. **草稿结构**：分别用 `download-draft` 与客户端「导出到剪映」下载同一
   `draft_id`，对比：
   - `draft_content.json` 的轨道、片段、时间、样式（随机项可能不同）；
   - `materials.*[].path` 的占位符相对路径；
   - `draft_meta_info.json` 的目录字段；
   - 剪映能否直接打开且素材完整。
4. **随机项**：转场/特效/贴纸/滤镜/BGM/字体/倍速/去重图片本身是随机选择，
   验收时应比较「候选池、数量、参数范围、写入位置规则」，而不是要求两次
   运行抽到同一项。需要固定时可给技能传 `--seed`。
