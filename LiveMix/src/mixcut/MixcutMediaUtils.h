#pragma once

// 文件作用：智能混剪专用的本地媒体工具。
// 在智能口播已验证的素材探测 / 关键帧 / 画布预处理基础上，补充：
// - 场景切换检测与视频切片（第一步「素材智能整理」）；
// - 指定时间点抽帧（每个场景切片的关键帧）；
// - 多段 TTS 音频拼接（长文案分段合成后合并成一条配音）。

#include "MixcutTypes.h"

#include <QString>
#include <QStringList>
#include <QVector>

namespace Mixcut {

// 检测视频的场景切换点，返回素材内绝对毫秒（不含 0 与片尾）。
// 使用缩小后的画面做 scene 滤镜检测，兼顾准确率与速度。
bool detectSceneCuts(const QString &videoPath, qint64 durationMs,
                     QVector<qint64> *cuts, QString *error);

// 把按场景切出来的原始区间整理成可剪辑的镜头：
// - 过短镜头（< 1.2 秒）并入相邻镜头；
// - 镜头数超过 maxClips 时合并最短板，控制大模型与预处理成本。
QVector<QPair<qint64, qint64>>
normalizeSceneRanges(const QVector<qint64> &cuts, qint64 durationMs,
                     int maxClips = 8);

// 在视频的指定时间点抽一帧 JPG；用于每个镜头的大模型画面理解。
bool extractFrameAt(const QString &videoPath, qint64 positionMs,
                    const QString &outputPath, QString *error);

// 把多段音频按顺序拼接成一条 MP3（24kHz 单声道），用于分段 TTS。
bool concatAudio(const QStringList &parts, const QString &outputPath,
                 QString *error);

// 本地画面质量评分（0–100）：亮度是否适中、细节密度、饱和度与分辨率。
// 在大模型不可用或没有返回质量分时兜底。
int localQualityScore(const VisualFeatures &features, int width, int height,
                      bool video);

// 场景切换点对应的镜头标签（无模型时的本地兜底）。
QStringList localSceneTags(const QString &fileName, MaterialKind kind,
                           const VisualFeatures &features);

} // namespace Mixcut
