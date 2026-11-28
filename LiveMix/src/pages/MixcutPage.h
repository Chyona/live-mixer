#pragma once

// 智能混剪工作区：首页、素材与文案单页工作台、AI 混剪过程与成片结果。
// 页面只负责交互与展示；真正的素材分析、TTS、ASR、镜头规划、智能包装
// 与剪映草稿合成由 Mixcut::PipelineService 在独立线程执行。

#include "../data/StudioData.h"

#include <QVector>
#include <QWidget>

class ElidedLabel;
class QLabel;
class QScrollArea;
class QStackedWidget;
class QTextEdit;
class QToolButton;
class QTimer;
class QVBoxLayout;
class QHBoxLayout;
class QMediaPlayer;
class QAudioOutput;
class QGraphicsView;
class QGraphicsVideoItem;
class QThread;
class Button;
class GlyphButton;
class ProgressBar;
class SeekBar;
class ConfigStore;

namespace Mixcut {
class PipelineService;
}

namespace Studio {
class MixPosterWidget;
class StageSummaryCard;
class StepRail;
class SegmentedControl;
class Waveform;
class VoiceOrbWidget;
}

class MixcutPage : public QWidget
{
    Q_OBJECT

public:
    explicit MixcutPage(ConfigStore *config = nullptr, QWidget *parent = nullptr);
    ~MixcutPage() override;

signals:
    void toastRequested(const QString &message);
    void navigateRequested(const QString &pageId);
    void taskStatusChanged(const QString &text, bool running);
    // 音色示例音的播放状态变化，音色库与工作台音色卡同步刷新。
    void voiceSampleChanged();

private:
    enum class Screen { Hub, Studio, Processing, Result };

    void buildUi();
    void showHub();
    void showStudio();
    void showProcessing();
    void showResult();

    void rebuildHub();
    void rebuildStudio();
    void buildProcessingScreen();
    void rebuildResult();

    QWidget *buildHubHero();
    QWidget *buildHubProcessPanel();
    QWidget *createRecentCard(const StudioData::MixcutResult &result, QWidget *parent);
    QWidget *buildStudioHeader();
    QWidget *buildMaterialCard();
    QWidget *buildOutputCard();
    QWidget *buildScriptCard();
    QWidget *buildVoiceSection();
    QWidget *buildStudioFooter();

    bool startProject();
    void addMaterialsFromDialog();
    void addMaterialPaths(const QStringList &paths, bool announce);
    void appendMaterials(const QVector<StudioData::MixcutMaterial> &materials,
                         bool announce);
    void addSampleMaterials();
    void removeMaterial(const QString &id);
    void clearMaterials();
    void chooseStyle(StudioData::MixcutStyle style);
    void chooseVoice(const QString &voiceId);
    void chooseRatio(const QString &ratio);
    void openVoicePicker();
    void openPromptEditor();
    void toggleVoiceSample(const QString &voiceId);
    void stopVoiceSample();
    void updatePromptFromEditor();
    void updateScriptFromEditor();
    void generateFromPrompt();
    void regenerateScript();
    void startScriptJob(qint64 durationMs, bool rewriting,
                        const QString &announce = QString());
    void tickScript();
    bool scriptJobRunning() const;
    void refreshStudioState();
    void refreshScriptMeta();
    void refreshScriptProgress();
    void refreshVoiceSection();
    void refreshFooter();
    int activeStep() const;
    bool canStart() const;
    bool hasMaterials() const;
    QString headerDescription() const;

    void startGeneration();
    void updateProcessing();
    // 真实流水线回调：过程快照、成功/失败收尾。
    void onPipelineRunUpdated(const StudioData::MixcutRun &run);
    void onPipelineFinished(bool success, const QString &message,
                            const StudioData::MixcutResult &result);
    void destroyPipeline();
    void loadHistory();
    void saveHistory();
    void openRecent(const StudioData::MixcutResult &result);
    void restartFromResult();
    void togglePlayback();
    void updatePlaybackControls();
    void updatePlayback();
    void finishPlayback();
    void resetResultPlayer();
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
    QWidget *m_studioScreen = nullptr;
    QWidget *m_processingScreen = nullptr;
    QWidget *m_resultScreen = nullptr;

    ConfigStore *m_config = nullptr;
    Mixcut::PipelineService *m_pipelineService = nullptr;
    QThread *m_pipelineThread = nullptr;
    bool m_pipelineRunning = false;
    QVector<StudioData::MixcutResult> m_history;
    QVector<StudioData::MixcutResult> m_persistedHistory;

    StudioData::MixcutProject m_project;
    bool m_scriptEdited = false;
    int m_scriptVariant = 0;
    bool m_scriptRewrite = false;
    int m_analysisProgress = 0;
    qint64 m_scriptStartedAt = 0;
    qint64 m_scriptDuration = 1800;
    QTimer *m_scriptTimer = nullptr;

    // 音色示例音试听（音色库与工作台共用同一播放器）。
    QMediaPlayer *m_samplePlayer = nullptr;
    QAudioOutput *m_sampleAudio = nullptr;
    QString m_sampleVoiceId;

    // 工作台动态控件
    QScrollArea *m_studioScroll = nullptr;
    QLabel *m_studioDescription = nullptr;
    QLabel *m_studioStepLabel = nullptr;
    Studio::StepRail *m_stepRail = nullptr;
    QLabel *m_outputHint = nullptr;
    QLabel *m_deliveryHint = nullptr;

    Studio::SegmentedControl *m_styleControl = nullptr;
    QTextEdit *m_promptEdit = nullptr;
    QTextEdit *m_scriptEdit = nullptr;
    QWidget *m_scriptSkeleton = nullptr;
    QLabel *m_scriptSkeletonProgress = nullptr;
    QWidget *m_scriptRewriteOverlay = nullptr;
    QLabel *m_scriptRewriteLabel = nullptr;
    QLabel *m_scriptRewriteProgress = nullptr;
    ProgressBar *m_scriptRewriteBar = nullptr;
    QLabel *m_scriptMeta = nullptr;
    QLabel *m_scriptStats = nullptr;
    QLabel *m_scriptSave = nullptr;
    Button *m_generateButton = nullptr;
    Button *m_regenerateButton = nullptr;
    bool m_settingText = false;

    QLabel *m_voiceMeta = nullptr;
    Studio::VoiceOrbWidget *m_voiceOrb = nullptr;
    QLabel *m_voiceName = nullptr;
    QLabel *m_voiceScene = nullptr;
    QLabel *m_voiceLanguage = nullptr;
    QWidget *m_voiceRecommendTag = nullptr;
    Button *m_voiceSampleButton = nullptr;
    QLabel *m_voiceSynthesisHint = nullptr;

    ElidedLabel *m_footerSummary = nullptr;
    QLabel *m_footerHint = nullptr;
    Button *m_startButton = nullptr;

    bool m_hasRun = false;
    StudioData::MixcutRun m_run;
    int m_runId = 0;

    QLabel *m_progressLabel = nullptr;
    QLabel *m_processingMeta = nullptr;
    QLabel *m_etaLabel = nullptr;
    Studio::MixPosterWidget *m_processingPoster = nullptr;
    QVector<Studio::StageSummaryCard *> m_stageCards;
    Studio::Waveform *m_processingVoiceWave = nullptr;
    QLabel *m_processingVoiceLabel = nullptr;
    QHBoxLayout *m_processingScenesLayout = nullptr;
    QLabel *m_processingScenesCount = nullptr;
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
    QString m_lastLogId;
    int m_lastScenesCount = -1;

    bool m_hasResult = false;
    StudioData::MixcutResult m_result;
    Studio::MixPosterWidget *m_resultPoster = nullptr;
    QLabel *m_resultGenerationLabel = nullptr;
    QLabel *m_resultPlayClock = nullptr;
    Button *m_resultPlayButton = nullptr;
    QToolButton *m_resultCenterPlay = nullptr;
    GlyphButton *m_resultPlayToggle = nullptr;
    SeekBar *m_resultPlayBar = nullptr;
    // 真实成片播放：海报与播放器画面共用同一尺寸的堆叠控件。
    QStackedWidget *m_resultMediaStack = nullptr;
    QGraphicsView *m_resultVideoView = nullptr;
    QGraphicsVideoItem *m_resultVideoItem = nullptr;
    QMediaPlayer *m_resultPlayer = nullptr;
    QAudioOutput *m_resultAudio = nullptr;
    QString m_resultFilePath;
    QString m_resultDraftPath;
    bool m_resultHasVideo = false;
    bool m_playing = false;
    int m_playProgress = 0;
    // 拖动进度条期间暂停跟随播放器位置，松开后按原播放状态恢复。
    bool m_resultSeeking = false;
    bool m_resultResumeAfterSeek = false;
    QTimer *m_playTimer = nullptr;

    QTimer *m_clockTimer = nullptr;
};
