#pragma once

// 文件作用：工作台首页，汇总 SQLite 中的真实素材、草稿与任务数据。

#include "../data/ClipTypes.h"
#include "../data/MediaTypes.h"
#include "../ui/WindowChrome.h"

#include <QHash>
#include <QVector>

class FlowNode;
class QVBoxLayout;
class StatCard;

class DashboardPage : public Page
{
    Q_OBJECT

public:
    explicit DashboardPage(QWidget *parent = nullptr);

    // sources / drafts / tasks 均来自 MediaRepository；页面只负责汇总与展示。
    // databaseError 非空时展示数据库不可用状态，避免把「读取失败」显示成空数据。
    void setData(const QVector<MediaSource> &sources, const QVector<ClipDraft> &drafts,
                 const QVector<ClipTask> &tasks,
                 const QString &databaseError = QString());

signals:
    void navigateRequested(const QString &pageId);
    void addSourceRequested();
    void openDraftDirRequested();
    void openSourceRequested(qint64 sourceId);

private:
    void rebuildProjects(const QVector<MediaSource> &sources,
                         const QHash<qint64, int> &downloadedBySource,
                         const QHash<qint64, qint64> &latestDraftBySource);
    void rebuildFeed(const QVector<MediaSource> &sources, const QVector<ClipDraft> &drafts,
                     const QVector<ClipTask> &tasks);

    PageHeader *m_header = nullptr;
    StatCard *m_sourceCard = nullptr;
    StatCard *m_draftCard = nullptr;
    StatCard *m_downloadCard = nullptr;
    StatCard *m_taskCard = nullptr;
    FlowNode *m_waitingNode = nullptr;
    FlowNode *m_asrNode = nullptr;
    FlowNode *m_selectionNode = nullptr;
    FlowNode *m_draftNode = nullptr;
    FlowNode *m_exportNode = nullptr;
    QWidget *m_projectsHost = nullptr;
    QVBoxLayout *m_projectsLayout = nullptr;
    QWidget *m_feedHost = nullptr;
    QVBoxLayout *m_feedLayout = nullptr;
};
