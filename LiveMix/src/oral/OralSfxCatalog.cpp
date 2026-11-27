// 文件作用：智能口播音效目录实现。
// 资源 ID 与素材服务 sfx 列表中的 resource_id 对应。

#include "OralSfxCatalog.h"

#include <QHash>

namespace Oral {

const QVector<SfxCatalogEntry> &sfxCatalog()
{
    static const QVector<SfxCatalogEntry> entries = {
        {QStringLiteral("2123770113113985024"), QStringLiteral("读评论提示音"),
         QStringLiteral("朗读评论、切换话题时的提示")},
        {QStringLiteral("2123770111855693824"), QStringLiteral("诡异的滴水声"),
         QStringLiteral("悬疑、氛围感叙述")},
        {QStringLiteral("2123770110643539968"), QStringLiteral("讲卖点"),
         QStringLiteral("引出产品卖点、核心优势")},
        {QStringLiteral("2123770109427191808"), QStringLiteral("综艺卡通biu"),
         QStringLiteral("趣味强调、轻快转场")},
        {QStringLiteral("2123770108227620864"),
         QStringLiteral("服装分类别场景金币声"),
         QStringLiteral("分类切换、商品展示、奖励提示")},
        {QStringLiteral("2123770106927386624"),
         QStringLiteral("抓特写场景弹簧声"),
         QStringLiteral("镜头拉近、细节特写")},
        {QStringLiteral("2123770105622958080"), QStringLiteral("悬疑咚声"),
         QStringLiteral("画面感、环境与情绪氛围")},
        {QStringLiteral("2123770104381444096"), QStringLiteral("开篇提示水滴声"),
         QStringLiteral("视频开头引入主题、章节起始")},
        {QStringLiteral("2123770103022489600"), QStringLiteral("字幕音效-嘭"),
         QStringLiteral("重点强调：卖点、数字、结论")},
        {QStringLiteral("2123770101801947136"), QStringLiteral("呼转场音效"),
         QStringLiteral("转场、节奏切换")},
        {QStringLiteral("2123770100271026176"), QStringLiteral("划重点拳击声"),
         QStringLiteral("强调关键信息、划重点")},
        {QStringLiteral("2123770098878517248"),
         QStringLiteral("写字幕场景打字声"),
         QStringLiteral("字幕逐字出现、文字节奏")},
        {QStringLiteral("2123770097435676672"), QStringLiteral("一滴水滴声"),
         QStringLiteral("日常叙述、轻氛围过渡")},
    };
    return entries;
}

QVector<ResolvedSfx> resolveSfx(const QVector<SfxAsset> &assets)
{
    QHash<QString, SfxAsset> byResourceId;
    for (const SfxAsset &asset : assets) {
        byResourceId.insert(asset.resourceId, asset);
    }
    QVector<ResolvedSfx> result;
    for (const SfxCatalogEntry &entry : sfxCatalog()) {
        const auto it = byResourceId.constFind(entry.resourceId);
        if (it == byResourceId.constEnd()) {
            continue;
        }
        result.append({it->name, it->url, it->durationMs, entry.usageHint});
    }
    return result;
}

} // namespace Oral
