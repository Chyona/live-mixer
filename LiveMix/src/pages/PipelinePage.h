#pragma once

// 文件作用：一键成片过程页。
// 页面按设计稿分为顶部总览、三阶段轨道、ASR / AI 选片 / 草稿生成三个过程面板
// 与底部状态栏；工作线程通过 PipelineUi::Event 持续推送真实过程数据。

#include "../data/PipelineTypes.h"

#include <QElapsedTimer>
#include <QString>
#include <QWidget>

class AsrPanelWidget;
class Button;
class Chip;
class DraftPanelWidget;
class ElidedLabel;
class QLabel;
class QStackedWidget;
class QTimer;
class SelectPanelWidget;
class StageRailWidget;
class StatusMark;

class PipelinePage : public QWidget
{
    Q_OBJECT

public:
    explicit PipelinePage(QWidget *parent = nullptr);

    bool hasRun() const;
    void resetRun();
    void handleEvent(const PipelineUi::Event &event);
    // 从任务中心打开仍在运行的任务：只恢复标题与当前步骤，细粒度过程从此刻开始。
    void showTaskSummary(const QString &title, int step, const QString &message,
                         int percent, qint64 elapsedMs);
    // 开发预览：切换过程面板。
    void showStep(int index);

signals:
    void toastRequested(const QString &message);
    void backgroundRequested();
    void viewResultsRequested();
    void rerunRequested();
    void startRunRequested();
    void materialRequested();

private:
    enum class RunStatus {
        Idle,
        Running,
        Done,
        Failed,
    };

    void buildEmptyPage();
    void buildRunPage();
    void setStage(PipelineUi::StageKey stage);
    void setFocusStage(int index);
    void updateSummary();
    void updateStageRail();
    QString stageMeta(PipelineUi::StageKey stage) const;
    QString footerText() const;
    int overallProgress() const;
    qint64 etaSeconds() const;
    static QString formatElapsed(qint64 milliseconds);
    static QString formatEta(qint64 seconds);

    QStackedWidget *m_pages = nullptr;
    QWidget *m_emptyPage = nullptr;
    QWidget *m_runPage = nullptr;

    // 顶部总览
    ElidedLabel *m_title = nullptr;
    Chip *m_titleChip = nullptr;
    QLabel *m_titleMeta = nullptr;
    QLabel *m_promptTextLabel = nullptr;
    QLabel *m_elapsedValue = nullptr;
    QLabel *m_etaDot = nullptr;
    QLabel *m_etaPrefix = nullptr;
    QLabel *m_etaValue = nullptr;
    QLabel *m_doneDot = nullptr;
    QLabel *m_doneStatus = nullptr;
    QLabel *m_hintDot = nullptr;
    QLabel *m_backgroundHint = nullptr;
    QLabel *m_overallValue = nullptr;
    QLabel *m_overallUnit = nullptr;
    Button *m_backstageButton = nullptr;
    Button *m_rerunButton = nullptr;
    Button *m_resultsButton = nullptr;

    StageRailWidget *m_stageRail = nullptr;
    QStackedWidget *m_panels = nullptr;
    AsrPanelWidget *m_asrPanel = nullptr;
    SelectPanelWidget *m_selectPanel = nullptr;
    DraftPanelWidget *m_draftPanel = nullptr;

    // 底部状态
    StatusMark *m_footerMark = nullptr;
    ElidedLabel *m_footerLabel = nullptr;
    QLabel *m_footerAsr = nullptr;
    QLabel *m_footerAi = nullptr;
    QLabel *m_footerDraft = nullptr;

    QTimer *m_clockTimer = nullptr;
    QElapsedTimer m_runClock;
    QElapsedTimer m_selectClock;

    RunStatus m_status = RunStatus::Idle;
    PipelineUi::StageKey m_stage = PipelineUi::StageKey::Asr;
    PipelineUi::StageStatus m_stageStatus[3] = {
        PipelineUi::StageStatus::Waiting,
        PipelineUi::StageStatus::Waiting,
        PipelineUi::StageStatus::Waiting,
    };
    int m_focus = 0;
    QString m_sourceTitle;
    QString m_promptLabel;
    QString m_errorMessage;
    qint64 m_sourceDurationMs = 0;
    qint64 m_finalElapsedMs = 0;
    qint64 m_clockOffsetMs = 0;
    int m_batchCount = 0;
    int m_finishedDrafts = 0;
    int m_savedDrafts = 0;
    qint64 m_tokens = 0;
    bool m_selectClockStarted = false;
    QVector<PipelineUi::Candidate> m_pickedCandidates;
    // 从任务中心恢复的粗粒度进度（没有本次会话的细粒度过程数据）。
    bool m_summaryMode = false;
    int m_summaryStagePercent = 0;
    QString m_summaryNote;
};
