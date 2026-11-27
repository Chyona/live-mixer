#pragma once

// 文件作用：智能口播专用的剪映小助手（capcut-mate）客户端。
// 与直播切片的 CapCutMateClient 完全独立，只暴露口播需要的接口：
// 上传、创建草稿、视频/图片/字幕/音频/关键帧、云渲染、草稿下载。
// 所有时间参数在这里统一使用微秒，调用方（OralPipelineService）负责毫秒换算。

#include <QString>
#include <QStringList>
#include <QVector>

#include <functional>

class HttpClient;

namespace Oral {

struct CapCutDraft
{
    QString url; // create_draft 返回的 draft_url，后续写接口必须原样携带
    QString id;
};

struct CapCutVideo
{
    QString url;
    int width = 0;
    int height = 0;
    qint64 startUs = 0;
    qint64 endUs = 0;
    double volume = 1.0;
    QString transition; // 可选；同一轨道相邻片段之间生效
    qint64 transitionDurationUs = 0;
};

struct CapCutImage
{
    QString url;
    int width = 0;
    int height = 0;
    qint64 startUs = 0;
    qint64 endUs = 0;
    QString inAnimation;
    QString outAnimation;
    qint64 inAnimationDurationUs = 0;
    qint64 outAnimationDurationUs = 0;
    QString transition;
    qint64 transitionDurationUs = 0;
};

struct CapCutCaption
{
    qint64 startUs = 0;
    qint64 endUs = 0;
    QString text;
    QString keyword;       // 多个关键词用 | 分隔
    QString keywordColor;  // 关键词颜色
    int keywordFontSize = 0; // 0 表示使用正文字号
    int fontSize = 0;      // 0 表示使用接口级字号
    QString inAnimation;
    QString outAnimation;
    qint64 inAnimationDurationUs = 0;
    qint64 outAnimationDurationUs = 0;
};

struct CapCutCaptionStyle
{
    QString font; // 空表示剪映默认字体
    int fontSize = 12;
    QString textColor = QStringLiteral("#FFFFFF");
    QString borderColor;
    double borderAlpha = 1.0;
    double borderWidth = 30.0;
    int alignment = 1;
    double transformX = 0.0;
    double transformY = 0.0;
    bool bold = false;
    bool hasShadow = false;
    bool hasBackground = false;
    QString backgroundColor = QStringLiteral("#000000");
    double backgroundAlpha = 0.5;
};

struct CapCutAudio
{
    QString url;
    qint64 startUs = 0;
    qint64 endUs = 0;
    qint64 durationUs = -1;
    double volume = 0.1;
};

struct CapCutKeyframe
{
    QString segmentId;
    QString property; // KFTypeAlpha / KFTypeScaleX ...
    qint64 offsetUs = 0; // 片段内绝对偏移
    double value = 1.0;
};

struct CapCutVideoStatus
{
    QString status; // pending / processing / completed / failed
    int progress = 0;
    QString videoUrl;
    QString errorMessage;
};

class CapCutClient
{
public:
    using DownloadProgress = std::function<void(int, const QString &)>;

    explicit CapCutClient(HttpClient *http);

    // 上传本地文件到对象存储，返回可被 add_* 使用的临时 URL。
    bool uploadFile(const QString &filePath, const QString &apiKey, QString *url,
                    QString *error);

    bool createDraft(int width, int height, CapCutDraft *draft, QString *error);

    // 创建一条新的视频轨；同一批片段共用请求级缩放/位置。
    bool addVideos(const CapCutDraft &draft, const QVector<CapCutVideo> &videos,
                   double scaleX, double scaleY, double alpha, int transformX,
                   int transformY, QStringList *segmentIds, QString *error);

    // 创建一条新的图片轨；每张图片可单独设置入/出场动画。
    bool addImages(const CapCutDraft &draft, const QVector<CapCutImage> &images,
                   double scale, int transformX, int transformY,
                   QStringList *segmentIds, QString *error);

    // 创建一条新的字幕轨；样式为接口级，单条字幕可覆盖字号与动画。
    bool addCaptions(const CapCutDraft &draft,
                     const QVector<CapCutCaption> &captions,
                     const CapCutCaptionStyle &style, QString *error);

    // 创建音频轨；BGM 与音效分别调用，先音效后 BGM 可以保证混音顺序。
    bool addAudios(const CapCutDraft &draft, const QVector<CapCutAudio> &audios,
                   QString *error);

    bool addKeyframes(const CapCutDraft &draft,
                      const QVector<CapCutKeyframe> &keyframes, QString *error);

    bool genVideo(const CapCutDraft &draft, const QString &apiKey, QString *error);
    bool genVideoStatus(const CapCutDraft &draft, CapCutVideoStatus *status,
                        QString *error);

    bool fetchDraftFiles(const QString &draftId, QStringList *fileUrls,
                         QString *error);

    // 下载完整草稿并本地化素材路径；先写隐藏暂存目录，成功后原子改名，
    // 剪映无需重启即可收录。progress 回调用于结果页「生成草稿」按钮。
    bool downloadDraft(const QString &draftId, const QString &draftTitle,
                       const QString &targetRoot, QString *localPath,
                       QString *error, const DownloadProgress &progress = {});

    static QString draftIdFromUrl(const QString &draftUrl);
    static bool writeDraftReadmeFile(const QString &draftDir);

private:
    HttpClient *m_http = nullptr;
};

} // namespace Oral
