#pragma once

// 文件作用：智能混剪的内置音效目录。
// 资源 ID 与素材服务 sfx 列表中的 resource_id 对应；名称与使用场景
// 提供给大模型挑选，URL 由素材服务返回。

#include "../api/AssetsClient.h"

#include <QString>
#include <QVector>

namespace Mixcut {

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

// 按资源 ID 把素材服务返回的音效与内置目录对齐。
QVector<ResolvedSfx> resolveSfx(const QVector<SfxAsset> &assets);

} // namespace Mixcut
