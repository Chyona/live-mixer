// 文件作用：内置服务地址的实现（剪映小助手）。
#include "ServiceEndpoints.h"

namespace ServiceEndpoints {

QString capCutMateBaseUrl()
{
    return QStringLiteral("https://capcut-mate.jcaigc.cn");
}

QString capCutMateApiPrefix()
{
    return QStringLiteral("/openapi/capcut-mate/v1");
}

QUrl capCutMateUrl(const QString &endpoint)
{
    QString path = capCutMateApiPrefix();
    if (!path.endsWith(QLatin1Char('/'))) {
        path += QLatin1Char('/');
    }
    QUrl url(capCutMateBaseUrl());
    url.setPath(path + endpoint);
    return url;
}

} // namespace ServiceEndpoints
