#pragma once

#include "ClipTypes.h"
#include "MediaTypes.h"

#include <QSqlDatabase>
#include <QString>
#include <QVector>

struct RecordingHandle
{
    qint64 sourceId = -1;
    qint64 sessionId = -1;
};

// SQLite-backed business data: source videos and recording sessions.
// User preferences are intentionally not stored here; see ConfigStore.
class MediaRepository
{
public:
    explicit MediaRepository(const QString &databasePath = QString());
    ~MediaRepository();

    MediaRepository(const MediaRepository &) = delete;
    MediaRepository &operator=(const MediaRepository &) = delete;

    QString databasePath() const { return m_databasePath; }
    bool isOpen() const { return m_db.isOpen(); }
    bool open(QString *error = nullptr);
    void close();

    QVector<MediaSource> sources(QString *error = nullptr) const;
    MediaSource source(qint64 sourceId, QString *error = nullptr) const;

    // ASR 缓存：fingerprint 由源文件大小和修改时间组成，源文件变化后会重新识别。
    // found 表示是否命中缓存；函数返回值表示数据库操作是否成功。
    bool loadCachedAsr(qint64 sourceId, const QString &fingerprint,
                       AsrTranscript *transcript, bool *found,
                       QString *error = nullptr) const;
    bool saveAsrResult(qint64 sourceId, const QString &fingerprint,
                       const AsrTranscript &transcript, QString *error = nullptr);

    // 剪映草稿记录（一条切片对应一条草稿）。
    qint64 addClipDraft(const ClipDraft &draft, QString *error = nullptr);
    QVector<ClipDraft> clipDrafts(qint64 sourceId = -1,
                                  QString *error = nullptr) const;
    ClipDraft clipDraft(qint64 id, QString *error = nullptr) const;
    bool markDraftDownloaded(qint64 draftId, const QString &localPath,
                             QString *error = nullptr);

    // 一键成片任务：启动时创建，进度随时更新，结束后保留历史供任务中心回看。
    qint64 createClipTask(qint64 sourceId, const QString &title,
                          QString *error = nullptr);
    bool updateClipTask(qint64 taskId, int step, int progress, const QString &message,
                        QString *error = nullptr);
    bool incrementClipTaskDrafts(qint64 taskId, QString *error = nullptr);
    bool finishClipTask(qint64 taskId, ClipTaskStatus status, const QString &message,
                        QString *error = nullptr);
    QVector<ClipTask> clipTasks(QString *error = nullptr) const;
    ClipTask clipTask(qint64 id, QString *error = nullptr) const;
    // 应用退出后遗留的 running 任务标记为失败，避免任务中心永远显示运行中。
    bool recoverInterruptedClipTasks(QString *error = nullptr);

    // container 为 "hls"（直播录制）或 "mp4"（mp4 直链下载）。
    RecordingHandle createRecordingSource(const QString &title, const QString &sourceUri,
                                          const QString &outputPath, qint64 startedAtMs,
                                          const QString &container = QStringLiteral("hls"),
                                          QString *error = nullptr);

    qint64 importLocalSource(const QString &title, const QString &filePath,
                             const QString &note, const ProbeInfo &probe,
                             QString *error = nullptr);

    bool finishRecording(qint64 sourceId, qint64 sessionId, SessionStatus sessionStatus,
                         SourceStatus sourceStatus, qint64 durationMs, qint64 sizeBytes,
                         const QString &errorMessage, qint64 finishedAtMs,
                         QString *error = nullptr);

    bool removeSource(qint64 sourceId, QString *error = nullptr);

    // Marks sessions left in the "recording" state by a previous process as
    // interrupted. Partial recordings are kept and flagged with a warning.
    bool recoverInterrupted(QString *error = nullptr);

    static QString defaultDatabasePath();

private:
    bool ensureSchema(QString *error);
    int schemaVersion();

    QString m_connectionName;
    QString m_databasePath;
    QSqlDatabase m_db;
};
