#include "SettingsPage.h"

#include "../core/AppConfig.h"
#include "../core/Brand.h"
#include "../ui/Controls.h"
#include "../ui/Icons.h"

#include <QBoxLayout>
#include <QDesktopServices>
#include <QDir>
#include <QEnterEvent>
#include <QFileDialog>
#include <QFrame>
#include <QHBoxLayout>
#include <QIntValidator>
#include <QLabel>
#include <QLineEdit>
#include <QMouseEvent>
#include <QNetworkAccessManager>
#include <QNetworkReply>
#include <QNetworkRequest>
#include <QPainter>
#include <QPainterPath>
#include <QPixmap>
#include <QResizeEvent>
#include <QScrollArea>
#include <QStackedWidget>
#include <QTimer>
#include <QUrl>
#include <QVBoxLayout>

#include <functional>

namespace {

constexpr int kFieldWidth = 560;
constexpr int kFinishGroupWidth = 820;
constexpr int kMinClipDuration = 30;
constexpr int kMaxClipDuration = 600;
constexpr int kClipDurationStep = 5;
constexpr int kClipDurationJitter = 20;

class PaneNavButton : public QAbstractButton
{
public:
    explicit PaneNavButton(const QString &text, QWidget *parent = nullptr)
        : QAbstractButton(parent)
    {
        setText(text);
        setFixedHeight(34);
        setCursor(Qt::PointingHandCursor);
        setAttribute(Qt::WA_Hover, true);
        setFocusPolicy(Qt::NoFocus);
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
    }

    void setActive(bool active)
    {
        m_active = active;
        update();
    }

protected:
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

    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        if (m_active) {
            painter.setPen(Qt::NoPen);
            painter.setBrush(Theme::alpha(Theme::Primary, 0.14));
            painter.drawRoundedRect(QRectF(rect()).adjusted(0.5, 0.5, -0.5, -0.5), 6, 6);
        } else if (m_hovered) {
            painter.setPen(Qt::NoPen);
            painter.setBrush(Theme::Surface2);
            painter.drawRoundedRect(QRectF(rect()).adjusted(0.5, 0.5, -0.5, -0.5), 6, 6);
        }
        painter.setFont(Theme::font(13, m_active ? 500 : 400));
        painter.setPen(m_active ? Theme::Primary : Theme::Text2);
        painter.drawText(QRect(12, 0, width() - 24, height()), Qt::AlignLeft | Qt::AlignVCenter,
                         text());
    }

private:
    bool m_active = false;
    bool m_hovered = false;
};

// 设置页每个面板各自滚动：卡片整体填满窗口，页头与左侧导航保持固定。
class PaneScrollArea : public QScrollArea
{
public:
    explicit PaneScrollArea(QWidget *parent = nullptr)
        : QScrollArea(parent)
    {
        setFrameShape(QFrame::NoFrame);
        setWidgetResizable(true);
        setHorizontalScrollBarPolicy(Qt::ScrollBarAlwaysOff);
        setVerticalScrollBarPolicy(Qt::ScrollBarAsNeeded);
        setStyleSheet(QStringLiteral(
            "QScrollArea { background: transparent; border: none; }"
            "QScrollArea > QWidget > QWidget { background: transparent; }"));
        viewport()->setAutoFillBackground(false);
    }
};

class StatusDotLine : public QWidget
{
public:
    explicit StatusDotLine(const QColor &color, QWidget *parent = nullptr)
        : QWidget(parent)
        , m_color(color)
    {
        setFixedSize(8, 8);
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        painter.setPen(Qt::NoPen);
        painter.setBrush(m_color);
        painter.drawEllipse(QRectF(rect()));
    }

private:
    QColor m_color;
};

QLabel *makeKbd(const QString &text, QWidget *parent)
{
    auto *chip = new QLabel(text, parent);
    chip->setFont(Theme::monoFont(11));
    chip->setStyleSheet(QStringLiteral(
        "color:#E9ECF4;background:#222734;border:1px solid #343B4E;"
        "border-bottom-width:2px;border-radius:4px;padding:1px 6px;"));
    chip->setSizePolicy(QSizePolicy::Fixed, QSizePolicy::Fixed);
    return chip;
}

FormField *makeField(const QString &label, QWidget *content, const QString &hint,
                     bool required, QWidget *parent)
{
    auto *field = new FormField(label, content, hint, required, parent);
    field->setFixedWidth(kFieldWidth);
    return field;
}

QWidget *makePathRow(InputField *input, QWidget *parent, const std::function<void()> &browse)
{
    auto *row = new QWidget(parent);
    auto *layout = new QHBoxLayout(row);
    layout->setContentsMargins(0, 0, 0, 0);
    layout->setSpacing(8);
    layout->addWidget(input, 1);
    auto *button = new Button(QStringLiteral("浏览…"), Button::Default, row);
    QObject::connect(button, &QAbstractButton::clicked, row, [browse] { browse(); });
    layout->addWidget(button);
    return row;
}

QWidget *makeCheckRow(const QStringList &labels, const QVector<int> &checked,
                      QWidget *parent, QVector<CheckBox *> *boxes = nullptr)
{
    auto *host = new QWidget(parent);
    auto *layout = new QVBoxLayout(host);
    layout->setContentsMargins(0, 3, 0, 0);
    layout->setSpacing(9);
    for (int i = 0; i < labels.size(); ++i) {
        auto *box = new CheckBox(labels[i], host);
        box->setChecked(checked.contains(i));
        layout->addWidget(box);
        if (boxes) {
            boxes->append(box);
        }
    }
    return host;
}

struct Pane
{
    PaneScrollArea *scroll = nullptr;
    QWidget *content = nullptr;
    QVBoxLayout *layout = nullptr;
};

Pane makePane(QWidget *parent)
{
    auto *scroll = new PaneScrollArea(parent);
    auto *content = new QWidget;
    content->setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Preferred);
    auto *layout = new QVBoxLayout(content);
    layout->setContentsMargins(22, 18, 22, 18);
    layout->setSpacing(0);
    layout->setAlignment(Qt::AlignTop);
    scroll->setWidget(content);
    return {scroll, content, layout};
}

// 成片设置的一行开关：整行可点，右侧是 Switch。
class ToggleRow : public QWidget
{
public:
    ToggleRow(const QString &title, const QString &description, bool checked,
              QWidget *parent = nullptr)
        : QWidget(parent)
    {
        setAttribute(Qt::WA_Hover, true);
        setCursor(Qt::PointingHandCursor);
        setMinimumHeight(56);
        auto *layout = new QHBoxLayout(this);
        layout->setContentsMargins(14, 12, 14, 12);
        layout->setSpacing(16);

        auto *text = new QWidget(this);
        auto *textLayout = new QVBoxLayout(text);
        textLayout->setContentsMargins(0, 0, 0, 0);
        textLayout->setSpacing(3);
        textLayout->addWidget(Ui::label(title, 13, 500, Theme::Text1, text));
        auto *hint = Ui::label(description, 11, 400, Theme::Text3, text);
        hint->setWordWrap(true);
        textLayout->addWidget(hint);
        layout->addWidget(text, 1);

        m_switch = new Switch(this);
        m_switch->setChecked(checked);
        m_switch->setAccessibleName(title);
        layout->addWidget(m_switch, 0, Qt::AlignVCenter);
    }

    Switch *toggle() const { return m_switch; }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        if (m_hovered) {
            painter.fillRect(rect(), Theme::alpha(Theme::Surface3, 0.40));
        }
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
        if (event->button() == Qt::LeftButton) {
            m_switch->toggle();
            event->accept();
            return;
        }
        QWidget::mousePressEvent(event);
    }

private:
    Switch *m_switch = nullptr;
    bool m_hovered = false;
};

// 小尺寸图标标签：把矢量图标画进位图，用于分组标题、关于面板等固定图标位。
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

// 分组标题：图标 + 标题 + 右侧统计，底部一条分隔线。
class SettingsGroupHeader : public QWidget
{
public:
    SettingsGroupHeader(Icons::Glyph glyph, const QString &title, QWidget *meta,
                        QWidget *parent = nullptr)
        : QWidget(parent)
    {
        auto *layout = new QHBoxLayout(this);
        layout->setContentsMargins(14, 10, 14, 10);
        layout->setSpacing(8);
        layout->addWidget(glyphLabel(glyph, 14, Theme::Text3, this), 0,
                          Qt::AlignVCenter);
        layout->addWidget(Ui::label(title, 12, 500, Theme::Text2, this), 0,
                          Qt::AlignVCenter);
        layout->addStretch(1);
        if (meta) {
            layout->addWidget(meta, 0, Qt::AlignVCenter);
        }
        setFixedHeight(layout->sizeHint().height());
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        QPainterPath path;
        path.addRoundedRect(QRectF(1, 1, width() - 2, height() + 7), 7, 7);
        painter.fillPath(path, Theme::alpha(Theme::Surface1, 0.45));
        painter.setPen(QPen(Theme::Border, 1.0));
        painter.drawLine(QPointF(1, height() - 0.5),
                         QPointF(width() - 1, height() - 0.5));
    }
};

// 成片设置的分组卡片：带图标标题栏，正文按需留白。
class SettingsGroup : public QWidget
{
public:
    SettingsGroup(Icons::Glyph glyph, const QString &title, QWidget *meta,
                  int bodyPadding, QWidget *parent = nullptr)
        : QWidget(parent)
    {
        setMaximumWidth(kFinishGroupWidth);
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Preferred);
        auto *root = new QVBoxLayout(this);
        root->setContentsMargins(0, 0, 0, 0);
        root->setSpacing(0);
        root->addWidget(new SettingsGroupHeader(glyph, title, meta, this));

        auto *body = new QWidget(this);
        m_bodyLayout = new QVBoxLayout(body);
        m_bodyLayout->setContentsMargins(bodyPadding, bodyPadding, bodyPadding,
                                         bodyPadding);
        m_bodyLayout->setSpacing(0);
        root->addWidget(body, 1);
    }

    QVBoxLayout *bodyLayout() const { return m_bodyLayout; }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        painter.setPen(QPen(Theme::Border, 1.0));
        painter.setBrush(Theme::alpha(Theme::Surface2, 0.50));
        painter.drawRoundedRect(QRectF(rect()).adjusted(0.5, 0.5, -0.5, -0.5), 8, 8);
    }

private:
    QVBoxLayout *m_bodyLayout = nullptr;
};

// 倍速刻度：0.5× / 1.0× / 1.5× / 2.0× 均匀铺在滑杆下方。
class SpeedTicks : public QWidget
{
public:
    explicit SpeedTicks(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        setFixedHeight(14);
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::TextAntialiasing, true);
        painter.setFont(Theme::monoFont(10));
        painter.setPen(Theme::Text3);
        const QStringList labels = {QStringLiteral("0.5×"), QStringLiteral("1.0×"),
                                    QStringLiteral("1.5×"), QStringLiteral("2.0×")};
        for (int i = 0; i < labels.size(); ++i) {
            const qreal ratio = i / qreal(labels.size() - 1);
            const qreal x = 8 + (width() - 16) * ratio;
            QRectF rect;
            int align = Qt::AlignHCenter;
            if (i == 0) {
                rect = QRectF(x, 0, 60, height());
                align = Qt::AlignLeft;
            } else if (i == labels.size() - 1) {
                rect = QRectF(x - 60, 0, 60, height());
                align = Qt::AlignRight;
            } else {
                rect = QRectF(x - 30, 0, 60, height());
            }
            painter.drawText(rect, align | Qt::AlignVCenter, labels.at(i));
        }
    }
};

QString formatSpeed(double value)
{
    const double scaled = value * 10.0;
    const int tenths = qRound(scaled);
    return qAbs(scaled - tenths) < 0.001
        ? QStringLiteral("%1×").arg(tenths / 10.0, 0, 'f', 1)
        : QStringLiteral("%1×").arg(value, 0, 'f', 2);
}

// 区间滑杆的两端刻度（单条成片时长：30 秒 / 600 秒）。
class EdgeTicks : public QWidget
{
public:
    EdgeTicks(const QString &left, const QString &right, QWidget *parent = nullptr)
        : QWidget(parent)
        , m_left(left)
        , m_right(right)
    {
        setFixedHeight(14);
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::TextAntialiasing, true);
        painter.setFont(Theme::monoFont(10));
        painter.setPen(Theme::Text3);
        painter.drawText(QRect(8, 0, width() - 16, height()),
                         Qt::AlignLeft | Qt::AlignVCenter, m_left);
        painter.drawText(QRect(8, 0, width() - 16, height()),
                         Qt::AlignRight | Qt::AlignVCenter, m_right);
    }

private:
    QString m_left;
    QString m_right;
};

// 单条成片时长：宽屏时说明在左、滑杆在右；窄窗口自动堆叠。
class DurationSetting : public QWidget
{
public:
    explicit DurationSetting(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        m_layout = new QBoxLayout(QBoxLayout::LeftToRight, this);
        m_layout->setContentsMargins(0, 0, 0, 0);
        m_layout->setSpacing(12);

        m_text = Ui::richLabel(QString(), 11, Theme::Text3, this);
        m_text->setWordWrap(true);
        m_text->setTextFormat(Qt::RichText);
        QSizePolicy textPolicy(QSizePolicy::Preferred, QSizePolicy::Minimum);
        textPolicy.setHeightForWidth(true);
        m_text->setSizePolicy(textPolicy);
        m_layout->addWidget(m_text, 1);

        m_sliderHost = new QWidget(this);
        auto *sliderLayout = new QVBoxLayout(m_sliderHost);
        sliderLayout->setContentsMargins(0, 0, 0, 0);
        sliderLayout->setSpacing(2);
        m_slider = new SliderControl(m_sliderHost);
        m_slider->setRange(kMinClipDuration, kMaxClipDuration, kClipDurationStep);
        m_slider->setAccessibleName(QStringLiteral("单条成片时长"));
        sliderLayout->addWidget(m_slider);
        sliderLayout->addWidget(new EdgeTicks(
            QStringLiteral("%1 秒").arg(kMinClipDuration),
            QStringLiteral("%1 秒").arg(kMaxClipDuration), m_sliderHost));
        m_layout->addWidget(m_sliderHost, 0);

        setDuration(60);
        applyResponsiveLayout();
    }

    SliderControl *slider() const { return m_slider; }

    void setDuration(int seconds)
    {
        m_slider->setValue(seconds);
        updateText();
    }

    int duration() const { return qRound(m_slider->value()); }

private:
    void updateText()
    {
        const QString accent = Theme::Text2.name();
        m_text->setText(
            QStringLiteral("AI 生成的成片在 <span style=\"color:%1;font-weight:500;\">"
                           "%2 秒</span>左右，实际成片时长会有 "
                           "<span style=\"color:%1;font-weight:500;\">±%3 秒浮动</span>。")
                .arg(accent, QString::number(duration()),
                     QString::number(kClipDurationJitter)));
    }

    void applyResponsiveLayout()
    {
        const bool horizontal = width() >= 700;
        m_horizontal = horizontal;
        m_layout->setDirection(horizontal ? QBoxLayout::LeftToRight
                                          : QBoxLayout::TopToBottom);
        m_layout->setSpacing(horizontal ? 32 : 12);
        m_layout->setAlignment(m_text, horizontal ? Qt::AlignVCenter : Qt::AlignTop);
        if (horizontal) {
            m_sliderHost->setMinimumWidth(400);
            m_sliderHost->setMaximumWidth(400);
        } else {
            m_sliderHost->setMinimumWidth(0);
            m_sliderHost->setMaximumWidth(QWIDGETSIZE_MAX);
        }
    }

    void resizeEvent(QResizeEvent *event) override
    {
        QWidget::resizeEvent(event);
        if ((width() >= 700) != m_horizontal) {
            applyResponsiveLayout();
        }
    }

    QBoxLayout *m_layout = nullptr;
    QLabel *m_text = nullptr;
    QWidget *m_sliderHost = nullptr;
    SliderControl *m_slider = nullptr;
    bool m_horizontal = true;
};

} // namespace

SettingsPage::SettingsPage(ConfigStore *configStore, QWidget *parent)
    : Page(parent)
    , m_config(configStore)
{
    const AppConfig config = m_config ? m_config->config() : ConfigStore::defaults();
    m_network = new QNetworkAccessManager(this);

    auto *header = new PageHeader(
        QStringLiteral("设置"),
        QStringLiteral("桌面客户端本地配置，连接自托管服务与本地输出路径。"), this);
    // 设置页卡片填满剩余高度，只有右侧面板内容滚动；页头始终固定。
    setFixedHeader(header);
    disablePageScroll();

    auto *outer = new Card(Theme::Surface1, Theme::Border, 8, this);
    outer->setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Expanding);
    auto *outerLayout = new QHBoxLayout;
    outerLayout->setContentsMargins(0, 0, 0, 0);
    outerLayout->setSpacing(0);
    outer->contentLayout()->addLayout(outerLayout);

    auto *side = new QWidget(outer);
    side->setFixedWidth(220);
    auto *sideLayout = new QVBoxLayout(side);
    sideLayout->setContentsMargins(8, 8, 8, 8);
    sideLayout->setSpacing(0);

    auto *stack = new QStackedWidget(outer);
    stack->setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Expanding);

    const QStringList paneLabels = {
        QStringLiteral("服务连接"), QStringLiteral("存储与输出"),
        QStringLiteral("成片设置"), QStringLiteral("AI 与 ASR"),
        QStringLiteral("快捷键"), QStringLiteral("通用"),
    };
    QVector<PaneNavButton *> navButtons;
    for (const QString &label : paneLabels) {
        auto *button = new PaneNavButton(label, side);
        navButtons.append(button);
        sideLayout->addWidget(button);
    }
    // 全部按钮创建完成后再接线：每个 handler 都要持有完整列表，才能清掉其余项的高亮。
    for (int index = 0; index < navButtons.size(); ++index) {
        connect(navButtons[index], &QAbstractButton::clicked, this,
                [index, navButtons, stack] {
                    for (int i = 0; i < navButtons.size(); ++i) {
                        navButtons[i]->setActive(i == index);
                    }
                    stack->setCurrentIndex(index);
                });
    }
    sideLayout->addStretch(1);
    navButtons.first()->setActive(true);
    outerLayout->addWidget(side);
    outerLayout->addWidget(Ui::separator(true, outer));
    outerLayout->addWidget(stack, 1);

    // ---- Service connection -------------------------------------------------
    Pane servicePane = makePane(stack);
    QVBoxLayout *serviceLayout = servicePane.layout;
    auto *warning = new Card(Theme::alpha(Theme::Danger, 0.13),
                             Theme::alpha(Theme::Danger, 0.30), 8, servicePane.content);
    warning->setMinimumHeight(40);
    auto *warningLayout = new QHBoxLayout;
    warningLayout->setContentsMargins(14, 10, 14, 10);
    warningLayout->setSpacing(10);
    warning->contentLayout()->addLayout(warningLayout);
    warningLayout->addWidget(new StatusDotLine(Theme::Danger, warning));
    auto *warningText = Ui::label(
        QStringLiteral("尚未配置 API 密钥 —— ASR 转写与 AI 切片暂不可用，配置后自动生效。"),
        12, 400, Theme::Danger, warning);
    warningText->setWordWrap(true);
    warningLayout->addWidget(warningText, 1);
    m_serviceWarning = warning;
    warning->setVisible(config.service.apiKey.trimmed().isEmpty());
    serviceLayout->addWidget(warning);
    serviceLayout->addSpacing(18);

    auto *apiInput = new InputField(servicePane.content);
    apiInput->setText(config.service.apiUrl);
    auto *apiField = makeField(
        QStringLiteral("API 地址（OpenAI 兼容）"), apiInput,
        QStringLiteral("该网关已聚合 ASR 语音识别与大模型能力，配置一次即可使用全部 AI 功能，"
                       "无需单独填写。"),
        true, servicePane.content);
    serviceLayout->addWidget(apiField, 0, Qt::AlignLeft);
    serviceLayout->addSpacing(16);

    auto *keyInput = new InputField(servicePane.content);
    keyInput->setPassword(true);
    keyInput->setPlaceholder(QStringLiteral("sk-..."));
    keyInput->setText(config.service.apiKey);
    auto *keyField = makeField(
        QStringLiteral("API 密钥"), keyInput,
        QStringLiteral("密钥仅保存在本机，用于 LLM 网关、豆包 ASR 及剪映小助手；"
                       "未配置则「一键成片」不可用。还没有密钥？"
                       "通过「获取密钥」登录后即可查看密钥。"),
        true, servicePane.content);
    auto *getKeyLink = new LinkLabel(QStringLiteral("获取密钥"), keyField);
    getKeyLink->setPixelSize(11);
    getKeyLink->setTextColor(Theme::Primary);
    getKeyLink->setHoverColor(Theme::PrimaryHover);
    getKeyLink->setTrailingGlyph(Icons::Glyph::ExternalLink, 10);
    getKeyLink->setToolTip(
        QStringLiteral("前往 jcaigc.cn 登录后查看 API 密钥"));
    connect(getKeyLink, &QAbstractButton::clicked, this, [] {
        QDesktopServices::openUrl(QUrl(QStringLiteral("https://jcaigc.cn")));
    });
    keyField->setLabelExtra(getKeyLink);
    // 不使用 AlignLeft：否则布局按单行 sizeHint 分配高度，换行后的提示会被裁掉。
    serviceLayout->addWidget(keyField, 0);
    serviceLayout->addSpacing(16);

    auto *testButton = new Button(QStringLiteral("测试连接并保存"), Button::Primary,
                                  servicePane.content);
    serviceLayout->addWidget(testButton, 0, Qt::AlignLeft);
    stack->addWidget(servicePane.scroll);

    // ---- Storage ------------------------------------------------------------
    Pane storagePane = makePane(stack);
    QVBoxLayout *storageLayout = storagePane.layout;

    auto chooseDirectory = [this](InputField *input) {
        const QString selected = QFileDialog::getExistingDirectory(
            this, QStringLiteral("选择目录"), input->text());
        if (!selected.isEmpty()) {
            input->setText(QDir::toNativeSeparators(selected));
        }
    };
    auto pathRow = [chooseDirectory](InputField *input, QWidget *parent) {
        return makePathRow(input, parent,
                           [chooseDirectory, input] { chooseDirectory(input); });
    };
    auto cleanedPath = [](const QString &text, const QString &fallback) {
        const QString trimmed = text.trimmed();
        return trimmed.isEmpty() ? fallback : QDir::cleanPath(trimmed);
    };
    auto firstInteger = [](const QString &text, bool *ok) {
        QString digits;
        for (const QChar ch : text) {
            if (ch.isDigit()) {
                digits.append(ch);
            } else if (!digits.isEmpty()) {
                break;
            }
        }
        return digits.toInt(ok);
    };

    auto *draftInput = new InputField(storagePane.content);
    draftInput->setText(QDir::toNativeSeparators(config.storage.draftDir));
    auto *draftField = makeField(
        QStringLiteral("剪映草稿输出目录"), pathRow(draftInput, storagePane.content),
        QStringLiteral("生成的剪映草稿包输出到此目录，可直接拖入剪映继续后期。"), false,
        storagePane.content);
    storageLayout->addWidget(draftField, 0, Qt::AlignLeft);
    storageLayout->addSpacing(16);

    auto *exportInput = new InputField(storagePane.content);
    exportInput->setText(QDir::toNativeSeparators(config.storage.exportDir));
    auto *exportField = makeField(
        QStringLiteral("成片导出目录"), pathRow(exportInput, storagePane.content),
        QString(), false, storagePane.content);
    storageLayout->addWidget(exportField, 0, Qt::AlignLeft);
    storageLayout->addSpacing(16);

    auto *cacheInput = new InputField(storagePane.content);
    cacheInput->setText(QDir::toNativeSeparators(config.storage.cacheDir));
    auto *cacheField = makeField(
        QStringLiteral("临时缓存目录"), pathRow(cacheInput, storagePane.content),
        QStringLiteral("ASR 抽音频、切片裁剪的临时文件存放处。"), false,
        storagePane.content);
    storageLayout->addWidget(cacheField, 0, Qt::AlignLeft);
    storageLayout->addSpacing(16);

    auto *recordingInput = new InputField(storagePane.content);
    recordingInput->setText(QDir::toNativeSeparators(config.storage.recordingDir));
    auto *recordingField = makeField(
        QStringLiteral("录制/下载输出目录"), pathRow(recordingInput, storagePane.content),
        QStringLiteral("mp4 直链下载的原文件、ffmpeg 录制的 m3u8 与 ts 分片，"
                       "以及成片时合成的完整 MP4 保存到此目录。"),
        false, storagePane.content);
    storageLayout->addWidget(recordingField, 0, Qt::AlignLeft);
    storageLayout->addSpacing(16);

    auto *segmentInput = new InputField(storagePane.content);
    segmentInput->setFixedWidth(120);
    segmentInput->lineEdit()->setValidator(new QIntValidator(1, 30, segmentInput));
    segmentInput->setText(QString::number(config.recording.segmentSeconds));
    auto *segmentField = makeField(
        QStringLiteral("HLS 分段长度（秒）"), segmentInput,
        QStringLiteral("录制时按该时长切分 ts 分片，建议 2–6 秒。"), false,
        storagePane.content);
    storageLayout->addWidget(segmentField, 0, Qt::AlignLeft);
    storageLayout->addSpacing(16);

    auto *limitRow = new QWidget(storagePane.content);
    auto *limitLayout = new QHBoxLayout(limitRow);
    limitLayout->setContentsMargins(0, 0, 0, 0);
    limitLayout->setSpacing(10);
    auto *limitInput = new InputField(limitRow);
    limitInput->setFixedWidth(120);
    limitInput->setText(QStringLiteral("%1 GB").arg(config.storage.cacheLimitGb));
    auto *autoClean = new CheckBox(QStringLiteral("超过后自动清理最旧缓存"), limitRow);
    autoClean->setChecked(config.storage.autoCleanCache);
    limitLayout->addWidget(limitInput, 0, Qt::AlignVCenter);
    limitLayout->addWidget(autoClean, 0, Qt::AlignVCenter);
    limitLayout->addStretch(1);
    auto *limitField = makeField(
        QStringLiteral("缓存上限"), limitRow,
        QStringLiteral("超过上限后按最后修改时间从旧到新删除预览缓存与任务临时文件，"
                       "正在使用的文件不会被清理。"),
        false, storagePane.content);
    storageLayout->addWidget(limitField, 0, Qt::AlignLeft);
    storageLayout->addSpacing(18);

    // Saving is handled by the header action so every pane persists together.
    if (m_config) {
        auto *configHint = Ui::label(
            QStringLiteral("配置文件：%1")
                .arg(QDir::toNativeSeparators(m_config->filePath())),
            11, 400, Theme::Text3, storagePane.content);
        configHint->setWordWrap(true);
        configHint->setTextInteractionFlags(Qt::TextSelectableByMouse);
        storageLayout->addWidget(configHint);
    }
    stack->addWidget(storagePane.scroll);

    // ---- 成片设置 ------------------------------------------------------------
    Pane finishPane = makePane(stack);
    QVBoxLayout *finishLayout = finishPane.layout;

    auto *finishHeader = new QWidget(finishPane.content);
    finishHeader->setMaximumWidth(kFinishGroupWidth);
    finishHeader->setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Preferred);
    auto *finishHeaderLayout = new QHBoxLayout(finishHeader);
    finishHeaderLayout->setContentsMargins(0, 0, 0, 20);
    finishHeaderLayout->setSpacing(12);
    auto *sparkleHost = new QWidget(finishHeader);
    sparkleHost->setFixedSize(26, 26);
    sparkleHost->setStyleSheet(QStringLiteral(
        "background:rgba(110,139,255,0.14);border:1px solid rgba(110,139,255,0.25);"
        "border-radius:7px;"));
    auto *sparkleLayout = new QVBoxLayout(sparkleHost);
    sparkleLayout->setContentsMargins(0, 0, 0, 0);
    QPixmap sparklePixmap(13, 13);
    sparklePixmap.fill(Qt::transparent);
    {
        QPainter painter(&sparklePixmap);
        painter.setRenderHint(QPainter::Antialiasing, true);
        Icons::paint(&painter, QRectF(0, 0, 13, 13), Icons::Glyph::Sparkle,
                     Theme::Primary, 2.0);
    }
    auto *sparkleLabel = new QLabel(sparkleHost);
    sparkleLabel->setPixmap(sparklePixmap);
    sparkleLayout->addWidget(sparkleLabel, 0, Qt::AlignCenter);
    finishHeaderLayout->addWidget(sparkleHost, 0, Qt::AlignTop);

    auto *finishText = new QWidget(finishHeader);
    auto *finishTextLayout = new QVBoxLayout(finishText);
    finishTextLayout->setContentsMargins(0, 0, 0, 0);
    finishTextLayout->setSpacing(2);
    finishTextLayout->addWidget(
        Ui::label(QStringLiteral("自动成片包装"), 13, 500, Theme::Text1, finishText));
    auto *finishHint = Ui::label(
        QStringLiteral("应用到每次一键成片；关闭的项不会添加，生成的草稿仍可在"
                       "剪映中二次调整。"),
        11, 400, Theme::Text3, finishText);
    finishHint->setWordWrap(true);
    finishTextLayout->addWidget(finishHint);
    finishHeaderLayout->addWidget(finishText, 1);

    QVector<Switch *> finishSwitches;

    const QStringList toggleTitles = {
        QStringLiteral("自动添加视频转场"), QStringLiteral("自动添加特效"),
        QStringLiteral("自动添加滤镜"),     QStringLiteral("自动添加贴纸"),
        QStringLiteral("自动添加 BGM"),      QStringLiteral("自动添加音效"),
        QStringLiteral("中英文双字幕"),
    };
    const QStringList toggleHints = {
        QStringLiteral("在片段衔接处加入自然转场，避免生硬跳切。"),
        QStringLiteral("按画面内容叠加高光、强调等特效，突出重点动作。"),
        QStringLiteral("根据场景与光线匹配滤镜，多条成片色调统一。"),
        QStringLiteral("按讲解节奏叠加价格、箭头等贴纸，重点信息更醒目。"),
        QStringLiteral("按内容情绪匹配背景音乐，人声段自动压低音量。"),
        QStringLiteral("在转场与重点动作处补充音效，增强节奏感。"),
        QStringLiteral("在中文字幕下方同步展示英文译文，便于发布到海外平台。"),
    };
    const QVector<bool> toggleValues = {
        config.finish.transitions, config.finish.effects,
        config.finish.filters,     config.finish.stickers,
        config.finish.bgm,         config.finish.sfx,
        config.finish.bilingualSubtitles,
    };
    // 「恢复默认」与设计稿一致：六项自动包装默认开启，中英文双字幕默认关闭。
    const QVector<bool> toggleDefaults = {true, true, true, true, true, true, false};
    QVector<ToggleRow *> visualRows;
    QVector<ToggleRow *> audioRows;
    QVector<ToggleRow *> subtitleRows;
    QVector<Switch *> visualSwitches;
    QVector<Switch *> audioSwitches;
    QVector<Switch *> subtitleSwitches;
    for (int i = 0; i < toggleTitles.size(); ++i) {
        auto *row = new ToggleRow(toggleTitles.at(i), toggleHints.at(i),
                                  toggleValues.at(i));
        Switch *sw = row->toggle();
        finishSwitches.append(sw);
        if (i < 4) {
            visualRows.append(row);
            visualSwitches.append(sw);
        } else if (i < 6) {
            audioRows.append(row);
            audioSwitches.append(sw);
        } else {
            subtitleRows.append(row);
            subtitleSwitches.append(sw);
        }
    }

    auto *visualMeta = Ui::label(QString(), 10, 400, Theme::Text3);
    visualMeta->setFont(Theme::monoFont(10));
    auto *audioMeta = Ui::label(QString(), 10, 400, Theme::Text3);
    audioMeta->setFont(Theme::monoFont(10));
    auto *subtitleMeta = Ui::label(QString(), 10, 400, Theme::Text3);
    subtitleMeta->setFont(Theme::monoFont(10));

    auto *visualGroup = new SettingsGroup(Icons::Glyph::Eye, QStringLiteral("画面"),
                                          visualMeta, 0, finishPane.content);
    for (int i = 0; i < visualRows.size(); ++i) {
        visualGroup->bodyLayout()->addWidget(visualRows.at(i));
        if (i < visualRows.size() - 1) {
            visualGroup->bodyLayout()->addWidget(Ui::separator(false));
        }
    }
    auto *audioGroup = new SettingsGroup(Icons::Glyph::Music, QStringLiteral("声音"),
                                         audioMeta, 0, finishPane.content);
    for (int i = 0; i < audioRows.size(); ++i) {
        audioGroup->bodyLayout()->addWidget(audioRows.at(i));
        if (i < audioRows.size() - 1) {
            audioGroup->bodyLayout()->addWidget(Ui::separator(false));
        }
    }
    auto *subtitleGroup =
        new SettingsGroup(Icons::Glyph::Subtitles, QStringLiteral("字幕"),
                          subtitleMeta, 0, finishPane.content);
    for (int i = 0; i < subtitleRows.size(); ++i) {
        subtitleGroup->bodyLayout()->addWidget(subtitleRows.at(i));
        if (i < subtitleRows.size() - 1) {
            subtitleGroup->bodyLayout()->addWidget(Ui::separator(false));
        }
    }

    // 视频倍速分组。
    auto *speedGroup = new SettingsGroup(Icons::Glyph::Bolt, QStringLiteral("视频倍速"),
                                         nullptr, 14, finishPane.content);
    auto *speedLayout = speedGroup->bodyLayout();

    auto *modeRow = new QWidget(speedGroup);
    auto *modeLayout = new QHBoxLayout(modeRow);
    modeLayout->setContentsMargins(0, 0, 0, 0);
    modeLayout->setSpacing(6);
    auto *autoButton = new ChoiceButton(QStringLiteral("自动"), modeRow);
    auto *fixedButton = new ChoiceButton(QStringLiteral("指定倍速"), modeRow);
    modeLayout->addWidget(autoButton);
    modeLayout->addWidget(fixedButton);
    modeLayout->addStretch(1);
    speedLayout->addWidget(modeRow);

    // 自动：每条成片独立取速。
    auto *autoPanel = new QWidget(speedGroup);
    auto *autoLayout = new QVBoxLayout(autoPanel);
    autoLayout->setContentsMargins(0, 0, 0, 0);
    autoLayout->setSpacing(0);
    auto *autoRangeRow = new QWidget(autoPanel);
    auto *autoRangeLayout = new QHBoxLayout(autoRangeRow);
    autoRangeLayout->setContentsMargins(0, 0, 0, 0);
    autoRangeLayout->setSpacing(10);
    autoRangeLayout->addWidget(
        Ui::label(QStringLiteral("每条成片独立随机取速"), 12, 400, Theme::Text2,
                  autoRangeRow));
    autoRangeLayout->addStretch(1);
    auto *rangeTag = Ui::label(QStringLiteral("1.0× – 1.5×"), 11, 400, Theme::Primary,
                               autoRangeRow);
    rangeTag->setFont(Theme::monoFont(11));
    rangeTag->setStyleSheet(QStringLiteral(
        "color:#6E8BFF;background:rgba(110,139,255,0.14);border-radius:4px;"
        "padding:1px 6px;"));
    autoRangeLayout->addWidget(rangeTag, 0, Qt::AlignVCenter);
    autoLayout->addWidget(autoRangeRow);
    autoLayout->addSpacing(8);
    auto *autoNote = Ui::label(
        QStringLiteral("以 45 秒切片为例，成片约 30–45 秒，多条成片节奏各不相同。"),
        11, 400, Theme::Text3, autoPanel);
    autoNote->setWordWrap(true);
    autoLayout->addWidget(autoNote);
    speedLayout->addSpacing(12);
    speedLayout->addWidget(autoPanel);

    // 指定倍速：固定滑杆。
    auto *fixedPanel = new QWidget(speedGroup);
    auto *fixedLayout = new QVBoxLayout(fixedPanel);
    fixedLayout->setContentsMargins(0, 0, 0, 0);
    fixedLayout->setSpacing(0);
    auto *fixedTop = new QWidget(fixedPanel);
    auto *fixedTopLayout = new QHBoxLayout(fixedTop);
    fixedTopLayout->setContentsMargins(0, 0, 0, 0);
    fixedTopLayout->setSpacing(8);
    fixedTopLayout->addWidget(
        Ui::label(QStringLiteral("固定倍速"), 12, 400, Theme::Text2, fixedTop));
    fixedTopLayout->addStretch(1);
    auto *speedValue = Ui::label(QString(), 15, 500, Theme::Text1, fixedTop);
    speedValue->setFont(Theme::monoFont(15, 500));
    fixedTopLayout->addWidget(speedValue);
    fixedLayout->addWidget(fixedTop);
    fixedLayout->addSpacing(10);
    auto *speedSlider = new SliderControl(fixedPanel);
    speedSlider->setRange(0.5, 2.0, 0.05);
    speedSlider->setAccessibleName(QStringLiteral("固定倍速"));
    speedSlider->setFixedHeight(20);
    fixedLayout->addWidget(speedSlider);
    fixedLayout->addSpacing(2);
    fixedLayout->addWidget(new SpeedTicks(fixedPanel));
    fixedLayout->addSpacing(8);
    auto *fixedNote = Ui::label(QString(), 11, 400, Theme::Text3, fixedPanel);
    fixedNote->setWordWrap(true);
    fixedLayout->addWidget(fixedNote);
    speedLayout->addSpacing(12);
    speedLayout->addWidget(fixedPanel);

    // 单条成片时长分组。
    auto *durationChip = Ui::label(QString(), 11, 400, Theme::Primary);
    durationChip->setFont(Theme::monoFont(11));
    durationChip->setStyleSheet(QStringLiteral(
        "color:#6E8BFF;background:rgba(110,139,255,0.14);border-radius:4px;"
        "padding:1px 6px;"));
    auto *durationGroup =
        new SettingsGroup(Icons::Glyph::Clock, QStringLiteral("单条成片时长"),
                          durationChip, 14, finishPane.content);
    auto *durationSetting = new DurationSetting(durationGroup);
    durationGroup->bodyLayout()->addWidget(durationSetting);

    finishLayout->addWidget(finishHeader);
    finishLayout->addWidget(visualGroup);
    finishLayout->addSpacing(12);
    finishLayout->addWidget(audioGroup);
    finishLayout->addSpacing(12);
    finishLayout->addWidget(subtitleGroup);
    finishLayout->addSpacing(12);
    finishLayout->addWidget(speedGroup);
    finishLayout->addSpacing(12);
    finishLayout->addWidget(durationGroup);
    finishLayout->addStretch(1);
    stack->addWidget(finishPane.scroll);

    auto updateSpeedText = [speedSlider, speedValue, fixedNote] {
        const double value = speedSlider->value();
        speedValue->setText(formatSpeed(value));
        const int exampleSeconds = qMax(1, int(qRound(45.0 / value)));
        fixedNote->setText(
            QStringLiteral("以 45 秒切片为例，成片约 %1 秒，所有成片保持同一速度。")
                .arg(exampleSeconds));
    };
    auto setSpeedMode = [autoButton, fixedButton, autoPanel,
                         fixedPanel](bool automatic) {
        autoButton->setSelected(automatic);
        fixedButton->setSelected(!automatic);
        autoPanel->setVisible(automatic);
        fixedPanel->setVisible(!automatic);
    };
    autoButton->setSelected(config.finish.speedAuto);
    fixedButton->setSelected(!config.finish.speedAuto);
    autoPanel->setVisible(config.finish.speedAuto);
    fixedPanel->setVisible(!config.finish.speedAuto);
    speedSlider->setValue(config.finish.fixedSpeed);
    updateSpeedText();

    auto countEnabled = [](const QVector<Switch *> &switches) {
        int count = 0;
        for (Switch *sw : switches) {
            count += sw->isChecked() ? 1 : 0;
        }
        return count;
    };
    auto updateGroupMeta = [visualMeta, audioMeta, subtitleMeta, visualSwitches,
                            audioSwitches, subtitleSwitches, countEnabled] {
        const int visualCount = countEnabled(visualSwitches);
        const int audioCount = countEnabled(audioSwitches);
        const int subtitleCount = countEnabled(subtitleSwitches);
        visualMeta->setText(QStringLiteral("已开启 %1 / %2")
                                .arg(visualCount)
                                .arg(visualSwitches.size()));
        visualMeta->setStyleSheet(
            QStringLiteral("color:%1;background:transparent;")
                .arg((visualCount == visualSwitches.size() ? Theme::Success
                                                           : Theme::Text3)
                         .name()));
        audioMeta->setText(QStringLiteral("已开启 %1 / %2")
                               .arg(audioCount)
                               .arg(audioSwitches.size()));
        audioMeta->setStyleSheet(
            QStringLiteral("color:%1;background:transparent;")
                .arg((audioCount == audioSwitches.size() ? Theme::Success
                                                         : Theme::Text3)
                         .name()));
        subtitleMeta->setText(QStringLiteral("已开启 %1 / %2")
                                  .arg(subtitleCount)
                                  .arg(subtitleSwitches.size()));
        subtitleMeta->setStyleSheet(
            QStringLiteral("color:%1;background:transparent;")
                .arg((subtitleCount == subtitleSwitches.size() ? Theme::Success
                                                               : Theme::Text3)
                         .name()));
    };

    // 「恢复默认」：与分组卡片同宽的小号幽灵按钮。
    auto *reset = new Button(QStringLiteral("恢复默认"), Button::Ghost, finishHeader);
    reset->setCompact(true);
    reset->setLeadingGlyph(Icons::Glyph::Reset);
    finishHeaderLayout->addWidget(reset, 0, Qt::AlignVCenter);

    auto *durationSlider = durationSetting->slider();
    auto updateResetEnabled = [reset, finishSwitches, toggleDefaults, autoButton,
                               speedSlider, durationSlider] {
        bool defaults = autoButton->isSelected();
        for (int i = 0; i < finishSwitches.size(); ++i) {
            defaults =
                defaults && finishSwitches.at(i)->isChecked() == toggleDefaults.at(i);
        }
        defaults = defaults && qAbs(speedSlider->value() - 1.0) < 0.001;
        defaults = defaults && qAbs(durationSlider->value() - 60.0) < 0.001;
        reset->setEnabled(!defaults);
    };

    durationSetting->setDuration(config.finish.clipDuration);
    durationChip->setText(QStringLiteral("%1 秒").arg(durationSetting->duration()));

    connect(autoButton, &QAbstractButton::clicked, this, [setSpeedMode] {
        setSpeedMode(true);
    });
    connect(fixedButton, &QAbstractButton::clicked, this, [setSpeedMode] {
        setSpeedMode(false);
    });
    connect(speedSlider, &SliderControl::valueChanged, this,
            [updateSpeedText](double) { updateSpeedText(); });
    connect(durationSlider, &SliderControl::valueChanged, this,
            [durationChip](double value) {
                durationChip->setText(
                    QStringLiteral("%1 秒").arg(qRound(value)));
            });

    for (Switch *sw : finishSwitches) {
        connect(sw, &QAbstractButton::toggled, this,
                [updateGroupMeta, updateResetEnabled](bool) {
                    updateGroupMeta();
                    updateResetEnabled();
                });
    }
    connect(speedSlider, &SliderControl::valueChanged, this,
            [updateResetEnabled](double) { updateResetEnabled(); });
    connect(durationSlider, &SliderControl::valueChanged, this,
            [updateResetEnabled](double) { updateResetEnabled(); });
    connect(autoButton, &QAbstractButton::clicked, this,
            [updateResetEnabled] { updateResetEnabled(); });
    connect(fixedButton, &QAbstractButton::clicked, this,
            [updateResetEnabled] { updateResetEnabled(); });
    updateGroupMeta();
    updateResetEnabled();

    connect(reset, &QAbstractButton::clicked, this,
            [this, finishSwitches, toggleDefaults, setSpeedMode, speedSlider,
             autoButton, durationSetting, updateResetEnabled] {
                for (int i = 0; i < finishSwitches.size(); ++i) {
                    finishSwitches.at(i)->setChecked(toggleDefaults.at(i));
                }
                autoButton->setSelected(true);
                setSpeedMode(true);
                speedSlider->setValue(1.0);
                durationSetting->setDuration(60);
                updateResetEnabled();
                emit toastRequested(QStringLiteral("已恢复默认成片设置"));
            });

    // ---- AI & ASR -----------------------------------------------------------
    Pane aiPane = makePane(stack);
    QVBoxLayout *aiLayout = aiPane.layout;
    m_promptSelect = new SelectBox(aiPane.content);
    auto *promptField = makeField(QStringLiteral("新建项目时默认提示词"), m_promptSelect,
                                  QString(), false, aiPane.content);
    aiLayout->addWidget(promptField, 0, Qt::AlignLeft);
    aiLayout->addSpacing(16);

    // 选片时长由提示词控制，不再单独设置区间。
    auto *durationNote = Ui::label(
        QStringLiteral("成片时长由所选提示词控制，不同提示词对成片时长的要求不同。"),
        11, 400, Theme::Text3, aiPane.content);
    durationNote->setWordWrap(true);
    auto *durationField = makeField(QStringLiteral("成片时长"), durationNote, QString(),
                                    false, aiPane.content);
    aiLayout->addWidget(durationField, 0, Qt::AlignLeft);
    aiLayout->addSpacing(16);

    auto *subtitleCheck =
        new CheckBox(QStringLiteral("ASR 完成后自动导出 SRT 字幕"), aiPane.content);
    subtitleCheck->setChecked(config.ai.exportSrt);
    auto *subtitleField = makeField(QStringLiteral("字幕导出"), subtitleCheck, QString(), false,
                                    aiPane.content);
    aiLayout->addWidget(subtitleField, 0, Qt::AlignLeft);
    stack->addWidget(aiPane.scroll);

    // ---- Shortcuts ----------------------------------------------------------
    Pane shortcutPane = makePane(stack);
    QVBoxLayout *shortcutLayout = shortcutPane.layout;
    auto *shortcutRow = new QWidget(shortcutPane.content);
    auto *shortcutRowLayout = new QHBoxLayout(shortcutRow);
    shortcutRowLayout->setContentsMargins(10, 8, 10, 8);
    shortcutRowLayout->setSpacing(6);
    shortcutRowLayout->addWidget(makeKbd(QStringLiteral("Space"), shortcutRow));
    shortcutRowLayout->addWidget(
        Ui::label(QStringLiteral("播放 / 暂停"), 12, 400, Theme::Text2, shortcutRow));
    shortcutRowLayout->addStretch(1);
    shortcutRowLayout->addWidget(
        Ui::label(QStringLiteral("视频预览"), 12, 400, Theme::Text2, shortcutRow));
    auto *shortcutLine = Ui::separator(false, shortcutPane.content);
    shortcutLayout->addWidget(shortcutRow);
    shortcutLayout->addWidget(shortcutLine);
    stack->addWidget(shortcutPane.scroll);

    // ---- General ------------------------------------------------------------
    Pane generalPane = makePane(stack);
    QVBoxLayout *generalLayout = generalPane.layout;
    auto *themeTabs = new SegmentTabs(
        {QStringLiteral("深色（推荐）"), QStringLiteral("跟随系统")}, SegmentTabs::Segmented,
        generalPane.content);
    themeTabs->setCurrentIndex(config.general.theme == QLatin1String("system") ? 1 : 0);
    auto *themeField = makeField(QStringLiteral("外观主题"), themeTabs, QString(), false,
                                 generalPane.content);
    generalLayout->addWidget(themeField, 0, Qt::AlignLeft);
    auto *themeHint = Ui::label(
        QStringLiteral("当前版本仅提供深色主题；跟随系统偏好会保存，后续版本生效。"),
        11, 400, Theme::Text3, generalPane.content);
    themeHint->setWordWrap(true);
    generalLayout->addWidget(themeHint);
    generalLayout->addSpacing(16);

#if defined(LIVEMIX_PLATFORM_WINDOWS)
    const QString backgroundLabel =
        QStringLiteral("关闭窗口时最小化到系统托盘继续跑任务");
#else
    const QString backgroundLabel =
        QStringLiteral("关闭窗口时最小化到菜单栏继续跑任务");
#endif
    QVector<CheckBox *> generalChecks;
    auto *startup = makeCheckRow({QStringLiteral("开机时自动启动"), backgroundLabel,
                                  QStringLiteral("任务完成时播放提示音"),
                                  QStringLiteral("系统通知")},
                                 {}, generalPane.content, &generalChecks);
    if (generalChecks.size() == 4) {
        generalChecks[0]->setChecked(config.general.launchAtLogin);
        generalChecks[1]->setChecked(config.general.minimizeToBackground);
        generalChecks[2]->setChecked(config.general.soundOnComplete);
        generalChecks[3]->setChecked(config.general.systemNotifications);
    }
    auto *startupField = makeField(QStringLiteral("启动与后台"), startup, QString(), false,
                                   generalPane.content);
    generalLayout->addWidget(startupField, 0, Qt::AlignLeft);
    generalLayout->addSpacing(16);

    // 关于：版本、公司官网与 GitHub 开源仓库入口。
    auto *about = new Card(Theme::alpha(Theme::Surface2, 0.50), Theme::Border, 8,
                           generalPane.content);
    auto *aboutLayout = about->contentLayout();
    aboutLayout->setContentsMargins(14, 14, 14, 14);
    aboutLayout->setSpacing(0);

    auto *aboutTop = new QWidget(about);
    auto *aboutTopLayout = new QHBoxLayout(aboutTop);
    aboutTopLayout->setContentsMargins(0, 0, 0, 0);
    aboutTopLayout->setSpacing(10);

    auto *logo = new QLabel(aboutTop);
    logo->setPixmap(Icons::brandIcon().pixmap(32, 32));
    logo->setFixedSize(32, 32);
    logo->setScaledContents(true);
    logo->setAttribute(Qt::WA_TransparentForMouseEvents, true);
    aboutTopLayout->addWidget(logo, 0, Qt::AlignVCenter);

    auto *aboutNames = new QWidget(aboutTop);
    auto *aboutNamesLayout = new QVBoxLayout(aboutNames);
    aboutNamesLayout->setContentsMargins(0, 0, 0, 0);
    aboutNamesLayout->setSpacing(1);

    auto *productRow = new QWidget(aboutNames);
    auto *productLayout = new QHBoxLayout(productRow);
    productLayout->setContentsMargins(0, 0, 0, 0);
    productLayout->setSpacing(6);
    productLayout->addWidget(
        Ui::label(Brand::product(), 13, 500, Theme::Text1, productRow), 0,
        Qt::AlignBottom);
    auto *version = Ui::label(QStringLiteral("v%1").arg(QStringLiteral(LIVEMIX_VERSION)),
                              10, 400, Theme::Text3, productRow);
    version->setFont(Theme::monoFont(10));
    productLayout->addWidget(version, 0, Qt::AlignBottom);
    productLayout->addStretch(1);
    aboutNamesLayout->addWidget(productRow);

    auto *companyRow = new QWidget(aboutNames);
    auto *companyLayout = new QHBoxLayout(companyRow);
    companyLayout->setContentsMargins(0, 0, 0, 0);
    companyLayout->setSpacing(4);
    auto *companyLink = new LinkLabel(Brand::company(), companyRow);
    companyLink->setPixelSize(11);
    companyLink->setTextColor(Theme::Text3);
    companyLink->setHoverColor(Theme::Text1);
    companyLink->setUnderlineOnHover(false);
    companyLink->setToolTip(QStringLiteral("在浏览器中打开公司官网"));
    connect(companyLink, &QAbstractButton::clicked, this, [] {
        QDesktopServices::openUrl(QUrl(Brand::website()));
    });
    companyLayout->addWidget(companyLink, 0, Qt::AlignVCenter);
    companyLayout->addWidget(glyphLabel(Icons::Glyph::ExternalLink, 10, Theme::Text3,
                                        companyRow),
                             0, Qt::AlignVCenter);
    companyLayout->addWidget(
        Ui::label(QStringLiteral("· %1 开源").arg(Brand::license()), 11, 400,
                  Theme::Text3, companyRow),
        0, Qt::AlignVCenter);
    companyLayout->addStretch(1);
    aboutNamesLayout->addWidget(companyRow);
    aboutTopLayout->addWidget(aboutNames, 1);

    auto *checkUpdate = new Button(QStringLiteral("检查更新"), Button::Default, aboutTop);
    connect(checkUpdate, &QAbstractButton::clicked, this,
            [this] { emit toastRequested(QStringLiteral("已是最新版本")); });
    aboutTopLayout->addWidget(checkUpdate, 0, Qt::AlignVCenter);
    aboutLayout->addWidget(aboutTop);
    aboutLayout->addSpacing(12);
    aboutLayout->addWidget(Ui::separator(false, about));
    aboutLayout->addSpacing(12);

    auto *repoCard = new Card(Theme::Surface1, Theme::Border, 6, about);
    repoCard->setHoverColors(Theme::Surface2, Theme::BorderStrong);
    repoCard->setClickable(true);
    auto *repoLayout = new QHBoxLayout;
    repoLayout->setContentsMargins(10, 8, 10, 8);
    repoLayout->setSpacing(8);
    repoCard->contentLayout()->addLayout(repoLayout);
    repoLayout->addWidget(glyphLabel(Icons::Glyph::Github, 14, Theme::Text3, repoCard),
                          0, Qt::AlignVCenter);
    auto *repoText = new QWidget(repoCard);
    auto *repoTextLayout = new QVBoxLayout(repoText);
    repoTextLayout->setContentsMargins(0, 0, 0, 0);
    repoTextLayout->setSpacing(0);
    repoTextLayout->addWidget(
        Ui::label(QStringLiteral("GitHub 开源仓库"), 11, 400, Theme::Text1, repoText));
    repoTextLayout->addWidget(
        Ui::label(QStringLiteral("查看源码、设计说明与更新记录"), 10, 400, Theme::Text3,
                  repoText));
    repoLayout->addWidget(repoText, 1);
    repoLayout->addWidget(
        glyphLabel(Icons::Glyph::ExternalLink, 12, Theme::Text3, repoCard), 0,
        Qt::AlignVCenter);
    repoCard->setToolTip(QStringLiteral("在浏览器中打开 GitHub 开源仓库"));
    connect(repoCard, &Card::clicked, this, [] {
        QDesktopServices::openUrl(QUrl(Brand::repo()));
    });
    aboutLayout->addWidget(repoCard);

    auto *aboutField = makeField(QStringLiteral("关于"), about, QString(), false,
                                 generalPane.content);
    generalLayout->addWidget(aboutField, 0, Qt::AlignLeft);
    stack->addWidget(generalPane.scroll);

    auto persist = [this, apiInput, keyInput, draftInput, exportInput, cacheInput,
                    recordingInput, segmentInput, limitInput, autoClean,
                    subtitleCheck, themeTabs, generalChecks, finishSwitches,
                    autoButton, speedSlider, durationSetting,
                    cleanedPath, firstInteger](QString *error) -> bool {
        if (!m_config) {
            if (error) {
                *error = QStringLiteral("配置未加载。");
            }
            return false;
        }

        AppConfig updated = m_config->config();
        updated.service.apiUrl = apiInput->text().trimmed();
        updated.service.apiKey = keyInput->text().trimmed();

        updated.storage.draftDir =
            cleanedPath(draftInput->text(), updated.storage.draftDir);
        updated.storage.exportDir =
            cleanedPath(exportInput->text(), updated.storage.exportDir);
        updated.storage.cacheDir =
            cleanedPath(cacheInput->text(), updated.storage.cacheDir);
        updated.storage.recordingDir =
            cleanedPath(recordingInput->text(), updated.storage.recordingDir);
        updated.storage.autoCleanCache = autoClean->isChecked();

        bool limitOk = false;
        const int cacheLimit = firstInteger(limitInput->text(), &limitOk);
        if (limitOk) {
            updated.storage.cacheLimitGb = qBound(1, cacheLimit, 1000);
        }

        bool segmentOk = false;
        const int seconds = segmentInput->text().toInt(&segmentOk);
        updated.recording.segmentSeconds =
            segmentOk ? qBound(1, seconds, 30) : updated.recording.segmentSeconds;

        updated.ai.defaultPrompt = m_promptSelect ? m_promptSelect->currentText()
                                                  : updated.ai.defaultPrompt;
        updated.ai.exportSrt = subtitleCheck->isChecked();

        if (finishSwitches.size() == 7) {
            updated.finish.transitions = finishSwitches[0]->isChecked();
            updated.finish.effects = finishSwitches[1]->isChecked();
            updated.finish.filters = finishSwitches[2]->isChecked();
            updated.finish.stickers = finishSwitches[3]->isChecked();
            updated.finish.bgm = finishSwitches[4]->isChecked();
            updated.finish.sfx = finishSwitches[5]->isChecked();
            updated.finish.bilingualSubtitles = finishSwitches[6]->isChecked();
        }
        updated.finish.speedAuto = autoButton->isSelected();
        updated.finish.fixedSpeed = speedSlider->value();
        updated.finish.clipDuration = durationSetting->duration();

        updated.general.theme = themeTabs->currentIndex() == 1
            ? QStringLiteral("system")
            : QStringLiteral("dark");
        if (generalChecks.size() == 4) {
            updated.general.launchAtLogin = generalChecks[0]->isChecked();
            updated.general.minimizeToBackground = generalChecks[1]->isChecked();
            updated.general.soundOnComplete = generalChecks[2]->isChecked();
            updated.general.systemNotifications = generalChecks[3]->isChecked();
        }

        m_config->setConfig(updated);
        return m_config->save(error);
    };

    auto *saveButton = new Button(QStringLiteral("保存设置"), Button::Primary, this);
    header->addAction(saveButton);
    connect(saveButton, &QAbstractButton::clicked, this, [this, persist, keyInput] {
        QString error;
        if (!persist(&error)) {
            emit toastRequested(QStringLiteral("保存设置失败：%1").arg(error));
            return;
        }
        if (m_serviceWarning) {
            m_serviceWarning->setVisible(keyInput->text().trimmed().isEmpty());
        }
        emit toastRequested(QStringLiteral("设置已保存"));
        emit settingsSaved();
    });

    connect(testButton, &QAbstractButton::clicked, this,
            [this, testButton, apiInput, keyInput, persist] {
                const QString key = keyInput->text().trimmed();
                QUrl apiUrl = QUrl::fromUserInput(apiInput->text().trimmed());
                const QString scheme = apiUrl.scheme().toLower();
                if (key.isEmpty() || !apiUrl.isValid()
                    || (scheme != QLatin1String("http")
                        && scheme != QLatin1String("https"))) {
                    emit toastRequested(
                        QStringLiteral("请填写有效的 API 地址和密钥"));
                    return;
                }

                QString error;
                if (!persist(&error)) {
                    emit toastRequested(QStringLiteral("保存设置失败：%1").arg(error));
                    return;
                }
                if (m_serviceWarning) {
                    m_serviceWarning->setVisible(false);
                }
                emit settingsSaved();

                QString path = apiUrl.path();
                if (!path.endsWith(QLatin1Char('/'))) {
                    path += QLatin1Char('/');
                }
                apiUrl.setPath(path + QStringLiteral("models"));

                QNetworkRequest request(apiUrl);
                request.setRawHeader("Authorization",
                                     QByteArray("Bearer ") + key.toUtf8());
                request.setRawHeader("Accept", "application/json");

                testButton->setEnabled(false);
                testButton->setText(QStringLiteral("测试中…"));
                QNetworkReply *reply = m_network->get(request);
                QTimer::singleShot(8000, reply, [reply] {
                    if (reply->isRunning()) {
                        reply->abort();
                    }
                });
                connect(reply, &QNetworkReply::finished, this,
                        [this, reply, testButton] {
                            testButton->setEnabled(true);
                            testButton->setText(QStringLiteral("测试连接并保存"));
                            const QNetworkReply::NetworkError networkError = reply->error();
                            const int status = reply->attribute(
                                QNetworkRequest::HttpStatusCodeAttribute).toInt();
                            if (networkError == QNetworkReply::OperationCanceledError) {
                                emit toastRequested(QStringLiteral(
                                    "连接超时，请检查 API 地址或网络"));
                            } else if (networkError != QNetworkReply::NoError
                                       && status == 0) {
                                emit toastRequested(QStringLiteral("连接失败：%1")
                                                        .arg(reply->errorString()));
                            } else if (status == 200) {
                                emit toastRequested(QStringLiteral(
                                    "连接成功：网关可达，ASR / LLM 均可用"));
                            } else if (status == 401 || status == 403) {
                                emit toastRequested(
                                    QStringLiteral("服务可达，但 API 密钥无效（HTTP %1）")
                                        .arg(status));
                            } else if (status > 0) {
                                emit toastRequested(
                                    QStringLiteral("服务可达（HTTP %1）").arg(status));
                            } else {
                                emit toastRequested(QStringLiteral("连接失败：%1")
                                                        .arg(reply->errorString()));
                            }
                            reply->deleteLater();
                        });
            });

    stack->setCurrentIndex(0);
    // 卡片占满页头之外的剩余高度，滚动交给右侧面板自己处理。
    bodyLayout()->addWidget(outer, 1);
}

void SettingsPage::setPrompts(const QVector<PromptEntry> &prompts)
{
    if (!m_promptSelect) {
        return;
    }

    const QString current = m_promptSelect->currentText();
    m_promptSelect->blockSignals(true);
    m_promptSelect->clear();
    for (const PromptEntry &prompt : prompts) {
        m_promptSelect->addItem(prompt.name);
    }

    QString desired = current;
    if (desired.isEmpty() && m_config) {
        desired = m_config->config().ai.defaultPrompt;
    }
    int index = m_promptSelect->findText(desired);
    if (index < 0 && m_config) {
        index = m_promptSelect->findText(m_config->config().ai.defaultPrompt);
    }
    if (index >= 0) {
        m_promptSelect->setCurrentIndex(index);
    }
    m_promptSelect->blockSignals(false);
}
