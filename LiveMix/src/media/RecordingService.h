#pragma once

#include <QHash>
#include <QObject>
#include <QProcess>
#include <QSet>
#include <QString>

class ConfigStore;
class MediaRepository;
class QNetworkAccessManager;
class QNetworkReply;

// Owns online sources: progressive mp4 links are downloaded over HTTP as the
// original file, while live/HLS/unknown links are recorded with ffmpeg.
// Configuration is read from ConfigStore at job start.
class RecordingService : public QObject
{
    Q_OBJECT

public:
    struct StartResult
    {
        bool ok = false;
        qint64 sourceId = -1;
        qint64 sessionId = -1;
        QString error;
    };

    RecordingService(ConfigStore *configStore, MediaRepository *repository,
                     QObject *parent = nullptr);
    ~RecordingService() override;

    StartResult startOnlineRecording(const QString &uri, const QString &title = QString());
    bool stopRecording(qint64 sourceId);
    bool isRecording(qint64 sourceId) const;
    void stopAllAndWait(int timeoutMs = 8000);
    void shutdown();

signals:
    void sourceAdded(qint64 sourceId);
    void recordingProgress(qint64 sourceId, qint64 durationMs);
    // 直链下载进度：totalBytes 未知时为 -1。
    void downloadProgress(qint64 sourceId, qint64 receivedBytes, qint64 totalBytes);
    // 下载后的处理阶段（如音频转码）提示。
    void downloadStatus(qint64 sourceId, const QString &text);
    void recordingFinished(qint64 sourceId, bool success, const QString &message);

private:
    struct Job;
    struct Probe;

    Job *findJob(qint64 sourceId) const;

    // 先探测链接内容：mp4 直链走下载，其余交给 ffmpeg 录制。
    void beginLinkProbe(const QString &uri, const QString &title);
    void finishLinkProbe(Probe *probe, bool timedOut);
    void abortPendingProbes();

    // forceHls：探测已确认内容是 HLS 播放列表时显式指定 demuxer，
    // 避免无扩展名 / 非标准 MIME 的链接被 ffmpeg 拒绝。
    StartResult startStreamRecording(const QString &sourceUri, const QString &title,
                                     bool forceHls = false);
    void startDownloadJob(const QString &sourceUri, const QString &title);
    void handleDownloadReadyRead(Job *job);
    void handleDownloadFinished(Job *job);
    void beginAudioNormalization(Job *job);
    // warning 写入源视频的 error_message；message 用于完成提示。
    void finishDownloadFile(Job *job, bool normalized, const QString &message,
                            const QString &warning = QString());

    void handleStandardOutput(Job *job);
    void handleStandardError(Job *job);
    void handleFinished(Job *job, int exitCode, QProcess::ExitStatus exitStatus);
    void finalize(Job *job, bool success, const QString &message);

    static QString deriveTitle(const QString &uri, const QString &title);
    static bool isHttpUrl(const QString &uri);

    ConfigStore *m_configStore = nullptr;
    MediaRepository *m_repository = nullptr;
    QNetworkAccessManager *m_network = nullptr;
    QHash<qint64, Job *> m_jobs;
    QSet<Probe *> m_probes;
};
