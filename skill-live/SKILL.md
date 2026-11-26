---
name: livemix-studio
description: 当用户想把一场直播或一段本地视频变成可直接发布的短视频切片时使用：录制直播流 / 抖音直播间、下载 mp4 直链、导入本地视频，整片语音转写，按提示词让 AI 选出高光句段并精确裁剪，生成可在剪映中继续编辑的剪映草稿，或直接云端导出 MP4 成片，并把草稿按需安装到本机剪映草稿箱。本技能逐参数复刻开源客户端 LiveMix Studio 的「直播录制 → 一键成片 → 剪映草稿 / 成片导出」完整能力，脚本全部使用 Python 标准库。触发词：直播切片、直播录像切片、直播回放、一键成片、直播录制、抖音直播录制、抖音直播间、HLS 录制、本地视频切片、AI 选片、高光切片、ASR 转写、语音转字幕、剪映草稿、生成剪映草稿、导出到剪映、导出成片、剪映草稿下载、草稿箱、批量切片、LiveMix、开源直播切片助手。
description_zh: 当用户想把一场直播或一段本地视频变成可直接发布的短视频切片时使用：录制直播流 / 抖音直播间、下载 mp4 直链、导入本地视频，整片语音转写，按提示词让 AI 选出高光句段并精确裁剪，生成可在剪映中继续编辑的剪映草稿，或直接云端导出 MP4 成片，并把草稿按需安装到本机剪映草稿箱。本技能逐参数复刻开源客户端 LiveMix Studio 的「直播录制 → 一键成片 → 剪映草稿 / 成片导出」完整能力，脚本全部使用 Python 标准库。触发词：直播切片、直播录像切片、直播回放、一键成片、直播录制、抖音直播录制、抖音直播间、HLS 录制、本地视频切片、AI 选片、高光切片、ASR 转写、语音转字幕、剪映草稿、生成剪映草稿、导出到剪映、导出成片、剪映草稿下载、草稿箱、批量切片、LiveMix、开源直播切片助手。
description_en: "Turn a live stream or local video into publish-ready short clips exactly like the open-source LiveMix Studio desktop client: record HLS/RTMP/HTTP streams and Douyin live rooms, download direct MP4 links, import local videos, transcribe the full audio with Doubao ASR, let an LLM pick highlight segments, cut them with ffmpeg, build an editable Jianying/CapCut draft with captions, transitions, effects, filters, stickers, BGM, SFX, bilingual subtitles and de-duplication, install the draft into the local Jianying draft folder, or export the final MP4 through CapCut Mate. Python-only scripts, parameter-for-parameter parity with the LiveMix client. Triggers: live clip, stream recording, Douyin live, one-click edit, Jianying draft, CapCut draft, export video, LiveMix."
slug: livemix-studio
version: 1.0.0
display_name: 开源直播切片助手
display_name_en: Open-Source Live Clip Assistant
summary: 逐参数复刻开源客户端 LiveMix Studio 的直播切片能力：直播录制 / 抖音直播间 / 本地素材 → 整片 ASR → AI 选片 → ffmpeg 精确裁剪 → 剪映草稿或 MP4 成片；脚本全部为 Python 标准库实现，并与客户端保持 1:1 的成片与草稿生成规则。
tags:
  - 直播切片
  - 直播录制
  - 抖音直播
  - 一键成片
  - 剪映草稿
  - 剪映
  - CapCut
  - ASR
  - 语音转写
  - AI选片
  - ffmpeg
  - 短视频
  - LiveMix
  - 开源
license: Apache-2.0
homepage: https://gogoshine.com
repository: https://github.com/Chyona/live-mixer
disable: false
---

<p align="center">
  <img src="_icon.png" width="88" alt="LiveMix Studio">
</p>

# 开源直播切片助手

> **把一场直播，变成几十条可直接发布的切片。**
> 本技能是开源客户端 **[LiveMix Studio](https://github.com/Chyona/live-mixer)** 的脚本化实现：
> 直播录制 / 本地素材 → 整片转写 → AI 选片 → 精确裁剪 → 剪映草稿 / MP4 成片。
> 脚本只用 **Python 3.9+ 标准库**，不依赖 Node.js。

---

## 这个技能能做什么

| 能力 | 说明 | 对应命令 |
| --- | --- | --- |
| **直播录制** | HTTP(S) 直播/HLS、RTMP/RTSP、mp4/mov 直链原文件下载；HLS 以分片形式落盘，崩溃/中断后可恢复 | `record` |
| **抖音直播间** | 粘贴 `live.douyin.com/<房间号>`、`v.douyin.com` 短链或纯房间号，自动解析带签名的 FLV/HLS 流并确认可播放 | `record` |
| **本地素材** | 拖入/指定本地视频，校验视频流与大小（≤10GB），只登记原文件不复制 | `import` |
| **整片转写** | 提取 16kHz 单声道 MP3 → 剪映小助手 `upload_file` → 豆包 `volc.seedasr.auc` 分句 + 逐词时间线；结果写入本地 SQLite 缓存，重复运行不重复计费 | `transcribe` / `clip` |
| **AI 选片** | 按提示词把整场转写切分为 5–30 分钟时间窗，大模型返回句段索引 + 标题/描述/话题 | `clip` |
| **剪映草稿** | 裁剪切片 → 上传 → `create_draft` → 主轨（含倍速/转场）→ 字幕 → 音效/BGM/双语字幕 → 保存；支持自动特效、滤镜、贴纸、平台去重 | `clip` |
| **导出到剪映** | 按需下载草稿与素材，改写为剪映占位符路径，原子发布到本机剪映草稿目录，剪映无需重启即可收录 | `download-draft` |
| **导出成片** | 调用剪映小助手 `gen_video` 云端合成 MP4，轮询进度并下载到本地 | `export` |
| **诊断与自检** | 检查 ffmpeg/ffprobe、配置、密钥连通性；运行 18 项纯逻辑自检 | `doctor` / `selftest` |

**1:1 范围**：成片与草稿的生成规则逐参数对齐 LiveMix Studio 客户端，包括服务地址、模型名、
ffmpeg 参数、分段与合并算法、字幕断句与时间对齐、画布选择、倍速、随机素材池、音量、
去重参数、草稿下载本地化流程。详见
[references/parity-checklist.md](references/parity-checklist.md)。

---

## 给 Agent 的执行流程

1. **确认技能根目录**：以下命令均以技能根目录为当前目录；若不在，使用绝对路径
   `python3 <skill-dir>/scripts/livemix.py ...`。
2. **先诊断**：首次使用或失败后运行 `doctor --ping`；纯逻辑疑问运行 `selftest`。
3. **判断用户处于哪一步**：

| 用户意图 | 执行 |
| --- | --- |
| 粘贴直播链接 / 抖音房间号 / 视频直链 | `record`（长时间录制先确认时长与磁盘，必要时 `--max-duration`） |
| 有本地视频文件 | `import`（只登记）或直接 `clip --input` |
| 「一键成片 / 切几条 / 挑高光」 | `clip`；用户未指定条数时用自动，未指定提示词时用配置默认 |
| 「导出到剪映 / 下载草稿 / 打开草稿」 | `download-draft` |
| 「导出 MP4 / 直接出片」 | `export` |
| 「只要转写 / 字幕稿」 | `transcribe` |
| 「查看素材库 / 找 source_id」 | `sources` |
| 「查看之前生成的草稿」 | `drafts` |
| 「换一种风格 / 重新剪」 | 重新 `clip`，不要改已下载的草稿 JSON |

4. **如实转述结果**：把 `draft_url`、`local_path`、`output_path`、`duration`、
   `packaging`、失败原因原样告诉用户，不要编造成功或路径。
5. **付费提示**：`upload_file`、`gen_video`、ASR、LLM 都会产生费用。
   在提交长素材（如整场几小时直播）或批量导出前，先向用户说明会产生费用；
   用户未确认时不要自行批量循环导出。
6. **不要替用户做内容决策**：标题/描述/话题来自模型，事实性内容提醒用户发布前复核；
   合规红线（金融建议、绝对化宣传、品牌侵权等）以提示词与人工复核为准。

---

## 硬规则（必须遵守）

1. **只使用本技能脚本生成 / 下载草稿**。下载到本机的剪映草稿文件由剪映管理，
   不要手改时间线、素材路径或 `draft_meta_info.json`；需要调整就重新 `clip`
   生成新草稿。唯一允许的本地化改写由 `download-draft` 完成。
2. **时间单位**：脚本内部与文件 API 用毫秒；提交给剪映小助手的时间全部换算为
   **微秒**（1 秒 = 1,000,000），例如 5 秒 = `5000000`。
3. **固定服务地址与模型**：
   - 剪映小助手：`https://capcut-mate.jcaigc.cn/openapi/capcut-mate/v1/`（编译期内置，不提供改地址入口）；
   - LLM / ASR 网关：默认 `https://api.jcaigc.cn/v1`；
   - 选片模型：`qwen3.8-flash`；ASR：`volc.seedasr.auc`；
   - 不要自行替换域名、模型或把 ASR 拼到剪映小助手地址上。
4. **先 `create_draft`**，后续每个写接口都原样透传返回的 `draft_url`，禁止自行拼接 `draft_id`。
5. **列表字段是 JSON 字符串**：`video_infos`、`image_infos`、`captions`、`audio_infos`、
   `effect_infos`、`filter_infos` 提交时是序列化后的字符串；`scene_timelines` 例外，是数组。
6. **成片数量受素材时长约束**：允许条数 `[ceil(总时长/30分钟), floor(总时长/5分钟)]`；
   自动模式取最少条数。数量过密会被平台判重复，过少会浪费素材。脚本会二次校验。
7. **HLS 录像先合成 MP4**：录制产物是 `index.m3u8 + segment_*.ts`；一键成片前先无损合成
   `index.mp4`（已存在且完整则复用），之后的音频提取、ASR、裁剪都按 MP4 处理。
8. **ASR 缓存指纹**为 `asr-v2:<文件字节数>:<修改时间毫秒>`。同一源文件重复 `clip`
   会直接复用缓存，不重复上传与计费；删除缓存（`--db` 指向的 SQLite）才会重新识别。
9. **字幕规则**：每条 ≤15 个字；按中英文标点断句但保留数字中的小数点/冒号/连字符；
   优先使用 ASR `words` 词级时间线，对不上时整句退回按字数分摊；中文位置
   `transform_x=0 / transform_y=-800`；中英双语时英文轨自动下移（单行时 `-920`）。
10. **随机性**：默认与客户端一样使用系统随机源（每条草稿的倍速、字体、转场、特效、
    贴纸、滤镜、BGM、音效、去重位置均独立随机）。`--seed N` 仅用于调试复现，
    不是客户端能力；不要用它伪装成「固定模板」。
11. **付费接口**：`upload_file` 与 `gen_video` 由剪映小助手按用量收费；ASR 与大模型
    由网关按用量计费。调用前应向用户说明会产生费用；具体单价以服务方页面为准。
    技能本体完全开源免费（Apache-2.0）。
12. **密钥安全**：`--api-key` / `LIVEMIX_API_KEY` / `config.json` 中的 `service.apiKey`
    均可提供密钥；`init` 写出的配置文件权限为 `0600`。不要把密钥写进提示词、
    提交到 Git 或发到外部服务。
13. **不要伪造结果**：`clip` 输出中的 `draft_url` 来自服务端；`download-draft` 的
    `local_path` 必须真实存在；`export` 的 `output_path` 必须真实落盘。失败时如实报告。

---

## 快速开始

### 0. 前置条件

- **Python 3.9+**（不需要安装任何 pip 包）；
- **ffmpeg / ffprobe**：已加入 `PATH`，或用 `LIVEMIX_FFMPEG_DIR` / 配置文件
  `recording.ffmpegPath` 指定；
- **API 密钥**：登录 <https://jcaigc.cn> 获取，同一密钥用于 LLM、ASR 与剪映小助手上传。

```bash
# 检查环境；--ping 会请求 <API 地址>/models 验证密钥
python3 scripts/livemix.py doctor --ping

# 可选：生成默认配置（~/.livemix/config.json），再填写 service.apiKey
python3 scripts/livemix.py init
```

也可以直接用环境变量：

```bash
export LIVEMIX_API_KEY="你的密钥"
# 可选：export LIVEMIX_API_URL="https://api.jcaigc.cn/v1"
```

### 1. 获取素材

```bash
# 抖音直播间（链接、短链或纯房间号）
python3 scripts/livemix.py record "https://live.douyin.com/123456" --title "今晚直播"

# HLS / 直播流 / 直链（mp4 直链会原文件下载）
python3 scripts/livemix.py record "https://example.com/live/index.m3u8" \
  --max-duration 1800

# 本地视频（只登记，不复制）
python3 scripts/livemix.py import "./demo.mp4"
```

> `record` 默认一直录到 Ctrl+C（或 `--max-duration` 到点）。录制完成后输出
> `material.local_path` 与 `material.source_id`；后续可用 `--source-id` 复用。

### 2. 一键成片

```bash
# 自动条数（每条最多使用 30 分钟素材），默认提示词
python3 scripts/livemix.py clip --input "./demo.mp4" --prompt "默认提示词"

# 指定 3 条，使用财经提示词，固定 1.2 倍速
python3 scripts/livemix.py clip --input "./demo.mp4" \
  --prompt "财经类直播" --clips 3 --speed 1.2

# 对已录制 / 已导入的素材复用 source_id
python3 scripts/livemix.py clip --source-id 2 --prompt "服装切片 · 黄金3秒定稿"

# 关闭自动包装（只要主轨 + 字幕）
python3 scripts/livemix.py clip --input "./demo.mp4" \
  --no-transitions --no-effects --no-filters --no-stickers \
  --no-bgm --no-sfx --no-dedup
```

输出 JSON 的 `drafts[]` 包含 `draft_url`、`title`、`description`、`tags`、`duration_ms`、
`canvas`、`packaging` 与可直接执行的导出/下载提示命令。

### 3. 交付

```bash
# 导出到本机剪映草稿箱（点击「导出到剪映」的脚本等价能力）
python3 scripts/livemix.py download-draft \
  --draft-url "https://.../draft?draft_id=..." --title "切片标题"

# 直接导出 MP4 成片（云端合成）
python3 scripts/livemix.py export \
  --draft-url "https://.../draft?draft_id=..." --title "切片标题"
```

> 两者可以独立使用：只要 MP4 不需要安装剪映；需要继续精修时下载草稿。

---

## 命令一览

| 命令 | 作用 | 关键参数 |
| --- | --- | --- |
| `init` | 写入默认配置 | `--force` |
| `doctor` | 检查环境与密钥 | `--ping` |
| `record <source>` | 录制 / 下载 / 抖音解析 | `--title` `--max-duration` `--output-dir` `--segment-seconds` `--no-reconnect` |
| `import <path>` | 导入本地视频 | `--title` `--note` |
| `clip` | 一键成片并生成草稿 | `--input` / `--source-id`、`--prompt`、`--clips`、`--speed`、`--seed`、8 组 `--[no-]…` 开关、`--json-out`、`--keep-work` |
| `transcribe` | 只做整片 ASR（带缓存） | `--input` / `--source-id`、`--output` |
| `download-draft` | 下载并安装剪映草稿 | `--draft-url` / `--draft-id` / `--row-id`、`--draft-root` |
| `export` | 云端导出 MP4 | `--draft-url` / `--row-id`、`--output-dir`、`--resume-url` |
| `sources` | 列出素材库 | `--status` |
| `drafts` | 列出草稿记录 | `--source-id` |
| `recover` | 恢复上次异常中断的录制 | — |
| `prompts list/show` | 查看内置提示词 | `list` / `show <名称>` |
| `selftest` | 纯逻辑自检（18 项） | — |

所有命令把**进度写 stderr、结果 JSON 写 stdout**；退出码 `0` 表示成功，`1` 表示失败，
`130` 表示用户中断。完整参数与输出字段见 [references/cli.md](references/cli.md)。

---

## 默认成片口径（与客户端一致）

| 项目 | 默认值 |
| --- | --- |
| 画布 | 按源视频宽高比在 `1920×1080` / `1080×1920` 中选最接近的一档 |
| 倍速 | 自动：每条在 `1.05×–1.50×` 独立随机（步进 0.01）；也可固定 `0.5×–2.0×` |
| 字幕 | 每条 ≤15 字；12 套字体/颜色/描边/入场动画随机；字号 8；位置 `(0, -800)` |
| 双语字幕 | 默认关闭；开启后中文轨下方追加 `Inter-SemiBold`、字号小 2 的英文轨 |
| 转场 | 默认开启；每条草稿随机一对相邻切片、随机一种转场，时长 800ms |
| 特效 | 默认开启；从 35 种候选随机 1 种，贴在 3–5 秒字幕窗口（闪光类 0.1–1 秒） |
| 滤镜 | 默认开启；从 10 种随机 1 种，强度 1–30，覆盖全片 |
| 贴纸 | 默认开启；从 16 张内置贴纸随机 1 张，贴在 3–10 秒字幕窗口 |
| BGM | 默认开启；素材服务随机 1 首，音量 0.1；短于成片时按整首循环铺满 |
| 音效 | 默认开启；13 种内置音效，大模型稀疏选点（约 5 处/分钟，最多 8 处），音量 0.1 |
| 平台去重 | 默认开启；主轨每段随机放大 0–10%；叠加 5 张 20 秒、透明度 3% 的随机图片 |
| 提示词 | `默认提示词`（服装切片结构化模板）；另有财经类与服装历史版共 4 套内置 |

完整规则见 [references/draft-generation.md](references/draft-generation.md)。

---

## 参考文档

| 文档 | 何时阅读 |
| --- | --- |
| [references/cli.md](references/cli.md) | 需要完整命令参数、输出 JSON 结构、批处理示例 |
| [references/configuration.md](references/configuration.md) | 配置字段、默认路径、环境变量、权限 |
| [references/recording.md](references/recording.md) | 直播录制、抖音解析、直链下载、本地导入、HLS 恢复 |
| [references/one-click-pipeline.md](references/one-click-pipeline.md) | 一键成片全流程、数量规则、时间窗切分、LLM 选片、裁剪 |
| [references/draft-generation.md](references/draft-generation.md) | 草稿每个接口的字段、字幕断句、倍速、包装池、音效/BGM/去重 |
| [references/export-and-download.md](references/export-and-download.md) | 导出成片、轮询、草稿下载本地化与原子发布 |
| [references/api-reference.md](references/api-reference.md) | 剪映小助手 / 素材服务 / ASR / LLM 接口与鉴权细节 |
| [references/prompt-catalog.md](references/prompt-catalog.md) | 4 套内置提示词、选择建议、输出契约 |
| [references/parity-checklist.md](references/parity-checklist.md) | 核对与客户端 1:1 的映射清单 |
| [references/troubleshooting.md](references/troubleshooting.md) | 报错与常见问题 |

---

## 目录结构

```
skill-live/
├── SKILL.md                     # 本文件
├── README.md                    # 快速说明
├── LICENSE                      # Apache-2.0
├── _icon.png
├── references/                  # 10 篇参考文档
└── scripts/                     # 纯标准库 Python 脚本（无第三方依赖）
    ├── livemix.py               # CLI 入口
    ├── cli.py                   # 子命令与参数
    ├── config.py                # 配置（对齐客户端 AppConfig）
    ├── store.py                 # SQLite 素材库 / ASR 缓存 / 草稿记录
    ├── recording.py             # 录制 / 下载 / 本地导入
    ├── douyin.py                # 抖音直播间解析
    ├── ffmpeg.py                # ffmpeg / ffprobe 调用
    ├── locator.py               # ffmpeg / ffprobe 定位
    ├── probe.py                 # 媒体探测
    ├── asr.py                   # 豆包 ASR
    ├── llm.py                   # 选片 / 音效选点 / 字幕翻译
    ├── caption.py               # 断句、词级时间对齐、均衡折行
    ├── pipeline.py              # 一键成片主流程
    ├── capcut.py                # 剪映小助手客户端
    ├── assets.py                # 素材服务（BGM / 图片 / 音效，HMAC 签名）
    ├── http_client.py           # 同步 HTTP 客户端（urllib）
    ├── draft_download.py        # 草稿本地化安装
    ├── video_export.py          # 成片导出
    ├── selftest.py              # 18 项纯逻辑自检
    ├── version.py               # 版本与技能元信息
    ├── util.py                  # Qt 语义工具（round / UTF-16 / 文件名清理）
    └── requirements.txt         # 无第三方依赖
```

---

## 常见问题（摘要）

- **`未找到 ffmpeg`**：安装 ffmpeg 并确认 `ffmpeg -version` / `ffprobe -version` 可执行；
  或 `export LIVEMIX_FFMPEG_DIR=/path/to/bin`。
- **`尚未配置 API 密钥`**：设置 `LIVEMIX_API_KEY` 或编辑 `~/.livemix/config.json`。
- **抖音解析不到流**：未开播、已下播、需要登录、链接失效都会导致；脚本会给出明确原因。
- **成片数量超限**：按 `[ceil(时长/30min), floor(时长/5min)]` 调整 `--clips`。
- **草稿在剪映看不到**：确认 `download-draft` 的 `--draft-root` 就是剪映「草稿位置」；
  重新打开剪映或重启一次即可收录。
- **云端合成失败**：`gen_video` 是付费接口，检查密钥额度与草稿是否仍然有效。

完整排查见 [references/troubleshooting.md](references/troubleshooting.md)。

---

## 开源与许可

- 技能本体：**Apache-2.0**，可自由使用、修改与二次开发；
- 上游客户端：<https://github.com/Chyona/live-mixer>（Apache-2.0）；
- 剪映小助手 / 素材服务 / ASR / LLM 为第三方服务，按各自条款与价格计费；
- 请遵守平台内容规范与版权要求，仅处理你有权使用的直播与视频素材。
