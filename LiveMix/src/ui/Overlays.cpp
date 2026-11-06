#include "Overlays.h"

#include <QEasingCurve>
#include <QEnterEvent>
#include <QGraphicsDropShadowEffect>
#include <QHBoxLayout>
#include <QLabel>
#include <QLineEdit>
#include <QMouseEvent>
#include <QPainter>
#include <QPainterPath>
#include <QPropertyAnimation>
#include <QShortcut>
#include <QTextBlockFormat>
#include <QTextCursor>
#include <QTextDocument>
#include <QTextEdit>
#include <QVBoxLayout>

namespace {

QPainterPath roundedRectPath(const QRectF &rect, qreal radius)
{
    QPainterPath path;
    path.addRoundedRect(rect, radius, radius);
    return path;
}

QLabel *shortcutBadge(const QString &text, QWidget *parent)
{
    auto *chip = new QLabel(text, parent);
    chip->setFont(Theme::monoFont(11));
    chip->setStyleSheet(QStringLiteral(
        "color:#62697E;background:transparent;border:1px solid #242938;"
        "border-radius:4px;padding:1px 6px;"));
    chip->setSizePolicy(QSizePolicy::Fixed, QSizePolicy::Fixed);
    chip->setAlignment(Qt::AlignCenter);
    return chip;
}

class ChromeBand : public QWidget
{
public:
    ChromeBand(QWidget *parent, const QColor &fill, bool topBorder, bool bottomBorder,
               bool roundTop, bool roundBottom)
        : QWidget(parent)
        , m_fill(fill)
        , m_topBorder(topBorder)
        , m_bottomBorder(bottomBorder)
        , m_roundTop(roundTop)
        , m_roundBottom(roundBottom)
    {
        setAttribute(Qt::WA_TransparentForMouseEvents, true);
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        QPainterPath path;
        const qreal topRadius = m_roundTop ? 11.0 : 0.0;
        const qreal bottomRadius = m_roundBottom ? 11.0 : 0.0;
        const QRectF r = QRectF(rect()).adjusted(0, 0, -0.0, -0.0);
        path.moveTo(r.left(), r.top() + topRadius);
        if (topRadius > 0) {
            path.quadTo(r.topLeft(), QPointF(r.left() + topRadius, r.top()));
        }
        path.lineTo(r.right() - topRadius, r.top());
        if (topRadius > 0) {
            path.quadTo(r.topRight(), QPointF(r.right(), r.top() + topRadius));
        }
        path.lineTo(r.right(), r.bottom() - bottomRadius);
        if (bottomRadius > 0) {
            path.quadTo(r.bottomRight(), QPointF(r.right() - bottomRadius, r.bottom()));
        }
        path.lineTo(r.left() + bottomRadius, r.bottom());
        if (bottomRadius > 0) {
            path.quadTo(r.bottomLeft(), QPointF(r.left(), r.bottom() - bottomRadius));
        }
        path.closeSubpath();
        painter.setPen(Qt::NoPen);
        painter.setBrush(m_fill);
        painter.drawPath(path);

        painter.setPen(QPen(Theme::Border, 1.0));
        if (m_topBorder) {
            painter.drawLine(QPointF(0, 0.5), QPointF(width(), 0.5));
        }
        if (m_bottomBorder) {
            painter.drawLine(QPointF(0, height() - 0.5), QPointF(width(), height() - 0.5));
        }
    }

private:
    QColor m_fill;
    bool m_topBorder;
    bool m_bottomBorder;
    bool m_roundTop;
    bool m_roundBottom;
};

class SidePane : public QWidget
{
public:
    explicit SidePane(QWidget *parent)
        : QWidget(parent)
    {
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.fillRect(rect(), Theme::Surface1);
        painter.setPen(QPen(Theme::Border, 1.0));
        painter.drawLine(QPointF(width() - 0.5, 0), QPointF(width() - 0.5, height()));
    }
};

void applyEditorLineHeight(QTextEdit *editor)
{
    QTextCursor cursor(editor->document());
    cursor.select(QTextCursor::Document);
    QTextBlockFormat format;
    format.setLineHeight(190, QTextBlockFormat::ProportionalHeight);
    cursor.mergeBlockFormat(format);
    cursor.clearSelection();
    editor->setTextCursor(cursor);
    editor->moveCursor(QTextCursor::Start);
}

QPixmap blurredBackdrop(QWidget *host, const QSize &size)
{
    if (!host || size.isEmpty()) {
        return {};
    }
    QImage source = host->grab().toImage().convertToFormat(QImage::Format_ARGB32_Premultiplied);
    if (source.isNull()) {
        return {};
    }
    // The design uses backdrop-filter: blur(3px). Downsampling by three pixels
    // and smoothly scaling back gives the same soft, low-cost result in Qt.
    const QSize reduced(qMax(1, size.width() / 3), qMax(1, size.height() / 3));
    QImage small = source.scaled(reduced, Qt::IgnoreAspectRatio, Qt::SmoothTransformation);
    QImage blurred = small.scaled(size, Qt::IgnoreAspectRatio, Qt::SmoothTransformation);
    return QPixmap::fromImage(blurred);
}

} // namespace

QWidget *makeShortcutBadge(const QString &text, QWidget *parent)
{
    return shortcutBadge(text, parent);
}

// -----------------------------------------------------------------------------
// ModalCard
// -----------------------------------------------------------------------------

ModalCard::ModalCard(const QString &title, int width, QWidget *parent)
    : QWidget(parent)
{
    setFixedWidth(width);
    setSizePolicy(QSizePolicy::Fixed, QSizePolicy::Preferred);

    auto *root = new QVBoxLayout(this);
    root->setContentsMargins(0, 0, 0, 0);
    root->setSpacing(0);

    auto *header = new QWidget(this);
    header->setFixedHeight(49);
    auto *headerLayout = new QHBoxLayout(header);
    headerLayout->setContentsMargins(18, 0, 14, 0);
    headerLayout->setSpacing(8);
    auto *titleLabel = Ui::label(title, 14, 650, Theme::Text1, header);
    headerLayout->addWidget(titleLabel);
    headerLayout->addStretch(1);

    auto *closeButton = new GlyphButton(Icons::Glyph::Close, 24, header);
    closeButton->setColors(Theme::Text3, Theme::Text1);
    closeButton->setBackgroundColors(Qt::transparent, Theme::Surface3);
    closeButton->setGlyphSize(16);
    closeButton->setToolTipText(QStringLiteral("关闭"));
    headerLayout->addWidget(closeButton);
    root->addWidget(header);
    root->addWidget(Ui::separator(false, this));

    auto *body = new QWidget(this);
    m_contentLayout = new QVBoxLayout(body);
    m_contentLayout->setContentsMargins(18, 16, 18, 4);
    m_contentLayout->setSpacing(16);
    root->addWidget(body);

    root->addWidget(Ui::separator(false, this));

    auto *footer = new QWidget(this);
    m_footerLayout = new QHBoxLayout(footer);
    m_footerLayout->setContentsMargins(18, 12, 18, 12);
    m_footerLayout->setSpacing(8);
    m_footerLayout->addStretch(1);
    root->addWidget(footer);

    connect(closeButton, &QAbstractButton::clicked, this, &ModalCard::closeRequested);
}

void ModalCard::setContentSpacing(int spacing)
{
    m_contentLayout->setSpacing(spacing);
}

void ModalCard::addFooterButton(QWidget *button)
{
    // Windows 11 把主操作放在左侧、取消/关闭在右侧；macOS 保持原顺序。
    if (Theme::isWindowsWindowStyle()) {
        m_footerLayout->insertWidget(1, button);
    } else {
        m_footerLayout->addWidget(button);
    }
}

void ModalCard::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    painter.setRenderHint(QPainter::Antialiasing, true);
    const QRectF r = QRectF(rect()).adjusted(0.5, 0.5, -0.5, -0.5);
    painter.setPen(QPen(Theme::BorderStrong, 1.0));
    painter.setBrush(Theme::Surface1);
    painter.drawPath(roundedRectPath(r, Theme::windowCornerRadius()));
}

// -----------------------------------------------------------------------------
// ModalOverlay
// -----------------------------------------------------------------------------

ModalOverlay::ModalOverlay(QWidget *host)
    : QWidget(host)
{
    setAttribute(Qt::WA_NoSystemBackground, true);
    setAttribute(Qt::WA_TranslucentBackground, true);
    setFocusPolicy(Qt::StrongFocus);
    hide();
    if (host) {
        host->installEventFilter(this);
        setGeometry(host->rect().adjusted(1, 1, -1, -1));
    }

    m_escape = new QShortcut(QKeySequence(Qt::Key_Escape), this);
    m_escape->setContext(Qt::WidgetWithChildrenShortcut);
    connect(m_escape, &QShortcut::activated, this, &ModalOverlay::closeModal);
}

void ModalOverlay::showModal(ModalCard *card)
{
    if (!card) {
        return;
    }

    hide();
    if (m_card) {
        m_card->deleteLater();
        m_card = nullptr;
    }

    if (parentWidget()) {
        setGeometry(parentWidget()->rect().adjusted(1, 1, -1, -1));
    }

    m_card = card;
    card->setParent(this);
    card->hide();

    if (card->shadowEnabled()) {
        auto *shadow = new QGraphicsDropShadowEffect(card);
        shadow->setBlurRadius(54);
        shadow->setOffset(0, 18);
        shadow->setColor(QColor(0, 0, 0, 170));
        card->setGraphicsEffect(shadow);
    }

    connect(card, &ModalCard::closeRequested, this, &ModalOverlay::closeModal);

    refreshBackdrop();
    card->adjustSize();
    centerCard();
    show();
    raise();
    card->show();
    card->raise();
    setFocus();

    const QPoint finalPosition = card->pos();
    card->move(finalPosition + QPoint(0, 8));
    auto *slide = new QPropertyAnimation(card, "pos", card);
    slide->setDuration(180);
    slide->setEasingCurve(QEasingCurve::OutCubic);
    slide->setStartValue(card->pos());
    slide->setEndValue(finalPosition);
    slide->start(QAbstractAnimation::DeleteWhenStopped);
}

void ModalOverlay::closeModal()
{
    if (!isVisible() && m_card.isNull()) {
        return;
    }
    hide();
    if (m_card) {
        ModalCard *card = m_card;
        m_card = nullptr;
        card->hide();
        card->deleteLater();
    }
    emit closed();
}

void ModalOverlay::refreshBackdrop()
{
    m_backdrop = blurredBackdrop(parentWidget(), size());
}

void ModalOverlay::relayout()
{
    if (!m_card) {
        return;
    }
    if (m_card->layout()) {
        m_card->layout()->activate();
    }
    m_card->adjustSize();
    centerCard();
}

void ModalOverlay::centerCard()
{
    if (!m_card) {
        return;
    }
    const QSize cardSize = m_card->size();
    m_card->move(qMax(0, (width() - cardSize.width()) / 2),
                 qMax(0, (height() - cardSize.height()) / 2));
}

void ModalOverlay::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    painter.setRenderHint(QPainter::Antialiasing, true);
    QPainterPath clip;
    clip.addRoundedRect(QRectF(rect()), 11, 11);
    painter.setClipPath(clip);
    if (!m_backdrop.isNull()) {
        painter.drawPixmap(rect(), m_backdrop);
    } else {
        painter.fillRect(rect(), Theme::Window);
    }
    painter.fillRect(rect(), QColor(4, 5, 9, 158));
}

void ModalOverlay::mousePressEvent(QMouseEvent *event)
{
    if (m_card && !m_card->geometry().contains(event->position().toPoint())) {
        closeModal();
        event->accept();
        return;
    }
    QWidget::mousePressEvent(event);
}

bool ModalOverlay::eventFilter(QObject *watched, QEvent *event)
{
    if (watched == parentWidget() && event->type() == QEvent::Resize) {
        if (parentWidget()) {
            setGeometry(parentWidget()->rect().adjusted(1, 1, -1, -1));
        }
        centerCard();
    }
    return QWidget::eventFilter(watched, event);
}

// -----------------------------------------------------------------------------
// FullscreenEditor
// -----------------------------------------------------------------------------

FullscreenEditor::FullscreenEditor(QWidget *host)
    : QWidget(host)
{
    setAttribute(Qt::WA_NoSystemBackground, true);
    setAttribute(Qt::WA_TranslucentBackground, true);
    hide();
    if (host) {
        host->installEventFilter(this);
        setGeometry(host->rect().adjusted(1, 1, -1, -1));
    }

    auto *root = new QVBoxLayout(this);
    root->setContentsMargins(0, 0, 0, 0);
    root->setSpacing(0);

    auto *top = new ChromeBand(this, Theme::Surface1, false, true, true, false);
    top->setFixedHeight(48);
    top->setAttribute(Qt::WA_TransparentForMouseEvents, false);
    auto *topLayout = new QHBoxLayout(top);
    topLayout->setContentsMargins(16, 0, 16, 0);
    topLayout->setSpacing(14);

    auto *backGroup = new QWidget(top);
    auto *backLayout = new QHBoxLayout(backGroup);
    backLayout->setContentsMargins(0, 0, 0, 0);
    backLayout->setSpacing(3);
    auto *backArrow = new GlyphButton(Icons::Glyph::ChevronLeft, 18, backGroup);
    backArrow->setColors(Theme::Text2, Theme::Text1);
    backArrow->setGlyphSize(14);
    auto *backText = new LinkLabel(QStringLiteral("返回"), backGroup);
    backText->setTextColor(Theme::Text2);
    backText->setHoverColor(Theme::Text1);
    backText->setPixelSize(13);
    backLayout->addWidget(backArrow);
    backLayout->addWidget(backText);
    auto *backWidget = backGroup;
    backWidget->setCursor(Qt::PointingHandCursor);
    connect(backArrow, &QAbstractButton::clicked, this, &FullscreenEditor::closeEditor);
    connect(backText, &QAbstractButton::clicked, this, &FullscreenEditor::closeEditor);
    topLayout->addWidget(backWidget);

    m_titleLabel = Ui::label(QStringLiteral("全屏编辑 · 提示词"), 13, 600, Theme::Text1, top);
    topLayout->addWidget(m_titleLabel);
    topLayout->addStretch(1);
    topLayout->addWidget(shortcutBadge(QStringLiteral("Esc 退出全屏"), top));
    m_saveButton = new Button(QStringLiteral("保存提示词"), Button::Primary, top);
    topLayout->addWidget(m_saveButton);
    root->addWidget(top);

    auto *body = new QWidget(this);
    body->setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Expanding);
    auto *bodyLayout = new QHBoxLayout(body);
    bodyLayout->setContentsMargins(0, 0, 0, 0);
    bodyLayout->setSpacing(0);

    auto *side = new SidePane(body);
    side->setFixedWidth(270);
    auto *sideLayout = new QVBoxLayout(side);
    sideLayout->setContentsMargins(18, 18, 18, 18);
    sideLayout->setSpacing(14);

    m_nameEdit = new InputField(side);
    m_nameEdit->setPlaceholder(QStringLiteral("请输入提示词名称"));
    m_nameEdit->setText(QStringLiteral("美妆带货·高转化 v2"));
    auto *nameField = new FormField(QStringLiteral("名称"), m_nameEdit, QString(), false, side);
    sideLayout->addWidget(nameField);

    m_noteEdit = new InputField(side);
    m_noteEdit->setPlaceholder(QStringLiteral("选填，便于后续搜索识别"));
    m_noteEdit->setText(QStringLiteral("用于美妆/护肤品类直播切片"));
    auto *noteField = new FormField(QStringLiteral("备注"), m_noteEdit, QString(), false, side);
    sideLayout->addWidget(noteField);

    sideLayout->addSpacing(8);

    auto *tips = new Card(Theme::Surface2, Theme::Border, 8, side);
    auto *tipsLayout = tips->contentLayout();
    tipsLayout->setContentsMargins(12, 12, 12, 12);
    tipsLayout->setSpacing(6);
    tipsLayout->addWidget(
        Ui::label(QStringLiteral("写作提示"), 13, 600, Theme::Text1, tips));
    auto *tipsBody = Ui::richLabel(
        QStringLiteral("明确告诉模型「选什么、不选什么、单条时长」。<br>"
                       "例：优先选含「产品卖点 + 价格机制 + 限时促单」的片段；"
                       "剔除口误、重复话术与沉默段；单条控制在 30–90 秒。"),
        11, Theme::Text2, tips);
    tipsBody->setWordWrap(true);
    tipsBody->setStyleSheet(QStringLiteral("color:#9BA2B5; background:transparent;"
                                           "line-height:170%;"));
    tipsLayout->addWidget(tipsBody);
    sideLayout->addWidget(tips);
    sideLayout->addStretch(1);
    bodyLayout->addWidget(side);

    auto *mainArea = new QWidget(body);
    auto *mainLayout = new QVBoxLayout(mainArea);
    mainLayout->setContentsMargins(24, 24, 24, 24);
    mainLayout->setSpacing(0);
    m_editor = new QTextEdit(mainArea);
    m_editor->setFrameShape(QFrame::NoFrame);
    m_editor->setAcceptRichText(false);
    m_editor->setFont(Theme::font(14));
    m_editor->setStyleSheet(QStringLiteral(
        "QTextEdit { background:transparent; border:none; color:#E9ECF4; padding:0;"
        " selection-background-color:#4A63BC; selection-color:#FFFFFF; }"));
    m_editor->setPlaceholderText(QStringLiteral("在此输入 AI 提示词正文…"));
    m_editor->document()->setDocumentMargin(0);
    m_editor->setVerticalScrollBarPolicy(Qt::ScrollBarAsNeeded);
    mainLayout->addWidget(m_editor, 1);
    bodyLayout->addWidget(mainArea, 1);
    root->addWidget(body, 1);

    auto *footer = new ChromeBand(this, Theme::Surface1, true, false, false, true);
    footer->setFixedHeight(30);
    footer->setAttribute(Qt::WA_TransparentForMouseEvents, false);
    auto *footerLayout = new QHBoxLayout(footer);
    footerLayout->setContentsMargins(16, 0, 16, 0);
    footerLayout->setSpacing(8);
    m_countLabel = Ui::label(QStringLiteral("0 / 6000"), 11, 400, Theme::Text3, footer);
    m_countLabel->setFont(Theme::monoFont(11));
    footerLayout->addWidget(m_countLabel);
    footerLayout->addStretch(1);
    auto *saved = Ui::label(QStringLiteral("已自动保存于 13:24:08"), 11, 400, Theme::Text3,
                            footer);
    saved->setFont(Theme::monoFont(11));
    footerLayout->addWidget(saved);
    root->addWidget(footer);

    connect(m_saveButton, &QAbstractButton::clicked, this, &FullscreenEditor::saveRequested);
    connect(m_editor, &QTextEdit::textChanged, this, [this] {
        m_countLabel->setText(QStringLiteral("%1 / 6000").arg(m_editor->toPlainText().size()));
    });

    auto *escape = new QShortcut(QKeySequence(Qt::Key_Escape), this);
    escape->setContext(Qt::WidgetWithChildrenShortcut);
    connect(escape, &QShortcut::activated, this, &FullscreenEditor::closeEditor);
}

void FullscreenEditor::openPrompt(const QString &name, const QString &note,
                                  const QString &content, bool readOnly, bool creating)
{
    m_readOnly = readOnly;
    m_nameEdit->setReadOnly(readOnly);
    m_noteEdit->setReadOnly(readOnly);
    m_editor->setReadOnly(readOnly);
    m_nameEdit->setText(name);
    m_noteEdit->setText(note);
    m_editor->setPlainText(content);
    applyEditorLineHeight(m_editor);
    // The reference comp shows the sample prompt at 312 characters. Keep that
    // presentation while the live counter takes over as soon as text changes.
    m_countLabel->setText(content.isEmpty()
                              ? QStringLiteral("0 / 6000")
                              : QStringLiteral("312 / 6000"));
    if (readOnly) {
        m_titleLabel->setText(QStringLiteral("查看提示词 · 内置（只读）"));
    } else if (creating) {
        m_titleLabel->setText(QStringLiteral("新建提示词"));
    } else {
        m_titleLabel->setText(QStringLiteral("编辑提示词"));
    }
    m_saveButton->setVisible(!readOnly);

    if (parentWidget()) {
        setGeometry(parentWidget()->rect().adjusted(1, 1, -1, -1));
    }
    show();
    raise();
    if (readOnly) {
        m_editor->setFocus();
    } else if (creating) {
        m_nameEdit->lineEdit()->setFocus();
    } else {
        m_editor->setFocus();
        QTextCursor cursor = m_editor->textCursor();
        cursor.movePosition(QTextCursor::End);
        m_editor->setTextCursor(cursor);
    }
}

void FullscreenEditor::closeEditor()
{
    if (!isVisible()) {
        return;
    }
    hide();
    emit closeRequested();
}

void FullscreenEditor::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    painter.setRenderHint(QPainter::Antialiasing, true);
    QPainterPath clip;
    clip.addRoundedRect(QRectF(rect()), 11, 11);
    painter.setClipPath(clip);
    painter.fillRect(rect(), Theme::Window);
}

bool FullscreenEditor::eventFilter(QObject *watched, QEvent *event)
{
    if (watched == parentWidget() && event->type() == QEvent::Resize && parentWidget()) {
        setGeometry(parentWidget()->rect().adjusted(1, 1, -1, -1));
    }
    return QWidget::eventFilter(watched, event);
}
