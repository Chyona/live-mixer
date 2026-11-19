// 文件作用：同步 HTTP 客户端实现（GET / JSON POST / 文件上传 / 文件下载）。
#include "HttpClient.h"

#include <QDir>
#include <QEventLoop>
#include <QFile>
#include <QFileInfo>
#include <QHttpMultiPart>
#include <QJsonDocument>
#include <QNetworkReply>
#include <QNetworkRequest>
#include <QSaveFile>
#include <QSharedPointer>
#include <QTimer>
#include <QUrlQuery>

namespace {

void applyCommonHeaders(QNetworkRequest *request)
{
    request->setRawHeader("Accept", "application/json");
    request->setHeader(QNetworkRequest::UserAgentHeader,
                       QStringLiteral("LiveMixStudio/1.0"));
    // 对象存储下载地址可能经过重定向，允许同安全级别的跳转。
    request->setAttribute(QNetworkRequest::RedirectPolicyAttribute,
                          QNetworkRequest::NoLessSafeRedirectPolicy);
}

QString uploadFileName(const QString &filePath)
{
    // 上传文件名使用纯 ASCII，避免 multipart 头里的中文编码问题。
    const QString suffix = QFileInfo(filePath).suffix().toLower();
    return suffix.isEmpty() ? QStringLiteral("upload.bin")
                            : QStringLiteral("upload.") + suffix;
}

} // namespace

HttpClient::HttpClient() = default;
HttpClient::~HttpClient() = default;

QJsonObject HttpClient::parseJsonObject(const QByteArray &body)
{
    QJsonParseError error;
    const QJsonDocument document = QJsonDocument::fromJson(body, &error);
    if (error.error != QJsonParseError::NoError || !document.isObject()) {
        return QJsonObject();
    }
    return document.object();
}

HttpResponse HttpClient::waitForReply(QNetworkReply *reply, int timeoutMs,
                                      const std::function<bool()> &shouldAbort)
{
    HttpResponse response;
    QEventLoop loop;
    QTimer timer;
    QTimer abortTimer;
    bool timedOut = false;

    timer.setSingleShot(true);
    QObject::connect(&timer, &QTimer::timeout, reply, [&] {
        timedOut = true;
        reply->abort();
    });
    if (shouldAbort) {
        abortTimer.setInterval(120);
        QObject::connect(&abortTimer, &QTimer::timeout, reply, [&] {
            if (shouldAbort()) {
                reply->abort();
            }
        });
    }
    QObject::connect(reply, &QNetworkReply::finished, &loop, &QEventLoop::quit);

    timer.start(timeoutMs);
    if (shouldAbort) {
        abortTimer.start();
    }
    if (!reply->isFinished()) {
        loop.exec();
    }
    timer.stop();
    abortTimer.stop();

    response.httpStatus =
        reply->attribute(QNetworkRequest::HttpStatusCodeAttribute).toInt();
    response.networkOk = reply->error() == QNetworkReply::NoError;
    response.body = reply->readAll();
    response.json = parseJsonObject(response.body);
    if (!response.networkOk) {
        response.error = timedOut ? QStringLiteral("请求超时（%1 秒）").arg(timeoutMs / 1000)
                                  : reply->errorString();
    }

    reply->deleteLater();
    return response;
}

HttpResponse HttpClient::get(const QUrl &url, int timeoutMs,
                             const std::function<bool()> &shouldAbort)
{
    QNetworkRequest request(url);
    applyCommonHeaders(&request);
    return waitForReply(m_manager.get(request), timeoutMs, shouldAbort);
}

HttpResponse HttpClient::postJson(const QUrl &url, const QJsonObject &payload,
                                  const QByteArray &authorization, int timeoutMs)
{
    QNetworkRequest request(url);
    request.setHeader(QNetworkRequest::ContentTypeHeader,
                      QStringLiteral("application/json"));
    if (!authorization.isEmpty()) {
        request.setRawHeader("Authorization", authorization);
    }
    applyCommonHeaders(&request);

    const QByteArray body = QJsonDocument(payload).toJson(QJsonDocument::Compact);
    return waitForReply(m_manager.post(request, body), timeoutMs);
}

HttpResponse HttpClient::postJsonStream(
    const QUrl &url, const QJsonObject &payload, const QByteArray &authorization,
    int idleTimeoutMs, const std::function<bool(const QByteArray &)> &onChunk)
{
    HttpResponse response;

    QNetworkRequest request(url);
    request.setHeader(QNetworkRequest::ContentTypeHeader,
                      QStringLiteral("application/json"));
    if (!authorization.isEmpty()) {
        request.setRawHeader("Authorization", authorization);
    }
    applyCommonHeaders(&request);
    request.setRawHeader("Accept", "text/event-stream");

    const QByteArray body = QJsonDocument(payload).toJson(QJsonDocument::Compact);
    QNetworkReply *reply = m_manager.post(request, body);

    QEventLoop loop;
    QTimer timer;
    bool timedOut = false;
    bool stoppedAtDone = false;

    const auto consume = [&] {
        const QByteArray chunk = reply->readAll();
        if (chunk.isEmpty()) {
            return;
        }
        // 每收到一块数据就重新计时：只要服务端仍在持续输出就不算超时。
        if (loop.isRunning()) {
            timer.start(idleTimeoutMs);
        }
        response.body += chunk;
        if (onChunk && onChunk(chunk)) {
            stoppedAtDone = true;
            loop.quit();
        }
    };

    QObject::connect(reply, &QNetworkReply::readyRead, &loop, consume);
    QObject::connect(reply, &QNetworkReply::finished, &loop, &QEventLoop::quit);
    QObject::connect(&timer, &QTimer::timeout, reply, [&] {
        timedOut = true;
        reply->abort();
    });

    timer.setSingleShot(true);
    timer.start(idleTimeoutMs);
    if (!reply->isFinished()) {
        loop.exec();
    }
    timer.stop();
    if (!stoppedAtDone) {
        consume();
    }

    response.httpStatus =
        reply->attribute(QNetworkRequest::HttpStatusCodeAttribute).toInt();
    response.networkOk = reply->error() == QNetworkReply::NoError;
    response.json = parseJsonObject(response.body);
    if (!response.networkOk) {
        response.error = timedOut
                             ? QStringLiteral("请求超时（%1 秒未收到数据）")
                                   .arg(idleTimeoutMs / 1000)
                             : reply->errorString();
    }

    reply->deleteLater();
    return response;
}

HttpResponse HttpClient::postMultipart(
    const QUrl &url, const QVector<QPair<QString, QString>> &fields,
    const QString &filePath, const QString &fileMimeType,
    const QByteArray &authorization, int timeoutMs)
{
    HttpResponse response;

    auto *multiPart = new QHttpMultiPart(QHttpMultiPart::FormDataType);

    for (const QPair<QString, QString> &field : fields) {
        QHttpPart part;
        part.setHeader(QNetworkRequest::ContentDispositionHeader,
                       QStringLiteral("form-data; name=\"%1\"").arg(field.first));
        part.setBody(field.second.toUtf8());
        multiPart->append(part);
    }

    // filePath 为空表示只提交文本字段（例如网关 ASR 用 file_url 传音频）。
    if (!filePath.isEmpty()) {
        auto *file = new QFile(filePath);
        if (!file->open(QIODevice::ReadOnly)) {
            response.error =
                QStringLiteral("无法读取待上传文件：%1").arg(file->errorString());
            delete file;
            delete multiPart;
            return response;
        }

        QHttpPart filePart;
        filePart.setHeader(
            QNetworkRequest::ContentDispositionHeader,
            QStringLiteral("form-data; name=\"file\"; filename=\"%1\"")
                .arg(uploadFileName(filePath)));
        filePart.setHeader(QNetworkRequest::ContentTypeHeader, fileMimeType);
        filePart.setBodyDevice(file);
        file->setParent(multiPart); // 由 multipart 统一释放
        multiPart->append(filePart);
    }

    QNetworkRequest request(url);
    applyCommonHeaders(&request);
    if (!authorization.isEmpty()) {
        request.setRawHeader("Authorization", authorization);
    }
    QNetworkReply *reply = m_manager.post(request, multiPart);
    multiPart->setParent(reply); // 请求结束后释放文件与表单

    response = waitForReply(reply, timeoutMs);
    return response;
}

namespace {

// 从 Content-Range（bytes 0-0/12345）或整个响应的 Content-Length 读取总字节数。
qint64 totalBytesFromReply(QNetworkReply *reply)
{
    const QByteArray range = reply->rawHeader("Content-Range");
    const int slash = range.lastIndexOf('/');
    if (slash >= 0) {
        return range.mid(slash + 1).toLongLong();
    }
    if (reply->attribute(QNetworkRequest::HttpStatusCodeAttribute).toInt() == 200) {
        return reply->header(QNetworkRequest::ContentLengthHeader).toLongLong();
    }
    return -1;
}

} // namespace

QHash<QString, qint64> HttpClient::probeContentLengths(const QStringList &urls,
                                                       int timeoutMs)
{
    QHash<QString, qint64> lengths;
    if (urls.isEmpty()) {
        return lengths;
    }

    QEventLoop loop;
    QTimer timer;
    QList<QNetworkReply *> replies;
    int pending = urls.size();
    bool timedOut = false;
    replies.reserve(urls.size());

    for (const QString &urlString : urls) {
        QNetworkRequest request{QUrl(urlString)};
        applyCommonHeaders(&request);
        // 只取第一个字节；拿到响应头后立即中断，避免探测变成完整下载。
        request.setRawHeader("Range", "bytes=0-0");
        QNetworkReply *reply = m_manager.get(request);
        replies.append(reply);
        auto captured = QSharedPointer<bool>::create(false);

        QObject::connect(
            reply, &QNetworkReply::metaDataChanged, &loop,
            [&lengths, reply, urlString, captured] {
                if (*captured) {
                    return;
                }
                const qint64 total = totalBytesFromReply(reply);
                if (total > 0) {
                    lengths.insert(urlString, total);
                }
                *captured = true;
                reply->abort();
            });
        QObject::connect(
            reply, &QNetworkReply::finished, &loop,
            [&lengths, &pending, &loop, &timedOut, reply, urlString, captured] {
                if (!*captured && !timedOut) {
                    const qint64 total = totalBytesFromReply(reply);
                    if (total > 0) {
                        lengths.insert(urlString, total);
                    }
                }
                if (--pending <= 0) {
                    loop.quit();
                }
            });
    }

    timer.setSingleShot(true);
    QObject::connect(&timer, &QTimer::timeout, &loop, [&] {
        timedOut = true;
        loop.quit();
    });
    timer.start(timeoutMs);
    loop.exec();
    timer.stop();

    for (QNetworkReply *reply : replies) {
        if (reply->isRunning()) {
            reply->abort();
        }
        reply->deleteLater();
    }
    return lengths;
}

bool HttpClient::downloadToFile(const QUrl &url, const QString &targetPath,
                                QString *error, int timeoutMs,
                                const std::function<void(qint64, qint64)> &progress,
                                const std::function<bool()> &shouldAbort)
{
    const QFileInfo targetInfo(targetPath);
    if (!QDir().mkpath(targetInfo.absolutePath())) {
        if (error) {
            *error = QStringLiteral("无法创建目录：%1").arg(targetInfo.absolutePath());
        }
        return false;
    }

    QSaveFile file(targetPath);
    if (!file.open(QIODevice::WriteOnly)) {
        if (error) {
            *error = QStringLiteral("无法写入文件：%1").arg(file.errorString());
        }
        return false;
    }

    QNetworkRequest request(url);
    applyCommonHeaders(&request);

    QNetworkReply *reply = m_manager.get(request);
    QEventLoop loop;
    QTimer timer;
    QTimer abortTimer;
    bool timedOut = false;
    bool aborted = false;

    QObject::connect(reply, &QNetworkReply::readyRead, &loop,
                     [&] { file.write(reply->readAll()); });
    QObject::connect(reply, &QNetworkReply::finished, &loop, &QEventLoop::quit);
    if (progress) {
        QObject::connect(reply, &QNetworkReply::downloadProgress, &loop,
                         [&progress](qint64 received, qint64 total) {
                             progress(received, total);
                         });
    }
    QObject::connect(&timer, &QTimer::timeout, &loop, [&] {
        timedOut = true;
        reply->abort();
    });
    if (shouldAbort) {
        abortTimer.setInterval(120);
        QObject::connect(&abortTimer, &QTimer::timeout, &loop, [&] {
            if (shouldAbort()) {
                aborted = true;
                reply->abort();
            }
        });
    }

    timer.setSingleShot(true);
    timer.start(timeoutMs);
    if (shouldAbort) {
        abortTimer.start();
    }
    if (!reply->isFinished()) {
        loop.exec();
    }
    timer.stop();
    abortTimer.stop();

    file.write(reply->readAll());

    const int status = reply->attribute(QNetworkRequest::HttpStatusCodeAttribute).toInt();
    const bool ok = !timedOut && !aborted && reply->error() == QNetworkReply::NoError
        && status >= 200 && status < 300;
    // 错误信息要能直接展示在界面上，不能带出带签名的完整 URL。
    QString replyError;
    if (aborted) {
        replyError = QStringLiteral("下载已取消");
    } else if (timedOut) {
        replyError = QStringLiteral("下载超时");
    } else if (status == 403) {
        replyError = QStringLiteral("服务器拒绝访问（HTTP 403，链接可能已失效）");
    } else if (status == 404) {
        replyError = QStringLiteral("文件不存在（HTTP 404）");
    } else if (status >= 400) {
        replyError = QStringLiteral("服务器返回 HTTP %1").arg(status);
    } else {
        QString text = reply->errorString();
        // errorString 形如 "Error transferring <url> - <reason>"，去掉 URL。
        const int dash = text.lastIndexOf(QStringLiteral(" - "));
        if (dash >= 0) {
            text = text.mid(dash + 3);
        }
        replyError = text;
    }
    reply->deleteLater();

    if (!ok) {
        file.cancelWriting();
        if (error) {
            *error = QStringLiteral("下载失败：%1").arg(replyError);
        }
        return false;
    }
    if (!file.commit()) {
        if (error) {
            *error = QStringLiteral("保存下载文件失败：%1").arg(file.errorString());
        }
        return false;
    }
    return true;
}
