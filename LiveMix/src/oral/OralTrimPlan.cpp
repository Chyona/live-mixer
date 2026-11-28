// 文件作用：智能口播「剪口气」删除计划实现。
// 只做纯时间计算，不调用 ASR / 大模型 / ffmpeg，便于独立验证区间结果。

#include "OralTrimPlan.h"

#include <algorithm>
#include <limits>

namespace {

// 超过该间隔视为静音并删除。
constexpr qint64 kSilenceMinMs = 500;
// 静音两端保留的余量，避免 ASR 词边界略偏时切掉起音/尾音。
constexpr qint64 kSpeechEdgeGuardMs = 120;
// 删除语气词时向相邻短间隙延伸的余量，让切点更干净。
constexpr qint64 kFillerEdgePadMs = 80;
// 过短的保留碎片（通常只是切点附近的噪声）并入删除。
constexpr qint64 kMinKeepMs = 120;
// 语气词删除总量上限：超过总时长 25% 视为模型误判，整体放弃语气词删除。
constexpr double kMaxFillerRatio = 0.25;

struct Range
{
    qint64 startMs = 0;
    qint64 endMs = 0;
};

Range clampRange(qint64 startMs, qint64 endMs, qint64 durationMs)
{
    return {qBound<qint64>(0, startMs, durationMs),
            qBound<qint64>(0, endMs, durationMs)};
}

// 合并重叠或首尾相接的区间，保持按时间递增。
QVector<Range> mergeRanges(QVector<Range> ranges)
{
    if (ranges.isEmpty()) {
        return ranges;
    }
    std::sort(ranges.begin(), ranges.end(),
              [](const Range &left, const Range &right) {
                  if (left.startMs != right.startMs) {
                      return left.startMs < right.startMs;
                  }
                  return left.endMs < right.endMs;
              });
    QVector<Range> merged;
    merged.append(ranges.first());
    for (int index = 1; index < ranges.size(); ++index) {
        const Range &next = ranges.at(index);
        Range &current = merged.last();
        if (next.startMs <= current.endMs) {
            current.endMs = qMax(current.endMs, next.endMs);
        } else {
            merged.append(next);
        }
    }
    return merged;
}

qint64 unionDuration(const QVector<Range> &ranges)
{
    qint64 total = 0;
    for (const Range &range : mergeRanges(ranges)) {
        total += qMax<qint64>(0, range.endMs - range.startMs);
    }
    return total;
}

// 口播有效语音区间：优先使用逐词时间线；某句没有词时间时退回整句时间。
QVector<Range> speechRanges(const Oral::SpeechAnalysis &speech, qint64 durationMs)
{
    QVector<Range> ranges;
    for (const Oral::Utterance &utterance : speech.utterances) {
        QVector<Range> wordRanges;
        for (const Oral::Word &word : utterance.words) {
            const Range range =
                clampRange(word.startMs, word.endMs, durationMs);
            if (range.endMs > range.startMs) {
                wordRanges.append(range);
            }
        }
        if (!wordRanges.isEmpty()) {
            ranges += wordRanges;
            continue;
        }
        const Range fallback =
            clampRange(utterance.startMs, utterance.endMs, durationMs);
        if (fallback.endMs > fallback.startMs) {
            ranges.append(fallback);
        }
    }

    // 逐词时间线整体缺失时，用句级时间兜底；否则不再重复叠加句级区间。
    if (ranges.isEmpty()) {
        for (const Oral::Utterance &utterance : speech.utterances) {
            const Range range =
                clampRange(utterance.startMs, utterance.endMs, durationMs);
            if (range.endMs > range.startMs) {
                ranges.append(range);
            }
        }
    }
    return mergeRanges(ranges);
}

// 语音之间的长间隔（含片头/片尾）生成静音删除区间。
QVector<Range> silenceDeleteRanges(const QVector<Range> &speech,
                                   qint64 durationMs)
{
    QVector<Range> deletes;
    if (speech.isEmpty()) {
        return deletes;
    }

    if (speech.first().startMs > kSilenceMinMs) {
        const qint64 end = speech.first().startMs - kSpeechEdgeGuardMs;
        if (end > 0) {
            deletes.append({0, end});
        }
    }
    for (int index = 1; index < speech.size(); ++index) {
        const Range &previous = speech.at(index - 1);
        const Range &current = speech.at(index);
        if (current.startMs - previous.endMs <= kSilenceMinMs) {
            continue;
        }
        const qint64 start = previous.endMs + kSpeechEdgeGuardMs;
        const qint64 end = current.startMs - kSpeechEdgeGuardMs;
        if (end > start) {
            deletes.append({start, end});
        }
    }
    if (durationMs - speech.last().endMs > kSilenceMinMs) {
        const qint64 start = speech.last().endMs + kSpeechEdgeGuardMs;
        if (start < durationMs) {
            deletes.append({start, durationMs});
        }
    }
    return deletes;
}

// 把大模型给出的词序号区间换算成时间删除区间；无效序号直接忽略。
QVector<Range> fillerDeleteRanges(const QVector<Oral::Word> &words,
                                  const QVector<Oral::FillerCut> &cuts,
                                  int *appliedCount)
{
    QVector<Range> deletes;
    if (words.isEmpty()) {
        return deletes;
    }
    for (const Oral::FillerCut &cut : cuts) {
        if (cut.startWord < 0 || cut.startWord >= words.size()
            || cut.endWord < cut.startWord || cut.endWord >= words.size()) {
            continue;
        }
        qint64 start = std::numeric_limits<qint64>::max();
        qint64 end = 0;
        for (int index = cut.startWord; index <= cut.endWord; ++index) {
            const Oral::Word &word = words.at(index);
            if (word.endMs <= word.startMs) {
                continue;
            }
            start = qMin(start, word.startMs);
            end = qMax(end, word.endMs);
        }
        if (start >= end) {
            continue;
        }

        // 向相邻短间隙延伸一点，但绝不越过相邻词的语音。
        qint64 previousEnd = 0;
        if (cut.startWord > 0) {
            const Oral::Word &previous = words.at(cut.startWord - 1);
            if (previous.endMs > previous.startMs) {
                previousEnd = previous.endMs;
            }
        }
        qint64 nextStart = std::numeric_limits<qint64>::max();
        if (cut.endWord + 1 < words.size()) {
            const Oral::Word &next = words.at(cut.endWord + 1);
            if (next.endMs > next.startMs) {
                nextStart = next.startMs;
            }
        }
        const qint64 cutStart = qMax(previousEnd, start - kFillerEdgePadMs);
        qint64 cutEnd = end + kFillerEdgePadMs;
        if (nextStart != std::numeric_limits<qint64>::max()) {
            cutEnd = qMin(cutEnd, nextStart);
        }
        if (cutEnd > cutStart) {
            deletes.append({cutStart, cutEnd});
            if (appliedCount) {
                ++*appliedCount;
            }
        }
    }
    return deletes;
}

QVector<Oral::TrimRange> complementKeepRanges(const QVector<Range> &deletes,
                                             qint64 durationMs,
                                             qint64 *removedMs)
{
    QVector<Oral::TrimRange> keep;
    qint64 cursor = 0;
    for (const Range &deleteRange : mergeRanges(deletes)) {
        const qint64 start = qBound<qint64>(0, deleteRange.startMs, durationMs);
        const qint64 end = qBound<qint64>(0, deleteRange.endMs, durationMs);
        if (end <= cursor) {
            continue;
        }
        if (start > cursor) {
            const qint64 keepEnd = qMin(start, durationMs);
            if (keepEnd > cursor) {
                keep.append({cursor, keepEnd});
            }
        }
        cursor = qMax(cursor, end);
    }
    if (cursor < durationMs) {
        keep.append({cursor, durationMs});
    }

    QVector<Oral::TrimRange> filtered;
    filtered.reserve(keep.size());
    qint64 keptMs = 0;
    for (const Oral::TrimRange &range : keep) {
        if (range.durationMs() >= kMinKeepMs) {
            filtered.append(range);
            keptMs += range.durationMs();
        }
    }
    if (removedMs) {
        *removedMs = qMax<qint64>(0, durationMs - keptMs);
    }
    return filtered;
}

} // namespace

namespace Oral {

QVector<TrimRange> buildTrimKeepRanges(const SpeechAnalysis &speech,
                                       const QVector<FillerCut> &fillerCuts,
                                       qint64 durationMs, TrimStats *stats)
{
    if (stats) {
        *stats = TrimStats();
        stats->sourceDurationMs = durationMs;
    }
    if (durationMs <= 0) {
        return {};
    }

    const QVector<Range> speechRangesLocal = speechRanges(speech, durationMs);
    if (speechRangesLocal.isEmpty()) {
        return {};
    }

    const QVector<Range> silenceDeletes =
        silenceDeleteRanges(speechRangesLocal, durationMs);
    const QVector<Word> words = flattenSpeechWords(speech);
    int fillerCount = 0;
    QVector<Range> fillerDeletes =
        fillerDeleteRanges(words, fillerCuts, &fillerCount);

    // 语气词删除量异常时整体放弃，避免模型把整段内容判成口气。
    bool fillerRejected = false;
    if (!fillerDeletes.isEmpty()
        && unionDuration(fillerDeletes) > qint64(durationMs * kMaxFillerRatio)) {
        fillerDeletes.clear();
        fillerCount = 0;
        fillerRejected = true;
    }

    QVector<Range> deletes = silenceDeletes;
    deletes += fillerDeletes;

    qint64 removedMs = 0;
    QVector<TrimRange> keep =
        complementKeepRanges(deletes, durationMs, &removedMs);

    // 兜底：删除后几乎没有内容时不做剪口，避免毁掉原片。
    qint64 keepDuration = 0;
    for (const TrimRange &range : keep) {
        keepDuration += range.durationMs();
    }
    if (keep.isEmpty() || keepDuration < 500) {
        if (stats) {
            *stats = TrimStats();
            stats->sourceDurationMs = durationMs;
            stats->outputDurationMs = durationMs;
        }
        return {};
    }

    if (stats) {
        stats->fillerCount = fillerCount;
        stats->silenceCount = silenceDeletes.size();
        stats->removedMs = removedMs;
        stats->outputDurationMs = keepDuration;
        stats->keepRangeCount = keep.size();
        stats->fillerCutsRejected = fillerRejected;
    }
    return keep;
}

} // namespace Oral
