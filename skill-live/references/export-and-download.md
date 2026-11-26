# 导出成片与下载草稿

对应实现：

- [`scripts/video_export.py`](../scripts/video_export.py)
- [`scripts/draft_download.py`](../scripts/draft_download.py)

客户端基准：`VideoExporter.cpp`、`CapCutMateClient::downloadDraft`。

---

# 一、导出成片（MP4）

## 1. 提交合成

`POST https://capcut-mate.jcaigc.cn/openapi/capcut-mate/v1/gen_video`

```json
{
  "apiKey": "<UUID 格式的 API 密钥>",
  "draft_url": "<原样透传>"
}
```

- `gen_video` 只表示任务已提交，不代表合成完成；
- 该接口需要 `apiKey`，且按分钟计费（具体价格以剪映小助手页面为准）；
- 调用前应向用户说明会产生费用。

## 2. 轮询状态

`POST .../gen_video_status`

```json
{"draft_url": "<原样透传>"}
```

返回：

```json
{
  "code": 0,
  "status": "pending | processing | completed | failed",
  "progress": 0,
  "video_url": "https://...",
  "error_message": ""
}
```

轮询策略（与客户端一致）：

- 首次等待 **1.5 秒**，之后每 **3 秒**一次；
- 最多 **600 次**（约 30 分钟）；
- `completed`：必须有 `video_url`，否则报错；
- `failed`：读取 `error_message` 或 `message`，立即停止，不重试到死循环；
- `pending` / `processing`：继续；
- 其它非空状态：报「未知状态」并停止；
- 若响应中提前出现 `video_url`，先记录下来，完成后使用。

## 3. 下载

- 目标：`<成片导出目录>/<标题>.mp4`；
- 标题经过文件名清理，最长 80 字；空标题回退「成片」；
- 下载超时 1 小时；先写 `.part`，成功后原子改名；失败不留半截文件；
- 已生成但下载失败时，可用 `--resume-url <video_url>` 直接续传，
  避免再次提交付费合成；
- 进度输出到 stderr；成功输出 `output_path`。

## 4. 失败提示

| 情况 | 错误 |
| --- | --- |
| 缺少 draft_url | `草稿记录缺少 draft_url，无法导出成片。` |
| 缺少密钥 | `尚未配置 API 密钥，无法导出成片。` |
| 合成失败 | 服务端 `error_message` |
| 未知状态 | `云端合成返回未知状态：xxx` |
| 超时 | `云端合成超时，请稍后重试。` |
| 下载失败 | `下载失败：...`（403/404/超时等） |

---

# 二、下载并安装剪映草稿

对应客户端卡片上的「导出到剪映 / 打开草稿文件夹」。剪映草稿在服务端生成后
只保留记录；下载时才会拉取草稿文件与素材。

## 1. 取文件列表

`GET .../get_draft?draft_id=<draft_id>`

```json
{"code": 0, "files": ["https://.../draft_content.json", "https://.../assets/..."]}
```

`draft_id` 从 `draft_url` 的查询参数 `draft_id` 中提取。
列表按相对路径去重；无法识别的路径直接报错。

## 2. 草稿目录命名

```
<draftRoot>/<清理后的标题>_<draftId>
<draftRoot>/.livemix-staging/<清理后的标题>_<draftId>   # 暂存
```

- 标题清理规则：控制字符与 `/ \ : * ? " < > | %` → `_`，压缩空白，
  去掉开头的点，空标题回退「直播切片」，最长 40 字；
- 所有内容先写入**根目录下的隐藏暂存目录**，全部就绪后原子改名到正式目录。
  剪映的目录监听只认可「完整出现」的新目录；边下边写会被跳过，只能重启收录。

## 3. 两阶段下载

### 阶段 1：配置

- `.json` 文件读入内存（不直接落盘），用于后续解析素材与改写；
- 其它非 JSON 文件（例如 `template.tmp`）直接下载到暂存目录；
- 暂存目录里已存在且非空的文件会跳过（支持失败重试）。

### 阶段 2：素材

遍历每个配置 JSON 的 `materials.videos / audios / images`：

- 只处理 `path` 是 `http(s)` 的远程素材；
- 草稿内相对路径：

```
assets/<videos|audios|images>/<安全文件名>_<6位短哈希>.<后缀>
```

- 文件名取自 URL 的 basename；为空时用 `material_name`；清理控制字符与
  非法字符，最长 36 字；
- 后缀缺失时按列表默认：video → `mp4`、audio → `mp3`、image → `jpg`；
- 同一素材 URL 在多个 JSON 中出现时只下载一次；
- 写盘同样先 `.part` 后原子改名。

> 6 位短哈希用于避免不同 URL 重名互相覆盖。客户端使用 Qt `qHash`，
> 技能使用 SHA-256 前 6 位；同一 URL 稳定，且不影响剪映读取（属非功能性差异）。

## 4. 改写 JSON

### 素材路径

对每个含 `materials` 的 JSON：

- `materials.videos[].path`、`audios[].path`、`images[].path` 中的远程 URL
  替换为：

```
##_draftpath_placeholder_0E685133-18CE-45ED-8CB8-2904A212EC80_##/<相对路径>
```

- 设置 `config.material_save_mode = 1`，告诉剪映素材在草稿目录内；
- 路径固定使用正斜杠（Windows 也不能写成反斜杠）。

### draft_meta_info.json

- `draft_name` = 草稿文件夹名；
- `draft_fold_path` = 最终草稿目录（正斜杠）；
- `draft_root_path` = 最终目录的父目录；
- `draft_materials[].value[].file_Path`：
  - 以占位符开头 → 去掉占位符；
  - 绝对路径（`/...` 或 `C:...`）→ 保持原样（无法可靠映射）；
  - 其余 → 规范为 `./<相对路径>`。

## 5. 提示文件

写入 UTF-8 BOM 的文本：

```
【剪映草稿】请用剪映打开查看成片.txt
```

内容说明「这是剪映草稿，不是可直接播放的视频，请用剪映打开」。
属于附加信息，写入失败不影响草稿可用。

## 6. 原子发布

- 如果正式目录已存在，先递归删除（同一业务 ID 的半成品会被替换）；
- `rename(暂存目录 → 正式目录)`，同一文件系统内为原子操作；
- 清理空的 `.livemix-staging` 根目录。

剪映的目录监听看到完整的新目录后会自动收录，无需重启。

## 7. 重试与取消

- 失败时保留暂存目录；再次运行会跳过已完成的素材文件；
- Ctrl+C 会中止下载；已落盘文件保留，下次继续；
- `--row-id` 成功后把 `downloaded_path` 写回 SQLite。

## 8. 打开草稿

- 草稿目录即 `local_path`；
- 剪映的「草稿位置」必须与 `--draft-root` / `storage.draftDir` 一致；
- 如果剪映仍看不到，重启剪映一次或检查草稿目录权限。

> **不要**手动修改下载后的草稿 JSON。需要调整内容时重新运行 `clip` 生成新草稿；
> 图片/视频/音频 URL 可直接复用，非必要不要重新上传。
