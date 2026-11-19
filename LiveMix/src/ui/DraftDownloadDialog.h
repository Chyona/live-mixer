#pragma once

// 文件作用：「打开草稿文件夹」下载弹窗。
// 展示「下载草稿配置 → 下载素材 → 写入草稿目录」三步进度、逐文件状态，
// 以及完成后的草稿路径。关闭弹窗不会中断下载，可从结果页或状态栏重新打开。

#include "../services/DraftDownloadJob.h"
#include "Overlays.h"

#include <QVector>

class Chip;
class DraftFileRow;
class DraftStageStepper;
class QLabel;
class ProgressBar;
class QScrollArea;

class DraftDownloadDialog : public ModalCard
{
    Q_OBJECT

public:
    explicit DraftDownloadDialog(QWidget *parent = nullptr);

    void setJob(const DraftDownloadJob &job);
    qint64 draftRowId() const { return m_job.draftRowId; }

signals:
    void cancelRequested(qint64 draftRowId);
    void retryRequested(qint64 draftRowId);
    void openFolderRequested(const QString &path);
    void toastRequested(const QString &message);
    // 弹窗内容高度变化后通知遮罩层重新居中。
    void layoutChanged();

private:
    enum class Mode { Running, Done, Error };

    static Mode modeFor(const DraftDownloadJob &job);
    static QString stageText(const DraftDownloadJob &job);
    static QString chipText(const DraftDownloadJob &job);
    static int chipTone(const DraftDownloadJob &job);
    static QVector<int> stepStates(const DraftDownloadJob &job);

    void rebuildContent();
    void rebuildFooter();
    void refresh();

    QWidget *buildFileList(QWidget *parent);
    QWidget *buildSuccessBanner(QWidget *parent);
    QWidget *buildErrorBanner(QWidget *parent);

    DraftDownloadJob m_job;
    Mode m_mode = Mode::Running;

    DraftStageStepper *m_stepper = nullptr;
    QLabel *m_stageLabel = nullptr;
    QLabel *m_percentLabel = nullptr;
    ProgressBar *m_progress = nullptr;
    QLabel *m_bytesLabel = nullptr;
    QLabel *m_metaLabel = nullptr;

    QVector<DraftFileRow *> m_fileRows;
    QVector<int> m_fileIndexes;
    QScrollArea *m_fileScroll = nullptr;
    int m_lastActiveRow = -1;

    struct GroupHeader
    {
        bool config = true;
        QLabel *label = nullptr;
    };
    QVector<GroupHeader> m_groupHeaders;
};
