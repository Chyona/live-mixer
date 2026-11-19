// 文件作用：应用内视频播放弹窗实现，负责播放器状态、进度条与错误提示。
#include "VideoPreview.h"

#include "../app/ApplicationStyle.h"
#include "../core/MediaUtils.h"
#include "../media/FfmpegLocator.h"
#include "../media/FfmpegUtils.h"
#include "Controls.h"
#include "Icons.h"

#include <QAudioOutput>
#include <QDebug>
#include <QDir>
#include <QEvent>
#include <QFile>
#include <QFileInfo>
#include <QGraphicsScene>
#include <QGraphicsVideoItem>
#include <QGraphicsView>
#include <QHBoxLayout>
#include <QLabel>
#include <QMediaPlayer>
#include <QMouseEvent>
#include <QProcess>
#include <QShortcut>
#include <QSlider>
#include <QStringList>
#include <QStyle>
#include <QTemporaryFile>
#include <QTimer>
#include <QUrl>
#include <QVBoxLayout>
#include <QVideoSink>

#include <climits>
#include <functional>

#ifdef Q_OS_WIN
#ifndef NOMINMAX
#define NOMINMAX
#endif
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#include <windows.h>
#endif

namespace {

// 进度条允许点击轨道直接跳转，并在按住时连续拖动；点击和拖动都立即定位。
class SeekSlider : public QSlider
{
public:
    explicit SeekSlider(QWidget *parent = nullptr)
        : QSlider(Qt::Horizontal, parent)
    {
    }

    std::function<void(int)> scrubStarted;
    std::function<void(int)> scrubbed;
    std::function<void(int)> scrubFinished;

protected:
    void mousePressEvent(QMouseEvent *event) override
    {
        if (event->button() != Qt::LeftButton) {
            QSlider::mousePressEvent(event);
            return;
        }
        m_dragging = true;
        setSliderDown(true);
        updateValueFromPosition(event->position().x());
        if (scrubStarted) {
            scrubStarted(value());
        }
        event->accept();
    }

    void mouseMoveEvent(QMouseEvent *event) override
    {
        if (!m_dragging) {
            QSlider::mouseMoveEvent(event);
            return;
        }
        updateValueFromPosition(event->position().x());
        if (scrubbed) {
            scrubbed(value());
        }
        event->accept();
    }

    void mouseReleaseEvent(QMouseEvent *event) override
    {
        if (!m_dragging || event->button() != Qt::LeftButton) {
            QSlider::mouseReleaseEvent(event);
            return;
        }
        m_dragging = false;
        updateValueFromPosition(event->position().x());
        setSliderDown(false);
        if (scrubFinished) {
            scrubFinished(value());
        }
        event->accept();
    }

private:
    void updateValueFromPosition(qreal x)
    {
        if (maximum() <= minimum()) {
            return;
        }
        setValue(QStyle::sliderValueFromPosition(
            minimum(), maximum(), qRound(x), qMax(1, width())));
    }

    bool m_dragging = false;
};

// 录制进程被强制结束时播放列表可能没有 ENDLIST，HLS 解复用器会一直等待后续
// 分片。转封装前补一份带 ENDLIST 的临时播放列表，保证完整输入能正常结束。
QTemporaryFile *createEndedPlaylist(const QString &playlistPath,
                                    QString *playlistForRemux)
{
    *playlistForRemux = playlistPath;
    QFile input(playlistPath);
    if (!input.open(QIODevice::ReadOnly)) {
        return nullptr;
    }
    const QByteArray data = input.readAll();
    if (data.contains("#EXT-X-ENDLIST")) {
        return nullptr;
    }

    const QString pattern = QFileInfo(playlistPath).absolutePath()
        + QStringLiteral("/.livemix-preview-XXXXXX.m3u8");
    auto *file = new QTemporaryFile(pattern);
    file->setAutoRemove(true);
    if (!file->open()) {
        delete file;
        return nullptr;
    }
    file->write(data);
    if (!data.endsWith('\n')) {
        file->write("\n");
    }
    file->write("#EXT-X-ENDLIST\n");
    file->close();
    *playlistForRemux = file->fileName();
    return file;
}

// 首次打开先转封装开头一小段，让预览尽快出现；完整文件在后台继续生成。
constexpr qint64 kHeadPreviewDurationMs = 60 * 1000;

QStringList remuxArguments(const QString &playlistPath, const QString &tempPath,
                           qint64 limitMs, double readRate)
{
    QStringList args{
        QStringLiteral("-y"),
        QStringLiteral("-nostdin"),
        QStringLiteral("-hide_banner"),
        QStringLiteral("-loglevel"),
        QStringLiteral("error"),
        QStringLiteral("-progress"),
        QStringLiteral("pipe:1"),
        QStringLiteral("-nostats"),
    };
    if (readRate > 0.0) {
        // 限制输入读取速度，避免完整预览转封装在播放期间占满磁盘。
        args << QStringLiteral("-readrate")
             << QString::number(readRate, 'f', 2);
    }
    args << QStringLiteral("-i") << playlistPath
         // 只保留画面和首条音轨，避免 HLS 中的附加数据流导致 MP4 封装失败。
         << QStringLiteral("-map") << QStringLiteral("0:v:0")
         << QStringLiteral("-map") << QStringLiteral("0:a:0?")
         << QStringLiteral("-c") << QStringLiteral("copy");
    if (limitMs > 0) {
        args << QStringLiteral("-t")
             << QString::number(limitMs / 1000.0, 'f', 3);
    }
    // 本地预览不需要 faststart：把 moov 移到文件头需要对整个文件再做一次
    // 读写，长录像会成倍放大后台转封装的磁盘压力。
    args << QStringLiteral("-f") << QStringLiteral("mp4") << tempPath;
    return args;
}

// 完整预览与 60 秒开头预览并行生成时，按录像时长限速到约 50 秒完成，
// 把后台转封装的磁盘读写摊平，避免拖慢正在播放的开头预览。
double pacedRemuxRate(qint64 durationMs)
{
    constexpr double kTargetSeconds = 50.0;
    if (durationMs <= 0) {
        return 0.0;
    }
    return qMax(1.0, durationMs / 1000.0 / kTargetSeconds);
}

void attachRemuxProgress(QProcess *process, QByteArray *buffer, qint64 durationMs,
                         const std::function<void(int)> &report)
{
    QObject::connect(process, &QProcess::readyReadStandardOutput, process,
                     [process, buffer, durationMs, report] {
                         *buffer += process->readAllStandardOutput();
                         int newline = buffer->indexOf('\n');
                         while (newline >= 0) {
                             const QString line =
                                 QString::fromUtf8(buffer->left(newline)).trimmed();
                             buffer->remove(0, newline + 1);
                             if (durationMs > 0
                                 && (line.startsWith(QLatin1String("out_time_us="))
                                     || line.startsWith(
                                         QLatin1String("out_time_ms=")))) {
                                 bool ok = false;
                                 const qint64 micros =
                                     line.section(QLatin1Char('='), 1, 1)
                                         .toLongLong(&ok);
                                 if (ok) {
                                     report(int(qBound<qint64>(
                                         qint64(0),
                                         micros * 100 / (durationMs * 1000),
                                         qint64(99))));
                                 }
                             }
                             newline = buffer->indexOf('\n');
                         }
                     });
}

void attachRemuxStderr(QProcess *process, const char *context)
{
    QObject::connect(process, &QProcess::readyReadStandardError, process,
                     [process, context] {
                         const QString message =
                             QString::fromLocal8Bit(process->readAllStandardError())
                                 .trimmed();
                         if (!message.isEmpty()) {
                             qWarning() << context << message;
                         }
                     });
}

} // namespace

VideoPreviewCard::VideoPreviewCard(const QString &title, const QString &filePath,
                                   QWidget *parent)
    : ModalCard(title, 820, parent)
{
    // 视频窗口会创建原生渲染表面，父级阴影特效可能影响其绘制，这里不添加阴影。
    setShadowEnabled(false);
    contentLayout()->setSpacing(10);

    auto *videoHost = new QWidget(this);
    videoHost->setFixedHeight(441);
    videoHost->setStyleSheet(QStringLiteral("background:#000;border-radius:8px;"));
    auto *videoLayout = new QVBoxLayout(videoHost);
    videoLayout->setContentsMargins(0, 0, 0, 0);
    videoLayout->setSpacing(0);

    // 使用 QGraphicsVideoItem 而非 QVideoWidget：画面走 Qt 绘制管线，
    // 截图、窗口合成与跨平台表现更一致。
    m_view = new QGraphicsView(videoHost);
    m_view->setFrameShape(QFrame::NoFrame);
    m_view->setHorizontalScrollBarPolicy(Qt::ScrollBarAlwaysOff);
    m_view->setVerticalScrollBarPolicy(Qt::ScrollBarAlwaysOff);
    m_view->setAlignment(Qt::AlignCenter);
    m_view->setContentsMargins(0, 0, 0, 0);
    m_scene = new QGraphicsScene(m_view);
    m_scene->setBackgroundBrush(Qt::black);
    m_videoItem = new QGraphicsVideoItem;
    m_videoItem->setAspectRatioMode(Qt::KeepAspectRatio);
    m_scene->addItem(m_videoItem);
    m_view->setScene(m_scene);
    m_view->viewport()->installEventFilter(this);
    videoLayout->addWidget(m_view);
    contentLayout()->addWidget(videoHost);

    m_status = Ui::label(QString(), 12, 400, Theme::Text2, this);
    m_status->setAlignment(Qt::AlignCenter);
    m_status->setWordWrap(true);
    m_status->setMinimumHeight(18);
    contentLayout()->addWidget(m_status);

    m_player = new QMediaPlayer(this);
    m_audio = new QAudioOutput(this);
    m_audio->setVolume(0.8);
    m_player->setAudioOutput(m_audio);
    m_player->setVideoOutput(m_videoItem);

    // 控制条放在 ModalCard 底栏：播放/暂停 + 当前时间 + 进度 + 总时长。
    auto *footer = footerLayout();
    while (QLayoutItem *item = footer->takeAt(0)) {
        delete item;
    }
    m_playButton = new GlyphButton(Icons::Glyph::Play, 30, this);
    m_playButton->setGlyphSize(14);
    m_playButton->setColors(Theme::Text1, Theme::Primary);
    m_playButton->setBackgroundColors(Theme::Surface3, Theme::alpha(Theme::Primary, 0.18));
    m_playButton->setToolTipText(QStringLiteral("播放 / 暂停（空格）"));
    // 供开发预览按文本触发；按钮本身仍只绘制图标。
    m_playButton->setText(QStringLiteral("播放预览"));
    m_playButton->setAccessibleName(QStringLiteral("播放或暂停视频"));

    m_current = Ui::label(QStringLiteral("00:00"), 11, 400, Theme::Text3, this);
    m_current->setFont(Theme::monoFont(11));
    m_current->setFixedWidth(48);
    m_duration = Ui::label(QStringLiteral("00:00"), 11, 400, Theme::Text3, this);
    m_duration->setFont(Theme::monoFont(11));
    m_duration->setFixedWidth(48);
    m_duration->setAlignment(Qt::AlignRight | Qt::AlignVCenter);

    auto *seekSlider = new SeekSlider(this);
    m_slider = seekSlider;
    m_slider->setRange(0, 0);
    m_slider->setCursor(Qt::PointingHandCursor);
    m_slider->setStyleSheet(QStringLiteral(
        "QSlider::groove:horizontal { height:4px; background:#2A3042; border-radius:2px; }"
        "QSlider::sub-page:horizontal { background:#6E8BFF; border-radius:2px; }"
        "QSlider::handle:horizontal { width:12px; height:12px; margin:-4px 0;"
        " border-radius:6px; background:#E9ECF4; }"));

    footer->addWidget(m_playButton);
    footer->addSpacing(8);
    footer->addWidget(m_current);
    footer->addWidget(m_slider, 1);
    footer->addWidget(m_duration);

    setupPlaybackConnections(m_player);

    seekSlider->scrubStarted = [this](int value) {
        m_seeking = true;
        m_seekTargetMs = qint64(value) * 1000;
        m_lastKnownPositionMs = m_seekTargetMs;
        // 用户主动定位后，交接完成时的位置校验不再纠偏。
        m_switchPositionGuardCancelled = true;
        if (m_handoffActive) {
            // 追帧交接中拖动进度：停止交接，松开后从新位置重新开始。
            abortFullPreviewHandoff(true);
        }
        m_current->setText(MediaUtils::formatDuration(m_seekTargetMs));
        m_player->setPosition(m_seekTargetMs);
    };
    seekSlider->scrubbed = [this](int value) {
        m_seekTargetMs = qint64(value) * 1000;
        m_lastKnownPositionMs = m_seekTargetMs;
        m_current->setText(MediaUtils::formatDuration(m_seekTargetMs));
    };
    seekSlider->scrubFinished = [this](int value) {
        m_seekTargetMs = qint64(value) * 1000;
        m_lastKnownPositionMs = m_seekTargetMs;
        if (m_pendingFullSwitch) {
            // 完整预览已就绪（或在交接中被拖动打断），按松开时的位置重新交接。
            m_pendingFullSwitch = false;
            m_seeking = false;
            m_player->setPosition(m_seekTargetMs);
            if (m_preloadPlayer) {
                beginFullPreviewHandoff();
            } else {
                preloadFullPreview();
            }
            return;
        }
        m_player->setPosition(m_seekTargetMs);
        m_seeking = false;
    };
    connect(this, &ModalCard::closeRequested, this, [this] {
        if (m_player) {
            m_player->stop();
        }
    });
    connect(m_playButton, &QAbstractButton::clicked, this, [this] {
        if (m_error) {
            return;
        }
        if (m_player->playbackState() == QMediaPlayer::PlayingState) {
            m_playIntent = false;
            // 用户主动暂停后停止交接后的位置校验，避免校验逻辑重新播放。
            m_switchPositionGuardCancelled = true;
            m_player->pause();
            return;
        }
        m_playIntent = true;
        // 播完后再次点击播放才从头开始；用户先把进度拖回中间时应从该位置继续。
        if (m_player->mediaStatus() == QMediaPlayer::EndOfMedia
            && m_player->position()
                >= qMax<qint64>(0, m_player->duration() - 500)) {
            m_player->setPosition(0);
        }
        m_player->play();
    });

    auto *space = new QShortcut(QKeySequence(Qt::Key_Space), this);
    space->setContext(Qt::WindowShortcut);
    connect(space, &QShortcut::activated, m_playButton, &QAbstractButton::click);

    QTimer::singleShot(0, this, [this] { updateVideoGeometry(); });

    if (filePath.endsWith(QLatin1String(".m3u8"), Qt::CaseInsensitive)) {
        prepareHlsPreview(filePath);
    } else {
        setStatus(QStringLiteral("正在加载视频…"), false);
        startPlayback(filePath);
    }
}

// 活动播放器的信号统一在这里连接；预加载播放器也使用同一套处理函数，
// 但只有成为 m_player 之后才会更新界面。
void VideoPreviewCard::setupPlaybackConnections(QMediaPlayer *player)
{
    connect(player, &QMediaPlayer::durationChanged, this,
            [this, player](qint64 duration) {
                if (m_player != player) {
                    return;
                }
                const int seconds = static_cast<int>(
                    qMin<qint64>(qMax<qint64>(0, duration / 1000), INT_MAX));
                m_slider->setRange(0, seconds);
                m_duration->setText(MediaUtils::formatDuration(duration));
            });
    connect(player, &QMediaPlayer::positionChanged, this,
            [this, player](qint64 position) {
                if (m_player != player || m_seeking) {
                    return;
                }
                m_seekTargetMs = -1;
                m_lastKnownPositionMs = position;
                const int seconds = static_cast<int>(
                    qMin<qint64>(qMax<qint64>(0, position / 1000), INT_MAX));
                m_slider->setValue(seconds);
                m_current->setText(MediaUtils::formatDuration(position));
            });
    connect(player, &QMediaPlayer::playbackStateChanged, this,
            [this, player](QMediaPlayer::PlaybackState state) {
                if (m_player != player) {
                    return;
                }
                if (!m_holdPausedFrame) {
                    m_playButton->setGlyph(state == QMediaPlayer::PlayingState
                                               ? Icons::Glyph::Pause
                                               : Icons::Glyph::Play);
                }
                if (state == QMediaPlayer::PlayingState && !m_error
                    && !m_headPreviewActive && !m_holdPausedFrame) {
                    m_status->hide();
                }
            });
    connect(player, &QMediaPlayer::mediaStatusChanged, this,
            [this, player](QMediaPlayer::MediaStatus status) {
                if (m_player != player) {
                    return;
                }
                switch (status) {
                case QMediaPlayer::LoadingMedia:
                    setStatus(QStringLiteral("正在加载视频…"), false);
                    break;
                case QMediaPlayer::BufferingMedia:
                    setStatus(QStringLiteral("正在缓冲…"), false);
                    break;
                case QMediaPlayer::LoadedMedia:
                case QMediaPlayer::BufferedMedia:
                    if (m_error) {
                        break;
                    }
                    if (m_headPreviewActive) {
                        if (m_fullPrepare || m_fullPrepareScheduled) {
                            setStatus(
                                QStringLiteral("已加载开头部分，完整预览准备中…"),
                                false);
                        }
                    } else {
                        m_status->hide();
                    }
                    break;
                case QMediaPlayer::EndOfMedia:
                    m_playButton->setGlyph(Icons::Glyph::Play);
                    break;
                case QMediaPlayer::InvalidMedia:
                    setStatus(QStringLiteral("无法播放该视频：格式或编码不受支持。"), true);
                    break;
                default:
                    break;
                }
            });
    connect(player, &QMediaPlayer::errorOccurred, this,
            [this, player](QMediaPlayer::Error, const QString &message) {
                if (m_player != player) {
                    return;
                }
                setStatus(QStringLiteral("播放失败：%1")
                              .arg(message.isEmpty() ? QStringLiteral("未知错误")
                                                     : message),
                          true);
            });
}

bool VideoPreviewCard::eventFilter(QObject *watched, QEvent *event)
{
    if (m_view && watched == m_view->viewport()
        && event->type() == QEvent::Resize) {
        updateVideoGeometry();
    }
    return ModalCard::eventFilter(watched, event);
}

void VideoPreviewCard::updateVideoGeometry()
{
    if (!m_view || !m_scene || !m_videoItem) {
        return;
    }
    const QSize viewport = m_view->viewport()->size();
    if (viewport.isEmpty()) {
        return;
    }
    m_scene->setSceneRect(0, 0, viewport.width(), viewport.height());
    m_videoItem->setSize(QSizeF(viewport));
    m_videoItem->setPos(0, 0);
}

VideoPreviewCard::~VideoPreviewCard()
{
    const auto stopPrepare = [this](QProcess *&process) {
        if (!process) {
            return;
        }
        const bool running = process->state() != QProcess::NotRunning;
        process->disconnect(this);
        if (running) {
            process->kill();
            process->waitForFinished(1000);
        }
        process = nullptr;
    };
    stopPrepare(m_prepare);
    stopPrepare(m_fullPrepare);
    // 关闭弹窗时中断的转封装会留下半截文件，清理后下次重新生成。
    if (!m_prepareTempPath.isEmpty()) {
        QFile::remove(m_prepareTempPath);
    }
    if (!m_fullTempPath.isEmpty()) {
        QFile::remove(m_fullTempPath);
    }
    ++m_handoffGeneration;
    m_handoffActive = false;
    if (m_preloadPlayer) {
        m_preloadPlayer->stop();
        m_preloadPlayer->setSource(QUrl());
    }
    if (m_player) {
        m_player->stop();
        m_player->setSource(QUrl());
    }
}

void VideoPreviewCard::startPlayback(const QString &filePath)
{
    m_playIntent = true;
    m_player->setSource(QUrl::fromLocalFile(filePath));
    QTimer::singleShot(0, this, [this] {
        if (m_player) {
            m_player->play();
        }
    });
}

void VideoPreviewCard::prepareHlsPreview(const QString &playlistPath)
{
    m_hlsPlaylistPath = playlistPath;
    m_hlsFullCachePath = FfmpegUtils::hlsPreviewCachePath(playlistPath);

    // 成片流程已合成完整 MP4 时直接播放它，避免再次转封装。
    const QString recordingMp4 = FfmpegUtils::recordingMp4Path(playlistPath);
    const QFileInfo recordingInfo(recordingMp4);
    if (recordingMp4 != playlistPath && recordingInfo.isFile()
        && recordingInfo.size() > 0) {
        setStatus(QStringLiteral("正在加载视频…"), false);
        startPlayback(recordingMp4);
        return;
    }

    const QFileInfo cached(m_hlsFullCachePath);
    if (cached.exists() && cached.size() > 0) {
        setStatus(QStringLiteral("正在加载视频…"), false);
        startPlayback(m_hlsFullCachePath);
        return;
    }

    const QString ffmpeg = FfmpegLocator::ffmpegPath();
    if (ffmpeg.isEmpty()
        || !QDir().mkpath(QFileInfo(m_hlsFullCachePath).absolutePath())) {
        setStatus(QStringLiteral("无法准备完整预览：未找到 ffmpeg。"), true);
        return;
    }

    m_prepareDurationMs = MediaUtils::hlsPlaylistDurationMs(playlistPath);
    // 短录像直接转封装完整文件即可；时长未知时先加载开头一段更稳妥。
    if (m_prepareDurationMs > 0 && m_prepareDurationMs <= kHeadPreviewDurationMs) {
        startFullPreview();
        return;
    }
    startHeadPreview(playlistPath);
}

void VideoPreviewCard::startHeadPreview(const QString &playlistPath)
{
    const QString cachePath = FfmpegUtils::hlsPreviewHeadCachePath(playlistPath);
    const QFileInfo cached(cachePath);
    if (cached.exists() && cached.size() > 0) {
        playHeadPreview(cachePath);
        return;
    }

    const QString ffmpeg = FfmpegLocator::ffmpegPath();
    if (ffmpeg.isEmpty()
        || !QDir().mkpath(QFileInfo(cachePath).absolutePath())) {
        setStatus(QStringLiteral("无法准备预览：未找到 ffmpeg。"), true);
        return;
    }

    QString inputPlaylist;
    QTemporaryFile *endedPlaylist = createEndedPlaylist(playlistPath, &inputPlaylist);

    // 先写入 .part，成功后再改名为缓存文件，避免中断留下半截内容被当成有效预览。
    const QString tempPath = cachePath + QStringLiteral(".part");
    QFile::remove(tempPath);
    m_prepareTempPath = tempPath;
    m_prepareOutput.clear();
    const qint64 previewMs = m_prepareDurationMs > 0
        ? qMin(m_prepareDurationMs, kHeadPreviewDurationMs)
        : kHeadPreviewDurationMs;
    setStatus(QStringLiteral("正在准备预览（首次打开需转封装）…"), false);
    m_playButton->setEnabled(false);
    m_slider->setEnabled(false);

    m_prepare = new QProcess(this);
    if (endedPlaylist) {
        // 作为子对象托底：即使转封装被中断，临时播放列表也会被清理。
        endedPlaylist->setParent(m_prepare);
    }
    attachRemuxProgress(m_prepare, &m_prepareOutput, previewMs, [this](int percent) {
        setStatus(QStringLiteral("正在准备预览 %1%…").arg(percent), false);
    });
    attachRemuxStderr(m_prepare, "HLS preview remux (head):");
    connect(m_prepare, QOverload<int, QProcess::ExitStatus>::of(&QProcess::finished),
            this, [this, cachePath, endedPlaylist](int exitCode, QProcess::ExitStatus) {
                if (endedPlaylist) {
                    endedPlaylist->remove();
                    endedPlaylist->deleteLater();
                }
                const QString tempPath = m_prepareTempPath;
                m_prepareTempPath.clear();
                m_prepare->deleteLater();
                m_prepare = nullptr;
                if (exitCode != 0 || QFileInfo(tempPath).size() <= 0) {
                    QFile::remove(tempPath);
                    setStatus(QStringLiteral(
                                  "无法生成预览，请尝试一键成片或使用外部播放器。"),
                              true);
                    return;
                }
                // 先清理旧缓存再改名，避免目标已存在时改名失败。
                QFile::remove(cachePath);
                if (!QFile::rename(tempPath, cachePath)) {
                    QFile::remove(tempPath);
                    setStatus(QStringLiteral("无法保存预览缓存。"), true);
                    return;
                }
                m_playButton->setEnabled(true);
                m_slider->setEnabled(true);
                playHeadPreview(cachePath);
            });
#ifdef Q_OS_WIN
    m_prepare->setCreateProcessArgumentsModifier(
        [](QProcess::CreateProcessArguments *args) {
            args->flags |= CREATE_NO_WINDOW;
        });
#endif
    m_prepare->setProgram(ffmpeg);
    m_prepare->setArguments(
        remuxArguments(inputPlaylist, tempPath, kHeadPreviewDurationMs, 0.0));
    m_prepare->start();
}

void VideoPreviewCard::playHeadPreview(const QString &filePath)
{
    m_headPreviewActive = true;
    setStatus(QStringLiteral("已加载开头部分，完整预览准备中…"), false);
    startPlayback(filePath);
    // 先让开头预览进入播放，再启动完整预览的后台转封装，避免首帧加载与
    // 磁盘写入互相影响。
    if (!m_fullPrepareScheduled) {
        m_fullPrepareScheduled = true;
        QTimer::singleShot(500, this, [this] {
            m_fullPrepareScheduled = false;
            if (m_headPreviewActive && !m_fullPrepare && !m_error) {
                startFullPreview();
            }
        });
    }
}

void VideoPreviewCard::startFullPreview()
{
    const QFileInfo cached(m_hlsFullCachePath);
    if (cached.exists() && cached.size() > 0) {
        onFullPreviewReady();
        return;
    }

    const QString ffmpeg = FfmpegLocator::ffmpegPath();
    if (ffmpeg.isEmpty()
        || !QDir().mkpath(QFileInfo(m_hlsFullCachePath).absolutePath())) {
        if (m_headPreviewActive) {
            setStatus(QStringLiteral("完整预览准备失败，当前可播放开头部分。"), false);
        } else {
            setStatus(QStringLiteral("无法准备完整预览：未找到 ffmpeg。"), true);
        }
        return;
    }

    QString inputPlaylist;
    QTemporaryFile *endedPlaylist =
        createEndedPlaylist(m_hlsPlaylistPath, &inputPlaylist);

    const QString tempPath = m_hlsFullCachePath + QStringLiteral(".part");
    QFile::remove(tempPath);
    m_fullTempPath = tempPath;
    m_fullOutput.clear();
    if (!m_headPreviewActive) {
        setStatus(QStringLiteral("正在准备完整预览（首次打开需转封装）…"), false);
        m_playButton->setEnabled(false);
        m_slider->setEnabled(false);
    }

    m_fullPrepare = new QProcess(this);
    if (endedPlaylist) {
        endedPlaylist->setParent(m_fullPrepare);
    }
    attachRemuxProgress(m_fullPrepare, &m_fullOutput, m_prepareDurationMs,
                        [this](int percent) {
                            setStatus(m_headPreviewActive
                                          ? QStringLiteral("完整预览准备中 %1%…")
                                                .arg(percent)
                                          : QStringLiteral("正在准备完整预览 %1%…")
                                                .arg(percent),
                                      false);
                        });
    attachRemuxStderr(m_fullPrepare, "HLS preview remux (full):");
    connect(m_fullPrepare, QOverload<int, QProcess::ExitStatus>::of(&QProcess::finished),
            this, [this, endedPlaylist](int exitCode, QProcess::ExitStatus) {
                if (endedPlaylist) {
                    endedPlaylist->remove();
                    endedPlaylist->deleteLater();
                }
                const QString tempPath = m_fullTempPath;
                m_fullTempPath.clear();
                m_fullPrepare->deleteLater();
                m_fullPrepare = nullptr;
                if (exitCode != 0 || QFileInfo(tempPath).size() <= 0) {
                    QFile::remove(tempPath);
                    if (m_headPreviewActive) {
                        setStatus(
                            QStringLiteral("完整预览准备失败，当前可播放开头部分。"),
                            false);
                    } else {
                        setStatus(QStringLiteral(
                                      "无法生成完整预览，请尝试一键成片或使用外部播放器。"),
                                  true);
                    }
                    return;
                }
                // 先清理旧缓存再改名，避免目标已存在时改名失败。
                QFile::remove(m_hlsFullCachePath);
                if (!QFile::rename(tempPath, m_hlsFullCachePath)) {
                    QFile::remove(tempPath);
                    if (m_headPreviewActive) {
                        setStatus(
                            QStringLiteral("完整预览缓存保存失败，当前可播放开头部分。"),
                            false);
                    } else {
                        setStatus(QStringLiteral("无法保存完整预览缓存。"), true);
                    }
                    return;
                }
                onFullPreviewReady();
            });
#ifdef Q_OS_WIN
    m_fullPrepare->setCreateProcessArgumentsModifier(
        [](QProcess::CreateProcessArguments *args) {
            args->flags |= CREATE_NO_WINDOW;
        });
#endif
    // 开头预览正在播放时，完整预览转封装限速进行，避免磁盘 I/O 抢占。
    const double readRate = m_headPreviewActive
        ? pacedRemuxRate(m_prepareDurationMs)
        : 0.0;
    m_fullPrepare->setProgram(ffmpeg);
    m_fullPrepare->setArguments(
        remuxArguments(inputPlaylist, tempPath, 0, readRate));
    m_fullPrepare->start();
}

void VideoPreviewCard::onFullPreviewReady()
{
    if (!m_headPreviewActive) {
        m_playButton->setEnabled(true);
        m_slider->setEnabled(true);
        setStatus(QStringLiteral("正在加载视频…"), false);
        startPlayback(m_hlsFullCachePath);
        return;
    }
    // 可以立即在后台加载完整预览；用户正在拖动时，加载完成后再延后交接。
    preloadFullPreview();
}

// 完整预览生成后，用第二个播放器在后台加载；开头预览继续正常播放。
void VideoPreviewCard::preloadFullPreview()
{
    if (m_preloadPlayer || !m_headPreviewActive) {
        return;
    }
    const QFileInfo cached(m_hlsFullCachePath);
    if (!cached.isFile() || cached.size() <= 0) {
        setStatus(QStringLiteral("完整预览准备失败，当前可播放开头部分。"), false);
        return;
    }

    m_preloadPlayer = new QMediaPlayer(this);
    m_preloadAudio = new QAudioOutput(this);
    // 追帧期间静音，交接完成后再恢复音量。
    m_preloadAudio->setVolume(0.0);
    m_preloadPlayer->setAudioOutput(m_preloadAudio);
    // 隐藏的视频 sink 用于预热解码器，交接时画面能立即出现。
    m_preloadSink = new QVideoSink(this);
    m_preloadPlayer->setVideoOutput(m_preloadSink);
    setupPlaybackConnections(m_preloadPlayer);

    QMediaPlayer *preload = m_preloadPlayer;
    connect(preload, &QMediaPlayer::mediaStatusChanged, this,
            [this, preload](QMediaPlayer::MediaStatus status) {
                if (m_player == preload) {
                    return; // 已成为活动播放器，由通用处理逻辑接管
                }
                if (status == QMediaPlayer::LoadedMedia
                    || status == QMediaPlayer::BufferedMedia) {
                    m_preloadReady = true;
                    if (m_seeking) {
                        m_pendingFullSwitch = true;
                        return;
                    }
                    beginFullPreviewHandoff();
                } else if (status == QMediaPlayer::InvalidMedia) {
                    discardPreloadPlayer();
                    setStatus(
                        QStringLiteral("完整预览加载失败，当前可播放开头部分。"),
                        false);
                }
            });
    connect(preload, &QMediaPlayer::errorOccurred, this,
            [this, preload](QMediaPlayer::Error, const QString &) {
                if (m_player == preload) {
                    return;
                }
                discardPreloadPlayer();
                if (m_headPreviewActive) {
                    setStatus(
                        QStringLiteral("完整预览加载失败，当前可播放开头部分。"),
                        false);
                }
            });
    preload->setSource(QUrl::fromLocalFile(m_hlsFullCachePath));
}

// 预加载完成后开始交接：开头预览继续播放，完整预览在后台加速追到相同
// 时间点；对齐后只交换音频/视频输出，避免重新加载和定位造成的卡顿。
void VideoPreviewCard::beginFullPreviewHandoff()
{
    if (!m_preloadPlayer || !m_preloadReady || !m_headPreviewActive
        || m_handoffActive) {
        return;
    }
    if (m_seeking) {
        m_pendingFullSwitch = true;
        return;
    }
    const bool playing = m_player->playbackState() == QMediaPlayer::PlayingState;
    const bool ended = m_player->mediaStatus() == QMediaPlayer::EndOfMedia;
    if (!playing) {
        // 用户暂停或开头预览已播完：直接切换，不需要追帧。
        handoffToFullPreview(ended ? m_playIntent : false);
        return;
    }

    setStatus(QStringLiteral("正在切换完整预览…"), false);
    // 拖动进度后 m_seekTargetMs 可能比实际位置更准确；否则用当前/最后位置兜底。
    m_handoffTargetMs = m_seekTargetMs >= 0
        ? m_seekTargetMs
        : qMax(m_player->position(), m_lastKnownPositionMs);
    m_preloadPlayer->setPosition(m_handoffTargetMs);
    m_preloadPlayer->setPlaybackRate(2.0);
    m_preloadPlayer->play();
    m_handoffActive = true;
    // 首次轮询稍晚一些，等后端完成定位并上报真实播放位置（seek 会落到关键帧）。
    scheduleHandoffPoll(++m_handoffGeneration);
}

void VideoPreviewCard::scheduleHandoffPoll(int generation)
{
    QTimer::singleShot(40, this, [this, generation] {
        if (generation != m_handoffGeneration || !m_handoffActive) {
            return;
        }
        if (!m_preloadPlayer || !m_player) {
            m_handoffActive = false;
            return;
        }
        if (m_seeking) {
            abortFullPreviewHandoff(true);
            return;
        }
        if (m_player->playbackState() != QMediaPlayer::PlayingState) {
            // 追帧期间用户暂停，或开头预览播放到结尾。
            const bool ended =
                m_player->mediaStatus() == QMediaPlayer::EndOfMedia;
            abortFullPreviewHandoff(false);
            handoffToFullPreview(ended ? m_playIntent : false);
            return;
        }
        const qint64 headPosition = m_player->position();
        const qint64 fullPosition = m_preloadPlayer->position();
        // 后端刚调用 play() 时可能还上报请求位置，先等它真正越过定位点，
        // 避免按错误位置提前交接。之后允许落后一帧多一点。
        if (fullPosition > m_handoffTargetMs
            && fullPosition + 30 >= headPosition) {
            handoffToFullPreview(true);
            return;
        }
        scheduleHandoffPoll(generation);
    });
}

void VideoPreviewCard::handoffToFullPreview(bool resume)
{
    if (!m_preloadPlayer || !m_preloadReady) {
        return;
    }
    m_handoffActive = false;
    ++m_handoffGeneration;

    QMediaPlayer *full = m_preloadPlayer;
    QMediaPlayer *old = m_player;
    const qint64 target = m_seekTargetMs >= 0
        ? m_seekTargetMs
        : qMax(old->position(), m_lastKnownPositionMs);

    full->setPlaybackRate(1.0);
    if (!resume) {
        full->pause();
        full->setPosition(target);
    } else if (full->playbackState() != QMediaPlayer::PlayingState) {
        full->setPosition(target);
        full->play();
    }

    m_switchPositionMs =
        resume ? qMax<qint64>(0, full->position()) : target;
    if (m_switchPositionMs <= 0) {
        m_switchPositionMs = target;
    }
    m_switchPositionGuardCancelled = false;
    m_seekTargetMs = -1;

    // 交换播放器与输出：预加载播放器已在目标位置附近，画面通常一帧内出现。
    old->pause();
    m_player = full;
    m_preloadPlayer = nullptr;
    full->setVideoOutput(m_videoItem);
    if (m_preloadSink) {
        m_preloadSink->deleteLater();
        m_preloadSink = nullptr;
    }
    // m_preloadAudio 交接后继续作为完整预览的音频输出。
    m_preloadAudio->setVolume(0.8);
    old->setAudioOutput(nullptr);
    old->stop();
    old->deleteLater();
    if (m_audio) {
        // 开头预览的音频输出已不再使用，及时释放音频设备。
        m_audio->deleteLater();
        m_audio = nullptr;
    }

    m_handoffActive = false;
    m_pendingFullSwitch = false;
    m_preloadReady = false;
    m_headPreviewActive = false;
    m_lastKnownPositionMs = m_switchPositionMs;

    const qint64 duration = m_player->duration();
    if (duration > 0) {
        const int seconds = static_cast<int>(
            qMin<qint64>(qMax<qint64>(0, duration / 1000), INT_MAX));
        m_slider->setRange(0, seconds);
        m_duration->setText(MediaUtils::formatDuration(duration));
    }
    const int seconds = static_cast<int>(
        qMin<qint64>(qMax<qint64>(0, m_switchPositionMs / 1000), INT_MAX));
    m_slider->setValue(seconds);
    m_current->setText(MediaUtils::formatDuration(m_switchPositionMs));
    m_playButton->setGlyph(resume ? Icons::Glyph::Pause : Icons::Glyph::Play);
    m_status->hide();
    qInfo() << "Full preview handoff; position" << m_switchPositionMs
            << "resume" << resume;
    if (resume) {
        verifyFullPreviewPosition(0);
        return;
    }

    // 暂停状态下刚接过来的播放器还没有可见帧：静音播放一小段渲染首帧后暂停。
    m_holdPausedFrame = true;
    m_preloadAudio->setVolume(0.0);
    m_player->play();
    QTimer::singleShot(120, this, [this] {
        if (!m_player) {
            return;
        }
        if (!m_playIntent) {
            m_player->pause();
        }
        m_preloadAudio->setVolume(0.8);
        m_holdPausedFrame = false;
        m_lastKnownPositionMs = m_player->position();
        m_playButton->setGlyph(m_player->playbackState()
                                       == QMediaPlayer::PlayingState
                                   ? Icons::Glyph::Pause
                                   : Icons::Glyph::Play);
    });
}

void VideoPreviewCard::abortFullPreviewHandoff(bool markPending)
{
    m_handoffActive = false;
    ++m_handoffGeneration;
    if (m_preloadPlayer) {
        m_preloadPlayer->pause();
        m_preloadPlayer->setPlaybackRate(1.0);
    }
    if (markPending) {
        m_pendingFullSwitch = true;
    }
}

void VideoPreviewCard::discardPreloadPlayer()
{
    m_handoffActive = false;
    ++m_handoffGeneration;
    m_preloadReady = false;
    if (m_preloadPlayer) {
        m_preloadPlayer->stop();
        m_preloadPlayer->setVideoOutput(nullptr);
        m_preloadPlayer->setAudioOutput(nullptr);
        m_preloadPlayer->deleteLater();
        m_preloadPlayer = nullptr;
    }
    if (m_preloadAudio) {
        m_preloadAudio->deleteLater();
        m_preloadAudio = nullptr;
    }
    if (m_preloadSink) {
        m_preloadSink->deleteLater();
        m_preloadSink = nullptr;
    }
    m_pendingFullSwitch = false;
}

void VideoPreviewCard::verifyFullPreviewPosition(int attempt)
{
    constexpr int kMaxAttempts = 24; // 约 6 秒
    if (attempt >= kMaxAttempts || m_switchPositionGuardCancelled
        || m_seeking) {
        return;
    }
    QTimer::singleShot(attempt == 0 ? 120 : 250, this, [this, attempt] {
        if (m_switchPositionGuardCancelled || m_seeking) {
            return;
        }
        const qint64 target = m_switchPositionMs;
        const qint64 position = m_player->position();
        // 定位正常会落在前一个关键帧上（可能回退数秒）；只有被后端重置
        // 回开头时才重新定位，避免大关键帧间隔的视频被反复 seek。
        if (target >= 2000 && position < 1000) {
            qWarning() << "Full preview position reset to" << position
                       << "retrying seek to" << target;
            m_player->setPosition(target);
        }
        if (m_playIntent
            && m_player->mediaStatus() != QMediaPlayer::EndOfMedia
            && m_player->playbackState() == QMediaPlayer::StoppedState) {
            // 个别后端定位后会把播放状态重置为 stopped，确保继续播放。
            m_player->play();
        }
        // 即使当前定位成功也继续观察一小段时间，兜住后端延迟重置位置的情况。
        verifyFullPreviewPosition(attempt + 1);
    });
}

void VideoPreviewCard::setStatus(const QString &text, bool error)
{
    m_error = error;
    if (text.isEmpty()) {
        m_status->hide();
        return;
    }
    m_status->setText(text);
    m_status->setStyleSheet(error ? QStringLiteral("color:#FF7A7A;")
                                  : QStringLiteral("color:#9BA2B5;"));
    m_status->show();
    if (error) {
        m_playButton->setEnabled(false);
        m_slider->setEnabled(false);
    }
}
