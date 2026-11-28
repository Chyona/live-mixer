# 智能混剪流水线（Mixcut Pipeline）

本目录是智能混剪功能的完整实现，与直播切片（`LiveClipService` / `ClipTypes`）、
智能口播（`OralPipelineService`）的业务编排互相独立；只复用已经验证过的
底层能力（`HttpClient`、`FfmpegUtils`、`Oral::AsrClient`、`Oral::CapCutClient`、
`Oral::MediaUtils` 与 `OralTypes` 中的语音模型）。

输入：AI 配音（由界面文案与所选音色合成）+ 杂乱的视频和图片素材。
输出：成片 MP4 + 可继续编辑的剪映草稿。

## 六步流水线

### 1. 素材智能整理（`MixcutPipelineService::analyzeMaterials`）

- 视频用 ffmpeg 的 `scene` 滤镜检测场景切换点（缩小到 320 宽加速），
  相邻切换点至少间隔 1.2 秒；过短镜头自动合并，单个视频最多保留 8 个镜头；
- 每个镜头抽取关键帧与运动参考帧，计算主色、亮度、饱和度、对比度、
  细节密度与运动量；
- 图片直接作为单个镜头，读取尺寸与画面特征；
- 大模型（`deepseek-flash`）按关键帧给出画面描述、内容标签、用途、
  质量评分与情绪；模型不支持图片输入时自动降级为纯文本理解；
- 没有模型结果时用本地特征兜底：亮度/细节/饱和度/分辨率得到 0–100
  的质量分，文件名与画面特征生成基础标签。

### 2. AI 语音理解（`understandSpeech`）

- `MixcutTtsClient`：调用语音网关 `POST /v1/audio/speech`，默认
  `seed-tts-2.0`，音色为界面选择的豆包 `*_uranus_bigtts`；
  长文案按句子切分（每段 ≤ 600 字）分别合成后用 ffmpeg 拼成一条 MP3；
  结果按「模型 + 音色 + 文案」缓存，重复混剪不重复计费；
- `Oral::AsrClient`：对合成配音做 ASR，首选 `volc.bigasr.auc_turbo`，
  未开通时回退 `qwen3-asr-flash`；ASR 不可用时按文案字数估算逐句时间轴，
  不阻断成片；
- `MixcutLlmClient::analyzeSemantics`：内容分段、关键词、段落情绪、
  叙事结构、重点句与发布文案；失败时按模板风格本地兜底。

### 3. AI 镜头规划（`planTimeline` + `MixcutEditPlanBuilder`）

- 大模型根据语义段、素材标签/描述/质量规划主镜头与画中画；
- 本地校验保证：主镜头从 0 到片尾连续覆盖、不重叠、单镜头 ≤ 6 秒、
  clipId 有效、视频取用位置在素材范围内；
- 模型缺失/无效时按语义段与素材标签本地规划，优先高质量与语义匹配的
  素材，相邻镜头避免同一素材；
- 画中画（`overlays`）0–4 处，位置支持左上/右上/左下/右下，本地保证
  时间窗 1.5–6 秒且互不重叠；
- 主画面使用「视频轨 + 图片轨」两层铺满，画中画按位置/类型分组各建一条轨。

### 4. 智能包装（`buildPackaging`）

- 字幕：按标点与最大字数拆分句段，用 ASR 逐词时间线对齐；关键词高亮；
  模板风格映射字幕样式（种草=智能关键词 / 测评=大字强调 / 故事=极简白字）；
- 动画：图片入出场渐显/渐隐，静态图片叠加轻微推近关键帧；
- 转场：主轨相邻同类型镜头之间使用「叠化 / 闪白 / 推近」；
- BGM：素材服务随机取一首，音量 0.12，短于成片时循环铺满；
- 音效：内置音效目录 + 大模型稀疏选点，最多 8 处，音量 0.18，
  本地保证同一音轨不重叠；
- 画面特效与滤镜：按模板风格选择剪映已支持的名称，失败只记录日志，
  不阻断成片。

### 5. 时间轴编排（`finalizeTimeline`）

- 所有结果固化为 `EditPlan` 并序列化为：
  - `cache/mixcut/plans/plan_<runId>_<timestamp>.json`（完整计划）；
  - 同名 `_timeline.json`（精简轨道与时间信息）。
- 写入前再次校验主镜头覆盖率，发现空档自动补齐。

### 6. 合成（`composeDraft` / `renderAndDownload`）

调用 capcut-mate：

- `create_draft` → `upload_file` → `add_videos` / `add_images` /
  `add_videos`（画中画）→ `add_captions` → `add_audios` →
  `add_effect` / `add_filters` → `add_keyframes`；
- 轨道结构：
  - V1：主视频镜头（音量 0，AI 配音单独走音频轨）；
  - V2：主图片镜头（渐显/渐隐 + 轻微推近）；
  - V3+：画中画（按位置/类型分组，缩放 0.42）；
  - T1：片头标题；T2：正文关键词字幕；
  - A1：AI 配音；A2：音效；A3：BGM；
- `gen_video` 提交云渲染，每 4 秒轮询 `gen_video_status`；
- 下载成片 MP4 到 `Movies/LiveMix/混剪成片/`，并把剪映草稿写入
  `Movies/LiveMix/CapDraft/`；
- 抽取成片约 1.2 秒处画面作为结果页与最近混剪卡片的预览图（避开淡入黑场）；
- 结果与素材写入 `mixcut_history.json`，重启后可回看与重新混剪。

## 文件

| 文件 | 作用 |
| --- | --- |
| `MixcutTypes.*` | 领域模型、素材/语义/镜头/包装类型、Edit Plan 与结果 JSON |
| `MixcutMediaUtils.*` | 场景切换检测、镜头区间整理、抽帧、TTS 分段拼接、本地质量评分 |
| `MixcutTtsClient.*` | 语音网关 TTS（seed-tts-2.0）与本地缓存 |
| `MixcutLlmClient.*` | 素材理解、语义/情绪/叙事分析、镜头规划、音效选点 |
| `MixcutSfxCatalog.*` | 内置音效目录与素材服务结果对齐 |
| `MixcutCapCutClient.*` | capcut-mate 客户端的特效/滤镜扩展（其余复用 Oral 客户端） |
| `MixcutEditPlanBuilder.*` | 字幕系统、主镜头覆盖校验、画中画校验与本地兜底 |
| `MixcutPipelineService.*` | 六步编排、进度日志、缓存、结果 |

## UI 映射

- 首页「最近混剪」只展示真实生成记录（`mixcut_history.json`）；
- 「一键成片」启动 `Mixcut::PipelineService` 工作线程，过程页 6 张阶段卡
  与实时日志对应上面六步；
- 结果页展示真实成片首帧、文件大小/编码、成片与草稿路径；
  「播放成片」用系统播放器打开 MP4，「打开草稿」直接打开草稿目录；
- 全局「成片设置」中的转场、特效、滤镜、BGM、音效开关会应用到混剪；
- 示例素材没有本地文件，只用于界面预览，不参与真实渲染。
