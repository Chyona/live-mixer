#pragma once

// 文件作用：剪映草稿下载任务（工作线程）。
// 只有用户点击「打开草稿文件夹」时才调用：拉取草稿文件、把素材下载到本地，
// 改写草稿 JSON 中的路径，最后返回可直接打开的草稿目录。
// 草稿先写入隐藏暂存目录，完成后由 CapCutMateClient 原子改名到正式目录，
// 剪映无需重启即可发现新草稿。
// 下载过程中通过 jobChanged 发出完整的状态快照，供结果页、弹窗与状态栏使用。

#include "DraftDownloadJob.h"

#include <QElapsedTimer>
#include <QObject>
#include <QString>

#include <atomic>

struct DraftDownloadEvent;

class DraftDownloader : public QObject
{
    Q_OBJECT

public:
    DraftDownloader(const QString &databasePath, const QString &draftRoot,
                    qint64 draftRowId, QObject *parent = nullptr);

public slots:
    void download();
    // 线程安全：只设置取消标记，由下载循环与网络请求自行退出。
    void cancel();

signals:
    void jobChanged(const DraftDownloadJob &job);
    void finished(bool success, const QString &message, const QString &localPath);
    // 用户主动取消，与下载失败区分开。
    void cancelled();

private:
    void applyEvent(const DraftDownloadEvent &event);
    void recompute();
    void report(bool force = false);
    void fail(const QString &message);

    QString m_databasePath;
    QString m_draftRoot;
    qint64 m_draftRowId = -1;

    DraftDownloadJob m_job;
    std::atomic_bool m_cancelled{false};

    QElapsedTimer m_clock;
    qint64 m_lastReportMs = -1;
    qint64 m_lastSampleMs = -1;
    qint64 m_lastSampleBytes = 0;
    double m_speed = 0.0;
};
