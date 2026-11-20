#pragma once

// 文件作用：在系统文件管理器中定位草稿文件夹与导出的成片文件，
// 并提供各平台对应的界面文案。macOS 使用 Finder 的 reveal（open -R），
// Windows 直接打开资源管理器中的目录。

#include <QString>

namespace FileManager {

// 在系统文件管理器中显示草稿文件夹。
// macOS 会在父目录中选中它并滚动到可见位置，Windows 直接打开该目录。
bool revealDraftFolder(const QString &path);

// 在系统文件管理器中定位任意文件或目录（成片 MP4、导出目录等）。
bool revealPath(const QString &path);

// 草稿导出完成后的动作文案与提示：
// 统一写作「打开草稿文件夹」，避免被误解为直接打开剪映。
QString openDraftFolderText();
// 在访达 / 资源管理器中打开剪映草稿文件夹；
// withDraftHint 为 true 时补充「可在剪映中打开该草稿继续编辑」。
QString openDraftFolderHint(bool withDraftHint = false);
// 打开草稿文件夹后的 Toast，例如：
// 已在访达中打开剪映草稿文件夹，可在剪映中打开「2026...」继续编辑
QString draftFolderOpenedMessage(const QString &draftName);

// 成片动作文案：macOS「在访达中显示」，Windows「在资源管理器中显示」。
QString revealPathLabel();

// 打开目录的动作文案：macOS「在访达中打开」，Windows「在资源管理器中打开」。
QString openFolderLabel();

} // namespace FileManager
