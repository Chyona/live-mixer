# 配置

技能沿用 LiveMix Studio 客户端的 `AppConfig` 结构（service / storage / recording /
ai / finish / general），并增加 `storage.stateDb` 用于技能自己的素材库与 ASR 缓存。

## 优先级

```
命令行参数（--api-url / --api-key / --db / --config）
    > 环境变量
    > 配置文件
    > 默认值
```

## 默认路径

| 内容 | 默认路径 |
| --- | --- |
| 配置 | `~/.livemix/config.json`（`LIVEMIX_CONFIG_PATH` 可覆盖） |
| SQLite | `~/.livemix/livemix.db`（`LIVEMIX_DB_PATH` 或 `storage.stateDb`） |
| 录制 | `~/Movies/LiveMix/Recordings`（Windows/Linux 回退到 `~/Videos` 等） |
| 草稿 | `~/Movies/LiveMix/CapDraft` |
| 成片 | `~/Movies/LiveMix/Exports` |
| 缓存 | macOS `~/Library/Caches/LiveMixSkill`；Linux `~/.cache/livemix-skill`；Windows `%LOCALAPPDATA%/LiveMixSkill/cache` |

客户端自身的配置与数据库位于 Qt 的 `AppConfigLocation` / `AppDataLocation`
（例如 macOS 的 `~/Library/Application Support/LiveMix Studio/livemix.db`），
技能**不会**自动写入客户端数据库；如需共用素材记录与 ASR 缓存，可把
`storage.stateDb` 指向该数据库。技能的 SQLite schema 与客户端 v4 对齐
（`media_sources`、`recording_sessions`、`asr_results.data_json`、`clip_drafts`、
`clip_tasks`、`prompts`），ASR 缓存 JSON 也使用客户端的
`words[].start_time / end_time` 格式，双方可以互相复用。

## 环境变量

| 变量 | 作用 |
| --- | --- |
| `LIVEMIX_CONFIG_PATH` | 配置文件路径 |
| `LIVEMIX_API_KEY` | API 密钥（覆盖 `service.apiKey`） |
| `LIVEMIX_API_URL` | 网关地址（覆盖 `service.apiUrl`） |
| `LIVEMIX_DB_PATH` | SQLite 路径 |
| `LIVEMIX_FFMPEG_DIR` | 包含 `ffmpeg` / `ffprobe` 的目录 |
| `LIVEMIX_RECORDING_DIR` | 录制目录 |
| `LIVEMIX_DRAFT_DIR` | 剪映草稿目录 |
| `LIVEMIX_EXPORT_DIR` | 成片导出目录 |
| `LIVEMIX_CACHE_DIR` | 技能缓存目录 |

## 完整示例

```json
{
  "version": 4,
  "service": {
    "apiUrl": "https://api.jcaigc.cn/v1",
    "apiKey": "sk-xxxxxxxx"
  },
  "storage": {
    "recordingDir": "/Users/me/Movies/LiveMix/Recordings",
    "exportDir": "/Users/me/Movies/LiveMix/Exports",
    "draftDir": "/Users/me/Movies/LiveMix/CapDraft",
    "cacheDir": "/Users/me/Library/Caches/LiveMixSkill",
    "cacheLimitGb": 100,
    "autoCleanCache": true,
    "stateDb": "/Users/me/.livemix/livemix.db"
  },
  "recording": {
    "format": "hls",
    "ffmpegPath": "",
    "segmentSeconds": 4,
    "reconnect": true,
    "reconnectDelayMaxSeconds": 5,
    "stopTimeoutMs": 8000
  },
  "ai": {
    "defaultPrompt": "默认提示词",
    "exportSrt": true
  },
  "finish": {
    "transitions": true,
    "effects": true,
    "filters": true,
    "stickers": true,
    "bgm": true,
    "sfx": true,
    "bilingualSubtitles": false,
    "dedup": true,
    "speedAuto": true,
    "fixedSpeed": 1.0
  },
  "general": {
    "theme": "dark",
    "launchAtLogin": false,
    "minimizeToBackground": false,
    "soundOnComplete": true,
    "systemNotifications": true
  }
}
```

## 字段说明

### service

- `apiUrl`：OpenAI 兼容网关，LLM 选片、字幕翻译、音效选点与豆包 ASR 共用；
- `apiKey`：同一密钥用于上述服务与剪映小助手的 `upload_file` / `gen_video`。

剪映小助手、素材服务的地址与 HMAC 凭证为技能内置常量，不读取配置、不提供设置入口
（与客户端一致）。

### storage

- `stateDb`：SQLite。包含 `media_sources`、`recording_sessions`、`asr_results`、
  `clip_drafts` 四张核心表；ASR 缓存在 `asr_results`，以 `source_id + 指纹` 命中。
- `cacheLimitGb` / `autoCleanCache`：保留字段，脚本不会主动清理素材；临时工作目录
  在每次 `clip` 结束后删除（`--keep-work` 可保留）。

### recording

- `segmentSeconds`：HLS 分片长度，1–30 秒；
- `reconnect` / `reconnectDelayMaxSeconds`：http(s) 流断线重连参数，仅传给 ffmpeg；
- `stopTimeoutMs`：停止录制时等待 ffmpeg 收尾的时间，超时后强制结束；
- `ffmpegPath`：可选，指定 ffmpeg 可执行文件；ffprobe 从同目录或 `PATH` 查找。

### ai

- `defaultPrompt`：`clip` 未传 `--prompt` 时使用；
- `exportSrt`：保留字段；技能可通过 `transcribe --output` 得到逐句 JSON，
  需要 SRT 时可在 Agent 侧转换。

### finish

8 个自动包装开关与客户端「成片设置」一一对应；`clip` 命令的
`--[no-]…` 参数可逐次覆盖。`speedAuto=true` 时每条成片独立随机
`1.05×–1.50×`；否则使用 `fixedSpeed`（0.5–2.0）。

## 密钥与安全

- `init` 写出的配置文件权限为 `0600`（仅当前用户可读写）；
- 不要把密钥提交到 Git、写进提示词或发送到外部服务；
- 日志与错误信息中不会打印完整密钥；`doctor` 只报告「是否已配置」。
