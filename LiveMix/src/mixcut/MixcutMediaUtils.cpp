// 文件作用：智能混剪媒体工具实现。

#include "MixcutMediaUtils.h"

#include "../media/FfmpegLocator.h"

#include <QDir>
#include <QFileInfo>
#include <QProcess>
#include <QRegularExpression>
#include <QtMath>

#include <algorithm>
#include <cmath>

namespace {

bool runFfmpeg(const QStringList &arguments, int timeoutMs, QString *error)
{
    const QString ffmpeg = FfmpegLocator::ffmpegPath();
    if (ffmpeg.isEmpty()) {
        if (error) {
            *error = QStringLiteral("未找到 ffmpeg，请重新安装应用。");
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
        const QString detail =
            QString::fromLocal8Bit(process.readAllStandardError()).trimmed();
        if (error) {
            *error = detail.isEmpty() ? QStringLiteral("ffmpeg 执行失败。")
                                      : detail.right(600);
        }
        return false;
    }
    return true;
}

struct Range
{
    qint64 startMs = 0;
    qint64 endMs = 0;
    qint64 durationMs() const { return endMs - startMs; }
};

} // namespace

namespace Mixcut {

bool detectSceneCuts(const QString &videoPath, qint64 durationMs,
                     QVector<qint64> *cuts, QString *error)
{
    if (!cuts || videoPath.isEmpty() || durationMs <= 0) {
        if (error) {
            *error = QStringLiteral("场景检测参数无效。");
        }
        return false;
    }
    cuts->clear();

    // showinfo 的日志写到 stderr；缩小到 320 宽再检测可显著加快长素材。
    const QStringList args = {
        QStringLiteral("-hide_banner"),
        QStringLiteral("-nostats"),
        QStringLiteral("-loglevel"),
        QStringLiteral("info"),
        QStringLiteral("-i"),
        videoPath,
        QStringLiteral("-an"),
        QStringLiteral("-vf"),
        QStringLiteral("scale=320:-2,select='gt(scene,0.30)',showinfo"),
        QStringLiteral("-f"),
        QStringLiteral("null"),
        QStringLiteral("-"),
    };
    const QString ffmpeg = FfmpegLocator::ffmpegPath();
    if (ffmpeg.isEmpty()) {
        if (error) {
            *error = QStringLiteral("未找到 ffmpeg，请重新安装应用。");
        }
        return false;
    }
    QProcess process;
    process.setProgram(ffmpeg);
    process.setArguments(args);
#ifdef Q_OS_WIN
    process.setCreateProcessArgumentsModifier(
        [](QProcess::CreateProcessArguments *processArgs) {
            processArgs->flags |= CREATE_NO_WINDOW;
        });
#endif
    process.start();
    if (!process.waitForStarted(5000)) {
        if (error) {
            *error = QStringLiteral("无法启动 ffmpeg：%1").arg(process.errorString());
        }
        return false;
    }
    // 场景检测只解码不编码，按 4 倍实时估算超时，下限 60 秒。
    const int timeoutMs =
        qBound(60000, int(durationMs * 4) + 30000, 10 * 60 * 1000);
    if (!process.waitForFinished(timeoutMs)) {
        process.kill();
        process.waitForFinished(1000);
        // 超时前已经输出的切换点仍然可用。
    }
    const QString log = QString::fromLocal8Bit(process.readAllStandardError());
    static const QRegularExpression pattern(
        QStringLiteral("pts_time:([0-9]+(?:\\.[0-9]+)?)"));
    QRegularExpressionMatchIterator it = pattern.globalMatch(log);
    const qint64 marginMs = 400;
    qint64 last = 0;
    while (it.hasNext()) {
        const QRegularExpressionMatch match = it.next();
        bool ok = false;
        const double seconds = match.captured(1).toDouble(&ok);
        if (!ok) {
            continue;
        }
        const qint64 atMs = qRound64(seconds * 1000.0);
        if (atMs <= marginMs || atMs >= durationMs - marginMs) {
            continue;
        }
        // 间隔小于 1.2 秒的切换点大概率是同一场景内的运动，忽略。
        if (atMs - last < 1200) {
            continue;
        }
        last = atMs;
        cuts->append(atMs);
    }
    return true;
}

QVector<QPair<qint64, qint64>>
normalizeSceneRanges(const QVector<qint64> &cuts, qint64 durationMs,
                     int maxClips)
{
    QVector<QPair<qint64, qint64>> ranges;
    if (durationMs <= 0) {
        return ranges;
    }
    qint64 start = 0;
    for (qint64 cut : cuts) {
        const qint64 clamped = qBound<qint64>(0, cut, durationMs);
        if (clamped - start >= 600) {
            ranges.append({start, clamped});
            start = clamped;
        }
    }
    if (durationMs - start >= 200) {
        ranges.append({start, durationMs});
    }
    if (ranges.isEmpty()) {
        ranges.append({0, durationMs});
    }

    // 过短镜头并入前一个（首个镜头并入后一个）。
    QVector<QPair<qint64, qint64>> merged;
    for (const auto &range : ranges) {
        const qint64 length = range.second - range.first;
        if (!merged.isEmpty() && length < 1200) {
            merged.last().second = range.second;
        } else if (merged.isEmpty() && length < 600 && ranges.size() > 1) {
            merged.append({range.first, range.second});
        } else {
            merged.append(range);
        }
    }
    // 合并后可能出现相邻重复区间，统一按连续区间重新整理。
    QVector<QPair<qint64, qint64>> cleaned;
    for (const auto &range : merged) {
        if (range.second - range.first <= 0) {
            continue;
        }
        // 只合并真正重叠的区间；首尾相接的相邻镜头必须保持独立。
        if (!cleaned.isEmpty() && range.first < cleaned.last().second) {
            cleaned.last().second = qMax(cleaned.last().second, range.second);
        } else {
            cleaned.append(range);
        }
    }
    if (cleaned.isEmpty()) {
        cleaned.append({0, durationMs});
    }

    const int cap = qMax(1, maxClips);
    while (cleaned.size() > cap) {
        int shortest = 0;
        for (int index = 1; index < cleaned.size(); ++index) {
            if (cleaned.at(index).second - cleaned.at(index).first
                < cleaned.at(shortest).second - cleaned.at(shortest).first) {
                shortest = index;
            }
        }
        if (shortest == 0) {
            cleaned[1].first = cleaned[0].first;
            cleaned.removeAt(0);
        } else {
            cleaned[shortest - 1].second = cleaned.at(shortest).second;
            cleaned.removeAt(shortest);
        }
    }
    return cleaned;
}

bool extractFrameAt(const QString &videoPath, qint64 positionMs,
                    const QString &outputPath, QString *error)
{
    if (!QDir().mkpath(QFileInfo(outputPath).absolutePath())) {
        if (error) {
            *error = QStringLiteral("无法创建关键帧目录：%1")
                         .arg(QFileInfo(outputPath).absolutePath());
        }
        return false;
    }
    const QStringList args = {
        QStringLiteral("-y"),
        QStringLiteral("-nostdin"),
        QStringLiteral("-hide_banner"),
        QStringLiteral("-loglevel"),
        QStringLiteral("error"),
        QStringLiteral("-ss"),
        QString::number(qMax<qint64>(0, positionMs) / 1000.0, 'f', 3),
        QStringLiteral("-i"),
        videoPath,
        QStringLiteral("-frames:v"),
        QStringLiteral("1"),
        QStringLiteral("-vf"),
        QStringLiteral("scale='min(640,iw)':-2"),
        QStringLiteral("-q:v"),
        QStringLiteral("3"),
        outputPath,
    };
    return runFfmpeg(args, 60000, error);
}

bool concatAudio(const QStringList &parts, const QString &outputPath,
                 QString *error)
{
    if (parts.isEmpty()) {
        if (error) {
            *error = QStringLiteral("没有可拼接的音频片段。");
        }
        return false;
    }
    if (parts.size() == 1) {
        if (QFileInfo(parts.first()).absoluteFilePath()
            == QFileInfo(outputPath).absoluteFilePath()) {
            return true;
        }
        QFile::remove(outputPath);
        return QFile::copy(parts.first(), outputPath);
    }
    if (!QDir().mkpath(QFileInfo(outputPath).absolutePath())) {
        if (error) {
            *error = QStringLiteral("无法创建音频目录：%1")
                         .arg(QFileInfo(outputPath).absolutePath());
        }
        return false;
    }
    QStringList args = {
        QStringLiteral("-y"),
        QStringLiteral("-nostdin"),
        QStringLiteral("-hide_banner"),
        QStringLiteral("-loglevel"),
        QStringLiteral("error"),
    };
    for (const QString &part : parts) {
        args << QStringLiteral("-i") << part;
    }
    QString filter;
    for (int index = 0; index < parts.size(); ++index) {
        filter += QStringLiteral("[%1:a]").arg(index);
    }
    filter += QStringLiteral("concat=n=%1:v=0:a=1[out]").arg(parts.size());
    args << QStringLiteral("-filter_complex") << filter
         << QStringLiteral("-map") << QStringLiteral("[out]")
         << QStringLiteral("-c:a") << QStringLiteral("libmp3lame")
         << QStringLiteral("-b:a") << QStringLiteral("128k")
         << QStringLiteral("-ar") << QStringLiteral("24000")
         << QStringLiteral("-ac") << QStringLiteral("1")
         << outputPath;
    return runFfmpeg(args, 10 * 60 * 1000, error);
}

int localQualityScore(const VisualFeatures &features, int width, int height,
                      bool video)
{
    // 亮度最舒服的区间是 0.35–0.75；过暗或过曝都扣分。
    const double brightnessPenalty =
        qBound(0.0, std::abs(features.brightness - 0.55) - 0.20, 1.0) * 55.0;
    const double detail = qBound(0.0, features.detail, 1.0) * 28.0;
    const double saturation =
        (1.0 - std::abs(features.saturation - 0.42)) * 12.0;
    const double contrast = qBound(0.0, features.contrast, 1.0) * 8.0;
    const int minSide = qMin(width, height);
    const double resolutionBonus =
        minSide >= 1080 ? 8.0 : (minSide >= 720 ? 4.0 : 0.0);
    // 视频有轻微运动更生动；完全静止的图片素材不加分。
    const double motionBonus = video ? features.motion * 5.0 : 0.0;

    double score = 42.0 + detail + saturation + contrast + resolutionBonus
        + motionBonus - brightnessPenalty;
    return qBound(10, qRound(score), 98);
}

QStringList localSceneTags(const QString &fileName, MaterialKind kind,
                           const VisualFeatures &features)
{
    QStringList tags;
    tags.append(kind == MaterialKind::Video ? QStringLiteral("视频镜头")
                                            : QStringLiteral("图片素材"));
    if (features.brightness >= 0.68) {
        tags.append(QStringLiteral("明亮"));
    } else if (features.brightness <= 0.28) {
        tags.append(QStringLiteral("暗调"));
    }
    if (features.saturation >= 0.55) {
        tags.append(QStringLiteral("色彩浓"));
    } else if (features.saturation <= 0.16) {
        tags.append(QStringLiteral("低饱和"));
    }
    if (features.detail >= 0.42) {
        tags.append(QStringLiteral("细节丰富"));
    }
    if (features.motion >= 0.30) {
        tags.append(QStringLiteral("动态"));
    }

    const QString stem = QFileInfo(fileName).completeBaseName();
    const QStringList parts =
        stem.split(QRegularExpression(QStringLiteral("[_\\-\\s.]+")),
                   Qt::SkipEmptyParts);
    for (const QString &part : parts) {
        if (part.size() < 2 || part.size() > 12 || tags.contains(part)) {
            continue;
        }
        // 纯数字或版本号不作为标签。
        if (part.toInt() > 0) {
            continue;
        }
        tags.append(part);
        if (tags.size() >= 6) {
            break;
        }
    }
    return tags;
}

} // namespace Mixcut
