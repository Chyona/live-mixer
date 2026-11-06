#pragma once

// 文件作用：在系统文件管理器中定位草稿文件夹，并提供各平台对应的界面文案。
// macOS 使用 Finder 的 reveal（open -R），Windows 直接打开资源管理器中的目录。

#include <QString>

namespace FileManager {

// 在系统文件管理器中显示草稿文件夹。
// macOS 会在父目录中选中它并滚动到可见位置，Windows 直接打开该目录。
bool revealDraftFolder(const QString &path);

// 动作文案：macOS「在 Finder 中显示」，Windows「打开草稿文件夹」。
QString revealLabel();

// 完成提示：与 revealDraftFolder 的实际行为保持一致。
QString revealDoneMessage(const QString &draftTitle);

} // namespace FileManager
