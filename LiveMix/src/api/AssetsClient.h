#pragma once

// 文件作用：素材服务（assets OpenAPI）客户端。
// 目前负责一键成片的自动 BGM 与自动音效：
// - BGM：随机取一首背景音乐；
// - 音效：按 sfx.json 的名称清单拉取音频地址与时长。
// 签名规范见 assets 后端 internal/pkg/sign：
//   METHOD\nPATH\nCANONICAL_QUERY\nTIMESTAMP\nNONCE\nBODY_SHA256_HEX
// 素材服务地址与凭证为编译期内置常量，不写入配置文件，也不提供设置入口。

#include <QString>
#include <QVector>

class HttpClient;

// 素材服务返回的一首背景音乐。
struct BgmTrack
{
    QString name;
    QString url; // 带签名的对象存储地址
    qint64 durationMs = 0;
};

// 素材服务返回的一条音效；resourceId 与 sfx.json 中的 id 对应。
struct SfxAsset
{
    QString resourceId;
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

    // 拉取音效列表（GET /openapi/assets/v1/sfx?page=1&page_size=100）。
    // 返回全部可用的音效，调用方再按 sfx.json 的名称筛选。
    bool listSfx(QVector<SfxAsset> *assets, QString *error);

private:
    HttpClient *m_http = nullptr;
};
