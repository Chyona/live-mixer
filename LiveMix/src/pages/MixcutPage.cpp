#include "MixcutPage.h"

#include "../ui/Controls.h"
#include "../ui/Icons.h"
#include "../ui/Overlays.h"
#include "../ui/StudioPosters.h"
#include "../ui/StudioWidgets.h"
#include "../ui/WindowChrome.h"

#include <QAbstractButton>
#include <QApplication>
#include <QClipboard>
#include <QDateTime>
#include <QDir>
#include <QEnterEvent>
#include <QFileDialog>
#include <QFileInfo>
#include <QFontMetrics>
#include <QGridLayout>
#include <QGuiApplication>
#include <QHBoxLayout>
#include <QLabel>
#include <QMessageBox>
#include <QPainter>
#include <QPainterPath>
#include <QRegularExpression>
#include <QScrollArea>
#include <QScrollBar>
#include <QShortcut>
#include <QStandardPaths>
#include <QStackedWidget>
#include <QTextEdit>
#include <QTimer>
#include <QToolButton>
#include <QVBoxLayout>
#include <QtMath>

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
    host->setObjectName(QStringLiteral("mixIconCircle"));
    host->setStyleSheet(
        QStringLiteral("#mixIconCircle { background:%1; border:1px solid %2;"
                       " border-radius:%3px; }")
            .arg(cssColor(background), cssColor(border))
            .arg(size / 2));
    auto *layout = new QHBoxLayout(host);
    layout->setContentsMargins(0, 0, 0, 0);
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
    host->setObjectName(QStringLiteral("mixSoftPill"));
    host->setStyleSheet(
        QStringLiteral("#mixSoftPill { background:%1; border:1px solid %2;"
                       " border-radius:9px; }")
            .arg(cssColor(background), cssColor(border)));
    auto *layout = new QHBoxLayout(host);
    layout->setContentsMargins(8, 2, 8, 2);
    layout->setSpacing(4);
    layout->addWidget(Studio::iconLabel(glyph, 10, foreground, host), 0,
                      Qt::AlignVCenter);
    layout->addWidget(Ui::label(text, 9, QFont::Medium, foreground, host));
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
    layout->addWidget(Ui::label(text, 10, QFont::Normal, Theme::Text3, row));
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
    layout->addWidget(Ui::label(title, 13, QFont::DemiBold, Theme::Text1, row));
    if (!hint.isEmpty()) {
        layout->addWidget(Ui::label(hint, 10, QFont::Normal, Theme::Text3, row));
    }
    layout->addStretch(1);
    if (right) {
        layout->addWidget(right, 0, Qt::AlignVCenter);
    }
    return row;
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
    host->setObjectName(QStringLiteral("mixGenerationPill"));
    host->setStyleSheet(
        QStringLiteral("#mixGenerationPill { background:%1; border:1px solid %2;"
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
    layout->addWidget(value);
    *valueLabel = value;
    return host;
}

QWidget *makeInfoPanel(const QString &title, Icons::Glyph glyph, QWidget *content,
                       QWidget *parent)
{
    auto *box = new QWidget(parent);
    box->setObjectName(QStringLiteral("mixInfoPanel"));
    box->setStyleSheet(
        QStringLiteral("#mixInfoPanel { background:%1; border:1px solid %2;"
                       " border-radius:7px; }")
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
    row->setObjectName(QStringLiteral("mixFileRow"));
    row->setStyleSheet(
        QStringLiteral("#mixFileRow { background:%1; border:1px solid %2;"
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
    nameRow->addWidget(Studio::tagLabel(tag, Theme::Text2, Theme::Surface3, 8, row));
    texts->addLayout(nameRow);
    auto *descriptionLabel = Ui::label(description, 9, QFont::Normal, Theme::Text3, row);
    texts->addWidget(descriptionLabel);
    layout->addLayout(texts, 1);
    if (right) {
        layout->addWidget(right, 0, Qt::AlignVCenter);
    }
    return row;
}

QWidget *makeCopyButton(const QString &label, std::function<void()> onCopy,
                        QWidget *parent)
{
    auto *button = new GlyphButton(Icons::Glyph::Copy, 18, parent);
    button->setGlyphSize(11);
    button->setColors(Theme::Text3, Theme::Primary);
    button->setToolTipText(QStringLiteral("复制%1").arg(label));
    QObject::connect(button, &QAbstractButton::clicked, button,
                     [onCopy = std::move(onCopy)] {
                         if (onCopy) {
                             onCopy();
                         }
                     });
    return button;
}

void setPlayIcon(QToolButton *button, bool playing)
{
    if (!button) {
        return;
    }
    button->setIcon(Icons::glyphIcon(playing ? Icons::Glyph::Pause
                                             : Icons::Glyph::Play,
                                     Qt::white, 18, 2.2));
}

QToolButton *makePlayOverlay(QWidget *parent)
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

/* ------------------------------------------------------------------ */
/* 可点击的自定义卡片                                                   */
/* ------------------------------------------------------------------ */

class MixHeroButton : public QAbstractButton
{
public:
    explicit MixHeroButton(QWidget *parent = nullptr)
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
        painter.setBrush(Theme::Surface1);
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

class MixHeroArt : public QWidget
{
public:
    explicit MixHeroArt(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        setAttribute(Qt::WA_TransparentForMouseEvents, true);
        setMinimumWidth(190);
        setMinimumHeight(208);
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Expanding);
    }

    void setState(StudioData::MixcutStyle style, const QString &voiceId)
    {
        m_style = style;
        m_voiceId = voiceId;
        update();
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        const QRectF area = QRectF(rect());
        const StudioData::MixcutStyleMeta &meta = StudioData::mixcutStyleMeta(m_style);
        const StudioData::MixcutVoice voice = StudioData::getMixcutVoice(m_voiceId);
        const QString script = StudioData::generateMixcutScript(m_style, 0);
        const StudioData::MixcutScriptStats stats = StudioData::mixcutScriptStats(script);
        const QString caption = StudioData::mixcutScriptLines(script).value(0);
        QString highlight;
        for (const QString &keyword : meta.keywords) {
            if (caption.contains(keyword)) {
                highlight = keyword;
                break;
            }
        }

        Studio::MixPosterSpec leftSpec;
        leftSpec.scene = QStringLiteral("serum");
        leftSpec.title = QStringLiteral("产品特写");
        leftSpec.caption = QStringLiteral("三秒成膜，清爽不黏");
        leftSpec.timecode = QStringLiteral("00:12");
        leftSpec.badge = QStringLiteral("成片");
        painter.save();
        painter.translate(area.left() + area.width() * 0.18, area.top() + 18 + 87);
        painter.rotate(-7);
        painter.setOpacity(0.4);
        Studio::paintMixPoster(painter, QRectF(-49, -87, 98, 174), leftSpec);
        painter.restore();

        Studio::MixPosterSpec rightSpec;
        rightSpec.scene = QStringLiteral("street");
        rightSpec.title = meta.captionTitle;
        rightSpec.caption = caption;
        rightSpec.highlight = highlight;
        rightSpec.voiceName = voice.name;
        rightSpec.timecode = stats.durationLabel;
        rightSpec.progress = 38;
        rightSpec.badge = QStringLiteral("智能混剪");
        painter.save();
        painter.translate(area.right() - 60, 104);
        painter.rotate(3);
        Studio::paintMixPoster(painter, QRectF(-58, -104, 117, 208), rightSpec);
        painter.restore();

        auto drawChip = [&painter](const QRectF &r, const QString &text,
                                   Icons::Glyph glyph, const QColor &iconColor) {
            painter.setPen(QPen(Theme::BorderStrong, 1.0));
            painter.setBrush(Theme::alpha(Theme::Surface2, 0.95));
            painter.drawRoundedRect(r, 6, 6);
            Icons::paint(&painter, QRectF(r.left() + 7, r.center().y() - 5, 10, 10),
                         glyph, iconColor, 2.0);
            painter.setFont(Theme::font(9));
            painter.setPen(Theme::Text2);
            painter.drawText(r.adjusted(21, 0, -6, 0),
                             Qt::AlignLeft | Qt::AlignVCenter, text);
        };
        drawChip(QRectF(area.left(), area.top() + 62, 130, 22),
                 QStringLiteral("文案已生成 · %1 句").arg(stats.sentences),
                 Icons::Glyph::Sparkle, Theme::Ai);
        drawChip(QRectF(area.right() - 140, area.bottom() - 72, 140, 22),
                 QStringLiteral("配音就绪 · %1").arg(voice.name),
                 Icons::Glyph::Mic, voice.accent);
    }

private:
    StudioData::MixcutStyle m_style = StudioData::MixcutStyle::Planting;
    QString m_voiceId = QStringLiteral("linlan");
};

class MixGradientPanel : public QWidget
{
public:
    explicit MixGradientPanel(QWidget *parent = nullptr)
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

class DarkStage : public QWidget
{
public:
    explicit DarkStage(QWidget *parent = nullptr)
        : QWidget(parent)
    {
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        const QRectF rect = QRectF(this->rect()).adjusted(0.5, 0.5, -0.5, -0.5);
        painter.setPen(QPen(Theme::Border, 1.0));
        painter.setBrush(QColor(QStringLiteral("#0c0e14")));
        painter.drawRoundedRect(rect, 8, 8);
        QRadialGradient glow(rect.center().x(), rect.top() + rect.height() * 0.1,
                             rect.width() * 0.8);
        glow.setColorAt(0, QColor(QStringLiteral("#1b2130")));
        glow.setColorAt(1, QColor(QStringLiteral("#0c0e14")));
        QPainterPath clip;
        clip.addRoundedRect(rect, 8, 8);
        painter.setClipPath(clip);
        painter.fillRect(rect, glow);
    }
};

} // namespace

class VoiceChoiceButton : public QAbstractButton
{
public:
    VoiceChoiceButton(const StudioData::MixcutVoice &voice, bool selected,
                      QWidget *parent = nullptr)
        : QAbstractButton(parent)
        , m_voice(voice)
        , m_selected(selected)
    {
        setCursor(Qt::PointingHandCursor);
        setFixedHeight(46);
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
        setToolTip(voice.description);
    }

    void setSelected(bool selected)
    {
        m_selected = selected;
        update();
    }

    QString voiceId() const { return m_voice.id; }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        const QRectF rect = QRectF(this->rect()).adjusted(0.5, 0.5, -0.5, -0.5);
        painter.setPen(QPen(m_selected ? Theme::alpha(Theme::Primary, 0.55)
                                      : (m_hovered ? Theme::BorderStrong
                                                   : Theme::Border),
                            1.0));
        painter.setBrush(m_selected ? Theme::alpha(Theme::Primary, 0.14)
                                    : Theme::alpha(Theme::Surface2, 0.45));
        painter.drawRoundedRect(rect, 7, 7);

        const QRectF orb(6, 11, 24, 24);
        QLinearGradient gradient(orb.topLeft(), orb.bottomRight());
        gradient.setColorAt(0, m_voice.gradientFrom);
        gradient.setColorAt(1, m_voice.gradientTo);
        painter.setPen(QPen(QColor(255, 255, 255, 46), 1.0));
        painter.setBrush(gradient);
        painter.drawEllipse(orb);
        painter.setFont(Theme::font(9, QFont::DemiBold));
        painter.setPen(Qt::white);
        painter.drawText(orb, Qt::AlignCenter, m_voice.name.left(1));

        painter.setFont(Theme::font(10, QFont::Medium));
        painter.setPen(Theme::Text1);
        painter.drawText(QRectF(36, 7, width() - 62, 16),
                         Qt::AlignLeft | Qt::AlignVCenter, m_voice.name);
        if (m_voice.recommended) {
            const QFontMetrics metrics(Theme::font(7));
            const int width = metrics.horizontalAdvance(QStringLiteral("荐")) + 4;
            const QRectF badge(36 + QFontMetrics(Theme::font(10, QFont::Medium))
                                      .horizontalAdvance(m_voice.name)
                                  + 4,
                               8, width, 12);
            painter.setPen(Qt::NoPen);
            painter.setBrush(Studio::aiSoft());
            painter.drawRoundedRect(badge, 2, 2);
            painter.setFont(Theme::font(7));
            painter.setPen(Theme::Ai);
            painter.drawText(badge, Qt::AlignCenter, QStringLiteral("荐"));
        }
        painter.setFont(Theme::font(8));
        painter.setPen(Theme::Text3);
        painter.drawText(QRectF(36, 24, width() - 62, 14),
                         Qt::AlignLeft | Qt::AlignVCenter,
                         QStringLiteral("%1 · %2").arg(m_voice.label,
                                                       m_voice.tags.value(0)));

        const QRectF check(width() - 21, height() / 2.0 - 6.5, 13, 13);
        painter.setPen(QPen(m_selected ? Theme::Primary : Theme::BorderStrong, 1.0));
        painter.setBrush(m_selected ? Theme::Primary : Qt::NoBrush);
        painter.drawEllipse(check);
        if (m_selected) {
            Icons::paint(&painter, check.adjusted(3, 3, -3, -3), Icons::Glyph::Check,
                         Theme::OnAccent, 2.4);
        }
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
    StudioData::MixcutVoice m_voice;
    bool m_selected = false;
    bool m_hovered = false;
};

/* ------------------------------------------------------------------ */
/* MixcutPage                                                           */
/* ------------------------------------------------------------------ */

MixcutPage::MixcutPage(QWidget *parent)
    : QWidget(parent)
{
    m_project = StudioData::defaultMixcutProject();
    m_project.hasSource = false;
    m_project.source = StudioData::MixcutSource();
    m_project.voiceStatus = StudioData::VoiceStatus::Idle;

    m_analysisTimer = new QTimer(this);
    m_analysisTimer->setInterval(60);
    connect(m_analysisTimer, &QTimer::timeout, this, &MixcutPage::tickAnalysis);

    m_voiceTimer = new QTimer(this);
    m_voiceTimer->setInterval(60);
    connect(m_voiceTimer, &QTimer::timeout, this, &MixcutPage::tickVoice);

    m_voiceDebounce = new QTimer(this);
    m_voiceDebounce->setSingleShot(true);
    m_voiceDebounce->setInterval(900);
    connect(m_voiceDebounce, &QTimer::timeout, this, &MixcutPage::startVoiceJob);

    m_playTimer = new QTimer(this);
    m_playTimer->setInterval(100);
    connect(m_playTimer, &QTimer::timeout, this, &MixcutPage::updatePlayback);

    m_runTimer = new QTimer(this);
    m_runTimer->setInterval(100);
    connect(m_runTimer, &QTimer::timeout, this, [this] {
        if (!m_hasRun) {
            return;
        }
        const qint64 elapsed = QDateTime::currentMSecsSinceEpoch() - m_runStartedAt;
        m_run = StudioData::computeMixcutRun(m_project, elapsed, m_runId);
        updateProcessing();
        if (m_run.done) {
            finishGeneration();
        } else {
            const int percent = qMin(99, static_cast<int>(std::floor(m_run.progress)));
            emit taskStatusChanged(
                QStringLiteral("智能混剪中 %1% · %2")
                    .arg(percent)
                    .arg(StudioData::formatElapsed(m_run.elapsedMs)),
                true);
        }
    });

    m_clockTimer = new QTimer(this);
    m_clockTimer->setInterval(10 * 1000);
    connect(m_clockTimer, &QTimer::timeout, this, &MixcutPage::updateGenerationClock);
    m_clockTimer->start();

    buildUi();
}

MixcutPage::~MixcutPage() = default;

void MixcutPage::buildUi()
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

    m_studioScreen = new QWidget(m_stack);
    m_stack->addWidget(m_studioScreen);
    m_processingScreen = new QWidget(m_stack);
    m_stack->addWidget(m_processingScreen);
    buildProcessingScreen();
    m_resultScreen = new QWidget(m_stack);
    m_stack->addWidget(m_resultScreen);

    rebuildHub();
}

bool MixcutPage::isRunning() const
{
    return m_hasRun && !m_run.done;
}

int MixcutPage::activeStep() const
{
    if (!m_project.hasSource) {
        return 1;
    }
    if (canStart()) {
        return 3;
    }
    return 2;
}

bool MixcutPage::canStart() const
{
    const StudioData::MixcutScriptStats stats =
        StudioData::mixcutScriptStats(m_project.script);
    return m_project.hasSource && m_project.analysisReady
        && m_project.voiceStatus == StudioData::VoiceStatus::Ready
        && stats.chars > 0;
}

/* ------------------------------------------------------------------ */
/* 首页                                                                 */
/* ------------------------------------------------------------------ */

void MixcutPage::showHub()
{
    m_screen = Screen::Hub;
    rebuildHub();
    m_stack->setCurrentWidget(m_hubScreen);
    if (m_hubScroll && m_hubScroll->verticalScrollBar()) {
        m_hubScroll->verticalScrollBar()->setValue(0);
    }
}

void MixcutPage::showStudio()
{
    m_screen = Screen::Studio;
    rebuildStudio();
    m_stack->setCurrentWidget(m_studioScreen);
}

void MixcutPage::showProcessing()
{
    m_screen = Screen::Processing;
    m_stack->setCurrentWidget(m_processingScreen);
}

void MixcutPage::showResult()
{
    m_screen = Screen::Result;
    rebuildResult();
    m_stack->setCurrentWidget(m_resultScreen);
}

void MixcutPage::rebuildHub()
{
    Studio::clearLayout(m_hubLayout);

    auto *header = new PageHeader(
        QStringLiteral("智能混剪"),
        QStringLiteral("上传一段视频，AI 写文案、配音并自动成片，成片与剪映草稿一次交付。"),
        this);
    header->setTitleMeta(new Chip(QStringLiteral("新功能"), Chip::Ai, header));
    auto *start = new Button(QStringLiteral("开始混剪"), Button::Primary, header);
    start->setLeadingGlyph(Icons::Glyph::Sparkle);
    connect(start, &QAbstractButton::clicked, this, &MixcutPage::startProject);
    header->addAction(start);
    m_hubLayout->addWidget(header);

    auto *stats = new QWidget(m_hubContent);
    auto *statsLayout = new QHBoxLayout(stats);
    statsLayout->setContentsMargins(0, 0, 0, 16);
    statsLayout->setSpacing(10);
    auto addStat = [&](const QString &html) {
        statsLayout->addWidget(Ui::richLabel(html, 10, Theme::Text3, stats));
    };
    addStat(QStringLiteral("本月已混剪 <b style=\"color:%1\">18</b> 条")
                .arg(Theme::Text1.name()));
    statsLayout->addWidget(Ui::label(QStringLiteral("·"), 10, QFont::Normal,
                                     Theme::BorderStrong, stats));
    addStat(QStringLiteral("平均成片用时 <b style=\"color:%1\">2 分 46 秒</b>")
                .arg(Theme::Text1.name()));
    statsLayout->addWidget(Ui::label(QStringLiteral("·"), 10, QFont::Normal,
                                     Theme::BorderStrong, stats));
    addStat(QStringLiteral("文案一次通过率 <b style=\"color:%1\">91%</b>")
                .arg(Theme::Success.name()));
    statsLayout->addStretch(1);
    statsLayout->addWidget(new StatusDot(6, Theme::Success, stats));
    statsLayout->addWidget(Ui::label(
        QStringLiteral("本地音色库已就绪 · %1 种音色")
            .arg(StudioData::mixcutVoices().size()),
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
    auto *recentMeta = new QWidget(m_hubContent);
    auto *recentMetaLayout = new QHBoxLayout(recentMeta);
    recentMetaLayout->setContentsMargins(0, 0, 0, 0);
    recentMetaLayout->setSpacing(4);
    recentMetaLayout->addWidget(Studio::iconLabel(Icons::Glyph::Clock, 10,
                                                   Theme::Text3, recentMeta));
    recentMetaLayout->addWidget(Ui::label(QStringLiteral("自动保存生成记录"), 10,
                                           QFont::Normal, Theme::Text3, recentMeta));
    auto *recentHeading = makeSectionHeading(
        QStringLiteral("最近混剪"),
        QStringLiteral("%1 个项目").arg(StudioData::recentMixcutResults().size()),
        recentMeta, m_hubContent);
    auto *recentHeadingRow = new QWidget(m_hubContent);
    auto *recentHeadingLayout = new QHBoxLayout(recentHeadingRow);
    recentHeadingLayout->setContentsMargins(0, 0, 0, 8);
    recentHeadingLayout->addWidget(recentHeading, 1);
    m_hubLayout->addWidget(recentHeadingRow);

    auto *recentRow = new QWidget(m_hubContent);
    auto *recentLayout = new QHBoxLayout(recentRow);
    recentLayout->setContentsMargins(0, 0, 0, 0);
    recentLayout->setSpacing(12);
    const QVector<StudioData::MixcutResult> recents =
        StudioData::recentMixcutResults();
    for (int index = 0; index < recents.size(); ++index) {
        recentLayout->addWidget(createRecentCard(recents.at(index), recentRow), 1);
    }
    m_hubLayout->addWidget(recentRow);

    m_hubLayout->addSpacing(20);
    const StudioData::MixcutVoice currentVoice =
        StudioData::getMixcutVoice(m_project.voiceId);
    auto *voiceMeta = new QWidget(m_hubContent);
    auto *voiceMetaLayout = new QHBoxLayout(voiceMeta);
    voiceMetaLayout->setContentsMargins(0, 0, 0, 0);
    voiceMetaLayout->setSpacing(6);
    auto *dot = new QWidget(voiceMeta);
    dot->setFixedSize(6, 6);
    dot->setStyleSheet(QStringLiteral("background:%1;border-radius:3px;")
                           .arg(cssColor(currentVoice.accent)));
    voiceMetaLayout->addWidget(dot);
    voiceMetaLayout->addWidget(Ui::label(QStringLiteral("当前默认："), 10,
                                           QFont::Normal, Theme::Text3, voiceMeta));
    voiceMetaLayout->addWidget(Ui::label(
        QStringLiteral("%1 · %2").arg(currentVoice.name, currentVoice.label), 10,
        QFont::Medium, Theme::Text2, voiceMeta));
    auto *voiceHeading = makeSectionHeading(
        QStringLiteral("常用音色"),
        QStringLiteral("点击设为默认音色，开始混剪时自动套用"), voiceMeta,
        m_hubContent);
    auto *voiceHeadingRow = new QWidget(m_hubContent);
    auto *voiceHeadingLayout = new QHBoxLayout(voiceHeadingRow);
    voiceHeadingLayout->setContentsMargins(0, 0, 0, 8);
    voiceHeadingLayout->addWidget(voiceHeading, 1);
    m_hubLayout->addWidget(voiceHeadingRow);

    auto *voiceGridHost = new QWidget(m_hubContent);
    auto *voiceGrid = new QGridLayout(voiceGridHost);
    voiceGrid->setContentsMargins(0, 0, 0, 0);
    voiceGrid->setHorizontalSpacing(10);
    voiceGrid->setVerticalSpacing(10);
    const QVector<StudioData::MixcutVoice> &voices = StudioData::mixcutVoices();
    for (int index = 0; index < voices.size(); ++index) {
        voiceGrid->addWidget(createHubVoiceCard(voices.at(index).id, voiceGridHost),
                             index / 3, index % 3);
    }
    m_hubLayout->addWidget(voiceGridHost);

    m_hubLayout->addSpacing(16);
    auto *tip = new QWidget(m_hubContent);
    tip->setObjectName(QStringLiteral("mixHubTip"));
    tip->setStyleSheet(
        QStringLiteral("#mixHubTip { background:%1; border:1px solid %2;"
                       " border-radius:8px; }")
            .arg(Theme::alpha(Theme::Surface1, 0.6).name(QColor::HexArgb),
                 Theme::Border.name()));
    auto *tipLayout = new QHBoxLayout(tip);
    tipLayout->setContentsMargins(12, 9, 12, 9);
    tipLayout->setSpacing(8);
    tipLayout->addWidget(Studio::iconLabel(Icons::Glyph::Sparkle, 12, Theme::Ai, tip));
    auto *tipLabel = Ui::label(
        QStringLiteral("小提示：文案、配音与镜头匹配都会自动完成；开始后可先试听配音，再一键成片。示例文案约 %1 字。")
            .arg(StudioData::mixcutScriptStats(
                     StudioData::generateMixcutScript(m_project.style, 0))
                     .chars),
        10, QFont::Normal, Theme::Text3, tip);
    tipLabel->setWordWrap(true);
    tipLayout->addWidget(tipLabel, 1);
    auto *ratio = new QWidget(tip);
    auto *ratioLayout = new QHBoxLayout(ratio);
    ratioLayout->setContentsMargins(0, 0, 0, 0);
    ratioLayout->setSpacing(4);
    ratioLayout->addWidget(Studio::iconLabel(Icons::Glyph::Palette, 10,
                                              Theme::Text2, ratio));
    ratioLayout->addWidget(Ui::label(QStringLiteral("支持 9:16 / 16:9 / 1:1"), 10,
                                      QFont::Normal, Theme::Text2, ratio));
    tipLayout->addWidget(ratio, 0, Qt::AlignVCenter);
    m_hubLayout->addWidget(tip);
}

QWidget *MixcutPage::buildHubHero()
{
    auto *card = new MixHeroButton(m_hubContent);
    auto *layout = new QHBoxLayout(card);
    layout->setContentsMargins(16, 16, 16, 16);
    layout->setSpacing(12);

    auto *left = new QWidget(card);
    auto *leftLayout = new QVBoxLayout(left);
    leftLayout->setContentsMargins(0, 0, 0, 0);
    leftLayout->setSpacing(0);
    leftLayout->addWidget(makeSoftPill(
        Icons::Glyph::Sparkle, QStringLiteral("AI 文案 · 配音 · 成片"), Theme::Ai,
        Studio::aiSoft(), Theme::alpha(Theme::Ai, 0.30), left), 0, Qt::AlignLeft);
    leftLayout->addSpacing(9);
    leftLayout->addWidget(Ui::label(QStringLiteral("一段视频，AI 帮你混剪成片"), 17,
                                     QFont::DemiBold, Theme::Text1, left));
    leftLayout->addSpacing(4);
    auto *description = Ui::label(
        QStringLiteral("上传原始视频，AI 自动撰写口播文案、生成配音，再按语义匹配镜头，字幕、音效与 BGM 一次到位，直接交付成片与剪映草稿。"),
        11, QFont::Normal, Theme::Text2, left);
    description->setWordWrap(true);
    description->setMaximumWidth(360);
    leftLayout->addWidget(description);
    leftLayout->addSpacing(10);
    leftLayout->addWidget(makeBullet(QStringLiteral("AI 看片写稿，几秒生成成片文案"),
                                     Theme::Ai, left));
    leftLayout->addWidget(makeBullet(QStringLiteral("6 种自然音色，选中即自动配音"),
                                     Theme::Ai, left));
    leftLayout->addWidget(makeBullet(QStringLiteral("字幕 / 音效 / BGM 智能卡点"),
                                     Theme::Ai, left));
    leftLayout->addWidget(makeBullet(QStringLiteral("成片与剪映草稿一次交付"), Theme::Ai,
                                     left));
    leftLayout->addStretch(1);
    auto *action = new QWidget(left);
    auto *actionLayout = new QHBoxLayout(action);
    actionLayout->setContentsMargins(0, 0, 0, 0);
    actionLayout->setSpacing(4);
    actionLayout->addWidget(Ui::label(QStringLiteral("开始混剪"), 11,
                                       QFont::Medium, Theme::Primary, action));
    actionLayout->addWidget(Studio::iconLabel(Icons::Glyph::ChevronRight, 11,
                                               Theme::Primary, action));
    actionLayout->addStretch(1);
    leftLayout->addWidget(action);
    layout->addWidget(left, 108);

    auto *art = new MixHeroArt(card);
    art->setState(m_project.style, m_project.voiceId);
    layout->addWidget(art, 92);
    connect(card, &QAbstractButton::clicked, this, &MixcutPage::startProject);
    return card;
}

QWidget *MixcutPage::buildHubProcessPanel()
{
    auto *panel = new MixGradientPanel(m_hubContent);
    auto *layout = qobject_cast<QVBoxLayout *>(panel->layout());
    auto *header = new QWidget(panel);
    auto *headerLayout = new QHBoxLayout(header);
    headerLayout->setContentsMargins(0, 0, 0, 0);
    headerLayout->setSpacing(8);
    headerLayout->addWidget(makeIconCircle(Icons::Glyph::Wand, 24, Theme::Ai,
                                            Studio::aiSoft(),
                                            Theme::alpha(Theme::Ai, 0.30), header),
                            0, Qt::AlignVCenter);
    auto *titles = new QVBoxLayout;
    titles->setContentsMargins(0, 0, 0, 0);
    titles->setSpacing(1);
    titles->addWidget(Ui::label(QStringLiteral("一条原片，三步成片"), 12,
                                QFont::DemiBold, Theme::Text1, header));
    titles->addWidget(Ui::label(QStringLiteral("全程无需手动剪辑，AI 自动完成"), 9,
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
        {QStringLiteral("上传原片"),
         QStringLiteral("拖入一段视频，AI 自动识别镜头与画面内容。"),
         Icons::Glyph::Upload},
        {QStringLiteral("AI 文案配音"),
         QStringLiteral("按画面生成口播文案，选好音色即刻配音。"),
         Icons::Glyph::Sparkle},
        {QStringLiteral("一键成片"),
         QStringLiteral("字幕、音效与 BGM 自动配置，交付成片与草稿。"),
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
    note->setObjectName(QStringLiteral("mixProcessNote"));
    note->setStyleSheet(
        QStringLiteral("#mixProcessNote { background:%1; border:1px solid %2;"
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

QWidget *MixcutPage::createRecentCard(const StudioData::MixcutResult &result,
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

    auto *poster = new Studio::MixPosterWidget(row);
    poster->setFixedSize(66, 118);
    const StudioData::MixcutStyleMeta &meta = StudioData::mixcutStyleMeta(result.style);
    QString caption = StudioData::shortMixcutCaption(result.script, 12);
    QString highlight;
    for (const QString &keyword : meta.keywords) {
        if (caption.contains(keyword)) {
            highlight = keyword;
            break;
        }
    }
    Studio::MixPosterSpec spec;
    spec.scene = result.style == StudioData::MixcutStyle::Review
        ? QStringLiteral("serum")
        : (result.style == StudioData::MixcutStyle::Story ? QStringLiteral("face")
                                                          : QStringLiteral("street"));
    spec.title = result.projectName.split(QStringLiteral(" · ")).value(0);
    spec.caption = caption;
    spec.highlight = highlight;
    spec.ratio = result.ratio;
    spec.timecode = result.durationLabel;
    spec.badge = QStringLiteral("成片");
    poster->setSpec(spec);
    layout->addWidget(poster, 0, Qt::AlignTop);

    const StudioData::MixcutVoice voice = StudioData::getMixcutVoice(result.voiceId);
    auto *texts = new QWidget(row);
    auto *textsLayout = new QVBoxLayout(texts);
    textsLayout->setContentsMargins(0, 1, 0, 0);
    textsLayout->setSpacing(0);
    auto *top = new QHBoxLayout;
    top->setContentsMargins(0, 0, 0, 0);
    top->addWidget(new Chip(QStringLiteral("已完成"), Chip::Ok, texts));
    top->addStretch(1);
    auto *time = Ui::label(StudioData::formatGenerationTime(result.generatedAt), 9,
                           QFont::Normal, Theme::Text3, texts);
    time->setToolTip(QStringLiteral("生成时间：%1")
                         .arg(StudioData::formatGenerationFullTime(result.generatedAt)));
    top->addWidget(time);
    textsLayout->addLayout(top);
    textsLayout->addSpacing(6);
    auto *name = new ElidedLabel(result.projectName, texts);
    name->setPixelSize(12);
    name->setFontWeight(QFont::Medium);
    name->setTextColor(Theme::Text1);
    textsLayout->addWidget(name);
    textsLayout->addSpacing(2);
    textsLayout->addWidget(Ui::label(
        QStringLiteral("%1 · %2 · %3")
            .arg(voice.label, result.durationLabel, result.ratio),
        9, QFont::Normal, Theme::Text3, texts));
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

QWidget *MixcutPage::createHubVoiceCard(const QString &voiceId, QWidget *parent)
{
    const StudioData::MixcutVoice voice = StudioData::getMixcutVoice(voiceId);
    const bool selected = voiceId == m_project.voiceId;
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
    auto *row = new QWidget(card);
    auto *layout = new QHBoxLayout(row);
    layout->setContentsMargins(0, 0, 0, 0);
    layout->setSpacing(10);
    auto *orb = new Studio::VoiceOrbWidget(voice, 30, row);
    layout->addWidget(orb, 0, Qt::AlignVCenter);

    auto *texts = new QVBoxLayout;
    texts->setContentsMargins(0, 0, 0, 0);
    texts->setSpacing(1);
    auto *nameRow = new QHBoxLayout;
    nameRow->setContentsMargins(0, 0, 0, 0);
    nameRow->setSpacing(5);
    nameRow->addWidget(Ui::label(voice.name, 11, QFont::Medium, Theme::Text1, row));
    nameRow->addWidget(Ui::label(voice.label, 9, QFont::Normal, Theme::Text3, row));
    if (voice.recommended) {
        nameRow->addWidget(Studio::tagLabel(QStringLiteral("推荐"), Theme::Ai,
                                             Studio::aiSoft(), 8, row));
    }
    nameRow->addStretch(1);
    texts->addLayout(nameRow);
    texts->addWidget(Ui::label(
        QStringLiteral("%1 · %2").arg(voice.gender,
                                       voice.tags.join(QStringLiteral(" · "))),
        9, QFont::Normal, Theme::Text3, row));
    layout->addLayout(texts, 1);

    auto *check = new QWidget(row);
    check->setFixedSize(15, 15);
    check->setObjectName(QStringLiteral("mixVoiceCheck"));
    check->setStyleSheet(
        QStringLiteral("#mixVoiceCheck { background:%1; border:1px solid %2;"
                       " border-radius:7px; }")
            .arg(selected ? Theme::Primary.name() : QStringLiteral("transparent"),
                 selected ? Theme::Primary.name() : Theme::BorderStrong.name()));
    auto *checkLayout = new QHBoxLayout(check);
    checkLayout->setContentsMargins(0, 0, 0, 0);
    checkLayout->addWidget(
        Studio::iconLabel(Icons::Glyph::Check, 8,
                          selected ? Theme::OnAccent : QColor(0, 0, 0, 0), check),
        0, Qt::AlignCenter);
    layout->addWidget(check, 0, Qt::AlignVCenter);
    card->contentLayout()->addWidget(row);
    connect(card, &Card::clicked, this, [this, voiceId] { chooseVoice(voiceId); });
    return card;
}

void MixcutPage::startProject()
{
    if (isRunning()) {
        const auto answer = QMessageBox::question(
            this, QStringLiteral("开始新项目"),
            QStringLiteral("当前有正在生成的智能混剪，开始新项目会取消它，是否继续？"),
            QMessageBox::Yes | QMessageBox::No, QMessageBox::No);
        if (answer != QMessageBox::Yes) {
            return;
        }
        m_runTimer->stop();
        m_hasRun = false;
        emit taskStatusChanged(QString(), false);
    }

    m_analysisTimer->stop();
    m_voiceTimer->stop();
    m_voiceDebounce->stop();
    m_playTimer->stop();
    m_playing = false;
    m_playProgress = 0;

    const StudioData::MixcutStyle style = m_project.style;
    const QString voiceId = m_project.voiceId;
    const QString ratio = m_project.ratio;
    m_project = StudioData::defaultMixcutProject();
    m_project.hasSource = false;
    m_project.source = StudioData::MixcutSource();
    m_project.style = style;
    m_project.voiceId = voiceId;
    m_project.ratio = ratio;
    m_project.voiceStatus = StudioData::VoiceStatus::Idle;
    m_analysisProgress = 0;
    m_voiceProgress = 0;
    m_scriptVariant = 0;
    m_scriptEdited = false;
    m_hasResult = false;
    showStudio();
}

/* ------------------------------------------------------------------ */
/* 工作台                                                               */
/* ------------------------------------------------------------------ */

void MixcutPage::rebuildStudio()
{
    auto *root = qobject_cast<QVBoxLayout *>(m_studioScreen->layout());
    if (!root) {
        root = new QVBoxLayout(m_studioScreen);
        root->setContentsMargins(0, 0, 0, 0);
        root->setSpacing(0);
    }
    int scrollValue = 0;
    if (auto *oldScroll = m_studioScreen->findChild<QScrollArea *>()) {
        if (oldScroll->verticalScrollBar()) {
            scrollValue = oldScroll->verticalScrollBar()->value();
        }
    }
    Studio::clearLayout(root);

    m_previewPoster = nullptr;
    m_previewPlayButton = nullptr;
    m_previewAnalysisOverlay = nullptr;
    m_previewAnalysisLabel = nullptr;
    m_previewAnalysisProgress = nullptr;
    m_previewAnalysisBar = nullptr;
    m_previewSkeleton = nullptr;
    m_ratioControl = nullptr;
    m_previewStatusLabel = nullptr;
    m_previewMeta = nullptr;
    m_playBar = nullptr;
    m_previewRowPlay = nullptr;
    m_playClock = nullptr;
    m_playDuration = nullptr;
    m_styleControl = nullptr;
    m_promptEdit = nullptr;
    m_scriptEdit = nullptr;
    m_scriptOverlay = nullptr;
    m_scriptOverlayLabel = nullptr;
    m_scriptOverlayDetail = nullptr;
    m_scriptOverlayProgress = nullptr;
    m_scriptOverlayBar = nullptr;
    m_scriptSkeleton = nullptr;
    m_scriptMeta = nullptr;
    m_scriptStats = nullptr;
    m_scriptSave = nullptr;
    m_generateButton = nullptr;
    m_regenerateButton = nullptr;
    m_voiceCards.clear();
    m_voiceWave = nullptr;
    m_voicePlay = nullptr;
    m_voiceMeta = nullptr;
    m_voiceDurationLabel = nullptr;
    m_voiceNameLabel = nullptr;
    m_voiceStatus = nullptr;
    m_voiceStatusDot = nullptr;
    m_voiceRegenerate = nullptr;
    m_voiceProgressBar = nullptr;
    m_footerSummary = nullptr;
    m_footerHint = nullptr;
    m_startButton = nullptr;
    m_studioDescription = nullptr;
    m_studioStepLabel = nullptr;
    m_stepRail = nullptr;

    root->addWidget(buildStudioHeader());
    root->addWidget(Ui::separator(false, m_studioScreen));
    auto *scrollContent = new QWidget;
    auto *body = new QVBoxLayout(scrollContent);
    body->setContentsMargins(22, 14, 22, 16);
    body->setSpacing(0);
    body->setAlignment(Qt::AlignTop);
    body->addWidget(m_project.hasSource ? buildWorkbench() : buildUploadPanel());
    auto *scroll = makeScrollArea(scrollContent, m_studioScreen);
    root->addWidget(scroll, 1);
    root->addWidget(Ui::separator(false, m_studioScreen));
    root->addWidget(buildStudioFooter());

    refreshStudioState();
    QTimer::singleShot(0, scroll, [scroll, scrollValue] {
        if (scroll->verticalScrollBar()) {
            scroll->verticalScrollBar()->setValue(scrollValue);
        }
    });
}

QWidget *MixcutPage::buildStudioHeader()
{
    auto *header = new QWidget(m_studioScreen);
    auto *layout = new QVBoxLayout(header);
    layout->setContentsMargins(22, 10, 22, 10);
    layout->setSpacing(0);

    auto *breadcrumb = new QWidget(header);
    auto *breadcrumbLayout = new QHBoxLayout(breadcrumb);
    breadcrumbLayout->setContentsMargins(0, 0, 0, 0);
    breadcrumbLayout->setSpacing(6);
    auto *back = new LinkLabel(QStringLiteral("智能混剪"), breadcrumb);
    back->setPixelSize(10);
    back->setTextColor(Theme::Text3);
    back->setHoverColor(Theme::Text1);
    back->setLeadingGlyph(Icons::Glyph::ChevronLeft, 10);
    back->setUnderlineOnHover(false);
    connect(back, &QAbstractButton::clicked, this, &MixcutPage::showHub);
    breadcrumbLayout->addWidget(back);
    breadcrumbLayout->addWidget(Ui::label(QStringLiteral("/"), 10, QFont::Normal,
                                           Theme::BorderStrong, breadcrumb));
    breadcrumbLayout->addWidget(Ui::label(QStringLiteral("新建混剪"), 10,
                                           QFont::Normal, Theme::Text2, breadcrumb));
    breadcrumbLayout->addWidget(Ui::label(QStringLiteral("/"), 10, QFont::Normal,
                                           Theme::BorderStrong, breadcrumb));
    m_studioStepLabel = Ui::label(QString(), 10, QFont::Normal, Theme::Text2,
                                  breadcrumb);
    breadcrumbLayout->addWidget(m_studioStepLabel);
    breadcrumbLayout->addSpacing(4);
    breadcrumbLayout->addWidget(new StatusDot(6, Theme::Success, breadcrumb));
    breadcrumbLayout->addWidget(Ui::label(QStringLiteral("草稿已自动保存"), 9,
                                           QFont::Normal, Theme::Text3, breadcrumb));
    breadcrumbLayout->addStretch(1);
    auto *exit = new Button(QStringLiteral("退出编辑"), Button::Ghost, breadcrumb);
    exit->setCompact(true);
    connect(exit, &QAbstractButton::clicked, this, &MixcutPage::showHub);
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
    titleLine->addWidget(Ui::label(QStringLiteral("智能混剪工作台"), 16,
                                    QFont::DemiBold, Theme::Text1, header));
    if (m_project.hasSource && m_project.source.sample) {
        titleLine->addWidget(new Chip(QStringLiteral("示例视频"), Chip::Ai, header));
    }
    titleLine->addStretch(1);
    titles->addLayout(titleLine);
    m_studioDescription = Ui::label(QString(), 11, QFont::Normal, Theme::Text2,
                                    header);
    m_studioDescription->setWordWrap(true);
    titles->addWidget(m_studioDescription);
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
    addFlow(QStringLiteral("原片"));
    flowLayout->addWidget(Ui::label(QStringLiteral("→"), 9, QFont::Normal,
                                    Theme::BorderStrong, flow));
    addFlow(QStringLiteral("AI 文案配音"));
    flowLayout->addWidget(Ui::label(QStringLiteral("→"), 9, QFont::Normal,
                                    Theme::BorderStrong, flow));
    addFlow(QStringLiteral("成片 + 草稿"));
    titleLayout->addWidget(flow, 0, Qt::AlignTop);
    layout->addWidget(titleRow);
    layout->addSpacing(10);

    m_stepRail = new Studio::StepRail(header);
    m_stepRail->setSteps(
        {QStringLiteral("上传视频"), QStringLiteral("文案与配音"),
         QStringLiteral("一键成片")},
        {QStringLiteral("原始素材"), QStringLiteral("AI 自动生成"),
         QStringLiteral("字幕 · 音效 · BGM")});
    layout->addWidget(m_stepRail);
    return header;
}

QWidget *MixcutPage::buildUploadPanel()
{
    auto *content = new QWidget;
    auto *layout = new QHBoxLayout(content);
    layout->setContentsMargins(0, 0, 0, 0);
    layout->setSpacing(14);

    auto *sourceCard = new Studio::SectionCard(
        Icons::Glyph::Film, QStringLiteral("原始视频"), QStringLiteral("必填"),
        content);
    auto *uploadAction = new Button(QStringLiteral("上传视频"), Button::Ghost,
                                    sourceCard);
    uploadAction->setCompact(true);
    uploadAction->setLeadingGlyph(Icons::Glyph::Upload);
    connect(uploadAction, &QAbstractButton::clicked, this,
            &MixcutPage::importVideoFile);
    sourceCard->headerLayout()->addWidget(uploadAction);
    sourceCard->setBodyMargins(QMargins(12, 12, 12, 12));
    auto *sourceBody = sourceCard->bodyLayout();
    sourceBody->setSpacing(0);

    auto *drop = new Studio::DropZone(sourceCard);
    drop->setMinimumHeight(312);
    auto *dropLayout = drop->contentLayout();
    dropLayout->setAlignment(Qt::AlignCenter);
    dropLayout->addWidget(makeIconCircle(Icons::Glyph::Upload, 44, Theme::Text2,
                                          Theme::Surface2, Theme::BorderStrong, drop),
                          0, Qt::AlignHCenter);
    dropLayout->addSpacing(2);
    dropLayout->addWidget(Ui::label(QStringLiteral("拖入一段视频，开始智能混剪"), 12,
                                      QFont::Medium, Theme::Text1, drop),
                          0, Qt::AlignHCenter);
    auto *hint = Ui::label(
        QStringLiteral("支持 MP4 / MOV，建议 15 秒–3 分钟；竖屏、横屏都可以，AI 会自动识别镜头并适配画幅。"),
        10, QFont::Normal, Theme::Text3, drop);
    hint->setWordWrap(true);
    hint->setAlignment(Qt::AlignCenter);
    hint->setMaximumWidth(360);
    dropLayout->addWidget(hint, 0, Qt::AlignHCenter);
    auto *buttons = new QWidget(drop);
    auto *buttonsLayout = new QHBoxLayout(buttons);
    buttonsLayout->setContentsMargins(0, 2, 0, 0);
    buttonsLayout->setSpacing(8);
    auto *upload = new Button(QStringLiteral("上传视频"), Button::Primary, buttons);
    upload->setLeadingGlyph(Icons::Glyph::Upload);
    connect(upload, &QAbstractButton::clicked, this, &MixcutPage::importVideoFile);
    auto *sample = new Button(QStringLiteral("使用示例视频"), Button::Default,
                              buttons);
    connect(sample, &QAbstractButton::clicked, this, [this] {
        importSource(StudioData::mixcutDemoSource(), true);
    });
    buttonsLayout->addWidget(upload);
    buttonsLayout->addWidget(sample);
    dropLayout->addWidget(buttons, 0, Qt::AlignHCenter);
    connect(drop, &Studio::DropZone::filesDropped, this,
            [this](const QStringList &paths) {
                if (paths.isEmpty()) {
                    return;
                }
                const QFileInfo info(paths.first());
                const QString suffix = info.suffix().toLower();
                if (!QStringList({QStringLiteral("mp4"), QStringLiteral("mov"),
                                  QStringLiteral("m4v"), QStringLiteral("webm"),
                                  QStringLiteral("avi"), QStringLiteral("mkv")})
                         .contains(suffix)) {
                    emit toastRequested(QStringLiteral("请选择视频文件（MP4 / MOV）"));
                    return;
                }
                importSource(
                    StudioData::mixcutSourceFromFile(info.fileName(), info.size()),
                    false);
            });
    sourceBody->addWidget(drop);
    sourceBody->addSpacing(12);

    auto *features = new QWidget(sourceCard);
    auto *featureLayout = new QHBoxLayout(features);
    featureLayout->setContentsMargins(0, 0, 0, 0);
    featureLayout->setSpacing(8);
    struct Feature {
        QString title;
        QString description;
        Icons::Glyph glyph;
    };
    const QVector<Feature> featureItems = {
        {QStringLiteral("看片写稿"), QStringLiteral("理解画面内容，自动生成口播文案。"),
         Icons::Glyph::Sparkle},
        {QStringLiteral("音色配音"), QStringLiteral("6 种自然音色，选中即自动配音。"),
         Icons::Glyph::Mic},
        {QStringLiteral("自动包装"), QStringLiteral("字幕、音效与 BGM 一次完成。"),
         Icons::Glyph::Wand},
    };
    for (const Feature &feature : featureItems) {
        auto *box = new QWidget(features);
        box->setObjectName(QStringLiteral("mixFeatureBox"));
        box->setStyleSheet(
            QStringLiteral("#mixFeatureBox { background:%1; border:1px solid %2;"
                           " border-radius:7px; }")
                .arg(Theme::alpha(Theme::Surface2, 0.45).name(QColor::HexArgb),
                     Theme::Border.name()));
        auto *boxLayout = new QVBoxLayout(box);
        boxLayout->setContentsMargins(10, 8, 10, 8);
        boxLayout->setSpacing(3);
        auto *head = new QHBoxLayout;
        head->setContentsMargins(0, 0, 0, 0);
        head->setSpacing(5);
        head->addWidget(Studio::iconLabel(feature.glyph, 11, Theme::Ai, box));
        head->addWidget(Ui::label(feature.title, 10, QFont::Medium, Theme::Text1,
                                    box));
        head->addStretch(1);
        boxLayout->addLayout(head);
        auto *desc = Ui::label(feature.description, 9, QFont::Normal, Theme::Text3,
                               box);
        desc->setWordWrap(true);
        boxLayout->addWidget(desc);
        featureLayout->addWidget(box, 1);
    }
    sourceBody->addWidget(features);
    sourceBody->addSpacing(12);
    sourceBody->addWidget(new Studio::Note(
        QStringLiteral("上传后 AI 会自动撰写文案并生成配音；文案、音色与画幅都能在成片前调整，不满意可随时换一版。"),
        Studio::Note::Ai));

    auto *right = new QWidget(content);
    auto *rightLayout = new QVBoxLayout(right);
    rightLayout->setContentsMargins(0, 0, 0, 0);
    rightLayout->setSpacing(12);
    auto *previewCard = new Studio::SectionCard(
        Icons::Glyph::Play, QStringLiteral("成片效果"),
        QStringLiteral("示例预览"), right);
    previewCard->setBodyMargins(QMargins(12, 12, 12, 12));
    auto *stage = new DarkStage(previewCard);
    stage->setFixedHeight(380);
    auto *stageLayout = new QHBoxLayout(stage);
    stageLayout->setContentsMargins(12, 12, 12, 12);
    stageLayout->setAlignment(Qt::AlignCenter);
    auto *poster = new Studio::MixPosterWidget(stage);
    poster->setFixedSize(194, 344);
    Studio::MixPosterSpec spec;
    spec.scene = QStringLiteral("street");
    spec.title = QStringLiteral("夏天通勤防晒指南");
    spec.caption = QStringLiteral("这支防晒推开三秒成膜，不搓泥也不泛白。");
    spec.highlight = QStringLiteral("三秒成膜");
    spec.voiceName = QStringLiteral("林澜");
    spec.secondaryScenes = {QStringLiteral("serum"), QStringLiteral("face")};
    spec.timecode = QStringLiteral("00:24");
    spec.progress = 62;
    poster->setSpec(spec);
    stageLayout->addWidget(poster, 0, Qt::AlignCenter);
    previewCard->bodyLayout()->addWidget(stage);
    previewCard->bodyLayout()->addSpacing(10);
    auto *metrics = new QWidget(previewCard);
    auto *metricsLayout = new QHBoxLayout(metrics);
    metricsLayout->setContentsMargins(0, 0, 0, 0);
    metricsLayout->setSpacing(6);
    metricsLayout->addWidget(new Studio::StatTile(QStringLiteral("AI 文案"),
                                                   QStringLiteral("5 句 · 约 24 秒"),
                                                   metrics));
    metricsLayout->addWidget(new Studio::StatTile(QStringLiteral("自然配音"),
                                                   QStringLiteral("6 种音色"), metrics));
    metricsLayout->addWidget(new Studio::StatTile(QStringLiteral("自动包装"),
                                                   QStringLiteral("字幕 / 音效 / BGM"),
                                                   metrics));
    previewCard->bodyLayout()->addWidget(metrics);
    rightLayout->addWidget(previewCard);
    rightLayout->addWidget(new Studio::Note(
        QStringLiteral("不需要剪辑经验：上传视频后，AI 按画面写文案、生成配音并自动卡点，约 1 分钟即可交付成片与剪映草稿。"),
        Studio::Note::Primary));
    rightLayout->addStretch(1);

    layout->addWidget(sourceCard, 142);
    layout->addWidget(right, 100);
    return content;
}

QWidget *MixcutPage::buildWorkbench()
{
    auto *content = new QWidget;
    auto *layout = new QHBoxLayout(content);
    layout->setContentsMargins(0, 0, 0, 0);
    layout->setSpacing(14);

    auto *left = new QWidget(content);
    auto *leftLayout = new QVBoxLayout(left);
    leftLayout->setContentsMargins(0, 0, 0, 0);
    leftLayout->setSpacing(14);
    leftLayout->addWidget(buildPreviewCard());
    leftLayout->addWidget(buildSourceCard());

    auto *tip = new QWidget(left);
    tip->setObjectName(QStringLiteral("mixStudioTip"));
    tip->setStyleSheet(
        QStringLiteral("#mixStudioTip { background:%1; border:1px solid %2;"
                       " border-radius:8px; }")
            .arg(Theme::alpha(Theme::Surface1, 0.6).name(QColor::HexArgb),
                 Theme::Border.name()));
    auto *tipLayout = new QHBoxLayout(tip);
    tipLayout->setContentsMargins(12, 8, 12, 8);
    tipLayout->setSpacing(8);
    tipLayout->addWidget(Studio::iconLabel(Icons::Glyph::Wand, 11, Theme::Ai, tip));
    auto *tipLabel = Ui::label(
        QStringLiteral("字幕、音效与 BGM 均由 AI 自动配置；成片完成后自动下载，同时写入剪映草稿。"),
        9, QFont::Normal, Theme::Text3, tip);
    tipLabel->setWordWrap(true);
    tipLayout->addWidget(tipLabel, 1);
    auto *remove = new LinkLabel(QStringLiteral("移除视频"), tip);
    remove->setPixelSize(9);
    remove->setTextColor(Theme::Text3);
    remove->setHoverColor(Theme::Danger);
    remove->setUnderlineOnHover(true);
    connect(remove, &QAbstractButton::clicked, this, &MixcutPage::clearSource);
    tipLayout->addWidget(remove, 0, Qt::AlignVCenter);
    leftLayout->addWidget(tip);
    leftLayout->addStretch(1);

    auto *right = new QWidget(content);
    auto *rightLayout = new QVBoxLayout(right);
    rightLayout->setContentsMargins(0, 0, 0, 0);
    rightLayout->setSpacing(14);
    rightLayout->addWidget(buildScriptCard());
    rightLayout->addWidget(buildVoiceSection());
    rightLayout->addWidget(buildAutoPackaging());
    rightLayout->addStretch(1);

    layout->addWidget(left, 104);
    layout->addWidget(right, 96);
    return content;
}

QWidget *MixcutPage::buildPreviewCard()
{
    auto *card = new Studio::SectionCard(
        Icons::Glyph::Play, QStringLiteral("成片预览"),
        m_project.voiceStatus == StudioData::VoiceStatus::Ready
            ? QStringLiteral("可试听")
            : QStringLiteral("文案与配音完成后可试听"),
        m_studioScreen);
    m_previewMeta = card->metaLabel();
    auto *ratioChip = new QLabel(m_project.ratio, card);
    ratioChip->setFont(Theme::monoFont(9));
    ratioChip->setStyleSheet(
        QStringLiteral("color:%1;background:%2;border:1px solid %3;"
                       "border-radius:4px;padding:1px 5px;")
            .arg(Theme::Text3.name(), Theme::Surface2.name(), Theme::Border.name()));
    card->headerLayout()->addWidget(ratioChip);
    card->setBodyMargins(QMargins(12, 12, 12, 12));
    auto *body = card->bodyLayout();
    body->setSpacing(0);

    auto *stage = new DarkStage(card);
    stage->setFixedHeight(380);
    auto *stageLayout = new QHBoxLayout(stage);
    stageLayout->setContentsMargins(12, 12, 12, 12);
    stageLayout->setAlignment(Qt::AlignCenter);

    const bool portrait = m_project.ratio == QLatin1String("9:16");
    const bool square = m_project.ratio == QLatin1String("1:1");
    int posterHeight = 348;
    if (m_project.ratio == QLatin1String("16:9")) {
        posterHeight = 196;
    } else if (square) {
        posterHeight = 296;
    }
    const int posterWidth = portrait ? qRound(posterHeight * 9.0 / 16.0)
        : (m_project.ratio == QLatin1String("16:9")
               ? qRound(posterHeight * 16.0 / 9.0)
               : posterHeight);
    auto *stack = new QWidget(stage);
    stack->setFixedSize(posterWidth, posterHeight);
    auto *stackLayout = new QGridLayout(stack);
    stackLayout->setContentsMargins(0, 0, 0, 0);
    m_previewPoster = new Studio::MixPosterWidget(stack);
    m_previewPoster->setFixedSize(posterWidth, posterHeight);
    stackLayout->addWidget(m_previewPoster, 0, 0);
    m_previewPlayButton = makePlayOverlay(stack);
    stackLayout->addWidget(m_previewPlayButton, 0, 0, Qt::AlignCenter);
    connect(m_previewPlayButton, &QToolButton::clicked, this,
            &MixcutPage::togglePlayback);

    m_previewAnalysisOverlay = new QWidget(stack);
    m_previewAnalysisOverlay->setFixedSize(posterWidth, posterHeight);
    m_previewAnalysisOverlay->setObjectName(QStringLiteral("mixAnalysisOverlay"));
    m_previewAnalysisOverlay->setStyleSheet(QStringLiteral(
        "#mixAnalysisOverlay { background:rgba(5,6,10,133); border-radius:10px; }"));
    auto *overlayLayout = new QVBoxLayout(m_previewAnalysisOverlay);
    overlayLayout->setContentsMargins(16, 16, 16, 16);
    overlayLayout->setSpacing(8);
    overlayLayout->setAlignment(Qt::AlignCenter);
    overlayLayout->addWidget(makeIconCircle(Icons::Glyph::Sparkle, 40, Theme::Ai,
                                             Studio::aiSoft(),
                                             Theme::alpha(Theme::Ai, 0.35),
                                             m_previewAnalysisOverlay),
                             0, Qt::AlignHCenter);
    m_previewAnalysisLabel = Ui::label(QStringLiteral("AI 正在看片并写文案"), 11,
                                       QFont::Medium, Qt::white,
                                       m_previewAnalysisOverlay);
    m_previewAnalysisLabel->setAlignment(Qt::AlignCenter);
    overlayLayout->addWidget(m_previewAnalysisLabel);
    auto *detail = Ui::label(
        QStringLiteral("已识别 %1 个镜头，完成后自动生成配音")
            .arg(m_project.source.shots),
        9, QFont::Normal, QColor(255, 255, 255, 153), m_previewAnalysisOverlay);
    detail->setAlignment(Qt::AlignCenter);
    detail->setWordWrap(true);
    overlayLayout->addWidget(detail);
    auto *progressRow = new QWidget(m_previewAnalysisOverlay);
    auto *progressLayout = new QVBoxLayout(progressRow);
    progressLayout->setContentsMargins(0, 0, 0, 0);
    progressLayout->setSpacing(4);
    auto *progressTop = new QHBoxLayout;
    progressTop->setContentsMargins(0, 0, 0, 0);
    progressTop->addWidget(Ui::label(QStringLiteral("分析画面"), 8, QFont::Normal,
                                       QColor(255, 255, 255, 140), progressRow));
    progressTop->addStretch(1);
    m_previewAnalysisProgress = monoLabel(QStringLiteral("0%"), 8,
                                          QColor(255, 255, 255, 140), progressRow);
    progressTop->addWidget(m_previewAnalysisProgress);
    progressLayout->addLayout(progressTop);
    m_previewAnalysisBar = new ProgressBar(progressRow);
    m_previewAnalysisBar->setBarColor(Theme::Ai);
    m_previewAnalysisBar->setTrackColor(QColor(255, 255, 255, 38));
    m_previewAnalysisBar->setValue(0);
    m_previewAnalysisBar->setFixedHeight(3);
    progressLayout->addWidget(m_previewAnalysisBar);
    progressRow->setFixedWidth(qMin(180, posterWidth - 32));
    overlayLayout->addWidget(progressRow, 0, Qt::AlignHCenter);
    stackLayout->addWidget(m_previewAnalysisOverlay, 0, 0, Qt::AlignCenter);
    m_previewAnalysisOverlay->raise();
    stageLayout->addWidget(stack, 0, Qt::AlignCenter);
    body->addWidget(stage);
    body->addSpacing(10);

    auto *playRow = new QWidget(card);
    auto *playLayout = new QHBoxLayout(playRow);
    playLayout->setContentsMargins(0, 0, 0, 0);
    playLayout->setSpacing(8);
    auto *play = new GlyphButton(Icons::Glyph::Play, 28, playRow);
    play->setGlyphSize(11);
    play->setColors(Theme::Text1, Theme::Primary);
    play->setBackgroundColors(Theme::Surface2, Theme::Surface3);
    play->setToolTipText(QStringLiteral("播放试听配音"));
    play->setText(QStringLiteral("播放试听"));
    connect(play, &QAbstractButton::clicked, this, &MixcutPage::togglePlayback);
    m_voicePlay = nullptr;
    m_previewRowPlay = play;
    playLayout->addWidget(play);
    auto *barHost = new QWidget(playRow);
    auto *barLayout = new QVBoxLayout(barHost);
    barLayout->setContentsMargins(0, 0, 0, 0);
    barLayout->setSpacing(2);
    m_playBar = new ProgressBar(barHost);
    m_playBar->setBarColor(Theme::Primary);
    m_playBar->setValue(0);
    m_playBar->setFixedHeight(4);
    barLayout->addWidget(m_playBar);
    auto *clockRow = new QHBoxLayout;
    clockRow->setContentsMargins(0, 0, 0, 0);
    m_playClock = monoLabel(QStringLiteral("00:00"), 8, Theme::Text3, barHost);
    clockRow->addWidget(m_playClock);
    clockRow->addStretch(1);
    m_playDuration = monoLabel(QStringLiteral("--:--"), 8, Theme::Text3, barHost);
    clockRow->addWidget(m_playDuration);
    barLayout->addLayout(clockRow);
    playLayout->addWidget(barHost, 1);
    m_previewStatusLabel = Ui::label(QStringLiteral("配音生成中"), 9, QFont::Normal,
                                     Theme::Text3, playRow);
    playLayout->addWidget(m_previewStatusLabel, 0, Qt::AlignVCenter);
    body->addWidget(playRow);
    body->addSpacing(10);

    body->addWidget(Ui::separator(false, card));
    body->addSpacing(10);
    auto *ratioRow = new QWidget(card);
    auto *ratioLayout = new QHBoxLayout(ratioRow);
    ratioLayout->setContentsMargins(0, 0, 0, 0);
    ratioLayout->setSpacing(10);
    ratioLayout->addWidget(Ui::label(QStringLiteral("成片画幅"), 10, QFont::Normal,
                                       Theme::Text3, ratioRow));
    m_ratioControl = new Studio::SegmentedControl(ratioRow);
    m_ratioControl->setOptions({QStringLiteral("9:16"), QStringLiteral("16:9"),
                                QStringLiteral("1:1")});
    m_ratioControl->setCompact(true);
    m_ratioControl->setFixedWidth(164);
    m_ratioControl->setCurrentIndex(m_project.ratio == QLatin1String("16:9")
                                        ? 1
                                        : (square ? 2 : 0));
    connect(m_ratioControl, &Studio::SegmentedControl::currentChanged, this,
            [this](int index) {
                chooseRatio(index == 0 ? QStringLiteral("9:16")
                                       : (index == 1 ? QStringLiteral("16:9")
                                                     : QStringLiteral("1:1")));
            });
    ratioLayout->addWidget(m_ratioControl);
    ratioLayout->addStretch(1);
    ratioLayout->addWidget(Ui::label(
        StudioData::mixcutRatioHint(m_project.ratio), 9, QFont::Normal,
        Theme::Text3, ratioRow));
    body->addWidget(ratioRow);
    return card;
}

QWidget *MixcutPage::buildSourceCard()
{
    auto *card = new Studio::SectionCard(
        Icons::Glyph::Film, QStringLiteral("原始视频"),
        QStringLiteral("%1 个镜头已识别").arg(m_project.source.shots),
        m_studioScreen);
    auto *replace = new Button(QStringLiteral("更换视频"), Button::Ghost, card);
    replace->setCompact(true);
    connect(replace, &QAbstractButton::clicked, this, &MixcutPage::importVideoFile);
    card->headerLayout()->addWidget(replace);
    card->setBodyMargins(QMargins(12, 12, 12, 12));
    auto *body = card->bodyLayout();
    body->setSpacing(0);

    auto *row = new QWidget(card);
    auto *rowLayout = new QHBoxLayout(row);
    rowLayout->setContentsMargins(0, 0, 0, 0);
    rowLayout->setSpacing(12);
    auto *scene = new Studio::MixSceneWidget(row);
    scene->setScene(m_project.source.scenes.value(0));
    scene->setFixedSize(88, 58);
    rowLayout->addWidget(scene);
    auto *texts = new QVBoxLayout;
    texts->setContentsMargins(0, 0, 0, 0);
    texts->setSpacing(2);
    auto *nameRow = new QHBoxLayout;
    nameRow->setContentsMargins(0, 0, 0, 0);
    nameRow->setSpacing(6);
    auto *name = new ElidedLabel(m_project.source.name, card);
    name->setPixelSize(11);
    name->setFontWeight(QFont::Medium);
    name->setTextColor(Theme::Text1);
    nameRow->addWidget(name, 1);
    if (m_project.source.sample) {
        nameRow->addWidget(new Chip(QStringLiteral("示例"), Chip::Ai, card));
    }
    texts->addLayout(nameRow);
    texts->addWidget(Ui::label(
        QStringLiteral("%1 · %2 · %3")
            .arg(m_project.source.size, m_project.source.resolution,
                 m_project.source.duration),
        9, QFont::Normal, Theme::Text3, card));
    auto *topics = new QWidget(card);
    auto *topicsLayout = new QHBoxLayout(topics);
    topicsLayout->setContentsMargins(0, 2, 0, 0);
    topicsLayout->setSpacing(4);
    const int topicCount = qMin(4, m_project.source.topics.size());
    for (int index = 0; index < topicCount; ++index) {
        topicsLayout->addWidget(Studio::tagLabel(
            m_project.source.topics.at(index), Theme::Ai,
            Theme::alpha(Theme::Ai, 0.13), 8, topics));
    }
    topicsLayout->addStretch(1);
    texts->addWidget(topics);
    rowLayout->addLayout(texts, 1);
    body->addWidget(row);
    body->addSpacing(10);

    auto *strip = new QWidget(card);
    auto *stripLayout = new QHBoxLayout(strip);
    stripLayout->setContentsMargins(0, 0, 0, 0);
    stripLayout->setSpacing(4);
    const QStringList scenes = m_project.source.scenes;
    for (int index = 0; index < scenes.size(); ++index) {
        auto *thumbHost = new QWidget(strip);
        auto *thumbLayout = new QVBoxLayout(thumbHost);
        thumbLayout->setContentsMargins(0, 0, 0, 0);
        thumbLayout->setSpacing(0);
        auto *thumb = new Studio::MixSceneWidget(thumbHost);
        thumb->setScene(scenes.at(index));
        thumb->setFixedHeight(36);
        thumb->setToolTip(QStringLiteral("镜头 %1 · %2")
                              .arg(index + 1)
                              .arg(StudioData::mixcutSceneLabel(scenes.at(index))));
        thumbLayout->addWidget(thumb);
        auto *number = new QLabel(QStringLiteral("%1").arg(index + 1, 2, 10,
                                                          QLatin1Char('0')),
                                  thumbHost);
        number->setAlignment(Qt::AlignCenter);
        number->setFont(Theme::monoFont(7));
        number->setStyleSheet(
            QStringLiteral("color:rgba(255,255,255,166);background:rgba(0,0,0,128);"));
        number->setFixedHeight(10);
        thumbLayout->addWidget(number);
        stripLayout->addWidget(thumbHost, 1);
    }
    body->addWidget(strip);
    body->addSpacing(6);
    auto *noteRow = new QWidget(card);
    auto *noteLayout = new QHBoxLayout(noteRow);
    noteLayout->setContentsMargins(0, 0, 0, 0);
    noteLayout->setSpacing(5);
    noteLayout->addWidget(Studio::iconLabel(Icons::Glyph::Sparkle, 9, Theme::Ai,
                                             noteRow));
    noteLayout->addWidget(Ui::label(
        QStringLiteral("已识别镜头与画面主体，成片时会按文案语义自动挑选。"), 9,
        QFont::Normal, Theme::Text3, noteRow), 1);
    noteLayout->addWidget(monoLabel(
        QStringLiteral("%1 段").arg(m_project.source.shots), 9, Theme::Text3,
        noteRow));
    body->addWidget(noteRow);
    return card;
}

QWidget *MixcutPage::buildScriptCard()
{
    auto *card = new Studio::SectionCard(Icons::Glyph::Sparkle,
                                          QStringLiteral("AI 口播文案"),
                                          m_studioScreen);
    m_scriptMeta = card->metaLabel();
    m_regenerateButton = new Button(QStringLiteral("换一版"), Button::Ghost, card);
    m_regenerateButton->setCompact(true);
    m_regenerateButton->setLeadingGlyph(Icons::Glyph::Reset);
    m_regenerateButton->setToolTip(QStringLiteral("让 AI 换一版文案（会沿用提示词）"));
    connect(m_regenerateButton, &QAbstractButton::clicked, this,
            &MixcutPage::regenerateScript);
    card->headerLayout()->addWidget(m_regenerateButton);
    card->setBodyMargins(QMargins(12, 12, 12, 12));
    auto *body = card->bodyLayout();
    body->setSpacing(0);

    auto *styleRow = new QWidget(card);
    auto *styleLayout = new QHBoxLayout(styleRow);
    styleLayout->setContentsMargins(0, 0, 0, 0);
    styleLayout->setSpacing(8);
    styleLayout->addWidget(Ui::label(QStringLiteral("口播风格"), 10, QFont::Normal,
                                       Theme::Text3, styleRow));
    m_styleControl = new Studio::SegmentedControl(styleRow);
    m_styleControl->setOptions({QStringLiteral("种草"), QStringLiteral("评测"),
                                QStringLiteral("故事")});
    m_styleControl->setCompact(true);
    const QVector<StudioData::MixcutStyle> styles = StudioData::mixcutStyles();
    int styleIndex = 0;
    for (int index = 0; index < styles.size(); ++index) {
        if (styles.at(index) == m_project.style) {
            styleIndex = index;
            break;
        }
    }
    m_styleControl->setCurrentIndex(styleIndex);
    connect(m_styleControl, &Studio::SegmentedControl::currentChanged, this,
            [this](int index) {
                const QVector<StudioData::MixcutStyle> all =
                    StudioData::mixcutStyles();
                if (index >= 0 && index < all.size()) {
                    chooseStyle(all.at(index));
                }
            });
    styleLayout->addWidget(m_styleControl, 1);
    body->addWidget(styleRow);
    body->addSpacing(8);

    auto *promptBox = new QWidget(card);
    promptBox->setObjectName(QStringLiteral("mixPromptBox"));
    promptBox->setStyleSheet(
        QStringLiteral("#mixPromptBox { background:%1; border:1px solid %2;"
                       " border-radius:7px; }")
            .arg(Theme::alpha(Theme::Surface2, 0.45).name(QColor::HexArgb),
                 Theme::Border.name()));
    auto *promptLayout = new QVBoxLayout(promptBox);
    promptLayout->setContentsMargins(8, 7, 8, 8);
    promptLayout->setSpacing(4);
    auto *promptHead = new QWidget(promptBox);
    auto *promptHeadLayout = new QHBoxLayout(promptHead);
    promptHeadLayout->setContentsMargins(0, 0, 0, 0);
    promptHeadLayout->setSpacing(5);
    promptHeadLayout->addWidget(Studio::iconLabel(Icons::Glyph::Sparkle, 9,
                                                   Theme::Ai, promptHead));
    promptHeadLayout->addWidget(Ui::label(QStringLiteral("文案提示词"), 9,
                                            QFont::Medium, Theme::Text2,
                                            promptHead));
    promptHeadLayout->addWidget(Ui::label(QStringLiteral("标题或原始文案"), 8,
                                            QFont::Normal, Theme::Text3,
                                            promptHead));
    promptHeadLayout->addStretch(1);
    auto *expand = new LinkLabel(QStringLiteral("放大"), promptHead);
    expand->setPixelSize(9);
    expand->setTextColor(Theme::Text3);
    expand->setHoverColor(Theme::Text1);
    expand->setLeadingGlyph(Icons::Glyph::Expand, 9);
    expand->setUnderlineOnHover(false);
    connect(expand, &QAbstractButton::clicked, this,
            &MixcutPage::openPromptEditor);
    promptHeadLayout->addWidget(expand);
    m_generateButton = new Button(QStringLiteral("生成文案"), Button::Ai, promptHead);
    m_generateButton->setCompact(true);
    m_generateButton->setLeadingGlyph(Icons::Glyph::Sparkle);
    m_generateButton->setToolTip(QStringLiteral("按提示词生成文案（⌘/Ctrl + Enter）"));
    connect(m_generateButton, &QAbstractButton::clicked, this,
            &MixcutPage::generateFromPrompt);
    promptHeadLayout->addWidget(m_generateButton);
    promptLayout->addWidget(promptHead);
    m_promptEdit = new QTextEdit(promptBox);
    Studio::applyTextAreaStyle(m_promptEdit, true, 10);
    m_promptEdit->setPlaceholderText(
        QStringLiteral("输入文案标题，或粘贴原始文案，例如：夏天通勤防晒怎么选"));
    m_promptEdit->setFixedHeight(52);
    m_promptEdit->setAcceptRichText(false);
    connect(m_promptEdit, &QTextEdit::textChanged, this,
            &MixcutPage::updatePromptFromEditor);
    promptLayout->addWidget(m_promptEdit);
    body->addWidget(promptBox);
    body->addSpacing(8);

    auto *scriptHost = new QWidget(card);
    auto *scriptStack = new QGridLayout(scriptHost);
    scriptStack->setContentsMargins(0, 0, 0, 0);
    m_scriptEdit = new QTextEdit(scriptHost);
    Studio::applyTextAreaStyle(m_scriptEdit, false, 11);
    m_scriptEdit->setPlaceholderText(
        QStringLiteral("输入口播文案，AI 会自动按句切成字幕并生成配音…"));
    m_scriptEdit->setAcceptRichText(false);
    m_scriptEdit->setFixedHeight(132);
    connect(m_scriptEdit, &QTextEdit::textChanged, this,
            &MixcutPage::updateScriptFromEditor);
    scriptStack->addWidget(m_scriptEdit, 0, 0);

    m_scriptOverlay = new QWidget(scriptHost);
    m_scriptOverlay->setObjectName(QStringLiteral("mixScriptOverlay"));
    m_scriptOverlay->setStyleSheet(QStringLiteral(
        "#mixScriptOverlay { background:rgba(7,9,14,200); border-radius:7px; }"));
    auto *overlayLayout = new QVBoxLayout(m_scriptOverlay);
    overlayLayout->setContentsMargins(16, 10, 16, 10);
    overlayLayout->setSpacing(4);
    overlayLayout->setAlignment(Qt::AlignCenter);
    m_scriptOverlayLabel = Ui::label(QStringLiteral("AI 正在看片并写文案"), 10,
                                      QFont::Normal, Theme::Ai,
                                      m_scriptOverlay);
    m_scriptOverlayLabel->setAlignment(Qt::AlignCenter);
    overlayLayout->addWidget(m_scriptOverlayLabel);
    m_scriptOverlayDetail = Ui::label(QString(), 8, QFont::Normal, Theme::Text3,
                                       m_scriptOverlay);
    m_scriptOverlayDetail->setAlignment(Qt::AlignCenter);
    overlayLayout->addWidget(m_scriptOverlayDetail);
    m_scriptSkeleton = new QWidget(m_scriptOverlay);
    auto *skeletonLayout = new QVBoxLayout(m_scriptSkeleton);
    skeletonLayout->setContentsMargins(0, 6, 0, 6);
    skeletonLayout->setSpacing(6);
    const QVector<int> widths = {92, 100, 84, 66};
    for (const int widthPercent : widths) {
        auto *lineHost = new QWidget(m_scriptSkeleton);
        auto *lineHostLayout = new QHBoxLayout(lineHost);
        lineHostLayout->setContentsMargins(0, 0, 0, 0);
        auto *line = new QWidget(lineHost);
        line->setFixedHeight(8);
        line->setStyleSheet(
            QStringLiteral("background:%1;border-radius:4px;")
                .arg(Theme::alpha(Theme::Surface3, 0.9).name()));
        lineHostLayout->addWidget(line, widthPercent);
        lineHostLayout->addStretch(100 - widthPercent);
        skeletonLayout->addWidget(lineHost);
    }
    overlayLayout->addWidget(m_scriptSkeleton);
    auto *overlayProgress = new QWidget(m_scriptOverlay);
    auto *overlayProgressLayout = new QHBoxLayout(overlayProgress);
    overlayProgressLayout->setContentsMargins(0, 4, 0, 0);
    overlayProgressLayout->setSpacing(6);
    m_scriptOverlayProgress = monoLabel(QStringLiteral("0%"), 8, Theme::Text3,
                                        overlayProgress);
    overlayProgressLayout->addWidget(m_scriptOverlayProgress);
    m_scriptOverlayBar = new ProgressBar(overlayProgress);
    m_scriptOverlayBar->setBarColor(Theme::Ai);
    m_scriptOverlayBar->setValue(0);
    m_scriptOverlayBar->setFixedHeight(2);
    overlayProgressLayout->addWidget(m_scriptOverlayBar, 1);
    overlayLayout->addWidget(overlayProgress);
    scriptStack->addWidget(m_scriptOverlay, 0, 0);
    body->addWidget(scriptHost);
    body->addSpacing(6);

    auto *statsRow = new QWidget(card);
    auto *statsLayout = new QHBoxLayout(statsRow);
    statsLayout->setContentsMargins(0, 0, 0, 0);
    statsLayout->setSpacing(7);
    m_scriptStats = Ui::label(QString(), 9, QFont::Normal, Theme::Text3, statsRow);
    statsLayout->addWidget(m_scriptStats, 1);
    auto *savedRow = new QWidget(statsRow);
    auto *savedLayout = new QHBoxLayout(savedRow);
    savedLayout->setContentsMargins(0, 0, 0, 0);
    savedLayout->setSpacing(3);
    savedLayout->addWidget(Studio::iconLabel(Icons::Glyph::Check, 9, Theme::Success,
                                              savedRow));
    m_scriptSave = Ui::label(QStringLiteral("已自动保存"), 9, QFont::Normal,
                             Theme::Success, savedRow);
    savedLayout->addWidget(m_scriptSave);
    statsLayout->addWidget(savedRow, 0);
    body->addWidget(statsRow);
    return card;
}

QWidget *MixcutPage::buildVoiceSection()
{
    auto *card = new Studio::SectionCard(Icons::Glyph::Mic, QStringLiteral("配音"),
                                          QStringLiteral("自动生成"),
                                          m_studioScreen);
    m_voiceMeta = card->metaLabel();
    card->setBodyMargins(QMargins(12, 12, 12, 12));
    auto *body = card->bodyLayout();
    body->setSpacing(0);

    auto *gridHost = new QWidget(card);
    auto *grid = new QGridLayout(gridHost);
    grid->setContentsMargins(0, 0, 0, 0);
    grid->setHorizontalSpacing(6);
    grid->setVerticalSpacing(6);
    const QVector<StudioData::MixcutVoice> &voices = StudioData::mixcutVoices();
    for (int index = 0; index < voices.size(); ++index) {
        auto *button = new VoiceChoiceButton(
            voices.at(index), voices.at(index).id == m_project.voiceId, gridHost);
        m_voiceCards.append(button);
        connect(button, &QAbstractButton::clicked, this,
                [this, id = voices.at(index).id] { chooseVoice(id); });
        grid->addWidget(button, index / 3, index % 3);
    }
    body->addWidget(gridHost);
    body->addSpacing(10);

    auto *playBox = new QWidget(card);
    playBox->setObjectName(QStringLiteral("mixVoicePlayBox"));
    playBox->setStyleSheet(
        QStringLiteral("#mixVoicePlayBox { background:%1; border:1px solid %2;"
                       " border-radius:7px; }")
            .arg(Theme::alpha(Theme::Surface2, 0.45).name(QColor::HexArgb),
                 Theme::Border.name()));
    auto *playLayout = new QVBoxLayout(playBox);
    playLayout->setContentsMargins(10, 8, 10, 8);
    playLayout->setSpacing(3);
    auto *controls = new QWidget(playBox);
    auto *controlsLayout = new QHBoxLayout(controls);
    controlsLayout->setContentsMargins(0, 0, 0, 0);
    controlsLayout->setSpacing(8);
    m_voicePlay = new GlyphButton(Icons::Glyph::Play, 24, controls);
    m_voicePlay->setGlyphSize(10);
    m_voicePlay->setColors(Theme::Text1, Theme::Primary);
    m_voicePlay->setBackgroundColors(Theme::Surface1, Theme::Surface3);
    m_voicePlay->setToolTipText(QStringLiteral("播放试听配音"));
    connect(m_voicePlay, &QAbstractButton::clicked, this,
            &MixcutPage::togglePlayback);
    controlsLayout->addWidget(m_voicePlay);
    m_voiceWave = new Studio::Waveform(controls);
    m_voiceWave->setBars(28);
    m_voiceWave->setFixedHeight(16);
    controlsLayout->addWidget(m_voiceWave, 1);
    m_voiceDurationLabel = monoLabel(QStringLiteral("—"), 8, Theme::Text3, controls);
    m_voiceDurationLabel->setFixedWidth(38);
    m_voiceDurationLabel->setAlignment(Qt::AlignRight | Qt::AlignVCenter);
    controlsLayout->addWidget(m_voiceDurationLabel);
    m_voiceRegenerate = new GlyphButton(Icons::Glyph::Reset, 20, controls);
    m_voiceRegenerate->setGlyphSize(10);
    m_voiceRegenerate->setColors(Theme::Text3, Theme::Primary);
    m_voiceRegenerate->setBackgroundColors(Qt::transparent, Theme::Surface3);
    m_voiceRegenerate->setToolTipText(QStringLiteral("重新生成配音"));
    connect(m_voiceRegenerate, &QAbstractButton::clicked, this,
            &MixcutPage::regenerateVoice);
    controlsLayout->addWidget(m_voiceRegenerate);
    playLayout->addWidget(controls);
    auto *statusRow = new QWidget(playBox);
    auto *statusLayout = new QHBoxLayout(statusRow);
    statusLayout->setContentsMargins(0, 0, 0, 0);
    statusLayout->setSpacing(5);
    m_voiceStatusDot = new QWidget(statusRow);
    m_voiceStatusDot->setFixedSize(4, 4);
    statusLayout->addWidget(m_voiceStatusDot);
    m_voiceStatus = Ui::label(QString(), 8, QFont::Normal, Theme::Text3, statusRow);
    statusLayout->addWidget(m_voiceStatus, 1);
    m_voiceNameLabel = Ui::label(QString(), 8, QFont::Normal, Theme::Text3,
                                  statusRow);
    statusLayout->addWidget(m_voiceNameLabel, 0);
    playLayout->addWidget(statusRow);
    m_voiceProgressBar = new ProgressBar(playBox);
    m_voiceProgressBar->setBarColor(Theme::Ai);
    m_voiceProgressBar->setValue(0);
    m_voiceProgressBar->setFixedHeight(2);
    playLayout->addWidget(m_voiceProgressBar);
    body->addWidget(playBox);
    return card;
}

QWidget *MixcutPage::buildAutoPackaging()
{
    auto *card = new Studio::SectionCard(Icons::Glyph::Wand,
                                          QStringLiteral("AI 自动包装"),
                                          QStringLiteral("无需手动设置"),
                                          m_studioScreen);
    card->setBodyMargins(QMargins(12, 12, 12, 12));
    auto *body = card->bodyLayout();
    body->setSpacing(0);
    auto *gridHost = new QWidget(card);
    auto *grid = new QGridLayout(gridHost);
    grid->setContentsMargins(0, 0, 0, 0);
    grid->setHorizontalSpacing(6);
    grid->setVerticalSpacing(6);
    struct Item {
        QString title;
        QString description;
        Icons::Glyph glyph;
    };
    const QVector<Item> items = {
        {QStringLiteral("智能字幕"), QStringLiteral("逐句生成 · 关键词高亮"),
         Icons::Glyph::Subtitles},
        {QStringLiteral("卡点音效"), QStringLiteral("转场与重音自动加入"),
         Icons::Glyph::Bolt},
        {QStringLiteral("情绪 BGM"), QStringLiteral("按文案匹配 · 人声闪避"),
         Icons::Glyph::Music},
        {QStringLiteral("镜头匹配"),
         QStringLiteral("从 %1 个镜头中按语义挑选").arg(m_project.source.shots),
         Icons::Glyph::Image},
    };
    for (int index = 0; index < items.size(); ++index) {
        auto *box = new QWidget(gridHost);
        box->setObjectName(QStringLiteral("mixAutoItem"));
        box->setStyleSheet(
            QStringLiteral("#mixAutoItem { background:%1; border:1px solid %2;"
                           " border-radius:7px; }")
                .arg(Theme::alpha(Theme::Surface2, 0.45).name(QColor::HexArgb),
                     Theme::Border.name()));
        auto *boxLayout = new QHBoxLayout(box);
        boxLayout->setContentsMargins(8, 6, 8, 6);
        boxLayout->setSpacing(8);
        boxLayout->addWidget(makeIconCircle(items.at(index).glyph, 24, Theme::Text2,
                                             Theme::Surface1, Theme::Border, box));
        auto *texts = new QVBoxLayout;
        texts->setContentsMargins(0, 0, 0, 0);
        texts->setSpacing(1);
        texts->addWidget(Ui::label(items.at(index).title, 10, QFont::Medium,
                                    Theme::Text1, box));
        auto *desc = Ui::label(items.at(index).description, 8, QFont::Normal,
                               Theme::Text3, box);
        desc->setWordWrap(true);
        texts->addWidget(desc);
        boxLayout->addLayout(texts, 1);
        boxLayout->addWidget(Ui::label(QStringLiteral("自动"), 8, QFont::Normal,
                                        Theme::Success, box), 0, Qt::AlignTop);
        grid->addWidget(box, index / 2, index % 2);
    }
    body->addWidget(gridHost);
    body->addSpacing(10);
    body->addWidget(Ui::separator(false, card));
    body->addSpacing(8);
    auto *delivery = new QWidget(card);
    auto *deliveryLayout = new QHBoxLayout(delivery);
    deliveryLayout->setContentsMargins(0, 0, 0, 0);
    deliveryLayout->setSpacing(6);
    deliveryLayout->addWidget(Ui::label(QStringLiteral("交付内容"), 10, QFont::Normal,
                                         Theme::Text2, delivery));
    deliveryLayout->addWidget(Studio::tagLabel(
        QStringLiteral("✓ MP4 成片"), Theme::Success,
        Theme::alpha(Theme::Success, 0.14), 8, delivery));
    deliveryLayout->addWidget(Studio::tagLabel(
        QStringLiteral("✓ 剪映草稿"), Theme::Text2, Theme::Surface2, 8, delivery));
    deliveryLayout->addStretch(1);
    deliveryLayout->addWidget(Ui::label(QStringLiteral("自动下载并写入草稿"), 8,
                                         QFont::Normal, Theme::Text3, delivery));
    body->addWidget(delivery);
    return card;
}

QWidget *MixcutPage::buildStudioFooter()
{
    auto *footer = new QWidget(m_studioScreen);
    footer->setFixedHeight(52);
    footer->setObjectName(QStringLiteral("mixStudioFooter"));
    footer->setStyleSheet(
        QStringLiteral("#mixStudioFooter { background:%1; }").arg(Theme::Surface1.name()));
    auto *layout = new QHBoxLayout(footer);
    layout->setContentsMargins(22, 0, 22, 0);
    layout->setSpacing(12);

    auto *texts = new QVBoxLayout;
    texts->setContentsMargins(0, 0, 0, 0);
    texts->setSpacing(1);
    m_footerSummary = new ElidedLabel(QString(), footer);
    m_footerSummary->setPixelSize(10);
    m_footerSummary->setTextColor(Theme::Text2);
    texts->addWidget(m_footerSummary);
    m_footerHint = Ui::label(QString(), 9, QFont::Normal, Theme::Text3, footer);
    texts->addWidget(m_footerHint);
    layout->addLayout(texts, 1);

    if (!m_project.hasSource) {
        auto *sample = new Button(QStringLiteral("使用示例视频"), Button::Default,
                                  footer);
        connect(sample, &QAbstractButton::clicked, this, [this] {
            importSource(StudioData::mixcutDemoSource(), true);
        });
        layout->addWidget(sample);
        auto *upload = new Button(QStringLiteral("上传视频"), Button::Primary,
                                  footer);
        upload->setLeadingGlyph(Icons::Glyph::Upload);
        connect(upload, &QAbstractButton::clicked, this,
                &MixcutPage::importVideoFile);
        layout->addWidget(upload);
    } else {
        m_startButton = new Button(QStringLiteral("一键成片"), Button::Primary,
                                   footer);
        m_startButton->setLeadingGlyph(Icons::Glyph::Sparkle);
        connect(m_startButton, &QAbstractButton::clicked, this,
                &MixcutPage::startGeneration);
        layout->addWidget(m_startButton);
    }
    return footer;
}

void MixcutPage::importVideoFile()
{
    const QString path = QFileDialog::getOpenFileName(
        this, QStringLiteral("选择原始视频"), QString(),
        QStringLiteral("视频文件 (*.mp4 *.mov *.m4v *.webm *.avi *.mkv)"));
    if (path.isEmpty()) {
        return;
    }
    const QFileInfo info(path);
    importSource(StudioData::mixcutSourceFromFile(info.fileName(), info.size()),
                 false);
}

void MixcutPage::importSource(const StudioData::MixcutSource &source, bool announce)
{
    if (m_project.hasSource && !m_project.script.trimmed().isEmpty()) {
        const auto answer = QMessageBox::question(
            this, QStringLiteral("更换视频"),
            QStringLiteral("更换视频会重新生成文案与配音，当前修改将丢失，是否继续？"),
            QMessageBox::Yes | QMessageBox::No, QMessageBox::No);
        if (answer != QMessageBox::Yes) {
            return;
        }
    }

    m_analysisTimer->stop();
    m_voiceTimer->stop();
    m_voiceDebounce->stop();
    m_playTimer->stop();
    m_playing = false;
    m_playProgress = 0;
    m_scriptEdited = false;
    m_scriptVariant = 0;
    m_analysisProgress = 0;
    m_voiceProgress = 0;

    m_project.source = source;
    m_project.hasSource = true;
    m_project.analysisReady = false;
    m_project.prompt.clear();
    m_project.script.clear();
    m_project.voiceStatus = StudioData::VoiceStatus::Idle;

    rebuildStudio();
    startAnalysis(2600,
                  announce
                      ? (source.sample
                             ? QStringLiteral("已载入示例视频，AI 正在看片并生成文案")
                             : QStringLiteral("已导入视频，AI 正在看片并生成文案"))
                      : QString());
}

void MixcutPage::clearSource()
{
    if (!m_project.script.trimmed().isEmpty()) {
        const auto answer = QMessageBox::question(
            this, QStringLiteral("移除视频"),
            QStringLiteral("移除后需要重新上传视频，是否继续？"),
            QMessageBox::Yes | QMessageBox::No, QMessageBox::No);
        if (answer != QMessageBox::Yes) {
            return;
        }
    }
    m_analysisTimer->stop();
    m_voiceTimer->stop();
    m_voiceDebounce->stop();
    m_playTimer->stop();
    m_playing = false;
    m_playProgress = 0;
    m_project.hasSource = false;
    m_project.source = StudioData::MixcutSource();
    m_project.analysisReady = false;
    m_project.prompt.clear();
    m_project.script.clear();
    m_project.voiceStatus = StudioData::VoiceStatus::Idle;
    m_analysisProgress = 0;
    m_voiceProgress = 0;
    rebuildStudio();
}

void MixcutPage::chooseStyle(StudioData::MixcutStyle style)
{
    if (style == m_project.style || !m_project.analysisReady) {
        return;
    }
    if (m_scriptEdited) {
        const auto answer = QMessageBox::question(
            this, QStringLiteral("切换风格"),
            QStringLiteral("切换风格会重写当前文案，手动修改的内容将丢失，是否继续？"),
            QMessageBox::Yes | QMessageBox::No, QMessageBox::No);
        if (answer != QMessageBox::Yes) {
            const QVector<StudioData::MixcutStyle> styles =
                StudioData::mixcutStyles();
            if (m_styleControl) {
                const int index = styles.indexOf(m_project.style);
                if (index >= 0) {
                    m_styleControl->setCurrentIndex(index);
                }
            }
            return;
        }
    }
    m_scriptVariant = 0;
    m_project.style = style;
    if (!m_project.hasSource) {
        refreshStudioState();
        return;
    }
    startAnalysis(1400, QStringLiteral("已切换为「%1」风格，正在重写文案")
                            .arg(StudioData::mixcutStyleName(style)));
}

void MixcutPage::chooseVoice(const QString &voiceId)
{
    if (voiceId == m_project.voiceId) {
        return;
    }
    m_project.voiceId = voiceId;
    m_playTimer->stop();
    m_playing = false;
    m_playProgress = 0;
    if (m_screen == Screen::Hub) {
        rebuildHub();
        emit toastRequested(
            QStringLiteral("已设为默认音色「%1 · %2」，开始混剪时自动套用")
                .arg(StudioData::getMixcutVoice(voiceId).name,
                     StudioData::getMixcutVoice(voiceId).label));
        return;
    }
    if (m_project.analysisReady && !m_project.script.trimmed().isEmpty()) {
        startVoiceJob();
    } else {
        refreshVoicePanel();
    }
    refreshPreview();
}

void MixcutPage::chooseRatio(const QString &ratio)
{
    if (ratio == m_project.ratio) {
        return;
    }
    m_project.ratio = ratio;
    m_playTimer->stop();
    m_playing = false;
    m_playProgress = 0;
    rebuildStudio();
}

/* ------------------------------------------------------------------ */
/* 分析、配音与预览                                                     */
/* ------------------------------------------------------------------ */

void MixcutPage::startAnalysis(qint64 durationMs, const QString &announce)
{
    if (!m_project.hasSource) {
        return;
    }
    m_analysisTimer->stop();
    m_voiceTimer->stop();
    m_voiceDebounce->stop();
    m_playTimer->stop();
    m_playing = false;
    m_playProgress = 0;
    m_analysisProgress = 0;
    m_voiceProgress = 0;
    m_analysisDuration = durationMs;
    m_analysisStartedAt = QDateTime::currentMSecsSinceEpoch();
    m_project.analysisReady = false;
    m_project.voiceStatus = StudioData::VoiceStatus::Idle;
    m_analysisTimer->start();
    refreshStudioState();
    if (!announce.isEmpty()) {
        emit toastRequested(announce);
    }
}

void MixcutPage::tickAnalysis()
{
    const qint64 elapsed = QDateTime::currentMSecsSinceEpoch() - m_analysisStartedAt;
    m_analysisProgress = qBound(
        0, static_cast<int>(qMin<qint64>(100, elapsed * 100 / m_analysisDuration)), 100);
    refreshAnalysisProgress();
    if (m_analysisProgress < 100) {
        return;
    }

    m_analysisTimer->stop();
    m_project.script = StudioData::generateMixcutScriptFromPrompt(
        m_project.prompt, m_project.style, m_scriptVariant);
    m_project.analysisReady = true;
    m_scriptEdited = false;
    m_project.voiceStatus = StudioData::VoiceStatus::Idle;
    m_voiceProgress = 0;
    if (m_scriptEdit) {
        m_settingScriptText = true;
        m_scriptEdit->setPlainText(m_project.script);
        m_settingScriptText = false;
    }
    refreshStudioState();
    startVoiceJob();
}

void MixcutPage::startVoiceJob()
{
    m_voiceTimer->stop();
    m_voiceDebounce->stop();
    if (!m_project.hasSource || !m_project.analysisReady
        || m_project.script.trimmed().isEmpty()) {
        m_project.voiceStatus = StudioData::VoiceStatus::Idle;
        m_voiceProgress = 0;
        refreshVoicePanel();
        return;
    }
    m_project.voiceStatus = StudioData::VoiceStatus::Generating;
    m_voiceProgress = 0;
    m_voiceTimer->start();
    refreshVoicePanel();
    refreshStudioState();
}

void MixcutPage::tickVoice()
{
    if (m_project.voiceStatus == StudioData::VoiceStatus::Ready) {
        m_voiceTimer->stop();
        return;
    }
    m_voiceProgress = qMin(100, m_voiceProgress + 4);
    if (m_voiceProgress >= 100) {
        m_voiceTimer->stop();
        m_project.voiceStatus = StudioData::VoiceStatus::Ready;
        refreshStudioState();
        return;
    }
    refreshVoicePanel();
}

void MixcutPage::scheduleVoiceSync()
{
    m_voiceDebounce->stop();
    m_voiceTimer->stop();
    m_voiceProgress = 0;
    if (!m_project.script.trimmed().isEmpty() && m_project.analysisReady) {
        m_project.voiceStatus = StudioData::VoiceStatus::Outdated;
        m_voiceDebounce->start();
    } else {
        m_project.voiceStatus = StudioData::VoiceStatus::Idle;
    }
    refreshVoicePanel();
}

void MixcutPage::refreshStudioState()
{
    if (!m_studioScreen) {
        return;
    }

    const int step = activeStep();
    if (m_stepRail) {
        m_stepRail->setActiveStep(step);
    }
    if (m_studioStepLabel) {
        m_studioStepLabel->setText(
            step == 1 ? QStringLiteral("上传视频")
                      : (step == 2 ? QStringLiteral("文案与配音")
                                   : QStringLiteral("一键成片")));
    }
    if (!m_project.hasSource) {
        if (m_studioDescription) {
            m_studioDescription->setText(
                QStringLiteral("上传一段视频，AI 会看片写文案、生成配音，并自动完成混剪包装。"));
        }
        if (m_footerSummary) {
            m_footerSummary->setText(
                QStringLiteral("上传视频后，AI 会自动撰写文案并生成配音"));
        }
        if (m_footerHint) {
            m_footerHint->setText(QStringLiteral("字幕、音效与 BGM 无需手动设置"));
        }
        return;
    }
    const QStringList stepLabels = {QStringLiteral("上传视频"),
                                    QStringLiteral("文案与配音"),
                                    QStringLiteral("一键成片")};
    if (m_stepRail) {
        m_stepRail->setActiveStep(step);
    }
    if (m_studioStepLabel) {
        m_studioStepLabel->setText(stepLabels.value(step - 1));
    }
    if (m_studioDescription) {
        if (!m_project.analysisReady) {
            m_studioDescription->setText(
                m_project.prompt.trimmed().isEmpty()
                    ? (m_project.script.isEmpty()
                           ? QStringLiteral("AI 正在理解画面并撰写口播文案，完成后自动生成配音。")
                           : QStringLiteral("AI 正在重写文案，完成后会自动重新生成配音。"))
                    : QStringLiteral("AI 正在按提示词重写文案，完成后会自动重新生成配音。"));
        } else if (m_project.voiceStatus == StudioData::VoiceStatus::Generating
                   || m_project.voiceStatus == StudioData::VoiceStatus::Outdated) {
            m_studioDescription->setText(
                QStringLiteral("文案已就绪，AI 正在合成配音，完成后即可一键成片。"));
        } else {
            m_studioDescription->setText(
                QStringLiteral("文案与配音已就绪，可以试听、微调，或直接一键成片。"));
        }
    }

    const bool analyzing = !m_project.analysisReady;
    if (m_styleControl) {
        m_styleControl->setEnabled(!analyzing);
    }
    if (m_ratioControl) {
        m_ratioControl->setEnabled(!analyzing);
    }
    if (m_promptEdit) {
        m_promptEdit->setReadOnly(analyzing);
        m_promptEdit->setEnabled(!analyzing);
    }
    if (m_scriptEdit) {
        m_scriptEdit->setReadOnly(analyzing);
        m_scriptEdit->setEnabled(!analyzing);
    }
    if (m_promptEdit && m_promptEdit->toPlainText() != m_project.prompt) {
        m_settingScriptText = true;
        m_promptEdit->setPlainText(m_project.prompt);
        m_settingScriptText = false;
    }
    if (m_scriptEdit && m_scriptEdit->toPlainText() != m_project.script) {
        m_settingScriptText = true;
        m_scriptEdit->setPlainText(m_project.script);
        m_settingScriptText = false;
    }
    if (m_regenerateButton) {
        m_regenerateButton->setEnabled(!analyzing && m_project.hasSource);
    }

    if (m_previewAnalysisOverlay) {
        m_previewAnalysisOverlay->setVisible(analyzing);
    }
    if (m_previewPlayButton) {
        m_previewPlayButton->setEnabled(!analyzing);
        setPlayIcon(m_previewPlayButton, m_playing);
    }
    if (m_previewAnalysisLabel) {
        m_previewAnalysisLabel->setText(
            m_project.prompt.trimmed().isEmpty()
                ? (m_project.script.isEmpty()
                       ? QStringLiteral("AI 正在看片并写文案")
                       : QStringLiteral("AI 正在重写文案"))
                : QStringLiteral("AI 正在按提示词写文案"));
    }
    if (m_previewAnalysisProgress) {
        m_previewAnalysisProgress->setText(
            QStringLiteral("%1%").arg(m_analysisProgress));
    }
    if (m_previewAnalysisBar) {
        m_previewAnalysisBar->setValue(m_analysisProgress);
    }

    if (m_scriptOverlay) {
        m_scriptOverlay->setVisible(analyzing);
    }
    if (m_scriptOverlayLabel) {
        m_scriptOverlayLabel->setText(
            m_project.prompt.trimmed().isEmpty()
                ? (m_project.script.isEmpty()
                       ? QStringLiteral("AI 正在看片并写文案")
                       : QStringLiteral("AI 正在重写文案"))
                : QStringLiteral("AI 正在按提示词生成文案"));
    }
    if (m_scriptOverlayDetail) {
        m_scriptOverlayDetail->setText(
            analyzing ? QStringLiteral("已识别 %1 个镜头，完成后自动生成配音")
                            .arg(m_project.source.shots)
                      : QString());
    }
    if (m_scriptSkeleton) {
        const bool skeleton = analyzing && m_project.script.trimmed().isEmpty();
        m_scriptSkeleton->setVisible(skeleton);
    }
    if (m_scriptOverlayProgress) {
        m_scriptOverlayProgress->setText(QStringLiteral("%1%").arg(m_analysisProgress));
    }
    if (m_scriptOverlayBar) {
        m_scriptOverlayBar->setValue(m_analysisProgress);
    }

    refreshScriptStats();
    refreshVoicePanel();
    refreshPreview();

    if (m_startButton) {
        const bool syncing =
            m_project.voiceStatus == StudioData::VoiceStatus::Generating
            || m_project.voiceStatus == StudioData::VoiceStatus::Outdated;
        if (analyzing) {
            m_startButton->setText(QStringLiteral("AI 正在写文案…"));
            m_startButton->setLeadingGlyph(Icons::Glyph::Sparkle);
        } else if (syncing) {
            m_startButton->setText(QStringLiteral("正在生成配音…"));
            m_startButton->setLeadingGlyph(Icons::Glyph::Sparkle);
        } else {
            m_startButton->setText(QStringLiteral("一键成片"));
            m_startButton->setLeadingGlyph(Icons::Glyph::Sparkle);
        }
        m_startButton->setEnabled(canStart());
        m_startButton->setToolTip(
            canStart()
                ? QStringLiteral("开始 AI 混剪，自动生成成片与剪映草稿")
                : (analyzing ? QStringLiteral("AI 正在撰写文案")
                             : (syncing ? QStringLiteral("配音生成中")
                                        : QStringLiteral("请先补全文案"))));
    }

    if (m_footerSummary) {
        const StudioData::MixcutScriptStats stats =
            StudioData::mixcutScriptStats(m_project.script);
        const StudioData::MixcutVoice voice =
            StudioData::getMixcutVoice(m_project.voiceId);
        const bool syncing =
            m_project.voiceStatus == StudioData::VoiceStatus::Generating
            || m_project.voiceStatus == StudioData::VoiceStatus::Outdated;
        QString summary;
        if (analyzing) {
            summary = m_project.prompt.trimmed().isEmpty()
                ? (m_project.script.isEmpty()
                       ? QStringLiteral("AI 正在看片并撰写文案…")
                       : QStringLiteral("AI 正在重写文案…"))
                : QStringLiteral("AI 正在按提示词重写文案…");
        } else if (syncing) {
            summary = QStringLiteral("正在合成「%1 · %2」配音…")
                          .arg(voice.name, voice.label);
        } else if (stats.chars == 0) {
            summary = QStringLiteral("文案为空，请先输入口播文案");
        } else if (canStart()) {
            summary = QStringLiteral("预计成片 %1 · %2 个镜头 · %3 · 成片 + 剪映草稿")
                          .arg(stats.durationLabel)
                          .arg(m_project.source.shots)
                          .arg(m_project.ratio);
        } else {
            summary = QStringLiteral("完成文案与配音后即可一键成片");
        }
        m_footerSummary->setText(summary);
    }
    if (m_footerHint) {
        m_footerHint->setText(
            analyzing
                ? QStringLiteral("已识别 %1 个镜头，文案完成后自动配音")
                      .arg(m_project.source.shots)
                : (m_project.voiceStatus == StudioData::VoiceStatus::Generating
                       ? QStringLiteral("配音完成后即可成片，无需其他设置")
                       : (StudioData::mixcutScriptStats(m_project.script).chars == 0
                              ? QStringLiteral("也可以点击「换一版」让 AI 重新撰写")
                              : (canStart()
                                     ? QStringLiteral("字幕、音效与 BGM 将自动配置，可先试听配音")
                                     : QStringLiteral("请稍候，正在准备成片")))));
    }
}

void MixcutPage::refreshAnalysisProgress()
{
    if (m_previewAnalysisProgress) {
        m_previewAnalysisProgress->setText(
            QStringLiteral("%1%").arg(m_analysisProgress));
    }
    if (m_previewAnalysisBar) {
        m_previewAnalysisBar->setValue(m_analysisProgress);
    }
    if (m_scriptOverlayProgress) {
        m_scriptOverlayProgress->setText(QStringLiteral("%1%").arg(m_analysisProgress));
    }
    if (m_scriptOverlayBar) {
        m_scriptOverlayBar->setValue(m_analysisProgress);
    }
    if (m_scriptMeta) {
        m_scriptMeta->setText(m_project.script.trimmed().isEmpty()
                                  ? QStringLiteral("正在根据画面生成…")
                                  : QStringLiteral("正在重写…"));
        m_scriptMeta->setVisible(true);
    }
}

void MixcutPage::refreshScriptStats()
{
    const StudioData::MixcutScriptStats stats =
        StudioData::mixcutScriptStats(m_project.script);
    if (m_scriptMeta) {
        if (!m_project.analysisReady) {
            m_scriptMeta->setText(m_project.script.trimmed().isEmpty()
                                      ? QStringLiteral("正在根据画面生成…")
                                      : QStringLiteral("正在重写…"));
        } else {
            m_scriptMeta->setText(QStringLiteral("%1 字 · %2 句")
                                      .arg(stats.chars)
                                      .arg(stats.sentences));
        }
        m_scriptMeta->setVisible(true);
    }
    if (m_scriptStats) {
        if (stats.chars > 0) {
            m_scriptStats->setText(
                QStringLiteral("%1 字 · %2 句 · 预计口播 %3 · AI 按句切字幕")
                    .arg(stats.chars)
                    .arg(stats.sentences)
                    .arg(stats.durationLabel));
        } else {
            m_scriptStats->setText(
                QStringLiteral("文案生成后，AI 自动按句切字幕并合成配音"));
        }
    }
    if (m_generateButton) {
        m_generateButton->setEnabled(m_project.analysisReady
                                     && !m_project.prompt.trimmed().isEmpty());
    }
}

QString MixcutPage::activeCaption() const
{
    const StudioData::MixcutScriptStats stats =
        StudioData::mixcutScriptStats(m_project.script);
    if (stats.lines.isEmpty()) {
        return StudioData::mixcutStyleMeta(m_project.style).captionTitle;
    }
    if (!m_playing && m_playProgress <= 0) {
        return stats.lines.first();
    }
    const int index = qBound(
        0, static_cast<int>(std::floor((m_playProgress / 100.0) * stats.lines.size())),
        stats.lines.size() - 1);
    return stats.lines.at(index);
}

void MixcutPage::refreshPreview()
{
    if (!m_previewPoster || !m_project.hasSource) {
        return;
    }
    const StudioData::MixcutStyleMeta &meta =
        StudioData::mixcutStyleMeta(m_project.style);
    const StudioData::MixcutVoice voice =
        StudioData::getMixcutVoice(m_project.voiceId);
    const StudioData::MixcutScriptStats stats =
        StudioData::mixcutScriptStats(m_project.script);
    const bool analyzing = !m_project.analysisReady;
    const bool voiceReady =
        m_project.voiceStatus == StudioData::VoiceStatus::Ready;

    Studio::MixPosterSpec spec = m_previewPoster->spec();
    spec.scene = m_project.source.scenes.value(0);
    spec.title = meta.captionTitle;
    QString caption = activeCaption();
    if (analyzing && m_project.script.trimmed().isEmpty()) {
        caption = QStringLiteral("AI 正在理解画面并撰写口播文案…");
    } else if (analyzing) {
        caption = QStringLiteral("AI 正在重写一版更合适的文案…");
    }
    spec.caption = caption;
    spec.highlight.clear();
    for (const QString &keyword : meta.keywords) {
        if (caption.contains(keyword)) {
            spec.highlight = keyword;
            break;
        }
    }
    spec.voiceName = voiceReady
        ? QStringLiteral("%1 · %2").arg(voice.name, voice.label)
        : QString();
    spec.secondaryScenes = m_project.source.scenes.mid(1, 2);
    spec.ratio = m_project.ratio;
    spec.progress = m_playProgress;
    spec.timecode = (m_playing || m_playProgress > 0)
        ? StudioData::formatElapsed(
              stats.seconds * m_playProgress * 10LL)
        : (stats.chars > 0 ? stats.durationLabel : QStringLiteral("—"));
    spec.dimmed = analyzing;
    m_previewPoster->setSpec(spec);

    if (m_previewPlayButton) {
        m_previewPlayButton->setVisible(!analyzing);
        setPlayIcon(m_previewPlayButton, m_playing);
    }
    if (m_playBar) {
        m_playBar->setValue(m_playProgress);
    }
    if (m_playClock) {
        m_playClock->setText(
            StudioData::formatElapsed(stats.seconds * m_playProgress * 10LL));
    }
    if (m_playDuration) {
        m_playDuration->setText(stats.chars > 0 ? stats.durationLabel
                                                : QStringLiteral("--:--"));
    }
    if (m_previewStatusLabel) {
        m_previewStatusLabel->setText(
            voiceReady ? QStringLiteral("配音就绪")
                       : (analyzing ? QStringLiteral("文案生成中")
                                    : QStringLiteral("配音生成中")));
    }
    if (m_previewMeta) {
        m_previewMeta->setText(
            analyzing ? QStringLiteral("文案与配音完成后可试听")
                      : (voiceReady ? QStringLiteral("可试听")
                                    : QStringLiteral("配音生成中")));
        m_previewMeta->setVisible(true);
    }
}

void MixcutPage::refreshVoicePanel()
{
    const StudioData::MixcutVoice voice =
        StudioData::getMixcutVoice(m_project.voiceId);
    const StudioData::MixcutScriptStats stats =
        StudioData::mixcutScriptStats(m_project.script);
    const bool ready = m_project.voiceStatus == StudioData::VoiceStatus::Ready;
    const bool syncing =
        m_project.voiceStatus == StudioData::VoiceStatus::Generating
        || m_project.voiceStatus == StudioData::VoiceStatus::Outdated;
    const bool analyzing = !m_project.analysisReady;

    for (VoiceChoiceButton *button : m_voiceCards) {
        if (button) {
            button->setSelected(button->voiceId() == voice.id);
        }
    }
    if (m_voiceMeta) {
        m_voiceMeta->setText(
            ready ? QStringLiteral("%1 · %2").arg(voice.label, stats.durationLabel)
                  : (syncing ? QStringLiteral("生成中…") : QStringLiteral("自动生成")));
    }
    if (m_voiceDurationLabel) {
        m_voiceDurationLabel->setText(
            ready ? stats.durationLabel : QStringLiteral("—"));
    }
    if (m_voiceWave) {
        m_voiceWave->setColor(syncing ? Theme::Ai
                                      : (ready ? voice.accent : Theme::Text3));
        m_voiceWave->setProgress(ready ? m_playProgress : -1);
        m_voiceWave->setAnimated(syncing);
    }
    if (m_voicePlay) {
        m_voicePlay->setEnabled(ready);
        m_voicePlay->setGlyph(m_playing ? Icons::Glyph::Pause : Icons::Glyph::Play);
    }
    if (m_voiceStatus) {
        QString status;
        if (ready) {
            status = m_playing ? QStringLiteral("正在试听配音，点击暂停")
                               : QStringLiteral("配音已生成，点击播放试听");
        } else if (syncing) {
            status = m_project.voiceStatus == StudioData::VoiceStatus::Outdated
                ? QStringLiteral("文案已修改，正在自动同步配音…")
                : QStringLiteral("正在合成配音… %1%").arg(m_voiceProgress);
        } else if (analyzing) {
            status = QStringLiteral("AI 写完文案后自动生成配音");
        } else if (stats.chars > 0) {
            status = QStringLiteral("选择音色后自动生成配音");
        } else {
            status = QStringLiteral("输入文案后自动生成配音");
        }
        m_voiceStatus->setText(status);
        m_voiceStatus->setStyleSheet(
            QStringLiteral("color:%1;background:transparent;")
                .arg((syncing ? Theme::Ai : Theme::Text3).name()));
    }
    if (m_voiceStatusDot) {
        m_voiceStatusDot->setStyleSheet(
            QStringLiteral("background:%1;border-radius:2px;")
                .arg((syncing ? Theme::Ai
                              : (ready ? Theme::Success : Theme::BorderStrong))
                         .name()));
    }
    if (m_voiceNameLabel) {
        m_voiceNameLabel->setText(
            QStringLiteral("%1 · %2").arg(voice.name, voice.label));
    }
    if (m_voiceRegenerate) {
        m_voiceRegenerate->setEnabled(ready);
    }
    if (m_voiceProgressBar) {
        m_voiceProgressBar->setVisible(syncing);
        m_voiceProgressBar->setValue(m_voiceProgress);
    }
}

void MixcutPage::updateScriptFromEditor()
{
    if (m_settingScriptText || !m_scriptEdit) {
        return;
    }
    if (!m_project.analysisReady) {
        return;
    }
    m_project.script = m_scriptEdit->toPlainText();
    m_scriptEdited = true;
    m_project.voiceStatus = m_project.script.trimmed().isEmpty()
        ? StudioData::VoiceStatus::Idle
        : StudioData::VoiceStatus::Outdated;
    m_playTimer->stop();
    m_playing = false;
    m_playProgress = 0;
    refreshScriptStats();
    scheduleVoiceSync();
    refreshPreview();
}

void MixcutPage::updatePromptFromEditor()
{
    if (m_settingScriptText || !m_promptEdit) {
        return;
    }
    m_project.prompt = m_promptEdit->toPlainText();
    refreshScriptStats();
}

void MixcutPage::regenerateScript()
{
    if (!m_project.analysisReady) {
        return;
    }
    if (m_scriptEdited) {
        const auto answer = QMessageBox::question(
            this, QStringLiteral("换一版"),
            QStringLiteral("换一版会覆盖你手动修改的文案，是否继续？"),
            QMessageBox::Yes | QMessageBox::No, QMessageBox::No);
        if (answer != QMessageBox::Yes) {
            return;
        }
    }
    m_scriptVariant += 1;
    startAnalysis(1400, QString());
}

void MixcutPage::generateFromPrompt()
{
    if (!m_project.analysisReady || m_project.prompt.trimmed().isEmpty()) {
        return;
    }
    if (m_scriptEdited) {
        const auto answer = QMessageBox::question(
            this, QStringLiteral("生成文案"),
            QStringLiteral("按提示词生成会覆盖你手动修改的文案，是否继续？"),
            QMessageBox::Yes | QMessageBox::No, QMessageBox::No);
        if (answer != QMessageBox::Yes) {
            return;
        }
    }
    m_scriptVariant += 1;
    startAnalysis(1400, QStringLiteral("正在按提示词生成文案"));
}

void MixcutPage::regenerateVoice()
{
    if (m_project.voiceStatus != StudioData::VoiceStatus::Ready) {
        return;
    }
    startVoiceJob();
}

/* ------------------------------------------------------------------ */
/* 播放                                                                 */
/* ------------------------------------------------------------------ */

void MixcutPage::togglePlayback()
{
    if (m_screen == Screen::Result) {
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
        if (m_resultCenterPlay) {
            setPlayIcon(m_resultCenterPlay, m_playing);
        }
        if (m_resultPlayButton) {
            m_resultPlayButton->setText(m_playing ? QStringLiteral("暂停播放")
                                                  : QStringLiteral("播放成片"));
            m_resultPlayButton->setLeadingGlyph(m_playing ? Icons::Glyph::Pause
                                                          : Icons::Glyph::Play);
        }
        return;
    }
    if (!m_project.hasSource) {
        return;
    }
    if (m_project.voiceStatus != StudioData::VoiceStatus::Ready) {
        emit toastRequested(
            !m_project.analysisReady
                ? QStringLiteral("AI 正在写文案，文案完成后会自动生成配音")
                : QStringLiteral("配音生成中，完成后即可试听"));
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
    refreshPreview();
    refreshVoicePanel();
    if (m_resultPlayButton) {
        m_resultPlayButton->setText(m_playing ? QStringLiteral("暂停播放")
                                              : QStringLiteral("播放成片"));
        m_resultPlayButton->setLeadingGlyph(m_playing ? Icons::Glyph::Pause
                                                      : Icons::Glyph::Play);
    }
}

void MixcutPage::updatePlayback()
{
    if (m_playing) {
        m_playProgress = qMin(100, m_playProgress + 2);
        if (m_playProgress >= 100) {
            m_playing = false;
            m_playTimer->stop();
        }
    }
    if (m_screen == Screen::Result) {
        if (m_resultPoster) {
            m_resultPoster->setProgress(m_playProgress);
        }
        if (m_resultPlayBar) {
            m_resultPlayBar->setValue(m_playProgress);
        }
        if (m_resultPlayClock && m_hasResult) {
            const int total = StudioData::parseElapsed(m_result.durationLabel);
            m_resultPlayClock->setText(StudioData::formatElapsed(
                static_cast<qint64>(total) * m_playProgress * 10LL));
        }
        if (m_resultCenterPlay) {
            setPlayIcon(m_resultCenterPlay, m_playing);
        }
        if (m_resultPoster && m_hasResult) {
            const QStringList lines = StudioData::mixcutScriptLines(m_result.script);
            const int index = lines.isEmpty()
                ? 0
                : qBound(0,
                         static_cast<int>(std::floor(
                             (m_playProgress / 100.0) * lines.size())),
                         lines.size() - 1);
            const StudioData::MixcutStyleMeta &meta =
                StudioData::mixcutStyleMeta(m_result.style);
            QString caption = lines.value(index, m_result.captionTitle);
            QString highlight;
            for (const QString &keyword : meta.keywords) {
                if (caption.contains(keyword)) {
                    highlight = keyword;
                    break;
                }
            }
            const int total = StudioData::parseElapsed(m_result.durationLabel);
            const QString clock = StudioData::formatElapsed(
                static_cast<qint64>(total) * m_playProgress * 10LL);
            Studio::MixPosterSpec spec = m_resultPoster->spec();
            spec.caption = caption;
            spec.highlight = highlight;
            spec.timecode = (m_playing || m_playProgress > 0)
                ? clock
                : m_result.durationLabel;
            spec.progress = m_playProgress;
            m_resultPoster->setSpec(spec);
        }
    } else if (m_project.hasSource) {
        refreshPreview();
        refreshVoicePanel();
    }
    if (m_resultPlayButton) {
        m_resultPlayButton->setText(m_playing ? QStringLiteral("暂停播放")
                                              : QStringLiteral("播放成片"));
        m_resultPlayButton->setLeadingGlyph(m_playing ? Icons::Glyph::Pause
                                                      : Icons::Glyph::Play);
    }
    if (m_previewPlayButton) {
        setPlayIcon(m_previewPlayButton, m_playing);
    }
    if (m_previewRowPlay) {
        m_previewRowPlay->setGlyph(m_playing ? Icons::Glyph::Pause
                                             : Icons::Glyph::Play);
    }
}

/* ------------------------------------------------------------------ */
/* 提示词放大编辑                                                       */
/* ------------------------------------------------------------------ */

void MixcutPage::openPromptEditor()
{
    auto *host = window();
    if (!host) {
        return;
    }

    auto *overlay = new ModalOverlay(host);
    auto *card = new ModalCard(QStringLiteral("编辑文案提示词"), 720);
    auto *hintRow = new QWidget(card);
    auto *hintLayout = new QHBoxLayout(hintRow);
    hintLayout->setContentsMargins(0, 0, 0, 0);
    hintLayout->setSpacing(8);
    auto *hint = Ui::label(
        QStringLiteral("输入文案标题或粘贴完整原始文案，保存后可在工作台点击「生成文案」按提示词重写。"),
        10, QFont::Normal, Theme::Text3, hintRow);
    hint->setWordWrap(true);
    hintLayout->addWidget(hint, 1);
    auto *countLabel = monoLabel(QString(), 10, Theme::Text3, hintRow);
    hintLayout->addWidget(countLabel, 0, Qt::AlignTop);
    card->contentLayout()->addWidget(hintRow);

    auto *editor = new QTextEdit(card);
    Studio::applyTextAreaStyle(editor, false, 12);
    editor->setAcceptRichText(false);
    editor->setMinimumHeight(300);
    editor->setPlainText(m_project.prompt);
    editor->setPlaceholderText(QStringLiteral(
        "输入文案标题，或粘贴完整原始文案，例如：夏天通勤防晒怎么选；也可以直接粘贴一段产品原始文案。"));
    card->contentLayout()->addWidget(editor);
    auto *footerHint = Ui::label(
        QStringLiteral("⌘/Ctrl + Enter 保存 · Esc 取消            提示词会随项目保存，可反复编辑"),
        9, QFont::Normal, Theme::Text3, card);
    card->contentLayout()->addWidget(footerHint);

    auto updateCount = [countLabel, editor] {
        countLabel->setText(
            QStringLiteral("%1 字").arg(editor->toPlainText().trimmed().size()));
    };
    updateCount();
    connect(editor, &QTextEdit::textChanged, card, updateCount);

    auto *cancel = new Button(QStringLiteral("取消"), Button::Default, card);
    auto *save = new Button(QStringLiteral("保存"), Button::Primary, card);
    connect(cancel, &QAbstractButton::clicked, overlay, &ModalOverlay::closeModal);
    auto savePrompt = [this, overlay, editor] {
        m_project.prompt = editor->toPlainText().trimmed();
        if (m_promptEdit) {
            m_settingScriptText = true;
            m_promptEdit->setPlainText(m_project.prompt);
            m_settingScriptText = false;
        }
        refreshScriptStats();
        overlay->closeModal();
    };
    connect(save, &QAbstractButton::clicked, save, savePrompt);
    auto *shortcut = new QShortcut(QKeySequence(Qt::CTRL | Qt::Key_Return), card);
    shortcut->setContext(Qt::WidgetWithChildrenShortcut);
    connect(shortcut, &QShortcut::activated, save, savePrompt);
    card->addFooterButton(cancel);
    card->addFooterButton(save);
    connect(overlay, &ModalOverlay::closed, overlay, &QObject::deleteLater);
    overlay->showModal(card);
    editor->setFocus();
}

/* ------------------------------------------------------------------ */
/* 生成过程                                                             */
/* ------------------------------------------------------------------ */

void MixcutPage::startGeneration()
{
    if (!canStart()) {
        return;
    }
    m_analysisTimer->stop();
    m_voiceTimer->stop();
    m_voiceDebounce->stop();
    m_playTimer->stop();
    m_playing = false;
    m_playProgress = 0;

    buildProcessingScreen();
    m_runId += 1;
    m_runStartedAt = QDateTime::currentMSecsSinceEpoch();
    m_run = StudioData::computeMixcutRun(m_project, 0, m_runId);
    m_hasRun = true;
    m_logCount = -1;
    m_lastScenesCount = -1;
    updateProcessing();
    showProcessing();
    m_runTimer->start();
    emit taskStatusChanged(QStringLiteral("智能混剪中 0% · 00:00"), true);
}

void MixcutPage::buildProcessingScreen()
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
    m_processingScenesLayout = nullptr;
    m_processingScenesCount = nullptr;
    m_logCount = -1;
    m_lastScenesCount = -1;

    const StudioData::MixcutStyleMeta &meta =
        StudioData::mixcutStyleMeta(m_project.style);
    const StudioData::MixcutVoice voice =
        StudioData::getMixcutVoice(m_project.voiceId);

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
    titleLine->addWidget(Ui::label(QStringLiteral("AI 正在混剪你的视频"), 16,
                                     QFont::DemiBold, Theme::Text1, top));
    titleLine->addWidget(new Chip(QStringLiteral("智能混剪中"), Chip::Run, top));
    titleLine->addWidget(monoLabel(
        QStringLiteral("%1 · 成片 + 剪映草稿").arg(m_project.ratio), 10,
        Theme::Text3, top));
    titleLine->addStretch(1);
    titleTexts->addLayout(titleLine);
    auto *metaLine = new QHBoxLayout;
    metaLine->setContentsMargins(0, 0, 0, 0);
    metaLine->setSpacing(8);
    metaLine->addWidget(Ui::label(meta.projectName + QStringLiteral(" · 智能混剪"), 10,
                                    QFont::Normal, Theme::Text2, top));
    metaLine->addWidget(Ui::label(QStringLiteral("·"), 10, QFont::Normal,
                                    Theme::BorderStrong, top));
    metaLine->addWidget(Ui::label(
        QStringLiteral("音色：%1 · %2").arg(voice.name, voice.label), 10,
        QFont::Normal, Theme::Text3, top));
    metaLine->addWidget(Ui::label(QStringLiteral("·"), 10, QFont::Normal,
                                    Theme::BorderStrong, top));
    m_processingMeta = Ui::label(QStringLiteral("已用时 00:00"), 10, QFont::Normal,
                                 Theme::Text3, top);
    metaLine->addWidget(m_processingMeta);
    metaLine->addWidget(Ui::label(QStringLiteral("·"), 10, QFont::Normal,
                                    Theme::BorderStrong, top));
    m_etaLabel = Ui::label(QStringLiteral("预计还需 22 秒"), 10, QFont::Normal,
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
    auto *background = new Button(QStringLiteral("后台运行"), Button::Default,
                                  progressHost);
    connect(background, &QAbstractButton::clicked, this, [this] {
        emit toastRequested(QStringLiteral("已转后台运行，完成后自动下载并通知你"));
        emit navigateRequested(QStringLiteral("dash"));
    });
    progressLayout->addWidget(background, 0, Qt::AlignVCenter);
    titleLayout->addWidget(progressHost, 0, Qt::AlignTop);
    topLayout->addWidget(titleRow);
    topLayout->addSpacing(12);

    auto *stages = new QWidget(top);
    auto *stagesLayout = new QHBoxLayout(stages);
    stagesLayout->setContentsMargins(0, 0, 0, 0);
    stagesLayout->setSpacing(8);
    for (int index = 0; index < 5; ++index) {
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
    const bool portrait = m_project.ratio == QLatin1String("9:16");
    const bool square = m_project.ratio == QLatin1String("1:1");
    int previewHeight = 380;
    if (m_project.ratio == QLatin1String("16:9")) {
        previewHeight = 206;
    } else if (square) {
        previewHeight = 300;
    }
    const int previewWidth = portrait ? qRound(previewHeight * 9.0 / 16.0)
        : (m_project.ratio == QLatin1String("16:9")
               ? qRound(previewHeight * 16.0 / 9.0)
               : previewHeight);
    m_processingPoster = new Studio::MixPosterWidget(stageHost);
    m_processingPoster->setFixedSize(previewWidth, previewHeight);
    Studio::MixPosterSpec spec;
    spec.scene = m_project.source.scenes.value(0);
    spec.title = meta.captionTitle;
    spec.caption = QStringLiteral("AI 正在拆解文案与画面节奏…");
    spec.voiceName = QStringLiteral("%1 · %2").arg(voice.name, voice.label);
    spec.secondaryScenes = m_project.source.scenes.mid(1, 2);
    spec.ratio = m_project.ratio;
    spec.timecode = QStringLiteral("00:00");
    spec.progress = 0;
    spec.badge = StudioData::mixcutStyleName(m_project.style);
    m_processingPoster->setSpec(spec);
    stageLayout->addWidget(m_processingPoster, 0, Qt::AlignCenter);
    stageHost->setMinimumHeight(previewHeight + 24);
    leftLayout->addWidget(stageHost, 1);

    auto *infoRow = new QWidget(left);
    auto *infoLayout = new QHBoxLayout(infoRow);
    infoLayout->setContentsMargins(0, 0, 0, 0);
    infoLayout->setSpacing(8);
    auto *voiceContent = new QWidget;
    auto *voiceContentLayout = new QVBoxLayout(voiceContent);
    voiceContentLayout->setContentsMargins(0, 0, 0, 0);
    voiceContentLayout->setSpacing(3);
    auto *voiceTop = new QHBoxLayout;
    voiceTop->setContentsMargins(0, 0, 0, 0);
    voiceTop->addStretch(1);
    m_processingVoiceLabel = monoLabel(QStringLiteral("0%"), 8, Theme::Text2,
                                       voiceContent);
    voiceTop->addWidget(m_processingVoiceLabel);
    voiceContentLayout->addLayout(voiceTop);
    auto *voiceWaveRow = new QHBoxLayout;
    voiceWaveRow->setContentsMargins(0, 0, 0, 0);
    voiceWaveRow->setSpacing(5);
    m_processingVoiceWave = new Studio::Waveform(voiceContent);
    m_processingVoiceWave->setBars(14);
    m_processingVoiceWave->setFixedHeight(14);
    m_processingVoiceWave->setColor(voice.accent);
    voiceWaveRow->addWidget(m_processingVoiceWave, 1);
    voiceWaveRow->addWidget(Ui::label(voice.label, 8, QFont::Normal, Theme::Text3,
                                      voiceContent));
    voiceContentLayout->addLayout(voiceWaveRow);
    infoLayout->addWidget(makeInfoPanel(QStringLiteral("配音对齐"), Icons::Glyph::Mic,
                                        voiceContent, infoRow),
                          1);

    auto *sceneContent = new QWidget;
    auto *sceneContentLayout = new QVBoxLayout(sceneContent);
    sceneContentLayout->setContentsMargins(0, 0, 0, 0);
    sceneContentLayout->setSpacing(3);
    auto *sceneTop = new QHBoxLayout;
    sceneTop->setContentsMargins(0, 0, 0, 0);
    sceneTop->addStretch(1);
    m_processingScenesCount = monoLabel(QStringLiteral("0/%1")
                                            .arg(m_project.source.shots),
                                        8, Theme::Text2, sceneContent);
    sceneTop->addWidget(m_processingScenesCount);
    sceneContentLayout->addLayout(sceneTop);
    auto *sceneThumbsHost = new QWidget(sceneContent);
    m_processingScenesLayout = new QHBoxLayout(sceneThumbsHost);
    m_processingScenesLayout->setContentsMargins(0, 0, 0, 0);
    m_processingScenesLayout->setSpacing(4);
    m_processingScenesLayout->addWidget(Ui::label(QStringLiteral("等待配音完成"), 8,
                                                   QFont::Normal, Theme::Text3,
                                                   sceneThumbsHost));
    m_processingScenesLayout->addStretch(1);
    sceneContentLayout->addWidget(sceneThumbsHost);
    infoLayout->addWidget(makeInfoPanel(QStringLiteral("镜头匹配"),
                                        Icons::Glyph::Image, sceneContent, infoRow),
                          1);
    leftLayout->addWidget(infoRow);

    auto *sourceRow = new QWidget(left);
    sourceRow->setObjectName(QStringLiteral("mixProcessingSource"));
    sourceRow->setStyleSheet(
        QStringLiteral("#mixProcessingSource { background:%1; border:1px solid %2;"
                       " border-radius:7px; }")
            .arg(Theme::Surface1.name(), Theme::Border.name()));
    auto *sourceLayout = new QHBoxLayout(sourceRow);
    sourceLayout->setContentsMargins(10, 7, 10, 7);
    sourceLayout->setSpacing(8);
    auto *wave = new Studio::Waveform(sourceRow);
    wave->setBars(18);
    wave->setColor(voice.accent);
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
    logPanel->setObjectName(QStringLiteral("mixLogPanel"));
    logPanel->setStyleSheet(
        QStringLiteral("#mixLogPanel { background:%1; border:1px solid %2;"
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
    m_logStageTitle = Ui::label(QStringLiteral("实时过程 · 解析文案"), 11,
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
    m_logStepsLabel = monoLabel(QStringLiteral("0/5 步"), 9, Theme::Text3,
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
    logProgressTop->addWidget(Ui::label(QStringLiteral("解析文案"), 10, QFont::Normal,
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
    statusRow->addWidget(Ui::label(QStringLiteral("可切到其他页面，完成后自动下载"),
                                    8, QFont::Normal, Theme::Text3, logFooter));
    logFooterLayout->addLayout(statusRow);
    logFooterLayout->addStretch(1);
    m_logFooter = monoLabel(QStringLiteral("字幕 0 · 音效 0"), 8, Theme::Text3,
                            logFooter);
    logFooterLayout->addWidget(m_logFooter);
    logLayout->addWidget(logFooter);
    bodyLayout->addWidget(logPanel, 1);
    root->addWidget(body, 1);
}

void MixcutPage::updateProcessing()
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
        Icons::Glyph::Doc, Icons::Glyph::Mic, Icons::Glyph::Image,
        Icons::Glyph::Wand, Icons::Glyph::Film,
    };
    const QStringList statusTexts = {QStringLiteral("等待"), QStringLiteral("进行中"),
                                     QStringLiteral("完成")};
    for (int index = 0; index < m_stageCards.size() && index < m_run.stages.size();
         ++index) {
        const StudioData::MixcutStageState &stage = m_run.stages.at(index);
        const int status = stage.status == StudioData::StageStatus::Done
            ? 2
            : (stage.status == StudioData::StageStatus::Running ? 1 : 0);
        m_stageCards.at(index)->setData(
            index + 1, glyphs.value(index, Icons::Glyph::Sparkle), stage.label,
            status, statusTexts.value(status), stage.detail,
            qRound(stage.progress), stage.status == StudioData::StageStatus::Running);
    }

    const StudioData::MixcutStyleMeta &meta =
        StudioData::mixcutStyleMeta(m_project.style);
    const StudioData::MixcutVoice voice =
        StudioData::getMixcutVoice(m_project.voiceId);
    if (m_processingPoster) {
        Studio::MixPosterSpec spec = m_processingPoster->spec();
        spec.caption = m_run.previewCaption;
        spec.highlight.clear();
        for (const QString &keyword : meta.keywords) {
            if (m_run.previewCaption.contains(keyword)) {
                spec.highlight = keyword;
                break;
            }
        }
        spec.voiceName = QStringLiteral("%1 · %2").arg(voice.name, voice.label);
        spec.timecode = StudioData::formatElapsed(m_run.elapsedMs);
        spec.progress = qRound(m_run.progress);
        m_processingPoster->setSpec(spec);
    }
    if (m_processingVoiceLabel) {
        m_processingVoiceLabel->setText(
            QStringLiteral("%1%").arg(qRound(m_run.voiceProgress)));
    }
    if (m_processingVoiceWave) {
        m_processingVoiceWave->setProgress(qRound(m_run.voiceProgress));
        m_processingVoiceWave->setAnimated(
            m_run.stage == StudioData::MixcutStage::Voice);
    }
    if (m_processingScenesCount) {
        m_processingScenesCount->setText(
            QStringLiteral("%1/%2").arg(m_run.matchedShots)
                .arg(m_project.source.shots));
    }
    const int sceneCount = qMin(
        m_project.source.scenes.size(),
        qMax(0, static_cast<int>(std::ceil(
                     m_run.matchedShots * 1.0 / qMax(1, m_project.source.shots) * 3))));
    if (m_processingScenesLayout && sceneCount != m_lastScenesCount) {
        m_lastScenesCount = sceneCount;
        Studio::clearLayout(m_processingScenesLayout);
        if (sceneCount == 0) {
            m_processingScenesLayout->addWidget(
                Ui::label(QStringLiteral("等待配音完成"), 8, QFont::Normal,
                          Theme::Text3, m_processingScenesLayout->parentWidget()));
        } else {
            for (const QString &scene : m_project.source.scenes.mid(0, sceneCount)) {
                auto *thumb = new Studio::MixSceneWidget(
                    m_processingScenesLayout->parentWidget());
                thumb->setScene(scene);
                thumb->setFixedSize(28, 20);
                m_processingScenesLayout->addWidget(thumb);
            }
            auto *eq = new QWidget(m_processingScenesLayout->parentWidget());
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
            m_processingScenesLayout->addStretch(1);
            m_processingScenesLayout->addWidget(eq, 0, Qt::AlignVCenter);
        }
    }
    if (m_processingSourceMeta) {
        const StudioData::MixcutScriptStats stats =
            StudioData::mixcutScriptStats(m_project.script);
        if (m_run.stage == StudioData::MixcutStage::Render) {
            m_processingSourceMeta->setText(
                QStringLiteral("%1/%2 帧")
                    .arg(StudioData::formatNumber(m_run.renderedFrames),
                         StudioData::formatNumber(m_run.totalFrames)));
        } else if (m_run.stage == StudioData::MixcutStage::Design) {
            m_processingSourceMeta->setText(
                QStringLiteral("%1 条字幕").arg(m_run.subtitleCount));
        } else {
            m_processingSourceMeta->setText(
                QStringLiteral("%1 句文案").arg(stats.sentences));
        }
    }

    const StudioData::MixcutStageState *current = nullptr;
    for (const StudioData::MixcutStageState &stage : m_run.stages) {
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
        for (const StudioData::MixcutStageState &stage : m_run.stages) {
            if (stage.status == StudioData::StageStatus::Done) {
                ++doneCount;
            }
        }
        m_logStepsLabel->setText(
            QStringLiteral("%1/%2 步").arg(doneCount).arg(m_run.stages.size()));
    }
    if (m_logFooter) {
        m_logFooter->setText(
            QStringLiteral("字幕 %1 · 音效 %2").arg(m_run.subtitleCount)
                .arg(m_run.sfxCount));
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
            for (const StudioData::MixcutLogEntry &log : m_run.logs) {
                const QString stageLabel = [&] {
                    for (const StudioData::MixcutStageState &stage : m_run.stages) {
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

void MixcutPage::finishGeneration()
{
    if (!m_hasRun) {
        return;
    }
    m_runTimer->stop();
    m_run = StudioData::computeMixcutRun(m_project, StudioData::mixcutRunTotalMs(m_project),
                                        m_runId);
    m_run.done = true;
    updateProcessing();
    m_hasRun = false;
    m_result = StudioData::makeMixcutResult(m_project,
                                            QDateTime::currentMSecsSinceEpoch());
    m_hasResult = true;
    showResult();
    emit taskStatusChanged(QString(), false);
    if (!isVisible()) {
        emit toastRequested(QStringLiteral("智能混剪成片已生成并自动下载，可预览查看"));
    }
}

/* ------------------------------------------------------------------ */
/* 结果                                                                 */
/* ------------------------------------------------------------------ */

void MixcutPage::rebuildResult()
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
    m_resultCenterPlay = nullptr;
    m_resultPlayBar = nullptr;

    if (!m_hasResult) {
        auto *empty = new QWidget(m_resultScreen);
        auto *emptyLayout = new QVBoxLayout(empty);
        emptyLayout->setContentsMargins(40, 40, 40, 40);
        emptyLayout->setSpacing(10);
        emptyLayout->setAlignment(Qt::AlignCenter);
        emptyLayout->addWidget(Studio::iconLabel(Icons::Glyph::Sparkle, 20,
                                                  Theme::Ai, empty),
                               0, Qt::AlignHCenter);
        emptyLayout->addWidget(Ui::label(QStringLiteral("还没有可查看的混剪成片"), 15,
                                          QFont::Medium, Theme::Text1, empty),
                               0, Qt::AlignHCenter);
        auto *hint = Ui::label(
            QStringLiteral("从智能混剪首页开始一个项目，成片会自动下载，并在这里预览、复制发布文案或打开剪映草稿。"),
            11, QFont::Normal, Theme::Text3, empty);
        hint->setWordWrap(true);
        hint->setAlignment(Qt::AlignCenter);
        hint->setMaximumWidth(380);
        emptyLayout->addWidget(hint, 0, Qt::AlignHCenter);
        auto *back = new Button(QStringLiteral("返回智能混剪"), Button::Primary, empty);
        connect(back, &QAbstractButton::clicked, this, &MixcutPage::showHub);
        emptyLayout->addWidget(back, 0, Qt::AlignHCenter);
        root->addWidget(empty, 1);
        return;
    }

    m_playing = false;
    m_playProgress = 0;
    m_playTimer->stop();

    const StudioData::MixcutVoice voice =
        StudioData::getMixcutVoice(m_result.voiceId);
    QString fileName = m_result.projectName;
    fileName.replace(QStringLiteral(" · "), QStringLiteral("_"));
    const QString movies = QStandardPaths::writableLocation(
        QStandardPaths::MoviesLocation);
    m_resultFilePath = QDir(movies).filePath(
        QStringLiteral("LiveMix/混剪成片/%1.mp4").arg(fileName));
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
    auto *back = new LinkLabel(QStringLiteral("智能混剪"), breadcrumb);
    back->setPixelSize(10);
    back->setTextColor(Theme::Text3);
    back->setHoverColor(Theme::Text1);
    back->setLeadingGlyph(Icons::Glyph::ChevronLeft, 10);
    back->setUnderlineOnHover(false);
    connect(back, &QAbstractButton::clicked, this, &MixcutPage::showHub);
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
    titleLine->addWidget(
        new Chip(QStringLiteral("已生成并自动导入剪映"), Chip::Ok, header));
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
    metaLine->addWidget(Ui::label(
        QStringLiteral("音色：%1 · %2").arg(voice.name, voice.label), 10,
        QFont::Normal, Theme::Text3, header));
    metaLine->addWidget(Ui::label(QStringLiteral("·"), 10, QFont::Normal,
                                    Theme::BorderStrong, header));
    metaLine->addWidget(Ui::label(
        QStringLiteral("%1 · %2").arg(
            m_result.ratio, StudioData::mixcutRatioDimensions(m_result.ratio)),
        10, QFont::Normal, Theme::Text3, header));
    metaLine->addWidget(Ui::label(QStringLiteral("·"), 10, QFont::Normal,
                                    Theme::BorderStrong, header));
    metaLine->addWidget(monoLabel(m_result.durationLabel, 10, Theme::Text3, header));
    metaLine->addStretch(1);
    titles->addLayout(metaLine);
    titleLayout->addLayout(titles, 1);

    auto *restart = new Button(QStringLiteral("重新混剪"), Button::Default, header);
    restart->setLeadingGlyph(Icons::Glyph::Reset);
    connect(restart, &QAbstractButton::clicked, this, &MixcutPage::restartFromResult);
    titleLayout->addWidget(restart);
    m_resultPlayButton = new Button(QStringLiteral("播放成片"), Button::Primary,
                                    header);
    m_resultPlayButton->setLeadingGlyph(Icons::Glyph::Play);
    connect(m_resultPlayButton, &QAbstractButton::clicked, this,
            &MixcutPage::togglePlayback);
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
    previewPanel->setObjectName(QStringLiteral("mixResultPreview"));
    previewPanel->setStyleSheet(
        QStringLiteral("#mixResultPreview { background:%1; border:1px solid %2;"
                       " border-radius:10px; }")
            .arg(QColor(QStringLiteral("#0c0e14")).name(), Theme::Border.name()));
    auto *previewLayout = new QVBoxLayout(previewPanel);
    previewLayout->setContentsMargins(16, 16, 16, 16);
    previewLayout->setSpacing(0);
    previewLayout->setAlignment(Qt::AlignHCenter);
    const bool portrait = m_result.ratio == QLatin1String("9:16");
    const bool square = m_result.ratio == QLatin1String("1:1");
    int posterHeight = portrait ? 494 : 202;
    if (square) {
        posterHeight = 360;
    }
    const int posterWidth = portrait ? qRound(posterHeight * 9.0 / 16.0)
        : (m_result.ratio == QLatin1String("16:9")
               ? qRound(posterHeight * 16.0 / 9.0)
               : posterHeight);
    auto *stack = new QWidget(previewPanel);
    stack->setFixedSize(posterWidth, posterHeight);
    auto *stackLayout = new QGridLayout(stack);
    stackLayout->setContentsMargins(0, 0, 0, 0);
    m_resultPoster = new Studio::MixPosterWidget(stack);
    m_resultPoster->setFixedSize(posterWidth, posterHeight);
    QString mainScene = QStringLiteral("street");
    if (m_result.style == StudioData::MixcutStyle::Review) {
        mainScene = QStringLiteral("serum");
    } else if (m_result.style == StudioData::MixcutStyle::Story) {
        mainScene = QStringLiteral("face");
    }
    QStringList secondary;
    for (const QString &scene : StudioData::mixcutDemoSource().scenes) {
        if (scene != mainScene && secondary.size() < 2) {
            secondary.append(scene);
        }
    }
    Studio::MixPosterSpec spec;
    spec.scene = mainScene;
    spec.title = m_result.captionTitle;
    spec.caption = StudioData::mixcutScriptLines(m_result.script).value(0);
    spec.voiceName = QStringLiteral("%1 · %2").arg(voice.name, voice.label);
    spec.secondaryScenes = secondary;
    spec.ratio = m_result.ratio;
    spec.timecode = m_result.durationLabel;
    spec.progress = 0;
    spec.badge = StudioData::mixcutStyleName(m_result.style);
    m_resultPoster->setSpec(spec);
    stackLayout->addWidget(m_resultPoster, 0, 0);
    m_resultCenterPlay = makePlayOverlay(stack);
    connect(m_resultCenterPlay, &QToolButton::clicked, this,
            &MixcutPage::togglePlayback);
    stackLayout->addWidget(m_resultCenterPlay, 0, 0, Qt::AlignCenter);
    previewLayout->addWidget(stack, 0, Qt::AlignHCenter);
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
    connect(play, &QAbstractButton::clicked, this, &MixcutPage::togglePlayback);
    playLayout->addWidget(play);
    auto *barHost = new QWidget(playRow);
    auto *barLayout = new QVBoxLayout(barHost);
    barLayout->setContentsMargins(0, 0, 0, 0);
    barLayout->setSpacing(2);
    m_resultPlayBar = new ProgressBar(barHost);
    m_resultPlayBar->setBarColor(Theme::Primary);
    m_resultPlayBar->setValue(0);
    m_resultPlayBar->setFixedHeight(4);
    barLayout->addWidget(m_resultPlayBar);
    auto *clockRow = new QHBoxLayout;
    clockRow->setContentsMargins(0, 0, 0, 0);
    m_resultPlayClock = monoLabel(QStringLiteral("00:00"), 8, Theme::Text3,
                                  barHost);
    clockRow->addWidget(m_resultPlayClock);
    clockRow->addStretch(1);
    clockRow->addWidget(monoLabel(m_result.durationLabel, 8, Theme::Text3,
                                  barHost));
    barLayout->addLayout(clockRow);
    playLayout->addWidget(barHost, 1);
    playLayout->addWidget(Studio::tagLabel(m_result.ratio,
                                            QColor(255, 255, 255, 191),
                                            QColor(0, 0, 0, 128), 8, playRow),
                          0, Qt::AlignVCenter);
    previewLayout->addWidget(playRow);
    previewLayout->addSpacing(12);

    auto *statsHost = new QWidget(previewPanel);
    auto *stats = new QGridLayout(statsHost);
    stats->setContentsMargins(0, 0, 0, 0);
    stats->setHorizontalSpacing(6);
    stats->setVerticalSpacing(6);
    stats->addWidget(new Studio::StatTile(QStringLiteral("文件大小"),
                                           m_result.fileSize, statsHost),
                     0, 0);
    stats->addWidget(new Studio::StatTile(
                         QStringLiteral("成片画幅"),
                         StudioData::mixcutRatioDimensions(m_result.ratio),
                         statsHost),
                     0, 1);
    stats->addWidget(new Studio::StatTile(
                         QStringLiteral("配音音色"),
                         QStringLiteral("%1 · %2").arg(voice.name, voice.label),
                         statsHost),
                     1, 0);
    stats->addWidget(new Studio::StatTile(QStringLiteral("BGM"), m_result.bgm,
                                           statsHost),
                     1, 1);
    previewLayout->addWidget(statsHost);
    leftLayout->addWidget(previewPanel);
    leftLayout->addWidget(new Studio::Note(
        QStringLiteral("成片已保存到本地输出目录：<br><span style=\"font-family:monospace;color:%1;\">%2</span>")
            .arg(Theme::Text2.name(), Studio::formatClipPath(m_resultFilePath, 64))));
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
        rowLayout->addWidget(
            makeCopyButton(copyLabel, [this, copy, copyLabel] { copyText(copy, copyLabel); },
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
        Icons::Glyph::Wand, QStringLiteral("混剪明细"),
        QStringLiteral("%1 项自动处理").arg(m_result.highlights.size()), right);
    details->setBodyMargins(QMargins(12, 12, 12, 12));
    auto *detailGrid = new QGridLayout;
    detailGrid->setContentsMargins(0, 0, 0, 0);
    detailGrid->setHorizontalSpacing(6);
    detailGrid->setVerticalSpacing(6);
    for (int index = 0; index < m_result.highlights.size(); ++index) {
        detailGrid->addWidget(
            new Studio::StatTile(m_result.highlights.at(index).first,
                                 m_result.highlights.at(index).second, details),
            index / 4, index % 4);
    }
    details->bodyLayout()->addLayout(detailGrid);
    details->bodyLayout()->addSpacing(8);
    auto *detailChips = new QWidget(details);
    auto *detailChipsLayout = new QHBoxLayout(detailChips);
    detailChipsLayout->setContentsMargins(0, 0, 0, 0);
    detailChipsLayout->setSpacing(6);
    QString compactScript = m_result.script;
    compactScript.remove(QRegularExpression(QStringLiteral("\\s")));
    detailChipsLayout->addWidget(new Chip(
        QStringLiteral("文案 · %1 字").arg(compactScript.size()), Chip::Neutral,
        detailChips));
    detailChipsLayout->addWidget(new Chip(
        QStringLiteral("字幕 · %1 条").arg(m_result.subtitleCount), Chip::Neutral,
        detailChips));
    detailChipsLayout->addWidget(new Chip(
        QStringLiteral("BGM · %1").arg(m_result.bgm), Chip::Neutral, detailChips));
    detailChipsLayout->addStretch(1);
    detailChipsLayout->addWidget(Ui::label(
        QStringLiteral("人声、画面、字幕与音效已自动对齐"), 8, QFont::Normal,
        Theme::Text3, detailChips));
    details->bodyLayout()->addWidget(detailChips);
    rightLayout->addWidget(details);

    auto *output = new Studio::SectionCard(
        Icons::Glyph::Download, QStringLiteral("输出文件"),
        QStringLiteral("已自动保存到本地"), right);
    output->setBodyMargins(QMargins(12, 12, 12, 12));
    auto *outputBody = output->bodyLayout();
    outputBody->setSpacing(8);
    auto *openFile = new Button(QStringLiteral("打开成片目录"), Button::Ghost, output);
    openFile->setCompact(true);
    openFile->setLeadingGlyph(Icons::Glyph::Folder);
    openFile->setTextColor(Theme::Success);
    connect(openFile, &QAbstractButton::clicked, this, [this] {
        emit toastRequested(QStringLiteral("已在访达中打开成片目录"));
    });
    outputBody->addWidget(makeFileRow(
        Icons::Glyph::Film, fileName + QStringLiteral(".mp4"), QStringLiteral("MP4"),
        QStringLiteral("%1 · %2 · H.264 · 30fps")
            .arg(StudioData::mixcutRatioDimensions(m_result.ratio),
                 m_result.fileSize),
        openFile, output));
    auto *openDraft = new Button(QStringLiteral("打开草稿"), Button::Ghost, output);
    openDraft->setCompact(true);
    openDraft->setLeadingGlyph(Icons::Glyph::Folder);
    openDraft->setTextColor(Theme::Success);
    connect(openDraft, &QAbstractButton::clicked, this, [this] {
        emit toastRequested(QStringLiteral("已在访达中打开剪映草稿文件夹"));
    });
    outputBody->addWidget(makeFileRow(
        Icons::Glyph::Scissors, fileName + QStringLiteral("_剪映草稿"),
        QStringLiteral("草稿"),
        QStringLiteral("已写入剪映草稿目录，可继续修改文案、配音与镜头"), openDraft,
        output));
    auto *pathRow = new QWidget(output);
    auto *pathLayout = new QHBoxLayout(pathRow);
    pathLayout->setContentsMargins(0, 2, 0, 0);
    pathLayout->setSpacing(5);
    pathLayout->addWidget(Studio::iconLabel(Icons::Glyph::Folder, 9, Theme::Text3,
                                             pathRow));
    auto *pathLabel = monoLabel(Studio::formatClipPath(m_resultDraftPath, 52), 8,
                                Theme::Text3, pathRow);
    pathLabel->setToolTip(m_resultDraftPath);
    pathLayout->addWidget(pathLabel, 1);
    pathLayout->addWidget(makeCopyButton(
                              QStringLiteral("草稿路径"),
                              [this] {
                                  copyText(m_resultDraftPath,
                                           QStringLiteral("草稿路径"));
                              },
                              pathRow),
                          0);
    outputBody->addWidget(pathRow);
    rightLayout->addWidget(output);

    auto *script = new Studio::SectionCard(
        Icons::Glyph::Doc, QStringLiteral("口播文案"),
        QStringLiteral("%1 字").arg(compactScript.size()), right);
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

    auto *adjust = new QWidget(right);
    adjust->setObjectName(QStringLiteral("mixAdjustTip"));
    adjust->setStyleSheet(
        QStringLiteral("#mixAdjustTip { background:%1; border:1px solid %2;"
                       " border-radius:8px; }")
            .arg(Theme::alpha(Theme::Surface1, 0.6).name(QColor::HexArgb),
                 Theme::Border.name()));
    auto *adjustLayout = new QHBoxLayout(adjust);
    adjustLayout->setContentsMargins(12, 9, 12, 9);
    adjustLayout->setSpacing(8);
    adjustLayout->addWidget(Studio::iconLabel(Icons::Glyph::Sparkle, 10, Theme::Ai,
                                               adjust));
    auto *adjustLabel = Ui::label(
        QStringLiteral("想换个表达？修改文案或更换音色后重新混剪，原片无需重复上传。"),
        9, QFont::Normal, Theme::Text3, adjust);
    adjustLabel->setWordWrap(true);
    adjustLayout->addWidget(adjustLabel, 1);
    auto *adjustLink = new LinkLabel(
        QStringLiteral("调整文案与音色并重新混剪 →"), adjust);
    adjustLink->setPixelSize(9);
    adjustLink->setTextColor(Theme::Primary);
    adjustLink->setHoverColor(Theme::PrimaryHover);
    adjustLink->setUnderlineOnHover(true);
    connect(adjustLink, &QAbstractButton::clicked, this,
            &MixcutPage::restartFromResult);
    adjustLayout->addWidget(adjustLink, 0, Qt::AlignVCenter);
    rightLayout->addWidget(adjust);
    rightLayout->addStretch(1);
    columnsLayout->addWidget(right, 116);

    scrollLayout->addWidget(columns);
    auto *scroll = makeScrollArea(scrollContent, m_resultScreen);
    root->addWidget(scroll, 1);
    setPlayIcon(m_resultCenterPlay, false);
    updateGenerationClock();
    updatePlayback();
}

void MixcutPage::openRecent(const StudioData::MixcutResult &result)
{
    if (isRunning()) {
        showProcessing();
        emit toastRequested(QStringLiteral("当前有智能混剪正在生成，完成后即可查看"));
        return;
    }
    m_result = result;
    m_hasResult = true;
    showResult();
}

void MixcutPage::restartFromResult()
{
    if (!m_hasResult) {
        return;
    }
    m_analysisTimer->stop();
    m_voiceTimer->stop();
    m_voiceDebounce->stop();
    m_playTimer->stop();
    m_playing = false;
    m_playProgress = 0;
    m_project.hasSource = true;
    if (m_project.source.name.isEmpty()) {
        m_project.source = StudioData::mixcutDemoSource();
    }
    m_project.analysisReady = true;
    m_project.prompt = m_result.scriptPrompt;
    m_project.script = m_result.script;
    m_project.style = m_result.style;
    m_project.voiceId = m_result.voiceId;
    m_project.ratio = m_result.ratio;
    m_project.voiceStatus = StudioData::VoiceStatus::Ready;
    m_analysisProgress = 100;
    m_voiceProgress = 100;
    m_scriptEdited = false;
    showStudio();
}

void MixcutPage::copyText(const QString &text, const QString &label)
{
    QGuiApplication::clipboard()->setText(text);
    emit toastRequested(QStringLiteral("已复制%1").arg(label));
}

void MixcutPage::updateGenerationClock()
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
