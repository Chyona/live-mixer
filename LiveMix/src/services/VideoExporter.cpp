// 文件作用：导出成片任务实现（gen_video / gen_video_status / 下载）。
#include "VideoExporter.h"

#include "../api/CapCutMateClient.h"
#include "../api/HttpClient.h"
#include "../core/MediaUtils.h"

#include <QDir>
#include <QFileInfo>
#include <QThread>
#include <QUrl>

#include <cmath>

namespace {

constexpr int kPollIntervalMs = 3000;
constexpr int kMaxPolls = 600; // 约 30 分钟
constexpr int kReportIntervalMs = 100;
constexpr double kComposeEnd = 60.0;
constexpr double kEncodeEnd = 86.0;
constexpr double kExportFileBytesPerSecond = 0.42 * 1024.0 * 1024.0;
constexpr int kFps = 30;
constexpr int kSegments = 3;

} // namespace

VideoExporter::VideoExporter(qint64 draftRowId, const QString &title,
                             const QString &draftName, const QString &durationText,
                             const QString &draftUrl, qint64 durationMs,
                             const QString &apiKey, const QString &exportDir,
                             QObject *parent)
    : QObject(parent)
    , m_draftRowId(draftRowId)
    , m_title(title)
    , m_draftName(draftName)
    , m_durationText(durationText)
    , m_draftUrl(draftUrl)
    , m_durationMs(qMax<qint64>(0, durationMs))
    , m_apiKey(apiKey)
    , m_exportDir(exportDir)
{
}

void VideoExporter::setResume(const QString &videoUrl, qint64 baseElapsedMs)
{
    m_resumeVideoUrl = videoUrl;
    m_baseElapsedMs = qMax<qint64>(0, baseElapsedMs);
}

void VideoExporter::cancel()
{
    m_cancelled.store(true);
}

qint64 VideoExporter::estimatedFileSizeBytes() const
{
    const qint64 seconds = qMax<qint64>(1, m_durationMs / 1000);
    return qMax<qint64>(1024 * 1024,
                        qint64(seconds * kExportFileBytesPerSecond));
}

qint64 VideoExporter::estimatedDurationMs() const
{
    // 与设计稿一致的 2:08–2:35 量级，随草稿序号略有差异。
    return 128000 + (m_draftRowId % 4) * 9000;
}

QString VideoExporter::targetPath() const
{
    QString name = MediaUtils::sanitizeFileComponent(
        m_title.isEmpty() ? QStringLiteral("成片") : m_title, 80);
    if (name.isEmpty()) {
        name = QStringLiteral("成片");
    }
    return QDir(m_exportDir).filePath(name + QStringLiteral(".mp4"));
}

void VideoExporter::report(bool force)
{
    const qint64 now = m_clock.isValid() ? m_clock.elapsed() : 0;
    if (!force && m_lastReportMs >= 0 && now - m_lastReportMs < kReportIntervalMs) {
        return;
    }
    m_lastReportMs = now;

    m_job.elapsedMs = m_baseElapsedMs + now;
    if (m_job.status == VideoExportStatus::Running) {
        if (m_job.stage == VideoExportStage::Download && m_speed > 0.0
            && m_job.fileSizeBytes > m_job.downloadedBytes) {
            const qint64 remaining = m_job.fileSizeBytes - m_job.downloadedBytes;
            m_job.etaSeconds = qMax(1, int(std::ceil(remaining / m_speed)));
        } else if (m_job.expectedMs > m_job.elapsedMs) {
            m_job.etaSeconds =
                qMax(1, int((m_job.expectedMs - m_job.elapsedMs + 999) / 1000));
        } else {
            m_job.etaSeconds = -1;
        }
    } else {
        m_job.etaSeconds = -1;
    }

    emit jobChanged(m_job);
}

void VideoExporter::fail(const QString &message)
{
    m_job.status = VideoExportStatus::Error;
    m_job.error = message;
    m_job.speedBytesPerSecond = 0.0;
    m_job.etaSeconds = -1;
    report(true);
    emit finished(false, message, m_job.outputPath);
}

bool VideoExporter::sleepInterruptible(int milliseconds)
{
    int remaining = milliseconds;
    while (remaining > 0) {
        if (m_cancelled.load()) {
            return false;
        }
        const int slice = qMin(remaining, 100);
        QThread::msleep(static_cast<unsigned long>(slice));
        remaining -= slice;
    }
    return !m_cancelled.load();
}

void VideoExporter::run()
{
    m_job = VideoExportJob();
    m_job.draftRowId = m_draftRowId;
    m_job.title = m_title.isEmpty() ? QStringLiteral("直播切片") : m_title;
    m_job.draftName = m_draftName;
    m_job.draftUrl = m_draftUrl;
    m_job.durationText = m_durationText;
    m_job.outputPath = targetPath();
    m_job.expectedMs = estimatedDurationMs();
    m_job.fileSizeBytes = estimatedFileSizeBytes();
    m_job.framesTotal =
        qMax(1, int(qMax<qint64>(1, m_durationMs / 1000) * kFps));
    m_job.segmentsTotal = kSegments;
    m_job.status = VideoExportStatus::Running;
    m_job.stage = VideoExportStage::Compose;

    m_clock.start();
    m_lastReportMs = -1;
    m_lastSampleMs = -1;
    m_lastSampleBytes = 0;
    m_speed = 0.0;
    report(true);

    if (m_draftUrl.trimmed().isEmpty()) {
        fail(QStringLiteral("草稿记录缺少 draft_url，无法导出成片。"));
        return;
    }
    if (m_exportDir.trimmed().isEmpty()) {
        fail(QStringLiteral("尚未配置成片导出目录，请到「设置 → 存储与输出」填写。"));
        return;
    }
    if (!QDir().mkpath(m_exportDir)) {
        fail(QStringLiteral("无法创建成片导出目录：%1").arg(m_exportDir));
        return;
    }

    HttpClient http;
    CapCutMateClient capcut(&http);

    QString videoUrl = m_resumeVideoUrl;

    if (videoUrl.isEmpty()) {
        if (!capcut.genVideo(m_draftUrl, m_apiKey, &m_job.error)) {
            fail(m_job.error.isEmpty() ? QStringLiteral("提交云端合成失败")
                                       : m_job.error);
            return;
        }

        bool completed = false;
        for (int poll = 0; poll < kMaxPolls; ++poll) {
            if (!sleepInterruptible(poll == 0 ? 1500 : kPollIntervalMs)) {
                emit cancelled();
                return;
            }

            CapCutVideoStatus status;
            QString error;
            if (!capcut.genVideoStatus(m_draftUrl, &status, &error)) {
                fail(error.isEmpty() ? QStringLiteral("查询成片合成状态失败")
                                     : error);
                return;
            }

            const QString state = status.status;
            const int remote = qBound(0, status.progress, 100);

            if (state == QLatin1String("completed")) {
                videoUrl = status.videoUrl;
                if (videoUrl.isEmpty()) {
                    fail(QStringLiteral("云端合成完成，但未返回成片地址。"));
                    return;
                }
                completed = true;
                break;
            }
            if (state == QLatin1String("failed")) {
                fail(status.errorMessage.isEmpty()
                         ? QStringLiteral("云端合成失败")
                         : status.errorMessage);
                return;
            }
            if (!state.isEmpty() && state != QLatin1String("pending")
                && state != QLatin1String("processing")) {
                fail(QStringLiteral("云端合成返回未知状态：%1").arg(state));
                return;
            }

            const double overall = remote * (kEncodeEnd / 100.0);
            if (overall < kComposeEnd) {
                m_job.stage = VideoExportStage::Compose;
                m_job.framesDone = int(
                    m_job.framesTotal * qMin(1.0, remote / 70.0));
                m_job.segmentsDone = 0;
            } else {
                m_job.stage = VideoExportStage::Encode;
                m_job.framesDone = m_job.framesTotal;
                m_job.segmentsDone = qMin(
                    kSegments,
                    int(((overall - kComposeEnd) / (kEncodeEnd - kComposeEnd))
                        * kSegments));
            }
            m_job.progress = qMin(85.9, overall);
            m_job.videoUrl = status.videoUrl;
            report();
        }

        if (!completed) {
            fail(QStringLiteral("云端合成超时，请稍后重试。"));
            return;
        }
    }

    if (m_cancelled.load()) {
        emit cancelled();
        return;
    }

    // 下载到本地：前端 86%–100% 的进度对应文件下载。
    m_job.videoUrl = videoUrl;
    m_job.stage = VideoExportStage::Download;
    m_job.progress = kEncodeEnd;
    m_job.framesDone = m_job.framesTotal;
    m_job.segmentsDone = kSegments;
    m_job.downloadedBytes = 0;
    m_job.speedBytesPerSecond = 0.0;
    m_speed = 0.0;
    m_lastSampleMs = m_clock.elapsed();
    m_lastSampleBytes = 0;
    report(true);

    const QUrl url(videoUrl);
    QString error;
    const bool ok = http.downloadToFile(
        url, m_job.outputPath, &error, 60 * 60 * 1000,
        [this](qint64 received, qint64 total) {
            m_job.downloadedBytes = qMax<qint64>(0, received);
            if (total > 0) {
                m_job.fileSizeBytes = total;
            }
            const qint64 now = m_clock.elapsed();
            if (m_lastSampleMs >= 0 && now - m_lastSampleMs >= 250) {
                const qint64 deltaBytes = received - m_lastSampleBytes;
                const qint64 deltaMs = now - m_lastSampleMs;
                if (deltaMs > 0) {
                    m_speed = deltaBytes * 1000.0 / deltaMs;
                    m_job.speedBytesPerSecond = m_speed;
                }
                m_lastSampleMs = now;
                m_lastSampleBytes = received;
            }
            const qint64 totalBytes =
                m_job.fileSizeBytes > 0 ? m_job.fileSizeBytes : 0;
            const double fraction = totalBytes > 0
                ? qBound(0.0, double(received) / totalBytes, 1.0)
                : 0.0;
            m_job.progress = totalBytes > 0
                ? kEncodeEnd + fraction * (100.0 - kEncodeEnd)
                : 92.0;
            report();
        },
        [this] { return m_cancelled.load(); });

    if (m_cancelled.load()) {
        emit cancelled();
        return;
    }
    if (!ok) {
        m_job.speedBytesPerSecond = 0.0;
        fail(error.isEmpty() ? QStringLiteral("成片下载失败") : error);
        return;
    }

    const qint64 actualBytes = QFileInfo(m_job.outputPath).size();
    if (actualBytes > 0) {
        m_job.fileSizeBytes = actualBytes;
        m_job.downloadedBytes = actualBytes;
    } else {
        m_job.downloadedBytes = m_job.fileSizeBytes;
    }
    m_job.status = VideoExportStatus::Done;
    m_job.stage = VideoExportStage::Done;
    m_job.progress = 100.0;
    m_job.speedBytesPerSecond = 0.0;
    m_job.error.clear();
    report(true);
    emit finished(true, QStringLiteral("成片已导出"), m_job.outputPath);
}
