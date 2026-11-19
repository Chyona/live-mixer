#include "CacheCleaner.h"

#include <QDateTime>
#include <QDebug>
#include <QDir>
#include <QDirIterator>
#include <QFile>
#include <QFileInfo>
#include <QSet>
#include <QStandardPaths>
#include <QStringList>
#include <QVector>

#include <algorithm>

namespace {

// 最近写入/读取过的预览缓存可能仍在播放或被任务复用，不参与清理。
constexpr qint64 kRecentFileGraceMs = 10 * 60 * 1000;

// clip-jobs 下的任务目录在运行期间会持续写入；只有长时间未变化的目录
// 才视为崩溃残留。正常任务结束时工作目录会被 LiveClipService 自动删除。
constexpr qint64 kStaleJobDirMs = 24 * 60 * 60 * 1000;

struct CacheFile
{
    QString path;
    qint64 size = 0;
    qint64 modifiedMs = 0;
};

QString cacheLocation()
{
    QString dir = QStandardPaths::writableLocation(QStandardPaths::CacheLocation);
    if (dir.isEmpty()) {
        dir = QDir(QDir::tempPath()).filePath(QStringLiteral("LiveMix"));
    }
    return QDir::cleanPath(dir);
}

void collectFiles(const QString &root, QVector<CacheFile> *files, qint64 *total)
{
    QDirIterator it(root, QDir::Files | QDir::NoSymLinks, QDirIterator::Subdirectories);
    while (it.hasNext()) {
        it.next();
        const QFileInfo info = it.fileInfo();
        if (!info.isFile()) {
            continue;
        }
        CacheFile file;
        file.path = info.absoluteFilePath();
        file.size = info.size();
        file.modifiedMs = info.lastModified().toMSecsSinceEpoch();
        *total += file.size;
        files->append(file);
    }
}

// 只收集长时间未更新的任务目录，避免删除正在运行的切片任务。
void collectStaleJobDirs(const QString &root, QSet<QString> *seenDirs,
                         QVector<CacheFile> *files, qint64 *total)
{
    const qint64 staleBefore = QDateTime::currentMSecsSinceEpoch() - kStaleJobDirMs;
    const QFileInfoList entries =
        QDir(root).entryInfoList(QDir::Dirs | QDir::NoDotAndDotDot, QDir::NoSort);
    for (const QFileInfo &entry : entries) {
        if (entry.lastModified().toMSecsSinceEpoch() > staleBefore) {
            continue;
        }
        const QString path = QDir::cleanPath(entry.absoluteFilePath());
        if (seenDirs->contains(path)) {
            continue;
        }
        seenDirs->insert(path);
        collectFiles(path, files, total);
    }
}

// 删除文件后清掉空的层级目录，避免 clip-jobs 下留下大量空目录。
void removeEmptyJobDirs(const QString &root)
{
    const QFileInfoList entries =
        QDir(root).entryInfoList(QDir::Dirs | QDir::NoDotAndDotDot, QDir::NoSort);
    for (const QFileInfo &entry : entries) {
        const QString jobDir = entry.absoluteFilePath();
        QStringList subdirs;
        QDirIterator it(jobDir, QDir::Dirs | QDir::NoDotAndDotDot, QDirIterator::Subdirectories);
        while (it.hasNext()) {
            subdirs.append(it.next());
        }
        // 目录路径越长层级越深，先删深层目录。
        std::sort(subdirs.begin(), subdirs.end(),
                  [](const QString &a, const QString &b) { return a.size() > b.size(); });
        for (const QString &subdir : subdirs) {
            QDir().rmdir(subdir);
        }
        QDir().rmdir(jobDir);
    }
}

} // namespace

CacheCleaner::CleanupResult CacheCleaner::enforceLimit(const QString &cacheDir,
                                                       qint64 limitBytes)
{
    CleanupResult result;
    if (limitBytes <= 0) {
        return result;
    }

    const QString location = cacheLocation();
    const QString previewsDir = QDir(location).filePath(QStringLiteral("previews"));
    const QString jobsDir = cacheDir.trimmed().isEmpty()
        ? QString()
        : QDir(cacheDir).filePath(QStringLiteral("clip-jobs"));
    // 用户切换过缓存目录时，旧位置的残留任务目录也一并清理。
    const QString legacyJobsDir = QDir(location).filePath(QStringLiteral("clip-jobs"));

    QVector<CacheFile> files;
    qint64 total = 0;
    collectFiles(previewsDir, &files, &total);

    QSet<QString> seenJobDirs;
    if (!jobsDir.isEmpty()) {
        collectStaleJobDirs(jobsDir, &seenJobDirs, &files, &total);
    }
    if (QDir::cleanPath(legacyJobsDir) != QDir::cleanPath(jobsDir)) {
        collectStaleJobDirs(legacyJobsDir, &seenJobDirs, &files, &total);
    }

    result.totalBytes = total;
    if (total <= limitBytes) {
        return result;
    }

    std::sort(files.begin(), files.end(), [](const CacheFile &a, const CacheFile &b) {
        if (a.modifiedMs != b.modifiedMs) {
            return a.modifiedMs < b.modifiedMs;
        }
        return a.path < b.path;
    });

    const qint64 recentBefore = QDateTime::currentMSecsSinceEpoch() - kRecentFileGraceMs;
    for (const CacheFile &file : files) {
        if (total <= limitBytes) {
            break;
        }
        if (file.modifiedMs > recentBefore) {
            continue;
        }
        if (!QFile::remove(file.path)) {
            qWarning() << "Unable to remove cache file:" << file.path;
            continue;
        }
        total -= file.size;
        result.freedBytes += file.size;
        ++result.removedFiles;
    }

    if (result.removedFiles > 0) {
        if (!jobsDir.isEmpty()) {
            removeEmptyJobDirs(jobsDir);
        }
        if (QDir::cleanPath(legacyJobsDir) != QDir::cleanPath(jobsDir)) {
            removeEmptyJobDirs(legacyJobsDir);
        }
        qInfo() << "Cache cleanup removed" << result.removedFiles << "files,"
                << result.freedBytes << "bytes; cache now" << total << "bytes";
    }
    return result;
}
