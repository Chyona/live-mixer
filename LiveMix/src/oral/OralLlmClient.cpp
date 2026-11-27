// 文件作用：智能口播大模型客户端实现。
// 统一走 OpenAI 兼容的 /v1/chat/completions，模型固定 deepseek-flash。
// 提示词要求模型只返回 JSON；解析时兼容 Markdown 代码块与前后解释文字。

#include "OralLlmClient.h"

#include "../api/HttpClient.h"

#include <QBuffer>
#include <QElapsedTimer>
#include <QImage>
#include <QImageReader>
#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonParseError>
#include <QJsonValue>
#include <QThread>
#include <QUrl>
#include <QtMath>

namespace {

constexpr char kModel[] = "deepseek-flash";
// 普通分析请求的等待上限；deepseek-flash 通常几秒内返回。
constexpr int kChatTimeoutMs = 5 * 60 * 1000;
// 单次素材分析最多带多少张关键帧，避免图片 token 费用失控。
constexpr int kMaxVisionImages = 12;

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

// 从响应 content 中取文本；兼容 [{type:"text", text:"..."}] 数组形式。
QString contentText(const QJsonValue &value)
{
    if (value.isString()) {
        return value.toString();
    }
    if (value.isArray()) {
        QString text;
        for (const QJsonValue &part : value.toArray()) {
            const QJsonObject object = part.toObject();
            if (object.value(QStringLiteral("type")).toString()
                == QLatin1String("text")) {
                text += object.value(QStringLiteral("text")).toString();
            } else if (object.contains(QStringLiteral("text"))) {
                text += object.value(QStringLiteral("text")).toString();
            }
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

// 截取第一个完整 JSON 对象；模型偶尔会在 JSON 前后附带解释。
QString extractBalancedObject(const QString &text)
{
    const int start = text.indexOf(QLatin1Char('{'));
    if (start < 0) {
        return QString();
    }
    int depth = 0;
    bool inString = false;
    bool escape = false;
    for (int index = start; index < text.size(); ++index) {
        const QChar ch = text.at(index);
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
        } else if (ch == QLatin1Char('{')) {
            ++depth;
        } else if (ch == QLatin1Char('}')) {
            --depth;
            if (depth == 0) {
                return text.mid(start, index - start + 1);
            }
        }
    }
    return QString();
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

bool postChat(const QString &baseUrl, const QString &apiKey,
              const QJsonArray &messages, double temperature, QString *content,
              QString *error, const Oral::LlmClient::StatusCallback &onStatus)
{
    if (apiKey.trimmed().isEmpty()) {
        if (error) {
            *error = QStringLiteral("大模型需要 API 密钥，请先在设置中填写。");
        }
        return false;
    }
    const QUrl url = chatCompletionsUrl(baseUrl);
    if (!url.isValid() || url.host().isEmpty()) {
        if (error) {
            *error = QStringLiteral("API 地址无效：%1").arg(baseUrl);
        }
        return false;
    }

    QJsonObject payload;
    payload.insert(QStringLiteral("model"), QString::fromLatin1(kModel));
    payload.insert(QStringLiteral("messages"), messages);
    payload.insert(QStringLiteral("temperature"), temperature);
    payload.insert(QStringLiteral("stream"), false);

    // 网络抖动或上游 5xx 时最多重试 2 次；4xx 直接返回，避免重复计费。
    QString lastError;
    for (int attempt = 0; attempt < 3; ++attempt) {
        if (attempt > 0 && onStatus) {
            onStatus(QStringLiteral("大模型连接波动，正在重试（%1/2）…").arg(attempt));
        }
        const HttpResponse response = HttpClient().postJson(
            url, payload, QByteArray("Bearer ") + apiKey.toUtf8(), kChatTimeoutMs);
        if (response.isHttpOk() && !response.json.contains(QStringLiteral("error"))) {
            const QJsonArray choices =
                response.json.value(QStringLiteral("choices")).toArray();
            if (choices.isEmpty()) {
                lastError = QStringLiteral("大模型返回空结果。");
            } else {
                const QJsonObject message =
                    choices.first().toObject().value(QStringLiteral("message")).toObject();
                const QString text = contentText(message.value(QStringLiteral("content")));
                if (!text.trimmed().isEmpty()) {
                    *content = text;
                    return true;
                }
                lastError = QStringLiteral("大模型返回内容为空。");
            }
        } else {
            lastError = requestErrorMessage(response);
            const bool retryable = response.httpStatus == 429
                || response.httpStatus >= 500 || !response.networkOk;
            if (!retryable) {
                break;
            }
        }
        if (attempt < 2) {
            QThread::msleep(800u << attempt);
        }
    }
    if (error) {
        *error = QStringLiteral("大模型调用失败：%1").arg(lastError);
    }
    return false;
}

// 请求并解析 JSON 对象；解析失败时追加更严格的指令重试一次。
bool requestJsonObject(const QString &baseUrl, const QString &apiKey,
                       const QString &systemPrompt, const QJsonValue &userContent,
                       double temperature, QJsonObject *object, QString *error,
                       const Oral::LlmClient::StatusCallback &onStatus)
{
    const auto buildMessages = [&](bool strict) {
        QJsonArray messages;
        QJsonObject system;
        system.insert(QStringLiteral("role"), QStringLiteral("system"));
        QString prompt = systemPrompt;
        if (strict) {
            prompt += QStringLiteral(
                "\n\n上一次输出不是合法 JSON。本次必须只输出一个 JSON 对象，"
                "不要使用 Markdown 代码块，不要输出任何解释或前后缀。");
        }
        system.insert(QStringLiteral("content"), prompt);
        messages.append(system);

        QJsonObject user;
        user.insert(QStringLiteral("role"), QStringLiteral("user"));
        user.insert(QStringLiteral("content"), userContent);
        messages.append(user);
        return messages;
    };

    for (int attempt = 0; attempt < 2; ++attempt) {
        QString content;
        QString callError;
        if (!postChat(baseUrl, apiKey, buildMessages(attempt > 0), temperature,
                      &content, &callError, onStatus)) {
            if (error) {
                *error = callError;
            }
            return false;
        }
        const QString cleaned = stripMarkdownFence(content);
        QJsonParseError parseError;
        QJsonDocument document = QJsonDocument::fromJson(cleaned.toUtf8(), &parseError);
        if (parseError.error == QJsonParseError::NoError && document.isObject()) {
            *object = document.object();
            return true;
        }
        const QString balanced = extractBalancedObject(cleaned);
        if (!balanced.isEmpty()) {
            document = QJsonDocument::fromJson(balanced.toUtf8(), &parseError);
            if (parseError.error == QJsonParseError::NoError && document.isObject()) {
                *object = document.object();
                return true;
            }
        }
        if (onStatus) {
            onStatus(QStringLiteral("大模型输出格式异常，正在重新生成…"));
        }
    }
    if (error) {
        *error = QStringLiteral("无法解析大模型返回的 JSON。");
    }
    return false;
}

QStringList stringArray(const QJsonValue &value)
{
    QStringList values;
    const QJsonArray array = value.toArray();
    for (const QJsonValue &item : array) {
        const QString text = item.toString().trimmed();
        if (!text.isEmpty()) {
            values.append(text);
        }
    }
    return values;
}

// 统一标签格式：去重、去 #、限制长度；发布话题再补回 #。
QStringList normalizeTags(const QStringList &raw, bool withHash)
{
    QStringList tags;
    for (QString tag : raw) {
        tag = tag.trimmed();
        while (tag.startsWith(QLatin1Char('#'))) {
            tag.remove(0, 1);
        }
        tag = tag.simplified();
        if (tag.isEmpty() || tag.size() > 18) {
            continue;
        }
        const QString display = withHash ? QStringLiteral("#") + tag : tag;
        if (!tags.contains(display)) {
            tags.append(display);
        }
        if (tags.size() >= 8) {
            break;
        }
    }
    return tags;
}

// 解析「秒」或「毫秒」字段；大模型提示词要求输出秒，这里仍兼容两种。
qint64 parseTimeMs(const QJsonObject &object, const QString &secondsKey,
                   const QString &millisKey)
{
    if (object.contains(millisKey)) {
        return qRound64(object.value(millisKey).toDouble());
    }
    if (object.contains(secondsKey)) {
        return qRound64(object.value(secondsKey).toDouble() * 1000.0);
    }
    return 0;
}

QString formatClock(qint64 ms)
{
    const qint64 seconds = qMax<qint64>(0, ms) / 1000;
    return QStringLiteral("%1:%2")
        .arg(seconds / 60, 2, 10, QLatin1Char('0'))
        .arg(seconds % 60, 2, 10, QLatin1Char('0'));
}

QString buildTranscriptText(const Oral::SpeechAnalysis &speech)
{
    QString text;
    for (int index = 0; index < speech.utterances.size(); ++index) {
        const Oral::Utterance &utterance = speech.utterances.at(index);
        text += QStringLiteral("[%1] (%2 - %3) %4\n")
                    .arg(index)
                    .arg(formatClock(utterance.startMs), formatClock(utterance.endMs),
                         utterance.text.trimmed());
    }
    return text;
}

// 把关键帧压缩成 data URL；图片过大时等比缩到 512px，JPEG 质量 72。
QString imageDataUrl(const QString &path)
{
    QImageReader reader(path);
    reader.setAutoTransform(true);
    QSize sourceSize = reader.size();
    if (sourceSize.isValid()
        && (sourceSize.width() > 512 || sourceSize.height() > 512)) {
        sourceSize.scale(512, 512, Qt::KeepAspectRatio);
        reader.setScaledSize(sourceSize);
    }
    const QImage image = reader.read();
    if (image.isNull()) {
        return QString();
    }
    QByteArray bytes;
    QBuffer buffer(&bytes);
    buffer.open(QIODevice::WriteOnly);
    image.save(&buffer, "JPG", 72);
    if (bytes.isEmpty()) {
        return QString();
    }
    return QStringLiteral("data:image/jpeg;base64,")
        + QString::fromLatin1(bytes.toBase64());
}

} // namespace

namespace Oral {

LlmClient::LlmClient(HttpClient *http)
    : m_http(http)
{
    // 当前实现为每次请求创建独立 HttpClient（HttpClient 内部有网络管理器，
    // 复用需要在同一线程）；保留构造参数以兼容后续可能的长连接优化。
    Q_UNUSED(m_http);
}

QString LlmClient::modelName()
{
    return QString::fromLatin1(kModel);
}

bool LlmClient::analyzeSemantics(const QString &baseUrl, const QString &apiKey,
                                 const SpeechAnalysis &speech,
                                 SemanticAnalysis *analysis, QString *error,
                                 const StatusCallback &onStatus)
{
    if (!analysis || speech.utterances.isEmpty()) {
        if (error) {
            *error = QStringLiteral("没有可分析的语音转写。");
        }
        return false;
    }
    if (onStatus) {
        onStatus(QStringLiteral("正在进行语义分析：内容分段与主题提取…"));
    }

    const QString systemPrompt = QString::fromUtf8(R"PROMPT(你是资深短视频编导，负责把真人口播整理成可剪辑的结构化脚本。
请完成：
1. 内容分段：按叙事结构把全文连续划分为若干语义段，不重叠、覆盖全片；段落类型从 hook(开场钩子)、pain(痛点)、point(核心观点)、example(案例)、evidence(证据/数据)、conclusion(结论)、cta(行动号召)、other 中选择。
2. 主题与关键词：提取 3–6 个话题、6–12 个关键词；关键词要真正出现在转写中，便于后续字幕高亮。
3. 重点句：挑出 1–5 句值得字幕强调的句子，并给出句中需要高亮的 1–3 个词。
4. 发布文案：标题 2–20 个汉字；描述不超过 120 个汉字；4 个带 # 的话题标签。
5. 每个语义段给出 visualHint：适合搭配什么 B-Roll 画面（如产品特写、上脸效果、通勤场景）。
要求：
- 时间使用「秒」为单位的数字，必须落在 0 到总时长之间，且按时间递增。
- 只依据转写内容，不要编造未出现的事实。
- 只输出一个 JSON 对象，不要 Markdown 代码块，不要输出解释。
输出字段：
{"title":"...","description":"...","tags":["#..."],"topics":["..."],"keywords":["..."],
 "segments":[{"start":0.0,"end":8.2,"title":"...","summary":"...","type":"hook","keywords":["..."],"visualHint":"..."}],
 "emphasis":[{"start":2.0,"end":5.5,"text":"...","words":["..."]}]})PROMPT");

    QString userText = QStringLiteral("## 口播转写\n总时长：%1 秒\n\n%2")
                           .arg(speech.durationMs / 1000.0, 0, 'f', 1)
                           .arg(buildTranscriptText(speech));
    if (!speech.speakers.isEmpty()) {
        userText += QStringLiteral("\n说话人：%1\n").arg(speech.speakers.join(
            QStringLiteral("、")));
    }
    userText += QStringLiteral("\n请按系统要求输出 JSON。");

    QJsonObject object;
    if (!requestJsonObject(baseUrl, apiKey, systemPrompt, QJsonValue(userText),
                           0.25, &object, error, onStatus)) {
        return false;
    }

    SemanticAnalysis result;
    result.title = object.value(QStringLiteral("title")).toString().trimmed();
    result.description =
        object.value(QStringLiteral("description")).toString().trimmed();
    result.tags = normalizeTags(stringArray(object.value(QStringLiteral("tags"))),
                                true);
    result.topics = normalizeTags(stringArray(object.value(QStringLiteral("topics"))),
                                  false);
    result.keywords =
        normalizeTags(stringArray(object.value(QStringLiteral("keywords"))), false);

    const QJsonArray segments = object.value(QStringLiteral("segments")).toArray();
    for (const QJsonValue &value : segments) {
        const QJsonObject item = value.toObject();
        SemanticSegment segment;
        segment.startMs = parseTimeMs(item, QStringLiteral("start"),
                                      QStringLiteral("startMs"));
        segment.endMs =
            parseTimeMs(item, QStringLiteral("end"), QStringLiteral("endMs"));
        // 显式钳制，避免 start+300 超过总时长时 qBound 出现 min > max。
        if (speech.durationMs > 0) {
            segment.startMs = qMax<qint64>(
                0, qMin(segment.startMs,
                        qMax<qint64>(0, speech.durationMs - 300)));
            segment.endMs = qMin(
                qMax<qint64>(segment.startMs + 300, segment.endMs),
                speech.durationMs);
        }
        segment.title = item.value(QStringLiteral("title")).toString().trimmed();
        segment.summary = item.value(QStringLiteral("summary")).toString().trimmed();
        segment.type = segmentTypeFromKey(
            item.value(QStringLiteral("type")).toString());
        segment.keywords = normalizeTags(
            stringArray(item.value(QStringLiteral("keywords"))), false);
        segment.visualHint =
            item.value(QStringLiteral("visualHint")).toString().trimmed();
        if (segment.endMs > segment.startMs) {
            result.segments.append(segment);
        }
    }

    const QJsonArray emphasis = object.value(QStringLiteral("emphasis")).toArray();
    for (const QJsonValue &value : emphasis) {
        const QJsonObject item = value.toObject();
        Emphasis entry;
        entry.startMs = parseTimeMs(item, QStringLiteral("start"),
                                    QStringLiteral("startMs"));
        entry.endMs = parseTimeMs(item, QStringLiteral("end"),
                                  QStringLiteral("endMs"));
        entry.text = item.value(QStringLiteral("text")).toString().trimmed();
        entry.words =
            normalizeTags(stringArray(item.value(QStringLiteral("words"))), false);
        if (!entry.text.isEmpty()) {
            result.emphasis.append(entry);
        }
    }

    // 兜底：模型没有给出分段时，按整段口播生成一个语义段，保证后续流程可继续。
    if (result.segments.isEmpty()) {
        SemanticSegment segment;
        segment.startMs = 0;
        segment.endMs = qMax<qint64>(1000, speech.durationMs);
        segment.title = QStringLiteral("口播内容");
        segment.summary = result.description.isEmpty() ? speech.text.left(60)
                                                       : result.description;
        segment.type = SegmentType::Point;
        segment.keywords = result.keywords.mid(0, 6);
        result.segments.append(segment);
    }
    if (result.title.isEmpty()) {
        result.title = result.segments.first().title.isEmpty()
            ? QStringLiteral("口播包装")
            : result.segments.first().title;
    }
    if (result.tags.isEmpty()) {
        result.tags = normalizeTags(result.topics, true);
    }
    if (result.keywords.isEmpty()) {
        for (const SemanticSegment &segment : result.segments) {
            for (const QString &keyword : segment.keywords) {
                if (!result.keywords.contains(keyword)) {
                    result.keywords.append(keyword);
                }
            }
        }
    }
    if (result.description.isEmpty()) {
        result.description = speech.text.left(120);
    }

    *analysis = result;
    return true;
}

bool LlmClient::analyzeMaterials(const QString &baseUrl, const QString &apiKey,
                                 const QVector<MaterialProfile> &inputs,
                                 QVector<MaterialProfile> *profiles, QString *error,
                                 const StatusCallback &onStatus)
{
    if (!profiles || inputs.isEmpty()) {
        if (error) {
            *error = QStringLiteral("没有可分析的素材。");
        }
        return false;
    }
    if (onStatus) {
        onStatus(QStringLiteral("正在理解 %1 个素材的画面与用途…")
                     .arg(inputs.size()));
    }

    // 先构造纯文本素材清单；关键帧作为额外图片内容附在后面。
    QString profileText;
    QJsonArray imageParts;
    int imageCount = 0;
    for (int index = 0; index < inputs.size(); ++index) {
        const MaterialProfile &material = inputs.at(index);
        profileText += QStringLiteral("[%1] id=%2\n")
                           .arg(index + 1)
                           .arg(material.id);
        profileText += QStringLiteral("名称：%1\n类型：%2\n")
                           .arg(material.name,
                                material.kind == MaterialKind::Video
                                    ? QStringLiteral("视频")
                                    : QStringLiteral("图片"));
        if (material.width > 0 && material.height > 0) {
            profileText += QStringLiteral("尺寸：%1×%2\n")
                               .arg(material.width)
                               .arg(material.height);
        }
        if (material.durationMs > 0) {
            profileText += QStringLiteral("时长：%1 秒\n")
                               .arg(material.durationMs / 1000.0, 0, 'f', 1);
        }
        const VisualFeatures &visual = material.visual;
        profileText += QStringLiteral(
                           "画面特征：主色 %1 / %2 / %3；亮度 %4；饱和度 %5；"
                           "细节密度 %6；运动量 %7\n")
                           .arg(visual.dominantColor1, visual.dominantColor2,
                                visual.dominantColor3)
                           .arg(visual.brightness, 0, 'f', 2)
                           .arg(visual.saturation, 0, 'f', 2)
                           .arg(visual.detail, 0, 'f', 2)
                           .arg(visual.motion, 0, 'f', 2);
        profileText += QLatin1Char('\n');

        if (imageCount < kMaxVisionImages && !material.keyframePath.isEmpty()) {
            const QString dataUrl = imageDataUrl(material.keyframePath);
            if (!dataUrl.isEmpty()) {
                QJsonObject labelPart;
                labelPart.insert(QStringLiteral("type"), QStringLiteral("text"));
                labelPart.insert(QStringLiteral("text"),
                                 QStringLiteral("素材 %1（id=%2）的关键帧：")
                                     .arg(material.name, material.id));
                imageParts.append(labelPart);
                QJsonObject imageUrl;
                imageUrl.insert(QStringLiteral("url"), dataUrl);
                QJsonObject imagePart;
                imagePart.insert(QStringLiteral("type"), QStringLiteral("image_url"));
                imagePart.insert(QStringLiteral("image_url"), imageUrl);
                imageParts.append(imagePart);
                ++imageCount;
            }
        }
    }

    const QString systemPrompt = QString::fromUtf8(R"PROMPT(你是短视频素材理解助手。用户会给出口播视频的相关素材（图片/视频）文件信息、本地画面特征，并尽量附带每个素材的关键帧图片。
请逐个判断素材画面的真实内容与可用场景：
- description：一句话描述画面内容（例如「白色防晒霜瓶身放在浅色桌面上」），不要编造看不出来的信息。
- tags：3–6 个中文标签，优先写具体物体、场景、质感、颜色、动作。
- role：从 product(产品/物品)、scene(场景/环境)、detail(细节/质地)、person(人物)、text(文字/截图)、other 中选择最贴切的一个。
- bestFor：适合搭配的口播内容类型，2–4 个短词。
- quality：0–100，画面清晰、主体明确的素材给高分；模糊、过暗、信息杂乱的给低分。
如果没有关键帧或无法确认画面内容，只根据文件名与画面特征做谨慎判断，不确定的标签不要写。
只输出一个 JSON 对象，不要 Markdown，不要解释：
{"materials":[{"id":"...","description":"...","tags":["..."],"role":"product","bestFor":["..."],"quality":85}]})PROMPT");

    QJsonValue userContent;
    QJsonArray userParts;
    QJsonObject textPart;
    textPart.insert(QStringLiteral("type"), QStringLiteral("text"));
    textPart.insert(QStringLiteral("text"),
                    QStringLiteral("## 素材清单\n%1\n请输出 JSON。").arg(profileText));
    userParts.append(textPart);
    for (const QJsonValue &part : imageParts) {
        userParts.append(part);
    }
    if (userParts.size() > 1) {
        userContent = QJsonValue(userParts);
    } else {
        userContent = QJsonValue(QStringLiteral("## 素材清单\n%1\n请输出 JSON。")
                                     .arg(profileText));
    }

    QJsonObject object;
    QString analyzeError;
    if (!requestJsonObject(baseUrl, apiKey, systemPrompt, userContent, 0.2,
                           &object, &analyzeError, onStatus)) {
        // 图片输入被模型拒绝时，降级为纯文本再试一次。
        if (userParts.size() > 1) {
            if (onStatus) {
                onStatus(QStringLiteral(
                    "当前模型不支持图片输入，已降级为文本素材理解…"));
            }
            const QJsonValue textOnly =
                QStringLiteral("## 素材清单\n%1\n请输出 JSON。").arg(profileText);
            if (requestJsonObject(baseUrl, apiKey, systemPrompt, textOnly, 0.2,
                                  &object, &analyzeError, onStatus)) {
                // 继续往下解析。
            } else {
                if (error) {
                    *error = analyzeError;
                }
                return false;
            }
        } else {
            if (error) {
                *error = analyzeError;
            }
            return false;
        }
    }

    // 以输入为底稿：模型漏掉的素材保留本地特征，不让整个流程失败。
    QVector<MaterialProfile> result = inputs;
    QHash<QString, int> indexById;
    for (int index = 0; index < result.size(); ++index) {
        indexById.insert(result.at(index).id, index);
    }
    const QJsonArray materials = object.value(QStringLiteral("materials")).toArray();
    for (const QJsonValue &value : materials) {
        const QJsonObject item = value.toObject();
        const QString id = item.value(QStringLiteral("id")).toString().trimmed();
        const auto it = indexById.constFind(id);
        if (it == indexById.constEnd()) {
            continue;
        }
        MaterialProfile &profile = result[it.value()];
        profile.description =
            item.value(QStringLiteral("description")).toString().trimmed();
        profile.tags = normalizeTags(
            stringArray(item.value(QStringLiteral("tags"))), false);
        profile.role = materialRoleKey(materialRoleFromKey(
            item.value(QStringLiteral("role")).toString()));
        profile.bestFor = normalizeTags(
            stringArray(item.value(QStringLiteral("bestFor"))), false);
        profile.quality = qBound(0, item.value(QStringLiteral("quality")).toInt(70),
                                 100);
    }
    // 模型没有给出任何有效字段时补一个可用的默认描述。
    for (MaterialProfile &profile : result) {
        if (profile.description.isEmpty()) {
            profile.description = profile.name;
        }
        if (profile.tags.isEmpty()) {
            profile.tags.append(profile.kind == MaterialKind::Video
                                    ? QStringLiteral("视频素材")
                                    : QStringLiteral("图片素材"));
        }
        if (profile.quality <= 0) {
            profile.quality = 60;
        }
    }
    *profiles = result;
    return true;
}

bool LlmClient::planShots(const QString &baseUrl, const QString &apiKey,
                          const SpeechAnalysis &speech,
                          const SemanticAnalysis &semantics,
                          const QVector<MaterialProfile> &materials,
                          int targetCount, QVector<Shot> *shots, QString *error,
                          const StatusCallback &onStatus)
{
    if (!shots || materials.isEmpty() || targetCount <= 0) {
        if (error) {
            *error = QStringLiteral("没有可用于镜头规划的素材。");
        }
        return false;
    }
    if (onStatus) {
        onStatus(QStringLiteral("正在规划 %1 处 B-Roll 镜头…").arg(targetCount));
    }

    QString segmentText;
    for (int index = 0; index < semantics.segments.size(); ++index) {
        const SemanticSegment &segment = semantics.segments.at(index);
        segmentText += QStringLiteral("[%1] %2 - %3s · %4\n")
                           .arg(index)
                           .arg(formatClock(segment.startMs),
                                formatClock(segment.endMs), segment.title);
        if (!segment.summary.isEmpty()) {
            segmentText += QStringLiteral("摘要：%1\n").arg(segment.summary);
        }
        if (!segment.keywords.isEmpty()) {
            segmentText += QStringLiteral("关键词：%1\n")
                               .arg(segment.keywords.join(QStringLiteral("、")));
        }
        if (!segment.visualHint.isEmpty()) {
            segmentText += QStringLiteral("画面建议：%1\n").arg(segment.visualHint);
        }
        segmentText += QLatin1Char('\n');
    }

    QString materialText;
    for (const MaterialProfile &material : materials) {
        materialText += QStringLiteral(
                            "id=%1 · %2 · %3 · 标签：%4 · 用途：%5 · 质量：%6\n")
                            .arg(material.id, material.name,
                                 material.kind == MaterialKind::Video
                                     ? QStringLiteral("视频")
                                     : QStringLiteral("图片"),
                                 material.tags.join(QStringLiteral("、")),
                                 material.role)
                            .arg(material.quality);
        if (!material.description.isEmpty()) {
            materialText += QStringLiteral("描述：%1\n").arg(material.description);
        }
    }

    const QString systemPrompt = QString::fromUtf8(R"PROMPT(你是资深短视频剪辑师，为真人口播规划 B-Roll 镜头（在口播画面上插入相关素材画面）。
硬性要求：
1. 只规划 %TARGET% 个镜头，不多不少；按时间从早到晚排序。
2. 每个镜头 1.5–4.0 秒；镜头之间不能重叠；避开片头 0.8 秒和片尾 0.8 秒。
3. 只在内容值得强调的位置插入：产品特写、上脸/使用效果、场景补充、数据/文字、人物反应等。
4. 优先选与语义段关键词/画面建议匹配的素材；素材可以重复使用，但相邻镜头不要用同一素材。
5. 视频素材可给 sourceIn（从素材第几秒开始截取），确保截取后仍能覆盖镜头时长。
6. transition 从「叠化」「渐显」「硬切」中选择；不确定时用「叠化」。
7. 只依据给定的语义段和素材，不要编造素材 id 或时间段。
8. 只输出一个 JSON 对象，不要 Markdown，不要解释。
输出格式：
{"shots":[{"start":12.4,"end":15.1,"materialId":"m1","shotType":"detail","transition":"叠化","sourceIn":2.0,"reason":"..."}]}
shotType 从 cutaway、detail、scene、product、person、other 中选择。)PROMPT");
    QString prompt = systemPrompt;
    prompt.replace(QStringLiteral("%TARGET%"), QString::number(targetCount));

    const QString userText = QStringLiteral(
                                 "## 口播信息\n总时长：%1 秒\n\n"
                                 "## 语义分段\n%2\n"
                                 "## 可用素材\n%3\n"
                                 "请输出 %4 个镜头的 JSON。")
                                 .arg(speech.durationMs / 1000.0, 0, 'f', 1)
                                 .arg(segmentText, materialText)
                                 .arg(targetCount);

    QJsonObject object;
    if (!requestJsonObject(baseUrl, apiKey, prompt, QJsonValue(userText), 0.4,
                           &object, error, onStatus)) {
        return false;
    }

    QVector<Shot> result;
    const QJsonArray shotArray = object.value(QStringLiteral("shots")).toArray();
    for (const QJsonValue &value : shotArray) {
        const QJsonObject item = value.toObject();
        Shot shot;
        shot.startMs = parseTimeMs(item, QStringLiteral("start"),
                                   QStringLiteral("startMs"));
        shot.endMs = parseTimeMs(item, QStringLiteral("end"),
                                 QStringLiteral("endMs"));
        shot.materialId =
            item.value(QStringLiteral("materialId")).toString().trimmed();
        shot.shotType = shotTypeFromKey(
            item.value(QStringLiteral("shotType")).toString());
        shot.transition =
            item.value(QStringLiteral("transition")).toString().trimmed();
        shot.reason = item.value(QStringLiteral("reason")).toString().trimmed();
        shot.sourceInMs = qRound64(
            item.value(QStringLiteral("sourceIn")).toDouble() * 1000.0);
        if (shot.endMs <= shot.startMs) {
            shot.endMs = shot.startMs + 2800;
        }
        shot.startMs = qBound<qint64>(800, shot.startMs,
                                      qMax<qint64>(800, speech.durationMs - 800));
        shot.endMs = qBound<qint64>(shot.startMs + 1200, shot.endMs,
                                    qMax<qint64>(shot.startMs + 1200,
                                                 speech.durationMs - 800));
        if (shot.materialId.isEmpty()) {
            continue;
        }
        result.append(shot);
    }
    *shots = result;
    return true;
}

bool LlmClient::selectSfx(const QString &baseUrl, const QString &apiKey,
                          const QVector<SubtitleCue> &cues,
                          const QVector<SfxCategoryHint> &categories,
                          QVector<int> *categoriesByCue, QString *error,
                          const StatusCallback &onStatus)
{
    if (!categoriesByCue || cues.isEmpty() || categories.isEmpty()) {
        if (error) {
            *error = QStringLiteral("没有可用的字幕或音效类别。");
        }
        return false;
    }
    if (onStatus) {
        onStatus(QStringLiteral("正在为 %1 条字幕挑选音效…").arg(cues.size()));
    }

    QString categoryText;
    for (int index = 0; index < categories.size(); ++index) {
        categoryText += QStringLiteral("%1 - %2（适用：%3）\n")
                            .arg(index)
                            .arg(categories.at(index).name,
                                 categories.at(index).usageHint);
    }
    QString cueText;
    for (int index = 0; index < cues.size(); ++index) {
        cueText += QStringLiteral("[%1] %2 - %3s %4\n")
                       .arg(index)
                       .arg(formatClock(cues.at(index).startMs),
                            formatClock(cues.at(index).endMs),
                            cues.at(index).text);
    }

    const QString systemPrompt = QString::fromUtf8(R"PROMPT(你是短视频字幕包装策划。请从字幕中稀疏挑选值得加音效的行。
原则：
1. 宁少勿多：1 分钟成片通常 3–5 处；整片最多 8 处。
2. 只对关键卖点、数字结论、场景氛围、互动号召等加音效；普通过渡、闲聊、重复叙述不加。
3. 音效类别必须与字幕语义高度匹配；没有合适匹配时可以少选或不选。
4. 只输出 JSON：{"selections":[{"index":0,"category":2}]}
index 是字幕序号，category 是音效类别序号；不要重复 index，不要输出额外解释。)PROMPT");

    const QString userText = QStringLiteral(
                                 "## 音效类别\n%1\n## 字幕\n%2\n"
                                 "建议音效总数不超过 %3 处。")
                                 .arg(categoryText, cueText)
                                 .arg(qBound(0, qRound(cues.size() * 0.2), 8));

    QJsonObject object;
    if (!requestJsonObject(baseUrl, apiKey, systemPrompt, QJsonValue(userText),
                           0.3, &object, error, onStatus)) {
        return false;
    }

    QVector<int> result(cues.size(), -1);
    const QJsonArray selections =
        object.value(QStringLiteral("selections")).toArray();
    for (const QJsonValue &value : selections) {
        const QJsonObject item = value.toObject();
        const int index = item.value(QStringLiteral("index")).toInt(-1);
        const int category = item.value(QStringLiteral("category")).toInt(-1);
        if (index >= 0 && index < result.size() && category >= 0
            && category < categories.size()) {
            result[index] = category;
        }
    }
    *categoriesByCue = result;
    return true;
}

} // namespace Oral
