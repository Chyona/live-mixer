// 文件作用：剪辑计划组件生成实现（字幕系统 + 镜头规划）。

#include "OralEditPlanBuilder.h"

#include <QSet>
#include <QStringList>

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

// 记录每个字符对应的起止时间；用于把字幕块对齐到逐词时间线。
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

QVector<CharTiming> buildCharTimings(const Oral::Utterance &utterance)
{
    const QString text = utterance.text;
    QVector<CharTiming> timings(text.size());
    if (utterance.words.isEmpty()) {
        return timings;
    }

    int cursor = 0;
    int mapped = 0;
    for (const Oral::Word &word : utterance.words) {
        const QString cleaned = stripAllSpaces(word.text);
        if (cleaned.isEmpty()) {
            continue;
        }
        // ASR 的逐词文本通常能在分句文本中直接找到；找不到时从游标继续，
        // 避免因为标点/空格差异导致整句时间线失效。
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
    // 映射覆盖太低时视为不可用，让调用方回退到按字数分摊。
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
        // 标点处优先断句；过长时在下一个非标点字符前强制断开。
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

    // 去掉以标点/空白开头的块，保证每条字幕第一屏有实际文字。
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

    // 强制断行可能留下 1–2 个字的尾巴（如「…小技」+「巧」）。把过短的
    // 块并入前一块，避免字幕闪一下单个字；合并后仍允许略超最大字数。
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

int maxCharsForPreset(Oral::SubtitlePreset preset, const Oral::Canvas &canvas)
{
    const bool portrait = canvas.height >= canvas.width;
    switch (preset) {
    case Oral::SubtitlePreset::Keyword:
        return portrait ? 13 : 17;
    case Oral::SubtitlePreset::Big:
        return portrait ? 10 : 13;
    case Oral::SubtitlePreset::Minimal:
        return portrait ? 15 : 19;
    }
    return 13;
}

QString animationForPreset(Oral::SubtitlePreset preset)
{
    switch (preset) {
    case Oral::SubtitlePreset::Keyword:
        return QStringLiteral("渐显");
    case Oral::SubtitlePreset::Big:
        return QStringLiteral("弹近");
    case Oral::SubtitlePreset::Minimal:
        return QStringLiteral("渐显");
    }
    return QString();
}

const Oral::SemanticSegment *segmentAt(const Oral::SemanticAnalysis &semantics,
                                       qint64 timeMs)
{
    for (const Oral::SemanticSegment &segment : semantics.segments) {
        if (timeMs >= segment.startMs && timeMs <= segment.endMs) {
            return &segment;
        }
    }
    return semantics.segments.isEmpty() ? nullptr : &semantics.segments.first();
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

int materialScore(const Oral::MaterialProfile &material,
                  const Oral::SemanticSegment *segment,
                  const Oral::SemanticAnalysis &semantics, int shotIndex)
{
    int score = 40 + material.quality / 2;
    const QStringList keywords =
        segment ? segment->keywords : semantics.keywords;
    for (const QString &tag : material.tags) {
        for (const QString &keyword : keywords) {
            if (tag.contains(keyword) || keyword.contains(tag)) {
                score += 18;
                break;
            }
        }
    }
    // 用途与段落类型的亲和度：开场更适合人物/场景，卖点更适合产品/细节。
    const QString role = material.role;
    if (segment) {
        switch (segment->type) {
        case Oral::SegmentType::Hook:
            score += role == QLatin1String("person")
                    || role == QLatin1String("scene")
                ? 20
                : 0;
            break;
        case Oral::SegmentType::Pain:
            score += role == QLatin1String("product")
                    || role == QLatin1String("detail")
                ? 18
                : 0;
            break;
        case Oral::SegmentType::Point:
            score += role == QLatin1String("product")
                    || role == QLatin1String("detail")
                ? 16
                : 0;
            break;
        case Oral::SegmentType::Example:
            score += role == QLatin1String("scene") ? 18 : 0;
            break;
        case Oral::SegmentType::Evidence:
            score += role == QLatin1String("text") ? 18 : 0;
            break;
        case Oral::SegmentType::Cta:
            score += role == QLatin1String("product")
                    || role == QLatin1String("person")
                ? 16
                : 0;
            break;
        default:
            break;
        }
    }
    // 轻微轮换，避免所有镜头都选同一个高分素材。
    score += (shotIndex * 7) % 11;
    return score;
}

const Oral::MaterialProfile *pickMaterial(
    const QVector<Oral::MaterialProfile> &materials,
    const Oral::SemanticSegment *segment,
    const Oral::SemanticAnalysis &semantics, int shotIndex,
    const QString &previousMaterialId)
{
    const Oral::MaterialProfile *best = nullptr;
    int bestScore = -1;
    for (const Oral::MaterialProfile &material : materials) {
        int score = materialScore(material, segment, semantics, shotIndex);
        if (!previousMaterialId.isEmpty() && material.id == previousMaterialId
            && materials.size() > 1) {
            score -= 120;
        }
        if (score > bestScore) {
            bestScore = score;
            best = &material;
        }
    }
    return best;
}

qint64 safeStartLimit(int durationMs)
{
    return qMin<qint64>(800, qMax<qint64>(0, durationMs / 10));
}

qint64 safeEndLimit(int durationMs)
{
    return qMax<qint64>(0, durationMs - safeStartLimit(durationMs));
}

// 统一校验镜头：时长、边界、重叠、素材来源。
QVector<Oral::Shot> normalizeShots(QVector<Oral::Shot> shots,
                                   const QVector<Oral::MaterialProfile> &materials,
                                   qint64 durationMs, bool transitions)
{
    QHash<QString, const Oral::MaterialProfile *> materialById;
    for (const Oral::MaterialProfile &material : materials) {
        materialById.insert(material.id, &material);
    }
    std::sort(shots.begin(), shots.end(),
              [](const Oral::Shot &left, const Oral::Shot &right) {
                  return left.startMs < right.startMs;
              });

    QVector<Oral::Shot> result;
    const qint64 startLimit = safeStartLimit(durationMs);
    const qint64 endLimit = safeEndLimit(durationMs);
    if (endLimit - startLimit < 1500) {
        return result;
    }
    qint64 previousEnd = startLimit;
    for (Oral::Shot shot : shots) {
        const auto it = materialById.constFind(shot.materialId);
        if (it == materialById.constEnd()) {
            continue;
        }
        const Oral::MaterialProfile &material = *it.value();
        shot.materialKind = material.kind;
        // 时长限制在 1.5–4 秒之间，并保证不越过片尾安全区。
        // 注意：不能直接用 qBound(start+1500, …, endLimit)，镜头靠近片尾时
        // 下界可能大于上界；先计算可用上界并做显式钳制。
        qint64 start = qMax(startLimit, qMin(shot.startMs, endLimit));
        if (start < previousEnd + 200) {
            start = previousEnd + 200;
        }
        if (endLimit - start < 1200) {
            continue;
        }
        const qint64 maxEnd = qMin<qint64>(start + 4000, endLimit);
        qint64 end = qMax<qint64>(start + 1500, qMin(shot.endMs, maxEnd));
        end = qMin(end, maxEnd);
        if (end - start < 1200) {
            continue;
        }
        shot.startMs = start;
        shot.endMs = end;

        // 视频素材：镜头时长不能超过素材本身；不够时允许 ffmpeg 循环。
        if (material.kind == Oral::MaterialKind::Video && material.durationMs > 0) {
            const qint64 maxIn =
                qMax<qint64>(0, material.durationMs - (end - start));
            shot.sourceInMs = qBound<qint64>(0, shot.sourceInMs, maxIn);
        } else {
            shot.sourceInMs = 0;
        }
        if (!transitions) {
            shot.transition.clear();
        } else if (shot.transition.isEmpty()) {
            shot.transition = QStringLiteral("叠化");
        }
        previousEnd = end;
        result.append(shot);
    }
    return result;
}

QVector<Oral::Shot> fillShots(const Oral::SpeechAnalysis &speech,
                              const Oral::SemanticAnalysis &semantics,
                              const QVector<Oral::MaterialProfile> &materials,
                              int targetCount, bool transitions,
                              QVector<Oral::Shot> existing)
{
    if (materials.isEmpty() || targetCount <= 0) {
        return existing;
    }
    const qint64 duration = qMax<qint64>(2000, speech.durationMs);
    const qint64 startLimit = safeStartLimit(duration);
    const qint64 endLimit = safeEndLimit(duration);
    if (endLimit - startLimit < 2000) {
        return existing;
    }

    // 目标数量不能超过「每 2.5 秒最多一个镜头」的物理上限。
    const int maxByDuration =
        qMax(1, int((endLimit - startLimit) / 2500));
    const int target = qMin(targetCount, maxByDuration);

    // 计算现有镜头的最大空隙，优先在空隙里补镜头，保持分布均匀。
    QString previousMaterial = existing.isEmpty()
        ? QString()
        : existing.last().materialId;
    int attempts = 0;
    while (existing.size() < target && attempts < target * 8) {
        ++attempts;
        std::sort(existing.begin(), existing.end(),
                  [](const Oral::Shot &left, const Oral::Shot &right) {
                      return left.startMs < right.startMs;
                  });
        qint64 bestGapStart = startLimit;
        qint64 bestGapEnd = endLimit;
        qint64 bestGap = endLimit - startLimit;
        qint64 cursor = startLimit;
        for (const Oral::Shot &shot : existing) {
            const qint64 gap = shot.startMs - cursor;
            if (gap > bestGap) {
                bestGap = gap;
                bestGapStart = cursor;
                bestGapEnd = shot.startMs;
            }
            cursor = qMax(cursor, shot.endMs);
        }
        const qint64 tailGap = endLimit - cursor;
        if (tailGap > bestGap) {
            bestGap = tailGap;
            bestGapStart = cursor;
            bestGapEnd = endLimit;
        }
        if (bestGap < 1800) {
            break;
        }

        const qint64 center = (bestGapStart + bestGapEnd) / 2;
        const qint64 durationGuess =
            qBound<qint64>(qint64(2200), bestGap / 2, qint64(3200));
        Oral::Shot shot;
        shot.startMs = qMax(bestGapStart + 200, center - durationGuess / 2);
        shot.endMs = qMin(bestGapEnd - 200, shot.startMs + durationGuess);
        if (shot.endMs - shot.startMs < 1500) {
            break;
        }
        const Oral::SemanticSegment *segment = segmentAt(semantics, center);
        const Oral::MaterialProfile *material =
            pickMaterial(materials, segment, semantics, int(existing.size()),
                         previousMaterial);
        if (!material) {
            break;
        }
        shot.materialId = material->id;
        shot.materialKind = material->kind;
        shot.shotType = segment && segment->type == Oral::SegmentType::Hook
            ? Oral::ShotType::Person
            : (material->role == QLatin1String("detail")
                   ? Oral::ShotType::Detail
                   : Oral::ShotType::Cutaway);
        shot.reason = segment ? segment->visualHint : QString();
        if (transitions) {
            shot.transition = QStringLiteral("叠化");
        }
        previousMaterial = material->id;
        existing.append(shot);
    }

    return normalizeShots(existing, materials, duration, transitions);
}

} // namespace

namespace Oral {

int brollCountForDensity(Density density)
{
    switch (density) {
    case Density::Low:
        return 8;
    case Density::High:
        return 16;
    case Density::Medium:
        break;
    }
    return 12;
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
    // 标题最多 20 字，避免在画面上折行过多。
    plan.titleText = plan.titleText.left(20);
    plan.titleStartMs = 0;
    plan.titleEndMs = qBound<qint64>(
        qint64(1800), qRound64(speech.durationMs * 0.18), qint64(2800));

    const int maxChars = maxCharsForPreset(preset, canvas);
    const QString animation = animationForPreset(preset);
    const bool highlightKeywords = preset == SubtitlePreset::Keyword;

    QSet<QString> globalKeywords;
    for (const QString &keyword : semantics.keywords) {
        if (!isGenericKeyword(keyword)) {
            globalKeywords.insert(keyword);
        }
    }
    for (const QString &topic : semantics.topics) {
        if (!isGenericKeyword(topic)) {
            globalKeywords.insert(topic);
        }
    }
    for (const Emphasis &emphasis : semantics.emphasis) {
        for (const QString &word : emphasis.words) {
            if (!isGenericKeyword(word)) {
                globalKeywords.insert(word);
            }
        }
    }

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
            // 优先使用逐词时间线；不可用时按字符位置分摊整句时长。
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
                const Oral::SemanticSegment *segment =
                    segmentAt(semantics, (start + end) / 2);
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
                    if (cue.text.contains(candidate) && !hits.contains(candidate)) {
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

    // 字幕轨道不允许时间重叠：逐条向后顺延；实在放不下的短句直接丢弃。
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
            // 最后一条字幕不能越过片尾。
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

ShotPlan buildShotPlan(const SpeechAnalysis &speech,
                       const SemanticAnalysis &semantics,
                       const QVector<MaterialProfile> &materials, int targetCount,
                       Density density, const QVector<Shot> &llmShots,
                       bool transitions)
{
    ShotPlan plan;
    plan.density = density;
    const qint64 duration = qMax<qint64>(2000, speech.durationMs);
    const qint64 startLimit = safeStartLimit(duration);
    const qint64 endLimit = safeEndLimit(duration);
    // 短口播不能塞入 UI 的 8/12/16 处镜头；按「每 2.5 秒最多一个」限流，
    // 保证口播原画面仍有呼吸空间，而不是被 B-Roll 铺满。
    const int maxByDuration =
        qMax(1, int((endLimit - startLimit) / 2500));
    plan.targetCount = qMax(1, qMin(targetCount, maxByDuration));

    QVector<Shot> normalized = normalizeShots(llmShots, materials, duration,
                                              transitions);
    // 模型给多了：均匀抽样保留，避免全部集中在开头。
    if (normalized.size() > plan.targetCount) {
        QVector<Shot> sampled;
        const double step = double(normalized.size()) / plan.targetCount;
        for (int index = 0; index < plan.targetCount; ++index) {
            const int sourceIndex =
                qBound(0, int(index * step), normalized.size() - 1);
            sampled.append(normalized.at(sourceIndex));
        }
        normalized = normalizeShots(sampled, materials, duration, transitions);
    }
    // 模型给少了或完全没给：本地按语义段与素材标签补齐。
    plan.shots = fillShots(speech, semantics, materials, plan.targetCount,
                           transitions, normalized);
    plan.targetCount = plan.shots.size();
    return plan;
}

} // namespace Oral
