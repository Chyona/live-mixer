// 文件作用：智能混剪剪映小助手客户端的特效 / 滤镜扩展实现。
// 协议与 Oral::CapCutClient 完全一致：列表字段序列化成 JSON 字符串，
// 业务错误仍以 HTTP 200 返回，真实结果看 code。

#include "MixcutCapCutClient.h"

#include "../api/HttpClient.h"
#include "../core/ServiceEndpoints.h"

#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <QUrl>

namespace {

bool checkEnvelope(const HttpResponse &response, QString *error)
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
            *error = QStringLiteral("剪映小助手返回 HTTP %1。")
                         .arg(response.httpStatus);
        }
        return false;
    }
    if (response.json.isEmpty()) {
        if (error) {
            *error = QStringLiteral("剪映小助手返回了无法解析的响应。");
        }
        return false;
    }
    const int code = response.json.value(QStringLiteral("code")).toInt(-1);
    if (code != 0) {
        if (error) {
            const QString message =
                response.json.value(QStringLiteral("message")).toString();
            *error = message.isEmpty()
                ? QStringLiteral("剪映小助手返回错误码 %1。").arg(code)
                : message;
        }
        return false;
    }
    return true;
}

QByteArray compactJson(const QJsonArray &array)
{
    return QJsonDocument(array).toJson(QJsonDocument::Compact);
}

HttpResponse postJson(const QUrl &url, const QJsonObject &payload)
{
    HttpClient http;
    return http.postJson(url, payload, QByteArray(), 10 * 60 * 1000);
}

} // namespace

namespace Mixcut {

bool CapCutClient::addEffects(const CapCutDraft &draft,
                              const QVector<CapCutEffect> &effects,
                              QString *error)
{
    if (draft.url.isEmpty() || effects.isEmpty()) {
        if (error) {
            *error = QStringLiteral("没有可写入的视频特效。");
        }
        return false;
    }
    QJsonArray array;
    for (const CapCutEffect &effect : effects) {
        if (effect.title.trimmed().isEmpty() || effect.endUs <= effect.startUs) {
            continue;
        }
        QJsonObject item;
        item.insert(QStringLiteral("effect_title"), effect.title);
        item.insert(QStringLiteral("start"), effect.startUs);
        item.insert(QStringLiteral("end"), effect.endUs);
        array.append(item);
    }
    if (array.isEmpty()) {
        return true;
    }
    QJsonObject payload;
    payload.insert(QStringLiteral("draft_url"), draft.url);
    payload.insert(QStringLiteral("effect_infos"),
                   QString::fromUtf8(compactJson(array)));
    const HttpResponse response = postJson(
        ServiceEndpoints::capCutMateUrl(QStringLiteral("add_effects")), payload);
    return checkEnvelope(response, error);
}

bool CapCutClient::addFilters(const CapCutDraft &draft,
                              const QVector<CapCutFilter> &filters,
                              QString *error)
{
    if (draft.url.isEmpty() || filters.isEmpty()) {
        if (error) {
            *error = QStringLiteral("没有可写入的滤镜。");
        }
        return false;
    }
    QJsonArray array;
    for (const CapCutFilter &filter : filters) {
        if (filter.title.trimmed().isEmpty() || filter.endUs <= filter.startUs) {
            continue;
        }
        QJsonObject item;
        item.insert(QStringLiteral("filter_title"), filter.title);
        item.insert(QStringLiteral("start"), filter.startUs);
        item.insert(QStringLiteral("end"), filter.endUs);
        item.insert(QStringLiteral("intensity"),
                    qBound(0.0, filter.intensity, 100.0));
        array.append(item);
    }
    if (array.isEmpty()) {
        return true;
    }
    QJsonObject payload;
    payload.insert(QStringLiteral("draft_url"), draft.url);
    payload.insert(QStringLiteral("filter_infos"),
                   QString::fromUtf8(compactJson(array)));
    const HttpResponse response = postJson(
        ServiceEndpoints::capCutMateUrl(QStringLiteral("add_filters")), payload);
    return checkEnvelope(response, error);
}

} // namespace Mixcut
