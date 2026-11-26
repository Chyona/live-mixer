# 故障排查

先运行：

```bash
python3 scripts/livemix.py doctor --ping
python3 scripts/livemix.py selftest
```

`doctor` 检查 ffmpeg/ffprobe、配置、密钥连通性；`selftest` 检查 18 项纯逻辑。
失败时加 `--verbose` 可看到 Python 堆栈；`clip --keep-work` 可保留中间文件。

---

## 环境

### 未找到 ffmpeg / ffprobe

```
未找到 ffmpeg，请安装后重试，或设置 LIVEMIX_FFMPEG_DIR。
未找到 ffprobe。请安装 ffmpeg，或设置 LIVEMIX_FFMPEG_DIR。
```

- 安装：macOS `brew install ffmpeg`；Windows 下载静态构建并加入 `PATH`；
- 或 `export LIVEMIX_FFMPEG_DIR=/path/to/bin`；
- 或配置 `recording.ffmpegPath` 指向 ffmpeg 可执行文件（ffprobe 从同目录查找）。

### ffmpeg 执行超时 / 执行失败

- 文件可能损坏、正在被其它程序写入、或磁盘已满；
- HLS 录制要求源流是 TS 兼容编码（H.264/AAC）；不兼容时会报编码错误；
- 保留 `ffmpeg.log` 与 `--keep-work` 目录排查；
- 裁剪失败时检查源文件是否可被 `ffplay` 正常播放。

---

## 配置与密钥

### 尚未配置 API 密钥

```
尚未配置 API 密钥，请设置 LIVEMIX_API_KEY 或配置 service.apiKey。
```

三种方式任选：`export LIVEMIX_API_KEY=...`、`--api-key ...`、
`~/.livemix/config.json` 的 `service.apiKey`。密钥来自 <https://jcaigc.cn>。

### 401 / 403

- 剪映小助手：密钥无效或没有该接口权限；
- 素材服务 401/403：内置凭证不可用，或服务端临时限制；重试 / 联系服务方；
- 网关 401/403：`doctor --ping` 会明确提示「密钥无效」。

### 429 / 5xx

脚本会按规则重试（选片 3 次、音效 2 次、翻译 3 次）。持续失败时：

- 稍后再试；
- 检查账户额度；
- 减小一次处理的素材量（例如先裁成较短素材）。

---

## 素材获取

### 无法开始录制

- 仅支持 `http/https/rtmp/rtmps/rtsp` 或抖音直播间/房间号；
- 抖音未开播、已下播、需要登录、链接失效都会解析失败；
- 直播间页面结构变化时解析会失效，需要同步更新 `douyin.py`。

### 抖音直播录制失败

```
抖音直播录制失败：直播间流拒绝访问（HTTP 403，可能已下播或链接已失效）。
抖音直播录制失败：直播间流不可用（HTTP xxx，可能已下播）。
```

每次录制都会重新解析带签名的流地址；如果页面能打开但流不可用，通常是
未开播/已下播/地区限制/需要登录。

### 下载失败：文件不完整 / 403 / 404

- 直链可能已过期或需要 Referer/Cookie；替换链接；
- 大文件下载中断会删除 `.part`，重试即可；
- 服务器返回 `application/octet-stream` 但实际是 mp4 时，按 `ftyp` 头识别。

### 源文件没有音频轨

直链下载成功但标记警告：

```
源文件没有音频轨，无法进行语音转写。
```

`clip` 会在提取 MP3 时失败；请更换带音轨的素材。

### HLS 录像恢复

中断的录像保留 `index.m3u8 + segment_*.ts`。`clip` 会追加临时 `#EXT-X-ENDLIST`
后无损合成 `index.mp4`。如果合成仍失败：

- 检查最后几个 `.ts` 是否被截断；
- 播放列表时长与实际分片时长差超过 6 秒会判定不完整；
- 可用 ffmpeg 手工检查：

```bash
ffmpeg -v error -i index.m3u8 -f null -
```

---

## 一键成片

### 成片数量超限

```
素材时长 00:60:00，成片数量需在 2–12 条之间：每 5 分钟素材最多生成 1 条……
```

按 `[ceil(总时长/30min), floor(总时长/5min)]` 调整 `--clips`。

### ASR 未识别到语音内容

- 音频为空、纯音乐、静音或语言不匹配；
- 检查 MP3 是否可播放；
- 极长素材可先截一段短素材验证链路。

### LLM 返回无法解析

- 首次失败会自动用严格 JSON 提示词重试；
- 仍失败时：更换提示词、减少时间窗长度、检查网关是否把 `content` 放在
  非标准字段；
- 自定义提示词要求输出额外字段时容易触发，建议只写选片偏好。

### LLM 模型服务繁忙 / 请求超时

脚本会重试；持续失败时检查网关状态与账户额度。大型直播会按 30 分钟窗口
拆成多次请求，单次失败只影响当前窗口。

### AI 没有选出可用的切片

- 提示词与内容不匹配（例如用服装提示词处理财经直播）；
- 素材话术太稀疏或全是闲聊；
- 换提示词，或先 `transcribe` 检查转写质量。

### 字幕 2019 / 添加字幕失败

剪映小助手把字幕写进同一条轨道，时间重叠会整批拒绝。脚本已做：

- 跨切片累计重叠保护；
- 终点截到当前切片播放结束；
- 丢弃 <50ms 的退化片段。

如果服务端仍返回 2019，检查：

- 是否存在重复字幕文本/时间（脚本已按索引去重）；
- 是否是服务端临时错误；
- 查看 `--verbose` 输出的服务端 `message`。

### 特效 / 滤镜 / 贴纸 / BGM / 音效未写入

这些是**非阻断**步骤，失败只记录：

- 素材服务不可用时 BGM/去重图片/音效目录缺失；
- 没有合适的字幕窗口时特效/贴纸跳过；
- 服务端对某些名称/资源 ID 不支持时写入失败。

草稿仍然可用，可在剪映中手动补充。

### 中英文双字幕失败

- 没有中文字幕可翻译；
- 译文为空、仍含中文或回显原文会被丢弃；
- 英文轨写入失败不影响中文轨；
- 检查网关是否支持批量 JSON 输出。

---

## 剪映草稿

### 草稿在剪映里看不到

1. 确认 `download-draft --draft-root` 与剪映「草稿位置」完全一致；
2. 草稿目录名形如 `<标题>_<draftId>`，应完整出现；
3. 重启剪映一次（目录监听有时需要）；
4. 检查目录权限与磁盘空间；
5. 检查是否存在 `.livemix-staging` 残留：失败时暂存目录会保留，重试会继续。

### 素材丢失 / 黑屏

- 下载阶段是否中断？重新 `download-draft`，已下载文件会复用；
- `draft_content.json` 的 `materials.*[].path` 是否以
  `##_draftpath_placeholder_..._##/assets/...` 开头；
- 不要手动修改草稿 JSON；需要调整时重新 `clip`。

### 草稿 JSON 被手动改坏

剪映草稿文件由剪映管理，手改 JSON 容易破坏结构（路径、时间线、素材索引）。
请删除损坏草稿并重新 `clip` / `download-draft`。技能不会覆盖服务端草稿，
重新生成即可。

---

## 导出成片

### 提交云端合成失败

- `gen_video` 需要 `apiKey` 且为付费接口；
- 检查密钥、额度、草稿是否仍有效；
- 服务端返回 `message` 会原样展示。

### 云端合成超时

- 轮询 600 次（约 30 分钟）仍无结果；
- 成片可能已在服务端生成：如果拿到过 `video_url`，用
  `export --resume-url <url>` 直接下载；
- 否则稍后重试。

### 下载失败 / 磁盘空间不足

- 成片下载超时 1 小时；失败不留半截文件；
- 检查导出目录权限与磁盘空间；
- 已生成成片时用 `--resume-url` 续传。

---

## 调试技巧

```bash
# 只看结果，不要进度
python3 scripts/livemix.py clip --input demo.mp4 --quiet

# 失败时打印堆栈
python3 scripts/livemix.py clip --input demo.mp4 --verbose

# 保留切片、MP3 与工作目录
python3 scripts/livemix.py clip --input demo.mp4 --keep-work

# 复现某次随机结果
python3 scripts/livemix.py clip --input demo.mp4 --seed 42

# 只做转写，检查 ASR 质量
python3 scripts/livemix.py transcribe --input demo.mp4 --output transcript.json

# 查看草稿记录
python3 scripts/livemix.py drafts
```

如果问题仍无法定位，请附上：

- `doctor` 输出（密钥已打码）；
- 失败命令与完整 stderr；
- `ffmpeg.log`（录制/合成失败时）；
- 转写 JSON 的片段（确认 ASR 质量）；
- 草稿 `draft_id`（不要附密钥或带签名的素材 URL）。
