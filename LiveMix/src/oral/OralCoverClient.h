#pragma once

// 文件作用：智能口播封面海报生成。
// 取口播视频首帧作为参考图，调用生图网关的 doubao-seedream-5-0-flash
// 完成图生图，得到一张与视频内容匹配的海报（画布尺寸、本地 JPG）。
// 网关与 LLM / ASR 共用设置页地址：POST <apiUrl>/images/generations。

#include <QString>

#include <functional>

class HttpClient;

namespace Oral {

class CoverClient
{
public:
    using StatusCallback = std::function<void(const QString &)>;

    explicit CoverClient(HttpClient *http);

    // referenceImagePath：口播首帧（本地图片）；prompt：海报提示词；
    // width × height：输出画布尺寸。成功时图片写入 outputPath。
    bool generate(const QString &baseUrl, const QString &apiKey,
                  const QString &referenceImagePath, const QString &prompt,
                  int width, int height, const QString &outputPath,
                  QString *error, const StatusCallback &onStatus = {});

    static QString modelName();

private:
    HttpClient *m_http = nullptr;
};

} // namespace Oral
