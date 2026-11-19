#pragma once

#include <QString>
#include <QUrl>

// 文件作用：集中定义编译期内置的服务地址（剪映小助手）。
// 与 ServiceConfig 不同，这里的地址不写入 config.json，设置页也不提供修改入口。
// 豆包 ASR 与 LLM 共用设置页配置的网关地址 https://api.jcaigc.cn/v1。
namespace ServiceEndpoints {

// CapCut Mate (剪映小助手) is a built-in service with a fixed address.
// API docs: https://docs.jcaigc.cn/capcut-mate/
QString capCutMateBaseUrl();

// Every CapCut Mate REST endpoint is served under this prefix, e.g.
// https://capcut-mate.jcaigc.cn/openapi/capcut-mate/v1/create_draft
QString capCutMateApiPrefix();

// Builds the full URL for a CapCut Mate endpoint, e.g. capCutMateUrl("create_draft").
QUrl capCutMateUrl(const QString &endpoint);

} // namespace ServiceEndpoints
