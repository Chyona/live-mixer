#pragma once

// 文件作用：智能口播流水线（工作线程任务）。
// 一键包装开始前先执行「剪口气」：用内置提示词删除语气词，并删除超过
// 0.5 秒的静音，得到一条连续口播视频；随后严格对应产品三步：
// 1. 理解：语音分析（ASR）+ 素材分析 + 语义分析 + 镜头规划 + 字幕/音频系统；
// 2. 生成：固化成统一剪辑计划 Edit Plan / Timeline JSON；
// 3. 合成：调用 capcut-mate 落轨、云渲染并下载成片（可选同时下载剪映草稿）。
//
// 该对象会被移动到独立 QThread 中执行，通过 runUpdated / finished 信号把
// 进度与结果传回界面线程。所有分析结果按源视频指纹缓存，重新包装时不会
// 重复上传或重复识别。

#include "../data/StudioData.h"
#include "OralTrimPlan.h"
#include "OralTypes.h"

#include <QObject>
#include <QString>

#include <atomic>

class HttpClient;

namespace Oral {

class CapCutClient;
class AsrClient;
class LlmClient;
class AudioAssetsClient;
struct CapCutDraft;

struct PipelineRequest
{
    int runId = 0;
    QString apiKey;
    QString gatewayBaseUrl; // OpenAI 兼容网关，同时用于 ASR 与 LLM
    StudioData::OralProject project;
    QString cacheDir;  // 分析缓存与剪辑计划
    QString outputDir; // 成片输出目录
    QString draftRoot; // 剪映草稿根目录
};

class PipelineService : public QObject
{
    Q_OBJECT

public:
    explicit PipelineService(const PipelineRequest &request,
                             QObject *parent = nullptr);

public slots:
    void run();
    // 线程安全：只设置取消标记，由流水线在阶段边界退出。
    void cancel();

signals:
    // 完整的过程快照：阶段、总进度、日志、关键词、匹配素材数与渲染帧数。
    void runUpdated(const StudioData::OralRun &run);
    // success=false 时 message 是面向用户的失败原因；result 内容不可用。
    void finished(bool success, const QString &message,
                  const StudioData::OralResult &result);

private:
    // 阶段进度辅助。
    void initRun();
    void setStage(StudioData::OralStage stage, StudioData::StageStatus status,
                  double progress, const QString &detail);
    void addLog(StudioData::OralStage stage, const QString &text,
                StudioData::Tone tone);
    void emitRun();
    void updateOverallProgress();
    bool isCancelled() const { return m_cancelled.load(); }

    // 1. 理解阶段。
    // mediaSourcePath 是实际进入成片的口播视频（剪口气之后的连续视频）；
    // 原片路径始终保留在 plan->originalSourcePath，供重新包装回到原片。
    bool ensureAnalysis(EditPlan *plan, bool *fromCache, AsrClient *asr,
                        LlmClient *llm, CapCutClient *capcut,
                        const QString &mediaSourcePath,
                        const SpeechAnalysis &precomputedSpeech, QString *error);
    bool loadAnalysisCache(const QString &cachePath, EditPlan *plan);
    bool saveAnalysisCache(const QString &cachePath, const EditPlan &plan,
                           QString *error);
    QString analysisCacheKey(const QString &sourcePath) const;

    // 剪口气：先识别原片，按内置提示词删除语气词，并删除超过 0.5 秒的
    // 静音；随后按保留区间裁剪拼接成一条连续视频。无需剪除时返回原片。
    // sourceSpeech 在本次实际识别过原片且未剪除时返回 ASR 结果，供理解阶段
    // 直接复用，避免同一条原片识别两次；命中剪口缓存时为空。
    bool trimFillerWords(const QString &sourcePath, AsrClient *asr,
                         LlmClient *llm, CapCutClient *capcut,
                         QString *outputPath, TrimStats *stats,
                         SpeechAnalysis *sourceSpeech, QString *error);
    // ASR 首选豆包极速版；未开通时回退标准版 / qwen，供剪口气与理解阶段共用。
    bool transcribeSpeech(const QString &audioPath, AsrClient *asr,
                          CapCutClient *capcut, SpeechAnalysis *speech,
                          QString *error);
    bool analyzeMaterialsLocal(const QVector<StudioData::OralMaterial> &materials,
                               QVector<MaterialProfile> *profiles, QString *error);
    void normalizeSemanticTimes(SemanticAnalysis *analysis, qint64 durationMs);

    // 2. 生成阶段：字幕、音频与镜头计划。
    bool buildAudioPlan(const EditPlan &plan, AudioPlan *audio,
                        AudioAssetsClient *assets, LlmClient *llm,
                        QString *error);

    // 3. 合成阶段：上传、落轨、渲染、下载。
    bool composeDraft(EditPlan *plan, CapCutClient *capcut, HttpClient *http,
                      const QString &workDir, CapCutDraft *draft, QString *error);
    bool addBrollTracks(EditPlan *plan, CapCutClient *capcut,
                        const CapCutDraft &draft, const QString &workDir,
                        QString *error);
    // 封面提示词 2：提示词 1 + 口播内容 → DeepSeek；每次成片都重新生成，
    // 调用失败时退回本地兜底提示词。
    bool ensureCoverPrompt(EditPlan *plan, HttpClient *http);
    // 封面海报：取口播首帧 → seedream 图生图 → 上传并写到最上层图片轨。
    // 生成失败只记录日志，不阻断成片。
    bool addCoverTrack(EditPlan *plan, CapCutClient *capcut,
                       const CapCutDraft &draft, const QString &workDir,
                       HttpClient *http);
    bool addSubtitleTracks(const EditPlan &plan, CapCutClient *capcut,
                           const CapCutDraft &draft, QString *error);
    bool addAudioTracks(const EditPlan &plan, CapCutClient *capcut,
                        const CapCutDraft &draft, QString *error);
    bool renderAndDownload(const EditPlan &plan, CapCutClient *capcut,
                           const CapCutDraft &draft, const QString &outputPath,
                           QString *draftPath, QString *error);
    StudioData::OralResult makeResult(const EditPlan &plan,
                                      const QString &videoPath,
                                      const QString &draftPath,
                                      const QString &previewPath) const;

    PipelineRequest m_request;
    StudioData::OralRun m_run;
    qint64 m_startedAtMs = 0;
    QString m_workDir;
    QString m_analysisFrameDir;
    QString m_editPlanPath;
    QString m_draftId;
    std::atomic_bool m_cancelled{false};
};

} // namespace Oral
