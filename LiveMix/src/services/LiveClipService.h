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

// 成片数量规则：每条成片至少使用 5 分钟素材，避免切得过碎导致内容重复；
// 最多使用 30 分钟素材，避免条数过少浪费素材。
// 允许条数 = [ceil(素材总时长 / 30 分钟), floor(素材总时长 / 5 分钟)]。
constexpr qint64 kMinClipMaterialMs = 5LL * 60 * 1000;
constexpr qint64 kMaxClipMaterialMs = 30LL * 60 * 1000;

int minClipCountForDuration(qint64 durationMs);
int maxClipCountForDuration(qint64 durationMs);

struct LiveClipRequest
{
    qint64 sourceId = -1;
    QString databasePath;
    QString apiKey;
    QString gatewayBaseUrl; // OpenAI 兼容网关，同时用于 ASR 与 LLM
    QString promptText;     // 选片系统提示词（内置提示词由后端导入）
    QString promptLabel;    // 过程页展示的提示词名称
    QString cacheDir;
    // 目标成片数量；<= 0 表示自动（按每条最多 30 分钟素材取最少条数）。
    // 指定时需落在 [minClipCountForDuration, maxClipCountForDuration] 内。
    int targetClipCount = -1;
    // 成片设置：每条成片 1.0×–1.5× 随机取速，或固定为 fixedSpeed。
    bool speedAuto = true;
    double fixedSpeed = 1.0;
    // 成片设置 → 中英文双字幕：在中文轨下方追加英文译文轨。
    bool bilingualSubtitles = false;
    // 成片设置 → 自动成片包装：开关关闭时不写对应轨道 / 转场。
    bool transitions = true;
    bool effects = true;
    bool filters = true;
    // 自动贴纸：从内置贴纸池随机挑一张，贴在 3–10 秒的字幕窗口上。
    bool stickers = true;
    // 自动 BGM：开启时从素材服务随机取一首背景音乐铺满成片。
    bool bgm = true;
    // 自动音效：开启时按内置音效目录，用大模型稀疏挑选字幕并写入音效轨。
    bool sfx = true;
    // 平台去重：每段切片随机放大，并叠加低透明度随机图片。
    bool dedup = true;
};

// 自动音效：内置目录提供资源 ID、名称与顺序，音频地址与时长从素材服务获取。
struct SfxCategory
{
    QString name;
    QString url;
    qint64 durationMs = 0;
    QString usageHint;
};

class HttpClient;
class LlmClient;
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
    // 把转写按成片数量均分为等长时间窗；窗口数即目标成片数。
    // 条数范围由 5–30 分钟/条的素材预算保证，因此窗口时长不超过 LLM 单次上限。
    QVector<TranscriptChunk> buildChunks(const AsrTranscript &transcript,
                                         int chunkCount) const;

    // 把 LLM 返回的句段索引转换为裁剪区间；相邻区间间隔 ≤ 500ms 时合并。
    QVector<ClipRange> mergeRanges(const QVector<ClipRange> &ranges) const;

    // 按后端规则整理 LLM 生成的短视频元数据。
    QString sanitizeTitle(const QString &raw) const;
    QString sanitizeDescription(const QString &raw) const;
    QStringList sanitizeTopics(const QStringList &raw) const;

    // llm 用于「中英文双字幕」时按需翻译字幕，可为空（为空时跳过英文轨）。
    // http 复用任务级连接，供素材服务随机 BGM 使用。
    bool createDraftForProject(MediaRepository *repository, CapCutMateClient *capcut,
                               LlmClient *llm, HttpClient *http,
                               const QString &inputPath,
                               const SelectedProject &project, int index,
                               double speed, const QVector<SfxCategory> &sfxCategories,
                               QString *error);

    // 裁剪统一使用本地 MP4：HLS 录像优先复用已有 MP4，必要时无损转封装。
    QString resolveDraftInputPath(const MediaSource &source, QString *error);

    QString sourceFingerprint(const QString &inputPath) const;

    LiveClipRequest m_request;
    QString m_workDir;
    // 源视频时长（毫秒），用于过程页在 ASR 返回前绘制整段音频波形与时间轴。
    qint64 m_sourceDurationMs = 0;
};
