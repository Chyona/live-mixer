#pragma once

// 文件作用：调用 api.jcaigc.cn 大模型网关做 AI 选片。
// 流程参考 live-mixer 后端：系统提示词使用用户选择的提示词，
// 用户提示词是带行号的 ASR 句段 + 固定 JSON 输出格式，
// 模型返回 indices / title / description / topics。

#include "../data/ClipTypes.h"

#include <QString>
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

private:
    HttpClient *m_http = nullptr;
};
