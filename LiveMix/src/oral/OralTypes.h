#pragma once

// 文件作用：智能口播流水线的领域数据模型。
// 这里定义从「素材/语音理解」到「统一剪辑计划 Edit Plan / Timeline JSON」的
// 全部中间结果。所有结构都与直播切片（ClipTypes / LiveClipService）完全独立，
// 只服务智能口播功能，避免两条业务线互相影响。
//
// 时间约定：
// - 领域结构内部统一使用毫秒（qint64），与 ASR 网关、ffmpeg 保持一致；
// - 提交给 capcut-mate 时再统一换算成微秒（1 秒 = 1,000,000 微秒）；
// - 剪辑计划 JSON 内的时间也使用毫秒，方便阅读和二次编辑。

#include "../data/StudioData.h"

#include <QJsonObject>
#include <QString>
#include <QStringList>
#include <QVector>

namespace Oral {

// 素材类型：图片或视频。
enum class MaterialKind { Image, Video };

// 素材在口播包装中的用途；由大模型结合画面与文件名判断。
enum class MaterialRole {
    Product, // 产品/物品特写
    Scene,   // 场景、环境
    Detail,  // 细节、质地
    Person,  // 人物、出镜
    Text,    // 文字、截图、图表
    Other,
};

// 镜头类型：决定 B-Roll 的取景与时长倾向。
enum class ShotType {
    Cutaway, // 切出画面
    Detail,  // 细节特写
    Scene,   // 场景补充
    Product, // 产品展示
    Person,  // 人物反应
    Other,
};

// 语义段落类型：开场钩子、痛点、观点、案例、结论、行动号召等。
enum class SegmentType {
    Hook,
    Pain,
    Point,
    Example,
    Evidence,
    Conclusion,
    Cta,
    Other,
};

// 字幕预设：与 UI 的「智能关键词 / 大字强调 / 极简白字」一一对应。
enum class SubtitlePreset { Keyword, Big, Minimal };

// B-Roll 密度：与 UI 的「少 / 适中 / 丰富」一一对应。
enum class Density { Low, Medium, High };

// 音轨类型。
enum class AudioKind { Bgm, Sfx };

QString materialKindKey(MaterialKind kind);
MaterialKind materialKindFromKey(const QString &key);
QString materialRoleKey(MaterialRole role);
MaterialRole materialRoleFromKey(const QString &key);
QString shotTypeKey(ShotType type);
ShotType shotTypeFromKey(const QString &key);
QString segmentTypeKey(SegmentType type);
SegmentType segmentTypeFromKey(const QString &key);
QString subtitlePresetKey(SubtitlePreset preset);
SubtitlePreset subtitlePresetFromKey(const QString &key);
Density densityFromStudio(StudioData::OralDensity density);
QString densityKey(Density density);
Density densityFromKey(const QString &key);
QString audioKindKey(AudioKind kind);

// 素材画面的本地视觉特征。即使大模型不支持图片输入，也能用这些特征
// 做基础理解（明暗、饱和度、细节密度、运动量、主色），并作为文本提示词。
struct VisualFeatures
{
    QString dominantColor1;
    QString dominantColor2;
    QString dominantColor3;
    double brightness = 0.0; // 0–1
    double saturation = 0.0; // 0–1
    double contrast = 0.0;   // 0–1
    double detail = 0.0;     // 0–1，边缘/纹理密度
    double motion = 0.0;     // 0–1，仅视频
    int keyframeCount = 0;

    QJsonObject toJson() const;
    static VisualFeatures fromJson(const QJsonObject &object);
};

// 单个素材的分析结果（素材分析阶段的输出）。
struct MaterialProfile
{
    QString id;          // 与 OralMaterial::id 对应
    QString name;        // 展示名
    QString path;        // 本地文件路径
    MaterialKind kind = MaterialKind::Image;
    int width = 0;
    int height = 0;
    qint64 durationMs = 0;
    QString description; // 大模型生成的一句话画面描述
    QString role;        // 产品 / 场景 / 细节 / 人物 / 文字 / 其它
    QStringList tags;    // 语义标签
    QStringList bestFor; // 适合插入的内容类型
    int quality = 0;     // 0–100，画面可用度
    VisualFeatures visual;
    QString keyframePath; // 预览关键帧（本地缓存）
    QString sourceInMs;   // 预留：字符串形式的素材内偏移，便于日志排查

    bool isVideo() const { return kind == MaterialKind::Video; }
    QJsonObject toJson() const;
    static MaterialProfile fromJson(const QJsonObject &object);
};

// ASR 逐词时间线。
struct Word
{
    QString text;
    qint64 startMs = 0;
    qint64 endMs = 0;
};

// ASR 的一句话。
struct Utterance
{
    QString text;
    QString speaker; // 「说话人 1」等展示名
    qint64 startMs = 0;
    qint64 endMs = 0;
    QVector<Word> words;
    double confidence = -1.0; // 服务未返回时为 -1

    QJsonObject toJson() const;
    static Utterance fromJson(const QJsonObject &object);
};

// 语音分析结果：全文、说话人、分句与逐词时间戳。
struct SpeechAnalysis
{
    QString text;
    QString language = QStringLiteral("zh");
    qint64 durationMs = 0;
    QStringList speakers; // 去重后的说话人展示名
    QVector<Utterance> utterances;
    int charCount = 0; // 去除空白后的字数

    bool isEmpty() const { return utterances.isEmpty() && text.isEmpty(); }
    QJsonObject toJson() const;
    static SpeechAnalysis fromJson(const QJsonObject &object);
};

// 语义段落（内容分段）。
struct SemanticSegment
{
    qint64 startMs = 0;
    qint64 endMs = 0;
    QString title;
    QString summary;
    SegmentType type = SegmentType::Other;
    QStringList keywords;
    QString visualHint; // 适合搭配什么画面，供镜头规划使用

    QJsonObject toJson() const;
    static SemanticSegment fromJson(const QJsonObject &object);
};

// 重点句：字幕需要强调的句子/词。
struct Emphasis
{
    qint64 startMs = 0;
    qint64 endMs = 0;
    QString text;
    QStringList words;

    QJsonObject toJson() const;
    static Emphasis fromJson(const QJsonObject &object);
};

// 语义分析结果：内容分段、主题、关键词、发布文案。
struct SemanticAnalysis
{
    QString title;
    QString description;
    QStringList tags;    // 带 # 号，直接用于发布文案
    QStringList topics;  // 不带 # 号
    QStringList keywords;
    QVector<SemanticSegment> segments;
    QVector<Emphasis> emphasis;

    QJsonObject toJson() const;
    static SemanticAnalysis fromJson(const QJsonObject &object);
};

// 镜头规划中的一处 B-Roll。
struct Shot
{
    qint64 startMs = 0;
    qint64 endMs = 0;
    QString materialId;
    MaterialKind materialKind = MaterialKind::Image;
    ShotType shotType = ShotType::Cutaway;
    QString transition; // 「叠化」「渐显」等展示名，空表示硬切
    QString reason;     // 大模型给出的插入理由
    qint64 sourceInMs = 0; // 视频素材从第几毫秒开始截取
    QString preparedPath;  // 预处理后的本地文件（画布尺寸）
    QString uploadedUrl;   // capcut-mate 上传后的临时地址

    qint64 durationMs() const { return qMax<qint64>(0, endMs - startMs); }
    QJsonObject toJson() const;
    static Shot fromJson(const QJsonObject &object);
};

// 镜头规划结果。
struct ShotPlan
{
    Density density = Density::Medium;
    int targetCount = 12;
    QVector<Shot> shots;

    QJsonObject toJson() const;
    static ShotPlan fromJson(const QJsonObject &object);
};

// 一条字幕。
struct SubtitleCue
{
    qint64 startMs = 0;
    qint64 endMs = 0;
    QString text;
    QStringList keywords; // 需要高亮的词
    bool emphasis = false;
    QString animation;

    qint64 durationMs() const { return qMax<qint64>(0, endMs - startMs); }
    QJsonObject toJson() const;
    static SubtitleCue fromJson(const QJsonObject &object);
};

// 字幕系统结果：正文轨 + 标题花字。
struct SubtitlePlan
{
    SubtitlePreset preset = SubtitlePreset::Keyword;
    QVector<SubtitleCue> cues;
    bool titleFx = true;
    QString titleText;
    qint64 titleStartMs = 0;
    qint64 titleEndMs = 2500;

    QJsonObject toJson() const;
    static SubtitlePlan fromJson(const QJsonObject &object);
};

// 一条 BGM / 音效。
struct AudioCue
{
    AudioKind kind = AudioKind::Sfx;
    QString name;
    QString url;
    qint64 startMs = 0;
    qint64 endMs = 0;
    qint64 durationMs = 0; // 素材本身时长；BGM 循环时用于铺轨
    double volume = 0.1;
    bool loop = false;

    QJsonObject toJson() const;
    static AudioCue fromJson(const QJsonObject &object);
};

// 音频系统结果：一首 BGM + 若干音效。
struct AudioPlan
{
    AudioCue bgm;
    QVector<AudioCue> sfx;
    bool bgmEnabled = true;
    bool sfxEnabled = true;

    QJsonObject toJson() const;
    static AudioPlan fromJson(const QJsonObject &object);
};

// 封面海报：取口播首帧用 doubao-seedream-5-0-flash 图生图生成。
// 产品指定的时间线是 [0, 10000] 微秒；领域结构统一用毫秒，提交时再 ×1000。
struct CoverPlan
{
    QString prompt;
    QString localPath;   // 本地生成的封面（画布尺寸）
    QString uploadedUrl; // capcut-mate 上传后的临时地址
    qint64 startMs = 0;
    qint64 endMs = 10; // 10 毫秒 = 10000 微秒

    QJsonObject toJson() const;
    static CoverPlan fromJson(const QJsonObject &object);
};

// 画布配置。
struct Canvas
{
    int width = 1080;
    int height = 1920;
    QString ratio = QStringLiteral("9:16");
    int fps = 30;

    QJsonObject toJson() const;
    static Canvas fromJson(const QJsonObject &object);
};

// 统一剪辑计划 / Timeline JSON。
// 这是「理解 → 规划 → 合成」三步之间唯一的契约：
// 分析结果先固化成计划，再由 capcut-mate 按计划落轨，渲染阶段不再重新理解内容。
struct EditPlan
{
    int version = 1;
    QString templateId = QStringLiteral("clean");
    QString createdAt;
    Canvas canvas;
    qint64 durationMs = 0;
    QString sourcePath;
    QString sourceName;
    int sourceWidth = 0;
    int sourceHeight = 0;
    qint64 sourceSizeBytes = 0;
    bool outputBoth = false;

    SpeechAnalysis speech;
    SemanticAnalysis semantics;
    QVector<MaterialProfile> materials;
    ShotPlan shots;
    SubtitlePlan subtitles;
    AudioPlan audio;
    CoverPlan cover;

    // 完整计划 JSON（包含分析、镜头、字幕、音频与渲染设置）。
    QJsonObject toJson() const;
    // 精简 Timeline JSON：只保留轨道与时间信息，便于剪映/其它工具消费。
    QJsonObject timelineJson() const;
    static EditPlan fromJson(const QJsonObject &object);
};

/* ------------------------------------------------------------------ */
/* 结果历史持久化                                                       */
/* ------------------------------------------------------------------ */

// OralResult 与 JSON 的互转，用于把真实生成记录保存到本地。
QJsonObject oralResultToJson(const StudioData::OralResult &result);
StudioData::OralResult oralResultFromJson(const QJsonObject &object);

} // namespace Oral
