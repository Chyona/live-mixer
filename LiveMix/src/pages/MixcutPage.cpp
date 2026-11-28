#include "MixcutPage.h"

#include "../core/AppConfig.h"
#include "../mixcut/MixcutPipelineService.h"
#include "../mixcut/MixcutTypes.h"
#include "../ui/Controls.h"
#include "../ui/Icons.h"
#include "../ui/Overlays.h"
#include "../ui/StudioPosters.h"
#include "../ui/StudioWidgets.h"
#include "../ui/WindowChrome.h"

#include <QAbstractButton>
#include <QApplication>
#include <QAudioOutput>
#include <QClipboard>
#include <QCursor>
#include <QDateTime>
#include <QDesktopServices>
#include <QDir>
#include <QDragEnterEvent>
#include <QDragLeaveEvent>
#include <QDropEvent>
#include <QEnterEvent>
#include <QFile>
#include <QFileDialog>
#include <QFileInfo>
#include <QFontMetrics>
#include <QGridLayout>
#include <QGuiApplication>
#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <QHBoxLayout>
#include <QLabel>
#include <QLayout>
#include <QLineEdit>
#include <QMediaPlayer>
#include <QMessageBox>
#include <QMimeData>
#include <QPainter>
#include <QPainterPath>
#include <QProgressBar>
#include <QRegularExpression>
#include <QScrollArea>
#include <QSaveFile>
#include <QScrollBar>
#include <QShortcut>
#include <QStandardPaths>
#include <QStackedWidget>
#include <QTextEdit>
#include <QThread>
#include <QTimer>
#include <QToolButton>
#include <QUrl>
#include <QVBoxLayout>
#include <QtMath>

#include <cmath>
#include <functional>
#include <memory>

namespace {

QString cssColor(const QColor &color)
{
    return color.name(QColor::HexArgb);
}

// QMediaPlayer 不能直接读取 qrc 里的 MP3：首次试听时把内置示例音
// 释放到缓存目录，再用本地文件 URL 播放，安装包仍只携带嵌入资源。
QString sampleAudioLocalPath(const QString &voiceId)
{
    QFile resource(QStringLiteral(":/audio/doubao/%1.mp3").arg(voiceId));
    if (!resource.exists() || !resource.open(QIODevice::ReadOnly)) {
        return QString();
    }
    const QByteArray data = resource.readAll();
    resource.close();
    if (data.isEmpty()) {
        return QString();
    }

    QString cacheDir =
        QStandardPaths::writableLocation(QStandardPaths::CacheLocation);
    if (cacheDir.isEmpty()) {
        cacheDir = QDir::tempPath();
    }
    cacheDir = QDir(cacheDir).filePath(QStringLiteral("voice-samples"));
    if (!QDir().mkpath(cacheDir)) {
        return QString();
    }
    const QString path = QDir(cacheDir).filePath(voiceId + QStringLiteral(".mp3"));
    if (QFileInfo::exists(path) && QFileInfo(path).size() == data.size()) {
        return path;
    }
    QFile target(path);
    if (!target.open(QIODevice::WriteOnly | QIODevice::Truncate)) {
        return QString();
    }
    if (target.write(data) != data.size()) {
        target.close();
        return QString();
    }
    target.close();
    return path;
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

class VoiceOptionButton : public QAbstractButton
{
public:
    VoiceOptionButton(const StudioData::MixcutVoice &voice, bool selected,
                      bool current, QWidget *parent = nullptr)
        : QAbstractButton(parent)
        , m_voice(voice)
        , m_selected(selected)
        , m_current(current)
    {
        setCursor(Qt::PointingHandCursor);
        setFixedHeight(46);
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
        setToolTip(QStringLiteral("%1 · %2 · 点击试听示例音")
                       .arg(voice.name, voice.description));
    }

    void setSelected(bool selected)
    {
        if (m_selected == selected) {
            return;
        }
        m_selected = selected;
        update();
    }

    void setPlaying(bool playing)
    {
        if (m_playing == playing) {
            return;
        }
        m_playing = playing;
        if (m_playing) {
            if (!m_eqTimer) {
                m_eqTimer = new QTimer(this);
                m_eqTimer->setInterval(120);
                connect(m_eqTimer, &QTimer::timeout, this, [this] {
                    m_phase = (m_phase + 1) % 12;
                    update();
                });
            }
            m_eqTimer->start();
        } else if (m_eqTimer) {
            m_eqTimer->stop();
        }
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
                                    : Theme::alpha(Theme::Surface2, 0.40));
        painter.drawRoundedRect(rect, 8, 8);

        const QRectF orb(8, 9, 28, 28);
        QLinearGradient gradient(orb.topLeft(), orb.bottomRight());
        gradient.setColorAt(0, m_voice.gradientFrom);
        gradient.setColorAt(1, m_voice.gradientTo);
        painter.setPen(QPen(QColor(255, 255, 255, 46), 1.0));
        painter.setBrush(gradient);
        painter.drawEllipse(orb);
        painter.setFont(Theme::font(10, QFont::DemiBold));
        painter.setPen(Qt::white);
        painter.drawText(orb, Qt::AlignCenter, m_voice.name.left(1));

        // 试听状态覆盖层：播放时显示动态音柱，否则显示播放角标。
        painter.save();
        painter.setPen(Qt::NoPen);
        painter.setBrush(QColor(0, 0, 0, m_playing ? 158 : (m_hovered ? 122 : 56)));
        painter.drawEllipse(orb);
        if (m_playing) {
            const qreal baseY = orb.center().y() - 5;
            for (int index = 0; index < 3; ++index) {
                const int heights[3] = {4, 8, 6};
                const int wobble = (m_phase + index * 4) % 12;
                const qreal height = heights[index] + (wobble < 6 ? wobble / 2 : 3);
                painter.setBrush(Qt::white);
                painter.drawRoundedRect(
                    QRectF(orb.center().x() - 4 + index * 3.5,
                           baseY + 10 - height, 2, height),
                    1, 1);
            }
        } else {
            Icons::paint(&painter,
                         QRectF(orb.center().x() - 4, orb.center().y() - 4, 8, 8),
                         Icons::Glyph::Play, QColor(255, 255, 255, 230), 1.8);
        }
        painter.restore();

        const qreal textLeft = 44;
        const qreal checkWidth = 13;
        const qreal textRight = width() - 8 - checkWidth - 6;

        const QFont nameFont = Theme::font(10, QFont::Medium);
        const QFontMetrics nameMetrics(nameFont);
        painter.setFont(nameFont);
        painter.setPen(Theme::Text1);
        qreal nameWidth = nameMetrics.horizontalAdvance(m_voice.name);
        const qreal nameMax = textRight - textLeft - (m_voice.recommended ? 20 : 0)
            - (m_current ? 30 : 0);
        if (nameWidth > nameMax) {
            nameWidth = qMax<qreal>(0, nameMax);
            painter.setClipRect(QRectF(textLeft, 6, nameWidth, 18));
        }
        painter.drawText(QRectF(textLeft, 6, nameWidth, 18),
                         Qt::AlignLeft | Qt::AlignVCenter,
                         nameMetrics.elidedText(m_voice.name, Qt::ElideRight,
                                                int(nameWidth)));
        painter.setClipping(false);
        qreal badgeX = textLeft + nameWidth + 4;
        if (m_voice.recommended) {
            const QRectF badge(badgeX, 8, 10, 12);
            painter.setPen(Qt::NoPen);
            painter.setBrush(Studio::aiSoft());
            painter.drawRoundedRect(badge, 2, 2);
            painter.setFont(Theme::font(7));
            painter.setPen(Theme::Ai);
            painter.drawText(badge, Qt::AlignCenter, QStringLiteral("荐"));
            badgeX += 14;
        }
        if (m_current) {
            const QRectF badge(badgeX, 8, 24, 12);
            painter.setPen(Qt::NoPen);
            painter.setBrush(Studio::primarySoft());
            painter.drawRoundedRect(badge, 2, 2);
            painter.setFont(Theme::font(7));
            painter.setPen(Theme::Primary);
            painter.drawText(badge, Qt::AlignCenter, QStringLiteral("当前"));
        }

        painter.setFont(Theme::font(8));
        painter.setPen(Theme::Text3);
        painter.drawText(
            QRectF(textLeft, 23, textRight - textLeft, 15),
            Qt::AlignLeft | Qt::AlignVCenter,
            QFontMetrics(Theme::font(8)).elidedText(
                QStringLiteral("%1 · %2 · %3")
                    .arg(m_voice.language, m_voice.gender, m_voice.scene),
                Qt::ElideRight, int(textRight - textLeft)));

        const QRectF check(width() - 8 - checkWidth, height() / 2.0 - 6.5, checkWidth,
                           checkWidth);
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
    bool m_current = false;
    bool m_playing = false;
    bool m_hovered = false;
    int m_phase = 0;
    QTimer *m_eqTimer = nullptr;
};

/* ------------------------------------------------------------------ */
/* MixcutPage                                                           */
/* ------------------------------------------------------------------ */

MixcutPage::MixcutPage(ConfigStore *config, QWidget *parent)
    : QWidget(parent)
    , m_config(config)
{
    m_project = StudioData::defaultMixcutProject();
    loadHistory();

    m_scriptTimer = new QTimer(this);
    m_scriptTimer->setInterval(60);
    connect(m_scriptTimer, &QTimer::timeout, this, &MixcutPage::tickScript);

    m_playTimer = new QTimer(this);
    m_playTimer->setInterval(100);
    connect(m_playTimer, &QTimer::timeout, this, &MixcutPage::updatePlayback);

    m_samplePlayer = new QMediaPlayer(this);
    m_sampleAudio = new QAudioOutput(this);
    m_sampleAudio->setVolume(0.78);
    m_samplePlayer->setAudioOutput(m_sampleAudio);
    connect(m_samplePlayer, &QMediaPlayer::mediaStatusChanged, this,
            [this](QMediaPlayer::MediaStatus status) {
                if (status == QMediaPlayer::EndOfMedia
                    || status == QMediaPlayer::InvalidMedia) {
                    stopVoiceSample();
                }
            });
    connect(m_samplePlayer, &QMediaPlayer::errorOccurred, this,
            [this](QMediaPlayer::Error, const QString &) { stopVoiceSample(); });
    connect(this, &MixcutPage::voiceSampleChanged, this,
            &MixcutPage::refreshVoiceSection);

    m_clockTimer = new QTimer(this);
    m_clockTimer->setInterval(10 * 1000);
    connect(m_clockTimer, &QTimer::timeout, this, &MixcutPage::updateGenerationClock);
    m_clockTimer->start();

    buildUi();
}

MixcutPage::~MixcutPage()
{
    destroyPipeline();
}

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
    // 头部固定，统计、入口卡片与最近混剪在下方独立滚动。
    auto *hubHeaderHost = new QWidget(m_hubScreen);
    auto *hubHeaderLayout = new QVBoxLayout(hubHeaderHost);
    hubHeaderLayout->setContentsMargins(22, 18, 22, 0);
    hubHeaderLayout->setSpacing(0);
    auto *hubHeader = new PageHeader(
        QStringLiteral("智能混剪"),
        QStringLiteral("上传视频或图片素材，输入提示词生成文案，选好音色后一键成片，成片与剪映草稿一次交付。"),
        hubHeaderHost);
    hubHeader->setTitleMeta(new Chip(QStringLiteral("新功能"), Chip::Ai, hubHeader));
    auto *hubStart = new Button(QStringLiteral("开始混剪"), Button::Primary,
                                hubHeader);
    hubStart->setLeadingGlyph(Icons::Glyph::Sparkle);
    connect(hubStart, &QAbstractButton::clicked, this, &MixcutPage::startProject);
    hubHeader->addAction(hubStart);
    hubHeaderLayout->addWidget(hubHeader);
    hubRoot->addWidget(hubHeaderHost);
    m_hubContent = new QWidget;
    m_hubLayout = new QVBoxLayout(m_hubContent);
    m_hubLayout->setContentsMargins(22, 0, 22, 24);
    m_hubLayout->setSpacing(0);
    m_hubLayout->setAlignment(Qt::AlignTop);
    m_hubScroll = makeScrollArea(m_hubContent, m_hubScreen);
    hubRoot->addWidget(m_hubScroll, 1);
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
    return m_pipelineRunning || (m_hasRun && !m_run.done);
}

bool MixcutPage::hasMaterials() const
{
    return !m_project.materials.isEmpty();
}

bool MixcutPage::scriptJobRunning() const
{
    return m_scriptTimer && m_scriptTimer->isActive();
}

int MixcutPage::activeStep() const
{
    return canStart() ? 2 : 1;
}

bool MixcutPage::canStart() const
{
    const StudioData::MixcutScriptStats stats =
        StudioData::mixcutScriptStats(m_project.script);
    return hasMaterials() && m_project.analysisReady && !scriptJobRunning()
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
    heroRow->setMinimumHeight(244);
    heroRow->setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
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
    const int recentCount = m_history.size();
    auto *recentHeading = makeSectionHeading(
        QStringLiteral("最近混剪"),
        recentCount > 0 ? QStringLiteral("%1 个项目").arg(recentCount)
                        : QStringLiteral("暂无记录"),
        recentMeta, m_hubContent);
    auto *recentHeadingRow = new QWidget(m_hubContent);
    auto *recentHeadingLayout = new QHBoxLayout(recentHeadingRow);
    recentHeadingLayout->setContentsMargins(0, 0, 0, 8);
    recentHeadingLayout->addWidget(recentHeading, 1);
    m_hubLayout->addWidget(recentHeadingRow);

    const QVector<StudioData::MixcutResult> recents = m_history;
    if (!recents.isEmpty()) {
        auto *recentGridHost = new QWidget(m_hubContent);
        auto *recentGrid = new QGridLayout(recentGridHost);
        recentGrid->setContentsMargins(0, 0, 0, 0);
        recentGrid->setHorizontalSpacing(12);
        recentGrid->setVerticalSpacing(12);
        for (int index = 0; index < recents.size(); ++index) {
            recentGrid->addWidget(createRecentCard(recents.at(index), recentGridHost),
                                  index / 3, index % 3);
        }
        for (int column = 0; column < 3; ++column) {
            recentGrid->setColumnStretch(column, 1);
        }
        m_hubLayout->addWidget(recentGridHost);
    } else {
        auto *empty = new Studio::EmptyRecent(
            QStringLiteral("还没有混剪记录"),
            QStringLiteral("完成第一个混剪后，成片、发布文案与剪映草稿会自动保存到这里，随时回看或重新混剪。"),
            {QStringLiteral("成片自动下载"), QStringLiteral("发布文案可复制"),
             QStringLiteral("可回看与重新混剪")},
            QStringLiteral("导入本地素材"), QStringLiteral("开始第一个混剪"),
            m_hubContent);
        connect(empty, &Studio::EmptyRecent::startRequested, this,
                &MixcutPage::startProject);
        connect(empty, &Studio::EmptyRecent::sampleRequested, this, [this] {
            if (startProject()) {
                addMaterialsFromDialog();
            }
        });
        m_hubLayout->addWidget(empty);
    }

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
        QStringLiteral("小提示：填写提示词生成文案、选好音色后点击「一键成片」，配音、字幕、音效与镜头匹配会一次完成。示例文案约 %1 字。")
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
    ratioLayout->addWidget(Ui::label(QStringLiteral("支持 9:16 / 16:9"), 10,
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
    leftLayout->addWidget(Ui::label(QStringLiteral("一份素材，AI 帮你混剪成片"), 17,
                                     QFont::DemiBold, Theme::Text1, left));
    leftLayout->addSpacing(4);
    auto *description = Ui::label(
        QStringLiteral("上传视频或图片素材，输入文案提示词生成口播；选好音色后一键成片，配音、字幕、音效与 BGM 一次到位，直接交付成片与剪映草稿。"),
        11, QFont::Normal, Theme::Text2, left);
    description->setWordWrap(true);
    description->setMaximumWidth(360);
    leftLayout->addWidget(description);
    leftLayout->addSpacing(10);
    leftLayout->addWidget(makeBullet(QStringLiteral("视频 / 图片混合，多素材一起剪"),
                                     Theme::Ai, left));
    leftLayout->addWidget(makeBullet(QStringLiteral("输入提示词，AI 快速生成口播文案"),
                                     Theme::Ai, left));
    leftLayout->addWidget(makeBullet(QStringLiteral("40 种豆包音色，一键成片时自动配音"),
                                     Theme::Ai, left));
    leftLayout->addWidget(makeBullet(QStringLiteral("字幕 / 音效 / BGM 智能卡点"),
                                     Theme::Ai, left));
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
    titles->addWidget(Ui::label(QStringLiteral("上传素材，三步成片"), 12,
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
        {QStringLiteral("上传素材"),
         QStringLiteral("拖入视频或图片素材，AI 自动识别镜头与画面内容。"),
         Icons::Glyph::Upload},
        {QStringLiteral("生成文案 · 选音色"),
         QStringLiteral("输入文案提示词生成口播，并挑选喜欢的音色。"),
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
    if (!result.previewPath.isEmpty()) {
        const QImage frame(result.previewPath);
        if (!frame.isNull()) {
            spec.frameImage = frame;
            spec.title.clear();
            spec.caption.clear();
            spec.highlight.clear();
        }
    }
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
            .arg(voice.name, result.durationLabel, result.ratio),
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

namespace {

class MaterialEmptyIcon : public QWidget
{
public:
    explicit MaterialEmptyIcon(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        setFixedSize(60, 60);
        setAttribute(Qt::WA_TransparentForMouseEvents, true);
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        const QRectF box(0, 0, 48, 48);
        painter.setPen(QPen(Theme::BorderStrong, 1.0));
        painter.setBrush(Theme::Surface2);
        painter.drawRoundedRect(box, 15, 15);
        Icons::paint(&painter, QRectF(14, 14, 20, 20), Icons::Glyph::Film,
                     Theme::Text2, 1.8);
        const QRectF badge(34, 34, 20, 20);
        painter.setPen(QPen(Theme::alpha(Theme::Ai, 0.35), 1.0));
        painter.setBrush(QColor(QStringLiteral("#1b1509")));
        painter.drawEllipse(badge);
        Icons::paint(&painter, badge.adjusted(5, 5, -5, -5), Icons::Glyph::Sparkle,
                     Theme::Ai, 1.8);
    }
};

class MixMaterialTile : public QWidget
{
public:
    MixMaterialTile(const StudioData::MixcutMaterial &material, QWidget *parent)
        : QWidget(parent)
        , m_material(material)
    {
        setFixedHeight(88);
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
        setAttribute(Qt::WA_Hover, true);
        setMouseTracking(true);
        setCursor(Qt::PointingHandCursor);
        setToolTip(QStringLiteral("移除「%1」").arg(material.name));
    }

    std::function<void()> removeRequested;

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        const QRectF rect = QRectF(this->rect()).adjusted(0.5, 0.5, -0.5, -0.5);
        const int nameHeight = 19;
        const QRectF sceneRect(rect.left() + 1, rect.top() + 1, rect.width() - 2,
                               rect.height() - nameHeight - 1);
        painter.save();
        QPainterPath clip;
        clip.addRoundedRect(rect, 7, 7);
        painter.setClipPath(clip);
        painter.fillRect(rect, Theme::alpha(Theme::Surface2, 0.45));
        Studio::paintMixScene(painter, sceneRect, m_material.scenes.value(0));
        painter.restore();

        painter.setPen(QPen(m_hovered ? Theme::BorderStrong : Theme::Border, 1.0));
        painter.setBrush(Qt::NoBrush);
        painter.drawRoundedRect(rect, 7, 7);

        // 类型角标。
        const QFont typeFont = Theme::font(8);
        const QFontMetrics typeMetrics(typeFont);
        const QString typeText = m_material.kind == StudioData::MixcutMaterialKind::Video
            ? QStringLiteral("视频")
            : QStringLiteral("图片");
        const qreal typeWidth = 8 + 2 + 2 + typeMetrics.horizontalAdvance(typeText) + 4;
        const QRectF typeBadge(sceneRect.left() + 4, sceneRect.top() + 4, typeWidth,
                               13);
        painter.setPen(Qt::NoPen);
        painter.setBrush(QColor(0, 0, 0, 140));
        painter.drawRoundedRect(typeBadge, 3, 3);
        Icons::paint(&painter,
                     QRectF(typeBadge.left() + 5, typeBadge.center().y() - 4, 8, 8),
                     m_material.kind == StudioData::MixcutMaterialKind::Video
                         ? Icons::Glyph::Film
                         : Icons::Glyph::Image,
                     QColor(255, 255, 255, 217), 1.8);
        painter.setFont(typeFont);
        painter.setPen(QColor(255, 255, 255, 217));
        painter.drawText(typeBadge.adjusted(17, 0, -3, 0),
                         Qt::AlignLeft | Qt::AlignVCenter, typeText);

        if (m_material.kind == StudioData::MixcutMaterialKind::Video) {
            const QString duration = StudioData::formatElapsed(m_material.durationMs);
            const QFont timeFont = Theme::monoFont(8);
            const QFontMetrics timeMetrics(timeFont);
            const qreal width = timeMetrics.horizontalAdvance(duration) + 8;
            const QRectF timeBadge(sceneRect.right() - width - 4,
                                   sceneRect.bottom() - 16, width, 12);
            painter.setPen(Qt::NoPen);
            painter.setBrush(QColor(0, 0, 0, 153));
            painter.drawRoundedRect(timeBadge, 3, 3);
            painter.setFont(timeFont);
            painter.setPen(QColor(255, 255, 255, 204));
            painter.drawText(timeBadge, Qt::AlignCenter, duration);
        }

        if (m_material.sample) {
            const QFont sampleFont = Theme::font(8);
            const QFontMetrics sampleMetrics(sampleFont);
            const qreal width = sampleMetrics.horizontalAdvance(QStringLiteral("示例")) + 8;
            const QRectF sampleBadge(sceneRect.left() + 4,
                                     sceneRect.bottom() - 16, width, 12);
            painter.setPen(Qt::NoPen);
            painter.setBrush(Theme::alpha(Theme::Ai, 0.9));
            painter.drawRoundedRect(sampleBadge, 3, 3);
            painter.setFont(sampleFont);
            painter.setPen(QColor(QStringLiteral("#1b1509")));
            painter.drawText(sampleBadge, Qt::AlignCenter, QStringLiteral("示例"));
        }

        if (m_hovered) {
            const QRectF removeRect(this->width() - 20, 4, 16, 16);
            painter.setPen(Qt::NoPen);
            painter.setBrush(m_removeHovered ? Theme::Danger
                                             : QColor(0, 0, 0, 153));
            painter.drawRoundedRect(removeRect, 3, 3);
            Icons::paint(&painter, removeRect.adjusted(4, 4, -4, -4),
                         Icons::Glyph::Trash, Qt::white, 1.8);
        }

        painter.setFont(Theme::font(9));
        painter.setPen(Theme::Text2);
        const QString elided = QFontMetrics(Theme::font(9)).elidedText(
            m_material.name, Qt::ElideRight,
            qMax(10, int(rect.width()) - 12));
        painter.drawText(
            QRectF(rect.left() + 6, rect.bottom() - nameHeight, rect.width() - 12,
                   nameHeight),
            Qt::AlignLeft | Qt::AlignVCenter, elided);

        m_removeHovered = m_hovered && removeRect().contains(mapFromGlobal(QCursor::pos()));
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
        m_removeHovered = false;
        update();
        QWidget::leaveEvent(event);
    }

    void mouseMoveEvent(QMouseEvent *event) override
    {
        if (m_hovered) {
            const bool overRemove = removeRect().contains(event->pos());
            if (overRemove != m_removeHovered) {
                m_removeHovered = overRemove;
                update();
            }
        }
        QWidget::mouseMoveEvent(event);
    }

    void mouseReleaseEvent(QMouseEvent *event) override
    {
        if (m_hovered && removeRect().contains(event->pos())
            && event->button() == Qt::LeftButton) {
            if (removeRequested) {
                removeRequested();
            }
            return;
        }
        QWidget::mouseReleaseEvent(event);
    }

private:
    QRectF removeRect() const
    {
        return QRectF(width() - 20, 4, 16, 16);
    }

    StudioData::MixcutMaterial m_material;
    bool m_hovered = false;
    bool m_removeHovered = false;
};

class MaterialDropArea : public QWidget
{
public:
    explicit MaterialDropArea(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        setAcceptDrops(true);
        m_overlay = new QWidget(this);
        m_overlay->setObjectName(QStringLiteral("mixMaterialDropOverlay"));
        m_overlay->setStyleSheet(
            QStringLiteral("#mixMaterialDropOverlay { background:rgba(7,9,14,184);"
                           " border-radius:9px; }"));
        auto *layout = new QVBoxLayout(m_overlay);
        layout->setContentsMargins(0, 0, 0, 0);
        layout->setAlignment(Qt::AlignCenter);
        layout->addWidget(Ui::label(QStringLiteral("松开鼠标，添加素材"), 11,
                                    QFont::Normal, Theme::Primary, m_overlay),
                          0, Qt::AlignCenter);
        m_overlay->hide();
    }

    std::function<void(const QStringList &)> filesDropped;

protected:
    void resizeEvent(QResizeEvent *event) override
    {
        m_overlay->setGeometry(rect());
        QWidget::resizeEvent(event);
    }

    void dragEnterEvent(QDragEnterEvent *event) override
    {
        if (event->mimeData()->hasUrls()) {
            m_dragging = true;
            m_overlay->setGeometry(rect());
            m_overlay->show();
            m_overlay->raise();
            event->acceptProposedAction();
        }
    }

    void dragLeaveEvent(QDragLeaveEvent *event) override
    {
        m_dragging = false;
        m_overlay->hide();
        QWidget::dragLeaveEvent(event);
    }

    void dropEvent(QDropEvent *event) override
    {
        m_dragging = false;
        m_overlay->hide();
        QStringList paths;
        for (const QUrl &url : event->mimeData()->urls()) {
            if (url.isLocalFile()) {
                paths.append(url.toLocalFile());
            }
        }
        if (!paths.isEmpty()) {
            event->acceptProposedAction();
            if (filesDropped) {
                filesDropped(paths);
            }
        }
    }

private:
    QWidget *m_overlay = nullptr;
    bool m_dragging = false;
};

} // namespace

bool MixcutPage::startProject()
{
    if (isRunning()) {
        const auto answer = QMessageBox::question(
            this, QStringLiteral("开始新项目"),
            QStringLiteral("当前有正在生成的智能混剪，开始新项目会取消它，是否继续？"),
            QMessageBox::Yes | QMessageBox::No, QMessageBox::No);
        if (answer != QMessageBox::Yes) {
            return false;
        }
        destroyPipeline();
        m_hasRun = false;
        emit taskStatusChanged(QString(), false);
    }

    m_scriptTimer->stop();
    m_playTimer->stop();
    stopVoiceSample();
    m_playing = false;
    m_playProgress = 0;

    const StudioData::MixcutStyle style = m_project.style;
    const QString voiceId = m_project.voiceId;
    const QString ratio = m_project.ratio;
    m_project = StudioData::defaultMixcutProject();
    m_project.style = style;
    m_project.voiceId = voiceId;
    m_project.ratio = ratio;
    m_analysisProgress = 0;
    m_scriptVariant = 0;
    m_scriptEdited = false;
    m_scriptRewrite = false;
    m_hasResult = false;
    showStudio();
    return true;
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
    if (m_studioScroll && m_studioScroll->verticalScrollBar()) {
        scrollValue = m_studioScroll->verticalScrollBar()->value();
    }
    Studio::clearLayout(root);
    m_studioScroll = nullptr;

    m_studioDescription = nullptr;
    m_studioStepLabel = nullptr;
    m_stepRail = nullptr;
    m_outputHint = nullptr;
    m_deliveryHint = nullptr;
    m_styleControl = nullptr;
    m_promptEdit = nullptr;
    m_scriptEdit = nullptr;
    m_scriptSkeleton = nullptr;
    m_scriptSkeletonProgress = nullptr;
    m_scriptRewriteOverlay = nullptr;
    m_scriptRewriteLabel = nullptr;
    m_scriptRewriteProgress = nullptr;
    m_scriptRewriteBar = nullptr;
    m_scriptMeta = nullptr;
    m_scriptStats = nullptr;
    m_scriptSave = nullptr;
    m_generateButton = nullptr;
    m_regenerateButton = nullptr;
    m_voiceMeta = nullptr;
    m_voiceOrb = nullptr;
    m_voiceName = nullptr;
    m_voiceScene = nullptr;
    m_voiceLanguage = nullptr;
    m_voiceRecommendTag = nullptr;
    m_voiceSampleButton = nullptr;
    m_voiceSynthesisHint = nullptr;
    m_footerSummary = nullptr;
    m_footerHint = nullptr;
    m_startButton = nullptr;

    root->addWidget(buildStudioHeader());
    root->addWidget(Ui::separator(false, m_studioScreen));

    auto *scrollContent = new QWidget;
    auto *body = new QHBoxLayout(scrollContent);
    body->setContentsMargins(22, 14, 22, 16);
    body->setSpacing(14);
    body->setAlignment(Qt::AlignTop);

    auto *left = new QWidget(scrollContent);
    auto *leftLayout = new QVBoxLayout(left);
    leftLayout->setContentsMargins(0, 0, 0, 0);
    leftLayout->setSpacing(14);
    leftLayout->addWidget(buildMaterialCard());
    leftLayout->addWidget(buildOutputCard());
    leftLayout->addStretch(1);

    auto *right = new QWidget(scrollContent);
    auto *rightLayout = new QVBoxLayout(right);
    rightLayout->setContentsMargins(0, 0, 0, 0);
    rightLayout->setSpacing(14);
    rightLayout->addWidget(buildScriptCard());
    rightLayout->addWidget(buildVoiceSection());
    rightLayout->addStretch(1);

    body->addWidget(left, 95);
    body->addWidget(right, 105);

    m_studioScroll = makeScrollArea(scrollContent, m_studioScreen);
    root->addWidget(m_studioScroll, 1);
    root->addWidget(Ui::separator(false, m_studioScreen));
    root->addWidget(buildStudioFooter());

    refreshStudioState();
    if (m_scriptEdit) {
        m_settingText = true;
        m_scriptEdit->setPlainText(m_project.script);
        m_settingText = false;
    }
    QTimer::singleShot(0, this, [this, scrollValue] {
        if (m_studioScroll && m_studioScroll->verticalScrollBar()) {
            m_studioScroll->verticalScrollBar()->setValue(scrollValue);
        }
    });
}

QWidget *MixcutPage::buildStudioHeader()
{
    auto *header = new QWidget(m_studioScreen);
    auto *layout = new QVBoxLayout(header);
    layout->setContentsMargins(22, 12, 22, 12);
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
    m_studioStepLabel = Ui::label(
        activeStep() == 2 ? QStringLiteral("一键成片") : QStringLiteral("素材与文案"),
        10, QFont::Normal, Theme::Text2, breadcrumb);
    breadcrumbLayout->addWidget(m_studioStepLabel);
    if (hasMaterials()) {
        breadcrumbLayout->addSpacing(4);
        breadcrumbLayout->addWidget(new StatusDot(6, Theme::Success, breadcrumb));
        breadcrumbLayout->addWidget(Ui::label(QStringLiteral("草稿已自动保存"), 9,
                                               QFont::Normal, Theme::Text3,
                                               breadcrumb));
    }
    breadcrumbLayout->addStretch(1);
    auto *exit = new Button(hasMaterials() ? QStringLiteral("退出编辑")
                                           : QStringLiteral("返回"),
                            Button::Ghost, breadcrumb);
    exit->setCompact(true);
    connect(exit, &QAbstractButton::clicked, this, &MixcutPage::showHub);
    breadcrumbLayout->addWidget(exit);
    layout->addWidget(breadcrumb);
    layout->addSpacing(6);

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
    bool samplesOnly = hasMaterials();
    for (const StudioData::MixcutMaterial &material : m_project.materials) {
        if (!material.sample) {
            samplesOnly = false;
            break;
        }
    }
    if (samplesOnly) {
        titleLine->addWidget(new Chip(QStringLiteral("示例素材"), Chip::Ai, header));
    }
    titleLine->addStretch(1);
    titles->addLayout(titleLine);
    m_studioDescription = Ui::label(headerDescription(), 11, QFont::Normal,
                                    Theme::Text2, header);
    m_studioDescription->setWordWrap(true);
    titles->addWidget(m_studioDescription);
    titleLayout->addLayout(titles, 1);
    layout->addWidget(titleRow);
    layout->addSpacing(10);

    m_stepRail = new Studio::StepRail(header);
    m_stepRail->setSteps({QStringLiteral("素材与文案"),
                          QStringLiteral("一键成片")},
                         {QStringLiteral("上传 · 提示词 · 写稿"),
                          QStringLiteral("配音 · 字幕 · 音效")});
    m_stepRail->setActiveStep(activeStep());
    layout->addWidget(m_stepRail);
    return header;
}

QWidget *MixcutPage::buildMaterialCard()
{
    const StudioData::MixcutMaterialStats stats =
        StudioData::mixcutMaterialStats(m_project.materials);
    QStringList metaParts;
    if (stats.videoCount > 0) {
        metaParts.append(QStringLiteral("%1 段视频").arg(stats.videoCount));
    }
    if (stats.imageCount > 0) {
        metaParts.append(QStringLiteral("%1 张图片").arg(stats.imageCount));
    }
    auto *card = new Studio::SectionCard(
        Icons::Glyph::Film, QStringLiteral("素材"),
        hasMaterials() ? metaParts.join(QStringLiteral(" · "))
                       : QStringLiteral("必填 · 视频 / 图片"),
        m_studioScreen);
    auto *upload = new Button(QStringLiteral("上传素材"), Button::Ghost, card);
    upload->setCompact(true);
    upload->setLeadingGlyph(Icons::Glyph::Upload);
    connect(upload, &QAbstractButton::clicked, this,
            &MixcutPage::addMaterialsFromDialog);
    card->headerLayout()->addWidget(upload);
    card->setBodyMargins(QMargins(12, 12, 12, 12));
    auto *body = card->bodyLayout();

    auto *dropHost = new MaterialDropArea(card);
    body->addWidget(dropHost);
    dropHost->filesDropped = [this](const QStringList &paths) {
        addMaterialPaths(paths, true);
    };
    auto *dropLayout = new QVBoxLayout(dropHost);
    dropLayout->setContentsMargins(0, 0, 0, 0);
    dropLayout->setSpacing(0);

    if (!hasMaterials()) {
        auto *dropZone = new Studio::DropZone(dropHost);
        dropZone->setMinimumHeight(252);
        auto *emptyLayout = dropZone->contentLayout();
        emptyLayout->setContentsMargins(16, 12, 16, 12);
        emptyLayout->setSpacing(4);
        emptyLayout->setAlignment(Qt::AlignCenter);
        emptyLayout->addWidget(new MaterialEmptyIcon(dropZone), 0,
                               Qt::AlignHCenter);
        emptyLayout->addSpacing(2);
        emptyLayout->addWidget(Ui::label(QStringLiteral("拖入视频或图片素材"), 14,
                                          QFont::DemiBold, Theme::Text1,
                                          dropZone),
                               0, Qt::AlignHCenter);
        auto *hint = Ui::label(
            QStringLiteral("支持多选；视频 MP4 / MOV，图片 JPG / PNG。竖屏、横屏都可以，AI 会自动识别镜头与画面。"),
            10, QFont::Normal, Theme::Text3, dropZone);
        hint->setWordWrap(true);
        hint->setAlignment(Qt::AlignCenter);
        hint->setFixedWidth(340);
        hint->setMinimumHeight(34);
        emptyLayout->addWidget(hint, 0, Qt::AlignHCenter);
        auto *buttons = new QWidget(dropZone);
        auto *buttonLayout = new QHBoxLayout(buttons);
        buttonLayout->setContentsMargins(0, 4, 0, 0);
        buttonLayout->setSpacing(8);
        auto *uploadButton = new Button(QStringLiteral("上传素材"), Button::Primary,
                                        buttons);
        uploadButton->setLeadingGlyph(Icons::Glyph::Upload);
        connect(uploadButton, &QAbstractButton::clicked, this,
                &MixcutPage::addMaterialsFromDialog);
        buttonLayout->addWidget(uploadButton);
        emptyLayout->addWidget(buttons, 0, Qt::AlignHCenter);
        auto *features = new QWidget(dropZone);
        auto *featureLayout = new QHBoxLayout(features);
        featureLayout->setContentsMargins(0, 8, 0, 0);
        featureLayout->setSpacing(16);
        for (const QString &feature :
             {QStringLiteral("视频 + 图片混合"), QStringLiteral("多选上传"),
              QStringLiteral("镜头自动识别")}) {
            auto *item = new QWidget(features);
            auto *itemLayout = new QHBoxLayout(item);
            itemLayout->setContentsMargins(0, 0, 0, 0);
            itemLayout->setSpacing(4);
            itemLayout->addWidget(Studio::iconLabel(Icons::Glyph::Check, 10,
                                                     Theme::Success, item));
            itemLayout->addWidget(Ui::label(feature, 9, QFont::Normal,
                                            Theme::Text3, item));
            featureLayout->addWidget(item);
        }
        emptyLayout->addWidget(features, 0, Qt::AlignHCenter);
        connect(dropZone, &Studio::DropZone::filesDropped, this,
                [this](const QStringList &paths) { addMaterialPaths(paths, true); });
        dropLayout->addWidget(dropZone);
    } else {
        auto *gridScroll = new QScrollArea(dropHost);
        gridScroll->setFrameShape(QFrame::NoFrame);
        gridScroll->setWidgetResizable(true);
        gridScroll->setHorizontalScrollBarPolicy(Qt::ScrollBarAlwaysOff);
        gridScroll->setVerticalScrollBarPolicy(Qt::ScrollBarAsNeeded);
        gridScroll->setMaximumHeight(182);
        gridScroll->setStyleSheet(QStringLiteral(
            "QScrollArea { background: transparent; border: none; }"
            "QScrollArea > QWidget > QWidget { background: transparent; }"));
        gridScroll->viewport()->setAutoFillBackground(false);
        auto *gridContent = new QWidget;
        auto *grid = new QGridLayout(gridContent);
        grid->setContentsMargins(0, 0, 0, 0);
        grid->setHorizontalSpacing(6);
        grid->setVerticalSpacing(6);
        const int columnCount = 6;
        for (int index = 0; index < m_project.materials.size(); ++index) {
            auto *tile = new MixMaterialTile(m_project.materials.at(index),
                                             gridContent);
            tile->removeRequested = [this, id = m_project.materials.at(index).id] {
                removeMaterial(id);
            };
            grid->addWidget(tile, index / columnCount, index % columnCount);
        }
        for (int column = 0; column < columnCount; ++column) {
            grid->setColumnStretch(column, 1);
        }
        grid->setRowStretch(grid->rowCount(), 1);
        gridScroll->setWidget(gridContent);
        dropLayout->addWidget(gridScroll);

        auto *statsRow = new QWidget(dropHost);
        statsRow->setObjectName(QStringLiteral("mixMaterialStats"));
        statsRow->setStyleSheet(
            QStringLiteral("#mixMaterialStats { background:transparent;"
                           " border-top:1px solid %1; }")
                .arg(Theme::Border.name()));
        auto *statsLayout = new QHBoxLayout(statsRow);
        statsLayout->setContentsMargins(0, 10, 0, 0);
        statsLayout->setSpacing(8);
        QString summary = QStringLiteral("%1 个素材 · %2 个镜头")
                              .arg(m_project.materials.size())
                              .arg(stats.shots);
        if (stats.videoCount > 0) {
            summary += QStringLiteral(" · 视频共 %1").arg(stats.durationLabel);
        }
        if (m_project.materials.size() > 12) {
            summary += QStringLiteral(" · 向下滚动查看更多");
        }
        statsLayout->addWidget(
            Ui::label(summary, 9, QFont::Normal, Theme::Text3, statsRow), 1);
        auto *aiHint = new QWidget(statsRow);
        auto *aiHintLayout = new QHBoxLayout(aiHint);
        aiHintLayout->setContentsMargins(0, 0, 0, 0);
        aiHintLayout->setSpacing(4);
        aiHintLayout->addWidget(Studio::iconLabel(Icons::Glyph::Sparkle, 10,
                                                   Theme::Ai, aiHint));
        aiHintLayout->addWidget(Ui::label(QStringLiteral("AI 按文案语义挑选画面"),
                                          9, QFont::Normal, Theme::Text3, aiHint));
        statsLayout->addWidget(aiHint, 0, Qt::AlignVCenter);
        auto *clear = new LinkLabel(QStringLiteral("清空"), statsRow);
        clear->setPixelSize(9);
        clear->setTextColor(Theme::Text3);
        clear->setHoverColor(Theme::Danger);
        clear->setUnderlineOnHover(true);
        connect(clear, &QAbstractButton::clicked, this, &MixcutPage::clearMaterials);
        statsLayout->addWidget(clear, 0, Qt::AlignVCenter);
        dropLayout->addWidget(statsRow);
    }
    return card;
}

QWidget *MixcutPage::buildOutputCard()
{
    auto *card = new Studio::SectionCard(
        Icons::Glyph::Wand, QStringLiteral("成片输出"),
        QStringLiteral("画幅可调 · 包装全自动"), m_studioScreen);
    card->setBodyMargins(QMargins(12, 12, 12, 12));
    auto *body = card->bodyLayout();

    auto *ratioRow = new QWidget(card);
    auto *ratioLayout = new QHBoxLayout(ratioRow);
    ratioLayout->setContentsMargins(0, 0, 0, 0);
    ratioLayout->setSpacing(8);
    ratioLayout->addWidget(Ui::label(QStringLiteral("成片画幅"), 10, QFont::Normal,
                                       Theme::Text3, ratioRow));
    auto *ratioControl = new Studio::SegmentedControl(ratioRow);
    ratioControl->setOptions({QStringLiteral("9:16"), QStringLiteral("16:9")});
    ratioControl->setCompact(true);
    ratioControl->setFixedWidth(164);
    ratioControl->setCurrentIndex(m_project.ratio == QLatin1String("16:9") ? 1 : 0);
    connect(ratioControl, &Studio::SegmentedControl::currentChanged, this,
            [this](int index) {
                chooseRatio(index == 1 ? QStringLiteral("16:9")
                                       : QStringLiteral("9:16"));
            });
    ratioLayout->addWidget(ratioControl);
    ratioLayout->addStretch(1);
    m_outputHint = Ui::label(QString(), 9, QFont::Normal, Theme::Text3, ratioRow);
    ratioLayout->addWidget(m_outputHint, 0, Qt::AlignVCenter);
    body->addWidget(ratioRow);
    body->addSpacing(10);
    body->addWidget(Ui::separator(false, card));
    body->addSpacing(10);

    const StudioData::MixcutMaterialStats stats =
        StudioData::mixcutMaterialStats(m_project.materials);
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
         stats.shots > 0
             ? QStringLiteral("从 %1 个镜头中按语义挑选").arg(stats.shots)
             : QStringLiteral("按文案语义自动挑选素材"),
         Icons::Glyph::Image},
    };
    auto *gridHost = new QWidget(card);
    auto *grid = new QGridLayout(gridHost);
    grid->setContentsMargins(0, 0, 0, 0);
    grid->setHorizontalSpacing(6);
    grid->setVerticalSpacing(6);
    for (int index = 0; index < items.size(); ++index) {
        auto *box = new QWidget(gridHost);
        box->setObjectName(QStringLiteral("mixOutputItem"));
        box->setStyleSheet(
            QStringLiteral("#mixOutputItem { background:%1; border:1px solid %2;"
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
        auto *desc = Ui::label(items.at(index).description, 9, QFont::Normal,
                               Theme::Text3, box);
        desc->setWordWrap(false);
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
    m_deliveryHint = Ui::label(QString(), 9, QFont::Normal, Theme::Text3,
                               delivery);
    deliveryLayout->addWidget(m_deliveryHint, 0, Qt::AlignVCenter);
    body->addWidget(delivery);
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
    m_regenerateButton->setToolTip(QStringLiteral("生成文案后可换一版"));
    connect(m_regenerateButton, &QAbstractButton::clicked, this,
            &MixcutPage::regenerateScript);
    card->headerLayout()->addWidget(m_regenerateButton);
    card->setBodyMargins(QMargins(12, 12, 12, 12));
    auto *body = card->bodyLayout();

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
    promptHeadLayout->addWidget(Ui::label(
        hasMaterials() ? QStringLiteral("标题或原始文案 · 用于生成")
                       : QStringLiteral("可先填写，上传素材后生成"),
        8, QFont::Normal, Theme::Text3, promptHead));
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
    m_promptEdit->setFixedHeight(48);
    m_promptEdit->setAcceptRichText(false);
    m_promptEdit->setPlainText(m_project.prompt);
    connect(m_promptEdit, &QTextEdit::textChanged, this,
            &MixcutPage::updatePromptFromEditor);
    promptLayout->addWidget(m_promptEdit);
    body->addWidget(promptBox);
    body->addSpacing(8);

    auto *scriptHost = new QWidget(card);
    scriptHost->setMinimumHeight(118);
    auto *scriptStack = new QGridLayout(scriptHost);
    scriptStack->setContentsMargins(0, 0, 0, 0);
    m_scriptEdit = new QTextEdit(scriptHost);
    Studio::applyTextAreaStyle(m_scriptEdit, false, 11);
    m_scriptEdit->setAcceptRichText(false);
    m_scriptEdit->setFixedHeight(118);
    m_scriptEdit->setPlainText(m_project.script);
    connect(m_scriptEdit, &QTextEdit::textChanged, this,
            &MixcutPage::updateScriptFromEditor);
    scriptStack->addWidget(m_scriptEdit, 0, 0);

    m_scriptSkeleton = new QWidget(scriptHost);
    m_scriptSkeleton->setObjectName(QStringLiteral("mixScriptSkeleton"));
    m_scriptSkeleton->setStyleSheet(
        QStringLiteral("#mixScriptSkeleton { background:#0c0e14;"
                       " border:1px solid %1; border-radius:7px; }")
            .arg(Theme::Border.name()));
    m_scriptSkeleton->setFixedHeight(118);
    auto *skeletonLayout = new QVBoxLayout(m_scriptSkeleton);
    skeletonLayout->setContentsMargins(12, 12, 12, 10);
    skeletonLayout->setSpacing(8);
    auto *skeletonHead = new QHBoxLayout;
    skeletonHead->setContentsMargins(0, 0, 0, 0);
    skeletonHead->setSpacing(6);
    auto *pulseDot = new QWidget(m_scriptSkeleton);
    pulseDot->setFixedSize(6, 6);
    pulseDot->setStyleSheet(QStringLiteral("background:%1;border-radius:3px;")
                                .arg(Theme::Ai.name()));
    skeletonHead->addWidget(pulseDot, 0, Qt::AlignVCenter);
    skeletonHead->addWidget(Ui::label(QStringLiteral("正在按提示词撰写口播文案"),
                                      10, QFont::Normal, Theme::Ai,
                                      m_scriptSkeleton));
    skeletonHead->addStretch(1);
    m_scriptSkeletonProgress = monoLabel(QStringLiteral("0%"), 9, Theme::Text3,
                                          m_scriptSkeleton);
    skeletonHead->addWidget(m_scriptSkeletonProgress);
    skeletonLayout->addLayout(skeletonHead);
    const QVector<int> widths = {92, 100, 84, 66};
    for (const int widthPercent : widths) {
        auto *lineHost = new QWidget(m_scriptSkeleton);
        auto *lineHostLayout = new QHBoxLayout(lineHost);
        lineHostLayout->setContentsMargins(0, 0, 0, 0);
        auto *line = new QWidget(lineHost);
        line->setFixedHeight(8);
        line->setStyleSheet(QStringLiteral("background:%1;border-radius:4px;")
                                .arg(Theme::Surface3.name()));
        lineHostLayout->addWidget(line, widthPercent);
        lineHostLayout->addStretch(100 - widthPercent);
        skeletonLayout->addWidget(lineHost);
    }
    skeletonLayout->addStretch(1);
    auto *skeletonBar = new ProgressBar(m_scriptSkeleton);
    skeletonBar->setBarColor(Theme::Ai);
    skeletonBar->setValue(0);
    skeletonBar->setFixedHeight(2);
    skeletonLayout->addWidget(skeletonBar);
    scriptStack->addWidget(m_scriptSkeleton, 0, 0);

    m_scriptRewriteOverlay = new QWidget(scriptHost);
    m_scriptRewriteOverlay->setObjectName(QStringLiteral("mixScriptOverlay"));
    m_scriptRewriteOverlay->setStyleSheet(QStringLiteral(
        "#mixScriptOverlay { background:rgba(7,9,14,200); border-radius:7px; }"));
    auto *overlayLayout = new QVBoxLayout(m_scriptRewriteOverlay);
    overlayLayout->setContentsMargins(16, 10, 16, 10);
    overlayLayout->setSpacing(6);
    overlayLayout->setAlignment(Qt::AlignCenter);
    m_scriptRewriteLabel = Ui::label(QString(), 10, QFont::Normal, Theme::Ai,
                                      m_scriptRewriteOverlay);
    m_scriptRewriteLabel->setAlignment(Qt::AlignCenter);
    overlayLayout->addWidget(m_scriptRewriteLabel);
    auto *overlayProgressRow = new QWidget(m_scriptRewriteOverlay);
    overlayProgressRow->setFixedWidth(128);
    auto *overlayProgressLayout = new QHBoxLayout(overlayProgressRow);
    overlayProgressLayout->setContentsMargins(0, 0, 0, 0);
    overlayProgressLayout->setSpacing(6);
    m_scriptRewriteProgress = monoLabel(QString(), 8, Theme::Text3,
                                         overlayProgressRow);
    overlayProgressLayout->addWidget(m_scriptRewriteProgress);
    m_scriptRewriteBar = new ProgressBar(overlayProgressRow);
    m_scriptRewriteBar->setBarColor(Theme::Ai);
    m_scriptRewriteBar->setValue(0);
    m_scriptRewriteBar->setFixedHeight(2);
    overlayProgressLayout->addWidget(m_scriptRewriteBar, 1);
    overlayLayout->addWidget(overlayProgressRow, 0, Qt::AlignHCenter);
    scriptStack->addWidget(m_scriptRewriteOverlay, 0, 0);
    body->addWidget(scriptHost);
    body->addSpacing(6);

    auto *statsRow = new QWidget(card);
    auto *statsLayout = new QHBoxLayout(statsRow);
    statsLayout->setContentsMargins(0, 0, 0, 0);
    statsLayout->setSpacing(7);
    m_scriptStats = Ui::richLabel(QString(), 9, Theme::Text3, statsRow);
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

    // 骨架屏与重写遮罩轮流出现；进度值与当前步骤提示在刷新时更新。
    return card;
}

QWidget *MixcutPage::buildVoiceSection()
{
    auto *card = new Studio::SectionCard(Icons::Glyph::Mic,
                                          QStringLiteral("配音音色"),
                                          m_studioScreen);
    m_voiceMeta = card->metaLabel();
    card->setBodyMargins(QMargins(12, 12, 12, 12));
    auto *body = card->bodyLayout();

    auto *row = new QWidget(card);
    row->setObjectName(QStringLiteral("mixVoiceRow"));
    row->setStyleSheet(
        QStringLiteral("#mixVoiceRow { background:%1; border:1px solid %2;"
                       " border-radius:8px; }")
            .arg(Theme::alpha(Theme::Surface2, 0.45).name(QColor::HexArgb),
                 Theme::Border.name()));
    auto *rowLayout = new QHBoxLayout(row);
    rowLayout->setContentsMargins(8, 8, 8, 8);
    rowLayout->setSpacing(8);

    auto *orbHost = new QWidget(row);
    orbHost->setFixedSize(32, 32);
    const StudioData::MixcutVoice voice =
        StudioData::getMixcutVoice(m_project.voiceId);
    m_voiceOrb = new Studio::VoiceOrbWidget(voice, 30, orbHost);
    m_voiceOrb->move(0, 0);
    auto *checkBadge = new QWidget(orbHost);
    checkBadge->setFixedSize(14, 14);
    checkBadge->move(18, 18);
    checkBadge->setObjectName(QStringLiteral("mixVoiceOrbCheck"));
    checkBadge->setStyleSheet(
        QStringLiteral("#mixVoiceOrbCheck { background:%1; border:1px solid %1;"
                       " border-radius:7px; }")
            .arg(Theme::Primary.name()));
    auto *checkLayout = new QHBoxLayout(checkBadge);
    checkLayout->setContentsMargins(0, 0, 0, 0);
    checkLayout->addWidget(Studio::iconLabel(Icons::Glyph::Check, 7,
                                              Theme::OnAccent, checkBadge),
                           0, Qt::AlignCenter);
    rowLayout->addWidget(orbHost, 0, Qt::AlignVCenter);

    auto *texts = new QVBoxLayout;
    texts->setContentsMargins(0, 0, 0, 0);
    texts->setSpacing(1);
    auto *nameRow = new QHBoxLayout;
    nameRow->setContentsMargins(0, 0, 0, 0);
    nameRow->setSpacing(5);
    m_voiceName = Ui::label(voice.name, 11, QFont::Medium, Theme::Text1, row);
    nameRow->addWidget(m_voiceName);
    m_voiceRecommendTag = Studio::tagLabel(QStringLiteral("荐"), Theme::Ai,
                                            Studio::aiSoft(), 7, row);
    m_voiceRecommendTag->setVisible(voice.recommended);
    nameRow->addWidget(m_voiceRecommendTag);
    m_voiceScene = Ui::label(voice.scene, 9, QFont::Normal, Theme::Text3, row);
    nameRow->addWidget(m_voiceScene);
    nameRow->addStretch(1);
    texts->addLayout(nameRow);
    m_voiceLanguage = Ui::label(
        QStringLiteral("%1 · %2").arg(voice.language, voice.gender), 9,
        QFont::Normal, Theme::Text3, row);
    texts->addWidget(m_voiceLanguage);
    rowLayout->addLayout(texts, 1);

    m_voiceSampleButton = new Button(QStringLiteral("示例音"), Button::Default, row);
    m_voiceSampleButton->setCompact(true);
    m_voiceSampleButton->setLeadingGlyph(Icons::Glyph::Play);
    m_voiceSampleButton->setFixedHeight(28);
    connect(m_voiceSampleButton, &QAbstractButton::clicked, this,
            [this] { toggleVoiceSample(m_project.voiceId); });
    rowLayout->addWidget(m_voiceSampleButton, 0, Qt::AlignVCenter);

    auto *change = new Button(QStringLiteral("更换音色"), Button::Default, row);
    change->setCompact(true);
    connect(change, &QAbstractButton::clicked, this, &MixcutPage::openVoicePicker);
    rowLayout->addWidget(change, 0, Qt::AlignVCenter);
    body->addWidget(row);
    body->addSpacing(8);

    auto *synthesis = new QWidget(card);
    synthesis->setObjectName(QStringLiteral("mixVoiceSynthesis"));
    synthesis->setStyleSheet(
        QStringLiteral("#mixVoiceSynthesis { background:%1; border:1px solid %2;"
                       " border-radius:7px; }")
            .arg(Theme::alpha(Theme::Surface2, 0.45).name(QColor::HexArgb),
                 Theme::Border.name()));
    auto *synthesisLayout = new QHBoxLayout(synthesis);
    synthesisLayout->setContentsMargins(10, 8, 10, 8);
    synthesisLayout->setSpacing(10);
    synthesisLayout->addWidget(makeIconCircle(Icons::Glyph::Mic, 24, Theme::Ai,
                                               Theme::Surface1, Theme::Border,
                                               synthesis),
                               0, Qt::AlignVCenter);
    auto *synthesisTexts = new QVBoxLayout;
    synthesisTexts->setContentsMargins(0, 0, 0, 0);
    synthesisTexts->setSpacing(1);
    auto *synthesisHead = new QHBoxLayout;
    synthesisHead->setContentsMargins(0, 0, 0, 0);
    synthesisHead->setSpacing(5);
    synthesisHead->addWidget(Ui::label(QStringLiteral("配音合成"), 10, QFont::Medium,
                                        Theme::Text2, synthesis));
    synthesisHead->addWidget(Studio::tagLabel(QStringLiteral("一键成片时"),
                                               Theme::Ai, Studio::aiSoft(), 8,
                                               synthesis));
    synthesisHead->addStretch(1);
    synthesisTexts->addLayout(synthesisHead);
    m_voiceSynthesisHint = Ui::label(QString(), 9, QFont::Normal, Theme::Text3,
                                     synthesis);
    m_voiceSynthesisHint->setWordWrap(false);
    synthesisTexts->addWidget(m_voiceSynthesisHint);
    synthesisLayout->addLayout(synthesisTexts, 1);
    body->addWidget(synthesis);

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

    if (!hasMaterials()) {
        auto *upload = new Button(QStringLiteral("上传素材"), Button::Primary,
                                  footer);
        upload->setLeadingGlyph(Icons::Glyph::Upload);
        connect(upload, &QAbstractButton::clicked, this,
                &MixcutPage::addMaterialsFromDialog);
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

void MixcutPage::addMaterialsFromDialog()
{
    const QStringList paths = QFileDialog::getOpenFileNames(
        this, QStringLiteral("选择素材"), QString(),
        QStringLiteral("素材文件 (*.mp4 *.mov *.m4v *.webm *.avi *.mkv *.jpg "
                       "*.jpeg *.png *.webp *.heic *.gif)"));
    if (paths.isEmpty()) {
        return;
    }
    addMaterialPaths(paths, true);
}

void MixcutPage::addMaterialPaths(const QStringList &paths, bool announce)
{
    QStringList valid;
    int ignored = 0;
    for (const QString &path : paths) {
        if (StudioData::isMixcutMaterialFile(path)) {
            valid.append(path);
        } else {
            ++ignored;
        }
    }
    if (valid.isEmpty()) {
        emit toastRequested(QStringLiteral("请选择视频或图片素材（MP4 / MOV / JPG / PNG）"));
        return;
    }
    if (ignored > 0) {
        emit toastRequested(QStringLiteral("已忽略 %1 个不支持的文件").arg(ignored));
    }
    QVector<StudioData::MixcutMaterial> materials;
    for (int index = 0; index < valid.size(); ++index) {
        const QFileInfo info(valid.at(index));
        materials.append(StudioData::mixcutMaterialFromFile(
            valid.at(index), info.size(), m_project.materials.size() + index));
    }
    appendMaterials(materials, announce);
}

void MixcutPage::appendMaterials(
    const QVector<StudioData::MixcutMaterial> &materials, bool announce)
{
    if (materials.isEmpty()) {
        return;
    }
    const bool first = m_project.materials.isEmpty();
    if (first) {
        m_scriptTimer->stop();
        m_scriptVariant = 0;
        m_scriptEdited = false;
        m_analysisProgress = 0;
        m_project.script.clear();
        m_project.analysisReady = true;
    }
    m_project.materials += materials;
    showStudio();
    if (announce) {
        if (first && materials.first().sample) {
            emit toastRequested(QStringLiteral(
                "已载入示例素材，填写文案提示词后点击「生成文案」"));
        } else if (first) {
            emit toastRequested(
                QStringLiteral("已导入 %1 个素材，填写文案提示词后点击「生成文案」")
                    .arg(materials.size()));
        } else {
            emit toastRequested(
                QStringLiteral("已添加 %1 个素材，会用于后续镜头匹配")
                    .arg(materials.size()));
        }
    }
}

void MixcutPage::addSampleMaterials()
{
    // 示例素材没有本地文件，不能参与真实 TTS / 草稿合成；引导选择本地素材。
    emit toastRequested(QStringLiteral(
        "示例素材仅用于界面预览，请选择本地视频或图片开始真实混剪"));
    addMaterialsFromDialog();
}

void MixcutPage::removeMaterial(const QString &id)
{
    int index = -1;
    for (int i = 0; i < m_project.materials.size(); ++i) {
        if (m_project.materials.at(i).id == id) {
            index = i;
            break;
        }
    }
    if (index < 0) {
        return;
    }
    if (m_project.materials.size() == 1 && !m_project.script.trimmed().isEmpty()) {
        const auto answer = QMessageBox::question(
            this, QStringLiteral("移除素材"),
            QStringLiteral("移除最后一个素材会清空当前文案，是否继续？"),
            QMessageBox::Yes | QMessageBox::No, QMessageBox::No);
        if (answer != QMessageBox::Yes) {
            return;
        }
    }
    m_project.materials.removeAt(index);
    if (m_project.materials.isEmpty()) {
        m_scriptTimer->stop();
        m_analysisProgress = 0;
        m_scriptEdited = false;
        m_scriptRewrite = false;
        m_project.analysisReady = false;
        m_project.script.clear();
    }
    rebuildStudio();
}

void MixcutPage::clearMaterials()
{
    if (m_project.script.trimmed().isEmpty()
        || QMessageBox::question(
               this, QStringLiteral("清空素材"),
               QStringLiteral("清空全部素材会同时清空文案，是否继续？"),
               QMessageBox::Yes | QMessageBox::No, QMessageBox::No)
            == QMessageBox::Yes) {
        m_scriptTimer->stop();
        m_analysisProgress = 0;
        m_scriptVariant = 0;
        m_scriptEdited = false;
        m_scriptRewrite = false;
        m_project.materials.clear();
        m_project.analysisReady = false;
        m_project.script.clear();
        rebuildStudio();
    }
}

void MixcutPage::chooseStyle(StudioData::MixcutStyle style)
{
    if (style == m_project.style || scriptJobRunning()) {
        return;
    }
    m_scriptVariant = 0;
    m_project.style = style;
    emit toastRequested(
        m_project.script.trimmed().isEmpty()
            ? QStringLiteral("已切换为「%1」风格，生成文案时会套用")
                  .arg(StudioData::mixcutStyleName(style))
            : QStringLiteral("已切换为「%1」风格，点击「换一版」可按新风格重写文案")
                  .arg(StudioData::mixcutStyleName(style)));
    refreshStudioState();
}

void MixcutPage::chooseVoice(const QString &voiceId)
{
    if (voiceId == m_project.voiceId) {
        return;
    }
    m_project.voiceId = voiceId;
    const StudioData::MixcutVoice voice = StudioData::getMixcutVoice(voiceId);
    emit toastRequested(
        QStringLiteral("已选择音色「%1 · %2」，一键成片时自动生成配音")
            .arg(voice.name, voice.label));
    refreshStudioState();
}

void MixcutPage::chooseRatio(const QString &ratio)
{
    if (ratio == m_project.ratio) {
        return;
    }
    m_project.ratio = ratio;
    refreshStudioState();
}

void MixcutPage::toggleVoiceSample(const QString &voiceId)
{
    if (voiceId.isEmpty()) {
        return;
    }
    if (m_sampleVoiceId == voiceId) {
        stopVoiceSample();
        return;
    }
    m_samplePlayer->stop();
    const StudioData::MixcutVoice voice = StudioData::getMixcutVoice(voiceId);
    m_sampleVoiceId = voiceId;
    const QString localPath = sampleAudioLocalPath(voiceId);
    if (localPath.isEmpty()) {
        emit toastRequested(QStringLiteral("示例音加载失败，请重试"));
        return;
    }
    m_samplePlayer->setSource(QUrl::fromLocalFile(localPath));
    m_samplePlayer->play();
    emit voiceSampleChanged();
}

void MixcutPage::stopVoiceSample()
{
    if (m_sampleVoiceId.isEmpty()) {
        return;
    }
    m_samplePlayer->stop();
    m_sampleVoiceId.clear();
    emit voiceSampleChanged();
}

void MixcutPage::openVoicePicker()
{
    auto *host = window();
    if (!host) {
        return;
    }
    auto *overlay = new ModalOverlay(host);
    auto *card = new ModalCard(QStringLiteral("选择配音音色"), 760);
    card->setContentSpacing(10);
    auto *micIcon = Studio::iconLabel(Icons::Glyph::Mic, 14, Theme::Ai, card);
    card->titleLayout()->insertWidget(0, micIcon);
    card->titleLayout()->insertWidget(
        2, Studio::pillLabel(
               QStringLiteral("豆包语音 · %1 种")
                   .arg(StudioData::mixcutVoices().size()),
               Theme::Text3, Theme::Surface2, Theme::Border, 9, card));

    struct PickerState {
        QString draftId;
        QString query;
        QString language = QStringLiteral("全部");
        QString gender = QStringLiteral("全部");
        QVector<VoiceOptionButton *> buttons;
        QGridLayout *grid = nullptr;
        QWidget *gridHost = nullptr;
        QLabel *countLabel = nullptr;
        QLabel *footerLabel = nullptr;
        std::function<void()> rebuild;
        std::function<void()> refresh;
    };
    auto state = std::make_shared<PickerState>();
    state->draftId = m_project.voiceId;

    auto *searchRow = new QWidget(card);
    auto *searchRowLayout = new QHBoxLayout(searchRow);
    searchRowLayout->setContentsMargins(0, 0, 0, 0);
    searchRowLayout->setSpacing(8);
    auto *searchHost = new QWidget(searchRow);
    searchHost->setFixedHeight(32);
    searchHost->setObjectName(QStringLiteral("mixVoiceSearch"));
    searchHost->setStyleSheet(
        QStringLiteral("#mixVoiceSearch { background:%1; border:1px solid %2;"
                       " border-radius:7px; }")
            .arg(Theme::alpha(Theme::Surface2, 0.6).name(QColor::HexArgb),
                 Theme::Border.name()));
    auto *searchLayout = new QHBoxLayout(searchHost);
    searchLayout->setContentsMargins(10, 0, 6, 0);
    searchLayout->setSpacing(6);
    searchLayout->addWidget(Studio::iconLabel(Icons::Glyph::Search, 12, Theme::Text3,
                                               searchHost));
    auto *searchEdit = new QLineEdit(searchHost);
    searchEdit->setPlaceholderText(QStringLiteral("搜索音色名称、场景或音色 ID"));
    searchEdit->setStyleSheet(QStringLiteral(
        "QLineEdit { background:transparent; border:none; color:%1;"
        " selection-background-color:#4A63BC; }")
                                  .arg(Theme::Text1.name()));
    searchEdit->setFont(Theme::font(10));
    searchLayout->addWidget(searchEdit, 1);
    auto *clearSearch = new GlyphButton(Icons::Glyph::Close, 16, searchHost);
    clearSearch->setGlyphSize(9);
    clearSearch->setColors(Theme::Text3, Theme::Text1);
    clearSearch->setBackgroundColors(Qt::transparent, Theme::Surface3);
    clearSearch->setToolTipText(QStringLiteral("清空搜索"));
    clearSearch->hide();
    searchLayout->addWidget(clearSearch);
    searchRowLayout->addWidget(searchHost, 1);
    state->countLabel = monoLabel(QString(), 9, Theme::Text3, searchRow);
    searchRowLayout->addWidget(state->countLabel, 0, Qt::AlignVCenter);
    card->contentLayout()->addWidget(searchRow);

    auto *filterRow = new QWidget(card);
    auto *filterLayout = new QHBoxLayout(filterRow);
    filterLayout->setContentsMargins(0, 0, 0, 0);
    filterLayout->setSpacing(6);
    filterLayout->addWidget(Ui::label(QStringLiteral("语言"), 9, QFont::Normal,
                                       Theme::Text3, filterRow));
    auto *languageControl = new Studio::SegmentedControl(filterRow);
    languageControl->setOptions({QStringLiteral("全部"), QStringLiteral("中文"),
                                 QStringLiteral("外语")});
    languageControl->setCompact(true);
    languageControl->setFixedWidth(132);
    filterLayout->addWidget(languageControl);
    filterLayout->addSpacing(4);
    filterLayout->addWidget(Ui::label(QStringLiteral("性别"), 9, QFont::Normal,
                                       Theme::Text3, filterRow));
    auto *genderControl = new Studio::SegmentedControl(filterRow);
    genderControl->setOptions({QStringLiteral("全部"), QStringLiteral("女声"),
                               QStringLiteral("男声")});
    genderControl->setCompact(true);
    genderControl->setFixedWidth(132);
    filterLayout->addWidget(genderControl);
    filterLayout->addStretch(1);
    auto *previewHint = new QWidget(filterRow);
    auto *previewHintLayout = new QHBoxLayout(previewHint);
    previewHintLayout->setContentsMargins(0, 0, 0, 0);
    previewHintLayout->setSpacing(4);
    previewHintLayout->addWidget(Studio::iconLabel(Icons::Glyph::Play, 10,
                                                    Theme::Text3, previewHint));
    previewHintLayout->addWidget(Ui::label(QStringLiteral("点击卡片试听示例音"), 9,
                                           QFont::Normal, Theme::Text3,
                                           previewHint));
    filterLayout->addWidget(previewHint, 0, Qt::AlignVCenter);
    card->contentLayout()->addWidget(filterRow);

    auto *gridContent = new QWidget;
    state->grid = new QGridLayout(gridContent);
    state->grid->setContentsMargins(0, 0, 0, 0);
    state->grid->setHorizontalSpacing(6);
    state->grid->setVerticalSpacing(6);
    state->grid->setAlignment(Qt::AlignTop);
    state->gridHost = gridContent;
    auto *scroll = new QScrollArea(card);
    scroll->setFrameShape(QFrame::NoFrame);
    scroll->setWidgetResizable(true);
    scroll->setHorizontalScrollBarPolicy(Qt::ScrollBarAlwaysOff);
    scroll->setVerticalScrollBarPolicy(Qt::ScrollBarAsNeeded);
    scroll->setMaximumHeight(360);
    scroll->setStyleSheet(QStringLiteral(
        "QScrollArea { background: transparent; border: none; }"
        "QScrollArea > QWidget > QWidget { background: transparent; }"));
    scroll->viewport()->setAutoFillBackground(false);
    scroll->setWidget(gridContent);
    card->contentLayout()->addWidget(scroll);

    state->rebuild = [this, state, searchEdit] {
        while (QLayoutItem *item = state->grid->takeAt(0)) {
            if (QWidget *widget = item->widget()) {
                widget->deleteLater();
            }
            delete item;
        }
        state->buttons.clear();
        const QString keyword = state->query.trimmed().toLower();
        QVector<StudioData::MixcutVoice> filtered;
        for (const StudioData::MixcutVoice &voice : StudioData::mixcutVoices()) {
            if (state->language == QStringLiteral("中文")
                && voice.language != QStringLiteral("中文")) {
                continue;
            }
            if (state->language == QStringLiteral("外语")
                && voice.language == QStringLiteral("中文")) {
                continue;
            }
            if (state->gender != QStringLiteral("全部")
                && voice.gender != state->gender) {
                continue;
            }
            if (!keyword.isEmpty()) {
                const QString haystack = QStringLiteral("%1 %2 %3 %4 %5 %6")
                                             .arg(voice.name, voice.id, voice.label,
                                                  voice.language, voice.scene,
                                                  voice.gender)
                                             .toLower();
                if (!haystack.contains(keyword)) {
                    continue;
                }
            }
            filtered.append(voice);
        }
        state->countLabel->setText(
            QStringLiteral("%1/%2").arg(filtered.size())
                .arg(StudioData::mixcutVoices().size()));
        if (filtered.isEmpty()) {
            auto *empty = Ui::label(
                QStringLiteral("没有找到匹配的音色，试试其他关键词或筛选条件"),
                10, QFont::Normal, Theme::Text3, state->gridHost);
            empty->setAlignment(Qt::AlignCenter);
            empty->setMinimumHeight(180);
            state->grid->addWidget(empty, 0, 0, 1, 3);
            return;
        }
        for (int index = 0; index < filtered.size(); ++index) {
            const StudioData::MixcutVoice voice = filtered.at(index);
            auto *option = new VoiceOptionButton(
                voice, voice.id == state->draftId,
                voice.id == m_project.voiceId, state->gridHost);
            option->setPlaying(voice.id == m_sampleVoiceId);
            state->buttons.append(option);
            connect(option, &QAbstractButton::clicked, this,
                    [this, state, id = voice.id] {
                        state->draftId = id;
                        if (state->refresh) {
                            state->refresh();
                        }
                        toggleVoiceSample(id);
                    });
            state->grid->addWidget(option, index / 3, index % 3);
        }
        Q_UNUSED(searchEdit);
    };

    state->refresh = [this, state] {
        for (VoiceOptionButton *button : state->buttons) {
            button->setSelected(button->voiceId() == state->draftId);
            button->setPlaying(button->voiceId() == m_sampleVoiceId);
        }
        if (!state->footerLabel) {
            return;
        }
        const StudioData::MixcutVoice currentVoice =
            StudioData::getMixcutVoice(m_project.voiceId);
        QString html =
            QStringLiteral(
                "<span style=\"color:%1;\">当前使用</span> "
                "<span style=\"color:%2;\">%3</span> "
                "<span style=\"color:%4;\">·</span> "
                "<span style=\"color:%1;\">%5</span>")
                .arg(Theme::Text3.name(), Theme::Text2.name(), currentVoice.name,
                     Theme::BorderStrong.name(), currentVoice.label);
        if (state->draftId != m_project.voiceId) {
            html += QStringLiteral(
                        " <span style=\"color:%1;\">→</span> "
                        "<span style=\"color:%2;\">已选 %3</span>")
                        .arg(Theme::BorderStrong.name(), Theme::Primary.name(),
                             StudioData::getMixcutVoice(state->draftId).name);
        }
        state->footerLabel->setText(html);
    };

    auto *footerLabel = Ui::richLabel(QString(), 9, Theme::Text3, card);
    footerLabel->setMinimumWidth(260);
    state->footerLabel = footerLabel;
    card->footerLayout()->insertWidget(0, footerLabel);

    auto *cancel = new Button(QStringLiteral("取消"), Button::Default, card);
    auto *apply = new Button(QStringLiteral("使用此音色"), Button::Primary, card);
    connect(cancel, &QAbstractButton::clicked, overlay, &ModalOverlay::closeModal);
    connect(apply, &QAbstractButton::clicked, this, [this, overlay, state] {
        chooseVoice(state->draftId);
        overlay->closeModal();
    });
    card->addFooterButton(cancel);
    card->addFooterButton(apply);

    connect(searchEdit, &QLineEdit::textChanged, this,
            [state, clearSearch](const QString &text) {
                state->query = text;
                clearSearch->setVisible(!text.isEmpty());
                if (state->rebuild) {
                    state->rebuild();
                }
            });
    connect(clearSearch, &QAbstractButton::clicked, searchEdit,
            &QLineEdit::clear);
    connect(languageControl, &Studio::SegmentedControl::currentChanged, this,
            [state](int index) {
                state->language = index == 1 ? QStringLiteral("中文")
                    : (index == 2 ? QStringLiteral("外语") : QStringLiteral("全部"));
                if (state->rebuild) {
                    state->rebuild();
                }
            });
    connect(genderControl, &Studio::SegmentedControl::currentChanged, this,
            [state](int index) {
                state->gender = index == 1 ? QStringLiteral("女声")
                    : (index == 2 ? QStringLiteral("男声") : QStringLiteral("全部"));
                if (state->rebuild) {
                    state->rebuild();
                }
            });
    connect(this, &MixcutPage::voiceSampleChanged, card, [state] {
        if (state->refresh) {
            state->refresh();
        }
    });
    connect(overlay, &ModalOverlay::closed, this, [this, overlay] {
        stopVoiceSample();
        overlay->deleteLater();
    });

    state->rebuild();
    state->refresh();
    overlay->showModal(card);
    searchEdit->setFocus();
    state->refresh();
}

void MixcutPage::updatePromptFromEditor()
{
    if (!m_promptEdit || m_settingText) {
        return;
    }
    m_project.prompt = m_promptEdit->toPlainText();
    refreshStudioState();
}

void MixcutPage::updateScriptFromEditor()
{
    if (!m_scriptEdit || m_settingText) {
        return;
    }
    m_project.script = m_scriptEdit->toPlainText();
    m_scriptEdited = true;
    refreshStudioState();
}

void MixcutPage::generateFromPrompt()
{
    if (!hasMaterials() || scriptJobRunning()) {
        return;
    }
    const QString prompt = m_project.prompt.trimmed();
    if (prompt.isEmpty()) {
        return;
    }
    if (m_scriptEdited
        && QMessageBox::question(
               this, QStringLiteral("生成文案"),
               QStringLiteral("按提示词生成会覆盖你手动修改的文案，是否继续？"),
               QMessageBox::Yes | QMessageBox::No, QMessageBox::No)
            != QMessageBox::Yes) {
        return;
    }
    const bool rewriting = !m_project.script.trimmed().isEmpty();
    if (rewriting) {
        ++m_scriptVariant;
    }
    startScriptJob(1800, rewriting);
}

void MixcutPage::regenerateScript()
{
    if (scriptJobRunning() || m_project.script.trimmed().isEmpty()) {
        return;
    }
    if (m_scriptEdited
        && QMessageBox::question(
               this, QStringLiteral("换一版文案"),
               QStringLiteral("换一版会覆盖你手动修改的文案，是否继续？"),
               QMessageBox::Yes | QMessageBox::No, QMessageBox::No)
            != QMessageBox::Yes) {
        return;
    }
    ++m_scriptVariant;
    startScriptJob(1800, true);
}

void MixcutPage::startScriptJob(qint64 durationMs, bool rewriting,
                                const QString &announce)
{
    if (!hasMaterials()) {
        return;
    }
    m_scriptTimer->stop();
    m_analysisProgress = 0;
    m_scriptRewrite = rewriting;
    m_scriptStartedAt = QDateTime::currentMSecsSinceEpoch();
    m_scriptDuration = durationMs;
    m_project.analysisReady = false;
    m_scriptTimer->start();
    refreshStudioState();
    if (!announce.isEmpty()) {
        emit toastRequested(announce);
    }
}

void MixcutPage::tickScript()
{
    const qint64 elapsed = QDateTime::currentMSecsSinceEpoch() - m_scriptStartedAt;
    m_analysisProgress = qBound(
        0, static_cast<int>(qMin<qint64>(100, elapsed * 100 / qMax<qint64>(1, m_scriptDuration))),
        100);
    refreshScriptProgress();
    if (m_analysisProgress < 100) {
        return;
    }
    m_scriptTimer->stop();
    m_project.script = StudioData::generateMixcutScriptFromPrompt(
        m_project.prompt, m_project.style, m_scriptVariant);
    m_project.analysisReady = true;
    m_scriptEdited = false;
    m_scriptRewrite = false;
    if (m_scriptEdit) {
        m_settingText = true;
        m_scriptEdit->setPlainText(m_project.script);
        m_settingText = false;
    }
    refreshStudioState();
}

void MixcutPage::refreshStudioState()
{
    if (!m_studioScreen) {
        return;
    }
    const bool job = scriptJobRunning();
    const bool ready = m_project.analysisReady;
    const bool hasScript = !m_project.script.trimmed().isEmpty();
    const StudioData::MixcutScriptStats stats =
        StudioData::mixcutScriptStats(m_project.script);

    if (m_studioDescription) {
        m_studioDescription->setText(headerDescription());
    }
    if (m_stepRail) {
        m_stepRail->setActiveStep(activeStep());
    }
    if (m_studioStepLabel) {
        m_studioStepLabel->setText(activeStep() == 2 ? QStringLiteral("一键成片")
                                                     : QStringLiteral("素材与文案"));
    }
    if (m_deliveryHint) {
        m_deliveryHint->setText(
            stats.chars > 0
                ? QStringLiteral("预计 %1 · 自动下载并写入草稿")
                      .arg(stats.durationLabel)
                : QStringLiteral("文案生成后自动估算时长"));
    }
    if (m_outputHint) {
        m_outputHint->setText(
            QStringLiteral("%1 · %2")
                .arg(StudioData::mixcutRatioDimensions(m_project.ratio),
                     StudioData::mixcutRatioHint(m_project.ratio)));
    }
    if (m_styleControl) {
        m_styleControl->setEnabled(!job);
    }
    if (m_promptEdit) {
        m_promptEdit->setEnabled(!job);
    }
    if (m_generateButton) {
        m_generateButton->setEnabled(hasMaterials() && ready && !job
                                     && !m_project.prompt.trimmed().isEmpty());
        m_generateButton->setText(job ? QStringLiteral("生成中…")
                                      : QStringLiteral("生成文案"));
        m_generateButton->setToolTip(
            !hasMaterials()
                ? QStringLiteral("请先上传素材")
                : (m_project.prompt.trimmed().isEmpty()
                       ? QStringLiteral("请先输入文案提示词")
                       : QStringLiteral("按提示词生成文案（⌘/Ctrl + Enter）")));
    }
    if (m_regenerateButton) {
        m_regenerateButton->setEnabled(ready && hasScript && !job);
        m_regenerateButton->setToolTip(
            hasScript ? QStringLiteral("让 AI 换一版文案（会沿用提示词）")
                      : QStringLiteral("生成文案后可换一版"));
    }
    if (m_scriptEdit) {
        m_scriptEdit->setEnabled(ready && !job);
        m_scriptEdit->setVisible(!(job && !hasScript));
        m_scriptEdit->setPlaceholderText(
            hasMaterials()
                ? QStringLiteral("填写文案提示词并点击「生成文案」，也可以直接在这里写口播文案…")
                : QStringLiteral("上传素材后，填写文案提示词生成口播文案…"));
    }
    if (m_scriptSkeleton) {
        m_scriptSkeleton->setVisible(job && !hasScript);
        m_scriptSkeleton->raise();
    }
    if (m_scriptRewriteOverlay) {
        m_scriptRewriteOverlay->setVisible(job && hasScript);
        if (job && hasScript) {
            m_scriptRewriteOverlay->raise();
        }
        if (m_scriptRewriteLabel) {
            m_scriptRewriteLabel->setText(
                m_project.prompt.trimmed().isEmpty()
                    ? QStringLiteral("正在换一版文案…")
                    : QStringLiteral("正在按提示词生成文案…"));
        }
    }
    if (m_startButton) {
        m_startButton->setEnabled(canStart());
        m_startButton->setText(job ? QStringLiteral("文案生成中…")
                                   : QStringLiteral("一键成片"));
        m_startButton->setToolTip(
            canStart()
                ? QStringLiteral("自动生成「%1」配音并合成成片与剪映草稿")
                      .arg(StudioData::getMixcutVoice(m_project.voiceId).name)
                : (job ? QStringLiteral("AI 正在撰写文案")
                       : QStringLiteral("请先生成或填写文案")));
    }
    refreshScriptMeta();
    refreshScriptProgress();
    refreshVoiceSection();
    refreshFooter();
}

void MixcutPage::refreshScriptMeta()
{
    if (!m_scriptMeta) {
        return;
    }
    const bool job = scriptJobRunning();
    const StudioData::MixcutScriptStats stats =
        StudioData::mixcutScriptStats(m_project.script);
    const bool hasScript = stats.chars > 0;
    if (job) {
        m_scriptMeta->setText(m_scriptRewrite
                                  ? QStringLiteral("正在重写…")
                                  : QStringLiteral("正在按提示词撰写…"));
    } else if (!hasMaterials()) {
        m_scriptMeta->setText(QStringLiteral("等待素材"));
    } else if (hasScript) {
        m_scriptMeta->setText(QStringLiteral("%1 字 · %2 句")
                                  .arg(stats.chars)
                                  .arg(stats.sentences));
    } else {
        m_scriptMeta->setText(QStringLiteral("待生成文案"));
    }
    m_scriptMeta->setVisible(true);
    if (m_scriptStats) {
        if (hasScript) {
            m_scriptStats->setText(
                QStringLiteral("%1 字 · %2 句 · 预计口播 "
                               "<b style=\"font-family:monospace;color:%3;\">%4</b>"
                               " · 成片时按句切字幕")
                    .arg(stats.chars)
                    .arg(stats.sentences)
                    .arg(Theme::Text2.name(), stats.durationLabel));
        } else {
            m_scriptStats->setText(
                hasMaterials()
                    ? QStringLiteral("填写提示词生成文案，一键成片时自动生成配音与字幕")
                    : QStringLiteral("上传素材并生成文案后，一键成片时自动生成配音"));
        }
    }
    if (m_scriptSave) {
        m_scriptSave->setVisible(hasMaterials());
    }
    if (m_scriptSave && m_scriptSave->parentWidget()) {
        m_scriptSave->parentWidget()->setVisible(hasMaterials());
    }
}

void MixcutPage::refreshScriptProgress()
{
    const QString percent = QStringLiteral("%1%").arg(m_analysisProgress);
    if (m_scriptSkeletonProgress) {
        m_scriptSkeletonProgress->setText(percent);
    }
    if (m_scriptRewriteProgress) {
        m_scriptRewriteProgress->setText(percent);
    }
    if (m_scriptRewriteBar) {
        m_scriptRewriteBar->setValue(m_analysisProgress);
    }
    if (m_scriptSkeleton) {
        auto *bar = m_scriptSkeleton->findChild<ProgressBar *>();
        if (bar) {
            bar->setValue(m_analysisProgress);
        }
    }
}

void MixcutPage::refreshVoiceSection()
{
    if (!m_voiceOrb) {
        return;
    }
    const StudioData::MixcutVoice voice =
        StudioData::getMixcutVoice(m_project.voiceId);
    const StudioData::MixcutScriptStats stats =
        StudioData::mixcutScriptStats(m_project.script);
    const bool hasScript = stats.chars > 0;
    m_voiceOrb->setVoice(voice);
    if (m_voiceName) {
        m_voiceName->setText(voice.name);
    }
    if (m_voiceRecommendTag) {
        m_voiceRecommendTag->setVisible(voice.recommended);
    }
    if (m_voiceScene) {
        m_voiceScene->setText(voice.scene);
    }
    if (m_voiceLanguage) {
        m_voiceLanguage->setText(
            QStringLiteral("%1 · %2").arg(voice.language, voice.gender));
    }
    if (m_voiceMeta) {
        m_voiceMeta->setText(hasScript
                                 ? QStringLiteral("%1 · 预计 %2")
                                       .arg(voice.name, stats.durationLabel)
                                 : QStringLiteral("40 种豆包音色"));
        m_voiceMeta->setVisible(true);
    }
    if (m_voiceSampleButton) {
        const bool playing = m_sampleVoiceId == voice.id;
        m_voiceSampleButton->setLeadingGlyph(playing ? Icons::Glyph::Pause
                                                     : Icons::Glyph::Play);
        m_voiceSampleButton->setToolTip(
            playing ? QStringLiteral("停止试听示例音")
                    : QStringLiteral("播放示例音"));
    }
    if (m_voiceSynthesisHint) {
        m_voiceSynthesisHint->setText(
            hasScript
                ? QStringLiteral("seed-tts-2.0 · %1 字 · 预计 %2")
                      .arg(stats.chars)
                      .arg(stats.durationLabel)
                : (scriptJobRunning()
                       ? QStringLiteral("文案生成后用 seed-tts-2.0 自动合成配音")
                       : QStringLiteral("生成文案并选好音色后，一键成片时自动合成配音")));
        m_voiceSynthesisHint->setToolTip(
            QStringLiteral("一键成片时调用语音网关 seed-tts-2.0 生成配音，"
                           "按合成字符数计费（2.8 元/万字符，本次约 %1 元）；"
                           "示例音仅用于试听。")
                .arg(stats.chars * 2.8 / 10000.0, 0, 'f', 2));
    }
}

void MixcutPage::refreshFooter()
{
    if (!m_footerSummary || !m_footerHint) {
        return;
    }
    const bool job = scriptJobRunning();
    const bool hasScript = !m_project.script.trimmed().isEmpty();
    const StudioData::MixcutScriptStats stats =
        StudioData::mixcutScriptStats(m_project.script);
    const StudioData::MixcutMaterialStats materialStats =
        StudioData::mixcutMaterialStats(m_project.materials);

    if (!hasMaterials()) {
        m_footerSummary->setText(QStringLiteral("等待上传素材"));
        m_footerHint->setText(
            QStringLiteral("支持视频与图片素材 · 多选上传 · 视频建议 15 秒–3 分钟"));
        return;
    }
    if (job) {
        m_footerSummary->setText(QStringLiteral("AI 正在按提示词撰写文案…"));
        m_footerHint->setText(QStringLiteral("文案生成后可手动微调，再选择音色一键成片"));
        return;
    }
    if (!hasScript) {
        m_footerSummary->setText(
            QStringLiteral("文案待生成 · 填写提示词后点击「生成文案」"));
        m_footerHint->setText(QStringLiteral("也可以直接在文案框中手动撰写"));
        return;
    }
    if (canStart()) {
        m_footerSummary->setText(
            QStringLiteral("预计成片 %1 · %2 个镜头 · %3 · 成片 + 剪映草稿")
                .arg(stats.durationLabel)
                .arg(materialStats.shots)
                .arg(m_project.ratio));
        m_footerHint->setText(
            QStringLiteral("将自动生成「%1」配音 · 字幕、音效与 BGM 自动配置")
                .arg(StudioData::getMixcutVoice(m_project.voiceId).name));
    } else {
        m_footerSummary->setText(QStringLiteral("生成文案后即可一键成片"));
        m_footerHint->setText(QStringLiteral("请稍候，正在准备成片"));
    }
}

QString MixcutPage::headerDescription() const
{
    if (!hasMaterials()) {
        return QStringLiteral("上传视频或图片素材，填写文案提示词生成口播，一键成片时自动配音并完成混剪包装。");
    }
    if (scriptJobRunning()) {
        return QStringLiteral("AI 正在按提示词撰写口播文案，生成后可手动微调。");
    }
    if (!m_project.script.trimmed().isEmpty()) {
        return QStringLiteral("文案已就绪，选择音色后点击「一键成片」，AI 会自动生成配音与成片。");
    }
    return QStringLiteral("素材已就绪，填写文案提示词并点击「生成文案」，也可以直接撰写口播文案。");
}

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
            m_settingText = true;
            m_promptEdit->setPlainText(m_project.prompt);
            m_settingText = false;
        }
        refreshStudioState();
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


void MixcutPage::startGeneration()
{
    if (!canStart()) {
        return;
    }
    // 示例素材没有本地文件，无法进入真实渲染；统一要求用户上传真实素材。
    QVector<StudioData::MixcutMaterial> materials;
    for (const StudioData::MixcutMaterial &material : m_project.materials) {
        if (material.sample) {
            emit toastRequested(QStringLiteral(
                "示例素材仅用于界面预览，请上传本地视频或图片后再一键成片"));
            return;
        }
        if (material.filePath.trimmed().isEmpty()
            || !QFileInfo::exists(material.filePath)) {
            emit toastRequested(
                QStringLiteral("素材「%1」文件不存在，请重新上传")
                    .arg(material.name));
            return;
        }
        materials.append(material);
    }
    if (materials.isEmpty()) {
        emit toastRequested(QStringLiteral("请先上传视频或图片素材"));
        return;
    }

    const AppConfig config = m_config ? m_config->config() : AppConfig();
    const QString apiKey = config.service.apiKey.trimmed();
    if (apiKey.isEmpty()) {
        emit toastRequested(QStringLiteral("请先在设置中填写 API Key，再开始混剪"));
        emit navigateRequested(QStringLiteral("settings"));
        return;
    }

    m_scriptTimer->stop();
    m_playTimer->stop();
    stopVoiceSample();
    m_playing = false;
    m_playProgress = 0;
    m_project.materials = materials;

    const QString movies = QStandardPaths::writableLocation(
        QStandardPaths::MoviesLocation);
    const QString baseDir = movies.isEmpty() ? QDir::homePath() : movies;
    const QString outputDir =
        QDir(baseDir).filePath(QStringLiteral("LiveMix/混剪成片"));
    const QString draftRoot =
        QDir(baseDir).filePath(QStringLiteral("LiveMix/CapDraft"));
    QString cacheDir = QStandardPaths::writableLocation(
        QStandardPaths::CacheLocation);
    if (cacheDir.isEmpty()) {
        cacheDir = QDir::tempPath();
    }
    cacheDir = QDir(cacheDir).filePath(QStringLiteral("mixcut"));

    Mixcut::PipelineRequest request;
    request.runId = m_runId + 1;
    request.apiKey = apiKey;
    request.gatewayBaseUrl = config.service.apiUrl;
    request.project = m_project;
    request.cacheDir = cacheDir;
    request.outputDir = outputDir;
    request.draftRoot = draftRoot;
    request.transitions = config.finish.transitions;
    request.effects = config.finish.effects;
    request.filters = config.finish.filters;
    request.bgm = config.finish.bgm;
    request.sfx = config.finish.sfx;

    destroyPipeline();
    buildProcessingScreen();
    m_runId = request.runId;
    m_run = StudioData::MixcutRun();
    m_run.runId = m_runId;
    m_run.stage = StudioData::MixcutStage::Parse;
    const QStringList labels = {
        QStringLiteral("素材整理"), QStringLiteral("语音理解"),
        QStringLiteral("镜头规划"), QStringLiteral("智能包装"),
        QStringLiteral("时间轴"), QStringLiteral("合成草稿")};
    const QVector<StudioData::MixcutStage> keys = {
        StudioData::MixcutStage::Parse, StudioData::MixcutStage::Voice,
        StudioData::MixcutStage::Match, StudioData::MixcutStage::Design,
        StudioData::MixcutStage::Timeline, StudioData::MixcutStage::Render};
    for (int index = 0; index < keys.size(); ++index) {
        StudioData::MixcutStageState stage;
        stage.key = keys.at(index);
        stage.label = labels.at(index);
        stage.detail = QStringLiteral("等待开始");
        m_run.stages.append(stage);
    }
    m_hasRun = true;
    m_pipelineRunning = true;
    m_logCount = -1;
    m_lastLogId.clear();
    m_lastScenesCount = -1;
    updateProcessing();
    showProcessing();
    emit taskStatusChanged(QStringLiteral("智能混剪中 0% · 00:00"), true);

    auto *service = new Mixcut::PipelineService(request);
    auto *thread = new QThread;
    service->moveToThread(thread);
    connect(thread, &QThread::started, service, &Mixcut::PipelineService::run);
    // 捕获 service 实例：取消旧项目后，旧线程晚到的信号必须被丢弃。
    connect(service, &Mixcut::PipelineService::runUpdated, this,
            [this, service, runId = request.runId](
                const StudioData::MixcutRun &run) {
                if (service == m_pipelineService && runId == m_runId) {
                    onPipelineRunUpdated(run);
                }
            });
    connect(service, &Mixcut::PipelineService::finished, this,
            [this, service, runId = request.runId](
                bool success, const QString &message,
                const StudioData::MixcutResult &result) {
                if (service == m_pipelineService && runId == m_runId) {
                    onPipelineFinished(success, message, result);
                }
            });
    connect(thread, &QThread::finished, service, &QObject::deleteLater);
    connect(thread, &QThread::finished, thread, &QObject::deleteLater);
    m_pipelineService = service;
    m_pipelineThread = thread;
    thread->start();
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
    const StudioData::MixcutSource source =
        StudioData::mixcutSourceFromMaterials(m_project.materials);

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
    titleLine->addWidget(Ui::label(QStringLiteral("AI 正在混剪你的素材"), 16,
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
    for (int index = 0; index < 6; ++index) {
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
    const int previewHeight = portrait ? 380 : 206;
    const int previewWidth = portrait ? qRound(previewHeight * 9.0 / 16.0)
                                      : qRound(previewHeight * 16.0 / 9.0);
    m_processingPoster = new Studio::MixPosterWidget(stageHost);
    m_processingPoster->setFixedSize(previewWidth, previewHeight);
    Studio::MixPosterSpec spec;
    spec.scene = source.scenes.value(0);
    spec.title = meta.captionTitle;
    spec.caption = QStringLiteral("AI 正在拆解文案与画面节奏…");
    spec.voiceName = QStringLiteral("%1 · %2").arg(voice.name, voice.label);
    spec.secondaryScenes = source.scenes.mid(1, 2);
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
    infoLayout->addWidget(makeInfoPanel(QStringLiteral("生成配音"), Icons::Glyph::Mic,
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
                                            .arg(source.shots),
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
        QStringLiteral("%1 · %2").arg(source.name, source.duration), sourceRow);
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
    m_logStepsLabel = monoLabel(QStringLiteral("0/6 步"), 9, Theme::Text3,
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
        Icons::Glyph::Image, Icons::Glyph::Mic, Icons::Glyph::Film,
        Icons::Glyph::Wand, Icons::Glyph::Doc, Icons::Glyph::Sparkle,
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
    const StudioData::MixcutSource source =
        StudioData::mixcutSourceFromMaterials(m_project.materials);
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
    double voiceProgress = m_run.voiceProgress;
    if (voiceProgress <= 0.0) {
        for (const StudioData::MixcutStageState &stage : m_run.stages) {
            if (stage.key == StudioData::MixcutStage::Voice) {
                voiceProgress = stage.status == StudioData::StageStatus::Done
                    ? 100.0
                    : stage.progress;
                break;
            }
        }
    }
    if (m_processingVoiceLabel) {
        m_processingVoiceLabel->setText(
            QStringLiteral("%1%").arg(qRound(voiceProgress)));
    }
    if (m_processingVoiceWave) {
        m_processingVoiceWave->setProgress(qRound(voiceProgress));
        m_processingVoiceWave->setAnimated(
            m_run.stage == StudioData::MixcutStage::Voice);
    }
    const int totalShots = qMax(1, m_run.clipCount > 0 ? m_run.clipCount
                                                       : source.shots);
    if (m_processingScenesCount) {
        const bool planningDone =
            m_run.stage == StudioData::MixcutStage::Match
            || m_run.stage == StudioData::MixcutStage::Design
            || m_run.stage == StudioData::MixcutStage::Timeline
            || m_run.stage == StudioData::MixcutStage::Render;
        m_processingScenesCount->setText(
            planningDone
                ? QStringLiteral("%1 个镜头").arg(m_run.matchedShots)
                : QStringLiteral("%1/%2").arg(m_run.matchedShots).arg(totalShots));
    }
    const int sceneCount = qMin(
        source.scenes.size(),
        qMax(0, static_cast<int>(std::ceil(
                     m_run.matchedShots * 1.0 / totalShots * 3))));
    if (m_processingScenesLayout && sceneCount != m_lastScenesCount) {
        m_lastScenesCount = sceneCount;
        Studio::clearLayout(m_processingScenesLayout);
        if (sceneCount == 0) {
            m_processingScenesLayout->addWidget(
                Ui::label(QStringLiteral("等待配音完成"), 8, QFont::Normal,
                          Theme::Text3, m_processingScenesLayout->parentWidget()));
        } else {
            for (const QString &scene : source.scenes.mid(0, sceneCount)) {
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
        } else if (m_run.stage == StudioData::MixcutStage::Timeline) {
            m_processingSourceMeta->setText(
                QStringLiteral("%1 个镜头").arg(m_run.matchedShots));
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

    const QString lastLogId =
        m_run.logs.isEmpty() ? QString() : m_run.logs.last().id;
    if (m_logLayout
        && (m_run.logs.size() != m_logCount || lastLogId != m_lastLogId)) {
        m_logCount = m_run.logs.size();
        m_lastLogId = lastLogId;
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

void MixcutPage::onPipelineRunUpdated(const StudioData::MixcutRun &run)
{
    if (!m_hasRun || run.runId != m_runId) {
        return;
    }
    m_run = run;
    updateProcessing();
    const int percent =
        qMin(99, static_cast<int>(std::floor(m_run.progress)));
    emit taskStatusChanged(
        QStringLiteral("智能混剪中 %1% · %2")
            .arg(percent)
            .arg(StudioData::formatElapsed(m_run.elapsedMs)),
        true);
}

void MixcutPage::onPipelineFinished(bool success, const QString &message,
                                    const StudioData::MixcutResult &result)
{
    // 取消旧项目后旧线程可能晚到；只接受当前服务实例的结果。
    if (!m_pipelineRunning) {
        return;
    }
    if (sender() && m_pipelineService && sender() != m_pipelineService) {
        return;
    }
    m_hasRun = false;
    m_pipelineRunning = false;
    m_pipelineService = nullptr;
    if (m_pipelineThread) {
        m_pipelineThread->quit();
        m_pipelineThread = nullptr;
    }
    emit taskStatusChanged(QString(), false);

    if (!success) {
        m_run.done = true;
        updateProcessing();
        QMessageBox::warning(this, QStringLiteral("智能混剪生成失败"), message);
        emit toastRequested(QStringLiteral("智能混剪生成失败：%1").arg(message));
        showStudio();
        return;
    }

    m_run.done = true;
    m_run.progress = 100.0;
    updateProcessing();
    m_result = result;
    m_hasResult = true;
    m_resultFilePath = result.filePath;
    m_resultDraftPath = result.draftPath;
    // 真实生成记录持久化。
    m_history.prepend(m_result);
    m_persistedHistory.prepend(m_result);
    saveHistory();
    showResult();
    if (!isVisible()) {
        emit toastRequested(QStringLiteral(
            "智能混剪成片已生成并自动下载，可预览查看"));
    } else {
        emit toastRequested(message);
    }
}

void MixcutPage::destroyPipeline()
{
    if (m_pipelineService) {
        m_pipelineService->cancel();
        // 旧线程可能因为阻塞网络请求晚到信号；先断开与界面的连接。
        disconnect(m_pipelineService, nullptr, this, nullptr);
    }
    if (m_pipelineThread) {
        m_pipelineThread->quit();
        // 阻塞网络请求无法立即中断；最多等 3 秒，之后让线程自行结束并回收。
        if (!m_pipelineThread->wait(3000)) {
            disconnect(m_pipelineThread, nullptr, this, nullptr);
        }
    }
    m_pipelineService = nullptr;
    m_pipelineThread = nullptr;
    m_pipelineRunning = false;
}

namespace {

QString mixcutHistoryFilePath()
{
    QString dir = QStandardPaths::writableLocation(QStandardPaths::AppDataLocation);
    if (dir.isEmpty()) {
        dir = QDir::homePath() + QStringLiteral("/.livemix");
    }
    return QDir(dir).filePath(QStringLiteral("mixcut_history.json"));
}

} // namespace

void MixcutPage::loadHistory()
{
    m_persistedHistory.clear();
    QFile file(mixcutHistoryFilePath());
    if (!file.exists() || !file.open(QIODevice::ReadOnly)) {
        m_history.clear();
        return;
    }
    const QJsonDocument document = QJsonDocument::fromJson(file.readAll());
    QJsonArray results;
    if (document.isArray()) {
        results = document.array();
    } else if (document.isObject()) {
        results = document.object().value(QStringLiteral("results")).toArray();
    }
    for (const QJsonValue &value : results) {
        const StudioData::MixcutResult result =
            Mixcut::mixcutResultFromJson(value.toObject());
        // 只保留有真实成片路径的记录，避免把损坏的历史当成可播放结果。
        if (!result.id.isEmpty() && !result.filePath.isEmpty()
            && QFileInfo::exists(result.filePath)) {
            m_persistedHistory.append(result);
        }
    }
    m_history = m_persistedHistory;
}

void MixcutPage::saveHistory()
{
    while (m_persistedHistory.size() > 30) {
        m_persistedHistory.removeLast();
    }
    QString dir = QFileInfo(mixcutHistoryFilePath()).absolutePath();
    if (!QDir().mkpath(dir)) {
        return;
    }
    QJsonArray results;
    for (const StudioData::MixcutResult &result : m_persistedHistory) {
        results.append(Mixcut::mixcutResultToJson(result));
    }
    QJsonObject root;
    root.insert(QStringLiteral("version"), 1);
    root.insert(QStringLiteral("results"), results);
    QSaveFile file(mixcutHistoryFilePath());
    if (!file.open(QIODevice::WriteOnly)) {
        return;
    }
    file.write(QJsonDocument(root).toJson(QJsonDocument::Indented));
    file.commit();
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
    const QString baseDir = movies.isEmpty() ? QDir::homePath() : movies;
    if (!m_result.filePath.trimmed().isEmpty()) {
        m_resultFilePath = m_result.filePath;
    } else {
        m_resultFilePath = QDir(baseDir).filePath(
            QStringLiteral("LiveMix/混剪成片/%1.mp4").arg(fileName));
    }
    if (!m_result.draftPath.trimmed().isEmpty()) {
        m_resultDraftPath = m_result.draftPath;
    } else {
        m_resultDraftPath = QDir(baseDir).filePath(
            QStringLiteral("LiveMix/CapDraft/%1_%2").arg(fileName, m_result.id));
    }
    if (!m_resultFilePath.isEmpty()) {
        fileName = QFileInfo(m_resultFilePath).completeBaseName();
    }

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
        new Chip(m_result.draftGenerated ? QStringLiteral("已生成并自动导入剪映")
                                         : QStringLiteral("成片已生成"),
                 Chip::Ok, header));
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

    auto *backToHub = new Button(QStringLiteral("返回"), Button::Ghost, header);
    backToHub->setLeadingGlyph(Icons::Glyph::ChevronLeft);
    backToHub->setToolTip(QStringLiteral("返回智能混剪"));
    connect(backToHub, &QAbstractButton::clicked, this, &MixcutPage::showHub);
    titleLayout->addWidget(backToHub);
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
    const int posterHeight = portrait ? 494 : 242;
    const int posterWidth = portrait ? qRound(posterHeight * 9.0 / 16.0)
                                     : qRound(posterHeight * 16.0 / 9.0);
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
    // 生成完成后用真实成片首帧替换设计稿插画。
    if (!m_result.previewPath.isEmpty()) {
        const QImage frame(m_result.previewPath);
        if (!frame.isNull()) {
            spec.frameImage = frame;
            spec.title.clear();
            spec.caption.clear();
            spec.highlight.clear();
            spec.badge = QStringLiteral("成片");
        }
    }
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
        const QString dir = QFileInfo(m_resultFilePath).absolutePath();
        if (!QDesktopServices::openUrl(QUrl::fromLocalFile(dir))) {
            emit toastRequested(QStringLiteral("无法打开成片目录：%1").arg(dir));
        }
    });
    outputBody->addWidget(makeFileRow(
        Icons::Glyph::Film, QFileInfo(m_resultFilePath).fileName(),
        QStringLiteral("MP4"),
        QStringLiteral("%1 · %2 · %3 · 30fps")
            .arg(StudioData::mixcutRatioDimensions(m_result.ratio),
                 m_result.fileSize,
                 m_result.videoCodec.isEmpty() ? QStringLiteral("H.264")
                                               : m_result.videoCodec),
        openFile, output));
    auto *openDraft = new Button(QStringLiteral("打开草稿"), Button::Ghost, output);
    openDraft->setCompact(true);
    openDraft->setLeadingGlyph(Icons::Glyph::Folder);
    openDraft->setTextColor(Theme::Success);
    connect(openDraft, &QAbstractButton::clicked, this, [this] {
        const QString dir = m_result.draftPath.trimmed().isEmpty()
            ? QFileInfo(m_resultDraftPath).absolutePath()
            : m_result.draftPath;
        if (!QDesktopServices::openUrl(QUrl::fromLocalFile(dir))) {
            emit toastRequested(QStringLiteral("无法打开剪映草稿目录：%1").arg(dir));
        }
    });
    outputBody->addWidget(makeFileRow(
        Icons::Glyph::Scissors,
        m_result.draftGenerated ? QFileInfo(m_resultDraftPath).fileName()
                                : QStringLiteral("剪映草稿"),
        QStringLiteral("草稿"),
        m_result.draftGenerated
            ? QStringLiteral("已写入剪映草稿目录，可继续修改文案、配音与镜头")
            : QStringLiteral("本次草稿下载失败，可稍后在剪映小助手中重新导出"),
        openDraft, output));
    openDraft->setEnabled(m_result.draftGenerated);
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
        QStringLiteral("想换个表达？修改文案或更换音色后重新混剪，素材无需重复上传。"),
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
    if (!result.filePath.isEmpty() && !QFileInfo::exists(result.filePath)) {
        emit toastRequested(QStringLiteral("成片文件已被移动或删除：%1")
                                .arg(result.filePath));
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
    m_scriptTimer->stop();
    m_playTimer->stop();
    stopVoiceSample();
    m_playing = false;
    m_playProgress = 0;
    if (!m_result.materials.isEmpty()) {
        m_project.materials = m_result.materials;
    } else if (m_project.materials.isEmpty()) {
        m_project.materials = StudioData::mixcutDemoMaterials();
    }
    m_project.analysisReady = true;
    m_project.prompt = m_result.scriptPrompt;
    m_project.script = m_result.script;
    m_project.style = m_result.style;
    m_project.voiceId = m_result.voiceId;
    m_project.ratio = m_result.ratio;
    m_analysisProgress = 100;
    m_scriptEdited = false;
    m_scriptRewrite = false;
    showStudio();
}

void MixcutPage::togglePlayback()
{
    if (!m_resultPoster) {
        return;
    }
    if (m_playProgress >= 100) {
        m_playProgress = 0;
    }
    m_playing = !m_playing;
    if (m_playing) {
        // 页面内播放的是过程示意；成片用系统播放器打开，避免再内嵌一套解码器。
        if (!m_resultFilePath.isEmpty() && QFileInfo::exists(m_resultFilePath)) {
            if (!QDesktopServices::openUrl(
                    QUrl::fromLocalFile(m_resultFilePath))) {
                emit toastRequested(QStringLiteral("无法打开成片文件：%1")
                                        .arg(m_resultFilePath));
            }
        }
        m_playTimer->start();
    } else {
        m_playTimer->stop();
    }
    setPlayIcon(m_resultCenterPlay, m_playing);
    if (m_resultPlayButton) {
        m_resultPlayButton->setText(m_playing ? QStringLiteral("暂停播放")
                                              : QStringLiteral("播放成片"));
        m_resultPlayButton->setLeadingGlyph(m_playing ? Icons::Glyph::Pause
                                                      : Icons::Glyph::Play);
    }
    updatePlayback();
}

void MixcutPage::updatePlayback()
{
    if (!m_resultPoster) {
        return;
    }
    if (m_playing) {
        m_playProgress = qMin(100, m_playProgress + 1);
        if (m_playProgress >= 100) {
            m_playing = false;
            m_playTimer->stop();
            setPlayIcon(m_resultCenterPlay, false);
            if (m_resultPlayButton) {
                m_resultPlayButton->setText(QStringLiteral("播放成片"));
                m_resultPlayButton->setLeadingGlyph(Icons::Glyph::Play);
            }
        }
    }

    const QStringList lines = StudioData::mixcutScriptLines(m_result.script);
    const int totalSeconds = qMax(1, StudioData::parseElapsed(m_result.durationLabel));
    const int currentSeconds =
        qRound(totalSeconds * (m_playProgress / 100.0));
    const QString currentClock = StudioData::formatElapsed(
        static_cast<qint64>(currentSeconds) * 1000);
    const int lineIndex = lines.isEmpty()
        ? 0
        : qMin(lines.size() - 1,
               static_cast<int>(std::floor(m_playProgress / 100.0 * lines.size())));
    const QString activeLine =
        lines.isEmpty() ? m_result.captionTitle : lines.at(lineIndex);
    const StudioData::MixcutStyleMeta &meta =
        StudioData::mixcutStyleMeta(m_result.style);
    QString highlight;
    for (const QString &keyword : meta.keywords) {
        if (activeLine.contains(keyword)) {
            highlight = keyword;
            break;
        }
    }
    Studio::MixPosterSpec spec = m_resultPoster->spec();
    spec.caption = activeLine;
    spec.highlight = highlight;
    spec.timecode = (m_playing || m_playProgress > 0) ? currentClock
                                                      : m_result.durationLabel;
    spec.progress = m_playProgress;
    m_resultPoster->setSpec(spec);
    if (m_resultPlayBar) {
        m_resultPlayBar->setValue(m_playProgress);
    }
    if (m_resultPlayClock) {
        m_resultPlayClock->setText(currentClock);
    }
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
