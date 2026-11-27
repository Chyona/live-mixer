// 文件作用：智能口播 ASR 客户端实现。
// 首选 POST /v1/audio/transcriptions，模型 volc.bigasr.auc_turbo，本地文件走
// multipart 的 file 字段；未开通时可回退到 seedasr（file_url）或 qwen（file）。
// 响应中的 segments / words 时间单位为秒，统一转换为毫秒。

#include "OralAsrClient.h"

#include "../api/HttpClient.h"

#include <QFileInfo>
#include <QHash>
#include <QJsonArray>
#include <QJsonObject>
#include <QSet>
#include <QUrl>
#include <QtMath>

#include <algorithm>

namespace {

// 豆包极速版：同步返回，收本地文件；单文件 ≤100MB、时长 ≤2 小时。
constexpr char kModel[] = "volc.bigasr.auc_turbo";
// 极速版未开通时的回退模型：标准版只收公网 URL；qwen 收本地文件。
constexpr char kFallbackUrlModel[] = "volc.seedasr.auc";
constexpr char kFallbackFileModel[] = "qwen3-asr-flash";
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

qint64 secondsToMs(const QJsonValue &value)
{
    if (value.isDouble()) {
        return qRound64(value.toDouble() * 1000.0);
    }
    if (value.isString()) {
        bool ok = false;
        const double seconds = value.toString().toDouble(&ok);
        if (ok) {
            return qRound64(seconds * 1000.0);
        }
    }
    return 0;
}

QString speakerId(const QJsonObject &object)
{
    static const QStringList keys = {
        QStringLiteral("speaker"),
        QStringLiteral("speaker_id"),
        QStringLiteral("speakerId"),
        QStringLiteral("spk"),
        QStringLiteral("channel"),
    };
    for (const QString &key : keys) {
        const QJsonValue value = object.value(key);
        if (value.isString() && !value.toString().trimmed().isEmpty()) {
            return value.toString().trimmed();
        }
        if (value.isDouble()) {
            return QString::number(value.toInt());
        }
    }
    return QString();
}

int charCountOf(const QString &text)
{
    int count = 0;
    for (const QChar &ch : text) {
        if (!ch.isSpace()) {
            ++count;
        }
    }
    return count;
}

// 最近一次 ASR 错误是否表示「账号没有开通该模型/资源」；只有这种情况才回退，
// 网络超时、音频损坏等错误继续向上报，避免反复重试放大问题。
bool isResourceAccessError(const QString &error)
{
    const QString lower = error.toLower();
    return lower.contains(QStringLiteral("not granted"))
        || lower.contains(QStringLiteral("not_supported"))
        || lower.contains(QStringLiteral("model_not_supported"))
        || lower.contains(QStringLiteral("resource"))
        || lower.contains(QStringLiteral("permission"))
        || lower.contains(QStringLiteral("forbidden"));
}

// 把 verbose_json 响应解析成领域模型；兼容 segments / utterances / 纯 text。
bool parseTranscript(const QJsonObject &json, Oral::SpeechAnalysis *analysis,
                     QString *error)
{
    Oral::SpeechAnalysis result;
    result.language =
        json.value(QStringLiteral("language")).toString(QStringLiteral("zh"));
    result.text = json.value(QStringLiteral("text")).toString().trimmed();

    QSet<QString> rawSpeakers;
    const QJsonArray speakers = json.value(QStringLiteral("speakers")).toArray();
    for (const QJsonValue &value : speakers) {
        if (value.isString()) {
            const QString id = value.toString().trimmed();
            if (!id.isEmpty()) {
                rawSpeakers.insert(id);
            }
        } else if (value.isObject()) {
            const QString id = speakerId(value.toObject());
            if (!id.isEmpty()) {
                rawSpeakers.insert(id);
            }
        }
    }

    const QJsonArray segments = json.value(QStringLiteral("segments")).toArray();
    for (const QJsonValue &value : segments) {
        const QString id = speakerId(value.toObject());
        if (!id.isEmpty()) {
            rawSpeakers.insert(id);
        }
    }

    QStringList sortedSpeakers = rawSpeakers.values();
    sortedSpeakers.sort();
    QHash<QString, QString> speakerNames;
    for (int index = 0; index < sortedSpeakers.size(); ++index) {
        speakerNames.insert(sortedSpeakers.at(index),
                            QStringLiteral("说话人 %1").arg(index + 1));
    }
    result.speakers = speakerNames.values();
    result.speakers.sort();

    const auto appendUtterance = [&](const QJsonObject &segment) {
        Oral::Utterance utterance;
        utterance.text = segment.value(QStringLiteral("text")).toString().trimmed();
        if (utterance.text.isEmpty()) {
            return;
        }
        utterance.startMs = secondsToMs(segment.value(QStringLiteral("start")));
        utterance.endMs = secondsToMs(segment.value(QStringLiteral("end")));
        const QString rawSpeaker = speakerId(segment);
        utterance.speaker = rawSpeaker.isEmpty()
            ? (result.speakers.isEmpty() ? QStringLiteral("说话人 1")
                                         : result.speakers.first())
            : speakerNames.value(rawSpeaker, rawSpeaker);
        utterance.confidence =
            segment.value(QStringLiteral("confidence")).toDouble(-1.0);

        const QJsonArray words = segment.value(QStringLiteral("words")).toArray();
        for (const QJsonValue &wordValue : words) {
            const QJsonObject wordObject = wordValue.toObject();
            Oral::Word word;
            word.text = wordObject.value(QStringLiteral("word")).toString();
            if (word.text.isEmpty()) {
                word.text = wordObject.value(QStringLiteral("text")).toString();
            }
            word.text = word.text.trimmed();
            if (word.text.isEmpty()) {
                continue;
            }
            word.startMs = secondsToMs(wordObject.value(QStringLiteral("start")));
            word.endMs = secondsToMs(wordObject.value(QStringLiteral("end")));
            utterance.words.append(word);
        }
        if (utterance.endMs <= utterance.startMs && !utterance.words.isEmpty()) {
            utterance.startMs = utterance.words.first().startMs;
            utterance.endMs = utterance.words.last().endMs;
        }
        if (utterance.endMs <= utterance.startMs) {
            utterance.endMs = utterance.startMs + 1000;
        }
        result.utterances.append(utterance);
    };

    for (const QJsonValue &value : segments) {
        appendUtterance(value.toObject());
    }
    const QJsonArray utterances = json.value(QStringLiteral("utterances")).toArray();
    for (const QJsonValue &value : utterances) {
        appendUtterance(value.toObject());
    }

    if (result.utterances.isEmpty() && !result.text.isEmpty()) {
        Oral::Utterance utterance;
        utterance.text = result.text;
        utterance.speaker = QStringLiteral("说话人 1");
        utterance.startMs = 0;
        utterance.endMs =
            qMax<qint64>(1000, secondsToMs(json.value(QStringLiteral("duration"))));
        result.utterances.append(utterance);
    }
    if (result.utterances.isEmpty()) {
        if (error) {
            *error = QStringLiteral("语音识别没有返回可用内容，"
                                    "请确认口播视频包含清晰人声。");
        }
        return false;
    }
    if (result.speakers.isEmpty()) {
        result.speakers.append(QStringLiteral("说话人 1"));
    }
    if (result.text.isEmpty()) {
        QStringList lines;
        for (const Oral::Utterance &utterance : result.utterances) {
            lines.append(utterance.text);
        }
        result.text = lines.join(QLatin1Char('\n'));
    }
    result.durationMs = qMax(
        secondsToMs(json.value(QStringLiteral("duration"))),
        result.utterances.isEmpty() ? 0 : result.utterances.last().endMs);
    result.charCount = charCountOf(result.text);

    std::sort(result.utterances.begin(), result.utterances.end(),
              [](const Oral::Utterance &left, const Oral::Utterance &right) {
                  return left.startMs < right.startMs;
              });
    *analysis = result;
    return true;
}

} // namespace

namespace Oral {

AsrClient::AsrClient(HttpClient *http)
    : m_http(http)
{
}

QString AsrClient::modelName()
{
    return QString::fromLatin1(kModel);
}

QString AsrClient::fallbackUrlModelName()
{
    return QString::fromLatin1(kFallbackUrlModel);
}

QString AsrClient::fallbackFileModelName()
{
    return QString::fromLatin1(kFallbackFileModel);
}

bool AsrClient::transcribe(const QString &audioPath,
                           const QString &gatewayBaseUrl, const QString &apiKey,
                           SpeechAnalysis *analysis, QString *error)
{
    return transcribeWithModel(audioPath, modelName(), gatewayBaseUrl, apiKey,
                               analysis, error);
}

bool AsrClient::transcribeWithModel(const QString &audioPath,
                                    const QString &model,
                                    const QString &gatewayBaseUrl,
                                    const QString &apiKey,
                                    SpeechAnalysis *analysis, QString *error)
{
    if (!m_http || !analysis) {
        if (error) {
            *error = QStringLiteral("ASR 客户端未初始化。");
        }
        return false;
    }
    const QFileInfo audioInfo(audioPath);
    if (!audioInfo.exists() || audioInfo.size() <= 0) {
        if (error) {
            *error = QStringLiteral("待识别音频不存在：%1").arg(audioPath);
        }
        return false;
    }
    // qwen3-asr-flash 的音频要 base64 后发送，官方建议原始文件 < 7.5MB；
    // 豆包极速版上限 100MB。这里按更严格的 qwen 上限保护，避免切换模型后
    // 才发现超限。
    if (model == fallbackFileModelName() && audioInfo.size() > 7LL * 1024 * 1024) {
        if (error) {
            *error = QStringLiteral("口播音频超过回退模型 7MB 上限，"
                                    "请开通豆包极速版或标准版。");
        }
        return false;
    }
    if (apiKey.trimmed().isEmpty()) {
        if (error) {
            *error = QStringLiteral("ASR 需要 API 密钥，请先在设置中填写。");
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

    const HttpResponse response = m_http->postMultipart(
        url,
        {
            {QStringLiteral("model"), model},
            {QStringLiteral("language"), QStringLiteral("zh")},
            {QStringLiteral("response_format"), QStringLiteral("verbose_json")},
            {QStringLiteral("timestamp_granularities[]"), QStringLiteral("word")},
        },
        audioPath, QStringLiteral("audio/mpeg"),
        QByteArray("Bearer ") + apiKey.toUtf8(), kAsrTimeoutMs);

    if (!response.isHttpOk() || response.json.contains(QStringLiteral("error"))) {
        if (error) {
            *error = QStringLiteral("语音识别失败：%1").arg(errorMessage(response));
        }
        return false;
    }
    return parseTranscript(response.json, analysis, error);
}

bool AsrClient::transcribeUrl(const QString &audioUrl, const QString &model,
                              const QString &gatewayBaseUrl,
                              const QString &apiKey, SpeechAnalysis *analysis,
                              QString *error)
{
    if (!m_http || !analysis) {
        if (error) {
            *error = QStringLiteral("ASR 客户端未初始化。");
        }
        return false;
    }
    if (audioUrl.trimmed().isEmpty() || apiKey.trimmed().isEmpty()) {
        if (error) {
            *error = QStringLiteral("ASR 需要音频地址与 API 密钥。");
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
    const HttpResponse response = m_http->postMultipart(
        url,
        {
            {QStringLiteral("file_url"), audioUrl},
            {QStringLiteral("model"), model},
            {QStringLiteral("language"), QStringLiteral("zh")},
            {QStringLiteral("response_format"), QStringLiteral("verbose_json")},
            {QStringLiteral("timestamp_granularities[]"), QStringLiteral("word")},
        },
        QString(), QString(), QByteArray("Bearer ") + apiKey.toUtf8(),
        kAsrTimeoutMs);
    if (!response.isHttpOk() || response.json.contains(QStringLiteral("error"))) {
        if (error) {
            *error = QStringLiteral("语音识别失败：%1").arg(errorMessage(response));
        }
        return false;
    }
    return parseTranscript(response.json, analysis, error);
}

} // namespace Oral

// 供流水线判断是否需要回退；放在命名空间外避免头文件暴露实现细节。
namespace Oral {
bool asrErrorNeedsFallback(const QString &error)
{
    return isResourceAccessError(error);
}
} // namespace Oral
