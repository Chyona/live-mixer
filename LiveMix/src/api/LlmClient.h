#pragma once

// 文件作用：调用 api.jcaigc.cn 大模型网关做 AI 选片与字幕翻译。
// 选片流程参考 live-mixer 后端：系统提示词使用用户选择的提示词，
// 用户提示词是带行号的 ASR 句段 + 固定 JSON 输出格式，
// 模型返回 indices / title / description / topics。
// 翻译用于「成片设置 → 中英文双字幕」，逐条返回与中文字幕等长的英文译文。

#include "../data/ClipTypes.h"

#include <QString>
#include <QStringList>
#include <QVector>

#include <functional>

class HttpClient;

struct LlmSelectionRequest
{
    QString baseUrl;       // 例如 https://api.jcaigc.cn/v1
    QString apiKey;
    QString systemPrompt;  // 选片提示词（内置提示词由后端导入）
    QVector<AsrUtterance> segments; // 本段待分析 ASR 句段，索引从 0 开始
    bool strictJson = false; // 解析失败重试时使用更严格的指令
    // 指定成片数量时，同段需要多次调用；这里说明本次是第几条，并排除已选句段。
    QString extraInstruction;
    QVector<int> excludedIndices;
};

struct LlmTranslationRequest
{
    QString baseUrl;
    QString apiKey;
    QStringList texts; // 按字幕顺序待翻译的中文文本
};

class LlmClient
{
public:
    using StreamCallback = std::function<void(const QString &)>;

    explicit LlmClient(HttpClient *http);

    // 流式调用：onThought 接收模型 reasoning_content 的增量（已限频），
    // onStatus 接收等待、重试等过程提示，供过程页展示“仍在工作”。
    bool selectProject(const LlmSelectionRequest &request, LlmSelectionResult *result,
                       QString *error, const StreamCallback &onThought = {},
                       const StreamCallback &onStatus = {});

    // 逐条把中文字幕翻译成英文，用于中英文双字幕。
    // translations 与 request.texts 等长，缺失或无效的条目为空；至少一条成功即返回 true。
    bool translateCaptions(const LlmTranslationRequest &request,
                           QStringList *translations, QString *error,
                           const StreamCallback &onStatus = {});

private:
    HttpClient *m_http = nullptr;
};
