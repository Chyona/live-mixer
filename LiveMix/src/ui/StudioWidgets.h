#pragma once

// 智能口播 / 智能混剪共用的工作室界面基础组件。
// 视觉参数对应设计稿的 SectionCard、Segmented、StepRail、SwitchRow、Note 等。

#include "Controls.h"
#include "Icons.h"

#include <QColor>
#include <QRect>
#include <QVector>
#include <QWidget>

class QDragEnterEvent;
class QDragLeaveEvent;
class QDropEvent;
class QEnterEvent;
class QHBoxLayout;
class QKeyEvent;
class QLabel;
class QLayout;
class QMouseEvent;
class QTextEdit;
class QVBoxLayout;

namespace Studio {

QColor aiSoft();
QColor successSoft();
QColor primarySoft();
QColor infoSoft();

void clearLayout(QLayout *layout);
QLabel *iconLabel(Icons::Glyph glyph, int size, const QColor &color, QWidget *parent);
QLabel *tagLabel(const QString &text, const QColor &color, const QColor &background,
                 int pixelSize = 9, QWidget *parent = nullptr);
QLabel *pillLabel(const QString &text, const QColor &color, const QColor &background,
                  const QColor &border, int pixelSize = 9,
                  QWidget *parent = nullptr);
void applyTextAreaStyle(QTextEdit *edit, bool aiFocus = false, int pixelSize = 11);
QString formatClipPath(const QString &path, int maxChars = 48);

// 卡片：40px 标题栏 + 正文区，标题栏可带图标、meta 与右侧动作。
class SectionCard : public QWidget
{
    Q_OBJECT

public:
    explicit SectionCard(Icons::Glyph glyph, const QString &title,
                         QWidget *parent = nullptr);
    SectionCard(Icons::Glyph glyph, const QString &title, const QString &meta,
                QWidget *parent = nullptr);

    QVBoxLayout *bodyLayout() const { return m_body; }
    QHBoxLayout *headerLayout() const { return m_header; }
    void setMeta(const QString &meta);
    QLabel *metaLabel() const { return m_meta; }
    void setBodyMargins(const QMargins &margins);
    void setHeaderActionsSpacing(int spacing);

protected:
    void paintEvent(QPaintEvent *event) override;

private:
    QVBoxLayout *m_root = nullptr;
    QVBoxLayout *m_body = nullptr;
    QHBoxLayout *m_header = nullptr;
    QLabel *m_meta = nullptr;
};

class Note : public QWidget
{
public:
    enum Tone { Muted, Primary, Ai };

    explicit Note(const QString &html, Tone tone = Muted, QWidget *parent = nullptr);
    QHBoxLayout *rowLayout() const { return m_row; }

protected:
    void paintEvent(QPaintEvent *event) override;

private:
    QHBoxLayout *m_row = nullptr;
};

class SegmentedControl : public QWidget
{
    Q_OBJECT

public:
    explicit SegmentedControl(QWidget *parent = nullptr);

    void setOptions(const QStringList &labels, const QStringList &hints = QStringList());
    void setCurrentIndex(int index);
    int currentIndex() const { return m_current; }
    void setPixelSize(int pixelSize);
    void setCompact(bool compact);
    void setEnabled(bool enabled);
    bool isEnabled() const { return m_enabled; }
    QSize sizeHint() const override;

signals:
    void currentChanged(int index);

protected:
    void paintEvent(QPaintEvent *event) override;
    void mouseMoveEvent(QMouseEvent *event) override;
    void leaveEvent(QEvent *event) override;
    void mousePressEvent(QMouseEvent *event) override;
    void keyPressEvent(QKeyEvent *event) override;

private:
    QRect segmentRect(int index) const;
    int segmentAt(const QPoint &point) const;
    int preferredHeight() const;

    QStringList m_labels;
    QStringList m_hints;
    int m_current = 0;
    int m_hovered = -1;
    int m_pixelSize = 11;
    bool m_compact = false;
    bool m_enabled = true;
};

class StepRail : public QWidget
{
    Q_OBJECT

public:
    explicit StepRail(QWidget *parent = nullptr);

    void setSteps(const QStringList &labels, const QStringList &hints);
    void setActiveStep(int step);
    int activeStep() const { return m_active; }
    QSize sizeHint() const override;

signals:
    void stepSelected(int step);

protected:
    void paintEvent(QPaintEvent *event) override;
    void mousePressEvent(QMouseEvent *event) override;

private:
    QStringList m_labels;
    QStringList m_hints;
    int m_active = 1;
    QVector<QRect> m_hitAreas;
};

class SwitchRow : public QWidget
{
    Q_OBJECT

public:
    SwitchRow(const QString &title, const QString &description, bool checked,
              QWidget *parent = nullptr);

    bool isChecked() const;
    void setChecked(bool checked);

signals:
    void toggled(bool checked);

protected:
    void paintEvent(QPaintEvent *event) override;
    void mouseReleaseEvent(QMouseEvent *event) override;
    void enterEvent(QEnterEvent *event) override;
    void leaveEvent(QEvent *event) override;

private:
    Switch *m_switch = nullptr;
    bool m_hovered = false;
};

class DropZone : public QWidget
{
    Q_OBJECT

public:
    explicit DropZone(QWidget *parent = nullptr);

    QVBoxLayout *contentLayout() const { return m_layout; }
    void setDragging(bool dragging);

signals:
    void filesDropped(const QStringList &paths);

protected:
    void paintEvent(QPaintEvent *event) override;
    void dragEnterEvent(QDragEnterEvent *event) override;
    void dragLeaveEvent(QDragLeaveEvent *event) override;
    void dropEvent(QDropEvent *event) override;

private:
    QVBoxLayout *m_layout = nullptr;
    bool m_dragging = false;
};

class StatTile : public QWidget
{
public:
    StatTile(const QString &label, const QString &value, QWidget *parent = nullptr);

    void setValue(const QString &value);

protected:
    void paintEvent(QPaintEvent *event) override;

private:
    QLabel *m_value = nullptr;
};

class AiBadge : public QWidget
{
public:
    explicit AiBadge(QWidget *parent = nullptr);
    QSize sizeHint() const override;

protected:
    void paintEvent(QPaintEvent *event) override;
};

class StageSummaryCard : public QWidget
{
public:
    explicit StageSummaryCard(QWidget *parent = nullptr);

    void setData(int number, Icons::Glyph glyph, const QString &label, int status,
                 const QString &statusText, const QString &detail, int progress,
                 bool active);

protected:
    void paintEvent(QPaintEvent *event) override;

private:
    int m_number = 0;
    Icons::Glyph m_glyph = Icons::Glyph::Sparkle;
    QString m_label;
    int m_status = 0;
    QString m_statusText;
    QString m_detail;
    int m_progress = 0;
    bool m_active = false;
};

} // namespace Studio
