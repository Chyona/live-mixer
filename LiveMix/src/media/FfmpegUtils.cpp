// 文件作用：ffmpeg 子进程调用实现（音频提取、视频裁剪、分辨率探测）。
#include "FfmpegUtils.h"

#include "../core/MediaUtils.h"
#include "FfmpegLocator.h"
#include "MediaProbe.h"

#include <QCryptographicHash>
#include <QDateTime>
#include <QDebug>
#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <QProcess>
#include <QStandardPaths>
#include <QStringList>
#include <QTemporaryFile>

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

QString shortError(const QString &text)
{
    const QString trimmed = text.trimmed();
    if (trimmed.isEmpty()) {
        return QStringLiteral("ffmpeg 执行失败。");
    }
    return trimmed.size() > 800 ? trimmed.right(800) : trimmed;
}

QString previewCachePath(const QString &videoPath, const QString &suffix)
{
    QString cacheDir = QStandardPaths::writableLocation(QStandardPaths::CacheLocation);
    if (cacheDir.isEmpty()) {
        cacheDir = QDir::tempPath();
    }
    const QFileInfo info(videoPath);
    const QString key = videoPath + QLatin1Char(':')
        + QString::number(info.lastModified().toMSecsSinceEpoch());
    const QString hash = QString::fromLatin1(
        QCryptographicHash::hash(key.toUtf8(), QCryptographicHash::Sha1).toHex().left(16));
    return QDir(cacheDir).filePath(QStringLiteral("previews/") + hash + suffix);
}

bool runFfmpeg(const QStringList &arguments, int timeoutMs, QString *error)
{
    const QString ffmpeg = FfmpegLocator::ffmpegPath();
    if (ffmpeg.isEmpty()) {
        if (error) {
            *error = QStringLiteral("未找到 ffmpeg，请重新安装应用或设置 LIVEMIX_FFMPEG_DIR。");
        }
        return false;
    }

    QProcess process;
    process.setProgram(ffmpeg);
    process.setArguments(arguments);
#ifdef Q_OS_WIN
    process.setCreateProcessArgumentsModifier(
        [](QProcess::CreateProcessArguments *args) {
            args->flags |= CREATE_NO_WINDOW;
        });
#endif
    process.start();
    if (!process.waitForStarted(5000)) {
        if (error) {
            *error = QStringLiteral("无法启动 ffmpeg：%1").arg(process.errorString());
        }
        return false;
    }
    if (!process.waitForFinished(timeoutMs)) {
        process.kill();
        process.waitForFinished(1000);
        if (error) {
            *error = QStringLiteral("ffmpeg 执行超时。");
        }
        return false;
    }
    if (process.exitStatus() != QProcess::NormalExit || process.exitCode() != 0) {
        if (error) {
            *error = shortError(QString::fromLocal8Bit(process.readAllStandardError()));
        }
        return false;
    }
    return true;
}

} // namespace

bool FfmpegUtils::extractMp3(const QString &inputPath, const QString &outputPath,
                             QString *error)
{
    const QFileInfo input(inputPath);
    if (!input.exists()) {
        if (error) {
            *error = QStringLiteral("源文件不存在：%1").arg(inputPath);
        }
        return false;
    }
    if (!QDir().mkpath(QFileInfo(outputPath).absolutePath())) {
        if (error) {
            *error = QStringLiteral("无法创建音频目录：%1")
                         .arg(QFileInfo(outputPath).absolutePath());
        }
        return false;
    }

    // ASR 标准版通过公网 URL 读取整段音频，不受上传体积限制；
    // 16kHz 单声道 64kbps 约每小时 28MB，足够容纳常见直播时长。
    //
    // ASR 返回的时间线是相对这份 MP3 的，所以 MP3 必须与源 MP4 等长：
    // 1. aresample=first_pts=0 在音轨晚于视频起点时补开头静音；
    // 2. apad 补尾部静音，-t 再精确截到视频时长（音轨比视频长时同样会截断）。
    qint64 targetDurationMs = 0;
    const bool hasDuration =
        mediaDurationMs(inputPath, &targetDurationMs) && targetDurationMs > 0;

    QStringList args = {
        QStringLiteral("-y"),           QStringLiteral("-nostdin"),
        QStringLiteral("-hide_banner"), QStringLiteral("-loglevel"),
        QStringLiteral("error"),        QStringLiteral("-i"),
        inputPath,                      QStringLiteral("-map"),
        QStringLiteral("0:a:0"),
    };
    if (hasDuration) {
        args << QStringLiteral("-af")
             << QStringLiteral("aresample=async=1:first_pts=0,apad")
             << QStringLiteral("-t")
             << QString::number(targetDurationMs / 1000.0, 'f', 3);
    } else {
        // 读不到容器时长时至少保证音轨起点对齐视频 t=0。
        args << QStringLiteral("-af")
             << QStringLiteral("aresample=async=1:first_pts=0");
    }
    args << QStringLiteral("-ac") << QStringLiteral("1")
         << QStringLiteral("-ar") << QStringLiteral("16000")
         << QStringLiteral("-c:a") << QStringLiteral("libmp3lame")
         << QStringLiteral("-b:a") << QStringLiteral("64k")
         << outputPath;
    if (!runFfmpeg(args, 30 * 60 * 1000, error)) {
        return false;
    }

    const QFileInfo output(outputPath);
    if (!output.exists() || output.size() <= 0) {
        if (error) {
            *error = QStringLiteral("音频提取失败，未生成有效文件。");
        }
        return false;
    }

    // 复查输出时长：MP3 按帧（16kHz 下约 72ms）对齐，允许少量取整误差；
    // 偏差超过 500ms 说明补静音/截断没有生效，继续下去会让字幕整体错位。
    qint64 outputDurationMs = 0;
    if (hasDuration && mediaDurationMs(outputPath, &outputDurationMs)) {
        const qint64 differenceMs = qAbs(outputDurationMs - targetDurationMs);
        if (differenceMs > 500) {
            if (error) {
                *error = QStringLiteral("音频时长与源视频不一致（源 %1ms，音频 %2ms）。")
                             .arg(targetDurationMs)
                             .arg(outputDurationMs);
            }
            return false;
        }
        if (differenceMs > 100) {
            qWarning() << "Extracted MP3 duration differs from source by"
                       << differenceMs << "ms";
        }
    }
    return true;
}

bool FfmpegUtils::cutClip(const QString &inputPath, qint64 startMs, qint64 endMs,
                          const QString &outputPath, QString *error)
{
    if (endMs <= startMs) {
        if (error) {
            *error = QStringLiteral("切片时间范围无效。");
        }
        return false;
    }
    if (!QDir().mkpath(QFileInfo(outputPath).absolutePath())) {
        if (error) {
            *error = QStringLiteral("无法创建切片目录：%1")
                         .arg(QFileInfo(outputPath).absolutePath());
        }
        return false;
    }

    const double startSeconds = startMs / 1000.0;
    const double durationSeconds = (endMs - startMs) / 1000.0;

    // -ss 放在 -i 之前可快速定位；配合重新编码，现代 ffmpeg 会丢弃关键帧之前的
    // 内容，得到准确的起止时间。重新编码为 H.264/AAC 以兼容剪映。
    const QStringList args = {
        QStringLiteral("-y"),
        QStringLiteral("-nostdin"),
        QStringLiteral("-hide_banner"),
        QStringLiteral("-loglevel"),
        QStringLiteral("error"),
        QStringLiteral("-ss"),
        QString::number(startSeconds, 'f', 3),
        QStringLiteral("-i"),
        inputPath,
        QStringLiteral("-t"),
        QString::number(durationSeconds, 'f', 3),
        QStringLiteral("-map"),
        QStringLiteral("0:v:0"),
        QStringLiteral("-map"),
        QStringLiteral("0:a:0?"),
        QStringLiteral("-c:v"),
        QStringLiteral("libx264"),
        QStringLiteral("-preset"),
        QStringLiteral("veryfast"),
        QStringLiteral("-crf"),
        QStringLiteral("20"),
        QStringLiteral("-pix_fmt"),
        QStringLiteral("yuv420p"),
        QStringLiteral("-c:a"),
        QStringLiteral("aac"),
        QStringLiteral("-b:a"),
        QStringLiteral("128k"),
        QStringLiteral("-movflags"),
        QStringLiteral("+faststart"),
        outputPath,
    };
    if (!runFfmpeg(args, 10 * 60 * 1000, error)) {
        return false;
    }

    const QFileInfo output(outputPath);
    if (!output.exists() || output.size() <= 0) {
        if (error) {
            *error = QStringLiteral("视频裁剪失败，未生成有效文件。");
        }
        return false;
    }
    return true;
}

bool FfmpegUtils::mediaDurationMs(const QString &filePath, qint64 *durationMs)
{
    const QString ffprobe = FfmpegLocator::ffprobePath();
    if (ffprobe.isEmpty()) {
        return false;
    }

    QProcess process;
    process.setProgram(ffprobe);
    process.setArguments({QStringLiteral("-v"), QStringLiteral("error"),
                          QStringLiteral("-show_entries"), QStringLiteral("format=duration"),
                          QStringLiteral("-of"), QStringLiteral("default=nw=1:nk=1"),
                          filePath});
#ifdef Q_OS_WIN
    process.setCreateProcessArgumentsModifier(
        [](QProcess::CreateProcessArguments *args) {
            args->flags |= CREATE_NO_WINDOW;
        });
#endif
    process.start();
    if (!process.waitForStarted(5000) || !process.waitForFinished(30000)) {
        process.kill();
        process.waitForFinished(1000);
        return false;
    }
    if (process.exitStatus() != QProcess::NormalExit || process.exitCode() != 0) {
        return false;
    }

    bool ok = false;
    const double seconds =
        QString::fromLatin1(process.readAllStandardOutput()).trimmed().toDouble(&ok);
    if (!ok || seconds <= 0.0) {
        return false;
    }
    if (durationMs) {
        *durationMs = qRound64(seconds * 1000.0);
    }
    return true;
}

bool FfmpegUtils::extractThumbnail(const QString &inputPath, qint64 positionMs,
                                   const QString &outputPath, QString *error)
{
    if (!QDir().mkpath(QFileInfo(outputPath).absolutePath())) {
        if (error) {
            *error = QStringLiteral("无法创建预览图目录。");
        }
        return false;
    }

    const double seconds = qMax<qint64>(0, positionMs) / 1000.0;
    const QStringList args = {
        QStringLiteral("-y"),           QStringLiteral("-nostdin"),
        QStringLiteral("-hide_banner"), QStringLiteral("-loglevel"),
        QStringLiteral("error"),        QStringLiteral("-ss"),
        QString::number(seconds, 'f', 3),
        QStringLiteral("-i"),           inputPath,
        QStringLiteral("-frames:v"),    QStringLiteral("1"),
        QStringLiteral("-vf"),          QStringLiteral("scale=640:-2"),
        outputPath,
    };
    if (!runFfmpeg(args, 30000, error)) {
        return false;
    }
    return QFileInfo::exists(outputPath) && QFileInfo(outputPath).size() > 0;
}

QString FfmpegUtils::thumbnailCachePath(const QString &videoPath)
{
    QString cacheDir = QStandardPaths::writableLocation(QStandardPaths::CacheLocation);
    if (cacheDir.isEmpty()) {
        cacheDir = QDir::tempPath();
    }
    const QFileInfo info(videoPath);
    const QString key = videoPath + QLatin1Char(':')
        + QString::number(info.lastModified().toMSecsSinceEpoch());
    const QString hash = QString::fromLatin1(
        QCryptographicHash::hash(key.toUtf8(), QCryptographicHash::Sha1).toHex().left(16));
    return QDir(cacheDir).filePath(QStringLiteral("previews/") + hash
                                   + QStringLiteral(".png"));
}

QString FfmpegUtils::hlsPreviewCachePath(const QString &videoPath)
{
    // 文件名带 full 标记：旧版本缓存只含前 5 分钟，不能复用。
    return previewCachePath(videoPath, QStringLiteral("-hls-full.mp4"));
}

QString FfmpegUtils::hlsPreviewHeadCachePath(const QString &videoPath)
{
    return previewCachePath(videoPath, QStringLiteral("-hls-head60.mp4"));
}

QString FfmpegUtils::recordingMp4Path(const QString &playlistPath)
{
    if (!playlistPath.endsWith(QLatin1String(".m3u8"), Qt::CaseInsensitive)) {
        return playlistPath;
    }
    return QDir(QFileInfo(playlistPath).absolutePath())
        .filePath(QStringLiteral("index.mp4"));
}

bool FfmpegUtils::ensureRecordingMp4(const QString &playlistPath, QString *mp4Path,
                                     QString *error)
{
    const QString outputPath = recordingMp4Path(playlistPath);
    if (playlistPath.isEmpty()) {
        if (error) {
            *error = QStringLiteral("源视频路径为空。");
        }
        return false;
    }
    if (!playlistPath.endsWith(QLatin1String(".m3u8"), Qt::CaseInsensitive)) {
        if (mp4Path) {
            *mp4Path = outputPath;
        }
        return true;
    }

    QFile playlistFile(playlistPath);
    if (!playlistFile.open(QIODevice::ReadOnly)) {
        if (error) {
            *error = QStringLiteral("无法读取 HLS 播放列表：%1").arg(playlistPath);
        }
        return false;
    }
    const QByteArray playlistData = playlistFile.readAll();
    playlistFile.close();
    const bool playlistComplete = playlistData.contains("#EXT-X-ENDLIST");
    const QFileInfo playlistInfo(playlistPath);
    const qint64 playlistMs = MediaUtils::hlsPlaylistDurationMs(playlistPath);

    // 正常完成的录像允许少量取整误差；意外中断的录像最后一段可能被截断，
    // 放宽到一个分片多一点，保证恢复出来的内容仍可成片。
    const qint64 toleranceMs = playlistComplete ? 2000 : 6000;

    auto isUsableMp4 = [&](const QString &path) {
        const QFileInfo info(path);
        if (!info.isFile() || info.size() <= 0) {
            return false;
        }
        // MP4 必须晚于播放列表生成，避免复用录制追加前的旧文件。
        if (info.lastModified() < playlistInfo.lastModified()) {
            return false;
        }
        qint64 durationMs = 0;
        if (!mediaDurationMs(path, &durationMs) || durationMs <= 0) {
            return false;
        }
        return playlistMs <= 0 || durationMs + toleranceMs >= playlistMs;
    };

    if (isUsableMp4(outputPath)) {
        qInfo() << "Reusing complete recording MP4:" << outputPath;
        if (mp4Path) {
            *mp4Path = outputPath;
        }
        return true;
    }

    // 之前预览生成的完整 MP4 缓存可以直接转正，避免同一录像存两份大文件。
    const QString cachePath = hlsPreviewCachePath(playlistPath);
    if (isUsableMp4(cachePath)) {
        QFile::remove(outputPath);
        if (QFile::rename(cachePath, outputPath)) {
            qInfo() << "Reused preview MP4 cache as recording MP4:" << outputPath;
            if (mp4Path) {
                *mp4Path = outputPath;
            }
            return true;
        }
        qWarning() << "Unable to move preview MP4 cache, falling back to remux:"
                   << cachePath;
    }

    // 中断的录像可能缺少 ENDLIST；合成前补一份临时列表，避免末尾分片被丢弃。
    QString playlistForRemux = playlistPath;
    QTemporaryFile endedPlaylist(playlistInfo.absolutePath()
                                 + QStringLiteral("/.livemix-remux-XXXXXX.m3u8"));
    if (!playlistComplete) {
        if (!endedPlaylist.open()) {
            if (error) {
                *error = QStringLiteral("无法创建临时播放列表：%1")
                             .arg(endedPlaylist.errorString());
            }
            return false;
        }
        endedPlaylist.write(playlistData);
        if (!playlistData.endsWith('\n')) {
            endedPlaylist.write("\n");
        }
        endedPlaylist.write("#EXT-X-ENDLIST\n");
        endedPlaylist.close();
        playlistForRemux = endedPlaylist.fileName();
    }

    // 清理上次崩溃留下的半成品，避免长期占用双份磁盘空间。
    const QDateTime staleBefore = QDateTime::currentDateTime().addSecs(-3600);
    const QFileInfoList staleParts = QDir(playlistInfo.absolutePath())
        .entryInfoList({QStringLiteral("index-*.part.mp4")}, QDir::Files);
    for (const QFileInfo &part : staleParts) {
        if (part.lastModified() < staleBefore) {
            QFile::remove(part.absoluteFilePath());
        }
    }

    QTemporaryFile tempOutput(playlistInfo.absolutePath()
                              + QStringLiteral("/index-XXXXXX.part.mp4"));
    if (!tempOutput.open()) {
        if (error) {
            *error = QStringLiteral("无法创建 MP4 临时文件：%1")
                         .arg(tempOutput.errorString());
        }
        return false;
    }
    const QString tempPath = tempOutput.fileName();
    tempOutput.close();

    const QStringList args = {
        QStringLiteral("-y"),          QStringLiteral("-nostdin"),
        QStringLiteral("-hide_banner"), QStringLiteral("-loglevel"),
        QStringLiteral("error"),       QStringLiteral("-i"),
        playlistForRemux,              QStringLiteral("-map"),
        QStringLiteral("0:v:0"),       QStringLiteral("-map"),
        QStringLiteral("0:a:0?"),      QStringLiteral("-c"),
        QStringLiteral("copy"),        QStringLiteral("-f"),
        QStringLiteral("mp4"),         QStringLiteral("-movflags"),
        QStringLiteral("+faststart"),  tempPath,
    };
    if (!runFfmpeg(args, 30 * 60 * 1000, error)) {
        QFile::remove(tempPath);
        return false;
    }

    qint64 outputMs = 0;
    if (!mediaDurationMs(tempPath, &outputMs) || outputMs <= 0) {
        QFile::remove(tempPath);
        if (error) {
            *error = QStringLiteral("MP4 合成失败：输出文件不可用。");
        }
        return false;
    }
    if (playlistMs > 0 && outputMs + toleranceMs < playlistMs) {
        QFile::remove(tempPath);
        if (error) {
            *error = QStringLiteral("MP4 合成不完整：输出 %1ms，播放列表 %2ms。")
                         .arg(outputMs)
                         .arg(playlistMs);
        }
        return false;
    }

    QFile::remove(outputPath);
    if (!QFile::rename(tempPath, outputPath)) {
        QFile::remove(tempPath);
        if (error) {
            *error = QStringLiteral("无法保存完整 MP4：%1").arg(outputPath);
        }
        return false;
    }
    // QTemporaryFile 默认 0600；完整 MP4 作为用户可见素材，恢复为常规读权限。
    QFile::setPermissions(outputPath,
                          QFile::ReadOwner | QFile::WriteOwner
                              | QFile::ReadGroup | QFile::ReadOther);

    qInfo() << "Recording synthesized to MP4:" << outputPath;
    if (mp4Path) {
        *mp4Path = outputPath;
    }
    return true;
}

bool FfmpegUtils::videoSize(const QString &filePath, int *width, int *height)
{
    const ProbeInfo probe = MediaProbe::probe(filePath);
    if (!probe.ok) {
        return false;
    }

    const QJsonObject root =
        QJsonDocument::fromJson(probe.json.toUtf8()).object();
    const QJsonArray streams = root.value(QStringLiteral("streams")).toArray();
    for (const QJsonValue &value : streams) {
        const QJsonObject stream = value.toObject();
        if (stream.value(QStringLiteral("codec_type")).toString()
            != QLatin1String("video")) {
            continue;
        }
        const QJsonObject disposition =
            stream.value(QStringLiteral("disposition")).toObject();
        if (disposition.value(QStringLiteral("attached_pic")).toInt() == 1) {
            continue;
        }
        const int streamWidth = stream.value(QStringLiteral("width")).toInt();
        const int streamHeight = stream.value(QStringLiteral("height")).toInt();
        if (streamWidth > 0 && streamHeight > 0) {
            if (width) {
                *width = streamWidth;
            }
            if (height) {
                *height = streamHeight;
            }
            return true;
        }
    }
    return false;
}
