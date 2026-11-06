#include "DashboardPage.h"

#include "../core/MediaUtils.h"
#include "../ui/Controls.h"

#include <QDate>
#include <QDateTime>
#include <QHBoxLayout>
#include <QLabel>
#include <QLinearGradient>
#include <QPainter>
#include <QPolygonF>
#include <QTime>
#include <QVBoxLayout>

#include <algorithm>
#include <functional>

namespace {

QString cssColor(const QColor &color)
{
    return color.name(QColor::HexArgb);
}

void setLabelColor(QLabel *label, const QColor &color)
{
    label->setStyleSheet(
        QStringLiteral("color:%1; background:transparent;").arg(cssColor(color)));
}

QString greetingText()
{
    const int hour = QTime::currentTime().hour();
    if (hour < 6) {
        return QStringLiteral("夜深了");
    }
    if (hour < 12) {
        return QStringLiteral("早上好");
    }
    if (hour < 14) {
        return QStringLiteral("中午好");
    }
    if (hour < 18) {
        return QStringLiteral("下午好");
    }
    return QStringLiteral("晚上好");
}

qint64 startOfTodayMs()
{
    return QDateTime(QDate::currentDate(), QTime(0, 0)).toMSecsSinceEpoch();
}

qint64 startOfWeekMs()
{
    const QDate today = QDate::currentDate();
    const QDate monday = today.addDays(1 - today.dayOfWeek());
    return QDateTime(monday, QTime(0, 0)).toMSecsSinceEpoch();
}

QString activityTime(qint64 timestampMs)
{
    if (timestampMs <= 0) {
        return QStringLiteral("—");
    }
    const QDateTime time = QDateTime::fromMSecsSinceEpoch(timestampMs);
    const QDate today = QDate::currentDate();
    if (time.date() == today) {
        return time.toString(QStringLiteral("HH:mm"));
    }
    if (time.date() == today.addDays(-1)) {
        return QStringLiteral("昨天 ") + time.toString(QStringLiteral("HH:mm"));
    }
    if (time.date().year() == today.year()) {
        return time.toString(QStringLiteral("MM-dd"));
    }
    return time.toString(QStringLiteral("yyyy-MM-dd"));
}

QString sourceDurationText(const MediaSource &source)
{
    if (source.isRecording()) {
        return QStringLiteral("录制中");
    }
    return source.durationMs > 0 ? MediaUtils::formatDuration(source.durationMs)
                                 : QStringLiteral("—");
}

QColor sourceStatusColor(const MediaSource &source)
{
    if (source.isRecording()) {
        return Theme::Danger;
    }
    if (source.isFailed()) {
        return Theme::Warn;
    }
    return source.draftCount > 0 ? Theme::Primary : Theme::Text3;
}

QString projectMetaText(const MediaSource &source, int downloaded)
{
    QStringList parts;
    if (source.kind == SourceKind::Local) {
        parts << QStringLiteral("本地导入");
    } else {
        parts << (source.isOnlineDownload() ? QStringLiteral("在线下载")
                                            : QStringLiteral("在线录制"));
    }

    if (source.isRecording()) {
        parts << (source.isOnlineDownload() ? QStringLiteral("正在下载")
                                            : QStringLiteral("正在录制"));
    } else {
        if (source.isFailed()) {
            const QString error = source.errorMessage.trimmed();
            parts << (error.isEmpty() ? QStringLiteral("处理失败")
                                      : QStringLiteral("处理失败：%1").arg(error));
        }
        if (source.draftCount > 0) {
            parts << QStringLiteral("%1 条剪映草稿").arg(source.draftCount);
            if (downloaded >= source.draftCount) {
                parts << QStringLiteral("已全部下载");
            } else if (downloaded > 0) {
                parts << QStringLiteral("已下载 %1 条").arg(downloaded);
            } else {
                parts << QStringLiteral("待下载");
            }
        } else if (!source.isFailed()) {
            parts << QStringLiteral("尚未生成剪映草稿");
        }
    }
    return parts.join(QStringLiteral(" · "));
}

void clearLayout(QVBoxLayout *layout)
{
    while (QLayoutItem *item = layout->takeAt(0)) {
        if (QWidget *widget = item->widget()) {
            widget->hide();
            widget->deleteLater();
        }
        delete item;
    }
}

QWidget *makeEmptyState(const QString &title, const QString &hint, QWidget *parent)
{
    auto *card = new Card(Theme::Surface2, Qt::transparent, 8, parent);
    auto *layout = card->contentLayout();
    layout->setContentsMargins(16, 18, 16, 18);
    layout->setSpacing(4);
    layout->addWidget(Ui::label(title, 12, 600, Theme::Text2, card));
    auto *hintLabel = Ui::label(hint, 11, 400, Theme::Text3, card);
    hintLabel->setWordWrap(true);
    layout->addWidget(hintLabel);
    return card;
}

struct ActivityItem
{
    qint64 atMs = 0;
    QString chip;
    Chip::Tone tone = Chip::Neutral;
    QString text;
};

} // namespace

class ProjectThumb : public QWidget
{
public:
    explicit ProjectThumb(const MediaSource &source, QWidget *parent = nullptr)
        : QWidget(parent)
        , m_dot(sourceStatusColor(source))
    {
        setFixedSize(56, 34);
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        painter.setPen(Qt::NoPen);

        QLinearGradient gradient(rect().topLeft(), rect().bottomRight());
        gradient.setColorAt(0.0, QColor("#2A3049"));
        gradient.setColorAt(1.0, QColor("#1A1E2C"));
        painter.setBrush(gradient);
        painter.drawRoundedRect(QRectF(rect()), 5, 5);

        QLinearGradient sheen(QPointF(rect().left(), rect().top()),
                              QPointF(rect().right(), rect().bottom()));
        sheen.setColorAt(0.25, QColor(110, 139, 255, 0));
        sheen.setColorAt(0.5, QColor(110, 139, 255, 64));
        sheen.setColorAt(0.75, QColor(110, 139, 255, 0));
        painter.setBrush(sheen);
        painter.drawRoundedRect(QRectF(rect()), 5, 5);

        QPolygonF play;
        play << QPointF(width() / 2.0 - 3.5, height() / 2.0 - 5.0)
             << QPointF(width() / 2.0 + 5.0, height() / 2.0)
             << QPointF(width() / 2.0 - 3.5, height() / 2.0 + 5.0);
        painter.setBrush(QColor(233, 236, 244, 150));
        painter.drawPolygon(play);

        painter.setPen(QPen(QColor("#14171F"), 1.6));
        painter.setBrush(m_dot);
        painter.drawEllipse(QPointF(8, 8), 3.2, 3.2);
    }

private:
    QColor m_dot;
};

class StatCard : public Card
{
public:
    explicit StatCard(const QString &labelText, QWidget *parent = nullptr)
        : Card(Theme::Surface1, Theme::Border, 8, parent)
    {
        auto *layout = contentLayout();
        layout->setContentsMargins(16, 14, 16, 14);
        layout->setSpacing(0);

        layout->addWidget(Ui::label(labelText, 11, 400, Theme::Text3, this));
        layout->addSpacing(4);
        m_value = Ui::label(QStringLiteral("0"), 26, 650, Theme::Text1, this);
        m_value->setFont(Theme::monoFont(26, 650));
        layout->addWidget(m_value);
        layout->addSpacing(10);
        m_hint = new ElidedLabel(QString(), this);
        m_hint->setPixelSize(11);
        m_hint->setTextColor(Theme::Text3);
        layout->addWidget(m_hint);
        setMinimumHeight(109);
    }

    void setValue(const QString &value, const QColor &color)
    {
        m_value->setText(value);
        setLabelColor(m_value, color);
    }

    void setHint(const QString &hint, const QColor &color = Theme::Text3)
    {
        m_hint->setText(hint);
        m_hint->setTextColor(color);
    }

private:
    QLabel *m_value = nullptr;
    ElidedLabel *m_hint = nullptr;
};

class FlowNode : public Card
{
public:
    explicit FlowNode(const QString &caption, QWidget *parent = nullptr)
        : Card(Theme::Surface2, Theme::Border, 7, parent)
    {
        auto *layout = contentLayout();
        layout->setContentsMargins(4, 10, 4, 10);
        layout->setSpacing(2);

        m_number = Ui::label(QStringLiteral("0"), 18, 650, Theme::Text1, this);
        m_number->setFont(Theme::monoFont(18, 650));
        m_number->setAlignment(Qt::AlignCenter);
        m_caption = Ui::label(caption, 11, 400, Theme::Text3, this);
        m_caption->setAlignment(Qt::AlignCenter);
        layout->addWidget(m_number);
        layout->addWidget(m_caption);
        setMinimumHeight(66);
    }

    void setCount(int count) { m_number->setText(QString::number(count)); }

    void setActive(bool active)
    {
        setCardColors(active ? Theme::alpha(Theme::Ai, 0.13) : Theme::Surface2,
                      active ? Theme::alpha(Theme::Ai, 0.40) : Theme::Border);
        setLabelColor(m_number, active ? Theme::Ai : Theme::Text1);
        setLabelColor(m_caption, active ? Theme::Ai : Theme::Text3);
    }

private:
    QLabel *m_number = nullptr;
    QLabel *m_caption = nullptr;
};

class FeedRow : public QWidget
{
public:
    FeedRow(const ActivityItem &item, bool last, QWidget *parent)
        : QWidget(parent)
        , m_last(last)
    {
        auto *layout = new QHBoxLayout(this);
        layout->setContentsMargins(0, 8, 0, 8);
        layout->setSpacing(10);

        auto *time = Ui::label(activityTime(item.atMs), 11, 400, Theme::Text3, this);
        time->setFont(Theme::monoFont(11));
        time->setFixedWidth(82);
        layout->addWidget(time);

        layout->addWidget(new Chip(item.chip, item.tone, this));
        auto *message = new ElidedLabel(item.text, this);
        message->setTextColor(Theme::Text1);
        message->setPixelSize(12);
        layout->addWidget(message, 1);
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        if (m_last) {
            return;
        }
        QPainter painter(this);
        painter.setPen(QPen(Theme::Border, 1.0, Qt::DashLine));
        painter.drawLine(QPointF(0, height() - 0.5), QPointF(width(), height() - 0.5));
    }

private:
    bool m_last = false;
};

Card *makeProjectRow(const MediaSource &source, int downloaded, QWidget *parent,
                     bool showDivider, const std::function<void()> &onClick)
{
    auto *row = new Card(QColor(Qt::transparent), QColor(Qt::transparent), 0, parent);
    row->setHoverColors(Theme::alpha(Qt::white, 0.03), QColor(Qt::transparent));
    row->setClickable(true);
    if (source.isRecording()) {
        row->setToolTip(QStringLiteral("录制进行中，可到源视频库查看"));
    } else if (source.isFailed()) {
        row->setToolTip(QStringLiteral("处理失败，可到源视频库重试"));
    } else if (source.draftCount > 0) {
        row->setToolTip(QStringLiteral("查看该视频的剪映草稿"));
    } else {
        row->setToolTip(QStringLiteral("前往源视频库开始一键成片"));
    }

    auto *rowLayout = new QVBoxLayout;
    rowLayout->setContentsMargins(0, 0, 0, 0);
    rowLayout->setSpacing(0);
    row->contentLayout()->addLayout(rowLayout);

    auto *content = new QWidget(row);
    auto *layout = new QHBoxLayout(content);
    layout->setContentsMargins(0, 9, 0, 9);
    layout->setSpacing(12);

    layout->addWidget(new ProjectThumb(source, content));
    auto *info = new QWidget(content);
    auto *infoLayout = new QVBoxLayout(info);
    infoLayout->setContentsMargins(0, 0, 0, 0);
    infoLayout->setSpacing(1);
    auto *title = new ElidedLabel(
        source.title.trimmed().isEmpty() ? QStringLiteral("未命名源视频") : source.title, info);
    title->setPixelSize(13);
    title->setFontWeight(600);
    title->setTextColor(Theme::Text1);
    infoLayout->addWidget(title);
    auto *meta = new ElidedLabel(projectMetaText(source, downloaded), info);
    meta->setPixelSize(11);
    meta->setTextColor(Theme::Text3);
    infoLayout->addWidget(meta);
    layout->addWidget(info, 1);

    auto *duration = Ui::label(sourceDurationText(source), 11, 400, Theme::Text2, content);
    duration->setFont(Theme::monoFont(11));
    layout->addWidget(duration, 0, Qt::AlignVCenter);

    rowLayout->addWidget(content);
    if (showDivider) {
        rowLayout->addWidget(Ui::separator(false, row));
    }

    QObject::connect(row, &Card::clicked, parent, [onClick] { onClick(); });
    return row;
}

DashboardPage::DashboardPage(QWidget *parent)
    : Page(parent)
{
    m_header = new PageHeader(greetingText(), QStringLiteral("正在读取本地数据…"), this);
    auto *openFolder = new Button(QStringLiteral("打开草稿目录"), Button::Ghost, this);
    connect(openFolder, &QAbstractButton::clicked, this,
            [this] { emit openDraftDirRequested(); });
    auto *addVideo = new Button(QStringLiteral("+ 添加源视频"), Button::Primary, this);
    connect(addVideo, &QAbstractButton::clicked, this,
            [this] { emit navigateRequested(QStringLiteral("material")); });
    m_header->addAction(openFolder);
    m_header->addAction(addVideo);
    bodyLayout()->addWidget(m_header);

    auto *stats = new QWidget(this);
    auto *statsLayout = new QHBoxLayout(stats);
    statsLayout->setContentsMargins(0, 0, 0, 0);
    statsLayout->setSpacing(12);
    m_sourceCard = new StatCard(QStringLiteral("源视频素材"), stats);
    m_draftCard = new StatCard(QStringLiteral("已生成剪映草稿"), stats);
    m_downloadCard = new StatCard(QStringLiteral("已下载草稿"), stats);
    m_taskCard = new StatCard(QStringLiteral("运行中任务"), stats);
    statsLayout->addWidget(m_sourceCard, 1);
    statsLayout->addWidget(m_draftCard, 1);
    statsLayout->addWidget(m_downloadCard, 1);
    statsLayout->addWidget(m_taskCard, 1);
    bodyLayout()->addWidget(stats);
    bodyLayout()->addSpacing(16);

    auto *dual = new QWidget(this);
    auto *dualLayout = new QHBoxLayout(dual);
    dualLayout->setContentsMargins(0, 0, 0, 0);
    dualLayout->setSpacing(12);

    auto *left = new Card(Theme::Surface1, Theme::Border, 8, dual);
    auto *leftLayout = left->contentLayout();
    leftLayout->setContentsMargins(16, 14, 16, 14);
    leftLayout->setSpacing(0);

    auto *pipelineHead = new QWidget(left);
    auto *pipelineHeadLayout = new QHBoxLayout(pipelineHead);
    pipelineHeadLayout->setContentsMargins(0, 0, 0, 12);
    pipelineHeadLayout->setSpacing(8);
    pipelineHeadLayout->addWidget(
        Ui::label(QStringLiteral("流水线概览"), 13, 600, Theme::Text1, pipelineHead));
    pipelineHeadLayout->addStretch(1);
    auto *more = new LinkLabel(QStringLiteral("查看任务中心 →"), pipelineHead);
    more->setTextColor(Theme::Text3);
    more->setHoverColor(Theme::Text2);
    more->setPixelSize(11);
    connect(more, &QAbstractButton::clicked, this,
            [this] { emit navigateRequested(QStringLiteral("tasks")); });
    pipelineHeadLayout->addWidget(more);
    leftLayout->addWidget(pipelineHead);

    auto *flow = new QWidget(left);
    auto *flowLayout = new QHBoxLayout(flow);
    flowLayout->setContentsMargins(0, 0, 0, 0);
    flowLayout->setSpacing(0);
    struct FlowSpec {
        FlowNode **slot;
        const char *caption;
    };
    const FlowSpec flowSpecs[] = {
        {&m_waitingNode, "待成片"},
        {&m_asrNode, "ASR 转写"},
        {&m_selectionNode, "AI 选片"},
        {&m_draftNode, "生成草稿"},
        {&m_exportNode, "已导出剪映"},
    };
    for (int i = 0; i < 5; ++i) {
        *flowSpecs[i].slot = new FlowNode(QString::fromUtf8(flowSpecs[i].caption), flow);
        flowLayout->addWidget(*flowSpecs[i].slot, 1);
        if (i < 4) {
            auto *arrow = Ui::label(QStringLiteral("→"), 13, 400, Theme::Text3, flow);
            arrow->setAlignment(Qt::AlignCenter);
            arrow->setFixedWidth(22);
            flowLayout->addWidget(arrow);
        }
    }
    leftLayout->addWidget(flow);
    leftLayout->addSpacing(18);

    auto *projectsHead = new QWidget(left);
    auto *projectsHeadLayout = new QHBoxLayout(projectsHead);
    projectsHeadLayout->setContentsMargins(0, 0, 0, 10);
    projectsHeadLayout->setSpacing(8);
    projectsHeadLayout->addWidget(
        Ui::label(QStringLiteral("最近项目"), 13, 600, Theme::Text1, projectsHead));
    projectsHeadLayout->addStretch(1);
    auto *allSources = new LinkLabel(QStringLiteral("全部素材 →"), projectsHead);
    allSources->setTextColor(Theme::Text3);
    allSources->setHoverColor(Theme::Text2);
    allSources->setPixelSize(11);
    connect(allSources, &QAbstractButton::clicked, this,
            [this] { emit navigateRequested(QStringLiteral("material")); });
    projectsHeadLayout->addWidget(allSources);
    leftLayout->addWidget(projectsHead);

    m_projectsHost = new QWidget(left);
    m_projectsLayout = new QVBoxLayout(m_projectsHost);
    m_projectsLayout->setContentsMargins(0, 0, 0, 0);
    m_projectsLayout->setSpacing(0);
    leftLayout->addWidget(m_projectsHost);
    leftLayout->addSpacing(10);
    leftLayout->addStretch(1);
    dualLayout->addWidget(left, 3);

    auto *right = new Card(Theme::Surface1, Theme::Border, 8, dual);
    auto *rightLayout = right->contentLayout();
    rightLayout->setContentsMargins(16, 14, 16, 14);
    rightLayout->setSpacing(0);
    rightLayout->addWidget(
        Ui::label(QStringLiteral("最近动态"), 13, 600, Theme::Text1, right));
    rightLayout->addSpacing(10);

    m_feedHost = new QWidget(right);
    m_feedLayout = new QVBoxLayout(m_feedHost);
    m_feedLayout->setContentsMargins(0, 0, 0, 0);
    m_feedLayout->setSpacing(0);
    rightLayout->addWidget(m_feedHost);
    rightLayout->addSpacing(12);
    rightLayout->addWidget(Ui::separator(false, right));
    rightLayout->addSpacing(12);
    rightLayout->addWidget(
        Ui::label(QStringLiteral("快捷操作"), 12, 400, Theme::Text3, right));
    rightLayout->addSpacing(8);
    auto *quick = new QWidget(right);
    auto *quickLayout = new QHBoxLayout(quick);
    quickLayout->setContentsMargins(0, 0, 0, 0);
    quickLayout->setSpacing(8);
    auto *add = new Button(QStringLiteral("添加视频"), Button::Primary, quick);
    add->setLeadingGlyph(Icons::Glyph::Plus);
    connect(add, &QAbstractButton::clicked, this, [this] { emit addSourceRequested(); });
    auto *library = new Button(QStringLiteral("去视频库"), Button::Default, quick);
    connect(library, &QAbstractButton::clicked, this,
            [this] { emit navigateRequested(QStringLiteral("material")); });
    quickLayout->addWidget(add);
    quickLayout->addWidget(library);
    quickLayout->addStretch(1);
    rightLayout->addWidget(quick);
    rightLayout->addStretch(1);
    dualLayout->addWidget(right, 2);

    bodyLayout()->addWidget(dual);
    bodyLayout()->addStretch(1);
}

void DashboardPage::setData(const QVector<MediaSource> &sources,
                            const QVector<ClipDraft> &drafts,
                            const QVector<ClipTask> &tasks,
                            const QString &databaseError)
{
    m_header->setTitle(greetingText());

    if (!databaseError.isEmpty()) {
        m_header->setDescription(
            QStringLiteral("<b style=\"color:#F6465D\">本地数据库不可用</b>：%1")
                .arg(databaseError.toHtmlEscaped()));
        for (StatCard *card : {m_sourceCard, m_draftCard, m_downloadCard, m_taskCard}) {
            card->setValue(QStringLiteral("—"), Theme::Text3);
            card->setHint(QStringLiteral("数据库不可用"), Theme::Danger);
        }
        for (FlowNode *node : {m_waitingNode, m_asrNode, m_selectionNode, m_draftNode,
                               m_exportNode}) {
            node->setCount(0);
            node->setActive(false);
        }
        clearLayout(m_projectsLayout);
        m_projectsLayout->addWidget(
            makeEmptyState(QStringLiteral("无法读取最近项目"), databaseError, m_projectsHost));
        clearLayout(m_feedLayout);
        m_feedLayout->addWidget(
            makeEmptyState(QStringLiteral("无法读取动态"), databaseError, m_feedHost));
        return;
    }

    int localSources = 0;
    int onlineSources = 0;
    int recordingSources = 0;
    int failedSources = 0;
    int waitingSources = 0;
    for (const MediaSource &source : sources) {
        if (source.kind == SourceKind::Local) {
            ++localSources;
        } else {
            ++onlineSources;
        }
        if (source.isRecording()) {
            ++recordingSources;
        } else if (source.isFailed()) {
            ++failedSources;
        } else if (source.draftCount == 0) {
            ++waitingSources;
        }
    }

    int downloadedDrafts = 0;
    int weekDrafts = 0;
    QHash<qint64, int> downloadedBySource;
    QHash<qint64, qint64> latestDraftBySource;
    const qint64 weekStart = startOfWeekMs();
    for (const ClipDraft &draft : drafts) {
        if (!draft.downloadedPath.isEmpty()) {
            ++downloadedDrafts;
            ++downloadedBySource[draft.sourceId];
        }
        if (draft.createdAtMs >= weekStart) {
            ++weekDrafts;
        }
        const qint64 previous = latestDraftBySource.value(draft.sourceId, 0);
        if (draft.createdAtMs > previous) {
            latestDraftBySource.insert(draft.sourceId, draft.createdAtMs);
        }
    }
    const int pendingDrafts = qMax(0, drafts.size() - downloadedDrafts);

    int runningTasks = 0;
    int failedTasks = 0;
    int completedToday = 0;
    int stepCounts[3] = {0, 0, 0};
    const qint64 todayStart = startOfTodayMs();
    for (const ClipTask &task : tasks) {
        if (task.status == ClipTaskStatus::Running) {
            ++runningTasks;
            if (task.step >= 0 && task.step < 3) {
                ++stepCounts[task.step];
            }
        } else if (task.status == ClipTaskStatus::Failed) {
            ++failedTasks;
        } else if (task.finishedAtMs >= todayStart) {
            ++completedToday;
        }
    }

    QString summary;
    if (sources.isEmpty() && drafts.isEmpty() && tasks.isEmpty()) {
        summary = QStringLiteral("还没有任何素材或任务。添加源视频，或导入本地文件，"
                                 "开始你的第一条切片。");
    } else {
        QStringList parts;
        if (runningTasks > 0) {
            parts << QStringLiteral("<b style=\"color:#E9ECF4\">%1 个任务</b>正在运行")
                         .arg(runningTasks);
        }
        if (pendingDrafts > 0) {
            parts << QStringLiteral("<b style=\"color:#FFB224\">%1 条剪映草稿</b>待下载")
                         .arg(pendingDrafts);
        }
        if (failedTasks > 0) {
            parts << QStringLiteral("<b style=\"color:#F6465D\">%1 个任务</b>失败")
                         .arg(failedTasks);
        }
        if (!parts.isEmpty()) {
            summary = QStringLiteral("当前有 %1。").arg(parts.join(QStringLiteral("，")));
        } else if (!drafts.isEmpty()) {
            summary = QStringLiteral("素材库共 <b style=\"color:#E9ECF4\">%1 个源视频</b>，"
                                     "已生成 <b style=\"color:#E9ECF4\">%2 条剪映草稿</b>，"
                                     "全部草稿已下载到本地。")
                          .arg(sources.size())
                          .arg(drafts.size());
        } else {
            summary = QStringLiteral("素材库共 <b style=\"color:#E9ECF4\">%1 个源视频</b>，"
                                     "还没有生成剪映草稿。")
                          .arg(sources.size());
        }
    }
    m_header->setDescription(summary);

    QString sourceHint;
    if (sources.isEmpty()) {
        sourceHint = QStringLiteral("还没有源视频");
    } else {
        QStringList parts;
        if (localSources > 0) {
            parts << QStringLiteral("本地 %1").arg(localSources);
        }
        if (onlineSources > 0) {
            parts << QStringLiteral("在线 %1").arg(onlineSources);
        }
        if (recordingSources > 0) {
            parts << QStringLiteral("%1 个录制中").arg(recordingSources);
        }
        if (failedSources > 0) {
            parts << QStringLiteral("%1 个失败").arg(failedSources);
        }
        sourceHint = parts.join(QStringLiteral(" · "));
    }
    m_sourceCard->setValue(QString::number(sources.size()),
                           sources.isEmpty() ? Theme::Text2 : Theme::Text1);
    m_sourceCard->setHint(sourceHint, failedSources > 0 ? Theme::Danger
                                      : recordingSources > 0 ? Theme::Primary
                                                             : Theme::Text3);

    m_draftCard->setValue(QString::number(drafts.size()),
                          drafts.isEmpty() ? Theme::Text2 : Theme::Ai);
    m_draftCard->setHint(drafts.isEmpty()
                             ? QStringLiteral("尚未生成草稿")
                             : weekDrafts > 0
                                   ? QStringLiteral("本周新增 %1 条").arg(weekDrafts)
                                   : QStringLiteral("本周暂无新增"));

    m_downloadCard->setValue(QString::number(downloadedDrafts),
                             downloadedDrafts == 0 ? Theme::Text2 : Theme::Success);
    m_downloadCard->setHint(drafts.isEmpty()
                                ? QStringLiteral("暂无草稿")
                                : pendingDrafts > 0
                                      ? QStringLiteral("待下载 %1 条").arg(pendingDrafts)
                                      : QStringLiteral("全部草稿已下载"),
                            pendingDrafts > 0 ? Theme::Ai : Theme::Text3);

    QString taskHint;
    QColor taskHintColor = Theme::Text3;
    if (runningTasks > 0) {
        taskHint = completedToday > 0 ? QStringLiteral("今日完成 %1 个").arg(completedToday)
                                      : QStringLiteral("正在处理中");
    } else if (failedTasks > 0) {
        taskHint = QStringLiteral("%1 个失败待重试").arg(failedTasks);
        taskHintColor = Theme::Danger;
    } else if (completedToday > 0) {
        taskHint = QStringLiteral("今日完成 %1 个").arg(completedToday);
    } else {
        taskHint = QStringLiteral("暂无运行中任务");
    }
    m_taskCard->setValue(QString::number(runningTasks),
                         runningTasks == 0 ? Theme::Text2 : Theme::Primary);
    m_taskCard->setHint(taskHint, taskHintColor);

    m_waitingNode->setCount(waitingSources);
    m_asrNode->setCount(stepCounts[0]);
    m_selectionNode->setCount(stepCounts[1]);
    m_draftNode->setCount(stepCounts[2]);
    m_exportNode->setCount(downloadedDrafts);
    m_waitingNode->setActive(false);
    m_asrNode->setActive(stepCounts[0] > 0);
    m_selectionNode->setActive(stepCounts[1] > 0);
    m_draftNode->setActive(stepCounts[2] > 0);
    m_exportNode->setActive(runningTasks == 0 && downloadedDrafts > 0);

    rebuildProjects(sources, downloadedBySource, latestDraftBySource);
    rebuildFeed(sources, drafts, tasks);
}

void DashboardPage::rebuildProjects(const QVector<MediaSource> &sources,
                                    const QHash<qint64, int> &downloadedBySource,
                                    const QHash<qint64, qint64> &latestDraftBySource)
{
    clearLayout(m_projectsLayout);

    if (sources.isEmpty()) {
        m_projectsLayout->addWidget(makeEmptyState(
            QStringLiteral("还没有源视频"),
            QStringLiteral("点击右上角「+ 添加源视频」，或把本地视频导入素材库。"),
            m_projectsHost));
        return;
    }

    QVector<MediaSource> recent = sources;
    std::sort(recent.begin(), recent.end(),
              [&latestDraftBySource](const MediaSource &a, const MediaSource &b) {
                  if (a.isRecording() != b.isRecording()) {
                      return a.isRecording();
                  }
                  const qint64 aTime = qMax(qMax(a.updatedAtMs, a.createdAtMs),
                                            latestDraftBySource.value(a.id, 0));
                  const qint64 bTime = qMax(qMax(b.updatedAtMs, b.createdAtMs),
                                            latestDraftBySource.value(b.id, 0));
                  if (aTime != bTime) {
                      return aTime > bTime;
                  }
                  return a.id > b.id;
              });
    const int count = qMin(4, recent.size());
    for (int i = 0; i < count; ++i) {
        const MediaSource &source = recent[i];
        auto *row = makeProjectRow(source, downloadedBySource.value(source.id), m_projectsHost,
                                   i < count - 1, [this, source] {
                                       emit openSourceRequested(source.id);
                                   });
        m_projectsLayout->addWidget(row);
    }
}

void DashboardPage::rebuildFeed(const QVector<MediaSource> &sources,
                                const QVector<ClipDraft> &drafts,
                                const QVector<ClipTask> &tasks)
{
    clearLayout(m_feedLayout);

    QHash<qint64, QString> sourceTitles;
    for (const MediaSource &source : sources) {
        sourceTitles.insert(source.id, source.title.trimmed());
    }

    QVector<ActivityItem> items;
    items.reserve(tasks.size() + qMin(6, drafts.size()) + qMin(6, sources.size()));

    // 素材本身也是动态：导入、录制完成/失败、正在录制都来自数据库状态。
    QVector<MediaSource> recentSources = sources;
    std::sort(recentSources.begin(), recentSources.end(),
              [](const MediaSource &a, const MediaSource &b) {
                  const auto activity = [](const MediaSource &source) {
                      return source.isRecording() ? source.createdAtMs
                                                  : qMax(source.updatedAtMs,
                                                         source.createdAtMs);
                  };
                  return activity(a) > activity(b);
              });
    const int recentSourceCount = qMin(6, recentSources.size());
    for (int i = 0; i < recentSourceCount; ++i) {
        const MediaSource &source = recentSources[i];
        const QString title = source.title.trimmed().isEmpty()
            ? QStringLiteral("未命名源视频")
            : source.title;
        ActivityItem item;
        if (source.isRecording()) {
            const bool download = source.isOnlineDownload();
            item.atMs = source.createdAtMs;
            item.chip = download ? QStringLiteral("下载中") : QStringLiteral("录制中");
            item.tone = Chip::Live;
            item.text = download ? QStringLiteral("「%1」正在下载").arg(title)
                                 : QStringLiteral("「%1」正在录制").arg(title);
        } else if (source.isFailed()) {
            item.atMs = qMax(source.updatedAtMs, source.createdAtMs);
            item.chip = QStringLiteral("失败");
            item.tone = Chip::Error;
            item.text = QStringLiteral("「%1」%2")
                            .arg(title, source.errorMessage.trimmed().isEmpty()
                                           ? QStringLiteral("处理失败")
                                           : QStringLiteral("处理失败：%1")
                                                 .arg(source.errorMessage.trimmed()));
        } else if (source.kind == SourceKind::Online) {
            item.atMs = qMax(source.updatedAtMs, source.createdAtMs);
            item.chip = QStringLiteral("就绪");
            item.tone = Chip::Ok;
            const QString warning = source.errorMessage.trimmed();
            const QString action = source.isOnlineDownload()
                ? QStringLiteral("下载结束")
                : QStringLiteral("录制结束");
            item.text = warning.isEmpty()
                ? QStringLiteral("「%1」%2，素材已就绪").arg(title, action)
                : QStringLiteral("「%1」%2，素材已就绪：%3")
                      .arg(title, action, warning);
        } else {
            item.atMs = source.createdAtMs;
            item.chip = QStringLiteral("素材");
            item.tone = Chip::Neutral;
            item.text = QStringLiteral("「%1」已导入素材库").arg(title);
        }
        if (item.atMs > 0) {
            items.append(item);
        }
    }

    for (const ClipTask &task : tasks) {
        const QString title =
            task.title.trimmed().isEmpty() ? QStringLiteral("未命名项目") : task.title;
        ActivityItem item;
        if (task.status == ClipTaskStatus::Running) {
            item.atMs = qMax(task.updatedAtMs, task.createdAtMs);
            item.chip = QStringLiteral("进行中");
            item.tone = Chip::Run;
            const QString detail =
                task.message.isEmpty()
                    ? QStringLiteral("%1 %2%").arg(clipTaskStepName(task.step)).arg(task.progress)
                    : task.message;
            item.text = QStringLiteral("%1 · %2").arg(title, detail);
        } else if (task.status == ClipTaskStatus::Completed) {
            item.atMs = qMax(task.finishedAtMs, task.updatedAtMs);
            item.chip = QStringLiteral("完成");
            item.tone = Chip::Ok;
            item.text = QStringLiteral("%1 · 已完成，生成 %2 条剪映草稿")
                            .arg(title)
                            .arg(task.draftCount);
        } else {
            item.atMs = qMax(task.finishedAtMs, task.updatedAtMs);
            item.chip = QStringLiteral("失败");
            item.tone = Chip::Error;
            item.text = QStringLiteral("%1 · %2")
                            .arg(title, task.message.isEmpty() ? QStringLiteral("任务执行失败。")
                                                               : task.message);
        }
        if (item.atMs > 0) {
            items.append(item);
        }
    }

    QVector<ClipDraft> recentDrafts = drafts;
    std::sort(recentDrafts.begin(), recentDrafts.end(),
              [](const ClipDraft &a, const ClipDraft &b) {
                  if (a.createdAtMs != b.createdAtMs) {
                      return a.createdAtMs > b.createdAtMs;
                  }
                  return a.id > b.id;
              });
    const int recentDraftCount = qMin(6, recentDrafts.size());
    for (int i = 0; i < recentDraftCount; ++i) {
        const ClipDraft &draft = recentDrafts[i];
        const QString draftTitle =
            draft.title.trimmed().isEmpty() ? QStringLiteral("直播切片") : draft.title;
        const QString sourceTitle = sourceTitles.value(draft.sourceId);
        ActivityItem item;
        item.atMs = draft.createdAtMs;
        item.chip = QStringLiteral("AI");
        item.tone = Chip::Ai;
        item.text = sourceTitle.isEmpty()
            ? QStringLiteral("「%1」剪映草稿已生成").arg(draftTitle)
            : QStringLiteral("%1 · 「%2」剪映草稿已生成").arg(sourceTitle, draftTitle);
        if (item.atMs > 0) {
            items.append(item);
        }
    }

    std::sort(items.begin(), items.end(), [](const ActivityItem &a, const ActivityItem &b) {
        if (a.atMs != b.atMs) {
            return a.atMs > b.atMs;
        }
        return a.chip < b.chip;
    });

    const int visibleCount = qMin(6, items.size());
    if (visibleCount == 0) {
        m_feedLayout->addWidget(makeEmptyState(
            QStringLiteral("暂无动态"),
            QStringLiteral("开始一键成片后，任务进度与草稿生成会显示在这里。"), m_feedHost));
        return;
    }
    for (int i = 0; i < visibleCount; ++i) {
        m_feedLayout->addWidget(
            new FeedRow(items[i], i == visibleCount - 1, m_feedHost));
    }
}
