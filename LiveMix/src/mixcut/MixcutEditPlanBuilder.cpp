// 文件作用：智能混剪剪辑计划组件生成实现（字幕 + 主镜头 + 画中画）。

#include "MixcutEditPlanBuilder.h"

#include <QHash>
#include <QSet>
#include <QStringList>
#include <QtMath>

#include <algorithm>

namespace {

bool isBreakPunctuation(QChar ch)
{
    static const QString marks =
        QStringLiteral("，。！？；、,.!?;：:…\n");
    return marks.contains(ch);
}

QString stripTrailingPunctuation(QString text)
{
    while (!text.isEmpty() && isBreakPunctuation(text.back())) {
        text.chop(1);
    }
    return text.trimmed();
}

QString stripAllSpaces(QString text)
{
    text.remove(QLatin1Char(' '));
    text.remove(QLatin1Char('\t'));
    text.remove(QLatin1Char('\n'));
    return text;
}

struct CharTiming
{
    qint64 startMs = -1;
    qint64 endMs = -1;
};

struct TextChunk
{
    int start = 0;
    int end = 0; // 左闭右开
    QString text;
};

QVector<CharTiming> buildCharTimings(const Mixcut::Utterance &utterance)
{
    const QString text = utterance.text;
    QVector<CharTiming> timings(text.size());
    if (utterance.words.isEmpty()) {
        return timings;
    }

    int cursor = 0;
    int mapped = 0;
    for (const Mixcut::Word &word : utterance.words) {
        const QString cleaned = stripAllSpaces(word.text);
        if (cleaned.isEmpty()) {
            continue;
        }
        int position = text.indexOf(cleaned, cursor, Qt::CaseSensitive);
        if (position < 0) {
            position = text.indexOf(cleaned, 0, Qt::CaseSensitive);
        }
        if (position < 0) {
            continue;
        }
        const int length = qMin(cleaned.size(), text.size() - position);
        const qint64 wordDuration = qMax<qint64>(1, word.endMs - word.startMs);
        for (int index = 0; index < length; ++index) {
            CharTiming &timing = timings[position + index];
            timing.startMs =
                word.startMs + wordDuration * index / qMax(1, length);
            timing.endMs =
                word.startMs + wordDuration * (index + 1) / qMax(1, length);
            ++mapped;
        }
        cursor = position + length;
    }
    if (mapped * 2 < text.size()) {
        return QVector<CharTiming>(text.size());
    }
    return timings;
}

QVector<TextChunk> splitIntoChunks(const QString &text, int maxChars)
{
    QVector<TextChunk> chunks;
    int start = 0;
    int count = 0;
    const int length = text.size();
    for (int index = 0; index < length; ++index) {
        ++count;
        const QChar ch = text.at(index);
        const bool punctuation = isBreakPunctuation(ch);
        const bool nextIsPunctuation =
            index + 1 < length && isBreakPunctuation(text.at(index + 1));
        if ((punctuation && count >= 3)
            || (count >= maxChars && !nextIsPunctuation)) {
            TextChunk chunk;
            chunk.start = start;
            chunk.end = index + 1;
            chunk.text = text.mid(start, index + 1 - start);
            chunks.append(chunk);
            start = index + 1;
            count = 0;
        }
    }
    if (start < length) {
        TextChunk chunk;
        chunk.start = start;
        chunk.end = length;
        chunk.text = text.mid(start);
        chunks.append(chunk);
    }

    QVector<TextChunk> cleaned;
    for (TextChunk chunk : chunks) {
        while (chunk.start < chunk.end
               && (chunk.text.trimmed().isEmpty()
                   || isBreakPunctuation(chunk.text.at(0)))) {
            ++chunk.start;
            chunk.text = text.mid(chunk.start, chunk.end - chunk.start);
        }
        chunk.text = stripTrailingPunctuation(chunk.text);
        if (!chunk.text.isEmpty()) {
            cleaned.append(chunk);
        }
    }

    QVector<TextChunk> merged;
    for (const TextChunk &chunk : cleaned) {
        if (!merged.isEmpty() && chunk.text.size() <= 2
            && merged.last().text.size() + chunk.text.size() <= maxChars + 4) {
            merged.last().end = chunk.end;
            merged.last().text += chunk.text;
        } else {
            merged.append(chunk);
        }
    }
    return merged;
}

int maxCharsForPreset(Mixcut::SubtitlePreset preset, const Mixcut::Canvas &canvas)
{
    const bool portrait = canvas.height >= canvas.width;
    switch (preset) {
    case Mixcut::SubtitlePreset::Keyword:
        return portrait ? 13 : 17;
    case Mixcut::SubtitlePreset::Big:
        return portrait ? 10 : 13;
    case Mixcut::SubtitlePreset::Minimal:
        return portrait ? 15 : 19;
    }
    return 13;
}

QString animationForPreset(Mixcut::SubtitlePreset preset)
{
    switch (preset) {
    case Mixcut::SubtitlePreset::Keyword:
        return QStringLiteral("渐显");
    case Mixcut::SubtitlePreset::Big:
        return QStringLiteral("弹近");
    case Mixcut::SubtitlePreset::Minimal:
        return QStringLiteral("渐显");
    }
    return QString();
}

bool isGenericKeyword(const QString &keyword)
{
    static const QSet<QString> stopWords = {
        QStringLiteral("这个"), QStringLiteral("那个"), QStringLiteral("然后"),
        QStringLiteral("就是"), QStringLiteral("可以"), QStringLiteral("我们"),
        QStringLiteral("你们"), QStringLiteral("他们"), QStringLiteral("因为"),
        QStringLiteral("所以"), QStringLiteral("但是"), QStringLiteral("如果"),
        QStringLiteral("还是"), QStringLiteral("已经"), QStringLiteral("真的"),
        QStringLiteral("比较"), QStringLiteral("非常"), QStringLiteral("一个"),
        QStringLiteral("什么"), QStringLiteral("怎么"), QStringLiteral("时候"),
        QStringLiteral("现在"), QStringLiteral("感觉"), QStringLiteral("其实"),
        QStringLiteral("东西"), QStringLiteral("大家"), QStringLiteral("今天"),
    };
    return keyword.size() < 2 || stopWords.contains(keyword);
}

int roleAffinity(Mixcut::MaterialRole role, Mixcut::SegmentType type)
{
    switch (type) {
    case Mixcut::SegmentType::Hook:
        return role == Mixcut::MaterialRole::Person
                    || role == Mixcut::MaterialRole::Scene
            ? 22
            : 0;
    case Mixcut::SegmentType::Pain:
        return role == Mixcut::MaterialRole::Product
                    || role == Mixcut::MaterialRole::Detail
            ? 20
            : 0;
    case Mixcut::SegmentType::Point:
        return role == Mixcut::MaterialRole::Product
                    || role == Mixcut::MaterialRole::Detail
            ? 18
            : 0;
    case Mixcut::SegmentType::Example:
        return role == Mixcut::MaterialRole::Scene ? 20 : 0;
    case Mixcut::SegmentType::Evidence:
        return role == Mixcut::MaterialRole::Text ? 20 : 0;
    case Mixcut::SegmentType::Cta:
        return role == Mixcut::MaterialRole::Product
                    || role == Mixcut::MaterialRole::Person
            ? 18
            : 0;
    default:
        break;
    }
    return 0;
}

int clipScore(const Mixcut::MaterialClip &clip,
              const Mixcut::SemanticSegment *segment,
              const Mixcut::SemanticAnalysis &semantics, int attempt)
{
    int score = 35 + clip.quality / 2;
    const QStringList keywords =
        segment ? segment->keywords : semantics.keywords;
    for (const QString &tag : clip.tags) {
        for (const QString &keyword : keywords) {
            if (tag.contains(keyword) || keyword.contains(tag)) {
                score += 19;
                break;
            }
        }
    }
    for (const QString &bestFor : clip.bestFor) {
        for (const QString &keyword : keywords) {
            if (bestFor.contains(keyword) || keyword.contains(bestFor)) {
                score += 10;
                break;
            }
        }
    }
    if (segment) {
        score += roleAffinity(clip.role, segment->type);
        if (!segment->visualHint.isEmpty()) {
            for (const QString &tag : clip.tags) {
                if (segment->visualHint.contains(tag)) {
                    score += 8;
                    break;
                }
            }
        }
    }
    // 视频镜头画面更有节奏，权重略高；但不要完全挤掉图片。
    if (clip.isVideo()) {
        score += 4;
    }
    score += (attempt * 7) % 11;
    return score;
}

const Mixcut::MaterialClip *pickClip(const Mixcut::MaterialLibrary &library,
                                     const Mixcut::SemanticSegment *segment,
                                     const Mixcut::SemanticAnalysis &semantics,
                                     const QString &previousAssetId,
                                     int attempt)
{
    // 注意：不能遍历 library.clips() 的临时返回值并返回元素指针，
    // 临时 QVector 在循环结束后即析构；这里直接遍历 assets 内的稳定元素。
    const Mixcut::MaterialClip *best = nullptr;
    int bestScore = -100000;
    for (const Mixcut::MaterialAsset &asset : library.assets) {
        for (const Mixcut::MaterialClip &clip : asset.clips) {
            if (clip.durationMs <= 0) {
                continue;
            }
            int score = clipScore(clip, segment, semantics, attempt);
            if (!previousAssetId.isEmpty() && clip.assetId == previousAssetId) {
                score -= 220;
            }
            if (score > bestScore) {
                bestScore = score;
                best = &clip;
            }
        }
    }
    return best;
}

void applyClipToShot(Mixcut::Shot *shot, const Mixcut::MaterialClip &clip,
                     int attempt, bool transitions)
{
    if (!shot) {
        return;
    }
    shot->clipId = clip.id;
    if (clip.isVideo()) {
        // 同一素材反复使用时错开取用位置，避免每次都是同一帧。
        const qint64 available = qMax<qint64>(0, clip.availableMs());
        const qint64 shotDuration = shot->durationMs();
        const qint64 slack = qMax<qint64>(0, available - shotDuration);
        const qint64 offset = slack > 0 ? (attempt * 977) % slack : 0;
        shot->sourceInMs = clip.sourceInMs + offset;
    } else {
        shot->sourceInMs = 0;
    }
    if (!transitions) {
        shot->transition.clear();
    } else if (shot->transition.isEmpty()) {
        shot->transition = QStringLiteral("叠化");
    }
}

// 用一个镜头覆盖 [startMs, endMs)，不足 1.2 秒的尾巴并入前一个镜头。
void appendLocalCoverage(QVector<Mixcut::Shot> *shots,
                         const Mixcut::MaterialLibrary &library,
                         const Mixcut::SpeechAnalysis &speech,
                         const Mixcut::SemanticAnalysis &semantics,
                         qint64 startMs, qint64 endMs, bool transitions)
{
    Q_UNUSED(speech);
    if (!shots || endMs <= startMs) {
        return;
    }
    qint64 cursor = qMax<qint64>(0, startMs);
    int attempt = int(shots->size());
    QString previousAssetId;
    if (!shots->isEmpty()) {
        const Mixcut::MaterialClip *lastClip =
            library.clipById(shots->last().clipId);
        if (lastClip) {
            previousAssetId = lastClip->assetId;
        }
    }
    while (cursor < endMs) {
        const qint64 remaining = endMs - cursor;
        if (remaining < 300) {
            break;
        }
        const Mixcut::SemanticSegment *segment =
            Mixcut::segmentAt(semantics, cursor + remaining / 2);
        const Mixcut::MaterialClip *clip =
            pickClip(library, segment, semantics, previousAssetId, attempt);
        if (!clip) {
            break;
        }
        // 图片 3.2–4.2 秒，视频 2.4–4.6 秒；质量高的素材可以多留一会儿。
        qint64 desired = clip->isVideo()
            ? 2600 + (clip->quality % 20) * 90
            : 3400 + (clip->quality % 10) * 80;
        if (remaining <= desired + 1200) {
            desired = remaining;
        }
        Mixcut::Shot shot;
        shot.startMs = cursor;
        shot.endMs = qMin(endMs, cursor + desired);
        shot.reason = segment ? segment->visualHint : QString();
        applyClipToShot(&shot, *clip, attempt, transitions);
        shots->append(shot);
        previousAssetId = clip->assetId;
        cursor = shot.endMs;
        ++attempt;
    }
}

qint64 clampSourceIn(const Mixcut::MaterialClip &clip, qint64 sourceInMs,
                     qint64 shotDurationMs)
{
    if (!clip.isVideo()) {
        return 0;
    }
    const qint64 available = qMax<qint64>(0, clip.availableMs());
    if (available <= 0) {
        return qMax<qint64>(0, clip.sourceInMs);
    }
    const qint64 maxIn = qMax<qint64>(0, available - shotDurationMs);
    return qBound<qint64>(0, sourceInMs - clip.sourceInMs, maxIn)
        + clip.sourceInMs;
}

QString validTransition(const QString &transition, bool transitions)
{
    if (!transitions) {
        return QString();
    }
    static const QStringList allowed = {
        QStringLiteral("叠化"), QStringLiteral("闪白"),
        QStringLiteral("推近"), QStringLiteral("硬切"),
    };
    if (allowed.contains(transition)) {
        return transition == QLatin1String("硬切") ? QString() : transition;
    }
    return QStringLiteral("叠化");
}

} // namespace

namespace Mixcut {

QString subtitlePresetName(SubtitlePreset preset)
{
    switch (preset) {
    case SubtitlePreset::Keyword:
        return QStringLiteral("智能关键词");
    case SubtitlePreset::Big:
        return QStringLiteral("大字强调");
    case SubtitlePreset::Minimal:
        return QStringLiteral("极简白字");
    }
    return QStringLiteral("智能关键词");
}

SubtitlePlan buildSubtitles(const SpeechAnalysis &speech,
                            const SemanticAnalysis &semantics,
                            SubtitlePreset preset, const Canvas &canvas,
                            bool titleFx)
{
    SubtitlePlan plan;
    plan.preset = preset;
    plan.titleFx = titleFx;
    plan.titleText = semantics.title.trimmed();
    if (plan.titleText.isEmpty() && !semantics.segments.isEmpty()) {
        plan.titleText = semantics.segments.first().title;
    }
    plan.titleText = plan.titleText.left(20);
    plan.titleStartMs = 0;
    plan.titleEndMs = qBound<qint64>(
        qint64(1800), qRound64(speech.durationMs * 0.18), qint64(2800));

    const int maxChars = maxCharsForPreset(preset, canvas);
    const QString animation = animationForPreset(preset);
    const bool highlightKeywords = preset == SubtitlePreset::Keyword;

    qint64 previousEnd = 0;
    for (const Utterance &utterance : speech.utterances) {
        QString text = utterance.text;
        text.replace(QLatin1Char('\n'), QLatin1Char(' '));
        text = text.trimmed();
        if (text.isEmpty()) {
            continue;
        }
        const QVector<CharTiming> timings = buildCharTimings(utterance);
        const QVector<TextChunk> chunks = splitIntoChunks(text, maxChars);
        const qint64 utteranceDuration =
            qMax<qint64>(1, utterance.endMs - utterance.startMs);
        for (const TextChunk &chunk : chunks) {
            SubtitleCue cue;
            cue.text = chunk.text;
            qint64 start = -1;
            qint64 end = -1;
            for (int index = chunk.start; index < chunk.end; ++index) {
                if (index < 0 || index >= timings.size()) {
                    continue;
                }
                if (timings.at(index).startMs >= 0) {
                    if (start < 0 || timings.at(index).startMs < start) {
                        start = timings.at(index).startMs;
                    }
                    end = qMax(end, timings.at(index).endMs);
                }
            }
            if (start < 0 || end <= start) {
                start = utterance.startMs
                    + utteranceDuration * chunk.start / qMax(1, text.size());
                end = utterance.startMs
                    + utteranceDuration * chunk.end / qMax(1, text.size());
            }
            start = qBound(utterance.startMs, start, utterance.endMs);
            end = qBound(start + 300, end, qMax(start + 300, utterance.endMs));
            if (start < previousEnd) {
                start = previousEnd;
                end = qMax(end, start + 300);
            }
            if (end <= start) {
                continue;
            }

            const QString cleanText = stripTrailingPunctuation(cue.text);
            if (cleanText.isEmpty()) {
                continue;
            }
            cue.text = cleanText;
            cue.startMs = start;
            cue.endMs = end;
            cue.animation = animation;

            if (highlightKeywords) {
                const SemanticSegment *segment =
                    Mixcut::segmentAt(semantics, (start + end) / 2);
                QStringList candidates = segment ? segment->keywords
                                                 : QStringList();
                candidates += semantics.keywords;
                for (const Emphasis &emphasis : semantics.emphasis) {
                    if (end >= emphasis.startMs && start <= emphasis.endMs) {
                        candidates += emphasis.words;
                        cue.emphasis = true;
                    }
                }
                QStringList hits;
                for (const QString &candidate : candidates) {
                    if (isGenericKeyword(candidate)) {
                        continue;
                    }
                    if (cue.text.contains(candidate)
                        && !hits.contains(candidate)) {
                        hits.append(candidate);
                    }
                    if (hits.size() >= 2) {
                        break;
                    }
                }
                cue.keywords = hits;
            }
            previousEnd = cue.endMs;
            plan.cues.append(cue);
        }
    }

    std::sort(plan.cues.begin(), plan.cues.end(),
              [](const SubtitleCue &left, const SubtitleCue &right) {
                  return left.startMs < right.startMs;
              });
    QVector<SubtitleCue> cleaned;
    qint64 cursor = 0;
    const qint64 durationLimit = qMax<qint64>(0, speech.durationMs);
    for (SubtitleCue cue : plan.cues) {
        if (cue.startMs < cursor) {
            cue.startMs = cursor;
        }
        if (durationLimit > 0) {
            cue.endMs = qMin(cue.endMs, durationLimit);
            if (cue.startMs >= durationLimit - 120) {
                continue;
            }
        }
        if (cue.endMs <= cue.startMs + 120) {
            continue;
        }
        cleaned.append(cue);
        cursor = cue.endMs;
    }
    plan.cues = cleaned;
    plan.titleEndMs = qMin(plan.titleEndMs, qMax<qint64>(0, speech.durationMs));
    return plan;
}

int targetMainShotCount(qint64 durationMs)
{
    if (durationMs <= 0) {
        return 4;
    }
    return qBound(4, int(qCeil(durationMs / 3200.0)), 40);
}

QVector<Shot> normalizeMainShots(const QVector<Shot> &raw,
                                 const MaterialLibrary &library,
                                 const SpeechAnalysis &speech,
                                 const SemanticAnalysis &semantics,
                                 bool transitions)
{
    QVector<Shot> result;
    if (library.assets.isEmpty() || speech.durationMs <= 0) {
        return result;
    }
    const qint64 duration = speech.durationMs;
    QVector<Shot> valid;
    for (Shot shot : raw) {
        if (!library.clipById(shot.clipId)) {
            continue;
        }
        shot.startMs = qBound<qint64>(0, shot.startMs, duration);
        shot.endMs = qBound<qint64>(0, shot.endMs, duration);
        if (shot.endMs - shot.startMs < 600) {
            continue;
        }
        valid.append(shot);
    }
    std::sort(valid.begin(), valid.end(),
              [](const Shot &left, const Shot &right) {
                  return left.startMs < right.startMs;
              });

    qint64 cursor = 0;
    for (const Shot &sourceShot : valid) {
        Shot shot = sourceShot;
        const qint64 start = qMax(cursor, shot.startMs);
        if (start - cursor >= 900) {
            appendLocalCoverage(&result, library, speech, semantics, cursor,
                                start, transitions);
        }
        qint64 end = qMin(duration, shot.endMs);
        if (end <= start + 400) {
            cursor = qMax(cursor, end);
            if (cursor >= duration) {
                break;
            }
            continue;
        }
        shot.startMs = start;
        shot.endMs = end;
        const MaterialClip *clip = library.clipById(shot.clipId);
        shot.sourceInMs =
            clampSourceIn(*clip, shot.sourceInMs, shot.durationMs());
        shot.transition = validTransition(shot.transition, transitions);
        result.append(shot);
        cursor = end;
        if (cursor >= duration) {
            break;
        }
    }
    if (cursor < duration - 300) {
        appendLocalCoverage(&result, library, speech, semantics, cursor,
                            duration, transitions);
    }
    if (result.isEmpty()) {
        appendLocalCoverage(&result, library, speech, semantics, 0, duration,
                            transitions);
    }

    // 单个镜头不超过 6 秒：过长的镜头均分，避免一个画面停留太久。
    QVector<Shot> split;
    for (const Shot &shot : result) {
        if (shot.durationMs() <= 6200) {
            split.append(shot);
            continue;
        }
        const int parts = int(qCeil(shot.durationMs() / 4200.0));
        const qint64 partDuration = shot.durationMs() / parts;
        for (int part = 0; part < parts; ++part) {
            Shot piece = shot;
            piece.startMs = shot.startMs + part * partDuration;
            piece.endMs = part == parts - 1 ? shot.endMs
                                            : piece.startMs + partDuration;
            piece.transition = QString(); // 同素材拆分使用硬切
            const MaterialClip *clip = library.clipById(piece.clipId);
            if (clip && clip->isVideo()) {
                piece.sourceInMs = clip->sourceInMs
                    + ((part * partDuration) % qMax<qint64>(1, clip->availableMs()));
            }
            split.append(piece);
        }
    }

    // 最终保证首尾严格覆盖，并清掉过短尾巴。
    QVector<Shot> cleaned;
    cursor = 0;
    for (Shot shot : split) {
        if (shot.startMs > cursor + 200) {
            appendLocalCoverage(&cleaned, library, speech, semantics, cursor,
                                shot.startMs, transitions);
        }
        shot.startMs = qMax(shot.startMs, cursor);
        shot.endMs = qMax(shot.endMs, shot.startMs + 600);
        if (shot.durationMs() < 600) {
            continue;
        }
        cleaned.append(shot);
        cursor = shot.endMs;
    }
    if (cursor < duration - 200) {
        appendLocalCoverage(&cleaned, library, speech, semantics, cursor,
                            duration, transitions);
    }
    return cleaned;
}

QVector<Shot> normalizeOverlays(const QVector<Shot> &raw,
                                const QVector<Shot> &mainShots,
                                const MaterialLibrary &library,
                                const SpeechAnalysis &speech,
                                const SemanticAnalysis &semantics)
{
    QVector<Shot> result;
    if (library.assets.isEmpty() || speech.durationMs <= 0
        || mainShots.isEmpty()) {
        return result;
    }
    const qint64 duration = speech.durationMs;

    const auto mainClipIdAt = [&mainShots](qint64 timeMs) {
        for (const Shot &shot : mainShots) {
            if (timeMs >= shot.startMs && timeMs < shot.endMs) {
                return shot.clipId;
            }
        }
        return QString();
    };

    QVector<Shot> candidates;
    for (Shot shot : raw) {
        const MaterialClip *clip = library.clipById(shot.clipId);
        if (!clip) {
            continue;
        }
        shot.startMs = qBound<qint64>(0, shot.startMs, duration);
        shot.endMs = qBound<qint64>(0, shot.endMs, duration);
        if (shot.endMs - shot.startMs < 1500) {
            continue;
        }
        if (shot.endMs - shot.startMs > 6000) {
            shot.endMs = shot.startMs + 6000;
        }
        shot.sourceInMs =
            clampSourceIn(*clip, shot.sourceInMs, shot.durationMs());
        shot.transition.clear();
        candidates.append(shot);
    }

    const auto addLocalOverlays = [&]() {
        // 本地兜底：在钩子/观点/证据/行动段各补一处画中画。
        static const QStringList positions = {
            QStringLiteral("top_right"), QStringLiteral("bottom_right"),
            QStringLiteral("top_left"),
        };
        int added = 0;
        for (const SemanticSegment &segment : semantics.segments) {
            if (added >= 3) {
                break;
            }
            if (segment.durationMs() < 6000) {
                continue;
            }
            if (segment.type != SegmentType::Hook
                && segment.type != SegmentType::Point
                && segment.type != SegmentType::Evidence
                && segment.type != SegmentType::Cta) {
                continue;
            }
            const QString mainClipId =
                mainClipIdAt(segment.startMs + segment.durationMs() / 2);
            const MaterialClip *mainClip = library.clipById(mainClipId);
            const MaterialClip *picked = nullptr;
            int bestScore = -100000;
            int attempt = 0;
            for (const MaterialAsset &asset : library.assets) {
                for (const MaterialClip &clip : asset.clips) {
                    ++attempt;
                    if (mainClip && clip.assetId == mainClip->assetId) {
                        continue;
                    }
                    int score = clip.quality + (clip.isVideo() ? 0 : 12);
                    for (const QString &tag : clip.tags) {
                        for (const QString &keyword : segment.keywords) {
                            if (tag.contains(keyword) || keyword.contains(tag)) {
                                score += 16;
                                break;
                            }
                        }
                    }
                    score += (attempt * 5) % 7;
                    if (score > bestScore) {
                        bestScore = score;
                        picked = &clip;
                    }
                }
            }
            if (!picked) {
                continue;
            }
            Shot overlay;
            overlay.startMs = qMin(duration - 2600, segment.startMs + 1200);
            overlay.endMs = qMin(segment.endMs - 300,
                                 overlay.startMs + 3400);
            if (overlay.endMs - overlay.startMs < 2200) {
                continue;
            }
            overlay.clipId = picked->id;
            overlay.position = pipPositionFromKey(
                positions.at(added % positions.size()));
            overlay.sourceInMs = picked->sourceInMs;
            overlay.reason = segment.visualHint;
            candidates.append(overlay);
            ++added;
        }
    };

    const auto validateCandidates = [&]() {
        result.clear();
        std::sort(candidates.begin(), candidates.end(),
                  [](const Shot &left, const Shot &right) {
                      return left.startMs < right.startMs;
                  });
        for (const Shot &shot : candidates) {
            if (result.size() >= 4) {
                break;
            }
            bool overlaps = false;
            for (const Shot &existing : result) {
                if (shot.startMs < existing.endMs
                    && shot.endMs > existing.startMs) {
                    overlaps = true;
                    break;
                }
            }
            if (overlaps) {
                continue;
            }
            const MaterialClip *clip = library.clipById(shot.clipId);
            if (!clip) {
                continue;
            }
            // 画中画与同一时刻的任一主镜头不能是同一个素材文件，
            // 否则会出现同一张图/同一段视频同时铺满和画中画。
            bool sameAssetAsMain = false;
            for (const Shot &main : mainShots) {
                if (shot.startMs >= main.endMs || shot.endMs <= main.startMs) {
                    continue;
                }
                const MaterialClip *mainClip =
                    library.clipById(main.clipId);
                if (mainClip && mainClip->assetId == clip->assetId) {
                    sameAssetAsMain = true;
                    break;
                }
            }
            if (sameAssetAsMain) {
                continue;
            }
            result.append(shot);
        }
    };

    validateCandidates();
    // 模型没有给出画中画，或给出的全部被本地校验拒绝时，按语义段自动补。
    if (result.isEmpty() && duration >= 12000) {
        addLocalOverlays();
        validateCandidates();
    }
    return result;
}

} // namespace Mixcut
