// 文件作用：批量导出弹窗实现。
#include "BatchExportDialog.h"

#include "../core/FileManager.h"
#include "../core/MediaUtils.h"
#include "Controls.h"
#include "Icons.h"

#include <QClipboard>
#include <QFrame>
#include <QGuiApplication>
#include <QHBoxLayout>
#include <QLabel>
#include <QPainter>
#include <QScrollArea>
#include <QTimer>
#include <QVBoxLayout>

namespace {

QString cssColor(const QColor &color)
{
    return QStringLiteral("rgba(%1,%2,%3,%4)")
        .arg(color.red())
        .arg(color.green())
        .arg(color.blue())
        .arg(color.alpha());
}

QString formatElapsed(qint64 milliseconds)
{
    const qint64 totalSeconds = qMax<qint64>(0, milliseconds) / 1000;
    return QStringLiteral("%1:%2")
        .arg(totalSeconds / 60, 2, 10, QLatin1Char('0'))
        .arg(totalSeconds % 60, 2, 10, QLatin1Char('0'));
}

QString formatEta(int seconds)
{
    if (seconds < 0) {
        return QStringLiteral("—");
    }
    if (seconds == 0) {
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

void setLabelTextIfChanged(QLabel *label, const QString &text)
{
    if (label && label->text() != text) {
        label->setText(text);
    }
}

class IconBadge : public QWidget
{
public:
    IconBadge(Icons::Glyph glyph, const QColor &color, const QColor &background,
              int side, QWidget *parent = nullptr)
        : QWidget(parent)
        , m_glyph(glyph)
        , m_color(color)
        , m_background(background)
    {
        setFixedSize(side, side);
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        if (m_background.alpha() > 0) {
            painter.setPen(Qt::NoPen);
            painter.setBrush(m_background);
            painter.drawEllipse(QRectF(rect()));
        }
        const qreal glyphSize = width() * 0.46;
        const QRectF glyphRect((width() - glyphSize) / 2.0,
                               (height() - glyphSize) / 2.0, glyphSize, glyphSize);
        Icons::paint(&painter, glyphRect, m_glyph, m_color, 2.0);
    }

private:
    Icons::Glyph m_glyph;
    QColor m_color;
    QColor m_background;
};

// 行尾状态：图标 + 文案；进行中显示旋转的加载圈。
class StatusCell : public QWidget
{
public:
    enum class Kind { Queued, Active, Done, Error };

    explicit StatusCell(int width, QWidget *parent = nullptr)
        : QWidget(parent)
    {
        setFixedWidth(width);
        setFixedHeight(16);
        m_timer = new QTimer(this);
        m_timer->setInterval(60);
        connect(m_timer, &QTimer::timeout, this, [this] {
            m_angle = (m_angle + 30) % 360;
            update();
        });
    }

    void setStatus(Kind kind, const QString &text, const QColor &color)
    {
        m_kind = kind;
        m_text = text;
        m_color = color;
        if (kind == Kind::Active) {
            m_timer->start();
        } else {
            m_timer->stop();
        }
        update();
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        painter.setRenderHint(QPainter::TextAntialiasing, true);

        const QRectF iconRect(0, (height() - 12) / 2.0, 12, 12);
        if (m_kind == Kind::Active) {
            const qreal side = 11.0;
            const QRectF box((12 - side) / 2.0, (height() - side) / 2.0, side, side);
            painter.setPen(QPen(Theme::alpha(m_color, 0.25), 1.5));
            painter.drawEllipse(box);
            painter.setPen(QPen(m_color, 1.5, Qt::SolidLine, Qt::RoundCap));
            painter.drawArc(box, int((90 - m_angle) * 16), 100 * 16);
        } else {
            Icons::Glyph glyph = Icons::Glyph::Clock;
            if (m_kind == Kind::Done) {
                glyph = Icons::Glyph::Check;
            } else if (m_kind == Kind::Error) {
                glyph = Icons::Glyph::Alert;
            }
            Icons::paint(&painter, iconRect, glyph, m_color, 1.6);
        }

        painter.setFont(Theme::font(10));
        painter.setPen(m_color);
        painter.drawText(QRectF(15, 0, width() - 15, height()),
                         Qt::AlignRight | Qt::AlignVCenter, m_text);
    }

private:
    Kind m_kind = Kind::Queued;
    QString m_text;
    QColor m_color = Theme::Text3;
    QTimer *m_timer = nullptr;
    int m_angle = 0;
};

} // namespace

// -----------------------------------------------------------------------------
// BatchRow
// -----------------------------------------------------------------------------

class BatchRow : public QWidget
{
public:
    BatchRow(bool video, QWidget *parent = nullptr)
        : QWidget(parent)
        , m_video(video)
    {
        setFixedHeight(46);
        auto *root = new QVBoxLayout(this);
        root->setContentsMargins(12, 4, 12, 5);
        root->setSpacing(4);

        auto *top = new QHBoxLayout;
        top->setContentsMargins(0, 0, 0, 0);
        top->setSpacing(10);
        m_order = new QLabel(this);
        m_order->setFixedSize(18, 18);
        m_order->setAlignment(Qt::AlignCenter);
        m_order->setFont(Theme::monoFont(9));
        top->addWidget(m_order, 0, Qt::AlignVCenter);

        m_title = new ElidedLabel(QString(), this);
        m_title->setPixelSize(11);
        m_title->setTextColor(Theme::Text1);
        top->addWidget(m_title, 1);

        m_duration = new QLabel(this);
        m_duration->setFont(Theme::monoFont(10));
        m_duration->setStyleSheet(QStringLiteral("color:#62697E;background:transparent;"));
        top->addWidget(m_duration, 0, Qt::AlignVCenter);

        m_status = new StatusCell(video ? 82 : 64, this);
        top->addWidget(m_status, 0, Qt::AlignVCenter);
        root->addLayout(top);

        auto *bottom = new QHBoxLayout;
        bottom->setContentsMargins(28, 0, 0, 0);
        bottom->setSpacing(8);
        m_bar = new ProgressBar(this);
        m_bar->setFixedHeight(3);
        bottom->addWidget(m_bar, 1);
        m_bytes = new QLabel(this);
        m_bytes->setFixedWidth(124);
        m_bytes->setAlignment(Qt::AlignRight | Qt::AlignVCenter);
        m_bytes->setFont(Theme::monoFont(9));
        m_bytes->setStyleSheet(QStringLiteral("color:#62697E;background:transparent;"));
        bottom->addWidget(m_bytes, 0, Qt::AlignVCenter);
        root->addLayout(bottom);
    }

    void setItem(int order, const BatchExportDialog::ViewItem &item,
                 const QString &error)
    {
        m_order->setText(QStringLiteral("%1").arg(order, 2, 10, QLatin1Char('0')));
        m_title->setText(item.title);
        m_title->setToolTip(item.error.isEmpty() ? error : item.error);
        m_duration->setText(item.durationText);

        const bool done = item.status == 2;
        const bool failed = item.status == 3;
        const bool queued = item.status == 0;
        const bool active = item.status == 1;

        QColor badgeColor = Theme::Text3;
        QColor badgeFill = Theme::Surface3;
        if (done) {
            badgeColor = Theme::Success;
            badgeFill = Theme::alpha(Theme::Success, 0.13);
        } else if (failed) {
            badgeColor = Theme::Danger;
            badgeFill = Theme::alpha(Theme::Danger, 0.16);
        } else if (active) {
            badgeColor = Theme::Primary;
            badgeFill = Theme::alpha(Theme::Primary, 0.14);
        }
        m_order->setStyleSheet(
            QStringLiteral("color:%1;background:%2;border-radius:4px;")
                .arg(badgeColor.name(), cssColor(badgeFill)));

        const int percent = qMin(100, int(item.progress));
        QString statusText;
        StatusCell::Kind kind = StatusCell::Kind::Queued;
        QColor statusColor = Theme::Text3;
        if (done) {
            statusText = m_video ? QStringLiteral("已导出") : QStringLiteral("已就绪");
            kind = StatusCell::Kind::Done;
            statusColor = Theme::Success;
        } else if (failed) {
            statusText = QStringLiteral("失败");
            kind = StatusCell::Kind::Error;
            statusColor = Theme::Danger;
        } else if (active) {
            kind = StatusCell::Kind::Active;
            statusColor = Theme::Primary;
            if (m_video) {
                const QString stage = item.stage == 2 ? QStringLiteral("下载")
                    : (item.stage == 1 ? QStringLiteral("编码")
                                       : QStringLiteral("合成"));
                statusText = QStringLiteral("%1中 %2%").arg(stage).arg(percent);
            } else {
                statusText = item.hasStage && item.stage == 2
                    ? QStringLiteral("写入中")
                    : QStringLiteral("%1%").arg(percent);
            }
        } else {
            statusText = QStringLiteral("排队中");
        }
        m_status->setStatus(kind, statusText, statusColor);

        const int barValue = done ? 100 : (queued ? 0 : qMin(100, int(item.progress)));
        m_bar->setValue(barValue);
        m_bar->setBarColor(failed ? Theme::Danger
                                  : (done ? Theme::Success : Theme::Primary));

        QString bytes;
        if (!m_video) {
            bytes = queued
                ? MediaUtils::formatByteSize(item.totalBytes)
                : QStringLiteral("%1 / %2").arg(
                      MediaUtils::formatByteSize(item.loadedBytes),
                      MediaUtils::formatByteSize(item.totalBytes));
        } else {
            const bool showDownload = item.hasStage && item.stage == 2
                && (active || failed);
            bytes = showDownload
                ? QStringLiteral("%1 / %2").arg(
                      MediaUtils::formatByteSize(item.loadedBytes),
                      MediaUtils::formatByteSize(item.totalBytes))
                : MediaUtils::formatByteSize(item.totalBytes);
        }
        m_bytes->setText(bytes);
        update();
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        if (m_activeBackground.alpha() > 0) {
            painter.fillRect(rect(), m_activeBackground);
        }
        painter.setPen(QPen(Theme::alpha(Theme::Border, 0.7), 1.0));
        painter.drawLine(0, height() - 1, width(), height() - 1);
    }

public:
    void setAccent(int status)
    {
        if (status == 1) {
            m_activeBackground = Theme::alpha(Theme::Primary, 0.06);
        } else if (status == 3) {
            m_activeBackground = Theme::alpha(Theme::Danger, 0.08);
        } else {
            m_activeBackground = Qt::transparent;
        }
        update();
    }

private:
    bool m_video = false;
    QColor m_activeBackground;
    QLabel *m_order = nullptr;
    ElidedLabel *m_title = nullptr;
    QLabel *m_duration = nullptr;
    StatusCell *m_status = nullptr;
    ProgressBar *m_bar = nullptr;
    QLabel *m_bytes = nullptr;
};

// -----------------------------------------------------------------------------
// BatchExportDialog
// -----------------------------------------------------------------------------

BatchExportDialog::BatchExportDialog(Mode mode, const QString &folderPath,
                                     QWidget *parent)
    : ModalCard(mode == Mode::Draft ? QStringLiteral("批量导出到剪映")
                                    : QStringLiteral("批量导出成片"),
                720, parent)
    , m_mode(mode)
    , m_folderPath(folderPath)
{
    setContentSpacing(0);
    rebuildContent();
    rebuildFooter();
}

void BatchExportDialog::setDraftBatch(const DraftBatchSnapshot &batch)
{
    if (m_mode != Mode::Draft) {
        return;
    }

    QVector<ViewItem> items;
    items.reserve(batch.items.size());
    for (const DraftBatchItem &item : batch.items) {
        ViewItem view;
        view.title = item.title;
        view.durationText = item.durationText;
        view.progress = item.progress;
        view.loadedBytes = item.loadedBytes;
        view.totalBytes = item.totalBytes;
        view.hasStage = true;
        view.stage = 2;
        view.error = item.error;
        switch (item.status) {
        case DraftBatchItemStatus::Queued:
            view.status = 0;
            view.stage = 0;
            break;
        case DraftBatchItemStatus::Downloading:
            view.status = 1;
            view.stage = 1;
            break;
        case DraftBatchItemStatus::Writing:
            view.status = 1;
            view.stage = 2;
            break;
        case DraftBatchItemStatus::Done:
            view.status = 2;
            break;
        case DraftBatchItemStatus::Error:
            view.status = 3;
            break;
        }
        items.append(view);
    }

    const bool structural = m_batchId != batch.id || m_running != batch.running
        || itemCount() != items.size();
    m_batchId = batch.id;
    m_running = batch.running;
    m_doneCount = batch.doneCount;
    m_failedCount = batch.failedCount;
    m_activeCount = batch.activeCount;
    m_queuedCount = batch.queuedCount;
    m_progress = batch.progress;
    m_loadedBytes = batch.loadedBytes;
    m_totalBytes = batch.totalBytes;
    m_elapsedMs = batch.elapsedMs;
    m_etaSeconds = batch.etaSeconds;
    m_items = items;

    if (structural) {
        rebuildContent();
        rebuildFooter();
        emit layoutChanged();
    } else {
        refresh();
    }
}

void BatchExportDialog::setVideoBatch(const VideoBatchSnapshot &batch)
{
    if (m_mode != Mode::Video) {
        return;
    }

    QVector<ViewItem> items;
    items.reserve(batch.items.size());
    for (const VideoBatchItem &item : batch.items) {
        ViewItem view;
        view.title = item.title;
        view.durationText = item.durationText;
        view.progress = item.progress;
        view.loadedBytes = item.downloadedBytes;
        view.totalBytes = item.fileSizeBytes;
        view.stage = int(item.stage);
        view.hasStage = item.hasStage;
        view.error = item.error;
        switch (item.status) {
        case VideoBatchItemStatus::Queued:
            view.status = 0;
            break;
        case VideoBatchItemStatus::Running:
            view.status = 1;
            break;
        case VideoBatchItemStatus::Done:
            view.status = 2;
            break;
        case VideoBatchItemStatus::Error:
            view.status = 3;
            break;
        }
        items.append(view);
    }

    const bool structural = m_batchId != batch.id || m_running != batch.running
        || itemCount() != items.size();
    m_batchId = batch.id;
    m_running = batch.running;
    m_doneCount = batch.doneCount;
    m_failedCount = batch.failedCount;
    m_activeCount = batch.activeCount;
    m_queuedCount = batch.queuedCount;
    m_progress = batch.progress;
    m_loadedBytes = batch.downloadedBytes;
    m_totalBytes = batch.totalBytes;
    m_elapsedMs = batch.elapsedMs;
    m_etaSeconds = batch.etaSeconds;
    m_items = items;

    if (structural) {
        rebuildContent();
        rebuildFooter();
        emit layoutChanged();
    } else {
        refresh();
    }
}

void BatchExportDialog::rebuildContent()
{
    while (QLayoutItem *item = contentLayout()->takeAt(0)) {
        if (QWidget *widget = item->widget()) {
            widget->hide();
            widget->deleteLater();
        }
        delete item;
    }

    m_chip = nullptr;
    m_countLabel = nullptr;
    m_hintLabel = nullptr;
    m_summaryLabel = nullptr;
    m_percentLabel = nullptr;
    m_progressBar = nullptr;
    m_summaryLeft = nullptr;
    m_summaryRight = nullptr;
    m_listHeaderLeft = nullptr;
    m_listHeaderRight = nullptr;
    m_listScroll = nullptr;
    m_rows.clear();
    m_lastActiveRow = -1;

    auto *root = new QWidget(this);
    auto *layout = new QVBoxLayout(root);
    layout->setContentsMargins(0, 0, 0, 0);
    layout->setSpacing(0);

    // 标题：数量 / 体积 + 当前状态说明 + 状态徽标。
    auto *head = new QWidget(root);
    auto *headLayout = new QHBoxLayout(head);
    headLayout->setContentsMargins(0, 0, 0, 0);
    headLayout->setSpacing(12);

    auto *textColumn = new QWidget(head);
    auto *textLayout = new QVBoxLayout(textColumn);
    textLayout->setContentsMargins(0, 0, 0, 0);
    textLayout->setSpacing(2);
    m_countLabel = Ui::label(QString(), 13, 650, Theme::Text1, textColumn);
    textLayout->addWidget(m_countLabel);
    m_hintLabel = Ui::label(QString(), 11, 400, Theme::Text2, textColumn);
    m_hintLabel->setWordWrap(true);
    textLayout->addWidget(m_hintLabel);
    headLayout->addWidget(textColumn, 1);

    QString chipText = QStringLiteral("正在导出");
    Chip::Tone chipTone = Chip::Run;
    if (!m_running) {
        if (m_failedCount > 0) {
            chipText = QStringLiteral("部分失败");
            chipTone = Chip::Error;
        } else {
            chipText = QStringLiteral("已完成");
            chipTone = Chip::Ok;
        }
    }
    m_chip = new Chip(chipText, chipTone, head);
    headLayout->addWidget(m_chip, 0, Qt::AlignTop);
    layout->addWidget(head);

    if (!m_running) {
        layout->addSpacing(12);
        layout->addWidget(buildBanner(root));
    }

    layout->addSpacing(12);
    layout->addWidget(buildSummary(root));
    layout->addSpacing(12);
    layout->addWidget(buildList(root));

    if (m_running) {
        layout->addSpacing(10);
        auto *hint = new ElidedLabel(
            m_modeDraft()
                ? QStringLiteral("批量导入会按草稿逐个写入剪映目录，已完成的草稿立即就绪；"
                                 "失败只影响当前草稿，队列不会中断。")
                : QStringLiteral("批量导出会按队列并发合成；单条失败不会中断队列，"
                                 "已完成的成片可立即使用。"),
            root);
        hint->setPixelSize(11);
        hint->setTextColor(Theme::Text3);
        layout->addWidget(hint);
    }
    layout->addStretch(1);

    contentLayout()->addWidget(root);
    root->show();
    contentLayout()->invalidate();
    refresh();
}

QWidget *BatchExportDialog::buildSummary(QWidget *parent)
{
    auto *summary = new QWidget(parent);
    auto *layout = new QVBoxLayout(summary);
    layout->setContentsMargins(0, 0, 0, 0);
    layout->setSpacing(0);

    auto *top = new QWidget(summary);
    auto *topLayout = new QHBoxLayout(top);
    topLayout->setContentsMargins(0, 0, 0, 0);
    topLayout->setSpacing(8);
    m_summaryLabel = Ui::label(QString(), 12, 400, Theme::Text2, top);
    topLayout->addWidget(m_summaryLabel);
    topLayout->addStretch(1);
    m_percentLabel = Ui::label(QString(), 17, 650, Theme::Primary, top);
    m_percentLabel->setFont(Theme::monoFont(17, 650));
    topLayout->addWidget(m_percentLabel);
    layout->addWidget(top);
    layout->addSpacing(8);

    m_progressBar = new ProgressBar(summary);
    m_progressBar->setFixedHeight(6);
    layout->addWidget(m_progressBar);
    layout->addSpacing(6);

    auto *bottom = new QWidget(summary);
    auto *bottomLayout = new QHBoxLayout(bottom);
    bottomLayout->setContentsMargins(0, 0, 0, 0);
    bottomLayout->setSpacing(12);
    m_summaryLeft = Ui::label(QString(), 10, 400, Theme::Text3, bottom);
    m_summaryLeft->setFont(Theme::monoFont(10));
    bottomLayout->addWidget(m_summaryLeft);
    bottomLayout->addStretch(1);
    m_summaryRight = Ui::label(QString(), 10, 400, Theme::Text3, bottom);
    m_summaryRight->setFont(Theme::monoFont(10));
    bottomLayout->addWidget(m_summaryRight);
    layout->addWidget(bottom);
    return summary;
}

QWidget *BatchExportDialog::buildList(QWidget *parent)
{
    auto *scroll = new QScrollArea(parent);
    scroll->setFrameShape(QFrame::NoFrame);
    scroll->setWidgetResizable(true);
    scroll->setHorizontalScrollBarPolicy(Qt::ScrollBarAlwaysOff);
    scroll->setVerticalScrollBarPolicy(Qt::ScrollBarAsNeeded);
    scroll->setStyleSheet(QStringLiteral(
        "QScrollArea { background:#101218; border:1px solid #242938; border-radius:8px; }"
        "QScrollArea > QWidget > QWidget { background:#101218; }"));

    auto *host = new QWidget(scroll);
    auto *layout = new QVBoxLayout(host);
    layout->setContentsMargins(0, 0, 0, 0);
    layout->setSpacing(0);

    auto *header = new QFrame(host);
    header->setFixedHeight(24);
    header->setStyleSheet(QStringLiteral(
        "QFrame { background:#1A1E28; border:none; border-bottom:1px solid #242938; }"));
    auto *headerLayout = new QHBoxLayout(header);
    headerLayout->setContentsMargins(12, 0, 12, 0);
    headerLayout->setSpacing(8);
    auto *title = Ui::label(QStringLiteral("导出队列"), 10, 500, Theme::Text2, header);
    headerLayout->addWidget(title);
    m_listHeaderLeft = Ui::label(QString(), 10, 400, Theme::Text3, header);
    m_listHeaderLeft->setFont(Theme::monoFont(10));
    headerLayout->addWidget(m_listHeaderLeft);
    headerLayout->addStretch(1);
    m_listHeaderRight = Ui::label(QString(), 10, 400, Theme::Text3, header);
    m_listHeaderRight->setFont(Theme::monoFont(10));
    headerLayout->addWidget(m_listHeaderRight);
    layout->addWidget(header);

    m_rows.clear();
    for (int i = 0; i < m_items.size(); ++i) {
        const ViewItem &item = m_items.at(i);
        auto *row = new BatchRow(!m_modeDraft(), host);
        row->setItem(i + 1, item, QString());
        layout->addWidget(row);
        m_rows.append(row);
    }
    scroll->setWidget(host);

    const int contentHeight = 24 + m_rows.size() * 46 + 2;
    scroll->setFixedHeight(qBound(72, contentHeight, 324));
    m_listScroll = scroll;
    return scroll;
}

QWidget *BatchExportDialog::buildFolderRow(QWidget *parent)
{
    auto *row = new QFrame(parent);
    row->setStyleSheet(QStringLiteral(
        "QFrame { background:rgba(20,23,31,0.72);"
        "border:1px solid #242938; border-radius:6px; }"));
    auto *layout = new QHBoxLayout(row);
    layout->setContentsMargins(8, 4, 4, 4);
    layout->setSpacing(8);
    layout->addWidget(new IconBadge(Icons::Glyph::Folder, Theme::Text3,
                                    Qt::transparent, 20, row),
                      0, Qt::AlignVCenter);
    auto *path = new ElidedLabel(m_folderPath, row);
    path->setPixelSize(10);
    path->setMonoFont(true);
    path->setTextColor(Theme::Text2);
    layout->addWidget(path, 1);

    auto *copy = new GlyphButton(Icons::Glyph::Copy, 20, row);
    copy->setGlyphSize(11);
    copy->setColors(Theme::Text3, Theme::Primary);
    copy->setToolTipText(m_modeDraft() ? QStringLiteral("复制草稿目录")
                                       : QStringLiteral("复制成片目录"));
    connect(copy, &QAbstractButton::clicked, this, [this] {
        QGuiApplication::clipboard()->setText(m_folderPath);
        emit toastRequested(m_modeDraft() ? QStringLiteral("已复制草稿目录")
                                          : QStringLiteral("已复制成片目录"));
    });
    layout->addWidget(copy, 0, Qt::AlignVCenter);
    return row;
}

QWidget *BatchExportDialog::buildBanner(QWidget *parent)
{
    const bool partial = m_failedCount > 0;
    const QColor tone = partial ? Theme::Danger : Theme::Success;
    auto *banner = new QFrame(parent);
    banner->setStyleSheet(
        QStringLiteral("QFrame { background:%1; border:1px solid %2; border-radius:8px; }")
            .arg(Theme::alpha(tone, 0.08).name(QColor::HexArgb),
                 Theme::alpha(tone, 0.30).name(QColor::HexArgb)));
    auto *layout = new QVBoxLayout(banner);
    layout->setContentsMargins(14, 12, 14, 12);
    layout->setSpacing(0);

    auto *top = new QWidget(banner);
    auto *topLayout = new QHBoxLayout(top);
    topLayout->setContentsMargins(0, 0, 0, 0);
    topLayout->setSpacing(10);
    topLayout->addWidget(
        new IconBadge(partial ? Icons::Glyph::Alert : Icons::Glyph::Check,
                      partial ? Theme::Danger : Theme::OnAccent,
                      partial ? Theme::alpha(Theme::Danger, 0.16) : Theme::Success, 28,
                      top),
        0, Qt::AlignTop);

    auto *text = new QWidget(top);
    auto *textLayout = new QVBoxLayout(text);
    textLayout->setContentsMargins(0, 0, 0, 0);
    textLayout->setSpacing(2);
    const QString headline = partial
        ? (m_modeDraft()
               ? QStringLiteral("%1 个草稿导出失败").arg(m_failedCount)
               : QStringLiteral("%1 条成片导出失败").arg(m_failedCount))
        : (m_modeDraft()
               ? QStringLiteral("%1 个草稿已导入剪映").arg(m_doneCount)
               : QStringLiteral("%1 条成片已导出").arg(m_doneCount));
    textLayout->addWidget(Ui::label(headline, 13, 650, tone, text));

    QString detail;
    if (partial) {
        detail = m_modeDraft()
            ? (m_doneCount > 0 ? QStringLiteral("%1 个已成功导入；").arg(m_doneCount)
                               : QString())
                  + QStringLiteral("失败项会保留已完成的文件，重试时只补齐剩余部分。")
            : (m_doneCount > 0 ? QStringLiteral("%1 条已成功导出；").arg(m_doneCount)
                               : QString())
                  + QStringLiteral("失败项会保留已完成的合成结果，重试时从断点继续。");
    } else {
        detail = m_modeDraft()
            ? QStringLiteral("无需逐个打开剪映，全部草稿已按草稿名写入本地目录。")
            : QStringLiteral("MP4 · 1080p · 30fps · 共 %1；无需逐条等待，"
                             "已保存到本地导出目录。")
                  .arg(MediaUtils::formatByteSize(m_totalBytes));
    }
    textLayout->addWidget(Ui::label(detail, 11, 400, Theme::Text2, text));
    topLayout->addWidget(text, 1);
    layout->addWidget(top);

    if (!partial || m_doneCount > 0) {
        layout->addSpacing(10);
        layout->addWidget(buildFolderRow(banner));
    }
    return banner;
}

void BatchExportDialog::rebuildFooter()
{
    QHBoxLayout *footer = footerLayout();
    while (QLayoutItem *item = footer->takeAt(0)) {
        if (QWidget *widget = item->widget()) {
            widget->hide();
            widget->deleteLater();
        }
        delete item;
    }
    footer->addStretch(1);

    const bool windows = Theme::isWindowsWindowStyle();
    auto addActions = [footer, windows](QWidget *primary, QWidget *secondary) {
        if (windows) {
            footer->addWidget(primary);
            footer->addWidget(secondary);
        } else {
            footer->addWidget(secondary);
            footer->addWidget(primary);
        }
    };

    if (m_running) {
        auto *hint = Ui::label(QStringLiteral("关闭窗口后批量导出会继续"), 10, 400,
                               Theme::Text3, this);
        footer->insertWidget(0, hint);
        footer->setStretch(0, 0);
        footer->setStretch(1, 1);

        auto *background =
            new Button(QStringLiteral("后台继续"), Button::Primary, this);
        connect(background, &QAbstractButton::clicked, this,
                [this] { emit closeRequested(); });
        auto *stop = new Button(QStringLiteral("停止导出"), Button::Ghost, this);
        connect(stop, &QAbstractButton::clicked, this,
                [this] { emit cancelRequested(); });
        addActions(background, stop);
        return;
    }

    auto makeClose = [this] {
        auto *close = new Button(QStringLiteral("关闭"), Button::Default, this);
        connect(close, &QAbstractButton::clicked, this,
                [this] { emit closeRequested(); });
        return close;
    };
    auto makeOpenFolder = [this] {
        auto *open = new Button(
            m_modeDraft()
                ? (Theme::isWindowsWindowStyle()
                       ? QStringLiteral("打开草稿文件夹")
                       : QStringLiteral("在访达中打开草稿文件夹"))
                : QStringLiteral("打开成片文件夹"),
            Button::Default, this);
        connect(open, &QAbstractButton::clicked, this, [this] {
            emit openFolderRequested(m_folderPath);
        });
        return open;
    };

    if (m_failedCount > 0) {
        footer->addWidget(makeClose());
        footer->addWidget(makeOpenFolder());
        auto *retry =
            new Button(QStringLiteral("重试失败项（%1）").arg(m_failedCount),
                       Button::Primary, this);
        connect(retry, &QAbstractButton::clicked, this,
                [this] { emit retryFailedRequested(); });
        if (windows) {
            footer->insertWidget(1, retry);
        } else {
            footer->addWidget(retry);
        }
        return;
    }

    auto *reveal = new Button(
        m_modeDraft()
            ? (Theme::isWindowsWindowStyle() ? QStringLiteral("打开草稿文件夹")
                                             : QStringLiteral("在访达中打开草稿文件夹"))
            : QStringLiteral("打开成片文件夹"),
        Button::Primary, this);
    reveal->setLeadingGlyph(Icons::Glyph::Folder);
    connect(reveal, &QAbstractButton::clicked, this, [this] {
        emit openFolderRequested(m_folderPath);
    });
    addActions(reveal, makeClose());
}

void BatchExportDialog::refresh()
{
    if (m_countLabel) {
        setLabelTextIfChanged(
            m_countLabel,
            m_modeDraft()
                ? QStringLiteral("%1 个草稿 · 共约 %2")
                      .arg(itemCount())
                      .arg(MediaUtils::formatByteSize(m_totalBytes))
                : QStringLiteral("%1 条成片 · 共约 %2")
                      .arg(itemCount())
                      .arg(MediaUtils::formatByteSize(m_totalBytes)));
    }
    if (m_hintLabel) {
        QString hint;
        if (m_running) {
            if (m_modeDraft()) {
                const int concurrency = 3;
                hint = itemCount() == 1
                    ? QStringLiteral("单个草稿将直接写入剪映草稿目录；关闭窗口后继续。")
                    : (itemCount() <= concurrency
                           ? QStringLiteral("同时导出 %1 个草稿；关闭窗口后继续。")
                                 .arg(itemCount())
                           : QStringLiteral("同时下载 %1 个，其余按顺序排队；"
                                            "关闭窗口后继续。")
                                 .arg(concurrency));
            } else {
                const int concurrency = qMin(2, itemCount());
                hint = itemCount() == 1
                    ? QStringLiteral("单条成片正在云端合成 MP4 · 1080p；关闭窗口后继续。")
                    : (itemCount() <= 2
                           ? QStringLiteral("同时合成 %1 条成片（MP4 · 1080p）；"
                                            "关闭窗口后继续。")
                                 .arg(itemCount())
                           : QStringLiteral("同时合成 %1 条成片（MP4 · 1080p），"
                                            "其余按顺序排队；关闭窗口后继续。")
                                 .arg(concurrency));
            }
        } else if (m_failedCount > 0) {
            hint = m_modeDraft()
                ? QStringLiteral("已完成的草稿可直接在剪映中编辑；"
                                 "失败项可只补齐剩余文件。")
                : QStringLiteral("已成功的成片已保存到本地；失败项会保留已完成的"
                                 "合成结果，重试时从断点继续。");
        } else {
            hint = m_modeDraft()
                ? QStringLiteral("全部草稿已写入剪映草稿目录，"
                                 "可在剪映「本地草稿」中继续编辑。")
                : QStringLiteral("全部成片已保存到本地导出目录，可直接发布。");
        }
        setLabelTextIfChanged(m_hintLabel, hint);
    }

    const int percent = m_running
        ? qMin(99, int(qBound(0.0, m_progress, 100.0)))
        : 100;
    const bool partialNow = partial();
    if (m_percentLabel) {
        setLabelTextIfChanged(m_percentLabel, QStringLiteral("%1%").arg(percent));
        const QString style = QStringLiteral("color:%1;background:transparent;")
                                  .arg(partialNow ? QStringLiteral("#F6465D")
                                                  : (!m_running ? QStringLiteral("#3ECF8E")
                                                                : QStringLiteral("#6E8BFF")));
        if (m_percentLabel->styleSheet() != style) {
            m_percentLabel->setStyleSheet(style);
        }
    }
    if (m_summaryLabel) {
        QString label;
        if (m_running) {
            label = m_modeDraft() ? QStringLiteral("正在导出剪映草稿")
                                  : QStringLiteral("正在导出成片");
        } else if (partialNow) {
            label = QStringLiteral("批量导出结束");
        } else {
            label = m_modeDraft() ? QStringLiteral("全部导入完成")
                                  : QStringLiteral("全部成片已导出");
        }
        setLabelTextIfChanged(m_summaryLabel, label);
    }
    if (m_progressBar) {
        m_progressBar->setValue(int(qBound(0.0, m_progress, 100.0)));
        m_progressBar->setBarColor(partialNow ? Theme::Danger
                                              : (!m_running ? Theme::Success
                                                            : Theme::Primary));
    }

    const int processed = m_doneCount + m_failedCount;
    if (m_summaryLeft) {
        setLabelTextIfChanged(
            m_summaryLeft,
            m_modeDraft()
                ? QStringLiteral("已处理 %1 / %2 · %3 / %4")
                      .arg(processed)
                      .arg(itemCount())
                      .arg(MediaUtils::formatByteSize(m_loadedBytes),
                           MediaUtils::formatByteSize(m_totalBytes))
                : QStringLiteral("已处理 %1 / %2 · 共约 %3")
                      .arg(processed)
                      .arg(itemCount())
                      .arg(MediaUtils::formatByteSize(m_totalBytes)));
    }
    if (m_summaryRight) {
        QString runningMeta;
        if (m_activeCount > 0) {
            runningMeta = QStringLiteral("%1 个%2").arg(m_activeCount)
                              .arg(m_modeDraft() ? QStringLiteral("下载中")
                                                 : QStringLiteral("导出中"));
        }
        if (m_queuedCount > 0) {
            if (!runningMeta.isEmpty()) {
                runningMeta += QStringLiteral(" · ");
            }
            runningMeta += QStringLiteral("%1 个排队").arg(m_queuedCount);
        }

        QString text;
        if (m_running) {
            text = runningMeta.isEmpty()
                ? QStringLiteral("预计还需 %1").arg(formatEta(m_etaSeconds))
                : QStringLiteral("%1 · 预计还需 %2")
                      .arg(runningMeta, formatEta(m_etaSeconds));
        } else if (partialNow) {
            text = QStringLiteral("%1 %2成功 · %3 %2失败")
                       .arg(m_doneCount)
                       .arg(m_modeDraft() ? QStringLiteral("个") : QStringLiteral("条"))
                       .arg(m_failedCount);
        } else {
            text = QStringLiteral("共 %1 %2 · 用时 %3")
                       .arg(m_doneCount)
                       .arg(m_modeDraft() ? QStringLiteral("个草稿")
                                          : QStringLiteral("条"))
                       .arg(formatElapsed(m_elapsedMs));
        }
        setLabelTextIfChanged(m_summaryRight, text);
    }

    if (m_listHeaderLeft) {
        setLabelTextIfChanged(
            m_listHeaderLeft,
            QStringLiteral("%1 / %2 %3")
                .arg(m_doneCount)
                .arg(itemCount())
                .arg(m_modeDraft() ? QStringLiteral("已就绪")
                                   : QStringLiteral("已导出")));
    }
    if (m_listHeaderRight) {
        QString text;
        if (m_running) {
            QStringList meta;
            if (m_activeCount > 0) {
                meta.append(QStringLiteral("%1 个%2").arg(m_activeCount)
                               .arg(m_modeDraft() ? QStringLiteral("下载中")
                                                  : QStringLiteral("导出中")));
            }
            if (m_queuedCount > 0) {
                meta.append(QStringLiteral("%1 个排队").arg(m_queuedCount));
            }
            text = meta.join(QStringLiteral(" · "));
        } else if (m_failedCount > 0) {
            text = QStringLiteral("%1 %2失败 · 共 %3 %2")
                       .arg(m_failedCount)
                       .arg(m_modeDraft() ? QStringLiteral("个") : QStringLiteral("条"))
                       .arg(itemCount());
        } else {
            text = QStringLiteral("共 %1 %2")
                       .arg(itemCount())
                       .arg(m_modeDraft() ? QStringLiteral("个") : QStringLiteral("条"));
        }
        setLabelTextIfChanged(m_listHeaderRight, text);
    }

    int activeRow = -1;
    for (int i = 0; i < m_rows.size() && i < m_items.size(); ++i) {
        m_rows.at(i)->setAccent(m_items.at(i).status);
        m_rows.at(i)->setItem(i + 1, m_items.at(i), QString());
        if (activeRow < 0 && m_items.at(i).status == 1) {
            activeRow = i;
        }
    }
    if (!m_running && activeRow < 0) {
        for (int i = 0; i < m_items.size(); ++i) {
            if (m_items.at(i).status == 3) {
                activeRow = i;
                break;
            }
        }
    }
    if (activeRow != m_lastActiveRow) {
        m_lastActiveRow = activeRow;
        if (m_listScroll && activeRow >= 0 && activeRow < m_rows.size()) {
            m_listScroll->ensureWidgetVisible(m_rows.at(activeRow), 0, 40);
        }
    }
}
