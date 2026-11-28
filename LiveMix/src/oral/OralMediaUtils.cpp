// 文件作用：智能口播媒体工具实现。

#include "OralMediaUtils.h"

#include "../media/FfmpegLocator.h"
#include "../media/FfmpegUtils.h"

#include <QColor>
#include <QDir>
#include <QFileInfo>
#include <QHash>
#include <QImage>
#include <QImageReader>
#include <QProcess>
#include <QRgb>
#include <QtMath>

#include <algorithm>
#include <cmath>

namespace {

// 图像采样时的最大边长，够用又不会让特征计算变慢。
constexpr int kSampleSize = 64;

QImage loadImageForAnalysis(const QString &path)
{
    QImageReader reader(path);
    reader.setAutoTransform(true);
    QSize size = reader.size();
    if (size.isValid()) {
        size.scale(kSampleSize, kSampleSize, Qt::KeepAspectRatio);
        reader.setScaledSize(size);
    }
    return reader.read().convertToFormat(QImage::Format_RGB32);
}

double lumaOf(QRgb pixel)
{
    return (0.2126 * qRed(pixel) + 0.7152 * qGreen(pixel)
            + 0.0722 * qBlue(pixel))
        / 255.0;
}

// 计算平均亮度、饱和度、对比度与细节密度；视频额外计算帧间运动量。
Oral::VisualFeatures featuresFromImages(const QVector<QImage> &images)
{
    Oral::VisualFeatures features;
    if (images.isEmpty()) {
        return features;
    }
    const QImage &first = images.first();
    if (first.isNull()) {
        return features;
    }

    double lumaSum = 0.0;
    double saturationSum = 0.0;
    double lumaSquareSum = 0.0;
    double gradientSum = 0.0;
    qint64 count = 0;
    QHash<QRgb, int> colorBuckets;

    for (int y = 0; y < first.height(); ++y) {
        for (int x = 0; x < first.width(); ++x) {
            const QRgb pixel = first.pixel(x, y);
            const double luma = lumaOf(pixel);
            const QColor color(pixel);
            lumaSum += luma;
            lumaSquareSum += luma * luma;
            saturationSum += color.saturationF();
            ++count;
            if (x + 1 < first.width()) {
                gradientSum += std::abs(luma - lumaOf(first.pixel(x + 1, y)));
            }
            // 每 4 个像素采样一次颜色，避免统计表过大。
            if ((x + y) % 4 == 0) {
                const int r = (qRed(pixel) >> 5) << 5;
                const int g = (qGreen(pixel) >> 5) << 5;
                const int b = (qBlue(pixel) >> 5) << 5;
                colorBuckets[qRgb(r, g, b)] += 1;
            }
        }
    }
    if (count > 0) {
        const double mean = lumaSum / count;
        features.brightness = qBound(0.0, mean, 1.0);
        features.saturation = qBound(0.0, saturationSum / count, 1.0);
        features.contrast = qBound(
            0.0, std::sqrt(qMax(0.0, lumaSquareSum / count - mean * mean)), 1.0);
        features.detail = qBound(0.0, gradientSum / count * 4.0, 1.0);
    }

    // 取出现次数最多的 3 个颜色桶，作为「主色」提供给大模型。
    QVector<QPair<int, QRgb>> sortedColors;
    for (auto it = colorBuckets.constBegin(); it != colorBuckets.constEnd(); ++it) {
        sortedColors.append({it.value(), it.key()});
    }
    std::sort(sortedColors.begin(), sortedColors.end(),
              [](const QPair<int, QRgb> &left, const QPair<int, QRgb> &right) {
                  return left.first > right.first;
              });
    QStringList colors;
    for (const auto &entry : sortedColors) {
        const QString hex = QColor(entry.second).name();
        if (!colors.contains(hex)) {
            colors.append(hex);
        }
        if (colors.size() >= 3) {
            break;
        }
    }
    while (colors.size() < 3) {
        colors.append(QStringLiteral("#000000"));
    }
    features.dominantColor1 = colors.at(0);
    features.dominantColor2 = colors.at(1);
    features.dominantColor3 = colors.at(2);

    // 运动量：两张关键帧的平均像素差。
    if (images.size() > 1) {
        const QImage &second = images.at(1);
        if (!second.isNull() && second.size() == first.size()) {
            double diffSum = 0.0;
            qint64 diffCount = 0;
            for (int y = 0; y < first.height(); y += 2) {
                for (int x = 0; x < first.width(); x += 2) {
                    const QRgb a = first.pixel(x, y);
                    const QRgb b = second.pixel(x, y);
                    diffSum += (std::abs(qRed(a) - qRed(b))
                                + std::abs(qGreen(a) - qGreen(b))
                                + std::abs(qBlue(a) - qBlue(b)))
                        / (3.0 * 255.0);
                    ++diffCount;
                }
            }
            if (diffCount > 0) {
                features.motion = qBound(0.0, diffSum / diffCount * 3.0, 1.0);
            }
        }
    }
    features.keyframeCount = images.size();
    return features;
}

// 统一的 ffmpeg 执行封装；只允许由本文件的预处理流程调用。
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
            *error = detail.isEmpty()
                ? QStringLiteral("ffmpeg 执行失败。")
                : detail.right(600);
        }
        return false;
    }
    return true;
}

} // namespace

namespace Oral {

bool probeImage(const QString &path, int *width, int *height, QString *error)
{
    QImageReader reader(path);
    reader.setAutoTransform(true);
    const QSize size = reader.size();
    if (!reader.canRead() || !size.isValid()) {
        if (error) {
            *error = QStringLiteral("无法读取图片：%1").arg(QFileInfo(path).fileName());
        }
        return false;
    }
    if (width) {
        *width = size.width();
    }
    if (height) {
        *height = size.height();
    }
    return true;
}

bool probeVideo(const QString &path, int *width, int *height, qint64 *durationMs,
                QString *error)
{
    int probedWidth = 0;
    int probedHeight = 0;
    if (!FfmpegUtils::videoSize(path, &probedWidth, &probedHeight)
        && (probedWidth <= 0 || probedHeight <= 0)) {
        if (error) {
            *error = QStringLiteral("无法读取视频尺寸：%1")
                         .arg(QFileInfo(path).fileName());
        }
        return false;
    }
    qint64 probedDuration = 0;
    if (!FfmpegUtils::mediaDurationMs(path, &probedDuration)
        || probedDuration <= 0) {
        if (error) {
            *error = QStringLiteral("无法读取视频时长：%1")
                         .arg(QFileInfo(path).fileName());
        }
        return false;
    }
    if (width) {
        *width = probedWidth;
    }
    if (height) {
        *height = probedHeight;
    }
    if (durationMs) {
        *durationMs = probedDuration;
    }
    return true;
}

bool extractKeyframes(const QString &path, MaterialKind kind, qint64 durationMs,
                      const QString &workDir, QStringList *frames, QString *error)
{
    if (!frames) {
        return false;
    }
    frames->clear();
    if (kind == MaterialKind::Image) {
        // 图片本身就是关键帧，视觉分析会再缩小读取。
        frames->append(path);
        return true;
    }
    if (!QDir().mkpath(workDir)) {
        if (error) {
            *error = QStringLiteral("无法创建关键帧目录：%1").arg(workDir);
        }
        return false;
    }
    const QString base = QFileInfo(path).completeBaseName().left(24);
    const QVector<double> ratios = {0.15, 0.5, 0.85};
    for (int index = 0; index < ratios.size(); ++index) {
        const qint64 position =
            qMax<qint64>(0, qRound64(durationMs * ratios.at(index)));
        const QString output = QDir(workDir).filePath(
            QStringLiteral("frame_%1_%2.jpg").arg(base).arg(index));
        QString frameError;
        if (FfmpegUtils::extractThumbnail(path, position, output, &frameError)) {
            frames->append(output);
        }
    }
    if (frames->isEmpty()) {
        if (error) {
            *error = QStringLiteral("无法抽取视频关键帧：%1")
                         .arg(QFileInfo(path).fileName());
        }
        return false;
    }
    return true;
}

bool extractFirstFrame(const QString &videoPath, const QString &outputPath,
                       QString *error)
{
    if (videoPath.trimmed().isEmpty()) {
        if (error) {
            *error = QStringLiteral("口播视频路径为空。");
        }
        return false;
    }
    if (!QDir().mkpath(QFileInfo(outputPath).absolutePath())) {
        if (error) {
            *error = QStringLiteral("无法创建封面帧目录：%1")
                         .arg(QFileInfo(outputPath).absolutePath());
        }
        return false;
    }
    // -ss 0 放在 -i 前，快速定位到片头；-frames:v 1 只保留首帧。
    const QStringList args = {
        QStringLiteral("-y"),
        QStringLiteral("-nostdin"),
        QStringLiteral("-hide_banner"),
        QStringLiteral("-loglevel"),
        QStringLiteral("error"),
        QStringLiteral("-ss"),
        QStringLiteral("0"),
        QStringLiteral("-i"),
        videoPath,
        QStringLiteral("-frames:v"),
        QStringLiteral("1"),
        QStringLiteral("-q:v"),
        QStringLiteral("2"),
        outputPath,
    };
    if (!runFfmpeg(args, 30000, error)) {
        return false;
    }
    if (!QFileInfo::exists(outputPath) || QFileInfo(outputPath).size() <= 0) {
        if (error) {
            *error = QStringLiteral("口播首帧为空。");
        }
        return false;
    }
    return true;
}

VisualFeatures analyzeVisualFeatures(const QStringList &frames, bool video)
{
    QVector<QImage> images;
    images.reserve(frames.size());
    for (const QString &frame : frames) {
        const QImage image = loadImageForAnalysis(frame);
        if (!image.isNull()) {
            images.append(image);
        }
        if (images.size() >= 2 && video) {
            // 视频只需要两帧计算运动量，第三帧对特征贡献有限。
            break;
        }
    }
    return featuresFromImages(images);
}

bool prepareCanvasImage(const QString &inputPath, int width, int height,
                        const QString &outputPath, QString *error)
{
    if (width <= 0 || height <= 0) {
        if (error) {
            *error = QStringLiteral("画布尺寸无效。");
        }
        return false;
    }
    QImageReader reader(inputPath);
    reader.setAutoTransform(true);
    QImage image = reader.read();
    if (image.isNull()) {
        if (error) {
            *error = QStringLiteral("无法读取图片：%1")
                         .arg(QFileInfo(inputPath).fileName());
        }
        return false;
    }
    // 等比铺满画布，再居中裁剪，避免出现黑边。
    image = image.scaled(width, height, Qt::KeepAspectRatioByExpanding,
                         Qt::SmoothTransformation);
    const int left = qMax(0, (image.width() - width) / 2);
    const int top = qMax(0, (image.height() - height) / 2);
    image = image.copy(left, top, qMin(width, image.width()),
                       qMin(height, image.height()));
    if (!QDir().mkpath(QFileInfo(outputPath).absolutePath())) {
        if (error) {
            *error = QStringLiteral("无法创建素材目录：%1")
                         .arg(QFileInfo(outputPath).absolutePath());
        }
        return false;
    }
    if (!image.save(outputPath, "JPG", 92)) {
        if (error) {
            *error = QStringLiteral("无法保存画布图片：%1").arg(outputPath);
        }
        return false;
    }
    return true;
}

bool prepareCanvasVideo(const QString &inputPath, qint64 sourceInMs,
                        qint64 durationMs, int width, int height, int fps,
                        const QString &outputPath, QString *error)
{
    if (durationMs <= 0 || width <= 0 || height <= 0) {
        if (error) {
            *error = QStringLiteral("视频片段参数无效。");
        }
        return false;
    }
    if (!QDir().mkpath(QFileInfo(outputPath).absolutePath())) {
        if (error) {
            *error = QStringLiteral("无法创建素材目录：%1")
                         .arg(QFileInfo(outputPath).absolutePath());
        }
        return false;
    }
    // -stream_loop -1 保证素材比镜头短时循环播放；缩放后居中裁剪，
    // 输出画布尺寸、无音轨的 H.264，B-Roll 不会盖掉口播人声。
    const QString filter = QStringLiteral(
                               "scale=%1:%2:force_original_aspect_ratio=increase,"
                               "crop=%1:%2,fps=%3")
                               .arg(width)
                               .arg(height)
                               .arg(qBound(1, fps, 60));
    const QStringList args = {
        QStringLiteral("-y"),
        QStringLiteral("-nostdin"),
        QStringLiteral("-hide_banner"),
        QStringLiteral("-loglevel"),
        QStringLiteral("error"),
        QStringLiteral("-stream_loop"),
        QStringLiteral("-1"),
        QStringLiteral("-i"),
        inputPath,
        QStringLiteral("-ss"),
        QString::number(qMax<qint64>(0, sourceInMs) / 1000.0, 'f', 3),
        QStringLiteral("-t"),
        QString::number(durationMs / 1000.0, 'f', 3),
        QStringLiteral("-an"),
        QStringLiteral("-vf"),
        filter,
        QStringLiteral("-c:v"),
        QStringLiteral("libx264"),
        QStringLiteral("-preset"),
        QStringLiteral("veryfast"),
        QStringLiteral("-crf"),
        QStringLiteral("20"),
        QStringLiteral("-pix_fmt"),
        QStringLiteral("yuv420p"),
        QStringLiteral("-movflags"),
        QStringLiteral("+faststart"),
        outputPath,
    };
    return runFfmpeg(args, 10 * 60 * 1000, error);
}

double coverScale(int sourceWidth, int sourceHeight, int canvasWidth,
                  int canvasHeight)
{
    if (sourceWidth <= 0 || sourceHeight <= 0 || canvasWidth <= 0
        || canvasHeight <= 0) {
        return 1.0;
    }
    return qMax(double(canvasWidth) / sourceWidth,
                double(canvasHeight) / sourceHeight);
}

QString formatFileSize(qint64 bytes)
{
    if (bytes <= 0) {
        return QStringLiteral("—");
    }
    const double mb = double(bytes) / (1024.0 * 1024.0);
    if (mb >= 1.0) {
        return QStringLiteral("%1 MB").arg(mb, 0, 'f', 1);
    }
    return QStringLiteral("%1 KB").arg(double(bytes) / 1024.0, 0, 'f', 0);
}

QString videoCodecName(const QString &path)
{
    const QString ffprobe = FfmpegLocator::ffprobePath();
    if (ffprobe.isEmpty() || path.isEmpty()) {
        return QString();
    }
    QProcess process;
    process.setProgram(ffprobe);
    process.setArguments({
        QStringLiteral("-v"), QStringLiteral("error"),
        QStringLiteral("-select_streams"), QStringLiteral("v:0"),
        QStringLiteral("-show_entries"), QStringLiteral("stream=codec_name"),
        QStringLiteral("-of"), QStringLiteral("default=nw=1:nk=1"),
        path,
    });
#ifdef Q_OS_WIN
    process.setCreateProcessArgumentsModifier(
        [](QProcess::CreateProcessArguments *args) {
            args->flags |= CREATE_NO_WINDOW;
        });
#endif
    process.start();
    if (!process.waitForStarted(3000) || !process.waitForFinished(15000)) {
        process.kill();
        return QString();
    }
    if (process.exitStatus() != QProcess::NormalExit || process.exitCode() != 0) {
        return QString();
    }
    const QString codec =
        QString::fromLocal8Bit(process.readAllStandardOutput()).trimmed().toLower();
    if (codec == QLatin1String("h264")) {
        return QStringLiteral("H.264");
    }
    if (codec == QLatin1String("hevc") || codec == QLatin1String("h265")) {
        return QStringLiteral("H.265");
    }
    if (codec == QLatin1String("av1")) {
        return QStringLiteral("AV1");
    }
    return codec.isEmpty() ? QString() : codec.toUpper();
}

} // namespace Oral
