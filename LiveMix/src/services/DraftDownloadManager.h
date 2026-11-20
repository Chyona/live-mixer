#pragma once

// 文件作用：草稿下载任务的注册表与线程编排。
// 每个草稿最多一个下载任务，任务在独立线程运行，界面通过 jobChanged 获取快照。
// 关闭下载弹窗不会中断任务；应用退出时统一取消并等待线程收尾。

#include "../data/ClipTypes.h"
#include "DraftDownloadJob.h"

#include <QHash>
#include <QObject>
#include <QSet>
#include <QVector>

class DraftDownloader;
class QThread;

class DraftDownloadManager : public QObject
{
    Q_OBJECT

public:
    explicit DraftDownloadManager(QObject *parent = nullptr);
    ~DraftDownloadManager() override;

    // 开始下载；同一草稿的进行中任务直接返回，已完成/失败的任务可重新开始。
    void start(const ClipDraft &draft, const QString &databasePath,
               const QString &draftRoot);
    void retry(qint64 draftRowId);
    // 仅下载阶段可取消；完成或失败后调用无效果。
    void cancel(qint64 draftRowId);

    bool hasJob(qint64 draftRowId) const;
    DraftDownloadJob job(qint64 draftRowId) const;
    QVector<DraftDownloadJob> jobs() const;

    // ---------------------------------------------------------------------
    // 批量导出到剪映：固定 3 路并发，其余按顺序排队。
    // 单条失败只影响当前草稿，队列继续；已完成的草稿立即就绪。
    // ---------------------------------------------------------------------
    void startBatch(const QVector<ClipDraft> &drafts, const QString &databasePath,
                    const QString &draftRoot);
    void cancelBatch();
    void retryBatchFailed();
    bool batchRunning() const { return m_batchRunning; }
    bool isBatchJob(qint64 draftRowId) const;
    DraftBatchSnapshot batch() const { return m_batch; }

signals:
    void jobChanged(const DraftDownloadJob &job);
    void jobRemoved(qint64 draftRowId);
    void jobFinished(const DraftDownloadJob &job);
    void toastRequested(const QString &message);
    void batchChanged(const DraftBatchSnapshot &batch);
    void batchFinished(const DraftBatchSnapshot &batch);
    void batchCleared();

private:
    struct Runtime
    {
        DraftDownloadJob job;
        QString databasePath;
        QString draftRoot;
        QThread *thread = nullptr;
        DraftDownloader *downloader = nullptr;
    };

    static constexpr int kBatchConcurrency = 3;

    Runtime *findRuntime(qint64 draftRowId) const;
    void launch(Runtime *runtime);
    void onJobChanged(qint64 draftRowId, const DraftDownloadJob &job);
    void onFinished(qint64 draftRowId, bool success, const QString &message,
                    const QString &localPath);
    void onCancelled(qint64 draftRowId);

    // 批量任务内部状态。
    void launchBatchItem(qint64 draftRowId);
    void launchQueuedBatchItems();
    void updateBatchFromJob(const DraftDownloadJob &job);
    void recomputeBatch();
    void finishBatchIfIdle();
    DraftBatchItem *batchItem(qint64 draftRowId);

    QHash<qint64, Runtime *> m_runtimes;
    DraftBatchSnapshot m_batch;
    QHash<qint64, ClipDraft> m_batchDrafts;
    QVector<qint64> m_batchQueue;
    QSet<qint64> m_batchActive;
    QSet<qint64> m_batchFinishedItems;
    QString m_batchDatabasePath;
    QString m_batchDraftRoot;
    bool m_batchRunning = false;
    bool m_batchCancelling = false;
    QSet<qint64> m_batchCancelIds;
    int m_batchCounter = 0;
    class QElapsedTimer *m_batchClock = nullptr;
};
