#include "StudioWidgets.h"

#include <QApplication>
#include <QDragEnterEvent>
#include <QDragLeaveEvent>
#include <QDropEvent>
#include <QEnterEvent>
#include <QFontMetrics>
#include <QHBoxLayout>
#include <QKeyEvent>
#include <QLabel>
#include <QLayout>
#include <QLinearGradient>
#include <QMimeData>
#include <QMouseEvent>
#include <QPainter>
#include <QPainterPath>
#include <QTextEdit>
#include <QUrl>
#include <QVBoxLayout>
#include <QVector>

namespace Studio {

QColor aiSoft()
{
    return Theme::alpha(Theme::Ai, 0.13);
}

QColor successSoft()
{
    return Theme::alpha(Theme::Success, 0.13);
}

QColor primarySoft()
{
    return Theme::alpha(Theme::Primary, 0.14);
}

QColor infoSoft()
{
    return Theme::alpha(Theme::Primary, 0.10);
}

void clearLayout(QLayout *layout)
{
    if (!layout) {
        return;
    }
    while (QLayoutItem *item = layout->takeAt(0)) {
        if (QWidget *widget = item->widget()) {
            widget->hide();
            widget->deleteLater();
        }
        if (QLayout *child = item->layout()) {
            clearLayout(child);
            delete child;
        }
        delete item;
    }
}

QLabel *iconLabel(Icons::Glyph glyph, int size, const QColor &color, QWidget *parent)
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

QLabel *tagLabel(const QString &text, const QColor &color, const QColor &background,
                 int pixelSize, QWidget *parent)
{
    auto *label = new QLabel(text, parent);
    label->setFont(Theme::font(pixelSize, QFont::Medium));
    label->setStyleSheet(QStringLiteral(
        "color:%1;background:%2;border-radius:3px;padding:1px 4px;")
                             .arg(color.name(QColor::HexArgb),
                                  background.name(QColor::HexArgb)));
    label->setSizePolicy(QSizePolicy::Fixed, QSizePolicy::Fixed);
    label->setAttribute(Qt::WA_TransparentForMouseEvents, true);
    return label;
}

QLabel *pillLabel(const QString &text, const QColor &color, const QColor &background,
                  const QColor &border, int pixelSize, QWidget *parent)
{
    auto *label = new QLabel(text, parent);
    label->setFont(Theme::font(pixelSize, QFont::Medium));
    label->setStyleSheet(QStringLiteral(
        "color:%1;background:%2;border:1px solid %3;border-radius:9px;padding:1px 7px;")
                             .arg(color.name(QColor::HexArgb),
                                  background.name(QColor::HexArgb),
                                  border.name(QColor::HexArgb)));
    label->setSizePolicy(QSizePolicy::Fixed, QSizePolicy::Fixed);
    label->setAttribute(Qt::WA_TransparentForMouseEvents, true);
    return label;
}

void applyTextAreaStyle(QTextEdit *edit, bool aiFocus, int pixelSize)
{
    if (!edit) {
        return;
    }
    edit->setFont(Theme::font(pixelSize));
    edit->setFrameShape(QFrame::NoFrame);
    edit->setTabChangesFocus(true);
    edit->setStyleSheet(
        QStringLiteral(
            "QTextEdit { background:#0c0e14; border:1px solid %1; border-radius:7px;"
            " padding:7px 9px; color:%2; selection-background-color:#4A63BC;"
            " selection-color:#FFFFFF; }"
            "QTextEdit:focus { border:1px solid %3; }"
            "QTextEdit:disabled { color:%4; }")
            .arg(Theme::Border.name(),
                 Theme::Text2.name(),
                 (aiFocus ? Theme::Ai : Theme::Primary).name(),
                 Theme::Text3.name()));
    QPalette palette = edit->palette();
    palette.setColor(QPalette::Base, QColor(QStringLiteral("#0c0e14")));
    palette.setColor(QPalette::Text, Theme::Text2);
    edit->setPalette(palette);
    edit->setVerticalScrollBarPolicy(Qt::ScrollBarAsNeeded);
}

QString formatClipPath(const QString &path, int maxChars)
{
    if (path.size() <= maxChars) {
        return path;
    }
    const int head = qMax(8, maxChars / 2);
    const int tail = qMax(6, maxChars - head - 1);
    return path.left(head) + QStringLiteral("…") + path.right(tail);
}

/* ------------------------------------------------------------------ */
/* SectionCard                                                          */
/* ------------------------------------------------------------------ */

SectionCard::SectionCard(Icons::Glyph glyph, const QString &title, QWidget *parent)
    : SectionCard(glyph, title, QString(), parent)
{
}

SectionCard::SectionCard(Icons::Glyph glyph, const QString &title,
                         const QString &meta, QWidget *parent)
    : QWidget(parent)
{
    auto *root = new QVBoxLayout(this);
    root->setContentsMargins(0, 0, 0, 0);
    root->setSpacing(0);

    auto *header = new QWidget(this);
    header->setFixedHeight(40);
    m_header = new QHBoxLayout(header);
    m_header->setContentsMargins(14, 0, 12, 0);
    m_header->setSpacing(8);
    m_header->addWidget(iconLabel(glyph, 14, Theme::Text3, header), 0,
                        Qt::AlignVCenter);
    auto *titleLabel = Ui::label(title, 12, QFont::DemiBold, Theme::Text1, header);
    m_header->addWidget(titleLabel, 0, Qt::AlignVCenter);
    m_meta = Ui::label(meta, 10, QFont::Normal, Theme::Text3, header);
    m_meta->setVisible(!meta.isEmpty());
    m_header->addWidget(m_meta, 0, Qt::AlignVCenter);
    m_header->addStretch(1);
    root->addWidget(header);
    root->addWidget(Ui::separator(false, this));

    auto *body = new QWidget(this);
    m_body = new QVBoxLayout(body);
    m_body->setContentsMargins(12, 12, 12, 12);
    m_body->setSpacing(0);
    root->addWidget(body, 1);
}

void SectionCard::setMeta(const QString &meta)
{
    m_meta->setText(meta);
    m_meta->setVisible(!meta.isEmpty());
}

void SectionCard::setBodyMargins(const QMargins &margins)
{
    m_body->setContentsMargins(margins);
}

void SectionCard::setHeaderActionsSpacing(int spacing)
{
    m_header->setSpacing(spacing);
}

void SectionCard::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    painter.setRenderHint(QPainter::Antialiasing, true);
    const QRectF rect = QRectF(this->rect()).adjusted(0.5, 0.5, -0.5, -0.5);
    painter.setPen(QPen(Theme::Border, 1.0));
    painter.setBrush(Theme::Surface1);
    painter.drawRoundedRect(rect, 10, 10);
}

/* ------------------------------------------------------------------ */
/* Note                                                                 */
/* ------------------------------------------------------------------ */

Note::Note(const QString &html, Tone tone, QWidget *parent)
    : QWidget(parent)
{
    setAttribute(Qt::WA_StyledBackground, true);
    setObjectName(QStringLiteral("studioNote"));
    setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
    const QColor textColor = tone == Ai ? Theme::Text2 : Theme::Text3;
    setStyleSheet(
        QStringLiteral("#studioNote { background:%1; border:1px solid %2; border-radius:8px; }")
            .arg((tone == Muted ? Theme::alpha(Theme::Surface2, 0.6)
                                : (tone == Primary ? primarySoft() : aiSoft()))
                     .name(QColor::HexArgb),
                 (tone == Muted ? Theme::Border
                                : (tone == Primary ? Theme::alpha(Theme::Primary, 0.25)
                                                   : Theme::alpha(Theme::Ai, 0.25)))
                     .name(QColor::HexArgb)));

    m_row = new QHBoxLayout(this);
    m_row->setContentsMargins(12, 9, 12, 9);
    m_row->setSpacing(7);
    auto *label = Ui::richLabel(html, 10, textColor, this);
    label->setWordWrap(true);
    label->setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Preferred);
    m_row->addWidget(label, 1);
}

void Note::paintEvent(QPaintEvent *event)
{
    QWidget::paintEvent(event);
}

/* ------------------------------------------------------------------ */
/* EmptyRecent                                                          */
/* ------------------------------------------------------------------ */

EmptyRecent::EmptyRecent(const QString &title, const QString &description,
                         const QStringList &features, const QString &sampleLabel,
                         const QString &startLabel, QWidget *parent)
    : QWidget(parent)
{
    setMinimumHeight(138);
    setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Preferred);

    auto *row = new QHBoxLayout(this);
    row->setContentsMargins(20, 16, 20, 16);
    row->setSpacing(20);

    // 左侧插画在 paintEvent 中绘制，这里只占位。
    auto *art = new QWidget(this);
    art->setFixedSize(124, 96);
    art->setAttribute(Qt::WA_TransparentForMouseEvents, true);
    row->addWidget(art, 0, Qt::AlignVCenter);

    auto *texts = new QWidget(this);
    auto *textLayout = new QVBoxLayout(texts);
    textLayout->setContentsMargins(0, 0, 0, 0);
    textLayout->setSpacing(0);
    auto *titleLabel = Ui::label(title, 14, QFont::DemiBold, Theme::Text1, texts);
    textLayout->addWidget(titleLabel);
    textLayout->addSpacing(4);
    auto *descriptionLabel = Ui::label(description, 10, QFont::Normal, Theme::Text3,
                                       texts);
    descriptionLabel->setWordWrap(true);
    descriptionLabel->setMaximumWidth(440);
    textLayout->addWidget(descriptionLabel);
    textLayout->addSpacing(8);
    auto *featureRow = new QWidget(texts);
    auto *featureLayout = new QHBoxLayout(featureRow);
    featureLayout->setContentsMargins(0, 0, 0, 0);
    featureLayout->setSpacing(14);
    for (const QString &feature : features) {
        auto *item = new QWidget(featureRow);
        auto *itemLayout = new QHBoxLayout(item);
        itemLayout->setContentsMargins(0, 0, 0, 0);
        itemLayout->setSpacing(4);
        itemLayout->addWidget(iconLabel(Icons::Glyph::Check, 10, Theme::Success,
                                        item));
        itemLayout->addWidget(Ui::label(feature, 9, QFont::Normal, Theme::Text3,
                                        item));
        featureLayout->addWidget(item);
    }
    featureLayout->addStretch(1);
    textLayout->addWidget(featureRow);
    row->addWidget(texts, 1);

    auto *buttons = new QWidget(this);
    auto *buttonLayout = new QHBoxLayout(buttons);
    buttonLayout->setContentsMargins(0, 0, 0, 0);
    buttonLayout->setSpacing(8);
    auto *sample = new Button(sampleLabel, Button::Default, buttons);
    sample->setLeadingGlyph(Icons::Glyph::Play);
    connect(sample, &QAbstractButton::clicked, this, &EmptyRecent::sampleRequested);
    auto *start = new Button(startLabel, Button::Primary, buttons);
    start->setLeadingGlyph(Icons::Glyph::Sparkle);
    connect(start, &QAbstractButton::clicked, this, &EmptyRecent::startRequested);
    buttonLayout->addWidget(sample);
    buttonLayout->addWidget(start);
    row->addWidget(buttons, 0, Qt::AlignVCenter);
}

void EmptyRecent::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    painter.setRenderHint(QPainter::Antialiasing, true);
    const QRectF rect = QRectF(this->rect()).adjusted(0.5, 0.5, -0.5, -0.5);
    QPainterPath card;
    card.addRoundedRect(rect, 10, 10);
    painter.fillPath(card, Theme::alpha(Theme::Surface1, 0.55));

    QRadialGradient glow(rect.left(), rect.top(), rect.width() * 0.8);
    glow.setColorAt(0, QColor(110, 139, 255, 23));
    glow.setColorAt(0.58, QColor(110, 139, 255, 0));
    painter.fillPath(card, glow);

    QPen dashPen(Theme::alpha(Theme::BorderStrong, 0.70), 1.0, Qt::DashLine);
    painter.setPen(dashPen);
    painter.setBrush(Qt::NoBrush);
    painter.drawPath(card);

    // 叠放的三张素材卡：前一张带胶片图标与占位字幕行。
    const qreal artX = rect.left() + 20;
    const qreal artY = rect.center().y() - 48;

    painter.save();
    painter.translate(artX + 29, artY + 57);
    painter.rotate(-8);
    painter.setPen(QPen(Theme::alpha(Theme::BorderStrong, 0.50), 1.0, Qt::DashLine));
    painter.setBrush(Theme::alpha(Theme::Surface2, 0.20));
    painter.drawRoundedRect(QRectF(-29, -41, 58, 82), 6, 6);
    painter.restore();

    painter.save();
    painter.translate(artX + 56, artY + 52);
    painter.rotate(5);
    painter.setPen(QPen(Theme::alpha(Theme::BorderStrong, 0.60), 1.0, Qt::DashLine));
    painter.setBrush(Theme::alpha(Theme::Surface2, 0.30));
    painter.drawRoundedRect(QRectF(-30, -43, 60, 86), 6, 6);
    painter.restore();

    painter.save();
    painter.translate(artX + 83, artY + 45);
    painter.setPen(QPen(Theme::alpha(Theme::Primary, 0.35), 1.0, Qt::DashLine));
    painter.setBrush(QColor(QStringLiteral("#0c0e14")));
    painter.drawRoundedRect(QRectF(-31, -45, 62, 90), 7, 7);
    Icons::paint(&painter, QRectF(-9, -22, 18, 18), Icons::Glyph::Film,
                 Theme::Text3, 1.8);
    painter.setPen(Qt::NoPen);
    painter.setBrush(Theme::alpha(Theme::Surface3, 0.80));
    painter.drawRoundedRect(QRectF(-23, 26, 46, 3), 1.5, 1.5);
    painter.setBrush(Theme::alpha(Theme::Surface3, 0.55));
    painter.drawRoundedRect(QRectF(-23, 33, 31, 3), 1.5, 1.5);
    painter.restore();

    const QRectF badge(artX + 100, artY + 76, 20, 20);
    painter.setPen(QPen(Theme::alpha(Theme::Ai, 0.35), 1.0));
    painter.setBrush(QColor(QStringLiteral("#1b1509")));
    painter.drawEllipse(badge);
    Icons::paint(&painter, badge.adjusted(5, 5, -5, -5), Icons::Glyph::Sparkle,
                 Theme::Ai, 1.8);
}

/* ------------------------------------------------------------------ */
/* SegmentedControl                                                     */
/* ------------------------------------------------------------------ */

SegmentedControl::SegmentedControl(QWidget *parent)
    : QWidget(parent)
{
    setCursor(Qt::PointingHandCursor);
    setFocusPolicy(Qt::StrongFocus);
    setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
}

void SegmentedControl::setOptions(const QStringList &labels, const QStringList &hints)
{
    m_labels = labels;
    m_hints = hints;
    if (m_current >= m_labels.size()) {
        m_current = qMax(0, m_labels.size() - 1);
    }
    updateGeometry();
    update();
}

void SegmentedControl::setCurrentIndex(int index)
{
    if (m_labels.isEmpty()) {
        return;
    }
    const int next = qBound(0, index, m_labels.size() - 1);
    if (m_current == next) {
        return;
    }
    m_current = next;
    update();
    emit currentChanged(next);
}

void SegmentedControl::setPixelSize(int pixelSize)
{
    m_pixelSize = pixelSize;
    updateGeometry();
    update();
}

void SegmentedControl::setCompact(bool compact)
{
    m_compact = compact;
    updateGeometry();
    update();
}

void SegmentedControl::setEnabled(bool enabled)
{
    m_enabled = enabled;
    setCursor(enabled ? Qt::PointingHandCursor : Qt::ArrowCursor);
    update();
}

int SegmentedControl::preferredHeight() const
{
    if (m_compact) {
        return m_hints.isEmpty() ? 24 : 36;
    }
    return m_hints.isEmpty() ? 30 : 42;
}

QSize SegmentedControl::sizeHint() const
{
    int width = 0;
    const QFont labelFont = Theme::font(m_pixelSize, QFont::Medium);
    const QFont hintFont = Theme::font(9);
    const QFontMetrics labelMetrics(labelFont);
    const QFontMetrics hintMetrics(hintFont);
    for (int index = 0; index < m_labels.size(); ++index) {
        width += qMax(labelMetrics.horizontalAdvance(m_labels.at(index)),
                      m_hints.size() > index
                          ? hintMetrics.horizontalAdvance(m_hints.at(index))
                          : 0);
        width += 28;
    }
    return QSize(width + 8, preferredHeight());
}

QRect SegmentedControl::segmentRect(int index) const
{
    if (m_labels.isEmpty()) {
        return QRect();
    }
    const int gap = 4;
    const int innerLeft = gap;
    const int innerWidth = width() - gap * 2;
    const int segmentWidth = innerWidth / m_labels.size();
    const int x = innerLeft + index * segmentWidth;
    return QRect(x, gap, segmentWidth, height() - gap * 2);
}

int SegmentedControl::segmentAt(const QPoint &point) const
{
    if (m_labels.isEmpty() || !m_enabled) {
        return -1;
    }
    for (int index = 0; index < m_labels.size(); ++index) {
        if (segmentRect(index).contains(point)) {
            return index;
        }
    }
    return -1;
}

void SegmentedControl::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    painter.setRenderHint(QPainter::Antialiasing, true);
    if (!m_enabled) {
        painter.setOpacity(0.5);
    }

    const QRectF outer = QRectF(rect()).adjusted(0.5, 0.5, -0.5, -0.5);
    painter.setPen(QPen(Theme::Border, 1.0));
    painter.setBrush(Theme::alpha(Theme::Surface2, 0.7));
    painter.drawRoundedRect(outer, 8, 8);

    for (int index = 0; index < m_labels.size(); ++index) {
        const QRect segment = segmentRect(index);
        const bool selected = index == m_current;

        if (selected) {
            const QRectF selectedRect = QRectF(segment).adjusted(0.5, 0.5, -0.5, -0.5);
            painter.setPen(QPen(Theme::alpha(Theme::Primary, 0.32), 1.0));
            painter.setBrush(primarySoft());
            painter.drawRoundedRect(selectedRect, 6, 6);
        } else if (index == m_hovered && m_enabled) {
            painter.setPen(Qt::NoPen);
            painter.setBrush(Theme::alpha(Theme::Surface3, 0.6));
            painter.drawRoundedRect(QRectF(segment).adjusted(0.5, 0.5, -0.5, -0.5), 6,
                                    6);
        }

        painter.setPen(selected ? Theme::Primary : Theme::Text2);
        painter.setFont(Theme::font(m_pixelSize, selected ? QFont::Medium : QFont::Normal));

        if (m_hints.isEmpty()) {
            painter.drawText(segment, Qt::AlignCenter, m_labels.at(index));
        } else {
            const QString hint = m_hints.value(index);
            const int labelHeight = QFontMetrics(painter.font()).height();
            QRect upper = segment.adjusted(2, 4, -2, -4);
            QRect lower = upper;
            if (hint.isEmpty()) {
                painter.drawText(upper, Qt::AlignCenter, m_labels.at(index));
            } else {
                upper.setHeight(labelHeight);
                lower.setTop(upper.bottom() + 1);
                painter.drawText(upper, Qt::AlignHCenter | Qt::AlignBottom,
                                 m_labels.at(index));
                painter.setFont(Theme::font(9));
                painter.setPen(selected ? Theme::alpha(Theme::Primary, 0.72)
                                        : Theme::Text3);
                painter.drawText(lower, Qt::AlignHCenter | Qt::AlignTop, hint);
            }
        }
    }
}

void SegmentedControl::mouseMoveEvent(QMouseEvent *event)
{
    const int hovered = segmentAt(event->pos());
    if (hovered != m_hovered) {
        m_hovered = hovered;
        update();
    }
    QWidget::mouseMoveEvent(event);
}

void SegmentedControl::leaveEvent(QEvent *event)
{
    m_hovered = -1;
    update();
    QWidget::leaveEvent(event);
}

void SegmentedControl::mousePressEvent(QMouseEvent *event)
{
    const int index = segmentAt(event->pos());
    if (index >= 0) {
        setCurrentIndex(index);
    }
    QWidget::mousePressEvent(event);
}

void SegmentedControl::keyPressEvent(QKeyEvent *event)
{
    if (!m_enabled || m_labels.isEmpty()) {
        QWidget::keyPressEvent(event);
        return;
    }
    if (event->key() == Qt::Key_Left) {
        setCurrentIndex(m_current - 1);
    } else if (event->key() == Qt::Key_Right) {
        setCurrentIndex(m_current + 1);
    } else {
        QWidget::keyPressEvent(event);
    }
}

/* ------------------------------------------------------------------ */
/* StepRail                                                             */
/* ------------------------------------------------------------------ */

StepRail::StepRail(QWidget *parent)
    : QWidget(parent)
{
    setFixedHeight(42);
}

void StepRail::setSteps(const QStringList &labels, const QStringList &hints)
{
    m_labels = labels;
    m_hints = hints;
    update();
}

void StepRail::setActiveStep(int step)
{
    m_active = step;
    update();
}

QSize StepRail::sizeHint() const
{
    return QSize(560, 42);
}

void StepRail::paintEvent(QPaintEvent *)
{
    if (m_labels.isEmpty()) {
        return;
    }
    QPainter painter(this);
    painter.setRenderHint(QPainter::Antialiasing, true);

    const QFont labelFont = Theme::font(11, QFont::Medium);
    const QFont hintFont = Theme::font(9);
    const QFontMetrics labelMetrics(labelFont);
    const QFontMetrics hintMetrics(hintFont);
    const int circle = 22;
    const int gap = 8;
    const int connector = 18;

    QVector<int> contentWidths;
    int total = 0;
    for (int index = 0; index < m_labels.size(); ++index) {
        const int textWidth = qMax(labelMetrics.horizontalAdvance(m_labels.at(index)),
                                   m_hints.size() > index
                                       ? hintMetrics.horizontalAdvance(m_hints.at(index))
                                       : 0);
        const int content = circle + gap + textWidth;
        contentWidths.append(content);
        total += content;
    }
    total += connector * (m_labels.size() - 1);

    const int extra = qMax(0, width() - total);
    const int extraPerGap = m_labels.size() > 1 ? extra / (m_labels.size() - 1) : 0;
    const int circleY = 10;

    m_hitAreas.clear();
    int x = 0;
    for (int index = 0; index < m_labels.size(); ++index) {
        const int number = index + 1;
        const bool done = number < m_active;
        const bool current = number == m_active;
        const QRect circleRect(x, circleY, circle, circle);

        QColor circleFill = Theme::Surface2;
        QColor circleBorder = Theme::BorderStrong;
        QColor circleText = Theme::Text3;
        if (done) {
            circleFill = primarySoft();
            circleBorder = Theme::alpha(Theme::Primary, 0.45);
            circleText = Theme::Primary;
        } else if (current) {
            circleFill = Theme::Primary;
            circleBorder = Theme::Primary;
            circleText = Theme::OnAccent;
        }
        if (current) {
            // 当前步骤外圈 3px 主色光晕，与设计稿的 focus ring 一致。
            painter.setPen(Qt::NoPen);
            painter.setBrush(Theme::alpha(Theme::Primary, 0.14));
            painter.drawEllipse(QRectF(circleRect).adjusted(-3, -3, 3, 3));
        }
        painter.setPen(QPen(circleBorder, 1.0));
        painter.setBrush(circleFill);
        painter.drawEllipse(circleRect);

        if (done) {
            Icons::paint(&painter, QRectF(circleRect).adjusted(6, 6, -6, -6),
                         Icons::Glyph::Check, circleText, 2.4);
        } else {
            painter.setFont(Theme::monoFont(9));
            painter.setPen(circleText);
            painter.drawText(circleRect, Qt::AlignCenter, QString::number(number));
        }

        const int textX = x + circle + gap;
        painter.setFont(labelFont);
        painter.setPen(current ? Theme::Text1 : (done ? Theme::Text2 : Theme::Text3));
        painter.drawText(QRect(textX, 1, contentWidths.at(index) - circle - gap, 16),
                         Qt::AlignLeft | Qt::AlignVCenter, m_labels.at(index));
        painter.setFont(hintFont);
        painter.setPen(Theme::Text3);
        painter.drawText(QRect(textX, 17, contentWidths.at(index) - circle - gap, 14),
                         Qt::AlignLeft | Qt::AlignVCenter,
                         m_hints.value(index));

        m_hitAreas.append(QRect(x, 0, contentWidths.at(index), height()));

        if (index < m_labels.size() - 1) {
            const int lineStart = x + contentWidths.at(index) + 6;
            const int lineEnd = lineStart + connector + extraPerGap - 12;
            painter.setPen(QPen(Theme::Border, 1.0));
            painter.drawLine(QPointF(lineStart, circleY + circle / 2.0),
                             QPointF(qMax(lineStart + 1, lineEnd),
                                     circleY + circle / 2.0));
        }
        x += contentWidths.at(index) + connector + extraPerGap;
    }
}

void StepRail::mousePressEvent(QMouseEvent *event)
{
    for (int index = 0; index < m_hitAreas.size(); ++index) {
        if (m_hitAreas.at(index).contains(event->pos()) && index + 1 < m_active) {
            emit stepSelected(index + 1);
            return;
        }
    }
    QWidget::mousePressEvent(event);
}

/* ------------------------------------------------------------------ */
/* SwitchRow                                                            */
/* ------------------------------------------------------------------ */

SwitchRow::SwitchRow(const QString &title, const QString &description, bool checked,
                     QWidget *parent)
    : QWidget(parent)
{
    setCursor(Qt::PointingHandCursor);
    setAttribute(Qt::WA_Hover, true);
    auto *row = new QHBoxLayout(this);
    row->setContentsMargins(12, 9, 12, 9);
    row->setSpacing(10);

    auto *texts = new QVBoxLayout;
    texts->setContentsMargins(0, 0, 0, 0);
    texts->setSpacing(2);
    texts->addWidget(Ui::label(title, 11, QFont::Medium, Theme::Text1, this));
    if (!description.isEmpty()) {
        m_description =
            Ui::label(description, 10, QFont::Normal, Theme::Text3, this);
        m_description->setWordWrap(true);
        texts->addWidget(m_description);
    }
    row->addLayout(texts, 1);

    m_switch = new Switch(this);
    m_switch->setChecked(checked);
    m_switch->setAttribute(Qt::WA_TransparentForMouseEvents, true);
    row->addWidget(m_switch, 0, Qt::AlignVCenter);
}

bool SwitchRow::isChecked() const
{
    return m_switch && m_switch->isChecked();
}

void SwitchRow::setChecked(bool checked)
{
    if (m_switch && m_switch->isChecked() != checked) {
        m_switch->setChecked(checked);
        update();
    }
}

void SwitchRow::setDescription(const QString &description)
{
    if (m_description) {
        m_description->setText(description);
    }
}

void SwitchRow::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    painter.setRenderHint(QPainter::Antialiasing, true);
    const QRectF rect = QRectF(this->rect()).adjusted(0.5, 0.5, -0.5, -0.5);
    painter.setPen(QPen(m_hovered ? Theme::BorderStrong : Theme::Border, 1.0));
    painter.setBrush(m_hovered ? Theme::alpha(Theme::Surface3, 0.45)
                               : Theme::alpha(Theme::Surface2, 0.6));
    painter.drawRoundedRect(rect, 8, 8);
}

void SwitchRow::mouseReleaseEvent(QMouseEvent *event)
{
    if (event->button() == Qt::LeftButton && m_switch && rect().contains(event->pos())) {
        const bool next = !m_switch->isChecked();
        m_switch->setChecked(next);
        emit toggled(next);
    }
    QWidget::mouseReleaseEvent(event);
}

void SwitchRow::enterEvent(QEnterEvent *event)
{
    m_hovered = true;
    update();
    QWidget::enterEvent(event);
}

void SwitchRow::leaveEvent(QEvent *event)
{
    m_hovered = false;
    update();
    QWidget::leaveEvent(event);
}

/* ------------------------------------------------------------------ */
/* DropZone                                                             */
/* ------------------------------------------------------------------ */

DropZone::DropZone(QWidget *parent)
    : QWidget(parent)
{
    setAcceptDrops(true);
    m_layout = new QVBoxLayout(this);
    m_layout->setContentsMargins(24, 24, 24, 24);
    m_layout->setSpacing(8);
    m_layout->setAlignment(Qt::AlignCenter);
}

void DropZone::setDragging(bool dragging)
{
    m_dragging = dragging;
    update();
}

void DropZone::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    painter.setRenderHint(QPainter::Antialiasing, true);
    const QRectF rect = QRectF(this->rect()).adjusted(0.5, 0.5, -0.5, -0.5);
    QPen pen(m_dragging ? Theme::alpha(Theme::Primary, 0.7)
                        : Theme::alpha(Theme::BorderStrong, 0.7),
             1.0, Qt::DashLine);
    pen.setDashPattern({4, 4});
    painter.setPen(pen);
    painter.setBrush(m_dragging ? Theme::alpha(Theme::Primary, 0.12)
                                : Theme::alpha(Theme::Surface2, 0.3));
    painter.drawRoundedRect(rect, 8, 8);
}

void DropZone::dragEnterEvent(QDragEnterEvent *event)
{
    if (event->mimeData()->hasUrls()) {
        setDragging(true);
        event->acceptProposedAction();
    }
}

void DropZone::dragLeaveEvent(QDragLeaveEvent *event)
{
    setDragging(false);
    QWidget::dragLeaveEvent(event);
}

void DropZone::dropEvent(QDropEvent *event)
{
    setDragging(false);
    QStringList paths;
    const QList<QUrl> urls = event->mimeData()->urls();
    for (const QUrl &url : urls) {
        if (url.isLocalFile()) {
            paths.append(url.toLocalFile());
        }
    }
    if (!paths.isEmpty()) {
        event->acceptProposedAction();
        emit filesDropped(paths);
    }
}

/* ------------------------------------------------------------------ */
/* StatTile                                                             */
/* ------------------------------------------------------------------ */

StatTile::StatTile(const QString &label, const QString &value, QWidget *parent)
    : QWidget(parent)
{
    setAttribute(Qt::WA_StyledBackground, true);
    setObjectName(QStringLiteral("studioStatTile"));
    setStyleSheet(QStringLiteral(
        "#studioStatTile { background:%1; border:1px solid %2; border-radius:6px; }")
                      .arg(Theme::alpha(Theme::Surface2, 0.55).name(QColor::HexArgb),
                           Theme::Border.name()));
    auto *layout = new QVBoxLayout(this);
    layout->setContentsMargins(10, 6, 10, 6);
    layout->setSpacing(1);
    layout->addWidget(Ui::label(label, 9, QFont::Normal, Theme::Text3, this));
    m_value = Ui::label(value, 11, QFont::Medium, Theme::Text1, this);
    m_value->setWordWrap(false);
    layout->addWidget(m_value);
}

void StatTile::setValue(const QString &value)
{
    if (m_value) {
        m_value->setText(value);
    }
}

void StatTile::paintEvent(QPaintEvent *event)
{
    QWidget::paintEvent(event);
}

/* ------------------------------------------------------------------ */
/* AiBadge                                                              */
/* ------------------------------------------------------------------ */

AiBadge::AiBadge(QWidget *parent)
    : QWidget(parent)
{
    setAttribute(Qt::WA_TransparentForMouseEvents, true);
    setFixedSize(30, 15);
    setToolTip(QStringLiteral("AI 智能成片"));
}

QSize AiBadge::sizeHint() const
{
    return QSize(30, 15);
}

void AiBadge::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    painter.setRenderHint(QPainter::Antialiasing, true);
    const QRectF rect = QRectF(this->rect()).adjusted(0.5, 0.5, -0.5, -0.5);
    painter.setPen(QPen(Theme::alpha(Theme::Ai, 0.4), 1.0));
    painter.setBrush(Theme::Surface2);
    painter.drawRoundedRect(rect, height() / 2.0, height() / 2.0);
    Icons::paint(&painter, QRectF(3, 4, 7, 7), Icons::Glyph::Sparkle, Theme::Ai, 2.0);
    painter.setFont(Theme::font(8, QFont::Medium));
    painter.setPen(Theme::Ai);
    painter.drawText(QRect(11, 0, width() - 12, height()),
                     Qt::AlignLeft | Qt::AlignVCenter, QStringLiteral("AI"));
}

/* ------------------------------------------------------------------ */
/* StageSummaryCard                                                     */
/* ------------------------------------------------------------------ */

StageSummaryCard::StageSummaryCard(QWidget *parent)
    : QWidget(parent)
{
    setFixedHeight(76);
    setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
}

void StageSummaryCard::setData(int number, Icons::Glyph glyph, const QString &label,
                               int status, const QString &statusText,
                               const QString &detail, int progress, bool active)
{
    m_number = number;
    m_glyph = glyph;
    m_label = label;
    m_status = status;
    m_statusText = statusText;
    m_detail = detail;
    m_progress = progress;
    m_active = active;
    update();
}

void StageSummaryCard::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    painter.setRenderHint(QPainter::Antialiasing, true);
    const QRectF rect = QRectF(this->rect()).adjusted(0.5, 0.5, -0.5, -0.5);

    QColor fill = Theme::Surface1;
    QColor border = Theme::Border;
    if (m_active) {
        fill = primarySoft();
        border = Theme::alpha(Theme::Primary, 0.5);
    } else if (m_status == 2) {
        border = Theme::alpha(Theme::Success, 0.25);
    }
    painter.setPen(QPen(border, 1.0));
    painter.setBrush(fill);
    painter.drawRoundedRect(rect, 8, 8);

    const QRectF circle(10, 8, 18, 18);
    QColor circleFill = Theme::Surface2;
    QColor circleBorder = Theme::BorderStrong;
    QColor circleText = Theme::Text3;
    if (m_status == 2) {
        circleFill = Theme::Success;
        circleBorder = Theme::Success;
        circleText = Theme::OnAccent;
    } else if (m_status == 1) {
        circleFill = Theme::Primary;
        circleBorder = Theme::Primary;
        circleText = Theme::OnAccent;
    }
    painter.setPen(QPen(circleBorder, 1.0));
    painter.setBrush(circleFill);
    painter.drawEllipse(circle);
    if (m_status == 2) {
        Icons::paint(&painter, circle.adjusted(5, 5, -5, -5), Icons::Glyph::Check,
                     circleText, 2.4);
    } else {
        painter.setFont(Theme::monoFont(9));
        painter.setPen(circleText);
        painter.drawText(circle, Qt::AlignCenter, QString::number(m_number));
    }

    Icons::paint(&painter, QRectF(34, 10, 12, 12), m_glyph, Theme::Text3, 2.0);
    painter.setFont(Theme::font(10, QFont::Medium));
    painter.setPen(m_active ? Theme::Text1 : Theme::Text2);
    painter.drawText(QRect(50, 5, width() - 105, 20),
                     Qt::AlignLeft | Qt::AlignVCenter, m_label);

    painter.setFont(Theme::font(8));
    painter.setPen(m_status == 2 ? Theme::Success
                                : (m_status == 1 ? Theme::Primary : Theme::Text3));
    painter.drawText(QRect(width() - 58, 5, 48, 20), Qt::AlignRight | Qt::AlignVCenter,
                     m_statusText);

    painter.setFont(Theme::font(8));
    painter.setPen(Theme::Text3);
    const QString detail = QFontMetrics(painter.font()).elidedText(
        m_detail, Qt::ElideRight, qMax(20, width() - 24));
    painter.drawText(QRect(10, 30, width() - 20, 16), Qt::AlignLeft | Qt::AlignVCenter,
                     detail);

    const QRectF track(10, height() - 13, width() - 20, 2);
    painter.setPen(Qt::NoPen);
    painter.setBrush(Theme::Surface3);
    painter.drawRoundedRect(track, 1, 1);
    if (m_progress > 0) {
        painter.setBrush(m_status == 2 ? Theme::Success : Theme::Primary);
        painter.drawRoundedRect(
            QRectF(track.left(), track.top(), track.width() * qBound(0, m_progress, 100)
                                                          / 100.0,
                   track.height()),
            1, 1);
    }
}

} // namespace Studio
