#pragma once

// 文件作用：直播切片流水线（工作线程任务）。
// 流程：HLS 录制源先合成完整 MP4（已存在则复用）→ 从 MP4 提取 MP3 →
// 整片提交一次 ASR（结果缓存 SQLite）→ 按转写时长均衡切成每段不超过
// 30 分钟的若干段调用 LLM 选片（返回句段索引）→ 合并相邻句段 →
// ffmpeg 裁剪 → 拼接到同一条剪映草稿。
// ASR 不再拆段；网关同步返回完整转写稿，等待过程由过程页按阶段与估算进度展示。
// 该对象会被移动到独立 QThread 中执行，通过信号把进度传回界面线程。

#include "../api/CapCutMateClient.h"
#include "../data/ClipTypes.h"
#include "../data/PipelineTypes.h"

#include <QObject>
#include <QString>
#include <QVector>

struct LiveClipRequest
{
    qint64 sourceId = -1;
    QString databasePath;
    QString apiKey;
    QString gatewayBaseUrl; // OpenAI 兼容网关，同时用于 ASR 与 LLM
    QString promptText;     // 选片系统提示词（内置提示词由后端导入）
    QString promptLabel;    // 过程页展示的提示词名称
    QString cacheDir;
};

class HttpClient;
class MediaRepository;
struct MediaSource;

class LiveClipService : public QObject
{
    Q_OBJECT

public:
    explicit LiveClipService(const LiveClipRequest &request,
                             QObject *parent = nullptr);

public slots:
    void run();

signals:
    // step: 0=ASR，1=AI 选片，2=生成草稿；percent 为当前步骤内的百分比。
    void progress(int step, const QString &message, int percent);
    // 过程页的细粒度事件：转写逐段写入、选片逐段完成、草稿逐步推进。
    void pipelineEvent(const PipelineUi::Event &event);
    void draftCreated(qint64 draftRowId);
    void finished(bool success, const QString &message);

private:
    // 一次 LLM 选片的转写范围；segments 是交给 LLM 的带索引句段列表。
    struct TranscriptChunk
    {
        qint64 startMs = 0;
        qint64 endMs = 0;
        QVector<AsrUtterance> segments;
    };

    bool ensureTranscript(MediaRepository *repository, HttpClient *http,
                          CapCutMateClient *capcut, const QString &mediaPath,
                          const QString &fingerprintPath, AsrTranscript *transcript,
                          QString *error);
    QVector<TranscriptChunk> buildChunks(const AsrTranscript &transcript) const;

    // 把 LLM 返回的句段索引转换为裁剪区间；相邻区间间隔 ≤ 500ms 时合并。
    QVector<ClipRange> mergeRanges(const QVector<ClipRange> &ranges) const;

    // 按后端规则整理 LLM 生成的短视频元数据。
    QString sanitizeTitle(const QString &raw) const;
    QString sanitizeDescription(const QString &raw) const;
    QStringList sanitizeTopics(const QStringList &raw) const;

    bool createDraftForProject(MediaRepository *repository, CapCutMateClient *capcut,
                               const QString &inputPath,
                               const SelectedProject &project, int index,
                               QString *error);

    // 裁剪统一使用本地 MP4：HLS 录像优先复用已有 MP4，必要时无损转封装。
    QString resolveDraftInputPath(const MediaSource &source, QString *error);

    QString sourceFingerprint(const QString &inputPath) const;

    LiveClipRequest m_request;
    QString m_workDir;
    // 源视频时长（毫秒），用于过程页在 ASR 返回前绘制整段音频波形与时间轴。
    qint64 m_sourceDurationMs = 0;
};
