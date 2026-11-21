#include "SpecPage.h"

#include "../core/Brand.h"
#include "../ui/Controls.h"

#include <QDesktopServices>
#include <QGridLayout>
#include <QHBoxLayout>
#include <QLabel>
#include <QPainter>
#include <QPainterPath>
#include <QUrl>
#include <QVBoxLayout>

namespace {

QLabel *wrappedText(const QString &html, bool rich, int pixelSize, const QColor &color,
                    int width, QWidget *parent)
{
    auto *label = rich ? Ui::richLabel(html, pixelSize, color, parent)
                       : Ui::label(html, pixelSize, 400, color, parent);
    label->setWordWrap(true);
    label->setFixedWidth(width);
    label->setAlignment(Qt::AlignLeft | Qt::AlignTop);
    const int height = label->heightForWidth(width);
    if (height > 0) {
        label->setMinimumHeight(height);
    }
    return label;
}

QLabel *paragraph(const QString &html, QWidget *parent, int pixelSize = 13)
{
    return wrappedText(html, true, pixelSize, Theme::Text2, 820, parent);
}

QWidget *sectionHeading(const QString &title, QWidget *parent)
{
    auto *row = new QWidget(parent);
    auto *layout = new QHBoxLayout(row);
    layout->setContentsMargins(0, 0, 0, 10);
    layout->setSpacing(8);
    auto *bar = new QWidget(row);
    bar->setFixedSize(3, 14);
    bar->setStyleSheet(QStringLiteral("background:#6E8BFF;border-radius:2px;"));
    layout->addWidget(bar);
    layout->addWidget(Ui::label(title, 14, 650, Theme::Text1, row));
    layout->addStretch(1);
    return row;
}

QWidget *makeSection(Page *page, const QString &title)
{
    auto *section = new QWidget(page);
    auto *layout = new QVBoxLayout(section);
    layout->setContentsMargins(0, 0, 0, 0);
    layout->setSpacing(0);
    layout->addWidget(sectionHeading(title, section));
    page->bodyLayout()->addWidget(section);
    return section;
}

class SwatchCard : public QWidget
{
public:
    SwatchCard(const QColor &color, const QString &name, const QString &hex,
               QWidget *parent = nullptr)
        : QWidget(parent)
        , m_color(color)
        , m_name(name)
        , m_hex(hex)
    {
        setMinimumHeight(99);
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        const QRectF outer = QRectF(rect()).adjusted(0.5, 0.5, -0.5, -0.5);
        QPainterPath outline;
        outline.addRoundedRect(outer, 7, 7);
        painter.setBrush(Theme::Surface1);
        painter.setPen(QPen(Theme::Border, 1.0));
        painter.drawPath(outline);

        painter.save();
        QPainterPath clip;
        clip.setFillRule(Qt::WindingFill);
        clip.moveTo(outer.left(), 56);
        clip.lineTo(outer.right(), 56);
        clip.lineTo(outer.right(), outer.top() + 7);
        clip.quadTo(outer.topRight(), QPointF(outer.right() - 7, outer.top()));
        clip.lineTo(outer.left() + 7, outer.top());
        clip.quadTo(outer.topLeft(), QPointF(outer.left(), outer.top() + 7));
        clip.closeSubpath();
        painter.setClipPath(clip);
        painter.fillRect(QRectF(outer.left(), outer.top(), outer.width(), 56), m_color);
        painter.restore();

        painter.setFont(Theme::font(11, 500));
        painter.setPen(Theme::Text1);
        painter.drawText(QRectF(8, 62, width() - 16, 17),
                         Qt::AlignLeft | Qt::AlignVCenter, m_name);
        painter.setFont(Theme::monoFont(10));
        painter.setPen(Theme::Text3);
        painter.drawText(QRectF(8, 79, width() - 16, 15),
                         Qt::AlignLeft | Qt::AlignVCenter, m_hex);
    }

private:
    QColor m_color;
    QString m_name;
    QString m_hex;
};

Card *makePlatformCard(const QString &title, bool current,
                       const QVector<QPair<QString, QString>> &rows, QWidget *parent)
{
    auto *card = new Card(Theme::Surface1,
                          current ? Theme::alpha(Theme::Primary, 0.45) : Theme::Border, 8,
                          parent);
    auto *layout = card->contentLayout();
    layout->setContentsMargins(14, 12, 14, 12);
    layout->setSpacing(7);

    auto *head = new QWidget(card);
    auto *headLayout = new QHBoxLayout(head);
    headLayout->setContentsMargins(0, 0, 0, 2);
    headLayout->setSpacing(6);
    headLayout->addWidget(Ui::label(title, 12, 650, Theme::Text1, head));
    if (current) {
        auto *badge = Ui::label(QStringLiteral("当前"), 10, 500, Theme::Primary, head);
        badge->setStyleSheet(QStringLiteral(
            "background:rgba(110,139,255,40);border-radius:7px;padding:1px 6px;"));
        headLayout->addWidget(badge);
    }
    headLayout->addStretch(1);
    layout->addWidget(head);

    for (const auto &row : rows) {
        const QString html =
            QStringLiteral("<span style=\"color:%1\">%2</span>&nbsp;&nbsp;%3")
                .arg(Theme::Text3.name(), row.first, row.second);
        auto *label = Ui::richLabel(html, 11, Theme::Text2, card);
        label->setWordWrap(true);
        label->setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Minimum);
        layout->addWidget(label);
    }
    layout->addStretch(1);
    return card;
}

Card *makePrinciple(const QString &title, const QString &body, const QColor &accent,
                    QWidget *parent)
{
    auto *card = new Card(Theme::Surface1, Theme::Border, 8, parent);
    auto *layout = card->contentLayout();
    layout->setContentsMargins(14, 14, 14, 14);
    layout->setSpacing(5);
    layout->addWidget(Ui::label(title, 13, 600, accent, card));
    auto *text = wrappedText(body, false, 12, Theme::Text2, 350, card);
    layout->addWidget(text);
    layout->addStretch(1);
    return card;
}

QWidget *typeRow(const QString &label, const QString &sample, int pixelSize, int weight,
                 bool mono, const QString &description, bool last, QWidget *parent)
{
    auto *row = new QWidget(parent);
    auto *rowLayout = new QVBoxLayout(row);
    rowLayout->setContentsMargins(0, 0, 0, 0);
    rowLayout->setSpacing(0);
    auto *content = new QWidget(row);
    auto *layout = new QHBoxLayout(content);
    layout->setContentsMargins(14, 10, 14, 10);
    layout->setSpacing(12);
    auto *name = Ui::label(label, 11, 400, Theme::Text3, content);
    name->setFont(Theme::monoFont(11));
    name->setFixedWidth(90);
    layout->addWidget(name);
    auto *sampleLabel = Ui::label(sample, pixelSize, weight, Theme::Text1, content);
    if (mono) {
        sampleLabel->setFont(Theme::monoFont(pixelSize, weight));
    }
    layout->addWidget(sampleLabel, 1);
    auto *desc = Ui::label(description, 11, 400, Theme::Text3, content);
    desc->setFont(Theme::monoFont(11));
    desc->setFixedWidth(120);
    layout->addWidget(desc);
    rowLayout->addWidget(content);
    if (!last) {
        rowLayout->addWidget(Ui::separator(false, row));
    }
    return row;
}

QLabel *kbd(const QString &text, QWidget *parent)
{
    auto *chip = new QLabel(text, parent);
    chip->setFont(Theme::monoFont(11));
    chip->setStyleSheet(QStringLiteral(
        "color:#E9ECF4;background:#222734;border:1px solid #343B4E;"
        "border-bottom-width:2px;border-radius:4px;padding:1px 6px;"));
    chip->setSizePolicy(QSizePolicy::Fixed, QSizePolicy::Fixed);
    return chip;
}

QWidget *shortcutRow(const QVector<QString> &keys, const QString &action,
                     const QString &description, bool plusBetween, bool last,
                     QWidget *parent)
{
    auto *row = new QWidget(parent);
    auto *root = new QVBoxLayout(row);
    root->setContentsMargins(0, 0, 0, 0);
    root->setSpacing(0);
    auto *content = new QWidget(row);
    auto *layout = new QHBoxLayout(content);
    layout->setContentsMargins(10, 7, 10, 7);
    layout->setSpacing(7);

    auto *left = new QWidget(content);
    left->setFixedWidth(340);
    auto *leftLayout = new QHBoxLayout(left);
    leftLayout->setContentsMargins(0, 0, 0, 0);
    leftLayout->setSpacing(4);
    for (int i = 0; i < keys.size(); ++i) {
        if (plusBetween && i > 0) {
            leftLayout->addWidget(Ui::label(QStringLiteral("+"), 12, 400, Theme::Text3, left));
        }
        leftLayout->addWidget(kbd(keys[i], left));
    }
    leftLayout->addWidget(Ui::label(action, 12, 400, Theme::Text1, left));
    leftLayout->addStretch(1);
    layout->addWidget(left);
    layout->addWidget(
        wrappedText(description, false, 12, Theme::Text2, 388, content), 1);
    root->addWidget(content);
    if (!last) {
        root->addWidget(Ui::separator(false, row));
    }
    return row;
}

QWidget *differenceRow(const QString &title, const QString &description, bool last,
                       QWidget *parent)
{
    auto *row = new QWidget(parent);
    auto *root = new QVBoxLayout(row);
    root->setContentsMargins(0, 0, 0, 0);
    root->setSpacing(0);
    auto *content = new QWidget(row);
    auto *layout = new QHBoxLayout(content);
    layout->setContentsMargins(10, 7, 10, 7);
    layout->setSpacing(12);
    auto *left = Ui::label(title, 12, 400, Theme::Text1, content);
    left->setFixedWidth(340);
    layout->addWidget(left);
    auto *right = wrappedText(description, false, 12, Theme::Text2, 388, content);
    layout->addWidget(right, 1);
    root->addWidget(content);
    if (!last) {
        root->addWidget(Ui::separator(false, row));
    }
    return row;
}

} // namespace

SpecPage::SpecPage(QWidget *parent)
    : Page(parent)
{
    auto *header = new PageHeader(
        QStringLiteral("交互与视觉设计说明"),
        QStringLiteral("把 Web 版「后台表格」重构为桌面端「生产工作台」的完整设计决策。"),
        this);
    bodyLayout()->addWidget(header);

    // Design positioning
    auto *positioning = makeSection(this, QStringLiteral("设计定位"));
    auto *positionLayout = qobject_cast<QVBoxLayout *>(positioning->layout());
    positionLayout->addWidget(paragraph(
        QStringLiteral(
            "原 Web 版是「数据管理后台」思维：长表格 + 弹窗表单。桌面客户端的核心用户是"
            "剪辑师与直播运营，他们的主工作流是 "
            "<b style=\"color:#E9ECF4\">听稿 → 选段 → 修剪 → 导出</b>，因此客户端把"
            "「切片工作区」作为一等公民——播放窗口、ASR 转写稿、切片列表、时间线四栏同屏，"
            "不再需要在列表页和详情页之间反复跳转。"),
        positioning));
    positionLayout->addSpacing(10);
    auto *principles = new QWidget(positioning);
    auto *principleLayout = new QHBoxLayout(principles);
    principleLayout->setContentsMargins(0, 0, 0, 0);
    principleLayout->setSpacing(12);
    principleLayout->addWidget(makePrinciple(
        QStringLiteral("深色专业工具风"),
        QStringLiteral("视频预览在深色背景下观感最准确，长时间选段不刺眼；"
                       "对标 DaVinci Resolve / Descript 的领域惯例。"),
        Theme::Primary, principles), 1);
    principleLayout->addWidget(makePrinciple(
        QStringLiteral("AI 能力视觉锚点"),
        QStringLiteral("所有 AI 动作（智能切片、AI 粗选、置信度）统一使用琥珀色，"
                       "与常规蓝色主操作区分，让用户一眼识别“这是模型在工作”。"),
        Theme::Ai, principles), 1);
    principleLayout->addWidget(makePrinciple(
        QStringLiteral("两段式切片语义"),
        QStringLiteral("蓝色块 = AI 粗选 clips0（候选），绿色块 = 人工精修 clips1（成片）。"
                       "时间线上颜色即状态，无需读字。"),
        Theme::Success, principles), 1);
    positionLayout->addWidget(principles);
    bodyLayout()->addSpacing(26);

    // Colors
    auto *colors = makeSection(this, QStringLiteral("色彩系统"));
    auto *colorsLayout = qobject_cast<QVBoxLayout *>(colors->layout());
    auto *swatchGrid = new QWidget(colors);
    auto *grid = new QGridLayout(swatchGrid);
    grid->setContentsMargins(0, 0, 0, 0);
    grid->setHorizontalSpacing(10);
    grid->setVerticalSpacing(10);
    struct Swatch {
        const char *color;
        const char *name;
    };
    const Swatch swatches[] = {
        {"#101218", "窗口底"}, {"#1A1E28", "面板/卡片"}, {"#6E8BFF", "主操作蓝"},
        {"#FFB224", "AI 琥珀"}, {"#3ECF8E", "完成/精修绿"}, {"#F6465D", "错误/删除红"},
    };
    for (int i = 0; i < 6; ++i) {
        const QColor color(QString::fromLatin1(swatches[i].color));
        grid->addWidget(new SwatchCard(color, QString::fromUtf8(swatches[i].name),
                                       QString::fromLatin1(swatches[i].color), swatchGrid),
                        i / 6, i % 6);
        grid->setColumnStretch(i % 6, 1);
    }
    colorsLayout->addWidget(swatchGrid);
    bodyLayout()->addSpacing(26);

    // Typography
    auto *typography = makeSection(this, QStringLiteral("字体排印"));
    auto *typeLayout = qobject_cast<QVBoxLayout *>(typography->layout());
    auto *typeCard = new Card(Qt::transparent, Theme::Border, 8, typography);
    auto *typeRows = typeCard->contentLayout();
    typeRows->setContentsMargins(0, 0, 0, 0);
    typeRows->setSpacing(0);
    typeRows->addWidget(typeRow(QStringLiteral("标题 17/650"),
                                QStringLiteral("直播切片工作台"), 17, 650, false,
                                QStringLiteral("页面标题"), false, typeCard));
    typeRows->addWidget(typeRow(QStringLiteral("正文 12.5/500"),
                                QStringLiteral("双击转写句加入精修，拖拽边缘修剪时长"), 13, 500,
                                false, QStringLiteral("列表/转写稿"), false, typeCard));
    typeRows->addWidget(typeRow(QStringLiteral("辅助 11/400"),
                                QStringLiteral("ASR 转写完成 · 共 1,204 句"), 11, 400, false,
                                QStringLiteral("说明文字"), false, typeCard));
    typeRows->addWidget(typeRow(QStringLiteral("等宽 11/mono"),
                                QStringLiteral("00:12:18 / 02:54:06 · clips0:13 clips1:6"), 12,
                                400, true, QStringLiteral("时间码/计数"), true, typeCard));
    typeLayout->addWidget(typeCard);
    typeLayout->addSpacing(10);
    auto *typeNote = paragraph(
        QStringLiteral("时间码、进度、计数一律使用等宽字体（SF Mono / Consolas），"
                       "数字跳动时不抖动；中文正文用系统字体栈保证 Win/Mac 原生渲染。"),
        typography);
    typeLayout->addWidget(typeNote);
    bodyLayout()->addSpacing(26);

    // Flow
    auto *flow = makeSection(this, QStringLiteral("核心交互流程"));
    auto *flowLayout = qobject_cast<QVBoxLayout *>(flow->layout());
    flowLayout->addWidget(paragraph(
        QStringLiteral(
            "<b style=\"color:#E9ECF4\">主路径：</b> 录制直播（或导入本地视频）→ "
            "视频库中点「一键成片」→ 选择提示词与成片数量（自动，或按素材时长"
            "指定条数；60 分钟素材可指定 2–12 条）→ "
            "ASR 转写 → AI 按提示词选出句段并合并成成片 → 生成剪映草稿 → "
            "在结果页标题旁与每张卡片上查看生成时间，并可点「导出到剪映」将草稿与素材"
            "下载到本地剪映目录，或点「导出成片」云端合成 MP4 并下载到本地；"
            "也可批量选择一次导出多条。"),
        flow));
    flowLayout->addSpacing(10);
    flowLayout->addWidget(paragraph(
        QStringLiteral(
            "<b style=\"color:#E9ECF4\">设计要点：</b> ① 转写稿每句带时间码，点击即跳转"
            "播放头，选中句高亮并自动划入当前 clip；② 时间线块可直接拖拽边缘修剪 in/out；"
            "③ 异步任务不阻塞界面，完成时系统通知 + 底部状态栏计数 +1；"
            "④ 窗口可关闭，任务在后台继续跑，重开后从「任务中心」恢复进度。"),
        flow));
    bodyLayout()->addSpacing(26);

    // Generation time
    auto *generation = makeSection(this, QStringLiteral("生成时间格式"));
    auto *generationLayout = qobject_cast<QVBoxLayout *>(generation->layout());
    generationLayout->addWidget(paragraph(
        QStringLiteral(
            "结果页标题与每张成片卡片使用同一套时间表达：每张卡片显示该草稿自己的"
            "生成时间，标题旁显示最近一条的生成时间；精确到秒，越近越详细，"
            "越久越简短。悬停任意时间可查看完整日期与时间；相对标签每 10 秒刷新，"
            "窗口跨天也不会残留昨天的文案。"),
        generation));
    generationLayout->addSpacing(10);

    auto *generationTable =
        new Card(Qt::transparent, Theme::Border, 8, generation);
    generationTable->setFixedWidth(820);
    auto *tableRows = generationTable->contentLayout();
    tableRows->setContentsMargins(0, 0, 0, 0);
    tableRows->setSpacing(0);

    auto *tableHeader = new QWidget(generationTable);
    tableHeader->setObjectName(QStringLiteral("generationTableHeader"));
    tableHeader->setAttribute(Qt::WA_StyledBackground, true);
    tableHeader->setStyleSheet(
        QStringLiteral("#generationTableHeader{background:%1;"
                       "border-top-left-radius:7px;border-top-right-radius:7px;}")
            .arg(Theme::Surface2.name()));
    auto *headerLayout = new QGridLayout(tableHeader);
    headerLayout->setContentsMargins(14, 8, 14, 8);
    headerLayout->setHorizontalSpacing(12);
    headerLayout->setColumnMinimumWidth(0, 120);
    headerLayout->setColumnMinimumWidth(1, 190);
    headerLayout->setColumnStretch(2, 1);
    headerLayout->addWidget(
        Ui::label(QStringLiteral("场景"), 10, 400, Theme::Text3, tableHeader), 0, 0);
    headerLayout->addWidget(
        Ui::label(QStringLiteral("展示"), 10, 400, Theme::Text3, tableHeader), 0, 1);
    headerLayout->addWidget(
        Ui::label(QStringLiteral("规则"), 10, 400, Theme::Text3, tableHeader), 0, 2);
    tableRows->addWidget(tableHeader);
    tableRows->addWidget(Ui::separator(false));

    struct GenerationExample {
        const char *scene;
        const char *value;
        const char *rule;
    };
    const GenerationExample generationExamples[] = {
        {"刚刚完成", "刚刚 · 14:32:05", "1 分钟内 · 绿色强调最新结果"},
        {"当天", "今天 14:32:05", "按日历日判断，跨天自动切换"},
        {"昨天", "昨天 09:15:20", "1 天前"},
        {"前天", "前天 18:40:05", "2 天前"},
        {"一周内", "4 天前 11:20:30", "3–6 天前保留相对天数"},
        {"一周前 / 一月前", "10月22日 15:09:50", "7 天前至今年内，直接显示日期"},
        {"跨年", "2025年12月3日 08:20:45", "非今年时补全年份"},
    };
    const int exampleCount =
        int(sizeof(generationExamples) / sizeof(generationExamples[0]));
    for (int i = 0; i < exampleCount; ++i) {
        const GenerationExample &example = generationExamples[i];
        auto *row = new QWidget(generationTable);
        auto *rowLayout = new QGridLayout(row);
        rowLayout->setContentsMargins(14, 8, 14, 8);
        rowLayout->setHorizontalSpacing(12);
        rowLayout->setColumnMinimumWidth(0, 120);
        rowLayout->setColumnMinimumWidth(1, 190);
        rowLayout->setColumnStretch(2, 1);
        rowLayout->addWidget(
            Ui::label(QString::fromUtf8(example.scene), 11, 400, Theme::Text3, row),
            0, 0);
        const QString value = QString::fromUtf8(example.value);
        auto *valueLabel = Ui::label(
            value, 11, 400,
            value.startsWith(QStringLiteral("刚刚")) ? Theme::Success : Theme::Text1,
            row);
        valueLabel->setFont(Theme::monoFont(11));
        rowLayout->addWidget(valueLabel, 0, 1);
        rowLayout->addWidget(
            Ui::label(QString::fromUtf8(example.rule), 11, 400, Theme::Text3, row),
            0, 2);
        tableRows->addWidget(row);
        if (i < exampleCount - 1) {
            tableRows->addWidget(Ui::separator(false));
        }
    }
    generationLayout->addWidget(generationTable);
    generationLayout->addSpacing(10);
    generationLayout->addWidget(
        wrappedText(QStringLiteral("每条草稿保存记录时写入自己的生成时间：卡片各自显示"
                                   "对应时间，页头显示最近一条；新草稿完成后对应卡片"
                                   "为「刚刚 · HH:mm:ss」，随后自然过渡到"
                                   "「今天 HH:mm:ss」。"),
                    false, 11, Theme::Text3, 820, generation));
    bodyLayout()->addSpacing(26);

    // Keyboard shortcuts
    auto *shortcuts = makeSection(this, QStringLiteral("桌面端快捷键"));
    auto *shortcutLayout = qobject_cast<QVBoxLayout *>(shortcuts->layout());
    auto *shortcutTable = new QWidget(shortcuts);
    shortcutTable->setFixedWidth(760);
    auto *shortcutRows = new QVBoxLayout(shortcutTable);
    shortcutRows->setContentsMargins(0, 0, 0, 0);
    shortcutRows->setSpacing(0);
    const QString mod = Theme::modifierKey();
    const QString enterKey =
        Theme::isWindowsWindowStyle() ? QStringLiteral("Enter") : QStringLiteral("↵");
    shortcutRows->addWidget(shortcutRow({QStringLiteral("Space")},
                                        QStringLiteral("播放 / 暂停"),
                                        QStringLiteral("视频预览控制"), false, false,
                                        shortcutTable));
    shortcutRows->addWidget(shortcutRow(
        {QStringLiteral("J"), QStringLiteral("K"), QStringLiteral("L")},
        QStringLiteral("减速 / 暂停 / 加速"), QStringLiteral("听写式逐句回听"), false,
        false, shortcutTable));
    shortcutRows->addWidget(shortcutRow(
        {QStringLiteral("I"), QStringLiteral("O")}, QStringLiteral("设置入点 / 出点"),
        QStringLiteral("精修 clip 边界"), false, false, shortcutTable));
    shortcutRows->addWidget(shortcutRow(
        {mod, enterKey}, QStringLiteral("当前句加入精修 clips1"),
        QStringLiteral("核心选段动作"), true, false, shortcutTable));
    shortcutRows->addWidget(shortcutRow(
        {mod, QStringLiteral("D")}, QStringLiteral("当前句从精修移除"),
        QStringLiteral("撤回误选"), true, false, shortcutTable));
    shortcutRows->addWidget(shortcutRow(
        {mod, QStringLiteral("N")}, QStringLiteral("新建源视频"),
        QStringLiteral("快速录入素材"), true, false, shortcutTable));
    shortcutRows->addWidget(shortcutRow(
        {mod, QStringLiteral("K")}, QStringLiteral("全局搜索（素材 / 项目 / 切片）"),
        QStringLiteral("快速跳转"), true, false, shortcutTable));
    shortcutRows->addWidget(shortcutRow(
        {mod, enterKey}, QStringLiteral("一键成片"),
        QStringLiteral("启动四步流水线"), true, true, shortcutTable));
    shortcutLayout->addWidget(shortcutTable);

    const bool windowsShell = Theme::isWindowsWindowStyle();
    shortcutLayout->addSpacing(14);
    shortcutLayout->addWidget(Ui::label(
        QStringLiteral("窗口操作 · %1")
            .arg(windowsShell ? QStringLiteral("Windows 11") : QStringLiteral("macOS")),
        11, 400, Theme::Text3, shortcuts));
    shortcutLayout->addSpacing(4);
    auto *windowTable = new QWidget(shortcuts);
    windowTable->setFixedWidth(760);
    auto *windowRows = new QVBoxLayout(windowTable);
    windowRows->setContentsMargins(0, 0, 0, 0);
    windowRows->setSpacing(0);
    if (windowsShell) {
        windowRows->addWidget(shortcutRow(
            {QStringLiteral("双击")}, QStringLiteral("最大化 / 向下还原"),
            QStringLiteral("标题栏空白区域"), false, false, windowTable));
        windowRows->addWidget(shortcutRow(
            {QStringLiteral("Alt"), QStringLiteral("Space")},
            QStringLiteral("打开窗口系统菜单"),
            QStringLiteral("还原 / 移动 / 大小 / 最小化 / 最大化 / 关闭"), true, false,
            windowTable));
        windowRows->addWidget(shortcutRow(
            {QStringLiteral("Alt"), QStringLiteral("F4")}, QStringLiteral("关闭窗口"),
            QStringLiteral("驻留系统托盘，后台任务继续"), true, true, windowTable));
    } else {
        windowRows->addWidget(shortcutRow(
            {mod, QStringLiteral("W")}, QStringLiteral("关闭窗口"),
            QStringLiteral("驻留菜单栏，后台任务继续"), true, true, windowTable));
    }
    shortcutLayout->addWidget(windowTable);
    bodyLayout()->addSpacing(26);

    // Window & platform conventions
    auto *windowSpec = makeSection(this, QStringLiteral("窗口与平台规范"));
    auto *windowSpecLayout = qobject_cast<QVBoxLayout *>(windowSpec->layout());
    windowSpecLayout->addWidget(paragraph(
        QStringLiteral(
            "同一份工作台在两套系统外壳下保持相同的界面内容、信息层级与业务流程；"
            "窗口控件、系统菜单、快捷键与关闭行为交给各自的平台惯例。"),
        windowSpec));
    windowSpecLayout->addSpacing(10);
    auto *platformCards = new QWidget(windowSpec);
    auto *platformLayout = new QHBoxLayout(platformCards);
    platformLayout->setContentsMargins(0, 0, 0, 0);
    platformLayout->setSpacing(12);
    platformLayout->addWidget(makePlatformCard(
        QStringLiteral("Windows 11"), windowsShell,
        {{QStringLiteral("窗口控件"),
          QStringLiteral("右上角最小化 / 最大化 / 关闭（46×32）；双击标题栏空白处"
                         "最大化，右键或 Alt+Space 打开系统菜单。")},
         {QStringLiteral("命令入口"),
          QStringLiteral("不使用传统菜单栏；页面切换与操作集中在左侧导航、页内按钮和"
                         "标题栏快捷提示，标题栏保持 32px 单层。")},
         {QStringLiteral("快捷键"),
          QStringLiteral("Ctrl 系快捷键；Alt+F4 关闭窗口，任务继续在后台运行。")},
         {QStringLiteral("关闭行为"),
          QStringLiteral("最小化后驻留任务栏；关闭按钮驻留系统托盘，任务继续；"
                         "任务栏与托盘图标都可恢复窗口。")},
         {QStringLiteral("窗口外观"),
          QStringLiteral("8px 圆角、32px 单层标题栏，阴影较 macOS 更收敛。")}},
        platformCards), 1);
    platformLayout->addWidget(makePlatformCard(
        QStringLiteral("macOS"), !windowsShell,
        {{QStringLiteral("窗口控件"),
          QStringLiteral("左上角红黄绿交通灯，指针移入整组时淡入关闭、最小化、最大化"
                         "符号。")},
         {QStringLiteral("菜单"),
          QStringLiteral("应用命令放在系统全局菜单栏，窗口内不重复出现菜单。")},
         {QStringLiteral("快捷键"),
          QStringLiteral("⌘ 系快捷键；⌘W 关闭窗口，任务继续在后台运行。")},
         {QStringLiteral("关闭行为"),
          QStringLiteral("关闭窗口后驻留菜单栏，后台任务继续。")},
         {QStringLiteral("窗口外观"),
          QStringLiteral("12px 圆角、38px 标题栏，窗口阴影更深。")}},
        platformCards), 1);
    windowSpecLayout->addWidget(platformCards);
    bodyLayout()->addSpacing(26);

    // Web differences
    auto *differences = makeSection(this, QStringLiteral("与 Web 版的结构差异"));
    auto *differenceLayout = qobject_cast<QVBoxLayout *>(differences->layout());
    auto *differenceTable = new QWidget(differences);
    differenceTable->setFixedWidth(760);
    auto *rows = new QVBoxLayout(differenceTable);
    rows->setContentsMargins(0, 0, 0, 0);
    rows->setSpacing(0);
    const QVector<QPair<QString, QString>> diffRows = {
        {QStringLiteral("源视频管理"),
         QStringLiteral("保留表格，但行内直接显示 ASR 进度条与切片数；双击行进入工作区，"
                        "替代原 Web 的多级跳转。")},
        {QStringLiteral("提示词管理"),
         QStringLiteral("保留列表；编辑器升级为右侧抽屉式，不打断当前工作区上下文。")},
        {QStringLiteral("任务管理"),
         QStringLiteral("Web 是表格，桌面改为卡片式队列——因为桌面用户需要一眼看到"
                        "“哪个任务在跑、还要多久”。")},
        {QStringLiteral("新增：工作台首页"),
         QStringLiteral("桌面端启动即看到流水线漏斗 + 待办精修数，"
                        "替代 Web 打开默认落在素材表。")},
        {QStringLiteral("新增：设置"),
         QStringLiteral("桌面端需要配置本地服务地址、草稿/成片输出目录、"
                        "菜单栏 / 系统托盘驻留等 Web 版不需要的能力。")},
    };
    for (int i = 0; i < diffRows.size(); ++i) {
        rows->addWidget(differenceRow(diffRows[i].first, diffRows[i].second,
                                      i == diffRows.size() - 1, differenceTable));
    }
    differenceLayout->addWidget(differenceTable);

    // 页脚：产品、公司与开源仓库入口。
    bodyLayout()->addSpacing(26);
    bodyLayout()->addWidget(Ui::separator(false, this));
    auto *footer = new QWidget(this);
    auto *footerLayout = new QHBoxLayout(footer);
    footerLayout->setContentsMargins(0, 16, 0, 4);
    footerLayout->setSpacing(8);
    footerLayout->addWidget(
        Ui::label(Brand::product(), 11, 500, Theme::Text2, footer), 0,
        Qt::AlignVCenter);
    footerLayout->addWidget(
        Ui::label(QStringLiteral("·"), 11, 400, Theme::BorderStrong, footer), 0,
        Qt::AlignVCenter);

    auto *credit = new QWidget(footer);
    auto *creditLayout = new QHBoxLayout(credit);
    creditLayout->setContentsMargins(0, 0, 0, 0);
    creditLayout->setSpacing(4);
    creditLayout->addWidget(
        Ui::label(QStringLiteral("由"), 11, 400, Theme::Text3, credit), 0,
        Qt::AlignVCenter);
    auto *company = new LinkLabel(Brand::company(), credit);
    company->setPixelSize(11);
    company->setTextColor(Theme::Text3);
    company->setHoverColor(Theme::Text1);
    company->setToolTip(QStringLiteral("在浏览器中打开公司官网"));
    connect(company, &QAbstractButton::clicked, this, [] {
        QDesktopServices::openUrl(QUrl(Brand::website()));
    });
    creditLayout->addWidget(company, 0, Qt::AlignVCenter);
    creditLayout->addWidget(
        Ui::label(QStringLiteral("开源发布"), 11, 400, Theme::Text3, credit), 0,
        Qt::AlignVCenter);
    footerLayout->addWidget(credit, 0, Qt::AlignVCenter);

    footerLayout->addWidget(
        Ui::label(QStringLiteral("·"), 11, 400, Theme::BorderStrong, footer), 0,
        Qt::AlignVCenter);
    auto *repo = new LinkLabel(QStringLiteral("GitHub 开源仓库"), footer);
    repo->setPixelSize(11);
    repo->setTextColor(Theme::Text3);
    repo->setHoverColor(Theme::Text1);
    repo->setLeadingGlyph(Icons::Glyph::Github, 12);
    repo->setToolTip(QStringLiteral("在浏览器中打开 GitHub 开源仓库"));
    connect(repo, &QAbstractButton::clicked, this, [] {
        QDesktopServices::openUrl(QUrl(Brand::repo()));
    });
    footerLayout->addWidget(repo, 0, Qt::AlignVCenter);
    footerLayout->addStretch(1);
    auto *license = Ui::label(Brand::license(), 10, 400, Theme::Text3, footer);
    license->setFont(Theme::monoFont(10));
    footerLayout->addWidget(license, 0, Qt::AlignVCenter);
    bodyLayout()->addWidget(footer);
    bodyLayout()->addStretch(1);
}
