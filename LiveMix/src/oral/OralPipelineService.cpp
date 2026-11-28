// 文件作用：智能口播流水线实现。
// 本文件按「理解 → 生成 → 合成」串起 ASR、DeepSeek 语义/素材/镜头分析、
// 字幕与音频系统、capcut-mate 落轨与云渲染。与直播切片服务没有任何调用关系。

#include "OralPipelineService.h"

#include "../api/HttpClient.h"
#include "../core/MediaUtils.h"
#include "../media/FfmpegUtils.h"
#include "OralAsrClient.h"
#include "OralAudioAssetsClient.h"
#include "OralCapCutClient.h"
#include "OralCoverClient.h"
#include "OralCoverPrompts.h"
#include "OralEditPlanBuilder.h"
#include "OralLlmClient.h"
#include "OralMediaUtils.h"
#include "OralSfxCatalog.h"

#include <QCryptographicHash>
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
#include <QThread>
#include <QUrl>
#include <QtMath>

#include <algorithm>

namespace {

constexpr int kStageCount = 4;
constexpr double kSfxVolume = 0.1;
constexpr double kBgmVolume = 0.12;
constexpr int kMaxSfxCount = 8;
constexpr qint64 kRenderPollMs = 4000;
constexpr qint64 kRenderTimeoutMs = 30 * 60 * 1000;
// 产品指定的封面时间线：[0, 10000] 微秒；领域结构统一用毫秒。
constexpr qint64 kCoverEndMs = 10;

QString sha1Hex(const QString &text)
{
    return QString::fromLatin1(
        QCryptographicHash::hash(text.toUtf8(), QCryptographicHash::Sha1)
            .toHex());
}

// 指纹绑定文件路径、大小与修改时间；源文件或任一素材变化都会让缓存失效。
QString fileFingerprint(const QString &path)
{
    const QFileInfo info(path);
    if (!info.exists()) {
        return path + QStringLiteral("|missing");
    }
    return QStringLiteral("%1|%2|%3")
        .arg(info.absoluteFilePath())
        .arg(info.size())
        .arg(info.lastModified().toMSecsSinceEpoch());
}

Oral::SubtitlePreset presetFromStudio(StudioData::OralSubtitleStyle style)
{
    switch (style) {
    case StudioData::OralSubtitleStyle::Keyword:
        return Oral::SubtitlePreset::Keyword;
    case StudioData::OralSubtitleStyle::Big:
        return Oral::SubtitlePreset::Big;
    case StudioData::OralSubtitleStyle::Minimal:
        break;
    }
    return Oral::SubtitlePreset::Minimal;
}

QString subtitlePresetName(Oral::SubtitlePreset preset)
{
    switch (preset) {
    case Oral::SubtitlePreset::Keyword:
        return QStringLiteral("智能关键词");
    case Oral::SubtitlePreset::Big:
        return QStringLiteral("大字强调");
    case Oral::SubtitlePreset::Minimal:
        return QStringLiteral("极简白字");
    }
    return QStringLiteral("智能关键词");
}

// 成片名取自语义标题前 12 字，与结果页「项目名 · 口播包装」保持一致。
QString projectNameFor(const Oral::EditPlan &plan)
{
    QString title = plan.semantics.title.trimmed();
    if (title.isEmpty()) {
        title = QFileInfo(plan.sourcePath).completeBaseName();
    }
    title = title.left(12);
    if (title.isEmpty()) {
        title = QStringLiteral("口播成片");
    }
    return title + QStringLiteral(" · 口播包装");
}

// 与 OralPage 的 fileName 规则一致：把「 · 」替换成下划线，方便文件系统使用。
QString safeFileNameFor(const Oral::EditPlan &plan)
{
    QString fileName = projectNameFor(plan);
    fileName.replace(QStringLiteral(" · "), QStringLiteral("_"));
    fileName = MediaUtils::sanitizeFileComponent(fileName, 48);
    if (fileName.isEmpty()) {
        fileName = QStringLiteral("口播成片");
    }
    return fileName;
}

// 第一步的输入：把口播全文与画幅等补充信息交给提示词 1。
QString coverOralContentFor(const Oral::EditPlan &plan)
{
    QString text = plan.speech.text.trimmed();
    if (text.isEmpty()) {
        QStringList utterances;
        for (const Oral::Utterance &utterance : plan.speech.utterances) {
            const QString line = utterance.text.trimmed();
            if (!line.isEmpty()) {
                utterances.append(line);
            }
        }
        text = utterances.join(QLatin1Char('\n'));
    }
    if (text.size() > 4000) {
        text = text.left(4000) + QStringLiteral("…");
    }

    QStringList lines;
    lines.append(QStringLiteral("口播文案："));
    lines.append(text);
    lines.append(QString());
    lines.append(QStringLiteral("可选补充："));
    lines.append(QStringLiteral("画幅：%1").arg(plan.canvas.ratio));
    if (!plan.semantics.title.trimmed().isEmpty()) {
        lines.append(QStringLiteral("视频标题：")
                     + plan.semantics.title.trimmed());
    }
    lines.append(QStringLiteral(
        "首帧图将在第二步作为图生图参考图提供；请严格按“以参考图人物为主体”"
        "的约束撰写提示词2。"));
    return lines.join(QLatin1Char('\n'));
}

// 提示词 1 调用失败时的本地兜底：至少保证封面仍能生成。
QString fallbackCoverPromptFor(const Oral::EditPlan &plan)
{
    QString title = plan.semantics.title.trimmed();
    if (title.isEmpty()) {
        title = plan.subtitles.titleText.trimmed();
    }
    if (title.isEmpty()) {
        title = QFileInfo(plan.sourcePath).completeBaseName();
    }
    title = title.simplified();
    if (title.size() > 14) {
        title = title.left(14);
    }

    QStringList points;
    for (const Oral::SemanticSegment &segment : plan.semantics.segments) {
        const QString point = segment.title.trimmed();
        if (!point.isEmpty() && !points.contains(point)) {
            points.append(point);
        }
        if (points.size() >= 3) {
            break;
        }
    }
    QString core = points.join(QStringLiteral("、"));
    if (core.isEmpty()) {
        core = plan.semantics.description.trimmed();
    }
    if (core.size() > 60) {
        core = core.left(60);
    }

    QStringList keywords;
    for (const QString &keyword : plan.semantics.keywords) {
        const QString clean = keyword.trimmed();
        if (!clean.isEmpty() && !keywords.contains(clean)) {
            keywords.append(clean);
        }
        if (keywords.size() >= 6) {
            break;
        }
    }

    const QString orientation = plan.canvas.width >= plan.canvas.height
        ? QStringLiteral("横版 16:9")
        : QStringLiteral("竖版 9:16");
    QStringList lines;
    lines.append(QStringLiteral("以参考图中的真人为海报主体，保留人物的外貌、发型、"
                                "服装与所处场景，生成一张")
                 + orientation + QStringLiteral("的短视频封面海报。"));
    if (!title.isEmpty()) {
        lines.append(QStringLiteral("视频主题：") + title
                     + QStringLiteral("。"));
    }
    if (!core.isEmpty()) {
        lines.append(QStringLiteral("内容要点：") + core
                     + QStringLiteral("。"));
    }
    if (!keywords.isEmpty()) {
        lines.append(QStringLiteral("关键词：")
                     + keywords.join(QStringLiteral("、"))
                     + QStringLiteral("。"));
    }
    lines.append(QStringLiteral("画面要求：真实摄影质感，主体清晰突出，半身或近景"
                                "构图，背景干净并适度虚化，光线通透有层次，色调"
                                "与参考图协调，具有吸引点击的视觉冲击力。"));
    if (title.isEmpty()) {
        lines.append(QStringLiteral("文字要求：画面中不要出现任何文字、水印、"
                                    "Logo、二维码、拼贴或多分格。"));
    } else {
        lines.append(QStringLiteral("文字要求：只排入一行清晰醒目的主标题「")
                     + title
                     + QStringLiteral("」，字体现代、排版简洁；不要出现其他文字、"
                                      "水印、Logo、二维码、拼贴或多分格。"));
    }
    return lines.join(QLatin1Char('\n'));
}

// BGM 短于成片时按整首长度循环铺满。
QVector<Oral::AudioCue> buildBgmSegments(const Oral::AudioCue &bgm,
                                         qint64 durationMs)
{
    QVector<Oral::AudioCue> segments;
    if (bgm.url.isEmpty() || bgm.durationMs <= 0 || durationMs <= 0) {
        return segments;
    }
    qint64 cursor = 0;
    while (cursor < durationMs) {
        Oral::AudioCue cue = bgm;
        cue.startMs = cursor;
        cue.endMs = qMin(durationMs, cursor + bgm.durationMs);
        segments.append(cue);
        cursor = cue.endMs;
    }
    return segments;
}

// 音效时间窗重叠检测：同一时间段只保留先出现的音效。
bool overlapsAny(const Oral::AudioCue &candidate,
                 const QVector<Oral::AudioCue> &existing)
{
    for (const Oral::AudioCue &cue : existing) {
        if (candidate.startMs < cue.endMs && cue.startMs < candidate.endMs) {
            return true;
        }
    }
    return false;
}

QString materialPreparedName(const QString &id, const QString &suffix)
{
    return MediaUtils::sanitizeFileComponent(id, 40) + suffix;
}

// 口播原片上传缓存：同一源文件在 6 天内重新包装时复用对象存储地址，
// 避免重复上传。URL 失效时 add_videos 会失败，调用方会重新上传并覆盖缓存。
constexpr qint64 kUploadCacheTtlMs = 6LL * 24 * 60 * 60 * 1000;

QString sourceUploadCachePath(const QString &cacheDir)
{
    return QDir(cacheDir).filePath(QStringLiteral("source_upload.json"));
}

bool readSourceUploadCache(const QString &cacheDir, const QString &fingerprint,
                           QString *url)
{
    QFile file(sourceUploadCachePath(cacheDir));
    if (!file.open(QIODevice::ReadOnly)) {
        return false;
    }
    const QJsonObject root = QJsonDocument::fromJson(file.readAll()).object();
    if (root.value(QStringLiteral("fingerprint")).toString() != fingerprint) {
        return false;
    }
    const qint64 savedAt =
        qint64(root.value(QStringLiteral("savedAt")).toDouble());
    if (savedAt <= 0
        || QDateTime::currentMSecsSinceEpoch() - savedAt > kUploadCacheTtlMs) {
        return false;
    }
    const QString cached = root.value(QStringLiteral("url")).toString();
    if (cached.isEmpty()) {
        return false;
    }
    if (url) {
        *url = cached;
    }
    return true;
}

void writeSourceUploadCache(const QString &cacheDir, const QString &fingerprint,
                            const QString &url)
{
    QJsonObject root;
    root.insert(QStringLiteral("fingerprint"), fingerprint);
    root.insert(QStringLiteral("url"), url);
    root.insert(QStringLiteral("savedAt"),
                double(QDateTime::currentMSecsSinceEpoch()));
    QSaveFile file(sourceUploadCachePath(cacheDir));
    if (!file.open(QIODevice::WriteOnly)) {
        return;
    }
    file.write(QJsonDocument(root).toJson(QJsonDocument::Compact));
    file.commit();
}

} // namespace

namespace Oral {

PipelineService::PipelineService(const PipelineRequest &request, QObject *parent)
    : QObject(parent)
    , m_request(request)
{
    // 跨线程信号使用自定义结构，先注册元类型。
    qRegisterMetaType<StudioData::OralRun>("StudioData::OralRun");
    qRegisterMetaType<StudioData::OralResult>("StudioData::OralResult");
}

void PipelineService::cancel()
{
    m_cancelled.store(true);
}

void PipelineService::initRun()
{
    m_startedAtMs = QDateTime::currentMSecsSinceEpoch();
    m_run = StudioData::OralRun();
    m_run.runId = m_request.runId;
    m_run.done = false;
    m_run.stage = StudioData::OralStage::Analyze;
    const QStringList labels = {
        QStringLiteral("理解口播"),
        QStringLiteral("匹配素材"),
        QStringLiteral("智能包装"),
        QStringLiteral("渲染成片"),
    };
    const QVector<StudioData::OralStage> stages = {
        StudioData::OralStage::Analyze,
        StudioData::OralStage::Match,
        StudioData::OralStage::Compose,
        StudioData::OralStage::Render,
    };
    for (int index = 0; index < kStageCount; ++index) {
        StudioData::OralStageState stage;
        stage.key = stages.at(index);
        stage.label = labels.at(index);
        stage.status = StudioData::StageStatus::Waiting;
        stage.progress = 0.0;
        m_run.stages.append(stage);
    }
}

void PipelineService::setStage(StudioData::OralStage stage,
                               StudioData::StageStatus status, double progress,
                               const QString &detail)
{
    for (StudioData::OralStageState &state : m_run.stages) {
        if (state.key != stage) {
            continue;
        }
        state.status = status;
        state.progress = qBound(0.0, progress, 100.0);
        if (!detail.isEmpty()) {
            state.detail = detail;
        }
        if (status == StudioData::StageStatus::Running) {
            m_run.stage = stage;
        }
        break;
    }
    updateOverallProgress();
    emitRun();
}

void PipelineService::addLog(StudioData::OralStage stage, const QString &text,
                             StudioData::Tone tone)
{
    StudioData::OralLogEntry entry;
    entry.id = QStringLiteral("%1-%2").arg(m_run.runId).arg(m_run.logs.size());
    entry.stage = stage;
    entry.atMs = QDateTime::currentMSecsSinceEpoch() - m_startedAtMs;
    entry.text = text;
    entry.tone = tone;
    m_run.logs.append(entry);
    emitRun();
}

void PipelineService::updateOverallProgress()
{
    // 四个阶段在总进度中的权重；渲染通常最耗时，给 30%。
    static const double weights[kStageCount] = {0.25, 0.20, 0.25, 0.30};
    double overall = 0.0;
    for (int index = 0; index < m_run.stages.size() && index < kStageCount;
         ++index) {
        overall += weights[index] * m_run.stages.at(index).progress / 100.0;
    }
    m_run.progress = qBound(0.0, overall * 100.0, 100.0);
    m_run.elapsedMs = QDateTime::currentMSecsSinceEpoch() - m_startedAtMs;
    if (m_run.progress > 3.0) {
        const double remaining =
            m_run.elapsedMs * (100.0 - m_run.progress) / m_run.progress;
        m_run.etaSeconds = qBound(0, int(remaining / 1000.0), 3600);
    } else {
        m_run.etaSeconds = 0;
    }
}

void PipelineService::emitRun()
{
    m_run.elapsedMs = QDateTime::currentMSecsSinceEpoch() - m_startedAtMs;
    emit runUpdated(m_run);
}

/* ------------------------------------------------------------------ */
/* 主流程                                                               */
/* ------------------------------------------------------------------ */

void PipelineService::run()
{
    initRun();
    emitRun();

    HttpClient http;
    AsrClient asr(&http);
    LlmClient llm(&http);
    CapCutClient capcut(&http);
    AudioAssetsClient assets(&http);

    EditPlan plan;
    bool fromCache = false;
    QString error;

    if (isCancelled()) {
        emit finished(false, QStringLiteral("已取消生成"), StudioData::OralResult());
        return;
    }

    setStage(StudioData::OralStage::Analyze, StudioData::StageStatus::Running, 1,
             QStringLiteral("读取口播原片"));
    if (!ensureAnalysis(&plan, &fromCache, &asr, &llm, &capcut, &error)) {
        addLog(StudioData::OralStage::Analyze, error, StudioData::Tone::Muted);
        emit finished(false, error, StudioData::OralResult());
        return;
    }
    if (isCancelled()) {
        emit finished(false, QStringLiteral("已取消生成"), StudioData::OralResult());
        return;
    }

    // 生成阶段：先固化字幕、音频与时间线，再开始落轨。
    setStage(StudioData::OralStage::Compose, StudioData::StageStatus::Running, 2,
             QStringLiteral("生成统一剪辑计划"));
    addLog(StudioData::OralStage::Compose,
           QStringLiteral("字幕系统：%1 条 · %2")
               .arg(plan.subtitles.cues.size())
               .arg(subtitlePresetName(plan.subtitles.preset)),
           StudioData::Tone::Ai);
    if (!buildAudioPlan(plan, &plan.audio, &assets, &llm, &error)) {
        addLog(StudioData::OralStage::Compose, error, StudioData::Tone::Muted);
        emit finished(false, error, StudioData::OralResult());
        return;
    }
    setStage(StudioData::OralStage::Compose, StudioData::StageStatus::Running, 15,
             QStringLiteral("剪辑计划已就绪"));
    addLog(StudioData::OralStage::Compose,
           QStringLiteral("已生成 Edit Plan / Timeline JSON"), StudioData::Tone::Ok);

    if (!QDir().mkpath(m_request.cacheDir)
        || !QDir().mkpath(QDir(m_request.cacheDir).filePath(QStringLiteral("plans")))) {
        const QString message = QStringLiteral("无法创建智能口播缓存目录。");
        emit finished(false, message, StudioData::OralResult());
        return;
    }
    m_editPlanPath = QDir(m_request.cacheDir)
                         .filePath(QStringLiteral("plans/plan_%1_%2.json")
                                       .arg(m_request.runId)
                                       .arg(QDateTime::currentMSecsSinceEpoch()));
    const auto savePlan = [this, &plan](QString *saveError) {
        QSaveFile file(m_editPlanPath);
        if (!file.open(QIODevice::WriteOnly)) {
            if (saveError) {
                *saveError = QStringLiteral("无法写入剪辑计划：%1")
                                 .arg(file.errorString());
            }
            return false;
        }
        file.write(QJsonDocument(plan.toJson()).toJson(QJsonDocument::Indented));
        if (!file.commit()) {
            if (saveError) {
                *saveError = QStringLiteral("保存剪辑计划失败。");
            }
            return false;
        }
        return true;
    };
    QString planError;
    if (!savePlan(&planError)) {
        emit finished(false, planError, StudioData::OralResult());
        return;
    }

    // 合成阶段：创建草稿、上传素材、写入轨道。
    CapCutDraft draft;
    if (!composeDraft(&plan, &capcut, &http, m_workDir, &draft, &error)) {
        addLog(StudioData::OralStage::Compose, error, StudioData::Tone::Muted);
        emit finished(false, error, StudioData::OralResult());
        return;
    }
    m_draftId = draft.id;
    setStage(StudioData::OralStage::Compose, StudioData::StageStatus::Done, 100,
             QStringLiteral("%1 条字幕 · %2 处 B-Roll")
                 .arg(plan.subtitles.cues.size())
                 .arg(plan.shots.shots.size()));
    addLog(StudioData::OralStage::Compose,
           QStringLiteral("字幕、画面与音频轨道对齐完成"), StudioData::Tone::Ok);
    // 上传完成后把真实 URL 写回计划，7 天内重新合成可复用素材。
    savePlan(nullptr);
    if (isCancelled()) {
        emit finished(false, QStringLiteral("已取消生成"), StudioData::OralResult());
        return;
    }

    // 渲染阶段：云渲染 + 下载成片（可选下载剪映草稿）。
    const QString fileName = safeFileNameFor(plan);
    const QString outputPath =
        QDir(m_request.outputDir).filePath(fileName + QStringLiteral(".mp4"));
    QString draftPath;
    if (!renderAndDownload(plan, &capcut, draft, outputPath, &draftPath, &error)) {
        addLog(StudioData::OralStage::Render, error, StudioData::Tone::Muted);
        emit finished(false, error, StudioData::OralResult());
        return;
    }

    // 结果页预览：抽取成片 0 秒帧（即刚写入的封面海报）缓存到本地。
    setStage(StudioData::OralStage::Render, StudioData::StageStatus::Running, 97,
             QStringLiteral("生成首帧预览"));
    QString previewPath;
    const QString posterPreviewPath =
        FfmpegUtils::posterFrameCachePath(outputPath);
    QString previewError;
    if (FfmpegUtils::extractThumbnail(outputPath, 0, posterPreviewPath,
                                      &previewError)) {
        previewPath = posterPreviewPath;
    } else {
        // 首帧截取失败时退而使用本地封面海报，结果页仍能显示封面预览。
        if (!plan.cover.localPath.isEmpty()
            && QFileInfo::exists(plan.cover.localPath)) {
            previewPath = plan.cover.localPath;
        }
        addLog(StudioData::OralStage::Render,
               QStringLiteral("成片首帧预览生成失败：%1").arg(previewError),
               StudioData::Tone::Muted);
    }

    setStage(StudioData::OralStage::Render, StudioData::StageStatus::Done, 100,
             QStringLiteral("%1×%2 · %3fps")
                 .arg(plan.canvas.width)
                 .arg(plan.canvas.height)
                 .arg(plan.canvas.fps));
    m_run.done = true;
    m_run.progress = 100.0;
    emitRun();

    const StudioData::OralResult result =
        makeResult(plan, outputPath, draftPath, previewPath);
    QString message = QStringLiteral("智能口播成片已生成并自动下载");
    if (!draftPath.isEmpty()) {
        message += QStringLiteral("，剪映草稿已写入本地目录");
    }
    emit finished(true, message, result);
}

/* ------------------------------------------------------------------ */
/* 1. 理解阶段                                                          */
/* ------------------------------------------------------------------ */

QString PipelineService::analysisCacheKey() const
{
    QStringList parts;
    parts.append(fileFingerprint(m_request.project.source.filePath));
    for (const StudioData::OralMaterial &material : m_request.project.materials) {
        // 带上素材 ID：同一文件被重新导入时 ID 会变化，避免复用旧 ID 的分镜。
        parts.append(material.id + QLatin1Char('|')
                     + fileFingerprint(material.filePath));
    }
    parts.sort();
    return sha1Hex(parts.join(QLatin1Char('\n')));
}

bool PipelineService::loadAnalysisCache(const QString &cachePath, EditPlan *plan)
{
    QFile file(cachePath);
    if (!file.exists() || !file.open(QIODevice::ReadOnly)) {
        return false;
    }
    const QJsonObject root = HttpClient::parseJsonObject(file.readAll());
    if (root.isEmpty()) {
        return false;
    }
    const SpeechAnalysis speech = SpeechAnalysis::fromJson(
        root.value(QStringLiteral("speech")).toObject());
    const SemanticAnalysis semantics = SemanticAnalysis::fromJson(
        root.value(QStringLiteral("semantics")).toObject());
    QVector<MaterialProfile> materials;
    for (const QJsonValue &value :
         root.value(QStringLiteral("materials")).toArray()) {
        materials.append(MaterialProfile::fromJson(value.toObject()));
    }
    if (speech.utterances.isEmpty() || materials.isEmpty()) {
        return false;
    }
    // 缓存清理可能删掉关键帧文件；此时视为缓存失效，重新做素材理解。
    for (const MaterialProfile &material : materials) {
        if (!material.keyframePath.isEmpty()
            && !QFileInfo::exists(material.keyframePath)) {
            return false;
        }
    }
    plan->speech = speech;
    plan->semantics = semantics;
    plan->materials = materials;
    return true;
}

bool PipelineService::saveAnalysisCache(const QString &cachePath,
                                        const EditPlan &plan, QString *error)
{
    QJsonObject root;
    root.insert(QStringLiteral("version"), 1);
    root.insert(QStringLiteral("speech"), plan.speech.toJson());
    root.insert(QStringLiteral("semantics"), plan.semantics.toJson());
    QJsonArray materials;
    for (const MaterialProfile &material : plan.materials) {
        materials.append(material.toJson());
    }
    root.insert(QStringLiteral("materials"), materials);

    QSaveFile file(cachePath);
    if (!file.open(QIODevice::WriteOnly)) {
        if (error) {
            *error = QStringLiteral("无法写入分析缓存：%1").arg(file.errorString());
        }
        return false;
    }
    file.write(QJsonDocument(root).toJson(QJsonDocument::Indented));
    if (!file.commit()) {
        if (error) {
            *error = QStringLiteral("保存分析缓存失败。");
        }
        return false;
    }
    return true;
}

bool PipelineService::analyzeMaterialsLocal(
    const QVector<StudioData::OralMaterial> &materials,
    QVector<MaterialProfile> *profiles, QString *error)
{
    if (!profiles) {
        return false;
    }
    profiles->clear();
    if (!QDir().mkpath(m_analysisFrameDir)) {
        if (error) {
            *error = QStringLiteral("无法创建素材关键帧目录。");
        }
        return false;
    }
    for (const StudioData::OralMaterial &material : materials) {
        if (material.filePath.trimmed().isEmpty()
            || !QFileInfo::exists(material.filePath)) {
            addLog(StudioData::OralStage::Match,
                   QStringLiteral("素材「%1」文件不存在，已跳过").arg(material.name),
                   StudioData::Tone::Muted);
            continue;
        }
        MaterialProfile profile;
        profile.id = material.id;
        profile.name = material.name;
        profile.path = material.filePath;
        const QString suffix = QFileInfo(material.filePath).suffix().toLower();
        profile.kind = (material.kind == QStringLiteral("视频")
                        || suffix == QLatin1String("mp4")
                        || suffix == QLatin1String("mov")
                        || suffix == QLatin1String("m4v")
                        || suffix == QLatin1String("mkv")
                        || suffix == QLatin1String("webm"))
            ? MaterialKind::Video
            : MaterialKind::Image;

        QString probeError;
        int width = 0;
        int height = 0;
        qint64 durationMs = 0;
        if (profile.kind == MaterialKind::Video) {
            if (!probeVideo(material.filePath, &width, &height, &durationMs,
                            &probeError)) {
                addLog(StudioData::OralStage::Match,
                       QStringLiteral("素材「%1」无法读取：%2")
                           .arg(material.name, probeError),
                       StudioData::Tone::Muted);
                continue;
            }
        } else if (!probeImage(material.filePath, &width, &height, &probeError)) {
            addLog(StudioData::OralStage::Match,
                   QStringLiteral("素材「%1」无法读取：%2")
                       .arg(material.name, probeError),
                   StudioData::Tone::Muted);
            continue;
        }
        profile.width = width;
        profile.height = height;
        profile.durationMs = durationMs;

        QStringList frames;
        if (!extractKeyframes(material.filePath, profile.kind, durationMs,
                              m_analysisFrameDir, &frames, &probeError)) {
            addLog(StudioData::OralStage::Match,
                   QStringLiteral("素材「%1」关键帧抽取失败，将仅按文件信息分析")
                       .arg(material.name),
                   StudioData::Tone::Muted);
        }
        profile.keyframePath = frames.value(0);
        profile.visual =
            analyzeVisualFeatures(frames, profile.kind == MaterialKind::Video);
        profile.description = material.name;
        profile.role = QStringLiteral("other");
        profile.quality = 60;
        profiles->append(profile);
        addLog(StudioData::OralStage::Match,
               QStringLiteral("已理解素材「%1」：%2×%3%4")
                   .arg(material.name)
                   .arg(width)
                   .arg(height)
                   .arg(durationMs > 0
                            ? QStringLiteral(" · %1 秒")
                                  .arg(durationMs / 1000.0, 0, 'f', 1)
                            : QString()),
               StudioData::Tone::Info);
    }
    if (profiles->isEmpty()) {
        if (error) {
            *error = QStringLiteral("没有可用的相关素材，请检查素材文件是否存在。");
        }
        return false;
    }
    return true;
}

void PipelineService::normalizeSemanticTimes(SemanticAnalysis *analysis,
                                             qint64 durationMs)
{
    if (!analysis) {
        return;
    }
    std::sort(analysis->segments.begin(), analysis->segments.end(),
              [](const SemanticSegment &left, const SemanticSegment &right) {
                  return left.startMs < right.startMs;
              });
    qint64 cursor = 0;
    for (SemanticSegment &segment : analysis->segments) {
        // 显式钳制，避免 qBound 的上下界在片尾附近反转。
        segment.startMs = qMax<qint64>(
            cursor, qMin(segment.startMs, qMax<qint64>(0, durationMs - 300)));
        segment.endMs = qMin(
            qMax<qint64>(segment.startMs + 300, segment.endMs), durationMs);
        cursor = segment.endMs;
    }
    for (Emphasis &emphasis : analysis->emphasis) {
        emphasis.startMs = qMax<qint64>(
            0, qMin(emphasis.startMs, qMax<qint64>(0, durationMs - 100)));
        emphasis.endMs = qMin(
            qMax<qint64>(emphasis.startMs + 100, emphasis.endMs), durationMs);
    }
    if (analysis->segments.isEmpty()) {
        SemanticSegment segment;
        segment.startMs = 0;
        segment.endMs = qMax<qint64>(1000, durationMs);
        segment.title = analysis->title.isEmpty() ? QStringLiteral("口播内容")
                                                   : analysis->title;
        segment.type = SegmentType::Point;
        analysis->segments.append(segment);
    }
}

bool PipelineService::ensureAnalysis(EditPlan *plan, bool *fromCache,
                                     AsrClient *asr, LlmClient *llm,
                                     CapCutClient *capcut, QString *error)
{
    if (!plan || !fromCache || !asr || !llm || !capcut) {
        return false;
    }
    *fromCache = false;

    const StudioData::OralProject &project = m_request.project;
    const QString ratio = project.ratio == QLatin1String("16:9")
        ? QStringLiteral("16:9")
        : QStringLiteral("9:16");
    plan->canvas.ratio = ratio;
    plan->canvas.width = ratio == QLatin1String("9:16") ? 1080 : 1920;
    plan->canvas.height = ratio == QLatin1String("9:16") ? 1920 : 1080;
    plan->canvas.fps = 30;
    plan->templateId = QStringLiteral("clean");
    plan->outputBoth = project.outputBoth;
    plan->createdAt = QDateTime::currentDateTime().toString(Qt::ISODate);
    plan->sourcePath = project.source.filePath;
    plan->sourceName = project.source.name;

    int sourceWidth = 0;
    int sourceHeight = 0;
    qint64 sourceDurationMs = 0;
    QString probeError;
    if (!probeVideo(plan->sourcePath, &sourceWidth, &sourceHeight,
                    &sourceDurationMs, &probeError)) {
        if (error) {
            *error = QStringLiteral("无法读取口播视频：%1").arg(probeError);
        }
        return false;
    }
    const QFileInfo sourceInfo(plan->sourcePath);
    plan->sourceWidth = sourceWidth;
    plan->sourceHeight = sourceHeight;
    plan->sourceSizeBytes = sourceInfo.size();
    plan->durationMs = sourceDurationMs;
    m_run.totalFrames = int(qMax<qint64>(
        1, sourceDurationMs * plan->canvas.fps / 1000));
    addLog(StudioData::OralStage::Analyze,
           QStringLiteral("已读取口播原片：%1×%2 · %3 · %4")
               .arg(sourceWidth)
               .arg(sourceHeight)
               .arg(StudioData::formatElapsed(sourceDurationMs),
                    formatFileSize(sourceInfo.size())),
           StudioData::Tone::Info);

    if (!QDir().mkpath(m_request.cacheDir)) {
        if (error) {
            *error = QStringLiteral("无法创建智能口播缓存目录。");
        }
        return false;
    }
    const QString cacheKey = analysisCacheKey();
    const QString cachePath = QDir(m_request.cacheDir)
                                  .filePath(QStringLiteral("analysis_%1.json")
                                                .arg(cacheKey));
    m_analysisFrameDir = QDir(m_request.cacheDir)
                             .filePath(QStringLiteral("analysis_%1_frames")
                                           .arg(cacheKey));
    m_workDir = QDir(m_request.cacheDir)
                    .filePath(QStringLiteral("work_%1_%2")
                                  .arg(m_request.runId)
                                  .arg(QDateTime::currentMSecsSinceEpoch()));
    if (!QDir().mkpath(m_workDir)) {
        if (error) {
            *error = QStringLiteral("无法创建临时工作目录。");
        }
        return false;
    }

    const bool cacheHit = loadAnalysisCache(cachePath, plan);
    *fromCache = cacheHit;
    if (cacheHit) {
        normalizeSemanticTimes(&plan->semantics, plan->durationMs);
        setStage(StudioData::OralStage::Analyze, StudioData::StageStatus::Done, 100,
                 QStringLiteral("%1 字 · %2 个语义段")
                     .arg(plan->speech.charCount)
                     .arg(plan->semantics.segments.size()));
        addLog(StudioData::OralStage::Analyze,
               QStringLiteral("命中本地分析缓存，跳过重复 ASR 与语义分析"),
               StudioData::Tone::Ok);
        setStage(StudioData::OralStage::Match, StudioData::StageStatus::Running, 25,
                 QStringLiteral("复用素材理解结果"));
        addLog(StudioData::OralStage::Match,
               QStringLiteral("复用 %1 个素材的理解结果")
                   .arg(plan->materials.size()),
               StudioData::Tone::Ok);
    } else {
        // 语音分析：提取 MP3 → 豆包极速版 ASR。
        setStage(StudioData::OralStage::Analyze, StudioData::StageStatus::Running,
                 8, QStringLiteral("提取口播音轨"));
        const QString audioPath =
            QDir(m_workDir).filePath(QStringLiteral("speech.mp3"));
        if (!FfmpegUtils::extractMp3(plan->sourcePath, audioPath, error)) {
            return false;
        }
        addLog(StudioData::OralStage::Analyze,
               QStringLiteral("已提取口播音轨，准备提交语音识别"),
               StudioData::Tone::Info);
        setStage(StudioData::OralStage::Analyze, StudioData::StageStatus::Running,
                 20, QStringLiteral("提交语音识别"));
        addLog(StudioData::OralStage::Analyze,
               QStringLiteral("正在调用 ASR（%1）…").arg(AsrClient::modelName()),
               StudioData::Tone::Info);
        SpeechAnalysis speech;
        QString primaryError;
        bool transcribed = asr->transcribe(
            audioPath, m_request.gatewayBaseUrl, m_request.apiKey, &speech,
            &primaryError);
        // 首选模型是产品指定的豆包极速版；如果当前 API Key 未开通该资源，
        // 自动回退到标准版（公网 URL）或 qwen3-asr-flash（本地文件），
        // 保证功能可用并在过程日志里明确提示。
        if (!transcribed && asrErrorNeedsFallback(primaryError)) {
            addLog(StudioData::OralStage::Analyze,
                   QStringLiteral("豆包极速版未开通，正在回退标准版 ASR…"),
                   StudioData::Tone::Muted);
            QString audioUrl;
            QString fallbackError;
            if (capcut->uploadFile(audioPath, m_request.apiKey, &audioUrl,
                                   &fallbackError)
                && asr->transcribeUrl(
                    audioUrl, AsrClient::fallbackUrlModelName(),
                    m_request.gatewayBaseUrl, m_request.apiKey, &speech,
                    &fallbackError)) {
                transcribed = true;
                addLog(StudioData::OralStage::Analyze,
                       QStringLiteral("已回退到 %1 完成识别")
                           .arg(AsrClient::fallbackUrlModelName()),
                       StudioData::Tone::Info);
            } else {
                addLog(StudioData::OralStage::Analyze,
                       QStringLiteral("标准版识别失败，继续回退本地文件模型：%1")
                           .arg(fallbackError),
                       StudioData::Tone::Muted);
                if (asr->transcribeWithModel(
                        audioPath, AsrClient::fallbackFileModelName(),
                        m_request.gatewayBaseUrl, m_request.apiKey, &speech,
                        &fallbackError)) {
                    transcribed = true;
                    addLog(StudioData::OralStage::Analyze,
                           QStringLiteral("已回退到 %1 完成识别")
                               .arg(AsrClient::fallbackFileModelName()),
                           StudioData::Tone::Info);
                } else {
                    primaryError += QStringLiteral("；回退也失败：%1")
                                        .arg(fallbackError);
                }
            }
        }
        if (!transcribed) {
            if (error) {
                *error = primaryError;
            }
            return false;
        }
        // 以源视频时长为准裁剪 ASR 时间线，避免尾部静音/容器差异导致字幕越过片尾。
        speech.durationMs = plan->durationMs;
        QVector<Utterance> clampedUtterances;
        for (Utterance utterance : speech.utterances) {
            if (utterance.startMs >= plan->durationMs) {
                continue;
            }
            utterance.endMs = qMin(utterance.endMs, plan->durationMs);
            if (utterance.endMs <= utterance.startMs) {
                continue;
            }
            for (Word &word : utterance.words) {
                word.startMs = qMin(word.startMs, utterance.endMs);
                word.endMs = qMin(word.endMs, utterance.endMs);
            }
            clampedUtterances.append(utterance);
        }
        if (clampedUtterances.isEmpty()) {
            if (error) {
                *error = QStringLiteral("语音识别结果与视频时长不匹配。");
            }
            return false;
        }
        speech.utterances = clampedUtterances;
        plan->speech = speech;
        addLog(StudioData::OralStage::Analyze,
               QStringLiteral("ASR 识别完成：%1 字 · %2 句 · %3 位说话人")
                   .arg(speech.charCount)
                   .arg(speech.utterances.size())
                   .arg(qMax(1, speech.speakers.size())),
               StudioData::Tone::Ok);
        setStage(StudioData::OralStage::Analyze, StudioData::StageStatus::Running,
                 55, QStringLiteral("语义分析：内容分段与主题提取"));
        addLog(StudioData::OralStage::Analyze,
               QStringLiteral("正在用 %1 做语义分析…").arg(LlmClient::modelName()),
               StudioData::Tone::Info);
        SemanticAnalysis semantics;
        if (!llm->analyzeSemantics(m_request.gatewayBaseUrl, m_request.apiKey,
                                   plan->speech, &semantics, error,
                                   [this](const QString &status) {
                                       addLog(StudioData::OralStage::Analyze,
                                              status, StudioData::Tone::Info);
                                   })) {
            return false;
        }
        normalizeSemanticTimes(&semantics, plan->durationMs);
        plan->semantics = semantics;
        addLog(StudioData::OralStage::Analyze,
               QStringLiteral("划分 %1 个语义段：%2")
                   .arg(semantics.segments.size())
                   .arg([&semantics] {
                       QStringList titles;
                       for (const SemanticSegment &segment : semantics.segments) {
                           titles.append(segment.title);
                       }
                       return titles.join(QStringLiteral(" / "));
                   }()),
               StudioData::Tone::Ai);
        if (!semantics.keywords.isEmpty()) {
            addLog(StudioData::OralStage::Analyze,
                   QStringLiteral("提取关键词：%1")
                       .arg(semantics.keywords.join(QStringLiteral("、"))),
                   StudioData::Tone::Ai);
        }
        setStage(StudioData::OralStage::Analyze, StudioData::StageStatus::Done, 100,
                 QStringLiteral("%1 字 · %2 个语义段")
                     .arg(plan->speech.charCount)
                     .arg(plan->semantics.segments.size()));

        // 素材分析：本地视觉特征 + DeepSeek 画面理解。
        setStage(StudioData::OralStage::Match, StudioData::StageStatus::Running,
                 10, QStringLiteral("分析相关素材"));
        QVector<MaterialProfile> profiles;
        if (!analyzeMaterialsLocal(project.materials, &profiles, error)) {
            return false;
        }
        setStage(StudioData::OralStage::Match, StudioData::StageStatus::Running,
                 55, QStringLiteral("大模型理解素材画面"));
        QVector<MaterialProfile> analyzed;
        if (!llm->analyzeMaterials(m_request.gatewayBaseUrl, m_request.apiKey,
                                   profiles, &analyzed, error,
                                   [this](const QString &status) {
                                       addLog(StudioData::OralStage::Match,
                                              status, StudioData::Tone::Info);
                                   })) {
            // 素材理解失败不阻断成片：保留本地特征与默认标签继续。
            addLog(StudioData::OralStage::Match,
                   QStringLiteral("素材画面理解失败，已使用本地特征继续：%1")
                       .arg(error ? *error : QString()),
                   StudioData::Tone::Muted);
            analyzed = profiles;
        }
        plan->materials = analyzed;
        addLog(StudioData::OralStage::Match,
               QStringLiteral("素材理解完成：%1 个素材已建立语义标签")
                   .arg(analyzed.size()),
               StudioData::Tone::Ai);

        QString cacheError;
        if (!saveAnalysisCache(cachePath, *plan, &cacheError)) {
            addLog(StudioData::OralStage::Match,
                   QStringLiteral("分析缓存写入失败：%1").arg(cacheError),
                   StudioData::Tone::Muted);
        }
    }

    // 镜头规划：缓存命中时也按当前密度/转场设置重新规划。
    setStage(StudioData::OralStage::Match, StudioData::StageStatus::Running,
             cacheHit ? 45 : 70, QStringLiteral("规划 B-Roll 镜头"));
    const int targetCount =
        brollCountForDensity(densityFromStudio(project.density));
    QVector<Shot> llmShots;
    QString shotError;
    if (!llm->planShots(m_request.gatewayBaseUrl, m_request.apiKey, plan->speech,
                        plan->semantics, plan->materials, targetCount, &llmShots,
                        &shotError,
                        [this](const QString &status) {
                            addLog(StudioData::OralStage::Match, status,
                                   StudioData::Tone::Info);
                        })) {
        addLog(StudioData::OralStage::Match,
               QStringLiteral("大模型镜头规划失败，改用本地语义匹配：%1")
                   .arg(shotError),
               StudioData::Tone::Muted);
    }
    plan->shots = buildShotPlan(plan->speech, plan->semantics, plan->materials,
                                targetCount, densityFromStudio(project.density),
                                llmShots, project.transitions);
    addLog(StudioData::OralStage::Match,
           QStringLiteral("镜头规划完成：%1 处 B-Roll%2")
               .arg(plan->shots.shots.size())
               .arg(project.transitions ? QStringLiteral(" · 动态转场")
                                        : QString()),
           StudioData::Tone::Ok);

    // 字幕系统：关键词高亮、字号与安全区随画布自适应。
    plan->subtitles = buildSubtitles(plan->speech, plan->semantics,
                                     presetFromStudio(project.subtitleStyle),
                                     plan->canvas, project.titleFx);
    m_run.previewCaption = plan->semantics.title.isEmpty()
        ? (plan->subtitles.cues.isEmpty() ? QString()
                                          : plan->subtitles.cues.first().text)
        : plan->semantics.title;
    m_run.previewKeywords = plan->semantics.keywords.mid(0, 4);
    m_run.matchedMaterials = qMin(8, int(plan->materials.size()));
    m_run.subtitleCount = plan->subtitles.cues.size();
    m_run.totalSubtitles = plan->subtitles.cues.size();
    addLog(StudioData::OralStage::Match,
           QStringLiteral("字幕系统：%1 条 · %2")
               .arg(plan->subtitles.cues.size())
               .arg(subtitlePresetName(plan->subtitles.preset)),
           StudioData::Tone::Ai);

    setStage(StudioData::OralStage::Match, StudioData::StageStatus::Done, 100,
             QStringLiteral("%1 个素材 · %2 处插入")
                 .arg(plan->materials.size())
                 .arg(plan->shots.shots.size()));
    return true;
}

/* ------------------------------------------------------------------ */
/* 音频系统                                                             */
/* ------------------------------------------------------------------ */

bool PipelineService::buildAudioPlan(const EditPlan &plan, AudioPlan *audio,
                                     AudioAssetsClient *assets, LlmClient *llm,
                                     QString *error)
{
    if (!audio || !assets || !llm) {
        if (error) {
            *error = QStringLiteral("音频系统未初始化。");
        }
        return false;
    }
    audio->bgmEnabled = m_request.project.bgm;
    audio->sfxEnabled = m_request.project.sfx;

    // BGM：随机取一首，短于成片时循环；音量压到 0.12 给口播让位。
    if (audio->bgmEnabled) {
        BgmTrack track;
        QString bgmError;
        if (assets->randomBgm(&track, &bgmError)) {
            qint64 durationMs = track.durationMs;
            if (durationMs <= 0) {
                FfmpegUtils::mediaDurationMs(track.url, &durationMs);
            }
            if (durationMs > 0) {
                audio->bgm.kind = AudioKind::Bgm;
                audio->bgm.name = track.name;
                audio->bgm.url = track.url;
                audio->bgm.durationMs = durationMs;
                audio->bgm.startMs = 0;
                audio->bgm.endMs = plan.durationMs;
                audio->bgm.volume = kBgmVolume;
                audio->bgm.loop = true;
                addLog(StudioData::OralStage::Compose,
                       QStringLiteral("匹配 BGM：%1 · 音量 %2 · 人声优先")
                           .arg(track.name)
                           .arg(kBgmVolume, 0, 'f', 2),
                       StudioData::Tone::Info);
            } else {
                addLog(StudioData::OralStage::Compose,
                       QStringLiteral("BGM「%1」时长未知，已跳过").arg(track.name),
                       StudioData::Tone::Muted);
            }
        } else {
            addLog(StudioData::OralStage::Compose,
                   QStringLiteral("背景音乐未获取，已跳过：%1").arg(bgmError),
                   StudioData::Tone::Muted);
        }
    }

    // 音效：先用大模型稀疏选点；失败时按重点句与片头做本地兜底。
    if (audio->sfxEnabled && !plan.subtitles.cues.isEmpty()) {
        QVector<SfxAsset> sfxAssets;
        QString assetError;
        if (!assets->listSfx(&sfxAssets, &assetError)) {
            addLog(StudioData::OralStage::Compose,
                   QStringLiteral("音效素材获取失败：%1").arg(assetError),
                   StudioData::Tone::Muted);
        } else {
            const QVector<ResolvedSfx> resolved = resolveSfx(sfxAssets);
            if (resolved.isEmpty()) {
                addLog(StudioData::OralStage::Compose,
                       QStringLiteral("音效素材目录为空"), StudioData::Tone::Muted);
            } else {
                QVector<SfxCategoryHint> hints;
                for (const ResolvedSfx &sfx : resolved) {
                    hints.append({sfx.name, sfx.usageHint});
                }
                QVector<int> categories;
                QString sfxError;
                const bool selected = llm->selectSfx(
                    m_request.gatewayBaseUrl, m_request.apiKey,
                    plan.subtitles.cues, hints, &categories, &sfxError,
                    [this](const QString &status) {
                        addLog(StudioData::OralStage::Compose, status,
                               StudioData::Tone::Info);
                    });
                if (selected) {
                    for (int index = 0;
                         index < categories.size()
                         && index < plan.subtitles.cues.size();
                         ++index) {
                        const int category = categories.at(index);
                        if (category < 0 || category >= resolved.size()) {
                            continue;
                        }
                        const ResolvedSfx &sfx = resolved.at(category);
                        AudioCue cue;
                        cue.kind = AudioKind::Sfx;
                        cue.name = sfx.name;
                        cue.url = sfx.url;
                        cue.startMs = plan.subtitles.cues.at(index).startMs;
                        cue.endMs = cue.startMs + sfx.durationMs;
                        cue.durationMs = sfx.durationMs;
                        cue.volume = kSfxVolume;
                        if (cue.endMs > plan.durationMs) {
                            continue;
                        }
                        if (!overlapsAny(cue, audio->sfx)) {
                            audio->sfx.append(cue);
                        }
                        if (audio->sfx.size() >= kMaxSfxCount) {
                            break;
                        }
                    }
                } else {
                    addLog(StudioData::OralStage::Compose,
                           QStringLiteral("音效选点失败，改用重点句兜底：%1")
                               .arg(sfxError),
                           StudioData::Tone::Muted);
                }

                // 兜底：模型未选或选得很少时，给重点句与片头补少量音效。
                const auto findByName = [&resolved](const QString &name) {
                    for (const ResolvedSfx &sfx : resolved) {
                        if (sfx.name == name) {
                            return sfx;
                        }
                    }
                    return ResolvedSfx();
                };
                if (audio->sfx.size() < 3) {
                    const ResolvedSfx accent =
                        findByName(QStringLiteral("字幕音效-嘭"));
                    for (const SubtitleCue &cue : plan.subtitles.cues) {
                        if (!cue.emphasis || accent.url.isEmpty()) {
                            continue;
                        }
                        AudioCue item;
                        item.kind = AudioKind::Sfx;
                        item.name = accent.name;
                        item.url = accent.url;
                        item.startMs = cue.startMs;
                        item.endMs = cue.startMs + accent.durationMs;
                        item.durationMs = accent.durationMs;
                        item.volume = kSfxVolume;
                        if (item.endMs <= plan.durationMs
                            && !overlapsAny(item, audio->sfx)) {
                            audio->sfx.append(item);
                        }
                        if (audio->sfx.size() >= 3) {
                            break;
                        }
                    }
                }
                if (audio->sfx.isEmpty()) {
                    const ResolvedSfx intro =
                        findByName(QStringLiteral("开篇提示水滴声"));
                    if (!intro.url.isEmpty() && plan.durationMs > 1500) {
                        AudioCue item;
                        item.kind = AudioKind::Sfx;
                        item.name = intro.name;
                        item.url = intro.url;
                        item.startMs = 300;
                        item.endMs = qMin(plan.durationMs,
                                          300 + intro.durationMs);
                        item.durationMs = intro.durationMs;
                        item.volume = kSfxVolume;
                        audio->sfx.append(item);
                    }
                }
            }
        }
        if (!audio->sfx.isEmpty()) {
            addLog(StudioData::OralStage::Compose,
                   QStringLiteral("音频系统：%1 处音效已排入时间线")
                       .arg(audio->sfx.size()),
                   StudioData::Tone::Info);
        }
    }
    return true;
}

/* ------------------------------------------------------------------ */
/* 3. 合成阶段                                                          */
/* ------------------------------------------------------------------ */

bool PipelineService::composeDraft(EditPlan *plan, CapCutClient *capcut,
                                   HttpClient *http, const QString &workDir,
                                   CapCutDraft *draft, QString *error)
{
    if (!plan || !capcut || !draft) {
        if (error) {
            *error = QStringLiteral("合成阶段参数无效。");
        }
        return false;
    }
    setStage(StudioData::OralStage::Compose, StudioData::StageStatus::Running,
             20, QStringLiteral("创建剪映草稿"));
    if (!capcut->createDraft(plan->canvas.width, plan->canvas.height, draft,
                             error)) {
        return false;
    }
    addLog(StudioData::OralStage::Compose,
           QStringLiteral("已创建剪映草稿：%1×%2")
               .arg(plan->canvas.width)
               .arg(plan->canvas.height),
           StudioData::Tone::Info);

    // 口播原片：整段放在 V1，保留原声；B-Roll 覆盖在其上方。
    setStage(StudioData::OralStage::Compose, StudioData::StageStatus::Running,
             28, QStringLiteral("上传口播原片"));
    const QString sourceFingerprint = fileFingerprint(plan->sourcePath);
    QString sourceUrl;
    bool sourceFromCache =
        readSourceUploadCache(m_request.cacheDir, sourceFingerprint, &sourceUrl);
    if (sourceFromCache) {
        addLog(StudioData::OralStage::Compose,
               QStringLiteral("复用 6 天内已上传的口播原片，跳过重复上传"),
               StudioData::Tone::Ok);
    } else {
        addLog(StudioData::OralStage::Compose,
               QStringLiteral("正在上传口播原片（%1）…")
                   .arg(formatFileSize(plan->sourceSizeBytes)),
               StudioData::Tone::Info);
        if (!capcut->uploadFile(plan->sourcePath, m_request.apiKey, &sourceUrl,
                                error)) {
            return false;
        }
    }
    CapCutVideo sourceVideo;
    sourceVideo.url = sourceUrl;
    sourceVideo.width = plan->sourceWidth;
    sourceVideo.height = plan->sourceHeight;
    sourceVideo.startUs = 0;
    sourceVideo.endUs = plan->durationMs * 1000;
    sourceVideo.volume = 1.0;
    const double scale = qBound(
        0.1, coverScale(plan->sourceWidth, plan->sourceHeight,
                        plan->canvas.width, plan->canvas.height),
        5.0);
    QStringList sourceSegments;
    bool sourceAdded = capcut->addVideos(*draft, {sourceVideo}, scale, scale,
                                         1.0, 0, 0, &sourceSegments, error);
    if (!sourceAdded && sourceFromCache) {
        // 缓存 URL 可能已过期：重新上传一次再落轨。
        addLog(StudioData::OralStage::Compose,
               QStringLiteral("缓存的原片地址已失效，正在重新上传…"),
               StudioData::Tone::Muted);
        sourceFromCache = false;
        if (!capcut->uploadFile(plan->sourcePath, m_request.apiKey, &sourceUrl,
                                error)) {
            return false;
        }
        sourceVideo.url = sourceUrl;
        sourceAdded = capcut->addVideos(*draft, {sourceVideo}, scale, scale,
                                        1.0, 0, 0, &sourceSegments, error);
    }
    if (!sourceAdded) {
        return false;
    }
    if (!sourceFromCache) {
        writeSourceUploadCache(m_request.cacheDir, sourceFingerprint, sourceUrl);
    }
    addLog(StudioData::OralStage::Compose,
           QStringLiteral("口播原片已写入 V1 轨"), StudioData::Tone::Ok);

    // B-Roll：视频与图片各自一条轨，覆盖在口播上方。
    setStage(StudioData::OralStage::Compose, StudioData::StageStatus::Running,
             42, QStringLiteral("预处理并写入 B-Roll"));
    addBrollTracks(plan, capcut, *draft, workDir, error);

    // 封面：口播首帧图生图生成海报，压在最上层，覆盖片头 10 毫秒。
    setStage(StudioData::OralStage::Compose, StudioData::StageStatus::Running,
             58, QStringLiteral("生成封面海报"));
    addCoverTrack(plan, capcut, *draft, workDir, http, error);

    // 字幕：标题花字单独一条轨，正文关键词高亮。
    setStage(StudioData::OralStage::Compose, StudioData::StageStatus::Running,
             68, QStringLiteral("写入字幕轨道"));
    if (!addSubtitleTracks(*plan, capcut, *draft, error)) {
        return false;
    }

    // 音频：先音效后 BGM，保证口播、音效、BGM 三层清晰。
    setStage(StudioData::OralStage::Compose, StudioData::StageStatus::Running,
             84, QStringLiteral("写入音频轨道"));
    addAudioTracks(*plan, capcut, *draft, error);
    return true;
}

bool PipelineService::addBrollTracks(EditPlan *plan, CapCutClient *capcut,
                                     const CapCutDraft &draft,
                                     const QString &workDir, QString *error)
{
    if (!plan || plan->shots.shots.isEmpty()) {
        addLog(StudioData::OralStage::Compose,
               QStringLiteral("本次没有规划 B-Roll，成片以口播原片为主"),
               StudioData::Tone::Info);
        return true;
    }
    QHash<QString, MaterialProfile> materialById;
    for (const MaterialProfile &material : plan->materials) {
        materialById.insert(material.id, material);
    }
    QHash<QString, QString> uploadedByMaterial;
    QVector<CapCutVideo> videos;
    QVector<int> videoShotIndexes;
    QVector<CapCutImage> images;
    QVector<int> imageShotIndexes;
    QVector<CapCutKeyframe> keyframes;
    int preparedCount = 0;

    for (int index = 0; index < plan->shots.shots.size(); ++index) {
        Shot &shot = plan->shots.shots[index];
        const auto it = materialById.constFind(shot.materialId);
        if (it == materialById.constEnd()) {
            continue;
        }
        const MaterialProfile &material = it.value();
        shot.materialKind = material.kind;

        // 图片按素材缓存上传结果；视频每个镜头截取的片段不同，必须逐个上传。
        QString uploadedUrl = material.kind == MaterialKind::Image
            ? uploadedByMaterial.value(material.id)
            : QString();
        if (uploadedUrl.isEmpty()) {
            QString preparedPath;
            if (material.kind == MaterialKind::Image) {
                preparedPath = QDir(workDir).filePath(
                    materialPreparedName(material.id, QStringLiteral(".jpg")));
                if (!prepareCanvasImage(material.path, plan->canvas.width,
                                        plan->canvas.height, preparedPath,
                                        error)) {
                    addLog(StudioData::OralStage::Compose,
                           QStringLiteral("图片「%1」预处理失败，已跳过")
                               .arg(material.name),
                           StudioData::Tone::Muted);
                    continue;
                }
            } else {
                // 视频 B-Roll 逐镜头截取；同一素材不同时间点可重复使用。
                preparedPath = QDir(workDir).filePath(QStringLiteral(
                    "broll_%1_%2.mp4").arg(index).arg(
                    MediaUtils::sanitizeFileComponent(material.id, 24)));
                if (!prepareCanvasVideo(material.path, shot.sourceInMs,
                                        shot.durationMs(), plan->canvas.width,
                                        plan->canvas.height, plan->canvas.fps,
                                        preparedPath, error)) {
                    addLog(StudioData::OralStage::Compose,
                           QStringLiteral("视频「%1」预处理失败，已跳过")
                               .arg(material.name),
                           StudioData::Tone::Muted);
                    continue;
                }
            }
            if (!capcut->uploadFile(preparedPath, m_request.apiKey, &uploadedUrl,
                                    error)) {
                addLog(StudioData::OralStage::Compose,
                       QStringLiteral("B-Roll「%1」上传失败，已跳过")
                           .arg(material.name),
                       StudioData::Tone::Muted);
                continue;
            }
            if (material.kind == MaterialKind::Image) {
                uploadedByMaterial.insert(material.id, uploadedUrl);
            }
            ++preparedCount;
        }
        shot.uploadedUrl = uploadedUrl;
        if (material.kind == MaterialKind::Image) {
            CapCutImage image;
            image.url = uploadedUrl;
            image.width = plan->canvas.width;
            image.height = plan->canvas.height;
            image.startUs = shot.startMs * 1000;
            image.endUs = shot.endMs * 1000;
            if (m_request.project.transitions) {
                image.inAnimation = QStringLiteral("渐显");
                image.outAnimation = QStringLiteral("渐隐");
                image.inAnimationDurationUs = 240000;
                image.outAnimationDurationUs = 240000;
            }
            images.append(image);
            imageShotIndexes.append(index);
        } else {
            CapCutVideo video;
            video.url = uploadedUrl;
            video.width = plan->canvas.width;
            video.height = plan->canvas.height;
            video.startUs = shot.startMs * 1000;
            video.endUs = shot.endMs * 1000;
            // B-Roll 只提供画面，声音仍来自 V1 的口播原片。
            video.volume = 0.0;
            videos.append(video);
            videoShotIndexes.append(index);
        }
    }

    if (!videos.isEmpty()) {
        QStringList segmentIds;
        if (capcut->addVideos(draft, videos, 1.0, 1.0, 1.0, 0, 0,
                              &segmentIds, error)) {
            addLog(StudioData::OralStage::Compose,
                   QStringLiteral("已写入 %1 段 B-Roll 视频").arg(videos.size()),
                   StudioData::Tone::Ok);
            if (m_request.project.transitions) {
                // 视频 B-Roll 用透明度关键帧做 240ms 淡入淡出。
                for (int i = 0; i < segmentIds.size() && i < videoShotIndexes.size();
                     ++i) {
                    const Shot &shot = plan->shots.shots.at(videoShotIndexes.at(i));
                    const qint64 durationUs = shot.durationMs() * 1000;
                    if (durationUs < 600000) {
                        continue;
                    }
                    keyframes.append({segmentIds.at(i), QStringLiteral("KFTypeAlpha"),
                                      0, 0.0});
                    keyframes.append({segmentIds.at(i), QStringLiteral("KFTypeAlpha"),
                                      240000, 1.0});
                    keyframes.append({segmentIds.at(i), QStringLiteral("KFTypeAlpha"),
                                      durationUs - 240000, 1.0});
                    keyframes.append({segmentIds.at(i), QStringLiteral("KFTypeAlpha"),
                                      durationUs, 0.0});
                }
            }
        } else {
            addLog(StudioData::OralStage::Compose,
                   QStringLiteral("B-Roll 视频轨道写入失败，已跳过：%1")
                       .arg(error ? *error : QString()),
                   StudioData::Tone::Muted);
        }
    }

    if (!images.isEmpty()) {
        QStringList segmentIds;
        if (capcut->addImages(draft, images, 1.0, 0, 0, &segmentIds,
                              error)) {
            addLog(StudioData::OralStage::Compose,
                   QStringLiteral("已写入 %1 张 B-Roll 图片").arg(images.size()),
                   StudioData::Tone::Ok);
            if (m_request.project.transitions) {
                // 图片轻微推近，避免静态画面显得死板。
                for (int i = 0; i < segmentIds.size() && i < imageShotIndexes.size();
                     ++i) {
                    const Shot &shot = plan->shots.shots.at(imageShotIndexes.at(i));
                    const qint64 durationUs = shot.durationMs() * 1000;
                    keyframes.append({segmentIds.at(i), QStringLiteral("KFTypeScaleX"),
                                      0, 1.0});
                    keyframes.append({segmentIds.at(i), QStringLiteral("KFTypeScaleY"),
                                      0, 1.0});
                    keyframes.append({segmentIds.at(i), QStringLiteral("KFTypeScaleX"),
                                      durationUs, 1.05});
                    keyframes.append({segmentIds.at(i), QStringLiteral("KFTypeScaleY"),
                                      durationUs, 1.05});
                }
            }
        } else {
            addLog(StudioData::OralStage::Compose,
                   QStringLiteral("B-Roll 图片轨道写入失败，已跳过：%1")
                       .arg(error ? *error : QString()),
                   StudioData::Tone::Muted);
        }
    }

    if (!keyframes.isEmpty()) {
        QString keyframeError;
        if (!capcut->addKeyframes(draft, keyframes, &keyframeError)) {
            addLog(StudioData::OralStage::Compose,
                   QStringLiteral("转场关键帧写入失败，画面保持硬切：%1")
                       .arg(keyframeError),
                   StudioData::Tone::Muted);
        }
    }
    addLog(StudioData::OralStage::Compose,
           QStringLiteral("B-Roll 就绪：%1 个素材已预处理").arg(preparedCount),
           StudioData::Tone::Info);
    return true;
}

bool PipelineService::ensureCoverPrompt(EditPlan *plan, HttpClient *http,
                                        const QString &cacheDir, QString *error)
{
    Q_UNUSED(error);
    if (!plan || !http) {
        return false;
    }
    if (!m_request.regenerateCover
        && !plan->cover.prompt.trimmed().isEmpty()) {
        return true;
    }

    // 提示词 2 按「源文件 + 提示词 1 内容 + 文本模型 + 画布」缓存，避免重复调用；
    // 提示词 1 调整后缓存指纹自动变化。重新包装时跳过缓存，重新生成。
    const QString promptCachePath = QDir(cacheDir).filePath(
        QStringLiteral("prompt_%1.txt")
            .arg(sha1Hex(QStringList{
                fileFingerprint(plan->sourcePath),
                Oral::coverDirectorPrompt(),
                CoverClient::promptModelName(),
                plan->canvas.ratio,
                QStringLiteral("%1x%2")
                    .arg(plan->canvas.width)
                    .arg(plan->canvas.height),
            }.join(QLatin1Char('|')))));
    if (!m_request.regenerateCover) {
        QFile cached(promptCachePath);
        if (cached.open(QIODevice::ReadOnly)) {
            const QString prompt = QString::fromUtf8(cached.readAll()).trimmed();
            cached.close();
            if (!prompt.isEmpty()) {
                plan->cover.prompt = prompt;
                addLog(StudioData::OralStage::Compose,
                       QStringLiteral("复用已生成的封面提示词 2"),
                       StudioData::Tone::Ok);
                return true;
            }
        }
    } else {
        addLog(StudioData::OralStage::Compose,
               QStringLiteral("重新包装：重新生成封面提示词 2"),
               StudioData::Tone::Info);
    }

    setStage(StudioData::OralStage::Compose, StudioData::StageStatus::Running,
             60, QStringLiteral("生成封面提示词 2"));
    addLog(StudioData::OralStage::Compose,
           QStringLiteral("正在用口播内容生成封面提示词 2（%1）…")
               .arg(CoverClient::promptModelName()),
           StudioData::Tone::Ai);
    CoverClient cover(http);
    QString prompt;
    QString promptError;
    if (cover.generatePrompt(m_request.gatewayBaseUrl, m_request.apiKey,
                             coverOralContentFor(*plan), &prompt, &promptError,
                             [this](const QString &status) {
                                 addLog(StudioData::OralStage::Compose, status,
                                        StudioData::Tone::Info);
                             })) {
        plan->cover.prompt = prompt;
        addLog(StudioData::OralStage::Compose,
               QStringLiteral("封面提示词 2 已生成（%1 字）").arg(prompt.size()),
               StudioData::Tone::Ok);
        if (QDir().mkpath(cacheDir)) {
            QSaveFile file(promptCachePath);
            if (file.open(QIODevice::WriteOnly)) {
                file.write(prompt.toUtf8());
                file.commit();
            }
        }
        return true;
    }

    plan->cover.prompt = fallbackCoverPromptFor(*plan);
    addLog(StudioData::OralStage::Compose,
           QStringLiteral("封面提示词生成失败，改用本地兜底提示词：%1")
               .arg(promptError),
           StudioData::Tone::Muted);
    return true;
}

bool PipelineService::addCoverTrack(EditPlan *plan, CapCutClient *capcut,
                                    const CapCutDraft &draft,
                                    const QString &workDir, HttpClient *http,
                                    QString *error)
{
    if (!plan || !capcut || !http) {
        return true;
    }

    plan->cover.startMs = 0;
    plan->cover.endMs = kCoverEndMs;
    const QString cacheDir =
        QDir(m_request.cacheDir).filePath(QStringLiteral("covers"));
    ensureCoverPrompt(plan, http, cacheDir, error);

    // 封面按「源文件 + 模型 + 提示词 2 + 画布」缓存；普通生成复用缓存，
    // 重新包装时强制重新生成。
    const QString cacheKey = sha1Hex(QStringList{
        fileFingerprint(plan->sourcePath),
        CoverClient::modelName(),
        plan->cover.prompt,
        QStringLiteral("%1x%2").arg(plan->canvas.width).arg(plan->canvas.height),
    }.join(QLatin1Char('|')));
    const QString coverPath = QDir(cacheDir).filePath(
        QStringLiteral("cover_%1.jpg").arg(cacheKey));

    // 缓存文件可能是上次中断留下的半截图片，必须能读出且尺寸正确才复用。
    // 重新包装时跳过缓存，强制重新调用图生图生成封面。
    bool coverCached = false;
    if (!m_request.regenerateCover && QFileInfo::exists(coverPath)
        && QFileInfo(coverPath).size() > 0) {
        int coverWidth = 0;
        int coverHeight = 0;
        QString probeError;
        coverCached = probeImage(coverPath, &coverWidth, &coverHeight,
                                 &probeError)
            && coverWidth == plan->canvas.width
            && coverHeight == plan->canvas.height;
        if (!coverCached) {
            QFile::remove(coverPath);
        }
    }
    QString posterPath = coverPath;
    if (coverCached) {
        addLog(StudioData::OralStage::Compose,
               QStringLiteral("复用已生成的封面海报，跳过重复生成"),
               StudioData::Tone::Ok);
    } else {
        setStage(StudioData::OralStage::Compose, StudioData::StageStatus::Running,
                 62, QStringLiteral("生成封面海报"));
        addLog(StudioData::OralStage::Compose,
               QStringLiteral("正在用口播首帧生成封面海报（%1）…")
                   .arg(CoverClient::modelName()),
               StudioData::Tone::Ai);
        const QString framePath =
            QDir(workDir).filePath(QStringLiteral("cover_frame.jpg"));
        QString localError;
        if (!extractFirstFrame(plan->sourcePath, framePath, &localError)) {
            addLog(StudioData::OralStage::Compose,
                   QStringLiteral("口播首帧提取失败，成片继续输出：%1")
                       .arg(localError),
                   StudioData::Tone::Muted);
            return true;
        }
        addLog(StudioData::OralStage::Compose,
               QStringLiteral("已提取口播首帧，正在调用图生图生成封面…"),
               StudioData::Tone::Info);

        const QString rawPath =
            QDir(workDir).filePath(QStringLiteral("cover_raw.jpg"));
        CoverClient cover(http);
        if (!cover.generate(
                m_request.gatewayBaseUrl, m_request.apiKey, framePath,
                plan->cover.prompt, plan->canvas.width, plan->canvas.height,
                rawPath, &localError,
                [this](const QString &status) {
                    addLog(StudioData::OralStage::Compose, status,
                           StudioData::Tone::Info);
                })) {
            addLog(StudioData::OralStage::Compose,
                   QStringLiteral("封面生成失败，成片继续输出：%1")
                       .arg(localError),
                   StudioData::Tone::Muted);
            return true;
        }
        // 模型输出尺寸可能与请求略有差异，统一裁成画布尺寸后写入缓存。
        // 先写 .part 再改名，避免中断留下半截缓存被下一次复用。
        const QString partPath =
            coverPath + QStringLiteral(".part.jpg");
        if (!prepareCanvasImage(rawPath, plan->canvas.width,
                                plan->canvas.height, partPath, &localError)) {
            addLog(StudioData::OralStage::Compose,
                   QStringLiteral("封面预处理失败，成片继续输出：%1")
                       .arg(localError),
                   StudioData::Tone::Muted);
            return true;
        }
        if (QFileInfo::exists(coverPath)) {
            QFile::remove(coverPath);
        }
        if (!QFile::rename(partPath, coverPath)) {
            posterPath = partPath;
            addLog(StudioData::OralStage::Compose,
                   QStringLiteral("封面缓存写入失败，本次使用临时文件"),
                   StudioData::Tone::Muted);
        }
        addLog(StudioData::OralStage::Compose,
               QStringLiteral("封面海报已生成：%1×%2")
                   .arg(plan->canvas.width)
                   .arg(plan->canvas.height),
               StudioData::Tone::Ok);
    }
    plan->cover.localPath = posterPath;

    setStage(StudioData::OralStage::Compose, StudioData::StageStatus::Running,
             64, QStringLiteral("写入封面轨道"));
    QString uploadedUrl;
    QString uploadError;
    if (!capcut->uploadFile(posterPath, m_request.apiKey, &uploadedUrl,
                            &uploadError)) {
        addLog(StudioData::OralStage::Compose,
               QStringLiteral("封面海报上传失败，已跳过：%1").arg(uploadError),
               StudioData::Tone::Muted);
        return true;
    }
    CapCutImage image;
    image.url = uploadedUrl;
    image.width = plan->canvas.width;
    image.height = plan->canvas.height;
    image.startUs = plan->cover.startMs * 1000;
    image.endUs = plan->cover.endMs * 1000;
    QStringList segmentIds;
    if (!capcut->addImages(draft, {image}, 1.0, 0, 0, &segmentIds,
                           &uploadError)) {
        addLog(StudioData::OralStage::Compose,
               QStringLiteral("封面轨道写入失败，已跳过：%1").arg(uploadError),
               StudioData::Tone::Muted);
        return true;
    }
    plan->cover.uploadedUrl = uploadedUrl;

    // 封面占据首帧：标题与正文从封面结束之后开始，避免遮住封面。
    if (plan->subtitles.titleStartMs < plan->cover.endMs) {
        plan->subtitles.titleStartMs = plan->cover.endMs;
    }
    for (SubtitleCue &cue : plan->subtitles.cues) {
        if (cue.startMs < plan->cover.endMs) {
            cue.startMs = plan->cover.endMs;
        }
        if (cue.endMs <= cue.startMs) {
            cue.endMs = qMin(plan->durationMs, cue.startMs + 300);
        }
    }

    addLog(StudioData::OralStage::Compose,
           QStringLiteral("封面海报已写入成片开头（0–%1 毫秒）")
               .arg(plan->cover.endMs),
           StudioData::Tone::Ok);
    return true;
}

bool PipelineService::addSubtitleTracks(const EditPlan &plan,
                                        CapCutClient *capcut,
                                        const CapCutDraft &draft, QString *error)
{
    // 标题花字：单独一条文本轨，出现在片头 2 秒。
    if (plan.subtitles.titleFx && !plan.subtitles.titleText.isEmpty()) {
        CapCutCaptionStyle style;
        style.fontSize = 14;
        style.textColor = QStringLiteral("#FFFFFF");
        style.borderColor = QStringLiteral("#000000");
        style.borderAlpha = 0.35;
        style.borderWidth = 20.0;
        style.alignment = 1;
        // capcut 草稿坐标里 transform.y 正方向是向上；标题放画面上方，
        // 正文（负值）留在下方，避免两条字幕轨在画面中间重叠。
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
            addLog(StudioData::OralStage::Compose,
                   QStringLiteral("标题花字写入失败，已跳过：%1").arg(titleError),
                   StudioData::Tone::Muted);
        } else {
            addLog(StudioData::OralStage::Compose,
                   QStringLiteral("生成片头标题「%1」").arg(title.text),
                   StudioData::Tone::Ai);
        }
    }

    if (plan.subtitles.cues.isEmpty()) {
        return true;
    }

    CapCutCaptionStyle style;
    style.alignment = 1;
    QVector<CapCutCaption> captions;
    captions.reserve(plan.subtitles.cues.size());
    switch (plan.subtitles.preset) {
    case SubtitlePreset::Keyword:
        style.fontSize = 11;
        style.textColor = QStringLiteral("#FFFFFF");
        style.borderColor = QStringLiteral("#000000");
        style.borderAlpha = 0.35;
        style.borderWidth = 18.0;
        style.transformY = -plan.canvas.height * 0.40;
        break;
    case SubtitlePreset::Big:
        style.fontSize = 16;
        style.textColor = QStringLiteral("#FFFFFF");
        style.borderColor = QStringLiteral("#000000");
        style.borderAlpha = 0.45;
        style.borderWidth = 26.0;
        style.transformY = -plan.canvas.height * 0.36;
        style.bold = true;
        break;
    case SubtitlePreset::Minimal:
        style.fontSize = 9;
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
        if (!cue.keywords.isEmpty() && plan.subtitles.preset == SubtitlePreset::Keyword) {
            caption.keyword = cue.keywords.join(QLatin1Char('|'));
            caption.keywordColor = QStringLiteral("#6E8BFF");
            caption.keywordFontSize = style.fontSize + 2;
        }
        caption.inAnimation = cue.animation;
        caption.inAnimationDurationUs = 160000;
        captions.append(caption);
    }
    if (!capcut->addCaptions(draft, captions, style, error)) {
        return false;
    }
    addLog(StudioData::OralStage::Compose,
           QStringLiteral("已写入 %1 条关键词字幕").arg(captions.size()),
           StudioData::Tone::Ok);
    return true;
}

bool PipelineService::addAudioTracks(const EditPlan &plan,
                                     CapCutClient *capcut,
                                     const CapCutDraft &draft, QString *error)
{
    // 先音效：贴字幕起点，音量 0.1。
    if (plan.audio.sfxEnabled && !plan.audio.sfx.isEmpty()) {
        QVector<CapCutAudio> sfx;
        for (const AudioCue &cue : plan.audio.sfx) {
            CapCutAudio audio;
            audio.url = cue.url;
            audio.startUs = cue.startMs * 1000;
            audio.endUs = cue.endMs * 1000;
            audio.durationUs = qMax<qint64>(0, cue.durationMs) * 1000;
            audio.volume = cue.volume;
            sfx.append(audio);
        }
        QString sfxError;
        if (capcut->addAudios(draft, sfx, &sfxError)) {
            addLog(StudioData::OralStage::Compose,
                   QStringLiteral("已写入 %1 处音效").arg(sfx.size()),
                   StudioData::Tone::Info);
        } else {
            addLog(StudioData::OralStage::Compose,
                   QStringLiteral("音效写入失败，已跳过：%1").arg(sfxError),
                   StudioData::Tone::Muted);
        }
    }

    // 再 BGM：短于成片时循环铺满，音量 0.12。
    if (plan.audio.bgmEnabled && !plan.audio.bgm.url.isEmpty()) {
        const QVector<AudioCue> segments =
            buildBgmSegments(plan.audio.bgm, plan.durationMs);
        QVector<CapCutAudio> audios;
        for (const AudioCue &cue : segments) {
            CapCutAudio audio;
            audio.url = cue.url;
            audio.startUs = cue.startMs * 1000;
            audio.endUs = cue.endMs * 1000;
            audio.durationUs = cue.durationMs * 1000;
            audio.volume = cue.volume;
            audios.append(audio);
        }
        QString bgmError;
        if (capcut->addAudios(draft, audios, &bgmError)) {
            addLog(StudioData::OralStage::Compose,
                   QStringLiteral("已写入 BGM「%1」%2")
                       .arg(plan.audio.bgm.name)
                       .arg(segments.size() > 1
                                ? QStringLiteral(" · 循环 %1 段")
                                      .arg(segments.size())
                                : QString()),
                   StudioData::Tone::Info);
        } else {
            addLog(StudioData::OralStage::Compose,
                   QStringLiteral("BGM 写入失败，已跳过：%1").arg(bgmError),
                   StudioData::Tone::Muted);
        }
    }
    Q_UNUSED(error);
    return true;
}

/* ------------------------------------------------------------------ */
/* 渲染                                                                 */
/* ------------------------------------------------------------------ */

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
    setStage(StudioData::OralStage::Render, StudioData::StageStatus::Running, 2,
             QStringLiteral("提交云渲染"));
    if (!capcut->genVideo(draft, m_request.apiKey, error)) {
        return false;
    }
    addLog(StudioData::OralStage::Render,
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
            addLog(StudioData::OralStage::Render,
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
            setStage(StudioData::OralStage::Render,
                     StudioData::StageStatus::Running,
                     qBound(3.0, 3.0 + progress * 0.72, 75.0),
                     QStringLiteral("云渲染 %1%").arg(progress));
            m_run.renderedFrames =
                qMin(m_run.totalFrames,
                     int(qint64(m_run.totalFrames) * progress / 100));
            if (progress >= lastReported + 20) {
                lastReported = progress;
                addLog(StudioData::OralStage::Render,
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

    // 取回最终的 video_url；状态接口在 completed 时才返回地址。
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

    setStage(StudioData::OralStage::Render, StudioData::StageStatus::Running, 82,
             QStringLiteral("下载成片"));
    addLog(StudioData::OralStage::Render,
           QStringLiteral("渲染完成，正在下载成片…"), StudioData::Tone::Ok);
    QString downloadError;
    HttpClient http;
    const bool downloaded = http.downloadToFile(
        QUrl(finalStatus.videoUrl), outputPath, &downloadError, 30 * 60 * 1000,
        [this](qint64 received, qint64 total) {
            const int percent = total > 0
                ? int(received * 100 / total)
                : 0;
            setStage(StudioData::OralStage::Render,
                     StudioData::StageStatus::Running,
                     qBound(83.0, 83.0 + percent * 0.10, 93.0),
                     QStringLiteral("下载成片 %1%").arg(percent));
        },
        [this] { return isCancelled(); });
    if (!downloaded) {
        if (error) {
            *error = QStringLiteral("成片下载失败：%1").arg(downloadError);
        }
        return false;
    }
    const QFileInfo outputInfo(outputPath);
    addLog(StudioData::OralStage::Render,
           QStringLiteral("成片已保存：%1 · %2")
               .arg(outputPath, formatFileSize(outputInfo.size())),
           StudioData::Tone::Ok);
    m_run.renderedFrames = m_run.totalFrames;

    // 输出「成片 + 剪映草稿」时自动下载草稿；失败不阻断成片，结果页仍可重试。
    if (plan.outputBoth) {
        setStage(StudioData::OralStage::Render, StudioData::StageStatus::Running,
                 95, QStringLiteral("下载剪映草稿"));
        QString localDraftPath;
        QString draftError;
        const QString title = projectNameFor(plan);
        if (capcut->downloadDraft(m_draftId, title, m_request.draftRoot,
                                  &localDraftPath, &draftError,
                                  [this](int percent, const QString &detail) {
                                      setStage(
                                          StudioData::OralStage::Render,
                                          StudioData::StageStatus::Running,
                                          qBound(95.0, 95.0 + percent * 0.04, 99.0),
                                          detail);
                                  })) {
            *draftPath = localDraftPath;
            addLog(StudioData::OralStage::Render,
                   QStringLiteral("剪映草稿已写入：%1").arg(localDraftPath),
                   StudioData::Tone::Ok);
        } else {
            addLog(StudioData::OralStage::Render,
                   QStringLiteral("剪映草稿下载失败，可在结果页重试：%1")
                       .arg(draftError),
                   StudioData::Tone::Muted);
        }
    }
    return true;
}

/* ------------------------------------------------------------------ */
/* 结果                                                                 */
/* ------------------------------------------------------------------ */

StudioData::OralResult PipelineService::makeResult(const EditPlan &plan,
                                                   const QString &videoPath,
                                                   const QString &draftPath,
                                                   const QString &previewPath) const
{
    StudioData::OralResult result;
    const qint64 now = QDateTime::currentMSecsSinceEpoch();
    result.id = QStringLiteral("oral-%1").arg(now);
    result.projectName = projectNameFor(plan);
    result.title = plan.semantics.title;
    result.description = plan.semantics.description;
    result.tags = plan.semantics.tags;
    result.templateId = plan.templateId;
    result.ratio = plan.canvas.ratio;
    result.durationLabel = StudioData::formatElapsed(plan.durationMs);
    result.fileSize = formatFileSize(QFileInfo(videoPath).size());
    result.outputBoth = plan.outputBoth;
    result.generatedAt = now;
    result.sourceLabel = QStringLiteral("真人口播 · %1").arg(result.durationLabel);
    result.script = plan.speech.text;
    result.subtitleCount = plan.subtitles.cues.size();
    result.brollCount = plan.shots.shots.size();
    result.draftGenerated = !draftPath.isEmpty();
    result.filePath = videoPath;
    result.previewPath = previewPath;
    result.draftPath = draftPath;
    result.draftId = m_draftId;
    result.editPlanPath = m_editPlanPath;
    result.videoCodec = videoCodecName(videoPath);
    result.sourcePath = plan.sourcePath;
    result.materials = m_request.project.materials;

    int transitionCount = 0;
    for (const Shot &shot : plan.shots.shots) {
        if (!shot.transition.isEmpty()) {
            ++transitionCount;
        }
    }
    result.highlights = {
        {QStringLiteral("字幕"),
         QStringLiteral("%1 条 · %2")
             .arg(result.subtitleCount)
             .arg(subtitlePresetName(plan.subtitles.preset))},
        {QStringLiteral("素材"),
         QStringLiteral("%1 处 B-Roll").arg(result.brollCount)},
        {QStringLiteral("节奏"),
         QStringLiteral("%1 个转场").arg(transitionCount)},
        {QStringLiteral("模板"), QStringLiteral("清透讲解")},
        {QStringLiteral("音效"),
         QStringLiteral("%1 处").arg(plan.audio.sfx.size())},
        {QStringLiteral("BGM"),
         !plan.audio.bgm.name.isEmpty()
             ? plan.audio.bgm.name
             : (plan.audio.bgmEnabled ? QStringLiteral("未获取")
                                      : QStringLiteral("未启用"))},
        {QStringLiteral("语义"),
         QStringLiteral("%1 个段落").arg(plan.semantics.segments.size())},
        {QStringLiteral("说话人"),
         QStringLiteral("%1 位").arg(qMax(1, plan.speech.speakers.size()))},
    };
    return result;
}

} // namespace Oral
