#include "AppConfig.h"

#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QJsonDocument>
#include <QJsonObject>
#include <QJsonParseError>
#include <QSaveFile>
#include <QStandardPaths>

namespace {

constexpr int kConfigVersion = 2;

// 旧版本的默认缓存上限；升级到 v2 时把仍为该值的配置迁移到新的 100GB 默认。
constexpr int kLegacyCacheLimitGb = 10;

QString moviesDirectory()
{
    QString dir = QStandardPaths::writableLocation(QStandardPaths::MoviesLocation);
    if (dir.isEmpty()) {
        dir = QDir::homePath();
    }
    return dir;
}

QString cacheDirectory()
{
    QString dir = QStandardPaths::writableLocation(QStandardPaths::CacheLocation);
    if (dir.isEmpty()) {
        dir = QDir(QDir::tempPath()).filePath(QStringLiteral("LiveMix"));
    }
    return dir;
}

QString stringValue(const QJsonObject &object, const QString &key, const QString &fallback)
{
    const QJsonValue value = object.value(key);
    if (!value.isString()) {
        return fallback;
    }
    const QString text = value.toString().trimmed();
    return text.isEmpty() ? fallback : text;
}

// Like stringValue but preserves an explicitly empty string, which is valid
// for optional values such as the API key.
QString rawStringValue(const QJsonObject &object, const QString &key,
                       const QString &fallback)
{
    const QJsonValue value = object.value(key);
    return value.isString() ? value.toString().trimmed() : fallback;
}

QString pathValue(const QJsonObject &object, const QString &key, const QString &fallback)
{
    const QJsonValue value = object.value(key);
    if (!value.isString() || value.toString().trimmed().isEmpty()) {
        return fallback;
    }
    return QDir::cleanPath(value.toString().trimmed());
}

int intValue(const QJsonObject &object, const QString &key, int fallback)
{
    const QJsonValue value = object.value(key);
    return value.isDouble() ? value.toInt(fallback) : fallback;
}

bool boolValue(const QJsonObject &object, const QString &key, bool fallback)
{
    const QJsonValue value = object.value(key);
    return value.isBool() ? value.toBool() : fallback;
}

QJsonObject toJson(const AppConfig &config)
{
    QJsonObject service;
    service.insert(QStringLiteral("apiUrl"), config.service.apiUrl);
    service.insert(QStringLiteral("apiKey"), config.service.apiKey);

    QJsonObject storage;
    storage.insert(QStringLiteral("recordingDir"), config.storage.recordingDir);
    storage.insert(QStringLiteral("exportDir"), config.storage.exportDir);
    storage.insert(QStringLiteral("draftDir"), config.storage.draftDir);
    storage.insert(QStringLiteral("cacheDir"), config.storage.cacheDir);
    storage.insert(QStringLiteral("cacheLimitGb"), config.storage.cacheLimitGb);
    storage.insert(QStringLiteral("autoCleanCache"), config.storage.autoCleanCache);

    QJsonObject recording;
    recording.insert(QStringLiteral("format"), config.recording.format);
    recording.insert(QStringLiteral("ffmpegPath"), config.recording.ffmpegPath);
    recording.insert(QStringLiteral("segmentSeconds"), config.recording.segmentSeconds);
    recording.insert(QStringLiteral("reconnect"), config.recording.reconnect);
    recording.insert(QStringLiteral("reconnectDelayMaxSeconds"),
                     config.recording.reconnectDelayMaxSeconds);
    recording.insert(QStringLiteral("stopTimeoutMs"), config.recording.stopTimeoutMs);

    QJsonObject ai;
    ai.insert(QStringLiteral("defaultPrompt"), config.ai.defaultPrompt);
    ai.insert(QStringLiteral("exportSrt"), config.ai.exportSrt);

    QJsonObject general;
    general.insert(QStringLiteral("theme"), config.general.theme);
    general.insert(QStringLiteral("launchAtLogin"), config.general.launchAtLogin);
    general.insert(QStringLiteral("minimizeToBackground"),
                   config.general.minimizeToBackground);
    general.insert(QStringLiteral("soundOnComplete"), config.general.soundOnComplete);
    general.insert(QStringLiteral("systemNotifications"),
                   config.general.systemNotifications);

    QJsonObject root;
    root.insert(QStringLiteral("version"), config.version);
    root.insert(QStringLiteral("service"), service);
    root.insert(QStringLiteral("storage"), storage);
    root.insert(QStringLiteral("recording"), recording);
    root.insert(QStringLiteral("ai"), ai);
    root.insert(QStringLiteral("general"), general);
    return root;
}

} // namespace

ConfigStore::ConfigStore(const QString &filePath)
    : m_filePath(filePath.isEmpty() ? defaultFilePath() : QDir::cleanPath(filePath))
    , m_config(defaults())
{
}

QString ConfigStore::defaultFilePath()
{
    const QString overridePath = qEnvironmentVariable("LIVEMIX_CONFIG_PATH").trimmed();
    if (!overridePath.isEmpty()) {
        return QDir::cleanPath(overridePath);
    }
    QString dir = QStandardPaths::writableLocation(QStandardPaths::AppConfigLocation);
    if (dir.isEmpty()) {
        dir = QDir::homePath() + QStringLiteral("/.livemix");
    }
    return QDir(dir).filePath(QStringLiteral("config.json"));
}

AppConfig ConfigStore::defaults()
{
    AppConfig config;
    const QString movies = moviesDirectory();
    config.storage.recordingDir = QDir(movies).filePath(QStringLiteral("LiveMix/Recordings"));
    config.storage.exportDir = QDir(movies).filePath(QStringLiteral("LiveMix/Exports"));
    config.storage.draftDir = QDir(movies).filePath(QStringLiteral("LiveMix/CapDraft"));
    config.storage.cacheDir = cacheDirectory();
    return config;
}

AppConfig ConfigStore::normalized(const AppConfig &config)
{
    AppConfig result = config;
    const AppConfig fallback = defaults();

    result.version = kConfigVersion;
    if (result.storage.recordingDir.trimmed().isEmpty()) {
        result.storage.recordingDir = fallback.storage.recordingDir;
    }
    if (result.storage.exportDir.trimmed().isEmpty()) {
        result.storage.exportDir = fallback.storage.exportDir;
    }
    if (result.storage.draftDir.trimmed().isEmpty()) {
        result.storage.draftDir = fallback.storage.draftDir;
    }
    if (result.storage.cacheDir.trimmed().isEmpty()) {
        result.storage.cacheDir = fallback.storage.cacheDir;
    }
    result.storage.recordingDir = QDir::cleanPath(result.storage.recordingDir);
    result.storage.exportDir = QDir::cleanPath(result.storage.exportDir);
    result.storage.draftDir = QDir::cleanPath(result.storage.draftDir);
    result.storage.cacheDir = QDir::cleanPath(result.storage.cacheDir);
    result.storage.cacheLimitGb = qBound(1, result.storage.cacheLimitGb, 1000);

    if (result.recording.format != QLatin1String("hls")) {
        result.recording.format = QStringLiteral("hls");
    }
    result.recording.segmentSeconds = qBound(1, result.recording.segmentSeconds, 30);
    result.recording.reconnectDelayMaxSeconds =
        qBound(1, result.recording.reconnectDelayMaxSeconds, 30);
    result.recording.stopTimeoutMs = qBound(1000, result.recording.stopTimeoutMs, 60000);
    result.recording.ffmpegPath = result.recording.ffmpegPath.trimmed();

    result.service.apiUrl = result.service.apiUrl.trimmed();
    result.service.apiKey = result.service.apiKey.trimmed();

    result.ai.defaultPrompt = result.ai.defaultPrompt.trimmed();
    if (result.ai.defaultPrompt.isEmpty()) {
        result.ai.defaultPrompt = fallback.ai.defaultPrompt;
    }

    if (result.general.theme != QLatin1String("system")) {
        result.general.theme = QStringLiteral("dark");
    }
    return result;
}

void ConfigStore::setConfig(const AppConfig &config)
{
    m_config = normalized(config);
}

bool ConfigStore::load(QString *error)
{
    m_config = defaults();

    const QFileInfo info(m_filePath);
    QDir().mkpath(info.absolutePath());

    QFile file(m_filePath);
    if (!file.exists()) {
        return save(error);
    }
    if (!file.open(QIODevice::ReadOnly)) {
        if (error) {
            *error = QStringLiteral("无法读取配置文件：%1").arg(file.errorString());
        }
        return false;
    }

    QJsonParseError parseError;
    const QJsonDocument document = QJsonDocument::fromJson(file.readAll(), &parseError);
    if (parseError.error != QJsonParseError::NoError || !document.isObject()) {
        if (error) {
            *error = QStringLiteral("配置文件格式错误：%1").arg(parseError.errorString());
        }
        return false;
    }

    const QJsonObject root = document.object();
    // 缺失 version 的旧配置按 v1 处理。
    const int fileVersion = root.value(QStringLiteral("version")).toInt(1);
    const QJsonObject service = root.value(QStringLiteral("service")).toObject();
    const QJsonObject storage = root.value(QStringLiteral("storage")).toObject();
    const QJsonObject recording = root.value(QStringLiteral("recording")).toObject();
    const QJsonObject ai = root.value(QStringLiteral("ai")).toObject();
    const QJsonObject general = root.value(QStringLiteral("general")).toObject();

    AppConfig loaded = defaults();
    loaded.storage.recordingDir =
        pathValue(storage, QStringLiteral("recordingDir"), loaded.storage.recordingDir);
    loaded.storage.exportDir =
        pathValue(storage, QStringLiteral("exportDir"), loaded.storage.exportDir);
    loaded.storage.draftDir =
        pathValue(storage, QStringLiteral("draftDir"), loaded.storage.draftDir);
    loaded.storage.cacheDir =
        pathValue(storage, QStringLiteral("cacheDir"), loaded.storage.cacheDir);
    loaded.storage.cacheLimitGb =
        intValue(storage, QStringLiteral("cacheLimitGb"), loaded.storage.cacheLimitGb);
    // v2 起默认缓存上限提高到 100GB。只迁移仍保留旧默认值的配置，
    // 用户手动改过的数值保持不变。
    if (fileVersion < 2 && loaded.storage.cacheLimitGb == kLegacyCacheLimitGb) {
        loaded.storage.cacheLimitGb = AppConfig{}.storage.cacheLimitGb;
    }
    loaded.storage.autoCleanCache =
        boolValue(storage, QStringLiteral("autoCleanCache"), loaded.storage.autoCleanCache);

    loaded.recording.format =
        stringValue(recording, QStringLiteral("format"), loaded.recording.format);
    loaded.recording.ffmpegPath =
        stringValue(recording, QStringLiteral("ffmpegPath"), QString());
    loaded.recording.segmentSeconds =
        intValue(recording, QStringLiteral("segmentSeconds"), loaded.recording.segmentSeconds);
    loaded.recording.reconnect =
        boolValue(recording, QStringLiteral("reconnect"), loaded.recording.reconnect);
    loaded.recording.reconnectDelayMaxSeconds =
        intValue(recording, QStringLiteral("reconnectDelayMaxSeconds"),
                 loaded.recording.reconnectDelayMaxSeconds);
    loaded.recording.stopTimeoutMs =
        intValue(recording, QStringLiteral("stopTimeoutMs"), loaded.recording.stopTimeoutMs);

    loaded.service.apiUrl =
        rawStringValue(service, QStringLiteral("apiUrl"), loaded.service.apiUrl);
    loaded.service.apiKey =
        rawStringValue(service, QStringLiteral("apiKey"), loaded.service.apiKey);

    loaded.ai.defaultPrompt =
        stringValue(ai, QStringLiteral("defaultPrompt"), loaded.ai.defaultPrompt);
    loaded.ai.exportSrt =
        boolValue(ai, QStringLiteral("exportSrt"), loaded.ai.exportSrt);

    loaded.general.theme =
        stringValue(general, QStringLiteral("theme"), loaded.general.theme);
    loaded.general.launchAtLogin =
        boolValue(general, QStringLiteral("launchAtLogin"), loaded.general.launchAtLogin);
    loaded.general.minimizeToBackground = boolValue(
        general, QStringLiteral("minimizeToBackground"), loaded.general.minimizeToBackground);
    loaded.general.soundOnComplete = boolValue(
        general, QStringLiteral("soundOnComplete"), loaded.general.soundOnComplete);
    loaded.general.systemNotifications = boolValue(
        general, QStringLiteral("systemNotifications"), loaded.general.systemNotifications);

    m_config = normalized(loaded);
    return true;
}

bool ConfigStore::save(QString *error)
{
    m_config = normalized(m_config);

    const QFileInfo info(m_filePath);
    if (!QDir().mkpath(info.absolutePath())) {
        if (error) {
            *error = QStringLiteral("无法创建配置目录：%1").arg(info.absolutePath());
        }
        return false;
    }

    QSaveFile file(m_filePath);
    if (!file.open(QIODevice::WriteOnly)) {
        if (error) {
            *error = QStringLiteral("无法写入配置文件：%1").arg(file.errorString());
        }
        return false;
    }
    const QByteArray payload =
        QJsonDocument(toJson(m_config)).toJson(QJsonDocument::Indented);
    if (file.write(payload) != payload.size()) {
        if (error) {
            *error = QStringLiteral("写入配置文件失败：%1").arg(file.errorString());
        }
        file.cancelWriting();
        return false;
    }
    if (!file.commit()) {
        if (error) {
            *error = QStringLiteral("保存配置文件失败：%1").arg(file.errorString());
        }
        return false;
    }
#ifndef Q_OS_WIN
    // The JSON may contain an API key; keep it private to the current user.
    QFile::setPermissions(m_filePath, QFile::ReadOwner | QFile::WriteOwner);
#endif
    return true;
}
