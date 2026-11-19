#pragma once

// 文件作用：一键成片过程页的数据模型。
// 工作线程把 ASR、AI 选片与草稿生成的细粒度过程整理成 Event，
// 通过队列信号交给界面线程，页面据此渲染实时过程和过程数据。

#include "ClipTypes.h"

#include <QMetaType>
#include <QString>
#include <QStringList>
#include <QVector>

namespace PipelineUi {

enum class StageKey {
    Asr = 0,
    Select = 1,
    Draft = 2,
};

enum class StageStatus {
    Waiting,
    Running,
    Done,
    Failed,
};

enum class StepStatus {
    Waiting,
    Running,
    Done,
    Failed,
};

enum class EventType {
    RunStarted,        // 源视频、提示词与总时长
    AsrPrepared,       // 整片识别准备：step 0=提取音轨，1=上传音频；flag=命中缓存
    AsrSubmitted,      // 整片识别任务已提交，开始等待服务返回
    AsrResult,         // 识别结果已返回，正在解析逐句时间轴
    AsrFinished,       // 整片转写完成，一次性携带完整转写稿
    SelectPlanned,     // 分段方案确定，预建候选卡片
    SelectBatchStarted,
    SelectThought,     // 模型流式思考增量
    SelectStatus,      // 等待/重试等过程状态
    SelectBatchSkipped,
    SelectBatchFinished,
    SelectFinished,
    DraftPlan,         // 选片完成，预建草稿列表
    DraftStarted,
    DraftStep,
    DraftFinished,
    RunFinished,
    TaskSummary,       // 从任务中心打开仍在运行的任务
};

// 一条候选切片：AI 选出的成片方案。
struct Candidate
{
    int index = 0;
    QString title;
    QString description;
    QStringList topics;
    QString range;
    qint64 startMs = 0;
    qint64 endMs = 0;
    qint64 durationMs = 0;
    int rangeCount = 0;
    int captionCount = 0;
    int sourceSentenceCount = 0;
    bool skipped = false;
};

struct Event
{
    EventType type = EventType::TaskSummary;
    int index = 0;      // 1-based：分段/草稿序号
    int count = 0;      // 总数：分段数/草稿数
    int step = 0;       // 任务步骤或草稿步骤（0-based）
    int status = 0;     // StepStatus
    int percent = 0;
    qint64 startMs = 0;
    qint64 endMs = 0;
    qint64 durationMs = 0;
    qint64 tokenCount = 0;
    int sentenceCount = 0;
    int charCount = 0;
    int captionCount = 0;
    // ASR 整片识别任务 ID；缓存命中时同样生成一个展示 ID。
    QString jobId;
    // 句级平均置信度（0–100）与说话人数；服务未返回时为 -1 / 0。
    double confidence = -1.0;
    int speakerCount = 0;
    bool flag = false;
    QString text;
    QString detail;
    QString promptLabel;
    QString sourceTitle;
    QVector<AsrUtterance> lines;
    QVector<Candidate> candidates;
};

inline QString formatClock(qint64 milliseconds)
{
    const qint64 totalSeconds = qMax<qint64>(0, milliseconds) / 1000;
    const qint64 hours = totalSeconds / 3600;
    const qint64 minutes = (totalSeconds % 3600) / 60;
    const qint64 seconds = totalSeconds % 60;
    return QStringLiteral("%1:%2:%3")
        .arg(hours, 2, 10, QLatin1Char('0'))
        .arg(minutes, 2, 10, QLatin1Char('0'))
        .arg(seconds, 2, 10, QLatin1Char('0'));
}

inline QString formatRange(qint64 startMs, qint64 endMs)
{
    return QStringLiteral("%1–%2").arg(formatClock(startMs), formatClock(endMs));
}

// 整片 ASR 的等待时长估算：按服务约 12 倍实时速度折算，
// 下限 20 秒、上限 30 分钟；仅用于过程页和任务中心的进度曲线。
inline qint64 estimateAsrRecognizeMs(qint64 audioDurationMs)
{
    if (audioDurationMs <= 0) {
        return 60000;
    }
    return qBound<qint64>(20000, qint64(audioDurationMs / 12.0), 30LL * 60 * 1000);
}

// 成片时长：不足一小时用 mm:ss，超过时用 h:mm:ss。
inline QString formatSpan(qint64 milliseconds)
{
    const qint64 totalSeconds = qMax<qint64>(0, milliseconds) / 1000;
    const qint64 hours = totalSeconds / 3600;
    const qint64 minutes = (totalSeconds % 3600) / 60;
    const qint64 seconds = totalSeconds % 60;
    if (hours > 0) {
        return QStringLiteral("%1:%2:%3")
            .arg(hours)
            .arg(minutes, 2, 10, QLatin1Char('0'))
            .arg(seconds, 2, 10, QLatin1Char('0'));
    }
    return QStringLiteral("%1:%2")
        .arg(minutes, 2, 10, QLatin1Char('0'))
        .arg(seconds, 2, 10, QLatin1Char('0'));
}

} // namespace PipelineUi

Q_DECLARE_METATYPE(PipelineUi::Event)
