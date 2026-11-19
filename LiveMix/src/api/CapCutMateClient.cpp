// 文件作用：剪映小助手接口调用，以及草稿下载后的本地路径改写。
#include "CapCutMateClient.h"

#include "../core/MediaUtils.h"
#include "../core/ServiceEndpoints.h"
#include "HttpClient.h"

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

// 剪映草稿内素材路径使用的占位符前缀（与剪映客户端约定一致）。
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
            *error = QStringLiteral("服务返回 HTTP %1。").arg(response.httpStatus);
        }
        return false;
    }
    if (response.json.isEmpty()) {
        if (error) {
            *error = QStringLiteral("服务返回了无法解析的响应。");
        }
        return false;
    }

    // 剪映小助手的错误同样以 HTTP 200 返回，真实结果在 code 字段。
    const int code = response.json.value(QStringLiteral("code")).toInt(-1);
    if (code != 0) {
        if (error) {
            const QString message =
                response.json.value(QStringLiteral("message")).toString();
            *error = message.isEmpty() ? QStringLiteral("服务返回错误码 %1。").arg(code)
                                       : message;
        }
        return false;
    }
    if (json) {
        *json = response.json;
    }
    return true;
}

QString fileMimeType(const QString &filePath)
{
    const QString suffix = QFileInfo(filePath).suffix().toLower();
    if (suffix == QLatin1String("mp3")) {
        return QStringLiteral("audio/mpeg");
    }
    if (suffix == QLatin1String("wav")) {
        return QStringLiteral("audio/wav");
    }
    return QStringLiteral("video/mp4");
}

bool isHttpUrl(const QString &value)
{
    return value.startsWith(QLatin1String("http://"), Qt::CaseInsensitive)
        || value.startsWith(QLatin1String("https://"), Qt::CaseInsensitive);
}

QString safeMaterialFileName(const QString &url, const QString &fallbackBase,
                             const QString &fallbackSuffix)
{
    QString fileName = QFileInfo(QUrl(url).path()).fileName();
    QString base = QFileInfo(fileName).completeBaseName();
    QString suffix = QFileInfo(fileName).suffix();
    if (base.isEmpty()) {
        base = fallbackBase;
    }
    base = MediaUtils::sanitizeFileComponent(base, 36);
    if (base.isEmpty()) {
        base = QStringLiteral("material");
    }
    // 加一段短哈希，避免不同素材重名互相覆盖。
    const QString hash = QString::number(qHash(url), 16).right(6);
    if (suffix.isEmpty()) {
        suffix = fallbackSuffix;
    }
    return QStringLiteral("%1_%2.%3").arg(base, hash, suffix.toLower());
}

// 素材列表在草稿 JSON 中的分组：videos / audios / images。
struct MaterialList
{
    const char *key;
    const char *subDir;
    const char *suffix;
    DraftFileKind kind;
};

const MaterialList kMaterialLists[] = {
    {"videos", "videos", "mp4", DraftFileKind::Video},
    {"audios", "audios", "mp3", DraftFileKind::Audio},
    {"images", "images", "jpg", DraftFileKind::Image},
};

DraftFileKind kindForPath(const QString &path)
{
    const QString suffix = QFileInfo(path).suffix().toLower();
    if (suffix == QLatin1String("mp4") || suffix == QLatin1String("mov")
        || suffix == QLatin1String("mkv") || suffix == QLatin1String("webm")
        || suffix == QLatin1String("m4v")) {
        return DraftFileKind::Video;
    }
    if (suffix == QLatin1String("mp3") || suffix == QLatin1String("wav")
        || suffix == QLatin1String("m4a") || suffix == QLatin1String("aac")
        || suffix == QLatin1String("flac")) {
        return DraftFileKind::Audio;
    }
    if (suffix == QLatin1String("png") || suffix == QLatin1String("jpg")
        || suffix == QLatin1String("jpeg") || suffix == QLatin1String("webp")) {
        return DraftFileKind::Image;
    }
    return DraftFileKind::Config;
}

// 将草稿 JSON 中已下载素材的远程 URL 改写为剪映占位符相对路径。
bool rewriteMaterialPaths(QJsonObject *root, const QHash<QString, QString> &localPaths)
{
    QJsonObject materials = root->value(QStringLiteral("materials")).toObject();
    if (materials.isEmpty()) {
        return true;
    }

    for (const MaterialList &list : kMaterialLists) {
        QJsonArray items = materials.value(QLatin1String(list.key)).toArray();
        for (int i = 0; i < items.size(); ++i) {
            QJsonObject item = items.at(i).toObject();
            const QString sourcePath = item.value(QStringLiteral("path")).toString();
            if (!isHttpUrl(sourcePath)) {
                continue;
            }
            const QString relative = localPaths.value(sourcePath);
            if (relative.isEmpty()) {
                continue;
            }
            // 草稿内路径固定使用正斜杠，Windows 上也不能写成反斜杠。
            item.insert(QStringLiteral("path"),
                        QString::fromLatin1(kDraftPathPlaceholder) + QLatin1Char('/')
                            + relative);
            items[i] = item;
        }
        materials.insert(QLatin1String(list.key), items);
    }

    root->insert(QStringLiteral("materials"), materials);

    // 素材已下载进草稿目录，必须把保存模式改为 1，否则剪映会认为素材在草稿外。
    QJsonObject config = root->value(QStringLiteral("config")).toObject();
    config.insert(QStringLiteral("material_save_mode"), 1);
    root->insert(QStringLiteral("config"), config);
    return true;
}

void localizeDraftMeta(QJsonObject *meta, const QString &draftDir,
                       const QString &folderName)
{
    meta->insert(QStringLiteral("draft_name"), folderName);
    meta->insert(QStringLiteral("draft_fold_path"),
                 QDir::fromNativeSeparators(draftDir));
    meta->insert(QStringLiteral("draft_root_path"),
                 QDir::fromNativeSeparators(QFileInfo(draftDir).absolutePath()));

    // draft_materials 里的 file_Path 只保留草稿内的相对形式。
    QJsonArray groups = meta->value(QStringLiteral("draft_materials")).toArray();
    for (int i = 0; i < groups.size(); ++i) {
        QJsonObject group = groups.at(i).toObject();
        QJsonArray values = group.value(QStringLiteral("value")).toArray();
        for (int j = 0; j < values.size(); ++j) {
            QJsonObject entry = values.at(j).toObject();
            QString filePath = entry.value(QStringLiteral("file_Path")).toString();
            if (filePath.isEmpty() || isHttpUrl(filePath)) {
                continue;
            }
            const QString placeholder = QString::fromLatin1(kDraftPathPlaceholder);
            const bool fromPlaceholder = filePath.startsWith(placeholder);
            if (fromPlaceholder) {
                filePath = filePath.mid(placeholder.size());
            } else if (filePath.startsWith(QLatin1Char('/'))
                       || (filePath.size() > 1 && filePath.at(1) == QLatin1Char(':'))) {
                // 无法可靠映射的绝对路径保持原样，避免写出错误的相对路径。
                continue;
            }
            while (filePath.startsWith(QLatin1Char('/'))) {
                filePath.remove(0, 1);
            }
            if (!filePath.isEmpty()) {
                entry.insert(QStringLiteral("file_Path"),
                             QStringLiteral("./") + filePath);
                values[j] = entry;
            }
        }
        group.insert(QStringLiteral("value"), values);
        groups[i] = group;
    }
    meta->insert(QStringLiteral("draft_materials"), groups);
}

// 把草稿文件 URL 映射为下载目录下的相对路径：/output/draft/<draftId>/xxx.json
QString relativeDraftFilePath(const QUrl &fileUrl, const QString &draftId)
{
    const QString path = fileUrl.path();
    int index = path.indexOf(draftId);
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

bool writeJsonFile(const QString &path, const QJsonObject &object, QString *error)
{
    QSaveFile file(path);
    if (!file.open(QIODevice::WriteOnly)) {
        if (error) {
            *error = QStringLiteral("无法写入草稿文件：%1").arg(file.errorString());
        }
        return false;
    }
    const QByteArray data =
        QJsonDocument(object).toJson(QJsonDocument::Indented);
    if (file.write(data) != data.size() || !file.commit()) {
        if (error) {
            *error = QStringLiteral("保存草稿文件失败：%1").arg(file.errorString());
        }
        return false;
    }
    return true;
}

} // namespace

CapCutMateClient::CapCutMateClient(HttpClient *http)
    : m_http(http)
{
}

QString CapCutMateClient::draftIdFromUrl(const QString &draftUrl)
{
    const QUrl url(draftUrl);
    return QUrlQuery(url).queryItemValue(QStringLiteral("draft_id"));
}

bool CapCutMateClient::uploadFile(const QString &filePath, const QString &apiKey,
                                  QString *url, QString *error)
{
    if (!m_http) {
        if (error) {
            *error = QStringLiteral("剪映小助手客户端未初始化。");
        }
        return false;
    }
    if (!QFileInfo::exists(filePath)) {
        if (error) {
            *error = QStringLiteral("待上传文件不存在：%1").arg(filePath);
        }
        return false;
    }

    const HttpResponse response = m_http->postMultipart(
        ServiceEndpoints::capCutMateUrl(QStringLiteral("upload_file")),
        {{QStringLiteral("apiKey"), apiKey}}, filePath, fileMimeType(filePath),
        QByteArray(), 15 * 60 * 1000);

    QJsonObject json;
    if (!checkEnvelope(response, &json, error)) {
        return false;
    }
    const QString remoteUrl = json.value(QStringLiteral("url")).toString();
    if (remoteUrl.isEmpty()) {
        if (error) {
            *error = QStringLiteral("上传成功但未返回文件 URL。");
        }
        return false;
    }
    if (url) {
        *url = remoteUrl;
    }
    return true;
}

bool CapCutMateClient::createDraft(int width, int height, CapCutDraftInfo *draft,
                                   QString *error)
{
    QJsonObject payload;
    payload.insert(QStringLiteral("width"), qMax(1, width));
    payload.insert(QStringLiteral("height"), qMax(1, height));

    const HttpResponse response = m_http->postJson(
        ServiceEndpoints::capCutMateUrl(QStringLiteral("create_draft")), payload,
        QByteArray(), 60000);
    QJsonObject json;
    if (!checkEnvelope(response, &json, error)) {
        return false;
    }

    const QString draftUrl = json.value(QStringLiteral("draft_url")).toString();
    const QString draftId = draftIdFromUrl(draftUrl);
    if (draftUrl.isEmpty() || draftId.isEmpty()) {
        if (error) {
            *error = QStringLiteral("创建草稿成功但未返回有效 draft_url。");
        }
        return false;
    }
    if (draft) {
        draft->draftUrl = draftUrl;
        draft->draftId = draftId;
    }
    return true;
}

bool CapCutMateClient::addVideos(const QString &draftUrl,
                                 const QVector<CapCutVideoInfo> &videos,
                                 QString *error)
{
    if (videos.isEmpty()) {
        if (error) {
            *error = QStringLiteral("没有可添加到草稿的视频片段。");
        }
        return false;
    }

    QJsonArray videoArray;
    for (const CapCutVideoInfo &video : videos) {
        QJsonObject videoInfo;
        videoInfo.insert(QStringLiteral("video_url"), video.url);
        // 切片文件本身已经裁剪好，start/end 表示在草稿时间轴上的累计位置。
        videoInfo.insert(QStringLiteral("start"), video.startUs);
        videoInfo.insert(QStringLiteral("end"), video.endUs);
        videoArray.append(videoInfo);
    }

    QJsonObject payload;
    payload.insert(QStringLiteral("draft_url"), draftUrl);
    payload.insert(QStringLiteral("video_infos"),
                   QString::fromUtf8(
                       QJsonDocument(videoArray).toJson(QJsonDocument::Compact)));

    const HttpResponse response = m_http->postJson(
        ServiceEndpoints::capCutMateUrl(QStringLiteral("add_videos")), payload,
        QByteArray(), 120000);
    return checkEnvelope(response, nullptr, error);
}

bool CapCutMateClient::addCaptions(const QString &draftUrl,
                                   const QVector<CapCutCaption> &captions,
                                   const CapCutCaptionStyle &style, QString *error)
{
    if (captions.isEmpty()) {
        return true;
    }

    QJsonArray captionArray;
    for (const CapCutCaption &caption : captions) {
        if (caption.endUs <= caption.startUs || caption.text.trimmed().isEmpty()) {
            continue;
        }
        QJsonObject object;
        object.insert(QStringLiteral("start"), caption.startUs);
        object.insert(QStringLiteral("end"), caption.endUs);
        object.insert(QStringLiteral("text"), caption.text);
        // 入场动画按接口要求逐条指定，空字符串表示不加动画。
        if (!style.inAnimation.isEmpty()) {
            object.insert(QStringLiteral("in_animation"), style.inAnimation);
        }
        captionArray.append(object);
    }
    if (captionArray.isEmpty()) {
        return true;
    }

    QJsonObject payload;
    payload.insert(QStringLiteral("draft_url"), draftUrl);
    payload.insert(QStringLiteral("captions"),
                   QString::fromUtf8(
                       QJsonDocument(captionArray).toJson(QJsonDocument::Compact)));
    // 字体、颜色、字号、动画作用于整批字幕；位置固定在画布坐标 (0, -800)。
    payload.insert(QStringLiteral("text_color"), style.textColor);
    if (!style.borderColor.isEmpty()) {
        payload.insert(QStringLiteral("border_color"), style.borderColor);
    }
    if (!style.font.isEmpty()) {
        payload.insert(QStringLiteral("font"), style.font);
    }
    payload.insert(QStringLiteral("font_size"), style.fontSize);
    payload.insert(QStringLiteral("transform_x"), style.transformX);
    payload.insert(QStringLiteral("transform_y"), style.transformY);

    const HttpResponse response = m_http->postJson(
        ServiceEndpoints::capCutMateUrl(QStringLiteral("add_captions")), payload,
        QByteArray(), 120000);
    const bool ok = checkEnvelope(response, nullptr, error);
    if (!ok) {
        // 服务端只会返回通用文案，真实异常在其日志里；本地至少记录错误码与文案，
        // 便于区分字幕时间重叠、草稿失效和网络错误。
        qWarning() << "add_captions failed; captions:" << captionArray.size()
                   << "httpStatus:" << response.httpStatus
                   << "code:" << response.json.value(QStringLiteral("code")).toInt(-1)
                   << "message:" << (error ? *error : QString());
    }
    return ok;
}

bool CapCutMateClient::fetchDraftFiles(const QString &draftId, QStringList *fileUrls,
                                       QString *error,
                                       const std::function<bool()> &shouldAbort)
{
    QUrl url = ServiceEndpoints::capCutMateUrl(QStringLiteral("get_draft"));
    QUrlQuery query;
    query.addQueryItem(QStringLiteral("draft_id"), draftId);
    url.setQuery(query);

    const HttpResponse response = m_http->get(url, 60000, shouldAbort);
    QJsonObject json;
    if (!checkEnvelope(response, &json, error)) {
        return false;
    }

    QStringList files;
    const QJsonArray array = json.value(QStringLiteral("files")).toArray();
    for (const QJsonValue &value : array) {
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

bool CapCutMateClient::downloadDraft(const QString &draftId, const QString &draftTitle,
                                     const QString &targetRoot, QString *localPath,
                                     QString *error,
                                     const DraftDownloadCallback &callback,
                                     const std::function<bool()> &shouldAbort)
{
    if (!m_http) {
        if (error) {
            *error = QStringLiteral("剪映小助手客户端未初始化。");
        }
        return false;
    }

    const auto cancelled = [&shouldAbort] {
        return shouldAbort && shouldAbort();
    };
    const auto notify = [&callback](DraftDownloadEvent event) {
        if (callback) {
            callback(event);
        }
    };
    const auto notifyStage = [&notify](DraftDownloadStage stage) {
        DraftDownloadEvent event;
        event.type = DraftDownloadEvent::Type::StageChanged;
        event.stage = stage;
        notify(event);
    };
    const auto notifyFile = [&notify](DraftDownloadEvent::Type type,
                                      const QString &path, DraftFileKind kind,
                                      qint64 loaded = 0, qint64 total = -1,
                                      const QString &message = QString()) {
        DraftDownloadEvent event;
        event.type = type;
        event.path = path;
        event.fileKind = kind;
        event.loadedBytes = loaded;
        event.totalBytes = total;
        event.error = message;
        notify(event);
    };
    const auto setError = [error](const QString &message) {
        if (error) {
            *error = message;
        }
    };

    QStringList fileUrls;
    if (!fetchDraftFiles(draftId, &fileUrls, error, shouldAbort)) {
        if (cancelled()) {
            setError(QStringLiteral("已取消下载"));
        }
        return false;
    }

    QString title = MediaUtils::sanitizeFileComponent(draftTitle.trimmed(), 40);
    if (title.isEmpty()) {
        title = QStringLiteral("直播切片");
    }
    const QString folderName = QStringLiteral("%1_%2").arg(title, draftId);
    const QString finalDir = QDir(targetRoot).filePath(folderName);

    // 先下载到草稿根目录下的隐藏暂存目录，全部写完后再原子改名成正式目录。
    // 剪映的目录监听只认可「完整出现」的新目录：直接往正式目录里边下载边写，
    // 它会在文件尚未就绪时跳过该目录，只能重启客户端后重新索引。
    // 暂存目录在失败时保留，重试只需补齐未完成或失败的素材。
    const QString stagingRoot =
        QDir(targetRoot).filePath(QStringLiteral(".livemix-staging"));
    const QString stagingDir = QDir(stagingRoot).filePath(folderName);
    if (!QDir().mkpath(stagingDir)) {
        setError(QStringLiteral("无法创建草稿暂存目录：%1").arg(stagingDir));
        return false;
    }

    struct ListedFile
    {
        QString url;
        QString relative;
        DraftFileKind kind = DraftFileKind::Config;
        bool isJson = false;
    };
    QVector<ListedFile> listed;
    QSet<QString> seenPaths;
    for (const QString &fileUrl : fileUrls) {
        const QUrl url(fileUrl);
        const QString relative = relativeDraftFilePath(url, draftId);
        if (relative.isEmpty()) {
            setError(QStringLiteral("无法识别草稿文件路径：%1").arg(fileUrl));
            return false;
        }
        if (seenPaths.contains(relative)) {
            continue;
        }
        seenPaths.insert(relative);
        const bool isJson = relative.endsWith(QLatin1String(".json"), Qt::CaseInsensitive);
        listed.append({fileUrl, relative,
                       isJson ? DraftFileKind::Config : kindForPath(relative), isJson});
    }

    // 第一阶段：草稿配置。先把完整列表展示出来，再逐个读取到内存或写入目录。
    notifyStage(DraftDownloadStage::Config);
    for (const ListedFile &file : listed) {
        notifyFile(DraftDownloadEvent::Type::FileQueued, file.relative, file.kind);
    }

    struct JsonFile
    {
        QString relative;
        QByteArray data;
    };
    QVector<JsonFile> jsonFiles;
    for (const ListedFile &file : listed) {
        if (cancelled()) {
            setError(QStringLiteral("已取消下载"));
            return false;
        }

        if (file.isJson) {
            const HttpResponse response = m_http->get(QUrl(file.url), 60000, shouldAbort);
            if (!response.isHttpOk() || response.body.isEmpty()) {
                const QString message = cancelled()
                    ? QStringLiteral("已取消下载")
                    : QStringLiteral("下载草稿文件失败：%1").arg(file.relative);
                setError(message);
                notifyFile(DraftDownloadEvent::Type::FileFailed, file.relative,
                           DraftFileKind::Config, 0, -1, message);
                return false;
            }

            const qint64 size = response.body.size();
            notifyFile(DraftDownloadEvent::Type::FileProgress, file.relative,
                       DraftFileKind::Config, size, size);
            notifyFile(DraftDownloadEvent::Type::FileDone, file.relative,
                       DraftFileKind::Config, size, size);
            jsonFiles.append({file.relative, response.body});
            continue;
        }

        // 草稿目录中的其它文件（例如 template.tmp）同样属于配置阶段。
        const QString localFile = QDir(stagingDir).filePath(file.relative);
        const QFileInfo existing(localFile);
        if (existing.exists() && existing.isFile() && existing.size() > 0) {
            notifyFile(DraftDownloadEvent::Type::FileProgress, file.relative, file.kind,
                       existing.size(), existing.size());
            notifyFile(DraftDownloadEvent::Type::FileDone, file.relative, file.kind,
                       existing.size(), existing.size());
            continue;
        }

        notifyFile(DraftDownloadEvent::Type::FileProgress, file.relative, file.kind, 0, -1);
        QString downloadError;
        const bool ok = m_http->downloadToFile(
            QUrl(file.url), localFile, &downloadError, 600000,
            [&](qint64 received, qint64 total) {
                notifyFile(DraftDownloadEvent::Type::FileProgress, file.relative,
                           file.kind, received, total);
            },
            shouldAbort);
        if (!ok) {
            const QString message = cancelled()
                ? QStringLiteral("已取消下载")
                : QStringLiteral("下载草稿文件失败：%1（%2）")
                      .arg(file.relative, downloadError);
            setError(message);
            notifyFile(DraftDownloadEvent::Type::FileFailed, file.relative, file.kind,
                       0, -1, message);
            return false;
        }
        const qint64 size = QFileInfo(localFile).size();
        notifyFile(DraftDownloadEvent::Type::FileDone, file.relative, file.kind, size,
                   size);
    }

    // 先解析全部配置，收集素材清单后再统一下载，界面可以在素材阶段开始前
    // 就显示完整的文件数量与预计进度。
    struct ParsedJson
    {
        QString relative;
        QJsonObject object;
    };
    QVector<ParsedJson> parsed;
    for (const JsonFile &jsonFile : jsonFiles) {
        const QJsonObject object = HttpClient::parseJsonObject(jsonFile.data);
        if (object.isEmpty()) {
            setError(QStringLiteral("草稿 JSON 无法解析：%1").arg(jsonFile.relative));
            return false;
        }
        parsed.append({jsonFile.relative, object});
    }

    struct AssetFile
    {
        QString url;
        QString relative;
        DraftFileKind kind = DraftFileKind::Config;
    };
    QVector<AssetFile> assets;
    QSet<QString> seenAssets;
    QHash<QString, QString> localPaths; // 素材 URL → 草稿内相对路径
    const auto addAsset = [&](const QString &url, const QString &relative,
                              DraftFileKind kind) {
        if (relative.isEmpty()) {
            return;
        }
        // 同一素材可能被多个 JSON 引用，路径映射始终记录。
        localPaths.insert(url, relative);
        if (seenAssets.contains(relative)) {
            return;
        }
        seenAssets.insert(relative);
        assets.append({url, relative, kind});
    };

    // 配置 JSON 中引用的远程素材。
    for (const ParsedJson &json : parsed) {
        const QJsonObject materials =
            json.object.value(QStringLiteral("materials")).toObject();
        for (const MaterialList &list : kMaterialLists) {
            const QJsonArray items = materials.value(QLatin1String(list.key)).toArray();
            for (const QJsonValue &value : items) {
                const QJsonObject item = value.toObject();
                const QString sourcePath = item.value(QStringLiteral("path")).toString();
                if (!isHttpUrl(sourcePath) || localPaths.contains(sourcePath)) {
                    continue;
                }
                const QString name = safeMaterialFileName(
                    sourcePath,
                    item.value(QStringLiteral("material_name")).toString(),
                    QLatin1String(list.suffix));
                addAsset(sourcePath,
                         QStringLiteral("assets/%1/%2")
                             .arg(QLatin1String(list.subDir), name),
                         list.kind);
            }
        }
    }

    // 先并发查询素材大小，界面可以在下载开始前展示完整列表与总进度。
    QStringList assetUrls;
    assetUrls.reserve(assets.size());
    for (const AssetFile &asset : assets) {
        assetUrls.append(asset.url);
    }
    const QHash<QString, qint64> assetSizes = m_http->probeContentLengths(assetUrls);
    if (cancelled()) {
        setError(QStringLiteral("已取消下载"));
        return false;
    }

    // 第二阶段：素材文件。
    notifyStage(DraftDownloadStage::Assets);
    for (const AssetFile &asset : assets) {
        notifyFile(DraftDownloadEvent::Type::FileQueued, asset.relative, asset.kind, 0,
                   assetSizes.value(asset.url, -1));
    }
    for (const AssetFile &asset : assets) {
        if (cancelled()) {
            setError(QStringLiteral("已取消下载"));
            return false;
        }

        const QString localFile = QDir(stagingDir).filePath(asset.relative);
        const QFileInfo existing(localFile);
        if (existing.exists() && existing.isFile() && existing.size() > 0) {
            // 上次中断时保留下来的完整文件：重试直接复用。
            notifyFile(DraftDownloadEvent::Type::FileProgress, asset.relative,
                       asset.kind, existing.size(), existing.size());
            notifyFile(DraftDownloadEvent::Type::FileDone, asset.relative, asset.kind,
                       existing.size(), existing.size());
            continue;
        }

        notifyFile(DraftDownloadEvent::Type::FileProgress, asset.relative, asset.kind, 0, -1);
        QString downloadError;
        const bool ok = m_http->downloadToFile(
            QUrl(asset.url), localFile, &downloadError, 600000,
            [&](qint64 received, qint64 total) {
                notifyFile(DraftDownloadEvent::Type::FileProgress, asset.relative,
                           asset.kind, received, total);
            },
            shouldAbort);

        if (!ok) {
            if (cancelled()) {
                setError(QStringLiteral("已取消下载"));
            } else {
                // 具体失败文件在文件列表里高亮，整体错误只保留简短原因。
                setError(downloadError);
            }
            notifyFile(DraftDownloadEvent::Type::FileFailed, asset.relative, asset.kind,
                       0, -1, downloadError);
            return false;
        }

        const qint64 size = QFileInfo(localFile).size();
        notifyFile(DraftDownloadEvent::Type::FileDone, asset.relative, asset.kind, size,
                   size);
    }

    // 第三阶段：改写路径并写入草稿目录。
    notifyStage(DraftDownloadStage::Writing);
    for (const ParsedJson &json : parsed) {
        if (cancelled()) {
            setError(QStringLiteral("已取消下载"));
            return false;
        }

        QJsonObject object = json.object;
        if (object.contains(QStringLiteral("materials"))) {
            rewriteMaterialPaths(&object, localPaths);
        } else if (QFileInfo(json.relative).fileName().compare(
                       QLatin1String("draft_meta_info.json"), Qt::CaseInsensitive)
                   == 0) {
            // meta 里的路径直接写成最终目录，避免发布后剪映索引到暂存路径。
            localizeDraftMeta(&object, finalDir, folderName);
        }

        const QString jsonPath = QDir(stagingDir).filePath(json.relative);
        const QFileInfo info(jsonPath);
        if (!QDir().mkpath(info.absolutePath())) {
            setError(QStringLiteral("无法创建目录：%1").arg(info.absolutePath()));
            return false;
        }
        if (!writeJsonFile(jsonPath, object, error)) {
            return false;
        }
    }

    // 全部文件就绪后再发布。QDir::rename 在同一文件系统内是原子操作：
    // 剪映看到的是一个从第一刻起就完整的正式目录，会直接收录。
    if (QFileInfo::exists(finalDir)) {
        // 正式目录名由业务 ID 决定；上次中断留下的半成品直接替换。
        QDir(finalDir).removeRecursively();
    }
    if (!QDir().rename(stagingDir, finalDir)) {
        setError(QStringLiteral("无法发布草稿目录：%1").arg(finalDir));
        return false;
    }
    // 暂存目录为空时一并清掉；仍有其它下载在用时保持不动。
    QDir().rmdir(stagingRoot);

    if (localPath) {
        *localPath = finalDir;
    }
    notifyStage(DraftDownloadStage::Done);
    return true;
}
