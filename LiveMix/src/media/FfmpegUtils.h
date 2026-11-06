#pragma once

// 文件作用：直播切片流程用到的 ffmpeg 操作：
// 1. 从视频/直播录像中提取标准 MP3 音频供 ASR 使用；
// 2. 按 LLM 给出的时间范围裁剪出成片片段。

#include <QString>

namespace FfmpegUtils {

// 提取单声道 16kHz、64kbps 的整段 MP3，兼顾语音识别效果与体积。
// 输出会补齐/截断到源视频时长（含音轨晚于视频起点时的开头静音），
// 保证 ASR 时间线与视频时间轴一致。
bool extractMp3(const QString &inputPath, const QString &outputPath, QString *error);

// 按绝对时间裁剪并转封装为 H.264/AAC MP4，保证剪映兼容。
bool cutClip(const QString &inputPath, qint64 startMs, qint64 endMs,
             const QString &outputPath, QString *error);

// 录制目录中的完整 MP4 路径（与 index.m3u8 同级，固定为 index.mp4）。
// 输入不是 m3u8 时原样返回。
QString recordingMp4Path(const QString &playlistPath);

// 确保录制目录中存在完整 MP4：
// 1. index.mp4 已存在且完整时直接复用；
// 2. 可复用的视频预览完整缓存会被移动到 index.mp4；
// 3. 否则无损合成（-c copy），原子改名后供预览与切片使用。
bool ensureRecordingMp4(const QString &playlistPath, QString *mp4Path, QString *error);

// 读取视频宽高；失败时返回 false，由调用方使用默认分辨率。
bool videoSize(const QString &filePath, int *width, int *height);

// 读取任意媒体的总时长（毫秒）。
bool mediaDurationMs(const QString &filePath, qint64 *durationMs);

// 截取一帧作为本地导入弹窗的视频预览图。
bool extractThumbnail(const QString &inputPath, qint64 positionMs,
                      const QString &outputPath, QString *error);

// 视频封面图缓存路径：按“路径 + 修改时间”生成，导入弹窗与视频库卡片共用。
QString thumbnailCachePath(const QString &videoPath);

// HLS 录像的完整 MP4 预览缓存路径；播放前会转封装整个录像。
QString hlsPreviewCachePath(const QString &videoPath);

// HLS 录像的开头预览缓存路径（约前 60 秒），先播放它以免等待完整转封装。
QString hlsPreviewHeadCachePath(const QString &videoPath);

} // namespace FfmpegUtils
