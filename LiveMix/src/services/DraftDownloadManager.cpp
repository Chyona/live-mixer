// 文件作用：草稿下载任务管理实现。
#include "DraftDownloadManager.h"

#include "DraftDownloader.h"
#include "../core/MediaUtils.h"

#include <QDebug>
#include <QThread>

DraftDownloadManager::DraftDownloadManager(QObject *parent)
    : QObject(parent)
{
    qRegisterMetaType<DraftDownloadJob>("DraftDownloadJob");
}

DraftDownloadManager::~DraftDownloadManager()
{
    for (Runtime *runtime : m_runtimes) {
        if (runtime->downloader) {
            runtime->downloader->cancel();
        }
        if (runtime->thread) {
            runtime->thread->requestInterruption();
            runtime->thread->quit();
            if (runtime->thread->isRunning() && !runtime->thread->wait(1500)) {
                // 网络请求无法立即中断时不再 terminate，让线程随进程退出回收。
                qWarning() << "Draft download still running at exit; detaching";
            }
        }
        delete runtime;
    }
    m_runtimes.clear();
}

void DraftDownloadManager::start(const ClipDraft &draft, const QString &databasePath,
                                 const QString &draftRoot)
{
    if (Runtime *existing = findRuntime(draft.id)) {
        // 进行中的任务不重复启动；已完成/失败的任务在草稿目录被删除后可重新下载。
        const bool finished = existing->job.status == DraftDownloadStatus::Done
            || existing->job.status == DraftDownloadStatus::Error;
        if (!finished || (existing->thread && existing->thread->isRunning())) {
            return;
        }
        m_runtimes.remove(draft.id);
        delete existing;
    }

    auto *runtime = new Runtime;
    runtime->databasePath = databasePath;
    runtime->draftRoot = draftRoot;
    runtime->job.draftRowId = draft.id;
    runtime->job.title = draft.title;
    runtime->job.draftName = draft.draftId;
    runtime->job.rangeText = MediaUtils::formatDuration(draft.sourceStartMs);
    runtime->job.durationText = MediaUtils::formatDuration(draft.durationMs);
    runtime->job.status = DraftDownloadStatus::Downloading;
    runtime->job.stage = DraftDownloadStage::Config;

    m_runtimes.insert(draft.id, runtime);
    emit jobChanged(runtime->job);
    launch(runtime);
}

void DraftDownloadManager::retry(qint64 draftRowId)
{
    Runtime *runtime = findRuntime(draftRowId);
    if (!runtime || runtime->job.status != DraftDownloadStatus::Error) {
        return;
    }
    if (runtime->thread && runtime->thread->isRunning()) {
        // 上一次线程尚未退出，等它收尾后用户可再次重试。
        return;
    }

    runtime->job.status = DraftDownloadStatus::Downloading;
    runtime->job.stage = DraftDownloadStage::Config;
    runtime->job.progress = 0.0;
    runtime->job.loadedBytes = 0;
    runtime->job.totalBytes = 0;
    runtime->job.doneFiles = 0;
    runtime->job.totalFiles = 0;
    runtime->job.speed = 0.0;
    runtime->job.etaSeconds = -1;
    runtime->job.error.clear();
    runtime->job.files.clear();

    emit jobChanged(runtime->job);
    launch(runtime);
}

void DraftDownloadManager::cancel(qint64 draftRowId)
{
    Runtime *runtime = findRuntime(draftRowId);
    if (!runtime || runtime->job.status != DraftDownloadStatus::Downloading) {
        return;
    }
    if (runtime->downloader) {
        runtime->downloader->cancel();
    }
}

bool DraftDownloadManager::hasJob(qint64 draftRowId) const
{
    return m_runtimes.contains(draftRowId);
}

DraftDownloadJob DraftDownloadManager::job(qint64 draftRowId) const
{
    Runtime *runtime = findRuntime(draftRowId);
    return runtime ? runtime->job : DraftDownloadJob();
}

QVector<DraftDownloadJob> DraftDownloadManager::jobs() const
{
    QVector<DraftDownloadJob> result;
    result.reserve(m_runtimes.size());
    for (Runtime *runtime : m_runtimes) {
        result.append(runtime->job);
    }
    return result;
}

DraftDownloadManager::Runtime *DraftDownloadManager::findRuntime(
    qint64 draftRowId) const
{
    return m_runtimes.value(draftRowId, nullptr);
}

void DraftDownloadManager::launch(Runtime *runtime)
{
    const qint64 draftRowId = runtime->job.draftRowId;
    auto *downloader = new DraftDownloader(runtime->databasePath, runtime->draftRoot,
                                           draftRowId);
    auto *thread = new QThread;
    downloader->moveToThread(thread);
    runtime->downloader = downloader;
    runtime->thread = thread;

    connect(thread, &QThread::started, downloader, &DraftDownloader::download);
    connect(downloader, &DraftDownloader::jobChanged, this,
            [this, draftRowId](const DraftDownloadJob &job) {
                onJobChanged(draftRowId, job);
            });
    connect(downloader, &DraftDownloader::finished, this,
            [this, draftRowId](bool success, const QString &message,
                               const QString &localPath) {
                onFinished(draftRowId, success, message, localPath);
            });
    connect(downloader, &DraftDownloader::cancelled, this, [this, draftRowId] {
        onCancelled(draftRowId);
    });
    connect(thread, &QThread::finished, this,
            [this, draftRowId, downloader, thread, runtime] {
                // 取消时 Runtime 已被释放，此时只需回收线程对象。
                if (Runtime *current = m_runtimes.value(draftRowId, nullptr);
                    current == runtime) {
                    if (current->downloader == downloader) {
                        current->downloader = nullptr;
                    }
                    if (current->thread == thread) {
                        current->thread = nullptr;
                    }
                }
                downloader->deleteLater();
                thread->deleteLater();
            });

    thread->start();
}

void DraftDownloadManager::onJobChanged(qint64 draftRowId,
                                        const DraftDownloadJob &job)
{
    Runtime *runtime = findRuntime(draftRowId);
    if (!runtime) {
        return;
    }

    // 工作线程在读取数据库失败时拿不到标题草稿信息，沿用界面侧的元数据，
    // 避免弹窗与卡片突然变成空白。
    DraftDownloadJob next = job;
    if (next.draftRowId < 0) {
        next.draftRowId = draftRowId;
    }
    if (next.title.isEmpty()) {
        next.title = runtime->job.title;
    }
    if (next.draftName.isEmpty()) {
        next.draftName = runtime->job.draftName;
    }
    if (next.rangeText.isEmpty()) {
        next.rangeText = runtime->job.rangeText;
    }
    if (next.durationText.isEmpty()) {
        next.durationText = runtime->job.durationText;
    }

    runtime->job = next;
    emit jobChanged(runtime->job);
}

void DraftDownloadManager::onFinished(qint64 draftRowId, bool success,
                                      const QString &message,
                                      const QString &localPath)
{
    Runtime *runtime = findRuntime(draftRowId);
    if (!runtime) {
        return;
    }

    if (success) {
        runtime->job.status = DraftDownloadStatus::Done;
        runtime->job.stage = DraftDownloadStage::Done;
        runtime->job.progress = 100.0;
        runtime->job.localPath = localPath;
        runtime->job.speed = 0.0;
        runtime->job.etaSeconds = -1;
    } else {
        runtime->job.status = DraftDownloadStatus::Error;
        runtime->job.error = message;
        runtime->job.speed = 0.0;
        runtime->job.etaSeconds = -1;
    }

    if (runtime->thread) {
        runtime->thread->quit();
    }

    emit jobChanged(runtime->job);
    emit jobFinished(runtime->job);
}

void DraftDownloadManager::onCancelled(qint64 draftRowId)
{
    Runtime *runtime = m_runtimes.take(draftRowId);
    if (!runtime) {
        return;
    }
    if (runtime->thread) {
        runtime->thread->quit();
    }
    delete runtime;

    emit jobRemoved(draftRowId);
    emit toastRequested(QStringLiteral("已取消下载"));
}
