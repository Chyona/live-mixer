#pragma once

#include "../data/MediaTypes.h"
#include "../ui/WindowChrome.h"

#include <QHash>
#include <QVector>

class QGridLayout;
class QLineEdit;
class QWidget;

class MaterialPage : public Page
{
    Q_OBJECT

public:
    explicit MaterialPage(QWidget *parent = nullptr);

    void setSources(const QVector<MediaSource> &sources);
    void updateRecordingProgress(qint64 sourceId, qint64 durationMs);
    void updateDownloadProgress(qint64 sourceId, qint64 receivedBytes, qint64 totalBytes);
    void updateDownloadStatus(qint64 sourceId, const QString &text);

signals:
    void addSourceRequested(const QString &initialTab);
    void openResultsRequested(qint64 sourceId);
    void startPipelineRequested(qint64 sourceId);
    void previewVideoRequested(qint64 sourceId);
    void stopRecordingRequested(qint64 sourceId);
    void deleteSourceRequested(qint64 sourceId);
    void toastRequested(const QString &message);

private:
    void rebuildCards();
    void applyFilter();

    QGridLayout *m_grid = nullptr;
    QWidget *m_gridHost = nullptr;
    QWidget *m_emptyState = nullptr;
    QLineEdit *m_searchEdit = nullptr;
    QVector<MediaSource> m_sources;
    QHash<qint64, QWidget *> m_cards;
    QString m_filter;
};
