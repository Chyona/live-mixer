// 文件作用：智能口播封面海报生成实现。
// 统一走 OpenAI 兼容的生图网关 /images/generations：
// 请求体里的 image 字段是首帧的 data URI，网关会原样透传给火山方舟完成图生图。

#include "OralCoverClient.h"

#include "../api/HttpClient.h"

#include <QBuffer>
#include <QDir>
#include <QFileInfo>
#include <QImage>
#include <QImageReader>
#include <QJsonArray>
#include <QJsonObject>
#include <QThread>
#include <QUrl>

namespace {

// 产品指定的封面生成模型。
constexpr char kModel[] = "doubao-seedream-5-0-flash";
// 网关同步等待出图；服务器预算约 240 秒，客户端多留余量。
constexpr int kImageTimeoutMs = 5 * 60 * 1000;
// 参考图长边上限：足够模型识别人物与场景，又不会让请求体过大。
constexpr int kReferenceMaxEdge = 1280;
constexpr int kReferenceQuality = 90;

QUrl imageGenerationsUrl(const QString &baseUrl)
{
    QUrl url = QUrl::fromUserInput(baseUrl.trimmed());
    if (!url.isValid() || url.host().isEmpty()) {
        return QUrl();
    }
    QString path = url.path();
    if (!path.endsWith(QLatin1Char('/'))) {
        path += QLatin1Char('/');
    }
    url.setPath(path + QStringLiteral("images/generations"));
    return url;
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
    return QStringLiteral("HTTP %1").arg(response.httpStatus);
}

// 读取参考图并编码成 data URI；长边超过 1280 时等比缩小。
bool referenceImageDataUri(const QString &path, QString *dataUri, QString *error)
{
    QImageReader reader(path);
    reader.setAutoTransform(true);
    QImage image = reader.read();
    if (image.isNull()) {
        if (error) {
            *error = QStringLiteral("无法读取口播首帧：%1")
                         .arg(QFileInfo(path).fileName());
        }
        return false;
    }
    if (image.width() > kReferenceMaxEdge || image.height() > kReferenceMaxEdge) {
        image = image.scaled(kReferenceMaxEdge, kReferenceMaxEdge,
                             Qt::KeepAspectRatio, Qt::SmoothTransformation);
    }
    QByteArray bytes;
    QBuffer buffer(&bytes);
    if (!buffer.open(QIODevice::WriteOnly)
        || !image.save(&buffer, "JPG", kReferenceQuality)) {
        if (error) {
            *error = QStringLiteral("无法编码口播首帧。");
        }
        return false;
    }
    *dataUri = QStringLiteral("data:image/jpeg;base64,")
        + QString::fromLatin1(bytes.toBase64());
    return true;
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

bool CoverClient::generate(const QString &baseUrl, const QString &apiKey,
                           const QString &referenceImagePath,
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

    QString dataUri;
    if (!referenceImageDataUri(referenceImagePath, &dataUri, error)) {
        return false;
    }

    QJsonObject payload;
    payload.insert(QStringLiteral("model"), QString::fromLatin1(kModel));
    payload.insert(QStringLiteral("prompt"), prompt);
    payload.insert(QStringLiteral("image"), dataUri);
    payload.insert(QStringLiteral("size"),
                   QStringLiteral("%1x%2").arg(width).arg(height));
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
