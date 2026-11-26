# 命令行参考

入口：`python3 scripts/livemix.py <命令> [选项]`

约定：

- **进度**写入 `stderr`；**结果 JSON** 写入 `stdout`（`--quiet` 可关闭进度）。
- 退出码：`0` 成功，`1` 失败（stdout 仍有 `{"ok": false, "error": ...}`），`130` 用户中断。
- 通用选项（每个子命令都可用）：
  - `--config PATH`：配置文件，默认 `~/.livemix/config.json`；
  - `--api-url URL` / `--api-key KEY`：覆盖网关地址 / 密钥；
  - `--db PATH`：覆盖 SQLite 路径；
  - `--quiet`：不打印进度；
  - `--verbose`：失败时打印 Python 堆栈。
- 通用选项必须写在**子命令之后**，例如
  `python3 scripts/livemix.py clip --input a.mp4 --api-key sk-xxx`。

---

## init

```bash
python3 scripts/livemix.py init [--force]
```

写出默认配置并打印 `config_path`。默认路径已存在时不会覆盖，除非 `--force`。
配置文件权限为 `0600`。

---

## doctor

```bash
python3 scripts/livemix.py doctor [--ping]
```

输出：

```json
{
  "ok": true,
  "config_path": "~/.livemix/config.json",
  "config_exists": true,
  "db_path": "~/.livemix/livemix.db",
  "api_url": "https://api.jcaigc.cn/v1",
  "api_key_configured": true,
  "ffmpeg": "/opt/homebrew/bin/ffmpeg",
  "ffprobe": "/opt/homebrew/bin/ffprobe",
  "paths": {"recording": "...", "export": "...", "draft": "...", "cache": "..."},
  "finish": {"transitions": true, "...": "..."},
  "prompts": ["财经类直播", "服装切片 · 黄金3秒定稿", "默认提示词", "服装切片 · 基础版"]
}
```

`--ping` 会请求 `<api_url>/models`，区分「连接成功 / 密钥无效（401/403）/
服务可达但其它 HTTP 状态 / 网络错误」。任一检查失败时退出码为 1。

---

## record

```bash
python3 scripts/livemix.py record <source> [选项]
```

| 选项 | 默认 | 说明 |
| --- | --- | --- |
| `--title` | 自动 | 素材标题；自动标题形如 `11-25 23:45 直播录制` |
| `--output-dir` | 配置 `storage.recordingDir` | 录制根目录 |
| `--max-duration` | `0` | 最长录制秒数；`0` 表示录到 Ctrl+C |
| `--segment-seconds` | `4` | HLS 分片秒数（1–30） |
| `--no-reconnect` | 关闭 | 是否对 http(s) 流启用 ffmpeg 断线重连 |
| `--stop-timeout` | `8000` | 停止时等待 ffmpeg 收尾的毫秒数 |

`<source>` 支持：

- `https://live.douyin.com/<房间号>`、`v.douyin.com` 短链、4–20 位纯数字房间号；
- `http/https/rtmp/rtmps/rtsp` 地址；
- mp4/mov/m4v 直链（原文件下载）。

输出：

```json
{
  "ok": true,
  "material": {
    "source_id": 1,
    "title": "今晚直播",
    "source_uri": "https://live.douyin.com/123456",
    "local_path": ".../index.m3u8",
    "output_dir": "...",
    "duration_ms": 3600000,
    "size_bytes": 123456789,
    "message": "录制已停止，文件已保存",
    "warning": "",
    "kind": "douyin"
  }
}
```

Ctrl+C 会向 ffmpeg 发送 `q` 并在超时后强制结束；已产生的播放列表会保留。

---

## import

```bash
python3 scripts/livemix.py import <path> [--title T] [--note N]
```

校验扩展名、可读性、单文件 ≤10GB、ffprobe 是否存在真实视频流。
原始文件保留在原位置，SQLite 只登记路径。输出 `material.source_id` 供 `clip` 使用。

---

## clip

```bash
python3 scripts/livemix.py clip (--input PATH | --source-id N) [选项]
```

| 选项 | 默认 | 说明 |
| --- | --- | --- |
| `--input` | — | 源视频路径（mp4 / m3u8 等） |
| `--source-id` | — | 已登记素材 ID（与 `--input` 二选一） |
| `--title` | 文件名 | 仅首次登记素材时使用 |
| `--prompt` | 配置 `ai.defaultPrompt` | 内置名称 / 文件路径 / 含换行的提示词文本 |
| `--prompt-file` | — | 提示词文件（优先于 `--prompt`） |
| `--clips` | 自动 | 目标成片数量；须落在 `[ceil(时长/30min), floor(时长/5min)]` |
| `--speed` | 自动 | 固定倍速 0.5–2.0；省略时按配置自动 1.05–1.50× |
| `--seed` | 系统随机 | 仅调试复现；默认与客户端一样随机 |
| `--keep-work` | 关闭 | 保留音频与切片临时文件 |
| `--json-out PATH` | — | 同时把结果写入文件 |
| `--[no-]transitions` | 配置 | 自动转场 |
| `--[no-]effects` | 配置 | 自动特效 |
| `--[no-]filters` | 配置 | 自动滤镜 |
| `--[no-]stickers` | 配置 | 自动贴纸 |
| `--[no-]bgm` | 配置 | 自动背景音乐 |
| `--[no-]sfx` | 配置 | 自动音效 |
| `--[no-]bilingual-subtitles` | 配置 | 中英文双字幕 |
| `--[no-]dedup` | 配置 | 平台去重 |

输出（字段较长，此处节选）：

```json
{
  "ok": true,
  "source_id": 2,
  "prompt": "默认提示词",
  "count": 3,
  "drafts": [
    {
      "row_id": 1,
      "draft_id": "2026...",
      "draft_url": "https://.../draft?draft_id=2026...",
      "title": "利率上行的真相",
      "description": "用公开数据拆解本轮加息如何影响资产价格与配置思路。",
      "tags": ["宏观经济", "利率周期", "资产配置"],
      "duration_ms": 47250,
      "duration": "00:47",
      "source_start_ms": 125000,
      "source_end_ms": 742000,
      "canvas": [1920, 1080],
      "packaging": ["切片随机放大", "转场「叠化」", "特效「星光」", "滤镜「清透感」", "贴纸「强烈推荐」", "去重图片 5 张", "背景音乐「...」"],
      "caption_style": {"font": "金陵体", "font_size": 8},
      "sfx_count": 3,
      "english_count": 0,
      "notes": {"sfx": "", "bgm": "", "english": ""},
      "export_hint": "python3 scripts/livemix.py export --draft-url '...' --title '...'",
      "download_hint": "python3 scripts/livemix.py download-draft --draft-url '...' --title '...'"
    }
  ]
}
```

单条草稿的自动包装失败（特效写入失败、BGM 获取失败等）不会中断整个任务，
会在 `packaging` / `notes` 中标注；`add_videos`、`add_captions` 等主链路失败会使任务失败。

---

## transcribe

```bash
python3 scripts/livemix.py transcribe (--input PATH | --source-id N) [--output FILE]
```

输出：

```json
{
  "ok": true,
  "source_id": 2,
  "cached": true,
  "duration_ms": 3600000,
  "utterance_count": 1234,
  "char_count": 45678,
  "speaker_count": 0,
  "average_confidence": -1,
  "text": "...",
  "utterances": [{"text": "...", "start_ms": 0, "end_ms": 1500, "words": []}],
  "output": "/path/to/transcript.json"
}
```

`--output` 写出完整结果；缓存键与 `clip` 相同（`asr-v2` 指纹），
所以先 `transcribe` 再 `clip` 不会重复识别。

---

## download-draft

```bash
python3 scripts/livemix.py download-draft \
  (--draft-url URL | --draft-id ID | --row-id N) \
  [--title T] [--draft-root DIR]
```

- `--row-id` 会从 SQLite 读取标题与草稿地址，成功后写回 `downloaded_path`；
- `--draft-root` 默认配置 `storage.draftDir`，应指向剪映的「草稿位置」；
- 下载先写 `.livemix-staging/<标题>_<draftId>`，完成后原子改名为正式目录；
- 失败时暂存目录保留，重试会复用已完成的素材文件。

输出 `local_path` 为正式草稿目录的绝对路径。

---

## export

```bash
python3 scripts/livemix.py export \
  (--draft-url URL | --row-id N) [--title T] \
  [--output-dir DIR] [--resume-url URL]
```

- 提交 `gen_video` 后首次等待 1.5 秒，随后每 3 秒轮询 `gen_video_status`，最多 600 次；
- `completed` 后以 1 小时超时下载到 `<输出目录>/<标题>.mp4`；
- `--resume-url` 用于「云端已完成、本地下载失败」时直接续传，避免重复合成。

输出 `output_path` 为本地 MP4 绝对路径。若下载阶段失败但云端已经生成成片，
命令会以退出码 1 返回：

```json
{
  "ok": false,
  "error": "下载失败：...",
  "video_url": "https://.../out.mp4",
  "resume_hint": "python3 scripts/livemix.py export --draft-url '...' --resume-url '...'"
}
```

按 `resume_hint` 重跑即可续传。

---

## sources

```bash
python3 scripts/livemix.py sources [--status recording|ready|failed]
```

列出 SQLite 中的素材记录（`media_sources`），按创建时间倒序。用于找回
`record` / `import` 输出的 `source_id`，再传给 `clip --source-id`。

---

## drafts

```bash
python3 scripts/livemix.py drafts [--source-id N]
```

输出全部草稿记录（按生成时间倒序），包含 `draft_url`、`downloaded_path`、话题等。
可用于批量导出：

```bash
python3 scripts/livemix.py drafts --source-id 2 > drafts.json
python3 - <<'PY'
import json, subprocess
for item in json.load(open("drafts.json"))["drafts"]:
    subprocess.run([
        "python3", "scripts/livemix.py", "export",
        "--draft-url", item["draft_url"], "--title", item["title"],
    ], check=True)
PY
```

---

## recover

```bash
python3 scripts/livemix.py recover
```

把 SQLite 中仍为 `recording` 的素材（上次进程被强杀、断电等）修正为：

- 有可用播放列表 / 文件 → `ready`，并写入警告「上次录制中断，已保留可恢复的录像。」；
- 没有可用文件 → `failed`。

`record`、`clip`、`transcribe` 在打开数据库后会自动先执行一次恢复。
技能以单进程顺序使用为前提：不要在同一数据库上并发运行 `record` 与其它命令。

输出：

```json
{"ok": true, "count": 1, "recovered": [{"source_id": 2, "status": "ready", "warning": "..."}]}
```

---

## prompts

```bash
python3 scripts/livemix.py prompts list
python3 scripts/livemix.py prompts show "默认提示词"
```

`show` 输出完整提示词内容。也可以用 `--prompt ./my-prompt.md` 传入自定义文件。

---

## selftest

```bash
python3 scripts/livemix.py selftest
```

18 项纯逻辑检查：字幕均衡拆分/数字与英文单词完整/标点与小数点、词级时间线、
英文轨位置、条数规则、时间窗覆盖、区间合并、标题话题清理、画布选择、BGM 循环、
音效去重分批、提示词目录、ASR JSON 解析、LLM JSON 解析、文件名清理、直播间号归一化、
UTF-16 长度。无需网络、ffmpeg 或密钥。
