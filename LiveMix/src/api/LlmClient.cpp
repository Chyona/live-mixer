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

    if (!request.extraInstruction.trimmed().isEmpty()
        || !request.excludedIndices.isEmpty()) {
        prompt += QStringLiteral("## 本次选片要求\n");
        if (!request.extraInstruction.trimmed().isEmpty()) {
            prompt += request.extraInstruction.trimmed();
            prompt += QLatin1Char('\n');
        }
        if (!request.excludedIndices.isEmpty()) {
            QStringList excluded;
            excluded.reserve(request.excludedIndices.size());
            for (int index : request.excludedIndices) {
                excluded.append(QString::number(index));
            }
            prompt += QStringLiteral("- 不要选择以下已经用于其他成片的句段索引：%1\n")
                          .arg(excluded.join(QStringLiteral(", ")));
        }
        prompt += QLatin1Char('\n');
    }

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

QString translationSystemPrompt()
{
    return QString::fromUtf8(R"PROMPT(你是直播短视频的英文字幕翻译。用户会给出按序号排列的中文字幕，每条都是按标点和时间切分后的口播片段。
请逐条翻译成自然、简洁、适合做视频字幕的英文：
- 条数、序号和顺序必须与输入完全一致，不合并、不拆分、不增删。
- 每条只翻译对应那一句，尽量简短；数字、品牌名与专有名词保留可读写法。
- 不要输出解释、注释或 Markdown 代码块，只输出一个 JSON 对象。)PROMPT");
}

QString buildTranslationPrompt(const QStringList &texts, bool strictJson)
{
    QString prompt;
    if (strictJson) {
        prompt += QStringLiteral(
            "上一次输出不是合法 JSON。本次必须只输出一个 JSON 对象，"
            "不要使用 Markdown 代码块，不要输出任何解释。\n\n");
    }
    prompt += QStringLiteral("## 中文字幕\n");
    for (int i = 0; i < texts.size(); ++i) {
        prompt += QStringLiteral("[%1] %2\n").arg(i).arg(texts.at(i).trimmed());
    }
    prompt += QStringLiteral("\n## 输出格式\n"
                             "- 仅输出 JSON 对象："
                             "{\"translations\":[{\"index\":0,\"text\":\"英文译文\"}]}\n"
                             "- translations 必须包含全部 %1 条，index 与输入序号一一对应，"
                             "text 只放英文译文。\n")
                  .arg(texts.size());
    return prompt;
}

int applyTranslationValue(const QJsonValue &value, int index, int count,
                          QStringList *translations)
{
    if (!value.isString() || index < 0 || index >= count) {
        return 0;
    }
    const QString text = value.toString().simplified();
    if (text.isEmpty()) {
        return 0;
    }
    (*translations)[index] = text;
    return 1;
}

int applyTranslationArray(const QJsonArray &array, int count, QStringList *translations)
{
    int filled = 0;
    int positional = 0;
    for (const QJsonValue &value : array) {
        if (value.isObject()) {
            const QJsonObject object = value.toObject();
            int index = positional;
            static const QStringList indexKeys = {
                QStringLiteral("index"), QStringLiteral("id"),
                QStringLiteral("i"),     QStringLiteral("no"),
            };
            for (const QString &key : indexKeys) {
                const QJsonValue indexValue = object.value(key);
                if (indexValue.isDouble()) {
                    index = static_cast<int>(indexValue.toDouble());
                    break;
                }
                if (indexValue.isString()) {
                    bool ok = false;
                    const int parsed = indexValue.toString().trimmed().toInt(&ok);
                    if (ok) {
                        index = parsed;
                        break;
                    }
                }
            }
            static const QStringList textKeys = {
                QStringLiteral("text"),    QStringLiteral("translation"),
                QStringLiteral("english"), QStringLiteral("en"),
                QStringLiteral("value"),
            };
            QJsonValue text;
            for (const QString &key : textKeys) {
                const QJsonValue candidate = object.value(key);
                if (candidate.isString()) {
                    text = candidate;
                    break;
                }
            }
            filled += applyTranslationValue(text, index, count, translations);
        } else {
            filled += applyTranslationValue(value, positional, count, translations);
        }
        ++positional;
    }
    return filled;
}

int applyTranslationsObject(const QJsonObject &object, int count,
                            QStringList *translations)
{
    static const QStringList arrayKeys = {
        QStringLiteral("translations"), QStringLiteral("items"),
        QStringLiteral("data"),         QStringLiteral("results"),
    };
    for (const QString &key : arrayKeys) {
        const QJsonValue value = object.value(key);
        if (value.isArray()) {
            const int filled = applyTranslationArray(value.toArray(), count, translations);
            if (filled > 0) {
                return filled;
            }
        } else if (value.isObject()) {
            // 兼容 {"translations":{"0":"...","1":"..."}} 的映射形式。
            const int filled = applyTranslationsObject(value.toObject(), count, translations);
            if (filled > 0) {
                return filled;
            }
        }
    }

    // 兼容网关直接返回 {"0":"...", "1":"..."} 的映射形式。
    int filled = 0;
    for (auto it = object.begin(); it != object.end(); ++it) {
        bool ok = false;
        const int index = it.key().trimmed().toInt(&ok);
        if (ok) {
            filled += applyTranslationValue(it.value(), index, count, translations);
        }
    }
    return filled;
}

int parseTranslations(const QString &rawContent, int count, QStringList *translations)
{
    QString content = stripMarkdownFence(rawContent);
    if (content.isEmpty()) {
        return 0;
    }

    QJsonParseError parseError;
    const auto fillFromDocument = [&](const QJsonDocument &document) {
        if (document.isObject()) {
            return applyTranslationsObject(document.object(), count, translations);
        }
        if (document.isArray()) {
            return applyTranslationArray(document.array(), count, translations);
        }
        return 0;
    };

    const QJsonDocument document = QJsonDocument::fromJson(content.toUtf8(), &parseError);
    if (parseError.error == QJsonParseError::NoError) {
        if (const int filled = fillFromDocument(document); filled > 0) {
            return filled;
        }
    }

    // 模型偶尔在 JSON 前后附带解释文字，截取第一个完整对象/数组再试。
    const QString objectText =
        extractBalanced(content, QLatin1Char('{'), QLatin1Char('}'));
    if (!objectText.isEmpty() && objectText != content) {
        const QJsonDocument inner = QJsonDocument::fromJson(objectText.toUtf8(), &parseError);
        if (parseError.error == QJsonParseError::NoError) {
            if (const int filled = fillFromDocument(inner); filled > 0) {
                return filled;
            }
        }
    }
    const QString arrayText =
        extractBalanced(content, QLatin1Char('['), QLatin1Char(']'));
    if (!arrayText.isEmpty()) {
        const QJsonDocument inner = QJsonDocument::fromJson(arrayText.toUtf8(), &parseError);
        if (parseError.error == QJsonParseError::NoError) {
            if (const int filled = fillFromDocument(inner); filled > 0) {
                return filled;
            }
        }
    }
    return 0;
}

QString sfxSystemPrompt(const QVector<LlmSfxCategory> &categories)
{
    QStringList ids;
    QStringList lines;
    ids.reserve(categories.size());
    lines.reserve(categories.size());
    for (int i = 0; i < categories.size(); ++i) {
        ids.append(QString::number(i));
        const LlmSfxCategory &category = categories.at(i);
        lines.append(QStringLiteral("%1 - %2（示例音效：%3）")
                         .arg(i)
                         .arg(category.usageHint)
                         .arg(category.name));
    }
    return QStringLiteral(
               "你是短视频字幕包装策划。你的任务是从整片字幕中稀疏挑选少量值得加音效的字幕行，"
               "而不是给每一行都配音效。\n\n"
               "可选音效类别（category 只能取以下整数之一：%1）：\n%2\n\n"
               "核心原则：\n"
               "1. 宁少勿多：音效过多会干扰观感；通常 1 分钟成片约 5 处音效即可。\n"
               "2. 只对值得提示的字幕加音效：如关键卖点、数字结论、场景氛围、互动号召等；"
               "普通过渡、闲聊、重复叙述不要加。\n"
               "3. 语义必须匹配 usage_hint：只有字幕内容与某类别的 usage_hint 高度契合时"
               "才选用该 category；没有合适匹配时可以不加任何音效。\n"
               "4. 不要凑数：若整片没有值得加音效的字幕，selections 返回空数组 []。\n\n"
               "输出要求：\n"
               "- 只返回 JSON：{\"selections\":[{\"index\":0,\"category\":2}]}\n"
               "- selections 仅包含需要加音效的字幕；每条含 index（从 0 起）与 category\n"
               "- 同一 index 不要重复出现\n"
               "- 不要输出未在列表中的 category")
        .arg(ids.join(QStringLiteral("、")))
        .arg(lines.join(QLatin1Char('\n')));
}

// 与 v3 一致：约每 60 秒 5 处音效，单条成片最多 8 处。
int recommendedSfxCount(qint64 timelineEndMs)
{
    if (timelineEndMs <= 0) {
        return 0;
    }
    return qBound(0, qRound(double(timelineEndMs) / 60000.0 * 5.0), 8);
}

QString buildSfxUserPrompt(const LlmSfxRequest &request, bool strictJson)
{
    QString prompt;
    if (strictJson) {
        prompt += QStringLiteral(
            "上一次输出不是合法 JSON。本次必须只输出一个 JSON 对象，"
            "不要使用 Markdown 代码块，不要输出任何解释。\n\n");
    }
    prompt += QStringLiteral("## 字幕\n");
    for (int i = 0; i < request.segments.size(); ++i) {
        const LlmSfxSegment &segment = request.segments.at(i);
        prompt += QStringLiteral("[%1] (%2s - %3s) %4\n")
                      .arg(i)
                      .arg(segment.startMs / 1000.0, 0, 'f', 2)
                      .arg(segment.endMs / 1000.0, 0, 'f', 2)
                      .arg(segment.text.trimmed());
    }

    const qint64 timelineEndMs =
        request.segments.isEmpty() ? 0 : request.segments.last().endMs;
    prompt += QStringLiteral(
                  "\n## 本次要求\n"
                  "- 本片字幕覆盖约 %1 秒，建议音效总数不超过 %2 处（可更少或为零）。\n"
                  "- 只选择语义与音效类别高度匹配的字幕，宁少勿多。\n")
                  .arg(timelineEndMs / 1000.0, 0, 'f', 1)
                  .arg(recommendedSfxCount(timelineEndMs));
    return prompt;
}

bool jsonIntValue(const QJsonValue &value, int *out)
{
    if (value.isDouble()) {
        *out = static_cast<int>(value.toDouble());
        return true;
    }
    if (value.isString()) {
        bool ok = false;
        const int parsed = value.toString().trimmed().toInt(&ok);
        if (ok) {
            *out = parsed;
            return true;
        }
    }
    return false;
}

bool tryParseSfxObject(const QJsonObject &object, int segmentCount,
                       int categoryCount, QVector<int> *categories)
{
    const QJsonValue selections = object.value(QStringLiteral("selections"));
    if (!selections.isArray()) {
        return false;
    }
    QVector<int> result(segmentCount, -1);
    for (const QJsonValue &value : selections.toArray()) {
        const QJsonObject item = value.toObject();
        int index = -1;
        int category = -1;
        if (!jsonIntValue(item.value(QStringLiteral("index")), &index)
            || !jsonIntValue(item.value(QStringLiteral("category")), &category)) {
            continue;
        }
        if (index < 0 || index >= segmentCount
            || category < 0 || category >= categoryCount) {
            continue;
        }
        result[index] = category;
    }
    *categories = result;
    return true;
}

bool parseSfxSelections(const QString &rawContent, int segmentCount,
                        int categoryCount, QVector<int> *categories)
{
    const QString content = stripMarkdownFence(rawContent);
    if (content.isEmpty()) {
        return false;
    }

    QJsonParseError parseError;
    const auto tryDocument = [&](const QJsonDocument &document) {
        return document.isObject()
            && tryParseSfxObject(document.object(), segmentCount, categoryCount,
                                 categories);
    };

    const QJsonDocument document =
        QJsonDocument::fromJson(content.toUtf8(), &parseError);
    if (parseError.error == QJsonParseError::NoError && tryDocument(document)) {
        return true;
    }

    // 模型偶尔在 JSON 前后附带解释文字，截取第一个完整对象再试。
    const QString objectText =
        extractBalanced(content, QLatin1Char('{'), QLatin1Char('}'));
    if (!objectText.isEmpty() && objectText != content) {
        const QJsonDocument inner =
            QJsonDocument::fromJson(objectText.toUtf8(), &parseError);
        if (parseError.error == QJsonParseError::NoError && tryDocument(inner)) {
            return true;
        }
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

bool LlmClient::translateCaptions(const LlmTranslationRequest &request,
                                  QStringList *translations, QString *error,
                                  const StreamCallback &onStatus)
{
    if (!m_http || !translations) {
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
    if (request.texts.isEmpty()) {
        translations->clear();
        return true;
    }

    const QUrl url = chatCompletionsUrl(request.baseUrl);
    if (!url.isValid() || url.host().isEmpty()) {
        if (error) {
            *error = QStringLiteral("API 地址无效：%1").arg(request.baseUrl);
        }
        return false;
    }

    constexpr int kMaxAttempts = 3;
    constexpr int kTranslationTimeoutMs = 180000;
    QString lastError;
    bool strictJson = false;

    for (int attempt = 0; attempt < kMaxAttempts; ++attempt) {
        QJsonArray messages;
        QJsonObject systemMessage;
        systemMessage.insert(QStringLiteral("role"), QStringLiteral("system"));
        systemMessage.insert(QStringLiteral("content"), translationSystemPrompt());
        messages.append(systemMessage);

        QJsonObject userMessage;
        userMessage.insert(QStringLiteral("role"), QStringLiteral("user"));
        userMessage.insert(QStringLiteral("content"),
                           buildTranslationPrompt(request.texts, strictJson));
        messages.append(userMessage);

        QJsonObject payload;
        payload.insert(QStringLiteral("model"), QString::fromLatin1(kModel));
        payload.insert(QStringLiteral("messages"), messages);
        payload.insert(QStringLiteral("temperature"), 0.1);
        // 推理模型会先消耗 reasoning token；给足余量，避免 JSON 被截断。
        payload.insert(QStringLiteral("max_tokens"),
                       qBound(2000, int(request.texts.size()) * 160, 8000));
        payload.insert(QStringLiteral("stream"), false);

        if (onStatus) {
            onStatus(attempt == 0 ? QStringLiteral("正在生成英文译文…")
                                  : QStringLiteral("英文译文格式异常，正在重试…"));
        }

        const HttpResponse response = m_http->postJson(
            url, payload, QByteArray("Bearer ") + request.apiKey.toUtf8(),
            kTranslationTimeoutMs);
        if (response.isHttpOk()) {
            QString content;
            const QJsonArray choices =
                response.json.value(QStringLiteral("choices")).toArray();
            if (!choices.isEmpty()) {
                const QJsonObject message = choices.first()
                                                .toObject()
                                                .value(QStringLiteral("message"))
                                                .toObject();
                content = contentText(message.value(QStringLiteral("content")));
            }
            if (content.trimmed().isEmpty()) {
                lastError = QStringLiteral("LLM 未返回英文译文。");
                strictJson = true;
                continue;
            }

            QStringList parsed;
            parsed.reserve(request.texts.size());
            for (int i = 0; i < request.texts.size(); ++i) {
                parsed.append(QString());
            }
            const int filled = parseTranslations(content, request.texts.size(), &parsed);
            if (filled > 0) {
                if (filled < request.texts.size()) {
                    qWarning() << "Caption translation incomplete:" << filled << "/"
                               << request.texts.size();
                }
                *translations = parsed;
                return true;
            }

            lastError = QStringLiteral("无法解析 LLM 返回的译文 JSON：%1")
                            .arg(content.left(256));
            strictJson = true;
            continue;
        }

        lastError = QStringLiteral("LLM 翻译请求失败：%1")
                        .arg(requestErrorMessage(response));
        const bool overloaded =
            response.httpStatus == 429
            || (response.httpStatus >= 500 && response.httpStatus < 600);
        const bool network = !response.networkOk;
        if (overloaded || network) {
            const int allowedAttempts = (network && !overloaded) ? 2 : kMaxAttempts;
            if (attempt + 1 < allowedAttempts) {
                const int waitSeconds = overloaded ? (attempt == 0 ? 2 : 5) : 2;
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
        break;
    }

    if (error) {
        *error = lastError;
    }
    return false;
}

bool LlmClient::selectSfx(const LlmSfxRequest &request, QVector<int> *categories,
                          QString *error, const StreamCallback &onStatus)
{
    if (!m_http || !categories) {
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

    const int segmentCount = request.segments.size();
    const int categoryCount = request.categories.size();
    if (segmentCount <= 0) {
        categories->clear();
        return true;
    }
    if (categoryCount <= 0) {
        categories->fill(-1, segmentCount);
        return true;
    }

    const QUrl url = chatCompletionsUrl(request.baseUrl);
    if (!url.isValid() || url.host().isEmpty()) {
        if (error) {
            *error = QStringLiteral("API 地址无效：%1").arg(request.baseUrl);
        }
        return false;
    }

    constexpr int kMaxAttempts = 2;
    constexpr int kSfxTimeoutMs = 180000;
    QString lastError;
    bool strictJson = false;

    for (int attempt = 0; attempt < kMaxAttempts; ++attempt) {
        QJsonArray messages;

        QJsonObject systemMessage;
        systemMessage.insert(QStringLiteral("role"), QStringLiteral("system"));
        systemMessage.insert(QStringLiteral("content"),
                             sfxSystemPrompt(request.categories));
        messages.append(systemMessage);

        QJsonObject userMessage;
        userMessage.insert(QStringLiteral("role"), QStringLiteral("user"));
        userMessage.insert(QStringLiteral("content"),
                           buildSfxUserPrompt(request, strictJson));
        messages.append(userMessage);

        QJsonObject payload;
        payload.insert(QStringLiteral("model"), QString::fromLatin1(kModel));
        payload.insert(QStringLiteral("messages"), messages);
        payload.insert(QStringLiteral("temperature"), 0.2);
        // 推理模型会先消耗 reasoning token；与 v3 一致给足余量。
        payload.insert(QStringLiteral("max_tokens"), 4000);
        payload.insert(QStringLiteral("stream"), false);

        if (onStatus) {
            onStatus(attempt == 0 ? QStringLiteral("正在挑选字幕音效…")
                                  : QStringLiteral("音效选点格式异常，正在重试…"));
        }

        const HttpResponse response = m_http->postJson(
            url, payload, QByteArray("Bearer ") + request.apiKey.toUtf8(),
            kSfxTimeoutMs);
        if (response.isHttpOk()) {
            QString content;
            const QJsonArray choices =
                response.json.value(QStringLiteral("choices")).toArray();
            if (!choices.isEmpty()) {
                const QJsonObject message = choices.first()
                                                .toObject()
                                                .value(QStringLiteral("message"))
                                                .toObject();
                content = contentText(message.value(QStringLiteral("content")));
            }
            if (content.trimmed().isEmpty()) {
                lastError = QStringLiteral("LLM 未返回音效选点。");
                strictJson = true;
                continue;
            }

            QVector<int> parsed;
            if (parseSfxSelections(content, segmentCount, categoryCount, &parsed)) {
                int assigned = 0;
                for (int category : parsed) {
                    if (category >= 0) {
                        ++assigned;
                    }
                }
                qInfo() << "LLM SFX selections:" << assigned << "/" << segmentCount;
                *categories = parsed;
                return true;
            }

            lastError = QStringLiteral("无法解析 LLM 返回的音效 JSON：%1")
                            .arg(content.left(256));
            strictJson = true;
            continue;
        }

        lastError = QStringLiteral("LLM 音效请求失败：%1")
                        .arg(requestErrorMessage(response));
        const bool overloaded =
            response.httpStatus == 429
            || (response.httpStatus >= 500 && response.httpStatus < 600);
        const bool network = !response.networkOk;
        if ((overloaded || network) && attempt + 1 < kMaxAttempts) {
            if (onStatus) {
                onStatus(overloaded ? QStringLiteral("模型服务繁忙，2 秒后重试…")
                                    : QStringLiteral("请求未完成，2 秒后重试…"));
            }
            QThread::msleep(2000);
            continue;
        }
        break;
    }

    if (error) {
        *error = lastError;
    }
    return false;
}
