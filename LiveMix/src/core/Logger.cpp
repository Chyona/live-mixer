#include "Logger.h"

#include <QCoreApplication>
#include <QDate>
#include <QDateTime>
#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QMutex>
#include <QMutexLocker>
#include <QStandardPaths>
#include <QStringList>
#include <QtGlobal>

#include <algorithm>
#include <cstdio>

namespace {

constexpr int kMaxLogFiles = 30;

QMutex g_mutex;
QFile g_file;
QDate g_fileDate; // Date the current file targets; set even when opening fails.
bool g_openFailed = false;
bool g_installed = false;
QString g_logDir;

const char *levelName(QtMsgType type)
{
    switch (type) {
    case QtDebugMsg:
        return "DEBUG";
    case QtInfoMsg:
        return "INFO";
    case QtWarningMsg:
        return "WARN";
    case QtCriticalMsg:
        return "ERROR";
    case QtFatalMsg:
        return "FATAL";
    }
    return "INFO";
}

QString appLogFolderName()
{
    const QString name = QCoreApplication::applicationName().trimmed();
    return name.isEmpty() ? QStringLiteral("LiveMix Studio") : name;
}

QString logDirectoryPath()
{
    const QString overrideDir = qEnvironmentVariable("LIVEMIX_LOG_DIR").trimmed();
    if (!overrideDir.isEmpty()) {
        return QDir::cleanPath(overrideDir);
    }

#if defined(Q_OS_MACOS)
    // Apple's convention for per-user application log files. The bundle itself
    // is often read-only, so never write inside it.
    const QString home = QStandardPaths::writableLocation(QStandardPaths::HomeLocation);
    if (!home.isEmpty()) {
        return QDir(home).filePath(QStringLiteral("Library/Logs/") + appLogFolderName());
    }
#elif defined(Q_OS_WIN)
    // The installer targets Program Files, which is read-only for normal runs.
    const QString appData =
        QStandardPaths::writableLocation(QStandardPaths::AppLocalDataLocation);
    if (!appData.isEmpty()) {
        return QDir(appData).filePath(QStringLiteral("logs"));
    }
#endif

    return QDir(QCoreApplication::applicationDirPath()).filePath(QStringLiteral("logs"));
}

QString logFilePathForDate(const QDate &date)
{
    return QDir(g_logDir).filePath(date.toString(QStringLiteral("yyyy-MM-dd"))
                                   + QStringLiteral(".log"));
}

QByteArray formatLine(const QDateTime &time, QtMsgType type,
                      const QMessageLogContext &context, const QString &message)
{
    QString line = time.toString(QStringLiteral("yyyy-MM-dd HH:mm:ss.zzz"));
    line += QStringLiteral(" [");
    line += QLatin1String(levelName(type));
    line += QStringLiteral("] ");

    if (context.category && qstrcmp(context.category, "default") != 0) {
        line += QLatin1Char('[');
        line += QString::fromUtf8(context.category);
        line += QStringLiteral("] ");
    }

    line += message;
    if (context.file && *context.file) {
        line += QStringLiteral(" (");
        line += QString::fromUtf8(context.file);
        line += QLatin1Char(':');
        line += QString::number(context.line);
        line += QLatin1Char(')');
    }
    line += QLatin1Char('\n');
    return line.toUtf8();
}

// Keeps the newest kMaxLogFiles date-named files, deleting older ones.
// Caller must hold g_mutex.
void pruneOldLogFilesLocked()
{
    const QDir dir(g_logDir);
    if (!dir.exists()) {
        return;
    }

    QStringList fileNames;
    const QFileInfoList entries =
        dir.entryInfoList({QStringLiteral("*.log")}, QDir::Files, QDir::NoSort);
    for (const QFileInfo &entry : entries) {
        if (QDate::fromString(entry.completeBaseName(), QStringLiteral("yyyy-MM-dd")).isValid()) {
            fileNames.append(entry.fileName());
        }
    }
    std::sort(fileNames.begin(), fileNames.end());

    while (fileNames.size() > kMaxLogFiles) {
        QFile::remove(dir.filePath(fileNames.takeFirst()));
    }
}

// Caller must hold g_mutex.
bool openLogFileLocked(const QDate &date)
{
    g_fileDate = date;
    if (g_file.isOpen()) {
        g_file.close();
    }
    if (!QDir().mkpath(g_logDir)) {
        return false;
    }
    g_file.setFileName(logFilePathForDate(date));
    if (!g_file.open(QIODevice::WriteOnly | QIODevice::Append | QIODevice::Text)) {
        return false;
    }
    pruneOldLogFilesLocked();
    return true;
}

void messageHandler(QtMsgType type, const QMessageLogContext &context,
                    const QString &message)
{
    const QDateTime now = QDateTime::currentDateTime();
    const QByteArray line = formatLine(now, type, context, message);

    {
        QMutexLocker locker(&g_mutex);
        const QDate today = now.date();
        if (g_fileDate != today) {
            // Date rolled over: close yesterday's file so it is reopened below.
            if (g_file.isOpen()) {
                g_file.close();
            }
            g_fileDate = today;
            g_openFailed = false;
        }
        if (!g_file.isOpen() && !g_openFailed && !openLogFileLocked(today)) {
            g_openFailed = true;
        }
        if (g_file.isOpen()) {
            g_file.write(line);
            g_file.flush();
        }
    }

    // Mirror to stderr so logs stay visible when launched from a terminal.
    std::fwrite(line.constData(), 1, static_cast<size_t>(line.size()), stderr);
    std::fflush(stderr);
}

} // namespace

bool Logger::install()
{
    QMutexLocker locker(&g_mutex);
    if (g_installed) {
        return !g_openFailed;
    }

    g_logDir = logDirectoryPath();
    g_fileDate = QDate::currentDate();
    g_openFailed = !openLogFileLocked(g_fileDate);
    g_installed = true;
    qInstallMessageHandler(messageHandler);

    return !g_openFailed;
}

QString Logger::logDirectory()
{
    QMutexLocker locker(&g_mutex);
    return g_logDir;
}
