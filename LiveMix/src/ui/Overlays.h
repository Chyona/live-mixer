#pragma once

#include "Controls.h"

#include <QPointer>
#include <QPixmap>
#include <QWidget>

class QHBoxLayout;
class QLabel;
class QShortcut;
class QTextEdit;
class QVBoxLayout;

class ModalCard : public QWidget
{
    Q_OBJECT

public:
    explicit ModalCard(const QString &title, int width, QWidget *parent = nullptr);

    QVBoxLayout *contentLayout() const { return m_contentLayout; }
    QHBoxLayout *footerLayout() const { return m_footerLayout; }
    void setContentSpacing(int spacing);
    void addFooterButton(QWidget *button);
    // 视频等原生渲染内容不需要父级阴影特效。
    void setShadowEnabled(bool enabled) { m_shadowEnabled = enabled; }
    bool shadowEnabled() const { return m_shadowEnabled; }

signals:
    void closeRequested();

protected:
    void paintEvent(QPaintEvent *event) override;

private:
    QVBoxLayout *m_contentLayout = nullptr;
    QHBoxLayout *m_footerLayout = nullptr;
    bool m_shadowEnabled = true;
};

class ModalOverlay : public QWidget
{
    Q_OBJECT

public:
    explicit ModalOverlay(QWidget *host);

    void showModal(ModalCard *card);
    void closeModal();
    // 弹窗内容动态变化（如显示视频预览）后，重新计算高度并居中。
    void relayout();
    bool isOpen() const { return !m_card.isNull() && isVisible(); }

signals:
    void closed();

protected:
    void paintEvent(QPaintEvent *event) override;
    void mousePressEvent(QMouseEvent *event) override;
    bool eventFilter(QObject *watched, QEvent *event) override;

private:
    void refreshBackdrop();
    void centerCard();
    void dismiss();

    QPointer<ModalCard> m_card;
    QPixmap m_backdrop;
    QShortcut *m_escape = nullptr;
};

class FullscreenEditor : public QWidget
{
    Q_OBJECT

public:
    explicit FullscreenEditor(QWidget *host);

    void openPrompt(const QString &name, const QString &note, const QString &content,
                    bool readOnly, bool creating = false);
    void closeEditor();

signals:
    void closeRequested();
    void saveRequested();

protected:
    void paintEvent(QPaintEvent *event) override;
    bool eventFilter(QObject *watched, QEvent *event) override;

private:
    InputField *m_nameEdit = nullptr;
    InputField *m_noteEdit = nullptr;
    QTextEdit *m_editor = nullptr;
    QLabel *m_countLabel = nullptr;
    QLabel *m_titleLabel = nullptr;
    Button *m_saveButton = nullptr;
    bool m_readOnly = false;
};

QWidget *makeShortcutBadge(const QString &text, QWidget *parent = nullptr);
