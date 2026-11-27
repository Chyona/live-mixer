// 文件作用：智能口播剪映小助手客户端实现。
// 所有接口统一走 https://capcut-mate.jcaigc.cn/openapi/capcut-mate/v1/。
// 列表字段必须序列化成 JSON 字符串（不是对象数组），时间单位统一微秒。

#include "OralCapCutClient.h"

#include "../api/HttpClient.h"
#include "../core/MediaUtils.h"
#include "../core/ServiceEndpoints.h"

#include <QDebug>
#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QHash>
#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <QSaveFile>
#include <QSet>
#include <QUrl>
#include <QUrlQuery>

namespace {

// 剪映草稿内素材占位符；下载草稿时必须改写成剪映可识别的相对路径。
constexpr char kDraftPathPlaceholder[] =
    "##_draftpath_placeholder_0E685133-18CE-45ED-8CB8-2904A212EC80_##";

bool checkEnvelope(const HttpResponse &response, QJsonObject *json, QString *error)
{
    if (!response.networkOk) {
        if (error) {
            *error = response.error.isEmpty() ? QStringLiteral("网络请求失败。")
                                              : response.error;
        }
        return false;
    }
    if (response.httpStatus != 200) {
        if (error) {
            *error = QStringLiteral("剪映小助手返回 HTTP %1。")
                         .arg(response.httpStatus);
        }
        return false;
    }
    if (response.json.isEmpty()) {
        if (error) {
            *error = QStringLiteral("剪映小助手返回了无法解析的响应。");
        }
        return false;
    }
    // 剪映小助手的业务错误同样以 HTTP 200 返回，真实结果看 code。
    const int code = response.json.value(QStringLiteral("code")).toInt(-1);
    if (code != 0) {
        if (error) {
            const QString message =
                response.json.value(QStringLiteral("message")).toString();
            *error = message.isEmpty()
                ? QStringLiteral("剪映小助手返回错误码 %1。").arg(code)
                : message;
        }
        return false;
    }
    if (json) {
        *json = response.json;
    }
    return true;
}

QByteArray compactJson(const QJsonArray &array)
{
    return QJsonDocument(array).toJson(QJsonDocument::Compact);
}

bool isHttpUrl(const QString &value)
{
    return value.startsWith(QLatin1String("http://"), Qt::CaseInsensitive)
        || value.startsWith(QLatin1String("https://"), Qt::CaseInsensitive);
}

QString safeMaterialFileName(const QString &url, const QString &fallbackBase,
                             const QString &fallbackSuffix)
{
    const QString fileName = QFileInfo(QUrl(url).path()).fileName();
    QString base = QFileInfo(fileName).completeBaseName();
    QString suffix = QFileInfo(fileName).suffix();
    if (base.isEmpty()) {
        base = fallbackBase;
    }
    base = MediaUtils::sanitizeFileComponent(base, 36);
    if (base.isEmpty()) {
        base = QStringLiteral("material");
    }
    const QString hash = QString::number(qHash(url), 16).right(6);
    if (suffix.isEmpty()) {
        suffix = fallbackSuffix;
    }
    return QStringLiteral("%1_%2.%3").arg(base, hash, suffix.toLower());
}

struct MaterialList
{
    const char *key;
    const char *subDir;
    const char *suffix;
};

const MaterialList kMaterialLists[] = {
    {"videos", "videos", "mp4"},
    {"audios", "audios", "mp3"},
    {"images", "images", "jpg"},
};

bool writeJsonFile(const QString &path, const QJsonObject &object, QString *error)
{
    QSaveFile file(path);
    if (!file.open(QIODevice::WriteOnly)) {
        if (error) {
            *error = QStringLiteral("无法写入草稿文件：%1").arg(path);
        }
        return false;
    }
    file.write(QJsonDocument(object).toJson(QJsonDocument::Indented));
    if (!file.commit()) {
        if (error) {
            *error = QStringLiteral("保存草稿文件失败：%1").arg(path);
        }
        return false;
    }
    return true;
}

QString relativeDraftFilePath(const QUrl &fileUrl, const QString &draftId)
{
    const QString path = fileUrl.path();
    const int index = path.indexOf(draftId);
    QString relative;
    if (index >= 0) {
        relative = path.mid(index + draftId.size());
    } else {
        relative = QLatin1Char('/') + QFileInfo(path).fileName();
    }
    while (relative.startsWith(QLatin1Char('/'))) {
        relative.remove(0, 1);
    }
    if (relative.isEmpty() || relative.contains(QStringLiteral(".."))) {
        return QString();
    }
    return relative;
}

// 把草稿 JSON 里的远程素材地址改成本地相对路径。
void rewriteMaterialPaths(QJsonObject *root, const QHash<QString, QString> &localPaths)
{
    QJsonObject materials = root->value(QStringLiteral("materials")).toObject();
    if (materials.isEmpty()) {
        return;
    }
    for (const MaterialList &list : kMaterialLists) {
        QJsonArray items = materials.value(QLatin1String(list.key)).toArray();
        for (int index = 0; index < items.size(); ++index) {
            QJsonObject item = items.at(index).toObject();
            const QString sourcePath = item.value(QStringLiteral("path")).toString();
            if (!isHttpUrl(sourcePath)) {
                continue;
            }
            const QString relative = localPaths.value(sourcePath);
            if (relative.isEmpty()) {
                continue;
            }
            item.insert(QStringLiteral("path"),
                        QString::fromLatin1(kDraftPathPlaceholder) + QLatin1Char('/')
                            + relative);
            items[index] = item;
        }
        materials.insert(QLatin1String(list.key), items);
    }
    root->insert(QStringLiteral("materials"), materials);

    // 素材已下载到草稿目录，必须把保存模式改为 1。
    QJsonObject config = root->value(QStringLiteral("config")).toObject();
    config.insert(QStringLiteral("material_save_mode"), 1);
    root->insert(QStringLiteral("config"), config);
}

void localizeDraftMeta(QJsonObject *meta, const QString &draftDir,
                       const QString &folderName)
{
    meta->insert(QStringLiteral("draft_name"), folderName);
    meta->insert(QStringLiteral("draft_fold_path"),
                 QDir::fromNativeSeparators(draftDir));
    meta->insert(QStringLiteral("draft_root_path"),
                 QDir::fromNativeSeparators(QFileInfo(draftDir).absolutePath()));

    QJsonArray groups = meta->value(QStringLiteral("draft_materials")).toArray();
    for (int groupIndex = 0; groupIndex < groups.size(); ++groupIndex) {
        QJsonObject group = groups.at(groupIndex).toObject();
        QJsonArray values = group.value(QStringLiteral("value")).toArray();
        for (int index = 0; index < values.size(); ++index) {
            QJsonObject entry = values.at(index).toObject();
            QString filePath = entry.value(QStringLiteral("file_Path")).toString();
            if (filePath.isEmpty() || isHttpUrl(filePath)) {
                continue;
            }
            const QString placeholder = QString::fromLatin1(kDraftPathPlaceholder);
            if (filePath.startsWith(placeholder)) {
                filePath = filePath.mid(placeholder.size());
            } else if (filePath.startsWith(QLatin1Char('/'))
                       || (filePath.size() > 1
                           && filePath.at(1) == QLatin1Char(':'))) {
                continue;
            }
            while (filePath.startsWith(QLatin1Char('/'))) {
                filePath.remove(0, 1);
            }
            if (!filePath.isEmpty()) {
                entry.insert(QStringLiteral("file_Path"),
                             QStringLiteral("./") + filePath);
                values[index] = entry;
            }
        }
        group.insert(QStringLiteral("value"), values);
        groups[groupIndex] = group;
    }
    meta->insert(QStringLiteral("draft_materials"), groups);
}

constexpr char kDraftReadmeFileName[] = "【剪映草稿】请用剪映打开查看成片.txt";

} // namespace

namespace Oral {

CapCutClient::CapCutClient(HttpClient *http)
    : m_http(http)
{
}

QString CapCutClient::draftIdFromUrl(const QString &draftUrl)
{
    const QUrl url(draftUrl);
    return QUrlQuery(url).queryItemValue(QStringLiteral("draft_id"));
}

bool CapCutClient::uploadFile(const QString &filePath, const QString &apiKey,
                              QString *url, QString *error)
{
    if (!m_http) {
        if (error) {
            *error = QStringLiteral("剪映小助手客户端未初始化。");
        }
        return false;
    }
    const QFileInfo info(filePath);
    if (!info.exists() || info.size() <= 0) {
        if (error) {
            *error = QStringLiteral("待上传文件不存在：%1").arg(filePath);
        }
        return false;
    }
    if (info.size() > 500LL * 1024 * 1024) {
        if (error) {
            *error = QStringLiteral("「%1」超过剪映小助手 500MB 上传上限。")
                         .arg(info.fileName());
        }
        return false;
    }

    const HttpResponse response = m_http->postMultipart(
        ServiceEndpoints::capCutMateUrl(QStringLiteral("upload_file")),
        {{QStringLiteral("apiKey"), apiKey}}, filePath, QString(), QByteArray(),
        30 * 60 * 1000);
    QJsonObject json;
    if (!checkEnvelope(response, &json, error)) {
        return false;
    }
    const QString remoteUrl = json.value(QStringLiteral("url")).toString().trimmed();
    if (remoteUrl.isEmpty()) {
        if (error) {
            *error = QStringLiteral("上传成功但未返回文件地址。");
        }
        return false;
    }
    if (url) {
        *url = remoteUrl;
    }
    return true;
}

bool CapCutClient::createDraft(int width, int height, CapCutDraft *draft,
                               QString *error)
{
    if (!m_http || !draft) {
        if (error) {
            *error = QStringLiteral("剪映小助手客户端未初始化。");
        }
        return false;
    }
    QJsonObject payload;
    payload.insert(QStringLiteral("width"), width);
    payload.insert(QStringLiteral("height"), height);
    const HttpResponse response = m_http->postJson(
        ServiceEndpoints::capCutMateUrl(QStringLiteral("create_draft")), payload,
        QByteArray(), 60000);
    QJsonObject json;
    if (!checkEnvelope(response, &json, error)) {
        return false;
    }
    const QString draftUrl =
        json.value(QStringLiteral("draft_url")).toString().trimmed();
    if (draftUrl.isEmpty()) {
        if (error) {
            *error = QStringLiteral("创建草稿成功但未返回 draft_url。");
        }
        return false;
    }
    draft->url = draftUrl;
    draft->id = draftIdFromUrl(draftUrl);
    return true;
}

bool CapCutClient::addVideos(const CapCutDraft &draft,
                             const QVector<CapCutVideo> &videos, double scaleX,
                             double scaleY, double alpha, int transformX,
                             int transformY, QStringList *segmentIds,
                             QString *error)
{
    if (!m_http || draft.url.isEmpty() || videos.isEmpty()) {
        if (error) {
            *error = QStringLiteral("没有可写入的视频片段。");
        }
        return false;
    }
    QJsonArray videoArray;
    for (const CapCutVideo &video : videos) {
        QJsonObject item;
        item.insert(QStringLiteral("video_url"), video.url);
        if (video.width > 0 && video.height > 0) {
            item.insert(QStringLiteral("width"), video.width);
            item.insert(QStringLiteral("height"), video.height);
        }
        item.insert(QStringLiteral("start"), video.startUs);
        item.insert(QStringLiteral("end"), video.endUs);
        item.insert(QStringLiteral("duration"), video.endUs - video.startUs);
        item.insert(QStringLiteral("volume"), video.volume);
        if (!video.transition.isEmpty()) {
            item.insert(QStringLiteral("transition"), video.transition);
            if (video.transitionDurationUs > 0) {
                item.insert(QStringLiteral("transition_duration"),
                            video.transitionDurationUs);
            }
        }
        videoArray.append(item);
    }

    QJsonObject payload;
    payload.insert(QStringLiteral("draft_url"), draft.url);
    payload.insert(QStringLiteral("video_infos"),
                   QString::fromUtf8(compactJson(videoArray)));
    payload.insert(QStringLiteral("scale_x"), scaleX);
    payload.insert(QStringLiteral("scale_y"), scaleY);
    payload.insert(QStringLiteral("alpha"), alpha);
    payload.insert(QStringLiteral("transform_x"), transformX);
    payload.insert(QStringLiteral("transform_y"), transformY);

    const HttpResponse response = m_http->postJson(
        ServiceEndpoints::capCutMateUrl(QStringLiteral("add_videos")), payload,
        QByteArray(), 10 * 60 * 1000);
    QJsonObject json;
    if (!checkEnvelope(response, &json, error)) {
        return false;
    }
    if (segmentIds) {
        segmentIds->clear();
        for (const QJsonValue &value :
             json.value(QStringLiteral("segment_ids")).toArray()) {
            segmentIds->append(value.toString());
        }
    }
    return true;
}

bool CapCutClient::addImages(const CapCutDraft &draft,
                             const QVector<CapCutImage> &images, double scale,
                             int transformX, int transformY,
                             QStringList *segmentIds, QString *error)
{
    if (!m_http || draft.url.isEmpty() || images.isEmpty()) {
        if (error) {
            *error = QStringLiteral("没有可写入的图片片段。");
        }
        return false;
    }
    QJsonArray imageArray;
    for (const CapCutImage &image : images) {
        QJsonObject item;
        item.insert(QStringLiteral("image_url"), image.url);
        if (image.width > 0 && image.height > 0) {
            item.insert(QStringLiteral("width"), image.width);
            item.insert(QStringLiteral("height"), image.height);
        }
        item.insert(QStringLiteral("start"), image.startUs);
        item.insert(QStringLiteral("end"), image.endUs);
        if (!image.inAnimation.isEmpty()) {
            item.insert(QStringLiteral("in_animation"), image.inAnimation);
            if (image.inAnimationDurationUs > 0) {
                item.insert(QStringLiteral("in_animation_duration"),
                            image.inAnimationDurationUs);
            }
        }
        if (!image.outAnimation.isEmpty()) {
            item.insert(QStringLiteral("out_animation"), image.outAnimation);
            if (image.outAnimationDurationUs > 0) {
                item.insert(QStringLiteral("out_animation_duration"),
                            image.outAnimationDurationUs);
            }
        }
        if (!image.transition.isEmpty()) {
            item.insert(QStringLiteral("transition"), image.transition);
            if (image.transitionDurationUs > 0) {
                item.insert(QStringLiteral("transition_duration"),
                            image.transitionDurationUs);
            }
        }
        imageArray.append(item);
    }

    QJsonObject payload;
    payload.insert(QStringLiteral("draft_url"), draft.url);
    payload.insert(QStringLiteral("image_infos"),
                   QString::fromUtf8(compactJson(imageArray)));
    payload.insert(QStringLiteral("scale_x"), scale);
    payload.insert(QStringLiteral("scale_y"), scale);
    payload.insert(QStringLiteral("transform_x"), transformX);
    payload.insert(QStringLiteral("transform_y"), transformY);
    payload.insert(QStringLiteral("alpha"), 1.0);

    const HttpResponse response = m_http->postJson(
        ServiceEndpoints::capCutMateUrl(QStringLiteral("add_images")), payload,
        QByteArray(), 10 * 60 * 1000);
    QJsonObject json;
    if (!checkEnvelope(response, &json, error)) {
        return false;
    }
    if (segmentIds) {
        segmentIds->clear();
        for (const QJsonValue &value :
             json.value(QStringLiteral("segment_ids")).toArray()) {
            segmentIds->append(value.toString());
        }
    }
    return true;
}

bool CapCutClient::addCaptions(const CapCutDraft &draft,
                               const QVector<CapCutCaption> &captions,
                               const CapCutCaptionStyle &style, QString *error)
{
    if (!m_http || draft.url.isEmpty() || captions.isEmpty()) {
        if (error) {
            *error = QStringLiteral("没有可写入的字幕。");
        }
        return false;
    }
    QJsonArray captionArray;
    for (const CapCutCaption &caption : captions) {
        QJsonObject item;
        item.insert(QStringLiteral("start"), caption.startUs);
        item.insert(QStringLiteral("end"), caption.endUs);
        item.insert(QStringLiteral("text"), caption.text);
        if (!caption.keyword.isEmpty()) {
            item.insert(QStringLiteral("keyword"), caption.keyword);
            if (!caption.keywordColor.isEmpty()) {
                item.insert(QStringLiteral("keyword_color"),
                            caption.keywordColor);
            }
            if (caption.keywordFontSize > 0) {
                item.insert(QStringLiteral("keyword_font_size"),
                            caption.keywordFontSize);
            }
        }
        if (caption.fontSize > 0) {
            item.insert(QStringLiteral("font_size"), caption.fontSize);
        }
        if (!caption.inAnimation.isEmpty()) {
            item.insert(QStringLiteral("in_animation"), caption.inAnimation);
            if (caption.inAnimationDurationUs > 0) {
                item.insert(QStringLiteral("in_animation_duration"),
                            caption.inAnimationDurationUs);
            }
        }
        if (!caption.outAnimation.isEmpty()) {
            item.insert(QStringLiteral("out_animation"), caption.outAnimation);
            if (caption.outAnimationDurationUs > 0) {
                item.insert(QStringLiteral("out_animation_duration"),
                            caption.outAnimationDurationUs);
            }
        }
        captionArray.append(item);
    }

    QJsonObject payload;
    payload.insert(QStringLiteral("draft_url"), draft.url);
    payload.insert(QStringLiteral("captions"),
                   QString::fromUtf8(compactJson(captionArray)));
    payload.insert(QStringLiteral("text_color"), style.textColor);
    if (!style.borderColor.isEmpty()) {
        payload.insert(QStringLiteral("border_color"), style.borderColor);
        payload.insert(QStringLiteral("border_alpha"), style.borderAlpha);
        payload.insert(QStringLiteral("border_width"), style.borderWidth);
    }
    payload.insert(QStringLiteral("alignment"), style.alignment);
    payload.insert(QStringLiteral("font_size"), style.fontSize);
    payload.insert(QStringLiteral("transform_x"), style.transformX);
    payload.insert(QStringLiteral("transform_y"), style.transformY);
    payload.insert(QStringLiteral("bold"), style.bold);
    payload.insert(QStringLiteral("has_shadow"), style.hasShadow);
    if (!style.font.isEmpty()) {
        payload.insert(QStringLiteral("font"), style.font);
    }
    if (style.hasBackground) {
        payload.insert(QStringLiteral("has_background"), true);
        QJsonObject background;
        background.insert(QStringLiteral("background_color"),
                          style.backgroundColor);
        background.insert(QStringLiteral("background_style"), 1);
        background.insert(QStringLiteral("background_alpha"),
                          style.backgroundAlpha);
        background.insert(QStringLiteral("background_round_radius"), 0.12);
        background.insert(QStringLiteral("background_height"), 0.18);
        background.insert(QStringLiteral("background_width"), 0.18);
        payload.insert(QStringLiteral("background_info"), background);
    }

    const HttpResponse response = m_http->postJson(
        ServiceEndpoints::capCutMateUrl(QStringLiteral("add_captions")), payload,
        QByteArray(), 10 * 60 * 1000);
    return checkEnvelope(response, nullptr, error);
}

bool CapCutClient::addAudios(const CapCutDraft &draft,
                             const QVector<CapCutAudio> &audios, QString *error)
{
    if (!m_http || draft.url.isEmpty() || audios.isEmpty()) {
        if (error) {
            *error = QStringLiteral("没有可写入的音频。");
        }
        return false;
    }
    QJsonArray audioArray;
    for (const CapCutAudio &audio : audios) {
        QJsonObject item;
        item.insert(QStringLiteral("audio_url"), audio.url);
        item.insert(QStringLiteral("start"), audio.startUs);
        item.insert(QStringLiteral("end"), audio.endUs);
        if (audio.durationUs > 0) {
            item.insert(QStringLiteral("duration"), audio.durationUs);
        }
        item.insert(QStringLiteral("volume"), audio.volume);
        audioArray.append(item);
    }
    QJsonObject payload;
    payload.insert(QStringLiteral("draft_url"), draft.url);
    payload.insert(QStringLiteral("audio_infos"),
                   QString::fromUtf8(compactJson(audioArray)));
    const HttpResponse response = m_http->postJson(
        ServiceEndpoints::capCutMateUrl(QStringLiteral("add_audios")), payload,
        QByteArray(), 10 * 60 * 1000);
    return checkEnvelope(response, nullptr, error);
}

bool CapCutClient::addKeyframes(const CapCutDraft &draft,
                                const QVector<CapCutKeyframe> &keyframes,
                                QString *error)
{
    if (!m_http || draft.url.isEmpty() || keyframes.isEmpty()) {
        return true;
    }
    QJsonArray keyframeArray;
    for (const CapCutKeyframe &keyframe : keyframes) {
        QJsonObject item;
        item.insert(QStringLiteral("segment_id"), keyframe.segmentId);
        item.insert(QStringLiteral("property"), keyframe.property);
        item.insert(QStringLiteral("offset"), keyframe.offsetUs);
        item.insert(QStringLiteral("value"), keyframe.value);
        keyframeArray.append(item);
    }
    QJsonObject payload;
    payload.insert(QStringLiteral("draft_url"), draft.url);
    payload.insert(QStringLiteral("keyframes"),
                   QString::fromUtf8(compactJson(keyframeArray)));
    const HttpResponse response = m_http->postJson(
        ServiceEndpoints::capCutMateUrl(QStringLiteral("add_keyframes")), payload,
        QByteArray(), 10 * 60 * 1000);
    return checkEnvelope(response, nullptr, error);
}

bool CapCutClient::genVideo(const CapCutDraft &draft, const QString &apiKey,
                            QString *error)
{
    if (!m_http || draft.url.isEmpty()) {
        if (error) {
            *error = QStringLiteral("草稿地址无效。");
        }
        return false;
    }
    QJsonObject payload;
    payload.insert(QStringLiteral("draft_url"), draft.url);
    payload.insert(QStringLiteral("apiKey"), apiKey);
    const HttpResponse response = m_http->postJson(
        ServiceEndpoints::capCutMateUrl(QStringLiteral("gen_video")), payload,
        QByteArray(), 120000);
    return checkEnvelope(response, nullptr, error);
}

bool CapCutClient::genVideoStatus(const CapCutDraft &draft,
                                  CapCutVideoStatus *status, QString *error)
{
    if (!m_http || draft.url.isEmpty() || !status) {
        if (error) {
            *error = QStringLiteral("草稿地址无效。");
        }
        return false;
    }
    QJsonObject payload;
    payload.insert(QStringLiteral("draft_url"), draft.url);
    const HttpResponse response = m_http->postJson(
        ServiceEndpoints::capCutMateUrl(QStringLiteral("gen_video_status")),
        payload, QByteArray(), 60000);
    QJsonObject json;
    if (!checkEnvelope(response, &json, error)) {
        return false;
    }
    status->status = json.value(QStringLiteral("status")).toString();
    status->progress = json.value(QStringLiteral("progress")).toInt();
    status->videoUrl = json.value(QStringLiteral("video_url")).toString();
    status->errorMessage =
        json.value(QStringLiteral("error_message")).toString();
    return true;
}

bool CapCutClient::fetchDraftFiles(const QString &draftId,
                                   QStringList *fileUrls, QString *error)
{
    if (!m_http) {
        if (error) {
            *error = QStringLiteral("剪映小助手客户端未初始化。");
        }
        return false;
    }
    QUrl url = ServiceEndpoints::capCutMateUrl(QStringLiteral("get_draft"));
    QUrlQuery query;
    query.addQueryItem(QStringLiteral("draft_id"), draftId);
    url.setQuery(query);
    const HttpResponse response = m_http->get(url, 60000);
    QJsonObject json;
    if (!checkEnvelope(response, &json, error)) {
        return false;
    }
    QStringList files;
    for (const QJsonValue &value :
         json.value(QStringLiteral("files")).toArray()) {
        const QString fileUrl = value.toString();
        if (!fileUrl.isEmpty()) {
            files.append(fileUrl);
        }
    }
    if (files.isEmpty()) {
        if (error) {
            *error = QStringLiteral("草稿文件列表为空。");
        }
        return false;
    }
    if (fileUrls) {
        *fileUrls = files;
    }
    return true;
}

bool CapCutClient::downloadDraft(const QString &draftId,
                                 const QString &draftTitle,
                                 const QString &targetRoot, QString *localPath,
                                 QString *error, const DownloadProgress &progress)
{
    if (!m_http) {
        if (error) {
            *error = QStringLiteral("剪映小助手客户端未初始化。");
        }
        return false;
    }

    QStringList fileUrls;
    if (!fetchDraftFiles(draftId, &fileUrls, error)) {
        return false;
    }
    if (progress) {
        progress(2, QStringLiteral("已获取草稿文件列表"));
    }

    QString title = MediaUtils::sanitizeFileComponent(draftTitle.trimmed(), 40);
    if (title.isEmpty()) {
        title = QStringLiteral("口播成片");
    }
    const QString folderName = QStringLiteral("%1_%2").arg(title, draftId);
    const QString finalDir = QDir(targetRoot).filePath(folderName);
    const QString stagingRoot =
        QDir(targetRoot).filePath(QStringLiteral(".livemix-oral-staging"));
    const QString stagingDir = QDir(stagingRoot).filePath(folderName);
    if (!QDir().mkpath(stagingDir)) {
        if (error) {
            *error = QStringLiteral("无法创建草稿暂存目录：%1").arg(stagingDir);
        }
        return false;
    }

    struct ListedFile
    {
        QString url;
        QString relative;
        bool isJson = false;
    };
    QVector<ListedFile> listed;
    QSet<QString> seen;
    for (const QString &fileUrl : fileUrls) {
        const QUrl url(fileUrl);
        const QString relative = relativeDraftFilePath(url, draftId);
        if (relative.isEmpty() || seen.contains(relative)) {
            continue;
        }
        seen.insert(relative);
        listed.append({fileUrl, relative,
                       relative.endsWith(QLatin1String(".json"),
                                         Qt::CaseInsensitive)});
    }
    if (listed.isEmpty()) {
        if (error) {
            *error = QStringLiteral("无法识别草稿文件路径。");
        }
        return false;
    }

    // 先把 JSON 配置下载到内存，非 JSON 配置写入暂存目录。
    struct JsonFile
    {
        QString relative;
        QByteArray data;
    };
    QVector<JsonFile> jsonFiles;
    QVector<ListedFile> plainConfigFiles;
    int step = 0;
    const int totalSteps = listed.size();
    for (const ListedFile &file : listed) {
        ++step;
        if (progress) {
            progress(qBound(2, 5 + step * 35 / qMax(1, totalSteps), 40),
                     QStringLiteral("读取草稿配置：%1").arg(file.relative));
        }
        if (file.isJson) {
            const HttpResponse response = m_http->get(QUrl(file.url), 60000);
            if (!response.isHttpOk() || response.body.isEmpty()) {
                if (error) {
                    *error = QStringLiteral("下载草稿文件失败：%1")
                                 .arg(file.relative);
                }
                return false;
            }
            jsonFiles.append({file.relative, response.body});
        } else {
            plainConfigFiles.append(file);
        }
    }
    for (const ListedFile &file : plainConfigFiles) {
        const QString localFile = QDir(stagingDir).filePath(file.relative);
        const QFileInfo existing(localFile);
        if (existing.exists() && existing.size() > 0) {
            continue;
        }
        QString downloadError;
        if (!m_http->downloadToFile(QUrl(file.url), localFile, &downloadError,
                                    600000)) {
            if (error) {
                *error = QStringLiteral("下载草稿文件失败：%1（%2）")
                             .arg(file.relative, downloadError);
            }
            return false;
        }
    }

    struct ParsedJson
    {
        QString relative;
        QJsonObject object;
    };
    QVector<ParsedJson> parsed;
    for (const JsonFile &jsonFile : jsonFiles) {
        const QJsonObject object = HttpClient::parseJsonObject(jsonFile.data);
        if (object.isEmpty()) {
            if (error) {
                *error = QStringLiteral("草稿 JSON 无法解析：%1")
                             .arg(jsonFile.relative);
            }
            return false;
        }
        parsed.append({jsonFile.relative, object});
    }

    struct AssetFile
    {
        QString url;
        QString relative;
    };
    QVector<AssetFile> assets;
    QHash<QString, QString> localPaths;
    QSet<QString> seenAssets;
    const auto addAsset = [&](const QString &url, const QString &relative) {
        if (relative.isEmpty()) {
            return;
        }
        localPaths.insert(url, relative);
        if (seenAssets.contains(relative)) {
            return;
        }
        seenAssets.insert(relative);
        assets.append({url, relative});
    };
    for (const ParsedJson &json : parsed) {
        const QJsonObject materials =
            json.object.value(QStringLiteral("materials")).toObject();
        for (const MaterialList &list : kMaterialLists) {
            const QJsonArray items = materials.value(QLatin1String(list.key)).toArray();
            for (const QJsonValue &value : items) {
                const QJsonObject item = value.toObject();
                const QString sourcePath =
                    item.value(QStringLiteral("path")).toString();
                if (!isHttpUrl(sourcePath) || localPaths.contains(sourcePath)) {
                    continue;
                }
                const QString name = safeMaterialFileName(
                    sourcePath,
                    item.value(QStringLiteral("material_name")).toString(),
                    QLatin1String(list.suffix));
                addAsset(sourcePath,
                         QStringLiteral("assets/%1/%2")
                             .arg(QLatin1String(list.subDir), name));
            }
        }
    }

    for (int index = 0; index < assets.size(); ++index) {
        const AssetFile &asset = assets.at(index);
        if (progress) {
            progress(qBound(40, 40 + (index + 1) * 45 / qMax(1, int(assets.size())),
                            85),
                     QStringLiteral("下载草稿素材：%1").arg(asset.relative));
        }
        const QString localFile = QDir(stagingDir).filePath(asset.relative);
        const QFileInfo existing(localFile);
        if (existing.exists() && existing.size() > 0) {
            continue;
        }
        QString downloadError;
        if (!m_http->downloadToFile(QUrl(asset.url), localFile, &downloadError,
                                    600000)) {
            if (error) {
                *error = QStringLiteral("下载草稿素材失败：%1（%2）")
                             .arg(asset.relative, downloadError);
            }
            return false;
        }
    }

    // 改写路径、写入元信息，再把暂存目录原子改名为正式目录。
    for (const ParsedJson &json : parsed) {
        QJsonObject object = json.object;
        if (object.contains(QStringLiteral("materials"))) {
            rewriteMaterialPaths(&object, localPaths);
        } else if (QFileInfo(json.relative).fileName().compare(
                       QLatin1String("draft_meta_info.json"),
                       Qt::CaseInsensitive)
                   == 0) {
            localizeDraftMeta(&object, finalDir, folderName);
        }
        const QString jsonPath = QDir(stagingDir).filePath(json.relative);
        if (!QDir().mkpath(QFileInfo(jsonPath).absolutePath())) {
            if (error) {
                *error = QStringLiteral("无法创建目录：%1")
                             .arg(QFileInfo(jsonPath).absolutePath());
            }
            return false;
        }
        if (!writeJsonFile(jsonPath, object, error)) {
            return false;
        }
    }
    writeDraftReadmeFile(stagingDir);

    if (QFileInfo::exists(finalDir)) {
        QDir(finalDir).removeRecursively();
    }
    if (!QDir().rename(stagingDir, finalDir)) {
        if (error) {
            *error = QStringLiteral("无法发布草稿目录：%1").arg(finalDir);
        }
        return false;
    }
    QDir().rmdir(stagingRoot);
    if (localPath) {
        *localPath = finalDir;
    }
    if (progress) {
        progress(100, QStringLiteral("草稿已写入剪映目录"));
    }
    return true;
}

bool CapCutClient::writeDraftReadmeFile(const QString &draftDir)
{
    const QByteArray bom("\xEF\xBB\xBF", 3);
    const QByteArray content =
        bom
        + QStringLiteral("这是一个剪映草稿，不是可直接播放的视频文件。\n\n"
                         "请打开剪映，在「本地草稿」中找到并打开这个草稿，"
                         "即可查看和导出成片。\n")
              .toUtf8();
    QFile file(QDir(draftDir).filePath(QString::fromUtf8(kDraftReadmeFileName)));
    if (!file.open(QIODevice::WriteOnly | QIODevice::Truncate)) {
        return false;
    }
    file.write(content);
    file.close();
    return true;
}

} // namespace Oral
