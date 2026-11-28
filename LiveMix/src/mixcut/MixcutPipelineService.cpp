// 文件作用：智能混剪流水线实现。
// 六步：素材整理 → 语音理解 → 镜头规划 → 智能包装 → 时间轴编排 →
// capcut-mate 合成。与直播切片 / 智能口播的业务编排互不影响；复用已经
// 验证过的 ASR、素材探测与 capcut-mate 底层客户端。

#include "MixcutPipelineService.h"

#include "../api/AssetsClient.h"
#include "../api/HttpClient.h"
#include "../core/MediaUtils.h"
#include "../media/FfmpegUtils.h"
#include "../oral/OralAsrClient.h"
#include "../oral/OralMediaUtils.h"
#include "MixcutEditPlanBuilder.h"
#include "MixcutLlmClient.h"
#include "MixcutMediaUtils.h"
#include "MixcutSfxCatalog.h"
#include "MixcutTtsClient.h"

#include <QDateTime>
#include <QDir>
#include <QElapsedTimer>
#include <QFile>
#include <QFileInfo>
#include <QHash>
#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <QSaveFile>
#include <QSet>
#include <QThread>
#include <QUrl>
#include <QtMath>

#include <algorithm>

namespace {

constexpr int kStageCount = 6;
constexpr qint64 kRenderPollMs = 4000;
constexpr qint64 kRenderTimeoutMs = 30 * 60 * 1000;
constexpr double kOverlayScale = 0.42;
constexpr double kSfxVolume = 0.18;
constexpr double kBgmVolume = 0.12;
constexpr int kMaxSfxCount = 8;
constexpr int kMaxLlmClipAnalysis = 32;

// 每个阶段占总进度的区间：素材整理 15%、语音 23%、规划 17%、
// 包装 15%、时间轴 6%、合成 24%。
const double kStageBase[kStageCount] = {0, 15, 38, 55, 70, 76};
const double kStageSpan[kStageCount] = {15, 23, 17, 15, 6, 24};

const char *kStageLabels[kStageCount] = {
    "素材整理", "语音理解", "镜头规划", "智能包装", "时间轴", "合成草稿",
};

QString styleDisplayName(StudioData::MixcutStyle style)
{
    const StudioData::MixcutStyleMeta &meta =
        StudioData::mixcutStyleMeta(style);
    return meta.projectName;
}

QString safeOutputBaseName(const Mixcut::EditPlan &plan)
{
    QString base = plan.semantics.title.trimmed();
    if (base.isEmpty()) {
        base = styleDisplayName(plan.style);
    }
    base = MediaUtils::sanitizeFileComponent(base, 32);
    if (base.isEmpty()) {
        base = QStringLiteral("智能混剪");
    }
    base += QLatin1Char('_')
        + QDateTime::currentDateTime().toString(QStringLiteral("yyyyMMdd_HHmmss"));
    return base;
}

QString subtitlePresetLabel(Mixcut::SubtitlePreset preset)
{
    return Mixcut::subtitlePresetName(preset);
}

// 以配音时长为准裁剪 ASR 时间线，避免尾部静音导致字幕越过片尾。
bool clampSpeechToDuration(Mixcut::SpeechAnalysis *speech, qint64 durationMs)
{
    if (!speech || durationMs <= 0) {
        return false;
    }
    speech->durationMs = durationMs;
    QVector<Mixcut::Utterance> clamped;
    for (Mixcut::Utterance utterance : speech->utterances) {
        if (utterance.startMs >= durationMs) {
            continue;
        }
        utterance.endMs = qMin(utterance.endMs, durationMs);
        if (utterance.endMs <= utterance.startMs) {
            continue;
        }
        for (Mixcut::Word &word : utterance.words) {
            word.startMs = qMin(word.startMs, utterance.endMs);
            word.endMs = qMin(word.endMs, utterance.endMs);
        }
        clamped.append(utterance);
    }
    if (clamped.isEmpty()) {
        return false;
    }
    speech->utterances = clamped;
    return true;
}

QStringList splitScriptSentences(const QString &script)
{
    QStringList sentences;
    QString current;
    for (const QChar &ch : script) {
        current.append(ch);
        const bool end = ch == QChar(0x3002) || ch == QChar(0xFF01)
            || ch == QChar(0xFF1F) || ch == QChar(0xFF1B)
            || ch == QLatin1Char('\n');
        if (end) {
            const QString sentence = current.trimmed();
            if (!sentence.isEmpty()) {
                sentences.append(sentence);
            }
            current.clear();
        }
    }
    const QString tail = current.trimmed();
    if (!tail.isEmpty()) {
        sentences.append(tail);
    }
    return sentences;
}

int visibleCharCount(const QString &text)
{
    int count = 0;
    for (const QChar &ch : text) {
        if (!ch.isSpace()) {
            ++count;
        }
    }
    return count;
}

void overlayTransform(Mixcut::PipPosition position, int canvasWidth,
                      int canvasHeight, int *transformX, int *transformY)
{
    const int margin = qRound(qMin(canvasWidth, canvasHeight) * 0.04);
    const int halfWidth = qRound(canvasWidth * kOverlayScale / 2.0);
    const int halfHeight = qRound(canvasHeight * kOverlayScale / 2.0);
    const int x = qMax(margin, canvasWidth / 2 - halfWidth - margin);
    const int y = qMax(margin, canvasHeight / 2 - halfHeight - margin);
    switch (position) {
    case Mixcut::PipPosition::TopLeft:
        *transformX = -x;
        *transformY = y;
        break;
    case Mixcut::PipPosition::BottomLeft:
        *transformX = -x;
        *transformY = -y;
        break;
    case Mixcut::PipPosition::BottomRight:
        *transformX = x;
        *transformY = -y;
        break;
    case Mixcut::PipPosition::TopRight:
    default:
        *transformX = x;
        *transformY = y;
        break;
    }
}

QString effectTitleForStyle(StudioData::MixcutStyle style)
{
    switch (style) {
    case StudioData::MixcutStyle::Review:
        return QStringLiteral("变清晰");
    case StudioData::MixcutStyle::Story:
        return QStringLiteral("秋日暖黄");
    case StudioData::MixcutStyle::Planting:
        break;
    }
    return QStringLiteral("细闪 II");
}

QString filterTitleForStyle(StudioData::MixcutStyle style)
{
    switch (style) {
    case StudioData::MixcutStyle::Review:
        return QStringLiteral("电影画质");
    case StudioData::MixcutStyle::Story:
        return QStringLiteral("电影柔光");
    case StudioData::MixcutStyle::Planting:
        break;
    }
    return QStringLiteral("清透感");
}

double filterIntensityForStyle(StudioData::MixcutStyle style)
{
    switch (style) {
    case StudioData::MixcutStyle::Review:
        return 22.0;
    case StudioData::MixcutStyle::Story:
        return 42.0;
    case StudioData::MixcutStyle::Planting:
        break;
    }
    return 32.0;
}

} // namespace

namespace Mixcut {

PipelineService::PipelineService(const PipelineRequest &request,
                                 QObject *parent)
    : QObject(parent)
    , m_request(request)
{
}

int PipelineService::stageIndex(StudioData::MixcutStage stage) const
{
    return qBound(0, static_cast<int>(stage), kStageCount - 1);
}

void PipelineService::initRun()
{
    qRegisterMetaType<StudioData::MixcutRun>("StudioData::MixcutRun");
    qRegisterMetaType<StudioData::MixcutResult>("StudioData::MixcutResult");

    m_run = StudioData::MixcutRun();
    m_run.runId = m_request.runId;
    m_run.stage = StudioData::MixcutStage::Parse;
    m_run.durationLabel = QStringLiteral("00:00");
    m_startedAtMs = QDateTime::currentMSecsSinceEpoch();
    for (int index = 0; index < kStageCount; ++index) {
        StudioData::MixcutStageState stage;
        stage.key = static_cast<StudioData::MixcutStage>(index);
        stage.label = QString::fromUtf8(kStageLabels[index]);
        stage.status = StudioData::StageStatus::Waiting;
        stage.progress = 0.0;
        stage.detail = QStringLiteral("等待开始");
        m_run.stages.append(stage);
    }
    emitRun();
}

void PipelineService::setStage(StudioData::MixcutStage stage,
                               StudioData::StageStatus status, double progress,
                               const QString &detail)
{
    const int index = stageIndex(stage);
    if (m_run.stages.isEmpty()) {
        initRun();
    }
    StudioData::MixcutStageState &state = m_run.stages[index];
    state.key = stage;
    state.status = status;
    state.progress = qBound(0.0, progress, 100.0);
    state.detail = detail;
    m_run.stage = stage;

    double overall = kStageBase[index] + kStageSpan[index] * state.progress / 100.0;
    if (status == StudioData::StageStatus::Done) {
        overall = kStageBase[index] + kStageSpan[index];
    }
    m_run.elapsedMs = QDateTime::currentMSecsSinceEpoch() - m_startedAtMs;
    m_run.progress = qBound(0.0, overall, status == StudioData::StageStatus::Done
                                  && index == kStageCount - 1
                              ? 100.0
                              : 99.0);
    if (m_run.durationMs > 0) {
        m_run.totalFrames = int(m_run.durationMs * 30 / 1000);
    }
    const double percent = m_run.progress;
    if (percent > 0.5) {
        const qint64 remainingMs = qint64(m_run.elapsedMs * (100.0 - percent)
                                          / percent);
        m_run.etaSeconds = qBound(0, int(remainingMs / 1000), 24 * 3600);
    } else if (m_run.elapsedMs > 0) {
        m_run.etaSeconds = int(m_run.elapsedMs / 1000 * 100);
    }
    m_run.totalMs = m_run.elapsedMs + qint64(m_run.etaSeconds) * 1000;
    emitRun();
}

void PipelineService::addLog(StudioData::MixcutStage stage, const QString &text,
                             StudioData::Tone tone)
{
    StudioData::MixcutLogEntry entry;
    entry.id = QStringLiteral("log-%1").arg(++m_logSequence);
    entry.stage = stage;
    entry.atMs = QDateTime::currentMSecsSinceEpoch() - m_startedAtMs;
    entry.text = text;
    entry.tone = tone;
    m_run.logs.append(entry);
    // 过程页最多保留最近 500 条，避免长任务日志无限增长。
    if (m_run.logs.size() > 500) {
        m_run.logs.remove(0, m_run.logs.size() - 500);
    }
    emitRun();
}

void PipelineService::emitRun()
{
    emit runUpdated(m_run);
}

void PipelineService::cancel()
{
    m_cancelled.store(true);
}

/* ------------------------------------------------------------------ */
/* 入口                                                                 */
/* ------------------------------------------------------------------ */

void PipelineService::run()
{
    initRun();
    addLog(StudioData::MixcutStage::Parse,
           QStringLiteral("开始智能混剪：%1 个素材 · %2 字文案 · %3 音色")
               .arg(m_request.project.materials.size())
               .arg(visibleCharCount(m_request.project.script))
               .arg(StudioData::getMixcutVoice(m_request.project.voiceId).name),
           StudioData::Tone::Info);

    if (m_request.apiKey.trimmed().isEmpty()) {
        const QString message = QStringLiteral("请先在设置中填写 API Key，再开始混剪。");
        addLog(StudioData::MixcutStage::Parse, message, StudioData::Tone::Muted);
        emit finished(false, message, StudioData::MixcutResult());
        return;
    }
    if (m_request.project.script.trimmed().isEmpty()) {
        const QString message = QStringLiteral("口播文案为空，请先生成或填写文案。");
        addLog(StudioData::MixcutStage::Parse, message, StudioData::Tone::Muted);
        emit finished(false, message, StudioData::MixcutResult());
        return;
    }

    m_workDir = QDir(m_request.cacheDir)
                    .filePath(QStringLiteral("work_%1").arg(m_request.runId));
    if (!QDir().mkpath(m_workDir)) {
        const QString message = QStringLiteral("无法创建混剪工作目录：%1").arg(m_workDir);
        addLog(StudioData::MixcutStage::Parse, message, StudioData::Tone::Muted);
        emit finished(false, message, StudioData::MixcutResult());
        return;
    }

    HttpClient http;
    LlmClient llm(&http);
    CapCutClient capcut(&http);

    EditPlan plan;
    plan.createdAt = QDateTime::currentDateTime().toString(Qt::ISODate);
    plan.style = m_request.project.style;
    plan.script = m_request.project.script.trimmed();
    plan.scriptPrompt = m_request.project.prompt;
    plan.voiceId = m_request.project.voiceId;
    const bool portrait = m_request.project.ratio != QLatin1String("16:9");
    plan.canvas.ratio = portrait ? QStringLiteral("9:16") : QStringLiteral("16:9");
    plan.canvas.width = portrait ? 1080 : 1920;
    plan.canvas.height = portrait ? 1920 : 1080;
    plan.canvas.fps = 30;

    QString error;
    if (!analyzeMaterials(&http, &plan.library, &error)) {
        addLog(StudioData::MixcutStage::Parse, error, StudioData::Tone::Muted);
        emit finished(false, error, StudioData::MixcutResult());
        return;
    }
    setStage(StudioData::MixcutStage::Parse, StudioData::StageStatus::Done, 100,
             QStringLiteral("%1 个素材 · %2 个可用镜头")
                 .arg(plan.library.assets.size())
                 .arg(plan.library.clipCount()));
    addLog(StudioData::MixcutStage::Parse,
           QStringLiteral("素材整理完成：%1 个视频 / %2 张图片，共 %3 个镜头")
               .arg(plan.library.videoCount())
               .arg(plan.library.imageCount())
               .arg(plan.library.clipCount()),
           StudioData::Tone::Ok);
    if (isCancelled()) {
        emit finished(false, QStringLiteral("已取消生成"), StudioData::MixcutResult());
        return;
    }

    if (!understandSpeech(&http, &llm, &plan, &error)) {
        addLog(StudioData::MixcutStage::Voice, error, StudioData::Tone::Muted);
        emit finished(false, error, StudioData::MixcutResult());
        return;
    }
    m_run.durationMs = plan.durationMs;
    m_run.durationLabel = StudioData::formatElapsed(plan.durationMs);
    m_run.totalFrames = int(plan.durationMs * plan.canvas.fps / 1000);
    setStage(StudioData::MixcutStage::Voice, StudioData::StageStatus::Done, 100,
             QStringLiteral("%1 · %2 个语义段")
                 .arg(StudioData::formatElapsed(plan.durationMs))
                 .arg(plan.semantics.segments.size()));
    addLog(StudioData::MixcutStage::Voice,
           QStringLiteral("语音理解完成：%1 · 叙事「%2」")
               .arg(StudioData::formatElapsed(plan.durationMs),
                    plan.semantics.narrative),
           StudioData::Tone::Ok);
    if (isCancelled()) {
        emit finished(false, QStringLiteral("已取消生成"), StudioData::MixcutResult());
        return;
    }

    if (!planTimeline(&llm, &plan, &error)) {
        addLog(StudioData::MixcutStage::Match, error, StudioData::Tone::Muted);
        emit finished(false, error, StudioData::MixcutResult());
        return;
    }
    setStage(StudioData::MixcutStage::Match, StudioData::StageStatus::Done, 100,
             QStringLiteral("%1 个主镜头 · %2 处画中画")
                 .arg(plan.shots.size())
                 .arg(plan.overlays.size()));
    addLog(StudioData::MixcutStage::Match,
           QStringLiteral("镜头规划完成：主画面 %1 个镜头，画中画 %2 处")
               .arg(plan.shots.size())
               .arg(plan.overlays.size()),
           StudioData::Tone::Ok);
    if (isCancelled()) {
        emit finished(false, QStringLiteral("已取消生成"), StudioData::MixcutResult());
        return;
    }

    if (!buildPackaging(&http, &llm, &plan, &error)) {
        addLog(StudioData::MixcutStage::Design, error, StudioData::Tone::Muted);
        emit finished(false, error, StudioData::MixcutResult());
        return;
    }
    setStage(StudioData::MixcutStage::Design, StudioData::StageStatus::Done, 100,
             QStringLiteral("%1 条字幕 · %2 处音效")
                 .arg(plan.subtitles.cues.size())
                 .arg(plan.audio.sfx.size()));
    addLog(StudioData::MixcutStage::Design,
           QStringLiteral("智能包装完成：%1 条字幕 · %2 · %3 处音效 · %4")
               .arg(plan.subtitles.cues.size())
               .arg(subtitlePresetLabel(plan.subtitles.preset))
               .arg(plan.audio.sfx.size())
               .arg(plan.audio.bgm.name.isEmpty()
                        ? QStringLiteral("BGM 未启用")
                        : QStringLiteral("BGM「%1」").arg(plan.audio.bgm.name)),
           StudioData::Tone::Ok);
    if (isCancelled()) {
        emit finished(false, QStringLiteral("已取消生成"), StudioData::MixcutResult());
        return;
    }

    if (!finalizeTimeline(&plan, &error)) {
        addLog(StudioData::MixcutStage::Timeline, error, StudioData::Tone::Muted);
        emit finished(false, error, StudioData::MixcutResult());
        return;
    }
    setStage(StudioData::MixcutStage::Timeline, StudioData::StageStatus::Done, 100,
             QStringLiteral("Timeline JSON 已保存"));
    if (isCancelled()) {
        emit finished(false, QStringLiteral("已取消生成"), StudioData::MixcutResult());
        return;
    }

    // 6. 合成：落轨 → 云渲染 → 下载成片与剪映草稿。
    CapCutDraft draft;
    if (!composeDraft(&plan, &capcut, m_workDir, &draft, &error)) {
        addLog(StudioData::MixcutStage::Render, error, StudioData::Tone::Muted);
        emit finished(false, error, StudioData::MixcutResult());
        return;
    }
    m_draftId = draft.id;

    const QString outputDir = m_request.outputDir;
    if (!QDir().mkpath(outputDir)) {
        const QString message =
            QStringLiteral("无法创建成片输出目录：%1").arg(outputDir);
        addLog(StudioData::MixcutStage::Render, message, StudioData::Tone::Muted);
        emit finished(false, message, StudioData::MixcutResult());
        return;
    }
    const QString outputPath =
        QDir(outputDir).filePath(safeOutputBaseName(plan)
                                 + QStringLiteral(".mp4"));
    QString draftPath;
    if (!renderAndDownload(plan, &capcut, draft, outputPath, &draftPath,
                           &error)) {
        addLog(StudioData::MixcutStage::Render, error, StudioData::Tone::Muted);
        emit finished(false, error, StudioData::MixcutResult());
        return;
    }

    setStage(StudioData::MixcutStage::Render, StudioData::StageStatus::Running,
             98, QStringLiteral("生成首帧预览"));
    QString previewPath;
    const QString previewDir =
        QDir(m_request.cacheDir).filePath(QStringLiteral("previews"));
    QDir().mkpath(previewDir);
    const QString previewFile = QDir(previewDir).filePath(
        QStringLiteral("preview_%1.jpg")
            .arg(QDateTime::currentMSecsSinceEpoch()));
    QString previewError;
    // 取 1.2 秒左右的画面：避开图片/视频淡入的首帧黑场，预览更接近成片观感。
    const qint64 previewAtMs =
        qMin<qint64>(1200, qMax<qint64>(300, plan.durationMs / 3));
    if (FfmpegUtils::extractThumbnail(outputPath, previewAtMs, previewFile,
                                      &previewError)) {
        previewPath = previewFile;
    } else {
        addLog(StudioData::MixcutStage::Render,
               QStringLiteral("成片首帧预览生成失败：%1").arg(previewError),
               StudioData::Tone::Muted);
    }

    m_run.done = true;
    m_run.progress = 100.0;
    m_run.etaSeconds = 0;
    m_run.elapsedMs = QDateTime::currentMSecsSinceEpoch() - m_startedAtMs;
    m_run.renderedFrames = m_run.totalFrames;
    setStage(StudioData::MixcutStage::Render, StudioData::StageStatus::Done, 100,
             QStringLiteral("%1×%2 · %3fps")
                 .arg(plan.canvas.width)
                 .arg(plan.canvas.height)
                 .arg(plan.canvas.fps));
    m_run.done = true;
    m_run.progress = 100.0;
    emitRun();

    const StudioData::MixcutResult result =
        makeResult(plan, outputPath, draftPath, previewPath);
    QString message = QStringLiteral("智能混剪成片已生成并自动下载");
    if (!draftPath.isEmpty()) {
        message += QStringLiteral("，剪映草稿已写入本地目录");
    }
    emit finished(true, message, result);
}

/* ------------------------------------------------------------------ */
/* 1. 素材整理                                                          */
/* ------------------------------------------------------------------ */

bool PipelineService::analyzeMaterials(HttpClient *http, MaterialLibrary *library,
                                       QString *error)
{
    Q_UNUSED(http);
    if (!library) {
        if (error) {
            *error = QStringLiteral("素材库参数无效。");
        }
        return false;
    }
    if (m_request.project.materials.isEmpty()) {
        if (error) {
            *error = QStringLiteral("请先上传视频或图片素材。");
        }
        return false;
    }

    const QString frameRoot = QDir(m_workDir).filePath(QStringLiteral("frames"));
    QDir().mkpath(frameRoot);
    const int materialCount = m_request.project.materials.size();
    int index = 0;
    QStringList keywordPool;

    for (const StudioData::MixcutMaterial &material :
         m_request.project.materials) {
        if (isCancelled()) {
            if (error) {
                *error = QStringLiteral("已取消生成");
            }
            return false;
        }
        ++index;
        if (material.filePath.trimmed().isEmpty()
            || !QFileInfo::exists(material.filePath)) {
            if (error) {
                *error = QStringLiteral("素材「%1」文件不存在，请重新上传。")
                             .arg(material.name);
            }
            return false;
        }
        setStage(StudioData::MixcutStage::Parse,
                 StudioData::StageStatus::Running,
                 3.0 + 27.0 * (index - 1) / qMax(1, materialCount),
                 QStringLiteral("分析素材 %1/%2：%3")
                     .arg(index)
                     .arg(materialCount)
                     .arg(material.name));
        addLog(StudioData::MixcutStage::Parse,
               QStringLiteral("正在分析素材「%1」…").arg(material.name),
               StudioData::Tone::Info);

        MaterialAsset asset;
        asset.id = material.id.trimmed().isEmpty()
            ? QStringLiteral("mat-%1").arg(index)
            : material.id;
        asset.name = material.name;
        asset.path = material.filePath;
        asset.kind = material.kind == StudioData::MixcutMaterialKind::Video
            ? MaterialKind::Video
            : MaterialKind::Image;
        asset.sizeBytes = QFileInfo(material.filePath).size();

        if (asset.kind == MaterialKind::Image) {
            int width = 0;
            int height = 0;
            QString probeError;
            if (!Oral::probeImage(material.filePath, &width, &height,
                                  &probeError)) {
                addLog(StudioData::MixcutStage::Parse,
                       QStringLiteral("图片「%1」读取失败，已跳过：%2")
                           .arg(material.name, probeError),
                       StudioData::Tone::Muted);
                continue;
            }
            asset.width = width;
            asset.height = height;
            asset.durationMs = 0;
            MaterialClip clip;
            clip.id = asset.id + QStringLiteral("-c0");
            clip.assetId = asset.id;
            clip.assetName = asset.name;
            clip.path = asset.path;
            clip.kind = MaterialKind::Image;
            clip.sourceInMs = 0;
            clip.sourceOutMs = 0;
            clip.durationMs = 3600;
            clip.width = width;
            clip.height = height;
            clip.keyframePath = material.filePath;
            clip.visual = Oral::analyzeVisualFeatures({material.filePath}, false);
            clip.quality = localQualityScore(clip.visual, width, height, false);
            clip.tags = localSceneTags(material.name, MaterialKind::Image,
                                       clip.visual);
            clip.bestFor = clip.tags.mid(0, 2);
            clip.description = QStringLiteral("%1 · 图片素材").arg(material.name);
            asset.clips.append(clip);
            asset.tags = clip.tags;
            asset.scenes = clip.tags.mid(0, 4);
            library->assets.append(asset);
            keywordPool += clip.tags;
            addLog(StudioData::MixcutStage::Parse,
                   QStringLiteral("图片「%1」· %2×%3 · 质量 %4")
                       .arg(material.name)
                       .arg(width)
                       .arg(height)
                       .arg(clip.quality),
                   StudioData::Tone::Info);
            continue;
        }

        int width = 0;
        int height = 0;
        qint64 durationMs = 0;
        QString probeError;
        if (!Oral::probeVideo(material.filePath, &width, &height, &durationMs,
                              &probeError)) {
            addLog(StudioData::MixcutStage::Parse,
                   QStringLiteral("视频「%1」读取失败，已跳过：%2")
                       .arg(material.name, probeError),
                   StudioData::Tone::Muted);
            continue;
        }
        asset.width = width;
        asset.height = height;
        asset.durationMs = durationMs;

        QVector<qint64> cuts;
        QString detectError;
        if (!detectSceneCuts(material.filePath, durationMs, &cuts,
                             &detectError)) {
            addLog(StudioData::MixcutStage::Parse,
                   QStringLiteral("场景检测失败，按整段使用：%1").arg(detectError),
                   StudioData::Tone::Muted);
        }
        const QVector<QPair<qint64, qint64>> ranges =
            normalizeSceneRanges(cuts, durationMs, 8);
        const QString clipDir =
            QDir(frameRoot).filePath(MediaUtils::sanitizeFileComponent(asset.id, 24));
        QDir().mkpath(clipDir);
        int clipIndex = 0;
        for (const auto &range : ranges) {
            if (isCancelled()) {
                if (error) {
                    *error = QStringLiteral("已取消生成");
                }
                return false;
            }
            const qint64 clipDuration = range.second - range.first;
            if (clipDuration < 600) {
                continue;
            }
            MaterialClip clip;
            clip.id = QStringLiteral("%1-c%2").arg(asset.id).arg(clipIndex);
            clip.assetId = asset.id;
            clip.assetName = asset.name;
            clip.path = asset.path;
            clip.kind = MaterialKind::Video;
            clip.sourceInMs = range.first;
            clip.sourceOutMs = range.second;
            clip.durationMs = clipDuration;
            clip.width = width;
            clip.height = height;

            const qint64 keyPos = range.first + clipDuration / 2;
            const QString keyPath = QDir(clipDir).filePath(
                QStringLiteral("clip_%1.jpg").arg(clipIndex));
            QString frameError;
            if (extractFrameAt(material.filePath, keyPos, keyPath,
                               &frameError)) {
                clip.keyframePath = keyPath;
            }
            const qint64 motionPos = range.first + clipDuration * 3 / 4;
            const QString motionPath = QDir(clipDir).filePath(
                QStringLiteral("clip_%1_m.jpg").arg(clipIndex));
            QStringList frames;
            if (!clip.keyframePath.isEmpty()) {
                frames.append(clip.keyframePath);
            }
            if (extractFrameAt(material.filePath, motionPos, motionPath,
                               &frameError)) {
                frames.append(motionPath);
            }
            clip.visual = Oral::analyzeVisualFeatures(frames, true);
            clip.quality =
                localQualityScore(clip.visual, width, height, true);
            clip.tags =
                localSceneTags(material.name, MaterialKind::Video, clip.visual);
            clip.bestFor = clip.tags.mid(0, 2);
            clip.description = QStringLiteral("%1 · 镜头 %2").arg(
                material.name, QString::number(clipIndex + 1));
            asset.clips.append(clip);
            keywordPool += clip.tags;
            ++clipIndex;
        }
        if (asset.clips.isEmpty()) {
            MaterialClip clip;
            clip.id = asset.id + QStringLiteral("-c0");
            clip.assetId = asset.id;
            clip.assetName = asset.name;
            clip.path = asset.path;
            clip.kind = MaterialKind::Video;
            clip.sourceInMs = 0;
            clip.sourceOutMs = durationMs;
            clip.durationMs = durationMs;
            clip.width = width;
            clip.height = height;
            clip.quality = 60;
            clip.tags = localSceneTags(material.name, MaterialKind::Video,
                                       clip.visual);
            clip.description = material.name;
            asset.clips.append(clip);
        }
        for (const MaterialClip &clip : asset.clips) {
            for (const QString &tag : clip.tags) {
                if (!asset.tags.contains(tag)) {
                    asset.tags.append(tag);
                }
            }
        }
        asset.scenes = asset.tags.mid(0, 4);
        library->assets.append(asset);
        addLog(StudioData::MixcutStage::Parse,
               QStringLiteral("视频「%1」· %2 · 切出 %3 个镜头")
                   .arg(material.name)
                   .arg(StudioData::formatElapsed(durationMs))
                   .arg(asset.clips.size()),
               StudioData::Tone::Ok);
    }

    if (library->assets.isEmpty()) {
        if (error) {
            *error = QStringLiteral("没有可用的视频或图片素材，请重新上传。");
        }
        return false;
    }

    m_run.clipCount = library->clipCount();
    m_run.matchedShots = library->clipCount();
    QVector<MaterialClip> clips = library->clips();
    fallbackClipAnalysis(library);
    if (clips.size() > kMaxLlmClipAnalysis) {
        addLog(StudioData::MixcutStage::Parse,
               QStringLiteral("镜头较多（%1 个），仅前 %2 个做 AI 画面理解，"
                              "其余使用本地特征标签，控制分析耗时与费用")
                   .arg(clips.size())
                   .arg(kMaxLlmClipAnalysis),
               StudioData::Tone::Muted);
    }
    int analyzed = 0;
    const int batchSize = 8;
    for (int start = 0; start < clips.size() && analyzed < kMaxLlmClipAnalysis;
         start += batchSize) {
        if (isCancelled()) {
            if (error) {
                *error = QStringLiteral("已取消生成");
            }
            return false;
        }
        QVector<MaterialClip> batch;
        for (int i = start;
             i < clips.size() && i < start + batchSize
             && analyzed < kMaxLlmClipAnalysis;
             ++i) {
            const MaterialClip *found = library->clipById(clips.at(i).id);
            if (found) {
                batch.append(*found);
            }
            ++analyzed;
        }
        setStage(StudioData::MixcutStage::Parse,
                 StudioData::StageStatus::Running,
                 30.0 + 60.0 * analyzed / qMax(1, clips.size()),
                 QStringLiteral("AI 理解画面 %1/%2")
                     .arg(analyzed)
                     .arg(clips.size()));
        QVector<MaterialClip> profiles = batch;
        QString llmError;
        LlmClient llm(http);
        if (!llm.analyzeMaterials(m_request.gatewayBaseUrl, m_request.apiKey,
                                  &profiles, &llmError,
                                  [this](const QString &status) {
                                      addLog(StudioData::MixcutStage::Parse,
                                             status, StudioData::Tone::Ai);
                                  })) {
            addLog(StudioData::MixcutStage::Parse,
                   QStringLiteral("画面上传/理解失败，使用本地特征兜底：%1")
                       .arg(llmError),
                   StudioData::Tone::Muted);
            continue;
        }
        QHash<QString, MaterialClip> byId;
        for (const MaterialClip &profile : profiles) {
            byId.insert(profile.id, profile);
        }
        for (MaterialAsset &asset : library->assets) {
            for (MaterialClip &clip : asset.clips) {
                const auto it = byId.constFind(clip.id);
                if (it != byId.constEnd()) {
                    clip = it.value();
                }
            }
        }
    }
    fallbackClipAnalysis(library);

    // 素材级标签汇总，供后续镜头匹配与过程页展示。
    for (MaterialAsset &asset : library->assets) {
        asset.tags.clear();
        for (const MaterialClip &clip : asset.clips) {
            for (const QString &tag : clip.tags) {
                if (!asset.tags.contains(tag)) {
                    asset.tags.append(tag);
                }
            }
        }
        asset.scenes = asset.tags.mid(0, 4);
    }
    keywordPool.clear();
    for (const MaterialAsset &asset : library->assets) {
        for (const MaterialClip &clip : asset.clips) {
            for (const QString &tag : clip.tags) {
                if (!keywordPool.contains(tag)) {
                    keywordPool.append(tag);
                }
            }
        }
    }
    m_run.previewKeywords = keywordPool.mid(0, 6);
    m_run.matchedShots = library->clipCount();
    if (!keywordPool.isEmpty()) {
        addLog(StudioData::MixcutStage::Parse,
               QStringLiteral("内容标签：%1")
                   .arg(keywordPool.mid(0, 6).join(QStringLiteral("、"))),
               StudioData::Tone::Ai);
    }
    return true;
}

void PipelineService::fallbackClipAnalysis(MaterialLibrary *library) const
{
    if (!library) {
        return;
    }
    for (MaterialAsset &asset : library->assets) {
        for (MaterialClip &clip : asset.clips) {
            if (clip.description.trimmed().isEmpty()) {
                clip.description = clip.assetName;
            }
            if (clip.tags.isEmpty()) {
                clip.tags = localSceneTags(clip.assetName, clip.kind,
                                           clip.visual);
            }
            if (clip.bestFor.isEmpty()) {
                clip.bestFor = clip.tags.mid(0, 2);
            }
            if (clip.quality <= 0) {
                clip.quality = localQualityScore(clip.visual, clip.width,
                                                 clip.height, clip.isVideo());
            }
            if (clip.role == MaterialRole::Other) {
                const QString joined = clip.tags.join(QStringLiteral(" "));
                if (joined.contains(QStringLiteral("产品"))
                    || joined.contains(QStringLiteral("特写"))) {
                    clip.role = MaterialRole::Product;
                } else if (joined.contains(QStringLiteral("人"))) {
                    clip.role = MaterialRole::Person;
                } else if (joined.contains(QStringLiteral("场景"))
                           || joined.contains(QStringLiteral("街"))) {
                    clip.role = MaterialRole::Scene;
                } else if (joined.contains(QStringLiteral("细节"))
                           || joined.contains(QStringLiteral("质地"))) {
                    clip.role = MaterialRole::Detail;
                } else if (joined.contains(QStringLiteral("文字"))
                           || joined.contains(QStringLiteral("截图"))) {
                    clip.role = MaterialRole::Text;
                }
            }
        }
    }
}

/* ------------------------------------------------------------------ */
/* 2. AI 语音理解                                                       */
/* ------------------------------------------------------------------ */

bool PipelineService::understandSpeech(HttpClient *http, LlmClient *llm,
                                       EditPlan *plan, QString *error)
{
    if (!http || !llm || !plan) {
        if (error) {
            *error = QStringLiteral("语音理解参数无效。");
        }
        return false;
    }
    setStage(StudioData::MixcutStage::Voice,
             StudioData::StageStatus::Running, 3,
             QStringLiteral("合成 AI 配音"));
    TtsClient tts(http);
    QString ttsPath;
    qint64 durationMs = 0;
    if (!tts.synthesize(
            m_request.gatewayBaseUrl, m_request.apiKey, plan->script,
            plan->voiceId, m_request.cacheDir, m_workDir, &ttsPath,
            &durationMs, error,
            [this](const QString &status) {
                addLog(StudioData::MixcutStage::Voice, status,
                       StudioData::Tone::Ai);
            })) {
        return false;
    }
    const StudioData::MixcutVoice voice =
        StudioData::getMixcutVoice(plan->voiceId);
    plan->ttsPath = ttsPath;
    plan->durationMs = durationMs;
    plan->speech.durationMs = durationMs;
    m_run.durationLabel = StudioData::formatElapsed(durationMs);
    m_run.durationMs = durationMs;
    m_run.totalFrames = int(durationMs * plan->canvas.fps / 1000);
    addLog(StudioData::MixcutStage::Voice,
           QStringLiteral("配音已合成：%1 · %2 · %3")
               .arg(voice.name, voice.label,
                    StudioData::formatElapsed(durationMs)),
           StudioData::Tone::Ok);
    setStage(StudioData::MixcutStage::Voice,
             StudioData::StageStatus::Running, 42,
             QStringLiteral("识别配音时间线"));

    if (!transcribeSpeech(plan, error)) {
        return false;
    }

    setStage(StudioData::MixcutStage::Voice,
             StudioData::StageStatus::Running, 72,
             QStringLiteral("理解语义、情绪与叙事结构"));
    if (!analyzeSemantics(llm, plan, error)) {
        return false;
    }
    m_run.previewKeywords = plan->semantics.keywords.mid(0, 6);
    m_run.previewCaption = StudioData::shortMixcutCaption(plan->script, 24);
    setStage(StudioData::MixcutStage::Voice,
             StudioData::StageStatus::Running, 92,
             QStringLiteral("对齐字幕与配音"));
    return true;
}

bool PipelineService::transcribeSpeech(EditPlan *plan, QString *error)
{
    Q_UNUSED(error);
    if (!plan) {
        return false;
    }
    HttpClient http;
    Oral::AsrClient asr(&http);
    SpeechAnalysis speech;
    QString primaryError;
    addLog(StudioData::MixcutStage::Voice,
           QStringLiteral("正在调用 ASR（%1）…")
               .arg(Oral::AsrClient::modelName()),
           StudioData::Tone::Info);
    bool ok = asr.transcribe(plan->ttsPath, m_request.gatewayBaseUrl,
                             m_request.apiKey, &speech, &primaryError);
    if (!ok && Oral::asrErrorNeedsFallback(primaryError)) {
        addLog(StudioData::MixcutStage::Voice,
               QStringLiteral("豆包极速版未开通，正在回退本地文件模型 %1…")
                   .arg(Oral::AsrClient::fallbackFileModelName()),
               StudioData::Tone::Muted);
        QString fallbackError;
        ok = asr.transcribeWithModel(
            plan->ttsPath, Oral::AsrClient::fallbackFileModelName(),
            m_request.gatewayBaseUrl, m_request.apiKey, &speech,
            &fallbackError);
        if (!ok) {
            primaryError += QStringLiteral("；回退也失败：%1").arg(fallbackError);
        }
    }
    if (ok && clampSpeechToDuration(&speech, plan->durationMs)
        && !speech.utterances.isEmpty()) {
        speech.text = speech.text.trimmed();
        plan->speech = speech;
        addLog(StudioData::MixcutStage::Voice,
               QStringLiteral("语音识别完成：%1 句 · %2 字")
                   .arg(speech.utterances.size())
                   .arg(speech.charCount),
               StudioData::Tone::Ok);
        return true;
    }
    addLog(StudioData::MixcutStage::Voice,
           QStringLiteral("语音识别不可用，按文案字数估算时间轴：%1")
               .arg(ok ? QStringLiteral("识别结果与配音时长不匹配")
                       : primaryError),
           StudioData::Tone::Muted);
    buildFallbackSpeech(plan);
    return true;
}

void PipelineService::buildFallbackSpeech(EditPlan *plan) const
{
    if (!plan) {
        return;
    }
    const QStringList sentences = splitScriptSentences(plan->script);
    SpeechAnalysis speech;
    speech.durationMs = plan->durationMs;
    speech.language = QStringLiteral("zh");
    speech.text = plan->script;
    speech.charCount = visibleCharCount(plan->script);
    const qint64 totalMs = qMax<qint64>(1, plan->durationMs);
    QVector<int> weights;
    qint64 totalWeight = 0;
    for (const QString &sentence : sentences) {
        const int weight = qMax(1, visibleCharCount(sentence));
        weights.append(weight);
        totalWeight += weight;
    }
    if (sentences.isEmpty() || totalWeight <= 0) {
        Utterance utterance;
        utterance.text = plan->script;
        utterance.startMs = 0;
        utterance.endMs = totalMs;
        speech.utterances.append(utterance);
    } else {
        qint64 cursor = 0;
        qint64 accumulated = 0;
        for (int index = 0; index < sentences.size(); ++index) {
            accumulated += weights.at(index);
            const qint64 end =
                index == sentences.size() - 1
                ? totalMs
                : qMin(totalMs, qRound64(double(totalMs) * accumulated / totalWeight));
            Utterance utterance;
            utterance.text = sentences.at(index);
            utterance.startMs = cursor;
            utterance.endMs =
                qMin(totalMs, qMax(cursor + 300, end));
            if (utterance.endMs <= utterance.startMs) {
                utterance.endMs = totalMs;
            }
            speech.utterances.append(utterance);
            cursor = utterance.endMs;
        }
    }
    plan->speech = speech;
}

bool PipelineService::analyzeSemantics(LlmClient *llm, EditPlan *plan,
                                       QString *error)
{
    if (!llm || !plan) {
        return false;
    }
    SemanticAnalysis semantics;
    QString llmError;
    if (llm->analyzeSemantics(
            m_request.gatewayBaseUrl, m_request.apiKey, plan->speech,
            &semantics, &llmError,
            [this](const QString &status) {
                addLog(StudioData::MixcutStage::Voice, status,
                       StudioData::Tone::Ai);
            })) {
        // 确保分段覆盖全片：模型给的时间可能没有对齐到片尾。
        if (!semantics.segments.isEmpty()) {
            semantics.segments.first().startMs = 0;
            semantics.segments.last().endMs = plan->durationMs;
        }
        plan->semantics = semantics;
        addLog(StudioData::MixcutStage::Voice,
               QStringLiteral("语义分析完成：%1 个段落 · 关键词 %2 个%3")
                   .arg(semantics.segments.size())
                   .arg(semantics.keywords.size())
                   .arg(semantics.keywords.isEmpty()
                            ? QString()
                            : QStringLiteral("（%1）")
                                  .arg(semantics.keywords.mid(0, 5)
                                           .join(QStringLiteral("、")))),
               StudioData::Tone::Ok);
        return true;
    }
    addLog(StudioData::MixcutStage::Voice,
           QStringLiteral("语义分析失败，使用本地兜底：%1").arg(llmError),
           StudioData::Tone::Muted);
    buildFallbackSemantics(plan);
    *error = llmError;
    return true;
}

void PipelineService::buildFallbackSemantics(EditPlan *plan) const
{
    if (!plan) {
        return;
    }
    const StudioData::MixcutStyleMeta &meta =
        StudioData::mixcutStyleMeta(plan->style);
    SemanticAnalysis semantics;
    semantics.title = meta.title;
    semantics.description = meta.description;
    semantics.tags = meta.tags;
    semantics.keywords = meta.keywords;
    semantics.topics = meta.keywords.mid(0, 4);
    semantics.narrative = QStringLiteral("钩子 → 痛点 → 方案 → 证据 → 行动");
    semantics.emotion = Emotion::Neutral;
    if (semantics.description.isEmpty()) {
        semantics.description = plan->script.left(120);
    }
    if (semantics.title.isEmpty()) {
        semantics.title = QStringLiteral("智能混剪");
    }
    if (semantics.tags.isEmpty()) {
        semantics.tags = {QStringLiteral("#智能混剪"), QStringLiteral("#AI成片"),
                          QStringLiteral("#短视频")};
    }

    const qint64 duration = qMax<qint64>(2000, plan->durationMs);
    const int count = qBound(3, int(duration / 15000), 6);
    static const SegmentType types[] = {
        SegmentType::Hook, SegmentType::Pain, SegmentType::Point,
        SegmentType::Example, SegmentType::Evidence, SegmentType::Cta,
    };
    static const NarrativeRole narratives[] = {
        NarrativeRole::Hook, NarrativeRole::Conflict, NarrativeRole::Setup,
        NarrativeRole::Turn, NarrativeRole::Payoff, NarrativeRole::Cta,
    };
    for (int index = 0; index < count; ++index) {
        SemanticSegment segment;
        segment.startMs = duration * index / count;
        segment.endMs = index == count - 1 ? duration
                                           : duration * (index + 1) / count;
        segment.type = types[qMin(index, 5)];
        segment.narrative = narratives[qMin(index, 5)];
        segment.title = QStringLiteral("段落 %1").arg(index + 1);
        segment.summary = semantics.description;
        segment.keywords = semantics.keywords.mid(0, 3);
        segment.emotion = semantics.emotion;
        semantics.segments.append(segment);
    }
    plan->semantics = semantics;
}

/* ------------------------------------------------------------------ */
/* 3. AI 镜头规划                                                       */
/* ------------------------------------------------------------------ */

bool PipelineService::planTimeline(LlmClient *llm, EditPlan *plan,
                                   QString *error)
{
    if (!llm || !plan) {
        return false;
    }
    const int target = targetMainShotCount(plan->durationMs);
    setStage(StudioData::MixcutStage::Match,
             StudioData::StageStatus::Running, 5,
             QStringLiteral("匹配语音片段与素材"));
    QVector<Shot> llmShots;
    QVector<Shot> llmOverlays;
    QString llmError;
    if (llm->planTimeline(
            m_request.gatewayBaseUrl, m_request.apiKey, plan->speech,
            plan->semantics, plan->library, target, &llmShots, &llmOverlays,
            &llmError,
            [this](const QString &status) {
                addLog(StudioData::MixcutStage::Match, status,
                       StudioData::Tone::Ai);
            })) {
        addLog(StudioData::MixcutStage::Match,
               QStringLiteral("大模型给出 %1 个主镜头、%2 处画中画，正在本地校验")
                   .arg(llmShots.size())
                   .arg(llmOverlays.size()),
               StudioData::Tone::Ai);
    } else {
        addLog(StudioData::MixcutStage::Match,
               QStringLiteral("镜头规划模型不可用，按语句与素材标签本地规划：%1")
                   .arg(llmError),
               StudioData::Tone::Muted);
    }

    setStage(StudioData::MixcutStage::Match,
             StudioData::StageStatus::Running, 65,
             QStringLiteral("校验镜头顺序与时长"));
    plan->shots = normalizeMainShots(llmShots, plan->library, plan->speech,
                                     plan->semantics, m_request.transitions);
    plan->overlays = normalizeOverlays(llmOverlays, plan->shots, plan->library,
                                       plan->speech, plan->semantics);
    if (plan->shots.isEmpty()) {
        if (error) {
            *error = QStringLiteral("没有规划出可用镜头，请更换素材后重试。");
        }
        return false;
    }
    m_run.matchedShots = plan->shots.size() + plan->overlays.size();
    if (m_run.previewCaption.isEmpty()) {
        m_run.previewCaption =
            StudioData::shortMixcutCaption(plan->script, 24);
    }
    addLog(StudioData::MixcutStage::Match,
           QStringLiteral("镜头校验完成：主画面连续覆盖 %1，共 %2 个镜头")
               .arg(StudioData::formatElapsed(plan->durationMs))
               .arg(plan->shots.size()),
           StudioData::Tone::Ok);
    return true;
}

/* ------------------------------------------------------------------ */
/* 4. 智能包装                                                          */
/* ------------------------------------------------------------------ */

bool PipelineService::buildPackaging(HttpClient *http, LlmClient *llm,
                                     EditPlan *plan, QString *error)
{
    if (!plan) {
        return false;
    }
    setStage(StudioData::MixcutStage::Design,
             StudioData::StageStatus::Running, 5,
             QStringLiteral("生成字幕系统"));
    const SubtitlePreset preset = subtitlePresetForStyle(plan->style);
    plan->subtitles = buildSubtitles(plan->speech, plan->semantics, preset,
                                     plan->canvas, true);
    m_run.subtitleCount = plan->subtitles.cues.size();
    addLog(StudioData::MixcutStage::Design,
           QStringLiteral("字幕：%1 条 · %2 · 关键词自动高亮")
               .arg(plan->subtitles.cues.size())
               .arg(subtitlePresetLabel(preset)),
           StudioData::Tone::Ai);

    setStage(StudioData::MixcutStage::Design,
             StudioData::StageStatus::Running, 35,
             QStringLiteral("匹配 BGM 与音效"));
    buildAudioPlan(http, llm, plan, error);
    m_run.sfxCount = plan->audio.sfx.size();

    setStage(StudioData::MixcutStage::Design,
             StudioData::StageStatus::Running, 72,
             QStringLiteral("应用转场、动画与画面特效"));
    buildEffectsAndFilters(plan);
    if (!plan->effects.isEmpty()) {
        addLog(StudioData::MixcutStage::Design,
               QStringLiteral("画面特效「%1」· %2–%3")
                   .arg(plan->effects.first().title,
                        StudioData::formatElapsed(plan->effects.first().startMs),
                        StudioData::formatElapsed(plan->effects.first().endMs)),
               StudioData::Tone::Info);
    }
    if (!plan->filters.isEmpty()) {
        addLog(StudioData::MixcutStage::Design,
               QStringLiteral("滤镜「%1」· 全片 · 强度 %2")
                   .arg(plan->filters.first().title)
                   .arg(qRound(plan->filters.first().intensity)),
               StudioData::Tone::Info);
    }
    return true;
}

bool PipelineService::buildAudioPlan(HttpClient *http, LlmClient *llm,
                                     EditPlan *plan, QString *error)
{
    Q_UNUSED(error);
    if (!http || !plan) {
        return true;
    }
    plan->audio = AudioPlan();
    plan->audio.bgmEnabled = m_request.bgm;
    plan->audio.sfxEnabled = m_request.sfx;

    AssetsClient assets(http);
    if (plan->audio.bgmEnabled) {
        BgmTrack track;
        QString bgmError;
        if (assets.randomBgm(&track, &bgmError)
            && !track.url.trimmed().isEmpty()) {
            AudioCue cue;
            cue.kind = AudioKind::Bgm;
            cue.name = track.name.isEmpty() ? QStringLiteral("背景音乐")
                                            : track.name;
            cue.url = track.url;
            cue.startMs = 0;
            cue.endMs = plan->durationMs;
            cue.durationMs = track.durationMs;
            cue.volume = kBgmVolume;
            cue.loop = true;
            plan->audio.bgm = cue;
            addLog(StudioData::MixcutStage::Design,
                   QStringLiteral("BGM 已匹配「%1」· 音量 %2")
                       .arg(cue.name)
                       .arg(qRound(cue.volume * 100)),
                   StudioData::Tone::Info);
        } else {
            addLog(StudioData::MixcutStage::Design,
                   QStringLiteral("BGM 获取失败，成片继续输出：%1").arg(bgmError),
                   StudioData::Tone::Muted);
        }
    }

    if (!plan->audio.sfxEnabled || plan->subtitles.cues.isEmpty()) {
        return true;
    }
    QVector<SfxAsset> sfxAssets;
    QString sfxError;
    if (!assets.listSfx(&sfxAssets, &sfxError)) {
        addLog(StudioData::MixcutStage::Design,
               QStringLiteral("音效素材获取失败，已跳过音效：%1").arg(sfxError),
               StudioData::Tone::Muted);
        return true;
    }
    const QVector<ResolvedSfx> resolved = resolveSfx(sfxAssets);
    if (resolved.isEmpty()) {
        addLog(StudioData::MixcutStage::Design,
               QStringLiteral("音效目录为空，已跳过音效"),
               StudioData::Tone::Muted);
        return true;
    }
    QVector<SfxCategoryHint> categories;
    for (const ResolvedSfx &sfx : resolved) {
        categories.append({sfx.name, sfx.usageHint});
    }
    QVector<int> categoriesByCue;
    bool selected = false;
    if (llm) {
        selected = llm->selectSfx(
            m_request.gatewayBaseUrl, m_request.apiKey, plan->subtitles.cues,
            categories, &categoriesByCue, &sfxError,
            [this](const QString &status) {
                addLog(StudioData::MixcutStage::Design, status,
                       StudioData::Tone::Ai);
            });
    }
    if (!selected) {
        categoriesByCue = QVector<int>(plan->subtitles.cues.size(), -1);
        // 本地兜底：均匀挑 3 条字幕，优先带数字/强调的句子。
        int added = 0;
        const int stride = qMax(1, int(plan->subtitles.cues.size() / 3));
        for (int index = 0;
             index < plan->subtitles.cues.size() && added < 3;
             index += stride) {
            categoriesByCue[index] = (added * 3) % resolved.size();
            ++added;
        }
        if (!selected) {
            addLog(StudioData::MixcutStage::Design,
                   QStringLiteral("音效选点模型不可用，已按字幕节奏均匀选点"),
                   StudioData::Tone::Muted);
        }
    }

    qint64 previousEndMs = 0;
    for (int index = 0;
         index < plan->subtitles.cues.size()
         && plan->audio.sfx.size() < kMaxSfxCount;
         ++index) {
        const int category = categoriesByCue.value(index, -1);
        if (category < 0 || category >= resolved.size()) {
            continue;
        }
        const SubtitleCue &cue = plan->subtitles.cues.at(index);
        const ResolvedSfx &sfx = resolved.at(category);
        AudioCue audio;
        audio.kind = AudioKind::Sfx;
        audio.name = sfx.name;
        audio.url = sfx.url;
        audio.startMs = qMax<qint64>(0, cue.startMs + 30);
        if (audio.startMs < previousEndMs) {
            audio.startMs = previousEndMs + 40;
        }
        const qint64 length =
            qMax<qint64>(700, sfx.durationMs > 0 ? sfx.durationMs : 1200);
        audio.endMs = qMin(plan->durationMs, audio.startMs + length);
        if (audio.endMs - audio.startMs < 300) {
            continue;
        }
        audio.durationMs = sfx.durationMs;
        audio.volume = kSfxVolume;
        plan->audio.sfx.append(audio);
        previousEndMs = audio.endMs;
    }
    if (!plan->audio.sfx.isEmpty()) {
        addLog(StudioData::MixcutStage::Design,
               QStringLiteral("已选 %1 处音效").arg(plan->audio.sfx.size()),
               StudioData::Tone::Info);
    }
    return true;
}

void PipelineService::buildEffectsAndFilters(EditPlan *plan) const
{
    if (!plan || plan->durationMs <= 0) {
        return;
    }
    if (m_request.effects) {
        EffectCue effect;
        effect.title = effectTitleForStyle(plan->style);
        const SemanticSegment *segment =
            plan->semantics.segments.isEmpty()
            ? nullptr
            : &plan->semantics.segments.first();
        for (const SemanticSegment &candidate : plan->semantics.segments) {
            if (candidate.type == SegmentType::Hook) {
                segment = &candidate;
                break;
            }
        }
        qint64 start = segment ? segment->startMs + 300 : 300;
        start = qBound<qint64>(0, start, qMax<qint64>(0, plan->durationMs - 1500));
        effect.startMs = start;
        effect.endMs = qMin(plan->durationMs, start + 3000);
        if (effect.endMs - effect.startMs >= 1500) {
            plan->effects.append(effect);
        }
    }
    if (m_request.filters) {
        FilterCue filter;
        filter.title = filterTitleForStyle(plan->style);
        filter.startMs = 0;
        filter.endMs = plan->durationMs;
        filter.intensity = filterIntensityForStyle(plan->style);
        plan->filters.append(filter);
    }
}

/* ------------------------------------------------------------------ */
/* 5. 时间轴编排                                                        */
/* ------------------------------------------------------------------ */

bool PipelineService::finalizeTimeline(EditPlan *plan, QString *error)
{
    if (!plan) {
        return false;
    }
    setStage(StudioData::MixcutStage::Timeline,
             StudioData::StageStatus::Running, 20,
             QStringLiteral("编排视频、字幕与音频轨道"));
    // 最后再校验一次：主镜头必须从 0 连续覆盖到片尾。
    qint64 cursor = 0;
    bool coverageOk = true;
    for (const Shot &shot : plan->shots) {
        if (shot.startMs > cursor + 150) {
            coverageOk = false;
            break;
        }
        cursor = qMax(cursor, shot.endMs);
    }
    if (!coverageOk || cursor < plan->durationMs - 300) {
        addLog(StudioData::MixcutStage::Timeline,
               QStringLiteral("主镜头存在空档，正在本地补齐"),
               StudioData::Tone::Muted);
        plan->shots = normalizeMainShots(plan->shots, plan->library,
                                         plan->speech, plan->semantics,
                                         m_request.transitions);
    }
    setStage(StudioData::MixcutStage::Timeline,
             StudioData::StageStatus::Running, 55,
             QStringLiteral("写入 Edit Plan / Timeline JSON"));
    if (!savePlan(*plan, error)) {
        return false;
    }
    addLog(StudioData::MixcutStage::Timeline,
           QStringLiteral("时间轴：主画面 %1 段 · 画中画 %2 段 · 字幕 %3 条 · 音频 %4 条")
               .arg(plan->shots.size())
               .arg(plan->overlays.size())
               .arg(plan->subtitles.cues.size())
               .arg((plan->audio.bgm.url.isEmpty() ? 0 : 1)
                    + plan->audio.sfx.size() + 1),
           StudioData::Tone::Ok);
    addLog(StudioData::MixcutStage::Timeline,
           QStringLiteral("Timeline JSON 已保存：%1").arg(m_planPath),
           StudioData::Tone::Info);
    return true;
}

bool PipelineService::savePlan(const EditPlan &plan, QString *error)
{
    const QString planDir =
        QDir(m_request.cacheDir).filePath(QStringLiteral("plans"));
    if (!QDir().mkpath(planDir)) {
        if (error) {
            *error = QStringLiteral("无法创建剪辑计划目录：%1").arg(planDir);
        }
        return false;
    }
    const QString base = QStringLiteral("plan_%1_%2")
                             .arg(m_request.runId)
                             .arg(QDateTime::currentMSecsSinceEpoch());
    if (m_planPath.isEmpty()) {
        m_planPath = QDir(planDir).filePath(base + QStringLiteral(".json"));
    }
    QSaveFile file(m_planPath);
    if (!file.open(QIODevice::WriteOnly)) {
        if (error) {
            *error = QStringLiteral("无法写入剪辑计划：%1").arg(file.errorString());
        }
        return false;
    }
    file.write(QJsonDocument(plan.toJson()).toJson(QJsonDocument::Indented));
    if (!file.commit()) {
        if (error) {
            *error = QStringLiteral("保存剪辑计划失败。");
        }
        return false;
    }
    // 额外输出一份精简 Timeline JSON，便于其它工具消费。
    QSaveFile timelineFile(QDir(planDir).filePath(base
                                                  + QStringLiteral("_timeline.json")));
    if (timelineFile.open(QIODevice::WriteOnly)) {
        timelineFile.write(
            QJsonDocument(plan.timelineJson()).toJson(QJsonDocument::Indented));
        timelineFile.commit();
    }
    return true;
}

/* ------------------------------------------------------------------ */
/* 6. 合成                                                              */
/* ------------------------------------------------------------------ */

bool PipelineService::composeDraft(EditPlan *plan, CapCutClient *capcut,
                                   const QString &workDir, CapCutDraft *draft,
                                   QString *error)
{
    if (!plan || !capcut || !draft) {
        if (error) {
            *error = QStringLiteral("合成阶段参数无效。");
        }
        return false;
    }
    setStage(StudioData::MixcutStage::Render,
             StudioData::StageStatus::Running, 2,
             QStringLiteral("创建剪映草稿"));
    if (!capcut->createDraft(plan->canvas.width, plan->canvas.height, draft,
                             error)) {
        return false;
    }
    addLog(StudioData::MixcutStage::Render,
           QStringLiteral("已创建剪映草稿：%1×%2")
               .arg(plan->canvas.width)
               .arg(plan->canvas.height),
           StudioData::Tone::Info);

    if (!addMainTracks(plan, capcut, *draft, workDir, error)) {
        return false;
    }
    if (isCancelled()) {
        if (error) {
            *error = QStringLiteral("已取消生成");
        }
        return false;
    }
    setStage(StudioData::MixcutStage::Render,
             StudioData::StageStatus::Running, 58,
             QStringLiteral("写入画中画轨道"));
    if (!addOverlayTracks(plan, capcut, *draft, workDir, error)) {
        return false;
    }
    if (isCancelled()) {
        if (error) {
            *error = QStringLiteral("已取消生成");
        }
        return false;
    }
    setStage(StudioData::MixcutStage::Render,
             StudioData::StageStatus::Running, 70,
             QStringLiteral("写入字幕轨道"));
    if (!addSubtitleTracks(*plan, capcut, *draft, error)) {
        return false;
    }
    setStage(StudioData::MixcutStage::Render,
             StudioData::StageStatus::Running, 82,
             QStringLiteral("写入配音、音效与 BGM"));
    if (!addAudioTracks(*plan, capcut, *draft, error)) {
        return false;
    }
    setStage(StudioData::MixcutStage::Render,
             StudioData::StageStatus::Running, 90,
             QStringLiteral("写入特效与滤镜"));
    if (!addEffectTracks(*plan, capcut, *draft, error)) {
        return false;
    }
    return true;
}

bool PipelineService::addMainTracks(EditPlan *plan, CapCutClient *capcut,
                                    const CapCutDraft &draft,
                                    const QString &workDir, QString *error)
{
    if (!plan) {
        return false;
    }
    setStage(StudioData::MixcutStage::Render,
             StudioData::StageStatus::Running, 8,
             QStringLiteral("预处理主画面素材"));
    QHash<QString, QString> uploadedImages;
    QVector<CapCutVideo> videos;
    QVector<CapCutImage> images;
    QVector<qint64> imageDurations;
    int prepared = 0;
    const int total = plan->shots.size();

    for (int index = 0; index < plan->shots.size(); ++index) {
        if (isCancelled()) {
            if (error) {
                *error = QStringLiteral("已取消生成");
            }
            return false;
        }
        const Shot &shot = plan->shots.at(index);
        const MaterialClip *clip = plan->library.clipById(shot.clipId);
        if (!clip) {
            continue;
        }
        const QString nextClipId = index + 1 < plan->shots.size()
            ? plan->shots.at(index + 1).clipId
            : QString();
        const MaterialClip *nextClip =
            nextClipId.isEmpty() ? nullptr : plan->library.clipById(nextClipId);
        const bool nextContiguous = nextClip
            && qAbs(plan->shots.at(index + 1).startMs - shot.endMs) < 120;

        QString url;
        if (clip->isVideo()) {
            const QString preparedPath = QDir(workDir).filePath(
                QStringLiteral("main_%1.mp4").arg(index, 3, 10, QLatin1Char('0')));
            QString prepareError;
            if (!Oral::prepareCanvasVideo(
                    clip->path, shot.sourceInMs, shot.durationMs(),
                    plan->canvas.width, plan->canvas.height, plan->canvas.fps,
                    preparedPath, &prepareError)) {
                addLog(StudioData::MixcutStage::Render,
                       QStringLiteral("视频「%1」预处理失败，已跳过：%2")
                           .arg(clip->assetName, prepareError),
                       StudioData::Tone::Muted);
                continue;
            }
            if (!capcut->uploadFile(preparedPath, m_request.apiKey, &url,
                                    error)) {
                return false;
            }
            CapCutVideo video;
            video.url = url;
            video.width = plan->canvas.width;
            video.height = plan->canvas.height;
            video.startUs = shot.startMs * 1000;
            video.endUs = shot.endMs * 1000;
            video.volume = 0.0;
            if (m_request.transitions && nextClip && nextClip->isVideo()
                && nextContiguous && !shot.transition.isEmpty()) {
                video.transition = shot.transition;
                video.transitionDurationUs = shot.transitionMs * 1000;
            }
            videos.append(video);
        } else {
            const QString cacheKey = clip->id;
            url = uploadedImages.value(cacheKey);
            if (url.isEmpty()) {
                const QString preparedPath = QDir(workDir).filePath(
                    QStringLiteral("main_i_%1.jpg")
                        .arg(MediaUtils::sanitizeFileComponent(clip->id, 24)));
                QString prepareError;
                if (!Oral::prepareCanvasImage(
                        clip->path, plan->canvas.width, plan->canvas.height,
                        preparedPath, &prepareError)) {
                    addLog(StudioData::MixcutStage::Render,
                           QStringLiteral("图片「%1」预处理失败，已跳过：%2")
                               .arg(clip->assetName, prepareError),
                           StudioData::Tone::Muted);
                    continue;
                }
                if (!capcut->uploadFile(preparedPath, m_request.apiKey, &url,
                                        error)) {
                    return false;
                }
                uploadedImages.insert(cacheKey, url);
            }
            CapCutImage image;
            image.url = url;
            image.width = plan->canvas.width;
            image.height = plan->canvas.height;
            image.startUs = shot.startMs * 1000;
            image.endUs = shot.endMs * 1000;
            // 片头第一张图片不做淡入，避免配音已经开始但画面还是黑场。
            if (shot.startMs > 0) {
                image.inAnimation = QStringLiteral("渐显");
                image.inAnimationDurationUs = 300000;
            }
            image.outAnimation = QStringLiteral("渐隐");
            image.outAnimationDurationUs = 300000;
            if (m_request.transitions && nextClip && !nextClip->isVideo()
                && nextContiguous && !shot.transition.isEmpty()) {
                image.transition = shot.transition;
                image.transitionDurationUs = shot.transitionMs * 1000;
            }
            images.append(image);
            imageDurations.append(shot.durationMs());
        }
        ++prepared;
        if (prepared % 4 == 0 || prepared == total) {
            setStage(StudioData::MixcutStage::Render,
                     StudioData::StageStatus::Running,
                     8.0 + 34.0 * prepared / qMax(1, total),
                     QStringLiteral("准备主画面 %1/%2").arg(prepared).arg(total));
        }
    }

    // 视频轨在下、图片轨在上：两者时间不重叠，图片只覆盖自己的镜头窗口。
    if (!videos.isEmpty()) {
        QStringList segmentIds;
        if (!capcut->addVideos(draft, videos, 1.0, 1.0, 1.0, 0, 0,
                               &segmentIds, error)) {
            return false;
        }
        addLog(StudioData::MixcutStage::Render,
               QStringLiteral("主视频轨：%1 段").arg(videos.size()),
               StudioData::Tone::Ok);
    }
    if (!images.isEmpty()) {
        QStringList segmentIds;
        if (!capcut->addImages(draft, images, 1.0, 0, 0, &segmentIds,
                               error)) {
            return false;
        }
        addLog(StudioData::MixcutStage::Render,
               QStringLiteral("主图片轨：%1 张").arg(images.size()),
               StudioData::Tone::Ok);
        // 静态图片轻微推近，避免画面死板。
        QVector<CapCutKeyframe> keyframes;
        for (int index = 0;
             index < segmentIds.size() && index < imageDurations.size();
             ++index) {
            const qint64 durationUs = imageDurations.at(index) * 1000;
            keyframes.append({segmentIds.at(index), QStringLiteral("KFTypeScaleX"),
                              0, 1.0});
            keyframes.append({segmentIds.at(index), QStringLiteral("KFTypeScaleY"),
                              0, 1.0});
            keyframes.append({segmentIds.at(index), QStringLiteral("KFTypeScaleX"),
                              durationUs, 1.05});
            keyframes.append({segmentIds.at(index), QStringLiteral("KFTypeScaleY"),
                              durationUs, 1.05});
        }
        QString keyframeError;
        if (!keyframes.isEmpty()
            && !capcut->addKeyframes(draft, keyframes, &keyframeError)) {
            addLog(StudioData::MixcutStage::Render,
                   QStringLiteral("图片推近关键帧写入失败，画面保持静态：%1")
                       .arg(keyframeError),
                   StudioData::Tone::Muted);
        }
    }
    if (videos.isEmpty() && images.isEmpty()) {
        if (error) {
            *error = QStringLiteral("没有成功预处理的主画面素材。");
        }
        return false;
    }
    return true;
}

bool PipelineService::addOverlayTracks(EditPlan *plan, CapCutClient *capcut,
                                       const CapCutDraft &draft,
                                       const QString &workDir, QString *error)
{
    if (!plan || plan->overlays.isEmpty()) {
        return true;
    }
    // 按「位置 + 类型」分组，每组一次 add_videos / add_images 调用，
    // 请求级缩放与位移相同，因此天然形成一条同位置的画中画轨。
    struct GroupKey
    {
        PipPosition position = PipPosition::TopRight;
        MaterialKind kind = MaterialKind::Video;
    };
    QVector<GroupKey> groupKeys;
    QHash<int, QVector<int>> groups;
    for (int index = 0; index < plan->overlays.size(); ++index) {
        const MaterialClip *clip =
            plan->library.clipById(plan->overlays.at(index).clipId);
        if (!clip) {
            continue;
        }
        GroupKey key;
        key.position = plan->overlays.at(index).position;
        key.kind = clip->kind;
        int groupIndex = -1;
        for (int i = 0; i < groupKeys.size(); ++i) {
            if (groupKeys.at(i).position == key.position
                && groupKeys.at(i).kind == key.kind) {
                groupIndex = i;
                break;
            }
        }
        if (groupIndex < 0) {
            groupKeys.append(key);
            groupIndex = groupKeys.size() - 1;
        }
        groups[groupIndex].append(index);
    }

    int prepared = 0;
    for (int groupIndex = 0; groupIndex < groupKeys.size(); ++groupIndex) {
        const GroupKey key = groupKeys.at(groupIndex);
        int transformX = 0;
        int transformY = 0;
        overlayTransform(key.position, plan->canvas.width, plan->canvas.height,
                         &transformX, &transformY);
        QVector<CapCutVideo> videos;
        QVector<CapCutImage> images;
        QVector<qint64> imageDurations;
        for (int overlayIndex : groups.value(groupIndex)) {
            if (isCancelled()) {
                if (error) {
                    *error = QStringLiteral("已取消生成");
                }
                return false;
            }
            const Shot &overlay = plan->overlays.at(overlayIndex);
            const MaterialClip *clip =
                plan->library.clipById(overlay.clipId);
            if (!clip) {
                continue;
            }
            QString url;
            if (clip->isVideo()) {
                const QString preparedPath = QDir(workDir).filePath(
                    QStringLiteral("overlay_%1.mp4").arg(overlayIndex));
                QString prepareError;
                if (!Oral::prepareCanvasVideo(
                        clip->path, overlay.sourceInMs, overlay.durationMs(),
                        plan->canvas.width, plan->canvas.height,
                        plan->canvas.fps, preparedPath, &prepareError)) {
                    addLog(StudioData::MixcutStage::Render,
                           QStringLiteral("画中画「%1」预处理失败，已跳过：%2")
                               .arg(clip->assetName, prepareError),
                           StudioData::Tone::Muted);
                    continue;
                }
                if (!capcut->uploadFile(preparedPath, m_request.apiKey, &url,
                                        error)) {
                    return false;
                }
                CapCutVideo video;
                video.url = url;
                video.width = plan->canvas.width;
                video.height = plan->canvas.height;
                video.startUs = overlay.startMs * 1000;
                video.endUs = overlay.endMs * 1000;
                video.volume = 0.0;
                videos.append(video);
            } else {
                const QString preparedPath = QDir(workDir).filePath(
                    QStringLiteral("overlay_i_%1.jpg").arg(overlayIndex));
                QString prepareError;
                if (!Oral::prepareCanvasImage(
                        clip->path, plan->canvas.width, plan->canvas.height,
                        preparedPath, &prepareError)) {
                    addLog(StudioData::MixcutStage::Render,
                           QStringLiteral("画中画「%1」预处理失败，已跳过：%2")
                               .arg(clip->assetName, prepareError),
                           StudioData::Tone::Muted);
                    continue;
                }
                if (!capcut->uploadFile(preparedPath, m_request.apiKey, &url,
                                        error)) {
                    return false;
                }
                CapCutImage image;
                image.url = url;
                image.width = plan->canvas.width;
                image.height = plan->canvas.height;
                image.startUs = overlay.startMs * 1000;
                image.endUs = overlay.endMs * 1000;
                image.inAnimation = QStringLiteral("渐显");
                image.inAnimationDurationUs = 220000;
                image.outAnimation = QStringLiteral("渐隐");
                image.outAnimationDurationUs = 220000;
                images.append(image);
                imageDurations.append(overlay.durationMs());
            }
            ++prepared;
        }
        if (!videos.isEmpty()) {
            QString addError;
            if (!capcut->addVideos(draft, videos, kOverlayScale, kOverlayScale,
                                   1.0, transformX, transformY, nullptr,
                                   &addError)) {
                addLog(StudioData::MixcutStage::Render,
                       QStringLiteral("画中画视频轨写入失败，已跳过：%1")
                           .arg(addError),
                       StudioData::Tone::Muted);
            }
        }
        if (!images.isEmpty()) {
            QString addError;
            if (!capcut->addImages(draft, images, kOverlayScale, transformX,
                                   transformY, nullptr, &addError)) {
                addLog(StudioData::MixcutStage::Render,
                       QStringLiteral("画中画图片轨写入失败，已跳过：%1")
                           .arg(addError),
                       StudioData::Tone::Muted);
            }
        }
    }
    if (prepared > 0) {
        addLog(StudioData::MixcutStage::Render,
               QStringLiteral("画中画就绪：%1 处 · 缩放 %2")
                   .arg(prepared)
                   .arg(kOverlayScale),
               StudioData::Tone::Ok);
    }
    return true;
}

bool PipelineService::addSubtitleTracks(const EditPlan &plan,
                                        CapCutClient *capcut,
                                        const CapCutDraft &draft, QString *error)
{
    if (plan.subtitles.titleFx && !plan.subtitles.titleText.isEmpty()) {
        CapCutCaptionStyle style;
        style.fontSize = 14;
        style.textColor = QStringLiteral("#FFFFFF");
        style.borderColor = QStringLiteral("#000000");
        style.borderAlpha = 0.35;
        style.borderWidth = 20.0;
        style.alignment = 1;
        style.transformY = plan.canvas.height * 0.30;
        style.bold = true;
        style.hasBackground = true;
        style.backgroundColor = QStringLiteral("#000000");
        style.backgroundAlpha = 0.35;
        CapCutCaption title;
        title.startUs = plan.subtitles.titleStartMs * 1000;
        title.endUs = plan.subtitles.titleEndMs * 1000;
        title.text = plan.subtitles.titleText;
        title.inAnimation = QStringLiteral("渐显");
        title.inAnimationDurationUs = 200000;
        QString titleError;
        if (!capcut->addCaptions(draft, {title}, style, &titleError)) {
            addLog(StudioData::MixcutStage::Render,
                   QStringLiteral("标题花字写入失败，已跳过：%1").arg(titleError),
                   StudioData::Tone::Muted);
        }
    }
    if (plan.subtitles.cues.isEmpty()) {
        return true;
    }

    CapCutCaptionStyle style;
    style.alignment = 1;
    QVector<CapCutCaption> captions;
    captions.reserve(plan.subtitles.cues.size());
    const bool portrait = plan.canvas.height >= plan.canvas.width;
    switch (plan.subtitles.preset) {
    case SubtitlePreset::Keyword:
        style.fontSize = portrait ? 11 : 10;
        style.textColor = QStringLiteral("#FFFFFF");
        style.borderColor = QStringLiteral("#000000");
        style.borderAlpha = 0.35;
        style.borderWidth = 18.0;
        style.transformY = -plan.canvas.height * 0.40;
        break;
    case SubtitlePreset::Big:
        style.fontSize = portrait ? 16 : 14;
        style.textColor = QStringLiteral("#FFFFFF");
        style.borderColor = QStringLiteral("#000000");
        style.borderAlpha = 0.45;
        style.borderWidth = 26.0;
        style.transformY = -plan.canvas.height * 0.36;
        style.bold = true;
        break;
    case SubtitlePreset::Minimal:
        style.fontSize = portrait ? 9 : 8;
        style.textColor = QStringLiteral("#FFFFFF");
        style.hasShadow = true;
        style.transformY = -plan.canvas.height * 0.41;
        break;
    }
    for (const SubtitleCue &cue : plan.subtitles.cues) {
        CapCutCaption caption;
        caption.startUs = cue.startMs * 1000;
        caption.endUs = cue.endMs * 1000;
        caption.text = cue.text;
        caption.inAnimation = cue.animation;
        caption.inAnimationDurationUs = 180000;
        if (!cue.keywords.isEmpty()) {
            caption.keyword = cue.keywords.join(QLatin1Char('|'));
            caption.keywordColor = QStringLiteral("#FFD166");
            if (plan.subtitles.preset == SubtitlePreset::Keyword) {
                caption.keywordFontSize = portrait ? 13 : 12;
            }
        }
        captions.append(caption);
    }
    if (!capcut->addCaptions(draft, captions, style, error)) {
        return false;
    }
    addLog(StudioData::MixcutStage::Render,
           QStringLiteral("字幕轨：%1 条 · %2")
               .arg(captions.size())
               .arg(subtitlePresetLabel(plan.subtitles.preset)),
           StudioData::Tone::Ok);
    return true;
}

bool PipelineService::addAudioTracks(const EditPlan &plan,
                                     CapCutClient *capcut,
                                     const CapCutDraft &draft, QString *error)
{
    if (plan.ttsPath.isEmpty() || !QFileInfo::exists(plan.ttsPath)) {
        if (error) {
            *error = QStringLiteral("配音文件不存在，无法写入音频轨。");
        }
        return false;
    }
    QString voiceUrl;
    if (!capcut->uploadFile(plan.ttsPath, m_request.apiKey, &voiceUrl, error)) {
        return false;
    }
    CapCutAudio voice;
    voice.url = voiceUrl;
    voice.startUs = 0;
    voice.endUs = plan.durationMs * 1000;
    voice.durationUs = plan.durationMs * 1000;
    voice.volume = 1.0;
    if (!capcut->addAudios(draft, {voice}, error)) {
        return false;
    }
    addLog(StudioData::MixcutStage::Render,
           QStringLiteral("配音轨已写入 · %1")
               .arg(StudioData::formatElapsed(plan.durationMs)),
           StudioData::Tone::Ok);

    if (plan.audio.sfxEnabled && !plan.audio.sfx.isEmpty()) {
        QVector<CapCutAudio> sfx;
        for (const AudioCue &cue : plan.audio.sfx) {
            if (cue.url.isEmpty() || cue.endMs <= cue.startMs) {
                continue;
            }
            CapCutAudio audio;
            audio.url = cue.url;
            audio.startUs = cue.startMs * 1000;
            audio.endUs = cue.endMs * 1000;
            audio.durationUs = qMax<qint64>(0, cue.durationMs) * 1000;
            audio.volume = cue.volume;
            sfx.append(audio);
        }
        if (!sfx.isEmpty()) {
            QString sfxError;
            if (capcut->addAudios(draft, sfx, &sfxError)) {
                addLog(StudioData::MixcutStage::Render,
                       QStringLiteral("音效轨：%1 处").arg(sfx.size()),
                       StudioData::Tone::Info);
            } else {
                addLog(StudioData::MixcutStage::Render,
                       QStringLiteral("音效写入失败，已跳过：%1").arg(sfxError),
                       StudioData::Tone::Muted);
            }
        }
    }

    if (plan.audio.bgmEnabled && !plan.audio.bgm.url.isEmpty()) {
        QVector<CapCutAudio> bgm;
        const qint64 trackMs = plan.audio.bgm.durationMs;
        if (trackMs <= 0) {
            CapCutAudio audio;
            audio.url = plan.audio.bgm.url;
            audio.startUs = 0;
            audio.endUs = plan.durationMs * 1000;
            audio.volume = plan.audio.bgm.volume;
            bgm.append(audio);
        } else {
            // BGM 短于成片时循环铺满；每段首尾相接，避免同一批音频重叠。
            qint64 cursor = 0;
            while (cursor < plan.durationMs) {
                const qint64 end = qMin(plan.durationMs, cursor + trackMs);
                CapCutAudio audio;
                audio.url = plan.audio.bgm.url;
                audio.startUs = cursor * 1000;
                audio.endUs = end * 1000;
                audio.durationUs = (end - cursor) * 1000;
                audio.volume = plan.audio.bgm.volume;
                bgm.append(audio);
                cursor = end;
            }
        }
        QString bgmError;
        if (!bgm.isEmpty() && capcut->addAudios(draft, bgm, &bgmError)) {
            addLog(StudioData::MixcutStage::Render,
                   QStringLiteral("BGM 轨：%1 段 · 音量 %2")
                       .arg(bgm.size())
                       .arg(qRound(plan.audio.bgm.volume * 100)),
                   StudioData::Tone::Info);
        } else if (!bgm.isEmpty()) {
            addLog(StudioData::MixcutStage::Render,
                   QStringLiteral("BGM 写入失败，已跳过：%1").arg(bgmError),
                   StudioData::Tone::Muted);
        }
    }
    return true;
}

bool PipelineService::addEffectTracks(const EditPlan &plan,
                                      CapCutClient *capcut,
                                      const CapCutDraft &draft, QString *error)
{
    if (plan.filters.isEmpty() && plan.effects.isEmpty()) {
        return true;
    }
    if (!plan.effects.isEmpty()) {
        QVector<CapCutEffect> effects;
        for (const EffectCue &cue : plan.effects) {
            effects.append({cue.title, cue.startMs * 1000, cue.endMs * 1000});
        }
        QString effectError;
        if (capcut->addEffects(draft, effects, &effectError)) {
            addLog(StudioData::MixcutStage::Render,
                   QStringLiteral("视频特效：%1 个").arg(effects.size()),
                   StudioData::Tone::Info);
        } else {
            addLog(StudioData::MixcutStage::Render,
                   QStringLiteral("视频特效写入失败，已跳过：%1").arg(effectError),
                   StudioData::Tone::Muted);
        }
    }
    if (!plan.filters.isEmpty()) {
        QVector<CapCutFilter> filters;
        for (const FilterCue &cue : plan.filters) {
            filters.append(
                {cue.title, cue.startMs * 1000, cue.endMs * 1000, cue.intensity});
        }
        QString filterError;
        if (capcut->addFilters(draft, filters, &filterError)) {
            addLog(StudioData::MixcutStage::Render,
                   QStringLiteral("滤镜：%1").arg(plan.filters.first().title),
                   StudioData::Tone::Info);
        } else {
            addLog(StudioData::MixcutStage::Render,
                   QStringLiteral("滤镜写入失败，已跳过：%1").arg(filterError),
                   StudioData::Tone::Muted);
        }
    }
    Q_UNUSED(error);
    return true;
}

bool PipelineService::renderAndDownload(const EditPlan &plan,
                                        CapCutClient *capcut,
                                        const CapCutDraft &draft,
                                        const QString &outputPath,
                                        QString *draftPath, QString *error)
{
    if (!capcut || !draftPath) {
        if (error) {
            *error = QStringLiteral("渲染参数无效。");
        }
        return false;
    }
    setStage(StudioData::MixcutStage::Render,
             StudioData::StageStatus::Running, 92,
             QStringLiteral("提交云渲染"));
    if (!capcut->genVideo(draft, m_request.apiKey, error)) {
        return false;
    }
    addLog(StudioData::MixcutStage::Render,
           QStringLiteral("已提交云渲染：%1×%2 · %3fps · %4")
               .arg(plan.canvas.width)
               .arg(plan.canvas.height)
               .arg(plan.canvas.fps)
               .arg(StudioData::formatElapsed(plan.durationMs)),
           StudioData::Tone::Info);

    QElapsedTimer renderClock;
    renderClock.start();
    int lastReported = -1;
    bool completed = false;
    while (renderClock.elapsed() < kRenderTimeoutMs) {
        if (isCancelled()) {
            if (error) {
                *error = QStringLiteral("已取消生成");
            }
            return false;
        }
        CapCutVideoStatus status;
        QString statusError;
        if (!capcut->genVideoStatus(draft, &status, &statusError)) {
            addLog(StudioData::MixcutStage::Render,
                   QStringLiteral("查询渲染状态失败：%1").arg(statusError),
                   StudioData::Tone::Muted);
        } else if (status.status == QLatin1String("failed")) {
            if (error) {
                *error = QStringLiteral("云渲染失败：%1")
                             .arg(status.errorMessage.isEmpty()
                                      ? QStringLiteral("未知原因")
                                      : status.errorMessage);
            }
            return false;
        } else if (status.status == QLatin1String("completed")) {
            completed = true;
            break;
        } else {
            const int progress = qBound(0, status.progress, 100);
            setStage(StudioData::MixcutStage::Render,
                     StudioData::StageStatus::Running,
                     92.0 + progress * 0.03,
                     QStringLiteral("云渲染 %1%").arg(progress));
            m_run.renderedFrames =
                qMin(m_run.totalFrames,
                     int(qint64(m_run.totalFrames) * progress / 100));
            if (progress >= lastReported + 20) {
                lastReported = progress;
                addLog(StudioData::MixcutStage::Render,
                       QStringLiteral("云渲染 %1% · 已渲染 %2/%3 帧")
                           .arg(progress)
                           .arg(m_run.renderedFrames)
                           .arg(m_run.totalFrames),
                       StudioData::Tone::Info);
            }
        }
        QThread::msleep(kRenderPollMs);
    }
    if (!completed) {
        if (error) {
            *error = QStringLiteral("云渲染超时（超过 30 分钟），请稍后在剪映草稿中查看。");
        }
        return false;
    }
    if (isCancelled()) {
        if (error) {
            *error = QStringLiteral("已取消生成");
        }
        return false;
    }

    CapCutVideoStatus finalStatus;
    if (!capcut->genVideoStatus(draft, &finalStatus, error)) {
        return false;
    }
    if (finalStatus.videoUrl.isEmpty()) {
        if (error) {
            *error = QStringLiteral("云渲染完成但未返回成片地址。");
        }
        return false;
    }

    setStage(StudioData::MixcutStage::Render,
             StudioData::StageStatus::Running, 95,
             QStringLiteral("下载成片"));
    addLog(StudioData::MixcutStage::Render,
           QStringLiteral("渲染完成，正在下载成片…"), StudioData::Tone::Ok);
    QString downloadError;
    HttpClient downloadHttp;
    const bool downloaded = downloadHttp.downloadToFile(
        QUrl(finalStatus.videoUrl), outputPath, &downloadError,
        30 * 60 * 1000,
        [this](qint64 received, qint64 total) {
            const int percent = total > 0 ? int(received * 100 / total) : 0;
            setStage(StudioData::MixcutStage::Render,
                     StudioData::StageStatus::Running,
                     95.0 + percent * 0.02,
                     QStringLiteral("下载成片 %1%").arg(percent));
        },
        [this] { return isCancelled(); });
    if (!downloaded) {
        if (error) {
            *error = QStringLiteral("成片下载失败：%1").arg(downloadError);
        }
        return false;
    }
    addLog(StudioData::MixcutStage::Render,
           QStringLiteral("成片已保存：%1 · %2")
               .arg(outputPath, Oral::formatFileSize(QFileInfo(outputPath).size())),
           StudioData::Tone::Ok);
    m_run.renderedFrames = m_run.totalFrames;

    if (!m_request.draftRoot.isEmpty()) {
        setStage(StudioData::MixcutStage::Render,
                 StudioData::StageStatus::Running, 97,
                 QStringLiteral("下载剪映草稿"));
        QString localDraftPath;
        QString draftError;
        const QString title =
            plan.semantics.title.isEmpty() ? QStringLiteral("智能混剪")
                                           : plan.semantics.title;
        if (capcut->downloadDraft(
                m_draftId, title, m_request.draftRoot, &localDraftPath,
                &draftError,
                [this](int percent, const QString &detail) {
                    setStage(StudioData::MixcutStage::Render,
                             StudioData::StageStatus::Running,
                             97.0 + percent * 0.02, detail);
                })) {
            *draftPath = localDraftPath;
            addLog(StudioData::MixcutStage::Render,
                   QStringLiteral("剪映草稿已写入：%1").arg(localDraftPath),
                   StudioData::Tone::Ok);
        } else {
            addLog(StudioData::MixcutStage::Render,
                   QStringLiteral("剪映草稿下载失败，成片可正常使用：%1")
                       .arg(draftError),
                   StudioData::Tone::Muted);
        }
    }
    return true;
}

/* ------------------------------------------------------------------ */
/* 结果                                                                 */
/* ------------------------------------------------------------------ */

StudioData::MixcutResult
PipelineService::makeResult(const EditPlan &plan, const QString &videoPath,
                            const QString &draftPath,
                            const QString &previewPath) const
{
    StudioData::MixcutResult result;
    const qint64 now = QDateTime::currentMSecsSinceEpoch();
    const StudioData::MixcutStyleMeta &meta =
        StudioData::mixcutStyleMeta(plan.style);
    const StudioData::MixcutVoice voice =
        StudioData::getMixcutVoice(plan.voiceId);
    result.id = QStringLiteral("mixcut-%1").arg(now);
    result.projectName = meta.projectName + QStringLiteral(" · 智能混剪");
    result.title = plan.semantics.title.trimmed();
    if (result.title.isEmpty()) {
        result.title = meta.title;
    }
    result.description = plan.semantics.description.trimmed();
    if (result.description.isEmpty()) {
        result.description = meta.description;
    }
    result.tags = plan.semantics.tags;
    if (result.tags.isEmpty()) {
        result.tags = meta.tags;
    }
    result.captionTitle = meta.captionTitle;
    result.voiceId = plan.voiceId;
    result.style = plan.style;
    result.ratio = plan.canvas.ratio;
    result.durationLabel = StudioData::formatElapsed(plan.durationMs);
    result.fileSize = Oral::formatFileSize(QFileInfo(videoPath).size());
    result.generatedAt = now;
    result.scriptPrompt = plan.scriptPrompt;
    result.script = plan.script;
    result.sceneCount = plan.shots.size() + plan.overlays.size();
    result.subtitleCount = plan.subtitles.cues.size();
    result.sfxCount = plan.audio.sfx.size();
    result.bgm = plan.audio.bgm.url.isEmpty()
        ? (plan.audio.bgmEnabled ? QStringLiteral("未获取")
                                 : QStringLiteral("未启用"))
        : plan.audio.bgm.name;
    result.draftGenerated = !draftPath.isEmpty();
    result.filePath = videoPath;
    result.previewPath = previewPath;
    result.draftPath = draftPath;
    result.draftId = m_draftId;
    result.editPlanPath = m_planPath;
    result.videoCodec = Oral::videoCodecName(videoPath);
    result.outputBoth = true;

    int videoCount = 0;
    int imageCount = 0;
    qint64 videoDuration = 0;
    for (StudioData::MixcutMaterial material : m_request.project.materials) {
        bool matched = false;
        for (const MaterialAsset &asset : plan.library.assets) {
            if (asset.id == material.id || asset.path == material.filePath) {
                material.durationMs =
                    asset.kind == MaterialKind::Video ? asset.durationMs : 0;
                material.resolution = QStringLiteral("%1×%2")
                                          .arg(asset.width)
                                          .arg(asset.height);
                material.shots = asset.clips.size();
                material.scenes = asset.scenes.mid(0, 4);
                material.topics = asset.tags.mid(0, 5);
                if (material.size.trimmed().isEmpty()) {
                    material.size =
                        Oral::formatFileSize(QFileInfo(asset.path).size());
                }
                matched = true;
                break;
            }
        }
        // 未能探测/分析的素材不进入历史，避免重新混剪时反复失败。
        if (matched) {
            result.materials.append(material);
        }
    }
    for (const StudioData::MixcutMaterial &material : result.materials) {
        if (material.kind == StudioData::MixcutMaterialKind::Video) {
            ++videoCount;
            videoDuration += material.durationMs;
        } else {
            ++imageCount;
        }
    }
    result.sourceLabel =
        QStringLiteral("素材 · %1 个（%2 视频 / %3 图片）· %4 个镜头")
            .arg(result.materials.size())
            .arg(videoCount)
            .arg(imageCount)
            .arg(plan.library.clipCount());
    if (result.materials.size() == 1) {
        result.sourceLabel = QStringLiteral("%1 · %2 · %3 个镜头")
                                 .arg(result.materials.first().name)
                                 .arg(result.materials.first().kind
                                          == StudioData::MixcutMaterialKind::Video
                                      ? StudioData::formatElapsed(videoDuration)
                                      : result.materials.first().resolution)
                                 .arg(plan.library.clipCount());
    }

    int transitionCount = 0;
    for (const Shot &shot : plan.shots) {
        if (!shot.transition.isEmpty()) {
            ++transitionCount;
        }
    }
    result.highlights = {
        {QStringLiteral("配音音色"),
         QStringLiteral("%1 · %2").arg(voice.name, voice.label)},
        {QStringLiteral("镜头匹配"),
         QStringLiteral("%1 个 · %2 处画中画")
             .arg(plan.shots.size())
             .arg(plan.overlays.size())},
        {QStringLiteral("智能字幕"),
         QStringLiteral("%1 条 · %2")
             .arg(result.subtitleCount)
             .arg(subtitlePresetLabel(plan.subtitles.preset))},
        {QStringLiteral("卡点音效"), QStringLiteral("%1 处").arg(result.sfxCount)},
        {QStringLiteral("智能包装"),
         QStringLiteral("%1 个转场%2")
             .arg(transitionCount)
             .arg(plan.filters.isEmpty()
                      ? QString()
                      : QStringLiteral(" · 滤镜「%1」")
                            .arg(plan.filters.first().title))},
        {QStringLiteral("BGM"), result.bgm},
    };
    return result;
}

} // namespace Mixcut
