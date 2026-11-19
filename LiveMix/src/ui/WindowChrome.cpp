#include "WindowChrome.h"

#include <QAction>
#include <QContextMenuEvent>
#include <QCoreApplication>
#include <QCursor>
#include <QEnterEvent>
#include <QEvent>
#include <QFontMetrics>
#include <QHBoxLayout>
#include <QIcon>
#include <QLabel>
#include <QMenu>
#include <QMouseEvent>
#include <QPainter>
#include <QPainterPath>
#include <QScrollArea>
#include <QScrollBar>
#include <QTimer>
#include <QVBoxLayout>
#include <QVariantAnimation>

namespace {

class TrafficLightButton : public QAbstractButton
{
public:
    enum class Role { Close, Minimize, Maximize };

    TrafficLightButton(Role role, const QColor &color, QWidget *parent = nullptr)
        : QAbstractButton(parent)
        , m_role(role)
        , m_color(color)
    {
        setFixedSize(12, 12);
        setCursor(Qt::ArrowCursor);
        setFocusPolicy(Qt::NoFocus);
        setAttribute(Qt::WA_Hover, true);
    }

    void setGlyphOpacity(qreal opacity)
    {
        if (qFuzzyCompare(m_glyphOpacity + 1.0, opacity + 1.0)) {
            return;
        }
        m_glyphOpacity = opacity;
        update();
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        painter.setPen(Qt::NoPen);
        painter.setBrush(m_color);
        painter.drawEllipse(QRectF(rect()));

        if (m_glyphOpacity <= 0.001) {
            return;
        }

        QColor ink;
        switch (m_role) {
        case Role::Close:
            ink = QColor("#691109");
            break;
        case Role::Minimize:
            ink = QColor("#8E591D");
            break;
        case Role::Maximize:
            ink = QColor("#286017");
            break;
        }

        painter.save();
        painter.setOpacity(m_glyphOpacity);
        painter.setBrush(ink);
        if (m_role == Role::Close) {
            painter.setPen(QPen(ink, 1.2, Qt::SolidLine, Qt::RoundCap));
            painter.drawLine(QPointF(3.6, 3.6), QPointF(8.4, 8.4));
            painter.drawLine(QPointF(8.4, 3.6), QPointF(3.6, 8.4));
        } else if (m_role == Role::Minimize) {
            painter.setPen(QPen(ink, 1.5, Qt::SolidLine, Qt::RoundCap));
            painter.drawLine(QPointF(2.9, 6.0), QPointF(9.1, 6.0));
        } else {
            // macOS 设计稿：两个对角实心三角，替代旧的加号。
            QPainterPath bottomRight;
            bottomRight.moveTo(9.0, 4.5);
            bottomRight.lineTo(9.0, 8.0);
            bottomRight.quadTo(9.0, 9.0, 8.0, 9.0);
            bottomRight.lineTo(4.5, 9.0);
            bottomRight.closeSubpath();
            QPainterPath topLeft;
            topLeft.moveTo(3.0, 7.5);
            topLeft.lineTo(3.0, 4.0);
            topLeft.quadTo(3.0, 3.0, 4.0, 3.0);
            topLeft.lineTo(7.5, 3.0);
            topLeft.closeSubpath();
            painter.setPen(Qt::NoPen);
            painter.drawPath(bottomRight);
            painter.drawPath(topLeft);
        }
        painter.restore();
    }

private:
    Role m_role;
    QColor m_color;
    qreal m_glyphOpacity = 0.0;
};

// 交通灯整组悬停时淡入符号，符合 macOS 的系统手势。
class TrafficLights : public QWidget
{
public:
    explicit TrafficLights(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        setFixedSize(52, 12);

        auto *layout = new QHBoxLayout(this);
        layout->setContentsMargins(0, 0, 0, 0);
        layout->setSpacing(8);

        m_close = new TrafficLightButton(TrafficLightButton::Role::Close, QColor("#FF5F57"),
                                         this);
        m_minimize = new TrafficLightButton(TrafficLightButton::Role::Minimize,
                                            QColor("#FEBC2E"), this);
        m_maximize = new TrafficLightButton(TrafficLightButton::Role::Maximize,
                                            QColor("#28C840"), this);
        m_close->setToolTip(QStringLiteral("关闭"));
        m_minimize->setToolTip(QStringLiteral("最小化"));
        m_maximize->setToolTip(QStringLiteral("最大化"));
        m_close->setAccessibleName(QStringLiteral("关闭"));
        m_minimize->setAccessibleName(QStringLiteral("最小化"));
        m_maximize->setAccessibleName(QStringLiteral("最大化"));
        layout->addWidget(m_close);
        layout->addWidget(m_minimize);
        layout->addWidget(m_maximize);

        m_animation = new QVariantAnimation(this);
        m_animation->setDuration(150);
        m_animation->setEasingCurve(QEasingCurve::OutCubic);
        connect(m_animation, &QVariantAnimation::valueChanged, this,
                [this](const QVariant &value) { applyOpacity(value.toReal()); });

        QWidget *const hoverTargets[] = {this, m_close, m_minimize, m_maximize};
        for (QWidget *widget : hoverTargets) {
            widget->installEventFilter(this);
        }
    }

    TrafficLightButton *closeButton() const { return m_close; }
    TrafficLightButton *minimizeButton() const { return m_minimize; }
    TrafficLightButton *maximizeButton() const { return m_maximize; }

protected:
    bool eventFilter(QObject *watched, QEvent *event) override
    {
        if (event->type() == QEvent::Enter || event->type() == QEvent::Leave) {
            // 在三个按钮之间移动时 Enter/Leave 顺序不稳定，下一轮事件循环
            // 再按光标与整组区域的关系决定悬停状态。
            QTimer::singleShot(0, this, [this] { updateHoverState(); });
        }
        return QWidget::eventFilter(watched, event);
    }

private:
    void updateHoverState()
    {
        const bool hovered = rect().contains(mapFromGlobal(QCursor::pos()));
        if (hovered == m_hovered) {
            return;
        }
        m_hovered = hovered;
        m_animation->stop();
        m_animation->setStartValue(m_opacity);
        m_animation->setEndValue(hovered ? 1.0 : 0.0);
        m_animation->start();
    }

    void applyOpacity(qreal opacity)
    {
        m_opacity = opacity;
        m_close->setGlyphOpacity(opacity);
        m_minimize->setGlyphOpacity(opacity);
        m_maximize->setGlyphOpacity(opacity);
    }

    TrafficLightButton *m_close = nullptr;
    TrafficLightButton *m_minimize = nullptr;
    TrafficLightButton *m_maximize = nullptr;
    QVariantAnimation *m_animation = nullptr;
    qreal m_opacity = 0.0;
    bool m_hovered = false;
};

class LogoMark : public QWidget
{
public:
    explicit LogoMark(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        setFixedSize(28, 28);
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        const QIcon brand = Icons::brandIcon();
        if (!brand.isNull()) {
            brand.paint(&painter, QRect(0, 0, 28, 28));
            return;
        }
        Icons::paintLogo(&painter, QRectF(rect()), Theme::Primary, Theme::OnAccent);
    }
};

class WindowsAppIconButton : public QAbstractButton
{
public:
    explicit WindowsAppIconButton(QWidget *parent = nullptr)
        : QAbstractButton(parent)
    {
        setFixedSize(24, 24);
        setCursor(Qt::PointingHandCursor);
        setFocusPolicy(Qt::NoFocus);
        setAttribute(Qt::WA_Hover, true);
        setToolTip(QStringLiteral("LiveMix Studio"));
        setAccessibleName(QStringLiteral("应用程序系统菜单"));
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        if (underMouse() || isDown()) {
            painter.setPen(Qt::NoPen);
            painter.setBrush(QColor(255, 255, 255, isDown() ? 12 : 20));
            painter.drawRoundedRect(QRectF(rect()), 4, 4);
        }

        const QIcon brand = Icons::brandIcon();
        if (!brand.isNull()) {
            brand.paint(&painter, QRect((width() - 16) / 2, (height() - 16) / 2, 16, 16));
        }
    }

    void enterEvent(QEnterEvent *event) override
    {
        update();
        QAbstractButton::enterEvent(event);
    }

    void leaveEvent(QEvent *event) override
    {
        update();
        QAbstractButton::leaveEvent(event);
    }
};

QLabel *shortcutChip(const QString &text, QWidget *parent)
{
    auto *chip = new QLabel(text, parent);
    chip->setFont(Theme::monoFont(11));
    chip->setStyleSheet(QStringLiteral(
        "color:#62697E;background:transparent;border:1px solid #242938;"
        "border-radius:4px;padding:1px 6px;"));
    chip->setAttribute(Qt::WA_TransparentForMouseEvents, true);
    chip->setSizePolicy(QSizePolicy::Fixed, QSizePolicy::Fixed);
    chip->setAlignment(Qt::AlignCenter);
    return chip;
}

QPainterPath roundedPath(const QRectF &rect, qreal topRadius, qreal bottomRadius)
{
    QPainterPath path;
    path.moveTo(rect.left(), rect.top() + topRadius);
    if (topRadius > 0) {
        path.quadTo(rect.topLeft(), QPointF(rect.left() + topRadius, rect.top()));
    }
    path.lineTo(rect.right() - topRadius, rect.top());
    if (topRadius > 0) {
        path.quadTo(rect.topRight(), QPointF(rect.right(), rect.top() + topRadius));
    }
    path.lineTo(rect.right(), rect.bottom() - bottomRadius);
    if (bottomRadius > 0) {
        path.quadTo(rect.bottomRight(), QPointF(rect.right() - bottomRadius, rect.bottom()));
    }
    path.lineTo(rect.left() + bottomRadius, rect.bottom());
    if (bottomRadius > 0) {
        path.quadTo(rect.bottomLeft(), QPointF(rect.left(), rect.bottom() - bottomRadius));
    }
    path.lineTo(rect.left(), rect.top() + topRadius);
    path.closeSubpath();
    return path;
}

} // namespace

// -----------------------------------------------------------------------------
// WindowCaptionButton（Windows 11 右上角窗口控件）
// -----------------------------------------------------------------------------

class WindowCaptionButton : public QAbstractButton
{
public:
    enum class Role { Minimize, Maximize, Close };

    explicit WindowCaptionButton(Role role, QWidget *parent = nullptr)
        : QAbstractButton(parent)
        , m_role(role)
    {
        setFixedSize(46, 32);
        setCursor(Qt::ArrowCursor);
        setFocusPolicy(Qt::NoFocus);
        setAttribute(Qt::WA_Hover, true);
        setContextMenuPolicy(Qt::PreventContextMenu);
        setToolTip(m_role == Role::Minimize ? QStringLiteral("最小化")
                   : m_role == Role::Close  ? QStringLiteral("关闭")
                                            : QStringLiteral("最大化"));
        setAccessibleName(toolTip());
    }

    void setMaximized(bool maximized)
    {
        if (m_maximized == maximized) {
            return;
        }
        m_maximized = maximized;
        setToolTip(maximized ? QStringLiteral("向下还原") : QStringLiteral("最大化"));
        setAccessibleName(toolTip());
        update();
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);

        const bool active = underMouse() || isDown();
        QColor background = Qt::transparent;
        QColor foreground = Theme::Text2;
        if (m_role == Role::Close) {
            if (isDown()) {
                background = QColor("#B22A20");
            } else if (underMouse()) {
                background = QColor("#C42B1C");
            }
            if (active) {
                foreground = Qt::white;
            }
        } else {
            if (isDown()) {
                background = QColor(255, 255, 255, 10);
            } else if (underMouse()) {
                background = QColor(255, 255, 255, 18);
            }
            if (active) {
                foreground = Theme::Text1;
            }
        }
        if (background.alpha() > 0) {
            painter.fillRect(rect(), background);
        }

        // 设计稿使用 10×10、1px 描边的 Windows 11 系统字形。
        const QRectF glyph(rect().center().x() - 5.0, rect().center().y() - 5.0, 10.0, 10.0);
        QPen pen(foreground, 1.0);
        pen.setJoinStyle(Qt::MiterJoin);
        painter.setPen(pen);
        painter.setBrush(Qt::NoBrush);
        const qreal x = glyph.left();
        const qreal y = glyph.top();

        switch (m_role) {
        case Role::Minimize:
            painter.drawLine(QPointF(x, y + 5.5), QPointF(x + 10.0, y + 5.5));
            break;
        case Role::Maximize:
            if (!m_maximized) {
                painter.drawRect(QRectF(x + 0.5, y + 0.5, 9.0, 9.0));
            } else {
                painter.drawRect(QRectF(x + 2.5, y + 2.5, 7.0, 7.0));
                painter.drawLine(QPointF(x + 0.5, y + 0.5), QPointF(x + 7.5, y + 0.5));
                painter.drawLine(QPointF(x + 7.5, y + 0.5), QPointF(x + 7.5, y + 2.5));
                painter.drawLine(QPointF(x + 0.5, y + 0.5), QPointF(x + 0.5, y + 7.5));
                painter.drawLine(QPointF(x + 0.5, y + 7.5), QPointF(x + 2.5, y + 7.5));
            }
            break;
        case Role::Close:
            painter.drawLine(QPointF(x + 0.5, y + 0.5), QPointF(x + 9.5, y + 9.5));
            painter.drawLine(QPointF(x + 9.5, y + 0.5), QPointF(x + 0.5, y + 9.5));
            break;
        }
    }

    void enterEvent(QEnterEvent *event) override
    {
        update();
        QAbstractButton::enterEvent(event);
    }

    void leaveEvent(QEvent *event) override
    {
        update();
        QAbstractButton::leaveEvent(event);
    }

    void mousePressEvent(QMouseEvent *event) override
    {
        QAbstractButton::mousePressEvent(event);
        update();
    }

    void mouseReleaseEvent(QMouseEvent *event) override
    {
        QAbstractButton::mouseReleaseEvent(event);
        update();
    }

private:
    Role m_role;
    bool m_maximized = false;
};

// -----------------------------------------------------------------------------
// WindowsTitleText（页面名 + 品牌名，仅页面名按可用宽度省略）
// -----------------------------------------------------------------------------

class WindowsTitleText : public QWidget
{
public:
    explicit WindowsTitleText(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
        setFixedHeight(QFontMetrics(Theme::font(12)).height() + 2);
        setAttribute(Qt::WA_TransparentForMouseEvents, true);
    }

    void setDocumentName(const QString &name)
    {
        m_name = name;
        setToolTip(name);
        update();
    }

    QSize sizeHint() const override
    {
        const QFontMetrics metrics(Theme::font(12));
        return QSize(metrics.horizontalAdvance(m_name)
                         + metrics.horizontalAdvance(separatorText())
                         + metrics.horizontalAdvance(studioText()),
                     height());
    }

    QSize minimumSizeHint() const override { return QSize(0, height()); }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::TextAntialiasing, true);
        const QFont font = Theme::font(12);
        const QFontMetrics metrics(font);
        painter.setFont(font);

        const QString separator = separatorText();
        const QString studio = studioText();
        const int suffixWidth = metrics.horizontalAdvance(separator)
            + metrics.horizontalAdvance(studio);
        const int pageWidth = qMax(0, width() - suffixWidth);
        const QString page =
            metrics.elidedText(m_name, Qt::ElideRight, pageWidth);
        const QRect pageRect(0, 0, pageWidth, height());
        painter.setPen(Theme::Text1);
        painter.drawText(pageRect, Qt::AlignLeft | Qt::AlignVCenter, page);

        const int pageAdvance = metrics.horizontalAdvance(page);
        const QRect separatorRect(pageAdvance, 0, metrics.horizontalAdvance(separator),
                                  height());
        painter.setPen(Theme::Text3);
        painter.drawText(separatorRect, Qt::AlignLeft | Qt::AlignVCenter, separator);
        const QRect studioRect(separatorRect.right() + 1, 0, metrics.horizontalAdvance(studio),
                               height());
        painter.setPen(Theme::Text2);
        painter.drawText(studioRect, Qt::AlignLeft | Qt::AlignVCenter, studio);
    }

private:
    static QString separatorText() { return QStringLiteral(" - "); }
    static QString studioText() { return QStringLiteral("LiveMix Studio"); }

    QString m_name = QStringLiteral("工作台");
};

// -----------------------------------------------------------------------------
// StatusDot
// -----------------------------------------------------------------------------

StatusDot::StatusDot(int diameter, const QColor &color, QWidget *parent)
    : QWidget(parent)
    , m_color(color)
    , m_glow(diameter <= 7)
{
    setFixedSize(diameter, diameter);
    setAttribute(Qt::WA_TransparentForMouseEvents, true);
}

void StatusDot::setColor(const QColor &color)
{
    m_color = color;
    update();
}

void StatusDot::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    painter.setRenderHint(QPainter::Antialiasing, true);
    if (m_glow) {
        painter.setPen(Qt::NoPen);
        painter.setBrush(Theme::alpha(m_color, 0.18));
        painter.drawEllipse(QRectF(rect()).adjusted(-1.5, -1.5, 1.5, 1.5));
    }
    painter.setPen(Qt::NoPen);
    painter.setBrush(m_color);
    painter.drawEllipse(QRectF(rect()));
}

// -----------------------------------------------------------------------------
// Page
// -----------------------------------------------------------------------------

Page::Page(QWidget *parent)
    : QWidget(parent)
{
    m_rootLayout = new QVBoxLayout(this);
    m_rootLayout->setContentsMargins(0, 0, 0, 0);
    m_rootLayout->setSpacing(0);

    m_scrollArea = new QScrollArea(this);
    m_scrollArea->setFrameShape(QFrame::NoFrame);
    m_scrollArea->setWidgetResizable(true);
    m_scrollArea->setHorizontalScrollBarPolicy(Qt::ScrollBarAsNeeded);
    m_scrollArea->setVerticalScrollBarPolicy(Qt::ScrollBarAsNeeded);
    m_scrollArea->setStyleSheet(QStringLiteral(
        "QScrollArea { background: transparent; border: none; }"
        "QScrollArea > QWidget > QWidget { background: transparent; }"));
    m_scrollArea->viewport()->setAutoFillBackground(false);

    m_content = new QWidget;
    m_content->setAttribute(Qt::WA_TranslucentBackground, true);
    m_content->setAutoFillBackground(false);
    m_content->setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Preferred);
    m_scrollArea->setWidget(m_content);

    m_bodyLayout = new QVBoxLayout(m_content);
    m_bodyLayout->setContentsMargins(22, 18, 22, 18);
    m_bodyLayout->setSpacing(0);
    m_bodyLayout->setAlignment(Qt::AlignTop);

    m_rootLayout->addWidget(m_scrollArea);
}

void Page::scrollToTop()
{
    if (m_scrollArea && m_scrollArea->verticalScrollBar()) {
        m_scrollArea->verticalScrollBar()->setValue(0);
    }
}

void Page::setFixedHeader(QWidget *header)
{
    if (!header || !m_rootLayout) {
        return;
    }

    // 页头与滚动内容共用同一套左右边距；顶部留白由页头自己承载。
    auto *host = new QWidget(this);
    auto *hostLayout = new QVBoxLayout(host);
    hostLayout->setContentsMargins(22, 18, 22, 0);
    hostLayout->setSpacing(0);
    hostLayout->addWidget(header);
    m_rootLayout->insertWidget(0, host);

    // 内容区不再重复保留 18px 顶部留白；页头底部已有 16px 间距。
    m_bodyLayout->setContentsMargins(22, 0, 22, 18);
}

// -----------------------------------------------------------------------------
// PageHeader
// -----------------------------------------------------------------------------

PageHeader::PageHeader(const QString &title, const QString &descriptionHtml, QWidget *parent)
    : QWidget(parent)
{
    auto *root = new QHBoxLayout(this);
    root->setContentsMargins(0, 0, 0, 16);
    root->setSpacing(12);

    auto *titles = new QVBoxLayout;
    titles->setContentsMargins(0, 0, 0, 0);
    titles->setSpacing(3);

    m_title = Ui::label(title, 17, 650, Theme::Text1);
    m_title->setMinimumHeight(26);
    m_description = Ui::richLabel(descriptionHtml, 12, Theme::Text2);
    m_description->setMinimumHeight(19);
    m_description->setWordWrap(true);
    m_description->setVisible(!descriptionHtml.isEmpty());
    titles->addWidget(m_title);
    titles->addWidget(m_description);

    root->addLayout(titles, 1);

    auto *actionsHost = new QWidget(this);
    m_actionsLayout = new QHBoxLayout(actionsHost);
    m_actionsLayout->setContentsMargins(0, 0, 0, 0);
    m_actionsLayout->setSpacing(8);
    actionsHost->setSizePolicy(QSizePolicy::Fixed, QSizePolicy::Preferred);
    root->addWidget(actionsHost, 0, Qt::AlignVCenter);
}

void PageHeader::setTitle(const QString &title)
{
    m_title->setText(title);
}

void PageHeader::setDescription(const QString &descriptionHtml)
{
    m_description->setText(descriptionHtml);
    m_description->setVisible(!descriptionHtml.isEmpty());
}

void PageHeader::addAction(QWidget *widget)
{
    m_actionsLayout->addWidget(widget);
}

// -----------------------------------------------------------------------------
// TitleBar
// -----------------------------------------------------------------------------

TitleBar::TitleBar(QWidget *parent)
    : QWidget(parent)
    , m_windows(Theme::isWindowsWindowStyle())
{
    setFixedHeight(m_windows ? 32 : 38);
    setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
    setMouseTracking(true);

    auto *root = new QHBoxLayout(this);
    root->setContentsMargins(0, 0, 0, 0);
    root->setSpacing(0);
    if (m_windows) {
        buildWindowsChrome(root);
    } else {
        buildMacChrome(root);
    }
}

void TitleBar::buildMacChrome(QHBoxLayout *root)
{
    root->setContentsMargins(14, 0, 14, 0);
    root->setSpacing(12);

    auto *traffic = new TrafficLights(this);
    root->addWidget(traffic);

    auto *titleHost = new QWidget(this);
    auto *titleLayout = new QHBoxLayout(titleHost);
    titleLayout->setContentsMargins(0, 0, 0, 0);
    titleLayout->setSpacing(8);
    titleHost->setAttribute(Qt::WA_TransparentForMouseEvents, true);
    auto *dot = new StatusDot(6, Theme::Success, titleHost);
    auto *brand = Ui::label(QStringLiteral("LiveMix Studio"), 12, 600, Theme::Text1, titleHost);
    // 初始不展示项目名；进入结果页后由 MainWindow 填入真实源视频标题。
    m_project = Ui::label(QString(), 12, 400, Theme::Text2, titleHost);
    m_project->setVisible(false);
    titleLayout->addWidget(dot);
    titleLayout->addWidget(brand);
    titleLayout->addWidget(m_project);
    root->addWidget(titleHost);
    root->addStretch(1);

    root->addWidget(shortcutChip(
        QStringLiteral("%1 新建素材").arg(Theme::shortcutText(QStringLiteral("N"))), this));
    root->addWidget(shortcutChip(
        QStringLiteral("%1 全局搜索").arg(Theme::shortcutText(QStringLiteral("K"))), this));

    connect(traffic->closeButton(), &QAbstractButton::clicked, this,
            &TitleBar::closeRequested);
    connect(traffic->minimizeButton(), &QAbstractButton::clicked, this,
            &TitleBar::minimizeRequested);
    connect(traffic->maximizeButton(), &QAbstractButton::clicked, this,
            &TitleBar::maximizeRequested);
}

void TitleBar::buildWindowsChrome(QHBoxLayout *root)
{
    // Windows 11 单层标题栏：左侧应用图标 + 文档名，右侧系统窗口控件。
    root->setContentsMargins(8, 0, 0, 0);
    root->setSpacing(0);

    auto *iconButton = new WindowsAppIconButton(this);
    m_iconButton = iconButton;
    iconButton->setContextMenuPolicy(Qt::CustomContextMenu);
    connect(iconButton, &QAbstractButton::clicked, this, &TitleBar::showSystemMenuAtIcon);
    connect(iconButton, &QWidget::customContextMenuRequested, this,
            [this](const QPoint &) { showSystemMenuAtIcon(); });
    root->addWidget(iconButton, 0, Qt::AlignVCenter);
    root->addSpacing(6);

    m_pageTitle = new WindowsTitleText(this);
    root->addWidget(m_pageTitle, 1, Qt::AlignVCenter);
    root->addSpacing(8);

    root->addWidget(shortcutChip(
        QStringLiteral("%1 新建素材").arg(Theme::shortcutText(QStringLiteral("N"))), this),
        0, Qt::AlignVCenter);
    root->addSpacing(6);
    root->addWidget(shortcutChip(
        QStringLiteral("%1 全局搜索").arg(Theme::shortcutText(QStringLiteral("K"))), this),
        0, Qt::AlignVCenter);
    root->addSpacing(8);

    auto *minimize = new WindowCaptionButton(WindowCaptionButton::Role::Minimize, this);
    m_maximizeButton = new WindowCaptionButton(WindowCaptionButton::Role::Maximize, this);
    auto *close = new WindowCaptionButton(WindowCaptionButton::Role::Close, this);
    root->addWidget(minimize);
    root->addWidget(m_maximizeButton);
    root->addWidget(close);

    connect(minimize, &QAbstractButton::clicked, this, &TitleBar::minimizeRequested);
    connect(m_maximizeButton, &QAbstractButton::clicked, this, &TitleBar::maximizeRequested);
    connect(close, &QAbstractButton::clicked, this, &TitleBar::closeRequested);
}

void TitleBar::setDocumentTitle(const QString &documentName, const QString &macSuffix)
{
    if (m_pageTitle) {
        m_pageTitle->setDocumentName(documentName.isEmpty() ? QStringLiteral("工作台")
                                                            : documentName);
    }
    if (m_project) {
        m_project->setText(macSuffix);
        m_project->setVisible(!macSuffix.isEmpty());
    }
}

void TitleBar::setRounded(bool rounded)
{
    m_rounded = rounded;
    update();
}

void TitleBar::setMaximized(bool maximized)
{
    m_maximized = maximized;
    if (m_maximizeButton) {
        m_maximizeButton->setMaximized(maximized);
    }
}

void TitleBar::showSystemMenuAtIcon()
{
    if (!m_windows || !m_iconButton) {
        return;
    }
    openSystemMenu(m_iconButton->mapToGlobal(QPoint(0, m_iconButton->height() + 2)));
}

void TitleBar::openSystemMenu(const QPoint &globalPos)
{
    if (!m_windows) {
        return;
    }

    QMenu menu(this);
    QAction *restore = menu.addAction(QStringLiteral("还原(R)"));
    restore->setEnabled(m_maximized);
    QAction *move = menu.addAction(QStringLiteral("移动(M)"));
    move->setEnabled(false);
    QAction *size = menu.addAction(QStringLiteral("大小(S)"));
    size->setEnabled(false);
    QAction *minimize = menu.addAction(QStringLiteral("最小化(N)"));
    QAction *maximize = menu.addAction(QStringLiteral("最大化(X)"));
    maximize->setEnabled(!m_maximized);
    menu.addSeparator();
    QAction *close = menu.addAction(QStringLiteral("关闭(C)"));

    QAction *chosen = menu.exec(globalPos);
    if (chosen == restore || chosen == maximize) {
        emit maximizeRequested();
    } else if (chosen == minimize) {
        emit minimizeRequested();
    } else if (chosen == close) {
        emit closeRequested();
    }
}

void TitleBar::contextMenuEvent(QContextMenuEvent *event)
{
    if (m_windows) {
        openSystemMenu(event->globalPos());
        event->accept();
        return;
    }
    QWidget::contextMenuEvent(event);
}

void TitleBar::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    painter.setRenderHint(QPainter::Antialiasing, true);
    const qreal radius = m_rounded ? (m_windows ? 7.0 : 11.0) : 0.0;
    painter.setPen(Qt::NoPen);
    painter.setBrush(Theme::Surface1);
    painter.drawPath(roundedPath(QRectF(rect()), radius, 0));

    painter.setPen(QPen(Theme::Border, 1.0));
    painter.drawLine(QPointF(0, height() - 0.5), QPointF(width(), height() - 0.5));
}

void TitleBar::mousePressEvent(QMouseEvent *event)
{
    if (event->button() == Qt::LeftButton) {
        m_dragging = true;
        m_dragOffset = event->globalPosition().toPoint() - window()->frameGeometry().topLeft();
        event->accept();
        return;
    }
    QWidget::mousePressEvent(event);
}

void TitleBar::mouseDoubleClickEvent(QMouseEvent *event)
{
    if (event->button() == Qt::LeftButton) {
        emit maximizeRequested();
        event->accept();
        return;
    }
    QWidget::mouseDoubleClickEvent(event);
}

void TitleBar::mouseMoveEvent(QMouseEvent *event)
{
    if (m_dragging && (event->buttons() & Qt::LeftButton) && !window()->isMaximized()) {
        window()->move(event->globalPosition().toPoint() - m_dragOffset);
        event->accept();
        return;
    }
    QWidget::mouseMoveEvent(event);
}

void TitleBar::mouseReleaseEvent(QMouseEvent *event)
{
    m_dragging = false;
    QWidget::mouseReleaseEvent(event);
}

// -----------------------------------------------------------------------------
// Sidebar
// -----------------------------------------------------------------------------

Sidebar::Sidebar(QWidget *parent)
    : QWidget(parent)
{
    setFixedWidth(196);
    setSizePolicy(QSizePolicy::Fixed, QSizePolicy::Expanding);

    auto *root = new QVBoxLayout(this);
    root->setContentsMargins(8, 10, 8, 8);
    root->setSpacing(0);

    auto *brandRow = new QWidget(this);
    auto *brandLayout = new QHBoxLayout(brandRow);
    brandLayout->setContentsMargins(8, 6, 8, 14);
    brandLayout->setSpacing(9);
    auto *logo = new LogoMark(brandRow);
    auto *names = new QVBoxLayout;
    names->setContentsMargins(0, 0, 0, 0);
    names->setSpacing(0);
    auto *name = Ui::label(QStringLiteral("LiveMix"), 13, 650, Theme::Text1, brandRow);
    auto *tagline = Ui::label(QStringLiteral("直播切片工作台"), 10, 400, Theme::Text3, brandRow);
    QFont taglineFont = tagline->font();
    taglineFont.setLetterSpacing(QFont::AbsoluteSpacing, 1.0);
    tagline->setFont(taglineFont);
    names->addWidget(name);
    names->addWidget(tagline);
    brandLayout->addWidget(logo, 0, Qt::AlignTop);
    brandLayout->addLayout(names);
    brandLayout->addStretch(1);
    root->addWidget(brandRow);

    struct NavSpec {
        const char *id;
        Icons::Glyph glyph;
        const char *text;
        const char *badge;
    };
    const NavSpec navSpecs[] = {
        {"dash", Icons::Glyph::Dashboard, "工作台", ""},
        {"material", Icons::Glyph::Library, "源视频库", ""},
        {"prompt", Icons::Glyph::Prompt, "提示词管理", ""},
        {"tasks", Icons::Glyph::Tasks, "任务中心", ""},
    };

    for (const auto &spec : navSpecs) {
        auto *item = new NavItem(spec.glyph, QString::fromUtf8(spec.text),
                                 QString::fromUtf8(spec.badge), this);
        m_items.insert(QString::fromLatin1(spec.id), item);
        root->addWidget(item);
        root->addSpacing(2);
        const QString id = QString::fromLatin1(spec.id);
        connect(item, &QAbstractButton::clicked, this, [this, id] {
            setActivePage(id);
            emit pageRequested(id);
        });
    }

    auto *sepWrap = new QWidget(this);
    auto *sepLayout = new QVBoxLayout(sepWrap);
    sepLayout->setContentsMargins(6, 8, 6, 8);
    sepLayout->setSpacing(0);
    sepLayout->addWidget(Ui::separator(false, sepWrap));
    root->addWidget(sepWrap);

    const NavSpec secondarySpecs[] = {
        {"settings", Icons::Glyph::Settings, "设置", ""},
    };
    for (const auto &spec : secondarySpecs) {
        auto *item = new NavItem(spec.glyph, QString::fromUtf8(spec.text),
                                 QString::fromUtf8(spec.badge), this);
        m_items.insert(QString::fromLatin1(spec.id), item);
        root->addWidget(item);
        root->addSpacing(2);
        const QString id = QString::fromLatin1(spec.id);
        connect(item, &QAbstractButton::clicked, this, [this, id] {
            setActivePage(id);
            emit pageRequested(id);
        });
    }

    root->addStretch(1);

    auto *foot = new QWidget(this);
    auto *footLayout = new QVBoxLayout(foot);
    footLayout->setContentsMargins(8, 8, 8, 8);
    footLayout->setSpacing(0);
    auto *connected = new QWidget(foot);
    auto *connectedLayout = new QHBoxLayout(connected);
    connectedLayout->setContentsMargins(2, 3, 2, 3);
    connectedLayout->setSpacing(7);
    m_databaseDot = new StatusDot(7, Theme::Text3, connected);
    m_databaseLabel =
        Ui::label(QStringLiteral("正在读取本地数据…"), 11, 400, Theme::Text3, connected);
    connectedLayout->addWidget(m_databaseDot);
    connectedLayout->addWidget(m_databaseLabel);
    connectedLayout->addStretch(1);
    auto *version = Ui::label(QStringLiteral("v" LIVEMIX_VERSION), 11, 400, Theme::Text3, foot);
    version->setContentsMargins(2, 3, 2, 3);
    footLayout->addWidget(connected);
    footLayout->addWidget(version);
    root->addWidget(foot);

    setActivePage(QStringLiteral("dash"));
}

void Sidebar::setActivePage(const QString &pageId)
{
    for (auto it = m_items.begin(); it != m_items.end(); ++it) {
        it.value()->setActive(it.key() == pageId);
    }
}

void Sidebar::setBadge(const QString &pageId, const QString &badge)
{
    NavItem *item = m_items.value(pageId, nullptr);
    if (item) {
        item->setBadge(badge);
    }
}

void Sidebar::setDatabaseReady(bool ready, const QString &error)
{
    if (!m_databaseDot || !m_databaseLabel) {
        return;
    }
    m_databaseDot->setColor(ready ? Theme::Success : Theme::Danger);
    m_databaseLabel->setText(ready ? QStringLiteral("本地数据已就绪")
                                   : QStringLiteral("本地数据库不可用"));
    m_databaseLabel->setStyleSheet(
        QStringLiteral("color:%1; background:transparent;")
            .arg((ready ? Theme::Text3 : Theme::Danger).name()));
    m_databaseLabel->setToolTip(error);
}

void Sidebar::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    painter.fillRect(rect(), Theme::Surface1);
    painter.setPen(QPen(Theme::Border, 1.0));
    painter.drawLine(QPointF(width() - 0.5, 0), QPointF(width() - 0.5, height()));
}

// -----------------------------------------------------------------------------
// StatusBar
// -----------------------------------------------------------------------------

StatusBar::StatusBar(QWidget *parent)
    : QWidget(parent)
{
    setFixedHeight(26);
    setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);

    auto *layout = new QHBoxLayout(this);
    layout->setContentsMargins(14, 0, 14, 0);
    layout->setSpacing(18);

    auto monoLabel = [this](const QString &text, const QColor &color) {
        auto *label = Ui::label(text, 11, 400, color, this);
        label->setFont(Theme::monoFont(11));
        return label;
    };

    auto *connection = new QWidget(this);
    auto *connectionLayout = new QHBoxLayout(connection);
    connectionLayout->setContentsMargins(0, 0, 0, 0);
    connectionLayout->setSpacing(5);
    m_serviceDot = new StatusDot(6, Theme::Text3, connection);
    connectionLayout->addWidget(m_serviceDot);
    m_serviceLabel =
        Ui::label(QStringLiteral("服务状态未知"), 11, 400, Theme::Text3, connection);
    m_serviceLabel->setFont(Theme::monoFont(11));
    connectionLayout->addWidget(m_serviceLabel);
    layout->addWidget(connection);
    m_queueLabel = monoLabel(QStringLiteral("队列空闲"), Theme::Text3);
    layout->addWidget(m_queueLabel);
    m_draftCountLabel = monoLabel(QStringLiteral("草稿 0 条"), Theme::Text3);
    layout->addWidget(m_draftCountLabel);

    m_draftStatus = new LinkLabel(QString(), this);
    m_draftStatus->setPixelSize(11);
    m_draftStatus->setMonoFont(true);
    m_draftStatus->setTextColor(Theme::Primary);
    m_draftStatus->setHoverColor(Theme::PrimaryHover);
    m_draftStatus->setToolTip(QStringLiteral("查看草稿下载进度"));
    m_draftStatus->hide();
    layout->addWidget(m_draftStatus);
    connect(m_draftStatus, &QAbstractButton::clicked, this, [this] {
        if (m_draftStatusDraftId >= 0) {
            emit draftDownloadRequested(m_draftStatusDraftId);
        }
    });

    layout->addStretch(1);
    m_ffmpegLabel = monoLabel(QStringLiteral("ffmpeg 检测中"), Theme::Text3);
    layout->addWidget(m_ffmpegLabel);
}

void StatusBar::setRounded(bool rounded)
{
    m_rounded = rounded;
    update();
}

void StatusBar::setDraftDownloadStatus(const QString &text, bool error,
                                       qint64 draftRowId)
{
    if (!m_draftStatus) {
        return;
    }
    m_draftStatusDraftId = draftRowId;
    m_draftStatus->setText(text);
    m_draftStatus->setTextColor(error ? Theme::Danger : Theme::Primary);
    m_draftStatus->setHoverColor(error ? Theme::Danger : Theme::PrimaryHover);
    m_draftStatus->show();
    m_draftStatus->updateGeometry();
}

void StatusBar::clearDraftDownloadStatus()
{
    if (!m_draftStatus) {
        return;
    }
    m_draftStatusDraftId = -1;
    m_draftStatus->hide();
    m_draftStatus->setText(QString());
}

void StatusBar::setServiceStatus(const QString &text, bool ok, const QString &tooltip)
{
    if (!m_serviceLabel || !m_serviceDot) {
        return;
    }
    const QColor color = ok ? Theme::Success : Theme::Warn;
    m_serviceDot->setColor(color);
    m_serviceLabel->setText(text);
    m_serviceLabel->setStyleSheet(
        QStringLiteral("color:%1; background:transparent;").arg(color.name()));
    m_serviceLabel->setToolTip(tooltip);
}

void StatusBar::setQueueCount(int count)
{
    if (!m_queueLabel) {
        return;
    }
    m_queueLabel->setText(count > 0 ? QStringLiteral("队列 %1").arg(count)
                                    : QStringLiteral("队列空闲"));
    m_queueLabel->setStyleSheet(
        QStringLiteral("color:%1; background:transparent;")
            .arg((count > 0 ? Theme::Primary : Theme::Text3).name()));
}

void StatusBar::setDraftCount(int count)
{
    if (!m_draftCountLabel) {
        return;
    }
    m_draftCountLabel->setText(count >= 0 ? QStringLiteral("草稿 %1 条").arg(count)
                                          : QStringLiteral("草稿 —"));
}

void StatusBar::setFfmpegAvailable(bool available)
{
    if (!m_ffmpegLabel) {
        return;
    }
    m_ffmpegLabel->setText(available ? QStringLiteral("ffmpeg ✓")
                                     : QStringLiteral("ffmpeg 未找到"));
    m_ffmpegLabel->setStyleSheet(
        QStringLiteral("color:%1; background:transparent;")
            .arg((available ? Theme::Success : Theme::Danger).name()));
}

void StatusBar::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    painter.setRenderHint(QPainter::Antialiasing, true);
    painter.setPen(Qt::NoPen);
    painter.setBrush(Theme::Surface1);
    const qreal radius = Theme::isWindowsWindowStyle() ? 7.0 : 11.0;
    painter.drawPath(roundedPath(QRectF(rect()), 0, m_rounded ? radius : 0.0));
    painter.setPen(QPen(Theme::Border, 1.0));
    painter.drawLine(QPointF(0, 0.5), QPointF(width(), 0.5));
}

// -----------------------------------------------------------------------------
// WindowShell
// -----------------------------------------------------------------------------

WindowShell::WindowShell(QWidget *parent)
    : QWidget(parent)
{
    m_radius = Theme::windowCornerRadius();
    auto *root = new QVBoxLayout(this);
    root->setContentsMargins(1, 1, 1, 1);
    root->setSpacing(0);

    m_titleBar = new TitleBar(this);
    root->addWidget(m_titleBar);

    auto *workspace = new QWidget(this);
    workspace->setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Expanding);
    m_workspaceLayout = new QHBoxLayout(workspace);
    m_workspaceLayout->setContentsMargins(0, 0, 0, 0);
    m_workspaceLayout->setSpacing(0);
    root->addWidget(workspace, 1);

    m_statusBar = new StatusBar(this);
    root->addWidget(m_statusBar);
}

void WindowShell::setRounded(bool rounded)
{
    m_rounded = rounded;
    m_radius = rounded ? Theme::windowCornerRadius() : 0;
    m_titleBar->setRounded(rounded);
    m_statusBar->setRounded(rounded);
    update();
}

void WindowShell::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    painter.setRenderHint(QPainter::Antialiasing, true);
    const QRectF r = QRectF(rect()).adjusted(0.5, 0.5, -0.5, -0.5);
    QPainterPath path;
    path.addRoundedRect(r, m_radius, m_radius);
    painter.setPen(QPen(Theme::BorderStrong, 1.0));
    painter.setBrush(Theme::Window);
    painter.drawPath(path);
}
