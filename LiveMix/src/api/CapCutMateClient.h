#pragma once

// 文件作用：剪映小助手（capcut-mate）接口客户端。
// 负责文件上传、创建草稿、添加视频（含转场）/ 字幕 / 滤镜 / 特效 / 音频，
// 以及点击「打开草稿文件夹」时的草稿下载。

#include "../services/DraftDownloadJob.h"

#include <QString>
#include <QStringList>
#include <QVector>

#include <functional>

class HttpClient;

// 草稿时间轴上的一段视频；start/end 为拼接后的累计时间（微秒）。
// sceneStartUs / sceneEndUs 为变速后的播放轴区间，与 start/end 一起提交为
// add_videos 的 scene_timelines（speed = (end-start)/(sceneEnd-sceneStart)）。
// 原速时不设置（保持 -1），接口按 1.0× 处理。
// 转场挂在相邻两段中的前一段上，由 add_videos 在该片段与下一段之间渲染。
struct CapCutVideoInfo
{
    QString url;
    qint64 startUs = 0;
    qint64 endUs = 0;
    qint64 sceneStartUs = -1;
    qint64 sceneEndUs = -1;
    QString transition;
    qint64 transitionDurationUs = 0;
};

// 草稿时间轴上的一段视频特效；title 须为剪映支持的特效名称。
struct CapCutEffectInfo
{
    QString title;
    qint64 startUs = 0;
    qint64 endUs = 0;
};

// 覆盖一段时间的滤镜；intensity 为 0–100，越低越淡。
struct CapCutFilterInfo
{
    QString title;
    qint64 startUs = 0;
    qint64 endUs = 0;
    double intensity = 100.0;
};

// 草稿音频轨的一段；BGM 短于成片时由调用方按整首长度拆成多段循环铺轨。
// volume 为 0–1 的轨音量。
struct CapCutAudioInfo
{
    QString url;
    qint64 startUs = 0;
    qint64 endUs = 0;
    double volume = 0.1;
};

struct CapCutCaption
{
    qint64 startUs = 0;
    qint64 endUs = 0;
    QString text;
};

// 一批字幕共用的样式；位置单位为画布像素，原点在画布中心（负值向上）。
struct CapCutCaptionStyle
{
    QString font;
    int fontSize = 15;
    QString textColor = QStringLiteral("#ffffff");
    QString borderColor;
    QString inAnimation;
    double transformX = 0.0;
    double transformY = -800.0;
};

struct CapCutDraftInfo
{
    QString draftUrl;
    QString draftId;
};

// gen_video_status 的结果；status 为 pending / processing / completed / failed。
struct CapCutVideoStatus
{
    QString status;
    int progress = 0; // 远端 0–100
    QString videoUrl;
    QString errorMessage;
};

// 草稿下载过程中的细粒度事件：阶段切换、文件入队、单个文件进度/完成/失败。
// 界面据此渲染「下载配置 → 下载素材 → 写入草稿目录」三步进度与文件列表。
struct DraftDownloadEvent
{
    enum class Type {
        StageChanged,
        FileQueued,
        FileProgress,
        FileDone,
        FileFailed,
    };

    Type type = Type::FileQueued;
    DraftDownloadStage stage = DraftDownloadStage::Config;
    DraftFileKind fileKind = DraftFileKind::Config;
    QString path;
    qint64 loadedBytes = 0;
    qint64 totalBytes = -1;
    QString error;
};

using DraftDownloadCallback = std::function<void(const DraftDownloadEvent &)>;

class CapCutMateClient
{
public:
    explicit CapCutMateClient(HttpClient *http);

    // 上传本地文件，返回对象存储的临时 URL。
    bool uploadFile(const QString &filePath, const QString &apiKey, QString *url,
                    QString *error);

    bool createDraft(int width, int height, CapCutDraftInfo *draft, QString *error);

    // 批量添加视频，多个切片按 start/end 顺序铺在同一条主视频轨上；
    // 每段设置 sceneStart/End 时同时提交 scene_timelines 应用变速。
    bool addVideos(const QString &draftUrl, const QVector<CapCutVideoInfo> &videos,
                   QString *error);

    bool addCaptions(const QString &draftUrl, const QVector<CapCutCaption> &captions,
                     const CapCutCaptionStyle &style, QString *error);

    // 添加视频特效轨（add_effects）；每条特效覆盖自己的时间窗。
    bool addEffects(const QString &draftUrl, const QVector<CapCutEffectInfo> &effects,
                    QString *error);

    // 添加滤镜轨（add_filters）；一条滤镜可覆盖整条成片时间轴。
    bool addFilters(const QString &draftUrl, const QVector<CapCutFilterInfo> &filters,
                    QString *error);

    // 添加音频轨（add_audios）；多段按 start/end 顺序铺在同一条音频轨上。
    bool addAudios(const QString &draftUrl, const QVector<CapCutAudioInfo> &audios,
                   QString *error);

    // 云端合成成片：提交 gen_video 后通过 gen_video_status 轮询。
    bool genVideo(const QString &draftUrl, const QString &apiKey, QString *error);
    bool genVideoStatus(const QString &draftUrl, CapCutVideoStatus *status,
                        QString *error);

    // 查询草稿包含的文件列表（草稿生成后调用，或下载前校验）。
    bool fetchDraftFiles(const QString &draftId, QStringList *fileUrls, QString *error,
                         const std::function<bool()> &shouldAbort = {});

    // 下载并本地化草稿：素材 URL 会下载到 assets 子目录，JSON 路径改写为剪映占位符。
    // 先写入草稿根目录下的隐藏暂存目录，完成后原子改名到正式目录，
    // 剪映的目录发现机制无需重启即可收录新草稿。返回草稿所在的本地目录。
    // 失败时保留暂存目录，重试会跳过已完成的素材文件。
    bool downloadDraft(const QString &draftId, const QString &draftTitle,
                       const QString &targetRoot, QString *localPath, QString *error,
                       const DraftDownloadCallback &callback = {},
                       const std::function<bool()> &shouldAbort = {});

    static QString draftIdFromUrl(const QString &draftUrl);

private:
    HttpClient *m_http = nullptr;
};
