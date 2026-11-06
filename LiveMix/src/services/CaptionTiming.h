#pragma once

// 文件作用：把 ASR 句段按标点拆成字幕行，并用词级时间戳确定每行的起止时间。
// 句内逗号、顿号等标点只决定断行位置；行的起止优先取 ASR words 的真实时间，
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

// 一句 ASR 文本拆出的全部字幕行；时间均为源视频上的绝对毫秒。
QVector<TimedCaption> splitUtterance(const AsrUtterance &utterance);

} // namespace CaptionTiming
