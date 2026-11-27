#pragma once

// 智能口播工作区：首页、真人口播分步创建、AI 包装过程与成片结果。
// 当前只实现交互界面与本地过程模拟，不调用真实的 ASR / TTS / 渲染服务。

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
class ProgressBar;

namespace Studio {
class OralPosterWidget;
class StageSummaryCard;
class MaterialVisualWidget;
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
    void clearSource();
    void addMaterials(const QStringList &paths);
    void removeMaterial(const QString &id);
    void importVideoFromPaths(const QStringList &paths);
    void importVideoFile(const QString &path);
    void probeSourceMetadata(const QString &path);
    void previewMaterial(const StudioData::OralMaterial &material);
    void ensureMaterialPreviews();
    void requestVideoThumbnail(const QString &path);
    void startNextThumbnailJob();
    void applyMaterialThumbnail(const QString &path, const QImage &image);
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

    // 视频素材缩略图：顺序生成，完成后直接刷新对应的缩略图控件。
    QStringList m_thumbnailQueue;
    QString m_thumbnailInFlight;
    QProcess *m_thumbnailProcess = nullptr;
    QSet<QString> m_failedPreviewPaths;
    // 按文件路径记录素材步骤中的缩略图控件；每次重建步骤时重新登记。
    QHash<QString, QVector<Studio::MaterialVisualWidget *>> m_previewWidgets;
    // 口播视频卡片的缩略图/元数据控件；元数据异步探测完成后就地刷新。
    std::function<void(const StudioData::OralSource &)> m_sourcePreviewUpdater;
    QLabel *m_sourceMetaLabel = nullptr;

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
