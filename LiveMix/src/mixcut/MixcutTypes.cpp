// 文件作用：智能混剪领域模型的枚举映射与 JSON 序列化实现。
// 序列化结果用于分析缓存、Edit Plan / Timeline JSON 与结果历史。

#include "MixcutTypes.h"

#include <QJsonArray>
#include <QJsonValue>
#include <QtMath>

namespace {

QJsonArray toArray(const QStringList &values)
{
    QJsonArray array;
    for (const QString &value : values) {
        array.append(value);
    }
    return array;
}

QStringList toStringList(const QJsonArray &array)
{
    QStringList values;
    for (const QJsonValue &value : array) {
        const QString text = value.toString().trimmed();
        if (!text.isEmpty()) {
            values.append(text);
        }
    }
    return values;
}

qint64 readMs(const QJsonObject &object, const QString &key, qint64 fallback = 0)
{
    const QJsonValue value = object.value(key);
    if (value.isDouble()) {
        return qRound64(value.toDouble());
    }
    if (value.isString()) {
        bool ok = false;
        const qint64 parsed = value.toString().toLongLong(&ok);
        if (ok) {
            return parsed;
        }
    }
    return fallback;
}

QString readString(const QJsonObject &object, const QString &key,
                   const QString &fallback = QString())
{
    const QString text = object.value(key).toString().trimmed();
    return text.isEmpty() ? fallback : text;
}

QJsonArray shotsToArray(const QVector<Mixcut::Shot> &shots)
{
    QJsonArray array;
    for (const Mixcut::Shot &shot : shots) {
        array.append(shot.toJson());
    }
    return array;
}

} // namespace

namespace Mixcut {

/* ------------------------------------------------------------------ */
/* 枚举映射                                                             */
/* ------------------------------------------------------------------ */

QString materialKindKey(MaterialKind kind)
{
    return kind == MaterialKind::Video ? QStringLiteral("video")
                                       : QStringLiteral("image");
}

MaterialKind materialKindFromKey(const QString &key)
{
    return key.compare(QLatin1String("video"), Qt::CaseInsensitive) == 0
        ? MaterialKind::Video
        : MaterialKind::Image;
}

QString materialRoleKey(MaterialRole role)
{
    switch (role) {
    case MaterialRole::Product:
        return QStringLiteral("product");
    case MaterialRole::Scene:
        return QStringLiteral("scene");
    case MaterialRole::Detail:
        return QStringLiteral("detail");
    case MaterialRole::Person:
        return QStringLiteral("person");
    case MaterialRole::Text:
        return QStringLiteral("text");
    case MaterialRole::Other:
        break;
    }
    return QStringLiteral("other");
}

MaterialRole materialRoleFromKey(const QString &key)
{
    if (key.compare(QLatin1String("product"), Qt::CaseInsensitive) == 0) {
        return MaterialRole::Product;
    }
    if (key.compare(QLatin1String("scene"), Qt::CaseInsensitive) == 0) {
        return MaterialRole::Scene;
    }
    if (key.compare(QLatin1String("detail"), Qt::CaseInsensitive) == 0) {
        return MaterialRole::Detail;
    }
    if (key.compare(QLatin1String("person"), Qt::CaseInsensitive) == 0) {
        return MaterialRole::Person;
    }
    if (key.compare(QLatin1String("text"), Qt::CaseInsensitive) == 0) {
        return MaterialRole::Text;
    }
    return MaterialRole::Other;
}

QString segmentTypeKey(SegmentType type)
{
    switch (type) {
    case SegmentType::Hook:
        return QStringLiteral("hook");
    case SegmentType::Pain:
        return QStringLiteral("pain");
    case SegmentType::Point:
        return QStringLiteral("point");
    case SegmentType::Example:
        return QStringLiteral("example");
    case SegmentType::Evidence:
        return QStringLiteral("evidence");
    case SegmentType::Conclusion:
        return QStringLiteral("conclusion");
    case SegmentType::Cta:
        return QStringLiteral("cta");
    case SegmentType::Other:
        break;
    }
    return QStringLiteral("other");
}

SegmentType segmentTypeFromKey(const QString &key)
{
    if (key.compare(QLatin1String("hook"), Qt::CaseInsensitive) == 0) {
        return SegmentType::Hook;
    }
    if (key.compare(QLatin1String("pain"), Qt::CaseInsensitive) == 0) {
        return SegmentType::Pain;
    }
    if (key.compare(QLatin1String("point"), Qt::CaseInsensitive) == 0) {
        return SegmentType::Point;
    }
    if (key.compare(QLatin1String("example"), Qt::CaseInsensitive) == 0) {
        return SegmentType::Example;
    }
    if (key.compare(QLatin1String("evidence"), Qt::CaseInsensitive) == 0) {
        return SegmentType::Evidence;
    }
    if (key.compare(QLatin1String("conclusion"), Qt::CaseInsensitive) == 0) {
        return SegmentType::Conclusion;
    }
    if (key.compare(QLatin1String("cta"), Qt::CaseInsensitive) == 0) {
        return SegmentType::Cta;
    }
    return SegmentType::Other;
}

QString emotionKey(Emotion emotion)
{
    switch (emotion) {
    case Emotion::Happy:
        return QStringLiteral("happy");
    case Emotion::Excited:
        return QStringLiteral("excited");
    case Emotion::Calm:
        return QStringLiteral("calm");
    case Emotion::Serious:
        return QStringLiteral("serious");
    case Emotion::Warm:
        return QStringLiteral("warm");
    case Emotion::Surprised:
        return QStringLiteral("surprised");
    case Emotion::Neutral:
        break;
    }
    return QStringLiteral("neutral");
}

Emotion emotionFromKey(const QString &key)
{
    if (key.compare(QLatin1String("happy"), Qt::CaseInsensitive) == 0) {
        return Emotion::Happy;
    }
    if (key.compare(QLatin1String("excited"), Qt::CaseInsensitive) == 0) {
        return Emotion::Excited;
    }
    if (key.compare(QLatin1String("calm"), Qt::CaseInsensitive) == 0) {
        return Emotion::Calm;
    }
    if (key.compare(QLatin1String("serious"), Qt::CaseInsensitive) == 0) {
        return Emotion::Serious;
    }
    if (key.compare(QLatin1String("warm"), Qt::CaseInsensitive) == 0) {
        return Emotion::Warm;
    }
    if (key.compare(QLatin1String("surprised"), Qt::CaseInsensitive) == 0) {
        return Emotion::Surprised;
    }
    return Emotion::Neutral;
}

QString narrativeRoleKey(NarrativeRole role)
{
    switch (role) {
    case NarrativeRole::Hook:
        return QStringLiteral("hook");
    case NarrativeRole::Setup:
        return QStringLiteral("setup");
    case NarrativeRole::Conflict:
        return QStringLiteral("conflict");
    case NarrativeRole::Turn:
        return QStringLiteral("turn");
    case NarrativeRole::Payoff:
        return QStringLiteral("payoff");
    case NarrativeRole::Cta:
        return QStringLiteral("cta");
    case NarrativeRole::Other:
        break;
    }
    return QStringLiteral("other");
}

NarrativeRole narrativeRoleFromKey(const QString &key)
{
    if (key.compare(QLatin1String("hook"), Qt::CaseInsensitive) == 0) {
        return NarrativeRole::Hook;
    }
    if (key.compare(QLatin1String("setup"), Qt::CaseInsensitive) == 0) {
        return NarrativeRole::Setup;
    }
    if (key.compare(QLatin1String("conflict"), Qt::CaseInsensitive) == 0) {
        return NarrativeRole::Conflict;
    }
    if (key.compare(QLatin1String("turn"), Qt::CaseInsensitive) == 0) {
        return NarrativeRole::Turn;
    }
    if (key.compare(QLatin1String("payoff"), Qt::CaseInsensitive) == 0) {
        return NarrativeRole::Payoff;
    }
    if (key.compare(QLatin1String("cta"), Qt::CaseInsensitive) == 0) {
        return NarrativeRole::Cta;
    }
    return NarrativeRole::Other;
}

QString pipPositionKey(PipPosition position)
{
    switch (position) {
    case PipPosition::TopLeft:
        return QStringLiteral("top_left");
    case PipPosition::BottomLeft:
        return QStringLiteral("bottom_left");
    case PipPosition::BottomRight:
        return QStringLiteral("bottom_right");
    case PipPosition::TopRight:
        break;
    }
    return QStringLiteral("top_right");
}

PipPosition pipPositionFromKey(const QString &key)
{
    if (key.compare(QLatin1String("top_left"), Qt::CaseInsensitive) == 0) {
        return PipPosition::TopLeft;
    }
    if (key.compare(QLatin1String("bottom_left"), Qt::CaseInsensitive) == 0) {
        return PipPosition::BottomLeft;
    }
    if (key.compare(QLatin1String("bottom_right"), Qt::CaseInsensitive) == 0) {
        return PipPosition::BottomRight;
    }
    return PipPosition::TopRight;
}

QString subtitlePresetKey(SubtitlePreset preset)
{
    switch (preset) {
    case SubtitlePreset::Big:
        return QStringLiteral("big");
    case SubtitlePreset::Minimal:
        return QStringLiteral("minimal");
    case SubtitlePreset::Keyword:
        break;
    }
    return QStringLiteral("keyword");
}

SubtitlePreset subtitlePresetFromKey(const QString &key)
{
    if (key.compare(QLatin1String("big"), Qt::CaseInsensitive) == 0) {
        return SubtitlePreset::Big;
    }
    if (key.compare(QLatin1String("minimal"), Qt::CaseInsensitive) == 0) {
        return SubtitlePreset::Minimal;
    }
    return SubtitlePreset::Keyword;
}

SubtitlePreset subtitlePresetForStyle(StudioData::MixcutStyle style)
{
    switch (style) {
    case StudioData::MixcutStyle::Review:
        return SubtitlePreset::Big;
    case StudioData::MixcutStyle::Story:
        return SubtitlePreset::Minimal;
    case StudioData::MixcutStyle::Planting:
        break;
    }
    return SubtitlePreset::Keyword;
}

/* ------------------------------------------------------------------ */
/* 素材                                                                 */
/* ------------------------------------------------------------------ */

QJsonObject MaterialClip::toJson() const
{
    QJsonObject object;
    object.insert(QStringLiteral("id"), id);
    object.insert(QStringLiteral("assetId"), assetId);
    object.insert(QStringLiteral("assetName"), assetName);
    object.insert(QStringLiteral("path"), path);
    object.insert(QStringLiteral("kind"), materialKindKey(kind));
    object.insert(QStringLiteral("sourceInMs"), sourceInMs);
    object.insert(QStringLiteral("sourceOutMs"), sourceOutMs);
    object.insert(QStringLiteral("durationMs"), durationMs);
    object.insert(QStringLiteral("width"), width);
    object.insert(QStringLiteral("height"), height);
    object.insert(QStringLiteral("description"), description);
    object.insert(QStringLiteral("role"), materialRoleKey(role));
    object.insert(QStringLiteral("tags"), toArray(tags));
    object.insert(QStringLiteral("bestFor"), toArray(bestFor));
    object.insert(QStringLiteral("quality"), quality);
    object.insert(QStringLiteral("emotion"), emotionKey(emotion));
    object.insert(QStringLiteral("keyframePath"), keyframePath);
    object.insert(QStringLiteral("visual"), visual.toJson());
    return object;
}

MaterialClip MaterialClip::fromJson(const QJsonObject &object)
{
    MaterialClip clip;
    clip.id = readString(object, QStringLiteral("id"));
    clip.assetId = readString(object, QStringLiteral("assetId"));
    clip.assetName = readString(object, QStringLiteral("assetName"));
    clip.path = readString(object, QStringLiteral("path"));
    clip.kind = materialKindFromKey(
        readString(object, QStringLiteral("kind"), QStringLiteral("video")));
    clip.sourceInMs = readMs(object, QStringLiteral("sourceInMs"));
    clip.sourceOutMs = readMs(object, QStringLiteral("sourceOutMs"));
    clip.durationMs = readMs(object, QStringLiteral("durationMs"),
                             qMax<qint64>(0, clip.sourceOutMs - clip.sourceInMs));
    clip.width = object.value(QStringLiteral("width")).toInt();
    clip.height = object.value(QStringLiteral("height")).toInt();
    clip.description = readString(object, QStringLiteral("description"));
    clip.role = materialRoleFromKey(readString(object, QStringLiteral("role")));
    clip.tags = toStringList(object.value(QStringLiteral("tags")).toArray());
    clip.bestFor =
        toStringList(object.value(QStringLiteral("bestFor")).toArray());
    clip.quality = qBound(0, object.value(QStringLiteral("quality")).toInt(60),
                          100);
    clip.emotion = emotionFromKey(readString(object, QStringLiteral("emotion")));
    clip.keyframePath = readString(object, QStringLiteral("keyframePath"));
    clip.visual =
        VisualFeatures::fromJson(object.value(QStringLiteral("visual")).toObject());
    return clip;
}

QJsonObject MaterialAsset::toJson() const
{
    QJsonObject object;
    object.insert(QStringLiteral("id"), id);
    object.insert(QStringLiteral("name"), name);
    object.insert(QStringLiteral("path"), path);
    object.insert(QStringLiteral("kind"), materialKindKey(kind));
    object.insert(QStringLiteral("width"), width);
    object.insert(QStringLiteral("height"), height);
    object.insert(QStringLiteral("durationMs"), durationMs);
    object.insert(QStringLiteral("sizeBytes"), sizeBytes);
    object.insert(QStringLiteral("tags"), toArray(tags));
    object.insert(QStringLiteral("scenes"), toArray(scenes));
    QJsonArray clipArray;
    for (const MaterialClip &clip : clips) {
        clipArray.append(clip.toJson());
    }
    object.insert(QStringLiteral("clips"), clipArray);
    return object;
}

MaterialAsset MaterialAsset::fromJson(const QJsonObject &object)
{
    MaterialAsset asset;
    asset.id = readString(object, QStringLiteral("id"));
    asset.name = readString(object, QStringLiteral("name"));
    asset.path = readString(object, QStringLiteral("path"));
    asset.kind = materialKindFromKey(
        readString(object, QStringLiteral("kind"), QStringLiteral("video")));
    asset.width = object.value(QStringLiteral("width")).toInt();
    asset.height = object.value(QStringLiteral("height")).toInt();
    asset.durationMs = readMs(object, QStringLiteral("durationMs"));
    asset.sizeBytes =
        qint64(object.value(QStringLiteral("sizeBytes")).toDouble());
    asset.tags = toStringList(object.value(QStringLiteral("tags")).toArray());
    asset.scenes =
        toStringList(object.value(QStringLiteral("scenes")).toArray());
    for (const QJsonValue &value :
         object.value(QStringLiteral("clips")).toArray()) {
        asset.clips.append(MaterialClip::fromJson(value.toObject()));
    }
    return asset;
}

QVector<MaterialClip> MaterialLibrary::clips() const
{
    QVector<MaterialClip> result;
    for (const MaterialAsset &asset : assets) {
        result += asset.clips;
    }
    return result;
}

const MaterialClip *MaterialLibrary::clipById(const QString &id) const
{
    for (const MaterialAsset &asset : assets) {
        for (const MaterialClip &clip : asset.clips) {
            if (clip.id == id) {
                return &clip;
            }
        }
    }
    return nullptr;
}

MaterialClip *MaterialLibrary::clipById(const QString &id)
{
    for (MaterialAsset &asset : assets) {
        for (MaterialClip &clip : asset.clips) {
            if (clip.id == id) {
                return &clip;
            }
        }
    }
    return nullptr;
}

int MaterialLibrary::videoCount() const
{
    int count = 0;
    for (const MaterialAsset &asset : assets) {
        if (asset.kind == MaterialKind::Video) {
            ++count;
        }
    }
    return count;
}

int MaterialLibrary::imageCount() const
{
    int count = 0;
    for (const MaterialAsset &asset : assets) {
        if (asset.kind == MaterialKind::Image) {
            ++count;
        }
    }
    return count;
}

/* ------------------------------------------------------------------ */
/* 语义                                                                 */
/* ------------------------------------------------------------------ */

QJsonObject SemanticSegment::toJson() const
{
    QJsonObject object;
    object.insert(QStringLiteral("startMs"), startMs);
    object.insert(QStringLiteral("endMs"), endMs);
    object.insert(QStringLiteral("title"), title);
    object.insert(QStringLiteral("summary"), summary);
    object.insert(QStringLiteral("type"), segmentTypeKey(type));
    object.insert(QStringLiteral("keywords"), toArray(keywords));
    object.insert(QStringLiteral("visualHint"), visualHint);
    object.insert(QStringLiteral("emotion"), emotionKey(emotion));
    object.insert(QStringLiteral("narrative"), narrativeRoleKey(narrative));
    return object;
}

SemanticSegment SemanticSegment::fromJson(const QJsonObject &object)
{
    SemanticSegment segment;
    segment.startMs = readMs(object, QStringLiteral("startMs"));
    segment.endMs = readMs(object, QStringLiteral("endMs"));
    segment.title = readString(object, QStringLiteral("title"));
    segment.summary = readString(object, QStringLiteral("summary"));
    segment.type = segmentTypeFromKey(readString(object, QStringLiteral("type")));
    segment.keywords =
        toStringList(object.value(QStringLiteral("keywords")).toArray());
    segment.visualHint = readString(object, QStringLiteral("visualHint"));
    segment.emotion =
        emotionFromKey(readString(object, QStringLiteral("emotion")));
    segment.narrative = narrativeRoleFromKey(
        readString(object, QStringLiteral("narrative")));
    return segment;
}

QJsonObject Emphasis::toJson() const
{
    QJsonObject object;
    object.insert(QStringLiteral("startMs"), startMs);
    object.insert(QStringLiteral("endMs"), endMs);
    object.insert(QStringLiteral("text"), text);
    object.insert(QStringLiteral("words"), toArray(words));
    return object;
}

Emphasis Emphasis::fromJson(const QJsonObject &object)
{
    Emphasis entry;
    entry.startMs = readMs(object, QStringLiteral("startMs"));
    entry.endMs = readMs(object, QStringLiteral("endMs"));
    entry.text = readString(object, QStringLiteral("text"));
    entry.words = toStringList(object.value(QStringLiteral("words")).toArray());
    return entry;
}

QJsonObject SemanticAnalysis::toJson() const
{
    QJsonObject object;
    object.insert(QStringLiteral("title"), title);
    object.insert(QStringLiteral("description"), description);
    object.insert(QStringLiteral("tags"), toArray(tags));
    object.insert(QStringLiteral("topics"), toArray(topics));
    object.insert(QStringLiteral("keywords"), toArray(keywords));
    object.insert(QStringLiteral("narrative"), narrative);
    object.insert(QStringLiteral("emotion"), emotionKey(emotion));
    QJsonArray segmentArray;
    for (const SemanticSegment &segment : segments) {
        segmentArray.append(segment.toJson());
    }
    object.insert(QStringLiteral("segments"), segmentArray);
    QJsonArray emphasisArray;
    for (const Emphasis &entry : emphasis) {
        emphasisArray.append(entry.toJson());
    }
    object.insert(QStringLiteral("emphasis"), emphasisArray);
    return object;
}

SemanticAnalysis SemanticAnalysis::fromJson(const QJsonObject &object)
{
    SemanticAnalysis analysis;
    analysis.title = readString(object, QStringLiteral("title"));
    analysis.description = readString(object, QStringLiteral("description"));
    analysis.tags = toStringList(object.value(QStringLiteral("tags")).toArray());
    analysis.topics =
        toStringList(object.value(QStringLiteral("topics")).toArray());
    analysis.keywords =
        toStringList(object.value(QStringLiteral("keywords")).toArray());
    analysis.narrative = readString(object, QStringLiteral("narrative"));
    analysis.emotion =
        emotionFromKey(readString(object, QStringLiteral("emotion")));
    for (const QJsonValue &value :
         object.value(QStringLiteral("segments")).toArray()) {
        analysis.segments.append(SemanticSegment::fromJson(value.toObject()));
    }
    for (const QJsonValue &value :
         object.value(QStringLiteral("emphasis")).toArray()) {
        analysis.emphasis.append(Emphasis::fromJson(value.toObject()));
    }
    return analysis;
}

const SemanticSegment *segmentAt(const SemanticAnalysis &semantics,
                                 qint64 timeMs)
{
    for (const SemanticSegment &segment : semantics.segments) {
        if (timeMs >= segment.startMs && timeMs <= segment.endMs) {
            return &segment;
        }
    }
    return semantics.segments.isEmpty() ? nullptr : &semantics.segments.first();
}

/* ------------------------------------------------------------------ */
/* 镜头                                                                 */
/* ------------------------------------------------------------------ */

QJsonObject Shot::toJson() const
{
    QJsonObject object;
    object.insert(QStringLiteral("startMs"), startMs);
    object.insert(QStringLiteral("endMs"), endMs);
    object.insert(QStringLiteral("clipId"), clipId);
    object.insert(QStringLiteral("transition"), transition);
    object.insert(QStringLiteral("transitionMs"), transitionMs);
    object.insert(QStringLiteral("sourceInMs"), sourceInMs);
    object.insert(QStringLiteral("reason"), reason);
    object.insert(QStringLiteral("position"), pipPositionKey(position));
    return object;
}

Shot Shot::fromJson(const QJsonObject &object)
{
    Shot shot;
    shot.startMs = readMs(object, QStringLiteral("startMs"));
    shot.endMs = readMs(object, QStringLiteral("endMs"));
    shot.clipId = readString(object, QStringLiteral("clipId"));
    shot.transition = readString(object, QStringLiteral("transition"));
    shot.transitionMs =
        readMs(object, QStringLiteral("transitionMs"), 400);
    shot.sourceInMs = readMs(object, QStringLiteral("sourceInMs"));
    shot.reason = readString(object, QStringLiteral("reason"));
    shot.position =
        pipPositionFromKey(readString(object, QStringLiteral("position")));
    return shot;
}

/* ------------------------------------------------------------------ */
/* 字幕与音频                                                           */
/* ------------------------------------------------------------------ */

QJsonObject SubtitleCue::toJson() const
{
    QJsonObject object;
    object.insert(QStringLiteral("startMs"), startMs);
    object.insert(QStringLiteral("endMs"), endMs);
    object.insert(QStringLiteral("text"), text);
    object.insert(QStringLiteral("keywords"), toArray(keywords));
    object.insert(QStringLiteral("emphasis"), emphasis);
    object.insert(QStringLiteral("animation"), animation);
    return object;
}

SubtitleCue SubtitleCue::fromJson(const QJsonObject &object)
{
    SubtitleCue cue;
    cue.startMs = readMs(object, QStringLiteral("startMs"));
    cue.endMs = readMs(object, QStringLiteral("endMs"));
    cue.text = readString(object, QStringLiteral("text"));
    cue.keywords =
        toStringList(object.value(QStringLiteral("keywords")).toArray());
    cue.emphasis = object.value(QStringLiteral("emphasis")).toBool();
    cue.animation = readString(object, QStringLiteral("animation"));
    return cue;
}

QJsonObject SubtitlePlan::toJson() const
{
    QJsonObject object;
    object.insert(QStringLiteral("preset"), subtitlePresetKey(preset));
    object.insert(QStringLiteral("titleFx"), titleFx);
    object.insert(QStringLiteral("titleText"), titleText);
    object.insert(QStringLiteral("titleStartMs"), titleStartMs);
    object.insert(QStringLiteral("titleEndMs"), titleEndMs);
    QJsonArray cueArray;
    for (const SubtitleCue &cue : cues) {
        cueArray.append(cue.toJson());
    }
    object.insert(QStringLiteral("cues"), cueArray);
    return object;
}

SubtitlePlan SubtitlePlan::fromJson(const QJsonObject &object)
{
    SubtitlePlan plan;
    plan.preset = subtitlePresetFromKey(
        readString(object, QStringLiteral("preset")));
    plan.titleFx = object.value(QStringLiteral("titleFx")).toBool(true);
    plan.titleText = readString(object, QStringLiteral("titleText"));
    plan.titleStartMs = readMs(object, QStringLiteral("titleStartMs"));
    plan.titleEndMs = readMs(object, QStringLiteral("titleEndMs"), 2600);
    for (const QJsonValue &value :
         object.value(QStringLiteral("cues")).toArray()) {
        plan.cues.append(SubtitleCue::fromJson(value.toObject()));
    }
    return plan;
}

QJsonObject AudioCue::toJson() const
{
    QJsonObject object;
    object.insert(QStringLiteral("kind"),
                  kind == AudioKind::Bgm ? QStringLiteral("bgm")
                                         : QStringLiteral("sfx"));
    object.insert(QStringLiteral("name"), name);
    object.insert(QStringLiteral("url"), url);
    object.insert(QStringLiteral("startMs"), startMs);
    object.insert(QStringLiteral("endMs"), endMs);
    object.insert(QStringLiteral("durationMs"), durationMs);
    object.insert(QStringLiteral("volume"), volume);
    object.insert(QStringLiteral("loop"), loop);
    return object;
}

AudioCue AudioCue::fromJson(const QJsonObject &object)
{
    AudioCue cue;
    cue.kind = readString(object, QStringLiteral("kind")) == QLatin1String("bgm")
        ? AudioKind::Bgm
        : AudioKind::Sfx;
    cue.name = readString(object, QStringLiteral("name"));
    cue.url = readString(object, QStringLiteral("url"));
    cue.startMs = readMs(object, QStringLiteral("startMs"));
    cue.endMs = readMs(object, QStringLiteral("endMs"));
    cue.durationMs = readMs(object, QStringLiteral("durationMs"));
    cue.volume = object.value(QStringLiteral("volume")).toDouble(0.1);
    cue.loop = object.value(QStringLiteral("loop")).toBool();
    return cue;
}

QJsonObject AudioPlan::toJson() const
{
    QJsonObject object;
    object.insert(QStringLiteral("bgmEnabled"), bgmEnabled);
    object.insert(QStringLiteral("sfxEnabled"), sfxEnabled);
    object.insert(QStringLiteral("bgm"), bgm.toJson());
    QJsonArray sfxArray;
    for (const AudioCue &cue : sfx) {
        sfxArray.append(cue.toJson());
    }
    object.insert(QStringLiteral("sfx"), sfxArray);
    return object;
}

AudioPlan AudioPlan::fromJson(const QJsonObject &object)
{
    AudioPlan plan;
    plan.bgmEnabled = object.value(QStringLiteral("bgmEnabled")).toBool(true);
    plan.sfxEnabled = object.value(QStringLiteral("sfxEnabled")).toBool(true);
    plan.bgm = AudioCue::fromJson(object.value(QStringLiteral("bgm")).toObject());
    for (const QJsonValue &value :
         object.value(QStringLiteral("sfx")).toArray()) {
        plan.sfx.append(AudioCue::fromJson(value.toObject()));
    }
    return plan;
}

QJsonObject EffectCue::toJson() const
{
    QJsonObject object;
    object.insert(QStringLiteral("title"), title);
    object.insert(QStringLiteral("startMs"), startMs);
    object.insert(QStringLiteral("endMs"), endMs);
    return object;
}

EffectCue EffectCue::fromJson(const QJsonObject &object)
{
    EffectCue cue;
    cue.title = readString(object, QStringLiteral("title"));
    cue.startMs = readMs(object, QStringLiteral("startMs"));
    cue.endMs = readMs(object, QStringLiteral("endMs"));
    return cue;
}

QJsonObject FilterCue::toJson() const
{
    QJsonObject object;
    object.insert(QStringLiteral("title"), title);
    object.insert(QStringLiteral("startMs"), startMs);
    object.insert(QStringLiteral("endMs"), endMs);
    object.insert(QStringLiteral("intensity"), intensity);
    return object;
}

FilterCue FilterCue::fromJson(const QJsonObject &object)
{
    FilterCue cue;
    cue.title = readString(object, QStringLiteral("title"));
    cue.startMs = readMs(object, QStringLiteral("startMs"));
    cue.endMs = readMs(object, QStringLiteral("endMs"));
    cue.intensity = object.value(QStringLiteral("intensity")).toDouble(35.0);
    return cue;
}

/* ------------------------------------------------------------------ */
/* 统一剪辑计划                                                         */
/* ------------------------------------------------------------------ */

QJsonObject EditPlan::toJson() const
{
    QJsonObject object;
    object.insert(QStringLiteral("version"), version);
    object.insert(QStringLiteral("createdAt"), createdAt);
    object.insert(QStringLiteral("canvas"), canvas.toJson());
    object.insert(QStringLiteral("durationMs"), durationMs);
    object.insert(QStringLiteral("style"), static_cast<int>(style));
    object.insert(QStringLiteral("script"), script);
    object.insert(QStringLiteral("scriptPrompt"), scriptPrompt);
    object.insert(QStringLiteral("voiceId"), voiceId);
    object.insert(QStringLiteral("ttsPath"), ttsPath);
    object.insert(QStringLiteral("speech"), speech.toJson());
    object.insert(QStringLiteral("semantics"), semantics.toJson());

    QJsonArray assetArray;
    for (const MaterialAsset &asset : library.assets) {
        assetArray.append(asset.toJson());
    }
    object.insert(QStringLiteral("materials"), assetArray);

    object.insert(QStringLiteral("shots"), shotsToArray(shots));
    object.insert(QStringLiteral("overlays"), shotsToArray(overlays));
    object.insert(QStringLiteral("subtitles"), subtitles.toJson());
    object.insert(QStringLiteral("audio"), audio.toJson());
    QJsonArray effectArray;
    for (const EffectCue &effect : effects) {
        effectArray.append(effect.toJson());
    }
    object.insert(QStringLiteral("effects"), effectArray);
    QJsonArray filterArray;
    for (const FilterCue &filter : filters) {
        filterArray.append(filter.toJson());
    }
    object.insert(QStringLiteral("filters"), filterArray);
    return object;
}

QJsonObject EditPlan::timelineJson() const
{
    QJsonObject object;
    object.insert(QStringLiteral("version"), 1);
    object.insert(QStringLiteral("durationMs"), durationMs);
    object.insert(QStringLiteral("canvas"), canvas.toJson());
    object.insert(QStringLiteral("voiceId"), voiceId);

    QJsonObject videoTrack;
    videoTrack.insert(QStringLiteral("name"), QStringLiteral("主画面"));
    videoTrack.insert(QStringLiteral("shots"), shotsToArray(shots));
    QJsonObject overlayTrack;
    overlayTrack.insert(QStringLiteral("name"), QStringLiteral("画中画"));
    overlayTrack.insert(QStringLiteral("shots"), shotsToArray(overlays));
    QJsonArray tracks;
    tracks.append(videoTrack);
    tracks.append(overlayTrack);

    QJsonObject subtitleTrack;
    subtitleTrack.insert(QStringLiteral("name"), QStringLiteral("字幕"));
    subtitleTrack.insert(QStringLiteral("plan"), subtitles.toJson());
    QJsonObject audioTrack;
    audioTrack.insert(QStringLiteral("name"), QStringLiteral("音频"));
    audioTrack.insert(QStringLiteral("plan"), audio.toJson());
    tracks.append(subtitleTrack);
    tracks.append(audioTrack);

    QJsonArray effectArray;
    for (const EffectCue &effect : effects) {
        effectArray.append(effect.toJson());
    }
    object.insert(QStringLiteral("effects"), effectArray);
    QJsonArray filterArray;
    for (const FilterCue &filter : filters) {
        filterArray.append(filter.toJson());
    }
    object.insert(QStringLiteral("filters"), filterArray);
    object.insert(QStringLiteral("tracks"), tracks);
    return object;
}

/* ------------------------------------------------------------------ */
/* 结果历史持久化                                                       */
/* ------------------------------------------------------------------ */

QJsonObject mixcutResultToJson(const StudioData::MixcutResult &result)
{
    QJsonObject object;
    object.insert(QStringLiteral("id"), result.id);
    object.insert(QStringLiteral("projectName"), result.projectName);
    object.insert(QStringLiteral("title"), result.title);
    object.insert(QStringLiteral("description"), result.description);
    object.insert(QStringLiteral("tags"), toArray(result.tags));
    object.insert(QStringLiteral("captionTitle"), result.captionTitle);
    object.insert(QStringLiteral("voiceId"), result.voiceId);
    object.insert(QStringLiteral("style"), static_cast<int>(result.style));
    object.insert(QStringLiteral("ratio"), result.ratio);
    object.insert(QStringLiteral("durationLabel"), result.durationLabel);
    object.insert(QStringLiteral("fileSize"), result.fileSize);
    object.insert(QStringLiteral("generatedAt"), result.generatedAt);
    object.insert(QStringLiteral("sourceLabel"), result.sourceLabel);
    object.insert(QStringLiteral("scriptPrompt"), result.scriptPrompt);
    object.insert(QStringLiteral("script"), result.script);
    object.insert(QStringLiteral("sceneCount"), result.sceneCount);
    object.insert(QStringLiteral("subtitleCount"), result.subtitleCount);
    object.insert(QStringLiteral("sfxCount"), result.sfxCount);
    object.insert(QStringLiteral("bgm"), result.bgm);
    object.insert(QStringLiteral("draftGenerated"), result.draftGenerated);
    object.insert(QStringLiteral("filePath"), result.filePath);
    object.insert(QStringLiteral("previewPath"), result.previewPath);
    object.insert(QStringLiteral("draftPath"), result.draftPath);
    object.insert(QStringLiteral("draftId"), result.draftId);
    object.insert(QStringLiteral("editPlanPath"), result.editPlanPath);
    object.insert(QStringLiteral("videoCodec"), result.videoCodec);
    object.insert(QStringLiteral("outputBoth"), result.outputBoth);
    QJsonArray materials;
    for (const StudioData::MixcutMaterial &material : result.materials) {
        if (material.filePath.trimmed().isEmpty()) {
            continue;
        }
        QJsonObject item;
        item.insert(QStringLiteral("id"), material.id);
        item.insert(QStringLiteral("name"), material.name);
        item.insert(QStringLiteral("kind"),
                    material.kind == StudioData::MixcutMaterialKind::Video
                        ? QStringLiteral("video")
                        : QStringLiteral("image"));
        item.insert(QStringLiteral("durationMs"), material.durationMs);
        item.insert(QStringLiteral("resolution"), material.resolution);
        item.insert(QStringLiteral("size"), material.size);
        item.insert(QStringLiteral("shots"), material.shots);
        item.insert(QStringLiteral("scenes"), toArray(material.scenes));
        item.insert(QStringLiteral("topics"), toArray(material.topics));
        item.insert(QStringLiteral("filePath"), material.filePath);
        materials.append(item);
    }
    object.insert(QStringLiteral("materials"), materials);
    QJsonArray highlights;
    for (const auto &highlight : result.highlights) {
        QJsonArray pair;
        pair.append(highlight.first);
        pair.append(highlight.second);
        highlights.append(pair);
    }
    object.insert(QStringLiteral("highlights"), highlights);
    return object;
}

StudioData::MixcutResult mixcutResultFromJson(const QJsonObject &object)
{
    StudioData::MixcutResult result;
    result.id = readString(object, QStringLiteral("id"));
    result.projectName = readString(object, QStringLiteral("projectName"));
    result.title = readString(object, QStringLiteral("title"));
    result.description = readString(object, QStringLiteral("description"));
    result.tags = toStringList(object.value(QStringLiteral("tags")).toArray());
    result.captionTitle = readString(object, QStringLiteral("captionTitle"));
    result.voiceId = readString(object, QStringLiteral("voiceId"),
                                QStringLiteral("zh_female_vv_uranus_bigtts"));
    const int style = object.value(QStringLiteral("style")).toInt(0);
    result.style = style == 1 ? StudioData::MixcutStyle::Review
        : (style == 2 ? StudioData::MixcutStyle::Story
                      : StudioData::MixcutStyle::Planting);
    result.ratio = readString(object, QStringLiteral("ratio"),
                              QStringLiteral("9:16"));
    result.durationLabel = readString(object, QStringLiteral("durationLabel"),
                                      QStringLiteral("00:00"));
    result.fileSize = readString(object, QStringLiteral("fileSize"));
    result.generatedAt =
        static_cast<qint64>(object.value(QStringLiteral("generatedAt")).toDouble());
    result.sourceLabel = readString(object, QStringLiteral("sourceLabel"));
    result.scriptPrompt = readString(object, QStringLiteral("scriptPrompt"));
    result.script = readString(object, QStringLiteral("script"));
    result.sceneCount = object.value(QStringLiteral("sceneCount")).toInt();
    result.subtitleCount =
        object.value(QStringLiteral("subtitleCount")).toInt();
    result.sfxCount = object.value(QStringLiteral("sfxCount")).toInt();
    result.bgm = readString(object, QStringLiteral("bgm"));
    result.draftGenerated =
        object.value(QStringLiteral("draftGenerated")).toBool();
    result.filePath = readString(object, QStringLiteral("filePath"));
    result.previewPath = readString(object, QStringLiteral("previewPath"));
    result.draftPath = readString(object, QStringLiteral("draftPath"));
    result.draftId = readString(object, QStringLiteral("draftId"));
    result.editPlanPath = readString(object, QStringLiteral("editPlanPath"));
    result.videoCodec = readString(object, QStringLiteral("videoCodec"));
    result.outputBoth = object.value(QStringLiteral("outputBoth")).toBool();
    for (const QJsonValue &value :
         object.value(QStringLiteral("materials")).toArray()) {
        const QJsonObject item = value.toObject();
        StudioData::MixcutMaterial material;
        material.id = readString(item, QStringLiteral("id"));
        material.name = readString(item, QStringLiteral("name"));
        material.kind =
            readString(item, QStringLiteral("kind")) == QLatin1String("image")
            ? StudioData::MixcutMaterialKind::Image
            : StudioData::MixcutMaterialKind::Video;
        material.durationMs = readMs(item, QStringLiteral("durationMs"));
        material.resolution = readString(item, QStringLiteral("resolution"));
        material.size = readString(item, QStringLiteral("size"));
        material.shots = item.value(QStringLiteral("shots")).toInt();
        material.scenes =
            toStringList(item.value(QStringLiteral("scenes")).toArray());
        material.topics =
            toStringList(item.value(QStringLiteral("topics")).toArray());
        material.filePath = readString(item, QStringLiteral("filePath"));
        if (!material.filePath.isEmpty()) {
            result.materials.append(material);
        }
    }
    for (const QJsonValue &value :
         object.value(QStringLiteral("highlights")).toArray()) {
        const QJsonArray pair = value.toArray();
        if (pair.size() >= 2) {
            result.highlights.append(
                {pair.at(0).toString(), pair.at(1).toString()});
        }
    }
    return result;
}

} // namespace Mixcut
