#pragma once

// 文件作用：智能口播封面海报生成。
// 分两步：
// 1. 内置提示词 1 + 口播文案 → DeepSeek 生成提示词 2；
// 2. 提示词 2 + 口播首帧 → doubao-seedream-5-0-flash 图生图，得到封面海报。
// 两个请求都与 LLM / ASR 共用设置页的网关地址。
// 首帧必须先上传成公网 URL：网关当前会把 data URI 中的 base64 当成 URL
// 透传给上游，返回「invalid url specified」。

#include <QString>

#include <functional>

class HttpClient;

namespace Oral {

class CoverClient
{
public:
    using StatusCallback = std::function<void(const QString &)>;

    explicit CoverClient(HttpClient *http);

    // 第一步：提示词 1 + 口播内容 → 提示词 2。
    // oralContent 由调用方按「口播文案 + 画幅等补充信息」组织。
    bool generatePrompt(const QString &baseUrl, const QString &apiKey,
                        const QString &oralContent, QString *prompt,
                        QString *error, const StatusCallback &onStatus = {});

    // 第二步：提示词 2 + 首帧图 → 封面。
    // referenceImageUrl：已上传的口播首帧公网地址；prompt：提示词 2；
    // width × height：输出画布尺寸。成功时图片写入 outputPath。
    bool generate(const QString &baseUrl, const QString &apiKey,
                  const QString &referenceImageUrl, const QString &prompt,
                  int width, int height, const QString &outputPath,
                  QString *error, const StatusCallback &onStatus = {});

    static QString modelName();
    // 提示词 2 使用的文本模型，与智能口播语义分析保持一致。
    static QString promptModelName();

private:
    HttpClient *m_http = nullptr;
};

} // namespace Oral
