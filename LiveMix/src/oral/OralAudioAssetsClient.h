#pragma once

// 文件作用：智能口播专用的音频素材客户端（背景音乐 / 音效）。
// 为避免与直播切片的 AssetsClient 相互影响，这里单独实现签名与请求逻辑；
// 素材服务地址与凭证仍是应用内置常量，不写入用户配置。

#include <QString>
#include <QVector>

class HttpClient;

namespace Oral {

struct BgmTrack
{
    QString name;
    QString url; // 带签名的对象存储地址
    qint64 durationMs = 0;
};

struct SfxAsset
{
    QString resourceId;
    QString name;
    QString url;
    qint64 durationMs = 0;
};

class AudioAssetsClient
{
public:
    explicit AudioAssetsClient(HttpClient *http);

    // 随机取一首背景音乐（GET /openapi/assets/v1/bgm/random?limit=1）。
    bool randomBgm(BgmTrack *track, QString *error);

    // 拉取音效列表（GET /openapi/assets/v1/sfx?page=1&page_size=100）。
    bool listSfx(QVector<SfxAsset> *assets, QString *error);

private:
    HttpClient *m_http = nullptr;
};

} // namespace Oral
