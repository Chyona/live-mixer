#pragma once

#include <QHash>
#include <QPointer>
#include <QVector>
#include <QWidget>

#include <memory>

class Button;
class ConfigStore;
class DashboardPage;
class DraftDownloadDialog;
class DraftDownloadManager;
class FullscreenEditor;
class LiveClipService;
class MaterialPage;
class MediaRepository;
class ModalOverlay;
class Page;
class PipelinePage;
class PromptPage;
class QCloseEvent;
class QMenu;
class QStackedWidget;
class QSystemTrayIcon;
class QThread;
class RecordingService;
class ResultsPage;
class SettingsPage;
class Sidebar;
class SpecPage;
class TasksPage;
class Toast;
class WindowShell;

class MainWindow : public QWidget
{
    Q_OBJECT

public:
    explicit MainWindow(QWidget *parent = nullptr);
    ~MainWindow() override;

    void navigateTo(const QString &pageId);
    void showPageForPreview(const QString &pageId);
    void showModalForPreview(const QString &modalId);
    void showFullscreenForPreview(bool readOnly);
    void showPipelineStepForPreview(int step);
    // 开发预览：在「添加源视频 → 本地文件」中预选一个文件，便于截图验证预览。
    void setPreviewLocalFile(const QString &path);

protected:
    void paintEvent(QPaintEvent *event) override;
    void resizeEvent(QResizeEvent *event) override;
    void changeEvent(QEvent *event) override;
    void closeEvent(QCloseEvent *event) override;

private:
    void buildUi();
    void buildPages();
    void wireSignals();
    void buildOverlays();
    void createResizeHandles();
    void updateResizeHandles();
    void applyWindowState();
    void toggleMaximize();
    void showToast(const QString &message);

    void openAddSource(const QString &initialTab = QStringLiteral("link"),
                       const QString &localFilePath = QString());
    bool startOnlineRecording(const QString &uri, const QString &title);
    bool importLocalVideo(const QString &filePath, const QString &note);
    void refreshMaterialLibrary();
    void openResultsForSource(qint64 sourceId);
    void refreshResults();
    void refreshTasks();
    void refreshDashboard();
    void scheduleDashboardRefresh();
    void updateServiceStatus();
    void openTask(qint64 taskId);
    void retryTask(qint64 taskId);
    void deleteSource(qint64 sourceId);
    void applySettings();
    void enforceCacheLimit();
    void ensureTray();
    void showFromTray();
    void quitFromTray();
    void openStartPipeline(qint64 sourceId = -1);
    void startClipPipeline(qint64 sourceId, const QString &promptText,
                           const QString &promptLabel);
    void openVideoPreview(qint64 sourceId);
    void requestDraftDownload(qint64 draftRowId);
    void showDraftDownloadDialog(qint64 draftRowId);
    void openDraftFolder(const QString &path);
    void updateDraftStatusBar();
    // 开发预览：用示例数据展示下载弹窗的各状态。
    void showDraftDownloadPreview(const QString &state);
    void openPromptEditor(int index);
    void openPromptViewer(int index);
    void openFullscreenPrompt(const QString &name, const QString &note, const QString &content,
                              bool readOnly, bool creating = false);

    WindowShell *m_shell = nullptr;
    QStackedWidget *m_stack = nullptr;
    Sidebar *m_sideBar = nullptr;
    Toast *m_toast = nullptr;
    ModalOverlay *m_modalOverlay = nullptr;
    FullscreenEditor *m_fullEditor = nullptr;

    DashboardPage *m_dashboard = nullptr;
    MaterialPage *m_material = nullptr;
    ResultsPage *m_results = nullptr;
    PipelinePage *m_pipeline = nullptr;
    PromptPage *m_prompt = nullptr;
    TasksPage *m_tasks = nullptr;
    SettingsPage *m_settings = nullptr;
    SpecPage *m_spec = nullptr;

    QHash<QString, int> m_pageIndexes;
    QVector<QWidget *> m_resizeHandles;
    int m_shadowMargin = 16;
    bool m_maximized = false;
    bool m_forceQuit = false;
    QSystemTrayIcon *m_tray = nullptr;
    QMenu *m_trayMenu = nullptr;

    // 一键成片任务与草稿下载任务各自运行在独立线程，避免阻塞界面。
    QThread *m_clipThread = nullptr;
    LiveClipService *m_clipService = nullptr;
    // 草稿下载任务注册表；下载弹窗关闭后任务继续运行。
    DraftDownloadManager *m_downloads = nullptr;
    QPointer<DraftDownloadDialog> m_downloadDialog;
    qint64 m_downloadDialogDraftId = -1;
    qint64 m_lastSourceId = -1;
    // 结果页当前展示的源视频；-1 表示展示全部草稿（如从首页进入）。
    qint64 m_resultsSourceId = -1;
    qint64 m_clipTaskId = -1;
    bool m_clipTaskActive = false;
    // 最近一次一键成片的源视频与提示词，用于过程页的「再跑一次」。
    QString m_lastPromptText;
    QString m_lastPromptLabel;
    bool m_dashboardRefreshPending = false;
    QString m_databaseError;
    QString m_previewLocalFile;

    // Declared last so the recording service is destroyed before the
    // repository and config store it depends on.
    std::unique_ptr<ConfigStore> m_configStore;
    std::unique_ptr<MediaRepository> m_repository;
    std::unique_ptr<RecordingService> m_recording;
};
