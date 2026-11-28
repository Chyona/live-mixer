// 文件作用：智能混剪 AI 配音客户端实现。

#include "MixcutTtsClient.h"

#include "../api/HttpClient.h"
#include "../media/FfmpegUtils.h"
#include "MixcutMediaUtils.h"

#include <QCryptographicHash>
#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QJsonObject>
#include <QThread>
#include <QUrl>

namespace {

constexpr char kModel[] = "seed-tts-2.0";
constexpr int kSpeechTimeoutMs = 10 * 60 * 1000;

QUrl speechUrl(const QString &baseUrl)
{
    QUrl url = QUrl::fromUserInput(baseUrl.trimmed());
    if (!url.isValid() || url.host().isEmpty()) {
        return QUrl();
    }
    QString path = url.path();
    if (!path.endsWith(QLatin1Char('/'))) {
        path += QLatin1Char('/');
    }
    url.setPath(path + QStringLiteral("audio/speech"));
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

// 二元音色与模型必须配套：seed-tts-2.0 只能配豆包 *_uranus_bigtts。
bool voiceMatchesModel(const QString &voiceId)
{
    return voiceId.trimmed().endsWith(QLatin1String("_uranus_bigtts"));
}

} // namespace

namespace Mixcut {

QStringList splitTtsText(const QString &text, int maxChars)
{
    const int limit = qMax(80, maxChars);
    const QString cleaned = text.trimmed();
    if (cleaned.isEmpty()) {
        return {};
    }

    // 先按句号/问号/感叹号/换行切句，再尽量把多句合并进同一个请求，
    // 避免每句话都单独计费一次；单句超过上限时才硬切。
    QStringList sentences;
    QString sentence;
    for (const QChar &ch : cleaned) {
        sentence.append(ch);
        const bool end = ch == QChar(0x3002) || ch == QChar(0xFF01)
            || ch == QChar(0xFF1F) || ch == QChar(0xFF1B)
            || ch == QLatin1Char('\n');
        if (end) {
            const QString trimmed = sentence.trimmed();
            if (!trimmed.isEmpty()) {
                sentences.append(trimmed);
            }
            sentence.clear();
        }
    }
    const QString tail = sentence.trimmed();
    if (!tail.isEmpty()) {
        sentences.append(tail);
    }
    if (sentences.isEmpty()) {
        sentences.append(cleaned);
    }

    QStringList chunks;
    QString current;
    for (const QString &part : sentences) {
        if (part.size() > limit) {
            if (!current.isEmpty()) {
                chunks.append(current);
                current.clear();
            }
            for (int start = 0; start < part.size(); start += limit) {
                const QString piece = part.mid(start, limit).trimmed();
                if (!piece.isEmpty()) {
                    chunks.append(piece);
                }
            }
            continue;
        }
        if (current.isEmpty()) {
            current = part;
        } else if (current.size() + part.size() <= limit) {
            current += part;
        } else {
            chunks.append(current);
            current = part;
        }
    }
    if (!current.isEmpty()) {
        chunks.append(current);
    }
    return chunks;
}

TtsClient::TtsClient(HttpClient *http)
    : m_http(http)
{
}

QString TtsClient::modelName()
{
    return QString::fromLatin1(kModel);
}

bool TtsClient::synthesize(const QString &baseUrl, const QString &apiKey,
                           const QString &text, const QString &voiceId,
                           const QString &cacheDir, const QString &workDir,
                           QString *outputPath, qint64 *durationMs, QString *error,
                           const StatusCallback &onStatus)
{
    if (!m_http || !outputPath) {
        if (error) {
            *error = QStringLiteral("配音合成客户端未初始化。");
        }
        return false;
    }
    const QString script = text.trimmed();
    if (script.isEmpty()) {
        if (error) {
            *error = QStringLiteral("口播文案为空，无法合成配音。");
        }
        return false;
    }
    if (apiKey.trimmed().isEmpty()) {
        if (error) {
            *error = QStringLiteral("配音需要 API 密钥，请先在设置中填写。");
        }
        return false;
    }
    if (voiceId.trimmed().isEmpty()) {
        if (error) {
            *error = QStringLiteral("请选择配音音色。");
        }
        return false;
    }
    if (!voiceMatchesModel(voiceId)) {
        if (error) {
            *error = QStringLiteral(
                "音色「%1」与配音模型 %2 不配套，请在音色库重新选择豆包音色。")
                         .arg(voiceId, QString::fromLatin1(kModel));
        }
        return false;
    }
    const QUrl url = speechUrl(baseUrl);
    if (!url.isValid() || url.host().isEmpty()) {
        if (error) {
            *error = QStringLiteral("API 地址无效：%1").arg(baseUrl);
        }
        return false;
    }

    // 配音按「模型 + 音色 + 文案」缓存：重新混剪同一份文案不重复计费。
    const QString key = QString::fromLatin1(
        QCryptographicHash::hash(
            QStringLiteral("%1|%2|%3")
                .arg(QString::fromLatin1(kModel), voiceId, script)
                .toUtf8(),
            QCryptographicHash::Sha1)
            .toHex());
    const QString ttsDir = QDir(cacheDir).filePath(QStringLiteral("tts"));
    const QString cachedPath =
        QDir(ttsDir).filePath(QStringLiteral("%1.mp3").arg(key));
    qint64 cachedDuration = 0;
    if (QFileInfo::exists(cachedPath)
        && FfmpegUtils::mediaDurationMs(cachedPath, &cachedDuration)
        && cachedDuration > 0) {
        *outputPath = cachedPath;
        if (durationMs) {
            *durationMs = cachedDuration;
        }
        if (onStatus) {
            onStatus(QStringLiteral("复用已合成的配音，跳过重复合成"));
        }
        return true;
    }
    if (!QDir().mkpath(ttsDir)) {
        if (error) {
            *error = QStringLiteral("无法创建配音缓存目录：%1").arg(ttsDir);
        }
        return false;
    }

    const QStringList chunks = splitTtsText(script);
    if (chunks.isEmpty()) {
        if (error) {
            *error = QStringLiteral("口播文案没有可合成的文字。");
        }
        return false;
    }
    const QString partDir =
        QDir(workDir).filePath(QStringLiteral("tts_%1").arg(key.left(10)));
    if (!QDir().mkpath(partDir)) {
        if (error) {
            *error = QStringLiteral("无法创建配音临时目录：%1").arg(partDir);
        }
        return false;
    }

    QStringList parts;
    for (int index = 0; index < chunks.size(); ++index) {
        if (onStatus) {
            onStatus(QStringLiteral("正在合成配音 %1/%2…")
                         .arg(index + 1)
                         .arg(chunks.size()));
        }
        const QString partPath = QDir(partDir).filePath(
            QStringLiteral("part_%1.mp3").arg(index, 3, 10, QLatin1Char('0')));
        QString chunkError;
        if (!synthesizeChunk(baseUrl, apiKey, chunks.at(index), voiceId,
                             partPath, &chunkError, onStatus)) {
            if (error) {
                *error = QStringLiteral("配音合成失败：%1").arg(chunkError);
            }
            return false;
        }
        parts.append(partPath);
    }

    const QString tempPath = cachedPath + QStringLiteral(".part.mp3");
    QFile::remove(tempPath);
    QString concatError;
    if (!concatAudio(parts, tempPath, &concatError)) {
        if (error) {
            *error = QStringLiteral("配音拼接失败：%1").arg(concatError);
        }
        return false;
    }
    qint64 totalDuration = 0;
    if (!FfmpegUtils::mediaDurationMs(tempPath, &totalDuration)
        || totalDuration <= 0) {
        if (error) {
            *error = QStringLiteral("合成的配音无法读取时长，请稍后重试。");
        }
        QFile::remove(tempPath);
        return false;
    }
    QFile::remove(cachedPath);
    if (!QFile::rename(tempPath, cachedPath)) {
        // 缓存目录不可写时仍把本次结果交给流水线使用。
        *outputPath = tempPath;
    } else {
        *outputPath = cachedPath;
    }
    if (durationMs) {
        *durationMs = totalDuration;
    }
    return true;
}

bool TtsClient::synthesizeChunk(const QString &baseUrl, const QString &apiKey,
                                const QString &text, const QString &voiceId,
                                const QString &outputPath, QString *error,
                                const StatusCallback &onStatus)
{
    const QUrl url = speechUrl(baseUrl);
    QJsonObject payload;
    payload.insert(QStringLiteral("model"), QString::fromLatin1(kModel));
    payload.insert(QStringLiteral("voice"), voiceId);
    payload.insert(QStringLiteral("input"), text);
    payload.insert(QStringLiteral("response_format"), QStringLiteral("mp3"));

    QString lastError;
    for (int attempt = 0; attempt < 3; ++attempt) {
        if (attempt > 0 && onStatus) {
            onStatus(QStringLiteral("配音连接波动，正在重试（%1/2）…").arg(attempt));
        }
        const HttpResponse response = m_http->postJson(
            url, payload, QByteArray("Bearer ") + apiKey.toUtf8(),
            kSpeechTimeoutMs);
        if (response.isHttpOk() && !response.body.isEmpty()) {
            const QByteArray trimmed = response.body.trimmed();
            // 失败时网关返回 JSON 错误体（即便是 HTTP 200），成功时是音频二进制。
            if (trimmed.startsWith('{')) {
                const QJsonObject object = HttpClient::parseJsonObject(trimmed);
                if (!object.isEmpty()) {
                    lastError = object.value(QStringLiteral("error"))
                                    .toObject()
                                    .value(QStringLiteral("message"))
                                    .toString();
                    if (lastError.isEmpty()) {
                        lastError = object.value(QStringLiteral("message"))
                                        .toString();
                    }
                    if (lastError.isEmpty()) {
                        lastError = QStringLiteral("配音服务返回了错误。");
                    }
                }
            } else {
                QFile file(outputPath);
                if (!file.open(QIODevice::WriteOnly | QIODevice::Truncate)) {
                    if (error) {
                        *error = QStringLiteral("无法写入配音文件：%1")
                                     .arg(outputPath);
                    }
                    return false;
                }
                if (file.write(response.body) != response.body.size()) {
                    if (error) {
                        *error = QStringLiteral("配音文件写入不完整。");
                    }
                    return false;
                }
                file.close();
                if (QFileInfo(outputPath).size() <= 0) {
                    lastError = QStringLiteral("配音服务返回了空音频。");
                } else {
                    return true;
                }
            }
        } else {
            lastError = errorMessage(response);
        }
        const bool retryable = response.httpStatus == 429
            || response.httpStatus >= 500 || !response.networkOk
            || lastError.contains(QStringLiteral("空音频"));
        if (!retryable) {
            break;
        }
        if (attempt < 2) {
            QThread::msleep(1000u << attempt);
        }
    }
    if (error) {
        *error = lastError.isEmpty() ? QStringLiteral("配音服务调用失败。")
                                     : lastError;
    }
    return false;
}

} // namespace Mixcut
