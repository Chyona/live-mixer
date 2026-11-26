# 素材获取

对应实现：[`scripts/recording.py`](../scripts/recording.py)
与 [`douyin.py`](../scripts/douyin.py)；
客户端基准：`LiveMix/src/media/RecordingService.cpp`。

---

## 1. 地址归一化

`normalize_stream_url()` 与客户端 `MainWindow::normalizeStreamUrl` 一致：

- 4–20 位纯数字 → `https://live.douyin.com/<数字>`（抖音房间号）；
- 支持 `http / https / rtmp / rtmps / rtsp`；
- 裸域名（如 `live.douyin.com/123`）会补 `http://`（等价 `QUrl::fromUserInput`）；
- 其它形式（本地路径、未知协议）报错。
- 抖音 `v.douyin.com` 短链会跟随跳转到 `live.douyin.com` 后解析。

---

## 2. HTTP(S) 链接：先嗅探，再决定下载或录制

对 `http/https` 先发起一次小范围探测：

```
GET <url>
User-Agent: LiveMixStudio/1.0
Accept: */*
Range: bytes=0-8191
超时: 8 秒
```

读取最多 8192 字节与响应头，按以下顺序分类：

1. **HLS 播放列表**：`Content-Type` 含 `mpegurl` / `m3u8`，或正文（去 BOM 后）
   以 `#EXTM3U` 开头；
2. **原文件直链**：`Content-Type` 以 `video/mp4`、`video/quicktime`、
   `application/mp4`、`video/x-m4v` 开头，或第 4–8 字节为 `ftyp`（MP4 box）；
3. 其余一律交给 ffmpeg 录制。

探测失败（HTTP ≥400、超时、网络错误且未读到 12 字节）时按扩展名兜底：
`.mp4/.m4v/.mov` 视为直链，否则视为流。

如果探测确认是 HLS 但 URL 没有 `.m3u8` 后缀，ffmpeg 会显式加 `-f hls`，
避免把标准输入误判为其它容器。

### 2.1 直链下载

- 输出：`<录制目录>/<时间戳>-<标题>/<URL 文件名>`；
- 先写 `<文件名>.part`，完整落盘后原子改名；
- 空闲超时 60 秒（每次读操作）；完成后校验 `Content-Length`；
- `ffprobe` 校验必须存在视频流；
- 若没有音轨：成功但附带警告「源文件没有音频轨，无法进行语音转写」；
- 若音轨不是 AAC：调用
  `ffmpeg -map 0:v:0 -map 0:a:0 -c:v copy -c:a aac -b:a 128k -movflags +faststart`
  仅转码音频，视频保持直拷；转换失败时保留原文件并给出警告；
- 数据库记录 `source_kind = download`，状态 `ready`。

### 2.2 直播 / HLS 录制

- 输出目录：`<录制目录>/<YYYYMMDD-HHMMSS>-<标题>`（同名自动加 `-2`、`-3`…）；
- 落盘：`index.m3u8` + `segment_%05d.ts` + `ffmpeg.log`；
- ffmpeg 参数（与客户端逐项一致）：

```bash
ffmpeg -hide_banner -y -loglevel warning -progress pipe:1 -nostats \
  [-reconnect 1 -reconnect_streamed 1 -reconnect_delay_max N] \
  [-f hls] \
  -i <source> \
  -c copy -f hls \
  -hls_time <segmentSeconds> \
  -hls_playlist_type event \
  -hls_segment_filename <dir>/segment_%05d.ts \
  <dir>/index.m3u8
```

说明：

- `-c copy` 要求源流为 TS 兼容编码（H.264 / AAC 等）；不兼容时 ffmpeg 报错，
  错误写入卡片/日志；
- `event` 播放列表会持续追加；停止录制时向 ffmpeg stdin 发送 `q\n`，
  等待 `stopTimeoutMs`（默认 8 秒）后仍未退出则 `kill`；
- 成功条件：ffmpeg 退出码 0 且播放列表非空，**或**用户主动停止且播放列表非空；
- 时长优先从 `#EXTINF` 累加，其次用 ffmpeg 进度，最后用墙钟时间；
- 中断/崩溃不会删除已产生的分片，便于恢复；`clip` 会先无损合成完整 MP4。
- 数据库记录 `source_kind = stream`，`local_path` 指向 `index.m3u8`。

---

## 3. 抖音直播间

每次录制都重新打开房间页，因为页面里的流地址带签名且会过期。

### 3.1 页面请求

```
GET <page>
User-Agent: Chrome 141 / macOS
Accept: text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8
Accept-Language: zh-CN,zh;q=0.9
Referer: https://live.douyin.com/
超时: 25 秒
页面上限: 16 MB
```

跟随 `v.douyin.com` 短链跳转；最终地址必须是 `live.douyin.com`。
HTTP ≥400、网络错误且无响应体、无法识别房间号都会明确报错。

### 3.2 解析顺序

页面内嵌 JSON 的引号可能被转义为 `\"`，先还原为 `"`，再：

1. `"flv_pull_url": {...}`：优先 FLV，长连接更适合长时间录制；
2. `"hls_pull_url_map": {...}`：HLS；
3. `"hls_pull_url":"..."` 字符串；
4. 页面内任意 `https?://…m3u8…` 地址。

清晰度优先级：`HD1` → `FULL_HD1` → `SD1` → `SD2` → 第一个 `http` 地址。
URL 中的 `&` / `/` / `=` / `\/` 会被还原。

### 3.3 可播放性确认

拿到流地址后先发：

```
GET <stream>
User-Agent: Chrome 141 / macOS
Accept: */*
Range: bytes=0-4095
Referer: https://live.douyin.com/
超时: 8 秒
```

只读取响应头：

- 2xx → 可播放，开始录制；
- 403 → 「直播间流拒绝访问（可能已下播或链接已失效）」；
- 其它 ≥400 → 「直播间流不可用（HTTP xxx，可能已下播）」；
- 网络错误 / 超时 → 明确的连接失败原因。

视频库保存的仍是**原始直播间链接**，真实流地址只用于 ffmpeg 输入，
避免把会过期的签名地址暴露给用户。

---

## 4. 本地视频导入

- 支持扩展名：`mp4 / mov / mkv / flv / ts / m2ts / avi / webm / m4v /
  mpg / mpeg / wmv / 3gp / m3u8`；
- 单文件 ≤ **10 GB**；
- `ffprobe -print_format json -show_format -show_streams` 必须至少有一条
  真实视频流；`attached_pic=1` 的封面图不算视频流；
- 纯音频、图片、损坏文件会被拒绝；
- **只登记，不复制**原始文件；
- 数据库记录 `source_kind = local`，状态 `ready`。

---

## 5. 失败与清理

| 场景 | 行为 |
| --- | --- |
| 直链下载失败 / 取消 | 删除 `.part` 与整次输出目录，来源标记 failed |
| 直链无音轨 | 成功，`error_message` 写入警告，`clip` 会在转写步骤失败 |
| 直播录制 ffmpeg 失败 | 保留已产生的分片与日志，来源标记 failed |
| 主动停止 | 保留文件；成功条件见上 |
| 抖音未开播/已下播 | 不创建素材记录，直接报错 |
| 应用退出 / Ctrl+C | 向 ffmpeg 发送 `q`，等待收尾；超时后强制结束 |
| 进程被强杀 / 断电 | 下次 `record`、`clip`、`transcribe` 打开数据库时自动恢复：有播放列表则标记 ready 并附警告，否则标记 failed；也可手动运行 `recover` |

`record` 的 JSON 中 `ok=false` 时，`material.message` / `material.warning`
给出具体原因；`--verbose` 不会额外打印网络请求体（避免泄露签名 URL）。
