#include "MediaUtils.h"

#include <QDir>
#include <QDirIterator>
#include <QFile>
#include <QFileInfo>
#include <QTextStream>

namespace MediaUtils {

QString formatDuration(qint64 milliseconds)
{
    const qint64 totalSeconds = qMax<qint64>(0, milliseconds) / 1000;
    const qint64 hours = totalSeconds / 3600;
    const qint64 minutes = (totalSeconds % 3600) / 60;
    const qint64 seconds = totalSeconds % 60;

    if (hours > 0) {
        return QStringLiteral("%1:%2:%3")
            .arg(hours)
            .arg(minutes, 2, 10, QLatin1Char('0'))
            .arg(seconds, 2, 10, QLatin1Char('0'));
    }
    return QStringLiteral("%1:%2")
        .arg(minutes, 2, 10, QLatin1Char('0'))
        .arg(seconds, 2, 10, QLatin1Char('0'));
}

QString formatByteSize(qint64 bytes)
{
    constexpr double kKb = 1024.0;
    constexpr double kMb = kKb * 1024.0;
    constexpr double kGb = kMb * 1024.0;
    if (bytes >= kGb) {
        return QStringLiteral("%1 GB").arg(bytes / kGb, 0, 'f', 2);
    }
    if (bytes >= kMb) {
        return QStringLiteral("%1 MB").arg(bytes / kMb, 0, 'f', 1);
    }
    return QStringLiteral("%1 KB")
        .arg(qMax<double>(1.0, bytes / kKb), 0, 'f', 0);
}

bool isWithinDirectory(const QString &root, const QString &path)
{
    const QString cleanRoot = QDir::cleanPath(root);
    const QString cleanPath = QDir::cleanPath(path);
    if (cleanRoot.isEmpty() || cleanPath.isEmpty()) {
        return false;
    }
#ifdef Q_OS_WIN
    const Qt::CaseSensitivity cs = Qt::CaseInsensitive;
#else
    const Qt::CaseSensitivity cs = Qt::CaseSensitive;
#endif
    if (cleanPath.compare(cleanRoot, cs) == 0) {
        return true;
    }
    const QString prefix =
        cleanRoot.endsWith(QLatin1Char('/')) ? cleanRoot : cleanRoot + QLatin1Char('/');
    return cleanPath.startsWith(prefix, cs);
}

qint64 directorySize(const QString &path)
{
    const QFileInfo rootInfo(path);
    if (!rootInfo.exists()) {
        return 0;
    }
    if (rootInfo.isFile()) {
        return rootInfo.size();
    }

    qint64 total = 0;
    QDirIterator it(path, QDir::Files | QDir::NoSymLinks, QDirIterator::Subdirectories);
    while (it.hasNext()) {
        const QFileInfo info(it.next());
        total += info.size();
    }
    return total;
}

qint64 hlsPlaylistDurationMs(const QString &playlistPath)
{
    QFile file(playlistPath);
    if (!file.open(QIODevice::ReadOnly | QIODevice::Text)) {
        return 0;
    }

    double totalSeconds = 0.0;
    QTextStream stream(&file);
    while (!stream.atEnd()) {
        const QString line = stream.readLine().trimmed();
        if (!line.startsWith(QLatin1String("#EXTINF:"))) {
            continue;
        }
        const QString value = line.mid(8).section(QLatin1Char(','), 0, 0).trimmed();
        bool ok = false;
        const double seconds = value.toDouble(&ok);
        if (ok && seconds > 0.0) {
            totalSeconds += seconds;
        }
    }
    return static_cast<qint64>(totalSeconds * 1000.0 + 0.5);
}

QString sanitizeFileComponent(const QString &value, int maxLength)
{
    QString result;
    result.reserve(value.size());
    for (const QChar ch : value) {
        if (ch.category() == QChar::Other_Control
            || QStringLiteral("/\\:*?\"<>|%").contains(ch)) {
            result.append(QLatin1Char('_'));
        } else {
            result.append(ch);
        }
    }
    result = result.simplified();
    while (result.startsWith(QLatin1Char('.'))) {
        result.remove(0, 1);
    }
    if (result.isEmpty()) {
        result = QStringLiteral("recording");
    }
    return result.left(qMax(1, maxLength));
}

} // namespace MediaUtils
