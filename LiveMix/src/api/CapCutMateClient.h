#pragma once

// 文件作用：剪映小助手（capcut-mate）接口客户端。
// 负责文件上传、创建草稿、添加视频与字幕，以及点击「打开草稿文件夹」时的草稿下载。

#include "../services/DraftDownloadJob.h"

#include <QString>
#include <QStringList>
#include <QVector>

#include <functional>

class HttpClient;

// 草稿时间轴上的一段视频；start/end 为拼接后的累计时间（微秒）。
struct CapCutVideoInfo
{
    QString url;
    qint64 startUs = 0;
    qint64 endUs = 0;
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

    // 批量添加视频，多个切片按 start/end 顺序铺在同一条主视频轨上。
    bool addVideos(const QString &draftUrl, const QVector<CapCutVideoInfo> &videos,
                   QString *error);

    bool addCaptions(const QString &draftUrl, const QVector<CapCutCaption> &captions,
                     const CapCutCaptionStyle &style, QString *error);

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
