#pragma once

// 文件作用：导出成片（MP4）任务的可观察状态快照。
// VideoExporter 在工作线程中维护这份快照，通过 jobChanged 发给界面；
// 结果页卡片、导出弹窗与底部状态栏读取同一份数据，保证进度一致。

#include <QMetaType>
#include <QString>
#include <QVector>
#include <QtGlobal>

enum class VideoExportStatus {
    Running,
    Done,
    Error,
};

enum class VideoExportStage {
    Compose,  // 云端合成画面
    Encode,   // 编码成片
    Download, // 下载到本地
    Done,
};

struct VideoExportJob
{
    qint64 draftRowId = -1;
    QString title;
    QString draftName;
    QString rangeText;    // 成片起点，如 00:12:04
    QString durationText; // 成片时长，如 61s
    QString draftUrl;
    // 云端渲染完成后返回的成片地址；下载失败重试时可直接续传。
    QString videoUrl;
    QString outputPath;

    VideoExportStatus status = VideoExportStatus::Running;
    VideoExportStage stage = VideoExportStage::Compose;
    double progress = 0.0;    // 0–100
    qint64 elapsedMs = 0;     // 实际已用时间
    qint64 expectedMs = 0;    // 预计总用时（用于界面估算）
    int etaSeconds = -1;      // 预计还需；-1 表示未知
    int framesDone = 0;
    int framesTotal = 0;
    int segmentsDone = 0;
    int segmentsTotal = 3;
    qint64 downloadedBytes = 0;
    qint64 fileSizeBytes = 0; // 下载前为估算值，完成后为真实大小
    double speedBytesPerSecond = 0.0;
    QString error;

    bool isRunning() const { return status == VideoExportStatus::Running; }
};

// -----------------------------------------------------------------------------
// 批量导出成片
// -----------------------------------------------------------------------------

enum class VideoBatchItemStatus {
    Queued,
    Running,
    Done,
    Error,
};

struct VideoBatchItem
{
    qint64 draftRowId = -1;
    QString title;
    QString durationText;
    VideoBatchItemStatus status = VideoBatchItemStatus::Queued;
    VideoExportStage stage = VideoExportStage::Compose;
    bool hasStage = false; // 排队中的条目还没有阶段
    double progress = 0.0;
    qint64 fileSizeBytes = 0;
    qint64 downloadedBytes = 0;
    qint64 elapsedMs = 0;
    qint64 expectedMs = 0;
    QString error;
};

struct VideoBatchSnapshot
{
    int id = 0;
    bool running = false;
    QVector<VideoBatchItem> items;
    int doneCount = 0;
    int failedCount = 0;
    int activeCount = 0;
    int queuedCount = 0;
    double progress = 0.0;
    qint64 totalBytes = 0;
    qint64 downloadedBytes = 0;
    qint64 elapsedMs = 0;
    int etaSeconds = -1;
};

Q_DECLARE_METATYPE(VideoExportJob)
Q_DECLARE_METATYPE(VideoBatchSnapshot)
