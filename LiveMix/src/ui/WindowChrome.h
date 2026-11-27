#pragma once

#include "Controls.h"

#include <QHash>
#include <QWidget>

class QContextMenuEvent;
class QHBoxLayout;
class QLabel;
class QScrollArea;
class QVBoxLayout;
class WindowCaptionButton;
class WindowsTitleText;

class StatusDot : public QWidget
{
    Q_OBJECT

public:
    explicit StatusDot(int diameter = 6, const QColor &color = Theme::Success,
                       QWidget *parent = nullptr);

    void setColor(const QColor &color);

protected:
    void paintEvent(QPaintEvent *event) override;

private:
    QColor m_color;
    bool m_glow;
};

class Page : public QWidget
{
    Q_OBJECT

public:
    explicit Page(QWidget *parent = nullptr);

    QVBoxLayout *bodyLayout() const { return m_bodyLayout; }
    QScrollArea *scrollArea() const { return m_scrollArea; }
    void scrollToTop();

    // 把页头移到滚动区域之外，固定在页面顶部；header 不要同时加进 bodyLayout。
    void setFixedHeader(QWidget *header);
    // 关闭页面级滚动：内容区直接铺满页面剩余高度，由页面内部自行滚动
    // （设置页的卡片填满窗口，只有右侧面板内容滚动）。
    void disablePageScroll();

protected:
    QWidget *contentWidget() const { return m_content; }

private:
    QVBoxLayout *m_rootLayout = nullptr;
    QScrollArea *m_scrollArea = nullptr;
    QWidget *m_content = nullptr;
    QVBoxLayout *m_bodyLayout = nullptr;
};

class PageHeader : public QWidget
{
    Q_OBJECT

public:
    PageHeader(const QString &title, const QString &descriptionHtml = QString(),
               QWidget *parent = nullptr);

    void setTitle(const QString &title);
    // 标题右侧的小标记（如成片生成时间）；传入的对象归页头所有。
    void setTitleMeta(QWidget *widget);
    void setDescription(const QString &descriptionHtml);
    QHBoxLayout *actionsLayout() const { return m_actionsLayout; }
    void addAction(QWidget *widget);

private:
    QLabel *m_title = nullptr;
    QHBoxLayout *m_titleRow = nullptr;
    QLabel *m_description = nullptr;
    QHBoxLayout *m_actionsLayout = nullptr;
};

class TitleBar : public QWidget
{
    Q_OBJECT

public:
    explicit TitleBar(QWidget *parent = nullptr);

    // Windows 用文档名（页面名 - LiveMix Studio），macOS 用品牌后的后缀。
    void setDocumentTitle(const QString &documentName, const QString &macSuffix);
    void setRounded(bool rounded);
    // Windows 窗口控件在最大化与还原之间切换图标。
    void setMaximized(bool maximized);
    // Windows 系统菜单：应用图标、右键标题栏或 Alt+Space 打开。
    void showSystemMenuAtIcon();

signals:
    void closeRequested();
    void minimizeRequested();
    void maximizeRequested();
    void addSourceRequested();
    void searchRequested();

protected:
    void paintEvent(QPaintEvent *event) override;
    void contextMenuEvent(QContextMenuEvent *event) override;
    void mousePressEvent(QMouseEvent *event) override;
    void mouseDoubleClickEvent(QMouseEvent *event) override;
    void mouseMoveEvent(QMouseEvent *event) override;
    void mouseReleaseEvent(QMouseEvent *event) override;

private:
    void buildMacChrome(QHBoxLayout *root);
    void buildWindowsChrome(QHBoxLayout *root);
    void openSystemMenu(const QPoint &globalPos);

    QLabel *m_project = nullptr;
    WindowsTitleText *m_pageTitle = nullptr;
    WindowCaptionButton *m_maximizeButton = nullptr;
    QWidget *m_iconButton = nullptr;
    QPoint m_dragOffset;
    bool m_rounded = true;
    bool m_windows = false;
    bool m_maximized = false;
    bool m_dragging = false;
};

class Sidebar : public QWidget
{
    Q_OBJECT

public:
    explicit Sidebar(QWidget *parent = nullptr);

    void setActivePage(const QString &pageId);
    void setBadge(const QString &pageId, const QString &badge);
    // 侧栏只展示真实状态：本地数据库是否可用。
    void setDatabaseReady(bool ready, const QString &error = QString());

signals:
    void pageRequested(const QString &pageId);

protected:
    void paintEvent(QPaintEvent *event) override;

private:
    QHash<QString, NavItem *> m_items;
    StatusDot *m_databaseDot = nullptr;
    QLabel *m_databaseLabel = nullptr;
};

class StatusBar : public QWidget
{
    Q_OBJECT

public:
    explicit StatusBar(QWidget *parent = nullptr);

    void setRounded(bool rounded);
    // 草稿下载进度入口；点击后打开对应草稿的下载弹窗。
    void setDraftDownloadStatus(const QString &text, bool error, qint64 draftRowId);
    void clearDraftDownloadStatus();
    // 批量导出草稿的进度入口；点击后打开批量导出弹窗。
    void setDraftBatchStatus(const QString &text, bool error);
    void clearDraftBatchStatus();
    // 成片导出进度入口；draftRowId < 0 表示批量任务。
    void setVideoExportStatus(const QString &text, bool error, qint64 draftRowId);
    void clearVideoExportStatus();
    // 真实的运行状态；未配置/未找到时明确展示，而不是固定假数据。
    void setServiceStatus(const QString &text, bool ok, const QString &tooltip = QString());
    void setQueueCount(int count);
    // 智能口播 / 智能混剪的后台任务入口，点击回到对应工作区。
    void setOralTaskStatus(const QString &text);
    void clearOralTaskStatus();
    void setMixcutTaskStatus(const QString &text);
    void clearMixcutTaskStatus();
    void setDraftCount(int count);
    void setFfmpegAvailable(bool available);

static constexpr qint64 kBatchIndicator = -2;

signals:
    void draftDownloadRequested(qint64 draftRowId);
    void draftBatchRequested();
    void videoExportRequested(qint64 draftRowId);
    void oralTaskRequested();
    void mixcutTaskRequested();

protected:
    void paintEvent(QPaintEvent *event) override;

private:
    bool m_rounded = true;
    StatusDot *m_serviceDot = nullptr;
    QLabel *m_serviceLabel = nullptr;
    QLabel *m_queueLabel = nullptr;
    QLabel *m_draftCountLabel = nullptr;
    QLabel *m_ffmpegLabel = nullptr;
    LinkLabel *m_oralStatus = nullptr;
    LinkLabel *m_mixcutStatus = nullptr;
    LinkLabel *m_draftStatus = nullptr;
    LinkLabel *m_videoStatus = nullptr;
    qint64 m_draftStatusDraftId = -1;
    qint64 m_videoStatusDraftId = -1;
};

class WindowShell : public QWidget
{
    Q_OBJECT

public:
    explicit WindowShell(QWidget *parent = nullptr);

    TitleBar *titleBar() const { return m_titleBar; }
    StatusBar *statusBar() const { return m_statusBar; }
    QHBoxLayout *workspaceLayout() const { return m_workspaceLayout; }
    void setRounded(bool rounded);
    bool isRounded() const { return m_rounded; }

protected:
    void paintEvent(QPaintEvent *event) override;

private:
    TitleBar *m_titleBar = nullptr;
    StatusBar *m_statusBar = nullptr;
    QHBoxLayout *m_workspaceLayout = nullptr;
    bool m_rounded = true;
    int m_radius = 12;
};
