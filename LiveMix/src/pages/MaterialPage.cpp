// 文件作用：源视频库页面，展示本地导入/在线下载/直播录制视频，
// 并发出开始一键成片的源视频 ID。
#include "MaterialPage.h"

#include "../core/MediaUtils.h"
#include "../media/FfmpegLocator.h"
#include "../media/FfmpegUtils.h"
#include "../ui/Controls.h"
#include "../ui/Icons.h"

#include <QDir>
#include <QEnterEvent>
#include <QFileInfo>
#include <QGridLayout>
#include <QHBoxLayout>
#include <QLabel>
#include <QLineEdit>
#include <QPainter>
#include <QPainterPath>
#include <QPointer>
#include <QProcess>
#include <QResizeEvent>
#include <QTimer>
#include <QVBoxLayout>

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

class SearchBox : public QWidget
{
public:
    explicit SearchBox(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        setFixedSize(280, 33);
        m_edit = new QLineEdit(this);
        m_edit->setPlaceholderText(QStringLiteral("搜索视频名称"));
        m_edit->setFont(Theme::font(12));
        m_edit->setStyleSheet(QStringLiteral(
            "QLineEdit { background: transparent; border: none; padding: 0; color: #E9ECF4;"
            " selection-background-color:#4A63BC; selection-color:#FFFFFF; }"));
        m_edit->setAttribute(Qt::WA_MacShowFocusRect, false);
        auto *layout = new QHBoxLayout(this);
        layout->setContentsMargins(30, 1, 11, 1);
        layout->setSpacing(0);
        layout->addWidget(m_edit);
    }

    QLineEdit *lineEdit() const { return m_edit; }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        painter.setBrush(Theme::Surface2);
        painter.setPen(QPen(Theme::Border, 1.0));
        painter.drawRoundedRect(QRectF(rect()).adjusted(0.5, 0.5, -0.5, -0.5), 8, 8);
        Icons::paint(&painter, QRectF(11, (height() - 13) / 2.0, 13, 13),
                     Icons::Glyph::Search, Theme::Text3, 2.0);
    }

private:
    QLineEdit *m_edit = nullptr;
};

QString sourceChipLabel(const MediaSource &source)
{
    if (source.kind == SourceKind::Local) {
        return QStringLiteral("本地导入");
    }
    return source.isOnlineDownload() ? QStringLiteral("在线下载")
                                     : QStringLiteral("直播录制");
}

bool matchesFilter(const MediaSource &source, const QString &filter)
{
    return filter.isEmpty()
        || source.title.contains(filter, Qt::CaseInsensitive)
        || source.note.contains(filter, Qt::CaseInsensitive);
}

// 异步生成视频库卡片封面：命中缓存直接回调，否则后台调用 ffmpeg 截帧。
class PosterLoader : public QObject
{
public:
    PosterLoader(const QString &videoPath, qint64 positionMs,
                 std::function<void(const QImage &)> onReady, QObject *parent)
        : QObject(parent)
        , m_videoPath(videoPath)
        , m_onReady(std::move(onReady))
    {
        m_cachePath = FfmpegUtils::thumbnailCachePath(videoPath);
        if (QFileInfo::exists(m_cachePath)) {
            const QImage cached(m_cachePath);
            if (!cached.isNull()) {
                QTimer::singleShot(0, this, [this, cached] {
                    if (m_onReady) {
                        m_onReady(cached);
                    }
                });
                return;
            }
        }
        start(positionMs);
    }

    ~PosterLoader() override
    {
        if (m_process) {
            m_process->disconnect(this);
            if (m_process->state() != QProcess::NotRunning) {
                m_process->kill();
                m_process->waitForFinished(1000);
            }
        }
    }

private:
    void start(qint64 positionMs)
    {
        const QString ffmpeg = FfmpegLocator::ffmpegPath();
        if (ffmpeg.isEmpty() || !QDir().mkpath(QFileInfo(m_cachePath).absolutePath())) {
            return;
        }

        auto *process = new QProcess(this);
        m_process = process;
        connect(process, QOverload<int, QProcess::ExitStatus>::of(&QProcess::finished),
                this, [this, process](int exitCode, QProcess::ExitStatus) {
                    if (exitCode == 0 && QFileInfo::exists(m_cachePath)) {
                        const QImage image(m_cachePath);
                        if (!image.isNull() && m_onReady) {
                            m_onReady(image);
                        }
                    }
                    process->deleteLater();
                });
#ifdef Q_OS_WIN
        process->setCreateProcessArgumentsModifier(
            [](QProcess::CreateProcessArguments *args) {
                args->flags |= CREATE_NO_WINDOW;
            });
#endif
        const double seconds = qMax<qint64>(0, positionMs) / 1000.0;
        const QStringList args = {
            QStringLiteral("-y"),
            QStringLiteral("-nostdin"),
            QStringLiteral("-hide_banner"),
            QStringLiteral("-loglevel"),
            QStringLiteral("error"),
            QStringLiteral("-ss"),
            QString::number(seconds, 'f', 3),
            QStringLiteral("-i"),
            m_videoPath,
            QStringLiteral("-frames:v"),
            QStringLiteral("1"),
            QStringLiteral("-vf"),
            QStringLiteral("scale=640:-2"),
            m_cachePath,
        };
        process->start(ffmpeg, args);
    }

    QString m_videoPath;
    QString m_cachePath;
    std::function<void(const QImage &)> m_onReady;
    QPointer<QProcess> m_process;
};

class MaterialCard : public Card
{
public:
    using Callback = std::function<void()>;

    MaterialCard(const MediaSource &source, QWidget *parent, Callback openResults,
                 Callback startPipeline, Callback previewVideo, Callback stopRecording,
                 Callback remove)
        : Card(Theme::Surface1, Theme::Border, 10, parent)
        , m_recording(source.isRecording())
        , m_download(source.isOnlineDownload())
        , m_canDelete(!source.isRecording())
        , m_openResults(std::move(openResults))
        , m_startPipeline(std::move(startPipeline))
        , m_previewVideo(std::move(previewVideo))
        , m_stopRecording(std::move(stopRecording))
        , m_remove(std::move(remove))
    {
        setHoverColors(Theme::Surface1, Theme::BorderStrong);
        setClickable(source.isReady());
        contentLayout()->setContentsMargins(1, 1, 1, 1);
        contentLayout()->setSpacing(0);

        m_thumb = new MediaThumbnail(m_recording ? MediaThumbnail::Recording
                                                 : MediaThumbnail::Cool,
                                     this);
        m_thumb->setTopRadius(9);
        if (m_recording) {
            m_thumb->setRecordingLabel(m_download ? QStringLiteral("DL · 0%")
                                                  : QStringLiteral("REC · 00:00"));
        } else {
            m_thumb->setDuration(MediaUtils::formatDuration(source.durationMs));
        }
        m_thumb->installEventFilter(this);
        contentLayout()->addWidget(m_thumb);

        auto *body = new QWidget(this);
        auto *bodyLayout = new QVBoxLayout(body);
        bodyLayout->setContentsMargins(12, 10, 12, 12);
        bodyLayout->setSpacing(0);

        auto *name = new ElidedLabel(source.title, body);
        name->setPixelSize(13);
        name->setFontWeight(600);
        name->setTextColor(Theme::Text1);
        bodyLayout->addWidget(name);
        bodyLayout->addSpacing(6);

        auto *metaRow = new QWidget(body);
        auto *metaLayout = new QHBoxLayout(metaRow);
        metaLayout->setContentsMargins(0, 0, 0, 0);
        metaLayout->setSpacing(6);
        if (m_recording) {
            auto *live = new Chip(
                m_download ? QStringLiteral("在线下载中")
                           : QStringLiteral("直播录制中"),
                Chip::Live, metaRow);
            live->setBlinking(true);
            metaLayout->addWidget(live);
        } else if (source.isFailed()) {
            metaLayout->addWidget(new Chip(
                m_download ? QStringLiteral("下载失败")
                           : QStringLiteral("录制失败"),
                Chip::Error, metaRow));
        } else {
            metaLayout->addWidget(new Chip(sourceChipLabel(source), Chip::Neutral, metaRow));
        }
        metaLayout->addStretch(1);
        bodyLayout->addWidget(metaRow);
        bodyLayout->addSpacing(8);

        if (source.isReady() && !source.errorMessage.isEmpty()) {
            auto *warning = Ui::label(source.errorMessage, 11, 400, Theme::Warn, body);
            warning->setWordWrap(true);
            bodyLayout->addWidget(warning);
            bodyLayout->addSpacing(6);
        }

        if (m_recording) {
            auto *hint = Ui::label(
                m_download
                    ? QStringLiteral("正在下载源视频；完成后到视频库点「一键成片」开始 AI 切片")
                    : QStringLiteral(
                          "录制仅保存到本地；停止后到视频库点「一键成片」开始 AI 切片"),
                11, 400, Theme::Text3, body);
            hint->setWordWrap(true);
            bodyLayout->addWidget(hint);
            bodyLayout->addStretch(1);

            m_stopButton = new Button(m_download ? QStringLiteral("取消下载")
                                                 : QStringLiteral("停止录制"),
                                      Button::Danger, this);
            m_stopButton->setCompact(true);
            m_stopButton->setCornerRadius(5);
            m_stopButton->raise();
            connect(m_stopButton, &QAbstractButton::clicked, this, [this] {
                if (m_stopRecording) {
                    m_stopRecording();
                }
            });
        } else if (source.isFailed()) {
            const QString message = source.errorMessage.isEmpty()
                ? QStringLiteral("未生成有效文件。")
                : source.errorMessage;
            auto *hint = Ui::label(message, 11, 400, Theme::Warn, body);
            hint->setWordWrap(true);
            bodyLayout->addWidget(hint);
            bodyLayout->addStretch(1);
        } else {
            auto *statusRow = new QWidget(body);
            auto *statusLayout = new QHBoxLayout(statusRow);
            statusLayout->setContentsMargins(0, 0, 0, 0);
            statusLayout->setSpacing(6);
            auto *bar = new ProgressBar(statusRow);
            bar->setValue(0);
            bar->setBarColor(Theme::Primary);
            statusLayout->addWidget(bar, 1);
            auto *statusLabel = Ui::label(QStringLiteral("待转写"), 11, 400, Theme::Text3,
                                          statusRow);
            statusLayout->addWidget(statusLabel, 0, Qt::AlignVCenter);
            bodyLayout->addWidget(statusRow);
            bodyLayout->addSpacing(10);

            // 已经生成过草稿的视频，入口改为查看草稿；重新成片在结果页操作。
            const bool hasDrafts = source.draftCount > 0;
            auto *action = new Button(
                hasDrafts ? QStringLiteral("查看草稿") : QStringLiteral("一键成片"),
                hasDrafts ? Button::Default : Button::Primary, body);
            action->setFixedHeight(30);
            connect(action, &QAbstractButton::clicked, this, [this, hasDrafts] {
                if (hasDrafts) {
                    if (m_openResults) {
                        m_openResults();
                    }
                } else if (m_startPipeline) {
                    m_startPipeline();
                }
            });
            bodyLayout->addWidget(action);

            // 封面 + 播放按钮：点击后在应用内播放该源视频。
            if (!source.localPath.isEmpty() && QFileInfo::exists(source.localPath)) {
                m_playButton = new GlyphButton(Icons::Glyph::Play, 40, this);
                m_playButton->setGlyphSize(18);
                m_playButton->setColors(Qt::white, Qt::white);
                m_playButton->setBackgroundColors(QColor(0, 0, 0, 150),
                                                  Theme::alpha(Theme::Primary, 0.95));
                m_playButton->setToolTipText(QStringLiteral("播放视频预览"));
                // 供开发预览按文本触发；按钮本身仍只绘制图标。
                m_playButton->setText(QStringLiteral("播放预览"));
                m_playButton->setAccessibleName(QStringLiteral("播放视频预览"));
                connect(m_playButton, &QAbstractButton::clicked, this, [this] {
                    if (m_previewVideo) {
                        m_previewVideo();
                    }
                });

                const qint64 positionMs =
                    source.durationMs > 3000 ? 1000
                                             : qMax<qint64>(0, source.durationMs / 3);
                new PosterLoader(source.localPath, positionMs,
                                 [this](const QImage &image) {
                                     if (m_thumb) {
                                         m_thumb->setPreviewImage(image);
                                     }
                                 },
                                 this);
            }
        }
        contentLayout()->addWidget(body);

        m_deleteButton = new GlyphButton(Icons::Glyph::Trash, 24, this);
        m_deleteButton->setGlyphSize(12);
        m_deleteButton->setColors(Qt::white, Qt::white);
        m_deleteButton->setBackgroundColors(QColor(0, 0, 0, 153), Theme::Danger);
        m_deleteButton->setToolTipText(QStringLiteral("从视频库移除（保留本地文件）"));
        m_deleteButton->hide();
        connect(m_deleteButton, &QAbstractButton::clicked, this, [this] {
            if (m_remove) {
                m_remove();
            }
        });

        connect(this, &Card::clicked, this, [this] {
            if (m_openResults) {
                m_openResults();
            }
        });
    }

    void setRecordingDuration(qint64 durationMs)
    {
        if (m_thumb && m_recording && !m_download) {
            m_thumb->setRecordingLabel(
                QStringLiteral("REC · %1").arg(MediaUtils::formatDuration(durationMs)));
        }
    }

    void setDownloadProgress(qint64 receivedBytes, qint64 totalBytes)
    {
        if (!m_thumb || !m_recording || !m_download) {
            return;
        }
        QString label;
        if (totalBytes > 0) {
            const int percent = static_cast<int>(
                qMin<qint64>(100, qMax<qint64>(0, receivedBytes * 100 / totalBytes)));
            label = QStringLiteral("DL · %1%").arg(percent);
        } else {
            label = QStringLiteral("DL · %1")
                        .arg(MediaUtils::formatByteSize(receivedBytes));
        }
        m_thumb->setRecordingLabel(label);
    }

    void setDownloadStatus(const QString &text)
    {
        if (m_thumb && m_recording && m_download && !text.isEmpty()) {
            m_thumb->setRecordingLabel(text);
        }
    }

protected:
    void enterEvent(QEnterEvent *event) override
    {
        Card::enterEvent(event);
        if (m_canDelete) {
            m_deleteButton->show();
            m_deleteButton->raise();
        }
        positionOverlays();
    }

    void leaveEvent(QEvent *event) override
    {
        Card::leaveEvent(event);
        m_deleteButton->hide();
    }

    void resizeEvent(QResizeEvent *event) override
    {
        Card::resizeEvent(event);
        positionOverlays();
    }

    bool eventFilter(QObject *watched, QEvent *event) override
    {
        if (watched == m_thumb && event->type() == QEvent::Resize) {
            positionOverlays();
        }
        return Card::eventFilter(watched, event);
    }

private:
    void positionOverlays()
    {
        m_deleteButton->move(width() - 6 - m_deleteButton->width(), 6);
        m_deleteButton->raise();
        if (m_stopButton) {
            const QRect thumb = m_thumb->geometry();
            m_stopButton->adjustSize();
            const int x = thumb.right() - 8 - m_stopButton->width();
            const int y = thumb.bottom() - 8 - m_stopButton->height();
            m_stopButton->move(x, y);
            m_stopButton->raise();
        }
        if (m_playButton) {
            const QRect thumb = m_thumb->geometry();
            m_playButton->move(thumb.center().x() - m_playButton->width() / 2,
                               thumb.center().y() - m_playButton->height() / 2);
            m_playButton->raise();
        }
    }

    bool m_recording = false;
    bool m_download = false;
    bool m_canDelete = false;
    Callback m_openResults;
    Callback m_startPipeline;
    Callback m_previewVideo;
    Callback m_stopRecording;
    Callback m_remove;
    MediaThumbnail *m_thumb = nullptr;
    GlyphButton *m_deleteButton = nullptr;
    GlyphButton *m_playButton = nullptr;
    Button *m_stopButton = nullptr;
};

} // namespace

MaterialPage::MaterialPage(QWidget *parent)
    : Page(parent)
{
    auto *header = new PageHeader(
        QStringLiteral("我的视频"),
        QStringLiteral("录制直播或导入本地视频，AI 自动转写、切片、成片。"), this);

    auto *search = new SearchBox(this);
    m_searchEdit = search->lineEdit();
    header->addAction(search);

    auto *record = new Button(QStringLiteral("录制直播"), Button::DangerSoft, this);
    record->setLeadingGlyph(Icons::Glyph::Record);
    connect(record, &QAbstractButton::clicked, this,
            [this] { emit addSourceRequested(QStringLiteral("link")); });
    header->addAction(record);

    auto *import = new Button(QStringLiteral("导入本地视频"), Button::Primary, this);
    import->setLeadingGlyph(Icons::Glyph::Plus);
    connect(import, &QAbstractButton::clicked, this,
            [this] { emit addSourceRequested(QStringLiteral("local")); });
    header->addAction(import);

    bodyLayout()->addWidget(header);

    auto *emptyCard = new Card(Theme::Surface1, Theme::Border, 10, this);
    m_emptyState = emptyCard;
    auto *emptyLayout = emptyCard->contentLayout();
    emptyLayout->setContentsMargins(24, 38, 24, 38);
    emptyLayout->setSpacing(8);
    auto *emptyTitle = Ui::label(QStringLiteral("还没有源视频"), 15, 600, Theme::Text1,
                                 m_emptyState);
    emptyTitle->setAlignment(Qt::AlignCenter);
    emptyLayout->addWidget(emptyTitle);
    auto *emptyHint = Ui::label(
        QStringLiteral("点击右上角「录制直播」或「导入本地视频」开始。"),
        12, 400, Theme::Text3, m_emptyState);
    emptyHint->setAlignment(Qt::AlignCenter);
    emptyHint->setWordWrap(true);
    emptyLayout->addWidget(emptyHint);
    bodyLayout()->addWidget(m_emptyState);

    m_gridHost = new QWidget(this);
    m_grid = new QGridLayout(m_gridHost);
    m_grid->setContentsMargins(0, 0, 0, 0);
    m_grid->setHorizontalSpacing(14);
    m_grid->setVerticalSpacing(14);
    m_gridHost->setVisible(false);
    bodyLayout()->addWidget(m_gridHost);
    bodyLayout()->addStretch(1);

    connect(m_searchEdit, &QLineEdit::textChanged, this, [this](const QString &text) {
        m_filter = text.trimmed();
        applyFilter();
    });
}

void MaterialPage::setSources(const QVector<MediaSource> &sources)
{
    m_sources = sources;
    rebuildCards();
}

void MaterialPage::updateRecordingProgress(qint64 sourceId, qint64 durationMs)
{
    QWidget *widget = m_cards.value(sourceId, nullptr);
    if (auto *card = dynamic_cast<MaterialCard *>(widget)) {
        card->setRecordingDuration(durationMs);
    }
}

void MaterialPage::updateDownloadProgress(qint64 sourceId, qint64 receivedBytes,
                                          qint64 totalBytes)
{
    QWidget *widget = m_cards.value(sourceId, nullptr);
    if (auto *card = dynamic_cast<MaterialCard *>(widget)) {
        card->setDownloadProgress(receivedBytes, totalBytes);
    }
}

void MaterialPage::updateDownloadStatus(qint64 sourceId, const QString &text)
{
    QWidget *widget = m_cards.value(sourceId, nullptr);
    if (auto *card = dynamic_cast<MaterialCard *>(widget)) {
        card->setDownloadStatus(text);
    }
}

void MaterialPage::rebuildCards()
{
    m_cards.clear();
    while (QLayoutItem *item = m_grid->takeAt(0)) {
        if (QWidget *widget = item->widget()) {
            widget->hide();
            widget->deleteLater();
        }
        delete item;
    }

    for (int i = 0; i < m_sources.size(); ++i) {
        const MediaSource source = m_sources.at(i);
        auto *card = new MaterialCard(
            source, m_gridHost,
            [this, id = source.id] { emit openResultsRequested(id); },
            [this, id = source.id] { emit startPipelineRequested(id); },
            [this, id = source.id] { emit previewVideoRequested(id); },
            [this, id = source.id] { emit stopRecordingRequested(id); },
            [this, id = source.id] { emit deleteSourceRequested(id); });
        m_grid->addWidget(card, i / 4, i % 4);
        m_cards.insert(source.id, card);
        card->setVisible(matchesFilter(source, m_filter));
    }
    for (int column = 0; column < 4; ++column) {
        m_grid->setColumnStretch(column, 1);
    }

    m_emptyState->setVisible(m_sources.isEmpty());
    m_gridHost->setVisible(!m_sources.isEmpty());
}

void MaterialPage::applyFilter()
{
    for (const MediaSource &source : m_sources) {
        if (QWidget *widget = m_cards.value(source.id, nullptr)) {
            widget->setVisible(matchesFilter(source, m_filter));
        }
    }
}
