#pragma once

#include <QString>

// User preferences only. Business records live in MediaRepository/SQLite.
struct ServiceConfig
{
    QString apiUrl = QStringLiteral("https://api.jcaigc.cn/v1");
    QString apiKey;
};

struct StorageConfig
{
    QString recordingDir;
    QString exportDir;
    QString draftDir;
    QString cacheDir;
    int cacheLimitGb = 100;
    bool autoCleanCache = true;
};

struct RecordingConfig
{
    QString format = QStringLiteral("hls");
    QString ffmpegPath;
    int segmentSeconds = 4;
    bool reconnect = true;
    int reconnectDelayMaxSeconds = 5;
    int stopTimeoutMs = 8000;
};

struct AiConfig
{
    QString defaultPrompt = QStringLiteral("默认提示词");
    bool exportSrt = true;
};

struct GeneralConfig
{
    QString theme = QStringLiteral("dark");
    bool launchAtLogin = false;
    bool minimizeToBackground = true;
    bool soundOnComplete = true;
    bool systemNotifications = true;
};

// 成片设置：自动包装开关、视频倍速与单条成片时长，应用到之后每次一键成片。
struct FinishConfig
{
    bool transitions = true;
    bool effects = true;
    bool filters = true;
    bool stickers = true;
    bool bgm = true;
    bool sfx = true;
    // true：每条成片在 1.0×–1.5× 之间独立随机取速；false：固定为 fixedSpeed。
    bool speedAuto = true;
    double fixedSpeed = 1.0; // 0.5–2.0
    int clipDuration = 60;   // 单条成片目标时长（秒），30–600
};

struct AppConfig
{
    int version = 1;
    ServiceConfig service;
    StorageConfig storage;
    RecordingConfig recording;
    AiConfig ai;
    FinishConfig finish;
    GeneralConfig general;
};

class ConfigStore
{
public:
    explicit ConfigStore(const QString &filePath = QString());

    QString filePath() const { return m_filePath; }
    const AppConfig &config() const { return m_config; }
    void setConfig(const AppConfig &config);

    // Loads config.json, creating it with defaults when missing. On parse
    // failure the in-memory config falls back to defaults and false is returned.
    bool load(QString *error = nullptr);
    bool save(QString *error = nullptr);

    static QString defaultFilePath();
    static AppConfig defaults();

private:
    static AppConfig normalized(const AppConfig &config);
    QString m_filePath;
    AppConfig m_config;
};
