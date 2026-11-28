#pragma once

// 文件作用：把「语音 + 语义 + 素材」转换成统一的剪辑计划组件。
// - 字幕：按标点与最大字数拆分 ASR 句段，用逐词时间线对齐，自动标记关键词；
// - 主镜头：校验大模型给出的镜头，缺失/重叠/空档在本地补齐，保证主画面
//   从 0 到片尾连续覆盖；
// - 画中画：校验时间窗、位置与素材，最多 4 处；
// - 结果始终满足：不重叠、不越界、相邻镜头尽量不重复同一素材。

#include "MixcutTypes.h"

namespace Mixcut {

// 生成字幕计划；titleFx 为 true 时额外生成片头标题花字。
SubtitlePlan buildSubtitles(const SpeechAnalysis &speech,
                            const SemanticAnalysis &semantics,
                            SubtitlePreset preset, const Canvas &canvas,
                            bool titleFx);

// 期望的主镜头数量：约每 3.2 秒一个，限制在 4–40 个之间。
int targetMainShotCount(qint64 durationMs);

// 归一化主镜头：
// - 丢弃无效 clipId 与非法时间；
// - 重叠部分截断，空档本地补齐；
// - 单个镜头超过 6 秒时均分；
// - transitions=false 时清空转场。
QVector<Shot> normalizeMainShots(const QVector<Shot> &raw,
                                 const MaterialLibrary &library,
                                 const SpeechAnalysis &speech,
                                 const SemanticAnalysis &semantics,
                                 bool transitions);

// 归一化画中画：时间窗 2–6 秒、不重叠、不越界、最多 4 处。
// raw 为空时按语义重点自动补 0–3 处。
QVector<Shot> normalizeOverlays(const QVector<Shot> &raw,
                                const QVector<Shot> &mainShots,
                                const MaterialLibrary &library,
                                const SpeechAnalysis &speech,
                                const SemanticAnalysis &semantics);

// 字幕预设的中文展示名。
QString subtitlePresetName(SubtitlePreset preset);

} // namespace Mixcut
