#pragma once

// 文件作用：网关（api.jcaigc.cn）下的豆包 ASR 客户端。
// 使用 POST /v1/audio/transcriptions，模型 volc.seedasr.auc（豆包标准版），
// 整段音频以公网 URL 提交；接口同步返回完整分句时间线与逐词时间线（words），
// 等待期间不返回中间结果。

#include "../data/ClipTypes.h"

#include <QString>

class HttpClient;

class AsrClient
{
public:
    explicit AsrClient(HttpClient *http);

    // gatewayBaseUrl 来自设置页的 API 地址（默认 https://api.jcaigc.cn/v1）。
    // 标准版只接受公网音频地址，调用前需要先把 MP3 上传成 URL。
    bool transcribe(const QString &audioUrl, const QString &gatewayBaseUrl,
                    const QString &apiKey, AsrTranscript *transcript, QString *error);

private:
    HttpClient *m_http = nullptr;
};
