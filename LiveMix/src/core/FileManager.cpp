#include "FileManager.h"

#include <QDesktopServices>
#include <QDir>
#include <QFileInfo>
#include <QProcess>
#include <QUrl>

bool FileManager::revealDraftFolder(const QString &path)
{
#ifdef Q_OS_MACOS
    // `open -R` 让 Finder 打开父目录并选中目标：列视图会自动横向滚动到目标列。
    // 直接 openUrl(目录) 只把窗口定位到目录本身，窄窗口下最右几列会被裁到窗口外，
    // 看起来像是只打开了上级目录。
    return QProcess::startDetached(QStringLiteral("/usr/bin/open"),
                                   {QStringLiteral("-R"), path});
#else
    return QDesktopServices::openUrl(QUrl::fromLocalFile(path));
#endif
}

bool FileManager::revealPath(const QString &path)
{
#ifdef Q_OS_MACOS
    return QProcess::startDetached(QStringLiteral("/usr/bin/open"),
                                   {QStringLiteral("-R"), path});
#else
    const QFileInfo info(path);
    // Windows 对文件使用 /select 才能在资源管理器中选中它。
    if (info.isFile()) {
        return QProcess::startDetached(
            QStringLiteral("explorer.exe"),
            {QStringLiteral("/select,") + QDir::toNativeSeparators(path)});
    }
    return QDesktopServices::openUrl(QUrl::fromLocalFile(path));
#endif
}

QString FileManager::revealLabel()
{
#ifdef Q_OS_MACOS
    return QStringLiteral("在 Finder 中显示");
#else
    return QStringLiteral("打开草稿文件夹");
#endif
}

QString FileManager::revealPathLabel()
{
#ifdef Q_OS_MACOS
    return QStringLiteral("在访达中显示");
#else
    return QStringLiteral("在资源管理器中显示");
#endif
}

QString FileManager::openFolderLabel()
{
#ifdef Q_OS_MACOS
    return QStringLiteral("在访达中打开");
#else
    return QStringLiteral("在资源管理器中打开");
#endif
}

QString FileManager::revealDoneMessage(const QString &draftTitle)
{
#ifdef Q_OS_MACOS
    return QStringLiteral("已在 Finder 中显示草稿 %1").arg(draftTitle);
#else
    return QStringLiteral("已打开草稿 %1 所在文件夹").arg(draftTitle);
#endif
}
