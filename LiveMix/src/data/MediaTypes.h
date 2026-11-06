#pragma once

#include <QString>
#include <QtGlobal>

enum class SourceKind {
    Online,
    Local,
};

enum class SourceStatus {
    Recording,
    Ready,
    Failed,
};

enum class SessionStatus {
    Recording,
    Completed,
    Failed,
    Cancelled,
};

struct MediaSource
{
    qint64 id = -1;
    QString title;
    QString sourceUri;
    QString localPath;
    QString note;
    SourceKind kind = SourceKind::Online;
    SourceStatus status = SourceStatus::Failed;
    qint64 durationMs = 0;
    qint64 sizeBytes = 0;
    QString probeJson;
    QString errorMessage;
    qint64 createdAtMs = 0;
    qint64 updatedAtMs = 0;
    // 该源视频已生成的剪映草稿数；>0 时视频库卡片显示「查看草稿」。
    int draftCount = 0;

    bool isRecording() const { return status == SourceStatus::Recording; }
    bool isReady() const { return status == SourceStatus::Ready; }
    bool isFailed() const { return status == SourceStatus::Failed; }

    // 在线源有两种落盘方式：mp4 直链直接下载原文件；直播/HLS 仍录制为
    // index.m3u8。视频库据此区分「在线下载」与「直播录制」。
    bool isOnlineDownload() const
    {
        return kind == SourceKind::Online && !localPath.isEmpty()
            && !localPath.endsWith(QLatin1String(".m3u8"), Qt::CaseInsensitive);
    }
};

struct RecordingSession
{
    qint64 id = -1;
    qint64 sourceId = -1;
    QString outputPath;
    QString container = QStringLiteral("hls");
    SessionStatus status = SessionStatus::Recording;
    qint64 startedAtMs = 0;
    qint64 finishedAtMs = 0;
    qint64 durationMs = 0;
    qint64 sizeBytes = 0;
    QString errorMessage;
};

struct ProbeInfo
{
    bool ok = false;
    qint64 durationMs = 0;
    qint64 sizeBytes = 0;
    QString json;
    QString error;
};
