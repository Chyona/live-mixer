#pragma once

// 文件作用：导出成片任务（工作线程）。
// 调用剪映小助手 gen_video 云端合成 MP4，轮询 gen_video_status 获取进度，
// 完成后把成片下载到设置中的成片导出目录。
// 下载失败时保留已生成的 video_url，重试可直接续传而不重新合成。

#include "VideoExportJob.h"

#include <QElapsedTimer>
#include <QObject>
#include <QString>

#include <atomic>

class VideoExporter : public QObject
{
    Q_OBJECT

public:
    VideoExporter(qint64 draftRowId, const QString &title, const QString &draftName,
                  const QString &durationText, const QString &draftUrl,
                  qint64 durationMs, const QString &apiKey, const QString &exportDir,
                  QObject *parent = nullptr);

    // 续传参数：videoUrl 非空时跳过云端合成直接下载；baseElapsedMs 用于界面计时。
    void setResume(const QString &videoUrl, qint64 baseElapsedMs);

public slots:
    void run();
    // 线程安全：只设置取消标记，由轮询与下载循环自行退出。
    void cancel();

signals:
    void jobChanged(const VideoExportJob &job);
    void finished(bool success, const QString &message, const QString &outputPath);
    // 用户主动取消，与导出失败区分开。
    void cancelled();

private:
    void report(bool force = false);
    void fail(const QString &message);
    bool sleepInterruptible(int milliseconds);
    qint64 estimatedFileSizeBytes() const;
    qint64 estimatedDurationMs() const;
    QString targetPath() const;

    qint64 m_draftRowId = -1;
    QString m_title;
    QString m_draftName;
    QString m_durationText;
    QString m_draftUrl;
    qint64 m_durationMs = 0;
    QString m_apiKey;
    QString m_exportDir;
    QString m_resumeVideoUrl;
    qint64 m_baseElapsedMs = 0;

    VideoExportJob m_job;
    std::atomic_bool m_cancelled{false};

    QElapsedTimer m_clock;
    qint64 m_lastReportMs = -1;
    qint64 m_lastSampleMs = -1;
    qint64 m_lastSampleBytes = 0;
    double m_speed = 0.0;
};
