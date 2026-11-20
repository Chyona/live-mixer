// 文件作用：草稿下载任务管理实现。
#include "DraftDownloadManager.h"

#include "DraftDownloader.h"
#include "../core/MediaUtils.h"

#include <QDebug>
#include <QElapsedTimer>
#include <QSet>
#include <QThread>

#include <cmath>

DraftDownloadManager::DraftDownloadManager(QObject *parent)
    : QObject(parent)
    , m_batchClock(new QElapsedTimer)
{
    qRegisterMetaType<DraftDownloadJob>("DraftDownloadJob");
    qRegisterMetaType<DraftBatchSnapshot>("DraftBatchSnapshot");
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
    delete m_batchClock;
    m_batchClock = nullptr;
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

void DraftDownloadManager::startBatch(const QVector<ClipDraft> &drafts,
                                      const QString &databasePath,
                                      const QString &draftRoot)
{
    if (m_batchRunning) {
        emit toastRequested(QStringLiteral("已有批量导出正在进行"));
        return;
    }

    QVector<ClipDraft> eligible;
    eligible.reserve(drafts.size());
    for (const ClipDraft &draft : drafts) {
        const Runtime *runtime = findRuntime(draft.id);
        const bool finishedOrError = runtime
            && (runtime->job.status == DraftDownloadStatus::Done
                || runtime->job.status == DraftDownloadStatus::Error);
        if (!runtime || (finishedOrError
                         && runtime->job.status == DraftDownloadStatus::Error)) {
            eligible.append(draft);
        }
    }

    if (eligible.isEmpty()) {
        emit toastRequested(QStringLiteral("所选草稿均已导出或正在导出"));
        return;
    }
    const int skipped = drafts.size() - eligible.size();
    if (skipped > 0) {
        emit toastRequested(
            QStringLiteral("已跳过 %1 个已导出或正在导出的草稿").arg(skipped));
    }

    m_batch = DraftBatchSnapshot();
    m_batch.id = ++m_batchCounter;
    m_batch.running = true;
    m_batchDrafts.clear();
    m_batchQueue.clear();
    m_batchActive.clear();
    m_batchFinishedItems.clear();
    m_batchDatabasePath = databasePath;
    m_batchDraftRoot = draftRoot;
    m_batchCancelling = false;
    m_batchRunning = true;
    m_batchClock->start();

    for (const ClipDraft &draft : eligible) {
        DraftBatchItem item;
        item.draftRowId = draft.id;
        item.title = draft.title.isEmpty() ? QStringLiteral("直播切片") : draft.title;
        item.durationText = MediaUtils::formatDuration(draft.durationMs);
        // 下载前不知道真实体积，先按 ~0.9 MB/s 的素材码率估算；
        // 真实大小在查询草稿文件后由任务快照回填。
        const qint64 seconds = qMax<qint64>(1, draft.durationMs / 1000);
        item.totalBytes = seconds * 900 * 1024;
        m_batch.items.append(item);
        m_batchDrafts.insert(draft.id, draft);
        m_batchQueue.append(draft.id);
    }

    launchQueuedBatchItems();
    recomputeBatch();
    emit batchChanged(m_batch);
}

void DraftDownloadManager::cancelBatch()
{
    if (!m_batchRunning) {
        return;
    }

    m_batchCancelling = true;
    const QList<qint64> active = m_batchActive.values();
    m_batchCancelIds.clear();
    for (qint64 draftRowId : active) {
        m_batchCancelIds.insert(draftRowId);
        cancel(draftRowId);
    }
    if (active.isEmpty()) {
        m_batchCancelling = false;
    }
    if (m_batchClock) {
        m_batchClock->invalidate();
    }
    m_batchRunning = false;
    m_batchQueue.clear();
    m_batchActive.clear();
    m_batchDrafts.clear();
    m_batch = DraftBatchSnapshot();
    emit batchCleared();
    emit toastRequested(QStringLiteral("已停止批量导出，已完成的草稿会保留"));
}

void DraftDownloadManager::retryBatchFailed()
{
    QVector<ClipDraft> retryable;
    for (const DraftBatchItem &item : m_batch.items) {
        if (item.status != DraftBatchItemStatus::Error) {
            continue;
        }
        const Runtime *runtime = findRuntime(item.draftRowId);
        if (runtime && runtime->job.status == DraftDownloadStatus::Done) {
            continue;
        }
        const ClipDraft draft = m_batchDrafts.value(item.draftRowId);
        if (draft.id >= 0) {
            retryable.append(draft);
        }
    }
    if (retryable.isEmpty()) {
        emit toastRequested(QStringLiteral("没有可重试的失败项"));
        return;
    }
    startBatch(retryable, m_batchDatabasePath, m_batchDraftRoot);
    emit toastRequested(QStringLiteral("正在重试 %1 条失败的草稿").arg(retryable.size()));
}

bool DraftDownloadManager::isBatchJob(qint64 draftRowId) const
{
    for (const DraftBatchItem &item : m_batch.items) {
        if (item.draftRowId == draftRowId) {
            return true;
        }
    }
    return false;
}

void DraftDownloadManager::launchBatchItem(qint64 draftRowId)
{
    if (m_batchCancelling) {
        return;
    }
    const ClipDraft draft = m_batchDrafts.value(draftRowId);
    if (draft.id < 0) {
        if (DraftBatchItem *item = batchItem(draftRowId)) {
            item->status = DraftBatchItemStatus::Error;
            item->error = QStringLiteral("缺少草稿记录");
        }
        m_batchFinishedItems.insert(draftRowId);
        return;
    }

    // 已在单独下载的任务直接加入批量并沿用其进度；其余重新启动。
    const Runtime *runtime = findRuntime(draftRowId);
    if (runtime && runtime->job.status == DraftDownloadStatus::Downloading) {
        m_batchActive.insert(draftRowId);
        updateBatchFromJob(runtime->job);
        return;
    }

    m_batchActive.insert(draftRowId);
    start(draft, m_batchDatabasePath, m_batchDraftRoot);
}

void DraftDownloadManager::launchQueuedBatchItems()
{
    while (m_batchActive.size() < kBatchConcurrency && !m_batchQueue.isEmpty()
           && !m_batchCancelling) {
        const qint64 draftRowId = m_batchQueue.takeFirst();
        const Runtime *runtime = findRuntime(draftRowId);
        if (runtime && runtime->job.status == DraftDownloadStatus::Done) {
            if (DraftBatchItem *item = batchItem(draftRowId)) {
                item->status = DraftBatchItemStatus::Done;
                item->progress = 100.0;
                item->loadedBytes = item->totalBytes;
            }
            m_batchFinishedItems.insert(draftRowId);
            continue;
        }
        launchBatchItem(draftRowId);
    }
}

DraftBatchItem *DraftDownloadManager::batchItem(qint64 draftRowId)
{
    for (DraftBatchItem &item : m_batch.items) {
        if (item.draftRowId == draftRowId) {
            return &item;
        }
    }
    return nullptr;
}

void DraftDownloadManager::updateBatchFromJob(const DraftDownloadJob &job)
{
    DraftBatchItem *item = batchItem(job.draftRowId);
    if (!item) {
        return;
    }

    item->progress = qBound(0.0, job.progress, 100.0);
    if (job.totalBytes > 0) {
        item->totalBytes = job.totalBytes;
    }
    item->loadedBytes = qMax<qint64>(0, job.loadedBytes);
    item->error = job.error;

    switch (job.status) {
    case DraftDownloadStatus::Downloading:
        item->status = DraftBatchItemStatus::Downloading;
        break;
    case DraftDownloadStatus::Writing:
        item->status = DraftBatchItemStatus::Writing;
        item->progress = 100.0;
        item->loadedBytes = item->totalBytes;
        break;
    case DraftDownloadStatus::Done:
        item->status = DraftBatchItemStatus::Done;
        item->progress = 100.0;
        item->loadedBytes = item->totalBytes;
        break;
    case DraftDownloadStatus::Error:
        item->status = DraftBatchItemStatus::Error;
        break;
    }
}

void DraftDownloadManager::recomputeBatch()
{
    m_batch.doneCount = 0;
    m_batch.failedCount = 0;
    m_batch.activeCount = 0;
    m_batch.queuedCount = 0;
    m_batch.loadedBytes = 0;
    m_batch.totalBytes = 0;
    double progressSum = 0.0;

    for (const DraftBatchItem &item : m_batch.items) {
        switch (item.status) {
        case DraftBatchItemStatus::Done:
            ++m_batch.doneCount;
            break;
        case DraftBatchItemStatus::Error:
            ++m_batch.failedCount;
            break;
        case DraftBatchItemStatus::Downloading:
        case DraftBatchItemStatus::Writing:
            ++m_batch.activeCount;
            break;
        case DraftBatchItemStatus::Queued:
        default:
            ++m_batch.queuedCount;
            break;
        }
        progressSum += item.progress;
        m_batch.loadedBytes += qMin(item.loadedBytes, item.totalBytes);
        m_batch.totalBytes += item.totalBytes;
    }

    if (m_batchRunning) {
        m_batch.progress = m_batch.items.isEmpty()
            ? 0.0
            : qMin(99.4, progressSum / m_batch.items.size());
    } else {
        m_batch.progress = 100.0;
    }

    if (m_batchClock && m_batchClock->isValid()) {
        m_batch.elapsedMs = m_batchClock->elapsed();
    }

    // 预计剩余：按当前并行任务的平均下载速度折算剩余体积；速度未知时给 —。
    qint64 remainingBytes = 0;
    double speedSum = 0.0;
    for (const DraftBatchItem &item : m_batch.items) {
        if (item.status == DraftBatchItemStatus::Queued
            || item.status == DraftBatchItemStatus::Downloading
            || item.status == DraftBatchItemStatus::Writing) {
            remainingBytes += qMax<qint64>(0, item.totalBytes - item.loadedBytes);
        }
        if (item.status == DraftBatchItemStatus::Downloading) {
            const Runtime *runtime = findRuntime(item.draftRowId);
            if (runtime) {
                speedSum += qMax(0.0, runtime->job.speed);
            }
        }
    }
    if (m_batchRunning && speedSum > 0.0 && remainingBytes > 0) {
        m_batch.etaSeconds =
            qMax(1, int(std::ceil(remainingBytes / speedSum)));
    } else {
        m_batch.etaSeconds = -1;
    }
}

void DraftDownloadManager::finishBatchIfIdle()
{
    if (!m_batchRunning || m_batchCancelling) {
        return;
    }
    if (!m_batchActive.isEmpty() || !m_batchQueue.isEmpty()) {
        return;
    }
    m_batchRunning = false;
    m_batch.progress = 100.0;
    recomputeBatch();
    emit batchChanged(m_batch);
    emit batchFinished(m_batch);
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

    if (m_batchRunning && !m_batchCancelling && batchItem(draftRowId)) {
        updateBatchFromJob(runtime->job);
        recomputeBatch();
        emit batchChanged(m_batch);
    }
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

    if (m_batchCancelIds.remove(draftRowId) && m_batchCancelIds.isEmpty()) {
        m_batchCancelling = false;
    }

    if (m_batchRunning && !m_batchCancelling && batchItem(draftRowId)) {
        updateBatchFromJob(runtime->job);
        m_batchActive.remove(draftRowId);
        m_batchFinishedItems.insert(draftRowId);
        launchQueuedBatchItems();
        recomputeBatch();
        emit batchChanged(m_batch);
    }

    emit jobFinished(runtime->job);
    finishBatchIfIdle();
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

    if (m_batchCancelIds.remove(draftRowId) && m_batchCancelIds.isEmpty()) {
        m_batchCancelling = false;
    }

    if (m_batchRunning && !m_batchCancelling && batchItem(draftRowId)) {
        m_batchActive.remove(draftRowId);
        if (DraftBatchItem *item = batchItem(draftRowId)) {
            item->status = DraftBatchItemStatus::Error;
            item->error = QStringLiteral("已取消");
        }
        m_batchFinishedItems.insert(draftRowId);
        launchQueuedBatchItems();
        recomputeBatch();
        emit batchChanged(m_batch);
        finishBatchIfIdle();
    }

    if (!m_batchCancelling) {
        emit toastRequested(QStringLiteral("已取消下载"));
    }
}
