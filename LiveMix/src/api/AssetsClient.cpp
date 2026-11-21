// 文件作用：素材服务 OpenAPI 调用，HMAC-SHA256 签名与资源解析。
#include "AssetsClient.h"

#include "../core/ServiceEndpoints.h"
#include "HttpClient.h"

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

// 素材服务为内置服务：地址与 HMAC 凭证按要求写死在客户端，
// 不写入配置文件，也不提供设置入口。
constexpr char kAssetsBaseUrl[] = "https://assets.gogoshine.com";
constexpr char kAssetsAccessKey[] = "ak_13398500743920e8a34e115fb46898fc";
constexpr char kAssetsSecretKey[] =
    "sk_5237a982ce2b8893c06b10e36633682ae73c887149c0e997";

// 与 Go 后端 canonicalizeQuery 一致：按 key、value 排序后用 "&" 连接。
// page=1&page_size=100 这类纯 ASCII 查询在两种实现下编码结果相同。
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

// nonce 长度需在 8~64 之间；16 位十六进制随机串足够避免 5 分钟内的重复。
QString makeNonce()
{
    return QString::number(QRandomGenerator::global()->generate64(), 16)
        .rightJustified(16, QLatin1Char('0'));
}

QVector<QPair<QByteArray, QByteArray>> signedHeaders(const QString &accessKey,
                                                     const QString &secretKey,
                                                     const QUrl &url)
{
    const QString timestamp =
        QString::number(QDateTime::currentSecsSinceEpoch());
    const QString nonce = makeNonce();
    const QString bodyHash = QString::fromLatin1(
        QCryptographicHash::hash(QByteArray(), QCryptographicHash::Sha256).toHex());

    const QUrlQuery query(url.query(QUrl::FullyEncoded));
    // GET 无请求体，body 哈希固定为空串的 SHA-256。
    const QString canonical = QStringList{
        QStringLiteral("GET"),
        url.path(),
        canonicalQuery(query),
        timestamp,
        nonce,
        bodyHash,
    }.join(QLatin1Char('\n'));

    const QString signature = QString::fromLatin1(
        QMessageAuthenticationCode::hash(canonical.toUtf8(), secretKey.toUtf8(),
                                         QCryptographicHash::Sha256)
            .toHex());

    return {
        {QByteArrayLiteral("X-Access-Key"), accessKey.toUtf8()},
        {QByteArrayLiteral("X-Timestamp"), timestamp.toUtf8()},
        {QByteArrayLiteral("X-Nonce"), nonce.toUtf8()},
        {QByteArrayLiteral("X-Signature"), signature.toUtf8()},
    };
}

// 拼出素材服务的完整地址；endpoint 形如 "bgm/random"、"sfx"。
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

// 校验素材服务统一响应：网络 / HTTP / 业务 code，成功后取出 data 对象。
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
            if (response.httpStatus == 401) {
                *error = QStringLiteral("素材服务鉴权失败（HTTP 401），请检查凭证。");
            } else if (response.httpStatus == 403) {
                *error = QStringLiteral(
                    "素材服务拒绝访问（HTTP 403），请确认凭证已开通对应资源权限。");
            } else {
                *error = QStringLiteral("素材服务返回 HTTP %1。")
                             .arg(response.httpStatus);
            }
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

AssetsClient::AssetsClient(HttpClient *http)
    : m_http(http)
{
}

bool AssetsClient::randomBgm(BgmTrack *track, QString *error)
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

    const HttpResponse response = m_http->getWithHeaders(
        url, signedHeaders(QString::fromLatin1(kAssetsAccessKey),
                           QString::fromLatin1(kAssetsSecretKey), url),
        15000);
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

bool AssetsClient::listSfx(QVector<SfxAsset> *assets, QString *error)
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

    const HttpResponse response = m_http->getWithHeaders(
        url, signedHeaders(QString::fromLatin1(kAssetsAccessKey),
                           QString::fromLatin1(kAssetsSecretKey), url),
        15000);
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
            // resource_id 在接口里是字符串，避免 64 位整数经 JSON double 丢精度。
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
