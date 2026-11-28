#pragma once

// 文件作用：智能混剪的剪映小助手（capcut-mate）客户端。
//
// 上传、草稿、视频/图片/字幕/音频/关键帧、云渲染与草稿下载复用智能口播
// 已经验证过的 Oral::CapCutClient（同一套 capcut-mate 协议），这里只补充
// 智能混剪需要的「视频特效」与「滤镜」两条轨道。
// 所有时间参数仍然使用微秒，调用方负责毫秒换算。

#include "../oral/OralCapCutClient.h"

#include <QString>
#include <QVector>

namespace Mixcut {

using CapCutDraft = Oral::CapCutDraft;
using CapCutVideo = Oral::CapCutVideo;
using CapCutImage = Oral::CapCutImage;
using CapCutCaption = Oral::CapCutCaption;
using CapCutCaptionStyle = Oral::CapCutCaptionStyle;
using CapCutAudio = Oral::CapCutAudio;
using CapCutKeyframe = Oral::CapCutKeyframe;
using CapCutVideoStatus = Oral::CapCutVideoStatus;

// 视频特效：title 须为剪映支持的特效名称。
struct CapCutEffect
{
    QString title;
    qint64 startUs = 0;
    qint64 endUs = 0;
};

// 滤镜：intensity 为 0–100。
struct CapCutFilter
{
    QString title;
    qint64 startUs = 0;
    qint64 endUs = 0;
    double intensity = 40.0;
};

class CapCutClient : public Oral::CapCutClient
{
public:
    using Oral::CapCutClient::CapCutClient;

    // add_effects：写入一条视频特效轨。
    bool addEffects(const CapCutDraft &draft,
                    const QVector<CapCutEffect> &effects, QString *error);

    // add_filters：写入一条滤镜轨。
    bool addFilters(const CapCutDraft &draft,
                    const QVector<CapCutFilter> &filters, QString *error);
};

} // namespace Mixcut
