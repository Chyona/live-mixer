#pragma once

#include <QString>

// 产品、公司与开源信息：侧边栏、设置页「关于」和设计说明页脚共用。
namespace Brand {

inline QString product()
{
    return QStringLiteral("LiveMix Studio");
}

inline QString company()
{
    return QStringLiteral("深圳市逗赛科技有限公司");
}

inline QString website()
{
    return QStringLiteral("https://gogoshine.com");
}

inline QString repo()
{
    return QStringLiteral("https://github.com/Chyona/live-mixer");
}

inline QString license()
{
    return QStringLiteral("Apache-2.0");
}

} // namespace Brand
