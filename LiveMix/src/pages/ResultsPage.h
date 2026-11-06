#pragma once

// 文件作用：AI 成片结果页，展示数据库中真实生成的剪映草稿。
// 点击「打开草稿文件夹」时才触发草稿下载，下载完成后在文件管理器中定位草稿目录。

#include "../data/ClipTypes.h"
#include "../services/DraftDownloadJob.h"
#include "../ui/WindowChrome.h"

#include <QHash>
#include <QVector>

class DraftCard;
class QGridLayout;
class QLabel;
class QWidget;

class ResultsPage : public Page
{
    Q_OBJECT

public:
    explicit ResultsPage(QWidget *parent = nullptr);

    void setDrafts(const QVector<ClipDraft> &drafts);
    // 下载任务状态变化时刷新对应卡片：按钮文案、进度底色与缩略图徽标。
    void setDownloadJob(const DraftDownloadJob &job);
    void removeDownloadJob(qint64 draftRowId);

signals:
    void backRequested();
    void restartRequested();
    void openDraftRequested(qint64 draftRowId);
    void toastRequested(const QString &message);

private:
    PageHeader *m_header = nullptr;
    QWidget *m_gridHost = nullptr;
    QGridLayout *m_grid = nullptr;
    QWidget *m_emptyState = nullptr;
    QHash<qint64, DraftCard *> m_cards;
    QHash<qint64, DraftDownloadJob> m_jobs;
};
