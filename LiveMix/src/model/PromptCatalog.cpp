#include "PromptCatalog.h"

#include "../data/MediaRepository.h"
#include "DefaultPrompts.h"
#include "GarmentPrompts.h"

namespace PromptCatalog {

QVector<PromptEntry> builtIns()
{
    // 财经类直播提示词从 live-mixer 后端原样导入，作为 LLM 选片的系统提示词。
    // 服装切片场景额外内置 clipflow-ai 的 3 个代表性历史版本（其中「默认提示词」
    // 为结构化合规模板），均为只读内置项。
    const QVector<DefaultPrompts::GarmentPromptVersion> garment =
        DefaultPrompts::garmentVersions();

    QVector<PromptEntry> entries;
    entries.reserve(garment.size() + 1);
    entries.append({-1, QStringLiteral("财经类直播"), QStringLiteral("适用于金融行业"),
                    QStringLiteral("金融/财经直播选片系统提示词，内置且不可编辑……"),
                    QStringLiteral("2026-08-26 14:44:41"), 0, true,
                    DefaultPrompts::content()});
    for (const DefaultPrompts::GarmentPromptVersion &version : garment) {
        entries.append({-1, version.name, version.note, version.summary, version.editedAt,
                        0, true, version.content});
    }
    return entries;
}

QVector<PromptEntry> all(const MediaRepository *repository, QString *error)
{
    QVector<PromptEntry> entries = builtIns();
    if (!repository || !repository->isOpen()) {
        return entries;
    }

    QString loadError;
    const QVector<PromptEntry> userEntries = repository->userPrompts(&loadError);
    if (!loadError.isEmpty()) {
        if (error) {
            *error = loadError;
        }
        return entries;
    }
    for (PromptEntry entry : userEntries) {
        entry.summary = summarize(entry.content);
        entries.append(entry);
    }
    return entries;
}

QString summarize(const QString &content)
{
    const QString text = content.simplified();
    constexpr int kMaxLength = 64;
    if (text.size() > kMaxLength) {
        return text.left(kMaxLength) + QStringLiteral("……");
    }
    return text;
}

} // namespace PromptCatalog
