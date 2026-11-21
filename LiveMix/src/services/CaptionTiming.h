#pragma once

// 文件作用：把 ASR 句段按标点拆成字幕条，并用词级时间戳确定每条的起止时间；
// 超过字数上限的句段会均衡切成多条独立字幕（不拆开英文单词与数字）。
// 句内逗号、顿号等标点只决定断句位置；字幕的起止优先取 ASR words 的真实时间，
// 没有词级时间线时才退化为按文字长度分摊。

#include "../data/ClipTypes.h"

#include <QString>
#include <QVector>

struct TimedCaption
{
    qint64 startMs = 0;
    qint64 endMs = 0;
    QString text;
};

namespace CaptionTiming {

// 一句 ASR 文本拆出的全部字幕；时间均为源视频上的绝对毫秒。
// 每条显示文本不超过 maxCharsPerCaption 个字符（不可拆分的英文单词/数字除外），
// 超长句段会被均衡切成的多条独立字幕，而不是用换行写进同一条字幕。
QVector<TimedCaption> splitUtterance(const AsrUtterance &utterance,
                                     int maxCharsPerCaption = 15);

} // namespace CaptionTiming
