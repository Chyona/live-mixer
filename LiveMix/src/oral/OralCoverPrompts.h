#pragma once

// 文件作用：智能口播封面生成的「提示词 1」。
// 产品要求提示词 1 内置在代码中，不提供配置入口。
// 提示词内容本身参与「提示词 2」的缓存指纹，修改后旧缓存会自动失效。

#include <QString>

namespace Oral {

// 短视频封面视觉导演提示词（提示词 1）。
QString coverDirectorPrompt();

} // namespace Oral
