// 文件作用：一键成片过程页实现。
// 顶部总览、三阶段轨道、ASR / AI 选片 / 草稿生成三个过程面板与底部状态栏
// 全部按设计稿还原；数据来自 LiveClipService 推送的真实过程事件。
#include "PipelinePage.h"

#include "../core/MediaUtils.h"
#include "../ui/Controls.h"
#include "../ui/Icons.h"

#include <QDateTime>
#include <QElapsedTimer>
#include <QEnterEvent>
#include <QFontMetrics>
#include <QGridLayout>
#include <QHBoxLayout>
#include <QHideEvent>
#include <QLabel>
#include <QLinearGradient>
#include <QLocale>
#include <QMouseEvent>
#include <QPainter>
#include <QPainterPath>
#include <QScrollArea>
#include <QScrollBar>
#include <QShowEvent>
#include <QStackedLayout>
#include <QStackedWidget>
#include <QStringList>
#include <QTextLayout>
#include <QTextOption>
#include <QTimer>
#include <QVBoxLayout>

#include <algorithm>
#include <cmath>
#include <functional>

constexpr int kStageCount = 3;
constexpr int kSidePadding = 22;
constexpr int kPanelGap = 12;
constexpr qreal kPi = 3.14159265358979323846;

// -----------------------------------------------------------------------------
// 通用小工具
// -----------------------------------------------------------------------------

static QString formatNumber(qint64 value)
{
    return QLocale(QLocale::English).toString(value);
}

static void setLabelColor(QLabel *label, const QColor &color)
{
    if (label) {
        label->setStyleSheet(
            QStringLiteral("color:%1; background:transparent;").arg(color.name()));
    }
}

static QLabel *monoLabel(const QString &text, int pixelSize, const QColor &color,
                  QWidget *parent)
{
    QLabel *label = Ui::label(text, pixelSize, 400, color, parent);
    label->setFont(Theme::monoFont(pixelSize));
    return label;
}

static QWidget *makeHLine(QWidget *parent)
{
    return Ui::separator(false, parent);
}

static QString elideText(const QString &text, const QFont &font, int width)
{
    return QFontMetrics(font).elidedText(text, Qt::ElideRight, width);
}

static QString stepLabelFor(int step)
{
    static const char *labels[] = {
        "裁剪切片", "上传素材", "创建剪映草稿",
        "添加视频", "添加字幕", "保存草稿",
    };
    return QString::fromUtf8(labels[qBound(0, step, 5)]);
}

static Icons::Glyph stepGlyphFor(int step)
{
    static const Icons::Glyph glyphs[] = {
        Icons::Glyph::Scissors, Icons::Glyph::Cloud, Icons::Glyph::Doc,
        Icons::Glyph::Video,    Icons::Glyph::Subtitles, Icons::Glyph::Check,
    };
    return glyphs[qBound(0, step, 5)];
}

static QString stageName(int stage)
{
    static const char *names[] = {"ASR 转写", "AI 选片", "生成剪映草稿"};
    return QString::fromUtf8(names[qBound(0, stage, 2)]);
}

// 运行时长固定为 mm:ss，超过一小时也继续累计分钟。
static QString formatElapsedValue(qint64 milliseconds)
{
    const qint64 totalSeconds = qMax<qint64>(0, milliseconds) / 1000;
    return QStringLiteral("%1:%2")
        .arg(totalSeconds / 60, 2, 10, QLatin1Char('0'))
        .arg(totalSeconds % 60, 2, 10, QLatin1Char('0'));
}

static QString formatEtaValue(qint64 seconds)
{
    if (seconds <= 0) {
        return QStringLiteral("0 秒");
    }
    if (seconds < 60) {
        return QStringLiteral("%1 秒").arg(seconds);
    }
    const qint64 minutes = seconds / 60;
    const qint64 rest = seconds % 60;
    return rest == 0 ? QStringLiteral("%1 分").arg(minutes)
                     : QStringLiteral("%1 分 %2 秒").arg(minutes).arg(rest);
}

// -----------------------------------------------------------------------------
// Tag：设计稿里的 9px 圆角标签
// -----------------------------------------------------------------------------

class Tag : public QWidget
{
public:
    Tag(const QString &text, const QColor &foreground, const QColor &background,
        const QColor &border, int height = 15, int radius = 3, bool mono = false,
        int pixelSize = 9, int padding = 5, int fixedWidth = 0, QWidget *parent = nullptr)
        : QWidget(parent)
        , m_text(text)
        , m_foreground(foreground)
        , m_background(background)
        , m_border(border)
        , m_radius(radius)
        , m_mono(mono)
        , m_pixelSize(pixelSize)
        , m_padding(padding)
        , m_fixedWidth(fixedWidth)
    {
        setFixedHeight(height);
        setSizePolicy(QSizePolicy::Fixed, QSizePolicy::Fixed);
        setAttribute(Qt::WA_TransparentForMouseEvents, true);
    }

    void setText(const QString &text)
    {
        m_text = text;
        updateGeometry();
        update();
    }

    void setStyle(const QColor &foreground, const QColor &background,
                  const QColor &border)
    {
        m_foreground = foreground;
        m_background = background;
        m_border = border;
        update();
    }

    QSize sizeHint() const override
    {
        if (m_fixedWidth > 0) {
            return QSize(m_fixedWidth, height());
        }
        const QFontMetrics metrics(fontForText());
        return QSize(metrics.horizontalAdvance(m_text) + m_padding * 2, height());
    }

    QSize minimumSizeHint() const override { return sizeHint(); }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        if (m_background.alpha() > 0) {
            painter.setBrush(m_background);
        } else {
            painter.setBrush(Qt::NoBrush);
        }
        if (m_border.alpha() > 0) {
            painter.setPen(QPen(m_border, 1.0));
        } else {
            painter.setPen(Qt::NoPen);
        }
        const QRectF box = QRectF(rect()).adjusted(0.5, 0.5, -0.5, -0.5);
        painter.drawRoundedRect(box, m_radius, m_radius);
        painter.setFont(fontForText());
        painter.setPen(m_foreground);
        painter.drawText(rect(), Qt::AlignCenter, m_text);
    }

private:
    QFont fontForText() const
    {
        return m_mono ? Theme::monoFont(m_pixelSize) : Theme::font(m_pixelSize);
    }

    QString m_text;
    QColor m_foreground;
    QColor m_background;
    QColor m_border;
    int m_radius;
    bool m_mono;
    int m_pixelSize;
    int m_padding;
    int m_fixedWidth;
};

static Tag *makeTag(const QString &text, const QColor &tone, bool withBorder = true,
             bool mono = false, int height = 15, int radius = 3,
             int padding = 5, QWidget *parent = nullptr)
{
    return new Tag(text, tone, Theme::alpha(tone, 0.13),
                   withBorder ? Theme::alpha(tone, 0.35) : Qt::transparent, height,
                   radius, mono, 9, padding, 0, parent);
}

// -----------------------------------------------------------------------------
// Spinner / StatusMark
// -----------------------------------------------------------------------------

class Spinner : public QWidget
{
public:
    explicit Spinner(int side = 12, const QColor &color = Theme::Primary,
                     QWidget *parent = nullptr)
        : QWidget(parent)
        , m_color(color)
    {
        setFixedSize(side, side);
        m_timer = new QTimer(this);
        m_timer->setInterval(60);
        connect(m_timer, &QTimer::timeout, this, [this] {
            m_angle = (m_angle + 30) % 360;
            update();
        });
    }

    void setColor(const QColor &color)
    {
        m_color = color;
        update();
    }

protected:
    void showEvent(QShowEvent *event) override
    {
        m_timer->start();
        QWidget::showEvent(event);
    }

    void hideEvent(QHideEvent *event) override
    {
        m_timer->stop();
        QWidget::hideEvent(event);
    }

    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        const qreal side = qMin(width(), height()) - 1.5;
        const QRectF box((width() - side) / 2.0, (height() - side) / 2.0, side, side);
        painter.setPen(QPen(Theme::BorderStrong, 1.5));
        painter.drawEllipse(box);
        painter.setPen(QPen(m_color, 1.5, Qt::SolidLine, Qt::RoundCap));
        painter.drawArc(box, int((90 - m_angle) * 16), 100 * 16);
    }

private:
    QTimer *m_timer = nullptr;
    QColor m_color;
    int m_angle = 0;
};

class StatusMark : public QWidget
{
public:
    enum class Mode { Dot, Check, Cross, Clock, Spinner, Ring };

    StatusMark(Mode mode, int side = 12, const QColor &color = Theme::Primary,
               QWidget *parent = nullptr)
        : QWidget(parent)
        , m_mode(mode)
        , m_color(color)
    {
        setFixedSize(side, side);
        m_timer = new QTimer(this);
        m_timer->setInterval(50);
        connect(m_timer, &QTimer::timeout, this, [this] {
            if (m_mode == Mode::Dot || m_mode == Mode::Spinner) {
                m_phase = (m_phase + 1) % 120;
                m_angle = (m_angle + 30) % 360;
                update();
            }
        });
        m_timer->start();
    }

    void setMode(Mode mode)
    {
        m_mode = mode;
        update();
    }

    void setColor(const QColor &color)
    {
        m_color = color;
        update();
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        const QRectF box = QRectF(rect()).adjusted(1, 1, -1, -1);
        switch (m_mode) {
        case Mode::Check:
            Icons::paint(&painter, box, Icons::Glyph::Check, m_color, 2.4);
            break;
        case Mode::Cross:
            Icons::paint(&painter, box, Icons::Glyph::Close, m_color, 2.2);
            break;
        case Mode::Clock:
            Icons::paint(&painter, box, Icons::Glyph::Clock, m_color, 2.0);
            break;
        case Mode::Dot: {
            const qreal pulse = 0.55 + 0.45 * std::sin(m_phase / 120.0 * 2.0 * kPi);
            QColor color = m_color;
            color.setAlphaF(0.35 + 0.65 * pulse);
            painter.setPen(Qt::NoPen);
            painter.setBrush(color);
            painter.drawEllipse(QPointF(rect().center()), 3.0, 3.0);
            break;
        }
        case Mode::Spinner: {
            const qreal side = qMin(width(), height()) - 2.0;
            const QRectF arc((width() - side) / 2.0, (height() - side) / 2.0, side, side);
            painter.setPen(QPen(Theme::BorderStrong, 1.5));
            painter.drawEllipse(arc);
            painter.setPen(QPen(m_color, 1.5, Qt::SolidLine, Qt::RoundCap));
            painter.drawArc(arc, int((90 - m_angle) * 16), 100 * 16);
            break;
        }
        case Mode::Ring: {
            painter.setPen(QPen(Theme::BorderStrong, 1.3));
            painter.setBrush(Qt::NoBrush);
            painter.drawEllipse(QRectF(rect()).adjusted(4.5, 4.5, -4.5, -4.5));
            break;
        }
        }
    }

private:
    Mode m_mode;
    QColor m_color;
    QTimer *m_timer = nullptr;
    int m_phase = 0;
    int m_angle = 0;
};

// -----------------------------------------------------------------------------
// 空状态音频柱
// -----------------------------------------------------------------------------

class EqBars : public QWidget
{
public:
    explicit EqBars(const QColor &color, QWidget *parent = nullptr)
        : QWidget(parent)
        , m_color(color)
    {
        setFixedSize(52, 36);
        m_timer = new QTimer(this);
        m_timer->setInterval(33);
        m_clock.start();
        connect(m_timer, &QTimer::timeout, this, [this] {
            m_phase = m_clock.elapsed() / 1000.0;
            update();
        });
        m_timer->start();
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        static const qreal base[] = {0.40, 0.90, 0.60, 1.00, 0.70, 0.45, 0.85};
        static const qreal delays[] = {0.00, 0.15, 0.30, 0.45, 0.60, 0.75, 0.90};
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        painter.setPen(Qt::NoPen);
        painter.setBrush(m_color);
        for (int i = 0; i < 7; ++i) {
            const qreal scale =
                0.70 + 0.30 * std::cos(2.0 * kPi * (m_phase - delays[i]));
            const qreal h = 36.0 * base[i] * scale;
            painter.drawRoundedRect(
                QRectF(i * 8.0, height() - h, 4.0, h), 2, 2);
        }
    }

private:
    QColor m_color;
    QTimer *m_timer = nullptr;
    QElapsedTimer m_clock;
    qreal m_phase = 0.0;
};

// -----------------------------------------------------------------------------
// 圆角步骤进度条
// -----------------------------------------------------------------------------

class StepProgress : public QWidget
{
public:
    explicit StepProgress(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        setFixedHeight(2);
        m_timer = new QTimer(this);
        m_timer->setInterval(40);
        connect(m_timer, &QTimer::timeout, this, [this] {
            if (m_indeterminate) {
                m_phase = (m_phase + 1) % 40;
                update();
            }
        });
    }

    void setState(bool running, int percent)
    {
        m_running = running;
        m_percent = qBound(0, percent, 100);
        m_indeterminate = running && m_percent <= 0;
        if (m_indeterminate) {
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
        painter.setPen(Qt::NoPen);
        painter.setBrush(Theme::alpha(Theme::Primary, 0.15));
        painter.drawRoundedRect(QRectF(0, 0, width(), 2), 1, 1);
        painter.setBrush(Theme::Primary);
        if (m_indeterminate) {
            const qreal seg = width() * 0.28;
            const qreal x = (width() - seg) * (0.5 + 0.5 * std::sin(
                                                     m_phase / 40.0 * 2.0 * kPi));
            painter.drawRoundedRect(QRectF(x, 0, seg, 2), 1, 1);
        } else if (m_percent > 0) {
            const qreal filled = qMax<qreal>(3.0, width() * m_percent / 100.0);
            painter.drawRoundedRect(QRectF(0, 0, filled, 2), 1, 1);
        }
    }

private:
    QTimer *m_timer = nullptr;
    bool m_running = false;
    bool m_indeterminate = false;
    int m_percent = 0;
    int m_phase = 0;
};

// -----------------------------------------------------------------------------
// 步骤图标
// -----------------------------------------------------------------------------

class StepIconBadge : public QWidget
{
public:
    explicit StepIconBadge(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        setFixedSize(24, 24);
    }

    void setState(int status, Icons::Glyph glyph)
    {
        m_status = status;
        m_glyph = glyph;
        update();
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        QColor fill = Theme::Surface2;
        QColor glyphColor = Theme::Text3;
        if (m_status == int(PipelineUi::StepStatus::Running)) {
            fill = Theme::alpha(Theme::Primary, 0.14);
            glyphColor = Theme::Primary;
        } else if (m_status == int(PipelineUi::StepStatus::Done)) {
            fill = Theme::alpha(Theme::Success, 0.13);
            glyphColor = Theme::Success;
        } else if (m_status == int(PipelineUi::StepStatus::Failed)) {
            fill = Theme::alpha(Theme::Danger, 0.13);
            glyphColor = Theme::Danger;
        }
        painter.setPen(Qt::NoPen);
        painter.setBrush(fill);
        painter.drawRoundedRect(QRectF(0, 0, 24, 24), 5, 5);
        painter.setOpacity(m_status == int(PipelineUi::StepStatus::Waiting) ? 0.6 : 1.0);
        Icons::paint(&painter, QRectF(5, 5, 14, 14), m_glyph, glyphColor, 1.8);
        painter.setOpacity(1.0);
        if (m_status == int(PipelineUi::StepStatus::Done)) {
            painter.setPen(Qt::NoPen);
            painter.setBrush(Theme::Success);
            painter.drawEllipse(QPointF(20, 4), 5, 5);
            Icons::paint(&painter, QRectF(17.2, 1.2, 5.6, 5.6), Icons::Glyph::Check,
                         Theme::OnAccent, 2.4);
        }
    }

private:
    int m_status = 0;
    Icons::Glyph m_glyph = Icons::Glyph::Check;
};

// -----------------------------------------------------------------------------
// 阶段轨道
// -----------------------------------------------------------------------------

class StageRailWidget : public QWidget
{
public:
    explicit StageRailWidget(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        setFixedHeight(68);
        setMouseTracking(true);
        setCursor(Qt::PointingHandCursor);
        m_timer = new QTimer(this);
        m_timer->setInterval(60);
        connect(m_timer, &QTimer::timeout, this, [this] {
            bool pulse = false;
            for (int i = 0; i < kStageCount; ++i) {
                if (m_status[i] == PipelineUi::StageStatus::Running) {
                    pulse = true;
                }
            }
            if (pulse) {
                m_phase = (m_phase + 1) % 60;
                update();
            }
        });
        m_timer->start();
    }

    void setState(const PipelineUi::StageStatus status[3], const int progress[3],
                  const QString meta[3], int focus)
    {
        for (int i = 0; i < kStageCount; ++i) {
            m_status[i] = status[i];
            m_progress[i] = progress[i];
            m_meta[i] = meta[i];
        }
        m_focus = focus;
        update();
    }

    std::function<void(int)> onFocus;

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        const qreal gap = 8.0;
        const qreal cardWidth = (width() - gap * 2.0) / 3.0;
        for (int i = 0; i < kStageCount; ++i) {
            const QRectF card(i * (cardWidth + gap), 0, cardWidth, height());
            QColor fill = Theme::Surface1;
            QColor border = Theme::Border;
            if (m_focus == i) {
                fill = Theme::alpha(Theme::Primary, 0.14);
                border = Theme::alpha(Theme::Primary, 0.5);
            } else if (m_status[i] == PipelineUi::StageStatus::Failed) {
                border = Theme::alpha(Theme::Danger, 0.35);
            } else if (m_status[i] == PipelineUi::StageStatus::Done) {
                border = Theme::alpha(Theme::Success, 0.25);
            } else if (m_hovered == i) {
                border = Theme::BorderStrong;
            }
            painter.setPen(QPen(border, 1.0));
            painter.setBrush(fill);
            painter.drawRoundedRect(card.adjusted(0.5, 0.5, -0.5, -0.5), 8, 8);

            const QRectF badge(card.left() + 12, card.top() + 8, 20, 20);
            if (m_status[i] == PipelineUi::StageStatus::Done) {
                painter.setPen(Qt::NoPen);
                painter.setBrush(Theme::Success);
                painter.drawEllipse(badge);
                Icons::paint(&painter, badge.adjusted(6, 6, -6, -6),
                             Icons::Glyph::Check, Theme::OnAccent, 2.6);
            } else if (m_status[i] == PipelineUi::StageStatus::Running) {
                painter.setPen(Qt::NoPen);
                painter.setBrush(Theme::alpha(Theme::Primary, 0.14));
                painter.drawEllipse(badge.adjusted(-3, -3, 3, 3));
                painter.setBrush(Theme::Primary);
                painter.drawEllipse(badge);
                painter.setPen(Theme::OnAccent);
                painter.setFont(Theme::monoFont(10));
                painter.drawText(badge, Qt::AlignCenter, QString::number(i + 1));
            } else if (m_status[i] == PipelineUi::StageStatus::Failed) {
                painter.setPen(Qt::NoPen);
                painter.setBrush(Theme::Danger);
                painter.drawEllipse(badge);
                Icons::paint(&painter, badge.adjusted(6, 6, -6, -6), Icons::Glyph::Close,
                             Theme::OnAccent, 2.6);
            } else {
                painter.setPen(QPen(Theme::BorderStrong, 1.0));
                painter.setBrush(Theme::Surface2);
                painter.drawEllipse(badge);
                painter.setPen(Theme::Text3);
                painter.setFont(Theme::monoFont(10));
                painter.drawText(badge, Qt::AlignCenter, QString::number(i + 1));
            }

            painter.setFont(Theme::font(12, 600));
            painter.setPen(m_focus == i ? Theme::Text1
                                        : (m_status[i] == PipelineUi::StageStatus::Waiting
                                               ? Theme::Text2
                                               : Theme::Text1));
            painter.drawText(QRectF(card.left() + 40, card.top() + 8,
                                    card.width() - 40, 20),
                             Qt::AlignLeft | Qt::AlignVCenter, stageName(i));

            QString statusText;
            QColor statusColor = Theme::Text3;
            if (m_status[i] == PipelineUi::StageStatus::Done) {
                statusText = QStringLiteral("完成");
                statusColor = Theme::Success;
            } else if (m_status[i] == PipelineUi::StageStatus::Running) {
                statusText = QStringLiteral("进行中");
                statusColor = Theme::Primary;
            } else if (m_status[i] == PipelineUi::StageStatus::Failed) {
                statusText = QStringLiteral("失败");
                statusColor = Theme::Danger;
            } else {
                statusText = QStringLiteral("等待");
            }
            QFont statusFont = Theme::font(10);
            painter.setFont(statusFont);
            const int statusWidth =
                QFontMetrics(statusFont).horizontalAdvance(statusText);
            const qreal statusRight = card.right() - 12;
            painter.setPen(statusColor);
            painter.drawText(QRectF(statusRight - statusWidth, card.top() + 8,
                                    statusWidth, 20),
                             Qt::AlignRight | Qt::AlignVCenter, statusText);
            if (m_status[i] == PipelineUi::StageStatus::Running) {
                const qreal alpha = qBound<qreal>(
                    0.25, 0.55 + 0.45 * std::sin(m_phase / 60.0 * 2.0 * kPi), 1.0);
                QColor dot = Theme::Primary;
                dot.setAlphaF(alpha);
                painter.setPen(Qt::NoPen);
                painter.setBrush(dot);
                painter.drawEllipse(QPointF(statusRight - statusWidth - 8,
                                           card.top() + 18),
                                    2, 2);
            }

            QFont metaFont = Theme::font(10);
            painter.setFont(metaFont);
            painter.setPen(Theme::Text3);
            const QString meta = elideText(m_meta[i], metaFont,
                                           int(card.width() - 24));
            painter.drawText(QRectF(card.left() + 12, card.top() + 34,
                                    card.width() - 24, 16),
                             Qt::AlignLeft | Qt::AlignVCenter, meta);

            const QRectF track(card.left() + 12, card.bottom() - 12,
                               card.width() - 24, 2);
            painter.setPen(Qt::NoPen);
            painter.setBrush(Theme::Surface3);
            painter.drawRoundedRect(track, 1, 1);
            if (m_progress[i] > 0) {
                painter.setBrush(m_status[i] == PipelineUi::StageStatus::Done
                                     ? Theme::Success
                                     : Theme::Primary);
                painter.drawRoundedRect(
                    QRectF(track.left(), track.top(),
                           qMax<qreal>(2.0, track.width() * m_progress[i] / 100.0), 2),
                    1, 1);
            }
        }
    }

    void mouseMoveEvent(QMouseEvent *event) override
    {
        const qreal cardWidth = (width() - 16.0) / 3.0;
        int hovered = -1;
        for (int i = 0; i < kStageCount; ++i) {
            if (event->position().x() >= i * (cardWidth + 8.0)
                && event->position().x() < (i + 1) * (cardWidth + 8.0)) {
                hovered = i;
            }
        }
        if (hovered != m_hovered) {
            m_hovered = hovered;
            update();
        }
        QWidget::mouseMoveEvent(event);
    }

    void leaveEvent(QEvent *event) override
    {
        m_hovered = -1;
        update();
        QWidget::leaveEvent(event);
    }

    void mousePressEvent(QMouseEvent *event) override
    {
        if (event->button() == Qt::LeftButton && onFocus) {
            const qreal cardWidth = (width() - 16.0) / 3.0;
            for (int i = 0; i < kStageCount; ++i) {
                if (event->position().x() >= i * (cardWidth + 8.0)
                    && event->position().x() < (i + 1) * (cardWidth + 8.0)) {
                    onFocus(i);
                    break;
                }
            }
        }
        QWidget::mousePressEvent(event);
    }

private:
    QTimer *m_timer = nullptr;
    PipelineUi::StageStatus m_status[3] = {
        PipelineUi::StageStatus::Waiting,
        PipelineUi::StageStatus::Waiting,
        PipelineUi::StageStatus::Waiting,
    };
    int m_progress[3] = {0, 0, 0};
    QString m_meta[3];
    int m_focus = 0;
    int m_hovered = -1;
    int m_phase = 0;
};

// -----------------------------------------------------------------------------
// 转写稿（整片识别完成后一次性展示）
// -----------------------------------------------------------------------------

class TranscriptView : public QWidget
{
public:
    explicit TranscriptView(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        setSizePolicy(QSizePolicy::Preferred, QSizePolicy::Preferred);
        setAttribute(Qt::WA_OpaquePaintEvent, false);
    }

    void clear()
    {
        m_lines.clear();
        m_layouts.clear();
        m_markers.clear();
        m_contentHeight = 0;
        setMinimumHeight(0);
        updateGeometry();
        update();
    }

    void setLines(const QVector<AsrUtterance> &lines)
    {
        m_lines.clear();
        m_lines.reserve(lines.size());
        for (const AsrUtterance &utterance : lines) {
            const QString text = utterance.text.trimmed();
            if (text.isEmpty()) {
                continue;
            }
            Line line;
            line.startMs = utterance.startMs;
            line.speaker = utterance.speaker.trimmed();
            line.text = text;
            m_lines.append(line);
        }
        rebuildLayouts();
    }

    void setMarkers(const QVector<PipelineUi::Candidate> &markers)
    {
        m_markers = markers;
        rebuildLayouts();
    }

    int lineCount() const { return m_lines.size(); }

    QSize sizeHint() const override
    {
        return QSize(400, m_contentHeight > 0 ? m_contentHeight : 40);
    }

protected:
    void resizeEvent(QResizeEvent *event) override
    {
        rebuildLayouts();
        QWidget::resizeEvent(event);
    }

    void paintEvent(QPaintEvent *event) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        painter.setRenderHint(QPainter::TextAntialiasing, true);

        const QFont timeFont = Theme::monoFont(10);
        const QFont badgeFont = Theme::font(9);
        const QFont textFont = Theme::font(12);
        const QFont markerFont = Theme::monoFont(9);

        for (int i = 0; i < m_lines.size() && i < m_layouts.size(); ++i) {
            const Line &line = m_lines.at(i);
            const Layout &layout = m_layouts.at(i);
            if (layout.y + layout.height < event->rect().top()) {
                continue;
            }
            if (layout.y > event->rect().bottom()) {
                break;
            }

            painter.setFont(timeFont);
            painter.setPen(Theme::Text3);
            painter.drawText(QRectF(14, layout.y + 3, 48, 17),
                             Qt::AlignLeft | Qt::AlignVCenter,
                             PipelineUi::formatClock(line.startMs));

            // 没有说话人分离结果时使用中性标签，避免伪造“主播 / 助播”。
            const QString speaker = line.speaker.isEmpty()
                                        ? QStringLiteral("说话人")
                                        : line.speaker.left(2);
            const bool anchor = line.speaker == QStringLiteral("主播");
            const QRectF badge(70, layout.y + 4, 30, 15);
            painter.setPen(Qt::NoPen);
            painter.setBrush(anchor ? Theme::alpha(Theme::Primary, 0.14)
                                    : Theme::Surface2);
            painter.drawRoundedRect(badge, 3, 3);
            painter.setFont(badgeFont);
            painter.setPen(anchor ? Theme::Primary : Theme::Text3);
            painter.drawText(badge, Qt::AlignCenter, speaker);

            const QRectF textRect(108, layout.y + 4, textWidthFor(line),
                                  layout.height - 8);
            painter.setFont(textFont);
            painter.setPen(Theme::Text2);
            painter.drawText(textRect, Qt::TextWordWrap | Qt::AlignLeft | Qt::AlignTop,
                             line.text);

            const PipelineUi::Candidate *marker = markerFor(line);
            if (marker) {
                const QString markerText =
                    QStringLiteral("入选 %1").arg(marker->index, 2, 10, QLatin1Char('0'));
                const qreal markerWidth =
                    QFontMetrics(markerFont).horizontalAdvance(markerText) + 8;
                const QRectF markerRect(width() - 14 - markerWidth, layout.y + 4,
                                        markerWidth, 14);
                painter.setFont(markerFont);
                painter.setPen(Qt::NoPen);
                painter.setBrush(Theme::alpha(Theme::Ai, 0.13));
                painter.drawRoundedRect(markerRect, 3, 3);
                painter.setPen(Theme::Ai);
                painter.drawText(markerRect, Qt::AlignCenter, markerText);
            }
        }
    }

private:
    struct Line
    {
        qint64 startMs = 0;
        QString speaker;
        QString text;
    };

    struct Layout
    {
        int y = 0;
        int height = 0;
    };

    const PipelineUi::Candidate *markerFor(const Line &line) const
    {
        for (const PipelineUi::Candidate &candidate : m_markers) {
            if (line.startMs >= candidate.startMs && line.startMs < candidate.endMs) {
                return &candidate;
            }
        }
        return nullptr;
    }

    qreal markerWidth() const
    {
        const QFont markerFont = Theme::monoFont(9);
        return QFontMetrics(markerFont).horizontalAdvance(QStringLiteral("入选 00")) + 8;
    }

    int textWidthFor(const Line &line) const
    {
        const qreal reserved = markerFor(line) ? markerWidth() + 8 : 0;
        return qMax(60, int(width() - 108 - 14 - reserved));
    }

    int measureHeight(const Line &line) const
    {
        const QFontMetrics metrics(Theme::font(12));
        const int textHeight =
            metrics.boundingRect(QRect(0, 0, textWidthFor(line), 0),
                                 Qt::TextWordWrap, line.text)
                .height();
        return qMax(23, textHeight + 7);
    }

    void rebuildLayouts()
    {
        m_layouts.resize(m_lines.size());
        int y = 4;
        for (int i = 0; i < m_lines.size(); ++i) {
            const int height = measureHeight(m_lines.at(i));
            m_layouts[i].y = y;
            m_layouts[i].height = height;
            y += height;
        }
        m_contentHeight = y + 4;
        setMinimumHeight(m_contentHeight);
        updateGeometry();
        update();
    }

    QVector<Line> m_lines;
    QVector<Layout> m_layouts;
    QVector<PipelineUi::Candidate> m_markers;
    int m_contentHeight = 0;
};

// -----------------------------------------------------------------------------
// 思考行
// -----------------------------------------------------------------------------

class ThoughtRow : public QWidget
{
public:
    ThoughtRow(const QString &kind, const QColor &tone, const QString &text,
               QWidget *parent = nullptr)
        : QWidget(parent)
    {
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Minimum);
        auto *layout = new QHBoxLayout(this);
        layout->setContentsMargins(8, 5, 8, 5);
        layout->setSpacing(8);
        auto *tag = new Tag(kind, tone, Theme::alpha(tone, 0.13),
                            Theme::alpha(tone, 0.30), 15, 3, false, 9, 5, 30);
        layout->addWidget(tag, 0, Qt::AlignTop);
        m_label = Ui::label(text, 12, 400, Theme::Text2, this);
        m_label->setWordWrap(true);
        m_label->setTextFormat(Qt::PlainText);
        m_label->setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Minimum);
        layout->addWidget(m_label, 1);
    }

    void setTextColor(const QColor &color)
    {
        setLabelColor(m_label, color);
    }

private:
    QLabel *m_label = nullptr;
};

// -----------------------------------------------------------------------------
// 实时思考面板：把 reasoning 流切成要点逐条展示，不把完整内容堆进历史日志
// -----------------------------------------------------------------------------

// 模型以连续 token 输出 reasoning，原文通常是一大段逗号长句。这里按句末标点、
// 换行和过长逗号切分成一条条“思考要点”，让实时面板逐条展示而不是挤成一行。
class ReasoningOutline
{
public:
    void reset()
    {
        m_pending.clear();
        m_carry.clear();
        m_points.clear();
    }

    void append(const QString &delta)
    {
        for (const QChar ch : delta) {
            if (ch == QLatin1Char('\r')) {
                continue;
            }
            if (ch == QLatin1Char('\n')) {
                commitPending();
                continue;
            }
            m_pending += ch;
            if (!isSentenceEnd(ch)) {
                splitLongPending();
                continue;
            }
            // “……”在第二个省略号处才断句，避免把停顿拆成两条。
            if (ch == QChar(0x2026) && !m_pending.endsWith(QStringLiteral("……"))) {
                splitLongPending();
                continue;
            }
            commitPending();
        }
    }

    // 已完成要点 + 正在输出的半句，最后一条即当前思考。
    QStringList displayPoints() const
    {
        QStringList points = m_points;
        const QString active = activeText();
        if (!active.isEmpty()) {
            points.append(active);
        }
        return points;
    }

private:
    static constexpr int kMaxPointChars = 64;  // 单条要点上限，约一行中文
    static constexpr int kMinSplitChars = 18;  // 长句尽量在逗号处断开
    static constexpr int kMinPointChars = 6;   // 过短的语气词并入下一条
    static constexpr int kMaxPoints = 12;

    static bool isSentenceEnd(QChar ch)
    {
        return ch == QChar(0x3002)  // 。
            || ch == QChar(0xFF01)  // ！
            || ch == QChar(0xFF1F)  // ？
            || ch == QChar(0xFF1B)  // ；
            || ch == QLatin1Char('!')
            || ch == QLatin1Char('?')
            || ch == QLatin1Char(';')
            || ch == QChar(0x2026);  // …
    }

    // 去掉 reasoning 里常见的 Markdown 标记与列表序号，只保留要点正文。
    static QString cleanPoint(QString text)
    {
        text.replace(QLatin1Char('\t'), QLatin1Char(' '));
        text.remove(QLatin1String("**"));
        text.remove(QLatin1Char('`'));
        for (;;) {
            text = text.trimmed();
            if (text.isEmpty()) {
                break;
            }
            const QChar first = text.at(0);
            if (first == QLatin1Char('#') || first == QLatin1Char('>')) {
                text.remove(0, 1);
                continue;
            }
            if ((first == QLatin1Char('-') || first == QLatin1Char('*')
                 || first == QChar(0x2022) || first == QChar(0x00B7))
                && text.size() >= 2 && text.at(1) == QLatin1Char(' ')) {
                text.remove(0, 2);
                continue;
            }
            int digits = 0;
            while (digits < text.size() && text.at(digits).isDigit()) {
                ++digits;
            }
            if (digits > 0 && digits < text.size()) {
                const QChar mark = text.at(digits);
                const bool spaced = digits + 1 < text.size()
                    && text.at(digits + 1) == QLatin1Char(' ');
                if (mark == QChar(0x3001) || mark == QLatin1Char(')')
                    || mark == QChar(0xFF09)
                    || (mark == QLatin1Char('.') && spaced)) {
                    text.remove(0, digits + 1);
                    continue;
                }
            }
            if (first == QLatin1Char('(') || first == QChar(0xFF08)) {
                const QChar close =
                    first == QLatin1Char('(') ? QChar(')') : QChar(0xFF09);
                const int end = text.indexOf(close, 1);
                if (end > 1 && end <= 4) {
                    bool allDigits = true;
                    for (int i = 1; i < end; ++i) {
                        if (!text.at(i).isDigit()) {
                            allDigits = false;
                            break;
                        }
                    }
                    if (allDigits) {
                        text.remove(0, end + 1);
                        continue;
                    }
                }
            }
            break;
        }
        text = text.simplified().trimmed();
        // 按逗号折行时去掉尾部停顿符，避免要点看起来像被截断的半句。
        while (!text.isEmpty()) {
            const QChar last = text.at(text.size() - 1);
            if (last != QChar(0xFF0C) && last != QChar(0x3001)
                && last != QChar(0xFF1A) && last != QLatin1Char(',')) {
                break;
            }
            text.chop(1);
            text = text.trimmed();
        }
        return text;
    }

    QString activeText() const { return cleanPoint(m_carry + m_pending); }

    void commitPending()
    {
        const QString text = cleanPoint(m_pending);
        m_pending.clear();
        addPoint(text);
    }

    void addPoint(QString text)
    {
        if (text.isEmpty()) {
            return;
        }
        if (!m_carry.isEmpty()) {
            text.prepend(m_carry);
            m_carry.clear();
        }
        if (text.size() < kMinPointChars) {
            m_carry = text;
            return;
        }
        if (!m_points.isEmpty() && m_points.last() == text) {
            return;
        }
        m_points.append(text);
        while (m_points.size() > kMaxPoints) {
            m_points.removeFirst();
        }
    }

    // 一直没有句末标点时，退到逗号处折成新要点，避免单条无限变长。
    void splitLongPending()
    {
        while (m_pending.size() > kMaxPointChars) {
            int cut = -1;
            for (int i = kMaxPointChars; i >= kMinSplitChars; --i) {
                const QChar ch = m_pending.at(i - 1);
                if (ch == QChar(0xFF0C) || ch == QChar(0x3001)
                    || ch == QLatin1Char(',') || ch == QChar(0xFF1A)) {
                    cut = i;
                    break;
                }
            }
            if (cut < 0) {
                cut = kMaxPointChars;
            }
            const QString head = m_pending.left(cut);
            m_pending.remove(0, cut);
            addPoint(cleanPoint(head));
        }
    }

    QString m_pending;
    QString m_carry;
    QStringList m_points;
};

// 思考要点视图：逐条绘制要点，最新一条高亮并带光标；固定高度避免打字机式跳动。
class ThoughtOutlineView : public QWidget
{
public:
    explicit ThoughtOutlineView(QWidget *parent = nullptr)
        : QWidget(parent)
        , m_font(Theme::font(11))
    {
        m_lineHeight = QFontMetrics(m_font).lineSpacing() + 1;
        setFixedHeight(m_lineHeight * kMaxLines + kPointGap * (kMaxPoints - 1) + 2);
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
        setAttribute(Qt::WA_TransparentForMouseEvents, true);
        m_blinkTimer = new QTimer(this);
        m_blinkTimer->setInterval(600);
        connect(m_blinkTimer, &QTimer::timeout, this, [this] {
            m_blinkOn = !m_blinkOn;
            if (m_streaming) {
                update();
            }
        });
    }

    void setPoints(const QStringList &points, bool streaming)
    {
        m_points = points;
        m_message.clear();
        setStreaming(streaming);
        update();
    }

    void setMessage(const QString &text, bool streaming = false)
    {
        m_message = text;
        m_points.clear();
        setStreaming(streaming);
        update();
    }

    void setTextColor(const QColor &color)
    {
        m_textColor = color;
        update();
    }

    void setCursorColor(const QColor &color)
    {
        m_cursorColor = color;
        update();
    }

    void setFadeColor(const QColor &color)
    {
        m_fadeColor = color;
        update();
    }

protected:
    void showEvent(QShowEvent *event) override
    {
        if (m_streaming) {
            m_blinkTimer->start();
        }
        QWidget::showEvent(event);
    }

    void hideEvent(QHideEvent *event) override
    {
        m_blinkTimer->stop();
        QWidget::hideEvent(event);
    }

    void paintEvent(QPaintEvent *) override
    {
        const QStringList blocks =
            m_message.isEmpty() ? m_points
                                : m_message.split(QLatin1Char('\n'), Qt::SkipEmptyParts);
        if (blocks.isEmpty()) {
            return;
        }
        const bool pointsMode = m_message.isEmpty();

        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        painter.setRenderHint(QPainter::TextAntialiasing, true);

        // 从最新一条往前选，固定高度内尽量多保留几条完整的要点。
        const int avail = height() - 2;
        int first = blocks.size() - 1;
        int used = 0;
        for (int i = blocks.size() - 1; i >= 0; --i) {
            const int need = wrappedHeight(blocks.at(i))
                + (used > 0 ? kPointGap : 0);
            if (used > 0 && used + need > avail) {
                break;
            }
            used += need;
            first = i;
            if (used >= avail) {
                break;
            }
        }

        const QColor olderColor = Theme::mix(m_textColor, m_fadeColor, 0.45);

        int y = 0;
        bool clipped = false;
        for (int i = first; i < blocks.size(); ++i) {
            const bool newest = i == blocks.size() - 1;
            const bool active = m_streaming && newest;
            const QColor color =
                !pointsMode || newest ? m_textColor : olderColor;
            y = paintBlock(painter, blocks.at(i), y, avail - y, color,
                           pointsMode, newest, active, &clipped);
            if (i + 1 < blocks.size()) {
                y += kPointGap;
            }
        }

        // 顶部渐隐提示“上面还有内容”，避免出现被硬切的半行。
        if (clipped) {
            QLinearGradient gradient(0, 0, 0, 10);
            gradient.setColorAt(0.0, m_fadeColor);
            QColor transparent = m_fadeColor;
            transparent.setAlpha(0);
            gradient.setColorAt(1.0, transparent);
            painter.fillRect(QRectF(0, 0, width(), 10), gradient);
        }
    }

private:
    static constexpr int kMaxLines = 4;
    static constexpr int kMaxPoints = 3;
    static constexpr int kPointGap = 4;
    static constexpr int kTextIndent = 13;

    void setStreaming(bool streaming)
    {
        m_streaming = streaming;
        if (streaming && isVisible()) {
            if (!m_blinkTimer->isActive()) {
                m_blinkTimer->start();
            }
        } else if (!streaming) {
            m_blinkTimer->stop();
            m_blinkOn = true;
        }
    }

    // 与实际绘制一致，按 QTextLine 的真实行高统计，避免最后一行被裁掉。
    int wrappedHeight(const QString &text) const
    {
        if (text.isEmpty()) {
            return m_lineHeight;
        }
        QTextLayout layout(text, m_font);
        QTextOption option;
        option.setWrapMode(QTextOption::WrapAtWordBoundaryOrAnywhere);
        layout.setTextOption(option);
        layout.beginLayout();
        int height = 0;
        const int lineWidth = qMax(40, width() - kTextIndent);
        while (true) {
            QTextLine line = layout.createLine();
            if (!line.isValid()) {
                break;
            }
            line.setLineWidth(lineWidth);
            height += line.height();
        }
        layout.endLayout();
        return qMax(m_lineHeight, height);
    }

    // 返回该要点绘制后的底部 y；超出 maxPixels 时只画末尾几行（顶部渐隐）。
    int paintBlock(QPainter &painter, const QString &text, int top, int maxPixels,
                   const QColor &color, bool bullet, bool newest, bool active,
                   bool *clipped)
    {
        const int textWidth = qMax(40, width() - kTextIndent);
        QTextLayout layout(text, m_font);
        QTextOption option;
        option.setWrapMode(QTextOption::WrapAtWordBoundaryOrAnywhere);
        layout.setTextOption(option);
        layout.beginLayout();
        QVector<QTextLine> lines;
        while (true) {
            QTextLine line = layout.createLine();
            if (!line.isValid()) {
                break;
            }
            line.setLineWidth(textWidth);
            lines.append(line);
        }
        layout.endLayout();
        if (lines.isEmpty()) {
            return top;
        }

        int firstLine = 0;
        int totalHeight = 0;
        for (const QTextLine &line : lines) {
            totalHeight += line.height();
        }
        if (maxPixels > 0 && totalHeight > maxPixels) {
            int tail = 0;
            for (int i = lines.size() - 1; i >= 0; --i) {
                if (tail + lines.at(i).height() > maxPixels
                    && i < lines.size() - 1) {
                    break;
                }
                tail += lines.at(i).height();
                firstLine = i;
            }
            if (clipped) {
                *clipped = true;
            }
        }
        if (bullet) {
            const qreal dotX = 3.0;
            const qreal dotY = top + lines.at(firstLine).height() / 2.0;
            painter.setPen(Qt::NoPen);
            painter.setBrush(newest ? m_cursorColor
                                    : Theme::mix(color, m_fadeColor, 0.35));
            painter.drawEllipse(QPointF(dotX, dotY), newest ? 2.0 : 1.5,
                                newest ? 2.0 : 1.5);
        }

        int y = top;
        painter.setPen(color);
        for (int i = firstLine; i < lines.size(); ++i) {
            lines.at(i).draw(&painter, QPointF(kTextIndent, y));
            y += lines.at(i).height();
        }

        if (active && m_blinkOn) {
            const QTextLine &last = lines.last();
            const qreal cursorX =
                kTextIndent + qMin<qreal>(last.naturalTextWidth(), textWidth - 2.0);
            const qreal lastTop = y - last.height();
            painter.fillRect(QRectF(cursorX + 1.5, lastTop + 2, 1.5,
                                    qMax<qreal>(8.0, m_lineHeight - 5)),
                             m_cursorColor);
        }
        return y;
    }

    QFont m_font;
    int m_lineHeight = 15;
    QStringList m_points;
    QString m_message;
    QColor m_textColor = Theme::Text2;
    QColor m_cursorColor = Theme::Ai;
    QColor m_fadeColor = Theme::Surface1;
    bool m_streaming = false;
    bool m_blinkOn = true;
    QTimer *m_blinkTimer = nullptr;
};

class LiveThinkingPanel : public QWidget
{
public:
    explicit LiveThinkingPanel(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
        auto *layout = new QVBoxLayout(this);
        layout->setContentsMargins(10, 7, 10, 8);
        layout->setSpacing(4);

        auto *header = new QHBoxLayout;
        header->setContentsMargins(0, 0, 0, 0);
        header->setSpacing(6);
        m_mark = new StatusMark(StatusMark::Mode::Clock, 11, Theme::Text3, this);
        header->addWidget(m_mark);
        m_title = new ElidedLabel(QStringLiteral("等待 AI 选片开始"), this);
        m_title->setPixelSize(12);
        m_title->setFontWeight(500);
        m_title->setTextColor(Theme::Text1);
        header->addWidget(m_title, 1);
        m_meta = monoLabel(QString(), 9, Theme::Text3, this);
        header->addWidget(m_meta);
        layout->addLayout(header);

        m_body = new ThoughtOutlineView(this);
        layout->addWidget(m_body);

        reset();
    }

    void reset()
    {
        m_index = 0;
        m_count = 0;
        m_totalChars = 0;
        m_outline.reset();
        m_rateStarted = false;
        setTone(Theme::Text3);
        setHeader(QStringLiteral("等待 AI 选片开始"), QString(),
                  StatusMark::Mode::Clock, Theme::Text3);
        m_body->setTextColor(Theme::Text2);
        m_body->setMessage(QStringLiteral("ASR 转写完成后会自动开始分段分析。"));
        hide();
    }

    void showPlan(int count)
    {
        m_index = 0;
        m_count = count;
        m_totalChars = 0;
        m_outline.reset();
        m_rateStarted = false;
        setTone(Theme::Ai);
        setHeader(QStringLiteral("准备开始分段分析"),
                  QStringLiteral("共 %1 段").arg(count), StatusMark::Mode::Spinner,
                  Theme::Ai);
        m_body->setTextColor(Theme::Text2);
        m_body->setMessage(
            QStringLiteral("模型将逐段读取转写、评估切片并锁定高价值片段。"));
        show();
    }

    void beginBatch(int index, int count)
    {
        m_index = index;
        m_count = count;
        m_totalChars = 0;
        m_outline.reset();
        m_rateStarted = false;
        setTone(Theme::Ai);
        setHeader(QStringLiteral("等待模型响应…"),
                  QStringLiteral("第 %1/%2 段").arg(index).arg(count),
                  StatusMark::Mode::Spinner, Theme::Ai);
        m_body->setTextColor(Theme::Text2);
        m_body->setMessage(
            QStringLiteral("已提交第 %1/%2 段转写，等待模型开始思考…")
                .arg(index)
                .arg(count));
        show();
    }

    void appendThought(int index, int count, const QString &delta)
    {
        if (m_index != index) {
            beginBatch(index, count);
        }
        if (!m_rateStarted) {
            m_rateClock.start();
            m_rateStarted = true;
        }
        m_totalChars += delta.size();
        m_outline.append(delta);
        setTone(Theme::Ai);
        const qint64 seconds = qMax<qint64>(1, m_rateClock.elapsed() / 1000);
        const int rate = int(qMin<qint64>(9999, m_totalChars / seconds));
        QString meta = QStringLiteral("已输出 %1 字").arg(formatNumber(m_totalChars));
        if (m_rateClock.elapsed() >= 1000) {
            meta += QStringLiteral(" · %1 字/s").arg(rate);
        }
        if (m_count > 0) {
            meta = QStringLiteral("第 %1/%2 段 · ").arg(m_index).arg(m_count) + meta;
        }
        setHeader(QStringLiteral("模型思考中"), meta, StatusMark::Mode::Spinner,
                  Theme::Ai);
        m_body->setTextColor(Theme::Text2);
        m_body->setPoints(m_outline.displayPoints(), true);
        show();
    }

    void showIdle(const QString &text)
    {
        QString meta = text;
        const int comma = text.indexOf(QStringLiteral("，"));
        if (comma >= 0) {
            meta = text.mid(comma + 1);
        }
        setTone(Theme::Ai);
        setHeader(QStringLiteral("模型仍在处理中"), meta, StatusMark::Mode::Spinner,
                  Theme::Ai);
        m_body->setTextColor(Theme::Text2);
        setOutlineBody(
            QStringLiteral("已提交第 %1/%2 段转写，等待模型返回思考内容。\n"
                           "连接保持中，任务仍在后台运行，可切到其他页面。")
                .arg(m_index)
                .arg(m_count));
        show();
    }

    void showContent(const QString &text)
    {
        Q_UNUSED(text);
        setTone(Theme::Ai);
        setHeader(QStringLiteral("正在生成选片结果…"),
                  m_count > 0
                      ? QStringLiteral("第 %1/%2 段").arg(m_index).arg(m_count)
                      : QString(),
                  StatusMark::Mode::Spinner, Theme::Ai);
        m_body->setTextColor(Theme::Text2);
        setOutlineBody(
            QStringLiteral("模型已完成思考，正在输出结构化选片结果。"));
        show();
    }

    void showStatus(const QString &text)
    {
        const bool warn = text.contains(QStringLiteral("重试"))
            || text.contains(QStringLiteral("繁忙"))
            || text.contains(QStringLiteral("不可用"));
        const QColor tone = warn ? Theme::Warn : Theme::Ai;
        setTone(tone);
        setHeader(text,
                  m_count > 0
                      ? QStringLiteral("第 %1/%2 段").arg(m_index).arg(m_count)
                      : QString(),
                  StatusMark::Mode::Spinner, tone);
        m_body->setTextColor(Theme::Text2);
        setOutlineBody(
            QStringLiteral("已保留第 %1/%2 段的转写与提示词，重试会继续当前候选。\n"
                           "通常几秒内自动恢复，无需手动操作。")
                .arg(m_index)
                .arg(m_count));
        show();
    }

    void finishBatch(int index, int count, bool picked)
    {
        m_index = index;
        m_count = count;
        if (picked) {
            setTone(Theme::Success);
            setHeader(QStringLiteral("第 %1 段选片完成").arg(index),
                      QStringLiteral("第 %1/%2 段").arg(index).arg(count),
                      StatusMark::Mode::Check, Theme::Success);
            m_body->setTextColor(Theme::Text2);
        } else {
            setTone(Theme::Text3);
            setHeader(QStringLiteral("第 %1 段未选出切片").arg(index),
                      QStringLiteral("第 %1/%2 段").arg(index).arg(count),
                      StatusMark::Mode::Cross, Theme::Text3, Theme::Text2);
            m_body->setTextColor(Theme::Text3);
        }
        setOutlineBody(QStringLiteral("该段已完成分析。"));
        show();
    }

    void completeAll(int picked)
    {
        setTone(Theme::Success);
        setHeader(QStringLiteral("AI 选片完成"),
                  QStringLiteral("已锁定 %1 条").arg(picked),
                  StatusMark::Mode::Check, Theme::Success);
        m_body->setTextColor(Theme::Text2);
        setOutlineBody(QStringLiteral("全部转写分段已完成分析。"));
        show();
    }

    void fail(const QString &message)
    {
        setTone(Theme::Danger);
        setHeader(QStringLiteral("AI 选片中断"), QString(),
                  StatusMark::Mode::Cross, Theme::Danger, Theme::Danger);
        m_body->setTextColor(Theme::Danger);
        m_body->setMessage(message.isEmpty() ? QStringLiteral("模型请求中断。")
                                             : message);
        show();
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        painter.setPen(QPen(Theme::alpha(m_tone, 0.22), 1.0));
        painter.setBrush(Theme::alpha(m_tone, 0.06));
        painter.drawRoundedRect(QRectF(rect()).adjusted(0.5, 0.5, -0.5, -0.5), 7, 7);
    }

private:
    // 有思考要点时展示结构化列表，否则退回状态说明文案。
    void setOutlineBody(const QString &fallback, bool streaming = false)
    {
        const QStringList points = m_outline.displayPoints();
        if (points.isEmpty()) {
            m_body->setMessage(fallback, streaming);
        } else {
            m_body->setPoints(points, streaming);
        }
    }

    void setTone(const QColor &tone)
    {
        m_tone = tone;
        m_body->setFadeColor(Theme::mix(Theme::Surface1, tone, 0.06));
        m_body->setCursorColor(tone);
        update();
    }

    void setHeader(const QString &title, const QString &meta, StatusMark::Mode mode,
                   const QColor &markColor, const QColor &titleColor = Theme::Text1)
    {
        m_title->setText(title);
        m_title->setTextColor(titleColor);
        m_meta->setText(meta);
        m_mark->setMode(mode);
        m_mark->setColor(markColor);
    }

    StatusMark *m_mark = nullptr;
    ElidedLabel *m_title = nullptr;
    QLabel *m_meta = nullptr;
    ThoughtOutlineView *m_body = nullptr;
    QColor m_tone = Theme::Ai;
    int m_index = 0;
    int m_count = 0;
    int m_totalChars = 0;
    ReasoningOutline m_outline;
    QElapsedTimer m_rateClock;
    bool m_rateStarted = false;
};

// -----------------------------------------------------------------------------
// 候选切片卡片
// -----------------------------------------------------------------------------

class CandidateCard : public QWidget
{
public:
    enum class State { Queued, Analyzing, Picked, Skipped };

    explicit CandidateCard(int index, QWidget *parent = nullptr)
        : QWidget(parent)
        , m_index(index)
    {
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Minimum);
        auto *root = new QVBoxLayout(this);
        root->setContentsMargins(10, 8, 10, 8);
        root->setSpacing(2);

        auto *top = new QHBoxLayout;
        top->setContentsMargins(0, 0, 0, 0);
        top->setSpacing(6);
        m_order = monoLabel(QStringLiteral("%1").arg(index, 2, 10, QLatin1Char('0')),
                            10, Theme::Text3, this);
        m_order->setFixedWidth(18);
        top->addWidget(m_order, 0, Qt::AlignTop);
        m_title = new ElidedLabel(QString(), this);
        m_title->setPixelSize(12);
        m_title->setFontWeight(500);
        m_title->setTextColor(Theme::Text1);
        top->addWidget(m_title, 1);
        m_badge = new Tag(QString(), Theme::Success, Theme::alpha(Theme::Success, 0.13),
                          Theme::alpha(Theme::Success, 0.35), 15, 3, true, 9, 5, 0,
                          this);
        top->addWidget(m_badge, 0, Qt::AlignTop);
        m_spinner = new Spinner(10, Theme::Ai, this);
        top->addWidget(m_spinner, 0, Qt::AlignVCenter);
        m_stateLabel = Ui::label(QStringLiteral("评估中"), 9, 400, Theme::Ai, this);
        m_stateLabel->setFont(Theme::monoFont(9));
        top->addWidget(m_stateLabel, 0, Qt::AlignVCenter);
        root->addLayout(top);

        m_meta = monoLabel(QString(), 9, Theme::Text3, this);
        root->addWidget(m_meta);

        m_reason = new ElidedLabel(QString(), this);
        m_reason->setPixelSize(10);
        m_reason->setTextColor(Theme::Text3);
        root->addWidget(m_reason);

        m_topics = new QWidget(this);
        m_topicsLayout = new QHBoxLayout(m_topics);
        m_topicsLayout->setContentsMargins(0, 2, 0, 0);
        m_topicsLayout->setSpacing(4);
        root->addWidget(m_topics);
        setState(State::Queued);
    }

    int index() const { return m_index; }

    void setQueued(const QString &range, int sentences)
    {
        setState(State::Queued);
        m_title->setText(QStringLiteral("第 %1 段 · 候选窗口").arg(m_index));
        m_title->setTextColor(Theme::Text2);
        m_badge->setText(QStringLiteral("等待评估"));
        m_badge->setStyle(Theme::Text3, Qt::transparent, Theme::Border);
        m_stateLabel->hide();
        m_spinner->hide();
        m_badge->show();
        m_meta->setText(QStringLiteral("%1 · %2 句转写").arg(range).arg(sentences));
        m_reason->hide();
        clearTopics();
    }

    void setAnalyzing(const QString &range, int sentences)
    {
        setState(State::Analyzing);
        m_title->setText(QStringLiteral("第 %1 段 · 正在评估").arg(m_index));
        m_title->setTextColor(Theme::Text2);
        m_badge->hide();
        m_spinner->show();
        m_stateLabel->show();
        m_stateLabel->setText(QStringLiteral("评估中"));
        setLabelColor(m_stateLabel, Theme::Ai);
        m_meta->setText(QStringLiteral("%1 · %2 句转写").arg(range).arg(sentences));
        m_reason->hide();
        clearTopics();
    }

    void setPicked(const PipelineUi::Candidate &candidate)
    {
        setState(State::Picked);
        m_index = candidate.index;
        m_order->setText(QStringLiteral("%1").arg(m_index, 2, 10, QLatin1Char('0')));
        m_title->setText(candidate.title.isEmpty()
                             ? QStringLiteral("直播切片")
                             : candidate.title);
        m_title->setTextColor(Theme::Text1);
        m_badge->setText(QStringLiteral("%1 段 · %2")
                             .arg(qMax(1, candidate.rangeCount))
                             .arg(PipelineUi::formatSpan(candidate.durationMs)));
        m_badge->setStyle(Theme::Success, Theme::alpha(Theme::Success, 0.13),
                          Theme::alpha(Theme::Success, 0.35));
        m_badge->show();
        m_spinner->hide();
        m_stateLabel->hide();
        m_meta->setText(QStringLiteral("%1 · %2 条字幕")
                            .arg(candidate.range)
                            .arg(candidate.captionCount));
        m_reason->setText(candidate.description);
        m_reason->setVisible(!candidate.description.trimmed().isEmpty());
        m_reason->setToolTip(candidate.description);
        clearTopics();
        const int count = qMin(2, candidate.topics.size());
        for (int i = 0; i < count; ++i) {
            auto *tag = makeTag(candidate.topics.at(i), Theme::Ai, false, false, 14,
                                3, 4, m_topics);
            m_topicsLayout->addWidget(tag);
        }
        m_topicsLayout->addStretch(1);
        m_topics->setVisible(count > 0);
    }

    void setSkipped(const QString &reason)
    {
        setState(State::Skipped);
        m_title->setText(QStringLiteral("第 %1 段 · 未选出切片").arg(m_index));
        m_title->setTextColor(Theme::Text3);
        m_badge->hide();
        m_spinner->hide();
        m_stateLabel->hide();
        m_meta->setText(reason);
        m_reason->hide();
        clearTopics();
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        QColor fill = Qt::transparent;
        QColor border = Theme::Border;
        qreal width = 1.0;
        Qt::PenStyle style = Qt::SolidLine;
        if (m_state == State::Analyzing) {
            fill = Theme::alpha(Theme::Ai, 0.13);
            border = Theme::alpha(Theme::Ai, 0.40);
        } else if (m_state == State::Picked) {
            fill = Theme::alpha(Theme::Success, 0.13);
            border = Theme::alpha(Theme::Success, 0.30);
        } else if (m_state == State::Queued) {
            style = Qt::DashLine;
            border = Theme::Border;
        }
        if (fill.alpha() > 0) {
            painter.setBrush(fill);
        } else {
            painter.setBrush(Qt::NoBrush);
        }
        painter.setPen(QPen(border, width, style));
        painter.drawRoundedRect(QRectF(rect()).adjusted(0.5, 0.5, -0.5, -0.5), 7, 7);
    }

private:
    void setState(State state)
    {
        m_state = state;
        update();
    }

    void clearTopics()
    {
        while (QLayoutItem *item = m_topicsLayout->takeAt(0)) {
            if (QWidget *widget = item->widget()) {
                widget->hide();
                widget->deleteLater();
            }
            delete item;
        }
        m_topics->hide();
    }

    int m_index = 0;
    State m_state = State::Queued;
    QLabel *m_order = nullptr;
    ElidedLabel *m_title = nullptr;
    Tag *m_badge = nullptr;
    Spinner *m_spinner = nullptr;
    QLabel *m_stateLabel = nullptr;
    QLabel *m_meta = nullptr;
    ElidedLabel *m_reason = nullptr;
    QWidget *m_topics = nullptr;
    QHBoxLayout *m_topicsLayout = nullptr;
};

// -----------------------------------------------------------------------------
// 草稿数据结构
// -----------------------------------------------------------------------------

struct DraftStepData
{
    int status = int(PipelineUi::StepStatus::Waiting);
    int progress = 0;
    QString detail = QStringLiteral("等待上一步完成");
    qint64 durationMs = 0;
};

struct DraftItemData
{
    PipelineUi::Candidate candidate;
    int status = 0; // 0=排队，1=生成中，2=已完成，3=失败
    int progress = 0;
    bool saved = false;
    QVector<DraftStepData> steps;
};

// -----------------------------------------------------------------------------
// 草稿列表行
// -----------------------------------------------------------------------------

class StepSegments : public QWidget
{
public:
    explicit StepSegments(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        setFixedHeight(2);
    }

    void setSteps(const QVector<DraftStepData> &steps)
    {
        m_steps = steps;
        update();
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        painter.setPen(Qt::NoPen);
        const int count = qMax(1, m_steps.size());
        const qreal gap = 3.0;
        const qreal segment = (width() - gap * (count - 1)) / qreal(count);
        for (int i = 0; i < count; ++i) {
            QColor color = Theme::Surface3;
            if (i < m_steps.size()) {
                const int status = m_steps.at(i).status;
                if (status == int(PipelineUi::StepStatus::Done)) {
                    color = Theme::Success;
                } else if (status == int(PipelineUi::StepStatus::Running)) {
                    color = Theme::Primary;
                } else if (status == int(PipelineUi::StepStatus::Failed)) {
                    color = Theme::Danger;
                }
            }
            painter.setBrush(color);
            painter.drawRoundedRect(QRectF(i * (segment + gap), 0, segment, 2), 1, 1);
        }
    }

private:
    QVector<DraftStepData> m_steps;
};

class RoundBadge : public QWidget
{
public:
    explicit RoundBadge(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        setFixedSize(20, 20);
    }

    void setData(int index, int status)
    {
        m_index = index;
        m_status = status;
        update();
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        if (m_status == 2) {
            painter.setPen(Qt::NoPen);
            painter.setBrush(Theme::Success);
            painter.drawEllipse(QRectF(0, 0, 20, 20));
            Icons::paint(&painter, QRectF(6, 6, 8, 8), Icons::Glyph::Check,
                         Theme::OnAccent, 2.6);
        } else if (m_status == 1) {
            painter.setPen(QPen(Theme::Primary, 1.0));
            painter.setBrush(Theme::Primary);
            painter.drawEllipse(QRectF(0.5, 0.5, 19, 19));
            painter.setPen(Theme::OnAccent);
            painter.setFont(Theme::monoFont(9));
            painter.drawText(rect(), Qt::AlignCenter, QString::number(m_index));
        } else if (m_status == 3) {
            painter.setPen(Qt::NoPen);
            painter.setBrush(Theme::Danger);
            painter.drawEllipse(QRectF(0, 0, 20, 20));
            Icons::paint(&painter, QRectF(6, 6, 8, 8), Icons::Glyph::Close,
                         Theme::OnAccent, 2.6);
        } else {
            painter.setPen(QPen(Theme::BorderStrong, 1.0));
            painter.setBrush(Theme::Surface2);
            painter.drawEllipse(QRectF(0.5, 0.5, 19, 19));
            painter.setPen(Theme::Text3);
            painter.setFont(Theme::monoFont(9));
            painter.drawText(rect(), Qt::AlignCenter, QString::number(m_index));
        }
    }

private:
    int m_index = 1;
    int m_status = 0;
};

class DraftRowWidget : public QWidget
{
public:
    explicit DraftRowWidget(const DraftItemData &item, QWidget *parent = nullptr)
        : QWidget(parent)
        , m_item(item)
    {
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
        setCursor(Qt::PointingHandCursor);
        auto *root = new QVBoxLayout(this);
        root->setContentsMargins(10, 8, 10, 8);
        root->setSpacing(5);

        auto *top = new QHBoxLayout;
        top->setContentsMargins(0, 0, 0, 0);
        top->setSpacing(8);
        m_badge = new RoundBadge(this);
        top->addWidget(m_badge, 0, Qt::AlignVCenter);
        m_title = new ElidedLabel(QString(), this);
        m_title->setPixelSize(12);
        m_title->setFontWeight(500);
        m_title->setTextColor(Theme::Text1);
        top->addWidget(m_title, 1);
        m_percent = monoLabel(QStringLiteral("0%"), 10, Theme::Text3, this);
        top->addWidget(m_percent, 0, Qt::AlignVCenter);
        root->addLayout(top);

        m_segments = new StepSegments(this);
        root->addWidget(m_segments);

        auto *bottom = new QHBoxLayout;
        bottom->setContentsMargins(0, 0, 0, 0);
        bottom->setSpacing(6);
        m_meta = monoLabel(QString(), 9, Theme::Text3, this);
        bottom->addWidget(m_meta);
        bottom->addStretch(1);
        m_status = monoLabel(QString(), 9, Theme::Text3, this);
        bottom->addWidget(m_status);
        root->addLayout(bottom);

        updateData(item);
    }

    void updateData(const DraftItemData &item)
    {
        m_item = item;
        m_badge->setData(item.candidate.index, item.status);
        m_title->setText(item.candidate.title.isEmpty()
                             ? QStringLiteral("直播切片")
                             : item.candidate.title);
        m_title->setTextColor(item.status == 0 ? Theme::Text3 : Theme::Text1);
        m_percent->setText(QStringLiteral("%1%").arg(item.progress));
        setLabelColor(m_percent, item.status == 2
                                     ? Theme::Success
                                     : (item.status == 1 ? Theme::Primary
                                                         : (item.status == 3
                                                                ? Theme::Danger
                                                                : Theme::Text3)));
        m_segments->setSteps(item.steps);
        m_meta->setText(QStringLiteral("%1 · %2")
                            .arg(item.candidate.range,
                                 PipelineUi::formatSpan(item.candidate.durationMs)));
        m_segments->setVisible(!item.steps.isEmpty());
        QString statusText;
        QColor statusColor = Theme::Text3;
        if (item.status == 2) {
            statusText = QStringLiteral("已生成");
            statusColor = Theme::Success;
        } else if (item.status == 1) {
            int active = -1;
            for (int i = 0; i < item.steps.size(); ++i) {
                if (item.steps.at(i).status == int(PipelineUi::StepStatus::Running)) {
                    active = i;
                    break;
                }
            }
            if (active >= 0) {
                const int stepPercent = item.steps.at(active).progress;
                statusText = stepPercent > 0
                                 ? QStringLiteral("%1 %2%")
                                       .arg(stepLabelFor(active))
                                       .arg(stepPercent)
                                 : stepLabelFor(active);
            } else {
                statusText = QStringLiteral("生成中");
            }
            statusColor = Theme::Primary;
        } else if (item.status == 3) {
            statusText = QStringLiteral("生成失败");
            statusColor = Theme::Danger;
        } else {
            statusText = QStringLiteral("排队中");
        }
        m_status->setText(statusText);
        setLabelColor(m_status, statusColor);
        update();
    }

    void setSelected(bool selected)
    {
        m_selected = selected;
        update();
    }

    std::function<void()> onClick;

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        QColor fill = Theme::Surface1;
        QColor border = Theme::Border;
        if (m_selected) {
            fill = Theme::alpha(Theme::Primary, 0.14);
            border = Theme::alpha(Theme::Primary, 0.5);
        } else if (m_hovered) {
            border = Theme::BorderStrong;
        }
        painter.setPen(QPen(border, 1.0));
        painter.setBrush(fill);
        painter.drawRoundedRect(QRectF(rect()).adjusted(0.5, 0.5, -0.5, -0.5), 8, 8);
    }

    void enterEvent(QEnterEvent *event) override
    {
        m_hovered = true;
        update();
        QWidget::enterEvent(event);
    }

    void leaveEvent(QEvent *event) override
    {
        m_hovered = false;
        update();
        QWidget::leaveEvent(event);
    }

    void mousePressEvent(QMouseEvent *event) override
    {
        if (event->button() == Qt::LeftButton && onClick) {
            onClick();
        }
        QWidget::mousePressEvent(event);
    }

private:
    DraftItemData m_item;
    RoundBadge *m_badge = nullptr;
    ElidedLabel *m_title = nullptr;
    QLabel *m_percent = nullptr;
    StepSegments *m_segments = nullptr;
    QLabel *m_meta = nullptr;
    QLabel *m_status = nullptr;
    bool m_selected = false;
    bool m_hovered = false;
};

// -----------------------------------------------------------------------------
// 草稿步骤行
// -----------------------------------------------------------------------------

class StepRow : public QWidget
{
public:
    explicit StepRow(int step, QWidget *parent = nullptr)
        : QWidget(parent)
        , m_step(step)
    {
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Minimum);
        auto *root = new QHBoxLayout(this);
        root->setContentsMargins(10, 6, 10, 6);
        root->setSpacing(10);

        m_icon = new StepIconBadge(this);
        root->addWidget(m_icon, 0, Qt::AlignTop);

        auto *content = new QVBoxLayout;
        content->setContentsMargins(0, 0, 0, 0);
        content->setSpacing(2);

        auto *top = new QHBoxLayout;
        top->setContentsMargins(0, 0, 0, 0);
        top->setSpacing(6);
        m_label = Ui::label(stepLabelFor(step), 12, 400, Theme::Text3, this);
        top->addWidget(m_label);
        top->addStretch(1);
        m_spinner = new Spinner(10, Theme::Primary, this);
        m_spinner->hide();
        top->addWidget(m_spinner, 0, Qt::AlignVCenter);
        m_status = monoLabel(QStringLiteral("排队"), 9, Theme::Text3, this);
        top->addWidget(m_status);
        content->addLayout(top);

        m_detail = Ui::label(QStringLiteral("等待上一步完成"), 11, 400, Theme::Text3, this);
        m_detail->setWordWrap(true);
        m_detail->setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Minimum);
        content->addWidget(m_detail);

        m_progress = new StepProgress(this);
        m_progress->hide();
        content->addWidget(m_progress);
        root->addLayout(content, 1);

        setData(int(PipelineUi::StepStatus::Waiting), 0,
                QStringLiteral("等待上一步完成"), 0);
    }

    void setData(int status, int percent, const QString &detail, qint64 durationMs)
    {
        m_statusValue = status;
        m_icon->setState(status, stepGlyphFor(m_step));

        const bool running = status == int(PipelineUi::StepStatus::Running);
        const bool done = status == int(PipelineUi::StepStatus::Done);
        const bool failed = status == int(PipelineUi::StepStatus::Failed);

        QFont labelFont = Theme::font(12, running ? 600 : 400);
        m_label->setFont(labelFont);
        setLabelColor(m_label, running ? Theme::Text1
                                       : (done ? Theme::Text1 : Theme::Text3));
        m_detail->setText(detail);
        setLabelColor(m_detail, running ? Theme::Text2 : Theme::Text3);

        m_spinner->setVisible(running);
        m_progress->setVisible(running);
        m_progress->setState(running, percent);

        if (running) {
            m_status->setText(percent > 0 ? QStringLiteral("%1%").arg(percent)
                                          : QStringLiteral("进行中"));
            setLabelColor(m_status, Theme::Primary);
        } else if (done) {
            m_status->setText(QStringLiteral("✓ %1s")
                                  .arg(QString::number(qMax<qint64>(0, durationMs) / 1000.0,
                                                       'f', 1)));
            setLabelColor(m_status, Theme::Success);
            m_status->show();
        } else if (failed) {
            m_status->setText(QStringLiteral("失败"));
            setLabelColor(m_status, Theme::Danger);
            m_status->show();
        } else {
            m_status->setText(QStringLiteral("排队"));
            setLabelColor(m_status, Theme::Text3);
            m_status->show();
        }
        update();
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        QColor fill = Theme::Surface1;
        QColor border = Theme::alpha(Theme::Border, 0.7);
        if (m_statusValue == int(PipelineUi::StepStatus::Running)) {
            fill = Theme::alpha(Theme::Primary, 0.14);
            border = Theme::alpha(Theme::Primary, 0.40);
        } else if (m_statusValue == int(PipelineUi::StepStatus::Failed)) {
            fill = Theme::alpha(Theme::Danger, 0.13);
            border = Theme::alpha(Theme::Danger, 0.35);
        } else if (m_statusValue == int(PipelineUi::StepStatus::Waiting)) {
            fill = Theme::alpha(Theme::Surface1, 0.5);
        }
        painter.setPen(QPen(border, 1.0));
        painter.setBrush(fill);
        painter.drawRoundedRect(QRectF(rect()).adjusted(0.5, 0.5, -0.5, -0.5), 7, 7);
    }

private:
    int m_step = 0;
    int m_statusValue = 0;
    StepIconBadge *m_icon = nullptr;
    QLabel *m_label = nullptr;
    QLabel *m_detail = nullptr;
    QLabel *m_status = nullptr;
    Spinner *m_spinner = nullptr;
    StepProgress *m_progress = nullptr;
};

class GlyphIcon : public QWidget
{
public:
    GlyphIcon(Icons::Glyph glyph, int side, const QColor &color, QWidget *parent = nullptr)
        : QWidget(parent)
        , m_glyph(glyph)
        , m_color(color)
    {
        setFixedSize(side, side);
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        Icons::paint(&painter, QRectF(rect()), m_glyph, m_color, 1.8);
    }

private:
    Icons::Glyph m_glyph;
    QColor m_color;
};

// -----------------------------------------------------------------------------
// ASR 面板：整片识别
// -----------------------------------------------------------------------------

enum class AsrPhase { Prepare, Recognize, Result, Done, Failed };
enum class AsrRowState { Pending, Active, Done, Failed };

constexpr int kAsrResultPhaseMs = 900;

static QString etaClock(qint64 etaMs)
{
    const qint64 seconds = qMax<qint64>(0, (etaMs + 999) / 1000);
    return formatElapsedValue(seconds * 1000);
}

static QString confidenceValue(double confidence)
{
    return confidence >= 0.0 ? QString::number(confidence, 'f', 1)
                             : QStringLiteral("—");
}

static QString tenThousandCount(int chars)
{
    return QStringLiteral("%1 万字").arg(QString::number(chars / 10000.0, 'f', 1));
}

static Tag *pillTag(const QString &text, const QColor &foreground,
                    const QColor &background, const QColor &border, QWidget *parent)
{
    return new Tag(text, foreground, background, border, 15, 8, true, 9, 6, 0, parent);
}

// 阶段图标：转圈、脉冲点、对勾、失败与闪电，供 ASR 子标题使用。
class PhaseMark : public QWidget
{
public:
    enum class Mode { Spinner, Dot, Check, Cross, Bolt };

    explicit PhaseMark(int side = 10, QWidget *parent = nullptr)
        : QWidget(parent)
    {
        setFixedSize(side, side);
        m_timer = new QTimer(this);
        m_timer->setInterval(60);
        connect(m_timer, &QTimer::timeout, this, [this] {
            m_angle = (m_angle + 30) % 360;
            m_step = (m_step + 1) % 120;
            update();
        });
    }

    void setMode(Mode mode, const QColor &color)
    {
        m_mode = mode;
        m_color = color;
        update();
    }

protected:
    void showEvent(QShowEvent *event) override
    {
        m_timer->start();
        QWidget::showEvent(event);
    }

    void hideEvent(QHideEvent *event) override
    {
        m_timer->stop();
        QWidget::hideEvent(event);
    }

    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        const QRectF box = QRectF(rect()).adjusted(1, 1, -1, -1);
        switch (m_mode) {
        case Mode::Check:
            Icons::paint(&painter, box, Icons::Glyph::Check, m_color, 2.2);
            break;
        case Mode::Cross:
            Icons::paint(&painter, box, Icons::Glyph::Close, m_color, 2.0);
            break;
        case Mode::Bolt:
            Icons::paint(&painter, box, Icons::Glyph::Bolt, m_color, 1.8);
            break;
        case Mode::Dot: {
            const qreal pulse = 0.55 + 0.45 * std::sin(m_step / 120.0 * 2.0 * kPi);
            QColor color = m_color;
            color.setAlphaF(0.35 + 0.65 * pulse);
            painter.setPen(Qt::NoPen);
            painter.setBrush(color);
            painter.drawEllipse(QPointF(rect().center()), 3.0, 3.0);
            break;
        }
        case Mode::Spinner: {
            const qreal side = qMin(width(), height()) - 2.0;
            const QRectF arc((width() - side) / 2.0, (height() - side) / 2.0, side, side);
            painter.setPen(QPen(Theme::BorderStrong, 1.4));
            painter.drawEllipse(arc);
            painter.setPen(QPen(m_color, 1.4, Qt::SolidLine, Qt::RoundCap));
            painter.drawArc(arc, int((90 - m_angle) * 16), 100 * 16);
            break;
        }
        }
    }

private:
    Mode m_mode = Mode::Spinner;
    QColor m_color = Theme::Primary;
    QTimer *m_timer = nullptr;
    int m_angle = 0;
    int m_step = 0;
};

// 子标题的浅色底栏，底边用 1px 分隔线。
class PanelStrip : public QWidget
{
public:
    explicit PanelStrip(const QColor &fill, QWidget *parent = nullptr)
        : QWidget(parent)
        , m_fill(fill)
    {
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.fillRect(rect(), m_fill);
        painter.setPen(QPen(Theme::Border, 1.0));
        painter.drawLine(QPointF(0, height() - 0.5),
                         QPointF(width(), height() - 0.5));
    }

private:
    QColor m_fill;
};

// 整段音频波形：底图 + 已处理进度 + 发光播放头；准备/解析阶段带遮罩。
class WaveformView : public QWidget
{
public:
    explicit WaveformView(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        setFixedHeight(124);
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
        m_bars.resize(96);
        for (int i = 0; i < m_bars.size(); ++i) {
            const qreal envelope =
                std::fabs(std::sin(i * 0.37)) * 0.42
                + std::fabs(std::sin(i * 0.11 + 1.2)) * 0.34;
            const qreal grain = ((i * 37) % 13) / 13.0;
            m_bars[i] = qMin<qreal>(1.0, 0.18 + envelope + grain * 0.16);
        }
        m_timer = new QTimer(this);
        m_timer->setInterval(60);
        connect(m_timer, &QTimer::timeout, this, [this] {
            m_angle = (m_angle + 30) % 360;
            m_step = (m_step + 1) % 120;
            update();
        });
    }

    void setState(AsrPhase phase, qreal progress)
    {
        m_phase = phase;
        m_progress = qBound<qreal>(0.0, progress, 100.0);
        update();
    }

protected:
    void showEvent(QShowEvent *event) override
    {
        m_timer->start();
        QWidget::showEvent(event);
    }

    void hideEvent(QHideEvent *event) override
    {
        m_timer->stop();
        QWidget::hideEvent(event);
    }

    void paintEvent(QPaintEvent *) override
    {
        if (width() <= 0) {
            return;
        }
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);

        const qreal gap = 2.0;
        const qreal barWidth =
            qMax<qreal>(1.0, (width() - gap * (m_bars.size() - 1)) / m_bars.size());
        const qreal radius = qMin<qreal>(barWidth / 2.0, 3.0);

        const auto drawBars = [&](const QColor &color, const QLinearGradient *gradient) {
            painter.setPen(Qt::NoPen);
            painter.setBrush(gradient ? QBrush(*gradient) : QBrush(color));
            for (int i = 0; i < m_bars.size(); ++i) {
                const qreal h = m_bars.at(i) * height();
                const QRectF bar(i * (barWidth + gap), (height() - h) / 2.0, barWidth,
                                 h);
                painter.drawRoundedRect(bar, radius, radius);
            }
        };

        drawBars(Theme::Surface3, nullptr);

        const qreal processedWidth = width() * m_progress / 100.0;
        if (processedWidth > 0.5) {
            QLinearGradient gradient(0, height(), 0, 0);
            gradient.setColorAt(0.0, Theme::alpha(Theme::Primary, 0.35));
            gradient.setColorAt(0.5, Theme::alpha(Theme::Primary, 0.70));
            gradient.setColorAt(1.0, Theme::Primary);
            painter.save();
            painter.setClipRect(QRectF(0, 0, processedWidth, height()));
            drawBars(Theme::Primary, &gradient);
            painter.restore();
        }

        // 播放头：外侧柔光 + 1px 实线 + 顶部脉冲点。
        const qreal x = processedWidth;
        painter.setPen(QPen(Theme::alpha(Theme::Primary, 0.28), 6.0));
        painter.drawLine(QPointF(x, 2), QPointF(x, height() - 2));
        painter.setPen(QPen(Theme::Primary, 1.0));
        painter.drawLine(QPointF(x, 0), QPointF(x, height()));
        QColor dotColor = Theme::Primary;
        dotColor.setAlphaF(0.55 + 0.45 * std::sin(m_step / 120.0 * 2.0 * kPi));
        painter.setPen(Qt::NoPen);
        painter.setBrush(dotColor);
        painter.drawEllipse(QPointF(x, 1.0), 3.5, 3.5);

        if (m_phase != AsrPhase::Prepare && m_phase != AsrPhase::Result) {
            return;
        }

        const bool preparing = m_phase == AsrPhase::Prepare;
        painter.fillRect(rect(), Theme::alpha(Theme::Window, preparing ? 0.75 : 0.65));

        const QString text = preparing ? QStringLiteral("正在提取音轨并提交识别任务…")
                                       : QStringLiteral("识别完成，正在解析转写结果…");
        const QColor accent = preparing ? Theme::Primary : Theme::Success;
        const QFont font = Theme::font(10);
        const QFontMetrics metrics(font);
        const qreal textWidth = metrics.horizontalAdvance(text);
        const qreal pillWidth = 12 + 10 + 8 + textWidth + 12;
        const qreal pillHeight = 24;
        const QRectF pill((width() - pillWidth) / 2.0,
                          (height() - pillHeight) / 2.0, pillWidth, pillHeight);

        painter.setPen(Qt::NoPen);
        painter.setBrush(QColor(0, 0, 0, 100));
        painter.drawRoundedRect(pill.translated(0, 4), 12, 12);

        painter.setBrush(Theme::alpha(Theme::Surface1, 0.95));
        painter.setPen(QPen(preparing ? Theme::Border : Theme::alpha(Theme::Success, 0.3),
                            1.0));
        painter.drawRoundedRect(pill, 12, 12);

        const QRectF arc(pill.left() + 12, pill.center().y() - 5, 10, 10);
        painter.setPen(QPen(Theme::alpha(accent, 0.28), 1.5));
        painter.drawEllipse(arc);
        painter.setPen(QPen(accent, 1.5, Qt::SolidLine, Qt::RoundCap));
        painter.drawArc(arc, int((90 - m_angle) * 16), 100 * 16);

        painter.setFont(font);
        painter.setPen(preparing ? Theme::Text2 : Theme::Success);
        painter.drawText(QRectF(pill.left() + 30, pill.top(), textWidth + 1, pillHeight),
                         Qt::AlignLeft | Qt::AlignVCenter, text);
    }

private:
    QVector<qreal> m_bars;
    QTimer *m_timer = nullptr;
    AsrPhase m_phase = AsrPhase::Prepare;
    qreal m_progress = 0.0;
    int m_angle = 0;
    int m_step = 0;
};

class EventRowWidget : public QWidget
{
public:
    explicit EventRowWidget(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Minimum);
        auto *root = new QHBoxLayout(this);
        root->setContentsMargins(8, 4, 8, 4);
        root->setSpacing(8);

        m_mark = new StatusMark(StatusMark::Mode::Ring, 12, Theme::Text3, this);
        root->addWidget(m_mark, 0, Qt::AlignTop);

        auto *column = new QVBoxLayout;
        column->setContentsMargins(0, 0, 0, 0);
        column->setSpacing(2);

        auto *titleRow = new QHBoxLayout;
        titleRow->setContentsMargins(0, 0, 0, 0);
        titleRow->setSpacing(8);
        m_title = Ui::label(QString(), 11, 400, Theme::Text2, this);
        titleRow->addWidget(m_title, 1);
        m_right = monoLabel(QString(), 9, Theme::Text3, this);
        setLabelColor(m_right, Theme::Text3);
        titleRow->addWidget(m_right, 0, Qt::AlignTop);
        column->addLayout(titleRow);

        m_detail = Ui::label(QString(), 10, 400, Theme::Text3, this);
        m_detail->setWordWrap(true);
        setLabelColor(m_detail, Theme::Text3);
        column->addWidget(m_detail);
        root->addLayout(column, 1);
    }

    void setRow(AsrRowState state, const QString &title, const QString &detail,
                const QString &right)
    {
        const bool stateChanged = !m_hasState || m_state != state;
        m_hasState = true;
        m_state = state;
        m_title->setText(title);
        m_detail->setText(detail);
        m_detail->setVisible(!detail.isEmpty());
        m_right->setText(right);
        m_right->setVisible(!right.isEmpty());
        if (!stateChanged) {
            return;
        }

        m_title->setFont(Theme::font(11, state == AsrRowState::Active ? 500 : 400));
        const QColor titleColor =
            state == AsrRowState::Active
                ? Theme::Text1
                : (state == AsrRowState::Pending ? Theme::Text3
                                                 : (state == AsrRowState::Failed
                                                        ? Theme::Danger
                                                        : Theme::Text2));
        setLabelColor(m_title, titleColor);
        setLabelColor(m_right,
                      state == AsrRowState::Failed ? Theme::Danger : Theme::Text3);

        switch (state) {
        case AsrRowState::Done:
            m_mark->setMode(StatusMark::Mode::Check);
            m_mark->setColor(Theme::Success);
            break;
        case AsrRowState::Active:
            m_mark->setMode(StatusMark::Mode::Spinner);
            m_mark->setColor(Theme::Primary);
            break;
        case AsrRowState::Failed:
            m_mark->setMode(StatusMark::Mode::Cross);
            m_mark->setColor(Theme::Danger);
            break;
        case AsrRowState::Pending:
            m_mark->setMode(StatusMark::Mode::Ring);
            m_mark->setColor(Theme::Text3);
            break;
        }
        update();
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        if (m_state != AsrRowState::Active && m_state != AsrRowState::Failed) {
            return;
        }
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        painter.setPen(Qt::NoPen);
        painter.setBrush(m_state == AsrRowState::Failed
                             ? Theme::alpha(Theme::Danger, 0.07)
                             : Theme::alpha(Theme::Primary, 0.07));
        painter.drawRoundedRect(QRectF(rect()), 5, 5);
    }

private:
    AsrRowState m_state = AsrRowState::Pending;
    bool m_hasState = false;
    StatusMark *m_mark = nullptr;
    QLabel *m_title = nullptr;
    QLabel *m_detail = nullptr;
    QLabel *m_right = nullptr;
};

class EventLogWidget : public QWidget
{
public:
    explicit EventLogWidget(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        auto *root = new QVBoxLayout(this);
        root->setContentsMargins(6, 6, 6, 6);
        root->setSpacing(1);

        auto *header = new QWidget(this);
        auto *headerLayout = new QHBoxLayout(header);
        headerLayout->setContentsMargins(6, 2, 6, 4);
        headerLayout->setSpacing(6);
        headerLayout->addWidget(
            Ui::label(QStringLiteral("任务动态"), 11, 500, Theme::Text2, header));
        headerLayout->addStretch(1);
        m_headerMark = new StatusMark(StatusMark::Mode::Dot, 10, Theme::Success, header);
        headerLayout->addWidget(m_headerMark);
        m_headerLabel = monoLabel(QStringLiteral("提交后每 2 秒查询"), 9, Theme::Text3,
                                  header);
        headerLayout->addWidget(m_headerLabel);
        root->addWidget(header);

        for (int i = 0; i < 6; ++i) {
            m_rows[i] = new EventRowWidget(this);
            root->addWidget(m_rows[i]);
        }
    }

    EventRowWidget *row(int index) { return m_rows[qBound(0, index, 5)]; }

    void setHeaderState(AsrPhase phase)
    {
        if (phase == AsrPhase::Result) {
            m_headerMark->setMode(StatusMark::Mode::Check);
            m_headerMark->setColor(Theme::Success);
            m_headerLabel->setText(QStringLiteral("识别已返回 · 正在解析"));
            setLabelColor(m_headerLabel, Theme::Success);
        } else if (phase == AsrPhase::Failed) {
            m_headerMark->setMode(StatusMark::Mode::Cross);
            m_headerMark->setColor(Theme::Danger);
            m_headerLabel->setText(QStringLiteral("已停止"));
            setLabelColor(m_headerLabel, Theme::Danger);
        } else {
            m_headerMark->setMode(StatusMark::Mode::Dot);
            m_headerMark->setColor(Theme::Success);
            m_headerLabel->setText(phase == AsrPhase::Prepare
                                       ? QStringLiteral("提交后每 2 秒查询")
                                       : QStringLiteral("每 2 秒自动查询"));
            setLabelColor(m_headerLabel, Theme::Text3);
        }
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        painter.setPen(QPen(Theme::Border, 1.0));
        painter.setBrush(Theme::mix(Theme::Surface1, Theme::Window, 0.6));
        painter.drawRoundedRect(QRectF(rect()).adjusted(0.5, 0.5, -0.5, -0.5), 8, 8);
    }

private:
    StatusMark *m_headerMark = nullptr;
    QLabel *m_headerLabel = nullptr;
    EventRowWidget *m_rows[6] = {};
};

// 进行中的进度条带一条移动高光，与设计稿的 progress-live 一致。
class SweepProgressBar : public QWidget
{
public:
    explicit SweepProgressBar(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        setFixedHeight(4);
        setMinimumWidth(40);
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
        m_timer = new QTimer(this);
        m_timer->setInterval(40);
        connect(m_timer, &QTimer::timeout, this, [this] {
            m_phase = (m_phase + 29) % 1000;
            update();
        });
    }

    void setValue(int percent)
    {
        m_value = qBound(0, percent, 100);
        update();
    }

    void setColor(const QColor &color)
    {
        m_color = color;
        update();
    }

    void setBusy(bool busy)
    {
        m_busy = busy;
        if (m_busy && isVisible()) {
            m_timer->start();
        } else {
            m_timer->stop();
        }
        update();
    }

    QSize sizeHint() const override { return QSize(100, 4); }

protected:
    void showEvent(QShowEvent *event) override
    {
        if (m_busy) {
            m_timer->start();
        }
        QWidget::showEvent(event);
    }

    void hideEvent(QHideEvent *event) override
    {
        m_timer->stop();
        QWidget::hideEvent(event);
    }

    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        const QRectF track(0, 0, width(), 4);
        painter.setPen(Qt::NoPen);
        painter.setBrush(Theme::Surface3);
        painter.drawRoundedRect(track, 2, 2);

        if (m_value <= 0) {
            return;
        }
        const qreal filled = qMin<qreal>(width(), qMax<qreal>(4.0, width() * m_value / 100.0));
        painter.setBrush(m_color);
        painter.drawRoundedRect(QRectF(0, 0, filled, 4), 2, 2);

        if (!m_busy) {
            return;
        }
        QPainterPath clip;
        clip.addRoundedRect(QRectF(0, 0, filled, 4), 2, 2);
        painter.save();
        painter.setClipPath(clip);
        const qreal bandWidth = width() * 0.45;
        const qreal bandX = (filled + bandWidth) * (m_phase / 1000.0) - bandWidth;
        QLinearGradient gradient(bandX, 0, bandX + bandWidth, 0);
        gradient.setColorAt(0.0, Qt::transparent);
        gradient.setColorAt(0.5, QColor(255, 255, 255, 115));
        gradient.setColorAt(1.0, Qt::transparent);
        painter.fillRect(QRectF(bandX, 0, bandWidth, 4), gradient);
        painter.restore();
    }

private:
    QTimer *m_timer = nullptr;
    int m_value = 0;
    int m_phase = 0;
    bool m_busy = false;
    QColor m_color = Theme::Primary;
};

class ProgressCard : public Card
{
public:
    explicit ProgressCard(QWidget *parent = nullptr)
        : Card(Theme::Surface1, Theme::Border, 8, parent)
    {
        auto *layout = new QVBoxLayout;
        layout->setContentsMargins(12, 12, 12, 12);
        layout->setSpacing(8);

        auto *head = new QHBoxLayout;
        head->setContentsMargins(0, 0, 0, 0);
        head->setSpacing(6);
        head->addWidget(Ui::label(QStringLiteral("转写进度"), 11, 500, Theme::Text2, this));
        m_phaseChip = pillTag(QStringLiteral("准备中"), Theme::Text3, Qt::transparent,
                              Theme::Border, this);
        head->addWidget(m_phaseChip);
        m_estimateChip = pillTag(QStringLiteral("估算"), Theme::Text3, Qt::transparent,
                                 Theme::Border, this);
        head->addWidget(m_estimateChip);
        head->addStretch(1);
        m_percent = monoLabel(QStringLiteral("0"), 18, Theme::Primary, this);
        m_percent->setFont(Theme::monoFont(18, 650));
        head->addWidget(m_percent, 0, Qt::AlignBottom);
        head->addWidget(Ui::label(QStringLiteral("%"), 10, 400, Theme::Text3, this), 0,
                        Qt::AlignBottom);
        layout->addLayout(head);

        m_bar = new SweepProgressBar(this);
        layout->addWidget(m_bar);

        m_stack = new QStackedLayout;
        m_stack->setContentsMargins(0, 0, 0, 0);

        m_waitPage = new QWidget(this);
        auto *waitLayout = new QVBoxLayout(m_waitPage);
        waitLayout->setContentsMargins(0, 0, 0, 0);
        waitLayout->setSpacing(8);

        auto *processedRow = new QHBoxLayout;
        processedRow->setContentsMargins(0, 0, 0, 0);
        processedRow->setSpacing(8);
        m_processed = monoLabel(QStringLiteral("已处理约 00:00:00 / 00:00:00"), 10,
                                Theme::Text3, m_waitPage);
        processedRow->addWidget(m_processed);
        processedRow->addStretch(1);
        m_phaseText = monoLabel(QStringLiteral("正在提交任务"), 10, Theme::Text3,
                                m_waitPage);
        processedRow->addWidget(m_phaseText);
        waitLayout->addLayout(processedRow);

        auto *timeRow = new QHBoxLayout;
        timeRow->setContentsMargins(0, 0, 0, 0);
        timeRow->setSpacing(4);
        timeRow->addWidget(Ui::label(QStringLiteral("已等待"), 10, 400, Theme::Text3,
                                     m_waitPage));
        m_waited = monoLabel(QStringLiteral("00:00"), 10, Theme::Text2, m_waitPage);
        m_waited->setFont(Theme::monoFont(10, 500));
        timeRow->addWidget(m_waited);
        timeRow->addStretch(1);
        timeRow->addWidget(Ui::label(QStringLiteral("预计还需"), 10, 400, Theme::Text3,
                                     m_waitPage));
        m_eta = monoLabel(QStringLiteral("—"), 10, Theme::Text2, m_waitPage);
        m_eta->setFont(Theme::monoFont(10, 500));
        timeRow->addWidget(m_eta);
        waitLayout->addLayout(timeRow);

        auto *noteCard = new Card(Theme::alpha(Theme::Surface2, 0.5), Theme::Border, 5,
                                  m_waitPage);
        auto *noteLayout = new QVBoxLayout;
        noteLayout->setContentsMargins(8, 6, 8, 6);
        m_note = Ui::label(QString(), 10, 400, Theme::Text3, noteCard);
        m_note->setWordWrap(true);
        noteLayout->addWidget(m_note);
        noteCard->contentLayout()->addLayout(noteLayout);
        waitLayout->addWidget(noteCard);

        auto *hintRow = new QHBoxLayout;
        hintRow->setContentsMargins(0, 0, 0, 0);
        hintRow->setSpacing(5);
        hintRow->addWidget(new GlyphIcon(Icons::Glyph::Bolt, 10, Theme::Ai, m_waitPage));
        hintRow->addWidget(Ui::label(QStringLiteral("可切到其他页面，完成后通知你"), 10,
                                     400, Theme::Text3, m_waitPage));
        hintRow->addStretch(1);
        waitLayout->addLayout(hintRow);
        m_stack->addWidget(m_waitPage);

        m_donePage = new QWidget(this);
        auto *doneLayout = new QVBoxLayout(m_donePage);
        doneLayout->setContentsMargins(0, 0, 0, 0);
        doneLayout->setSpacing(8);

        auto *metrics = new QGridLayout;
        metrics->setContentsMargins(0, 0, 0, 0);
        metrics->setHorizontalSpacing(6);
        metrics->setVerticalSpacing(6);
        static const char *metricNames[] = {"转写句数", "识别字数", "平均置信度", "说话人"};
        for (int i = 0; i < 4; ++i) {
            auto *box = new Card(Theme::Surface2, Theme::Border, 5, m_donePage);
            auto *boxLayout = new QVBoxLayout;
            boxLayout->setContentsMargins(8, 6, 8, 6);
            boxLayout->setSpacing(1);
            boxLayout->addWidget(Ui::label(QString::fromUtf8(metricNames[i]), 9, 400,
                                           Theme::Text3, box));
            m_metricValues[i] = monoLabel(QStringLiteral("—"), 11, Theme::Text1, box);
            boxLayout->addWidget(m_metricValues[i]);
            box->contentLayout()->addLayout(boxLayout);
            metrics->addWidget(box, i / 2, i % 2);
        }
        doneLayout->addLayout(metrics);

        auto *successCard = new Card(Theme::alpha(Theme::Success, 0.13),
                                     Theme::alpha(Theme::Success, 0.25), 5, m_donePage);
        auto *successLayout = new QHBoxLayout;
        successLayout->setContentsMargins(8, 4, 8, 4);
        successLayout->setSpacing(6);
        successLayout->addWidget(
            new StatusMark(StatusMark::Mode::Check, 10, Theme::Success, successCard));
        m_successLabel = Ui::label(QStringLiteral("转写完成"), 10, 400, Theme::Success,
                                   successCard);
        successLayout->addWidget(m_successLabel);
        successLayout->addStretch(1);
        successCard->contentLayout()->addLayout(successLayout);
        doneLayout->addWidget(successCard);

        auto *doneHint = Ui::label(QStringLiteral("已交给 AI 选片，无需手动操作。"), 10,
                                   400, Theme::Text3, m_donePage);
        doneLayout->addWidget(doneHint);
        m_stack->addWidget(m_donePage);

        layout->addLayout(m_stack);
        contentLayout()->addLayout(layout);
        setPhase(AsrPhase::Prepare);
    }

    void setPhase(AsrPhase phase)
    {
        m_phase = phase;
        QString text;
        QColor foreground = Theme::Text3;
        QColor background = Qt::transparent;
        QColor border = Theme::Border;
        QColor accent = Theme::Primary;
        switch (phase) {
        case AsrPhase::Prepare:
            text = QStringLiteral("准备中");
            break;
        case AsrPhase::Recognize:
            text = QStringLiteral("识别中");
            foreground = Theme::Primary;
            background = Theme::alpha(Theme::Primary, 0.14);
            border = Theme::alpha(Theme::Primary, 0.35);
            break;
        case AsrPhase::Result:
            text = QStringLiteral("解析结果");
            foreground = Theme::Ai;
            background = Theme::alpha(Theme::Ai, 0.13);
            border = Theme::alpha(Theme::Ai, 0.35);
            break;
        case AsrPhase::Done:
            text = QStringLiteral("已完成");
            foreground = Theme::Success;
            background = Theme::alpha(Theme::Success, 0.13);
            border = Theme::alpha(Theme::Success, 0.35);
            accent = Theme::Success;
            break;
        case AsrPhase::Failed:
            text = QStringLiteral("识别失败");
            foreground = Theme::Danger;
            background = Theme::alpha(Theme::Danger, 0.13);
            border = Theme::alpha(Theme::Danger, 0.35);
            accent = Theme::Danger;
            break;
        }
        m_phaseChip->setText(text);
        m_phaseChip->setStyle(foreground, background, border);
        m_estimateChip->setVisible(phase == AsrPhase::Prepare
                                   || phase == AsrPhase::Recognize);
        m_stack->setCurrentWidget(phase == AsrPhase::Done ? m_donePage : m_waitPage);
        m_bar->setBusy(phase != AsrPhase::Done && phase != AsrPhase::Failed);
        m_bar->setColor(accent);
        setLabelColor(m_percent, accent);

        if (phase == AsrPhase::Result) {
            setNote(QStringLiteral("识别结果已返回，正在解析逐句时间轴与说话人；随后直接交给 AI 选片。"),
                    Theme::Text3);
        } else if (phase == AsrPhase::Prepare || phase == AsrPhase::Recognize) {
            setNote(QStringLiteral("进度按音频时长与服务速度估算，实际以服务返回为准；完成后一次性返回整稿。"),
                    Theme::Text3);
        }
    }

    void setProgress(int percent)
    {
        m_percent->setText(QString::number(qBound(0, percent, 100)));
        m_bar->setValue(percent);
    }

    void setWaiting(qint64 estimatedSeconds, qint64 totalMs, qint64 elapsedMs,
                    qint64 etaMs, bool etaKnown)
    {
        m_processed->setText(
            QStringLiteral("已处理约 %1 / %2")
                .arg(PipelineUi::formatClock(estimatedSeconds * 1000),
                     PipelineUi::formatClock(qMax<qint64>(0, totalMs))));
        m_waited->setText(formatElapsedValue(elapsedMs));
        m_eta->setText(etaKnown ? etaClock(etaMs) : QStringLiteral("—"));
        m_phaseText->setText(m_phase == AsrPhase::Prepare
                                 ? QStringLiteral("正在提交任务")
                                 : (m_phase == AsrPhase::Result
                                        ? QStringLiteral("正在解析结果")
                                        : QStringLiteral("服务端处理中")));
    }

    void setDone(int sentences, int chars, double confidence, int speakers,
                 qint64 elapsedMs)
    {
        m_metricValues[0]->setText(QStringLiteral("%1 句").arg(formatNumber(sentences)));
        m_metricValues[1]->setText(tenThousandCount(chars));
        m_metricValues[2]->setText(
            QStringLiteral("%1%").arg(confidenceValue(confidence)));
        m_metricValues[3]->setText(
            speakers > 0 ? QStringLiteral("%1 人").arg(speakers) : QStringLiteral("—"));
        m_successLabel->setText(
            QStringLiteral("转写完成 · 用时 %1").arg(formatElapsedValue(elapsedMs)));
    }

    void setError(const QString &message)
    {
        m_phase = AsrPhase::Failed;
        setNote(message.isEmpty() ? QStringLiteral("转写失败") : message, Theme::Danger);
        m_phaseText->setText(QStringLiteral("已停止"));
        m_stack->setCurrentWidget(m_waitPage);
    }

private:
    void setNote(const QString &text, const QColor &color)
    {
        m_note->setText(text);
        setLabelColor(m_note, color);
    }

    AsrPhase m_phase = AsrPhase::Prepare;
    Tag *m_phaseChip = nullptr;
    Tag *m_estimateChip = nullptr;
    QLabel *m_percent = nullptr;
    SweepProgressBar *m_bar = nullptr;
    QStackedLayout *m_stack = nullptr;
    QWidget *m_waitPage = nullptr;
    QWidget *m_donePage = nullptr;
    QLabel *m_processed = nullptr;
    QLabel *m_phaseText = nullptr;
    QLabel *m_waited = nullptr;
    QLabel *m_eta = nullptr;
    QLabel *m_note = nullptr;
    QLabel *m_metricValues[4] = {};
    QLabel *m_successLabel = nullptr;
};

class PollCard : public Card
{
public:
    explicit PollCard(QWidget *parent = nullptr)
        : Card(Theme::Surface1, Theme::Border, 8, parent)
    {
        auto *layout = new QVBoxLayout;
        layout->setContentsMargins(12, 12, 12, 12);
        layout->setSpacing(8);

        auto *head = new QHBoxLayout;
        head->setContentsMargins(0, 0, 0, 0);
        head->setSpacing(6);
        head->addWidget(Ui::label(QStringLiteral("服务轮询"), 11, 500, Theme::Text2, this));
        head->addStretch(1);
        m_statusMark = new StatusMark(StatusMark::Mode::Dot, 10, Theme::Success, this);
        head->addWidget(m_statusMark);
        m_statusLabel = monoLabel(QStringLiteral("正常"), 9, Theme::Success, this);
        head->addWidget(m_statusLabel);
        layout->addLayout(head);

        auto *rows = new QVBoxLayout;
        rows->setContentsMargins(0, 0, 0, 0);
        rows->setSpacing(4);
        static const char *labels[] = {"识别接口", "轮询间隔", "上次查询", "已查询",
                                       "自动重试"};
        for (int i = 0; i < 5; ++i) {
            auto *row = new QHBoxLayout;
            row->setContentsMargins(0, 0, 0, 0);
            row->setSpacing(8);
            row->addWidget(Ui::label(QString::fromUtf8(labels[i]), 10, 400, Theme::Text3,
                                     this));
            row->addStretch(1);
            m_values[i] = monoLabel(QStringLiteral("—"), 10, Theme::Text2, this);
            row->addWidget(m_values[i]);
            rows->addLayout(row);
        }
        m_values[0]->setText(QStringLiteral("同步 · 整片返回"));
        m_values[1]->setText(QStringLiteral("每 2 秒"));
        layout->addLayout(rows);

        auto *line = Ui::separator(false, this);
        layout->addWidget(line);

        m_note = Ui::label(QString(), 10, 400, Theme::Text3, this);
        m_note->setWordWrap(true);
        layout->addWidget(m_note);
        contentLayout()->addLayout(layout);
    }

    void setState(AsrPhase phase, int pollCount, qint64 lastPollMs)
    {
        QString status;
        QColor statusColor = Theme::Success;
        if (phase == AsrPhase::Done) {
            status = QStringLiteral("已完成");
        } else if (phase == AsrPhase::Result) {
            status = QStringLiteral("已返回");
        } else if (phase == AsrPhase::Failed) {
            status = QStringLiteral("已停止");
            statusColor = Theme::Danger;
        } else {
            status = QStringLiteral("正常");
        }
        m_statusLabel->setText(status);
        setLabelColor(m_statusLabel, statusColor);
        m_statusMark->setColor(statusColor);

        QString lastPoll;
        if (phase == AsrPhase::Prepare || phase == AsrPhase::Failed) {
            lastPoll = QStringLiteral("—");
        } else if (phase == AsrPhase::Recognize) {
            lastPoll = pollCount == 0
                           ? QStringLiteral("等待首次")
                           : (lastPollMs < 400
                                  ? QStringLiteral("刚刚")
                                  : QStringLiteral("%1s 前")
                                        .arg(QString::number(lastPollMs / 1000.0, 'f',
                                                             1)));
        } else {
            lastPoll = QStringLiteral("已停止");
        }
        m_values[2]->setText(lastPoll);
        m_values[3]->setText(QStringLiteral("%1 次").arg(pollCount));
        m_values[4]->setText(QStringLiteral("0 次"));

        QString note;
        if (phase == AsrPhase::Done) {
            note = QStringLiteral("识别任务已完成，状态轮询已停止。");
        } else if (phase == AsrPhase::Result) {
            note = QStringLiteral("识别结果已返回，状态轮询已停止；正在解析转写稿。");
        } else if (phase == AsrPhase::Failed) {
            note = QStringLiteral("识别任务已停止，可重新发起一键成片。");
        } else if (phase == AsrPhase::Prepare) {
            note = QStringLiteral("提交后会每 2 秒确认任务状态；识别任务在服务端进行，可切到其他页面。");
        } else {
            note = QStringLiteral("识别任务在服务端进行；可切到其他页面，完成后通知你。");
        }
        m_note->setText(note);
    }

private:
    StatusMark *m_statusMark = nullptr;
    QLabel *m_statusLabel = nullptr;
    QLabel *m_values[5] = {};
    QLabel *m_note = nullptr;
};

class AudioCard : public Card
{
public:
    explicit AudioCard(QWidget *parent = nullptr)
        : Card(Theme::Surface1, Theme::Border, 8, parent)
    {
        auto *layout = new QVBoxLayout;
        layout->setContentsMargins(12, 12, 12, 12);
        layout->setSpacing(8);

        auto *head = new QHBoxLayout;
        head->setContentsMargins(0, 0, 0, 0);
        head->setSpacing(6);
        head->addWidget(Ui::label(QStringLiteral("音频信息"), 11, 500, Theme::Text2, this));
        head->addStretch(1);
        head->addWidget(pillTag(QStringLiteral("中文"), Theme::Text3, Qt::transparent,
                                Theme::Border, this));
        layout->addLayout(head);

        auto *rows = new QVBoxLayout;
        rows->setContentsMargins(0, 0, 0, 0);
        rows->setSpacing(4);
        static const char *labels[] = {"时长", "采样率", "识别模式", "输出", "任务 ID"};
        for (int i = 0; i < 5; ++i) {
            auto *row = new QHBoxLayout;
            row->setContentsMargins(0, 0, 0, 0);
            row->setSpacing(8);
            row->addWidget(Ui::label(QString::fromUtf8(labels[i]), 10, 400, Theme::Text3,
                                     this));
            row->addStretch(1);
            m_values[i] = monoLabel(QStringLiteral("—"), 10, Theme::Text2, this);
            row->addWidget(m_values[i]);
            rows->addLayout(row);
        }
        m_values[1]->setText(QStringLiteral("16 kHz · 单声道"));
        m_values[2]->setText(QStringLiteral("整片识别"));
        m_values[3]->setText(QStringLiteral("逐句时间轴 + 标点"));
        layout->addLayout(rows);

        auto *line = Ui::separator(false, this);
        layout->addWidget(line);

        auto *footer = new QHBoxLayout;
        footer->setContentsMargins(0, 0, 0, 0);
        footer->setSpacing(5);
        footer->addWidget(new GlyphIcon(Icons::Glyph::Sparkle, 10, Theme::Ai, this));
        footer->addWidget(Ui::label(QStringLiteral("转写完成后自动进入 AI 选片"), 10, 400,
                                    Theme::Text3, this));
        footer->addStretch(1);
        layout->addLayout(footer);
        contentLayout()->addLayout(layout);
    }

    void setInfo(qint64 totalMs, const QString &jobId)
    {
        m_values[0]->setText(PipelineUi::formatClock(qMax<qint64>(0, totalMs)));
        m_values[4]->setText(jobId.isEmpty() ? QStringLiteral("—") : jobId);
    }

private:
    QLabel *m_values[5] = {};
};

class AsrPanelWidget : public QWidget
{
public:
    explicit AsrPanelWidget(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        auto *root = new QHBoxLayout(this);
        root->setContentsMargins(kSidePadding, 12, kSidePadding, 12);
        root->setSpacing(kPanelGap);

        // 左侧：等待过程 / 转写稿
        auto *card = new Card(Theme::Surface1, Theme::Border, 9, this);
        auto *cardLayout = new QVBoxLayout;
        cardLayout->setContentsMargins(0, 0, 0, 0);
        cardLayout->setSpacing(0);

        auto *header = new QWidget(card);
        auto *headerLayout = new QHBoxLayout(header);
        headerLayout->setContentsMargins(12, 7, 12, 7);
        headerLayout->setSpacing(8);
        m_headerMark = new StatusMark(StatusMark::Mode::Spinner, 12, Theme::Primary, header);
        headerLayout->addWidget(m_headerMark);
        m_headerTitle = Ui::label(QStringLiteral("ASR 转写"), 12, 500, Theme::Text1,
                                  header);
        headerLayout->addWidget(m_headerTitle);
        m_headerChip = pillTag(QStringLiteral("整片识别"), Theme::Text3, Qt::transparent,
                               Theme::Border, header);
        headerLayout->addWidget(m_headerChip);
        headerLayout->addStretch(1);
        m_headerMeta = monoLabel(QStringLiteral("已等待 00:00"), 10, Theme::Text3, header);
        headerLayout->addWidget(m_headerMeta);
        cardLayout->addWidget(header);
        cardLayout->addWidget(makeHLine(card));

        m_subBar = new PanelStrip(Theme::alpha(Theme::Surface2, 0.4), card);
        auto *subLayout = new QHBoxLayout(m_subBar);
        subLayout->setContentsMargins(12, 6, 12, 6);
        subLayout->setSpacing(8);
        m_subMark = new PhaseMark(10, m_subBar);
        subLayout->addWidget(m_subMark);
        m_subTitle = Ui::label(QStringLiteral("正在提取音轨并提交识别任务…"), 10, 400,
                               Theme::Text3, m_subBar);
        subLayout->addWidget(m_subTitle, 1);
        m_subRight = monoLabel(QString(), 9, Theme::Text3, m_subBar);
        subLayout->addWidget(m_subRight);
        cardLayout->addWidget(m_subBar);

        m_body = new QWidget(card);
        m_bodyStack = new QStackedLayout(m_body);
        m_bodyStack->setContentsMargins(0, 0, 0, 0);

        auto *waitPage = new QWidget(m_body);
        auto *waitLayout = new QVBoxLayout(waitPage);
        waitLayout->setContentsMargins(0, 0, 0, 0);
        waitLayout->setSpacing(0);

        auto *waitScroll = new QScrollArea(waitPage);
        waitScroll->setFrameShape(QFrame::NoFrame);
        waitScroll->setWidgetResizable(true);
        waitScroll->setHorizontalScrollBarPolicy(Qt::ScrollBarAlwaysOff);
        waitScroll->setStyleSheet(QStringLiteral(
            "QScrollArea { background:transparent; border:none; }"
            "QScrollArea > QWidget > QWidget { background:transparent; }"));
        auto *waitHost = new QWidget(waitScroll);
        auto *waitHostLayout = new QVBoxLayout(waitHost);
        waitHostLayout->setContentsMargins(12, 12, 12, 12);
        waitHostLayout->setSpacing(12);

        auto *waveCard = new Card(Theme::Window, Theme::Border, 8, waitHost);
        auto *waveLayout = new QVBoxLayout;
        waveLayout->setContentsMargins(0, 0, 0, 0);
        waveLayout->setSpacing(0);
        auto *waveHead = new QWidget(waveCard);
        auto *waveHeadLayout = new QHBoxLayout(waveHead);
        waveHeadLayout->setContentsMargins(12, 8, 12, 0);
        waveHeadLayout->setSpacing(8);
        waveHeadLayout->addWidget(monoLabel(QStringLiteral("音频波形 · 整片识别"), 9,
                                            Theme::Text3, waveHead));
        waveHeadLayout->addStretch(1);
        waveHeadLayout->addWidget(monoLabel(QStringLiteral("16 kHz · 单声道"), 9,
                                            Theme::Text3, waveHead));
        waveLayout->addWidget(waveHead);

        m_wave = new WaveformView(waveCard);
        auto *waveWrap = new QWidget(waveCard);
        auto *waveWrapLayout = new QHBoxLayout(waveWrap);
        waveWrapLayout->setContentsMargins(12, 8, 12, 0);
        waveWrapLayout->addWidget(m_wave);
        waveLayout->addWidget(waveWrap);

        auto *times = new QWidget(waveCard);
        auto *timesLayout = new QHBoxLayout(times);
        timesLayout->setContentsMargins(12, 6, 12, 10);
        timesLayout->setSpacing(0);
        for (int i = 0; i < 5; ++i) {
            m_waveTimes[i] = monoLabel(QStringLiteral("00:00:00"), 9, Theme::Text3, times);
            if (i == 0) {
                timesLayout->addWidget(m_waveTimes[i], 0, Qt::AlignLeft);
            } else if (i == 4) {
                timesLayout->addWidget(m_waveTimes[i], 0, Qt::AlignRight);
            } else {
                timesLayout->addWidget(m_waveTimes[i], 1, Qt::AlignHCenter);
            }
        }
        waveLayout->addWidget(times);
        waveCard->contentLayout()->addLayout(waveLayout);
        waitHostLayout->addWidget(waveCard);
        // 顶部细线放在时间行上方，保持与设计稿一致。
        auto *waveLineWrap = new QWidget(waveCard);
        auto *waveLineLayout = new QHBoxLayout(waveLineWrap);
        waveLineLayout->setContentsMargins(12, 8, 12, 0);
        waveLineLayout->setSpacing(0);
        auto *waveLine = new QLabel(waveLineWrap);
        waveLine->setFixedHeight(1);
        waveLine->setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
        waveLine->setStyleSheet(QStringLiteral("background: rgba(36,41,56,0.6);"));
        waveLineLayout->addWidget(waveLine);
        waveLayout->insertWidget(waveLayout->count() - 1, waveLineWrap);

        m_eventLog = new EventLogWidget(waitHost);
        waitHostLayout->addWidget(m_eventLog, 1);
        waitScroll->setWidget(waitHost);
        waitLayout->addWidget(waitScroll);
        m_bodyStack->addWidget(waitPage);

        m_transcriptScroll = new QScrollArea(m_body);
        m_transcriptScroll->setFrameShape(QFrame::NoFrame);
        m_transcriptScroll->setWidgetResizable(true);
        m_transcriptScroll->setHorizontalScrollBarPolicy(Qt::ScrollBarAlwaysOff);
        m_transcriptScroll->setStyleSheet(QStringLiteral(
            "QScrollArea { background:transparent; border:none; }"
            "QScrollArea > QWidget > QWidget { background:transparent; }"));
        m_transcript = new TranscriptView(m_transcriptScroll);
        m_transcriptScroll->setWidget(m_transcript);
        m_bodyStack->addWidget(m_transcriptScroll);
        m_bodyStack->setCurrentIndex(0);
        cardLayout->addWidget(m_body, 1);

        cardLayout->addWidget(makeHLine(card));
        auto *footer = new QWidget(card);
        auto *footerLayout = new QHBoxLayout(footer);
        footerLayout->setContentsMargins(12, 5, 12, 5);
        footerLayout->setSpacing(6);
        m_footerLeft = monoLabel(QStringLiteral("任务 — · 整片识别"), 9, Theme::Text3,
                                 footer);
        footerLayout->addWidget(m_footerLeft);
        footerLayout->addStretch(1);
        m_footerRight = monoLabel(QStringLiteral("可切到其他页面，完成后通知你"), 9,
                                  Theme::Text3, footer);
        footerLayout->addWidget(m_footerRight);
        cardLayout->addWidget(footer);
        card->contentLayout()->addLayout(cardLayout);
        root->addWidget(card, 1);

        // 右侧：过程数据
        auto *sideScroll = new QScrollArea(this);
        sideScroll->setFixedWidth(300);
        sideScroll->setFrameShape(QFrame::NoFrame);
        sideScroll->setWidgetResizable(true);
        sideScroll->setHorizontalScrollBarPolicy(Qt::ScrollBarAlwaysOff);
        sideScroll->setStyleSheet(QStringLiteral(
            "QScrollArea { background:transparent; border:none; }"
            "QScrollArea > QWidget > QWidget { background:transparent; }"));
        auto *sideHost = new QWidget(sideScroll);
        auto *sideLayout = new QVBoxLayout(sideHost);
        sideLayout->setContentsMargins(0, 0, 0, 0);
        sideLayout->setSpacing(10);
        m_progressCard = new ProgressCard(sideHost);
        m_pollCard = new PollCard(sideHost);
        m_audioCard = new AudioCard(sideHost);
        sideLayout->addWidget(m_progressCard);
        sideLayout->addWidget(m_pollCard);
        sideLayout->addWidget(m_audioCard);
        sideLayout->addStretch(1);
        sideScroll->setWidget(sideHost);
        root->addWidget(sideScroll);

        m_ticker = new QTimer(this);
        m_ticker->setInterval(80);
        connect(m_ticker, &QTimer::timeout, this, [this] { tick(); });

        reset();
    }

    void reset()
    {
        m_phase = AsrPhase::Prepare;
        m_durationMs = 0;
        m_jobId.clear();
        m_prepareNote = QStringLiteral("正在提取音轨并提交识别任务…");
        m_cached = false;
        m_finished = false;
        m_failed = false;
        m_errorMessage.clear();
        m_sentences = 0;
        m_chars = 0;
        m_confidence = -1.0;
        m_speakers = 0;
        m_progress = 0;
        m_lastNotifiedPercent = -1;
        m_prepareStep = 0;
        m_extractStartMs = 0;
        m_extractDoneMs = -1;
        m_submitStartMs = -1;
        m_submitDoneMs = -1;
        m_elapsedBaseMs = 0;
        m_finalElapsedMs = 0;
        m_summaryMode = false;
        m_summaryPercent = 0;
        m_recognizeClock.invalidate();
        m_resultClock.invalidate();
        m_asrClock.start();
        m_transcript->clear();
        m_wave->setState(AsrPhase::Prepare, 0);
        m_progressCard->setPhase(AsrPhase::Prepare);
        m_progressCard->setProgress(0);
        m_pollCard->setState(AsrPhase::Prepare, 0, 0);
        m_audioCard->setInfo(0, QString());
        for (int i = 0; i < 5; ++i) {
            m_waveTimes[i]->setText(QStringLiteral("00:00:00"));
        }
        updateEventLog();
        refreshPhase();
        updateWaitingUi();
        m_ticker->start();
    }

    void handlePrepared(int step, qint64 durationMs, bool cached, const QString &note,
                        const QString &jobId)
    {
        if (!jobId.isEmpty()) {
            m_jobId = jobId;
        }
        m_durationMs = qMax(m_durationMs, qMax<qint64>(0, durationMs));
        m_cached = m_cached || cached;
        if (step >= 0) {
            m_prepareStep = qMax(m_prepareStep, step);
        }
        if (step == 0 && m_extractDoneMs < 0) {
            m_extractStartMs = elapsedMs();
        } else if (step >= 1 && m_extractDoneMs < 0) {
            m_extractDoneMs = elapsedMs();
            m_submitStartMs = m_extractDoneMs;
        }
        if (!note.isEmpty()) {
            m_prepareNote = note;
        }
        if (m_phase == AsrPhase::Prepare) {
            refreshPhase();
        }
        updateWaveTimes();
        m_audioCard->setInfo(m_durationMs, m_jobId);
        updateWaitingUi();
        notifyChanged();
    }

    void handleSubmitted(const QString &jobId, qint64 durationMs)
    {
        if (!jobId.isEmpty()) {
            m_jobId = jobId;
        }
        m_durationMs = qMax(m_durationMs, qMax<qint64>(0, durationMs));
        if (m_extractDoneMs < 0) {
            m_extractDoneMs = elapsedMs();
        }
        m_submitStartMs = m_extractDoneMs;
        m_submitDoneMs = elapsedMs();
        m_phase = AsrPhase::Recognize;
        m_recognizeClock.start();
        refreshPhase();
        m_audioCard->setInfo(m_durationMs, m_jobId);
        updateWaitingUi();
        notifyChanged();
    }

    void handleResult(const PipelineUi::Event &event)
    {
        m_phase = AsrPhase::Result;
        m_sentences = qMax(0, event.count);
        m_chars = qMax(0, event.charCount);
        m_confidence = event.confidence;
        m_speakers = qMax(0, event.speakerCount);
        if (!event.jobId.isEmpty()) {
            m_jobId = event.jobId;
        }
        m_resultClock.start();
        refreshPhase();
        updateWaitingUi();
        notifyChanged();
    }

    void handleFinished(const PipelineUi::Event &event)
    {
        const qint64 finalElapsed = elapsedMs();
        m_finished = true;
        m_failed = false;
        m_phase = AsrPhase::Done;
        m_sentences = qMax(0, event.count);
        m_chars = qMax(0, event.charCount);
        m_confidence = event.confidence;
        m_speakers = qMax(0, event.speakerCount);
        if (!event.jobId.isEmpty()) {
            m_jobId = event.jobId;
        }
        m_durationMs = qMax(m_durationMs, qMax<qint64>(0, event.durationMs));
        m_finalElapsedMs = finalElapsed;
        m_progress = 100;
        m_transcript->setLines(event.lines);
        if (m_sentences <= 0) {
            m_sentences = m_transcript->lineCount();
        }
        m_ticker->stop();
        refreshPhase();
        m_progressCard->setDone(m_sentences, m_chars, m_confidence, m_speakers,
                                m_finalElapsedMs);
        m_progressCard->setProgress(100);
        m_audioCard->setInfo(m_durationMs, m_jobId);
        m_pollCard->setState(m_phase, 0, 0);
        QTimer::singleShot(0, this, [this] {
            if (m_transcriptScroll && m_transcriptScroll->verticalScrollBar()) {
                m_transcriptScroll->verticalScrollBar()->setValue(0);
            }
        });
        notifyChanged();
    }

    void handleFailed(const QString &message)
    {
        const qint64 finalElapsed = elapsedMs();
        m_failed = true;
        m_phase = AsrPhase::Failed;
        m_errorMessage = message;
        m_finalElapsedMs = finalElapsed;
        m_ticker->stop();
        refreshPhase();
        m_progressCard->setPhase(AsrPhase::Failed);
        m_progressCard->setError(message.isEmpty() ? QStringLiteral("转写失败") : message);
        m_pollCard->setState(AsrPhase::Failed, 0, 0);
        m_audioCard->setInfo(m_durationMs, m_jobId);
        updateEventLog();
        notifyChanged();
    }

    void setTaskSummary(int percent, const QString &note, qint64 elapsedMsValue)
    {
        m_summaryMode = true;
        m_summaryPercent = qBound(0, percent, 100);
        m_progress = m_summaryPercent;
        m_lastNotifiedPercent = m_progress;
        m_phase = AsrPhase::Recognize;
        m_prepareNote = note;
        m_elapsedBaseMs = qMax<qint64>(0, elapsedMsValue);
        m_asrClock.start();
        m_recognizeClock.start();
        m_prepareStep = 2;
        m_ticker->start();
        refreshPhase();
        m_progressCard->setPhase(AsrPhase::Recognize);
        updateWaitingUi();
        notifyChanged();
    }

    void setMarkers(const QVector<PipelineUi::Candidate> &markers)
    {
        m_transcript->setMarkers(markers);
    }

    int percent() const
    {
        if (m_summaryMode) {
            return m_summaryPercent;
        }
        return m_progress;
    }

    int sentences() const { return m_sentences; }
    qint64 durationMs() const { return m_durationMs; }
    bool finished() const { return m_finished; }
    bool failed() const { return m_failed; }
    bool cached() const { return m_cached; }
    AsrPhase phase() const { return m_phase; }
    double confidence() const { return m_confidence; }
    int speakers() const { return m_speakers; }
    qint64 elapsed() const { return elapsedMs(); }

    std::function<void()> onChanged;

private:
    qint64 elapsedMs() const
    {
        if (m_finished || m_failed) {
            return m_finalElapsedMs;
        }
        const qint64 live = m_asrClock.isValid() ? m_asrClock.elapsed() : 0;
        return m_elapsedBaseMs + live;
    }

    qint64 recognizeElapsedMs() const
    {
        return m_recognizeClock.isValid() ? m_recognizeClock.elapsed() : 0;
    }

    qint64 resultElapsedMs() const
    {
        return m_resultClock.isValid() ? m_resultClock.elapsed() : 0;
    }

    qint64 estimatedRecognizeMs() const
    {
        return PipelineUi::estimateAsrRecognizeMs(m_durationMs);
    }

    int computePercent() const
    {
        if (m_phase == AsrPhase::Done) {
            return 100;
        }
        if (m_phase == AsrPhase::Failed) {
            return m_progress;
        }
        if (m_phase == AsrPhase::Result) {
            const qreal ratio =
                qBound<qreal>(0.0, resultElapsedMs() / qreal(kAsrResultPhaseMs), 1.0);
            return 96 + int(ratio * 4.0);
        }
        if (m_phase == AsrPhase::Recognize) {
            // 前端估算曲线：前期先给出可见进展，再逐步逼近 96%；
            // 即使实际耗时超过估算，进度仍单调缓慢增长，不会卡死在 100%。
            const qreal estimate = qMax<qreal>(1.0, estimatedRecognizeMs());
            const qreal ratio = qMax<qreal>(0.0, recognizeElapsedMs() / estimate);
            return int((1.0 - std::exp(-3.0 * ratio)) * 96.0);
        }
        return 0;
    }

    bool etaKnown() const
    {
        return m_phase == AsrPhase::Recognize || m_phase == AsrPhase::Result;
    }

    qint64 etaMs() const
    {
        if (m_phase == AsrPhase::Recognize) {
            return qMax<qint64>(0, estimatedRecognizeMs() - recognizeElapsedMs())
                + kAsrResultPhaseMs;
        }
        if (m_phase == AsrPhase::Result) {
            return qMax<qint64>(0, kAsrResultPhaseMs - resultElapsedMs());
        }
        return 0;
    }

    void tick()
    {
        if (m_phase == AsrPhase::Done || m_phase == AsrPhase::Failed) {
            return;
        }
        if (!m_summaryMode) {
            m_progress = computePercent();
        } else {
            m_progress = m_summaryPercent;
        }
        updateWaitingUi();
        if (m_progress != m_lastNotifiedPercent) {
            m_lastNotifiedPercent = m_progress;
            notifyChanged();
        }
    }

    void updateWaitingUi()
    {
        if (m_phase == AsrPhase::Done) {
            return;
        }
        const qint64 totalElapsed = elapsedMs();
        const qint64 estimatedSeconds =
            qint64((m_durationMs / 1000.0) * m_progress / 100.0);
        m_progressCard->setProgress(m_progress);
        m_progressCard->setWaiting(estimatedSeconds, m_durationMs, totalElapsed,
                                   etaMs(), etaKnown());
        // 网关是同步接口，没有服务端任务状态可查；“轮询”表示过程页每 2 秒
        // 确认一次本地识别任务仍在进行，用于维持等待反馈。
        m_pollCard->setState(m_phase, int(recognizeElapsedMs() / 2000),
                             recognizeElapsedMs() % 2000);
        m_wave->setState(m_phase, m_progress);
        if (m_phase != AsrPhase::Failed) {
            m_headerMeta->setText(
                QStringLiteral("已等待 %1").arg(formatElapsedValue(totalElapsed)));
        }
        m_footerLeft->setText(QStringLiteral("任务 %1 · 整片识别")
                                  .arg(m_jobId.isEmpty() ? QStringLiteral("—")
                                                         : m_jobId));
        updateEventLog();
    }

    void updateWaveTimes()
    {
        const qint64 totalSeconds = m_durationMs / 1000;
        const qreal ratios[5] = {0.0, 0.25, 0.5, 0.75, 1.0};
        for (int i = 0; i < 5; ++i) {
            m_waveTimes[i]->setText(
                PipelineUi::formatClock(qint64(totalSeconds * ratios[i]) * 1000));
        }
    }

    qreal resultRatio() const
    {
        if (m_phase == AsrPhase::Done) {
            return 1.0;
        }
        if (m_phase != AsrPhase::Result) {
            return 0.0;
        }
        return qBound<qreal>(0.0, resultElapsedMs() / qreal(kAsrResultPhaseMs), 1.0);
    }

    void updateEventLog()
    {
        const qint64 totalElapsed = elapsedMs();
        const bool extracted =
            m_phase == AsrPhase::Recognize || m_phase == AsrPhase::Result
            || m_phase == AsrPhase::Done || m_prepareStep >= 1;
        const bool submitted =
            m_phase == AsrPhase::Recognize || m_phase == AsrPhase::Result
            || m_phase == AsrPhase::Done;
        const bool recognizeDone =
            m_phase == AsrPhase::Result || m_phase == AsrPhase::Done;

        AsrRowState extractState = AsrRowState::Active;
        QString extractRight = formatElapsedValue(totalElapsed - m_extractStartMs);
        if (extracted) {
            extractState = AsrRowState::Done;
            extractRight = m_extractDoneMs >= 0
                               ? formatElapsedValue(m_extractDoneMs - m_extractStartMs)
                               : formatElapsedValue(totalElapsed);
        } else if (m_phase == AsrPhase::Failed) {
            extractState = AsrRowState::Failed;
            extractRight = QStringLiteral("已停止");
        }
        m_eventLog->row(0)->setRow(
            extractState, QStringLiteral("提取音轨"),
            QStringLiteral("%1 · 16 kHz · 单声道")
                .arg(PipelineUi::formatClock(m_durationMs)),
            extractRight);

        QString submitRight;
        AsrRowState submitState = AsrRowState::Pending;
        if (submitted) {
            submitState = AsrRowState::Done;
            if (m_submitStartMs >= 0 && m_submitDoneMs >= m_submitStartMs) {
                submitRight = formatElapsedValue(m_submitDoneMs - m_submitStartMs);
            }
        } else if (m_prepareStep >= 1) {
            submitState = m_phase == AsrPhase::Failed ? AsrRowState::Failed
                                                      : AsrRowState::Active;
            submitRight = m_phase == AsrPhase::Failed
                              ? QStringLiteral("已停止")
                              : formatElapsedValue(totalElapsed - m_submitStartMs);
        }
        m_eventLog->row(1)->setRow(
            submitState, QStringLiteral("提交识别任务"),
            QStringLiteral("%1 · 整片识别")
                .arg(m_jobId.isEmpty() ? QStringLiteral("—") : m_jobId),
            submitRight);

        AsrRowState recognizeState = AsrRowState::Pending;
        QString recognizeDetail = QStringLiteral("等待服务接收");
        QString recognizeRight;
        if (recognizeDone) {
            recognizeState = AsrRowState::Done;
            recognizeDetail =
                QStringLiteral("%1 句 · %2% 置信度")
                    .arg(formatNumber(m_sentences), confidenceValue(m_confidence));
            recognizeRight = QStringLiteral("已完成");
        } else if (m_phase == AsrPhase::Recognize) {
            recognizeState = AsrRowState::Active;
            const int pollCount = int(recognizeElapsedMs() / 2000);
            recognizeDetail =
                m_summaryMode && !m_prepareNote.isEmpty()
                    ? m_prepareNote
                    : (pollCount == 0
                           ? QStringLiteral("等待首次状态查询")
                           : QStringLiteral("状态查询 #%1 · 服务正常").arg(pollCount));
            recognizeRight = formatElapsedValue(totalElapsed);
        } else if (m_phase == AsrPhase::Failed && m_submitDoneMs >= 0) {
            recognizeState = AsrRowState::Failed;
            recognizeDetail = m_errorMessage.isEmpty() ? QStringLiteral("识别失败")
                                                       : m_errorMessage;
            recognizeRight = QStringLiteral("已停止");
        }
        m_eventLog->row(2)->setRow(
            recognizeState,
            recognizeDone ? QStringLiteral("整片识别完成")
                          : QStringLiteral("整片识别中"),
            recognizeDetail, recognizeRight);

        const qreal ratio = resultRatio();
        const auto ratioState = [&ratio, recognizeDone](qreal threshold,
                                                        qreal activeThreshold) {
            if (!recognizeDone) {
                return AsrRowState::Pending;
            }
            if (ratio >= threshold) {
                return AsrRowState::Done;
            }
            if (ratio >= activeThreshold) {
                return AsrRowState::Active;
            }
            return AsrRowState::Pending;
        };
        const auto ratioRight = [](AsrRowState state) {
            if (state == AsrRowState::Done) {
                return QStringLiteral("完成");
            }
            if (state == AsrRowState::Active) {
                return QStringLiteral("进行中");
            }
            return QStringLiteral("待完成");
        };

        const AsrRowState parseState = ratioState(0.35, 0.0);
        m_eventLog->row(3)->setRow(parseState, QStringLiteral("解析逐句时间轴"),
                                    QStringLiteral("逐句时间戳与标点"),
                                    ratioRight(parseState));

        AsrRowState diarizeState = ratioState(0.7, 0.35);
        QString diarizeDetail = QStringLiteral("区分主播 / 助播");
        QString diarizeRight = ratioRight(diarizeState);
        if (recognizeDone && m_speakers <= 0) {
            diarizeState = AsrRowState::Pending;
            diarizeDetail = QStringLiteral("服务未返回说话人信息");
            diarizeRight = QStringLiteral("未提供");
        }
        m_eventLog->row(4)->setRow(diarizeState, QStringLiteral("说话人分离"),
                                    diarizeDetail, diarizeRight);

        const AsrRowState handoffState = ratioState(1.0, 0.7);
        m_eventLog->row(5)->setRow(handoffState, QStringLiteral("生成 AI 选片输入"),
                                    QStringLiteral("转写稿交给 AI 选片"),
                                    ratioRight(handoffState));
        m_eventLog->setHeaderState(m_phase);
    }

    void refreshPhase()
    {
        if (m_phase == AsrPhase::Prepare) {
            m_headerMark->setMode(StatusMark::Mode::Spinner);
            m_headerMark->setColor(Theme::Primary);
            m_headerTitle->setText(QStringLiteral("ASR 转写"));
            m_headerChip->setText(QStringLiteral("整片识别"));
            m_headerChip->setStyle(Theme::Text3, Qt::transparent, Theme::Border);
            m_subMark->setMode(PhaseMark::Mode::Spinner, Theme::Primary);
            m_subTitle->setText(m_prepareNote);
            m_subRight->setText(QString());
            m_bodyStack->setCurrentIndex(0);
            m_footerRight->setText(QStringLiteral("可切到其他页面，完成后通知你"));
        } else if (m_phase == AsrPhase::Recognize) {
            m_headerMark->setMode(StatusMark::Mode::Dot);
            m_headerMark->setColor(Theme::Primary);
            m_headerTitle->setText(QStringLiteral("ASR 转写"));
            m_headerChip->setText(QStringLiteral("整片识别"));
            m_headerChip->setStyle(Theme::Text3, Qt::transparent, Theme::Border);
            m_subMark->setMode(PhaseMark::Mode::Bolt, Theme::Ai);
            m_subTitle->setText(
                m_summaryMode && !m_prepareNote.isEmpty()
                    ? m_prepareNote
                    : QStringLiteral("同步接口整片返回，完成前无逐句结果"));
            m_subRight->setText(QStringLiteral("每 2 秒查询状态"));
            m_bodyStack->setCurrentIndex(0);
            m_footerRight->setText(QStringLiteral("可切到其他页面，完成后通知你"));
        } else if (m_phase == AsrPhase::Result) {
            m_headerMark->setMode(StatusMark::Mode::Check);
            m_headerMark->setColor(Theme::Success);
            m_headerTitle->setText(QStringLiteral("ASR 转写"));
            m_headerChip->setText(QStringLiteral("整片识别"));
            m_headerChip->setStyle(Theme::Text3, Qt::transparent, Theme::Border);
            m_subMark->setMode(PhaseMark::Mode::Spinner, Theme::Success);
            m_subTitle->setText(QStringLiteral("识别完成，正在解析逐句时间轴与说话人…"));
            m_subRight->setText(QString());
            m_bodyStack->setCurrentIndex(0);
            m_footerRight->setText(QStringLiteral("可切到其他页面，完成后通知你"));
        } else if (m_phase == AsrPhase::Done) {
            m_headerMark->setMode(StatusMark::Mode::Check);
            m_headerMark->setColor(Theme::Success);
            m_headerTitle->setText(QStringLiteral("转写稿"));
            m_headerChip->setText(QStringLiteral("整片返回"));
            m_headerChip->setStyle(Theme::Text3, Qt::transparent, Theme::Border);
            m_headerMeta->setText(
                QStringLiteral("%1 句 · %2")
                    .arg(formatNumber(m_sentences), tenThousandCount(m_chars)));
            m_subMark->setMode(PhaseMark::Mode::Check, Theme::Success);
            m_subTitle->setText(
                m_cached ? QStringLiteral("转写结果来自本地缓存，已交给 AI 选片")
                         : QStringLiteral("转写完成，已交给 AI 选片"));
            m_subRight->setText(
                QStringLiteral("用时 %1").arg(formatElapsedValue(m_finalElapsedMs)));
            m_bodyStack->setCurrentIndex(1);
            m_footerLeft->setText(
                QStringLiteral("音频 %1 · 中文 · 16 kHz")
                    .arg(PipelineUi::formatClock(m_durationMs)));
            m_footerRight->setText(
                QStringLiteral("说话人 %1 · 置信度 %2%")
                    .arg(m_speakers > 0 ? QString::number(m_speakers)
                                        : QStringLiteral("—"),
                         confidenceValue(m_confidence)));
        } else {
            m_headerMark->setMode(StatusMark::Mode::Cross);
            m_headerMark->setColor(Theme::Danger);
            m_headerTitle->setText(QStringLiteral("ASR 转写"));
            m_headerChip->setText(QStringLiteral("识别失败"));
            m_headerChip->setStyle(Theme::Danger, Theme::alpha(Theme::Danger, 0.13),
                                   Theme::alpha(Theme::Danger, 0.35));
            m_subMark->setMode(PhaseMark::Mode::Cross, Theme::Danger);
            m_subTitle->setText(m_errorMessage.isEmpty() ? QStringLiteral("转写失败")
                                                         : m_errorMessage);
            m_subRight->setText(QString());
            m_headerMeta->setText(
                QStringLiteral("已等待 %1").arg(formatElapsedValue(m_finalElapsedMs)));
            m_bodyStack->setCurrentIndex(0);
            m_footerRight->setText(QStringLiteral("转写失败，可重新发起一键成片"));
        }
        m_progressCard->setPhase(m_phase);
        m_eventLog->setHeaderState(m_phase);
        updateWaveTimes();
        updateEventLog();
    }

    void notifyChanged()
    {
        if (onChanged) {
            onChanged();
        }
    }

    AsrPhase m_phase = AsrPhase::Prepare;
    qint64 m_durationMs = 0;
    QString m_jobId;
    QString m_prepareNote;
    QString m_errorMessage;
    bool m_cached = false;
    bool m_finished = false;
    bool m_failed = false;
    int m_sentences = 0;
    int m_chars = 0;
    double m_confidence = -1.0;
    int m_speakers = 0;
    int m_progress = 0;
    int m_lastNotifiedPercent = -1;
    int m_prepareStep = 0;
    qint64 m_extractStartMs = 0;
    qint64 m_extractDoneMs = -1;
    qint64 m_submitStartMs = -1;
    qint64 m_submitDoneMs = -1;
    qint64 m_elapsedBaseMs = 0;
    qint64 m_finalElapsedMs = 0;
    bool m_summaryMode = false;
    int m_summaryPercent = 0;

    QElapsedTimer m_asrClock;
    QElapsedTimer m_recognizeClock;
    QElapsedTimer m_resultClock;
    QTimer *m_ticker = nullptr;

    StatusMark *m_headerMark = nullptr;
    QLabel *m_headerTitle = nullptr;
    Tag *m_headerChip = nullptr;
    QLabel *m_headerMeta = nullptr;
    PanelStrip *m_subBar = nullptr;
    PhaseMark *m_subMark = nullptr;
    QLabel *m_subTitle = nullptr;
    QLabel *m_subRight = nullptr;
    QWidget *m_body = nullptr;
    QStackedLayout *m_bodyStack = nullptr;
    WaveformView *m_wave = nullptr;
    QLabel *m_waveTimes[5] = {};
    EventLogWidget *m_eventLog = nullptr;
    QScrollArea *m_transcriptScroll = nullptr;
    TranscriptView *m_transcript = nullptr;
    QLabel *m_footerLeft = nullptr;
    QLabel *m_footerRight = nullptr;
    ProgressCard *m_progressCard = nullptr;
    PollCard *m_pollCard = nullptr;
    AudioCard *m_audioCard = nullptr;
};

// -----------------------------------------------------------------------------
// AI 选片面板
// -----------------------------------------------------------------------------

class SelectPanelWidget : public QWidget
{
public:
    explicit SelectPanelWidget(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        auto *root = new QHBoxLayout(this);
        root->setContentsMargins(kSidePadding, 12, kSidePadding, 12);
        root->setSpacing(kPanelGap);

        // 左侧：大模型选片过程
        auto *processCard = new Card(Theme::Surface1, Theme::Border, 9, this);
        auto *processLayout = new QVBoxLayout;
        processLayout->setContentsMargins(0, 0, 0, 0);
        processLayout->setSpacing(0);
        processCard->contentLayout()->addLayout(processLayout);

        auto *header = new QWidget(processCard);
        auto *headerLayout = new QHBoxLayout(header);
        headerLayout->setContentsMargins(12, 7, 12, 7);
        headerLayout->setSpacing(8);
        m_headerMark = new StatusMark(StatusMark::Mode::Clock, 12, Theme::Text3, header);
        headerLayout->addWidget(m_headerMark);
        headerLayout->addWidget(
            Ui::label(QStringLiteral("AI 选片过程"), 12, 500, Theme::Text1, header));
        m_stateTag = new Tag(QStringLiteral("等待"), Theme::Text3, Qt::transparent,
                             Theme::Border, 15, 7, true, 9, 6, 0, header);
        headerLayout->addWidget(m_stateTag);
        headerLayout->addStretch(1);
        m_tokenLabel = monoLabel(QStringLiteral("0 tokens"), 10, Theme::Text3, header);
        headerLayout->addWidget(m_tokenLabel);
        processLayout->addWidget(header);
        processLayout->addWidget(makeHLine(processCard));

        auto *promptBar = new QWidget(processCard);
        auto *promptLayout = new QHBoxLayout(promptBar);
        promptLayout->setContentsMargins(12, 6, 12, 6);
        promptLayout->setSpacing(6);
        m_promptTag = new Tag(QStringLiteral("提示词"), Theme::Ai, Theme::alpha(Theme::Ai, 0.13),
                              Qt::transparent, 14, 3, true, 9, 5, 0, promptBar);
        promptLayout->addWidget(m_promptTag);
        m_promptLabelWidget = new ElidedLabel(QStringLiteral("—"), promptBar);
        m_promptLabelWidget->setPixelSize(10);
        m_promptLabelWidget->setTextColor(Theme::Text3);
        promptLayout->addWidget(m_promptLabelWidget, 1);
        m_scanInfo = monoLabel(QStringLiteral("按转写时长分段"), 9, Theme::Text3,
                               promptBar);
        promptLayout->addWidget(m_scanInfo);
        processLayout->addWidget(promptBar);
        processLayout->addWidget(makeHLine(processCard));

        m_body = new QWidget(processCard);
        m_bodyStack = new QStackedLayout(m_body);
        m_bodyStack->setContentsMargins(0, 0, 0, 0);

        m_scroll = new QScrollArea(m_body);
        m_scroll->setFrameShape(QFrame::NoFrame);
        m_scroll->setWidgetResizable(true);
        m_scroll->setHorizontalScrollBarPolicy(Qt::ScrollBarAlwaysOff);
        m_scroll->setStyleSheet(QStringLiteral(
            "QScrollArea { background:transparent; border:none; }"
            "QScrollArea > QWidget > QWidget { background:transparent; }"));
        m_thoughtHost = new QWidget(m_scroll);
        m_thoughtLayout = new QVBoxLayout(m_thoughtHost);
        m_thoughtLayout->setContentsMargins(4, 4, 4, 4);
        m_thoughtLayout->setSpacing(0);
        m_thoughtLayout->addStretch(1);
        m_scroll->setWidget(m_thoughtHost);
        m_bodyStack->addWidget(m_scroll);

        m_waiting = new QWidget(m_body);
        auto *waitingLayout = new QVBoxLayout(m_waiting);
        waitingLayout->setContentsMargins(20, 20, 20, 20);
        waitingLayout->setSpacing(8);
        waitingLayout->addStretch(1);
        auto *sparkleCircle = new Card(Theme::alpha(Theme::Ai, 0.13), Qt::transparent, 16,
                                       m_waiting);
        sparkleCircle->setFixedSize(32, 32);
        auto *circleLayout = new QVBoxLayout;
        circleLayout->setContentsMargins(8, 8, 8, 8);
        sparkleCircle->contentLayout()->addLayout(circleLayout);
        circleLayout->addWidget(
            new GlyphIcon(Icons::Glyph::Sparkle, 16, Theme::Ai, sparkleCircle));
        waitingLayout->addWidget(sparkleCircle, 0, Qt::AlignHCenter);
        m_waitingTitle = Ui::label(QStringLiteral("等待 ASR 转写完成"), 12, 500,
                                   Theme::Text2, m_waiting);
        m_waitingTitle->setAlignment(Qt::AlignCenter);
        waitingLayout->addWidget(m_waitingTitle);
        m_waitingText = Ui::label(
            QStringLiteral("转写完成后 AI 会立即从转写稿开始粗筛，"
                           "并边思考边锁定切片，不需要手动等待。"),
            10, 400, Theme::Text3, m_waiting);
        m_waitingText->setAlignment(Qt::AlignCenter);
        m_waitingText->setWordWrap(true);
        m_waitingText->setFixedWidth(320);
        m_waitingText->setMinimumHeight(m_waitingText->heightForWidth(320));
        waitingLayout->addWidget(m_waitingText);
        waitingLayout->addStretch(1);
        m_bodyStack->addWidget(m_waiting);
        m_bodyStack->setCurrentWidget(m_waiting);
        processLayout->addWidget(m_body, 1);

        // 实时思考面板：固定在卡片底部，把 reasoning 切成要点逐条展示最新几条；
        // 历史日志区只放规划/读取/选入等关键节点，避免长 reasoning 刷屏。
        m_thinkingHost = new QWidget(processCard);
        auto *thinkingHostLayout = new QVBoxLayout(m_thinkingHost);
        thinkingHostLayout->setContentsMargins(10, 8, 10, 10);
        thinkingHostLayout->setSpacing(0);
        m_thinkingPanel = new LiveThinkingPanel(m_thinkingHost);
        thinkingHostLayout->addWidget(m_thinkingPanel);
        m_thinkingHost->hide();
        processLayout->addWidget(m_thinkingHost);
        root->addWidget(processCard, 1);

        // 右侧：候选切片
        auto *candidateCard = new Card(Theme::Surface1, Theme::Border, 9, this);
        candidateCard->setFixedWidth(320);
        auto *candidateLayout = new QVBoxLayout;
        candidateLayout->setContentsMargins(0, 0, 0, 0);
        candidateLayout->setSpacing(0);
        candidateCard->contentLayout()->addLayout(candidateLayout);

        auto *candidateHeader = new QWidget(candidateCard);
        auto *candidateHeaderLayout = new QHBoxLayout(candidateHeader);
        candidateHeaderLayout->setContentsMargins(12, 7, 12, 7);
        candidateHeaderLayout->setSpacing(6);
        candidateHeaderLayout->addWidget(
            Ui::label(QStringLiteral("候选切片"), 12, 500, Theme::Text1, candidateHeader));
        m_pickedTag = new Tag(QStringLiteral("已锁定 0/0"), Theme::Success,
                              Theme::alpha(Theme::Success, 0.13),
                              Theme::alpha(Theme::Success, 0.35), 15, 7, true, 9, 6, 0,
                              candidateHeader);
        candidateHeaderLayout->addWidget(m_pickedTag);
        candidateHeaderLayout->addStretch(1);
        m_candidateCount = monoLabel(QStringLiteral("候选 0"), 9, Theme::Text3,
                                     candidateHeader);
        candidateHeaderLayout->addWidget(m_candidateCount);
        candidateLayout->addWidget(candidateHeader);
        candidateLayout->addWidget(makeHLine(candidateCard));

        m_candidateScroll = new QScrollArea(candidateCard);
        m_candidateScroll->setFrameShape(QFrame::NoFrame);
        m_candidateScroll->setWidgetResizable(true);
        m_candidateScroll->setHorizontalScrollBarPolicy(Qt::ScrollBarAlwaysOff);
        m_candidateScroll->setStyleSheet(QStringLiteral(
            "QScrollArea { background:transparent; border:none; }"
            "QScrollArea > QWidget > QWidget { background:transparent; }"));
        auto *candidateHost = new QWidget(m_candidateScroll);
        m_candidatesLayout = new QVBoxLayout(candidateHost);
        m_candidatesLayout->setContentsMargins(8, 8, 8, 8);
        m_candidatesLayout->setSpacing(6);
        m_candidatesLayout->addStretch(1);
        m_candidateScroll->setWidget(candidateHost);
        candidateLayout->addWidget(m_candidateScroll, 1);

        candidateLayout->addWidget(makeHLine(candidateCard));
        auto *candidateFooter = new QWidget(candidateCard);
        auto *candidateFooterLayout = new QHBoxLayout(candidateFooter);
        candidateFooterLayout->setContentsMargins(12, 5, 12, 5);
        candidateFooterLayout->setSpacing(6);
        m_scanFooter = monoLabel(QStringLiteral("扫描 0/0 段"), 9, Theme::Text3,
                                 candidateFooter);
        candidateFooterLayout->addWidget(m_scanFooter);
        m_selectedFooter = monoLabel(QStringLiteral("已选 0 句"), 9, Theme::Text3,
                                     candidateFooter);
        candidateFooterLayout->addWidget(m_selectedFooter);
        candidateFooterLayout->addStretch(1);
        candidateFooterLayout->addWidget(
            monoLabel(QStringLiteral("均衡切分 · 句号优先"), 9, Theme::Text3,
                      candidateFooter));
        candidateLayout->addWidget(candidateFooter);
        root->addWidget(candidateCard);

        reset();
    }

    void reset()
    {
        m_batchCount = 0;
        m_batchDone = 0;
        m_activeBatch = 0;
        m_picked = 0;
        m_tokens = 0;
        m_rate = 0;
        m_running = false;
        m_finished = false;
        m_failed = false;
        m_started = false;
        m_promptText.clear();
        m_batches.clear();
        m_promptLabelWidget->setText(QStringLiteral("—"));
        m_scanInfo->setText(QStringLiteral("按转写时长分段"));
        m_tokenLabel->setText(QStringLiteral("0 tokens"));
        m_pickedTag->setText(QStringLiteral("已锁定 0/0"));
        m_candidateCount->setText(QStringLiteral("候选 0"));
        m_scanFooter->setText(QStringLiteral("扫描 0/0 段"));
        m_selectedFooter->setText(QStringLiteral("已选 0 句"));
        m_thinkingPanel->reset();
        m_thinkingHost->hide();
        m_waitingTitle->setText(QStringLiteral("等待 ASR 转写完成"));
        m_waitingText->setText(
            QStringLiteral("转写完成后 AI 会立即从转写稿开始粗筛，"
                           "并边思考边锁定切片，不需要手动等待。"));
        m_bodyStack->setCurrentWidget(m_waiting);
        m_headerMark->setMode(StatusMark::Mode::Clock);
        m_headerMark->setColor(Theme::Text3);
        setStateTag(QStringLiteral("等待"), Theme::Text3, Qt::transparent, Theme::Border);
        clearThoughts();
        clearCandidates();
    }

    void setPrompt(const QString &label)
    {
        m_promptText = label;
        m_promptLabelWidget->setText(label.isEmpty() ? QStringLiteral("—") : label);
    }

    void setPlanned(const QVector<PipelineUi::Candidate> &batches, int sentenceCount,
                    int charCount)
    {
        m_batches = batches;
        m_batchCount = batches.size();
        m_scanInfo->setText(m_batchCount <= 1
                                ? QStringLiteral("整段扫描 · 无需切分")
                                : QStringLiteral("分 %1 段扫描 · 均衡切分")
                                      .arg(m_batchCount));
        m_candidateCount->setText(QStringLiteral("候选 %1").arg(m_batchCount));
        m_scanFooter->setText(QStringLiteral("扫描 0/%1 段").arg(m_batchCount));
        m_pickedTag->setText(QStringLiteral("已锁定 0/%1").arg(m_batchCount));
        clearCandidates();
        for (const PipelineUi::Candidate &batch : batches) {
            auto *card = new CandidateCard(batch.index, m_candidateScroll->widget());
            card->setQueued(batch.range, batch.sourceSentenceCount);
            m_candidatesLayout->insertWidget(m_candidatesLayout->count() - 1, card);
            m_candidates.append(card);
        }
        m_bodyStack->setCurrentWidget(m_scroll);
        m_thinkingHost->show();
        m_thinkingPanel->showPlan(m_batchCount);
        addThought(QStringLiteral("规划"), Theme::Ai,
                   QStringLiteral("读取 %1 句转写稿（%2 字），按提示词「%3」分 %4 段扫描。")
                       .arg(formatNumber(sentenceCount))
                       .arg(formatNumber(charCount))
                       .arg(m_promptText.isEmpty() ? QStringLiteral("默认") : m_promptText)
                       .arg(m_batchCount),
                   QStringLiteral("规划"));
    }

    void startBatch(int index, int count, qint64 startMs, qint64 endMs, int sentences,
                    const QString &note)
    {
        m_batchCount = qMax(m_batchCount, count);
        m_activeBatch = index;
        m_running = true;
        m_started = true;
        m_failed = false;
        setStateTag(QStringLiteral("推理中"), Theme::Ai, Theme::alpha(Theme::Ai, 0.13),
                    Theme::alpha(Theme::Ai, 0.35));
        m_headerMark->setMode(StatusMark::Mode::Dot);
        m_headerMark->setColor(Theme::Ai);
        m_bodyStack->setCurrentWidget(m_scroll);
        m_thinkingHost->show();
        m_thinkingPanel->beginBatch(index, m_batchCount);
        const QString text =
            note.isEmpty()
                ? QStringLiteral("读取第 %1/%2 段（%3）· %4 句转写，按提示词评估。")
                      .arg(index)
                      .arg(m_batchCount)
                      .arg(PipelineUi::formatRange(startMs, endMs))
                      .arg(sentences)
                : note;
        addThought(QStringLiteral("读取"), Theme::Primary, text, QStringLiteral("tool"));

        if (index >= 1 && index <= m_candidates.size()) {
            CandidateCard *card = m_candidates.at(index - 1);
            card->setAnalyzing(PipelineUi::formatRange(startMs, endMs), sentences);
            QTimer::singleShot(0, this, [this, card] {
                if (m_candidateScroll) {
                    m_candidateScroll->ensureWidgetVisible(card, 0, 20);
                }
            });
        }
        updateStats();
    }

    void skipBatch(int index, int count, const QString &reason, qint64 tokens)
    {
        m_batchCount = qMax(m_batchCount, count);
        m_batchDone = qMax(m_batchDone, index);
        m_running = false;
        m_tokens += tokens;
        m_thinkingHost->show();
        m_thinkingPanel->finishBatch(index, m_batchCount, false);
        addThought(QStringLiteral("排除"), Theme::Danger,
                   QStringLiteral("第 %1/%2 段未选出切片：%3")
                       .arg(index)
                       .arg(m_batchCount)
                       .arg(reason),
                   QStringLiteral("skip"));
        if (index >= 1 && index <= m_candidates.size()) {
            m_candidates.at(index - 1)->setSkipped(reason);
        }
        updateStats();
    }

    void finishBatch(const PipelineUi::Candidate &candidate, qint64 tokens)
    {
        m_batchDone = qMax(m_batchDone, candidate.index);
        m_running = false;
        ++m_picked;
        m_tokens += tokens;
        m_thinkingHost->show();
        m_thinkingPanel->finishBatch(candidate.index, m_batchCount, true);
        addThought(QStringLiteral("选入"), Theme::Success,
                   QStringLiteral("第 %1/%2 段选出「%3」｜ %4 段 · %5 ｜ %6 条字幕")
                       .arg(candidate.index)
                       .arg(qMax(m_batchCount, candidate.index))
                       .arg(candidate.title)
                       .arg(qMax(1, candidate.rangeCount))
                       .arg(PipelineUi::formatSpan(candidate.durationMs))
                       .arg(candidate.captionCount),
                   QStringLiteral("pick"));
        if (candidate.index >= 1 && candidate.index <= m_candidates.size()) {
            m_candidates.at(candidate.index - 1)->setPicked(candidate);
        }
        if (candidate.index >= 1 && candidate.index <= m_batches.size()) {
            m_batches[candidate.index - 1] = candidate;
        }
        updateStats();
    }

    void finish(int pickedCount, int batchCount, qint64 tokens)
    {
        m_finished = true;
        m_running = false;
        m_rate = 0;
        m_picked = qMax(m_picked, pickedCount);
        m_batchCount = qMax(m_batchCount, batchCount);
        m_batchDone = m_batchCount;
        if (tokens > 0) {
            m_tokens = tokens;
        }
        m_thinkingHost->show();
        m_thinkingPanel->completeAll(m_picked);
        m_headerMark->setMode(StatusMark::Mode::Check);
        m_headerMark->setColor(Theme::Success);
        setStateTag(QStringLiteral("已完成"), Theme::Success,
                    Theme::alpha(Theme::Success, 0.13),
                    Theme::alpha(Theme::Success, 0.35));
        addThought(QStringLiteral("完成"), Theme::Success,
                   QStringLiteral("已锁定 %1 条切片，移交草稿生成。").arg(m_picked),
                   QStringLiteral("finish"));
        updateStats();
    }

    void setFailed(const QString &message)
    {
        m_failed = true;
        m_running = false;
        m_rate = 0;
        m_thinkingHost->show();
        m_thinkingPanel->fail(message);
        m_headerMark->setMode(StatusMark::Mode::Cross);
        m_headerMark->setColor(Theme::Danger);
        setStateTag(QStringLiteral("已中断"), Theme::Danger,
                    Theme::alpha(Theme::Danger, 0.13),
                    Theme::alpha(Theme::Danger, 0.35));
        addThought(QStringLiteral("失败"), Theme::Danger, message,
                   QStringLiteral("skip"));
        updateStats();
    }

    void setTokenRate(int rate)
    {
        m_rate = qMax(0, rate);
        updateStats();
    }

    // 模型 reasoning_content 的实时增量：按句读切分后逐条追加到思考面板。
    void appendThoughtDelta(int index, int count, const QString &text)
    {
        if (text.isEmpty()) {
            return;
        }
        m_batchCount = qMax(m_batchCount, count);
        m_bodyStack->setCurrentWidget(m_scroll);
        m_thinkingHost->show();
        m_thinkingPanel->appendThought(index, count, text);
    }

    // 等待/重试/生成结果等状态只更新固定面板；仅重试等关键状态写入历史。
    void appendStatus(const QString &text)
    {
        if (text.isEmpty()) {
            return;
        }
        m_thinkingHost->show();
        if (text.startsWith(QStringLiteral("模型仍在处理中"))) {
            m_thinkingPanel->showIdle(text);
            return;
        }
        if (text.startsWith(QStringLiteral("正在生成选片结果"))) {
            m_thinkingPanel->showContent(text);
            return;
        }
        m_thinkingPanel->showStatus(text);
        addThought(QStringLiteral("状态"), Theme::Ai, text, QStringLiteral("think"));
        m_bodyStack->setCurrentWidget(m_scroll);
    }

    int pickedCount() const { return m_picked; }
    int batchCount() const { return m_batchCount; }
    int batchDone() const { return m_batchDone; }
    qint64 tokens() const { return m_tokens; }
    bool finished() const { return m_finished; }
    bool failed() const { return m_failed; }
    bool started() const { return m_started; }
    int stagePercent() const
    {
        if (m_finished) {
            return 100;
        }
        if (m_batchCount <= 0) {
            return 0;
        }
        const int processed = m_batchDone * 100 + (m_running ? 50 : 0);
        return qBound(0, processed / m_batchCount, 99);
    }

private:
    void setStateTag(const QString &text, const QColor &foreground,
                     const QColor &background, const QColor &border)
    {
        m_stateTag->setText(text);
        m_stateTag->setStyle(foreground, background, border);
    }

    ThoughtRow *addThought(const QString &kind, const QColor &tone,
                           const QString &text, const QString &kindKey)
    {
        auto *row = new ThoughtRow(kind, tone, text, m_thoughtHost);
        if (kindKey == QLatin1String("pick")) {
            row->setTextColor(Theme::Text1);
        } else if (kindKey == QLatin1String("skip")) {
            row->setTextColor(Theme::Text3);
        } else {
            row->setTextColor(Theme::Text2);
        }
        m_thoughts.append(row);
        m_thoughtLayout->insertWidget(m_thoughtLayout->count() - 1, row);
        scrollThoughtsToBottom();
        return row;
    }

    void scrollThoughtsToBottom()
    {
        QTimer::singleShot(0, this, [this] {
            if (m_scroll && m_scroll->verticalScrollBar()) {
                QScrollBar *bar = m_scroll->verticalScrollBar();
                bar->setValue(bar->maximum());
            }
        });
    }

    void updateStats()
    {
        m_tokenLabel->setText(m_rate > 0
                                  ? QStringLiteral("%1 tokens · %2 tok/s")
                                        .arg(formatNumber(m_tokens))
                                        .arg(m_rate)
                                  : QStringLiteral("%1 tokens").arg(formatNumber(m_tokens)));
        m_pickedTag->setText(QStringLiteral("已锁定 %1/%2").arg(m_picked).arg(m_batchCount));
        m_candidateCount->setText(QStringLiteral("候选 %1").arg(m_batchCount));
        const int scanned = m_finished ? m_batchCount : m_batchDone;
        m_scanFooter->setText(
            QStringLiteral("扫描 %1/%2 段").arg(scanned).arg(m_batchCount));
        int selectedSentences = 0;
        for (const PipelineUi::Candidate &batch : m_batches) {
            selectedSentences += batch.captionCount;
        }
        m_selectedFooter->setText(QStringLiteral("已选 %1 句").arg(selectedSentences));
    }

    void clearThoughts()
    {
        for (ThoughtRow *row : m_thoughts) {
            m_thoughtLayout->removeWidget(row);
            row->hide();
            row->deleteLater();
        }
        m_thoughts.clear();
    }

    void clearCandidates()
    {
        m_candidates.clear();
        while (m_candidatesLayout->count() > 1) {
            QLayoutItem *item = m_candidatesLayout->takeAt(0);
            if (QWidget *widget = item->widget()) {
                widget->hide();
                widget->deleteLater();
            }
            delete item;
        }
    }

    int m_batchCount = 0;
    int m_batchDone = 0;
    int m_activeBatch = 0;
    int m_picked = 0;
    qint64 m_tokens = 0;
    int m_rate = 0;
    bool m_running = false;
    bool m_finished = false;
    bool m_failed = false;
    bool m_started = false;
    QString m_promptText;
    QVector<PipelineUi::Candidate> m_batches;
    QVector<CandidateCard *> m_candidates;
    QVector<ThoughtRow *> m_thoughts;

    StatusMark *m_headerMark = nullptr;
    Tag *m_stateTag = nullptr;
    QLabel *m_tokenLabel = nullptr;
    Tag *m_promptTag = nullptr;
    ElidedLabel *m_promptLabelWidget = nullptr;
    QLabel *m_scanInfo = nullptr;
    QWidget *m_body = nullptr;
    QStackedLayout *m_bodyStack = nullptr;
    QScrollArea *m_scroll = nullptr;
    QWidget *m_thoughtHost = nullptr;
    QVBoxLayout *m_thoughtLayout = nullptr;
    QWidget *m_waiting = nullptr;
    QLabel *m_waitingTitle = nullptr;
    QLabel *m_waitingText = nullptr;
    QWidget *m_thinkingHost = nullptr;
    LiveThinkingPanel *m_thinkingPanel = nullptr;
    Tag *m_pickedTag = nullptr;
    QLabel *m_candidateCount = nullptr;
    QScrollArea *m_candidateScroll = nullptr;
    QVBoxLayout *m_candidatesLayout = nullptr;
    QLabel *m_scanFooter = nullptr;
    QLabel *m_selectedFooter = nullptr;
};

// -----------------------------------------------------------------------------
// 生成剪映草稿面板
// -----------------------------------------------------------------------------

class DraftPanelWidget : public QWidget
{
public:
    explicit DraftPanelWidget(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        auto *root = new QVBoxLayout(this);
        root->setContentsMargins(0, 0, 0, 0);
        root->setSpacing(0);

        // 顶部统计条
        auto *strip = new QWidget(this);
        strip->setFixedHeight(30);
        auto *stripLayout = new QHBoxLayout(strip);
        stripLayout->setContentsMargins(kSidePadding, 0, kSidePadding, 0);
        stripLayout->setSpacing(7);
        m_headerTitle = Ui::label(QStringLiteral("0 条切片依次生成"), 12, 500,
                                  Theme::Text2, strip);
        stripLayout->addWidget(m_headerTitle);
        auto *dot1 = Ui::label(QStringLiteral("·"), 11, 400, Theme::BorderStrong, strip);
        stripLayout->addWidget(dot1);
        m_concurrency = Ui::label(QStringLiteral("逐条执行"), 10, 400, Theme::Text3,
                                  strip);
        stripLayout->addWidget(m_concurrency);
        auto *dot2 = Ui::label(QStringLiteral("·"), 11, 400, Theme::BorderStrong, strip);
        stripLayout->addWidget(dot2);
        stripLayout->addWidget(Ui::label(QStringLiteral("已完成"), 10, 400, Theme::Text3,
                                         strip));
        m_doneValue = monoLabel(QStringLiteral("0/0"), 10, Theme::Success, strip);
        stripLayout->addWidget(m_doneValue);
        auto *dot3 = Ui::label(QStringLiteral("·"), 11, 400, Theme::BorderStrong, strip);
        stripLayout->addWidget(dot3);
        stripLayout->addWidget(Ui::label(QStringLiteral("已保存"), 10, 400, Theme::Text3,
                                         strip));
        m_savedValue = monoLabel(QStringLiteral("0/0"), 10, Theme::Primary, strip);
        stripLayout->addWidget(m_savedValue);
        m_readyTag = new Tag(QStringLiteral("草稿就绪 0 条"), Theme::Success,
                             Theme::alpha(Theme::Success, 0.13),
                             Theme::alpha(Theme::Success, 0.35), 15, 7, true, 9, 6, 0,
                             strip);
        m_readyTag->hide();
        stripLayout->addWidget(m_readyTag);
        stripLayout->addStretch(1);
        stripLayout->addWidget(new GlyphIcon(Icons::Glyph::Bolt, 10, Theme::Ai, strip));
        stripLayout->addWidget(
            Ui::label(QStringLiteral("按源画幅生成 · 自动匹配字幕样式"), 10, 400, Theme::Text3,
                      strip));
        root->addWidget(strip);
        root->addWidget(makeHLine(this));

        auto *body = new QWidget(this);
        auto *bodyLayout = new QHBoxLayout(body);
        bodyLayout->setContentsMargins(kSidePadding, 12, kSidePadding, 12);
        bodyLayout->setSpacing(kPanelGap);

        // 左侧：切片草稿列表
        auto *listCard = new Card(Theme::Surface1, Theme::Border, 9, body);
        listCard->setFixedWidth(330);
        auto *listLayout = new QVBoxLayout;
        listLayout->setContentsMargins(0, 0, 0, 0);
        listLayout->setSpacing(0);
        listCard->contentLayout()->addLayout(listLayout);
        auto *listHeader = new QWidget(listCard);
        auto *listHeaderLayout = new QHBoxLayout(listHeader);
        listHeaderLayout->setContentsMargins(12, 7, 12, 7);
        listHeaderLayout->setSpacing(6);
        listHeaderLayout->addWidget(
            Ui::label(QStringLiteral("切片草稿"), 12, 500, Theme::Text1, listHeader));
        listHeaderLayout->addStretch(1);
        m_readyLabel = monoLabel(QStringLiteral("0 / 0 条就绪"), 10, Theme::Text3,
                                 listHeader);
        listHeaderLayout->addWidget(m_readyLabel);
        listLayout->addWidget(listHeader);
        listLayout->addWidget(makeHLine(listCard));

        m_rowScroll = new QScrollArea(listCard);
        m_rowScroll->setFrameShape(QFrame::NoFrame);
        m_rowScroll->setWidgetResizable(true);
        m_rowScroll->setHorizontalScrollBarPolicy(Qt::ScrollBarAlwaysOff);
        m_rowScroll->setStyleSheet(QStringLiteral(
            "QScrollArea { background:transparent; border:none; }"
            "QScrollArea > QWidget > QWidget { background:transparent; }"));
        auto *rowHost = new QWidget(m_rowScroll);
        m_rowLayout = new QVBoxLayout(rowHost);
        m_rowLayout->setContentsMargins(8, 8, 8, 8);
        m_rowLayout->setSpacing(6);
        m_emptyLabel = Ui::label(
            QStringLiteral("选片完成后，这里会显示每条切片草稿的生成进度。"), 11, 400,
            Theme::Text3, rowHost);
        m_emptyLabel->setAlignment(Qt::AlignCenter);
        m_emptyLabel->setWordWrap(true);
        m_rowLayout->addWidget(m_emptyLabel);
        m_rowLayout->addStretch(1);
        m_rowScroll->setWidget(rowHost);
        listLayout->addWidget(m_rowScroll, 1);
        bodyLayout->addWidget(listCard);

        // 右侧：当前草稿的详细步骤
        auto *detailCard = new Card(Theme::Surface1, Theme::Border, 9, body);
        auto *detailLayout = new QVBoxLayout;
        detailLayout->setContentsMargins(0, 0, 0, 0);
        detailLayout->setSpacing(0);
        detailCard->contentLayout()->addLayout(detailLayout);

        auto *detailHeader = new QWidget(detailCard);
        auto *detailHeaderLayout = new QHBoxLayout(detailHeader);
        detailHeaderLayout->setContentsMargins(12, 8, 12, 8);
        detailHeaderLayout->setSpacing(10);
        auto *detailTitleLayout = new QVBoxLayout;
        detailTitleLayout->setContentsMargins(0, 0, 0, 0);
        detailTitleLayout->setSpacing(2);
        auto *detailTop = new QHBoxLayout;
        detailTop->setContentsMargins(0, 0, 0, 0);
        detailTop->setSpacing(6);
        m_detailIndex = monoLabel(QStringLiteral("01 / 01"), 9, Theme::Text3,
                                  detailHeader);
        detailTop->addWidget(m_detailIndex);
        m_detailTitle = new ElidedLabel(QStringLiteral("等待生成剪映草稿"), detailHeader);
        m_detailTitle->setPixelSize(13);
        m_detailTitle->setFontWeight(650);
        m_detailTitle->setTextColor(Theme::Text1);
        detailTop->addWidget(m_detailTitle, 1);
        detailTitleLayout->addLayout(detailTop);
        m_detailMeta = monoLabel(
            QStringLiteral("AI 选片完成后，会在这里逐条生成剪映草稿。"), 9,
            Theme::Text3, detailHeader);
        detailTitleLayout->addWidget(m_detailMeta);
        detailHeaderLayout->addLayout(detailTitleLayout, 1);
        m_detailTag = new Tag(QStringLiteral("排队中"), Theme::Text3, Qt::transparent,
                              Theme::Border, 15, 7, true, 9, 6, 0, detailHeader);
        detailHeaderLayout->addWidget(m_detailTag, 0, Qt::AlignTop);
        detailLayout->addWidget(detailHeader);
        detailLayout->addWidget(makeHLine(detailCard));

        auto *detailProgressRow = new QWidget(detailCard);
        auto *detailProgressLayout = new QHBoxLayout(detailProgressRow);
        detailProgressLayout->setContentsMargins(12, 10, 12, 0);
        detailProgressLayout->setSpacing(8);
        m_detailProgress = new ProgressBar(detailProgressRow);
        detailProgressLayout->addWidget(m_detailProgress, 1);
        m_detailPercent = monoLabel(QStringLiteral("0%"), 9, Theme::Text3,
                                    detailProgressRow);
        m_detailPercent->setFixedWidth(34);
        m_detailPercent->setAlignment(Qt::AlignRight | Qt::AlignVCenter);
        detailProgressLayout->addWidget(m_detailPercent);
        detailLayout->addWidget(detailProgressRow);

        m_stepScroll = new QScrollArea(detailCard);
        m_stepScroll->setFrameShape(QFrame::NoFrame);
        m_stepScroll->setWidgetResizable(true);
        m_stepScroll->setHorizontalScrollBarPolicy(Qt::ScrollBarAlwaysOff);
        m_stepScroll->setStyleSheet(QStringLiteral(
            "QScrollArea { background:transparent; border:none; }"
            "QScrollArea > QWidget > QWidget { background:transparent; }"));
        auto *stepHost = new QWidget(m_stepScroll);
        auto *stepLayout = new QVBoxLayout(stepHost);
        stepLayout->setContentsMargins(10, 10, 10, 10);
        stepLayout->setSpacing(6);
        for (int i = 0; i < 6; ++i) {
            m_steps[i] = new StepRow(i, stepHost);
            stepLayout->addWidget(m_steps[i]);
        }
        stepLayout->addStretch(1);
        m_stepScroll->setWidget(stepHost);
        detailLayout->addWidget(m_stepScroll, 1);

        detailLayout->addWidget(makeHLine(detailCard));
        auto *detailFooter = new QWidget(detailCard);
        auto *detailFooterLayout = new QHBoxLayout(detailFooter);
        detailFooterLayout->setContentsMargins(12, 5, 12, 5);
        detailFooterLayout->setSpacing(6);
        m_outputInfo = monoLabel(QStringLiteral("输出按源画幅 · H.264"), 9, Theme::Text3,
                                 detailFooter);
        detailFooterLayout->addWidget(m_outputInfo);
        detailFooterLayout->addStretch(1);
        m_detailRemaining = monoLabel(QStringLiteral("等待开始"), 9, Theme::Text3,
                                      detailFooter);
        detailFooterLayout->addWidget(m_detailRemaining);
        detailLayout->addWidget(detailFooter);
        bodyLayout->addWidget(detailCard, 1);

        root->addWidget(body, 1);
        reset();
    }

    void reset()
    {
        m_items.clear();
        m_clocks.clear();
        m_rows.clear();
        m_count = 0;
        m_pinned = -1;
        m_activeIndex = -1;
        m_finished = false;
        m_failed = false;
        while (m_rowLayout->count() > 2) {
            QLayoutItem *item = m_rowLayout->takeAt(1);
            if (QWidget *widget = item->widget()) {
                widget->hide();
                widget->deleteLater();
            }
            delete item;
        }
        m_emptyLabel->show();
        updateHeader();
        refreshDetail();
    }

    void setPlan(const QVector<PipelineUi::Candidate> &candidates)
    {
        m_items.clear();
        m_clocks.clear();
        m_rows.clear();
        while (m_rowLayout->count() > 2) {
            QLayoutItem *item = m_rowLayout->takeAt(1);
            if (QWidget *widget = item->widget()) {
                widget->hide();
                widget->deleteLater();
            }
            delete item;
        }
        m_count = candidates.size();
        m_items.reserve(m_count);
        m_clocks.resize(m_count);
        for (const PipelineUi::Candidate &candidate : candidates) {
            DraftItemData data;
            data.candidate = candidate;
            data.steps.resize(6);
            for (int i = 0; i < 6; ++i) {
                data.steps[i].status = int(PipelineUi::StepStatus::Waiting);
                data.steps[i].detail = QStringLiteral("等待上一步完成");
            }
            m_items.append(data);
        }
        for (int i = 0; i < m_items.size(); ++i) {
            auto *row = new DraftRowWidget(m_items.at(i), m_rowScroll->widget());
            const int itemIndex = i;
            row->onClick = [this, itemIndex] {
                m_pinned = itemIndex;
                m_activeIndex = itemIndex;
                updateRowSelection();
                refreshDetail();
            };
            m_rowLayout->insertWidget(m_rowLayout->count() - 1, row);
            m_rows.append(row);
        }
        m_emptyLabel->setVisible(m_items.isEmpty());
        m_pinned = -1;
        m_activeIndex = m_items.isEmpty() ? -1 : 0;
        updateRowSelection();
        updateHeader();
        refreshDetail();
    }

    void startDraft(int index, int count, const PipelineUi::Candidate &candidate)
    {
        if (index < 1) {
            return;
        }
        m_count = qMax(m_count, count);
        ensureItem(index);
        DraftItemData &item = m_items[index - 1];
        if (!candidate.title.isEmpty()) {
            item.candidate = candidate;
        }
        item.status = 1;
        item.progress = 0;
        item.saved = false;
        for (int i = 0; i < item.steps.size(); ++i) {
            if (item.steps[i].status != int(PipelineUi::StepStatus::Done)) {
                item.steps[i].status = int(PipelineUi::StepStatus::Waiting);
                item.steps[i].progress = 0;
                item.steps[i].detail = QStringLiteral("等待上一步完成");
            }
        }
        m_clocks[index - 1].start();
        if (m_pinned < 0) {
            m_activeIndex = index - 1;
        }
        updateRow(index - 1);
        updateHeader();
        refreshDetail();
    }

    void setStep(int draftIndex, int step, int status, int percent,
                 const QString &detail, qint64 durationMs)
    {
        if (draftIndex < 1) {
            return;
        }
        ensureItem(draftIndex);
        if (step < 0 || step >= 6) {
            return;
        }
        DraftItemData &item = m_items[draftIndex - 1];
        DraftStepData &data = item.steps[step];
        data.status = status;
        data.progress = qBound(0, percent, 100);
        if (!detail.isEmpty()) {
            data.detail = detail;
        }
        if (status == int(PipelineUi::StepStatus::Done)) {
            data.progress = 100;
            data.durationMs = durationMs;
        }
        if (status == int(PipelineUi::StepStatus::Failed)) {
            item.status = 3;
        } else if (item.status == 0
                   || (item.status == 1 && status != int(PipelineUi::StepStatus::Waiting))) {
            item.status = 1;
        }
        if (step == 5 && data.status == int(PipelineUi::StepStatus::Done)) {
            item.saved = true;
        }
        bool allDone = true;
        int sum = 0;
        for (const DraftStepData &entry : item.steps) {
            sum += entry.status == int(PipelineUi::StepStatus::Done) ? 100 : entry.progress;
            if (entry.status != int(PipelineUi::StepStatus::Done)) {
                allDone = false;
            }
        }
        item.progress = qBound(0, sum / 6, 100);
        if (allDone) {
            item.status = 2;
            item.progress = 100;
        }
        if (m_pinned < 0 && item.status == 1) {
            m_activeIndex = draftIndex - 1;
        }
        updateRow(draftIndex - 1);
        updateHeader();
        refreshDetail();
    }

    void finishDraft(int draftIndex)
    {
        if (draftIndex < 1) {
            return;
        }
        ensureItem(draftIndex);
        DraftItemData &item = m_items[draftIndex - 1];
        item.status = 2;
        item.progress = 100;
        for (DraftStepData &entry : item.steps) {
            if (entry.status == int(PipelineUi::StepStatus::Running)) {
                entry.status = int(PipelineUi::StepStatus::Done);
                entry.progress = 100;
            }
        }
        item.saved = true;
        if (m_pinned < 0) {
            m_activeIndex = draftIndex - 1;
        }
        updateRow(draftIndex - 1);
        updateHeader();
        refreshDetail();
    }

    void setFailed(int draftIndex, const QString &message)
    {
        if (draftIndex >= 1) {
            ensureItem(draftIndex);
            DraftItemData &item = m_items[draftIndex - 1];
            item.status = 3;
            updateRow(draftIndex - 1);
        }
        m_failed = true;
        updateHeader();
        refreshDetail();
        Q_UNUSED(message);
    }

    void setFinished()
    {
        m_finished = true;
        updateHeader();
        refreshDetail();
    }

    int doneCount() const
    {
        int count = 0;
        for (const DraftItemData &item : m_items) {
            if (item.status == 2) {
                ++count;
            }
        }
        return count;
    }
    int savedCount() const
    {
        int count = 0;
        for (const DraftItemData &item : m_items) {
            if (item.saved) {
                ++count;
            }
        }
        return count;
    }
    int total() const { return m_count; }
    int activeIndex() const { return m_activeIndex; }
    bool finished() const { return m_finished; }
    int stagePercent() const
    {
        if (m_items.isEmpty()) {
            return 0;
        }
        int sum = 0;
        for (const DraftItemData &item : m_items) {
            sum += item.progress;
        }
        return qBound(0, sum / m_items.size(), 100);
    }

private:
    void ensureItem(int index)
    {
        while (m_items.size() < index) {
            DraftItemData data;
            data.steps.resize(6);
            for (int i = 0; i < 6; ++i) {
                data.steps[i].status = int(PipelineUi::StepStatus::Waiting);
                data.steps[i].detail = QStringLiteral("等待上一步完成");
            }
            m_items.append(data);
            m_clocks.append(QElapsedTimer());
            auto *row = new DraftRowWidget(data, m_rowScroll->widget());
            const int itemIndex = m_items.size() - 1;
            row->onClick = [this, itemIndex] {
                m_pinned = itemIndex;
                m_activeIndex = itemIndex;
                updateRowSelection();
                refreshDetail();
            };
            m_rowLayout->insertWidget(m_rowLayout->count() - 1, row);
            m_rows.append(row);
        }
        m_count = qMax(m_count, index);
        m_emptyLabel->hide();
    }

    void updateRow(int index)
    {
        if (index < 0 || index >= m_rows.size() || index >= m_items.size()) {
            return;
        }
        m_rows.at(index)->updateData(m_items.at(index));
        updateRowSelection();
    }

    void updateRowSelection()
    {
        for (int i = 0; i < m_rows.size(); ++i) {
            m_rows.at(i)->setSelected(i == m_activeIndex);
        }
    }

    void updateHeader()
    {
        const int done = doneCount();
        const int saved = savedCount();
        m_headerTitle->setText(QStringLiteral("%1 条切片依次生成").arg(m_count));
        m_doneValue->setText(QStringLiteral("%1/%2").arg(done).arg(m_count));
        m_savedValue->setText(QStringLiteral("%1/%2").arg(saved).arg(m_count));
        m_readyLabel->setText(QStringLiteral("%1 / %2 条就绪").arg(done).arg(m_count));
        const bool showReady = done > 0 && done < m_count;
        m_readyTag->setVisible(showReady);
        if (showReady) {
            m_readyTag->setText(QStringLiteral("草稿就绪 %1 条").arg(done));
        }
    }

    void refreshDetail()
    {
        if (m_items.isEmpty() || m_activeIndex < 0 || m_activeIndex >= m_items.size()) {
            m_detailIndex->setText(QStringLiteral("01 / 01"));
            m_detailTitle->setText(QStringLiteral("等待生成剪映草稿"));
            m_detailMeta->setText(
                QStringLiteral("AI 选片完成后，会在这里逐条生成剪映草稿。"));
            m_detailTag->setText(QStringLiteral("排队中"));
            m_detailTag->setStyle(Theme::Text3, Qt::transparent, Theme::Border);
            m_detailProgress->setValue(0);
            m_detailProgress->setBarColor(Theme::Primary);
            m_detailPercent->setText(QStringLiteral("0%"));
            for (int i = 0; i < 6; ++i) {
                m_steps[i]->setData(int(PipelineUi::StepStatus::Waiting), 0,
                                    QStringLiteral("等待上一步完成"), 0);
            }
            m_detailRemaining->setText(QStringLiteral("等待开始"));
            return;
        }

        const DraftItemData &item = m_items.at(m_activeIndex);
        m_detailIndex->setText(QStringLiteral("%1 / %2")
                                   .arg(item.candidate.index, 2, 10, QLatin1Char('0'))
                                   .arg(m_count, 2, 10, QLatin1Char('0')));
        m_detailTitle->setText(item.candidate.title.isEmpty()
                                   ? QStringLiteral("直播切片")
                                   : item.candidate.title);
        m_detailMeta->setText(
            QStringLiteral("%1 · %2 · %3 段 · %4 条字幕")
                .arg(item.candidate.range,
                     PipelineUi::formatSpan(item.candidate.durationMs))
                .arg(qMax(1, item.candidate.rangeCount))
                .arg(item.candidate.captionCount));
        if (item.status == 2) {
            m_detailTag->setText(QStringLiteral("已生成"));
            m_detailTag->setStyle(Theme::Success, Theme::alpha(Theme::Success, 0.13),
                                  Theme::alpha(Theme::Success, 0.35));
            m_detailProgress->setBarColor(Theme::Success);
        } else if (item.status == 1) {
            m_detailTag->setText(QStringLiteral("生成中"));
            m_detailTag->setStyle(Theme::Primary, Theme::alpha(Theme::Primary, 0.13),
                                  Theme::alpha(Theme::Primary, 0.35));
            m_detailProgress->setBarColor(Theme::Primary);
        } else if (item.status == 3) {
            m_detailTag->setText(QStringLiteral("生成失败"));
            m_detailTag->setStyle(Theme::Danger, Theme::alpha(Theme::Danger, 0.13),
                                  Theme::alpha(Theme::Danger, 0.35));
            m_detailProgress->setBarColor(Theme::Danger);
        } else {
            m_detailTag->setText(QStringLiteral("排队中"));
            m_detailTag->setStyle(Theme::Text3, Qt::transparent, Theme::Border);
            m_detailProgress->setBarColor(Theme::Primary);
        }
        m_detailProgress->setValue(item.progress);
        m_detailPercent->setText(QStringLiteral("%1%").arg(item.progress));
        for (int i = 0; i < 6; ++i) {
            const DraftStepData &step = item.steps.at(i);
            m_steps[i]->setData(step.status, step.progress, step.detail,
                                step.durationMs);
        }
        if (item.status == 2) {
            m_detailRemaining->setText(QStringLiteral("草稿已就绪"));
        } else if (item.status == 1) {
            const qint64 elapsed = m_activeIndex < m_clocks.size()
                                       && m_clocks.at(m_activeIndex).isValid()
                                   ? m_clocks.at(m_activeIndex).elapsed()
                                   : 0;
            m_detailRemaining->setText(
                QStringLiteral("已用时 %1").arg(formatElapsedValue(elapsed)));
        } else if (item.status == 3) {
            m_detailRemaining->setText(QStringLiteral("生成失败"));
        } else {
            m_detailRemaining->setText(QStringLiteral("等待开始"));
        }
    }

    int m_count = 0;
    int m_pinned = -1;
    int m_activeIndex = -1;
    bool m_finished = false;
    bool m_failed = false;
    QVector<DraftItemData> m_items;
    QVector<QElapsedTimer> m_clocks;
    QVector<DraftRowWidget *> m_rows;

    QLabel *m_headerTitle = nullptr;
    QLabel *m_concurrency = nullptr;
    QLabel *m_doneValue = nullptr;
    QLabel *m_savedValue = nullptr;
    Tag *m_readyTag = nullptr;
    QLabel *m_readyLabel = nullptr;
    QLabel *m_emptyLabel = nullptr;
    QScrollArea *m_rowScroll = nullptr;
    QVBoxLayout *m_rowLayout = nullptr;
    ElidedLabel *m_detailTitle = nullptr;
    QLabel *m_detailIndex = nullptr;
    QLabel *m_detailMeta = nullptr;
    Tag *m_detailTag = nullptr;
    ProgressBar *m_detailProgress = nullptr;
    QLabel *m_detailPercent = nullptr;
    QScrollArea *m_stepScroll = nullptr;
    StepRow *m_steps[6] = {};
    QLabel *m_outputInfo = nullptr;
    QLabel *m_detailRemaining = nullptr;
};

// -----------------------------------------------------------------------------
// 底部状态栏
// -----------------------------------------------------------------------------

class FooterBar : public QWidget
{
public:
    explicit FooterBar(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        setFixedHeight(28);
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.fillRect(rect(), Theme::Surface1);
        painter.setPen(QPen(Theme::Border, 1.0));
        painter.drawLine(QPointF(0, 0.5), QPointF(width(), 0.5));
    }
};
// -----------------------------------------------------------------------------
// PipelinePage
// -----------------------------------------------------------------------------

PipelinePage::PipelinePage(QWidget *parent)
    : QWidget(parent)
{
    auto *root = new QVBoxLayout(this);
    root->setContentsMargins(0, 0, 0, 0);
    root->setSpacing(0);

    m_pages = new QStackedWidget(this);
    m_pages->setStyleSheet(QStringLiteral("QStackedWidget { background:transparent; }"));
    root->addWidget(m_pages);

    buildEmptyPage();
    buildRunPage();

    m_clockTimer = new QTimer(this);
    m_clockTimer->setInterval(500);
    connect(m_clockTimer, &QTimer::timeout, this, [this] {
        if (m_status == RunStatus::Running) {
            updateSummary();
        }
    });
}

void PipelinePage::buildEmptyPage()
{
    m_emptyPage = new QWidget(m_pages);
    auto *layout = new QVBoxLayout(m_emptyPage);
    layout->setContentsMargins(40, 40, 40, 40);
    layout->setSpacing(10);
    layout->addStretch(1);
    auto *bars = new EqBars(Theme::Primary, m_emptyPage);
    layout->addWidget(bars, 0, Qt::AlignHCenter);
    auto *title = Ui::label(QStringLiteral("还没有运行中的一键成片"), 16, 650,
                            Theme::Text1, m_emptyPage);
    title->setAlignment(Qt::AlignCenter);
    layout->addWidget(title);
    auto *desc = Ui::label(
        QStringLiteral("从源视频库选择一条视频开始，ASR 转写、AI 选片与草稿生成的"
                       "过程会实时展示在这里。"),
        12, 400, Theme::Text3, m_emptyPage);
    desc->setAlignment(Qt::AlignCenter);
    desc->setWordWrap(true);
    desc->setFixedWidth(400);
    const int descHeight = desc->heightForWidth(400);
    if (descHeight > 0) {
        desc->setFixedHeight(descHeight);
    }
    layout->addWidget(desc, 0, Qt::AlignHCenter);
    auto *buttons = new QHBoxLayout;
    buttons->setContentsMargins(0, 4, 0, 0);
    buttons->setSpacing(8);
    auto *start = new Button(QStringLiteral("开始一键成片"), Button::Primary, m_emptyPage);
    auto *material = new Button(QStringLiteral("去视频库"), Button::Default, m_emptyPage);
    connect(start, &QAbstractButton::clicked, this, &PipelinePage::startRunRequested);
    connect(material, &QAbstractButton::clicked, this, &PipelinePage::materialRequested);
    buttons->addStretch(1);
    buttons->addWidget(start);
    buttons->addWidget(material);
    buttons->addStretch(1);
    layout->addLayout(buttons);
    layout->addStretch(1);
    m_pages->addWidget(m_emptyPage);
}

void PipelinePage::buildRunPage()
{
    m_runPage = new QWidget(m_pages);
    auto *root = new QVBoxLayout(m_runPage);
    root->setContentsMargins(0, 0, 0, 0);
    root->setSpacing(0);

    auto *header = new QWidget(m_runPage);
    auto *headerLayout = new QVBoxLayout(header);
    headerLayout->setContentsMargins(kSidePadding, 14, kSidePadding, 12);
    headerLayout->setSpacing(0);

    auto *topRow = new QHBoxLayout;
    topRow->setContentsMargins(0, 0, 0, 0);
    topRow->setSpacing(12);

    auto *titles = new QVBoxLayout;
    titles->setContentsMargins(0, 0, 0, 0);
    titles->setSpacing(3);
    auto *titleRow = new QHBoxLayout;
    titleRow->setContentsMargins(0, 0, 0, 0);
    titleRow->setSpacing(8);
    m_title = new ElidedLabel(QStringLiteral("正在准备一键成片…"), header);
    m_title->setPixelSize(15);
    m_title->setFontWeight(650);
    m_title->setTextColor(Theme::Text1);
    m_title->setSizePolicy(QSizePolicy::Preferred, QSizePolicy::Fixed);
    titleRow->addWidget(m_title);
    m_titleChip = new Chip(QStringLiteral("一键成片中"), Chip::Run, header);
    titleRow->addWidget(m_titleChip);
    m_titleMeta = monoLabel(QStringLiteral("—"), 11, Theme::Text3, header);
    titleRow->addWidget(m_titleMeta);
    titleRow->addStretch(1);
    titles->addLayout(titleRow);

    auto *metaRow = new QHBoxLayout;
    metaRow->setContentsMargins(0, 0, 0, 0);
    metaRow->setSpacing(6);
    m_promptTextLabel = Ui::label(QStringLiteral("提示词：—"), 11, 400, Theme::Text3,
                                  header);
    metaRow->addWidget(m_promptTextLabel);
    auto addDot = [header, metaRow] {
        metaRow->addWidget(
            Ui::label(QStringLiteral("·"), 11, 400, Theme::BorderStrong, header));
    };
    addDot();
    metaRow->addWidget(Ui::label(QStringLiteral("已用时"), 11, 400, Theme::Text3,
                                 header));
    m_elapsedValue = monoLabel(QStringLiteral("00:00"), 11, Theme::Text2, header);
    m_elapsedValue->setFont(Theme::monoFont(11, 500));
    metaRow->addWidget(m_elapsedValue);
    m_doneDot = Ui::label(QStringLiteral("·"), 11, 400, Theme::BorderStrong, header);
    m_doneDot->hide();
    metaRow->addWidget(m_doneDot);
    m_doneStatus = Ui::label(QStringLiteral("全部步骤已完成"), 11, 400, Theme::Success,
                             header);
    m_doneStatus->hide();
    metaRow->addWidget(m_doneStatus);
    m_etaDot = Ui::label(QStringLiteral("·"), 11, 400, Theme::BorderStrong, header);
    metaRow->addWidget(m_etaDot);
    m_etaPrefix = Ui::label(QStringLiteral("预计还需"), 11, 400, Theme::Text3, header);
    metaRow->addWidget(m_etaPrefix);
    m_etaValue = monoLabel(QStringLiteral("—"), 11, Theme::Text2, header);
    m_etaValue->setFont(Theme::monoFont(11, 500));
    metaRow->addWidget(m_etaValue);
    m_hintDot = Ui::label(QStringLiteral("·"), 11, 400, Theme::BorderStrong, header);
    metaRow->addWidget(m_hintDot);
    m_backgroundHint =
        Ui::label(QStringLiteral("可切到其他页面，任务在后台继续"), 11, 400, Theme::Text3,
                  header);
    metaRow->addWidget(m_backgroundHint);
    metaRow->addStretch(1);
    titles->addLayout(metaRow);
    topRow->addLayout(titles, 1);

    auto *actions = new QHBoxLayout;
    actions->setContentsMargins(0, 0, 0, 0);
    actions->setSpacing(12);
    auto *overall = new QVBoxLayout;
    overall->setContentsMargins(0, 0, 0, 0);
    overall->setSpacing(2);
    auto *overallRow = new QHBoxLayout;
    overallRow->setContentsMargins(0, 0, 0, 0);
    overallRow->setSpacing(2);
    m_overallValue = monoLabel(QStringLiteral("0"), 22, Theme::Primary, header);
    m_overallValue->setFont(Theme::monoFont(22, 650));
    overallRow->addWidget(m_overallValue, 0, Qt::AlignBottom);
    m_overallUnit = Ui::label(QStringLiteral("%"), 10, 400, Theme::Text3, header);
    overallRow->addWidget(m_overallUnit, 0, Qt::AlignBottom);
    overall->addLayout(overallRow);
    auto *overallLabel = Ui::label(QStringLiteral("总进度"), 9, 400, Theme::Text3,
                                   header);
    overallLabel->setAlignment(Qt::AlignRight);
    overall->addWidget(overallLabel);
    actions->addLayout(overall);

    m_backstageButton = new Button(QStringLiteral("后台运行"), Button::Default, header);
    m_rerunButton = new Button(QStringLiteral("再跑一次"), Button::Default, header);
    m_resultsButton = new Button(QStringLiteral("查看成片"), Button::Primary, header);
    m_rerunButton->hide();
    m_resultsButton->hide();
    connect(m_backstageButton, &QAbstractButton::clicked, this,
            &PipelinePage::backgroundRequested);
    connect(m_rerunButton, &QAbstractButton::clicked, this,
            &PipelinePage::rerunRequested);
    connect(m_resultsButton, &QAbstractButton::clicked, this,
            &PipelinePage::viewResultsRequested);
    actions->addWidget(m_backstageButton);
    actions->addWidget(m_rerunButton);
    actions->addWidget(m_resultsButton);
    topRow->addLayout(actions);
    headerLayout->addLayout(topRow);

    m_stageRail = new StageRailWidget(header);
    m_stageRail->onFocus = [this](int index) { setFocusStage(index); };
    headerLayout->addSpacing(12);
    headerLayout->addWidget(m_stageRail);
    root->addWidget(header);
    root->addWidget(makeHLine(m_runPage));

    m_panels = new QStackedWidget(m_runPage);
    m_panels->setStyleSheet(QStringLiteral("QStackedWidget { background:transparent; }"));
    m_asrPanel = new AsrPanelWidget(m_panels);
    m_selectPanel = new SelectPanelWidget(m_panels);
    m_draftPanel = new DraftPanelWidget(m_panels);
    m_panels->addWidget(m_asrPanel);
    m_panels->addWidget(m_selectPanel);
    m_panels->addWidget(m_draftPanel);
    m_asrPanel->onChanged = [this] { updateSummary(); };
    root->addWidget(m_panels, 1);

    auto *footer = new FooterBar(m_runPage);
    auto *footerLayout = new QHBoxLayout(footer);
    footerLayout->setContentsMargins(kSidePadding, 0, kSidePadding, 0);
    footerLayout->setSpacing(10);
    m_footerMark = new StatusMark(StatusMark::Mode::Dot, 10, Theme::Primary, footer);
    footerLayout->addWidget(m_footerMark);
    m_footerLabel = new ElidedLabel(QStringLiteral("正在准备任务…"), footer);
    m_footerLabel->setPixelSize(10);
    m_footerLabel->setTextColor(Theme::Text2);
    footerLayout->addWidget(m_footerLabel, 1);
    footerLayout->addStretch(0);
    m_footerAsr = monoLabel(QStringLiteral("ASR 约 0%"), 10, Theme::Text3, footer);
    m_footerAi = monoLabel(QStringLiteral("AI 0 tokens"), 10, Theme::Text3, footer);
    m_footerDraft = monoLabel(QStringLiteral("草稿 0/0"), 10, Theme::Text3, footer);
    footerLayout->addWidget(m_footerAsr);
    footerLayout->addWidget(m_footerAi);
    footerLayout->addWidget(m_footerDraft);
    root->addWidget(footer);

    m_pages->addWidget(m_runPage);
}

bool PipelinePage::hasRun() const
{
    return m_status != RunStatus::Idle;
}

void PipelinePage::resetRun()
{
    m_asrPanel->reset();
    m_selectPanel->reset();
    m_draftPanel->reset();
    m_pickedCandidates.clear();

    m_status = RunStatus::Running;
    m_stage = PipelineUi::StageKey::Asr;
    m_focus = 0;
    for (int i = 0; i < 3; ++i) {
        m_stageStatus[i] = PipelineUi::StageStatus::Waiting;
    }
    m_stageStatus[0] = PipelineUi::StageStatus::Running;
    m_sourceTitle = QStringLiteral("正在准备一键成片…");
    m_promptLabel.clear();
    m_errorMessage.clear();
    m_sourceDurationMs = 0;
    m_finalElapsedMs = 0;
    m_clockOffsetMs = 0;
    m_batchCount = 0;
    m_finishedDrafts = 0;
    m_savedDrafts = 0;
    m_tokens = 0;
    m_selectClockStarted = false;
    m_selectClock.invalidate();
    m_summaryMode = false;
    m_summaryStagePercent = 0;
    m_summaryNote.clear();
    m_runClock.start();

    m_title->setText(m_sourceTitle);
    m_title->setMinimumWidth(qMin(460, m_title->sizeHint().width() + 8));
    m_titleMeta->setText(QStringLiteral("—"));
    m_promptTextLabel->setText(QStringLiteral("提示词：—"));
    m_titleChip->setText(QStringLiteral("一键成片中"));
    m_titleChip->setTone(Chip::Run);
    m_overallValue->setText(QStringLiteral("0"));
    setLabelColor(m_overallValue, Theme::Primary);
    setLabelColor(m_overallUnit, Theme::Text3);
    m_backstageButton->show();
    m_rerunButton->hide();
    m_resultsButton->hide();

    m_pages->setCurrentWidget(m_runPage);
    m_panels->setCurrentIndex(0);
    m_clockTimer->start();
    updateSummary();
}

void PipelinePage::handleEvent(const PipelineUi::Event &event)
{
    switch (event.type) {
    case PipelineUi::EventType::RunStarted:
        m_summaryMode = false;
        m_summaryNote.clear();
        m_sourceTitle = event.sourceTitle;
        m_promptLabel = event.promptLabel;
        m_sourceDurationMs = event.durationMs;
        m_batchCount = event.count;
        m_title->setText(m_sourceTitle.isEmpty()
                             ? QStringLiteral("一键成片")
                             : m_sourceTitle);
        m_title->setMinimumWidth(qMin(460, m_title->sizeHint().width() + 8));
        m_promptTextLabel->setText(QStringLiteral("提示词：%1")
                                       .arg(m_promptLabel.isEmpty()
                                                ? QStringLiteral("—")
                                                : m_promptLabel));
        m_stageStatus[0] = PipelineUi::StageStatus::Running;
        setStage(PipelineUi::StageKey::Asr);
        updateSummary();
        break;

    case PipelineUi::EventType::AsrPrepared:
        m_asrPanel->handlePrepared(event.step, event.durationMs, event.flag, event.text,
                                   event.jobId);
        if (event.durationMs > 0) {
            m_sourceDurationMs = event.durationMs;
        }
        if (m_stageStatus[0] != PipelineUi::StageStatus::Done) {
            m_stageStatus[0] = PipelineUi::StageStatus::Running;
        }
        updateSummary();
        break;

    case PipelineUi::EventType::AsrSubmitted:
        m_asrPanel->handleSubmitted(event.jobId, event.durationMs);
        if (event.durationMs > 0) {
            m_sourceDurationMs = qMax(m_sourceDurationMs, event.durationMs);
        }
        if (m_stageStatus[0] != PipelineUi::StageStatus::Done) {
            m_stageStatus[0] = PipelineUi::StageStatus::Running;
        }
        updateSummary();
        break;

    case PipelineUi::EventType::AsrResult:
        m_asrPanel->handleResult(event);
        if (event.durationMs > 0) {
            m_sourceDurationMs = qMax(m_sourceDurationMs, event.durationMs);
        }
        updateSummary();
        break;

    case PipelineUi::EventType::AsrFinished:
        m_asrPanel->handleFinished(event);
        if (event.durationMs > 0) {
            m_sourceDurationMs = qMax(m_sourceDurationMs, event.durationMs);
        }
        m_stageStatus[0] = PipelineUi::StageStatus::Done;
        updateSummary();
        break;

    case PipelineUi::EventType::SelectPlanned:
        m_batchCount = qMax(1, event.count);
        m_selectPanel->setPrompt(m_promptLabel);
        m_selectPanel->setPlanned(event.candidates, event.sentenceCount,
                                  event.charCount);
        m_stageStatus[1] = PipelineUi::StageStatus::Waiting;
        updateSummary();
        break;

    case PipelineUi::EventType::SelectBatchStarted:
        m_stageStatus[0] = PipelineUi::StageStatus::Done;
        m_stageStatus[1] = PipelineUi::StageStatus::Running;
        if (m_stage != PipelineUi::StageKey::Select) {
            setStage(PipelineUi::StageKey::Select);
        }
        if (!m_selectClockStarted) {
            m_selectClock.start();
            m_selectClockStarted = true;
        }
        m_selectPanel->startBatch(event.index, event.count, event.startMs, event.endMs,
                                  event.sentenceCount, event.text);
        updateSummary();
        break;

    case PipelineUi::EventType::SelectThought:
        m_selectPanel->appendThoughtDelta(event.index, event.count, event.text);
        updateSummary();
        break;

    case PipelineUi::EventType::SelectStatus:
        m_selectPanel->appendStatus(event.text);
        updateSummary();
        break;

    case PipelineUi::EventType::SelectBatchSkipped:
        m_stageStatus[1] = PipelineUi::StageStatus::Running;
        m_selectPanel->skipBatch(event.index, event.count, event.text, event.tokenCount);
        m_tokens = m_selectPanel->tokens();
        updateSummary();
        break;

    case PipelineUi::EventType::SelectBatchFinished:
        m_stageStatus[1] = PipelineUi::StageStatus::Running;
        if (!event.candidates.isEmpty()) {
            const PipelineUi::Candidate &candidate = event.candidates.first();
            m_selectPanel->finishBatch(candidate, event.tokenCount);
            m_pickedCandidates.append(candidate);
            m_asrPanel->setMarkers(m_pickedCandidates);
        }
        m_tokens = m_selectPanel->tokens();
        updateSummary();
        break;

    case PipelineUi::EventType::SelectFinished:
        m_selectPanel->finish(event.count, qMax(1, event.sentenceCount),
                              event.tokenCount);
        m_stageStatus[1] = PipelineUi::StageStatus::Done;
        if (event.count > 0) {
            m_batchCount = event.count;
        }
        m_tokens = m_selectPanel->tokens();
        updateSummary();
        break;

    case PipelineUi::EventType::DraftPlan:
        m_draftPanel->setPlan(event.candidates);
        m_stageStatus[2] = PipelineUi::StageStatus::Waiting;
        m_batchCount = qMax(m_batchCount, event.count);
        updateSummary();
        break;

    case PipelineUi::EventType::DraftStarted:
        m_stageStatus[1] = PipelineUi::StageStatus::Done;
        m_stageStatus[2] = PipelineUi::StageStatus::Running;
        if (m_stage != PipelineUi::StageKey::Draft) {
            setStage(PipelineUi::StageKey::Draft);
        }
        m_draftPanel->startDraft(event.index, event.count,
                                 event.candidates.isEmpty()
                                     ? PipelineUi::Candidate()
                                     : event.candidates.first());
        updateSummary();
        break;

    case PipelineUi::EventType::DraftStep:
        m_stageStatus[2] = event.status == int(PipelineUi::StepStatus::Failed)
                               ? PipelineUi::StageStatus::Failed
                               : PipelineUi::StageStatus::Running;
        m_draftPanel->setStep(event.index, event.step, event.status, event.percent,
                              event.detail, event.durationMs);
        m_finishedDrafts = m_draftPanel->doneCount();
        m_savedDrafts = m_draftPanel->savedCount();
        updateSummary();
        break;

    case PipelineUi::EventType::DraftFinished:
        m_draftPanel->finishDraft(event.index);
        m_finishedDrafts = m_draftPanel->doneCount();
        m_savedDrafts = m_draftPanel->savedCount();
        updateSummary();
        break;

    case PipelineUi::EventType::RunFinished:
        m_finalElapsedMs = m_clockOffsetMs + m_runClock.elapsed();
        m_clockTimer->stop();
        if (event.flag) {
            m_status = RunStatus::Done;
            for (int i = 0; i < 3; ++i) {
                m_stageStatus[i] = PipelineUi::StageStatus::Done;
            }
            m_draftPanel->setFinished();
            m_finishedDrafts = m_draftPanel->doneCount();
            m_savedDrafts = m_draftPanel->savedCount();
            m_titleChip->setText(QStringLiteral("已完成"));
            m_titleChip->setTone(Chip::Ok);
            m_backstageButton->hide();
            m_rerunButton->show();
            m_resultsButton->show();
        } else {
            m_status = RunStatus::Failed;
            m_errorMessage = event.text;
            PipelineUi::StageKey failedStage = PipelineUi::StageKey::Asr;
            if (m_draftPanel->total() > 0) {
                failedStage = PipelineUi::StageKey::Draft;
            } else if (m_selectPanel->started()) {
                failedStage = PipelineUi::StageKey::Select;
            }
            if (m_stageStatus[int(failedStage)] == PipelineUi::StageStatus::Waiting) {
                failedStage = m_stage;
            }
            m_stageStatus[int(failedStage)] = PipelineUi::StageStatus::Failed;
            if (failedStage == PipelineUi::StageKey::Asr) {
                m_asrPanel->handleFailed(m_errorMessage);
            } else if (failedStage == PipelineUi::StageKey::Select) {
                m_selectPanel->setFailed(m_errorMessage);
            } else {
                m_draftPanel->setFailed(m_draftPanel->activeIndex() + 1,
                                        m_errorMessage);
            }
            m_savedDrafts = m_draftPanel->savedCount();
            setFocusStage(int(failedStage));
            m_titleChip->setText(QStringLiteral("已失败"));
            m_titleChip->setTone(Chip::Error);
            m_backstageButton->hide();
            m_rerunButton->show();
            m_resultsButton->setVisible(m_savedDrafts > 0);
        }
        updateSummary();
        break;

    case PipelineUi::EventType::TaskSummary:
        resetRun();
        m_sourceTitle = event.sourceTitle;
        m_promptLabel = QStringLiteral("—");
        m_batchCount = 0;
        m_sourceDurationMs = 0;
        m_clockOffsetMs = qMax<qint64>(0, event.durationMs);
        m_runClock.start();
        m_summaryMode = true;
        m_summaryStagePercent = qBound(0, event.percent, 100);
        m_summaryNote = event.text;
        m_title->setText(m_sourceTitle.isEmpty() ? QStringLiteral("一键成片")
                                                 : m_sourceTitle);
        m_title->setMinimumWidth(qMin(460, m_title->sizeHint().width() + 8));
        m_promptTextLabel->setText(QStringLiteral("提示词：—"));
        m_status = RunStatus::Running;
        for (int i = 0; i < 3; ++i) {
            m_stageStatus[i] = i < event.step
                                   ? PipelineUi::StageStatus::Done
                                   : (i == event.step
                                          ? PipelineUi::StageStatus::Running
                                          : PipelineUi::StageStatus::Waiting);
        }
        setStage(static_cast<PipelineUi::StageKey>(qBound(0, event.step, 2)));
        if (event.step == 0) {
            m_asrPanel->setTaskSummary(event.percent, event.text, event.durationMs);
        }
        updateSummary();
        break;
    }
}

void PipelinePage::showTaskSummary(const QString &title, int step, const QString &message,
                                   int percent, qint64 elapsedMs)
{
    PipelineUi::Event event;
    event.type = PipelineUi::EventType::TaskSummary;
    event.sourceTitle = title;
    event.step = step;
    event.percent = percent;
    event.text = message;
    event.durationMs = elapsedMs;
    handleEvent(event);
}

void PipelinePage::showStep(int index)
{
    index = qBound(0, index, 2);
    if (!hasRun()) {
        resetRun();
        // 开发预览没有真实任务：按所选步骤构造一个一致的阶段状态。
        for (int i = 0; i < 3; ++i) {
            m_stageStatus[i] = i < index
                                   ? PipelineUi::StageStatus::Done
                                   : (i == index ? PipelineUi::StageStatus::Running
                                                 : PipelineUi::StageStatus::Waiting);
        }
        m_stage = static_cast<PipelineUi::StageKey>(index);
    }
    setFocusStage(index);
    updateSummary();
}

void PipelinePage::setStage(PipelineUi::StageKey stage)
{
    m_stage = stage;
    setFocusStage(int(stage));
}

void PipelinePage::setFocusStage(int index)
{
    m_focus = qBound(0, index, 2);
    if (m_panels) {
        m_panels->setCurrentIndex(m_focus);
    }
    updateStageRail();
}

void PipelinePage::updateSummary()
{
    const qint64 elapsed = m_status == RunStatus::Running
                               ? m_clockOffsetMs + m_runClock.elapsed()
                               : m_finalElapsedMs;
    m_elapsedValue->setText(formatElapsed(elapsed));

    const bool done = m_status == RunStatus::Done;
    const bool running = m_status == RunStatus::Running;
    m_doneDot->setVisible(done);
    m_doneStatus->setVisible(done);
    m_etaDot->setVisible(running);
    m_etaPrefix->setVisible(running);
    m_etaValue->setVisible(running);
    m_hintDot->setVisible(running);
    m_backgroundHint->setVisible(running);

    const int overall = overallProgress();
    m_overallValue->setText(QString::number(overall));
    if (m_status == RunStatus::Done) {
        setLabelColor(m_overallValue, Theme::Success);
        setLabelColor(m_overallUnit, Theme::Success);
    } else if (m_status == RunStatus::Failed) {
        setLabelColor(m_overallValue, Theme::Danger);
        setLabelColor(m_overallUnit, Theme::Danger);
    } else {
        setLabelColor(m_overallValue, Theme::Primary);
        setLabelColor(m_overallUnit, Theme::Text3);
    }

    bool canEstimate = false;
    if (m_status == RunStatus::Running) {
        if (m_stage == PipelineUi::StageKey::Asr) {
            canEstimate = m_asrPanel->percent() > 10;
        } else if (m_stage == PipelineUi::StageKey::Select) {
            canEstimate = m_selectPanel->batchDone() > 0 || m_selectPanel->finished();
        } else {
            canEstimate = m_draftPanel->doneCount() > 0
                || m_draftPanel->stagePercent() > 10;
        }
    }
    if (m_stage == PipelineUi::StageKey::Asr
        && m_stageStatus[0] == PipelineUi::StageStatus::Done) {
        canEstimate = false;
    }
    if (elapsed < 5000) {
        canEstimate = false;
    }
    if (m_status == RunStatus::Running && canEstimate && overall >= 3) {
        m_etaValue->setText(formatEta(etaSeconds()));
    } else if (m_status == RunStatus::Done) {
        m_etaValue->setText(QStringLiteral("0 秒"));
    } else {
        m_etaValue->setText(QStringLiteral("—"));
    }

    if (m_stageStatus[1] == PipelineUi::StageStatus::Running
        && !m_selectPanel->finished() && m_selectClockStarted
        && m_status == RunStatus::Running) {
        const qint64 seconds = m_selectClock.elapsed() / 1000;
        m_selectPanel->setTokenRate(
            seconds > 0 ? int(qMin<qint64>(9999, m_tokens / seconds)) : 0);
    }

    QString durationText = QStringLiteral("—");
    if (m_sourceDurationMs > 0) {
        durationText = MediaUtils::formatDuration(m_sourceDurationMs);
    }
    if (m_batchCount > 0) {
        m_titleMeta->setText(QStringLiteral("%1 · %2 条切片")
                                 .arg(durationText)
                                 .arg(m_batchCount));
    } else {
        m_titleMeta->setText(durationText);
    }

    m_finishedDrafts = m_draftPanel ? m_draftPanel->doneCount() : 0;
    m_savedDrafts = m_draftPanel ? m_draftPanel->savedCount() : 0;
    m_tokens = m_selectPanel ? m_selectPanel->tokens() : m_tokens;

    m_footerLabel->setText(footerText());
    m_footerLabel->setTextColor(m_status == RunStatus::Failed ? Theme::Danger
                                                              : Theme::Text2);
    m_footerAsr->setText(
        QStringLiteral("ASR 约 %1%").arg(m_asrPanel->percent()));
    m_footerAi->setText(QStringLiteral("AI %1 tokens").arg(formatNumber(m_tokens)));
    m_footerDraft->setText(
        QStringLiteral("草稿 %1/%2").arg(m_finishedDrafts).arg(m_draftPanel->total()));

    if (m_status == RunStatus::Done) {
        m_footerMark->setMode(StatusMark::Mode::Check);
        m_footerMark->setColor(Theme::Success);
    } else if (m_status == RunStatus::Failed) {
        m_footerMark->setMode(StatusMark::Mode::Cross);
        m_footerMark->setColor(Theme::Danger);
    } else {
        m_footerMark->setMode(StatusMark::Mode::Dot);
        m_footerMark->setColor(m_stage == PipelineUi::StageKey::Select ? Theme::Ai
                                                                       : Theme::Primary);
    }

    updateStageRail();
}

void PipelinePage::updateStageRail()
{
    if (!m_stageRail) {
        return;
    }
    PipelineUi::StageStatus statuses[3];
    int progress[3] = {0, 0, 0};
    QString meta[3];
    for (int i = 0; i < 3; ++i) {
        statuses[i] = m_stageStatus[i];
        meta[i] = stageMeta(static_cast<PipelineUi::StageKey>(i));
    }
    if (statuses[0] == PipelineUi::StageStatus::Done) {
        progress[0] = 100;
    } else if (statuses[0] != PipelineUi::StageStatus::Waiting) {
        progress[0] = m_asrPanel->percent();
    }
    if (statuses[1] == PipelineUi::StageStatus::Done) {
        progress[1] = 100;
    } else if (statuses[1] != PipelineUi::StageStatus::Waiting) {
        progress[1] = m_selectPanel->stagePercent();
    }
    if (statuses[2] == PipelineUi::StageStatus::Done) {
        progress[2] = 100;
    } else if (statuses[2] != PipelineUi::StageStatus::Waiting) {
        progress[2] = m_draftPanel->stagePercent();
    }
    if (m_summaryMode && statuses[int(m_stage)] == PipelineUi::StageStatus::Running) {
        progress[int(m_stage)] = m_summaryStagePercent;
    }
    m_stageRail->setState(statuses, progress, meta, m_focus);
}

QString PipelinePage::stageMeta(PipelineUi::StageKey stage) const
{
    if (m_summaryMode && stage == m_stage) {
        return m_summaryNote.isEmpty() ? QStringLiteral("正在处理…") : m_summaryNote;
    }
    switch (stage) {
    case PipelineUi::StageKey::Asr:
        if (m_stageStatus[0] == PipelineUi::StageStatus::Failed) {
            return QStringLiteral("转写失败");
        }
        if (m_stageStatus[0] == PipelineUi::StageStatus::Waiting) {
            return QStringLiteral("等待开始");
        }
        if (m_stageStatus[0] == PipelineUi::StageStatus::Done) {
            return QStringLiteral("%1 句 · %2% 置信度")
                .arg(formatNumber(m_asrPanel->sentences()),
                     confidenceValue(m_asrPanel->confidence()));
        }
        if (m_asrPanel->phase() == AsrPhase::Prepare) {
            return QStringLiteral("正在提交识别任务…");
        }
        if (m_asrPanel->phase() == AsrPhase::Result) {
            return QStringLiteral("识别完成 · 正在解析结果");
        }
        return QStringLiteral("已等待 %1 · 约 %2%")
            .arg(formatElapsedValue(m_asrPanel->elapsed()))
            .arg(m_asrPanel->percent());
    case PipelineUi::StageKey::Select:
        if (m_stageStatus[1] == PipelineUi::StageStatus::Failed) {
            return QStringLiteral("选片失败");
        }
        if (m_stageStatus[1] == PipelineUi::StageStatus::Waiting) {
            return QStringLiteral("等待 ASR 完成 · 约 %1%")
                .arg(m_asrPanel->percent());
        }
        if (m_stageStatus[1] == PipelineUi::StageStatus::Done) {
            return QStringLiteral("精选 %1 条 · 扫描 %2 段")
                .arg(m_selectPanel->pickedCount())
                .arg(m_selectPanel->batchCount());
        }
        return QStringLiteral("已锁定 %1 条 · %2 tokens")
            .arg(m_selectPanel->pickedCount())
            .arg(formatNumber(m_tokens));
    case PipelineUi::StageKey::Draft:
        if (m_stageStatus[2] == PipelineUi::StageStatus::Failed) {
            return QStringLiteral("草稿生成失败");
        }
        if (m_stageStatus[2] == PipelineUi::StageStatus::Waiting) {
            return QStringLiteral("等待 AI 选片完成");
        }
        if (m_stageStatus[2] == PipelineUi::StageStatus::Done) {
            return QStringLiteral("%1 条草稿 · 全部已保存")
                .arg(m_draftPanel->total());
        }
        return QStringLiteral("已完成 %1/%2 · 已保存 %3/%2")
            .arg(m_finishedDrafts)
            .arg(m_draftPanel->total())
            .arg(m_savedDrafts);
    }
    return QString();
}

QString PipelinePage::footerText() const
{
    if (m_summaryMode && m_status == RunStatus::Running && !m_summaryNote.isEmpty()) {
        return m_summaryNote;
    }
    if (m_status == RunStatus::Done) {
        return QStringLiteral("全部完成，%1 条剪映草稿已生成")
            .arg(m_finishedDrafts);
    }
    if (m_status == RunStatus::Failed) {
        return m_errorMessage.isEmpty() ? QStringLiteral("任务执行失败。")
                                        : m_errorMessage;
    }
    if (m_stage == PipelineUi::StageKey::Asr) {
        if (m_stageStatus[0] == PipelineUi::StageStatus::Done) {
            return QStringLiteral("转写完成，正在准备 AI 选片…");
        }
        return QStringLiteral("正在整片识别 · 进度为估算，可后台运行");
    }
    if (m_stage == PipelineUi::StageKey::Select) {
        if (m_stageStatus[1] == PipelineUi::StageStatus::Done) {
            return QStringLiteral("已锁定 %1 条切片，准备生成剪映草稿…")
                .arg(m_selectPanel->pickedCount());
        }
        return m_selectPanel->pickedCount() > 0
                   ? QStringLiteral("已锁定 %1 条切片，继续分析剩余分段")
                         .arg(m_selectPanel->pickedCount())
                   : QStringLiteral("正在读稿并建立候选池");
    }
    if (m_finishedDrafts > 0) {
        return QStringLiteral("%1 条草稿已就绪，其余依次生成中")
            .arg(m_finishedDrafts);
    }
    return QStringLiteral("%1 条草稿依次生成，保存到本地草稿库")
        .arg(m_draftPanel->total());
}

int PipelinePage::overallProgress() const
{
    if (m_status == RunStatus::Done) {
        return 100;
    }
    if (m_summaryMode) {
        return qBound(0, (int(m_stage) * 100 + m_summaryStagePercent) / 3, 99);
    }
    int asr = m_stageStatus[0] == PipelineUi::StageStatus::Done
                  ? 100
                  : m_asrPanel->percent();
    int select = m_stageStatus[1] == PipelineUi::StageStatus::Done
                     ? 100
                     : m_selectPanel->stagePercent();
    int draft = m_stageStatus[2] == PipelineUi::StageStatus::Done
                    ? 100
                    : m_draftPanel->stagePercent();
    return qBound(0, (asr + select + draft) / 3, 99);
}

qint64 PipelinePage::etaSeconds() const
{
    const int overall = overallProgress();
    if (overall <= 0) {
        return 0;
    }
    const qint64 elapsed = m_clockOffsetMs + m_runClock.elapsed();
    return qMax<qint64>(1, elapsed * (100 - overall) / overall / 1000);
}

QString PipelinePage::formatElapsed(qint64 milliseconds)
{
    return formatElapsedValue(milliseconds);
}

QString PipelinePage::formatEta(qint64 seconds)
{
    return formatEtaValue(seconds);
}
