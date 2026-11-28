#pragma once

// 文件作用：直播切片流程用到的 ffmpeg 操作：
// 1. 从视频/直播录像中提取标准 MP3 音频供 ASR 使用；
// 2. 按 LLM 给出的时间范围裁剪出成片片段。

#include <QPair>
#include <QString>
#include <QVector>

namespace FfmpegUtils {

// 提取单声道 16kHz、64kbps 的整段 MP3，兼顾语音识别效果与体积。
// 输出会补齐/截断到源视频时长（含音轨晚于视频起点时的开头静音），
// 保证 ASR 时间线与视频时间轴一致。
bool extractMp3(const QString &inputPath, const QString &outputPath, QString *error);

// 按绝对时间裁剪并转封装为 H.264/AAC MP4，保证剪映兼容。
// 变速不在这里烘焙：成片倍速由 add_videos 的 scene_timelines 写入草稿。
bool cutClip(const QString &inputPath, qint64 startMs, qint64 endMs,
             const QString &outputPath, QString *error);

// 按保留区间裁剪并拼接成一条连续 MP4（H.264/AAC）。
// 与直播切片一致采用「先定保留区间、再裁剪拼接」的删除方式；这里用一次
// filter_complex 完成，避免多段重复编码以及逐段音画累计漂移。
// ranges 为毫秒区间，内部会排序、合并重叠区间并忽略空区间。
bool cutKeepRanges(const QString &inputPath,
                   const QVector<QPair<qint64, qint64>> &ranges,
                   const QString &outputPath, QString *error);

// 保持画面不重新编码，只对音轨套用 ffmpeg 音频滤镜（智能口播用于降噪）。
// audioFilter 例如 "afftdn=nr=12:nf=-35"；画面沿用原编码，音轨转为 AAC。
bool applyAudioFilter(const QString &inputPath, const QString &audioFilter,
                      const QString &outputPath, QString *error);

// 录制目录中的完整 MP4 路径（与 index.m3u8 同级，固定为 index.mp4）。
// 输入不是 m3u8 时原样返回。
QString recordingMp4Path(const QString &playlistPath);

// 确保录制目录中存在完整 MP4：
// 1. index.mp4 已存在且完整时直接复用；
// 2. 可复用的视频预览完整缓存会被移动到 index.mp4；
// 3. 上次改名失败留下的 index-*.part.mp4 会先重试转正，避免重复转封装；
// 4. 否则无损合成（-c copy），校验时长后原子改名为 index.mp4；
//    改名失败时保留临时文件，重试只需重新改名。
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

// 成片首帧预览缓存路径：固定取 0 秒帧，与素材缩略图（0.5 秒）分开缓存。
QString posterFrameCachePath(const QString &videoPath);

// HLS 录像的完整 MP4 预览缓存路径；播放前会转封装整个录像。
QString hlsPreviewCachePath(const QString &videoPath);

// HLS 录像的开头预览缓存路径（约前 60 秒），先播放它以免等待完整转封装。
QString hlsPreviewHeadCachePath(const QString &videoPath);

} // namespace FfmpegUtils
