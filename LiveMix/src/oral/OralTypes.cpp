// 文件作用：智能口播领域模型的枚举映射与 JSON 序列化实现。
// 序列化结果既用于本地分析缓存，也用于最终的 Edit Plan / Timeline JSON。

#include "OralTypes.h"

#include <QJsonArray>
#include <QJsonValue>
#include <QtMath>

namespace {

// 通用：把字符串数组转换为 QJsonArray。
QJsonArray toArray(const QStringList &values)
{
    QJsonArray array;
    for (const QString &value : values) {
        array.append(value);
    }
    return array;
}

// 通用：把 QJsonArray 转换为去空白的字符串数组。
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

// 通用：读取毫秒时间；兼容 ms / startMs / start_ms 三种写法。
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

} // namespace

namespace Oral {

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

QString shotTypeKey(ShotType type)
{
    switch (type) {
    case ShotType::Cutaway:
        return QStringLiteral("cutaway");
    case ShotType::Detail:
        return QStringLiteral("detail");
    case ShotType::Scene:
        return QStringLiteral("scene");
    case ShotType::Product:
        return QStringLiteral("product");
    case ShotType::Person:
        return QStringLiteral("person");
    case ShotType::Other:
        break;
    }
    return QStringLiteral("other");
}

ShotType shotTypeFromKey(const QString &key)
{
    if (key.compare(QLatin1String("cutaway"), Qt::CaseInsensitive) == 0) {
        return ShotType::Cutaway;
    }
    if (key.compare(QLatin1String("detail"), Qt::CaseInsensitive) == 0) {
        return ShotType::Detail;
    }
    if (key.compare(QLatin1String("scene"), Qt::CaseInsensitive) == 0) {
        return ShotType::Scene;
    }
    if (key.compare(QLatin1String("product"), Qt::CaseInsensitive) == 0) {
        return ShotType::Product;
    }
    if (key.compare(QLatin1String("person"), Qt::CaseInsensitive) == 0) {
        return ShotType::Person;
    }
    return ShotType::Other;
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

QString subtitlePresetKey(SubtitlePreset preset)
{
    switch (preset) {
    case SubtitlePreset::Keyword:
        return QStringLiteral("keyword");
    case SubtitlePreset::Big:
        return QStringLiteral("big");
    case SubtitlePreset::Minimal:
        break;
    }
    return QStringLiteral("minimal");
}

SubtitlePreset subtitlePresetFromKey(const QString &key)
{
    if (key.compare(QLatin1String("keyword"), Qt::CaseInsensitive) == 0) {
        return SubtitlePreset::Keyword;
    }
    if (key.compare(QLatin1String("big"), Qt::CaseInsensitive) == 0) {
        return SubtitlePreset::Big;
    }
    return SubtitlePreset::Minimal;
}

Density densityFromStudio(StudioData::OralDensity density)
{
    switch (density) {
    case StudioData::OralDensity::Low:
        return Density::Low;
    case StudioData::OralDensity::High:
        return Density::High;
    case StudioData::OralDensity::Medium:
        break;
    }
    return Density::Medium;
}

QString densityKey(Density density)
{
    switch (density) {
    case Density::Low:
        return QStringLiteral("low");
    case Density::High:
        return QStringLiteral("high");
    case Density::Medium:
        break;
    }
    return QStringLiteral("medium");
}

Density densityFromKey(const QString &key)
{
    if (key.compare(QLatin1String("low"), Qt::CaseInsensitive) == 0) {
        return Density::Low;
    }
    if (key.compare(QLatin1String("high"), Qt::CaseInsensitive) == 0) {
        return Density::High;
    }
    return Density::Medium;
}

QString audioKindKey(AudioKind kind)
{
    return kind == AudioKind::Bgm ? QStringLiteral("bgm") : QStringLiteral("sfx");
}

/* ------------------------------------------------------------------ */
/* 视觉特征                                                             */
/* ------------------------------------------------------------------ */

QJsonObject VisualFeatures::toJson() const
{
    QJsonObject object;
    object.insert(QStringLiteral("dominantColor1"), dominantColor1);
    object.insert(QStringLiteral("dominantColor2"), dominantColor2);
    object.insert(QStringLiteral("dominantColor3"), dominantColor3);
    object.insert(QStringLiteral("brightness"), brightness);
    object.insert(QStringLiteral("saturation"), saturation);
    object.insert(QStringLiteral("contrast"), contrast);
    object.insert(QStringLiteral("detail"), detail);
    object.insert(QStringLiteral("motion"), motion);
    object.insert(QStringLiteral("keyframeCount"), keyframeCount);
    return object;
}

VisualFeatures VisualFeatures::fromJson(const QJsonObject &object)
{
    VisualFeatures features;
    features.dominantColor1 = readString(object, QStringLiteral("dominantColor1"));
    features.dominantColor2 = readString(object, QStringLiteral("dominantColor2"));
    features.dominantColor3 = readString(object, QStringLiteral("dominantColor3"));
    features.brightness = object.value(QStringLiteral("brightness")).toDouble();
    features.saturation = object.value(QStringLiteral("saturation")).toDouble();
    features.contrast = object.value(QStringLiteral("contrast")).toDouble();
    features.detail = object.value(QStringLiteral("detail")).toDouble();
    features.motion = object.value(QStringLiteral("motion")).toDouble();
    features.keyframeCount =
        object.value(QStringLiteral("keyframeCount")).toInt();
    return features;
}

/* ------------------------------------------------------------------ */
/* 素材                                                                 */
/* ------------------------------------------------------------------ */

QJsonObject MaterialProfile::toJson() const
{
    QJsonObject object;
    object.insert(QStringLiteral("id"), id);
    object.insert(QStringLiteral("name"), name);
    object.insert(QStringLiteral("path"), path);
    object.insert(QStringLiteral("kind"), materialKindKey(kind));
    object.insert(QStringLiteral("width"), width);
    object.insert(QStringLiteral("height"), height);
    object.insert(QStringLiteral("durationMs"), durationMs);
    object.insert(QStringLiteral("description"), description);
    object.insert(QStringLiteral("role"), role);
    object.insert(QStringLiteral("tags"), toArray(tags));
    object.insert(QStringLiteral("bestFor"), toArray(bestFor));
    object.insert(QStringLiteral("quality"), quality);
    object.insert(QStringLiteral("visual"), visual.toJson());
    object.insert(QStringLiteral("keyframePath"), keyframePath);
    object.insert(QStringLiteral("sourceInMs"), sourceInMs);
    return object;
}

MaterialProfile MaterialProfile::fromJson(const QJsonObject &object)
{
    MaterialProfile profile;
    profile.id = readString(object, QStringLiteral("id"));
    profile.name = readString(object, QStringLiteral("name"));
    profile.path = readString(object, QStringLiteral("path"));
    profile.kind = materialKindFromKey(
        readString(object, QStringLiteral("kind"), QStringLiteral("image")));
    profile.width = object.value(QStringLiteral("width")).toInt();
    profile.height = object.value(QStringLiteral("height")).toInt();
    profile.durationMs = readMs(object, QStringLiteral("durationMs"));
    profile.description = readString(object, QStringLiteral("description"));
    profile.role = readString(object, QStringLiteral("role"),
                              QStringLiteral("other"));
    profile.tags = toStringList(object.value(QStringLiteral("tags")).toArray());
    profile.bestFor = toStringList(object.value(QStringLiteral("bestFor")).toArray());
    profile.quality = object.value(QStringLiteral("quality")).toInt();
    profile.visual =
        VisualFeatures::fromJson(object.value(QStringLiteral("visual")).toObject());
    profile.keyframePath = readString(object, QStringLiteral("keyframePath"));
    profile.sourceInMs = readString(object, QStringLiteral("sourceInMs"));
    return profile;
}

/* ------------------------------------------------------------------ */
/* 语音                                                                 */
/* ------------------------------------------------------------------ */

QJsonObject Utterance::toJson() const
{
    QJsonObject object;
    object.insert(QStringLiteral("text"), text);
    object.insert(QStringLiteral("speaker"), speaker);
    object.insert(QStringLiteral("startMs"), startMs);
    object.insert(QStringLiteral("endMs"), endMs);
    object.insert(QStringLiteral("confidence"), confidence);
    QJsonArray wordArray;
    for (const Word &word : words) {
        QJsonObject wordObject;
        wordObject.insert(QStringLiteral("text"), word.text);
        wordObject.insert(QStringLiteral("startMs"), word.startMs);
        wordObject.insert(QStringLiteral("endMs"), word.endMs);
        wordArray.append(wordObject);
    }
    object.insert(QStringLiteral("words"), wordArray);
    return object;
}

Utterance Utterance::fromJson(const QJsonObject &object)
{
    Utterance utterance;
    utterance.text = readString(object, QStringLiteral("text"));
    utterance.speaker = readString(object, QStringLiteral("speaker"));
    utterance.startMs = readMs(object, QStringLiteral("startMs"));
    utterance.endMs = readMs(object, QStringLiteral("endMs"));
    utterance.confidence =
        object.value(QStringLiteral("confidence")).toDouble(-1.0);
    const QJsonArray words = object.value(QStringLiteral("words")).toArray();
    for (const QJsonValue &value : words) {
        const QJsonObject wordObject = value.toObject();
        Word word;
        word.text = readString(wordObject, QStringLiteral("text"));
        word.startMs = readMs(wordObject, QStringLiteral("startMs"));
        word.endMs = readMs(wordObject, QStringLiteral("endMs"));
        if (!word.text.isEmpty()) {
            utterance.words.append(word);
        }
    }
    return utterance;
}

QJsonObject SpeechAnalysis::toJson() const
{
    QJsonObject object;
    object.insert(QStringLiteral("text"), text);
    object.insert(QStringLiteral("language"), language);
    object.insert(QStringLiteral("durationMs"), durationMs);
    object.insert(QStringLiteral("charCount"), charCount);
    object.insert(QStringLiteral("speakers"), toArray(speakers));
    QJsonArray utteranceArray;
    for (const Utterance &utterance : utterances) {
        utteranceArray.append(utterance.toJson());
    }
    object.insert(QStringLiteral("utterances"), utteranceArray);
    return object;
}

SpeechAnalysis SpeechAnalysis::fromJson(const QJsonObject &object)
{
    SpeechAnalysis analysis;
    analysis.text = readString(object, QStringLiteral("text"));
    analysis.language = readString(object, QStringLiteral("language"),
                                   QStringLiteral("zh"));
    analysis.durationMs = readMs(object, QStringLiteral("durationMs"));
    analysis.charCount = object.value(QStringLiteral("charCount")).toInt();
    analysis.speakers =
        toStringList(object.value(QStringLiteral("speakers")).toArray());
    const QJsonArray utterances =
        object.value(QStringLiteral("utterances")).toArray();
    for (const QJsonValue &value : utterances) {
        analysis.utterances.append(Utterance::fromJson(value.toObject()));
    }
    return analysis;
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
    return object;
}

SemanticSegment SemanticSegment::fromJson(const QJsonObject &object)
{
    SemanticSegment segment;
    segment.startMs = readMs(object, QStringLiteral("startMs"));
    segment.endMs = readMs(object, QStringLiteral("endMs"));
    segment.title = readString(object, QStringLiteral("title"));
    segment.summary = readString(object, QStringLiteral("summary"));
    segment.type = segmentTypeFromKey(
        readString(object, QStringLiteral("type"), QStringLiteral("other")));
    segment.keywords =
        toStringList(object.value(QStringLiteral("keywords")).toArray());
    segment.visualHint = readString(object, QStringLiteral("visualHint"));
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
    Emphasis emphasis;
    emphasis.startMs = readMs(object, QStringLiteral("startMs"));
    emphasis.endMs = readMs(object, QStringLiteral("endMs"));
    emphasis.text = readString(object, QStringLiteral("text"));
    emphasis.words = toStringList(object.value(QStringLiteral("words")).toArray());
    return emphasis;
}

QJsonObject SemanticAnalysis::toJson() const
{
    QJsonObject object;
    object.insert(QStringLiteral("title"), title);
    object.insert(QStringLiteral("description"), description);
    object.insert(QStringLiteral("tags"), toArray(tags));
    object.insert(QStringLiteral("topics"), toArray(topics));
    object.insert(QStringLiteral("keywords"), toArray(keywords));
    QJsonArray segments;
    for (const SemanticSegment &segment : this->segments) {
        segments.append(segment.toJson());
    }
    object.insert(QStringLiteral("segments"), segments);
    QJsonArray emphasisArray;
    for (const Emphasis &item : emphasis) {
        emphasisArray.append(item.toJson());
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
    analysis.topics = toStringList(object.value(QStringLiteral("topics")).toArray());
    analysis.keywords =
        toStringList(object.value(QStringLiteral("keywords")).toArray());
    const QJsonArray segments = object.value(QStringLiteral("segments")).toArray();
    for (const QJsonValue &value : segments) {
        analysis.segments.append(SemanticSegment::fromJson(value.toObject()));
    }
    const QJsonArray emphasis = object.value(QStringLiteral("emphasis")).toArray();
    for (const QJsonValue &value : emphasis) {
        analysis.emphasis.append(Emphasis::fromJson(value.toObject()));
    }
    return analysis;
}

/* ------------------------------------------------------------------ */
/* 镜头                                                                 */
/* ------------------------------------------------------------------ */

QJsonObject Shot::toJson() const
{
    QJsonObject object;
    object.insert(QStringLiteral("startMs"), startMs);
    object.insert(QStringLiteral("endMs"), endMs);
    object.insert(QStringLiteral("materialId"), materialId);
    object.insert(QStringLiteral("materialKind"), materialKindKey(materialKind));
    object.insert(QStringLiteral("shotType"), shotTypeKey(shotType));
    object.insert(QStringLiteral("transition"), transition);
    object.insert(QStringLiteral("reason"), reason);
    object.insert(QStringLiteral("sourceInMs"), sourceInMs);
    object.insert(QStringLiteral("preparedPath"), preparedPath);
    object.insert(QStringLiteral("uploadedUrl"), uploadedUrl);
    return object;
}

Shot Shot::fromJson(const QJsonObject &object)
{
    Shot shot;
    shot.startMs = readMs(object, QStringLiteral("startMs"));
    shot.endMs = readMs(object, QStringLiteral("endMs"));
    shot.materialId = readString(object, QStringLiteral("materialId"));
    shot.materialKind = materialKindFromKey(
        readString(object, QStringLiteral("materialKind"), QStringLiteral("image")));
    shot.shotType = shotTypeFromKey(
        readString(object, QStringLiteral("shotType"), QStringLiteral("cutaway")));
    shot.transition = readString(object, QStringLiteral("transition"));
    shot.reason = readString(object, QStringLiteral("reason"));
    shot.sourceInMs = readMs(object, QStringLiteral("sourceInMs"));
    shot.preparedPath = readString(object, QStringLiteral("preparedPath"));
    shot.uploadedUrl = readString(object, QStringLiteral("uploadedUrl"));
    return shot;
}

QJsonObject ShotPlan::toJson() const
{
    QJsonObject object;
    object.insert(QStringLiteral("density"), densityKey(density));
    object.insert(QStringLiteral("targetCount"), targetCount);
    QJsonArray shotArray;
    for (const Shot &shot : shots) {
        shotArray.append(shot.toJson());
    }
    object.insert(QStringLiteral("shots"), shotArray);
    return object;
}

ShotPlan ShotPlan::fromJson(const QJsonObject &object)
{
    ShotPlan plan;
    plan.density = densityFromKey(
        readString(object, QStringLiteral("density"), QStringLiteral("medium")));
    plan.targetCount = object.value(QStringLiteral("targetCount")).toInt();
    const QJsonArray shots = object.value(QStringLiteral("shots")).toArray();
    for (const QJsonValue &value : shots) {
        plan.shots.append(Shot::fromJson(value.toObject()));
    }
    return plan;
}

/* ------------------------------------------------------------------ */
/* 字幕                                                                 */
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
    cue.keywords = toStringList(object.value(QStringLiteral("keywords")).toArray());
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
    QJsonArray cues;
    for (const SubtitleCue &cue : this->cues) {
        cues.append(cue.toJson());
    }
    object.insert(QStringLiteral("cues"), cues);
    return object;
}

SubtitlePlan SubtitlePlan::fromJson(const QJsonObject &object)
{
    SubtitlePlan plan;
    plan.preset = subtitlePresetFromKey(
        readString(object, QStringLiteral("preset"), QStringLiteral("keyword")));
    plan.titleFx = object.value(QStringLiteral("titleFx")).toBool(true);
    plan.titleText = readString(object, QStringLiteral("titleText"));
    plan.titleStartMs = readMs(object, QStringLiteral("titleStartMs"));
    plan.titleEndMs = readMs(object, QStringLiteral("titleEndMs"), 2500);
    const QJsonArray cues = object.value(QStringLiteral("cues")).toArray();
    for (const QJsonValue &value : cues) {
        plan.cues.append(SubtitleCue::fromJson(value.toObject()));
    }
    return plan;
}

/* ------------------------------------------------------------------ */
/* 音频                                                                 */
/* ------------------------------------------------------------------ */

QJsonObject AudioCue::toJson() const
{
    QJsonObject object;
    object.insert(QStringLiteral("kind"), audioKindKey(kind));
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
    const QJsonArray sfx = object.value(QStringLiteral("sfx")).toArray();
    for (const QJsonValue &value : sfx) {
        plan.sfx.append(AudioCue::fromJson(value.toObject()));
    }
    return plan;
}

/* ------------------------------------------------------------------ */
/* 画布与剪辑计划                                                       */
/* ------------------------------------------------------------------ */

QJsonObject CoverPlan::toJson() const
{
    QJsonObject object;
    object.insert(QStringLiteral("prompt"), prompt);
    object.insert(QStringLiteral("localPath"), localPath);
    object.insert(QStringLiteral("uploadedUrl"), uploadedUrl);
    object.insert(QStringLiteral("startMs"), startMs);
    object.insert(QStringLiteral("endMs"), endMs);
    return object;
}

CoverPlan CoverPlan::fromJson(const QJsonObject &object)
{
    CoverPlan cover;
    cover.prompt = readString(object, QStringLiteral("prompt"));
    cover.localPath = readString(object, QStringLiteral("localPath"));
    cover.uploadedUrl = readString(object, QStringLiteral("uploadedUrl"));
    cover.startMs = readMs(object, QStringLiteral("startMs"));
    cover.endMs = readMs(object, QStringLiteral("endMs"), 10);
    return cover;
}

QJsonObject Canvas::toJson() const
{
    QJsonObject object;
    object.insert(QStringLiteral("width"), width);
    object.insert(QStringLiteral("height"), height);
    object.insert(QStringLiteral("ratio"), ratio);
    object.insert(QStringLiteral("fps"), fps);
    return object;
}

Canvas Canvas::fromJson(const QJsonObject &object)
{
    Canvas canvas;
    canvas.width = object.value(QStringLiteral("width")).toInt(1080);
    canvas.height = object.value(QStringLiteral("height")).toInt(1920);
    canvas.ratio = readString(object, QStringLiteral("ratio"),
                              QStringLiteral("9:16"));
    canvas.fps = object.value(QStringLiteral("fps")).toInt(30);
    return canvas;
}

QJsonObject EditPlan::toJson() const
{
    QJsonObject root;
    root.insert(QStringLiteral("version"), version);
    root.insert(QStringLiteral("kind"), QStringLiteral("livemix.oral.edit_plan"));
    root.insert(QStringLiteral("templateId"), templateId);
    root.insert(QStringLiteral("createdAt"), createdAt);
    root.insert(QStringLiteral("canvas"), canvas.toJson());
    root.insert(QStringLiteral("durationMs"), durationMs);
    root.insert(QStringLiteral("outputBoth"), outputBoth);

    QJsonObject source;
    source.insert(QStringLiteral("path"), sourcePath);
    source.insert(QStringLiteral("name"), sourceName);
    source.insert(QStringLiteral("width"), sourceWidth);
    source.insert(QStringLiteral("height"), sourceHeight);
    source.insert(QStringLiteral("sizeBytes"), sourceSizeBytes);
    root.insert(QStringLiteral("source"), source);

    // 分析结果完整保留，方便重新包装时复用，也方便排查「为什么这样剪」。
    root.insert(QStringLiteral("speech"), speech.toJson());
    root.insert(QStringLiteral("semantics"), semantics.toJson());

    QJsonArray materialArray;
    for (const MaterialProfile &material : materials) {
        materialArray.append(material.toJson());
    }
    root.insert(QStringLiteral("materials"), materialArray);

    root.insert(QStringLiteral("shotPlan"), shots.toJson());
    root.insert(QStringLiteral("subtitles"), subtitles.toJson());
    root.insert(QStringLiteral("audio"), audio.toJson());
    root.insert(QStringLiteral("cover"), cover.toJson());
    root.insert(QStringLiteral("timeline"), timelineJson());
    return root;
}

QJsonObject EditPlan::timelineJson() const
{
    // Timeline 只描述「哪条轨、什么素材、从何时到何时」，
    // 不重复携带大段分析文本，便于剪映或其它工具直接消费。
    QJsonObject timeline;
    QJsonArray videoTracks;
    QJsonArray audioTracks;
    QJsonArray textTracks;

    // V1：口播原片，完整覆盖 0–duration。
    QJsonObject sourceTrack;
    sourceTrack.insert(QStringLiteral("track"), QStringLiteral("V1"));
    sourceTrack.insert(QStringLiteral("type"), QStringLiteral("source"));
    sourceTrack.insert(QStringLiteral("startMs"), qint64(0));
    sourceTrack.insert(QStringLiteral("endMs"), durationMs);
    sourceTrack.insert(QStringLiteral("path"), sourcePath);
    videoTracks.append(sourceTrack);

    // V2：B-Roll 视频；V3：B-Roll 图片。两者按原时间轴绝对时间摆放。
    for (const Shot &shot : shots.shots) {
        QJsonObject item;
        item.insert(QStringLiteral("track"),
                    shot.materialKind == MaterialKind::Video
                        ? QStringLiteral("V2")
                        : QStringLiteral("V3"));
        item.insert(QStringLiteral("type"), materialKindKey(shot.materialKind));
        item.insert(QStringLiteral("startMs"), shot.startMs);
        item.insert(QStringLiteral("endMs"), shot.endMs);
        item.insert(QStringLiteral("materialId"), shot.materialId);
        item.insert(QStringLiteral("sourceInMs"), shot.sourceInMs);
        item.insert(QStringLiteral("shotType"), shotTypeKey(shot.shotType));
        item.insert(QStringLiteral("transition"), shot.transition);
        videoTracks.append(item);
    }

    // V4：封面海报，压在最上层，只覆盖片头 [0, 10000] 微秒（10 毫秒）。
    if (!cover.localPath.isEmpty() || !cover.uploadedUrl.isEmpty()) {
        QJsonObject item;
        item.insert(QStringLiteral("track"), QStringLiteral("V4"));
        item.insert(QStringLiteral("type"), QStringLiteral("cover"));
        item.insert(QStringLiteral("startMs"), cover.startMs);
        item.insert(QStringLiteral("endMs"), cover.endMs);
        item.insert(QStringLiteral("path"),
                    cover.uploadedUrl.isEmpty() ? cover.localPath
                                                : cover.uploadedUrl);
        videoTracks.append(item);
    }

    // A1：BGM；A2：音效。
    if (audio.bgmEnabled && !audio.bgm.url.isEmpty()) {
        QJsonObject bgm;
        bgm.insert(QStringLiteral("track"), QStringLiteral("A1"));
        bgm.insert(QStringLiteral("type"), QStringLiteral("bgm"));
        bgm.insert(QStringLiteral("startMs"), audio.bgm.startMs);
        bgm.insert(QStringLiteral("endMs"), audio.bgm.endMs);
        bgm.insert(QStringLiteral("name"), audio.bgm.name);
        bgm.insert(QStringLiteral("volume"), audio.bgm.volume);
        audioTracks.append(bgm);
    }
    if (audio.sfxEnabled) {
        for (const AudioCue &cue : audio.sfx) {
            QJsonObject item;
            item.insert(QStringLiteral("track"), QStringLiteral("A2"));
            item.insert(QStringLiteral("type"), QStringLiteral("sfx"));
            item.insert(QStringLiteral("startMs"), cue.startMs);
            item.insert(QStringLiteral("endMs"), cue.endMs);
            item.insert(QStringLiteral("name"), cue.name);
            item.insert(QStringLiteral("volume"), cue.volume);
            audioTracks.append(item);
        }
    }

    // T1：标题花字；T2：正文关键词字幕。
    if (subtitles.titleFx && !subtitles.titleText.isEmpty()) {
        QJsonObject title;
        title.insert(QStringLiteral("track"), QStringLiteral("T1"));
        title.insert(QStringLiteral("type"), QStringLiteral("title"));
        title.insert(QStringLiteral("startMs"), subtitles.titleStartMs);
        title.insert(QStringLiteral("endMs"), subtitles.titleEndMs);
        title.insert(QStringLiteral("text"), subtitles.titleText);
        textTracks.append(title);
    }
    for (const SubtitleCue &cue : subtitles.cues) {
        QJsonObject item;
        item.insert(QStringLiteral("track"), QStringLiteral("T2"));
        item.insert(QStringLiteral("type"), QStringLiteral("caption"));
        item.insert(QStringLiteral("startMs"), cue.startMs);
        item.insert(QStringLiteral("endMs"), cue.endMs);
        item.insert(QStringLiteral("text"), cue.text);
        item.insert(QStringLiteral("keywords"), toArray(cue.keywords));
        textTracks.append(item);
    }

    timeline.insert(QStringLiteral("video"), videoTracks);
    timeline.insert(QStringLiteral("audio"), audioTracks);
    timeline.insert(QStringLiteral("text"), textTracks);
    return timeline;
}

EditPlan EditPlan::fromJson(const QJsonObject &object)
{
    EditPlan plan;
    plan.version = object.value(QStringLiteral("version")).toInt(1);
    plan.templateId = readString(object, QStringLiteral("templateId"),
                                 QStringLiteral("clean"));
    plan.createdAt = readString(object, QStringLiteral("createdAt"));
    plan.canvas = Canvas::fromJson(object.value(QStringLiteral("canvas")).toObject());
    plan.durationMs = readMs(object, QStringLiteral("durationMs"));
    plan.outputBoth = object.value(QStringLiteral("outputBoth")).toBool();
    const QJsonObject source = object.value(QStringLiteral("source")).toObject();
    plan.sourcePath = readString(source, QStringLiteral("path"));
    plan.sourceName = readString(source, QStringLiteral("name"));
    plan.sourceWidth = source.value(QStringLiteral("width")).toInt();
    plan.sourceHeight = source.value(QStringLiteral("height")).toInt();
    plan.sourceSizeBytes = readMs(source, QStringLiteral("sizeBytes"));
    plan.speech = SpeechAnalysis::fromJson(
        object.value(QStringLiteral("speech")).toObject());
    plan.semantics = SemanticAnalysis::fromJson(
        object.value(QStringLiteral("semantics")).toObject());
    const QJsonArray materials =
        object.value(QStringLiteral("materials")).toArray();
    for (const QJsonValue &value : materials) {
        plan.materials.append(MaterialProfile::fromJson(value.toObject()));
    }
    plan.shots = ShotPlan::fromJson(
        object.value(QStringLiteral("shotPlan")).toObject());
    plan.subtitles = SubtitlePlan::fromJson(
        object.value(QStringLiteral("subtitles")).toObject());
    plan.audio = AudioPlan::fromJson(object.value(QStringLiteral("audio")).toObject());
    plan.cover = CoverPlan::fromJson(
        object.value(QStringLiteral("cover")).toObject());
    return plan;
}

/* ------------------------------------------------------------------ */
/* 结果历史                                                             */
/* ------------------------------------------------------------------ */

namespace {

// 素材缩略图（QImage）不写入历史：体积大且可从素材文件重新生成。
// 这里只保存重新包装所需的路径与展示元数据。
QJsonObject oralMaterialToJson(const StudioData::OralMaterial &material)
{
    QJsonObject object;
    object.insert(QStringLiteral("id"), material.id);
    object.insert(QStringLiteral("name"), material.name);
    object.insert(QStringLiteral("tag"), material.tag);
    object.insert(QStringLiteral("kind"), material.kind);
    object.insert(QStringLiteral("duration"), material.duration);
    object.insert(QStringLiteral("visual"), material.visual);
    object.insert(QStringLiteral("filePath"), material.filePath);
    object.insert(QStringLiteral("paletteFrom"),
                  material.paletteFrom.name(QColor::HexArgb));
    object.insert(QStringLiteral("paletteTo"),
                  material.paletteTo.name(QColor::HexArgb));
    return object;
}

StudioData::OralMaterial oralMaterialFromJson(const QJsonObject &object)
{
    StudioData::OralMaterial material;
    material.id = readString(object, QStringLiteral("id"));
    material.name = readString(object, QStringLiteral("name"));
    material.tag = readString(object, QStringLiteral("tag"));
    material.kind = readString(object, QStringLiteral("kind"),
                               QStringLiteral("图片"));
    material.duration = readString(object, QStringLiteral("duration"));
    material.visual = readString(object, QStringLiteral("visual"));
    material.filePath = readString(object, QStringLiteral("filePath"));
    const QColor paletteFrom(readString(object, QStringLiteral("paletteFrom")));
    if (paletteFrom.isValid()) {
        material.paletteFrom = paletteFrom;
    }
    const QColor paletteTo(readString(object, QStringLiteral("paletteTo")));
    if (paletteTo.isValid()) {
        material.paletteTo = paletteTo;
    }
    return material;
}

} // namespace

QJsonObject oralResultToJson(const StudioData::OralResult &result)
{
    QJsonObject object;
    object.insert(QStringLiteral("id"), result.id);
    object.insert(QStringLiteral("projectName"), result.projectName);
    object.insert(QStringLiteral("title"), result.title);
    object.insert(QStringLiteral("description"), result.description);
    object.insert(QStringLiteral("tags"), toArray(result.tags));
    object.insert(QStringLiteral("templateId"), result.templateId);
    object.insert(QStringLiteral("ratio"), result.ratio);
    object.insert(QStringLiteral("durationLabel"), result.durationLabel);
    object.insert(QStringLiteral("fileSize"), result.fileSize);
    object.insert(QStringLiteral("outputBoth"), result.outputBoth);
    object.insert(QStringLiteral("generatedAt"), result.generatedAt);
    object.insert(QStringLiteral("sourceLabel"), result.sourceLabel);
    object.insert(QStringLiteral("script"), result.script);
    object.insert(QStringLiteral("subtitleCount"), result.subtitleCount);
    object.insert(QStringLiteral("brollCount"), result.brollCount);
    object.insert(QStringLiteral("draftGenerated"), result.draftGenerated);
    object.insert(QStringLiteral("filePath"), result.filePath);
    object.insert(QStringLiteral("previewPath"), result.previewPath);
    object.insert(QStringLiteral("draftPath"), result.draftPath);
    object.insert(QStringLiteral("draftId"), result.draftId);
    object.insert(QStringLiteral("editPlanPath"), result.editPlanPath);
    object.insert(QStringLiteral("videoCodec"), result.videoCodec);
    object.insert(QStringLiteral("sourcePath"), result.sourcePath);
    QJsonArray materials;
    for (const StudioData::OralMaterial &material : result.materials) {
        // 示例素材没有真实路径，无法用于重新包装，不写入历史。
        if (material.filePath.trimmed().isEmpty()) {
            continue;
        }
        materials.append(oralMaterialToJson(material));
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

StudioData::OralResult oralResultFromJson(const QJsonObject &object)
{
    StudioData::OralResult result;
    result.id = readString(object, QStringLiteral("id"));
    result.projectName = readString(object, QStringLiteral("projectName"));
    result.title = readString(object, QStringLiteral("title"));
    result.description = readString(object, QStringLiteral("description"));
    result.tags = toStringList(object.value(QStringLiteral("tags")).toArray());
    result.templateId = readString(object, QStringLiteral("templateId"),
                                   QStringLiteral("clean"));
    result.ratio = readString(object, QStringLiteral("ratio"),
                              QStringLiteral("9:16"));
    result.durationLabel =
        readString(object, QStringLiteral("durationLabel"), QStringLiteral("00:00"));
    result.fileSize = readString(object, QStringLiteral("fileSize"));
    result.outputBoth = object.value(QStringLiteral("outputBoth")).toBool();
    result.generatedAt =
        static_cast<qint64>(object.value(QStringLiteral("generatedAt")).toDouble());
    result.sourceLabel = readString(object, QStringLiteral("sourceLabel"));
    result.script = readString(object, QStringLiteral("script"));
    result.subtitleCount =
        object.value(QStringLiteral("subtitleCount")).toInt();
    result.brollCount = object.value(QStringLiteral("brollCount")).toInt();
    result.draftGenerated =
        object.value(QStringLiteral("draftGenerated")).toBool();
    result.filePath = readString(object, QStringLiteral("filePath"));
    result.previewPath = readString(object, QStringLiteral("previewPath"));
    result.draftPath = readString(object, QStringLiteral("draftPath"));
    result.draftId = readString(object, QStringLiteral("draftId"));
    result.editPlanPath = readString(object, QStringLiteral("editPlanPath"));
    result.videoCodec = readString(object, QStringLiteral("videoCodec"));
    result.sourcePath = readString(object, QStringLiteral("sourcePath"));
    for (const QJsonValue &value :
         object.value(QStringLiteral("materials")).toArray()) {
        StudioData::OralMaterial material =
            oralMaterialFromJson(value.toObject());
        if (!material.filePath.trimmed().isEmpty()) {
            result.materials.append(material);
        }
    }
    const QJsonArray highlights =
        object.value(QStringLiteral("highlights")).toArray();
    for (const QJsonValue &value : highlights) {
        const QJsonArray pair = value.toArray();
        if (pair.size() >= 2) {
            result.highlights.append(
                {pair.at(0).toString(), pair.at(1).toString()});
        }
    }
    return result;
}

} // namespace Oral
