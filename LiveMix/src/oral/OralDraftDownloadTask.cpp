// 文件作用：结果页草稿下载任务实现。

#include "OralDraftDownloadTask.h"

#include "../api/HttpClient.h"
#include "OralCapCutClient.h"

namespace Oral {

DraftDownloadTask::DraftDownloadTask(const QString &draftId,
                                     const QString &draftTitle,
                                     const QString &targetRoot, QObject *parent)
    : QObject(parent)
    , m_draftId(draftId)
    , m_draftTitle(draftTitle)
    , m_targetRoot(targetRoot)
{
}

void DraftDownloadTask::run()
{
    HttpClient http;
    CapCutClient capcut(&http);
    QString localPath;
    QString error;
    const bool ok = capcut.downloadDraft(
        m_draftId, m_draftTitle, m_targetRoot, &localPath, &error,
        [this](int percent, const QString &detail) {
            emit progress(percent, detail);
        });
    emit finished(ok, localPath, error);
}

} // namespace Oral
