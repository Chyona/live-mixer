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

QString FileManager::revealPathLabel()
{
#ifdef Q_OS_MACOS
    return QStringLiteral("在访达中显示");
#else
    return QStringLiteral("在资源管理器中显示");
#endif
}

QString FileManager::openDraftFolderText()
{
    return QStringLiteral("打开草稿文件夹");
}

QString FileManager::openDraftFolderHint(bool withDraftHint)
{
#ifdef Q_OS_MACOS
    const QString base = QStringLiteral("在访达中打开剪映草稿文件夹");
#else
    const QString base = QStringLiteral("在资源管理器中打开剪映草稿文件夹");
#endif
    return withDraftHint ? base + QStringLiteral("，可在剪映中打开该草稿继续编辑")
                         : base;
}

QString FileManager::draftFolderOpenedMessage(const QString &draftName)
{
#ifdef Q_OS_MACOS
    return QStringLiteral("已在访达中打开剪映草稿文件夹，可在剪映中打开「%1」继续编辑")
        .arg(draftName);
#else
    return QStringLiteral(
               "已在资源管理器中打开剪映草稿文件夹，可在剪映中打开「%1」继续编辑")
        .arg(draftName);
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

