// 文件作用：草稿下载任务实现，参考 capcut-mate desktop-client 的本地化流程。
#include "DraftDownloader.h"

#include "../api/CapCutMateClient.h"
#include "../api/HttpClient.h"
#include "../core/MediaUtils.h"
#include "../data/MediaRepository.h"

#include <QDebug>
#include <QDir>
#include <QFileInfo>

#include <cmath>

namespace {

constexpr double kConfigProgressShare = 8.0;
constexpr double kAssetProgressShare = 82.0;
constexpr double kWritingProgressBase = 90.0;
constexpr qint64 kReportIntervalMs = 90;

DraftFileProgress *findFile(QVector<DraftFileProgress> *files, const QString &path)
{
    for (DraftFileProgress &file : *files) {
        if (file.path == path) {
            return &file;
        }
    }
    return nullptr;
}

} // namespace

DraftDownloader::DraftDownloader(const QString &databasePath, const QString &draftRoot,
                                 qint64 draftRowId, QObject *parent)
    : QObject(parent)
    , m_databasePath(databasePath)
    , m_draftRoot(draftRoot)
    , m_draftRowId(draftRowId)
{
}

void DraftDownloader::cancel()
{
    m_cancelled.store(true);
}

void DraftDownloader::download()
{
    MediaRepository repository(m_databasePath);
    QString error;
    if (!repository.open(&error)) {
        fail(QStringLiteral("本地数据库不可用：%1").arg(error));
        return;
    }

    const ClipDraft draft = repository.clipDraft(m_draftRowId, &error);
    if (draft.id < 0) {
        fail(error);
        return;
    }

    m_job = DraftDownloadJob();
    m_job.draftRowId = draft.id;
    m_job.title = draft.title;
    m_job.draftName = draft.draftId;
    m_job.rangeText = MediaUtils::formatDuration(draft.sourceStartMs);
    m_job.durationText = MediaUtils::formatDuration(draft.durationMs);
    m_job.stage = DraftDownloadStage::Config;
    m_job.status = DraftDownloadStatus::Downloading;
    m_clock.start();
    m_lastReportMs = -1;
    m_lastSampleMs = 0;
    m_lastSampleBytes = 0;
    m_speed = 0.0;

    // 已经下载过且目录仍在：直接打开，避免重复下载。
    // 但设置里的输出目录可能已经改过，旧目录中的草稿要迁移过去，否则用户会认为
    // 「剪映草稿输出目录」设置没有生效。
    if (!draft.downloadedPath.isEmpty()) {
        const QString existingPath = QDir::cleanPath(draft.downloadedPath);
        const QFileInfo existing(existingPath);
        if (existing.exists() && existing.isDir()) {
            if (MediaUtils::isWithinDirectory(m_draftRoot, existingPath)) {
                m_job.status = DraftDownloadStatus::Done;
                m_job.stage = DraftDownloadStage::Done;
                m_job.progress = 100;
                m_job.localPath = existingPath;
                report(true);
                emit finished(true, QStringLiteral("草稿已下载，正在打开目录。"),
                              existingPath);
                return;
            }

            const QString targetPath =
                QDir(QDir::cleanPath(m_draftRoot)).filePath(existing.fileName());
            const QFileInfo target(targetPath);
            if (target.exists() && target.isDir()) {
                // 新目录里已有一份（例如之前重新下载过），以它为准。
                if (!repository.markDraftDownloaded(draft.id, targetPath, &error)) {
                    qWarning() << "Failed to persist migrated draft path:" << error;
                }
                m_job.status = DraftDownloadStatus::Done;
                m_job.stage = DraftDownloadStage::Done;
                m_job.progress = 100;
                m_job.localPath = targetPath;
                report(true);
                emit finished(true, QStringLiteral("草稿已下载，正在打开目录。"),
                              targetPath);
                return;
            }

            // 优先整体移动，保留用户可能在剪映里做过的修改；跨磁盘移动失败时
            // 再走下面的重新下载流程。
            if (QDir().mkpath(m_draftRoot)
                && QDir().rename(existingPath, targetPath)) {
                if (!repository.markDraftDownloaded(draft.id, targetPath, &error)) {
                    qWarning() << "Failed to persist migrated draft path:" << error;
                }
                m_job.status = DraftDownloadStatus::Done;
                m_job.stage = DraftDownloadStage::Done;
                m_job.progress = 100;
                m_job.localPath = targetPath;
                report(true);
                emit finished(true, QStringLiteral("草稿已移动到：%1").arg(targetPath),
                              targetPath);
                return;
            }
        }
    }

    if (draft.draftId.isEmpty()) {
        fail(QStringLiteral("草稿记录缺少 draft_id。"));
        return;
    }
    if (!QDir().mkpath(m_draftRoot)) {
        fail(QStringLiteral("无法创建剪映草稿目录：%1").arg(m_draftRoot));
        return;
    }

    report(true);

    HttpClient http;
    CapCutMateClient capcut(&http);

    QString localPath;
    const bool ok = capcut.downloadDraft(
        draft.draftId, draft.title, m_draftRoot, &localPath, &error,
        [this](const DraftDownloadEvent &event) { applyEvent(event); },
        [this] { return m_cancelled.load(); });

    if (m_cancelled.load()) {
        // 取消后由管理器移除任务，不进入失败态。
        emit cancelled();
        return;
    }
    if (!ok) {
        fail(error.isEmpty() ? QStringLiteral("下载草稿失败。") : error);
        return;
    }

    // downloadDraft 会先写隐藏暂存目录、完成后再原子改名到正式目录；
    // 剪映的目录监听看到完整新目录后会自动收录，无需重启。
    if (!repository.markDraftDownloaded(draft.id, localPath, &error)) {
        // 下载本身已成功，路径写库失败只影响下次是否复用，仍然返回成功。
        qWarning() << "Failed to persist draft download path:" << error;
    }

    m_job.status = DraftDownloadStatus::Done;
    m_job.stage = DraftDownloadStage::Done;
    m_job.progress = 100;
    m_job.localPath = localPath;
    m_speed = 0.0;
    m_job.speed = 0.0;
    m_job.etaSeconds = -1;
    m_job.doneFiles = m_job.files.size();
    report(true);
    emit finished(true, QStringLiteral("草稿已下载到：%1").arg(localPath), localPath);
}

void DraftDownloader::applyEvent(const DraftDownloadEvent &event)
{
    switch (event.type) {
    case DraftDownloadEvent::Type::StageChanged:
        m_job.stage = event.stage;
        if (event.stage == DraftDownloadStage::Writing) {
            m_job.status = DraftDownloadStatus::Writing;
            m_speed = 0.0;
            m_job.speed = 0.0;
            m_job.etaSeconds = -1;
        } else if (event.stage == DraftDownloadStage::Assets) {
            m_job.status = DraftDownloadStatus::Downloading;
        }
        break;
    case DraftDownloadEvent::Type::FileQueued: {
        if (findFile(&m_job.files, event.path)) {
            break;
        }
        DraftFileProgress file;
        file.path = event.path;
        file.kind = event.fileKind;
        file.status = DraftFileStatus::Queued;
        // 下载前通过 HEAD 预取的大小，让总进度从素材阶段开始就准确。
        file.totalBytes = event.totalBytes;
        m_job.files.append(file);
        break;
    }
    case DraftDownloadEvent::Type::FileProgress: {
        DraftFileProgress *file = findFile(&m_job.files, event.path);
        if (!file) {
            DraftFileProgress created;
            created.path = event.path;
            created.kind = event.fileKind;
            m_job.files.append(created);
            file = &m_job.files.last();
        }
        if (file->status != DraftFileStatus::Done
            && file->status != DraftFileStatus::Error) {
            file->status = DraftFileStatus::Downloading;
        }
        file->loadedBytes = qMax<qint64>(0, event.loadedBytes);
        if (event.totalBytes > 0) {
            file->totalBytes = event.totalBytes;
        }
        if (file->totalBytes > 0) {
            const qint64 percent = file->loadedBytes * 100 / file->totalBytes;
            file->progress = int(qBound<qint64>(0LL, percent, 100LL));
        } else {
            file->progress = file->loadedBytes > 0 ? 1 : 0;
        }
        break;
    }
    case DraftDownloadEvent::Type::FileDone: {
        DraftFileProgress *file = findFile(&m_job.files, event.path);
        if (!file) {
            DraftFileProgress created;
            created.path = event.path;
            created.kind = event.fileKind;
            m_job.files.append(created);
            file = &m_job.files.last();
        }
        if (event.totalBytes > 0) {
            file->totalBytes = event.totalBytes;
        }
        if (event.loadedBytes > 0) {
            file->loadedBytes = event.loadedBytes;
        }
        if (file->totalBytes < 0) {
            file->totalBytes = file->loadedBytes;
        }
        file->loadedBytes = qMax(file->loadedBytes, file->totalBytes);
        file->status = DraftFileStatus::Done;
        file->progress = 100;
        break;
    }
    case DraftDownloadEvent::Type::FileFailed: {
        DraftFileProgress *file = findFile(&m_job.files, event.path);
        if (!file) {
            DraftFileProgress created;
            created.path = event.path;
            created.kind = event.fileKind;
            m_job.files.append(created);
            file = &m_job.files.last();
        }
        file->status = DraftFileStatus::Error;
        break;
    }
    }

    recompute();
    report();
}

void DraftDownloader::recompute()
{
    int doneFiles = 0;
    qint64 loaded = 0;
    qint64 total = 0;
    double configSum = 0.0;
    double assetSum = 0.0;
    double configWeight = 0.0;
    double assetWeight = 0.0;

    for (const DraftFileProgress &file : m_job.files) {
        loaded += file.loadedBytes;
        total += qMax(file.totalBytes, file.loadedBytes);
        if (file.status == DraftFileStatus::Done) {
            ++doneFiles;
        }

        double fraction = 0.0;
        if (file.status == DraftFileStatus::Done) {
            fraction = 1.0;
        } else if (file.totalBytes > 0) {
            fraction = qBound(0.0,
                              double(file.loadedBytes) / double(file.totalBytes), 1.0);
        } else if (file.status == DraftFileStatus::Downloading) {
            fraction = 0.01;
        }

        // 大文件占更大权重，整体百分比与已下载字节数保持一致。
        const double weight =
            file.totalBytes > 0 ? double(file.totalBytes) : 1.0;
        if (file.kind == DraftFileKind::Config) {
            configSum += fraction * weight;
            configWeight += weight;
        } else {
            assetSum += fraction * weight;
            assetWeight += weight;
        }
    }

    m_job.loadedBytes = loaded;
    m_job.totalBytes = qMax(total, loaded);
    m_job.doneFiles = doneFiles;
    m_job.totalFiles = m_job.files.size();

    if (m_job.status == DraftDownloadStatus::Done) {
        m_job.progress = 100.0;
        m_job.etaSeconds = -1;
        return;
    }
    if (m_job.status == DraftDownloadStatus::Writing) {
        m_job.progress = kWritingProgressBase;
        m_job.etaSeconds = -1;
        return;
    }
    if (m_job.status == DraftDownloadStatus::Error) {
        m_job.etaSeconds = -1;
        return;
    }

    const double configFraction = configWeight > 0.0 ? configSum / configWeight : 1.0;
    const double assetFraction = assetWeight > 0.0 ? assetSum / assetWeight : 0.0;
    double progress = 0.0;
    if (m_job.stage == DraftDownloadStage::Config) {
        progress = kConfigProgressShare * configFraction;
    } else {
        progress = kConfigProgressShare + kAssetProgressShare * assetFraction;
    }
    m_job.progress = qBound(0.0, progress, kWritingProgressBase - 1.0);

    // 速度按最近一次采样窗口内的字节增量平滑计算。
    if (m_lastSampleMs >= 0) {
        const qint64 now = m_clock.elapsed();
        const qint64 elapsedMs = now - m_lastSampleMs;
        if (elapsedMs >= 200) {
            const double instant =
                double(loaded - m_lastSampleBytes) / (elapsedMs / 1000.0);
            m_speed = m_speed <= 0.0 ? instant : m_speed * 0.65 + instant * 0.35;
            m_lastSampleMs = now;
            m_lastSampleBytes = loaded;
        }
    }
    m_job.speed = qMax(0.0, m_speed);
    if (m_job.speed > 1000.0 && m_job.totalBytes > m_job.loadedBytes) {
        const double remaining = double(m_job.totalBytes - m_job.loadedBytes);
        m_job.etaSeconds = int(std::ceil(remaining / m_job.speed));
    } else {
        m_job.etaSeconds = -1;
    }
}

void DraftDownloader::report(bool force)
{
    const qint64 now = m_clock.isValid() ? m_clock.elapsed() : 0;
    if (!force && m_lastReportMs >= 0 && now - m_lastReportMs < kReportIntervalMs) {
        return;
    }
    m_lastReportMs = now;
    emit jobChanged(m_job);
}

void DraftDownloader::fail(const QString &message)
{
    m_job.status = DraftDownloadStatus::Error;
    m_job.error = message;
    m_job.speed = 0.0;
    m_job.etaSeconds = -1;
    report(true);
    emit finished(false, message, QString());
}
