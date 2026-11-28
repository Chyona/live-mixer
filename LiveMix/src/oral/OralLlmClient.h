#pragma once

// 文件作用：智能口播专用的大模型客户端。
// 与直播切片的 LlmClient 完全独立，模型固定为 deepseek-flash：
// 1. 语音文本的语义分析（内容分段、主题、关键词、重点句、发布文案）；
// 2. 素材理解（图片/视频关键帧 + 本地视觉特征，输出标签、用途与质量）；
// 3. B-Roll 镜头规划（按语义段落与素材标签给出插入时间）；
// 4. 音效选点（稀疏挑选值得加音效的字幕）。
// 所有调用都要求模型只输出 JSON，解析失败会自动用更严格的提示重试一次。

#include "OralTypes.h"

#include <QJsonObject>
#include <QString>
#include <QVector>

#include <functional>

class HttpClient;

namespace Oral {

// 音效类别：名称 + 给模型看的使用场景说明。音频地址由素材服务另行获取。
struct SfxCategoryHint
{
    QString name;
    QString usageHint;
};

class LlmClient
{
public:
    using StatusCallback = std::function<void(const QString &)>;

    explicit LlmClient(HttpClient *http);

    // 语义分析：内容分段、主题提取、重点句与发布文案。
    bool analyzeSemantics(const QString &baseUrl, const QString &apiKey,
                          const SpeechAnalysis &speech, SemanticAnalysis *analysis,
                          QString *error, const StatusCallback &onStatus = {});

    // 剪口气：从逐词转写中找出可整词删除的语气词/口头禅。
    // 返回全局词序号闭区间（见 Oral::flattenSpeechWords）；提示词内置，
    // 不提供配置。没有逐词时间线时返回空结果。
    bool analyzeFillers(const QString &baseUrl, const QString &apiKey,
                        const SpeechAnalysis &speech,
                        QVector<FillerCut> *cuts, QString *error,
                        const StatusCallback &onStatus = {});

    // 素材分析：视觉特征 + 关键帧图片理解，输出每个素材的描述、标签与用途。
    // 图片输入不被当前模型支持时，会自动降级为纯文本分析。
    bool analyzeMaterials(const QString &baseUrl, const QString &apiKey,
                          const QVector<MaterialProfile> &inputs,
                          QVector<MaterialProfile> *profiles, QString *error,
                          const StatusCallback &onStatus = {});

    // 镜头规划：返回按时间排序且互不重叠的 B-Roll 镜头；具体数量由调用方
    // 设定 targetCount，模型返回值仍需在本地做边界与重叠校验。
    bool planShots(const QString &baseUrl, const QString &apiKey,
                   const SpeechAnalysis &speech, const SemanticAnalysis &semantics,
                   const QVector<MaterialProfile> &materials, int targetCount,
                   QVector<Shot> *shots, QString *error,
                   const StatusCallback &onStatus = {});

    // 音效选点：categories 与返回数组等长，-1 表示该字幕不加音效。
    bool selectSfx(const QString &baseUrl, const QString &apiKey,
                   const QVector<SubtitleCue> &cues,
                   const QVector<SfxCategoryHint> &categories,
                   QVector<int> *categoriesByCue, QString *error,
                   const StatusCallback &onStatus = {});

    static QString modelName();

private:
    HttpClient *m_http = nullptr;
};

} // namespace Oral
