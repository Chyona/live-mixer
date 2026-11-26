# 开源直播切片助手

把一场直播（或一段本地视频）变成可直接发布的短视频切片：

**直播录制 / 抖音直播间 / 本地素材 → 整片 ASR → AI 选片 → ffmpeg 精确裁剪 →
剪映草稿 / MP4 成片。**

本技能是开源客户端 [LiveMix Studio](https://github.com/Chyona/live-mixer) 的脚本化实现，
成片与剪映草稿的生成规则与客户端逐参数一致。所有脚本使用 **Python 3.9+ 标准库**，
不需要 Node.js，也不需要安装 pip 依赖；外部依赖仅为 `ffmpeg` / `ffprobe`。

## 快速开始

```bash
# 1. 检查环境（可选 --ping 验证 API 密钥）
python3 scripts/livemix.py doctor --ping

# 2. 提供密钥（二选一）
export LIVEMIX_API_KEY="你的密钥"
# 或 python3 scripts/livemix.py init 后编辑 ~/.livemix/config.json

# 3. 获取素材
python3 scripts/livemix.py record "https://live.douyin.com/123456" --title "今晚直播"
# 或
python3 scripts/livemix.py import ./demo.mp4

# 4. 一键成片
python3 scripts/livemix.py clip --input ./demo.mp4 --prompt "默认提示词"

# 5. 导出到剪映草稿箱，或直接导出 MP4
python3 scripts/livemix.py download-draft --draft-url "..." --title "切片标题"
python3 scripts/livemix.py export --draft-url "..." --title "切片标题"
```

## 命令

| 命令 | 作用 |
| --- | --- |
| `init` / `doctor` | 初始化配置 / 检查 ffmpeg、配置与密钥 |
| `record` | HTTP(S)、HLS、RTMP/RTSP 直播录制；mp4/mov 直链下载；抖音直播间解析 |
| `import` | 登记本地视频（≤10GB，必须有视频流） |
| `clip` | 一键成片并生成剪映草稿 |
| `transcribe` | 只做整片 ASR（带 SQLite 缓存） |
| `download-draft` | 下载并安装草稿到本机剪映草稿目录 |
| `export` | 云端合成并下载 MP4 成片 |
| `sources` | 查看素材库（录制 / 下载 / 导入） |
| `drafts` | 查看草稿记录 |
| `recover` | 恢复上次异常中断的录制 |
| `prompts` | 查看 4 套内置提示词 |
| `selftest` | 18 项纯逻辑自检 |

进度写 stderr，结果 JSON 写 stdout，便于脚本与 Agent 集成。
完整说明见 [SKILL.md](SKILL.md) 与 [references/](references/)。

## 与客户端的一致性

- 相同服务地址与模型：剪映小助手、`qwen3.8-flash`、`volc.seedasr.auc`；
- 相同 ffmpeg 参数：MP3 提取、H.264/AAC 裁剪、HLS 无损合成；
- 相同数量规则：每条 5–30 分钟素材、等长时间窗、句号对齐切点；
- 相同字幕算法：≤15 字、词级时间线、数字/英文单词不拆；
- 相同草稿字段：`scene_timelines` 倍速、800ms 转场、12 套字幕样式、
  35 种特效、10 种滤镜、16 张贴纸、13 种音效、BGM 循环、去重图层；
- 相同下载流程：暂存目录 + 剪映占位符路径改写 + 原子发布。

细节与已知的「非功能性差异」见 [references/parity-checklist.md](references/parity-checklist.md)。

## 费用说明

技能本体开源免费（Apache-2.0）。剪映小助手的 `upload_file` 与 `gen_video`、
ASR 与大模型网关由服务方按用量计费；调用前请向使用者说明，具体单价以服务方页面为准。

## 许可

Apache-2.0，见 [LICENSE](LICENSE)。
