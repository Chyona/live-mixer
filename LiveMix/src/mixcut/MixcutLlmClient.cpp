// 文件作用：智能混剪大模型客户端实现。
// 统一走 OpenAI 兼容的 /v1/chat/completions，模型固定 deepseek-flash。
// 提示词要求模型只返回 JSON；解析时兼容 Markdown 代码块与前后解释文字。

#include "MixcutLlmClient.h"

#include "../api/HttpClient.h"

#include <QBuffer>
#include <QImage>
#include <QImageReader>
#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonParseError>
#include <QJsonValue>
#include <QThread>
#include <QUrl>
#include <QtMath>

#include <algorithm>

namespace {

constexpr char kModel[] = "deepseek-flash";
constexpr int kChatTimeoutMs = 5 * 60 * 1000;
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
              QString *error, const Mixcut::LlmClient::StatusCallback &onStatus)
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

bool requestJsonObject(const QString &baseUrl, const QString &apiKey,
                       const QString &systemPrompt, const QJsonValue &userContent,
                       double temperature, QJsonObject *object, QString *error,
                       const Mixcut::LlmClient::StatusCallback &onStatus)
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

int jsonInt(const QJsonValue &value, int fallback)
{
    if (value.isDouble()) {
        return value.toInt(fallback);
    }
    if (value.isString()) {
        bool ok = false;
        const int parsed = value.toString().trimmed().toInt(&ok);
        return ok ? parsed : fallback;
    }
    return fallback;
}

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

QString buildTranscriptText(const Mixcut::SpeechAnalysis &speech)
{
    QString text;
    for (int index = 0; index < speech.utterances.size(); ++index) {
        const Mixcut::Utterance &utterance = speech.utterances.at(index);
        text += QStringLiteral("[%1] (%2 - %3) %4\n")
                    .arg(index)
                    .arg(formatClock(utterance.startMs), formatClock(utterance.endMs),
                         utterance.text.trimmed());
    }
    return text;
}

QString clipKindName(Mixcut::MaterialKind kind)
{
    return kind == Mixcut::MaterialKind::Video ? QStringLiteral("视频镜头")
                                               : QStringLiteral("图片");
}

} // namespace

namespace Mixcut {

LlmClient::LlmClient(HttpClient *http)
    : m_http(http)
{
    // 每次请求在调用线程内新建 HttpClient，避免跨线程复用网络管理器。
    Q_UNUSED(m_http);
}

QString LlmClient::modelName()
{
    return QString::fromLatin1(kModel);
}

bool LlmClient::analyzeMaterials(const QString &baseUrl, const QString &apiKey,
                                 QVector<MaterialClip> *clips, QString *error,
                                 const StatusCallback &onStatus)
{
    if (!clips || clips->isEmpty()) {
        if (error) {
            *error = QStringLiteral("没有可分析的素材镜头。");
        }
        return false;
    }
    if (onStatus) {
        onStatus(QStringLiteral("正在理解 %1 个镜头的画面与用途…")
                     .arg(clips->size()));
    }

    QString profileText;
    QJsonArray imageParts;
    int imageCount = 0;
    for (int index = 0; index < clips->size(); ++index) {
        const MaterialClip &clip = clips->at(index);
        profileText += QStringLiteral("[%1] id=%2\n")
                           .arg(index + 1)
                           .arg(clip.id);
        profileText += QStringLiteral("素材：%1\n类型：%2\n")
                           .arg(clip.assetName, clipKindName(clip.kind));
        if (clip.width > 0 && clip.height > 0) {
            profileText += QStringLiteral("尺寸：%1×%2\n")
                               .arg(clip.width)
                               .arg(clip.height);
        }
        profileText += QStringLiteral("镜头时长：%1 秒\n")
                           .arg(clip.durationMs / 1000.0, 0, 'f', 1);
        if (clip.isVideo()) {
            profileText += QStringLiteral("素材内区间：%1–%2 秒\n")
                               .arg(clip.sourceInMs / 1000.0, 0, 'f', 1)
                               .arg(clip.sourceOutMs / 1000.0, 0, 'f', 1);
        }
        const VisualFeatures &visual = clip.visual;
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

        if (imageCount < kMaxVisionImages && !clip.keyframePath.isEmpty()) {
            const QString dataUrl = imageDataUrl(clip.keyframePath);
            if (!dataUrl.isEmpty()) {
                QJsonObject labelPart;
                labelPart.insert(QStringLiteral("type"), QStringLiteral("text"));
                labelPart.insert(QStringLiteral("text"),
                                 QStringLiteral("镜头 %1（id=%2）的关键帧：")
                                     .arg(clip.assetName, clip.id));
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

    const QString systemPrompt = QString::fromUtf8(R"PROMPT(你是短视频素材整理助手。用户会把杂乱的视频素材按场景切成镜头，并提供每个镜头的文件信息、本地画面特征与关键帧。
请逐个判断镜头画面的真实内容与可用场景：
- description：一句话描述画面内容（例如「白色防晒霜瓶身放在浅色桌面上」），不要编造看不出来的信息。
- tags：3–6 个中文标签，优先写具体物体、场景、质感、颜色、动作。
- role：从 product(产品/物品)、scene(场景/环境)、detail(细节/质地)、person(人物)、text(文字/截图)、other 中选择最贴切的一个。
- bestFor：适合搭配的口播内容类型，2–4 个短词。
- quality：0–100，画面清晰、主体明确的镜头给高分；模糊、过暗、信息杂乱的给低分。
- emotion：neutral / happy / excited / calm / serious / warm / surprised。
如果没有关键帧或无法确认画面内容，只根据文件名与画面特征做谨慎判断，不确定的标签不要写。
只输出一个 JSON 对象，不要 Markdown，不要解释：
{"materials":[{"id":"...","description":"...","tags":["..."],"role":"product","bestFor":["..."],"quality":85,"emotion":"calm"}]})PROMPT");

    const QString textPrompt =
        QStringLiteral("## 镜头清单\n%1\n请输出 JSON。").arg(profileText);
    QJsonValue userContent;
    QJsonArray userParts;
    QJsonObject textPart;
    textPart.insert(QStringLiteral("type"), QStringLiteral("text"));
    textPart.insert(QStringLiteral("text"), textPrompt);
    userParts.append(textPart);
    for (const QJsonValue &part : imageParts) {
        userParts.append(part);
    }
    userContent = userParts.size() > 1 ? QJsonValue(userParts)
                                       : QJsonValue(textPrompt);

    QJsonObject object;
    QString analyzeError;
    if (!requestJsonObject(baseUrl, apiKey, systemPrompt, userContent, 0.2,
                           &object, &analyzeError, onStatus)) {
        if (userParts.size() > 1) {
            if (onStatus) {
                onStatus(QStringLiteral(
                    "当前模型不支持图片输入，已降级为文本素材理解…"));
            }
            if (!requestJsonObject(baseUrl, apiKey, systemPrompt,
                                   QJsonValue(textPrompt), 0.2, &object,
                                   &analyzeError, onStatus)) {
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

    QHash<QString, int> indexById;
    for (int index = 0; index < clips->size(); ++index) {
        indexById.insert(clips->at(index).id, index);
    }
    const QJsonArray materials = object.value(QStringLiteral("materials")).toArray();
    for (const QJsonValue &value : materials) {
        const QJsonObject item = value.toObject();
        const QString id = item.value(QStringLiteral("id")).toString().trimmed();
        const auto it = indexById.constFind(id);
        if (it == indexById.constEnd()) {
            continue;
        }
        MaterialClip &clip = (*clips)[it.value()];
        const QString description =
            item.value(QStringLiteral("description")).toString().trimmed();
        if (!description.isEmpty()) {
            clip.description = description;
        }
        const QStringList tags =
            normalizeTags(stringArray(item.value(QStringLiteral("tags"))), false);
        if (!tags.isEmpty()) {
            clip.tags = tags;
        }
        const QString roleKey =
            item.value(QStringLiteral("role")).toString().trimmed();
        if (!roleKey.isEmpty()) {
            clip.role = materialRoleFromKey(roleKey);
        }
        const QStringList bestFor =
            normalizeTags(stringArray(item.value(QStringLiteral("bestFor"))), false);
        if (!bestFor.isEmpty()) {
            clip.bestFor = bestFor;
        }
        const int quality = jsonInt(item.value(QStringLiteral("quality")), 0);
        if (quality > 0) {
            clip.quality = qBound(0, quality, 100);
        }
        const QString emotionValue =
            item.value(QStringLiteral("emotion")).toString().trimmed();
        if (!emotionValue.isEmpty()) {
            clip.emotion = emotionFromKey(emotionValue);
        }
    }
    return true;
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
        onStatus(QStringLiteral("正在进行语义分析：分段、情绪与叙事结构…"));
    }

    const QString systemPrompt = QString::fromUtf8(R"PROMPT(你是资深短视频编导，负责把 AI 口播整理成可剪辑的结构化脚本。
请完成：
1. 内容分段：按叙事结构把全文连续划分为若干语义段，不重叠、覆盖全片；段落类型从 hook(开场钩子)、pain(痛点)、point(核心观点)、example(案例)、evidence(证据/数据)、conclusion(结论)、cta(行动号召)、other 中选择。
2. 每个语义段标注：标题、摘要、关键词、适合搭配的画面（visualHint）、情绪（neutral/happy/excited/calm/serious/warm/surprised）与叙事角色（hook/setup/conflict/turn/payoff/cta/other）。
3. 叙事结构：用一句话概括全片的叙事推进，例如「钩子 → 痛点 → 方案 → 证据 → 行动」。
4. 整体情绪：从上述情绪枚举中选一个。
5. 主题与关键词：提取 3–6 个话题、6–12 个关键词；关键词必须真正出现在转写中，便于字幕高亮与素材匹配。
6. 重点句：挑出 1–5 句值得字幕强调的句子，并给出句中需要高亮的 1–3 个词。
7. 发布文案：标题 2–20 个汉字；描述不超过 120 个汉字；4 个带 # 的话题标签。
要求：
- 时间使用「秒」为单位的数字，必须落在 0 到总时长之间，且按时间递增。
- 只依据转写内容，不要编造未出现的事实。
- 只输出一个 JSON 对象，不要 Markdown 代码块，不要输出解释。
输出字段：
{"title":"...","description":"...","tags":["#..."],"topics":["..."],"keywords":["..."],
 "narrative":"...","emotion":"calm",
 "segments":[{"start":0.0,"end":8.2,"title":"...","summary":"...","type":"hook","keywords":["..."],"visualHint":"...","emotion":"excited","narrative":"hook"}],
 "emphasis":[{"start":2.0,"end":5.5,"text":"...","words":["..."]}]})PROMPT");

    QString userText = QStringLiteral("## AI 口播转写\n总时长：%1 秒\n\n%2")
                           .arg(speech.durationMs / 1000.0, 0, 'f', 1)
                           .arg(buildTranscriptText(speech));
    if (!speech.speakers.isEmpty()) {
        userText += QStringLiteral("\n说话人：%1\n")
                        .arg(speech.speakers.join(QStringLiteral("、")));
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
    result.narrative =
        object.value(QStringLiteral("narrative")).toString().trimmed();
    result.emotion =
        emotionFromKey(object.value(QStringLiteral("emotion")).toString());

    const QJsonArray segments = object.value(QStringLiteral("segments")).toArray();
    for (const QJsonValue &value : segments) {
        const QJsonObject item = value.toObject();
        SemanticSegment segment;
        segment.startMs = parseTimeMs(item, QStringLiteral("start"),
                                      QStringLiteral("startMs"));
        segment.endMs =
            parseTimeMs(item, QStringLiteral("end"), QStringLiteral("endMs"));
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
        segment.emotion =
            emotionFromKey(item.value(QStringLiteral("emotion")).toString());
        segment.narrative = narrativeRoleFromKey(
            item.value(QStringLiteral("narrative")).toString());
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

    // 兜底：模型没有给出分段时生成一个覆盖全片的语义段，流程继续。
    if (result.segments.isEmpty()) {
        SemanticSegment segment;
        segment.startMs = 0;
        segment.endMs = qMax<qint64>(1000, speech.durationMs);
        segment.title = QStringLiteral("口播内容");
        segment.summary = result.description.isEmpty() ? speech.text.left(60)
                                                       : result.description;
        segment.type = SegmentType::Point;
        segment.keywords = result.keywords.mid(0, 6);
        segment.emotion = result.emotion;
        result.segments.append(segment);
    }
    if (result.title.isEmpty()) {
        result.title = result.segments.first().title.isEmpty()
            ? QStringLiteral("智能混剪")
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
    if (result.narrative.isEmpty()) {
        result.narrative = QStringLiteral("钩子 → 观点 → 证明 → 行动");
    }

    *analysis = result;
    return true;
}

bool LlmClient::planTimeline(const QString &baseUrl, const QString &apiKey,
                             const SpeechAnalysis &speech,
                             const SemanticAnalysis &semantics,
                             const MaterialLibrary &library, int targetShots,
                             QVector<Shot> *shots, QVector<Shot> *overlays,
                             QString *error, const StatusCallback &onStatus)
{
    if (!shots || !overlays || library.assets.isEmpty() || targetShots <= 0) {
        if (error) {
            *error = QStringLiteral("没有可用于镜头规划的素材。");
        }
        return false;
    }
    if (onStatus) {
        onStatus(QStringLiteral("正在规划 %1 个主镜头与画中画…").arg(targetShots));
    }

    QString segmentText;
    for (int index = 0; index < semantics.segments.size(); ++index) {
        const SemanticSegment &segment = semantics.segments.at(index);
        segmentText += QStringLiteral("[%1] %2 - %3s · %4 · %5\n")
                           .arg(index)
                           .arg(formatClock(segment.startMs),
                                formatClock(segment.endMs), segment.title,
                                segmentTypeKey(segment.type));
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

    QString clipText;
    for (const MaterialClip &clip : library.clips()) {
        clipText += QStringLiteral(
                        "id=%1 · %2 · %3 · 时长 %4 秒 · 标签：%5 · 用途：%6 · 质量：%7\n")
                        .arg(clip.id, clip.assetName, clipKindName(clip.kind))
                        .arg(clip.durationMs / 1000.0, 0, 'f', 1)
                        .arg(clip.tags.join(QStringLiteral("、")),
                             materialRoleKey(clip.role))
                        .arg(clip.quality);
        if (!clip.description.isEmpty()) {
            clipText += QStringLiteral("描述：%1\n").arg(clip.description);
        }
        if (clip.isVideo()) {
            clipText += QStringLiteral("素材内可用区间：%1–%2 秒\n")
                            .arg(clip.sourceInMs / 1000.0, 0, 'f', 1)
                            .arg(clip.sourceOutMs / 1000.0, 0, 'f', 1);
        }
    }

    const QString systemPrompt = QString::fromUtf8(R"PROMPT(你是资深短视频剪辑师，为 AI 口播规划画面。成片没有真人口播画面，主画面完全由素材承担。
硬性要求：
1. 主镜头必须从 0 到总时长连续覆盖：不重叠、不留空；每个主镜头 1.5–6.0 秒。
2. 主镜头数量接近 %TARGET% 个（允许 ±2 个），按时间从早到晚排序。
3. 优先选标签/描述与语义段关键词、画面建议匹配的素材；素材可以重复使用，但相邻主镜头不要用同一个素材文件。
4. clipId 必须来自素材列表；视频镜头可给 sourceIn（从素材第几秒开始截取），需保证截取后能覆盖镜头时长。
5. transition 从「叠化」「闪白」「硬切」中选择；不确定时用「叠化」。
6. overlays 是可选画中画（0–4 个）：每个 2–6 秒，必须完全落在某个主镜头区间内；position 从 top_left、top_right、bottom_left、bottom_right 中选择；画中画之间不能重叠；不要与所在主镜头使用同一素材文件。
7. 只依据给定素材与语义段，不要编造 clipId 或时间段。
8. 只输出一个 JSON 对象，不要 Markdown，不要解释。
输出格式：
{"shots":[{"start":0.0,"end":3.4,"clipId":"...","transition":"叠化","sourceIn":1.2,"reason":"..."}],
 "overlays":[{"start":6.0,"end":9.5,"clipId":"...","position":"top_right","sourceIn":0.0,"reason":"..."}]})PROMPT");
    QString prompt = systemPrompt;
    prompt.replace(QStringLiteral("%TARGET%"), QString::number(targetShots));

    const QString userText = QStringLiteral(
                                 "## 口播信息\n总时长：%1 秒\n\n"
                                 "## 语义分段\n%2\n"
                                 "## 可用素材镜头\n%3\n"
                                 "请输出完整的主镜头与画中画 JSON。")
                                 .arg(speech.durationMs / 1000.0, 0, 'f', 1)
                                 .arg(segmentText, clipText);

    QJsonObject object;
    if (!requestJsonObject(baseUrl, apiKey, prompt, QJsonValue(userText), 0.35,
                           &object, error, onStatus)) {
        return false;
    }

    QHash<QString, const MaterialClip *> clipById;
    for (const MaterialAsset &asset : library.assets) {
        for (const MaterialClip &clip : asset.clips) {
            clipById.insert(clip.id, &clip);
        }
    }

    QVector<Shot> parsedShots;
    for (const QJsonValue &value :
         object.value(QStringLiteral("shots")).toArray()) {
        const QJsonObject item = value.toObject();
        Shot shot;
        shot.startMs = parseTimeMs(item, QStringLiteral("start"),
                                   QStringLiteral("startMs"));
        shot.endMs = parseTimeMs(item, QStringLiteral("end"),
                                 QStringLiteral("endMs"));
        shot.clipId = item.value(QStringLiteral("clipId")).toString().trimmed();
        if (!clipById.contains(shot.clipId) || shot.endMs <= shot.startMs) {
            continue;
        }
        shot.transition =
            item.value(QStringLiteral("transition")).toString().trimmed();
        shot.sourceInMs = qRound64(
            item.value(QStringLiteral("sourceIn")).toDouble() * 1000.0);
        shot.reason = item.value(QStringLiteral("reason")).toString().trimmed();
        parsedShots.append(shot);
    }

    QVector<Shot> parsedOverlays;
    for (const QJsonValue &value :
         object.value(QStringLiteral("overlays")).toArray()) {
        const QJsonObject item = value.toObject();
        Shot shot;
        shot.startMs = parseTimeMs(item, QStringLiteral("start"),
                                   QStringLiteral("startMs"));
        shot.endMs = parseTimeMs(item, QStringLiteral("end"),
                                 QStringLiteral("endMs"));
        shot.clipId = item.value(QStringLiteral("clipId")).toString().trimmed();
        if (!clipById.contains(shot.clipId) || shot.endMs <= shot.startMs) {
            continue;
        }
        shot.position = pipPositionFromKey(
            item.value(QStringLiteral("position")).toString());
        shot.sourceInMs = qRound64(
            item.value(QStringLiteral("sourceIn")).toDouble() * 1000.0);
        shot.reason = item.value(QStringLiteral("reason")).toString().trimmed();
        parsedOverlays.append(shot);
    }

    *shots = parsedShots;
    *overlays = parsedOverlays;
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

    const QString userText =
        QStringLiteral("## 音效类别\n%1\n## 字幕\n%2\n建议音效总数不超过 %3 处。")
            .arg(categoryText, cueText)
            .arg(qBound(0, qRound(cues.size() * 0.2), 8));

    QJsonObject object;
    if (!requestJsonObject(baseUrl, apiKey, systemPrompt, QJsonValue(userText),
                           0.3, &object, error, onStatus)) {
        return false;
    }

    QVector<int> result(cues.size(), -1);
    for (const QJsonValue &value :
         object.value(QStringLiteral("selections")).toArray()) {
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

} // namespace Mixcut
