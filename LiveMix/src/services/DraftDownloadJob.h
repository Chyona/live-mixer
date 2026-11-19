#pragma once

// 文件作用：剪映草稿下载任务的可观察状态快照。
// DraftDownloader 在工作线程中维护这份快照，通过 jobChanged 信号发给界面；
// 结果页卡片、下载弹窗与底部状态栏读取同一份数据，保证进度一致。

#include <QMetaType>
#include <QString>
#include <QVector>
#include <QtGlobal>

enum class DraftDownloadStatus {
    Downloading,
    Writing,
    Done,
    Error,
};

enum class DraftDownloadStage {
    Config,
    Assets,
    Writing,
    Done,
};

enum class DraftFileKind {
    Config,
    Video,
    Audio,
    Image,
};

enum class DraftFileStatus {
    Queued,
    Downloading,
    Done,
    Error,
};

struct DraftFileProgress
{
    QString path; // 相对草稿目录的路径
    DraftFileKind kind = DraftFileKind::Config;
    DraftFileStatus status = DraftFileStatus::Queued;
    qint64 loadedBytes = 0;
    qint64 totalBytes = -1; // 未知时为 -1
    int progress = 0;       // 0–100
};

struct DraftDownloadJob
{
    qint64 draftRowId = -1;
    QString title;
    QString draftName;
    QString rangeText;    // 成片起点，如 00:12:04
    QString durationText; // 成片时长，如 61s

    DraftDownloadStatus status = DraftDownloadStatus::Downloading;
    DraftDownloadStage stage = DraftDownloadStage::Config;
    double progress = 0.0; // 分阶段加权，0–100
    qint64 loadedBytes = 0;
    qint64 totalBytes = 0;
    int doneFiles = 0;
    int totalFiles = 0;
    double speed = 0.0; // 字节/秒
    int etaSeconds = -1;
    QString error;
    QString localPath;

    QVector<DraftFileProgress> files;

    bool isRunning() const
    {
        return status == DraftDownloadStatus::Downloading
            || status == DraftDownloadStatus::Writing;
    }
};

Q_DECLARE_METATYPE(DraftDownloadJob)
