#pragma once

// 文件作用：同步 HTTP 客户端封装。
// 直播切片任务运行在独立工作线程中，因此这里用嵌套事件循环等待网络请求，
// 让上层编排代码保持“顺序执行”的直观写法，同时不阻塞界面线程。

#include <QByteArray>
#include <QHash>
#include <QJsonObject>
#include <QNetworkAccessManager>
#include <QPair>
#include <QString>
#include <QStringList>
#include <QUrl>
#include <QVector>

#include <functional>

class HttpResponse
{
public:
    bool networkOk = false;   // 网络层是否成功（不代表业务 code 成功）
    int httpStatus = 0;       // HTTP 状态码，0 表示没有收到响应
    QByteArray body;
    QJsonObject json;
    QString error;            // 网络层错误或超时说明

    bool isHttpOk() const { return networkOk && httpStatus >= 200 && httpStatus < 300; }
};

class HttpClient
{
public:
    HttpClient();
    ~HttpClient();

    HttpClient(const HttpClient &) = delete;
    HttpClient &operator=(const HttpClient &) = delete;

    // shouldAbort 非空时会定期检查，返回 true 立即中断请求（用于取消下载）。
    HttpResponse get(const QUrl &url, int timeoutMs = 60000,
                     const std::function<bool()> &shouldAbort = {});

    HttpResponse postJson(const QUrl &url, const QJsonObject &payload,
                          const QByteArray &authorization = QByteArray(),
                          int timeoutMs = 120000);

    // POST 并逐块回调响应体，用于 SSE 流式输出；完整响应仍会累积在
    // HttpResponse::body 中。idleTimeoutMs 为空闲超时：每收到一块数据就重新
    // 计时，服务端持续输出时不会因总耗时过长而中断。
    // onChunk 返回 true 表示已收到 [DONE]，立即结束读取，不等待连接关闭。
    HttpResponse postJsonStream(
        const QUrl &url, const QJsonObject &payload, const QByteArray &authorization,
        int idleTimeoutMs, const std::function<bool(const QByteArray &)> &onChunk);

    // 以 multipart/form-data 提交文本字段；filePath 非空时同时上传单个文件。
    HttpResponse postMultipart(const QUrl &url,
                               const QVector<QPair<QString, QString>> &fields,
                               const QString &filePath, const QString &fileMimeType,
                               const QByteArray &authorization = QByteArray(),
                               int timeoutMs = 600000);

    // 将远程文件流式写入本地路径；失败时不会留下半截文件。
    // progress 回调 (已接收字节, 总字节)，总字节未知时为 -1。
    bool downloadToFile(const QUrl &url, const QString &targetPath, QString *error,
                        int timeoutMs = 600000,
                        const std::function<void(qint64, qint64)> &progress = {},
                        const std::function<bool()> &shouldAbort = {});

    // 并行探测一组下载地址的总字节数，供下载前列出文件大小与总进度。
    // 使用 Range: bytes=0-0 拿到 Content-Range 后立即中断，不下载文件内容。
    // 素材托管在腾讯 COS，签名 URL 对 HEAD 返回 403，因此不能用 HEAD 探测。
    // 超时或查询失败的文件不会出现在结果中。
    QHash<QString, qint64> probeContentLengths(const QStringList &urls,
                                               int timeoutMs = 5000);

    static QJsonObject parseJsonObject(const QByteArray &body);

private:
    HttpResponse waitForReply(class QNetworkReply *reply, int timeoutMs,
                              const std::function<bool()> &shouldAbort = {});

    QNetworkAccessManager m_manager;
};
