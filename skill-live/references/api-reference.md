# 接口参考

本技能调用四类服务：

| 服务 | 基地址 | 用途 |
| --- | --- | --- |
| 剪映小助手（CapCut Mate） | `https://capcut-mate.jcaigc.cn/openapi/capcut-mate/v1/` | 上传、草稿、导出 |
| LLM / ASR 网关 | 配置 `service.apiUrl`，默认 `https://api.jcaigc.cn/v1` | 选片、翻译、音效、ASR |
| 素材服务 | `https://assets.gogoshine.com/openapi/assets/v1/` | BGM、去重图片、音效目录 |
| 对象存储 / CDN | 各接口返回的 URL | 上传素材、下载成片/草稿 |

---

## 1. 剪映小助手

### 通用约定

- 请求/响应均为 JSON（`upload_file` 为 multipart）；
- **业务错误仍返回 HTTP 200**，真实结果看 `code`：

```json
{"code": 0, "message": "", "...": "..."}
```

- `code != 0` 时取 `message` 报错；
- 网络错误 / HTTP != 200 / 无法解析的响应都视为失败；
- `draft_url` 必须原样透传，`draft_id` 从查询参数提取。

### 接口表

| 接口 | 方法 | 关键字段 | 超时 |
| --- | --- | --- | --- |
| `upload_file` | POST multipart | `apiKey`、`file`（ASCII 文件名 `upload.<ext>`） | 15 分钟 |
| `create_draft` | POST JSON | `width`、`height` | 60 秒 |
| `add_videos` | POST JSON | `draft_url`、`video_infos`（字符串）、`scene_timelines`（数组，可选）、`scale_x`、`scale_y` | 120 秒 |
| `add_images` | POST JSON | `draft_url`、`image_infos`（字符串）、`scale_x`、`scale_y`、`transform_x`、`transform_y`、`alpha` | 120 秒 |
| `add_sticker` | POST JSON | `draft_url`、`sticker_id`、`start`、`end`、`scale`、`transform_x`、`transform_y` | 60 秒 |
| `add_captions` | POST JSON | `draft_url`、`captions`（字符串）、`text_color`、`border_color`、`font`、`font_size`、`transform_x`、`transform_y` | 120 秒 |
| `add_effects` | POST JSON | `draft_url`、`effect_infos`（字符串） | 120 秒 |
| `add_filters` | POST JSON | `draft_url`、`filter_infos`（字符串） | 120 秒 |
| `add_audios` | POST JSON | `draft_url`、`audio_infos`（字符串） | 120 秒 |
| `gen_video` | POST JSON | `apiKey`、`draft_url` | 60 秒 |
| `gen_video_status` | POST JSON | `draft_url` | 60 秒 |
| `get_draft` | GET query | `draft_id` | 60 秒 |

### 数据结构

`video_infos`（序列化后提交）：

```json
[
  {"video_url": "https://...", "start": 0, "end": 4500000,
   "transition": "叠化", "transition_duration": 800000}
]
```

`scene_timelines`（直接作为数组提交，与视频一一对应）：

```json
[{"start": 0, "end": 3103000}]
```

`image_infos`：

```json
[{"image_url": "https://...", "start": 0, "end": 20000000}]
```

`captions`：

```json
[{"start": 0, "end": 1200000, "text": "字幕", "in_animation": "渐显"}]
```

`audio_infos`：

```json
[{"audio_url": "https://...", "start": 0, "end": 1200000,
  "volume": 0.1, "duration": 1200000}]
```

`effect_infos`：

```json
[{"effect_title": "星光", "start": 0, "end": 3200000}]
```

`filter_infos`：

```json
[{"filter_title": "清透感", "start": 0, "end": 6206000, "intensity": 12}]
```

### 时间单位

所有 `start` / `end` / `transition_duration` / `duration` 均为**微秒**。
脚本内部用毫秒，提交前统一 ×1000。

---

## 2. 大模型网关

### 选片

`POST <apiUrl>/chat/completions`，`Content-Type: application/json`，
`Authorization: Bearer <apiKey>`。

```json
{
  "model": "qwen3.8-flash",
  "messages": [{"role": "system", "content": "..."}, {"role": "user", "content": "..."}],
  "temperature": 0.2,
  "max_tokens": 4000,
  "stream": true,
  "stream_options": {"include_usage": true}
}
```

- 响应为 SSE：`data: {...}`，结束标记 `data: [DONE]`；
- 内容字段兼容 `delta.content` 字符串或 `[{type:"text",text:"..."}]`；
- 推理内容字段兼容 `delta.reasoning_content` 与 `delta.reasoning`；
- `usage` 可能出现在任意 chunk，取最后一次非零值；
- 网关忽略 `stream=true` 时，从非流式 `choices[0].message` 兜底。

### 音效选点

同样的 URL 与鉴权，`stream=false`，`temperature=0.2`，`max_tokens=4000`。
响应内容是一个 JSON 对象（可被代码块围栏或前后文字包裹）：

```json
{"selections": [{"index": 3, "category": 0}]}
```

### 字幕翻译

`stream=false`，`temperature=0.1`，`max_tokens=clamp(2000, 条数×160, 8000)`。
响应：

```json
{"translations": [{"index": 0, "text": "Hello world"}]}
```

解析还兼容：

- `items` / `data` / `results` 作为数组键；
- `translations` 内层对象；
- `{"0":"...","1":"..."}` 的数字键映射；
- 对象字段 `text` / `translation` / `english` / `en` / `value`；
- 索引字段 `index` / `id` / `i` / `no`；
- 数组元素直接是字符串（按位置对应）。

### 重试

| 场景 | 行为 |
| --- | --- |
| 429 / 5xx | 选片最多 3 次（等待 2s、5s）；音效最多 2 次（2s）；翻译最多 3 次（2s、5s） |
| 网络超时 / 断网 | 选片最多 2 次（2s）；翻译最多 2 次（2s） |
| JSON 解析失败 | 选片由流水线换「严格 JSON」提示词重试一次；音效/翻译内部重试 |
| 内容为空 | 同解析失败处理 |

---

## 3. 豆包 ASR

`POST <apiUrl>/audio/transcriptions`，`multipart/form-data`，
`Authorization: Bearer <apiKey>`，超时 35 分钟。

| 字段 | 值 |
| --- | --- |
| `file_url` | 公网可访问的音频 URL |
| `model` | `volc.seedasr.auc` |
| `language` | `zh` |
| `response_format` | `verbose_json` |
| `timestamp_granularities[]` | `word` |

响应（OpenAI verbose_json）：

```json
{
  "text": "整段文本",
  "duration": 3600.0,
  "segments": [
    {
      "text": "一句话",
      "start": 0.0,
      "end": 3.2,
      "speaker": "1",
      "confidence": 0.92,
      "words": [{"word": "一", "start": 0.0, "end": 0.1}]
    }
  ]
}
```

- 时间单位秒，脚本统一换算毫秒；
- `speaker` 兼容 `speaker / speaker_id / speaker_label`；
- `confidence` 兼容 0–1 与 0–100，0–1 自动 ×100；
- `words[].word` 或 `words[].text`；
- 无 `segments` 但有 `text` 时退化为一条整段语句。

---

## 4. 素材服务

### 地址

`https://assets.gogoshine.com/openapi/assets/v1/{endpoint}`

| 接口 | 查询参数 | 返回 `data` |
| --- | --- | --- |
| `bgm/random` | `limit=1` | `items[0].file_url / name / duration_ms` |
| `images/random` | `limit`、`tags`（如「去重」） | `items[].file_url / width / height` |
| `sfx` | `page=1&page_size=100` | `items[].resource_id / name / file_url / duration_ms` |

### HMAC-SHA256 签名

所有请求为 GET，带四个头：

```
X-Access-Key: ak_...
X-Timestamp: <Unix 秒>
X-Nonce: <16 位十六进制随机串>
X-Signature: <hex(HMAC-SHA256(secret, canonical))>
```

`canonical` 按换行拼接：

```
GET
<path>
<canonicalQuery>
<timestamp>
<nonce>
<sha256(空请求体)>
```

- `canonicalQuery`：把查询参数按 key、value 排序后以 `&` 连接，
  key/value 使用 percent-encoding（例如 `tags=%E5%8E%BB%E9%87%8D`）；
- 空请求体的 SHA-256 为
  `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`。

响应为统一封装 `{"code":0,"data":{...}}`；401 / 403 有专门的错误文案。

---

## 5. 错误与超时汇总

| 服务 | 业务错误字段 | 备注 |
| --- | --- | --- |
| 剪映小助手 | `code != 0` + `message` | HTTP 200 也可能是错误 |
| 素材服务 | `code != 0` + `message` | 401/403 单独提示 |
| ASR | HTTP 非 2xx 或 `error.message` | 超时 35 分钟 |
| LLM | HTTP 非 2xx / `error.message` / `message` | 429、5xx、网络错误按上文重试 |

脚本所有 HTTP 请求都设置超时；下载类请求先写 `.part`，失败不留半截文件。
