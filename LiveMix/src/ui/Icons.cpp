#include "Icons.h"

#include "../app/ApplicationStyle.h"

#include <QByteArray>
#include <QPainter>
#include <QPixmap>
#include <QSvgRenderer>

#include <utility>

namespace Icons {
namespace {

const char *sourceFor(Glyph glyph)
{
    switch (glyph) {
    case Glyph::Dashboard:
        return R"(<rect x="3" y="3" width="7" height="9" rx="1.5"/><rect x="14" y="3" width="7" height="5" rx="1.5"/><rect x="14" y="12" width="7" height="9" rx="1.5"/><rect x="3" y="16" width="7" height="5" rx="1.5"/>)";
    case Glyph::Library:
        return R"(<path d="M22 12l-9 9-9-9 9-9z"/>)";
    case Glyph::Prompt:
        return R"(<path d="M12 20h9M16 3a5 5 0 017 7L7 19l-4 1 1-4z"/>)";
    case Glyph::Tasks:
        return R"(<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>)";
    case Glyph::Settings:
        return R"(<circle cx="12" cy="12" r="3"/><path d="M19 12a7 7 0 00-.1-1.2l2-1.6-2-3.4-2.4 1a7 7 0 00-2-1.2L14 3h-4l-.5 2.6a7 7 0 00-2 1.2l-2.4-1-2 3.4 2 1.6a7 7 0 000 2.4l-2 1.6 2 3.4 2.4-1a7 7 0 002 1.2L10 21h4l.5-2.6a7 7 0 002-1.2l2.4 1 2-3.4-2-1.6c.1-.4.1-.8.1-1.2z"/>)";
    case Glyph::Spec:
        return R"(<path d="M12 3l9 5-9 5-9-5z"/><path d="M3 13l9 5 9-5"/>)";
    case Glyph::Plus:
        return R"(<path d="M12 5v14M5 12h14"/>)";
    case Glyph::Search:
        return R"(<circle cx="11" cy="11" r="7"/><path d="M21 21l-4-4"/>)";
    case Glyph::ChevronLeft:
        return R"(<path d="M15 18l-6-6 6-6"/>)";
    case Glyph::ChevronDown:
        return R"(<path d="M6 9l6 6 6-6"/>)";
    case Glyph::Eye:
        return R"(<path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/>)";
    case Glyph::Copy:
        return R"(<rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"/>)";
    case Glyph::Trash:
        return R"(<path d="M3 6h18M8 6V4a2 2 0 012-2h4a2 2 0 012 2v2M19 6l-1 14a2 2 0 01-2 2H8a2 2 0 01-2-2L5 6M10 11v6M14 11v6"/>)";
    case Glyph::Check:
        return R"(<path d="M20 6L9 17l-5-5"/>)";
    case Glyph::Bolt:
        return R"(<path d="M13 2L3 14h7l-1 8 10-12h-7z" fill="COLOR" stroke="none"/>)";
    case Glyph::Play:
        return R"(<path d="M8 5v14l11-7z" fill="COLOR" stroke="none"/>)";
    case Glyph::Pause:
        return R"(<path d="M7 5h4v14H7zM13 5h4v14h-4z" fill="COLOR" stroke="none"/>)";
    case Glyph::Record:
        return R"(<circle cx="12" cy="12" r="6" fill="COLOR" stroke="none"/>)";
    case Glyph::ArrowRight:
        return R"(<path d="M5 12h14M13 6l6 6-6 6"/>)";
    case Glyph::Close:
        return R"(<path d="M6 6l12 12M18 6L6 18"/>)";
    case Glyph::Folder:
        return R"(<path d="M3 7a2 2 0 012-2h4l2 2h8a2 2 0 012 2v9a2 2 0 01-2 2H5a2 2 0 01-2-2z"/>)";
    case Glyph::Alert:
        return R"(<path d="M12 3l9 16H3z"/><path d="M12 9v5M12 17h.01"/>)";
    case Glyph::Download:
        return R"(<path d="M12 3v12M7 10l5 5 5-5M4 21h16"/>)";
    case Glyph::Doc:
        return R"(<path d="M6 2.5h8l4 4v15H6z"/><path d="M14 2.5v4h4M9 12h6M9 16h4"/>)";
    case Glyph::Scissors:
        return R"(<circle cx="6" cy="6" r="2.5"/><circle cx="6" cy="18" r="2.5"/><path d="M20 4L8.5 15.5M20 20L8.5 8.5"/>)";
    case Glyph::Subtitles:
        return R"(<rect x="3" y="5" width="18" height="14" rx="2"/><path d="M7 12h4M13 12h4M7 16h7"/>)";
    case Glyph::Shield:
        return R"(<path d="M12 3l7 3v5c0 4.6-3 8.1-7 10-4-1.9-7-5.4-7-10V6z"/><path d="M9 11.5l2 2 4-4"/>)";
    case Glyph::Sparkle:
        return R"(<path d="M12 2.5l2.05 6.45L20.5 11l-6.45 2.05L12 19.5l-2.05-6.45L3.5 11l6.45-2.05z" fill="COLOR" stroke="none"/>)";
    case Glyph::Cloud:
        return R"(<path d="M7 18h10a4 4 0 001-7.9A6 6 0 006.3 9 3.5 3.5 0 007 18z"/>)";
    case Glyph::Clock:
        return R"(<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>)";
    case Glyph::Video:
        return R"(<rect x="2.5" y="5" width="19" height="14" rx="2"/><path d="M10 9l5 3-5 3z" fill="COLOR" stroke="none"/>)";
    case Glyph::Film:
        return R"(<rect x="2.5" y="4.5" width="19" height="15" rx="2"/><path d="M7 4.5v15M17 4.5v15M2.5 9h4.5M2.5 15h4.5M17 9h4.5M17 15h4.5"/>)";
    case Glyph::Batch:
        return R"(<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M8 9l2 2 4-4M8 16h8"/>)";
    case Glyph::Reset:
        return R"(<path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 00-6.74 2.74L3 8"/><path d="M3 3v5h5"/>)";
    case Glyph::Music:
        return R"(<path d="M9 18V5l10-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="16" cy="16" r="3"/>)";
    case Glyph::ExternalLink:
        return R"(<path d="M15 3h6v6"/><path d="M10 14 21 3"/><path d="M18 13v6a2 2 0 01-2 2H5a2 2 0 01-2-2V8a2 2 0 012-2h6"/>)";
    case Glyph::Github:
        return R"svg(<g transform="scale(1.5)"><path fill="COLOR" stroke="none" d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z"/></g>)svg";
    case Glyph::Window:
        return R"(<rect x="3" y="4" width="18" height="16" rx="2.5"/><path d="M3 9h18"/>)";
    case Glyph::Power:
        return R"(<path d="M12 3v9"/><path d="M6.6 6.6a8 8 0 1 0 10.8 0"/>)";
    case Glyph::Mic:
        return R"(<rect x="9" y="2.5" width="6" height="12" rx="3"/><path d="M5.5 11a6.5 6.5 0 0 0 13 0M12 17.5V21M8.5 21h7"/>)";
    case Glyph::Mixcut:
        return R"(<rect x="2.5" y="6.5" width="13" height="14.5" rx="2.5"/><path d="M6.8 6.5v14.5M11.2 6.5v14.5M2.5 11h4.3M2.5 16h4.3M11.2 11h4.3M11.2 16h4.3"/><path d="M19 2.5l.9 2.3 2.3.9-2.3.9-.9 2.3-.9-2.3-2.3-.9 2.3-.9z" fill="COLOR" stroke="none"/>)";
    case Glyph::Palette:
        return R"(<path d="M12 3a9 9 0 1 0 0 18h1.4a2.1 2.1 0 0 0 1.6-3.4 2.1 2.1 0 0 1 1.6-3.4H18a3 3 0 0 0 3-3A8.8 8.8 0 0 0 12 3z"/><circle cx="7.5" cy="11" r="1"/><circle cx="10" cy="7" r="1"/><circle cx="14.5" cy="7" r="1"/><circle cx="17" cy="10.5" r="1"/>)";
    case Glyph::Upload:
        return R"(<path d="M12 16V4M7 9l5-5 5 5"/><path d="M4 15v4a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-4"/>)";
    case Glyph::Wand:
        return R"(<path d="M15 4V2M15 10V8M11 6H9M21 6h-2M18.4 3.6l-1.4 1.4M18.4 8.4l-1.4-1.4M11.6 8.4l1.4-1.4"/><path d="M3 21 17 7l1.5 1.5L4.5 22.5z"/>)";
    case Glyph::Image:
        return R"(<rect x="3" y="4" width="18" height="16" rx="2"/><circle cx="8.5" cy="9.5" r="1.5"/><path d="m21 15-5-5L5 20"/>)";
    case Glyph::Expand:
        return R"(<path d="M9 3H3v6M15 3h6v6M21 15v6h-6M3 15v6h6"/>)";
    case Glyph::ChevronRight:
        return R"(<path d="m9 18 6-6-6-6"/>)";
    }
    return "";
}

bool isFilled(Glyph glyph)
{
    return glyph == Glyph::Bolt || glyph == Glyph::Play || glyph == Glyph::Pause
        || glyph == Glyph::Record || glyph == Glyph::Sparkle || glyph == Glyph::Github;
}

QByteArray svgMarkup(Glyph glyph, const QColor &color, qreal strokeWidth)
{
    const QByteArray hex = color.name(QColor::HexRgb).toLatin1();
    QByteArray body(sourceFor(glyph));
    body.replace("COLOR", hex);

    QByteArray svg;
    svg += R"(<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" )";
    if (!isFilled(glyph)) {
        svg += R"(fill="none" stroke=")";
        svg += hex;
        svg += R"(" stroke-width=")";
        svg += QByteArray::number(strokeWidth, 'f', 2);
        svg += R"(" stroke-linecap="round" stroke-linejoin="round")";
    } else {
        svg += R"(fill="none")";
    }
    svg += ">";
    svg += body;
    svg += "</svg>";
    return svg;
}

} // namespace

void paint(QPainter *painter, const QRectF &rect, Glyph glyph, const QColor &color,
           qreal strokeWidth)
{
    const int side = qRound(qMin(rect.width(), rect.height()));
    if (side <= 0) {
        return;
    }

    QSvgRenderer renderer(svgMarkup(glyph, color, strokeWidth));
    if (!renderer.isValid()) {
        return;
    }

    const QRectF target(rect.center().x() - side / 2.0, rect.center().y() - side / 2.0,
                        side, side);
    painter->save();
    renderer.render(painter, target);
    painter->restore();
}

QIcon glyphIcon(Glyph glyph, const QColor &color, int size, qreal strokeWidth)
{
    QIcon icon;
    const int logicalSize = qMax(1, size);
    for (int scale : {1, 2, 3}) {
        const int pixels = logicalSize * scale;
        QPixmap pixmap(pixels, pixels);
        pixmap.fill(Qt::transparent);
        QPainter painter(&pixmap);
        painter.setRenderHint(QPainter::Antialiasing, true);
        // 预留 1px，避免圆头描边在图标边缘被裁切。
        paint(&painter, QRectF(1, 1, pixels - 2, pixels - 2), glyph, color, strokeWidth);
        painter.end();
        icon.addPixmap(pixmap);
    }
    return icon;
}

void paintLogo(QPainter *painter, const QRectF &rect, const QColor &background,
               const QColor &foreground)
{
    painter->save();
    painter->setRenderHint(QPainter::Antialiasing, true);
    painter->setPen(Qt::NoPen);
    painter->setBrush(background);
    painter->drawRoundedRect(rect, rect.width() * 0.27, rect.width() * 0.27);
    const QRectF mark(rect.left() + rect.width() * 0.29, rect.top() + rect.height() * 0.27,
                      rect.width() * 0.45, rect.height() * 0.46);
    paint(painter, mark, Glyph::Play, foreground, 2.0);
    painter->restore();
}

QIcon brandIcon()
{
    static const QIcon icon(QStringLiteral(":/brand/logo.png"));
    return icon;
}

QIcon appIcon()
{
    // 任务栏 / 桌面 / 托盘使用应用图标；窗口内 UI 仍由 brandIcon() 提供。
#ifdef LIVEMIX_PLATFORM_WINDOWS
    // Windows 标准图标满幅绘制；macOS 标准图标四周保留透明留白。
    static const QIcon app(QStringLiteral(":/app/logo-windows.png"));
    if (!app.isNull()) {
        return app;
    }
#endif
    static const QIcon shared(QStringLiteral(":/app/logo.png"));
    if (!shared.isNull()) {
        return shared;
    }

    const QIcon brand = brandIcon();
    if (!brand.isNull()) {
        return brand;
    }

    // 资源缺失时退回矢量标记，保证窗口与托盘仍有可识别图标。
    QPixmap pixmap(256, 256);
    pixmap.fill(Qt::transparent);
    QPainter painter(&pixmap);
    painter.setRenderHint(QPainter::Antialiasing, true);
    paintLogo(&painter, QRectF(16, 16, 224, 224), Theme::Primary, Theme::OnAccent);
    painter.end();

    QIcon icon;
    icon.addPixmap(pixmap);
    return icon;
}

} // namespace Icons
