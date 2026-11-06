// 文件作用：成片结果页实现，卡片内容全部来自 SQLite 中的草稿记录。
#include "ResultsPage.h"

#include "../core/FileManager.h"
#include "../core/MediaUtils.h"
#include "../ui/Controls.h"
#include "../ui/Icons.h"

#include <QClipboard>
#include <QGridLayout>
#include <QGuiApplication>
#include <QHBoxLayout>
#include <QLabel>
#include <QLayout>
#include <QScrollArea>
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

} // namespace

class DraftCard : public Card
{
public:
    using CopyCallback = std::function<void(const QString &text, const QString &label)>;
    using OpenCallback = std::function<void(qint64 draftId)>;

    DraftCard(const ClipDraft &draft, int index, QWidget *parent, CopyCallback copy,
              OpenCallback open)
        : Card(Theme::Surface1, Theme::Border, 10, parent)
        , m_downloaded(!draft.downloadedPath.isEmpty())
    {
        setHoverColors(Theme::Surface1, Theme::BorderStrong);
        setHoverEnabled(true);
        contentLayout()->setContentsMargins(1, 1, 1, 1);
        contentLayout()->setSpacing(0);

        m_thumb = new MediaThumbnail(MediaThumbnail::Cool, this);
        m_thumb->setTopRadius(9);
        m_thumb->setReadyBadge(m_downloaded ? QStringLiteral("已下载到本地")
                                            : QStringLiteral("草稿已就绪"));
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

        m_openButton = new Button(FileManager::revealLabel(), Button::Primary, body);
        connect(m_openButton, &QAbstractButton::clicked, this, [open, id = draft.id] {
            open(id);
        });
        layout->addWidget(m_openButton);

        contentLayout()->addWidget(body);
        setDownloadState(nullptr);
    }

    // 根据下载任务快照刷新徽标与按钮：准备中/写入草稿带进度，失败可重试。
    void setDownloadState(const DraftDownloadJob *job)
    {
        if (!m_openButton || !m_thumb) {
            return;
        }

        QString badge;
        MediaThumbnail::BadgeTone tone = MediaThumbnail::BadgeOk;
        const int shown = job ? qMin(99, int(qBound(0.0, job->progress, 100.0))) : 0;

        if (!job) {
            badge = m_downloaded ? QStringLiteral("已下载到本地")
                                 : QStringLiteral("草稿已就绪");
            m_openButton->setText(FileManager::revealLabel());
            m_openButton->setVariant(Button::Primary);
            m_openButton->setProgressPercent(-1);
        } else {
            switch (job->status) {
            case DraftDownloadStatus::Downloading:
                badge = QStringLiteral("准备中");
                tone = MediaThumbnail::BadgeRun;
                m_openButton->setText(QStringLiteral("准备中 %1%").arg(shown));
                m_openButton->setVariant(Button::Primary);
                m_openButton->setProgressPercent(shown);
                break;
            case DraftDownloadStatus::Writing:
                badge = QStringLiteral("正在写入草稿");
                tone = MediaThumbnail::BadgeRun;
                m_openButton->setText(QStringLiteral("写入草稿 %1%").arg(shown));
                m_openButton->setVariant(Button::Primary);
                m_openButton->setProgressPercent(shown);
                break;
            case DraftDownloadStatus::Done:
                badge = QStringLiteral("已下载到本地");
                m_openButton->setText(FileManager::revealLabel());
                m_openButton->setVariant(Button::Primary);
                m_openButton->setProgressPercent(-1);
                break;
            case DraftDownloadStatus::Error:
                badge = QStringLiteral("下载失败");
                tone = MediaThumbnail::BadgeError;
                m_openButton->setText(QStringLiteral("下载失败 · 重试"));
                m_openButton->setVariant(Button::Danger);
                m_openButton->setProgressPercent(-1);
                break;
            }
        }

        m_thumb->setReadyBadge(badge);
        m_thumb->setReadyBadgeTone(tone);
    }

    // 宽度交给网格的三等分列均分；若沿用内容 sizeHint，标签多的卡片会撑宽整列。
    QSize sizeHint() const override
    {
        return QSize(0, QWidget::sizeHint().height());
    }

private:
    MediaThumbnail *m_thumb = nullptr;
    Button *m_openButton = nullptr;
    bool m_downloaded = false;
};

ResultsPage::ResultsPage(QWidget *parent)
    : Page(parent)
{
    m_header = new PageHeader(
        QStringLiteral("AI 成片结果"),
        QStringLiteral("完成一键成片后，每条切片会在这里显示为一条剪映草稿。"), this);

    auto *back = new Button(QStringLiteral("← 返回视频库"), Button::Default, this);
    connect(back, &QAbstractButton::clicked, this, &ResultsPage::backRequested);
    m_header->addAction(back);

    auto *restart = new Button(QStringLiteral("重新一键成片"), Button::Primary, this);
    restart->setLeadingGlyph(Icons::Glyph::Bolt);
    connect(restart, &QAbstractButton::clicked, this, &ResultsPage::restartRequested);
    m_header->addAction(restart);

    // 标题与「重新一键成片」固定，不随下方草稿卡片一起滚动。
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
}

void ResultsPage::setDrafts(const QVector<ClipDraft> &drafts)
{
    m_cards.clear();
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
            [this](qint64 draftId) { emit openDraftRequested(draftId); });
        m_grid->addWidget(card, i / 3, i % 3);
        cards.append(card);
        m_cards.insert(draft.id, card);
        // 重新进入结果页时恢复仍在进行或已失败的下载状态。
        if (m_jobs.contains(draft.id)) {
            card->setDownloadState(&m_jobs[draft.id]);
        }
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
    if (m_header) {
        m_header->setDescription(
            drafts.isEmpty()
                ? QStringLiteral("完成一键成片后，每条切片会在这里显示为一条剪映草稿。")
                : QStringLiteral("已生成 %1 条剪映草稿，打开时自动下载到本地剪映草稿目录。")
                      .arg(drafts.size()));
    }
}

void ResultsPage::setDownloadJob(const DraftDownloadJob &job)
{
    m_jobs.insert(job.draftRowId, job);
    if (DraftCard *card = m_cards.value(job.draftRowId, nullptr)) {
        card->setDownloadState(&job);
    }
}

void ResultsPage::removeDownloadJob(qint64 draftRowId)
{
    m_jobs.remove(draftRowId);
    if (DraftCard *card = m_cards.value(draftRowId, nullptr)) {
        card->setDownloadState(nullptr);
    }
}
