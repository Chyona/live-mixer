#pragma once

#include <QString>

// 文件作用：本地缓存容量控制。预览缓存与一键成片临时工作目录共享
// 「缓存上限」，超出后按最后修改时间从旧到新删除，避免无限增长。
namespace CacheCleaner {

struct CleanupResult
{
    qint64 totalBytes = 0; // 清理前的缓存总大小
    qint64 freedBytes = 0; // 本次实际释放的字节数
    int removedFiles = 0;  // 本次删除的文件数
};

// 删除缓存中最旧的文件直到总量不超过 limitBytes。
// cacheDir 为一键成片任务目录的父目录（设置中的「临时缓存目录」），
// 预览缓存固定在系统 CacheLocation 下，两者都会参与统计。
// limitBytes <= 0 时不做任何清理。该函数只做元数据扫描与删除，可安全地在
// 界面线程周期调用。
CleanupResult enforceLimit(const QString &cacheDir, qint64 limitBytes);

} // namespace CacheCleaner
