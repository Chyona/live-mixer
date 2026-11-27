#pragma once

// 智能口播工作区：首页、真人口播分步创建、AI 包装过程与成片结果。
// 当前只实现交互界面与本地过程模拟，不调用真实的 ASR / TTS / 渲染服务。

#include "../data/StudioData.h"

#include <QWidget>

class QLabel;
class ElidedLabel;
class QToolButton;
class QScrollArea;
class QStackedWidget;
class QTimer;
class QVBoxLayout;
class QHBoxLayout;
class Button;
class ProgressBar;

namespace Studio {
class OralPosterWidget;
class StageSummaryCard;
}

class OralPage : public QWidget
{
    Q_OBJECT

public:
    explicit OralPage(QWidget *parent = nullptr);
    ~OralPage() override;

signals:
    void toastRequested(const QString &message);
    void navigateRequested(const QString &pageId);
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
    void openVideoFile();
    void openMaterialFiles();
    void setSourceSample();
    void clearSource();
    void addMaterials(const QStringList &paths);
    void removeMaterial(const QString &id);
    void chooseTemplate(const QString &templateId);
    void startGeneration();
    void updateProcessing();
    void finishGeneration();
    void openRecent(const StudioData::OralResult &result);
    void restartFromResult();

    void applyRatio(const QString &ratio);
    void applyOutput(bool both);

    void togglePlayback();
    void updatePlayback();
    void startDraftGeneration();
    void updateDraftGeneration();

    void updateGenerationClock();
    void copyText(const QString &text, const QString &label);

    bool isRunning() const;

    Screen m_screen = Screen::Hub;
    QStackedWidget *m_stack = nullptr;
    QWidget *m_hubScreen = nullptr;
    QScrollArea *m_hubScroll = nullptr;
    QWidget *m_hubContent = nullptr;
    QVBoxLayout *m_hubLayout = nullptr;
    QWidget *m_setupScreen = nullptr;
    QWidget *m_processingScreen = nullptr;
    QWidget *m_resultScreen = nullptr;

    StudioData::OralProject m_project;
    bool m_transcriptExpanded = false;

    bool m_hasRun = false;
    StudioData::OralRun m_run;
    int m_runId = 0;
    qint64 m_runStartedAt = 0;
    QTimer *m_runTimer = nullptr;

    // 过程页控件
    QLabel *m_progressLabel = nullptr;
    QLabel *m_processingMeta = nullptr;
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
    ProgressBar *m_resultPlayBar = nullptr;
    Button *m_resultDraftButton = nullptr;
    QLabel *m_resultDraftHint = nullptr;
    QString m_resultFilePath;
    QString m_resultDraftPath;
    bool m_playing = false;
    int m_playProgress = 0;
    QTimer *m_playTimer = nullptr;
    int m_draftState = 0; // 0 idle / 1 running / 2 done
    int m_draftProgress = 0;
    QTimer *m_draftTimer = nullptr;

    QTimer *m_clockTimer = nullptr;
};
