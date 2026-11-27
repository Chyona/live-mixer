// 文件作用：导出成片任务管理实现。
#include "VideoExportManager.h"

#include "VideoExporter.h"
#include "../core/MediaUtils.h"

#include <QDebug>
#include <QElapsedTimer>
#include <QThread>

#include <cmath>

namespace {

qint64 estimateFileSizeBytes(const ClipDraft &draft)
{
    const qint64 seconds = qMax<qint64>(1, draft.durationMs / 1000);
    return qMax<qint64>(1024 * 1024, qint64(seconds * 0.42 * 1024.0 * 1024.0));
}

qint64 estimateDurationMs(const ClipDraft &draft)
{
    return 128000 + (draft.id % 4) * 9000;
}

} // namespace

VideoExportManager::VideoExportManager(QObject *parent)
    : QObject(parent)
    , m_batchClock(new QElapsedTimer)
{
    qRegisterMetaType<VideoExportJob>("VideoExportJob");
    qRegisterMetaType<VideoBatchSnapshot>("VideoBatchSnapshot");
}

VideoExportManager::~VideoExportManager()
{
    for (Runtime *runtime : m_runtimes) {
        if (runtime->exporter) {
            runtime->exporter->cancel();
        }
        if (runtime->thread) {
            runtime->thread->requestInterruption();
            runtime->thread->quit();
            if (runtime->thread->isRunning() && !runtime->thread->wait(1500)) {
                qWarning() << "Video export still running at exit; detaching";
            }
        }
        delete runtime;
    }
    m_runtimes.clear();
    delete m_batchClock;
    m_batchClock = nullptr;
}

void VideoExportManager::start(const ClipDraft &draft, const QString &apiKey,
                               const QString &exportDir)
{
    if (Runtime *existing = findRuntime(draft.id)) {
        const bool finished = existing->job.status == VideoExportStatus::Done
            || existing->job.status == VideoExportStatus::Error;
        if (!finished || (existing->thread && existing->thread->isRunning())) {
            return;
        }
        m_runtimes.remove(draft.id);
        delete existing;
    }

    auto *runtime = new Runtime;
    runtime->draft = draft;
    runtime->apiKey = apiKey;
    runtime->exportDir = exportDir;

    runtime->job.draftRowId = draft.id;
    runtime->job.title = draft.title.isEmpty() ? QStringLiteral("直播切片") : draft.title;
    runtime->job.draftName = draft.draftId;
    runtime->job.draftUrl = draft.draftUrl;
    runtime->job.rangeText = MediaUtils::formatDuration(draft.sourceStartMs);
    runtime->job.durationText = MediaUtils::formatDuration(draft.durationMs);
    runtime->job.status = VideoExportStatus::Running;
    runtime->job.stage = VideoExportStage::Compose;
    runtime->job.expectedMs = estimateDurationMs(draft);
    runtime->job.fileSizeBytes = estimateFileSizeBytes(draft);

    m_runtimes.insert(draft.id, runtime);
    emit jobChanged(runtime->job);
    launch(runtime, QString(), 0);
}

void VideoExportManager::restart(const ClipDraft &draft, const QString &apiKey,
                                 const QString &exportDir)
{
    if (Runtime *existing = findRuntime(draft.id)) {
        if (existing->job.status == VideoExportStatus::Running) {
            return;
        }
        m_runtimes.remove(draft.id);
        delete existing;
    }
    start(draft, apiKey, exportDir);
}

void VideoExportManager::retry(qint64 draftRowId)
{
    Runtime *runtime = findRuntime(draftRowId);
    if (!runtime || runtime->job.status != VideoExportStatus::Error) {
        return;
    }
    if (runtime->thread && runtime->thread->isRunning()) {
        return;
    }

    const QString resumeUrl = runtime->job.videoUrl;
    const qint64 baseElapsed = runtime->job.elapsedMs;

    runtime->job.status = VideoExportStatus::Running;
    runtime->job.stage = resumeUrl.isEmpty() ? VideoExportStage::Compose
                                             : VideoExportStage::Download;
    runtime->job.progress = resumeUrl.isEmpty() ? 0.0 : 86.0;
    runtime->job.error.clear();
    runtime->job.speedBytesPerSecond = 0.0;
    runtime->job.etaSeconds = -1;
    runtime->job.downloadedBytes = 0;

    emit jobChanged(runtime->job);
    launch(runtime, resumeUrl, baseElapsed);
}

void VideoExportManager::cancel(qint64 draftRowId)
{
    Runtime *runtime = findRuntime(draftRowId);
    if (!runtime || runtime->job.status != VideoExportStatus::Running) {
        return;
    }
    if (runtime->exporter) {
        runtime->exporter->cancel();
    }
}

bool VideoExportManager::hasJob(qint64 draftRowId) const
{
    return m_runtimes.contains(draftRowId);
}

VideoExportJob VideoExportManager::job(qint64 draftRowId) const
{
    Runtime *runtime = findRuntime(draftRowId);
    return runtime ? runtime->job : VideoExportJob();
}

QVector<VideoExportJob> VideoExportManager::jobs() const
{
    QVector<VideoExportJob> result;
    result.reserve(m_runtimes.size());
    for (Runtime *runtime : m_runtimes) {
        result.append(runtime->job);
    }
    return result;
}

void VideoExportManager::startBatch(const QVector<ClipDraft> &drafts,
                                    const QString &apiKey, const QString &exportDir)
{
    if (m_batchRunning) {
        emit toastRequested(QStringLiteral("已有批量导出正在进行"));
        return;
    }

    QVector<ClipDraft> eligible;
    eligible.reserve(drafts.size());
    for (const ClipDraft &draft : drafts) {
        const Runtime *runtime = findRuntime(draft.id);
        if (!runtime
            || (runtime->job.status == VideoExportStatus::Error
                && !(runtime->thread && runtime->thread->isRunning()))) {
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

    m_batch = VideoBatchSnapshot();
    m_batch.id = ++m_batchCounter;
    m_batch.running = true;
    m_batchDrafts.clear();
    m_batchQueue.clear();
    m_batchActive.clear();
    m_batchCancelIds.clear();
    m_batchApiKey = apiKey;
    m_batchExportDir = exportDir;
    m_batchCancelling = false;
    m_batchRunning = true;
    m_batchClock->start();

    for (const ClipDraft &draft : eligible) {
        VideoBatchItem item;
        item.draftRowId = draft.id;
        item.title = draft.title.isEmpty() ? QStringLiteral("直播切片") : draft.title;
        item.durationText = MediaUtils::formatDuration(draft.durationMs);
        item.fileSizeBytes = estimateFileSizeBytes(draft);
        item.expectedMs = estimateDurationMs(draft);
        m_batch.items.append(item);
        m_batchDrafts.insert(draft.id, draft);
        m_batchQueue.append(draft.id);
    }

    launchQueuedBatchItems();
    recomputeBatch();
    emit batchChanged(m_batch);
    // 极端情况下（队列项全部因缺少草稿记录而立即失败）不会有任务回调，
    // 这里补一次收尾判断，避免批量任务永远停在运行态。
    finishBatchIfIdle();
}

void VideoExportManager::cancelBatch()
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
    m_batch = VideoBatchSnapshot();
    emit batchCleared();
    emit toastRequested(QStringLiteral("已停止批量导出，已完成的成片会保留"));
}

void VideoExportManager::retryBatchFailed()
{
    QVector<ClipDraft> retryable;
    for (const VideoBatchItem &item : m_batch.items) {
        if (item.status != VideoBatchItemStatus::Error) {
            continue;
        }
        const Runtime *runtime = findRuntime(item.draftRowId);
        if (runtime && runtime->job.status == VideoExportStatus::Done) {
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
    startBatch(retryable, m_batchApiKey, m_batchExportDir);
    emit toastRequested(QStringLiteral("正在重试 %1 条失败的成片").arg(retryable.size()));
}

bool VideoExportManager::isBatchJob(qint64 draftRowId) const
{
    for (const VideoBatchItem &item : m_batch.items) {
        if (item.draftRowId == draftRowId) {
            return true;
        }
    }
    return false;
}

VideoExportManager::Runtime *VideoExportManager::findRuntime(
    qint64 draftRowId) const
{
    return m_runtimes.value(draftRowId, nullptr);
}

void VideoExportManager::launch(Runtime *runtime, const QString &resumeVideoUrl,
                                qint64 baseElapsedMs)
{
    const qint64 draftRowId = runtime->job.draftRowId;
    auto *exporter = new VideoExporter(
        runtime->draft.id, runtime->draft.title, runtime->draft.draftId,
        runtime->job.durationText, runtime->draft.draftUrl,
        runtime->draft.durationMs, runtime->apiKey, runtime->exportDir);
    if (!resumeVideoUrl.isEmpty() || baseElapsedMs > 0) {
        exporter->setResume(resumeVideoUrl, baseElapsedMs);
    }

    auto *thread = new QThread;
    exporter->moveToThread(thread);
    runtime->exporter = exporter;
    runtime->thread = thread;

    connect(thread, &QThread::started, exporter, &VideoExporter::run);
    connect(exporter, &VideoExporter::jobChanged, this,
            [this, draftRowId](const VideoExportJob &job) {
                onJobChanged(draftRowId, job);
            });
    connect(exporter, &VideoExporter::finished, this,
            [this, draftRowId](bool success, const QString &message,
                               const QString &outputPath) {
                onFinished(draftRowId, success, message, outputPath);
            });
    connect(exporter, &VideoExporter::cancelled, this,
            [this, draftRowId] { onCancelled(draftRowId); });
    connect(thread, &QThread::finished, this,
            [this, draftRowId, exporter, thread, runtime] {
                if (Runtime *current = m_runtimes.value(draftRowId, nullptr);
                    current == runtime) {
                    if (current->exporter == exporter) {
                        current->exporter = nullptr;
                    }
                    if (current->thread == thread) {
                        current->thread = nullptr;
                    }
                }
                exporter->deleteLater();
                thread->deleteLater();
            });

    thread->start();
}

void VideoExportManager::onJobChanged(qint64 draftRowId,
                                      const VideoExportJob &job)
{
    Runtime *runtime = findRuntime(draftRowId);
    if (!runtime) {
        return;
    }
    VideoExportJob next = job;
    next.draftRowId = draftRowId;
    if (next.title.isEmpty()) {
        next.title = runtime->job.title;
    }
    if (next.outputPath.isEmpty()) {
        next.outputPath = runtime->job.outputPath;
    }
    runtime->job = next;
    emit jobChanged(runtime->job);

    if (m_batchRunning && !m_batchCancelling && batchItem(draftRowId)) {
        updateBatchFromJob(runtime->job);
        recomputeBatch();
        emit batchChanged(m_batch);
    }
}

void VideoExportManager::onFinished(qint64 draftRowId, bool success,
                                    const QString &message,
                                    const QString &outputPath)
{
    Runtime *runtime = findRuntime(draftRowId);
    if (!runtime) {
        return;
    }

    if (success) {
        runtime->job.status = VideoExportStatus::Done;
        runtime->job.stage = VideoExportStage::Done;
        runtime->job.progress = 100.0;
        runtime->job.outputPath = outputPath;
        runtime->job.etaSeconds = -1;
        runtime->job.speedBytesPerSecond = 0.0;
    } else {
        runtime->job.status = VideoExportStatus::Error;
        runtime->job.error = message;
        runtime->job.etaSeconds = -1;
        runtime->job.speedBytesPerSecond = 0.0;
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
        launchQueuedBatchItems();
        recomputeBatch();
        emit batchChanged(m_batch);
    }

    emit jobFinished(runtime->job);
    finishBatchIfIdle();
}

void VideoExportManager::onCancelled(qint64 draftRowId)
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
        if (VideoBatchItem *item = batchItem(draftRowId)) {
            item->status = VideoBatchItemStatus::Error;
            item->error = QStringLiteral("已取消");
        }
        launchQueuedBatchItems();
        recomputeBatch();
        emit batchChanged(m_batch);
        finishBatchIfIdle();
    }

    if (!m_batchCancelling) {
        emit toastRequested(QStringLiteral("已取消导出"));
    }
}

void VideoExportManager::launchQueuedBatchItems()
{
    while (m_batchActive.size() < kBatchConcurrency && !m_batchQueue.isEmpty()
           && !m_batchCancelling) {
        const qint64 draftRowId = m_batchQueue.takeFirst();
        const Runtime *runtime = findRuntime(draftRowId);
        if (runtime && runtime->job.status == VideoExportStatus::Done) {
            if (VideoBatchItem *item = batchItem(draftRowId)) {
                item->status = VideoBatchItemStatus::Done;
                item->progress = 100.0;
                item->hasStage = true;
                item->stage = VideoExportStage::Done;
                item->downloadedBytes = item->fileSizeBytes;
            }
            continue;
        }

        m_batchActive.insert(draftRowId);

        if (runtime && runtime->job.status == VideoExportStatus::Running) {
            // 已在单独导出的任务直接加入批量并沿用进度。
            updateBatchFromJob(runtime->job);
            continue;
        }

        const ClipDraft draft = m_batchDrafts.value(draftRowId);
        if (draft.id < 0) {
            m_batchActive.remove(draftRowId);
            if (VideoBatchItem *item = batchItem(draftRowId)) {
                item->status = VideoBatchItemStatus::Error;
                item->error = QStringLiteral("缺少草稿记录");
            }
            continue;
        }
        // 批量中的失败项重试时沿用已生成的成片地址。
        const QString resumeUrl = runtime ? runtime->job.videoUrl : QString();
        const qint64 baseElapsed = runtime ? runtime->job.elapsedMs : 0;
        const QString apiKey = m_batchApiKey;
        const QString exportDir = m_batchExportDir;

        auto *next = new Runtime;
        next->draft = draft;
        next->apiKey = apiKey;
        next->exportDir = exportDir;
        next->job.draftRowId = draft.id;
        next->job.title = draft.title.isEmpty() ? QStringLiteral("直播切片")
                                                : draft.title;
        next->job.draftName = draft.draftId;
        next->job.draftUrl = draft.draftUrl;
        next->job.rangeText = MediaUtils::formatDuration(draft.sourceStartMs);
        next->job.durationText = MediaUtils::formatDuration(draft.durationMs);
        next->job.status = VideoExportStatus::Running;
        next->job.stage = resumeUrl.isEmpty() ? VideoExportStage::Compose
                                              : VideoExportStage::Download;
        next->job.progress = resumeUrl.isEmpty() ? 0.0 : 86.0;
        next->job.expectedMs = estimateDurationMs(draft);
        next->job.fileSizeBytes = estimateFileSizeBytes(draft);
        next->job.videoUrl = resumeUrl;

        if (Runtime *existing = findRuntime(draftRowId)) {
            m_runtimes.remove(draftRowId);
            delete existing;
        }
        m_runtimes.insert(draftRowId, next);
        emit jobChanged(next->job);
        launch(next, resumeUrl, baseElapsed);
    }
}

VideoBatchItem *VideoExportManager::batchItem(qint64 draftRowId)
{
    for (VideoBatchItem &item : m_batch.items) {
        if (item.draftRowId == draftRowId) {
            return &item;
        }
    }
    return nullptr;
}

void VideoExportManager::updateBatchFromJob(const VideoExportJob &job)
{
    VideoBatchItem *item = batchItem(job.draftRowId);
    if (!item) {
        return;
    }
    item->stage = job.stage;
    item->hasStage = job.stage != VideoExportStage::Compose || job.progress > 0.0;
    item->progress = qBound(0.0, job.progress, 100.0);
    item->fileSizeBytes = job.fileSizeBytes;
    item->downloadedBytes = job.downloadedBytes;
    item->elapsedMs = job.elapsedMs;
    item->expectedMs = job.expectedMs;
    item->error = job.error;

    switch (job.status) {
    case VideoExportStatus::Running:
        item->status = VideoBatchItemStatus::Running;
        break;
    case VideoExportStatus::Done:
        item->status = VideoBatchItemStatus::Done;
        item->hasStage = true;
        item->stage = VideoExportStage::Done;
        item->progress = 100.0;
        item->downloadedBytes = item->fileSizeBytes;
        break;
    case VideoExportStatus::Error:
        item->status = VideoBatchItemStatus::Error;
        break;
    }
}

void VideoExportManager::recomputeBatch()
{
    m_batch.doneCount = 0;
    m_batch.failedCount = 0;
    m_batch.activeCount = 0;
    m_batch.queuedCount = 0;
    m_batch.downloadedBytes = 0;
    m_batch.totalBytes = 0;
    double progressSum = 0.0;

    for (const VideoBatchItem &item : m_batch.items) {
        switch (item.status) {
        case VideoBatchItemStatus::Done:
            ++m_batch.doneCount;
            break;
        case VideoBatchItemStatus::Error:
            ++m_batch.failedCount;
            break;
        case VideoBatchItemStatus::Running:
            ++m_batch.activeCount;
            break;
        case VideoBatchItemStatus::Queued:
        default:
            ++m_batch.queuedCount;
            break;
        }
        progressSum += item.progress;
        m_batch.downloadedBytes += qMin(item.downloadedBytes, item.fileSizeBytes);
        m_batch.totalBytes += item.fileSizeBytes;
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

    if (m_batchRunning) {
        qint64 remaining = 0;
        for (const VideoBatchItem &item : m_batch.items) {
            if (item.status == VideoBatchItemStatus::Queued) {
                remaining += item.expectedMs;
            } else if (item.status == VideoBatchItemStatus::Running) {
                remaining += qMax<qint64>(0, item.expectedMs - item.elapsedMs);
            }
        }
        const int lanes = qMax(1, qMin(kBatchConcurrency,
                                       qMax(1, m_batch.activeCount)));
        m_batch.etaSeconds = remaining > 0
            ? qMax(1, int((remaining / lanes + 999) / 1000))
            : -1;
    } else {
        m_batch.etaSeconds = -1;
    }
}

void VideoExportManager::finishBatchIfIdle()
{
    if (!m_batchRunning || m_batchCancelling) {
        return;
    }
    if (!m_batchActive.isEmpty() || !m_batchQueue.isEmpty()) {
        return;
    }
    m_batchRunning = false;
    // 快照里的 running 是界面判断“进行中/已完成”的唯一依据，必须同步结束，
    // 否则批量弹窗和结果页会一直停在“正在导出 99%”。
    m_batch.running = false;
    m_batch.progress = 100.0;
    recomputeBatch();
    emit batchChanged(m_batch);
    emit batchFinished(m_batch);
}
