#pragma once

// 文件作用：草稿下载任务的注册表与线程编排。
// 每个草稿最多一个下载任务，任务在独立线程运行，界面通过 jobChanged 获取快照。
// 关闭下载弹窗不会中断任务；应用退出时统一取消并等待线程收尾。

#include "../data/ClipTypes.h"
#include "DraftDownloadJob.h"

#include <QHash>
#include <QObject>
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

signals:
    void jobChanged(const DraftDownloadJob &job);
    void jobRemoved(qint64 draftRowId);
    void jobFinished(const DraftDownloadJob &job);
    void toastRequested(const QString &message);

private:
    struct Runtime
    {
        DraftDownloadJob job;
        QString databasePath;
        QString draftRoot;
        QThread *thread = nullptr;
        DraftDownloader *downloader = nullptr;
    };

    Runtime *findRuntime(qint64 draftRowId) const;
    void launch(Runtime *runtime);
    void onJobChanged(qint64 draftRowId, const DraftDownloadJob &job);
    void onFinished(qint64 draftRowId, bool success, const QString &message,
                    const QString &localPath);
    void onCancelled(qint64 draftRowId);

    QHash<qint64, Runtime *> m_runtimes;
};
