#include "OralPage.h"

#include "../ui/Controls.h"
#include "../ui/Icons.h"
#include "../ui/StudioPosters.h"
#include "../ui/StudioWidgets.h"
#include "../ui/WindowChrome.h"

#include <QApplication>
#include <QClipboard>
#include <QDateTime>
#include <QDir>
#include <QFileDialog>
#include <QFileInfo>
#include <QFontMetrics>
#include <QEnterEvent>
#include <QGridLayout>
#include <QGuiApplication>
#include <QHBoxLayout>
#include <QLabel>
#include <QMessageBox>
#include <QPainter>
#include <QPainterPath>
#include <QScrollArea>
#include <QRegularExpression>
#include <QScrollBar>
#include <QStandardPaths>
#include <QtMath>
#include <QStackedWidget>
#include <QTextEdit>
#include <QToolButton>
#include <QTimer>
#include <QVBoxLayout>

#include <cmath>
#include <functional>

namespace {

QString cssColor(const QColor &color)
{
    return color.name(QColor::HexArgb);
}

QScrollArea *makeScrollArea(QWidget *content, QWidget *parent)
{
    auto *area = new QScrollArea(parent);
    area->setFrameShape(QFrame::NoFrame);
    area->setWidgetResizable(true);
    area->setHorizontalScrollBarPolicy(Qt::ScrollBarAlwaysOff);
    area->setVerticalScrollBarPolicy(Qt::ScrollBarAsNeeded);
    area->setStyleSheet(QStringLiteral(
        "QScrollArea { background: transparent; border: none; }"
        "QScrollArea > QWidget > QWidget { background: transparent; }"));
    area->viewport()->setAutoFillBackground(false);
    area->setWidget(content);
    return area;
}

QWidget *makeIconCircle(Icons::Glyph glyph, int size, const QColor &foreground,
                        const QColor &background, const QColor &border,
                        QWidget *parent)
{
    auto *host = new QWidget(parent);
    host->setFixedSize(size, size);
    host->setObjectName(QStringLiteral("oralIconCircle"));
    host->setStyleSheet(
        QStringLiteral("#oralIconCircle { background:%1; border:1px solid %2;"
                       " border-radius:%3px; }")
            .arg(cssColor(background), cssColor(border))
            .arg(size / 2));
    auto *layout = new QHBoxLayout(host);
    layout->setContentsMargins(0, 0, 0, 0);
    layout->setSpacing(0);
    layout->addWidget(Studio::iconLabel(glyph, qMax(10, qRound(size * 0.5)), foreground,
                                        host),
                      0, Qt::AlignCenter);
    return host;
}

QWidget *makeSoftPill(Icons::Glyph glyph, const QString &text, const QColor &foreground,
                      const QColor &background, const QColor &border,
                      QWidget *parent)
{
    auto *host = new QWidget(parent);
    host->setObjectName(QStringLiteral("oralSoftPill"));
    host->setStyleSheet(
        QStringLiteral("#oralSoftPill { background:%1; border:1px solid %2;"
                       " border-radius:9px; }")
            .arg(cssColor(background), cssColor(border)));
    auto *layout = new QHBoxLayout(host);
    layout->setContentsMargins(8, 2, 8, 2);
    layout->setSpacing(4);
    layout->addWidget(Studio::iconLabel(glyph, 10, foreground, host), 0,
                      Qt::AlignVCenter);
    auto *label = Ui::label(text, 9, QFont::Medium, foreground, host);
    layout->addWidget(label, 0, Qt::AlignVCenter);
    host->setSizePolicy(QSizePolicy::Fixed, QSizePolicy::Fixed);
    return host;
}

QWidget *makeBullet(const QString &text, const QColor &color, QWidget *parent)
{
    auto *row = new QWidget(parent);
    auto *layout = new QHBoxLayout(row);
    layout->setContentsMargins(0, 0, 0, 0);
    layout->setSpacing(6);
    layout->addWidget(Studio::iconLabel(Icons::Glyph::Check, 10, color, row), 0,
                      Qt::AlignVCenter);
    auto *label = Ui::label(text, 10, QFont::Normal, Theme::Text3, row);
    layout->addWidget(label, 0, Qt::AlignVCenter);
    layout->addStretch(1);
    return row;
}

QWidget *makeSectionHeading(const QString &title, const QString &hint,
                            QWidget *right, QWidget *parent)
{
    auto *row = new QWidget(parent);
    auto *layout = new QHBoxLayout(row);
    layout->setContentsMargins(0, 0, 0, 0);
    layout->setSpacing(8);
    layout->addWidget(Ui::label(title, 13, QFont::DemiBold, Theme::Text1, row), 0,
                      Qt::AlignVCenter);
    if (!hint.isEmpty()) {
        layout->addWidget(Ui::label(hint, 10, QFont::Normal, Theme::Text3, row), 0,
                          Qt::AlignVCenter);
    }
    layout->addStretch(1);
    if (right) {
        layout->addWidget(right, 0, Qt::AlignVCenter);
    }
    return row;
}

QString highlightedHtml(const QString &text, const QString &highlight)
{
    QString html = text.toHtmlEscaped();
    if (highlight.isEmpty()) {
        return html;
    }
    const QString escapedHighlight = highlight.toHtmlEscaped();
    html.replace(escapedHighlight,
                 QStringLiteral("<span style=\"background:%1;color:%2;"
                                "border-radius:2px;padding:0 2px;\">%3</span>")
                     .arg(cssColor(Theme::alpha(Theme::Ai, 0.20)),
                          cssColor(Theme::Ai), escapedHighlight));
    return html;
}

QLabel *monoLabel(const QString &text, int pixelSize, const QColor &color,
                  QWidget *parent)
{
    auto *label = Ui::label(text, pixelSize, QFont::Normal, color, parent);
    label->setFont(Theme::monoFont(pixelSize));
    return label;
}

QWidget *makeGenerationPill(QLabel **valueLabel, QWidget *parent)
{
    auto *host = new QWidget(parent);
    host->setObjectName(QStringLiteral("oralGenerationPill"));
    host->setStyleSheet(
        QStringLiteral("#oralGenerationPill { background:%1; border:1px solid %2;"
                       " border-radius:5px; }")
            .arg(Theme::Surface2.name(), Theme::Border.name()));
    host->setFixedHeight(21);
    auto *layout = new QHBoxLayout(host);
    layout->setContentsMargins(8, 0, 8, 0);
    layout->setSpacing(6);
    layout->addWidget(Studio::iconLabel(Icons::Glyph::Clock, 12, Theme::Text3, host));
    layout->addWidget(Ui::label(QStringLiteral("生成时间"), 10, QFont::Normal,
                                Theme::Text3, host));
    auto *value = monoLabel(QString(), 9, Theme::Text1, host);
    value->setToolTip(QString());
    layout->addWidget(value);
    *valueLabel = value;
    return host;
}


/* ------------------------------------------------------------------ */
/* 可点击的自定义卡片                                                   */
/* ------------------------------------------------------------------ */

class OralHeroButton : public QAbstractButton
{
public:
    explicit OralHeroButton(QWidget *parent = nullptr)
        : QAbstractButton(parent)
    {
        setCursor(Qt::PointingHandCursor);
        setMinimumHeight(244);
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Preferred);
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        const QRectF rect = QRectF(this->rect()).adjusted(0.5, 0.5, -0.5, -0.5);
        QLinearGradient base(rect.topLeft(), rect.bottomLeft());
        base.setColorAt(0, Theme::Surface1);
        base.setColorAt(1, Theme::Surface1);
        painter.setBrush(base);
        painter.setPen(QPen(m_hovered ? Theme::alpha(Theme::Primary, 0.45)
                                      : Theme::Border,
                            1.0));
        painter.drawRoundedRect(rect, 12, 12);

        QRadialGradient glow(rect.right(), rect.top(), rect.width() * 0.9);
        glow.setColorAt(0, QColor(110, 139, 255, 33));
        glow.setColorAt(0.55, QColor(110, 139, 255, 7));
        glow.setColorAt(1, QColor(110, 139, 255, 0));
        QPainterPath clip;
        clip.addRoundedRect(rect, 12, 12);
        painter.setClipPath(clip);
        painter.fillRect(rect, glow);
    }

    void enterEvent(QEnterEvent *event) override
    {
        m_hovered = true;
        update();
        QAbstractButton::enterEvent(event);
    }

    void leaveEvent(QEvent *event) override
    {
        m_hovered = false;
        update();
        QAbstractButton::leaveEvent(event);
    }

private:
    bool m_hovered = false;
};

class OralHeroArt : public QWidget
{
public:
    explicit OralHeroArt(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        setAttribute(Qt::WA_TransparentForMouseEvents, true);
        setMinimumWidth(190);
        setMinimumHeight(208);
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Expanding);
    }

    void setTemplateId(const QString &id)
    {
        m_templateId = id;
        update();
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        const QRectF area = QRectF(rect());

        Studio::OralPosterSpec leftSpec;
        leftSpec.templateId = QStringLiteral("magazine");
        leftSpec.caption = StudioData::oralTemplate(QStringLiteral("magazine")).caption;
        leftSpec.ratio = QStringLiteral("9:16");
        leftSpec.badge = StudioData::oralTemplate(QStringLiteral("magazine")).name;
        leftSpec.materials = StudioData::demoMaterials().mid(2, 1);
        leftSpec.timecode.clear();
        painter.save();
        painter.translate(area.left() + area.width() * 0.18,
                          area.top() + 18 + 87);
        painter.rotate(-7);
        painter.setOpacity(0.4);
        Studio::paintOralPoster(painter, QRectF(-49, -87, 98, 174), leftSpec);
        painter.restore();

        const StudioData::OralTemplate tmpl = StudioData::oralTemplate(m_templateId);
        Studio::OralPosterSpec rightSpec;
        rightSpec.templateId = m_templateId;
        rightSpec.headline = tmpl.headline;
        rightSpec.caption = QStringLiteral("记住这三点");
        rightSpec.keywords = {QStringLiteral("油皮"), QStringLiteral("通勤")};
        rightSpec.materials = StudioData::demoMaterials().mid(0, 2);
        rightSpec.timecode = QStringLiteral("00:58");
        rightSpec.ratio = QStringLiteral("9:16");
        painter.save();
        painter.translate(area.right() - 60, 0 + 104);
        painter.rotate(3);
        Studio::paintOralPoster(painter, QRectF(-58, -104, 117, 208), rightSpec);
        painter.restore();

        auto drawFloat = [&painter](const QRectF &r, const QColor &dot,
                                    const QString &text, Icons::Glyph glyph,
                                    const QColor &iconColor) {
            painter.setPen(QPen(Theme::BorderStrong, 1.0));
            painter.setBrush(Theme::alpha(Theme::Surface2, 0.95));
            painter.drawRoundedRect(r, 6, 6);
            const QRectF iconRect(r.left() + 7, r.center().y() - 5, 10, 10);
            Q_UNUSED(dot);
            Icons::paint(&painter, iconRect, glyph, iconColor, 2.0);
            painter.setFont(Theme::font(9));
            painter.setPen(Theme::Text2);
            painter.drawText(r.adjusted(21, 0, -6, 0), Qt::AlignLeft | Qt::AlignVCenter,
                             text);
        };
        drawFloat(QRectF(area.left(), area.top() + 64, 120, 22),
                  Theme::Ai, QStringLiteral("素材匹配 8/8"), Icons::Glyph::Sparkle,
                  Theme::Ai);
        const QRectF saveRect(area.right() - 134, area.bottom() - 46, 134, 22);
        painter.setPen(QPen(Theme::BorderStrong, 1.0));
        painter.setBrush(Theme::alpha(Theme::Surface2, 0.95));
        painter.drawRoundedRect(saveRect, 6, 6);
        painter.setPen(Qt::NoPen);
        painter.setBrush(Theme::Success);
        painter.drawEllipse(QPointF(saveRect.left() + 11, saveRect.center().y()), 3,
                            3);
        painter.setFont(Theme::font(9));
        painter.setPen(Theme::Text2);
        painter.drawText(saveRect.adjusted(19, 0, -5, 0),
                         Qt::AlignLeft | Qt::AlignVCenter,
                         QStringLiteral("字幕 · 转场已生成"));
    }

private:
    QString m_templateId = QStringLiteral("clean");
};

class GradientPanel : public QWidget
{
public:
    explicit GradientPanel(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        auto *layout = new QVBoxLayout(this);
        layout->setContentsMargins(16, 16, 16, 16);
        layout->setSpacing(0);
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        const QRectF rect = QRectF(this->rect()).adjusted(0.5, 0.5, -0.5, -0.5);
        painter.setPen(QPen(Theme::Border, 1.0));
        painter.setBrush(Theme::Surface1);
        painter.drawRoundedRect(rect, 12, 12);
        QRadialGradient glow(rect.left(), rect.top(), rect.width());
        glow.setColorAt(0, QColor(255, 178, 36, 18));
        glow.setColorAt(0.6, QColor(255, 178, 36, 3));
        glow.setColorAt(1, QColor(255, 178, 36, 0));
        QPainterPath clip;
        clip.addRoundedRect(rect, 12, 12);
        painter.setClipPath(clip);
        painter.fillRect(rect, glow);
    }
};

class SourcePreview : public QAbstractButton
{
public:
    SourcePreview(const StudioData::OralSource &source, QWidget *parent = nullptr)
        : QAbstractButton(parent)
        , m_source(source)
    {
        setCursor(Qt::PointingHandCursor);
        setFixedHeight(250);
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        QPainterPath clip;
        clip.addRoundedRect(QRectF(rect()), 8, 8);
        painter.setClipPath(clip);
        Studio::paintStudioScene(painter, QRectF(rect()));

        QLinearGradient shade(0, 0, 0, height());
        shade.setColorAt(0, QColor(4, 5, 9, 115));
        shade.setColorAt(0.4, QColor(4, 5, 9, 0));
        shade.setColorAt(0.65, QColor(4, 5, 9, 0));
        shade.setColorAt(1, QColor(4, 5, 9, 204));
        painter.fillRect(rect(), shade);
        painter.setClipping(false);

        auto drawChip = [&painter](const QRectF &r, const QColor &bg,
                                   const QColor &border, const QString &text,
                                   const QColor &color) {
            painter.setPen(QPen(border, 1.0));
            painter.setBrush(bg);
            painter.drawRoundedRect(r, 4, 4);
            painter.setFont(Theme::font(9));
            painter.setPen(color);
            painter.drawText(r, Qt::AlignCenter, text);
        };
        drawChip(QRectF(8, 8, 54, 19), Theme::alpha(Theme::Primary, 0.18),
                 Theme::alpha(Theme::Primary, 0.4), QStringLiteral("口播原片"),
                 Theme::Primary);
        if (m_source.sample) {
            drawChip(QRectF(68, 8, 54, 19), QColor(0, 0, 0, 115),
                     Theme::BorderStrong, QStringLiteral("示例素材"),
                     QColor(255, 255, 255, 191));
        }
        const QString resolution = m_source.resolution;
        painter.setFont(Theme::monoFont(9));
        painter.setPen(QColor(255, 255, 255, 217));
        const QRectF resolutionRect(width() - 90, 8, 82, 19);
        painter.setPen(Qt::NoPen);
        painter.setBrush(QColor(0, 0, 0, 140));
        painter.drawRoundedRect(resolutionRect, 4, 4);
        painter.setPen(QColor(255, 255, 255, 217));
        painter.drawText(resolutionRect, Qt::AlignCenter, resolution);

        const QRectF playCircle(width() / 2.0 - 22, height() / 2.0 - 22, 44, 44);
        painter.setPen(QPen(QColor(255, 255, 255, 76), 1.0));
        painter.setBrush(QColor(0, 0, 0, 150));
        painter.drawEllipse(playCircle);
        QPainterPath play;
        play.moveTo(playCircle.center().x() - 4, playCircle.center().y() - 7);
        play.lineTo(playCircle.center().x() + 8, playCircle.center().y());
        play.lineTo(playCircle.center().x() - 4, playCircle.center().y() + 7);
        play.closeSubpath();
        painter.setBrush(QColor(255, 255, 255, 230));
        painter.setPen(Qt::NoPen);
        painter.drawPath(play);

        const QRectF waveRect(12, height() - 26, width() - 78, 14);
        for (int index = 0; index < 34; ++index) {
            const qreal barWidth = waveRect.width() / 34.0 - 2.0;
            const int barHeight = 20 + (index * 37) % 80;
            const qreal h = waveRect.height() * barHeight / 100.0;
            painter.setBrush(QColor(255, 255, 255, 140));
            painter.drawRoundedRect(
                QRectF(waveRect.left() + index * (barWidth + 2.0),
                       waveRect.center().y() - h / 2.0, barWidth, h),
                barWidth / 2.0, barWidth / 2.0);
        }
        painter.setFont(Theme::monoFont(9));
        painter.setPen(QColor(255, 255, 255, 179));
        painter.drawText(QRectF(width() - 60, height() - 26, 48, 14),
                         Qt::AlignRight | Qt::AlignVCenter, m_source.duration);
    }

private:
    StudioData::OralSource m_source;
};

class OutputChoiceCard : public QAbstractButton
{
public:
    OutputChoiceCard(const QString &title, const QString &description,
                     const QString &badge, Icons::Glyph glyph, bool selected,
                     QWidget *parent = nullptr)
        : QAbstractButton(parent)
        , m_title(title)
        , m_description(description)
        , m_badge(badge)
        , m_glyph(glyph)
        , m_selected(selected)
    {
        setCursor(Qt::PointingHandCursor);
        setFixedHeight(49);
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
    }

    void setSelected(bool selected)
    {
        m_selected = selected;
        update();
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        const QRectF rect = QRectF(this->rect()).adjusted(0.5, 0.5, -0.5, -0.5);
        painter.setPen(QPen(m_selected ? Theme::alpha(Theme::Primary, 0.55)
                                      : Theme::Border,
                            1.0));
        painter.setBrush(m_selected ? Theme::alpha(Theme::Primary, 0.14)
                                    : Theme::alpha(Theme::Surface2, 0.45));
        painter.drawRoundedRect(rect, 7, 7);

        const QRectF iconRect(10, 10, 28, 28);
        painter.setPen(QPen(m_selected ? Theme::alpha(Theme::Primary, 0.35)
                                       : Theme::Border,
                            1.0));
        painter.setBrush(m_selected ? Theme::alpha(Theme::Primary, 0.15)
                                    : Theme::Surface2);
        painter.drawRoundedRect(iconRect, 6, 6);
        Icons::paint(&painter, iconRect.adjusted(8, 8, -8, -8), m_glyph,
                     m_selected ? Theme::Primary : Theme::Text3, 2.0);

        painter.setFont(Theme::font(10, QFont::Medium));
        painter.setPen(Theme::Text1);
        const QRectF titleRect(47, 7, width() - 80, 17);
        painter.drawText(titleRect, Qt::AlignLeft | Qt::AlignVCenter, m_title);
        if (!m_badge.isEmpty()) {
            const QFontMetrics metrics(Theme::font(8));
            const int badgeWidth = metrics.horizontalAdvance(m_badge) + 8;
            const QRectF badgeRect(47 + QFontMetrics(Theme::font(10, QFont::Medium))
                                            .horizontalAdvance(m_title)
                                     + 6,
                                   8, badgeWidth, 14);
            painter.setPen(Qt::NoPen);
            painter.setBrush(Theme::alpha(Theme::Primary, 0.15));
            painter.drawRoundedRect(badgeRect, 3, 3);
            painter.setFont(Theme::font(8));
            painter.setPen(Theme::Primary);
            painter.drawText(badgeRect, Qt::AlignCenter, m_badge);
        }
        painter.setFont(Theme::font(9));
        painter.setPen(Theme::Text3);
        painter.drawText(QRectF(47, 24, width() - 80, 16),
                         Qt::AlignLeft | Qt::AlignVCenter, m_description);

        const QRectF radio(width() - 23, height() / 2.0 - 7, 14, 14);
        painter.setPen(QPen(m_selected ? Theme::Primary : Theme::BorderStrong, 1.0));
        painter.setBrush(m_selected ? Theme::Primary : Qt::NoBrush);
        painter.drawEllipse(radio);
        if (m_selected) {
            Icons::paint(&painter, radio.adjusted(3, 3, -3, -3), Icons::Glyph::Check,
                         Theme::OnAccent, 2.6);
        }
    }

private:
    QString m_title;
    QString m_description;
    QString m_badge;
    Icons::Glyph m_glyph;
    bool m_selected;
};

class MaterialTile : public Card
{
public:
    MaterialTile(const StudioData::OralMaterial &material,
                 std::function<void()> onRemove, QWidget *parent = nullptr)
        : Card(Theme::Surface2, Theme::Border, 7, parent)
        , m_onRemove(std::move(onRemove))
    {
        setHoverEnabled(true);
        setHoverColors(Theme::Surface2, Theme::BorderStrong);
        setMinimumHeight(122);
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
        contentLayout()->setContentsMargins(0, 0, 0, 0);
        contentLayout()->setSpacing(0);

        auto *art = new Studio::MaterialVisualWidget(this);
        art->setMaterial(material);
        art->setFixedHeight(96);
        m_art = art;
        contentLayout()->addWidget(art);

        auto *bottom = new QWidget(this);
        auto *bottomLayout = new QHBoxLayout(bottom);
        bottomLayout->setContentsMargins(6, 2, 6, 3);
        bottomLayout->setSpacing(4);
        auto *name = new ElidedLabel(material.name, bottom);
        name->setPixelSize(9);
        name->setTextColor(Theme::Text2);
        bottomLayout->addWidget(name, 1);
        bottomLayout->addWidget(Ui::label(material.kind, 8, QFont::Normal,
                                          Theme::Text3, bottom));
        contentLayout()->addWidget(bottom);

        m_tag = Studio::tagLabel(material.tag, QColor(255, 255, 255, 230),
                                 QColor(0, 0, 0, 166), 8, this);
        m_tag->adjustSize();
        m_tag->show();
        if (!material.duration.isEmpty()) {
            m_duration = Studio::tagLabel(material.duration,
                                          QColor(255, 255, 255, 217),
                                          QColor(0, 0, 0, 140), 8, this);
            m_duration->setFont(Theme::monoFont(8));
            m_duration->show();
        }

        m_remove = new GlyphButton(Icons::Glyph::Trash, 20, this);
        m_remove->setGlyphSize(10);
        m_remove->setColors(Qt::white, Qt::white);
        m_remove->setBackgroundColors(QColor(0, 0, 0, 153), Theme::Danger);
        m_remove->setToolTipText(QStringLiteral("移除「%1」").arg(material.name));
        m_remove->hide();
        connect(m_remove, &QAbstractButton::clicked, this, [this] {
            if (m_onRemove) {
                m_onRemove();
            }
        });
    }

protected:
    void resizeEvent(QResizeEvent *event) override
    {
        Card::resizeEvent(event);
        if (m_art) {
            m_art->setGeometry(0, 0, width(), m_art->height());
        }
        if (m_tag) {
            m_tag->move(4, qMax(0, (m_art ? m_art->height() : 96) - m_tag->height() - 4));
        }
        if (m_duration) {
            m_duration->move(4, 4);
        }
        if (m_remove) {
            m_remove->move(width() - m_remove->width() - 4, 4);
            m_remove->raise();
        }
        if (m_tag) {
            m_tag->raise();
        }
        if (m_duration) {
            m_duration->raise();
        }
    }

    void enterEvent(QEnterEvent *event) override
    {
        Card::enterEvent(event);
        if (m_remove) {
            m_remove->show();
            m_remove->raise();
        }
    }

    void leaveEvent(QEvent *event) override
    {
        Card::leaveEvent(event);
        if (m_remove) {
            m_remove->hide();
        }
    }

private:
    std::function<void()> m_onRemove;
    Studio::MaterialVisualWidget *m_art = nullptr;
    QLabel *m_tag = nullptr;
    QLabel *m_duration = nullptr;
    GlyphButton *m_remove = nullptr;
};

class AddMaterialTile : public QAbstractButton
{
public:
    explicit AddMaterialTile(QWidget *parent = nullptr)
        : QAbstractButton(parent)
    {
        setCursor(Qt::PointingHandCursor);
        setMinimumHeight(122);
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
        setToolTip(QStringLiteral("添加相关素材"));
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        const QRectF rect = QRectF(this->rect()).adjusted(0.5, 0.5, -0.5, -0.5);
        QPen pen(m_hovered ? Theme::alpha(Theme::Primary, 0.5)
                           : Theme::alpha(Theme::BorderStrong, 0.7),
                 1.0, Qt::DashLine);
        pen.setDashPattern({4, 4});
        painter.setPen(pen);
        painter.setBrush(Theme::alpha(Theme::Surface2, 0.2));
        painter.drawRoundedRect(rect, 7, 7);
        Icons::paint(&painter, QRectF(width() / 2.0 - 7, height() / 2.0 - 20, 14, 14),
                     Icons::Glyph::Plus,
                     m_hovered ? Theme::Primary : Theme::Text3, 2.0);
        painter.setFont(Theme::font(9));
        painter.setPen(m_hovered ? Theme::Primary : Theme::Text3);
        painter.drawText(QRectF(0, height() / 2.0 - 1, width(), 18),
                         Qt::AlignCenter, QStringLiteral("上传素材"));
    }

    void enterEvent(QEnterEvent *event) override
    {
        m_hovered = true;
        update();
        QAbstractButton::enterEvent(event);
    }

    void leaveEvent(QEvent *event) override
    {
        m_hovered = false;
        update();
        QAbstractButton::leaveEvent(event);
    }

private:
    bool m_hovered = false;
};

class DarkStage : public QWidget
{
public:
    explicit DarkStage(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        setStyleSheet(QStringLiteral("background:transparent;"));
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        const QRectF rect = QRectF(this->rect()).adjusted(0.5, 0.5, -0.5, -0.5);
        painter.setPen(QPen(Theme::Border, 1.0));
        painter.setBrush(QColor(QStringLiteral("#0c0e14")));
        painter.drawRoundedRect(rect, 10, 10);
        QRadialGradient glow(rect.center().x(), rect.top() + rect.height() * 0.1,
                             rect.width() * 0.8);
        glow.setColorAt(0, QColor(QStringLiteral("#1b2130")));
        glow.setColorAt(1, QColor(QStringLiteral("#0c0e14")));
        QPainterPath clip;
        clip.addRoundedRect(rect, 10, 10);
        painter.setClipPath(clip);
        painter.fillRect(rect, glow);
    }
};

QWidget *makeSourceBox(const QString &label, const QString &value, QWidget *parent)
{
    auto *box = new QWidget(parent);
    box->setObjectName(QStringLiteral("oralSourceBox"));
    box->setStyleSheet(QStringLiteral(
        "#oralSourceBox { background:%1; border:1px solid %2; border-radius:6px; }")
                           .arg(Theme::alpha(Theme::Surface2, 0.6).name(QColor::HexArgb),
                                Theme::Border.name()));
    auto *layout = new QVBoxLayout(box);
    layout->setContentsMargins(10, 6, 10, 6);
    layout->setSpacing(1);
    layout->addWidget(Ui::label(label, 9, QFont::Normal, Theme::Text3, box));
    auto *valueLabel = monoLabel(value, 12, Theme::Text1, box);
    valueLabel->setFont(Theme::monoFont(12, QFont::Medium));
    layout->addWidget(valueLabel);
    return box;
}

QWidget *makeInfoPanel(const QString &title, Icons::Glyph glyph, QWidget *content,
                       QWidget *parent)
{
    auto *box = new QWidget(parent);
    box->setObjectName(QStringLiteral("oralInfoPanel"));
    box->setStyleSheet(QStringLiteral(
        "#oralInfoPanel { background:%1; border:1px solid %2; border-radius:7px; }")
                           .arg(Theme::Surface1.name(), Theme::Border.name()));
    auto *layout = new QVBoxLayout(box);
    layout->setContentsMargins(10, 8, 10, 8);
    layout->setSpacing(4);
    auto *header = new QWidget(box);
    auto *headerLayout = new QHBoxLayout(header);
    headerLayout->setContentsMargins(0, 0, 0, 0);
    headerLayout->setSpacing(4);
    headerLayout->addWidget(Studio::iconLabel(glyph, 10, Theme::Ai, header));
    headerLayout->addWidget(Ui::label(title, 9, QFont::Normal, Theme::Text3, header));
    headerLayout->addStretch(1);
    layout->addWidget(header);
    layout->addWidget(content);
    return box;
}

QWidget *makeFileRow(Icons::Glyph glyph, const QString &name, const QString &tag,
                     const QString &description, QWidget *right, QWidget *parent)
{
    auto *row = new QWidget(parent);
    row->setObjectName(QStringLiteral("oralFileRow"));
    row->setStyleSheet(
        QStringLiteral("#oralFileRow { background:%1; border:1px solid %2;"
                       " border-radius:8px; }")
            .arg(Theme::alpha(Theme::Surface2, 0.45).name(QColor::HexArgb),
                 Theme::Border.name()));
    auto *layout = new QHBoxLayout(row);
    layout->setContentsMargins(10, 10, 10, 10);
    layout->setSpacing(10);
    layout->addWidget(makeIconCircle(glyph, 32, Theme::Text2, Theme::Surface1,
                                     Theme::Border, row),
                      0, Qt::AlignVCenter);

    auto *texts = new QVBoxLayout;
    texts->setContentsMargins(0, 0, 0, 0);
    texts->setSpacing(2);
    auto *nameRow = new QHBoxLayout;
    nameRow->setContentsMargins(0, 0, 0, 0);
    nameRow->setSpacing(6);
    auto *nameLabel = new ElidedLabel(name, row);
    nameLabel->setPixelSize(11);
    nameLabel->setFontWeight(QFont::Medium);
    nameLabel->setTextColor(Theme::Text1);
    nameRow->addWidget(nameLabel, 1);
    nameRow->addWidget(Studio::tagLabel(tag, Theme::Text2, Theme::Surface3, 8, row), 0);
    texts->addLayout(nameRow);
    auto *descriptionLabel = Ui::label(description, 9, QFont::Normal, Theme::Text3, row);
    descriptionLabel->setWordWrap(false);
    texts->addWidget(descriptionLabel);
    layout->addLayout(texts, 1);
    if (right) {
        layout->addWidget(right, 0, Qt::AlignVCenter);
    }
    return row;
}

void setResultPlayIcon(QToolButton *button, bool playing)
{
    if (!button) {
        return;
    }
    button->setIcon(Icons::glyphIcon(playing ? Icons::Glyph::Pause
                                             : Icons::Glyph::Play,
                                     Qt::white, 18, 2.2));
}

QToolButton *makeResultPlayOverlay(QWidget *parent)
{
    auto *button = new QToolButton(parent);
    button->setFixedSize(44, 44);
    button->setCursor(Qt::PointingHandCursor);
    button->setIconSize(QSize(18, 18));
    button->setStyleSheet(
        QStringLiteral(
            "QToolButton { background:rgba(0,0,0,118);"
            " border:1px solid rgba(255,255,255,76); border-radius:22px; }"
            "QToolButton:hover { background:rgba(0,0,0,163); }"));
    button->setIcon(Icons::glyphIcon(Icons::Glyph::Play, Qt::white, 18, 2.2));
    return button;
}

QWidget *makeCopyButton(const QString &text, const QString &label,
                        std::function<void()> onCopy, QWidget *parent)
{
    auto *button = new GlyphButton(Icons::Glyph::Copy, 18, parent);
    button->setGlyphSize(11);
    button->setColors(Theme::Text3, Theme::Primary);
    button->setToolTipText(QStringLiteral("复制%1").arg(label));
    QObject::connect(button, &QAbstractButton::clicked, button,
                     [onCopy = std::move(onCopy), text] {
                         Q_UNUSED(text);
                         if (onCopy) {
                             onCopy();
                         }
                     });
    return button;
}

} // namespace

/* ------------------------------------------------------------------ */
/* OralPage                                                             */
/* ------------------------------------------------------------------ */

OralPage::OralPage(QWidget *parent)
    : QWidget(parent)
{
    m_project = StudioData::defaultOralProject();

    m_runTimer = new QTimer(this);
    m_runTimer->setInterval(100);
    connect(m_runTimer, &QTimer::timeout, this, [this] {
        if (!m_hasRun) {
            return;
        }
        const qint64 elapsed = QDateTime::currentMSecsSinceEpoch() - m_runStartedAt;
        m_run = StudioData::computeOralRun(elapsed, m_runId);
        updateProcessing();
        if (m_run.done) {
            finishGeneration();
        } else {
            const int percent = qMin(99, static_cast<int>(std::floor(m_run.progress)));
            emit taskStatusChanged(
                QStringLiteral("口播包装中 %1% · %2")
                    .arg(percent)
                    .arg(StudioData::formatElapsed(m_run.elapsedMs)),
                true);
        }
    });

    m_playTimer = new QTimer(this);
    m_playTimer->setInterval(100);
    connect(m_playTimer, &QTimer::timeout, this, &OralPage::updatePlayback);

    m_draftTimer = new QTimer(this);
    m_draftTimer->setInterval(110);
    connect(m_draftTimer, &QTimer::timeout, this, &OralPage::updateDraftGeneration);

    m_clockTimer = new QTimer(this);
    m_clockTimer->setInterval(10 * 1000);
    connect(m_clockTimer, &QTimer::timeout, this, &OralPage::updateGenerationClock);
    m_clockTimer->start();

    buildUi();
}

OralPage::~OralPage() = default;

void OralPage::buildUi()
{
    auto *root = new QVBoxLayout(this);
    root->setContentsMargins(0, 0, 0, 0);
    root->setSpacing(0);

    m_stack = new QStackedWidget(this);
    m_stack->setStyleSheet(QStringLiteral("QStackedWidget { background: transparent; }"));
    root->addWidget(m_stack);

    m_hubScreen = new QWidget(m_stack);
    auto *hubRoot = new QVBoxLayout(m_hubScreen);
    hubRoot->setContentsMargins(0, 0, 0, 0);
    hubRoot->setSpacing(0);
    m_hubContent = new QWidget;
    m_hubLayout = new QVBoxLayout(m_hubContent);
    m_hubLayout->setContentsMargins(22, 18, 22, 24);
    m_hubLayout->setSpacing(0);
    m_hubLayout->setAlignment(Qt::AlignTop);
    m_hubScroll = makeScrollArea(m_hubContent, m_hubScreen);
    hubRoot->addWidget(m_hubScroll);
    m_stack->addWidget(m_hubScreen);

    m_setupScreen = new QWidget(m_stack);
    m_stack->addWidget(m_setupScreen);

    m_processingScreen = new QWidget(m_stack);
    m_stack->addWidget(m_processingScreen);
    buildProcessingScreen();

    m_resultScreen = new QWidget(m_stack);
    m_stack->addWidget(m_resultScreen);

    rebuildHub();
}

bool OralPage::isRunning() const
{
    return m_hasRun && !m_run.done;
}

/* ------------------------------------------------------------------ */
/* 首页                                                                 */
/* ------------------------------------------------------------------ */

void OralPage::showHub()
{
    m_screen = Screen::Hub;
    rebuildHub();
    m_stack->setCurrentWidget(m_hubScreen);
    if (m_hubScroll && m_hubScroll->verticalScrollBar()) {
        m_hubScroll->verticalScrollBar()->setValue(0);
    }
}

void OralPage::showSetup()
{
    m_screen = Screen::Setup;
    rebuildSetup();
    m_stack->setCurrentWidget(m_setupScreen);
}

void OralPage::showProcessing()
{
    m_screen = Screen::Processing;
    m_stack->setCurrentWidget(m_processingScreen);
}

void OralPage::showResult()
{
    m_screen = Screen::Result;
    rebuildResult();
    m_stack->setCurrentWidget(m_resultScreen);
}

void OralPage::rebuildHub()
{
    Studio::clearLayout(m_hubLayout);

    auto *header = new PageHeader(
        QStringLiteral("智能口播"),
        QStringLiteral("上传真人口播与相关素材，选择模板，一键包装成片。"), this);
    auto *newChip = new Chip(QStringLiteral("新功能"), Chip::Ai, header);
    header->setTitleMeta(newChip);
    auto *start = new Button(QStringLiteral("开始口播包装"), Button::Primary, header);
    start->setLeadingGlyph(Icons::Glyph::Sparkle);
    connect(start, &QAbstractButton::clicked, this, &OralPage::startProject);
    header->addAction(start);
    m_hubLayout->addWidget(header);

    auto *stats = new QWidget(m_hubContent);
    auto *statsLayout = new QHBoxLayout(stats);
    statsLayout->setContentsMargins(0, 0, 0, 16);
    statsLayout->setSpacing(10);
    auto addStat = [&](const QString &html) {
        statsLayout->addWidget(Ui::richLabel(html, 10, Theme::Text3, stats));
    };
    addStat(QStringLiteral("本月已生成 <b style=\"color:%1\">24</b> 条口播")
                .arg(Theme::Text1.name()));
    statsLayout->addWidget(Ui::label(QStringLiteral("·"), 10, QFont::Normal,
                                     Theme::BorderStrong, stats));
    addStat(QStringLiteral("平均包装用时 <b style=\"color:%1\">3 分 12 秒</b>")
                .arg(Theme::Text1.name()));
    statsLayout->addWidget(Ui::label(QStringLiteral("·"), 10, QFont::Normal,
                                     Theme::BorderStrong, stats));
    addStat(QStringLiteral("素材匹配率 <b style=\"color:%1\">92%</b>")
                .arg(Theme::Success.name()));
    statsLayout->addStretch(1);
    auto *readyDot = new StatusDot(6, Theme::Success, stats);
    statsLayout->addWidget(readyDot);
    statsLayout->addWidget(Ui::label(QStringLiteral("本地素材库已就绪 · 8 个相关素材"),
                                     10, QFont::Normal, Theme::Text3, stats));
    m_hubLayout->addWidget(stats);

    auto *heroRow = new QWidget(m_hubContent);
    auto *heroLayout = new QHBoxLayout(heroRow);
    heroLayout->setContentsMargins(0, 0, 0, 0);
    heroLayout->setSpacing(14);
    heroLayout->addWidget(buildHubHero(), 146);
    heroLayout->addWidget(buildHubProcessPanel(), 100);
    m_hubLayout->addWidget(heroRow);

    m_hubLayout->addSpacing(20);
    auto *recentHeading = makeSectionHeading(
        QStringLiteral("最近口播"),
        QStringLiteral("%1 个项目").arg(StudioData::recentOralResults().size()),
        nullptr, m_hubContent);
    auto *recentMeta = new QWidget(m_hubContent);
    auto *recentMetaLayout = new QHBoxLayout(recentMeta);
    recentMetaLayout->setContentsMargins(0, 0, 0, 0);
    recentMetaLayout->setSpacing(4);
    recentMetaLayout->addWidget(Studio::iconLabel(Icons::Glyph::Clock, 10,
                                                  Theme::Text3, recentMeta));
    recentMetaLayout->addWidget(Ui::label(QStringLiteral("自动保存生成记录"), 10,
                                          QFont::Normal, Theme::Text3, recentMeta));
    auto *recentHeadingRow = new QWidget(m_hubContent);
    auto *recentHeadingLayout = new QHBoxLayout(recentHeadingRow);
    recentHeadingLayout->setContentsMargins(0, 0, 0, 8);
    recentHeadingLayout->setSpacing(0);
    recentHeadingLayout->addWidget(recentHeading, 1);
    recentHeadingLayout->addWidget(recentMeta, 0, Qt::AlignVCenter);
    m_hubLayout->addWidget(recentHeadingRow);

    auto *recentRow = new QWidget(m_hubContent);
    auto *recentLayout = new QHBoxLayout(recentRow);
    recentLayout->setContentsMargins(0, 0, 0, 0);
    recentLayout->setSpacing(12);
    for (const StudioData::OralResult &result : StudioData::recentOralResults()) {
        recentLayout->addWidget(createRecentCard(result, recentRow), 1);
    }
    m_hubLayout->addWidget(recentRow);

    m_hubLayout->addSpacing(20);
    const QString templateId = m_project.templateId;
    const StudioData::OralTemplate current = StudioData::oralTemplate(templateId);
    auto *templateMeta = new QWidget(m_hubContent);
    auto *templateMetaLayout = new QHBoxLayout(templateMeta);
    templateMetaLayout->setContentsMargins(0, 0, 0, 0);
    templateMetaLayout->setSpacing(6);
    auto *dot = new QWidget(templateMeta);
    dot->setFixedSize(6, 6);
    dot->setStyleSheet(QStringLiteral("background:%1;border-radius:3px;")
                           .arg(cssColor(current.accent)));
    templateMetaLayout->addWidget(dot);
    templateMetaLayout->addWidget(Ui::label(QStringLiteral("当前默认："), 10,
                                            QFont::Normal, Theme::Text3, templateMeta));
    templateMetaLayout->addWidget(Ui::label(current.name, 10, QFont::Medium,
                                            Theme::Text2, templateMeta));
    auto *templateHeading = makeSectionHeading(
        QStringLiteral("包装模板"),
        QStringLiteral("点击模板设为默认，开始包装时自动套用"), templateMeta,
        m_hubContent);
    auto *templateHeadingRow = new QWidget(m_hubContent);
    auto *templateHeadingLayout = new QHBoxLayout(templateHeadingRow);
    templateHeadingLayout->setContentsMargins(0, 0, 0, 8);
    templateHeadingLayout->setSpacing(0);
    templateHeadingLayout->addWidget(templateHeading, 1);
    m_hubLayout->addWidget(templateHeadingRow);

    auto *templateGrid = new QWidget(m_hubContent);
    auto *grid = new QGridLayout(templateGrid);
    grid->setContentsMargins(0, 0, 0, 0);
    grid->setHorizontalSpacing(10);
    grid->setVerticalSpacing(10);
    const QVector<StudioData::OralTemplate> &templates = StudioData::oralTemplates();
    for (int index = 0; index < templates.size(); ++index) {
        grid->addWidget(createTemplateCard(
                            templates.at(index).id,
                            templates.at(index).id == m_project.templateId, false,
                            templateGrid),
                        index / 3, index % 3);
    }
    m_hubLayout->addWidget(templateGrid);

    m_hubLayout->addSpacing(16);
    auto *tip = new QWidget(m_hubContent);
    tip->setObjectName(QStringLiteral("oralHubTip"));
    tip->setStyleSheet(
        QStringLiteral("#oralHubTip { background:%1; border:1px solid %2;"
                       " border-radius:8px; }")
            .arg(Theme::alpha(Theme::Surface1, 0.6).name(QColor::HexArgb),
                 Theme::Border.name()));
    auto *tipLayout = new QHBoxLayout(tip);
    tipLayout->setContentsMargins(12, 9, 12, 9);
    tipLayout->setSpacing(8);
    tipLayout->addWidget(Studio::iconLabel(Icons::Glyph::Wand, 12, Theme::Ai, tip));
    auto *tipLabel = Ui::label(
        QStringLiteral("小提示：先选模板再开始包装，AI 会自动按模板节奏调整字幕、素材插入密度和音乐风格。"),
        10, QFont::Normal, Theme::Text3, tip);
    tipLabel->setWordWrap(true);
    tipLayout->addWidget(tipLabel, 1);
    auto *ratio = new QWidget(tip);
    auto *ratioLayout = new QHBoxLayout(ratio);
    ratioLayout->setContentsMargins(0, 0, 0, 0);
    ratioLayout->setSpacing(4);
    ratioLayout->addWidget(Studio::iconLabel(Icons::Glyph::Palette, 10,
                                              Theme::Text2, ratio));
    ratioLayout->addWidget(Ui::label(QStringLiteral("支持 9:16 / 16:9"), 10,
                                      QFont::Normal, Theme::Text2, ratio));
    tipLayout->addWidget(ratio, 0, Qt::AlignVCenter);
    m_hubLayout->addWidget(tip);
}

QWidget *OralPage::buildHubHero()
{
    auto *card = new OralHeroButton(m_hubContent);
    auto *layout = new QHBoxLayout(card);
    layout->setContentsMargins(16, 16, 16, 16);
    layout->setSpacing(12);

    auto *left = new QWidget(card);
    auto *leftLayout = new QVBoxLayout(left);
    leftLayout->setContentsMargins(0, 0, 0, 0);
    leftLayout->setSpacing(0);
    leftLayout->addWidget(
        makeSoftPill(Icons::Glyph::Sparkle, QStringLiteral("AI 口播包装"),
                     Theme::Primary, Theme::alpha(Theme::Primary, 0.14),
                     Theme::alpha(Theme::Primary, 0.30), left),
        0, Qt::AlignLeft);
    leftLayout->addSpacing(9);
    leftLayout->addWidget(Ui::label(QStringLiteral("真人口播 · 一键包装"), 17,
                                    QFont::DemiBold, Theme::Text1, left));
    leftLayout->addSpacing(4);
    auto *description = Ui::label(
        QStringLiteral("上传一段真人口播和相关素材，AI 理解内容后自动插入画面、字幕与节奏包装，直接生成可发布的成片。"),
        11, QFont::Normal, Theme::Text2, left);
    description->setWordWrap(true);
    description->setMaximumWidth(350);
    leftLayout->addWidget(description);
    leftLayout->addSpacing(10);
    leftLayout->addWidget(makeBullet(QStringLiteral("智能素材匹配，B-Roll 自动插入"),
                                     Theme::Primary, left));
    leftLayout->addWidget(makeBullet(QStringLiteral("模板、字幕、转场、BGM 一次到位"),
                                     Theme::Primary, left));
    leftLayout->addWidget(makeBullet(
        QStringLiteral("成片生成后自动下载，也可同时生成剪映草稿"), Theme::Primary,
        left));
    leftLayout->addStretch(1);

    auto *action = new QWidget(left);
    auto *actionLayout = new QHBoxLayout(action);
    actionLayout->setContentsMargins(0, 0, 0, 0);
    actionLayout->setSpacing(4);
    auto *actionLabel = Ui::label(QStringLiteral("开始口播包装"), 11,
                                  QFont::Medium, Theme::Primary, action);
    actionLayout->addWidget(actionLabel);
    actionLayout->addWidget(Studio::iconLabel(Icons::Glyph::ChevronRight, 11,
                                               Theme::Primary, action));
    actionLayout->addStretch(1);
    leftLayout->addWidget(action);

    layout->addWidget(left, 108);
    auto *art = new OralHeroArt(card);
    art->setTemplateId(m_project.templateId);
    layout->addWidget(art, 92);
    connect(card, &QAbstractButton::clicked, this, &OralPage::startProject);
    return card;
}

QWidget *OralPage::buildHubProcessPanel()
{
    auto *panel = new GradientPanel(m_hubContent);
    auto *layout = qobject_cast<QVBoxLayout *>(panel->layout());
    layout->setSpacing(0);

    auto *header = new QWidget(panel);
    auto *headerLayout = new QHBoxLayout(header);
    headerLayout->setContentsMargins(0, 0, 0, 0);
    headerLayout->setSpacing(8);
    headerLayout->addWidget(makeIconCircle(Icons::Glyph::Wand, 24, Theme::Ai,
                                           Studio::aiSoft(),
                                           Theme::alpha(Theme::Ai, 0.3), header),
                            0, Qt::AlignVCenter);
    auto *titles = new QVBoxLayout;
    titles->setContentsMargins(0, 0, 0, 0);
    titles->setSpacing(1);
    titles->addWidget(Ui::label(QStringLiteral("一次成片，三步完成"), 12,
                                QFont::DemiBold, Theme::Text1, header));
    titles->addWidget(Ui::label(QStringLiteral("全程无需剪辑，AI 自动完成"), 9,
                                QFont::Normal, Theme::Text3, header));
    headerLayout->addLayout(titles, 1);
    layout->addWidget(header);
    layout->addSpacing(12);

    struct StepInfo {
        QString title;
        QString description;
        Icons::Glyph glyph;
    };
    const QVector<StepInfo> steps = {
        {QStringLiteral("上传口播与素材"),
         QStringLiteral("一段真人口播，加上产品、场景等相关素材。"),
         Icons::Glyph::Upload},
        {QStringLiteral("选择包装模板"),
         QStringLiteral("6 套风格任选，字幕、B-Roll 与音乐自动适配。"),
         Icons::Glyph::Palette},
        {QStringLiteral("一键包装成片"),
         QStringLiteral("AI 理解内容、匹配画面并渲染，完成后自动下载。"),
         Icons::Glyph::Wand},
    };
    for (int index = 0; index < steps.size(); ++index) {
        auto *row = new QWidget(panel);
        auto *rowLayout = new QHBoxLayout(row);
        rowLayout->setContentsMargins(0, 0, 0, 0);
        rowLayout->setSpacing(10);
        auto *number = new QLabel(QString::number(index + 1), row);
        number->setFixedSize(24, 24);
        number->setAlignment(Qt::AlignCenter);
        number->setFont(Theme::monoFont(9));
        number->setStyleSheet(
            QStringLiteral("color:%1;background:%2;border:1px solid %3;"
                           "border-radius:12px;")
                .arg(Theme::Text2.name(), Theme::Surface2.name(),
                     Theme::BorderStrong.name()));
        rowLayout->addWidget(number, 0, Qt::AlignTop);
        auto *texts = new QVBoxLayout;
        texts->setContentsMargins(0, 0, 0, 0);
        texts->setSpacing(1);
        auto *titleRow = new QHBoxLayout;
        titleRow->setContentsMargins(0, 0, 0, 0);
        titleRow->setSpacing(5);
        titleRow->addWidget(Studio::iconLabel(steps.at(index).glyph, 10,
                                               Theme::Text3, row));
        titleRow->addWidget(Ui::label(steps.at(index).title, 10, QFont::Medium,
                                       Theme::Text1, row));
        titleRow->addStretch(1);
        texts->addLayout(titleRow);
        auto *desc = Ui::label(steps.at(index).description, 9, QFont::Normal,
                               Theme::Text3, row);
        desc->setWordWrap(true);
        texts->addWidget(desc);
        rowLayout->addLayout(texts, 1);
        layout->addWidget(row);
        if (index < steps.size() - 1) {
            layout->addSpacing(8);
        }
    }
    layout->addStretch(1);

    auto *note = new QWidget(panel);
    note->setObjectName(QStringLiteral("oralProcessNote"));
    note->setStyleSheet(
        QStringLiteral("#oralProcessNote { background:%1; border:1px solid %2;"
                       " border-radius:6px; }")
            .arg(Theme::alpha(Theme::Surface2, 0.6).name(QColor::HexArgb),
                 Theme::Border.name()));
    auto *noteLayout = new QHBoxLayout(note);
    noteLayout->setContentsMargins(10, 7, 10, 7);
    noteLayout->setSpacing(6);
    noteLayout->addWidget(Studio::iconLabel(Icons::Glyph::Clock, 9, Theme::Text3,
                                             note));
    auto *noteLabel = Ui::label(
        QStringLiteral("可切到其他页面，任务在后台继续，完成后自动下载"), 9,
        QFont::Normal, Theme::Text3, note);
    noteLabel->setWordWrap(true);
    noteLayout->addWidget(noteLabel, 1);
    layout->addSpacing(8);
    layout->addWidget(note);
    return panel;
}

QWidget *OralPage::createTemplateCard(const QString &templateId, bool selected,
                                      bool compact, QWidget *parent)
{
    const StudioData::OralTemplate tmpl = StudioData::oralTemplate(templateId);
    auto *card = new Card(
        selected ? Theme::mix(Theme::Surface1, Theme::Primary, 0.08) : Theme::Surface1,
        selected ? Theme::alpha(Theme::Primary, 0.5) : Theme::Border, 9, parent);
    card->setHoverEnabled(true);
    card->setClickable(true);
    card->setHoverColors(selected ? Theme::mix(Theme::Surface1, Theme::Primary, 0.10)
                                  : Theme::alpha(Theme::Surface2, 0.4),
                         selected ? Theme::alpha(Theme::Primary, 0.6)
                                  : Theme::BorderStrong);
    card->contentLayout()->setContentsMargins(10, 10, 10, 10);
    card->contentLayout()->setSpacing(10);

    auto *row = new QWidget(card);
    auto *layout = new QHBoxLayout(row);
    layout->setContentsMargins(0, 0, 0, 0);
    layout->setSpacing(10);

    auto *poster = new Studio::OralPosterWidget(row);
    poster->setFixedSize(compact ? 61 : 70, compact ? 108 : 124);
    Studio::OralPosterSpec spec;
    spec.templateId = templateId;
    spec.headline = tmpl.headline;
    spec.caption = tmpl.caption;
    spec.materials = StudioData::demoMaterials().mid(0, 1);
    spec.ratio = QStringLiteral("9:16");
    spec.badge = tmpl.name;
    spec.timecode.clear();
    poster->setSpec(spec);
    layout->addWidget(poster, 0, Qt::AlignTop);

    auto *texts = new QVBoxLayout;
    texts->setContentsMargins(0, 1, 0, 0);
    texts->setSpacing(0);
    auto *nameRow = new QHBoxLayout;
    nameRow->setContentsMargins(0, 0, 0, 0);
    nameRow->setSpacing(6);
    auto *dot = new QWidget(row);
    dot->setFixedSize(6, 6);
    dot->setStyleSheet(QStringLiteral("background:%1;border-radius:3px;")
                           .arg(cssColor(tmpl.accent)));
    nameRow->addWidget(dot, 0, Qt::AlignVCenter);
    nameRow->addWidget(Ui::label(tmpl.name, 11, QFont::Medium, Theme::Text1, row), 0);
    nameRow->addStretch(1);
    auto *check = new QWidget(row);
    check->setFixedSize(15, 15);
    check->setObjectName(QStringLiteral("oralTemplateCheck"));
    check->setStyleSheet(
        QStringLiteral("#oralTemplateCheck { background:%1; border:1px solid %2;"
                       " border-radius:7px; }")
            .arg(selected ? Theme::Primary.name() : QStringLiteral("transparent"),
                 selected ? Theme::Primary.name() : Theme::BorderStrong.name()));
    auto *checkLayout = new QHBoxLayout(check);
    checkLayout->setContentsMargins(0, 0, 0, 0);
    checkLayout->addWidget(
        Studio::iconLabel(Icons::Glyph::Check, 8,
                          selected ? Theme::OnAccent : QColor(0, 0, 0, 0), check),
        0, Qt::AlignCenter);
    nameRow->addWidget(check, 0, Qt::AlignVCenter);
    texts->addLayout(nameRow);
    texts->addSpacing(2);
    texts->addWidget(Ui::label(tmpl.tagline, 9, QFont::Normal, Theme::Text3, row));
    texts->addSpacing(4);
    auto *description = Ui::label(tmpl.description, 9, QFont::Normal, Theme::Text3,
                                  row);
    description->setWordWrap(true);
    description->setMaximumHeight(compact ? 34 : 44);
    texts->addWidget(description);
    texts->addStretch(1);

    auto *tags = new QWidget(row);
    auto *tagsLayout = new QHBoxLayout(tags);
    tagsLayout->setContentsMargins(0, 0, 0, 0);
    tagsLayout->setSpacing(4);
    const int tagCount = compact ? 2 : tmpl.tags.size();
    for (int index = 0; index < qMin(tagCount, tmpl.tags.size()); ++index) {
        tagsLayout->addWidget(Studio::tagLabel(tmpl.tags.at(index), Theme::Text2,
                                                Theme::Surface3, 8, tags),
                              0, Qt::AlignLeft);
    }
    tagsLayout->addStretch(1);
    texts->addWidget(tags);
    layout->addLayout(texts, 1);
    card->contentLayout()->addWidget(row);
    connect(card, &Card::clicked, this,
            [this, templateId] { chooseTemplate(templateId); });
    return card;
}

QWidget *OralPage::createRecentCard(const StudioData::OralResult &result,
                                    QWidget *parent)
{
    auto *card = new Card(Theme::Surface1, Theme::Border, 10, parent);
    card->setFixedHeight(142);
    card->setHoverEnabled(true);
    card->setClickable(true);
    card->setHoverColors(Theme::Surface1, Theme::BorderStrong);
    card->contentLayout()->setContentsMargins(10, 10, 10, 10);
    card->contentLayout()->setSpacing(0);

    auto *row = new QWidget(card);
    auto *layout = new QHBoxLayout(row);
    layout->setContentsMargins(0, 0, 0, 0);
    layout->setSpacing(12);

    auto *poster = new Studio::OralPosterWidget(row);
    poster->setFixedSize(66, 118);
    const StudioData::OralTemplate tmpl = StudioData::oralTemplate(result.templateId);
    Studio::OralPosterSpec spec;
    spec.templateId = result.templateId;
    spec.headline = result.projectName.split(QStringLiteral(" · ")).value(0);
    const QString caption = result.tags.value(1, result.tags.value(0)).mid(1);
    spec.caption = caption.isEmpty() ? tmpl.caption : caption;
    spec.materials = StudioData::demoMaterials().mid(0, 1);
    spec.ratio = result.ratio;
    spec.badge = tmpl.name;
    spec.timecode = result.durationLabel;
    poster->setSpec(spec);
    layout->addWidget(poster, 0, Qt::AlignTop);

    auto *texts = new QWidget(row);
    auto *textsLayout = new QVBoxLayout(texts);
    textsLayout->setContentsMargins(0, 1, 0, 0);
    textsLayout->setSpacing(0);
    auto *top = new QHBoxLayout;
    top->setContentsMargins(0, 0, 0, 0);
    top->setSpacing(6);
    top->addWidget(new Chip(QStringLiteral("已完成"), Chip::Ok, texts), 0);
    top->addStretch(1);
    auto *time = Ui::label(StudioData::formatGenerationTime(result.generatedAt), 9,
                           QFont::Normal, Theme::Text3, texts);
    time->setToolTip(QStringLiteral("生成时间：%1")
                         .arg(StudioData::formatGenerationFullTime(result.generatedAt)));
    top->addWidget(time, 0);
    textsLayout->addLayout(top);
    textsLayout->addSpacing(6);
    auto *name = new ElidedLabel(result.projectName, texts);
    name->setPixelSize(12);
    name->setFontWeight(QFont::Medium);
    name->setTextColor(Theme::Text1);
    textsLayout->addWidget(name);
    textsLayout->addSpacing(2);
    auto *meta = Ui::label(QStringLiteral("%1 · %2 · %3")
                               .arg(tmpl.name, result.durationLabel, result.ratio),
                           9, QFont::Normal, Theme::Text3, texts);
    textsLayout->addWidget(meta);
    textsLayout->addStretch(1);
    auto *bottom = new QHBoxLayout;
    bottom->setContentsMargins(0, 0, 0, 0);
    bottom->setSpacing(4);
    auto *title = new ElidedLabel(result.title, texts);
    title->setPixelSize(10);
    title->setTextColor(Theme::Text2);
    bottom->addWidget(title, 1);
    bottom->addWidget(Studio::iconLabel(Icons::Glyph::ChevronRight, 11,
                                         Theme::Text3, texts));
    textsLayout->addLayout(bottom);
    layout->addWidget(texts, 1);
    card->contentLayout()->addWidget(row);
    connect(card, &Card::clicked, this,
            [this, result] { openRecent(result); });
    return card;
}

/* ------------------------------------------------------------------ */
/* 创建流程                                                             */
/* ------------------------------------------------------------------ */

void OralPage::startProject()
{
    if (isRunning()) {
        const auto answer = QMessageBox::question(
            this, QStringLiteral("开始新项目"),
            QStringLiteral("当前有正在生成的智能口播，开始新项目会取消它，是否继续？"),
            QMessageBox::Yes | QMessageBox::No, QMessageBox::No);
        if (answer != QMessageBox::Yes) {
            return;
        }
        m_runTimer->stop();
        m_hasRun = false;
        emit taskStatusChanged(QString(), false);
    }

    if (m_playTimer) {
        m_playTimer->stop();
    }
    if (m_draftTimer) {
        m_draftTimer->stop();
    }
    m_playing = false;
    m_playProgress = 0;
    m_draftState = 0;
    m_draftProgress = 0;

    const QString templateId = m_project.templateId;
    m_project = StudioData::defaultOralProject();
    m_project.templateId = templateId;
    m_transcriptExpanded = false;
    showSetup();
}

void OralPage::rebuildSetup()
{
    auto *root = qobject_cast<QVBoxLayout *>(m_setupScreen->layout());
    if (!root) {
        root = new QVBoxLayout(m_setupScreen);
        root->setContentsMargins(0, 0, 0, 0);
        root->setSpacing(0);
    }
    int scrollValue = 0;
    if (auto *oldScroll = m_setupScreen->findChild<QScrollArea *>()) {
        if (oldScroll->verticalScrollBar()) {
            scrollValue = oldScroll->verticalScrollBar()->value();
        }
    }
    Studio::clearLayout(root);

    root->addWidget(buildSetupHeader());
    root->addWidget(Ui::separator(false, m_setupScreen));

    auto *scrollContent = new QWidget;
    auto *body = new QVBoxLayout(scrollContent);
    body->setContentsMargins(22, 14, 22, 16);
    body->setSpacing(0);
    body->setAlignment(Qt::AlignTop);
    body->addWidget(m_project.step == 1 ? buildSetupStepOne()
                                        : buildSetupStepTwo());
    auto *scroll = makeScrollArea(scrollContent, m_setupScreen);
    root->addWidget(scroll, 1);
    root->addWidget(Ui::separator(false, m_setupScreen));
    root->addWidget(buildSetupFooter());

    QTimer::singleShot(0, scroll, [scroll, scrollValue] {
        if (scroll->verticalScrollBar()) {
            scroll->verticalScrollBar()->setValue(scrollValue);
        }
    });
}

QWidget *OralPage::buildSetupHeader()
{
    auto *header = new QWidget(m_setupScreen);
    auto *layout = new QVBoxLayout(header);
    layout->setContentsMargins(22, 10, 22, 10);
    layout->setSpacing(0);

    auto *breadcrumb = new QWidget(header);
    auto *breadcrumbLayout = new QHBoxLayout(breadcrumb);
    breadcrumbLayout->setContentsMargins(0, 0, 0, 0);
    breadcrumbLayout->setSpacing(6);
    auto *back = new LinkLabel(QStringLiteral("智能口播"), breadcrumb);
    back->setPixelSize(10);
    back->setTextColor(Theme::Text3);
    back->setHoverColor(Theme::Text1);
    back->setLeadingGlyph(Icons::Glyph::ChevronLeft, 10);
    back->setUnderlineOnHover(false);
    connect(back, &QAbstractButton::clicked, this, &OralPage::showHub);
    breadcrumbLayout->addWidget(back);
    breadcrumbLayout->addWidget(Ui::label(QStringLiteral("/"), 10, QFont::Normal,
                                           Theme::BorderStrong, breadcrumb));
    breadcrumbLayout->addWidget(Ui::label(QStringLiteral("真人口播"), 10,
                                           QFont::Normal, Theme::Text2, breadcrumb));
    breadcrumbLayout->addWidget(Ui::label(QStringLiteral("/"), 10, QFont::Normal,
                                           Theme::BorderStrong, breadcrumb));
    breadcrumbLayout->addWidget(
        Ui::label(m_project.step == 1 ? QStringLiteral("上传素材")
                                      : QStringLiteral("包装设置"),
                  10, QFont::Normal, Theme::Text2, breadcrumb));
    auto *savedDot = new StatusDot(6, Theme::Success, breadcrumb);
    breadcrumbLayout->addSpacing(4);
    breadcrumbLayout->addWidget(savedDot);
    breadcrumbLayout->addWidget(Ui::label(QStringLiteral("草稿已自动保存"), 9,
                                           QFont::Normal, Theme::Text3, breadcrumb));
    breadcrumbLayout->addStretch(1);
    auto *exit = new Button(QStringLiteral("退出编辑"), Button::Ghost, breadcrumb);
    exit->setCompact(true);
    connect(exit, &QAbstractButton::clicked, this, &OralPage::showHub);
    breadcrumbLayout->addWidget(exit);
    layout->addWidget(breadcrumb);
    layout->addSpacing(5);

    auto *titleRow = new QWidget(header);
    auto *titleLayout = new QHBoxLayout(titleRow);
    titleLayout->setContentsMargins(0, 0, 0, 0);
    titleLayout->setSpacing(10);
    auto *titles = new QVBoxLayout;
    titles->setContentsMargins(0, 0, 0, 0);
    titles->setSpacing(1);
    auto *titleLine = new QHBoxLayout;
    titleLine->setContentsMargins(0, 0, 0, 0);
    titleLine->setSpacing(8);
    titleLine->addWidget(Ui::label(QStringLiteral("真人口播包装"), 16,
                                    QFont::DemiBold, Theme::Text1, header));
    if (m_project.step == 1 && m_project.hasSource && m_project.source.sample) {
        titleLine->addWidget(new Chip(QStringLiteral("示例项目"), Chip::Ai, header));
    }
    titleLine->addStretch(1);
    titles->addLayout(titleLine);
    auto *description = Ui::label(
        m_project.step == 1
            ? QStringLiteral("添加一段真人口播和相关素材，AI 会先理解内容，再按模板完成包装。")
            : QStringLiteral("选择包装模板和细节，AI 会按口播节奏自动生成字幕、B-Roll 与音乐。"),
        11, QFont::Normal, Theme::Text2, header);
    description->setWordWrap(true);
    titles->addWidget(description);
    titleLayout->addLayout(titles, 1);

    auto *flow = new QWidget(header);
    auto *flowLayout = new QHBoxLayout(flow);
    flowLayout->setContentsMargins(0, 2, 0, 0);
    flowLayout->setSpacing(5);
    auto addFlow = [&](const QString &text) {
        auto *chip = new QLabel(text, flow);
        chip->setFont(Theme::font(9));
        chip->setStyleSheet(
            QStringLiteral("color:%1;background:%2;border:1px solid %3;"
                           "border-radius:4px;padding:1px 6px;")
                .arg(Theme::Text3.name(), Theme::Surface2.name(),
                     Theme::Border.name()));
        flowLayout->addWidget(chip);
    };
    addFlow(QStringLiteral("口播原片"));
    flowLayout->addWidget(Ui::label(QStringLiteral("→"), 9, QFont::Normal,
                                    Theme::BorderStrong, flow));
    addFlow(QStringLiteral("AI 包装"));
    flowLayout->addWidget(Ui::label(QStringLiteral("→"), 9, QFont::Normal,
                                    Theme::BorderStrong, flow));
    addFlow(QStringLiteral("成片"));
    titleLayout->addWidget(flow, 0, Qt::AlignTop);
    layout->addWidget(titleRow);
    layout->addSpacing(10);

    auto *rail = new Studio::StepRail(header);
    rail->setSteps({QStringLiteral("上传素材"), QStringLiteral("包装设置"),
                    QStringLiteral("生成成片")},
                   {QStringLiteral("口播 + 相关素材"), QStringLiteral("模板与输出"),
                    QStringLiteral("AI 自动包装")});
    rail->setActiveStep(m_project.step);
    connect(rail, &Studio::StepRail::stepSelected, this, [this](int step) {
        if (step == 1) {
            m_project.step = 1;
            rebuildSetup();
        }
    });
    layout->addWidget(rail);
    return header;
}

QWidget *OralPage::buildSetupStepOne()
{
    auto *content = new QWidget;
    auto *layout = new QVBoxLayout(content);
    layout->setContentsMargins(0, 0, 0, 0);
    layout->setSpacing(0);

    if (m_project.hasSource && m_project.source.sample) {
        auto *note = new Studio::Note(
            QStringLiteral(
                "<b style=\"color:%1\">已载入示例项目</b>：这段口播已经过 ASR 识别，"
                "素材也已打好标签，可直接下一步体验 AI 包装；替换口播或素材后会重新识别。")
                .arg(Theme::Text1.name()),
            Studio::Note::Ai);
        auto *clear = new LinkLabel(QStringLiteral("清空示例"), note);
        clear->setPixelSize(10);
        clear->setTextColor(Theme::Ai);
        clear->setHoverColor(Theme::Text1);
        clear->setUnderlineOnHover(true);
        connect(clear, &QAbstractButton::clicked, this, [this] {
            clearSource();
            emit toastRequested(QStringLiteral("已清空示例，请上传自己的口播与素材"));
        });
        note->rowLayout()->addWidget(clear, 0, Qt::AlignTop);
        layout->addWidget(note);
        layout->addSpacing(12);
    }

    auto *grid = new QWidget(content);
    auto *gridLayout = new QHBoxLayout(grid);
    gridLayout->setContentsMargins(0, 0, 0, 0);
    gridLayout->setSpacing(14);

    auto *sourceCard = new Studio::SectionCard(
        Icons::Glyph::Film, QStringLiteral("口播视频"), QStringLiteral("必填"), grid);
    auto *sourceAction = new Button(
        m_project.hasSource ? QStringLiteral("替换视频") : QStringLiteral("上传视频"),
        Button::Ghost, sourceCard);
    sourceAction->setCompact(true);
    sourceAction->setLeadingGlyph(Icons::Glyph::Upload);
    connect(sourceAction, &QAbstractButton::clicked, this, &OralPage::openVideoFile);
    sourceCard->headerLayout()->addWidget(sourceAction);
    sourceCard->setBodyMargins(QMargins(12, 12, 12, 12));
    auto *sourceBody = sourceCard->bodyLayout();
    sourceBody->setSpacing(0);

    if (!m_project.hasSource) {
        auto *drop = new Studio::DropZone(sourceCard);
        drop->setMinimumHeight(268);
        auto *dropLayout = drop->contentLayout();
        dropLayout->setAlignment(Qt::AlignCenter);
        dropLayout->addWidget(makeIconCircle(Icons::Glyph::Upload, 44, Theme::Text2,
                                              Theme::Surface2, Theme::BorderStrong,
                                              drop),
                              0, Qt::AlignHCenter);
        dropLayout->addSpacing(2);
        dropLayout->addWidget(Ui::label(QStringLiteral("拖入口播视频，或上传本地文件"),
                                        12, QFont::Medium, Theme::Text1, drop),
                              0, Qt::AlignHCenter);
        auto *hint = Ui::label(
            QStringLiteral("支持 MP4 / MOV，建议 1080p 以上，1–10 分钟；人声越清晰，识别越准。"),
            10, QFont::Normal, Theme::Text3, drop);
        hint->setWordWrap(true);
        hint->setAlignment(Qt::AlignCenter);
        hint->setMaximumWidth(340);
        dropLayout->addWidget(hint, 0, Qt::AlignHCenter);
        auto *buttons = new QWidget(drop);
        auto *buttonsLayout = new QHBoxLayout(buttons);
        buttonsLayout->setContentsMargins(0, 2, 0, 0);
        buttonsLayout->setSpacing(8);
        auto *upload = new Button(QStringLiteral("上传口播视频"), Button::Primary,
                                  buttons);
        upload->setLeadingGlyph(Icons::Glyph::Upload);
        connect(upload, &QAbstractButton::clicked, this, &OralPage::openVideoFile);
        auto *sample = new Button(QStringLiteral("使用示例口播"), Button::Default,
                                  buttons);
        connect(sample, &QAbstractButton::clicked, this, &OralPage::setSourceSample);
        buttonsLayout->addWidget(upload);
        buttonsLayout->addWidget(sample);
        dropLayout->addWidget(buttons, 0, Qt::AlignHCenter);
        connect(drop, &Studio::DropZone::filesDropped, this,
                [this](const QStringList &paths) {
                    if (paths.isEmpty()) {
                        return;
                    }
                    const QFileInfo info(paths.first());
                    m_project.source = StudioData::demoOralSource();
                    m_project.source.name = info.fileName();
                    m_project.source.size = QStringLiteral("%1 MB")
                                                .arg(qMax<qint64>(
                                                    1, qRound64(info.size()
                                                                / (1024.0 * 1024.0))));
                    m_project.source.sample = false;
                    m_project.hasSource = true;
                    rebuildSetup();
                    emit toastRequested(
                        QStringLiteral("已导入口播视频，AI 正在识别画面与口播内容"));
                });
        sourceBody->addWidget(drop);
    } else {
        auto *preview = new SourcePreview(m_project.source, sourceCard);
        connect(preview, &QAbstractButton::clicked, this, [this] {
            emit toastRequested(QStringLiteral("正在播放口播原片（原型演示）"));
        });
        sourceBody->addWidget(preview);
        sourceBody->addSpacing(8);

        auto *fileRow = new QWidget(sourceCard);
        auto *fileLayout = new QHBoxLayout(fileRow);
        fileLayout->setContentsMargins(0, 0, 0, 0);
        fileLayout->setSpacing(8);
        auto *fileTexts = new QVBoxLayout;
        fileTexts->setContentsMargins(0, 0, 0, 0);
        fileTexts->setSpacing(1);
        auto *name = new ElidedLabel(m_project.source.name, fileRow);
        name->setPixelSize(11);
        name->setFontWeight(QFont::Medium);
        name->setTextColor(Theme::Text1);
        fileTexts->addWidget(name);
        auto *meta = Ui::label(QStringLiteral("%1 · %2 · 本地文件")
                                   .arg(m_project.source.size,
                                        m_project.source.resolution),
                               9, QFont::Normal, Theme::Text3, fileRow);
        fileTexts->addWidget(meta);
        fileLayout->addLayout(fileTexts, 1);
        auto *remove = new LinkLabel(QStringLiteral("移除"), fileRow);
        remove->setPixelSize(9);
        remove->setTextColor(Theme::Text3);
        remove->setHoverColor(Theme::Danger);
        remove->setLeadingGlyph(Icons::Glyph::Trash, 9);
        remove->setUnderlineOnHover(false);
        connect(remove, &QAbstractButton::clicked, this, [this] {
            m_project.hasSource = false;
            m_project.source = StudioData::OralSource();
            rebuildSetup();
        });
        fileLayout->addWidget(remove, 0, Qt::AlignVCenter);
        sourceBody->addWidget(fileRow);
        sourceBody->addSpacing(10);

        auto *stats = new QWidget(sourceCard);
        auto *statsLayout = new QHBoxLayout(stats);
        statsLayout->setContentsMargins(0, 0, 0, 0);
        statsLayout->setSpacing(6);
        statsLayout->addWidget(makeSourceBox(QStringLiteral("口播时长"),
                                             m_project.source.duration, stats));
        statsLayout->addWidget(makeSourceBox(
            QStringLiteral("识别字数"),
            QStringLiteral("%1 字").arg(m_project.source.words), stats));
        statsLayout->addWidget(makeSourceBox(
            QStringLiteral("语义段"),
            QStringLiteral("%1 段").arg(m_project.source.segments), stats));
        sourceBody->addWidget(stats);
        sourceBody->addSpacing(10);

        auto *topics = new QWidget(sourceCard);
        auto *topicsLayout = new QHBoxLayout(topics);
        topicsLayout->setContentsMargins(0, 0, 0, 0);
        topicsLayout->setSpacing(5);
        topicsLayout->addWidget(Studio::iconLabel(Icons::Glyph::Sparkle, 9,
                                                   Theme::Ai, topics));
        topicsLayout->addWidget(Ui::label(QStringLiteral("关键主题"), 9, QFont::Normal,
                                           Theme::Text3, topics));
        for (const QString &topic : m_project.source.topics) {
            topicsLayout->addWidget(Studio::tagLabel(
                topic, Theme::Ai, Theme::alpha(Theme::Ai, 0.13), 9, topics));
        }
        topicsLayout->addStretch(1);
        sourceBody->addWidget(topics);
        sourceBody->addSpacing(10);

        auto *transcript = new Card(Theme::alpha(Theme::Surface2, 0.4),
                                    Theme::Border, 8, sourceCard);
        transcript->contentLayout()->setContentsMargins(0, 0, 0, 0);
        transcript->contentLayout()->setSpacing(0);
        auto *transcriptHeader = new QWidget(transcript);
        auto *transcriptHeaderLayout = new QHBoxLayout(transcriptHeader);
        transcriptHeaderLayout->setContentsMargins(10, 6, 10, 6);
        transcriptHeaderLayout->setSpacing(6);
        transcriptHeaderLayout->addWidget(
            Studio::iconLabel(Icons::Glyph::Sparkle, 9, Theme::Ai, transcriptHeader));
        transcriptHeaderLayout->addWidget(Ui::label(QStringLiteral("AI 口播稿"), 10,
                                                     QFont::Medium, Theme::Text2,
                                                     transcriptHeader));
        transcriptHeaderLayout->addWidget(
            new Chip(QStringLiteral("已识别"), Chip::Ok, transcriptHeader));
        transcriptHeaderLayout->addStretch(1);
        auto *confidence = monoLabel(QStringLiteral("置信度 98.2%"), 9,
                                     Theme::Text3, transcriptHeader);
        transcriptHeaderLayout->addWidget(confidence);
        auto *expand = new LinkLabel(
            m_transcriptExpanded ? QStringLiteral("收起") : QStringLiteral("展开全文"),
            transcriptHeader);
        expand->setPixelSize(9);
        expand->setTextColor(Theme::Primary);
        expand->setHoverColor(Theme::PrimaryHover);
        expand->setUnderlineOnHover(true);
        connect(expand, &QAbstractButton::clicked, this, [this] {
            m_transcriptExpanded = !m_transcriptExpanded;
            rebuildSetup();
        });
        transcriptHeaderLayout->addWidget(expand);
        transcript->contentLayout()->addWidget(transcriptHeader);
        transcript->contentLayout()->addWidget(Ui::separator(false, transcript));
        const QVector<StudioData::OralTranscriptLine> lines =
            m_transcriptExpanded
            ? m_project.source.transcript
            : m_project.source.transcript.mid(0, 3);
        for (int index = 0; index < lines.size(); ++index) {
            const StudioData::OralTranscriptLine &line = lines.at(index);
            auto *lineRow = new QWidget(transcript);
            auto *lineLayout = new QHBoxLayout(lineRow);
            lineLayout->setContentsMargins(10, 5, 10, 5);
            lineLayout->setSpacing(9);
            lineLayout->addWidget(monoLabel(line.time, 8, Theme::Text3, lineRow), 0,
                                  Qt::AlignTop);
            auto *textLabel = Ui::richLabel(highlightedHtml(line.text, line.highlight),
                                            10, Theme::Text2, lineRow);
            textLabel->setWordWrap(true);
            lineLayout->addWidget(textLabel, 1);
            transcript->contentLayout()->addWidget(lineRow);
            if (index < lines.size() - 1) {
                transcript->contentLayout()->addWidget(Ui::separator(false, transcript));
            }
        }
        sourceBody->addWidget(transcript);
    }

    auto *materialsCard = new Studio::SectionCard(
        Icons::Glyph::Image, QStringLiteral("相关素材"),
        QStringLiteral("%1 个").arg(m_project.materials.size()), grid);
    auto *addButton = new Button(QStringLiteral("添加"), Button::Default, materialsCard);
    addButton->setCompact(true);
    addButton->setLeadingGlyph(Icons::Glyph::Plus);
    connect(addButton, &QAbstractButton::clicked, this,
            &OralPage::openMaterialFiles);
    materialsCard->headerLayout()->addWidget(addButton);
    materialsCard->setBodyMargins(QMargins(12, 12, 12, 12));
    auto *materialsBody = materialsCard->bodyLayout();
    materialsBody->setSpacing(0);
    auto *materialsHint = Ui::label(
        QStringLiteral("添加与口播内容相关的产品、场景或细节素材，AI 会按语义自动匹配并插入画面。"),
        10, QFont::Normal, Theme::Text3, materialsCard);
    materialsHint->setWordWrap(true);
    materialsBody->addWidget(materialsHint);
    materialsBody->addSpacing(10);

    auto *tileHost = new QWidget(materialsCard);
    auto *tileGrid = new QGridLayout(tileHost);
    tileGrid->setContentsMargins(0, 0, 0, 0);
    tileGrid->setHorizontalSpacing(6);
    tileGrid->setVerticalSpacing(6);
    int tileIndex = 0;
    for (const StudioData::OralMaterial &material : m_project.materials) {
        auto *tile = new MaterialTile(
            material, [this, id = material.id] { removeMaterial(id); }, tileHost);
        tileGrid->addWidget(tile, tileIndex / 3, tileIndex % 3);
        ++tileIndex;
    }
    auto *addTile = new AddMaterialTile(tileHost);
    connect(addTile, &QAbstractButton::clicked, this, &OralPage::openMaterialFiles);
    tileGrid->addWidget(addTile, tileIndex / 3, tileIndex % 3);
    for (int column = 0; column < 3; ++column) {
        tileGrid->setColumnStretch(column, 1);
    }
    materialsBody->addWidget(tileHost);
    materialsBody->addSpacing(10);

    auto *estimate = new QWidget(materialsCard);
    estimate->setObjectName(QStringLiteral("oralEstimate"));
    estimate->setStyleSheet(
        QStringLiteral("#oralEstimate { background:%1; border:1px solid %2;"
                       " border-radius:7px; }")
            .arg(Theme::alpha(Theme::Surface2, 0.45).name(QColor::HexArgb),
                 Theme::Border.name()));
    auto *estimateLayout = new QVBoxLayout(estimate);
    estimateLayout->setContentsMargins(10, 8, 10, 8);
    estimateLayout->setSpacing(4);
    if (!m_project.materials.isEmpty()) {
        auto *top = new QHBoxLayout;
        top->setContentsMargins(0, 0, 0, 0);
        top->setSpacing(5);
        top->addWidget(Studio::iconLabel(Icons::Glyph::Sparkle, 9, Theme::Ai,
                                          estimate));
        top->addWidget(Ui::label(QStringLiteral("AI 匹配预估"), 9, QFont::Normal,
                                  Theme::Text2, estimate));
        top->addStretch(1);
        auto *match = monoLabel(QStringLiteral("相关度 92%"), 9, Theme::Success,
                                estimate);
        top->addWidget(match);
        estimateLayout->addLayout(top);
        auto *estimateText = Ui::label(
            QStringLiteral("预计插入 %1 处 B-Roll，覆盖约 68% 口播时长；可在下一步调整插入密度。")
                .arg(StudioData::oralBrollCount(m_project.density)),
            9, QFont::Normal, Theme::Text3, estimate);
        estimateText->setWordWrap(true);
        estimateLayout->addWidget(estimateText);
        auto *bars = new QWidget(estimate);
        auto *barsLayout = new QHBoxLayout(bars);
        barsLayout->setContentsMargins(0, 2, 0, 0);
        barsLayout->setSpacing(3);
        for (int index = 0; index < 8; ++index) {
            auto *bar = new QWidget(bars);
            bar->setFixedHeight(4);
            bar->setStyleSheet(
                QStringLiteral("background:%1;border-radius:2px;")
                    .arg(index < qMin(8, m_project.materials.size())
                             ? Theme::alpha(Theme::Success, 0.7).name()
                             : Theme::BorderStrong.name()));
            bar->setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
            if (index < m_project.materials.size()) {
                bar->setToolTip(QStringLiteral("%1 · %2")
                                    .arg(m_project.materials.at(index).name,
                                         m_project.materials.at(index).tag));
            } else {
                bar->setToolTip(QStringLiteral("待补充素材"));
            }
            barsLayout->addWidget(bar);
        }
        estimateLayout->addWidget(bars);
    } else {
        auto *empty = Ui::label(
            QStringLiteral("至少添加 1 个相关素材，才能让 AI 自动插入画面。"), 9,
            QFont::Normal, Theme::Text3, estimate);
        empty->setWordWrap(true);
        estimateLayout->addWidget(empty);
    }
    materialsBody->addWidget(estimate);
    materialsBody->addSpacing(8);

    if (!m_project.materials.isEmpty()) {
        auto *restore = new LinkLabel(QStringLiteral("恢复示例素材"), materialsCard);
        restore->setPixelSize(9);
        restore->setTextColor(Theme::Text3);
        restore->setHoverColor(Theme::Text1);
        restore->setUnderlineOnHover(true);
        connect(restore, &QAbstractButton::clicked, this, [this] {
            m_project.materials = StudioData::demoMaterials();
            rebuildSetup();
            emit toastRequested(QStringLiteral("已恢复示例素材"));
        });
        materialsBody->addWidget(restore, 0, Qt::AlignLeft);
    } else {
        auto *suggestTitle = Ui::label(
            QStringLiteral("还没有素材？先用示例素材体验 AI 匹配："), 9,
            QFont::Normal, Theme::Text3, materialsCard);
        materialsBody->addWidget(suggestTitle);
        materialsBody->addSpacing(6);
        auto *suggest = new QWidget(materialsCard);
        auto *suggestLayout = new QGridLayout(suggest);
        suggestLayout->setContentsMargins(0, 0, 0, 0);
        suggestLayout->setHorizontalSpacing(6);
        suggestLayout->setVerticalSpacing(6);
        const QVector<StudioData::OralMaterial> demos =
            StudioData::demoMaterials().mid(0, 4);
        for (int index = 0; index < demos.size(); ++index) {
            const StudioData::OralMaterial material = demos.at(index);
            auto *sample = new Card(Theme::alpha(Theme::Surface2, 0.45),
                                    Theme::Border, 6, suggest);
            sample->setHoverEnabled(true);
            sample->setClickable(true);
            sample->setHoverColors(Theme::alpha(Theme::Primary, 0.10),
                                   Theme::alpha(Theme::Primary, 0.45));
            sample->contentLayout()->setContentsMargins(4, 4, 4, 4);
            auto *sampleLayout = new QHBoxLayout;
            sampleLayout->setContentsMargins(0, 0, 0, 0);
            sampleLayout->setSpacing(5);
            auto *art = new Studio::MaterialVisualWidget(sample);
            art->setMaterial(material);
            art->setFixedSize(36, 28);
            sampleLayout->addWidget(art);
            auto *name = new ElidedLabel(material.name, sample);
            name->setPixelSize(9);
            name->setTextColor(Theme::Text2);
            sampleLayout->addWidget(name, 1);
            sampleLayout->addWidget(Studio::iconLabel(Icons::Glyph::Plus, 9,
                                                       Theme::Text3, sample));
            sample->contentLayout()->addLayout(sampleLayout);
            connect(sample, &Card::clicked, this, [this, material] {
                m_project.materials.append(material);
                rebuildSetup();
            });
            suggestLayout->addWidget(sample, index / 2, index % 2);
        }
        materialsBody->addWidget(suggest);
    }

    gridLayout->addWidget(sourceCard, 142);
    gridLayout->addWidget(materialsCard, 100);
    layout->addWidget(grid);
    layout->addSpacing(12);
    layout->addWidget(new Studio::Note(
        QStringLiteral("上传后 AI 会先识别口播，再按语义选择素材位置。你可以在下一步选择包装模板、字幕样式与 B-Roll 密度，全部设置都可在生成前调整。")));
    return content;
}

QWidget *OralPage::buildSetupStepTwo()
{
    auto *content = new QWidget;
    auto *layout = new QHBoxLayout(content);
    layout->setContentsMargins(0, 0, 0, 0);
    layout->setSpacing(14);

    auto *left = new QWidget(content);
    auto *leftLayout = new QVBoxLayout(left);
    leftLayout->setContentsMargins(0, 0, 0, 0);
    leftLayout->setSpacing(14);

    const StudioData::OralTemplate tmpl =
        StudioData::oralTemplate(m_project.templateId);
    auto *templateCard = new Studio::SectionCard(
        Icons::Glyph::Sparkle, QStringLiteral("包装模板"),
        QStringLiteral("已选：%1").arg(tmpl.name), left);
    templateCard->setBodyMargins(QMargins(12, 12, 12, 12));
    auto *templateGridHost = new QWidget(templateCard);
    auto *templateGrid = new QGridLayout(templateGridHost);
    templateGrid->setContentsMargins(0, 0, 0, 0);
    templateGrid->setHorizontalSpacing(8);
    templateGrid->setVerticalSpacing(8);
    const QVector<StudioData::OralTemplate> &templates = StudioData::oralTemplates();
    for (int index = 0; index < templates.size(); ++index) {
        templateGrid->addWidget(
            createTemplateCard(templates.at(index).id,
                               templates.at(index).id == m_project.templateId, true,
                               templateGridHost),
            index / 3, index % 3);
    }
    templateCard->bodyLayout()->addWidget(templateGridHost);
    leftLayout->addWidget(templateCard);

    auto *detailCard = new Studio::SectionCard(
        Icons::Glyph::Film, QStringLiteral("包装细节"), left);
    detailCard->setBodyMargins(QMargins(14, 14, 14, 14));
    auto *detailLayout = new QVBoxLayout;
    detailLayout->setContentsMargins(0, 0, 0, 0);
    detailLayout->setSpacing(0);
    auto *options = new QWidget(detailCard);
    auto *optionsLayout = new QHBoxLayout(options);
    optionsLayout->setContentsMargins(0, 0, 0, 0);
    optionsLayout->setSpacing(16);

    auto *subtitleColumn = new QVBoxLayout;
    subtitleColumn->setContentsMargins(0, 0, 0, 0);
    subtitleColumn->setSpacing(4);
    auto *subtitleHead = new QHBoxLayout;
    subtitleHead->setContentsMargins(0, 0, 0, 0);
    subtitleHead->addWidget(Ui::label(QStringLiteral("字幕样式"), 10, QFont::Medium,
                                       Theme::Text2, options));
    subtitleHead->addStretch(1);
    subtitleHead->addWidget(Ui::label(QStringLiteral("关键词自动高亮"), 9,
                                       QFont::Normal, Theme::Text3, options));
    subtitleColumn->addLayout(subtitleHead);
    auto *subtitle = new Studio::SegmentedControl(options);
    subtitle->setOptions({QStringLiteral("智能关键词"), QStringLiteral("大字强调"),
                          QStringLiteral("极简白字")});
    subtitle->setCurrentIndex(m_project.subtitleStyle
                                      == StudioData::OralSubtitleStyle::Keyword
                                  ? 0
                                  : (m_project.subtitleStyle
                                             == StudioData::OralSubtitleStyle::Big
                                         ? 1
                                         : 2));
    connect(subtitle, &Studio::SegmentedControl::currentChanged, this, [this](int index) {
        m_project.subtitleStyle = index == 0 ? StudioData::OralSubtitleStyle::Keyword
            : (index == 1 ? StudioData::OralSubtitleStyle::Big
                          : StudioData::OralSubtitleStyle::Minimal);
        rebuildSetup();
    });
    subtitleColumn->addWidget(subtitle);
    auto *subtitleHint = Ui::label(
        m_project.subtitleStyle == StudioData::OralSubtitleStyle::Keyword
            ? QStringLiteral("自动识别重点词并高亮，适合知识与测评内容。")
            : (m_project.subtitleStyle == StudioData::OralSubtitleStyle::Big
                   ? QStringLiteral("短句放大、节奏更快，适合带货与强观点内容。")
                   : QStringLiteral("统一的简洁白字，不抢画面。")),
        9, QFont::Normal, Theme::Text3, options);
    subtitleHint->setWordWrap(true);
    subtitleColumn->addWidget(subtitleHint);
    optionsLayout->addLayout(subtitleColumn, 1);

    auto *densityColumn = new QVBoxLayout;
    densityColumn->setContentsMargins(0, 0, 0, 0);
    densityColumn->setSpacing(4);
    auto *densityHead = new QHBoxLayout;
    densityHead->setContentsMargins(0, 0, 0, 0);
    densityHead->addWidget(Ui::label(QStringLiteral("B-Roll 密度"), 10,
                                       QFont::Medium, Theme::Text2, options));
    densityHead->addStretch(1);
    densityHead->addWidget(monoLabel(
        QStringLiteral("%1 处").arg(StudioData::oralBrollCount(m_project.density)), 9,
        Theme::Text3, options));
    densityColumn->addLayout(densityHead);
    auto *density = new Studio::SegmentedControl(options);
    density->setOptions({QStringLiteral("少"), QStringLiteral("适中"),
                         QStringLiteral("丰富")},
                        {QStringLiteral("8 处"), QStringLiteral("12 处"),
                         QStringLiteral("16 处")});
    density->setCompact(true);
    density->setCurrentIndex(m_project.density == StudioData::OralDensity::Low
                                 ? 0
                                 : (m_project.density
                                            == StudioData::OralDensity::High
                                        ? 2
                                        : 1));
    connect(density, &Studio::SegmentedControl::currentChanged, this, [this](int index) {
        m_project.density = index == 0 ? StudioData::OralDensity::Low
            : (index == 2 ? StudioData::OralDensity::High
                          : StudioData::OralDensity::Medium);
        rebuildSetup();
    });
    densityColumn->addWidget(density);
    auto *densityHint = Ui::label(
        m_project.density == StudioData::OralDensity::Low
            ? QStringLiteral("保留更多口播原画面，适合强信任内容。")
            : (m_project.density == StudioData::OralDensity::High
                   ? QStringLiteral("更多素材画面，节奏更紧凑，适合带货。")
                   : QStringLiteral("口播与素材穿插，兼顾信息密度与真实感。")),
        9, QFont::Normal, Theme::Text3, options);
    densityHint->setWordWrap(true);
    densityColumn->addWidget(densityHint);
    optionsLayout->addLayout(densityColumn, 1);

    detailLayout->addWidget(options);
    detailLayout->addSpacing(14);

    auto *toggles = new QWidget(detailCard);
    auto *toggleGrid = new QGridLayout(toggles);
    toggleGrid->setContentsMargins(0, 0, 0, 0);
    toggleGrid->setHorizontalSpacing(6);
    toggleGrid->setVerticalSpacing(6);
    auto makeSwitch = [this, toggles](const QString &title, const QString &description,
                                      bool checked, std::function<void(bool)> setter) {
        auto *row = new Studio::SwitchRow(title, description, checked, toggles);
        connect(row, &Studio::SwitchRow::toggled, this, [this, setter](bool value) {
            setter(value);
            rebuildSetup();
        });
        return row;
    };
    toggleGrid->addWidget(makeSwitch(
                              QStringLiteral("动态转场"),
                              QStringLiteral("片段衔接更自然"), m_project.transitions,
                              [this](bool value) { m_project.transitions = value; }),
                          0, 0);
    toggleGrid->addWidget(makeSwitch(
                              QStringLiteral("花字标题"),
                              QStringLiteral("重点句自动生成花字"), m_project.titleFx,
                              [this](bool value) { m_project.titleFx = value; }),
                          0, 1);
    toggleGrid->addWidget(makeSwitch(
                              QStringLiteral("背景音乐"),
                              QStringLiteral("按情绪匹配，人声自动闪避"), m_project.bgm,
                              [this](bool value) { m_project.bgm = value; }),
                          1, 0);
    toggleGrid->addWidget(makeSwitch(
                              QStringLiteral("音效点缀"),
                              QStringLiteral("转场与重点处补充音效"), m_project.sfx,
                              [this](bool value) { m_project.sfx = value; }),
                          1, 1);
    detailLayout->addWidget(toggles);
    detailCard->bodyLayout()->addLayout(detailLayout);
    leftLayout->addWidget(detailCard);
    leftLayout->addStretch(1);

    auto *right = new QWidget(content);
    auto *rightLayout = new QVBoxLayout(right);
    rightLayout->setContentsMargins(0, 0, 0, 0);
    rightLayout->setSpacing(12);

    auto *previewCard = new Studio::SectionCard(
        Icons::Glyph::Play, QStringLiteral("实时预览"), tmpl.name, right);
    auto *ratioChip = new QLabel(m_project.ratio, previewCard);
    ratioChip->setFont(Theme::monoFont(9));
    ratioChip->setStyleSheet(
        QStringLiteral("color:%1;background:%2;border:1px solid %3;"
                       "border-radius:4px;padding:1px 5px;")
            .arg(Theme::Text3.name(), Theme::Surface2.name(), Theme::Border.name()));
    previewCard->headerLayout()->addWidget(ratioChip);
    previewCard->setBodyMargins(QMargins(12, 12, 12, 12));
    auto *previewBody = previewCard->bodyLayout();
    previewBody->setSpacing(0);

    auto *stage = new DarkStage(previewCard);
    stage->setFixedHeight(392);
    auto *stageLayout = new QHBoxLayout(stage);
    stageLayout->setContentsMargins(12, 12, 12, 12);
    stageLayout->setAlignment(Qt::AlignCenter);
    const int previewHeight = m_project.ratio == QLatin1String("9:16") ? 392 : 212;
    const int previewWidth = m_project.ratio == QLatin1String("9:16")
        ? qRound(previewHeight * 9.0 / 16.0)
        : qRound(previewHeight * 16.0 / 9.0);
    auto *poster = new Studio::OralPosterWidget(stage);
    poster->setFixedSize(previewWidth, previewHeight);
    Studio::OralPosterSpec spec;
    spec.templateId = m_project.templateId;
    spec.headline = tmpl.headline;
    spec.caption = m_project.hasSource && !m_project.source.transcript.isEmpty()
        ? m_project.source.transcript.first().text
        : tmpl.caption;
    spec.keywords = m_project.hasSource ? m_project.source.topics.mid(0, 3)
                                        : QStringList();
    spec.materials = m_project.materials.mid(
        0, m_project.density == StudioData::OralDensity::Low ? 1 : 2);
    spec.ratio = m_project.ratio;
    spec.subtitleStyle = m_project.subtitleStyle;
    spec.timecode = QStringLiteral("00:58");
    poster->setSpec(spec);
    stageLayout->addWidget(poster, 0, Qt::AlignCenter);
    previewBody->addWidget(stage);
    previewBody->addSpacing(12);

    auto *ratioHead = new QHBoxLayout;
    ratioHead->setContentsMargins(0, 0, 0, 0);
    ratioHead->addWidget(Ui::label(QStringLiteral("成片画幅"), 10, QFont::Medium,
                                    Theme::Text2, previewCard));
    ratioHead->addStretch(1);
    ratioHead->addWidget(Ui::label(
        m_project.ratio == QLatin1String("9:16") ? QStringLiteral("适合抖音 / 视频号")
                                                  : QStringLiteral("适合 B 站 / 横屏"),
        9, QFont::Normal, Theme::Text3, previewCard));
    previewBody->addLayout(ratioHead);
    previewBody->addSpacing(5);
    auto *ratio = new Studio::SegmentedControl(previewCard);
    ratio->setOptions({QStringLiteral("9:16"), QStringLiteral("16:9")});
    ratio->setCurrentIndex(m_project.ratio == QLatin1String("9:16") ? 0 : 1);
    connect(ratio, &Studio::SegmentedControl::currentChanged, this,
            [this](int index) {
                applyRatio(index == 0 ? QStringLiteral("9:16")
                                      : QStringLiteral("16:9"));
            });
    previewBody->addWidget(ratio);
    previewBody->addSpacing(12);

    auto *outputHead = new QHBoxLayout;
    outputHead->setContentsMargins(0, 0, 0, 0);
    outputHead->addWidget(Ui::label(QStringLiteral("输出内容"), 10, QFont::Medium,
                                     Theme::Text2, previewCard));
    outputHead->addStretch(1);
    outputHead->addWidget(Ui::label(QStringLiteral("默认直出成片"), 9, QFont::Normal,
                                     Theme::Text3, previewCard));
    previewBody->addLayout(outputHead);
    previewBody->addSpacing(5);
    auto *videoOption = new OutputChoiceCard(
        QStringLiteral("直接生成成片"),
        QStringLiteral("MP4 · 1080p，生成后自动下载"), QStringLiteral("默认"),
        Icons::Glyph::Film, !m_project.outputBoth, previewCard);
    connect(videoOption, &QAbstractButton::clicked, this, [this] {
        if (!m_project.outputBoth) {
            return;
        }
        applyOutput(false);
    });
    previewBody->addWidget(videoOption);
    previewBody->addSpacing(6);
    auto *bothOption = new OutputChoiceCard(
        QStringLiteral("成片 + 剪映草稿"),
        QStringLiteral("成片自动下载，并生成可编辑草稿"), QString(),
        Icons::Glyph::Scissors, m_project.outputBoth, previewCard);
    connect(bothOption, &QAbstractButton::clicked, this, [this] {
        if (m_project.outputBoth) {
            return;
        }
        applyOutput(true);
    });
    previewBody->addWidget(bothOption);

    auto *note = new Studio::Note(
        QStringLiteral(
            "开始后展示「理解口播 → 匹配素材 → 智能包装 → 渲染」全过程，可后台运行，完成后通知你。"),
        Studio::Note::Primary);
    rightLayout->addWidget(previewCard);
    rightLayout->addWidget(note);
    rightLayout->addStretch(1);

    layout->addWidget(left, 147);
    layout->addWidget(right, 100);
    return content;
}

QWidget *OralPage::buildSetupFooter()
{
    auto *footer = new QWidget(m_setupScreen);
    footer->setFixedHeight(52);
    footer->setObjectName(QStringLiteral("oralFooter"));
    footer->setStyleSheet(
        QStringLiteral("#oralFooter { background:%1; }").arg(Theme::Surface1.name()));
    auto *layout = new QHBoxLayout(footer);
    layout->setContentsMargins(22, 0, 22, 0);
    layout->setSpacing(12);

    const bool ready = m_project.hasSource && !m_project.materials.isEmpty();
    auto *texts = new QVBoxLayout;
    texts->setContentsMargins(0, 0, 0, 0);
    texts->setSpacing(1);
    auto *summary = new ElidedLabel(
        m_project.step == 1
            ? (ready ? QStringLiteral("素材已就绪，AI 将先识别口播，再按语义匹配素材")
                     : QStringLiteral("至少需要 1 段口播视频和 1 个相关素材"))
            : QStringLiteral("预计成片 00:58 · %1 处 B-Roll · %2 · %3")
                  .arg(StudioData::oralBrollCount(m_project.density))
                  .arg(m_project.ratio)
                  .arg(m_project.outputBoth ? QStringLiteral("成片 + 剪映草稿")
                                            : QStringLiteral("成片 MP4")),
        footer);
    summary->setPixelSize(10);
    summary->setTextColor(Theme::Text2);
    texts->addWidget(summary);
    auto *hint = Ui::label(
        m_project.step == 1
            ? QStringLiteral("设置会随项目自动保存，可随时返回修改")
            : QStringLiteral("生成约 2–4 分钟；完成后自动下载，可后台运行"),
        9, QFont::Normal, Theme::Text3, footer);
    texts->addWidget(hint);
    layout->addLayout(texts, 1);

    if (m_project.step == 2) {
        auto *previous = new Button(QStringLiteral("上一步"), Button::Default, footer);
        connect(previous, &QAbstractButton::clicked, this, [this] {
            m_project.step = 1;
            rebuildSetup();
        });
        layout->addWidget(previous);
        auto *start = new Button(QStringLiteral("一键包装"), Button::Primary, footer);
        start->setLeadingGlyph(Icons::Glyph::Sparkle);
        start->setEnabled(ready);
        start->setToolTip(ready ? QStringLiteral("开始 AI 自动包装")
                                : QStringLiteral("请先补齐口播与素材"));
        connect(start, &QAbstractButton::clicked, this, &OralPage::startGeneration);
        layout->addWidget(start);
    } else {
        auto *next = new Button(QStringLiteral("下一步：包装设置"), Button::Primary,
                                footer);
        next->setEnabled(ready);
        next->setToolTip(ready ? QStringLiteral("选择包装模板与输出设置")
                               : QStringLiteral("请先添加口播视频和相关素材"));
        connect(next, &QAbstractButton::clicked, this, [this] {
            m_project.step = 2;
            rebuildSetup();
        });
        layout->addWidget(next);
    }
    return footer;
}

void OralPage::openVideoFile()
{
    const QString path = QFileDialog::getOpenFileName(
        this, QStringLiteral("选择口播视频"), QString(),
        QStringLiteral("视频文件 (*.mp4 *.mov *.m4v *.mkv *.avi *.webm)"));
    if (path.isEmpty()) {
        return;
    }
    const QFileInfo info(path);
    m_project.source = StudioData::demoOralSource();
    m_project.source.name = info.fileName();
    m_project.source.size = QStringLiteral("%1 MB")
                                .arg(qMax<qint64>(
                                    1, qRound64(info.size() / (1024.0 * 1024.0))));
    m_project.source.sample = false;
    m_project.hasSource = true;
    rebuildSetup();
    emit toastRequested(QStringLiteral("已导入口播视频，AI 正在识别画面与口播内容"));
}

void OralPage::openMaterialFiles()
{
    const QStringList paths = QFileDialog::getOpenFileNames(
        this, QStringLiteral("添加相关素材"), QString(),
        QStringLiteral("图片与视频 (*.png *.jpg *.jpeg *.webp *.mp4 *.mov *.mkv)"));
    if (!paths.isEmpty()) {
        addMaterials(paths);
    }
}

void OralPage::addMaterials(const QStringList &paths)
{
    int index = m_project.materials.size();
    QStringList addedNames;
    for (const QString &path : paths) {
        if (m_project.materials.size() >= 24) {
            break;
        }
        const QFileInfo info(path);
        const bool video =
            QStringList({QStringLiteral("mp4"), QStringLiteral("mov"),
                         QStringLiteral("mkv"), QStringLiteral("avi"),
                         QStringLiteral("webm")})
                .contains(info.suffix().toLower());
        m_project.materials.append(
            StudioData::oralMaterialFromFile(info.fileName(), info.size(), video,
                                             index));
        ++index;
        addedNames.append(info.fileName());
        if (m_project.materials.size() >= 24) {
            break;
        }
    }
    rebuildSetup();
    emit toastRequested(
        QStringLiteral("已添加 %1 个素材，AI 正在建立匹配").arg(addedNames.size()));
}

void OralPage::removeMaterial(const QString &id)
{
    for (int index = 0; index < m_project.materials.size(); ++index) {
        if (m_project.materials.at(index).id == id) {
            m_project.materials.removeAt(index);
            break;
        }
    }
    rebuildSetup();
}

void OralPage::setSourceSample()
{
    m_project.source = StudioData::demoOralSource();
    m_project.hasSource = true;
    rebuildSetup();
    emit toastRequested(QStringLiteral("已载入示例口播，可直接体验"));
}

void OralPage::clearSource()
{
    m_project.hasSource = false;
    m_project.source = StudioData::OralSource();
    m_project.materials.clear();
    rebuildSetup();
}

void OralPage::chooseTemplate(const QString &templateId)
{
    m_project.templateId = templateId;
    if (m_screen == Screen::Hub) {
        rebuildHub();
    } else {
        rebuildSetup();
    }
    emit toastRequested(QStringLiteral("已选择「%1」模板，开始包装时自动套用")
                            .arg(StudioData::oralTemplateName(templateId)));
}

void OralPage::applyRatio(const QString &ratio)
{
    if (m_project.ratio == ratio) {
        return;
    }
    m_project.ratio = ratio;
    rebuildSetup();
}

void OralPage::applyOutput(bool both)
{
    if (m_project.outputBoth == both) {
        return;
    }
    m_project.outputBoth = both;
    rebuildSetup();
}

/* ------------------------------------------------------------------ */
/* 生成过程                                                             */
/* ------------------------------------------------------------------ */

void OralPage::startGeneration()
{
    if (!m_project.hasSource || m_project.materials.isEmpty()) {
        return;
    }

    if (m_playTimer) {
        m_playTimer->stop();
    }
    if (m_draftTimer) {
        m_draftTimer->stop();
    }
    m_playing = false;
    m_playProgress = 0;

    buildProcessingScreen();
    m_runId += 1;
    m_runStartedAt = QDateTime::currentMSecsSinceEpoch();
    m_run = StudioData::computeOralRun(0, m_runId);
    m_hasRun = true;
    m_logCount = -1;
    m_lastKeywordCount = -1;
    m_lastMatchedCount = -1;
    updateProcessing();
    showProcessing();
    m_runTimer->start();
    emit taskStatusChanged(
        QStringLiteral("口播包装中 0% · 00:00"), true);
}

void OralPage::buildProcessingScreen()
{
    auto *root = qobject_cast<QVBoxLayout *>(m_processingScreen->layout());
    if (!root) {
        root = new QVBoxLayout(m_processingScreen);
        root->setContentsMargins(0, 0, 0, 0);
        root->setSpacing(0);
    }
    Studio::clearLayout(root);
    m_stageCards.clear();
    m_processingPoster = nullptr;
    m_keywordStatus = nullptr;
    m_keywordLayout = nullptr;
    m_matchedLayout = nullptr;
    m_matchedCount = nullptr;
    m_logCount = -1;

    auto *top = new QWidget(m_processingScreen);
    auto *topLayout = new QVBoxLayout(top);
    topLayout->setContentsMargins(22, 14, 22, 12);
    topLayout->setSpacing(0);

    auto *titleRow = new QWidget(top);
    auto *titleLayout = new QHBoxLayout(titleRow);
    titleLayout->setContentsMargins(0, 0, 0, 0);
    titleLayout->setSpacing(8);
    auto *titleTexts = new QVBoxLayout;
    titleTexts->setContentsMargins(0, 0, 0, 0);
    titleTexts->setSpacing(2);
    auto *titleLine = new QHBoxLayout;
    titleLine->setContentsMargins(0, 0, 0, 0);
    titleLine->setSpacing(8);
    titleLine->addWidget(Ui::label(QStringLiteral("AI 正在包装你的口播"), 16,
                                     QFont::DemiBold, Theme::Text1, top));
    titleLine->addWidget(new Chip(QStringLiteral("智能包装中"), Chip::Run, top));
    auto *ratioLabel = monoLabel(
        QStringLiteral("%1 · %2")
            .arg(m_project.ratio, m_project.outputBoth
                                      ? QStringLiteral("成片 + 剪映草稿")
                                      : QStringLiteral("成片 MP4")),
        10, Theme::Text3, top);
    titleLine->addWidget(ratioLabel);
    titleLine->addStretch(1);
    titleTexts->addLayout(titleLine);
    auto *metaLine = new QHBoxLayout;
    metaLine->setContentsMargins(0, 0, 0, 0);
    metaLine->setSpacing(8);
    auto *projectLabel = Ui::label(
        QStringLiteral("通勤防晒 · 口播包装"), 10, QFont::Normal, Theme::Text2, top);
    metaLine->addWidget(projectLabel);
    metaLine->addWidget(Ui::label(QStringLiteral("·"), 10, QFont::Normal,
                                    Theme::BorderStrong, top));
    metaLine->addWidget(Ui::label(
        QStringLiteral("模板：%1").arg(StudioData::oralTemplateName(m_project.templateId)),
        10, QFont::Normal, Theme::Text3, top));
    metaLine->addWidget(Ui::label(QStringLiteral("·"), 10, QFont::Normal,
                                    Theme::BorderStrong, top));
    m_processingMeta = Ui::label(QStringLiteral("已用时 00:00"), 10, QFont::Normal,
                                 Theme::Text3, top);
    metaLine->addWidget(m_processingMeta);
    metaLine->addWidget(Ui::label(QStringLiteral("·"), 10, QFont::Normal,
                                    Theme::BorderStrong, top));
    m_etaLabel = Ui::label(QStringLiteral("预计还需 24 秒"), 10, QFont::Normal,
                           Theme::Text3, top);
    metaLine->addWidget(m_etaLabel);
    metaLine->addStretch(1);
    titleTexts->addLayout(metaLine);
    titleLayout->addLayout(titleTexts, 1);

    auto *progressHost = new QWidget(top);
    auto *progressLayout = new QHBoxLayout(progressHost);
    progressLayout->setContentsMargins(0, 0, 0, 0);
    progressLayout->setSpacing(12);
    auto *percentBox = new QVBoxLayout;
    percentBox->setContentsMargins(0, 0, 0, 0);
    percentBox->setSpacing(2);
    m_progressLabel = Ui::richLabel(QString(), 23, Theme::Primary, progressHost);
    m_progressLabel->setAlignment(Qt::AlignRight);
    percentBox->addWidget(m_progressLabel);
    auto *percentLabel = Ui::label(QStringLiteral("总进度"), 9, QFont::Normal,
                                    Theme::Text3, progressHost);
    percentLabel->setAlignment(Qt::AlignRight);
    percentBox->addWidget(percentLabel);
    progressLayout->addLayout(percentBox);
    auto *runInBackground = new Button(QStringLiteral("后台运行"), Button::Default,
                                       progressHost);
    connect(runInBackground, &QAbstractButton::clicked, this, [this] {
        emit toastRequested(QStringLiteral("已转后台运行，完成后自动下载并通知你"));
        emit navigateRequested(QStringLiteral("dash"));
    });
    progressLayout->addWidget(runInBackground, 0, Qt::AlignVCenter);
    titleLayout->addWidget(progressHost, 0, Qt::AlignTop);
    topLayout->addWidget(titleRow);
    topLayout->addSpacing(12);

    auto *stages = new QWidget(top);
    auto *stagesLayout = new QHBoxLayout(stages);
    stagesLayout->setContentsMargins(0, 0, 0, 0);
    stagesLayout->setSpacing(8);
    for (int index = 0; index < 4; ++index) {
        auto *card = new Studio::StageSummaryCard(stages);
        m_stageCards.append(card);
        stagesLayout->addWidget(card, 1);
    }
    topLayout->addWidget(stages);
    root->addWidget(top);
    root->addWidget(Ui::separator(false, m_processingScreen));

    auto *body = new QWidget(m_processingScreen);
    auto *bodyLayout = new QHBoxLayout(body);
    bodyLayout->setContentsMargins(22, 12, 22, 12);
    bodyLayout->setSpacing(14);

    auto *left = new QWidget(body);
    left->setFixedWidth(360);
    auto *leftLayout = new QVBoxLayout(left);
    leftLayout->setContentsMargins(0, 0, 0, 0);
    leftLayout->setSpacing(8);

    auto *stageHost = new DarkStage(left);
    auto *stageLayout = new QHBoxLayout(stageHost);
    stageLayout->setContentsMargins(12, 12, 12, 12);
    stageLayout->setAlignment(Qt::AlignCenter);
    const int previewHeight = m_project.ratio == QLatin1String("9:16") ? 388 : 212;
    const int previewWidth = m_project.ratio == QLatin1String("9:16")
        ? qRound(previewHeight * 9.0 / 16.0)
        : qRound(previewHeight * 16.0 / 9.0);
    m_processingPoster = new Studio::OralPosterWidget(stageHost);
    m_processingPoster->setFixedSize(previewWidth, previewHeight);
    Studio::OralPosterSpec spec;
    spec.templateId = m_project.templateId;
    spec.headline = StudioData::oralTemplate(m_project.templateId).headline;
    spec.caption = QStringLiteral("正在理解口播内容…");
    spec.ratio = m_project.ratio;
    spec.timecode = QStringLiteral("00:00");
    spec.progress = 0;
    spec.subtitleStyle = m_project.subtitleStyle;
    m_processingPoster->setSpec(spec);
    stageLayout->addWidget(m_processingPoster, 0, Qt::AlignCenter);
    stageHost->setMinimumHeight(previewHeight + 24);
    leftLayout->addWidget(stageHost, 1);

    auto *infoRow = new QWidget(left);
    auto *infoLayout = new QHBoxLayout(infoRow);
    infoLayout->setContentsMargins(0, 0, 0, 0);
    infoLayout->setSpacing(8);
    auto *keywordContent = new QWidget;
    m_keywordLayout = new QHBoxLayout(keywordContent);
    m_keywordLayout->setContentsMargins(0, 0, 0, 0);
    m_keywordLayout->setSpacing(4);
    m_keywordStatus = Ui::label(QStringLiteral("正在读取口播内容…"), 8, QFont::Normal,
                                 Theme::Text3, keywordContent);
    m_keywordLayout->addWidget(m_keywordStatus);
    m_keywordLayout->addStretch(1);
    infoLayout->addWidget(makeInfoPanel(QStringLiteral("AI 理解"),
                                        Icons::Glyph::Sparkle, keywordContent,
                                        infoRow),
                          1);

    auto *matchedContent = new QWidget;
    auto *matchedBox = new QVBoxLayout(matchedContent);
    matchedBox->setContentsMargins(0, 0, 0, 0);
    matchedBox->setSpacing(3);
    auto *matchedTop = new QHBoxLayout;
    matchedTop->setContentsMargins(0, 0, 0, 0);
    matchedTop->addStretch(1);
    m_matchedCount = monoLabel(QStringLiteral("0/8"), 8, Theme::Text2, matchedContent);
    matchedTop->addWidget(m_matchedCount);
    matchedBox->addLayout(matchedTop);
    auto *matchedThumbsHost = new QWidget(matchedContent);
    m_matchedLayout = new QHBoxLayout(matchedThumbsHost);
    m_matchedLayout->setContentsMargins(0, 0, 0, 0);
    m_matchedLayout->setSpacing(4);
    auto *waiting = Ui::label(QStringLiteral("等待口播理解完成"), 8, QFont::Normal,
                              Theme::Text3, matchedThumbsHost);
    m_matchedLayout->addWidget(waiting);
    m_matchedLayout->addStretch(1);
    matchedBox->addWidget(matchedThumbsHost);
    infoLayout->addWidget(makeInfoPanel(QStringLiteral("素材匹配"),
                                        Icons::Glyph::Image, matchedContent,
                                        infoRow),
                          1);
    leftLayout->addWidget(infoRow);

    auto *sourceRow = new QWidget(left);
    sourceRow->setObjectName(QStringLiteral("oralProcessingSource"));
    sourceRow->setStyleSheet(
        QStringLiteral("#oralProcessingSource { background:%1; border:1px solid %2;"
                       " border-radius:7px; }")
            .arg(Theme::Surface1.name(), Theme::Border.name()));
    auto *sourceLayout = new QHBoxLayout(sourceRow);
    sourceLayout->setContentsMargins(10, 7, 10, 7);
    sourceLayout->setSpacing(8);
    auto *wave = new Studio::Waveform(sourceRow);
    wave->setBars(18);
    wave->setColor(StudioData::oralTemplate(m_project.templateId).accent);
    wave->setAnimated(true);
    wave->setFixedWidth(64);
    sourceLayout->addWidget(wave, 0, Qt::AlignVCenter);
    auto *sourceLabel = new ElidedLabel(
        QStringLiteral("%1 · %2")
            .arg(m_project.source.name, m_project.source.duration),
        sourceRow);
    sourceLabel->setPixelSize(9);
    sourceLabel->setTextColor(Theme::Text3);
    sourceLayout->addWidget(sourceLabel, 1);
    m_processingSourceMeta = monoLabel(QString(), 8, Theme::Text3, sourceRow);
    sourceLayout->addWidget(m_processingSourceMeta, 0);
    leftLayout->addWidget(sourceRow);
    bodyLayout->addWidget(left);

    auto *logPanel = new QWidget(body);
    logPanel->setObjectName(QStringLiteral("oralLogPanel"));
    logPanel->setStyleSheet(
        QStringLiteral("#oralLogPanel { background:%1; border:1px solid %2;"
                       " border-radius:10px; }")
            .arg(Theme::Surface1.name(), Theme::Border.name()));
    auto *logLayout = new QVBoxLayout(logPanel);
    logLayout->setContentsMargins(0, 0, 0, 0);
    logLayout->setSpacing(0);

    auto *logHeader = new QWidget(logPanel);
    auto *logHeaderLayout = new QHBoxLayout(logHeader);
    logHeaderLayout->setContentsMargins(14, 10, 14, 10);
    logHeaderLayout->setSpacing(8);
    logHeaderLayout->addWidget(makeIconCircle(Icons::Glyph::Sparkle, 20,
                                               Theme::Primary,
                                               Theme::alpha(Theme::Primary, 0.14),
                                               Theme::alpha(Theme::Primary, 0.35),
                                               logHeader),
                               0, Qt::AlignVCenter);
    auto *logTitles = new QVBoxLayout;
    logTitles->setContentsMargins(0, 0, 0, 0);
    logTitles->setSpacing(1);
    m_logStageTitle = Ui::label(QStringLiteral("实时过程 · 理解口播"), 11,
                                QFont::Medium, Theme::Text1, logHeader);
    m_logStageDetail = new ElidedLabel(QStringLiteral("正在准备…"), logHeader);
    m_logStageDetail->setPixelSize(9);
    m_logStageDetail->setTextColor(Theme::Text3);
    logTitles->addWidget(m_logStageTitle);
    logTitles->addWidget(m_logStageDetail);
    logHeaderLayout->addLayout(logTitles, 1);
    auto *logCounts = new QHBoxLayout;
    logCounts->setContentsMargins(0, 0, 0, 0);
    logCounts->setSpacing(10);
    m_logElapsedLabel = monoLabel(QStringLiteral("00:00"), 9, Theme::Text3,
                                  logHeader);
    logCounts->addWidget(m_logElapsedLabel);
    m_logStepsLabel = monoLabel(QStringLiteral("0/4 步"), 9, Theme::Text3,
                                logHeader);
    logCounts->addWidget(m_logStepsLabel);
    logHeaderLayout->addLayout(logCounts);
    logLayout->addWidget(logHeader);
    logLayout->addWidget(Ui::separator(false, logPanel));

    auto *logProgressHost = new QWidget(logPanel);
    auto *logProgressLayout = new QVBoxLayout(logProgressHost);
    logProgressLayout->setContentsMargins(14, 8, 14, 8);
    logProgressLayout->setSpacing(4);
    auto *logProgressTop = new QHBoxLayout;
    logProgressTop->setContentsMargins(0, 0, 0, 0);
    logProgressTop->addWidget(Ui::label(QStringLiteral("理解口播"), 10, QFont::Normal,
                                         Theme::Text2, logProgressHost));
    logProgressTop->addStretch(1);
    m_logStageProgress = monoLabel(QStringLiteral("0%"), 9, Theme::Primary,
                                   logProgressHost);
    logProgressTop->addWidget(m_logStageProgress);
    logProgressLayout->addLayout(logProgressTop);
    m_logStageBar = new ProgressBar(logProgressHost);
    m_logStageBar->setBarColor(Theme::Primary);
    m_logStageBar->setValue(0);
    m_logStageBar->setFixedHeight(4);
    logProgressLayout->addWidget(m_logStageBar);
    logLayout->addWidget(logProgressHost);
    logLayout->addWidget(Ui::separator(false, logPanel));

    m_logContent = new QWidget;
    m_logLayout = new QVBoxLayout(m_logContent);
    m_logLayout->setContentsMargins(14, 8, 14, 8);
    m_logLayout->setSpacing(2);
    m_logLayout->setAlignment(Qt::AlignTop);
    m_logScroll = makeScrollArea(m_logContent, logPanel);
    logLayout->addWidget(m_logScroll, 1);
    logLayout->addWidget(Ui::separator(false, logPanel));

    auto *logFooter = new QWidget(logPanel);
    auto *logFooterLayout = new QHBoxLayout(logFooter);
    logFooterLayout->setContentsMargins(14, 5, 14, 5);
    logFooterLayout->setSpacing(8);
    auto *statusRow = new QHBoxLayout;
    statusRow->setContentsMargins(0, 0, 0, 0);
    statusRow->setSpacing(5);
    statusRow->addWidget(Studio::iconLabel(Icons::Glyph::Clock, 9, Theme::Text3,
                                            logFooter));
    auto *statusLabel = Ui::label(QStringLiteral("可切到其他页面，完成后自动下载"), 8,
                                  QFont::Normal, Theme::Text3, logFooter);
    statusRow->addWidget(statusLabel);
    logFooterLayout->addLayout(statusRow);
    logFooterLayout->addStretch(1);
    m_logFooter = monoLabel(QStringLiteral("字幕 0/24"), 8, Theme::Text3, logFooter);
    logFooterLayout->addWidget(m_logFooter);
    logLayout->addWidget(logFooter);
    bodyLayout->addWidget(logPanel, 1);

    root->addWidget(body, 1);
}

void OralPage::updateProcessing()
{
    if (!m_hasRun) {
        return;
    }
    const int percent = m_run.done ? 100
        : qMin(99, static_cast<int>(std::floor(m_run.progress)));
    if (m_progressLabel) {
        m_progressLabel->setText(
            QStringLiteral("<span style=\"font-size:23px;font-weight:650;color:%1;\">%2"
                           "</span><span style=\"font-size:10px;font-weight:400;"
                           "color:%3;\">%</span>")
                .arg(Theme::Primary.name())
                .arg(percent)
                .arg(Theme::Text3.name()));
    }
    if (m_processingMeta) {
        m_processingMeta->setText(
            QStringLiteral("已用时 %1").arg(StudioData::formatElapsed(m_run.elapsedMs)));
    }
    if (m_etaLabel) {
        m_etaLabel->setText(m_run.done
                                ? QStringLiteral("全部步骤已完成")
                                : QStringLiteral("预计还需 %1")
                                      .arg(StudioData::formatEta(m_run.etaSeconds)));
        m_etaLabel->setStyleSheet(
            QStringLiteral("color:%1;background:transparent;")
                .arg((m_run.done ? Theme::Success : Theme::Text3).name()));
    }

    const QVector<Icons::Glyph> glyphs = {
        Icons::Glyph::Mic, Icons::Glyph::Image, Icons::Glyph::Wand,
        Icons::Glyph::Film,
    };
    const QStringList statusTexts = {QStringLiteral("等待"), QStringLiteral("进行中"),
                                     QStringLiteral("完成")};
    for (int index = 0; index < m_stageCards.size() && index < m_run.stages.size();
         ++index) {
        const StudioData::OralStageState &stage = m_run.stages.at(index);
        const int status = stage.status == StudioData::StageStatus::Done
            ? 2
            : (stage.status == StudioData::StageStatus::Running ? 1 : 0);
        m_stageCards.at(index)->setData(
            index + 1, glyphs.value(index, Icons::Glyph::Sparkle), stage.label,
            status, statusTexts.value(status), stage.detail,
            qRound(stage.progress), stage.status == StudioData::StageStatus::Running);
    }

    if (m_processingPoster) {
        Studio::OralPosterSpec spec = m_processingPoster->spec();
        spec.caption = m_run.previewCaption;
        spec.keywords = m_run.previewKeywords;
        spec.materials = m_project.materials.mid(
            0, qMin(2, static_cast<int>(std::ceil(m_run.matchedMaterials / 4.0))));
        spec.timecode = StudioData::formatElapsed(m_run.elapsedMs);
        spec.progress = qRound(m_run.progress);
        m_processingPoster->setSpec(spec);
    }

    if (m_keywordLayout && m_run.previewKeywords.size() != m_lastKeywordCount) {
        m_lastKeywordCount = m_run.previewKeywords.size();
        Studio::clearLayout(m_keywordLayout);
        if (m_run.previewKeywords.isEmpty()) {
            m_keywordStatus = Ui::label(QStringLiteral("正在读取口播内容…"), 8,
                                        QFont::Normal, Theme::Text3,
                                        m_keywordLayout->parentWidget());
            m_keywordLayout->addWidget(m_keywordStatus);
        } else {
            for (const QString &keyword : m_run.previewKeywords) {
                if (m_keywordLayout->count() >= 4) {
                    break;
                }
                m_keywordLayout->addWidget(Studio::tagLabel(
                    keyword, Theme::Ai, Theme::alpha(Theme::Ai, 0.13), 8,
                    m_keywordLayout->parentWidget()));
            }
        }
        m_keywordLayout->addStretch(1);
    }

    if (m_matchedCount) {
        m_matchedCount->setText(
            QStringLiteral("%1/8").arg(m_run.matchedMaterials));
    }
    const int thumbCount = qMin(2, static_cast<int>(
                                        std::ceil(m_run.matchedMaterials / 4.0)));
    if (m_matchedLayout && thumbCount != m_lastMatchedCount) {
        m_lastMatchedCount = thumbCount;
        Studio::clearLayout(m_matchedLayout);
        if (thumbCount == 0) {
            m_matchedLayout->addWidget(
                Ui::label(QStringLiteral("等待口播理解完成"), 8, QFont::Normal,
                          Theme::Text3, m_matchedLayout->parentWidget()));
        } else {
            for (const StudioData::OralMaterial &material :
                 m_project.materials.mid(0, thumbCount)) {
                auto *thumb = new Studio::MaterialVisualWidget(
                    m_matchedLayout->parentWidget());
                thumb->setMaterial(material);
                thumb->setFixedSize(28, 20);
                m_matchedLayout->addWidget(thumb);
            }
            auto *eq = new QWidget(m_matchedLayout->parentWidget());
            auto *eqLayout = new QHBoxLayout(eq);
            eqLayout->setContentsMargins(0, 0, 0, 0);
            eqLayout->setSpacing(2);
            for (int index = 0; index < 5; ++index) {
                auto *bar = new QWidget(eq);
                bar->setFixedSize(2, 5 + (index * 3) % 8);
                bar->setStyleSheet(QStringLiteral("background:%1;border-radius:1px;")
                                       .arg(Theme::alpha(Theme::Ai, 0.7).name()));
                eqLayout->addWidget(bar, 0, Qt::AlignBottom);
            }
            m_matchedLayout->addStretch(1);
            m_matchedLayout->addWidget(eq, 0, Qt::AlignVCenter);
        }
    }

    if (m_processingSourceMeta) {
        m_processingSourceMeta->setText(
            m_run.stage == StudioData::OralStage::Render
                ? QStringLiteral("%1/%2 帧")
                      .arg(StudioData::formatNumber(m_run.renderedFrames),
                           StudioData::formatNumber(m_run.totalFrames))
                : QStringLiteral("%1 · %2")
                      .arg(StudioData::oralTemplateName(m_project.templateId),
                           m_project.ratio));
    }

    const StudioData::OralStageState *current = nullptr;
    for (const StudioData::OralStageState &stage : m_run.stages) {
        if (stage.key == m_run.stage) {
            current = &stage;
            break;
        }
    }
    if (!current && !m_run.stages.isEmpty()) {
        current = &m_run.stages.first();
    }
    if (current) {
        if (m_logStageTitle) {
            m_logStageTitle->setText(QStringLiteral("实时过程 · %1").arg(current->label));
        }
        if (m_logStageDetail) {
            m_logStageDetail->setText(current->detail);
        }
        if (m_logStageProgress) {
            m_logStageProgress->setText(
                QStringLiteral("%1%").arg(qFloor(current->progress)));
        }
        if (m_logStageBar) {
            m_logStageBar->setValue(qRound(current->progress));
        }
    }
    if (m_logElapsedLabel) {
        m_logElapsedLabel->setText(StudioData::formatElapsed(m_run.elapsedMs));
    }
    if (m_logStepsLabel) {
        int doneCount = 0;
        for (const StudioData::OralStageState &stage : m_run.stages) {
            if (stage.status == StudioData::StageStatus::Done) {
                ++doneCount;
            }
        }
        m_logStepsLabel->setText(
            QStringLiteral("%1/%2 步").arg(doneCount).arg(m_run.stages.size()));
    }
    if (m_logFooter) {
        m_logFooter->setText(
            QStringLiteral("字幕 %1/24").arg(m_run.subtitleCount));
    }

    if (m_logLayout && m_run.logs.size() != m_logCount) {
        m_logCount = m_run.logs.size();
        Studio::clearLayout(m_logLayout);
        if (m_run.logs.isEmpty()) {
            auto *row = new QWidget;
            auto *rowLayout = new QHBoxLayout(row);
            rowLayout->setContentsMargins(0, 6, 0, 6);
            rowLayout->setSpacing(6);
            rowLayout->addWidget(Studio::iconLabel(Icons::Glyph::Sparkle, 9,
                                                    Theme::Ai, row));
            rowLayout->addWidget(Ui::label(QStringLiteral("正在连接 AI 服务，准备开始…"),
                                            10, QFont::Normal, Theme::Text3, row));
            rowLayout->addStretch(1);
            m_logLayout->addWidget(row);
        } else {
            QString lastStage;
            for (const StudioData::OralLogEntry &log : m_run.logs) {
                const QString stageLabel = [&] {
                    for (const StudioData::OralStageState &stage : m_run.stages) {
                        if (stage.key == log.stage) {
                            return stage.label;
                        }
                    }
                    return QString();
                }();
                if (stageLabel != lastStage) {
                    lastStage = stageLabel;
                    auto *sep = new QWidget;
                    auto *sepLayout = new QHBoxLayout(sep);
                    sepLayout->setContentsMargins(0, 6, 0, 2);
                    sepLayout->setSpacing(6);
                    auto *line1 = new QWidget(sep);
                    line1->setFixedHeight(1);
                    line1->setStyleSheet(
                        QStringLiteral("background:%1;").arg(Theme::Border.name()));
                    line1->setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
                    sepLayout->addWidget(line1, 1);
                    sepLayout->addWidget(Ui::label(stageLabel, 8, QFont::Normal,
                                                     Theme::Text3, sep));
                    auto *line2 = new QWidget(sep);
                    line2->setFixedHeight(1);
                    line2->setStyleSheet(
                        QStringLiteral("background:%1;").arg(Theme::Border.name()));
                    line2->setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
                    sepLayout->addWidget(line2, 1);
                    m_logLayout->addWidget(sep);
                }
                auto *row = new QWidget;
                auto *rowLayout = new QHBoxLayout(row);
                rowLayout->setContentsMargins(6, 2, 6, 2);
                rowLayout->setSpacing(7);
                auto *dot = new QWidget(row);
                dot->setFixedSize(4, 4);
                QColor dotColor = Theme::BorderStrong;
                QColor textColor = Theme::Text3;
                if (log.tone == StudioData::Tone::Ai) {
                    dotColor = Theme::Ai;
                    textColor = Theme::Ai;
                } else if (log.tone == StudioData::Tone::Ok) {
                    dotColor = Theme::Success;
                    textColor = Theme::Success;
                } else if (log.tone == StudioData::Tone::Info) {
                    dotColor = Theme::Primary;
                    textColor = Theme::Text2;
                }
                dot->setStyleSheet(QStringLiteral("background:%1;border-radius:2px;")
                                       .arg(dotColor.name()));
                rowLayout->addWidget(dot, 0, Qt::AlignTop);
                auto *text = Ui::label(log.text, 9, QFont::Normal, textColor, row);
                text->setWordWrap(true);
                rowLayout->addWidget(text, 1);
                rowLayout->addWidget(monoLabel(StudioData::formatElapsed(log.atMs), 8,
                                                Theme::Text3, row),
                                     0, Qt::AlignTop);
                m_logLayout->addWidget(row);
            }
        }
        m_logLayout->addStretch(1);
        QTimer::singleShot(0, this, [this] {
            if (m_logScroll && m_logScroll->verticalScrollBar()) {
                m_logScroll->verticalScrollBar()->setValue(
                    m_logScroll->verticalScrollBar()->maximum());
            }
        });
    }
}

void OralPage::finishGeneration()
{
    if (!m_hasRun) {
        return;
    }
    m_runTimer->stop();
    m_run = StudioData::computeOralRun(StudioData::oralRunTotalMs(), m_runId);
    m_run.done = true;
    updateProcessing();
    m_hasRun = false;
    m_result = StudioData::makeOralResult(
        m_project, QDateTime::currentMSecsSinceEpoch(),
        StudioData::oralTemplateName(m_project.templateId));
    m_hasResult = true;
    showResult();
    emit taskStatusChanged(QString(), false);
    if (!isVisible()) {
        emit toastRequested(QStringLiteral("智能口播成片已生成并自动下载，可预览查看"));
    }
}

/* ------------------------------------------------------------------ */
/* 结果                                                                 */
/* ------------------------------------------------------------------ */

void OralPage::rebuildResult()
{
    auto *root = qobject_cast<QVBoxLayout *>(m_resultScreen->layout());
    if (!root) {
        root = new QVBoxLayout(m_resultScreen);
        root->setContentsMargins(0, 0, 0, 0);
        root->setSpacing(0);
    }
    Studio::clearLayout(root);

    m_resultPoster = nullptr;
    m_resultGenerationLabel = nullptr;
    m_resultPlayClock = nullptr;
    m_resultPlayButton = nullptr;
    m_resultPlayBar = nullptr;
    m_resultDraftButton = nullptr;
    m_resultDraftHint = nullptr;
    m_resultCenterPlay = nullptr;

    if (!m_hasResult) {
        auto *empty = new QWidget(m_resultScreen);
        auto *emptyLayout = new QVBoxLayout(empty);
        emptyLayout->setContentsMargins(40, 40, 40, 40);
        emptyLayout->setSpacing(10);
        emptyLayout->setAlignment(Qt::AlignCenter);
        auto *icon = new QWidget(empty);
        Q_UNUSED(icon);
        emptyLayout->addWidget(Studio::iconLabel(Icons::Glyph::Sparkle, 20,
                                                  Theme::Ai, empty),
                               0, Qt::AlignHCenter);
        emptyLayout->addWidget(Ui::label(QStringLiteral("还没有可查看的口播成片"), 15,
                                          QFont::Medium, Theme::Text1, empty),
                               0, Qt::AlignHCenter);
        auto *hint = Ui::label(
            QStringLiteral("从智能口播首页开始一个项目，成片会自动下载，并在这里预览、复制发布文案或生成剪映草稿。"),
            11, QFont::Normal, Theme::Text3, empty);
        hint->setWordWrap(true);
        hint->setAlignment(Qt::AlignCenter);
        hint->setMaximumWidth(380);
        emptyLayout->addWidget(hint, 0, Qt::AlignHCenter);
        auto *back = new Button(QStringLiteral("返回智能口播"), Button::Primary, empty);
        connect(back, &QAbstractButton::clicked, this, &OralPage::showHub);
        emptyLayout->addWidget(back, 0, Qt::AlignHCenter);
        root->addWidget(empty, 1);
        return;
    }

    m_playing = false;
    m_playProgress = 0;
    m_draftState = m_result.draftGenerated ? 2 : 0;
    m_draftProgress = 100 * (m_draftState == 2 ? 1 : 0);
    if (m_playTimer) {
        m_playTimer->stop();
    }
    if (m_draftTimer) {
        m_draftTimer->stop();
    }

    const StudioData::OralTemplate tmpl =
        StudioData::oralTemplate(m_result.templateId);
    QString fileName = m_result.projectName;
    fileName.replace(QStringLiteral(" · "), QStringLiteral("_"));
    const QString movies = QStandardPaths::writableLocation(
        QStandardPaths::MoviesLocation);
    m_resultFilePath = QDir(movies).filePath(
        QStringLiteral("LiveMix/口播成片/%1.mp4").arg(fileName));
    m_resultDraftPath = QDir(movies).filePath(
        QStringLiteral("LiveMix/CapDraft/%1_%2").arg(fileName, m_result.id));

    auto *header = new QWidget(m_resultScreen);
    auto *headerLayout = new QVBoxLayout(header);
    headerLayout->setContentsMargins(22, 10, 22, 10);
    headerLayout->setSpacing(0);

    auto *breadcrumb = new QWidget(header);
    auto *breadcrumbLayout = new QHBoxLayout(breadcrumb);
    breadcrumbLayout->setContentsMargins(0, 0, 0, 0);
    breadcrumbLayout->setSpacing(6);
    auto *back = new LinkLabel(QStringLiteral("智能口播"), breadcrumb);
    back->setPixelSize(10);
    back->setTextColor(Theme::Text3);
    back->setHoverColor(Theme::Text1);
    back->setLeadingGlyph(Icons::Glyph::ChevronLeft, 10);
    back->setUnderlineOnHover(false);
    connect(back, &QAbstractButton::clicked, this, &OralPage::showHub);
    breadcrumbLayout->addWidget(back);
    breadcrumbLayout->addWidget(Ui::label(QStringLiteral("/"), 10, QFont::Normal,
                                           Theme::BorderStrong, breadcrumb));
    breadcrumbLayout->addWidget(Ui::label(QStringLiteral("成片结果"), 10,
                                           QFont::Normal, Theme::Text2, breadcrumb));
    breadcrumbLayout->addSpacing(4);
    breadcrumbLayout->addWidget(new StatusDot(6, Theme::Success, breadcrumb));
    breadcrumbLayout->addWidget(Ui::label(QStringLiteral("已自动保存"), 9,
                                           QFont::Normal, Theme::Success, breadcrumb));
    breadcrumbLayout->addStretch(1);
    headerLayout->addWidget(breadcrumb);
    headerLayout->addSpacing(4);

    auto *titleRow = new QWidget(header);
    auto *titleLayout = new QHBoxLayout(titleRow);
    titleLayout->setContentsMargins(0, 0, 0, 0);
    titleLayout->setSpacing(10);
    auto *titles = new QVBoxLayout;
    titles->setContentsMargins(0, 0, 0, 0);
    titles->setSpacing(2);
    auto *titleLine = new QHBoxLayout;
    titleLine->setContentsMargins(0, 0, 0, 0);
    titleLine->setSpacing(8);
    auto *projectName = new ElidedLabel(m_result.projectName, header);
    projectName->setSizePolicy(QSizePolicy::Preferred, QSizePolicy::Fixed);
    projectName->setPixelSize(17);
    projectName->setFontWeight(QFont::DemiBold);
    projectName->setTextColor(Theme::Text1);
    titleLine->addWidget(projectName, 0);
    titleLine->addWidget(new Chip(QStringLiteral("已生成并自动下载"), Chip::Ok,
                                   header));
    titleLine->addWidget(makeGenerationPill(&m_resultGenerationLabel, header));
    titleLine->addStretch(1);
    titles->addLayout(titleLine);
    auto *metaLine = new QHBoxLayout;
    metaLine->setContentsMargins(0, 0, 0, 0);
    metaLine->setSpacing(8);
    metaLine->addWidget(Ui::label(m_result.sourceLabel, 10, QFont::Normal,
                                    Theme::Text3, header));
    metaLine->addWidget(Ui::label(QStringLiteral("·"), 10, QFont::Normal,
                                    Theme::BorderStrong, header));
    metaLine->addWidget(Ui::label(QStringLiteral("模板：%1").arg(tmpl.name), 10,
                                    QFont::Normal, Theme::Text3, header));
    metaLine->addWidget(Ui::label(QStringLiteral("·"), 10, QFont::Normal,
                                    Theme::BorderStrong, header));
    metaLine->addWidget(Ui::label(
        QStringLiteral("%1 · %2").arg(m_result.ratio,
                                       StudioData::oralRatioDimensions(m_result.ratio)),
        10, QFont::Normal, Theme::Text3, header));
    metaLine->addWidget(Ui::label(QStringLiteral("·"), 10, QFont::Normal,
                                    Theme::BorderStrong, header));
    metaLine->addWidget(monoLabel(m_result.durationLabel, 10, Theme::Text3, header));
    metaLine->addStretch(1);
    titles->addLayout(metaLine);
    titleLayout->addLayout(titles, 1);

    auto *restart = new Button(QStringLiteral("重新包装"), Button::Default, header);
    restart->setLeadingGlyph(Icons::Glyph::Reset);
    connect(restart, &QAbstractButton::clicked, this, &OralPage::restartFromResult);
    titleLayout->addWidget(restart);
    m_resultPlayButton = new Button(QStringLiteral("播放成片"), Button::Primary,
                                    header);
    m_resultPlayButton->setLeadingGlyph(Icons::Glyph::Play);
    connect(m_resultPlayButton, &QAbstractButton::clicked, this,
            &OralPage::togglePlayback);
    titleLayout->addWidget(m_resultPlayButton);
    headerLayout->addWidget(titleRow);
    root->addWidget(header);
    root->addWidget(Ui::separator(false, m_resultScreen));

    auto *scrollContent = new QWidget;
    auto *scrollLayout = new QVBoxLayout(scrollContent);
    scrollLayout->setContentsMargins(22, 14, 22, 16);
    scrollLayout->setSpacing(0);
    auto *columns = new QWidget(scrollContent);
    auto *columnsLayout = new QHBoxLayout(columns);
    columnsLayout->setContentsMargins(0, 0, 0, 0);
    columnsLayout->setSpacing(16);

    auto *left = new QWidget(columns);
    auto *leftLayout = new QVBoxLayout(left);
    leftLayout->setContentsMargins(0, 0, 0, 0);
    leftLayout->setSpacing(12);

    auto *previewPanel = new QWidget(left);
    previewPanel->setObjectName(QStringLiteral("oralResultPreview"));
    previewPanel->setStyleSheet(
        QStringLiteral("#oralResultPreview { background:%1; border:1px solid %2;"
                       " border-radius:10px; }")
            .arg(QColor(QStringLiteral("#0c0e14")).name(), Theme::Border.name()));
    auto *previewLayout = new QVBoxLayout(previewPanel);
    previewLayout->setContentsMargins(16, 16, 16, 16);
    previewLayout->setSpacing(0);
    previewLayout->setAlignment(Qt::AlignHCenter);
    const bool portrait = m_result.ratio == QLatin1String("9:16");
    const int posterHeight = portrait ? 494 : 202;
    const int posterWidth = portrait ? qRound(posterHeight * 9.0 / 16.0)
                                     : qRound(posterHeight * 16.0 / 9.0);
    auto *poster = new Studio::OralPosterWidget(previewPanel);
    poster->setFixedSize(posterWidth, posterHeight);
    Studio::OralPosterSpec spec;
    spec.templateId = m_result.templateId;
    spec.ratio = m_result.ratio;
    spec.headline = m_result.projectName.split(QStringLiteral(" · ")).value(0);
    spec.caption = tmpl.layout == StudioData::OralLayout::Punch
        ? m_result.tags.value(0).mid(1)
        : StudioData::shortOralCaption(m_result.script);
    if (spec.caption.isEmpty()) {
        spec.caption = tmpl.caption;
    }
    spec.keywords = m_result.tags.mid(0, 2);
    for (QString &keyword : spec.keywords) {
        keyword = keyword.mid(1);
    }
    spec.materials = StudioData::demoMaterials().mid(0, 2);
    spec.timecode = m_result.durationLabel;
    spec.progress = 0;
    poster->setSpec(spec);
    m_resultPoster = poster;
    auto *posterHost = new QWidget(previewPanel);
    posterHost->setFixedSize(posterWidth, posterHeight);
    auto *posterHostLayout = new QGridLayout(posterHost);
    posterHostLayout->setContentsMargins(0, 0, 0, 0);
    posterHostLayout->addWidget(poster, 0, 0);
    m_resultCenterPlay = makeResultPlayOverlay(posterHost);
    connect(m_resultCenterPlay, &QToolButton::clicked, this,
            &OralPage::togglePlayback);
    posterHostLayout->addWidget(m_resultCenterPlay, 0, 0, Qt::AlignCenter);
    previewLayout->addWidget(posterHost, 0, Qt::AlignHCenter);
    previewLayout->addSpacing(12);

    auto *playRow = new QWidget(previewPanel);
    auto *playLayout = new QHBoxLayout(playRow);
    playLayout->setContentsMargins(0, 0, 0, 0);
    playLayout->setSpacing(8);
    auto *play = new GlyphButton(Icons::Glyph::Play, 28, playRow);
    play->setGlyphSize(11);
    play->setColors(Theme::Text1, Theme::Primary);
    play->setBackgroundColors(Theme::Surface2, Theme::Surface3);
    play->setToolTipText(QStringLiteral("播放或暂停"));
    connect(play, &QAbstractButton::clicked, this, &OralPage::togglePlayback);
    playLayout->addWidget(play);
    auto *playBarHost = new QWidget(playRow);
    auto *playBarLayout = new QVBoxLayout(playBarHost);
    playBarLayout->setContentsMargins(0, 0, 0, 0);
    playBarLayout->setSpacing(2);
    m_resultPlayBar = new ProgressBar(playBarHost);
    m_resultPlayBar->setBarColor(Theme::Primary);
    m_resultPlayBar->setValue(0);
    m_resultPlayBar->setFixedHeight(4);
    playBarLayout->addWidget(m_resultPlayBar);
    auto *clockRow = new QHBoxLayout;
    clockRow->setContentsMargins(0, 0, 0, 0);
    auto *timeLabel = monoLabel(QStringLiteral("00:00"), 8, Theme::Text3, playBarHost);
    m_resultPlayClock = timeLabel;
    clockRow->addWidget(timeLabel);
    clockRow->addStretch(1);
    clockRow->addWidget(monoLabel(m_result.durationLabel, 8, Theme::Text3,
                                  playBarHost));
    playBarLayout->addLayout(clockRow);
    playLayout->addWidget(playBarHost, 1);
    auto *ratioTag = Studio::tagLabel(m_result.ratio, QColor(255, 255, 255, 191),
                                      QColor(0, 0, 0, 128), 8, playRow);
    playLayout->addWidget(ratioTag, 0, Qt::AlignVCenter);
    previewLayout->addWidget(playRow);
    previewLayout->addSpacing(12);

    auto *statsGridHost = new QWidget(previewPanel);
    auto *statsGrid = new QGridLayout(statsGridHost);
    statsGrid->setContentsMargins(0, 0, 0, 0);
    statsGrid->setHorizontalSpacing(6);
    statsGrid->setVerticalSpacing(6);
    statsGrid->addWidget(new Studio::StatTile(QStringLiteral("文件大小"),
                                              m_result.fileSize, statsGridHost),
                         0, 0);
    statsGrid->addWidget(new Studio::StatTile(
                             QStringLiteral("成片画幅"),
                             StudioData::oralRatioDimensions(m_result.ratio),
                             statsGridHost),
                         0, 1);
    statsGrid->addWidget(new Studio::StatTile(QStringLiteral("包装模板"), tmpl.name,
                                              statsGridHost),
                         1, 0);
    statsGrid->addWidget(new Studio::StatTile(
                             QStringLiteral("B-Roll"),
                             QStringLiteral("%1 处").arg(m_result.brollCount),
                             statsGridHost),
                         1, 1);
    previewLayout->addWidget(statsGridHost);
    leftLayout->addWidget(previewPanel);

    auto *pathNote = new Studio::Note(
        QStringLiteral("成片已保存到本地输出目录：<br><span style=\"font-family:monospace;"
                       "color:%1;\">%2</span>")
            .arg(Theme::Text2.name(), Studio::formatClipPath(m_resultFilePath, 64)));
    leftLayout->addWidget(pathNote);
    leftLayout->addStretch(1);
    columnsLayout->addWidget(left, 84);

    auto *right = new QWidget(columns);
    auto *rightLayout = new QVBoxLayout(right);
    rightLayout->setContentsMargins(0, 0, 0, 0);
    rightLayout->setSpacing(12);

    auto *publish = new Studio::SectionCard(
        Icons::Glyph::Sparkle, QStringLiteral("发布文案"),
        QStringLiteral("AI 生成，可直接复制"), right);
    auto *copyAll = new LinkLabel(QStringLiteral("复制全部"), publish);
    copyAll->setPixelSize(9);
    copyAll->setTextColor(Theme::Primary);
    copyAll->setHoverColor(Theme::PrimaryHover);
    copyAll->setUnderlineOnHover(false);
    connect(copyAll, &QAbstractButton::clicked, this, [this] {
        copyText(QStringLiteral("%1\n%2\n%3")
                     .arg(m_result.title, m_result.description,
                          m_result.tags.join(QLatin1Char(' '))),
                 QStringLiteral("标题、描述与话题"));
    });
    publish->headerLayout()->addWidget(copyAll);
    publish->setBodyMargins(QMargins(12, 12, 12, 12));
    auto *publishBody = publish->bodyLayout();
    publishBody->setSpacing(0);
    auto addPublishRow = [&](const QString &label, QWidget *content,
                             const QString &copy, const QString &copyLabel, int gap) {
        auto *row = new QWidget(publish);
        auto *rowLayout = new QHBoxLayout(row);
        rowLayout->setContentsMargins(0, 0, 0, 0);
        rowLayout->setSpacing(8);
        auto *labelWidget = Ui::label(label, 9, QFont::Normal, Theme::Text3, row);
        labelWidget->setFixedWidth(38);
        rowLayout->addWidget(labelWidget, 0, Qt::AlignTop);
        rowLayout->addWidget(content, 1, Qt::AlignVCenter);
        rowLayout->addWidget(makeCopyButton(
                                 copy, copyLabel,
                                 [this, copy, copyLabel] { copyText(copy, copyLabel); },
                                 row),
                             0, Qt::AlignTop);
        publishBody->addWidget(row);
        if (gap > 0) {
            publishBody->addSpacing(gap);
        }
    };
    auto *titleLabel = Ui::label(m_result.title, 11, QFont::Medium, Theme::Text1,
                                 publish);
    titleLabel->setWordWrap(true);
    addPublishRow(QStringLiteral("标题"), titleLabel, m_result.title,
                  QStringLiteral("标题"), 8);
    auto *descriptionLabel = Ui::label(m_result.description, 10, QFont::Normal,
                                       Theme::Text2, publish);
    descriptionLabel->setWordWrap(true);
    addPublishRow(QStringLiteral("描述"), descriptionLabel, m_result.description,
                  QStringLiteral("描述"), 8);
    auto *tagsHost = new QWidget(publish);
    auto *tagsLayout = new QHBoxLayout(tagsHost);
    tagsLayout->setContentsMargins(0, 0, 0, 0);
    tagsLayout->setSpacing(4);
    for (const QString &tag : m_result.tags) {
        tagsLayout->addWidget(Studio::tagLabel(
            tag, Theme::Primary, Theme::alpha(Theme::Primary, 0.14), 9, tagsHost));
    }
    tagsLayout->addStretch(1);
    addPublishRow(QStringLiteral("话题"), tagsHost,
                  m_result.tags.join(QLatin1Char(' ')), QStringLiteral("话题"), 0);
    rightLayout->addWidget(publish);

    auto *details = new Studio::SectionCard(
        Icons::Glyph::Sparkle, QStringLiteral("包装明细"),
        QStringLiteral("%1 项自动处理").arg(m_result.highlights.size()), right);
    details->setBodyMargins(QMargins(12, 12, 12, 12));
    auto *detailsGrid = new QGridLayout;
    detailsGrid->setContentsMargins(0, 0, 0, 0);
    detailsGrid->setHorizontalSpacing(6);
    detailsGrid->setVerticalSpacing(6);
    for (int index = 0; index < m_result.highlights.size(); ++index) {
        detailsGrid->addWidget(
            new Studio::StatTile(m_result.highlights.at(index).first,
                                 m_result.highlights.at(index).second, details),
            index / 4, index % 4);
    }
    details->bodyLayout()->addLayout(detailsGrid);
    details->bodyLayout()->addSpacing(8);
    auto *detailChips = new QWidget(details);
    auto *detailChipsLayout = new QHBoxLayout(detailChips);
    detailChipsLayout->setContentsMargins(0, 0, 0, 0);
    detailChipsLayout->setSpacing(6);
    detailChipsLayout->addWidget(
        new Chip(QStringLiteral("素材 · 相关度 92%"), Chip::Neutral, detailChips));
    detailChipsLayout->addWidget(new Chip(
        QStringLiteral("字幕 · %1 条").arg(m_result.subtitleCount), Chip::Neutral,
        detailChips));
    detailChipsLayout->addStretch(1);
    detailChipsLayout->addWidget(
        Ui::label(QStringLiteral("字幕、画面与音频已自动对齐"), 8, QFont::Normal,
                  Theme::Text3, detailChips));
    details->bodyLayout()->addWidget(detailChips);
    rightLayout->addWidget(details);

    auto *output = new Studio::SectionCard(
        Icons::Glyph::Download, QStringLiteral("输出文件"),
        QStringLiteral("已自动保存到本地"), right);
    output->setBodyMargins(QMargins(12, 12, 12, 12));
    auto *outputBody = output->bodyLayout();
    outputBody->setSpacing(8);
    auto *videoRight = new Chip(QStringLiteral("已自动下载"), Chip::Ok, output);
    outputBody->addWidget(makeFileRow(
        Icons::Glyph::Film, fileName + QStringLiteral(".mp4"), QStringLiteral("MP4"),
        QStringLiteral("%1 · %2 · H.264 · 30fps")
            .arg(StudioData::oralRatioDimensions(m_result.ratio), m_result.fileSize),
        videoRight, output));
    auto *draftRight = new QWidget(output);
    auto *draftRightLayout = new QHBoxLayout(draftRight);
    draftRightLayout->setContentsMargins(0, 0, 0, 0);
    draftRightLayout->setSpacing(0);
    m_resultDraftButton = new Button(QStringLiteral("生成草稿"), Button::Default,
                                     draftRight);
    m_resultDraftButton->setCompact(true);
    m_resultDraftButton->setLeadingGlyph(Icons::Glyph::Scissors);
    if (m_draftState == 2) {
        m_resultDraftButton->setText(QStringLiteral("打开草稿"));
        m_resultDraftButton->setLeadingGlyph(Icons::Glyph::Folder);
        m_resultDraftButton->setTextColor(Theme::Success);
    }
    connect(m_resultDraftButton, &QAbstractButton::clicked, this, [this] {
        if (m_draftState == 2) {
            emit toastRequested(QStringLiteral("已在访达中打开剪映草稿文件夹"));
            return;
        }
        startDraftGeneration();
    });
    draftRightLayout->addWidget(m_resultDraftButton);
    auto *draftRow = makeFileRow(
        Icons::Glyph::Scissors, fileName + QStringLiteral("_剪映草稿"),
        QStringLiteral("草稿"),
        m_draftState == 2
            ? QStringLiteral("已写入剪映草稿目录，可继续修改字幕、素材与音乐")
            : QStringLiteral("生成可二次编辑的剪映草稿"),
        draftRight, output);
    m_resultDraftHint = nullptr;
    for (QLabel *label : draftRow->findChildren<QLabel *>()) {
        if (label->text().startsWith(QStringLiteral("生成可二次编辑"))) {
            m_resultDraftHint = label;
            break;
        }
    }
    outputBody->addWidget(draftRow);
    auto *pathRow = new QWidget(output);
    auto *pathRowLayout = new QHBoxLayout(pathRow);
    pathRowLayout->setContentsMargins(0, 2, 0, 0);
    pathRowLayout->setSpacing(5);
    pathRowLayout->addWidget(Studio::iconLabel(Icons::Glyph::Folder, 9,
                                                 Theme::Text3, pathRow));
    auto *pathLabel = monoLabel(Studio::formatClipPath(m_resultDraftPath, 52), 8,
                                Theme::Text3, pathRow);
    pathLabel->setToolTip(m_resultDraftPath);
    pathRowLayout->addWidget(pathLabel, 1);
    pathRowLayout->addWidget(makeCopyButton(
                                 m_resultDraftPath, QStringLiteral("草稿路径"),
                                 [this] {
                                     copyText(m_resultDraftPath,
                                              QStringLiteral("草稿路径"));
                                 },
                                 pathRow),
                             0);
    outputBody->addWidget(pathRow);
    rightLayout->addWidget(output);

    auto *script = new Studio::SectionCard(
        Icons::Glyph::Doc, QStringLiteral("口播原稿"),
        QStringLiteral("%1 字")
            .arg(QString(m_result.script).remove(QRegularExpression(QStringLiteral("\\s"))).size()),
        right);
    auto *scriptCopy = new LinkLabel(QStringLiteral("复制"), script);
    scriptCopy->setPixelSize(9);
    scriptCopy->setTextColor(Theme::Primary);
    scriptCopy->setHoverColor(Theme::PrimaryHover);
    scriptCopy->setUnderlineOnHover(false);
    connect(scriptCopy, &QAbstractButton::clicked, this, [this] {
        copyText(m_result.script, QStringLiteral("口播文案"));
    });
    script->headerLayout()->addWidget(scriptCopy);
    script->setBodyMargins(QMargins(12, 12, 12, 12));
    auto *scriptEdit = new QTextEdit(script);
    scriptEdit->setReadOnly(true);
    Studio::applyTextAreaStyle(scriptEdit, false, 10);
    scriptEdit->setFixedHeight(128);
    scriptEdit->setPlainText(m_result.script);
    script->bodyLayout()->addWidget(scriptEdit);
    rightLayout->addWidget(script);

    auto *adjustTip = new QWidget(right);
    adjustTip->setObjectName(QStringLiteral("oralAdjustTip"));
    adjustTip->setStyleSheet(
        QStringLiteral("#oralAdjustTip { background:%1; border:1px solid %2;"
                       " border-radius:8px; }")
            .arg(Theme::alpha(Theme::Surface1, 0.6).name(QColor::HexArgb),
                 Theme::Border.name()));
    auto *adjustLayout = new QHBoxLayout(adjustTip);
    adjustLayout->setContentsMargins(12, 9, 12, 9);
    adjustLayout->setSpacing(8);
    adjustLayout->addWidget(Studio::iconLabel(Icons::Glyph::Sparkle, 10, Theme::Ai,
                                               adjustTip));
    auto *adjustLabel = Ui::label(
        QStringLiteral("对结果不满意？可以调整模板、字幕或配图风格后重新包装，口播原片不会重复上传。"),
        9, QFont::Normal, Theme::Text3, adjustTip);
    adjustLabel->setWordWrap(true);
    adjustLayout->addWidget(adjustLabel, 1);
    auto *adjust = new LinkLabel(
        QStringLiteral("调整设置并重新包装 →"), adjustTip);
    adjust->setPixelSize(9);
    adjust->setTextColor(Theme::Primary);
    adjust->setHoverColor(Theme::PrimaryHover);
    adjust->setUnderlineOnHover(true);
    connect(adjust, &QAbstractButton::clicked, this, &OralPage::restartFromResult);
    adjustLayout->addWidget(adjust, 0, Qt::AlignVCenter);
    rightLayout->addWidget(adjustTip);
    rightLayout->addStretch(1);
    columnsLayout->addWidget(right, 116);

    scrollLayout->addWidget(columns);
    auto *scroll = makeScrollArea(scrollContent, m_resultScreen);
    root->addWidget(scroll, 1);

    setResultPlayIcon(m_resultCenterPlay, false);
    updateGenerationClock();
    updatePlayback();
}

void OralPage::openRecent(const StudioData::OralResult &result)
{
    if (isRunning()) {
        showProcessing();
        emit toastRequested(QStringLiteral("当前有智能口播正在生成，完成后即可查看"));
        return;
    }
    m_result = result;
    m_hasResult = true;
    showResult();
}

void OralPage::restartFromResult()
{
    if (!m_hasResult) {
        return;
    }
    if (m_playTimer) {
        m_playTimer->stop();
    }
    if (m_draftTimer) {
        m_draftTimer->stop();
    }
    m_playing = false;
    m_playProgress = 0;
    m_draftState = 0;
    m_draftProgress = 0;
    const StudioData::OralResult result = m_result;
    m_project = StudioData::defaultOralProject();
    m_project.step = 2;
    m_project.templateId = result.templateId;
    m_project.ratio = result.ratio;
    m_project.outputBoth = result.outputBoth;
    showSetup();
}

void OralPage::togglePlayback()
{
    if (!m_hasResult) {
        return;
    }
    if (m_playProgress >= 100) {
        m_playProgress = 0;
    }
    m_playing = !m_playing;
    if (m_playing) {
        m_playTimer->start();
    } else {
        m_playTimer->stop();
    }
    if (m_resultPlayButton) {
        m_resultPlayButton->setText(m_playing ? QStringLiteral("暂停播放")
                                              : QStringLiteral("播放成片"));
        m_resultPlayButton->setLeadingGlyph(m_playing ? Icons::Glyph::Pause
                                                      : Icons::Glyph::Play);
    }
    setResultPlayIcon(m_resultCenterPlay, m_playing);
    updatePlayback();
}

void OralPage::updatePlayback()
{
    if (m_playing) {
        m_playProgress = qMin(100, m_playProgress + 1);
        if (m_playProgress >= 100) {
            m_playing = false;
            m_playTimer->stop();
            if (m_resultPlayButton) {
                m_resultPlayButton->setText(QStringLiteral("播放成片"));
                m_resultPlayButton->setLeadingGlyph(Icons::Glyph::Play);
            }
            setResultPlayIcon(m_resultCenterPlay, false);
        }
    }
    if (m_resultPoster) {
        m_resultPoster->setProgress(m_playProgress);
    }
    if (m_resultPlayBar) {
        m_resultPlayBar->setValue(m_playProgress);
    }
    if (m_resultPlayClock && m_hasResult) {
        const int total = StudioData::parseElapsed(m_result.durationLabel);
        const int current = qRound(total * m_playProgress / 100.0);
        m_resultPlayClock->setText(StudioData::formatElapsed(current * 1000LL));
    }
}

void OralPage::startDraftGeneration()
{
    if (m_draftState != 0) {
        return;
    }
    m_draftState = 1;
    m_draftProgress = 0;
    if (m_resultDraftButton) {
        m_resultDraftButton->setText(QStringLiteral("0%"));
        m_resultDraftButton->setEnabled(false);
    }
    m_draftTimer->start();
}

void OralPage::updateDraftGeneration()
{
    m_draftProgress = qMin(100, m_draftProgress + 9);
    if (m_resultDraftButton) {
        m_resultDraftButton->setText(QStringLiteral("%1%").arg(m_draftProgress));
    }
    if (m_draftProgress >= 100) {
        m_draftTimer->stop();
        m_draftState = 2;
        if (m_resultDraftButton) {
            m_resultDraftButton->setText(QStringLiteral("打开草稿"));
            m_resultDraftButton->setLeadingGlyph(Icons::Glyph::Folder);
            m_resultDraftButton->setTextColor(Theme::Success);
            m_resultDraftButton->setEnabled(true);
        }
        if (m_resultDraftHint) {
            m_resultDraftHint->setText(
                QStringLiteral("已写入剪映草稿目录，可继续修改字幕、素材与音乐"));
        }
        emit toastRequested(
            QStringLiteral("剪映草稿已生成，可在剪映「本地草稿」中继续编辑"));
    }
}

void OralPage::updateGenerationClock()
{
    if (!m_hasResult || !m_resultGenerationLabel) {
        return;
    }
    const QString label = StudioData::formatGenerationTime(m_result.generatedAt);
    m_resultGenerationLabel->setText(label);
    m_resultGenerationLabel->setStyleSheet(
        QStringLiteral("color:%1;background:transparent;")
            .arg((label.startsWith(QStringLiteral("刚刚")) ? Theme::Success
                                                           : Theme::Text1)
                     .name()));
    m_resultGenerationLabel->setToolTip(
        QStringLiteral("生成时间：%1")
            .arg(StudioData::formatGenerationFullTime(m_result.generatedAt)));
}

void OralPage::copyText(const QString &text, const QString &label)
{
    QGuiApplication::clipboard()->setText(text);
    emit toastRequested(QStringLiteral("已复制%1").arg(label));
}
