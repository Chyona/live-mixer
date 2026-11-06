#pragma once

// Demo content for the prompt management page. Business data (sources, drafts,
// tasks) is read from SQLite via MediaRepository.

#include "DefaultPrompts.h"
#include "GarmentPrompts.h"

#include <QString>
#include <QStringList>
#include <QVector>

namespace Demo {

struct PromptEntry {
    QString name;
    QString note;
    QString summary;
    // 编辑时间统一为 yyyy-MM-dd HH:mm:ss。
    QString editedAt;
    int uses = 0;
    bool builtIn = false;
    QString content;
};

inline QVector<PromptEntry> prompts()
{
    // 默认提示词从 live-mixer 后端原样导入，作为 LLM 选片的系统提示词。
    // 服装切片场景额外内置 clipflow-ai 的 3 个代表性历史版本，均为只读内置项。
    const QString defaultContent = DefaultPrompts::content();
    const QString demoContent = QStringLiteral(
        "你是一名直播切片专家。请从直播转写稿中筛选高价值片段，输出每条的起止时间与一句话摘要。\n"
        "\n"
        "【优先保留】\n"
        "1. 包含具体产品卖点、成分、使用效果的讲解段落；\n"
        "2. 价格机制、优惠券、限时福利等促单话术；\n"
        "3. 现场演示（质地、上脸、对比）前后的完整说明。\n"
        "\n"
        "【必须剔除】\n"
        "- 口误、重复寒暄、长时间沉默或无关闲聊；\n"
        "- 单条时长不在 30–90 秒区间的片段。");

    const QVector<DefaultPrompts::GarmentPromptVersion> garment =
        DefaultPrompts::garmentVersions();

    QVector<PromptEntry> entries;
    entries.reserve(garment.size() + 3);
    entries.append({QStringLiteral("默认提示词"), QStringLiteral("适用于金融行业"),
                    QStringLiteral("金融/财经直播选片系统提示词，内置且不可编辑……"),
                    QStringLiteral("2026-08-26 14:44:41"), 0, true, defaultContent});
    for (const DefaultPrompts::GarmentPromptVersion &version : garment) {
        entries.append({version.name, version.note, version.summary, version.editedAt,
                        0, true, version.content});
    }

    entries.append({QStringLiteral("美妆带货·高转化"),
                    QStringLiteral("用于美妆/护肤品类直播切片"),
                    QStringLiteral("优先选「质地展示 + 价格锚点 + 限时机制」三连片段，"
                                   "剔除口误和重复话术……"),
                    QStringLiteral("2026-09-26 14:02:00"), 5, false, demoContent});
    entries.append({QStringLiteral("游戏高光"), QString(),
                    QStringLiteral("识别团战击杀、翻盘、高光操作时刻，情绪上扬的解说段落优先……"),
                    QStringLiteral("2026-09-20 09:44:00"), 2, false,
                    QStringLiteral("你是一名游戏直播切片导演。请识别团战击杀、翻盘、高光操作时刻，"
                                   "优先保留了情绪上扬的解说段落，剔除无聊发育与重复画面。"
                                   "单条控制在 20–60 秒，输出时间码与一句话摘要。")});
    return entries;
}

} // namespace Demo
