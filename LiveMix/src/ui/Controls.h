#pragma once

#include "Icons.h"
#include "../app/ApplicationStyle.h"

#include <QAbstractButton>
#include <QColor>
#include <QComboBox>
#include <QFont>
#include <QImage>
#include <QLabel>
#include <QMargins>
#include <QSize>
#include <QStringList>
#include <QVector>
#include <QWidget>

class QBoxLayout;
class QGraphicsOpacityEffect;
class QHBoxLayout;
class QKeyEvent;
class QLabel;
class QLineEdit;
class QPropertyAnimation;
class QResizeEvent;
class QTimer;
class QVBoxLayout;
class QWheelEvent;

namespace Ui {

QLabel *label(const QString &text, int pixelSize = 13, int weight = QFont::Normal,
              const QColor &color = Theme::Text1, QWidget *parent = nullptr);
QLabel *richLabel(const QString &html, int pixelSize = 13,
                  const QColor &color = Theme::Text1, QWidget *parent = nullptr);
QVBoxLayout *vbox(QWidget *owner, const QMargins &margins = {}, int spacing = 0);
QHBoxLayout *hbox(QWidget *owner, const QMargins &margins = {}, int spacing = 0);
QLabel *separator(bool vertical = false, QWidget *parent = nullptr);

QString toneColorName(const QString &tone);

} // namespace Ui

class Card : public QWidget
{
    Q_OBJECT

public:
    explicit Card(QWidget *parent = nullptr, int radius = 8);
    Card(const QColor &fill, const QColor &border, int radius = 8,
         QWidget *parent = nullptr);

    void setCardColors(const QColor &fill, const QColor &border);
    void setRadius(int radius);
    void setHoverColors(const QColor &fill, const QColor &border);
    void setClickable(bool clickable);
    void setHoverEnabled(bool enabled);
    void setBorderWidth(qreal width);
    QVBoxLayout *contentLayout() const { return m_layout; }

signals:
    void clicked();

protected:
    void paintEvent(QPaintEvent *event) override;
    void enterEvent(QEnterEvent *event) override;
    void leaveEvent(QEvent *event) override;
    void mousePressEvent(QMouseEvent *event) override;
    void mouseReleaseEvent(QMouseEvent *event) override;

private:
    QVBoxLayout *m_layout = nullptr;
    int m_radius = 8;
    QColor m_fill;
    QColor m_border;
    qreal m_borderWidth = 1.0;
    bool m_clickable = false;
    bool m_hoverEnabled = false;
    bool m_hovered = false;
    bool m_pressed = false;
    QColor m_hoverFill;
    QColor m_hoverBorder;
};

class Button : public QAbstractButton
{
    Q_OBJECT

public:
    enum Variant {
        Default,
        Primary,
        Ai,
        Ghost,
        DangerSoft,
        Danger,
    };

    explicit Button(const QString &text, Variant variant = Default,
                    QWidget *parent = nullptr);

    void setLeadingGlyph(Icons::Glyph glyph);
    void setCompact(bool compact);
    void setVariant(Variant variant);
    Variant variant() const { return m_variant; }
    void setCornerRadius(int radius);
    // 在按钮左侧按百分比铺一层进度底色（0–100，负数表示不显示）。
    void setProgressPercent(int percent);
    // 覆盖文字与图标颜色（如成片已导出时的成功色）；无效颜色表示跟随变体。
    void setTextColor(const QColor &color);

    QSize sizeHint() const override;
    QSize minimumSizeHint() const override;

protected:
    void paintEvent(QPaintEvent *event) override;
    void enterEvent(QEnterEvent *event) override;
    void leaveEvent(QEvent *event) override;
    void mousePressEvent(QMouseEvent *event) override;
    void mouseReleaseEvent(QMouseEvent *event) override;

private:
    Variant m_variant;
    bool m_hasGlyph = false;
    Icons::Glyph m_glyph = Icons::Glyph::Plus;
    bool m_hovered = false;
    int m_fontSize = 12;
    int m_height = 32;
    int m_padding = 13;
    int m_radius = 7;
    int m_progressPercent = -1;
    QColor m_textColor;
};

class GlyphButton : public QAbstractButton
{
    Q_OBJECT

public:
    GlyphButton(Icons::Glyph glyph, int side = 18, QWidget *parent = nullptr);

    void setGlyph(Icons::Glyph glyph);
    void setColors(const QColor &normal, const QColor &hover);
    void setBackgroundColors(const QColor &normal, const QColor &hover);
    void setGlyphSize(qreal size);
    void setToolTipText(const QString &text);

    QSize sizeHint() const override;

protected:
    void paintEvent(QPaintEvent *event) override;
    void enterEvent(QEnterEvent *event) override;
    void leaveEvent(QEvent *event) override;

private:
    Icons::Glyph m_glyph;
    int m_side;
    qreal m_glyphSize;
    QColor m_normal;
    QColor m_hoverColor;
    QColor m_background;
    QColor m_hoverBackground;
    bool m_hovered = false;
};

class LinkLabel : public QAbstractButton
{
    Q_OBJECT

public:
    explicit LinkLabel(const QString &text, QWidget *parent = nullptr);

    void setTextColor(const QColor &color);
    void setPixelSize(int pixelSize);
    void setHoverColor(const QColor &color);
    void setDisabledVisual(bool disabled);
    void setMonoFont(bool mono);
    // 在文字左侧绘制图标（如 GitHub 开源仓库入口）。
    void setLeadingGlyph(Icons::Glyph glyph, int size = 10);
    // 在文字右侧绘制图标（如「获取密钥」后的外链角标）。
    void setTrailingGlyph(Icons::Glyph glyph, int size = 10);
    void setUnderlineOnHover(bool underline);
    QSize sizeHint() const override;

protected:
    void paintEvent(QPaintEvent *event) override;
    void enterEvent(QEnterEvent *event) override;
    void leaveEvent(QEvent *event) override;

private:
    QColor m_color = Theme::Primary;
    QColor m_hoverColor = Theme::PrimaryHover;
    int m_pixelSize = 12;
    bool m_disabled = false;
    bool m_hovered = false;
    bool m_mono = false;
    bool m_hasGlyph = false;
    Icons::Glyph m_glyph = Icons::Glyph::Github;
    int m_glyphSize = 10;
    bool m_hasTrailingGlyph = false;
    Icons::Glyph m_trailingGlyph = Icons::Glyph::ExternalLink;
    int m_trailingGlyphSize = 10;
    bool m_underlineOnHover = true;
};

class Chip : public QWidget
{
    Q_OBJECT

public:
    enum Tone { Neutral, Ok, Run, Ai, Error, Live };

    explicit Chip(const QString &text, Tone tone = Neutral, QWidget *parent = nullptr);

    void setText(const QString &text);
    void setTone(Tone tone);
    void setShowDot(bool showDot);
    void setBlinking(bool blinking);
    QSize sizeHint() const override;

protected:
    void paintEvent(QPaintEvent *event) override;

private:
    QString m_text;
    Tone m_tone;
    bool m_showDot = false;
    bool m_blinking = false;
    bool m_blinkOn = true;
    QTimer *m_blinkTimer = nullptr;
};

class MediaThumbnail : public QWidget
{
    Q_OBJECT

public:
    enum Mode { Cool, Recording };
    enum BadgeTone { BadgeOk, BadgeRun, BadgeError, BadgeNeutral };

    explicit MediaThumbnail(Mode mode = Cool, QWidget *parent = nullptr);

    void setDuration(const QString &duration);
    void setReadyBadge(const QString &text);
    void setReadyBadgeTone(BadgeTone tone);
    void setRecordingLabel(const QString &text);
    void setPreviewImage(const QImage &image);
    void setTopRadius(int radius);
    bool hasHeightForWidth() const override;
    int heightForWidth(int width) const override;
    QSize sizeHint() const override;
    QSize minimumSizeHint() const override;

protected:
    void paintEvent(QPaintEvent *event) override;
    void showEvent(QShowEvent *event) override;
    void hideEvent(QHideEvent *event) override;

private:
    Mode m_mode;
    QString m_duration;
    QString m_readyBadge;
    BadgeTone m_readyBadgeTone = BadgeOk;
    QString m_recordingLabel;
    QImage m_previewImage;
    int m_topRadius = 9;
    bool m_blinkOn = true;
    QTimer *m_blinkTimer = nullptr;
};

class ElidedLabel : public QWidget
{
    Q_OBJECT

public:
    explicit ElidedLabel(const QString &text = QString(), QWidget *parent = nullptr);

    void setText(const QString &text);
    QString text() const { return m_text; }
    void setTextColor(const QColor &color);
    void setPixelSize(int pixelSize);
    void setFontWeight(int weight);
    void setMonoFont(bool mono);
    QSize sizeHint() const override;
    QSize minimumSizeHint() const override;

protected:
    void paintEvent(QPaintEvent *event) override;

private:
    QFont labelFont() const;

    QString m_text;
    QColor m_color = Theme::Text1;
    int m_pixelSize = 12;
    int m_weight = QFont::Normal;
    bool m_mono = false;
};

class ProgressBar : public QWidget
{
    Q_OBJECT

public:
    explicit ProgressBar(QWidget *parent = nullptr);

    void setValue(int percent);
    int value() const { return m_value; }
    void setBarColor(const QColor &color);
    void setTrackColor(const QColor &color);
    QSize sizeHint() const override;

protected:
    void paintEvent(QPaintEvent *event) override;

private:
    int m_value = 0;
    QColor m_barColor = Theme::Primary;
    QColor m_trackColor = Theme::Surface3;
};

class NavItem : public QAbstractButton
{
    Q_OBJECT

public:
    NavItem(Icons::Glyph glyph, const QString &text, const QString &badge = QString(),
            QWidget *parent = nullptr);

    void setActive(bool active);
    bool isActive() const { return m_active; }
    void setBadge(const QString &badge);
    QSize sizeHint() const override;

protected:
    void paintEvent(QPaintEvent *event) override;
    void enterEvent(QEnterEvent *event) override;
    void leaveEvent(QEvent *event) override;

private:
    Icons::Glyph m_glyph;
    QString m_badge;
    bool m_active = false;
    bool m_hovered = false;
};

class CheckBox : public QAbstractButton
{
    Q_OBJECT

public:
    explicit CheckBox(const QString &text, QWidget *parent = nullptr);

    QSize sizeHint() const override;

protected:
    void paintEvent(QPaintEvent *event) override;
    void enterEvent(QEnterEvent *event) override;
    void leaveEvent(QEvent *event) override;

private:
    bool m_hovered = false;
};

class SegmentTabs : public QWidget
{
    Q_OBJECT

public:
    enum Style { SoftTabs, Segmented };

    explicit SegmentTabs(const QStringList &labels, Style style = SoftTabs,
                         QWidget *parent = nullptr);

    void setCurrentIndex(int index);
    int currentIndex() const { return m_current; }
    void setPixelSize(int pixelSize);
    QSize sizeHint() const override;

signals:
    void currentChanged(int index);

protected:
    void paintEvent(QPaintEvent *event) override;
    void mouseMoveEvent(QMouseEvent *event) override;
    void leaveEvent(QEvent *event) override;
    void mousePressEvent(QMouseEvent *event) override;

private:
    QRect tabRect(int index) const;
    int tabAt(const QPoint &point) const;

    QStringList m_labels;
    Style m_style;
    int m_current = 0;
    int m_pixelSize = 12;
    int m_hovered = -1;
};

// 单选胶囊按钮：设置页的倍速方式、一键成片弹窗的成片数量都使用它。
class ChoiceButton : public QAbstractButton
{
    Q_OBJECT

public:
    explicit ChoiceButton(const QString &text, QWidget *parent = nullptr);

    void setSelected(bool selected);
    bool isSelected() const { return m_selected; }
    QSize sizeHint() const override;

protected:
    void paintEvent(QPaintEvent *event) override;
    void enterEvent(QEnterEvent *event) override;
    void leaveEvent(QEvent *event) override;

private:
    bool m_selected = false;
    bool m_hovered = false;
};

// 成片设置里的开关。勾选状态由 QAbstractButton 的 checked 管理。
class Switch : public QAbstractButton
{
    Q_OBJECT
    Q_PROPERTY(qreal knob READ knob WRITE setKnob)

public:
    explicit Switch(QWidget *parent = nullptr);

    QSize sizeHint() const override;
    QSize minimumSizeHint() const override;
    qreal knob() const { return m_knob; }
    void setKnob(qreal knob);

protected:
    void paintEvent(QPaintEvent *event) override;

private:
    qreal m_knob = 0.0;
    class QVariantAnimation *m_animation = nullptr;
};

// 视频倍速滑杆：0.5×–2.0×，支持拖动、方向键与 Home/End。
class SliderControl : public QWidget
{
    Q_OBJECT

public:
    explicit SliderControl(QWidget *parent = nullptr);

    void setRange(double minimum, double maximum, double step);
    double value() const { return m_value; }
    void setValue(double value);
    QSize sizeHint() const override;

signals:
    void valueChanged(double value);

protected:
    void paintEvent(QPaintEvent *event) override;
    void mousePressEvent(QMouseEvent *event) override;
    void mouseMoveEvent(QMouseEvent *event) override;
    void mouseReleaseEvent(QMouseEvent *event) override;
    void keyPressEvent(QKeyEvent *event) override;
    void wheelEvent(QWheelEvent *event) override;

private:
    void setValueFromX(qreal x);
    QRectF trackRect() const;
    qreal ratio() const;

    double m_minimum = 0.5;
    double m_maximum = 2.0;
    double m_step = 0.05;
    double m_value = 1.0;
    bool m_dragging = false;
};

// 长任务的三段式阶段指示器（导出草稿 / 导出成片共用同样式）。
class StageStepper : public QWidget
{
    Q_OBJECT

public:
    enum State { Waiting, Active, Done, Error };

    explicit StageStepper(const QStringList &labels, QWidget *parent = nullptr);

    void setStates(const QVector<int> &states);
    QSize sizeHint() const override;

protected:
    void paintEvent(QPaintEvent *event) override;

private:
    QStringList m_labels;
    QVector<int> m_states;
};

class FormField : public QWidget
{
    Q_OBJECT

public:
    FormField(const QString &labelText, QWidget *content, const QString &hint = QString(),
              bool required = false, QWidget *parent = nullptr);

    QWidget *contentWidget() const { return m_content; }
    // 提示语按富文本渲染，链接可通过系统默认浏览器打开。
    void setHintRichText(bool enabled);
    // 标题行右侧的附加控件，如 API 密钥字段的「获取密钥」外链。
    void setLabelExtra(QWidget *widget);

protected:
    bool eventFilter(QObject *watched, QEvent *event) override;

private:
    // 富文本行高会超出 QLabel 自带的 wordWrap 高度计算，按文档实际高度补足。
    void adjustHintHeight();

    QWidget *m_content = nullptr;
    QLabel *m_hint = nullptr;
    QHBoxLayout *m_labelLayout = nullptr;
};

class SelectBox : public QComboBox
{
    Q_OBJECT

public:
    explicit SelectBox(QWidget *parent = nullptr);

    QSize sizeHint() const override;

protected:
    void paintEvent(QPaintEvent *event) override;
    void mousePressEvent(QMouseEvent *event) override;

private:
    void polishPopup();
};

class InputField : public QWidget
{
    Q_OBJECT

public:
    explicit InputField(QWidget *parent = nullptr);

    QLineEdit *lineEdit() const { return m_edit; }
    QHBoxLayout *rowLayout() const { return m_layout; }
    void setText(const QString &text);
    QString text() const;
    void setPlaceholder(const QString &text);
    void setPassword(bool password);
    void setReadOnly(bool readOnly);
    void setMonoFont(bool mono);

protected:
    void paintEvent(QPaintEvent *event) override;

private:
    QLineEdit *m_edit = nullptr;
    QHBoxLayout *m_layout = nullptr;
};

class Toast : public QWidget
{
    Q_OBJECT

public:
    explicit Toast(QWidget *parent = nullptr);

    void showMessage(const QString &text, int durationMs = 1800);
    void reposition();

protected:
    void paintEvent(QPaintEvent *event) override;

private:
    QString m_text;
    QGraphicsOpacityEffect *m_effect = nullptr;
    QPropertyAnimation *m_fadeAnimation = nullptr;
    QPropertyAnimation *m_slideAnimation = nullptr;
    QTimer *m_hideTimer = nullptr;
    int m_durationMs = 1800;
};
