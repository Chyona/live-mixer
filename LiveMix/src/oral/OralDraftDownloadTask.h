#pragma once

// 文件作用：结果页「生成草稿 / 打开草稿」的下载任务（工作线程）。
// 成片生成时如果用户只选择「直接生成成片」，服务端草稿仍会保留；
// 用户之后点击「生成草稿」时，由本任务把该草稿下载并本地化到剪映目录。
// 任务与直播切片的 DraftDownloader 完全独立。

#include <QObject>
#include <QString>

namespace Oral {

class DraftDownloadTask : public QObject
{
    Q_OBJECT

public:
    DraftDownloadTask(const QString &draftId, const QString &draftTitle,
                      const QString &targetRoot, QObject *parent = nullptr);

public slots:
    void run();

signals:
    void progress(int percent, const QString &detail);
    void finished(bool success, const QString &localPath,
                  const QString &error);

private:
    QString m_draftId;
    QString m_draftTitle;
    QString m_targetRoot;
};

} // namespace Oral
