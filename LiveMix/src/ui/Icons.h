#pragma once

#include <QColor>
#include <QIcon>
#include <QRectF>

class QPainter;

namespace Icons {

enum class Glyph {
    Dashboard,
    Library,
    Prompt,
    Tasks,
    Settings,
    Spec,
    Plus,
    Search,
    Bolt,
    ChevronLeft,
    ChevronDown,
    Eye,
    Copy,
    Trash,
    Check,
    Play,
    Pause,
    Record,
    ArrowRight,
    Close,
    Folder,
    Alert,
    Download,
    Doc,
    Scissors,
    Subtitles,
    Sparkle,
    Cloud,
    Clock,
    Video,
};

void paint(QPainter *painter, const QRectF &rect, Glyph glyph, const QColor &color,
           qreal strokeWidth = 2.0);
void paintLogo(QPainter *painter, const QRectF &rect, const QColor &background,
               const QColor &foreground);
// 窗口内 UI 使用的公司 Logo（Qt 资源内 256px 透明背景位图）；资源缺失时为空。
QIcon brandIcon();
// 任务栏 / 桌面 / 托盘使用的应用图标；Windows 使用满幅版本。
QIcon appIcon();

} // namespace Icons
