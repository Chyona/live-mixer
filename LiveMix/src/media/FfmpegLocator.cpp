#include "FfmpegLocator.h"

#include <QCoreApplication>
#include <QDir>
#include <QFileInfo>
#include <QStandardPaths>
#include <QStringList>

namespace {

QString executableName(const QString &baseName)
{
#ifdef Q_OS_WIN
    return baseName + QStringLiteral(".exe");
#else
    return baseName;
#endif
}

QString resolve(const QString &baseName)
{
    const QString name = executableName(baseName);
    QStringList candidates;

    const QString overrideDir = qEnvironmentVariable("LIVEMIX_FFMPEG_DIR").trimmed();
    if (!overrideDir.isEmpty()) {
        candidates.append(QDir(overrideDir).filePath(name));
    }

    const QString appDir = QCoreApplication::applicationDirPath();
#ifdef Q_OS_MACOS
    candidates.append(QDir(appDir).filePath(QStringLiteral("../Helpers/") + name));
#endif
    candidates.append(QDir(appDir).filePath(name));

    const QString fromPath = QStandardPaths::findExecutable(baseName);
    if (!fromPath.isEmpty()) {
        candidates.append(fromPath);
    }

#ifdef Q_OS_MACOS
    candidates.append(QStringLiteral("/opt/homebrew/bin/") + name);
    candidates.append(QStringLiteral("/usr/local/bin/") + name);
#endif

    for (const QString &candidate : candidates) {
        const QFileInfo info(candidate);
        if (info.isFile() && info.isExecutable()) {
            const QString canonical = info.canonicalFilePath();
            return canonical.isEmpty() ? info.absoluteFilePath() : canonical;
        }
    }
    return QString();
}

} // namespace

QString FfmpegLocator::ffmpegPath()
{
    return resolve(QStringLiteral("ffmpeg"));
}

QString FfmpegLocator::ffprobePath()
{
    return resolve(QStringLiteral("ffprobe"));
}

QString FfmpegLocator::locate(const QString &executableBaseName)
{
    return resolve(executableBaseName);
}
