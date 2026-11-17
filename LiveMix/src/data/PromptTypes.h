#pragma once

#include <QString>
#include <QtGlobal>

// 提示词管理中的一条记录。
// 内置提示词来自代码常量，不写入数据库（id < 0 且 builtIn 为 true）；
// 用户创建的提示词持久化在 SQLite 中，用 id 标识。
struct PromptEntry
{
    qint64 id = -1;
    QString name;
    QString note;
    QString summary;
    // 编辑时间统一为 yyyy-MM-dd HH:mm:ss。
    QString editedAt;
    int uses = 0;
    bool builtIn = false;
    QString content;
};
