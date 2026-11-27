#pragma once

// 文件作用：智能口播内置音效目录。
// 目录只保存资源 ID、展示名与语义用途；音频地址与时长由音频素材服务按
// resource_id 返回。与直播切片的 sfx 目录相互独立，便于口播单独调整。

#include "OralAudioAssetsClient.h"

#include <QString>
#include <QVector>

namespace Oral {

struct SfxCatalogEntry
{
    QString resourceId;
    QString name;
    QString usageHint;
};

struct ResolvedSfx
{
    QString name;
    QString url;
    qint64 durationMs = 0;
    QString usageHint;
};

const QVector<SfxCatalogEntry> &sfxCatalog();

// 按资源 ID 匹配素材服务返回的音效，忽略服务端缺货的条目。
QVector<ResolvedSfx> resolveSfx(const QVector<SfxAsset> &assets);

} // namespace Oral
