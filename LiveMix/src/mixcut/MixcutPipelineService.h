#pragma once

// 文件作用：智能混剪流水线（工作线程任务）。
// 严格对应产品六步：
// 1. 素材智能整理：视频场景切片、图片分析、本地画面特征、大模型标签与质量评分；
// 2. AI 语音理解：脚本 → TTS 配音 → ASR → 语义分段、关键词、情绪、叙事结构；
// 3. AI 镜头规划：语音片段与素材匹配、主镜头顺序/时长、画中画布局；
// 4. 智能包装：字幕、动画、转场、BGM、音效、特效与滤镜；
// 5. 时间轴编排：固化成 Edit Plan / Timeline JSON；
// 6. 合成：调用 capcut-mate 落轨、云渲染、下载成片与剪映草稿。
//
// 该对象会被移动到独立 QThread 中执行，通过 runUpdated / finished 信号把
// 进度与结果传回界面线程。

#include "../data/StudioData.h"
#include "MixcutCapCutClient.h"
#include "MixcutTypes.h"

#include <QObject>
#include <QString>

#include <atomic>

class HttpClient;

namespace Mixcut {

class CapCutClient;
class TtsClient;
class LlmClient;

struct PipelineRequest
{
    int runId = 0;
    QString apiKey;
    QString gatewayBaseUrl; // OpenAI 兼容网关，同时用于 TTS / ASR / LLM
    StudioData::MixcutProject project;
    QString cacheDir;
    QString outputDir;
    QString draftRoot;
    // 全局成片设置（设置页 → 成片设置）。
    bool transitions = true;
    bool effects = true;
    bool filters = true;
    bool bgm = true;
    bool sfx = true;
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
    // 完整的过程快照：阶段、总进度、日志、关键词、镜头数与渲染帧数。
    void runUpdated(const StudioData::MixcutRun &run);
    // success=false 时 message 是面向用户的失败原因；result 内容不可用。
    void finished(bool success, const QString &message,
                  const StudioData::MixcutResult &result);

private:
    // 阶段进度辅助。
    void initRun();
    void setStage(StudioData::MixcutStage stage, StudioData::StageStatus status,
                  double progress, const QString &detail);
    void addLog(StudioData::MixcutStage stage, const QString &text,
                StudioData::Tone tone);
    void emitRun();
    bool isCancelled() const { return m_cancelled.load(); }
    int stageIndex(StudioData::MixcutStage stage) const;

    // 1. 素材整理。
    bool analyzeMaterials(HttpClient *http, MaterialLibrary *library,
                          QString *error);
    void fallbackClipAnalysis(MaterialLibrary *library) const;

    // 2. 语音理解。
    bool understandSpeech(HttpClient *http, LlmClient *llm, EditPlan *plan,
                          QString *error);
    bool transcribeSpeech(EditPlan *plan, QString *error);
    void buildFallbackSpeech(EditPlan *plan) const;
    bool analyzeSemantics(LlmClient *llm, EditPlan *plan, QString *error);
    void buildFallbackSemantics(EditPlan *plan) const;

    // 3. 镜头规划。
    bool planTimeline(LlmClient *llm, EditPlan *plan, QString *error);

    // 4. 智能包装。
    bool buildPackaging(HttpClient *http, LlmClient *llm, EditPlan *plan,
                        QString *error);
    bool buildAudioPlan(HttpClient *http, LlmClient *llm, EditPlan *plan,
                        QString *error);
    void buildEffectsAndFilters(EditPlan *plan) const;

    // 5. 时间轴编排。
    bool finalizeTimeline(EditPlan *plan, QString *error);
    bool savePlan(const EditPlan &plan, QString *error);

    // 6. 合成。
    bool composeDraft(EditPlan *plan, CapCutClient *capcut,
                      const QString &workDir, CapCutDraft *draft,
                      QString *error);
    bool addMainTracks(EditPlan *plan, CapCutClient *capcut,
                       const CapCutDraft &draft, const QString &workDir,
                       QString *error);
    bool addOverlayTracks(EditPlan *plan, CapCutClient *capcut,
                          const CapCutDraft &draft, const QString &workDir,
                          QString *error);
    bool addSubtitleTracks(const EditPlan &plan, CapCutClient *capcut,
                           const CapCutDraft &draft, QString *error);
    bool addAudioTracks(const EditPlan &plan, CapCutClient *capcut,
                        const CapCutDraft &draft, QString *error);
    bool addEffectTracks(const EditPlan &plan, CapCutClient *capcut,
                         const CapCutDraft &draft, QString *error);
    bool renderAndDownload(const EditPlan &plan, CapCutClient *capcut,
                           const CapCutDraft &draft, const QString &outputPath,
                           QString *draftPath, QString *error);
    StudioData::MixcutResult makeResult(const EditPlan &plan,
                                        const QString &videoPath,
                                        const QString &draftPath,
                                        const QString &previewPath) const;

    PipelineRequest m_request;
    StudioData::MixcutRun m_run;
    qint64 m_startedAtMs = 0;
    QString m_workDir;
    QString m_planPath;
    QString m_draftId;
    qint64 m_logSequence = 0;
    std::atomic_bool m_cancelled{false};
};

} // namespace Mixcut
