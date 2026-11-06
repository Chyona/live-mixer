#include "MediaProbe.h"

#include "FfmpegLocator.h"

#include <QFileInfo>
#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <QJsonParseError>
#include <QProcess>
#include <QStringList>

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

QString trimmedError(const QString &text)
{
    const QString trimmed = text.trimmed();
    if (trimmed.isEmpty()) {
        return QStringLiteral("ffprobe 执行失败。");
    }
    return trimmed.size() > 600 ? trimmed.left(600) + QStringLiteral("…") : trimmed;
}

} // namespace

ProbeInfo MediaProbe::probe(const QString &filePath, int timeoutMs)
{
    ProbeInfo result;

    const QFileInfo fileInfo(filePath);
    if (!fileInfo.exists() || !fileInfo.isFile()) {
        result.error = QStringLiteral("文件不存在：%1").arg(filePath);
        return result;
    }

    const QString ffprobe = FfmpegLocator::ffprobePath();
    if (ffprobe.isEmpty()) {
        result.error = QStringLiteral(
            "未找到 ffprobe。请重新安装应用，或设置 LIVEMIX_FFMPEG_DIR 指向包含 ffprobe 的目录。");
        return result;
    }

    QProcess process;
    process.setProgram(ffprobe);
    process.setArguments({QStringLiteral("-v"), QStringLiteral("error"),
                          QStringLiteral("-print_format"), QStringLiteral("json"),
                          QStringLiteral("-show_format"), QStringLiteral("-show_streams"),
                          filePath});
#ifdef Q_OS_WIN
    process.setCreateProcessArgumentsModifier(
        [](QProcess::CreateProcessArguments *arguments) {
            arguments->flags |= CREATE_NO_WINDOW;
        });
#endif
    process.start();
    if (!process.waitForStarted(3000)) {
        result.error = QStringLiteral("无法启动 ffprobe：%1").arg(process.errorString());
        return result;
    }
    if (!process.waitForFinished(timeoutMs)) {
        process.kill();
        process.waitForFinished(1000);
        result.error = QStringLiteral("读取媒体信息超时。");
        return result;
    }

    if (process.exitStatus() != QProcess::NormalExit || process.exitCode() != 0) {
        result.error = trimmedError(QString::fromLocal8Bit(process.readAllStandardError()));
        return result;
    }

    const QByteArray payload = process.readAllStandardOutput();
    QJsonParseError parseError;
    const QJsonDocument document = QJsonDocument::fromJson(payload, &parseError);
    if (parseError.error != QJsonParseError::NoError || !document.isObject()) {
        result.error = QStringLiteral("无法解析 ffprobe 输出：%1").arg(parseError.errorString());
        return result;
    }

    const QJsonObject root = document.object();
    const QJsonObject format = root.value(QStringLiteral("format")).toObject();
    if (format.isEmpty()) {
        result.error = QStringLiteral("无法识别媒体格式。");
        return result;
    }

    // Local imports are video-only. Accept a container when it has at least
    // one real video stream; cover art is reported as attached_pic and ignored.
    bool hasVideoStream = false;
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
        hasVideoStream = true;
        break;
    }
    if (!hasVideoStream) {
        result.error = QStringLiteral("文件中没有视频流，仅支持视频文件。");
        return result;
    }

    bool durationOk = false;
    double durationSeconds = 0.0;
    const QJsonValue durationValue = format.value(QStringLiteral("duration"));
    if (durationValue.isString()) {
        durationSeconds = durationValue.toString().toDouble(&durationOk);
    } else if (durationValue.isDouble()) {
        durationSeconds = durationValue.toDouble();
        durationOk = true;
    }
    if (durationOk && durationSeconds > 0.0) {
        result.durationMs = static_cast<qint64>(durationSeconds * 1000.0 + 0.5);
    }
    result.sizeBytes = fileInfo.size();
    result.json = QString::fromUtf8(payload);
    result.ok = true;
    return result;
}
