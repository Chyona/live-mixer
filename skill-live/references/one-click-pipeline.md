# 一键成片流水线

对应实现：[`scripts/pipeline.py`](../scripts/pipeline.py)；
客户端基准：`LiveMix/src/services/LiveClipService.cpp`。

```
准备完整 MP4（HLS 才需要）
  → 提取 MP3
  → 整片 ASR（带缓存）
  → 数量校验 + 等长时间窗
  → 逐窗 LLM 选片
  → 索引映射 / 去重 / 合并
  → 逐条裁剪 → 上传 → 建草稿 → 写轨道
```

---

## 1. 准备完整 MP4

- 源是普通文件（mp4/mov/…）→ 直接使用；
- 源是 `.m3u8` → 优先复用录制目录中已存在的完整 MP4：
  1. `<录制目录>/<素材标题>.mp4`
  2. `<录制目录>/<录制目录名>.mp4`
  3. `<录制目录>/<源链接文件名>.mp4`
  4. `<录制目录>/recording.mp4`

  复用条件：文件存在且非空、修改时间不早于播放列表、时长 > 0、
  且不短于播放列表 6 秒（允许中断录像少一个分片）。

  没有可用文件时调用 `ensure_recording_mp4()`：

  - 播放列表含 `#EXT-X-ENDLIST` 视为正常完成，容差 2 秒；否则容差 6 秒；
  - 中断录像先补一份临时播放列表（追加 `#EXT-X-ENDLIST`），避免末尾分片被丢弃；
  - 清理超过 1 小时的 `index-*.part.mp4` 半成品；
  - 无损合成：
    `ffmpeg -i <playlist> -map 0:v:0 -map 0:a:0? -c copy -f mp4 -movflags +faststart <temp>`
  - 校验时长后原子改名为 `index.mp4`，权限恢复 `0644`。

之后的音频提取、ASR 与裁剪都按完整 MP4 处理，避免直接 seek HLS 分片导致
切片偏短、字幕错位。

---

## 2. 提取 MP3

```bash
ffmpeg -y -nostdin -hide_banner -loglevel error -i <mp4> -map 0:a:0 \
  -af aresample=async=1:first_pts=0,apad \
  -t <源视频时长秒，三位小数> \
  -ac 1 -ar 16000 -c:a libmp3lame -b:a 64k <work>/audio.mp3
```

- 音轨晚于视频起点：`first_pts=0` 补开头静音；
- 音轨短于视频：`apad` 补尾静音，`-t` 精确截到视频时长；
- 输出时长与源视频偏差 >500ms → 失败（会让字幕整体错位）；
  >100ms 打印警告；
- 读不到容器时长时只做 `first_pts=0`，不截断。

> ASR 时间线是相对这份 MP3 的，因此「与源视频等长」是一致性的前提。

---

## 3. 整片 ASR（带缓存）

### 3.1 缓存

- 指纹：`asr-v2:<源文件字节数>:<修改时间毫秒>`；
- 指纹绑定**源文件**（HLS 用 `index.m3u8`），重新合成 MP4 不会使缓存失效；
- 缓存存放在 SQLite `asr_results`，命中时跳过提取/上传/识别，不重复计费；
- 版本号 `asr-v2` 参与指纹：词级时间线上线前的旧缓存会自动失效。

### 3.2 上传与识别

1. `upload_file`（剪映小助手，multipart：`apiKey` + `file`）→ 公网 URL；
2. `POST <apiUrl>/audio/transcriptions`，multipart 字段：

| 字段 | 值 |
| --- | --- |
| `file_url` | 上传得到的 URL |
| `model` | `volc.seedasr.auc` |
| `language` | `zh` |
| `response_format` | `verbose_json` |
| `timestamp_granularities[]` | `word` |

3. 请求头 `Authorization: Bearer <apiKey>`，超时 35 分钟；
4. 网关为同步接口，一次返回完整转写；
5. 解析 `segments[]`：`start/end`（秒→毫秒）、`speaker` / `speaker_id` /
   `speaker_label`、`confidence`（0–1 自动换算为 0–100）、`words[]`
   （`word` 或 `text`）；
6. 无 `segments` 但有 `text` 时，退化为一条 `0 → duration` 的整段语句；
7. 空文本或 `end <= start` 的分句会被丢弃；
8. 结果写入 `asr_results`，并删除临时 `audio.mp3`。

---

## 4. 成片数量规则

常量：

- 每条成片**至少**使用 5 分钟素材 → 最多条数 `floor(总时长 / 5min)`；
- 每条成片**最多**使用 30 分钟素材 → 最少条数 `ceil(总时长 / 30min)`。

```python
min_clip_count = max(1, ceil(duration_ms / 30min))
max_clip_count = max(1, floor(duration_ms / 5min))
```

示例：

| 素材时长 | 允许条数 | 自动模式 |
| --- | --- | --- |
| 8 分钟 | 1 | 1 |
| 60 分钟 | 2–12 | 2 |
| 3 小时 | 6–36 | 6 |
| 时长未知 | 1–100 | 1 |

`--clips N` 必须落在允许区间；自动模式取最少条数。素材时长未知时会先探测；
仍未知时按 1 条处理。

---

## 5. 等长时间窗

把转写按目标条数均分为等长时间窗（`build_chunks`）：

- 目标条数先 `qBound(1, target, 句数)`；
- 单个时间窗时长 `asr_duration / chunk_count`；
- 对第 `i` 个切点，目标时间 `asr_duration * i / chunk_count`；
- 每个切点前后搜索范围：`min(5 分钟, max(1 秒, 窗口时长 / 10))`；
- 优先在范围内找**离目标最近的句号**（以该句的 `end_ms` 衡量距离）；
- 找不到句号时，取离目标最近的**句段边界**（下一句 `start_ms`）；
- 剩余段至少各占一句，切点索引单调前进，不会切出空段。

效果：60 分钟选 6 条 → 6 个约 10 分钟的时间窗，各成片时间范围互不重叠；
窗口时长始终在 5–30 分钟，天然满足单次 LLM 选片不超过 30 分钟的上限。

如果源时长缺失或元数据异常导致目标条数过少，会使窗口超过 30 分钟：
流水线会临时提高条数到 `ceil(asr时长 / 30min)`，保证 LLM 单次输入不超限。

---

## 6. LLM 选片

### 6.1 请求

`POST <apiUrl>/chat/completions`，`Authorization: Bearer <apiKey>`：

```json
{
  "model": "qwen3.8-flash",
  "messages": [
    {"role": "system", "content": "<所选提示词>"},
    {"role": "user", "content": "## 视频ASR\n[0] (0.00 - 3.20) 文本\n...\n\n## 输出格式\n..."}
  ],
  "temperature": 0.2,
  "max_tokens": 4000,
  "stream": true,
  "stream_options": {"include_usage": true}
}
```

用户提示词 = 带行号的 ASR 列表 + 固定输出格式：

```
## 视频ASR
[0] (0.00 - 3.20) ...
...

## 输出格式
- 仅输出一个 JSON 对象，不要输出任何额外文字、注释或角色标记。
- 字段：
  - indices：选中的句段索引（整数数组），按原顺序递增。索引必须来自 视频ASR 列表的行号（从 0 开始）。
  - title：短视频标题，2～12 个汉字。
  - description：短视频内容介绍，128 个字以内，概括成片核心观点。
  - topics：短视频话题字符串数组，共 2～6 个；每个话题 2～12 个汉字，不要带 # 号。
- 标题、介绍与话题须依据选中句段撰写，不得编造 ASR 未出现的事实。
- 示例：{"indices":[2,5,9,13],"title":"利率上行的真相","description":"用公开数据拆解本轮加息如何影响资产价格与配置思路。","topics":["宏观经济","利率周期","资产配置"]}
```

### 6.2 流式解析与重试

- 逐行解析 SSE `data:`；支持 `reasoning_content` / `reasoning` 与
  `content`（字符串或 `[{type:"text",text:"..."}]` 数组）；
- 网关忽略 `stream=true` 直接返回完整 completion JSON 时也能兜底；
- 429 / 5xx：最多 3 次，首次等待 2 秒、之后 5 秒；
- 网络超时 / 断网：最多 2 次，等待 2 秒；
- 内容为空 → `LLM 未返回最终选片结果`；
- JSON 解析失败 → `无法解析…`，流水线用「严格 JSON」提示词再试一次：

```
上一次输出不是合法 JSON。本次必须只输出一个 JSON 对象，
不要使用 Markdown 代码块，不要输出任何解释。
```

### 6.3 解析容错

- 去掉 Markdown 代码块围栏；
- 直接解析对象；
- 对象前后有解释文字时，截取第一个完整 `{...}`；
- 兼容 `indices` 或 `indexes`；
- 兼容旧输出：只返回 `[1,2,3]` 索引数组；
- 索引支持数字或数字字符串；越界、重复、负值会被忽略。

---

## 7. 索引 → 区间 → 合并

对每个时间窗：

1. 按模型返回顺序取 `chunk.segments[index]`；
2. 忽略越界、同窗重复、`end <= start` 的索引；
3. 每个合法句段生成 `{start_ms, end_ms}`，同时保留该句段作为字幕来源；
4. **合并**：保留模型返回顺序；若下一区间 `start >= 当前 start` 且
   `下一 start - 当前 end <= 500ms`，合并为当前区间（取并集）；
5. 标题：trim 后截到 12 个 UTF-16 字符，少于 2 字视为空（回退「直播切片」）；
6. 描述：trim 后截到 128 个 UTF-16 字符；
7. 话题：去掉开头所有 `#` / `＃`，trim，截到 12 字，去重，只保留 2–12 字，
   最多 6 个；
8. 模型返回空索引或全部无效 → 跳过该时间窗（不影响其他条）；
9. 所有时间窗都为空 → 任务失败：「AI 没有选出可用的切片」。

---

## 8. 裁剪与倍速

每个区间用 ffmpeg 从完整 MP4 裁剪并重新编码：

```bash
ffmpeg -y -nostdin -hide_banner -loglevel error \
  -ss <start秒> -t <时长秒> -i <mp4> \
  -map 0:v:0 -map 0:a:0? \
  -c:v libx264 -preset veryfast -crf 20 -pix_fmt yuv420p \
  -c:a aac -b:a 128k \
  -movflags +faststart <clip.mp4>
```

- `-ss` 放在 `-i` 之前快速定位；重新编码保证起止时间精确；
- 切片按**原速**裁剪，倍速在草稿 `add_videos` 的 `scene_timelines` 中应用，
  剪映里可继续调整速度，也避免重复转码损失画质；
- 切片实际时长与请求区间偏差 >2.5 秒时，以请求区间为准（防止源时间戳异常）；
- 画布取**第一段**成功探测到的分辨率，按宽高比在
  `1920×1080` / `1080×1920` 中选择最接近的一档；探测失败默认横屏。

倍速：

```python
if speed_auto:
    speed = 1.05 + 0.01 * random_int(0, 45)   # 1.05×–1.50×
else:
    speed = clamp(fixed_speed, 0.5, 2.0)
```

每段：

- 素材时长 `source_ms` = 切片实际时长（异常时用区间时长）；
- 播放轴时长 `playback_ms = max(1, round(source_ms / speed))`；
- `add_videos` 的 `start/end` 是源素材在草稿时间轴上的累计位置，
  `scene_timelines` 的 `start/end` 是变速后的播放区间；
- 倍速为 1.0 时不提交 `scene_timelines`（所有段必须同时具备才会提交）。

---

## 9. 失败语义

| 步骤 | 失败是否中断 |
| --- | --- |
| HLS 合成 / MP3 提取 / ASR / 选片 | 是（没有可用转写或选片无法继续） |
| 单条草稿的裁剪 / 上传 / create_draft / add_videos / add_captions | 是（该次 `clip` 失败，已生成的前序草稿保留） |
| 转场、特效、滤镜、贴纸、去重图片、SFX、BGM、英文轨 | 否（降级并写入 `packaging` / `notes`） |

工作目录在任务结束后自动删除，除非 `--keep-work`。
生成的草稿记录写入 SQLite `clip_drafts`，可用 `drafts` 命令查看。
