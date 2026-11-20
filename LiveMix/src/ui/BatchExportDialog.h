#pragma once

// 文件作用：批量导出弹窗，同时服务「批量导出到剪映」与「批量导出成片」。
// 两者使用相同的队列、总进度与失败重试结构，仅阶段文案、并发说明与
// 完成后的输出目录不同。

#include "../services/DraftDownloadJob.h"
#include "../services/VideoExportJob.h"
#include "Overlays.h"

#include <QVector>

class BatchRow;
class Chip;
class QLabel;
class ProgressBar;
class QScrollArea;

class BatchExportDialog : public ModalCard
{
    Q_OBJECT

public:
    enum class Mode {
        Draft,
        Video,
    };

    struct ViewItem
    {
        QString title;
        QString durationText;
        int status = 0; // 0=排队, 1=进行中, 2=完成, 3=失败
        int stage = 0;  // VideoExportStage；草稿模式不使用
        bool hasStage = false;
        double progress = 0.0;
        qint64 loadedBytes = 0;
        qint64 totalBytes = 0;
        QString error;
    };

    BatchExportDialog(Mode mode, const QString &folderPath,
                      QWidget *parent = nullptr);

    Mode mode() const { return m_mode; }
    void setDraftBatch(const DraftBatchSnapshot &batch);
    void setVideoBatch(const VideoBatchSnapshot &batch);

signals:
    void cancelRequested();
    void retryFailedRequested();
    void openFolderRequested(const QString &path);
    void toastRequested(const QString &message);
    void layoutChanged();

private:
    bool m_modeDraft() const { return m_mode == Mode::Draft; }
    bool running() const { return m_running; }
    bool partial() const { return !m_running && m_failedCount > 0; }
    int itemCount() const { return m_items.size(); }

    void rebuildContent();
    void rebuildFooter();
    void refresh();

    QWidget *buildBanner(QWidget *parent);
    QWidget *buildFolderRow(QWidget *parent);
    QWidget *buildSummary(QWidget *parent);
    QWidget *buildList(QWidget *parent);

    Mode m_mode;
    QString m_folderPath;

    bool m_running = true;
    int m_batchId = -1;
    int m_doneCount = 0;
    int m_failedCount = 0;
    int m_activeCount = 0;
    int m_queuedCount = 0;
    double m_progress = 0.0;
    qint64 m_loadedBytes = 0;
    qint64 m_totalBytes = 0;
    qint64 m_elapsedMs = 0;
    int m_etaSeconds = -1;
    QVector<ViewItem> m_items;

    Chip *m_chip = nullptr;
    QLabel *m_countLabel = nullptr;
    QLabel *m_hintLabel = nullptr;
    QLabel *m_summaryLabel = nullptr;
    QLabel *m_percentLabel = nullptr;
    ProgressBar *m_progressBar = nullptr;
    QLabel *m_summaryLeft = nullptr;
    QLabel *m_summaryRight = nullptr;
    QLabel *m_listHeaderLeft = nullptr;
    QLabel *m_listHeaderRight = nullptr;
    QScrollArea *m_listScroll = nullptr;
    QVector<BatchRow *> m_rows;
    int m_lastActiveRow = -1;
};
