// 文件作用：主窗口与页面编排。负责源视频库、一键成片任务、成片结果与草稿下载的串联。
#include "MainWindow.h"

#include "ApplicationStyle.h"
#include "../core/AppConfig.h"
#include "../core/AutoStart.h"
#include "../core/FileManager.h"
#include "../core/MediaUtils.h"
#include "../data/MediaRepository.h"
#include "../media/FfmpegLocator.h"
#include "../media/FfmpegUtils.h"
#include "../media/MediaProbe.h"
#include "../media/RecordingService.h"
#include "../model/DemoData.h"
#include "../services/DraftDownloadManager.h"
#include "../services/LiveClipService.h"
#include "../pages/DashboardPage.h"
#include "../pages/MaterialPage.h"
#include "../pages/PipelinePage.h"
#include "../pages/PromptPage.h"
#include "../pages/ResultsPage.h"
#include "../pages/SettingsPage.h"
#include "../pages/SpecPage.h"
#include "../pages/TasksPage.h"
#include "../ui/Controls.h"
#include "../ui/DraftDownloadDialog.h"
#include "../ui/Icons.h"
#include "../ui/Overlays.h"
#include "../ui/VideoPreview.h"
#include "../ui/WindowChrome.h"

#include <QAction>
#include <QApplication>
#include <QCloseEvent>
#include <QDateTime>
#include <QDebug>
#include <QDesktopServices>
#include <QDir>
#include <QDragEnterEvent>
#include <QDragLeaveEvent>
#include <QDropEvent>
#include <QEnterEvent>
#include <QEventLoop>
#include <QFileDialog>
#include <QFileInfo>
#include <QLocale>
#include <QFontMetrics>
#include <QHBoxLayout>
#include <QImage>
#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <QLabel>
#include <QLineEdit>
#include <QMenu>
#include <QMimeData>
#include <QMouseEvent>
#include <QPainter>
#include <QPainterPath>
#include <QPolygonF>
#include <QResizeEvent>
#include <QScreen>
#include <QSet>
#include <QShortcut>
#include <QStackedWidget>
#include <QStandardPaths>
#include <QSystemTrayIcon>
#include <QTextDocument>
#include <QThread>
#include <QTimer>
#include <QUrl>
#include <QVBoxLayout>
#include <QWindow>

#include <functional>

namespace {

constexpr qint64 kMaxLocalVideoBytes = 10LL * 1024 * 1024 * 1024;

QString formatFileSize(qint64 bytes)
{
    constexpr double kMb = 1024.0 * 1024.0;
    const double megabytes = bytes / kMb;
    if (megabytes >= 1024.0) {
        return QStringLiteral("%1 GB").arg(megabytes / 1024.0, 0, 'f', 2);
    }
    if (megabytes >= 1.0) {
        return QStringLiteral("%1 MB").arg(megabytes, 0, 'f', 1);
    }
    return QStringLiteral("%1 KB").arg(bytes / 1024.0, 0, 'f', 0);
}

const QStringList &videoExtensions()
{
    static const QStringList extensions = {
        QStringLiteral("mp4"),  QStringLiteral("mov"),  QStringLiteral("mkv"),
        QStringLiteral("flv"),  QStringLiteral("ts"),   QStringLiteral("m2ts"),
        QStringLiteral("avi"),  QStringLiteral("webm"), QStringLiteral("m4v"),
        QStringLiteral("mpg"),  QStringLiteral("mpeg"), QStringLiteral("wmv"),
        QStringLiteral("3gp"),  QStringLiteral("m3u8"),
    };
    return extensions;
}

bool isSupportedVideoFile(const QString &path)
{
    const QFileInfo info(path);
    return info.isFile() && info.isReadable()
        && videoExtensions().contains(info.suffix().toLower());
}

QString videoFormatHint()
{
    return QStringLiteral("mp4 / mov / mkv / flv / ts / m3u8");
}

QString videoFileFilter()
{
    return QStringLiteral(
        "视频文件 (*.mp4 *.mov *.mkv *.flv *.ts *.m2ts *.avi *.webm *.m4v "
        "*.mpg *.mpeg *.wmv *.3gp *.m3u8)");
}

bool normalizeStreamUrl(const QString &text, QString *normalized)
{
    const QUrl url = QUrl::fromUserInput(text);
    const QString scheme = url.scheme().toLower();
    static const QSet<QString> schemes = {
        QStringLiteral("http"), QStringLiteral("https"), QStringLiteral("rtmp"),
        QStringLiteral("rtmps"), QStringLiteral("rtsp"),
    };
    if (!url.isValid() || !schemes.contains(scheme)) {
        return false;
    }
    if (normalized) {
        *normalized = url.toString();
    }
    return true;
}

class ResizeHandle : public QWidget
{
public:
    ResizeHandle(Qt::Edges edges, Qt::CursorShape cursor, QWidget *parent)
        : QWidget(parent)
        , m_edges(edges)
    {
        setCursor(cursor);
        setAttribute(Qt::WA_NoSystemBackground, true);
    }

protected:
    void mousePressEvent(QMouseEvent *event) override
    {
        if (event->button() == Qt::LeftButton && window()->windowHandle()) {
            window()->windowHandle()->startSystemResize(m_edges);
            event->accept();
            return;
        }
        QWidget::mousePressEvent(event);
    }

private:
    Qt::Edges m_edges;
};

class DropZone : public QWidget
{
public:
    explicit DropZone(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        setMinimumHeight(108);
        setCursor(Qt::PointingHandCursor);
        setAttribute(Qt::WA_Hover, true);
        setAcceptDrops(true);
    }

    void setFilePath(const QString &path)
    {
        m_filePath = QFileInfo(path).absoluteFilePath();
        update();
        if (m_onFileSelected) {
            m_onFileSelected(m_filePath);
        }
    }

    QString filePath() const { return m_filePath; }

    void setRejectHandler(std::function<void(const QString &)> handler)
    {
        m_onReject = std::move(handler);
    }

    void setFileSelectedHandler(std::function<void(const QString &)> handler)
    {
        m_onFileSelected = std::move(handler);
    }

protected:
    void enterEvent(QEnterEvent *event) override
    {
        m_hovered = true;
        update();
        QWidget::enterEvent(event);
    }

    void leaveEvent(QEvent *event) override
    {
        m_hovered = false;
        update();
        QWidget::leaveEvent(event);
    }

    void dragEnterEvent(QDragEnterEvent *event) override
    {
        // Accept URL drags so a drop event can explain why unsupported files
        // are rejected instead of silently ignoring the user's action.
        if (event->mimeData() && event->mimeData()->hasUrls()) {
            m_dropActive = true;
            update();
            event->acceptProposedAction();
        }
    }

    void dragLeaveEvent(QDragLeaveEvent *event) override
    {
        m_dropActive = false;
        update();
        QWidget::dragLeaveEvent(event);
    }

    void dropEvent(QDropEvent *event) override
    {
        const QMimeData *mimeData = event->mimeData();
        const QString path = firstSupportedPath(mimeData);
        m_dropActive = false;
        if (!path.isEmpty()) {
            setFilePath(path);
            event->acceptProposedAction();
        } else if (mimeData && mimeData->hasUrls()) {
            bool hasLocalFile = false;
            for (const QUrl &url : mimeData->urls()) {
                if (url.isLocalFile()) {
                    hasLocalFile = true;
                    break;
                }
            }
            if (m_onReject) {
                m_onReject(hasLocalFile
                               ? QStringLiteral("仅支持视频文件：%1")
                                     .arg(videoFormatHint())
                               : QStringLiteral("请拖入本地视频文件"));
            }
            event->ignore();
        }
        update();
    }

    void mouseReleaseEvent(QMouseEvent *event) override
    {
        if (event->button() == Qt::LeftButton
            && rect().contains(event->position().toPoint())) {
            chooseFile();
        }
        QWidget::mouseReleaseEvent(event);
    }

    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        penSelected(&painter);
        painter.drawRoundedRect(QRectF(rect()).adjusted(0.75, 0.75, -0.75, -0.75), 10, 10);

        if (!m_filePath.isEmpty()) {
            const QFontMetrics metrics(Theme::font(13, 500));
            const QString name = metrics.elidedText(QFileInfo(m_filePath).fileName(),
                                                    Qt::ElideMiddle, width() - 32);
            painter.setFont(Theme::font(13, 500));
            painter.setPen(m_hovered ? Theme::Primary : Theme::Text1);
            painter.drawText(QRect(16, height() / 2 - 26, width() - 32, 24),
                             Qt::AlignCenter, name);
            painter.setFont(Theme::font(11));
            painter.setPen(Theme::Text3);
            painter.drawText(QRect(16, height() / 2 + 2, width() - 32, 20),
                             Qt::AlignCenter, QStringLiteral("点击重新选择文件"));
            return;
        }

        painter.setFont(Theme::font(13));
        painter.setPen(m_hovered ? Theme::Text2 : Theme::Text3);
        painter.drawText(QRect(16, height() / 2 - 26, width() - 32, 24),
                         Qt::AlignCenter, QStringLiteral("拖拽本地视频文件到此处，或点击选择"));
        painter.setFont(Theme::font(11));
        painter.setPen(Theme::Text3);
        painter.drawText(
            QRect(16, height() / 2 + 2, width() - 32, 20), Qt::AlignCenter,
            QStringLiteral("仅支持视频文件：%1，单文件 ≤ 10 GB").arg(videoFormatHint()));
    }

private:
    static QString firstSupportedPath(const QMimeData *mimeData)
    {
        if (!mimeData || !mimeData->hasUrls()) {
            return QString();
        }
        for (const QUrl &url : mimeData->urls()) {
            if (!url.isLocalFile()) {
                continue;
            }
            const QString path = url.toLocalFile();
            if (isSupportedVideoFile(path)) {
                return path;
            }
        }
        return QString();
    }

    void chooseFile()
    {
        QString start = QStandardPaths::writableLocation(QStandardPaths::MoviesLocation);
        if (start.isEmpty()) {
            start = QDir::homePath();
        }
        const QString path = QFileDialog::getOpenFileName(
            this, QStringLiteral("选择本地视频"), start, videoFileFilter());
        if (!path.isEmpty()) {
            setFilePath(path);
        }
    }

    void penSelected(QPainter *painter)
    {
        QPen pen((m_hovered || m_dropActive) ? Theme::Primary : Theme::BorderStrong, 1.5,
                 Qt::DashLine);
        pen.setDashPattern({4.0, 3.0});
        painter->setPen(pen);
        painter->setBrush(Qt::NoBrush);
    }

    QString m_filePath;
    bool m_hovered = false;
    bool m_dropActive = false;
    std::function<void(const QString &)> m_onReject;
    std::function<void(const QString &)> m_onFileSelected;
};

// 本地视频预览缩略图：按控件尺寸等比裁剪绘制，保持圆角。
class PreviewThumbnail : public QWidget
{
public:
    explicit PreviewThumbnail(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        setCursor(Qt::PointingHandCursor);
        setToolTip(QStringLiteral("点击打开视频预览"));
    }

    std::function<void()> clicked;

    void setImage(const QImage &image)
    {
        m_image = image;
        m_message.clear();
        update();
    }

    void setMessage(const QString &message)
    {
        m_image = QImage();
        m_message = message;
        update();
    }

protected:
    void mouseReleaseEvent(QMouseEvent *event) override
    {
        if (event->button() == Qt::LeftButton && clicked
            && rect().contains(event->position().toPoint())) {
            clicked();
        }
        QWidget::mouseReleaseEvent(event);
    }

    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        QPainterPath clip;
        clip.addRoundedRect(QRectF(rect()), 8, 8);
        painter.setClipPath(clip);
        painter.fillRect(rect(), Theme::Surface2);

        if (m_image.isNull()) {
            painter.setPen(Theme::Text3);
            painter.setFont(Theme::font(12));
            painter.drawText(rect(), Qt::AlignCenter,
                             m_message.isEmpty() ? QStringLiteral("视频预览")
                                                 : m_message);
            return;
        }

        const QImage scaled = m_image.scaled(size(), Qt::KeepAspectRatioByExpanding,
                                             Qt::SmoothTransformation);
        const QPoint topLeft((width() - scaled.width()) / 2,
                             (height() - scaled.height()) / 2);
        painter.drawImage(topLeft, scaled);

        // 中央播放标记，让用户一眼确认这是已选中的视频。
        const QPointF center(width() / 2.0, height() / 2.0);
        const qreal radius = qBound<qreal>(16.0, height() * 0.18, 24.0);
        painter.setPen(Qt::NoPen);
        painter.setBrush(QColor(0, 0, 0, 120));
        painter.drawEllipse(center, radius, radius);
        QPolygonF triangle;
        triangle << QPointF(center.x() - radius * 0.35, center.y() - radius * 0.45)
                 << QPointF(center.x() + radius * 0.55, center.y())
                 << QPointF(center.x() - radius * 0.35, center.y() + radius * 0.45);
        painter.setBrush(Qt::white);
        painter.drawPolygon(triangle);
    }

private:
    QImage m_image;
    QString m_message;
};

// 本地导入弹窗中的视频预览：左侧缩略图，右侧文件名与媒体信息。
class LocalVideoPreview : public Card
{
public:
    explicit LocalVideoPreview(QWidget *parent = nullptr)
        : Card(Theme::Surface2, Theme::Border, 10, parent)
    {
        contentLayout()->setContentsMargins(10, 10, 10, 10);
        contentLayout()->setSpacing(0);

        auto *row = new QHBoxLayout;
        row->setContentsMargins(0, 0, 0, 0);
        row->setSpacing(12);

        m_thumbnail = new PreviewThumbnail(this);
        m_thumbnail->setFixedSize(176, 99);
        row->addWidget(m_thumbnail, 0, Qt::AlignTop);

        auto *info = new QWidget(this);
        auto *layout = new QVBoxLayout(info);
        layout->setContentsMargins(0, 2, 0, 2);
        layout->setSpacing(4);

        m_name = new ElidedLabel(QString(), info);
        m_name->setPixelSize(13);
        m_name->setFontWeight(600);
        m_name->setTextColor(Theme::Text1);
        layout->addWidget(m_name);

        m_meta = new ElidedLabel(QString(), info);
        m_meta->setPixelSize(11);
        m_meta->setTextColor(Theme::Text3);
        layout->addWidget(m_meta);

        auto *hint = Ui::label(QStringLiteral("点击缩略图打开视频预览"), 11, 400,
                               Theme::Text3, info);
        layout->addWidget(hint);
        layout->addStretch(1);

        row->addWidget(info, 1);
        contentLayout()->addLayout(row);
    }

    void setFile(const QString &path)
    {
        m_name->setText(QFileInfo(path).fileName());
        m_meta->setText(QStringLiteral("正在读取视频信息…"));
        m_thumbnail->setMessage(QStringLiteral("正在生成预览…"));
        // 缩略图只是截帧；点击后交给系统播放器做完整预览。
        m_thumbnail->clicked = [path] {
            QDesktopServices::openUrl(QUrl::fromLocalFile(path));
        };

        QApplication::setOverrideCursor(Qt::WaitCursor);
        // 先让“正在读取视频信息/生成预览”的加载状态显示出来，再执行阻塞探测。
        QApplication::processEvents(QEventLoop::ExcludeUserInputEvents);
        const ProbeInfo probe = MediaProbe::probe(path);

        int width = 0;
        int height = 0;
        if (probe.ok) {
            const QJsonArray streams =
                QJsonDocument::fromJson(probe.json.toUtf8())
                    .object()
                    .value(QStringLiteral("streams"))
                    .toArray();
            for (const QJsonValue &value : streams) {
                const QJsonObject stream = value.toObject();
                if (stream.value(QStringLiteral("codec_type")).toString()
                    != QLatin1String("video")) {
                    continue;
                }
                if (stream.value(QStringLiteral("disposition"))
                        .toObject()
                        .value(QStringLiteral("attached_pic"))
                        .toInt()
                    == 1) {
                    continue;
                }
                width = stream.value(QStringLiteral("width")).toInt();
                height = stream.value(QStringLiteral("height")).toInt();
                break;
            }
        }

        // 缩略图按“路径 + 修改时间”缓存，重复选择同一文件时无需再次截帧。
        const QString thumbnailPath = FfmpegUtils::thumbnailCachePath(path);
        QImage thumbnail(thumbnailPath);
        if (thumbnail.isNull()) {
            const qint64 positionMs =
                probe.durationMs > 3000 ? 1000 : qMax<qint64>(0, probe.durationMs / 3);
            QString thumbnailError;
            if (FfmpegUtils::extractThumbnail(path, positionMs, thumbnailPath,
                                              &thumbnailError)) {
                thumbnail.load(thumbnailPath);
            }
        }

        QApplication::restoreOverrideCursor();

        if (thumbnail.isNull()) {
            m_thumbnail->setMessage(QStringLiteral("无法生成视频预览"));
        } else {
            m_thumbnail->setImage(thumbnail);
        }

        if (!probe.ok) {
            m_meta->setText(QStringLiteral("无法读取视频信息：%1").arg(probe.error));
            return;
        }

        QStringList parts;
        parts.append(MediaUtils::formatDuration(probe.durationMs));
        if (width > 0 && height > 0) {
            parts.append(QStringLiteral("%1×%2").arg(width).arg(height));
        }
        parts.append(formatFileSize(probe.sizeBytes));
        m_meta->setText(parts.join(QStringLiteral(" · ")));
    }

private:
    PreviewThumbnail *m_thumbnail = nullptr;
    ElidedLabel *m_name = nullptr;
    ElidedLabel *m_meta = nullptr;
};

QWidget *makeNotice(const QString &text, QWidget *parent)
{
    auto *card = new Card(Theme::Surface2, Qt::transparent, 8, parent);
    auto *layout = card->contentLayout();
    layout->setContentsMargins(12, 10, 12, 10);
    layout->setSpacing(0);
    auto *label = Ui::label(text, 11, 400, Theme::Text3, card);
    label->setWordWrap(true);
    layout->addWidget(label);
    return card;
}

} // namespace

MainWindow::MainWindow(QWidget *parent)
    : QWidget(parent)
{
    m_configStore = std::make_unique<ConfigStore>();
    QString configError;
    const bool configLoaded = m_configStore->load(&configError);

    m_repository = std::make_unique<MediaRepository>();
    QString databaseError;
    const bool databaseOpened = m_repository->open(&databaseError);
    if (!databaseOpened) {
        m_databaseError = databaseError;
    }
    if (databaseOpened) {
        QString recoveryError;
        if (!m_repository->recoverInterrupted(&recoveryError)) {
            qWarning() << "Failed to recover interrupted recordings:" << recoveryError;
        }
    }
    m_recording = std::make_unique<RecordingService>(m_configStore.get(),
                                                     m_repository.get());

    setWindowFlags(Qt::Window | Qt::FramelessWindowHint);
    setAttribute(Qt::WA_TranslucentBackground, true);
    setWindowTitle(QStringLiteral("LiveMix Studio · 直播切片工作台"));
    setWindowIcon(Icons::appIcon());
    setMinimumSize(1160, 740);

    buildUi();
    buildPages();
    wireSignals();
    buildOverlays();
    createResizeHandles();

    // 侧栏与底栏只展示真实状态：数据库是否打开、API 密钥是否配置、ffmpeg 是否可用。
    m_sideBar->setDatabaseReady(databaseOpened, databaseError);
    updateServiceStatus();

    if (!configLoaded) {
        QTimer::singleShot(0, this, [this, configError] {
            showToast(QStringLiteral("配置读取失败，已使用默认配置：%1").arg(configError));
        });
    }
    if (!databaseOpened) {
        QTimer::singleShot(0, this, [this, databaseError] {
            showToast(QStringLiteral("本地数据库不可用：%1").arg(databaseError));
        });
    } else {
        QString taskRecoveryError;
        if (!m_repository->recoverInterruptedClipTasks(&taskRecoveryError)) {
            qWarning() << "Failed to recover interrupted clip tasks:" << taskRecoveryError;
        }
        refreshMaterialLibrary();
        refreshResults();
        refreshTasks();
    }
    refreshDashboard();

    const QScreen *screen = QApplication::primaryScreen();
    QSize target(1432, 912);
    if (screen) {
        const QRect available = screen->availableGeometry();
        target.setWidth(qMin(target.width(), available.width() - 24));
        target.setHeight(qMin(target.height(), available.height() - 24));
        resize(target);
        move(available.center() - QPoint(target.width() / 2, target.height() / 2));
    } else {
        resize(target);
    }

    // 走统一导航入口，初始化标题栏的文档名并同步侧栏选中状态。
    navigateTo(QStringLiteral("dash"));

    auto *newShortcut = new QShortcut(QKeySequence::New, this);
    connect(newShortcut, &QShortcut::activated, this, [this] {
        if ((m_modalOverlay && m_modalOverlay->isOpen())
            || (m_fullEditor && m_fullEditor->isVisible())) {
            return;
        }
        openAddSource(QStringLiteral("link"));
    });

    auto *searchShortcut = new QShortcut(QKeySequence(Qt::CTRL | Qt::Key_K), this);
    connect(searchShortcut, &QShortcut::activated, this, [this] {
        if ((m_modalOverlay && m_modalOverlay->isOpen())
            || (m_fullEditor && m_fullEditor->isVisible())) {
            return;
        }
        showToast(QStringLiteral("全局搜索 · 原型预览"));
    });

    if (Theme::isWindowsWindowStyle()) {
        // Windows 11 的 Alt+Space 打开标题栏系统菜单。
        auto *systemMenuShortcut =
            new QShortcut(QKeySequence(Qt::ALT | Qt::Key_Space), this);
        systemMenuShortcut->setContext(Qt::WindowShortcut);
        connect(systemMenuShortcut, &QShortcut::activated, this, [this] {
            if ((m_modalOverlay && m_modalOverlay->isOpen())
                || (m_fullEditor && m_fullEditor->isVisible())) {
                return;
            }
            m_shell->titleBar()->showSystemMenuAtIcon();
        });
    } else {
        // macOS 使用 ⌘W 关闭窗口；有弹窗时先关闭当前弹窗。
        auto *closeShortcut = new QShortcut(QKeySequence::Close, this);
        connect(closeShortcut, &QShortcut::activated, this, [this] {
            if (m_modalOverlay && m_modalOverlay->isOpen()) {
                m_modalOverlay->closeModal();
                return;
            }
            if (m_fullEditor && m_fullEditor->isVisible()) {
                m_fullEditor->closeEditor();
                return;
            }
            close();
        });
    }
}

MainWindow::~MainWindow()
{
    // 窗口销毁时后台任务无法继续，任务中心应显示为中断而不是永久运行中。
    if (m_clipTaskActive && m_repository && m_repository->isOpen()) {
        m_repository->finishClipTask(m_clipTaskId, ClipTaskStatus::Failed,
                                     QStringLiteral("应用退出时任务未完成。"));
        m_clipTaskActive = false;
        m_clipTaskId = -1;
    }

    if (m_recording) {
        m_recording->shutdown();
    }

    // 退出时给后台任务短暂收尾时间。网络请求 / ffmpeg 无法立即中断；
    // 超时后不再 terminate（会破坏线程内的 Qt 对象并导致退出崩溃），
    // 让线程随进程退出由操作系统回收，任务已在上方标记为中断。
    for (QThread *thread : {m_clipThread}) {
        if (!thread || !thread->isRunning()) {
            continue;
        }
        thread->requestInterruption();
        if (!thread->wait(1500)) {
            qWarning() << "Background worker still running at exit; detaching";
        }
    }
}

void MainWindow::closeEvent(QCloseEvent *event)
{
    const AppConfig config = m_configStore ? m_configStore->config() : AppConfig();
    if (!m_forceQuit && config.general.minimizeToBackground) {
        ensureTray();
        if (m_tray && m_tray->isVisible()) {
            event->ignore();
            hide();
            if (config.general.systemNotifications) {
                m_tray->showMessage(QStringLiteral("LiveMix Studio"),
                                    QStringLiteral("应用仍在后台运行，录制任务会继续。"),
                                    QSystemTrayIcon::Information, 3000);
            }
            return;
        }
    }

    if (m_recording) {
        m_recording->stopAllAndWait(4000);
    }
    QWidget::closeEvent(event);
}

void MainWindow::applySettings()
{
    if (!m_configStore) {
        return;
    }
    const AppConfig config = m_configStore->config();

    if (AutoStart::isSupported()) {
        QString error;
        if (!AutoStart::setEnabled(config.general.launchAtLogin, &error)) {
            showToast(QStringLiteral("开机启动设置失败：%1").arg(error));
        }
    }

    const bool trayNeeded = config.general.minimizeToBackground
        || config.general.systemNotifications;
    if (trayNeeded) {
        ensureTray();
        if (m_tray) {
            m_tray->show();
        }
    } else if (m_tray) {
        m_tray->hide();
    }
    updateServiceStatus();
}

void MainWindow::updateServiceStatus()
{
    if (!m_shell || !m_configStore) {
        return;
    }
    const bool configured = !m_configStore->config().service.apiKey.trimmed().isEmpty();
    m_shell->statusBar()->setServiceStatus(
        configured ? QStringLiteral("服务已配置") : QStringLiteral("服务未配置"), configured,
        configured ? QStringLiteral("API 密钥已配置，可开始一键成片。")
                   : QStringLiteral("请到「设置 → 服务连接」填写 API 密钥。"));
    m_shell->statusBar()->setFfmpegAvailable(!FfmpegLocator::ffmpegPath().isEmpty());
}

void MainWindow::ensureTray()
{
    if (m_tray) {
        if (!m_tray->isVisible()) {
            m_tray->show();
        }
        return;
    }
    if (!QSystemTrayIcon::isSystemTrayAvailable()) {
        return;
    }

    m_tray = new QSystemTrayIcon(Icons::appIcon(), this);
    m_tray->setToolTip(QStringLiteral("LiveMix Studio"));

    m_trayMenu = new QMenu(this);
    QAction *showAction = m_trayMenu->addAction(QStringLiteral("显示主窗口"));
    QAction *quitAction = m_trayMenu->addAction(QStringLiteral("退出 LiveMix"));
    connect(showAction, &QAction::triggered, this, &MainWindow::showFromTray);
    connect(quitAction, &QAction::triggered, this, &MainWindow::quitFromTray);
    m_tray->setContextMenu(m_trayMenu);

    connect(m_tray, &QSystemTrayIcon::activated, this,
            [this](QSystemTrayIcon::ActivationReason reason) {
                if (reason == QSystemTrayIcon::Trigger
                    || reason == QSystemTrayIcon::DoubleClick) {
                    showFromTray();
                }
            });
    m_tray->show();
}

void MainWindow::showFromTray()
{
    show();
    raise();
    activateWindow();
}

void MainWindow::quitFromTray()
{
    m_forceQuit = true;
    close();
}

void MainWindow::buildUi()
{
    auto *outer = new QVBoxLayout(this);
    outer->setContentsMargins(m_shadowMargin, m_shadowMargin, m_shadowMargin, m_shadowMargin);
    outer->setSpacing(0);
    m_shell = new WindowShell(this);
    outer->addWidget(m_shell);

    connect(m_shell->titleBar(), &TitleBar::closeRequested, this, &QWidget::close);
    connect(m_shell->titleBar(), &TitleBar::minimizeRequested, this, &QWidget::showMinimized);
    connect(m_shell->titleBar(), &TitleBar::maximizeRequested, this, &MainWindow::toggleMaximize);

    m_sideBar = new Sidebar(m_shell);
    m_shell->workspaceLayout()->addWidget(m_sideBar);

    m_stack = new QStackedWidget(m_shell);
    m_stack->setStyleSheet(QStringLiteral("QStackedWidget { background: transparent; }"));
    m_shell->workspaceLayout()->addWidget(m_stack, 1);

    connect(m_sideBar, &Sidebar::pageRequested, this, &MainWindow::navigateTo);
}

void MainWindow::buildPages()
{
    auto addPage = [this](const QString &id, QWidget *page) {
        m_pageIndexes.insert(id, m_stack->addWidget(page));
    };

    m_dashboard = new DashboardPage;
    addPage(QStringLiteral("dash"), m_dashboard);
    m_material = new MaterialPage;
    addPage(QStringLiteral("material"), m_material);
    m_results = new ResultsPage;
    addPage(QStringLiteral("workspace"), m_results);
    m_pipeline = new PipelinePage;
    addPage(QStringLiteral("pipeline"), m_pipeline);
    m_prompt = new PromptPage;
    addPage(QStringLiteral("prompt"), m_prompt);
    m_tasks = new TasksPage;
    addPage(QStringLiteral("tasks"), m_tasks);
    m_settings = new SettingsPage(m_configStore.get());
    addPage(QStringLiteral("settings"), m_settings);
    m_spec = new SpecPage;
    addPage(QStringLiteral("spec"), m_spec);
}

void MainWindow::wireSignals()
{
    // 草稿下载任务管理器：结果页、下载弹窗与底部状态栏共享同一份任务状态。
    m_downloads = new DraftDownloadManager(this);
    connect(m_downloads, &DraftDownloadManager::jobChanged, this,
            [this](const DraftDownloadJob &job) {
                if (m_results) {
                    m_results->setDownloadJob(job);
                }
                updateDraftStatusBar();
                if (m_downloadDialog && m_downloadDialogDraftId == job.draftRowId) {
                    m_downloadDialog->setJob(job);
                }
            });
    connect(m_downloads, &DraftDownloadManager::jobRemoved, this,
            [this](qint64 draftRowId) {
                if (m_results) {
                    m_results->removeDownloadJob(draftRowId);
                }
                updateDraftStatusBar();
                // 取消下载后关闭对应弹窗；其它弹窗打开时保持原样。
                if (m_downloadDialog && m_downloadDialogDraftId == draftRowId
                    && m_downloadDialog->isVisible() && m_modalOverlay) {
                    m_modalOverlay->closeModal();
                }
            });
    connect(m_downloads, &DraftDownloadManager::jobFinished, this,
            [this](const DraftDownloadJob &job) {
                if (m_results) {
                    m_results->setDownloadJob(job);
                }
                updateDraftStatusBar();
                if (job.status == DraftDownloadStatus::Done) {
                    // 下载路径已落库，刷新卡片徽标与视频库入口。
                    refreshResults();
                    refreshMaterialLibrary();
                    if (!job.localPath.isEmpty()) {
                        openDraftFolder(job.localPath);
                    }
                    const bool dialogOpen = m_downloadDialog
                        && m_downloadDialogDraftId == job.draftRowId
                        && m_downloadDialog->isVisible();
                    if (!dialogOpen) {
                        showToast(
                            QStringLiteral("「%1」草稿已下载完成，已定位到草稿文件夹")
                                .arg(job.title));
                    }
                } else {
                    const bool dialogOpen = m_downloadDialog
                        && m_downloadDialogDraftId == job.draftRowId
                        && m_downloadDialog->isVisible();
                    if (!dialogOpen) {
                        showToast(
                            QStringLiteral("草稿下载失败，可在结果卡片上重试"));
                    }
                }
            });
    connect(m_downloads, &DraftDownloadManager::toastRequested, this,
            &MainWindow::showToast);
    connect(m_shell->statusBar(), &StatusBar::draftDownloadRequested, this,
            &MainWindow::showDraftDownloadDialog);

    connect(m_dashboard, &DashboardPage::navigateRequested, this,
            [this](const QString &pageId) {
                // 从首页进入结果页时展示全部草稿，不再沿用上次的源视频筛选。
                if (pageId == QStringLiteral("workspace")) {
                    m_resultsSourceId = -1;
                    refreshResults();
                }
                navigateTo(pageId);
            });
    connect(m_dashboard, &DashboardPage::addSourceRequested, this,
            [this] { openAddSource(QStringLiteral("link")); });
    connect(m_dashboard, &DashboardPage::openDraftDirRequested, this, [this] {
        const AppConfig config = m_configStore ? m_configStore->config() : AppConfig();
        const QString draftDir = config.storage.draftDir.trimmed();
        if (draftDir.isEmpty()) {
            showToast(QStringLiteral("尚未配置剪映草稿目录，请到「设置 → 存储与输出」填写。"));
            return;
        }
        if (!QDir().mkpath(draftDir)) {
            showToast(QStringLiteral("无法创建剪映草稿目录：%1").arg(draftDir));
            return;
        }
        if (!QDesktopServices::openUrl(QUrl::fromLocalFile(draftDir))) {
            showToast(QStringLiteral("无法打开剪映草稿目录：%1").arg(draftDir));
        }
    });
    connect(m_dashboard, &DashboardPage::openSourceRequested, this,
            [this](qint64 sourceId) {
                if (!m_repository || !m_repository->isOpen()) {
                    showToast(QStringLiteral("本地数据库不可用，无法打开项目。"));
                    return;
                }
                QString error;
                const MediaSource source = m_repository->source(sourceId, &error);
                if (source.id < 0) {
                    showToast(error);
                    return;
                }
                if (source.draftCount > 0) {
                    openResultsForSource(sourceId);
                    return;
                }
                navigateTo(QStringLiteral("material"));
                showToast(source.isRecording()
                              ? (source.isOnlineDownload()
                                     ? QStringLiteral("下载完成后即可开始一键成片。")
                                     : QStringLiteral("录制完成后即可开始一键成片。"))
                              : QStringLiteral("「%1」还没有剪映草稿，可在视频库开始一键成片。")
                                    .arg(source.title));
            });

    connect(m_material, &MaterialPage::addSourceRequested, this,
            [this](const QString &tab) { openAddSource(tab); });
    connect(m_material, &MaterialPage::openResultsRequested, this,
            &MainWindow::openResultsForSource);
    connect(m_material, &MaterialPage::startPipelineRequested, this,
            [this](qint64 sourceId) { openStartPipeline(sourceId); });
    connect(m_material, &MaterialPage::previewVideoRequested, this,
            &MainWindow::openVideoPreview);
    connect(m_material, &MaterialPage::stopRecordingRequested, this, [this](qint64 sourceId) {
        if (!m_recording->stopRecording(sourceId)) {
            showToast(QStringLiteral("未找到正在进行的录制任务。"));
        }
    });
    connect(m_material, &MaterialPage::deleteSourceRequested, this,
            &MainWindow::deleteSource);
    connect(m_material, &MaterialPage::toastRequested, this, &MainWindow::showToast);

    connect(m_recording.get(), &RecordingService::sourceAdded, this,
            [this](qint64) { refreshMaterialLibrary(); });
    connect(m_recording.get(), &RecordingService::recordingProgress, this,
            [this](qint64 sourceId, qint64 durationMs) {
                m_material->updateRecordingProgress(sourceId, durationMs);
            });
    connect(m_recording.get(), &RecordingService::downloadProgress, this,
            [this](qint64 sourceId, qint64 receivedBytes, qint64 totalBytes) {
                m_material->updateDownloadProgress(sourceId, receivedBytes, totalBytes);
            });
    connect(m_recording.get(), &RecordingService::downloadStatus, this,
            [this](qint64 sourceId, const QString &text) {
                m_material->updateDownloadStatus(sourceId, text);
            });
    connect(m_recording.get(), &RecordingService::recordingFinished, this,
            [this](qint64, bool success, const QString &message) {
                refreshMaterialLibrary();
                QString toastText = message;
                if (toastText.isEmpty()) {
                    toastText = success ? QStringLiteral("处理完成")
                                        : QStringLiteral("处理失败");
                }
                if (toastText.size() > 120) {
                    toastText = toastText.left(117) + QStringLiteral("…");
                }

                const AppConfig config =
                    m_configStore ? m_configStore->config() : AppConfig();
                if (config.general.soundOnComplete) {
                    QApplication::beep();
                }
                if (config.general.systemNotifications) {
                    ensureTray();
                    if (m_tray) {
                        m_tray->show();
                        m_tray->showMessage(QStringLiteral("LiveMix Studio"), toastText,
                                            QSystemTrayIcon::Information, 4000);
                    }
                }
                showToast(toastText);
            });

    connect(m_results, &ResultsPage::backRequested, this,
            [this] { navigateTo(QStringLiteral("material")); });
    connect(m_results, &ResultsPage::restartRequested, this, [this] {
        openStartPipeline(m_resultsSourceId >= 0 ? m_resultsSourceId : m_lastSourceId);
    });
    connect(m_results, &ResultsPage::openDraftRequested, this,
            &MainWindow::requestDraftDownload);
    connect(m_results, &ResultsPage::toastRequested, this, &MainWindow::showToast);

    connect(m_pipeline, &PipelinePage::toastRequested, this, &MainWindow::showToast);
    connect(m_pipeline, &PipelinePage::backgroundRequested, this, [this] {
        showToast(QStringLiteral("已转后台运行，完成后通知你"));
        navigateTo(QStringLiteral("dash"));
    });
    connect(m_pipeline, &PipelinePage::viewResultsRequested, this, [this] {
        refreshResults();
        navigateTo(QStringLiteral("workspace"));
    });
    connect(m_pipeline, &PipelinePage::rerunRequested, this, [this] {
        if (m_lastSourceId < 0 || m_lastPromptText.trimmed().isEmpty()) {
            openStartPipeline(m_lastSourceId);
            return;
        }
        startClipPipeline(m_lastSourceId, m_lastPromptText, m_lastPromptLabel);
    });
    connect(m_pipeline, &PipelinePage::startRunRequested, this,
            [this] { openStartPipeline(); });
    connect(m_pipeline, &PipelinePage::materialRequested, this,
            [this] { navigateTo(QStringLiteral("material")); });

    connect(m_prompt, &PromptPage::newPromptRequested, this,
            [this] { openPromptEditor(-1); });
    connect(m_prompt, &PromptPage::editPromptRequested, this, &MainWindow::openPromptEditor);
    connect(m_prompt, &PromptPage::viewPromptRequested, this, &MainWindow::openPromptViewer);
    connect(m_prompt, &PromptPage::deletePromptRequested, this, [this](int index) {
        const QVector<Demo::PromptEntry> prompts = Demo::prompts();
        if (index >= 0 && index < prompts.size()) {
            showToast(QStringLiteral("已删除提示词 %1").arg(prompts[index].name));
        }
    });

    connect(m_tasks, &TasksPage::viewTaskRequested, this, &MainWindow::openTask);
    connect(m_tasks, &TasksPage::retryTaskRequested, this, &MainWindow::retryTask);
    connect(m_tasks, &TasksPage::refreshRequested, this, &MainWindow::refreshTasks);
    connect(m_tasks, &TasksPage::toastRequested, this, &MainWindow::showToast);

    connect(m_settings, &SettingsPage::toastRequested, this, &MainWindow::showToast);
    connect(m_settings, &SettingsPage::settingsSaved, this, &MainWindow::applySettings);
}

void MainWindow::buildOverlays()
{
    m_modalOverlay = new ModalOverlay(m_shell);
    m_fullEditor = new FullscreenEditor(m_shell);
    m_toast = new Toast(m_shell);

    connect(m_modalOverlay, &ModalOverlay::closed, this, [this] {
        m_downloadDialog = nullptr;
        m_downloadDialogDraftId = -1;
    });
    connect(m_fullEditor, &FullscreenEditor::saveRequested, this, [this] {
        showToast(QStringLiteral("提示词已保存"));
        m_fullEditor->closeEditor();
    });
}

void MainWindow::navigateTo(const QString &pageId)
{
    if (!m_pageIndexes.contains(pageId)) {
        return;
    }
    const int index = m_pageIndexes.value(pageId);
    m_stack->setCurrentIndex(index);
    if (auto *page = qobject_cast<Page *>(m_stack->widget(index))) {
        page->scrollToTop();
    }
    if (pageId == QLatin1String("tasks")) {
        refreshTasks();
    } else if (pageId == QLatin1String("material")) {
        // 草稿数量会随成片任务变化，进入视频库时重新读取以更新卡片入口。
        refreshMaterialLibrary();
    }

    static const QSet<QString> primaryPages = {
        QStringLiteral("dash"), QStringLiteral("material"), QStringLiteral("prompt"),
        QStringLiteral("tasks"), QStringLiteral("settings"), QStringLiteral("spec"),
    };
    m_sideBar->setActivePage(primaryPages.contains(pageId) ? pageId : QString());

    // macOS 用品牌后的短后缀；Windows 用「文档名 - LiveMix Studio」。
    QString documentName;
    QString projectLabel;
    if (pageId == QLatin1String("workspace")) {
        // 标题栏展示当前结果页对应的源视频，避免沿用界面演示时的固定项目名。
        QString projectName;
        if (m_resultsSourceId >= 0 && m_repository && m_repository->isOpen()) {
            QString error;
            projectName = m_repository->source(m_resultsSourceId, &error).title.trimmed();
        }
        if (projectName.isEmpty()) {
            documentName = QStringLiteral("全部剪映草稿");
            projectLabel = QStringLiteral("· 全部剪映草稿");
        } else {
            documentName = projectName;
            const QString elided =
                QFontMetrics(Theme::font(12)).elidedText(projectName, Qt::ElideRight, 320);
            projectLabel = QStringLiteral("· %1").arg(elided);
        }
    } else if (pageId == QLatin1String("material")) {
        documentName = QStringLiteral("源视频库");
        projectLabel = QStringLiteral("· 源视频库");
    } else if (pageId == QLatin1String("pipeline")) {
        documentName = QStringLiteral("一键成片");
        projectLabel = QStringLiteral("· 一键成片");
    } else if (pageId == QLatin1String("prompt")) {
        documentName = QStringLiteral("提示词管理");
    } else if (pageId == QLatin1String("tasks")) {
        documentName = QStringLiteral("任务中心");
        projectLabel = QStringLiteral("· 任务中心");
    } else if (pageId == QLatin1String("settings")) {
        documentName = QStringLiteral("设置");
        projectLabel = QStringLiteral("· 设置");
    } else if (pageId == QLatin1String("spec")) {
        documentName = QStringLiteral("设计说明");
        projectLabel = QStringLiteral("· 设计说明");
    } else {
        documentName = QStringLiteral("工作台");
    }
    m_shell->titleBar()->setDocumentTitle(documentName, projectLabel);
    if (Theme::isWindowsWindowStyle()) {
        setWindowTitle(QStringLiteral("%1 - LiveMix Studio").arg(documentName));
    } else {
        setWindowTitle(QStringLiteral("LiveMix Studio · 直播切片工作台"));
    }
}

void MainWindow::showPageForPreview(const QString &pageId)
{
    navigateTo(pageId);
}

void MainWindow::setPreviewLocalFile(const QString &path)
{
    m_previewLocalFile = path;
}

void MainWindow::showModalForPreview(const QString &modalId)
{
    if (modalId == QLatin1String("add")) {
        openAddSource(QStringLiteral("link"));
    } else if (modalId == QLatin1String("add-local")) {
        openAddSource(QStringLiteral("local"), m_previewLocalFile);
    } else if (modalId == QLatin1String("start")) {
        openStartPipeline();
    } else if (modalId == QLatin1String("prompt")) {
        openPromptEditor(-1);
    } else if (modalId == QLatin1String("viewer")) {
        openPromptViewer(1);
    } else if (modalId.startsWith(QLatin1String("download"))) {
        showDraftDownloadPreview(modalId);
    }
}

void MainWindow::showFullscreenForPreview(bool readOnly)
{
    QVector<Demo::PromptEntry> prompts = Demo::prompts();
    if (!prompts.isEmpty()) {
        openFullscreenPrompt(prompts[1].name, prompts[1].note, prompts[1].content, readOnly);
    }
}

void MainWindow::showPipelineStepForPreview(int step)
{
    navigateTo(QStringLiteral("pipeline"));
    m_pipeline->showStep(step);
}

void MainWindow::openAddSource(const QString &initialTab, const QString &localFilePath)
{
    auto *card = new ModalCard(QStringLiteral("添加源视频"), 540);

    auto *tabs = new SegmentTabs({QStringLiteral("线上链接"), QStringLiteral("本地文件")},
                                 SegmentTabs::SoftTabs, card);
    auto *pages = new QStackedWidget(card);
    pages->setStyleSheet(QStringLiteral("QStackedWidget { background:transparent; }"));

    auto *linkPane = new QWidget(pages);
    auto *linkLayout = new QVBoxLayout(linkPane);
    linkLayout->setContentsMargins(0, 0, 0, 0);
    linkLayout->setSpacing(16);
    auto *urlInput = new InputField(linkPane);
    urlInput->setPlaceholder(
        QStringLiteral("粘贴 m3u8 / mp4 链接，如 https://.../live.m3u8"));
    auto *urlField = new FormField(
        QStringLiteral("视频地址"), urlInput,
        QStringLiteral("mp4 直链直接下载原文件；m3u8 / 直播流会录制为本地 HLS。"),
        true, linkPane);
    linkLayout->addWidget(urlField);
    auto *nameInput = new InputField(linkPane);
    nameInput->setPlaceholder(QStringLiteral("不填则自动取流标题"));
    auto *nameField = new FormField(QStringLiteral("名称（选填）"), nameInput, QString(), false,
                                    linkPane);
    linkLayout->addWidget(nameField);
    pages->addWidget(linkPane);

    auto *localPane = new QWidget(pages);
    auto *localLayout = new QVBoxLayout(localPane);
    localLayout->setContentsMargins(0, 0, 0, 0);
    localLayout->setSpacing(16);
    auto *dropZone = new DropZone(localPane);
    dropZone->setRejectHandler([this](const QString &message) { showToast(message); });
    localLayout->addWidget(dropZone);

    // 选中文件后展示视频缩略图与时长/分辨率/体积，让用户确认当前选择。
    auto *preview = new LocalVideoPreview(localPane);
    preview->setVisible(false);
    localLayout->addWidget(preview);
    dropZone->setFileSelectedHandler([this, preview](const QString &path) {
        preview->setFile(path);
        preview->setVisible(true);
        // 弹窗已经显示时，预览出现后需要重新计算高度并居中，避免内容重叠。
        if (m_modalOverlay) {
            m_modalOverlay->relayout();
        }
    });

    auto *noteInput = new InputField(localPane);
    noteInput->setPlaceholder(QStringLiteral("选填，便于后续搜索识别"));
    auto *noteField = new FormField(QStringLiteral("备注"), noteInput, QString(), false,
                                    localPane);
    localLayout->addWidget(noteField);
    pages->addWidget(localPane);
    connect(tabs, &SegmentTabs::currentChanged, this,
            [pages](int index) { pages->setCurrentIndex(index); });
    if (initialTab == QLatin1String("local")) {
        tabs->setCurrentIndex(1);
    }
    pages->setCurrentIndex(initialTab == QLatin1String("local") ? 1 : 0);

    card->contentLayout()->setSpacing(14);
    card->contentLayout()->addWidget(tabs, 0, Qt::AlignLeft);
    card->contentLayout()->addWidget(pages);

    auto *cancel = new Button(QStringLiteral("取消"), Button::Default, card);
    auto *add = new Button(QStringLiteral("添加"), Button::Primary, card);
    connect(cancel, &QAbstractButton::clicked, this, [this] { m_modalOverlay->closeModal(); });
    connect(add, &QAbstractButton::clicked, this,
            [this, pages, urlInput, nameInput, dropZone, noteInput] {
                if (pages->currentIndex() == 0) {
                    const QString input = urlInput->text().trimmed();
                    if (input.isEmpty()) {
                        showToast(QStringLiteral("请输入视频地址"));
                        return;
                    }
                    QString url;
                    if (!normalizeStreamUrl(input, &url)) {
                        showToast(QStringLiteral(
                            "仅支持 http / https / rtmp / rtsp 视频地址"));
                        return;
                    }
                    if (startOnlineRecording(url, nameInput->text().trimmed())) {
                        m_modalOverlay->closeModal();
                    }
                } else {
                    const QString path = dropZone->filePath();
                    if (path.isEmpty()) {
                        showToast(QStringLiteral("请选择本地视频文件"));
                        return;
                    }
                    if (importLocalVideo(path, noteInput->text().trimmed())) {
                        m_modalOverlay->closeModal();
                    }
                }
            });
    card->addFooterButton(cancel);
    card->addFooterButton(add);

    m_modalOverlay->showModal(card);

    if (!localFilePath.isEmpty() && QFileInfo::exists(localFilePath)) {
        // 开发预览：等弹窗完成布局后再选中文件，同时验证动态展开逻辑。
        QTimer::singleShot(250, dropZone, [dropZone, localFilePath] {
            dropZone->setFilePath(localFilePath);
        });
    }
}

bool MainWindow::startOnlineRecording(const QString &uri, const QString &title)
{
    if (!m_recording) {
        showToast(QStringLiteral("录制服务不可用。"));
        return false;
    }
    const RecordingService::StartResult result = m_recording->startOnlineRecording(uri, title);
    if (!result.ok) {
        showToast(QStringLiteral("无法开始录制：%1").arg(result.error));
        return false;
    }
    showToast(QStringLiteral("已开始获取视频，完成后保存到本地。"));
    refreshMaterialLibrary();
    return true;
}

bool MainWindow::importLocalVideo(const QString &filePath, const QString &note)
{
    if (!isSupportedVideoFile(filePath)) {
        showToast(QStringLiteral("仅支持视频文件：%1").arg(videoFormatHint()));
        return false;
    }
    if (QFileInfo(filePath).size() > kMaxLocalVideoBytes) {
        showToast(QStringLiteral("单个本地视频不能超过 10 GB。"));
        return false;
    }
    if (!m_repository || !m_repository->isOpen()) {
        showToast(QStringLiteral("本地数据库不可用，无法导入视频。"));
        return false;
    }

    QApplication::setOverrideCursor(Qt::WaitCursor);
    const ProbeInfo probe = MediaProbe::probe(filePath);
    QApplication::restoreOverrideCursor();

    if (!probe.ok) {
        showToast(QStringLiteral("导入失败：%1").arg(probe.error));
        return false;
    }

    const QString title = QFileInfo(filePath).fileName();
    QString error;
    const qint64 sourceId =
        m_repository->importLocalSource(title, filePath, note, probe, &error);
    if (sourceId < 0) {
        showToast(QStringLiteral("导入失败：%1").arg(error));
        return false;
    }
    refreshMaterialLibrary();
    showToast(QStringLiteral("已导入 %1").arg(title));
    return true;
}

void MainWindow::refreshMaterialLibrary()
{
    if (!m_material || !m_repository || !m_repository->isOpen()) {
        return;
    }
    QString error;
    const QVector<MediaSource> sources = m_repository->sources(&error);
    if (!error.isEmpty()) {
        showToast(error);
    }
    m_material->setSources(sources);
    if (m_sideBar) {
        m_sideBar->setBadge(QStringLiteral("material"),
                            sources.isEmpty() ? QString()
                                              : QString::number(sources.size()));
    }
    scheduleDashboardRefresh();
}

void MainWindow::openResultsForSource(qint64 sourceId)
{
    m_resultsSourceId = sourceId;
    if (sourceId >= 0) {
        m_lastSourceId = sourceId;
    }
    refreshResults();
    navigateTo(QStringLiteral("workspace"));
}

void MainWindow::refreshResults()
{
    if (!m_results || !m_repository || !m_repository->isOpen()) {
        return;
    }
    QString error;
    const QVector<ClipDraft> drafts =
        m_repository->clipDrafts(m_resultsSourceId, &error);
    if (!error.isEmpty()) {
        qWarning() << "Failed to load clip drafts:" << error;
    }
    m_results->setDrafts(drafts);
    scheduleDashboardRefresh();
}

void MainWindow::refreshTasks()
{
    if (!m_tasks || !m_repository || !m_repository->isOpen()) {
        return;
    }
    QString error;
    const QVector<ClipTask> tasks = m_repository->clipTasks(&error);
    if (!error.isEmpty()) {
        qWarning() << "Failed to load clip tasks:" << error;
    }
    m_tasks->setTasks(tasks);

    int running = 0;
    for (const ClipTask &task : tasks) {
        if (task.status == ClipTaskStatus::Running) {
            ++running;
        }
    }
    if (m_sideBar) {
        m_sideBar->setBadge(QStringLiteral("tasks"),
                            running > 0 ? QString::number(running) : QString());
    }
    if (m_shell) {
        m_shell->statusBar()->setQueueCount(running);
    }
    scheduleDashboardRefresh();
}

void MainWindow::scheduleDashboardRefresh()
{
    if (!m_dashboard || m_dashboardRefreshPending) {
        return;
    }
    m_dashboardRefreshPending = true;
    // 一次操作可能连续触发素材、草稿、任务三处刷新，合并到下一轮事件循环只读一次库。
    QTimer::singleShot(0, this, [this] {
        m_dashboardRefreshPending = false;
        refreshDashboard();
    });
}

void MainWindow::refreshDashboard()
{
    if (!m_dashboard) {
        return;
    }
    if (!m_repository || !m_repository->isOpen()) {
        m_dashboard->setData({}, {}, {},
                             m_databaseError.isEmpty()
                                 ? QStringLiteral("本地数据库尚未打开。")
                                 : m_databaseError);
        if (m_shell) {
            m_shell->statusBar()->setDraftCount(-1);
        }
        return;
    }

    QString error;
    const QVector<MediaSource> sources = m_repository->sources(&error);
    if (!error.isEmpty()) {
        qWarning() << "Failed to load dashboard sources:" << error;
    }
    error.clear();
    const QVector<ClipDraft> drafts = m_repository->clipDrafts(-1, &error);
    if (!error.isEmpty()) {
        qWarning() << "Failed to load dashboard drafts:" << error;
    }
    error.clear();
    const QVector<ClipTask> tasks = m_repository->clipTasks(&error);
    if (!error.isEmpty()) {
        qWarning() << "Failed to load dashboard tasks:" << error;
    }
    if (m_shell) {
        m_shell->statusBar()->setDraftCount(drafts.size());
    }
    m_dashboard->setData(sources, drafts, tasks);
}

void MainWindow::openTask(qint64 taskId)
{
    if (!m_repository || !m_repository->isOpen()) {
        showToast(QStringLiteral("本地数据库不可用，无法打开任务。"));
        return;
    }
    QString error;
    const ClipTask task = m_repository->clipTask(taskId, &error);
    if (task.id < 0) {
        showToast(error);
        return;
    }

    if (task.status == ClipTaskStatus::Running) {
        navigateTo(QStringLiteral("pipeline"));
        // 当前任务仍在本次会话中运行：页面已有完整过程数据，直接返回即可。
        if (m_pipeline && !(m_clipTaskActive && taskId == m_clipTaskId)) {
            const qint64 elapsedMs =
                qMax<qint64>(0, QDateTime::currentMSecsSinceEpoch() - task.createdAtMs);
            m_pipeline->showTaskSummary(task.title, task.step, task.message,
                                        task.progress, elapsedMs);
        }
    } else if (task.status == ClipTaskStatus::Completed) {
        m_resultsSourceId = task.sourceId;
        m_lastSourceId = task.sourceId;
        refreshResults();
        navigateTo(QStringLiteral("workspace"));
    } else {
        showToast(task.message.isEmpty() ? QStringLiteral("任务执行失败。") : task.message);
    }
}

void MainWindow::retryTask(qint64 taskId)
{
    if (!m_repository || !m_repository->isOpen()) {
        showToast(QStringLiteral("本地数据库不可用，无法重试任务。"));
        return;
    }
    QString error;
    const ClipTask task = m_repository->clipTask(taskId, &error);
    if (task.id < 0) {
        showToast(error);
        return;
    }
    openStartPipeline(task.sourceId);
}

void MainWindow::startClipPipeline(qint64 sourceId, const QString &promptText,
                                   const QString &promptLabel)
{
    if (m_clipThread) {
        showToast(QStringLiteral("已有一键成片任务在运行，请等待完成。"));
        return;
    }
    if (!m_repository || !m_repository->isOpen()) {
        showToast(QStringLiteral("本地数据库不可用，无法开始任务。"));
        return;
    }

    const AppConfig config = m_configStore ? m_configStore->config() : AppConfig();
    if (config.service.apiKey.trimmed().isEmpty()) {
        showToast(QStringLiteral("尚未配置 API 密钥，请到「设置 → 服务连接」填写。"));
        return;
    }

    // 先把任务落库，任务中心即可立即看到并回看进度。
    QString taskError;
    const MediaSource source = m_repository->source(sourceId, &taskError);
    if (source.id < 0) {
        showToast(taskError);
        return;
    }
    const qint64 taskId =
        m_repository->createClipTask(sourceId, source.title, &taskError);
    if (taskId < 0) {
        showToast(QStringLiteral("无法创建任务记录：%1").arg(taskError));
        return;
    }
    m_clipTaskId = taskId;
    m_clipTaskActive = true;
    refreshTasks();

    LiveClipRequest request;
    request.sourceId = sourceId;
    request.databasePath = m_repository->databasePath();
    request.apiKey = config.service.apiKey.trimmed();
    request.gatewayBaseUrl = config.service.apiUrl.trimmed();
    request.promptText = promptText;
    request.promptLabel = promptLabel;
    request.cacheDir = config.storage.cacheDir;
    m_lastSourceId = sourceId;
    m_lastPromptText = promptText;
    m_lastPromptLabel = promptLabel;
    // 任务完成后的结果页只展示这次成片的草稿。
    m_resultsSourceId = sourceId;
    qRegisterMetaType<PipelineUi::Event>("PipelineUi::Event");

    auto *service = new LiveClipService(request);
    auto *thread = new QThread;
    service->moveToThread(thread);
    m_clipService = service;
    m_clipThread = thread;

    connect(thread, &QThread::started, service, &LiveClipService::run);
    connect(service, &LiveClipService::progress, this,
            [this](int step, const QString &message, int percent) {
                if (m_clipTaskActive && m_repository) {
                    m_repository->updateClipTask(m_clipTaskId, step, percent, message);
                    refreshTasks();
                }
            });
    connect(service, &LiveClipService::pipelineEvent, this,
            [this](const PipelineUi::Event &event) {
                if (m_pipeline) {
                    m_pipeline->handleEvent(event);
                }
                // 设计稿的「可切到其他页面，完成后通知你」：ASR 完成时
                // 如果用户不在过程页，补一条 Toast，任务仍在后台继续。
                if (event.type == PipelineUi::EventType::AsrFinished && m_stack
                    && m_stack->currentWidget() != m_pipeline) {
                    showToast(QStringLiteral("ASR 转写完成 · %1 句，已开始 AI 选片")
                                  .arg(QLocale(QLocale::English).toString(event.count)));
                }
            });
    connect(service, &LiveClipService::draftCreated, this, [this](qint64 draftRowId) {
        Q_UNUSED(draftRowId);
        if (m_clipTaskActive && m_repository) {
            m_repository->incrementClipTaskDrafts(m_clipTaskId);
            refreshTasks();
        }
    });
    connect(service, &LiveClipService::finished, this,
            [this](bool success, const QString &message) {
                PipelineUi::Event event;
                event.type = PipelineUi::EventType::RunFinished;
                event.flag = success;
                event.text = message;
                if (m_pipeline) {
                    m_pipeline->handleEvent(event);
                }
                if (m_clipTaskActive && m_repository) {
                    m_repository->finishClipTask(
                        m_clipTaskId,
                        success ? ClipTaskStatus::Completed : ClipTaskStatus::Failed,
                        message);
                    m_clipTaskActive = false;
                    m_clipTaskId = -1;
                    refreshTasks();
                }
                // 失败前可能已生成部分草稿，结果页与视频库入口统一刷新一次。
                refreshResults();
                refreshMaterialLibrary();
                if (success) {
                    showToast(message);
                } else {
                    showToast(QStringLiteral("一键成片失败：%1").arg(message));
                }
                if (m_clipThread) {
                    m_clipThread->quit();
                }
            });
    connect(thread, &QThread::finished, this, [this, service, thread] {
        if (m_clipService == service) {
            m_clipService = nullptr;
        }
        if (m_clipThread == thread) {
            m_clipThread = nullptr;
        }
        service->deleteLater();
        thread->deleteLater();
    });

    navigateTo(QStringLiteral("pipeline"));
    m_pipeline->resetRun();
    thread->start();
}

void MainWindow::requestDraftDownload(qint64 draftRowId)
{
    if (!m_repository || !m_repository->isOpen() || !m_downloads) {
        showToast(QStringLiteral("本地数据库不可用，无法下载草稿。"));
        return;
    }

    QString error;
    const ClipDraft draft = m_repository->clipDraft(draftRowId, &error);
    if (draft.id < 0) {
        showToast(error);
        return;
    }

    const AppConfig config = m_configStore ? m_configStore->config() : AppConfig();
    const QString draftDir = config.storage.draftDir.trimmed();
    if (draftDir.isEmpty()) {
        showToast(QStringLiteral("尚未配置剪映草稿目录，请到「设置 → 存储与输出」填写。"));
        return;
    }

    // 已有任务：进行中或失败时重新打开进度弹窗，已完成的直接打开目录。
    if (m_downloads->hasJob(draftRowId)) {
        const DraftDownloadJob job = m_downloads->job(draftRowId);
        if (job.status == DraftDownloadStatus::Done) {
            if (QFileInfo(job.localPath).isDir()) {
                openDraftFolder(job.localPath);
                showToast(FileManager::revealDoneMessage(draft.title));
                return;
            }
            // 草稿目录已被删除：继续往下重新下载。
        } else {
            showDraftDownloadDialog(draftRowId);
            return;
        }
    }

    // 已下载到当前草稿目录：直接打开，避免重复下载。
    if (!draft.downloadedPath.isEmpty()
        && MediaUtils::isWithinDirectory(draftDir, draft.downloadedPath)
        && QFileInfo(draft.downloadedPath).isDir()) {
        openDraftFolder(draft.downloadedPath);
        showToast(FileManager::revealDoneMessage(draft.title));
        return;
    }

    m_downloads->start(draft, m_repository->databasePath(), draftDir);
    showDraftDownloadDialog(draftRowId);
}

void MainWindow::showDraftDownloadDialog(qint64 draftRowId)
{
    if (!m_downloads || !m_modalOverlay) {
        return;
    }
    const DraftDownloadJob job = m_downloads->job(draftRowId);
    if (job.draftRowId < 0) {
        return;
    }

    auto *dialog = new DraftDownloadDialog;
    dialog->setJob(job);
    connect(dialog, &DraftDownloadDialog::cancelRequested, m_downloads,
            &DraftDownloadManager::cancel);
    connect(dialog, &DraftDownloadDialog::retryRequested, m_downloads,
            &DraftDownloadManager::retry);
    connect(dialog, &DraftDownloadDialog::openFolderRequested, this,
            [this](const QString &path) { openDraftFolder(path); });
    connect(dialog, &DraftDownloadDialog::toastRequested, this,
            &MainWindow::showToast);
    connect(dialog, &DraftDownloadDialog::layoutChanged, this, [this] {
        if (m_modalOverlay) {
            m_modalOverlay->relayout();
        }
    });

    m_downloadDialog = dialog;
    m_downloadDialogDraftId = draftRowId;
    m_modalOverlay->showModal(dialog);
}

void MainWindow::openDraftFolder(const QString &path)
{
    if (path.isEmpty() || !QFileInfo(path).isDir()) {
        showToast(QStringLiteral("草稿目录不存在：%1").arg(path));
        return;
    }
    if (!FileManager::revealDraftFolder(path)) {
        showToast(QStringLiteral("无法打开草稿目录：%1").arg(path));
    }
}

void MainWindow::showDraftDownloadPreview(const QString &state)
{
    struct Seed
    {
        const char *path;
        DraftFileKind kind;
        qint64 size;
    };
    const qint64 kb = 1024;
    const qint64 mb = 1024 * kb;
    const Seed seeds[] = {
        {"draft_content.json", DraftFileKind::Config, 186 * kb},
        {"draft_meta_info.json", DraftFileKind::Config, 8 * kb},
        {"draft_info.json", DraftFileKind::Config, 9 * kb},
        {"draft_agency_config.json", DraftFileKind::Config, 2 * kb},
        {"attachment_pc_common.json", DraftFileKind::Config, 1 * kb},
        {"assets/videos/202610221509505958369c_01.mp4", DraftFileKind::Video, 49 * mb},
        {"assets/videos/202610221509505958369c_02.mp4", DraftFileKind::Video, 12 * mb},
        {"assets/audios/202610221509505958369c_voice_01.mp3", DraftFileKind::Audio,
         2 * mb + 614 * kb},
        {"assets/audios/202610221509505958369c_voice_02.mp3", DraftFileKind::Audio,
         3 * mb},
        {"assets/audios/bgm_soft_piano.mp3", DraftFileKind::Audio, 2 * mb},
        {"assets/images/202610221509505958369c_cover.png", DraftFileKind::Image,
         614 * kb},
    };

    const bool doneState = state == QLatin1String("download-done");
    const bool errorState = state == QLatin1String("download-error");
    const bool writingState = state == QLatin1String("download-writing");

    DraftDownloadJob job;
    job.draftRowId = -1;
    job.title = QStringLiteral("面霜机制讲解 + 价格锚点");
    job.draftName = QStringLiteral("202610221509505958369c");
    job.rangeText = QStringLiteral("00:12:04");
    job.durationText = QStringLiteral("61s");

    qint64 loaded = 0;
    qint64 total = 0;
    int doneFiles = 0;
    int index = 0;
    for (const Seed &seed : seeds) {
        DraftFileProgress file;
        file.path = QString::fromLatin1(seed.path);
        file.kind = seed.kind;
        file.totalBytes = seed.size;
        if (doneState || writingState || index < 5) {
            file.status = DraftFileStatus::Done;
            file.loadedBytes = seed.size;
            file.progress = 100;
            ++doneFiles;
        } else if (errorState && index == 5) {
            file.status = DraftFileStatus::Error;
        } else if (!errorState && index == 5) {
            file.status = DraftFileStatus::Downloading;
            file.loadedBytes = seed.size * 42 / 100;
            file.progress = 42;
        } else {
            file.status = DraftFileStatus::Queued;
        }
        loaded += file.loadedBytes;
        total += seed.size;
        job.files.append(file);
        ++index;
    }

    job.loadedBytes = loaded;
    job.totalBytes = total;
    job.doneFiles = doneFiles;
    job.totalFiles = job.files.size();
    if (doneState) {
        job.status = DraftDownloadStatus::Done;
        job.stage = DraftDownloadStage::Done;
        job.progress = 100.0;
        job.localPath = QStringLiteral(
            "~/Movies/JianyingPro/User Data/Projects/com.lveditor.draft/"
            "面霜机制讲解_202610221509505958369c");
    } else if (writingState) {
        job.status = DraftDownloadStatus::Writing;
        job.stage = DraftDownloadStage::Writing;
        job.progress = 94.0;
    } else if (errorState) {
        job.status = DraftDownloadStatus::Error;
        job.stage = DraftDownloadStage::Assets;
        job.progress = 15.0;
        job.error = QStringLiteral("下载失败：网络连接中断（HTTP 504）");
    } else {
        job.status = DraftDownloadStatus::Downloading;
        job.stage = DraftDownloadStage::Assets;
        job.progress = 42.0;
        job.speed = 9.4 * 1024.0 * 1024.0;
        job.etaSeconds = 6;
    }

    if (state == QLatin1String("download-card")) {
        // 预览结果页卡片与状态栏的进行中样式。
        if (m_repository && m_repository->isOpen() && m_results) {
            QString error;
            const QVector<ClipDraft> drafts = m_repository->clipDrafts(-1, &error);
            if (!drafts.isEmpty()) {
                job.draftRowId = drafts.first().id;
                job.title = drafts.first().title;
                job.draftName = drafts.first().draftId;
                m_results->setDownloadJob(job);
                m_shell->statusBar()->setDraftDownloadStatus(
                    QStringLiteral("● 剪映草稿下载中 42%"), false, job.draftRowId);
            }
        }
        navigateTo(QStringLiteral("workspace"));
        return;
    }

    auto *dialog = new DraftDownloadDialog;
    dialog->setJob(job);
    connect(dialog, &DraftDownloadDialog::toastRequested, this, &MainWindow::showToast);
    connect(dialog, &DraftDownloadDialog::layoutChanged, this, [this] {
        if (m_modalOverlay) {
            m_modalOverlay->relayout();
        }
    });
    m_modalOverlay->showModal(dialog);
}

void MainWindow::updateDraftStatusBar()
{
    if (!m_shell) {
        return;
    }
    const QVector<DraftDownloadJob> jobs =
        m_downloads ? m_downloads->jobs() : QVector<DraftDownloadJob>();

    int activeIndex = -1;
    int failedIndex = -1;
    for (int i = 0; i < jobs.size(); ++i) {
        if (activeIndex < 0 && jobs.at(i).isRunning()) {
            activeIndex = i;
        } else if (failedIndex < 0
                   && jobs.at(i).status == DraftDownloadStatus::Error) {
            failedIndex = i;
        }
    }

    const int index = activeIndex >= 0 ? activeIndex : failedIndex;
    if (index < 0) {
        m_shell->statusBar()->clearDraftDownloadStatus();
        return;
    }

    const DraftDownloadJob &job = jobs.at(index);
    QString text;
    if (job.status == DraftDownloadStatus::Error) {
        text = QStringLiteral("● 剪映草稿下载失败");
    } else if (job.status == DraftDownloadStatus::Writing) {
        text = QStringLiteral("● 剪映草稿写入中 %1%")
                   .arg(qMin(99, int(job.progress)));
    } else {
        text = QStringLiteral("● 剪映草稿下载中 %1%")
                   .arg(qMin(99, int(job.progress)));
    }
    m_shell->statusBar()->setDraftDownloadStatus(
        text, job.status == DraftDownloadStatus::Error, job.draftRowId);
}

void MainWindow::deleteSource(qint64 sourceId)
{
    if (m_recording && m_recording->isRecording(sourceId)) {
        showToast(QStringLiteral("录制进行中，请先停止录制。"));
        return;
    }
    if (!m_repository || !m_repository->isOpen()) {
        showToast(QStringLiteral("本地数据库不可用，无法删除。"));
        return;
    }
    QString error;
    if (!m_repository->removeSource(sourceId, &error)) {
        showToast(QStringLiteral("删除失败：%1").arg(error));
        return;
    }
    if (sourceId == m_resultsSourceId) {
        m_resultsSourceId = -1;
    }
    refreshMaterialLibrary();
    // 草稿、ASR 缓存与任务记录通过外键级联删除，相关页面需要同步刷新。
    refreshResults();
    refreshTasks();
    showToast(QStringLiteral("已从视频库移除，本地文件保留。"));
}

void MainWindow::openVideoPreview(qint64 sourceId)
{
    if (!m_repository || !m_repository->isOpen()) {
        showToast(QStringLiteral("本地数据库不可用，无法播放视频。"));
        return;
    }
    QString error;
    const MediaSource source = m_repository->source(sourceId, &error);
    if (source.id < 0) {
        showToast(error);
        return;
    }
    if (source.localPath.isEmpty() || !QFileInfo::exists(source.localPath)) {
        showToast(QStringLiteral("视频文件不存在，无法播放。"));
        return;
    }
    auto *card = new VideoPreviewCard(source.title, source.localPath);
    m_modalOverlay->showModal(card);
}

void MainWindow::openStartPipeline(qint64 sourceId)
{
    if (!m_repository || !m_repository->isOpen()) {
        showToast(QStringLiteral("本地数据库不可用，无法开始一键成片。"));
        return;
    }

    // 指定了源视频就直接使用；否则选最近一个已就绪且文件仍存在的视频。
    MediaSource source;
    if (sourceId >= 0) {
        QString error;
        source = m_repository->source(sourceId, &error);
        if (source.id < 0) {
            showToast(error);
            return;
        }
    } else {
        const QVector<MediaSource> sources = m_repository->sources();
        for (const MediaSource &candidate : sources) {
            if (candidate.isReady() && !candidate.localPath.isEmpty()
                && QFileInfo::exists(candidate.localPath)) {
                source = candidate;
                break;
            }
        }
        if (source.id < 0) {
            showToast(QStringLiteral("请先在视频库导入本地视频，或完成一次下载/录制。"));
            return;
        }
    }

    if (!source.isReady()) {
        showToast(QStringLiteral("该视频尚未就绪，请等待下载或录制完成。"));
        return;
    }
    if (!QFileInfo::exists(source.localPath)) {
        showToast(QStringLiteral("源视频文件不存在：%1").arg(source.localPath));
        return;
    }
    m_lastSourceId = source.id;

    auto *card = new ModalCard(QStringLiteral("一键成片"), 440);

    auto *videoInput = new InputField(card);
    videoInput->setText(QStringLiteral("%1 · %2")
                            .arg(source.title,
                                 MediaUtils::formatDuration(source.durationMs)));
    videoInput->setReadOnly(true);
    auto *videoField = new FormField(
        QStringLiteral("视频"), videoInput,
        QStringLiteral("先提取 MP3 并调用 ASR；单段转写不超过 30 分钟，更长时按总时长"
                       "均衡切成多段（如 40 分钟 → 2 段、66 分钟 → 3 段），切点尽量"
                       "落在句号上。"),
        false, card);
    card->contentLayout()->addWidget(videoField);

    auto *promptGroup = new QWidget(card);
    auto *promptLayout = new QVBoxLayout(promptGroup);
    promptLayout->setContentsMargins(0, 0, 0, 0);
    promptLayout->setSpacing(6);
    promptLayout->addWidget(
        Ui::label(QStringLiteral("使用提示词"), 12, 400, Theme::Text2, promptGroup));
    auto *select = new SelectBox(promptGroup);
    const QVector<Demo::PromptEntry> prompts = Demo::prompts();
    for (const Demo::PromptEntry &prompt : prompts) {
        select->addItem(prompt.name);
    }
    const AppConfig config = m_configStore ? m_configStore->config() : AppConfig();
    const int defaultPromptIndex = select->findText(config.ai.defaultPrompt);
    if (defaultPromptIndex >= 0) {
        select->setCurrentIndex(defaultPromptIndex);
    }
    promptLayout->addWidget(select);
    auto *hintRow = new QWidget(promptGroup);
    auto *hintLayout = new QHBoxLayout(hintRow);
    hintLayout->setContentsMargins(0, 0, 0, 0);
    hintLayout->setSpacing(4);
    hintLayout->addWidget(
        Ui::label(QStringLiteral("提示词决定 AI 怎么选片段。"), 11, 400, Theme::Text3,
                  hintRow));
    auto *manage = new LinkLabel(QStringLiteral("管理提示词 →"), hintRow);
    manage->setPixelSize(11);
    connect(manage, &QAbstractButton::clicked, this, [this] {
        m_modalOverlay->closeModal();
        navigateTo(QStringLiteral("prompt"));
    });
    hintLayout->addWidget(manage);
    hintLayout->addStretch(1);
    promptLayout->addWidget(hintRow);
    card->contentLayout()->addWidget(promptGroup);

    card->contentLayout()->addWidget(makeNotice(
        QStringLiteral("流程：提取 MP3 → ASR 转写（缓存到本地）→ 按转写时长均衡切成"
                       "每段不超过 30 分钟的多段，由 AI 按提示词选出若干片段并合并成成片"
                       " → 裁剪并生成剪映草稿。"
                       "草稿只在点击「%1」时下载。")
            .arg(FileManager::revealLabel()),
        card));

    auto *cancel = new Button(QStringLiteral("取消"), Button::Default, card);
    auto *start = new Button(QStringLiteral("开始"), Button::Primary, card);
    connect(cancel, &QAbstractButton::clicked, this, [this] { m_modalOverlay->closeModal(); });
    connect(start, &QAbstractButton::clicked, this, [this, source, select] {
        const QVector<Demo::PromptEntry> entries = Demo::prompts();
        QString promptText;
        const int index = select->currentIndex();
        if (index >= 0 && index < entries.size()) {
            promptText = entries.at(index).content;
        }
        const QString promptLabel = select->currentText();
        m_modalOverlay->closeModal();
        startClipPipeline(source.id, promptText, promptLabel);
    });
    card->addFooterButton(cancel);
    card->addFooterButton(start);

    m_modalOverlay->showModal(card);
}

void MainWindow::openPromptEditor(int index)
{
    const QVector<Demo::PromptEntry> prompts = Demo::prompts();
    if (index >= 0 && index < prompts.size()) {
        const Demo::PromptEntry &entry = prompts[index];
        // 内置提示词不可编辑，即使从编辑入口进入也保持只读。
        openFullscreenPrompt(entry.name, entry.note, entry.content, entry.builtIn);
        return;
    }
    openFullscreenPrompt(QString(), QString(), QString(), false, true);
}

void MainWindow::openPromptViewer(int index)
{
    const QVector<Demo::PromptEntry> prompts = Demo::prompts();
    if (index < 0 || index >= prompts.size()) {
        return;
    }
    const Demo::PromptEntry &entry = prompts[index];
    // 支持编辑的提示词直接进入可编辑页面，内置提示词保持只读。
    openFullscreenPrompt(entry.name, entry.note, entry.content, entry.builtIn);
}

void MainWindow::openFullscreenPrompt(const QString &name, const QString &note,
                                      const QString &content, bool readOnly, bool creating)
{
    m_fullEditor->openPrompt(name, note, content, readOnly, creating);
    m_fullEditor->raise();
}

void MainWindow::showToast(const QString &message)
{
    if (!m_toast) {
        return;
    }
    m_toast->raise();
    m_toast->showMessage(message);
}

void MainWindow::toggleMaximize()
{
    if (isMaximized() || isFullScreen()) {
        showNormal();
    } else {
        showMaximized();
    }
}

void MainWindow::applyWindowState()
{
    if (!m_shell) {
        return;
    }
    const bool maximized = isMaximized() || isFullScreen();
    m_shell->titleBar()->setMaximized(maximized);
    if (maximized == m_maximized) {
        return;
    }
    m_maximized = maximized;
    if (auto *layout = this->layout()) {
        const int margin = maximized ? 0 : m_shadowMargin;
        layout->setContentsMargins(margin, margin, margin, margin);
    }
    m_shell->setRounded(!maximized);
    updateResizeHandles();
    update();
}

void MainWindow::createResizeHandles()
{
    auto add = [this](Qt::Edges edges, Qt::CursorShape cursor) {
        auto *handle = new ResizeHandle(edges, cursor, this);
        handle->setAttribute(Qt::WA_TransparentForMouseEvents, false);
        m_resizeHandles.append(handle);
        handle->show();
        handle->raise();
    };
    add(Qt::LeftEdge | Qt::TopEdge, Qt::SizeFDiagCursor);
    add(Qt::RightEdge | Qt::TopEdge, Qt::SizeBDiagCursor);
    add(Qt::LeftEdge | Qt::BottomEdge, Qt::SizeBDiagCursor);
    add(Qt::RightEdge | Qt::BottomEdge, Qt::SizeFDiagCursor);
    add(Qt::TopEdge, Qt::SizeVerCursor);
    add(Qt::BottomEdge, Qt::SizeVerCursor);
    add(Qt::LeftEdge, Qt::SizeHorCursor);
    add(Qt::RightEdge, Qt::SizeHorCursor);
    updateResizeHandles();
}

void MainWindow::updateResizeHandles()
{
    if (m_resizeHandles.size() < 8) {
        return;
    }
    const bool visible = !m_maximized;
    for (QWidget *handle : m_resizeHandles) {
        handle->setVisible(visible);
    }
    if (!visible) {
        return;
    }
    const int corner = m_shadowMargin;
    const int edge = 8;
    const int w = width();
    const int h = height();
    m_resizeHandles[0]->setGeometry(0, 0, corner, corner);
    m_resizeHandles[1]->setGeometry(w - corner, 0, corner, corner);
    m_resizeHandles[2]->setGeometry(0, h - corner, corner, corner);
    m_resizeHandles[3]->setGeometry(w - corner, h - corner, corner, corner);
    m_resizeHandles[4]->setGeometry(corner, 0, w - 2 * corner, edge);
    m_resizeHandles[5]->setGeometry(corner, h - edge, w - 2 * corner, edge);
    m_resizeHandles[6]->setGeometry(0, corner, edge, h - 2 * corner);
    m_resizeHandles[7]->setGeometry(w - edge, corner, edge, h - 2 * corner);
    for (QWidget *handle : m_resizeHandles) {
        handle->raise();
    }
}

void MainWindow::resizeEvent(QResizeEvent *event)
{
    QWidget::resizeEvent(event);
    updateResizeHandles();
    if (m_toast) {
        m_toast->reposition();
    }
}

void MainWindow::changeEvent(QEvent *event)
{
    QWidget::changeEvent(event);
    if (event->type() == QEvent::WindowStateChange) {
        applyWindowState();
    }
}

void MainWindow::paintEvent(QPaintEvent *)
{
    if (m_maximized) {
        return;
    }
    QPainter painter(this);
    painter.setRenderHint(QPainter::Antialiasing, true);
    const QRectF shellRect = QRectF(rect()).adjusted(m_shadowMargin, m_shadowMargin,
                                                     -m_shadowMargin, -m_shadowMargin);
    // Windows 11 的窗口阴影比 macOS 更收敛，圆角也更小。
    const bool windows = Theme::isWindowsWindowStyle();
    const int radius = Theme::windowCornerRadius();
    const int layers = windows ? 10 : 13;
    painter.setBrush(Qt::NoBrush);
    for (int i = layers; i >= 0; --i) {
        const qreal expansion = i + 1.0;
        QColor shadow(0, 0, 0, (windows ? 3 : 4) + (layers - i));
        painter.setPen(QPen(shadow, 2.0));
        const QRectF r = shellRect.adjusted(-expansion, -expansion + 3, expansion,
                                            expansion + 3);
        painter.drawRoundedRect(r, radius + expansion, radius + expansion);
    }
}
