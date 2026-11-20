#pragma once

// 文件作用：导出成片任务的注册表与线程编排。
// 每个草稿最多一个导出任务，任务在独立线程运行，界面通过 jobChanged 获取快照。
// 批量导出固定 2 路并发，其余排队；关闭导出弹窗不会中断任务。

#include "../data/ClipTypes.h"
#include "VideoExportJob.h"

#include <QHash>
#include <QObject>
#include <QSet>
#include <QVector>

class QElapsedTimer;
class QThread;
class VideoExporter;

class VideoExportManager : public QObject
{
    Q_OBJECT

public:
    explicit VideoExportManager(QObject *parent = nullptr);
    ~VideoExportManager() override;

    // 开始导出；同一草稿的进行中任务直接返回。
    void start(const ClipDraft &draft, const QString &apiKey,
               const QString &exportDir);
    // 已导出的成片文件被移动或删除时重新导出。
    void restart(const ClipDraft &draft, const QString &apiKey,
                 const QString &exportDir);
    void retry(qint64 draftRowId);
    // 取消会移除任务记录（已生成的画面由云端保留）。
    void cancel(qint64 draftRowId);

    bool hasJob(qint64 draftRowId) const;
    VideoExportJob job(qint64 draftRowId) const;
    QVector<VideoExportJob> jobs() const;

    // 批量导出成片：固定 2 路并发。
    void startBatch(const QVector<ClipDraft> &drafts, const QString &apiKey,
                    const QString &exportDir);
    void cancelBatch();
    void retryBatchFailed();
    bool batchRunning() const { return m_batchRunning; }
    bool isBatchJob(qint64 draftRowId) const;
    VideoBatchSnapshot batch() const { return m_batch; }

signals:
    void jobChanged(const VideoExportJob &job);
    void jobRemoved(qint64 draftRowId);
    void jobFinished(const VideoExportJob &job);
    void toastRequested(const QString &message);
    void batchChanged(const VideoBatchSnapshot &batch);
    void batchFinished(const VideoBatchSnapshot &batch);
    void batchCleared();

private:
    struct Runtime
    {
        ClipDraft draft;
        VideoExportJob job;
        QString apiKey;
        QString exportDir;
        QThread *thread = nullptr;
        VideoExporter *exporter = nullptr;
    };

    static constexpr int kBatchConcurrency = 2;

    Runtime *findRuntime(qint64 draftRowId) const;
    void launch(Runtime *runtime, const QString &resumeVideoUrl,
                qint64 baseElapsedMs);
    void onJobChanged(qint64 draftRowId, const VideoExportJob &job);
    void onFinished(qint64 draftRowId, bool success, const QString &message,
                    const QString &outputPath);
    void onCancelled(qint64 draftRowId);

    void launchQueuedBatchItems();
    void updateBatchFromJob(const VideoExportJob &job);
    void recomputeBatch();
    void finishBatchIfIdle();
    VideoBatchItem *batchItem(qint64 draftRowId);

    QHash<qint64, Runtime *> m_runtimes;
    VideoBatchSnapshot m_batch;
    QHash<qint64, ClipDraft> m_batchDrafts;
    QVector<qint64> m_batchQueue;
    QSet<qint64> m_batchActive;
    QSet<qint64> m_batchCancelIds;
    QString m_batchApiKey;
    QString m_batchExportDir;
    bool m_batchRunning = false;
    bool m_batchCancelling = false;
    int m_batchCounter = 0;
    QElapsedTimer *m_batchClock = nullptr;
};
