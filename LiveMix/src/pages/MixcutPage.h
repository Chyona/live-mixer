#pragma once

// 智能混剪工作区：首页、文案与配音工作台、AI 混剪过程与成片结果。
// 当前只交付交互界面与本地过程模拟，不调用真实的视频理解 / TTS / 渲染服务。

#include "../data/StudioData.h"

#include <QWidget>

class ElidedLabel;
class QLabel;
class QScrollArea;
class QStackedWidget;
class QTextEdit;
class QToolButton;
class GlyphButton;
class QTimer;
class QVBoxLayout;
class QHBoxLayout;
class Button;
class ProgressBar;

namespace Studio {
class MixPosterWidget;
class StageSummaryCard;
class StepRail;
class SegmentedControl;
class Waveform;
}

class VoiceChoiceButton;

class MixcutPage : public QWidget
{
    Q_OBJECT

public:
    explicit MixcutPage(QWidget *parent = nullptr);
    ~MixcutPage() override;

signals:
    void toastRequested(const QString &message);
    void navigateRequested(const QString &pageId);
    void taskStatusChanged(const QString &text, bool running);

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
    QWidget *createHubVoiceCard(const QString &voiceId, QWidget *parent);
    QWidget *buildUploadPanel();
    QWidget *buildWorkbench();
    QWidget *buildStudioHeader();
    QWidget *buildPreviewCard();
    QWidget *buildSourceCard();
    QWidget *buildScriptCard();
    QWidget *buildVoiceSection();
    QWidget *buildAutoPackaging();
    QWidget *buildStudioFooter();

    void startProject();
    void importVideoFile();
    void importSource(const StudioData::MixcutSource &source, bool announce);
    void clearSource();
    void chooseStyle(StudioData::MixcutStyle style);
    void chooseVoice(const QString &voiceId);
    void chooseRatio(const QString &ratio);

    void startAnalysis(qint64 durationMs, const QString &announce);
    void tickAnalysis();
    void startVoiceJob();
    void tickVoice();
    void scheduleVoiceSync();
    void refreshStudioState();
    void refreshAnalysisProgress();
    void refreshVoicePanel();
    void refreshScriptStats();
    void refreshPreview();
    void updateScriptFromEditor();
    void updatePromptFromEditor();
    void regenerateScript();
    void generateFromPrompt();
    void regenerateVoice();
    void openPromptEditor();
    void togglePlayback();
    void updatePlayback();

    void startGeneration();
    void updateProcessing();
    void finishGeneration();
    void openRecent(const StudioData::MixcutResult &result);
    void restartFromResult();

    void updateGenerationClock();
    void copyText(const QString &text, const QString &label);
    bool isRunning() const;
    int activeStep() const;
    bool canStart() const;
    QString activeCaption() const;

    Screen m_screen = Screen::Hub;
    QStackedWidget *m_stack = nullptr;

    QWidget *m_hubScreen = nullptr;
    QScrollArea *m_hubScroll = nullptr;
    QWidget *m_hubContent = nullptr;
    QVBoxLayout *m_hubLayout = nullptr;
    QWidget *m_studioScreen = nullptr;
    QWidget *m_processingScreen = nullptr;
    QWidget *m_resultScreen = nullptr;

    StudioData::MixcutProject m_project;
    int m_scriptVariant = 0;
    bool m_scriptEdited = false;
    bool m_settingScriptText = false;
    int m_analysisProgress = 0;
    int m_voiceProgress = 0;
    qint64 m_analysisStartedAt = 0;
    qint64 m_analysisDuration = 2600;
    QTimer *m_analysisTimer = nullptr;
    QTimer *m_voiceTimer = nullptr;
    QTimer *m_voiceDebounce = nullptr;

    // 工作台动态控件
    QLabel *m_studioDescription = nullptr;
    QLabel *m_studioStepLabel = nullptr;
    Studio::StepRail *m_stepRail = nullptr;
    Studio::MixPosterWidget *m_previewPoster = nullptr;
    QToolButton *m_previewPlayButton = nullptr;
    QWidget *m_previewAnalysisOverlay = nullptr;
    QLabel *m_previewAnalysisLabel = nullptr;
    QLabel *m_previewAnalysisProgress = nullptr;
    ProgressBar *m_previewAnalysisBar = nullptr;
    QWidget *m_previewSkeleton = nullptr;
    Studio::SegmentedControl *m_ratioControl = nullptr;
    QLabel *m_previewStatusLabel = nullptr;
    QLabel *m_previewMeta = nullptr;
    ProgressBar *m_playBar = nullptr;
    GlyphButton *m_previewRowPlay = nullptr;
    QLabel *m_playClock = nullptr;
    QLabel *m_playDuration = nullptr;

    Studio::SegmentedControl *m_styleControl = nullptr;
    QTextEdit *m_promptEdit = nullptr;
    QTextEdit *m_scriptEdit = nullptr;
    QWidget *m_scriptOverlay = nullptr;
    QLabel *m_scriptOverlayLabel = nullptr;
    QLabel *m_scriptOverlayDetail = nullptr;
    QLabel *m_scriptOverlayProgress = nullptr;
    ProgressBar *m_scriptOverlayBar = nullptr;
    QWidget *m_scriptSkeleton = nullptr;
    QLabel *m_scriptMeta = nullptr;
    QLabel *m_scriptStats = nullptr;
    QLabel *m_scriptSave = nullptr;
    Button *m_generateButton = nullptr;
    Button *m_regenerateButton = nullptr;

    QVector<VoiceChoiceButton *> m_voiceCards;
    Studio::Waveform *m_voiceWave = nullptr;
    GlyphButton *m_voicePlay = nullptr;
    QLabel *m_voiceMeta = nullptr;
    QLabel *m_voiceDurationLabel = nullptr;
    QLabel *m_voiceNameLabel = nullptr;
    QLabel *m_voiceStatus = nullptr;
    QWidget *m_voiceStatusDot = nullptr;
    GlyphButton *m_voiceRegenerate = nullptr;
    ProgressBar *m_voiceProgressBar = nullptr;

    ElidedLabel *m_footerSummary = nullptr;
    QLabel *m_footerHint = nullptr;
    Button *m_startButton = nullptr;

    bool m_playing = false;
    int m_playProgress = 0;
    QTimer *m_playTimer = nullptr;

    bool m_hasRun = false;
    StudioData::MixcutRun m_run;
    int m_runId = 0;
    qint64 m_runStartedAt = 0;
    QTimer *m_runTimer = nullptr;

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
    int m_lastScenesCount = -1;

    bool m_hasResult = false;
    StudioData::MixcutResult m_result;
    Studio::MixPosterWidget *m_resultPoster = nullptr;
    QLabel *m_resultGenerationLabel = nullptr;
    QLabel *m_resultPlayClock = nullptr;
    Button *m_resultPlayButton = nullptr;
    QToolButton *m_resultCenterPlay = nullptr;
    ProgressBar *m_resultPlayBar = nullptr;
    QString m_resultFilePath;
    QString m_resultDraftPath;

    QTimer *m_clockTimer = nullptr;
};
