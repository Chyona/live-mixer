#pragma once

// 文件作用：智能口播「剪口气」的删除计划。
//
// 输入 ASR 词级时间线与内置提示词识别出的语气词区间，输出若干「保留区间」：
// 1. 语气词/口头禅按词删除；
// 2. 相邻语音之间超过 0.5 秒的间隔视为静音并删除；
// 3. 语音两侧保留少量余量，避免切掉起音和尾音。
//
// 删除方式与直播切片一致：先确定要保留的时间区间，再按区间裁剪并拼接成
// 一条连续视频（真正执行由 FfmpegUtils::cutKeepRanges 完成）。

#include "OralTypes.h"

#include <QVector>

namespace Oral {

// 一段连续的时间区间（毫秒）。
struct TrimRange
{
    qint64 startMs = 0;
    qint64 endMs = 0;

    qint64 durationMs() const { return qMax<qint64>(0, endMs - startMs); }
};

// 剪口气的结果统计，用于过程日志。
struct TrimStats
{
    int fillerCount = 0;   // 删除的语气词处数
    int silenceCount = 0;  // 删除的静音段数
    qint64 removedMs = 0;  // 合计删除时长
    qint64 sourceDurationMs = 0;
    qint64 outputDurationMs = 0;
    int keepRangeCount = 0;
    // 大模型返回的语气词过多、可能误删时整体放弃语气词删除，只删静音。
    bool fillerCutsRejected = false;
};

// 由词级时间线与语气词删除区间计算保留区间；无法剪除或剪除后过短时返回空。
QVector<TrimRange> buildTrimKeepRanges(const SpeechAnalysis &speech,
                                       const QVector<FillerCut> &fillerCuts,
                                       qint64 durationMs, TrimStats *stats);

} // namespace Oral
