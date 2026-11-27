#include "StudioData.h"

#include <QDateTime>
#include <QFileInfo>
#include <QHash>
#include <QPair>
#include <QRegularExpression>

#include <algorithm>
#include <cmath>

namespace StudioData {
namespace {

QString strippedName(const QString &fileName)
{
    const QFileInfo info(fileName);
    return info.completeBaseName().isEmpty() ? fileName : info.completeBaseName();
}

QString uploadSize(qint64 bytes)
{
    if (bytes <= 0) {
        return QStringLiteral("186 MB");
    }
    if (bytes < 1024 * 1024) {
        return QStringLiteral("%1 KB").arg(qMax<qint64>(1, qRound64(bytes / 1024.0)));
    }
    return QStringLiteral("%1 MB").arg(qRound64(bytes / (1024.0 * 1024.0)));
}

QString formatGenerationClock(const QDateTime &date)
{
    return date.toString(QStringLiteral("HH:mm:ss"));
}

QStringList splitPromptSentences(const QString &prompt)
{
    QStringList result;
    QString current;

    for (const QChar ch : prompt) {
        if (ch == QLatin1Char('\r')) {
            continue;
        }
        if (ch == QLatin1Char('\n')) {
            const QString trimmed = current.trimmed();
            if (!trimmed.isEmpty()) {
                result.append(trimmed);
            }
            current.clear();
            continue;
        }

        current.append(ch);
        if (QStringLiteral("。！？!?").contains(ch)) {
            const QString trimmed = current.trimmed();
            if (!trimmed.isEmpty()) {
                result.append(trimmed);
            }
            current.clear();
        }
    }

    const QString trimmed = current.trimmed();
    if (!trimmed.isEmpty()) {
        result.append(trimmed);
    }

    return result;
}

const QStringList kOralKeywords = {
    QStringLiteral("油皮"), QStringLiteral("通勤"), QStringLiteral("成膜"),
    QStringLiteral("不搓泥"), QStringLiteral("SPF50+"),
};

struct OralCaptionSeed {
    qint64 atMs;
    QString text;
};

const QVector<OralCaptionSeed> &oralCaptionSeeds()
{
    static const QVector<OralCaptionSeed> seeds = {
        {2600, QStringLiteral("夏天通勤最怕的不是晒黑，是闷。")},
        {7800, QStringLiteral("油皮选防晒，先看质地，再看成膜。")},
        {12800, QStringLiteral("推开三秒成膜，不搓泥也不泛白。")},
        {17300, QStringLiteral("SPF50+，早晚通勤完全够用。")},
        {21000, QStringLiteral("怕闷痘的姐妹，可以从这支试起。")},
    };
    return seeds;
}

struct OralLogSeed {
    qint64 atMs;
    QString text;
    Tone tone;
};

struct OralStageSeed {
    OralStage key;
    QString label;
    qint64 durationMs;
    QString doneDetail;
    QVector<OralLogSeed> logs;
};

const QVector<OralStageSeed> &oralStageSeeds()
{
    static const QVector<OralStageSeed> seeds = {
        {OralStage::Analyze, QStringLiteral("理解口播"), 6000,
         QStringLiteral("486 字 · 6 个语义段"),
         {{300, QStringLiteral("已提取口播音轨，02:48 · 48kHz"), Tone::Info},
          {1100, QStringLiteral("ASR 识别完成，共 486 字，置信度 98.2%"), Tone::Ok},
          {2100,
           QStringLiteral("划分 6 个语义段：开场钩子 / 痛点 / 卖点 / 质地 / 建议 / 结尾"),
           Tone::Ai},
          {3300, QStringLiteral("提取关键词：通勤、油皮、成膜、不搓泥、SPF50+"),
           Tone::Ai},
          {4600, QStringLiteral("检测到 3 处重点句，将加大字幕强调"), Tone::Ai}}},
        {OralStage::Match, QStringLiteral("匹配素材"), 5000,
         QStringLiteral("8 个素材 · 12 处插入"),
         {{400, QStringLiteral("从素材库匹配到 8 个高相关素材"), Tone::Ok},
          {1200, QStringLiteral("产品特写 → 痛点段落的第 2 句"), Tone::Ai},
          {2200, QStringLiteral("上脸效果 → 质地演示段落"), Tone::Ai},
          {3200, QStringLiteral("通勤场景 → 开场钩子句"), Tone::Ai},
          {4200, QStringLiteral("插入密度：适中 · 预计 12 处 B-Roll"), Tone::Ok}}},
        {OralStage::Compose, QStringLiteral("智能包装"), 7000,
         QStringLiteral("24 条字幕 · 4 个转场"),
         {{300, QStringLiteral("生成 24 条字幕，关键词自动高亮"), Tone::Ai},
          {1500, QStringLiteral("应用模板：清透讲解 · 字号与安全区自适应"),
           Tone::Ai},
          {2700, QStringLiteral("添加 4 个转场：推近 / 擦除 / 叠化 / 闪白"),
           Tone::Info},
          {3900, QStringLiteral("匹配 BGM：轻快通勤 · 人声段自动闪避"),
           Tone::Info},
          {5100, QStringLiteral("生成片头标题「油皮防晒怎么选」"), Tone::Ai},
          {6100, QStringLiteral("字幕、素材与音频轨道对齐完成"), Tone::Ok}}},
        {OralStage::Render, QStringLiteral("渲染成片"), 6000,
         QStringLiteral("1080×1920 · 30fps"),
         {{400, QStringLiteral("开始云渲染 1080×1920 · 30fps"), Tone::Info},
          {1900, QStringLiteral("已渲染 480/1440 帧，画面编码中"), Tone::Info},
          {3500, QStringLiteral("已渲染 1120/1440 帧，混音完成"), Tone::Info},
          {5100, QStringLiteral("成片已生成，自动下载到本地"), Tone::Ok}}},
    };
    return seeds;
}

qint64 oralStageStart(OralStage key)
{
    qint64 start = 0;
    for (const OralStageSeed &seed : oralStageSeeds()) {
        if (seed.key == key) {
            return start;
        }
        start += seed.durationMs;
    }
    return start;
}

double stageRatio(qint64 value, qint64 start, qint64 end)
{
    if (value >= end) {
        return 1.0;
    }
    if (value <= start) {
        return 0.0;
    }
    return static_cast<double>(value - start) / static_cast<double>(qMax<qint64>(1, end - start));
}

struct MixcutLogSeed {
    qint64 atMs;
    QString text;
    Tone tone;
};

struct MixcutStageSeed {
    MixcutStage key;
    QString label;
    qint64 durationMs;
    QString doneDetail;
    QVector<MixcutLogSeed> logs;
};

struct MixcutContext {
    MixcutScriptStats stats;
    MixcutVoice voice;
    MixcutSource source;
    QString ratio;
    QStringList keywords;
    QStringList captions;
    int subtitleCount = 0;
    int sfxCount = 0;
    int cuts = 0;
    int frames = 0;
    QString dimensions;
    QString bgm;
};

MixcutContext makeMixcutContext(const MixcutProject &project)
{
    MixcutContext ctx;
    const MixcutSource source = project.hasSource ? project.source : mixcutDemoSource();
    const MixcutStyleMeta &meta = mixcutStyleMeta(project.style);
    const MixcutVoice voice = getMixcutVoice(project.voiceId);
    const QString script = project.script.trimmed().isEmpty()
        ? generateMixcutScriptFromPrompt(project.prompt, project.style, 0)
        : project.script;
    ctx.stats = mixcutScriptStats(script);
    ctx.voice = voice;
    ctx.source = source;
    ctx.ratio = project.ratio;
    ctx.keywords = meta.keywords;
    ctx.captions = ctx.stats.lines;
    ctx.subtitleCount = qMax(8, qRound(ctx.stats.chars / 7.0));
    ctx.sfxCount = qBound(3, qRound(ctx.stats.seconds / 5.0), 12);
    ctx.cuts = qBound(4, qRound(ctx.stats.seconds / 3.0), source.shots);
    ctx.frames = ctx.stats.seconds * 30;
    ctx.dimensions = mixcutRatioDimensions(project.ratio);
    ctx.bgm = meta.bgm;
    return ctx;
}

QVector<MixcutStageSeed> mixcutStageSeeds(const MixcutContext &ctx)
{
    return {
        {MixcutStage::Parse, QStringLiteral("解析文案"), 3000,
         QStringLiteral("%1 句 · %2 字")
             .arg(ctx.stats.sentences)
             .arg(ctx.stats.chars),
         {{300,
           QStringLiteral("读取口播文案，共 %1 字").arg(ctx.stats.chars),
           Tone::Info},
          {1200,
           QStringLiteral("按语义切成 %1 句字幕，标注重音与停顿")
               .arg(ctx.stats.sentences),
           Tone::Ai},
          {2300,
           QStringLiteral("提取关键词：%1")
               .arg(ctx.keywords.mid(0, 4).join(QStringLiteral("、"))),
           Tone::Ai}}},
        {MixcutStage::Voice, QStringLiteral("配音对齐"), 4200,
         QStringLiteral("%1 · %2").arg(ctx.voice.name, ctx.stats.durationLabel),
         {{300,
           QStringLiteral("载入已生成的配音：%1 · %2")
               .arg(ctx.voice.name, ctx.voice.label),
           Tone::Info},
          {1500, QStringLiteral("按镜头切点重排配音节奏，重音与停顿保持自然"),
           Tone::Ai},
          {3200,
           QStringLiteral("配音与画面时间轴对齐完成 · %1").arg(ctx.stats.durationLabel),
           Tone::Ok}}},
        {MixcutStage::Match, QStringLiteral("匹配画面"), 4400,
         QStringLiteral("%1 个镜头 · %2 处切换")
             .arg(ctx.source.shots)
             .arg(ctx.cuts),
         {{300,
           QStringLiteral("分析原片 %1 个镜头，识别画面主体与景别")
               .arg(ctx.source.shots),
           Tone::Ai},
          {1600, QStringLiteral("开场钩子 → 通勤街景（00:02–00:08）"), Tone::Ai},
          {2800, QStringLiteral("产品卖点 → 产品特写 / 质地展示"), Tone::Ai},
          {3800,
           QStringLiteral("按文案重音调整切点，共 %1 处镜头切换").arg(ctx.cuts),
           Tone::Ok}}},
        {MixcutStage::Design, QStringLiteral("字幕与音效"), 5000,
         QStringLiteral("%1 条字幕 · %2 个音效")
             .arg(ctx.subtitleCount)
             .arg(ctx.sfxCount),
         {{300,
           QStringLiteral("生成 %1 条字幕，关键词自动高亮").arg(ctx.subtitleCount),
           Tone::Ai},
          {1400,
           QStringLiteral("匹配 BGM：%1 · 人声段自动闪避").arg(ctx.bgm),
           Tone::Info},
          {2500,
           QStringLiteral("在 %1 处转场加入卡点音效").arg(ctx.sfxCount),
           Tone::Info},
          {3600, QStringLiteral("片头标题与花字已生成"), Tone::Ai},
          {4500, QStringLiteral("字幕、画面与音频轨道对齐完成"), Tone::Ok}}},
        {MixcutStage::Render, QStringLiteral("渲染成片"), 5000,
         QStringLiteral("%1 · 30fps").arg(ctx.dimensions),
         {{400,
           QStringLiteral("开始云渲染 %1 · 30fps").arg(ctx.dimensions),
           Tone::Info},
          {1800,
           QStringLiteral("画面编码中，已渲染 %1/%2 帧")
               .arg(qRound(ctx.frames * 0.34))
               .arg(ctx.frames),
           Tone::Info},
          {3300,
           QStringLiteral("混音完成，已渲染 %1/%2 帧")
               .arg(qRound(ctx.frames * 0.72))
               .arg(ctx.frames),
           Tone::Info},
          {4600, QStringLiteral("成片与剪映草稿已生成，自动下载到本地"),
           Tone::Ok}}},
    };
}

} // namespace

/* ------------------------------------------------------------------ */
/* 格式化                                                              */
/* ------------------------------------------------------------------ */

QString formatElapsed(qint64 milliseconds)
{
    const qint64 totalSeconds = qMax<qint64>(0, milliseconds / 1000);
    const qint64 minutes = totalSeconds / 60;
    const qint64 seconds = totalSeconds % 60;
    return QStringLiteral("%1:%2")
        .arg(minutes, 2, 10, QLatin1Char('0'))
        .arg(seconds, 2, 10, QLatin1Char('0'));
}

QString formatEta(int seconds)
{
    if (seconds <= 0) {
        return QStringLiteral("0 秒");
    }
    if (seconds < 60) {
        return QStringLiteral("%1 秒").arg(seconds);
    }
    const int minutes = seconds / 60;
    const int rest = seconds % 60;
    return rest == 0 ? QStringLiteral("%1 分").arg(minutes)
                     : QStringLiteral("%1 分 %2 秒").arg(minutes).arg(rest);
}

QString formatNumber(qint64 value)
{
    QString text = QString::number(value);
    for (int index = text.size() - 3; index > 0; index -= 3) {
        text.insert(index, QLatin1Char(','));
    }
    return text;
}

QString formatGenerationTime(qint64 timestampMs)
{
    if (timestampMs <= 0) {
        return QString();
    }
    const QDateTime date = QDateTime::fromMSecsSinceEpoch(timestampMs);
    const QDateTime now = QDateTime::currentDateTime();
    const QString clock = formatGenerationClock(date);
    const qint64 elapsed = now.toMSecsSinceEpoch() - timestampMs;

    if (elapsed >= 0 && elapsed < 60 * 1000) {
        return QStringLiteral("刚刚 · %1").arg(clock);
    }

    const int dayDiff = static_cast<int>(date.date().daysTo(now.date()));
    if (dayDiff <= 0) {
        return QStringLiteral("今天 %1").arg(clock);
    }
    if (dayDiff == 1) {
        return QStringLiteral("昨天 %1").arg(clock);
    }
    if (dayDiff == 2) {
        return QStringLiteral("前天 %1").arg(clock);
    }
    if (dayDiff < 7) {
        return QStringLiteral("%1 天前 %2").arg(dayDiff).arg(clock);
    }
    if (date.date().year() == now.date().year()) {
        return QStringLiteral("%1月%2日 %3")
            .arg(date.date().month())
            .arg(date.date().day())
            .arg(clock);
    }
    return QStringLiteral("%1年%2月%3日 %4")
        .arg(date.date().year())
        .arg(date.date().month())
        .arg(date.date().day())
        .arg(clock);
}

QString formatGenerationFullTime(qint64 timestampMs)
{
    const QDateTime date = QDateTime::fromMSecsSinceEpoch(timestampMs);
    return QStringLiteral("%1年%2月%3日 %4")
        .arg(date.date().year())
        .arg(date.date().month())
        .arg(date.date().day())
        .arg(formatGenerationClock(date));
}

int parseElapsed(const QString &clock)
{
    const QStringList parts = clock.split(QLatin1Char(':'));
    if (parts.size() == 2) {
        return parts.at(0).toInt() * 60 + parts.at(1).toInt();
    }
    if (parts.size() == 3) {
        return parts.at(0).toInt() * 3600 + parts.at(1).toInt() * 60
            + parts.at(2).toInt();
    }
    return qMax(0, clock.toInt());
}

/* ------------------------------------------------------------------ */
/* 智能口播：模板                                                       */
/* ------------------------------------------------------------------ */

const QVector<OralTemplate> &oralTemplates()
{
    static const QVector<OralTemplate> templates = {
        {QStringLiteral("punch"), QStringLiteral("高能带货"),
         QStringLiteral("大字快节奏 · 关键词高亮"),
         QStringLiteral("适合种草、带货与转化型口播，重点信息一眼可见。"),
         {QStringLiteral("带货"), QStringLiteral("转化"), QStringLiteral("大字")},
         QColor(QStringLiteral("#FFB224")), QColor(QStringLiteral("#2b1c0b")),
         QColor(QStringLiteral("#3d2410")), QColor(QStringLiteral("#0d0a07")),
         OralLayout::Punch, QStringLiteral("油皮防晒怎么选"),
         QStringLiteral("记住这三点，夏天不闷痘"), OralFont::Sans},
        {QStringLiteral("clean"), QStringLiteral("清透讲解"),
         QStringLiteral("留白呼吸感 · 重点划线"),
         QStringLiteral("适合知识、测评与经验分享，信息清楚，节奏舒服。"),
         {QStringLiteral("知识"), QStringLiteral("讲解"), QStringLiteral("清透")},
         QColor(QStringLiteral("#6E8BFF")), QColor(QStringLiteral("#101d2b")),
         QColor(QStringLiteral("#173044")), QColor(QStringLiteral("#0a1119")),
         OralLayout::Clean, QStringLiteral("3 个通勤防晒技巧"),
         QStringLiteral("轻、透、稳，一支就够了"), OralFont::Sans},
        {QStringLiteral("vlog"), QStringLiteral("生活 Vlog"),
         QStringLiteral("暖调手写 · 轻松日常"),
         QStringLiteral("适合生活方式、好物分享与日常记录，亲切不端着。"),
         {QStringLiteral("日常"), QStringLiteral("种草"), QStringLiteral("暖调")},
         QColor(QStringLiteral("#FF9E7A")), QColor(QStringLiteral("#2d1c13")),
         QColor(QStringLiteral("#4a2f1f")), QColor(QStringLiteral("#120c08")),
         OralLayout::Vlog, QStringLiteral("我的通勤包里有什么"),
         QStringLiteral("这支防晒，我回购了三次"), OralFont::Serif},
        {QStringLiteral("tech"), QStringLiteral("科技质感"),
         QStringLiteral("冷色网格 · 数据标注"),
         QStringLiteral("适合数码测评、成分分析与硬核讲解，理性有质感。"),
         {QStringLiteral("测评"), QStringLiteral("数码"), QStringLiteral("数据")},
         QColor(QStringLiteral("#55D6E0")), QColor(QStringLiteral("#081b20")),
         QColor(QStringLiteral("#0d2c34")), QColor(QStringLiteral("#051014")),
         OralLayout::Tech, QStringLiteral("实测 8 小时通勤"),
         QStringLiteral("成膜速度 3.2 秒 · 不搓泥"), OralFont::Mono},
        {QStringLiteral("magazine"), QStringLiteral("杂志大片"),
         QStringLiteral("衬线标题 · 高级留白"),
         QStringLiteral("适合品牌故事、质感展示与高阶种草，画面更克制。"),
         {QStringLiteral("品牌"), QStringLiteral("质感"), QStringLiteral("克制")},
         QColor(QStringLiteral("#E8D5A9")), QColor(QStringLiteral("#241f17")),
         QColor(QStringLiteral("#3b3327")), QColor(QStringLiteral("#0c0a07")),
         OralLayout::Magazine, QStringLiteral("THE EVERYDAY EDIT"),
         QStringLiteral("轻盈防晒，让通勤更从容"), OralFont::Serif},
        {QStringLiteral("neon"), QStringLiteral("潮酷弹幕"),
         QStringLiteral("霓虹撞色 · 动态花字"),
         QStringLiteral("适合年轻潮流、剧情与强观点内容，画面更有冲击力。"),
         {QStringLiteral("潮流"), QStringLiteral("年轻"), QStringLiteral("高能")},
         QColor(QStringLiteral("#FF5BB5")), QColor(QStringLiteral("#1e0b28")),
         QColor(QStringLiteral("#3c1650")), QColor(QStringLiteral("#0b0610")),
         OralLayout::Neon, QStringLiteral("别乱买！先看这条"),
         QStringLiteral("油皮防晒的正确答案"), OralFont::Sans},
    };
    return templates;
}

OralTemplate oralTemplate(const QString &id)
{
    for (const OralTemplate &item : oralTemplates()) {
        if (item.id == id) {
            return item;
        }
    }
    return oralTemplates().at(1);
}

QString oralTemplateName(const QString &id)
{
    return oralTemplate(id).name;
}

const QVector<OralMaterial> &demoMaterials()
{
    static const QVector<OralMaterial> materials = {
        {QStringLiteral("mat-serum"), QStringLiteral("产品特写"),
         QStringLiteral("主图"), QStringLiteral("图片"), QString(),
         QStringLiteral("serum"), QColor(QStringLiteral("#d8a15a")),
         QColor(QStringLiteral("#5a3520"))},
        {QStringLiteral("mat-swatch"), QStringLiteral("质地展示"),
         QStringLiteral("质地"), QStringLiteral("视频"), QStringLiteral("00:06"),
         QStringLiteral("swatch"), QColor(QStringLiteral("#f1d5c6")),
         QColor(QStringLiteral("#9e6d6b"))},
        {QStringLiteral("mat-face"), QStringLiteral("上脸效果"),
         QStringLiteral("效果"), QStringLiteral("视频"), QStringLiteral("00:08"),
         QStringLiteral("face"), QColor(QStringLiteral("#e6b19b")),
         QColor(QStringLiteral("#4a2d35"))},
        {QStringLiteral("mat-city"), QStringLiteral("通勤场景"),
         QStringLiteral("场景"), QStringLiteral("视频"), QStringLiteral("00:05"),
         QStringLiteral("city"), QColor(QStringLiteral("#2c496d")),
         QColor(QStringLiteral("#e0a15f"))},
        {QStringLiteral("mat-bag"), QStringLiteral("包装开箱"),
         QStringLiteral("开箱"), QStringLiteral("图片"), QString(),
         QStringLiteral("bag"), QColor(QStringLiteral("#d7aa84")),
         QColor(QStringLiteral("#553c34"))},
        {QStringLiteral("mat-sun"), QStringLiteral("户外阳光"),
         QStringLiteral("场景"), QStringLiteral("视频"), QStringLiteral("00:04"),
         QStringLiteral("sun"), QColor(QStringLiteral("#f0b55d")),
         QColor(QStringLiteral("#315074"))},
        {QStringLiteral("mat-ingredient"), QStringLiteral("成分特写"),
         QStringLiteral("成分"), QStringLiteral("图片"), QString(),
         QStringLiteral("cream"), QColor(QStringLiteral("#a9c7b2")),
         QColor(QStringLiteral("#2d4439"))},
        {QStringLiteral("mat-desk"), QStringLiteral("使用步骤"),
         QStringLiteral("教程"), QStringLiteral("视频"), QStringLiteral("00:07"),
         QStringLiteral("desk"), QColor(QStringLiteral("#9e8cc4")),
         QColor(QStringLiteral("#2c243d"))},
    };
    return materials;
}

OralSource demoOralSource()
{
    OralSource source;
    source.name = QStringLiteral("通勤防晒_口播原片.mp4");
    source.duration = QStringLiteral("02:48");
    source.resolution = QStringLiteral("1920×1080");
    source.size = QStringLiteral("412 MB");
    source.words = 486;
    source.segments = 6;
    source.sample = true;
    source.transcript = {
        {QStringLiteral("00:00"),
         QStringLiteral("夏天通勤最怕的不是晒黑，是闷。"), QStringLiteral("闷")},
        {QStringLiteral("00:12"),
         QStringLiteral("油皮选防晒，先看质地，再看成膜速度。"),
         QStringLiteral("成膜速度")},
        {QStringLiteral("00:35"),
         QStringLiteral("这支是水感质地，推开三秒就成膜，不搓泥。"),
         QStringLiteral("三秒成膜")},
        {QStringLiteral("01:02"),
         QStringLiteral("SPF50+ PA++++，早晚通勤完全够用。"),
         QStringLiteral("SPF50+")},
        {QStringLiteral("01:36"),
         QStringLiteral("上脸不泛白，后续跟妆也不会打架。"),
         QStringLiteral("不泛白")},
        {QStringLiteral("02:10"),
         QStringLiteral("如果你也怕闷痘，可以先从这支试起。"),
         QStringLiteral("不闷痘")},
    };
    source.topics = {QStringLiteral("通勤防晒"), QStringLiteral("油皮"),
                     QStringLiteral("成膜"), QStringLiteral("不搓泥"),
                     QStringLiteral("SPF50+")};
    return source;
}

OralProject defaultOralProject()
{
    OralProject project;
    project.hasSource = true;
    project.source = demoOralSource();
    project.materials = demoMaterials();
    return project;
}

QString oralRatioDimensions(const QString &ratio)
{
    return ratio == QLatin1String("16:9") ? QStringLiteral("1920×1080")
                                          : QStringLiteral("1080×1920");
}

int oralBrollCount(OralDensity density)
{
    switch (density) {
    case OralDensity::Low:
        return 8;
    case OralDensity::High:
        return 16;
    case OralDensity::Medium:
    default:
        return 12;
    }
}

QString oralDensityName(OralDensity density)
{
    switch (density) {
    case OralDensity::Low:
        return QStringLiteral("少");
    case OralDensity::High:
        return QStringLiteral("丰富");
    case OralDensity::Medium:
    default:
        return QStringLiteral("适中");
    }
}

QString shortOralCaption(const QString &text, int maxLength)
{
    const QString first = text.split(QRegularExpression(QStringLiteral("[。！？\\n]"))).value(0).trimmed();
    if (first.isEmpty()) {
        return QString();
    }
    if (first.size() <= maxLength) {
        return first;
    }

    const QStringList clauses = first.split(QRegularExpression(QStringLiteral("[，,]")));
    QString result;
    for (const QString &clause : clauses) {
        const QString next = result.isEmpty() ? clause : result + QStringLiteral("，") + clause;
        if (!result.isEmpty() && next.size() > maxLength) {
            break;
        }
        result = next;
    }
    return result.isEmpty() ? first.left(maxLength) + QStringLiteral("…") : result;
}

/* ------------------------------------------------------------------ */
/* 智能口播：生成过程                                                   */
/* ------------------------------------------------------------------ */

qint64 oralRunTotalMs()
{
    qint64 total = 0;
    for (const OralStageSeed &seed : oralStageSeeds()) {
        total += seed.durationMs;
    }
    return total;
}

OralRun computeOralRun(qint64 elapsedMs, int runId)
{
    const qint64 totalMs = oralRunTotalMs();
    const qint64 clamped = qBound<qint64>(0, elapsedMs, totalMs);
    const bool done = clamped >= totalMs;

    OralRun run;
    run.runId = runId;
    run.done = done;
    run.elapsedMs = clamped;
    run.totalMs = totalMs;
    run.progress = totalMs > 0 ? (static_cast<double>(clamped) / totalMs) * 100.0 : 0.0;
    run.etaSeconds = done ? 0 : static_cast<int>(std::ceil((totalMs - clamped) / 1000.0));
    run.stage = OralStage::Render;
    run.totalFrames = 1440;

    qint64 offset = 0;
    for (const OralStageSeed &seed : oralStageSeeds()) {
        const qint64 start = offset;
        const qint64 end = start + seed.durationMs;
        offset = end;

        OralStageState stage;
        stage.key = seed.key;
        stage.label = seed.label;
        if (clamped >= end) {
            stage.status = StageStatus::Done;
        } else if (clamped >= start) {
            stage.status = StageStatus::Running;
        } else {
            stage.status = StageStatus::Waiting;
        }
        stage.progress = clamped >= end
            ? 100.0
            : (clamped <= start
                   ? 0.0
                   : (static_cast<double>(clamped - start) / seed.durationMs) * 100.0);

        QString lastLog;
        for (int index = 0; index < seed.logs.size(); ++index) {
            const OralLogSeed &log = seed.logs.at(index);
            if (clamped < start + log.atMs) {
                continue;
            }
            OralLogEntry entry;
            entry.id = QStringLiteral("%1-%2").arg(static_cast<int>(seed.key)).arg(index);
            entry.stage = seed.key;
            entry.atMs = start + log.atMs;
            entry.text = log.text;
            entry.tone = log.tone;
            run.logs.append(entry);
            lastLog = log.text;
        }

        if (stage.status == StageStatus::Done) {
            stage.detail = seed.doneDetail;
        } else if (stage.status == StageStatus::Running) {
            stage.detail = lastLog.isEmpty() ? QStringLiteral("正在准备…") : lastLog;
            run.stage = seed.key;
        } else {
            stage.detail = QStringLiteral("等待上一步完成");
        }
        run.stages.append(stage);
    }

    const qint64 matchStart = oralStageStart(OralStage::Match);
    const qint64 matchEnd = matchStart + 5000;
    const qint64 composeStart = oralStageStart(OralStage::Compose);
    const qint64 renderStart = oralStageStart(OralStage::Render);
    const qint64 renderEnd = renderStart + 6000;

    if (clamped < 2600) {
        run.previewCaption = QStringLiteral("正在理解口播内容…");
    } else {
        run.previewCaption = oralCaptionSeeds().first().text;
        for (const OralCaptionSeed &caption : oralCaptionSeeds()) {
            if (caption.atMs <= clamped) {
                run.previewCaption = caption.text;
            }
        }
    }

    for (int index = 0; index < kOralKeywords.size(); ++index) {
        if (clamped >= 2600 + index * 2600) {
            run.previewKeywords.append(kOralKeywords.at(index));
        }
    }

    run.matchedMaterials = qRound(stageRatio(clamped, matchStart, matchEnd) * 8.0);
    run.subtitleCount = qRound(stageRatio(clamped, composeStart, composeStart + 7000) * 24.0);
    run.renderedFrames = qRound(stageRatio(clamped, renderStart, renderEnd) * 1440.0);
    return run;
}

/* ------------------------------------------------------------------ */
/* 智能口播：结果                                                       */
/* ------------------------------------------------------------------ */

OralResult makeOralResult(const OralProject &project, qint64 generatedAt,
                          const QString &templateName)
{
    OralResult result;
    result.id = QStringLiteral("oral-%1").arg(generatedAt);
    result.projectName = QStringLiteral("通勤防晒 · 口播包装");
    result.title = QStringLiteral("油皮夏天通勤防晒，记住这三点就够了");
    result.description = QStringLiteral("不搓泥、不闷痘，8 小时通勤实测。油皮姐妹的夏日防晒答案。");
    result.tags = {QStringLiteral("#通勤防晒"), QStringLiteral("#油皮护肤"),
                   QStringLiteral("#防晒测评"), QStringLiteral("#夏日好物")};
    result.templateId = project.templateId;
    result.ratio = project.ratio;
    result.durationLabel = QStringLiteral("00:58");
    result.fileSize = project.ratio == QLatin1String("16:9")
        ? QStringLiteral("124.8 MB")
        : QStringLiteral("86.4 MB");
    result.outputBoth = project.outputBoth;
    result.generatedAt = generatedAt;
    result.sourceLabel = QStringLiteral("真人口播 · %1")
                             .arg(project.hasSource ? project.source.duration
                                                    : QStringLiteral("02:48"));
    QStringList scriptLines;
    for (const OralTranscriptLine &line : demoOralSource().transcript) {
        scriptLines.append(line.text);
    }
    result.script = scriptLines.join(QLatin1Char('\n'));
    result.subtitleCount = 24;
    result.brollCount = oralBrollCount(project.density);
    result.draftGenerated = project.outputBoth;
    result.highlights = {
        {QStringLiteral("字幕"),
         QStringLiteral("%1 条 · 关键词高亮").arg(result.subtitleCount)},
        {QStringLiteral("素材"),
         QStringLiteral("%1 处 B-Roll").arg(result.brollCount)},
        {QStringLiteral("节奏"),
         QStringLiteral("%1 个转场").arg(project.transitions ? 4 : 0)},
        {QStringLiteral("模板"), templateName},
    };
    return result;
}

QVector<OralResult> recentOralResults()
{
    const qint64 now = QDateTime::currentMSecsSinceEpoch();
    const qint64 hour = 60 * 60 * 1000;
    const qint64 day = 24 * hour;

    QVector<OralResult> results;
    OralResult first;
    first.id = QStringLiteral("recent-commute");
    first.projectName = QStringLiteral("通勤防晒 · 口播包装");
    first.title = QStringLiteral("油皮夏天通勤防晒，记住这三点就够了");
    first.description = QStringLiteral("不搓泥、不闷痘，8 小时通勤实测。油皮姐妹的夏日防晒答案。");
    first.tags = {QStringLiteral("#通勤防晒"), QStringLiteral("#油皮护肤"),
                  QStringLiteral("#防晒测评"), QStringLiteral("#夏日好物")};
    first.templateId = QStringLiteral("clean");
    first.ratio = QStringLiteral("9:16");
    first.durationLabel = QStringLiteral("00:58");
    first.fileSize = QStringLiteral("86.4 MB");
    first.outputBoth = true;
    first.generatedAt = now - 2 * hour;
    first.sourceLabel = QStringLiteral("真人口播 · 02:48");
    {
        QStringList lines;
        for (const OralTranscriptLine &line : demoOralSource().transcript) {
            lines.append(line.text);
        }
        first.script = lines.join(QLatin1Char('\n'));
    }
    first.subtitleCount = 24;
    first.brollCount = 12;
    first.draftGenerated = true;
    first.highlights = {
        {QStringLiteral("字幕"), QStringLiteral("24 条 · 关键词高亮")},
        {QStringLiteral("素材"), QStringLiteral("12 处 B-Roll")},
        {QStringLiteral("节奏"), QStringLiteral("4 个转场")},
        {QStringLiteral("模板"), QStringLiteral("清透讲解")},
    };

    OralResult second;
    second.id = QStringLiteral("recent-ingredient");
    second.projectName = QStringLiteral("成分科普 · 口播包装");
    second.title = QStringLiteral("5% 神经酰胺到底是什么？一条视频讲清楚");
    second.description =
        QStringLiteral("成分党必看：神经酰胺 + 角鲨烷，为什么适合换季维稳。");
    second.tags = {QStringLiteral("#成分科普"), QStringLiteral("#护肤知识"),
                   QStringLiteral("#神经酰胺"), QStringLiteral("#敏感肌")};
    second.templateId = QStringLiteral("tech");
    second.ratio = QStringLiteral("9:16");
    second.durationLabel = QStringLiteral("01:04");
    second.fileSize = QStringLiteral("96.2 MB");
    second.outputBoth = false;
    second.generatedAt = now - 26 * hour;
    second.sourceLabel = QStringLiteral("真人口播 · 03:12");
    second.script = QStringLiteral(
        "换季护肤，先别急着叠加功效产品。\n"
        "神经酰胺是皮肤屏障的重要组成部分，能帮助减少水分流失。\n"
        "搭配角鲨烷，可以在表面形成一层轻薄的保护膜。\n"
        "这罐面霜用的是 5% 神经酰胺，敏感肌也能用。");
    second.subtitleCount = 26;
    second.brollCount = 10;
    second.draftGenerated = false;
    second.highlights = {
        {QStringLiteral("字幕"), QStringLiteral("26 条 · 关键词高亮")},
        {QStringLiteral("素材"), QStringLiteral("10 处 B-Roll")},
        {QStringLiteral("节奏"), QStringLiteral("3 个转场")},
        {QStringLiteral("模板"), QStringLiteral("科技质感")},
    };

    OralResult third;
    third.id = QStringLiteral("recent-unbox");
    third.projectName = QStringLiteral("新品开箱 · 口播包装");
    third.title = QStringLiteral("开箱这支防晒，包装和肤感都很惊喜");
    third.description =
        QStringLiteral("第一次开箱就空瓶的防晒，质地轻盈，通勤党可以直接抄作业。");
    third.tags = {QStringLiteral("#开箱"), QStringLiteral("#防晒推荐"),
                  QStringLiteral("#通勤好物")};
    third.templateId = QStringLiteral("magazine");
    third.ratio = QStringLiteral("16:9");
    third.durationLabel = QStringLiteral("00:42");
    third.fileSize = QStringLiteral("124.8 MB");
    third.outputBoth = false;
    third.generatedAt = now - 2 * day;
    third.sourceLabel = QStringLiteral("真人口播 · 01:36");
    {
        QStringList lines;
        const QVector<OralTranscriptLine> transcript = demoOralSource().transcript;
        for (int index = 0; index < qMin(4, transcript.size()); ++index) {
            lines.append(transcript.at(index).text);
        }
        third.script = lines.join(QLatin1Char('\n'));
    }
    third.subtitleCount = 16;
    third.brollCount = 8;
    third.draftGenerated = false;
    third.highlights = {
        {QStringLiteral("字幕"), QStringLiteral("16 条 · 关键词高亮")},
        {QStringLiteral("素材"), QStringLiteral("8 处 B-Roll")},
        {QStringLiteral("节奏"), QStringLiteral("3 个转场")},
        {QStringLiteral("模板"), QStringLiteral("杂志大片")},
    };

    results = {first, second, third};
    return results;
}

OralMaterial oralMaterialFromFile(const QString &fileName, qint64 sizeBytes,
                                  bool video, int index)
{
    Q_UNUSED(sizeBytes);
    static const QStringList visuals = {
        QStringLiteral("serum"), QStringLiteral("swatch"), QStringLiteral("face"),
        QStringLiteral("city"), QStringLiteral("bag"), QStringLiteral("sun"),
    };
    static const QVector<QPair<QColor, QColor>> palettes = {
        {QColor(QStringLiteral("#d8a15a")), QColor(QStringLiteral("#5a3520"))},
        {QColor(QStringLiteral("#dfb6a0")), QColor(QStringLiteral("#6b3f3f"))},
        {QColor(QStringLiteral("#a9c7b2")), QColor(QStringLiteral("#2d4439"))},
        {QColor(QStringLiteral("#829ccc")), QColor(QStringLiteral("#29364f"))},
        {QColor(QStringLiteral("#d6a080")), QColor(QStringLiteral("#4b3227"))},
        {QColor(QStringLiteral("#c7a1c9")), QColor(QStringLiteral("#3b2943"))},
    };

    OralMaterial material;
    material.id = QStringLiteral("upload-%1-%2")
                      .arg(QDateTime::currentMSecsSinceEpoch())
                      .arg(index);
    QString name = strippedName(fileName).left(16);
    if (name.isEmpty()) {
        name = QStringLiteral("素材 %1").arg(index + 1);
    }
    material.name = name;
    material.tag = video ? QStringLiteral("视频素材") : QStringLiteral("图片素材");
    material.kind = video ? QStringLiteral("视频") : QStringLiteral("图片");
    material.duration = video
        ? QStringLiteral("00:0%1").arg(4 + index % 5)
        : QString();
    material.visual = visuals.at(index % visuals.size());
    const QPair<QColor, QColor> palette = palettes.at(index % palettes.size());
    material.paletteFrom = palette.first;
    material.paletteTo = palette.second;
    return material;
}

/* ------------------------------------------------------------------ */
/* 智能混剪：音色、风格与文案                                           */
/* ------------------------------------------------------------------ */

QString mixcutStyleName(MixcutStyle style)
{
    switch (style) {
    case MixcutStyle::Review:
        return QStringLiteral("评测");
    case MixcutStyle::Story:
        return QStringLiteral("故事");
    case MixcutStyle::Planting:
    default:
        return QStringLiteral("种草");
    }
}

QVector<MixcutStyle> mixcutStyles()
{
    return {MixcutStyle::Planting, MixcutStyle::Review, MixcutStyle::Story};
}

const QVector<MixcutVoice> &mixcutVoices()
{
    static const QVector<MixcutVoice> voices = {
        {QStringLiteral("linlan"), QStringLiteral("林澜"), QStringLiteral("知性女声"),
         QStringLiteral("女声"), {QStringLiteral("自然"), QStringLiteral("知性")},
         QStringLiteral("自然有质感的知性女声，适合干货讲解与日常种草。"),
         QColor(QStringLiteral("#3C4C94")), QColor(QStringLiteral("#6E8BFF")),
         QColor(QStringLiteral("#6E8BFF")), true},
        {QStringLiteral("xiaotao"), QStringLiteral("小桃"), QStringLiteral("元气少女"),
         QStringLiteral("女声"), {QStringLiteral("甜美"), QStringLiteral("轻快")},
         QStringLiteral("明亮轻快，适合年轻化种草与生活分享。"),
         QColor(QStringLiteral("#A34A38")), QColor(QStringLiteral("#FF9E7A")),
         QColor(QStringLiteral("#FF9E7A")), false},
        {QStringLiteral("chenyu"), QStringLiteral("陈屿"), QStringLiteral("磁性男声"),
         QStringLiteral("男声"), {QStringLiteral("磁性"), QStringLiteral("沉稳")},
         QStringLiteral("低音沉稳，适合评测、数码与品牌解说。"),
         QColor(QStringLiteral("#0E5A64")), QColor(QStringLiteral("#55D6E0")),
         QColor(QStringLiteral("#55D6E0")), false},
        {QStringLiteral("anan"), QStringLiteral("安安"), QStringLiteral("温柔女声"),
         QStringLiteral("女声"), {QStringLiteral("温柔"), QStringLiteral("治愈")},
         QStringLiteral("温柔亲切，适合生活方式与情感叙事。"),
         QColor(QStringLiteral("#6B4A72")), QColor(QStringLiteral("#C7A1C9")),
         QColor(QStringLiteral("#C7A1C9")), false},
        {QStringLiteral("akai"), QStringLiteral("阿凯"), QStringLiteral("活力男声"),
         QStringLiteral("男声"), {QStringLiteral("活力"), QStringLiteral("带货")},
         QStringLiteral("热情有冲劲，适合带货与强节奏内容。"),
         QColor(QStringLiteral("#8A5A0E")), QColor(QStringLiteral("#FFB224")),
         QColor(QStringLiteral("#FFB224")), false},
        {QStringLiteral("laozhou"), QStringLiteral("老周"), QStringLiteral("沉稳男声"),
         QStringLiteral("男声"), {QStringLiteral("解说"), QStringLiteral("可信")},
         QStringLiteral("纪录片式叙述，可信度高，适合测评与知识内容。"),
         QColor(QStringLiteral("#2F5B43")), QColor(QStringLiteral("#7FD8A8")),
         QColor(QStringLiteral("#7FD8A8")), false},
    };
    return voices;
}

MixcutVoice getMixcutVoice(const QString &id)
{
    for (const MixcutVoice &voice : mixcutVoices()) {
        if (voice.id == id) {
            return voice;
        }
    }
    return mixcutVoices().first();
}

const MixcutStyleMeta &mixcutStyleMeta(MixcutStyle style)
{
    static const MixcutStyleMeta planting = {
        QStringLiteral("通勤防晒"), QStringLiteral("夏天通勤防晒指南"),
        QStringLiteral("夏天通勤防晒，油皮也能清爽一整天"),
        QStringLiteral("三秒成膜、不搓泥，通勤路上的防晒答案。油皮姐妹可以直接抄作业。"),
        {QStringLiteral("#通勤防晒"), QStringLiteral("#油皮护肤"),
         QStringLiteral("#防晒测评"), QStringLiteral("#夏日好物")},
        {QStringLiteral("通勤"), QStringLiteral("成膜"), QStringLiteral("不搓泥"),
         QStringLiteral("SPF50+")},
        QStringLiteral("轻快通勤")};
    static const MixcutStyleMeta review = {
        QStringLiteral("防晒实测"), QStringLiteral("8 小时通勤实测"),
        QStringLiteral("实测 8 小时通勤：这支防晒凭什么不搓泥"),
        QStringLiteral("成膜速度、泛白程度、跟妆表现，一条视频讲清楚。油皮通勤党必看。"),
        {QStringLiteral("#防晒测评"), QStringLiteral("#成分党"),
         QStringLiteral("#通勤实测"), QStringLiteral("#护肤干货")},
        {QStringLiteral("实测"), QStringLiteral("成膜"), QStringLiteral("不泛白"),
         QStringLiteral("8 小时")},
        QStringLiteral("科技脉搏")};
    static const MixcutStyleMeta story = {
        QStringLiteral("通勤好物"), QStringLiteral("我的通勤防晒故事"),
        QStringLiteral("找了三年，终于遇到通勤不闷的防晒"),
        QStringLiteral("从踩雷到回购，一个油皮打工人的真实防晒记录。"),
        {QStringLiteral("#通勤日常"), QStringLiteral("#好物分享"),
         QStringLiteral("#油皮护肤"), QStringLiteral("#防晒")},
        {QStringLiteral("通勤"), QStringLiteral("回购"), QStringLiteral("不闷痘"),
         QStringLiteral("油皮")},
        QStringLiteral("暖阳钢琴")};
    switch (style) {
    case MixcutStyle::Review:
        return review;
    case MixcutStyle::Story:
        return story;
    case MixcutStyle::Planting:
    default:
        return planting;
    }
}

const QVector<QStringList> &mixcutScriptSeeds(MixcutStyle style)
{
    static const QVector<QStringList> planting = {
        {QStringLiteral("夏天通勤，最怕底妆被晒花，脸又闷又油。"),
         QStringLiteral("这支防晒是水感质地，推开三秒成膜，不搓泥也不泛白。"),
         QStringLiteral("轻轻一层就够，后续跟妆也很服帖。"),
         QStringLiteral("SPF50+ PA++++，从地铁到工位，一整天都稳稳的。"),
         QStringLiteral("油皮姐妹，真的可以从这支开始试。")},
        {QStringLiteral("通勤防晒别再乱买了，油皮先看这三点。"),
         QStringLiteral("第一，质地要水感，推开不拉扯皮肤。"),
         QStringLiteral("第二，成膜要快，三秒成膜才不耽误出门。"),
         QStringLiteral("第三，不泛白、不搓泥，后续跟妆才服帖。"),
         QStringLiteral("这支全做到了，SPF50+ 也够一整天的通勤。")}};
    static const QVector<QStringList> review = {
        {QStringLiteral("今天实测这支通勤防晒，重点看成膜速度和跟妆表现。"),
         QStringLiteral("上手是水感质地，推开三秒成膜，成膜后不黏手。"),
         QStringLiteral("上脸不泛白，油皮鼻翼两侧也没有明显搓泥。"),
         QStringLiteral("带妆八小时，只有轻微出油，底妆基本完整。"),
         QStringLiteral("SPF50+ PA++++，日常通勤完全够用。")},
        {QStringLiteral("一支防晒到底好不好，通勤八小时就能看出来。"),
         QStringLiteral("我们测了成膜速度、泛白程度和跟妆表现。"),
         QStringLiteral("它在三秒内成膜，纸巾按压不粘，说明不搓泥。"),
         QStringLiteral("上脸是自然哑光感，没有假白和油光。"),
         QStringLiteral("如果你也是油皮通勤党，这支值得试。")}};
    static const QVector<QStringList> story = {
        {QStringLiteral("以前我总觉得，防晒和清爽不能同时拥有。"),
         QStringLiteral("直到通勤路上被太阳晒到泛红，我才认真找替代。"),
         QStringLiteral("这支是朋友递给我的，第一次用就被质地惊到。"),
         QStringLiteral("水感、三秒成膜，戴口罩也不闷、不搓泥。"),
         QStringLiteral("现在它已经成了我通勤包里的固定位置。")},
        {QStringLiteral("三年前开始每天通勤，我的防晒换了不下十支。"),
         QStringLiteral("要么太油，要么搓泥，跟妆永远像在赌运气。"),
         QStringLiteral("后来同事推荐了这支，说油皮通勤很稳。"),
         QStringLiteral("我半信半疑用了一周，鼻翼居然没有再闷痘。"),
         QStringLiteral("现在回购第三次了，也推荐给同样通勤的你。")}};
    switch (style) {
    case MixcutStyle::Review:
        return review;
    case MixcutStyle::Story:
        return story;
    case MixcutStyle::Planting:
    default:
        return planting;
    }
}

QString generateMixcutScript(MixcutStyle style, int variant)
{
    const QVector<QStringList> &seeds = mixcutScriptSeeds(style);
    const int index = seeds.isEmpty() ? 0 : qAbs(variant) % seeds.size();
    return seeds.at(index).join(QLatin1Char('\n'));
}

namespace {

QString cleanPromptTitle(const QString &prompt)
{
    QString title = prompt;
    title.replace(QRegularExpression(QStringLiteral("\\s+")), QStringLiteral(" "));
    title.replace(QRegularExpression(QStringLiteral("[。！？!?\\s]+$")), QString());
    return title.trimmed();
}

bool looksLikeOriginalCopy(const QString &prompt)
{
    const QString trimmed = prompt.trimmed();
    QString withoutTrailing = trimmed;
    withoutTrailing.replace(QRegularExpression(QStringLiteral("[。！？!?\\s]+$")),
                            QString());
    const bool hasMultipleSentences =
        withoutTrailing.contains(QRegularExpression(QStringLiteral("[。！？!?\\n]")));
    const bool endsWithPeriod = trimmed.endsWith(QStringLiteral("。"))
        || trimmed.endsWith(QLatin1Char('.'));
    return trimmed.size() > 40 || hasMultipleSentences || endsWithPeriod;
}

const QVector<QStringList> &promptCtas(MixcutStyle style)
{
    static const QVector<QStringList> planting = {
        {QStringLiteral("想试试的姐妹，可以直接冲。"),
         QStringLiteral("油皮姐妹，可以先从这支试起。")}};
    static const QVector<QStringList> review = {
        {QStringLiteral("以上就是这次实测，数据都摆在这儿。"),
         QStringLiteral("综合看下来，这支值得通勤党试一试。")}};
    static const QVector<QStringList> story = {
        {QStringLiteral("这就是我和它之间的故事。"),
         QStringLiteral("希望我的经历，能给同样通勤的你一点参考。")}};
    switch (style) {
    case MixcutStyle::Review:
        return review;
    case MixcutStyle::Story:
        return story;
    case MixcutStyle::Planting:
    default:
        return planting;
    }
}

QString promptScript(const QString &title, MixcutStyle style, int variant)
{
    const QString cleanTitle = cleanPromptTitle(title).isEmpty()
        ? title.left(24)
        : cleanPromptTitle(title);

    static const QVector<QStringList> planting = {
        {QStringLiteral("%1？"),
         QStringLiteral("油皮通勤，先看质地和成膜速度。"),
         QStringLiteral("这支防晒是水感质地，推开三秒成膜，不搓泥也不泛白。"),
         QStringLiteral("轻轻一层就够，后续跟妆也很服帖。"),
         QStringLiteral("SPF50+ PA++++，从地铁到工位，一整天都稳稳的。"),
         QStringLiteral("想试试的姐妹，可以直接冲。")},
        {QStringLiteral("关于「%1」，一条视频说清楚。"),
         QStringLiteral("先说结论：通勤防晒，质地和成膜速度最重要。"),
         QStringLiteral("水感质地，推开三秒成膜，不搓泥、不泛白。"),
         QStringLiteral("一层就够，后续跟妆服帖，SPF50+ 也够用。"),
         QStringLiteral("油皮姐妹，这支真的可以试。")}};
    static const QVector<QStringList> review = {
        {QStringLiteral("今天实测：%1。"),
         QStringLiteral("重点看成膜速度、泛白程度和跟妆表现。"),
         QStringLiteral("上手是水感质地，推开三秒成膜，成膜后不黏手。"),
         QStringLiteral("上脸不泛白，油皮鼻翼两侧也没有明显搓泥。"),
         QStringLiteral("带妆八小时，只有轻微出油，底妆基本完整。"),
         QStringLiteral("以上就是这次实测，数据都摆在这儿。")},
        {QStringLiteral("%1，通勤八小时就能看出差别。"),
         QStringLiteral("我们测了成膜速度、泛白程度和跟妆表现。"),
         QStringLiteral("它在三秒内成膜，纸巾按压不粘，说明不搓泥。"),
         QStringLiteral("上脸是自然哑光感，没有假白和油光。"),
         QStringLiteral("如果你也是油皮通勤党，这支值得试。")}};
    static const QVector<QStringList> story = {
        {QStringLiteral("关于「%1」，我想讲讲自己的经历。"),
         QStringLiteral("以前总觉得，防晒和清爽不能同时拥有。"),
         QStringLiteral("直到通勤路上被太阳晒到泛红，我才认真找替代。"),
         QStringLiteral("这支是朋友递给我的，第一次用就被质地惊到。"),
         QStringLiteral("水感、三秒成膜，戴口罩也不闷、不搓泥。"),
         QStringLiteral("这就是我和它之间的故事。")},
        {QStringLiteral("%1，是我最近最想分享的一件事。"),
         QStringLiteral("三年来，我的通勤包里换过很多支防晒。"),
         QStringLiteral("要么太油，要么搓泥，跟妆永远像在赌运气。"),
         QStringLiteral("直到用上这支水感防晒，鼻翼才没有再闷痘。"),
         QStringLiteral("现在回购第三次了，也推荐给同样通勤的你。")}};

    const QVector<QStringList> &seeds = style == MixcutStyle::Review ? review
        : (style == MixcutStyle::Story ? story : planting);
    const QStringList &selected = seeds.at(qAbs(variant) % seeds.size());
    QStringList lines;
    for (const QString &line : selected) {
        lines.append(line.contains(QStringLiteral("%1")) ? line.arg(cleanTitle)
                                                          : line);
    }
    return lines.join(QLatin1Char('\n'));
}

} // namespace

QString generateMixcutScriptFromPrompt(const QString &prompt, MixcutStyle style,
                                       int variant)
{
    const QString clean = prompt.trimmed();
    if (clean.isEmpty()) {
        return generateMixcutScript(style, variant);
    }

    if (looksLikeOriginalCopy(clean)) {
        QStringList sentences = splitPromptSentences(clean);
        if (sentences.size() > 8) {
            sentences = sentences.mid(0, 8);
        }
        const QStringList &ctas = promptCtas(style).first();
        sentences.append(ctas.at(qAbs(variant) % ctas.size()));
        return sentences.join(QLatin1Char('\n'));
    }

    return promptScript(clean, style, variant);
}

QStringList mixcutScriptLines(const QString &script)
{
    QStringList lines;
    for (const QString &line : script.split(QLatin1Char('\n'))) {
        const QString trimmed = line.trimmed();
        if (!trimmed.isEmpty()) {
            lines.append(trimmed);
        }
    }
    return lines;
}

MixcutScriptStats mixcutScriptStats(const QString &script)
{
    MixcutScriptStats stats;
    stats.lines = mixcutScriptLines(script);
    QString compact = script;
    compact.remove(QRegularExpression(QStringLiteral("\\s")));
    stats.chars = compact.size();

    const QStringList sentences = script.split(
        QRegularExpression(QStringLiteral("[。！？!?\\n]")));
    for (const QString &sentence : sentences) {
        if (!sentence.trimmed().isEmpty()) {
            ++stats.sentences;
        }
    }

    stats.seconds = qMax(6, qRound(stats.chars / 4.2));
    stats.durationLabel = formatElapsed(static_cast<qint64>(stats.seconds) * 1000);
    return stats;
}

QString shortMixcutCaption(const QString &text, int maxLength)
{
    return shortOralCaption(text, maxLength);
}

/* ------------------------------------------------------------------ */
/* 智能混剪：示例原片                                                   */
/* ------------------------------------------------------------------ */

MixcutSource mixcutDemoSource()
{
    MixcutSource source;
    source.name = QStringLiteral("通勤防晒_原片.mp4");
    source.duration = QStringLiteral("01:32");
    source.resolution = QStringLiteral("3840×2160");
    source.size = QStringLiteral("268 MB");
    source.shots = 12;
    source.sample = true;
    source.scenes = {QStringLiteral("street"), QStringLiteral("serum"),
                     QStringLiteral("swatch"), QStringLiteral("face"),
                     QStringLiteral("desk"), QStringLiteral("sun"),
                     QStringLiteral("bag"), QStringLiteral("box")};
    source.topics = {QStringLiteral("通勤场景"), QStringLiteral("产品特写"),
                     QStringLiteral("上脸效果"), QStringLiteral("质地展示"),
                     QStringLiteral("户外光线")};
    return source;
}

MixcutProject defaultMixcutProject()
{
    MixcutProject project;
    project.source = mixcutDemoSource();
    return project;
}

MixcutSource mixcutSourceFromFile(const QString &fileName, qint64 sizeBytes)
{
    MixcutSource source = mixcutDemoSource();
    source.name = fileName;
    source.duration = QStringLiteral("01:28");
    source.resolution = QStringLiteral("1920×1080");
    source.size = uploadSize(sizeBytes);
    source.shots = 12;
    source.sample = false;
    source.topics = {QStringLiteral("已识别画面"), QStringLiteral("产品特写"),
                     QStringLiteral("通勤场景")};
    return source;
}

QString mixcutRatioDimensions(const QString &ratio)
{
    if (ratio == QLatin1String("16:9")) {
        return QStringLiteral("1920×1080");
    }
    if (ratio == QLatin1String("1:1")) {
        return QStringLiteral("1080×1080");
    }
    return QStringLiteral("1080×1920");
}

QString mixcutRatioHint(const QString &ratio)
{
    if (ratio == QLatin1String("16:9")) {
        return QStringLiteral("适合 B 站 / 横屏");
    }
    if (ratio == QLatin1String("1:1")) {
        return QStringLiteral("适合信息流");
    }
    return QStringLiteral("适合抖音 / 视频号");
}

QString mixcutFileSize(int seconds, const QString &ratio)
{
    const double rate = ratio == QLatin1String("16:9") ? 1.9
        : (ratio == QLatin1String("1:1") ? 1.55 : 1.45);
    return QStringLiteral("%1 MB").arg(seconds * rate, 0, 'f', 1);
}

QString mixcutSceneLabel(const QString &scene)
{
    if (scene == QLatin1String("street")) {
        return QStringLiteral("通勤街景");
    }
    if (scene == QLatin1String("serum")) {
        return QStringLiteral("产品特写");
    }
    if (scene == QLatin1String("face")) {
        return QStringLiteral("上脸效果");
    }
    if (scene == QLatin1String("swatch")) {
        return QStringLiteral("质地展示");
    }
    if (scene == QLatin1String("desk")) {
        return QStringLiteral("使用步骤");
    }
    if (scene == QLatin1String("sun")) {
        return QStringLiteral("户外光线");
    }
    if (scene == QLatin1String("bag")) {
        return QStringLiteral("开箱收纳");
    }
    return QStringLiteral("包装开箱");
}

QPair<QColor, QColor> mixcutScenePalette(const QString &scene)
{
    if (scene == QLatin1String("street")) {
        return {QColor(QStringLiteral("#1B3550")), QColor(QStringLiteral("#0B1622"))};
    }
    if (scene == QLatin1String("serum")) {
        return {QColor(QStringLiteral("#3B2A1A")), QColor(QStringLiteral("#140F0B"))};
    }
    if (scene == QLatin1String("face")) {
        return {QColor(QStringLiteral("#38242A")), QColor(QStringLiteral("#150F14"))};
    }
    if (scene == QLatin1String("swatch")) {
        return {QColor(QStringLiteral("#4A342B")), QColor(QStringLiteral("#1B1310"))};
    }
    if (scene == QLatin1String("desk")) {
        return {QColor(QStringLiteral("#2E2A46")), QColor(QStringLiteral("#151322"))};
    }
    if (scene == QLatin1String("sun")) {
        return {QColor(QStringLiteral("#4A3418")), QColor(QStringLiteral("#1A1209"))};
    }
    if (scene == QLatin1String("bag")) {
        return {QColor(QStringLiteral("#3B2B23")), QColor(QStringLiteral("#14100D"))};
    }
    return {QColor(QStringLiteral("#3A2E1D")), QColor(QStringLiteral("#15110B"))};
}

QColor mixcutSceneAccent(const QString &scene)
{
    if (scene == QLatin1String("street")) {
        return QColor(QStringLiteral("#FFC177"));
    }
    if (scene == QLatin1String("serum")) {
        return QColor(QStringLiteral("#FFB224"));
    }
    if (scene == QLatin1String("face")) {
        return QColor(QStringLiteral("#F3C9B2"));
    }
    if (scene == QLatin1String("swatch")) {
        return QColor(QStringLiteral("#F1D5C6"));
    }
    if (scene == QLatin1String("desk")) {
        return QColor(QStringLiteral("#C7A1C9"));
    }
    if (scene == QLatin1String("sun")) {
        return QColor(QStringLiteral("#FFD9A3"));
    }
    if (scene == QLatin1String("bag")) {
        return QColor(QStringLiteral("#E8C9A8"));
    }
    return QColor(QStringLiteral("#EFD8B2"));
}

/* ------------------------------------------------------------------ */
/* 智能混剪：生成过程                                                   */
/* ------------------------------------------------------------------ */

qint64 mixcutRunTotalMs(const MixcutProject &project)
{
    const MixcutContext ctx = makeMixcutContext(project);
    qint64 total = 0;
    for (const MixcutStageSeed &seed : mixcutStageSeeds(ctx)) {
        total += seed.durationMs;
    }
    return total;
}

MixcutRun computeMixcutRun(const MixcutProject &project, qint64 elapsedMs, int runId)
{
    const MixcutContext ctx = makeMixcutContext(project);
    const QVector<MixcutStageSeed> seeds = mixcutStageSeeds(ctx);
    qint64 totalMs = 0;
    for (const MixcutStageSeed &seed : seeds) {
        totalMs += seed.durationMs;
    }
    const qint64 clamped = qBound<qint64>(0, elapsedMs, totalMs);
    const bool done = clamped >= totalMs;

    MixcutRun run;
    run.runId = runId;
    run.done = done;
    run.elapsedMs = clamped;
    run.totalMs = totalMs;
    run.progress = totalMs > 0 ? (static_cast<double>(clamped) / totalMs) * 100.0 : 0.0;
    run.etaSeconds = done ? 0 : static_cast<int>(std::ceil((totalMs - clamped) / 1000.0));
    run.stage = MixcutStage::Render;
    run.totalFrames = ctx.frames;
    run.durationLabel = ctx.stats.durationLabel;

    QHash<int, qint64> starts;
    QHash<int, qint64> ends;
    qint64 offset = 0;
    for (const MixcutStageSeed &seed : seeds) {
        const qint64 start = offset;
        const qint64 end = start + seed.durationMs;
        offset = end;
        starts.insert(static_cast<int>(seed.key), start);
        ends.insert(static_cast<int>(seed.key), end);

        MixcutStageState stage;
        stage.key = seed.key;
        stage.label = seed.label;
        if (clamped >= end) {
            stage.status = StageStatus::Done;
        } else if (clamped >= start) {
            stage.status = StageStatus::Running;
        } else {
            stage.status = StageStatus::Waiting;
        }
        stage.progress = clamped >= end
            ? 100.0
            : (clamped <= start
                   ? 0.0
                   : (static_cast<double>(clamped - start) / seed.durationMs) * 100.0);

        QString lastLog;
        for (int index = 0; index < seed.logs.size(); ++index) {
            const MixcutLogSeed &log = seed.logs.at(index);
            if (clamped < start + log.atMs) {
                continue;
            }
            MixcutLogEntry entry;
            entry.id = QStringLiteral("%1-%2").arg(static_cast<int>(seed.key)).arg(index);
            entry.stage = seed.key;
            entry.atMs = start + log.atMs;
            entry.text = log.text;
            entry.tone = log.tone;
            run.logs.append(entry);
            lastLog = log.text;
        }

        if (stage.status == StageStatus::Done) {
            stage.detail = seed.doneDetail;
        } else if (stage.status == StageStatus::Running) {
            stage.detail = lastLog.isEmpty() ? QStringLiteral("正在准备…") : lastLog;
            run.stage = seed.key;
        } else {
            stage.detail = QStringLiteral("等待上一步完成");
        }
        run.stages.append(stage);
    }

    const qint64 parseEnd = ends.value(static_cast<int>(MixcutStage::Parse));
    const qint64 voiceStart = starts.value(static_cast<int>(MixcutStage::Voice));
    const qint64 voiceEnd = ends.value(static_cast<int>(MixcutStage::Voice));
    const qint64 matchStart = starts.value(static_cast<int>(MixcutStage::Match));
    const qint64 matchEnd = ends.value(static_cast<int>(MixcutStage::Match));
    const qint64 designStart = starts.value(static_cast<int>(MixcutStage::Design));
    const qint64 designEnd = ends.value(static_cast<int>(MixcutStage::Design));
    const qint64 renderStart = starts.value(static_cast<int>(MixcutStage::Render));
    const qint64 renderEnd = ends.value(static_cast<int>(MixcutStage::Render));

    if (clamped < parseEnd) {
        run.previewCaption = QStringLiteral("AI 正在拆解文案与画面节奏…");
    } else if (!ctx.captions.isEmpty()) {
        const double ratio = stageRatio(clamped, parseEnd, totalMs);
        const int index = qBound(0, static_cast<int>(std::floor(ratio * ctx.captions.size())),
                                 ctx.captions.size() - 1);
        run.previewCaption = ctx.captions.at(index);
    } else {
        run.previewCaption = QStringLiteral("正在生成成片…");
    }

    const int keywordCount = qMin(ctx.keywords.size(),
        qRound(stageRatio(clamped, 0, parseEnd) * ctx.keywords.size()));
    run.previewKeywords = ctx.keywords.mid(0, keywordCount);
    run.voiceProgress = stageRatio(clamped, voiceStart, voiceEnd) * 100.0;
    run.matchedShots = qRound(stageRatio(clamped, matchStart, matchEnd) * ctx.source.shots);
    run.subtitleCount = qRound(stageRatio(clamped, designStart, designEnd)
                               * ctx.subtitleCount);
    run.sfxCount = qRound(stageRatio(clamped, designStart, designEnd) * ctx.sfxCount);
    run.renderedFrames = qRound(stageRatio(clamped, renderStart, renderEnd) * ctx.frames);
    return run;
}

/* ------------------------------------------------------------------ */
/* 智能混剪：结果                                                       */
/* ------------------------------------------------------------------ */

MixcutResult makeMixcutResult(const MixcutProject &project, qint64 generatedAt,
                              const QString &scriptOverride)
{
    const MixcutSource source = project.hasSource ? project.source : mixcutDemoSource();
    const MixcutStyleMeta &meta = mixcutStyleMeta(project.style);
    const MixcutVoice voice = getMixcutVoice(project.voiceId);
    const QString script = !scriptOverride.trimmed().isEmpty()
        ? scriptOverride
        : (!project.script.trimmed().isEmpty()
               ? project.script
               : generateMixcutScriptFromPrompt(project.prompt, project.style, 0));
    const MixcutScriptStats stats = mixcutScriptStats(script);
    const int subtitleCount = qMax(8, qRound(stats.chars / 7.0));
    const int sfxCount = qBound(3, qRound(stats.seconds / 5.0), 12);
    const int cuts = qBound(4, qRound(stats.seconds / 3.0), source.shots);

    MixcutResult result;
    result.id = QStringLiteral("mixcut-%1").arg(generatedAt);
    result.projectName = meta.projectName + QStringLiteral(" · 智能混剪");
    result.title = meta.title;
    result.description = meta.description;
    result.tags = meta.tags;
    result.captionTitle = meta.captionTitle;
    result.voiceId = project.voiceId;
    result.style = project.style;
    result.ratio = project.ratio;
    result.durationLabel = stats.durationLabel;
    result.fileSize = mixcutFileSize(stats.seconds, project.ratio);
    result.generatedAt = generatedAt;
    result.sourceLabel = QStringLiteral("原片 · %1 · %2 个镜头")
                             .arg(source.duration)
                             .arg(source.shots);
    result.scriptPrompt = project.prompt;
    result.script = script;
    result.sceneCount = source.shots;
    result.subtitleCount = subtitleCount;
    result.sfxCount = sfxCount;
    result.bgm = meta.bgm;
    result.draftGenerated = true;
    result.highlights = {
        {QStringLiteral("配音音色"),
         QStringLiteral("%1 · %2").arg(voice.name, voice.label)},
        {QStringLiteral("镜头匹配"),
         QStringLiteral("%1 个 · %2 处切换").arg(source.shots).arg(cuts)},
        {QStringLiteral("智能字幕"),
         QStringLiteral("%1 条 · 关键词高亮").arg(subtitleCount)},
        {QStringLiteral("卡点音效"), QStringLiteral("%1 处").arg(sfxCount)},
    };
    return result;
}

QVector<MixcutResult> recentMixcutResults()
{
    const qint64 now = QDateTime::currentMSecsSinceEpoch();
    const qint64 hour = 60 * 60 * 1000;
    const qint64 day = 24 * hour;

    struct Seed {
        MixcutStyle style;
        const char *voice;
        const char *ratio;
        qint64 offsetMs;
        const char *prompt;
    };
    const Seed seeds[] = {
        {MixcutStyle::Planting, "linlan", "9:16", 2 * hour,
         "夏天通勤防晒怎么选"},
        {MixcutStyle::Review, "chenyu", "16:9", 25 * hour,
         "通勤八小时防晒表现"},
        {MixcutStyle::Story, "anan", "9:16", 3 * day,
         "我的通勤防晒故事"},
    };

    QVector<MixcutResult> results;
    for (const Seed &seed : seeds) {
        MixcutProject project;
        project.hasSource = true;
        project.source = mixcutDemoSource();
        project.analysisReady = true;
        project.prompt = QString::fromUtf8(seed.prompt);
        project.script = generateMixcutScriptFromPrompt(project.prompt, seed.style, 0);
        project.style = seed.style;
        project.voiceId = QString::fromLatin1(seed.voice);
        project.ratio = QString::fromLatin1(seed.ratio);
        project.voiceStatus = VoiceStatus::Ready;
        results.append(makeMixcutResult(project, now - seed.offsetMs));
    }
    return results;
}

} // namespace StudioData
