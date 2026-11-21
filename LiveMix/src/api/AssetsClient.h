#pragma once

// 文件作用：素材服务（assets OpenAPI）客户端。
// 目前只负责一键成片的自动 BGM：按 HMAC-SHA256 签名协议随机取一首背景音乐。
// 签名规范见 assets 后端 internal/pkg/sign：
//   METHOD\nPATH\nCANONICAL_QUERY\nTIMESTAMP\nNONCE\nBODY_SHA256_HEX
// 素材服务地址与凭证为编译期内置常量，不写入配置文件，也不提供设置入口。

#include <QString>

class HttpClient;

// 素材服务返回的一首背景音乐。
struct BgmTrack
{
    QString name;
    QString url; // 带签名的对象存储地址
    qint64 durationMs = 0;
};

class AssetsClient
{
public:
    explicit AssetsClient(HttpClient *http);

    // 随机取一首 BGM（GET /openapi/assets/v1/bgm/random?limit=1）。
    // 地址与签名凭证为内置常量；失败时写入 error。
    bool randomBgm(BgmTrack *track, QString *error);

private:
    HttpClient *m_http = nullptr;
};
