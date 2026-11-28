# 智能口播流水线（Oral Pipeline）

本目录是智能口播功能的完整实现，与直播切片（`LiveClipService` / `ClipTypes` /
`CapCutMateClient` / `AsrClient` / `LlmClient`）完全解耦：不修改、不调用这些业务的
服务层代码，只复用 `HttpClient`、`FfmpegUtils`、`MediaUtils` 等通用底层能力。

## 三步流程

1. **理解**
   - `OralAsrClient`：语音识别。首选 `volc.bigasr.auc_turbo`（本地 MP3）；
     若当前 API Key 未开通该资源，自动回退 `volc.seedasr.auc`（公网 URL）或
     `qwen3-asr-flash`（本地文件），并在过程日志中提示。
   - `OralLlmClient`：固定使用 `deepseek-flash`。
     - 语义分析：内容分段、主题/关键词、重点句、发布文案；
     - 素材分析：关键帧图片 + 本地视觉特征（主色/亮度/饱和度/细节/运动）；
     - 镜头规划：按语义段与素材标签输出 B-Roll 时间窗；
     - 音效选点：稀疏挑选值得加音效的字幕。
   - `OralMediaUtils`：素材探测、关键帧抽取、画布尺寸预处理、画面特征。
   - `OralEditPlanBuilder`：字幕拆分（逐词时间线对齐、关键词高亮）与镜头本地兜底。
   - `OralAudioAssetsClient` / `OralSfxCatalog`：BGM 与音效素材。

2. **生成**
   - 所有结果固化到 `Oral::EditPlan`，并序列化为
     `Edit Plan / Timeline JSON` 写入 `cache/oral/plans/plan_<runId>.json`。
   - 分析结果按「源视频 + 素材」文件指纹缓存，重新包装时跳过重复 ASR/理解。

3. **合成**
   - `OralCoverClient` / `OralCoverPrompts`：两步生成封面。
     第一步用内置提示词 1 + 口播内容调用 `deepseek-flash` 生成提示词 2；
     第二步用提示词 2 + 口播首帧调用 `doubao-seedream-5-0-flash` 图生图。
     提示词 2 与封面每次成片都重新生成，重新包装会得到不同的封面；
     图生图请求携带随机种子，避免相同提示词产出完全相同的图片。
   - `OralCapCutClient`：上传、创建草稿、视频/图片/字幕/音频/关键帧、云渲染、
     下载成片与剪映草稿。
   - 轨道结构：
     - V1：口播原片（保留原声）；
     - V2：视频 B-Roll（音量 0，透明度淡入淡出）；
     - V3：图片 B-Roll（渐显/渐隐 + 轻微推近）；
     - V4：封面海报（压在最上层，只覆盖片头 `[0, 10000]` 微秒）；
     - T1：片头标题；T2：正文关键词字幕；
     - A1：BGM；A2：音效。
   - 封面海报作为成片首帧，标题与正文从封面结束后开始，避免遮挡封面。
   - 输出「成片 + 剪映草稿」时自动下载草稿；只输出成片时草稿仍保留在服务端，
     结果页可稍后再下载。

## 文件

| 文件 | 作用 |
| --- | --- |
| `OralTypes.*` | 领域模型、Edit Plan / Timeline JSON 序列化、结果历史 JSON |
| `OralMediaUtils.*` | ffprobe/ffmpeg 封装、关键帧、画面特征、画布预处理、编码探测 |
| `OralAsrClient.*` | ASR（豆包极速版首选 + 标准版/qwen 回退） |
| `OralLlmClient.*` | DeepSeek 语义/素材/镜头/音效分析 |
| `OralAudioAssetsClient.*` | 独立签名的 BGM / 音效素材服务 |
| `OralSfxCatalog.*` | 内置音效目录 |
| `OralCoverPrompts.*` | 内置封面提示词 1（视觉导演，不提供配置） |
| `OralCoverClient.*` | 提示词 2 生成（deepseek-flash）+ 首帧图生图封面（doubao-seedream-5-0-flash） |
| `OralCapCutClient.*` | capcut-mate 接口与草稿本地化 |
| `OralEditPlanBuilder.*` | 字幕系统与镜头规划 |
| `OralPipelineService.*` | 工作线程编排、缓存、进度与结果 |
| `OralDraftDownloadTask.*` | 结果页「生成草稿」下载任务 |

## 与 UI 的映射

- 模板只支持「清透讲解」；其它模板保持展示但不进入流水线。
- 字幕样式：智能关键词 / 大字强调 / 极简白字。
- B-Roll 密度：少 / 适中 / 丰富 → 8 / 12 / 16 处，并按视频时长与 2.5 秒最小间隔限流。
- 动态转场：图片入/出场动画 + 视频透明度关键帧。
- 花字标题：片头标题独立文本轨。
- 结果页预览：左侧直接展示成片 0 秒帧（即封面海报）；旧历史记录没有预览缓存时自动补截一次。
- BGM / 音效：可分别关闭；BGM 音量 0.12 给口播让位，短于成片时循环铺满。
