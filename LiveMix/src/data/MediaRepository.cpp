// 文件作用：SQLite 业务数据访问，包含源视频、录制会话、ASR 缓存与剪映草稿。
#include "MediaRepository.h"

#include "../core/MediaUtils.h"

#include <QDateTime>
#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QJsonArray>
#include <QJsonDocument>
#include <QSqlError>
#include <QSqlQuery>
#include <QStandardPaths>
#include <QVariant>

namespace {

constexpr int kSchemaVersion = 4;

// SQLite NOT NULL TEXT columns reject a null QString, but Qt's default
// constructed QString is null. Normalize empty user-facing text to "".
QString textOrEmpty(const QString &value)
{
    return value.isNull() ? QStringLiteral("") : value;
}

QString kindToDb(SourceKind kind)
{
    return kind == SourceKind::Local ? QStringLiteral("local") : QStringLiteral("online");
}

SourceKind kindFromDb(const QString &value)
{
    return value == QLatin1String("local") ? SourceKind::Local : SourceKind::Online;
}

QString sourceStatusToDb(SourceStatus status)
{
    switch (status) {
    case SourceStatus::Recording:
        return QStringLiteral("recording");
    case SourceStatus::Ready:
        return QStringLiteral("ready");
    case SourceStatus::Failed:
        return QStringLiteral("failed");
    }
    return QStringLiteral("failed");
}

SourceStatus sourceStatusFromDb(const QString &value)
{
    if (value == QLatin1String("recording")) {
        return SourceStatus::Recording;
    }
    if (value == QLatin1String("ready")) {
        return SourceStatus::Ready;
    }
    return SourceStatus::Failed;
}

QString sessionStatusToDb(SessionStatus status)
{
    switch (status) {
    case SessionStatus::Recording:
        return QStringLiteral("recording");
    case SessionStatus::Completed:
        return QStringLiteral("completed");
    case SessionStatus::Failed:
        return QStringLiteral("failed");
    case SessionStatus::Cancelled:
        return QStringLiteral("cancelled");
    }
    return QStringLiteral("failed");
}

MediaSource sourceFromQuery(const QSqlQuery &query)
{
    MediaSource source;
    source.id = query.value(0).toLongLong();
    source.title = query.value(1).toString();
    source.kind = kindFromDb(query.value(2).toString());
    source.sourceUri = query.value(3).toString();
    source.localPath = query.value(4).toString();
    source.note = query.value(5).toString();
    source.status = sourceStatusFromDb(query.value(6).toString());
    source.durationMs = query.value(7).toLongLong();
    source.sizeBytes = query.value(8).toLongLong();
    source.probeJson = query.value(9).toString();
    source.errorMessage = query.value(10).toString();
    source.createdAtMs = query.value(11).toLongLong();
    source.updatedAtMs = query.value(12).toLongLong();
    source.draftCount = query.value(13).toInt();
    return source;
}

QString tagsToJson(const QStringList &tags)
{
    QJsonArray array;
    for (const QString &tag : tags) {
        array.append(tag);
    }
    return QString::fromUtf8(QJsonDocument(array).toJson(QJsonDocument::Compact));
}

QStringList tagsFromJson(const QString &text)
{
    QStringList tags;
    const QJsonArray array = QJsonDocument::fromJson(text.toUtf8()).array();
    for (const QJsonValue &value : array) {
        const QString tag = value.toString();
        if (!tag.isEmpty()) {
            tags.append(tag);
        }
    }
    return tags;
}

ClipDraft draftFromQuery(const QSqlQuery &query)
{
    ClipDraft draft;
    draft.id = query.value(0).toLongLong();
    draft.sourceId = query.value(1).toLongLong();
    draft.draftId = query.value(2).toString();
    draft.draftUrl = query.value(3).toString();
    draft.title = query.value(4).toString();
    draft.description = query.value(5).toString();
    draft.tags = tagsFromJson(query.value(6).toString());
    draft.sourceStartMs = query.value(7).toLongLong();
    draft.sourceEndMs = query.value(8).toLongLong();
    draft.durationMs = query.value(9).toLongLong();
    draft.createdAtMs = query.value(10).toLongLong();
    draft.downloadedPath = query.value(11).toString();
    return draft;
}

ClipTaskStatus taskStatusFromDb(const QString &value)
{
    if (value == QLatin1String("running")) {
        return ClipTaskStatus::Running;
    }
    if (value == QLatin1String("completed")) {
        return ClipTaskStatus::Completed;
    }
    return ClipTaskStatus::Failed;
}

ClipTask taskFromQuery(const QSqlQuery &query)
{
    ClipTask task;
    task.id = query.value(0).toLongLong();
    task.sourceId = query.value(1).toLongLong();
    task.title = query.value(2).toString();
    task.status = taskStatusFromDb(query.value(3).toString());
    task.step = query.value(4).toInt();
    task.progress = query.value(5).toInt();
    task.message = query.value(6).toString();
    task.draftCount = query.value(7).toInt();
    task.createdAtMs = query.value(8).toLongLong();
    task.updatedAtMs = query.value(9).toLongLong();
    task.finishedAtMs = query.value(10).toLongLong();
    return task;
}

bool execSchema(QSqlDatabase &db, const QString &sql, QString *error)
{
    QSqlQuery query(db);
    if (query.exec(sql)) {
        return true;
    }
    if (error) {
        *error = QStringLiteral("%1\nSQL: %2").arg(query.lastError().text(), sql);
    }
    return false;
}

} // namespace

MediaRepository::MediaRepository(const QString &databasePath)
    : m_connectionName(QStringLiteral("livemix-%1")
                           .arg(reinterpret_cast<quintptr>(this), 0, 16))
    , m_databasePath(databasePath.isEmpty() ? defaultDatabasePath()
                                            : QDir::cleanPath(databasePath))
{
}

MediaRepository::~MediaRepository()
{
    close();
}

QString MediaRepository::defaultDatabasePath()
{
    const QString overridePath = qEnvironmentVariable("LIVEMIX_DB_PATH").trimmed();
    if (!overridePath.isEmpty()) {
        return QDir::cleanPath(overridePath);
    }
    QString dir = QStandardPaths::writableLocation(QStandardPaths::AppDataLocation);
    if (dir.isEmpty()) {
        dir = QDir::homePath() + QStringLiteral("/.livemix");
    }
    return QDir(dir).filePath(QStringLiteral("livemix.db"));
}

bool MediaRepository::open(QString *error)
{
    if (m_db.isOpen()) {
        return true;
    }

    if (!QSqlDatabase::isDriverAvailable(QStringLiteral("QSQLITE"))) {
        if (error) {
            *error = QStringLiteral("Qt SQLite 驱动不可用。");
        }
        return false;
    }

    const QFileInfo info(m_databasePath);
    if (!QDir().mkpath(info.absolutePath())) {
        if (error) {
            *error = QStringLiteral("无法创建数据库目录：%1").arg(info.absolutePath());
        }
        return false;
    }

    m_db = QSqlDatabase::addDatabase(QStringLiteral("QSQLITE"), m_connectionName);
    m_db.setDatabaseName(m_databasePath);
    if (!m_db.open()) {
        if (error) {
            *error = QStringLiteral("无法打开数据库：%1").arg(m_db.lastError().text());
        }
        close();
        return false;
    }

    QSqlQuery pragma(m_db);
    if (!pragma.exec(QStringLiteral("PRAGMA foreign_keys = ON"))) {
        if (error) {
            *error = QStringLiteral("无法启用外键约束：%1").arg(pragma.lastError().text());
        }
        close();
        return false;
    }
    pragma.exec(QStringLiteral("PRAGMA journal_mode = WAL"));
    pragma.exec(QStringLiteral("PRAGMA synchronous = NORMAL"));
    pragma.exec(QStringLiteral("PRAGMA busy_timeout = 5000"));

    if (!ensureSchema(error)) {
        close();
        return false;
    }
    return true;
}

void MediaRepository::close()
{
    if (m_db.isValid()) {
        m_db.close();
        m_db = QSqlDatabase();
    }
    if (QSqlDatabase::contains(m_connectionName)) {
        QSqlDatabase::removeDatabase(m_connectionName);
    }
}

int MediaRepository::schemaVersion()
{
    QSqlQuery query(m_db);
    if (!query.exec(QStringLiteral("PRAGMA user_version")) || !query.next()) {
        return -1;
    }
    return query.value(0).toInt();
}

bool MediaRepository::ensureSchema(QString *error)
{
    const int version = schemaVersion();
    if (version < 0) {
        if (error) {
            *error = QStringLiteral("无法读取数据库版本。");
        }
        return false;
    }
    if (version > kSchemaVersion) {
        if (error) {
            *error = QStringLiteral("数据库版本 %1 高于当前应用支持的版本 %2。")
                         .arg(version)
                         .arg(kSchemaVersion);
        }
        return false;
    }
    if (version == kSchemaVersion) {
        return true;
    }

    if (!m_db.transaction()) {
        if (error) {
            *error = QStringLiteral("无法开始数据库事务：%1").arg(m_db.lastError().text());
        }
        return false;
    }

    const QStringList statements = {
        QStringLiteral(
            "CREATE TABLE IF NOT EXISTS media_sources ("
            "id INTEGER PRIMARY KEY AUTOINCREMENT,"
            "title TEXT NOT NULL,"
            "source_kind TEXT NOT NULL CHECK (source_kind IN ('online','local')),"
            "source_uri TEXT NOT NULL,"
            "local_path TEXT NOT NULL DEFAULT '',"
            "note TEXT NOT NULL DEFAULT '',"
            "status TEXT NOT NULL CHECK (status IN ('recording','ready','failed')),"
            "duration_ms INTEGER NOT NULL DEFAULT 0,"
            "size_bytes INTEGER NOT NULL DEFAULT 0,"
            "probe_json TEXT NOT NULL DEFAULT '',"
            "error_message TEXT NOT NULL DEFAULT '',"
            "created_at_ms INTEGER NOT NULL,"
            "updated_at_ms INTEGER NOT NULL"
            ")"),
        QStringLiteral(
            "CREATE TABLE IF NOT EXISTS recording_sessions ("
            "id INTEGER PRIMARY KEY AUTOINCREMENT,"
            "source_id INTEGER NOT NULL REFERENCES media_sources(id) ON DELETE CASCADE,"
            "output_path TEXT NOT NULL,"
            "container TEXT NOT NULL DEFAULT 'hls',"
            "status TEXT NOT NULL CHECK (status IN ('recording','completed','failed','cancelled')),"
            "started_at_ms INTEGER NOT NULL,"
            "finished_at_ms INTEGER NOT NULL DEFAULT 0,"
            "duration_ms INTEGER NOT NULL DEFAULT 0,"
            "size_bytes INTEGER NOT NULL DEFAULT 0,"
            "error_message TEXT NOT NULL DEFAULT ''"
            ")"),
        QStringLiteral(
            "CREATE INDEX IF NOT EXISTS idx_media_sources_status_created "
            "ON media_sources(status, created_at_ms DESC)"),
        QStringLiteral(
            "CREATE INDEX IF NOT EXISTS idx_recording_sessions_source "
            "ON recording_sessions(source_id, status)"),
        // ASR 结果按源视频缓存；重新一键成片时直接复用，避免重复计费。
        QStringLiteral(
            "CREATE TABLE IF NOT EXISTS asr_results ("
            "source_id INTEGER PRIMARY KEY REFERENCES media_sources(id) ON DELETE CASCADE,"
            "fingerprint TEXT NOT NULL,"
            "data_json TEXT NOT NULL,"
            "created_at_ms INTEGER NOT NULL"
            ")"),
        // 每条切片生成的剪映草稿；下载到本地后记录 downloaded_path。
        QStringLiteral(
            "CREATE TABLE IF NOT EXISTS clip_drafts ("
            "id INTEGER PRIMARY KEY AUTOINCREMENT,"
            "source_id INTEGER NOT NULL REFERENCES media_sources(id) ON DELETE CASCADE,"
            "draft_id TEXT NOT NULL,"
            "draft_url TEXT NOT NULL,"
            "title TEXT NOT NULL DEFAULT '',"
            "description TEXT NOT NULL DEFAULT '',"
            "tags_json TEXT NOT NULL DEFAULT '[]',"
            "source_start_ms INTEGER NOT NULL DEFAULT 0,"
            "source_end_ms INTEGER NOT NULL DEFAULT 0,"
            "duration_ms INTEGER NOT NULL DEFAULT 0,"
            "created_at_ms INTEGER NOT NULL,"
            "downloaded_path TEXT NOT NULL DEFAULT ''"
            ")"),
        QStringLiteral(
            "CREATE INDEX IF NOT EXISTS idx_clip_drafts_source "
            "ON clip_drafts(source_id, created_at_ms)"),
        // 一键成片任务；progress/step 用于离开页面后回看，结束后保留历史。
        QStringLiteral(
            "CREATE TABLE IF NOT EXISTS clip_tasks ("
            "id INTEGER PRIMARY KEY AUTOINCREMENT,"
            "source_id INTEGER NOT NULL REFERENCES media_sources(id) ON DELETE CASCADE,"
            "title TEXT NOT NULL DEFAULT '',"
            "status TEXT NOT NULL CHECK (status IN ('running','completed','failed')),"
            "step INTEGER NOT NULL DEFAULT 0,"
            "progress INTEGER NOT NULL DEFAULT 0,"
            "message TEXT NOT NULL DEFAULT '',"
            "draft_count INTEGER NOT NULL DEFAULT 0,"
            "created_at_ms INTEGER NOT NULL,"
            "updated_at_ms INTEGER NOT NULL,"
            "finished_at_ms INTEGER NOT NULL DEFAULT 0"
            ")"),
        QStringLiteral(
            "CREATE INDEX IF NOT EXISTS idx_clip_tasks_created "
            "ON clip_tasks(created_at_ms DESC)"),
    };

    for (const QString &statement : statements) {
        QString statementError;
        if (!execSchema(m_db, statement, &statementError)) {
            m_db.rollback();
            if (error) {
                *error = QStringLiteral("初始化数据库失败：%1").arg(statementError);
            }
            return false;
        }
    }

    // v2 → v3：补上成片实际总时长字段；旧版草稿都是单段视频，用区间长度回填。
    bool hasDurationColumn = false;
    QSqlQuery columnQuery(m_db);
    if (columnQuery.exec(QStringLiteral("PRAGMA table_info(clip_drafts)"))) {
        while (columnQuery.next()) {
            if (columnQuery.value(1).toString() == QLatin1String("duration_ms")) {
                hasDurationColumn = true;
                break;
            }
        }
    }
    if (!hasDurationColumn) {
        QString alterError;
        if (!execSchema(m_db,
                        QStringLiteral("ALTER TABLE clip_drafts ADD COLUMN "
                                       "duration_ms INTEGER NOT NULL DEFAULT 0"),
                        &alterError)) {
            m_db.rollback();
            if (error) {
                *error = QStringLiteral("升级草稿表失败：%1").arg(alterError);
            }
            return false;
        }
        execSchema(m_db,
                   QStringLiteral("UPDATE clip_drafts"
                                  " SET duration_ms = source_end_ms - source_start_ms"
                                  " WHERE duration_ms = 0 AND source_end_ms > source_start_ms"),
                   nullptr);
    }

    QString versionError;
    if (!execSchema(m_db, QStringLiteral("PRAGMA user_version = 4"), &versionError)) {
        m_db.rollback();
        if (error) {
            *error = QStringLiteral("写入数据库版本失败：%1").arg(versionError);
        }
        return false;
    }

    if (!m_db.commit()) {
        if (error) {
            *error = QStringLiteral("提交数据库结构失败：%1").arg(m_db.lastError().text());
        }
        m_db.rollback();
        return false;
    }
    return true;
}

QVector<MediaSource> MediaRepository::sources(QString *error) const
{
    QVector<MediaSource> result;
    if (!m_db.isOpen()) {
        if (error) {
            *error = QStringLiteral("数据库尚未打开。");
        }
        return result;
    }

    QSqlQuery query(m_db);
    query.setForwardOnly(true);
    const QString sql = QStringLiteral(
        "SELECT id, title, source_kind, source_uri, local_path, note, status,"
        " duration_ms, size_bytes, probe_json, error_message,"
        " created_at_ms, updated_at_ms,"
        " (SELECT COUNT(*) FROM clip_drafts"
        "  WHERE clip_drafts.source_id = media_sources.id) AS draft_count"
        " FROM media_sources"
        " ORDER BY CASE status WHEN 'recording' THEN 0 WHEN 'ready' THEN 1 ELSE 2 END,"
        " created_at_ms DESC");
    if (!query.exec(sql)) {
        if (error) {
            *error = QStringLiteral("读取视频库失败：%1").arg(query.lastError().text());
        }
        return result;
    }
    while (query.next()) {
        result.append(sourceFromQuery(query));
    }
    return result;
}

RecordingHandle MediaRepository::createRecordingSource(const QString &title,
                                                        const QString &sourceUri,
                                                        const QString &outputPath,
                                                        qint64 startedAtMs,
                                                        const QString &container,
                                                        QString *error)
{
    RecordingHandle handle;
    if (!m_db.isOpen()) {
        if (error) {
            *error = QStringLiteral("数据库尚未打开。");
        }
        return handle;
    }
    if (!m_db.transaction()) {
        if (error) {
            *error = QStringLiteral("无法开始数据库事务：%1").arg(m_db.lastError().text());
        }
        return handle;
    }

    QSqlQuery sourceQuery(m_db);
    sourceQuery.prepare(QStringLiteral(
        "INSERT INTO media_sources"
        " (title, source_kind, source_uri, local_path, note, status,"
        "  duration_ms, size_bytes, probe_json, error_message,"
        "  created_at_ms, updated_at_ms)"
        " VALUES (?, ?, ?, ?, '', 'recording', 0, 0, '', '', ?, ?)"));
    sourceQuery.addBindValue(textOrEmpty(title));
    sourceQuery.addBindValue(kindToDb(SourceKind::Online));
    sourceQuery.addBindValue(textOrEmpty(sourceUri));
    sourceQuery.addBindValue(textOrEmpty(outputPath));
    sourceQuery.addBindValue(startedAtMs);
    sourceQuery.addBindValue(startedAtMs);

    if (!sourceQuery.exec()) {
        if (error) {
            *error = QStringLiteral("创建源视频记录失败：%1")
                         .arg(sourceQuery.lastError().text());
        }
        m_db.rollback();
        return handle;
    }
    handle.sourceId = sourceQuery.lastInsertId().toLongLong();

    QSqlQuery sessionQuery(m_db);
    sessionQuery.prepare(QStringLiteral(
        "INSERT INTO recording_sessions"
        " (source_id, output_path, container, status, started_at_ms)"
        " VALUES (?, ?, ?, 'recording', ?)"));
    sessionQuery.addBindValue(handle.sourceId);
    sessionQuery.addBindValue(outputPath);
    sessionQuery.addBindValue(textOrEmpty(container));
    sessionQuery.addBindValue(startedAtMs);

    if (!sessionQuery.exec()) {
        if (error) {
            *error = QStringLiteral("创建录制会话失败：%1")
                         .arg(sessionQuery.lastError().text());
        }
        m_db.rollback();
        handle = RecordingHandle();
        return handle;
    }
    handle.sessionId = sessionQuery.lastInsertId().toLongLong();

    if (!m_db.commit()) {
        if (error) {
            *error = QStringLiteral("提交录制记录失败：%1").arg(m_db.lastError().text());
        }
        m_db.rollback();
        return RecordingHandle();
    }
    return handle;
}

qint64 MediaRepository::importLocalSource(const QString &title, const QString &filePath,
                                          const QString &note, const ProbeInfo &probe,
                                          QString *error)
{
    if (!m_db.isOpen()) {
        if (error) {
            *error = QStringLiteral("数据库尚未打开。");
        }
        return -1;
    }

    const qint64 now = QDateTime::currentMSecsSinceEpoch();
    const qint64 size = probe.sizeBytes > 0 ? probe.sizeBytes : QFileInfo(filePath).size();

    QSqlQuery query(m_db);
    query.prepare(QStringLiteral(
        "INSERT INTO media_sources"
        " (title, source_kind, source_uri, local_path, note, status,"
        "  duration_ms, size_bytes, probe_json, error_message,"
        "  created_at_ms, updated_at_ms)"
        " VALUES (?, ?, ?, ?, ?, 'ready', ?, ?, ?, '', ?, ?)"));
    query.addBindValue(textOrEmpty(title));
    query.addBindValue(kindToDb(SourceKind::Local));
    query.addBindValue(textOrEmpty(filePath));
    query.addBindValue(textOrEmpty(filePath));
    query.addBindValue(textOrEmpty(note));
    query.addBindValue(probe.durationMs);
    query.addBindValue(size);
    query.addBindValue(textOrEmpty(probe.json));
    query.addBindValue(now);
    query.addBindValue(now);

    if (!query.exec()) {
        if (error) {
            *error = QStringLiteral("导入本地视频失败：%1").arg(query.lastError().text());
        }
        return -1;
    }
    return query.lastInsertId().toLongLong();
}

bool MediaRepository::finishRecording(qint64 sourceId, qint64 sessionId,
                                      SessionStatus sessionStatus, SourceStatus sourceStatus,
                                      qint64 durationMs, qint64 sizeBytes,
                                      const QString &errorMessage, qint64 finishedAtMs,
                                      QString *error)
{
    if (!m_db.isOpen()) {
        if (error) {
            *error = QStringLiteral("数据库尚未打开。");
        }
        return false;
    }
    if (!m_db.transaction()) {
        if (error) {
            *error = QStringLiteral("无法开始数据库事务：%1").arg(m_db.lastError().text());
        }
        return false;
    }

    QSqlQuery sessionQuery(m_db);
    sessionQuery.prepare(QStringLiteral(
        "UPDATE recording_sessions"
        " SET status = ?, finished_at_ms = ?, duration_ms = ?, size_bytes = ?,"
        "     error_message = ?"
        " WHERE id = ? AND source_id = ? AND status = 'recording'"));
    sessionQuery.addBindValue(sessionStatusToDb(sessionStatus));
    sessionQuery.addBindValue(finishedAtMs);
    sessionQuery.addBindValue(durationMs);
    sessionQuery.addBindValue(sizeBytes);
    sessionQuery.addBindValue(textOrEmpty(errorMessage));
    sessionQuery.addBindValue(sessionId);
    sessionQuery.addBindValue(sourceId);

    if (!sessionQuery.exec()) {
        if (error) {
            *error = QStringLiteral("更新录制会话失败：%1")
                         .arg(sessionQuery.lastError().text());
        }
        m_db.rollback();
        return false;
    }

    QSqlQuery sourceQuery(m_db);
    sourceQuery.prepare(QStringLiteral(
        "UPDATE media_sources"
        " SET status = ?, duration_ms = ?, size_bytes = ?, error_message = ?,"
        "     updated_at_ms = ?"
        " WHERE id = ?"));
    sourceQuery.addBindValue(sourceStatusToDb(sourceStatus));
    sourceQuery.addBindValue(durationMs);
    sourceQuery.addBindValue(sizeBytes);
    sourceQuery.addBindValue(textOrEmpty(errorMessage));
    sourceQuery.addBindValue(finishedAtMs);
    sourceQuery.addBindValue(sourceId);

    if (!sourceQuery.exec()) {
        if (error) {
            *error = QStringLiteral("更新源视频失败：%1")
                         .arg(sourceQuery.lastError().text());
        }
        m_db.rollback();
        return false;
    }
    if (sourceQuery.numRowsAffected() <= 0) {
        if (error) {
            *error = QStringLiteral("未找到要更新的源视频。");
        }
        m_db.rollback();
        return false;
    }

    if (!m_db.commit()) {
        if (error) {
            *error = QStringLiteral("提交录制结果失败：%1").arg(m_db.lastError().text());
        }
        m_db.rollback();
        return false;
    }
    return true;
}

bool MediaRepository::removeSource(qint64 sourceId, QString *error)
{
    if (!m_db.isOpen()) {
        if (error) {
            *error = QStringLiteral("数据库尚未打开。");
        }
        return false;
    }

    QSqlQuery statusQuery(m_db);
    statusQuery.prepare(QStringLiteral("SELECT status FROM media_sources WHERE id = ?"));
    statusQuery.addBindValue(sourceId);
    if (!statusQuery.exec() || !statusQuery.next()) {
        if (error) {
            *error = QStringLiteral("未找到源视频。");
        }
        return false;
    }
    if (statusQuery.value(0).toString() == QLatin1String("recording")) {
        if (error) {
            *error = QStringLiteral("录制进行中，请先停止录制。");
        }
        return false;
    }

    QSqlQuery deleteQuery(m_db);
    deleteQuery.prepare(QStringLiteral("DELETE FROM media_sources WHERE id = ?"));
    deleteQuery.addBindValue(sourceId);
    if (!deleteQuery.exec()) {
        if (error) {
            *error = QStringLiteral("删除源视频失败：%1").arg(deleteQuery.lastError().text());
        }
        return false;
    }
    if (deleteQuery.numRowsAffected() <= 0) {
        if (error) {
            *error = QStringLiteral("未找到源视频。");
        }
        return false;
    }
    return true;
}

MediaSource MediaRepository::source(qint64 sourceId, QString *error) const
{
    if (!m_db.isOpen()) {
        if (error) {
            *error = QStringLiteral("数据库尚未打开。");
        }
        return MediaSource();
    }

    QSqlQuery query(m_db);
    query.prepare(QStringLiteral(
        "SELECT id, title, source_kind, source_uri, local_path, note, status,"
        " duration_ms, size_bytes, probe_json, error_message,"
        " created_at_ms, updated_at_ms,"
        " (SELECT COUNT(*) FROM clip_drafts"
        "  WHERE clip_drafts.source_id = media_sources.id) AS draft_count"
        " FROM media_sources WHERE id = ?"));
    query.addBindValue(sourceId);
    if (!query.exec()) {
        if (error) {
            *error = QStringLiteral("读取源视频失败：%1").arg(query.lastError().text());
        }
        return MediaSource();
    }
    if (!query.next()) {
        if (error) {
            *error = QStringLiteral("未找到源视频。");
        }
        return MediaSource();
    }
    return sourceFromQuery(query);
}

bool MediaRepository::loadCachedAsr(qint64 sourceId, const QString &fingerprint,
                                    AsrTranscript *transcript, bool *found,
                                    QString *error) const
{
    if (found) {
        *found = false;
    }
    if (!m_db.isOpen()) {
        if (error) {
            *error = QStringLiteral("数据库尚未打开。");
        }
        return false;
    }

    QSqlQuery query(m_db);
    query.prepare(QStringLiteral(
        "SELECT fingerprint, data_json FROM asr_results WHERE source_id = ?"));
    query.addBindValue(sourceId);
    if (!query.exec()) {
        if (error) {
            *error = QStringLiteral("读取 ASR 缓存失败：%1").arg(query.lastError().text());
        }
        return false;
    }
    if (!query.next()) {
        return true;
    }
    // 源文件发生变化（重新录制、替换文件）后缓存失效。
    if (query.value(0).toString() != fingerprint) {
        return true;
    }

    const QJsonObject data =
        QJsonDocument::fromJson(query.value(1).toString().toUtf8()).object();
    if (data.isEmpty()) {
        return true;
    }
    if (transcript) {
        *transcript = AsrTranscript::fromJson(data, nullptr);
        if (transcript->isEmpty()) {
            return true;
        }
    }
    if (found) {
        *found = true;
    }
    return true;
}

bool MediaRepository::saveAsrResult(qint64 sourceId, const QString &fingerprint,
                                    const AsrTranscript &transcript, QString *error)
{
    if (!m_db.isOpen()) {
        if (error) {
            *error = QStringLiteral("数据库尚未打开。");
        }
        return false;
    }

    QSqlQuery query(m_db);
    query.prepare(QStringLiteral(
        "INSERT OR REPLACE INTO asr_results"
        " (source_id, fingerprint, data_json, created_at_ms)"
        " VALUES (?, ?, ?, ?)"));
    query.addBindValue(sourceId);
    query.addBindValue(fingerprint);
    query.addBindValue(
        QString::fromUtf8(QJsonDocument(transcript.toJson()).toJson(QJsonDocument::Compact)));
    query.addBindValue(QDateTime::currentMSecsSinceEpoch());
    if (!query.exec()) {
        if (error) {
            *error = QStringLiteral("保存 ASR 缓存失败：%1").arg(query.lastError().text());
        }
        return false;
    }
    return true;
}

qint64 MediaRepository::addClipDraft(const ClipDraft &draft, QString *error)
{
    if (!m_db.isOpen()) {
        if (error) {
            *error = QStringLiteral("数据库尚未打开。");
        }
        return -1;
    }

    QSqlQuery query(m_db);
    query.prepare(QStringLiteral(
        "INSERT INTO clip_drafts"
        " (source_id, draft_id, draft_url, title, description, tags_json,"
        "  source_start_ms, source_end_ms, duration_ms, created_at_ms, downloaded_path)"
        " VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"));
    query.addBindValue(draft.sourceId);
    query.addBindValue(textOrEmpty(draft.draftId));
    query.addBindValue(textOrEmpty(draft.draftUrl));
    query.addBindValue(textOrEmpty(draft.title));
    query.addBindValue(textOrEmpty(draft.description));
    query.addBindValue(tagsToJson(draft.tags));
    query.addBindValue(draft.sourceStartMs);
    query.addBindValue(draft.sourceEndMs);
    query.addBindValue(qMax<qint64>(0, draft.durationMs));
    query.addBindValue(draft.createdAtMs > 0 ? draft.createdAtMs
                                             : QDateTime::currentMSecsSinceEpoch());
    query.addBindValue(textOrEmpty(draft.downloadedPath));
    if (!query.exec()) {
        if (error) {
            *error = QStringLiteral("保存草稿记录失败：%1").arg(query.lastError().text());
        }
        return -1;
    }
    return query.lastInsertId().toLongLong();
}

QVector<ClipDraft> MediaRepository::clipDrafts(qint64 sourceId, QString *error) const
{
    QVector<ClipDraft> result;
    if (!m_db.isOpen()) {
        if (error) {
            *error = QStringLiteral("数据库尚未打开。");
        }
        return result;
    }

    QSqlQuery query(m_db);
    query.setForwardOnly(true);
    QString sql = QStringLiteral(
        "SELECT id, source_id, draft_id, draft_url, title, description, tags_json,"
        " source_start_ms, source_end_ms, duration_ms, created_at_ms, downloaded_path"
        " FROM clip_drafts");
    if (sourceId >= 0) {
        sql += QStringLiteral(" WHERE source_id = ?");
    }
    sql += QStringLiteral(" ORDER BY created_at_ms ASC, id ASC");
    query.prepare(sql);
    if (sourceId >= 0) {
        query.addBindValue(sourceId);
    }
    if (!query.exec()) {
        if (error) {
            *error = QStringLiteral("读取草稿列表失败：%1").arg(query.lastError().text());
        }
        return result;
    }
    while (query.next()) {
        result.append(draftFromQuery(query));
    }
    return result;
}

ClipDraft MediaRepository::clipDraft(qint64 id, QString *error) const
{
    if (!m_db.isOpen()) {
        if (error) {
            *error = QStringLiteral("数据库尚未打开。");
        }
        return ClipDraft();
    }

    QSqlQuery query(m_db);
    query.prepare(QStringLiteral(
        "SELECT id, source_id, draft_id, draft_url, title, description, tags_json,"
        " source_start_ms, source_end_ms, duration_ms, created_at_ms, downloaded_path"
        " FROM clip_drafts WHERE id = ?"));
    query.addBindValue(id);
    if (!query.exec()) {
        if (error) {
            *error = QStringLiteral("读取草稿失败：%1").arg(query.lastError().text());
        }
        return ClipDraft();
    }
    if (!query.next()) {
        if (error) {
            *error = QStringLiteral("未找到草稿记录。");
        }
        return ClipDraft();
    }
    return draftFromQuery(query);
}

bool MediaRepository::markDraftDownloaded(qint64 draftId, const QString &localPath,
                                          QString *error)
{
    if (!m_db.isOpen()) {
        if (error) {
            *error = QStringLiteral("数据库尚未打开。");
        }
        return false;
    }

    QSqlQuery query(m_db);
    query.prepare(QStringLiteral(
        "UPDATE clip_drafts SET downloaded_path = ? WHERE id = ?"));
    query.addBindValue(textOrEmpty(localPath));
    query.addBindValue(draftId);
    if (!query.exec()) {
        if (error) {
            *error = QStringLiteral("更新草稿下载路径失败：%1")
                         .arg(query.lastError().text());
        }
        return false;
    }
    return query.numRowsAffected() > 0;
}

qint64 MediaRepository::createClipTask(qint64 sourceId, const QString &title,
                                       QString *error)
{
    if (!m_db.isOpen()) {
        if (error) {
            *error = QStringLiteral("数据库尚未打开。");
        }
        return -1;
    }

    const qint64 now = QDateTime::currentMSecsSinceEpoch();
    QSqlQuery query(m_db);
    query.prepare(QStringLiteral(
        "INSERT INTO clip_tasks"
        " (source_id, title, status, step, progress, message, draft_count,"
        "  created_at_ms, updated_at_ms, finished_at_ms)"
        " VALUES (?, ?, 'running', 0, 0, ?, 0, ?, ?, 0)"));
    query.addBindValue(sourceId);
    query.addBindValue(textOrEmpty(title));
    query.addBindValue(QStringLiteral("正在准备任务…"));
    query.addBindValue(now);
    query.addBindValue(now);
    if (!query.exec()) {
        if (error) {
            *error = QStringLiteral("创建任务记录失败：%1").arg(query.lastError().text());
        }
        return -1;
    }
    return query.lastInsertId().toLongLong();
}

bool MediaRepository::updateClipTask(qint64 taskId, int step, int progress,
                                     const QString &message, QString *error)
{
    if (!m_db.isOpen()) {
        if (error) {
            *error = QStringLiteral("数据库尚未打开。");
        }
        return false;
    }

    QSqlQuery query(m_db);
    query.prepare(QStringLiteral(
        "UPDATE clip_tasks"
        " SET step = ?, progress = ?, message = ?, updated_at_ms = ?"
        " WHERE id = ? AND status = 'running'"));
    query.addBindValue(qBound(0, step, 2));
    query.addBindValue(qBound(0, progress, 100));
    query.addBindValue(textOrEmpty(message));
    query.addBindValue(QDateTime::currentMSecsSinceEpoch());
    query.addBindValue(taskId);
    if (!query.exec()) {
        if (error) {
            *error = QStringLiteral("更新任务进度失败：%1").arg(query.lastError().text());
        }
        return false;
    }
    return true;
}

bool MediaRepository::incrementClipTaskDrafts(qint64 taskId, QString *error)
{
    if (!m_db.isOpen()) {
        if (error) {
            *error = QStringLiteral("数据库尚未打开。");
        }
        return false;
    }

    QSqlQuery query(m_db);
    query.prepare(QStringLiteral(
        "UPDATE clip_tasks"
        " SET draft_count = draft_count + 1, updated_at_ms = ?"
        " WHERE id = ? AND status = 'running'"));
    query.addBindValue(QDateTime::currentMSecsSinceEpoch());
    query.addBindValue(taskId);
    if (!query.exec()) {
        if (error) {
            *error = QStringLiteral("更新任务草稿数失败：%1").arg(query.lastError().text());
        }
        return false;
    }
    return true;
}

bool MediaRepository::finishClipTask(qint64 taskId, ClipTaskStatus status,
                                     const QString &message, QString *error)
{
    if (!m_db.isOpen()) {
        if (error) {
            *error = QStringLiteral("数据库尚未打开。");
        }
        return false;
    }

    const qint64 now = QDateTime::currentMSecsSinceEpoch();
    QSqlQuery query(m_db);
    if (status == ClipTaskStatus::Completed) {
        query.prepare(QStringLiteral(
            "UPDATE clip_tasks"
            " SET status = 'completed', progress = 100, step = 2, message = ?,"
            "     finished_at_ms = ?, updated_at_ms = ?"
            " WHERE id = ? AND status = 'running'"));
    } else {
        query.prepare(QStringLiteral(
            "UPDATE clip_tasks"
            " SET status = 'failed', message = ?, finished_at_ms = ?, updated_at_ms = ?"
            " WHERE id = ? AND status = 'running'"));
    }
    query.addBindValue(textOrEmpty(message));
    query.addBindValue(now);
    query.addBindValue(now);
    query.addBindValue(taskId);
    if (!query.exec()) {
        if (error) {
            *error = QStringLiteral("结束任务失败：%1").arg(query.lastError().text());
        }
        return false;
    }
    return true;
}

QVector<ClipTask> MediaRepository::clipTasks(QString *error) const
{
    QVector<ClipTask> result;
    if (!m_db.isOpen()) {
        if (error) {
            *error = QStringLiteral("数据库尚未打开。");
        }
        return result;
    }

    QSqlQuery query(m_db);
    query.setForwardOnly(true);
    if (!query.exec(QStringLiteral(
            "SELECT id, source_id, title, status, step, progress, message,"
            " draft_count, created_at_ms, updated_at_ms, finished_at_ms"
            " FROM clip_tasks"
            " ORDER BY CASE status WHEN 'running' THEN 0 WHEN 'failed' THEN 1 ELSE 2 END,"
            " created_at_ms DESC"))) {
        if (error) {
            *error = QStringLiteral("读取任务列表失败：%1").arg(query.lastError().text());
        }
        return result;
    }
    while (query.next()) {
        result.append(taskFromQuery(query));
    }
    return result;
}

ClipTask MediaRepository::clipTask(qint64 id, QString *error) const
{
    if (!m_db.isOpen()) {
        if (error) {
            *error = QStringLiteral("数据库尚未打开。");
        }
        return ClipTask();
    }

    QSqlQuery query(m_db);
    query.prepare(QStringLiteral(
        "SELECT id, source_id, title, status, step, progress, message,"
        " draft_count, created_at_ms, updated_at_ms, finished_at_ms"
        " FROM clip_tasks WHERE id = ?"));
    query.addBindValue(id);
    if (!query.exec()) {
        if (error) {
            *error = QStringLiteral("读取任务失败：%1").arg(query.lastError().text());
        }
        return ClipTask();
    }
    if (!query.next()) {
        if (error) {
            *error = QStringLiteral("未找到任务记录。");
        }
        return ClipTask();
    }
    return taskFromQuery(query);
}

bool MediaRepository::recoverInterruptedClipTasks(QString *error)
{
    if (!m_db.isOpen()) {
        if (error) {
            *error = QStringLiteral("数据库尚未打开。");
        }
        return false;
    }

    const qint64 now = QDateTime::currentMSecsSinceEpoch();
    QSqlQuery query(m_db);
    query.prepare(QStringLiteral(
        "UPDATE clip_tasks"
        " SET status = 'failed', message = ?, finished_at_ms = ?, updated_at_ms = ?"
        " WHERE status = 'running'"));
    query.addBindValue(QStringLiteral("应用退出时任务未完成。"));
    query.addBindValue(now);
    query.addBindValue(now);
    if (!query.exec()) {
        if (error) {
            *error = QStringLiteral("恢复中断任务失败：%1").arg(query.lastError().text());
        }
        return false;
    }
    return true;
}

bool MediaRepository::recoverInterrupted(QString *error)
{
    if (!m_db.isOpen()) {
        if (error) {
            *error = QStringLiteral("数据库尚未打开。");
        }
        return false;
    }

    struct PendingSession
    {
        qint64 id = -1;
        qint64 sourceId = -1;
        QString outputPath;
        QString container;
    };

    QVector<PendingSession> pending;
    QSqlQuery query(m_db);
    query.setForwardOnly(true);
    if (!query.exec(QStringLiteral(
            "SELECT id, source_id, output_path, container FROM recording_sessions"
            " WHERE status = 'recording'"))) {
        if (error) {
            *error = QStringLiteral("读取未完成录制失败：%1").arg(query.lastError().text());
        }
        return false;
    }
    while (query.next()) {
        pending.append({query.value(0).toLongLong(), query.value(1).toLongLong(),
                        query.value(2).toString(), query.value(3).toString()});
    }
    if (pending.isEmpty()) {
        return true;
    }

    if (!m_db.transaction()) {
        if (error) {
            *error = QStringLiteral("无法开始数据库事务：%1").arg(m_db.lastError().text());
        }
        return false;
    }

    const qint64 now = QDateTime::currentMSecsSinceEpoch();
    for (const PendingSession &item : pending) {
        const bool isFileSession = item.container == QLatin1String("mp4");
        const QFileInfo outputInfo(item.outputPath);
        const bool validOutput = outputInfo.exists() && outputInfo.size() > 0;
        if (isFileSession && !validOutput) {
            // 下载中断只会留下临时文件；正式路径不存在时清理掉，避免残留大文件。
            QFile::remove(item.outputPath + QStringLiteral(".part"));
            QFile::remove(item.outputPath + QStringLiteral(".normalizing.mp4"));
        }
        const qint64 size = validOutput
            ? (isFileSession ? outputInfo.size()
                             : MediaUtils::directorySize(outputInfo.absolutePath()))
            : 0;
        // mp4 下载只在完整落盘后才改名为正式路径，因此能读到文件即可用；
        // 时长交给后续 ffprobe/ASR 重新计算，这里不做阻塞探测。
        const qint64 durationMs =
            validOutput && !isFileSession
            ? MediaUtils::hlsPlaylistDurationMs(item.outputPath)
            : 0;
        const SourceStatus sourceStatus =
            validOutput ? SourceStatus::Ready : SourceStatus::Failed;
        const QString warning = validOutput
            ? (isFileSession ? QStringLiteral("上次下载意外中断，文件可能不完整。")
                             : QStringLiteral("上次录制意外中断，文件可能不完整。"))
            : (isFileSession ? QStringLiteral("上次下载意外中断且未生成有效文件。")
                             : QStringLiteral("上次录制意外中断且未生成有效文件。"));

        QSqlQuery sessionQuery(m_db);
        sessionQuery.prepare(QStringLiteral(
            "UPDATE recording_sessions"
            " SET status = 'failed', finished_at_ms = ?, duration_ms = ?, size_bytes = ?,"
            "     error_message = ?"
            " WHERE id = ?"));
        sessionQuery.addBindValue(now);
        sessionQuery.addBindValue(durationMs);
        sessionQuery.addBindValue(size);
        sessionQuery.addBindValue(QStringLiteral("应用退出时录制未正常结束。"));
        sessionQuery.addBindValue(item.id);
        if (!sessionQuery.exec()) {
            if (error) {
                *error = QStringLiteral("恢复录制会话失败：%1")
                             .arg(sessionQuery.lastError().text());
            }
            m_db.rollback();
            return false;
        }

        QSqlQuery sourceQuery(m_db);
        sourceQuery.prepare(QStringLiteral(
            "UPDATE media_sources"
            " SET status = ?, duration_ms = ?, size_bytes = ?, error_message = ?,"
            "     updated_at_ms = ?"
            " WHERE id = ?"));
        sourceQuery.addBindValue(sourceStatusToDb(sourceStatus));
        sourceQuery.addBindValue(durationMs);
        sourceQuery.addBindValue(size);
        sourceQuery.addBindValue(warning);
        sourceQuery.addBindValue(now);
        sourceQuery.addBindValue(item.sourceId);
        if (!sourceQuery.exec()) {
            if (error) {
                *error = QStringLiteral("恢复源视频失败：%1")
                             .arg(sourceQuery.lastError().text());
            }
            m_db.rollback();
            return false;
        }
    }

    if (!m_db.commit()) {
        if (error) {
            *error = QStringLiteral("提交恢复结果失败：%1").arg(m_db.lastError().text());
        }
        m_db.rollback();
        return false;
    }
    return true;
}
