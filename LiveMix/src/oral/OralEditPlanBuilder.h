#pragma once

// 文件作用：把「语音 + 语义 + 素材」转换成统一的剪辑计划组件。
// 这里负责字幕系统与镜头规划的最终落定：
// - 字幕：按标点与最大字数拆分 ASR 句段，用逐词时间线对齐，自动标记关键词；
// - 镜头：校验大模型给出的 B-Roll，缺失时按语义段与素材标签本地补齐；
// - 结果始终满足：不重叠、不早于片头安全区、不晚于片尾安全区。

#include "OralTypes.h"

namespace Oral {

// 生成字幕计划；titleFx 为 true 时额外生成片头标题花字。
SubtitlePlan buildSubtitles(const SpeechAnalysis &speech,
                            const SemanticAnalysis &semantics,
                            SubtitlePreset preset, const Canvas &canvas,
                            bool titleFx);

// 生成最终镜头计划。
// llmShots 可以为空或不完整；本地会补足到 targetCount，并做边界/重叠校验。
ShotPlan buildShotPlan(const SpeechAnalysis &speech,
                       const SemanticAnalysis &semantics,
                       const QVector<MaterialProfile> &materials, int targetCount,
                       Density density, const QVector<Shot> &llmShots,
                       bool transitions);

// UI 密度对应的目标 B-Roll 数量（8 / 12 / 16）。
int brollCountForDensity(Density density);

} // namespace Oral
