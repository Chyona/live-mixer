#pragma once

// 文件作用：智能口播的本地媒体工具。
// 负责素材探测、关键帧抽取、画面特征计算，以及把 B-Roll 预处理成画布尺寸。
// 这些函数只在智能口播流水线内部使用，不修改直播切片相关代码。

#include "OralTypes.h"

#include <QString>
#include <QStringList>

namespace Oral {

// 读取图片尺寸；失败时返回 false。
bool probeImage(const QString &path, int *width, int *height, QString *error);

// 读取视频尺寸与时长（毫秒）。
bool probeVideo(const QString &path, int *width, int *height,
                qint64 *durationMs, QString *error);

// 抽取用于素材理解的关键帧。图片直接返回原路径；视频抽取 3 帧写入 workDir。
bool extractKeyframes(const QString &path, MaterialKind kind, qint64 durationMs,
                      const QString &workDir, QStringList *frames, QString *error);

// 抽取口播视频的第一帧，作为封面图生图的参考图（原始分辨率 JPEG）。
bool extractFirstFrame(const QString &videoPath, const QString &outputPath,
                       QString *error);

// 从关键帧计算本地视觉特征，作为大模型理解失败时的兜底信息。
VisualFeatures analyzeVisualFeatures(const QStringList &frames, bool video);

// 把图片按「等比铺满 + 居中裁剪」处理成画布尺寸，保存为 JPG。
bool prepareCanvasImage(const QString &inputPath, int width, int height,
                        const QString &outputPath, QString *error);

// 把视频素材截取指定时长，并缩放/裁剪成画布尺寸；输出 H.264/AAC 兼容 MP4。
// 素材比镜头短时自动循环（-stream_loop）。
bool prepareCanvasVideo(const QString &inputPath, qint64 sourceInMs,
                        qint64 durationMs, int width, int height, int fps,
                        const QString &outputPath, QString *error);

// 计算把源画面等比铺满画布所需的最小缩放比例（1.0 表示原尺寸）。
double coverScale(int sourceWidth, int sourceHeight, int canvasWidth,
                  int canvasHeight);

// 文件大小展示：1.2 MB / 980 KB。
QString formatFileSize(qint64 bytes);

// 读取成片的首条视频流编码名，转换为展示用名称（H.264 / H.265 / AV1）。
// 读取失败返回空字符串，由界面使用默认文案。
QString videoCodecName(const QString &path);

} // namespace Oral
