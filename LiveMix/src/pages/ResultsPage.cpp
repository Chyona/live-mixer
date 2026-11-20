// 文件作用：成片结果页实现，卡片内容全部来自 SQLite 中的草稿记录。
#include "ResultsPage.h"

#include "../core/FileManager.h"
#include "../core/MediaUtils.h"
#include "../ui/Controls.h"
#include "../ui/Icons.h"

#include <QClipboard>
#include <QDateTime>
#include <QFileInfo>
#include <QGraphicsDropShadowEffect>
#include <QGridLayout>
#include <QGuiApplication>
#include <QHBoxLayout>
#include <QKeyEvent>
#include <QLabel>
#include <QLayout>
#include <QPainter>
#include <QPixmap>
#include <QResizeEvent>
#include <QScrollArea>
#include <QShortcut>
#include <QTimer>
#include <QVBoxLayout>

#include <functional>

namespace {

// 话题标签的流式布局：卡片变窄时标签自动换行，避免把卡片越撑越宽。
class FlowLayout : public QLayout
{
public:
    explicit FlowLayout(QWidget *parent = nullptr, int horizontalSpacing = 4,
                        int verticalSpacing = 4)
        : QLayout(parent)
        , m_horizontalSpacing(horizontalSpacing)
        , m_verticalSpacing(verticalSpacing)
    {
    }

    ~FlowLayout() override
    {
        while (QLayoutItem *item = takeAt(0)) {
            delete item;
        }
    }

    void addItem(QLayoutItem *item) override { m_items.append(item); }
    int count() const override { return m_items.size(); }
    QLayoutItem *itemAt(int index) const override { return m_items.value(index); }
    QLayoutItem *takeAt(int index) override
    {
        return index >= 0 && index < m_items.size() ? m_items.takeAt(index) : nullptr;
    }
    Qt::Orientations expandingDirections() const override { return {}; }
    bool hasHeightForWidth() const override { return true; }

    int heightForWidth(int width) const override
    {
        return doLayout(QRect(0, 0, width, 0), true);
    }

    QSize minimumSize() const override
    {
        QSize size;
        for (const QLayoutItem *item : m_items) {
            size = size.expandedTo(item->minimumSize());
        }
        return size;
    }

    QSize sizeHint() const override { return minimumSize(); }

    void setGeometry(const QRect &rect) override
    {
        QLayout::setGeometry(rect);
        doLayout(rect, false);
    }

private:
    int doLayout(const QRect &rect, bool testOnly) const
    {
        int x = rect.x();
        int y = rect.y();
        int lineHeight = 0;
        for (QLayoutItem *item : m_items) {
            const QSize hint = item->sizeHint();
            if (x + hint.width() > rect.right() + 1 && lineHeight > 0) {
                x = rect.x();
                y += lineHeight + m_verticalSpacing;
                lineHeight = 0;
            }
            if (!testOnly) {
                item->setGeometry(QRect(QPoint(x, y), hint));
            }
            x += hint.width() + m_horizontalSpacing;
            lineHeight = qMax(lineHeight, hint.height());
        }
        return y + lineHeight - rect.y();
    }

    QList<QLayoutItem *> m_items;
    int m_horizontalSpacing = 4;
    int m_verticalSpacing = 4;
};

QLabel *tagLabel(const QString &text, QWidget *parent)
{
    auto *tag = new QLabel(text, parent);
    tag->setFont(Theme::font(10));
    tag->setStyleSheet(QStringLiteral(
        "color:#6E8BFF;background:rgba(110,139,255,36);border-radius:4px;"
        "padding:1px 6px;"));
    tag->setSizePolicy(QSizePolicy::Fixed, QSizePolicy::Fixed);
    tag->setAttribute(Qt::WA_TransparentForMouseEvents, true);
    return tag;
}

// 小尺寸矢量图标标签，用于生成时间标记。
QLabel *glyphLabel(Icons::Glyph glyph, int size, const QColor &color, QWidget *parent)
{
    constexpr qreal kPixelRatio = 2.0;
    QPixmap pixmap(qRound(size * kPixelRatio), qRound(size * kPixelRatio));
    pixmap.setDevicePixelRatio(kPixelRatio);
    pixmap.fill(Qt::transparent);
    QPainter painter(&pixmap);
    painter.setRenderHint(QPainter::Antialiasing, true);
    Icons::paint(&painter, QRectF(0, 0, size, size), glyph, color, 2.0);
    painter.end();

    auto *label = new QLabel(parent);
    label->setPixmap(pixmap);
    label->setFixedSize(size, size);
    label->setAttribute(Qt::WA_TransparentForMouseEvents, true);
    return label;
}

constexpr qint64 kGenerationMinuteMs = 60 * 1000;

QString generationClock(const QDateTime &date)
{
    return date.toString(QStringLiteral("HH:mm:ss"));
}

// 成片生成时间：精确到秒，越近越详细（刚刚 → 今天 → 昨天 → 前天 → N 天前 →
// 月日 → 年月日），与设计稿的页头 / 卡片共用同一套文案。
QString formatGenerationTime(qint64 timestampMs)
{
    if (timestampMs <= 0) {
        return QString();
    }
    const QDateTime date = QDateTime::fromMSecsSinceEpoch(timestampMs);
    const QDateTime now = QDateTime::currentDateTime();
    const QString clock = generationClock(date);
    const qint64 elapsed = now.toMSecsSinceEpoch() - timestampMs;

    if (elapsed >= 0 && elapsed < kGenerationMinuteMs) {
        return QStringLiteral("刚刚 · %1").arg(clock);
    }

    const int dayDiff = date.date().daysTo(now.date());
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
        .arg(generationClock(date));
}

bool isFreshGeneration(const QString &label)
{
    return label.startsWith(QStringLiteral("刚刚"));
}

QString generationValueStyle(const QString &label)
{
    return QStringLiteral("color:%1;background:transparent;")
        .arg((isFreshGeneration(label) ? Theme::Success : Theme::Text1).name());
}

void setButtonState(Button *button, const QString &text, Button::Variant variant,
                    Icons::Glyph glyph, int progress, const QColor &textColor)
{
    if (!button) {
        return;
    }
    if (button->text() != text) {
        button->setText(text);
    }
    button->setVariant(variant);
    button->setLeadingGlyph(glyph);
    button->setProgressPercent(progress);
    button->setTextColor(textColor);
}

// 批量选择模式下覆盖整张卡片的透明层：负责圆点、选中底色与点击切换。
class CardSelectOverlay : public QWidget
{
public:
    explicit CardSelectOverlay(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        setCursor(Qt::PointingHandCursor);
        setAttribute(Qt::WA_Hover, true);
        m_timer = new QTimer(this);
        m_timer->setInterval(60);
        connect(m_timer, &QTimer::timeout, this, [this] {
            m_angle = (m_angle + 30) % 360;
            update();
        });
        hide();
    }

    void setState(bool eligible, bool selected, bool anyRunning, bool bothDone)
    {
        m_eligible = eligible;
        m_selected = selected;
        m_anyRunning = anyRunning;
        m_bothDone = bothDone;
        setCursor(eligible ? Qt::PointingHandCursor : Qt::ArrowCursor);
        if (anyRunning) {
            m_timer->start();
        } else {
            m_timer->stop();
        }
        update();
    }

    std::function<void()> onClicked;

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        if (m_selected) {
            painter.fillRect(rect(), Theme::alpha(Theme::Primary, 0.12));
        }

        const qreal side = 20.0;
        const QRectF circle(width() - 8.0 - side, 8.0, side, side);
        if (m_eligible && m_selected) {
            painter.setPen(QPen(Theme::Primary, 1.0));
            painter.setBrush(Theme::Primary);
            painter.drawEllipse(circle);
            Icons::paint(&painter, circle.adjusted(4, 4, -4, -4), Icons::Glyph::Check,
                         Theme::OnAccent, 2.2);
            return;
        }
        if (m_eligible) {
            painter.setPen(QPen(Theme::alpha(Qt::white, 0.55), 1.0));
            painter.setBrush(Theme::alpha(Qt::black, 0.40));
            painter.drawEllipse(circle);
            return;
        }

        painter.setPen(QPen(Theme::BorderStrong, 1.0));
        painter.setBrush(Theme::alpha(Qt::black, 0.40));
        painter.drawEllipse(circle);
        if (m_anyRunning) {
            const qreal inner = 11.0;
            const QRectF box(circle.center().x() - inner / 2.0,
                             circle.center().y() - inner / 2.0, inner, inner);
            painter.setPen(QPen(Theme::alpha(Theme::Primary, 0.25), 1.5));
            painter.drawEllipse(box);
            painter.setPen(QPen(Theme::Primary, 1.5, Qt::SolidLine, Qt::RoundCap));
            painter.drawArc(box, int((90 - m_angle) * 16), 100 * 16);
        } else if (m_bothDone) {
            Icons::paint(&painter, circle.adjusted(5, 5, -5, -5), Icons::Glyph::Check,
                         Theme::Success, 2.0);
        }
    }

    void mousePressEvent(QMouseEvent *event) override
    {
        if (m_eligible && event->button() == Qt::LeftButton && onClicked) {
            onClicked();
            event->accept();
            return;
        }
        event->accept();
    }

private:
    bool m_eligible = true;
    bool m_selected = false;
    bool m_anyRunning = false;
    bool m_bothDone = false;
    QTimer *m_timer = nullptr;
    int m_angle = 0;
};

} // namespace

// -----------------------------------------------------------------------------
// DraftCard
// -----------------------------------------------------------------------------

class DraftCard : public Card
{
public:
    using CopyCallback = std::function<void(const QString &text, const QString &label)>;
    using OpenCallback = std::function<void(qint64 draftId)>;
    using VideoCallback = std::function<void(qint64 draftId)>;
    using ToggleCallback = std::function<void(qint64 draftId)>;

    struct State
    {
        const DraftDownloadJob *draftJob = nullptr;
        const VideoExportJob *videoJob = nullptr;
        const DraftBatchItem *draftBatch = nullptr;
        const VideoBatchItem *videoBatch = nullptr;
        bool selecting = false;
        bool selected = false;
        bool selectable = true;
        bool anyRunning = false;
        bool bothDone = false;
        bool anyError = false;
        bool draftEligible = true;
        bool videoEligible = true;
        bool draftDownloaded = false;
    };

    DraftCard(const ClipDraft &draft, int index, QWidget *parent, CopyCallback copy,
              OpenCallback open, VideoCallback video, ToggleCallback toggle)
        : Card(Theme::Surface1, Theme::Border, 10, parent)
        , m_draft(draft)
        , m_open(std::move(open))
        , m_video(std::move(video))
        , m_toggle(std::move(toggle))
    {
        setHoverEnabled(true);
        setHoverColors(Theme::Surface1, Theme::BorderStrong);
        contentLayout()->setContentsMargins(1, 1, 1, 1);
        contentLayout()->setSpacing(0);

        m_thumb = new MediaThumbnail(MediaThumbnail::Cool, this);
        m_thumb->setTopRadius(9);
        m_thumb->setDuration(MediaUtils::formatDuration(draft.durationMs));
        contentLayout()->addWidget(m_thumb);

        auto *body = new QWidget(this);
        auto *layout = new QVBoxLayout(body);
        layout->setContentsMargins(12, 12, 12, 12);
        layout->setSpacing(0);

        auto addRow = [&](const QString &label, QWidget *value, const QString &copyText,
                          const QString &copyLabel, int bottomGap) {
            auto *row = new QWidget(body);
            auto *rowLayout = new QHBoxLayout(row);
            rowLayout->setContentsMargins(0, 0, 0, 0);
            rowLayout->setSpacing(8);
            auto *lb = Ui::label(label, 11, 400, Theme::Text3, row);
            lb->setFixedWidth(40);
            rowLayout->addWidget(lb, 0, Qt::AlignTop);
            rowLayout->addWidget(value, 1, Qt::AlignVCenter);
            auto *copyButton = new GlyphButton(Icons::Glyph::Copy, 18, row);
            copyButton->setGlyphSize(11);
            copyButton->setColors(Theme::Text3, Theme::Primary);
            copyButton->setToolTipText(QStringLiteral("复制%1").arg(copyLabel));
            connect(copyButton, &QAbstractButton::clicked, this,
                    [copy, copyText, copyLabel] { copy(copyText, copyLabel); });
            rowLayout->addWidget(copyButton, 0, Qt::AlignTop);
            layout->addWidget(row);
            if (bottomGap > 0) {
                layout->addSpacing(bottomGap);
            }
        };

        // 生成时间标记：信息区顶部整行，精确到秒，悬停查看完整时间。
        m_generatedAtBar = new QWidget(body);
        m_generatedAtBar->setObjectName(QStringLiteral("generationTimeBar"));
        m_generatedAtBar->setAttribute(Qt::WA_StyledBackground, true);
        m_generatedAtBar->setStyleSheet(
            QStringLiteral("#generationTimeBar{background:%1;border-radius:5px;}")
                .arg(Theme::Surface2.name()));
        m_generatedAtBar->setFixedHeight(20);
        m_generatedAtBar->setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
        auto *generationLayout = new QHBoxLayout(m_generatedAtBar);
        generationLayout->setContentsMargins(8, 1, 8, 1);
        generationLayout->setSpacing(6);
        generationLayout->addWidget(
            glyphLabel(Icons::Glyph::Clock, 12, Theme::Text3, m_generatedAtBar), 0,
            Qt::AlignVCenter);
        generationLayout->addWidget(
            Ui::label(QStringLiteral("生成时间"), 11, 400, Theme::Text3,
                      m_generatedAtBar),
            0, Qt::AlignVCenter);
        m_generatedAtValue = new ElidedLabel(QString(), m_generatedAtBar);
        m_generatedAtValue->setPixelSize(11);
        m_generatedAtValue->setFontWeight(QFont::Medium);
        m_generatedAtValue->setMonoFont(true);
        m_generatedAtValue->setTextColor(Theme::Text1);
        generationLayout->addWidget(m_generatedAtValue, 1, Qt::AlignVCenter);
        // 外层承载条与下间距，隐藏时整块收起，不留下空白。
        m_generatedAtHost = new QWidget(body);
        auto *generationHostLayout = new QVBoxLayout(m_generatedAtHost);
        generationHostLayout->setContentsMargins(0, 0, 0, 0);
        generationHostLayout->setSpacing(0);
        generationHostLayout->addWidget(m_generatedAtBar);
        generationHostLayout->addSpacing(8);
        m_generatedAtHost->hide();
        layout->addWidget(m_generatedAtHost);

        const QString themeText =
            QStringLiteral("%1 · %2")
                .arg(index + 1, 2, 10, QLatin1Char('0'))
                .arg(draft.title.isEmpty() ? QStringLiteral("直播切片") : draft.title);
        auto *theme = Ui::label(themeText, 12, 600, Theme::Text1, body);
        addRow(QStringLiteral("主题"), theme, themeText, QStringLiteral("主题"), 8);

        const QString descriptionText =
            draft.description.isEmpty() ? QStringLiteral("AI 已选出该片段并生成草稿。")
                                        : draft.description;
        auto *description = new ElidedLabel(descriptionText, body);
        description->setPixelSize(12);
        description->setTextColor(Theme::Text1);
        addRow(QStringLiteral("描述"), description, descriptionText,
               QStringLiteral("描述"), 8);

        auto *tags = new QWidget(body);
        tags->setSizePolicy(QSizePolicy::Ignored, QSizePolicy::Preferred);
        auto *tagsLayout = new FlowLayout(tags);
        tagsLayout->setContentsMargins(0, 0, 0, 0);
        const QStringList tagList =
            draft.tags.isEmpty() ? QStringList{QStringLiteral("直播切片")} : draft.tags;
        QStringList displayTags;
        for (const QString &tag : tagList) {
            // 后端的 topics 不带 #，界面统一补上；兼容历史记录里已带 # 的数据。
            const QString displayTag = tag.startsWith(QLatin1Char('#'))
                ? tag
                : QStringLiteral("#") + tag;
            displayTags.append(displayTag);
            tagsLayout->addWidget(tagLabel(displayTag, tags));
        }
        addRow(QStringLiteral("话题"), tags, displayTags.join(QLatin1Char(' ')),
               QStringLiteral("话题"), 8);

        auto *draftName = Ui::label(draft.draftId, 11, 400, Theme::Text2, body);
        draftName->setFont(Theme::monoFont(11));
        addRow(QStringLiteral("草稿"), draftName, draft.draftId,
               QStringLiteral("草稿名"), 4);

        const QString rangeText =
            QStringLiteral("%1 – %2").arg(
                MediaUtils::formatDuration(draft.sourceStartMs),
                MediaUtils::formatDuration(draft.sourceEndMs));
        auto *range = Ui::label(rangeText, 11, 400, Theme::Text3, body);
        range->setFont(Theme::monoFont(11));
        addRow(QStringLiteral("区间"), range, rangeText, QStringLiteral("时间点"), 8);

        m_buttonRow = new QWidget(body);
        auto *buttonLayout = new QHBoxLayout(m_buttonRow);
        buttonLayout->setContentsMargins(0, 0, 0, 0);
        buttonLayout->setSpacing(6);
        m_draftButton = new Button(QStringLiteral("导出到剪映"), Button::Primary,
                                   m_buttonRow);
        m_draftButton->setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
        m_videoButton = new Button(QStringLiteral("导出成片"), Button::Default,
                                   m_buttonRow);
        m_videoButton->setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
        buttonLayout->addWidget(m_draftButton, 1);
        buttonLayout->addWidget(m_videoButton, 1);
        layout->addWidget(m_buttonRow);

        m_selectionStatus = new ElidedLabel(QString(), body);
        m_selectionStatus->setPixelSize(11);
        m_selectionStatus->setTextColor(Theme::Text3);
        m_selectionStatus->setFixedHeight(32);
        m_selectionStatus->hide();
        layout->addWidget(m_selectionStatus);

        connect(m_draftButton, &QAbstractButton::clicked, this, [this] {
            if (m_state.draftBatch
                && m_state.draftBatch->status != DraftBatchItemStatus::Done) {
                if (m_openDraftBatch) {
                    m_openDraftBatch();
                }
                return;
            }
            if (m_open) {
                m_open(m_draft.id);
            }
        });
        connect(m_videoButton, &QAbstractButton::clicked, this, [this] {
            if (m_state.videoBatch
                && m_state.videoBatch->status != VideoBatchItemStatus::Done) {
                if (m_openVideoBatch) {
                    m_openVideoBatch();
                }
                return;
            }
            if (m_video) {
                m_video(m_draft.id);
            }
        });

        contentLayout()->addWidget(body);

        m_overlay = new CardSelectOverlay(this);
        m_overlay->onClicked = [this] { m_toggle(m_draft.id); };
        m_overlay->hide();

        setState(State());
    }

    void setOpenBatchCallbacks(std::function<void()> draftBatch,
                               std::function<void()> videoBatch)
    {
        m_openDraftBatch = std::move(draftBatch);
        m_openVideoBatch = std::move(videoBatch);
    }

    // 同一批成片共享页头的批次时间；时间戳为 0 时隐藏标记。
    void setGeneratedAt(qint64 timestampMs)
    {
        const QString label = formatGenerationTime(timestampMs);
        m_generatedAtValue->setText(label);
        m_generatedAtValue->setTextColor(isFreshGeneration(label) ? Theme::Success
                                                                 : Theme::Text1);
        m_generatedAtBar->setToolTip(
            QStringLiteral("生成时间：%1").arg(formatGenerationFullTime(timestampMs)));
        m_generatedAtHost->setVisible(!label.isEmpty());
    }

    void setState(const State &state)
    {
        m_state = state;
        const DraftDownloadJob *draftJob = state.draftJob;
        const VideoExportJob *videoJob = state.videoJob;

        // 缩略图徽标：先报错，再报进行中，最后按完成情况显示。
        QString badge = QStringLiteral("待导出");
        MediaThumbnail::BadgeTone tone = MediaThumbnail::BadgeNeutral;
        if (state.anyError) {
            badge = QStringLiteral("导出失败");
            tone = MediaThumbnail::BadgeError;
        } else if (videoJob && videoJob->status == VideoExportStatus::Running) {
            badge = QStringLiteral("成片导出中");
            tone = MediaThumbnail::BadgeRun;
        } else if (draftJob && draftJob->isRunning()) {
            badge = QStringLiteral("导出草稿中");
            tone = MediaThumbnail::BadgeRun;
        } else if (videoJob && videoJob->status == VideoExportStatus::Done
                   && draftJob && draftJob->status == DraftDownloadStatus::Done) {
            badge = QStringLiteral("已导出");
            tone = MediaThumbnail::BadgeOk;
        } else if (videoJob && videoJob->status == VideoExportStatus::Done) {
            badge = QStringLiteral("成片已导出");
            tone = MediaThumbnail::BadgeOk;
        } else if (draftJob && draftJob->status == DraftDownloadStatus::Done) {
            badge = QStringLiteral("已下载到本地");
            tone = MediaThumbnail::BadgeOk;
        } else if (state.draftDownloaded) {
            badge = QStringLiteral("已下载到本地");
            tone = MediaThumbnail::BadgeOk;
        } else if (state.draftBatch
                   && state.draftBatch->status == DraftBatchItemStatus::Queued) {
            badge = QStringLiteral("草稿排队中");
            tone = MediaThumbnail::BadgeRun;
        } else if (state.videoBatch
                   && state.videoBatch->status == VideoBatchItemStatus::Queued) {
            badge = QStringLiteral("成片排队中");
            tone = MediaThumbnail::BadgeRun;
        }
        m_thumb->setReadyBadge(badge);
        m_thumb->setReadyBadgeTone(tone);

        // 两个动作按钮。
        updateDraftButton();
        updateVideoButton();

        m_buttonRow->setVisible(!state.selecting);
        m_selectionStatus->setVisible(state.selecting);
        if (state.selecting) {
            updateSelectionStatus();
        }

        m_overlay->setVisible(state.selecting);
        m_overlay->setState(state.selectable, state.selected, state.anyRunning,
                            state.bothDone);
        m_overlay->setCursor(state.selectable ? Qt::PointingHandCursor
                                              : Qt::ArrowCursor);
        if (state.selecting) {
            m_overlay->raise();
        }

        if (state.selecting && state.selected && state.selectable) {
            setCardColors(Theme::Surface1, Theme::alpha(Theme::Primary, 0.70));
            setHoverColors(Theme::Surface1, Theme::alpha(Theme::Primary, 0.70));
        } else {
            setCardColors(Theme::Surface1, Theme::Border);
            setHoverColors(Theme::Surface1,
                           state.selecting && state.selectable
                               ? Theme::alpha(Theme::Primary, 0.45)
                               : Theme::BorderStrong);
        }
        setHoverEnabled(!state.selecting || state.selectable);
    }

    QSize sizeHint() const override
    {
        return QSize(0, QWidget::sizeHint().height());
    }

protected:
    void resizeEvent(QResizeEvent *event) override
    {
        Card::resizeEvent(event);
        if (m_overlay) {
            m_overlay->setGeometry(rect());
        }
    }

private:
    void updateDraftButton()
    {
        const DraftDownloadJob *job = m_state.draftJob;
        const DraftBatchItem *batchItem = m_state.draftBatch;
        const int percent =
            job ? qMin(99, int(qBound(0.0, job->progress, 100.0))) : 0;
        const bool done = (job && job->status == DraftDownloadStatus::Done)
            || m_state.draftDownloaded;

        QString tooltip;
        if (batchItem && batchItem->status != DraftBatchItemStatus::Done) {
            tooltip = QStringLiteral("查看批量导出进度");
        } else if (done) {
            tooltip = FileManager::openDraftFolderHint(true);
        } else {
            tooltip = QStringLiteral("下载草稿与素材到剪映草稿目录，可继续二次编辑");
        }
        if (m_draftButton->toolTip() != tooltip) {
            m_draftButton->setToolTip(tooltip);
        }

        if (batchItem && batchItem->status == DraftBatchItemStatus::Queued) {
            setButtonState(m_draftButton, QStringLiteral("批量排队中"), Button::Primary,
                           Icons::Glyph::Clock, -1, QColor());
            return;
        }
        if (batchItem && batchItem->status == DraftBatchItemStatus::Error) {
            setButtonState(m_draftButton, QStringLiteral("批量中失败"), Button::Danger,
                           Icons::Glyph::Alert, -1, QColor());
            return;
        }
        if (job && job->status == DraftDownloadStatus::Downloading) {
            setButtonState(m_draftButton,
                           QStringLiteral("准备中 %1%").arg(percent), Button::Primary,
                           Icons::Glyph::Download, percent, QColor());
            return;
        }
        if (job && job->status == DraftDownloadStatus::Writing) {
            setButtonState(m_draftButton,
                           QStringLiteral("写入草稿 %1%").arg(percent), Button::Primary,
                           Icons::Glyph::Download, percent, QColor());
            return;
        }
        if (job && job->status == DraftDownloadStatus::Error) {
            setButtonState(m_draftButton, QStringLiteral("下载失败 · 重试"),
                           Button::Danger, Icons::Glyph::Alert, -1, QColor());
            return;
        }
        if (done) {
            setButtonState(m_draftButton, FileManager::openDraftFolderText(),
                           Button::Primary, Icons::Glyph::Check, -1, QColor());
            return;
        }
        setButtonState(m_draftButton, QStringLiteral("导出到剪映"), Button::Primary,
                       Icons::Glyph::Download, -1, QColor());
    }

    void updateVideoButton()
    {
        const VideoExportJob *job = m_state.videoJob;
        const VideoBatchItem *batchItem = m_state.videoBatch;
        const int percent =
            job ? qMin(99, int(qBound(0.0, job->progress, 100.0))) : 0;

        if (batchItem && batchItem->status == VideoBatchItemStatus::Queued) {
            setButtonState(m_videoButton, QStringLiteral("批量排队中"), Button::Default,
                           Icons::Glyph::Clock, -1, QColor());
            return;
        }
        if (batchItem && batchItem->status == VideoBatchItemStatus::Error) {
            setButtonState(m_videoButton, QStringLiteral("批量中失败"), Button::Danger,
                           Icons::Glyph::Alert, -1, QColor());
            return;
        }
        if (job && job->status == VideoExportStatus::Running) {
            setButtonState(m_videoButton,
                           QStringLiteral("导出中 %1%").arg(percent), Button::Default,
                           Icons::Glyph::Film, percent, Theme::Primary);
            return;
        }
        if (job && job->status == VideoExportStatus::Error) {
            setButtonState(m_videoButton, QStringLiteral("导出失败 · 重试"),
                           Button::Danger, Icons::Glyph::Alert, -1, QColor());
            return;
        }
        if (job && job->status == VideoExportStatus::Done) {
            setButtonState(m_videoButton, FileManager::revealPathLabel(),
                           Button::Default, Icons::Glyph::Check, -1, Theme::Success);
            return;
        }
        setButtonState(m_videoButton, QStringLiteral("导出成片"), Button::Default,
                       Icons::Glyph::Film, -1, QColor());
    }

    void updateSelectionStatus()
    {
        const State &state = m_state;
        QString text;
        QColor color = Theme::Text3;

        if (!state.selectable) {
            if (state.anyRunning) {
                text = QStringLiteral("正在导出，完成后可再次选择");
                color = Theme::Primary;
            } else if (state.bothDone) {
                text = QStringLiteral("草稿与成片均已导出");
                color = Theme::Success;
            } else {
                text = QStringLiteral("暂不可选");
            }
        } else if (state.selected) {
            text = QStringLiteral("已选择 · 将加入批量导出");
            color = Theme::Primary;
        } else if (state.anyError) {
            text = QStringLiteral("上次导出失败 · 可重试");
            color = Theme::Danger;
        } else {
            const QString hint = state.draftEligible && state.videoEligible
                ? QStringLiteral("可导出到剪映 / 成片")
                : (state.draftEligible ? QStringLiteral("仅可导出到剪映")
                                       : QStringLiteral("仅可导出成片"));
            text = QStringLiteral("点击卡片选择 · %1").arg(hint);
        }
        if (m_selectionStatus->text() != text) {
            m_selectionStatus->setText(text);
            m_selectionStatus->setToolTip(text);
        }
        if (m_selectionColor != color) {
            m_selectionColor = color;
            m_selectionStatus->setTextColor(color);
        }
    }

    ClipDraft m_draft;
    State m_state;
    OpenCallback m_open;
    VideoCallback m_video;
    ToggleCallback m_toggle;
    std::function<void()> m_openDraftBatch;
    std::function<void()> m_openVideoBatch;

    MediaThumbnail *m_thumb = nullptr;
    QWidget *m_generatedAtHost = nullptr;
    QWidget *m_generatedAtBar = nullptr;
    ElidedLabel *m_generatedAtValue = nullptr;
    QWidget *m_buttonRow = nullptr;
    Button *m_draftButton = nullptr;
    Button *m_videoButton = nullptr;
    ElidedLabel *m_selectionStatus = nullptr;
    QColor m_selectionColor;
    CardSelectOverlay *m_overlay = nullptr;
};

// -----------------------------------------------------------------------------
// ResultsPage
// -----------------------------------------------------------------------------

ResultsPage::ResultsPage(QWidget *parent)
    : Page(parent)
{
    m_header = new PageHeader(
        QStringLiteral("AI 成片结果"),
        QStringLiteral("完成一键成片后，每条切片会在这里显示为一条剪映草稿。"), this);

    // 标题旁的生成时间标记：与卡片信息区顶部共用批次时间与文案。
    m_generationBadge = new QWidget(this);
    m_generationBadge->setObjectName(QStringLiteral("generationTimeBadge"));
    m_generationBadge->setAttribute(Qt::WA_StyledBackground, true);
    m_generationBadge->setStyleSheet(
        QStringLiteral("#generationTimeBadge{background:%1;border:1px solid %2;"
                       "border-radius:6px;}")
            .arg(Theme::Surface2.name(), Theme::Border.name()));
    m_generationBadge->setFixedHeight(22);
    m_generationBadge->setSizePolicy(QSizePolicy::Fixed, QSizePolicy::Fixed);
    auto *generationLayout = new QHBoxLayout(m_generationBadge);
    generationLayout->setContentsMargins(8, 0, 8, 0);
    generationLayout->setSpacing(6);
    generationLayout->addWidget(
        glyphLabel(Icons::Glyph::Clock, 12, Theme::Text3, m_generationBadge), 0,
        Qt::AlignVCenter);
    generationLayout->addWidget(
        Ui::label(QStringLiteral("生成时间"), 11, 400, Theme::Text2,
                  m_generationBadge),
        0, Qt::AlignVCenter);
    m_generationValue = Ui::label(QString(), 11, 400, Theme::Text1, m_generationBadge);
    m_generationValue->setFont(Theme::monoFont(11, QFont::Medium));
    generationLayout->addWidget(m_generationValue, 0, Qt::AlignVCenter);
    m_generationBadge->hide();
    m_header->setTitleMeta(m_generationBadge);

    m_batchButton = new Button(QStringLiteral("批量选择"), Button::Default, this);
    m_batchButton->setLeadingGlyph(Icons::Glyph::Batch);
    m_batchButton->setToolTip(
        QStringLiteral("选择多个草稿一次导入剪映或导出成片"));
    connect(m_batchButton, &QAbstractButton::clicked, this, [this] {
        if (m_selecting) {
            exitSelection();
            return;
        }
        if (m_hasDraftBatch && m_draftBatch.running) {
            emit openDraftBatchRequested();
            return;
        }
        if (m_hasVideoBatch && m_videoBatch.running) {
            emit openVideoBatchRequested();
            return;
        }
        enterSelection();
    });
    m_header->addAction(m_batchButton);

    auto *back = new Button(QStringLiteral("← 返回视频库"), Button::Default, this);
    connect(back, &QAbstractButton::clicked, this, &ResultsPage::backRequested);
    m_header->addAction(back);

    auto *restart = new Button(QStringLiteral("重新一键成片"), Button::Primary, this);
    restart->setLeadingGlyph(Icons::Glyph::Bolt);
    connect(restart, &QAbstractButton::clicked, this, &ResultsPage::restartRequested);
    m_header->addAction(restart);

    // 标题与操作固定，不随下方草稿卡片一起滚动。
    setFixedHeader(m_header);

    auto *emptyCard = new Card(Theme::Surface1, Theme::Border, 10, this);
    m_emptyState = emptyCard;
    auto *emptyLayout = emptyCard->contentLayout();
    emptyLayout->setContentsMargins(24, 38, 24, 38);
    emptyLayout->setSpacing(8);
    auto *emptyTitle = Ui::label(QStringLiteral("还没有剪映草稿"), 15, 600, Theme::Text1,
                                 m_emptyState);
    emptyTitle->setAlignment(Qt::AlignCenter);
    emptyLayout->addWidget(emptyTitle);
    auto *emptyHint = Ui::label(
        QStringLiteral("在视频库中点击「一键成片」，完成后草稿会显示在这里。"),
        12, 400, Theme::Text3, m_emptyState);
    emptyHint->setAlignment(Qt::AlignCenter);
    emptyHint->setWordWrap(true);
    emptyLayout->addWidget(emptyHint);
    bodyLayout()->addWidget(m_emptyState);

    // 结果页始终三等分排布，横向空间不足时由卡片内部自适应，不出现横向滚动条。
    scrollArea()->setHorizontalScrollBarPolicy(Qt::ScrollBarAlwaysOff);

    m_gridHost = new QWidget(this);
    m_gridHost->setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Preferred);
    m_grid = new QGridLayout(m_gridHost);
    m_grid->setContentsMargins(0, 0, 0, 0);
    m_grid->setHorizontalSpacing(14);
    m_grid->setVerticalSpacing(14);
    m_gridHost->setVisible(false);
    bodyLayout()->addWidget(m_gridHost);
    bodyLayout()->addStretch(1);

    buildSelectionBar();

    // 批量选择态：Esc 退出，⌘/Ctrl + A 全选。
    auto *escape = new QShortcut(QKeySequence(Qt::Key_Escape), this);
    escape->setContext(Qt::WidgetWithChildrenShortcut);
    connect(escape, &QShortcut::activated, this, [this] {
        if (m_selecting) {
            exitSelection();
        }
    });
    auto *selectAllShortcut = new QShortcut(QKeySequence::SelectAll, this);
    selectAllShortcut->setContext(Qt::WidgetWithChildrenShortcut);
    connect(selectAllShortcut, &QShortcut::activated, this, [this] {
        if (m_selecting) {
            selectAll();
        }
    });

    // 「刚刚」会过期、跨天文案会变化，相对标签每 10 秒刷新一次。
    auto *generationTimer = new QTimer(this);
    generationTimer->setInterval(10000);
    connect(generationTimer, &QTimer::timeout, this,
            &ResultsPage::updateGenerationTimes);
    generationTimer->start();

    updateHeader();
}

void ResultsPage::setDrafts(const QVector<ClipDraft> &drafts)
{
    m_drafts = drafts;
    m_cards.clear();
    m_draftDownloaded.clear();
    for (const ClipDraft &draft : drafts) {
        m_draftDownloaded.insert(
            draft.id,
            !draft.downloadedPath.isEmpty()
                && QFileInfo(draft.downloadedPath).isDir());
    }
    // 重新读取草稿（如后台下载完成）时，保留仍在列表中的选择，避免打断批量选择。
    if (!m_selecting) {
        m_selected.clear();
    } else {
        QSet<qint64> available;
        for (const ClipDraft &draft : drafts) {
            available.insert(draft.id);
        }
        for (auto it = m_selected.begin(); it != m_selected.end();) {
            if (!available.contains(*it)) {
                it = m_selected.erase(it);
            } else {
                ++it;
            }
        }
        if (drafts.isEmpty()) {
            m_selecting = false;
            if (m_selectionBar) {
                m_selectionBar->hide();
            }
        }
    }
    while (QLayoutItem *item = m_grid->takeAt(0)) {
        if (QWidget *widget = item->widget()) {
            widget->hide();
            widget->deleteLater();
        }
        delete item;
    }

    QVector<DraftCard *> cards;
    cards.reserve(drafts.size());
    for (int i = 0; i < drafts.size(); ++i) {
        const ClipDraft &draft = drafts.at(i);
        auto *card = new DraftCard(
            draft, i, m_gridHost,
            [this](const QString &text, const QString &label) {
                QGuiApplication::clipboard()->setText(text);
                emit toastRequested(QStringLiteral("已复制%1").arg(label));
            },
            [this](qint64 draftId) { emit openDraftRequested(draftId); },
            [this](qint64 draftId) { emit videoExportRequested(draftId); },
            [this](qint64 draftId) { toggleSelected(draftId); });
        card->setOpenBatchCallbacks(
            [this] { emit openDraftBatchRequested(); },
            [this] { emit openVideoBatchRequested(); });
        m_grid->addWidget(card, i / 3, i % 3);
        cards.append(card);
        m_cards.insert(draft.id, card);
    }

    // 卡片忽略内容宽度，三列拉伸因子相同，窗口变宽变窄时始终保持等宽三等分。
    for (DraftCard *card : cards) {
        card->setMinimumWidth(0);
        card->setSizePolicy(QSizePolicy::Ignored, QSizePolicy::Preferred);
    }

    for (int column = 0; column < 3; ++column) {
        m_grid->setColumnStretch(column, 1);
    }

    m_emptyState->setVisible(drafts.isEmpty());
    m_gridHost->setVisible(!drafts.isEmpty());
    refreshAllCards();
    updateHeader();
    updateGenerationTimes();
    updateSelectionBar();
}

void ResultsPage::setDownloadJob(const DraftDownloadJob &job)
{
    m_jobs.insert(job.draftRowId, job);
    refreshCard(job.draftRowId);
    updateSelectionBar();
}

void ResultsPage::removeDownloadJob(qint64 draftRowId)
{
    m_jobs.remove(draftRowId);
    refreshCard(draftRowId);
    updateSelectionBar();
}

void ResultsPage::setVideoJob(const VideoExportJob &job)
{
    m_videoJobs.insert(job.draftRowId, job);
    refreshCard(job.draftRowId);
    updateSelectionBar();
}

void ResultsPage::removeVideoJob(qint64 draftRowId)
{
    m_videoJobs.remove(draftRowId);
    refreshCard(draftRowId);
    updateSelectionBar();
}

void ResultsPage::setDraftBatch(const DraftBatchSnapshot &batch)
{
    m_draftBatch = batch;
    m_hasDraftBatch = true;
    refreshAllCards();
    updateHeader();
    updateSelectionBar();
}

void ResultsPage::clearDraftBatch()
{
    m_hasDraftBatch = false;
    m_draftBatch = DraftBatchSnapshot();
    refreshAllCards();
    updateHeader();
    updateSelectionBar();
}

void ResultsPage::setVideoBatch(const VideoBatchSnapshot &batch)
{
    m_videoBatch = batch;
    m_hasVideoBatch = true;
    refreshAllCards();
    updateHeader();
    updateSelectionBar();
}

void ResultsPage::clearVideoBatch()
{
    m_hasVideoBatch = false;
    m_videoBatch = VideoBatchSnapshot();
    refreshAllCards();
    updateHeader();
    updateSelectionBar();
}

bool ResultsPage::draftEligible(qint64 draftRowId) const
{
    if (m_jobs.contains(draftRowId)) {
        return m_jobs.value(draftRowId).status == DraftDownloadStatus::Error;
    }
    // 已经下载到本地的草稿不再参与批量导入，避免重复写入剪映目录。
    return !m_draftDownloaded.value(draftRowId, false);
}

bool ResultsPage::videoEligible(qint64 draftRowId) const
{
    if (!m_videoJobs.contains(draftRowId)) {
        return true;
    }
    return m_videoJobs.value(draftRowId).status == VideoExportStatus::Error;
}

bool ResultsPage::anyEligible(qint64 draftRowId) const
{
    return draftEligible(draftRowId) || videoEligible(draftRowId);
}

QVector<qint64> ResultsPage::eligibleFor(bool draftAction) const
{
    QVector<qint64> ids;
    for (const ClipDraft &draft : m_drafts) {
        if (draftAction ? draftEligible(draft.id) : videoEligible(draft.id)) {
            ids.append(draft.id);
        }
    }
    return ids;
}

QVector<qint64> ResultsPage::selectableIds() const
{
    QVector<qint64> ids;
    for (const ClipDraft &draft : m_drafts) {
        if (anyEligible(draft.id)) {
            ids.append(draft.id);
        }
    }
    return ids;
}

const DraftBatchItem *ResultsPage::draftBatchItem(qint64 draftRowId) const
{
    if (!m_hasDraftBatch || !m_draftBatch.running) {
        return nullptr;
    }
    for (const DraftBatchItem &item : m_draftBatch.items) {
        if (item.draftRowId == draftRowId) {
            return &item;
        }
    }
    return nullptr;
}

const VideoBatchItem *ResultsPage::videoBatchItem(qint64 draftRowId) const
{
    if (!m_hasVideoBatch || !m_videoBatch.running) {
        return nullptr;
    }
    for (const VideoBatchItem &item : m_videoBatch.items) {
        if (item.draftRowId == draftRowId) {
            return &item;
        }
    }
    return nullptr;
}

void ResultsPage::refreshCard(qint64 draftRowId)
{
    DraftCard *card = m_cards.value(draftRowId, nullptr);
    if (!card) {
        return;
    }

    DraftCard::State state;
    state.draftJob = m_jobs.contains(draftRowId) ? &m_jobs[draftRowId] : nullptr;
    state.videoJob =
        m_videoJobs.contains(draftRowId) ? &m_videoJobs[draftRowId] : nullptr;
    state.draftBatch = draftBatchItem(draftRowId);
    state.videoBatch = videoBatchItem(draftRowId);
    state.selecting = m_selecting;
    state.selected = m_selected.contains(draftRowId);
    state.draftEligible = draftEligible(draftRowId);
    state.videoEligible = videoEligible(draftRowId);
    state.selectable = state.draftEligible || state.videoEligible;
    state.draftDownloaded = m_draftDownloaded.value(draftRowId, false);
    state.anyRunning =
        (state.draftJob && state.draftJob->isRunning())
        || (state.videoJob && state.videoJob->status == VideoExportStatus::Running);
    const bool draftDone =
        state.draftDownloaded
        || (state.draftJob
            && state.draftJob->status == DraftDownloadStatus::Done);
    state.bothDone = draftDone && state.videoJob
        && state.videoJob->status == VideoExportStatus::Done;
    state.anyError =
        (state.draftJob && state.draftJob->status == DraftDownloadStatus::Error)
        || (state.videoJob && state.videoJob->status == VideoExportStatus::Error);
    card->setState(state);

    if (m_selecting && m_selected.contains(draftRowId) && !state.selectable) {
        m_selected.remove(draftRowId);
    }
}

void ResultsPage::refreshAllCards()
{
    for (const ClipDraft &draft : m_drafts) {
        refreshCard(draft.id);
    }
    pruneSelection();
}

void ResultsPage::pruneSelection()
{
    if (!m_selecting || m_selected.isEmpty()) {
        return;
    }
    for (auto it = m_selected.begin(); it != m_selected.end();) {
        if (!anyEligible(*it)) {
            it = m_selected.erase(it);
        } else {
            ++it;
        }
    }
}

void ResultsPage::updateHeader()
{
    if (!m_header) {
        return;
    }

    m_header->setDescription(
        m_drafts.isEmpty()
            ? QStringLiteral("完成一键成片后，每条切片会在这里显示为一条剪映草稿。")
            : QStringLiteral("已生成 %1 条剪映草稿；可批量导入剪映或批量导出成片，"
                             "单条导出约需 2–3 分钟。")
                  .arg(m_drafts.size()));

    if (!m_batchButton) {
        return;
    }

    if (m_selecting) {
        m_batchButton->setText(QStringLiteral("退出批量"));
        m_batchButton->setEnabled(true);
        m_batchButton->setToolTip(QStringLiteral("退出批量选择"));
    } else if (m_hasDraftBatch && m_draftBatch.running) {
        m_batchButton->setText(
            QStringLiteral("批量导出草稿 %1%")
                .arg(qMin(99, int(m_draftBatch.progress))));
        m_batchButton->setEnabled(true);
        m_batchButton->setToolTip(QStringLiteral("打开批量导出草稿进度"));
    } else if (m_hasVideoBatch && m_videoBatch.running) {
        m_batchButton->setText(
            QStringLiteral("批量导出成片 %1%")
                .arg(qMin(99, int(m_videoBatch.progress))));
        m_batchButton->setEnabled(true);
        m_batchButton->setToolTip(QStringLiteral("打开批量导出成片进度"));
    } else {
        m_batchButton->setText(QStringLiteral("批量选择"));
        const bool canSelect = !selectableIds().isEmpty();
        m_batchButton->setEnabled(canSelect);
        m_batchButton->setToolTip(
            canSelect ? QStringLiteral("选择多个草稿一次导入剪映或导出成片")
                      : QStringLiteral("所有草稿与成片均已完成或正在导出"));
    }
}

void ResultsPage::updateGenerationTimes()
{
    // 最新一条草稿的创建时刻即本批成片的生成时间。
    qint64 generatedAtMs = 0;
    for (const ClipDraft &draft : m_drafts) {
        generatedAtMs = qMax(generatedAtMs, draft.createdAtMs);
    }

    if (m_generationBadge && m_generationValue) {
        const QString label = formatGenerationTime(generatedAtMs);
        m_generationValue->setText(label);
        m_generationValue->setStyleSheet(generationValueStyle(label));
        m_generationBadge->setToolTip(
            QStringLiteral("生成时间：%1").arg(formatGenerationFullTime(generatedAtMs)));
        m_generationBadge->setVisible(!label.isEmpty());
    }

    for (DraftCard *card : m_cards) {
        card->setGeneratedAt(generatedAtMs);
    }
}

void ResultsPage::enterSelection()
{
    if (m_selecting) {
        return;
    }
    m_selecting = true;
    m_selected.clear();
    refreshAllCards();
    updateHeader();
    m_selectionBar->show();
    m_selectionBar->raise();
    positionSelectionBar();
    updateSelectionBar();
}

void ResultsPage::exitSelection()
{
    if (!m_selecting) {
        return;
    }
    m_selecting = false;
    m_selected.clear();
    if (m_selectionBar) {
        m_selectionBar->hide();
    }
    refreshAllCards();
    updateHeader();
}

void ResultsPage::toggleSelected(qint64 draftRowId)
{
    if (!m_selecting || !anyEligible(draftRowId)) {
        return;
    }
    if (m_selected.contains(draftRowId)) {
        m_selected.remove(draftRowId);
    } else {
        m_selected.insert(draftRowId);
    }
    refreshCard(draftRowId);
    updateSelectionBar();
}

void ResultsPage::selectAll()
{
    if (!m_selecting) {
        return;
    }
    m_selected.clear();
    for (qint64 id : selectableIds()) {
        m_selected.insert(id);
    }
    refreshAllCards();
    updateSelectionBar();
}

void ResultsPage::clearSelection()
{
    if (!m_selecting) {
        return;
    }
    m_selected.clear();
    refreshAllCards();
    updateSelectionBar();
}

void ResultsPage::startDraftBatch()
{
    if (!m_selecting) {
        return;
    }
    QVector<qint64> ids;
    for (qint64 id : m_selected) {
        if (draftEligible(id)) {
            ids.append(id);
        }
    }
    if (ids.isEmpty()) {
        return;
    }
    exitSelection();
    emit batchDraftRequested(ids);
}

void ResultsPage::startVideoBatch()
{
    if (!m_selecting) {
        return;
    }
    QVector<qint64> ids;
    for (qint64 id : m_selected) {
        if (videoEligible(id)) {
            ids.append(id);
        }
    }
    if (ids.isEmpty()) {
        return;
    }
    exitSelection();
    emit batchVideoRequested(ids);
}

void ResultsPage::buildSelectionBar()
{
    auto *bar = new Card(Theme::Surface2, Theme::BorderStrong, 10, this);
    bar->setObjectName(QStringLiteral("selectionBar"));
    bar->setFixedWidth(900);
    bar->contentLayout()->setContentsMargins(14, 10, 14, 10);
    auto *barLayout = new QHBoxLayout;
    barLayout->setContentsMargins(0, 0, 0, 0);
    barLayout->setSpacing(12);
    bar->contentLayout()->addLayout(barLayout);

    auto *iconHost = new QWidget(bar);
    iconHost->setFixedSize(30, 30);
    iconHost->setStyleSheet(QStringLiteral(
        "background:rgba(110,139,255,0.14);border:1px solid rgba(110,139,255,0.25);"
        "border-radius:7px;"));
    auto *iconLayout = new QVBoxLayout(iconHost);
    iconLayout->setContentsMargins(0, 0, 0, 0);
    QPixmap batchPixmap(14, 14);
    batchPixmap.fill(Qt::transparent);
    {
        QPainter painter(&batchPixmap);
        painter.setRenderHint(QPainter::Antialiasing, true);
        Icons::paint(&painter, QRectF(0, 0, 14, 14), Icons::Glyph::Batch,
                     Theme::Primary, 2.0);
    }
    auto *iconLabel = new QLabel(iconHost);
    iconLabel->setPixmap(batchPixmap);
    iconLabel->setAttribute(Qt::WA_TransparentForMouseEvents, true);
    iconLayout->addWidget(iconLabel, 0, Qt::AlignCenter);
    barLayout->addWidget(iconHost, 0, Qt::AlignVCenter);

    auto *textColumn = new QWidget(bar);
    auto *textLayout = new QVBoxLayout(textColumn);
    textLayout->setContentsMargins(0, 0, 0, 0);
    textLayout->setSpacing(1);
    m_selectionTitle = Ui::label(QStringLiteral("选择要批量导出的草稿"), 12, 500,
                                 Theme::Text1, textColumn);
    textLayout->addWidget(m_selectionTitle);
    m_selectionMeta = Ui::label(QString(), 10, 400, Theme::Text3, textColumn);
    m_selectionMeta->setFont(Theme::monoFont(10));
    textLayout->addWidget(m_selectionMeta);
    barLayout->addWidget(textColumn, 1);

    auto *selectAllLink = new LinkLabel(QStringLiteral("全选"), bar);
    selectAllLink->setPixelSize(11);
    selectAllLink->setTextColor(Theme::Text2);
    selectAllLink->setHoverColor(Theme::Text1);
    connect(selectAllLink, &QAbstractButton::clicked, this, &ResultsPage::selectAll);
    barLayout->addWidget(selectAllLink, 0, Qt::AlignVCenter);

    auto *clearLink = new LinkLabel(QStringLiteral("清空"), bar);
    clearLink->setPixelSize(11);
    clearLink->setTextColor(Theme::Text2);
    clearLink->setHoverColor(Theme::Text1);
    connect(clearLink, &QAbstractButton::clicked, this, &ResultsPage::clearSelection);
    barLayout->addWidget(clearLink, 0, Qt::AlignVCenter);

    auto *separator = Ui::separator(true, bar);
    separator->setFixedHeight(16);
    barLayout->addWidget(separator, 0, Qt::AlignVCenter);

    auto *cancel = new Button(QStringLiteral("取消"), Button::Ghost, bar);
    cancel->setCompact(true);
    connect(cancel, &QAbstractButton::clicked, this, &ResultsPage::exitSelection);
    barLayout->addWidget(cancel, 0, Qt::AlignVCenter);

    m_exportVideoButton = new Button(QStringLiteral("导出成片"), Button::Default, bar);
    m_exportVideoButton->setCompact(true);
    m_exportVideoButton->setLeadingGlyph(Icons::Glyph::Film);
    connect(m_exportVideoButton, &QAbstractButton::clicked, this,
            &ResultsPage::startVideoBatch);
    barLayout->addWidget(m_exportVideoButton, 0, Qt::AlignVCenter);

    m_exportDraftButton = new Button(QStringLiteral("导出到剪映"), Button::Primary, bar);
    m_exportDraftButton->setCompact(true);
    m_exportDraftButton->setLeadingGlyph(Icons::Glyph::Download);
    connect(m_exportDraftButton, &QAbstractButton::clicked, this,
            &ResultsPage::startDraftBatch);
    barLayout->addWidget(m_exportDraftButton, 0, Qt::AlignVCenter);

    auto *shadow = new QGraphicsDropShadowEffect(bar);
    shadow->setBlurRadius(48);
    shadow->setOffset(0, 18);
    shadow->setColor(QColor(0, 0, 0, 140));
    bar->setGraphicsEffect(shadow);

    bar->hide();
    m_selectionBar = bar;
}

void ResultsPage::positionSelectionBar()
{
    if (!m_selectionBar) {
        return;
    }
    const int barWidth = qMin(900, qMax(320, width() - 44));
    m_selectionBar->setFixedWidth(barWidth);
    const int barHeight = m_selectionBar->sizeHint().height();
    m_selectionBar->setGeometry((width() - barWidth) / 2,
                                height() - barHeight - 10, barWidth, barHeight);
}

void ResultsPage::updateSelectionBar()
{
    pruneSelection();

    const int total = m_drafts.size();
    const int selectable = selectableIds().size();
    const int nonSelectable = total - selectable;
    const int selectedCount = m_selected.size();

    int selectedDraftCount = 0;
    int selectedVideoCount = 0;
    for (qint64 id : m_selected) {
        if (draftEligible(id)) {
            ++selectedDraftCount;
        }
        if (videoEligible(id)) {
            ++selectedVideoCount;
        }
    }

    if (m_selectionTitle) {
        m_selectionTitle->setText(
            selectedCount > 0
                ? QStringLiteral("已选 %1 个草稿").arg(selectedCount)
                : QStringLiteral("选择要批量导出的草稿"));
    }
    if (m_selectionMeta) {
        QString meta = QStringLiteral("共 %1 个草稿").arg(total);
        meta += nonSelectable > 0
            ? QStringLiteral(" · %1 个可批量导出 · %2 个已完成或导出中")
                  .arg(selectable)
                  .arg(nonSelectable)
            : QStringLiteral(" · 全部可批量导出");
        meta += QStringLiteral(" · Esc 退出");
        m_selectionMeta->setText(meta);
        m_selectionMeta->setToolTip(meta);
    }
    if (m_exportDraftButton) {
        m_exportDraftButton->setText(
            selectedDraftCount > 0
                ? QStringLiteral("导出到剪映（%1）").arg(selectedDraftCount)
                : QStringLiteral("导出到剪映"));
        m_exportDraftButton->setEnabled(selectedDraftCount > 0);
    }
    if (m_exportVideoButton) {
        m_exportVideoButton->setText(
            selectedVideoCount > 0
                ? QStringLiteral("导出成片（%1）").arg(selectedVideoCount)
                : QStringLiteral("导出成片"));
        m_exportVideoButton->setEnabled(selectedVideoCount > 0);
    }

    if (m_selectionBar && m_selectionBar->isVisible()) {
        positionSelectionBar();
        m_selectionBar->raise();
    }
}

void ResultsPage::resizeEvent(QResizeEvent *event)
{
    Page::resizeEvent(event);
    positionSelectionBar();
}
