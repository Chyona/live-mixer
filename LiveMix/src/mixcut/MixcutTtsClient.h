#pragma once

// 文件作用：智能混剪的 AI 配音客户端。
// 走语音网关 POST /v1/audio/speech（OpenAI 兼容），默认模型 seed-tts-2.0，
// 音色为界面上选择的豆包 *_uranus_bigtts 音色。
// 长文案按句子切成不超过 600 字的片段分别合成，再用 ffmpeg 拼接成一条 MP3；
// 合成结果按「模型 + 音色 + 文案」缓存在本地，重复混剪不再重复计费。

#include <QString>

#include <functional>

class HttpClient;

namespace Mixcut {

class TtsClient
{
public:
    using StatusCallback = std::function<void(const QString &)>;

    explicit TtsClient(HttpClient *http);

    // 合成整段配音。outputPath 返回缓存中可复用的 MP3 路径，
    // durationMs 返回实际音频时长。失败时 error 为面向用户的原因。
    bool synthesize(const QString &baseUrl, const QString &apiKey,
                    const QString &text, const QString &voiceId,
                    const QString &cacheDir, const QString &workDir,
                    QString *outputPath, qint64 *durationMs, QString *error,
                    const StatusCallback &onStatus = {});

    static QString modelName();

private:
    bool synthesizeChunk(const QString &baseUrl, const QString &apiKey,
                         const QString &text, const QString &voiceId,
                         const QString &outputPath, QString *error,
                         const StatusCallback &onStatus);

    HttpClient *m_http = nullptr;
};

// 把长文案按句子切分成若干段，每段不超过 maxChars；公开给测试与日志使用。
QStringList splitTtsText(const QString &text, int maxChars = 600);

} // namespace Mixcut
