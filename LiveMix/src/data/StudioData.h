#pragma once

// 智能口播 / 智能混剪的示例数据与生成过程模拟。
// 两个模块当前只交付交互界面：素材、模板、文案与过程数据均来自这里，
// 用于 1:1 还原设计稿中的各种状态，不触发真实的 ASR / TTS / 渲染。

#include <QColor>
#include <QImage>
#include <QMetaType>
#include <QString>
#include <QStringList>
#include <QVector>

namespace StudioData {

QString formatElapsed(qint64 milliseconds);
QString formatEta(int seconds);
QString formatNumber(qint64 value);
QString formatGenerationTime(qint64 timestampMs);
QString formatGenerationFullTime(qint64 timestampMs);
int parseElapsed(const QString &clock);

/* ------------------------------------------------------------------ */
/* 智能口播                                                            */
/* ------------------------------------------------------------------ */

enum class OralLayout { Punch, Clean, Vlog, Tech, Magazine, Neon };
enum class OralFont { Sans, Serif, Mono };
enum class OralSubtitleStyle { Keyword, Big, Minimal };
enum class OralDensity { Low, Medium, High };
// 降噪强度：轻度 / 标准 / 强力。只在开启降噪时生效。
enum class OralDenoiseLevel { Light, Standard, Strong };
enum class OralStage { Analyze, Match, Compose, Render };
enum class Tone { Info, Ai, Ok, Muted };
enum class StageStatus { Waiting, Running, Done };

struct OralTemplate {
    QString id;
    QString name;
    QString tagline;
    QString description;
    QStringList tags;
    QColor accent;
    QColor gradientStart;
    QColor gradientMiddle;
    QColor gradientEnd;
    OralLayout layout = OralLayout::Clean;
    QString headline;
    QString caption;
    OralFont font = OralFont::Sans;
};

struct OralMaterial {
    QString id;
    QString name;
    QString tag;
    QString kind;
    QString duration;
    QString visual;
    QColor paletteFrom;
    QColor paletteTo;
    // 本地导入素材的真实路径；示例素材为空。
    QString filePath;
    // 导入素材的缩略图（图片按需缩放，视频截取首帧）；为空时回退到内置插画。
    QImage preview;
};

struct OralTranscriptLine {
    QString time;
    QString text;
    QString highlight;
};

struct OralSource {
    QString name;
    QString duration;
    QString resolution;
    QString size;
    int words = 0;
    int segments = 0;
    bool sample = false;
    QVector<OralTranscriptLine> transcript;
    QStringList topics;
    // 本地导入口播视频的真实路径与首帧缩略图；示例素材为空。
    QString filePath;
    QImage preview;
};

struct OralProject {
    int step = 1;
    bool hasSource = false;
    OralSource source;
    QVector<OralMaterial> materials;
    QString templateId = QStringLiteral("clean");
    OralSubtitleStyle subtitleStyle = OralSubtitleStyle::Keyword;
    OralDensity density = OralDensity::Medium;
    bool transitions = true;
    bool titleFx = true;
    bool bgm = true;
    bool sfx = true;
    // 剪气口默认开启：生成时清理呼吸声与长停顿，并保留 0.4 秒自然换气。
    // 额外删除指令由用户在生成前选填，生成时与默认规则一起执行。
    bool breathCut = true;
    QString breathCutInstruction;
    // 降噪默认关闭；开启后按强度处理人声音轨。
    bool denoise = false;
    OralDenoiseLevel denoiseLevel = OralDenoiseLevel::Standard;
    QString ratio = QStringLiteral("9:16");
    bool outputBoth = false;
};

struct OralResult {
    QString id;
    QString projectName;
    QString title;
    QString description;
    QStringList tags;
    QString templateId;
    QString ratio;
    QString durationLabel;
    QString fileSize;
    bool outputBoth = false;
    qint64 generatedAt = 0;
    QString sourceLabel;
    QString script;
    int subtitleCount = 0;
    int brollCount = 0;
    bool draftGenerated = false;
    // 本次成片启用的人声处理与额外删除指令；重新包装时据此恢复设置。
    // 旧历史没有这些字段时按「默认开启剪气口、关闭降噪」处理。
    bool breathCut = true;
    QString breathCutInstruction;
    bool denoise = false;
    OralDenoiseLevel denoiseLevel = OralDenoiseLevel::Standard;
    // 智能口播真实产物路径：成片 MP4、剪映草稿目录、草稿 ID 与剪辑计划 JSON。
    // 示例历史记录保持为空，界面会退回原型展示。
    QString filePath;
    // 成片首帧预览（真实视频 0 秒帧，即封面海报）的本地缓存路径。
    QString previewPath;
    QString draftPath;
    QString draftId;
    QString editPlanPath;
    // 成片实际视频编码（H.264 / H.265 等），用于结果页输出信息。
    QString videoCodec;
    // 本次成片使用的口播原片路径。随历史记录持久化，重新包装时用来恢复
    // 原始输入；旧版本历史没有该字段时回退到剪辑计划 JSON。
    QString sourcePath;
    // 本次成片使用的真实素材（含缩略图）。缩略图不写入历史 JSON；素材路径
    // 与元数据会随历史保存（见 Oral::oralResultToJson），重新包装时可原样恢复。
    QVector<OralMaterial> materials;
    QVector<QPair<QString, QString>> highlights;
};

struct OralStageState {
    OralStage key = OralStage::Analyze;
    QString label;
    StageStatus status = StageStatus::Waiting;
    double progress = 0.0;
    QString detail;
};

struct OralLogEntry {
    QString id;
    OralStage stage = OralStage::Analyze;
    qint64 atMs = 0;
    QString text;
    Tone tone = Tone::Info;
};

struct OralRun {
    int runId = 0;
    bool done = false;
    qint64 elapsedMs = 0;
    qint64 totalMs = 0;
    double progress = 0.0;
    int etaSeconds = 0;
    OralStage stage = OralStage::Analyze;
    QVector<OralStageState> stages;
    QVector<OralLogEntry> logs;
    QString previewCaption;
    QStringList previewKeywords;
    int matchedMaterials = 0;
    int subtitleCount = 0;
    // 本次成片的字幕总数，过程页进度使用「已生成/总数」而不是固定 24。
    int totalSubtitles = 0;
    int renderedFrames = 0;
    int totalFrames = 0;
};

const QVector<OralTemplate> &oralTemplates();
OralTemplate oralTemplate(const QString &id);
QString oralTemplateName(const QString &id);
const QVector<OralMaterial> &demoMaterials();
OralSource demoOralSource();
OralProject defaultOralProject();
QVector<OralResult> recentOralResults();
QString oralRatioDimensions(const QString &ratio);
qint64 oralRunTotalMs();
OralRun computeOralRun(qint64 elapsedMs, int runId);
OralResult makeOralResult(const OralProject &project, qint64 generatedAt,
                          const QString &templateName);
OralMaterial oralMaterialFromFile(const QString &filePath, qint64 sizeBytes,
                                  bool video, int index);
QString shortOralCaption(const QString &text, int maxLength = 20);
int oralBrollCount(OralDensity density);
QString oralDensityName(OralDensity density);
QString oralDenoiseLevelName(OralDenoiseLevel level);
QString oralDenoiseLevelDescription(OralDenoiseLevel level);
QString oralDenoiseLevelKey(OralDenoiseLevel level);
OralDenoiseLevel oralDenoiseLevelFromKey(const QString &key);

/* ------------------------------------------------------------------ */
/* 智能混剪                                                            */
/* ------------------------------------------------------------------ */

enum class MixcutStyle { Planting, Review, Story };
enum class MixcutStage { Parse, Voice, Match, Design, Render };
enum class MixcutMaterialKind { Video, Image };

QString mixcutStyleName(MixcutStyle style);
QVector<MixcutStyle> mixcutStyles();

struct MixcutStyleMeta {
    QString projectName;
    QString captionTitle;
    QString title;
    QString description;
    QStringList tags;
    QStringList keywords;
    QString bgm;
};

// 一条上传素材（视频或图片）。混剪项目可混合多段视频与多张图片。
struct MixcutMaterial {
    QString id;
    QString name;
    MixcutMaterialKind kind = MixcutMaterialKind::Video;
    // 视频时长（毫秒）；图片为 0。
    qint64 durationMs = 0;
    QString resolution;
    QString size;
    // 视频镜头数；单张图片固定为 1。
    int shots = 0;
    // 示例素材标记（用于「示例素材」胶囊与提示文案）。
    bool sample = false;
    QStringList scenes;
    QStringList topics;
    // 本地导入素材的真实路径；示例素材为空。
    QString filePath;
};

// 由全部素材汇总出的原片概况，供过程页与结果页展示。
struct MixcutSource {
    QString name;
    QString duration;
    QString resolution;
    QString size;
    int shots = 0;
    bool sample = false;
    QStringList scenes;
    QStringList topics;
};

struct MixcutVoice {
    QString id;
    QString name;
    QString label;
    QString gender;
    // 音色所属语言与适用场景，用于音色库筛选与展示。
    QString language;
    QString scene;
    QStringList tags;
    QString description;
    QColor gradientFrom;
    QColor gradientTo;
    QColor accent;
    bool recommended = false;
    // 本地示例音资源路径（qrc）；音色库试听使用。
    QString sampleSrc;
};

struct MixcutMaterialStats {
    int videoCount = 0;
    int imageCount = 0;
    int shots = 0;
    QString durationLabel;
};

struct MixcutProject {
    QVector<MixcutMaterial> materials;
    bool analysisReady = false;
    QString prompt;
    QString script;
    MixcutStyle style = MixcutStyle::Planting;
    QString voiceId = QStringLiteral("zh_female_vv_uranus_bigtts");
    QString ratio = QStringLiteral("9:16");
};

struct MixcutResult {
    QString id;
    QString projectName;
    QString title;
    QString description;
    QStringList tags;
    QString captionTitle;
    QString voiceId;
    MixcutStyle style = MixcutStyle::Planting;
    QString ratio;
    QString durationLabel;
    QString fileSize;
    qint64 generatedAt = 0;
    QString sourceLabel;
    QString scriptPrompt;
    QString script;
    int sceneCount = 0;
    int subtitleCount = 0;
    int sfxCount = 0;
    QString bgm;
    bool draftGenerated = false;
    QVector<QPair<QString, QString>> highlights;
};

struct MixcutScriptStats {
    int chars = 0;
    int sentences = 0;
    int seconds = 0;
    QString durationLabel;
    QStringList lines;
};

struct MixcutStageState {
    MixcutStage key = MixcutStage::Parse;
    QString label;
    StageStatus status = StageStatus::Waiting;
    double progress = 0.0;
    QString detail;
};

struct MixcutLogEntry {
    QString id;
    MixcutStage stage = MixcutStage::Parse;
    qint64 atMs = 0;
    QString text;
    Tone tone = Tone::Info;
};

struct MixcutRun {
    int runId = 0;
    bool done = false;
    qint64 elapsedMs = 0;
    qint64 totalMs = 0;
    double progress = 0.0;
    int etaSeconds = 0;
    MixcutStage stage = MixcutStage::Parse;
    QVector<MixcutStageState> stages;
    QVector<MixcutLogEntry> logs;
    QString previewCaption;
    QStringList previewKeywords;
    double voiceProgress = 0.0;
    int matchedShots = 0;
    int subtitleCount = 0;
    int sfxCount = 0;
    int renderedFrames = 0;
    int totalFrames = 0;
    QString durationLabel;
};

const QVector<MixcutVoice> &mixcutVoices();
MixcutVoice getMixcutVoice(const QString &id);
const MixcutStyleMeta &mixcutStyleMeta(MixcutStyle style);
const QVector<MixcutMaterial> &mixcutDemoMaterials();
MixcutSource mixcutDemoSource();
MixcutSource mixcutSourceFromMaterials(const QVector<MixcutMaterial> &materials);
MixcutMaterialStats mixcutMaterialStats(const QVector<MixcutMaterial> &materials);
bool isMixcutMaterialFile(const QString &filePath);
MixcutMaterial mixcutMaterialFromFile(const QString &filePath, qint64 sizeBytes,
                                      int index);
MixcutProject defaultMixcutProject();
QString mixcutRatioDimensions(const QString &ratio);
QString mixcutRatioHint(const QString &ratio);
QString mixcutFileSize(int seconds, const QString &ratio);
QString generateMixcutScript(MixcutStyle style, int variant);
QString generateMixcutScriptFromPrompt(const QString &prompt, MixcutStyle style,
                                       int variant);
QStringList mixcutScriptLines(const QString &script);
MixcutScriptStats mixcutScriptStats(const QString &script);
QString shortMixcutCaption(const QString &text, int maxLength = 20);
QVector<MixcutResult> recentMixcutResults();
MixcutResult makeMixcutResult(const MixcutProject &project, qint64 generatedAt,
                              const QString &scriptOverride = QString());
qint64 mixcutRunTotalMs(const MixcutProject &project);
MixcutRun computeMixcutRun(const MixcutProject &project, qint64 elapsedMs, int runId);
QString mixcutSceneLabel(const QString &scene);

// 供调色板/海报复用的场景与模板颜色。
QPair<QColor, QColor> mixcutScenePalette(const QString &scene);
QColor mixcutSceneAccent(const QString &scene);

} // namespace StudioData

// 智能口播流水线在独立线程运行，过程快照与结果通过 Qt 信号跨线程传递。
Q_DECLARE_METATYPE(StudioData::OralRun)
Q_DECLARE_METATYPE(StudioData::OralResult)
