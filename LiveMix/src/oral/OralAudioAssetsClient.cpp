// 文件作用：智能口播音频素材客户端实现，复制自素材服务签名规范但完全独立。
// 签名规范：METHOD\nPATH\nCANONICAL_QUERY\nTIMESTAMP\nNONCE\nBODY_SHA256_HEX。

#include "OralAudioAssetsClient.h"

#include "../api/HttpClient.h"
#include "../core/ServiceEndpoints.h"

#include <QCryptographicHash>
#include <QDateTime>
#include <QJsonArray>
#include <QJsonObject>
#include <QMessageAuthenticationCode>
#include <QRandomGenerator>
#include <QStringList>
#include <QUrl>
#include <QUrlQuery>

#include <algorithm>

namespace {

// 素材服务是内置服务：地址与签名凭证固定写死，不写入配置文件。
constexpr char kAssetsBaseUrl[] = "https://assets.gogoshine.com";
constexpr char kAssetsAccessKey[] = "ak_13398500743920e8a34e115fb46898fc";
constexpr char kAssetsSecretKey[] =
    "sk_5237a982ce2b8893c06b10e36633682ae73c887149c0e997";

QString canonicalQuery(const QUrlQuery &query)
{
    QList<QPair<QString, QString>> items = query.queryItems(QUrl::FullyEncoded);
    std::sort(items.begin(), items.end(),
              [](const QPair<QString, QString> &left,
                 const QPair<QString, QString> &right) {
                  if (left.first != right.first) {
                      return left.first < right.first;
                  }
                  return left.second < right.second;
              });
    QStringList parts;
    parts.reserve(items.size());
    for (const QPair<QString, QString> &item : items) {
        parts.append(item.first + QLatin1Char('=') + item.second);
    }
    return parts.join(QLatin1Char('&'));
}

QString makeNonce()
{
    return QString::number(QRandomGenerator::global()->generate64(), 16)
        .rightJustified(16, QLatin1Char('0'));
}

QVector<QPair<QByteArray, QByteArray>> signedHeaders(const QUrl &url)
{
    const QString timestamp = QString::number(QDateTime::currentSecsSinceEpoch());
    const QString nonce = makeNonce();
    const QString bodyHash = QString::fromLatin1(
        QCryptographicHash::hash(QByteArray(), QCryptographicHash::Sha256).toHex());
    const QString canonical = QStringList{
        QStringLiteral("GET"),
        url.path(),
        canonicalQuery(QUrlQuery(url.query(QUrl::FullyEncoded))),
        timestamp,
        nonce,
        bodyHash,
    }.join(QLatin1Char('\n'));
    const QString signature = QString::fromLatin1(
        QMessageAuthenticationCode::hash(canonical.toUtf8(),
                                         QByteArray(kAssetsSecretKey),
                                         QCryptographicHash::Sha256)
            .toHex());
    return {
        {QByteArrayLiteral("X-Access-Key"), QByteArray(kAssetsAccessKey)},
        {QByteArrayLiteral("X-Timestamp"), timestamp.toUtf8()},
        {QByteArrayLiteral("X-Nonce"), nonce.toUtf8()},
        {QByteArrayLiteral("X-Signature"), signature.toUtf8()},
    };
}

QUrl serviceUrl(const QString &endpoint)
{
    QUrl url = QUrl::fromUserInput(QString::fromLatin1(kAssetsBaseUrl));
    QString path = url.path();
    if (path.isEmpty()) {
        path = QStringLiteral("/");
    }
    if (!path.endsWith(QLatin1Char('/'))) {
        path += QLatin1Char('/');
    }
    path += ServiceEndpoints::assetsApiPrefix().mid(1) + QLatin1Char('/')
        + endpoint;
    url.setPath(path);
    return url;
}

bool checkResponse(const HttpResponse &response, QJsonObject *data, QString *error)
{
    if (!response.networkOk) {
        if (error) {
            *error = response.error.isEmpty() ? QStringLiteral("网络请求失败。")
                                              : response.error;
        }
        return false;
    }
    if (response.httpStatus != 200) {
        if (error) {
            *error = QStringLiteral("素材服务返回 HTTP %1。")
                         .arg(response.httpStatus);
        }
        return false;
    }
    if (response.json.isEmpty()) {
        if (error) {
            *error = QStringLiteral("素材服务返回了无法解析的响应。");
        }
        return false;
    }
    const int code = response.json.value(QStringLiteral("code")).toInt(-1);
    if (code != 0) {
        if (error) {
            const QString message =
                response.json.value(QStringLiteral("message")).toString();
            *error = message.isEmpty()
                ? QStringLiteral("素材服务返回错误码 %1。").arg(code)
                : message;
        }
        return false;
    }
    if (data) {
        *data = response.json.value(QStringLiteral("data")).toObject();
    }
    return true;
}

} // namespace

namespace Oral {

AudioAssetsClient::AudioAssetsClient(HttpClient *http)
    : m_http(http)
{
}

bool AudioAssetsClient::randomBgm(BgmTrack *track, QString *error)
{
    if (!m_http) {
        if (error) {
            *error = QStringLiteral("HTTP 客户端不可用。");
        }
        return false;
    }
    QUrl url = serviceUrl(QStringLiteral("bgm/random"));
    QUrlQuery query;
    query.addQueryItem(QStringLiteral("limit"), QStringLiteral("1"));
    url.setQuery(query);

    const HttpResponse response =
        m_http->getWithHeaders(url, signedHeaders(url), 15000);
    QJsonObject data;
    if (!checkResponse(response, &data, error)) {
        return false;
    }
    const QJsonArray items = data.value(QStringLiteral("items")).toArray();
    if (items.isEmpty()) {
        if (error) {
            *error = QStringLiteral("素材服务没有可用的背景音乐。");
        }
        return false;
    }
    const QJsonObject item = items.first().toObject();
    const QString fileUrl =
        item.value(QStringLiteral("file_url")).toString().trimmed();
    if (fileUrl.isEmpty()) {
        if (error) {
            *error = QStringLiteral("素材服务返回的背景音乐缺少文件地址。");
        }
        return false;
    }
    if (track) {
        track->url = fileUrl;
        track->name = item.value(QStringLiteral("name")).toString().trimmed();
        if (track->name.isEmpty()) {
            track->name = QStringLiteral("背景音乐");
        }
        track->durationMs =
            qint64(item.value(QStringLiteral("duration_ms")).toDouble());
    }
    return true;
}

bool AudioAssetsClient::listSfx(QVector<SfxAsset> *assets, QString *error)
{
    if (!m_http) {
        if (error) {
            *error = QStringLiteral("HTTP 客户端不可用。");
        }
        return false;
    }
    QUrl url = serviceUrl(QStringLiteral("sfx"));
    QUrlQuery query;
    query.addQueryItem(QStringLiteral("page"), QStringLiteral("1"));
    query.addQueryItem(QStringLiteral("page_size"), QStringLiteral("100"));
    url.setQuery(query);

    const HttpResponse response =
        m_http->getWithHeaders(url, signedHeaders(url), 15000);
    QJsonObject data;
    if (!checkResponse(response, &data, error)) {
        return false;
    }
    if (assets) {
        assets->clear();
        const QJsonArray items = data.value(QStringLiteral("items")).toArray();
        assets->reserve(items.size());
        for (const QJsonValue &value : items) {
            const QJsonObject item = value.toObject();
            const QString resourceId =
                item.value(QStringLiteral("resource_id")).toString().trimmed();
            const QString name =
                item.value(QStringLiteral("name")).toString().trimmed();
            const QString fileUrl =
                item.value(QStringLiteral("file_url")).toString().trimmed();
            const qint64 durationMs =
                qint64(item.value(QStringLiteral("duration_ms")).toDouble());
            if (resourceId.isEmpty() || name.isEmpty() || fileUrl.isEmpty()
                || durationMs <= 0) {
                continue;
            }
            assets->append({resourceId, name, fileUrl, durationMs});
        }
    }
    return true;
}

} // namespace Oral
