#pragma once

// 文件作用：智能混剪流水线的领域数据模型。
//
// 流水线严格对应产品的六步：
// 1. 素材整理：视频切片、图片分析、场景识别、内容标签、质量评分；
// 2. AI 语音理解：TTS 配音 → ASR → 语义分段、关键词、情绪、叙事结构；
// 3. AI 镜头规划：语音片段与素材匹配、镜头顺序、时长、画面布局；
// 4. 智能包装：字幕、动画、转场、背景音乐、音效、画面特效；
// 5. 时间轴编排：固化成统一的 Edit Plan / Timeline JSON；
// 6. 调用 capcut-mate 合成剪映草稿并云渲染成片。
//
// 时间约定：
// - 领域结构统一使用毫秒（qint64），与 ASR、ffmpeg 保持一致；
// - 提交给 capcut-mate 时再统一换算成微秒（1 秒 = 1,000,000 微秒）；
// - 时间轴 JSON 同样使用毫秒，方便阅读与二次编辑。
//
// 语音识别结果复用智能口播已经验证过的 SpeechAnalysis / Utterance / Word
// 模型（见 oral/OralTypes.h），避免同一份 ASR 解析逻辑维护两遍。

#include "../data/StudioData.h"
#include "../oral/OralTypes.h"

#include <QJsonObject>
#include <QString>
#include <QStringList>
#include <QVector>

namespace Mixcut {

using SpeechAnalysis = Oral::SpeechAnalysis;
using Utterance = Oral::Utterance;
using Word = Oral::Word;
using VisualFeatures = Oral::VisualFeatures;
using Canvas = Oral::Canvas;

/* ------------------------------------------------------------------ */
/* 枚举                                                                 */
/* ------------------------------------------------------------------ */

enum class MaterialKind { Image, Video };
enum class MaterialRole { Product, Scene, Detail, Person, Text, Other };
enum class SegmentType { Hook, Pain, Point, Example, Evidence, Conclusion, Cta, Other };
enum class Emotion { Neutral, Happy, Excited, Calm, Serious, Warm, Surprised };
enum class NarrativeRole { Hook, Setup, Conflict, Turn, Payoff, Cta, Other };
enum class Layout { Fullscreen, Pip };
enum class PipPosition { TopLeft, TopRight, BottomLeft, BottomRight };
enum class SubtitlePreset { Keyword, Big, Minimal };

QString materialKindKey(MaterialKind kind);
MaterialKind materialKindFromKey(const QString &key);
QString materialRoleKey(MaterialRole role);
MaterialRole materialRoleFromKey(const QString &key);
QString segmentTypeKey(SegmentType type);
SegmentType segmentTypeFromKey(const QString &key);
QString emotionKey(Emotion emotion);
Emotion emotionFromKey(const QString &key);
QString narrativeRoleKey(NarrativeRole role);
NarrativeRole narrativeRoleFromKey(const QString &key);
QString pipPositionKey(PipPosition position);
PipPosition pipPositionFromKey(const QString &key);
QString subtitlePresetKey(SubtitlePreset preset);
SubtitlePreset subtitlePresetFromKey(const QString &key);
// 模板风格 → 字幕预设：种草=关键词高亮，测评=大字强调，故事=极简白字。
SubtitlePreset subtitlePresetForStyle(StudioData::MixcutStyle style);

/* ------------------------------------------------------------------ */
/* 素材整理                                                             */
/* ------------------------------------------------------------------ */

// 一个可剪辑的画面单元：
// - 图片素材固定一个 clip；
// - 视频素材按场景切换点切成多个 clip，每个 clip 有自己的素材内区间。
// 镜头规划只引用 clipId，不直接引用素材文件。
struct MaterialClip
{
    QString id;          // 全局唯一，例如 "mat-1-c3"
    QString assetId;     // 所属素材
    QString assetName;   // 素材文件名（展示用）
    QString path;        // 本地文件路径
    MaterialKind kind = MaterialKind::Video;
    qint64 sourceInMs = 0;  // 素材内起点
    qint64 sourceOutMs = 0; // 素材内终点
    qint64 durationMs = 0;  // sourceOut - sourceIn
    int width = 0;
    int height = 0;
    // 大模型理解结果。
    QString description;
    MaterialRole role = MaterialRole::Other;
    QStringList tags;
    QStringList bestFor;
    int quality = 60; // 0–100，清晰、主体明确给高分
    Emotion emotion = Emotion::Neutral;
    // 本地关键帧与视觉特征，供大模型不可用时兜底。
    QString keyframePath;
    VisualFeatures visual;

    bool isVideo() const { return kind == MaterialKind::Video; }
    qint64 availableMs() const
    {
        return qMax<qint64>(0, sourceOutMs - sourceInMs);
    }
    QJsonObject toJson() const;
    static MaterialClip fromJson(const QJsonObject &object);
};

// 一个用户上传的素材文件及其切分后的镜头。
struct MaterialAsset
{
    QString id;
    QString name;
    QString path;
    MaterialKind kind = MaterialKind::Video;
    int width = 0;
    int height = 0;
    qint64 durationMs = 0;
    qint64 sizeBytes = 0;
    QVector<MaterialClip> clips;
    // 素材级标签：由 clip 标签汇总，供过程页展示。
    QStringList tags;
    QStringList scenes;

    QJsonObject toJson() const;
    static MaterialAsset fromJson(const QJsonObject &object);
};

// 全量素材库，镜头规划与合成阶段共用。
struct MaterialLibrary
{
    QVector<MaterialAsset> assets;

    QVector<MaterialClip> clips() const;
    const MaterialClip *clipById(const QString &id) const;
    MaterialClip *clipById(const QString &id);
    int clipCount() const { return clips().size(); }
    int videoCount() const;
    int imageCount() const;
};

/* ------------------------------------------------------------------ */
/* 语音理解                                                             */
/* ------------------------------------------------------------------ */

// 语义段落：包含内容类型、情绪与叙事角色。
struct SemanticSegment
{
    qint64 startMs = 0;
    qint64 endMs = 0;
    QString title;
    QString summary;
    SegmentType type = SegmentType::Other;
    QStringList keywords;
    QString visualHint; // 适合搭配什么画面
    Emotion emotion = Emotion::Neutral;
    NarrativeRole narrative = NarrativeRole::Other;

    qint64 durationMs() const { return qMax<qint64>(0, endMs - startMs); }
    QJsonObject toJson() const;
    static SemanticSegment fromJson(const QJsonObject &object);
};

struct Emphasis
{
    qint64 startMs = 0;
    qint64 endMs = 0;
    QString text;
    QStringList words;

    QJsonObject toJson() const;
    static Emphasis fromJson(const QJsonObject &object);
};

// 语义分析结果：内容分段、主题、关键词、叙事结构、情绪与发布文案。
struct SemanticAnalysis
{
    QString title;
    QString description;
    QStringList tags;
    QStringList topics;
    QStringList keywords;
    QString narrative; // 例如「钩子 → 痛点 → 方案 → 证据 → 行动」
    Emotion emotion = Emotion::Neutral;
    QVector<SemanticSegment> segments;
    QVector<Emphasis> emphasis;

    QJsonObject toJson() const;
    static SemanticAnalysis fromJson(const QJsonObject &object);
};

const SemanticSegment *segmentAt(const SemanticAnalysis &semantics,
                                 qint64 timeMs);

/* ------------------------------------------------------------------ */
/* 镜头规划                                                             */
/* ------------------------------------------------------------------ */

// 主时间轴上的一段画面；overlays 使用同一结构加画面位置。
struct Shot
{
    qint64 startMs = 0;
    qint64 endMs = 0;
    QString clipId;
    QString transition; // 叠化 / 闪白 / 推近，空表示硬切
    qint64 transitionMs = 400;
    qint64 sourceInMs = 0; // 视频 clip 从素材第几毫秒开始截取
    QString reason;
    // 画中画位置；主镜头保持 TopRight 默认值但忽略。
    PipPosition position = PipPosition::TopRight;

    qint64 durationMs() const { return qMax<qint64>(0, endMs - startMs); }
    QJsonObject toJson() const;
    static Shot fromJson(const QJsonObject &object);
};

/* ------------------------------------------------------------------ */
/* 智能包装                                                             */
/* ------------------------------------------------------------------ */

struct SubtitleCue
{
    qint64 startMs = 0;
    qint64 endMs = 0;
    QString text;
    QStringList keywords;
    bool emphasis = false;
    QString animation;

    qint64 durationMs() const { return qMax<qint64>(0, endMs - startMs); }
    QJsonObject toJson() const;
    static SubtitleCue fromJson(const QJsonObject &object);
};

struct SubtitlePlan
{
    SubtitlePreset preset = SubtitlePreset::Keyword;
    QVector<SubtitleCue> cues;
    bool titleFx = true;
    QString titleText;
    qint64 titleStartMs = 0;
    qint64 titleEndMs = 2600;

    QJsonObject toJson() const;
    static SubtitlePlan fromJson(const QJsonObject &object);
};

enum class AudioKind { Bgm, Sfx };

struct AudioCue
{
    AudioKind kind = AudioKind::Sfx;
    QString name;
    QString url;
    qint64 startMs = 0;
    qint64 endMs = 0;
    qint64 durationMs = 0;
    double volume = 0.1;
    bool loop = false;

    QJsonObject toJson() const;
    static AudioCue fromJson(const QJsonObject &object);
};

struct AudioPlan
{
    bool bgmEnabled = true;
    bool sfxEnabled = true;
    AudioCue bgm;
    QVector<AudioCue> sfx;

    QJsonObject toJson() const;
    static AudioPlan fromJson(const QJsonObject &object);
};

// 视频特效：只覆盖一段时间窗。
struct EffectCue
{
    QString title;
    qint64 startMs = 0;
    qint64 endMs = 0;

    qint64 durationMs() const { return qMax<qint64>(0, endMs - startMs); }
    QJsonObject toJson() const;
    static EffectCue fromJson(const QJsonObject &object);
};

// 滤镜：通常覆盖整条成片，intensity 0–100。
struct FilterCue
{
    QString title;
    qint64 startMs = 0;
    qint64 endMs = 0;
    double intensity = 35.0;

    QJsonObject toJson() const;
    static FilterCue fromJson(const QJsonObject &object);
};

/* ------------------------------------------------------------------ */
/* 统一剪辑计划                                                         */
/* ------------------------------------------------------------------ */

struct EditPlan
{
    int version = 1;
    QString createdAt;
    Canvas canvas;
    qint64 durationMs = 0;
    StudioData::MixcutStyle style = StudioData::MixcutStyle::Planting;
    QString script;
    QString scriptPrompt;
    QString voiceId;
    QString ttsPath; // 本地合成的 AI 配音

    SpeechAnalysis speech;
    SemanticAnalysis semantics;
    MaterialLibrary library;
    QVector<Shot> shots;    // 主画面：连续覆盖 [0, durationMs]
    QVector<Shot> overlays; // 画中画

    SubtitlePlan subtitles;
    AudioPlan audio;
    QVector<EffectCue> effects;
    QVector<FilterCue> filters;

    // 完整计划 JSON（分析、素材、镜头、字幕、音频、特效与渲染设置）。
    QJsonObject toJson() const;
    // 精简 Timeline JSON：只保留轨道与时间信息，便于其它工具消费。
    QJsonObject timelineJson() const;
};

/* ------------------------------------------------------------------ */
/* 结果历史持久化                                                       */
/* ------------------------------------------------------------------ */

QJsonObject mixcutResultToJson(const StudioData::MixcutResult &result);
StudioData::MixcutResult mixcutResultFromJson(const QJsonObject &object);

} // namespace Mixcut
