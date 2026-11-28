#pragma once

// 智能口播工作区：首页、真人口播分步创建、AI 包装过程与成片结果。
// 页面只负责交互与展示；真正的 ASR、大模型分析、剪映草稿与云渲染由
// Oral::PipelineService 在独立线程执行，两者通过信号连接。

#include "../data/StudioData.h"

#include <QHash>
#include <QSet>
#include <QWidget>

#include <functional>

class QLabel;
class ElidedLabel;
class QToolButton;
class QScrollArea;
class QStackedWidget;
class QTimer;
class QVBoxLayout;
class QHBoxLayout;
class QProcess;
class Button;
class GlyphButton;
class ProgressBar;
class SeekBar;
class ConfigStore;
class QThread;
class QMediaPlayer;
class QAudioOutput;
class QGraphicsVideoItem;
class QGraphicsView;

namespace Oral {
class PipelineService;
class DraftDownloadTask;
}

namespace Studio {
class OralPosterWidget;
class StageSummaryCard;
class MaterialVisualWidget;
}

class OralPage : public QWidget
{
    Q_OBJECT

public:
    explicit OralPage(ConfigStore *config = nullptr, QWidget *parent = nullptr);
    ~OralPage() override;

signals:
    void toastRequested(const QString &message);
    void navigateRequested(const QString &pageId);
    // 素材预览：由主窗口弹出视频播放或图片预览。
    void previewRequested(const QString &title, const QString &filePath,
                          bool video);
    // 底部状态栏的 AI 任务入口；running=false 表示结束并收起。
    void taskStatusChanged(const QString &text, bool running);

private:
    enum class Screen { Hub, Setup, Processing, Result };

    void buildUi();
    void showHub();
    void showSetup();
    void showProcessing();
    void showResult();

    void rebuildHub();
    void rebuildSetup();
    void buildProcessingScreen();
    void rebuildResult();

    QWidget *buildHubHeader();
    // 离开首页时记录列表滚动位置，返回时恢复。
    void rememberHubScroll();

    QWidget *buildSetupHeader();
    QWidget *buildSetupStepOne();
    QWidget *buildSetupStepTwo();
    QWidget *buildSetupFooter();
    QWidget *buildHubProcessPanel();
    QWidget *buildHubHero();
    QWidget *createTemplateCard(const QString &templateId, bool selected,
                                bool compact, QWidget *parent);
    QWidget *createRecentCard(const StudioData::OralResult &result, QWidget *parent);

    void startProject();
    // 用示例口播与示例素材直接体验完整包装流程。
    void startSampleProject();
    void openNewProject(bool sample);
    void openVideoFile();
    void openMaterialFiles();
    void clearSource();
    void addMaterials(const QStringList &paths);
    void removeMaterial(const QString &id);
    void importVideoFromPaths(const QStringList &paths);
    void importVideoFile(const QString &path);
    void probeSourceMetadata(const QString &path);
    void previewMaterial(const StudioData::OralMaterial &material);
    void ensureMaterialPreviews();
    void requestVideoThumbnail(const QString &path);
    // 最近口播卡片：成片首帧缺失时补截 0 秒帧并就地刷新海报。
    void requestPosterFrame(const QString &path);
    void requestPreviewFrame(const QString &path, bool materialFrame);
    void startNextThumbnailJob();
    void applyMaterialThumbnail(const QString &path, const QImage &image);
    void applyPosterFrame(const QString &path, const QImage &image);
    void chooseTemplate(const QString &templateId);
    void startGeneration();
    void updateProcessing();
    // 真实流水线回调：过程快照、成功/失败收尾。
    void onPipelineRunUpdated(const StudioData::OralRun &run);
    void onPipelineFinished(bool success, const QString &message,
                            const StudioData::OralResult &result);
    void destroyPipeline();
    void loadHistory();
    void saveHistory();
    void startDraftDownload();
    void onDraftProgress(int percent, const QString &detail);
    void onDraftFinished(bool success, const QString &localPath,
                         const QString &error);
    void openDraftFolder();
    void openExportFolder();
    void openResultVideoPreview();
    void resetResultPlayer();
    void openRecent(const StudioData::OralResult &result);
    void restartFromResult();

    void applyRatio(const QString &ratio);
    void applyOutput(bool both);

    void togglePlayback();
    void updatePlayback();
    // 播放按钮、中心悬浮按钮与底部控制条的图标/文案统一跟随 m_playing。
    void updatePlaybackControls();
    // 播放结束：切回海报并把进度、时间码与控制条一起归零。
    void finishPlayback();
    // 进度条拖动定位：permille 为 0–1000 的千分比。
    void seekResultTo(int permille);

    void updateGenerationClock();
    void copyText(const QString &text, const QString &label);

    bool isRunning() const;

    Screen m_screen = Screen::Hub;
    QStackedWidget *m_stack = nullptr;
    QWidget *m_hubScreen = nullptr;
    QScrollArea *m_hubScroll = nullptr;
    QWidget *m_hubContent = nullptr;
    QVBoxLayout *m_hubLayout = nullptr;
    // 全部已完成的口播项目，最新的在前；新生成的成片自动置顶。
    QVector<StudioData::OralResult> m_history;
    // 真实生成记录单独持久化；演示数据每次启动追加在真实记录之后。
    QVector<StudioData::OralResult> m_persistedHistory;
    int m_hubScrollTop = 0;
    QWidget *m_setupScreen = nullptr;
    QWidget *m_processingScreen = nullptr;
    QWidget *m_resultScreen = nullptr;

    StudioData::OralProject m_project;

    // 预览截帧任务：素材缩略图取 0.5 秒帧，最近口播封面取成片 0 秒首帧。
    // 共用一条串行队列，避免同时启动多个 ffmpeg 进程。
    enum class PreviewFrameKind { Material, Poster };
    struct PreviewFrameJob {
        QString path;
        PreviewFrameKind kind = PreviewFrameKind::Material;
    };
    QVector<PreviewFrameJob> m_thumbnailQueue;
    PreviewFrameJob m_thumbnailInFlight;
    QProcess *m_thumbnailProcess = nullptr;
    QSet<QString> m_failedPreviewPaths;
    // 按文件路径记录素材步骤中的缩略图控件；每次重建步骤时重新登记。
    QHash<QString, QVector<Studio::MaterialVisualWidget *>> m_previewWidgets;
    // 按成片路径记录最近口播卡片的海报控件；每次重建首页时重新登记。
    QHash<QString, QVector<Studio::OralPosterWidget *>> m_recentPosterWidgets;
    // 口播视频卡片的缩略图/元数据控件；元数据异步探测完成后就地刷新。
    std::function<void(const StudioData::OralSource &)> m_sourcePreviewUpdater;
    QLabel *m_sourceMetaLabel = nullptr;

    bool m_hasRun = false;
    StudioData::OralRun m_run;
    int m_runId = 0;
    qint64 m_runStartedAt = 0;
    QTimer *m_runTimer = nullptr;

    // 真实流水线：每次生成创建一个工作线程，信号排队回到界面线程。
    ConfigStore *m_config = nullptr;
    QThread *m_pipelineThread = nullptr;
    Oral::PipelineService *m_pipelineService = nullptr;
    bool m_pipelineRunning = false;
    // 结果页「生成草稿」下载任务。
    QThread *m_draftThread = nullptr;
    Oral::DraftDownloadTask *m_draftTask = nullptr;

    // 过程页控件
    QLabel *m_progressLabel = nullptr;
    QLabel *m_processingMeta = nullptr;
    QLabel *m_processingProjectLabel = nullptr;
    QLabel *m_etaLabel = nullptr;
    Studio::OralPosterWidget *m_processingPoster = nullptr;
    QVector<Studio::StageSummaryCard *> m_stageCards;
    QLabel *m_keywordStatus = nullptr;
    QHBoxLayout *m_keywordLayout = nullptr;
    QHBoxLayout *m_matchedLayout = nullptr;
    QLabel *m_matchedCount = nullptr;
    QLabel *m_processingSourceMeta = nullptr;
    QLabel *m_logStageTitle = nullptr;
    ElidedLabel *m_logStageDetail = nullptr;
    QLabel *m_logStageProgress = nullptr;
    ProgressBar *m_logStageBar = nullptr;
    QScrollArea *m_logScroll = nullptr;
    QWidget *m_logContent = nullptr;
    QVBoxLayout *m_logLayout = nullptr;
    QLabel *m_logFooter = nullptr;
    QLabel *m_logStepsLabel = nullptr;
    QLabel *m_logElapsedLabel = nullptr;
    int m_logCount = -1;
    int m_lastKeywordCount = -1;
    int m_lastMatchedCount = -1;

    // 结果页
    bool m_hasResult = false;
    StudioData::OralResult m_result;
    Studio::OralPosterWidget *m_resultPoster = nullptr;
    QLabel *m_resultGenerationLabel = nullptr;
    QLabel *m_resultPlayClock = nullptr;
    Button *m_resultPlayButton = nullptr;
    QToolButton *m_resultCenterPlay = nullptr;
    GlyphButton *m_resultPlayToggle = nullptr;
    SeekBar *m_resultPlayBar = nullptr;
    Button *m_resultDraftButton = nullptr;
    QLabel *m_resultDraftHint = nullptr;
    QString m_resultFilePath;
    QString m_resultDraftPath;
    bool m_playing = false;
    int m_playProgress = 0;
    // 拖动进度条期间暂停跟随播放器位置，松开后按原播放状态恢复。
    bool m_resultSeeking = false;
    bool m_resultResumeAfterSeek = false;
    QTimer *m_playTimer = nullptr;
    int m_draftState = 0; // 0 idle / 1 running / 2 done
    int m_draftProgress = 0;

    // 结果页真实视频播放：海报与播放器放在同一个堆叠控件里，点击播放时切换。
    QStackedWidget *m_resultMediaStack = nullptr;
    QGraphicsView *m_resultVideoView = nullptr;
    QGraphicsVideoItem *m_resultVideoItem = nullptr;
    QMediaPlayer *m_resultPlayer = nullptr;
    QAudioOutput *m_resultAudio = nullptr;
    bool m_resultHasVideo = false;

    QTimer *m_clockTimer = nullptr;
};
