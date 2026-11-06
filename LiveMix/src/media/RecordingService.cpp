#include "RecordingService.h"

#include "../core/AppConfig.h"
#include "../core/MediaUtils.h"
#include "../data/MediaRepository.h"
#include "FfmpegLocator.h"
#include "MediaProbe.h"

#include <QDateTime>
#include <QDebug>
#include <QDir>
#include <QElapsedTimer>
#include <QFile>
#include <QFileInfo>
#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <QNetworkAccessManager>
#include <QNetworkReply>
#include <QNetworkRequest>
#include <QProcess>
#include <QTimer>
#include <QUrl>

#ifdef Q_OS_WIN
#ifndef NOMINMAX
#define NOMINMAX
#endif
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#include <windows.h>
#endif

namespace {

constexpr qint64 kStderrTailLimit = 16 * 1024;
constexpr int kProbeTimeoutMs = 8000;
constexpr int kDownloadIdleTimeoutMs = 60000;
constexpr int kProbeSniffBytes = 8192;
constexpr int kProgressIntervalMs = 100;

enum class OnlineLinkKind {
    DirectFile, // 渐进式 mp4/mov，直接下载原文件
    Stream,     // HLS/直播/未知，交给 ffmpeg
};

QString lastErrorLine(const QString &text)
{
    const QStringList lines = text.split(QLatin1Char('\n'));
    for (int i = lines.size() - 1; i >= 0; --i) {
        const QString line = lines[i].trimmed();
        if (line.isEmpty()) {
            continue;
        }
        return line.size() > 400 ? line.left(400) + QStringLiteral("…") : line;
    }
    return QString();
}

bool urlLooksLikeMp4File(const QString &uri)
{
    const QString suffix = QFileInfo(QUrl(uri).path()).suffix().toLower();
    return suffix == QLatin1String("mp4") || suffix == QLatin1String("m4v")
        || suffix == QLatin1String("mov");
}

bool looksLikeHlsPlaylist(const QByteArray &head, const QString &contentType)
{
    const QString type = contentType.toLower();
    if (type.contains(QLatin1String("mpegurl")) || type.contains(QLatin1String("m3u8"))) {
        return true;
    }

    QByteArray body = head;
    if (body.startsWith("\xEF\xBB\xBF")) {
        body.remove(0, 3);
    }
    return body.trimmed().startsWith("#EXTM3U");
}

OnlineLinkKind classifyOnlineLink(const QByteArray &head, const QString &contentType)
{
    const QString type = contentType.toLower();
    if (looksLikeHlsPlaylist(head, contentType)) {
        return OnlineLinkKind::Stream;
    }

    if (type.startsWith(QLatin1String("video/mp4"))
        || type.startsWith(QLatin1String("video/quicktime"))
        || type.startsWith(QLatin1String("application/mp4"))
        || type.startsWith(QLatin1String("video/x-m4v"))) {
        return OnlineLinkKind::DirectFile;
    }

    // 部分对象存储返回 application/octet-stream，按 MP4 box 头兜底识别。
    if (head.size() >= 12 && head.mid(4, 4) == QByteArrayLiteral("ftyp")) {
        return OnlineLinkKind::DirectFile;
    }
    return OnlineLinkKind::Stream;
}

QString downloadFileName(const QString &uri)
{
    const QFileInfo urlInfo(QUrl(uri).path());
    QString base = urlInfo.completeBaseName();
    QString suffix = urlInfo.suffix().toLower();
    if (base.isEmpty()) {
        base = QStringLiteral("source");
    }
    if (suffix != QLatin1String("mp4") && suffix != QLatin1String("m4v")
        && suffix != QLatin1String("mov")) {
        suffix = QStringLiteral("mp4");
    }
    return MediaUtils::sanitizeFileComponent(base, 80) + QLatin1Char('.') + suffix;
}

bool prepareOutputDirectory(const QString &rootPath, const QString &title,
                            QString *outputDir, QString *error)
{
    if (!QDir().mkpath(rootPath)) {
        if (error) {
            *error = QStringLiteral("无法创建录制目录：%1").arg(rootPath);
        }
        return false;
    }

    const QString stamp =
        QDateTime::currentDateTime().toString(QStringLiteral("yyyyMMdd-HHmmss"));
    const QString folderBase =
        stamp + QLatin1Char('-') + MediaUtils::sanitizeFileComponent(title);
    QString path = QDir(rootPath).filePath(folderBase);
    for (int suffix = 2; QFileInfo::exists(path) && suffix < 1000; ++suffix) {
        path = QDir(rootPath).filePath(folderBase + QLatin1Char('-')
                                       + QString::number(suffix));
    }
    if (!QDir().mkpath(path)) {
        if (error) {
            *error = QStringLiteral("无法创建录制目录：%1").arg(path);
        }
        return false;
    }
    if (outputDir) {
        *outputDir = path;
    }
    return true;
}

struct AudioProbeInfo
{
    bool hasAudio = false;
    QString codec;
};

AudioProbeInfo audioInfoFromProbe(const ProbeInfo &probe)
{
    AudioProbeInfo info;
    QJsonParseError parseError;
    const QJsonDocument document =
        QJsonDocument::fromJson(probe.json.toUtf8(), &parseError);
    if (parseError.error != QJsonParseError::NoError || !document.isObject()) {
        return info;
    }

    const QJsonArray streams =
        document.object().value(QStringLiteral("streams")).toArray();
    for (const QJsonValue &value : streams) {
        const QJsonObject stream = value.toObject();
        if (stream.value(QStringLiteral("codec_type")).toString()
            != QLatin1String("audio")) {
            continue;
        }
        info.hasAudio = true;
        info.codec =
            stream.value(QStringLiteral("codec_name")).toString().toLower();
        break;
    }
    return info;
}

QString downloadErrorText(int status, const QString &networkError)
{
    if (status == 403) {
        return QStringLiteral("服务器拒绝访问（HTTP 403，链接可能已失效）");
    }
    if (status == 404) {
        return QStringLiteral("文件不存在（HTTP 404）");
    }
    if (status >= 400) {
        return QStringLiteral("服务器返回 HTTP %1").arg(status);
    }

    QString text = networkError;
    // errorString 形如 "Error transferring <url> - <reason>"，去掉 URL。
    const int dash = text.lastIndexOf(QStringLiteral(" - "));
    if (dash >= 0) {
        text = text.mid(dash + 3);
    }
    return text.isEmpty() ? QStringLiteral("网络错误") : text;
}

} // namespace

struct RecordingService::Probe
{
    QString uri;
    QString title;
    QNetworkReply *reply = nullptr;
    QTimer *timer = nullptr;
    QByteArray head;
    bool decided = false;
};

struct RecordingService::Job
{
    enum class Mode {
        Stream,
        Download,
    };

    Mode mode = Mode::Stream;
    qint64 sourceId = -1;
    qint64 sessionId = -1;
    QString title;
    QString outputDir;
    QString outputPath;
    qint64 startedAtMs = 0;
    bool stopRequested = false;
    bool finalized = false;

    // ffmpeg HLS 录制。
    QProcess *process = nullptr;
    QFile *logFile = nullptr;
    QByteArray stdoutBuffer;
    QString stderrTail;
    qint64 firstOutTimeMs = -1;
    qint64 lastOutTimeMs = -1;
    qint64 lastDurationMs = 0;

    // HTTP 直链下载。
    QNetworkReply *reply = nullptr;
    QFile *downloadFile = nullptr;
    QString tempPath;
    QString normalizedTempPath;
    QTimer *idleTimer = nullptr;
    qint64 receivedBytes = 0;
    qint64 totalBytes = -1;
    qint64 lastProgressMs = 0;
    bool downloadTimedOut = false;
    QString writeError;
    QProcess *normalizeProcess = nullptr;
    ProbeInfo probe;
    QString warning;
};

RecordingService::RecordingService(ConfigStore *configStore, MediaRepository *repository,
                                   QObject *parent)
    : QObject(parent)
    , m_configStore(configStore)
    , m_repository(repository)
    , m_network(new QNetworkAccessManager(this))
{
}

RecordingService::~RecordingService()
{
    blockSignals(true);
    stopAllAndWait();
}

RecordingService::StartResult RecordingService::startOnlineRecording(
    const QString &uri, const QString &title)
{
    StartResult result;
    const QString sourceUri = uri.trimmed();
    if (sourceUri.isEmpty()) {
        result.error = QStringLiteral("视频地址不能为空。");
        return result;
    }
    if (!m_repository || !m_repository->isOpen()) {
        result.error = QStringLiteral("本地数据库不可用，无法开始录制。");
        return result;
    }
    if (!m_configStore) {
        result.error = QStringLiteral("配置未加载，无法开始录制。");
        return result;
    }

    const AppConfig config = m_configStore->config();
    QString ffmpeg = config.recording.ffmpegPath.trimmed();
    if (!ffmpeg.isEmpty()) {
        const QFileInfo info(ffmpeg);
        if (!info.isFile() || !info.isExecutable()) {
            result.error = QStringLiteral("配置中的 ffmpeg 路径无效：%1").arg(ffmpeg);
            return result;
        }
    } else {
        ffmpeg = FfmpegLocator::ffmpegPath();
    }
    if (ffmpeg.isEmpty()) {
        result.error = QStringLiteral(
            "未找到 ffmpeg。请重新安装应用，或设置 LIVEMIX_FFMPEG_DIR 指向包含 ffmpeg 的目录。");
        return result;
    }
    if (FfmpegLocator::ffprobePath().isEmpty()) {
        result.error = QStringLiteral(
            "未找到 ffprobe。请重新安装应用，或设置 LIVEMIX_FFMPEG_DIR 指向包含 ffprobe 的目录。");
        return result;
    }

    const QString scheme = QUrl(sourceUri).scheme().toLower();
    if (scheme == QLatin1String("http") || scheme == QLatin1String("https")) {
        // 先嗅探响应，再决定是直链下载还是 ffmpeg 录制。
        beginLinkProbe(sourceUri, title.trimmed());
        result.ok = true;
        return result;
    }
    if (scheme == QLatin1String("rtmp") || scheme == QLatin1String("rtmps")
        || scheme == QLatin1String("rtsp")) {
        return startStreamRecording(sourceUri, title);
    }

    result.error = QStringLiteral("仅支持 http / https / rtmp / rtsp 视频地址。");
    return result;
}

void RecordingService::beginLinkProbe(const QString &uri, const QString &title)
{
    auto *probe = new Probe;
    probe->uri = uri;
    probe->title = title;

    QNetworkRequest request{QUrl(uri)};
    request.setAttribute(QNetworkRequest::RedirectPolicyAttribute,
                         QNetworkRequest::NoLessSafeRedirectPolicy);
    request.setHeader(QNetworkRequest::UserAgentHeader,
                      QStringLiteral("LiveMixStudio/1.0"));
    request.setRawHeader("Accept", "*/*");
    request.setRawHeader("Range", "bytes=0-8191");
    probe->reply = m_network->get(request);
    m_probes.insert(probe);

    probe->timer = new QTimer(probe->reply);
    probe->timer->setSingleShot(true);
    connect(probe->timer, &QTimer::timeout, this, [this, probe] {
        finishLinkProbe(probe, true);
    });
    probe->timer->start(kProbeTimeoutMs);

    connect(probe->reply, &QNetworkReply::readyRead, this, [this, probe] {
        if (probe->decided) {
            return;
        }
        probe->head.append(probe->reply->readAll());
        if (probe->head.size() > kProbeSniffBytes) {
            probe->head.truncate(kProbeSniffBytes);
        }
        if (probe->head.size() >= kProbeSniffBytes) {
            finishLinkProbe(probe, false);
        }
    });
    connect(probe->reply, &QNetworkReply::finished, this, [this, probe] {
        finishLinkProbe(probe, false);
    });
}

void RecordingService::finishLinkProbe(Probe *probe, bool timedOut)
{
    if (!probe || probe->decided) {
        return;
    }
    probe->decided = true;

    QNetworkReply *reply = probe->reply;
    probe->reply = nullptr;
    QByteArray head = probe->head;
    QString contentType;
    int status = 0;
    bool networkError = false;
    if (reply) {
        head.append(reply->readAll());
        if (head.size() > kProbeSniffBytes) {
            head.truncate(kProbeSniffBytes);
        }
        contentType = QString::fromLatin1(reply->rawHeader("Content-Type"));
        status = reply->attribute(QNetworkRequest::HttpStatusCodeAttribute).toInt();
        networkError = reply->error() != QNetworkReply::NoError
            && reply->error() != QNetworkReply::OperationCanceledError;
        reply->disconnect(this);
        reply->abort();
        reply->deleteLater();
    }

    if (probe->timer) {
        probe->timer->stop();
        probe->timer->disconnect(this);
        probe->timer->deleteLater();
        probe->timer = nullptr;
    }

    const QString uri = probe->uri;
    const QString title = probe->title;
    m_probes.remove(probe);
    delete probe;

    OnlineLinkKind kind = OnlineLinkKind::Stream;
    const bool fallback = status >= 400 || timedOut || (networkError && head.size() < 12);
    if (fallback) {
        // 探测失败时按扩展名兜底；失败信息交给最终执行的任务报告。
        kind = urlLooksLikeMp4File(uri) ? OnlineLinkKind::DirectFile
                                        : OnlineLinkKind::Stream;
    } else {
        kind = classifyOnlineLink(head, contentType);
    }

    if (kind == OnlineLinkKind::DirectFile) {
        startDownloadJob(uri, title);
        return;
    }

    // 无扩展名或 MIME 不标准的 HLS 链接，ffmpeg 不会按内容自动识别，
    // 这里把探测结果作为显式 demuxer 传下去。
    const bool forceHls = !fallback && looksLikeHlsPlaylist(head, contentType);
    const StartResult result = startStreamRecording(uri, title, forceHls);
    if (!result.ok) {
        emit recordingFinished(-1, false, result.error);
    }
}

void RecordingService::abortPendingProbes()
{
    const QList<Probe *> probes = m_probes.values();
    for (Probe *probe : probes) {
        if (!probe) {
            continue;
        }
        probe->decided = true;
        m_probes.remove(probe);
        if (probe->timer) {
            probe->timer->stop();
            probe->timer->disconnect(this);
            probe->timer->deleteLater();
            probe->timer = nullptr;
        }
        if (probe->reply) {
            probe->reply->disconnect(this);
            probe->reply->abort();
            probe->reply->deleteLater();
            probe->reply = nullptr;
        }
        delete probe;
    }
}

RecordingService::StartResult RecordingService::startStreamRecording(
    const QString &sourceUri, const QString &title, bool forceHls)
{
    StartResult result;
    if (!m_repository || !m_repository->isOpen()) {
        result.error = QStringLiteral("本地数据库不可用，无法开始录制。");
        return result;
    }
    if (!m_configStore) {
        result.error = QStringLiteral("配置未加载，无法开始录制。");
        return result;
    }

    const AppConfig config = m_configStore->config();
    QString ffmpeg = config.recording.ffmpegPath.trimmed();
    if (!ffmpeg.isEmpty()) {
        const QFileInfo info(ffmpeg);
        if (!info.isFile() || !info.isExecutable()) {
            result.error = QStringLiteral("配置中的 ffmpeg 路径无效：%1").arg(ffmpeg);
            return result;
        }
        ffmpeg = info.absoluteFilePath();
    } else {
        ffmpeg = FfmpegLocator::ffmpegPath();
    }
    if (ffmpeg.isEmpty()) {
        result.error = QStringLiteral(
            "未找到 ffmpeg。请重新安装应用，或设置 LIVEMIX_FFMPEG_DIR 指向包含 ffmpeg 的目录。");
        return result;
    }

    const QString resolvedTitle = deriveTitle(sourceUri, title);
    QString outputDir;
    QString dirError;
    if (!prepareOutputDirectory(config.storage.recordingDir, resolvedTitle, &outputDir,
                                &dirError)) {
        result.error = dirError;
        return result;
    }

    const QString playlistPath = QDir(outputDir).filePath(QStringLiteral("index.m3u8"));
    const QString segmentPattern =
        QDir(outputDir).filePath(QStringLiteral("segment_%05d.ts"));
    const qint64 startedAtMs = QDateTime::currentMSecsSinceEpoch();

    QString dbError;
    const RecordingHandle handle = m_repository->createRecordingSource(
        resolvedTitle, sourceUri, playlistPath, startedAtMs, QStringLiteral("hls"),
        &dbError);
    if (handle.sourceId < 0 || handle.sessionId < 0) {
        QDir(outputDir).removeRecursively();
        result.error = dbError.isEmpty() ? QStringLiteral("无法创建录制记录。") : dbError;
        return result;
    }

    auto *job = new Job;
    job->mode = Job::Mode::Stream;
    job->sourceId = handle.sourceId;
    job->sessionId = handle.sessionId;
    job->title = resolvedTitle;
    job->outputDir = outputDir;
    job->outputPath = playlistPath;
    job->startedAtMs = startedAtMs;
    m_jobs.insert(job->sourceId, job);

    job->logFile = new QFile(QDir(outputDir).filePath(QStringLiteral("ffmpeg.log")));
    if (!job->logFile->open(QIODevice::WriteOnly | QIODevice::Append)) {
        qWarning() << "Unable to open ffmpeg log:" << job->logFile->errorString();
        delete job->logFile;
        job->logFile = nullptr;
    }

    QStringList args;
    args << QStringLiteral("-hide_banner")
         << QStringLiteral("-y")
         << QStringLiteral("-loglevel") << QStringLiteral("warning")
         << QStringLiteral("-progress") << QStringLiteral("pipe:1")
         << QStringLiteral("-nostats");

    if (config.recording.reconnect && isHttpUrl(sourceUri)) {
        args << QStringLiteral("-reconnect") << QStringLiteral("1")
             << QStringLiteral("-reconnect_streamed") << QStringLiteral("1")
             << QStringLiteral("-reconnect_delay_max")
             << QString::number(config.recording.reconnectDelayMaxSeconds);
    }

    if (forceHls) {
        args << QStringLiteral("-f") << QStringLiteral("hls");
    }
    args << QStringLiteral("-i") << sourceUri
         << QStringLiteral("-c") << QStringLiteral("copy")
         << QStringLiteral("-f") << QStringLiteral("hls")
         << QStringLiteral("-hls_time") << QString::number(config.recording.segmentSeconds)
         << QStringLiteral("-hls_playlist_type") << QStringLiteral("event")
         << QStringLiteral("-hls_segment_filename") << segmentPattern
         << playlistPath;

    if (job->logFile) {
        job->logFile->write(QStringLiteral("ffmpeg: %1\nargs: %2\n\n")
                                .arg(ffmpeg, args.join(QLatin1Char(' ')))
                                .toUtf8());
        job->logFile->flush();
    }

    auto *process = new QProcess(this);
    job->process = process;
    process->setProgram(ffmpeg);
    process->setArguments(args);
    process->setProcessChannelMode(QProcess::SeparateChannels);
#ifdef Q_OS_WIN
    process->setCreateProcessArgumentsModifier(
        [](QProcess::CreateProcessArguments *arguments) {
            arguments->flags |= CREATE_NO_WINDOW;
        });
#endif

    const qint64 sourceId = job->sourceId;
    connect(process, &QProcess::readyReadStandardOutput, this, [this, sourceId] {
        if (Job *current = findJob(sourceId)) {
            handleStandardOutput(current);
        }
    });
    connect(process, &QProcess::readyReadStandardError, this, [this, sourceId] {
        if (Job *current = findJob(sourceId)) {
            handleStandardError(current);
        }
    });
    connect(process, &QProcess::errorOccurred, this,
            [this, sourceId](QProcess::ProcessError processError) {
                if (processError != QProcess::FailedToStart) {
                    return;
                }
                Job *current = findJob(sourceId);
                if (!current) {
                    return;
                }
                const QString message = current->process
                    ? QStringLiteral("无法启动 ffmpeg：%1").arg(current->process->errorString())
                    : QStringLiteral("无法启动 ffmpeg。");
                finalize(current, false, message);
            });
    connect(process, QOverload<int, QProcess::ExitStatus>::of(&QProcess::finished), this,
            [this, sourceId](int exitCode, QProcess::ExitStatus exitStatus) {
                if (Job *current = findJob(sourceId)) {
                    handleFinished(current, exitCode, exitStatus);
                }
            });

    emit sourceAdded(handle.sourceId);
    process->start();

    result.ok = true;
    result.sourceId = handle.sourceId;
    result.sessionId = handle.sessionId;
    return result;
}

void RecordingService::startDownloadJob(const QString &sourceUri, const QString &title)
{
    if (!m_repository || !m_repository->isOpen() || !m_configStore) {
        emit recordingFinished(-1, false, QStringLiteral("本地数据库或配置不可用，无法下载。"));
        return;
    }

    const AppConfig config = m_configStore->config();
    const QString resolvedTitle = deriveTitle(sourceUri, title);
    QString outputDir;
    QString dirError;
    if (!prepareOutputDirectory(config.storage.recordingDir, resolvedTitle, &outputDir,
                                &dirError)) {
        emit recordingFinished(-1, false, dirError);
        return;
    }

    const QString finalPath = QDir(outputDir).filePath(downloadFileName(sourceUri));
    const QString tempPath = finalPath + QStringLiteral(".part");
    const qint64 startedAtMs = QDateTime::currentMSecsSinceEpoch();

    QString dbError;
    const RecordingHandle handle = m_repository->createRecordingSource(
        resolvedTitle, sourceUri, finalPath, startedAtMs, QStringLiteral("mp4"),
        &dbError);
    if (handle.sourceId < 0 || handle.sessionId < 0) {
        QDir(outputDir).removeRecursively();
        emit recordingFinished(-1, false,
                               dbError.isEmpty() ? QStringLiteral("无法创建下载记录。")
                                                 : dbError);
        return;
    }

    auto *job = new Job;
    job->mode = Job::Mode::Download;
    job->sourceId = handle.sourceId;
    job->sessionId = handle.sessionId;
    job->title = resolvedTitle;
    job->outputDir = outputDir;
    job->outputPath = finalPath;
    job->tempPath = tempPath;
    job->startedAtMs = startedAtMs;
    m_jobs.insert(job->sourceId, job);

    emit sourceAdded(job->sourceId);

    job->downloadFile = new QFile(tempPath);
    if (!job->downloadFile->open(QIODevice::WriteOnly | QIODevice::Truncate)) {
        finalize(job, false,
                 QStringLiteral("下载失败：无法写入文件（%1）")
                     .arg(job->downloadFile->errorString()));
        return;
    }

    QNetworkRequest request{QUrl(sourceUri)};
    request.setAttribute(QNetworkRequest::RedirectPolicyAttribute,
                         QNetworkRequest::NoLessSafeRedirectPolicy);
    request.setHeader(QNetworkRequest::UserAgentHeader,
                      QStringLiteral("LiveMixStudio/1.0"));
    request.setRawHeader("Accept", "*/*");
    job->reply = m_network->get(request);

    const qint64 sourceId = job->sourceId;
    job->idleTimer = new QTimer(this);
    job->idleTimer->setSingleShot(true);
    connect(job->idleTimer, &QTimer::timeout, this, [this, sourceId] {
        Job *current = findJob(sourceId);
        if (!current) {
            return;
        }
        current->downloadTimedOut = true;
        if (current->reply) {
            current->reply->abort();
        }
    });
    job->idleTimer->start(kDownloadIdleTimeoutMs);

    connect(job->reply, &QNetworkReply::readyRead, this, [this, sourceId] {
        if (Job *current = findJob(sourceId)) {
            handleDownloadReadyRead(current);
        }
    });
    connect(job->reply, &QNetworkReply::downloadProgress, this,
            [this, sourceId](qint64, qint64 total) {
                if (Job *current = findJob(sourceId)) {
                    current->totalBytes = total;
                }
            });
    connect(job->reply, &QNetworkReply::finished, this, [this, sourceId] {
        if (Job *current = findJob(sourceId)) {
            handleDownloadFinished(current);
        }
    });
}

void RecordingService::handleDownloadReadyRead(Job *job)
{
    if (!job || !job->reply || !job->downloadFile) {
        return;
    }
    if (job->idleTimer) {
        job->idleTimer->start(kDownloadIdleTimeoutMs);
    }

    const QByteArray chunk = job->reply->readAll();
    if (chunk.isEmpty()) {
        return;
    }
    if (job->downloadFile->write(chunk) != chunk.size()) {
        job->writeError = QStringLiteral("写入下载文件失败：%1")
                              .arg(job->downloadFile->errorString());
        job->reply->abort();
        return;
    }
    job->receivedBytes += chunk.size();

    const qint64 now = QDateTime::currentMSecsSinceEpoch();
    if (now - job->lastProgressMs >= kProgressIntervalMs) {
        job->lastProgressMs = now;
        emit downloadProgress(job->sourceId, job->receivedBytes, job->totalBytes);
    }
}

void RecordingService::handleDownloadFinished(Job *job)
{
    if (!job || job->finalized) {
        return;
    }
    if (job->idleTimer) {
        job->idleTimer->stop();
    }

    QNetworkReply *reply = job->reply;
    job->reply = nullptr;
    int status = 0;
    bool networkOk = false;
    QString networkError;
    if (reply) {
        status = reply->attribute(QNetworkRequest::HttpStatusCodeAttribute).toInt();
        networkOk = reply->error() == QNetworkReply::NoError;
        networkError = reply->errorString();
        reply->disconnect(this);
        reply->deleteLater();
    }
    if (job->downloadFile) {
        job->downloadFile->flush();
        job->downloadFile->close();
    }

    emit downloadProgress(job->sourceId, job->receivedBytes, job->totalBytes);

    if (job->stopRequested) {
        finalize(job, false, QStringLiteral("下载已取消"));
        return;
    }
    if (job->downloadTimedOut) {
        finalize(job, false, QStringLiteral("下载失败：连接超时，请检查网络或链接。"));
        return;
    }
    if (!job->writeError.isEmpty()) {
        finalize(job, false, job->writeError);
        return;
    }
    if (!networkOk || status < 200 || status >= 300) {
        finalize(job, false,
                 QStringLiteral("下载失败：%1").arg(downloadErrorText(status, networkError)));
        return;
    }

    const QFileInfo fileInfo(job->tempPath);
    if (!fileInfo.exists() || fileInfo.size() <= 0) {
        finalize(job, false, QStringLiteral("下载失败：未收到有效文件内容。"));
        return;
    }
    if (job->totalBytes > 0 && fileInfo.size() != job->totalBytes) {
        finalize(job, false,
                 QStringLiteral("下载失败：文件不完整（%1 / %2 字节）。")
                     .arg(fileInfo.size())
                     .arg(job->totalBytes));
        return;
    }

    const ProbeInfo probe = MediaProbe::probe(job->tempPath);
    if (!probe.ok) {
        finalize(job, false,
                 QStringLiteral("下载完成但无法识别视频：%1").arg(probe.error));
        return;
    }
    job->probe = probe;

    const AudioProbeInfo audio = audioInfoFromProbe(probe);
    if (!audio.hasAudio) {
        finishDownloadFile(
            job, false,
            QStringLiteral("下载完成（源文件没有音频轨）"),
            QStringLiteral("源文件没有音频轨，无法进行语音转写。"));
        return;
    }
    if (audio.codec != QLatin1String("aac")) {
        beginAudioNormalization(job);
        return;
    }
    finishDownloadFile(job, false, QStringLiteral("下载完成"));
}

void RecordingService::beginAudioNormalization(Job *job)
{
    if (!job || job->finalized) {
        return;
    }

    const AppConfig config = m_configStore->config();
    QString ffmpeg = config.recording.ffmpegPath.trimmed();
    if (!ffmpeg.isEmpty()) {
        const QFileInfo info(ffmpeg);
        if (!info.isFile() || !info.isExecutable()) {
            ffmpeg.clear();
        } else {
            ffmpeg = info.absoluteFilePath();
        }
    }
    if (ffmpeg.isEmpty()) {
        ffmpeg = FfmpegLocator::ffmpegPath();
    }
    if (ffmpeg.isEmpty()) {
        finishDownloadFile(
            job, false, QStringLiteral("下载完成（音频格式转换失败）"),
            QStringLiteral("音频格式转换失败，部分播放器可能无声。"));
        return;
    }

    job->normalizedTempPath = job->outputPath + QStringLiteral(".normalizing.mp4");
    QFile::remove(job->normalizedTempPath);

    const QStringList args = {
        QStringLiteral("-y"),
        QStringLiteral("-nostdin"),
        QStringLiteral("-hide_banner"),
        QStringLiteral("-loglevel"),
        QStringLiteral("error"),
        QStringLiteral("-i"),
        job->tempPath,
        QStringLiteral("-map"),
        QStringLiteral("0:v:0"),
        QStringLiteral("-map"),
        QStringLiteral("0:a:0"),
        QStringLiteral("-c:v"),
        QStringLiteral("copy"),
        QStringLiteral("-c:a"),
        QStringLiteral("aac"),
        QStringLiteral("-b:a"),
        QStringLiteral("128k"),
        QStringLiteral("-movflags"),
        QStringLiteral("+faststart"),
        job->normalizedTempPath,
    };

    auto *process = new QProcess(this);
    job->normalizeProcess = process;
    process->setProgram(ffmpeg);
    process->setArguments(args);
#ifdef Q_OS_WIN
    process->setCreateProcessArgumentsModifier(
        [](QProcess::CreateProcessArguments *arguments) {
            arguments->flags |= CREATE_NO_WINDOW;
        });
#endif

    const qint64 sourceId = job->sourceId;
    connect(process, QOverload<int, QProcess::ExitStatus>::of(&QProcess::finished), this,
            [this, sourceId](int exitCode, QProcess::ExitStatus exitStatus) {
                Job *current = findJob(sourceId);
                if (!current || current->finalized) {
                    return;
                }
                QProcess *finishedProcess = current->normalizeProcess;
                current->normalizeProcess = nullptr;
                if (finishedProcess) {
                    finishedProcess->deleteLater();
                }
                if (current->stopRequested) {
                    QFile::remove(current->normalizedTempPath);
                    finalize(current, false, QStringLiteral("下载已取消"));
                    return;
                }
                const bool ok = exitStatus == QProcess::NormalExit && exitCode == 0
                    && QFileInfo(current->normalizedTempPath).size() > 0;
                if (ok) {
                    finishDownloadFile(current, true,
                                       QStringLiteral("下载完成（音频已转换为 AAC）"));
                } else {
                    qWarning() << "Audio normalization failed for" << current->tempPath;
                    QFile::remove(current->normalizedTempPath);
                    finishDownloadFile(
                        current, false, QStringLiteral("下载完成（音频格式转换失败）"),
                        QStringLiteral("音频格式转换失败，部分播放器可能无声。"));
                }
            });
    connect(process, &QProcess::errorOccurred, this,
            [this, sourceId](QProcess::ProcessError processError) {
                if (processError != QProcess::FailedToStart) {
                    return;
                }
                Job *current = findJob(sourceId);
                if (!current || current->finalized) {
                    return;
                }
                QProcess *failedProcess = current->normalizeProcess;
                current->normalizeProcess = nullptr;
                if (failedProcess) {
                    failedProcess->deleteLater();
                }
                QFile::remove(current->normalizedTempPath);
                finishDownloadFile(
                    current, false, QStringLiteral("下载完成（音频格式转换失败）"),
                    QStringLiteral("音频格式转换失败，部分播放器可能无声。"));
            });

    emit downloadProgress(job->sourceId, job->receivedBytes, job->totalBytes);
    emit downloadStatus(job->sourceId, QStringLiteral("DL · 转换音频…"));
    process->start();
}

void RecordingService::finishDownloadFile(Job *job, bool normalized,
                                          const QString &message,
                                          const QString &warning)
{
    if (!job || job->finalized) {
        return;
    }
    const QString sourcePath = normalized ? job->normalizedTempPath : job->tempPath;
    QFile::remove(job->outputPath);
    if (!QFile::rename(sourcePath, job->outputPath)) {
        finalize(job, false, QStringLiteral("保存下载文件失败：%1").arg(job->outputPath));
        return;
    }
    if (normalized) {
        QFile::remove(job->tempPath);
    }
    job->warning = warning;
    finalize(job, true, message);
}

bool RecordingService::stopRecording(qint64 sourceId)
{
    Job *job = findJob(sourceId);
    if (!job) {
        return false;
    }
    if (job->stopRequested) {
        return true;
    }
    job->stopRequested = true;

    if (job->mode == Job::Mode::Download) {
        // abort()/kill() 可能同步触发 finished 回调并销毁 job，先取出指针。
        QNetworkReply *reply = job->reply;
        QProcess *normalizer = job->normalizeProcess;
        if (normalizer && normalizer->state() != QProcess::NotRunning) {
            normalizer->kill();
        }
        if (reply) {
            reply->abort();
        }
        return true;
    }

    if (job->process && job->process->state() != QProcess::NotRunning) {
        job->process->write("q\n");
    }

    const int timeoutMs = m_configStore
        ? qBound(1000, m_configStore->config().recording.stopTimeoutMs, 60000)
        : 8000;
    QTimer::singleShot(timeoutMs, this, [this, sourceId] {
        Job *current = findJob(sourceId);
        if (current && current->process
            && current->process->state() != QProcess::NotRunning) {
            current->process->kill();
        }
    });
    return true;
}

bool RecordingService::isRecording(qint64 sourceId) const
{
    return m_jobs.contains(sourceId);
}

void RecordingService::stopAllAndWait(int timeoutMs)
{
    abortPendingProbes();

    const QList<qint64> ids = m_jobs.keys();
    for (qint64 id : ids) {
        stopRecording(id);
    }

    QElapsedTimer timer;
    timer.start();
    for (qint64 id : ids) {
        Job *job = findJob(id);
        if (!job) {
            continue;
        }
        if (job->mode == Job::Mode::Download) {
            if (job->normalizeProcess
                && job->normalizeProcess->state() != QProcess::NotRunning) {
                const int remaining = timeoutMs - static_cast<int>(timer.elapsed());
                if (remaining <= 0
                    || !job->normalizeProcess->waitForFinished(remaining)) {
                    job->normalizeProcess->kill();
                    job->normalizeProcess->waitForFinished(1000);
                }
            }
            if (Job *current = findJob(id)) {
                finalize(current, false, QStringLiteral("应用退出，下载已中断。"));
            }
            continue;
        }

        if (!job->process) {
            continue;
        }
        const int remaining = timeoutMs - static_cast<int>(timer.elapsed());
        if (job->process->state() != QProcess::NotRunning) {
            if (remaining <= 0 || !job->process->waitForFinished(remaining)) {
                job->process->kill();
                job->process->waitForFinished(1000);
            }
        }
        if (Job *current = findJob(id)) {
            const int exitCode = current->process ? current->process->exitCode() : -1;
            const QProcess::ExitStatus exitStatus =
                current->process ? current->process->exitStatus() : QProcess::CrashExit;
            handleFinished(current, exitCode, exitStatus);
        }
    }
}

void RecordingService::shutdown()
{
    stopAllAndWait();
}

RecordingService::Job *RecordingService::findJob(qint64 sourceId) const
{
    return m_jobs.value(sourceId, nullptr);
}

void RecordingService::handleStandardOutput(Job *job)
{
    if (!job || !job->process) {
        return;
    }
    job->stdoutBuffer.append(job->process->readAllStandardOutput());

    int newline = -1;
    while ((newline = job->stdoutBuffer.indexOf('\n')) >= 0) {
        QByteArray line = job->stdoutBuffer.left(newline).trimmed();
        job->stdoutBuffer.remove(0, newline + 1);
        if (line.isEmpty()) {
            continue;
        }

        const int equals = line.indexOf('=');
        if (equals <= 0) {
            continue;
        }
        const QByteArray key = line.left(equals);
        const QByteArray value = line.mid(equals + 1);

        if (key == "out_time_us" || key == "out_time_ms") {
            bool ok = false;
            const qint64 microseconds = value.toLongLong(&ok);
            if (!ok) {
                continue;
            }
            const qint64 milliseconds = microseconds / 1000;
            if (job->firstOutTimeMs < 0) {
                job->firstOutTimeMs = milliseconds;
            }
            job->lastOutTimeMs = milliseconds;
            job->lastDurationMs =
                qMax<qint64>(0, job->lastOutTimeMs - job->firstOutTimeMs);
        } else if (key == "progress") {
            emit recordingProgress(job->sourceId, job->lastDurationMs);
        }
    }
}

void RecordingService::handleStandardError(Job *job)
{
    if (!job || !job->process) {
        return;
    }
    const QByteArray payload = job->process->readAllStandardError();
    if (payload.isEmpty()) {
        return;
    }
    job->stderrTail.append(QString::fromLocal8Bit(payload));
    if (job->stderrTail.size() > kStderrTailLimit) {
        job->stderrTail = job->stderrTail.right(kStderrTailLimit);
    }
    if (job->logFile) {
        job->logFile->write(payload);
    }
}

void RecordingService::handleFinished(Job *job, int exitCode,
                                      QProcess::ExitStatus exitStatus)
{
    if (!job || job->finalized) {
        return;
    }

    handleStandardOutput(job);
    handleStandardError(job);

    const QFileInfo playlistInfo(job->outputPath);
    const bool playlistValid = playlistInfo.isFile() && playlistInfo.size() > 0;

    bool success = false;
    QString message;
    if (exitStatus == QProcess::NormalExit && exitCode == 0 && playlistValid) {
        success = true;
        message = job->stopRequested ? QStringLiteral("录制已停止，文件已保存")
                                     : QStringLiteral("录制完成");
    } else if (job->stopRequested && playlistValid) {
        success = true;
        message = QStringLiteral("录制已停止，文件已保存");
    } else {
        QString reason = lastErrorLine(job->stderrTail);
        if (reason.isEmpty()) {
            reason = QStringLiteral("ffmpeg 退出码 %1").arg(exitCode);
        }
        message = QStringLiteral("录制失败：%1").arg(reason);
    }

    finalize(job, success, message);
}

void RecordingService::finalize(Job *job, bool success, const QString &message)
{
    if (!job || job->finalized) {
        return;
    }
    job->finalized = true;

    if (job->mode == Job::Mode::Stream) {
        if (job->process && job->process->state() != QProcess::NotRunning) {
            job->process->kill();
            job->process->waitForFinished(1000);
        }
        if (job->logFile) {
            job->logFile->flush();
            job->logFile->close();
            delete job->logFile;
            job->logFile = nullptr;
        }
    } else {
        if (job->idleTimer) {
            job->idleTimer->stop();
            job->idleTimer->deleteLater();
            job->idleTimer = nullptr;
        }
        if (job->reply) {
            job->reply->disconnect(this);
            job->reply->abort();
            job->reply->deleteLater();
            job->reply = nullptr;
        }
        if (job->downloadFile) {
            job->downloadFile->flush();
            job->downloadFile->close();
            delete job->downloadFile;
            job->downloadFile = nullptr;
        }
        if (QProcess *normalizer = job->normalizeProcess) {
            job->normalizeProcess = nullptr;
            // 先断开回调，避免 kill/wait 触发的 finished 再次进入 finalize。
            normalizer->disconnect(this);
            if (normalizer->state() != QProcess::NotRunning) {
                normalizer->kill();
                normalizer->waitForFinished(1000);
            }
            normalizer->deleteLater();
        }
        if (!success) {
            if (!job->tempPath.isEmpty()) {
                QFile::remove(job->tempPath);
            }
            if (!job->normalizedTempPath.isEmpty()) {
                QFile::remove(job->normalizedTempPath);
            }
            if (!job->outputPath.isEmpty()) {
                QFile::remove(job->outputPath);
            }
            QDir(job->outputDir).removeRecursively();
        }
    }

    const qint64 finishedAtMs = QDateTime::currentMSecsSinceEpoch();
    qint64 durationMs = 0;
    qint64 sizeBytes = 0;
    if (job->mode == Job::Mode::Stream) {
        durationMs = MediaUtils::hlsPlaylistDurationMs(job->outputPath);
        if (durationMs <= 0) {
            durationMs = job->lastDurationMs;
        }
        if (durationMs <= 0) {
            durationMs = qMax<qint64>(0, finishedAtMs - job->startedAtMs);
        }
        sizeBytes = MediaUtils::directorySize(job->outputDir);
    } else if (success) {
        durationMs = job->probe.durationMs;
        if (durationMs <= 0) {
            const ProbeInfo finalProbe = MediaProbe::probe(job->outputPath);
            if (finalProbe.ok) {
                durationMs = finalProbe.durationMs;
            }
        }
        sizeBytes = QFileInfo(job->outputPath).size();
    }

    QString finalMessage = message;
    QString dbError;
    const QString warning = success ? job->warning : QString();
    if (!m_repository->finishRecording(
            job->sourceId, job->sessionId,
            success ? SessionStatus::Completed : SessionStatus::Failed,
            success ? SourceStatus::Ready : SourceStatus::Failed,
            durationMs, sizeBytes, success ? warning : finalMessage, finishedAtMs,
            &dbError)) {
        if (dbError.isEmpty()) {
            dbError = QStringLiteral("数据库更新失败。");
        }
        finalMessage = finalMessage.isEmpty()
            ? dbError
            : finalMessage + QStringLiteral("（%1）").arg(dbError);
        qWarning() << "Failed to persist recording result:" << dbError;
    }

    const qint64 sourceId = job->sourceId;
    if (job->process) {
        job->process->disconnect(this);
        job->process->deleteLater();
        job->process = nullptr;
    }
    if (job->mode == Job::Mode::Download) {
        emit downloadProgress(sourceId, job->receivedBytes, job->totalBytes);
    }
    m_jobs.remove(sourceId);
    delete job;

    emit recordingFinished(sourceId, success, finalMessage);
}

QString RecordingService::deriveTitle(const QString &uri, const QString &title)
{
    const QString explicitTitle = title.trimmed();
    if (!explicitTitle.isEmpty()) {
        return explicitTitle;
    }

    const QUrl url(uri);
    QString base;
    if (url.isLocalFile()) {
        base = QFileInfo(url.toLocalFile()).completeBaseName();
    } else {
        base = QFileInfo(url.path()).completeBaseName();
    }
    if (base.isEmpty()) {
        base = QStringLiteral("直播录制");
    }
    return QStringLiteral("%1 %2")
        .arg(QDateTime::currentDateTime().toString(QStringLiteral("MM-dd HH:mm")), base);
}

bool RecordingService::isHttpUrl(const QString &uri)
{
    const QString scheme = QUrl(uri).scheme().toLower();
    return scheme == QLatin1String("http") || scheme == QLatin1String("https");
}
