#pragma once

// 文件作用：直播切片流程的领域数据结构。
// 包含 ASR 转写结果、LLM 选片方案、剪映草稿记录与后台任务记录。

#include <QJsonObject>
#include <QString>
#include <QStringList>
#include <QVector>

// ASR 返回的逐字时间线（毫秒）。
struct AsrWord
{
    QString text;
    qint64 startMs = 0;
    qint64 endMs = 0;
};

// ASR 返回的一句话；startMs/endMs 由该句第一个和最后一个字推导。
struct AsrUtterance
{
    QString text;
    qint64 startMs = 0;
    qint64 endMs = 0;
    QVector<AsrWord> words;
    // 服务返回说话人分离结果时才非空；网关当前通常不返回，界面会退化为通用标签。
    QString speaker;
    // 0–100 的句级置信度；< 0 表示服务没有返回。
    double confidence = -1.0;
};

// 一整段直播的转写结果。
struct AsrTranscript
{
    QString text;
    QVector<AsrUtterance> utterances;

    bool isEmpty() const { return utterances.isEmpty() && text.isEmpty(); }

    // 最后一句的结束时间，用于确定转写覆盖的时长。
    qint64 durationMs() const;

    // 说话人数量（按非空 speaker 去重）；服务未返回说话人信息时为 0。
    int speakerCount() const;
    // 有效的句级平均置信度（0–100）；没有可用的置信度时返回 -1。
    double averageConfidence() const;

    // 解析 ASR 响应中的 data 对象（含 text / utterances）。
    static AsrTranscript fromJson(const QJsonObject &data, QString *error = nullptr);

    // 解析 OpenAI /audio/transcriptions 的 verbose_json 响应：
    // segments[].words 是逐词时间线（秒），单句没有 words 时也能正常解析。
    static AsrTranscript fromOpenAiVerboseJson(const QJsonObject &json);

    QJsonObject toJson() const;
};

// 一段连续的视频区间（绝对毫秒）。
struct ClipRange
{
    qint64 startMs = 0;
    qint64 endMs = 0;

    qint64 durationMs() const { return qMax<qint64>(0, endMs - startMs); }
};

// LLM 返回的选片结果：选中句段索引 + 短视频元数据。
// indices 是本次提示词中 ASR 列表的 0-based 行号。
struct LlmSelectionResult
{
    QVector<int> indices;
    QString title;
    QString description;
    QStringList topics; // 不带 #，由界面展示时补充
    QString reasoning;  // 模型流式返回的思考内容（用于日志/过程回看）
    // 网关返回的 token 用量（部分网关可能不返回，此时保持 0）。
    int promptTokens = 0;
    int completionTokens = 0;
    int totalTokens = 0;
};

// 一段转写经 LLM 选片、合并后的成片方案。
struct SelectedProject
{
    QVector<ClipRange> ranges;          // 合并相邻句段后的裁剪区间
    QVector<AsrUtterance> captions;     // 被选中的 ASR 句段，用于生成字幕
    QString title;
    QString description;
    QStringList topics;
};

// 一条已生成、可下载的剪映草稿记录。
struct ClipDraft
{
    qint64 id = -1;
    qint64 sourceId = -1;
    QString draftId;
    QString draftUrl;
    QString title;
    QString description;
    QStringList tags;       // 存储不带 # 的话题
    qint64 sourceStartMs = 0;
    qint64 sourceEndMs = 0;
    qint64 durationMs = 0;  // 成片实际总时长（多段拼接后）
    qint64 createdAtMs = 0;
    QString downloadedPath;
};

// 任务步骤名称：0=ASR 转写，1=AI 选片，2=生成草稿。
QString clipTaskStepName(int step);

// 一键成片后台任务状态。
enum class ClipTaskStatus {
    Running,
    Completed,
    Failed,
};

// 任务中心展示的一键成片任务；进度按步骤保存，便于离开页面后回看。
struct ClipTask
{
    qint64 id = -1;
    qint64 sourceId = -1;
    QString title;
    ClipTaskStatus status = ClipTaskStatus::Running;
    int step = 0;      // 0=ASR，1=AI 选片，2=生成草稿
    int progress = 0;  // 当前步骤内进度 0–100
    QString message;
    int draftCount = 0;
    qint64 createdAtMs = 0;
    qint64 updatedAtMs = 0;
    qint64 finishedAtMs = 0;

    bool isRunning() const { return status == ClipTaskStatus::Running; }
};
