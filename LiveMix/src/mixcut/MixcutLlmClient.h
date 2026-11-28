#pragma once

// 文件作用：智能混剪专用的大模型客户端。
// 与直播切片 / 智能口播的 LLM 客户端相互独立，模型固定 deepseek-flash：
// 1. 素材理解：镜头关键帧 + 本地视觉特征 → 画面描述、标签、用途、质量、情绪；
// 2. 语音语义：内容分段、关键词、情绪、叙事结构、重点句与发布文案；
// 3. 镜头规划：按语义段与素材标签规划主画面顺序、时长与画中画布局；
// 4. 音效选点：稀疏挑选值得加音效的字幕。
// 所有调用都要求模型只输出 JSON，解析失败会自动用更严格的提示重试一次。

#include "MixcutTypes.h"

#include <QJsonObject>
#include <QString>
#include <QVector>

#include <functional>

class HttpClient;

namespace Mixcut {

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

    // 素材理解：就地把每个镜头的 description / tags / role / quality / emotion
    // 写回 clips。没有关键帧的镜头只按文件信息与视觉特征判断。
    bool analyzeMaterials(const QString &baseUrl, const QString &apiKey,
                          QVector<MaterialClip> *clips, QString *error,
                          const StatusCallback &onStatus = {});

    // 语音语义：分段、关键词、情绪、叙事结构、重点句与发布文案。
    bool analyzeSemantics(const QString &baseUrl, const QString &apiKey,
                          const SpeechAnalysis &speech,
                          SemanticAnalysis *analysis, QString *error,
                          const StatusCallback &onStatus = {});

    // 镜头规划：主画面必须连续覆盖全片；overlays 为可选画中画。
    // targetShots 是期望的主镜头数量，模型给多给少都由本地补齐/限流。
    bool planTimeline(const QString &baseUrl, const QString &apiKey,
                      const SpeechAnalysis &speech,
                      const SemanticAnalysis &semantics,
                      const MaterialLibrary &library, int targetShots,
                      QVector<Shot> *shots, QVector<Shot> *overlays,
                      QString *error, const StatusCallback &onStatus = {});

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

} // namespace Mixcut
