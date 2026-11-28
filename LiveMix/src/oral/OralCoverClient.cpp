// 文件作用：智能口播封面海报生成实现。
// 统一走 OpenAI 兼容的生图网关 /images/generations：
// 请求体里的 image 字段是首帧的公网 URL，网关会透传给火山方舟完成图生图。
// 不要传 data URI：网关当前会把其中的 base64 当成 URL，上游报 invalid url。

#include "OralCoverClient.h"

#include "../api/HttpClient.h"
#include "OralCoverPrompts.h"

#include <QDir>
#include <QFileInfo>
#include <QImage>
#include <QJsonArray>
#include <QJsonObject>
#include <QRandomGenerator>
#include <QStringList>
#include <QThread>
#include <QUrl>

namespace {

// 产品指定的封面生成模型。
constexpr char kModel[] = "doubao-seedream-5-0-flash";
// 提示词 2 使用与智能口播语义分析一致的文本模型。
constexpr char kPromptModel[] = "deepseek-flash";
// 网关同步等待出图；服务器预算约 240 秒，客户端多留余量。
constexpr int kImageTimeoutMs = 5 * 60 * 1000;
constexpr int kChatTimeoutMs = 5 * 60 * 1000;

QUrl gatewayUrl(const QString &baseUrl, const QString &pathSuffix)
{
    QUrl url = QUrl::fromUserInput(baseUrl.trimmed());
    if (!url.isValid() || url.host().isEmpty()) {
        return QUrl();
    }
    QString path = url.path();
    if (!path.endsWith(QLatin1Char('/'))) {
        path += QLatin1Char('/');
    }
    url.setPath(path + pathSuffix);
    return url;
}

QUrl imageGenerationsUrl(const QString &baseUrl)
{
    return gatewayUrl(baseUrl, QStringLiteral("images/generations"));
}

QUrl chatCompletionsUrl(const QString &baseUrl)
{
    return gatewayUrl(baseUrl, QStringLiteral("chat/completions"));
}

QString requestErrorMessage(const HttpResponse &response)
{
    const QJsonObject error =
        response.json.value(QStringLiteral("error")).toObject();
    const QString message =
        error.value(QStringLiteral("message")).toString().trimmed();
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
    QString fallback = QStringLiteral("HTTP %1").arg(response.httpStatus);
    // 网关偶尔返回非 JSON 错误页；带上截断后的响应体，否则日志里只剩
    // 一个状态码，无法判断是限流、额度还是模型不可用。
    const QString body = QString::fromUtf8(response.body).simplified();
    if (!body.isEmpty()) {
        fallback += QStringLiteral("：%1").arg(body.left(300));
    }
    return fallback;
}

// 兼容 choices[0].message.content 的字符串与 [{type:"text", text:"..."}] 形式。
QString messageContentText(const QJsonValue &value)
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

// 去掉模型偶尔包在整个输出外面的 Markdown 代码块围栏。
QString stripOuterFence(QString text)
{
    text = text.trimmed();
    if (!text.startsWith(QLatin1String("```"))) {
        return text;
    }
    const int lineEnd = text.indexOf(QLatin1Char('\n'));
    if (lineEnd < 0) {
        return text;
    }
    text.remove(0, lineEnd + 1);
    const int close = text.lastIndexOf(QLatin1String("```"));
    if (close >= 0) {
        text.truncate(close);
    }
    return text.trimmed();
}

// 提示词 1 要求输出主版本 + 备选版；图生图只需要主版本的提示词 2。
QString cutAtAlternativeVersions(QString text)
{
    const QStringList markers = {
        QStringLiteral("## 备选"),
        QStringLiteral("备选B版"),
        QStringLiteral("## 一句话"),
        QStringLiteral("一句话改动建议"),
    };
    int cut = -1;
    for (const QString &marker : markers) {
        const int index = text.indexOf(marker);
        if (index >= 0 && (cut < 0 || index < cut)) {
            cut = index;
        }
    }
    if (cut >= 0) {
        text.truncate(cut);
    }
    return text.trimmed();
}

// 从「提示词2：」之后截取主版本提示词，去掉备选版本与改动建议。
QString extractPrompt2(const QString &content)
{
    const QString text = stripOuterFence(content);
    int marker = text.indexOf(QStringLiteral("提示词2"));
    if (marker < 0) {
        marker = text.indexOf(QStringLiteral("提示词 2"));
    }
    if (marker < 0) {
        // 模型偶尔直接输出 1–9 段结构，此时整体作为提示词 2。
        return cutAtAlternativeVersions(text);
    }
    int colon = text.indexOf(QStringLiteral("："), marker);
    if (colon < 0) {
        colon = text.indexOf(QLatin1Char(':'), marker);
    }
    if (colon < 0) {
        return cutAtAlternativeVersions(text);
    }
    QString body = stripOuterFence(text.mid(colon + 1));
    body = cutAtAlternativeVersions(body);
    while (body.startsWith(QLatin1Char('*')) || body.startsWith(QLatin1Char('#'))
           || body.startsWith(QLatin1Char(' '))) {
        body.remove(0, 1);
    }
    body = body.trimmed();
    while (body.endsWith(QLatin1Char('*')) || body.endsWith(QLatin1Char('#'))) {
        body.chop(1);
        body = body.trimmed();
    }
    return body;
}

// 网关返回 b64_json 时直接解码保存。
bool saveBase64Image(const QString &base64, const QString &outputPath,
                     QString *error)
{
    const QByteArray bytes = QByteArray::fromBase64(base64.toLatin1());
    QImage image;
    if (!image.loadFromData(bytes)) {
        if (error) {
            *error = QStringLiteral("图片网关返回的图片无法解析。");
        }
        return false;
    }
    if (!image.save(outputPath, "JPG", 92)) {
        if (error) {
            *error = QStringLiteral("无法保存封面图片：%1").arg(outputPath);
        }
        return false;
    }
    return true;
}

} // namespace

namespace Oral {

CoverClient::CoverClient(HttpClient *http)
    : m_http(http)
{
}

QString CoverClient::modelName()
{
    return QString::fromLatin1(kModel);
}

QString CoverClient::promptModelName()
{
    return QString::fromLatin1(kPromptModel);
}

bool CoverClient::generatePrompt(const QString &baseUrl, const QString &apiKey,
                                 const QString &oralContent, QString *prompt,
                                 QString *error, const StatusCallback &onStatus)
{
    if (!m_http) {
        if (error) {
            *error = QStringLiteral("封面提示词客户端未初始化。");
        }
        return false;
    }
    if (!prompt) {
        if (error) {
            *error = QStringLiteral("封面提示词输出参数无效。");
        }
        return false;
    }
    if (apiKey.trimmed().isEmpty()) {
        if (error) {
            *error = QStringLiteral("封面提示词生成需要 API 密钥，请先在设置中填写。");
        }
        return false;
    }
    if (oralContent.trimmed().isEmpty()) {
        if (error) {
            *error = QStringLiteral("口播内容为空，无法生成封面提示词。");
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

    QJsonArray messages;
    QJsonObject system;
    system.insert(QStringLiteral("role"), QStringLiteral("system"));
    system.insert(QStringLiteral("content"), Oral::coverDirectorPrompt());
    messages.append(system);
    QJsonObject user;
    user.insert(QStringLiteral("role"), QStringLiteral("user"));
    user.insert(QStringLiteral("content"), oralContent);
    messages.append(user);

    QJsonObject payload;
    payload.insert(QStringLiteral("model"), QString::fromLatin1(kPromptModel));
    payload.insert(QStringLiteral("messages"), messages);
    payload.insert(QStringLiteral("temperature"), 0.6);
    payload.insert(QStringLiteral("stream"), false);

    // 网络抖动或上游 5xx 时重试；4xx 直接返回，避免重复计费。
    QString lastError;
    for (int attempt = 0; attempt < 3; ++attempt) {
        if (attempt > 0 && onStatus) {
            onStatus(QStringLiteral("封面提示词连接波动，正在重试（%1/2）…")
                         .arg(attempt));
        }
        const HttpResponse response = m_http->postJson(
            url, payload, QByteArray("Bearer ") + apiKey.toUtf8(),
            kChatTimeoutMs);
        if (response.isHttpOk()
            && !response.json.contains(QStringLiteral("error"))) {
            const QJsonArray choices =
                response.json.value(QStringLiteral("choices")).toArray();
            if (!choices.isEmpty()) {
                const QJsonObject message =
                    choices.first()
                        .toObject()
                        .value(QStringLiteral("message"))
                        .toObject();
                const QString content = messageContentText(
                    message.value(QStringLiteral("content")));
                const QString prompt2 = extractPrompt2(content);
                if (!prompt2.isEmpty()) {
                    *prompt = prompt2;
                    return true;
                }
                lastError = QStringLiteral("大模型返回的提示词为空。");
            } else {
                lastError = QStringLiteral("大模型返回空结果。");
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
        *error = QStringLiteral("封面提示词生成失败：%1").arg(lastError);
    }
    return false;
}

bool CoverClient::generate(const QString &baseUrl, const QString &apiKey,
                           const QString &referenceImageUrl,
                           const QString &prompt, int width, int height,
                           const QString &outputPath, QString *error,
                           const StatusCallback &onStatus)
{
    if (!m_http) {
        if (error) {
            *error = QStringLiteral("封面生成客户端未初始化。");
        }
        return false;
    }
    if (apiKey.trimmed().isEmpty()) {
        if (error) {
            *error = QStringLiteral("封面生成需要 API 密钥，请先在设置中填写。");
        }
        return false;
    }
    if (referenceImageUrl.trimmed().isEmpty()) {
        if (error) {
            *error = QStringLiteral("封面参考图地址为空。");
        }
        return false;
    }
    if (prompt.trimmed().isEmpty()) {
        if (error) {
            *error = QStringLiteral("封面提示词为空。");
        }
        return false;
    }
    if (width <= 0 || height <= 0) {
        if (error) {
            *error = QStringLiteral("封面输出尺寸无效。");
        }
        return false;
    }
    const QUrl url = imageGenerationsUrl(baseUrl);
    if (!url.isValid() || url.host().isEmpty()) {
        if (error) {
            *error = QStringLiteral("API 地址无效：%1").arg(baseUrl);
        }
        return false;
    }
    if (!QDir().mkpath(QFileInfo(outputPath).absolutePath())) {
        if (error) {
            *error = QStringLiteral("无法创建封面目录：%1")
                         .arg(QFileInfo(outputPath).absolutePath());
        }
        return false;
    }

    QJsonObject payload;
    payload.insert(QStringLiteral("model"), QString::fromLatin1(kModel));
    payload.insert(QStringLiteral("prompt"), prompt);
    // 只传 URL：网关会把 data URI 的 base64 当 URL 解析，上游直接 400。
    payload.insert(QStringLiteral("image"), referenceImageUrl.trimmed());
    payload.insert(QStringLiteral("size"),
                   QStringLiteral("%1x%2").arg(width).arg(height));
    // 显式传随机种子，不依赖网关默认值；即使提示词与参考图完全相同，
    // 重新成片也会得到另一张封面。火山方舟种子范围为 [-1, 2147483647]。
    payload.insert(QStringLiteral("seed"),
                   QRandomGenerator::global()->bounded(1, 2147483647));
    // Seedream 5.0 Flash 每次只出一张，不支持 n / 组图参数，不要传。
    // 火山方舟的生图接口默认带「AI生成」水印；封面需要干净画面，显式关闭。
    payload.insert(QStringLiteral("watermark"), false);

    // 502/503 是网关或上游暂不可用，重试一次；504 表示同步等待超时，
    // 后台可能仍在出图并计费，不重试，避免重复扣费。
    HttpResponse response;
    for (int attempt = 0; attempt < 2; ++attempt) {
        if (attempt > 0 && onStatus) {
            onStatus(QStringLiteral("图片网关暂时不可用，正在重试…"));
        }
        response = m_http->postJson(
            url, payload, QByteArray("Bearer ") + apiKey.toUtf8(),
            kImageTimeoutMs);
        if (response.isHttpOk()
            && !response.json.contains(QStringLiteral("error"))) {
            break;
        }
        const bool retryable = response.httpStatus == 502
            || response.httpStatus == 503;
        if (!retryable || attempt == 1) {
            if (error) {
                *error = QStringLiteral("封面生成失败：%1")
                             .arg(requestErrorMessage(response));
            }
            return false;
        }
        QThread::msleep(2000);
    }

    const QJsonArray data =
        response.json.value(QStringLiteral("data")).toArray();
    if (data.isEmpty()) {
        if (error) {
            *error = QStringLiteral("封面生成失败：网关未返回图片。");
        }
        return false;
    }
    const QJsonObject item = data.first().toObject();
    const QString remoteUrl =
        item.value(QStringLiteral("url")).toString().trimmed();
    QString saveError;
    if (!remoteUrl.isEmpty()) {
        if (!m_http->downloadToFile(QUrl(remoteUrl), outputPath, &saveError,
                                    3 * 60 * 1000)) {
            if (error) {
                *error = QStringLiteral("封面图片下载失败：%1").arg(saveError);
            }
            return false;
        }
    } else {
        const QString base64 =
            item.value(QStringLiteral("b64_json")).toString();
        if (base64.isEmpty()) {
            if (error) {
                *error = QStringLiteral("封面生成失败：响应中没有图片地址。");
            }
            return false;
        }
        if (!saveBase64Image(base64, outputPath, error)) {
            return false;
        }
    }
    if (!QFileInfo::exists(outputPath) || QFileInfo(outputPath).size() <= 0) {
        if (error) {
            *error = QStringLiteral("封面图片未落盘：%1").arg(outputPath);
        }
        return false;
    }
    return true;
}

} // namespace Oral
