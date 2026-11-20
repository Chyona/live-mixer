#pragma once

// 文件作用：「导出成片」MP4 导出弹窗。
// 展示「合成画面 → 编码成片 → 下载到本地」三步进度、输出规格与路径，
// 以及完成后的定位入口。关闭弹窗不会中断导出，可从结果页或状态栏重新打开。

#include "../services/VideoExportJob.h"
#include "Overlays.h"

class Chip;
class QLabel;
class ProgressBar;
class StageStepper;

class VideoExportDialog : public ModalCard
{
    Q_OBJECT

public:
    explicit VideoExportDialog(QWidget *parent = nullptr);

    void setJob(const VideoExportJob &job);
    qint64 draftRowId() const { return m_job.draftRowId; }

signals:
    void cancelRequested(qint64 draftRowId);
    void retryRequested(qint64 draftRowId);
    void revealRequested(const QString &path);
    void toastRequested(const QString &message);
    // 弹窗内容高度变化后通知遮罩层重新居中。
    void layoutChanged();

private:
    enum class Mode { Running, Done, Error };

    static Mode modeFor(const VideoExportJob &job);
    static QVector<int> stepStates(const VideoExportJob &job);
    static QString stageLabel(const VideoExportJob &job);

    void rebuildContent();
    void rebuildFooter();
    void refresh();

    QWidget *buildSuccessBanner(QWidget *parent);
    QWidget *buildErrorBanner(QWidget *parent);
    QWidget *buildOutputInfo(QWidget *parent);

    VideoExportJob m_job;
    Mode m_mode = Mode::Running;
    bool m_built = false;

    Chip *m_chip = nullptr;
    StageStepper *m_stepper = nullptr;
    QLabel *m_stageLabel = nullptr;
    QLabel *m_percentLabel = nullptr;
    ProgressBar *m_progress = nullptr;
    QLabel *m_detailLabel = nullptr;
    QLabel *m_metaLabel = nullptr;
    QLabel *m_outputSizeLabel = nullptr;
};
