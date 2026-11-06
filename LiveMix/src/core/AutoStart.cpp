#include "AutoStart.h"

#include <QCoreApplication>
#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QSaveFile>
#include <QSettings>

#if defined(Q_OS_MACOS)
namespace {

QString launchAgentPath()
{
    return QDir::homePath()
        + QStringLiteral("/Library/LaunchAgents/com.livemix.studio.plist");
}

QString xmlEscape(const QString &value)
{
    QString escaped = value;
    escaped.replace(QLatin1Char('&'), QStringLiteral("&amp;"));
    escaped.replace(QLatin1Char('<'), QStringLiteral("&lt;"));
    escaped.replace(QLatin1Char('>'), QStringLiteral("&gt;"));
    escaped.replace(QLatin1Char('"'), QStringLiteral("&quot;"));
    escaped.replace(QLatin1Char('\''), QStringLiteral("&apos;"));
    return escaped;
}

} // namespace
#endif

namespace AutoStart {

bool isSupported()
{
#if defined(Q_OS_MACOS) || defined(Q_OS_WIN)
    return true;
#else
    return false;
#endif
}

bool setEnabled(bool enabled, QString *error)
{
#if defined(Q_OS_MACOS)
    const QString path = launchAgentPath();

    if (!enabled) {
        if (QFileInfo::exists(path) && !QFile::remove(path)) {
            if (error) {
                *error = QStringLiteral("无法删除开机启动配置：%1").arg(path);
            }
            return false;
        }
        return true;
    }

    const QString appPath = QCoreApplication::applicationFilePath();
    if (appPath.startsWith(QLatin1String("/Volumes/"))) {
        if (error) {
            *error = QStringLiteral("请先将应用移动到 Applications 文件夹，再启用开机启动。");
        }
        return false;
    }

    if (!QDir().mkpath(QFileInfo(path).absolutePath())) {
        if (error) {
            *error = QStringLiteral("无法创建 LaunchAgents 目录。");
        }
        return false;
    }

    const QString plist = QStringLiteral(
        "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n"
        "<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" "
        "\"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n"
        "<plist version=\"1.0\">\n"
        "<dict>\n"
        "  <key>Label</key>\n"
        "  <string>com.livemix.studio</string>\n"
        "  <key>ProgramArguments</key>\n"
        "  <array>\n"
        "    <string>%1</string>\n"
        "  </array>\n"
        "  <key>RunAtLoad</key>\n"
        "  <true/>\n"
        "  <key>LimitLoadToSessionType</key>\n"
        "  <string>Aqua</string>\n"
        "  <key>ProcessType</key>\n"
        "  <string>Interactive</string>\n"
        "</dict>\n"
        "</plist>\n")
        .arg(xmlEscape(appPath));

    QSaveFile file(path);
    if (!file.open(QIODevice::WriteOnly)) {
        if (error) {
            *error = QStringLiteral("无法写入开机启动配置：%1").arg(file.errorString());
        }
        return false;
    }
    const QByteArray payload = plist.toUtf8();
    if (file.write(payload) != payload.size() || !file.commit()) {
        if (error) {
            *error = QStringLiteral("保存开机启动配置失败：%1").arg(file.errorString());
        }
        return false;
    }
    QFile::setPermissions(path, QFile::ReadOwner | QFile::WriteOwner | QFile::ReadGroup
                                    | QFile::ReadOther);
    return true;
#elif defined(Q_OS_WIN)
    QSettings settings(
        QStringLiteral("HKEY_CURRENT_USER\\Software\\Microsoft\\Windows\\CurrentVersion\\Run"),
        QSettings::NativeFormat);

    if (enabled) {
        const QString appPath =
            QDir::toNativeSeparators(QCoreApplication::applicationFilePath());
        settings.setValue(QStringLiteral("LiveMixStudio"),
                          QStringLiteral("\"%1\"").arg(appPath));
    } else {
        settings.remove(QStringLiteral("LiveMixStudio"));
    }
    settings.sync();
    if (settings.status() != QSettings::NoError) {
        if (error) {
            *error = QStringLiteral("无法更新 Windows 开机启动项。");
        }
        return false;
    }
    return true;
#else
    Q_UNUSED(enabled);
    if (error) {
        *error = QStringLiteral("当前平台不支持开机启动。");
    }
    return false;
#endif
}

} // namespace AutoStart
