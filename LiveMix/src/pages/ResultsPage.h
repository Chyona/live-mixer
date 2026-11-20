#pragma once

// 文件作用：AI 成片结果页，展示数据库中真实生成的剪映草稿。
// 每张卡片并列「导出到剪映」与「导出成片」两个动作；右上角进入批量选择后，
// 可一次导入剪映或批量导出成片。下载与导出状态来自两个任务管理器。

#include "../data/ClipTypes.h"
#include "../services/DraftDownloadJob.h"
#include "../services/VideoExportJob.h"
#include "../ui/WindowChrome.h"

#include <QHash>
#include <QSet>
#include <QVector>

class Button;
class DraftCard;
class QGridLayout;
class QLabel;
class QPushButton;
class QVBoxLayout;
class QWidget;

class ResultsPage : public Page
{
    Q_OBJECT

public:
    explicit ResultsPage(QWidget *parent = nullptr);

    void setDrafts(const QVector<ClipDraft> &drafts);

    // 草稿下载状态。
    void setDownloadJob(const DraftDownloadJob &job);
    void removeDownloadJob(qint64 draftRowId);
    // 成片导出状态。
    void setVideoJob(const VideoExportJob &job);
    void removeVideoJob(qint64 draftRowId);

    // 批量任务快照：只用于卡片状态与页头进度入口。
    void setDraftBatch(const DraftBatchSnapshot &batch);
    void clearDraftBatch();
    void setVideoBatch(const VideoBatchSnapshot &batch);
    void clearVideoBatch();

signals:
    void backRequested();
    void restartRequested();
    void openDraftRequested(qint64 draftRowId);
    void videoExportRequested(qint64 draftRowId);
    void openDraftBatchRequested();
    void openVideoBatchRequested();
    void batchDraftRequested(const QVector<qint64> &draftRowIds);
    void batchVideoRequested(const QVector<qint64> &draftRowIds);
    void toastRequested(const QString &message);

protected:
    void resizeEvent(QResizeEvent *event) override;

private:
    bool draftEligible(qint64 draftRowId) const;
    bool videoEligible(qint64 draftRowId) const;
    bool anyEligible(qint64 draftRowId) const;
    QVector<qint64> eligibleFor(bool draftAction) const;
    QVector<qint64> selectableIds() const;

    const DraftBatchItem *draftBatchItem(qint64 draftRowId) const;
    const VideoBatchItem *videoBatchItem(qint64 draftRowId) const;

    void refreshCard(qint64 draftRowId);
    void refreshAllCards();
    void updateHeader();
    // 页头与全部卡片共用一个批次生成时间；相对标签每 10 秒刷新。
    void updateGenerationTimes();
    void pruneSelection();
    void updateSelectionBar();

    void enterSelection();
    void exitSelection();
    void toggleSelected(qint64 draftRowId);
    void selectAll();
    void clearSelection();
    void startDraftBatch();
    void startVideoBatch();

    void buildSelectionBar();
    void positionSelectionBar();

    PageHeader *m_header = nullptr;
    QWidget *m_generationBadge = nullptr;
    QLabel *m_generationValue = nullptr;
    Button *m_batchButton = nullptr;
    QWidget *m_gridHost = nullptr;
    QGridLayout *m_grid = nullptr;
    QWidget *m_emptyState = nullptr;

    QVector<ClipDraft> m_drafts;
    QHash<qint64, DraftCard *> m_cards;
    QHash<qint64, DraftDownloadJob> m_jobs;
    QHash<qint64, VideoExportJob> m_videoJobs;
    // 草稿目录是否已存在；批量进度刷新很频繁，不能在每次刷新时访问文件系统。
    QHash<qint64, bool> m_draftDownloaded;

    DraftBatchSnapshot m_draftBatch;
    VideoBatchSnapshot m_videoBatch;
    bool m_hasDraftBatch = false;
    bool m_hasVideoBatch = false;

    bool m_selecting = false;
    QSet<qint64> m_selected;
    QWidget *m_selectionBar = nullptr;
    QLabel *m_selectionTitle = nullptr;
    QLabel *m_selectionMeta = nullptr;
    Button *m_exportVideoButton = nullptr;
    Button *m_exportDraftButton = nullptr;
};
