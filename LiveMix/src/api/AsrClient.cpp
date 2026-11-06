// 文件作用：调用 api.jcaigc.cn 的豆包语音识别接口，并把分句结果转换为领域模型。
#include "AsrClient.h"

#include "HttpClient.h"

#include <QJsonObject>
#include <QUrl>

namespace {

// 豆包标准版：识别效果最好，支持长音频；只接受公网 URL，
// 网关内部轮询任务。整片直播可能识别十几分钟，这里预留 35 分钟 HTTP 超时，
// 与服务端 1900s 的网关超时保持一致（略留余量）。
constexpr char kModel[] = "volc.seedasr.auc";
constexpr int kAsrTimeoutMs = 35 * 60 * 1000;

QUrl transcriptionsUrl(const QString &gatewayBaseUrl)
{
    QUrl url = QUrl::fromUserInput(gatewayBaseUrl.trimmed());
    if (!url.isValid() || url.host().isEmpty()) {
        return QUrl();
    }
    QString path = url.path();
    if (!path.endsWith(QLatin1Char('/'))) {
        path += QLatin1Char('/');
    }
    url.setPath(path + QStringLiteral("audio/transcriptions"));
    return url;
}

QString errorMessage(const HttpResponse &response)
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

AsrClient::AsrClient(HttpClient *http)
    : m_http(http)
{
}

bool AsrClient::transcribe(const QString &audioUrl, const QString &gatewayBaseUrl,
                           const QString &apiKey, AsrTranscript *transcript,
                           QString *error)
{
    if (!m_http || !transcript) {
        if (error) {
            *error = QStringLiteral("ASR 客户端未初始化。");
        }
        return false;
    }
    if (audioUrl.trimmed().isEmpty() || apiKey.trimmed().isEmpty()) {
        if (error) {
            *error = QStringLiteral("ASR 需要音频 URL 与 API 密钥。");
        }
        return false;
    }

    const QUrl url = transcriptionsUrl(gatewayBaseUrl);
    if (!url.isValid() || url.host().isEmpty()) {
        if (error) {
            *error = QStringLiteral("API 地址无效：%1").arg(gatewayBaseUrl);
        }
        return false;
    }

    // 标准版只收公网地址，因此用 file_url 字段而不是 file 文件字段。
    // timestamp_granularities 与 OpenAI 一致：要求每个分句额外返回逐词时间线，
    // 字幕按标点断句后用它对齐起止时间；网关不支持时该字段会被忽略。
    const HttpResponse response = m_http->postMultipart(
        url,
        {
            {QStringLiteral("file_url"), audioUrl},
            {QStringLiteral("model"), QString::fromLatin1(kModel)},
            {QStringLiteral("language"), QStringLiteral("zh")},
            {QStringLiteral("response_format"), QStringLiteral("verbose_json")},
            {QStringLiteral("timestamp_granularities[]"), QStringLiteral("word")},
        },
        QString(), QString(), QByteArray("Bearer ") + apiKey.toUtf8(), kAsrTimeoutMs);

    if (!response.isHttpOk() || response.json.contains(QStringLiteral("error"))) {
        if (error) {
            *error = QStringLiteral("ASR 识别失败：%1").arg(errorMessage(response));
        }
        return false;
    }

    const QJsonObject json = response.json;
    // 分句时间线与逐词时间线的单位都是秒。
    const AsrTranscript result = AsrTranscript::fromOpenAiVerboseJson(json);

    // 静音音频在网关侧不算失败（返回空 text），由上层在合并后统一判断是否有内容。
    if (result.isEmpty() && !json.contains(QStringLiteral("text"))) {
        if (error) {
            *error = QStringLiteral("ASR 返回格式异常。");
        }
        return false;
    }

    *transcript = result;
    return true;
}
