// 文件作用：直播切片流水线实现，串起 ASR、LLM、ffmpeg 与剪映小助手。
// 录制源处理：HLS/TS 只用于录制恢复；成片前先无损合成完整 MP4（已存在时
// 直接复用），之后的音频提取、ASR 与裁剪都按本地 MP4 处理。
// 选片策略：单次 LLM 选片最多覆盖 30 分钟转写；超过时按总时长均衡切成
// 若干段分别调用（40 分钟 → 2×20，66 分钟 → 3×22），切点尽量落在句号上。
// 模型返回句段索引与标题/描述/话题，再把选中句段合并、裁剪并拼成一条草稿。
#include "LiveClipService.h"

#include "../api/AsrClient.h"
#include "../api/CapCutMateClient.h"
#include "../api/HttpClient.h"
#include "../api/LlmClient.h"
#include "../core/MediaUtils.h"
#include "../data/MediaRepository.h"
#include "../media/FfmpegUtils.h"
#include "CaptionTiming.h"

#include <QDateTime>
#include <QDebug>
#include <QDir>
#include <QElapsedTimer>
#include <QFile>
#include <QFileInfo>
#include <QRandomGenerator>
#include <QSet>
#include <QThread>
#include <QTimer>
#include <QUrl>

#include <cmath>

namespace {

// 单次 LLM 选片最多覆盖 30 分钟转写；超过时均衡切成多段，避免硬切。
constexpr qint64 kLlmChunkLimitMs = 30LL * 60 * 1000;

// 在每段目标切点前后各 5 分钟内寻找句号。
constexpr qint64 kChunkSplitSearchMs = 5LL * 60 * 1000;

// 相邻选中句段间隔 ≤ 500ms 时合并为同一段视频（与后端 ClipMergeGapMS 一致）。
constexpr qint64 kRangeMergeGapMs = 500;

// 整片识别返回后，至少让“解析结果”阶段可见约 900ms，
// 与过程页的解析动画保持一致；期间完成 SQLite 缓存写入。
constexpr int kAsrResultMinDisplayMs = 900;

// 工作目录在任务结束（无论成功或失败）后自动清理。
class WorkDirCleanup
{
public:
    explicit WorkDirCleanup(const QString &dir)
        : m_dir(dir)
    {
    }

    ~WorkDirCleanup()
    {
        if (!m_dir.isEmpty()) {
            QDir(m_dir).removeRecursively();
        }
    }

private:
    QString m_dir;
};

QString formatSeconds(qint64 milliseconds)
{
    return MediaUtils::formatDuration(qMax<qint64>(0, milliseconds));
}

// 单次 LLM 最多处理 30 分钟转写：段数向上取整，再按段数均分总时长。
int chunkCountForDuration(qint64 durationMs)
{
    if (durationMs <= kLlmChunkLimitMs) {
        return 1;
    }
    return int((durationMs + kLlmChunkLimitMs - 1) / kLlmChunkLimitMs);
}

qint64 totalDuration(const QVector<ClipRange> &ranges)
{
    qint64 total = 0;
    for (const ClipRange &range : ranges) {
        total += range.durationMs();
    }
    return total;
}

int transcriptCharCount(const AsrTranscript &transcript)
{
    int chars = 0;
    for (const AsrUtterance &utterance : transcript.utterances) {
        chars += utterance.text.size();
    }
    return chars;
}

// 过程页展示的整片识别任务 ID；由本次运行随机生成，缓存命中时也保持一致。
QString makeAsrJobId()
{
    const QString suffix =
        QString::number(QRandomGenerator::global()->generate(), 16).toUpper().right(6);
    return QStringLiteral("job_asr_%1").arg(suffix.rightJustified(6, QLatin1Char('0')));
}

PipelineUi::Candidate candidateFromProject(const SelectedProject &project, int index)
{
    PipelineUi::Candidate candidate;
    candidate.index = index;
    candidate.title = project.title.isEmpty() ? QStringLiteral("直播切片") : project.title;
    candidate.description = project.description;
    candidate.topics = project.topics;
    candidate.durationMs = totalDuration(project.ranges);
    candidate.rangeCount = project.ranges.size();
    candidate.captionCount = project.captions.size();
    candidate.sourceSentenceCount = project.captions.size();
    if (!project.ranges.isEmpty()) {
        candidate.startMs = project.ranges.first().startMs;
        candidate.endMs = project.ranges.last().endMs;
        candidate.range = PipelineUi::formatRange(candidate.startMs, candidate.endMs);
    }
    return candidate;
}

// 每条成片从下列样式中随机挑一种，字体与动画名称均为剪映小助手支持的值。
const QVector<CapCutCaptionStyle> &captionStylePresets()
{
    static const QVector<CapCutCaptionStyle> presets = {
        {QStringLiteral("新青年体"), 8, QStringLiteral("#ffffff"),
         QStringLiteral("#000000"), QString()},
        {QStringLiteral("飞扬行书"), 8, QStringLiteral("#ffde00"),
         QStringLiteral("#000000"), QString()},
        {QStringLiteral("荔枝体"), 8, QStringLiteral("#ffffff"),
         QStringLiteral("#000000"), QStringLiteral("打字机 II")},
        {QStringLiteral("金陵体"), 8, QStringLiteral("#000000"),
         QStringLiteral("#ffffff"), QStringLiteral("向右缓入")},
        {QStringLiteral("后现代体"), 8, QStringLiteral("#ffffff"),
         QStringLiteral("#fe8a80"), QString()},
        {QStringLiteral("墩墩体"), 8, QStringLiteral("#ffffff"),
         QStringLiteral("#ff1837"), QStringLiteral("打字光标")},
        {QStringLiteral("文研体"), 8, QStringLiteral("#ffffff"),
         QStringLiteral("#ff1837"), QStringLiteral("向右擦除")},
        {QStringLiteral("古印宋简"), 8, QStringLiteral("#ffffff"),
         QStringLiteral("#000000"), QStringLiteral("日出")},
        {QStringLiteral("山雁体"), 8, QStringLiteral("#ffffff"),
         QStringLiteral("#000000"), QStringLiteral("波浪弹入")},
        {QStringLiteral("俊雅体"), 8, QStringLiteral("#ffffff"),
         QStringLiteral("#000000"), QString()},
        {QStringLiteral("风雅宋"), 8, QStringLiteral("#ab4a37"),
         QStringLiteral("#ffffff"), QString()},
        {QStringLiteral("挥墨体"), 8, QStringLiteral("#ab4a37"),
         QStringLiteral("#ffffff"), QStringLiteral("渐显")},
    };
    return presets;
}

CapCutCaptionStyle randomCaptionStyle()
{
    const QVector<CapCutCaptionStyle> &presets = captionStylePresets();
    return presets.at(QRandomGenerator::global()->bounded(int(presets.size())));
}

// 判断一句转写是否以句末标点结尾；允许句号后跟引号、括号等收尾符号。
bool endsWithSentenceEnd(const QString &text)
{
    QString trimmed = text.trimmed();
    static const QString trailingMarks = QStringLiteral("”’」』）)》】\"'");
    while (!trimmed.isEmpty() && trailingMarks.contains(trimmed.at(trimmed.size() - 1))) {
        trimmed.chop(1);
    }
    if (trimmed.isEmpty()) {
        return false;
    }
    const QChar last = trimmed.at(trimmed.size() - 1);
    return last == QChar(0x3002)      // 。
        || last == QChar(0xFF01)      // ！
        || last == QChar(0xFF1F)      // ？
        || last == QLatin1Char('.')
        || last == QLatin1Char('!')
        || last == QLatin1Char('?');
}

// 返回目标切点之后一段的起始句索引；优先取目标时间前后 5 分钟内离目标
// 最近的句号，找不到句号时退回离目标最近的句段边界。
// searchFrom 用于保证多段切分时索引持续前进，不会切出空段。
int splitIndexNear(const QVector<AsrUtterance> &utterances, qint64 targetMs,
                   int searchFrom)
{
    int periodSplit = 0;
    qint64 periodDistance = kChunkSplitSearchMs + 1;
    int nearestSplit = qMax(1, searchFrom);
    qint64 nearestDistance = -1;

    for (int i = qMax(1, searchFrom); i < utterances.size(); ++i) {
        const qint64 boundaryDistance = qAbs(utterances.at(i).startMs - targetMs);
        if (nearestDistance < 0 || boundaryDistance < nearestDistance) {
            nearestDistance = boundaryDistance;
            nearestSplit = i;
        }

        if (!endsWithSentenceEnd(utterances.at(i - 1).text)) {
            continue;
        }
        // 切点落在上一句的句号之后，按句号结束时间衡量与目标的距离。
        const qint64 distance = qAbs(utterances.at(i - 1).endMs - targetMs);
        if (distance <= kChunkSplitSearchMs
            && (periodSplit == 0 || distance < periodDistance)) {
            periodDistance = distance;
            periodSplit = i;
        }
    }

    return periodSplit > 0 ? periodSplit : nearestSplit;
}

} // namespace

LiveClipService::LiveClipService(const LiveClipRequest &request, QObject *parent)
    : QObject(parent)
    , m_request(request)
{
}

void LiveClipService::run()
{
    qInfo() << "Live clip job started for source" << m_request.sourceId;

    m_workDir = QDir(m_request.cacheDir)
                    .filePath(QStringLiteral("clip-jobs/source_%1_%2")
                                  .arg(m_request.sourceId)
                                  .arg(QDateTime::currentMSecsSinceEpoch()));
    if (!QDir().mkpath(m_workDir)) {
        emit finished(false, QStringLiteral("无法创建临时工作目录：%1").arg(m_workDir));
        return;
    }
    WorkDirCleanup cleanup(m_workDir);

    if (m_request.apiKey.trimmed().isEmpty()) {
        emit finished(false, QStringLiteral(
                                  "尚未配置 API 密钥，请先到「设置 → 服务连接」填写。"));
        return;
    }
    if (m_request.gatewayBaseUrl.trimmed().isEmpty()) {
        emit finished(false, QStringLiteral("API 地址为空，请检查「设置 → 服务连接」。"));
        return;
    }
    if (m_request.promptText.trimmed().isEmpty()) {
        emit finished(false, QStringLiteral("未选择选片提示词，请重试。"));
        return;
    }

    // 工作线程内单独打开数据库连接；SQLite 连接不能跨线程复用。
    MediaRepository repository(m_request.databasePath);
    QString error;
    if (!repository.open(&error)) {
        emit finished(false, QStringLiteral("本地数据库不可用：%1").arg(error));
        return;
    }

    const MediaSource source = repository.source(m_request.sourceId, &error);
    if (source.id < 0) {
        emit finished(false, error);
        return;
    }
    if (source.status != SourceStatus::Ready) {
        emit finished(false,
                      source.status == SourceStatus::Recording
                          ? QStringLiteral("源视频仍在录制，请等待录制完成后再生成成片。")
                          : QStringLiteral("源视频不可用，请重新录制或导入。"));
        return;
    }
    const QFileInfo inputInfo(source.localPath);
    if (source.localPath.isEmpty() || !inputInfo.exists() || !inputInfo.isFile()) {
        emit finished(false, QStringLiteral("源视频文件不存在：%1").arg(source.localPath));
        return;
    }

    m_sourceDurationMs = qMax<qint64>(0, source.durationMs);

    PipelineUi::Event runStarted;
    runStarted.type = PipelineUi::EventType::RunStarted;
    runStarted.sourceTitle = source.title;
    runStarted.promptLabel = m_request.promptLabel;
    runStarted.durationMs = m_sourceDurationMs;
    // ASR 返回前先按源视频时长预判段数；ASR 完成后会按实际转写时长重算。
    runStarted.count = chunkCountForDuration(m_sourceDurationMs);
    emit pipelineEvent(runStarted);

    // ---- 第零步：准备本地 MP4 ---------------------------------------------
    // 录制阶段只保留 HLS/TS 便于断点恢复；成片前一次性合成完整 MP4，
    // 之后的音频提取、ASR 与裁剪都按本地 MP4 处理。已合成时直接复用。
    if (source.localPath.endsWith(QLatin1String(".m3u8"), Qt::CaseInsensitive)) {
        emit progress(0, QStringLiteral("正在准备完整 MP4…"), 0);
    }
    QString draftInputPath = resolveDraftInputPath(source, &error);
    if (draftInputPath.isEmpty()) {
        emit finished(false, QStringLiteral("准备完整 MP4 失败：%1").arg(error));
        return;
    }

    HttpClient http;
    CapCutMateClient capcut(&http);

    // ---- 第一步：ASR 转写（命中缓存时不重复计费） --------------------------
    // 音频从本地 MP4 提取；缓存指纹仍使用录制源，重新合成 MP4 不会触发重新识别。
    AsrTranscript transcript;
    if (!ensureTranscript(&repository, &http, &capcut, draftInputPath,
                          source.localPath, &transcript, &error)) {
        emit finished(false, error);
        return;
    }

    const QVector<TranscriptChunk> chunks = buildChunks(transcript);
    if (chunks.isEmpty()) {
        emit finished(false, QStringLiteral("转写结果中没有可用于选片的语句。"));
        return;
    }

    // 先把分段方案交给过程页，候选卡片在 AI 开始前就能展示“等待评估”。
    PipelineUi::Event selectPlan;
    selectPlan.type = PipelineUi::EventType::SelectPlanned;
    selectPlan.count = chunks.size();
    selectPlan.sentenceCount = transcript.utterances.size();
    selectPlan.charCount = transcriptCharCount(transcript);
    selectPlan.candidates.reserve(chunks.size());
    for (int i = 0; i < chunks.size(); ++i) {
        const TranscriptChunk &chunk = chunks.at(i);
        PipelineUi::Candidate candidate;
        candidate.index = i + 1;
        candidate.startMs = chunk.startMs;
        candidate.endMs = chunk.endMs;
        candidate.durationMs = qMax<qint64>(0, chunk.endMs - chunk.startMs);
        candidate.range = PipelineUi::formatRange(chunk.startMs, chunk.endMs);
        candidate.sourceSentenceCount = chunk.segments.size();
        // 尚未真正选出内容，已选句数保持 0，只把候选池句数放在 sourceSentenceCount。
        candidate.captionCount = 0;
        selectPlan.candidates.append(candidate);
    }
    emit pipelineEvent(selectPlan);

    // ---- 第二步：逐段调用 LLM；每段不超过 30 分钟，长转写按总时长均衡切分 ----
    LlmClient llm(&http);
    QVector<SelectedProject> projects;
    projects.reserve(chunks.size());
    qint64 totalCompletionTokens = 0;
    for (int i = 0; i < chunks.size(); ++i) {
        const TranscriptChunk &chunk = chunks.at(i);
        emit progress(1,
                      QStringLiteral("正在分析第 %1/%2 段（%3 – %4）…")
                          .arg(i + 1)
                          .arg(chunks.size())
                          .arg(formatSeconds(chunk.startMs), formatSeconds(chunk.endMs)),
                      100 * i / chunks.size());

        PipelineUi::Event batchStarted;
        batchStarted.type = PipelineUi::EventType::SelectBatchStarted;
        batchStarted.index = i + 1;
        batchStarted.count = chunks.size();
        batchStarted.startMs = chunk.startMs;
        batchStarted.endMs = chunk.endMs;
        batchStarted.sentenceCount = chunk.segments.size();
        emit pipelineEvent(batchStarted);

        LlmSelectionRequest request;
        request.baseUrl = m_request.gatewayBaseUrl;
        request.apiKey = m_request.apiKey;
        request.systemPrompt = m_request.promptText;
        request.segments = chunk.segments;

        // 模型 reasoning_content 的增量实时写入过程页；状态提示用于等待/重试。
        const int batchCount = chunks.size();
        const auto onThought = [this, i, batchCount](const QString &text) {
            PipelineUi::Event thought;
            thought.type = PipelineUi::EventType::SelectThought;
            thought.index = i + 1;
            thought.count = batchCount;
            thought.text = text;
            emit pipelineEvent(thought);
        };
        const auto onStatus = [this, i, batchCount](const QString &text) {
            PipelineUi::Event status;
            status.type = PipelineUi::EventType::SelectStatus;
            status.index = i + 1;
            status.count = batchCount;
            status.text = text;
            emit pipelineEvent(status);
        };

        LlmSelectionResult result;
        QString llmError;
        bool selected = llm.selectProject(request, &result, &llmError, onThought,
                                          onStatus);
        if (!selected) {
            // 只有模型输出无法解析时才用严格模式再试一次；网络/限流失败
            // 已在 LlmClient 内部退避重试，不应再叠加一整轮等待。
            const bool parseFailure =
                llmError.contains(QStringLiteral("无法解析"))
                || llmError.contains(QStringLiteral("返回内容为空"))
                || llmError.contains(QStringLiteral("未返回最终选片结果"));
            if (parseFailure) {
                request.strictJson = true;
                batchStarted.text =
                    QStringLiteral("首次输出无法解析，正在用严格模式重试…");
                emit pipelineEvent(batchStarted);
                selected = llm.selectProject(request, &result, &llmError, onThought,
                                             onStatus);
            }
        }
        if (!selected) {
            emit finished(false, QStringLiteral("第 %1 段选片失败：%2")
                                      .arg(i + 1)
                                      .arg(llmError));
            return;
        }

        totalCompletionTokens += result.completionTokens;
        if (result.indices.isEmpty()) {
            qInfo() << "LLM returned no indices for chunk" << i;
            PipelineUi::Event skipped;
            skipped.type = PipelineUi::EventType::SelectBatchSkipped;
            skipped.index = i + 1;
            skipped.count = chunks.size();
            skipped.startMs = chunk.startMs;
            skipped.endMs = chunk.endMs;
            skipped.sentenceCount = chunk.segments.size();
            skipped.tokenCount = result.completionTokens;
            skipped.text = QStringLiteral("该段没有符合提示词的片段");
            emit pipelineEvent(skipped);
            continue;
        }

        // 按模型返回顺序把索引映射成时间区间；越界索引直接忽略。
        // 模型偶尔会重复返回同一个索引，重复句段会生成时间完全相同的字幕，
        // 剪映小助手按轨道写入时判定片段重叠并整批拒绝（2019 字幕添加失败），
        // 因此这里先按索引去重。
        QVector<ClipRange> ranges;
        QVector<AsrUtterance> captions;
        QSet<int> usedIndices;
        for (int index : result.indices) {
            if (index < 0 || index >= chunk.segments.size()
                || usedIndices.contains(index)) {
                continue;
            }
            const AsrUtterance &seg = chunk.segments.at(index);
            if (seg.endMs <= seg.startMs) {
                continue;
            }
            usedIndices.insert(index);
            ranges.append({seg.startMs, seg.endMs});
            captions.append(seg);
        }
        if (ranges.isEmpty()) {
            PipelineUi::Event skipped;
            skipped.type = PipelineUi::EventType::SelectBatchSkipped;
            skipped.index = i + 1;
            skipped.count = chunks.size();
            skipped.startMs = chunk.startMs;
            skipped.endMs = chunk.endMs;
            skipped.sentenceCount = chunk.segments.size();
            skipped.tokenCount = result.completionTokens;
            skipped.text = QStringLiteral("该段没有有效的裁剪区间");
            emit pipelineEvent(skipped);
            continue;
        }
        ranges = mergeRanges(ranges);

        SelectedProject project;
        project.ranges = ranges;
        project.captions = captions;
        project.title = sanitizeTitle(result.title);
        project.description = sanitizeDescription(result.description);
        project.topics = sanitizeTopics(result.topics);
        projects.append(project);

        PipelineUi::Event batchFinished;
        batchFinished.type = PipelineUi::EventType::SelectBatchFinished;
        batchFinished.index = i + 1;
        batchFinished.count = chunks.size();
        batchFinished.startMs = chunk.startMs;
        batchFinished.endMs = chunk.endMs;
        batchFinished.tokenCount = result.completionTokens;
        batchFinished.candidates.append(candidateFromProject(project, i + 1));
        emit pipelineEvent(batchFinished);

        const QString displayTitle =
            project.title.isEmpty() ? QStringLiteral("直播切片") : project.title;
        emit progress(1,
                      QStringLiteral("第 %1/%2 段已选出：%3（%4 段，共 %5）")
                          .arg(i + 1)
                          .arg(chunks.size())
                          .arg(displayTitle)
                          .arg(project.ranges.size())
                          .arg(formatSeconds(totalDuration(project.ranges))),
                      100 * (i + 1) / chunks.size());
    }

    if (projects.isEmpty()) {
        emit finished(false, QStringLiteral("AI 没有选出可用的切片，请更换提示词后重试。"));
        return;
    }

    PipelineUi::Event selectFinished;
    selectFinished.type = PipelineUi::EventType::SelectFinished;
    selectFinished.count = projects.size();
    selectFinished.tokenCount = totalCompletionTokens;
    selectFinished.sentenceCount = chunks.size();
    selectFinished.text = QStringLiteral("已锁定 %1 条切片").arg(projects.size());
    emit pipelineEvent(selectFinished);

    // ---- 第三步：裁剪、上传并生成剪映草稿 -----------------------------------
    PipelineUi::Event draftPlan;
    draftPlan.type = PipelineUi::EventType::DraftPlan;
    draftPlan.count = projects.size();
    draftPlan.candidates.reserve(projects.size());
    for (int i = 0; i < projects.size(); ++i) {
        draftPlan.candidates.append(candidateFromProject(projects.at(i), i + 1));
    }
    emit pipelineEvent(draftPlan);

    for (int i = 0; i < projects.size(); ++i) {
        const SelectedProject &project = projects.at(i);
        const QString displayTitle =
            project.title.isEmpty() ? QStringLiteral("直播切片") : project.title;

        PipelineUi::Event draftStarted;
        draftStarted.type = PipelineUi::EventType::DraftStarted;
        draftStarted.index = i + 1;
        draftStarted.count = projects.size();
        draftStarted.candidates.append(candidateFromProject(project, i + 1));
        emit pipelineEvent(draftStarted);

        emit progress(2,
                      QStringLiteral("正在生成第 %1/%2 条草稿：%3")
                          .arg(i + 1)
                          .arg(projects.size())
                          .arg(displayTitle),
                      100 * i / projects.size());
        if (!createDraftForProject(&repository, &capcut, draftInputPath, project, i,
                                   &error)) {
            emit finished(false, QStringLiteral("第 %1 条草稿生成失败：%2")
                                      .arg(i + 1)
                                      .arg(error));
            return;
        }

        PipelineUi::Event draftFinished;
        draftFinished.type = PipelineUi::EventType::DraftFinished;
        draftFinished.index = i + 1;
        draftFinished.count = projects.size();
        emit pipelineEvent(draftFinished);

        emit progress(2,
                      QStringLiteral("第 %1/%2 条草稿已生成：%3")
                          .arg(i + 1)
                          .arg(projects.size())
                          .arg(displayTitle),
                      100 * (i + 1) / projects.size());
    }

    qInfo() << "Live clip job finished, drafts:" << projects.size();
    emit finished(true,
                  QStringLiteral("已生成 %1 条剪映草稿，可在「AI 成片结果」中查看。")
                      .arg(projects.size()));
}

bool LiveClipService::ensureTranscript(MediaRepository *repository, HttpClient *http,
                                       CapCutMateClient *capcut,
                                       const QString &mediaPath,
                                       const QString &fingerprintPath,
                                       AsrTranscript *transcript, QString *error)
{
    // 指纹仍绑定录制源（HLS 播放列表），同一录像重新合成 MP4 时不会重复识别。
    const QString fingerprint = sourceFingerprint(fingerprintPath);
    const QString jobId = makeAsrJobId();

    bool cached = false;
    if (!repository->loadCachedAsr(m_request.sourceId, fingerprint, transcript, &cached,
                                   error)) {
        return false;
    }
    const qint64 audioDurationMs = qMax(m_sourceDurationMs, transcript->durationMs());

    const auto emitPrepared = [this, &jobId](int step, qint64 durationMs, bool cachedFlag,
                                             const QString &note) {
        PipelineUi::Event event;
        event.type = PipelineUi::EventType::AsrPrepared;
        event.step = step;
        event.durationMs = durationMs;
        event.jobId = jobId;
        event.flag = cachedFlag;
        event.text = note;
        emit pipelineEvent(event);
    };

    if (cached) {
        emitPrepared(2, audioDurationMs, true,
                     QStringLiteral("已命中本地 ASR 缓存，跳过重复识别"));

        PipelineUi::Event done;
        done.type = PipelineUi::EventType::AsrFinished;
        done.jobId = jobId;
        done.count = transcript->utterances.size();
        done.charCount = transcriptCharCount(*transcript);
        done.durationMs = audioDurationMs;
        done.confidence = transcript->averageConfidence();
        done.speakerCount = transcript->speakerCount();
        done.flag = true;
        done.text = QStringLiteral("转写结果来自本地缓存");
        emit pipelineEvent(done);

        emit progress(0, QStringLiteral("已命中本地 ASR 缓存，跳过重复识别"), 100);
        return true;
    }

    // 1. 视频转 MP3；整段音频只提交一个识别任务，不做分段，
    //    避免拆段破坏上下文和说话人分离。
    emitPrepared(0, m_sourceDurationMs, false, QStringLiteral("正在提取 MP3 音频…"));
    emit progress(0, QStringLiteral("正在提取 MP3 音频…"), 5);
    const QString audioPath = QDir(m_workDir).filePath(QStringLiteral("audio.mp3"));
    if (!FfmpegUtils::extractMp3(mediaPath, audioPath, error)) {
        return false;
    }

    // 2. 上传整段音频，取得 ASR 标准版要求的公网 URL。
    emitPrepared(1, m_sourceDurationMs, false,
                 QStringLiteral("音频已提取，正在上传整片音频…"));
    emit progress(0, QStringLiteral("正在上传整片音频…"), 10);
    QString audioUrl;
    if (!capcut->uploadFile(audioPath, m_request.apiKey, &audioUrl, error)) {
        return false;
    }
    emitPrepared(1, m_sourceDurationMs, false,
                 QStringLiteral("整片音频已上传，正在提交识别任务…"));

    // 3. 提交整片识别任务。网关是同步接口，请求返回时一次性拿到完整转写稿；
    //    等待期间由过程页按音频时长估算进度，并以任务状态心跳维持反馈。
    PipelineUi::Event submitted;
    submitted.type = PipelineUi::EventType::AsrSubmitted;
    submitted.jobId = jobId;
    submitted.durationMs = m_sourceDurationMs;
    submitted.text = QStringLiteral("整片识别任务已提交");
    emit pipelineEvent(submitted);
    emit progress(0, QStringLiteral("正在整片识别…"), 15);

    AsrClient asr(http);
    AsrTranscript result;

    // 网关同步返回，过程页用本地估算曲线展示进度；任务中心也需要持续前进，
    // 因此每 5 秒上报一次估算进度（等待期间由 HttpClient 的嵌套事件循环驱动）。
    QElapsedTimer recognizeClock;
    recognizeClock.start();
    const qint64 estimatedRecognizeMs =
        PipelineUi::estimateAsrRecognizeMs(m_sourceDurationMs);
    QTimer taskProgressTimer;
    taskProgressTimer.setInterval(5000);
    QObject::connect(&taskProgressTimer, &QTimer::timeout, this,
                     [this, &recognizeClock, estimatedRecognizeMs] {
                         const qreal ratio =
                             recognizeClock.elapsed() / qreal(qMax<qint64>(
                                 1, estimatedRecognizeMs));
                         const int percent =
                             20 + int((1.0 - std::exp(-3.0 * ratio)) * 70.0);
                         emit progress(0, QStringLiteral("正在整片识别…"),
                                       qBound(20, percent, 90));
                     });
    taskProgressTimer.start();

    const bool transcribed =
        asr.transcribe(audioUrl, m_request.gatewayBaseUrl, m_request.apiKey, &result,
                       error);
    taskProgressTimer.stop();
    if (!transcribed) {
        return false;
    }
    if (result.isEmpty()) {
        if (error) {
            *error = QStringLiteral("ASR 未识别到语音内容。");
        }
        return false;
    }

    int utterancesWithWords = 0;
    for (const AsrUtterance &utterance : result.utterances) {
        if (!utterance.words.isEmpty()) {
            ++utterancesWithWords;
        }
    }
    if (utterancesWithWords == 0) {
        // 不影响成片：字幕会退回按文字长度分摊。出现这条日志通常说明
        // ASR 网关还没有把上游的 words 透传下来。
        qWarning() << "ASR response has no word timestamps; caption timing falls back "
                      "to proportional allocation";
    } else {
        qInfo() << "ASR word timestamps available for" << utterancesWithWords << "of"
                << result.utterances.size() << "utterances";
    }

    // 4. 结果返回后进入“解析结果”阶段：先把完整转写稿写入 SQLite，
    //    再让该阶段至少可见约 900ms，与交互设计保持一致。
    QElapsedTimer resultPhaseClock;
    resultPhaseClock.start();

    PipelineUi::Event resultReady;
    resultReady.type = PipelineUi::EventType::AsrResult;
    resultReady.jobId = jobId;
    resultReady.durationMs = qMax(m_sourceDurationMs, result.durationMs());
    resultReady.count = result.utterances.size();
    resultReady.charCount = transcriptCharCount(result);
    resultReady.confidence = result.averageConfidence();
    resultReady.speakerCount = result.speakerCount();
    resultReady.text = QStringLiteral("识别完成，正在解析逐句时间轴…");
    emit pipelineEvent(resultReady);
    emit progress(0, QStringLiteral("识别完成，正在解析转写结果…"), 90);

    emit progress(0, QStringLiteral("正在保存 ASR 缓存…"), 95);
    if (!repository->saveAsrResult(m_request.sourceId, fingerprint, result, error)) {
        return false;
    }
    *transcript = result;

    const qint64 remainingMs = kAsrResultMinDisplayMs - resultPhaseClock.elapsed();
    if (remainingMs > 0) {
        QThread::msleep(static_cast<unsigned long>(remainingMs));
    }

    QFile::remove(audioPath);

    PipelineUi::Event done;
    done.type = PipelineUi::EventType::AsrFinished;
    done.jobId = jobId;
    done.count = result.utterances.size();
    done.charCount = transcriptCharCount(result);
    done.durationMs = qMax(m_sourceDurationMs, result.durationMs());
    done.confidence = result.averageConfidence();
    done.speakerCount = result.speakerCount();
    done.text = QStringLiteral("语音转写完成");
    emit pipelineEvent(done);

    emit progress(0, QStringLiteral("语音转写完成，共 %1 句").arg(result.utterances.size()),
                  100);
    return true;
}

QVector<LiveClipService::TranscriptChunk>
LiveClipService::buildChunks(const AsrTranscript &transcript) const
{
    QVector<TranscriptChunk> chunks;
    const QVector<AsrUtterance> &utterances = transcript.utterances;
    if (utterances.isEmpty()) {
        return chunks;
    }

    // 以 ASR 的实际时长决定分段：每段目标时长不超过 30 分钟，
    // 段数 = 总时长 / 30 分钟向上取整，再按段数均分（40 分钟 → 2×20，
    // 66 分钟 → 3×22）。切点尽量落在句号上，避免把一句话拆到两组提示词里。
    const qint64 asrDurationMs = transcript.durationMs();
    const int utteranceCount = int(utterances.size());
    const int chunkCount = qMin(chunkCountForDuration(asrDurationMs), utteranceCount);

    const auto appendChunk = [&chunks](const QVector<AsrUtterance> &segments) {
        if (segments.isEmpty()) {
            return;
        }
        TranscriptChunk chunk;
        chunk.startMs = segments.first().startMs;
        chunk.endMs = segments.last().endMs;
        chunk.segments = segments;
        chunks.append(chunk);
    };

    if (chunkCount <= 1) {
        appendChunk(utterances);
        return chunks;
    }

    int previousSplit = 0;
    for (int i = 1; i < chunkCount; ++i) {
        const qint64 targetMs = asrDurationMs * i / chunkCount;
        // 剩余段至少各占一句，避免切点越过末尾。
        const int maxSplit = utteranceCount - (chunkCount - i);
        int split = splitIndexNear(utterances, targetMs, previousSplit + 1);
        split = qBound(previousSplit + 1, split, maxSplit);
        appendChunk(utterances.mid(previousSplit, split - previousSplit));
        previousSplit = split;
    }
    appendChunk(utterances.mid(previousSplit));
    return chunks;
}

QVector<ClipRange>
LiveClipService::mergeRanges(const QVector<ClipRange> &ranges) const
{
    QVector<ClipRange> merged;
    if (ranges.isEmpty()) {
        return merged;
    }

    // 与后端 MergeAdjacentClipRanges 一致：保留模型返回顺序，
    // 相邻区间间隔 ≤ 500ms（含向前重叠）时取并集。
    ClipRange current = ranges.first();
    for (int i = 1; i < ranges.size(); ++i) {
        const ClipRange &next = ranges.at(i);
        const qint64 gap = next.startMs - current.endMs;
        if (next.startMs >= current.startMs && gap <= kRangeMergeGapMs) {
            current.endMs = qMax(current.endMs, next.endMs);
            continue;
        }
        merged.append(current);
        current = next;
    }
    merged.append(current);
    return merged;
}

QString LiveClipService::sanitizeTitle(const QString &raw) const
{
    QString title = raw.trimmed();
    if (title.size() > 12) {
        title.truncate(12);
    }
    return title.size() >= 2 ? title : QString();
}

QString LiveClipService::sanitizeDescription(const QString &raw) const
{
    QString description = raw.trimmed();
    if (description.size() > 128) {
        description.truncate(128);
    }
    return description;
}

QStringList LiveClipService::sanitizeTopics(const QStringList &raw) const
{
    QStringList topics;
    QSet<QString> seen;
    for (const QString &item : raw) {
        QString topic = item.trimmed();
        while (topic.startsWith(QLatin1Char('#'))
               || topic.startsWith(QChar(0xFF03))) {
            topic.remove(0, 1);
            topic = topic.trimmed();
        }
        if (topic.isEmpty()) {
            continue;
        }
        if (topic.size() > 12) {
            topic.truncate(12);
        }
        if (topic.size() < 2 || seen.contains(topic)) {
            continue;
        }
        seen.insert(topic);
        topics.append(topic);
        if (topics.size() >= 6) {
            break;
        }
    }
    return topics;
}

QString LiveClipService::resolveDraftInputPath(const MediaSource &source,
                                               QString *error)
{
    if (!source.localPath.endsWith(QLatin1String(".m3u8"), Qt::CaseInsensitive)) {
        return source.localPath;
    }

    // HLS 的输入 seek 落在分片/关键帧边界时会偏后，导致切片时长短于请求值。
    // 成片阶段统一使用录制目录里的完整 MP4；没有时由 ensureRecordingMp4()
    // 复用预览缓存或无损合成一次，后续任务直接复用该文件。
    const QFileInfo playlist(source.localPath);
    const QString dirPath = playlist.absolutePath();
    const qint64 playlistMs = MediaUtils::hlsPlaylistDurationMs(source.localPath);
    const QString sourceBase =
        QFileInfo(QUrl(source.sourceUri).path()).completeBaseName();
    // 兼容旧版本/外部工具在录制目录里生成的 MP4；标准产物 index.mp4
    // 交给 ensureRecordingMp4() 按播放列表完成度和生成时间统一校验。
    QStringList candidates;
    candidates << QDir(dirPath).filePath(
                      MediaUtils::sanitizeFileComponent(source.title, 80)
                      + QStringLiteral(".mp4"));
    candidates << QDir(dirPath).filePath(playlist.absoluteDir().dirName()
                                         + QStringLiteral(".mp4"));
    if (!sourceBase.isEmpty()) {
        candidates << QDir(dirPath).filePath(
            MediaUtils::sanitizeFileComponent(sourceBase, 80)
            + QStringLiteral(".mp4"));
    }
    candidates << QDir(dirPath).filePath(QStringLiteral("recording.mp4"));
    for (const QString &candidate : candidates) {
        const QFileInfo info(candidate);
        if (!info.isFile() || info.size() <= 0
            || info.lastModified() < playlist.lastModified()) {
            continue;
        }
        qint64 durationMs = 0;
        if (!FfmpegUtils::mediaDurationMs(candidate, &durationMs) || durationMs <= 0) {
            continue;
        }
        // 中断恢复的录像最后一段可能被截断，允许少一个分片。
        if (playlistMs > 0 && durationMs + 6000 < playlistMs) {
            continue;
        }
        qInfo() << "Draft clips will be cut from existing MP4:" << candidate;
        return candidate;
    }

    QString mp4Path;
    if (!FfmpegUtils::ensureRecordingMp4(source.localPath, &mp4Path, error)) {
        return QString();
    }
    if (mp4Path.isEmpty()) {
        if (error) {
            *error = QStringLiteral("无法准备完整 MP4 文件。");
        }
        return QString();
    }
    qInfo() << "Draft clips will be cut from MP4:" << mp4Path;
    return mp4Path;
}

bool LiveClipService::createDraftForProject(MediaRepository *repository,
                                            CapCutMateClient *capcut,
                                            const QString &inputPath,
                                            const SelectedProject &project, int index,
                                            QString *error)
{
    if (project.ranges.isEmpty()) {
        if (error) {
            *error = QStringLiteral("成片没有可用的视频片段。");
        }
        return false;
    }

    QVector<CapCutVideoInfo> videos;
    QVector<CapCutCaption> captions;
    QStringList clipPaths;
    QVector<qint64> actualDurations;
    int canvasWidth = 1920;
    int canvasHeight = 1080;
    bool hasCanvasSize = false;

    // 草稿生成的过程页按真实执行顺序拆成 6 步；每步开始/结束时上报，
    // 单次接口调用没有内部进度，用 0/100 表示开始与完成。
    QElapsedTimer stepClock;
    auto emitStep = [this, index, &stepClock](int step, PipelineUi::StepStatus status,
                                              int percent, const QString &detail) {
        PipelineUi::Event event;
        event.type = PipelineUi::EventType::DraftStep;
        event.index = index + 1;
        event.step = step;
        event.status = static_cast<int>(status);
        event.percent = qBound(0, percent, 100);
        event.detail = detail;
        event.durationMs = stepClock.isValid() ? stepClock.elapsed() : 0;
        emit pipelineEvent(event);
    };
    auto emitFailure = [&](int step, const QString &fallback) {
        emitStep(step, PipelineUi::StepStatus::Failed, 0,
                 (error && !error->isEmpty()) ? *error : fallback);
    };

    // 字幕先按标点断句，并优先用 ASR 词级时间线得到每条字幕的绝对时间。
    QVector<TimedCaption> timedCaptions;
    for (const AsrUtterance &utterance : project.captions) {
        timedCaptions += CaptionTiming::splitUtterance(utterance);
    }

    // 1. 按时间范围裁剪并重新编码为剪映兼容的 H.264/AAC MP4。
    stepClock.start();
    const int rangeCount = project.ranges.size();
    emitStep(0, PipelineUi::StepStatus::Running, 0,
             QStringLiteral("准备裁入 %1 段视频").arg(rangeCount));
    for (int i = 0; i < rangeCount; ++i) {
        const ClipRange &range = project.ranges.at(i);
        const QString clipPath = QDir(m_workDir).filePath(
            QStringLiteral("clip_%1_%2.mp4").arg(index + 1).arg(i + 1));

        emitStep(0, PipelineUi::StepStatus::Running, 100 * i / rangeCount,
                 QStringLiteral("正在裁入第 %1/%2 段 · %3")
                     .arg(i + 1)
                     .arg(rangeCount)
                     .arg(PipelineUi::formatRange(range.startMs, range.endMs)));
        if (!FfmpegUtils::cutClip(inputPath, range.startMs, range.endMs, clipPath,
                                  error)) {
            emitFailure(0, QStringLiteral("视频裁剪失败"));
            return false;
        }
        clipPaths.append(clipPath);

        if (!hasCanvasSize) {
            int width = 0;
            int height = 0;
            if (FfmpegUtils::videoSize(clipPath, &width, &height)) {
                canvasWidth = width;
                canvasHeight = height;
                hasCanvasSize = true;
            }
        }

        // 用切片真实时长铺时间轴；偏差过大时回退理论时长（与后端一致）。
        qint64 actualMs = 0;
        const qint64 wantMs = range.durationMs();
        if (!FfmpegUtils::mediaDurationMs(clipPath, &actualMs) || actualMs <= 0
            || qAbs(actualMs - wantMs) > 2500) {
            actualMs = wantMs;
        }
        actualDurations.append(actualMs);

        emitStep(0, PipelineUi::StepStatus::Running, 100 * (i + 1) / rangeCount,
                 QStringLiteral("已裁入第 %1/%2 段 · %3")
                     .arg(i + 1)
                     .arg(rangeCount)
                     .arg(PipelineUi::formatRange(range.startMs, range.endMs)));
    }
    emitStep(0, PipelineUi::StepStatus::Done, 100,
             QStringLiteral("裁入 %1 段 · 共 %2")
                 .arg(rangeCount)
                 .arg(formatSeconds(totalDuration(project.ranges))));

    // 2. 上传切片；剪映小助手所有素材都必须用 URL 表示。
    stepClock.restart();
    emitStep(1, PipelineUi::StepStatus::Running, 0,
             QStringLiteral("准备上传 %1 个切片").arg(clipPaths.size()));
    qint64 offsetMs = 0;
    for (int i = 0; i < clipPaths.size(); ++i) {
        emitStep(1, PipelineUi::StepStatus::Running, 100 * i / clipPaths.size(),
                 QStringLiteral("上传第 %1/%2 个切片")
                     .arg(i + 1)
                     .arg(clipPaths.size()));
        QString clipUrl;
        if (!capcut->uploadFile(clipPaths.at(i), m_request.apiKey, &clipUrl, error)) {
            emitFailure(1, QStringLiteral("切片上传失败"));
            return false;
        }

        CapCutVideoInfo video;
        video.url = clipUrl;
        video.startUs = offsetMs * 1000;
        video.endUs = (offsetMs + actualDurations.at(i)) * 1000;
        videos.append(video);
        offsetMs += actualDurations.at(i);

        emitStep(1, PipelineUi::StepStatus::Running, 100 * (i + 1) / clipPaths.size(),
                 QStringLiteral("已上传第 %1/%2 个切片")
                     .arg(i + 1)
                     .arg(clipPaths.size()));
    }
    emitStep(1, PipelineUi::StepStatus::Done, 100,
             QStringLiteral("%1 个切片已上传").arg(clipPaths.size()));

    // 3. 只把被 LLM 选中句段拆分出的字幕按累计时间偏移写入；
    //    合并区间内的未选中内容不会产生字幕。
    qint64 captionOffsetMs = 0;
    // 剪映小助手把字幕写进同一条轨道，时间一旦重叠会整批失败（2019）。
    // 切片实际时长可能短于 ASR 区间（尤其是 HLS 分片 seek），上一段末尾字幕
    // 会越过下一段起点，所以重叠保护必须跨区间累计，并截到当前切片实际结尾。
    qint64 previousCaptionEndUs = -1;
    for (int i = 0; i < rangeCount; ++i) {
        const ClipRange &range = project.ranges.at(i);
        const qint64 rangeEndUs = (captionOffsetMs + actualDurations.at(i)) * 1000;
        for (const TimedCaption &timed : timedCaptions) {
            const qint64 captionStart = qMax(timed.startMs, range.startMs);
            const qint64 captionEnd = qMin(timed.endMs, range.endMs);
            // 词级时间线下单个字可能只有几十毫秒，只丢弃真正退化的超短片段。
            if (captionEnd - captionStart < 50) {
                continue;
            }
            qint64 startUs = (captionOffsetMs + (captionStart - range.startMs)) * 1000;
            qint64 endUs = qMin(
                (captionOffsetMs + (captionEnd - range.startMs)) * 1000, rangeEndUs);
            if (previousCaptionEndUs >= 0 && startUs < previousCaptionEndUs) {
                startUs = previousCaptionEndUs;
            }
            if (endUs - startUs < 50 * 1000) {
                continue;
            }
            CapCutCaption caption;
            caption.startUs = startUs;
            caption.endUs = endUs;
            caption.text = timed.text;
            captions.append(caption);
            previousCaptionEndUs = endUs;
        }
        captionOffsetMs += actualDurations.at(i);
    }

    // 4. 创建草稿，把所有裁剪片段依次添加到主视频轨。
    stepClock.restart();
    emitStep(2, PipelineUi::StepStatus::Running, 0,
             QStringLiteral("正在新建 %1×%2 工程").arg(canvasWidth).arg(canvasHeight));
    CapCutDraftInfo draft;
    if (!capcut->createDraft(canvasWidth, canvasHeight, &draft, error)) {
        emitFailure(2, QStringLiteral("创建剪映草稿失败"));
        return false;
    }
    emitStep(2, PipelineUi::StepStatus::Done, 100,
             QStringLiteral("新建 %1×%2 工程 · 保持源画幅")
                 .arg(canvasWidth)
                 .arg(canvasHeight));

    stepClock.restart();
    emitStep(3, PipelineUi::StepStatus::Running, 0,
             QStringLiteral("写入 %1 个视频片段").arg(videos.size()));
    if (!capcut->addVideos(draft.draftUrl, videos, error)) {
        emitFailure(3, QStringLiteral("添加切片资源失败"));
        return false;
    }
    emitStep(3, PipelineUi::StepStatus::Done, 100,
             QStringLiteral("%1 个视频片段已写入主轨").arg(videos.size()));

    stepClock.restart();
    emitStep(4, PipelineUi::StepStatus::Running, 0,
             QStringLiteral("逐句写入 %1 条字幕").arg(captions.size()));
    // 字体、描边、入场动画与位置每条成片随机选择一套。
    const CapCutCaptionStyle captionStyle = randomCaptionStyle();
    if (!capcut->addCaptions(draft.draftUrl, captions, captionStyle, error)) {
        emitFailure(4, QStringLiteral("添加字幕失败"));
        return false;
    }
    emitStep(4, PipelineUi::StepStatus::Done, 100,
             QStringLiteral("%1 条字幕 · %2")
                 .arg(captions.size())
                 .arg(captionStyle.font));

    // 5. 只落库草稿信息；用户点击「打开草稿文件夹」时才下载草稿文件。
    stepClock.restart();
    emitStep(5, PipelineUi::StepStatus::Running, 0, QStringLiteral("保存草稿记录"));
    ClipDraft record;
    record.sourceId = m_request.sourceId;
    record.draftId = draft.draftId;
    record.draftUrl = draft.draftUrl;
    record.title = project.title.isEmpty() ? QStringLiteral("直播切片") : project.title;
    record.description = project.description;
    record.tags = project.topics;
    if (record.tags.isEmpty()) {
        record.tags.append(QStringLiteral("直播切片"));
    }
    record.sourceStartMs = project.ranges.first().startMs;
    record.sourceEndMs = project.ranges.last().endMs;
    record.durationMs = offsetMs;
    record.createdAtMs = QDateTime::currentMSecsSinceEpoch();

    const qint64 rowId = repository->addClipDraft(record, error);
    if (rowId < 0) {
        emitFailure(5, QStringLiteral("保存草稿记录失败"));
        return false;
    }
    emitStep(5, PipelineUi::StepStatus::Done, 100,
             QStringLiteral("草稿已保存 · 可在成片结果中打开"));
    emit draftCreated(rowId);

    for (const QString &clipPath : clipPaths) {
        QFile::remove(clipPath);
    }
    return true;
}

QString LiveClipService::sourceFingerprint(const QString &inputPath) const
{
    // 文件大小 + 修改时间足以识别“同一个源文件”，避免修改文件后误用旧缓存。
    // 版本号参与指纹：字幕改用词级时间线后，旧缓存里的 utterances 没有 words，
    // 必须让它失效并重新识别一次，否则成片仍会按字符占比分摊时间。
    const QFileInfo info(inputPath);
    const QString cacheVersion = QStringLiteral("asr-v2");
    return QStringLiteral("%1:%2:%3")
        .arg(cacheVersion)
        .arg(info.size())
        .arg(info.lastModified().toMSecsSinceEpoch());
}
