// 文件作用：组装选片提示词、调用 chat/completions 并解析 indices 结果。
#include "LlmClient.h"

#include "HttpClient.h"

#include <QElapsedTimer>
#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <QJsonParseError>
#include <QJsonValue>
#include <QStringList>
#include <QThread>
#include <QTimer>
#include <QUrl>

namespace {

constexpr char kModel[] = "qwen3.8-flash";
// 流式请求的空闲超时：模型持续输出时不计时，只有连续 5 分钟没有新数据才中断。
constexpr int kLlmIdleTimeoutMs = 5 * 60 * 1000;

QUrl chatCompletionsUrl(const QString &baseUrl)
{
    QUrl url = QUrl::fromUserInput(baseUrl.trimmed());
    if (!url.isValid() || url.host().isEmpty()) {
        return QUrl();
    }
    QString path = url.path();
    if (!path.endsWith(QLatin1Char('/'))) {
        path += QLatin1Char('/');
    }
    url.setPath(path + QStringLiteral("chat/completions"));
    return url;
}

// 与后端 ai_slice_worker.go 的 aiSliceUserPromptOutputFormat 保持一致。
QString outputFormat()
{
    return QString::fromUtf8(R"PROMPT(## 输出格式
- 仅输出一个 JSON 对象，不要输出任何额外文字、注释或角色标记。
- 字段：
  - indices：选中的句段索引（整数数组），按原顺序递增。索引必须来自 视频ASR 列表的行号（从 0 开始）。
  - title：短视频标题，2～12 个汉字。
  - description：短视频内容介绍，128 个字以内，概括成片核心观点。
  - topics：短视频话题字符串数组，共 2～6 个；每个话题 2～12 个汉字，不要带 # 号。
- 标题、介绍与话题须依据选中句段撰写，不得编造 ASR 未出现的事实。
- 示例：{"indices":[2,5,9,13],"title":"利率上行的真相","description":"用公开数据拆解本轮加息如何影响资产价格与配置思路。","topics":["宏观经济","利率周期","资产配置"]})PROMPT");
}

// 与后端 formatASRSegmentLine 一致：`[i] (开始秒 - 结束秒) 文本`。
QString formatAsrLine(int index, const AsrUtterance &utterance)
{
    return QStringLiteral("[%1] (%2 - %3) %4")
        .arg(index)
        .arg(utterance.startMs / 1000.0, 0, 'f', 2)
        .arg(utterance.endMs / 1000.0, 0, 'f', 2)
        .arg(utterance.text.trimmed());
}

QString buildUserPrompt(const LlmSelectionRequest &request)
{
    QString prompt;
    if (request.strictJson) {
        prompt += QStringLiteral(
            "上一次输出不是合法 JSON。本次必须只输出一个 JSON 对象，"
            "不要使用 Markdown 代码块，不要输出任何解释。\n\n");
    }

    prompt += QStringLiteral("## 视频ASR\n");
    for (int i = 0; i < request.segments.size(); ++i) {
        prompt += formatAsrLine(i, request.segments.at(i));
        prompt += QLatin1Char('\n');
    }
    prompt += QLatin1Char('\n');
    prompt += outputFormat();
    return prompt;
}

QString contentText(const QJsonValue &value)
{
    if (value.isString()) {
        return value.toString();
    }
    // 少数网关会把内容拆成 [{type:"text", text:"..."}] 数组。
    if (value.isArray()) {
        QString text;
        for (const QJsonValue &part : value.toArray()) {
            text += part.toObject().value(QStringLiteral("text")).toString();
        }
        return text;
    }
    return QString();
}

QString stripMarkdownFence(QString content)
{
    content = content.trimmed();
    if (!content.startsWith(QLatin1String("```"))) {
        return content;
    }
    content.remove(0, 3);
    if (content.startsWith(QLatin1String("json"), Qt::CaseInsensitive)) {
        content.remove(0, 4);
    }
    const int end = content.lastIndexOf(QLatin1String("```"));
    if (end >= 0) {
        content.truncate(end);
    }
    return content.trimmed();
}

// 从文本中截取第一个完整的 JSON 对象或数组，忽略字符串内的括号。
QString extractBalanced(const QString &text, QChar open, QChar close)
{
    const int start = text.indexOf(open);
    if (start < 0) {
        return QString();
    }
    int depth = 0;
    bool inString = false;
    bool escape = false;
    for (int i = start; i < text.size(); ++i) {
        const QChar ch = text.at(i);
        if (inString) {
            if (escape) {
                escape = false;
            } else if (ch == QLatin1Char('\\')) {
                escape = true;
            } else if (ch == QLatin1Char('"')) {
                inString = false;
            }
            continue;
        }
        if (ch == QLatin1Char('"')) {
            inString = true;
        } else if (ch == open) {
            ++depth;
        } else if (ch == close) {
            --depth;
            if (depth == 0) {
                return text.mid(start, i - start + 1);
            }
        }
    }
    return QString();
}

QVector<int> parseIntArray(const QJsonArray &array)
{
    QVector<int> indices;
    for (const QJsonValue &value : array) {
        if (value.isDouble()) {
            indices.append(static_cast<int>(value.toDouble()));
        } else if (value.isString()) {
            bool ok = false;
            const int index = value.toString().trimmed().toInt(&ok);
            if (ok) {
                indices.append(index);
            }
        }
    }
    return indices;
}

QStringList parseStringArray(const QJsonArray &array)
{
    QStringList result;
    for (const QJsonValue &value : array) {
        const QString text = value.toString().trimmed();
        if (!text.isEmpty()) {
            result.append(text);
        }
    }
    return result;
}

bool tryParseObject(const QString &content, LlmSelectionResult *result)
{
    QJsonParseError parseError;
    const QJsonDocument document =
        QJsonDocument::fromJson(content.toUtf8(), &parseError);
    if (parseError.error != QJsonParseError::NoError || !document.isObject()) {
        return false;
    }
    const QJsonObject object = document.object();
    const bool hasIndices = object.contains(QStringLiteral("indices"));
    const bool hasIndexes = object.contains(QStringLiteral("indexes"));
    if (!hasIndices && !hasIndexes) {
        return false;
    }

    const QJsonArray indices =
        object.value(hasIndices ? QStringLiteral("indices")
                                : QStringLiteral("indexes"))
            .toArray();
    result->indices = parseIntArray(indices);
    result->title = object.value(QStringLiteral("title")).toString().trimmed();
    result->description =
        object.value(QStringLiteral("description")).toString().trimmed();
    result->topics =
        parseStringArray(object.value(QStringLiteral("topics")).toArray());
    return true;
}

bool parseSelection(const QString &rawContent, LlmSelectionResult *result,
                    QString *error)
{
    const QString content = stripMarkdownFence(rawContent);
    if (content.isEmpty()) {
        if (error) {
            *error = QStringLiteral("LLM 返回内容为空。");
        }
        return false;
    }

    if (tryParseObject(content, result)) {
        return true;
    }

    // 模型偶尔在 JSON 前后附带解释文字，截取第一个完整对象再试。
    const QString objectText = extractBalanced(content, QLatin1Char('{'),
                                               QLatin1Char('}'));
    if (!objectText.isEmpty() && objectText != content
        && tryParseObject(objectText, result)) {
        return true;
    }

    // 兼容旧输出：仅返回索引数组。
    const QString arrayText = extractBalanced(content, QLatin1Char('['),
                                              QLatin1Char(']'));
    if (!arrayText.isEmpty()) {
        QJsonParseError parseError;
        const QJsonDocument document =
            QJsonDocument::fromJson(arrayText.toUtf8(), &parseError);
        if (parseError.error == QJsonParseError::NoError && document.isArray()) {
            result->indices = parseIntArray(document.array());
            return true;
        }
    }

    if (error) {
        *error = QStringLiteral("无法解析 LLM 返回的选片 JSON：%1")
                     .arg(content.left(256));
    }
    return false;
}

QString requestErrorMessage(const HttpResponse &response)
{
    const QJsonObject error = response.json.value(QStringLiteral("error")).toObject();
    const QString message = error.value(QStringLiteral("message")).toString().trimmed();
    if (!message.isEmpty()) {
        return message;
    }
    const QString topLevel =
        response.json.value(QStringLiteral("message")).toString().trimmed();
    if (!topLevel.isEmpty()) {
        return topLevel;
    }
    if (!response.error.isEmpty()) {
        return response.error;
    }
    return QStringLiteral("HTTP %1").arg(response.httpStatus);
}

} // namespace

LlmClient::LlmClient(HttpClient *http)
    : m_http(http)
{
}

bool LlmClient::selectProject(const LlmSelectionRequest &request,
                              LlmSelectionResult *result, QString *error,
                              const StreamCallback &onThought,
                              const StreamCallback &onStatus)
{
    if (!m_http || !result) {
        if (error) {
            *error = QStringLiteral("LLM 客户端未初始化。");
        }
        return false;
    }
    if (request.apiKey.trimmed().isEmpty()) {
        if (error) {
            *error = QStringLiteral("尚未配置 API 密钥。");
        }
        return false;
    }
    const QUrl url = chatCompletionsUrl(request.baseUrl);
    if (!url.isValid() || url.host().isEmpty()) {
        if (error) {
            *error = QStringLiteral("API 地址无效：%1").arg(request.baseUrl);
        }
        return false;
    }

    QJsonArray messages;
    if (!request.systemPrompt.trimmed().isEmpty()) {
        QJsonObject systemMessage;
        systemMessage.insert(QStringLiteral("role"), QStringLiteral("system"));
        systemMessage.insert(QStringLiteral("content"), request.systemPrompt);
        messages.append(systemMessage);
    }
    QJsonObject userMessage;
    userMessage.insert(QStringLiteral("role"), QStringLiteral("user"));
    userMessage.insert(QStringLiteral("content"), buildUserPrompt(request));
    messages.append(userMessage);

    QJsonObject streamOptions;
    streamOptions.insert(QStringLiteral("include_usage"), true);

    QJsonObject payload;
    payload.insert(QStringLiteral("model"), QString::fromLatin1(kModel));
    payload.insert(QStringLiteral("messages"), messages);
    payload.insert(QStringLiteral("temperature"), 0.2);
    // 思考模式会先输出推理内容，预留足够 token 防止 JSON 被截断。
    payload.insert(QStringLiteral("max_tokens"), 4000);
    payload.insert(QStringLiteral("stream"), true);
    payload.insert(QStringLiteral("stream_options"), streamOptions);

    struct StreamState
    {
        QString content;
        QString reasoning;
        int promptTokens = 0;
        int completionTokens = 0;
        int totalTokens = 0;
    };

    const auto applyUsage = [](const QJsonObject &usage, StreamState *state) {
        const int prompt = usage.value(QStringLiteral("prompt_tokens")).toInt();
        const int completion =
            usage.value(QStringLiteral("completion_tokens")).toInt();
        const int total = usage.value(QStringLiteral("total_tokens")).toInt();
        if (prompt > 0 || completion > 0 || total > 0) {
            state->promptTokens = prompt;
            state->completionTokens = completion;
            state->totalTokens = total > 0 ? total : prompt + completion;
        }
    };

    constexpr int kMaxAttempts = 3;
    QString lastError;

    for (int attempt = 0; attempt < kMaxAttempts; ++attempt) {
        StreamState state;
        QString thoughtBuffer;
        QElapsedTimer thoughtClock;
        thoughtClock.start();
        QElapsedTimer requestClock;
        requestClock.start();
        QByteArray pending;
        bool contentStarted = false;
        bool doneSeen = false;

        // 推理增量按 120ms / 24 字限频，避免逐 token 刷爆界面事件队列。
        const auto flushThought = [&](bool force) {
            if (thoughtBuffer.isEmpty()) {
                return;
            }
            if (!force && thoughtBuffer.size() < 24
                && thoughtClock.elapsed() < 120) {
                return;
            }
            const QString text = thoughtBuffer;
            thoughtBuffer.clear();
            thoughtClock.restart();
            if (onThought) {
                onThought(text);
            }
        };

        const auto handleObject = [&](const QJsonObject &object) {
            applyUsage(object.value(QStringLiteral("usage")).toObject(), &state);
            const QJsonArray choices =
                object.value(QStringLiteral("choices")).toArray();
            for (const QJsonValue &choiceValue : choices) {
                const QJsonObject delta =
                    choiceValue.toObject()
                        .value(QStringLiteral("delta"))
                        .toObject();
                QString reasoning =
                    contentText(delta.value(QStringLiteral("reasoning_content")));
                if (reasoning.isEmpty()) {
                    reasoning =
                        contentText(delta.value(QStringLiteral("reasoning")));
                }
                if (!reasoning.isEmpty()) {
                    state.reasoning += reasoning;
                    thoughtBuffer += reasoning;
                    flushThought(false);
                }

                const QString content =
                    contentText(delta.value(QStringLiteral("content")));
                if (!content.isEmpty()) {
                    state.content += content;
                    if (!contentStarted) {
                        contentStarted = true;
                        flushThought(true);
                        if (onStatus) {
                            onStatus(QStringLiteral("正在生成选片结果…"));
                        }
                    }
                }
            }
        };

        const auto processLine = [&](QByteArray line) {
            if (line.endsWith('\r')) {
                line.chop(1);
            }
            if (!line.startsWith("data:")) {
                return;
            }
            const QByteArray data = line.mid(5).trimmed();
            if (data.isEmpty()) {
                return;
            }
            if (data == "[DONE]") {
                doneSeen = true;
                return;
            }
            QJsonParseError parseError;
            const QJsonDocument document = QJsonDocument::fromJson(data, &parseError);
            if (parseError.error == QJsonParseError::NoError
                && document.isObject()) {
                handleObject(document.object());
            }
        };

        const auto processChunk = [&](const QByteArray &chunk) {
            pending += chunk;
            int newline = -1;
            while ((newline = pending.indexOf('\n')) >= 0) {
                const QByteArray line = pending.left(newline);
                pending.remove(0, newline + 1);
                processLine(line);
            }
        };

        // 长时间没有新 token 时主动上报等待状态，让界面持续可见地“在工作”。
        QTimer idleTimer;
        idleTimer.setSingleShot(true);
        idleTimer.setInterval(8000);
        QObject::connect(&idleTimer, &QTimer::timeout, &idleTimer, [&] {
            if (onStatus) {
                onStatus(QStringLiteral("模型仍在处理中，已等待 %1 秒…")
                             .arg(requestClock.elapsed() / 1000));
            }
            idleTimer.start();
        });
        idleTimer.start();

        const HttpResponse response = m_http->postJsonStream(
            url, payload, QByteArray("Bearer ") + request.apiKey.toUtf8(),
            kLlmIdleTimeoutMs, [&](const QByteArray &chunk) {
                idleTimer.start();
                processChunk(chunk);
                return doneSeen;
            });
        idleTimer.stop();
        if (!pending.isEmpty()) {
            processLine(pending);
            pending.clear();
        }
        flushThought(true);

        if (response.isHttpOk()) {
            StreamState finalState = state;
            // 兼容网关忽略 stream=true 而直接返回完整 completion JSON 的情况。
            if (finalState.content.trimmed().isEmpty()) {
                const QJsonArray choices =
                    response.json.value(QStringLiteral("choices")).toArray();
                if (!choices.isEmpty()) {
                    const QJsonObject message =
                        choices.first()
                            .toObject()
                            .value(QStringLiteral("message"))
                            .toObject();
                    finalState.content =
                        contentText(message.value(QStringLiteral("content")));
                    QString reasoning = contentText(
                        message.value(QStringLiteral("reasoning_content")));
                    if (reasoning.isEmpty()) {
                        reasoning =
                            contentText(message.value(QStringLiteral("reasoning")));
                    }
                    if (!reasoning.isEmpty()) {
                        finalState.reasoning = reasoning;
                        if (onThought) {
                            onThought(reasoning);
                        }
                    }
                    applyUsage(
                        response.json.value(QStringLiteral("usage")).toObject(),
                        &finalState);
                }
            }
            if (finalState.totalTokens <= 0) {
                applyUsage(
                    response.json.value(QStringLiteral("usage")).toObject(),
                    &finalState);
            }
            if (finalState.content.trimmed().isEmpty()) {
                lastError = QStringLiteral("LLM 未返回最终选片结果。");
                if (error) {
                    *error = lastError;
                }
                return false;
            }

            result->reasoning = finalState.reasoning;
            result->promptTokens = finalState.promptTokens;
            result->completionTokens = finalState.completionTokens;
            result->totalTokens = finalState.totalTokens;
            return parseSelection(finalState.content.trimmed(), result, error);
        }

        lastError = QStringLiteral("LLM 请求失败：%1")
                        .arg(requestErrorMessage(response));
        const bool overloaded =
            response.httpStatus == 429
            || (response.httpStatus >= 500 && response.httpStatus < 600);
        const bool network = !response.networkOk;
        if (overloaded || network) {
            // 超时/断网只再试一次；429/5xx 最多三次，避免无限延长等待。
            const int allowedAttempts = (network && !overloaded) ? 2 : kMaxAttempts;
            if (attempt + 1 < allowedAttempts) {
                const int waitSeconds =
                    overloaded ? (attempt == 0 ? 2 : 5) : 2;
                if (onStatus) {
                    const QString reason =
                        response.httpStatus == 429
                            ? QStringLiteral("模型服务繁忙")
                            : (overloaded ? QStringLiteral("模型服务暂时不可用")
                                          : QStringLiteral("请求未完成"));
                    onStatus(QStringLiteral("%1，%2 秒后重试…")
                                 .arg(reason)
                                 .arg(waitSeconds));
                }
                QThread::msleep(static_cast<unsigned long>(waitSeconds) * 1000);
                continue;
            }
        }
        if (error) {
            *error = lastError;
        }
        return false;
    }

    if (error) {
        *error = lastError;
    }
    return false;
}
