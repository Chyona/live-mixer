#pragma once

// 文件作用：智能口播专用的语音识别客户端。
// 与直播切片的 AsrClient 完全独立：
// - 首选模型固定为豆包极速版 volc.bigasr.auc_turbo，直接上传本地 MP3；
// - 如果当前 API Key 未开通该资源，调用方可通过 transcribeUrl /
//   transcribeWithModel 回退到 volc.seedasr.auc 或 qwen3-asr-flash；
// - 解析分句、逐词时间戳与可选的说话人信息，输出 Oral::SpeechAnalysis。
// 这样直播切片后续调整 ASR 模型或链路时，不会影响智能口播。

#include "OralTypes.h"

#include <QString>

class HttpClient;

namespace Oral {

class AsrClient
{
public:
    explicit AsrClient(HttpClient *http);

    // 首选链路：本地文件 + volc.bigasr.auc_turbo。
    // gatewayBaseUrl 来自设置页的 API 地址（默认 https://api.jcaigc.cn/v1）。
    bool transcribe(const QString &audioPath, const QString &gatewayBaseUrl,
                    const QString &apiKey, SpeechAnalysis *analysis,
                    QString *error);

    // 指定模型的本地上传链路；用于极速版未开通时回退到 qwen3-asr-flash。
    bool transcribeWithModel(const QString &audioPath, const QString &model,
                             const QString &gatewayBaseUrl,
                             const QString &apiKey, SpeechAnalysis *analysis,
                             QString *error);

    // 公网地址链路；volc.seedasr.auc 只接受公网音频地址。
    bool transcribeUrl(const QString &audioUrl, const QString &model,
                       const QString &gatewayBaseUrl, const QString &apiKey,
                       SpeechAnalysis *analysis, QString *error);

    static QString modelName();
    static QString fallbackUrlModelName();
    static QString fallbackFileModelName();

private:
    HttpClient *m_http = nullptr;
};

// 是否为「模型/资源未开通」类错误；流水线据此决定是否回退。
bool asrErrorNeedsFallback(const QString &error);

} // namespace Oral
