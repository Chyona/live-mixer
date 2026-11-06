#pragma once

// 文件作用：源视频的应用内播放弹窗。
// 使用 Qt Multimedia 播放本地视频；HLS 录像先转封装开头一小段立即播放，
// 完整文件在后台继续生成并自动切换，同时提供点击/拖动进度定位。

#include "Overlays.h"

#include <QByteArray>
#include <QString>

class QAudioOutput;
class QEvent;
class GlyphButton;
class QGraphicsScene;
class QGraphicsVideoItem;
class QGraphicsView;
class QLabel;
class QMediaPlayer;
class QProcess;
class QSlider;

class VideoPreviewCard : public ModalCard
{
    Q_OBJECT

public:
    VideoPreviewCard(const QString &title, const QString &filePath,
                     QWidget *parent = nullptr);
    ~VideoPreviewCard() override;

protected:
    bool eventFilter(QObject *watched, QEvent *event) override;

private:
    void setStatus(const QString &text, bool error);
    void startPlayback(const QString &filePath);
    void prepareHlsPreview(const QString &playlistPath);
    void startHeadPreview(const QString &playlistPath);
    void playHeadPreview(const QString &filePath);
    void startFullPreview();
    void onFullPreviewReady();
    void switchToFullPreview();
    void restoreFullPreview();
    void verifyFullPreviewPosition(int attempt);
    void updateVideoGeometry();

    QMediaPlayer *m_player = nullptr;
    QAudioOutput *m_audio = nullptr;
    QGraphicsView *m_view = nullptr;
    QGraphicsScene *m_scene = nullptr;
    QGraphicsVideoItem *m_videoItem = nullptr;
    QSlider *m_slider = nullptr;
    QLabel *m_current = nullptr;
    QLabel *m_duration = nullptr;
    QLabel *m_status = nullptr;
    GlyphButton *m_playButton = nullptr;
    QProcess *m_prepare = nullptr;      // 开头预览（前 60 秒）的转封装进程
    QProcess *m_fullPrepare = nullptr;  // 完整预览的后台转封装进程
    QString m_hlsPlaylistPath;
    QString m_hlsFullCachePath;
    QString m_prepareTempPath;
    QString m_fullTempPath;
    QByteArray m_prepareOutput;
    QByteArray m_fullOutput;
    qint64 m_prepareDurationMs = 0;
    qint64 m_switchPositionMs = 0;
    qint64 m_seekTargetMs = -1;
    qint64 m_lastKnownPositionMs = 0;
    bool m_headPreviewActive = false;
    bool m_switchingToFull = false;
    bool m_switchResume = false;
    bool m_pendingFullSwitch = false;
    bool m_switchPositionGuardCancelled = false;
    bool m_seeking = false;
    bool m_error = false;
};
