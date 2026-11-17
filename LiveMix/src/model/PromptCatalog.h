#pragma once

#include "../data/PromptTypes.h"

#include <QString>
#include <QVector>

class MediaRepository;

// 提示词目录：内置模板来自代码常量，用户提示词从 SQLite 读取。
namespace PromptCatalog {

QVector<PromptEntry> builtIns();

// 内置提示词在前，用户提示词按最近编辑时间倒序在后。
// 数据库不可用时只返回内置提示词，保证提示词页始终可用。
QVector<PromptEntry> all(const MediaRepository *repository, QString *error = nullptr);

// 由正文生成列表页展示的一行摘要。
QString summarize(const QString &content);

} // namespace PromptCatalog
