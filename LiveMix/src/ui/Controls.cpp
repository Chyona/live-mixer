#include "Controls.h"

#include <QAbstractItemView>
#include <QApplication>
#include <QEasingCurve>
#include <QEnterEvent>
#include <QFontMetrics>
#include <QGraphicsOpacityEffect>
#include <QHBoxLayout>
#include <QLabel>
#include <QLineEdit>
#include <QLinearGradient>
#include <QMouseEvent>
#include <QPainter>
#include <QPainterPath>
#include <QPropertyAnimation>
#include <QRadialGradient>
#include <QShowEvent>
#include <QStyleOption>
#include <QTimer>
#include <QVBoxLayout>

#include <algorithm>

namespace {

QString cssColor(const QColor &color)
{
    if (color.alpha() == 255) {
        return color.name(QColor::HexRgb);
    }
    return QStringLiteral("rgba(%1,%2,%3,%4)")
        .arg(color.red())
        .arg(color.green())
        .arg(color.blue())
        .arg(color.alpha());
}

void drawPill(QPainter *painter, const QRectF &rect, const QColor &fill,
              const QColor &border, int radius = -1)
{
    painter->save();
    painter->setRenderHint(QPainter::Antialiasing, true);
    if (fill.alpha() > 0) {
        painter->setBrush(fill);
    } else {
        painter->setBrush(Qt::NoBrush);
    }
    if (border.alpha() > 0) {
        painter->setPen(QPen(border, 1.0));
    } else {
        painter->setPen(Qt::NoPen);
    }
    const qreal r = radius < 0 ? rect.height() / 2.0 : radius;
    painter->drawRoundedRect(rect.adjusted(0.5, 0.5, -0.5, -0.5), r, r);
    painter->restore();
}

QColor chipColor(Chip::Tone tone)
{
    switch (tone) {
    case Chip::Ok:
        return Theme::Success;
    case Chip::Run:
        return Theme::Primary;
    case Chip::Ai:
        return Theme::Ai;
    case Chip::Error:
    case Chip::Live:
        return Theme::Danger;
    case Chip::Neutral:
        break;
    }
    return Theme::Text2;
}

} // namespace

namespace Ui {

QLabel *label(const QString &text, int pixelSize, int weight, const QColor &color,
              QWidget *parent)
{
    auto *result = new QLabel(text, parent);
    result->setFont(Theme::font(pixelSize, weight));
    result->setStyleSheet(QStringLiteral("color:%1; background:transparent;").arg(cssColor(color)));
    result->setContentsMargins(0, 0, 0, 0);
    result->setMargin(0);
    result->setTextFormat(Qt::PlainText);
    result->setAttribute(Qt::WA_TransparentForMouseEvents, true);
    return result;
}

QLabel *richLabel(const QString &html, int pixelSize, const QColor &color, QWidget *parent)
{
    auto *result = new QLabel(html, parent);
    result->setFont(Theme::font(pixelSize));
    result->setTextFormat(Qt::RichText);
    result->setStyleSheet(QStringLiteral("color:%1; background:transparent;").arg(cssColor(color)));
    result->setContentsMargins(0, 0, 0, 0);
    result->setMargin(0);
    result->setTextInteractionFlags(Qt::NoTextInteraction);
    return result;
}

QVBoxLayout *vbox(QWidget *owner, const QMargins &margins, int spacing)
{
    auto *layout = new QVBoxLayout(owner);
    layout->setContentsMargins(margins);
    layout->setSpacing(spacing);
    return layout;
}

QHBoxLayout *hbox(QWidget *owner, const QMargins &margins, int spacing)
{
    auto *layout = new QHBoxLayout(owner);
    layout->setContentsMargins(margins);
    layout->setSpacing(spacing);
    return layout;
}

QLabel *separator(bool vertical, QWidget *parent)
{
    auto *line = new QLabel(parent);
    if (vertical) {
        line->setFixedWidth(1);
        line->setSizePolicy(QSizePolicy::Fixed, QSizePolicy::Expanding);
    } else {
        line->setFixedHeight(1);
        line->setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
    }
    line->setStyleSheet(QStringLiteral("background:%1;").arg(Theme::Border.name()));
    return line;
}

QString toneColorName(const QString &tone)
{
    if (tone == QLatin1String("ok") || tone == QLatin1String("success")) {
        return QStringLiteral("ok");
    }
    if (tone == QLatin1String("run") || tone == QLatin1String("primary")) {
        return QStringLiteral("run");
    }
    if (tone == QLatin1String("ai")) {
        return QStringLiteral("ai");
    }
    if (tone == QLatin1String("err") || tone == QLatin1String("danger")) {
        return QStringLiteral("err");
    }
    return QStringLiteral("neutral");
}

} // namespace Ui

// -----------------------------------------------------------------------------
// Card
// -----------------------------------------------------------------------------

Card::Card(QWidget *parent, int radius)
    : Card(Theme::Surface1, Theme::Border, radius, parent)
{
}

Card::Card(const QColor &fill, const QColor &border, int radius, QWidget *parent)
    : QWidget(parent)
    , m_radius(radius)
    , m_fill(fill)
    , m_border(border)
    , m_hoverFill(fill)
    , m_hoverBorder(border)
{
    setAttribute(Qt::WA_StyledBackground, false);
    setAutoFillBackground(false);
    m_layout = new QVBoxLayout(this);
    m_layout->setContentsMargins(0, 0, 0, 0);
    m_layout->setSpacing(0);
}

void Card::setCardColors(const QColor &fill, const QColor &border)
{
    m_fill = fill;
    m_border = border;
    m_hoverFill = fill;
    m_hoverBorder = border;
    update();
}

void Card::setRadius(int radius)
{
    m_radius = radius;
    update();
}

void Card::setHoverColors(const QColor &fill, const QColor &border)
{
    m_hoverFill = fill;
    m_hoverBorder = border;
    update();
}

void Card::setClickable(bool clickable)
{
    m_clickable = clickable;
    m_hoverEnabled = m_hoverEnabled || clickable;
    setCursor(clickable ? Qt::PointingHandCursor : Qt::ArrowCursor);
    setAttribute(Qt::WA_Hover, m_hoverEnabled);
    update();
}

void Card::setHoverEnabled(bool enabled)
{
    m_hoverEnabled = enabled;
    setAttribute(Qt::WA_Hover, enabled);
    update();
}

void Card::setBorderWidth(qreal width)
{
    m_borderWidth = width;
    update();
}

void Card::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    painter.setRenderHint(QPainter::Antialiasing, true);
    const bool active = m_hoverEnabled && (m_hovered || m_pressed);
    const QColor fill = active && m_hoverFill.alpha() > 0 ? m_hoverFill : m_fill;
    const QColor border = active ? m_hoverBorder : m_border;

    if (fill.alpha() > 0) {
        painter.setBrush(fill);
    } else {
        painter.setBrush(Qt::NoBrush);
    }
    if (border.alpha() > 0 && m_borderWidth > 0) {
        painter.setPen(QPen(border, m_borderWidth));
    } else {
        painter.setPen(Qt::NoPen);
    }
    const QRectF r = rect().adjusted(m_borderWidth / 2.0, m_borderWidth / 2.0,
                                     -m_borderWidth / 2.0, -m_borderWidth / 2.0);
    painter.drawRoundedRect(r, m_radius, m_radius);
}

void Card::enterEvent(QEnterEvent *event)
{
    if (m_hoverEnabled) {
        m_hovered = true;
        update();
    }
    QWidget::enterEvent(event);
}

void Card::leaveEvent(QEvent *event)
{
    m_hovered = false;
    m_pressed = false;
    update();
    QWidget::leaveEvent(event);
}

void Card::mousePressEvent(QMouseEvent *event)
{
    if (m_clickable && event->button() == Qt::LeftButton) {

        m_pressed = true;
        update();
        event->accept();
        return;
    }
    QWidget::mousePressEvent(event);
}

void Card::mouseReleaseEvent(QMouseEvent *event)
{
    const bool shouldClick = m_clickable && m_pressed && rect().contains(event->position().toPoint());
    m_pressed = false;
    update();
    if (shouldClick && event->button() == Qt::LeftButton) {
        emit clicked();
        event->accept();
        return;
    }
    QWidget::mouseReleaseEvent(event);
}

// -----------------------------------------------------------------------------
// Button
// -----------------------------------------------------------------------------

Button::Button(const QString &text, Variant variant, QWidget *parent)
    : QAbstractButton(parent)
    , m_variant(variant)
{
    setText(text);
    setCursor(Qt::PointingHandCursor);
    setFocusPolicy(Qt::StrongFocus);
    setAttribute(Qt::WA_Hover, true);
    setSizePolicy(QSizePolicy::Preferred, QSizePolicy::Fixed);
    setFixedHeight(m_height);
    setFont(Theme::font(12));
    setAccessibleName(text);
}

void Button::setLeadingGlyph(Icons::Glyph glyph)
{
    m_hasGlyph = true;
    m_glyph = glyph;
    updateGeometry();
    update();
}

void Button::setCompact(bool compact)
{
    m_fontSize = compact ? 11 : 12;
    m_height = compact ? 24 : 32;
    m_padding = compact ? 9 : 13;
    setFixedHeight(m_height);
    updateGeometry();
    update();
}

void Button::setVariant(Variant variant)
{
    m_variant = variant;
    update();
}

void Button::setProgressPercent(int percent)
{
    const int next = percent < 0 ? -1 : qBound(0, percent, 100);
    if (next == m_progressPercent) {
        return;
    }
    m_progressPercent = next;
    update();
}

void Button::setCornerRadius(int radius)
{
    m_radius = radius;
    update();
}

QSize Button::sizeHint() const
{
    const QFont f = Theme::font(m_fontSize, (m_variant == Primary || m_variant == Ai)
                                                 ? QFont::DemiBold
                                                 : QFont::Normal);
    const QFontMetrics metrics(f);
    int width = metrics.horizontalAdvance(text()) + m_padding * 2 + 2;
    if (m_hasGlyph) {
        width += 13 + 6;
    }
    return QSize(width, m_height);
}

QSize Button::minimumSizeHint() const
{
    return sizeHint();
}

void Button::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    painter.setRenderHint(QPainter::Antialiasing, true);

    QColor fill;
    QColor border;
    QColor textColor;
    int weight = QFont::Normal;

    switch (m_variant) {
    case Primary:
        fill = m_hovered ? Theme::PrimaryHover : Theme::Primary;
        border = fill;
        textColor = Theme::OnAccent;
        weight = 650;
        break;
    case Ai:
        fill = m_hovered ? Theme::mix(Theme::Ai, Qt::white, 0.08) : Theme::Ai;
        border = Theme::Ai;
        textColor = QColor("#1A1204");
        weight = 650;
        break;
    case Ghost:
        fill = m_hovered ? Theme::Surface2 : QColor(Qt::transparent);
        border = Theme::BorderStrong;
        textColor = Theme::Text1;
        break;
    case DangerSoft:
        fill = m_hovered ? Theme::alpha(Theme::Danger, 0.20) : Theme::alpha(Theme::Danger, 0.13);
        border = Theme::alpha(Theme::Danger, 0.40);
        textColor = Theme::Danger;
        break;
    case Danger:
        fill = m_hovered ? Theme::mix(Theme::Danger, Qt::white, 0.06) : Theme::Danger;
        border = Theme::Danger;
        textColor = Qt::white;
        weight = 600;
        break;
    case Default:
    default:
        fill = m_hovered ? Theme::Surface3 : Theme::Surface2;
        border = Theme::BorderStrong;
        textColor = Theme::Text1;
        break;
    }

    if (isDown()) {
        fill = Theme::mix(fill.isValid() ? fill : Qt::transparent, Qt::black, 0.08);
    }
    if (!isEnabled()) {
        fill = Theme::alpha(fill, 0.55);
        border = Theme::alpha(border, 0.45);
        textColor = Theme::alpha(textColor, 0.55);
    }

    drawPill(&painter, QRectF(rect()), fill, border, m_radius);

    // 下载进度底色：从左向右覆盖按钮，文字仍保持可读。
    if (m_progressPercent > 0 && width() > 0) {
        painter.save();
        QPainterPath clip;
        clip.addRoundedRect(QRectF(rect()), m_radius, m_radius);
        painter.setClipPath(clip);
        painter.setPen(Qt::NoPen);
        painter.setBrush(QColor(11, 12, 16, 52));
        painter.drawRect(
            QRectF(0, 0, width() * (m_progressPercent / 100.0), height()));
        painter.restore();
    }

    if (hasFocus()) {
        painter.setBrush(Qt::NoBrush);
        painter.setPen(QPen(Theme::alpha(Theme::Text1, 0.28), 1.0));
        painter.drawRoundedRect(QRectF(rect()).adjusted(2.5, 2.5, -2.5, -2.5), 5.5, 5.5);
    }

    const QFont f = Theme::font(m_fontSize, weight);
    painter.setFont(f);
    painter.setPen(textColor);
    const QFontMetrics metrics(f);
    const int iconPart = m_hasGlyph ? 13 + 6 : 0;
    const int contentWidth = metrics.horizontalAdvance(text()) + iconPart;
    int x = (width() - contentWidth) / 2;
    const QRect textRect(x + iconPart, 0, metrics.horizontalAdvance(text()) + 2, height());

    if (m_hasGlyph) {
        Icons::paint(&painter, QRectF(x, (height() - 13) / 2.0, 13, 13), m_glyph,
                     textColor, 2.0);
    }
    painter.drawText(textRect, Qt::AlignLeft | Qt::AlignVCenter, text());
}

void Button::enterEvent(QEnterEvent *event)
{
    m_hovered = true;
    update();
    QAbstractButton::enterEvent(event);
}

void Button::leaveEvent(QEvent *event)
{
    m_hovered = false;
    update();
    QAbstractButton::leaveEvent(event);
}

void Button::mousePressEvent(QMouseEvent *event)
{
    update();
    QAbstractButton::mousePressEvent(event);
}

void Button::mouseReleaseEvent(QMouseEvent *event)
{
    update();
    QAbstractButton::mouseReleaseEvent(event);
}

// -----------------------------------------------------------------------------
// GlyphButton
// -----------------------------------------------------------------------------

GlyphButton::GlyphButton(Icons::Glyph glyph, int side, QWidget *parent)
    : QAbstractButton(parent)
    , m_glyph(glyph)
    , m_side(side)
    , m_glyphSize(side - 2)
    , m_normal(Theme::Text3)
    , m_hoverColor(Theme::Primary)
    , m_background(Qt::transparent)
    , m_hoverBackground(Qt::transparent)
{
    setFixedSize(side, side);
    setCursor(Qt::PointingHandCursor);
    setAttribute(Qt::WA_Hover, true);
    setFocusPolicy(Qt::NoFocus);
}

void GlyphButton::setGlyph(Icons::Glyph glyph)
{
    m_glyph = glyph;
    update();
}

void GlyphButton::setColors(const QColor &normal, const QColor &hover)
{
    m_normal = normal;
    m_hoverColor = hover;
    update();
}

void GlyphButton::setBackgroundColors(const QColor &normal, const QColor &hover)
{
    m_background = normal;
    m_hoverBackground = hover;
    update();
}

void GlyphButton::setGlyphSize(qreal size)
{
    m_glyphSize = size;
    update();
}

void GlyphButton::setToolTipText(const QString &text)
{
    setToolTip(text);
}

QSize GlyphButton::sizeHint() const
{
    return QSize(m_side, m_side);
}

void GlyphButton::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    painter.setRenderHint(QPainter::Antialiasing, true);

    const QColor background = m_hovered ? m_hoverBackground : m_background;
    if (background.alpha() > 0) {
        painter.setPen(Qt::NoPen);
        painter.setBrush(background);
        painter.drawRoundedRect(QRectF(rect()).adjusted(0.5, 0.5, -0.5, -0.5), 6, 6);
    }

    const QColor color = m_hovered ? m_hoverColor : m_normal;
    const QRectF glyphRect((width() - m_glyphSize) / 2.0,
                           (height() - m_glyphSize) / 2.0, m_glyphSize, m_glyphSize);
    Icons::paint(&painter, glyphRect, m_glyph, isEnabled() ? color : Theme::alpha(color, 0.45),
                 2.0);
}

void GlyphButton::enterEvent(QEnterEvent *event)
{
    m_hovered = true;
    update();
    QAbstractButton::enterEvent(event);
}

void GlyphButton::leaveEvent(QEvent *event)
{
    m_hovered = false;
    update();
    QAbstractButton::leaveEvent(event);
}

// -----------------------------------------------------------------------------
// LinkLabel
// -----------------------------------------------------------------------------

LinkLabel::LinkLabel(const QString &text, QWidget *parent)
    : QAbstractButton(parent)
{
    setText(text);
    setCursor(Qt::PointingHandCursor);
    setFocusPolicy(Qt::NoFocus);
    setAttribute(Qt::WA_Hover, true);
    setFont(Theme::font(m_pixelSize));
    setSizePolicy(QSizePolicy::Preferred, QSizePolicy::Fixed);
    setFixedHeight(QFontMetrics(Theme::font(m_pixelSize)).height() + 3);
    setAccessibleName(text);
}

void LinkLabel::setTextColor(const QColor &color)
{
    m_color = color;
    update();
}

void LinkLabel::setPixelSize(int pixelSize)
{
    m_pixelSize = pixelSize;
    setFont(m_mono ? Theme::monoFont(m_pixelSize) : Theme::font(m_pixelSize));
    setFixedHeight(QFontMetrics(m_mono ? Theme::monoFont(m_pixelSize)
                                       : Theme::font(m_pixelSize))
                           .height()
                   + 3);
    updateGeometry();
    update();
}

void LinkLabel::setMonoFont(bool mono)
{
    if (m_mono == mono) {
        return;
    }
    m_mono = mono;
    setFont(m_mono ? Theme::monoFont(m_pixelSize) : Theme::font(m_pixelSize));
    setFixedHeight(QFontMetrics(m_mono ? Theme::monoFont(m_pixelSize)
                                       : Theme::font(m_pixelSize))
                           .height()
                   + 3);
    updateGeometry();
    update();
}

void LinkLabel::setHoverColor(const QColor &color)
{
    m_hoverColor = color;
    update();
}

void LinkLabel::setDisabledVisual(bool disabled)
{
    m_disabled = disabled;
    setCursor(disabled ? Qt::ArrowCursor : Qt::PointingHandCursor);
    update();
}

QSize LinkLabel::sizeHint() const
{
    const QFontMetrics metrics(m_mono ? Theme::monoFont(m_pixelSize)
                                      : Theme::font(m_pixelSize));
    return QSize(metrics.horizontalAdvance(text()) + 2, metrics.height() + 3);
}

void LinkLabel::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    painter.setRenderHint(QPainter::TextAntialiasing, true);
    QFont f = m_mono ? Theme::monoFont(m_pixelSize) : Theme::font(m_pixelSize);
    f.setUnderline(m_hovered && !m_disabled);
    painter.setFont(f);
    painter.setPen(m_disabled ? Theme::Text3 : (m_hovered ? m_hoverColor : m_color));
    painter.drawText(rect(), Qt::AlignLeft | Qt::AlignVCenter, text());
}

void LinkLabel::enterEvent(QEnterEvent *event)
{
    if (!m_disabled) {
        m_hovered = true;
        update();
    }
    QAbstractButton::enterEvent(event);
}

void LinkLabel::leaveEvent(QEvent *event)
{
    m_hovered = false;
    update();
    QAbstractButton::leaveEvent(event);
}

// -----------------------------------------------------------------------------
// Chip
// -----------------------------------------------------------------------------

Chip::Chip(const QString &text, Tone tone, QWidget *parent)
    : QWidget(parent)
    , m_text(text)
    , m_tone(tone)
{
    setFont(Theme::font(11));
    setFixedHeight(20);
    setSizePolicy(QSizePolicy::Fixed, QSizePolicy::Fixed);
    setAttribute(Qt::WA_TransparentForMouseEvents, true);
    m_showDot = (tone == Live);
}

void Chip::setText(const QString &text)
{
    m_text = text;
    updateGeometry();
    update();
}

void Chip::setTone(Tone tone)
{
    m_tone = tone;
    m_showDot = (tone == Live);
    update();
}

void Chip::setShowDot(bool showDot)
{
    m_showDot = showDot;
    updateGeometry();
    update();
}

void Chip::setBlinking(bool blinking)
{
    m_blinking = blinking;
    if (blinking) {
        if (!m_blinkTimer) {
            m_blinkTimer = new QTimer(this);
            m_blinkTimer->setInterval(600);
            connect(m_blinkTimer, &QTimer::timeout, this, [this] {
                m_blinkOn = !m_blinkOn;
                update();
            });
        }
        m_blinkTimer->start();
    } else if (m_blinkTimer) {
        m_blinkTimer->stop();
        m_blinkOn = true;
    }
    update();
}

QSize Chip::sizeHint() const
{
    const QFontMetrics metrics(Theme::font(11));
    int width = metrics.horizontalAdvance(m_text) + 16 + 2;
    if (m_showDot) {
        width += 6 + 5;
    }
    return QSize(width, 20);
}

void Chip::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    painter.setRenderHint(QPainter::Antialiasing, true);

    const QColor tone = chipColor(m_tone);
    QColor fill = Theme::alpha(tone, 0.13);
    QColor border = Theme::alpha(tone, 0.35);
    QColor textColor = tone;
    if (m_tone == Neutral) {
        fill = Qt::transparent;
        border = Theme::Border;
        textColor = Theme::Text2;
    }

    painter.setBrush(fill.alpha() > 0 ? fill : QBrush(Qt::NoBrush));
    painter.setPen(QPen(border, 1.0));
    painter.drawRoundedRect(QRectF(rect()).adjusted(0.5, 0.5, -0.5, -0.5), 10, 10);

    int x = 8;
    if (m_showDot) {
        painter.setPen(Qt::NoPen);
        QColor dot = Theme::alpha(tone, 0.9);
        if (m_blinking && !m_blinkOn) {
            dot = Theme::alpha(tone, 0.25);
        }
        painter.setBrush(dot);
        painter.drawEllipse(QPointF(x + 3, height() / 2.0), 3, 3);
        x += 11;
    }

    painter.setFont(Theme::font(11));
    painter.setPen(textColor);
    painter.drawText(QRect(x, 0, width() - x - 6, height()), Qt::AlignLeft | Qt::AlignVCenter,
                     m_text);
}

// -----------------------------------------------------------------------------
// MediaThumbnail
// -----------------------------------------------------------------------------

namespace {

QPainterPath topRoundedPath(const QRectF &rect, qreal radius)
{
    QPainterPath path;
    path.moveTo(rect.left(), rect.top() + radius);
    path.quadTo(rect.topLeft(), QPointF(rect.left() + radius, rect.top()));
    path.lineTo(rect.right() - radius, rect.top());
    path.quadTo(rect.topRight(), QPointF(rect.right(), rect.top() + radius));
    path.lineTo(rect.bottomRight());
    path.lineTo(rect.bottomLeft());
    path.closeSubpath();
    return path;
}

} // namespace

MediaThumbnail::MediaThumbnail(Mode mode, QWidget *parent)
    : QWidget(parent)
    , m_mode(mode)
{
    QSizePolicy policy(QSizePolicy::Preferred, QSizePolicy::Preferred);
    policy.setHeightForWidth(true);
    setSizePolicy(policy);
    setMinimumHeight(80);
    if (m_mode == Recording) {
        m_blinkTimer = new QTimer(this);
        m_blinkTimer->setInterval(600);
        connect(m_blinkTimer, &QTimer::timeout, this, [this] {
            m_blinkOn = !m_blinkOn;
            update();
        });
        m_blinkTimer->start();
    }
}

void MediaThumbnail::setDuration(const QString &duration)
{
    m_duration = duration;
    update();
}

void MediaThumbnail::setReadyBadge(const QString &text)
{
    m_readyBadge = text;
    update();
}

void MediaThumbnail::setReadyBadgeTone(BadgeTone tone)
{
    if (m_readyBadgeTone == tone) {
        return;
    }
    m_readyBadgeTone = tone;
    update();
}

void MediaThumbnail::setRecordingLabel(const QString &text)
{
    m_recordingLabel = text;
    update();
}

void MediaThumbnail::setPreviewImage(const QImage &image)
{
    m_previewImage = image;
    update();
}

void MediaThumbnail::setTopRadius(int radius)
{
    m_topRadius = radius;
    update();
}

bool MediaThumbnail::hasHeightForWidth() const
{
    return true;
}

int MediaThumbnail::heightForWidth(int width) const
{
    return qMax(72, qRound(width * 9.0 / 16.0));
}

QSize MediaThumbnail::sizeHint() const
{
    return QSize(280, 158);
}

QSize MediaThumbnail::minimumSizeHint() const
{
    return QSize(120, 68);
}

void MediaThumbnail::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    painter.setRenderHint(QPainter::Antialiasing, true);
    const QRectF area(rect());
    const QPainterPath clip = topRoundedPath(area, m_topRadius);
    painter.save();
    painter.setClipPath(clip);

    if (!m_previewImage.isNull() && area.width() > 0 && area.height() > 0) {
        // 已有封面帧：等比铺满并居中裁剪，底部压暗保证时长标签可读。
        const QImage scaled = m_previewImage.scaled(
            area.size().toSize(), Qt::KeepAspectRatioByExpanding,
            Qt::SmoothTransformation);
        painter.drawImage(
            QPointF((area.width() - scaled.width()) / 2.0,
                    (area.height() - scaled.height()) / 2.0),
            scaled);
        QLinearGradient shade(area.topLeft(), area.bottomLeft());
        shade.setColorAt(0.55, QColor(0, 0, 0, 0));
        shade.setColorAt(1.0, QColor(0, 0, 0, 95));
        painter.fillRect(area, shade);
    } else {
        if (m_mode == Recording) {
            QRadialGradient radial(
                QPointF(area.center().x(), area.top() + area.height() * 0.2),
                area.width() * 0.72);
            radial.setColorAt(0.0, QColor("#3A2030"));
            radial.setColorAt(0.7, QColor("#211826"));
            radial.setColorAt(1.0, QColor("#151824"));
            painter.fillRect(area, radial);
        } else {
            QLinearGradient linear(area.topLeft(), area.bottomRight());
            linear.setColorAt(0.0, QColor("#262D4A"));
            linear.setColorAt(0.58, QColor("#1B2033"));
            linear.setColorAt(1.0, QColor("#151824"));
            painter.fillRect(area, linear);

            QRadialGradient glow(QPointF(area.left() + area.width() * 0.22,
                                         area.top() + area.height() * 0.16),
                                 area.width() * 0.7);
            glow.setColorAt(0.0, QColor(75, 96, 158, 34));
            glow.setColorAt(1.0, QColor(75, 96, 158, 0));
            painter.fillRect(area, glow);
        }

        // A faint diagonal light sweep, matching the placeholder artwork in the spec.
        QLinearGradient sweep(QPointF(area.left() - area.width() * 0.25, area.top()),
                              QPointF(area.left() + area.width() * 0.75, area.bottom()));
        sweep.setColorAt(0.25, QColor(255, 255, 255, 0));
        sweep.setColorAt(0.5, QColor(255, 255, 255, 9));
        sweep.setColorAt(0.75, QColor(255, 255, 255, 0));
        painter.fillRect(area, sweep);
    }
    painter.restore();

    painter.setFont(Theme::monoFont(10));
    painter.setRenderHint(QPainter::TextAntialiasing, true);

    if (!m_duration.isEmpty()) {
        const QFontMetrics metrics(Theme::monoFont(10));
        const int textWidth = metrics.horizontalAdvance(m_duration);
        const QRectF pill(area.right() - 8 - textWidth - 12, area.bottom() - 6 - 18,
                          textWidth + 12, 18);
        painter.setPen(Qt::NoPen);
        painter.setBrush(QColor(0, 0, 0, 166));
        painter.drawRoundedRect(pill, 4, 4);
        painter.setPen(QColor(255, 255, 255, 235));
        painter.drawText(pill, Qt::AlignCenter, m_duration);
    }

    if (!m_readyBadge.isEmpty()) {
        const QColor tone = m_readyBadgeTone == BadgeRun
            ? Theme::Primary
            : (m_readyBadgeTone == BadgeError ? Theme::Danger : Theme::Success);
        const QFontMetrics metrics(Theme::font(10));
        const int textWidth = metrics.horizontalAdvance(m_readyBadge);
        const QRectF pill(8, 8, textWidth + 16, 19);
        painter.setPen(QPen(Theme::alpha(tone, 0.35), 1.0));
        painter.setBrush(Theme::alpha(tone, 0.15));
        painter.drawRoundedRect(pill, 5, 5);
        painter.setFont(Theme::font(10));
        painter.setPen(tone);
        painter.drawText(pill, Qt::AlignCenter, m_readyBadge);
    }

    if (!m_recordingLabel.isEmpty()) {
        const QFontMetrics metrics(Theme::monoFont(10));
        const int textWidth = metrics.horizontalAdvance(m_recordingLabel);
        const QRectF pill(8, 8, textWidth + 23, 20);
        painter.setPen(Qt::NoPen);
        painter.setBrush(Theme::alpha(Theme::Danger, 0.92));
        painter.drawRoundedRect(pill, 5, 5);
        painter.setBrush(QColor(255, 255, 255, m_blinkOn ? 255 : 70));
        painter.drawEllipse(QPointF(pill.left() + 8, pill.center().y()), 3, 3);
        painter.setFont(Theme::monoFont(10));
        painter.setPen(Qt::white);
        painter.drawText(pill.adjusted(16, 0, -2, 0), Qt::AlignLeft | Qt::AlignVCenter,
                         m_recordingLabel);
    }
}

void MediaThumbnail::showEvent(QShowEvent *event)
{
    if (m_blinkTimer) {
        m_blinkTimer->start();
    }
    QWidget::showEvent(event);
}

void MediaThumbnail::hideEvent(QHideEvent *event)
{
    if (m_blinkTimer) {
        m_blinkTimer->stop();
    }
    QWidget::hideEvent(event);
}

// -----------------------------------------------------------------------------
// ElidedLabel
// -----------------------------------------------------------------------------

ElidedLabel::ElidedLabel(const QString &text, QWidget *parent)
    : QWidget(parent)
    , m_text(text)
{
    setSizePolicy(QSizePolicy::Ignored, QSizePolicy::Fixed);
    setMinimumWidth(0);
    setFixedHeight(QFontMetrics(Theme::font(m_pixelSize)).height() + 2);
    setToolTip(text);
    setAttribute(Qt::WA_TransparentForMouseEvents, true);
}

void ElidedLabel::setText(const QString &text)
{
    m_text = text;
    setToolTip(text);
    update();
}

void ElidedLabel::setTextColor(const QColor &color)
{
    m_color = color;
    update();
}

void ElidedLabel::setPixelSize(int pixelSize)
{
    m_pixelSize = pixelSize;
    setFixedHeight(QFontMetrics(Theme::font(m_pixelSize, m_weight)).height() + 2);
    updateGeometry();
    update();
}

void ElidedLabel::setFontWeight(int weight)
{
    m_weight = weight;
    setFixedHeight(QFontMetrics(Theme::font(m_pixelSize, m_weight)).height() + 2);
    updateGeometry();
    update();
}

void ElidedLabel::setMonoFont(bool mono)
{
    if (m_mono == mono) {
        return;
    }
    m_mono = mono;
    setFixedHeight(QFontMetrics(labelFont()).height() + 2);
    updateGeometry();
    update();
}

QFont ElidedLabel::labelFont() const
{
    return m_mono ? Theme::monoFont(m_pixelSize, m_weight)
                  : Theme::font(m_pixelSize, m_weight);
}

QSize ElidedLabel::sizeHint() const
{
    const QFontMetrics metrics(labelFont());
    return QSize(metrics.horizontalAdvance(m_text), metrics.height() + 2);
}

QSize ElidedLabel::minimumSizeHint() const
{
    return QSize(0, QFontMetrics(labelFont()).height() + 2);
}

void ElidedLabel::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    painter.setRenderHint(QPainter::TextAntialiasing, true);
    const QFont font = labelFont();
    painter.setFont(font);
    painter.setPen(m_color);
    const QString shown =
        QFontMetrics(font).elidedText(m_text, Qt::ElideRight, width());
    painter.drawText(rect(), Qt::AlignLeft | Qt::AlignVCenter, shown);
}

// -----------------------------------------------------------------------------
// ProgressBar
// -----------------------------------------------------------------------------

ProgressBar::ProgressBar(QWidget *parent)
    : QWidget(parent)
{
    setFixedHeight(4);
    setMinimumWidth(40);
    setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
}

void ProgressBar::setValue(int percent)
{
    m_value = qBound(0, percent, 100);
    update();
}

void ProgressBar::setBarColor(const QColor &color)
{
    m_barColor = color;
    update();
}

void ProgressBar::setTrackColor(const QColor &color)
{
    m_trackColor = color;
    update();
}

QSize ProgressBar::sizeHint() const
{
    return QSize(100, 4);
}

void ProgressBar::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    painter.setRenderHint(QPainter::Antialiasing, true);
    const QRectF track(0, 0, width(), 4);
    painter.setPen(Qt::NoPen);
    painter.setBrush(m_trackColor);
    painter.drawRoundedRect(track, 2, 2);
    if (m_value > 0) {
        const qreal filledWidth = std::max(4.0, width() * m_value / 100.0);
        painter.setBrush(m_barColor);
        painter.drawRoundedRect(QRectF(0, 0, qMin<qreal>(filledWidth, width()), 4), 2, 2);
    }
}

// -----------------------------------------------------------------------------
// NavItem
// -----------------------------------------------------------------------------

NavItem::NavItem(Icons::Glyph glyph, const QString &text, const QString &badge, QWidget *parent)
    : QAbstractButton(parent)
    , m_glyph(glyph)
    , m_badge(badge)
{
    setText(text);
    setFixedHeight(34);
    setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
    setCursor(Qt::PointingHandCursor);
    setAttribute(Qt::WA_Hover, true);
    setFocusPolicy(Qt::NoFocus);
    setAccessibleName(text);
}

void NavItem::setActive(bool active)
{
    m_active = active;
    update();
}

void NavItem::setBadge(const QString &badge)
{
    if (m_badge == badge) {
        return;
    }
    m_badge = badge;
    update();
}

QSize NavItem::sizeHint() const
{
    return QSize(180, 34);
}

void NavItem::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    painter.setRenderHint(QPainter::Antialiasing, true);

    if (m_active) {
        painter.setBrush(Theme::alpha(Theme::Primary, 0.14));
        painter.setPen(QPen(Theme::alpha(Theme::Primary, 0.25), 1.0));
        painter.drawRoundedRect(QRectF(rect()).adjusted(0.5, 0.5, -0.5, -0.5), 7, 7);
    } else if (m_hovered) {
        painter.setBrush(Theme::Surface2);
        painter.setPen(Qt::NoPen);
        painter.drawRoundedRect(QRectF(rect()).adjusted(0.5, 0.5, -0.5, -0.5), 7, 7);
    }

    const QColor contentColor = m_active ? Theme::Primary
                                         : (m_hovered ? Theme::Text1 : Theme::Text2);
    Icons::paint(&painter, QRectF(10, (height() - 16) / 2.0, 16, 16), m_glyph,
                 contentColor, 2.0);

    const QFont f = Theme::font(13);
    painter.setFont(f);
    painter.setPen(contentColor);
    painter.drawText(QRect(36, 0, width() - 36 - (m_badge.isEmpty() ? 12 : 46), height()),
                     Qt::AlignLeft | Qt::AlignVCenter, text());

    if (!m_badge.isEmpty()) {
        const QFont bf = Theme::monoFont(10);
        const QFontMetrics metrics(bf);
        const int badgeWidth = metrics.horizontalAdvance(m_badge) + 12;
        const QRectF badgeRect(width() - 10 - badgeWidth, (height() - 17) / 2.0, badgeWidth, 17);
        painter.setBrush(m_active ? Theme::alpha(Theme::Primary, 0.20) : Theme::Surface3);
        painter.setPen(Qt::NoPen);
        painter.drawRoundedRect(badgeRect, 8.5, 8.5);
        painter.setFont(bf);
        painter.setPen(m_active ? Theme::Primary : Theme::Text2);
        painter.drawText(badgeRect, Qt::AlignCenter, m_badge);
    }
}

void NavItem::enterEvent(QEnterEvent *event)
{
    m_hovered = true;
    update();
    QAbstractButton::enterEvent(event);
}

void NavItem::leaveEvent(QEvent *event)
{
    m_hovered = false;
    update();
    QAbstractButton::leaveEvent(event);
}

// -----------------------------------------------------------------------------
// CheckBox
// -----------------------------------------------------------------------------

CheckBox::CheckBox(const QString &text, QWidget *parent)
    : QAbstractButton(parent)
{
    setText(text);
    setCheckable(true);
    setCursor(Qt::PointingHandCursor);
    setFocusPolicy(Qt::StrongFocus);
    setAttribute(Qt::WA_Hover, true);
    setFont(Theme::font(13));
    setSizePolicy(QSizePolicy::Preferred, QSizePolicy::Fixed);
    setFixedHeight(20);
    setAccessibleName(text);
}

QSize CheckBox::sizeHint() const
{
    const QFontMetrics metrics(Theme::font(13));
    return QSize(22 + metrics.horizontalAdvance(text()) + 4, 20);
}

void CheckBox::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    painter.setRenderHint(QPainter::Antialiasing, true);

    const QRectF box(0.5, (height() - 14) / 2.0 + 0.5, 13, 13);
    if (isChecked()) {
        painter.setBrush(Theme::Primary);
        painter.setPen(QPen(Theme::Primary, 1.0));
    } else {
        painter.setBrush(Theme::Surface2);
        painter.setPen(QPen(m_hovered ? Theme::BorderStrong : Theme::Border, 1.0));
    }
    painter.drawRoundedRect(box, 3, 3);

    if (isChecked()) {
        Icons::paint(&painter, QRectF(3.5, (height() - 9) / 2.0, 9, 9), Icons::Glyph::Check,
                     Theme::OnAccent, 3.0);
    }

    painter.setFont(Theme::font(13));
    painter.setPen(m_hovered ? Theme::Text1 : Theme::Text2);
    painter.drawText(QRect(22, 0, width() - 22, height()), Qt::AlignLeft | Qt::AlignVCenter,
                     text());
}

void CheckBox::enterEvent(QEnterEvent *event)
{
    m_hovered = true;
    update();
    QAbstractButton::enterEvent(event);
}

void CheckBox::leaveEvent(QEvent *event)
{
    m_hovered = false;
    update();
    QAbstractButton::leaveEvent(event);
}

// -----------------------------------------------------------------------------
// SegmentTabs
// -----------------------------------------------------------------------------

SegmentTabs::SegmentTabs(const QStringList &labels, Style style, QWidget *parent)
    : QWidget(parent)
    , m_labels(labels)
    , m_style(style)
{
    setAttribute(Qt::WA_Hover, true);
    setMouseTracking(true);
    setCursor(Qt::PointingHandCursor);
    setSizePolicy(QSizePolicy::Fixed, QSizePolicy::Fixed);
    setFixedHeight(style == Segmented ? 33 : 31);
}

void SegmentTabs::setCurrentIndex(int index)
{
    if (index < 0 || index >= m_labels.size() || index == m_current) {
        return;
    }
    m_current = index;
    update();
    emit currentChanged(index);
}

void SegmentTabs::setPixelSize(int pixelSize)
{
    m_pixelSize = pixelSize;
    updateGeometry();
    update();
}

QSize SegmentTabs::sizeHint() const
{
    const QFontMetrics metrics(Theme::font(m_pixelSize));
    int width = 0;
    if (m_style == SoftTabs) {
        for (int i = 0; i < m_labels.size(); ++i) {
            width += metrics.horizontalAdvance(m_labels[i]) + 28;
            if (i < m_labels.size() - 1) {
                width += 4;
            }
        }
    } else {
        for (int i = 0; i < m_labels.size(); ++i) {
            width += metrics.horizontalAdvance(m_labels[i]) + 32;
        }
        width += 6;
    }
    return QSize(width, m_style == Segmented ? 33 : 31);
}

QRect SegmentTabs::tabRect(int index) const
{
    const QFontMetrics metrics(Theme::font(m_pixelSize));
    int x = 0;
    if (m_style == SoftTabs) {
        for (int i = 0; i < index; ++i) {
            x += metrics.horizontalAdvance(m_labels[i]) + 28 + 4;
        }
        return QRect(x, 0, metrics.horizontalAdvance(m_labels[index]) + 28, 31);
    }

    x = 3;
    for (int i = 0; i < index; ++i) {
        x += metrics.horizontalAdvance(m_labels[i]) + 32;
    }
    return QRect(x, 3, metrics.horizontalAdvance(m_labels[index]) + 32, 27);
}

int SegmentTabs::tabAt(const QPoint &point) const
{
    for (int i = 0; i < m_labels.size(); ++i) {
        if (tabRect(i).contains(point)) {
            return i;
        }
    }
    return -1;
}

void SegmentTabs::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    painter.setRenderHint(QPainter::Antialiasing, true);
    painter.setFont(Theme::font(m_pixelSize));

    if (m_style == Segmented) {
        painter.setBrush(Theme::Surface2);
        painter.setPen(QPen(Theme::Border, 1.0));
        painter.drawRoundedRect(QRectF(rect()).adjusted(0.5, 0.5, -0.5, -0.5), 7, 7);
    }

    for (int i = 0; i < m_labels.size(); ++i) {
        const QRect rect = tabRect(i);
        const bool active = i == m_current;
        if (m_style == SoftTabs) {
            if (active) {
                painter.setBrush(Theme::alpha(Theme::Primary, 0.14));
                painter.setPen(Qt::NoPen);
                painter.drawRoundedRect(QRectF(rect).adjusted(0.5, 0.5, -0.5, -0.5), 7, 7);
            } else if (m_hovered == i) {
                painter.setBrush(Theme::Surface2);
                painter.setPen(Qt::NoPen);
                painter.drawRoundedRect(QRectF(rect).adjusted(0.5, 0.5, -0.5, -0.5), 7, 7);
            }
        } else if (active) {
            painter.setBrush(Theme::Primary);
            painter.setPen(Qt::NoPen);
            painter.drawRoundedRect(QRectF(rect).adjusted(0.5, 0.5, -0.5, -0.5), 5, 5);
        }

        QFont f = Theme::font(m_pixelSize, active ? 600 : 400);
        painter.setFont(f);
        painter.setPen(m_style == Segmented && active ? Theme::OnAccent
                                                     : (active ? Theme::Primary : Theme::Text2));
        painter.drawText(rect, Qt::AlignCenter, m_labels[i]);
    }
}

void SegmentTabs::mouseMoveEvent(QMouseEvent *event)
{
    const int hovered = tabAt(event->position().toPoint());
    if (hovered != m_hovered) {
        m_hovered = hovered;
        update();
    }
    QWidget::mouseMoveEvent(event);
}

void SegmentTabs::leaveEvent(QEvent *event)
{
    m_hovered = -1;
    update();
    QWidget::leaveEvent(event);
}

void SegmentTabs::mousePressEvent(QMouseEvent *event)
{
    if (event->button() == Qt::LeftButton) {
        const int index = tabAt(event->position().toPoint());
        if (index >= 0) {
            setCurrentIndex(index);
        }
        event->accept();
        return;
    }
    QWidget::mousePressEvent(event);
}

// -----------------------------------------------------------------------------
// FormField
// -----------------------------------------------------------------------------

FormField::FormField(const QString &labelText, QWidget *content, const QString &hint,
                     bool required, QWidget *parent)
    : QWidget(parent)
    , m_content(content)
{
    auto *root = new QVBoxLayout(this);
    root->setContentsMargins(0, 0, 0, 0);
    root->setSpacing(0);

    auto *labelRow = new QWidget(this);
    auto *labelLayout = new QHBoxLayout(labelRow);
    labelLayout->setContentsMargins(0, 0, 0, 8);
    labelLayout->setSpacing(3);
    labelLayout->addWidget(Ui::label(labelText, 12, 400, Theme::Text2, labelRow));
    if (required) {
        labelLayout->addWidget(Ui::label(QStringLiteral("*"), 12, 400, Theme::Danger, labelRow),
                               0, Qt::AlignTop);
    }
    labelLayout->addStretch(1);
    root->addWidget(labelRow);

    if (content) {
        content->setParent(this);
        root->addWidget(content);
    }

    if (!hint.isEmpty()) {
        auto *hintLabel = Ui::label(hint, 11, 400, Theme::Text3, this);
        hintLabel->setWordWrap(true);
        hintLabel->setContentsMargins(0, 7, 0, 0);
        root->addWidget(hintLabel);
    }
}

// -----------------------------------------------------------------------------
// SelectBox
// -----------------------------------------------------------------------------

SelectBox::SelectBox(QWidget *parent)
    : QComboBox(parent)
{
    setFont(Theme::font(12));
    setCursor(Qt::PointingHandCursor);
    setFixedHeight(35);
    setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
    setMaxVisibleItems(9);
    setStyleSheet(QStringLiteral(
        "QComboBox { background: transparent; border: none; padding: 0; }"
        "QComboBox::drop-down { border: none; width: 0px; }"
        "QComboBox QAbstractItemView { background: #1A1E28; color: #E9ECF4;"
        " border: 1px solid #343B4E; padding: 5px; outline: 0;"
        " selection-background-color: rgba(110,139,255,51); }"));
    polishPopup();
}

void SelectBox::polishPopup()
{
    if (!view()) {
        return;
    }
    view()->setFont(Theme::font(12));
    view()->setCursor(Qt::PointingHandCursor);
    if (view()->window()) {
        view()->window()->setStyleSheet(QStringLiteral(
            "QAbstractItemView { background:#1A1E28; color:#E9ECF4; border:1px solid #343B4E;"
            " border-radius:7px; padding:5px; outline:0;"
            " selection-background-color:rgba(110,139,255,51); selection-color:#E9ECF4; }"
            "QAbstractItemView::item { min-height:26px; padding:2px 8px; border-radius:5px; }"));
    }
}

QSize SelectBox::sizeHint() const
{
    return QSize(220, 35);
}

void SelectBox::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    painter.setRenderHint(QPainter::Antialiasing, true);
    const bool focused = hasFocus();
    painter.setBrush(Theme::Surface2);
    painter.setPen(QPen(focused ? Theme::Primary : Theme::Border, 1.0));
    painter.drawRoundedRect(QRectF(rect()).adjusted(0.5, 0.5, -0.5, -0.5), 7, 7);

    painter.setFont(Theme::font(12));
    painter.setPen(isEnabled() ? Theme::Text1 : Theme::Text3);
    painter.drawText(QRect(11, 0, width() - 38, height()), Qt::AlignLeft | Qt::AlignVCenter,
                     currentText());
    Icons::paint(&painter, QRectF(width() - 25, (height() - 12) / 2.0, 12, 12),
                 Icons::Glyph::ChevronDown, Theme::Text3, 2.0);
}

void SelectBox::mousePressEvent(QMouseEvent *event)
{
    if (event->button() == Qt::LeftButton) {
        if (view() && view()->isVisible()) {
            hidePopup();
        } else {
            showPopup();
        }
        event->accept();
        return;
    }
    QComboBox::mousePressEvent(event);
}

// -----------------------------------------------------------------------------
// InputField
// -----------------------------------------------------------------------------

InputField::InputField(QWidget *parent)
    : QWidget(parent)
{
    setFixedHeight(35);
    setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
    setFont(Theme::font(12));
    m_layout = new QHBoxLayout(this);
    m_layout->setContentsMargins(10, 1, 10, 1);
    m_layout->setSpacing(8);
    m_edit = new QLineEdit(this);
    m_edit->setFont(Theme::monoFont(12));
    m_edit->setStyleSheet(QStringLiteral(
        "QLineEdit { background: transparent; border: none; padding: 0; color: #E9ECF4;"
        " selection-background-color: #4A63BC; selection-color: #FFFFFF; }"));
    m_edit->setAttribute(Qt::WA_MacShowFocusRect, false);
    m_edit->installEventFilter(this);
    m_layout->addWidget(m_edit, 1);
}

void InputField::setText(const QString &text)
{
    m_edit->setText(text);
}

QString InputField::text() const
{
    return m_edit->text();
}

void InputField::setPlaceholder(const QString &text)
{
    m_edit->setPlaceholderText(text);
}

void InputField::setPassword(bool password)
{
    m_edit->setEchoMode(password ? QLineEdit::Password : QLineEdit::Normal);
}

void InputField::setReadOnly(bool readOnly)
{
    m_readOnly = readOnly;
    m_edit->setReadOnly(readOnly);
    m_edit->setCursorPosition(0);
    if (readOnly) {
        m_edit->setStyleSheet(QStringLiteral(
            "QLineEdit { background: transparent; border: none; padding: 0; color: #9BA2B5; }"));
    }
    update();
}

void InputField::setMonoFont(bool mono)
{
    m_edit->setFont(mono ? Theme::monoFont(12) : Theme::font(12));
}

void InputField::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    painter.setRenderHint(QPainter::Antialiasing, true);
    painter.setBrush(m_readOnly ? Theme::Surface2 : Theme::Surface2);
    painter.setPen(QPen(m_focused ? Theme::Primary : Theme::Border, 1.0));
    painter.drawRoundedRect(QRectF(rect()).adjusted(0.5, 0.5, -0.5, -0.5), 7, 7);
}

bool InputField::eventFilter(QObject *watched, QEvent *event)
{
    if (watched == m_edit) {
        if (event->type() == QEvent::FocusIn) {
            m_focused = true;
            update();
        } else if (event->type() == QEvent::FocusOut) {
            m_focused = false;
            update();
        }
    }
    return QWidget::eventFilter(watched, event);
}

// -----------------------------------------------------------------------------
// Toast
// -----------------------------------------------------------------------------

Toast::Toast(QWidget *parent)
    : QWidget(parent)
{
    setAttribute(Qt::WA_TransparentForMouseEvents, true);
    setAttribute(Qt::WA_ShowWithoutActivating, true);
    setFont(Theme::font(13));
    m_effect = new QGraphicsOpacityEffect(this);
    m_effect->setOpacity(1.0);
    setGraphicsEffect(m_effect);
    m_fadeAnimation = new QPropertyAnimation(m_effect, "opacity", this);
    m_fadeAnimation->setDuration(180);
    m_fadeAnimation->setEasingCurve(QEasingCurve::OutCubic);
    connect(m_fadeAnimation, &QPropertyAnimation::finished, this, [this] {
        if (m_effect->opacity() <= 0.01) {
            hide();
        }
    });
    m_slideAnimation = new QPropertyAnimation(this, "pos", this);
    m_slideAnimation->setDuration(200);
    m_slideAnimation->setEasingCurve(QEasingCurve::OutCubic);
    m_hideTimer = new QTimer(this);
    m_hideTimer->setSingleShot(true);
    connect(m_hideTimer, &QTimer::timeout, this, [this] {
        m_fadeAnimation->stop();
        m_fadeAnimation->setStartValue(m_effect->opacity());
        m_fadeAnimation->setEndValue(0.0);
        m_fadeAnimation->start();
    });
    hide();
}

void Toast::showMessage(const QString &text, int durationMs)
{
    m_text = text;
    m_durationMs = durationMs;
    const QFontMetrics metrics(Theme::font(13));
    const int w = qMin(parentWidget() ? parentWidget()->width() - 40 : 600,
                       metrics.horizontalAdvance(text) + 40);
    setFixedSize(qMax(120, w), 37);

    QPoint finalPosition;
    if (parentWidget()) {
        const int x = (parentWidget()->width() - width()) / 2;
        const int y = parentWidget()->height() - 34 - height();
        finalPosition = QPoint(x, y);
        move(x, y + 10);
    }

    raise();
    show();
    m_fadeAnimation->stop();
    m_fadeAnimation->setStartValue(0.0);
    m_fadeAnimation->setEndValue(1.0);
    m_fadeAnimation->start();
    if (parentWidget()) {
        m_slideAnimation->stop();
        m_slideAnimation->setStartValue(pos());
        m_slideAnimation->setEndValue(finalPosition);
        m_slideAnimation->start();
    }
    m_hideTimer->start(m_durationMs);
    update();
}

void Toast::reposition()
{
    if (!parentWidget() || !isVisible()) {
        return;
    }
    move((parentWidget()->width() - width()) / 2,
         parentWidget()->height() - 34 - height());
}

void Toast::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    painter.setRenderHint(QPainter::Antialiasing, true);
    painter.setBrush(Theme::Surface3);
    painter.setPen(QPen(Theme::BorderStrong, 1.0));
    painter.drawRoundedRect(QRectF(rect()).adjusted(0.5, 0.5, -0.5, -0.5), 8, 8);
    painter.setFont(Theme::font(13));
    painter.setPen(Theme::Text1);
    painter.drawText(rect(), Qt::AlignCenter, m_text);
}
